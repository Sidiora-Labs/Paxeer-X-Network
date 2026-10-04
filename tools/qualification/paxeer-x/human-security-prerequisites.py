#!/usr/bin/env python3
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import signal
import stat
import subprocess
import sys
import tempfile
import time

ROOT = Path(__file__).resolve().parents[3]
FIXTURE = 'LAYERX_HUMAN_SECURITY_PREREQUISITES_FIXTURE'
SCHEMA = 'layerx.human.security-prerequisites.fixture.v1'
SOURCE_FILES = [
    'docker/kernel/init.sh', 'docker/human-service/entrypoint.sh',
    'human/crates/layerx-human-security-provider/src/main.rs',
    'human/crates/layerx-human-security-provider/src/lib.rs',
    'human/crates/layerx-human-security-provider/src/state.rs',
    'human/crates/layerx-human-security-provider/src/transport.rs',
    'human/crates/layerx-human-security-provider/src/probe.rs',
    'platform/hosted/runtime-clock/src/main.rs',
    'platform/hosted/runtime-clock/src/server.rs',
]
SOURCE_FILES += [str(p.relative_to(ROOT)) for p in sorted((ROOT / 'platform/hosted/human').glob('*.py'))]
FUNCTIONS = ('log', 'missing', 'service', 'human_genesis_project', 'human_policy_bundle_install',
             'human_material_generate', 'human_project', 'human_security_waits',
             'human_security_prerequisite', 'human_security_prepare')


def require(value, reason):
    if not value:
        raise RuntimeError(reason)


def strict_json(path):
    def pairs(items):
        result = {}
        for key, value in items:
            require(key not in result, 'duplicate fixture or manifest field')
            result[key] = value
        return result
    return json.loads(path.read_text(), object_pairs_hook=pairs)


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def fixture():
    require(FIXTURE in os.environ, FIXTURE + ' is required; no producer material synthesized')
    path = Path(os.environ[FIXTURE])
    require(path.is_absolute() and path.resolve() == path, 'fixture path must be canonical')
    info = path.stat()
    require(stat.S_ISREG(info.st_mode) and info.st_uid == os.geteuid()
            and stat.S_IMODE(info.st_mode) == 0o600 and info.st_nlink == 1,
            'fixture must be one protected owner file')
    value = strict_json(path)
    require(set(value) == {'schema', 'source_revision', 'source_sha256', 'network_id',
                          'kernel_state', 'executables'}, 'fixture contract fields differ')
    require(value['schema'] == SCHEMA, 'fixture schema differs')
    revision = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT, text=True).strip()
    require(value['source_revision'] == revision, 'fixture source revision differs')
    require(set(value['source_sha256']) == set(SOURCE_FILES), 'source binding inventory differs')
    for relative in SOURCE_FILES:
        require(value['source_sha256'][relative] == digest(ROOT / relative),
                'fixture does not bind selected production source: ' + relative)
    require(type(value['network_id']) is int and 0 < value['network_id'] <= 0xffffffff,
            'genuine producer network id required')
    state = Path(value['kernel_state'])
    require(state.is_absolute() and state.resolve() == state and state.is_dir(),
            'genuine kernel producer directory required')
    for file in state.rglob('*'):
        require(not file.is_symlink() and (file.is_file() or file.is_dir()),
                'producer fixture must contain ordinary files and directories')
        require(file.name != '.env', 'credential environment file is outside fixture contract')
    require(set(value['executables']) == {'security-provider', 'runtime-clock'},
            'genuine prebuilt provider and runtime clock required')
    for name, artifact in value['executables'].items():
        require(set(artifact) == {'path', 'sha256', 'source_revision'}, 'executable binding fields differ')
        executable = Path(artifact['path'])
        require(executable.is_absolute() and executable.resolve() == executable
                and executable.is_file() and os.access(executable, os.X_OK),
                'prebuilt executable missing: ' + name)
        require(executable.read_bytes()[:4] == b'\x7fELF', 'real compiled executable required: ' + name)
        require(artifact['source_revision'] == revision and artifact['sha256'] == digest(executable),
                'prebuilt executable provenance differs: ' + name)
    return value


def functions():
    lines = (ROOT / 'docker/kernel/init.sh').read_text().splitlines(keepends=True)
    result = {}
    for index, line in enumerate(lines):
        match = re.match(r'^([A-Za-z_][A-Za-z_0-9]*)\(\) \{', line)
        if not match or match[1] not in FUNCTIONS:
            continue
        if line.rstrip().endswith('}'):
            result[match[1]] = line
        else:
            end = next(i for i in range(index + 1, len(lines)) if lines[i].rstrip() == '}')
            result[match[1]] = ''.join(lines[index:end + 1])
    require(set(result) == set(FUNCTIONS), 'production security prerequisite functions are incomplete')
    return '\n'.join(result[name] for name in FUNCTIONS)


def clone_tree(source, destination):
    shutil.copytree(source, destination)
    for item in [source, *source.rglob('*')]:
        target = destination / item.relative_to(source)
        info = item.stat()
        os.chown(target, info.st_uid, info.st_gid)
        os.chmod(target, stat.S_IMODE(info.st_mode))


def run(argv, **kwargs):
    return subprocess.run(argv, check=True, **kwargs)


def namespace_setup(work, value):
    run(['mount', '--make-rprivate', '/'])
    for destination in ('/run', '/var/lib', '/usr/local'):
        run(['mount', '-t', 'tmpfs', '-o', 'nosuid,nodev,mode=0755', 'tmpfs', destination])
    require(Path('/data').is_dir(), 'existing namespace mount point /data required')
    run(['mount', '--bind', str(work / 'data'), '/data'])
    Path('/usr/local/bin').mkdir()
    Path('/usr/local/lib/layerx-human').mkdir(parents=True)
    run(['mount', '--bind', str(ROOT / 'platform/hosted/human'), '/usr/local/lib/layerx-human'])
    run(['mount', '-o', 'remount,bind,ro', '/usr/local/lib/layerx-human'])
    for name, executable in value['executables'].items():
        shutil.copy2(executable['path'], '/usr/local/bin/layerx-' + ('human-security-provider' if name == 'security-provider' else name))
    shutil.copy2(ROOT / 'docker/human-service/entrypoint.sh', '/usr/local/bin/human-entrypoint')
    os.chmod('/usr/local/bin/human-entrypoint', 0o755)
    for directory in ('/run/layerx/init', '/run/layerx/node', '/run/layerx/human',
                      '/run/layerx/human-material', '/run/human-material',
                      '/run/human-private', '/var/lib/layerx/human'):
        Path(directory).mkdir(parents=True, exist_ok=True)
    node_fixture = work / 'node'
    require(node_fixture.is_dir(), 'genuine core.env and sequencer-public-key producer outputs required')
    for name in ('core.env', 'sequencer-public-key'):
        shutil.copy2(node_fixture / name, Path('/run/layerx/node') / name)
    os.chown('/run/layerx/human', 4020, 4020)
    os.chmod('/run/layerx/human', 0o750)
    Path('/run/human-private/security').mkdir()
    os.chown('/run/human-private/security', 4020, 4020)
    os.chmod('/run/human-private/security', 0o700)
    state = Path('/data/human-state')
    require(not (state / 'material').exists() and not (state / 'material.new').exists(),
            'fixture requires unprepared genuine producer state')
    (state / 'security').mkdir(exist_ok=True)
    os.chown(state / 'security', 4020, 4020)
    os.chmod(state / 'security', 0o700)


def shell_source(value):
    genesis_assignment = next(line for line in (ROOT / 'docker/kernel/init.sh').read_text().splitlines()
                              if line.startswith('genesis_files='))
    return '\n'.join([
        'set -euo pipefail', 'umask 077', 'kernel_profile=full',
        'layerx=/data/layerx', 'keys=$layerx/keys', 'genesis=$layerx/genesis',
        'human_state=/data/human-state', 'human_policy=$keys/human-policy/policy.json',
        'human_out=$human_state/material/human', 'human_material=/run/layerx/human-material',
        'run=/run/layerx', 'status=$run/init', 'trust_history_file=$human_state/trust-history',
        genesis_assignment, 'export LAYERX_NODE_PAXEER_CHAIN_ID=125',
        'export LAYERX_NODE_NETWORK_ID=' + str(value['network_id']), functions(),
    ]) + '\n'


def start_service(work, source):
    script = work / 'service.sh'
    script.write_text(source + '''human_root=$human_state/security service human-security 4020 "$(human_security_waits)" human_security_prepare - -- \
    env LAYERX_HUMAN_SECURITY_PROVIDER_SOCKET="$run/human/security.sock" \
    LAYERX_HUMAN_SECURITY_PROVIDER_STATE_ROOT=/var/lib/layerx/human/security \
    LAYERX_HUMAN_SECURITY_PROVIDER_ALLOWED_UID=4020 \
    LAYERX_HUMAN_SECURITY_PROVIDER_TRUST_HISTORY=/run/human-private/security/trust-history \
    LAYERX_HUMAN_SECURITY_PROVIDER_DEADLINE_SECONDS=5 \
    /usr/local/bin/human-entrypoint security
wait "$!"
''')
    output = open(work / 'service.log', 'ab')
    process = subprocess.Popen(['/bin/bash', str(script)], stdout=output, stderr=output,
                               start_new_session=True, env={'PATH': '/usr/local/bin:/usr/bin:/bin'})
    output.close()
    return process


def stop_service(process):
    try:
        os.killpg(process.pid, signal.SIGTERM)
        process.wait(timeout=4)
    except (ProcessLookupError, subprocess.TimeoutExpired):
        pass
    try:
        os.killpg(process.pid, signal.SIGKILL)
    except ProcessLookupError:
        pass
    process.wait(timeout=4)


def status():
    path = Path('/run/layerx/init/human-security')
    return path.read_text().strip() if path.is_file() else ''


def wait_for(predicate, process, seconds=15):
    deadline = time.monotonic() + seconds
    while time.monotonic() < deadline:
        require(process.poll() is None, 'real service supervisor exited permanently')
        if predicate():
            return
        time.sleep(0.05)
    raise RuntimeError('bounded security service observation failed')


def probe():
    return subprocess.run(['setpriv', '--reuid=4020', '--regid=4020', '--clear-groups',
                           '--', '/usr/local/bin/layerx-human-security-provider', 'probe'],
                          env={'PATH': '/usr/local/bin:/usr/bin:/bin',
                               'LAYERX_HUMAN_SECURITY_PROVIDER_SOCKET': '/run/layerx/human/security.sock',
                               'LAYERX_HUMAN_SECURITY_PROVIDER_DEADLINE_SECONDS': '1'},
                          stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=2).returncode == 0


def ready(process):
    wait_for(lambda: status().startswith('4020 running ') and probe(), process, 20)
    projected = Path('/run/layerx/human-material/human-security/trust-history')
    private = Path('/run/human-private/security/trust-history')
    original = Path('/data/human-state/trust-history')
    for path, uid, gid, mode in ((projected, 0, 4020, 0o440), (private, 4020, 4020, 0o600)):
        info = path.lstat()
        require(stat.S_ISREG(info.st_mode) and not path.is_symlink() and info.st_nlink == 1
                and (info.st_uid, info.st_gid, stat.S_IMODE(info.st_mode)) == (uid, gid, mode),
                'normal protected projection or private-copy mode differs')
    require(original.read_bytes() == projected.read_bytes() == private.read_bytes(),
            'real private trust copy differs from retained producer material')
    require(len({(p.stat().st_dev, p.stat().st_ino) for p in (original, projected, private)}) == 3,
            'trust history must be three independent private files')
    require(Path('/data/human-state/material/genesis-binding').is_file()
            and Path('/data/human-state/material/bundle-binding').is_file(),
            'real sealed material generation did not run')
    projection = Path('/data/human-state/genesis-binding')
    info = projection.lstat()
    require((info.st_uid, info.st_gid, stat.S_IMODE(info.st_mode)) == (0, 0, 0o700)
            and not projection.is_symlink(), 'genuine genesis private projection is not protected')
    require({p.name for p in projection.iterdir()} == {'metadata.lxgb', 'asset-id', 'replica-id'},
            'genuine genesis private projection inventory differs')
    for name in ('metadata.lxgb', 'asset-id', 'replica-id'):
        private_genesis = projection / name
        shared_genesis = Path('/data/layerx/genesis') / name
        info = private_genesis.lstat()
        require(stat.S_ISREG(info.st_mode) and info.st_nlink == 1
                and (info.st_uid, info.st_gid, stat.S_IMODE(info.st_mode)) == (0, 0, 0o600)
                and private_genesis.read_bytes() == shared_genesis.read_bytes(),
                'genuine genesis source bytes or protected private copy differ')


def driver(work, case, argument):
    value = strict_json(work / 'fixture.json')
    host_namespaces = strict_json(work / 'host-namespaces.json')
    require(os.getpid() == 1 and all(Path('/proc/self/ns/' + name).stat().st_ino != inode
                                   for name, inode in host_namespaces.items()),
            'driver requires private mount and PID namespaces')
    namespace_setup(work, value)
    source = shell_source(value)
    if case == 'list':
        result = subprocess.check_output(['/bin/bash', '-c', source + 'human_security_waits'], text=True)
        paths = result.split()
        expected = ['/data/layerx/genesis/metadata.lxgb', '/data/layerx/keys/sequencer.key',
                    '/data/layerx/genesis/asset-id', '/data/layerx/genesis/replica-id',
                    '/data/layerx/keys/publication/binding-policy.json',
                    '/data/layerx/keys/publication/authorization.json', '/run/layerx/node/core.env',
                    '/run/layerx/node/sequencer-public-key', '/data/layerx/keys/human-policy/policy.json',
                    '/data/layerx/keys/human-policy/bundle-manifest.json',
                    '/data/layerx/keys/human-policy/inputs', '/data/layerx/keys/human-policy/journal',
                    '/data/human-state/trust-history']
        require(paths == expected, 'security wait dependencies differ from frozen producer order')
        admission = subprocess.run(['/bin/bash', '-c', source + 'human_security_prerequisite'],
                                   stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
        require(admission.returncode == 1 and not admission.stdout,
                'genuine producer inputs refused by canonical prerequisite admission')
        for path in paths:
            require(Path(path).exists(), 'genuine owner producer output missing: ' + path)
        manifest = strict_json(Path('/data/layerx/keys/human-policy/bundle-manifest.json'))
        for section in ('inputs', 'journal', 'onboarding'):
            for item in manifest[section]:
                path = Path('/data/layerx/keys/human-policy') / section / item['name']
                require(path.is_file(), 'genuine declared producer output missing')
                paths.append(str(path))
        (work / 'dependencies.json').write_text(json.dumps(paths))
        return
    hidden = None
    history = Path('/data/human-state/trust-history')
    if case in ('missing', 'retry', 'restart', 'owner-before-trust'):
        path = Path(argument)
        hidden = work / 'removed-input'
        path.rename(hidden)
        if case == 'owner-before-trust':
            history.rename(work / 'removed-trust')
    elif case == 'trust-mode':
        history.chmod(0o444)
    elif case == 'trust-owner':
        os.chown(history, 4020, 4020)
    elif case == 'trust-oversize':
        history.write_bytes(history.read_bytes() + bytes(1048577))
    elif case == 'trust-empty':
        history.write_bytes(b'')
    elif case == 'trust-trailing':
        history.write_bytes(history.read_bytes() + b'\x00')
    elif case == 'trust-symlink':
        history.rename(work / 'genuine-history')
        history.symlink_to(work / 'genuine-history')
    elif case == 'trust-hardlink':
        os.link(history, work / 'genuine-history-link')
    elif case == 'private-symlink':
        Path('/run/human-private/security').rmdir()
        Path('/run/human-private/security').symlink_to(work)
    elif case == 'private-mode':
        Path('/run/human-private/security').chmod(0o750)
    elif case == 'private-copy-symlink':
        Path('/run/human-private/security/trust-history').symlink_to(history)
    elif case in ('private-copy-mode', 'private-copy-hardlink'):
        private_copy = Path('/run/human-private/security/trust-history')
        private_copy.write_bytes(history.read_bytes())
        os.chown(private_copy, 4020, 4020)
        private_copy.chmod(0o600 if case == 'private-copy-hardlink' else 0o644)
        if case == 'private-copy-hardlink':
            os.link(private_copy, '/run/human-private/security/other-history')
    process = start_service(work, source)
    try:
        if hidden:
            wait_for(lambda: status().startswith('4020 waiting '), process)
            observed = status()
            if argument in json.loads((work.parent / 'dependencies.json').read_text())[:13]:
                require(observed == '4020 waiting ' + argument, 'missing prerequisite wait reason is imprecise')
            else:
                require('owner-policy ' in observed and Path(argument).name in observed,
                        'declared producer refusal omitted exact missing input')
            require(not Path('/data/human-state/material.new').exists()
                    and not Path('/data/human-state/material').exists()
                    and not Path('/run/layerx/human-material/human-security').exists()
                    and not Path('/run/layerx/human/security.sock').exists(),
                    'preparation or provider launch happened before producer prerequisites')
            if case == 'retry':
                time.sleep(5.5)
                require(process.poll() is None and status() == observed and not probe(),
                        'bounded retry claimed ready or exited while producer absent')
            hidden.rename(argument)
            if case == 'owner-before-trust':
                wait_for(lambda: status() == '4020 waiting ' + str(history), process, 12)
                (work / 'removed-trust').rename(history)
            ready(process)
            if case == 'restart':
                pid = int(status().split()[2])
                Path(argument).rename(hidden)
                os.kill(pid, signal.SIGTERM)
                wait_for(lambda: status() == observed, process, 12)
                require(not probe(), 'restart skipped prerequisite checks')
                hidden.rename(argument)
                ready(process)
        elif case == 'complete':
            ready(process)
        else:
            time.sleep(7)
            require(process.poll() is None and not probe(), 'refused trust/private boundary became ready')
            require(not Path('/run/layerx/human/security.sock').exists(), 'refused boundary opened provider socket')
            refused_log = (work / 'service.log').read_text()
            if case == 'trust-trailing':
                require('invalid security provider configuration' in refused_log,
                        'real provider canonical trust parser did not refuse malformed history')
            elif case.startswith('private-copy-'):
                require('security trust-history private copy refused' in refused_log,
                        'private destination protection did not report its precise refusal')
            elif case.startswith('private-'):
                require('private runtime directory refused:' in refused_log,
                        'private directory protection did not report its precise refusal')
            if case.startswith('trust-') and case != 'trust-trailing':
                require(status() == '4020 waiting trust-history protected material refused',
                        'protected source refusal did not precede preparation')
                require(not Path('/data/human-state/material').exists(),
                        'invalid source material reached generator')
    finally:
        stop_service(process)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--run-cases', action='store_true')
    parser.add_argument('--driver', nargs=3, metavar=('WORK', 'CASE', 'INPUT'))
    args = parser.parse_args()
    if args.driver:
        driver(Path(args.driver[0]), args.driver[1], args.driver[2])
        return
    if not args.run_cases:
        run(['/bin/bash', str(ROOT / 'tools/bringup/check-live.test.sh'), '--human-security-prerequisites'])
        return
    require(os.geteuid() == 0, 'root is required for real protected UID and mount boundaries')
    for executable in ('unshare', 'mount', 'setpriv', 'bash'):
        require(shutil.which(executable), 'required real process tool missing: ' + executable)
    value = fixture()
    parent = Path(os.environ.get('LAYERX_QUALIFICATION_LOG_DIR', '/root/lx-ops'))
    parent.mkdir(mode=0o700, parents=True, exist_ok=True)
    work = Path(tempfile.mkdtemp(prefix='human-security-prerequisites-', dir=parent))
    os.chmod(work, 0o700)
    started = time.monotonic()
    results = []

    def execute(case, argument=''):
        require(time.monotonic() - started < 840, 'focused gate exceeded bounded qualification window')
        directory = work / (str(len(results)) + '-' + case)
        directory.mkdir(mode=0o700)
        clone_tree(Path(value['kernel_state']) / 'data', directory / 'data')
        clone_tree(Path(value['kernel_state']) / 'run/layerx/node', directory / 'node')
        (directory / 'fixture.json').write_text(json.dumps(value))
        (directory / 'host-namespaces.json').write_text(json.dumps({
            name: Path('/proc/self/ns/' + name).stat().st_ino for name in ('mnt', 'pid')}))
        with open(directory / 'driver.log', 'wb') as output:
            command = ['unshare', '--mount', '--pid', '--fork', '--kill-child', '--mount-proc',
                       '/usr/bin/python3', str(Path(__file__).resolve()), '--driver', str(directory), case, argument]
            completed = subprocess.run(command, stdout=output, stderr=output, timeout=45,
                                       env={'PATH': '/usr/bin:/bin'})
        results.append({'case': case, 'input': argument, 'exit_code': completed.returncode,
                        'log_path': str(directory / 'driver.log')})
        (work / 'results.json').write_text(json.dumps(results, indent=2))
        require(completed.returncode == 0, 'real security prerequisite case failed: ' + case + '; log=' + str(directory / 'driver.log'))
        if case == 'list':
            shutil.copyfile(directory / 'dependencies.json', work / 'dependencies.json')
        print('pass security-prerequisite ' + case, flush=True)

    execute('list')
    dependencies = strict_json(work / 'dependencies.json')
    require(14 <= len(dependencies) <= 100, 'producer dependency inventory is not bounded')
    for dependency in dependencies:
        execute('missing', dependency)
    execute('owner-before-trust', dependencies[13])
    execute('retry', dependencies[0])
    execute('complete')
    for dependency in dependencies[:14]:
        execute('restart', dependency)
    for case in ('trust-mode', 'trust-owner', 'trust-oversize', 'trust-empty', 'trust-symlink',
                 'trust-hardlink', 'trust-trailing', 'private-symlink', 'private-mode',
                 'private-copy-symlink', 'private-copy-mode', 'private-copy-hardlink'):
        execute(case)
    print('human-security-prerequisites: all genuine production cases passed; evidence=' + str(work))


if __name__ == '__main__':
    try:
        main()
    except (RuntimeError, OSError, ValueError, KeyError, subprocess.SubprocessError) as error:
        print('human-security-prerequisites: ' + str(error), file=sys.stderr)
        raise SystemExit(1)
