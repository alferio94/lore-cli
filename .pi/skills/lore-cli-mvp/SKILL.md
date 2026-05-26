---
name: lore-cli-mvp
description: >
  Product scope and HTTP contract guidance for the Lore CLI MVP.
  Trigger: When planning, specifying, designing, implementing, or reviewing lore-cli MVP commands, auth, local config, status, logout, or doctor behavior.
license: Apache-2.0
metadata:
  author: gentleman-programming
  version: "1.0"
---

## When to Use

- Creating or updating SDD artifacts for `create-lore-cli-mvp`.
- Implementing or reviewing `lore login`, `lore status`, `lore logout`, or `lore doctor`.
- Making decisions about CLI scope versus the broader Lore installer vision.
- Integrating the CLI with existing `lore-server` HTTP contracts.

## Critical Patterns

- Keep the MVP intentionally narrow: local auth/config diagnostics first, installer/Pi automation later.
- For the current multi-harness install slice, Pi stays the preserved default target while Antigravity is documented as the prompt-and-skills-first MVP target.
- Antigravity MVP non-goals must stay explicit: no Pi-style overlay emulation, no auto-install guarantee, no daemon/autostart behavior, and MCP remains optional.
- Accepted exception for `cli-password-login-keychain-token`: primary login may use `POST /v1/auth/login` with email + hidden password to mint a normal user API token, then persist only that token in the OS keychain.
- Manual `--token` login remains the explicit compatibility path; automation may use `--password-stdin`, but never `--password` argv or plaintext password env vars.
- Treat `logout` as local credential removal only unless a later spec explicitly adds server-side revocation.
- Do not call `POST /v1/bootstrap/init` as part of normal `login`; bootstrap is an operator setup route, not a runtime login route.
- Prefer explicit, user-safe output: never print the stored token, password, or full Authorization header.
- If a server contract gap appears, document it in SDD before changing `lore-server`.

## Command Contract Summary

| Command | MVP behavior |
| --- | --- |
| `lore login` | Primary path: accept or prompt for server URL + email, read a hidden password (or `--password-stdin` for automation), mint a normal user API token with `POST /v1/auth/login`, validate it with `GET /v1/me`, then persist metadata-only config plus the token in the OS keychain. Manual `--token` remains compatibility mode. |
| `lore status` | Read local config, report server URL/config presence, check `/healthz`, `/readyz`, and authenticated `/v1/me` when a token exists. |
| `lore logout` | Remove local config/credentials and clearly state that remote token revocation is not performed. |
| `lore doctor` | Run diagnostics for config file, URL parsing, network reachability, readiness, token validity, and Pi availability when feasible. |

## Server Contracts

- `GET /healthz` is public and returns `200` with `{ "data": { "status": "ok" } }` when live.
- `GET /readyz` is public and returns `200` with the same success envelope when ready, or `503` with an error envelope when not ready.
- `GET /v1/me` requires `Authorization: Bearer <user-api-token>`.
- Successful `/v1/me` includes `id`, `user_id`, `roles`, `token_id`, `token_source`, and `kind` under `data`.
- Unauthorized responses use `{ "error": { "code", "message", "request_id" } }` and should become actionable CLI diagnostics.
- Bootstrap subjects are rejected from `/v1/me`; a bootstrap token is not a valid CLI login token.

## Commands

```bash
# Accepted auth flows for the current MVP slice
lore login --server https://example.test --email admin@example.com
printf '%s\n' '<password-from-secret-store>' | lore login --server https://example.test --email admin@example.com --password-stdin
lore login --server https://example.test --token "$LORE_API_TOKEN"
lore status
lore doctor
lore logout
```

## Install Target Contract Note

- Pi remains the default recommended install target and the only path with the preserved Pi-native extension/runtime behavior; Pi MCP migration stays deferred out of this MVP.
- Antigravity wording should describe the MVP as prompt append/merge plus managed skills, with optional MCP only when explicitly selected.
- When optional Antigravity MCP is documented, describe it as a per-session stdio command that launches `lore mcp serve`; never describe it as a raw `/v1/mcp` URL or token-based config.
- Do not describe Antigravity as a Pi clone, and do not imply parity for overlays, background subagents, automatic install flows, daemonization, or autostart.

## Resources

- Lore artifacts: `sdd/create-lore-cli-mvp/init` and `sdd/create-lore-cli-mvp/explore`.
- Server reference repo: `/Users/alfonsocarmona/personal/lore2/lore-server`.
