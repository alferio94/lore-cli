package httpclient

import (
	"errors"
	"strings"
	"testing"
)

func TestErrorTypesRemainActionableAndSecretSafe(t *testing.T) {
	tests := []struct {
		name    string
		msg     string
		wantAll []string
	}{
		{
			name:    "api error with request id",
			msg:     (&APIError{StatusCode: 418, Code: "teapot", Message: "brew later", RequestID: "req-1"}).Error(),
			wantAll: []string{"status=418", "code=teapot", "message=brew later", "request_id=req-1"},
		},
		{
			name:    "unauthorized with request id",
			msg:     (&UnauthorizedError{APIError: APIError{Message: "wrong token", RequestID: "req-2"}}).Error(),
			wantAll: []string{"authentication failed", "wrong token", "request_id=req-2"},
		},
		{
			name:    "unsupported server guidance",
			msg:     (&UnsupportedServerError{APIError: APIError{RequestID: "req-3"}}).Error(),
			wantAll: []string{"password login is unsupported on this server", "--token", "request_id=req-3"},
		},
		{
			name:    "readiness with request id",
			msg:     (&ReadinessError{APIError: APIError{Message: "warming up", RequestID: "req-4"}}).Error(),
			wantAll: []string{"service not ready", "warming up", "request_id=req-4"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, want := range tt.wantAll {
				if !strings.Contains(tt.msg, want) {
					t.Fatalf("error = %q, want substring %q", tt.msg, want)
				}
			}
		})
	}
}

func TestErrorTypesHandleNilReceivers(t *testing.T) {
	if got := (*APIError)(nil).Error(); got != "api error" {
		t.Fatalf("(*APIError)(nil).Error() = %q, want api error", got)
	}
	if got := (*UnauthorizedError)(nil).Error(); got != "authentication failed" {
		t.Fatalf("(*UnauthorizedError)(nil).Error() = %q, want authentication failed", got)
	}
	if got := (*UnsupportedServerError)(nil).Error(); got != "password login is unsupported on this server" {
		t.Fatalf("(*UnsupportedServerError)(nil).Error() = %q, want unsupported-server fallback", got)
	}
	if got := (*ReadinessError)(nil).Error(); got != "service not ready" {
		t.Fatalf("(*ReadinessError)(nil).Error() = %q, want service not ready", got)
	}
	if got := (*NetworkError)(nil).Error(); got != "network request failed" {
		t.Fatalf("(*NetworkError)(nil).Error() = %q, want network request failed", got)
	}
	if (*NetworkError)(nil).Unwrap() != nil {
		t.Fatal("(*NetworkError)(nil).Unwrap() != nil, want nil")
	}
}

func TestNetworkErrorUnwrapsUnderlyingError(t *testing.T) {
	base := errors.New("boom")
	err := &NetworkError{URL: "https://example.test/v1/mcp", Err: base}
	if got := err.Error(); !strings.Contains(got, "https://example.test/v1/mcp") || !strings.Contains(got, "boom") {
		t.Fatalf("Error() = %q, want URL and cause", got)
	}
	if !errors.Is(err, base) {
		t.Fatal("errors.Is(err, base) = false, want true")
	}
	if err.Unwrap() != base {
		t.Fatalf("Unwrap() = %v, want %v", err.Unwrap(), base)
	}
}
