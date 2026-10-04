#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/../../.."
python3 - <<'PY'
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys

root=Path.cwd()
source_paths={"platform/hosted/gateway/src/state.rs","tests/platform/gateway_maintenance_head.rs",
              "tools/paxeer-x/gates/16.6.sh","platform/hosted/gateway/tests/fixtures/maintained-authority.json"}
expected={"native-maintenance","ordinary-domain-preserved","domain-confusion-true","domain-confusion-false",
          "tamper-/receipt_hex","tamper-/receipt_digest","tamper-/state_root","tamper-/batch_evidence/header_hex",
          "tamper-/batch_evidence/header_signature","tamper-/batch_evidence/receipt_proof_hex",
          "foreign-key","unauthorized-batch","stale-observed_sequence","stale-observed_at",
          "stale-activity-receipt","not-current-route-refusal"}

def require(condition,detail):
    if not condition: raise RuntimeError(detail)

def digest(path):
    value=hashlib.sha256()
    with path.open("rb") as handle:
        for part in iter(lambda:handle.read(1024*1024),b""): value.update(part)
    return value.hexdigest()

def unique(pairs):
    value={}
    for key,item in pairs:
        require(key not in value,"duplicate manifest key")
        value[key]=item
    return value

try:
    supplied=os.environ.get("PAXEER_X_GATEWAY_MAINTENANCE_ARTIFACTS")
    require(supplied,"actual prebuilt gateway maintenance artifact manifest required")
    path=Path(supplied).resolve(strict=True)
    require(path.stat().st_size<=65536,"artifact manifest bound")
    artifact=json.loads(path.read_text(),object_pairs_hook=unique)
    require(set(artifact)=={"schema","source_revision","source_files","executable"},"artifact manifest fields")
    require(artifact["schema"]=="layerx.gateway.maintenance-artifacts.v1","artifact schema")
    revision=subprocess.check_output(["git","rev-parse","HEAD"],text=True).strip()
    require(artifact["source_revision"]==revision,"artifact revision mismatch")
    require(set(artifact["source_files"])==source_paths,"source inventory mismatch")
    for relative,pinned in artifact["source_files"].items():
        require(digest(root/relative)==pinned,"source mismatch: "+relative)
    executable=artifact["executable"]
    require(set(executable)=={"path","sha256"},"executable fields")
    binary=Path(executable["path"]).resolve(strict=True)
    require(binary.is_file() and os.access(binary,os.X_OK),"actual test artifact missing")
    require(digest(binary)==executable["sha256"],"artifact digest mismatch")
    environment={key:value for key,value in os.environ.items()
                 if not key.startswith("LAYERX_") and not key.startswith("PAXEER_X_")}
    run=subprocess.run([str(binary),"state::native_maintenance_tests::","--nocapture","--test-threads=1"],
                       env=environment,text=True,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,timeout=600,check=False)
    sys.stdout.write(run.stdout)
    require(run.returncode==0,"production maintenance verifier failed: "+str(run.returncode))
    require("4 passed; 0 failed; 0 ignored;" in run.stdout,"actual focused test count mismatch")
    cases=[line.split("GATEWAY_MAINTENANCE_CASE ",1)[1].strip()
           for line in run.stdout.splitlines() if "GATEWAY_MAINTENANCE_CASE " in line]
    require(len(cases)==len(expected) and set(cases)==expected,"actual case inventory mismatch")
    require(digest(binary)==executable["sha256"],"artifact changed during gate")
    for relative,pinned in artifact["source_files"].items():
        require(digest(root/relative)==pinned,"source changed during gate: "+relative)
    print("PAXEER_X_GATE tests=16 skipped=0")
except (OSError,ValueError,RuntimeError,subprocess.SubprocessError) as error:
    print("gateway maintenance gate refused: "+str(error),file=sys.stderr)
    print("PAXEER_X_GATE tests=0 skipped=0")
    sys.exit(1)
PY
