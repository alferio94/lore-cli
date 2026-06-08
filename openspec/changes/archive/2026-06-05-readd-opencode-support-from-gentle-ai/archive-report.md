# Archive Report: readd-opencode-support-from-gentle-ai

**Change**: readd-opencode-support-from-gentle-ai  
**Status**: archived  
**Date**: 2026-06-05  
**Implementation commit**: `a8a9edf` — Re-add OpenCode install support

## Traceability

Referenced artifact IDs from the change lifecycle:
- Exploration: `79232292-fd2b-4230-ba64-99b2de957833`
- Proposal: `1ac3dba1-080f-487e-9ac6-14193a2b1cd2`
- Spec: `fdc8cb5e-2a86-45fb-b80b-60ddefdf5eb5`
- Design: `4714f031-6cc6-4a9e-b5df-d4f38ebbf90d`
- Prior verify: `815458a6-f8ed-465e-be89-209855472645`
- Final verify: `84756d19-7ef4-4a25-93fa-b847963c7098`

## Final Verified Behavior

The archived change restores OpenCode install support with the corrected contract:
- `background-agents.ts` and `model-variants.ts` are copied to `~/.config/opencode/plugins/`.
- Those local plugin files are **not** registered in `tui.json`.
- `tui.json` registers only the community `opencode-subagent-statusline`.
- Foreign/non-Lore-owned `mcp.lore` blocks fail closed with backup-before-abort semantics.
- Lore-owned or absent `mcp.lore` blocks proceed with additive merge.
- `mcp.lore` rendering carries `managed_by: lore-cli`.
- OpenCode assets exclude `sdd-engram` and `logo`.

## Verification

Final verification status: **PASS**.

Evidence recorded in the final verify memory and repository tests:
- Full suite pass: `go test ./...`
- Focused OpenCode/install/TUI checks passed, including the foreign/resolved `mcp.lore` ownership path and the corrected plugin-registration semantics.
- Cleanup polish resolved the stale install-help target list and the misleading OpenCode test name.

## Archive Result

- No OpenSpec `openspec/specs/` tree existed in this repository fallback, so there was no filesystem spec delta to merge.
- The completed change has been moved to the archive location and is now read-only history.
- All phase artifacts remain preserved for traceability.
