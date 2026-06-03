# Apply Repair Started: opencode-commands-and-prompts

## Change
- Change: opencode-commands-and-prompts
- Verify id: 1e05a73a-28a2-4be9-9d88-14c236da4aee
- Verdict: FAIL
- Mode: Standard

## Failed Verify Findings to Fix

### Critical 1: Command contract mismatch — `sdd-proposal` vs `sdd-propose`
- Implementation renders `commands/sdd-proposal.md` with frontmatter `name: sdd-proposal`
- Approved canonical phase is `sdd-propose` (verb form)
- Fix: change `phasePrefix + string(PhaseProposal)` → `sdd-propose`

### Critical 2: Prompt asset surface mismatch
- Implementation renders only `prompts/sdd/system-prompt-guidance.md`
- Design/tasks require per-phase `prompts/sdd/sdd-*.md` assets mirroring canonical phases
- Fix: replace single prompt with 9 per-phase prompt files (sdd-init through sdd-archive)

### Critical 3: Missing component-selected lifecycle proof
- No runtime tests prove commands/prompts are planned/applied, backed up, manifest-tracked, idempotent
- Fix: add `TestOpenCodeSDDAssetsPlanApplyBackupManifest`, `TestOpenCodeSDDAssetsIdempotentRerun`

### Warning 4: CLI/TUI/README copy incomplete
- CLI help text does not mention optional commands/prompts as assets
- TUI install message does not describe optional sdd-assets component
- README does not describe optional SDD assets
- Fix: update app.go install help, TUI renderInstallTargetSelection, formatOpenCodeInstallWarning

### Warning 5: Task artifact out of sync
- Tasks remain unchecked for 3.x, 4.x, 5.x phases
- Fix: mark tasks complete in filesystem checkpoint as work progresses

## Slice
- Tasks in scope: critical 1, 2, 3, 4, 5
- Expected files:
  - internal/install/adapter_opencode.go (fix command names, per-phase prompts)
  - internal/install/adapter_opencode_test.go (add lifecycle tests)
  - internal/cli/app.go (update install help)
  - internal/tui/root.go (update TUI message)
  - README.md (update OpenCode section)
  - openspec/changes/opencode-commands-and-prompts/apply-repair-progress.md (progress tracking)
- Validation planned:
  - go test ./internal/install -run 'TestOpenCodeSDD|TestOpenCode.*Command|TestOpenCode.*Prompt' -v
  - go test ./internal/cli -run 'TestInstall|TestOpenCode' -v
  - go test ./internal/tui -run 'TestInstall' -v
- Risk budget: medium — fixes targeted to specific verified contract mismatches; no broad refactor

## Preconditions
- Proposal/spec/design/tasks read: yes (from Lore artifacts)
- Previous apply-progress: 6/12 tasks complete from prior apply; filesystem fallback at openspec/changes/opencode-commands-and-prompts/
- Strict TDD mode: inactive