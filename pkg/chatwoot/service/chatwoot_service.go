package chatwoot_service

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
	"mime/multipart"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	chatwoot_model "github.com/EvolutionAPI/evolution-go/pkg/chatwoot/model"
	chatwoot_repository "github.com/EvolutionAPI/evolution-go/pkg/chatwoot/repository"
	instance_model "github.com/EvolutionAPI/evolution-go/pkg/instance/model"
	instance_repository "github.com/EvolutionAPI/evolution-go/pkg/instance/repository"
	logger_wrapper "github.com/EvolutionAPI/evolution-go/pkg/logger"
	"github.com/patrickmn/go-cache"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

type ChatwootService interface {
	Set(instanceID string, payload *chatwoot_model.SetChatwootPayload) (*chatwoot_model.ChatwootConfigView, error)
	Find(instanceID string) (*chatwoot_model.ChatwootConfigView, error)
	HandleWebhook(instanceID string, headers http.Header, body []byte) error
	SyncWhatsAppMessage(instance *instance_model.Instance, evt *events.Message, client *whatsmeow.Client)
	ShouldSkipOutgoingSync(instanceID string, messageID string) bool
}

type chatwootService struct {
	repository         chatwoot_repository.ChatwootRepository
	instanceRepository instance_repository.InstanceRepository
	clientPointer      map[string]*whatsmeow.Client
	httpClient         *http.Client
	skipCache          *cache.Cache
	lidCache           *cache.Cache
	loggerWrapper      *logger_wrapper.LoggerManager
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

type chatwootContactSearchResponse struct {
	Payload []struct {
		ID          int    `json:"id"`
		PhoneNumber string `json:"phone_number"`
		Identifier  string `json:"identifier"`
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

type chatwootWebhookPayload struct {
	Event       string `json:"event"`
	ID          interface{} `json:"id"`
	Content     string `json:"content"`
	MessageType string `json:"message_type"`
	Private     bool   `json:"private"`
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
	} `json:"attachments"`
}

func (s *chatwootService) Set(instanceID string, payload *chatwoot_model.SetChatwootPayload) (*chatwoot_model.ChatwootConfigView, error) {
	if payload == nil {
		return nil, fmt.Errorf("payload is required")
	}
	if _, err := s.instanceRepository.GetInstanceByID(instanceID); err != nil {
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
	if _, err := s.instanceRepository.GetInstanceByID(instanceID); err != nil {
		return nil, err
	}

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

func (s *chatwootService) ShouldSkipOutgoingSync(instanceID string, messageID string) bool {
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

	if payload.Event != "message_created" {
		return nil
	}
	if payload.Private {
		return nil
	}
	if payload.MessageType != "outgoing" {
		return nil
	}
	if payload.Sender.Type != "user" && payload.Sender.Type != "agent_bot" {
		return nil
	}
	chatwootMessageID := webhookMessageID(payload.ID)

	instance, err := s.instanceRepository.GetInstanceByID(instanceID)
	if err != nil {
		return err
	}

	client := s.clientPointer[instanceID]
	if client == nil || !client.IsConnected() {
		return fmt.Errorf("instance %s is not connected", instanceID)
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
		remoteJID = payload.Conversation.ContactInbox.SourceID
	}
	if remoteJID == "" {
		return fmt.Errorf("unable to resolve remote jid for conversation %d", payload.Conversation.ID)
	}

	recipient, ok := parseChatwootRemoteJID(remoteJID)
	if !ok {
		return fmt.Errorf("invalid remote jid: %s", remoteJID)
	}
	s.loggerWrapper.GetLogger(instanceID).LogInfo("[%s] Chatwoot webhook resolved recipient: remote=%s parsed=%s", instanceID, remoteJID, recipient.String())

	if strings.TrimSpace(payload.Content) != "" {
		if err := s.sendTextFromChatwoot(instance, client, recipient, remoteJID, payload.Content, chatwootMessageID); err != nil {
			s.loggerWrapper.GetLogger(instanceID).LogError("[%s] Failed to send text from Chatwoot to WhatsApp: %v", instanceID, err)
		}
	}

	for _, a := range payload.Attachments {
		if a.DataURL == "" {
			continue
		}
		if err := s.sendMediaFromChatwoot(instance, client, recipient, remoteJID, a.DataURL, a.FileType, chatwootMessageID); err != nil {
			s.loggerWrapper.GetLogger(instanceID).LogError("[%s] Failed to send media from Chatwoot to WhatsApp: %v", instanceID, err)
		}
	}

	return nil
}

func (s *chatwootService) SyncWhatsAppMessage(instance *instance_model.Instance, evt *events.Message, client *whatsmeow.Client) {
	if instance == nil || evt == nil || client == nil {
		return
	}

	cfg, err := s.repository.GetConfig(instance.Id)
	if err != nil {
		s.loggerWrapper.GetLogger(instance.Id).LogError("[%s] Failed to load Chatwoot config: %v", instance.Id, err)
		return
	}
	if cfg == nil || !cfg.Enabled {
		return
	}

	chatJID := normalizeRemoteJID(evt.Info.Chat.String())
	if chatJID == "" {
		return
	}

	if strings.HasSuffix(chatJID, "@g.us") || strings.HasSuffix(chatJID, "@newsletter") || strings.Contains(chatJID, "@broadcast") {
		return
	}

	if evt.Info.IsFromMe && s.ShouldSkipOutgoingSync(instance.Id, evt.Info.ID) {
		return
	}

	if shouldIgnoreJID(cfg.IgnoreJids, chatJID) {
		return
	}
	s.cacheLIDMappingFromMessage(instance.Id, evt)

	content, mediaType := extractMessageContent(evt.Message)
	messageType := "incoming"
	if evt.Info.IsFromMe {
		messageType = "outgoing"
	}
	messageSourceID := strings.TrimSpace(evt.Info.ID)
	if messageSourceID != "" {
		if evt.Info.IsFromMe {
			messageSourceID = "wa-out:" + messageSourceID
		} else {
			messageSourceID = "wa-in:" + messageSourceID
		}
	}

	binding, err := s.getOrCreateBinding(instance, cfg, evt)
	if err != nil {
		s.loggerWrapper.GetLogger(instance.Id).LogError("[%s] Failed to resolve Chatwoot binding: %v", instance.Id, err)
		return
	}

	var mediaData []byte
	if mediaType != "" {
		mediaData, err = downloadMediaFromEvent(client, evt.Message)
		if err != nil {
			s.loggerWrapper.GetLogger(instance.Id).LogWarn("[%s] Failed to download media for Chatwoot sync: %v", instance.Id, err)
			mediaType = ""
		}
	}

	if mediaType == "" {
		if strings.TrimSpace(content) == "" {
			content = "[message]"
		}
		if err := s.sendMessageToChatwoot(cfg, binding, content, messageType, messageSourceID, nil, ""); err != nil {
			s.loggerWrapper.GetLogger(instance.Id).LogError("[%s] Failed to sync message to Chatwoot: %v", instance.Id, err)
		}
		return
	}

	if strings.TrimSpace(content) == "" {
		content = fmt.Sprintf("[%s]", mediaType)
	}

	if err := s.sendMessageToChatwoot(cfg, binding, content, messageType, messageSourceID, mediaData, mediaType); err != nil {
		s.loggerWrapper.GetLogger(instance.Id).LogError("[%s] Failed to sync media message to Chatwoot: %v", instance.Id, err)
	}
}

func (s *chatwootService) getOrCreateBinding(instance *instance_model.Instance, cfg *chatwoot_model.ChatwootConfig, evt *events.Message) (*chatwoot_model.ChatwootBinding, error) {
	remoteJID := normalizeRemoteJID(evt.Info.Chat.String())

	binding, err := s.repository.GetBindingByRemoteJID(instance.Id, remoteJID)
	if err != nil {
		return nil, err
	}
	if binding != nil {
		return binding, nil
	}

	phone := toE164(remoteJID, cfg.MergeBrazilContacts)
	contactName := strings.TrimSpace(evt.Info.PushName)
	if contactName == "" {
		contactName = phone
	}

	contactID, err := s.createChatwootContact(cfg, contactName, phone, remoteJID)
	if err != nil {
		return nil, err
	}

	if err := s.createChatwootContactInbox(cfg, contactID, remoteJID); err != nil {
		return nil, err
	}

	conversationID, err := s.createChatwootConversation(cfg, contactID, remoteJID)
	if err != nil {
		return nil, err
	}

	binding = &chatwoot_model.ChatwootBinding{
		InstanceID:     instance.Id,
		RemoteJID:      remoteJID,
		ContactID:      contactID,
		ConversationID: conversationID,
		SourceID:       remoteJID,
	}

	if err := s.repository.SaveBinding(binding); err != nil {
		return nil, err
	}

	return binding, nil
}

func (s *chatwootService) createChatwootContact(cfg *chatwoot_model.ChatwootConfig, name string, phone string, identifier string) (int, error) {
	body := map[string]interface{}{
		"inbox_id":     cfg.InboxID,
		"name":         name,
		"phone_number": phone,
		"identifier":   identifier,
	}

	respBody, err := s.chatwootRequestJSON(http.MethodPost, cfg, fmt.Sprintf("/api/v1/accounts/%s/contacts", cfg.AccountID), body)
	if err != nil {
		var httpErr *chatwootHTTPError
		if errors.As(err, &httpErr) && httpErr.StatusCode == http.StatusUnprocessableEntity {
			// Contact may already exist in the account (another inbox/instance).
			// Reuse it to avoid stopping message sync.
			existingID, lookupErr := s.findExistingContactID(cfg, identifier, phone)
			if lookupErr == nil && existingID > 0 {
				return existingID, nil
			}
			if lookupErr != nil {
				return 0, fmt.Errorf("%v (lookup existing contact failed: %v)", err, lookupErr)
			}
		}
		return 0, err
	}

	var resp chatwootContactCreateResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return 0, err
	}

	if resp.ID > 0 {
		return resp.ID, nil
	}
	if len(resp.Payload) > 0 && resp.Payload[0].ID > 0 {
		return resp.Payload[0].ID, nil
	}

	return 0, fmt.Errorf("chatwoot contact id not found in response")
}

func (s *chatwootService) findExistingContactID(cfg *chatwoot_model.ChatwootConfig, identifier string, phone string) (int, error) {
	identifier = strings.TrimSpace(identifier)
	phone = strings.TrimSpace(phone)
	normalizedPhone := normalizePhoneForCompare(phone)

	queries := uniqueNonEmptyStrings([]string{
		identifier,
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
				if strings.EqualFold(strings.TrimSpace(ci.SourceID), identifier) {
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
	content string,
	messageType string,
	messageSourceID string,
	media []byte,
	mediaType string,
) error {
	route := fmt.Sprintf("/api/v1/accounts/%s/conversations/%d/messages", cfg.AccountID, binding.ConversationID)

	if len(media) == 0 {
		body := map[string]interface{}{
			"content":      content,
			"message_type": messageType,
			"private":      false,
		}
		if strings.TrimSpace(messageSourceID) != "" {
			body["source_id"] = messageSourceID
		}
		_, err := s.chatwootRequestJSON(http.MethodPost, cfg, route, body)
		return err
	}

	return s.chatwootRequestMultipart(cfg, route, content, messageType, messageSourceID, media, mediaType)
}

func (s *chatwootService) sendTextFromChatwoot(
	instance *instance_model.Instance,
	client *whatsmeow.Client,
	recipient types.JID,
	remoteJID string,
	content string,
	chatwootMessageID string,
) error {
	msg := &waE2E.Message{
		Conversation: proto.String(content),
	}

	if err := s.sendToWhatsAppWithRecipientFallback(instance.Id, client, recipient, remoteJID, msg); err != nil {
		return err
	}

	s.loggerWrapper.GetLogger(instance.Id).LogInfo("[%s] Chatwoot message %s sent to WhatsApp as text", instance.Id, chatwootMessageID)
	return nil
}

func (s *chatwootService) sendMediaFromChatwoot(
	instance *instance_model.Instance,
	client *whatsmeow.Client,
	recipient types.JID,
	remoteJID string,
	dataURL string,
	fileType string,
	chatwootMessageID string,
) error {
	fileData, mimeType, err := s.downloadAttachmentData(dataURL)
	if err != nil {
		return err
	}

	mimeType = strings.ToLower(mimeType)
	mediaKind := normalizeChatwootFileType(fileType, mimeType)

	uploadType := whatsmeow.MediaDocument
	switch mediaKind {
	case "image":
		uploadType = whatsmeow.MediaImage
	case "video":
		uploadType = whatsmeow.MediaVideo
	case "audio":
		uploadType = whatsmeow.MediaAudio
	default:
		uploadType = whatsmeow.MediaDocument
	}

	uploaded, err := client.Upload(context.Background(), fileData, uploadType)
	if err != nil {
		return err
	}

	var msg *waE2E.Message
	switch mediaKind {
	case "image":
		msg = &waE2E.Message{ImageMessage: &waE2E.ImageMessage{
			URL:           proto.String(uploaded.URL),
			DirectPath:    proto.String(uploaded.DirectPath),
			MediaKey:      uploaded.MediaKey,
			Mimetype:      proto.String(normalizeMimeType(mimeType, "image/jpeg")),
			FileEncSHA256: uploaded.FileEncSHA256,
			FileSHA256:    uploaded.FileSHA256,
			FileLength:    proto.Uint64(uint64(len(fileData))),
		}}
	case "video":
		msg = &waE2E.Message{VideoMessage: &waE2E.VideoMessage{
			URL:           proto.String(uploaded.URL),
			DirectPath:    proto.String(uploaded.DirectPath),
			MediaKey:      uploaded.MediaKey,
			Mimetype:      proto.String(normalizeMimeType(mimeType, "video/mp4")),
			FileEncSHA256: uploaded.FileEncSHA256,
			FileSHA256:    uploaded.FileSHA256,
			FileLength:    proto.Uint64(uint64(len(fileData))),
		}}
	case "audio":
		msg = &waE2E.Message{AudioMessage: &waE2E.AudioMessage{
			URL:           proto.String(uploaded.URL),
			DirectPath:    proto.String(uploaded.DirectPath),
			MediaKey:      uploaded.MediaKey,
			Mimetype:      proto.String(normalizeMimeType(mimeType, "audio/ogg")),
			FileEncSHA256: uploaded.FileEncSHA256,
			FileSHA256:    uploaded.FileSHA256,
			FileLength:    proto.Uint64(uint64(len(fileData))),
		}}
	default:
		fileName := fmt.Sprintf("chatwoot-%d", time.Now().Unix())
		msg = &waE2E.Message{DocumentMessage: &waE2E.DocumentMessage{
			Title:         proto.String(fileName),
			FileName:      proto.String(fileName),
			URL:           proto.String(uploaded.URL),
			DirectPath:    proto.String(uploaded.DirectPath),
			MediaKey:      uploaded.MediaKey,
			Mimetype:      proto.String(normalizeMimeType(mimeType, "application/octet-stream")),
			FileEncSHA256: uploaded.FileEncSHA256,
			FileSHA256:    uploaded.FileSHA256,
			FileLength:    proto.Uint64(uint64(len(fileData))),
		}}
	}

	if err := s.sendToWhatsAppWithRecipientFallback(instance.Id, client, recipient, remoteJID, msg); err != nil {
		return err
	}

	s.loggerWrapper.GetLogger(instance.Id).LogInfo("[%s] Chatwoot message %s sent to WhatsApp as media (%s)", instance.Id, chatwootMessageID, mediaKind)
	return nil
}

func (s *chatwootService) sendToWhatsAppWithRecipientFallback(
	instanceID string,
	client *whatsmeow.Client,
	recipient types.JID,
	remoteJID string,
	msg *waE2E.Message,
) error {
	err := s.sendWhatsAppMessageWithRetry(instanceID, client, recipient, msg)
	if err == nil {
		return nil
	}
	if !shouldTryAlternateRecipient(err) {
		return err
	}

	alt, reason, ok := s.resolveAlternateRecipient(instanceID, client, recipient, remoteJID)
	if !ok || alt.IsEmpty() || strings.EqualFold(alt.String(), recipient.String()) {
		return err
	}

	s.loggerWrapper.GetLogger(instanceID).LogWarn("[%s] Retrying Chatwoot->WhatsApp with alternate recipient (%s): %s -> %s", instanceID, reason, recipient.String(), alt.String())
	altErr := s.sendWhatsAppMessageWithRetry(instanceID, client, alt, msg)
	if altErr == nil {
		return nil
	}
	return fmt.Errorf("%v (alternate recipient %s failed: %v)", err, alt.String(), altErr)
}

func (s *chatwootService) resolveAlternateRecipient(
	instanceID string,
	client *whatsmeow.Client,
	recipient types.JID,
	remoteJID string,
) (types.JID, string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()

	if recipient.Server == types.DefaultUserServer {
		if lid, ok := s.getCachedLIDForPN(instanceID, remoteJID); ok {
			if parsed, err := types.ParseJID(lid); err == nil && !parsed.IsEmpty() {
				return parsed, "instance_lid_cache", true
			}
		}

		if client.Store != nil && client.Store.LIDs != nil {
			if lid, err := client.Store.LIDs.GetLIDForPN(ctx, recipient); err == nil && !lid.IsEmpty() {
				return lid, "store_lid_map", true
			}
		}

		// Force an up-to-date user info query to refresh LID mappings.
		if _, err := client.GetUserInfo(ctx, []types.JID{recipient}); err == nil {
			if client.Store != nil && client.Store.LIDs != nil {
				if lid, err := client.Store.LIDs.GetLIDForPN(ctx, recipient); err == nil && !lid.IsEmpty() {
					return lid, "usync_lid_map", true
				}
			}
		}
		return types.JID{}, "", false
	}

	if recipient.Server == types.HiddenUserServer && client.Store != nil && client.Store.LIDs != nil {
		if pn, err := client.Store.LIDs.GetPNForLID(ctx, recipient); err == nil && !pn.IsEmpty() {
			return pn, "store_pn_map", true
		}
	}

	return types.JID{}, "", false
}

func (s *chatwootService) sendWhatsAppMessageWithRetry(
	instanceID string,
	client *whatsmeow.Client,
	recipient types.JID,
	msg *waE2E.Message,
) error {
	const (
		maxAttempts = 2
		timeoutPerTry = 30 * time.Second
		retryDelay = 2 * time.Second
	)

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		messageID := client.GenerateMessageID()
		s.skipCache.Set(fmt.Sprintf("%s:%s", instanceID, messageID), true, 5*time.Minute)

		ctx, cancel := context.WithTimeout(context.Background(), timeoutPerTry)
		_, err := client.SendMessage(ctx, recipient, msg, whatsmeow.SendRequestExtra{ID: messageID})
		cancel()
		if err == nil {
			return nil
		}

		lastErr = err
		if attempt >= maxAttempts || !shouldRetryWhatsmeowSend(err) {
			break
		}

		s.loggerWrapper.GetLogger(instanceID).LogWarn("[%s] Chatwoot->WhatsApp send attempt %d failed, retrying: %v", instanceID, attempt, err)
		time.Sleep(retryDelay)
	}

	return lastErr
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
	media []byte,
	mediaType string,
) error {
	fullURL, err := joinChatwootURL(cfg.URL, route)
	if err != nil {
		return err
	}

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	_ = writer.WriteField("content", content)
	_ = writer.WriteField("message_type", messageType)
	_ = writer.WriteField("private", "false")
	_ = writer.WriteField("file_type", mediaType)
	if strings.TrimSpace(messageSourceID) != "" {
		_ = writer.WriteField("source_id", messageSourceID)
	}

	fileName := fmt.Sprintf("attachment-%d", time.Now().UnixNano())
	part, err := writer.CreateFormFile("attachments[]", fileName)
	if err != nil {
		return err
	}
	if _, err := part.Write(media); err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodPost, fullURL, body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("api_access_token", cfg.Token)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Fallback when attachment upload fails: keep text sync and preserve media metadata.
		fallbackContent := fmt.Sprintf("%s\n[attachment:%s upload_failed]", content, mediaType)
		fallbackBody := map[string]interface{}{
			"content":      fallbackContent,
			"message_type": messageType,
			"private":      false,
		}
		if strings.TrimSpace(messageSourceID) != "" {
			fallbackBody["source_id"] = messageSourceID
		}
		_, fallbackErr := s.chatwootRequestJSON(http.MethodPost, cfg, route, fallbackBody)
		if fallbackErr != nil {
			return fmt.Errorf("chatwoot multipart failed [%d]: %s", resp.StatusCode, string(respBody))
		}
	}

	return nil
}

func extractMessageContent(msg *waE2E.Message) (string, string) {
	if msg == nil {
		return "", ""
	}

	if msg.GetConversation() != "" {
		return msg.GetConversation(), ""
	}
	if ext := msg.GetExtendedTextMessage(); ext != nil {
		return ext.GetText(), ""
	}
	if img := msg.GetImageMessage(); img != nil {
		return img.GetCaption(), "image"
	}
	if video := msg.GetVideoMessage(); video != nil {
		return video.GetCaption(), "video"
	}
	if audio := msg.GetAudioMessage(); audio != nil {
		return "", "audio"
	}
	if doc := msg.GetDocumentMessage(); doc != nil {
		caption := doc.GetCaption()
		if caption == "" {
			caption = doc.GetTitle()
		}
		if caption == "" {
			caption = doc.GetFileName()
		}
		return caption, "document"
	}
	if sticker := msg.GetStickerMessage(); sticker != nil {
		_ = sticker
		return "[sticker]", "image"
	}
	return "[message]", ""
}

func downloadMediaFromEvent(client *whatsmeow.Client, msg *waE2E.Message) ([]byte, error) {
	if msg.GetImageMessage() != nil {
		return client.Download(context.Background(), msg.GetImageMessage())
	}
	if msg.GetVideoMessage() != nil {
		return client.Download(context.Background(), msg.GetVideoMessage())
	}
	if msg.GetAudioMessage() != nil {
		return client.Download(context.Background(), msg.GetAudioMessage())
	}
	if msg.GetDocumentMessage() != nil {
		return client.Download(context.Background(), msg.GetDocumentMessage())
	}
	if msg.GetStickerMessage() != nil {
		return client.Download(context.Background(), msg.GetStickerMessage())
	}
	return nil, fmt.Errorf("unsupported media type")
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

func parseChatwootRemoteJID(raw string) (types.JID, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return types.JID{}, false
	}
	if strings.Contains(raw, "@") {
		jid, err := types.ParseJID(raw)
		if err == nil {
			return jid, true
		}
	}
	normalized := strings.TrimPrefix(raw, "+")
	if normalized == "" {
		return types.JID{}, false
	}
	return types.JID{User: normalized, Server: types.DefaultUserServer}, true
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

	if mergeBrazilContacts && len(base) == 13 && strings.HasPrefix(base, "55") {
		// 55 + DDD(2) + 9 + number(8) => remove optional 9 for contact merge.
		if base[4] == '9' {
			base = base[:4] + base[5:]
		}
	}

	if base == "" {
		return ""
	}
	return "+" + base
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

func (s *chatwootService) cacheLIDMappingFromMessage(instanceID string, evt *events.Message) {
	if s == nil || s.lidCache == nil || evt == nil {
		return
	}
	chatJID := normalizeRemoteJID(evt.Info.Chat.String())
	senderAlt := normalizeRemoteJID(evt.Info.SenderAlt.String())
	if chatJID == "" || senderAlt == "" {
		return
	}
	if !strings.HasSuffix(strings.ToLower(chatJID), "@s.whatsapp.net") {
		return
	}
	if !strings.HasSuffix(strings.ToLower(senderAlt), "@lid") {
		return
	}
	s.lidCache.Set(fmt.Sprintf("%s:%s", instanceID, chatJID), senderAlt, 24*time.Hour)
}

func (s *chatwootService) getCachedLIDForPN(instanceID string, pn string) (string, bool) {
	if s == nil || s.lidCache == nil {
		return "", false
	}
	pn = normalizeRemoteJID(pn)
	if pn != "" && !strings.Contains(pn, "@") {
		normalizedUser := strings.TrimPrefix(strings.TrimSpace(pn), "+")
		if normalizedUser != "" {
			pn = normalizedUser + "@" + types.DefaultUserServer
		}
	}
	if pn == "" {
		return "", false
	}
	key := fmt.Sprintf("%s:%s", instanceID, pn)
	value, ok := s.lidCache.Get(key)
	if !ok {
		return "", false
	}
	lid, _ := value.(string)
	lid = normalizeRemoteJID(lid)
	if lid == "" {
		return "", false
	}
	return lid, true
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

func shouldTryAlternateRecipient(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "server returned error 463") {
		return true
	}
	if strings.Contains(msg, "server returned error 479") {
		return true
	}
	if strings.Contains(msg, "no lid found") {
		return true
	}
	if strings.Contains(msg, "failed to get lid for pn") {
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
	instanceRepository instance_repository.InstanceRepository,
	clientPointer map[string]*whatsmeow.Client,
	loggerWrapper *logger_wrapper.LoggerManager,
) ChatwootService {
	return &chatwootService{
		repository:         repository,
		instanceRepository: instanceRepository,
		clientPointer:      clientPointer,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
		skipCache:     cache.New(10*time.Minute, 20*time.Minute),
		lidCache:      cache.New(24*time.Hour, 30*time.Minute),
		loggerWrapper: loggerWrapper,
	}
}
