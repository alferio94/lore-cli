#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"
WORK_DIR="$(mktemp -d)"; PROFILE=".github/release-profiles/.task51-test-$$.json"
cleanup() { rm -rf "$WORK_DIR" "$PROFILE"; }
trap cleanup EXIT

ruby -e 'require "yaml"; YAML.parse_file(ARGV.fetch(0))' .github/workflows/release.yml
bash -n scripts/release-profile-ldflags.sh scripts/release-promotion-guard.sh
python3 -c 'import json,re,pathlib; text=pathlib.Path(".github/workflows/release.yml").read_text(); pattern="printf "+chr(39)+r"(\{.*?\})\\n"+chr(39); records=re.findall(pattern,text); assert len(records)==2; [json.loads(record.replace("%s","x")) for record in records]'
! grep -Eq '^[[:space:]]+push:' .github/workflows/release.yml
grep -Fq 'workflow_dispatch:' .github/workflows/release.yml && grep -Fq 'default: false' .github/workflows/release.yml
[[ "$(grep -Fc 'required: true' .github/workflows/release.yml)" -eq 8 ]]
grep -Fq 'contents: read' .github/workflows/release.yml
[[ "$(grep -Fc 'contents: write' .github/workflows/release.yml)" -eq 1 ]]
grep -Fq 'needs: [build, attest]' .github/workflows/release.yml
grep -Fq 'attestations: write' .github/workflows/release.yml
grep -Fq 'actions/attest-build-provenance@' .github/workflows/release.yml
grep -Fq 'sha256sum "${assets[@]}" > SHA256SUMS' .github/workflows/release.yml
grep -Fq 'sha256sum -c SHA256SUMS' .github/workflows/release.yml
grep -Fq 'target_integration":"not-tested' .github/workflows/release.yml && grep -Fq 'adoption":"not-measured' .github/workflows/release.yml && grep -Fq '"race_linux_amd64":"passed"' .github/workflows/release.yml
! grep -Eq 'continue-on-error|set -x|go version -m|echo.*(GH_TOKEN|PROFILE_LDFLAGS)' .github/workflows/release.yml
! grep -Eq '(echo|printf).*(\$CANARY_AUDIENCE|\$ACCOUNTABLE_OWNER|\$PROMOTION_EVIDENCE|\$REMOTE_AUTHORIZATION|\$GH_TOKEN|\$PROFILE_LDFLAGS)' .github/workflows/release.yml
awk '/uses:/ && $0 !~ /@[0-9a-f]{40}/ { exit 1 }' .github/workflows/release.yml

CHECKPOINT="$WORK_DIR/operator-checkpoint.sh"
ruby -ryaml -e 'File.write(ARGV[1], YAML.load_file(ARGV[0]).fetch("jobs").fetch("validate").fetch("steps").first.fetch("run"))' .github/workflows/release.yml "$CHECKPOINT"
run_checkpoint() {
  CANARY_AUDIENCE=$1 OBSERVATION_WINDOW=$2 ACCOUNTABLE_OWNER=$3 REMAINING_TARGET_ORDER=$4 \
    PROMOTION_STAGE=${PROMOTION_STAGE-stable} PROMOTION_EVIDENCE=${PROMOTION_EVIDENCE-docs/rollout/evidence/test.json} \
    PROMOTION_EVALUATED_AT=${PROMOTION_EVALUATED_AT-2026-01-20T00:00:00Z} REMOTE_AUTHORIZATION=${REMOTE_AUTHORIZATION-auth-current} \
    AUTHORIZED=${AUTHORIZED-true} TAG=v9.9.8 PROFILE=.github/release-profiles/development-all-off.json PROFILE_ID=test-profile bash "$CHECKPOINT"
}
expect_checkpoint_failure() {
  expected=$1; shift
  if output="$(run_checkpoint "$@" 2>&1)"; then
    echo "operator checkpoint unexpectedly accepted invalid input" >&2
    exit 1
  fi
  [[ "$output" == "operator checkpoint invalid: $expected" ]]
}
expect_checkpoint_failure canary-audience '' 24h test-owner pi,codex,antigravity
expect_checkpoint_failure observation-window test-canary '' test-owner pi,codex,antigravity
expect_checkpoint_failure accountable-owner test-canary 24h '' pi,codex,antigravity
expect_checkpoint_failure remaining-target-order test-canary 24h test-owner ''
expect_checkpoint_failure canary-audience 'INVALID=fixture' 24h test-owner pi,codex,antigravity
expect_checkpoint_failure observation-window test-canary 0h test-owner pi,codex,antigravity
expect_checkpoint_failure accountable-owner test-canary 24h 'Owner Name' pi,codex,antigravity
expect_checkpoint_failure remaining-target-order test-canary 24h test-owner pi,pi,antigravity
expect_checkpoint_failure remaining-target-order test-canary 24h test-owner pi,claude,antigravity
expect_checkpoint_failure remaining-target-order test-canary 24h test-owner codex,pi,antigravity
PROMOTION_STAGE='' expect_checkpoint_failure promotion-stage test-canary 24h test-owner pi,codex,antigravity
PROMOTION_EVIDENCE='' expect_checkpoint_failure promotion-evidence test-canary 24h test-owner pi,codex,antigravity
REMOTE_AUTHORIZATION='' expect_checkpoint_failure remote-authorization test-canary 24h test-owner pi,codex,antigravity
output="$(AUTHORIZED=false run_checkpoint test-canary 24h test-owner pi,codex,antigravity 2>&1)" && exit 1
[[ "$output" == 'release authorization is required' ]]
[[ -z "$(run_checkpoint test-canary 24h test-owner pi,codex,antigravity)" ]]

PROMOTION="$WORK_DIR/promotion.json"; PROMOTION_PROFILE="$WORK_DIR/promotion-profile.json"
make_evidence() { python3 - "$PROMOTION" "$PROMOTION_PROFILE" "$1" "${2:-valid}" <<'PY'
import json,sys
from datetime import datetime,timedelta,timezone
path,profile_path,current,mutation=sys.argv[1:]
seq=['readiness','opencode-e-canary','e-observation','e-pi','e-codex','e-antigravity','d-opencode-canary','d-pi','d-codex','d-antigravity','recovery-rehearsal','a-opencode-canary','a-pi','a-codex','a-antigravity','stable']
pub=set(seq)-{'readiness','e-observation','recovery-rehearsal'}; records=[]
for i,stage in enumerate(seq[:seq.index(current)]):
 d=(datetime(2026,1,1,tzinfo=timezone.utc)+timedelta(days=i)).strftime('%Y-%m-%dT%H:%M:%SZ')
 r={'stage':stage,'outcome':'go','completed_at':d,'valid_until':'2026-02-01T00:00:00Z','profile_id':f'p{i}','artifact_digest':f'{i+1:064x}','next_profile_id':f'p{i+1}','next_artifact_digest':f'{i+2:064x}','authorization_receipt':f'auth-{i}' if stage in pub else ''}
 if stage=='recovery-rehearsal': r.update(recovery_receipt='recovery-ok',transaction_recovery='passed',user_restoration='passed',foreign_content_preserved='passed')
 records.append(r)
profile_id=f'profile-{current}'
if records: records[-1].update(next_profile_id=profile_id,next_artifact_digest='e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855')
gates={target:'off' for target in ['opencode','pi','codex','antigravity']}
for event in seq[1:seq.index(current)+1]:
 if event in {'e-observation','recovery-rehearsal','stable'}: continue
 gates['opencode' if 'opencode' in event else event[2:]]='E' if event=='opencode-e-canary' else event[0].upper()
profile={'id':profile_id,'release':{'channel':'stable' if current=='stable' else 'prerelease','artifact_sha256':'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855'},'gates':gates}
data={'schema':'lore.promotion-evidence/v1','admission_blocked':False,'undo_claimed':False,'records':records}
if mutation=='missing': records.clear()
elif mutation=='failed': records[-1]['outcome']='no-go'
elif mutation=='stale': records[0]['valid_until']='2026-01-10T00:00:00Z'
elif mutation=='lineage': records[-1]['next_profile_id']='wrong-profile'
elif mutation=='profile-stage': profile['gates']['pi']='off'
elif mutation=='conflict': records.append(dict(records[-1]))
elif mutation=='observation': records[2]['completed_at']=records[1]['completed_at']
elif mutation=='recovery-receipt': records[10]['recovery_receipt']=''
elif mutation.startswith('recovery-'): records[10][mutation[9:]]='failed'
elif mutation=='digest': records[-1]['artifact_digest']='not-a-digest'
elif mutation=='stable-missing': records.pop()
elif mutation=='blocked': data['admission_blocked']=True
elif mutation=='undo': data['undo_claimed']=True
open(path,'w').write(json.dumps(data,separators=(',',':'))); open(profile_path,'w').write(json.dumps(profile,separators=(',',':')))
PY
}
run_promotion() { scripts/release-promotion-guard.sh "$PROMOTION" "$1" "$2" 24h 2026-01-20T00:00:00Z "$3" "$PROMOTION_PROFILE"; }
for stage in opencode-e-canary e-pi e-codex e-antigravity d-opencode-canary d-pi d-codex d-antigravity a-opencode-canary a-pi a-codex a-antigravity stable; do
  make_evidence "$stage"; run_promotion "$stage" pi,codex,antigravity "auth-current-$stage" >/dev/null
done
expect_promotion_failure() { expected=$1 stage=$2 mutation=$3 auth=${4-auth-current}; make_evidence "$stage" "$mutation"; output="$(run_promotion "$stage" pi,codex,antigravity "$auth" 2>&1)" && return 1; [[ "$output" == "promotion checkpoint invalid: $expected" ]]; }
expect_promotion_failure stage-order stable missing
expect_promotion_failure stage-evidence stable failed
expect_promotion_failure evidence-window stable stale
expect_promotion_failure authorization-reuse e-pi valid auth-1
expect_promotion_failure lineage stable lineage
expect_promotion_failure profile-stage stable profile-stage
expect_promotion_failure stage-order stable conflict
expect_promotion_failure observation-window e-pi observation
expect_promotion_failure remote-authorization a-opencode-canary valid ''
expect_promotion_failure recovery a-opencode-canary recovery-receipt
for field in transaction_recovery user_restoration foreign_content_preserved; do expect_promotion_failure recovery a-opencode-canary "recovery-$field"; done
expect_promotion_failure lineage stable digest
expect_promotion_failure stage-order stable stable-missing
make_evidence stable; output="$(run_promotion stable pi,antigravity,codex auth-current 2>&1)" && exit 1; [[ "$output" == 'promotion checkpoint invalid: stage-evidence' ]]
expect_promotion_failure admission-blocked stable blocked
expect_promotion_failure undo-claim stable undo
unsafe='/Users/private/user-content/Bearer-secret/Authorization/X-MCP-Header/eyJzY2hlbWEiOi.json'
output="$(scripts/release-promotion-guard.sh "$unsafe" stable pi,codex,antigravity 24h 2026-01-20T00:00:00Z fixture-auth "$unsafe" 2>&1)" && exit 1
[[ "$output" == 'promotion checkpoint invalid: evidence' ]]

python3 - <<'PY'
from pathlib import Path
text = Path('.github/workflows/release.yml').read_text()
guard = text.index('./scripts/release-promotion-guard.sh')
for marker in ('git fetch --force origin', 'go build -trimpath', 'actions/attest-build-provenance', 'gh release create'):
    assert guard < text.index(marker), marker
assert 'git tag ' not in text and 'git push ' not in text
PY

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
