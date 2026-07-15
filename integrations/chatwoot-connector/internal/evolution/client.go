package evolution

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Instance struct {
	Id               string `json:"id"`
	Name             string `json:"name"`
	Token            string `json:"token"`
	Jid              string `json:"jid"`
	Connected        bool   `json:"connected"`
	DisconnectReason string `json:"disconnect_reason"`
}

type TextRequest struct {
	Number    string `json:"number"`
	Text      string `json:"text"`
	ID        string `json:"id"`
	FormatJID *bool  `json:"formatJid,omitempty"`
}

type MediaRequest struct {
	Number    string
	Type      string
	Caption   string
	Filename  string
	ID        string
	FormatJID *bool
	Data      []byte
}

type API interface {
	GetInstance(ctx context.Context, instanceID string) (*Instance, error)
	ListInstances(ctx context.Context) ([]Instance, error)
	SendText(ctx context.Context, instance *Instance, request TextRequest) error
	SendMedia(ctx context.Context, instance *Instance, request MediaRequest) error
}

type Client struct {
	baseURL      string
	globalAPIKey string
	httpClient   *http.Client
}

func NewClient(baseURL, globalAPIKey string) (*Client, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("invalid EVOLUTION_API_URL")
	}
	if strings.TrimSpace(globalAPIKey) == "" {
		return nil, fmt.Errorf("EVOLUTION_API_KEY is required")
	}
	return &Client{
		baseURL:      baseURL,
		globalAPIKey: globalAPIKey,
		httpClient:   &http.Client{Timeout: 60 * time.Second},
	}, nil
}

func (c *Client) GetInstance(ctx context.Context, instanceID string) (*Instance, error) {
	endpoint := c.baseURL + "/instance/info/" + url.PathEscape(strings.TrimSpace(instanceID))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("apikey", c.globalAPIKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("evolution instance lookup failed: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("evolution instance lookup failed [%d]: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var result struct {
		Data Instance `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("invalid evolution instance response: %w", err)
	}
	if result.Data.Id == "" || result.Data.Token == "" {
		return nil, fmt.Errorf("evolution instance %q did not return id/token", instanceID)
	}
	return &result.Data, nil
}

func (c *Client) ListInstances(ctx context.Context) ([]Instance, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/instance/all", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("apikey", c.globalAPIKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("evolution instance listing failed: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("evolution instance listing failed [%d]: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var result struct {
		Data []Instance `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("invalid evolution instance list response: %w", err)
	}
	return result.Data, nil
}

func (c *Client) SendText(ctx context.Context, instance *Instance, payload TextRequest) error {
	return c.sendJSON(ctx, instance, "/send/text", payload)
}

func (c *Client) SendMedia(ctx context.Context, instance *Instance, payload MediaRequest) error {
	if instance == nil || strings.TrimSpace(instance.Token) == "" {
		return fmt.Errorf("evolution instance token is missing")
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	fields := map[string]string{
		"number":   payload.Number,
		"type":     payload.Type,
		"caption":  payload.Caption,
		"filename": payload.Filename,
		"id":       payload.ID,
	}
	if payload.FormatJID != nil {
		fields["formatJid"] = strconv.FormatBool(*payload.FormatJID)
	}
	for key, value := range fields {
		if err := writer.WriteField(key, value); err != nil {
			return err
		}
	}
	part, err := writer.CreateFormFile("file", payload.Filename)
	if err != nil {
		return err
	}
	if _, err := part.Write(payload.Data); err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/send/media", &body)
	if err != nil {
		return err
	}
	req.Header.Set("apikey", instance.Token)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	return c.do(req)
}

func (c *Client) sendJSON(ctx context.Context, instance *Instance, endpoint string, payload interface{}) error {
	if instance == nil || strings.TrimSpace(instance.Token) == "" {
		return fmt.Errorf("evolution instance token is missing")
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("apikey", instance.Token)
	req.Header.Set("Content-Type", "application/json")
	return c.do(req)
}

func (c *Client) do(req *http.Request) error {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("evolution request failed: %w", err)
	}
	defer resp.Body.Close()
	body, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return readErr
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("evolution request failed [%d]: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}
