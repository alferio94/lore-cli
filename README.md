# Lore CLI

Thin Go CLI for Lore server authentication, diagnostics, a default interactive TUI, and releaseable tagged binaries with first-party install scripts.

## Interactive entrypoint
- `lore` starts the interactive TUI by default.
- `lore tui` starts the same interactive TUI explicitly.
- `lore --help` stays non-interactive.
- Use explicit subcommands for automation and scripts.

The root TUI offers `Status`, `Login`, `Logout`, `Doctor`, `Install`, `Update`, and `Quit`. `Install` is Pi-first: Pi is selectable and recommended, Codex is supported as a config-only projection target, Antigravity is the supported prompt-and-skills MVP target, and Claude Code/OpenCode stay visible as roadmap-only `Coming soon` targets. `Update` can surface binary update availability in the background and asks for confirmation before running the binary-only CLI updater.

## Explicit commands
- `lore login --server https://example.test --email admin@example.com`
- `printf '%s\n' '<password-from-secret-store>' | lore login --server https://example.test --email admin@example.com --password-stdin`
- `lore login --server https://example.test --token "$LORE_API_TOKEN"` (compatibility mode)
- `lore status`
- `lore logout`
- `lore doctor`
- `lore install`
- `lore install --dry-run --target pi --component pi-extensions`
- `lore remember --project-id <project-id> --type decision --title "Ship it" --content "..."`
- `lore recall --project-id <project-id> --type decision --limit 10`
- `lore version`
- `lore version --json`
- `lore update`
- `lore update --dry-run`
- `lore update --yes`

`login` uses email + hidden password by default to mint a reusable API token with `POST /v1/auth/login`, validates the minted token with `GET /v1/me`, and saves metadata-only local config plus the token in the OS keychain. `--token` remains available as an older-server compatibility mode.
`status` reports saved login metadata presence plus `/healthz`, `/readyz`, and `/v1/me` state.
`logout` removes local login metadata plus the matching OS keychain credential only and does not revoke server-side tokens.
`doctor` prints actionable config, URL, network, readiness, auth, and Pi-availability diagnostics.
`install` reuses healthy saved Lore login state, runs the same config `/healthz` `/readyz` `/v1/me` preflight as `status`, and keeps Pi as the default recommended target. The current Pi slice uses hosted Lore MCP via `pi-mcp-adapter` as the default backend: it installs the portable Lore agent pack into the managed `~/.pi/agent` Pi runtime files, manages `settings.json` with the pinned hosted MCP package and `mcp.json` with HTTP MCP configuration pointing to the hosted Lore MCP endpoint (`<lore_url>/v1/mcp`) with a bearer token materialized in plaintext on disk (matching the Antigravity local plaintext-token tradeoff), and preserves existing package order/other entries while keeping `lore-install.json` as bookkeeping-only metadata rather than a runtime source. The default install does NOT include `extensions/lore-memory.ts` or `extensions/lore-footer.ts` by default — those Lore extensions assets remain available in the repository for rollback or explicit testing via `lore install --target pi --component pi-extensions`, but are dormant for standard installs. A default complete install also renders an extended-skills bundle (skill-creator, skill-registry, judgment-day) into `skills/<name>/SKILL.md`; these are installer-managed CLI convenience skills separate from core-pack agents, and `lore install` reruns reconcile the bundle while `lore update` does not touch skill files or runtime content. It also renders managed global agent overlays under `~/.pi/agent/agents/lore-managed-*.md`; runtime resolution is `builtin < managed < user`; user-owned collisions are reported and left untouched; and stale managed overlays are backed up before delete/update so rollback can restore them from the managed backup root. Lore-managed installs ignore project `.pi/agents` by default at runtime; opt in explicitly with `settings.json.lore.agent_resolution.project_agents=enabled` when you really want project-local agents back in play. When a legacy managed `extensions/lore-delegation.ts` exists from an older install, `lore install` reports a scoped cleanup action, backs that file up under the managed backup root, and deletes only that obsolete path during apply. Antigravity is the supported prompt-and-skills MVP target and includes the same extended-skills bundle; the contract is prompt append/merge plus managed skills, and `lore-cli`/`internal/agentpack` is the source of truth for the managed persona, config, and skills that Lore installs. `lore install` writes the managed Gemini agent profile to `~/.gemini/config/agents/lore.json`; its English `description`, `default: true`, and generated `systemInstruction` come from the portable agentpack plus a small Antigravity/Gemini runtime suffix rather than from a separate hand-maintained prompt asset. The Gemini profile intentionally omits a fixed `tools` field because Lore MCP tools are exposed by server role and permissions. Optional Antigravity MCP writes direct remote config to `~/.gemini/config/mcp_config.json` with `mcpServers.lore.serverUrl = ${lore_url}/v1/mcp` and `headers.Authorization = Bearer <saved-token>`. That header is plaintext on disk because current Gemini/Antigravity compatibility requires it, so Lore keeps the tradeoff explicit and limits it to the local Gemini MCP config instead of Pi-managed files. Pi-style overlay emulation is out of scope, and no auto-install, daemon, or autostart guarantee is claimed. Codex is supported as a config-only install target that writes Lore-managed `~/.codex/agents.md`, skills, and manifest state with backup-before-overwrite semantics; it does not configure Lore MCP, install Codex, run `codex exec`, or claim live subagent execution. Claude Code and OpenCode remain visible as `Coming soon` roadmap targets.
`remember` creates one memory with explicit REST fields only; `--project-id`, `--type`, `--title`, and `--content` are required, `--scope` defaults to `project`, `--metadata-json` must be a JSON object, and `--json` prints `{\"data\": {...}}`.
`recall` lists memories by explicit filters only; `--project-id` is required, optional filters are `--type`, `--scope`, and `--limit`, semantic/full-text search is out of scope, and `--json` prints `{\"data\": [...]}`.
`version` prints build metadata without requiring config, auth, or network access.
`update` checks GitHub Releases for the latest matching Lore CLI archive and updates only the active Lore CLI binary. It does not mutate the Pi runtime (`~/.pi`), extensions, settings, extended-skills bundle, themes, or model/provider config. Use `lore install` (with no component overrides) to refresh the extended-skills bundle and managed content. `--dry-run` prints the plan without mutation, and `--yes` skips the interactive confirmation prompt after the same safety checks pass.

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

## Agent config contract
The CLI uses a second config file, `agent-config.json`, as a sibling to `config.json` under the same Lore config directory (`os.UserConfigDir()/lore/agent-config.json`, or `$LORE_CONFIG_DIR/agent-config.json` when overridden).

`agent-config.json` stores the canonical SDD agent model contract in a secret-free, versioned JSON schema (`schema_version: 1`). It is **not** coupled to auth-owned `config.json` and is not written by `lore login` or `lore logout`. The schema declares all canonical SDD phases (`sdd-init` through `sdd-archive`) with an initial model of `gpt-5.4`. The file is configuration-only: it does **not** enable live Codex execution, subagent invocation, or a Codex runner.

The install, status, and doctor commands perform read-only diagnostics against `agent-config.json`: they report the file path, schema version, and declared agent count. Codex install consumes this contract for config-only projection, but these diagnostics do not execute agents or imply Codex runner/MCP support.

## Saved login state
The CLI stores non-secret login metadata in `config.json` with restrictive permissions (`0700` dir, `0600` file) and stores the user API token in the OS keychain. Raw API tokens are not stored in `config.json` or install manifests; the only files Lore CLI writes with the API token in plaintext are `~/.pi/agent/mcp.json` (Pi hosted MCP config) and `~/.gemini/config/mcp_config.json` (Antigravity MCP config), both matching the local plaintext-token tradeoff that Antigravity/Gemini requires for direct HTTP header-in-file MCP configuration.

Linux/headless environments must provide a working Secret Service/keyring session. If the credential backend is unavailable, `login`, `status`, `doctor`, `remember`, `recall`, hidden broker calls, and `install` fail closed with remediation instead of falling back to plaintext token storage.

## Memory command smoke flow
Use the CLI memory MVP only after `lore login` succeeds and when you already know the target `project_id`.

```sh
lore login --server https://example.test --email admin@example.com
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
- `remember`, `recall`, and `install` reuse the saved server URL plus the API token resolved from the OS keychain.
- Default Pi install uses hosted Lore MCP via `pi-mcp-adapter`; the installer writes `~/.pi/agent/settings.json` with a pinned immutable git reference (`git:github.com/nicobailon/pi-mcp-adapter@<sha>`) and `~/.pi/agent/mcp.json` with HTTP MCP configuration pointing to the hosted Lore MCP endpoint (`<lore_url>/v1/mcp`) with bearer-token auth materialized in plaintext. lore-memory assets (extensions/lore-memory.ts, extensions/lore-footer.ts) remain available for rollback or explicit testing via `lore install --target pi --component pi-extensions` but are not installed by default. Pi mcp.json materializes the bearer token in plaintext on disk, matching the Antigravity local plaintext-token tradeoff.
- `lore install` blocks before any Pi writes when saved login metadata is missing, invalid, unhealthy, or cannot reach the keychain, and surfaces remediation guidance instead.
- Existing `settings.json.packages` entries are preserved additively; Lore CLI appends the hosted MCP package only when it is absent.
- Managed global overlays are installer-owned only when tracked in `lore-install.json`; a user-created file at the same managed path is reported as a conflict and is never clobbered.
- Runtime precedence for shared identities is `builtin < managed < user`; project `.pi/agents` stay disabled by default for Lore-managed installs unless `settings.json.lore.agent_resolution.project_agents=enabled` is set explicitly.
- Existing legacy `extensions/lore-delegation.ts` files are treated as cleanup-only migration artifacts: dry-runs report the delete, applies back it up and remove it, and reruns do not regenerate it.
- Managed overlay rollback is backup-first: updates and deletes copy the prior managed content into the managed backup root before mutation so operators can restore and rerun install to reconverge.
- Human output is concise and omits raw `content`, `metadata`, and secrets.
- Request failures surface request IDs when the server provides them.
- `lore api request` is a hidden machine broker for allowlisted authenticated REST calls used by the managed Pi runtime, including project-context fetches via `/v1/projects/{id}/context`; this Pi path is separate from Antigravity's direct remote MCP config.
- Single-memory fetch, non-`--body-json` broker body input modes, project lookup UX, broad non-Pi runtime installs, and semantic search are intentionally out of scope for this MVP.
- Antigravity install writes the managed Gemini agent profile to `~/.gemini/config/agents/lore.json` and, when the optional MCP component is selected, writes `~/.gemini/config/mcp_config.json` with `mcpServers.lore.serverUrl = <lore-url>/v1/mcp` plus `headers.Authorization = Bearer <saved-token>`; rerun `lore install` after login or server changes.

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

Windows PowerShell 5.1 or PowerShell 7+ on Windows (prefer a pinned release tag):

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
- `install.ps1 -Version latest` (convenience path; pinned tags are safer on older locked-down Windows hosts)

After install:
- both installers run a direct version check before reporting success
- run the binary immediately from the printed install path (`~/.local/bin/lore` or `%LOCALAPPDATA%\Programs\Lore\lore.exe`) if you did not opt into PATH
- if you used `--add-to-path` or `-AddToPath`, open a new terminal/session before running `lore` by name
- if you skipped PATH opt-in, rerun the installer with the flag later or add the printed path yourself

The installers always re-download the selected release, verify checksums, replace the target binary idempotently, and run `lore version` / `lore.exe version` before reporting success.

### Binary-only self-update
- `lore update` updates only the active Lore CLI binary.
- It does not touch Pi runtime files under `~/.pi`, extensions, settings, skills, themes, or model/provider config.
- `lore update --dry-run` shows the current/latest/target plan without mutating the binary.
- `lore update --yes` stays non-interactive after safety checks pass; it does not bypass refusal conditions.
- The TUI can show update availability in the background and lets you select `Update`, then confirm before the updater runs.
- Safety checks fail closed on GitHub release lookup errors, unsupported/dev versions, PATH mismatches, symlinked or otherwise unsafe targets, missing release assets, checksum mismatches, and Windows self-update.
- On supported Unix targets, the updater verifies the selected release archive against `SHA256SUMS` before any replacement attempt. Backup/rollback details are surfaced only when the updater reports a backup path.
- On Windows, automatic self-update is intentionally unsupported in this slice; download the matching release archive and replace `lore.exe` manually after exiting Lore CLI.

### Manual uninstall and config retention
- macOS/Linux: delete `~/.local/bin/lore` or your custom `--bin-dir` target.
- Windows: delete `%LOCALAPPDATA%\Programs\Lore\lore.exe` or your custom `-InstallDir` target.
- Login metadata is preserved by default under `os.UserConfigDir()/lore/config.json`; use `lore logout` if you also want to remove the matching OS keychain credential.

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
6. publishes archives, installer scripts, and `SHA256SUMS` to the GitHub Release;
7. uses `docs/releases/<tag>.md` as the GitHub Release body when that file exists, otherwise falls back to generated release notes.

Release notes convention:
- add an optional file at `docs/releases/vX.Y.Z.md` before pushing the annotated tag;
- example: `docs/releases/v0.2.5.md` will become the Release body for tag `v0.2.5`;
- if the file is absent, the workflow still succeeds and GitHub auto-generates the notes.

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
- Package managers, code signing, notarization, and automatic Windows self-update remain out of scope for this slice.

## Out of scope
This slice intentionally excludes:
- runtime-agent installation flows beyond the Pi-first managed install path, the prompt-and-skills-first Antigravity MVP contract, and the portable pack components already supported today; broader non-Pi parity, Pi-style overlay emulation for Antigravity, and auto-install guarantees remain out of scope;
- code signing, notarization, provenance attestation, MSI/installer packaging, or other signing/distribution automation beyond SHA256 checksums;
- automatic Windows self-update and package-manager integration;
- renaming existing macOS/Linux release assets;
- keychain/SSO/browser auth, multi-profile storage, admin token issuance or revocation UX, and remote logout.
