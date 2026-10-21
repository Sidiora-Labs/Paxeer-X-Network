import argparse
import contextlib
import fcntl
import os
import stat
import shutil
import subprocess
from pathlib import Path
from policy import load, require, sha, verified_tool_bytes
TOOLS = {'go': (['version'], 'go1.26.4'), 'node': (['--version'], 'v24.16.0'), 'pnpm': (['--version'], '9.15.0')}

def read_pins(path, expected_sha):
    require(sha(expected_sha), 'missing reviewed toolchain manifest digest')
    path = Path(path)
    require(path.is_absolute() and path.is_file(), 'missing absolute toolchain manifest')
    with path.open('rb') as source:
        data = source.read(4 * 1024 * 1024 + 1)
    require(len(data) <= 4 * 1024 * 1024, 'toolchain manifest too large')
    verified_tool_bytes(data, expected_sha)
    pins = load(data)
    require(type(pins) is dict and set(pins) == {'schema_version', 'tools'}, 'invalid toolchain manifest')
    require(type(pins['schema_version']) is int and pins['schema_version'] == 1, 'invalid toolchain manifest version')
    require(type(pins['tools']) is dict and set(pins['tools']) == set(TOOLS), 'incomplete toolchain manifest')
    for pin in pins['tools'].values():
        require(type(pin) is dict and set(pin) == {'path', 'sha256'}, 'invalid toolchain pin')
        require(isinstance(pin['path'], str) and Path(pin['path']).is_absolute() and sha(pin['sha256']), 'invalid toolchain identity')
    return pins['tools']

def verified_executable(path, expected_sha):
    path = Path(path)
    require(path.is_absolute(), 'tool executable must be absolute')
    with path.open('rb') as executable:
        metadata = os.fstat(executable.fileno())
        require(stat.S_ISREG(metadata.st_mode) and metadata.st_mode & 73, 'tool is not executable')
        require(0 < metadata.st_size <= 256 * 1024 * 1024, 'invalid tool executable size')
        data = executable.read(256 * 1024 * 1024 + 1)
    verified_tool_bytes(data, expected_sha)
    return str(path.resolve())

@contextlib.contextmanager
def execution_snapshot(path, expected_sha):
    path = Path(path)
    require(path.is_absolute(), 'tool executable must be absolute')
    require(hasattr(os, 'memfd_create'), 'immutable tool execution requires Linux memfd')
    descriptor = os.memfd_create('reviewed-executable', os.MFD_ALLOW_SEALING | os.MFD_CLOEXEC)
    try:
        with path.open('rb') as source:
            metadata = os.fstat(source.fileno())
            require(stat.S_ISREG(metadata.st_mode) and metadata.st_mode & 73, 'tool is not executable')
            require(0 < metadata.st_size <= 256 * 1024 * 1024, 'invalid tool executable size')
            data = source.read(256 * 1024 * 1024 + 1)
        verified_tool_bytes(data, expected_sha)
        with os.fdopen(os.dup(descriptor), 'wb') as target:
            target.write(data)
            target.flush()
        os.fchmod(descriptor, 0o500)
        fcntl.fcntl(descriptor, fcntl.F_ADD_SEALS, fcntl.F_SEAL_WRITE | fcntl.F_SEAL_GROW | fcntl.F_SEAL_SHRINK | fcntl.F_SEAL_SEAL)
        yield '/proc/self/fd/' + str(descriptor), descriptor
    finally:
        os.close(descriptor)


@contextlib.contextmanager
def materialized_pnpm_bundle(pin):
    require(type(pin) is dict and set(pin) == {'path', 'sha256'} and type(pin['path']) is str and Path(pin['path']).is_absolute() and sha(pin['sha256']), 'invalid reviewed pnpm bundle identity')
    source = Path(pin['path'])
    require(source.is_file() and not source.is_symlink(), 'missing direct standalone pnpm bundle')
    with source.open('rb') as bundle:
        metadata = os.fstat(bundle.fileno())
        require(stat.S_ISREG(metadata.st_mode) and 0 < metadata.st_size <= 256 * 1024 * 1024, 'invalid standalone pnpm bundle size')
        data = bundle.read(256 * 1024 * 1024 + 1)
    verified_tool_bytes(data, pin['sha256'])
    with __import__('tempfile').TemporaryDirectory(prefix='reviewed-pnpm-executable-') as temporary:
        executable = Path(temporary) / 'pnpm'
        with executable.open('xb') as output:
            output.write(data)
            output.flush()
            os.fsync(output.fileno())
        executable.chmod(0o500)
        require(verified_executable(executable, pin['sha256']) == str(executable), 'managed pnpm executable checksum mismatch')
        yield {'path': str(executable), 'sha256': pin['sha256']}


def verify_materialized_tools(pins, env=None):
    with materialized_pnpm_bundle(pins['pnpm']) as bundle:
        selected = {**pins, 'pnpm': bundle}
        environment = dict(os.environ if env is None else env)
        directories = [str(Path(selected[name]['path']).resolve().parent) for name in ('pnpm', 'node', 'go')]
        environment['PATH'] = os.pathsep.join(directories + [environment.get('PATH', '')])
        result = verify_tools(selected, env=environment, pnpm_bundle=bundle)
        result['pnpm']['reviewed_source_path'] = pins['pnpm']['path']
        result['pnpm']['materialization_mode'] = '0500'
        result['pnpm']['ephemeral_executable'] = True
        return result

def reviewed_go_environment(path, env=None):
    executable = Path(path).resolve()
    root = executable.parent.parent
    require(executable.parent.name == 'bin' and (root / 'src' / 'runtime' / 'runtime2.go').is_file() and (root / 'VERSION').is_file(), 'reviewed Go installation is incomplete')
    environment = dict(os.environ if env is None else env)
    environment['GOROOT'] = str(root)
    environment['GOTOOLCHAIN'] = 'local'
    return environment

def verify_tools(pins, env=None, pnpm_bundle=None):
    executables = {name: verified_executable(pin['path'], pin['sha256']) for name, pin in pins.items()}
    for name, executable in executables.items():
        selected = shutil.which(name, path=(env or os.environ).get('PATH'))
        require(selected is not None and str(Path(selected).resolve()) == executable, 'toolchain PATH mismatch: ' + name)
    for name, (arguments, expected) in TOOLS.items():
        environment = reviewed_go_environment(executables[name], env) if name == 'go' else env
        if name == 'pnpm' and pnpm_bundle is not None:
            with execution_snapshot(executables['node'], pins['node']['sha256']) as (node, node_descriptor), execution_snapshot(pnpm_bundle['path'], pnpm_bundle['sha256']) as (bundle, bundle_descriptor):
                actual = subprocess.check_output([node, '--preserve-symlinks-main', bundle, *arguments], pass_fds=(node_descriptor, bundle_descriptor), text=True, timeout=10, env=environment).strip()
        else:
            with execution_snapshot(executables[name], pins[name]['sha256']) as (snapshot, descriptor):
                actual = subprocess.check_output([snapshot, *arguments], pass_fds=(descriptor,), text=True, timeout=10, env=environment).strip()
        require(expected in actual.split(), 'unreviewed toolchain: ' + name)
    return {name: {'path': executables[name], 'sha256': pins[name]['sha256']} for name in TOOLS}

def run_reviewed_tool(pins, name, arguments, *, cwd=None, timeout=1800, env=None):
    require(name in TOOLS and type(arguments) is list and arguments and all(type(value) is str and '\x00' not in value for value in arguments), 'invalid reviewed tool invocation')
    require(type(timeout) is int and 1 <= timeout <= 1800, 'unbounded reviewed tool invocation')
    environment = dict(os.environ if env is None else env)
    if name == 'go':
        environment = reviewed_go_environment(pins['go']['path'], environment)
    if name == 'pnpm':
        with materialized_pnpm_bundle(pins['pnpm']) as bundle, execution_snapshot(pins['node']['path'], pins['node']['sha256']) as (node, node_descriptor), execution_snapshot(bundle['path'], bundle['sha256']) as (script, script_descriptor):
            result = subprocess.run([node, '--preserve-symlinks-main', script, *arguments], pass_fds=(node_descriptor, script_descriptor), cwd=cwd, env=environment, timeout=timeout, check=False)
    else:
        with execution_snapshot(pins[name]['path'], pins[name]['sha256']) as (executable, descriptor):
            result = subprocess.run([executable, *arguments], pass_fds=(descriptor,), cwd=cwd, env=environment, timeout=timeout, check=False)
    require(result.returncode == 0, 'reviewed ' + name + ' command exited ' + str(result.returncode))
    return result.returncode

def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--manifest', required=True)
    parser.add_argument('--manifest-sha256', required=True)
    parser.add_argument('--materialize-pnpm-bundle', action='store_true')
    parser.add_argument('--tool', choices=sorted(TOOLS))
    parser.add_argument('--cwd')
    parser.add_argument('--timeout-seconds', type=int, default=1800)
    parser.add_argument('arguments', nargs=argparse.REMAINDER)
    args = parser.parse_args()
    pins = read_pins(args.manifest, args.manifest_sha256)
    if args.tool is not None:
        arguments = args.arguments[1:] if args.arguments[:1] == ['--'] else args.arguments
        run_reviewed_tool(pins, args.tool, arguments, cwd=args.cwd, timeout=args.timeout_seconds)
        return
    require(not args.arguments, 'unexpected tool verification arguments')
    if args.materialize_pnpm_bundle:
        verify_materialized_tools(pins)
    else:
        verify_tools(pins)
if __name__ == '__main__':
    main()
