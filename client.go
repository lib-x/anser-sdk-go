// Package anser provides a client for the Anser v1 notification API.
package anser

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const maxResponseBytes = 32 << 20

// Config configures a client. ServerURL is the installation URL without /v1.
type Config struct {
	ServerURL string
	APIKey    string
	Timeout   time.Duration
}

// Format omits the API key from formatted diagnostics.
func (Config) Format(state fmt.State, _ rune) {
	_, _ = io.WriteString(state, "anser.Config")
}

// Format omits the provider key from formatted diagnostics.
func (SpeechConfig) Format(state fmt.State, _ rune) {
	_, _ = io.WriteString(state, "anser.SpeechConfig")
}

// Client is safe for concurrent use after construction.
type Client struct {
	base string
	key  string
	http *http.Client
}

// Format avoids exposing credentials when a client is logged.
func (c *Client) Format(state fmt.State, _ rune) {
	_, _ = io.WriteString(state, "anser.Client")
}

type APIError struct {
	Status     int             `json:"-"`
	Code       string          `json:"code"`
	Message    string          `json:"message"`
	Details    json.RawMessage `json:"details"`
	RetryAfter string          `json:"-"`
}

func (e *APIError) Error() string {
	return fmt.Sprintf("anser: HTTP %d (%s)", e.Status, e.Code)
}

type SendOptions struct {
	IdempotencyKey string
	Wait           bool
	WaitTimeout    time.Duration
}

type PageOptions struct {
	Cursor string
	Limit  uint32
}

func New(config Config) (*Client, error) {
	server, err := url.Parse(config.ServerURL)
	if err != nil || server.Hostname() == "" || server.User != nil || server.ForceQuery ||
		server.RawQuery != "" || strings.Contains(config.ServerURL, "#") ||
		(server.Scheme != "http" && server.Scheme != "https") {
		return nil, errors.New("anser: invalid server URL")
	}
	if !graphic(config.APIKey) || config.Timeout < 0 {
		return nil, errors.New("anser: invalid API key or timeout")
	}
	transport := &http.Transport{
		DialContext:         (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:   true,
		MaxIdleConns:        100,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
	}
	return &Client{
		base: strings.TrimRight(server.String(), "/") + "/v1",
		key:  config.APIKey,
		http: &http.Client{
			Timeout:       cmp.Or(config.Timeout, 45*time.Second),
			Transport:     transport,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}, nil
}

// SendSpeech synthesizes and delivers an MP3. Use a global administrator credential.
func (c *Client) SendSpeech(ctx context.Context, input SpeechSend, idempotencyKey string) (Receipt, error) {
	if idempotencyKey != "" && (len(idempotencyKey) > 128 || !graphic(idempotencyKey)) {
		return Receipt{}, errors.New("anser: invalid idempotency key")
	}
	return request[Receipt](ctx, c, http.MethodPost, "/speech/send", input, idempotencyKey)
}

func (c *Client) Send(ctx context.Context, input Publish, options SendOptions) (Receipt, error) {
	return c.deliver(ctx, "/send", input, options)
}

func (c *Client) Publish(ctx context.Context, topic string, input Publish, options SendOptions) (Receipt, error) {
	path, err := resource("/topics/", topic)
	if err != nil {
		return Receipt{}, err
	}
	return c.deliver(ctx, path+"/publish", input, options)
}

func (c *Client) Message(ctx context.Context, id string) (MessageDetail, error) {
	path, err := resource("/messages/", id)
	if err != nil {
		return MessageDetail{}, err
	}
	return request[MessageDetail](ctx, c, http.MethodGet, path, nil, "")
}

func (c *Client) Messages(ctx context.Context, options PageOptions) (Page[MessageDetail], error) {
	return request[Page[MessageDetail]](ctx, c, http.MethodGet, pagePath("/messages", options), nil, "")
}

func pagePath(path string, options PageOptions) string {
	query := url.Values{}
	if options.Cursor != "" {
		query.Set("cursor", options.Cursor)
	}
	if options.Limit != 0 {
		query.Set("limit", strconv.FormatUint(uint64(options.Limit), 10))
	}
	return withQuery(path, query)
}

func (c *Client) Templates(ctx context.Context, options PageOptions) (Page[Template], error) {
	return request[Page[Template]](ctx, c, http.MethodGet, pagePath("/templates", options), nil, "")
}

func (c *Client) Template(ctx context.Context, id string) (Template, error) {
	path, err := resource("/templates/", id)
	if err != nil {
		return Template{}, err
	}
	return request[Template](ctx, c, http.MethodGet, path, nil, "")
}

func (c *Client) CreateTemplate(ctx context.Context, input TemplateInput) (Template, error) {
	return request[Template](ctx, c, http.MethodPost, "/templates", input, "")
}

func (c *Client) UpdateTemplate(ctx context.Context, id string, input TemplateInput) (Template, error) {
	path, err := resource("/templates/", id)
	if err != nil {
		return Template{}, err
	}
	return request[Template](ctx, c, http.MethodPut, path, input, "")
}

func (c *Client) DeleteTemplate(ctx context.Context, id string) error {
	path, err := resource("/templates/", id)
	if err != nil {
		return err
	}
	_, err = request[struct{}](ctx, c, http.MethodDelete, path, nil, "")
	return err
}

func (c *Client) Subscribe(ctx context.Context, topic string, input SubscriptionInput) (Subscription, error) {
	path, err := resource("/topics/", topic)
	if err != nil {
		return Subscription{}, err
	}
	return request[Subscription](ctx, c, http.MethodPost, path+"/subscriptions", input, "")
}

func (c *Client) Unsubscribe(ctx context.Context, topic, id string) error {
	path, err := resource("/topics/", topic)
	if err != nil {
		return err
	}
	path, err = resource(path+"/subscriptions/", id)
	if err != nil {
		return err
	}
	_, err = request[struct{}](ctx, c, http.MethodDelete, path, nil, "")
	return err
}

func (c *Client) deliver(ctx context.Context, path string, input Publish, options SendOptions) (Receipt, error) {
	if len(options.IdempotencyKey) > 128 || (options.IdempotencyKey != "" && !graphic(options.IdempotencyKey)) {
		return Receipt{}, errors.New("anser: invalid idempotency key")
	}
	if input.TemplateID != "" && input.TemplateName != "" {
		return Receipt{}, errors.New("anser: choose a template ID or name")
	}
	if input.TemplateID != "" || input.TemplateName != "" {
		if input.Title != "" || input.Body != "" || input.Format != "" {
			return Receipt{}, errors.New("anser: the template supplies title, body and format")
		}
	} else if len(input.Variables) != 0 {
		return Receipt{}, errors.New("anser: template variables require a template ID or name")
	}

	if len(input.Channels) > 0 && len(input.Targets) > 0 {
		return Receipt{}, errors.New("anser: choose channels or targets")
	}
	var channels any
	if len(input.Channels) > 0 {
		channels = input.Channels
	}
	if len(input.Targets) > 0 {
		channels = input.Targets
	}
	body := struct {
		Publish
		Channels any `json:"channels,omitempty"`
	}{input, channels}
	query := url.Values{}
	if options.Wait {
		query.Set("wait", "true")
		if options.WaitTimeout != 0 {
			query.Set("timeout_ms", strconv.FormatInt(max(1, min(30000, options.WaitTimeout.Milliseconds())), 10))
		}
	}
	return request[Receipt](ctx, c, http.MethodPost, withQuery(path, query), body, options.IdempotencyKey)
}

func graphic(value string) bool {
	return value != "" && !strings.ContainsFunc(value, func(r rune) bool { return r < '!' || r > '~' })
}

func resource(prefix, id string) (string, error) {
	if id == "" || id == "." || id == ".." {
		return "", errors.New("anser: invalid path segment")
	}
	return prefix + url.PathEscape(id), nil
}

func withQuery(path string, query url.Values) string {
	if len(query) == 0 {
		return path
	}
	return path + "?" + query.Encode()
}

func request[T any](ctx context.Context, c *Client, method, path string, input any, idempotencyKey string) (T, error) {
	var result T
	var body io.Reader
	if input != nil {
		data, err := json.Marshal(input)
		if err != nil {
			return result, fmt.Errorf("anser: encode request: %w", err)
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if err != nil {
		return result, errors.New("anser: invalid request")
	}
	req.Header.Set("Authorization", "Bearer "+c.key)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if idempotencyKey != "" {
		req.Header.Set("Idempotency-Key", idempotencyKey)
	}

	response, err := c.http.Do(req)
	if err != nil {
		return result, fmt.Errorf("anser: request failed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNoContent {
		return result, nil
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return result, fmt.Errorf("anser: read response: %w", err)
	}
	if len(data) > maxResponseBytes {
		return result, errors.New("anser: response too large")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		api := &APIError{Status: response.StatusCode, RetryAfter: response.Header.Get("Retry-After")}
		if json.Unmarshal(data, api) != nil || api.Code == "" {
			api.Code = "http_error"
		}
		return result, api
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return result, fmt.Errorf("anser: decode response: %w", err)
	}
	return result, nil
}
