#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"
WORK_DIR="$(mktemp -d)"; PROFILE=".github/release-profiles/.task51-test-$$.json"
cleanup() { rm -rf "$WORK_DIR" "$PROFILE"; }
trap cleanup EXIT

ruby -e 'require "yaml"; YAML.parse_file(ARGV.fetch(0))' .github/workflows/release.yml && bash -n scripts/release-profile-ldflags.sh
python3 -c 'import json,re,pathlib; text=pathlib.Path(".github/workflows/release.yml").read_text(); pattern="printf "+chr(39)+r"(\{.*?\})\\n"+chr(39); records=re.findall(pattern,text); assert len(records)==2; [json.loads(record.replace("%s","x")) for record in records]'
! grep -Eq '^[[:space:]]+push:' .github/workflows/release.yml
grep -Fq 'workflow_dispatch:' .github/workflows/release.yml && grep -Fq 'default: false' .github/workflows/release.yml
grep -Fq 'contents: read' .github/workflows/release.yml
[[ "$(grep -Fc 'contents: write' .github/workflows/release.yml)" -eq 1 ]]
grep -Fq 'needs: [build, attest]' .github/workflows/release.yml
grep -Fq 'attestations: write' .github/workflows/release.yml
grep -Fq 'target_integration":"not-tested' .github/workflows/release.yml && grep -Fq 'adoption":"not-measured' .github/workflows/release.yml && grep -Fq '"race_linux_amd64":"passed"' .github/workflows/release.yml
! grep -Eq 'continue-on-error|set -x|go version -m|echo.*(GH_TOKEN|PROFILE_LDFLAGS)' .github/workflows/release.yml
awk '/uses:/ && $0 !~ /@[0-9a-f]{40}/ { exit 1 }' .github/workflows/release.yml

python3 - "$PROFILE" <<'PY'
import json, sys
profile = {"schema":"lore.release-profile/v1","id":"task51-all-off","version":1,
 "release":{"version":"v9.9.8","channel":"stable","artifact_sha256":"b"*64},
 "rollback":{"version":"v9.9.7","profile_id":"default-off"},
 "gates":{"pi":"off","opencode":"off","codex":"off","antigravity":"off"}}
open(sys.argv[1], "w", encoding="utf-8").write(json.dumps(profile, separators=(",", ":")))
PY
./scripts/release-profile-ldflags.sh --guard "$PROFILE" v9.9.8 task51-all-off >/dev/null
LDFLAGS="$(./scripts/release-profile-ldflags.sh "$PROFILE")"
go build -trimpath -ldflags "-X=github.com/alferio94/lore-cli/internal/version.Version=v9.9.8 $LDFLAGS" -o "$WORK_DIR/lore" ./cmd/lore
./scripts/release-profile-ldflags.sh --verify-static "$PROFILE" "$WORK_DIR/lore" >/dev/null
./scripts/release-profile-ldflags.sh --verify "$PROFILE" v9.9.8 task51-all-off "$WORK_DIR/lore" >/dev/null
if ./scripts/release-profile-ldflags.sh --guard .github/release-profiles/prerelease-opencode-e.json v0.3.0-rc.1 prerelease-opencode-e >/dev/null 2>&1; then
  echo "task 5.1 unexpectedly authorized the prerelease profile" >&2
  exit 1
fi
echo "release workflow guards passed"
