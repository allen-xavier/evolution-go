package chatwoot_service

import (
	"bytes"
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
	send_service "github.com/EvolutionAPI/evolution-go/pkg/sendMessage/service"
	"github.com/patrickmn/go-cache"
)

type ChatwootService interface {
	Set(instanceID string, payload *chatwoot_model.SetChatwootPayload) (*chatwoot_model.ChatwootConfigView, error)
	Find(instanceID string) (*chatwoot_model.ChatwootConfigView, error)
	HandleWebhook(instanceID string, headers http.Header, body []byte) error
	HandleEvolutionEvent(instance *instance_model.Instance, eventType string, queueName string, payload []byte)
}

type chatwootService struct {
	repository         chatwoot_repository.ChatwootRepository
	instanceRepository instance_repository.InstanceRepository
	sendMessageService send_service.SendService
	lidResolver        func(instanceID string, lidJID string) (string, bool)
	httpClient         *http.Client
	skipCache          *cache.Cache
	webhookCache       *cache.Cache
	eventQueue         chan chatwootEvent
	loggerWrapper      *logger_wrapper.LoggerManager
}

type chatwootEvent struct {
	instance  *instance_model.Instance
	eventType string
	queueName string
	payload   []byte
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
	MediaType       string
	Media           []byte
	MessageID       string
	MessageSourceID string
	FromMe          bool
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
	webhookKey := fmt.Sprintf("%s:%s", instanceID, chatwootMessageID)
	if err := s.webhookCache.Add(webhookKey, true, 24*time.Hour); err != nil {
		s.loggerWrapper.GetLogger(instanceID).LogInfo("[%s] Skipping duplicated Chatwoot webhook message: %s", instanceID, chatwootMessageID)
		return nil
	}

	instance, err := s.instanceRepository.GetInstanceByID(instanceID)
	if err != nil {
		s.webhookCache.Delete(webhookKey)
		return err
	}

	remoteJID := ""
	binding, err := s.repository.GetBindingByConversationID(instanceID, payload.Conversation.ID)
	if err != nil {
		s.webhookCache.Delete(webhookKey)
		return err
	}
	if binding != nil {
		remoteJID = binding.RemoteJID
	}
	if remoteJID == "" {
		remoteJID = remoteJIDFromSourceID(instanceID, payload.Conversation.ContactInbox.SourceID)
	}
	if remoteJID == "" {
		s.webhookCache.Delete(webhookKey)
		return fmt.Errorf("unable to resolve remote jid for conversation %d", payload.Conversation.ID)
	}
	s.loggerWrapper.GetLogger(instanceID).LogInfo("[%s] Chatwoot webhook resolved recipient: remote=%s", instanceID, remoteJID)

	var sendErrors []string
	hasAttachment := false
	for _, a := range payload.Attachments {
		if strings.TrimSpace(a.DataURL) != "" {
			hasAttachment = true
			break
		}
	}

	content := applyChatwootSignature(cfg, payload.Content, payload.Sender.Name)
	if strings.TrimSpace(content) != "" && !hasAttachment {
		if err := s.sendTextFromChatwoot(instance, remoteJID, content, chatwootMessageID); err != nil {
			s.loggerWrapper.GetLogger(instanceID).LogError("[%s] Failed to send text from Chatwoot to WhatsApp: %v", instanceID, err)
			sendErrors = append(sendErrors, fmt.Sprintf("text:%v", err))
		}
	}

	attachmentIndex := 0
	for _, a := range payload.Attachments {
		if a.DataURL == "" {
			continue
		}
		caption := ""
		if attachmentIndex == 0 {
			caption = content
		}
		attachmentIndex++
		if err := s.sendMediaFromChatwoot(instance, remoteJID, a.DataURL, a.FileType, a.FileName, caption, chatwootMessageID); err != nil {
			s.loggerWrapper.GetLogger(instanceID).LogError("[%s] Failed to send media from Chatwoot to WhatsApp: %v", instanceID, err)
			sendErrors = append(sendErrors, fmt.Sprintf("media:%v", err))
		}
	}

	if len(sendErrors) > 0 {
		s.webhookCache.Delete(webhookKey)
		return fmt.Errorf("failed to deliver chatwoot message %s: %s", chatwootMessageID, strings.Join(sendErrors, " | "))
	}

	return nil
}

func (s *chatwootService) HandleEvolutionEvent(instance *instance_model.Instance, eventType string, queueName string, payload []byte) {
	if instance == nil {
		return
	}
	if eventType != "Message" && eventType != "SendMessage" {
		return
	}

	event := chatwootEvent{
		instance:  instance,
		eventType: eventType,
		queueName: queueName,
		payload:   append([]byte(nil), payload...),
	}

	select {
	case s.eventQueue <- event:
	default:
		s.loggerWrapper.GetLogger(instance.Id).LogError("[%s] Chatwoot event queue is full; dropping %s event", instance.Id, eventType)
	}
}

func (s *chatwootService) runEventWorker() {
	for evt := range s.eventQueue {
		s.syncEvolutionEventToChatwoot(evt)
	}
}

func (s *chatwootService) syncEvolutionEventToChatwoot(evt chatwootEvent) {
	instance := evt.instance
	if instance == nil {
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

	var payload evolutionWebhookPayload
	if err := json.Unmarshal(evt.payload, &payload); err != nil {
		s.loggerWrapper.GetLogger(instance.Id).LogError("[%s] Failed to parse Evolution event for Chatwoot: %v", instance.Id, err)
		return
	}
	if payload.Event == "" {
		payload.Event = evt.eventType
	}
	if payload.InstanceID == "" {
		payload.InstanceID = instance.Id
	}
	if payload.Event != "Message" && payload.Event != "SendMessage" {
		return
	}

	message, ok := s.extractEvolutionMessage(payload, evt.payload)
	if !ok {
		return
	}

	if message.RemoteJID == "" {
		return
	}
	if strings.HasSuffix(message.RemoteJID, "@g.us") ||
		strings.HasSuffix(message.RemoteJID, "@newsletter") ||
		strings.Contains(message.RemoteJID, "@broadcast") {
		return
	}

	if message.FromMe && s.shouldSkipOutgoingSync(instance.Id, message.MessageID) {
		return
	}
	if shouldIgnoreJID(cfg.IgnoreJids, message.RemoteJID) {
		return
	}

	messageType := "incoming"
	if message.FromMe {
		messageType = "outgoing"
	}

	if strings.TrimSpace(message.Content) == "" {
		if message.MediaType != "" {
			message.Content = fmt.Sprintf("[%s]", message.MediaType)
		} else {
			message.Content = "[message]"
		}
	}

	if len(message.Media) == 0 || message.MediaType == "" {
		if err := s.syncEvolutionMessageWithBindingRefresh(instance, cfg, message, messageType, nil, ""); err != nil {
			s.loggerWrapper.GetLogger(instance.Id).LogError("[%s] Failed to sync message to Chatwoot: %v", instance.Id, err)
		}
		return
	}

	if err := s.syncEvolutionMessageWithBindingRefresh(instance, cfg, message, messageType, message.Media, message.MediaType); err != nil {
		s.loggerWrapper.GetLogger(instance.Id).LogError("[%s] Failed to sync media message to Chatwoot: %v", instance.Id, err)
	}
}

func (s *chatwootService) syncEvolutionMessageWithBindingRefresh(
	instance *instance_model.Instance,
	cfg *chatwoot_model.ChatwootConfig,
	message evolutionMessage,
	messageType string,
	media []byte,
	mediaType string,
) error {
	binding, err := s.getOrCreateBindingByRemote(instance, cfg, message.RemoteJID, message.ContactName, false)
	if err != nil {
		return fmt.Errorf("failed to resolve Chatwoot binding: %v", err)
	}

	err = s.sendMessageToChatwoot(cfg, binding, message.Content, messageType, message.MessageSourceID, media, mediaType)
	if !isChatwootNotFound(err) {
		return err
	}

	s.loggerWrapper.GetLogger(instance.Id).LogWarn("[%s] Chatwoot binding not found remotely; recreating contact/conversation for %s", instance.Id, message.RemoteJID)

	binding, refreshErr := s.getOrCreateBindingByRemote(instance, cfg, message.RemoteJID, message.ContactName, true)
	if refreshErr != nil {
		return fmt.Errorf("%v (failed to refresh Chatwoot binding: %v)", err, refreshErr)
	}

	return s.sendMessageToChatwoot(cfg, binding, message.Content, messageType, message.MessageSourceID, media, mediaType)
}

func (s *chatwootService) getOrCreateBindingByRemote(instance *instance_model.Instance, cfg *chatwoot_model.ChatwootConfig, remoteJID string, contactName string, forceRefresh bool) (*chatwoot_model.ChatwootBinding, error) {
	remoteJID = normalizeRemoteJID(remoteJID)
	contactRef := buildChatwootContactRef(instance.Id, remoteJID, contactName, cfg.MergeBrazilContacts)

	binding, err := s.repository.GetBindingByRemoteJID(instance.Id, remoteJID)
	if err != nil {
		return nil, err
	}
	if !forceRefresh && binding != nil && strings.EqualFold(strings.TrimSpace(binding.SourceID), contactRef.SourceID) {
		return binding, nil
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

	_, err := s.sendMessageService.SendText(&send_service.TextStruct{
		Number:    number,
		Text:      text,
		Id:        messageID,
		FormatJid: formatJID,
	}, instance)
	if err != nil {
		s.skipCache.Delete(fmt.Sprintf("%s:%s", instance.Id, messageID))
		return err
	}

	s.loggerWrapper.GetLogger(instance.Id).LogInfo("[%s] Chatwoot message %s sent to WhatsApp through Evolution send service as text", instance.Id, chatwootMessageID)
	return nil
}

func (s *chatwootService) sendMediaFromChatwoot(
	instance *instance_model.Instance,
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

	_, err = s.sendMessageService.SendMediaFile(&send_service.MediaStruct{
		Number:    number,
		Type:      mediaKind,
		Caption:   caption,
		Filename:  fileName,
		Id:        messageID,
		FormatJid: formatJID,
	}, fileData, instance)
	if err != nil {
		s.skipCache.Delete(fmt.Sprintf("%s:%s", instance.Id, messageID))
		return err
	}

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
		if resp.StatusCode == http.StatusNotFound {
			return &chatwootHTTPError{
				StatusCode: resp.StatusCode,
				Body:       string(respBody),
			}
		}

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

func (s *chatwootService) extractEvolutionMessage(payload evolutionWebhookPayload, rawPayload []byte) (evolutionMessage, bool) {
	data := payload.Data
	if data == nil {
		return evolutionMessage{}, false
	}

	info := mapFromAny(mapLookup(data, "Info", "info"))
	messageMap := mapFromAny(mapLookup(data, "Message", "message"))

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

	content, mediaType, media := s.extractContentAndMedia(messageMap)
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
		MediaType:       mediaType,
		Media:           media,
		MessageID:       messageID,
		MessageSourceID: sourcePrefix + messageID,
		FromMe:          fromMe,
	}, true
}

func (s *chatwootService) extractContentAndMedia(messageMap map[string]interface{}) (string, string, []byte) {
	if len(messageMap) == 0 {
		return "[message]", "", nil
	}

	if conversation := strings.TrimSpace(mapString(messageMap, "conversation", "Conversation")); conversation != "" {
		return conversation, "", nil
	}
	if ext := mapFromAny(mapLookup(messageMap, "extendedTextMessage", "ExtendedTextMessage")); ext != nil {
		return mapString(ext, "text", "Text"), "", nil
	}
	if child := mapFromAny(mapLookup(messageMap, "documentWithCaptionMessage", "DocumentWithCaptionMessage")); child != nil {
		if nested := mapFromAny(mapLookup(child, "message", "Message")); nested != nil {
			return s.extractContentAndMedia(nested)
		}
	}

	if img := mapFromAny(mapLookup(messageMap, "imageMessage", "ImageMessage")); img != nil {
		media, mediaType := s.extractWebhookMedia(messageMap, img, "image")
		return mapString(img, "caption", "Caption"), mediaType, media
	}
	if video := mapFromAny(mapLookup(messageMap, "videoMessage", "VideoMessage")); video != nil {
		media, mediaType := s.extractWebhookMedia(messageMap, video, "video")
		return mapString(video, "caption", "Caption"), mediaType, media
	}
	if audio := mapFromAny(mapLookup(messageMap, "audioMessage", "AudioMessage")); audio != nil {
		media, mediaType := s.extractWebhookMedia(messageMap, audio, "audio")
		return "[audio]", mediaType, media
	}
	if doc := mapFromAny(mapLookup(messageMap, "documentMessage", "DocumentMessage")); doc != nil {
		media, mediaType := s.extractWebhookMedia(messageMap, doc, "document")
		content := firstNonEmptyString(
			mapString(doc, "caption", "Caption"),
			mapString(doc, "title", "Title"),
			mapString(doc, "fileName", "FileName", "filename"),
		)
		return content, mediaType, media
	}
	if sticker := mapFromAny(mapLookup(messageMap, "stickerMessage", "StickerMessage")); sticker != nil {
		media, mediaType := s.extractWebhookMedia(messageMap, sticker, "image")
		return "[sticker]", mediaType, media
	}

	media, mediaType := s.extractWebhookMedia(messageMap, nil, "")
	if len(media) > 0 || mediaType != "" {
		return fmt.Sprintf("[%s]", firstNonEmptyString(mediaType, "media")), mediaType, media
	}

	return "[message]", "", nil
}

func (s *chatwootService) resolveEvolutionRemoteJID(payload evolutionWebhookPayload, data map[string]interface{}, info map[string]interface{}) string {
	remoteJID := normalizeRemoteJID(mapString(info, "Chat", "chat", "RemoteJID", "remoteJid"))
	if remoteJID == "" {
		remoteJID = normalizeRemoteJID(mapString(data, "remoteJid", "remoteJID", "jid"))
	}
	if !isLIDRemoteJID(remoteJID) {
		return remoteJID
	}

	if pnJID := firstPhoneRemoteJID(
		mapString(info, "SenderAlt", "senderAlt"),
		mapString(info, "RecipientAlt", "recipientAlt"),
		mapString(info, "Sender", "sender"),
		mapString(data, "SenderAlt", "senderAlt"),
		mapString(data, "RecipientAlt", "recipientAlt"),
		mapString(data, "Sender", "sender"),
	); pnJID != "" {
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

func (s *chatwootService) extractWebhookMedia(root map[string]interface{}, mediaMap map[string]interface{}, defaultType string) ([]byte, string) {
	mimeType := firstNonEmptyString(
		mapString(root, "mimetype", "mimeType", "MimeType"),
		mapString(mediaMap, "mimetype", "mimeType", "MimeType"),
	)
	mediaType := defaultType
	if mediaType == "" && mimeType != "" {
		mediaType = normalizeChatwootFileType("", mimeType)
	}

	encoded := firstNonEmptyString(
		mapString(root, "base64", "Base64"),
		mapString(mediaMap, "base64", "Base64"),
	)
	if encoded != "" {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(encoded)), "data:") {
			data, detectedMime, err := decodeDataURL(encoded)
			if err == nil {
				if mediaType == "" {
					mediaType = normalizeChatwootFileType("", firstNonEmptyString(detectedMime, mimeType))
				}
				return data, mediaType
			}
			return nil, mediaType
		}

		data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
		if err == nil {
			if mediaType == "" {
				mediaType = "document"
			}
			return data, mediaType
		}
		return nil, mediaType
	}

	mediaURL := firstNonEmptyString(
		mapString(root, "mediaUrl", "mediaURL"),
		mapString(mediaMap, "mediaUrl", "mediaURL"),
	)
	if mediaURL == "" {
		return nil, mediaType
	}

	data, detectedMime, err := s.downloadAttachmentData(mediaURL)
	if err != nil {
		return nil, mediaType
	}
	if mediaType == "" {
		mediaType = normalizeChatwootFileType("", firstNonEmptyString(detectedMime, mimeType))
	}
	return data, mediaType
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
	instanceRepository instance_repository.InstanceRepository,
	sendMessageService send_service.SendService,
	lidResolver func(instanceID string, lidJID string) (string, bool),
	loggerWrapper *logger_wrapper.LoggerManager,
) ChatwootService {
	service := &chatwootService{
		repository:         repository,
		instanceRepository: instanceRepository,
		sendMessageService: sendMessageService,
		lidResolver:        lidResolver,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
		skipCache:     cache.New(10*time.Minute, 20*time.Minute),
		webhookCache:  cache.New(24*time.Hour, 1*time.Hour),
		eventQueue:    make(chan chatwootEvent, 20000),
		loggerWrapper: loggerWrapper,
	}

	for i := 0; i < 4; i++ {
		go service.runEventWorker()
	}

	return service
}
