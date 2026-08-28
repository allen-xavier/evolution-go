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

func newTestWatchdog(settings *fakeSettings, evolutionAPI *fakeEvolution) *Watchdog {
	w := New(settings, &fakeConfigs{enabled: map[string]bool{"inst-1": true}}, evolutionAPI, nil)
	w.probeInterval = time.Minute
	w.ackTimeout = 30 * time.Second
	w.alertCooldown = time.Hour
	return w
}

func connectedInstance() evolution.Instance {
	return evolution.Instance{Id: "inst-1", Name: "Michele14", Jid: "553121201708:20@s.whatsapp.net", Connected: true, Token: "token"}
}

func TestDisabledByDefaultSendsNothing(t *testing.T) {
	settings := &fakeSettings{kv: map[string]string{}}
	evolutionAPI := &fakeEvolution{instances: []evolution.Instance{connectedInstance()}}
	w := newTestWatchdog(settings, evolutionAPI)

	w.tick(context.Background())

	if len(evolutionAPI.sent) != 0 {
		t.Fatalf("expected no probes while disabled, got %d", len(evolutionAPI.sent))
	}
}

func TestEnabledSendsProbeToOwnNumber(t *testing.T) {
	settings := &fakeSettings{kv: map[string]string{SettingKeyEnabled: "true"}}
	evolutionAPI := &fakeEvolution{instances: []evolution.Instance{connectedInstance()}}
	w := newTestWatchdog(settings, evolutionAPI)

	w.tick(context.Background())

	if len(evolutionAPI.sent) != 1 {
		t.Fatalf("expected 1 probe, got %d", len(evolutionAPI.sent))
	}
	probe := evolutionAPI.sent[0]
	if probe.Number != "553121201708" {
		t.Fatalf("expected self number 553121201708, got %q", probe.Number)
	}
	if !containsMarker(probe.Text) {
		t.Fatalf("probe text must contain the watchdog marker, got %q", probe.Text)
	}
	if status := w.Status(); len(status.Instances) != 1 || !status.Instances[0].PendingProbe {
		t.Fatalf("expected pending probe in status, got %+v", status.Instances)
	}
}

func TestProbeEchoAcknowledges(t *testing.T) {
	settings := &fakeSettings{kv: map[string]string{SettingKeyEnabled: "true"}}
	evolutionAPI := &fakeEvolution{instances: []evolution.Instance{connectedInstance()}}
	w := newTestWatchdog(settings, evolutionAPI)

	w.tick(context.Background())
	echo := fmt.Sprintf(`{"event":"SendMessage","instanceId":"inst-1","data":{"Info":{"IsFromMe":true},"Message":{"conversation":"%s 2026-08-28T00:00:00Z"}}}`, chatwoot_model.WatchdogProbeMarker)
	w.Observe([]byte(echo))

	status := w.Status()
	if status.Instances[0].PendingProbe {
		t.Fatal("expected probe to be acknowledged")
	}
	if status.Instances[0].LastAckAt == nil {
		t.Fatal("expected LastAckAt to be recorded")
	}
}

func TestMissingEchoRaisesAlert(t *testing.T) {
	settings := &fakeSettings{kv: map[string]string{SettingKeyEnabled: "true"}}
	evolutionAPI := &fakeEvolution{instances: []evolution.Instance{connectedInstance()}}
	w := newTestWatchdog(settings, evolutionAPI)

	start := time.Now()
	w.now = func() time.Time { return start }
	w.tick(context.Background())

	w.now = func() time.Time { return start.Add(31 * time.Second) }
	w.tick(context.Background())

	status := w.Status()
	if status.Instances[0].LastAlertAt == nil {
		t.Fatal("expected alert after ack timeout")
	}
	if status.Instances[0].Healthy {
		t.Fatal("expected instance to be unhealthy after missed echo")
	}
}

func TestDisconnectedInstanceDoesNotAlert(t *testing.T) {
	settings := &fakeSettings{kv: map[string]string{SettingKeyEnabled: "true"}}
	instance := connectedInstance()
	evolutionAPI := &fakeEvolution{instances: []evolution.Instance{instance}}
	w := newTestWatchdog(settings, evolutionAPI)

	start := time.Now()
	w.now = func() time.Time { return start }
	w.tick(context.Background())

	instance.Connected = false
	evolutionAPI.instances = []evolution.Instance{instance}
	w.now = func() time.Time { return start.Add(31 * time.Second) }
	w.tick(context.Background())

	if status := w.Status(); status.Instances[0].LastAlertAt != nil {
		t.Fatal("disconnected instance must not raise zombie alerts")
	}
}

func TestMissingEchoDeliversAlertWebhook(t *testing.T) {
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
	w.now = func() time.Time { return start }
	w.tick(context.Background())

	w.now = func() time.Time { return start.Add(31 * time.Second) }
	w.tick(context.Background())

	select {
	case payload := <-received:
		if payload["event"] != "watchdog_alert" {
			t.Fatalf("expected watchdog_alert event, got %v", payload["event"])
		}
		if payload["instanceId"] != "inst-1" {
			t.Fatalf("expected instanceId inst-1, got %v", payload["instanceId"])
		}
	case <-time.After(3 * time.Second):
		t.Fatal("expected alert webhook to be delivered")
	}
}

func TestMissingEchoWithoutWebhookOnlyLogs(t *testing.T) {
	settings := &fakeSettings{kv: map[string]string{SettingKeyEnabled: "true"}}
	evolutionAPI := &fakeEvolution{instances: []evolution.Instance{connectedInstance()}}
	w := newTestWatchdog(settings, evolutionAPI)

	start := time.Now()
	w.now = func() time.Time { return start }
	w.tick(context.Background())

	w.now = func() time.Time { return start.Add(31 * time.Second) }
	w.tick(context.Background())

	if status := w.Status(); status.Instances[0].LastAlertAt == nil {
		t.Fatal("expected alert even without webhook configured")
	}
}

func containsMarker(text string) bool {
	return len(text) >= len(chatwoot_model.WatchdogProbeMarker) && text[:len(chatwoot_model.WatchdogProbeMarker)] == chatwoot_model.WatchdogProbeMarker
}
