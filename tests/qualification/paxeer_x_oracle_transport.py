#!/usr/bin/env python3
"""Build and qualify the signed oracle transport and its selected native ABI."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import time

ROOT = Path(__file__).resolve().parents[2]
COMMAND = 'timeout 1800s python3 tests/qualification/paxeer_x_oracle_transport.py'
COMPONENTS = ['test_oracle_adapter', 'test_oracle_intake', 'test_oracle_checks',
              'test_oracle_halt', 'test_oracle_replay_absent', 'test_genesis_module_table']
SOURCES = ['include/layerx/lx_oracle.h', 'include/layerx/lx_perps.h', 'include/layerx/lxp_genesis.h',
           'src/network/lx_oracle_adapter.c', 'src/modules/perps/lx_perps_command.c',
           'src/modules/perps/lx_perps_dispatch.c', 'src/protocol/lxp_genesis.c',
           'platform/hosted/node/bootstrap.sh', 'tests/network/test_oracle_adapter.c',
           'tests/test_genesis_module_table.c', 'tests/modules/test_oracle_transport.c',
           'tests/daemon/lxp_test_oracle_transport_client.c', 'tests/qualification/paxeer_x_oracle_transport.py']

def require(ok, reason):
    if not ok:
        raise RuntimeError(reason)

def run(argv, **kw):
    return subprocess.run([str(a) for a in argv], cwd=ROOT, check=True, **kw)

def digest(path):
    with Path(path).open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()

def identity():
    require(not run(['git', 'status', '--porcelain'], capture_output=True, text=True).stdout.strip(), 'dirty source')
    return run(['git', 'rev-parse', 'HEAD'], capture_output=True, text=True).stdout.strip()

def directory():
    p = Path(os.environ['PAXEER_X_ORACLE_BUILD_DIR'])
    require(p.is_absolute() and p.is_dir() and not p.is_symlink() and p.stat().st_mode & 0o077 == 0, 'private build directory required')
    return p

def build():
    out = directory()
    revision = identity()
    commands = []
    def produce(argv, **kw):
        commands.append([str(a) for a in argv])
        print('BUILD ' + json.dumps(commands[-1]), flush=True)
        run(argv, **kw)
    produce([sys.executable, 'tools/bringup/tests/foundation-artifacts.py', '--build', '--output', out / 'foundation'])
    native = ROOT / 'build/liblayerx.a'
    programs = ROOT / 'programs/target/debug/liblayerx_programs_sandbox.a'
    produce(['make', '-j5', *('build/tests/' + name for name in COMPONENTS)])
    env = dict(os.environ, PAXEER_X_RUNTIME_NATIVE_LIBRARY=str(native), PAXEER_X_RUNTIME_NATIVE_REVISION=revision,
               PAXEER_X_RUNTIME_PROGRAMS_LIBRARY=str(programs), PAXEER_X_RUNTIME_EVIDENCE=str(out / 'common'))
    produce(['bash', 'tools/paxeer-x/gates/24.14.sh', 'build'], env=env)
    binaries = {}
    for name in COMPONENTS:
        shutil.copyfile(ROOT / 'build/tests' / name, out / name)
        (out / name).chmod(0o500)
        binaries[name] = {'path': str(out / name), 'sha256': digest(out / name)}
    for name, source in [('oracle-transport', 'tests/modules/test_oracle_transport.c'),
                         ('oracle-client', 'tests/daemon/lxp_test_oracle_transport_client.c')]:
        produce(['cc', '-std=c17', '-O2', '-ffunction-sections', '-fdata-sections', '-Iinclude', '-Itests/daemon', source,
                 '-Wl,--gc-sections', '-Wl,--start-group', native, programs, '-Wl,--end-group',
                 '-lcrypto', '-lsqlite3', '-pthread', '-ldl', '-lm', '-o', out / name])
        (out / name).chmod(0o500)
        binaries[name] = {'path': str(out / name), 'sha256': digest(out / name)}
    require(identity() == revision, 'source changed during build')
    value = {'revision': revision, 'tree': run(['git', 'rev-parse', 'HEAD^{tree}'], capture_output=True, text=True).stdout.strip(),
             'sources': {p: digest(ROOT / p) for p in SOURCES}, 'binaries': binaries, 'commands': commands, 'exit_code': 0}
    (out / 'targets.json').write_text(json.dumps(value, indent=2) + '\n')
    (out / 'targets.json').chmod(0o600)

def targets():
    out = directory()
    path = out / 'targets.json'
    require(path.stat().st_mode & 0o077 == 0 and not path.is_symlink(), 'unprotected target manifest')
    value = json.loads(path.read_text())
    require(value['revision'] == identity() and value['exit_code'] == 0, 'prebuilt revision mismatch')
    require(value['sources'] == {p: digest(ROOT / p) for p in SOURCES}, 'prebuilt source digest mismatch')
    require(set(value['binaries']) == set(COMPONENTS + ['oracle-transport', 'oracle-client']), 'incomplete prebuilt targets')
    for item in value['binaries'].values():
        p = Path(item['path'])
        require(p.parent == out and not p.is_symlink() and os.access(p, os.X_OK) and digest(p) == item['sha256'], 'prebuilt binary mismatch')
    return value

def runtime_import():
    sys.path.insert(0, str(ROOT / 'tests/daemon'))
    import paxeer_x_runtime_fixture as fixture
    return fixture

def rows(raw, prefix):
    return [dict(x.split('=', 1) for x in line.split()[1:]) for line in raw.decode().splitlines() if line.startswith(prefix + ' ')]

def worker(base):
    fixture = runtime_import()
    built = targets()
    out = directory()
    bundle = fixture.artifacts(out / 'foundation/manifest.json')
    client = fixture.client_artifact(out / 'common/client-manifest.json')
    for name in ('net', 'pid', 'mnt'):
        require(os.readlink('/proc/self/ns/' + name) != os.environ['ORACLE_PARENT_' + name.upper()], 'namespace not isolated')
    fixture.run(['ip', 'link', 'set', 'lo', 'up'])
    fixture.run(['mount', '-t', 'tmpfs', '-o', 'mode=0755,nosuid,nodev', 'oracle-fixture', '/tmp'])
    source = Path('/tmp/oracle-source'); source.mkdir(mode=0o755)
    fixture.run(['mount', '--bind', ROOT, source])
    fixture.run(['mount', '-o', 'remount,bind,ro,nosuid,nodev', source])
    fixture.ROOT = source
    python_root = Path('/tmp/oracle-python'); python_root.mkdir(mode=0o755)
    fixture.run(['mount', '--bind', sys.prefix, python_root])
    fixture.run(['mount', '-o', 'remount,bind,ro,nosuid,nodev', python_root])
    os.environ['PATH'] = str(python_root / 'bin') + ':/usr/local/bin:/usr/bin:/bin'
    class ActivatedFixture(fixture.RuntimeFixture):
        def produce(self, label, argv, env=None, timeout=120):
            if label == 'bootstrap':
                argv = [*argv, '--perps-oracle-transport', '1']
            return super().produce(label, argv, env, timeout)
    cases = []
    for activated in (False, True):
        d = base / ('activated' if activated else 'legacy')
        runtime = (ActivatedFixture if activated else fixture.RuntimeFixture)(d, bundle, client)
        retained = []
        try:
            runtime.generate()
            wires = d / 'activities'; wires.mkdir(mode=0o700)
            sequence, bob_sequence = 0, 0
            def invoke(operation, expected=0, observation_sequence=2, replay=None, actor='treasury'):
                nonlocal sequence, bob_sequence
                seq = bob_sequence if actor == 'bob' else sequence
                path = replay or wires / (operation + '-' + str(seq) + '-' + str(time.time_ns()))
                command = [built['binaries']['oracle-client']['path'], str(d / 'run/layerxd.lni.sock'), str(d / 'salt'),
                           'replay' if replay else operation, str(seq), str(observation_sequence), str(path), str(expected)]
                env = runtime.env | {'PAXEER_X_FIXTURE_KEYS': str(d / 'keys'), 'ORACLE_ASSET': fixture.ASSET,
                    'ORACLE_CREDIT_PROFILE': str(d / 'custody.profile'), 'ORACLE_CREDIT_FILE': str(d / (operation + '.credit'))}
                result = subprocess.run(command, env=env, capture_output=True, timeout=45)
                (d / (operation + '-' + str(time.time_ns()) + '.log')).write_bytes(result.stdout + result.stderr)
                require(result.returncode == 0, 'oracle client failed: ' + operation + ', fixture=' + str(d))
                got = rows(result.stdout, 'receipt')
                ingress = operation.startswith('ingress-') and replay is None
                if ingress:
                    require(len(rows(result.stdout, 'refusal')) == 1 and not got, 'missing ingress refusal')
                    return None, path, []
                require(len(got) == 1 and int(got[0]['result']) == expected, 'wrong receipt result')
                if replay is None:
                    retained.extend(got)
                    if actor == 'bob': bob_sequence += 1
                    else: sequence += 1
                return got[0], path, rows(result.stdout, 'observation')
            def state(include_oracle=False, bob=False):
                op = 'read-bob' if bob else 'read-state' if include_oracle else 'read-accounts'
                result = subprocess.run([built['binaries']['oracle-client']['path'], str(d / 'run/layerxd.lni.sock'), str(d / 'salt'), op, '0', '0', '/dev/null', '0'],
                    env=runtime.env | {'PAXEER_X_FIXTURE_KEYS': str(d / 'keys'), 'ORACLE_ASSET': fixture.ASSET}, capture_output=True, timeout=20)
                (d / ('state-' + str(time.time_ns()) + '.log')).write_bytes(result.stdout + result.stderr)
                require(result.returncode == 0, 'authenticated state read failed: ' + str(d))
                got = rows(result.stdout, 'state')
                require(len(got) == (3 if include_oracle else 2) and len({r['root'] for r in got}) == 1, 'inconsistent signed state head')
                return {r['label']: bytes.fromhex(r['raw']) for r in got}
            def balance(raw):
                n = int.from_bytes(raw[:2], 'big')
                return int.from_bytes(raw[3+n:19+n], 'big')
            from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey
            from cryptography.hazmat.primitives.serialization import Encoding, PublicFormat
            for actor, operation in [('treasury', 'credit'), ('bob', 'credit-bob')]:
                public = Ed25519PrivateKey.from_private_bytes((d / ('keys/' + actor + '.seed')).read_bytes()).public_key().public_bytes(Encoding.Raw, PublicFormat.Raw)
                did = ('did:layerx:' + public.hex()).encode()
                account = b'agent:' + did + b':main'
                beneficiary = hashlib.sha256(b'LX:ACCOUNT:v1' + len(account).to_bytes(4, 'big') + account).digest()
                amount = 1000000
                runtime.produce(operation + '-deposit', ['python3', fixture.ROOT / 'platform/hosted/paxeer/evm.py', 'send',
                    '--rpc', runtime.rpc_url, '--chain', '125', '--key-file', d / 'keys/deployer.key', '--value', str(amount * 10**12),
                    '0x0000000000000000000000000000000000001013', 'deposit(bytes32)', '0x' + beneficiary.hex()])
                deposited = json.loads((d / (operation + '-deposit.log')).read_text())
                require(int(deposited['status'], 16) == 1, 'native deposit reverted')
                logs = [e for e in deposited['logs'] if e['address'].lower() == '0x0000000000000000000000000000000000001013' and len(e['topics']) == 4]
                require(len(logs) == 1, 'deposit event count')
                entry = logs[0]; data = bytes.fromhex(entry['data'].removeprefix('0x'))
                require(entry['topics'][2].removeprefix('0x').lower() == fixture.ASSET and data[:32] == beneficiary and int.from_bytes(data[32:64], 'big') == amount, 'deposit binding')
                runtime.wait(lambda: int(runtime.rpc('eth_blockNumber'), 16) >= int(deposited['blockNumber'], 16) + 2)
                runtime.produce(operation + '-proof', [runtime.binary('layerx-custody-proof'), 'light-credit', '--rpc', 'http://127.0.0.1:' + str(runtime.ports[2]),
                    '--profile', d / 'custody.profile', '--deposit-id', entry['topics'][1], '--owner-key', '0x' + public.hex(), '--output', d / (operation + '.credit')])
                credit = (d / (operation + '.credit')).read_bytes()
                require(len(credit) > 363 and credit[:5] == b'LXDC3' and credit[43:75] == bytes.fromhex(entry['topics'][1][2:]) and credit[75:107].hex() == fixture.ASSET and credit[107:139] == beneficiary and credit[139:171] == public and int.from_bytes(credit[191:207], 'big') == amount, 'credit proof binding')
                invoke(operation, actor=actor)
                require(balance(state(bob=actor == 'bob')['owner']) == amount, 'native funding balance')
            prefix = 'activated-' if activated else 'legacy-'
            cases.append(prefix + 'real-custody-funding')
            before = state()
            insurance, _, _ = invoke('insurance')
            after = state()
            require(balance(after['insurance']) - balance(before['insurance']) == 10000 and
                    balance(before['owner']) - balance(after['owner']) == 10000 + int(insurance['fee']), 'insurance conservation and fee')
            cases.append(prefix + 'real-insurance-funding')
            market, _, _ = invoke('market')
            require(int(market['module']) == (2 if activated else 1) and int(market['fee']) > 0, 'selected market ABI or production fee')
            if not activated:
                for operation, expected in [('legacy72', -729), ('versioned', -3)]:
                    original, wire, _ = invoke(operation, expected)
                    require(int(original['module']) == 1, 'legacy receipt ABI changed')
                    unchanged = state()
                    runtime.restart(kill=operation == 'versioned')
                    repeated, _, _ = invoke(operation, expected, replay=wire)
                    require(original['raw'] == repeated['raw'] and state() == unchanged, 'legacy terminal replay changed')
                    cases.append(prefix + operation + '-terminal-replay')
            else:
                original, wire, observations = invoke('oracle', observation_sequence=1)
                require(len(observations) == 1 and int(original['module']) == 2 and int(original['fee']) > 0, 'adapter positive evidence')
                snapshot = state(True); value = snapshot['oracle']; observation = observations[0]
                require(len(value) == 72 and int.from_bytes(value[:8], 'big') == 1 and int.from_bytes(value[8:24], 'big') == 100 and
                        int.from_bytes(value[24:32], 'big') == int(observation['time']) and int.from_bytes(value[32:40], 'big') == 1 and value[40:].hex() == observation['key'], 'committed observation differs')
                for kill in (False, True):
                    runtime.restart(kill=kill)
                    repeated, _, _ = invoke('oracle', replay=wire)
                    require(repeated['raw'] == original['raw'] and state(True) == snapshot, 'oracle restart retry changed receipt/state/balance')
                    cases.append(prefix + ('forced' if kill else 'clean') + '-restart')
                advanced, _, _ = invoke('oracle', observation_sequence=3)
                require(int.from_bytes(state(True)['oracle'][:8], 'big') == 3, 'second observation not committed')
                cases.append(prefix + 'adapter-monotonic-update')
                negatives = [('ingress-outer', -201), ('ingress-network', -100), ('ingress-protocol', -101),
                    ('ingress-actor', -200), ('ingress-expired', -303), ('bad-inner', -729), ('key-mismatch', -729),
                    ('version-zero', -3), ('version-unknown', -3), ('truncated', -3), ('overlong', -3), ('legacy72', -3),
                    ('zero-market', -3), ('zero-price', -3), ('zero-sequence', -3), ('zero-time', -3), ('zero-source', -3),
                    ('stale', -704), ('future', -802), ('bounds-low', -731), ('bounds-high', -731), ('deviation', -732), ('duplicate', -730), ('regressed', -730), ('fee', -600)]
                for operation, expected in negatives:
                    before = state(True)
                    receipt, _, _ = invoke(operation, expected, observation_sequence=3 if operation == 'duplicate' else 2 if operation == 'regressed' else 4)
                    after = state(True)
                    require(after['oracle'] == before['oracle'] and after['insurance'] == before['insurance'], 'negative mutated oracle or insurance: ' + operation)
                    if receipt is None:
                        require(after == before, 'ingress refusal mutated state: ' + operation)
                    else:
                        require(balance(before['owner']) - balance(after['owner']) == int(receipt['fee']), 'negative fee mismatch: ' + operation)
                    cases.append(prefix + operation)
                before = state(True)
                invoke('disallowed-key', -729, actor='bob')
                after = state(True)
                require(after['oracle'] == before['oracle'] and after['insurance'] == before['insurance'], 'disallowed oracle mutated state')
                cases.append(prefix + 'disallowed-key')
            runtime.catch_up(retained)
            cases.append(prefix + 'authenticated-replica-catchup')
        finally:
            runtime.cleanup()
    (base / 'result.json').write_text(json.dumps({'cases': cases, 'skipped': 0}, indent=2) + '\n')

def qualify():
    out = directory()
    evidence = Path(os.environ['PAXEER_X_EVIDENCE_DIR'])
    evidence.mkdir(mode=0o700, parents=True, exist_ok=True)
    result = {'task': '4.4', 'command': COMMAND, 'cases': [], 'tests': 0, 'skipped': 0, 'exit_code': 1}
    try:
        built = targets(); result['revision'] = built['revision']
        for name in COMPONENTS + ['oracle-transport']:
            log = evidence / (name + '.log')
            with log.open('wb') as stream:
                run([built['binaries'][name]['path']], stdout=stream, stderr=stream, timeout=120)
            result['cases'].append(name)
        run(['sh', 'tools/lx_oracle_adapter_isolation.sh'])
        symbols = run(['nm', built['binaries']['test_oracle_replay_absent']['path']], capture_output=True, text=True).stdout
        require('lx_oracle_adapter_run' not in symbols, 'replay linked external adapter')
        result['cases'].append('replay-network-isolation')
        base = Path(tempfile.mkdtemp(prefix='px-oracle-', dir='/var/tmp'))
        result['runtime_directory'] = str(base)
        env = dict(os.environ)
        for name in ('net', 'pid', 'mnt'):
            env['ORACLE_PARENT_' + name.upper()] = os.readlink('/proc/self/ns/' + name)
        with (evidence / 'runtime.log').open('wb') as stream:
            run(['unshare', '--mount', '--net', '--pid', '--fork', '--kill-child=KILL', '--mount-proc', '--propagation', 'private',
                 sys.executable, str(Path(__file__).resolve()), '--worker', base], env=env, stdout=stream, stderr=stream, timeout=900)
        runtime = json.loads((base / 'result.json').read_text())
        require(runtime['skipped'] == 0, 'runtime skipped a case')
        result['cases'].extend(runtime['cases']); result['exit_code'] = 0
    except (OSError, ValueError, KeyError, RuntimeError, subprocess.SubprocessError) as error:
        result['failure'] = str(error)
        print(str(error), file=sys.stderr)
    result['tests'] = len(result['cases'])
    path = evidence / 'oracle-transport-result.json'
    path.write_text(json.dumps(result, indent=2) + '\n'); path.chmod(0o600)
    print('PAXEER_X_GATE tests=' + str(result['tests']) + ' skipped=0')
    print('revision=' + result.get('revision', 'unknown') + ' command=' + COMMAND + ' exit_code=' + str(result['exit_code']) + ' log=' + str(path))
    return result['exit_code']

def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--build', action='store_true')
    parser.add_argument('--worker', type=Path)
    args = parser.parse_args()
    os.umask(0o077)
    if args.build:
        build(); return 0
    if args.worker:
        worker(args.worker); return 0
    return qualify()

if __name__ == '__main__':
    sys.exit(main())
