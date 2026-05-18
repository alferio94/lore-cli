package output

import (
	"fmt"
	"strings"

	"github.com/alferio94/lore-cli/internal/httpclient"
)

const (
	// RedactedToken is the shared placeholder used for token-safe output.
	RedactedToken = "<redacted>"

	StatusOK   = "ok"
	StatusWarn = "warn"
	StatusFail = "fail"
)

// Check is a user-visible diagnostic line.
type Check struct {
	Name   string
	Status string
	Detail string
	Action string
}

type MemoryEnvelope struct {
	Data httpclient.Memory `json:"data"`
}

type MemoriesEnvelope struct {
	Data []httpclient.Memory `json:"data"`
}

// RenderChecks formats a diagnostic report.
func RenderChecks(title string, checks []Check) string {
	var b strings.Builder
	b.WriteString(title)
	b.WriteString("\n")
	for _, check := range checks {
		b.WriteString("- [")
		b.WriteString(strings.ToUpper(check.Status))
		b.WriteString("] ")
		b.WriteString(check.Name)
		if check.Detail != "" {
			b.WriteString(": ")
			b.WriteString(check.Detail)
		}
		b.WriteString("\n")
		if check.Action != "" {
			b.WriteString("  action: ")
			b.WriteString(check.Action)
			b.WriteString("\n")
		}
	}
	return b.String()
}

// FormatSubject renders a token-safe identity summary.
func FormatSubject(subject httpclient.Subject) string {
	roles := "none"
	if len(subject.Roles) > 0 {
		roles = strings.Join(subject.Roles, ",")
	}
	return fmt.Sprintf("authenticated kind=%s user_id=%s subject_id=%s roles=%s token_source=%s token_id=%s", emptyFallback(subject.Kind, "unknown"), emptyFallback(subject.UserID, "unknown"), emptyFallback(subject.ID, "unknown"), roles, emptyFallback(subject.TokenSource, "unknown"), emptyFallback(subject.TokenID, "unknown"))
}

// FormatLoginSuccess renders a token-safe login confirmation.
func FormatLoginSuccess(subject httpclient.Subject, path string) string {
	return fmt.Sprintf("login succeeded: %s\nconfig saved to %s", FormatSubject(subject), path)
}

// FormatLogoutResult renders an idempotent local-only logout summary.
func FormatLogoutResult(path string, removed bool) string {
	if removed {
		return fmt.Sprintf("logout succeeded: removed local config at %s; no server-side token revocation was performed", path)
	}
	return fmt.Sprintf("logout succeeded: no local config remained at %s; no server-side token revocation was performed", path)
}

// FormatRememberSuccess renders a concise saved-memory confirmation.
func FormatRememberSuccess(memory httpclient.Memory) string {
	return fmt.Sprintf("remember succeeded: id=%s project_id=%s scope=%s type=%s title=%q created_by=%s", emptyFallback(memory.ID, "unknown"), emptyFallback(memory.ProjectID, "unknown"), emptyFallback(memory.Scope, "unknown"), emptyFallback(memory.Type, "unknown"), memory.Title, emptyFallback(memory.CreatedBy, "unknown"))
}

// FormatRecallResult renders a concise filtered memory list.
func FormatRecallResult(memories []httpclient.Memory) string {
	var b strings.Builder
	if len(memories) == 1 {
		fmt.Fprintf(&b, "recall returned %d memory", len(memories))
	} else {
		fmt.Fprintf(&b, "recall returned %d memories", len(memories))
	}
	b.WriteString("\n")
	for _, memory := range memories {
		fmt.Fprintf(&b, "- id=%s project_id=%s scope=%s type=%s title=%q created_by=%s\n", emptyFallback(memory.ID, "unknown"), emptyFallback(memory.ProjectID, "unknown"), emptyFallback(memory.Scope, "unknown"), emptyFallback(memory.Type, "unknown"), memory.Title, emptyFallback(memory.CreatedBy, "unknown"))
	}
	return b.String()
}

func NewMemoryEnvelope(memory httpclient.Memory) MemoryEnvelope {
	return MemoryEnvelope{Data: memory}
}

func NewMemoriesEnvelope(memories []httpclient.Memory) MemoriesEnvelope {
	return MemoriesEnvelope{Data: memories}
}

func emptyFallback(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
