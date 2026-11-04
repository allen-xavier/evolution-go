package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	chatwoot_model "github.com/allen-xavier/evolution-go-chatwoot-connector/internal/model"
)

// Chatwoot waits only a few seconds for the webhook answer and marks the
// message as failed ("Net::ReadTimeout") when it takes longer, even though the
// connector goes on and delivers it. Uploading a video through the proxy (or
// converting it to MP4) takes longer than that, so the webhook is answered as
// soon as the message is stored as an outbound job, and the delivery runs in
// the background. Messages of the same conversation are sent one at a time, in
// the order Chatwoot created them.

// outboundInflightLease keeps the retry worker away from a job that is being
// delivered right after the webhook. In this process the worker also skips
// in-flight jobs; the lease only matters when the connector stops mid-send,
// and the job is then retried by the next process.
const outboundInflightLease = 2 * time.Minute

func inflightKey(instanceID string, chatwootMessageID string) string {
	return instanceID + ":" + chatwootMessageID
}

// dispatchOutbound queues the delivery of a Chatwoot message that is already
// stored as an outbound job.
func (s *chatwootService) dispatchOutbound(
	cfg *chatwoot_model.ChatwootConfig,
	payload chatwootWebhookPayload,
	job *chatwoot_model.ChatwootOutboundJob,
) {
	key := inflightKey(job.InstanceID, job.ChatwootMessageID)
	lane := fmt.Sprintf("%s:%d", job.InstanceID, payload.Conversation.ID)

	s.dispatchMu.Lock()
	if s.inflight == nil {
		s.inflight = map[string]bool{}
	}
	if s.lanes == nil {
		s.lanes = map[string][]func(){}
	}
	s.inflight[key] = false
	queue, running := s.lanes[lane]
	s.lanes[lane] = append(queue, func() { s.deliverDispatched(cfg, payload, job) })
	if !running {
		s.dispatchWG.Add(1)
	}
	s.dispatchMu.Unlock()

	if !running {
		go s.runLane(lane)
	}
}

func (s *chatwootService) runLane(lane string) {
	defer s.dispatchWG.Done()
	for {
		s.dispatchMu.Lock()
		queue := s.lanes[lane]
		if len(queue) == 0 {
			delete(s.lanes, lane)
			s.dispatchMu.Unlock()
			return
		}
		task := queue[0]
		s.lanes[lane] = queue[1:]
		s.dispatchMu.Unlock()
		task()
	}
}

// startInflight marks the job as being sent, unless the agent deleted the
// message while it waited in the lane. Both happen under dispatchMu, so a
// deletion either drops the queued message or sees it as already sent.
func (s *chatwootService) startInflight(instanceID string, chatwootMessageID string) bool {
	s.dispatchMu.Lock()
	defer s.dispatchMu.Unlock()
	if s.deletedInChatwoot(instanceID, chatwootMessageID) {
		return false
	}
	s.inflight[inflightKey(instanceID, chatwootMessageID)] = true
	return true
}

func (s *chatwootService) finishInflight(instanceID string, chatwootMessageID string) {
	s.dispatchMu.Lock()
	delete(s.inflight, inflightKey(instanceID, chatwootMessageID))
	s.dispatchMu.Unlock()
}

// inflightState reports whether the job is handled by a lane and whether its
// sending has started.
func (s *chatwootService) inflightState(instanceID string, chatwootMessageID string) (inflight bool, started bool) {
	s.dispatchMu.Lock()
	defer s.dispatchMu.Unlock()
	started, inflight = s.inflight[inflightKey(instanceID, chatwootMessageID)]
	return inflight, started
}

// deliverDispatched is the first delivery attempt of a message from the webhook.
// A failure keeps the job for the retry worker, as before.
func (s *chatwootService) deliverDispatched(
	cfg *chatwoot_model.ChatwootConfig,
	payload chatwootWebhookPayload,
	job *chatwoot_model.ChatwootOutboundJob,
) {
	instanceID := job.InstanceID
	chatwootMessageID := job.ChatwootMessageID
	logger := s.loggerWrapper.GetLogger(instanceID)
	defer s.finishInflight(instanceID, chatwootMessageID)

	if !s.startInflight(instanceID, chatwootMessageID) {
		s.failOutboundJob(job, errors.New("message deleted in Chatwoot before delivery"))
		return
	}

	err := s.deliverChatwootPayload(instanceID, cfg, payload)
	if err == nil {
		s.clearOutboundRecovery(instanceID)
		if deleteErr := s.repository.DeleteOutboundJob(job); deleteErr != nil {
			logger.LogError(
				"[%s] Chatwoot message %s reached WhatsApp, but its retry job could not be removed: %v",
				instanceID,
				chatwootMessageID,
				deleteErr,
			)
		}
		return
	}

	now := time.Now()
	job.NextAttemptAt = now.Add(outboundNextRetryDelay(err, 0))
	job.LastError = truncateText(err.Error(), 4000)
	if isPermanentOutboundError(err) {
		job.Attempts = 1
		job.FailedAt = &now
	} else {
		s.maybeRecoverOutboundSession(instanceID, err)
	}
	if saveErr := s.repository.SaveOutboundJob(job); saveErr != nil {
		// The stored job keeps its lease, so the worker still retries it.
		logger.LogError(
			"[%s] Failed to update Chatwoot message %s after delivery failure: delivery_error=%v persistence_error=%v",
			instanceID,
			chatwootMessageID,
			err,
			saveErr,
		)
		return
	}
	if job.FailedAt != nil {
		logger.LogError(
			"[%s] Chatwoot message %s recorded as a terminal delivery failure: %v",
			instanceID,
			chatwootMessageID,
			err,
		)
		s.notifyUndelivered(cfg, payload, err, false)
		return
	}
	logger.LogWarn(
		"[%s] Chatwoot message %s persisted for retry after delivery failure: %v",
		instanceID,
		chatwootMessageID,
		err,
	)
}

// WaitOutbound waits for the deliveries started by webhooks, so a stopping
// connector does not cut a send in half. Unfinished jobs are retried by the
// next process once their lease ends.
func (s *chatwootService) WaitOutbound(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		s.dispatchWG.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// markChatwootMessageFailed turns the message red in Chatwoot, now that the
// webhook no longer reports the failure. Errors only cost the red mark (the
// private note still tells the agents), so they are logged.
func (s *chatwootService) markChatwootMessageFailed(
	cfg *chatwoot_model.ChatwootConfig,
	payload chatwootWebhookPayload,
	reason string,
) {
	messageID, err := strconv.Atoi(webhookMessageID(payload.ID))
	if cfg == nil || err != nil || messageID <= 0 || payload.Conversation.ID <= 0 {
		return
	}
	route := fmt.Sprintf("/api/v1/accounts/%s/conversations/%d/messages/%d", cfg.AccountID, payload.Conversation.ID, messageID)
	if _, err := s.chatwootRequestJSON(http.MethodPatch, cfg, route, map[string]interface{}{
		"status":         "failed",
		"external_error": truncateText(strings.TrimSpace(reason), 500),
	}); err != nil {
		s.loggerWrapper.GetLogger(cfg.InstanceID).LogWarn(
			"[%s] Could not mark Chatwoot message %d as failed: %v",
			cfg.InstanceID,
			messageID,
			err,
		)
	}
}
