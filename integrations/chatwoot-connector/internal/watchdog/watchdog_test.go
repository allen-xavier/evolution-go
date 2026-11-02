package watchdog

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/allen-xavier/evolution-go-chatwoot-connector/internal/evolution"
	chatwoot_model "github.com/allen-xavier/evolution-go-chatwoot-connector/internal/model"
)

type fakeSettings struct {
	mu sync.Mutex
	kv map[string]string
}

func (f *fakeSettings) GetSetting(key string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.kv[key], nil
}

func (f *fakeSettings) SetSetting(key string, value string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.kv[key] = value
	return nil
}

type fakeConfigs struct {
	enabled map[string]bool
}

func (f *fakeConfigs) GetConfig(instanceID string) (*chatwoot_model.ChatwootConfig, error) {
	if !f.enabled[instanceID] {
		return nil, nil
	}
	return &chatwoot_model.ChatwootConfig{InstanceID: instanceID, Enabled: true}, nil
}

type fakeEvolution struct {
	mu        sync.Mutex
	instances []evolution.Instance
	sent      []evolution.TextRequest
	sentTo    []string
}

func (f *fakeEvolution) ListInstances(ctx context.Context) ([]evolution.Instance, error) {
	return f.instances, nil
}

func (f *fakeEvolution) SendText(ctx context.Context, instance *evolution.Instance, request evolution.TextRequest) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, request)
	f.sentTo = append(f.sentTo, instance.Id)
	return nil
}

func (f *fakeEvolution) GetInstance(ctx context.Context, instanceID string) (*evolution.Instance, error) {
	return nil, fmt.Errorf("not implemented")
}
func (f *fakeEvolution) SendMedia(ctx context.Context, instance *evolution.Instance, request evolution.MediaRequest) error {
	return fmt.Errorf("not implemented")
}
func (f *fakeEvolution) SetProxy(ctx context.Context, instanceID string, config evolution.ProxyConfig) error {
	return fmt.Errorf("not implemented")
}
func (f *fakeEvolution) RemoveProxy(ctx context.Context, instanceID string) error {
	return fmt.Errorf("not implemented")
}
func (f *fakeEvolution) DisconnectInstance(ctx context.Context, instanceID string) error {
	return fmt.Errorf("not implemented")
}
func (f *fakeEvolution) ReconnectInstance(ctx context.Context, instanceID string) error {
	return fmt.Errorf("not implemented")
}
func (f *fakeEvolution) CheckNumbers(ctx context.Context, instanceID string, numbers []string) ([]evolution.NumberCheck, error) {
	return nil, fmt.Errorf("not implemented")
}
func (f *fakeEvolution) DeleteMessageForEveryone(ctx context.Context, instance *evolution.Instance, request evolution.DeleteMessageRequest) error {
	return fmt.Errorf("not implemented")
}

func newTestWatchdog(settings *fakeSettings, evolutionAPI *fakeEvolution) *Watchdog {
	w := New(settings, &fakeConfigs{enabled: map[string]bool{"inst-1": true}}, evolutionAPI, nil)
	w.checkInterval = time.Minute
	w.silenceWindow = 15 * time.Minute
	w.alertCooldown = time.Hour
	return w
}

func connectedInstance() evolution.Instance {
	return evolution.Instance{Id: "inst-1", Name: "Michele14", Jid: "553121201708:20@s.whatsapp.net", Connected: true, Token: "token"}
}

// sendEvent is the SendMessage event Evolution publishes after sending to chat.
func sendEvent(chat string) []byte {
	return []byte(fmt.Sprintf(`{"event":"SendMessage","instanceId":"inst-1","instanceName":"Michele14","data":{"Info":{"Chat":"%s@s.whatsapp.net","Sender":"553121201708:20@s.whatsapp.net","IsFromMe":true,"ID":"3EB0"},"Message":{"conversation":"oi"}}}`, chat))
}

// sendThree sends to two contacts at start, start+1m and start+2m.
func sendThree(w *Watchdog, start time.Time) {
	for i, chat := range []string{"5511999990001", "5511999990002", "5511999990001"} {
		at := start.Add(time.Duration(i) * time.Minute)
		w.now = func() time.Time { return at }
		w.Observe(sendEvent(chat))
	}
}

func TestDisabledSendsNothingAndNeverAlerts(t *testing.T) {
	settings := &fakeSettings{kv: map[string]string{}}
	evolutionAPI := &fakeEvolution{instances: []evolution.Instance{connectedInstance()}}
	w := newTestWatchdog(settings, evolutionAPI)

	start := time.Now()
	sendThree(w, start)
	w.now = func() time.Time { return start.Add(time.Hour) }
	w.tick(context.Background())

	if len(evolutionAPI.sent) != 0 {
		t.Fatalf("the watchdog must not send messages, got %d", len(evolutionAPI.sent))
	}
	if status := w.Status(); status.Instances[0].LastAlertAt != nil {
		t.Fatal("disabled watchdog must not alert")
	}
}

func TestEnabledNeverSendsSelfProbe(t *testing.T) {
	settings := &fakeSettings{kv: map[string]string{SettingKeyEnabled: "true"}}
	evolutionAPI := &fakeEvolution{instances: []evolution.Instance{connectedInstance()}}
	w := newTestWatchdog(settings, evolutionAPI)

	w.tick(context.Background())

	if len(evolutionAPI.sent) != 0 {
		t.Fatalf("self-message probes prove nothing and must not be sent, got %d", len(evolutionAPI.sent))
	}
}

// The bug of 2026-11-02: the SendMessage event (produced locally by Evolution)
// counted as the echo, so an instance that received nothing for hours looked healthy.
func TestSendingWithoutAnythingBackRaisesAlert(t *testing.T) {
	settings := &fakeSettings{kv: map[string]string{SettingKeyEnabled: "true"}}
	evolutionAPI := &fakeEvolution{instances: []evolution.Instance{connectedInstance()}}
	w := newTestWatchdog(settings, evolutionAPI)

	start := time.Now()
	sendThree(w, start)

	w.now = func() time.Time { return start.Add(14 * time.Minute) }
	w.tick(context.Background())
	if status := w.Status(); status.Instances[0].LastAlertAt != nil {
		t.Fatal("must wait for the whole silence window")
	}

	w.now = func() time.Time { return start.Add(15 * time.Minute) }
	w.tick(context.Background())
	status := w.Status()
	if status.Instances[0].LastAlertAt == nil {
		t.Fatal("expected alert: 3 sends and nothing back for 15 minutes")
	}
	if status.Instances[0].Healthy || status.Instances[0].UnansweredSends != 3 {
		t.Fatalf("expected unhealthy with 3 unanswered sends, got %+v", status.Instances[0])
	}

	// Cooldown: no second alert within the hour.
	first := *status.Instances[0].LastAlertAt
	w.now = func() time.Time { return start.Add(30 * time.Minute) }
	w.tick(context.Background())
	if got := *w.Status().Instances[0].LastAlertAt; !got.Equal(first) {
		t.Fatal("expected a single alert within the cooldown")
	}
}

func TestReceiptOrMessageClearsTheInstance(t *testing.T) {
	for _, event := range []string{
		`{"event":"Receipt","instanceId":"inst-1","state":"Delivered","data":{"Chat":"5511999990001@s.whatsapp.net","MessageIDs":["3EB0"]}}`,
		`{"event":"Message","instanceId":"inst-1","data":{"Info":{"Chat":"5511999990009@s.whatsapp.net","IsFromMe":false},"Message":{"conversation":"olá"}}}`,
	} {
		settings := &fakeSettings{kv: map[string]string{SettingKeyEnabled: "true"}}
		evolutionAPI := &fakeEvolution{instances: []evolution.Instance{connectedInstance()}}
		w := newTestWatchdog(settings, evolutionAPI)

		start := time.Now()
		sendThree(w, start)
		w.now = func() time.Time { return start.Add(10 * time.Minute) }
		w.Observe([]byte(event))

		w.now = func() time.Time { return start.Add(20 * time.Minute) }
		w.tick(context.Background())
		status := w.Status()
		if status.Instances[0].LastAlertAt != nil || !status.Instances[0].Healthy || status.Instances[0].LastAckAt == nil {
			t.Fatalf("event from the server must clear the instance: %s -> %+v", event, status.Instances[0])
		}
	}
}

func TestSelfMessagesAndSingleChatDoNotAlert(t *testing.T) {
	settings := &fakeSettings{kv: map[string]string{SettingKeyEnabled: "true"}}
	evolutionAPI := &fakeEvolution{instances: []evolution.Instance{connectedInstance()}}
	w := newTestWatchdog(settings, evolutionAPI)

	start := time.Now()
	w.now = func() time.Time { return start }
	// Own number (notes, old probes), a probe marker and a single contact with the phone off.
	w.Observe(sendEvent("553121201708"))
	w.Observe([]byte(fmt.Sprintf(`{"event":"SendMessage","instanceId":"inst-1","data":{"Info":{"Chat":"5511999990001@s.whatsapp.net","Sender":"553121201708:20@s.whatsapp.net"},"Message":{"conversation":"%s x"}}}`, chatwoot_model.WatchdogProbeMarker)))
	for i := 0; i < 5; i++ {
		w.Observe(sendEvent("5511999990003"))
	}

	w.now = func() time.Time { return start.Add(time.Hour) }
	w.tick(context.Background())
	if status := w.Status(); status.Instances[0].LastAlertAt != nil {
		t.Fatalf("one offline contact is not a zombie session: %+v", status.Instances[0])
	}
}

func TestDisconnectedInstanceDoesNotAlert(t *testing.T) {
	settings := &fakeSettings{kv: map[string]string{SettingKeyEnabled: "true"}}
	instance := connectedInstance()
	instance.Connected = false
	evolutionAPI := &fakeEvolution{instances: []evolution.Instance{instance}}
	w := newTestWatchdog(settings, evolutionAPI)

	start := time.Now()
	sendThree(w, start)
	w.now = func() time.Time { return start.Add(time.Hour) }
	w.tick(context.Background())

	if status := w.Status(); status.Instances[0].LastAlertAt != nil {
		t.Fatal("disconnected instance must not raise zombie alerts")
	}
}

func TestAlertDeliversWebhook(t *testing.T) {
	received := make(chan map[string]interface{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&payload); err == nil {
			received <- payload
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	settings := &fakeSettings{kv: map[string]string{
		SettingKeyEnabled:    "true",
		SettingKeyWebhookURL: server.URL,
	}}
	evolutionAPI := &fakeEvolution{instances: []evolution.Instance{connectedInstance()}}
	w := newTestWatchdog(settings, evolutionAPI)

	start := time.Now()
	sendThree(w, start)
	w.now = func() time.Time { return start.Add(16 * time.Minute) }
	w.tick(context.Background())

	select {
	case payload := <-received:
		if payload["event"] != "watchdog_alert" || payload["instanceId"] != "inst-1" || payload["instanceName"] != "Michele14" {
			t.Fatalf("unexpected webhook payload %v", payload)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("expected alert webhook to be delivered")
	}
}
