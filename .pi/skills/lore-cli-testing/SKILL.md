---
name: lore-cli-testing
description: >
  Testing expectations for Lore CLI command, config, and HTTP-client behavior.
  Trigger: When writing, updating, or reviewing tests for lore-cli.
license: Apache-2.0
metadata:
  author: gentleman-programming
  version: "1.0"
---

## When to Use

- Adding unit or integration-style tests for `lore-cli`.
- Verifying login/status/logout/doctor behavior.
- Reviewing whether command behavior is covered without depending on live services.

## Critical Patterns

- Do not require the Railway or local `lore-server` service for normal tests.
- Use `httptest.Server` for HTTP contract tests.
- Use temporary directories/files for config tests; never touch the user's real config in tests.
- Assert that token values are not printed in stdout, stderr, logs, or error messages.
- Cover both success and actionable failure diagnostics.
- Keep command tests stable by injecting IO streams, config paths, and fake clients where possible.

## Minimum MVP Coverage

| Area | Cases |
| --- | --- |
| URL normalization | valid `http/https`, trailing slash removal, invalid schemes, empty URL. |
| Config store | save/load/delete, missing file, restrictive permissions where portable, token redaction. |
| `login` | validates via `/v1/me`, saves only on success, rejects bootstrap/unauthorized token responses. |
| `status` | no config, health ready, readiness failure, auth success, auth failure. |
| `logout` | idempotent local removal, clear message that server revocation was not performed. |
| `doctor` | config diagnostics, network failure, health/readiness status, token validity. |

## HTTP Test Pattern

```go
srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    switch r.URL.Path {
    case "/v1/me":
        if r.Header.Get("Authorization") != "Bearer test-token" {
            w.WriteHeader(http.StatusUnauthorized)
            _, _ = w.Write([]byte(`{"error":{"code":"unauthorized","message":"invalid token","request_id":"test"}}`))
            return
        }
        _, _ = w.Write([]byte(`{"data":{"id":"sub_1","user_id":"user_1","roles":["admin"],"token_id":"tok_1","token_source":"api_token","kind":"user"}}`))
    default:
        http.NotFound(w, r)
    }
}))
defer srv.Close()
```

## Commands

```bash
go test ./...
go test ./internal/config ./internal/httpclient ./internal/cli
```

## Resources

- Go implementation skill: `.pi/skills/lore-cli-go/SKILL.md`.
- Product behavior skill: `.pi/skills/lore-cli-mvp/SKILL.md`.
