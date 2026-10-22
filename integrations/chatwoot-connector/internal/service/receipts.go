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

// WhatsApp delivery/read receipts → message status in Chatwoot (✓ sent, ✓✓
// delivered, read). Evolution publishes them as "Receipt" events on the
// "receipt" queue when AMQP_GLOBAL_EVENTS contains READ_RECEIPT. Only messages
// the connector sent to WhatsApp are tracked (their ID is chosen by the
// connector), through the API-channel endpoint
// PATCH /api/v1/accounts/:account/conversations/:conversation/messages/:id.

const (
	messageReceiptRetention = 30 * 24 * time.Hour
	receiptCleanupInterval  = 6 * time.Hour
)

var receiptStatusRank = map[string]int{"sent": 1, "delivered": 2, "read": 3}

// receiptStatus maps Evolution's receipt state to the Chatwoot message status.
// ReadSelf (this account read the contact's message) does not change our ticks.
func receiptStatus(state string) string {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "delivered":
		return "delivered"
	case "read":
		return "read"
	default:
		return ""
	}
}

// receiptDevice returns the device number of a JID like "5531...:38@s.whatsapp.net"
// or "2846...:38@lid" (no ":N" means device 0, the phone).
func receiptDevice(jid string) int {
	user := strings.SplitN(strings.TrimSpace(jid), "@", 2)[0]
	parts := strings.SplitN(user, ":", 2)
	if len(parts) < 2 {
		return 0
	}
	device, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0
	}
	return device
}

func receiptMessageIDs(data map[string]interface{}) []string {
	raw := mapLookup(data, "MessageIDs", "messageIds", "message_ids")
	list, ok := raw.([]interface{})
	if !ok {
		return nil
	}
	ids := make([]string, 0, len(list))
	for _, item := range list {
		if id, ok := item.(string); ok && strings.TrimSpace(id) != "" {
			ids = append(ids, strings.TrimSpace(id))
		}
	}
	return ids
}

// rememberMessageReceipt links a WhatsApp message the connector just sent to its
// Chatwoot message. Failures only cost the ticks of that message, so they are
// logged and never block the delivery.
func (s *chatwootService) rememberMessageReceipt(instanceID string, whatsAppMessageID string, conversationID int, chatwootMessageID string) {
	messageID, err := strconv.Atoi(strings.TrimSpace(chatwootMessageID))
	if err != nil || messageID <= 0 || conversationID <= 0 || strings.TrimSpace(whatsAppMessageID) == "" {
		return
	}
	if err := s.repository.SaveMessageReceipt(&chatwoot_model.ChatwootMessageReceipt{
		InstanceID:        instanceID,
		WhatsAppMessageID: whatsAppMessageID,
		ConversationID:    conversationID,
		ChatwootMessageID: messageID,
		Status:            "sent",
	}); err != nil {
		s.loggerWrapper.GetLogger(instanceID).LogWarn(
			"[%s] Could not record receipt link for Chatwoot message %s: %v",
			instanceID,
			chatwootMessageID,
			err,
		)
	}
}

// handleReceiptEvent moves the Chatwoot message forward (never back: a read
// message is not turned into delivered by a late delivery receipt).
func (s *chatwootService) handleReceiptEvent(payload evolutionWebhookPayload) error {
	status := receiptStatus(payload.State)
	instanceID := strings.TrimSpace(payload.InstanceID)
	if status == "" || instanceID == "" {
		return nil
	}
	if isGroup, _ := mapLookup(payload.Data, "IsGroup", "isGroup").(bool); isGroup {
		return nil
	}
	ids := receiptMessageIDs(payload.Data)
	if len(ids) == 0 {
		return nil
	}
	sender, _ := mapLookup(payload.Data, "Sender", "sender").(string)
	chat, _ := mapLookup(payload.Data, "Chat", "chat").(string)
	// Receipts from this account's own devices (the number's phone, other linked
	// devices) say nothing about the contact: only the contact's receipts count.
	if fromMe, _ := mapLookup(payload.Data, "IsFromMe", "isFromMe").(bool); fromMe {
		s.loggerWrapper.GetLogger(instanceID).LogInfo(
			"[%s] Ignoring %s receipt from own device %s (chat %s)",
			instanceID,
			status,
			sender,
			chat,
		)
		return nil
	}
	// Device ":0" is the contact's phone; ":N" are linked devices (WhatsApp Web,
	// Desktop, tablet, integrations). WhatsApp counts those as delivered too, but
	// the user wants ✓✓ to mean "reached the contact's phone" (to notice when the
	// contact is offline or has blocked the number), so delivery only counts from
	// the phone. A read on any device is still a read.
	if status == "delivered" && receiptDevice(sender) != 0 {
		s.loggerWrapper.GetLogger(instanceID).LogInfo(
			"[%s] Ignoring delivered receipt from linked device %s (chat %s): only the contact's phone counts",
			instanceID,
			sender,
			chat,
		)
		return nil
	}
	s.loggerWrapper.GetLogger(instanceID).LogInfo(
		"[%s] WhatsApp %s receipt from %s (chat %s) for %d message(s)",
		instanceID,
		status,
		sender,
		chat,
		len(ids),
	)

	cfg, err := s.repository.GetConfig(instanceID)
	if err != nil {
		return fmt.Errorf("load Chatwoot config for %s: %w", instanceID, err)
	}
	if cfg == nil || !cfg.Enabled {
		return nil
	}

	for _, id := range ids {
		receipt, err := s.repository.GetMessageReceipt(instanceID, id)
		if err != nil {
			return fmt.Errorf("load receipt link for %s: %w", id, err)
		}
		if receipt == nil || receiptStatusRank[receipt.Status] >= receiptStatusRank[status] {
			continue
		}
		route := fmt.Sprintf(
			"/api/v1/accounts/%s/conversations/%d/messages/%d",
			cfg.AccountID,
			receipt.ConversationID,
			receipt.ChatwootMessageID,
		)
		if _, err := s.chatwootRequestJSON(http.MethodPatch, cfg, route, map[string]interface{}{"status": status}); err != nil {
			var httpErr *chatwootHTTPError
			if errors.As(err, &httpErr) && httpErr.StatusCode >= 400 && httpErr.StatusCode < 500 {
				// Message deleted, conversation moved or Chatwoot without the
				// endpoint: retrying will not help.
				s.loggerWrapper.GetLogger(instanceID).LogWarn(
					"[%s] Chatwoot refused status %s for message %d: %v",
					instanceID,
					status,
					receipt.ChatwootMessageID,
					err,
				)
				continue
			}
			return fmt.Errorf("update Chatwoot message %d status: %w", receipt.ChatwootMessageID, err)
		}
		if err := s.repository.UpdateMessageReceiptStatus(receipt.ID, status); err != nil {
			return fmt.Errorf("save receipt status for %s: %w", id, err)
		}
		s.loggerWrapper.GetLogger(instanceID).LogInfo(
			"[%s] WhatsApp %s receipt applied to Chatwoot message %d",
			instanceID,
			status,
			receipt.ChatwootMessageID,
		)
	}
	return nil
}

func (s *chatwootService) cleanupMessageReceipts() {
	removed, err := s.repository.DeleteMessageReceiptsBefore(time.Now().Add(-messageReceiptRetention))
	if err != nil {
		s.loggerWrapper.GetLogger("system").LogWarn("[system] Could not clean old receipt links: %v", err)
		return
	}
	if removed > 0 {
		s.loggerWrapper.GetLogger("system").LogInfo("[system] Removed %d old receipt links", removed)
	}
}

// runReceiptCleanup keeps the receipt link table small.
func (s *chatwootService) runReceiptCleanup(ctx context.Context) {
	s.cleanupMessageReceipts()
	ticker := time.NewTicker(receiptCleanupInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.cleanupMessageReceipts()
		}
	}
}
