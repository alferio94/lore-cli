package install

import (
	"context"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/alferio94/lore-cli/internal/agentpack"
)

// TestNoActiveSourceTeachesStalePiEnvelopeContract is a focused static guard for the
// spec invariant: no active (non-test) Go file under `internal/` may teach the old
// Pi delegation envelope contract. Forbidden patterns include the old `next` field,
// the old `running` final-status wording, the old SDD key list with `next`, the old
// worker key list with `next`, and the old explicit `next` field. Test files are
// excluded because they may legitimately mention these patterns in negative regression
// assertions or historical references.
func TestNoActiveSourceTeachesStalePiEnvelopeContract(t *testing.T) {
	repoRoot, absRoot := repoInternalRoot(t)
	// Walk only internal/ — the same scope as the opencodeready-package
	// guard.
	if err := filepath.WalkDir(absRoot, func(path string, d os.DirEntry, walkErr error) error {
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
		rel, err := filepath.Rel(repoRoot, path)
		if err != nil {
			return err
		}
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
	}); err != nil {
		t.Fatalf("filepath.WalkDir(%q) error = %v", absRoot, err)
	}
}

// TestNoActiveSourceTeachesLegacyDelegationOwnership is a focused static guard for
// the spec invariant: no active (non-test) Go file under `internal/` may describe
// the legacy `lore-delegation.ts` Pi extension as the active delegation owner.
// The current owner is `lore-pi-runtime`; the legacy extension is currently
// disabled/blocked. Test files are excluded.
func TestNoActiveSourceTeachesLegacyDelegationOwnership(t *testing.T) {
	repoRoot, absRoot := repoInternalRoot(t)
	if err := filepath.WalkDir(absRoot, func(path string, d os.DirEntry, walkErr error) error {
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
		rel, err := filepath.Rel(repoRoot, path)
		if err != nil {
			return err
		}
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
	}); err != nil {
		t.Fatalf("filepath.WalkDir(%q) error = %v", absRoot, err)
	}
}

func repoInternalRoot(t *testing.T) (string, string) {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	for {
		goMod := filepath.Join(dir, "go.mod")
		if info, statErr := os.Stat(goMod); statErr == nil && !info.IsDir() {
			internalRoot := filepath.Join(dir, "internal")
			if info, statErr := os.Stat(internalRoot); statErr != nil {
				t.Fatalf("stat intended internal root %q: %v", internalRoot, statErr)
			} else if !info.IsDir() {
				t.Fatalf("intended internal root %q is not a directory", internalRoot)
			}
			return dir, internalRoot
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not locate repository root with go.mod from %q", dir)
		}
		dir = parent
	}
}

// TestDeprecatedLoreMemoryAssetNotEmbedded is a focused guard for the spec
// invariant: the deprecated `internal/install/assets/pi/lore-memory.ts` file MUST
// NOT be present in the install asset directory, and the embed.FS in `pi.go` MUST
// NOT be able to read it. This enforces the "not available at any moment" contract
// for the deprecated Pi-native memory extension.
func TestDeprecatedLoreMemoryAssetNotEmbedded(t *testing.T) {
	assetPath := filepath.Join("assets", "pi", "lore-memory.ts")
	if _, err := installAssets.ReadFile(assetPath); err == nil {
		t.Fatalf("installAssets.ReadFile(%q) succeeded; want deprecated asset to be removed from the embed.FS", assetPath)
	} else if !strings.Contains(err.Error(), "file does not exist") && !os.IsNotExist(err) {
		// Embed.FS ReadFile returns a *PathError wrapping fs.ErrNotExist; accept any
		// not-exist form, but reject any other failure mode.
		t.Fatalf("installAssets.ReadFile(%q) error = %v, want not-exist error", assetPath, err)
	}
}

// TestDefaultPiLayoutDoesNotIncludeLoreMemory is a focused guard for the spec
// invariant: the default Pi layout's ManagedFiles list MUST NOT include the
// deprecated `extensions/lore-memory.ts` path. The path is reserved for
// historical manifest upgrade filtering only.
func TestDefaultPiLayoutDoesNotIncludeLoreMemory(t *testing.T) {
	layout := ResolvePiLayout(t.TempDir())
	for _, managed := range layout.ManagedFiles {
		if strings.HasSuffix(managed, managedDeprecatedLoreMemoryRelativePath) || strings.Contains(managed, "lore-memory.ts") {
			t.Fatalf("layout.ManagedFiles includes deprecated lore-memory.ts: %v", layout.ManagedFiles)
		}
	}
}

// TestDefaultPiAdapterRenderDoesNotEmitLoreMemory is a focused guard for the
// spec invariant: the default Pi adapter's default-component render MUST NOT
// include any file at the deprecated `extensions/lore-memory.ts` path, even
// when the optional `pi-extensions` component is explicitly selected.
func TestDefaultPiAdapterRenderDoesNotEmitLoreMemory(t *testing.T) {
	adapter := defaultPiAdapter()
	definition := agentpack.DefaultDefinition()

	for _, components := range [][]ComponentID{
		{ComponentCorePack, ComponentLoreServerMCP, ComponentExtendedSkills},
		{ComponentCorePack, ComponentLoreServerMCP, ComponentExtendedSkills, ComponentPiExtensions},
		{ComponentCorePack, ComponentPiExtensions},
	} {
		rendered, err := adapter.Render(context.Background(), RenderRequest{
			Target:     TargetPi,
			Definition: definition,
			Components: components,
		})
		if err != nil {
			t.Fatalf("Render(%v) error = %v, want nil", components, err)
		}
		for _, file := range rendered {
			if strings.HasSuffix(file.RelativePath, "lore-memory.ts") {
				t.Fatalf("Render(%v) emitted deprecated %s; lore-memory.ts must not be rendered in any install path", components, file.RelativePath)
			}
		}
	}
}

// TestOpenCodeBundledPluginAssetsExcludeSddEngramAndLogo is a
// static guard for the spec invariant: the bundled OpenCode plugin
// assets under `internal/install/assets/opencode/plugins/` MUST
// NOT include any `sdd-engram` or `logo` plugin file. The test
// walks the embed.FS subtree and asserts no managed file basename
// resolves to an excluded plugin.
func TestOpenCodeBundledPluginAssetsExcludeSddEngramAndLogo(t *testing.T) {
	assetFS := opencodeEmbeddedAssetFS()
	entries, err := fs.ReadDir(assetFS, "assets/opencode/plugins")
	if err != nil {
		t.Fatalf("fs.ReadDir(assets/opencode/plugins) error = %v, want nil", err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		for _, excluded := range excludedOpenCodePluginNames {
			if matchesExcludedOpenCodePlugin(name, excluded) {
				t.Fatalf("bundled OpenCode plugin asset %q resolves to explicitly excluded plugin %q", name, excluded)
			}
		}
	}
}

// TestOpenCodeBundledPluginAssetsNoGentleWordingLeakage is a
// static guard for the spec invariant: the bundled OpenCode plugin
// assets MUST NOT contain any Gentle-authored copy. The test
// inspects every byte of every bundled asset (including tui.json)
// and rejects any of the documented forbidden tokens.
func TestOpenCodePromptMarkdownAssetsNotEmbedded(t *testing.T) {
	assetFS := opencodeEmbeddedAssetFS()
	if err := fs.WalkDir(assetFS, "assets/opencode", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if strings.HasPrefix(path, "assets/opencode/prompts/") || strings.HasSuffix(path, ".md") {
			t.Fatalf("OpenCode prompt asset %q is embedded; prompts must be rendered from internal/agentpack", path)
		}
		return nil
	}); err != nil {
		t.Fatalf("fs.WalkDir(assets/opencode) error = %v", err)
	}
}

func TestHarnessRenderedOutputsUseCanonicalContracts(t *testing.T) {
	for _, tc := range []struct {
		name    string
		adapter HarnessAdapter
		target  TargetID
	}{
		{name: "opencode", adapter: defaultOpenCodeAdapter(), target: TargetOpenCode},
		{name: "pi", adapter: defaultPiAdapter(), target: TargetPi},
		{name: "codex", adapter: defaultCodexAdapter(), target: TargetCodex},
		{name: "antigravity", adapter: defaultAntigravityAdapter(), target: TargetAntigravity},
	} {
		rendered, err := tc.adapter.Render(context.Background(), RenderRequest{Target: tc.target, ServerURL: "https://lore.example", SavedToken: "test-token"})
		if err != nil {
			t.Fatalf("%s Render error = %v", tc.name, err)
		}
		joined := renderedContent(rendered)
		if !strings.Contains(joined, "Lore") {
			t.Fatalf("%s rendered output missing Lore marker", tc.name)
		}
		if strings.Contains(joined, "Final output status must be one of: `completed`, `running`, `needs_user_input`, `failed`") {
			t.Fatalf("%s rendered output contains stale final-status contract", tc.name)
		}
	}
}

func renderedContent(files []RenderedFile) string {
	var b strings.Builder
	for _, file := range files {
		b.Write(file.Content)
		b.WriteByte('\n')
	}
	return b.String()
}

// TestCrossHarnessCanonicalRoleContractParity guards the role-level semantic
// contract, not byte layout: each harness owns its wrappers, while the
// canonical behavior remains equivalent for every rendered role.
func TestCrossHarnessCanonicalRoleContractParity(t *testing.T) {
	roles := renderCrossHarnessRoleContracts(t)
	phases := agentpack.OrderedPhaseIDs()

	for _, target := range []TargetID{TargetPi, TargetOpenCode, TargetCodex, TargetAntigravity} {
		target := target
		t.Run(string(target), func(t *testing.T) {
			canonical, ok := agentpack.NormalizeHarnessPrompt(string(target))
			if !ok || string(canonical) != string(target) {
				t.Fatalf("NormalizeHarnessPrompt(%q) = %q, %t; want canonical target", target, canonical, ok)
			}

			wantRoles := []string{agentpack.RoleLoreWorker}
			if target != TargetPi { // Pi has managed role overlays but no CLI-rendered orchestrator.
				wantRoles = append([]string{agentpack.RoleOrchestrator}, wantRoles...)
			}
			for _, phase := range phases {
				wantRoles = append(wantRoles, agentpack.PhaseAgentName(phase))
			}
			for _, role := range wantRoles {
				content, exists := roles[target][role]
				if !exists {
					t.Fatalf("missing rendered target-role contract target=%s role=%s; got roles %v", target, role, sortedCrossHarnessRoles(roles[target]))
				}
				if role == agentpack.RoleOrchestrator {
					assertCanonicalOrchestratorContract(t, target, content)
					continue
				}

				assertCanonicalLoreMCPRetrievalContract(t, target, role, content)
				assertCanonicalSkillContract(t, target, role, content)
				assertCanonicalEnvelopeContract(t, target, role, content)
				if phase, isPhase := agentpack.PhaseForAgentName(role); isPhase {
					assertCanonicalArtifactOwnerPersistence(t, target, role, content)
					assertCanonicalPhaseContract(t, target, role, phase, content)
				} else {
					assertGenericWorkerLeastPrivilege(t, target, role, content)
				}
			}
		})
	}

	for _, unknown := range []string{"", "claude-code", "unknown"} {
		if target, ok := agentpack.NormalizeHarnessPrompt(unknown); ok || target != "" {
			t.Fatalf("NormalizeHarnessPrompt(%q) = %q, %t; want deterministic rejection", unknown, target, ok)
		}
	}
	for _, unknown := range []string{"", "proposal", "propose", "sdd-proposal", "sdd-unknown"} {
		if phase, ok := agentpack.PhaseForAgentName(unknown); ok || phase != "" {
			t.Fatalf("PhaseForAgentName(%q) = %q, %t; want deterministic rejection", unknown, phase, ok)
		}
	}
}

func renderCrossHarnessRoleContracts(t *testing.T) map[TargetID]map[string]string {
	t.Helper()
	request := func(target TargetID) RenderRequest {
		return RenderRequest{Target: target, Definition: agentpack.DefaultDefinition(), Components: []ComponentID{ComponentCorePack}}
	}
	contracts := make(map[TargetID]map[string]string, 4)

	piFiles, err := defaultPiAdapter().RenderManagedAgents(context.Background(), request(TargetPi))
	if err != nil {
		t.Fatalf("render Pi managed agents: %v", err)
	}
	contracts[TargetPi] = make(map[string]string, len(piFiles))
	for _, file := range piFiles {
		role := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(file.RelativePath), "lore-managed-"), ".md")
		contracts[TargetPi][role] = string(file.Content)
	}

	for _, tc := range []struct {
		target  TargetID
		adapter HarnessAdapter
	}{
		{TargetOpenCode, defaultOpenCodeAdapter()},
		{TargetCodex, defaultCodexAdapter()},
		{TargetAntigravity, defaultAntigravityAdapter()},
	} {
		files, err := tc.adapter.Render(context.Background(), request(tc.target))
		if err != nil {
			t.Fatalf("render %s contracts: %v", tc.target, err)
		}
		contracts[tc.target] = extractCrossHarnessRoleContracts(t, tc.target, files)
	}
	return contracts
}

func extractCrossHarnessRoleContracts(t *testing.T, target TargetID, files []RenderedFile) map[string]string {
	t.Helper()
	byPath := make(map[string]RenderedFile, len(files))
	for _, file := range files {
		byPath[filepath.ToSlash(file.RelativePath)] = file
	}
	roles := map[string]string{}
	switch target {
	case TargetOpenCode:
		roles[agentpack.RoleOrchestrator] = string(byPath["AGENTS.md"].Content)
		roles[agentpack.RoleLoreWorker] = string(byPath["prompts/lore-worker.md"].Content)
		for _, phase := range agentpack.OrderedPhaseIDs() {
			path := "prompts/sdd/" + agentpack.PhaseEnvelopeName(phase) + ".md"
			roles[agentpack.PhaseAgentName(phase)] = string(byPath[path].Content)
		}
	case TargetCodex:
		roles[agentpack.RoleOrchestrator] = string(byPath["AGENTS.md"].Content)
		for _, role := range append([]string{agentpack.RoleLoreWorker}, agentpack.SDDPhaseAgentNames()...) {
			roles[role] = string(byPath["skills/"+role+"/SKILL.md"].Content)
		}
	case TargetAntigravity:
		var profile antigravityAgentProfile
		if err := json.Unmarshal(byPath["../config/agents/lore.json"].Content, &profile); err != nil {
			t.Fatalf("decode Antigravity agent profile: %v", err)
		}
		roles[agentpack.RoleOrchestrator] = profile.SystemInstruction
		for _, role := range append([]string{agentpack.RoleLoreWorker}, agentpack.SDDPhaseAgentNames()...) {
			roles[role] = string(byPath["skills/"+role+"/SKILL.md"].Content)
		}
	default:
		t.Fatalf("unsupported cross-harness target %q", target)
	}
	return roles
}

func assertCanonicalOrchestratorContract(t *testing.T, target TargetID, content string) {
	if strings.Contains(content, "`lore_memory_save`") {
		t.Fatalf("target=%s orchestrator has an overbroad lore_memory_save requirement without artifact ownership", target)
	}
	t.Helper()
	for _, marker := range []string{"orchestrator", "SDD"} {
		if !strings.Contains(strings.ToLower(content), strings.ToLower(marker)) {
			t.Fatalf("target=%s orchestrator missing semantic marker %q", target, marker)
		}
	}
}

func assertCanonicalLoreMCPRetrievalContract(t *testing.T, target TargetID, role, content string) {
	t.Helper()
	for _, marker := range []string{
		"`lore_project_activity`", "`lore_project_context`", "`lore_memory_search`", "`lore_memory_get`",
		"activity", "do not pass query text", "exactly one project identity",
	} {
		if !strings.Contains(content, marker) {
			t.Fatalf("target=%s role=%s missing canonical Lore MCP retrieval marker %q", target, role, marker)
		}
	}
	if !strings.Contains(content, "OMIT full `content`") && !strings.Contains(content, "OMITS full `content`") {
		t.Fatalf("target=%s role=%s omits preview-versus-full-body retrieval semantics", target, role)
	}
	if !strings.Contains(content, "full body") && !strings.Contains(content, "full memory body") {
		t.Fatalf("target=%s role=%s omits full-body retrieval semantics", target, role)
	}
}

func assertCanonicalSkillContract(t *testing.T, target TargetID, role, content string) {
	t.Helper()
	for _, marker := range []string{"skill", "`skill_resolution`"} {
		if !strings.Contains(strings.ToLower(content), strings.ToLower(marker)) {
			t.Fatalf("target=%s role=%s missing skill-resolution marker %q", target, role, marker)
		}
	}
	if role == agentpack.RoleLoreWorker {
		for _, marker := range []string{"project-local", "Lore-wide", "legacy Claude"} {
			if !strings.Contains(content, marker) {
				t.Fatalf("target=%s generic worker missing skill-precedence marker %q", target, marker)
			}
		}
		return
	}
	for _, marker := range []string{"project-local before Lore-wide", "MUST load and follow", "phase skill"} {
		if !strings.Contains(content, marker) {
			t.Fatalf("target=%s SDD role=%s missing mandatory skill-resolution marker %q", target, role, marker)
		}
	}
}

func assertCanonicalArtifactOwnerPersistence(t *testing.T, target TargetID, role, content string) {
	t.Helper()
	for _, marker := range []string{"`lore_memory_save`", "configured artifact authority", "Persist durable artifacts"} {
		if !strings.Contains(content, marker) {
			t.Fatalf("target=%s SDD artifact owner role=%s missing persistence contract marker %q", target, role, marker)
		}
	}
	if strings.Contains(content, "Persist the full") {
		return
	}
	if phase, ok := agentpack.PhaseForAgentName(role); !ok || phase != agentpack.PhaseApply || !strings.Contains(content, "Persist `apply-started`") {
		t.Fatalf("target=%s SDD artifact owner role=%s lacks full-artifact persistence semantics", target, role)
	}
}

func assertGenericWorkerLeastPrivilege(t *testing.T, target TargetID, role, content string) {
	t.Helper()
	if role != agentpack.RoleLoreWorker {
		t.Fatalf("target=%s expected generic worker role, got %s", target, role)
	}
	if strings.Contains(content, "`lore_memory_save`") {
		t.Fatalf("target=%s generic worker role=%s has an overbroad lore_memory_save requirement without artifact ownership", target, role)
	}
	if !strings.Contains(content, "prefer repository evidence over assumptions") {
		t.Fatalf("target=%s generic worker role=%s lacks evidence-first guidance", target, role)
	}
}

func assertCanonicalEnvelopeContract(t *testing.T, target TargetID, role, content string) {
	t.Helper()
	for _, field := range agentpack.WorkerEnvelopeFields {
		if !strings.Contains(content, "`"+field+"`") {
			t.Fatalf("target=%s role=%s missing envelope field %q", target, role, field)
		}
	}
	for _, status := range agentpack.FinalStatusValues {
		if !strings.Contains(content, "`"+status+"`") {
			t.Fatalf("target=%s role=%s missing allowed final status %q", target, role, status)
		}
	}
	for _, stale := range []string{"`running`", "`next`", "`executive_summary`", "`next_recommended`"} {
		if !strings.Contains(content, stale) {
			t.Fatalf("target=%s role=%s missing stale-envelope field prohibition %q", target, role, stale)
		}
	}
	if !strings.Contains(content, "Do not use `running`") && !strings.Contains(content, "`running` is reserved") {
		t.Fatalf("target=%s role=%s does not prohibit running as a final envelope status", target, role)
	}
	if phase, isPhase := agentpack.PhaseForAgentName(role); isPhase {
		if !strings.Contains(content, "`phase`") || !strings.Contains(content, "set `phase` to `"+agentpack.PhaseEnvelopeName(phase)+"`") {
			t.Fatalf("target=%s role=%s missing canonical SDD phase envelope", target, role)
		}
	} else if strings.Contains(content, "exactly these keys: `status`, `phase`") {
		t.Fatalf("target=%s worker incorrectly declares SDD phase envelope", target)
	}
}

func assertCanonicalPhaseContract(t *testing.T, target TargetID, role string, phase agentpack.PhaseID, content string) {
	t.Helper()
	phaseName := agentpack.PhaseEnvelopeName(phase)
	if !strings.Contains(content, "set `phase` to `"+phaseName+"`") {
		t.Fatalf("target=%s role=%s missing canonical envelope phase %q", target, role, phaseName)
	}
	if !strings.Contains(strings.ToLower(content), "sdd "+strings.ToLower(phaseName)) {
		t.Fatalf("target=%s role=%s missing canonical SDD phase semantic marker %q", target, role, phaseName)
	}
	if phase == agentpack.PhaseApply {
		for _, marker := range []string{"apply-started", "apply-partial", "apply-progress", "apply-report"} {
			if !strings.Contains(content, marker) {
				t.Fatalf("target=%s role=%s missing apply recovery marker %q", target, role, marker)
			}
		}
	}
}

func sortedCrossHarnessRoles(roles map[string]string) []string {
	values := make([]string, 0, len(roles))
	for role := range roles {
		values = append(values, role)
	}
	sort.Strings(values)
	return values
}

func TestOpenCodeBundledPluginAssetsNoGentleWordingLeakage(t *testing.T) {
	assetFS := opencodeEmbeddedAssetFS()
	forbidden := []string{
		"gentle",
		"gentle-ai",
		"gentleprogramming",
		"gentleman-programming",
	}
	walkErr := fs.WalkDir(assetFS, "assets/opencode", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		content, readErr := assetFS.(interface {
			ReadFile(string) ([]byte, error)
		}).ReadFile(path)
		if readErr != nil {
			t.Fatalf("ReadFile(%q) error = %v", path, readErr)
		}
		lower := strings.ToLower(string(content))
		for _, token := range forbidden {
			if strings.Contains(lower, token) {
				t.Fatalf("bundled OpenCode asset %q leaked forbidden Gentle token %q; content=%q", path, token, string(content))
			}
		}
		return nil
	})
	if walkErr != nil {
		t.Fatalf("fs.WalkDir(assets/opencode) error = %v", walkErr)
	}
}

// W3.3-E traceability: R3/R6/R9/R12/R14; C1-C5, C20, C33-C34,
// C42, and C45. The guard is syntax-aware so comments and negative test
// fixtures cannot masquerade as production coupling.
func TestW33EStaticGuardKeepsTransactionOutOfServerCLIAndW4Contracts(t *testing.T) {
	root, _ := repoInternalRoot(t)
	w33Files := []string{
		"internal/install/transaction.go",
		"internal/install/transaction_fs.go",
		"internal/install/transaction_authority.go",
		"internal/install/hosted_mcp_finalizer.go",
		"internal/install/profile_store.go",
		"internal/install/profile_store_authority.go",
	}
	for _, suffix := range []string{"unix.go", "windows.go"} {
		w33Files = append(w33Files,
			"internal/install/transaction_fs_"+suffix,
			"internal/install/transaction_authority_"+suffix,
			"internal/install/profile_store_"+suffix,
		)
	}
	for _, rel := range w33Files {
		file := parseW33EGoFile(t, root, rel)
		for _, spec := range file.Imports {
			path, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				t.Fatalf("%s import: %v", rel, err)
			}
			for _, forbidden := range []string{"internal/cli", "internal/httpclient", "net/http", "net/rpc", "database/sql"} {
				if path == forbidden || strings.HasSuffix(path, "/"+forbidden) {
					t.Fatalf("%s imports prohibited server/CLI authority %q", rel, path)
				}
			}
		}
		ast.Inspect(file, func(node ast.Node) bool {
			switch value := node.(type) {
			case *ast.BasicLit:
				if value.Kind != token.STRING {
					return true
				}
				literal, err := strconv.Unquote(value.Value)
				if err != nil {
					return true
				}
				lower := strings.ToLower(literal)
				for _, forbidden := range []string{
					"lore_project_activity", "lore_project_context", "lore_memory_search", "lore_memory_get",
					"repository_id", "project_key", "/v1/memories", "/v1/projects", "query_text", "next_cursor",
					"lore-install.json", "claude-code", "codex", "antigravity",
				} {
					if strings.Contains(lower, forbidden) {
						t.Errorf("%s contains prohibited W3.3 contract literal %q", rel, literal)
					}
				}
			case *ast.CallExpr:
				if selector, ok := value.Fun.(*ast.SelectorExpr); ok {
					if base, ok := selector.X.(*ast.Ident); ok && (base.Name == "http" || base.Name == "exec" || base.Name == "sql") {
						t.Errorf("%s calls prohibited external authority %s.%s", rel, base.Name, selector.Sel.Name)
					}
				}
			}
			return true
		})
	}
}

// W3.3-E traceability: R12; C20, C31-C34, C42, C44.
func TestW33EStaticGuardHasOneTransactionAndNoProfileAuthorityReuse(t *testing.T) {
	root, internalRoot := repoInternalRoot(t)
	allowedProfile := map[string]bool{
		"internal/install/profile_store.go":           true,
		"internal/install/profile_store_authority.go": true,
		"internal/install/transaction.go":             true,
	}
	var transactionCallers []string
	err := filepath.WalkDir(internalRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		file := parseW33EGoFile(t, root, rel)
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			name := w33ECalledName(call.Fun)
			switch name {
			case "applyTransactionFS", "applyTransactionFSWithWait":
				transactionCallers = append(transactionCallers, rel+":"+name)
			case "acquireStoreAuthority", "withStoreAuthority", "beginHeldProfileCompletion":
				if !allowedProfile[rel] {
					t.Errorf("%s reuses profile-store authority through %s", rel, name)
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	wantCallers := []string{"internal/install/transaction_fs.go:applyTransactionFSWithWait"}
	if !reflect.DeepEqual(transactionCallers, wantCallers) {
		t.Fatalf("production transaction entrypoints = %v, want sole wrapper call %v", transactionCallers, wantCallers)
	}
}

// W3.3-E traceability: R12/R14; C4-C5, C31-C34, C42, C45.
func TestW33EStaticGuardFinalizerAndCompletionExposeNoExternalFactsOrDirectDeletion(t *testing.T) {
	root, _ := repoInternalRoot(t)
	for _, rel := range []string{"internal/install/hosted_mcp_finalizer.go", "internal/install/transaction.go"} {
		file := parseW33EGoFile(t, root, rel)
		ast.Inspect(file, func(node ast.Node) bool {
			switch value := node.(type) {
			case *ast.FuncDecl:
				if value.Name.Name == "finalizeHostedMCP" {
					got := w33EFieldTypes(value.Type.Params)
					want := []string{"TransactionPlan", "TransactionInput", "*transactionFSJournal", "hostedMCPCredentialResolver", "hostedMCPRenderer"}
					if !reflect.DeepEqual(got, want) {
						t.Errorf("finalizer inputs = %v, want %v", got, want)
					}
				}
				if value.Name.Name == "completeHostedMCPCompletion" {
					got := w33EFieldTypes(value.Type.Params)
					want := []string{"*hostedMCPCompletionHandoff", "TransactionPlan", "TransactionInput", "ProfileStore", "PreparedProject"}
					if !reflect.DeepEqual(got, want) {
						t.Errorf("completion inputs = %v, want %v", got, want)
					}
				}
			case *ast.CallExpr:
				if selector, ok := value.Fun.(*ast.SelectorExpr); ok {
					if base, ok := selector.X.(*ast.Ident); ok && (base.Name == "os" || base.Name == "unix" || base.Name == "windows") {
						switch selector.Sel.Name {
						case "Remove", "RemoveAll", "Unlink", "Unlinkat", "DeleteFile", "MoveFileEx":
							t.Errorf("%s directly deletes/moves platform journal or paths through %s.%s", rel, base.Name, selector.Sel.Name)
						}
					}
				}
			}
			return true
		})
	}
}

// W3.3-E traceability: R3/R6/R9/R12; C1-C10 and C45.
// Existing legacy remember/recall HTTP commands are outside this change; this
// guard prevents the accepted W3.3 transaction from becoming their caller or
// teaching W4 activity/search/get/cursor semantics.
func TestW33EStaticGuardCLIHasNoCanonicalTransactionOrW4Wire(t *testing.T) {
	root, _ := repoInternalRoot(t)
	for _, rel := range []string{"internal/cli/app.go", "internal/cli/actions.go"} {
		file := parseW33EGoFile(t, root, rel)
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			name := w33ECalledName(call.Fun)
			for _, forbidden := range []string{
				"SealTransactionPlan", "applyTransactionFS", "finalizeHostedMCP", "completeHostedMCPCompletion",
				"LoreProjectActivity", "LoreProjectContext", "LoreMemorySearch", "LoreMemoryGet",
			} {
				if name == forbidden {
					t.Errorf("%s wires excluded W3.3/W4 behavior through %s", rel, name)
				}
			}
			return true
		})
	}
}

func parseW33EGoFile(t *testing.T, root, rel string) *ast.File {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse %s: %v", rel, err)
	}
	return file
}

func w33ECalledName(expr ast.Expr) string {
	switch value := expr.(type) {
	case *ast.Ident:
		return value.Name
	case *ast.SelectorExpr:
		return value.Sel.Name
	default:
		return ""
	}
}

func w33EFieldTypes(fields *ast.FieldList) []string {
	if fields == nil {
		return nil
	}
	var result []string
	for _, field := range fields.List {
		name := ""
		switch typ := field.Type.(type) {
		case *ast.Ident:
			name = typ.Name
		case *ast.StarExpr:
			if id, ok := typ.X.(*ast.Ident); ok {
				name = "*" + id.Name
			}
		}
		count := len(field.Names)
		if count == 0 {
			count = 1
		}
		for i := 0; i < count; i++ {
			result = append(result, name)
		}
	}
	return result
}
