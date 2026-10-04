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
TEST = 'real_programs_authenticated_peer_preserves_material_budget_and_owner_decisions'
SOURCES = ('agent/crates/layerx-agentd/src/human.rs',
           'agent/crates/layerx-agentd/src/human_runtime.rs',
           'agent/crates/layerx-agentd/src/approval/native_program.rs',
           'agent/crates/layerx-agentd/src/approval/native_program_presentation.rs',
           'agent/crates/layerx-agentd/src/budget/budget_proof.rs',
           'agent/crates/layerx-agentd/src/budget/program_settlement.rs',
           'agent/crates/layerx-agentd/src/budget/program_sources.rs',
           'agent/crates/layerx-agentd/tests/native_program_human_peer.rs',
           'tools/qualification/paxeer-x/native-program-human-peer.py')


def require(value, message):
    if not value:
        raise RuntimeError(message)


def main():
    target = Path(os.environ.get('CARGO_TARGET_DIR', '/root/lx-target/agent'))
    if sys.argv[1:] == ['--build']:
        result = subprocess.run(['/root/.cargo/bin/cargo', 'test', '--locked', '--manifest-path',
                                 str(ROOT / 'agent/Cargo.toml'), '-p', 'layerx-agentd', '--test',
                                 'native_program_human_peer', '--no-run'], cwd=ROOT,
                                env={**os.environ, 'CARGO_TARGET_DIR': str(target)}, timeout=1140,
                                check=False)
        return result.returncode
    require(not sys.argv[1:], 'unsupported gate arguments')
    path = os.environ.get('PAXEER_X_NATIVE_PROGRAM_HUMAN_PEER_FIXTURE')
    require(path, 'genuine protected native Programs owner/peer/proof fixture is required')
    path = Path(path)
    info = path.lstat()
    require(stat.S_ISREG(info.st_mode) and 0 < info.st_size <= 65_536 and info.st_mode & 0o077 == 0,
            'native peer fixture must be protected and bounded')
    fixture = json.loads(path.read_text())
    require(fixture.get('schema') == 'paxeer-x.native-program-human-peer.v1', 'invalid genuine fixture schema')
    hashes = fixture.get('source_hashes', {})
    require(all(hashes.get(source) == hashlib.sha256((ROOT / source).read_bytes()).hexdigest()
                for source in SOURCES), 'genuine fixture does not bind final source')
    require(fixture.get('isolated_real_owner') is True, 'isolated genuine retained owner is required')
    uid, gid = fixture.get('uid'), fixture.get('gid')
    require(isinstance(uid, int) and uid > 0 and isinstance(gid, int) and gid > 0
            and info.st_uid == uid, 'fixture must belong to its genuine authenticated peer')
    binaries = [binary for binary in (target / 'debug/deps').glob('native_program_human_peer-*')
                if binary.is_file() and os.access(binary, os.X_OK) and not binary.name.endswith('.d')]
    require(len(binaries) == 1, 'exact current prebuilt native peer test binary required')
    def peer_identity():
        os.setgroups([])
        os.setgid(gid)
        os.setuid(uid)
    require(os.getuid() in (0, uid), 'test must use the genuine admitted kernel peer')
    result = subprocess.run([str(binaries[0]), '--exact', TEST], cwd=ROOT, env=os.environ,
                            preexec_fn=peer_identity if os.getuid() == 0 else None,
                            timeout=480, capture_output=True, check=False)
    sys.stdout.buffer.write(result.stdout)
    sys.stderr.buffer.write(result.stderr)
    require(result.returncode == 0 and b'running 1 test' in result.stdout and b'1 passed; 0 failed' in result.stdout,
            'genuine native owner peer gate failed or did not execute')
    return 0


if __name__ == '__main__':
    try:
        sys.exit(main())
    except RuntimeError as error:
        print('native Programs peer refused: ' + str(error), file=sys.stderr)
        sys.exit(1)
    except (OSError, ValueError, subprocess.TimeoutExpired):
        print('native Programs peer refused: genuine fixture or process unavailable', file=sys.stderr)
        sys.exit(1)
