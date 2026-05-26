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

func TestLoginSuccessPostsCredentialsWithoutAuthorizationHeader(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.Method, http.MethodPost; got != want {
			t.Fatalf("method = %q, want %q", got, want)
		}
		if got, want := r.URL.Path, "/v1/auth/login"; got != want {
			t.Fatalf("path = %q, want %q", got, want)
		}
		if got := r.Header.Get("Authorization"); got != "" {
			t.Fatalf("Authorization header = %q, want empty", got)
		}
		var req PasswordLoginRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.Email != "admin@example.com" || req.Password != "secret-password" {
			t.Fatalf("request = %+v", req)
		}
		writeJSON(w, http.StatusCreated, map[string]any{"data": map[string]any{"user": map[string]any{"id": "user-1"}, "api_token": map[string]any{"token": "minted-token"}}})
	}))
	defer server.Close()

	client := newTestClient(t, server.URL, time.Second)
	result, err := client.Login(context.Background(), "admin@example.com", "secret-password")
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if result.Token != "minted-token" {
		t.Fatalf("result = %+v, want minted token", result)
	}
}

func TestLoginMapsCredentialAndInputErrorsWithoutPasswordLeak(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		code       string
		message    string
		assertErr  func(*testing.T, error)
	}{
		{name: "invalid request", statusCode: http.StatusBadRequest, code: "invalid_request", message: "invalid login input", assertErr: func(t *testing.T, err error) {
			var apiErr *APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("Login() error = %T %v, want *APIError", err, err)
			}
		}},
		{name: "unauthorized", statusCode: http.StatusUnauthorized, code: "unauthorized", message: "unauthorized", assertErr: func(t *testing.T, err error) {
			var unauthorized *UnauthorizedError
			if !errors.As(err, &unauthorized) {
				t.Fatalf("Login() error = %T %v, want *UnauthorizedError", err, err)
			}
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, tt.statusCode, map[string]any{"error": map[string]any{"code": tt.code, "message": tt.message, "request_id": "req-login"}})
			}))
			defer server.Close()

			client := newTestClient(t, server.URL, time.Second)
			_, err := client.Login(context.Background(), "admin@example.com", "secret-password")
			if err == nil {
				t.Fatal("Login() error = nil, want typed error")
			}
			tt.assertErr(t, err)
			if strings.Contains(err.Error(), "secret-password") {
				t.Fatalf("error leaked password: %v", err)
			}
		})
	}
}

func TestLoginMapsOlderServerRoutesToUnsupportedServerError(t *testing.T) {
	for _, statusCode := range []int{http.StatusNotFound, http.StatusMethodNotAllowed} {
		t.Run(http.StatusText(statusCode), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("X-Request-Id", "req-unsupported")
				writeJSON(w, statusCode, map[string]any{"error": map[string]any{"code": "not_supported", "message": "route missing"}})
			}))
			defer server.Close()

			client := newTestClient(t, server.URL, time.Second)
			_, err := client.Login(context.Background(), "admin@example.com", "secret-password")
			var unsupported *UnsupportedServerError
			if !errors.As(err, &unsupported) {
				t.Fatalf("Login() error = %T %v, want *UnsupportedServerError", err, err)
			}
			if unsupported.RequestID != "req-unsupported" {
				t.Fatalf("RequestID = %q, want req-unsupported", unsupported.RequestID)
			}
			if !strings.Contains(err.Error(), "--token") {
				t.Fatalf("error = %v, want manual token guidance", err)
			}
			if strings.Contains(err.Error(), "secret-password") {
				t.Fatalf("error leaked password: %v", err)
			}
		})
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

func TestValidateBrokerRequestRejectsUnsafeTargetsAndBodies(t *testing.T) {
	tests := []struct {
		name    string
		method  string
		path    string
		body    json.RawMessage
		wantErr string
	}{
		{name: "full url", method: http.MethodGet, path: "https://example.test/v1/memories", wantErr: "relative API path"},
		{name: "unsafe path", method: http.MethodGet, path: "/v1/me", wantErr: "allowlisted"},
		{name: "legacy path", method: http.MethodGet, path: "/v1/context", wantErr: "allowlisted"},
		{name: "deep skills route approve", method: http.MethodGet, path: "/v1/skills/name/approve", wantErr: "allowlisted"},
		{name: "deep skills route publish", method: http.MethodGet, path: "/v1/skills/name/publish", wantErr: "allowlisted"},
		{name: "deep project route", method: http.MethodGet, path: "/v1/projects/p-1/memories", wantErr: "allowlisted"},
		{name: "get body", method: http.MethodGet, path: "/v1/memories", body: json.RawMessage(`{"project_id":"lore-cli"}`), wantErr: "does not accept a body"},
		{name: "post scalar body", method: http.MethodPost, path: "/v1/memories", body: json.RawMessage(`"bad"`), wantErr: "JSON object or array"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ValidateBrokerRequest(tt.method, tt.path, tt.body)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("ValidateBrokerRequest() err = %v, want substring %q", err, tt.wantErr)
			}
		})
	}
}

func TestValidateBrokerRequestAllowsManagedMemorySkillAndProjectRoutes(t *testing.T) {
	tests := []struct {
		name   string
		method string
		path   string
	}{
		{name: "get memories", method: http.MethodGet, path: "/v1/memories?project_id=lore-cli"},
		{name: "post memories", method: http.MethodPost, path: "/v1/memories"},
		{name: "get memory by id", method: http.MethodGet, path: "/v1/memories/m-1"},
		{name: "get skills", method: http.MethodGet, path: "/v1/skills"},
		{name: "post skills", method: http.MethodPost, path: "/v1/skills"},
		{name: "get skill by name", method: http.MethodGet, path: "/v1/skills/sdd-apply"},
		{name: "get projects", method: http.MethodGet, path: "/v1/projects"},
		{name: "post projects", method: http.MethodPost, path: "/v1/projects"},
		{name: "get project by id", method: http.MethodGet, path: "/v1/projects/p-1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := ValidateBrokerRequest(tt.method, tt.path, nil); err != nil {
				t.Fatalf("ValidateBrokerRequest() error = %v", err)
			}
		})
	}
}

func TestRequestJSONUsesAllowlistedPathAndRequestID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.Method, http.MethodGet; got != want {
			t.Fatalf("method = %q, want %q", got, want)
		}
		if got, want := r.URL.RequestURI(), "/v1/memories?project_id=lore-cli"; got != want {
			t.Fatalf("request URI = %q, want %q", got, want)
		}
		if got, want := r.Header.Get("Authorization"), "Bearer secret-token"; got != want {
			t.Fatalf("Authorization = %q, want %q", got, want)
		}
		w.Header().Set("X-Request-Id", "req-memories-1")
		writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"project_id": "lore-cli"}})
	}))
	defer server.Close()

	client := newTestClient(t, server.URL, time.Second)
	result, err := client.RequestJSON(context.Background(), http.MethodGet, "/v1/memories?project_id=lore-cli", "secret-token", nil)
	if err != nil {
		t.Fatalf("RequestJSON() error = %v", err)
	}
	if result.StatusCode != http.StatusOK || result.RequestID != "req-memories-1" {
		t.Fatalf("result = %+v, want status and request id", result)
	}
	if string(result.Data) != `{"project_id":"lore-cli"}` {
		t.Fatalf("data = %s, want decoded envelope data", result.Data)
	}
}

func TestValidateBrokerMCPCallAllowsOnlyKnownTools(t *testing.T) {
	if _, _, err := ValidateBrokerMCPCall("lore_project_context", json.RawMessage(`{"project_id":"p1"}`)); err != nil {
		t.Fatalf("ValidateBrokerMCPCall allowed tool error = %v", err)
	}
	if _, _, err := ValidateBrokerMCPCall("lore_delete", json.RawMessage(`{}`)); err == nil || !strings.Contains(err.Error(), "allowlisted") {
		t.Fatalf("ValidateBrokerMCPCall rejected tool err = %v, want allowlist error", err)
	}
	if _, _, err := ValidateBrokerMCPCall("lore_project_context", json.RawMessage(`[]`)); err == nil || !strings.Contains(err.Error(), "JSON object") {
		t.Fatalf("ValidateBrokerMCPCall array args err = %v, want object error", err)
	}
}

func TestMCPCallPostsJSONRPCBodyAndDecodesResult(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.Method, http.MethodPost; got != want {
			t.Fatalf("method = %q, want %q", got, want)
		}
		if got, want := r.URL.RequestURI(), "/v1/mcp"; got != want {
			t.Fatalf("request URI = %q, want %q", got, want)
		}
		if got, want := r.Header.Get("Authorization"), "Bearer secret-token"; got != want {
			t.Fatalf("Authorization = %q, want %q", got, want)
		}
		var body struct {
			JSONRPC string `json:"jsonrpc"`
			Method  string `json:"method"`
			Params  struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			} `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body.JSONRPC != "2.0" || body.Method != "tools/call" || body.Params.Name != "lore_project_context" || body.Params.Arguments["project_id"] != "p1" {
			t.Fatalf("JSON-RPC body = %+v", body)
		}
		w.Header().Set("X-Request-Id", "req-mcp")
		writeJSON(w, http.StatusOK, map[string]any{"jsonrpc": "2.0", "id": "lore-cli-mcp-call", "result": map[string]any{"context": "ok"}})
	}))
	defer server.Close()

	client := newTestClient(t, server.URL, time.Second)
	result, err := client.MCPCall(context.Background(), "secret-token", "lore_project_context", json.RawMessage(`{"project_id":"p1"}`))
	if err != nil {
		t.Fatalf("MCPCall() error = %v", err)
	}
	if result.StatusCode != http.StatusOK || result.RequestID != "req-mcp" || string(result.Data) != `{"context":"ok"}` {
		t.Fatalf("result = %+v", result)
	}
}

func TestMCPCallConvertsJSONRPCError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Request-Id", "req-mcp")
		writeJSON(w, http.StatusOK, map[string]any{"jsonrpc": "2.0", "id": "lore-cli-mcp-call", "error": map[string]any{"code": -32602, "message": "bad args"}})
	}))
	defer server.Close()

	client := newTestClient(t, server.URL, time.Second)
	_, err := client.MCPCall(context.Background(), "secret-token", "lore_project_context", json.RawMessage(`{}`))
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Code != "-32602" || apiErr.Message != "bad args" || apiErr.RequestID != "req-mcp" {
		t.Fatalf("MCPCall() err = %T %v, want JSON-RPC APIError", err, err)
	}
}

func TestMCPJSONRPCPostsMethodAndParamsAndDecodesResult(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.Method, http.MethodPost; got != want {
			t.Fatalf("method = %q, want %q", got, want)
		}
		var body struct {
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body.Method != "tools/list" || len(body.Params) != 0 {
			t.Fatalf("JSON-RPC body = %+v", body)
		}
		writeJSON(w, http.StatusOK, map[string]any{"jsonrpc": "2.0", "id": "lore-cli-mcp", "result": map[string]any{"tools": []map[string]any{{"name": "lore_me"}}}})
	}))
	defer server.Close()

	client := newTestClient(t, server.URL, time.Second)
	result, err := client.MCPJSONRPC(context.Background(), "secret-token", "tools/list", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("MCPJSONRPC() error = %v", err)
	}
	if string(result.Data) != `{"tools":[{"name":"lore_me"}]}` {
		t.Fatalf("result.Data = %s, want tools list", result.Data)
	}
}

func TestMCPJSONRPCRejectsNonObjectParams(t *testing.T) {
	client := newTestClient(t, "https://example.test", time.Second)
	if _, err := client.MCPJSONRPC(context.Background(), "secret-token", "tools/list", json.RawMessage(`[]`)); err == nil || !strings.Contains(err.Error(), "JSON object") {
		t.Fatalf("MCPJSONRPC() err = %v, want object rejection", err)
	}
}

func TestMCPForwardReturnsResult(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"jsonrpc": "2.0", "id": "lore-cli-mcp", "result": map[string]any{"tools": []map[string]any{{"name": "lore_me"}}}})
	}))
	defer server.Close()

	client := newTestClient(t, server.URL, time.Second)
	result, err := client.MCPForward(context.Background(), "secret-token", "tools/list", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("MCPForward() error = %v", err)
	}
	if string(result) != `{"tools":[{"name":"lore_me"}]}` {
		t.Fatalf("result = %s, want tools list", result)
	}
}

func TestMCPForwardShapesTokenSafeErrors(t *testing.T) {
	tests := []struct {
		name       string
		client     func(t *testing.T) *HTTPClient
		wantSubstr string
	}{
		{
			name: "api error with request id",
			client: func(t *testing.T) *HTTPClient {
				t.Helper()
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					writeJSON(w, http.StatusUnauthorized, map[string]any{"error": map[string]any{"code": "unauthorized", "message": "invalid token", "request_id": "req-mcp-auth"}})
				}))
				t.Cleanup(server.Close)
				return newTestClient(t, server.URL, time.Second)
			},
			wantSubstr: "upstream tools/list failed: authentication failed: invalid token (request_id=req-mcp-auth)",
		},
		{
			name: "network error",
			client: func(t *testing.T) *HTTPClient {
				t.Helper()
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
				url := srv.URL
				srv.Close()
				return newTestClient(t, url, 50*time.Millisecond)
			},
			wantSubstr: "upstream tools/call failed: network request failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := tt.client(t)
			_, err := client.MCPForward(context.Background(), "secret-token", map[string]string{"api error with request id": "tools/list", "network error": "tools/call"}[tt.name], json.RawMessage(`{}`))
			var forwardErr *MCPForwardError
			if !errors.As(err, &forwardErr) || !strings.Contains(forwardErr.Error(), tt.wantSubstr) {
				t.Fatalf("MCPForward() err = %T %v, want substring %q", err, err, tt.wantSubstr)
			}
			if strings.Contains(forwardErr.Error(), "secret-token") {
				t.Fatalf("MCPForward() error leaked token: %q", forwardErr.Error())
			}
		})
	}
}

func TestRequestJSONErrorFallsBackToHeaderRequestID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Request-Id", "req-skills-500")
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": map[string]any{"code": "internal_error", "message": "boom"}})
	}))
	defer server.Close()

	client := newTestClient(t, server.URL, time.Second)
	_, err := client.RequestJSON(context.Background(), http.MethodGet, "/v1/skills", "secret-token", nil)
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("RequestJSON() error = %T %v, want *APIError", err, err)
	}
	if apiErr.RequestID != "req-skills-500" {
		t.Fatalf("RequestID = %q, want req-skills-500", apiErr.RequestID)
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
