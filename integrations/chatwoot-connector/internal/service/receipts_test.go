package service

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/allen-xavier/evolution-go-chatwoot-connector/internal/model"
)

type receiptCall struct {
	method string
	path   string
	status string
}

func receiptTestService(t *testing.T, respond int) (*chatwootService, *identityMemoryRepository, *[]receiptCall, func()) {
	t.Helper()
	var mu sync.Mutex
	calls := []receiptCall{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		status, _ := body["status"].(string)
		mu.Lock()
		calls = append(calls, receiptCall{method: r.Method, path: r.URL.Path, status: status})
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(respond)
		_, _ = w.Write([]byte(`{}`))
	}))
	repo := &identityMemoryRepository{
		configs: map[string]*model.ChatwootConfig{
			"inst": {InstanceID: "inst", Enabled: true, URL: server.URL, Token: "token", AccountID: "1"},
		},
	}
	service := &chatwootService{
		repository:    repo,
		httpClient:    server.Client(),
		loggerWrapper: testLoggerManager(),
	}
	return service, repo, &calls, server.Close
}

func receiptEvent(state string, ids ...string) evolutionWebhookPayload {
	list := make([]interface{}, 0, len(ids))
	for _, id := range ids {
		list = append(list, id)
	}
	return evolutionWebhookPayload{
		Event:      "Receipt",
		State:      state,
		InstanceID: "inst",
		Data:       map[string]interface{}{"MessageIDs": list, "IsGroup": false},
	}
}

func TestReceiptMovesChatwootStatusForwardOnly(t *testing.T) {
	service, repo, calls, done := receiptTestService(t, http.StatusOK)
	defer done()

	waID := chatwootOutboundMessageID("inst", "431", "text")
	service.rememberMessageReceipt("inst", waID, 55, "431")

	if err := service.handleReceiptEvent(receiptEvent("Delivered", waID)); err != nil {
		t.Fatal(err)
	}
	if err := service.handleReceiptEvent(receiptEvent("Read", waID)); err != nil {
		t.Fatal(err)
	}
	// Late delivery receipt after read: must not move the status back.
	if err := service.handleReceiptEvent(receiptEvent("Delivered", waID)); err != nil {
		t.Fatal(err)
	}

	want := []receiptCall{
		{method: http.MethodPatch, path: "/api/v1/accounts/1/conversations/55/messages/431", status: "delivered"},
		{method: http.MethodPatch, path: "/api/v1/accounts/1/conversations/55/messages/431", status: "read"},
	}
	if len(*calls) != len(want) {
		t.Fatalf("unexpected Chatwoot calls: %#v", *calls)
	}
	for i := range want {
		if (*calls)[i] != want[i] {
			t.Fatalf("call %d = %#v, want %#v", i, (*calls)[i], want[i])
		}
	}
	saved, _ := repo.GetMessageReceipt("inst", waID)
	if saved == nil || saved.Status != "read" {
		t.Fatalf("receipt status not saved: %#v", saved)
	}
}

func TestReceiptIgnoresUnknownMessagesSelfReadAndGroups(t *testing.T) {
	service, _, calls, done := receiptTestService(t, http.StatusOK)
	defer done()

	waID := chatwootOutboundMessageID("inst", "500", "text")
	service.rememberMessageReceipt("inst", waID, 60, "500")

	group := receiptEvent("Read", waID)
	group.Data["IsGroup"] = true
	// The number's own phone confirming the copy it got (contact in airplane mode).
	ownDevice := receiptEvent("Delivered", waID)
	ownDevice.Data["IsFromMe"] = true
	ownDevice.Data["Sender"] = "5531988880986:0@s.whatsapp.net"
	for _, event := range []evolutionWebhookPayload{
		receiptEvent("Read", "MESSAGE-FROM-PHONE"), // not sent by the connector
		receiptEvent("ReadSelf", waID),             // this account read the contact
		group,
		ownDevice,
	} {
		if err := service.handleReceiptEvent(event); err != nil {
			t.Fatal(err)
		}
	}
	if len(*calls) != 0 {
		t.Fatalf("expected no Chatwoot calls, got %#v", *calls)
	}
}

func TestDeliveredOnlyFromContactPhoneButReadFromAnyDevice(t *testing.T) {
	service, _, calls, done := receiptTestService(t, http.StatusOK)
	defer done()

	waID := chatwootOutboundMessageID("inst", "412789", "text")
	service.rememberMessageReceipt("inst", waID, 90, "412789")

	// Contact's phone in airplane mode, WhatsApp Web (device 38) online: still one tick.
	web := receiptEvent("Delivered", waID)
	web.Data["Sender"] = "28462999949545:38@lid"
	if err := service.handleReceiptEvent(web); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 0 {
		t.Fatalf("delivery on a linked device must not count: %#v", *calls)
	}

	phone := receiptEvent("Delivered", waID)
	phone.Data["Sender"] = "28462999949545@lid"
	if err := service.handleReceiptEvent(phone); err != nil {
		t.Fatal(err)
	}
	readOnWeb := receiptEvent("Read", waID)
	readOnWeb.Data["Sender"] = "28462999949545:38@lid"
	if err := service.handleReceiptEvent(readOnWeb); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 2 || (*calls)[0].status != "delivered" || (*calls)[1].status != "read" {
		t.Fatalf("expected delivered (phone) then read (web): %#v", *calls)
	}
}

func TestReceiptDevice(t *testing.T) {
	for jid, want := range map[string]int{
		"553193291010@s.whatsapp.net":    0,
		"553193291010:0@s.whatsapp.net":  0,
		"28462999949545:38@lid":          38,
		"553193291010:12@s.whatsapp.net": 12,
		"":                               0,
	} {
		if got := receiptDevice(jid); got != want {
			t.Fatalf("receiptDevice(%q) = %d, want %d", jid, got, want)
		}
	}
}

func TestReceiptChatwootRefusalIsNotRetried(t *testing.T) {
	service, repo, _, done := receiptTestService(t, http.StatusNotFound)
	defer done()

	waID := chatwootOutboundMessageID("inst", "777", "text")
	service.rememberMessageReceipt("inst", waID, 70, "777")
	if err := service.handleReceiptEvent(receiptEvent("Read", waID)); err != nil {
		t.Fatalf("a 4xx from Chatwoot must not be retried: %v", err)
	}
	saved, _ := repo.GetMessageReceipt("inst", waID)
	if saved == nil || saved.Status != "sent" {
		t.Fatalf("status must stay as sent when Chatwoot refuses: %#v", saved)
	}
}

func TestHandleEvolutionEventRoutesReceipts(t *testing.T) {
	service, _, calls, done := receiptTestService(t, http.StatusOK)
	defer done()

	waID := chatwootOutboundMessageID("inst", "900", "text")
	service.rememberMessageReceipt("inst", waID, 80, "900")
	raw, _ := json.Marshal(map[string]interface{}{
		"event":      "Receipt",
		"state":      "Read",
		"instanceId": "inst",
		"data":       map[string]interface{}{"MessageIDs": []string{waID}, "IsGroup": false, "Type": "read"},
	})
	if err := service.HandleEvolutionEvent(raw); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 1 || (*calls)[0].status != "read" {
		t.Fatalf("expected one read update, got %#v", *calls)
	}
}
