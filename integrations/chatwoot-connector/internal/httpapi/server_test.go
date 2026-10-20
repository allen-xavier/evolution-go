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
func (fakeEvolution) ReconnectInstance(context.Context, string) error               { return nil }
func (fakeEvolution) CheckNumbers(_ context.Context, instanceID string, numbers []string) ([]evolution.NumberCheck, error) {
	checks := make([]evolution.NumberCheck, 0, len(numbers))
	for _, number := range numbers {
		checks = append(checks, evolution.NumberCheck{
			Query:      number,
			OnWhatsApp: number == "5531999990000",
			RemoteJID:  "553199990000@s.whatsapp.net",
			JID:        instanceID,
		})
	}
	return checks, nil
}

func TestCheckNumbersRequiresAdminKeyAndAnswersPerNumber(t *testing.T) {
	router := New(fakeService{}, fakeEvolution{}, nil, nil, "global-key")

	unauthorized := httptest.NewRecorder()
	router.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodPost, "/chatwoot/check/instance-1",
		strings.NewReader(`{"numbers":["5531999990000"]}`)))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized response, got %d", unauthorized.Code)
	}

	request := httptest.NewRequest(http.MethodPost, "/chatwoot/check/instance-1",
		strings.NewReader(`{"numbers":["5531999990000"," ","5531888880000"]}`))
	request.Header.Set("apikey", "global-key")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d %s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	if !strings.Contains(body, `"query":"5531999990000","onWhatsapp":true,"jid":"instance-1"`) ||
		!strings.Contains(body, `"query":"5531888880000","onWhatsapp":false`) ||
		strings.Count(body, `"query"`) != 2 {
		t.Fatalf("unexpected response: %s", body)
	}

	empty := httptest.NewRequest(http.MethodPost, "/chatwoot/check/instance-1", strings.NewReader(`{"numbers":[]}`))
	empty.Header.Set("apikey", "global-key")
	emptyResponse := httptest.NewRecorder()
	router.ServeHTTP(emptyResponse, empty)
	if emptyResponse.Code != http.StatusBadRequest {
		t.Fatalf("expected bad request for empty list, got %d", emptyResponse.Code)
	}
}

func TestUIIsServedWithoutExposingTheAdminAPI(t *testing.T) {
	router := New(fakeService{}, fakeEvolution{}, nil, nil, "global-key")

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
	router := New(fakeService{}, fakeEvolution{}, nil, nil, "global-key")
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
