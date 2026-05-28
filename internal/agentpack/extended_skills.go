package agentpack

// SkillInstructionAsset represents a portable non-agent skill that can be
// rendered to harness-specific skill directories.
type SkillInstructionAsset struct {
	Name        string
	Description string
	Body        string
}

// DefaultExtendedSkills returns the portable extended skill bundle:
// skill-creator, skill-registry, and judgment-day.
// These skills are harness-agnostic and avoid runtime-specific primitive names.
func DefaultExtendedSkills() []SkillInstructionAsset {
	return []SkillInstructionAsset{
		SkillCreatorPortable(),
		SkillRegistryPortable(),
		JudgmentDayPortable(),
	}
}

// SkillCreatorPortable returns the portable skill-creator skill content.
func SkillCreatorPortable() SkillInstructionAsset {
	return SkillInstructionAsset{
		Name:        "skill-creator",
		Description: "Creates new AI agent skills following the Agent Skills spec. Trigger: When user asks to create a new skill, add agent instructions, or document patterns for AI.",
		Body: `## When to Create a Skill

Create a skill when:
- A pattern is used repeatedly and AI needs guidance
- Project-specific conventions differ from generic best practices
- Complex workflows need step-by-step instructions
- Decision trees help AI choose the right approach

**Don't create a skill when:**
- Documentation already exists (create a reference instead)
- Pattern is trivial or self-explanatory
- It's a one-off task

## Skill Structure

skills/<skill-name>/
- SKILL.md              (Required - main skill file)
- assets/               (Optional - templates, schemas, examples)
- references/           (Optional - links to local docs)

## SKILL.md Template

---
name: <skill-name>
description: >
  <One-line description of what this skill does>.
  Trigger: <When the AI should load this skill>.
license: Apache-2.0
metadata:
  author: gentleman-programming
  version: "1.0"
---

## When to Use

<Bullet points of when to use this skill>

## Critical Patterns

<The most important rules - what AI MUST know>

## Code Examples

<Minimal, focused examples>

## Commands

<Common commands>

## Resources

- Templates: See assets/ for <description>
- Documentation: See references/ for local docs

## Naming Conventions

Type: Generic skill
Pattern: <technology>
Examples: pytest, playwright, typescript

Type: Project-specific
Pattern: <project>-<component>
Examples: myapp-api, myapp-ui

Type: Testing skill
Pattern: <project>-test-<component>
Examples: myapp-test-sdk, myapp-test-api

Type: Workflow skill
Pattern: <action>-<target>
Examples: skill-creator, jira-task

## Frontmatter Fields

| Field | Required | Description |
|-------|----------|-------------|
| name | Yes | Skill identifier (lowercase, hyphens) |
| description | Yes | What + Trigger in one block |
| license | Yes | Always Apache-2.0 |
| metadata.author | Yes | gentleman-programming |
| metadata.version | Yes | Semantic version as string |

## Content Guidelines

### DO
- Start with the most critical patterns
- Use tables for decision trees
- Keep code examples minimal and focused
- Include Commands section with copy-paste commands

### DON'T
- Add Keywords section (agent searches frontmatter, not body)
- Duplicate content from existing docs (reference instead)
- Include lengthy explanations (link to docs)
- Add troubleshooting sections (keep focused)
- Use web URLs in references (use local paths)

## Checklist Before Creating

- [ ] Skill doesn't already exist (check skills/)
- [ ] Pattern is reusable (not one-off)
- [ ] Name follows conventions
- [ ] Frontmatter is complete (description includes trigger keywords)
- [ ] Critical patterns are clear
- [ ] Code examples are minimal
- [ ] Commands section exists
- [ ] Added to AGENTS.md or harness skill registry

## Resources

- Templates: See assets/ for SKILL.md template
`,
	}
}

// SkillRegistryPortable returns the portable skill-registry skill content.
func SkillRegistryPortable() SkillInstructionAsset {
	return SkillInstructionAsset{
		Name:        "skill-registry",
		Description: "Create or refresh the project skill registry for Lore-based workflows. Trigger: When the user asks to update the skill registry, when no registry exists, or when a needed skill cannot be resolved.",
		Body: `## Purpose

Build a compact registry that lets delegators resolve the right standards with minimal token cost.

## Source Priority

1. project — .ai/skills/<skill_name>/SKILL.md, .pi/skills/, .agents/skills/
2. harness-global — harness-specific global skills directory
3. compatibility — optional external/tool-global skills only when explicitly enabled

## What to Capture Per Skill

- name
- display_name
- source
- path_or_origin
- stacks
- categories
- triggers
- compact_rules
- priority
- override_of when a project-local skill intentionally overrides a harness-global skill

## Build Steps

### 1. Scan project-local overrides
- Read .ai/skills/*/SKILL.md if present
- These are repo-specific overrides and should stay minimal

### 2. Scan harness-global skills
- Read relevant skills from the harness skills directory
- Prefer skills matching the project's actual stack, categories, and recurring work patterns
- Ignore unrelated global skills rather than dumping everything into the registry

### 3. Optionally include tool-global compatibility skills
Only when compatibility mode is explicitly enabled.

### 4. Deduplicate
- Same skill name from project and harness-global -> keep the project version first and record that it overrides harness-global
- Same skill name from harness-global and compatibility -> keep harness-global first

### 5. Generate compact rules blocks
Compact rules should be concise, imperative, and directly usable by workers.

### 6. Write the registry
Write to the appropriate harness-specific registry path (e.g., .atl/skill-registry.md).

### 7. Optionally save to Lore memory
If Lore memory writes are healthy, upsert the registry under topic key skill-registry.
If Lore persistence is degraded, keep the filesystem registry only and record that fallback explicitly.

## Output Shape

The registry should contain:
- source policy (project greater than harness-global by default)
- skills table
- compact rules section
- project convention file references
- last refresh note
- compatibility mode note when tool-global was included

## Rules

- Treat the harness-global skills directory as the default runtime-global source of truth
- Keep project-local skills minimal and specific
- Do not dump every skill in existence; prioritize relevance to the project's stack and workflows
- If no relevant skill is found, write an explicit empty result instead of guessing
`,
	}
}

// JudgmentDayPortable returns the harness-agnostic judgment-day skill.
// This version avoids Pi-specific primitive names and uses harness-native
// sub-agent delegation and result-retrieval wording.
func JudgmentDayPortable() SkillInstructionAsset {
	return SkillInstructionAsset{
		Name:        "judgment-day",
		Description: "Parallel adversarial review protocol. Trigger: When user says 'judgment day', 'judgment-day', 'review adversarial', 'dual review', or equivalent.",
		Body: `## When to Use

- User explicitly asks for 'judgment day', 'judgment-day', or equivalent trigger phrases
- After significant implementations before merging
- When high-confidence review of code, features, or architecture is needed
- When a single reviewer might miss edge cases or have blind spots

## Critical Patterns

### Pattern 0: Skill Resolution (BEFORE launching judges)

Follow the skill resolver protocol before launching ANY sub-agent:

1. Obtain the skill registry: search for skill-registry observation, fallback to .atl/skill-registry.md from the project root, skip if none
2. Identify the target files/scope — what code will the judges review?
3. Match relevant skills from the registry's Compact Rules by code context and task context
4. Build a Project Standards (auto-resolved) block with the matching compact rules
5. Inject this block into BOTH Judge prompts AND the Fix Agent prompt (identical for all)

If no registry exists: warn the user and proceed with generic review only.

### Pattern 1: Parallel Blind Review

- Launch TWO sub-agents via harness-native delegation (async, parallel — never sequential)
- Each agent receives the same target but works independently
- Neither agent knows about the other — no cross-contamination
- Both use identical review criteria but may find different issues
- NEVER do the review yourself as the orchestrator — your job is coordination only

### Pattern 2: Verdict Synthesis

The orchestrator compares results after both result retrieval calls return:

Confirmed   -> found by BOTH agents          -> high confidence, fix immediately
Suspect A   -> found ONLY by Judge A         -> needs triage
Suspect B   -> found ONLY by Judge B         -> needs triage
Contradiction -> agents DISAGREE on the same thing -> flag for manual decision

Present findings as a structured verdict table (see Output Format).

### Pattern 3: Warning Classification

Judges MUST classify every WARNING into one of two sub-types:

WARNING (real)        -> Causes a bug, data loss, security hole, or incorrect behavior
                        in a realistic production scenario. Fix required.
WARNING (theoretical) -> Requires a contrived scenario, corrupted input, or conditions
                        that cannot arise through normal usage. Report but do NOT block.

How to classify: ask 'Can a normal user, using the tool as intended, trigger this?' If YES -> real. If NO -> theoretical.

Theoretical warnings are reported as INFO in the verdict table. They are NOT fixed, do NOT trigger re-judgment, and do NOT count toward the convergence threshold.

### Pattern 4: Fix and Re-judge

1. If confirmed CRITICALs or real WARNINGs exist -> delegate a Fix Agent (separate delegation)
2. After Fix Agent completes -> re-launch both judges in parallel (same blind protocol, fresh delegates)
3. After 2 fix iterations, if issues remain -> present findings to user and ASK whether to continue
4. If both judges return clean -> JUDGMENT: APPROVED

### Pattern 5: Convergence Threshold

Round 1: Present the verdict table to the user. ASK: 'Fix confirmed issues?' Only fix after user confirms. Then re-judge with full scope.

Round 2+: Only re-judge if there are confirmed CRITICALs. For anything else:
- Real WARNINGs (confirmed): Fix inline, do NOT re-launch judges
- Theoretical WARNINGs: Report as INFO. Do NOT fix, do NOT re-judge
- SUGGESTIONs: Fix inline if trivial. Do NOT re-judge

APPROVED criteria after Round 1: 0 confirmed CRITICALs + 0 confirmed real WARNINGs = APPROVED.

## Sub-Agent Prompt Templates

### Judge Prompt (use for BOTH Judge A and Judge B — identical)

You are an adversarial code reviewer. Your ONLY job is to find problems.

## Target
<describe target: files, feature, architecture, component>

{if compact rules were resolved in Pattern 0, inject the following block}
## Project Standards (auto-resolved)
<paste matching compact rules blocks from the skill registry>

## Review Criteria
- Correctness: Does the code do what it claims? Are there logical errors?
- Edge cases: What inputs or states aren't handled?
- Error handling: Are errors caught, propagated, and logged properly?
- Performance: Any N+1 queries, inefficient loops, unnecessary allocations?
- Security: Any injection risks, exposed secrets, improper auth checks?
- Naming and conventions: Does it follow the project's established patterns?

## Return Format
Return a structured list of findings ONLY. No praise, no approval.

Each finding:
- Severity: CRITICAL | WARNING (real) | WARNING (theoretical) | SUGGESTION
- File: path/to/file.ext (line N if applicable)
- Description: What is wrong and why it matters
- Suggested fix: one-line description of the fix (not code, just intent)

WARNING classification rule: Ask 'Can a normal user, using the tool as intended, trigger this?'
- YES -> WARNING (real)
- NO -> WARNING (theoretical)

Always include at the end: Skill Resolution: {injected|fallback-registry|fallback-path|none} — {details}

If you find NO issues, return:
VERDICT: CLEAN — No issues found.

### Fix Agent Prompt

You are a surgical fix agent. You apply ONLY the confirmed issues listed below.

## Confirmed Issues to Fix
<paste the confirmed findings table from the verdict synthesis>

{if compact rules were resolved in Pattern 0, inject the following block}
## Project Standards (auto-resolved)
<paste matching compact rules blocks from the skill registry>

## Context
- Original review criteria: <paste same criteria used for judges>
- Target: <same target description>

## Instructions
- Fix ONLY the confirmed issues listed above
- Do NOT refactor beyond what is strictly needed to fix each issue
- Do NOT change code that was not flagged
- Scope rule: If you fix a pattern in one file, search for the SAME pattern in ALL other files touched by this change and fix them ALL
- After each fix, note: file changed, line changed, what was done

Return a summary:
## Fixes Applied
- [file:line] — <what was fixed>

Skill Resolution: {injected|fallback-registry|fallback-path|none} — {details}

## Output Format

## Judgment Day — <target>

### Round <N> — Verdict

| Finding | Judge A | Judge B | Severity | Status |
|---------|---------|---------|----------|--------|
| Missing null check | YES | YES | CRITICAL | Confirmed |
| Race condition | YES | NO | WARNING (real) | Suspect (A only) |

Confirmed issues: 1 CRITICAL
Suspect issues: 1 WARNING
Contradictions: none

### Fixes Applied (Round <N>)
- auth.go:42 — Added nil check before dereferencing

### Round <N+1> — Re-judgment
- Judge A: PASS
- Judge B: PASS

---

### JUDGMENT: APPROVED
Both judges pass clean. The target is cleared for merge.

## Blocking Rules (MANDATORY)

1. MUST NOT declare JUDGMENT: APPROVED until: Round 1 judges return CLEAN, OR Round 2 judges confirm 0 CRITICALs + 0 confirmed real WARNINGs
2. MUST NOT run code-modifying actions after fixes until re-judgment completes
3. MUST NOT save a session summary or tell the user 'done' until every JD reaches a terminal state (APPROVED or ESCALATED)
4. After the Fix Agent returns, your IMMEDIATE next action is re-launching judges in parallel for re-judgment
5. When running multiple JDs in parallel, each JD is independent

## Self-Check (before ANY terminal action)

Before pushing, committing, summarizing, or telling the user 'done':

1. List every active JD target
2. For each: is it in state APPROVED or ESCALATED?
3. If ANY JD had fixes applied, did Round 2 run?
4. If Round 2 found issues, did you ASK the user whether to continue?

If ANY answer is 'no' — you skipped a step. Go back and complete it before proceeding.

## Rules

- The orchestrator NEVER reviews code itself — it only launches judges, reads results, and synthesizes
- Judges MUST be launched via harness-native delegation (async) so they run in parallel
- The Fix Agent is a separate delegation — never use one of the judges as the fixer
- If user provides custom review criteria, include them in BOTH judge prompts (identical)
- If target scope is unclear, stop and ask before launching
- After 2 fix iterations, ASK the user before continuing
- Always wait for BOTH judges to complete before synthesizing — never accept a partial verdict
- Suspect findings (only one judge) are reported but NOT automatically fixed
`,
	}
}
