#!/usr/bin/env python3
import argparse
import contextlib
import fcntl
import hashlib
import json
import os
from pathlib import Path
import re
import shlex
import signal
import stat
import subprocess
import tempfile
import time
import uuid

VERSION = 1
QUOTA_BYTES = 5368709120
QUOTA_INODES = 65536
CONTROLLERS = ('cpu', 'memory', 'pids', 'io')
RECIPE = 'platform/hosted/registry/builder-environment'
PROVISIONER = 'platform/hosted/registry/node-provision-build-boundary.sh'


def require(condition, message):
    if not condition:
        raise RuntimeError(message)


def run(command, *, timeout=120, env=None, check=True):
    result = subprocess.run(list(map(str, command)), capture_output=True, text=True,
                            timeout=timeout, env=env)
    if check and result.returncode:
        raise RuntimeError(f'command failed ({result.returncode}): {shlex.join(list(map(str, command)))}\n{result.stderr}')
    return result


def write_json(path, data):
    path = Path(path)
    temporary = path.with_name(path.name + '.new-' + uuid.uuid4().hex)
    descriptor = os.open(temporary, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(descriptor, 'w') as stream:
        json.dump(data, stream, sort_keys=True, indent=2)
        stream.write('\n')
        stream.flush()
        os.fsync(stream.fileno())
    os.replace(temporary, path)
    directory = os.open(path.parent, os.O_RDONLY | os.O_DIRECTORY)
    try:
        os.fsync(directory)
    finally:
        os.close(directory)


def regular(path):
    path = Path(path)
    require(path.is_absolute() and path.resolve(strict=True) == path,
            'artifact path must be canonical and absolute')
    info = path.lstat()
    require(stat.S_ISREG(info.st_mode) and info.st_nlink == 1, 'artifact must be singly linked and regular')
    return path


def digest(path):
    path = regular(path)
    require(path.name != '.env' and not path.name.startswith('.env.'), 'credential path refused')
    with path.open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def source(repo):
    repo = Path(repo).resolve(strict=True)
    require(not run(['git', '-C', repo, 'status', '--porcelain', '--untracked-files=all']).stdout,
            'source checkout is dirty')
    return {'revision': run(['git', '-C', repo, 'rev-parse', 'HEAD']).stdout.strip(),
            'tree': run(['git', '-C', repo, 'rev-parse', 'HEAD^{tree}']).stdout.strip()}


def private_directory(path, repo):
    path = Path(path).absolute()
    require(path.parent.resolve(strict=True) == path.parent, 'output parent must be canonical')
    require(not path.is_relative_to(Path(repo).resolve()), 'artifacts must be outside source checkout')
    require(not path.exists() and not path.is_symlink(), 'output must not exist')
    path.mkdir(mode=0o700)
    return path


def image_id(image):
    require(re.fullmatch(r'sha256:[0-9a-f]{64}', image) is not None, 'immutable image ID required')
    actual = run(['docker', 'image', 'inspect', '--format', '{{.Id}}', image]).stdout.strip()
    require(actual == image, 'runtime image identity mismatch')
    return actual


def build(repo, output):
    repo = Path(repo).resolve(strict=True)
    identity = source(repo)
    output = private_directory(output, repo)
    write_json(output / 'build-state.json', {'phase': 'building', 'source': identity})
    commands = [
        ['bash', str(repo / RECIPE / 'build-env.sh'), str(output / 'builder')],
        ['docker', 'build', '--target', 'base', '--iidfile', str(output / 'runtime-image-id'),
         '--file', str(repo / 'docker/platform-registry/Dockerfile'), str(repo)],
    ]
    for index, command in enumerate(commands):
        with (output / f'build-{index}.log').open('w') as log:
            completed = subprocess.run(command, stdout=log, stderr=subprocess.STDOUT)
        write_json(output / f'build-{index}.json', {'command': command, 'exit_code': completed.returncode})
        require(completed.returncode == 0, f'canonical build {index} failed; see {output / f"build-{index}.log"}')
    runtime = image_id((output / 'runtime-image-id').read_text().strip())
    run(['python3', repo / 'platform/hosted/gateway/tests/local/registry_image.py',
         '--image', runtime, '--output', output / 'runtime-image.json'])
    evidence = json.loads((output / 'runtime-image.json').read_text())
    container = 'layerx-artifacts-extract-' + uuid.uuid4().hex
    binaries = output / 'bin'
    binaries.mkdir(mode=0o700)
    artifacts = {}
    try:
        run(['docker', 'create', '--name', container, runtime])
        for name, location in [('layerx-program-registry', '/usr/local/bin/layerx-program-registry'),
                               ('layerx-cgroup-exec', '/usr/bin/layerx-cgroup-exec')]:
            target = binaries / name
            run(['docker', 'cp', container + ':' + location, target])
            require(os.access(target, os.X_OK), 'extracted executable is not executable')
            artifacts[name] = {'path': str(target), 'sha256': digest(target)}
    finally:
        run(['docker', 'rm', container], check=False)
    require(source(repo) == identity, 'source changed during build')
    builder = output / 'builder'
    require((builder / 'source-revision').read_text().strip() == identity['revision'], 'builder source mismatch')
    manifest = {'schema_version': VERSION, 'repo': str(repo), 'source': identity,
                'runtime_image': runtime, 'runtime_evidence': str(output / 'runtime-image.json'),
                'isolation_sha256': evidence['isolation_sha256'], 'artifacts': artifacts,
                'builder_image': image_id((builder / 'image-id').read_text().strip()),
                'builder_root': str(builder / 'rootfs'),
                'builder_digest': (builder / 'environment-tree-digest').read_text().strip(),
                'quota': {'slots': 1, 'bytes': QUOTA_BYTES, 'inodes': QUOTA_INODES},
                'registry_readiness': 'unclaimed'}
    write_json(output / 'artifacts.json', manifest)
    write_json(output / 'build-state.json', {'phase': 'built', 'source': identity})
    return output / 'artifacts.json'


def validate(manifest_path):
    manifest_path = regular(manifest_path)
    require(manifest_path.stat().st_mode & 0o077 == 0, 'manifest permissions are not protected')
    manifest = json.loads(manifest_path.read_text())
    require(manifest['schema_version'] == VERSION, 'unknown artifact schema')
    require(source(manifest['repo']) == manifest['source'], 'source identity mismatch')
    require(manifest['registry_readiness'] == 'unclaimed', 'artifact manifest cannot claim registry readiness')
    require(manifest['quota'] == {'slots': 1, 'bytes': QUOTA_BYTES, 'inodes': QUOTA_INODES}, 'quota policy mismatch')
    for name in ('layerx-program-registry', 'layerx-cgroup-exec'):
        artifact = manifest['artifacts'][name]
        require(os.access(regular(artifact['path']), os.X_OK), 'missing executable')
        require(digest(artifact['path']) == artifact['sha256'], 'executable digest mismatch')
    image_id(manifest['runtime_image'])
    image_id(manifest['builder_image'])
    evidence = json.loads(regular(manifest['runtime_evidence']).read_text())
    require(evidence == {'image_id': manifest['runtime_image'], 'isolation_sha256': manifest['isolation_sha256']},
            'isolation image evidence mismatch')
    require(re.fullmatch(r'[0-9a-f]{64}', manifest['isolation_sha256']) is not None
            and manifest['isolation_sha256'] != '0' * 64, 'invalid isolation digest')
    root = Path(manifest['builder_root'])
    require(root.is_dir() and root.resolve(strict=True) == root, 'missing or noncanonical builder root')
    require((root.parent / 'source-revision').read_text().strip() == manifest['source']['revision'], 'builder source mismatch')
    actual = run(['python3', Path(manifest['repo']) / RECIPE / 'digest.py', root], timeout=300).stdout.strip()
    require(actual == manifest['builder_digest'], 'builder rootfs digest mismatch')
    return manifest


@contextlib.contextmanager
def locked(directory):
    directory = Path(directory)
    require(directory.resolve(strict=True) == directory, 'lifecycle directory must be canonical')
    require(directory.stat().st_uid == os.getuid() and directory.stat().st_mode & 0o077 == 0,
            'lifecycle directory is not privately owned')
    descriptor = os.open(directory / 'lifecycle.lock', os.O_CREAT | os.O_RDWR | os.O_NOFOLLOW, 0o600)
    try:
        fcntl.flock(descriptor, fcntl.LOCK_EX | fcntl.LOCK_NB)
        yield
    finally:
        os.close(descriptor)


def load_state(directory):
    directory = Path(directory)
    state = json.loads(regular(directory / 'lifecycle.json').read_text())
    require(state['schema_version'] == VERSION and state['directory'] == str(directory)
            and state['owner_uid'] == os.getuid(), 'lifecycle ownership mismatch')
    require(re.fullmatch('[0-9a-f]{32}', state['id']) is not None, 'invalid lifecycle identity')
    require(state['quota_root'] == str(directory / 'quota'), 'quota ownership mismatch')
    for container in state['containers']:
        require(container['name'].startswith('layerx-artifacts-' + state['id'] + '-'), 'foreign container record')
    return state


def quota_state(directory):
    root = Path(directory) / 'quota'
    slot = root / 'slot-0'
    info = slot.stat()
    require(info.st_dev != root.stat().st_dev, 'quota slot is not a distinct filesystem')
    require((info.st_uid, info.st_gid, stat.S_IMODE(info.st_mode)) == (4030, 4030, 0o700), 'quota ownership mismatch')
    mount = json.loads(run(['findmnt', '--json', '--first-only', '--mountpoint', slot,
                            '--output', 'SOURCE,FSTYPE,OPTIONS,TARGET']).stdout)['filesystems'][0]
    require(mount['fstype'] == 'ext4' and mount['target'] == str(slot), 'quota filesystem mismatch')
    require(set(('rw', 'nosuid', 'nodev', 'noatime')).issubset(mount['options'].split(',')), 'quota mount flags mismatch')
    require('noexec' not in mount['options'].split(','), 'quota execution was disabled')
    loop = mount['source']
    require(re.fullmatch('/dev/loop[0-9]+', loop) is not None, 'quota backing is not an owned loop')
    backing = run(['losetup', '-n', '-O', 'BACK-FILE', loop]).stdout.strip()
    require(Path(backing).resolve() == (root / 'slot-0.ext4').resolve(), 'foreign loop backing')
    require(run(['losetup', '-n', '-O', 'AUTOCLEAR', loop]).stdout.strip() == '1', 'loop autoclear missing')
    fs = os.statvfs(slot)
    require(fs.f_blocks * fs.f_frsize <= QUOTA_BYTES and fs.f_files <= QUOTA_INODES,
            'quota byte/inode ceiling exceeded')
    return {'mountpoint': str(slot), 'device': info.st_dev, 'loop': loop, 'backing': backing,
            'bytes': fs.f_blocks * fs.f_frsize, 'inodes': fs.f_files}


def prepare(manifest_path, directory):
    manifest = validate(manifest_path)
    directory = private_directory(directory, manifest['repo'])
    state = {'schema_version': VERSION, 'id': uuid.uuid4().hex, 'owner_uid': os.getuid(),
             'directory': str(directory), 'manifest': str(regular(manifest_path)),
             'source': manifest['source'], 'quota_root': str(directory / 'quota'),
             'phase': 'allocating', 'containers': [], 'jobs': [], 'mount': None}
    write_json(directory / 'lifecycle.json', state)
    with locked(directory):
        environment = {'PATH': os.environ.get('PATH', os.defpath), 'LANG': 'C.UTF-8'}
        environment.update(LAYERX_REGISTRY_NODE_QUOTA_ROOT=state['quota_root'],
                           LAYERX_REGISTRY_NODE_LOCK=str(directory / 'quota.lock'),
                           LAYERX_REGISTRY_MAX_BUILDS='1', LAYERX_REGISTRY_BUILD_QUOTA_BYTES=str(QUOTA_BYTES),
                           LAYERX_REGISTRY_BUILD_QUOTA_INODES=str(QUOTA_INODES))
        try:
            run(['sh', Path(manifest['repo']) / PROVISIONER], env=environment, timeout=120)
            state['mount'] = quota_state(directory)
            state['phase'] = 'prepared'
        except BaseException as error:
            state['phase'] = 'allocation_failed'
            state['error'] = str(error)
            raise
        finally:
            write_json(directory / 'lifecycle.json', state)
    return state


def inspect(directory):
    with locked(directory):
        state = load_state(directory)
        require(state['phase'] in ('prepared', 'running', 'interrupted'), 'lifecycle is not reusable')
        require(quota_state(directory) == state['mount'], 'retained quota identity mismatch')
        require(source(json.loads(regular(state['manifest']).read_text())['repo']) == state['source'],
                'retained fixture source mismatch')
        return state


def cgroup_for_pid(pid):
    entries = Path(f'/proc/{pid}/cgroup').read_text().splitlines()
    relative = next(line[3:] for line in entries if line.startswith('0::'))
    path = Path('/sys/fs/cgroup' + relative)
    require(path != Path('/sys/fs/cgroup') and path.resolve(strict=True) == path, 'host root cgroup refused')
    require(str(pid) in (path / 'cgroup.procs').read_text().split(), 'container PID cgroup mismatch')
    return path


def delegate(path, pid):
    require(set(CONTROLLERS).issubset((path / 'cgroup.controllers').read_text().split()), 'missing cgroup controller')
    main = path / 'main'
    main.mkdir()
    (main / 'cgroup.procs').write_text(str(pid))
    (path / 'cgroup.subtree_control').write_text('+cpu +memory +pids +io')
    workers = path / 'workers'
    workers.mkdir()
    (workers / 'cgroup.subtree_control').write_text('+cpu +memory +pids +io')
    for entry in [workers, workers / 'cgroup.procs', workers / 'cgroup.threads', workers / 'cgroup.subtree_control']:
        os.chown(entry, 4030, 4030)
    return workers


def run_job(directory, command, *, memory=67108864, pids=16, cpu=1000000, wall=3000,
            io_limit=104857600, mandatory=True, interrupt=False, unavailable=False):
    directory = Path(directory)
    with locked(directory):
        state = load_state(directory)
        require(state['phase'] == 'prepared', 'fixture is not prepared')
        require(quota_state(directory) == state['mount'], 'quota identity changed')
        manifest = json.loads(regular(state['manifest']).read_text())
        require(source(manifest['repo']) == state['source'], 'source identity changed')
        name = 'layerx-artifacts-' + state['id'] + '-' + uuid.uuid4().hex[:8]
        control = directory / name
        control.mkdir(mode=0o755)
        control.chmod(0o755)
        record = {'name': name, 'id': None, 'cgroup': None, 'phase': 'planned'}
        state['containers'].append(record)
        state['phase'] = 'running'
        write_json(directory / 'lifecycle.json', state)
        process = None
        started = time.monotonic()
        observations = []
        try:
            invocation = ['docker', 'create', '--name', name, '--network', 'none', '--cgroupns', 'private',
                          '--cpus', '1', '--memory', '256m', '--pids-limit', '128',
                          '--cap-drop', 'ALL', '--cap-add', 'SETUID', '--cap-add', 'SETGID',
                          '--security-opt', 'no-new-privileges',
                          '--mount', f'type=bind,src={control},dst=/control',
                          '--mount', f'type=bind,src={state["quota_root"]},dst=/quota',
                          '--mount', f'type=bind,src={manifest["builder_root"]},dst=/builder,readonly',
                          '--mount', 'type=bind,src=/sys/fs/cgroup,dst=/host-cgroup',
                          manifest['runtime_image'], '/bin/sh', '-c',
                          'while [ ! -f /control/command ]; do :; done; exec su -s /bin/sh layerx -c "$(cat /control/command)"']
            record['id'] = run(invocation).stdout.strip()
            write_json(directory / 'lifecycle.json', state)
            run(['docker', 'start', name])
            pid = int(run(['docker', 'inspect', '--format', '{{.State.Pid}}', name]).stdout)
            group = cgroup_for_pid(pid)
            record['cgroup'] = str(group)
            record['cgroup_inode'] = group.stat().st_ino
            write_json(directory / 'lifecycle.json', state)
            workers = delegate(group, pid)
            inside = '/host-cgroup/' + str(workers.relative_to('/sys/fs/cgroup'))
            if unavailable:
                inside += '/unavailable'
            supervisor = ['/usr/bin/layerx-cgroup-exec', '--cgroup-root', inside,
                          '--workspace-device-path', '/quota/slot-0', f'--memory-max={memory}',
                          f'--cpu-time-max-usec={cpu}', f'--pids-max={pids}', f'--io-write-max={io_limit}',
                          f'--wall-time-max-ms={wall}']
            if mandatory:
                supervisor += ['--cgroup-v2', '--attach-before-exec', '--kill-tree-on-exit']
            supervisor += ['--', *command]
            (control / 'command').write_text(shlex.join(supervisor))
            record['phase'] = 'executing'
            write_json(directory / 'lifecycle.json', state)
            process = subprocess.Popen(['docker', 'wait', name], stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
            deadline = time.monotonic() + wall / 1000 + 15
            interrupted = False
            while process.poll() is None:
                for job in workers.glob('job-*'):
                    sample = {}
                    for key in ('memory.max', 'memory.events', 'pids.max', 'pids.events', 'cpu.stat', 'io.stat', 'cgroup.procs'):
                        try:
                            sample[key] = (job / key).read_text()
                        except FileNotFoundError:
                            pass
                    if sample and len(observations) < 2000:
                        observations.append(sample)
                if interrupt and observations and not interrupted:
                    run(['docker', 'kill', name])
                    interrupted = True
                require(time.monotonic() < deadline, 'supervised job exceeded outer deadline')
                time.sleep(0.005)
            stdout, stderr = process.communicate(timeout=5)
            require(process.returncode == 0, 'docker wait failed: ' + stderr)
            exit_code = int(stdout.strip())
            logs = run(['docker', 'logs', name], check=False)
            result = {'exit_code': exit_code, 'stdout': logs.stdout, 'stderr': logs.stderr,
                      'elapsed': time.monotonic() - started, 'observations': observations,
                      'interrupted': interrupted, 'command': command}
            state['jobs'].append(result)
            return result
        finally:
            run(['docker', 'stop', '--time', '2', name], check=False)
            removed = run(['docker', 'rm', name], check=False)
            if process is not None and process.poll() is None:
                process.wait(timeout=10)
            group_name = record.get('cgroup')
            absent = not group_name or not Path(group_name).exists()
            record['phase'] = 'cleaned' if removed.returncode == 0 and absent else 'cleanup_failed'
            state['phase'] = 'prepared' if record['phase'] == 'cleaned' else 'interrupted'
            write_json(directory / 'lifecycle.json', state)
            require(record['phase'] == 'cleaned', 'owned job cleanup incomplete')


def startup_boundary(manifest, output):
    name = 'layerx-artifacts-startup-' + uuid.uuid4().hex
    try:
        result = run(['docker', 'run', '--name', name, '--network', 'none', '--cgroupns', 'private',
                      '--cap-drop', 'ALL', '--cap-add', 'CHOWN', '--cap-add', 'SETUID', '--cap-add', 'SETGID',
                      '--security-opt', 'no-new-privileges',
                      '--mount', 'type=bind,src=/sys/fs/cgroup,dst=/run/layerx/host-cgroup',
                      '-e', 'LAYERX_REGISTRY_HOST_CGROUP_MOUNT=/run/layerx/host-cgroup',
                      manifest['runtime_image'], '/usr/local/bin/layerx-program-registry'], check=False, timeout=30)
        write_json(output, {'exit_code': result.returncode, 'stderr': result.stderr, 'registry_ready': False})
        require(result.returncode != 0 and 'LAYERX_REGISTRY_BUILDER_IMAGE_DIGEST is required' in result.stderr,
                'production privilege-drop preconfiguration boundary was not reached: ' + result.stderr)
    finally:
        run(['docker', 'stop', '--time', '2', name], check=False)
        require(run(['docker', 'rm', name], check=False).returncode == 0, 'startup container cleanup failed')


def cleanup(directory):
    directory = Path(directory)
    with locked(directory):
        state = load_state(directory)
        state['phase'] = 'cleaning'
        write_json(directory / 'lifecycle.json', state)
        try:
            for container in state['containers']:
                if container['phase'] == 'cleaned':
                    continue
                if container.get('id'):
                    actual = run(['docker', 'inspect', '--format', '{{.Id}}', container['name']], check=False)
                    if actual.returncode == 0:
                        require(actual.stdout.strip() == container['id'], 'container ownership changed')
                        run(['docker', 'stop', '--time', '2', container['name']])
                        run(['docker', 'rm', container['name']])
                require(not container.get('cgroup') or not Path(container['cgroup']).exists(), 'owned cgroup remains')
                container['phase'] = 'cleaned'
            slot = directory / 'quota/slot-0'
            if run(['mountpoint', '-q', slot], check=False).returncode == 0:
                mount = quota_state(directory)
                if state['mount'] is not None:
                    require(mount == state['mount'], 'refusing cleanup of replaced mount')
                state['mount'] = mount
                write_json(directory / 'lifecycle.json', state)
                run(['systemd-mount', '--umount', slot])
            require(run(['mountpoint', '-q', slot], check=False).returncode != 0, 'quota mount remains')
            image = directory / 'quota/slot-0.ext4'
            require(not run(['losetup', '-j', image]).stdout.strip(), 'owned loop remains attached')
            state['phase'] = 'cleaned'
            state['cleanup'] = {'mount_absent': True, 'loop_absent': True, 'containers_absent': True}
        except BaseException as error:
            state['phase'] = 'cleanup_failed'
            state['cleanup_error'] = str(error)
            raise
        finally:
            write_json(directory / 'lifecycle.json', state)
    return state


def main():
    os.umask(0o077)
    parser = argparse.ArgumentParser(description='Source-bound registry artifacts and owned isolation lifecycle; no registry readiness claim.')
    sub = parser.add_subparsers(dest='command', required=True)
    build_parser = sub.add_parser('build')
    build_parser.add_argument('--repo', required=True)
    build_parser.add_argument('--output', required=True)
    for name in ('validate', 'prepare'):
        command = sub.add_parser(name)
        command.add_argument('--manifest', required=True)
        if name == 'prepare':
            command.add_argument('--directory', required=True)
    for name in ('inspect', 'cleanup'):
        sub.add_parser(name).add_argument('--directory', required=True)
    args = parser.parse_args()
    if args.command == 'build':
        print(build(args.repo, args.output))
    elif args.command == 'validate':
        print(json.dumps(validate(args.manifest), sort_keys=True))
    elif args.command == 'prepare':
        print(json.dumps(prepare(args.manifest, args.directory), sort_keys=True))
    elif args.command == 'inspect':
        print(json.dumps(inspect(args.directory), sort_keys=True))
    else:
        print(json.dumps(cleanup(args.directory), sort_keys=True))


if __name__ == '__main__':
    try:
        main()
    except (RuntimeError, OSError, ValueError, KeyError, subprocess.SubprocessError) as error:
        raise SystemExit('registry-artifacts: ' + str(error))
