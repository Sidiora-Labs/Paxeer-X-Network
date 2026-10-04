#!/usr/bin/env bash
set -euo pipefail
repo_root=$(cd "$(dirname "$0")/../.." && pwd)
: "${LAYERX_GO_PROGRAMS_TEST:?prebuilt Go Programs test executable required}"
: "${LAYERX_GO_PROGRAMS_TEST_SHA256:?prebuilt executable digest required}"
: "${LAYERX_GO_PROGRAMS_REVISION:?published candidate revision required}"
: "${LAYERX_GO_PROGRAMS_LOG:?private focused behavior log required}"
export LAYERX_GO_PROGRAMS_ROOT="$repo_root"
python3 - <<'PYTHON'
import hashlib
import os
from pathlib import Path
import subprocess
root = Path(os.environ['LAYERX_GO_PROGRAMS_ROOT'])
binary = Path(os.environ['LAYERX_GO_PROGRAMS_TEST'])
expected = os.environ['LAYERX_GO_PROGRAMS_TEST_SHA256']
revision = os.environ['LAYERX_GO_PROGRAMS_REVISION']
if not binary.is_absolute() or not binary.is_file() or not os.access(binary, os.X_OK):
    raise SystemExit('actual prebuilt Go test executable required')
if len(expected) != 64 or any(c not in '0123456789abcdef' for c in expected):
    raise SystemExit('canonical prebuilt executable digest required')
if hashlib.sha256(binary.read_bytes()).hexdigest() != expected:
    raise SystemExit('Go Programs executable differs from the built artifact')
actual = subprocess.run(['git', 'rev-parse', 'HEAD'], cwd=root, check=True, capture_output=True, text=True).stdout.strip()
if actual != revision:
    raise SystemExit('Go Programs candidate revision mismatch')
subprocess.run(['git', 'diff', '--quiet', 'HEAD', '--', 'platform/sdk/go', 'platform/sdk/conformance/fixtures', 'programs/fixtures/pay5', 'scripts/paxeer-x/verify-sdk-program-go.sh'], cwd=root, check=True)
log = Path(os.environ['LAYERX_GO_PROGRAMS_LOG'])
if not log.is_absolute() or not log.parent.is_dir() or log.parent.stat().st_mode & 0o077:
    raise SystemExit('private focused behavior log required')
PYTHON
cd "$repo_root/platform/sdk/go"
"$LAYERX_GO_PROGRAMS_TEST" -test.v -test.count=1 -test.run='^(TestProgram|TestPrograms|TestSignedProgram|TestNativeProgram|TestNativeLifecycle|TestSignedTerminalV4Vectors|TestAppliedEmptyLegsRequireZeroRoot|TestNativeAccountAuthorizationVectors|TestLayerXKey)' | tee "$LAYERX_GO_PROGRAMS_LOG"
for required in TestProgramSDKDiscoveryCanonicalProof TestProgramSDKNativeABIPolicy TestProgramSDKInterfaceNativeBindingAndBound TestSignedTerminalV4Vectors; do
    if ! grep -q "^--- PASS: $required " "$LAYERX_GO_PROGRAMS_LOG"; then
        printf 'required Go Programs behavior did not pass: %s\n' "$required" >&2
        exit 1
    fi
done
python3 - <<'PYTHON'
import hashlib
import os
from pathlib import Path
import subprocess
if hashlib.sha256(Path(os.environ['LAYERX_GO_PROGRAMS_TEST']).read_bytes()).hexdigest() != os.environ['LAYERX_GO_PROGRAMS_TEST_SHA256']:
    raise SystemExit('Go Programs executable changed during focused verification')
subprocess.run(['git', 'diff', '--quiet', 'HEAD', '--', 'platform/sdk/go', 'platform/sdk/conformance/fixtures', 'programs/fixtures/pay5', 'scripts/paxeer-x/verify-sdk-program-go.sh'], cwd=os.environ['LAYERX_GO_PROGRAMS_ROOT'], check=True)
PYTHON
