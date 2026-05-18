package httpclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
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
	if err := c.get(ctx, "/healthz", "", nil, &body); err != nil {
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
	if err := c.get(ctx, "/readyz", "", nil, &body); err != nil {
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
	if err := c.get(ctx, "/v1/me", token, nil, &body); err != nil {
		return Subject{}, err
	}
	return body.Data, nil
}

// CreateMemory posts a memory create request.
func (c *HTTPClient) CreateMemory(ctx context.Context, token string, req CreateMemoryRequest) (Memory, error) {
	var body memoryEnvelope
	if err := c.request(ctx, http.MethodPost, "/v1/memories", token, nil, req, &body); err != nil {
		return Memory{}, err
	}
	return body.Data, nil
}

// ListMemories lists memories with explicit filters.
func (c *HTTPClient) ListMemories(ctx context.Context, token string, filter ListMemoriesFilter) ([]Memory, error) {
	query := url.Values{}
	if trimmed := strings.TrimSpace(filter.ProjectID); trimmed != "" {
		query.Set("project_id", trimmed)
	}
	if trimmed := strings.TrimSpace(filter.Scope); trimmed != "" {
		query.Set("scope", trimmed)
	}
	if trimmed := strings.TrimSpace(filter.Type); trimmed != "" {
		query.Set("type", trimmed)
	}
	if filter.Limit > 0 {
		query.Set("limit", strconv.Itoa(filter.Limit))
	}

	var body memoriesEnvelope
	if err := c.get(ctx, "/v1/memories", token, query, &body); err != nil {
		return nil, err
	}
	return body.Data, nil
}

func (c *HTTPClient) get(ctx context.Context, path, token string, query url.Values, dst any) error {
	return c.request(ctx, http.MethodGet, path, token, query, nil, dst)
}

// RequestJSON performs a machine-safe authenticated JSON request for allowlisted API paths only.
func (c *HTTPClient) RequestJSON(ctx context.Context, method, path, token string, body json.RawMessage) (RequestJSONResult, error) {
	normalizedPath, err := ValidateBrokerRequest(method, path, body)
	if err != nil {
		return RequestJSONResult{}, err
	}

	res, err := c.do(ctx, strings.ToUpper(strings.TrimSpace(method)), normalizedPath, token, body)
	if err != nil {
		return RequestJSONResult{}, err
	}
	defer res.Body.Close()

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return RequestJSONResult{}, decodeAPIError(res, normalizedPath)
	}

	payload, err := io.ReadAll(res.Body)
	if err != nil {
		return RequestJSONResult{}, fmt.Errorf("read success response from %s: %w", normalizedPath, err)
	}
	if len(bytes.TrimSpace(payload)) == 0 {
		return RequestJSONResult{StatusCode: res.StatusCode, RequestID: strings.TrimSpace(res.Header.Get("X-Request-Id"))}, nil
	}

	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return RequestJSONResult{}, fmt.Errorf("decode success response from %s: %w", normalizedPath, err)
	}
	data := envelope.Data
	if len(bytes.TrimSpace(data)) == 0 {
		data = json.RawMessage("null")
	}
	return RequestJSONResult{StatusCode: res.StatusCode, RequestID: strings.TrimSpace(res.Header.Get("X-Request-Id")), Data: data}, nil
}

// ValidateBrokerRequest normalizes and validates hidden broker request inputs.
func ValidateBrokerRequest(method, rawPath string, body json.RawMessage) (string, error) {
	normalizedMethod := strings.ToUpper(strings.TrimSpace(method))
	if normalizedMethod == "" {
		return "", errors.New("method is required")
	}

	trimmedPath := strings.TrimSpace(rawPath)
	if trimmedPath == "" {
		return "", errors.New("path is required")
	}
	parsed, err := url.Parse(trimmedPath)
	if err != nil {
		return "", fmt.Errorf("path must be a valid relative API path: %w", err)
	}
	if parsed.IsAbs() || parsed.Host != "" || strings.HasPrefix(trimmedPath, "//") {
		return "", errors.New("path must be a relative API path, not a full URL")
	}
	if !strings.HasPrefix(parsed.Path, "/") {
		return "", errors.New("path must start with /")
	}
	if !isBrokerPathAllowed(normalizedMethod, parsed.Path) {
		return "", errors.New("path is not allowlisted for lore api request")
	}

	trimmedBody := bytes.TrimSpace(body)
	if len(trimmedBody) > 0 {
		if normalizedMethod == http.MethodGet || normalizedMethod == http.MethodDelete {
			return "", fmt.Errorf("%s does not accept a body", normalizedMethod)
		}
		var decoded any
		if err := json.Unmarshal(trimmedBody, &decoded); err != nil {
			return "", fmt.Errorf("body-json must be valid JSON: %w", err)
		}
		switch decoded.(type) {
		case map[string]any, []any:
		default:
			return "", errors.New("body-json must decode to a JSON object or array")
		}
	}

	requestURI := parsed.Path
	if parsed.RawQuery != "" {
		requestURI += "?" + parsed.RawQuery
	}
	return requestURI, nil
}

func isBrokerPathAllowed(method, path string) bool {
	switch {
	case method == http.MethodGet && path == "/v1/context":
		return true
	case method == http.MethodGet && path == "/v1/search":
		return true
	case method == http.MethodPost && path == "/v1/observations":
		return true
	case (method == http.MethodPatch || method == http.MethodDelete || method == http.MethodGet) && strings.HasPrefix(path, "/v1/observations/") && path != "/v1/observations/":
		return true
	case method == http.MethodGet && path == "/v1/timeline":
		return true
	case method == http.MethodGet && path == "/v1/stats":
		return true
	case method == http.MethodPost && path == "/v1/sessions":
		return true
	default:
		return false
	}
}

func (c *HTTPClient) request(ctx context.Context, method, path, token string, query url.Values, src, dst any) error {
	requestPath := path
	if len(query) > 0 {
		requestPath += "?" + query.Encode()
	}

	var body json.RawMessage
	if src != nil {
		payload, err := json.Marshal(src)
		if err != nil {
			return fmt.Errorf("encode request body for %s: %w", path, err)
		}
		body = payload
	}

	res, err := c.do(ctx, method, requestPath, token, body)
	if err != nil {
		return err
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

func (c *HTTPClient) do(ctx context.Context, method, requestPath, token string, body json.RawMessage) (*http.Response, error) {
	if c == nil || c.client == nil {
		return nil, errors.New("http client is not configured")
	}

	requestURL := c.baseURL + requestPath
	req, err := http.NewRequestWithContext(ctx, method, requestURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if len(bytes.TrimSpace(body)) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	if trimmed := strings.TrimSpace(token); trimmed != "" {
		req.Header.Set("Authorization", "Bearer "+trimmed)
	}

	res, err := c.client.Do(req)
	if err != nil {
		return nil, &NetworkError{URL: requestURL, Err: err}
	}
	return res, nil
}

func decodeAPIError(res *http.Response, path string) error {
	var body errorEnvelope
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		return &APIError{StatusCode: res.StatusCode, Code: "invalid_error_response", Message: fmt.Sprintf("decode error response from %s: %v", path, err), RequestID: strings.TrimSpace(res.Header.Get("X-Request-Id"))}
	}
	requestID := strings.TrimSpace(body.Error.RequestID)
	if requestID == "" {
		requestID = strings.TrimSpace(res.Header.Get("X-Request-Id"))
	}
	apiErr := APIError{
		StatusCode: res.StatusCode,
		Code:       body.Error.Code,
		Message:    body.Error.Message,
		RequestID:  requestID,
	}
	if res.StatusCode == http.StatusUnauthorized {
		return &UnauthorizedError{APIError: apiErr}
	}
	if res.StatusCode == http.StatusServiceUnavailable && path == "/readyz" {
		return &ReadinessError{APIError: apiErr}
	}
	return &apiErr
}
