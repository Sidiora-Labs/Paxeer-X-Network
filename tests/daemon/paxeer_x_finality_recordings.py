#!/usr/bin/env python3
import argparse
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import signal
import subprocess
import sys
import tempfile
import time

sys.path.insert(0, str(Path(__file__).resolve().parent))
import paxeer_x_runtime_fixture as fixture
import paxeer_x_finality_fixture as finality

ROOT = Path(__file__).resolve().parents[2]
MODULE = importlib.util.spec_from_file_location('finality_authority_chain', ROOT / 'tests/daemon/finality-authority-chain.py')
chain = importlib.util.module_from_spec(MODULE)
MODULE.loader.exec_module(chain)


def save(path, value):
    path = Path(path)
    temporary = path.with_suffix('.tmp')
    temporary.write_text(json.dumps(value, sort_keys=True) + '\n')
    temporary.chmod(0o600)
    os.replace(temporary, path)


def require(condition, message):
    fixture.require(condition, message)


def capture(producer, guarantor, batch, ident, admit, label):
    recorder = chain.Recorder(producer.runtime.rpc_url)
    try:
        environment = producer.runtime.env | producer.settlement
        environment['LAYERX_NODE_PAXEER_RPC_URL'] = recorder.url
        command = [str(producer.binaries['lxp_test_daemon_finality_authority']), 'recorded',
                   str(guarantor['state']), str(batch), ident, 'admit' if admit else 'refuse']
        completed = subprocess.run(command, cwd=fixture.ROOT, env=environment,
                                   capture_output=True, text=True, timeout=300)
        (producer.work / (label + '.log')).write_text(completed.stdout + completed.stderr)
        require(completed.returncode == 0, 'actual finalized verifier ' + label + ' exit=' + str(completed.returncode))
        markers = [line.removeprefix('FINALITY_RECORDINGS_CASES ') for line in completed.stdout.splitlines()
                   if line.startswith('FINALITY_RECORDINGS_CASES ')]
        require(len(markers) == 1, 'actual finality case ledger missing')
        observed = json.loads(markers[0])
        require(set(observed.get('cases', {})) == set(chain.RECORDING_CASES)
                and all(observed['cases'].values()), 'mandatory production verifier case missing')
        number = recorder.rpc('eth_blockNumber', [])
        block = recorder.rpc('eth_getBlockByNumber', [number, False])
        require(int(block['number'], 16) == int(number, 16), 'captured block number mismatch')
        exchanges = recorder.snapshot()
        return dict(state_dir=str(guarantor['state']), batch=batch, checkpoint_id=ident,
                    exchanges=exchanges, exchange_sha256=chain.exchange_digest(exchanges)), \
            dict(number=int(number, 16), hash=block['hash']), observed
    finally:
        recorder.close()


def refuses_to_publish(runtime):
    runtime.start_role('sequencer')
    process = runtime.processes['sequencer']
    end = time.monotonic() + 60
    while time.monotonic() < end:
        if process.poll() is not None:
            del runtime.processes['sequencer']
            return True, process.returncode
        if (runtime.directory / 'run/layerxd.lni.sock').is_socket() \
                and runtime.invoke('ready', 0, 'damaged-ready', check=False).returncode == 0:
            return False, None
        time.sleep(.2)
    return False, None


def damaged_logs(runtime, producer, recorded):
    log = runtime.directory / 'node/logs/evidence.log'
    original = log.read_bytes()
    require(bool(recorded['records']), 'real durable finality records missing')
    first, last = recorded['records'][0], recorded['records'][-1]
    runtime.stop_role('sequencer')
    outcomes = {}
    try:
        corrupt = bytearray(original)
        corrupt[first['offset'] + 40] ^= 1
        cut = last['offset'] + 32 + last['length'] // 2
        for name, data in (('corrupted', bytes(corrupt)), ('truncated', original[:cut])):
            log.write_bytes(data)
            refused, exit_code = refuses_to_publish(runtime)
            outcomes[name] = dict(refused_before_publication=refused, exit=exit_code)
            if 'sequencer' in runtime.processes:
                runtime.stop_role('sequencer')
            require(refused, name + ' durable evidence was published instead of refused')
    finally:
        if 'sequencer' in runtime.processes:
            runtime.stop_role('sequencer')
        log.write_bytes(original)
        runtime.start_role('sequencer')
        runtime.readiness()
    require(producer.records() == recorded, 'restored finality frontier changed')
    outcomes['restored_frontier_identical'] = True
    return outcomes


def isolate():
    require(os.geteuid() == 0, 'namespace worker requires root')
    for name in ('net', 'pid', 'mnt'):
        require(os.readlink('/proc/self/ns/' + name) != os.environ['PAXEER_X_PARENT_' + name.upper()],
                'disposable worker namespace not isolated')
    fixture.run(['ip', 'link', 'set', 'lo', 'up'])
    fixture.run(['mount', '-t', 'tmpfs', '-o', 'mode=0755,nosuid,nodev', 'finality-recordings', '/tmp'])
    source = Path('/tmp/runtime-source')
    source.mkdir(mode=0o755)
    fixture.run(['mount', '--bind', ROOT, source])
    fixture.run(['mount', '-o', 'remount,bind,ro,nosuid,nodev', source])
    fixture.ROOT = source
    python_root = Path('/tmp/runtime-python')
    python_root.mkdir(mode=0o755)
    fixture.run(['mount', '--bind', sys.prefix, python_root])
    fixture.run(['mount', '-o', 'remount,bind,ro,nosuid,nodev', python_root])
    os.environ['PATH'] = str(python_root / 'bin') + ':/usr/local/bin:/usr/bin:/bin'


def worker(directory, bundle, client, supplemental):
    isolate()
    runtime = fixture.RuntimeFixture(directory, bundle, client)
    producer = None
    result = {'version': 1, 'purpose': 'finality-authority-recordings-result'}
    try:
        runtime.generate()
        producer = finality.FinalityProducer(runtime, supplemental)
        producer.bond()
        producer.tls()
        producer.start()
        for batch, (operation, sequence) in enumerate((('register', 0), ('open', 1), ('open-bob', 0)), 1):
            runtime.invoke(operation, sequence, operation)
            producer.registered(batch)
        batch = 3
        provenance = producer.provenance(batch)
        ident = provenance['checkpoint_id']
        first = producer.guarantors[0]
        before, block_before, observation_before = capture(producer, first, batch, ident, True, 'recorded-before')
        result['before'] = observation_before
        members = producer.call('checkpointGuarantors(uint64)(bytes32[])', batch)
        producer.stop()
        departed = producer.guarantors[1]
        status = producer.unbond(departed)
        transition = dict(guarantor_id=departed['id'], eligible_after=status['eligible'],
                          still_final=int(producer.call('statusOf(uint64)(uint8)', batch)[0]) == finality.STATUS_FINAL,
                          guarantors_unchanged=producer.call('checkpointGuarantors(uint64)(bytes32[])', batch) == members)
        require(not transition['eligible_after'] and transition['still_final'] and transition['guarantors_unchanged'],
                'real guarantor membership transition failed')
        records = producer.records()
        state = runtime.invoke('read', 0, 'before-restart').stdout
        prefix = producer.record_bytes(records)
        runtime.restart()
        restarted = producer.records()
        restart = dict(records_identical=restarted == records,
                       bytes_identical=producer.record_bytes(restarted) == prefix,
                       state_identical=runtime.invoke('read', 0, 'after-restart').stdout == state)
        require(all(restart.values()), 'real restarted daemon frontier changed')
        after, block_after, observation_after = capture(producer, first, batch, ident, False, 'recorded-after')
        result['after'] = observation_after
        manifest = dict(version=1, purpose='finality-authority-recordings', chain_id=125, anchor=chain.ANCHOR,
                        source_revision=bundle['source_revision'], capture_block=block_after,
                        phases=dict(before=before, after=after), producer=provenance,
                        required_cases=list(chain.REQUIRED_RECORDING_CASES),
                        membership_transition=transition, restart=restart)
        manifest_path = Path(directory) / 'recordings.json'
        save(manifest_path, manifest)
        result['recordings'] = chain.verify_recordings(producer.binaries['lxp_test_daemon_finality_authority'], manifest_path)
        result['damaged_log'] = damaged_logs(runtime, producer, restarted)
        result['all_required_cases_executed'] = True
        result['manifest'] = str(manifest_path)
        save(Path(directory) / 'result.json', result)
    finally:
        if producer is not None:
            producer.cleanup()
        runtime.cleanup()


def main():
    os.umask(0o077)
    parser = argparse.ArgumentParser()
    parser.add_argument('--worker')
    arguments = parser.parse_args()
    bundle = fixture.artifacts(os.environ.get('PAXEER_X_RUNTIME_ARTIFACTS', ''))
    client = fixture.client_artifact(os.environ.get('PAXEER_X_RUNTIME_CLIENT_MANIFEST', ''))
    supplemental = finality.supplemental(os.environ.get('PAXEER_X_FINALITY_SUPPLEMENTAL_MANIFEST', ''))
    if arguments.worker:
        worker(arguments.worker, bundle, client, supplemental)
        return 0
    evidence = Path(os.environ.get('PAXEER_X_EVIDENCE_DIR', ''))
    require(evidence.is_absolute() and evidence.is_dir(), 'private evidence directory required')
    directory = Path(tempfile.mkdtemp(prefix='px-finality-recordings-', dir='/var/tmp'))
    directory.rmdir()
    environment = dict(os.environ)
    environment['PYTHONDONTWRITEBYTECODE'] = '1'
    for name in ('net', 'pid', 'mnt'):
        environment['PAXEER_X_PARENT_' + name.upper()] = os.readlink('/proc/self/ns/' + name)
    command = ['unshare', '--mount', '--net', '--pid', '--fork', '--kill-child=KILL', '--mount-proc',
               '--propagation', 'private', sys.executable, str(Path(__file__).resolve()), '--worker', str(directory)]
    with (evidence / 'recordings-worker.log').open('wb') as log:
        completed = subprocess.run(command, env=environment, stdout=log, stderr=log, timeout=1500)
    (evidence / 'recordings-directory').write_text(str(directory) + '\n')
    result_path = directory / 'result.json'
    result = json.loads(result_path.read_text()) if result_path.is_file() else {}
    save(evidence / 'recordings-result.json', result)
    require(completed.returncode == 0, 'real disposable finality worker exit=' + str(completed.returncode))
    require(result.get('all_required_cases_executed') is True, 'finalized-history cases did not all execute')
    print('PAXEER_X_GATE finalized-history=1 membership-transition=1 restart=1 damaged-refusals=2 skipped=0')
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
