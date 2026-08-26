# Design: Canonical Capability Profile Compiler — W3.3-D Unit-2 Restore Amendment

## Status and invariant boundary
This amendment supersedes design `c501f036-fdd3-4db2-b488-897d5513b9c6` (`2d9bce27…21f7`) only for Unit 2, following blocked preflight `cb095df9-acc6-4922-9d2d-e7844c2cf8c9` and parity readback `bd6c9568-6d5b-4830-9054-14a79b3ca57e`. Normative spec `a148536c-4af4-479c-b07b-b7525a06743d` (`92015e80…51ff`) and observable semantics do not change. Validated independent-PASS Unit 1 is immutable.

The sequence remains: consume Unit-1 handoff → hold selected-target authority → acquire profile authority → authoritative snapshot/rebase/provisional profile write → canonical v3 publication last → sole final commit → target release → profile release. Authority order, deadlock design, reverse rollback (D residue, prior v3, profile, then remaining C/B journal in reverse), manifest-last, residual-risk precedence, redaction, unrelated-root independence, v2 non-mutation, server exclusion, and prohibition on a second transaction remain unchanged.

## Decisions and internal interfaces

| Choice | Rejected alternative | Rationale |
|---|---|---|
| Add the two build-tagged adapters to Unit 2 | Common-code raw-path deletion | Only adapters retain accepted B12/B13 canonical identity, no-follow/reparse, OS authority, durability, and cleanup semantics. |
| Use an opaque held-state contract | Exported delete API or platform handles | D needs exact rollback, not general deletion or leaked kernel/journal internals. |
| Preserve public `Complete*` through the same acquisition primitive | Parallel lock lifecycle | One acquisition implementation preserves ordering and avoids split authority. |

```go
type heldProfileState interface{ heldProfileState() }
type heldRestorePlatform interface {
    snapshotHeld(authority) ([]byte, bool, heldProfileState, error)
    restoreHeld(authority, heldProfileState) error
}
type heldStoreAuthority struct { /* unexported platform + owned authority */ }
```

`acquireStoreAuthority` returns the internal held lifecycle; `withStoreAuthority` delegates to it and preserves current public behavior. The D-only lease asserts `heldRestorePlatform`, snapshots and rebases while the same authority is held, and calls `restoreHeld` before release on abort. The opaque state binds the exact authority object and canonical target identity; it records presence independently of byte length and adapter-private restoration facts. The seam is unexported, adds no dependency, exposes no handle or journal value, and cannot serve as a general delete API.

For prior absence, `restoreHeld` may remove only the adapter-validated canonical leaf while the originating authority is live. Unix uses held-parent descriptor-relative no-follow identity checks; Windows revalidates parent volume/index, folded leaf, reparse state, and owned mutex before deletion. Raw caller paths are forbidden. Both adapters durably flush applicable directory state and remove only owned matching residue. For prior presence, restoration uses same-directory protected temporary creation, exact bytes and POSIX mode or Windows protected state, file flush, atomic replacement, directory/write-through durability, and temporary cleanup. Any identity, restore, durability, cleanup, or release failure preserves redacted recovery evidence and promotes existing residual risk. No v2 path is named or mutated.

## Serialized eight-file universe

| Role | File | Required starting SHA-256 |
|---|---|---|
| Unit-1 preservation | `internal/install/hosted_mcp_finalizer.go` | `7fdb2b7ea24594dead4fa14dde1d51c535686dab85ed50f6c906a895a6ee8289` |
| Unit-1 preservation | `internal/install/hosted_mcp_finalizer_test.go` | `a719f9fdaad4ff4e065390d90f04d3f4c12178d565ca845a8ecd7010f65e1663` |
| Unit-2 edit | `internal/install/profile_store.go` | `d83e2bf7ac560353337e892876f113fbe72c21312041b39b4e7d870d5b84d1bb` |
| Unit-2 edit | `internal/install/profile_store_authority.go` | `321a4d516e3ed7a6c8257eb45d681adbcb699b00863d26d01e259fb3f889c44e` |
| Unit-2 edit | `internal/install/profile_store_unix.go` | `5acdf670df1adc1ae9db1681dcc1848964bf165aadb0657ab30c04f692fc2b7f` |
| Unit-2 edit | `internal/install/profile_store_windows.go` | `11f8ce898403285e1a3e90fddd3ca11ec539b1b756aba1b0bc999c95271cb47b` |
| Unit-1 result gate + Unit-2 edit | `internal/install/transaction.go` | `acd952c917948fbdca98d589008087d7081ecb9b9bad3b8d71e7dee7208d116f` |
| Unit-1 result gate + Unit-2 edit/test | `internal/install/transaction_test.go` | `d4421dd83b8caf9dce608b7f2926915ce5108dfc6ee7e19299b5c7033fcbddcd` |

No other file, dependency, workflow, Git/CI surface, E, or W4 enters the candidate. Unit 2 starts only after all eight hashes and Unit-1 PASS lineage match, then freezes one unsplit candidate and a fresh cap. Historical cap 875 is invalid.

## Verification strategy and evidence scope
`transaction_test.go` adds table-driven absent/present rollback, failure-order, v2-preservation, and residue assertions. A fake `heldRestorePlatform` proves authority identity and call order; each native adapter’s existing `fail(stage)` hook is reached through an unexported optional test seam, so the same tests execute Unix or Windows behavior on that OS without new files. Later native runners/overlays are validation evidence, not design-file authority.

W3.2 acceptance `w3.2-acceptance-accepted-1-dg-6e8e4ac1` at candidate `1fd4dd2d…a766` and final B1–B18 verification digest `a774564a…469e` remain valid historical authority. Unchanged acquisition identity/order, contention/timeouts, rebase/conflict/idempotence, crash/live-owner safety, B12 path rejection, permissions, redaction, and public `Complete*` evidence remain reusable subject to preservation checks. Scoped reverification is required for both adapters’ new held snapshot/restore path: absent canonical deletion, present exact bytes/mode/protection, authority ownership, no-follow/reparse resistance, durability ordering, injected restore failures, residue cleanup, D reverse rollback, and residual-risk precedence. No build or test runs in this design phase.
