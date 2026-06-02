package install

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alferio94/lore-cli/internal/agentconfig"
	"github.com/alferio94/lore-cli/internal/agentpack"
)

func TestResolveOpenCodeLayout(t *testing.T) {
	homeDir := "/home/user"
	layout := ResolveOpenCodeLayout(homeDir)

	if layout.Target != TargetOpenCode {
		t.Fatalf("layout.Target = %q, want %q", layout.Target, TargetOpenCode)
	}
	if got, want := layout.RootDir, filepath.Join(homeDir, ".config", "opencode"); got != want {
		t.Fatalf("layout.RootDir = %q, want %q", got, want)
	}
	if got, want := layout.ManifestPath, filepath.Join(homeDir, ".config", "opencode", "lore-install.json"); got != want {
		t.Fatalf("layout.ManifestPath = %q, want %q", got, want)
	}
	if got, want := layout.Paths[opencodeAgentsPathKey], filepath.Join(homeDir, ".config", "opencode", "AGENTS.md"); got != want {
		t.Fatalf("layout.Paths[%q] = %q, want %q", opencodeAgentsPathKey, got, want)
	}
	if got, want := layout.Paths[opencodeJSONPathKey], filepath.Join(homeDir, ".config", "opencode", "opencode.json"); got != want {
		t.Fatalf("layout.Paths[%q] = %q, want %q", opencodeJSONPathKey, got, want)
	}
	if got, want := layout.Paths[opencodeSkillsDirPathKey], filepath.Join(homeDir, ".config", "opencode", "skills"); got != want {
		t.Fatalf("layout.Paths[%q] = %q, want %q", opencodeSkillsDirPathKey, got, want)
	}
	if got, want := layout.Paths[opencodeCommandsDirPathKey], filepath.Join(homeDir, ".config", "opencode", "commands"); got != want {
		t.Fatalf("layout.Paths[%q] = %q, want %q", opencodeCommandsDirPathKey, got, want)
	}
}

func TestOpenCodeRenderProducesAgentsAndManagedSkills(t *testing.T) {
	adapter := defaultOpenCodeAdapter()
	files, err := adapter.Render(context.Background(), RenderRequest{
		Target:     TargetOpenCode,
		Assets:     agentpack.DefaultOperationalAssets(),
		Components: []ComponentID{ComponentCorePack, ComponentExtendedSkills},
	})
	if err != nil {
		t.Fatalf("Render() error = %v, want nil", err)
	}

	byPath := map[string]RenderedFile{}
	for _, file := range files {
		byPath[file.RelativePath] = file
	}

	agentsFile, ok := byPath[opencodeAgentsFileName]
	if !ok {
		t.Fatalf("rendered paths = %v, want %q", sortedRenderedPaths(files), opencodeAgentsFileName)
	}
	content := string(agentsFile.Content)
	for _, want := range []string{
		"This file is managed by `lore install --target opencode`",
		"~/.config/opencode/skills",
		"~/.config/opencode/opencode.json",
		"~/.config/opencode/commands",
		"sdd-apply: `gpt-5.4`",
		"## Orchestrator instruction",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("AGENTS.md = %q, want substring %q", content, want)
		}
	}
	for _, forbidden := range []string{"Bearer ", "mcpServers", "codex exec"} {
		if strings.Contains(content, forbidden) {
			t.Fatalf("AGENTS.md = %q, want %q omitted", content, forbidden)
		}
	}

	applySkill, ok := byPath[filepath.ToSlash(filepath.Join("skills", "sdd-apply", "SKILL.md"))]
	if !ok {
		t.Fatalf("rendered paths = %v, want OpenCode sdd-apply skill", sortedRenderedPaths(files))
	}
	if !containsAll(string(applySkill.Content), "~/.config/opencode/skills/sdd-apply/SKILL.md", "~/.config/opencode/skills/_shared/sdd-phase-common/SKILL.md") {
		t.Fatalf("sdd-apply skill = %q, want OpenCode skill paths", string(applySkill.Content))
	}

	if _, ok := byPath[filepath.ToSlash(filepath.Join("skills", "skill-creator", "SKILL.md"))]; !ok {
		t.Fatalf("rendered paths = %v, want extended skill output", sortedRenderedPaths(files))
	}
}

func TestOpenCodeAgentConfigUsesCustomModels(t *testing.T) {
	adapter := defaultOpenCodeAdapter()
	cfg := agentconfig.DefaultConfig()
	cfg.SDDAgents["sdd-verify"] = agentconfig.Agent{Model: "gpt-4o-mini"}

	files, err := adapter.Render(context.Background(), RenderRequest{
		Target:      TargetOpenCode,
		Assets:      agentpack.DefaultOperationalAssets(),
		Components:  []ComponentID{ComponentCorePack},
		AgentConfig: cfg,
	})
	if err != nil {
		t.Fatalf("Render() error = %v, want nil", err)
	}
	content := string(renderedFileByPath(t, files, opencodeAgentsFileName).Content)
	if !strings.Contains(content, "sdd-verify: `gpt-4o-mini`") {
		t.Fatalf("AGENTS.md = %q, want custom sdd-verify model", content)
	}

	block, err := renderOpenCodeLoreBlock(cfg, false)
	if err != nil {
		t.Fatalf("renderOpenCodeLoreBlock() error = %v, want nil", err)
	}
	jsonText := string(block)
	if !containsAll(jsonText, `"managed_by": "lore-cli"`, `"sdd-verify": {`, `"model": "gpt-4o-mini"`, `"skills_dir": "~/.config/opencode/skills"`) {
		t.Fatalf("lore block = %q, want custom model and skills dir", jsonText)
	}
	if strings.Contains(jsonText, `"commands_dir"`) {
		t.Fatalf("lore block = %q, want commands omitted when not explicitly approved", jsonText)
	}
}

func TestOpenCodeCommandsOmittedWithoutApprovedBoundary(t *testing.T) {
	files, err := renderOpenCodeCommandFiles(RenderRequest{Target: TargetOpenCode}, false)
	if err != nil {
		t.Fatalf("renderOpenCodeCommandFiles(false) error = %v, want nil", err)
	}
	if len(files) != 0 {
		t.Fatalf("renderOpenCodeCommandFiles(false) = %v, want no files", files)
	}
}

func TestOpenCodeCommandsFailClosedWithoutApprovedBoundary(t *testing.T) {
	_, err := renderOpenCodeCommandFiles(RenderRequest{Target: TargetOpenCode}, true)
	if err == nil || !strings.Contains(err.Error(), "approved explicit command asset boundary") {
		t.Fatalf("renderOpenCodeCommandFiles(true) error = %v, want fail-closed command-boundary error", err)
	}
}

func TestOpenCodeMergePreservesUserJSON(t *testing.T) {
	desired, err := renderOpenCodeLoreBlock(agentconfig.DefaultConfig(), false)
	if err != nil {
		t.Fatalf("renderOpenCodeLoreBlock() error = %v, want nil", err)
	}
	existing := []byte(`{"theme":"midnight","nested":{"keep":true}}`)
	merged, err := mergeOpenCodeJSON(existing, desired)
	if err != nil {
		t.Fatalf("mergeOpenCodeJSON() error = %v, want nil", err)
	}
	text := string(merged)
	if !containsAll(text, `"theme": "midnight"`, `"nested": {`, `"keep": true`, `"lore": {`, `"managed_by": "lore-cli"`) {
		t.Fatalf("merged opencode.json = %q, want preserved user keys plus lore block", text)
	}
}

func TestOpenCodeMergeRejectsAmbiguousLoreOwnership(t *testing.T) {
	desired, err := renderOpenCodeLoreBlock(agentconfig.DefaultConfig(), false)
	if err != nil {
		t.Fatalf("renderOpenCodeLoreBlock() error = %v, want nil", err)
	}

	tests := []struct {
		name     string
		existing []byte
		want     string
	}{
		{name: "invalid json", existing: []byte(`{"lore":`), want: "decode existing opencode.json"},
		{name: "non object root", existing: []byte(`[]`), want: "must contain a JSON object"},
		{name: "non object lore", existing: []byte(`{"lore":true}`), want: "must be an object"},
		{name: "missing managed_by", existing: []byte(`{"lore":{"schema_version":1}}`), want: `not managed by "lore-cli"`},
		{name: "foreign managed_by", existing: []byte(`{"lore":{"managed_by":"someone-else"}}`), want: `not managed by "lore-cli"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := mergeOpenCodeJSON(tt.existing, desired)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("mergeOpenCodeJSON() error = %v, want substring %q", err, tt.want)
			}
		})
	}
}
