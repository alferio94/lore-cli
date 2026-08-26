# Proposal: Canonical Capability Profile Compiler Authority Rebaseline

## Intent
Resume the compiler delivery without rewriting its historical record: Gentle AI remains blocked historical authority, while Pi/Lore becomes execution and review authority for W2B–W4.

## Scope
### In Scope
- Preserve immutable W1–W2A acceptance receipts and W2B candidate/non-acceptance lineage.
- Require hybrid Lore+OpenSpec persistence and hash read-back for every remaining phase artifact.
- Rebaseline the remaining delivery chain: W2B gate, then W2C, W3, and W4; QA is the delivery target.

### Out of Scope
- Production rollout, publishing, push/PR, source/test changes, or acceptance of W2B from local checks.
- Replacing, relabeling, or deleting historical evidence or activating bounded review.

## Capabilities
### New Capabilities
- None.

### Modified Capabilities
- `canonical-capability-profile-compiler`: delivery authority, evidence persistence, and acceptance gating are rebaselined; product requirements remain inherited from immutable legacy lineage.

## Approach
Treat Engram mirrors as immutable historical inputs. Pi/Lore records new execution/review evidence only after both Lore and OpenSpec contain identical content. W2B remains a candidate until a fresh Pi/Lore gate accepts it; no later work may begin beforehand. QA, not production, closes W4.

## Affected Areas
| Area | Impact | Description |
|---|---|---|
| `openspec/changes/canonical-capability-profile-compiler/` | Modified | Hybrid planning and provenance artifacts only |
| Lore `sdd/canonical-capability-profile-compiler/*` | Modified | Authority and phase evidence |

## Risks
| Risk | Likelihood | Mitigation |
|---|---|---|
| Historical evidence drift | Med | Immutable hashes and dual-store read-back |
| Candidate mistaken for acceptance | High | Preserve W2B 2B.4 as an external gate |
| Scope creep | Med | QA-only target; production excluded |

## Rollback Plan
Stop subsequent phases, retain all evidence, and revert only mutable proposal/rebaseline records; do not alter historical mirrors or acceptance receipts.

## Dependencies
- Recovered Engram observations 1475, 1476, 1477, 1479, 1484, and 1485 with matching hashes.

## Success Criteria
- [ ] Gentle AI historical block and Pi/Lore W2B–W4 authority are both explicit.
- [ ] W1–W2A remain immutable; W2B remains unaccepted.
- [ ] Each remaining artifact passes Lore/OpenSpec hash equality before advancement.
- [ ] QA is the terminal delivery target; production is excluded.
