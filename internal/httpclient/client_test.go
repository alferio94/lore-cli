package httpclient

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNewNormalizesBaseURL(t *testing.T) {
	client, err := New(" https://example.test/// ", 0)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if got, want := client.BaseURL(), "https://example.test"; got != want {
		t.Fatalf("BaseURL() = %q, want %q", got, want)
	}
	if got, want := client.client.Timeout, defaultTimeout; got != want {
		t.Fatalf("timeout = %s, want %s", got, want)
	}
}

func TestHealthSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			t.Fatalf("path = %q, want /healthz", r.URL.Path)
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"status": "ok"}})
	}))
	defer server.Close()

	client := newTestClient(t, server.URL, time.Second)
	if err := client.Health(context.Background()); err != nil {
		t.Fatalf("Health() error = %v", err)
	}
}

func TestReadyServiceUnavailableReturnsReadinessError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"error": map[string]any{
				"code":       "service_unavailable",
				"message":    "service not ready",
				"request_id": "req-ready-1",
			},
		})
	}))
	defer server.Close()

	client := newTestClient(t, server.URL, time.Second)
	err := client.Ready(context.Background())
	var readyErr *ReadinessError
	if !errors.As(err, &readyErr) {
		t.Fatalf("Ready() error = %T %v, want *ReadinessError", err, err)
	}
	if readyErr.Code != "service_unavailable" || readyErr.RequestID != "req-ready-1" {
		t.Fatalf("ReadinessError = %+v, want code and request_id", readyErr)
	}
}

func TestMeSuccessSetsAuthorizationHeader(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.Header.Get("Authorization"), "Bearer secret-token"; got != want {
			t.Fatalf("Authorization header = %q, want %q", got, want)
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{
			"id":           "subject-1",
			"user_id":      "user-1",
			"roles":        []string{"admin"},
			"token_id":     "token-1",
			"token_source": "api_token",
			"kind":         "user",
		}})
	}))
	defer server.Close()

	client := newTestClient(t, server.URL, time.Second)
	subject, err := client.Me(context.Background(), " secret-token ")
	if err != nil {
		t.Fatalf("Me() error = %v", err)
	}
	if subject.UserID != "user-1" || subject.TokenID != "token-1" {
		t.Fatalf("Subject = %+v, want decoded body", subject)
	}
}

func TestMeUnauthorizedReturnsTokenSafeTypedError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{
			"error": map[string]any{
				"code":       "unauthorized",
				"message":    "normal user API token required",
				"request_id": "req-me-401",
			},
		})
	}))
	defer server.Close()

	client := newTestClient(t, server.URL, time.Second)
	_, err := client.Me(context.Background(), "secret-token")
	var unauthorized *UnauthorizedError
	if !errors.As(err, &unauthorized) {
		t.Fatalf("Me() error = %T %v, want *UnauthorizedError", err, err)
	}
	if unauthorized.RequestID != "req-me-401" {
		t.Fatalf("RequestID = %q, want req-me-401", unauthorized.RequestID)
	}
	if strings.Contains(err.Error(), "secret-token") {
		t.Fatalf("error leaked token: %v", err)
	}
}

func TestHealthServerErrorReturnsAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": map[string]any{
				"code":       "internal_error",
				"message":    "internal server error",
				"request_id": "req-health-500",
			},
		})
	}))
	defer server.Close()

	client := newTestClient(t, server.URL, time.Second)
	err := client.Health(context.Background())
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("Health() error = %T %v, want *APIError", err, err)
	}
	if apiErr.StatusCode != http.StatusInternalServerError || apiErr.RequestID != "req-health-500" {
		t.Fatalf("APIError = %+v, want status and request_id", apiErr)
	}
}

func TestMeTimeoutReturnsNetworkErrorWithoutTokenLeak(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(150 * time.Millisecond)
		writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"id": "late"}})
	}))
	defer server.Close()

	client := newTestClient(t, server.URL, 25*time.Millisecond)
	_, err := client.Me(context.Background(), "secret-token")
	var networkErr *NetworkError
	if !errors.As(err, &networkErr) {
		t.Fatalf("Me() error = %T %v, want *NetworkError", err, err)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want deadline exceeded", err)
	}
	if strings.Contains(err.Error(), "secret-token") {
		t.Fatalf("error leaked token: %v", err)
	}
}

func newTestClient(t *testing.T, baseURL string, timeout time.Duration) *HTTPClient {
	t.Helper()
	client, err := New(baseURL, timeout)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return client
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		panic(err)
	}
}
