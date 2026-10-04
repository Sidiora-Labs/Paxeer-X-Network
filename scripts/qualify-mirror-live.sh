#!/usr/bin/env bash
set -euo pipefail

config_path="${LAYERX_MIRROR_LIVE_CONFIG:?set LAYERX_MIRROR_LIVE_CONFIG}"
status_url="${LAYERX_MIRROR_STATUS_URL:-http://127.0.0.1:9091/status}"
publisher_bin="${LAYERX_MIRROR_PUBLISHER_BIN:-interop/target/release/layerx-mirror-publisher}"
fault_controller="${LAYERX_MIRROR_FAULT_CONTROLLER:?set LAYERX_MIRROR_FAULT_CONTROLLER to the authenticated devnet fault-control executable}"
evidence_dir="${LAYERX_MIRROR_LIVE_EVIDENCE_DIR:-}"
if [[ -n "${evidence_dir}" ]]; then
  python3 - "${evidence_dir}" <<'PY_PRIVATE'
import os, stat, sys
from pathlib import Path
path = Path(sys.argv[1]).resolve()
root = Path.cwd().resolve()
info = path.stat()
if (root == path or root in path.parents or not stat.S_ISDIR(info.st_mode)
        or info.st_uid != os.geteuid() or info.st_mode & 0o077):
    raise SystemExit("mirror live evidence directory must be private and outside the repository")
PY_PRIVATE
fi
record_phase() {
  if [[ -n "${evidence_dir}" ]]; then
    (umask 077; printf '%s\n' "$2" > "${evidence_dir}/$1.json")
  fi
}

"${publisher_bin}" "${config_path}" &
publisher_pid=$!
cleanup() {
  kill "${publisher_pid}" 2>/dev/null || true
  wait "${publisher_pid}" 2>/dev/null || true
}
trap cleanup EXIT

snapshot() {
  curl --fail --silent --show-error --max-time 5 "${status_url}"
}

wait_for() {
  local expression=$1
  local deadline=$((SECONDS + 1200))
  while (( SECONDS < deadline )); do
    status="$(snapshot || true)"
    if [[ -n "${status}" ]] && jq -e "${expression}" >/dev/null <<<"${status}"; then
      printf '%s' "${status}"
      return 0
    fi
    sleep 5
  done
  snapshot || true
  return 1
}

status="$(wait_for '.ethereum.phase == "retrieved_verified" and .solana.phase == "retrieved_verified" and (.ethereum.latest_batch_mirrored == .solana.latest_batch_mirrored) and (.ethereum.latest_batch_mirrored != null)')"
record_phase baseline "${status}"
baseline="$(jq -r '.ethereum.latest_batch_mirrored' <<<"${status}")"

"${fault_controller}" stall ethereum
status="$(wait_for ".ethereum.error_class == \"rpc\" and .solana.latest_batch_mirrored > ${baseline}")"
record_phase ethereum_stall "${status}"
"${fault_controller}" restore ethereum
status="$(wait_for '.ethereum.phase == "retrieved_verified" and (.ethereum.latest_batch_mirrored == .solana.latest_batch_mirrored)')"
record_phase ethereum_restored "${status}"

baseline="$(jq -r '.solana.latest_batch_mirrored' <<<"${status}")"
"${fault_controller}" stall solana
status="$(wait_for ".solana.error_class == \"rpc\" and .ethereum.latest_batch_mirrored > ${baseline}")"
record_phase solana_stall "${status}"
"${fault_controller}" restore solana
status="$(wait_for '.solana.phase == "retrieved_verified" and (.ethereum.latest_batch_mirrored == .solana.latest_batch_mirrored)')"
record_phase solana_restored "${status}"

"${fault_controller}" reorg ethereum
status="$(wait_for '.ethereum.reorgs_observed > 0')"
record_phase ethereum_reorg "${status}"
status="$(wait_for '.ethereum.phase == "retrieved_verified" and (.ethereum.latest_batch_mirrored == .solana.latest_batch_mirrored)')"
record_phase ethereum_recovered "${status}"

"${fault_controller}" reorg solana
status="$(wait_for '.solana.reorgs_observed > 0')"
record_phase solana_reorg "${status}"
status="$(wait_for '.solana.phase == "retrieved_verified" and (.ethereum.latest_batch_mirrored == .solana.latest_batch_mirrored)')"
record_phase solana_recovered "${status}"

if [[ -n "${evidence_dir}" ]]; then
  python3 - "${evidence_dir}" "${publisher_bin}" "${fault_controller}" <<'PY_EVIDENCE'
import datetime, hashlib, json, os, subprocess, sys
from pathlib import Path
root = Path.cwd().resolve()
directory = Path(sys.argv[1]).resolve()
def git(*args):
    return subprocess.check_output(['git', '-C', str(root), *args], text=True).strip()
if git('status', '--porcelain=v1', '--untracked-files=normal'):
    raise SystemExit('mirror live evidence refuses dirty source')
paths = sorted((root / 'interop/crates/layerx-mirror/src').rglob('*.rs'))
paths += [root / 'scripts/qualify-mirror-live.sh']
record = {'schema': 'layerx.mirror-live-evidence.v1', 'exit_code': 0,
          'revision': git('rev-parse', 'HEAD'), 'tree': git('rev-parse', 'HEAD^{tree}'),
          'source_hashes': {str(p.relative_to(root)): hashlib.sha256(p.read_bytes()).hexdigest() for p in paths},
          'publisher_sha256': hashlib.sha256(Path(sys.argv[2]).read_bytes()).hexdigest(),
          'fault_controller_sha256': hashlib.sha256(Path(sys.argv[3]).read_bytes()).hexdigest(),
          'finished_at': datetime.datetime.now(datetime.timezone.utc).isoformat(),
          'phases': {p.stem: json.loads(p.read_text()) for p in directory.glob('*.json') if p.name != 'evidence.json'}}
fd = os.open(directory / 'evidence.json', os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
with os.fdopen(fd, 'w') as stream:
    json.dump(record, stream, sort_keys=True)
    stream.write('\n')
PY_EVIDENCE
fi
