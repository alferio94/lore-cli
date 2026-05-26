package httpclient

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// Subject is the authenticated identity returned by /v1/me.
type Subject struct {
	ID          string   `json:"id"`
	UserID      string   `json:"user_id"`
	Roles       []string `json:"roles"`
	TokenID     string   `json:"token_id"`
	TokenSource string   `json:"token_source"`
	Kind        string   `json:"kind"`
}

// Memory is the Lore memory payload returned by memory endpoints.
type Memory struct {
	ID        string         `json:"id"`
	ProjectID string         `json:"project_id"`
	Scope     string         `json:"scope"`
	Type      string         `json:"type"`
	Title     string         `json:"title"`
	Content   string         `json:"content"`
	Metadata  map[string]any `json:"metadata,omitempty"`
	CreatedBy string         `json:"created_by"`
	CreatedAt time.Time      `json:"created_at,omitempty"`
	UpdatedAt time.Time      `json:"updated_at,omitempty"`
}

// CreateMemoryRequest is the REST create payload for POST /v1/memories.
type CreateMemoryRequest struct {
	ProjectID string         `json:"project_id"`
	Scope     string         `json:"scope"`
	Type      string         `json:"type"`
	Title     string         `json:"title"`
	Content   string         `json:"content"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// ListMemoriesFilter contains supported query params for GET /v1/memories.
type ListMemoriesFilter struct {
	ProjectID string
	Scope     string
	Type      string
	Limit     int
}

// Status is the common Lore status payload used by health/readiness endpoints.
type Status struct {
	Status string `json:"status"`
}

// Client defines the narrow MVP Lore HTTP surface.
type Client interface {
	Health(ctx context.Context) error
	Ready(ctx context.Context) error
	Login(ctx context.Context, email, password string) (PasswordLoginResult, error)
	Me(ctx context.Context, token string) (Subject, error)
	CreateMemory(ctx context.Context, token string, req CreateMemoryRequest) (Memory, error)
	ListMemories(ctx context.Context, token string, filter ListMemoriesFilter) ([]Memory, error)
	RequestJSON(ctx context.Context, method, path, token string, body json.RawMessage) (RequestJSONResult, error)
	MCPJSONRPC(ctx context.Context, token, method string, params json.RawMessage) (RequestJSONResult, error)
	MCPForward(ctx context.Context, token, method string, params json.RawMessage) (json.RawMessage, error)
	MCPCall(ctx context.Context, token, toolName string, arguments json.RawMessage) (RequestJSONResult, error)
}

// PasswordLoginRequest is the POST /v1/auth/login payload.
type PasswordLoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// PasswordLoginResult carries the minted reusable API token.
type PasswordLoginResult struct {
	Token string
}

type statusEnvelope struct {
	Data Status `json:"data"`
}

type subjectEnvelope struct {
	Data Subject `json:"data"`
}

type passwordLoginEnvelope struct {
	Data struct {
		APIToken struct {
			Token string `json:"token"`
		} `json:"api_token"`
	} `json:"data"`
}

type memoryEnvelope struct {
	Data Memory `json:"data"`
}

type memoriesEnvelope struct {
	Data []Memory `json:"data"`
}

type errorEnvelope struct {
	Error struct {
		Code      string `json:"code"`
		Message   string `json:"message"`
		RequestID string `json:"request_id"`
	} `json:"error"`
}

// RequestJSONResult carries token-safe machine broker results.
type RequestJSONResult struct {
	StatusCode int
	RequestID  string
	Data       json.RawMessage
}

// APIError is a token-safe error decoded from Lore error envelopes.
type APIError struct {
	StatusCode int
	Code       string
	Message    string
	RequestID  string
}

func (e *APIError) Error() string {
	if e == nil {
		return "api error"
	}
	if e.RequestID != "" {
		return fmt.Sprintf("api error: status=%d code=%s message=%s request_id=%s", e.StatusCode, e.Code, e.Message, e.RequestID)
	}
	return fmt.Sprintf("api error: status=%d code=%s message=%s", e.StatusCode, e.Code, e.Message)
}

// UnauthorizedError indicates that a provided user API token was rejected.
type UnauthorizedError struct {
	APIError
}

func (e *UnauthorizedError) Error() string {
	if e == nil {
		return "authentication failed"
	}
	if e.RequestID != "" {
		return fmt.Sprintf("authentication failed: %s (request_id=%s)", e.Message, e.RequestID)
	}
	return fmt.Sprintf("authentication failed: %s", e.Message)
}

// UnsupportedServerError indicates the server does not support password login.
type UnsupportedServerError struct {
	APIError
}

func (e *UnsupportedServerError) Error() string {
	if e == nil {
		return "password login is unsupported on this server"
	}
	message := "password login is unsupported on this server; use lore login --server <url> --token <token>"
	if e.RequestID != "" {
		return fmt.Sprintf("%s (request_id=%s)", message, e.RequestID)
	}
	return message
}

// ReadinessError indicates that the server is live but not ready.
type ReadinessError struct {
	APIError
}

func (e *ReadinessError) Error() string {
	if e == nil {
		return "service not ready"
	}
	if e.RequestID != "" {
		return fmt.Sprintf("service not ready: %s (request_id=%s)", e.Message, e.RequestID)
	}
	return fmt.Sprintf("service not ready: %s", e.Message)
}

// NetworkError indicates transport-level failures before a valid envelope was received.
type NetworkError struct {
	URL string
	Err error
}

func (e *NetworkError) Error() string {
	if e == nil {
		return "network request failed"
	}
	return fmt.Sprintf("network request failed for %s: %v", e.URL, e.Err)
}

func (e *NetworkError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// MCPForwardError is a token-safe error surfaced to the local MCP stdio bridge.
type MCPForwardError struct {
	Message string
}

func (e *MCPForwardError) Error() string {
	if e == nil || e.Message == "" {
		return "upstream MCP request failed"
	}
	return e.Message
}
