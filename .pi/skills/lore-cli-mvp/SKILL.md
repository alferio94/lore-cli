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
- Build against normal user API tokens. Do not use username/password, browser auth, JWT login, or bootstrap tokens for normal login.
- Treat `logout` as local credential removal only unless a later spec explicitly adds server-side revocation.
- Do not call `POST /v1/bootstrap/init` as part of normal `login`; bootstrap is an operator setup route, not a runtime login route.
- Prefer explicit, user-safe output: never print the stored token or full Authorization header.
- If a server contract gap appears, document it in SDD before changing `lore-server`.

## Command Contract Summary

| Command | MVP behavior |
| --- | --- |
| `lore login` | Accept or prompt for server URL and API token, validate with `GET /v1/me`, then persist local config only after validation succeeds. |
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
# Expected MVP smoke shape once implemented
lore login --server https://example.test --token "$LORE_API_TOKEN"
lore status
lore doctor
lore logout
```

## Resources

- Lore artifacts: `sdd/create-lore-cli-mvp/init` and `sdd/create-lore-cli-mvp/explore`.
- Server reference repo: `/Users/alfonsocarmona/personal/lore2/lore-server`.
