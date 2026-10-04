#!/usr/bin/env bash
set -euo pipefail
umask 077

root=$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)
evidence_root=${PAXEER_X_EVIDENCE_DIR:-}
exports_file=${GITHUB_ENV:-}
command_timeout=600
while [ "$#" -gt 0 ]; do
    case $1 in
    --evidence-root|--exports-file|--timeout)
        [ "$#" -ge 2 ] || exit 2
        case $1 in
        --evidence-root) evidence_root=$2 ;;
        --exports-file) exports_file=$2 ;;
        --timeout) command_timeout=$2 ;;
        esac
        shift 2 ;;
    *) echo 'usage: beta-ci-inputs.sh --evidence-root PRIVATE_DIR --exports-file PATH [--timeout SECONDS]' >&2; exit 2 ;;
    esac
done
[ -n "$evidence_root" ] && [ -n "$exports_file" ] || {
    echo 'beta-ci-inputs: explicit private evidence root and exports file required' >&2
    exit 2
}
[[ $command_timeout =~ ^[0-9]+$ ]] && ((command_timeout >= 1 && command_timeout <= 1800)) || exit 2
input_directory=$(python3 - "$root" "$evidence_root" <<'PY'
import hashlib
import json
import os
from pathlib import Path
import re
import stat
import subprocess
import sys
import tempfile

root = Path(sys.argv[1]).resolve()
base = Path(sys.argv[2]).absolute()
if base != base.resolve() or base == root or root in base.parents:
    raise SystemExit('beta-ci-inputs: evidence root must be canonical and outside source')
info = base.stat()
if not stat.S_ISDIR(info.st_mode) or info.st_uid != os.geteuid() or info.st_mode & 0o077:
    raise SystemExit('beta-ci-inputs: evidence root must be owned and private')
directory = Path(tempfile.mkdtemp(prefix='beta-ci-', dir=base))
archive = directory / 'historical-archive'
archive.mkdir(mode=0o700)
revision = 'f17f904928e2dacd38aabb8d4327edf523b8bafe'
pins = {
    'spec/layerx-beta/spec.kvx': 'b571925104ce6e957e5c654a0636c2f118ab6af88c2fe02ecb50df1da9d8876c',
    'spec/layerx-beta/qualification.kvx': '179d43198b172571495bf202b0422acdb1b1b966a9d091030876bfa3535d32e7',
}
records = []

def recover(relative):
    path = Path(relative)
    if path.is_absolute() or '..' in path.parts or any(part.startswith('.env') for part in path.parts):
        raise SystemExit('beta-ci-inputs: invalid historical input path')
    result = subprocess.run(['git', '-C', str(root), 'show', revision + ':' + relative],
                            stdin=subprocess.DEVNULL, capture_output=True, timeout=20)
    if result.returncode:
        raise SystemExit('beta-ci-inputs: pinned historical input unavailable')
    data = result.stdout
    if len(data) > 16 * 1024 * 1024:
        raise SystemExit('beta-ci-inputs: historical input exceeds bound')
    digest = hashlib.sha256(data).hexdigest()
    if relative in pins and digest != pins[relative]:
        raise SystemExit('beta-ci-inputs: pinned historical input digest mismatch')
    destination = archive / relative
    destination.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
    with destination.open('xb') as stream:
        stream.write(data)
    destination.chmod(0o600)
    records.append({'path': relative, 'sha256': digest, 'bytes': len(data)})
    return data

spec = recover('spec/layerx-beta/spec.kvx')
ledger = recover('spec/layerx-beta/qualification.kvx')
references = re.findall(r'^\s*evidence\s*=\s*"([^"\n]+)"', ledger.decode(), re.M)
if len(references) != 48 or len(set(references)) != 48:
    raise SystemExit('beta-ci-inputs: historical evidence inventory mismatch')
for relative in references:
    if not relative.startswith('spec/layerx-beta/evidence/'):
        raise SystemExit('beta-ci-inputs: historical evidence outside pinned archive')
    recover(relative)
selected = re.search(r'^\[task\.4\.3\]\s*\n(.*?)(?=^\[|\Z)', spec.decode(), re.M | re.S)
if selected is None or not re.search(r'^verify_cmd\s*=\s*"make beta-driver-test"\s*$', selected[1], re.M):
    raise SystemExit('beta-ci-inputs: pinned producer task contract mismatch')
candidate = subprocess.check_output(['git', '-C', str(root), 'rev-parse', 'HEAD'], text=True).strip()
if not re.fullmatch('[0-9a-f]{40}', candidate):
    raise SystemExit('beta-ci-inputs: actual candidate revision unavailable')
manifest = {'schema': 'layerx-beta-ci-input-provenance-v1', 'historical_revision': revision,
            'historical_release_credit': False, 'candidate': candidate, 'inputs': records,
            'fresh_task': '4.3', 'fresh_command': 'make beta-driver-test'}
(directory / 'input-provenance.json').write_text(json.dumps(manifest, indent=2) + '\n')
(directory / 'candidate').write_text(candidate + '\n')
print(directory)
PY
)
spec_file=$input_directory/historical-archive/spec/layerx-beta/spec.kvx
ledger_file=$input_directory/current-qualification.kvx
candidate=$(cat "$input_directory/candidate")
bash "$root/scripts/ci/beta-qualify.sh" --spec "$spec_file" --task 4.3 \
    --ledger "$ledger_file" --evidence-root "$evidence_root" --timeout "$command_timeout" \
    --evidence-kind directory -- 'make beta-driver-test'
python3 - "$ledger_file" "$spec_file" "$candidate" "$evidence_root" "$exports_file" <<'PY'
import os
from pathlib import Path
import re
import stat
import sys

ledger, spec, candidate, base, output = sys.argv[1:]
info = Path(ledger).stat()
if not stat.S_ISREG(info.st_mode) or info.st_uid != os.geteuid() or info.st_mode & 0o077 or info.st_nlink != 1:
    raise SystemExit('beta-ci-inputs: fresh ledger ownership invalid')
text = Path(ledger).read_text()
if not re.search(r'^\[gate\.4\.3\.\d+\]$', text, re.M) or not re.search(r'^source_evidence\s*=', text, re.M):
    raise SystemExit('beta-ci-inputs: executed typed fresh ledger required')
values = {'PAXEER_X_BETA_LEDGER_FILE': ledger, 'PAXEER_X_BETA_SPEC_FILE': spec,
          'PAXEER_X_BETA_CANDIDATE': candidate, 'PAXEER_X_EVIDENCE_DIR': base}
if any('\n' in value or '\r' in value for value in values.values()):
    raise SystemExit('beta-ci-inputs: invalid exported input path')
with open(output, 'a', encoding='utf-8') as stream:
    for name, value in values.items():
        stream.write(name + '=' + value + '\n')
print('beta-ci-inputs: retained historical archive and captured actual candidate-bound driver outcome')
PY
