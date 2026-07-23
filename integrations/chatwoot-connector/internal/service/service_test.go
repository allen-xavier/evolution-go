package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/allen-xavier/evolution-go-chatwoot-connector/internal/evolution"
	"github.com/allen-xavier/evolution-go-chatwoot-connector/internal/logging"
	"github.com/allen-xavier/evolution-go-chatwoot-connector/internal/model"
)

type outboundFakeEvolution struct {
	sendTextErr error
	textCalls   int
}

func (f *outboundFakeEvolution) GetInstance(context.Context, string) (*evolution.Instance, error) {
	return &evolution.Instance{Id: "eef4c22f-766f-4c77-a376-52219f57adfc", Token: "token", Connected: true}, nil
}

func (*outboundFakeEvolution) ListInstances(context.Context) ([]evolution.Instance, error) {
	return nil, nil
}

func (f *outboundFakeEvolution) SendText(context.Context, *evolution.Instance, evolution.TextRequest) error {
	f.textCalls++
	return f.sendTextErr
}

func (*outboundFakeEvolution) SendMedia(context.Context, *evolution.Instance, evolution.MediaRequest) error {
	return nil
}

func (*outboundFakeEvolution) SetProxy(context.Context, string, evolution.ProxyConfig) error {
	return nil
}

func (*outboundFakeEvolution) RemoveProxy(context.Context, string) error {
	return nil
}

func (*outboundFakeEvolution) DisconnectInstance(context.Context, string) error {
	return nil
}

type identityMemoryRepository struct {
	configs  map[string]*model.ChatwootConfig
	bindings map[string]*model.ChatwootBinding
	aliases  map[string]string
	deleted  []uint
	jobs     map[uint]*model.ChatwootOutboundJob
	nextJob  uint
}

func identityKey(instanceID string, jid string) string {
	return instanceID + "|" + normalizeRemoteJID(jid)
}

func (r *identityMemoryRepository) SaveConfig(config *model.ChatwootConfig) error {
	if r.configs == nil {
		r.configs = map[string]*model.ChatwootConfig{}
	}
	r.configs[config.InstanceID] = config
	return nil
}

func (r *identityMemoryRepository) GetConfig(instanceID string) (*model.ChatwootConfig, error) {
	return r.configs[instanceID], nil
}

func (r *identityMemoryRepository) SaveBinding(binding *model.ChatwootBinding) error {
	if r.bindings == nil {
		r.bindings = map[string]*model.ChatwootBinding{}
	}
	for key, current := range r.bindings {
		if current.ID == binding.ID {
			delete(r.bindings, key)
		}
	}
	r.bindings[identityKey(binding.InstanceID, binding.RemoteJID)] = binding
	return nil
}

func (r *identityMemoryRepository) DeleteBinding(binding *model.ChatwootBinding) error {
	delete(r.bindings, identityKey(binding.InstanceID, binding.RemoteJID))
	r.deleted = append(r.deleted, binding.ID)
	return nil
}

func (r *identityMemoryRepository) GetBindingByRemoteJID(instanceID string, remoteJID string) (*model.ChatwootBinding, error) {
	return r.bindings[identityKey(instanceID, remoteJID)], nil
}

func (r *identityMemoryRepository) GetBindingByConversationID(instanceID string, conversationID int) (*model.ChatwootBinding, error) {
	for _, binding := range r.bindings {
		if binding.InstanceID == instanceID && binding.ConversationID == conversationID {
			return binding, nil
		}
	}
	return nil, nil
}

func (r *identityMemoryRepository) SaveIdentityAlias(instanceID string, aliasJID string, canonicalJID string) error {
	if r.aliases == nil {
		r.aliases = map[string]string{}
	}
	r.aliases[identityKey(instanceID, aliasJID)] = normalizeRemoteJID(canonicalJID)
	return nil
}

func (r *identityMemoryRepository) ResolveIdentityAlias(instanceID string, aliasJID string) (string, error) {
	return r.aliases[identityKey(instanceID, aliasJID)], nil
}

func (r *identityMemoryRepository) EnqueueOutboundJob(job *model.ChatwootOutboundJob) error {
	if r.jobs == nil {
		r.jobs = map[uint]*model.ChatwootOutboundJob{}
	}
	for _, current := range r.jobs {
		if current.InstanceID == job.InstanceID && current.ChatwootMessageID == job.ChatwootMessageID {
			current.Payload = append([]byte(nil), job.Payload...)
			current.NextAttemptAt = job.NextAttemptAt
			current.LastError = job.LastError
			return nil
		}
	}
	r.nextJob++
	job.ID = r.nextJob
	copyJob := *job
	copyJob.Payload = append([]byte(nil), job.Payload...)
	r.jobs[job.ID] = &copyJob
	return nil
}

func (r *identityMemoryRepository) ListDueOutboundJobs(now time.Time, limit int) ([]model.ChatwootOutboundJob, error) {
	result := []model.ChatwootOutboundJob{}
	for _, job := range r.jobs {
		if !job.NextAttemptAt.After(now) {
			result = append(result, *job)
		}
	}
	return result, nil
}

func (r *identityMemoryRepository) SaveOutboundJob(job *model.ChatwootOutboundJob) error {
	copyJob := *job
	r.jobs[job.ID] = &copyJob
	return nil
}

func (r *identityMemoryRepository) DeleteOutboundJob(job *model.ChatwootOutboundJob) error {
	delete(r.jobs, job.ID)
	return nil
}

func testLoggerManager() *logging.Manager {
	return logging.New(slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestExtractEvolutionTextMessage(t *testing.T) {
	service := &chatwootService{
		httpClient: &http.Client{Timeout: time.Second},
	}

	raw := []byte(`{
		"event": "Message",
		"data": {
			"Info": {
				"Chat": "553193291010@s.whatsapp.net",
				"ID": "ABC123",
				"IsFromMe": false,
				"PushName": "Cliente"
			},
			"Message": {
				"conversation": "ola"
			}
		}
	}`)

	var payload evolutionWebhookPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}

	msg, ok := service.extractEvolutionMessage(payload, raw)
	if !ok {
		t.Fatal("expected message to be extracted")
	}
	if msg.RemoteJID != "553193291010@s.whatsapp.net" {
		t.Fatalf("unexpected remote jid: %s", msg.RemoteJID)
	}
	if msg.Content != "ola" {
		t.Fatalf("unexpected content: %s", msg.Content)
	}
	if msg.MessageSourceID != "wa-in:ABC123" {
		t.Fatalf("unexpected source id: %s", msg.MessageSourceID)
	}
	if msg.FromMe {
		t.Fatal("expected incoming message")
	}
}

func TestExtractEvolutionUnavailableMessage(t *testing.T) {
	service := &chatwootService{
		httpClient: &http.Client{Timeout: time.Second},
	}
	raw := []byte(`{
		"event": "Message",
		"instanceId": "eef4c22f-766f-4c77-a376-52219f57adfc",
		"data": {
			"Info": {
				"Chat": "90465080737994@lid",
				"SenderAlt": "5516991635281@s.whatsapp.net",
				"ID": "UNAVAILABLE123",
				"IsFromMe": false
			},
			"IsUnavailable": true
		}
	}`)

	var payload evolutionWebhookPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	message, ok := service.extractEvolutionMessage(payload, raw)
	if !ok {
		t.Fatal("expected unavailable event to retain its identity")
	}
	if !message.Unavailable {
		t.Fatal("expected unavailable marker to be preserved")
	}
	if message.RemoteJID != "5516991635281@s.whatsapp.net" {
		t.Fatalf("unexpected unavailable remote jid: %s", message.RemoteJID)
	}
}

func TestExtractEvolutionMediaMessageWithBase64(t *testing.T) {
	service := &chatwootService{
		httpClient: &http.Client{Timeout: time.Second},
	}

	encoded := base64.StdEncoding.EncodeToString([]byte("image-bytes"))
	raw := []byte(`{
		"event": "Message",
		"data": {
			"Info": {
				"Chat": "553193291010@s.whatsapp.net",
				"ID": "IMG123",
				"IsFromMe": false
			},
			"Message": {
				"base64": "` + encoded + `",
				"imageMessage": {
					"caption": "foto",
					"mimetype": "image/jpeg"
				}
			}
		}
	}`)

	var payload evolutionWebhookPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}

	msg, ok := service.extractEvolutionMessage(payload, raw)
	if !ok {
		t.Fatal("expected message to be extracted")
	}
	if msg.Content != "foto" {
		t.Fatalf("unexpected content: %s", msg.Content)
	}
	if msg.Media.FileType != "image" {
		t.Fatalf("unexpected media type: %s", msg.Media.FileType)
	}
	if msg.Media.MIMEType != "image/jpeg" {
		t.Fatalf("unexpected media mime: %s", msg.Media.MIMEType)
	}
	if string(msg.Media.Data) != "image-bytes" {
		t.Fatalf("unexpected media bytes: %q", string(msg.Media.Data))
	}
}

func TestNormalizeMediaAttachmentRepairsMIMEAndExtension(t *testing.T) {
	tests := []struct {
		name          string
		attachment    mediaAttachment
		wantMIME      string
		wantType      string
		wantExtension string
	}{
		{
			name: "whatsapp ogg voice note",
			attachment: mediaAttachment{
				Data:     append([]byte("OggS\x00"), make([]byte, 32)...),
				FileType: "audio",
				MIMEType: "application/octet-stream",
			},
			wantMIME:      "audio/ogg",
			wantType:      "audio",
			wantExtension: ".ogg",
		},
		{
			name: "jpeg without extension",
			attachment: mediaAttachment{
				Data:     []byte{0xff, 0xd8, 0xff, 0xe0, 0x00, 0x10, 'J', 'F', 'I', 'F', 0x00},
				FileType: "image",
				MIMEType: "application/octet-stream",
				FileName: "foto",
			},
			wantMIME:      "image/jpeg",
			wantType:      "image",
			wantExtension: ".jpg",
		},
		{
			name: "pdf with wrong mime and extension",
			attachment: mediaAttachment{
				Data:     []byte("%PDF-1.7\n1 0 obj\n<<>>\nendobj\n"),
				FileType: "document",
				MIMEType: "image/jpeg",
				FileName: "contrato.bin",
			},
			wantMIME:      "application/pdf",
			wantType:      "document",
			wantExtension: ".pdf",
		},
		{
			name: "docx preserves declared container mime",
			attachment: mediaAttachment{
				Data:     []byte{'P', 'K', 0x03, 0x04, 0x14, 0x00, 0x00, 0x00},
				FileType: "document",
				MIMEType: "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
				FileName: "proposta",
			},
			wantMIME:      "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
			wantType:      "document",
			wantExtension: ".docx",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeMediaAttachment(tt.attachment)
			if got.MIMEType != tt.wantMIME {
				t.Fatalf("unexpected mime: got %q want %q", got.MIMEType, tt.wantMIME)
			}
			if got.FileType != tt.wantType {
				t.Fatalf("unexpected file type: got %q want %q", got.FileType, tt.wantType)
			}
			if !strings.HasSuffix(strings.ToLower(got.FileName), tt.wantExtension) {
				t.Fatalf("unexpected filename: got %q, expected extension %q", got.FileName, tt.wantExtension)
			}
		})
	}
}

func TestChatwootMultipartSendsNormalizedAttachmentHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("failed to parse multipart: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		file, header, err := r.FormFile("attachments[]")
		if err != nil {
			t.Errorf("missing attachment: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		defer file.Close()
		data, _ := io.ReadAll(file)
		if !strings.HasSuffix(header.Filename, ".ogg") {
			t.Errorf("unexpected filename: %q", header.Filename)
		}
		if got := header.Header.Get("Content-Type"); got != "audio/ogg" {
			t.Errorf("unexpected part content type: %q", got)
		}
		if got := r.FormValue("file_type"); got != "audio" {
			t.Errorf("unexpected chatwoot file type: %q", got)
		}
		if !strings.HasPrefix(string(data), "OggS") {
			t.Errorf("unexpected attachment data")
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	service := &chatwootService{httpClient: server.Client()}
	err := service.chatwootRequestMultipart(
		&model.ChatwootConfig{URL: server.URL, Token: "token"},
		"/messages",
		"",
		"incoming",
		"wa-in:AUDIO1",
		mediaAttachment{
			Data:     append([]byte("OggS\x00"), make([]byte, 32)...),
			FileType: "audio",
			MIMEType: "application/octet-stream",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
}

func TestMediaWithoutCaptionDoesNotCreateSyntheticText(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString(append([]byte("OggS\x00"), make([]byte, 16)...))
	tests := []struct {
		name       string
		messageKey string
		mimeType   string
		fileType   string
	}{
		{name: "audio", messageKey: "audioMessage", mimeType: "audio/ogg; codecs=opus", fileType: "audio"},
		{name: "image", messageKey: "imageMessage", mimeType: "image/jpeg", fileType: "image"},
		{name: "sticker", messageKey: "stickerMessage", mimeType: "image/webp", fileType: "image"},
	}

	service := &chatwootService{httpClient: &http.Client{Timeout: time.Second}}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			message := map[string]interface{}{
				"base64": encoded,
				tt.messageKey: map[string]interface{}{
					"mimetype": tt.mimeType,
				},
			}
			content, attachment := service.extractContentAndMedia(message)
			if content != "" {
				t.Fatalf("unexpected synthetic content: %q", content)
			}
			if attachment.FileType != tt.fileType {
				t.Fatalf("unexpected file type: got %q want %q", attachment.FileType, tt.fileType)
			}
		})
	}
}

func TestChatwootSourceIDAndRecipientForSend(t *testing.T) {
	instanceID := "b8592312-2083-476e-879c-509e68b7b337"
	remoteJID := "553193291010@s.whatsapp.net"
	sourceID := chatwootSourceID(instanceID, remoteJID)

	if got := remoteJIDFromSourceID(instanceID, sourceID); got != remoteJID {
		t.Fatalf("unexpected remote jid from source id: %s", got)
	}

	number, formatJID := recipientForSend(remoteJID)
	if number != "553193291010" {
		t.Fatalf("unexpected recipient number: %s", number)
	}
	if formatJID != nil {
		t.Fatal("expected nil formatJid for normal phone sends")
	}

	lid := "28462999949545@lid"
	lidSourceID := chatwootSourceID(instanceID, lid)
	expectedLIDSourceID := instanceID + ":lid:" + lid
	if lidSourceID != expectedLIDSourceID {
		t.Fatalf("unexpected lid source id: %s", lidSourceID)
	}
	if got := remoteJIDFromSourceID(instanceID, lidSourceID); got != lid {
		t.Fatalf("unexpected lid from source id: %s", got)
	}

	number, formatJID = recipientForSend(lid)
	if number != lid {
		t.Fatalf("unexpected lid recipient: %s", number)
	}
	if formatJID == nil || *formatJID {
		t.Fatal("expected formatJid=false for lid sends")
	}
}

func TestBuildChatwootContactRefDoesNotUseLIDAsPhone(t *testing.T) {
	instanceID := "eef4c22f-766f-4c77-a376-52219f57adfc"
	lid := "90465080737994@lid"

	ref := buildChatwootContactRef(instanceID, lid, "", true)
	if ref.Phone != "" {
		t.Fatalf("lid must not be used as phone number, got: %s", ref.Phone)
	}
	if ref.Identifier != instanceID+":lid:"+lid {
		t.Fatalf("unexpected lid identifier: %s", ref.Identifier)
	}
	if ref.SourceID != ref.Identifier {
		t.Fatalf("lid source id and identifier should match, got source=%s identifier=%s", ref.SourceID, ref.Identifier)
	}
	if ref.Name != lid {
		t.Fatalf("unexpected lid fallback name: %s", ref.Name)
	}

	phoneRef := buildChatwootContactRef(instanceID, "553193291010@s.whatsapp.net", "", true)
	if phoneRef.Phone == "" {
		t.Fatal("expected real WhatsApp JID to produce phone number")
	}
	if phoneRef.Identifier != "553193291010@s.whatsapp.net" {
		t.Fatalf("unexpected phone identifier: %s", phoneRef.Identifier)
	}
}

func TestResolveEvolutionRemoteJIDUsesSenderAltPhoneBeforeLID(t *testing.T) {
	service := &chatwootService{
		httpClient: &http.Client{Timeout: time.Second},
	}

	raw := []byte(`{
		"event": "Message",
		"instanceId": "eef4c22f-766f-4c77-a376-52219f57adfc",
		"data": {
			"Info": {
				"Chat": "90465080737994@lid",
				"Sender": "90465080737994@lid",
				"SenderAlt": "5516991635281@s.whatsapp.net",
				"ID": "LID123",
				"IsFromMe": false
			},
			"Message": {
				"conversation": "ola"
			}
		}
	}`)

	var payload evolutionWebhookPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}

	msg, ok := service.extractEvolutionMessage(payload, raw)
	if !ok {
		t.Fatal("expected message to be extracted")
	}
	if msg.RemoteJID != "5516991635281@s.whatsapp.net" {
		t.Fatalf("unexpected remote jid: %s", msg.RemoteJID)
	}
}

func TestResolveOutgoingLIDUsesRecipientAndNeverOwnSender(t *testing.T) {
	service := &chatwootService{
		httpClient: &http.Client{Timeout: time.Second},
	}
	raw := []byte(`{
		"event": "SendMessage",
		"instanceId": "eef4c22f-766f-4c77-a376-52219f57adfc",
		"data": {
			"Info": {
				"Chat": "90465080737994@lid",
				"Sender": "11111111111111@lid",
				"SenderAlt": "5511000000000@s.whatsapp.net",
				"RecipientAlt": "5516991635281@s.whatsapp.net",
				"ID": "LID-OUT-1",
				"IsFromMe": true
			},
			"Message": {
				"conversation": "resposta"
			}
		}
	}`)

	var payload evolutionWebhookPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	message, ok := service.extractEvolutionMessage(payload, raw)
	if !ok {
		t.Fatal("expected message to be extracted")
	}
	if message.RemoteJID != "5516991635281@s.whatsapp.net" {
		t.Fatalf("outgoing remote jid = %s, want recipient phone", message.RemoteJID)
	}
	if containsJID(message.IdentityAliases, "11111111111111@lid") {
		t.Fatalf("own sender LID must not be associated with customer: %#v", message.IdentityAliases)
	}
	if !containsJID(message.IdentityAliases, "90465080737994@lid") {
		t.Fatalf("customer chat LID was not captured: %#v", message.IdentityAliases)
	}
}

func TestResolveEvolutionRemoteJIDUsesLIDResolver(t *testing.T) {
	service := &chatwootService{
		httpClient: &http.Client{Timeout: time.Second},
		lidResolver: func(instanceID string, lidJID string) (string, bool) {
			if instanceID != "eef4c22f-766f-4c77-a376-52219f57adfc" || lidJID != "90465080737994@lid" {
				return "", false
			}
			return "5516991635281@s.whatsapp.net", true
		},
	}

	raw := []byte(`{
		"event": "Message",
		"instanceId": "eef4c22f-766f-4c77-a376-52219f57adfc",
		"data": {
			"Info": {
				"Chat": "90465080737994@lid",
				"Sender": "90465080737994@lid",
				"ID": "LID123",
				"IsFromMe": false
			},
			"Message": {
				"conversation": "ola"
			}
		}
	}`)

	var payload evolutionWebhookPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}

	msg, ok := service.extractEvolutionMessage(payload, raw)
	if !ok {
		t.Fatal("expected message to be extracted")
	}
	if msg.RemoteJID != "5516991635281@s.whatsapp.net" {
		t.Fatalf("unexpected remote jid: %s", msg.RemoteJID)
	}
}

func TestCanonicalIdentityPersistsLIDAndReusesPhone(t *testing.T) {
	const (
		instanceID = "eef4c22f-766f-4c77-a376-52219f57adfc"
		lidJID     = "90465080737994@lid"
		phoneJID   = "5516991635281@s.whatsapp.net"
	)
	repository := &identityMemoryRepository{}
	service := &chatwootService{
		repository:    repository,
		loggerWrapper: testLoggerManager(),
	}

	messageWithPhone := evolutionMessage{
		RemoteJID:       phoneJID,
		IdentityAliases: []string{lidJID},
	}
	if err := service.canonicalizeMessageIdentity(instanceID, &messageWithPhone); err != nil {
		t.Fatal(err)
	}

	lidOnlyMessage := evolutionMessage{
		RemoteJID:       lidJID,
		IdentityAliases: []string{lidJID},
	}
	if err := service.canonicalizeMessageIdentity(instanceID, &lidOnlyMessage); err != nil {
		t.Fatal(err)
	}
	if lidOnlyMessage.RemoteJID != phoneJID {
		t.Fatalf("LID resolved to %s, want %s", lidOnlyMessage.RemoteJID, phoneJID)
	}
}

func TestPhoneIdentityMigratesProvisionalLIDConversation(t *testing.T) {
	const (
		instanceID = "eef4c22f-766f-4c77-a376-52219f57adfc"
		lidJID     = "90465080737994@lid"
		phoneJID   = "5516991635281@s.whatsapp.net"
	)
	provisional := &model.ChatwootBinding{
		ID:             7,
		InstanceID:     instanceID,
		RemoteJID:      lidJID,
		ConversationID: 91,
		SourceID:       chatwootSourceID(instanceID, lidJID),
	}
	repository := &identityMemoryRepository{
		bindings: map[string]*model.ChatwootBinding{
			identityKey(instanceID, lidJID): provisional,
		},
	}
	service := &chatwootService{
		repository:    repository,
		loggerWrapper: testLoggerManager(),
	}

	binding, err := service.getOrCreateBindingByRemote(
		&evolution.Instance{Id: instanceID},
		&model.ChatwootConfig{InstanceID: instanceID, MergeBrazilContacts: true},
		phoneJID,
		[]string{lidJID},
		"Cliente",
		false,
	)
	if err != nil {
		t.Fatal(err)
	}
	if binding.ID != provisional.ID || binding.ConversationID != provisional.ConversationID {
		t.Fatalf("provisional conversation was not reused: %#v", binding)
	}
	if binding.RemoteJID != phoneJID {
		t.Fatalf("binding remote jid = %s, want %s", binding.RemoteJID, phoneJID)
	}
	if old, _ := repository.GetBindingByRemoteJID(instanceID, lidJID); old != nil {
		t.Fatal("old LID binding key should have been migrated")
	}
}

func TestCanonicalBindingRemovesDuplicateLIDBinding(t *testing.T) {
	const (
		instanceID = "eef4c22f-766f-4c77-a376-52219f57adfc"
		lidJID     = "90465080737994@lid"
		phoneJID   = "5516991635281@s.whatsapp.net"
	)
	canonical := &model.ChatwootBinding{
		ID:             10,
		InstanceID:     instanceID,
		RemoteJID:      phoneJID,
		ConversationID: 100,
	}
	duplicate := &model.ChatwootBinding{
		ID:             11,
		InstanceID:     instanceID,
		RemoteJID:      lidJID,
		ConversationID: 101,
	}
	repository := &identityMemoryRepository{
		bindings: map[string]*model.ChatwootBinding{
			identityKey(instanceID, phoneJID): canonical,
			identityKey(instanceID, lidJID):   duplicate,
		},
	}
	service := &chatwootService{
		repository:    repository,
		loggerWrapper: testLoggerManager(),
	}

	binding, err := service.getOrCreateBindingByRemote(
		&evolution.Instance{Id: instanceID},
		&model.ChatwootConfig{InstanceID: instanceID},
		phoneJID,
		[]string{lidJID},
		"Cliente",
		false,
	)
	if err != nil {
		t.Fatal(err)
	}
	if binding.ID != canonical.ID {
		t.Fatalf("got binding %d, want canonical %d", binding.ID, canonical.ID)
	}
	if len(repository.deleted) != 1 || repository.deleted[0] != duplicate.ID {
		t.Fatalf("duplicate binding was not removed: %#v", repository.deleted)
	}
}

func TestFailedChatwootSendIsPersistedAndRetried(t *testing.T) {
	const (
		instanceID = "eef4c22f-766f-4c77-a376-52219f57adfc"
		remoteJID  = "5516991635281@s.whatsapp.net"
	)
	repository := &identityMemoryRepository{
		configs: map[string]*model.ChatwootConfig{
			instanceID: {
				InstanceID: instanceID,
				Enabled:    true,
				InboxID:    7,
			},
		},
		bindings: map[string]*model.ChatwootBinding{
			identityKey(instanceID, remoteJID): {
				ID:             1,
				InstanceID:     instanceID,
				RemoteJID:      remoteJID,
				ConversationID: 91,
			},
		},
	}
	evolutionAPI := &outboundFakeEvolution{sendTextErr: errors.New("server returned error 463")}
	service := NewChatwootService(repository, evolutionAPI, nil, testLoggerManager()).(*chatwootService)

	body := []byte(`{
		"event": "message_created",
		"id": 249999,
		"content": "mensagem importante",
		"message_type": "outgoing",
		"private": false,
		"conversation": {
			"id": 91,
			"inbox_id": 7,
			"contact_inbox": {"source_id": "unused"}
		},
		"sender": {"type": "user", "name": "Atendente"}
	}`)
	if err := service.HandleWebhook(instanceID, nil, body); err != nil {
		t.Fatalf("durably queued webhook should return success, got: %v", err)
	}
	if len(repository.jobs) != 1 {
		t.Fatalf("expected one durable outbound job, got %d", len(repository.jobs))
	}

	var job *model.ChatwootOutboundJob
	for _, stored := range repository.jobs {
		copyJob := *stored
		job = &copyJob
	}
	if job == nil || job.ChatwootMessageID != "249999" {
		t.Fatalf("unexpected durable job: %#v", job)
	}

	evolutionAPI.sendTextErr = nil
	service.processOutboundJob(job)
	if len(repository.jobs) != 0 {
		t.Fatalf("successful retry should delete durable job: %#v", repository.jobs)
	}
	if evolutionAPI.textCalls != 2 {
		t.Fatalf("expected initial send and durable retry, got %d calls", evolutionAPI.textCalls)
	}
}

func TestOutboundRetryDelayIsBounded(t *testing.T) {
	if got := outboundRetryDelay(1); got != 15*time.Second {
		t.Fatalf("first retry delay = %v", got)
	}
	if got := outboundRetryDelay(20); got != 5*time.Minute {
		t.Fatalf("maximum retry delay = %v", got)
	}
}

func TestToE164DoesNotRemoveBrazilNinthDigitForDDD16(t *testing.T) {
	got := toE164("5516991635281@s.whatsapp.net", true)
	if got != "+5516991635281" {
		t.Fatalf("unexpected ddd16 phone: %s", got)
	}
}

func TestParseChatwootContactIDSupportsArrayPayload(t *testing.T) {
	body := []byte(`{"payload":[{"id":321}]}`)
	id, err := parseChatwootContactID(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != 321 {
		t.Fatalf("unexpected id: %d", id)
	}
}

func TestParseChatwootContactIDSupportsObjectPayload(t *testing.T) {
	body := []byte(`{"payload":{"contact":{"id":654}}}`)
	id, err := parseChatwootContactID(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != 654 {
		t.Fatalf("unexpected id: %d", id)
	}
}

func TestParseChatwootContactIDSupportsTopLevelID(t *testing.T) {
	body := []byte(`{"id":"987"}`)
	id, err := parseChatwootContactID(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != 987 {
		t.Fatalf("unexpected id: %d", id)
	}
}
