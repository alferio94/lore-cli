package output

import (
	"strings"
	"testing"

	"github.com/alferio94/lore-cli/internal/httpclient"
)

func TestFormatRememberSuccessIsConcise(t *testing.T) {
	memory := httpclient.Memory{ID: "m1", ProjectID: "p1", Scope: "project", Type: "decision", Title: "Ship it", Content: "secret body", Metadata: map[string]any{"token": "secret-token"}, CreatedBy: "user-1"}
	got := FormatRememberSuccess(memory)
	for _, want := range []string{"id=m1", "project_id=p1", "type=decision", `title="Ship it"`, "created_by=user-1"} {
		if !strings.Contains(got, want) {
			t.Fatalf("output = %q, want %q", got, want)
		}
	}
	for _, unwanted := range []string{"secret body", "secret-token"} {
		if strings.Contains(got, unwanted) {
			t.Fatalf("output leaked %q: %q", unwanted, got)
		}
	}
}

func TestFormatRecallResultIsConcise(t *testing.T) {
	got := FormatRecallResult([]httpclient.Memory{{ID: "m1", ProjectID: "p1", Scope: "project", Type: "decision", Title: "t1", Content: "c1", Metadata: map[string]any{"api_token": "secret-token"}, CreatedBy: "user-1"}})
	for _, want := range []string{"recall returned 1 memory", "id=m1", "project_id=p1", `title="t1"`, "created_by=user-1"} {
		if !strings.Contains(got, want) {
			t.Fatalf("output = %q, want %q", got, want)
		}
	}
	for _, unwanted := range []string{"c1", "secret-token"} {
		if strings.Contains(got, unwanted) {
			t.Fatalf("output leaked %q: %q", unwanted, got)
		}
	}
}

func TestRenderChecksAndLoginFormattingStayUserVisibleAndSecretSafe(t *testing.T) {
	got := RenderChecks("Lore status", []Check{{Name: "config", Status: StatusOK, Detail: "saved login state", Action: "Run lore login again."}, {Name: "auth", Status: StatusFail, Detail: "token missing"}})
	for _, want := range []string{"Lore status", "- [OK] config: saved login state", "  action: Run lore login again.", "- [FAIL] auth: token missing"} {
		if !strings.Contains(got, want) {
			t.Fatalf("RenderChecks() = %q, want %q", got, want)
		}
	}

	subject := httpclient.Subject{Kind: "user", UserID: "user-1", ID: "subject-1", Roles: []string{"admin", "developer"}, TokenSource: "api_token", TokenID: "token-1"}
	login := FormatLoginSuccess(subject, "/tmp/lore/config.json")
	logout := FormatLogoutResult("/tmp/lore/config.json", true)
	for _, want := range []string{"authenticated kind=user user_id=user-1 subject_id=subject-1 roles=admin,developer token_source=api_token token_id=token-1", "OS keychain-backed login metadata saved to /tmp/lore/config.json", "removed local config at /tmp/lore/config.json", "no server-side token revocation was performed"} {
		if !strings.Contains(login+"\n"+logout, want) {
			t.Fatalf("combined output = %q, want %q", login+"\n"+logout, want)
		}
	}
}
