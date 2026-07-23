package proxymanager

import (
	"context"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/allen-xavier/evolution-go-chatwoot-connector/internal/evolution"
)

type fakeRepository struct {
	raw        string
	err        error
	duplicates []string
}

func (f fakeRepository) GetInstanceProxy(string) (string, error) {
	return f.raw, f.err
}
func (f fakeRepository) GetProxyTest(string) (*TestRecord, error) {
	return nil, f.err
}
func (f fakeRepository) FindInstancesByPublicIP(string, string) ([]string, error) {
	return f.duplicates, f.err
}
func (f fakeRepository) SaveProxyTest(*TestRecord) error {
	return f.err
}
func (f fakeRepository) DeleteProxyTest(string) error {
	return f.err
}
func (f fakeRepository) ListInstanceProxies() ([]InstanceProxy, error) {
	return nil, f.err
}

type fakeEvolution struct {
	setConfig       evolution.ProxyConfig
	setIDs          []string
	removedID       string
	disconnectedIDs []string
}

func (*fakeEvolution) GetInstance(context.Context, string) (*evolution.Instance, error) {
	return nil, nil
}
func (*fakeEvolution) ListInstances(context.Context) ([]evolution.Instance, error) {
	return nil, nil
}
func (*fakeEvolution) SendText(context.Context, *evolution.Instance, evolution.TextRequest) error {
	return nil
}
func (*fakeEvolution) SendMedia(context.Context, *evolution.Instance, evolution.MediaRequest) error {
	return nil
}
func (f *fakeEvolution) SetProxy(_ context.Context, instanceID string, config evolution.ProxyConfig) error {
	f.setConfig = config
	f.setIDs = append(f.setIDs, instanceID)
	return nil
}
func (f *fakeEvolution) RemoveProxy(_ context.Context, instanceID string) error {
	f.removedID = instanceID
	return nil
}
func (f *fakeEvolution) DisconnectInstance(_ context.Context, instanceID string) error {
	f.disconnectedIDs = append(f.disconnectedIDs, instanceID)
	return nil
}

type monitorRepository struct {
	configs []InstanceProxy
	records map[string]*TestRecord
}

func (r *monitorRepository) GetInstanceProxy(instanceID string) (string, error) {
	for _, config := range r.configs {
		if config.InstanceID == instanceID {
			return config.RawProxy, nil
		}
	}
	return "", nil
}
func (r *monitorRepository) GetProxyTest(instanceID string) (*TestRecord, error) {
	record := r.records[instanceID]
	if record == nil {
		return nil, nil
	}
	copy := *record
	return &copy, nil
}
func (r *monitorRepository) FindInstancesByPublicIP(publicIP, exceptInstanceID string) ([]string, error) {
	var ids []string
	for id, record := range r.records {
		if id != exceptInstanceID && record.PublicIP == publicIP {
			ids = append(ids, id)
		}
	}
	return ids, nil
}
func (r *monitorRepository) SaveProxyTest(record *TestRecord) error {
	copy := *record
	r.records[record.InstanceID] = &copy
	return nil
}
func (r *monitorRepository) DeleteProxyTest(instanceID string) error {
	delete(r.records, instanceID)
	return nil
}
func (r *monitorRepository) ListInstanceProxies() ([]InstanceProxy, error) {
	return append([]InstanceProxy(nil), r.configs...), nil
}

func TestGetMasksPasswordAndSetKeepsExistingPassword(t *testing.T) {
	api := &fakeEvolution{}
	manager := New(fakeRepository{
		raw: `{"protocol":"http","host":"proxy.example","port":"823","username":"account;sessid.one","password":"secret"}`,
	}, api, true)

	view, err := manager.Get("instance-1")
	if err != nil {
		t.Fatal(err)
	}
	if !view.Enabled || !view.HasPassword || view.Username != "account;sessid.one" {
		t.Fatalf("unexpected view: %#v", view)
	}

	resolved, err := manager.resolveInput("instance-1", ConfigInput{
		Protocol: "http",
		Host:     "proxy.example",
		Port:     "823",
		Username: "account;sessid.two",
	})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Password != "secret" || resolved.Username != "account;sessid.two" {
		t.Fatalf("stored credentials were not merged: %#v", resolved)
	}
}

func TestProxyTestUsesConfiguredProxyForIPAndWhatsApp(t *testing.T) {
	const username = "proxy-user"
	const password = "proxy-secret"
	expectedAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte(username+":"+password))

	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Proxy-Authorization") != expectedAuth {
			t.Fatalf("unexpected proxy authorization")
		}
		switch r.URL.Path {
		case "/ip":
			_, _ = w.Write([]byte(`{"ip":"203.0.113.17"}`))
		case "/wa":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("WhatsApp"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer proxyServer.Close()

	parsed, _ := url.Parse(proxyServer.URL)
	host, port, _ := net.SplitHostPort(parsed.Host)
	manager := New(fakeRepository{
		raw: fmt.Sprintf(
			`{"protocol":"http","host":%q,"port":%q,"username":%q,"password":%q}`,
			host,
			port,
			username,
			password,
		),
	}, &fakeEvolution{}, true)
	manager.ipCheckURL = "http://destination.invalid/ip"
	manager.whatsappURL = "http://destination.invalid/wa"
	manager.timeout = 2 * time.Second

	result, err := manager.Test(context.Background(), "instance-1", ConfigInput{
		Protocol: "http",
		Host:     host,
		Port:     port,
		Username: username,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Working || !result.WhatsAppReachable || result.PublicIP != "203.0.113.17" {
		t.Fatalf("unexpected proxy result: %#v", result)
	}
}

func TestProxyErrorsDoNotExposeCredentials(t *testing.T) {
	manager := New(fakeRepository{
		raw: `{"protocol":"http","host":"127.0.0.1","port":"1","username":"private-user","password":"private-password"}`,
	}, &fakeEvolution{}, true)
	manager.ipCheckURL = "http://destination.invalid/ip"
	manager.whatsappURL = "http://destination.invalid/wa"
	manager.timeout = 200 * time.Millisecond

	_, err := manager.Test(context.Background(), "instance-1", ConfigInput{
		Protocol: "http",
		Host:     "127.0.0.1",
		Port:     "1",
		Username: "private-user",
	})
	if err == nil {
		t.Fatal("expected proxy test to fail")
	}
	if strings.Contains(err.Error(), "private-user") || strings.Contains(err.Error(), "private-password") {
		t.Fatalf("proxy credentials leaked in error: %v", err)
	}
}

func TestSetRejectsAnIPAlreadyUsedByAnotherInstance(t *testing.T) {
	manager := New(fakeRepository{
		raw:        `{"protocol":"http","host":"127.0.0.1","port":"1","username":"user","password":"secret"}`,
		duplicates: []string{"instance-2"},
	}, &fakeEvolution{}, true)

	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ip":
			_, _ = w.Write([]byte(`{"ip":"203.0.113.20"}`))
		case "/wa":
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	defer proxyServer.Close()

	parsed, _ := url.Parse(proxyServer.URL)
	host, port, _ := net.SplitHostPort(parsed.Host)
	manager.repository = fakeRepository{
		raw:        fmt.Sprintf(`{"protocol":"http","host":%q,"port":%q}`, host, port),
		duplicates: []string{"instance-2"},
	}
	manager.ipCheckURL = "http://destination.invalid/ip"
	manager.whatsappURL = "http://destination.invalid/wa"

	_, err := manager.Set(context.Background(), "instance-1", ConfigInput{
		Protocol: "http",
		Host:     host,
		Port:     port,
	})
	if err == nil || !strings.Contains(err.Error(), "instance-2") {
		t.Fatalf("expected duplicate IP to be rejected, got %v", err)
	}
}

func TestMandatoryModeBlocksProxyRemoval(t *testing.T) {
	api := &fakeEvolution{}
	manager := New(fakeRepository{}, api, true)

	err := manager.Remove(context.Background(), "instance-1")
	if err == nil || !strings.Contains(err.Error(), "mandatory proxy") {
		t.Fatalf("expected proxy removal to be blocked, got %v", err)
	}
	if api.removedID != "" {
		t.Fatalf("Evolution remove endpoint should not be called, got %q", api.removedID)
	}
}

func TestProxyWithChangingIPIsRejected(t *testing.T) {
	requestCount := 0
	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ip" {
			http.NotFound(w, r)
			return
		}
		requestCount++
		if requestCount%2 == 1 {
			_, _ = w.Write([]byte(`{"ip":"203.0.113.30"}`))
		} else {
			_, _ = w.Write([]byte(`{"ip":"203.0.113.31"}`))
		}
	}))
	defer proxyServer.Close()

	parsed, _ := url.Parse(proxyServer.URL)
	host, port, _ := net.SplitHostPort(parsed.Host)
	raw := fmt.Sprintf(`{"protocol":"http","host":%q,"port":%q}`, host, port)
	manager := New(fakeRepository{raw: raw}, &fakeEvolution{}, true)
	manager.ipCheckURL = "http://destination.invalid/ip"

	result, err := manager.Test(context.Background(), "instance-1", ConfigInput{Protocol: "http", Host: host, Port: port})
	if err != nil {
		t.Fatal(err)
	}
	if result.StableIP || len(result.ObservedIPs) != 2 || result.ObservedIPs[0] == result.ObservedIPs[1] {
		t.Fatalf("expected rotating IP to be detected: %#v", result)
	}

	_, err = manager.Set(context.Background(), "instance-1", ConfigInput{Protocol: "http", Host: host, Port: port})
	if err == nil || !strings.Contains(err.Error(), "different IPs") {
		t.Fatalf("expected rotating proxy save to be blocked, got %v", err)
	}
}

func newMonitorProxy(t *testing.T, publicIP string) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ip":
			_, _ = w.Write([]byte(fmt.Sprintf(`{"ip":%q}`, publicIP)))
		case "/wa":
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	parsed, _ := url.Parse(server.URL)
	host, port, _ := net.SplitHostPort(parsed.Host)
	return fmt.Sprintf(`{"protocol":"http","host":%q,"port":%q}`, host, port)
}

func TestMonitorAlertsOnDuplicateWithoutDisconnectingActiveInstances(t *testing.T) {
	sharedRaw := newMonitorProxy(t, "203.0.113.40")

	repository := &monitorRepository{
		configs: []InstanceProxy{
			{InstanceID: "instance-1", RawProxy: sharedRaw},
			{InstanceID: "instance-2", RawProxy: sharedRaw},
		},
		records: map[string]*TestRecord{},
	}
	api := &fakeEvolution{}
	manager := New(repository, api, true)
	manager.ipCheckURL = "http://destination.invalid/ip"
	manager.whatsappURL = "http://destination.invalid/wa"

	if err := manager.MonitorOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := manager.MonitorOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := manager.MonitorOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(api.disconnectedIDs) != 0 {
		t.Fatalf("alert mode must preserve active sessions: %#v", api.disconnectedIDs)
	}
	record := repository.records["instance-2"]
	if record == nil || record.Quarantined || record.UnsafeCount != 3 || record.UnsafeReason == "" {
		t.Fatalf("expected a persistent collision alert without quarantine: %#v", record)
	}
}

func TestOptionalQuarantineReconnectsOnlyAfterUniqueIP(t *testing.T) {
	sharedRaw := newMonitorProxy(t, "203.0.113.40")
	uniqueRaw := newMonitorProxy(t, "203.0.113.41")
	repository := &monitorRepository{
		configs: []InstanceProxy{
			{InstanceID: "instance-1", RawProxy: sharedRaw},
			{InstanceID: "instance-2", RawProxy: sharedRaw},
		},
		records: map[string]*TestRecord{},
	}
	api := &fakeEvolution{}
	manager := New(repository, api, true)
	manager.SetQuarantineOnUnsafe(true)
	manager.ipCheckURL = "http://destination.invalid/ip"
	manager.whatsappURL = "http://destination.invalid/wa"

	if err := manager.MonitorOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := manager.MonitorOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(api.disconnectedIDs) != 1 || api.disconnectedIDs[0] != "instance-2" {
		t.Fatalf("expected deterministic quarantine of instance-2: %#v", api.disconnectedIDs)
	}
	if !repository.records["instance-2"].Quarantined {
		t.Fatal("instance-2 should be marked quarantined")
	}

	repository.configs[1].RawProxy = uniqueRaw
	if err := manager.MonitorOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(api.setIDs) != 1 || api.setIDs[0] != "instance-2" {
		t.Fatalf("expected instance-2 to reconnect after unique stable IP: %#v", api.setIDs)
	}
	if repository.records["instance-2"].Quarantined {
		t.Fatal("instance-2 should have left quarantine")
	}
}

func TestAlertModeResumesAnInstanceQuarantinedByStrictMode(t *testing.T) {
	sharedRaw := newMonitorProxy(t, "203.0.113.40")
	repository := &monitorRepository{
		configs: []InstanceProxy{
			{InstanceID: "instance-1", RawProxy: sharedRaw},
			{InstanceID: "instance-2", RawProxy: sharedRaw},
		},
		records: map[string]*TestRecord{
			"instance-2": {
				InstanceID:   "instance-2",
				PublicIP:     "203.0.113.40",
				Quarantined:  true,
				UnsafeCount:  2,
				UnsafeReason: "public IP collision",
			},
		},
	}
	api := &fakeEvolution{}
	manager := New(repository, api, true)
	manager.ipCheckURL = "http://destination.invalid/ip"
	manager.whatsappURL = "http://destination.invalid/wa"

	if err := manager.MonitorOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(api.setIDs) != 1 || api.setIDs[0] != "instance-2" {
		t.Fatalf("expected alert mode to resume the old quarantine: %#v", api.setIDs)
	}
	if repository.records["instance-2"].Quarantined {
		t.Fatal("instance-2 should no longer be quarantined in alert mode")
	}
}
