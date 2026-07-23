package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/allen-xavier/evolution-go-chatwoot-connector/internal/evolution"
	"github.com/allen-xavier/evolution-go-chatwoot-connector/internal/model"
)

type fakeService struct{}

func (fakeService) Set(string, *model.SetChatwootPayload) (*model.ChatwootConfigView, error) {
	return &model.ChatwootConfigView{}, nil
}
func (fakeService) Find(string) (*model.ChatwootConfigView, error) {
	return &model.ChatwootConfigView{}, nil
}
func (fakeService) HandleWebhook(string, http.Header, []byte) error { return nil }
func (fakeService) HandleEvolutionEvent([]byte) error               { return nil }
func (fakeService) Run(context.Context)                             {}

type fakeEvolution struct{}

func (fakeEvolution) GetInstance(context.Context, string) (*evolution.Instance, error) {
	return &evolution.Instance{}, nil
}
func (fakeEvolution) ListInstances(context.Context) ([]evolution.Instance, error) {
	return []evolution.Instance{{
		Id:        "instance-1",
		Name:      "Comercial",
		Token:     "instance-secret-must-not-leak",
		Jid:       "5511999999999@s.whatsapp.net",
		Connected: true,
	}}, nil
}
func (fakeEvolution) SendText(context.Context, *evolution.Instance, evolution.TextRequest) error {
	return nil
}
func (fakeEvolution) SendMedia(context.Context, *evolution.Instance, evolution.MediaRequest) error {
	return nil
}
func (fakeEvolution) SetProxy(context.Context, string, evolution.ProxyConfig) error { return nil }
func (fakeEvolution) RemoveProxy(context.Context, string) error                     { return nil }
func (fakeEvolution) DisconnectInstance(context.Context, string) error              { return nil }

func TestUIIsServedWithoutExposingTheAdminAPI(t *testing.T) {
	router := New(fakeService{}, fakeEvolution{}, nil, "global-key")

	uiRequest := httptest.NewRequest(http.MethodGet, "/chatwoot", nil)
	uiResponse := httptest.NewRecorder()
	router.ServeHTTP(uiResponse, uiRequest)
	if uiResponse.Code != http.StatusOK || !strings.Contains(uiResponse.Body.String(), "Evolution + Chatwoot") {
		t.Fatalf("unexpected UI response: status=%d body=%q", uiResponse.Code, uiResponse.Body.String())
	}

	unauthorizedRequest := httptest.NewRequest(http.MethodGet, "/chatwoot/instances", nil)
	unauthorizedResponse := httptest.NewRecorder()
	router.ServeHTTP(unauthorizedResponse, unauthorizedRequest)
	if unauthorizedResponse.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized response, got %d", unauthorizedResponse.Code)
	}
}

func TestInstancesEndpointDoesNotExposeInstanceTokens(t *testing.T) {
	router := New(fakeService{}, fakeEvolution{}, nil, "global-key")
	request := httptest.NewRequest(http.MethodGet, "/chatwoot/instances", nil)
	request.Header.Set("apikey", "global-key")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", response.Code)
	}
	body := response.Body.String()
	if !strings.Contains(body, "Comercial") || !strings.Contains(body, `"connected":true`) {
		t.Fatalf("unexpected response: %s", body)
	}
	if strings.Contains(body, "instance-secret-must-not-leak") || strings.Contains(body, `"token"`) {
		t.Fatalf("instance token leaked in response: %s", body)
	}
}
