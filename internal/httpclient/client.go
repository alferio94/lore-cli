package httpclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/alferio94/lore-cli/internal/config"
)

const defaultTimeout = 5 * time.Second

// HTTPClient implements the narrow Lore CLI HTTP surface.
type HTTPClient struct {
	baseURL string
	client  *http.Client
}

// New returns a client with normalized base URL and a finite timeout.
func New(baseURL string, timeout time.Duration) (*HTTPClient, error) {
	normalized, err := config.NormalizeServerURL(baseURL)
	if err != nil {
		return nil, err
	}
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	return &HTTPClient{
		baseURL: normalized,
		client:  &http.Client{Timeout: timeout},
	}, nil
}

// BaseURL returns the normalized server URL.
func (c *HTTPClient) BaseURL() string {
	return c.baseURL
}

// Health checks GET /healthz.
func (c *HTTPClient) Health(ctx context.Context) error {
	var body statusEnvelope
	if err := c.get(ctx, "/healthz", "", &body); err != nil {
		return err
	}
	if body.Data.Status != "ok" {
		return &APIError{StatusCode: http.StatusOK, Code: "invalid_response", Message: "healthz returned non-ok status"}
	}
	return nil
}

// Ready checks GET /readyz.
func (c *HTTPClient) Ready(ctx context.Context) error {
	var body statusEnvelope
	if err := c.get(ctx, "/readyz", "", &body); err != nil {
		return err
	}
	if body.Data.Status != "ok" {
		return &APIError{StatusCode: http.StatusOK, Code: "invalid_response", Message: "readyz returned non-ok status"}
	}
	return nil
}

// Me checks authenticated GET /v1/me.
func (c *HTTPClient) Me(ctx context.Context, token string) (Subject, error) {
	var body subjectEnvelope
	if err := c.get(ctx, "/v1/me", token, &body); err != nil {
		return Subject{}, err
	}
	return body.Data, nil
}

func (c *HTTPClient) get(ctx context.Context, path, token string, dst any) error {
	if c == nil || c.client == nil {
		return errors.New("http client is not configured")
	}
	url := c.baseURL + path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if trimmed := strings.TrimSpace(token); trimmed != "" {
		req.Header.Set("Authorization", "Bearer "+trimmed)
	}

	res, err := c.client.Do(req)
	if err != nil {
		return &NetworkError{URL: url, Err: err}
	}
	defer res.Body.Close()

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return decodeAPIError(res, path)
	}
	if err := json.NewDecoder(res.Body).Decode(dst); err != nil {
		return fmt.Errorf("decode success response from %s: %w", path, err)
	}
	return nil
}

func decodeAPIError(res *http.Response, path string) error {
	var body errorEnvelope
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		return &APIError{StatusCode: res.StatusCode, Code: "invalid_error_response", Message: fmt.Sprintf("decode error response from %s: %v", path, err)}
	}
	apiErr := APIError{
		StatusCode: res.StatusCode,
		Code:       body.Error.Code,
		Message:    body.Error.Message,
		RequestID:  body.Error.RequestID,
	}
	if res.StatusCode == http.StatusUnauthorized {
		return &UnauthorizedError{APIError: apiErr}
	}
	if res.StatusCode == http.StatusServiceUnavailable && path == "/readyz" {
		return &ReadinessError{APIError: apiErr}
	}
	return &apiErr
}
