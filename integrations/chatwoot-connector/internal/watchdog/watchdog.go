// Package watchdog implements an end-to-end liveness probe for linked
// WhatsApp devices. A device can keep the websocket "connected" while the
// WhatsApp server silently stops routing messages to it (zombie session).
// The watchdog periodically sends a self-message through Evolution and checks
// that the echo comes back through the AMQP event flow. A probe without an
// echo within the ack timeout raises an alert in the logs.
package watchdog

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/allen-xavier/evolution-go-chatwoot-connector/internal/evolution"
	"github.com/allen-xavier/evolution-go-chatwoot-connector/internal/logging"
	chatwoot_model "github.com/allen-xavier/evolution-go-chatwoot-connector/internal/model"
)

const (
	// SettingKeyEnabled persists the UI toggle in the connector_settings table.
	SettingKeyEnabled = "watchdog_enabled"
	// SettingKeyWebhookURL persists the alert webhook URL.
	SettingKeyWebhookURL = "watchdog_webhook_url"

	defaultProbeInterval = 15 * time.Minute
	defaultAckTimeout    = 10 * time.Minute
	defaultAlertCooldown = 1 * time.Hour
)

type SettingsStore interface {
	GetSetting(key string) (string, error)
	SetSetting(key string, value string) error
}

type ConfigLookup interface {
	GetConfig(instanceID string) (*chatwoot_model.ChatwootConfig, error)
}

type probeState struct {
	sentAt       time.Time
	lastAckAt    time.Time
	lastAlertAt  time.Time
	instanceName string
}

type InstanceStatus struct {
	InstanceID     string     `json:"instanceId"`
	InstanceName   string     `json:"instanceName,omitempty"`
	Monitored      bool       `json:"monitored"`
	PendingProbe   bool       `json:"pendingProbe"`
	LastProbeAt    *time.Time `json:"lastProbeAt,omitempty"`
	LastAckAt      *time.Time `json:"lastAckAt,omitempty"`
	LastAlertAt    *time.Time `json:"lastAlertAt,omitempty"`
	Healthy        bool       `json:"healthy"`
}

type StatusView struct {
	Enabled              bool             `json:"enabled"`
	WebhookURL           string           `json:"webhookUrl"`
	ProbeIntervalSeconds int              `json:"probeIntervalSeconds"`
	AckTimeoutSeconds    int              `json:"ackTimeoutSeconds"`
	Instances            []InstanceStatus `json:"instances"`
}

type Watchdog struct {
	settings      SettingsStore
	configs       ConfigLookup
	evolution     evolution.API
	loggerWrapper *logging.Manager
	httpClient    *http.Client

	probeInterval time.Duration
	ackTimeout    time.Duration
	alertCooldown time.Duration
	now           func() time.Time

	mu     sync.Mutex
	probes map[string]*probeState
	acked  map[string]time.Time
}

func New(settings SettingsStore, configs ConfigLookup, evolutionAPI evolution.API, loggerWrapper *logging.Manager) *Watchdog {
	return &Watchdog{
		settings:      settings,
		configs:       configs,
		evolution:     evolutionAPI,
		loggerWrapper: loggerWrapper,
		httpClient:    &http.Client{Timeout: 10 * time.Second},
		probeInterval: defaultProbeInterval,
		ackTimeout:    defaultAckTimeout,
		alertCooldown: defaultAlertCooldown,
		now:           time.Now,
		probes:        map[string]*probeState{},
		acked:         map[string]time.Time{},
	}
}

func (w *Watchdog) Enabled() bool {
	if w.settings == nil {
		return false
	}
	value, err := w.settings.GetSetting(SettingKeyEnabled)
	if err != nil {
		w.log("", "watchdog failed to read enabled setting: %v", err)
		return false
	}
	return strings.EqualFold(strings.TrimSpace(value), "true")
}

func (w *Watchdog) SetEnabled(ctx context.Context, enabled bool) error {
	if w.settings == nil {
		return fmt.Errorf("settings store is not configured")
	}
	value := "false"
	if enabled {
		value = "true"
	}
	if err := w.settings.SetSetting(SettingKeyEnabled, value); err != nil {
		return err
	}
	w.log("", "watchdog enabled=%v", enabled)
	return nil
}

func (w *Watchdog) WebhookURL() string {
	if w.settings == nil {
		return ""
	}
	value, err := w.settings.GetSetting(SettingKeyWebhookURL)
	if err != nil {
		w.log("", "watchdog failed to read webhook url setting: %v", err)
		return ""
	}
	return strings.TrimSpace(value)
}

func (w *Watchdog) SetConfig(ctx context.Context, enabled bool, webhookURL string) error {
	if err := w.SetEnabled(ctx, enabled); err != nil {
		return err
	}
	webhookURL = strings.TrimSpace(webhookURL)
	if webhookURL != "" && !strings.HasPrefix(webhookURL, "http://") && !strings.HasPrefix(webhookURL, "https://") {
		return fmt.Errorf("webhook url must start with http:// or https://")
	}
	if err := w.settings.SetSetting(SettingKeyWebhookURL, webhookURL); err != nil {
		return err
	}
	return nil
}

func (w *Watchdog) Status() StatusView {
	w.mu.Lock()
	defer w.mu.Unlock()

	view := StatusView{
		Enabled:              w.Enabled(),
		WebhookURL:           w.WebhookURL(),
		ProbeIntervalSeconds: int(w.probeInterval.Seconds()),
		AckTimeoutSeconds:    int(w.ackTimeout.Seconds()),
		Instances:            []InstanceStatus{},
	}
	for instanceID, probe := range w.probes {
		status := InstanceStatus{
			InstanceID:   instanceID,
			InstanceName: probe.instanceName,
			Monitored:    true,
		}
		if !probe.sentAt.IsZero() {
			sentAt := probe.sentAt
			status.LastProbeAt = &sentAt
			status.PendingProbe = true
		}
		if !probe.lastAlertAt.IsZero() {
			alertAt := probe.lastAlertAt
			status.LastAlertAt = &alertAt
		}
		if ackAt, ok := w.acked[instanceID]; ok {
			ackCopy := ackAt
			status.LastAckAt = &ackCopy
		}
		status.Healthy = status.LastAckAt != nil && (status.LastProbeAt == nil || !status.LastAckAt.Before(*status.LastProbeAt))
		if status.PendingProbe && w.now().Sub(probe.sentAt) > w.ackTimeout {
			status.Healthy = false
		}
		view.Instances = append(view.Instances, status)
	}
	return view
}

func (w *Watchdog) Run(ctx context.Context) {
	timer := time.NewTimer(time.Minute)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			w.tick(ctx)
			timer.Reset(w.probeInterval)
		}
	}
}

func (w *Watchdog) tick(ctx context.Context) {
	if !w.Enabled() {
		return
	}

	instances, err := w.evolution.ListInstances(ctx)
	if err != nil {
		w.log("", "watchdog failed to list evolution instances: %v", err)
		return
	}

	for i := range instances {
		instance := instances[i]
		if !instance.Connected || strings.TrimSpace(instance.Jid) == "" {
			w.clearPending(instance.Id)
			continue
		}
		w.checkPending(instance)

		cfg, err := w.configs.GetConfig(instance.Id)
		if err != nil {
			w.log(instance.Id, "watchdog failed to load chatwoot config: %v", err)
			continue
		}
		if cfg == nil || !cfg.Enabled {
			continue
		}

		w.sendProbe(ctx, &instance)
	}
}

// checkPending raises an alert when a previously sent probe never got its echo.
func (w *Watchdog) checkPending(instance evolution.Instance) {
	w.mu.Lock()
	defer w.mu.Unlock()

	probe, ok := w.probes[instance.Id]
	if !ok || probe.sentAt.IsZero() {
		return
	}
	age := w.now().Sub(probe.sentAt)
	if age <= w.ackTimeout {
		return
	}

	probe.sentAt = time.Time{}
	if w.now().Sub(probe.lastAlertAt) < w.alertCooldown {
		return
	}
	probe.lastAlertAt = w.now()
	ageRounded := age.Round(time.Second)
	instanceName := probe.instanceName
	if w.loggerWrapper != nil {
		w.loggerWrapper.GetLogger(instance.Id).LogError(
			"watchdog ALERT: instance %q (%s) did not echo the self-message probe sent %s ago; WhatsApp may have stopped routing messages to this linked device (zombie session). Relink the instance if it persists",
			instanceName, instance.Jid, ageRounded)
	}
	go w.dispatchAlertWebhook(instance, instanceName, ageRounded)
}

// dispatchAlertWebhook notifies the configured URL about a suspected zombie session.
func (w *Watchdog) dispatchAlertWebhook(instance evolution.Instance, instanceName string, probeAge time.Duration) {
	webhookURL := w.WebhookURL()
	if webhookURL == "" {
		return
	}

	payload, err := json.Marshal(map[string]interface{}{
		"event":        "watchdog_alert",
		"instanceId":   instance.Id,
		"instanceName": instanceName,
		"jid":          instance.Jid,
		"probeAge":     probeAge.String(),
		"message":      "Instância não respondeu ao probe de monitoramento; o WhatsApp pode ter parado de entregar mensagens para este aparelho vinculado (sessão zumbi). Relink necessário.",
		"timestamp":    w.now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(payload))
	if err != nil {
		w.log(instance.Id, "watchdog failed to build alert webhook request: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := w.httpClient.Do(req)
	if err != nil {
		w.log(instance.Id, "watchdog failed to deliver alert webhook: %v", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		w.log(instance.Id, "watchdog alert webhook returned status %d", resp.StatusCode)
		return
	}
	w.log(instance.Id, "watchdog alert webhook delivered to %s", webhookURL)
}

func (w *Watchdog) clearPending(instanceID string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if probe, ok := w.probes[instanceID]; ok {
		probe.sentAt = time.Time{}
	}
}

func (w *Watchdog) sendProbe(ctx context.Context, instance *evolution.Instance) {
	number := selfNumber(instance.Jid)
	if number == "" {
		return
	}

	text := fmt.Sprintf("%s %s", chatwoot_model.WatchdogProbeMarker, w.now().UTC().Format(time.RFC3339))
	if err := w.evolution.SendText(ctx, instance, evolution.TextRequest{Number: number, Text: text}); err != nil {
		w.log(instance.Id, "watchdog failed to send self-message probe: %v", err)
		return
	}

	w.mu.Lock()
	probe, ok := w.probes[instance.Id]
	if !ok {
		probe = &probeState{}
		w.probes[instance.Id] = probe
	}
	probe.sentAt = w.now()
	probe.instanceName = instance.Name
	w.mu.Unlock()

	w.log(instance.Id, "watchdog self-message probe sent to %s", number)
}

// Observe inspects every AMQP event; a probe echo acknowledges the pending probe.
func (w *Watchdog) Observe(raw []byte) {
	if !bytes.Contains(raw, []byte(chatwoot_model.WatchdogProbeMarker)) {
		return
	}

	var payload struct {
		Event      string `json:"event"`
		InstanceID string `json:"instanceId"`
		Data       struct {
			Info struct {
				IsFromMe bool `json:"IsFromMe"`
			} `json:"Info"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return
	}
	if payload.InstanceID == "" || (payload.Event != "Message" && payload.Event != "SendMessage") {
		return
	}
	if payload.Event != "SendMessage" && !payload.Data.Info.IsFromMe {
		return
	}

	w.mu.Lock()
	if probe, ok := w.probes[payload.InstanceID]; ok {
		if !probe.sentAt.IsZero() {
			latency := w.now().Sub(probe.sentAt)
			w.log(payload.InstanceID, "watchdog probe acknowledged in %s", latency.Round(time.Millisecond))
		}
		probe.sentAt = time.Time{}
	}
	w.acked[payload.InstanceID] = w.now()
	w.mu.Unlock()
}

// selfNumber extracts the phone number from a JID like "553121201708:20@s.whatsapp.net".
func selfNumber(jid string) string {
	jid = strings.TrimSpace(jid)
	if at := strings.Index(jid, "@"); at > -1 {
		jid = jid[:at]
	}
	if colon := strings.Index(jid, ":"); colon > -1 {
		jid = jid[:colon]
	}
	return jid
}

func (w *Watchdog) log(instanceID string, format string, args ...interface{}) {
	if w.loggerWrapper == nil {
		return
	}
	w.loggerWrapper.GetLogger(instanceID).LogInfo(format, args...)
}
