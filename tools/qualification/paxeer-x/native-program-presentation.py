#!/usr/bin/env python3
import hashlib
import json
import os
from pathlib import Path
import stat
import subprocess
import sys

sys.dont_write_bytecode = True
ROOT = Path(__file__).resolve().parents[3]
TEST = 'real_program_call_presentation_preserves_limits_and_owned_verified_budget'
SOURCES = ('agent/crates/layerx-agentd/src/approval/native_program.rs',
           'agent/crates/layerx-agentd/src/approval/native_program_presentation.rs',
           'agent/crates/layerx-agentd/src/approval/mod.rs',
           'agent/crates/layerx-agentd/src/budget/program_settlement.rs',
           'agent/crates/layerx-agentd/src/budget/program_sources.rs',
           'agent/crates/layerx-agentd/src/budget/budget_proof.rs',
           'agent/crates/layerx-agentd/tests/native_program_presentation.rs',
           'tools/qualification/paxeer-x/native-program-presentation.py')


def require(value, message):
    if not value:
        raise RuntimeError(message)


def main():
    path = os.environ.get('PAXEER_X_NATIVE_PROGRAM_PRESENTATION_FIXTURE')
    require(path, 'genuine protected TLS/LNI native Programs presentation fixture is required')
    path = Path(path)
    info = path.lstat()
    require(stat.S_ISREG(info.st_mode) and 0 < info.st_size <= 65_536 and info.st_mode & 0o077 == 0,
            'native fixture must be protected and bounded')
    fixture = json.loads(path.read_text())
    require(fixture.get('schema') == 'paxeer-x.native-program-presentation.v1', 'invalid real fixture schema')
    hashes = fixture.get('source_hashes', {})
    require(all(hashes.get(source) == hashlib.sha256((ROOT / source).read_bytes()).hexdigest()
                for source in SOURCES), 'real fixture does not bind final intake and producer sources')
    target = Path(os.environ.get('CARGO_TARGET_DIR', '/root/lx-target/agent'))
    binaries = [binary for binary in (target / 'debug/deps').glob('native_program_presentation-*')
                if binary.is_file() and os.access(binary, os.X_OK) and not binary.name.endswith('.d')]
    require(len(binaries) == 1, 'exact current prebuilt native intake test binary required')
    result = subprocess.run([str(binaries[0]), '--exact', TEST], cwd=ROOT, env=os.environ,
                            timeout=480, capture_output=True, check=False)
    sys.stdout.buffer.write(result.stdout)
    sys.stderr.buffer.write(result.stderr)
    require(result.returncode == 0 and b'running 1 test' in result.stdout and b'1 passed; 0 failed' in result.stdout,
            'real TLS/native proof intake and substitution gate failed or did not execute')


if __name__ == '__main__':
    try:
        main()
    except RuntimeError as error:
        print('native Programs presentation refused: ' + str(error), file=sys.stderr)
        sys.exit(1)
    except (OSError, ValueError, subprocess.TimeoutExpired):
        print('native Programs presentation refused: genuine native fixture or process unavailable', file=sys.stderr)
        sys.exit(1)
