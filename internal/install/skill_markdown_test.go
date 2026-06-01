package install

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alferio94/lore-cli/internal/agentpack"
)

func TestRenderManagedSkillMarkdownUsesBlockScalarDescriptions(t *testing.T) {
	skills := agentpack.OperationalAssets{}.ExtendedSkills(agentpack.PiSkillPathResolver())
	for _, skill := range skills {
		content := renderManagedSkillMarkdown(skill)
		assertExtendedSkillDescriptionBlock(t, skill.Name, content)
	}
}

func TestRenderedExtendedSkillsUseBlockScalarDescriptionsForPiAndAntigravity(t *testing.T) {
	homeDir := t.TempDir()

	piFiles, err := renderPiFiles(ResolvePiLayout(homeDir), PiInstallRequest{
		HomeDir:        homeDir,
		ServerURL:      "https://lore.example",
		LoreBinaryPath: "/usr/local/bin/lore",
		LoreConfigDir:  filepath.Join(homeDir, ".lore"),
		LoreCLIVersion: "v1.2.3",
		Now:            time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("renderPiFiles error: %v", err)
	}
	for _, file := range piFiles {
		if !isExtendedSkillPath(file.relativePath) {
			continue
		}
		assertExtendedSkillDescriptionBlock(t, file.relativePath, string(file.content))
	}

	agFiles, err := renderAntigravityFiles(InstallRequest{
		HomeDir:        homeDir,
		ServerURL:      "https://lore.example",
		SavedToken:     "secret-token",
		LoreBinaryPath: "/usr/local/bin/lore",
		LoreConfigDir:  filepath.Join(homeDir, ".lore"),
		LoreCLIVersion: "v1.2.3",
		Target:         TargetAntigravity,
		Now:            time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("renderAntigravityFiles error: %v", err)
	}
	for _, file := range agFiles {
		if !isExtendedSkillPath(file.RelativePath) {
			continue
		}
		assertExtendedSkillDescriptionBlock(t, file.RelativePath, string(file.Content))
	}
}

func isExtendedSkillPath(path string) bool {
	return path == "skills/judgment-day/SKILL.md" ||
		path == "skills/skill-creator/SKILL.md" ||
		path == "skills/skill-registry/SKILL.md"
}

func assertExtendedSkillDescriptionBlock(t *testing.T, name, content string) {
	t.Helper()

	if !strings.Contains(content, "description: >\n") {
		t.Fatalf("%s missing block-scalar description: %q", name, content[:min(len(content), 200)])
	}
	if strings.Contains(content, "description: Parallel adversarial review protocol. Trigger:") ||
		strings.Contains(content, "description: Create or refresh the project skill registry for Lore-based workflows. Trigger:") ||
		strings.Contains(content, "description: Creates new AI agent skills following the Agent Skills spec. Trigger:") {
		t.Fatalf("%s still contains inline description+Trigger frontmatter: %q", name, content[:min(len(content), 200)])
	}

	parts := strings.SplitN(content, "---\n", 3)
	if len(parts) < 3 {
		t.Fatalf("%s missing expected frontmatter delimiters", name)
	}
	frontmatter := parts[1]
	if !strings.Contains(frontmatter, "description: >\n  ") {
		t.Fatalf("%s description block is not indented: %q", name, frontmatter)
	}
	if !strings.Contains(frontmatter, "Trigger:") {
		t.Fatalf("%s description block missing Trigger text: %q", name, frontmatter)
	}
}
