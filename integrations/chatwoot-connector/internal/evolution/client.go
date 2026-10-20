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

type ProxyConfig struct {
	Protocol string `json:"protocol"`
	Host     string `json:"host"`
	Port     string `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
}

// NumberCheck is the result of asking WhatsApp whether a phone number has an account.
type NumberCheck struct {
	Query        string `json:"query"`
	OnWhatsApp   bool   `json:"onWhatsapp"`
	JID          string `json:"jid"`
	RemoteJID    string `json:"remoteJid"`
	VerifiedName string `json:"verifiedName"`
}

type API interface {
	GetInstance(ctx context.Context, instanceID string) (*Instance, error)
	ListInstances(ctx context.Context) ([]Instance, error)
	SendText(ctx context.Context, instance *Instance, request TextRequest) error
	SendMedia(ctx context.Context, instance *Instance, request MediaRequest) error
	SetProxy(ctx context.Context, instanceID string, config ProxyConfig) error
	RemoveProxy(ctx context.Context, instanceID string) error
	DisconnectInstance(ctx context.Context, instanceID string) error
	ReconnectInstance(ctx context.Context, instanceID string) error
	CheckNumbers(ctx context.Context, instanceID string, numbers []string) ([]NumberCheck, error)
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

func (c *Client) SetProxy(ctx context.Context, instanceID string, config ProxyConfig) error {
	instanceID = strings.TrimSpace(instanceID)
	if instanceID == "" {
		return fmt.Errorf("evolution instance id is missing")
	}
	body, err := json.Marshal(config)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.baseURL+"/instance/proxy/"+url.PathEscape(instanceID),
		bytes.NewReader(body),
	)
	if err != nil {
		return err
	}
	req.Header.Set("apikey", c.globalAPIKey)
	req.Header.Set("Content-Type", "application/json")
	return c.do(req)
}

func (c *Client) RemoveProxy(ctx context.Context, instanceID string) error {
	instanceID = strings.TrimSpace(instanceID)
	if instanceID == "" {
		return fmt.Errorf("evolution instance id is missing")
	}
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodDelete,
		c.baseURL+"/instance/proxy/"+url.PathEscape(instanceID),
		nil,
	)
	if err != nil {
		return err
	}
	req.Header.Set("apikey", c.globalAPIKey)
	return c.do(req)
}

func (c *Client) DisconnectInstance(ctx context.Context, instanceID string) error {
	instanceID = strings.TrimSpace(instanceID)
	if instanceID == "" {
		return fmt.Errorf("evolution instance id is missing")
	}
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.baseURL+"/instance/disconnect/"+url.PathEscape(instanceID),
		nil,
	)
	if err != nil {
		return err
	}
	req.Header.Set("apikey", c.globalAPIKey)
	return c.do(req)
}

func (c *Client) ReconnectInstance(ctx context.Context, instanceID string) error {
	instance, err := c.GetInstance(ctx, instanceID)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/instance/reconnect", nil)
	if err != nil {
		return err
	}
	req.Header.Set("apikey", instance.Token)
	return c.do(req)
}

// CheckNumbers asks WhatsApp, through the given instance, which numbers have an account.
// Evolution's /user/check needs the instance token, so it is looked up with the global key.
func (c *Client) CheckNumbers(ctx context.Context, instanceID string, numbers []string) ([]NumberCheck, error) {
	instance, err := c.GetInstance(ctx, instanceID)
	if err != nil {
		return nil, err
	}
	// formatJid=true makes Evolution try the formatted number first and retry the raw one
	// for numbers it did not find (covers Brazilian mobiles with and without the extra 9).
	body, err := json.Marshal(map[string]interface{}{"number": numbers, "formatJid": true})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/user/check", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("apikey", instance.Token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("evolution number check failed: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("evolution number check failed [%d]: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var result struct {
		Data *struct {
			Users []struct {
				Query        string `json:"Query"`
				IsInWhatsapp bool   `json:"IsInWhatsapp"`
				JID          string `json:"JID"`
				RemoteJID    string `json:"RemoteJID"`
				VerifiedName string `json:"VerifiedName"`
			} `json:"Users"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("invalid evolution number check response: %w", err)
	}
	// Evolution answers 200 with an empty body when the instance could not reach WhatsApp.
	if result.Data == nil {
		return nil, fmt.Errorf("evolution number check returned no result (instance offline?)")
	}
	checks := make([]NumberCheck, 0, len(result.Data.Users))
	for _, user := range result.Data.Users {
		checks = append(checks, NumberCheck{
			Query:        user.Query,
			OnWhatsApp:   user.IsInWhatsapp,
			JID:          user.JID,
			RemoteJID:    user.RemoteJID,
			VerifiedName: user.VerifiedName,
		})
	}
	return checks, nil
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
