package evolution

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGetInstanceUsesGlobalAPIKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/instance/info/instance-1" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("apikey") != "global-key" {
			t.Fatalf("unexpected api key: %s", r.Header.Get("apikey"))
		}
		_, _ = io.WriteString(w, `{"message":"success","data":{"id":"instance-1","name":"sales","token":"instance-token"}}`)
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "global-key")
	if err != nil {
		t.Fatal(err)
	}
	instance, err := client.GetInstance(context.Background(), "instance-1")
	if err != nil {
		t.Fatal(err)
	}
	if instance.Token != "instance-token" {
		t.Fatalf("unexpected instance token: %s", instance.Token)
	}
}

func TestDisconnectInstanceUsesAdministrativeEndpoint(t *testing.T) {
	var method, path, apiKey string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method = r.Method
		path = r.URL.Path
		apiKey = r.Header.Get("apikey")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "global-key")
	if err != nil {
		t.Fatal(err)
	}
	if err := client.DisconnectInstance(context.Background(), "instance 1"); err != nil {
		t.Fatal(err)
	}
	if method != http.MethodPost || path != "/instance/disconnect/instance 1" || apiKey != "global-key" {
		t.Fatalf("unexpected request: method=%s path=%s apiKey=%s", method, path, apiKey)
	}
}

func TestListInstancesUsesGlobalAPIKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/instance/all" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("apikey") != "global-key" {
			t.Fatalf("unexpected api key: %s", r.Header.Get("apikey"))
		}
		_, _ = io.WriteString(w, `{"message":"success","data":[{"id":"instance-1","name":"sales","token":"secret","jid":"5511999999999@s.whatsapp.net","connected":true}]}`)
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "global-key")
	if err != nil {
		t.Fatal(err)
	}
	instances, err := client.ListInstances(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(instances) != 1 || instances[0].Name != "sales" || !instances[0].Connected {
		t.Fatalf("unexpected instances: %#v", instances)
	}
}

func TestSendTextUsesInstanceTokenAndEvolutionPayload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/send/text" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("apikey") != "instance-token" {
			t.Fatalf("unexpected api key: %s", r.Header.Get("apikey"))
		}
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["number"] != "5511999999999" || body["text"] != "hello" || body["id"] != "message-id" {
			t.Fatalf("unexpected payload: %#v", body)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, _ := NewClient(server.URL, "global-key")
	err := client.SendText(context.Background(), &Instance{Token: "instance-token"}, TextRequest{
		Number: "5511999999999",
		Text:   "hello",
		ID:     "message-id",
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestSetAndRemoveProxyUseAdminAPI(t *testing.T) {
	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("apikey") != "global-key" {
			t.Fatalf("unexpected api key: %s", r.Header.Get("apikey"))
		}
		calls = append(calls, r.Method+" "+r.URL.Path)
		if r.Method == http.MethodPost {
			var body ProxyConfig
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body.Host != "proxy.example" || body.Password != "secret" {
				t.Fatalf("unexpected proxy payload: %#v", body)
			}
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, _ := NewClient(server.URL, "global-key")
	if err := client.SetProxy(context.Background(), "instance-1", ProxyConfig{
		Protocol: "http",
		Host:     "proxy.example",
		Port:     "823",
		Username: "user",
		Password: "secret",
	}); err != nil {
		t.Fatal(err)
	}
	if err := client.RemoveProxy(context.Background(), "instance-1"); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 ||
		calls[0] != "POST /instance/proxy/instance-1" ||
		calls[1] != "DELETE /instance/proxy/instance-1" {
		t.Fatalf("unexpected calls: %#v", calls)
	}
}

func TestSendMediaUsesMultipartContract(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/send/media" || r.Header.Get("apikey") != "instance-token" {
			t.Fatalf("unexpected request")
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatal(err)
		}
		if r.FormValue("number") != "28462999949545@lid" || r.FormValue("formatJid") != "false" {
			t.Fatalf("unexpected recipient fields: %#v", r.MultipartForm.Value)
		}
		file, _, err := r.FormFile("file")
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		data, _ := io.ReadAll(file)
		if strings.TrimSpace(string(data)) != "image-data" {
			t.Fatalf("unexpected file: %q", string(data))
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	formatJID := false
	client, _ := NewClient(server.URL, "global-key")
	err := client.SendMedia(context.Background(), &Instance{Token: "instance-token"}, MediaRequest{
		Number:    "28462999949545@lid",
		Type:      "image",
		Filename:  "photo.jpg",
		ID:        "message-id",
		FormatJID: &formatJID,
		Data:      []byte("image-data"),
	})
	if err != nil {
		t.Fatal(err)
	}
}
