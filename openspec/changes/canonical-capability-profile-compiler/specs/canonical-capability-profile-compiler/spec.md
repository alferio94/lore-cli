# Canonical Capability Profile Compiler Specification

## Purpose

Define a versioned, secret-safe compiler contract that resolves canonical content, user profile/model intent, target capability constraints, and existing ownership into a complete deterministic projection plan before Lore CLI mutates a supported harness.

## Normative Terms

- **Canonical input**: version, `agentpack.Definition` identity, target, requested profile/scope, role overrides, requested capabilities, configuration sources, existing harness-local choices, and current projection/manifest facts.
- **Resolved IR**: versioned result containing compiler/profile identity, resolved role models with provenance, capability decisions, ownership, migrations, ordered projections, hashes, and rollback facts.
- **Mutation**: any profile/config persistence, directory/file write, deletion, backup, manifest/pointer update, or runtime activation.

## Requirements

### Requirement: Versioned Compiler Contract

The compiler **MUST** validate a versioned canonical input and produce a versioned resolved IR. Both schemas **MUST** identify their compatibility version and all behavior-affecting inputs; the resolved IR **MUST NOT** depend on unrecorded ambient state.

#### Scenario: Compile a compatible input
- GIVEN a supported input version and valid canonical content
- WHEN compilation completes
- THEN the resolved IR identifies its schema and compiler versions
- AND every projected decision is derivable from recorded input and provenance

#### Scenario: Reject a malformed input
- GIVEN a compatible version with a missing or invalid required field
- WHEN compilation is requested
- THEN compilation fails with the field path and corrective guidance
- AND no resolved plan or mutation is admitted

#### Scenario: Reject an unsupported schema version
- GIVEN an input or persisted IR with an unsupported compatibility version
- WHEN it is loaded
- THEN processing fails closed with supported-version guidance
- AND no migration or mutation is guessed

### Requirement: Profile and Model Precedence

The compiler **MUST** resolve profile and per-role model values in this descending order: one-shot CLI, project config, global user config, explicit existing harness-local choice, canonical defaults. A role override **MUST** override the selected profile only for that role. Each result **MUST** record source, source location/scope, and overridden lower-priority candidates.

#### Scenario: Resolve competing sources
- GIVEN all five source tiers define a profile or role model
- WHEN compilation resolves the value
- THEN the highest-priority valid value wins
- AND provenance lists the ignored candidates in precedence order

#### Scenario: Apply a role override
- GIVEN a selected profile and a higher-priority model override for one role
- WHEN roles are resolved
- THEN only that role uses the override
- AND all other roles retain profile-derived models

#### Scenario: Preserve an explicit local choice
- GIVEN no CLI, project, or global value and an explicit user-owned local model
- WHEN compilation resolves that role
- THEN the local model wins over canonical defaults
- AND it is identified as harness-local provenance

### Requirement: Explicit Global and Project Profile Scope

Profile persistence **MUST** require an explicit `global` or `project` scope. Project selection **MUST** be bound to a deterministic project identity and take precedence only within that project; global selection **MUST** remain the fallback elsewhere.

#### Scenario: Select a project profile
- GIVEN global and current-project profiles exist
- WHEN compilation occurs inside the matching project identity
- THEN the project profile wins and both sources are explained

#### Scenario: Persist the requested scope
- GIVEN a valid profile and an explicit persistence scope
- WHEN an admitted apply succeeds
- THEN only the requested scope is updated
- AND dry-run, failed compile, or failed apply does not persist it

### Requirement: Closed Capability State Model

Every known or requested capability **MUST** resolve to exactly one state: `supported`, `unsupported`, `staged/inactive`, or `unknown`, with target, reason, evidence source, and required/optional intent. No state **MAY** imply runtime activation unless it is `supported` and the target contract permits activation.

#### Scenario: Resolve a supported matrix entry
- GIVEN a known capability supported by the selected target
- WHEN compilation resolves the matrix
- THEN its state is `supported` with target evidence

#### Scenario: Resolve an unknown identifier
- GIVEN an identifier absent from the compiler registry
- WHEN compilation resolves the request
- THEN its state is `unknown` with spelling/version guidance

#### Scenario: Resolve staged capability data
- GIVEN bounded-review projection data for Pi
- WHEN the matrix is compiled
- THEN its state is `staged/inactive`
- AND it is not reported as runtime available

### Requirement: Validation Before Mutation

The compiler **MUST** validate the complete selected-target plan before mutation. Invalid profiles and requested `unsupported` or `unknown` capabilities **MUST** fail the operation as a whole; Lore CLI **MUST NOT** silently skip them or install a partial plan.

#### Scenario: Reject an unsupported request
- GIVEN a requested capability is unsupported for the target
- WHEN compile/apply is invoked
- THEN the command explains the target limitation and alternatives
- AND no mutation occurs

#### Scenario: Reject an invalid profile
- GIVEN a profile has an unknown role, invalid model, or incomplete required mapping
- WHEN compilation is requested
- THEN all detected validation errors are actionable
- AND no mutation occurs

#### Scenario: Admit a complete plan
- GIVEN every requested capability, ownership decision, model, and migration validates
- WHEN compilation completes
- THEN one complete ordered target plan is admitted
- AND apply receives only that immutable admitted plan

### Requirement: Harness-Specific Capability Truth

The matrix **MUST** preserve target asymmetry. Pi, OpenCode, Codex, and Antigravity **MUST** expose only their validated current contracts. Claude Code **MUST** resolve as explicit unsupported/roadmap state and **MUST NOT** enter projection or apply.

#### Scenario: Compile supported targets independently
- GIVEN equivalent intent for Pi, OpenCode, Codex, and Antigravity
- WHEN each target is compiled
- THEN each plan contains only that target's supported or staged contracts
- AND missing parity is explained rather than emulated

#### Scenario: Reject Claude Code
- GIVEN Claude Code is selected or a Claude-only capability is requested
- WHEN compilation runs
- THEN it fails with an unsupported/roadmap explanation
- AND no adapter, projection, backup, or manifest operation runs

### Requirement: Bounded Review Remains Inactive

Bounded-review artifacts **MUST** remain `staged/inactive`, with `RuntimeActive=false`, and **MUST NOT** create runtime discovery, authority, hook, or activation state. Portable fallback content **MUST** remain a separate selectable capability and **MUST NOT** be represented as bounded-review activation.

#### Scenario: Stage bounded-review artifacts
- GIVEN Pi requests the validated bounded-review projection
- WHEN its plan is compiled and applied
- THEN staged artifacts retain inactive metadata
- AND runtime activation files are absent

#### Scenario: Select portable fallback
- GIVEN bounded review is unavailable or not selected
- WHEN portable fallback is selected
- THEN fallback identity and ownership are distinct
- AND no bounded-review availability is claimed

### Requirement: Projection Ownership

Every projected path **MUST** use exactly one ownership mode: `replace`, `marker-merge`, `additive`, or `conflict`. Foreign content **MUST NOT** be silently overwritten, removed, or adopted.

#### Scenario: Replace compiler-owned content
- GIVEN manifest/hash evidence proves the path is compiler-owned
- WHEN a `replace` projection changes
- THEN replacement is planned with prior and next hashes

#### Scenario: Merge valid managed markers
- GIVEN foreign content surrounds one valid managed marker region
- WHEN `marker-merge` is applied
- THEN only the managed region changes
- AND surrounding bytes are preserved

#### Scenario: Add without overwrite
- GIVEN an `additive` projection targets absent content
- WHEN apply runs
- THEN content is added without changing existing foreign entries

#### Scenario: Conflict on foreign ownership
- GIVEN ownership is ambiguous, markers are malformed, or foreign content occupies a protected path
- WHEN compilation evaluates the projection
- THEN ownership resolves to `conflict` and blocks mutation
- AND explain output identifies the path and remediation

### Requirement: Deterministic Projection Identity

For identical normalized canonical input and relevant existing-state facts, compilation **MUST** produce identical ordered IR, projection bytes, identities, and hashes. Ordering **MUST** be stable. v2 **MUST** preserve behavior and ownership semantics, not universal legacy byte identity; any byte change **MUST** be deterministic and attributable.

#### Scenario: Repeat compilation
- GIVEN identical normalized input and existing-state facts
- WHEN compilation is repeated
- THEN ordered output bytes and hashes are identical

#### Scenario: Normalize unordered input
- GIVEN semantically equal maps or selections in different input orders
- WHEN each is compiled
- THEN canonical ordering and projection identity match

#### Scenario: Explain a compatible byte change
- GIVEN compiler evolution changes bytes without changing behavior or ownership
- WHEN reconciliation is explained
- THEN old/new hashes and the deterministic change reason are reported
- AND the change is not claimed to be universal byte preservation

### Requirement: Complete Dry-Run and Explain Report

Dry-run/explain **MUST** report selected sources and precedence, resolved role models, every capability state/error, target asymmetry, ownership, migrations, ordered projections, current/next hashes, backup boundary, rollback action, and whether mutation is admitted. Dry-run **MUST NOT** mutate state.

#### Scenario: Explain an admitted plan
- GIVEN a valid request
- WHEN dry-run/explain is invoked
- THEN every required report section is present with deterministic ordering
- AND mutation count is zero

#### Scenario: Explain a rejected plan
- GIVEN capability, profile, migration, or ownership errors
- WHEN dry-run/explain runs
- THEN all safely reportable errors identify source and remediation
- AND admitted status is false

#### Scenario: Explain precedence
- GIVEN multiple profile/model candidates
- WHEN explain is requested
- THEN the winner and each shadowed source are shown
- AND no source value is misattributed

### Requirement: Provenance Manifest and Reconciliation

A successful apply **MUST** persist a manifest containing schema/compiler version, canonical pack identity, input/resolved-IR identity, profile ID/version/scope, per-role model provenance, capability IDs/states/reasons, target identity, ownership modes, projection hashes, migrations, and rollback boundary. Reconciliation **MUST** compare these facts before deciding no-op, update, migration, or conflict.

#### Scenario: Record successful provenance
- GIVEN an admitted plan applies successfully
- WHEN the manifest is published
- THEN all required identities and decisions match the applied plan
- AND the manifest hash is reproducible

#### Scenario: Reconcile unchanged state
- GIVEN manifest, owned files, compiler/profile identity, and hashes match
- WHEN reconciliation runs
- THEN the result is a deterministic no-op

#### Scenario: Reconcile drift or migration
- GIVEN owned-state drift or a recognized older manifest
- WHEN reconciliation runs
- THEN it reports update, migration, or conflict before mutation
- AND no ownership fact is inferred from path alone

### Requirement: Atomic Compile and Apply Boundary

Apply **MUST** consume only a fully compiled immutable plan. Existing selected-target backup and rollback boundaries **MUST** remain authoritative: backups precede destructive writes, the manifest/pointer publishes last, and failure restores the prior coherent state. This change **MUST NOT** invent cross-target transaction authority.

#### Scenario: Apply an admitted target plan
- GIVEN a complete admitted plan
- WHEN all writes succeed
- THEN projections are committed in order and manifest/pointer publishes last

#### Scenario: Fail during apply
- GIVEN a write or final publication fails after mutation begins
- WHEN rollback executes
- THEN the existing backup boundary restores prior files and manifest/pointer
- AND the command reports rollback outcome and residual risk

#### Scenario: Fail before apply
- GIVEN compilation or preflight fails
- WHEN the operation ends
- THEN no backup, temporary publish, file write, deletion, or profile persistence occurs

### Requirement: Supported-Target Behavioral Compatibility

For unchanged intent, v2 **MUST** preserve current behavior and ownership semantics for Pi, OpenCode, Codex, and Antigravity. Explicit user-owned local model choices **MUST** be preserved under precedence; higher-priority CLI/project/global choices **MAY** deterministically replace them and **MUST** explain that decision.

#### Scenario: Preserve Pi behavior
- GIVEN an existing valid Pi install and unchanged intent
- WHEN v2 reconciles it
- THEN Pi-native behavior, staged bounded-review status, and ownership boundaries remain equivalent

#### Scenario: Preserve OpenCode behavior
- GIVEN user-owned managed-agent model or variant values and no higher source
- WHEN OpenCode is compiled
- THEN those values and foreign-content safeguards are preserved

#### Scenario: Preserve Codex behavior
- GIVEN valid existing Codex phase-model choices and no higher source
- WHEN Codex is compiled
- THEN role behavior and supported file/MCP ownership remain equivalent

#### Scenario: Preserve Antigravity behavior
- GIVEN a valid Antigravity prompt/skills/profile projection
- WHEN v2 reconciles unchanged intent
- THEN marker-owned prompt content, skills, profile, and selected optional MCP semantics remain equivalent

### Requirement: Forward and Backward Compatibility

The compiler **MUST** support explicitly registered older compatible versions through deterministic migrations. Unsupported major versions and unknown behavior-affecting fields **MUST** fail closed. Namespaced extension fields **MAY** be retained and ignored only when the schema declares them non-behavioral; their treatment **MUST** be deterministic and explained.

#### Scenario: Migrate a registered older version
- GIVEN an older version with a registered lossless or declared migration
- WHEN it is loaded
- THEN migration steps and resulting identity are recorded before apply

#### Scenario: Reject an unknown behavioral field
- GIVEN a profile, input, IR, or manifest contains an unknown non-extension field
- WHEN validation runs
- THEN it fails with field path and supported schema guidance

#### Scenario: Carry a safe extension
- GIVEN a namespaced extension declared non-behavioral
- WHEN a compatible compiler processes it
- THEN the extension is retained as specified without changing resolution
- AND explain identifies that it was not interpreted

### Requirement: Bounded Credential Compatibility

Raw secrets **MUST** remain absent from canonical input snapshots and normalized identity material, resolved IR, profile stores, manifests and provenance, compiler/input/IR/plan/projection display identities and hashes, dry-run/explain output, logs, diagnostics, errors, and all non-sensitive generated files. Existing compatibility adapters **MAY** resolve a non-secret credential reference or slot only after the immutable non-secret plan is sealed and **MAY** materialize the bearer token only in the final protected adapter-managed target file and its rollback/backup copy when current harness behavior requires it.

Sensitive finalization **MUST** be explicit, minimal, target-owned, restrictive-permission preserving, redacted on failure, and excluded from display hashes; it **MUST NOT** mutate plan identity or provenance. Secret-safe structural reconciliation and ownership evidence **MUST** fail closed when ambiguous, and no token-derived display hash **MAY** permit reconstruction or leakage. Failed compilation and dry-run **MUST NOT** resolve or materialize secrets. Failed apply **MUST** use existing rollback and **MUST NOT** leave additional secret-bearing temporary files. This bounded compatibility **MUST NOT** add a credential broker, runtime helper, subprocess, or runtime authority.

#### Scenario: Keep canonical artifacts secret-free
- GIVEN a secret-backed integration is selected through a non-secret credential slot
- WHEN compilation, persistence, or explanation occurs
- THEN snapshots, normalized identity material, IR, profile stores, provenance, identities, display hashes, and non-sensitive files contain no raw secret
- AND diagnostics and errors expose only safe references or redaction

#### Scenario: Materialize an allowed final sensitive file
- GIVEN an immutable admitted plan explicitly marks an adapter-owned sensitive projection
- WHEN apply reaches its final protected write and resolves the approved credential slot
- THEN only the required final target file and protected rollback/backup copy may contain the bearer token
- AND restrictive permissions and target ownership are preserved

#### Scenario: Avoid secret resolution before finalization
- GIVEN dry-run is requested or compilation/preflight fails
- WHEN the operation ends
- THEN no credential slot is resolved and no secret is materialized
- AND no secret-bearing backup, temporary file, or target file is created

#### Scenario: Redact and roll back finalizer failure
- GIVEN sensitive finalization or its protected write fails after apply begins
- WHEN failure handling and rollback run
- THEN errors are redacted and the existing target rollback boundary restores prior coherent state
- AND no additional secret-bearing temporary file remains

#### Scenario: Keep identity independent of token bytes
- GIVEN the same sealed non-secret plan resolves different bearer-token bytes at finalization
- WHEN compiler, input, IR, plan, projection display identities, and display hashes are compared
- THEN those identities and hashes remain identical and reveal nothing about either token
- AND finalization does not mutate manifest provenance or plan identity

#### Scenario: Reject ambiguous sensitive reconciliation
- GIVEN sanitized structure, manifest evidence, markers, or ownership facts for a sensitive target are absent or ambiguous
- WHEN reconciliation evaluates the target
- THEN it resolves to conflict before credential resolution or mutation
- AND no token-derived hash or diagnostic is emitted

### Requirement: Baseline Isolation and Acceptance Integrity

The known unrelated `internal/cli.TestStatusAndDoctorActionsPreserveDiagnosticSemantics` failure (`9` checks versus expected `10`) **MUST NOT** be an acceptance criterion, attributed to this change, or silently fixed within it. New or changed compiler/projection regressions **MUST** still fail acceptance.

#### Scenario: Report the known baseline failure
- GIVEN focused change tests pass and the unchanged full suite has only the recorded baseline failure
- WHEN acceptance evidence is produced
- THEN status distinguishes the baseline failure from change verification

#### Scenario: Detect a change regression
- GIVEN a compiler, projection, ownership, rollback, or security test fails
- WHEN acceptance is evaluated
- THEN the change is rejected regardless of the known baseline failure

## Non-Goals

- Activating bounded review, RDD/review authority, hooks, or runtime discovery.
- Adding Claude Code support or emulating Pi semantics on other harnesses.
- Replacing existing adapters as projection/render backends or replacing `agentpack.Definition` as portable content source.
- Adding a credential broker, secret-management service, runtime helper, subprocess, or new runtime authority; a future broker remains out of scope.
- Universal legacy byte identity, broad projection-format rewrites, server memory/MCP implementation, policy distribution, or local delegation/runtime execution.
- Expanding existing backup/rollback authority into a cross-target transaction.
- Fixing unrelated CLI baseline failures, publishing v2, changing dependencies, or performing Git/remote/PR work.

## Acceptance Evidence

Acceptance **MUST** include runtime-coverable automated evidence for: schema/version fixtures and migrations; complete precedence permutations and provenance; global/project scope isolation; all capability states across five named targets; mutation-spy rejection paths; ownership fixtures with foreign content; bounded-review inactivity and fallback separation; repeatable ordering/bytes/hashes; full secret-safe dry-run/explain snapshots; manifest reconciliation; injected apply failures with rollback; and semantic compatibility fixtures for Pi, OpenCode, Codex, and Antigravity. Credential-compatibility evidence **MUST** prove secret-free canonical artifacts, post-seal resolution only at the final protected write, allowed sensitive target/backup scope, restrictive permissions, token-independent identities/display hashes, fail-closed sanitized reconciliation, redacted finalizer failures, rollback, and absence of residual secret-bearing temporary files. Focused tests **MUST** pass. Full-suite output **MUST** report the known unrelated baseline separately, and any additional failure **MUST** block acceptance.

## Specification Accounting

- Requirements: 16
- Scenarios: 49
## Governance Rebaseline Delta (Non-Product Contract)

### Canonical/Delta Arrangement

The preceding canonical product contract is the exact recovered content of legacy Engram observation 1476 (`sha256:002144fb74921eca22c9e6f6f0e7f770e33ee20d7e497f477b274f52adbae410`). This active OpenSpec document is a hybrid-persistence delivery overlay: the canonical prefix remains unchanged, while this section records only the authorized governance rebaseline. This delta does not add, remove, renumber, weaken, or reinterpret the 16 product requirements or 49 product scenarios above.

### Governance Constraints

| Constraint | Authorized delta |
| --- | --- |
| Artifact persistence | Each remaining phase artifact MUST be stored as identical full English content in Lore and OpenSpec. Both copies MUST be read back and their SHA-256 content digests MUST match before advancement; a failed write, read-back, or mismatch is a hard stop. |
| Authority | Gentle AI remains blocked historical authority. Pi/Lore is the execution and review authority only for W2B, W2C, W3, and W4; this authority migration is not an acceptance event. |
| Delivery target | QA is the sole delivery target for this rebaseline. Production rollout, publication, and production acceptance are excluded. |
| Accepted lineage | W1a, W1b, W1c, and W2A accepting gates and receipts are immutable historical acceptance. They MUST NOT be replaced, relabeled, or reaccepted by this rebaseline. |
| W2B lineage | Candidate `sha256:ddd84029cfdc526cbebc40fd5b07cc7b84f096c26c402142e832e9dbb6e3e58b` remains non-accepting and unaccepted. W2C, W3, and W4 remain blocked until a fresh Pi/Lore W2B gate accepts the complete W2B slice. |

### Governance Accounting

- Product requirements: 16 (unchanged from legacy observation 1476)
- Product scenarios: 49 (unchanged from legacy observation 1476)
- Governance constraints: 5 (delivery controls only; not product requirements or scenarios)
# Normative Product Amendment: W2C Manifest-v3 Rollback-Boundary Provenance

## Status and Delta Strategy

This amendment is authorized only to correct W2C defects D1–D3 after failed verification `dg-fc07240d` / `verify-report-W2C` (`sha256:2d2c793c75e50771b12b349e143cf655c661b1ef439b953a665f9dcfb43e5f18`) and needs-input `dg-a3888401` / `apply-correction-W2C-D1-D3-report` (`sha256:bbb89981a05264bda7ae51fad3f511fb1c7822d2e636a5fd8bcc872410581a3a`). The preserved pre-amendment active spec is `spec-pre-amendment-W2C.md`; legacy Engram 1476 remains immutable. This is a product-contract delta, not acceptance and not an implementation authorization.

It augments R8 (Deterministic Projection Identity), R10 (Provenance Manifest and Reconciliation), R13 (Forward and Backward Compatibility), and R14 (Bounded Credential Compatibility). It adds no requirement: the active requirement count remains **16**. It adds the ten traceable scenarios below: active product-scenario count is **59** (legacy 49 + A1–A10); the legacy 49 remain unchanged.

## Normative Manifest-v3 Contract (R10)

A v3 manifest **MUST** include required object `rollback_boundary` immediately after `projections`, with canonical member order and shape:

```json
"rollback_boundary":{"id":"rb-v1","kind":"selected-target-backup-v1","target":"pi","resources":[{"path":"skills/a.md","ownership":"replace","hash":"aaa"}],"finalizations":[]}
```

`id`, `kind`, and `target` are required strings; `resources` and `finalizations` are required arrays, including `[]`. `id` is a non-secret logical boundary identifier matching `^[a-z0-9][a-z0-9._:-]{0,127}$`; it is a compiler-recorded, deterministic sealed-plan fact, never a backup location, handle, timestamp, token, or random value. `kind` is exactly `selected-target-backup-v1`. `target` exactly equals root `target`.

`resources` contains complete `Projection` objects (`path`, `ownership`, `hash`) and `finalizations` contains complete `FinalizationReference` objects (`path`, `finalizer_id`, `provider`, `slot`), using the existing field types and validation. Each list is lexically ascending by `path`, has unique paths, and is byte-for-byte equivalent after canonical JSON normalization to root `projections` and `finalizations`, respectively. Thus every projected resource, its ownership and hash, and every finalization reference are bound to the boundary; finalization paths **MUST** be projected-resource paths. No new ownership value is introduced.

The canonical root order is existing fields through `projections`, then `rollback_boundary`, then `role_models`, `capabilities`, `reconciliation`, `finalizations`, `migrations`, and `extensions`. Object order is `id`, `kind`, `target`, `resources`, `finalizations`. Canonical manifest identity and all manifest/decision hashes **MUST** cover the complete canonical `rollback_boundary`; an accepted one-field `rollback_boundary.id` change therefore produces a different identity/hash and, absent higher-priority conflict or migration, `update`.

Missing fields return zero manifest / zero decision and `required_field` at `rollback_boundary` or its member path. Wrong JSON type, invalid `id`, unsupported `kind`, target mismatch, unsorted/duplicate paths, unmatched resource/finalization record, or finalization outside resources return zero output and `invalid_field` at the precise member/index path. Unknown or duplicate JSON members retain existing `unknown_field`/`duplicate_field` behavior. Errors **MUST NOT** echo rejected values.

This field records rollback provenance only: codec, migration, identity, and reconciliation remain pure and perform no I/O. It neither creates backups nor runs transaction/runtime rollback; W3 owns those effects.

## Reconciliation and Migration (R10, R13)

Exact boundary equality participates in deterministic no-op. A valid boundary difference yields `update` unless either manifest has ownership `conflict` (then `conflict`) or a registered lossless migration applies (then `migration`). A v3 manifest lacking or unable to validate this field is rejected before a decision. v1 and v2 lack reconstructable boundary provenance and **MUST NOT** migrate to v3: `Migrate` returns zero manifest and `invalid_field` at `rollback_boundary`; `Reconcile` returns zero decision and that typed rejection (a preflight conflict), never noop/update/migration. No default boundary, inferred resource list, or guessed finalization is permitted.

## Purity and Secret Safety (R8, R14)

Encode/normalization **MUST NOT** mutate any caller-owned manifest, slice, map, `json.RawMessage`, or nested extension value, whether success or failure; output and decoded collections **MUST NOT** alias input bytes or caller-owned mutable storage. Defensive-copy accessors/returns apply to projections, role models, capabilities, finalizations, migrations, rollback-boundary lists, extensions, and raw extension values.

Extension validation **MUST** recursively traverse every object key, array element, and scalar. Paths use `extensions["<key>"]` followed by `.member` and `[index]`; it rejects `invalid_field` at that exact nested path for a key containing (case-insensitively) `secret`, `token`, `password`, `bearer`, `credential`, `api_key`, `apikey`, or `authorization`, or a string scalar matching `(?i)^(bearer|basic)[ \t]+[^\r\n]+$`. The error is a fixed redacted message and contains no key value or scalar payload. Encode, Decode, Migrate, and Reconcile return zero admitted output/decision on such input; no identity/hash or diagnostic may reveal it.

## Amendment Scenarios

#### Scenario A1: Canonically encode and decode boundary provenance (R10)
- GIVEN a valid v3 manifest and matching non-secret boundary copies
- WHEN it is encoded then decoded
- THEN required fields and canonical order are preserved and identity/hash covers them

#### Scenario A2: Reject a missing boundary (R10)
- GIVEN v3 JSON without `rollback_boundary`
- WHEN Decode or Reconcile processes it
- THEN it returns zero output with `required_field` at `rollback_boundary`

#### Scenario A3: Reject invalid boundary members (R10)
- GIVEN a boundary with a wrong type, invalid id, kind, target, order, duplicate path, or unmatched record
- WHEN it is encoded or decoded
- THEN it returns zero output with `invalid_field` at the precise member/index path

#### Scenario A4: Bind resources, ownership, and hashes (R10)
- GIVEN boundary resources differ from root projections in one path, ownership, or hash
- WHEN the manifest is validated
- THEN it is rejected before reconciliation or mutation

#### Scenario A5: Bind finalization references (R10)
- GIVEN boundary finalizations differ from root finalizations or name an unprojected path
- WHEN the manifest is validated
- THEN it is rejected with a redacted precise path

#### Scenario A6: Detect one-field boundary identity mutation (R8, R10)
- GIVEN two valid manifests differing only in accepted `rollback_boundary.id`
- WHEN their canonical identities are compared and reconciled
- THEN hashes differ and outcome is deterministic `update` absent conflict or migration

#### Scenario A7: Reject unreconstructable v1/v2 provenance (R13)
- GIVEN a v1 or v2 manifest without rollback-boundary facts
- WHEN Migrate or Reconcile is requested
- THEN it returns zero output at `rollback_boundary` and never guesses a boundary

#### Scenario A8: Decide no-op, update, and conflict deterministically (R10)
- GIVEN matching boundaries, a valid boundary difference, and a conflicting ownership fixture
- WHEN reconciliation runs
- THEN outcomes are respectively noop, update, and conflict without I/O

#### Scenario A9: Reject recursive secret-shaped extensions without disclosure (R14)
- GIVEN an extension nesting a bearer- or basic-shaped scalar or prohibited key in an object or array
- WHEN Encode, Decode, Migrate, or Reconcile processes it
- THEN it returns redacted `invalid_field`, zero admitted output, and no secret-derived identity/hash

#### Scenario A10: Preserve caller state during canonicalization (R8)
- GIVEN a valid unsorted manifest with aliased slices, maps, and raw extension values
- WHEN Encode is called repeatedly
- THEN canonical bytes match and the caller-owned object remains structurally and byte-identical

# Normative Product Amendment: W3.2 Cross-Process Profile-Store Commit Authority

## Status, Lineage, and Scope

Option B is authorized after failed verification `dg-d477c679` / `verify-report-W3.2` (`sha256:0a9fe23da74a44f118ef8bd6479f6b767dd1a2acbc73984b8203f3488528e936`) and needs-input `dg-e0175564` / `apply-correction-W3.2-C1-report` (`sha256:993c00623552eb5609af643265c2061353eb7faf5ac2fd666804a3c7adee32bd`). The preserved pre-amendment active spec is `spec-pre-amendment-W3.2-C1.md` (`sha256:c916bb2fa035df84375bdc36a8654d8ad86b68cf62257d10beed6ec2db821110`). Legacy Engram 1476 and the W2C pre-amendment snapshot remain immutable.

This amendment augments R3 (Explicit Global and Project Profile Scope), R8 (Deterministic Projection Identity), R12 (Atomic Compile and Apply Boundary), and R14 (Bounded Credential Compatibility). It adds no requirement: the active requirement count remains **16**. It adds scenarios B1–B18: the active product-scenario count is **77** (prior active 59 + 18). Prior scenarios remain unchanged. This is a W3.2 profile-store correction contract and QA-only planning authority, not acceptance or source/test authorization. W3.3 and W4 remain blocked.

## Normative Observable Contract

### Authority scope and supported targets

Every mutating profile-store completion **MUST** obtain exclusive cross-process writer authority scoped to the canonical profile-state path on `darwin`, `linux`, and `windows`. Equivalent lexical paths, symlinked existing ancestors, and platform case/volume aliases **MUST** converge on one authority scope. For an absent state file, identity is the canonical existing parent plus final leaf; the state or authority leaf **MUST NOT** be followed through a symlink, hard-link alias, junction, mount-point substitution, or reparse point. Invalid or unsafe path resolution **MUST** fail before authority acquisition or persistence.

Authority applies only to the W3.2 profile-state commit. It **MUST NOT** serialize unrelated state paths, create cross-target transaction authority, extend the selected-target rollback boundary, or perform W3.3 target backup, target writes, finalization, manifest publication, or secret resolution.

### Acquisition and typed outcomes

A completion **MUST** perform all input and boundary validation that requires no current-state mutation before attempting authority. Only a valid `success` completion may acquire it. The CLI wait budget **MUST** be 5 seconds measured with a monotonic clock; retries **MUST** stop at the deadline, use a poll/backoff interval no greater than 100 ms, and return without entering the critical section. A caller-selected zero-wait attempt **MUST** return busy immediately.

Typed errors **MUST** expose stable `Code()` and `Path()` values and a fixed redacted message:

| Condition | Code | Exact logical path |
| --- | --- | --- |
| Live owner blocks a zero-wait attempt | `profile_state_busy` | `profile_store.authority` |
| Positive wait budget expires before ownership | `profile_state_timeout` | `profile_store.authority` |
| Stale incompatible global-profile update | `profile_state_conflict` | `global_profile` |
| Stale incompatible project identity record | `profile_state_conflict` | `projects[].project_id` |
| Stale incompatible project-profile update | `profile_state_conflict` | `projects[].profile_id` |
| Unsafe/canonicalization failure | `invalid_profile_state` | `profile_store.path` |
| Commit/durability I/O failure | `profile_state_io` | `profile_store.commit` |
| Failed restoration of prior state | `profile_state_io` | `profile_store.rollback` |

Actual filesystem paths, project roots, profile values, lock-owner metadata, and rejected payloads **MUST NOT** appear in error text. Busy and timeout are not conflict and **MUST NOT** be reported as successful or partially committed.

### Conditional commit under authority

Authority **MUST** remain continuously held from the authoritative re-read through semantic rebase, stale/conflict detection, full-state validation, same-directory temporary write, file durability flush, atomic replacement, directory durability flush where supported/applicable, rollback of a failed commit, and temporary/authority cleanup. No observer may receive success before replacement and required durability steps complete.

The writer **MUST** re-read current state while authoritative and apply only the prepared semantic delta; it **MUST NOT** publish the prepared full snapshot blindly. Changes to distinct project keys, or one global and one project key, are non-conflicting and **MUST** both persist regardless of process overlap. If a touched key changed from the prepared baseline to the same desired value, completion is an idempotent success/no-op. If it changed to a different value, the first committed value remains and the later completion **MUST** return the exact typed conflict path above without mutation. Unchanged keys **MUST** be preserved byte-semantically and canonical output ordering remains deterministic.

### Crash, security, residue, and rollback

Process crash, forced exit, or handle close **MUST** release authority automatically, or a contender **MUST** recover only from operating-system proof that no live owner holds it. Timestamp age, PID text, hostname text, or metadata alone **MUST NOT** prove staleness. A process **MUST NOT** delete, replace, chmod, or claim another live writer's authority artifact. Residue from a dead owner **MAY** be cleaned only while exclusive ownership is proven; residue alone **MUST NOT** block indefinitely.

State directories/files and any authority artifact **MUST** retain least-privilege protection: `0700` directory and `0600` file semantics on POSIX, and current-user-only equivalent ACL access on Windows. Authority creation and cleanup **MUST** resist symlink/reparse swaps and path traversal. Optional authority metadata **MUST** be minimal and contain no credentials, profile values, project roots, canonical input, IR, bearer material, or other secrets.

Dry-run, `reconcile_rejected`, `apply_failed`, `finalization_failed`, `manifest_failed`, malformed facts, unsafe paths, and other rejected preconditions **MUST NOT** acquire writer authority, create authority/temp artifacts, or persist profile state. On commit failure before replacement, prior bytes remain unchanged. On failure after replacement but before durable success, restoration of the prior coherent state **MUST** occur while authority remains held; no temporary file may remain. A failed restoration reports `profile_state_io` at `profile_store.rollback` and explicit residual risk. Retrying an already committed identical intent **MUST** be a no-op success.

## Implementation Choice Boundary

The implementation **MAY** use a kernel-backed file lock, an exclusively held lock artifact, or another standard-library/platform adapter, and **MAY** vary the atomic-replacement/durability primitive by OS. It **MAY NOT** weaken canonical path scoping, bounded timing, typed outcomes, semantic merge/conflict rules, live-owner safety, permissions, secret safety, rollback, or cleanup. A lock filename, metadata schema, polling mechanism, and OS syscall are design choices, not product contract, provided all observable requirements above hold without a new daemon, subprocess, server, dependency, credential broker, or cross-target coordinator.

## Amendment Scenarios

#### Scenario B1: Serialize canonical aliases on every supported OS (R3, R12)
- GIVEN two independent CLI processes address one state file through equivalent canonical aliases on darwin, linux, or windows
- WHEN both valid success completions reach persistence
- THEN they use one path-scoped exclusive writer authority and cannot overlap the commit section

#### Scenario B2: Hold authority across the complete conditional commit (R12)
- GIVEN a valid success completion acquires writer authority
- WHEN it re-reads, rebases, validates, writes, flushes, replaces, and cleans up
- THEN authority remains continuously held until durable success or completed rollback

#### Scenario B3: Return typed busy without waiting (R3)
- GIVEN a live writer owns `profile_store.authority` and another caller selects zero wait
- WHEN the second caller attempts completion
- THEN it returns `profile_state_busy` at `profile_store.authority` without mutation

#### Scenario B4: Bound positive lock waiting (R3)
- GIVEN a live writer remains authoritative beyond the 5-second CLI budget
- WHEN another CLI process waits with retries no greater than 100 ms
- THEN it stops at the monotonic deadline with `profile_state_timeout` at `profile_store.authority`

#### Scenario B5: Preserve distinct concurrent project updates (R3, R12)
- GIVEN alpha and beta processes prepare from the same baseline and update different project keys
- WHEN both success completions finish in either order
- THEN both ProjectID/profile records persist and neither successful update is lost

#### Scenario B6: Preserve concurrent global and project updates (R3, R12)
- GIVEN one process changes `global_profile` and another changes one `projects[].profile_id` from the same baseline
- WHEN both success completions finish
- THEN both changes persist and unrelated records remain unchanged

#### Scenario B7: Reject a stale incompatible same-key completion (R3, R8)
- GIVEN two processes prepare the same profile key and request different values
- WHEN one commits before the other revalidates under authority
- THEN the first value remains and the later returns `profile_state_conflict` at that key's exact logical path

#### Scenario B8: Accept an idempotent same-key retry (R3, R8)
- GIVEN a stale completion's touched key already equals its desired value
- WHEN it re-reads under authority
- THEN it returns success as a no-op and canonical state bytes remain unchanged

#### Scenario B9: Rebase across unrelated stale changes (R3, R12)
- GIVEN a prepared completion is stale only because unrelated keys changed
- WHEN it acquires authority and validates the current state
- THEN it rebases its semantic delta and preserves every unrelated committed change

#### Scenario B10: Release authority after process death (R12)
- GIVEN a writer crashes or exits while authoritative
- WHEN another process attempts the same canonical state path
- THEN operating-system ownership release or proven dead-owner recovery permits bounded safe progress without guessed staleness

#### Scenario B11: Protect a live writer from lock deletion (R12)
- GIVEN authority metadata appears old but its owner is still live
- WHEN a contender inspects the authority artifact
- THEN it neither deletes nor replaces the artifact and returns busy or timeout according to its wait policy

#### Scenario B12: Reject symlink, reparse, or traversal substitution (R12, R14)
- GIVEN the state/authority leaf or canonicalization path is swapped through unsafe indirection
- WHEN completion validates the path
- THEN it returns `invalid_profile_state` at `profile_store.path` before authority or persistence

#### Scenario B13: Enforce restrictive authority permissions and cleanup (R12, R14)
- GIVEN an implementation creates an authority artifact on a supported OS
- WHEN completion succeeds, fails, or releases normally
- THEN least-privilege permissions apply and no owned temporary or authority residue remains

#### Scenario B14: Keep non-success boundaries lock-free (R3, R12)
- GIVEN dry-run, reconcile rejection, apply failure, finalization failure, or manifest failure
- WHEN completion receives that boundary
- THEN it acquires no authority, creates no artifact, and preserves profile-state bytes

#### Scenario B15: Keep rejected success input lock-free (R3, R12)
- GIVEN malformed facts, an invalid profile, or an unsafe store path
- WHEN success completion is requested
- THEN validation rejects it before authority acquisition and no state or artifact is created

#### Scenario B16: Keep authority diagnostics secret-free (R14)
- GIVEN contention, timeout, conflict, crash recovery, or I/O failure includes sensitive process context
- WHEN an error or optional authority record is produced
- THEN only fixed codes/paths and non-secret minimum metadata are exposed with no payload or host path

#### Scenario B17: Roll back a failed durable commit (R12)
- GIVEN replacement starts but a write, replacement, or required durability step fails
- WHEN failure handling runs while authority is held
- THEN prior coherent bytes are restored, temporary residue is removed, and the exact commit or rollback I/O path is reported

#### Scenario B18: Preserve idempotence after ambiguous retry (R3, R12)
- GIVEN a caller retries identical intent after losing its response to a completed durable commit
- WHEN it revalidates current state under authority
- THEN it returns no-op success without duplicate records, byte drift, or rollback-boundary expansion

## Traceability and Acceptance Delta

| Contract area | Requirements | Scenarios | Required QA evidence |
| --- | --- | --- | --- |
| Canonical cross-process exclusivity and bounded acquisition | R3, R12 | B1–B4 | Independent-process probes on darwin, linux, windows; zero-wait and monotonic-timeout assertions |
| Semantic rebase, no lost update, conflict, idempotence | R3, R8, R12 | B5–B9, B18 | Barrier-controlled distinct-key, global/project, same-key, and lost-response fixtures |
| Crash/live-owner safety | R12 | B10–B11 | Forced process exit and live-owner contention probes; no timestamp/PID-only reclamation |
| Path, permission, cleanup, secret safety | R12, R14 | B12–B16 | Symlink/reparse/path-alias probes, POSIX mode/Windows ACL checks, residue and redaction inspection |
| Durable atomicity and rollback | R12 | B2, B17 | Injected failures before/after replacement and durability; prior-byte and no-residue proof |

Acceptance **MUST** preserve the failed W3.2 receipt and needs-input lineage, demonstrate the B1–B18 matrix on all supported OS targets or equivalent CI runners, and keep the known unrelated CLI baseline isolated. This amendment does not accept W3.2, alter the immutable 463/650 candidate ledger, prescribe a correction budget, or unlock W3.3/W4. A bounded design amendment is required before tasks or apply correction.
# Normative Product Amendment: W3.3/W4 Lore Server Contract Gate

## Status, Lineage, and Scope

This bounded amendment follows completed alignment delegation `dg-b4d8ba50` and `server_data.md` evidence preserved by `server-contract-alignment.md` (`sha256:b592b962c3856ccff006bad96456aa241a36cb9dbdc3bea2a2c87542d11724b7`, Lore memory `a814f9cd-c7c4-4931-9c7f-b5fdf55af5f0`). The handoff records Lore Server merge `47d9c9b` as the persistence/MCP authority. The pre-amendment active spec is `spec-pre-amendment-W3.3-W4.md` (`sha256:529f22527880fabcd37ecc4be4b3a4bf6dc051cf9cfe0c4c9b43558be56966d8`). Earlier snapshots, hashes, amendments, failed receipts, B13 evidence, and server handoff evidence remain immutable.

This amendment augments R3, R6, R9, R12, and R14. It adds no requirement: the active count remains **16**. It adds C1–C10, making the active scenario count **87** (prior 77 + 10). It is a planning gate only: it does not accept or unlock W3.3/W4, alter W3.2/B13 lineage, authorize source/test/Git changes, change Lore Server, or add HTTP repository-aware parity.

## Normative Authority Boundary

Lore Server **MUST** remain sole authority for project UUID/key resolution; repository registration, project binding, and authorization; project/repository pair filtering; compact stored summaries/previews and requested metadata projection; activity grouping, budgets, and ordering; stable errors and audit; full scoped memory get; and pagination semantics. The CLI **MUST NOT** reproduce, infer, or claim those behaviors.

Lore CLI **MUST** remain sole authority only for local compiler `ProjectID` and profile state, selected-harness MCP endpoint/auth rendering, the installation transaction, and portable agent guidance. Local `ProjectID` **MUST NOT** be interpreted, converted, or sent as a server project UUID, project key, or `repository_id`.

The CLI **MUST NOT** infer repository identity from filesystem paths, Git remotes, URLs, titles, metadata, summaries, previews, content, or other text. It **MUST NOT** add a local repository registry, binding model, access-control decision, audit authority, summary generator, discovery full-body path, query-text or semantic-relevance claim for `lore_memory_search`, or cursor/resumable/definite-page assumption.

## W3.3 and W4 Gate

W3.3 **MUST** treat endpoint and token configuration only as an adapter-owned target projection from the sealed plan. Lore Server **MUST** remain external to backup, mutation, finalization coordination, manifest publication, rollback, and profile-store authority; W3.3 **MUST NOT** call server repository or memory APIs to perform installation.

W4 guidance **MUST** direct agents to bounded `lore_project_activity`, then `lore_project_context` or filter-driven `lore_memory_search`, then `lore_memory_get` for full content using the same project scope and, when supplied, the same explicit bound repository UUID. Discovery **MUST** remain compact; metadata is projected only when requested. Guidance **MUST NOT** promise generated summaries, relevance search, full discovery bodies, activity ordering beyond the server contract, or resumable pagination.

## Cross-Repository Scenarios

#### Scenario C1: Keep local and server project identities distinct (R3)
- GIVEN local profile state contains a compiler `ProjectID` and MCP guidance requires a server project identity
- WHEN W3.3 or W4 renders the selected target
- THEN it keeps the values distinct and never converts or substitutes one for the other

#### Scenario C2: Require an explicit bound repository UUID (R3, R6)
- GIVEN optional repository-scoped discovery is selected
- WHEN the CLI renders configuration or guidance
- THEN it accepts only the explicit server-bound repository UUID and infers none from paths, remotes, URLs, or text

#### Scenario C3: Preserve server repository and authorization authority (R6, R12)
- GIVEN repository registration, binding, pair filtering, authorization, stable errors, or audit is required
- WHEN the W3.3/W4 gate is evaluated
- THEN those behaviors remain server-owned and no local model or HTTP parity is introduced

#### Scenario C4: Project endpoint and token configuration only (R12, R14)
- GIVEN a sealed plan contains selected-harness MCP endpoint and credential-slot facts
- WHEN W3.3 installs that harness
- THEN it performs only the target-owned projection and does not make Lore Server a transaction participant

#### Scenario C5: Keep installation independent of memory APIs (R12)
- GIVEN W3.3 performs backup, writes, finalization, manifest publication, or rollback
- WHEN the installation transaction runs
- THEN no repository or memory API call coordinates, authorizes, or persists that transaction

#### Scenario C6: Keep discovery compact and server-authored (R6, R9)
- GIVEN activity, context, or filtered search returns summaries, previews, or projected metadata
- WHEN W4 guidance presents discovery results
- THEN it exposes no full body, generates no summary, and attributes projection and budgets to Lore Server

#### Scenario C7: Retrieve a full body with identical scope (R6, R9)
- GIVEN compact discovery identifies a memory under one project and optional repository UUID
- WHEN an agent needs the full body
- THEN guidance requires `lore_memory_get` with the same project scope and the same repository UUID when supplied

#### Scenario C8: Preserve server activity and error semantics (R6, R9)
- GIVEN Lore Server controls activity grouping/order/budgets, metadata projection, stable errors, and audit
- WHEN CLI explain output or agent guidance describes them
- THEN it reports the boundary without locally reordering, emulating, or claiming enforcement

#### Scenario C9: Describe search as filter-driven only (R6, R9)
- GIVEN `lore_memory_search` supports type, scope, limit, and optional repository filtering without query text
- WHEN W4 guidance describes search
- THEN it makes no lexical, semantic, fuzzy, relevance, or query-text claim

#### Scenario C10: Avoid pagination guarantees (R6, R9)
- GIVEN a compact search response includes bounded pagination metadata without a resumable cursor guarantee
- WHEN CLI or agent guidance presents continuation behavior
- THEN it promises neither cursor resumption nor definite page boundaries

## Acceptance Delta

Before W3.3 or W4 acceptance, QA evidence **MUST** cover C1–C10 through projection/explain/guidance fixtures without server mutation. The gate **MUST** preserve all earlier acceptance and failure lineage and must not be treated as W3.2/B13 verification or acceptance. A bounded design amendment is required next.

# Normative Product Amendment: W3.3 B-C1 Selected-Target Mutation Authority

## Status and Scope

C1-A is authorized to correct W3.3-B defect B-C1 after failed verification `verify-report-W3.3-B`. This amendment augments R12 and R14 without changing the 16 accepted requirements or prior 87 scenarios. It adds C11–C20; active scenario count is **97**. It is specification authority only, not acceptance or implementation authority.

## Normative Observable Contract

Each mutating installation transaction **MUST** obtain one authority scoped to the canonical selected-target root. Canonical aliases of that root **MUST** share authority; unrelated roots **MUST** remain independent. Authority **MUST** include process-local guarding and cross-process, OS-backed exclusion. W3.2 profile-store authority remains separate and **MUST NOT** be called, imported, or reused to serialize W3.3 target writes.

Authority **MUST** be acquired before journal creation or backup and held continuously through writes, Commit or Rollback, required durability, owned residue cleanup, and release. A zero-wait contender **MUST** return fixed `target_authority_busy` at `selected_target.authority`. A waiting contender **MUST** stop within five seconds measured monotonically and return fixed `target_authority_timeout` at that path. Polling **MUST** follow the design's safe interval, never exceeding 100 ms.

Owner death **MUST** be established only by OS ownership semantics, never timestamp, PID, hostname, or metadata. After OS-proven release, the next owner **MUST**, while authoritative, recover any durable orphan journal to its original coherent selected-target state before admitting new mutation. Authority **MUST** remain held through recovery, restoration, durability, and cleanup.

Failed restoration **MUST** report a redacted restoration failure and explicit residual risk, preserve recovery evidence, and leave that root blocked from mutation until recovery succeeds. Busy, timeout, recovery, rollback, and cleanup diagnostics **MUST NOT** disclose actual paths, owner metadata, journal content, credentials, or rejected payloads.

## Amendment Scenarios

#### Scenario C11: Reject same-process overlap (R12)
- GIVEN two transactions in one process select the same canonical target root
- WHEN one holds authority
- THEN the other cannot create a journal, back up, or mutate that root

#### Scenario C12: Reject cross-process overlap (R12)
- GIVEN two processes select canonical aliases of one target root
- WHEN both attempt mutation
- THEN OS-backed exclusion admits only one transaction at a time

#### Scenario C13: Bound busy and timeout outcomes (R12)
- GIVEN a live owner remains authoritative
- WHEN zero-wait and waiting contenders run
- THEN they return respectively fixed busy and five-second timeout outcomes with polling at most 100 ms apart

#### Scenario C14: Recover after owner death (R12)
- GIVEN OS semantics prove owner death and a durable orphan journal exists
- WHEN the next owner acquires authority
- THEN it restores the original coherent state and cleans residue before new mutation

#### Scenario C15: Fail closed when orphan recovery fails (R12, R14)
- GIVEN orphan restoration or durability fails
- WHEN recovery runs under authority
- THEN residual risk is reported, recovery evidence remains, and that root admits no mutation

#### Scenario C16: Hold authority through successful completion (R12)
- GIVEN an admitted transaction owns authority
- WHEN writes, Commit, durability, and cleanup succeed
- THEN no contender enters before cleanup completes and authority releases

#### Scenario C17: Hold authority through rollback (R12)
- GIVEN a transaction fails after backup or mutation begins
- WHEN Rollback restores and durably cleans the target
- THEN authority remains held; failed restoration reports residual risk and blocks that root

#### Scenario C18: Redact authority failures (R14)
- GIVEN contention, timeout, owner death, recovery, or rollback includes sensitive context
- WHEN an outcome is exposed
- THEN busy/timeout use their fixed code and logical path and all sensitive context is absent

#### Scenario C19: Keep unrelated roots independent (R12)
- GIVEN transactions select two unrelated canonical target roots
- WHEN both mutate concurrently
- THEN neither authority blocks the other

#### Scenario C20: Keep profile-store authority separate (R12)
- GIVEN W3.3 serializes selected-target writes
- WHEN authority is acquired and held
- THEN no profile-store authority call, import, or ownership scope participates
# Normative Product Amendment: W3.3-C Hosted MCP Finalizer (Option 1)

## Status, Lineage, and Scope

W3.3-C option 1 augments R12 and R14 after preserved preflight. The preceding specification remains unchanged, including all accepted W1/W2/W3.1/W3.2/W3.3-A/B/C1-A authority. No requirement is added; C21–C30 make **107** active scenarios. This is specification authority only.

## Observable Contract

The finalizer **MUST** receive the admitted `TransactionPlan` and original `TransactionInput`. Before resolution or mutation, it **MUST** re-seal the original input through accepted A authority and require resulting report/intent equivalence with the admitted plan. An external endpoint, permit, or unbound intent **MUST NOT** be accepted. The final `/v1/mcp` URL **MUST** be a sealed non-secret admitted-intent fact.

Each equivalence-admitted attempt **MUST** call the credential resolver exactly once and make zero Lore Server, API, repository, storage, or memory calls. Order **MUST** be re-seal, equivalence, resolve, render, protected write, then B completion. No local mutation **MAY** occur outside accepted protected B authority. Success **MUST** discard completed journal state; invalid intent or finalizer-stage failure **MUST** fail fast and roll back through active B authority. Rollback failure **MUST** retain existing residual-risk behavior.

| Condition | Code | Path |
| --- | --- | --- |
| Report/intent mismatch or forbidden external fact | `hosted_mcp_invalid_intent` | `hosted_mcp.intent` |
| Credential resolution failure | `hosted_mcp_resolve_failed` | `hosted_mcp.credential` |
| Rendering failure | `hosted_mcp_render_failed` | `hosted_mcp.config` |
| Protected-write failure | `hosted_mcp_write_failed` | `hosted_mcp.write` |
| Rollback failure | `transaction_residual_risk` | `transaction.rollback` |

Messages and metadata **MUST** be fixed and redacted, disclosing no endpoint detail beyond the sealed final URL, credential/token, project/repository-sensitive value, path, rendered configuration, journal byte, or server internal.

## Deterministic Scenarios

#### Scenario C21: Complete one admitted finalization
- GIVEN equivalent re-sealed intent and active B authority
- WHEN finalization succeeds
- THEN normative order uses one resolver and zero prohibited calls
- AND protected write completes before B journal discard

#### Scenario C22: Reject mismatched or external intent
- GIVEN re-sealing differs or an endpoint, permit, or intent is externally supplied
- WHEN finalization starts
- THEN it returns `hosted_mcp_invalid_intent` at `hosted_mcp.intent`
- AND resolver, render, write, and prohibited-call counts are zero before B rollback

#### Scenario C23: Bind the final endpoint fact
- GIVEN original input admits hosted MCP
- WHEN A re-seals it
- THEN the final `/v1/mcp` URL is the sole permitted endpoint fact

#### Scenario C24: Fail credential resolution
- GIVEN equivalent intent and a failing resolver
- WHEN finalization runs
- THEN exactly one resolver call returns `hosted_mcp_resolve_failed` at `hosted_mcp.credential`
- AND render/write counts are zero before B rollback

#### Scenario C25: Fail rendering
- GIVEN resolution succeeds once and rendering fails
- WHEN finalization runs
- THEN it returns `hosted_mcp_render_failed` at `hosted_mcp.config`
- AND protected-write count is zero before B rollback

#### Scenario C26: Fail the protected write
- GIVEN resolution and rendering succeed
- WHEN the accepted B protected write fails
- THEN it returns `hosted_mcp_write_failed` at `hosted_mcp.write`
- AND B rollback runs without any unprotected mutation

#### Scenario C27: Preserve residual-risk behavior
- GIVEN any rollback-triggering finalizer failure and failed B rollback
- WHEN failure is reported
- THEN outcome is `transaction_residual_risk` at `transaction.rollback`

#### Scenario C28: Redact every observable outcome
- GIVEN success or any failure includes sensitive context
- WHEN messages or metadata are observed
- THEN only permitted fixed facts appear and every forbidden value is absent

#### Scenario C29: Fail fast with zero unexpected calls
- GIVEN failure at any ordered stage
- WHEN call traces and local state are inspected
- THEN no later stage or prohibited call occurred and no unprotected mutation exists

#### Scenario C30: Preserve B cleanup semantics
- GIVEN success, invalid intent, or a finalizer-stage failure
- WHEN completion ends
- THEN B discards or rolls back journal state with deterministic residue

## Exclusions

Excluded: profile completion D, failure-matrix/static gate E, W4, server API invention, local Lore binding/retrieval/search/cursor behavior, and accepted A/B contract changes.

# Normative Product Correction: W3.3-D Completion Authority (Option 1)

## Status, Supersession, and Scope

The user-selected W3.3-D option 1 corrects the blocker recorded by `apply-W3.3-D-preflight-report` (`fa166d92-7d07-4531-9173-c5796d40d453`). It supersedes only W3.3-C success completion in C21/C30 and the C observable-contract clauses requiring C to complete/discard B. The accepted W3.3-C verification (`6fac8baf-c17f-479e-adf8-d50d1f563e1b`) remains historical evidence but **MUST** be scoped-reverified after implementation. C22–C29 failure behavior and all W1/W2/W3.1/W3.2/W3.3-A/B/C1-A behavior remain unchanged. No requirement is added; C31–C46 make **123** active scenarios. This is specification authority only.

## C-to-D Ownership Transfer

C success is provisional. After re-seal, equivalence, one credential resolution, render, and protected config write, C **MUST NOT** commit, discard, clean, or release the active sealed B journal/selected-target authority. C **MUST** return one opaque, single-use active completion handoff. The handoff **MUST** bind the admitted plan and original-input identities, selected target identity and canonical authority scope, successful C protected-write fact, profile persistence fact, and canonical v3 manifest value already admitted by A. It **MUST NOT** expose or accept caller-supplied endpoint, permit, credential, target path, profile payload, manifest bytes, or replacement manifest value.

D **MUST** be the mandatory next and sole owner of a valid handoff. Transfer is atomic and target-bound. Before transfer, C retains its existing taxonomy and rollback ownership. After transfer, only D **MAY** append, commit, roll back, clean, or release the held authority. No post-C second transaction is permitted.

Missing, double-used, foreign-target, stale, altered, or unbound handoff use **MUST** be rejected before new mutation as fixed redacted `hosted_mcp_invalid_intent` at `hosted_mcp.intent`. An invalid candidate does not transfer ownership; a formerly consumed handoff has no authority. A valid transferred handoff whose bound canonical facts fail D validation **MUST** be rolled back by D.

## D Completion and Error Contract

D **MUST** derive all completion inputs from the handoff and accepted canonical artifacts. It **MUST** verify the bound plan/input identities, selected target and scope, C-success fact, profile fact, and canonical manifest value; canonical v3 bytes **MUST** be encoded internally from that sealed value. D **MUST** append reversible prior-profile and prior-`lore-provenance-v3.json` state to the held completion journal, preserving exact prior absence or bytes and mode. Accepted W3.2 profile-path authority remains separate in scope but **MUST** remain held as part of D's one completion until final commit or rollback.

Normative order is: validate handoff/facts; begin reversible profile completion; canonically encode and validate v3; create/write/flush its protected target-scoped temporary; atomically replace target-root `lore-provenance-v3.json`; durably flush the applicable directory; remove owned temporary residue; then perform one logical final commit/cleanup/release. The v3 replacement is the last content publication. No success, profile completion, journal discard, or authority release **MAY** be observable earlier.

Legacy target-root `lore-install.json` v2 **MUST** remain byte-for-byte and mode-for-mode untouched: no backup-as-migration, decode-as-v3, overwrite, deletion, rename, adoption, or inferred ownership.

| Primary boundary | Observable primary outcome |
| --- | --- |
| Handoff/bound-fact validation | fixed `hosted_mcp_invalid_intent` at `hosted_mcp.intent` |
| Profile completion | existing exact `profile_state_*` code/path |
| Canonical manifest validation/encoding | existing strict manifest code and precise logical field path |
| Manifest publication, durability, final commit, or cleanup | fixed redacted `transaction_io` at `transaction.journal` |
| Any incomplete coherent rollback | overriding `transaction_residual_risk` at `transaction.rollback` |

The first failing D boundary is primary if rollback completes. D **MUST** perform exactly one reverse coherent rollback while authorities remain held: remove owned D secret/temporary residue; restore prior v3 absence or exact bytes/mode and durability; restore prior profile state and durability; restore C's protected config and remaining B resources in reverse journal order; clean owned residue; release. Later rollback diagnostics **MUST NOT** replace the primary error unless any restoration, durability, cleanup required for coherence, or release cannot complete; then the residual-risk outcome overrides and retains recovery evidence.

Profile persistence is success-only: a provisional D profile write **MUST** be absent or restored after every failed outcome. All messages, metadata, and recovery evidence **MUST** preserve prior redaction, reveal no endpoint beyond the permitted sealed final URL, token, profile value, project/repository-sensitive value, host path, manifest/journal bytes, owner metadata, or server internal. C retains exactly one resolver call and zero Lore Server/API/repository/storage/memory calls; D adds no resolver or such call and creates no duplicated server/storage/memory authority.

## Deterministic Scenarios

#### Scenario C31: Transfer active completion ownership (R12)
- GIVEN C finishes its protected write under active B authority
- WHEN C succeeds provisionally
- THEN it transfers one target-bound handoff without commit, cleanup, or release

#### Scenario C32: Prevent premature completion (R12)
- GIVEN C has returned a valid handoff but D has not durably published v3
- WHEN completion state is observed
- THEN no success, committed profile, journal discard, or released authority is observable

#### Scenario C33: Derive all D facts canonically (R8, R10, R12)
- GIVEN a valid handoff
- WHEN D validates its inputs
- THEN plan/input, target/scope, C success, profile, and v3 facts come only from sealed artifacts
- AND caller endpoint, permit, profile payload, or manifest bytes cannot substitute them

#### Scenario C34: Publish manifest last and commit once (R10, R12)
- GIVEN D profile completion and canonical v3 data are valid
- WHEN D succeeds
- THEN v3 is durably published after every other content write
- AND D performs the sole final commit/cleanup/release with no second transaction

#### Scenario C35: Fail D bound-fact validation coherently (R12, R14)
- GIVEN a valid transferred handoff has a mismatched bound canonical fact
- WHEN D validates before a D write
- THEN D returns the fixed invalid-intent outcome and coherently rolls back C/B

#### Scenario C36: Fail reversible profile completion (R3, R12)
- GIVEN D validation passes and profile begin, rebase, write, or durability fails
- WHEN D handles the existing typed profile error
- THEN that error remains primary and D rolls back all held C/B resources once

#### Scenario C37: Fail canonical v3 validation or encoding (R10, R12)
- GIVEN profile completion is provisional and sealed manifest data is invalid
- WHEN strict canonical encoding rejects it
- THEN its redacted manifest code/path is primary and D restores profile then C/B

#### Scenario C38: Fail every v3 publication boundary (R10, R12)
- GIVEN failure is injected at temp creation, write, file flush, replace, or directory flush
- WHEN D handles the first failure
- THEN `transaction_io@transaction.journal` is primary and reverse rollback is coherent

#### Scenario C39: Fail final commit or cleanup (R12)
- GIVEN v3 was durably replaced last but final journal commit or required cleanup fails
- WHEN D still holds authority
- THEN it restores v3, profile, and C/B resources rather than reporting partial success

#### Scenario C40: Override only for incomplete rollback (R12, R14)
- GIVEN any D primary failure and a rollback restoration, durability, cleanup, or release failure
- WHEN the outcome is reported
- THEN `transaction_residual_risk@transaction.rollback` overrides the primary error
- AND recovery evidence remains redacted and the target stays blocked as accepted

#### Scenario C41: Restore prior v3 exactly (R10, R12)
- GIVEN prior v3 is absent or present with specific bytes and mode
- WHEN any D failure rolls back
- THEN absence is restored or exact bytes/mode are restored durably with no residue

#### Scenario C42: Preserve legacy v2 (R10, R13)
- GIVEN target-root `lore-install.json` v2 exists
- WHEN D succeeds or fails at any boundary
- THEN its bytes, mode, name, and ownership evidence remain unchanged

#### Scenario C43: Persist profile only on complete success (R3, R12)
- GIVEN D provisionally writes the selected profile
- WHEN D later succeeds or fails
- THEN the profile is respectively committed once or restored exactly before release

#### Scenario C44: Reject handoff misuse without new mutation (R12, R14)
- GIVEN a missing, double-used, foreign-target, stale, altered, or unbound handoff
- WHEN completion is attempted
- THEN fixed invalid-intent behavior occurs before new mutation with no disclosure

#### Scenario C45: Preserve resolver and server-call boundaries (R14)
- GIVEN C transfers a successful handoff to D
- WHEN the complete flow is traced
- THEN C has exactly one resolver call, D has zero, and both have zero prohibited server/storage/memory calls

#### Scenario C46: Keep unrelated targets independent (R12)
- GIVEN two valid flows own unrelated canonical selected-target roots
- WHEN they reach C-to-D completion concurrently
- THEN neither selected-target authority blocks the other
- AND accepted profile-path authority alone may serialize a shared profile-state path

## Exclusions

Excluded: E, W4, full-suite or remote-CI authority, server/API/storage/memory contract invention, post-C second transactions, design/tasks/source/test/workflow/Git changes, and acceptance of D or global W3.3.
# Normative Product Amendment: W4 Observable Routing

## Status, Authority, and Supersession

This amendment supersedes canonical spec `a148536c-4af4-479c-b07b-b7525a06743d` (SHA-256 `92015e805bbfba559fdf81201e5e1e0ca2fcf11acbe3dbe6074619c7cfb051ff`) only by appending W4 public behavior. It resolves needs-input receipt `4722fcd3-c100-4339-8987-76da7c555765` from authoritative proposal `370522aa-580d-4906-ad8d-dac6f8f41e69` (SHA-256 `b532e6eb70a43fa658bbcb60ae8a7c47e8d46ce76ae35ad892fa60dbe08aab94`). The preceding 16 requirements and 123 scenarios, all accepted W1–W3.3 normative content, and their lineage remain unchanged. This amendment supersedes only preceding statements that W4 is excluded or pending.

## Added Requirements

### Requirement 17: Canonical and Legacy Mode Routing

The CLI **MUST** route `install --explain`, `install --dry-run`, and `install` to canonical explain, dry-run, and apply. It **MUST** route `install --legacy` and `install --legacy --dry-run` to legacy apply and dry-run. `--explain` **MUST** conflict with `--dry-run`, `--legacy`, and `--yes`; `--yes` **MUST** be valid only for canonical or legacy apply. Apply **MUST** prompt only on a TTY unless `--yes` is present; non-TTY apply without `--yes` **MUST** return `confirmation_required` without reading stdin.

#### Scenario D1: Route canonical explain
- GIVEN canonical explain is enabled for the target
- WHEN `install --explain` is invoked
- THEN the canonical explain route runs and reports zero mutation

#### Scenario D2: Route canonical dry-run
- GIVEN canonical dry-run is enabled for the target
- WHEN `install --dry-run` is invoked
- THEN the canonical dry-run route runs and reports zero mutation

#### Scenario D3: Route default canonical apply
- GIVEN canonical apply is enabled for the target
- WHEN `install` is invoked and confirmation is satisfied
- THEN the canonical apply route runs

#### Scenario D4: Route explicit legacy apply
- GIVEN the legacy compatibility path remains available
- WHEN `install --legacy` is invoked and confirmation is satisfied
- THEN only legacy apply runs and the route is identified as `legacy`

#### Scenario D5: Route explicit legacy dry-run
- GIVEN the legacy compatibility path remains available
- WHEN `install --legacy --dry-run` is invoked
- THEN only legacy dry-run runs with zero mutation

#### Scenario D6: Reject every explain conflict
- GIVEN any of `--dry-run`, `--legacy`, or `--yes` accompanies `--explain`
- WHEN flags are parsed
- THEN the CLI writes conflict guidance to stderr, exits 2, and runs no route

#### Scenario D7: Reject yes with either dry-run route
- GIVEN `--yes` accompanies canonical or legacy `--dry-run`
- WHEN flags are parsed
- THEN the CLI writes usage guidance to stderr, exits 2, and runs no route

#### Scenario D8: Accept yes only for apply
- GIVEN canonical or legacy apply is selected with `--yes`
- WHEN routing begins
- THEN confirmation is satisfied without a prompt and the selected apply route continues

#### Scenario D9: Confirm or decline interactively
- GIVEN apply is selected on a TTY without `--yes`
- WHEN the user accepts or declines the confirmation prompt
- THEN acceptance continues apply, while decline performs zero effects and exits 0

#### Scenario D10: Refuse noninteractive confirmation
- GIVEN apply is selected without `--yes` and stdin or stdout is not an interactive TTY
- WHEN confirmation is required
- THEN `confirmation_required` is returned with exit 1 and no byte is read from stdin

### Requirement 18: Human, JSON, and TUI Rendering

CLI modes **MUST** accept `--format human|json`, defaulting to `human`. Human final success reports **MUST** use stdout; progress, warnings, deprecation, and errors **MUST** use stderr. JSON mode **MUST** emit exactly one newline-terminated object on stdout using `schema_version: "lore.install.result/v1"`; it **MUST** include mode, route, target, outcome, admission, changed-state, rollback, residual-risk, report, warnings, and structured-error facts as applicable, and **MUST NOT** emit ANSI, animation, spinner, progress chatter, or a second object. Syntactically admitted failures **MUST** use that object; parser failures **MUST** use stderr and exit 2 without JSON. The TUI **MUST** render the same typed result and event meanings.

#### Scenario D11: Render human success channels
- GIVEN human explain, dry-run, or apply succeeds
- WHEN final output is emitted
- THEN the final report is on stdout and stdout contains no warning, progress, or error line

#### Scenario D12: Render human diagnostics channels
- GIVEN human mode emits progress, warning, deprecation, or failure
- WHEN streams are inspected
- THEN those records are on stderr and no failure is duplicated as a stdout final report

#### Scenario D13: Render one JSON success object
- GIVEN a syntactically valid JSON-mode invocation succeeds
- WHEN output is captured
- THEN stdout is one `lore.install.result/v1` object plus one final newline and stderr is empty
- AND no ANSI, spinner, animation, or progress record appears

#### Scenario D14: Structure an admitted JSON failure
- GIVEN a syntactically valid route returns non-admission, confirmation-required, disabled-route, apply, rollback, cancellation, or residual-risk failure
- WHEN JSON output is captured
- THEN stdout contains exactly one versioned object with the stable outcome/error and rollback facts
- AND stderr is empty and secrets are absent

#### Scenario D15: Keep parser errors outside JSON
- GIVEN an unknown flag, conflicting flag, missing value, or format other than `human` or `json`
- WHEN parsing fails
- THEN stderr contains actionable usage text, stdout is empty, and exit code is 2

#### Scenario D16: Preserve renderer parity
- GIVEN one typed result/event sequence is rendered by human CLI, JSON CLI, and TUI
- WHEN semantic fields are compared
- THEN mode, route, target, outcome, admission, effects, rollback, residual risk, warnings, and error code agree

### Requirement 19: Exact Exit Status Precedence

The command **MUST** use only exits 0, 1, 2, 3, and 130 for W4 outcomes. Exit 0 **MUST** mean success or voluntary pre-execution decline/cancel; 1 **MUST** mean non-admission, disabled route, confirmation required, or failure/cancellation with complete rollback; 2 **MUST** mean usage, flag, or format error; 3 **MUST** mean residual risk and override every primary outcome; 130 **MUST** mean signal interruption and, after mutation authority is acquired, **MUST** occur only after complete rollback.

#### Scenario D17: Return zero for clean completion
- GIVEN success or voluntary cancellation before execution
- WHEN the command ends without residual risk
- THEN it exits 0

#### Scenario D18: Return one for operational refusal or recovered failure
- GIVEN non-admission, disabled routing, confirmation-required, or an execution failure/cancel with complete rollback
- WHEN no usage error or residual risk exists
- THEN it exits 1

#### Scenario D19: Return two for usage rejection
- GIVEN parsing, flag combination, format, or TUI-TTY validation fails
- WHEN the command ends
- THEN it exits 2 before domain execution

#### Scenario D20: Give residual risk absolute precedence
- GIVEN any primary success, failure, cancellation, or interruption leaves incomplete coherent rollback or cleanup
- WHEN the final outcome is selected
- THEN it exits 3 and reports residual risk

#### Scenario D21: Interrupt before authority
- GIVEN a termination signal arrives before mutation authority is acquired
- WHEN handling completes
- THEN no rollback is invented, no effects remain, and exit is 130

#### Scenario D22: Interrupt after authority with complete rollback
- GIVEN a termination signal arrives after mutation authority is acquired
- WHEN rollback and required cleanup complete
- THEN the command reports interruption only after completion and exits 130

#### Scenario D23: Interrupt with incomplete rollback
- GIVEN a termination signal arrives after authority and rollback cannot restore coherence
- WHEN the final outcome is selected
- THEN residual risk overrides interruption and exit is 3 rather than 130

### Requirement 20: Sealed Explain Behavior

Canonical explain **MUST** validate and seal the deterministic non-secret report while performing no mutation, credential resolution, network call, finalizer action, target/profile authority acquisition, backup, journal, profile persistence, or runtime activation. It **MUST** include deterministic ordering, route/gate status, admission, guidance, and redacted errors; repeated equivalent inputs and relevant local facts **MUST** produce byte-identical format-specific output.

#### Scenario D24: Explain without effects or external access
- GIVEN a valid or rejected canonical request
- WHEN explain runs
- THEN mutation, authority, credential, network, finalizer, backup, journal, and persistence counts are zero

#### Scenario D25: Seal an admitted report
- GIVEN identical normalized input and relevant local facts
- WHEN explain is repeated in one format
- THEN ordered output bytes match and identify canonical explain, target gate, and admission

#### Scenario D26: Explain rejection actionably
- GIVEN profile, capability, ownership, migration, or route validation rejects the request
- WHEN explain completes
- THEN each safe error has stable guidance and admission is false without effects

#### Scenario D27: Redact and repeat safely
- GIVEN rejected facts contain credentials, paths, project/repository-sensitive values, or payloads
- WHEN human or JSON explain is repeated
- THEN forbidden values never appear and deterministic safe output remains stable

### Requirement 21: Canonical Dry-Run and Apply Execution

Canonical dry-run and apply **MUST** consume the accepted W3.3 compile, reconciliation, transaction, finalization, completion, and outcome seams rather than reconstructing them. Both **MUST** identify the canonical route; dry-run **MUST** stop before confirmation, credential resolution, authority, backup, journal, finalizer, profile persistence, and mutation. Apply **MUST** confirm before authority, resolve credentials only at the accepted post-seal finalization boundary, expose typed phase/progress events, preserve rollback and residual-risk precedence, leave legacy v2 untouched, publish v3 manifest last, and leave no owned temporary/journal residue after coherent completion.

#### Scenario D28: Execute a sealed canonical dry-run
- GIVEN canonical dry-run is enabled and compilation is admitted
- WHEN dry-run executes accepted pre-mutation seams
- THEN it reports the complete prospective result with zero confirmation, credential, authority, finalizer, persistence, or mutation activity

#### Scenario D29: Apply only after route and confirmation
- GIVEN canonical apply is enabled and confirmation is satisfied
- WHEN execution starts
- THEN canonical route discrimination and sealed-plan admission precede target authority or mutation

#### Scenario D30: Resolve credentials only at finalization
- GIVEN a sealed canonical apply requires a credential slot
- WHEN accepted W3.3 finalization is reached
- THEN resolution occurs exactly at that boundary and never during explain, dry-run, confirmation, or preflight

#### Scenario D31: Emit typed progress without changing semantics
- GIVEN canonical apply advances through accepted W3.3 phases
- WHEN events are observed
- THEN ordered textual phase/progress records identify the canonical route and never claim a phase before its boundary

#### Scenario D32: Roll back an apply failure completely
- GIVEN canonical apply fails after authority with recoverable prior state
- WHEN accepted rollback completes
- THEN prior coherent target/profile state is restored, outcome records complete rollback, and exit is 1

#### Scenario D33: Report residual risk from apply
- GIVEN canonical apply or rollback cannot restore required coherence or cleanup
- WHEN outcome is rendered
- THEN residual risk overrides the primary failure and exit is 3

#### Scenario D34: Preserve publication and residue invariants
- GIVEN canonical apply succeeds or rolls back coherently
- WHEN target bytes are inspected
- THEN legacy `lore-install.json` v2 is byte/mode unchanged, v3 was the last publication, and no owned temporary or journal residue remains

### Requirement 22: TUI State and Cancellation

The TUI **MUST** require a TTY and use the same typed domain workflow as CLI renderers without duplicating transaction logic. It **MUST** support deterministic prepare, confirmation, execute, result, navigation, cancellation, retry, and resize behavior. Back/Esc before Execute **MUST** discard Prepared with zero effects; during Execute navigation **MUST** remain blocked and cancellation **MUST** wait for rollback/final result. Retry **MUST** re-Prepare from current facts and **MUST NOT** reuse or reconstruct Prepared; residual risk **MUST** disable retry. Text phase/progress **MUST** always exist; animation **MUST** be decorative and disabled by `LORE_NO_ANIMATION=1`.

#### Scenario D35: Reject TUI without a TTY
- GIVEN TUI is requested without an interactive terminal
- WHEN startup validates the environment
- THEN it emits CLI usage guidance, performs no domain work, and exits 2

#### Scenario D36: Navigate before execution safely
- GIVEN a plan is Prepared but Execute has not begun
- WHEN Back or Esc is selected
- THEN Prepared is discarded, the prior selection state is shown, and effects remain zero

#### Scenario D37: Confirm before TUI execute
- GIVEN TUI displays an admitted Prepared plan
- WHEN the user accepts confirmation
- THEN Execute begins through the same canonical or explicit legacy domain route

#### Scenario D38: Block navigation during execute
- GIVEN target authority or mutation is active
- WHEN navigation keys are pressed
- THEN no screen transition abandons execution and current textual progress remains visible

#### Scenario D39: Cancel execute coherently
- GIVEN the user requests cancellation during Execute
- WHEN cancellation is processed
- THEN the TUI waits for rollback and a typed final result before allowing exit or navigation

#### Scenario D40: Retry from fresh facts
- GIVEN a retryable failure with no residual risk
- WHEN Retry is selected
- THEN current facts are re-read and a new Prepare occurs without reuse or reconstruction of prior Prepared

#### Scenario D41: Disable retry on residual risk
- GIVEN the final result reports residual risk
- WHEN result actions are rendered
- THEN Retry is unavailable and recovery guidance remains visible

#### Scenario D42: Reflow on resize without semantic change
- GIVEN any nonterminal TUI state
- WHEN terminal dimensions change
- THEN content reflows without changing selection, phase, route, outcome, or domain execution

#### Scenario D43: Provide reduced-motion text parity
- GIVEN `LORE_NO_ANIMATION=1` or animation is unavailable
- WHEN TUI work proceeds
- THEN animation is absent while the same textual phases, progress, cancellation, and final semantics remain available

### Requirement 23: Per-Target Canonical Gates and Kill Switch

Pi, OpenCode, Codex, and Antigravity **MUST** each have independent canonical gates: E admits explain; D admits explain and dry-run; A admits explain, dry-run, and apply. Off admits none. A disabled canonical route **MUST** return `canonical_route_disabled` with exit 1 and **MUST NOT** fall back to legacy. Emergency policy **MAY** demote A→D→E→off per target. Canonical apply **MUST** become the default only for a target at A; E and D **MUST NOT** silently change the default route.

#### Scenario D44: Enforce E gate
- GIVEN a target is at E
- WHEN explain, dry-run, and apply are each requested
- THEN only explain is admitted and the other canonical routes return `canonical_route_disabled`

#### Scenario D45: Enforce D gate
- GIVEN a target is at D
- WHEN explain, dry-run, and apply are each requested
- THEN explain and dry-run are admitted while apply returns `canonical_route_disabled`

#### Scenario D46: Enforce A gate and default
- GIVEN a target is at A
- WHEN explain, dry-run, or default install is invoked
- THEN each canonical route is admitted and default install selects canonical apply

#### Scenario D47: Enforce off gate
- GIVEN a target is off
- WHEN any canonical mode is requested
- THEN `canonical_route_disabled` is returned with exit 1 and zero canonical or legacy execution

#### Scenario D48: Never fall back to legacy
- GIVEN a canonical route is disabled at any gate
- WHEN routing fails
- THEN no legacy adapter, confirmation, preparation, or mutation runs unless `--legacy` was explicitly supplied in a separate valid invocation

#### Scenario D49: Demote one target independently
- GIVEN emergency policy demotes one target from A through D, E, or off
- WHEN all four targets are inspected
- THEN only that target loses the corresponding canonical routes and explicit legacy availability is unchanged

### Requirement 24: Explicit Legacy Deprecation and Retirement

Every explicit legacy invocation **MUST** identify the legacy route and emit actionable deprecation guidance without changing its selected behavior. Human mode **MUST** place that warning on stderr; JSON and TUI **MUST** carry the same typed warning in their final/result rendering. Retirement **MUST NOT** be automatic. Eligibility **MUST** require all four targets at A, CLI/TUI and compatibility/golden parity, no rollback regressions, a reviewed adoption assessment, and both one published deprecation-bearing version and 30 elapsed days after its publication. Removal **MUST** require a separate reviewed change.

#### Scenario D50: Warn on human legacy use
- GIVEN explicit legacy apply or dry-run uses human format
- WHEN routing succeeds
- THEN stderr identifies legacy/deprecation and the selected legacy behavior remains unchanged

#### Scenario D51: Structure machine-readable legacy warning
- GIVEN explicit legacy use is rendered as JSON or TUI
- WHEN the final result is observed
- THEN route is `legacy` and typed deprecation guidance is present without extra JSON stderr chatter

#### Scenario D52: Block retirement before all targets reach A
- GIVEN any of Pi, OpenCode, Codex, or Antigravity is below A
- WHEN retirement eligibility is reviewed
- THEN legacy remains available regardless of other evidence

#### Scenario D53: Require parity, rollback, and adoption evidence
- GIVEN all four targets are at A but parity, no-regression, or adoption review is missing
- WHEN eligibility is evaluated
- THEN retirement is ineligible and the missing evidence is identified

#### Scenario D54: Enforce the full deprecation window
- GIVEN all technical/review evidence exists
- WHEN no deprecation-bearing version is published or fewer than 30 days have elapsed since publication
- THEN retirement remains ineligible

#### Scenario D55: Remove legacy only through separate review
- GIVEN every eligibility condition is satisfied
- WHEN legacy removal is proposed
- THEN a separate reviewed change must authorize deletion of the flag, TUI choice, adapter, and old callers; W4 itself deletes none

### Requirement 25: Harness Guidance and Server Authority

Canonical projections for Pi, OpenCode, Codex, and Antigravity **MUST** use each target's validated parser contract and preserve target asymmetry. Guidance **MUST** distinguish local compiler `ProjectID` from Lore Server project UUID/key, accept an optional repository selector only as an explicit server-bound repository UUID, and direct discovery through bounded `lore_project_activity`, then `lore_project_context` or filter-driven `lore_memory_search`, then scoped `lore_memory_get` for full content. Server authority, authorization, filtering, ordering, budgets, errors, audit, and security/redaction **MUST** remain unchanged and **MUST NOT** be inferred or reimplemented locally.

#### Scenario D56: Project parser-valid guidance to four targets
- GIVEN equivalent admitted guidance for Pi, OpenCode, Codex, and Antigravity
- WHEN each projection is parsed by its supported target contract
- THEN each is accepted in target-native form and unsupported parity is explained rather than emulated

#### Scenario D57: Keep local ProjectID separate
- GIVEN local profile state has a compiler `ProjectID` and guidance needs server project scope
- WHEN any target projection is rendered
- THEN the local value is never converted, substituted, or sent as a server UUID or key

#### Scenario D58: Require explicit repository UUID
- GIVEN repository-scoped guidance is selected
- WHEN projection input is validated
- THEN only an explicit bound repository UUID is accepted and none is inferred from paths, remotes, URLs, titles, metadata, or text

#### Scenario D59: Render the bounded discovery sequence
- GIVEN an agent needs project orientation and then a full memory body
- WHEN guidance is followed
- THEN activity is used first, context or filter-driven search narrows discovery, and full get uses the same project and optional repository scope

#### Scenario D60: Preserve filter-only search semantics
- GIVEN guidance describes `lore_memory_search`
- WHEN its accepted inputs are presented
- THEN only type, scope, limit, and optional repository filtering are claimed, with no query-text or relevance promise

#### Scenario D61: Preserve compact/full-body boundaries
- GIVEN activity, context, or search returns compact metadata, summary, or preview
- WHEN guidance presents the result
- THEN it claims no full body or generated summary and requires full get for content

#### Scenario D62: Preserve server security and authority
- GIVEN authorization, project/repository binding, pair filtering, ordering, budgets, stable errors, audit, or redaction is involved
- WHEN CLI output or projected guidance describes behavior
- THEN it attributes enforcement to Lore Server and exposes no secret or locally invented authority

### Requirement 26: Deterministic W4 Acceptance Evidence

W4 acceptance **MUST** be traceable to D1–D69 and include deterministic human/JSON/TUI goldens, canonical-versus-legacy route markers, accepted W3.3 event/outcome traces, target and host-platform coverage, race and vet checks, the full suite, and required CI evidence. Fixtures **MUST** be isolated from user state and live services and **MUST** inspect all streams for secret disclosure. Existing Requirement 16 baseline isolation remains authoritative; any additional regression **MUST** block acceptance.

#### Scenario D63: Reproduce deterministic goldens
- GIVEN fixed inputs, terminal dimensions, gate state, and local facts
- WHEN human, JSON, and TUI fixtures repeat
- THEN bytes match approved goldens, JSON is singular/versioned, and no ANSI appears where prohibited

#### Scenario D64: Trace every W4 scenario
- GIVEN the W4 acceptance ledger
- WHEN traceability is reviewed
- THEN each D1–D69 scenario maps to an automated test or explicit platform/CI evidence with no orphan test claim

#### Scenario D65: Distinguish canonical and legacy paths
- GIVEN equivalent canonical and explicit legacy invocations
- WHEN result/event traces and goldens are inspected
- THEN stable route markers prove which path ran and prove disabled canonical routing never invoked legacy

#### Scenario D66: Cover targets, platforms, concurrency, and cancellation
- GIVEN Pi, OpenCode, Codex, and Antigravity across supported darwin, linux, and windows evidence
- WHEN acceptance runs route, TTY/non-TTY, signal, resize, retry, and authority-bound cancellation cases
- THEN observable outcomes match this amendment without race-dependent variance

#### Scenario D67: Pass focused race and vet checks
- GIVEN the W4 candidate
- WHEN focused tests, `go test -race ./...`, and `go vet ./...` run in supported evidence environments
- THEN they pass without new race, vet, stream, redaction, or residue defect

#### Scenario D68: Evaluate the full suite and CI
- GIVEN focused evidence passes
- WHEN `go test ./...` and required remote CI complete
- THEN every W4-related job passes and any failure beyond the isolated Requirement 16 baseline blocks acceptance

#### Scenario D69: Keep acceptance hermetic and secret-safe
- GIVEN golden, parser, TUI, signal, rollback, and platform fixtures execute
- WHEN filesystem, network, stdout, stderr, event, and recovery evidence are inspected
- THEN tests use isolated state/fakes, make no unintended live-service call, and disclose no token, credential, sensitive path, or payload

## W4 Traceability Map

| W4 area | Requirement | Scenarios |
| --- | --- | --- |
| Modes, conflicts, confirmation | R17 | D1–D10 |
| Formats, streams, renderer parity | R18 | D11–D16 |
| Exit precedence | R19 | D17–D23 |
| Explain purity and determinism | R20 | D24–D27 |
| Canonical W3.3 execution seams | R21 | D28–D34 |
| TUI state and accessibility | R22 | D35–D43 |
| Per-target rollout gates | R23 | D44–D49 |
| Legacy deprecation/retirement | R24 | D50–D55 |
| Harness guidance/server boundary | R25 | D56–D62 |
| Deterministic acceptance | R26 | D63–D69 |

## Active Specification Accounting

- Requirements: **26** (preserved 16 + W4 R17–R26)
- Scenarios: **192** (preserved 123 + W4 D1–D69)
- Superseded canonical spec: `a148536c-4af4-479c-b07b-b7525a06743d` / `92015e805bbfba559fdf81201e5e1e0ca2fcf11acbe3dbe6074619c7cfb051ff`
- Resolved needs-input receipt: `4722fcd3-c100-4339-8987-76da7c555765`
- W4 proposal authority: `370522aa-580d-4906-ad8d-dac6f8f41e69` / `b532e6eb70a43fa658bbcb60ae8a7c47e8d46ce76ae35ad892fa60dbe08aab94`

## Boundaries

This amendment specifies W4 behavior only. It does not redesign W3.3, normalize the parallel W4 design, authorize tasks or implementation, mutate source/tests/workflows/CI/Git, alter server contracts, publish or remove legacy behavior, or accept W4.
