package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/allen-xavier/evolution-go-chatwoot-connector/internal/evolution"
	"github.com/allen-xavier/evolution-go-chatwoot-connector/internal/model"
)

const dispatchTestInstance = "eef4c22f-766f-4c77-a376-52219f57adfc"

// slowEvolution holds every text send until release is closed, like a video
// that takes a long time to reach WhatsApp.
type slowEvolution struct {
	outboundFakeEvolution
	release chan struct{}
	started chan string
	mu      sync.Mutex
	sent    []string
}

func (f *slowEvolution) SendText(_ context.Context, _ *evolution.Instance, request evolution.TextRequest) error {
	f.started <- request.Text
	<-f.release
	f.mu.Lock()
	f.sent = append(f.sent, request.Text)
	f.mu.Unlock()
	return nil
}

func dispatchTestService(evolutionAPI evolution.API, chatwootURL string) (*chatwootService, *identityMemoryRepository) {
	remoteJID := "5516991635281@s.whatsapp.net"
	repository := &identityMemoryRepository{
		configs: map[string]*model.ChatwootConfig{
			dispatchTestInstance: {InstanceID: dispatchTestInstance, Enabled: true, InboxID: 7, URL: chatwootURL, AccountID: "1", Token: "t"},
		},
		bindings: map[string]*model.ChatwootBinding{
			identityKey(dispatchTestInstance, remoteJID): {
				ID: 1, InstanceID: dispatchTestInstance, RemoteJID: remoteJID, ConversationID: 91,
			},
		},
	}
	return NewChatwootService(repository, evolutionAPI, nil, testLoggerManager()).(*chatwootService), repository
}

func dispatchWebhook(id int, content string) []byte {
	return []byte(fmt.Sprintf(`{
		"event":"message_created", "id":%d, "content":%q,
		"message_type":"outgoing", "private":false,
		"conversation":{"id":91,"inbox_id":7,"contact_inbox":{"source_id":"unused"}},
		"sender":{"type":"user","name":"Atendente"}
	}`, id, content))
}

func TestWebhookAnswersBeforeTheSlowSendFinishes(t *testing.T) {
	evolutionAPI := &slowEvolution{release: make(chan struct{}), started: make(chan string, 4)}
	service, repository := dispatchTestService(evolutionAPI, "")

	answered := make(chan error, 1)
	go func() { answered <- service.HandleWebhook(dispatchTestInstance, nil, dispatchWebhook(260100, "video")) }()
	select {
	case err := <-answered:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("webhook waited for the WhatsApp send")
	}
	<-evolutionAPI.started

	if len(repository.jobs) != 1 {
		t.Fatalf("message must be stored while it is sent, got %d jobs", len(repository.jobs))
	}
	for _, job := range repository.jobs {
		if until := time.Until(job.NextAttemptAt); until < outboundInflightLease-time.Minute {
			t.Fatalf("stored job must keep the retry worker away, next attempt in %s", until)
		}
		// Even when the lease is over, the worker must not send it a second time.
		job.NextAttemptAt = time.Now().Add(-time.Second)
	}
	service.processDueOutboundJobs(context.Background())
	select {
	case text := <-evolutionAPI.started:
		t.Fatalf("worker sent the in-flight message again: %q", text)
	default:
	}

	close(evolutionAPI.release)
	waitOutbound(t, service)
	if len(repository.jobs) != 0 {
		t.Fatalf("delivered message must leave no job, got %#v", repository.jobs)
	}
	if len(evolutionAPI.sent) != 1 {
		t.Fatalf("message sent %d times, want 1", len(evolutionAPI.sent))
	}
}

func TestMessagesOfOneConversationKeepTheirOrder(t *testing.T) {
	evolutionAPI := &slowEvolution{release: make(chan struct{}), started: make(chan string, 4)}
	service, _ := dispatchTestService(evolutionAPI, "")

	for index, text := range []string{"primeira", "segunda", "terceira"} {
		if err := service.HandleWebhook(dispatchTestInstance, nil, dispatchWebhook(260200+index, text)); err != nil {
			t.Fatal(err)
		}
	}
	if first := <-evolutionAPI.started; first != "primeira" {
		t.Fatalf("first send = %q", first)
	}
	select {
	case text := <-evolutionAPI.started:
		t.Fatalf("%q started before the first message finished", text)
	case <-time.After(100 * time.Millisecond):
	}

	close(evolutionAPI.release)
	waitOutbound(t, service)
	want := []string{"primeira", "segunda", "terceira"}
	if fmt.Sprint(evolutionAPI.sent) != fmt.Sprint(want) {
		t.Fatalf("sent %v, want %v", evolutionAPI.sent, want)
	}
}

func TestMessageDeletedWhileQueuedIsNotSent(t *testing.T) {
	evolutionAPI := &slowEvolution{release: make(chan struct{}), started: make(chan string, 4)}
	service, repository := dispatchTestService(evolutionAPI, "")

	if err := service.HandleWebhook(dispatchTestInstance, nil, dispatchWebhook(260300, "primeira")); err != nil {
		t.Fatal(err)
	}
	if err := service.HandleWebhook(dispatchTestInstance, nil, dispatchWebhook(260301, "apagada")); err != nil {
		t.Fatal(err)
	}
	<-evolutionAPI.started
	if !service.webhookCacheDeleted(dispatchTestInstance, "260301") {
		t.Fatal("could not record the deletion")
	}
	if !service.cancelWaitingReply(dispatchTestInstance, "260301") {
		t.Fatal("a queued message has not reached WhatsApp yet")
	}
	if service.cancelWaitingReply(dispatchTestInstance, "260300") {
		t.Fatal("a message being sent may already be on WhatsApp")
	}

	close(evolutionAPI.release)
	waitOutbound(t, service)
	if fmt.Sprint(evolutionAPI.sent) != "[primeira]" {
		t.Fatalf("sent %v, want only the first message", evolutionAPI.sent)
	}
	for _, job := range repository.jobs {
		if job.ChatwootMessageID != "260301" || job.FailedAt == nil {
			t.Fatalf("deleted message must be kept as stopped, got %#v", job)
		}
	}
}

func TestTerminalFailureTurnsTheChatwootMessageRed(t *testing.T) {
	var mu sync.Mutex
	var patches []map[string]interface{}
	var patchPaths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch {
			body, _ := io.ReadAll(r.Body)
			var decoded map[string]interface{}
			_ = json.Unmarshal(body, &decoded)
			mu.Lock()
			patches = append(patches, decoded)
			patchPaths = append(patchPaths, r.URL.Path)
			mu.Unlock()
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":1}`))
	}))
	defer server.Close()

	evolutionAPI := &outboundFakeEvolution{
		sendTextErr: errors.New(`evolution request failed [500]: {"error":"number is not registered on WhatsApp"}`),
	}
	service, _ := dispatchTestService(evolutionAPI, server.URL)
	service.httpClient = server.Client()

	if err := service.HandleWebhook(dispatchTestInstance, nil, dispatchWebhook(260400, "teste")); err != nil {
		t.Fatal(err)
	}
	waitOutbound(t, service)

	if len(patches) != 1 || patchPaths[0] != "/api/v1/accounts/1/conversations/91/messages/260400" {
		t.Fatalf("expected one status update of message 260400, got %v %v", patchPaths, patches)
	}
	if patches[0]["status"] != "failed" || patches[0]["external_error"] == "" {
		t.Fatalf("message must be marked failed with the reason, got %v", patches[0])
	}
}

func TestTransientFailureIsRetriedWithoutTurningRed(t *testing.T) {
	var patched bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch {
			patched = true
		}
		_, _ = w.Write([]byte(`{"id":1}`))
	}))
	defer server.Close()

	evolutionAPI := &outboundFakeEvolution{sendTextErr: errors.New("evolution request failed [502]: bad gateway")}
	service, repository := dispatchTestService(evolutionAPI, server.URL)
	service.httpClient = server.Client()

	if err := service.HandleWebhook(dispatchTestInstance, nil, dispatchWebhook(260500, "teste")); err != nil {
		t.Fatal(err)
	}
	waitOutbound(t, service)
	if patched {
		t.Fatal("a message that will be retried must not turn red")
	}
	for _, job := range repository.jobs {
		if job.FailedAt != nil || time.Until(job.NextAttemptAt) > time.Minute {
			t.Fatalf("job must be retried soon, got %#v", job)
		}
	}
}

func (s *chatwootService) webhookCacheDeleted(instanceID string, chatwootMessageID string) bool {
	return s.webhookCache.Add(deletedMessageKey(instanceID, chatwootMessageID), true, deletedMessageCacheTTL) == nil
}
