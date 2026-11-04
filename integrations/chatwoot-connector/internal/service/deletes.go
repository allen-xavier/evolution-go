package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	evolution "github.com/allen-xavier/evolution-go-chatwoot-connector/internal/evolution"
	chatwoot_model "github.com/allen-xavier/evolution-go-chatwoot-connector/internal/model"
)

// Agent deletes a reply in Chatwoot → "delete for everyone" on WhatsApp.
// Chatwoot's DELETE /api/v1/accounts/:account/conversations/:conversation/messages/:id
// keeps the message, replaces its content, drops the attachments and sets
// content_attributes.deleted = true, which reaches the inbox webhook as
// message_updated. The WhatsApp IDs come from the receipt links saved when the
// connector sent the message (text and each attachment are separate WhatsApp
// messages), or from source_id "wa-out:<id>" for messages sent from the phone.

const (
	// WhatsApp only revokes recent messages; the note text says "2 dias".
	whatsAppRevokeWindow = 48 * time.Hour
	// Chatwoot repeats message_updated (status changes, edits): act once.
	deletedMessageCacheTTL = 7 * 24 * time.Hour
	receiptStatusDeleted   = "deleted"
	revokeRequestTimeout   = 60 * time.Second
)

const (
	revokeReasonExpired = "passou do prazo de 2 dias que o WhatsApp dá para apagar para todos"
	revokeReasonRefused = "o WhatsApp recusou o pedido de apagar"
	revokeReasonNoLink  = "mensagem antiga, sem vínculo com a mensagem do WhatsApp"
	revokeReasonNoChat  = "não foi possível identificar o contato no WhatsApp"
)

type revokeTarget struct {
	whatsAppMessageID string
	// receiptID is zero for a message sent from the phone (no receipt link).
	receiptID uint
}

func deletedMessageKey(instanceID string, chatwootMessageID string) string {
	return fmt.Sprintf("%s:deleted:%s", instanceID, chatwootMessageID)
}

// deletedInChatwoot reports that the agent deleted this Chatwoot message.
func (s *chatwootService) deletedInChatwoot(instanceID string, chatwootMessageID string) bool {
	if s.webhookCache == nil {
		return false
	}
	_, found := s.webhookCache.Get(deletedMessageKey(instanceID, chatwootMessageID))
	return found
}

// chatwootMessageDeleted reads content_attributes.deleted (an object, or the
// same object encoded as a JSON string).
func chatwootMessageDeleted(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return false
	}
	if trimmed[0] == '"' {
		var encoded string
		if err := json.Unmarshal(trimmed, &encoded); err != nil {
			return false
		}
		trimmed = []byte(encoded)
	}
	var attributes map[string]interface{}
	if err := json.Unmarshal(trimmed, &attributes); err != nil {
		return false
	}
	switch deleted := attributes["deleted"].(type) {
	case bool:
		return deleted
	case string:
		return strings.EqualFold(strings.TrimSpace(deleted), "true")
	default:
		return false
	}
}

// chatwootTimestamp parses created_at: ISO 8601 or Unix seconds. Zero if unknown.
func chatwootTimestamp(raw json.RawMessage) time.Time {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return time.Time{}
	}
	var value interface{}
	if err := json.Unmarshal(trimmed, &value); err != nil {
		return time.Time{}
	}
	switch typed := value.(type) {
	case float64:
		if typed > 0 {
			return time.Unix(int64(typed), 0)
		}
	case string:
		text := strings.TrimSpace(typed)
		if seconds, err := strconv.ParseInt(text, 10, 64); err == nil && seconds > 0 {
			return time.Unix(seconds, 0)
		}
		for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05 MST", "2006-01-02 15:04:05 -0700"} {
			if parsed, err := time.Parse(layout, text); err == nil {
				return parsed
			}
		}
	}
	return time.Time{}
}

// handleChatwootMessageUpdated revokes on WhatsApp a reply the agent deleted in
// Chatwoot. Every other message_updated (status, edits, incoming messages,
// private notes) is ignored. When the message cannot be revoked the agents get
// a private note: it is still on the customer's phone.
func (s *chatwootService) handleChatwootMessageUpdated(
	instanceID string,
	cfg *chatwoot_model.ChatwootConfig,
	payload chatwootWebhookPayload,
) error {
	if payload.Private || payload.MessageType != "outgoing" || !chatwootMessageDeleted(payload.ContentAttributes) {
		return nil
	}
	chatwootMessageID := webhookMessageID(payload.ID)
	messageID, err := strconv.Atoi(chatwootMessageID)
	if err != nil || messageID <= 0 {
		return nil
	}
	logger := s.loggerWrapper.GetLogger(instanceID)
	key := deletedMessageKey(instanceID, chatwootMessageID)
	if s.webhookCache != nil {
		if err := s.webhookCache.Add(key, true, deletedMessageCacheTTL); err != nil {
			logger.LogInfo("[%s] Chatwoot message %s deletion already handled", instanceID, chatwootMessageID)
			return nil
		}
	}

	// A reply still waiting for WhatsApp (number disconnected, retries) is
	// dropped: it has not reached the customer, so no note is needed.
	waiting := s.cancelWaitingReply(instanceID, chatwootMessageID)
	notify := func(reason string) {
		if waiting {
			logger.LogInfo("[%s] Deleted Chatwoot message %s had not reached WhatsApp yet (%s)", instanceID, chatwootMessageID, reason)
			return
		}
		s.notifyNotRevoked(cfg, payload, reason)
	}

	receipts, err := s.repository.ListMessageReceiptsByChatwootMessage(instanceID, messageID)
	if err != nil {
		if s.webhookCache != nil {
			s.webhookCache.Delete(key)
		}
		return fmt.Errorf("load WhatsApp messages of deleted Chatwoot message %s: %w", chatwootMessageID, err)
	}
	targets := []revokeTarget{}
	alreadyRevoked := false
	var sentAt time.Time
	for _, receipt := range receipts {
		if receipt.Status == receiptStatusDeleted {
			alreadyRevoked = true
			continue
		}
		targets = append(targets, revokeTarget{whatsAppMessageID: receipt.WhatsAppMessageID, receiptID: receipt.ID})
		if !receipt.CreatedAt.IsZero() && (sentAt.IsZero() || receipt.CreatedAt.Before(sentAt)) {
			sentAt = receipt.CreatedAt
		}
	}
	if len(targets) == 0 && alreadyRevoked {
		logger.LogInfo("[%s] Chatwoot message %s was already deleted on WhatsApp", instanceID, chatwootMessageID)
		return nil
	}
	sourceID := strings.TrimSpace(payload.SourceID)
	if strings.HasPrefix(strings.ToLower(sourceID), "wa-out:") {
		if phoneMessageID := strings.TrimSpace(sourceID[len("wa-out:"):]); phoneMessageID != "" {
			targets = append(targets, revokeTarget{whatsAppMessageID: phoneMessageID})
		}
	}
	if len(targets) == 0 {
		// message_created only sends messages written by agents or bots; any
		// other outgoing message never reached WhatsApp.
		if payload.Sender.Type != "user" && payload.Sender.Type != "agent_bot" {
			return nil
		}
		logger.LogWarn("[%s] Deleted Chatwoot message %s has no WhatsApp message link", instanceID, chatwootMessageID)
		notify(revokeReasonNoLink)
		return nil
	}
	if sentAt.IsZero() {
		sentAt = chatwootTimestamp(payload.CreatedAt)
	}
	if !sentAt.IsZero() && time.Since(sentAt) > whatsAppRevokeWindow {
		logger.LogWarn(
			"[%s] Deleted Chatwoot message %s was sent at %s, too old to delete on WhatsApp",
			instanceID,
			chatwootMessageID,
			sentAt.Format(time.RFC3339),
		)
		notify(revokeReasonExpired)
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), revokeRequestTimeout)
	defer cancel()
	instance, err := s.evolutionClient.GetInstance(ctx, instanceID)
	if err != nil {
		logger.LogError("[%s] Could not load instance to delete Chatwoot message %s on WhatsApp: %v", instanceID, chatwootMessageID, err)
		notify(revokeReasonRefused)
		return nil
	}
	if !instance.Connected {
		disconnected := newInstanceDisconnectedError(instance)
		logger.LogWarn("[%s] Deleted Chatwoot message %s not deleted on WhatsApp: %v", instanceID, chatwootMessageID, disconnected)
		notify(fmt.Sprintf("o WhatsApp %s está desconectado", disconnected.label()))
		return nil
	}
	remoteJID, _, err := s.resolveChatwootRecipient(instanceID, payload)
	if err != nil {
		logger.LogWarn("[%s] Deleted Chatwoot message %s: %v", instanceID, chatwootMessageID, err)
		notify(revokeReasonNoChat)
		return nil
	}
	chat := s.revokeChatJID(ctx, instance.Id, remoteJID)

	failed := 0
	for _, target := range targets {
		request := evolution.DeleteMessageRequest{Chat: chat, MessageID: target.whatsAppMessageID}
		if err := s.evolutionClient.DeleteMessageForEveryone(ctx, instance, request); err != nil {
			failed++
			logger.LogError(
				"[%s] WhatsApp refused to delete message %s (Chatwoot message %s, chat %s): %v",
				instanceID,
				target.whatsAppMessageID,
				chatwootMessageID,
				chat,
				err,
			)
			continue
		}
		if target.receiptID > 0 {
			if err := s.repository.UpdateMessageReceiptStatus(target.receiptID, receiptStatusDeleted); err != nil {
				logger.LogWarn("[%s] Could not mark WhatsApp message %s as deleted: %v", instanceID, target.whatsAppMessageID, err)
			}
		}
		logger.LogInfo(
			"[%s] Chatwoot message %s deleted for everyone on WhatsApp (message %s, chat %s)",
			instanceID,
			chatwootMessageID,
			target.whatsAppMessageID,
			chat,
		)
	}
	switch {
	case failed == len(targets):
		notify(revokeReasonRefused)
	case failed > 0 && !waiting:
		s.postPrivateNote(cfg, payload.Conversation.ID, instanceID, fmt.Sprintf(
			"⚠️ Mensagem apagada só em parte no WhatsApp: %d de %d partes continuam no WhatsApp do cliente (%s).",
			failed,
			len(targets),
			revokeReasonRefused,
		))
	}
	return nil
}

// cancelWaitingReply closes the retry job of a reply that has not reached
// WhatsApp yet, and reports whether there was one.
func (s *chatwootService) cancelWaitingReply(instanceID string, chatwootMessageID string) bool {
	// A message just received from the webhook is handled by its lane: still
	// queued, the lane drops it (the deletion is already recorded); already being
	// sent, it may reach WhatsApp and follows the normal revoke path.
	if inflight, started := s.inflightState(instanceID, chatwootMessageID); inflight {
		return !started
	}
	job, err := s.repository.GetPendingOutboundJob(instanceID, chatwootMessageID)
	if err != nil {
		s.loggerWrapper.GetLogger(instanceID).LogWarn("[%s] Could not look up queued reply %s: %v", instanceID, chatwootMessageID, err)
		return false
	}
	if job == nil {
		return false
	}
	s.failOutboundJob(job, errors.New("message deleted in Chatwoot before delivery"))
	return true
}

func (s *chatwootService) notifyNotRevoked(cfg *chatwoot_model.ChatwootConfig, payload chatwootWebhookPayload, reason string) {
	s.postPrivateNote(cfg, payload.Conversation.ID, cfg.InstanceID, fmt.Sprintf(
		"⚠️ Mensagem apagada só no Chatwoot: ela continua no WhatsApp do cliente (%s).",
		reason,
	))
}

// revokeChatJID is the chat the message was sent to. For a phone number the
// send asked WhatsApp for the account's JID (CHECK_USER_EXISTS), which can drop
// or keep a Brazilian ninth digit, so the same lookup is made here.
func (s *chatwootService) revokeChatJID(ctx context.Context, instanceID string, remoteJID string) string {
	remoteJID = normalizeRemoteJID(remoteJID)
	if strings.Contains(remoteJID, "@") && !isPhoneRemoteJID(remoteJID) {
		return remoteJID
	}
	number := strings.TrimPrefix(strings.SplitN(remoteJID, "@", 2)[0], "+")
	fallback := number + "@s.whatsapp.net"
	checks, err := s.evolutionClient.CheckNumbers(ctx, instanceID, []string{number})
	if err != nil {
		s.loggerWrapper.GetLogger(instanceID).LogWarn("[%s] Could not look up WhatsApp JID of %s, using %s: %v", instanceID, number, fallback, err)
		return fallback
	}
	for _, check := range checks {
		if jid := normalizeRemoteJID(check.JID); check.OnWhatsApp && isPhoneRemoteJID(jid) {
			return jid
		}
	}
	return fallback
}
