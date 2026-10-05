#!/usr/bin/env python3
import hashlib
import json
import os
from pathlib import Path
import shutil
import stat
import subprocess
import sys
import tempfile

sys.dont_write_bytecode = True
ROOT = Path(__file__).resolve().parents[3]
TEST = 'real_native_effect_prepare_human_owner_decision_and_reopen'
SOURCES = ('agent/crates/layerx-agentd/src/human_runtime.rs',
           'agent/crates/layerx-agentd/src/human.rs',
           'agent/crates/layerx-agentd/src/approval/native_effect.rs',
           'agent/crates/layerx-agentd/src/capability/binding.rs',
           'agent/crates/layerx-agentd/src/capability/effects.rs',
           'agent/crates/layerx-agentd/src/policy/native_program.rs',
           'agent/crates/layerx-agentd/src/budget/reserve.rs',
           'agent/crates/layerx-agentd/src/budget/program_sources.rs',
           'agent/crates/layerx-agentd/src/budget/program_settlement.rs',
           'agent/crates/layerx-agentd/src/budget/budget_proof.rs',
           'agent/crates/layerx-agentd/src/agent_rpc_wire.rs',
           'agent/crates/layerx-agentd/tests/human_approval_admission.rs',
           'tools/qualification/paxeer-x/human-approval-admission.py')


def require(value, message):
    if not value:
        raise RuntimeError(message)


def main():
    require(not sys.argv[1:], 'unsupported gate arguments')
    path = os.environ.get('PAXEER_X_HUMAN_APPROVAL_ADMISSION_FIXTURE')
    require(path, 'genuine protected disposable mTLS/LNI native-effect admission fixture required')
    path = Path(path)
    info = path.lstat()
    require(stat.S_ISREG(info.st_mode) and 0 < info.st_size <= 65_536 and info.st_mode & 0o077 == 0,
            'fixture must be protected and bounded')
    fixture = json.loads(path.read_text())
    require(fixture.get('schema') == 'paxeer-x.human-approval-admission.v1', 'invalid fixture schema')
    require(fixture.get('isolated_real_owner') is True, 'disposable genuine owner required')
    hashes = fixture.get('source_hashes', {})
    require(all(hashes.get(source) == hashlib.sha256((ROOT / source).read_bytes()).hexdigest()
                for source in SOURCES), 'genuine fixture must bind the entire final source')
    uid, gid = fixture.get('uid'), fixture.get('gid')
    require(type(uid) is int and uid > 0 and type(gid) is int and gid > 0 and info.st_uid == uid,
            'fixture must belong to its genuinely admitted kernel peer')
    require(os.getuid() in (0, uid), 'genuine peer identity unavailable')
    target = Path(os.environ.get('CARGO_TARGET_DIR', '/root/lx-target/agent'))
    binaries = [p for p in (target / 'debug/deps').glob('human_approval_admission-*')
                if p.is_file() and os.access(p, os.X_OK) and not p.name.endswith('.d')]
    require(len(binaries) == 1, 'exact current prebuilt native-effect admission test required')
    require(binaries[0].stat().st_mtime_ns >= max((ROOT / p).stat().st_mtime_ns for p in SOURCES),
            'prebuilt admission binary predates final implementation')
    def peer_identity():
        os.setgroups([])
        os.setgid(gid)
        os.setuid(uid)
    with tempfile.TemporaryDirectory(prefix='layerx-native-admission-', dir='/tmp') as disposable:
        directory = Path(disposable)
        binary = directory / 'human_approval_admission'
        shutil.copyfile(binaries[0], binary)
        require(hashlib.sha256(binary.read_bytes()).digest() == hashlib.sha256(binaries[0].read_bytes()).digest(),
                'copied prebuilt admission artifact differs')
        if os.getuid() == 0:
            os.chown(directory, uid, gid)
            os.chown(binary, uid, gid)
        os.chmod(directory, 0o700)
        os.chmod(binary, 0o500)
        result = subprocess.run([str(binary), '--exact', TEST], cwd=directory, env=os.environ,
                                preexec_fn=peer_identity if os.getuid() == 0 else None,
                                timeout=480, capture_output=True, check=False)
    sys.stdout.buffer.write(result.stdout)
    sys.stderr.buffer.write(result.stderr)
    require(result.returncode == 0 and b'running 1 test' in result.stdout and b'1 passed; 0 failed' in result.stdout,
            'genuine admission owner corpus failed or did not execute')
    return 0


if __name__ == '__main__':
    try:
        sys.exit(main())
    except RuntimeError as error:
        print('human approval admission refused: ' + str(error), file=sys.stderr)
        sys.exit(1)
    except (OSError, ValueError, subprocess.TimeoutExpired):
        print('human approval admission refused: genuine fixture or process unavailable', file=sys.stderr)
        sys.exit(1)
