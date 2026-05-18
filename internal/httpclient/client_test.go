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

func TestCreateMemorySuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/memories" {
			t.Fatalf("request = %s %s, want POST /v1/memories", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer secret-token" {
			t.Fatalf("Authorization = %q", got)
		}
		var req CreateMemoryRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.ProjectID != "p1" || req.Scope != "project" || req.Metadata["team"] != "cli" {
			t.Fatalf("request body = %+v", req)
		}
		writeJSON(w, http.StatusCreated, map[string]any{"data": map[string]any{"id": "m1", "project_id": "p1", "scope": "project", "type": "decision", "title": "t1", "content": "c1", "created_by": "user-1"}})
	}))
	defer server.Close()

	client := newTestClient(t, server.URL, time.Second)
	memory, err := client.CreateMemory(context.Background(), " secret-token ", CreateMemoryRequest{ProjectID: "p1", Scope: "project", Type: "decision", Title: "t1", Content: "c1", Metadata: map[string]any{"team": "cli"}})
	if err != nil {
		t.Fatalf("CreateMemory() error = %v", err)
	}
	if memory.ID != "m1" || memory.CreatedBy != "user-1" {
		t.Fatalf("memory = %+v", memory)
	}
}

func TestListMemoriesSuccessAndQueryEncoding(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/memories" {
			t.Fatalf("request = %s %s, want GET /v1/memories", r.Method, r.URL.Path)
		}
		q := r.URL.Query()
		if q.Get("project_id") != "p1" || q.Get("type") != "decision" || q.Get("scope") != "project" || q.Get("limit") != "10" {
			t.Fatalf("query = %v", q)
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": []map[string]any{{"id": "m1", "project_id": "p1", "scope": "project", "type": "decision", "title": "t1", "content": "c1", "created_by": "user-1"}}})
	}))
	defer server.Close()

	client := newTestClient(t, server.URL, time.Second)
	memories, err := client.ListMemories(context.Background(), "secret-token", ListMemoriesFilter{ProjectID: "p1", Scope: "project", Type: "decision", Limit: 10})
	if err != nil {
		t.Fatalf("ListMemories() error = %v", err)
	}
	if len(memories) != 1 || memories[0].ID != "m1" {
		t.Fatalf("memories = %+v", memories)
	}
}

func TestMemoryEndpointsReturnTypedAPIErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		status := http.StatusBadRequest
		code := "invalid_input"
		message := "invalid memory input"
		if r.Method == http.MethodGet {
			status = http.StatusInternalServerError
			code = "internal_error"
			message = "boom"
		}
		writeJSON(w, status, map[string]any{"error": map[string]any{"code": code, "message": message, "request_id": "req-memory"}})
	}))
	defer server.Close()

	client := newTestClient(t, server.URL, time.Second)
	if _, err := client.CreateMemory(context.Background(), "secret-token", CreateMemoryRequest{}); err == nil {
		t.Fatal("CreateMemory() error = nil, want APIError")
	} else {
		var apiErr *APIError
		if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusBadRequest || apiErr.RequestID != "req-memory" {
			t.Fatalf("CreateMemory() err = %T %v", err, err)
		}
	}
	if _, err := client.ListMemories(context.Background(), "secret-token", ListMemoriesFilter{ProjectID: "p1"}); err == nil {
		t.Fatal("ListMemories() error = nil, want APIError")
	} else if strings.Contains(err.Error(), "secret-token") {
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
