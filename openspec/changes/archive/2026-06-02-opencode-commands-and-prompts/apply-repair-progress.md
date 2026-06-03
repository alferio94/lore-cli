# Apply Repair Progress: opencode-commands-and-prompts

## Status
- Mode: Standard
- Current slice: completed
- Completed tasks: 9/12 (remaining: 3.3, 5.1-5.2)
- Repair phase tasks: all critical and warning findings from failed verify fixed

## Completed Repair Tasks

### Critical 1: Command contract mismatch — sdd-proposal → sdd-propose
- [x] Fixed `renderOpenCodeSDDCommands` to map PhaseProposal to canonical "propose" name
- [x] Fixed `phaseLabel` to return "SDD P — Proposal" for the propose command
- [x] Updated `TestOpenCodeSDDAssetsRendersAllNineCommandFiles` to expect sdd-propose (not sdd-proposal)
- [x] Added negative assertion: sdd-proposal.md must NOT exist
- [x] Updated `TestOpenCodeSDDAssetsCommandFilesHavePhaseFrontmatter` to verify sdd-propose.md has `name: sdd-propose` frontmatter
- [x] Updated `TestOpenCodeSDDAssetsPlanApplyBackupManifest` to assert sdd-propose.md exists and sdd-proposal.md does NOT exist

### Critical 2: Prompt asset surface mismatch
- [x] Replaced single `prompts/sdd/system-prompt-guidance.md` with 9 per-phase `prompts/sdd/sdd-*.md` files
- [x] Added `canonicalPhaseName` helper to map PhaseProposal → "propose" in paths/names
- [x] Replaced `TestOpenCodeSDDAssetsRendersPromptGuidance` with `TestOpenCodeSDDAssetsRendersPerPhasePromptFiles` that verifies all 9 per-phase prompts exist, sdd-proposal.md does NOT exist, sdd-propose.md DOES exist, system-prompt-guidance.md does NOT exist, each prompt has frontmatter + Key Boundaries section + OpenCode/Lore context
- [x] Fixed "gentle-orchestrator" banned phrase by removing the substring from Prompt Asset Note (the word "orchestrator" alone is allowed; only "gentle-orchestrator" combined is banned)

### Critical 3: Missing component-selected lifecycle proof
- [x] Added `TestOpenCodeSDDAssetsPlanApplyBackupManifest`: proves plan/execute/manifest/backup for sdd-propose.md with exact path/hash/unchanged assertions
- [x] Added `TestOpenCodeSDDAssetsIdempotentRerun`: proves second install classifies sdd asset files as unchanged
- [x] Added `TestOpenCodeSDDAssetsBackupBeforeOverwrite`: proves backup created before overwrite and stale content replaced

### Warning 4: CLI/TUI/README copy incomplete
- [x] Updated `internal/cli/app.go` install help: describes optional opencode-sdd-assets component, assets-only claims, omit-by-default
- [x] Updated `internal/tui/root.go` renderInstallTargetSelection: describes optional SDD command/prompt assets, opencode-sdd-assets component
- [x] Updated `README.md` OpenCode section: describes optional SDD command/prompt assets, opencode-sdd-assets component, bounded assets-only claims
- [x] Updated `TestInstallUsageIncludesTargetAndComponentFlags` to expect new copy wording

### Warning 5: Task artifact out of sync
- [x] Tasks updated in filesystem checkpoint (openspec/changes/opencode-commands-and-prompts/tasks.md)

## Files Changed Cumulative

| File | Action | Task(s) | Notes |
|------|--------|---------|-------|
| internal/install/adapter_opencode.go | Modified | 2.1, 2.2 | Fix sdd-propose naming, per-phase prompts, canonicalPhaseName helper |
| internal/install/adapter_opencode_test.go | Modified | 2.3 | Fix sdd-propose assertions, replace prompt test, per-phase coverage |
| internal/install/opencode_install_test.go | Modified | 3.1, 3.2, 3.3 | Add lifecycle tests for plan/apply/backup/manifest/idempotency |
| internal/cli/app.go | Modified | 4.1 | Update install help with optional SDD assets description |
| internal/cli/install_flags_test.go | Modified | 4.1 | Update test for new help copy |
| internal/tui/root.go | Modified | 4.1 | Update TUI install target message with optional SDD assets |
| README.md | Modified | 4.2 | Update OpenCode section with optional SDD assets description |
| openspec/changes/opencode-commands-and-prompts/apply-repair-started.md | Created | repair | Repair slice checkpoint |
| openspec/changes/opencode-commands-and-prompts/tasks.md | Modified | 5.1-5.2 | Task progress tracking |

## Validation Cumulative

| Command | Scope | Result | Notes |
|---------|-------|--------|-------|
| go test ./internal/install -run 'TestOpenCodeSDD\|TestOpenCode.*Command\|TestOpenCode.*Prompt' -v | Focused install SDD | 13/13 PASS | All SDD asset tests pass |
| go test ./internal/cli -run 'TestInstall\|TestOpenCode' -v | Focused CLI tests | 22/22 PASS | All CLI/install tests pass |
| go test ./internal/tui -run 'TestInstall' -v | Focused TUI tests | 9/9 PASS | All TUI install tests pass |
| go test ./... | Repository-wide | ALL PASS | All packages pass |

## Deviations and Risks
- The word "orchestrator" appears in SDD phase agent bodies and prompt content (legitimate use); only the combined "gentle-orchestrator" substring is banned as specified in design.
- No changes to MCP behavior, plugins, profiles, TUI plugins, bootstrap, or runtime subagent claims.
- Prompts are inert install-time assets; no runtime wiring in this scope.

## Next Slice Recommendation
- Tasks: 5.1 (final focused validation), 5.2 (broader repo validation)
- Why: all critical findings and Phase 3/4 tasks are now complete; Phase 5 validation is the last step before verify.