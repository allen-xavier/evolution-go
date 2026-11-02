// Package watchdog detects linked WhatsApp devices that keep the websocket
// "connected" and keep sending while the WhatsApp server silently stops
// delivering anything to them (zombie session, e.g. a stuck offline queue).
//
// It watches the AMQP event flow: every Message or Receipt event proves the
// server is still delivering to the device. When an instance sends messages to
// other chats and nothing at all comes back (no message, no delivery or read
// receipt) for the silence window, it raises an alert in the logs and on the
// optional webhook.
//
// A self-message probe cannot prove this: WhatsApp never delivers a message
// back to the device that sent it, and the SendMessage event is produced
// locally by Evolution, without the server.
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

	defaultCheckInterval = 5 * time.Minute
	defaultSilenceWindow = 15 * time.Minute
	defaultAlertCooldown = 1 * time.Hour
	// One contact with the phone off sends no receipt: only alert after several
	// sends to more than one chat.
	defaultMinSends = 3
	defaultMinChats = 2
)

type SettingsStore interface {
	GetSetting(key string) (string, error)
	SetSetting(key string, value string) error
}

type ConfigLookup interface {
	GetConfig(instanceID string) (*chatwoot_model.ChatwootConfig, error)
}

type instanceState struct {
	// Sends to other chats since the last event delivered by the server.
	firstUnansweredAt time.Time
	unansweredSends   int
	unansweredChats   map[string]bool
	lastInboundAt     time.Time
	lastAlertAt       time.Time
	instanceName      string
}

type InstanceStatus struct {
	InstanceID      string     `json:"instanceId"`
	InstanceName    string     `json:"instanceName,omitempty"`
	Monitored       bool       `json:"monitored"`
	PendingProbe    bool       `json:"pendingProbe"`
	UnansweredSends int        `json:"unansweredSends"`
	LastProbeAt     *time.Time `json:"lastProbeAt,omitempty"`
	LastAckAt       *time.Time `json:"lastAckAt,omitempty"`
	LastAlertAt     *time.Time `json:"lastAlertAt,omitempty"`
	Healthy         bool       `json:"healthy"`
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

	checkInterval time.Duration
	silenceWindow time.Duration
	alertCooldown time.Duration
	minSends      int
	minChats      int
	now           func() time.Time

	mu        sync.Mutex
	instances map[string]*instanceState
}

func New(settings SettingsStore, configs ConfigLookup, evolutionAPI evolution.API, loggerWrapper *logging.Manager) *Watchdog {
	return &Watchdog{
		settings:      settings,
		configs:       configs,
		evolution:     evolutionAPI,
		loggerWrapper: loggerWrapper,
		httpClient:    &http.Client{Timeout: 10 * time.Second},
		checkInterval: defaultCheckInterval,
		silenceWindow: defaultSilenceWindow,
		alertCooldown: defaultAlertCooldown,
		minSends:      defaultMinSends,
		minChats:      defaultMinChats,
		now:           time.Now,
		instances:     map[string]*instanceState{},
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
		ProbeIntervalSeconds: int(w.checkInterval.Seconds()),
		AckTimeoutSeconds:    int(w.silenceWindow.Seconds()),
		Instances:            []InstanceStatus{},
	}
	for instanceID, state := range w.instances {
		status := InstanceStatus{
			InstanceID:      instanceID,
			InstanceName:    state.instanceName,
			Monitored:       true,
			PendingProbe:    state.unansweredSends > 0,
			UnansweredSends: state.unansweredSends,
			Healthy:         !w.silentLocked(state),
		}
		if !state.firstUnansweredAt.IsZero() {
			at := state.firstUnansweredAt
			status.LastProbeAt = &at
		}
		if !state.lastInboundAt.IsZero() {
			at := state.lastInboundAt
			status.LastAckAt = &at
		}
		if !state.lastAlertAt.IsZero() {
			at := state.lastAlertAt
			status.LastAlertAt = &at
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
			timer.Reset(w.checkInterval)
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
			w.reset(instance.Id)
			continue
		}
		cfg, err := w.configs.GetConfig(instance.Id)
		if err != nil {
			w.log(instance.Id, "watchdog failed to load chatwoot config: %v", err)
			continue
		}
		if cfg == nil || !cfg.Enabled {
			continue
		}
		w.checkSilence(instance)
	}
}

// silentLocked reports whether the instance sent enough messages without any
// event coming back for the whole silence window. Callers hold w.mu.
func (w *Watchdog) silentLocked(state *instanceState) bool {
	return state.unansweredSends >= w.minSends &&
		len(state.unansweredChats) >= w.minChats &&
		!state.firstUnansweredAt.IsZero() &&
		w.now().Sub(state.firstUnansweredAt) >= w.silenceWindow
}

// checkSilence raises an alert when the instance keeps sending and the server
// delivers nothing back.
func (w *Watchdog) checkSilence(instance evolution.Instance) {
	w.mu.Lock()
	state, ok := w.instances[instance.Id]
	if !ok || !w.silentLocked(state) || w.now().Sub(state.lastAlertAt) < w.alertCooldown {
		w.mu.Unlock()
		return
	}
	state.lastAlertAt = w.now()
	if instance.Name != "" {
		state.instanceName = instance.Name
	}
	instanceName := state.instanceName
	sends := state.unansweredSends
	chats := len(state.unansweredChats)
	silence := w.now().Sub(state.firstUnansweredAt).Round(time.Minute)
	w.mu.Unlock()

	if w.loggerWrapper != nil {
		w.loggerWrapper.GetLogger(instance.Id).LogError(
			"watchdog ALERT: instance %q (%s) sent %d message(s) to %d chat(s) in the last %s and WhatsApp delivered nothing back (no message, no delivery or read receipt); WhatsApp may have stopped delivering to this linked device (zombie session)",
			instanceName, instance.Jid, sends, chats, silence)
	}
	go w.dispatchAlertWebhook(instance, instanceName, sends, silence)
}

// dispatchAlertWebhook notifies the configured URL about a suspected zombie session.
func (w *Watchdog) dispatchAlertWebhook(instance evolution.Instance, instanceName string, sends int, silence time.Duration) {
	webhookURL := w.WebhookURL()
	if webhookURL == "" {
		return
	}

	payload, err := json.Marshal(map[string]interface{}{
		"event":        "watchdog_alert",
		"instanceId":   instance.Id,
		"instanceName": instanceName,
		"jid":          instance.Jid,
		"probeAge":     silence.String(),
		"sends":        sends,
		"message":      fmt.Sprintf("O WhatsApp %s enviou %d mensagem(ns) nos últimos %s e não recebeu nada de volta (nem mensagem, nem confirmação de entrega). Ele pode ter parado de receber mensagens.", instanceName, sends, silence),
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

func (w *Watchdog) reset(instanceID string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if state, ok := w.instances[instanceID]; ok {
		state.firstUnansweredAt = time.Time{}
		state.unansweredSends = 0
		state.unansweredChats = nil
	}
}

func (w *Watchdog) stateLocked(instanceID string) *instanceState {
	state, ok := w.instances[instanceID]
	if !ok {
		state = &instanceState{}
		w.instances[instanceID] = state
	}
	return state
}

// Observe inspects every AMQP event. Message and Receipt events come from the
// WhatsApp server and clear the instance; SendMessage events to other chats
// count as sends waiting for anything to come back.
func (w *Watchdog) Observe(raw []byte) {
	var payload struct {
		Event        string `json:"event"`
		InstanceID   string `json:"instanceId"`
		InstanceName string `json:"instanceName"`
		Data         struct {
			Info struct {
				Chat    string `json:"Chat"`
				Sender  string `json:"Sender"`
				IsGroup bool   `json:"IsGroup"`
			} `json:"Info"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil || payload.InstanceID == "" {
		return
	}

	switch payload.Event {
	case "Message", "Receipt":
		w.mu.Lock()
		state := w.stateLocked(payload.InstanceID)
		if payload.InstanceName != "" {
			state.instanceName = payload.InstanceName
		}
		state.firstUnansweredAt = time.Time{}
		state.unansweredSends = 0
		state.unansweredChats = nil
		state.lastInboundAt = w.now()
		w.mu.Unlock()
	case "SendMessage":
		chat := jidUser(payload.Data.Info.Chat)
		// Old self-message probes and notes to the own number get nothing back.
		if chat == "" || chat == jidUser(payload.Data.Info.Sender) ||
			bytes.Contains(raw, []byte(chatwoot_model.WatchdogProbeMarker)) {
			return
		}
		w.mu.Lock()
		state := w.stateLocked(payload.InstanceID)
		if payload.InstanceName != "" {
			state.instanceName = payload.InstanceName
		}
		if state.unansweredSends == 0 {
			state.firstUnansweredAt = w.now()
			state.unansweredChats = map[string]bool{}
		}
		state.unansweredSends++
		state.unansweredChats[chat] = true
		w.mu.Unlock()
	}
}

// jidUser extracts the user part of a JID like "553121201708:20@s.whatsapp.net".
func jidUser(jid string) string {
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
