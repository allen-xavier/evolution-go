package service

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/allen-xavier/evolution-go-chatwoot-connector/internal/evolution"
	"github.com/allen-xavier/evolution-go-chatwoot-connector/internal/model"
)

const (
	deleteTestInstance = "eef4c22f-766f-4c77-a376-52219f57adfc"
	// Contact saved with the ninth digit; WhatsApp knows the account without it.
	deleteTestRemote = "5531999990986@s.whatsapp.net"
	deleteTestJID    = "553199990986@s.whatsapp.net"
)

func deleteTestSetup(t *testing.T) (*chatwootService, *identityMemoryRepository, *outboundFakeEvolution, *chatwootNoteRecorder) {
	t.Helper()
	notes := newChatwootNoteRecorder(t, 91)
	repository := &identityMemoryRepository{
		configs: map[string]*model.ChatwootConfig{
			deleteTestInstance: {InstanceID: deleteTestInstance, Enabled: true, InboxID: 7, AccountID: "1", URL: notes.server.URL, Token: "t"},
		},
		bindings: map[string]*model.ChatwootBinding{
			identityKey(deleteTestInstance, deleteTestRemote): {ID: 1, InstanceID: deleteTestInstance, RemoteJID: deleteTestRemote, ConversationID: 91},
		},
		jobs: map[uint]*model.ChatwootOutboundJob{},
	}
	evolutionAPI := &outboundFakeEvolution{
		numberChecks: []evolution.NumberCheck{{Query: "5531999990986", OnWhatsApp: true, JID: deleteTestJID, RemoteJID: deleteTestJID}},
	}
	service := NewChatwootService(repository, evolutionAPI, nil, testLoggerManager()).(*chatwootService)
	return service, repository, evolutionAPI, notes
}

func messageUpdatedWebhook(t *testing.T, messageID int, change func(map[string]interface{})) []byte {
	t.Helper()
	payload := map[string]interface{}{
		"event":              "message_updated",
		"id":                 messageID,
		"content":            "This message was deleted",
		"message_type":       "outgoing",
		"private":            false,
		"content_attributes": map[string]interface{}{"deleted": true},
		"created_at":         time.Now().Add(-time.Hour).UTC().Format(time.RFC3339Nano),
		"source_id":          nil,
		"conversation": map[string]interface{}{
			"id":            91,
			"inbox_id":      7,
			"contact_inbox": map[string]interface{}{"source_id": "unused"},
		},
		"sender": map[string]interface{}{"type": "user", "name": "Atendente"},
	}
	if change != nil {
		change(payload)
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestDeletedReplyIsDeletedForEveryoneOnWhatsApp(t *testing.T) {
	service, repository, evolutionAPI, notes := deleteTestSetup(t)
	textID := chatwootOutboundMessageID(deleteTestInstance, "700", "text")
	mediaID := chatwootOutboundMessageID(deleteTestInstance, "700", "media:https://chatwoot/file.jpg")
	service.rememberMessageReceipt(deleteTestInstance, textID, 91, "700")
	service.rememberMessageReceipt(deleteTestInstance, mediaID, 91, "700")
	// Another message of the same conversation stays.
	service.rememberMessageReceipt(deleteTestInstance, chatwootOutboundMessageID(deleteTestInstance, "701", "text"), 91, "701")

	if err := service.HandleWebhook(deleteTestInstance, nil, messageUpdatedWebhook(t, 700, nil)); err != nil {
		t.Fatal(err)
	}

	want := []evolution.DeleteMessageRequest{
		{Chat: deleteTestJID, MessageID: textID},
		{Chat: deleteTestJID, MessageID: mediaID},
	}
	if len(evolutionAPI.deleteCalls) != len(want) {
		t.Fatalf("delete calls = %#v, want %#v", evolutionAPI.deleteCalls, want)
	}
	for i := range want {
		if evolutionAPI.deleteCalls[i] != want[i] {
			t.Fatalf("delete call %d = %#v, want %#v", i, evolutionAPI.deleteCalls[i], want[i])
		}
	}
	if len(notes.notes) != 0 {
		t.Fatalf("a successful delete needs no note, got %q", notes.notes)
	}
	for _, id := range []string{textID, mediaID} {
		if receipt, _ := repository.GetMessageReceipt(deleteTestInstance, id); receipt == nil || receipt.Status != receiptStatusDeleted {
			t.Fatalf("receipt %s not marked deleted: %#v", id, receipt)
		}
	}
	if !service.deletedInChatwoot(deleteTestInstance, "700") || service.deletedInChatwoot(deleteTestInstance, "701") {
		t.Fatal("only message 700 is deleted")
	}
}

func TestDeletedReplyIsHandledOnce(t *testing.T) {
	service, repository, evolutionAPI, notes := deleteTestSetup(t)
	textID := chatwootOutboundMessageID(deleteTestInstance, "710", "text")
	service.rememberMessageReceipt(deleteTestInstance, textID, 91, "710")

	body := messageUpdatedWebhook(t, 710, nil)
	for i := 0; i < 3; i++ {
		if err := service.HandleWebhook(deleteTestInstance, nil, body); err != nil {
			t.Fatal(err)
		}
	}
	// After a restart (empty cache) the receipt says it is already deleted.
	restarted := NewChatwootService(repository, evolutionAPI, nil, testLoggerManager()).(*chatwootService)
	if err := restarted.HandleWebhook(deleteTestInstance, nil, body); err != nil {
		t.Fatal(err)
	}
	if len(evolutionAPI.deleteCalls) != 1 {
		t.Fatalf("expected one delete, got %#v", evolutionAPI.deleteCalls)
	}
	if len(notes.notes) != 0 {
		t.Fatalf("unexpected notes: %q", notes.notes)
	}
}

func TestDeletedReplyTooOldGetsNote(t *testing.T) {
	service, repository, evolutionAPI, notes := deleteTestSetup(t)
	textID := chatwootOutboundMessageID(deleteTestInstance, "720", "text")
	service.rememberMessageReceipt(deleteTestInstance, textID, 91, "720")
	repository.receipts[deleteTestInstance+"|"+textID].CreatedAt = time.Now().Add(-whatsAppRevokeWindow - time.Hour)

	if err := service.HandleWebhook(deleteTestInstance, nil, messageUpdatedWebhook(t, 720, nil)); err != nil {
		t.Fatal(err)
	}
	if len(evolutionAPI.deleteCalls) != 0 {
		t.Fatalf("a message older than the WhatsApp limit must not be revoked: %#v", evolutionAPI.deleteCalls)
	}
	if len(notes.notes) != 1 ||
		!strings.Contains(notes.notes[0], "apagada só no Chatwoot") ||
		!strings.Contains(notes.notes[0], "continua no WhatsApp do cliente") ||
		!strings.Contains(notes.notes[0], "prazo de 2 dias") {
		t.Fatalf("unexpected notes: %q", notes.notes)
	}
}

func TestDeletedReplyWithoutLinkGetsNote(t *testing.T) {
	service, _, evolutionAPI, notes := deleteTestSetup(t)

	if err := service.HandleWebhook(deleteTestInstance, nil, messageUpdatedWebhook(t, 730, nil)); err != nil {
		t.Fatal(err)
	}
	if len(evolutionAPI.deleteCalls) != 0 {
		t.Fatalf("unexpected delete calls: %#v", evolutionAPI.deleteCalls)
	}
	if len(notes.notes) != 1 || !strings.Contains(notes.notes[0], "sem vínculo") {
		t.Fatalf("unexpected notes: %q", notes.notes)
	}
}

func TestDeletedReplyRefusedByWhatsAppGetsNote(t *testing.T) {
	service, repository, evolutionAPI, notes := deleteTestSetup(t)
	evolutionAPI.deleteErr = errors.New("evolution request failed [500]: {\"error\":\"server returned error 479\"}")
	textID := chatwootOutboundMessageID(deleteTestInstance, "740", "text")
	service.rememberMessageReceipt(deleteTestInstance, textID, 91, "740")

	if err := service.HandleWebhook(deleteTestInstance, nil, messageUpdatedWebhook(t, 740, nil)); err != nil {
		t.Fatal(err)
	}
	if len(evolutionAPI.deleteCalls) != 1 {
		t.Fatalf("expected one delete attempt, got %#v", evolutionAPI.deleteCalls)
	}
	if len(notes.notes) != 1 || !strings.Contains(notes.notes[0], "recusou") {
		t.Fatalf("unexpected notes: %q", notes.notes)
	}
	if receipt, _ := repository.GetMessageReceipt(deleteTestInstance, textID); receipt.Status == receiptStatusDeleted {
		t.Fatal("a refused delete must not be marked as deleted")
	}
}

func TestDeletedMessageFromPhoneUsesSourceID(t *testing.T) {
	service, _, evolutionAPI, notes := deleteTestSetup(t)
	body := messageUpdatedWebhook(t, 750, func(payload map[string]interface{}) {
		payload["source_id"] = "wa-out:3EB0PHONE123"
	})

	if err := service.HandleWebhook(deleteTestInstance, nil, body); err != nil {
		t.Fatal(err)
	}
	if len(evolutionAPI.deleteCalls) != 1 || evolutionAPI.deleteCalls[0] != (evolution.DeleteMessageRequest{Chat: deleteTestJID, MessageID: "3EB0PHONE123"}) {
		t.Fatalf("unexpected delete calls: %#v", evolutionAPI.deleteCalls)
	}
	if len(notes.notes) != 0 {
		t.Fatalf("unexpected notes: %q", notes.notes)
	}

	old := messageUpdatedWebhook(t, 751, func(payload map[string]interface{}) {
		payload["source_id"] = "wa-out:3EB0PHONE456"
		payload["created_at"] = time.Now().Add(-72 * time.Hour).Unix()
	})
	if err := service.HandleWebhook(deleteTestInstance, nil, old); err != nil {
		t.Fatal(err)
	}
	if len(evolutionAPI.deleteCalls) != 1 || len(notes.notes) != 1 || !strings.Contains(notes.notes[0], "prazo") {
		t.Fatalf("old phone message: calls=%#v notes=%q", evolutionAPI.deleteCalls, notes.notes)
	}
}

func TestMessageUpdatedIgnoresOtherChanges(t *testing.T) {
	service, repository, evolutionAPI, notes := deleteTestSetup(t)
	for _, id := range []string{"760", "761", "762", "763", "764"} {
		service.rememberMessageReceipt(deleteTestInstance, chatwootOutboundMessageID(deleteTestInstance, id, "text"), 91, id)
	}
	cases := map[string][]byte{
		"incoming": messageUpdatedWebhook(t, 760, func(p map[string]interface{}) { p["message_type"] = "incoming" }),
		"private":  messageUpdatedWebhook(t, 761, func(p map[string]interface{}) { p["private"] = true }),
		"status": messageUpdatedWebhook(t, 762, func(p map[string]interface{}) {
			p["content"] = "Oi"
			p["content_attributes"] = map[string]interface{}{}
		}),
		"other inbox": messageUpdatedWebhook(t, 763, func(p map[string]interface{}) {
			p["conversation"].(map[string]interface{})["inbox_id"] = 8
		}),
		"bot without link": messageUpdatedWebhook(t, 769, func(p map[string]interface{}) {
			p["sender"] = map[string]interface{}{"type": "contact"}
		}),
	}
	for name, body := range cases {
		if err := service.HandleWebhook(deleteTestInstance, nil, body); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}

	// Chatwoot integration disabled for the instance.
	repository.configs[deleteTestInstance].Enabled = false
	if err := service.HandleWebhook(deleteTestInstance, nil, messageUpdatedWebhook(t, 764, nil)); err != nil {
		t.Fatal(err)
	}

	if len(evolutionAPI.deleteCalls) != 0 || len(notes.notes) != 0 {
		t.Fatalf("expected nothing, got calls=%#v notes=%q", evolutionAPI.deleteCalls, notes.notes)
	}
}

func TestDeletedReplyWaitingInQueueIsNotSent(t *testing.T) {
	service, repository, evolutionAPI, notes := deleteTestSetup(t)
	job := storedOutboundJob(t, repository, time.Now().Add(-5*time.Minute))
	service.rememberMessageReceipt(deleteTestInstance, chatwootOutboundMessageID(deleteTestInstance, job.ChatwootMessageID, "text"), 91, job.ChatwootMessageID)

	if err := service.HandleWebhook(deleteTestInstance, nil, messageUpdatedWebhook(t, 260001, nil)); err != nil {
		t.Fatal(err)
	}
	service.processOutboundJob(repository.jobs[job.ID])
	if evolutionAPI.textCalls != 0 {
		t.Fatalf("a deleted reply must not be sent, got %d sends", evolutionAPI.textCalls)
	}
	if stored := repository.jobs[job.ID]; stored == nil || stored.FailedAt == nil {
		t.Fatalf("the job should be closed: %#v", stored)
	}
	if len(notes.notes) != 0 {
		t.Fatalf("unexpected notes: %q", notes.notes)
	}
}

func TestDeletedReplyWaitingForDisconnectedNumberGetsNoNote(t *testing.T) {
	service, repository, evolutionAPI, notes := deleteTestSetup(t)
	evolutionAPI.disconnected = true
	job := storedOutboundJob(t, repository, time.Now().Add(-5*time.Minute))
	service.rememberMessageReceipt(deleteTestInstance, chatwootOutboundMessageID(deleteTestInstance, job.ChatwootMessageID, "text"), 91, job.ChatwootMessageID)

	if err := service.HandleWebhook(deleteTestInstance, nil, messageUpdatedWebhook(t, 260001, nil)); err != nil {
		t.Fatal(err)
	}
	if stored := repository.jobs[job.ID]; stored == nil || stored.FailedAt == nil {
		t.Fatalf("the queued reply should be cancelled: %#v", stored)
	}
	if len(evolutionAPI.deleteCalls) != 0 || len(notes.notes) != 0 {
		t.Fatalf("the reply never reached the customer: calls=%#v notes=%q", evolutionAPI.deleteCalls, notes.notes)
	}

	// Same situation without a queued reply: it is on the customer's phone.
	service.rememberMessageReceipt(deleteTestInstance, chatwootOutboundMessageID(deleteTestInstance, "780", "text"), 91, "780")
	if err := service.HandleWebhook(deleteTestInstance, nil, messageUpdatedWebhook(t, 780, nil)); err != nil {
		t.Fatal(err)
	}
	if len(notes.notes) != 1 || !strings.Contains(notes.notes[0], "desconectado") {
		t.Fatalf("unexpected notes: %q", notes.notes)
	}
}

func TestChatwootMessageDeletedAttribute(t *testing.T) {
	for raw, want := range map[string]bool{
		`{"deleted":true}`:          true,
		`"{\"deleted\":true}"`:      true,
		`{"deleted":"true"}`:        true,
		`{"deleted":false}`:         false,
		`{"in_reply_to":12}`:        false,
		`null`:                      false,
		``:                          false,
		`[]`:                        false,
		`{"deleted":true,"x":[1]}`:  true,
		`"not json"`:                false,
		`{"external_created_at":1}`: false,
	} {
		if got := chatwootMessageDeleted(json.RawMessage(raw)); got != want {
			t.Errorf("chatwootMessageDeleted(%s) = %v, want %v", raw, got, want)
		}
	}
	at := time.Date(2026, 11, 2, 15, 4, 5, 0, time.UTC)
	for _, raw := range []string{`"2026-11-02T15:04:05.000Z"`, `1793631845`, `"1793631845"`} {
		if got := chatwootTimestamp(json.RawMessage(raw)); !got.Equal(at) {
			t.Errorf("chatwootTimestamp(%s) = %s, want %s", raw, got, at)
		}
	}
}
