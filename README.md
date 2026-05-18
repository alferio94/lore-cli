# Lore CLI

Thin Go CLI for Lore server authentication, diagnostics, a default interactive TUI, and releaseable tagged binaries.

## Interactive entrypoint
- `lore` starts the interactive TUI by default.
- `lore tui` starts the same interactive TUI explicitly.
- `lore --help` stays non-interactive.
- Use explicit subcommands for automation and scripts.

The root TUI offers `Status`, `Login`, `Logout`, `Doctor`, `Quit`, and a disabled `Install` placeholder marked coming soon. Version reporting is intentionally CLI-only in this slice.

## Explicit commands
- `lore login --server https://example.test --token "$LORE_API_TOKEN"`
- `lore status`
- `lore logout`
- `lore doctor`
- `lore version`
- `lore version --json`

`login` validates the provided normal user API token with `GET /v1/me` before saving local config.
`status` reports config presence plus `/healthz`, `/readyz`, and `/v1/me` state.
`logout` removes local config only and does not revoke server-side tokens.
`doctor` prints actionable config, URL, network, readiness, auth, and Pi-availability diagnostics.
`version` prints build metadata without requiring config, auth, or network access.

Default local `version` output:
- `lore version dev commit=none buildDate=unknown`

JSON output fields:
- `version`
- `commit`
- `buildDate`

## Local config path
By default the CLI stores config under `os.UserConfigDir()/lore/config.json`.

Overrides for deterministic tests and local development:
- `LORE_CONFIG_DIR`
- injected config directory in code via `config.NewStore(...)`

## Token storage warning
The current CLI stores one user API token in a local JSON config file with restrictive permissions (`0700` dir, `0600` file). This is a temporary tradeoff for simplicity and is less secure than OS keychain storage.

## Releases

### Supported release matrix
Initial tagged releases publish exactly these archives:
- `darwin/amd64`
- `darwin/arm64`
- `linux/amd64`
- `linux/arm64`

Windows assets are intentionally excluded from this first release slice.

### Tag policy
Releases are created only from annotated semantic version tags matching `vX.Y.Z`.

Example:
- `git tag -a v1.2.3 -m "v1.2.3"`
- `git push origin v1.2.3`

The GitHub Actions workflow then:
1. validates the tag shape and annotated-tag object type;
2. runs `go test ./...` as a release gate;
3. builds platform archives with injected version metadata;
4. publishes archives plus `SHA256SUMS` to the GitHub Release.

### Asset naming
Each supported target is published as:
- `lore-cli_<tag>_<os>_<arch>.tar.gz`

Examples:
- `lore-cli_v1.2.3_darwin_arm64.tar.gz`
- `lore-cli_v1.2.3_linux_amd64.tar.gz`

Each archive contains a single executable named `lore`.

### Build metadata injection
Release builds inject metadata with Go ldflags targeting:
- `github.com/alferio94/lore-cli/internal/version.Version`
- `github.com/alferio94/lore-cli/internal/version.Commit`
- `github.com/alferio94/lore-cli/internal/version.BuildDate`

Local builds keep the defaults:
- version: `dev`
- commit: `none`
- build date: `unknown`

### Download and verify
Example install flow for a Linux amd64 release:

```sh
curl -LO https://github.com/alferio94/lore-cli/releases/download/v1.2.3/lore-cli_v1.2.3_linux_amd64.tar.gz
curl -LO https://github.com/alferio94/lore-cli/releases/download/v1.2.3/SHA256SUMS
grep 'lore-cli_v1.2.3_linux_amd64.tar.gz' SHA256SUMS | sha256sum -c -
tar -xzf lore-cli_v1.2.3_linux_amd64.tar.gz
./lore version
```

## Out of scope
This slice intentionally excludes:
- `lore update` or any self-update network logic;
- runtime-agent installation flows beyond the disabled placeholder;
- code signing, notarization, or provenance attestation beyond SHA256 checksums;
- initial Windows release assets;
- keychain/SSO/browser auth, multi-profile storage, admin token issuance or revocation UX, and remote logout.
