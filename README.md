# Lore CLI

Thin Go CLI for Lore server authentication, diagnostics, a default interactive TUI, and releaseable tagged binaries with first-party install scripts.

## Interactive entrypoint
- `lore` starts the interactive TUI by default.
- `lore tui` starts the same interactive TUI explicitly.
- `lore --help` stays non-interactive.
- Use explicit subcommands for automation and scripts.

The root TUI offers `Status`, `Login`, `Logout`, `Doctor`, `Install`, and `Quit`. `Install` is Pi-first: Pi is selectable and recommended, while Claude Code, OpenCode, Codex, and Antigravity stay visible as non-selectable `Coming soon` targets. Version reporting is intentionally CLI-only in this slice.

## Explicit commands
- `lore login --server https://example.test --token "$LORE_API_TOKEN"`
- `lore status`
- `lore logout`
- `lore doctor`
- `lore install`
- `lore remember --project-id <project-id> --type decision --title "Ship it" --content "..."`
- `lore recall --project-id <project-id> --type decision --limit 10`
- `lore version`
- `lore version --json`

`login` validates the provided normal user API token with `GET /v1/me` before saving local config.
`status` reports config presence plus `/healthz`, `/readyz`, and `/v1/me` state.
`logout` removes local config only and does not revoke server-side tokens.
`doctor` prints actionable config, URL, network, readiness, auth, and Pi-availability diagnostics.
`install` reuses healthy saved Lore login state, runs the same config `/healthz` `/readyz` `/v1/me` preflight as `status`, installs only the managed `~/.pi/agent` Pi runtime files, and writes non-secret `~/.pi/agent/lore-install.json` metadata. Generated Pi assets call the hidden `lore api request` broker so no raw API token is written into Pi files.
`remember` creates one memory with explicit REST fields only; `--project-id`, `--type`, `--title`, and `--content` are required, `--scope` defaults to `project`, `--metadata-json` must be a JSON object, and `--json` prints `{\"data\": {...}}`.
`recall` lists memories by explicit filters only; `--project-id` is required, optional filters are `--type`, `--scope`, and `--limit`, semantic/full-text search is out of scope, and `--json` prints `{\"data\": [...]}`.
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

## Memory command smoke flow
Use the CLI memory MVP only after `lore login` succeeds and when you already know the target `project_id`.

```sh
lore login --server https://example.test --token "$LORE_API_TOKEN"
lore remember \
  --project-id prj_123 \
  --type decision \
  --title "Ship installer smoke fix" \
  --content "PowerShell file:// fixtures now copy locally" \
  --metadata-json '{"area":"release","kind":"bugfix"}'
lore recall --project-id prj_123 --type decision --limit 10
lore recall --project-id prj_123 --json
lore logout
lore status # reports no saved config / unauthenticated after logout
```

Notes:
- `remember` requires `--content`; positional memory content is not accepted in this MVP.
- `remember`, `recall`, and `install` reuse the saved server URL and API token from `login`.
- `install` blocks before any Pi writes when saved config is missing, invalid, or unhealthy, and surfaces login/remediation guidance instead.
- Human output is concise and omits raw `content`, `metadata`, and secrets.
- Request failures surface request IDs when the server provides them.
- `lore api request` is a hidden machine broker for allowlisted authenticated API calls used by the managed Pi runtime.
- Single-memory fetch, non-`--body-json` broker body input modes, project lookup UX, MCP transport, semantic search, and `lore update` are intentionally out of scope for this MVP.

## Releases

### Supported release matrix
Tagged releases publish exactly these platform archives plus installer scripts and `SHA256SUMS`:
- `darwin/amd64`
- `darwin/arm64`
- `linux/amd64`
- `linux/arm64`
- `windows/amd64`
- `windows/arm64`
- `install.sh`
- `install.ps1`

### Recommended scripted install
Pinned release asset URLs are the primary documented path.

macOS/Linux:

```sh
curl -fsSL https://github.com/alferio94/lore-cli/releases/download/v1.2.3/install.sh | sh
```

Windows PowerShell 5.1 or PowerShell 7+ on Windows:

```powershell
powershell -ExecutionPolicy Bypass -c "irm https://github.com/alferio94/lore-cli/releases/download/v1.2.3/install.ps1 | iex"
```

Defaults:
- macOS/Linux installs `lore` to `~/.local/bin/lore`
- Windows installs `lore.exe` to `%LOCALAPPDATA%\Programs\Lore\lore.exe`
- the installer verifies the selected archive against the release `SHA256SUMS`
- PATH is not modified unless you opt in with `--add-to-path` or `-AddToPath`
- no interactive PATH prompt is shown, so piped and CI installs stay non-blocking

Useful overrides:
- `install.sh --version v1.2.3 --bin-dir "$HOME/bin" --add-to-path`
- `install.sh --version latest` (secondary convenience path; pinned tags remain recommended)
- `install.ps1 -Version v1.2.3 -InstallDir "$env:LOCALAPPDATA\Programs\Lore" -AddToPath`
- `install.ps1 -Version latest`

After install:
- both installers run a direct version check before reporting success
- run the binary immediately from the printed install path (`~/.local/bin/lore` or `%LOCALAPPDATA%\Programs\Lore\lore.exe`) if you did not opt into PATH
- if you used `--add-to-path` or `-AddToPath`, open a new terminal/session before running `lore` by name
- if you skipped PATH opt-in, rerun the installer with the flag later or add the printed path yourself

The installers always re-download the selected release, verify checksums, replace the target binary idempotently, and run `lore version` / `lore.exe version` before reporting success.

### Manual uninstall and config retention
- macOS/Linux: delete `~/.local/bin/lore` or your custom `--bin-dir` target.
- Windows: delete `%LOCALAPPDATA%\Programs\Lore\lore.exe` or your custom `-InstallDir` target.
- Config is preserved by default under `os.UserConfigDir()/lore/config.json`; removing config is a separate optional cleanup step.

### Tag policy
Releases are created only from annotated semantic version tags matching `vX.Y.Z`.

Example:
- `git tag -a v1.2.3 -m "v1.2.3"`
- `git push origin v1.2.3`

The GitHub Actions workflow then:
1. validates the tag shape and annotated-tag object type;
2. runs `go test ./...` as a release gate;
3. validates installer syntax and runs Unix installer smoke tests;
4. builds platform archives with injected version metadata;
5. renders `install.sh` and `install.ps1` with the tag embedded as their default version;
6. publishes archives, installer scripts, and `SHA256SUMS` to the GitHub Release.

### Asset naming
Supported targets are published as:
- macOS/Linux: `lore-cli_<tag>_<os>_<arch>.tar.gz`
- Windows: `lore-cli_<tag>_windows_<arch>.zip`
- Installers: `install.sh`, `install.ps1`

Examples:
- `lore-cli_v1.2.3_darwin_arm64.tar.gz`
- `lore-cli_v1.2.3_linux_amd64.tar.gz`
- `lore-cli_v1.2.3_windows_amd64.zip`

Archive contents:
- macOS/Linux archives contain a single executable named `lore`
- Windows archives contain a single executable named `lore.exe`

### Build metadata injection
Release builds inject metadata with Go ldflags targeting:
- `github.com/alferio94/lore-cli/internal/version.Version`
- `github.com/alferio94/lore-cli/internal/version.Commit`
- `github.com/alferio94/lore-cli/internal/version.BuildDate`

Local builds keep the defaults:
- version: `dev`
- commit: `none`
- build date: `unknown`

### Security notes
- Remote script execution is a convenience tradeoff; pinned URLs are preferred over mutable branch-tip URLs.
- `SHA256SUMS` verification provides release-asset integrity checks but does not replace signing or notarization.
- `lore update`, package managers, code signing, and notarization remain out of scope for this slice.

## Out of scope
This slice intentionally excludes:
- `lore update` or any self-update network logic;
- runtime-agent installation flows beyond the Pi-first managed install path and visible `Coming soon` placeholders for other clients;
- code signing, notarization, provenance attestation, MSI/installer packaging, or other signing/distribution automation beyond SHA256 checksums;
- renaming existing macOS/Linux release assets;
- keychain/SSO/browser auth, multi-profile storage, admin token issuance or revocation UX, and remote logout.
