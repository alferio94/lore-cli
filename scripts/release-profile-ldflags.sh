#!/bin/sh
set -eu

usage() {
  echo "usage: $0 PROFILE.json | $0 --guard PROFILE.json TAG PROFILE_ID | $0 --verify-static PROFILE.json BINARY | $0 --verify PROFILE.json TAG PROFILE_ID BINARY" >&2
  exit 2
}

validate_identity() {
  profile=$1 tag=$2 profile_id=$3
  case "$profile" in .github/release-profiles/*.json) ;; *) echo "release profile path is not authorized" >&2; exit 1 ;; esac
  go run ./internal/releaseprofile/cmd/profileldflags "$profile" >/dev/null
  python3 - "$profile" "$tag" "$profile_id" <<'PY'
import json, re, sys
path, tag, expected_id = sys.argv[1:]
try:
    profile = json.load(open(path, encoding="utf-8"))
except Exception:
    raise SystemExit("release profile identity validation failed")
release = profile.get("release", {})
gates = profile.get("gates", {})
safe_path = re.fullmatch(r"\.github/release-profiles/[A-Za-z0-9._-]+\.json", path)
safe_id = re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9._+-]{0,127}", expected_id or "")
digest = release.get("artifact_sha256", "")
if (not safe_path or not safe_id or profile.get("id") != expected_id or release.get("version") != tag or
        not re.fullmatch(r"[0-9a-f]{64}", digest) or release.get("channel") != "stable" or
        any(value != "off" for value in gates.values())):
    raise SystemExit("release profile is not authorized for task 5.1 publication")
PY
}

case "${1-}" in
  --guard)
    [ "$#" -eq 4 ] || usage
    validate_identity "$2" "$3" "$4"
    echo "release profile authorization verified"
    ;;
  --verify-static)
    [ "$#" -eq 3 ] || usage
    ldflags=$(go run ./internal/releaseprofile/cmd/profileldflags "$2")
    for assignment in $ldflags; do
      value=${assignment#*=}; value=${value#*=}
      grep -aFq -- "$value" "$3" || { echo "built binary profile embedding verification failed" >&2; exit 1; }
    done
    echo "built binary profile embedding verified"
    ;;
  --verify)
    [ "$#" -eq 5 ] || usage
    validate_identity "$2" "$3" "$4"
    identity=$("$5" version --json)
    IDENTITY="$identity" python3 - "$2" <<'PY'
import json, os, sys
profile = json.load(open(sys.argv[1], encoding="utf-8"))
actual = json.loads(os.environ["IDENTITY"]).get("release_profile", {})
expected = {"schema": profile["schema"], "id": profile["id"], "version": profile["version"],
            "release": profile["release"]["version"], "channel": profile["release"]["channel"],
            "artifact_sha256": profile["release"]["artifact_sha256"], "provenance_status": "valid",
            "gates": profile["gates"]}
if actual != expected:
    raise SystemExit("built binary release profile verification failed")
PY
    echo "built binary release profile verified"
    ;;
  *)
    [ "$#" -eq 1 ] || usage
    exec go run ./internal/releaseprofile/cmd/profileldflags "$1"
    ;;
esac
