#!/usr/bin/env python3
"""Build or qualify budget partial defunding and revocation Activities."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import time
import shutil

ROOT = Path(__file__).resolve().parents[2]
TARGET = ROOT / "build/paxeer-x-3.1"
BINARY = TARGET / "tests/test_budget_close"
MANIFEST = TARGET / "budget-lifecycle.json"
SOURCES = (
    "include/layerx/lx_budget.h",
    "src/modules/budget/lx_budget_module.c",
    "src/modules/budget/lx_budget_close.c",
    "src/modules/budget/lx_budget_codec.c",
    "src/protocol/lxp_kernel.c",
    "tests/modules/test_budget_close.c",
    "tests/modules/paxeer_x_budget_lifecycle.py",
    "tests/daemon/lxp_test_budget_lifecycle.c",
    "tests/daemon/lxp_test_runtime_fixture.c",
    "tests/daemon/lxp_test_program_admission.c",
    "tests/daemon/paxeer_x_runtime_fixture.py",
)
REQUIRED = {
    "helper-path", "codec-defund", "codec-defund-length", "codec-defund-version",
    "codec-revoke", "codec-revoke-zero-sequence", "activity-ids-distinct",
    "defund-unauthorized", "revoke-unauthorized", "defund-overdraw",
    "defund-u128-max", "defund-zero", "close-distinct-payload",
    "defund-rejects-revoke-payload", "defund-partial", "defund-duplicate",
    "spend-after-defund-clamped", "revoke-stale-sequence", "spend-after-rollover",
    "revoke", "revoke-duplicate", "revoke-after-revoke", "spend-after-revoke",
    "defund-after-revoke", "fund-after-revoke", "record-durable-roundtrip",
    "replica-replay-identical", "defund-overflow-encoding",
}
COMMAND = ["timeout", "1800s", "python3", "tests/modules/paxeer_x_budget_lifecycle.py"]


def digest(path):
    with path.open("rb") as source:
        return hashlib.file_digest(source, "sha256").hexdigest()


def candidate():
    revision = subprocess.run(["git", "rev-parse", "HEAD"], cwd=ROOT, check=True,
                              capture_output=True, text=True).stdout.strip()
    dirty = subprocess.run(["git", "status", "--porcelain", "--", *SOURCES], cwd=ROOT,
                           check=True, capture_output=True, text=True).stdout.splitlines()
    return revision, {name: digest(ROOT / name) for name in SOURCES}, dirty


def build():
    revision, sources, _ = candidate()
    TARGET.mkdir(parents=True, exist_ok=True)
    MANIFEST.unlink(missing_ok=True)
    command = ["make", "-j5", "BUILD_DIR=build/paxeer-x-3.1", f"LXP_REVISION={revision}",
               "build/paxeer-x-3.1/tests/test_budget_close"]
    print("BUILD " + json.dumps(command), flush=True)
    subprocess.run(command, cwd=ROOT, check=True)
    archive = os.environ["PAXEER_X_RUNTIME_NATIVE_LIBRARY"]
    programs = os.environ["PAXEER_X_RUNTIME_PROGRAMS_LIBRARY"]
    subprocess.run(["cc", "-std=c17", "-O2", "-ffunction-sections", "-fdata-sections", "-Iinclude", "-Itests/daemon",
        "tests/daemon/lxp_test_budget_lifecycle.c", "-Wl,--gc-sections", "-Wl,--start-group", archive, programs,
        "-Wl,--end-group", "-lcrypto", "-lsqlite3", "-pthread", "-ldl", "-lm", "-o", str(TARGET / "budget-client")], cwd=ROOT, check=True)
    if candidate()[1] != sources:
        raise RuntimeError("candidate sources changed during build")
    MANIFEST.write_text(json.dumps({"revision": revision, "sources": sources,
        "binary_sha256": digest(BINARY), "driver_sha256": digest(TARGET / "budget-client"), "command": command}, indent=2) + "\n")


def qualify():
    evidence = Path(os.environ["PAXEER_X_EVIDENCE_DIR"]) if "PAXEER_X_EVIDENCE_DIR" in os.environ \
        else Path(tempfile.mkdtemp(prefix="paxeer-x-budget-lifecycle-"))
    evidence.mkdir(parents=True, exist_ok=True, mode=0o700)
    os.chmod(evidence, 0o700)
    log_path = evidence / "budget-lifecycle.log"
    result = {"task": "3.1", "tests": 0, "skipped": 0, "cases": [], "command": COMMAND}
    code = 1
    try:
        revision, sources, dirty = candidate()
        result["revision"] = revision
        result["uncommitted_sources"] = dirty
        if not BINARY.is_file() or not MANIFEST.is_file():
            raise RuntimeError("missing prebuilt test_budget_close or its manifest; run --build first")
        manifest = json.loads(MANIFEST.read_text())
        if manifest.get("sources") != sources or manifest.get("binary_sha256") != digest(BINARY) or manifest.get("driver_sha256") != digest(TARGET / "budget-client"):
            raise RuntimeError("prebuilt binary does not match the current candidate sources")
        with log_path.open("w") as log:
            completed = subprocess.run([str(BINARY)], cwd=ROOT, stdout=log,
                                       stderr=subprocess.STDOUT, timeout=600, check=False)
        result["binary_exit_code"] = completed.returncode
        result["log"] = str(log_path)
        output = log_path.read_text()
        cases = {}
        for line in output.splitlines():
            if line.startswith("BUDGET_CASE "):
                _, name, status = line.split(" ")
                if name in cases:
                    raise RuntimeError(f"duplicate case {name}")
                cases[name] = status
        result["cases"] = [{"name": name, "status": status} for name, status in cases.items()]
        result["tests"] = len(cases)
        if completed.returncode != 0:
            raise RuntimeError(f"test_budget_close exited {completed.returncode}; see {log_path}")
        if any(status != "ok" for status in cases.values()):
            raise RuntimeError("a lifecycle case failed")
        missing = REQUIRED - set(cases)
        if missing:
            raise RuntimeError("missing required cases: " + ", ".join(sorted(missing)))
        if f"BUDGET_LIFECYCLE cases={len(cases)} skipped=0" not in output.splitlines():
            raise RuntimeError("missing or inconsistent case accounting")
        runtime_result = launch_runtime(evidence)
        result["runtime"] = runtime_result
        result["cases"].extend({"name": name, "status": "ok"} for name in runtime_result["cases"])
        result["tests"] = len(result["cases"])
        result["artifact_manifest"] = manifest
        code = 0
    except (OSError, ValueError, KeyError, RuntimeError, subprocess.SubprocessError) as error:
        result["failure"] = str(error)
        print(str(error), file=sys.stderr)
    result["exit_code"] = code
    destination = evidence / "budget-lifecycle.json"
    destination.write_text(json.dumps(result, indent=2) + "\n")
    os.chmod(destination, 0o600)
    print(f"revision={result.get('revision', 'unknown')} command={' '.join(COMMAND)} exit_code={code}")
    print(f"PAXEER_X_GATE tests={result['tests']} skipped=0")
    print(f"Evidence: {destination}")
    return code



def runtime_import():
    sys.path.insert(0, str(ROOT / 'tests/daemon'))
    import paxeer_x_runtime_fixture as fixture
    return fixture


def rows(raw, prefix):
    return [dict(x.split('=', 1) for x in line.split()[1:]) for line in raw.decode().splitlines() if line.startswith(prefix + ' ')]


def runtime_worker(directory):
    fixture = runtime_import()
    bundle = fixture.artifacts(os.environ['PAXEER_X_RUNTIME_ARTIFACTS'])
    client = fixture.client_artifact(os.environ['PAXEER_X_RUNTIME_CLIENT_MANIFEST'])
    for name in ('net', 'pid', 'mnt'):
        fixture.require(os.readlink('/proc/self/ns/' + name) != os.environ['PAXEER_X_PARENT_' + name.upper()], 'namespace isolation')
    fixture.run(['ip', 'link', 'set', 'lo', 'up'])
    fixture.run(['mount', '-t', 'tmpfs', '-o', 'mode=0755,nosuid,nodev', 'budget-fixture', '/tmp'])
    source = Path('/tmp/budget-source'); source.mkdir(mode=0o755)
    fixture.run(['mount', '--bind', ROOT, source])
    fixture.run(['mount', '-o', 'remount,bind,ro,nosuid,nodev', source])
    fixture.ROOT = source
    python_root = Path('/tmp/budget-python'); python_root.mkdir(mode=0o755)
    fixture.run(['mount', '--bind', sys.prefix, python_root])
    fixture.run(['mount', '-o', 'remount,bind,ro,nosuid,nodev', python_root])
    os.environ['PATH'] = str(python_root / 'bin') + ':/usr/local/bin:/usr/bin:/bin'
    runtime = fixture.RuntimeFixture(directory, bundle, client)
    retained, cases = [], []
    sequence, bob_sequence = 6, 1
    try:
        runtime.generate()
        for operation, seq in [('register', 0), ('open', 1), ('open-bob', 0), ('mint', 2), ('burn', 3), ('grant-issue', 4), ('grant-revoke', 5)]:
            got = rows(runtime.invoke(operation, seq, operation).stdout, 'receipt')
            fixture.require(len(got) == 1 and got[0]['result'] == '0', 'canonical asset preparation')
            retained.extend(got)
        activities = directory / 'activities'; activities.mkdir(mode=0o700)
        def invoke(operation, amount=0, expected=0, replay=None, source_sequence=1):
            nonlocal sequence, bob_sequence
            unauthorized = operation.startswith('unauthorized') or operation == 'credit-bob'
            seq = bob_sequence if unauthorized else sequence
            wire = replay or activities / (operation + '-' + str(seq))
            command = [str(TARGET / 'budget-client'), str(directory / 'run/layerxd.lni.sock'), str(directory / 'salt'),
                       'replay' if replay else operation, str(seq), str(amount), str(wire), str(expected)]
            env = runtime.env | {'PAXEER_X_FIXTURE_KEYS': str(directory / 'keys'), 'BUDGET_SOURCE_SEQUENCE': str(source_sequence), 'BUDGET_CREDIT_PROFILE': str(directory / 'custody.profile'), 'BUDGET_CREDIT_FILE': str(directory / (operation + '.credit'))}
            completed = subprocess.run(command, env=env, capture_output=True, timeout=45)
            label = ('replay-' if replay else '') + operation + '-' + str(seq)
            (directory / (label + '.log')).write_bytes(completed.stdout + completed.stderr)
            fixture.require(completed.returncode == 0, 'budget client failed: ' + label)
            got = rows(completed.stdout, 'receipt')
            fixture.require(len(got) == 1 and int(got[0]['result']) == expected, 'canonical budget receipt')
            if replay is None:
                retained.extend(got)
                if unauthorized: bob_sequence += 1
                else: sequence += 1
            return got[0], wire
        def state(operation='read'):
            command = [str(TARGET / 'budget-client'), str(directory / 'run/layerxd.lni.sock'), str(directory / 'salt'), operation, '0', '0', '/dev/null', '0']
            completed = subprocess.run(command, env=runtime.env | {'PAXEER_X_FIXTURE_KEYS': str(directory / 'keys')}, capture_output=True, timeout=20)
            (directory / ('state-' + str(time.time_ns()) + '.log')).write_bytes(completed.stdout + completed.stderr)
            fixture.require(completed.returncode == 0, 'authenticated budget/account state failed')
            got = rows(completed.stdout, 'state')
            fixture.require(len(got) == 4 and len({r['root'] for r in got}) == 1, 'state witnesses do not share signed head')
            return {r['label']: bytes.fromhex(r['raw']) for r in got}
        def balance(raw):
            n = int.from_bytes(raw[:2], 'big')
            return int.from_bytes(raw[3+n:19+n], 'big')
        def number(raw, at, width=16): return int.from_bytes(raw[at:at+width], 'big')
        def money(value): return tuple(balance(value[k]) for k in ('owner', 'recipient', 'custody'))
        def no_effect(operation, amount, expected):
            before = state()
            invoke(operation, amount, expected)
            after = state()
            fixture.require(before == after, operation + ' changed economic state')
            cases.append('runtime-' + operation + '-' + str(expected))
        from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey
        from cryptography.hazmat.primitives.serialization import Encoding, PublicFormat
        for actor, operation in [('treasury', 'credit-owner'), ('bob', 'credit-bob')]:
            public = Ed25519PrivateKey.from_private_bytes((directory / ('keys/' + actor + '.seed')).read_bytes()).public_key().public_bytes(Encoding.Raw, PublicFormat.Raw)
            did = ('did:layerx:' + public.hex()).encode()
            account_name = b'agent:' + did + b':main'
            beneficiary = hashlib.sha256(b'LX:ACCOUNT:v1' + len(account_name).to_bytes(4, 'big') + account_name).digest()
            amount = 1000000
            runtime.produce(operation + '-deposit', ['python3', fixture.ROOT / 'platform/hosted/paxeer/evm.py', 'send',
                '--rpc', runtime.rpc_url, '--chain', '125', '--key-file', directory / 'keys/deployer.key', '--value', str(amount * 10**12),
                '0x0000000000000000000000000000000000001013', 'deposit(bytes32)', '0x' + beneficiary.hex()])
            deposited = json.loads((directory / (operation + '-deposit.log')).read_text())
            fixture.require(int(deposited['status'], 16) == 1, 'real fee deposit reverted')
            logs = [entry for entry in deposited['logs'] if entry['address'].lower() == '0x0000000000000000000000000000000000001013' and len(entry['topics']) == 4]
            fixture.require(len(logs) == 1, 'exactly one native custody deposit')
            entry = logs[0]; data = bytes.fromhex(entry['data'].removeprefix('0x'))
            fixture.require(entry['topics'][2].removeprefix('0x').lower() == fixture.ASSET and data[:32] == beneficiary and int.from_bytes(data[32:64], 'big') == amount, 'custody deposit identity/amount')
            runtime.wait(lambda: int(runtime.rpc('eth_blockNumber'), 16) >= int(deposited['blockNumber'], 16) + 2)
            runtime.produce(operation + '-proof', [runtime.binary('layerx-custody-proof'), 'light-credit',
                '--rpc', 'http://127.0.0.1:' + str(runtime.ports[2]), '--profile', directory / 'custody.profile',
                '--deposit-id', entry['topics'][1], '--owner-key', '0x' + public.hex(), '--output', directory / (operation + '.credit')])
            credit = (directory / (operation + '.credit')).read_bytes()
            fixture.require(len(credit) > 363 and credit[:5] == b'LXDC3' and credit[43:75] == bytes.fromhex(entry['topics'][1][2:]) and credit[75:107].hex() == fixture.ASSET and credit[107:139] == beneficiary and credit[139:171] == public and int.from_bytes(credit[191:207], 'big') == amount, 'native credit proof differs from real deposit')
            invoke(operation)
        cases.append('runtime-real-native-fee-funding')
        initial_snapshots = set((directory / 'node/checkpoints').glob('*.lxs'))
        invoke('create', 300)
        invoke('spend', 120)
        before = state()
        fixture.require(money(before) == (8600, 120, 180), 'initial budget spending')
        no_effect('unauthorized-defund', 10, -207)
        no_effect('unauthorized-revoke', 2, -207)
        no_effect('defund', 181, -714)
        no_effect('overflow', 18446744073709551615, -714)
        no_effect('revoke', 1, -212)
        defund, defund_wire = invoke('defund', 150)
        partial = state(); record = partial['budget']
        fixture.require(money(partial) == (8750, 120, 30), 'exact partial refund')
        fixture.require(number(record,162) == 150 and number(record,210) == 120 and record[275:277] == b'\0\0', 'partial allowance or lifecycle state')
        repeated, _ = invoke('defund', replay=defund_wire)
        fixture.require(repeated['raw'] == defund['raw'] and state() == partial, 'original defund receipt or double refund')
        cases.append('runtime-defund-original-receipt')
        runtime.catch_up(retained)
        snapshots = runtime.wait(lambda: sorted(set((directory / 'node/checkpoints').glob('*.lxs')) - initial_snapshots))
        fixture.require(all(p.stat().st_size > 0 for p in snapshots), 'empty production snapshot')
        runtime.restart(kill=False)
        fixture.require(state() == partial, 'graceful snapshot recovery changed budget or balance')
        repeated, _ = invoke('defund', replay=defund_wire)
        fixture.require(repeated['raw'] == defund['raw'] and state() == partial, 'snapshot replay original receipt')
        cases.append('runtime-production-snapshot-graceful-restart')
        boundary = number(record,250,8) + number(record,242,8)
        delay = boundary / 1000 - time.time() + 1
        fixture.require(delay < 35, 'unbounded budget period')
        if delay > 0: time.sleep(delay)
        invoke('spend', 10)
        rolled = state()
        fixture.require(money(rolled) == (8750, 130, 20), 'rollover balances')
        fixture.require(number(rolled['budget'],250,8) >= boundary and number(rolled['budget'],210) == 10, 'actual period did not roll')
        cases.append('runtime-real-period-rollover')
        revoke, revoke_wire = invoke('revoke', 2)
        revoked = state(); record = revoked['budget']
        fixture.require(money(revoked) == (8770,130,0) and record[276] == 1 and number(record,266,8) == 2, 'atomic full refund/revocation')
        fixture.require(number(record,162) == number(record,210), 'revocation allowance')
        runtime.catch_up(retained)
        runtime.restart(kill=True)
        fixture.require(state() == revoked, 'forced same-disk restart changed state')
        repeated, _ = invoke('revoke', replay=revoke_wire)
        fixture.require(repeated['raw'] == revoke['raw'] and state() == revoked, 'forced restart original receipt/double refund')
        no_effect('spend', 1, -716)
        no_effect('revoke', 3, -716)
        cases.append('runtime-forced-restart-revocation-receipt')
        # A separate live budget proves CLOSE retains its independent operation.
        invoke('create-close', 20, source_sequence=2)
        invoke('close', 2)
        closed = state('read-close')['budget']
        fixture.require(closed[275] == 1 and closed[276] == 0, 'CLOSE collapsed into REVOKE')
        cases.append('runtime-compatible-close')
        reports = runtime.catch_up(retained)
        fixture.require(len(reports) == len(retained), 'replica missing canonical outcomes')
        for row in retained:
            fixture.require(runtime.invoke('receipt', row['id'], 'receipt-' + row['id']).stdout.decode().strip() == row['raw'], 'retained receipt bytes changed')
        cases.append('runtime-authenticated-independent-replica')
        fixture.write_json(directory / 'budget-runtime.json', {'cases': cases, 'receipts': len(retained),
            'snapshots': [str(p) for p in snapshots], 'graceful_restart': True, 'forced_restart': True,
            'same_disk': True, 'real_chain_id': 125, 'skipped': 0})
    finally:
        runtime.cleanup()


def launch_runtime(evidence):
    fixture = runtime_import()
    fixture.artifacts(os.environ['PAXEER_X_RUNTIME_ARTIFACTS'])
    fixture.client_artifact(os.environ['PAXEER_X_RUNTIME_CLIENT_MANIFEST'])
    directory = Path(tempfile.mkdtemp(prefix='px-budget-', dir='/var/tmp')); directory.rmdir()
    env = dict(os.environ)
    for name in ('net', 'pid', 'mnt'):
        env['PAXEER_X_PARENT_' + name.upper()] = os.readlink('/proc/self/ns/' + name)
    with (evidence / 'runtime.log').open('wb') as log:
        result = subprocess.run(['unshare', '--mount', '--net', '--pid', '--fork', '--kill-child=KILL', '--mount-proc',
            '--propagation', 'private', 'python3', str(Path(__file__).resolve()), '--worker', str(directory)],
            env=env, stdout=log, stderr=log, timeout=900)
    (evidence / 'runtime-directory').write_text(str(directory) + '\n')
    fixture.require(result.returncode == 0, 'real budget lifecycle failed: ' + str(directory))
    value = json.loads((directory / 'budget-runtime.json').read_text())
    fixture.require(value['skipped'] == 0 and value['graceful_restart'] and value['forced_restart'], 'incomplete durable lifecycle')
    return value


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--build", action="store_true",
                        help="build the declared test_budget_close target only")
    parser.add_argument("--worker")
    arguments = parser.parse_args()
    os.umask(0o077)
    if arguments.worker:
        runtime_worker(Path(arguments.worker))
        return 0
    if arguments.build:
        build()
        return 0
    return qualify()


if __name__ == "__main__":
    sys.exit(main())
