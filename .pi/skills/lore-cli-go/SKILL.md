---
name: lore-cli-go
description: >
  Go implementation conventions for the Lore CLI greenfield repository.
  Trigger: When scaffolding, implementing, refactoring, or reviewing Go code in lore-cli.
license: Apache-2.0
metadata:
  author: gentleman-programming
  version: "1.0"
---

## When to Use

- Creating the initial `lore-cli` Go module and command structure.
- Implementing command handlers, config persistence, or the `lore-server` HTTP client.
- Reviewing Go code for CLI ergonomics, testability, and secret safety.

## Critical Patterns

- Keep command behavior thin and testable: parse inputs at the edge, then call small internal packages.
- Prefer packages that can be tested without invoking a real binary or network.
- Keep the HTTP client small and typed around only the MVP endpoints: `/healthz`, `/readyz`, and `/v1/me`.
- Normalize server URLs before saving config: trim whitespace, require `http` or `https`, remove trailing slash for the stored base URL.
- Never log or print tokens. Redact token values in errors and diagnostics.
- Use context-aware HTTP requests and finite timeouts.
- Keep generated or machine-local config out of git.

## Suggested Repository Shape

```text
lore-cli/
├── cmd/lore/              # main package
├── internal/cli/          # command construction and command handlers
├── internal/config/       # config paths, load/save/delete, redaction
├── internal/httpclient/   # health, readiness, and /v1/me client
├── internal/output/       # human-readable output helpers if needed
├── go.mod
└── README.md
```

## Local Config Guidelines

- MVP may use a user config file with restrictive permissions; document the security tradeoff.
- Store only the normalized server URL and token needed for API access.
- Prefer deterministic config path behavior that can be overridden in tests via env var or injected path.
- `logout` should be idempotent: missing config should not be a hard failure.

## Code Examples

```go
// Keep client methods narrow and easy to fake in tests.
type Client interface {
    Health(ctx context.Context) error
    Ready(ctx context.Context) error
    Me(ctx context.Context, token string) (Subject, error)
}
```

```go
// Never include raw token values in formatted output.
func RedactToken(token string) string {
    if token == "" {
        return "<missing>"
    }
    return "<redacted>"
}
```

## Commands

```bash
go mod init github.com/gentleman-programming/lore-cli
go test ./...
go run ./cmd/lore --help
```

## Resources

- Product behavior skill: `.pi/skills/lore-cli-mvp/SKILL.md`.
- Test guidance skill: `.pi/skills/lore-cli-testing/SKILL.md`.
