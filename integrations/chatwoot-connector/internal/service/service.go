package service

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	evolution "github.com/allen-xavier/evolution-go-chatwoot-connector/internal/evolution"
	logger_wrapper "github.com/allen-xavier/evolution-go-chatwoot-connector/internal/logging"
	chatwoot_model "github.com/allen-xavier/evolution-go-chatwoot-connector/internal/model"
	chatwoot_repository "github.com/allen-xavier/evolution-go-chatwoot-connector/internal/repository"
	"github.com/patrickmn/go-cache"
)

type ChatwootService interface {
	Set(instanceID string, payload *chatwoot_model.SetChatwootPayload) (*chatwoot_model.ChatwootConfigView, error)
	Find(instanceID string) (*chatwoot_model.ChatwootConfigView, error)
	HandleWebhook(instanceID string, headers http.Header, body []byte) error
	HandleEvolutionEvent(payload []byte) error
	Run(ctx context.Context)
}

const (
	outboundWorkerInterval    = 5 * time.Second
	outboundInitialRetryDelay = 15 * time.Second
	outboundMaximumRetryDelay = 5 * time.Minute
	outboundWorkerBatchSize   = 20
	// At the five-minute cap, 288 attempts retain transient failures for
	// approximately 24 hours without allowing an unbounded retry loop.
	outboundMaximumAttempts   = 288
	outboundRecoveryThreshold = 2
	outboundRecoveryWindow    = 2 * time.Minute
	outboundRecoveryCooldown  = 10 * time.Minute
	lidIdentityGracePeriod    = 5 * time.Second
	lidIdentityPollInterval   = 100 * time.Millisecond
)

type chatwootService struct {
	repository         chatwoot_repository.ChatwootRepository
	evolutionClient    evolution.API
	lidResolver        func(instanceID string, lidJID string) (string, bool)
	httpClient         *http.Client
	skipCache          *cache.Cache
	webhookCache       *cache.Cache
	evolutionCache     *cache.Cache
	contactSyncCache   *cache.Cache
	quoteCache         *cache.Cache
	lidGracePeriod     time.Duration
	lidPollInterval    time.Duration
	identityLocks      [256]sync.Mutex
	outboundRecoveryMu sync.Mutex
	outboundRecovery   map[string]outboundRecoveryState
	loggerWrapper      *logger_wrapper.Manager
}

type outboundRecoveryState struct {
	failures      int
	lastFailure   time.Time
	lastReconnect time.Time
}

type chatwootEvent struct {
	instance *evolution.Instance
	payload  []byte
}

type chatwootContactCreateResponse struct {
	ID      int `json:"id"`
	Payload []struct {
		ID int `json:"id"`
	} `json:"payload"`
}

type chatwootContactInboxCreateResponse struct {
	SourceID string `json:"source_id"`
}

type chatwootConversationCreateResponse struct {
	ID int `json:"id"`
}

type chatwootMessageCreateResponse struct {
	ID       interface{} `json:"id"`
	SourceID string      `json:"source_id"`
}

type chatwootConversationMessagesResponse struct {
	Payload []struct {
		ID       int    `json:"id"`
		SourceID string `json:"source_id"`
	} `json:"payload"`
}

type chatwootContactSearchResponse struct {
	Payload []struct {
		ID             int    `json:"id"`
		PhoneNumber    string `json:"phone_number"`
		Identifier     string `json:"identifier"`
		ContactInboxes []struct {
			SourceID string `json:"source_id"`
			Inbox    struct {
				ID int `json:"id"`
			} `json:"inbox"`
		} `json:"contact_inboxes"`
	} `json:"payload"`
}

type chatwootContactConversationsResponse struct {
	Payload []struct {
		ID       int    `json:"id"`
		InboxID  int    `json:"inbox_id"`
		SourceID string `json:"source_id"`
	} `json:"payload"`
}

type chatwootHTTPError struct {
	StatusCode int
	Body       string
}

func (e *chatwootHTTPError) Error() string {
	return fmt.Sprintf("chatwoot request failed [%d]: %s", e.StatusCode, e.Body)
}

func isChatwootNotFound(err error) bool {
	var httpErr *chatwootHTTPError
	return errors.As(err, &httpErr) && httpErr.StatusCode == http.StatusNotFound
}

type chatwootWebhookPayload struct {
	Event        string      `json:"event"`
	ID           interface{} `json:"id"`
	Content      string      `json:"content"`
	MessageType  string      `json:"message_type"`
	SourceID     string      `json:"source_id"`
	Private      bool        `json:"private"`
	Conversation struct {
		ID           int `json:"id"`
		InboxID      int `json:"inbox_id"`
		ContactInbox struct {
			SourceID string `json:"source_id"`
		} `json:"contact_inbox"`
	} `json:"conversation"`
	Sender struct {
		Type string `json:"type"`
		Name string `json:"name"`
	} `json:"sender"`
	Attachments []struct {
		FileType string `json:"file_type"`
		DataURL  string `json:"data_url"`
		FileName string `json:"file_name"`
	} `json:"attachments"`
}

type evolutionWebhookPayload struct {
	Event        string                 `json:"event"`
	Data         map[string]interface{} `json:"data"`
	InstanceID   string                 `json:"instanceId"`
	InstanceName string                 `json:"instanceName"`
}

type evolutionMessage struct {
	RemoteJID       string
	ContactName     string
	Content         string
	Media           mediaAttachment
	MessageID       string
	MessageSourceID string
	FromMe          bool
	Unavailable     bool
	IdentityAliases []string
	QuotedStanzaID  string
	QuotedSummary   string
}

type mediaAttachment struct {
	Data     []byte
	FileType string
	MIMEType string
	FileName string
}

type chatwootContactRef struct {
	SourceID   string
	Identifier string
	Phone      string
	Name       string
}

func (s *chatwootService) Set(instanceID string, payload *chatwoot_model.SetChatwootPayload) (*chatwoot_model.ChatwootConfigView, error) {
	if payload == nil {
		return nil, fmt.Errorf("payload is required")
	}
	if _, err := s.evolutionClient.GetInstance(context.Background(), instanceID); err != nil {
		return nil, err
	}

	if payload.SignDelimiter == "" {
		payload.SignDelimiter = "\n"
	}
	if payload.DaysLimitImportMessages <= 0 {
		payload.DaysLimitImportMessages = 3
	}

	if payload.Enabled {
		if strings.TrimSpace(payload.URL) == "" {
			return nil, fmt.Errorf("url is required when chatwoot is enabled")
		}
		if strings.TrimSpace(payload.AccountID) == "" {
			return nil, fmt.Errorf("accountId is required when chatwoot is enabled")
		}
		if strings.TrimSpace(payload.Token) == "" {
			return nil, fmt.Errorf("token is required when chatwoot is enabled")
		}
		if payload.InboxID <= 0 {
			return nil, fmt.Errorf("inboxId must be greater than zero when chatwoot is enabled")
		}
	}

	cfg, err := s.repository.GetConfig(instanceID)
	if err != nil {
		return nil, err
	}
	if cfg == nil {
		cfg = &chatwoot_model.ChatwootConfig{
			InstanceID: instanceID,
		}
	}

	ignoreJidsJSON := "[]"
	if len(payload.IgnoreJids) > 0 {
		raw, err := json.Marshal(payload.IgnoreJids)
		if err != nil {
			return nil, fmt.Errorf("failed to serialize ignoreJids: %v", err)
		}
		ignoreJidsJSON = string(raw)
	}

	cfg.Enabled = payload.Enabled
	cfg.AccountID = strings.TrimSpace(payload.AccountID)
	cfg.Token = strings.TrimSpace(payload.Token)
	cfg.URL = strings.TrimRight(strings.TrimSpace(payload.URL), "/")
	cfg.InboxID = payload.InboxID
	cfg.InboxIdentifier = strings.TrimSpace(payload.InboxIdentifier)
	cfg.SignMsg = payload.SignMsg
	cfg.ReopenConversation = payload.ReopenConversation
	cfg.ConversationPending = payload.ConversationPending
	cfg.MergeBrazilContacts = payload.MergeBrazilContacts
	cfg.ImportContacts = payload.ImportContacts
	cfg.ImportMessages = payload.ImportMessages
	cfg.DaysLimitImportMessages = payload.DaysLimitImportMessages
	cfg.NameInbox = strings.TrimSpace(payload.NameInbox)
	cfg.SignDelimiter = payload.SignDelimiter
	cfg.Organization = strings.TrimSpace(payload.Organization)
	cfg.Logo = strings.TrimSpace(payload.Logo)
	cfg.IgnoreJids = ignoreJidsJSON
	cfg.WebhookSecret = strings.TrimSpace(payload.WebhookSecret)

	if err := s.repository.SaveConfig(cfg); err != nil {
		return nil, err
	}

	s.loggerWrapper.GetLogger(instanceID).LogInfo("[%s] Chatwoot configuration updated", instanceID)
	return s.Find(instanceID)
}

func (s *chatwootService) Find(instanceID string) (*chatwoot_model.ChatwootConfigView, error) {
	cfg, err := s.repository.GetConfig(instanceID)
	if err != nil {
		return nil, err
	}
	if cfg == nil {
		return &chatwoot_model.ChatwootConfigView{
			Enabled:                 false,
			SignMsg:                 true,
			ReopenConversation:      true,
			ConversationPending:     false,
			MergeBrazilContacts:     true,
			ImportContacts:          true,
			ImportMessages:          true,
			SignDelimiter:           "\n",
			DaysLimitImportMessages: 3,
			IgnoreJids:              []string{},
		}, nil
	}

	return mapConfigToView(cfg), nil
}

func (s *chatwootService) shouldSkipOutgoingSync(instanceID string, messageID string) bool {
	key := fmt.Sprintf("%s:%s", instanceID, messageID)
	if _, ok := s.skipCache.Get(key); ok {
		s.skipCache.Delete(key)
		return true
	}
	return false
}

func (s *chatwootService) HandleWebhook(instanceID string, headers http.Header, body []byte) error {
	cfg, err := s.repository.GetConfig(instanceID)
	if err != nil {
		return err
	}
	if cfg == nil || !cfg.Enabled {
		return nil
	}

	if cfg.WebhookSecret != "" {
		if err := verifyWebhookSignature(cfg.WebhookSecret, headers, body); err != nil {
			return err
		}
	}

	var payload chatwootWebhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return err
	}
	if cfg.InboxID > 0 && payload.Conversation.InboxID > 0 && payload.Conversation.InboxID != cfg.InboxID {
		return nil
	}

	if payload.Event != "message_created" {
		return nil
	}
	if payload.Private {
		return nil
	}
	if payload.MessageType != "outgoing" {
		return nil
	}
	chatwootMessageID := webhookMessageID(payload.ID)
	if isEvolutionMessageSourceID(payload.SourceID) {
		s.loggerWrapper.GetLogger(instanceID).LogInfo(
			"[%s] Skipping Chatwoot echo for mirrored WhatsApp message: message=%s source_id=%s",
			instanceID,
			chatwootMessageID,
			payload.SourceID,
		)
		return nil
	}
	if payload.Sender.Type != "user" && payload.Sender.Type != "agent_bot" {
		return nil
	}
	webhookKey := fmt.Sprintf("%s:%s", instanceID, chatwootMessageID)
	if err := s.webhookCache.Add(webhookKey, true, 24*time.Hour); err != nil {
		s.loggerWrapper.GetLogger(instanceID).LogInfo("[%s] Skipping duplicated Chatwoot webhook message: %s", instanceID, chatwootMessageID)
		return nil
	}

	if err := s.deliverChatwootPayload(instanceID, cfg, payload); err != nil {
		now := time.Now()
		job := &chatwoot_model.ChatwootOutboundJob{
			InstanceID:        instanceID,
			ChatwootMessageID: chatwootMessageID,
			Payload:           append([]byte(nil), body...),
			NextAttemptAt:     now.Add(outboundInitialRetryDelay),
			LastError:         truncateText(err.Error(), 4000),
		}
		if isPermanentOutboundError(err) {
			job.Attempts = 1
			job.FailedAt = &now
		} else {
			s.maybeRecoverOutboundSession(instanceID, err)
		}
		if queueErr := s.repository.EnqueueOutboundJob(job); queueErr != nil {
			s.webhookCache.Delete(webhookKey)
			return fmt.Errorf("deliver Chatwoot message %s: %v (persist retry job: %w)", chatwootMessageID, err, queueErr)
		}
		if job.FailedAt != nil {
			s.loggerWrapper.GetLogger(instanceID).LogError(
				"[%s] Chatwoot message %s recorded as a terminal delivery failure: %v",
				instanceID,
				chatwootMessageID,
				err,
			)
			return nil
		}
		s.loggerWrapper.GetLogger(instanceID).LogWarn(
			"[%s] Chatwoot message %s persisted for retry after delivery failure: %v",
			instanceID,
			chatwootMessageID,
			err,
		)
		return nil
	}
	s.clearOutboundRecovery(instanceID)

	return nil
}

func (s *chatwootService) deliverChatwootPayload(
	instanceID string,
	cfg *chatwoot_model.ChatwootConfig,
	payload chatwootWebhookPayload,
) error {
	chatwootMessageID := webhookMessageID(payload.ID)
	instance, err := s.evolutionClient.GetInstance(context.Background(), instanceID)
	if err != nil {
		return err
	}

	remoteJID := ""
	binding, err := s.repository.GetBindingByConversationID(instanceID, payload.Conversation.ID)
	if err != nil {
		return err
	}
	if binding != nil {
		remoteJID = binding.RemoteJID
	}
	if remoteJID == "" {
		remoteJID = remoteJIDFromSourceID(instanceID, payload.Conversation.ContactInbox.SourceID)
	}
	if isLIDRemoteJID(remoteJID) {
		canonicalJID, resolveErr := s.repository.ResolveIdentityAlias(instanceID, remoteJID)
		if resolveErr != nil {
			return fmt.Errorf("resolve WhatsApp LID %s: %w", remoteJID, resolveErr)
		}
		if isPhoneRemoteJID(canonicalJID) {
			remoteJID = canonicalJID
		}
	}
	if remoteJID == "" {
		return fmt.Errorf("unable to resolve remote jid for conversation %d", payload.Conversation.ID)
	}
	s.loggerWrapper.GetLogger(instanceID).LogInfo("[%s] Chatwoot webhook resolved recipient: remote=%s message=%s", instanceID, remoteJID, chatwootMessageID)

	var sendErrors []string
	hasAttachment := false
	for _, attachment := range payload.Attachments {
		if strings.TrimSpace(attachment.DataURL) != "" {
			hasAttachment = true
			break
		}
	}

	content := applyChatwootSignature(cfg, payload.Content, payload.Sender.Name)
	if strings.TrimSpace(content) != "" && !hasAttachment {
		if err := s.sendTextFromChatwoot(instance, remoteJID, content, chatwootMessageID); err != nil {
			s.loggerWrapper.GetLogger(instanceID).LogError("[%s] Failed to send Chatwoot text message %s to WhatsApp: %v", instanceID, chatwootMessageID, err)
			sendErrors = append(sendErrors, fmt.Sprintf("text:%v", err))
		}
	}

	attachmentIndex := 0
	for _, attachment := range payload.Attachments {
		if attachment.DataURL == "" {
			continue
		}
		caption := ""
		if attachmentIndex == 0 {
			caption = content
		}
		attachmentIndex++
		if err := s.sendMediaFromChatwoot(
			instance,
			remoteJID,
			attachment.DataURL,
			attachment.FileType,
			attachment.FileName,
			caption,
			chatwootMessageID,
		); err != nil {
			s.loggerWrapper.GetLogger(instanceID).LogError("[%s] Failed to send Chatwoot media message %s to WhatsApp: %v", instanceID, chatwootMessageID, err)
			sendErrors = append(sendErrors, fmt.Sprintf("media:%v", err))
		}
	}

	if len(sendErrors) > 0 {
		return fmt.Errorf("failed to deliver chatwoot message %s: %s", chatwootMessageID, strings.Join(sendErrors, " | "))
	}

	return nil
}

func (s *chatwootService) Run(ctx context.Context) {
	s.processDueOutboundJobs(ctx)
	ticker := time.NewTicker(outboundWorkerInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.processDueOutboundJobs(ctx)
		}
	}
}

func (s *chatwootService) processDueOutboundJobs(ctx context.Context) {
	jobs, err := s.repository.ListDueOutboundJobs(time.Now(), outboundWorkerBatchSize)
	if err != nil {
		s.loggerWrapper.GetLogger("outbound-worker").LogError("Failed to load durable Chatwoot outbound jobs: %v", err)
		return
	}

	for index := range jobs {
		if ctx.Err() != nil {
			return
		}
		if s.processOutboundJob(&jobs[index]) {
			// Let the instance finish reconnecting before processing more queued
			// messages. The worker will pick them up on its next tick.
			break
		}
	}
}

// processOutboundJob reports whether it initiated a session reconnect.
func (s *chatwootService) processOutboundJob(job *chatwoot_model.ChatwootOutboundJob) bool {
	if job == nil {
		return false
	}

	var payload chatwootWebhookPayload
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		s.failOutboundJob(job, fmt.Errorf("invalid stored Chatwoot webhook: %w", err))
		return false
	}

	cfg, err := s.repository.GetConfig(job.InstanceID)
	if err != nil {
		s.rescheduleOutboundJob(job, fmt.Errorf("load Chatwoot config: %w", err))
		return false
	}
	if cfg == nil || !cfg.Enabled {
		s.rescheduleOutboundJob(job, errors.New("Chatwoot integration is disabled"))
		return false
	}

	if err := s.deliverChatwootPayload(job.InstanceID, cfg, payload); err != nil {
		reconnected := s.maybeRecoverOutboundSession(job.InstanceID, err)
		s.rescheduleOutboundJob(job, err)
		return reconnected
	}
	s.clearOutboundRecovery(job.InstanceID)

	if err := s.repository.DeleteOutboundJob(job); err != nil {
		s.loggerWrapper.GetLogger(job.InstanceID).LogError(
			"[%s] Chatwoot message %s reached WhatsApp, but its retry job could not be removed: %v",
			job.InstanceID,
			job.ChatwootMessageID,
			err,
		)
		return false
	}
	s.loggerWrapper.GetLogger(job.InstanceID).LogInfo(
		"[%s] Durable Chatwoot message %s delivered to WhatsApp after %d retries",
		job.InstanceID,
		job.ChatwootMessageID,
		job.Attempts+1,
	)
	return false
}

func (s *chatwootService) rescheduleOutboundJob(job *chatwoot_model.ChatwootOutboundJob, deliveryErr error) {
	job.Attempts++
	job.LastError = truncateText(deliveryErr.Error(), 4000)
	if isPermanentOutboundError(deliveryErr) || job.Attempts >= outboundMaximumAttempts {
		s.failOutboundJob(job, deliveryErr)
		return
	}
	job.FailedAt = nil
	job.NextAttemptAt = time.Now().Add(outboundRetryDelay(job.Attempts))
	if err := s.repository.SaveOutboundJob(job); err != nil {
		s.loggerWrapper.GetLogger(job.InstanceID).LogError(
			"[%s] Failed to reschedule durable Chatwoot message %s: delivery_error=%v persistence_error=%v",
			job.InstanceID,
			job.ChatwootMessageID,
			deliveryErr,
			err,
		)
		return
	}
	s.loggerWrapper.GetLogger(job.InstanceID).LogWarn(
		"[%s] Durable Chatwoot message %s retry %d failed; next attempt at %s: %v",
		job.InstanceID,
		job.ChatwootMessageID,
		job.Attempts,
		job.NextAttemptAt.Format(time.RFC3339),
		deliveryErr,
	)
}

func (s *chatwootService) failOutboundJob(job *chatwoot_model.ChatwootOutboundJob, deliveryErr error) {
	if job.Attempts == 0 {
		job.Attempts = 1
	}
	now := time.Now()
	job.FailedAt = &now
	job.LastError = truncateText(deliveryErr.Error(), 4000)
	if err := s.repository.SaveOutboundJob(job); err != nil {
		s.loggerWrapper.GetLogger(job.InstanceID).LogError(
			"[%s] Failed to record terminal Chatwoot message %s: delivery_error=%v persistence_error=%v",
			job.InstanceID,
			job.ChatwootMessageID,
			deliveryErr,
			err,
		)
		return
	}
	s.loggerWrapper.GetLogger(job.InstanceID).LogError(
		"[%s] Durable Chatwoot message %s stopped after %d attempts and was retained for audit: %v",
		job.InstanceID,
		job.ChatwootMessageID,
		job.Attempts,
		deliveryErr,
	)
}

func isPermanentOutboundError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "not registered on whatsapp") ||
		strings.Contains(message, "is not registered")
}

func isRecoverableSessionError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "server returned error 400")
}

func (s *chatwootService) maybeRecoverOutboundSession(instanceID string, deliveryErr error) bool {
	instanceID = strings.TrimSpace(instanceID)
	if instanceID == "" || !isRecoverableSessionError(deliveryErr) {
		return false
	}

	now := time.Now()
	s.outboundRecoveryMu.Lock()
	if s.outboundRecovery == nil {
		s.outboundRecovery = map[string]outboundRecoveryState{}
	}
	state := s.outboundRecovery[instanceID]
	if state.lastFailure.IsZero() || now.Sub(state.lastFailure) > outboundRecoveryWindow {
		state.failures = 0
	}
	state.failures++
	state.lastFailure = now
	if state.failures < outboundRecoveryThreshold ||
		(!state.lastReconnect.IsZero() && now.Sub(state.lastReconnect) < outboundRecoveryCooldown) {
		s.outboundRecovery[instanceID] = state
		s.outboundRecoveryMu.Unlock()
		return false
	}
	state.failures = 0
	state.lastReconnect = now
	s.outboundRecovery[instanceID] = state
	s.outboundRecoveryMu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := s.evolutionClient.ReconnectInstance(ctx, instanceID); err != nil {
		s.loggerWrapper.GetLogger(instanceID).LogError(
			"[%s] Automatic WhatsApp session reconnect failed after repeated delivery errors: %v",
			instanceID,
			err,
		)
		return false
	}
	s.loggerWrapper.GetLogger(instanceID).LogWarn(
		"[%s] Automatic WhatsApp session reconnect started after %d delivery errors returned server error 400",
		instanceID,
		outboundRecoveryThreshold,
	)
	return true
}

func (s *chatwootService) clearOutboundRecovery(instanceID string) {
	s.outboundRecoveryMu.Lock()
	delete(s.outboundRecovery, strings.TrimSpace(instanceID))
	s.outboundRecoveryMu.Unlock()
}

func outboundRetryDelay(attempt int) time.Duration {
	if attempt <= 1 {
		return outboundInitialRetryDelay
	}
	delay := outboundInitialRetryDelay
	for current := 1; current < attempt; current++ {
		delay *= 2
		if delay >= outboundMaximumRetryDelay {
			return outboundMaximumRetryDelay
		}
	}
	return delay
}

func truncateText(value string, max int) string {
	if max <= 0 || len(value) <= max {
		return value
	}
	return value[:max]
}

func (s *chatwootService) HandleEvolutionEvent(raw []byte) error {
	var payload evolutionWebhookPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return fmt.Errorf("invalid evolution webhook: %w", err)
	}
	if payload.Event != "Message" && payload.Event != "SendMessage" {
		return nil
	}
	if strings.TrimSpace(payload.InstanceID) == "" {
		return fmt.Errorf("evolution webhook is missing instanceId")
	}

	event := chatwootEvent{
		instance: &evolution.Instance{Id: payload.InstanceID, Name: payload.InstanceName},
		payload:  append([]byte(nil), raw...),
	}
	return s.syncEvolutionEventToChatwoot(event)
}

func (s *chatwootService) syncEvolutionEventToChatwoot(evt chatwootEvent) error {
	instance := evt.instance
	if instance == nil {
		return nil
	}

	cfg, err := s.repository.GetConfig(instance.Id)
	if err != nil {
		s.loggerWrapper.GetLogger(instance.Id).LogError("[%s] Failed to load Chatwoot config: %v", instance.Id, err)
		return fmt.Errorf("load Chatwoot config for %s: %w", instance.Id, err)
	}
	if cfg == nil || !cfg.Enabled {
		return nil
	}

	var payload evolutionWebhookPayload
	if err := json.Unmarshal(evt.payload, &payload); err != nil {
		s.loggerWrapper.GetLogger(instance.Id).LogError("[%s] Failed to parse Evolution event for Chatwoot: %v", instance.Id, err)
		return fmt.Errorf("parse Evolution event for %s: %w", instance.Id, err)
	}
	if payload.InstanceID == "" {
		payload.InstanceID = instance.Id
	}
	if payload.Event != "Message" && payload.Event != "SendMessage" {
		return nil
	}

	message, ok := s.extractEvolutionMessage(payload, evt.payload)
	if !ok {
		return nil
	}

	if message.RemoteJID == "" {
		return nil
	}
	if strings.HasSuffix(message.RemoteJID, "@g.us") ||
		strings.HasSuffix(message.RemoteJID, "@newsletter") ||
		strings.Contains(message.RemoteJID, "@broadcast") {
		return nil
	}

	if err := s.canonicalizeMessageIdentity(instance.Id, &message); err != nil {
		return fmt.Errorf("canonicalize WhatsApp identity for %s: %w", instance.Id, err)
	}
	if message.Unavailable {
		return fmt.Errorf(
			"WhatsApp message %s from %s is unavailable or could not be decrypted",
			message.MessageID,
			message.RemoteJID,
		)
	}
	unlockIdentity := s.lockMessageIdentity(instance.Id, message)
	defer unlockIdentity()

	if message.FromMe && s.shouldSkipOutgoingSync(instance.Id, message.MessageID) {
		return nil
	}
	if err := s.stabilizeLIDIdentity(instance.Id, &message); err != nil {
		return fmt.Errorf("stabilize WhatsApp identity for %s: %w", instance.Id, err)
	}
	if shouldIgnoreJID(cfg.IgnoreJids, message.RemoteJID) {
		return nil
	}

	messageType := "incoming"
	if message.FromMe {
		messageType = "outgoing"
	}

	eventKey := fmt.Sprintf("%s:%s", instance.Id, message.MessageSourceID)
	if err := s.evolutionCache.Add(eventKey, true, 24*time.Hour); err != nil {
		reconciled, reconcileErr := s.reconcileDuplicateEvolutionIdentity(instance, cfg, message, messageType)
		if reconcileErr != nil {
			s.loggerWrapper.GetLogger(instance.Id).LogError(
				"[%s] Failed to reconcile duplicated Evolution message %s: %v",
				instance.Id,
				message.MessageID,
				reconcileErr,
			)
			return fmt.Errorf("reconcile duplicated WhatsApp message %s: %w", message.MessageID, reconcileErr)
		}
		if reconciled {
			return nil
		}
		s.loggerWrapper.GetLogger(instance.Id).LogInfo(
			"[%s] Skipping duplicated Evolution message: id=%s remote=%s",
			instance.Id,
			message.MessageID,
			message.RemoteJID,
		)
		return nil
	}

	hasMedia := len(message.Media.Data) > 0 && message.Media.FileType != ""
	if strings.TrimSpace(message.Content) == "" && !hasMedia {
		message.Content = "[message]"
	}

	if !hasMedia {
		message.Media = mediaAttachment{}
		if err := s.syncEvolutionMessageWithBindingRefresh(instance, cfg, message, messageType); err != nil {
			s.evolutionCache.Delete(eventKey)
			s.loggerWrapper.GetLogger(instance.Id).LogError("[%s] Failed to sync message to Chatwoot: %v", instance.Id, err)
			return fmt.Errorf("sync WhatsApp message %s to Chatwoot: %w", message.MessageID, err)
		}
		s.logEvolutionSyncSuccess(instance.Id, message, messageType)
		return nil
	}

	if err := s.syncEvolutionMessageWithBindingRefresh(instance, cfg, message, messageType); err != nil {
		s.evolutionCache.Delete(eventKey)
		s.loggerWrapper.GetLogger(instance.Id).LogError("[%s] Failed to sync media message to Chatwoot: %v", instance.Id, err)
		return fmt.Errorf("sync WhatsApp media message %s to Chatwoot: %w", message.MessageID, err)
	}
	s.logEvolutionSyncSuccess(instance.Id, message, messageType)
	return nil
}

func (s *chatwootService) stabilizeLIDIdentity(instanceID string, message *evolutionMessage) error {
	if message == nil || !isLIDRemoteJID(message.RemoteJID) {
		return nil
	}

	lidJID := normalizeRemoteJID(message.RemoteJID)
	resolve := func() (string, error) {
		canonicalJID, err := s.repository.ResolveIdentityAlias(instanceID, lidJID)
		if err != nil {
			return "", err
		}
		if isPhoneRemoteJID(canonicalJID) {
			return normalizeRemoteJID(canonicalJID), nil
		}
		if s.lidResolver == nil {
			return "", nil
		}
		resolved, ok := s.lidResolver(instanceID, lidJID)
		if !ok || !isPhoneRemoteJID(resolved) {
			return "", nil
		}
		resolved = normalizeRemoteJID(resolved)
		if err := s.repository.SaveIdentityAlias(instanceID, lidJID, resolved); err != nil {
			return "", err
		}
		return resolved, nil
	}

	canonicalJID, err := resolve()
	if err != nil {
		return err
	}
	if canonicalJID == "" && s.lidGracePeriod > 0 {
		deadline := time.NewTimer(s.lidGracePeriod)
		pollInterval := s.lidPollInterval
		if pollInterval <= 0 {
			pollInterval = lidIdentityPollInterval
		}
		if pollInterval >= s.lidGracePeriod {
			pollInterval = s.lidGracePeriod / 2
			if pollInterval <= 0 {
				pollInterval = time.Nanosecond
			}
		}
		poll := time.NewTicker(pollInterval)
		defer deadline.Stop()
		defer poll.Stop()

		for canonicalJID == "" {
			select {
			case <-poll.C:
				canonicalJID, err = resolve()
				if err != nil {
					return err
				}
			case <-deadline.C:
				return nil
			}
		}
	}
	if canonicalJID == "" {
		return nil
	}

	if !containsJID(message.IdentityAliases, lidJID) {
		message.IdentityAliases = append(message.IdentityAliases, lidJID)
	}
	message.RemoteJID = canonicalJID
	return nil
}

func (s *chatwootService) reconcileDuplicateEvolutionIdentity(
	instance *evolution.Instance,
	cfg *chatwoot_model.ChatwootConfig,
	message evolutionMessage,
	messageType string,
) (bool, error) {
	if instance == nil || !isPhoneRemoteJID(message.RemoteJID) {
		return false, nil
	}

	aliases := uniqueLIDJIDs(message.IdentityAliases)
	if len(aliases) == 0 {
		return false, nil
	}

	provisional, err := s.findAliasBinding(instance.Id, aliases)
	if err != nil {
		return false, err
	}
	if provisional == nil {
		return false, nil
	}

	canonical, err := s.repository.GetBindingByRemoteJID(instance.Id, message.RemoteJID)
	if err != nil {
		return false, err
	}
	contactRef := buildChatwootContactRef(instance.Id, message.RemoteJID, message.ContactName, cfg.MergeBrazilContacts)

	if canonical == nil {
		previousJID := provisional.RemoteJID
		provisional.RemoteJID = message.RemoteJID
		if err := s.repository.SaveBinding(provisional); err != nil {
			return false, fmt.Errorf("migrate duplicated LID binding: %w", err)
		}
		s.syncChatwootContactIdentity(cfg, provisional, contactRef)
		s.loggerWrapper.GetLogger(instance.Id).LogInfo(
			"[%s] Reconciled duplicated Evolution message %s by migrating conversation %d from %s to %s",
			instance.Id,
			message.MessageID,
			provisional.ConversationID,
			previousJID,
			message.RemoteJID,
		)
		return true, nil
	}
	if canonical.ID == provisional.ID {
		return false, nil
	}

	if err := s.sendMessageToChatwoot(cfg, canonical, message, messageType); err != nil {
		return false, fmt.Errorf("reroute duplicated message to canonical conversation %d: %w", canonical.ConversationID, err)
	}
	if err := s.repository.DeleteBinding(provisional); err != nil {
		s.loggerWrapper.GetLogger(instance.Id).LogWarn(
			"[%s] Duplicated Evolution message %s reached canonical conversation %d, but provisional LID binding %d could not be removed: %v",
			instance.Id,
			message.MessageID,
			canonical.ConversationID,
			provisional.ID,
			err,
		)
		return true, nil
	}
	s.loggerWrapper.GetLogger(instance.Id).LogWarn(
		"[%s] Rerouted duplicated Evolution message %s from provisional conversation %d to canonical conversation %d (%s)",
		instance.Id,
		message.MessageID,
		provisional.ConversationID,
		canonical.ConversationID,
		message.RemoteJID,
	)
	return true, nil
}

func (s *chatwootService) lockMessageIdentity(instanceID string, message evolutionMessage) func() {
	identityJID := normalizeRemoteJID(message.RemoteJID)
	if aliases := uniqueLIDJIDs(message.IdentityAliases); len(aliases) > 0 {
		identityJID = aliases[0]
	}
	key := strings.TrimSpace(instanceID) + "|" + identityJID
	var hash uint32 = 2166136261
	for index := 0; index < len(key); index++ {
		hash ^= uint32(key[index])
		hash *= 16777619
	}
	mutex := &s.identityLocks[hash%uint32(len(s.identityLocks))]
	mutex.Lock()
	return mutex.Unlock
}

func (s *chatwootService) syncEvolutionMessageWithBindingRefresh(
	instance *evolution.Instance,
	cfg *chatwoot_model.ChatwootConfig,
	message evolutionMessage,
	messageType string,
) error {
	binding, err := s.getOrCreateBindingByRemote(instance, cfg, message.RemoteJID, message.IdentityAliases, message.ContactName, false)
	if err != nil {
		return fmt.Errorf("failed to resolve Chatwoot binding: %v", err)
	}

	err = s.sendMessageToChatwoot(cfg, binding, message, messageType)
	if !isChatwootNotFound(err) {
		return err
	}

	s.loggerWrapper.GetLogger(instance.Id).LogWarn("[%s] Chatwoot binding not found remotely; recreating contact/conversation for %s", instance.Id, message.RemoteJID)

	binding, refreshErr := s.getOrCreateBindingByRemote(instance, cfg, message.RemoteJID, message.IdentityAliases, message.ContactName, true)
	if refreshErr != nil {
		return fmt.Errorf("%v (failed to refresh Chatwoot binding: %v)", err, refreshErr)
	}

	return s.sendMessageToChatwoot(cfg, binding, message, messageType)
}

func (s *chatwootService) getOrCreateBindingByRemote(
	instance *evolution.Instance,
	cfg *chatwoot_model.ChatwootConfig,
	remoteJID string,
	identityAliases []string,
	contactName string,
	forceRefresh bool,
) (*chatwoot_model.ChatwootBinding, error) {
	remoteJID = normalizeRemoteJID(remoteJID)
	contactRef := buildChatwootContactRef(instance.Id, remoteJID, contactName, cfg.MergeBrazilContacts)

	binding, err := s.repository.GetBindingByRemoteJID(instance.Id, remoteJID)
	if err != nil {
		return nil, err
	}
	if !forceRefresh && binding != nil {
		if err := s.removeDuplicateAliasBindings(instance.Id, binding, identityAliases); err != nil {
			return nil, err
		}
		if isPhoneRemoteJID(remoteJID) {
			s.syncChatwootContactIdentity(cfg, binding, contactRef)
		}
		return binding, nil
	}

	if !forceRefresh && binding == nil && isPhoneRemoteJID(remoteJID) {
		provisional, migrateErr := s.findAliasBinding(instance.Id, identityAliases)
		if migrateErr != nil {
			return nil, migrateErr
		}
		if provisional != nil {
			previousJID := provisional.RemoteJID
			provisional.RemoteJID = remoteJID
			if err := s.repository.SaveBinding(provisional); err != nil {
				return nil, fmt.Errorf("migrate provisional LID binding: %w", err)
			}
			s.loggerWrapper.GetLogger(instance.Id).LogInfo(
				"[%s] Migrated Chatwoot conversation %d from WhatsApp LID %s to %s",
				instance.Id,
				provisional.ConversationID,
				previousJID,
				remoteJID,
			)
			s.syncChatwootContactIdentity(cfg, provisional, contactRef)
			return provisional, nil
		}
	}

	contactID, err := s.createChatwootContact(cfg, contactRef)
	if err != nil {
		return nil, err
	}

	if err := s.createChatwootContactInbox(cfg, contactID, contactRef.SourceID); err != nil {
		return nil, err
	}

	conversationID, err := s.createChatwootConversation(cfg, contactID, contactRef.SourceID)
	if err != nil {
		return nil, err
	}

	if binding == nil {
		binding = &chatwoot_model.ChatwootBinding{
			InstanceID: instance.Id,
			RemoteJID:  remoteJID,
		}
	}
	binding.ContactID = contactID
	binding.ConversationID = conversationID
	binding.SourceID = contactRef.SourceID

	if err := s.repository.SaveBinding(binding); err != nil {
		existing, lookupErr := s.repository.GetBindingByRemoteJID(instance.Id, remoteJID)
		if lookupErr == nil && existing != nil {
			return existing, nil
		}
		return nil, err
	}

	return binding, nil
}

func (s *chatwootService) canonicalizeMessageIdentity(instanceID string, message *evolutionMessage) error {
	if message == nil {
		return nil
	}

	message.RemoteJID = normalizeRemoteJID(message.RemoteJID)
	message.IdentityAliases = uniqueLIDJIDs(message.IdentityAliases)

	if isPhoneRemoteJID(message.RemoteJID) {
		for _, aliasJID := range message.IdentityAliases {
			if err := s.repository.SaveIdentityAlias(instanceID, aliasJID, message.RemoteJID); err != nil {
				return fmt.Errorf("save alias %s -> %s: %w", aliasJID, message.RemoteJID, err)
			}
		}
		return nil
	}

	if !isLIDRemoteJID(message.RemoteJID) {
		return nil
	}

	canonicalJID, err := s.repository.ResolveIdentityAlias(instanceID, message.RemoteJID)
	if err != nil {
		return err
	}
	if isPhoneRemoteJID(canonicalJID) {
		if !containsJID(message.IdentityAliases, message.RemoteJID) {
			message.IdentityAliases = append(message.IdentityAliases, message.RemoteJID)
		}
		message.RemoteJID = normalizeRemoteJID(canonicalJID)
	}
	return nil
}

func (s *chatwootService) findAliasBinding(instanceID string, identityAliases []string) (*chatwoot_model.ChatwootBinding, error) {
	for _, aliasJID := range uniqueLIDJIDs(identityAliases) {
		binding, err := s.repository.GetBindingByRemoteJID(instanceID, aliasJID)
		if err != nil {
			return nil, err
		}
		if binding != nil {
			return binding, nil
		}
	}
	return nil, nil
}

func (s *chatwootService) removeDuplicateAliasBindings(
	instanceID string,
	canonicalBinding *chatwoot_model.ChatwootBinding,
	identityAliases []string,
) error {
	if canonicalBinding == nil {
		return nil
	}
	for _, aliasJID := range uniqueLIDJIDs(identityAliases) {
		aliasBinding, err := s.repository.GetBindingByRemoteJID(instanceID, aliasJID)
		if err != nil {
			return err
		}
		if aliasBinding == nil || aliasBinding.ID == canonicalBinding.ID {
			continue
		}
		if err := s.repository.DeleteBinding(aliasBinding); err != nil {
			return fmt.Errorf("remove duplicate LID binding %s: %w", aliasJID, err)
		}
		s.loggerWrapper.GetLogger(instanceID).LogWarn(
			"[%s] Removed duplicate LID binding for conversation %d; canonical conversation is %d (%s)",
			instanceID,
			aliasBinding.ConversationID,
			canonicalBinding.ConversationID,
			canonicalBinding.RemoteJID,
		)
	}
	return nil
}

func (s *chatwootService) syncChatwootContactIdentity(
	cfg *chatwoot_model.ChatwootConfig,
	binding *chatwoot_model.ChatwootBinding,
	contactRef chatwootContactRef,
) {
	if cfg == nil || binding == nil || binding.ContactID <= 0 {
		return
	}

	body := map[string]interface{}{}
	if strings.TrimSpace(contactRef.Phone) != "" {
		body["phone_number"] = contactRef.Phone
	}
	if name := strings.TrimSpace(contactRef.Name); name != "" && !isLIDRemoteJID(name) {
		body["name"] = name
	}
	if len(body) == 0 {
		return
	}

	cacheKey := fmt.Sprintf(
		"%s:%d:%s:%s",
		binding.InstanceID,
		binding.ContactID,
		strings.TrimSpace(contactRef.Phone),
		strings.TrimSpace(contactRef.Name),
	)
	if s.contactSyncCache != nil {
		if err := s.contactSyncCache.Add(cacheKey, true, 24*time.Hour); err != nil {
			return
		}
	}

	route := fmt.Sprintf("/api/v1/accounts/%s/contacts/%d", cfg.AccountID, binding.ContactID)
	if _, err := s.chatwootRequestJSON(http.MethodPut, cfg, route, body); err != nil {
		if s.contactSyncCache != nil {
			s.contactSyncCache.Delete(cacheKey)
		}
		s.loggerWrapper.GetLogger(binding.InstanceID).LogWarn(
			"[%s] Chatwoot contact %d for conversation %d could not be synchronized: %v",
			binding.InstanceID,
			binding.ContactID,
			binding.ConversationID,
			err,
		)
	}
}

func (s *chatwootService) logEvolutionSyncSuccess(instanceID string, message evolutionMessage, messageType string) {
	s.loggerWrapper.GetLogger(instanceID).LogInfo(
		"[%s] WhatsApp message synced to Chatwoot: id=%s remote=%s direction=%s media=%t",
		instanceID,
		message.MessageID,
		message.RemoteJID,
		messageType,
		len(message.Media.Data) > 0,
	)
}

func uniqueLIDJIDs(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = normalizeRemoteJID(value)
		if !isLIDRemoteJID(value) {
			continue
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, value)
	}
	return result
}

func containsJID(values []string, expected string) bool {
	expected = strings.ToLower(normalizeRemoteJID(expected))
	for _, value := range values {
		if strings.ToLower(normalizeRemoteJID(value)) == expected {
			return true
		}
	}
	return false
}

func (s *chatwootService) createChatwootContact(cfg *chatwoot_model.ChatwootConfig, contactRef chatwootContactRef) (int, error) {
	body := map[string]interface{}{
		"inbox_id":   cfg.InboxID,
		"name":       contactRef.Name,
		"identifier": contactRef.Identifier,
	}
	if strings.TrimSpace(contactRef.Phone) != "" {
		body["phone_number"] = contactRef.Phone
	}

	respBody, err := s.chatwootRequestJSON(http.MethodPost, cfg, fmt.Sprintf("/api/v1/accounts/%s/contacts", cfg.AccountID), body)
	if err != nil {
		var httpErr *chatwootHTTPError
		if errors.As(err, &httpErr) && httpErr.StatusCode == http.StatusUnprocessableEntity {
			// Contact may already exist in the account (another inbox/instance).
			// Reuse it to avoid stopping message sync.
			existingID, lookupErr := s.findExistingContactID(cfg, contactRef)
			if lookupErr == nil && existingID > 0 {
				return existingID, nil
			}
			if lookupErr != nil {
				return 0, fmt.Errorf("%v (lookup existing contact failed: %v)", err, lookupErr)
			}
		}
		return 0, err
	}

	contactID, err := parseChatwootContactID(respBody)
	if err != nil {
		return 0, err
	}
	return contactID, nil
}

func (s *chatwootService) findExistingContactID(cfg *chatwoot_model.ChatwootConfig, contactRef chatwootContactRef) (int, error) {
	identifier := strings.TrimSpace(contactRef.Identifier)
	phone := strings.TrimSpace(contactRef.Phone)
	sourceID := strings.TrimSpace(contactRef.SourceID)
	normalizedPhone := normalizePhoneForCompare(phone)

	queries := uniqueNonEmptyStrings([]string{
		identifier,
		sourceID,
		phone,
		strings.TrimPrefix(phone, "+"),
		normalizedPhone,
	})

	seen := make(map[int]struct{})
	candidates := make([]chatwootContactSearchResponse, 0, len(queries))

	for _, q := range queries {
		route := fmt.Sprintf("/api/v1/accounts/%s/contacts/search?q=%s", cfg.AccountID, url.Values{"q": []string{q}}.Encode())
		respBody, err := s.chatwootRequestJSON(http.MethodGet, cfg, route, nil)
		if err != nil {
			continue
		}

		var resp chatwootContactSearchResponse
		if err := json.Unmarshal(respBody, &resp); err != nil {
			continue
		}
		candidates = append(candidates, resp)
	}

	// Fallback for Chatwoot versions where /contacts/search is limited:
	// use /contacts/filter with exact identifier and phone matching.
	type filterReq struct {
		Attribute string
		Value     string
	}
	filterRequests := make([]filterReq, 0, 6)
	if identifier != "" {
		filterRequests = append(filterRequests, filterReq{Attribute: "identifier", Value: identifier})
	}
	for _, phoneValue := range uniqueNonEmptyStrings([]string{phone, strings.TrimPrefix(phone, "+"), normalizedPhone}) {
		filterRequests = append(filterRequests, filterReq{Attribute: "phone_number", Value: phoneValue})
	}

	for _, fr := range filterRequests {
		filterBody := map[string]interface{}{
			"payload": []map[string]interface{}{
				{
					"attribute_key":   fr.Attribute,
					"filter_operator": "equal_to",
					"values":          []string{fr.Value},
					"query_operator":  nil,
				},
			},
		}
		route := fmt.Sprintf("/api/v1/accounts/%s/contacts/filter", cfg.AccountID)
		respBody, err := s.chatwootRequestJSON(http.MethodPost, cfg, route, filterBody)
		if err != nil {
			continue
		}

		var resp chatwootContactSearchResponse
		if err := json.Unmarshal(respBody, &resp); err != nil {
			continue
		}
		candidates = append(candidates, resp)
	}

	// Last resort: scan the contacts list pages to find an exact match.
	for page := 1; page <= 10; page++ {
		route := fmt.Sprintf("/api/v1/accounts/%s/contacts?page=%d", cfg.AccountID, page)
		respBody, err := s.chatwootRequestJSON(http.MethodGet, cfg, route, nil)
		if err != nil {
			break
		}
		var resp chatwootContactSearchResponse
		if err := json.Unmarshal(respBody, &resp); err != nil {
			break
		}
		if len(resp.Payload) == 0 {
			break
		}
		candidates = append(candidates, resp)
		if len(resp.Payload) < 15 {
			break
		}
	}

	for _, resp := range candidates {
		for _, item := range resp.Payload {
			if item.ID <= 0 {
				continue
			}

			if _, ok := seen[item.ID]; ok {
				continue
			}
			seen[item.ID] = struct{}{}

			itemIdentifier := strings.TrimSpace(item.Identifier)
			itemPhone := normalizePhoneForCompare(item.PhoneNumber)

			matchIdentifier := identifier != "" && strings.EqualFold(itemIdentifier, identifier)
			matchPhone := normalizedPhone != "" && itemPhone != "" &&
				(strings.EqualFold(itemPhone, normalizedPhone) ||
					strings.HasSuffix(itemPhone, normalizedPhone) ||
					strings.HasSuffix(normalizedPhone, itemPhone))

			if matchIdentifier || matchPhone {
				return item.ID, nil
			}

			// If there is an inbox mapping with the same source_id, it is the same external contact.
			for _, ci := range item.ContactInboxes {
				if strings.EqualFold(strings.TrimSpace(ci.SourceID), sourceID) ||
					strings.EqualFold(strings.TrimSpace(ci.SourceID), identifier) {
					return item.ID, nil
				}
			}
		}
	}

	return 0, nil
}

func (s *chatwootService) createChatwootConversation(cfg *chatwoot_model.ChatwootConfig, contactID int, sourceID string) (int, error) {
	body := map[string]interface{}{
		"source_id":  sourceID,
		"inbox_id":   cfg.InboxID,
		"contact_id": contactID,
	}

	if cfg.ConversationPending {
		body["status"] = "pending"
	} else {
		body["status"] = "open"
	}

	respBody, err := s.chatwootRequestJSON(http.MethodPost, cfg, fmt.Sprintf("/api/v1/accounts/%s/conversations", cfg.AccountID), body)
	if err != nil {
		var httpErr *chatwootHTTPError
		if errors.As(err, &httpErr) && httpErr.StatusCode == http.StatusUnprocessableEntity {
			// Conversation may already exist for this contact/source_id.
			existingID, lookupErr := s.findExistingConversationID(cfg, contactID, sourceID)
			if lookupErr == nil && existingID > 0 {
				return existingID, nil
			}
		}
		return 0, err
	}

	var resp chatwootConversationCreateResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return 0, err
	}
	if resp.ID <= 0 {
		return 0, fmt.Errorf("chatwoot conversation id not found in response")
	}
	return resp.ID, nil
}

func (s *chatwootService) findExistingConversationID(cfg *chatwoot_model.ChatwootConfig, contactID int, sourceID string) (int, error) {
	route := fmt.Sprintf("/api/v1/accounts/%s/contacts/%d/conversations", cfg.AccountID, contactID)
	respBody, err := s.chatwootRequestJSON(http.MethodGet, cfg, route, nil)
	if err != nil {
		return 0, err
	}

	var resp chatwootContactConversationsResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return 0, err
	}

	trimmedSourceID := strings.TrimSpace(sourceID)
	for _, item := range resp.Payload {
		if item.ID <= 0 {
			continue
		}
		if cfg.InboxID > 0 && item.InboxID != 0 && item.InboxID != cfg.InboxID {
			continue
		}
		if trimmedSourceID == "" || strings.EqualFold(strings.TrimSpace(item.SourceID), trimmedSourceID) {
			return item.ID, nil
		}
	}

	return 0, nil
}

func (s *chatwootService) createChatwootContactInbox(cfg *chatwoot_model.ChatwootConfig, contactID int, sourceID string) error {
	route := fmt.Sprintf("/api/v1/accounts/%s/contacts/%d/contact_inboxes", cfg.AccountID, contactID)
	body := map[string]interface{}{
		"inbox_id":  cfg.InboxID,
		"source_id": sourceID,
	}

	respBody, err := s.chatwootRequestJSON(http.MethodPost, cfg, route, body)
	if err != nil {
		var httpErr *chatwootHTTPError
		if errors.As(err, &httpErr) && httpErr.StatusCode == http.StatusUnprocessableEntity {
			return nil
		}
		return err
	}

	var resp chatwootContactInboxCreateResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		// Response shape can vary by Chatwoot version; ignore parse errors here.
		return nil
	}

	if strings.TrimSpace(resp.SourceID) == "" {
		return nil
	}

	return nil
}

func (s *chatwootService) sendMessageToChatwoot(
	cfg *chatwoot_model.ChatwootConfig,
	binding *chatwoot_model.ChatwootBinding,
	message evolutionMessage,
	messageType string,
) error {
	route := fmt.Sprintf("/api/v1/accounts/%s/conversations/%d/messages", cfg.AccountID, binding.ConversationID)

	if len(message.Media.Data) == 0 {
		content := message.Content
		body := map[string]interface{}{
			"message_type": messageType,
			"private":      false,
		}
		if message.QuotedStanzaID != "" {
			if replyToID := s.findQuotedChatwootMessageID(cfg, binding, message.QuotedStanzaID); replyToID > 0 {
				body["content_attributes"] = map[string]interface{}{"in_reply_to": replyToID}
				s.logQuoteResolution(binding.InstanceID, message, fmt.Sprintf("native in_reply_to=%d", replyToID))
			} else {
				content = formatQuotedReply(message.QuotedSummary, content)
				s.logQuoteResolution(binding.InstanceID, message, "text fallback")
			}
		}
		body["content"] = content
		if strings.TrimSpace(message.MessageSourceID) != "" {
			body["source_id"] = message.MessageSourceID
		}
		respBody, err := s.chatwootRequestJSON(http.MethodPost, cfg, route, body)
		if err == nil {
			s.rememberMirroredChatwootMessage(binding.InstanceID, message.MessageSourceID, respBody)
		}
		return err
	}

	content := message.Content
	if message.QuotedStanzaID != "" && message.QuotedSummary != "" {
		content = formatQuotedReply(message.QuotedSummary, content)
	}
	respBody, err := s.chatwootRequestMultipart(cfg, route, content, messageType, message.MessageSourceID, message.Media)
	if err == nil {
		s.rememberMirroredChatwootMessage(binding.InstanceID, message.MessageSourceID, respBody)
	}
	return err
}

// findQuotedChatwootMessageID resolves a WhatsApp stanza ID to the Chatwoot
// message ID that mirrors it, so replies can use the native in_reply_to
// attribute. Any failure returns zero and the caller falls back to plain text.
func (s *chatwootService) findQuotedChatwootMessageID(
	cfg *chatwoot_model.ChatwootConfig,
	binding *chatwoot_model.ChatwootBinding,
	stanzaID string,
) int {
	stanzaID = strings.TrimSpace(stanzaID)
	if stanzaID == "" || cfg == nil || binding == nil || binding.ConversationID <= 0 {
		return 0
	}

	cacheKey := fmt.Sprintf("%s:%d:%s", binding.InstanceID, binding.ConversationID, stanzaID)
	if s.quoteCache != nil {
		if cached, found := s.quoteCache.Get(cacheKey); found {
			if messageID, ok := cached.(int); ok {
				return messageID
			}
		}
		// Outgoing messages created in the Chatwoot UI carry no source_id, but
		// the connector recorded their WhatsApp ID at send time.
		outboundKey := fmt.Sprintf("%s|outbound|%s", binding.InstanceID, stanzaID)
		if cached, found := s.quoteCache.Get(outboundKey); found {
			if messageID, ok := cached.(int); ok {
				s.quoteCache.Set(cacheKey, messageID, 24*time.Hour)
				return messageID
			}
		}
	}

	route := fmt.Sprintf("/api/v1/accounts/%s/conversations/%d/messages", cfg.AccountID, binding.ConversationID)
	respBody, err := s.chatwootRequestJSON(http.MethodGet, cfg, route, nil)
	if err != nil {
		if s.loggerWrapper != nil {
			s.loggerWrapper.GetLogger(binding.InstanceID).LogWarn(
				"[%s] Failed to list Chatwoot messages for quoted lookup in conversation %d: %v",
				binding.InstanceID,
				binding.ConversationID,
				err,
			)
		}
		return 0
	}

	var resp chatwootConversationMessagesResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		if s.loggerWrapper != nil {
			s.loggerWrapper.GetLogger(binding.InstanceID).LogWarn(
				"[%s] Failed to parse Chatwoot messages for quoted lookup in conversation %d: %v",
				binding.InstanceID,
				binding.ConversationID,
				err,
			)
		}
		return 0
	}

	wantIn := "wa-in:" + stanzaID
	wantOut := "wa-out:" + stanzaID
	for _, message := range resp.Payload {
		if message.ID <= 0 {
			continue
		}
		if message.SourceID == wantIn || message.SourceID == wantOut {
			if s.quoteCache != nil {
				s.quoteCache.Set(cacheKey, message.ID, 24*time.Hour)
			}
			return message.ID
		}
	}
	if s.loggerWrapper != nil {
		s.loggerWrapper.GetLogger(binding.InstanceID).LogInfo(
			"[%s] Quoted stanza %s not mirrored in conversation %d (%d messages scanned)",
			binding.InstanceID,
			stanzaID,
			binding.ConversationID,
			len(resp.Payload),
		)
	}
	return 0
}

// rememberOutboundMessageMapping stores the WhatsApp message ID assigned to an
// outgoing Chatwoot message, so quoted replies to it can later be resolved to
// the native Chatwoot message ID via in_reply_to. In-memory only; after a
// restart the lookup simply falls back to plain-text quoting.
func (s *chatwootService) rememberOutboundMessageMapping(instanceID string, whatsappMessageID string, chatwootMessageID string) {
	if s.quoteCache == nil {
		return
	}
	id, err := strconv.Atoi(strings.TrimSpace(chatwootMessageID))
	if err != nil || id <= 0 {
		return
	}
	s.quoteCache.Set(fmt.Sprintf("%s|outbound|%s", instanceID, whatsappMessageID), id, 24*time.Hour)
}

func (s *chatwootService) sendTextFromChatwoot(
	instance *evolution.Instance,
	remoteJID string,
	content string,
	chatwootMessageID string,
) error {
	text := strings.TrimSpace(content)
	if text == "" {
		text = content
	}

	number, formatJID := recipientForSend(remoteJID)
	messageID := chatwootOutboundMessageID(instance.Id, chatwootMessageID, "text")
	s.skipCache.Set(fmt.Sprintf("%s:%s", instance.Id, messageID), true, 10*time.Minute)

	err := s.evolutionClient.SendText(context.Background(), instance, evolution.TextRequest{
		Number:    number,
		Text:      text,
		ID:        messageID,
		FormatJID: formatJID,
	})
	if err != nil {
		s.skipCache.Delete(fmt.Sprintf("%s:%s", instance.Id, messageID))
		return err
	}

	s.rememberOutboundMessageMapping(instance.Id, messageID, chatwootMessageID)
	s.loggerWrapper.GetLogger(instance.Id).LogInfo("[%s] Chatwoot message %s sent to WhatsApp through Evolution send service as text", instance.Id, chatwootMessageID)
	return nil
}

func (s *chatwootService) sendMediaFromChatwoot(
	instance *evolution.Instance,
	remoteJID string,
	dataURL string,
	fileType string,
	fileName string,
	caption string,
	chatwootMessageID string,
) error {
	fileData, mimeType, err := s.downloadAttachmentData(dataURL)
	if err != nil {
		return err
	}

	mimeType = strings.ToLower(mimeType)
	mediaKind := normalizeChatwootFileType(fileType, mimeType)
	if strings.TrimSpace(fileName) == "" {
		fileName = fmt.Sprintf("chatwoot-%d", time.Now().Unix())
	}

	number, formatJID := recipientForSend(remoteJID)
	messageID := chatwootOutboundMessageID(instance.Id, chatwootMessageID, "media:"+dataURL)
	s.skipCache.Set(fmt.Sprintf("%s:%s", instance.Id, messageID), true, 10*time.Minute)

	err = s.evolutionClient.SendMedia(context.Background(), instance, evolution.MediaRequest{
		Number:    number,
		Type:      mediaKind,
		Caption:   caption,
		Filename:  fileName,
		ID:        messageID,
		FormatJID: formatJID,
		Data:      fileData,
	})
	if err != nil {
		s.skipCache.Delete(fmt.Sprintf("%s:%s", instance.Id, messageID))
		return err
	}

	s.rememberOutboundMessageMapping(instance.Id, messageID, chatwootMessageID)
	s.loggerWrapper.GetLogger(instance.Id).LogInfo("[%s] Chatwoot message %s sent to WhatsApp through Evolution send service as media (%s)", instance.Id, chatwootMessageID, mediaKind)
	return nil
}

func (s *chatwootService) downloadAttachmentData(raw string) ([]byte, string, error) {
	dataURL := strings.TrimSpace(raw)
	if dataURL == "" {
		return nil, "", errors.New("empty attachment URL")
	}

	if strings.HasPrefix(strings.ToLower(dataURL), "data:") {
		return decodeDataURL(dataURL)
	}

	req, err := http.NewRequest(http.MethodGet, dataURL, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("User-Agent", "evolution-go-chatwoot/1.0")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, "", fmt.Errorf("failed to download attachment: status %d", resp.StatusCode)
	}

	fileData, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", err
	}

	return fileData, resp.Header.Get("Content-Type"), nil
}

func decodeDataURL(dataURL string) ([]byte, string, error) {
	const prefix = "data:"
	if !strings.HasPrefix(strings.ToLower(dataURL), prefix) {
		return nil, "", errors.New("invalid data URL")
	}

	comma := strings.Index(dataURL, ",")
	if comma < 0 {
		return nil, "", errors.New("invalid data URL payload")
	}

	meta := dataURL[len(prefix):comma]
	rawPayload := dataURL[comma+1:]

	mimeType := "application/octet-stream"
	isBase64 := false
	if meta != "" {
		parts := strings.Split(meta, ";")
		if strings.TrimSpace(parts[0]) != "" {
			mimeType = strings.TrimSpace(parts[0])
		}
		for _, flag := range parts[1:] {
			if strings.EqualFold(strings.TrimSpace(flag), "base64") {
				isBase64 = true
				break
			}
		}
	}

	if isBase64 {
		payload := strings.TrimSpace(rawPayload)
		data, err := base64.StdEncoding.DecodeString(payload)
		if err != nil {
			return nil, "", err
		}
		return data, mimeType, nil
	}

	decoded, err := url.PathUnescape(rawPayload)
	if err != nil {
		return nil, "", err
	}
	return []byte(decoded), mimeType, nil
}

func (s *chatwootService) chatwootRequestJSON(
	method string,
	cfg *chatwoot_model.ChatwootConfig,
	route string,
	body interface{},
) ([]byte, error) {
	var payload []byte
	var err error
	if body != nil {
		payload, err = json.Marshal(body)
		if err != nil {
			return nil, err
		}
	}

	fullURL, err := joinChatwootURL(cfg.URL, route)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest(method, fullURL, bytes.NewBuffer(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("api_access_token", cfg.Token)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &chatwootHTTPError{
			StatusCode: resp.StatusCode,
			Body:       string(respBody),
		}
	}
	return respBody, nil
}

func (s *chatwootService) chatwootRequestMultipart(
	cfg *chatwoot_model.ChatwootConfig,
	route string,
	content string,
	messageType string,
	messageSourceID string,
	attachment mediaAttachment,
) ([]byte, error) {
	fullURL, err := joinChatwootURL(cfg.URL, route)
	if err != nil {
		return nil, err
	}

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	attachment = normalizeMediaAttachment(attachment)

	if strings.TrimSpace(content) != "" {
		_ = writer.WriteField("content", content)
	}
	_ = writer.WriteField("message_type", messageType)
	_ = writer.WriteField("private", "false")
	_ = writer.WriteField("file_type", attachment.FileType)
	if strings.TrimSpace(messageSourceID) != "" {
		_ = writer.WriteField("source_id", messageSourceID)
	}

	partHeader := make(textproto.MIMEHeader)
	partHeader.Set("Content-Disposition", mime.FormatMediaType("form-data", map[string]string{
		"name":     "attachments[]",
		"filename": attachment.FileName,
	}))
	partHeader.Set("Content-Type", attachment.MIMEType)
	part, err := writer.CreatePart(partHeader)
	if err != nil {
		return nil, err
	}
	if _, err := part.Write(attachment.Data); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}

	req, err := http.NewRequest(http.MethodPost, fullURL, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("api_access_token", cfg.Token)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if resp.StatusCode == http.StatusNotFound {
			return nil, &chatwootHTTPError{
				StatusCode: resp.StatusCode,
				Body:       string(respBody),
			}
		}

		// Fallback when attachment upload fails: keep text sync and preserve media metadata.
		fallbackContent := fmt.Sprintf("%s\n[attachment:%s upload_failed]", content, attachment.FileType)
		fallbackBody := map[string]interface{}{
			"content":      fallbackContent,
			"message_type": messageType,
			"private":      false,
		}
		if strings.TrimSpace(messageSourceID) != "" {
			fallbackBody["source_id"] = messageSourceID
		}
		fallbackRespBody, fallbackErr := s.chatwootRequestJSON(http.MethodPost, cfg, route, fallbackBody)
		if fallbackErr != nil {
			return nil, fmt.Errorf("chatwoot multipart failed [%d]: %s", resp.StatusCode, string(respBody))
		}
		return fallbackRespBody, nil
	}

	return respBody, nil
}

func (s *chatwootService) extractEvolutionMessage(payload evolutionWebhookPayload, rawPayload []byte) (evolutionMessage, bool) {
	data := payload.Data
	if data == nil {
		return evolutionMessage{}, false
	}

	info := mapFromAny(mapLookup(data, "Info", "info"))
	messageMap := mapFromAny(mapLookup(data, "Message", "message"))
	if isWhatsAppProtocolMessage(messageMap) {
		return evolutionMessage{}, false
	}

	remoteJID := s.resolveEvolutionRemoteJID(payload, data, info)
	if remoteJID == "" {
		remoteJID = normalizeRemoteJID(mapString(data, "remoteJid", "remoteJID", "jid"))
	}
	if remoteJID == "" {
		return evolutionMessage{}, false
	}

	fromMe := mapBool(info, "IsFromMe", "isFromMe", "fromMe")
	if payload.Event == "SendMessage" {
		fromMe = true
	}

	messageID := strings.TrimSpace(mapString(info, "ID", "id", "MessageID", "messageId"))
	if messageID == "" {
		messageID = "event-" + stablePayloadHash(rawPayload, 16)
	}

	content, media := s.extractContentAndMedia(messageMap)
	quotedStanzaID, quotedSummary := extractQuotedContext(data)
	sourcePrefix := "wa-in:"
	if fromMe {
		sourcePrefix = "wa-out:"
	}

	contactName := strings.TrimSpace(firstNonEmptyString(
		mapString(data, "PushName", "pushName"),
		mapString(info, "PushName", "pushName"),
		contactNameFallback(remoteJID),
	))

	return evolutionMessage{
		RemoteJID:       remoteJID,
		ContactName:     contactName,
		Content:         content,
		Media:           media,
		MessageID:       messageID,
		MessageSourceID: sourcePrefix + messageID,
		FromMe:          fromMe,
		Unavailable:     mapBool(data, "IsUnavailable", "isUnavailable") || mapBool(info, "IsUnavailable", "isUnavailable"),
		IdentityAliases: evolutionIdentityAliases(data, info, fromMe),
		QuotedStanzaID:  quotedStanzaID,
		QuotedSummary:   quotedSummary,
	}, true
}

func evolutionIdentityAliases(data map[string]interface{}, info map[string]interface{}, fromMe bool) []string {
	candidates := []string{
		mapString(info, "Chat", "chat", "RemoteJID", "remoteJid"),
		mapString(data, "remoteJid", "remoteJID", "jid"),
	}
	if fromMe {
		candidates = append(
			candidates,
			mapString(info, "Recipient", "recipient"),
			mapString(info, "RecipientAlt", "recipientAlt"),
			mapString(data, "Recipient", "recipient"),
			mapString(data, "RecipientAlt", "recipientAlt"),
		)
	} else {
		candidates = append(
			candidates,
			mapString(info, "Sender", "sender"),
			mapString(info, "SenderAlt", "senderAlt"),
			mapString(data, "Sender", "sender"),
			mapString(data, "SenderAlt", "senderAlt"),
		)
	}

	aliases := make([]string, 0, len(candidates))
	seen := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		candidate = normalizeRemoteJID(candidate)
		if !isLIDRemoteJID(candidate) {
			continue
		}
		key := strings.ToLower(candidate)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		aliases = append(aliases, candidate)
	}
	return aliases
}

func (s *chatwootService) extractContentAndMedia(messageMap map[string]interface{}) (string, mediaAttachment) {
	if len(messageMap) == 0 {
		return "[message]", mediaAttachment{}
	}

	if conversation := strings.TrimSpace(mapString(messageMap, "conversation", "Conversation")); conversation != "" {
		return conversation, mediaAttachment{}
	}
	if ext := mapFromAny(mapLookup(messageMap, "extendedTextMessage", "ExtendedTextMessage")); ext != nil {
		return mapString(ext, "text", "Text"), mediaAttachment{}
	}
	if child := mapFromAny(mapLookup(messageMap, "documentWithCaptionMessage", "DocumentWithCaptionMessage")); child != nil {
		if nested := mapFromAny(mapLookup(child, "message", "Message")); nested != nil {
			return s.extractContentAndMedia(nested)
		}
	}

	if img := mapFromAny(mapLookup(messageMap, "imageMessage", "ImageMessage")); img != nil {
		return mapString(img, "caption", "Caption"), s.extractWebhookMedia(messageMap, img, "image")
	}
	if video := mapFromAny(mapLookup(messageMap, "videoMessage", "VideoMessage")); video != nil {
		return mapString(video, "caption", "Caption"), s.extractWebhookMedia(messageMap, video, "video")
	}
	if audio := mapFromAny(mapLookup(messageMap, "audioMessage", "AudioMessage")); audio != nil {
		return "", s.extractWebhookMedia(messageMap, audio, "audio")
	}
	if doc := mapFromAny(mapLookup(messageMap, "documentMessage", "DocumentMessage")); doc != nil {
		media := s.extractWebhookMedia(messageMap, doc, "document")
		content := firstNonEmptyString(
			mapString(doc, "caption", "Caption"),
			mapString(doc, "title", "Title"),
			mapString(doc, "fileName", "FileName", "filename"),
		)
		return content, media
	}
	if sticker := mapFromAny(mapLookup(messageMap, "stickerMessage", "StickerMessage")); sticker != nil {
		return "", s.extractWebhookMedia(messageMap, sticker, "image")
	}

	media := s.extractWebhookMedia(messageMap, nil, "")
	if len(media.Data) > 0 || media.FileType != "" {
		return "", media
	}

	return "[message]", mediaAttachment{}
}

func extractQuotedContext(data map[string]interface{}) (string, string) {
	if !mapBool(data, "isQuoted", "IsQuoted") {
		return "", ""
	}
	quoted := mapFromAny(mapLookup(data, "quoted", "Quoted"))
	if quoted == nil {
		return "", ""
	}
	stanzaID := strings.TrimSpace(mapString(quoted, "stanzaID", "stanzaId", "StanzaID"))
	summary := summarizeQuotedMessage(mapFromAny(mapLookup(quoted, "quotedMessage", "QuotedMessage")))
	return stanzaID, summary
}

func summarizeQuotedMessage(messageMap map[string]interface{}) string {
	if len(messageMap) == 0 {
		return ""
	}

	if conversation := strings.TrimSpace(mapString(messageMap, "conversation", "Conversation")); conversation != "" {
		return conversation
	}
	if ext := mapFromAny(mapLookup(messageMap, "extendedTextMessage", "ExtendedTextMessage")); ext != nil {
		if text := strings.TrimSpace(mapString(ext, "text", "Text")); text != "" {
			return text
		}
	}
	if child := mapFromAny(mapLookup(messageMap, "documentWithCaptionMessage", "DocumentWithCaptionMessage")); child != nil {
		if nested := mapFromAny(mapLookup(child, "message", "Message")); nested != nil {
			return summarizeQuotedMessage(nested)
		}
	}

	if img := mapFromAny(mapLookup(messageMap, "imageMessage", "ImageMessage")); img != nil {
		return quotedMediaSummary(img, "[imagem]")
	}
	if video := mapFromAny(mapLookup(messageMap, "videoMessage", "VideoMessage")); video != nil {
		return quotedMediaSummary(video, "[vídeo]")
	}
	if audio := mapFromAny(mapLookup(messageMap, "audioMessage", "AudioMessage")); audio != nil {
		return quotedMediaSummary(audio, "[áudio]")
	}
	if doc := mapFromAny(mapLookup(messageMap, "documentMessage", "DocumentMessage")); doc != nil {
		return quotedMediaSummary(doc, "[documento]")
	}
	if sticker := mapFromAny(mapLookup(messageMap, "stickerMessage", "StickerMessage")); sticker != nil {
		return quotedMediaSummary(sticker, "[figurinha]")
	}

	return ""
}

func quotedMediaSummary(mediaMap map[string]interface{}, label string) string {
	if caption := strings.TrimSpace(mapString(mediaMap, "caption", "Caption")); caption != "" {
		return caption
	}
	return label
}

func formatQuotedReply(summary string, content string) string {
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return content
	}
	// Plain-text fallback: avoid Markdown blockquote ("> ") here — some
	// Chatwoot frontends render it as an unreadable box inside the bubble.
	quoted := "Mensagem citada: " + strings.ReplaceAll(summary, "\n", " ")
	if strings.TrimSpace(content) == "" || content == "[message]" {
		return quoted
	}
	return quoted + "\n" + content
}

// logQuoteResolution records how a quoted reply was delivered to Chatwoot, to
// aid diagnosing quote issues in production.
func (s *chatwootService) logQuoteResolution(instanceID string, message evolutionMessage, resolution string) {
	if s.loggerWrapper == nil {
		return
	}
	s.loggerWrapper.GetLogger(instanceID).LogInfo(
		"[%s] Quoted reply %s (stanza=%s, summary=%d chars, content=%d chars): %s",
		instanceID,
		message.MessageID,
		message.QuotedStanzaID,
		len(message.QuotedSummary),
		len(message.Content),
		resolution,
	)
}

func isWhatsAppProtocolMessage(messageMap map[string]interface{}) bool {
	return mapLookup(messageMap, "protocolMessage", "ProtocolMessage") != nil
}

func isEvolutionMessageSourceID(sourceID string) bool {
	sourceID = strings.ToLower(strings.TrimSpace(sourceID))
	return strings.HasPrefix(sourceID, "wa-in:") || strings.HasPrefix(sourceID, "wa-out:")
}

func (s *chatwootService) rememberMirroredChatwootMessage(instanceID string, sourceID string, responseBody []byte) {
	if s.webhookCache == nil || !isEvolutionMessageSourceID(sourceID) || len(responseBody) == 0 {
		return
	}

	var response chatwootMessageCreateResponse
	if err := json.Unmarshal(responseBody, &response); err != nil {
		return
	}
	messageID := webhookMessageID(response.ID)
	if messageID == "unknown" {
		return
	}
	s.webhookCache.Set(fmt.Sprintf("%s:%s", instanceID, messageID), true, 24*time.Hour)
}

func (s *chatwootService) resolveEvolutionRemoteJID(payload evolutionWebhookPayload, data map[string]interface{}, info map[string]interface{}) string {
	remoteJID := normalizeRemoteJID(mapString(info, "Chat", "chat", "RemoteJID", "remoteJid"))
	if remoteJID == "" {
		remoteJID = normalizeRemoteJID(mapString(data, "remoteJid", "remoteJID", "jid"))
	}
	if !isLIDRemoteJID(remoteJID) {
		return remoteJID
	}

	fromMe := payload.Event == "SendMessage" || mapBool(info, "IsFromMe", "isFromMe", "fromMe")
	phoneCandidates := []string{}
	if fromMe {
		phoneCandidates = append(
			phoneCandidates,
			mapString(info, "RecipientAlt", "recipientAlt"),
			mapString(data, "RecipientAlt", "recipientAlt"),
			mapString(info, "Recipient", "recipient"),
			mapString(data, "Recipient", "recipient"),
		)
	} else {
		phoneCandidates = append(
			phoneCandidates,
			mapString(info, "SenderAlt", "senderAlt"),
			mapString(data, "SenderAlt", "senderAlt"),
			mapString(info, "Sender", "sender"),
			mapString(data, "Sender", "sender"),
		)
	}

	if pnJID := firstPhoneRemoteJID(phoneCandidates...); pnJID != "" {
		return pnJID
	}

	if s.lidResolver != nil && payload.InstanceID != "" {
		if resolved, ok := s.lidResolver(payload.InstanceID, remoteJID); ok {
			if pnJID := firstPhoneRemoteJID(resolved); pnJID != "" {
				return pnJID
			}
		}
	}

	return remoteJID
}

func (s *chatwootService) extractWebhookMedia(root map[string]interface{}, mediaMap map[string]interface{}, defaultType string) mediaAttachment {
	mimeType := firstNonEmptyString(
		mapString(root, "mimetype", "mimeType", "MimeType"),
		mapString(mediaMap, "mimetype", "mimeType", "MimeType"),
	)
	mediaType := defaultType
	if mediaType == "" && mimeType != "" {
		mediaType = normalizeChatwootFileType("", mimeType)
	}
	fileName := firstNonEmptyString(
		mapString(mediaMap, "fileName", "FileName", "filename"),
		mapString(root, "fileName", "FileName", "filename"),
	)
	attachment := mediaAttachment{FileType: mediaType, MIMEType: mimeType, FileName: fileName}

	encoded := firstNonEmptyString(
		mapString(root, "base64", "Base64"),
		mapString(mediaMap, "base64", "Base64"),
	)
	if encoded != "" {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(encoded)), "data:") {
			data, detectedMime, err := decodeDataURL(encoded)
			if err == nil {
				attachment.Data = data
				if attachment.MIMEType == "" {
					attachment.MIMEType = detectedMime
				}
				if attachment.FileType == "" {
					attachment.FileType = normalizeChatwootFileType("", attachment.MIMEType)
				}
				return attachment
			}
			return attachment
		}

		data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
		if err == nil {
			attachment.Data = data
			if attachment.FileType == "" {
				attachment.FileType = "document"
			}
			return attachment
		}
		return attachment
	}

	mediaURL := firstNonEmptyString(
		mapString(root, "mediaUrl", "mediaURL"),
		mapString(mediaMap, "mediaUrl", "mediaURL"),
	)
	if mediaURL == "" {
		return attachment
	}

	data, detectedMime, err := s.downloadAttachmentData(mediaURL)
	if err != nil {
		return attachment
	}
	attachment.Data = data
	if attachment.MIMEType == "" {
		attachment.MIMEType = detectedMime
	}
	if attachment.FileType == "" {
		attachment.FileType = normalizeChatwootFileType("", attachment.MIMEType)
	}
	return attachment
}

func shouldIgnoreJID(ignoreJIDsJSON string, remoteJID string) bool {
	if strings.TrimSpace(ignoreJIDsJSON) == "" {
		return false
	}
	var ignore []string
	if err := json.Unmarshal([]byte(ignoreJIDsJSON), &ignore); err != nil {
		return false
	}
	for _, j := range ignore {
		if strings.EqualFold(strings.TrimSpace(j), remoteJID) {
			return true
		}
	}
	return false
}

func normalizeRemoteJID(jid string) string {
	jid = strings.TrimSpace(jid)
	if jid == "" {
		return ""
	}
	if strings.Contains(jid, ":") {
		parts := strings.SplitN(jid, "@", 2)
		left := strings.SplitN(parts[0], ":", 2)[0]
		if len(parts) == 2 {
			return left + "@" + parts[1]
		}
		return left
	}
	return jid
}

func toE164(jid string, mergeBrazilContacts bool) string {
	base := jid
	if idx := strings.Index(base, "@"); idx > 0 {
		base = base[:idx]
	}
	base = strings.TrimSpace(base)
	base = strings.TrimPrefix(base, "+")

	if mergeBrazilContacts && shouldRemoveBrazilNinthDigit(base) {
		if base[4] == '9' {
			base = base[:4] + base[5:]
		}
	}

	if base == "" {
		return ""
	}
	return "+" + base
}

func shouldRemoveBrazilNinthDigit(base string) bool {
	if len(base) != 13 || !strings.HasPrefix(base, "55") || base[4] != '9' {
		return false
	}
	ddd, err := strconv.Atoi(base[2:4])
	if err != nil {
		return false
	}
	return ddd >= 31
}

func contactNameFallback(remoteJID string) string {
	if isLIDRemoteJID(remoteJID) {
		return normalizeRemoteJID(remoteJID)
	}
	return toE164(remoteJID, true)
}

func buildChatwootContactRef(instanceID string, remoteJID string, contactName string, mergeBrazilContacts bool) chatwootContactRef {
	remoteJID = normalizeRemoteJID(remoteJID)
	sourceID := chatwootSourceID(instanceID, remoteJID)
	phone := toE164(remoteJID, mergeBrazilContacts)
	identifier := remoteJID

	if isLIDRemoteJID(remoteJID) {
		// LID is not a real phone number. Use an instance-scoped identifier to
		// prevent Chatwoot from merging it into an unrelated phone contact.
		phone = ""
		identifier = sourceID
	}

	name := strings.TrimSpace(contactName)
	if name == "" {
		name = firstNonEmptyString(phone, remoteJID, sourceID)
	}

	return chatwootContactRef{
		SourceID:   sourceID,
		Identifier: identifier,
		Phone:      phone,
		Name:       name,
	}
}

func chatwootSourceID(instanceID string, remoteJID string) string {
	remoteJID = normalizeRemoteJID(remoteJID)
	if isLIDRemoteJID(remoteJID) {
		return strings.TrimSpace(instanceID) + ":lid:" + remoteJID
	}
	return strings.TrimSpace(instanceID) + ":" + remoteJID
}

func remoteJIDFromSourceID(instanceID string, sourceID string) string {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return ""
	}

	prefix := strings.TrimSpace(instanceID) + ":"
	if strings.HasPrefix(sourceID, prefix) {
		remote := strings.TrimPrefix(sourceID, prefix)
		if strings.HasPrefix(remote, "lid:") {
			remote = strings.TrimPrefix(remote, "lid:")
		}
		return normalizeRemoteJID(remote)
	}
	return normalizeRemoteJID(sourceID)
}

func isLIDRemoteJID(remoteJID string) bool {
	return strings.HasSuffix(strings.ToLower(strings.TrimSpace(remoteJID)), "@lid")
}

func isPhoneRemoteJID(remoteJID string) bool {
	return strings.HasSuffix(strings.ToLower(strings.TrimSpace(remoteJID)), "@s.whatsapp.net")
}

func firstPhoneRemoteJID(values ...string) string {
	for _, value := range values {
		jid := normalizeRemoteJID(value)
		if isPhoneRemoteJID(jid) {
			return jid
		}
	}
	return ""
}

func recipientForSend(remoteJID string) (string, *bool) {
	remoteJID = normalizeRemoteJID(remoteJID)
	formatFalse := false

	if remoteJID == "" {
		return "", nil
	}
	if isLIDRemoteJID(remoteJID) ||
		strings.Contains(remoteJID, "@g.us") ||
		strings.Contains(remoteJID, "@broadcast") ||
		strings.Contains(remoteJID, "@newsletter") {
		return remoteJID, &formatFalse
	}
	if idx := strings.Index(remoteJID, "@"); idx > 0 {
		return strings.TrimPrefix(remoteJID[:idx], "+"), nil
	}
	return strings.TrimPrefix(remoteJID, "+"), nil
}

func chatwootOutboundMessageID(instanceID string, chatwootMessageID string, part string) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		"chatwoot",
		instanceID,
		chatwootMessageID,
		part,
	}, ":")))
	hexID := strings.ToUpper(hex.EncodeToString(sum[:]))
	return "3EB" + hexID[:29]
}

func stablePayloadHash(payload []byte, size int) string {
	sum := sha256.Sum256(payload)
	value := hex.EncodeToString(sum[:])
	if size > 0 && len(value) > size {
		return value[:size]
	}
	return value
}

func applyChatwootSignature(cfg *chatwoot_model.ChatwootConfig, content string, senderName string) string {
	if cfg == nil || !cfg.SignMsg {
		return content
	}
	senderName = strings.TrimSpace(senderName)
	if senderName == "" || strings.TrimSpace(content) == "" {
		return content
	}
	delimiter := normalizeEscapedDelimiter(cfg.SignDelimiter)
	if delimiter == "" {
		delimiter = "\n"
	}
	return senderName + delimiter + content
}

func normalizeEscapedDelimiter(value string) string {
	if value == "\\n" {
		return "\n"
	}
	return value
}

func parseChatwootContactID(respBody []byte) (int, error) {
	var payload interface{}
	if err := json.Unmarshal(respBody, &payload); err != nil {
		return 0, err
	}

	contactID := findChatwootContactID(payload, 0)
	if contactID > 0 {
		return contactID, nil
	}

	return 0, fmt.Errorf("chatwoot contact id not found in response")
}

func findChatwootContactID(value interface{}, depth int) int {
	if depth > 12 {
		return 0
	}

	switch v := value.(type) {
	case map[string]interface{}:
		if id := intFromAny(mapLookup(v, "id")); id > 0 {
			return id
		}

		for _, key := range []string{"contact", "payload", "data", "result"} {
			child := mapLookup(v, key)
			if child == nil {
				continue
			}
			if id := findChatwootContactID(child, depth+1); id > 0 {
				return id
			}
		}

		for _, child := range v {
			if id := findChatwootContactID(child, depth+1); id > 0 {
				return id
			}
		}

	case []interface{}:
		for _, item := range v {
			if id := findChatwootContactID(item, depth+1); id > 0 {
				return id
			}
		}
	}

	return 0
}

func intFromAny(value interface{}) int {
	switch v := value.(type) {
	case nil:
		return 0
	case int:
		if v > 0 {
			return v
		}
	case int8:
		if v > 0 {
			return int(v)
		}
	case int16:
		if v > 0 {
			return int(v)
		}
	case int32:
		if v > 0 {
			return int(v)
		}
	case int64:
		if v > 0 {
			return int(v)
		}
	case uint:
		return int(v)
	case uint8:
		return int(v)
	case uint16:
		return int(v)
	case uint32:
		return int(v)
	case uint64:
		return int(v)
	case float32:
		if v > 0 {
			return int(v)
		}
	case float64:
		if v > 0 {
			return int(v)
		}
	case json.Number:
		if i, err := v.Int64(); err == nil && i > 0 {
			return int(i)
		}
		if f, err := v.Float64(); err == nil && f > 0 {
			return int(f)
		}
	case string:
		value := strings.TrimSpace(v)
		if value == "" {
			return 0
		}
		if i, err := strconv.Atoi(value); err == nil && i > 0 {
			return i
		}
	}
	return 0
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func mapFromAny(value interface{}) map[string]interface{} {
	if value == nil {
		return nil
	}
	if m, ok := value.(map[string]interface{}); ok {
		return m
	}
	return nil
}

func mapLookup(m map[string]interface{}, keys ...string) interface{} {
	if m == nil {
		return nil
	}
	for _, key := range keys {
		if value, ok := m[key]; ok {
			return value
		}
		for existingKey, value := range m {
			if strings.EqualFold(existingKey, key) {
				return value
			}
		}
	}
	return nil
}

func mapString(m map[string]interface{}, keys ...string) string {
	value := mapLookup(m, keys...)
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case float64:
		if v == float64(int64(v)) {
			return strconv.FormatInt(int64(v), 10)
		}
		return fmt.Sprintf("%v", v)
	case bool:
		return strconv.FormatBool(v)
	default:
		return fmt.Sprintf("%v", v)
	}
}

func mapBool(m map[string]interface{}, keys ...string) bool {
	value := mapLookup(m, keys...)
	switch v := value.(type) {
	case bool:
		return v
	case string:
		parsed, _ := strconv.ParseBool(strings.TrimSpace(v))
		return parsed
	case float64:
		return v != 0
	default:
		return false
	}
}

func mapConfigToView(cfg *chatwoot_model.ChatwootConfig) *chatwoot_model.ChatwootConfigView {
	view := &chatwoot_model.ChatwootConfigView{
		Enabled:                 cfg.Enabled,
		AccountID:               cfg.AccountID,
		Token:                   cfg.Token,
		URL:                     cfg.URL,
		InboxID:                 cfg.InboxID,
		InboxIdentifier:         cfg.InboxIdentifier,
		SignMsg:                 cfg.SignMsg,
		ReopenConversation:      cfg.ReopenConversation,
		ConversationPending:     cfg.ConversationPending,
		MergeBrazilContacts:     cfg.MergeBrazilContacts,
		ImportContacts:          cfg.ImportContacts,
		ImportMessages:          cfg.ImportMessages,
		DaysLimitImportMessages: cfg.DaysLimitImportMessages,
		NameInbox:               cfg.NameInbox,
		SignDelimiter:           cfg.SignDelimiter,
		Organization:            cfg.Organization,
		Logo:                    cfg.Logo,
		WebhookSecret:           cfg.WebhookSecret,
	}

	if strings.TrimSpace(view.SignDelimiter) == "" {
		view.SignDelimiter = "\n"
	}
	if view.DaysLimitImportMessages <= 0 {
		view.DaysLimitImportMessages = 3
	}

	if strings.TrimSpace(cfg.IgnoreJids) == "" {
		view.IgnoreJids = []string{}
		return view
	}

	var ignore []string
	if err := json.Unmarshal([]byte(cfg.IgnoreJids), &ignore); err != nil {
		view.IgnoreJids = []string{}
		return view
	}
	view.IgnoreJids = ignore
	return view
}

func joinChatwootURL(base string, route string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(base))
	if err != nil {
		return "", err
	}
	if u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("invalid chatwoot url")
	}

	routeURL, err := url.Parse(strings.TrimSpace(route))
	if err != nil {
		return "", err
	}

	u.Path = path.Join(u.Path, routeURL.Path)
	baseQuery := u.Query()
	for key, values := range routeURL.Query() {
		baseQuery.Del(key)
		for _, v := range values {
			baseQuery.Add(key, v)
		}
	}
	u.RawQuery = baseQuery.Encode()

	return u.String(), nil
}

func normalizePhoneForCompare(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range raw {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func uniqueNonEmptyStrings(values []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		result = append(result, v)
	}
	return result
}

func normalizeChatwootFileType(fileType string, mimeType string) string {
	ft := strings.ToLower(strings.TrimSpace(fileType))
	switch ft {
	case "image", "video", "audio", "document":
		return ft
	}

	m := strings.ToLower(mimeType)
	switch {
	case strings.HasPrefix(m, "image/"):
		return "image"
	case strings.HasPrefix(m, "video/"):
		return "video"
	case strings.HasPrefix(m, "audio/"):
		return "audio"
	default:
		return "document"
	}
}

func normalizeMediaAttachment(attachment mediaAttachment) mediaAttachment {
	declaredMIME := canonicalMIMEType(attachment.MIMEType)
	detectedMIME := ""
	if len(attachment.Data) > 0 {
		detectedMIME = canonicalMIMEType(http.DetectContentType(attachment.Data))
	}

	resolvedMIME := declaredMIME
	if isGenericMIMEType(resolvedMIME) {
		resolvedMIME = detectedMIME
	} else if isStrongDetectedMIME(detectedMIME) && !mimeTypesCompatible(resolvedMIME, detectedMIME) {
		resolvedMIME = detectedMIME
	}
	if resolvedMIME == "" {
		resolvedMIME = "application/octet-stream"
	}

	// WhatsApp voice notes use an Ogg/Opus container. Go correctly detects the
	// container as application/ogg, but Chatwoot needs audio/ogg to render its
	// audio player instead of a generic downloadable document.
	if detectedMIME == "application/ogg" && (attachment.FileType == "audio" || strings.HasPrefix(declaredMIME, "audio/")) {
		resolvedMIME = "audio/ogg"
	}
	// MP4 is also the container used by M4A audio. Preserve the semantic audio
	// type supplied by WhatsApp instead of classifying it as video from sniffing.
	if detectedMIME == "video/mp4" && (attachment.FileType == "audio" || strings.HasPrefix(declaredMIME, "audio/")) {
		resolvedMIME = "audio/mp4"
	}

	resolvedType := normalizeChatwootFileType("", resolvedMIME)
	if isGenericMIMEType(resolvedMIME) && strings.TrimSpace(attachment.FileType) != "" {
		resolvedType = normalizeChatwootFileType(attachment.FileType, resolvedMIME)
	}

	attachment.MIMEType = resolvedMIME
	attachment.FileType = resolvedType
	attachment.FileName = normalizeAttachmentFileName(attachment.FileName, resolvedMIME, resolvedType)
	return attachment
}

func canonicalMIMEType(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	parsed, _, err := mime.ParseMediaType(value)
	if err == nil && parsed != "" {
		return strings.ToLower(strings.TrimSpace(parsed))
	}
	if separator := strings.IndexByte(value, ';'); separator >= 0 {
		value = value[:separator]
	}
	return strings.ToLower(strings.TrimSpace(value))
}

func isGenericMIMEType(value string) bool {
	switch canonicalMIMEType(value) {
	case "", "application/octet-stream", "binary/octet-stream", "application/binary":
		return true
	default:
		return false
	}
}

func isStrongDetectedMIME(value string) bool {
	value = canonicalMIMEType(value)
	return value != "" && value != "application/octet-stream" && value != "text/plain"
}

func mimeTypesCompatible(declared string, detected string) bool {
	declared = canonicalMIMEType(declared)
	detected = canonicalMIMEType(detected)
	if declared == detected {
		return true
	}
	if (declared == "audio/ogg" || declared == "video/ogg" || declared == "application/ogg") && detected == "application/ogg" {
		return true
	}
	if (declared == "audio/mp4" || declared == "video/mp4") && detected == "video/mp4" {
		return true
	}
	if strings.HasPrefix(declared, "application/vnd.openxmlformats-officedocument.") && detected == "application/zip" {
		return true
	}
	declaredParts := strings.SplitN(declared, "/", 2)
	detectedParts := strings.SplitN(detected, "/", 2)
	return len(declaredParts) == 2 && len(detectedParts) == 2 && declaredParts[0] == detectedParts[0]
}

func normalizeAttachmentFileName(rawName string, mimeType string, fileType string) string {
	name := strings.TrimSpace(strings.ReplaceAll(rawName, "\\", "/"))
	name = path.Base(name)
	name = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 || r == '"' {
			return -1
		}
		return r
	}, name)
	if name == "" || name == "." || name == "/" {
		base := normalizeChatwootFileType(fileType, mimeType)
		if base == "document" {
			base = "arquivo"
		}
		name = fmt.Sprintf("%s-%d", base, time.Now().UnixNano())
	}

	preferredExtension := preferredExtensionForMIME(mimeType, fileType)
	if preferredExtension == "" {
		return name
	}
	currentExtension := strings.ToLower(path.Ext(name))
	if extensionMatchesMIME(currentExtension, mimeType) {
		return name
	}
	if currentExtension != "" {
		name = strings.TrimSuffix(name, path.Ext(name))
	}
	return strings.TrimRight(name, ". ") + preferredExtension
}

func preferredExtensionForMIME(mimeType string, fileType string) string {
	switch canonicalMIMEType(mimeType) {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "image/tiff":
		return ".tiff"
	case "audio/ogg", "application/ogg":
		if fileType == "audio" || canonicalMIMEType(mimeType) == "audio/ogg" {
			return ".ogg"
		}
		return ".ogx"
	case "audio/mpeg":
		return ".mp3"
	case "audio/mp4":
		return ".m4a"
	case "audio/aac":
		return ".aac"
	case "audio/wav", "audio/x-wav":
		return ".wav"
	case "video/mp4":
		return ".mp4"
	case "video/webm":
		return ".webm"
	case "application/pdf":
		return ".pdf"
	case "application/zip":
		return ".zip"
	case "application/vnd.openxmlformats-officedocument.wordprocessingml.document":
		return ".docx"
	case "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":
		return ".xlsx"
	case "application/vnd.openxmlformats-officedocument.presentationml.presentation":
		return ".pptx"
	case "text/plain":
		return ".txt"
	case "text/csv":
		return ".csv"
	case "application/json":
		return ".json"
	case "application/octet-stream":
		return ".bin"
	}

	extensions, err := mime.ExtensionsByType(canonicalMIMEType(mimeType))
	if err == nil && len(extensions) > 0 {
		return extensions[0]
	}
	return ""
}

func extensionMatchesMIME(extension string, mimeType string) bool {
	extension = strings.ToLower(strings.TrimSpace(extension))
	if extension == "" {
		return false
	}
	preferred := preferredExtensionForMIME(mimeType, "")
	if extension == preferred {
		return true
	}
	switch canonicalMIMEType(mimeType) {
	case "image/jpeg":
		return extension == ".jpeg" || extension == ".jpe"
	case "image/tiff":
		return extension == ".tif"
	case "audio/ogg":
		return extension == ".oga"
	}
	extensions, err := mime.ExtensionsByType(canonicalMIMEType(mimeType))
	if err != nil {
		return false
	}
	for _, candidate := range extensions {
		if extension == strings.ToLower(candidate) {
			return true
		}
	}
	return false
}

func normalizeMimeType(mime string, fallback string) string {
	m := strings.TrimSpace(mime)
	if m == "" {
		return fallback
	}
	return m
}

func shouldRetryWhatsmeowSend(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "timed out") || strings.Contains(msg, "timeout") {
		return true
	}
	if strings.Contains(msg, "usync") {
		return true
	}
	if strings.Contains(msg, "failed to get device list") {
		return true
	}
	return false
}

func verifyWebhookSignature(secret string, headers http.Header, body []byte) error {
	signature := headers.Get("X-Chatwoot-Signature")
	timestamp := headers.Get("X-Chatwoot-Timestamp")
	if signature == "" || timestamp == "" {
		return errors.New("missing chatwoot signature headers")
	}

	message := timestamp + "." + string(body)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(message))
	expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	if !hmac.Equal([]byte(expected), []byte(signature)) {
		return errors.New("invalid chatwoot signature")
	}

	// Reject stale webhook signatures older than 5 minutes.
	ts, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid chatwoot signature timestamp: %v", err)
	}
	if absDuration(time.Now().Unix()-ts) > 300 {
		return errors.New("stale chatwoot webhook signature")
	}

	return nil
}

func absDuration(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

func webhookMessageID(id interface{}) string {
	switch v := id.(type) {
	case nil:
		return "unknown"
	case string:
		if strings.TrimSpace(v) == "" {
			return "unknown"
		}
		return v
	case float64:
		if v == float64(int64(v)) {
			return strconv.FormatInt(int64(v), 10)
		}
		return fmt.Sprintf("%.2f", v)
	default:
		return fmt.Sprintf("%v", v)
	}
}

func NewChatwootService(
	repository chatwoot_repository.ChatwootRepository,
	evolutionClient evolution.API,
	lidResolver func(instanceID string, lidJID string) (string, bool),
	loggerWrapper *logger_wrapper.Manager,
) ChatwootService {
	service := &chatwootService{
		repository:      repository,
		evolutionClient: evolutionClient,
		lidResolver:     lidResolver,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
		skipCache:        cache.New(10*time.Minute, 20*time.Minute),
		webhookCache:     cache.New(24*time.Hour, 1*time.Hour),
		evolutionCache:   cache.New(24*time.Hour, 1*time.Hour),
		contactSyncCache: cache.New(24*time.Hour, 1*time.Hour),
		quoteCache:       cache.New(24*time.Hour, 1*time.Hour),
		outboundRecovery: map[string]outboundRecoveryState{},
		lidGracePeriod:   lidIdentityGracePeriod,
		lidPollInterval:  lidIdentityPollInterval,
		loggerWrapper:    loggerWrapper,
	}

	return service
}
