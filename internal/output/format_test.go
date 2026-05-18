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
