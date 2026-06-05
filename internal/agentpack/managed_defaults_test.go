package agentpack

import (
	"strings"
	"testing"
)

// TestDefaultManagedAgentAssetsEmitCanonicalPiEnvelopeContract verifies that every managed
// agent asset emitted by `defaultManagedAgentAssets()` teaches the canonical Pi Lore delegation
// adapter contract and forbids obsolete response fields. This guards against drift between
// the generator and the runtime builtin agent prompts in `lore-pi-runtime/agents/*.md`.
func TestDefaultManagedAgentAssetsEmitCanonicalPiEnvelopeContract(t *testing.T) {
	assets := defaultManagedAgentAssets()
	if len(assets) != 10 {
		t.Fatalf("len(defaultManagedAgentAssets()) = %d, want 10 canonical overlays (lore-worker + 9 SDD phases)", len(assets))
	}

	// The first asset is the lore-worker; the rest are the 9 SDD phase overlays.
	wantFirstName := RoleLoreWorker
	if got := assets[0].Name; got != wantFirstName {
		t.Fatalf("assets[0].Name = %q, want %q", got, wantFirstName)
	}
	wantOrder := []string{
		"sdd-init", "sdd-explore", "sdd-propose", "sdd-spec", "sdd-design",
		"sdd-tasks", "sdd-apply", "sdd-verify", "sdd-archive",
	}
	for i, want := range wantOrder {
		if got := assets[i+1].Name; got != want {
			t.Fatalf("assets[%d].Name = %q, want %q", i+1, got, want)
		}
	}

	// Forbidden canonical-field patterns (must not appear in any body).
	forbiddenFieldPatterns := []struct {
		name    string
		pattern string
	}{
		{"old next-only worker key list", "exactly these keys: `status`, `summary`, `artifacts`, `next`, "},
		{"old SDD key list with next", "envelope with keys `status`, `phase`, `summary`, `artifacts`, `next`, "},
		{"old status-with-running pattern", "`status`: `completed` | `running` | `needs_user_input` | `failed`"},
		{"old final-status-with-running pattern", "Final output status must be one of: `completed`, `running`, `needs_user_input`, `failed`"},
	}

	// Required canonical snippets.
	requiredFieldList := "Return ONLY one JSON object with exactly these keys: `status`, `summary`, `artifacts`, `files`, `validations`, `risks`, `next_step`, `continuation`, `question`, `options`, `skill_resolution`."
	// Final-status wording appears in two forms: the explicit "Final output status must be one of:"
	// form (used by SDD bodies) or the per-bullet "`status`: `completed` | `needs_user_input` | `failed`" form
	// (used by the lore-worker body and the Go managed_defaults template).
	requiredFinalStatus := "Final output status must be one of: `completed`, `needs_user_input`, `failed`"
	requiredFinalStatusBullet := "`status`: `completed` | `needs_user_input` | `failed`"
	requiredDoNotUse := "Do not use `running`, `next`, `executive_summary`, or `next_recommended`"
	requiredDoNotUseShort := "Do not use `next`, `executive_summary`, or `next_recommended`"
	requiredRuntimeOwnership := "Delegation is provided by the `lore-pi-runtime` package"
	requiredPiAdapter := "This is the Pi Lore delegation adapter contract"
	requiredSkillResolution := "`skill_resolution`: `injected` | `fallback-registry` | `fallback-path` | `none`"
	// The SDD agents only mention `skill_resolution` as a backtick-quoted field name in the
	// canonical field list; the full skill_resolution value set is added by the runtime-injected
	// contract at build time. Accept either form (worker bullet or SDD backtick-quoted name).
	requiredSkillResolutionExample := "`skill_resolution`"
	// SDD bodies use the same canonical field list as the worker, but with `phase` added
	// in the key list and `set \`phase\` to ...` example in the body.
	requiredSDDFieldList := "Return ONLY the compact SDD JSON envelope with keys `status`, `phase`, `summary`, `artifacts`, `files`, `validations`, `risks`, `next_step`, `continuation`, `question`, `options`, `skill_resolution`"
	requiredSDDFinalStatus := "Final output status must be one of: `completed`, `needs_user_input`, `failed`"

	for _, asset := range assets {
		body := asset.Body.Template
		isWorker := asset.Name == RoleLoreWorker
		isSDD := asset.Role == "sdd"

		// Required canonical snippets (always present, both worker and SDD).
		if !contains(body, requiredFinalStatus) && !contains(body, requiredFinalStatusBullet) {
			t.Fatalf("managed asset %q body = %q, want canonical final-status wording", asset.Name, body)
		}
		if !contains(body, requiredDoNotUse) && !contains(body, requiredDoNotUseShort) {
			t.Fatalf("managed asset %q body = %q, want obsolete-field do-not-use warning", asset.Name, body)
		}
		if !contains(body, requiredRuntimeOwnership) {
			t.Fatalf("managed asset %q body = %q, want runtime-ownership note", asset.Name, body)
		}
		if !contains(body, requiredPiAdapter) {
			t.Fatalf("managed asset %q body = %q, want Pi adapter contract label", asset.Name, body)
		}
		if !contains(body, requiredSkillResolution) && !contains(body, requiredSkillResolutionExample) {
			t.Fatalf("managed asset %q body = %q, want canonical skill_resolution set or example", asset.Name, body)
		}

		// Field list and "final running" wording: worker uses one form, SDD uses another.
		switch {
		case isWorker:
			if !contains(body, requiredFieldList) {
				t.Fatalf("managed worker %q body = %q, want canonical worker field list", asset.Name, body)
			}
		case isSDD:
			if !contains(body, requiredSDDFieldList) {
				t.Fatalf("managed SDD %q body = %q, want canonical SDD field list", asset.Name, body)
			}
			if !contains(body, requiredSDDFinalStatus) && !contains(body, requiredFinalStatusBullet) {
				t.Fatalf("managed SDD %q body = %q, want canonical SDD final-status wording", asset.Name, body)
			}
		}

		// Forbidden patterns: every body must reject the old contract.
		for _, forbidden := range forbiddenFieldPatterns {
			if contains(body, forbidden.pattern) {
				t.Fatalf("managed asset %q body = %q, want obsolete response contract fragment %q omitted", asset.Name, body, forbidden.pattern)
			}
		}

		// Forbidden response-field labels in the canonical contract section. Accept the
		// "do not use" warning wording since it is the only legitimate mention of these words.
		contractSection := extractResponseContractSection(body)
		if contractSection != "" {
			for _, forbidden := range []string{"`executive_summary`", "`next_recommended`"} {
				if contains(contractSection, forbidden) {
					t.Fatalf("managed asset %q contract section teaches forbidden canonical field %q", asset.Name, forbidden)
				}
			}
		}
	}
}

// TestDefaultManagedAgentAssetsPhaseMappingIsConsistent verifies that the SDD phase body
// example value (`set \`phase\` to \`<X>\“) matches the render-time mapped phase for that agent.
func TestDefaultManagedAgentAssetsPhaseMappingIsConsistent(t *testing.T) {
	assets := defaultManagedAgentAssets()

	// Map the SDD phase body phase value to the asset name. The `proposal` phase maps to
	// `sdd-propose` via the render-time alias.
	phaseToBodyPhase := map[string]string{
		"init":     "init",
		"explore":  "explore",
		"proposal": "propose",
		"spec":     "spec",
		"design":   "design",
		"tasks":    "tasks",
		"apply":    "apply",
		"verify":   "verify",
		"archive":  "archive",
	}
	phaseToAgent := map[string]string{
		"init":     "sdd-init",
		"explore":  "sdd-explore",
		"proposal": "sdd-propose",
		"spec":     "sdd-spec",
		"design":   "sdd-design",
		"tasks":    "sdd-tasks",
		"apply":    "sdd-apply",
		"verify":   "sdd-verify",
		"archive":  "sdd-archive",
	}

	for canonicalPhase, bodyPhase := range phaseToBodyPhase {
		agentName := phaseToAgent[canonicalPhase]
		var asset *AgentInstructionAsset
		for i := range assets {
			if assets[i].Name == agentName {
				asset = &assets[i]
				break
			}
		}
		if asset == nil {
			t.Fatalf("no managed asset for canonical phase %q (agent %q)", canonicalPhase, agentName)
		}
		wantSnippet := "set `phase` to `" + bodyPhase + "`"
		if !contains(asset.Body.Template, wantSnippet) {
			t.Fatalf("managed %q body = %q, want %q", agentName, asset.Body.Template, wantSnippet)
		}
	}
}

// TestDefaultManagedAgentAssetsNoDuplicateResponseContractSection verifies that the response
// contract key list appears exactly once in each body (no duplicated contract sections).
func TestDefaultManagedAgentAssetsNoDuplicateResponseContractSection(t *testing.T) {
	assets := defaultManagedAgentAssets()
	for _, asset := range assets {
		body := asset.Body.Template
		isSDD := asset.Role == "sdd"
		var snippet string
		if isSDD {
			snippet = "Return ONLY the compact SDD JSON envelope with keys `status`, `phase`, `summary`, `artifacts`, `files`, `validations`, `risks`, `next_step`, `continuation`, `question`, `options`, `skill_resolution`"
		} else {
			snippet = "Return ONLY one JSON object with exactly these keys: `status`, `summary`, `artifacts`, `files`, `validations`, `risks`, `next_step`, `continuation`, `question`, `options`, `skill_resolution`."
		}
		count := strings.Count(body, snippet)
		if count != 1 {
			t.Fatalf("managed asset %q: canonical response-contract key list must appear exactly once (got %d)", asset.Name, count)
		}
	}
}

// TestDefaultManagedAgentAssetsTeachCanonicalMemoryToolSelection verifies that every
// generated managed agent (lore-worker and all SDD phase bodies) teaches the
// harness-neutral canonical memory-tool guidance. The guidance must explicitly:
//   - prefer MCP Lore Server tools over the deprecated Pi-native `lore-memory.ts`
//     extension, which has been removed and is not available in any install path,
//   - use `lore_memory_search` for discovery, with filter-driven inputs and no
//     query text,
//   - teach that `lore_memory_search` accepts exactly one of `project_id` /
//     `project_key` and prefers `project_key` when a stable key is known,
//   - teach that search returns compact `content_preview` and omits full `content`,
//   - teach that full-body retrieval requires `lore_memory_get` with `project_id`
//     plus memory `id` (and that `project_key` is not a supported substitute),
//   - reserve harness-local fallback tools for cases when MCP is unavailable.
func TestDefaultManagedAgentAssetsTeachCanonicalMemoryToolSelection(t *testing.T) {
	assets := defaultManagedAgentAssets()
	if len(assets) == 0 {
		t.Fatal("defaultManagedAgentAssets() returned no assets")
	}

	workerRequired := []string{
		"## Lore memory tool selection (canonical)",
		"Prefer MCP Lore Server tools (`lore_memory_*`) over any deprecated harness-local memory extension.",
		"Use `lore_memory_search` for memory discovery.",
		"`lore_memory_search` accepts exactly one of `project_id` (UUID) or `project_key` per call.",
		"Prefer `project_key` when a stable key is known",
		"`lore_memory_search` returns compact `content_preview` entries and OMITS full `content`.",
		"call `lore_memory_get` with `project_id` (UUID) plus the memory `id`",
		"`lore_memory_get` requires a `project_id`; passing `project_key` is not a supported substitute.",
		"Harness-local or harness-native fallback tools",
		"MUST only be used when MCP Lore Server tools are unavailable.",
		"The Pi-native `lore-memory.ts` extension was removed and is not available in any install path",
	}
	sddRequired := []string{
		"Lore memory tool selection (canonical):",
		"Prefer MCP Lore Server tools (`lore_memory_*`) over deprecated harness-local memory extensions.",
		"Use `lore_memory_search` for discovery.",
		"pass `type`, `scope`, and `limit`; do not pass query text",
		"compact `content_preview` and OMITS full `content`",
		"`lore_memory_search` accepts exactly one of `project_id` (UUID) or `project_key` per call.",
		"Prefer `project_key` when a stable key is known",
		"call `lore_memory_get` with `project_id` (UUID) plus the memory `id`",
		"`lore_memory_get` requires `project_id`; passing `project_key` is not a supported substitute.",
		"MUST only be used when MCP Lore Server tools are unavailable.",
		"The Pi-native `lore-memory.ts` extension was removed and is not available in any install path",
	}

	for _, asset := range assets {
		body := asset.Body.Template
		isWorker := asset.Name == RoleLoreWorker
		isSDD := asset.Role == "sdd"
		var required []string
		switch {
		case isWorker:
			required = workerRequired
		case isSDD:
			required = sddRequired
		default:
			continue
		}
		for _, want := range required {
			if !contains(body, want) {
				t.Fatalf("managed asset %q body missing canonical memory-tool snippet %q", asset.Name, want)
			}
		}
	}
}

// extractResponseContractSection returns a window of the body that includes the canonical
// response-contract key list but excludes the "Do not use" warning line, since that warning
// legitimately mentions the obsolete field names. This is best-effort.
func extractResponseContractSection(body string) string {
	// Find the "Return ONLY ..." line; the contract section ends at the next "## " heading OR at
	// the "Do not use ..." line, whichever comes first.
	start := -1
	for _, marker := range []string{"Return ONLY one JSON object with exactly these keys:", "Return ONLY the compact SDD JSON envelope with keys"} {
		idx := strings.Index(body, marker)
		if idx != -1 {
			start = idx
			break
		}
	}
	if start == -1 {
		return ""
	}
	rest := body[start:]
	// End at the first "Do not use" line (case-insensitive) so the warning is excluded.
	lowerRest := strings.ToLower(rest)
	doNotUseIdx := strings.Index(lowerRest, "do not use")
	// End at the next "## " heading.
	headingIdx := strings.Index(rest, "\n## ")
	end := len(rest)
	if doNotUseIdx != -1 && doNotUseIdx < end {
		end = doNotUseIdx
	}
	if headingIdx != -1 && headingIdx < end {
		end = headingIdx
	}
	return rest[:end]
}
