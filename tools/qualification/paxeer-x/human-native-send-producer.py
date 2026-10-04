#!/usr/bin/env python3
import argparse
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
TEST = 'genuine_human_owner_session_consent_preview_and_reopen'
SOURCES = (
    'human/crates/layerx-human-service/src/server/agent_creation_native.rs',
    'human/crates/layerx-human-service/src/server/agent_creation.rs',
    'human/crates/layerx-human-service/src/server/agent_runtime.rs',
    'human/crates/layerx-human-service/src/server/native_send.rs',
    'human/crates/layerx-human-service/src/server/production_components.rs',
    'human/crates/layerx-human-service/src/custody/signer.rs',
    'human/crates/layerx-human-service/src/custody/provider.rs',
    'agent/crates/layerx-agentd/src/human_runtime.rs',
    'agent/crates/layerx-agentd/src/capability/binding.rs',
    'agent/crates/layerx-agentd/src/session_control.rs',
    'human/crates/layerx-human-service/tests/native_send_producer.rs',
    'human/crates/layerx-human-service/Cargo.toml',
    'tools/qualification/paxeer-x/human-native-send-producer.py',
)
CASES = {
    'denied-before-consent', 'actual-session-and-signed-grant',
    'immutable-preview-without-economic-effect', 'foreign-and-changed-context-refused',
    'exact-preview-purpose-signed', 'durable-reopen-and-exact-replay',
}


def require(value, message):
    if not value:
        raise RuntimeError(message)


def duplicate_free(pairs):
    result = {}
    for key, value in pairs:
        require(key not in result, 'duplicate JSON field')
        result[key] = value
    return result


def protected(path, maximum):
    path = Path(path)
    require(not any(part.startswith('.env') for part in path.parts), 'credential environment paths refused')
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    with os.fdopen(fd, 'rb') as stream:
        info = os.fstat(stream.fileno())
        require(stat.S_ISREG(info.st_mode) and info.st_nlink == 1 and info.st_mode & 0o077 == 0
                and 0 < info.st_size <= maximum, 'protected single-link bounded input required')
        data = stream.read(maximum + 1)
        require(len(data) == info.st_size and len(data) <= maximum, 'input changed or exceeded bound')
    return json.loads(data, object_pairs_hook=duplicate_free), info


def digest(path):
    path = Path(path)
    require(not any(part.startswith('.env') for part in path.parts), 'credential environment paths refused')
    require(not path.is_symlink() and path.is_file(), 'actual source or compiler artifact required')
    value = hashlib.sha256()
    with path.open('rb') as stream:
        for chunk in iter(lambda: stream.read(1048576), b''):
            value.update(chunk)
    return value.hexdigest()


def git(*arguments):
    result = subprocess.run(['git', '--no-optional-locks', '-C', str(ROOT), *arguments],
                            capture_output=True, check=False, timeout=20)
    require(result.returncode == 0 and len(result.stdout) <= 4194304, 'bounded candidate identity unavailable')
    return result.stdout.decode().strip()


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--candidate-manifest', required=True)
    args = parser.parse_args()
    sys.path.insert(0, str(ROOT / 'tools/paxeer-x'))
    from candidate import catalogue, load_private, validate
    candidate = load_private(args.candidate_manifest)
    validate(candidate, catalogue(ROOT / 'spec/paxeer-x/spec.kvx'))
    revision = git('rev-parse', 'HEAD')
    require(candidate['source']['revision'] == revision and candidate['source']['integrated'] is True
            and candidate['source']['dirty'] is False and not git('status', '--porcelain', '--untracked-files=all'),
            'clean published integrated candidate required')
    fixture_path = os.environ.get('PAXEER_X_HUMAN_NATIVE_SEND_PRODUCER_FIXTURE')
    require(fixture_path, 'genuine disposable daemon, Human TLS, fresh passkey and HumanPrimary attestor foundation required')
    fixture, metadata = protected(fixture_path, 65536)
    require(fixture.get('schema') == 'paxeer-x.human-native-send-producer.v1'
            and fixture.get('isolated_real_owner') is True, 'closed authentic foundation profile required')
    uid, gid = fixture.get('uid'), fixture.get('gid')
    require(type(uid) is int and uid > 0 and type(gid) is int and gid > 0 and metadata.st_uid == uid
            and os.geteuid() in (0, uid), 'actual admitted non-root kernel peer required')
    hashes = {path: digest(ROOT / path) for path in SOURCES}
    require(fixture.get('source_hashes') == hashes, 'authentic fixture must bind final producer and owners')
    artifacts_path = os.environ.get('PAXEER_X_HUMAN_NATIVE_SEND_PRODUCER_ARTIFACTS')
    require(artifacts_path, 'genuine prebuilt focused compiler artifact manifest required')
    artifacts, _ = protected(artifacts_path, 65536)
    require(set(artifacts) == {'schema', 'source_revision', 'source_hashes', 'executable'}
            and artifacts['schema'] == 'paxeer-x.human-native-send-producer.artifacts.v1'
            and artifacts['source_revision'] == revision and artifacts['source_hashes'] == hashes,
            'actual source-bound compiler artifact profile required')
    executable = artifacts['executable']
    require(set(executable) == {'path', 'sha256'} and digest(executable['path']) == executable['sha256']
            and os.access(executable['path'], os.X_OK), 'actual unchanged executable required')
    def peer():
        os.setgroups([])
        os.setgid(gid)
        os.setuid(uid)
    with tempfile.TemporaryDirectory(prefix='layerx-human-native-producer-', dir='/tmp') as temporary:
        directory = Path(temporary)
        binary = directory / 'native_send_producer'
        shutil.copyfile(executable['path'], binary)
        require(digest(binary) == executable['sha256'], 'copied compiler artifact differs')
        if os.geteuid() == 0:
            os.chown(directory, uid, gid)
            os.chown(binary, uid, gid)
        os.chmod(directory, 0o700)
        os.chmod(binary, 0o500)
        result = subprocess.run([str(binary), '--exact', TEST, '--nocapture', '--test-threads=1'],
                                cwd=directory, env=os.environ,
                                preexec_fn=peer if os.geteuid() == 0 else None,
                                timeout=480, capture_output=True, check=False)
    sys.stdout.buffer.write(result.stdout)
    sys.stderr.buffer.write(result.stderr)
    observed = result.stdout.decode(errors='replace')
    cases = [line.split('NATIVE_SEND_PRODUCER_CASE ', 1)[1].strip()
             for line in observed.splitlines() if 'NATIVE_SEND_PRODUCER_CASE ' in line]
    require(result.returncode == 0 and len(cases) == len(CASES) and set(cases) == CASES
            and '1 passed; 0 failed; 0 ignored;' in observed,
            'actual owner session, consent, immutable preview and replay corpus failed or skipped')
    require(hashes == {path: digest(ROOT / path) for path in SOURCES}
            and digest(executable['path']) == executable['sha256'], 'candidate or executable changed during verification')
    print('human native Send producer: six genuine owner cases passed revision=' + revision)


if __name__ == '__main__':
    try:
        main()
    except RuntimeError as error:
        print('human native Send producer refused: ' + str(error), file=sys.stderr)
        sys.exit(1)
    except (OSError, ValueError, subprocess.TimeoutExpired):
        print('human native Send producer refused: authentic candidate, fixture or corpus unavailable', file=sys.stderr)
        sys.exit(1)
