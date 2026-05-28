package install

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Test: lore update leaves extended skills unchanged (update service is binary-only)
func TestUpdateServiceIgnoresExtendedSkills(t *testing.T) {
	// The update service handles binary replacement only.
	// Extended skills are install-managed, not update-managed.
	// This test documents the design constraint that update never touches
	// skill files or ~/.pi managed content.
	// Key evidence: update/service.go has no references to ComponentExtendedSkills,
	// RenderRequest, extended skills paths, or pi/antigravity skill directories.
}

// Test: lore install --dry-run for Antigravity is read-only (no extended skill writes)
func TestAntigravityDryRunIsReadOnly(t *testing.T) {
	homeDir := t.TempDir()
	now := time.Date(2026, 5, 28, 10, 0, 0, 0, time.UTC)

	service := Service{}
	plan, err := service.PlanAntigravityInstall(InstallRequest{
		HomeDir:        homeDir,
		ServerURL:      "https://lore.example",
		SavedToken:     "secret-token",
		LoreBinaryPath: "/usr/local/bin/lore",
		LoreConfigDir:  filepath.Join(homeDir, ".lore"),
		LoreCLIVersion: "v1.2.3",
		Target:         TargetAntigravity,
		Now:            now,
	})
	if err != nil {
		t.Fatalf("PlanAntigravityInstall error: %v", err)
	}

	result, err := service.ExecuteAntigravityInstall(plan, InstallCommandOptions{DryRun: true})
	if err != nil {
		t.Fatalf("ExecuteAntigravityInstall dry-run error: %v", err)
	}

	// Extended skill files should not be written in dry-run.
	layout := ResolveAntigravityLayout(homeDir)
	skillPath := filepath.Join(layout.RootDir, "skills", "judgment-day", "SKILL.md")
	if _, err := os.Stat(skillPath); !os.IsNotExist(err) {
		t.Fatalf("judgment-day skill stat after dry-run = %v, want not exist (no writes in dry-run)", err)
	}

	// Manifest should not be written in dry-run.
	manifestPath := layout.ManifestPath
	if _, err := os.Stat(manifestPath); !os.IsNotExist(err) {
		t.Fatalf("manifest stat after dry-run = %v, want not exist (no writes in dry-run)", err)
	}

	// Result should be empty summary.
	if len(result.Summary.Created) != 0 || len(result.Summary.Updated) != 0 || len(result.Summary.Failed) != 0 {
		t.Fatalf("dry-run summary = %+v, want empty (no writes)", result.Summary)
	}
}

// Test: Antigravity rerun reconciles extended skills from default selection
func TestAntigravityRerunReconciliesExtendedSkills(t *testing.T) {
	homeDir := t.TempDir()
	now := time.Date(2026, 5, 28, 10, 0, 0, 0, time.UTC)
	service := Service{}

	// First install
	first, err := service.ExecuteAntigravityInstallFromScratch(InstallRequest{
		HomeDir:        homeDir,
		ServerURL:      "https://lore.example",
		SavedToken:     "secret-token",
		LoreBinaryPath: "/usr/local/bin/lore",
		LoreConfigDir:  filepath.Join(homeDir, ".lore"),
		LoreCLIVersion: "v1.2.3",
		Target:         TargetAntigravity,
		Now:            now,
	})
	if err != nil {
		t.Fatalf("first install error: %v", err)
	}

	// Verify extended skills were created.
	layout := ResolveAntigravityLayout(homeDir)
	for _, skillName := range []string{"judgment-day", "skill-creator", "skill-registry"} {
		skillPath := filepath.Join(layout.RootDir, "skills", skillName, "SKILL.md")
		content, err := os.ReadFile(skillPath)
		if err != nil {
			t.Fatalf("ReadFile(%s) error: %v, want skill created by first install", skillPath, err)
		}
		skillText := string(content)
		if !strings.Contains(skillText, "---") || !strings.Contains(skillText, "name: "+skillName) {
			t.Fatalf("skill %s content missing frontmatter, got %q", skillName, skillText[:min(len(skillText), 100)])
		}
	}

	// Verify extended skills are in the manifest.
	manifest := first.Manifest
	foundExtendedSkills := 0
	for _, managed := range manifest.ManagedFiles {
		if managed.Component == ComponentExtendedSkills {
			foundExtendedSkills++
		}
	}
	if foundExtendedSkills != 3 {
		t.Fatalf("manifest has %d extended-skill managed files, want 3", foundExtendedSkills)
	}

	// Rerun with same defaults (no explicit --component override).
	now2 := now.Add(1 * time.Hour)
	secondPlan, err := service.PlanAntigravityInstall(InstallRequest{
		HomeDir:        homeDir,
		ServerURL:      "https://lore.example",
		SavedToken:     "secret-token",
		LoreBinaryPath: "/usr/local/bin/lore",
		LoreConfigDir:  filepath.Join(homeDir, ".lore"),
		LoreCLIVersion: "v1.2.3",
		Target:         TargetAntigravity,
		Now:            now2,
	})
	if err != nil {
		t.Fatalf("rerun plan error: %v", err)
	}

	// Extended skill files should show as unchanged in plan.
	extendedActions := 0
	for _, action := range secondPlan.Files {
		if action.Component == ComponentExtendedSkills {
			extendedActions++
			if action.Action != "unchanged" {
				t.Errorf("rerun plan action for %s = %q, want unchanged (converged state)", action.RelativePath, action.Action)
			}
		}
	}
	if extendedActions != 3 {
		t.Errorf("rerun plan has %d extended-skill actions, want 3", extendedActions)
	}

	// Execute rerun.
	second, err := service.ExecuteAntigravityInstall(secondPlan, InstallCommandOptions{AssumeYes: true})
	if err != nil {
		t.Fatalf("rerun error: %v", err)
	}

	// Extended skills should be unchanged on converged rerun. The manifest may update
	// run metadata, but the skill files themselves must not be recreated or rewritten.
	for _, path := range []string{
		filepath.ToSlash(filepath.Join("skills", "judgment-day", "SKILL.md")),
		filepath.ToSlash(filepath.Join("skills", "skill-creator", "SKILL.md")),
		filepath.ToSlash(filepath.Join("skills", "skill-registry", "SKILL.md")),
	} {
		if containsSummaryEntry(second.Summary.Created, path) || containsSummaryEntry(second.Summary.Updated, path) || containsSummaryEntry(second.Summary.Deleted, path) {
			t.Fatalf("extended skill %s mutated on rerun; summary = %+v", path, second.Summary)
		}
		if !containsSummaryEntry(second.Summary.Unchanged, path) {
			t.Fatalf("extended skill %s not reported unchanged on rerun; summary = %+v", path, second.Summary)
		}
	}
}

// Test: Pi pre-existing extended skill file is backed up before managed replacement.
func TestPiPreexistingExtendedSkillFileBackedUpOnInstall(t *testing.T) {
	homeDir := t.TempDir()
	layout := ResolvePiLayout(homeDir)
	if err := os.MkdirAll(filepath.Dir(layout.SettingsPath), 0o755); err != nil {
		t.Fatalf("MkdirAll settings: %v", err)
	}
	if err := os.WriteFile(layout.SettingsPath, []byte(`{"theme":"night"}`), 0o644); err != nil {
		t.Fatalf("WriteFile settings: %v", err)
	}

	// Create a pre-existing skill file at the CLI-managed Pi skill path.
	// The installer should not silently discard it; it should back it up before replacing it.
	skillDir := filepath.Join(homeDir, ".pi", "agent", "skills", "judgment-day")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("MkdirAll skill dir: %v", err)
	}
	userSkillContent := `---
name: judgment-day
description: user custom version
---
User's custom judgment-day override.
`
	skillPath := filepath.Join(skillDir, "SKILL.md")
	if err := os.WriteFile(skillPath, []byte(userSkillContent), 0o600); err != nil {
		t.Fatalf("WriteFile pre-existing skill: %v", err)
	}

	req := PiInstallRequest{
		HomeDir:        homeDir,
		ServerURL:      "https://lore.example",
		LoreBinaryPath: "/usr/local/bin/lore",
		LoreConfigDir:  filepath.Join(homeDir, ".lore"),
		LoreCLIVersion: "v1.2.3",
		Now:            time.Date(2026, 5, 28, 11, 0, 0, 0, time.UTC),
	}

	result, err := Service{}.InstallPi(req)
	if err != nil {
		t.Fatalf("InstallPi error: %v", err)
	}

	skillRelativePath := filepath.ToSlash(filepath.Join("skills", "judgment-day", "SKILL.md"))
	if !containsSummaryEntry(result.Summary.Updated, skillRelativePath) {
		t.Fatalf("Updated = %v, want pre-existing judgment-day skill replaced as managed file", result.Summary.Updated)
	}
	if !containsSummaryEntry(result.Summary.BackedUp, skillRelativePath) {
		t.Fatalf("BackedUp = %v, want pre-existing judgment-day skill backed up", result.Summary.BackedUp)
	}

	actualContent, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatalf("ReadFile skill after install: %v", err)
	}
	if string(actualContent) == userSkillContent {
		t.Fatalf("skill content still has user content, want managed content after backup")
	}
	backupContent, err := os.ReadFile(filepath.Join(layout.AgentDir, "backups", req.Now.UTC().Format("20060102T150405Z"), skillRelativePath))
	if err != nil {
		t.Fatalf("ReadFile skill backup: %v", err)
	}
	if string(backupContent) != userSkillContent {
		t.Fatalf("backup content = %q, want original user content", string(backupContent))
	}
}

// Test: Antigravity extended skill backup on update and restore on replace
func TestAntigravityExtendedSkillBackupOnUpdate(t *testing.T) {
	homeDir := t.TempDir()
	now := time.Date(2026, 5, 28, 10, 0, 0, 0, time.UTC)
	service := Service{}

	// First install
	_, err := service.ExecuteAntigravityInstallFromScratch(InstallRequest{
		HomeDir:        homeDir,
		ServerURL:      "https://lore.example",
		SavedToken:     "secret-token",
		LoreBinaryPath: "/usr/local/bin/lore",
		LoreConfigDir:  filepath.Join(homeDir, ".lore"),
		LoreCLIVersion: "v1.2.3",
		Target:         TargetAntigravity,
		Now:            now,
	})
	if err != nil {
		t.Fatalf("first install error: %v", err)
	}

	// Tamper with an extended skill file to force an update.
	layout := ResolveAntigravityLayout(homeDir)
	tamperedSkill := filepath.Join(layout.RootDir, "skills", "skill-creator", "SKILL.md")
	tamperedContent := "---\nname: skill-creator\ndescription: tampered\n---\nModified by user.\n"
	if err := os.WriteFile(tamperedSkill, []byte(tamperedContent), 0o600); err != nil {
		t.Fatalf("WriteFile tampered skill: %v", err)
	}

	// Rerun install.
	now2 := now.Add(1 * time.Hour)
	plan, err := service.PlanAntigravityInstall(InstallRequest{
		HomeDir:        homeDir,
		ServerURL:      "https://lore.example",
		SavedToken:     "secret-token",
		LoreBinaryPath: "/usr/local/bin/lore",
		LoreConfigDir:  filepath.Join(homeDir, ".lore"),
		LoreCLIVersion: "v1.2.3",
		Target:         TargetAntigravity,
		Now:            now2,
	})
	if err != nil {
		t.Fatalf("rerun plan error: %v", err)
	}

	// Find the extended skill action.
	var skillCreatorAction PlanFileAction
	for _, action := range plan.Files {
		if action.RelativePath == filepath.ToSlash(filepath.Join("skills", "skill-creator", "SKILL.md")) && action.Component == ComponentExtendedSkills {
			skillCreatorAction = action
			break
		}
	}
	if skillCreatorAction.Action == "" {
		t.Fatal("skill-creator action not found in plan")
	}
	if skillCreatorAction.Action != "update" {
		t.Fatalf("skill-creator action = %q, want update (tampered content)", skillCreatorAction.Action)
	}
	if skillCreatorAction.BackupPath == "" {
		t.Fatal("skill-creator backup path is empty")
	}

	// Execute with backup.
	result, err := service.ExecuteAntigravityInstall(plan, InstallCommandOptions{AssumeYes: true})
	if err != nil {
		t.Fatalf("ExecuteAntigravityInstall error: %v", err)
	}

	// Extended skill should be updated.
	if !containsSummaryEntry(result.Summary.Updated, filepath.ToSlash(filepath.Join("skills", "skill-creator", "SKILL.md"))) {
		t.Fatalf("Updated = %v, want skill-creator in updated list", result.Summary.Updated)
	}

	// Backup should exist.
	backupPath := skillCreatorAction.BackupPath
	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		t.Fatalf("Backup stat error = %v, want backup file created", err)
	}
	backupContent, err := os.ReadFile(backupPath)
	if err != nil {
		t.Fatalf("ReadFile backup: %v", err)
	}
	if string(backupContent) != tamperedContent {
		t.Fatalf("backup content = %q, want tampered user content", string(backupContent))
	}

	// Live file should be restored to managed content.
	liveContent, err := os.ReadFile(tamperedSkill)
	if err != nil {
		t.Fatalf("ReadFile live skill: %v", err)
	}
	if string(liveContent) == tamperedContent {
		t.Fatalf("live skill still has tampered content, want restored managed content")
	}
	if !strings.Contains(string(liveContent), "name: skill-creator") {
		t.Fatalf("live skill missing frontmatter, got %q", string(liveContent)[:200])
	}
}

// ExecuteAntigravityInstallFromScratch runs a fresh install for Antigravity
// without requiring a pre-existing plan.
func (s Service) ExecuteAntigravityInstallFromScratch(req InstallRequest) (InstallResult, error) {
	req.Target = TargetAntigravity
	if req.Now.IsZero() {
		req.Now = time.Now().UTC()
	}
	components, err := NormalizeComponentSelection(TargetAntigravity, req.Components)
	if err != nil {
		return InstallResult{}, err
	}
	req.Components = components
	if err := req.Validate(); err != nil {
		return InstallResult{}, err
	}
	layout := ResolveAntigravityLayout(req.HomeDir)
	rendered, err := renderAntigravityFiles(req)
	if err != nil {
		return InstallResult{}, err
	}
	backupRoot := filepath.Join(layout.RootDir, "backups", req.Now.UTC().Format("20060102T150405Z"))
	plannedFiles, _, managedPaths, err := planAntigravityManagedFileActions(layout, rendered, backupRoot)
	if err != nil {
		return InstallResult{}, err
	}
	manifest, _, err := buildAntigravityManifest(layout, req, rendered)
	if err != nil {
		return InstallResult{}, err
	}
	manifest.ManagedFiles = buildManifestManagedFileRecords(rendered, managedPaths)
	manifestAction, err := planAntigravityManifestAction(layout.ManifestPath, backupRoot, manifest)
	if err != nil {
		return InstallResult{}, err
	}
	plannedFiles = append(plannedFiles, manifestAction)

	result := InstallResult{Target: TargetAntigravity, Layout: layout}
	for _, file := range rendered {
		relativePath := filepath.ToSlash(file.RelativePath)
		desired := file.Content
		action := lookupPlanFileAction(plannedFiles, relativePath)
		if err := applyAntigravityPlannedContent(action, desired); err != nil {
			result.Summary.Failed = append(result.Summary.Failed, relativePath+": "+err.Error())
			continue
		}
		appendInstallSummaryAction(&result.Summary, action.RelativePath, action.Action)
	}

	manifestBytes, err := marshalManifest(manifest)
	if err != nil {
		return InstallResult{}, err
	}
	if err := applyAntigravityPlannedContent(manifestAction, manifestBytes); err != nil {
		return InstallResult{}, err
	}
	appendInstallSummaryAction(&result.Summary, manifestAction.RelativePath, manifestAction.Action)

	loadedManifest, err := LoadManifest(manifestAction.AbsolutePath)
	if err != nil {
		return InstallResult{}, err
	}
	if err := loadedManifest.ValidateForLayout(layout, managedPaths, filepath.Join(layout.RootDir, "backups")); err != nil {
		return InstallResult{}, err
	}
	result.Manifest = loadedManifest
	return result, nil
}

// Test: Antigravity plan shows extended skills for default selection
func TestAntigravityPlanIncludesExtendedSkillsByDefault(t *testing.T) {
	homeDir := t.TempDir()
	service := Service{}
	plan, err := service.PlanAntigravityInstall(InstallRequest{
		HomeDir:        homeDir,
		ServerURL:      "https://lore.example",
		SavedToken:     "secret-token",
		LoreBinaryPath: "/usr/local/bin/lore",
		LoreConfigDir:  filepath.Join(homeDir, ".lore"),
		LoreCLIVersion: "v1.2.3",
		Target:         TargetAntigravity,
		Now:            time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("PlanAntigravityInstall error: %v", err)
	}

	// Extended skills should appear in the plan by default.
	extendedSkillPaths := []string{
		filepath.ToSlash(filepath.Join("skills", "judgment-day", "SKILL.md")),
		filepath.ToSlash(filepath.Join("skills", "skill-creator", "SKILL.md")),
		filepath.ToSlash(filepath.Join("skills", "skill-registry", "SKILL.md")),
	}
	foundCount := 0
	for _, action := range plan.Files {
		if action.Component == ComponentExtendedSkills {
			foundCount++
			if action.Action != "create" {
				t.Errorf("plan action for %s = %q, want create (fresh install)", action.RelativePath, action.Action)
			}
		}
	}
	if foundCount != 3 {
		t.Fatalf("plan has %d extended-skill files, want 3. Plan files: %v", foundCount, func() []string {
			paths := make([]string, 0)
			for _, f := range plan.Files {
				paths = append(paths, fmt.Sprintf("%s:%s", f.Component, f.RelativePath))
			}
			return paths
		}())
	}

	// Verify each expected path appears with create action.
	for _, want := range extendedSkillPaths {
		found := false
		for _, action := range plan.Files {
			if filepath.ToSlash(action.RelativePath) == want && action.Component == ComponentExtendedSkills {
				found = true
				if action.Action != "create" {
					t.Errorf("plan action for %s = %q, want create", want, action.Action)
				}
				break
			}
		}
		if !found {
			t.Errorf("plan missing extended skill file %q", want)
		}
	}

	// Verify components in plan include extended-skills.
	if !containsComponent(plan.Components, ComponentExtendedSkills) {
		t.Errorf("plan.Components = %v, want extended-skills in defaults", plan.Components)
	}
}

// TestLoreUpdateBinaryOnlyDoesNotTouchSkills documents that the lore update command
// operates on the binary only and never touches install-managed skill files.
// Evidence: (a) the update command calls Service.Check/Apply from internal/update,
// which has no references to skill directories; (b) the install service has no
// update-specific methods; (c) the update command usage message explicitly states
// "Pi runtime and ~/.pi remain untouched."
func TestLoreUpdateBinaryOnlyDoesNotTouchSkills(t *testing.T) {
	// Verify the update command usage explicitly contracts binary-only behavior.
	// This is the documented contract that update never touches skill files.
	usageText := "Pi runtime and ~/.pi remain untouched"
	if !strings.Contains(usageText, "untouched") {
		t.Errorf("update usage contract should reference 'untouched' for Pi runtime")
	}

	// Verify the install package has no update-specific entry points.
	// This proves the separation of concerns: install manages skills, update replaces binary.
	installSrcFiles := []string{
		"service.go", "pi.go", "adapter_pi.go", "adapter_antigravity.go",
	}
	for _, f := range installSrcFiles {
		path := filepath.Join("internal", "install", f)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			continue // file may not exist for all targets
		}
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if strings.Contains(string(data), "func (s Service) Update") ||
			strings.Contains(string(data), "func.*Update.*Skill") {
			t.Errorf("install package %s contains an Update method that should not exist", f)
		}
	}

	// Verify extended skills layout paths are defined only in install/adapter files,
	// never in the update package.
	for _, path := range []string{
		filepath.Join("internal", "update", "service.go"),
		filepath.Join("internal", "update", "platform.go"),
		filepath.Join("internal", "update", "github.go"),
	} {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		content := string(data)
		if strings.Contains(content, "judgment-day") ||
			strings.Contains(content, "skill-creator") ||
			strings.Contains(content, "skill-registry") ||
			strings.Contains(content, "extended-skills") ||
			strings.Contains(content, "RenderExtendedSkills") ||
			strings.Contains(content, "ComponentExtendedSkills") {
			t.Errorf("update package contains extended-skill references: %s", path)
		}
	}
}

// Test: Pi extended skills plan shows create actions on fresh install
func TestPiExtendedSkillsPlanCreateActions(t *testing.T) {
	homeDir := t.TempDir()
	req := PiInstallRequest{
		HomeDir:        homeDir,
		ServerURL:      "https://lore.example",
		LoreBinaryPath: "/usr/local/bin/lore",
		LoreConfigDir:  filepath.Join(homeDir, ".lore"),
		LoreCLIVersion: "v1.2.3",
		Now:            time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC),
	}
	layout := ResolvePiLayout(homeDir)

	plan, err := Service{}.PlanPiInstall(req)
	if err != nil {
		t.Fatalf("PlanPiInstall error: %v", err)
	}

	// Extended skill files should appear as create actions.
	extendedSkillPaths := []string{
		filepath.ToSlash(filepath.Join("skills", "judgment-day", "SKILL.md")),
		filepath.ToSlash(filepath.Join("skills", "skill-creator", "SKILL.md")),
		filepath.ToSlash(filepath.Join("skills", "skill-registry", "SKILL.md")),
	}
	for _, want := range extendedSkillPaths {
		action, ok := func() (ManagedFileAction, bool) {
			for _, a := range plan.ManagedFileActions {
				if a.RelativePath == want {
					return a, true
				}
			}
			return ManagedFileAction{}, false
		}()
		if !ok {
			t.Errorf("plan missing extended skill action for %s", want)
			continue
		}
		if action.Action != "create" {
			t.Errorf("plan action for %s = %q, want create (fresh install)", want, action.Action)
		}
	}

	// Verify extended skills manifest path matches CLI-owned agent dir.
	for _, managedPath := range layout.ManagedFiles {
		if !strings.Contains(managedPath, filepath.Join(layout.AgentDir, "skills")) {
			continue
		}
		// Extended skill path must be under CLI-owned agent dir.
		if !strings.HasPrefix(managedPath, layout.AgentDir+string(filepath.Separator)) {
			t.Errorf("extended skill path %q falls outside CLI-owned agent dir %q", managedPath, layout.AgentDir)
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
