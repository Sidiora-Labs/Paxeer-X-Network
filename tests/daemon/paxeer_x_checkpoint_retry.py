#!/usr/bin/env python3
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import stat
import subprocess
import sys
import tempfile

sys.dont_write_bytecode = True
ROOT = Path(__file__).resolve().parents[2]
COMMAND = 'timeout 1800s python3 tests/daemon/paxeer_x_checkpoint_retry.py'
CASES = {
    'keeper': ('TestCheckpointRetryDurableRestart', 'TestCheckpointRetryChallengeResolution',
               'TestCheckpointRetryMembershipAndBindings'),
    'precompile': ('TestChallengeResolutionIsAuthorityOnlyThroughRun',
                   'TestUpheldChallengeSlashesThroughRun', 'TestBelowThresholdStaysSubmittedThroughRun',
                   'TestRefusedCheckpointsRevert', 'TestRevertCarriesReason'),
}


def require(value, reason):
    if not value:
        raise RuntimeError(reason)


def private(path):
    path = Path(path)
    info = path.lstat()
    require(stat.S_ISREG(info.st_mode) and info.st_nlink == 1 and
            stat.S_IMODE(info.st_mode) == 0o600 and info.st_uid == os.geteuid(),
            'private owned regular manifest required')
    return json.loads(path.read_text())


def manifest():
    name = os.environ.get('PAXEER_X_CHECKPOINT_ARTIFACT_MANIFEST')
    require(name, 'PAXEER_X_CHECKPOINT_ARTIFACT_MANIFEST required; build task targets before this gate')
    value = private(name)
    revision = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT, text=True).strip()
    require(value.get('version') == 1 and value.get('source_revision') == revision,
            'checkpoint artifact source revision mismatch')
    require(not subprocess.check_output(['git', 'status', '--porcelain'], cwd=ROOT), 'dirty checkpoint source')
    require(set(value.get('artifacts', {})) == {'keeper', 'precompile', 'guarantor'}, 'checkpoint artifact set')
    for name, row in value['artifacts'].items():
        path = Path(row['path'])
        require(path.is_absolute() and path.is_file() and not any(p.is_symlink() for p in (path, *path.parents))
                and os.access(path, os.X_OK), 'missing genuine prebuilt executable: ' + name)
        with path.open('rb') as stream:
            require(hashlib.file_digest(stream, 'sha256').hexdigest() == row['sha256'], 'artifact hash: ' + name)
        require(path.open('rb').read(4) == b'\x7fELF', 'native ELF required: ' + name)
    return value


def main():
    os.umask(0o077)
    evidence = Path(os.environ.get('PAXEER_X_EVIDENCE_DIR', '/var/tmp'))
    require(evidence.is_dir() and not evidence.is_symlink(), 'private evidence parent required')
    evidence = Path(tempfile.mkdtemp(prefix='checkpoint-retry-', dir=evidence))
    evidence.chmod(0o700)
    revision = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT, text=True).strip()
    status, completed = 1, []
    try:
        value = manifest()
        import paxeer_x_runtime_fixture as runtime
        runtime.artifacts(os.environ.get('PAXEER_X_RUNTIME_ARTIFACTS', ''))
        runtime.client_artifact(os.environ.get('PAXEER_X_RUNTIME_CLIENT_MANIFEST', ''))
        for executable in ('unshare', 'ip', 'mount', 'openssl', 'jq', 'bash'):
            require(shutil.which(executable), 'missing preinstalled fixture prerequisite: ' + executable)
        codec_cases = ['test_anchor_checkpoint_record_comparison', 'test_wire_membership_and_registration',
                       'test_checkpoint_progress_durable_deadline_and_binding', 'test_checkpoint_wire_all_progress_states']
        command = [sys.executable, str(ROOT / 'tests/daemon/guarantor-settlement.py'),
                   *('SettlementTests.' + name for name in codec_cases), '-v']
        with (evidence / 'producer-progress.log').open('w') as log:
            result = subprocess.run(command, cwd=ROOT, stdout=log, stderr=log, timeout=60)
        require(result.returncode == 0, 'producer progress protocol failed: ' + str(result.returncode))
        completed.extend(codec_cases)
        for kind, names in CASES.items():
            command = [value['artifacts'][kind]['path'], '-test.v', '-test.count=1',
                       '-test.timeout=300s', '-test.run=^(' + '|'.join(names) + ')$']
            with (evidence / (kind + '.log')).open('w') as log:
                result = subprocess.run(command, cwd=ROOT, stdout=log, stderr=log, timeout=310)
            output = (evidence / (kind + '.log')).read_text()
            require(result.returncode == 0, kind + ' failed: ' + str(result.returncode))
            for name in names:
                require(re.search(r'^--- PASS: ' + re.escape(name) + r' \(', output, re.M), 'missing real test: ' + name)
            require('--- SKIP:' not in output and 'no tests to run' not in output, kind + ' skipped coverage')
            completed.extend(names)
        directory = Path(tempfile.mkdtemp(prefix='px-retry-', dir='/var/tmp'))
        directory.rmdir()
        env = os.environ.copy()
        for name in ('net', 'mnt', 'pid'):
            env['PAXEER_X_PARENT_' + name.upper()] = os.readlink('/proc/self/ns/' + name)
        command = ['unshare', '--mount', '--net', '--pid', '--fork', '--kill-child=KILL',
                   '--mount-proc', '--propagation', 'private', sys.executable,
                   str(ROOT / 'tests/daemon/guarantor-settlement-chain.py'), '--worker', str(directory)]
        with (evidence / 'chain.log').open('w') as log:
            result = subprocess.run(command, cwd=ROOT, env=env, stdout=log, stderr=log, timeout=1050)
        (evidence / 'runtime-directory').write_text(str(directory) + '\n')
        require(result.returncode == 0, 'real producer/anchor integration failed: ' + str(result.returncode))
        result = json.loads((directory / 'result.json').read_text())
        require(result['scenarios'] == ['zero-window', 'retry-boundaries', 'challenged', 'deadline', 'reverted-finalize'],
                'incomplete real checkpoint lifecycle coverage')
        completed.extend(result['scenarios'])
        status = 0
    except Exception as error:
        (evidence / 'failure.txt').write_text(type(error).__name__ + ': ' + str(error) + '\n')
    finally:
        with (evidence / 'result.json').open('x') as output:
            json.dump({'revision': revision, 'command': COMMAND, 'exit_code': status,
                       'evidence': str(evidence), 'completed': completed}, output, sort_keys=True)
            output.write('\n')
        print(f'{revision} | {COMMAND} | {status} | {evidence}')
        print('PAXEER_X_GATE tests=' + str(len(completed)) + ' skipped=0')
    return status


if __name__ == '__main__':
    raise SystemExit(main())
