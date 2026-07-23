package proxymanager

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/allen-xavier/evolution-go-chatwoot-connector/internal/evolution"
	xproxy "golang.org/x/net/proxy"
)

const (
	defaultIPCheckURL  = "https://api.ipify.org?format=json"
	defaultWhatsAppURL = "https://web.whatsapp.com/"
)

type Repository interface {
	GetInstanceProxy(instanceID string) (string, error)
	GetProxyTest(instanceID string) (*TestRecord, error)
	FindInstancesByPublicIP(publicIP, exceptInstanceID string) ([]string, error)
	SaveProxyTest(record *TestRecord) error
	DeleteProxyTest(instanceID string) error
	ListInstanceProxies() ([]InstanceProxy, error)
}

type Manager struct {
	repository         Repository
	evolution          evolution.API
	ipCheckURL         string
	whatsappURL        string
	timeout            time.Duration
	proxyRequired      bool
	quarantineOnUnsafe bool
	setMu              sync.Mutex
}

type ConfigInput struct {
	Protocol string `json:"protocol"`
	Host     string `json:"host"`
	Port     string `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
}

type ConfigView struct {
	Enabled            bool        `json:"enabled"`
	Protocol           string      `json:"protocol,omitempty"`
	Host               string      `json:"host,omitempty"`
	Port               string      `json:"port,omitempty"`
	Username           string      `json:"username,omitempty"`
	HasPassword        bool        `json:"hasPassword"`
	Required           bool        `json:"required"`
	QuarantineOnUnsafe bool        `json:"quarantineOnUnsafe"`
	LastTest           *TestResult `json:"lastTest,omitempty"`
}

type TestResult struct {
	Working            bool      `json:"working"`
	PublicIP           string    `json:"publicIp,omitempty"`
	WhatsAppReachable  bool      `json:"whatsAppReachable"`
	WhatsAppStatus     int       `json:"whatsAppStatus,omitempty"`
	LatencyMS          int64     `json:"latencyMs"`
	TestedAt           time.Time `json:"testedAt"`
	UniqueIP           bool      `json:"uniqueIp"`
	DuplicateInstances []string  `json:"duplicateInstances,omitempty"`
	StableIP           bool      `json:"stableIp"`
	ObservedIPs        []string  `json:"observedIps,omitempty"`
	Quarantined        bool      `json:"quarantined"`
	UnsafeReason       string    `json:"unsafeReason,omitempty"`
}

type InstanceProxy struct {
	InstanceID string
	RawProxy   string
}

type TestRecord struct {
	InstanceID        string    `gorm:"primaryKey;size:191"`
	PublicIP          string    `gorm:"index;size:64;not null"`
	ProxyFingerprint  string    `gorm:"size:64;not null"`
	StableIP          bool      `gorm:"not null;default:false"`
	WhatsAppReachable bool      `gorm:"not null"`
	WhatsAppStatus    int       `gorm:"not null"`
	LatencyMS         int64     `gorm:"not null"`
	TestedAt          time.Time `gorm:"not null"`
	Quarantined       bool      `gorm:"not null;default:false"`
	UnsafeCount       int       `gorm:"not null;default:0"`
	UnsafeReason      string    `gorm:"size:500"`
}

func (TestRecord) TableName() string {
	return "chatwoot_proxy_tests"
}

func New(repository Repository, evolutionAPI evolution.API, proxyRequired bool) *Manager {
	return &Manager{
		repository:    repository,
		evolution:     evolutionAPI,
		ipCheckURL:    defaultIPCheckURL,
		whatsappURL:   defaultWhatsAppURL,
		timeout:       20 * time.Second,
		proxyRequired: proxyRequired,
	}
}

func (m *Manager) SetQuarantineOnUnsafe(enabled bool) {
	m.quarantineOnUnsafe = enabled
}

func (m *Manager) Get(instanceID string) (*ConfigView, error) {
	config, enabled, err := m.currentConfig(instanceID)
	if err != nil {
		return nil, err
	}
	if !enabled {
		return &ConfigView{
			Required:           m.proxyRequired,
			QuarantineOnUnsafe: m.quarantineOnUnsafe,
		}, nil
	}
	view := viewOf(config)
	view.Required = m.proxyRequired
	view.QuarantineOnUnsafe = m.quarantineOnUnsafe
	record, err := m.repository.GetProxyTest(instanceID)
	if err != nil {
		return nil, err
	}
	if record != nil && record.ProxyFingerprint == proxyFingerprint(config) {
		view.LastTest = testResultOf(record)
		duplicates, err := m.repository.FindInstancesByPublicIP(record.PublicIP, instanceID)
		if err != nil {
			return nil, err
		}
		view.LastTest.DuplicateInstances = duplicates
		view.LastTest.UniqueIP = len(duplicates) == 0
	}
	return view, nil
}

func (m *Manager) Set(ctx context.Context, instanceID string, input ConfigInput) (*ConfigView, error) {
	m.setMu.Lock()
	defer m.setMu.Unlock()

	config, err := m.resolveInput(instanceID, input)
	if err != nil {
		return nil, err
	}
	result, err := m.testConfig(ctx, instanceID, config)
	if err != nil {
		return nil, fmt.Errorf("proxy was not saved because its test failed: %w", err)
	}
	if !result.StableIP {
		return nil, fmt.Errorf("proxy was not saved because it returned different IPs across independent connections: %s", strings.Join(result.ObservedIPs, ", "))
	}
	if !result.UniqueIP {
		return nil, fmt.Errorf("proxy was not saved because IP %s is already used by instance(s): %s", result.PublicIP, strings.Join(result.DuplicateInstances, ", "))
	}
	if err := m.evolution.SetProxy(ctx, strings.TrimSpace(instanceID), config); err != nil {
		return nil, err
	}
	if err := m.repository.SaveProxyTest(recordOf(instanceID, config, result)); err != nil {
		return nil, fmt.Errorf("proxy was saved, but its verified IP could not be recorded: %w", err)
	}
	view := viewOf(config)
	view.Required = m.proxyRequired
	view.QuarantineOnUnsafe = m.quarantineOnUnsafe
	view.LastTest = result
	return view, nil
}

func (m *Manager) Remove(ctx context.Context, instanceID string) error {
	if strings.TrimSpace(instanceID) == "" {
		return errors.New("instanceId is required")
	}
	if m.proxyRequired {
		return errors.New("proxy removal is disabled because mandatory proxy mode is enabled")
	}
	instanceID = strings.TrimSpace(instanceID)
	if err := m.evolution.RemoveProxy(ctx, instanceID); err != nil {
		return err
	}
	return m.repository.DeleteProxyTest(instanceID)
}

func (m *Manager) Test(ctx context.Context, instanceID string, input ConfigInput) (*TestResult, error) {
	config, err := m.resolveInput(instanceID, input)
	if err != nil {
		return nil, err
	}
	result, err := m.testConfig(ctx, instanceID, config)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (m *Manager) testConfig(ctx context.Context, instanceID string, config evolution.ProxyConfig) (*TestResult, error) {
	started := time.Now()
	result := &TestResult{TestedAt: started.UTC()}

	firstIP, err := m.probePublicIP(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("proxy did not return a public IP: %w", sanitizeProxyError(err, config))
	}
	secondIP, err := m.probePublicIP(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("proxy failed its second independent IP check: %w", sanitizeProxyError(err, config))
	}
	result.ObservedIPs = []string{firstIP, secondIP}
	result.PublicIP = secondIP
	result.StableIP = firstIP == secondIP
	if !result.StableIP {
		result.LatencyMS = time.Since(started).Milliseconds()
		result.UnsafeReason = "proxy returned different IPs across independent connections"
		return result, nil
	}

	status, err := m.probeWhatsApp(ctx, config)
	result.LatencyMS = time.Since(started).Milliseconds()
	if err != nil {
		return nil, fmt.Errorf("proxy returned IP %s, but WhatsApp was not reachable: %w", secondIP, sanitizeProxyError(err, config))
	}
	result.WhatsAppReachable = true
	result.WhatsAppStatus = status
	result.Working = true
	duplicates, err := m.repository.FindInstancesByPublicIP(result.PublicIP, strings.TrimSpace(instanceID))
	if err != nil {
		return nil, err
	}
	result.DuplicateInstances = duplicates
	result.UniqueIP = len(duplicates) == 0
	return result, nil
}

func (m *Manager) proxyClient(config evolution.ProxyConfig) (*http.Client, *http.Transport, error) {
	proxyURL, err := buildProxyURL(config)
	if err != nil {
		return nil, nil, err
	}
	transport, err := proxyTransport(proxyURL)
	if err != nil {
		return nil, nil, err
	}
	return &http.Client{Transport: transport, Timeout: m.timeout}, transport, nil
}

func (m *Manager) probePublicIP(ctx context.Context, config evolution.ProxyConfig) (string, error) {
	client, transport, err := m.proxyClient(config)
	if err != nil {
		return "", err
	}
	defer transport.CloseIdleConnections()
	return testPublicIP(ctx, client, m.ipCheckURL)
}

func (m *Manager) probeWhatsApp(ctx context.Context, config evolution.ProxyConfig) (int, error) {
	client, transport, err := m.proxyClient(config)
	if err != nil {
		return 0, err
	}
	defer transport.CloseIdleConnections()
	return testWhatsApp(ctx, client, m.whatsappURL)
}

func recordOf(instanceID string, config evolution.ProxyConfig, result *TestResult) *TestRecord {
	return &TestRecord{
		InstanceID:        strings.TrimSpace(instanceID),
		PublicIP:          result.PublicIP,
		ProxyFingerprint:  proxyFingerprint(config),
		StableIP:          result.StableIP,
		WhatsAppReachable: result.WhatsAppReachable,
		WhatsAppStatus:    result.WhatsAppStatus,
		LatencyMS:         result.LatencyMS,
		TestedAt:          result.TestedAt,
		Quarantined:       false,
	}
}

func proxyFingerprint(config evolution.ProxyConfig) string {
	payload, _ := json.Marshal(config)
	return fmt.Sprintf("%x", sha256.Sum256(payload))
}

func testResultOf(record *TestRecord) *TestResult {
	return &TestResult{
		Working:           true,
		PublicIP:          record.PublicIP,
		WhatsAppReachable: record.WhatsAppReachable,
		WhatsAppStatus:    record.WhatsAppStatus,
		LatencyMS:         record.LatencyMS,
		TestedAt:          record.TestedAt,
		StableIP:          record.StableIP,
		ObservedIPs:       []string{record.PublicIP},
		Quarantined:       record.Quarantined,
		UnsafeReason:      record.UnsafeReason,
	}
}

func (m *Manager) RunMonitor(ctx context.Context, interval time.Duration, report func(error)) {
	if interval <= 0 {
		interval = time.Minute
	}
	run := func() {
		if err := m.MonitorOnce(ctx); err != nil && report != nil {
			report(err)
		}
	}
	run()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			run()
		}
	}
}

func (m *Manager) MonitorOnce(ctx context.Context) error {
	m.setMu.Lock()
	defer m.setMu.Unlock()

	instances, err := m.repository.ListInstanceProxies()
	if err != nil {
		return err
	}

	type observation struct {
		instance InstanceProxy
		config   evolution.ProxyConfig
		result   *TestResult
		err      error
		record   *TestRecord
	}
	observations := make(map[string]*observation, len(instances))
	ipGroups := make(map[string][]string)

	for _, instance := range instances {
		var config evolution.ProxyConfig
		if err := json.Unmarshal([]byte(instance.RawProxy), &config); err != nil {
			observations[instance.InstanceID] = &observation{instance: instance, err: fmt.Errorf("stored proxy configuration is invalid")}
			continue
		}
		config.Protocol = normalizeProtocol(config.Protocol)
		record, recordErr := m.repository.GetProxyTest(instance.InstanceID)
		if recordErr != nil {
			return recordErr
		}
		result, testErr := m.testConfig(ctx, instance.InstanceID, config)
		observations[instance.InstanceID] = &observation{instance: instance, config: config, result: result, err: testErr, record: record}
		if testErr == nil && result.StableIP {
			ipGroups[result.PublicIP] = append(ipGroups[result.PublicIP], instance.InstanceID)
		}
	}

	unsafeReasons := make(map[string]string)
	for instanceID, observation := range observations {
		if observation.err != nil {
			unsafeReasons[instanceID] = observation.err.Error()
		} else if !observation.result.StableIP {
			unsafeReasons[instanceID] = observation.result.UnsafeReason
		}
	}
	for publicIP, instanceIDs := range ipGroups {
		if len(instanceIDs) < 2 {
			continue
		}
		sort.Strings(instanceIDs)
		for _, instanceID := range instanceIDs[1:] {
			unsafeReasons[instanceID] = fmt.Sprintf("public IP %s is also used by instance %s", publicIP, instanceIDs[0])
		}
	}

	for instanceID, observation := range observations {
		reason := unsafeReasons[instanceID]
		if reason == "" {
			if observation.record != nil && observation.record.Quarantined {
				if err := m.evolution.SetProxy(ctx, instanceID, observation.config); err != nil {
					return fmt.Errorf("failed to reconnect quarantined instance %s: %w", instanceID, err)
				}
			}
			if err := m.repository.SaveProxyTest(recordOf(instanceID, observation.config, observation.result)); err != nil {
				return err
			}
			continue
		}

		unsafeCount := 1
		quarantined := false
		if observation.record != nil {
			quarantined = observation.record.Quarantined
			if observation.record.UnsafeReason == reason {
				unsafeCount = observation.record.UnsafeCount + 1
			}
		}
		if quarantined && !m.quarantineOnUnsafe {
			if err := validateConfig(observation.config); err == nil {
				if err := m.evolution.SetProxy(ctx, instanceID, observation.config); err != nil {
					return fmt.Errorf("failed to resume previously quarantined instance %s: %w", instanceID, err)
				}
				quarantined = false
			}
		}
		if m.quarantineOnUnsafe && unsafeCount >= 2 && !quarantined {
			if err := m.evolution.DisconnectInstance(ctx, instanceID); err != nil {
				return fmt.Errorf("failed to quarantine instance %s: %w", instanceID, err)
			}
			quarantined = true
		}

		record := &TestRecord{
			InstanceID:       instanceID,
			ProxyFingerprint: proxyFingerprint(observation.config),
			TestedAt:         time.Now().UTC(),
			Quarantined:      quarantined,
			UnsafeCount:      unsafeCount,
			UnsafeReason:     reason,
		}
		if observation.result != nil {
			record.PublicIP = observation.result.PublicIP
			record.StableIP = observation.result.StableIP
			record.WhatsAppReachable = observation.result.WhatsAppReachable
			record.WhatsAppStatus = observation.result.WhatsAppStatus
			record.LatencyMS = observation.result.LatencyMS
			record.TestedAt = observation.result.TestedAt
		} else if observation.record != nil {
			record.PublicIP = observation.record.PublicIP
		}
		if err := m.repository.SaveProxyTest(record); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) resolveInput(instanceID string, input ConfigInput) (evolution.ProxyConfig, error) {
	current, _, err := m.currentConfig(instanceID)
	if err != nil {
		return evolution.ProxyConfig{}, err
	}
	config := evolution.ProxyConfig{
		Protocol: normalizeProtocol(input.Protocol),
		Host:     strings.TrimSpace(input.Host),
		Port:     strings.TrimSpace(input.Port),
		Username: strings.TrimSpace(input.Username),
		Password: input.Password,
	}
	if config.Password == "" {
		config.Password = current.Password
	}
	if err := validateConfig(config); err != nil {
		return evolution.ProxyConfig{}, err
	}
	return config, nil
}

func (m *Manager) currentConfig(instanceID string) (evolution.ProxyConfig, bool, error) {
	instanceID = strings.TrimSpace(instanceID)
	if instanceID == "" {
		return evolution.ProxyConfig{}, false, errors.New("instanceId is required")
	}
	raw, err := m.repository.GetInstanceProxy(instanceID)
	if err != nil {
		return evolution.ProxyConfig{}, false, err
	}
	if strings.TrimSpace(raw) == "" {
		return evolution.ProxyConfig{}, false, nil
	}
	var config evolution.ProxyConfig
	if err := json.Unmarshal([]byte(raw), &config); err != nil {
		return evolution.ProxyConfig{}, false, fmt.Errorf("invalid proxy configuration stored for instance: %w", err)
	}
	config.Protocol = normalizeProtocol(config.Protocol)
	return config, true, nil
}

func viewOf(config evolution.ProxyConfig) *ConfigView {
	return &ConfigView{
		Enabled:     true,
		Protocol:    config.Protocol,
		Host:        config.Host,
		Port:        config.Port,
		Username:    config.Username,
		HasPassword: config.Password != "",
	}
}

func normalizeProtocol(protocol string) string {
	switch strings.ToLower(strings.TrimSpace(protocol)) {
	case "https":
		return "https"
	case "socks5":
		return "socks5"
	default:
		return "http"
	}
}

func validateConfig(config evolution.ProxyConfig) error {
	if config.Host == "" {
		return errors.New("proxy host is required")
	}
	port, err := strconv.Atoi(config.Port)
	if err != nil || port < 1 || port > 65535 {
		return errors.New("proxy port must be between 1 and 65535")
	}
	if config.Username != "" && config.Password == "" {
		return errors.New("proxy password is required when username is set")
	}
	return nil
}

func buildProxyURL(config evolution.ProxyConfig) (*url.URL, error) {
	if err := validateConfig(config); err != nil {
		return nil, err
	}
	proxyURL := &url.URL{
		Scheme: config.Protocol,
		Host:   net.JoinHostPort(config.Host, config.Port),
	}
	if config.Username != "" {
		proxyURL.User = url.UserPassword(config.Username, config.Password)
	}
	return proxyURL, nil
}

func proxyTransport(proxyURL *url.URL) (*http.Transport, error) {
	transport := &http.Transport{
		ForceAttemptHTTP2:     true,
		DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
		IdleConnTimeout:       30 * time.Second,
	}
	if proxyURL.Scheme == "socks5" {
		dialer, err := xproxy.FromURL(proxyURL, &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second})
		if err != nil {
			return nil, err
		}
		contextDialer, ok := dialer.(xproxy.ContextDialer)
		if !ok {
			return nil, errors.New("SOCKS5 proxy does not support context dialing")
		}
		transport.DialContext = contextDialer.DialContext
		return transport, nil
	}
	transport.Proxy = http.ProxyURL(proxyURL)
	return transport, nil
}

func testPublicIP(ctx context.Context, client *http.Client, endpoint string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("IP check returned HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return "", err
	}
	var payload struct {
		IP string `json:"ip"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", errors.New("IP check returned invalid JSON")
	}
	if net.ParseIP(strings.TrimSpace(payload.IP)) == nil {
		return "", errors.New("IP check returned an invalid address")
	}
	return strings.TrimSpace(payload.IP), nil
}

func testWhatsApp(ctx context.Context, client *http.Client, endpoint string) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 Chrome/126 Safari/537.36")
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	_, _ = io.CopyN(io.Discard, resp.Body, 1024)
	if resp.StatusCode >= 500 {
		return resp.StatusCode, fmt.Errorf("WhatsApp returned HTTP %d", resp.StatusCode)
	}
	return resp.StatusCode, nil
}

func sanitizeProxyError(err error, config evolution.ProxyConfig) error {
	if err == nil {
		return nil
	}
	message := err.Error()
	for _, secret := range []string{config.Password, config.Username} {
		if secret != "" {
			message = strings.ReplaceAll(message, secret, "[redacted]")
			if escaped := url.QueryEscape(secret); escaped != secret {
				message = strings.ReplaceAll(message, escaped, "[redacted]")
			}
		}
	}
	return errors.New(message)
}
