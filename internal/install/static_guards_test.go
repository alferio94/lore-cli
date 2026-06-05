package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoActiveSourceTeachesStalePiEnvelopeContract is a focused static guard for the
// spec invariant: no active (non-test) Go file under `internal/` may teach the old
// Pi delegation envelope contract. Forbidden patterns include the old `next` field,
// the old `running` final-status wording, the old SDD key list with `next`, the old
// worker key list with `next`, and the old explicit `next` field. Test files are
// excluded because they may legitimately mention these patterns in negative regression
// assertions or historical references.
func TestNoActiveSourceTeachesStalePiEnvelopeContract(t *testing.T) {
	repoRoot, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	// Walk only internal/ — the same scope as the OpenCode guard.
	absRoot := filepath.Join(repoRoot, "internal")
	_ = filepath.WalkDir(absRoot, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		name := filepath.Base(path)
		if !strings.HasSuffix(name, ".go") {
			return nil
		}
		if strings.HasSuffix(name, "_test.go") {
			return nil
		}
		rel, _ := filepath.Rel(repoRoot, path)
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		text := string(content)

		// Forbidden canonical-field patterns (the old envelope shape with `next`).
		forbidden := []struct {
			name    string
			pattern string
		}{
			{"old next-only worker key list", "exactly these keys: `status`, `summary`, `artifacts`, `next`, "},
			{"old next+continuation worker key list", "exactly these keys: `status`, `summary`, `artifacts`, `next`, `continuation`"},
			{"old SDD envelope with next", "envelope with keys `status`, `phase`, `summary`, `artifacts`, `next`"},
			{"old status-with-running pattern", "`status`: `completed` | `running` | `needs_user_input` | `failed`"},
			{"old final-status-with-running pattern", "Final output status must be one of: `completed`, `running`, `needs_user_input`, `failed`"},
		}
		for _, f := range forbidden {
			if strings.Contains(text, f.pattern) {
				t.Fatalf("active non-test source %s contains forbidden %s pattern: %q", rel, f.name, f.pattern)
			}
		}
		return nil
	})
}

// TestNoActiveSourceTeachesLegacyDelegationOwnership is a focused static guard for
// the spec invariant: no active (non-test) Go file under `internal/` may describe
// the legacy `lore-delegation.ts` Pi extension as the active delegation owner.
// The current owner is `lore-pi-runtime`; the legacy extension is currently
// disabled/blocked. Test files are excluded.
func TestNoActiveSourceTeachesLegacyDelegationOwnership(t *testing.T) {
	repoRoot, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	absRoot := filepath.Join(repoRoot, "internal")
	_ = filepath.WalkDir(absRoot, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		name := filepath.Base(path)
		if !strings.HasSuffix(name, ".go") {
			return nil
		}
		if strings.HasSuffix(name, "_test.go") {
			return nil
		}
		rel, _ := filepath.Rel(repoRoot, path)
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		text := string(content)
		// Forbidden ownership claims: anything that asserts the legacy
		// `lore-delegation` extension is the active owner.
		// Note: the literal string `lore-delegation` may legitimately appear
		// as the disabled extension name in `pi-runtime.contract.json` paths
		// or in comments; the test only flags the active-owner pattern.
		forbidden := []struct {
			name    string
			pattern string
		}{
			{"legacy delegation is active", "delegation is provided by the legacy `lore-delegation` extension"},
			{"legacy delegation extension is active", "the `lore-delegation` Pi extension is active"},
		}
		for _, f := range forbidden {
			if strings.Contains(text, f.pattern) {
				t.Fatalf("active non-test source %s contains forbidden %s: %q", rel, f.name, f.pattern)
			}
		}
		return nil
	})
}
