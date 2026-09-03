package install

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/alferio94/lore-cli/internal/agentpack"
)

const agentContextBaselinePath = "testdata/agent_context_baselines.json"
const updateAgentContextBaselinesEnv = "LORE_UPDATE_AGENT_CONTEXT_BASELINES"
const updateAgentContextBaselinesReasonEnv = "LORE_UPDATE_AGENT_CONTEXT_BASELINE_REASON"

type contextMetrics struct {
	Bytes           int `json:"bytes"`
	Chars           int `json:"chars"`
	Lines           int `json:"lines"`
	EstimatedTokens int `json:"estimated_tokens"`
}

type contextNormalizedBlock struct {
	SHA256 string `json:"sha256"`
	Bytes  int    `json:"bytes"`
	Chars  int    `json:"chars"`
}

type contextMeasurement struct {
	Target           string                   `json:"target"`
	Role             string                   `json:"role"`
	Path             string                   `json:"path"`
	Measured         contextMetrics           `json:"measured"`
	Budget           contextMetrics           `json:"budget"`
	NormalizedBlocks []contextNormalizedBlock `json:"normalized_blocks,omitempty"`
}

type contextDuplicateMetrics struct {
	RepeatedBlocks       int `json:"repeated_blocks"`
	DuplicateOccurrences int `json:"duplicate_occurrences"`
	DuplicateBytes       int `json:"duplicate_bytes"`
	DuplicateChars       int `json:"duplicate_chars"`
	DuplicateTokens      int `json:"duplicate_estimated_tokens"`
}

type contextBundle struct {
	Target      string                  `json:"target"`
	Role        string                  `json:"role"`
	Name        string                  `json:"name"`
	Paths       []string                `json:"paths"`
	Measured    contextMetrics          `json:"measured"`
	Budget      contextMetrics          `json:"budget"`
	Duplication contextDuplicateMetrics `json:"within_bundle_duplication"`
}

type contextSourceReuse struct {
	SHA256      string   `json:"sha256"`
	Bytes       int      `json:"bytes"`
	Chars       int      `json:"chars"`
	Targets     []string `json:"targets"`
	Occurrences int      `json:"occurrences"`
}

type contextInventory struct {
	Version      int                  `json:"version"`
	BudgetReview string               `json:"budget_review"`
	Units        []contextMeasurement `json:"units"`
	Bundles      []contextBundle      `json:"bundles"`
	SourceReuse  []contextSourceReuse `json:"source_reuse"`
}

func TestAgentContextBaselines(t *testing.T) {
	first := renderContextInventory(t)
	second := renderContextInventory(t)
	if got, want := marshalContextInventory(t, second), marshalContextInventory(t, first); string(got) != string(want) {
		t.Fatal("rendered context inventory is not deterministic")
	}

	wantJSON, baseline := loadContextBaseline(t)
	if os.Getenv(updateAgentContextBaselinesEnv) == "1" {
		reason := strings.TrimSpace(os.Getenv(updateAgentContextBaselinesReasonEnv))
		if reason == "" {
			t.Fatalf("refusing to regenerate agent context baseline without %s; record the reviewed reason with the fixture diff", updateAgentContextBaselinesReasonEnv)
		}
		if err := validateP0MetricsUnchanged(first, baseline); err != nil {
			t.Fatal(err)
		}
		first.BudgetReview = reason
		if err := os.WriteFile(agentContextBaselinePath, marshalContextInventory(t, first), 0o644); err != nil {
			t.Fatalf("write baseline fixture: %v", err)
		}
		return
	}

	if err := validateContextBudgets(first, baseline); err != nil {
		t.Fatal(err)
	}
	first.BudgetReview = baseline.BudgetReview
	firstJSON := marshalContextInventory(t, first)
	if string(wantJSON) != string(firstJSON) {
		t.Fatalf("agent context baseline drifted; regenerate deliberately with %s=1 and %s=<reviewed reason>, then review the measured and budget changes\nwant:\n%s\ngot:\n%s", updateAgentContextBaselinesEnv, updateAgentContextBaselinesReasonEnv, wantJSON, firstJSON)
	}
}

// TestAgentContextBudgetReviewGuard keeps every ceiling tied to a captured
// measurement. The fixture is the reviewable approval point: normal test runs never
// write it, and regeneration requires both an explicit flag and a stated reason.
func TestAgentContextBundleTokenEstimatesUseAggregateUnicodeChars(t *testing.T) {
	for _, bundle := range renderContextInventory(t).Bundles {
		want := (bundle.Measured.Chars + 3) / 4
		if bundle.Measured.EstimatedTokens != want {
			t.Fatalf("bundle target=%s role=%s name=%s estimated_tokens=%d, want ceil(characters/4)=%d", bundle.Target, bundle.Role, bundle.Name, bundle.Measured.EstimatedTokens, want)
		}
	}
}

func TestAgentContextBudgetReviewGuard(t *testing.T) {
	_, baseline := loadContextBaseline(t)
	if err := validateBaselineBudgetDerivation(baseline); err != nil {
		t.Fatal(err)
	}

	regression := baseline
	regression.Units = append([]contextMeasurement(nil), baseline.Units...)
	regression.Units[0].Measured.Bytes++
	if err := validateContextBudgets(regression, baseline); err == nil {
		t.Fatal("budget regression unexpectedly passed")
	} else {
		for _, want := range []string{
			"target=" + regression.Units[0].Target,
			"role=" + regression.Units[0].Role,
			"path=" + regression.Units[0].Path,
			"metric=bytes",
			"measured=", "budget=",
		} {
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("budget regression error %q missing %q", err, want)
			}
		}
	}
}

func loadContextBaseline(t *testing.T) ([]byte, contextInventory) {
	t.Helper()
	data, err := os.ReadFile(agentContextBaselinePath)
	if err != nil {
		t.Fatalf("read baseline fixture: %v", err)
	}
	var inventory contextInventory
	if err := json.Unmarshal(data, &inventory); err != nil {
		t.Fatalf("parse baseline fixture: %v", err)
	}
	return data, inventory
}

func validateBaselineBudgetDerivation(baseline contextInventory) error {
	if strings.TrimSpace(baseline.BudgetReview) == "" {
		return fmt.Errorf("agent context budget review guard rejected fixture without budget_review; regenerate with %s=1 and %s=<reviewed reason>", updateAgentContextBaselinesEnv, updateAgentContextBaselinesReasonEnv)
	}
	for _, unit := range baseline.Units {
		if err := validateBudgetMatchesMeasured("unit", unit.Target, unit.Role, unit.Path, unit.Budget, unit.Measured); err != nil {
			return err
		}
	}
	for _, bundle := range baseline.Bundles {
		if err := validateBudgetMatchesMeasured("bundle", bundle.Target, bundle.Role, bundle.Name, bundle.Budget, bundle.Measured); err != nil {
			return err
		}
	}
	return nil
}

func validateContextBudgets(actual, baseline contextInventory) error {
	if err := validateBaselineBudgetDerivation(baseline); err != nil {
		return err
	}
	unitBudgets := make(map[string]contextMeasurement, len(baseline.Units))
	for _, unit := range baseline.Units {
		unitBudgets[contextUnitKey(unit)] = unit
	}
	for _, unit := range actual.Units {
		budget, ok := unitBudgets[contextUnitKey(unit)]
		if !ok {
			return fmt.Errorf("agent context budget missing for unit target=%s role=%s path=%s", unit.Target, unit.Role, unit.Path)
		}
		if err := validateMeasuredWithinBudget("unit", unit.Target, unit.Role, unit.Path, unit.Measured, budget.Budget); err != nil {
			return err
		}
	}
	bundleBudgets := make(map[string]contextBundle, len(baseline.Bundles))
	for _, bundle := range baseline.Bundles {
		bundleBudgets[contextBundleKey(bundle)] = bundle
	}
	for _, bundle := range actual.Bundles {
		budget, ok := bundleBudgets[contextBundleKey(bundle)]
		if !ok {
			return fmt.Errorf("agent context budget missing for bundle target=%s role=%s path=%s", bundle.Target, bundle.Role, bundle.Name)
		}
		if err := validateMeasuredWithinBudget("bundle", bundle.Target, bundle.Role, bundle.Name, bundle.Measured, budget.Budget); err != nil {
			return err
		}
	}
	return nil
}

func validateBudgetMatchesMeasured(kind, target, role, path string, budget, measured contextMetrics) error {
	for _, metric := range []struct {
		name   string
		budget int
		actual int
	}{
		{"bytes", budget.Bytes, measured.Bytes},
		{"chars", budget.Chars, measured.Chars},
		{"lines", budget.Lines, measured.Lines},
		{"estimated_tokens", budget.EstimatedTokens, measured.EstimatedTokens},
	} {
		if metric.budget != metric.actual {
			return fmt.Errorf("agent context budget review guard rejected %s target=%s role=%s path=%s metric=%s budget=%d measured=%d: budgets must match captured measurements; percentage budgets and unreviewed headroom are not allowed", kind, target, role, path, metric.name, metric.budget, metric.actual)
		}
	}
	return nil
}

func validateMeasuredWithinBudget(kind, target, role, path string, measured, budget contextMetrics) error {
	for _, metric := range []struct {
		name   string
		actual int
		budget int
	}{
		{"bytes", measured.Bytes, budget.Bytes},
		{"chars", measured.Chars, budget.Chars},
		{"lines", measured.Lines, budget.Lines},
		{"estimated_tokens", measured.EstimatedTokens, budget.EstimatedTokens},
	} {
		if metric.actual > metric.budget {
			return fmt.Errorf("agent context budget exceeded for %s target=%s role=%s path=%s metric=%s measured=%d budget=%d", kind, target, role, path, metric.name, metric.actual, metric.budget)
		}
	}
	return nil
}

func renderContextInventory(t *testing.T) contextInventory {
	t.Helper()
	request := func(target TargetID) RenderRequest {
		return RenderRequest{
			Target:     target,
			Definition: agentpack.DefaultDefinition(),
			Components: []ComponentID{ComponentCorePack},
		}
	}

	files := make(map[string][]byte)
	units := make([]contextMeasurement, 0, 80)
	add := func(target, role string, file RenderedFile) {
		path := filepath.ToSlash(file.RelativePath)
		key := target + "\x00" + path
		if _, exists := files[key]; exists {
			t.Fatalf("duplicate context inventory file %s/%s", target, path)
		}
		files[key] = file.Content
		metrics := measureContext(file.Content)
		units = append(units, contextMeasurement{Target: target, Role: role, Path: path, Measured: metrics, Budget: metrics, NormalizedBlocks: normalizedContextBlocks(file.Content)})
	}

	piFiles, err := defaultPiAdapter().RenderManagedAgents(context.Background(), request(TargetPi))
	if err != nil {
		t.Fatalf("render Pi managed agents: %v", err)
	}
	for _, file := range piFiles {
		add(string(TargetPi), strings.TrimSuffix(strings.TrimPrefix(filepath.Base(file.RelativePath), "lore-managed-"), ".md"), file)
	}

	openCodeFiles, err := defaultOpenCodeAdapter().Render(context.Background(), request(TargetOpenCode))
	if err != nil {
		t.Fatalf("render OpenCode context: %v", err)
	}
	for _, file := range openCodeFiles {
		if role, ok := openCodeContextRole(file.RelativePath); ok {
			add(string(TargetOpenCode), role, file)
		}
	}

	codexFiles, err := defaultCodexAdapter().Render(context.Background(), request(TargetCodex))
	if err != nil {
		t.Fatalf("render Codex context: %v", err)
	}
	for _, file := range codexFiles {
		if role, ok := codexContextRole(file.RelativePath); ok {
			add(string(TargetCodex), role, file)
		}
	}

	antigravityFiles, err := defaultAntigravityAdapter().Render(context.Background(), request(TargetAntigravity))
	if err != nil {
		t.Fatalf("render Antigravity context: %v", err)
	}
	for _, file := range antigravityFiles {
		if role, ok := antigravityContextRole(file.RelativePath); ok {
			add(string(TargetAntigravity), role, file)
		}
	}

	sort.Slice(units, func(i, j int) bool {
		return contextUnitKey(units[i]) < contextUnitKey(units[j])
	})
	bundles := renderContextBundles(t, units, files)
	return contextInventory{Version: 2, Units: units, Bundles: bundles, SourceReuse: contextSourceReuseRecords(units)}
}

func openCodeContextRole(path string) (string, bool) {
	path = filepath.ToSlash(path)
	switch path {
	case "AGENTS.md", "prompts/lore.md":
		return agentpack.RoleOrchestrator, true
	case "prompts/lore-worker.md", "skills/lore-worker/SKILL.md":
		return agentpack.RoleLoreWorker, true
	}
	if role, ok := skillRoleFromPath(path); ok {
		return role, true
	}
	return phaseRoleFromPath(path, "prompts/sdd/", ".md")
}

func codexContextRole(path string) (string, bool) {
	path = filepath.ToSlash(path)
	if path == "AGENTS.md" {
		return agentpack.RoleOrchestrator, true
	}
	if path == "skills/_shared/sdd-phase-common.md" {
		return "shared", true
	}
	return skillRoleFromPath(path)
}

func antigravityContextRole(path string) (string, bool) {
	path = filepath.ToSlash(path)
	switch path {
	case "../GEMINI.md", "../config/agents/lore.json":
		return agentpack.RoleOrchestrator, true
	case "skills/_shared/sdd-phase-common.md":
		return "shared", true
	}
	return skillRoleFromPath(path)
}

func skillRoleFromPath(path string) (string, bool) {
	parts := strings.Split(filepath.ToSlash(path), "/")
	if len(parts) == 3 && parts[0] == "skills" && parts[2] == "SKILL.md" {
		return parts[1], true
	}
	return "", false
}

func phaseRoleFromPath(path, prefix, suffix string) (string, bool) {
	if !strings.HasPrefix(path, prefix) || !strings.HasSuffix(path, suffix) {
		return "", false
	}
	phase := strings.TrimSuffix(strings.TrimPrefix(path, prefix), suffix)
	for _, id := range agentpack.OrderedPhaseIDs() {
		if phase == string(id) || (id == agentpack.PhaseProposal && phase == "propose") {
			return agentpack.PhaseAgentName(id), true
		}
	}
	return "", false
}

func renderContextBundles(t *testing.T, units []contextMeasurement, files map[string][]byte) []contextBundle {
	t.Helper()
	byTargetRole := make(map[string][]contextMeasurement)
	for _, unit := range units {
		byTargetRole[unit.Target+"\x00"+unit.Role] = append(byTargetRole[unit.Target+"\x00"+unit.Role], unit)
	}
	bundle := func(target, role, name string, paths []string) contextBundle {
		sort.Strings(paths)
		metrics := contextMetrics{}
		for _, path := range paths {
			value, ok := files[target+"\x00"+path]
			if !ok {
				t.Fatalf("bundle %s/%s references missing path %s", target, role, path)
			}
			metrics = addContextMetrics(metrics, measureContext(value))
		}
		metrics.EstimatedTokens = (metrics.Chars + 3) / 4
		return contextBundle{Target: target, Role: role, Name: name, Paths: paths, Measured: metrics, Budget: metrics, Duplication: measureWithinBundleDuplication(target, paths, files)}
	}

	bundles := make([]contextBundle, 0, len(byTargetRole))
	for key, roleUnits := range byTargetRole {
		parts := strings.Split(key, "\x00")
		target, role := parts[0], parts[1]
		paths := make([]string, 0, len(roleUnits))
		for _, unit := range roleUnits {
			paths = append(paths, unit.Path)
		}
		switch target {
		case string(TargetPi):
			bundles = append(bundles, bundle(target, role, "overlay", paths))
		case string(TargetOpenCode):
			for _, path := range paths {
				if strings.HasPrefix(path, "prompts/") {
					bundles = append(bundles, bundle(target, role, "referenced-prompt", []string{path}))
				}
			}
		case string(TargetCodex):
			if role == "shared" {
				continue
			}
			bundlePaths := []string{"AGENTS.md"}
			if role != agentpack.RoleOrchestrator {
				bundlePaths = append(bundlePaths, "skills/"+role+"/SKILL.md")
			}
			if strings.HasPrefix(role, "sdd-") {
				bundlePaths = append(bundlePaths, "skills/_shared/sdd-phase-common.md")
			}
			bundles = append(bundles, bundle(target, role, "effective-context", bundlePaths))
		case string(TargetAntigravity):
			if role == "shared" {
				continue
			}
			bundlePaths := []string{"../GEMINI.md", "../config/agents/lore.json"}
			if role != agentpack.RoleOrchestrator {
				bundlePaths = append(bundlePaths, "skills/"+role+"/SKILL.md")
			}
			if strings.HasPrefix(role, "sdd-") {
				bundlePaths = append(bundlePaths, "skills/_shared/sdd-phase-common.md")
			}
			bundles = append(bundles, bundle(target, role, "effective-context", bundlePaths))
		}
	}
	sort.Slice(bundles, func(i, j int) bool {
		return contextBundleKey(bundles[i]) < contextBundleKey(bundles[j])
	})
	return bundles
}

func TestContextOverlapAccounting(t *testing.T) {
	const repeated = "This normalized block is deliberately long enough to qualify for overlap accounting and appears twice."
	files := map[string][]byte{
		"pi\x00a.md":       []byte(repeated),
		"opencode\x00b.md": []byte(repeated),
		"codex\x00c.md":    []byte(repeated + "\n\n" + repeated),
	}

	if got := measureWithinBundleDuplication("pi", []string{"a.md"}, files); got != (contextDuplicateMetrics{}) {
		t.Fatalf("cross-target source reuse counted as within-bundle duplication: %+v", got)
	}
	sourceReuse := contextSourceReuseRecords([]contextMeasurement{
		{Target: "pi", NormalizedBlocks: normalizedContextBlocks(files["pi\x00a.md"])},
		{Target: "opencode", NormalizedBlocks: normalizedContextBlocks(files["opencode\x00b.md"])},
	})
	if len(sourceReuse) != 1 || strings.Join(sourceReuse[0].Targets, ",") != "opencode,pi" || sourceReuse[0].Occurrences != 2 {
		t.Fatalf("cross-target reuse record = %+v, want one source-reuse record excluded from bundle duplication", sourceReuse)
	}
	got := measureWithinBundleDuplication("codex", []string{"c.md"}, files)
	wantChars := utf8.RuneCountInString(repeated)
	if got.RepeatedBlocks != 1 || got.DuplicateOccurrences != 1 || got.DuplicateBytes != len(repeated) || got.DuplicateChars != wantChars || got.DuplicateTokens != (wantChars+3)/4 {
		t.Fatalf("within-bundle duplication = %+v, want one charged repeated block", got)
	}

	first := normalizedContextBlocks([]byte("\r\n\u00a0 " + repeated + "\t\n"))
	second := normalizedContextBlocks([]byte(repeated))
	if len(first) != 1 || len(second) != 1 || first[0] != second[0] {
		t.Fatalf("normalized overlap blocks differ for equivalent Unicode-safe whitespace: first=%+v second=%+v", first, second)
	}
}

func normalizedContextBlocks(content []byte) []contextNormalizedBlock {
	candidates := normalizedContextCandidates(string(content))
	blocks := make([]contextNormalizedBlock, 0, len(candidates))
	for _, candidate := range candidates {
		chars := utf8.RuneCountInString(candidate)
		if chars < 80 {
			continue
		}
		digest := sha256.Sum256([]byte(candidate))
		blocks = append(blocks, contextNormalizedBlock{SHA256: fmt.Sprintf("%x", digest), Bytes: len(candidate), Chars: chars})
	}
	sort.Slice(blocks, func(i, j int) bool {
		if blocks[i].SHA256 != blocks[j].SHA256 {
			return blocks[i].SHA256 < blocks[j].SHA256
		}
		return blocks[i].Bytes < blocks[j].Bytes
	})
	return blocks
}

func normalizedContextCandidates(content string) []string {
	content = strings.ReplaceAll(strings.ReplaceAll(content, "\r\n", "\n"), "\r", "\n")
	lines := strings.Split(content, "\n")
	candidates := make([]string, 0)
	for index := 0; index < len(lines); {
		if isCodeFence(lines[index]) {
			end := index + 1
			for end < len(lines) && !isCodeFence(lines[end]) {
				end++
			}
			if end < len(lines) {
				end++
			}
			candidates = append(candidates, normalizeContextBlock(strings.Join(lines[index:end], "\n")))
			index = end
			continue
		}
		if strings.TrimSpace(lines[index]) == "" {
			index++
			continue
		}
		end := index + 1
		for end < len(lines) && strings.TrimSpace(lines[end]) != "" && !isCodeFence(lines[end]) {
			end++
		}
		candidates = append(candidates, normalizeContextBlock(strings.Join(lines[index:end], "\n")))
		index = end
	}
	return candidates
}

func isCodeFence(line string) bool {
	trimmed := strings.TrimSpace(line)
	return strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~")
}

func normalizeContextBlock(block string) string {
	var normalized strings.Builder
	for _, r := range strings.TrimSpace(block) {
		if r == '\n' {
			normalized.WriteRune(r)
			continue
		}
		if unicode.IsSpace(r) {
			if normalized.Len() == 0 || strings.HasSuffix(normalized.String(), " ") || strings.HasSuffix(normalized.String(), "\n") {
				continue
			}
			normalized.WriteByte(' ')
			continue
		}
		normalized.WriteRune(r)
	}
	return strings.TrimSpace(normalized.String())
}

func measureWithinBundleDuplication(target string, paths []string, files map[string][]byte) contextDuplicateMetrics {
	type occurrence struct {
		block contextNormalizedBlock
		path  string
	}
	occurrences := make(map[string][]occurrence)
	for _, path := range paths {
		for _, block := range normalizedContextBlocks(files[target+"\x00"+path]) {
			occurrences[block.SHA256] = append(occurrences[block.SHA256], occurrence{block: block, path: path})
		}
	}
	keys := make([]string, 0, len(occurrences))
	for key := range occurrences {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		left, right := occurrences[keys[i]][0].block, occurrences[keys[j]][0].block
		if left.Chars != right.Chars {
			return left.Chars > right.Chars
		}
		return keys[i] < keys[j]
	})
	metrics := contextDuplicateMetrics{}
	for _, key := range keys {
		matches := occurrences[key]
		if len(matches) < 2 {
			continue
		}
		metrics.RepeatedBlocks++
		metrics.DuplicateOccurrences += len(matches) - 1
		metrics.DuplicateBytes += (len(matches) - 1) * matches[0].block.Bytes
		metrics.DuplicateChars += (len(matches) - 1) * matches[0].block.Chars
		metrics.DuplicateTokens += (len(matches) - 1) * ((matches[0].block.Chars + 3) / 4)
	}
	return metrics
}

func contextSourceReuseRecords(units []contextMeasurement) []contextSourceReuse {
	type sourceOccurrence struct{ target string }
	occurrences := make(map[string][]sourceOccurrence)
	blocks := make(map[string]contextNormalizedBlock)
	for _, unit := range units {
		for _, block := range unit.NormalizedBlocks {
			occurrences[block.SHA256] = append(occurrences[block.SHA256], sourceOccurrence{target: unit.Target})
			blocks[block.SHA256] = block
		}
	}
	records := make([]contextSourceReuse, 0)
	for hash, matches := range occurrences {
		targetSet := make(map[string]struct{})
		for _, match := range matches {
			targetSet[match.target] = struct{}{}
		}
		if len(targetSet) < 2 {
			continue
		}
		targets := make([]string, 0, len(targetSet))
		for target := range targetSet {
			targets = append(targets, target)
		}
		sort.Strings(targets)
		block := blocks[hash]
		records = append(records, contextSourceReuse{SHA256: hash, Bytes: block.Bytes, Chars: block.Chars, Targets: targets, Occurrences: len(matches)})
	}
	sort.Slice(records, func(i, j int) bool { return records[i].SHA256 < records[j].SHA256 })
	return records
}

func validateP0MetricsUnchanged(actual, baseline contextInventory) error {
	if len(actual.Units) != len(baseline.Units) || len(actual.Bundles) != len(baseline.Bundles) {
		return fmt.Errorf("refusing baseline update: P0 inventory drifted (units %d/%d, bundles %d/%d)", len(actual.Units), len(baseline.Units), len(actual.Bundles), len(baseline.Bundles))
	}
	for index := range actual.Units {
		if contextUnitKey(actual.Units[index]) != contextUnitKey(baseline.Units[index]) || actual.Units[index].Measured != baseline.Units[index].Measured || actual.Units[index].Budget != baseline.Units[index].Budget {
			return fmt.Errorf("refusing baseline update: P0 unit inventory or metric drift at index %d", index)
		}
	}
	actualTokens, baselineTokens := 0, 0
	for index := range actual.Bundles {
		if contextBundleKey(actual.Bundles[index]) != contextBundleKey(baseline.Bundles[index]) || actual.Bundles[index].Measured != baseline.Bundles[index].Measured || actual.Bundles[index].Budget != baseline.Bundles[index].Budget {
			return fmt.Errorf("refusing baseline update: P0 bundle inventory or metric drift at index %d", index)
		}
		actualTokens += actual.Bundles[index].Measured.EstimatedTokens
		baselineTokens += baseline.Bundles[index].Measured.EstimatedTokens
	}
	if actualTokens != 74203 || baselineTokens != 74203 {
		return fmt.Errorf("refusing baseline update: aggregate bundle token drift actual=%d baseline=%d want=74203", actualTokens, baselineTokens)
	}
	return nil
}

func measureContext(content []byte) contextMetrics {
	chars := utf8.RuneCount(content)
	lines := 0
	if len(content) > 0 {
		lines = strings.Count(string(content), "\n")
		if !strings.HasSuffix(string(content), "\n") {
			lines++
		}
	}
	return contextMetrics{
		Bytes:           len(content),
		Chars:           chars,
		Lines:           lines,
		EstimatedTokens: (chars + 3) / 4,
	}
}

func addContextMetrics(total, value contextMetrics) contextMetrics {
	return contextMetrics{
		Bytes:           total.Bytes + value.Bytes,
		Chars:           total.Chars + value.Chars,
		Lines:           total.Lines + value.Lines,
		EstimatedTokens: 0,
	}
}

func marshalContextInventory(t *testing.T, inventory contextInventory) []byte {
	t.Helper()
	data, err := json.MarshalIndent(inventory, "", "  ")
	if err != nil {
		t.Fatalf("marshal context inventory: %v", err)
	}
	return append(data, '\n')
}

func contextUnitKey(unit contextMeasurement) string {
	return unit.Target + "\x00" + unit.Role + "\x00" + unit.Path
}

func contextBundleKey(bundle contextBundle) string {
	return bundle.Target + "\x00" + bundle.Role + "\x00" + bundle.Name
}
