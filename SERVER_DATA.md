# Lore Server Boundary

Lore Server owns durable identity, authorization, persistence, and MCP memory semantics. Lore CLI is a client and installer: it authenticates a user, keeps local credential metadata, and renders supported harness configuration. It must not recreate server-side identity, access control, repository binding, retrieval, audit, or storage rules.

## CLI-facing contract

| Area | Server responsibility | CLI responsibility |
| --- | --- | --- |
| Authentication | `POST /v1/auth/login` mints a normal user API token; `GET /v1/me` validates it. | Obtain or accept the token, validate it, and store only local metadata plus the keychain credential. |
| Health | `/healthz` reports liveness; `/readyz` reports readiness. | Surface actionable status, doctor, and install-preflight diagnostics. |
| Hosted MCP | `<lore_url>/v1/mcp` applies server authentication and tool policy. | Configure supported targets with the saved bearer token where their native MCP configuration requires it. |
| Memory | Server defines project/repository identity, scope, retrieval, compact/full boundaries, and authorization. | Use documented server tools and explicit IDs; do not infer authority from remotes, paths, titles, or content. |

A bootstrap token is not a valid CLI login token. Logout removes local credentials only; it does not revoke the remote token.

## Memory boundary

Server memory discovery is intentionally bounded. Compact activity, context, and search responses are not full-memory retrieval; full content requires the explicit scoped get path. Repository selection, where supported, is an explicit bound repository identifier and is validated by the server.

Do not add client-side lexical or semantic search behavior under an existing memory operation unless Lore Server first defines that contract. Do not duplicate server-side repository registration, pair filtering, non-disclosure, audit, ordering, grouping, or transaction behavior in Lore CLI.

## Historical server evidence

The following is historical delivery evidence retained for maintainers, not a fresh server audit or a claim about the current Lore Server deployment:

- Compact summaries, project-repository bindings, and repository-aware MCP retrieval were recorded as merged through Lore Server PRs #2–#6 and #8–#11, with final merge `47d9c9b`.
- The recorded server verification covered compact/full retrieval boundaries, explicit repository scoping, binding validation, sanitized failures, and transactional memory/audit behavior.
- Historical server SDD settlement details are not a prerequisite for CLI work and should not be reconstructed from this handoff.

Revalidate current server source, deployment policy, and MCP schemas before changing an adapter or making an operational guarantee.

## CLI maintainer rules

- Use `GET /healthz`, `GET /readyz`, and authenticated `GET /v1/me` for the documented CLI health/auth checks.
- Treat server errors as actionable diagnostics without printing tokens, passwords, or Authorization headers.
- Keep the CLI's local installation concepts separate from server project and repository identifiers.
- Preserve the local plaintext-token warning where a target's native HTTP MCP configuration requires a bearer header on disk.
- Keep Lore Server changes in Lore Server; a missing server contract requires a separately specified server change.

## References

- CLI behavior and supported target configuration: [`README.md`](README.md)
- CLI product and HTTP guidance: [`.pi/skills/lore-cli-mvp/SKILL.md`](.pi/skills/lore-cli-mvp/SKILL.md)
- Canonical rollout closure: [`verify-report-W4-global-aggregate.md`](openspec/changes/archive/2026-09-02-canonical-capability-profile-compiler/canonical-capability-profile-compiler/verify-report-W4-global-aggregate.md)
