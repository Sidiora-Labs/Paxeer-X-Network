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
SOURCES = (
    'agent/crates/layerx-agentd/src/approval/mod.rs',
    'agent/crates/layerx-agentd/src/approval/events.rs',
    'agent/crates/layerx-agentd/src/policy/approval.rs',
    'agent/crates/layerx-agentd/src/human_runtime.rs',
    'agent/crates/layerx-agentd/src/human.rs',
    'agent/crates/layerx-agentd/src/managed_agent.rs',
    'agent/crates/layerx-agentd/src/budget/reserve.rs',
    'agent/crates/layerx-agentd/tests/human_projection_facts.rs',
    'tools/qualification/paxeer-x/human-projection-facts.py',
)


def require(condition, message):
    if not condition:
        raise RuntimeError(message)


def main():
    path = os.environ.get('PAXEER_X_HUMAN_PROJECTION_FACTS_FIXTURE')
    require(path, 'genuine authenticated approval, budget and managed receipt fixture required')
    path = Path(path)
    metadata = path.lstat()
    require(stat.S_ISREG(metadata.st_mode) and 0 < metadata.st_size <= 65536
            and metadata.st_mode & 0o077 == 0, 'protected bounded fixture required')
    fixture = json.loads(path.read_text())
    require(fixture.get('schema') == 'paxeer-x.human-projection-facts.v1', 'wrong genuine fixture profile')
    hashes = fixture.get('source_hashes', {})
    require(all(hashes.get(source) == hashlib.sha256((ROOT / source).read_bytes()).hexdigest()
                for source in SOURCES), 'fixture does not bind final producer and consumer sources')
    target = Path(os.environ.get('CARGO_TARGET_DIR', '/root/lx-target/human-facts'))
    binaries = [path for path in (target / 'debug/deps').glob('human_projection_facts-*')
                if path.is_file() and os.access(path, os.X_OK) and not path.name.endswith('.d')]
    require(len(binaries) == 1, 'exact current prebuilt projection test binary required')
    result = subprocess.run([str(binaries[0]), '--test-threads=1'], cwd=ROOT, env=os.environ,
                            timeout=480, capture_output=True, check=False)
    sys.stdout.buffer.write(result.stdout)
    sys.stderr.buffer.write(result.stderr)
    require(result.returncode == 0 and b'running 5 tests' in result.stdout
            and b'5 passed; 0 failed' in result.stdout,
            'real approval/export and reservation cases failed or did not execute')


if __name__ == '__main__':
    try:
        main()
    except RuntimeError as error:
        print('Human projection refused: ' + str(error), file=sys.stderr)
        sys.exit(1)
    except (OSError, ValueError, subprocess.TimeoutExpired):
        print('Human projection refused: genuine fixture or process unavailable', file=sys.stderr)
        sys.exit(1)
