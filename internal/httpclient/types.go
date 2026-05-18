package httpclient

import (
	"context"
	"fmt"
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

// Status is the common Lore status payload used by health/readiness endpoints.
type Status struct {
	Status string `json:"status"`
}

// Client defines the narrow MVP Lore HTTP surface.
type Client interface {
	Health(ctx context.Context) error
	Ready(ctx context.Context) error
	Me(ctx context.Context, token string) (Subject, error)
}

type statusEnvelope struct {
	Data Status `json:"data"`
}

type subjectEnvelope struct {
	Data Subject `json:"data"`
}

type errorEnvelope struct {
	Error struct {
		Code      string `json:"code"`
		Message   string `json:"message"`
		RequestID string `json:"request_id"`
	} `json:"error"`
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
