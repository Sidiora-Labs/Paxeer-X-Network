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
TEST = 'genuine_native_journey_holds_reopens_and_resumes_original_authority'
SOURCES = ('human/crates/layerx-human-service/src/journeys/engine.rs',
           'human/crates/layerx-human-service/src/server/agent_runtime.rs',
           'human/crates/layerx-human-service/src/server/production_components.rs',
           'human/crates/layerx-human-service/src/server/native_send.rs',
           'human/crates/layerx-human-service/src/custody/signer.rs',
           'human/crates/layerx-human-service/src/custody/provider.rs',
           'human/crates/layerx-human-service/src/custody/mod.rs',
           'agent/crates/layerx-agentd/src/human_runtime.rs',
           'agent/crates/layerx-agentd/src/approval/native_effect.rs',
           'agent/crates/layerx-agentd/src/agent_rpc_wire.rs',
           'agent/crates/layerx-agent-api/src/identity.rs',
           'agent/crates/layerx-sdk/src/native_effect.rs',
           'human/schema/human-api/v1.kvx',
           'human/crates/layerx-human-service/tests/native_journey_approval.rs',
           'tools/qualification/paxeer-x/native-journey-approval.py')


def require(value, message):
    if not value:
        raise RuntimeError(message)


def main():
    require(not sys.argv[1:], 'unsupported gate arguments')
    path = os.environ.get('PAXEER_X_NATIVE_JOURNEY_APPROVAL_FIXTURE')
    require(path, 'protected genuine native Journey LNI/KMS/authpeer authority fixture required')
    info = Path(path).lstat()
    require(stat.S_ISREG(info.st_mode) and 0 < info.st_size <= 65_536 and info.st_mode & 0o077 == 0,
            'native Journey authority fixture must be protected and bounded')
    fixture = json.loads(Path(path).read_text())
    require(fixture.get('schema') == 'paxeer-x.native-journey-approval.v2'
            and fixture.get('isolated_real_owner') is True, 'genuine disposable owner required')
    hashes = fixture.get('source_hashes', {})
    require(all(hashes.get(source) == hashlib.sha256((ROOT / source).read_bytes()).hexdigest()
                for source in SOURCES), 'genuine fixture must bind final Journey source')
    uid, gid = fixture.get('uid'), fixture.get('gid')
    require(type(uid) is int and uid > 0 and type(gid) is int and gid > 0 and info.st_uid == uid,
            'genuine admitted kernel peer and protected fixture ownership required')
    require(os.getuid() in (0, uid), 'actual admitted peer unavailable')
    target = Path(os.environ.get('CARGO_TARGET_DIR', '/root/lx-target/human'))
    binaries = [p for p in (target / 'debug/deps').glob('native_journey_approval-*')
                if p.is_file() and os.access(p, os.X_OK) and not p.name.endswith('.d')]
    require(len(binaries) == 1, 'exact prebuilt native Journey corpus required')
    require(binaries[0].stat().st_mtime_ns >= max((ROOT / source).stat().st_mtime_ns for source in SOURCES),
            'prebuilt corpus predates final source')
    def peer_identity():
        os.setgroups([])
        os.setgid(gid)
        os.setuid(uid)
    with tempfile.TemporaryDirectory(prefix='layerx-native-journey-', dir='/tmp') as temporary:
        directory = Path(temporary)
        binary = directory / 'native_journey_approval'
        shutil.copyfile(binaries[0], binary)
        require(hashlib.sha256(binary.read_bytes()).digest() == hashlib.sha256(binaries[0].read_bytes()).digest(),
                'copied genuine test artifact differs')
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
            'genuine native Journey corpus failed or did not execute')
    return 0


if __name__ == '__main__':
    try:
        sys.exit(main())
    except RuntimeError as error:
        print('native Journey admission refused: ' + str(error), file=sys.stderr)
        sys.exit(1)
    except (OSError, ValueError, subprocess.TimeoutExpired):
        print('native Journey admission refused: authentic fixture or process unavailable', file=sys.stderr)
        sys.exit(1)
