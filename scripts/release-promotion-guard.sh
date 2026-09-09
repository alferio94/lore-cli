#!/usr/bin/env bash
set -euo pipefail
[[ $# -eq 7 ]] || { echo "promotion checkpoint invalid: arguments" >&2; exit 1; }
python3 - "$@" <<'PY'
import json,re,sys
from datetime import datetime,timedelta

evidence,stage,order,window,evaluated,authorization,profile_path=sys.argv[1:]
def fail(field):
    print(f"promotion checkpoint invalid: {field}",file=sys.stderr); raise SystemExit(1)
def timestamp(value):
    if not isinstance(value,str) or not re.fullmatch(r"[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z",value): fail("timestamp")
    try: return datetime.fromisoformat(value[:-1]+"+00:00")
    except ValueError: fail("timestamp")
def safe(value): return isinstance(value,str) and re.fullmatch(r"[a-z0-9][a-z0-9._+-]{0,127}",value)
def digest(value): return isinstance(value,str) and re.fullmatch(r"[0-9a-f]{64}",value)
try:
    data=json.load(open(evidence,encoding="utf-8")); profile=json.load(open(profile_path,encoding="utf-8"))
except (OSError,ValueError): fail("evidence")
if not isinstance(data,dict) or not isinstance(profile,dict) or set(data)!={"schema","admission_blocked","undo_claimed","records"} or data["schema"]!="lore.promotion-evidence/v1": fail("schema")
if data["admission_blocked"] is not False: fail("admission-blocked")
if data["undo_claimed"] is not False: fail("undo-claim")
targets=order.split(",")
if len(targets)!=3 or len(set(targets))!=3 or any(not safe(x) or x=="opencode" for x in targets): fail("target-order")
sequence=["readiness","opencode-e-canary","e-observation",*[f"e-{x}" for x in targets],"d-opencode-canary",*[f"d-{x}" for x in targets],"recovery-rehearsal","a-opencode-canary",*[f"a-{x}" for x in targets],"stable"]
publication=set(sequence)-{"readiness","e-observation","recovery-rehearsal"}
if stage not in publication or not safe(authorization): fail("remote-authorization")
now=timestamp(evaluated)
match=re.fullmatch(r"([1-9][0-9]{0,3})(h|d)",window)
if not match: fail("observation-window")
records=data["records"]
if not isinstance(records,list) or len(records)!=sequence.index(stage): fail("stage-order")
base={"stage","outcome","completed_at","valid_until","profile_id","artifact_digest","next_profile_id","next_artifact_digest","authorization_receipt"}
recovery=base|{"recovery_receipt","transaction_recovery","user_restoration","foreign_content_preserved"}
used=set(); previous=None; completed={}
for record,want in zip(records,sequence):
    if not isinstance(record,dict) or set(record)!=(recovery if want=="recovery-rehearsal" else base): fail("evidence-shape")
    if record["stage"]!=want or record["outcome"]!="go": fail("stage-evidence")
    done,valid=timestamp(record["completed_at"]),timestamp(record["valid_until"])
    if done>now or now>valid: fail("evidence-window")
    values=(record["profile_id"],record["next_profile_id"])
    if not all(safe(x) for x in values) or not digest(record["artifact_digest"]) or not digest(record["next_artifact_digest"]): fail("lineage")
    if previous and previous!=(record["profile_id"],record["artifact_digest"]): fail("lineage")
    previous=(record["next_profile_id"],record["next_artifact_digest"]); completed[want]=done
    receipt=record["authorization_receipt"]
    if want in publication:
        if not safe(receipt) or receipt in used: fail("authorization-reuse")
        used.add(receipt)
    elif receipt!="": fail("remote-authorization")
    if want=="recovery-rehearsal" and (not safe(record["recovery_receipt"]) or any(record[x]!="passed" for x in ("transaction_recovery","user_restoration","foreign_content_preserved"))): fail("recovery")
if "e-observation" in completed:
    delay=timedelta(**({"hours":int(match[1])} if match[2]=="h" else {"days":int(match[1])}))
    if completed["e-observation"] < completed["opencode-e-canary"]+delay: fail("observation-window")
release=profile.get("release")
current=(profile.get("id"),release.get("artifact_sha256") if isinstance(release,dict) else None)
if previous!=current or not safe(current[0]) or not digest(current[1]): fail("lineage")
gates={target:"off" for target in ["opencode",*targets]}
for event in sequence[1:sequence.index(stage)+1]:
    if event in {"e-observation","recovery-rehearsal","stable"}: continue
    level="E" if event=="opencode-e-canary" else event[0].upper()
    target="opencode" if "opencode" in event else event[2:]
    gates[target]=level
if profile.get("gates")!=gates or release.get("channel")!=("stable" if stage=="stable" else "prerelease"): fail("profile-stage")
if authorization in used: fail("authorization-reuse")
print("promotion checkpoint accepted")
PY
