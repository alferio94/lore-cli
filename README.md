# Lore CLI

Thin Go CLI MVP for Lore server authentication, diagnostics, and a default interactive TUI.

## Interactive entrypoint
- `lore` starts the interactive TUI by default.
- `lore tui` starts the same interactive TUI explicitly.
- `lore --help` stays non-interactive.
- Use explicit subcommands for automation and scripts.

The root TUI offers `Status`, `Login`, `Logout`, `Doctor`, `Quit`, and a disabled `Install` placeholder marked coming soon. Runtime-agent installation and version/release flows remain deferred and are intentionally out of scope in this change.

## Explicit commands
- `lore login --server https://example.test --token "$LORE_API_TOKEN"`
- `lore status`
- `lore logout`
- `lore doctor`

`login` validates the provided normal user API token with `GET /v1/me` before saving local config.
`status` reports config presence plus `/healthz`, `/readyz`, and `/v1/me` state.
`logout` removes local config only and does not revoke server-side tokens.
`doctor` prints actionable config, URL, network, readiness, auth, and Pi-availability diagnostics.

## Local config path
By default the CLI stores config under `os.UserConfigDir()/lore/config.json`.

Overrides for deterministic tests and local development:
- `LORE_CONFIG_DIR`
- injected config directory in code via `config.NewStore(...)`

## Token storage warning
The MVP stores one user API token in a local JSON config file with restrictive permissions (`0700` dir, `0600` file). This is a temporary tradeoff for simplicity and is less secure than OS keychain storage.

## Out of scope
This MVP intentionally excludes install/update/uninstall flows, Pi MCP automation, bootstrap-init login, keychain/SSO/browser auth, multi-profile storage, admin token issuance/revocation UX, and remote logout.
