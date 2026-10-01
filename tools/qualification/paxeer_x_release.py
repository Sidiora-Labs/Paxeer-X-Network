#!/usr/bin/env python3
import argparse
import base64
import datetime
import hashlib
import json
import os
from pathlib import Path
import re
import signal
import socket
import stat
import subprocess
import sys
import tempfile
import time

sys.dont_write_bytecode = True
ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / 'tools/paxeer-x'))
from candidate import (SCHEMA, UNKNOWN, MAX_BYTES, DIGEST, BINDING_FIELDS, Invalid,
                       load_private, write_private, validate, catalogue, protected_path)

MANIFEST = 'paxeer-x.qualification-manifest.v1'
DEADLINE_SECONDS = 1500


def require(ok, message):
    if not ok:
        raise Invalid(message)


def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(',', ':'), ensure_ascii=True).encode()


def digest(value):
    return hashlib.sha256(value).hexdigest()


def remaining(deadline):
    seconds = deadline - time.monotonic()
    require(seconds > 0, 'absolute evidence deadline exceeded')
    return seconds


def execute(argv, deadline, **kwargs):
    return subprocess.run(argv, stdin=subprocess.DEVNULL, timeout=remaining(deadline),
                          check=False, **kwargs)


def private_dir(path):
    path = protected_path(path).resolve()
    require(path != ROOT and ROOT not in path.parents, 'evidence must be outside source')
    info = path.stat()
    require(stat.S_ISDIR(info.st_mode) and info.st_uid == os.geteuid() and not info.st_mode & 0o077,
            'evidence directory must be private and owned')
    return path


def candidate(path):
    value = load_private(path)
    validate(value, catalogue(ROOT / 'spec/paxeer-x/spec.kvx'), ROOT,
             mainline=os.environ.get('PAXEER_X_MAINLINE', 'refs/heads/main'))
    require(value['schema'] == SCHEMA and not value['source']['dirty'] and value['source']['integrated'],
            'clean integrated candidate required')
    return value


def prepare(candidate_path, contract_path, evidence):
    cand = candidate(candidate_path)
    contract = load_private(contract_path)
    require(set(contract) == {'schema', 'cells', 'required_acceptance', 'environment_sha256',
                             'inputs_sha256', 'images_sha256'}, 'contract fields invalid')
    require(contract['schema'] == 'paxeer-x.qualification-contract.v1', 'contract schema invalid')
    require(contract['cells'] and contract['required_acceptance'], 'complete contract required')
    for key in ('environment_sha256', 'inputs_sha256', 'images_sha256'):
        require(re.fullmatch('[0-9a-f]{64}', contract[key]), 'immutable contract binding missing')
    env = dict(os.environ, PAXEER_X_EVIDENCE_DIR=str(private_dir(evidence)))
    spec_text = (ROOT / 'spec/paxeer-x/spec.kvx').read_text()
    sections = dict(re.findall(r'^\[([^\]]+)\]\n(.*?)(?=^\[|\Z)', spec_text, re.M | re.S))
    required = contract['required_acceptance']
    require(isinstance(required, list) and len(set(required)) == len(required), 'acceptance inventory repeats')
    for criterion in required:
        match = re.fullmatch(r'([0-9]+)\.([0-9]+)', criterion)
        require(match and re.search(r'^ac_' + match[2] + r' = ',
                sections.get('req.' + match[1], ''), re.M), 'unknown acceptance criterion')
    cells = []
    for declared in contract['cells']:
        require(isinstance(declared, dict) and 'selector' in declared, 'invalid cell')
        selector = declared['selector']
        require(re.fullmatch(r'[0-9]+(?:\.[0-9]+)+', selector), 'invalid selector')
        result = subprocess.run([str(ROOT / 'tools/paxeer-x/verify-task.sh'), '--identity', selector,
                                 '--manifest', str(candidate_path)], cwd=ROOT, env=env,
                                capture_output=True, timeout=60, check=False)
        require(result.returncode == 0, 'registered gate identity refused')
        identity = json.loads(result.stdout)
        cell = dict(declared)
        require(cell.get('gate_identity') == identity['identity'], 'registered gate identity mismatch')
        require(cell.get('command') == ['tools/paxeer-x/gates/' + selector + '.sh'],
                'only exact production registered gate commands are accepted')
        cells.append(cell)
    manifest = {'schema': MANIFEST, 'candidate_sha256': digest(canonical(cand)),
                'revision': cand['source']['revision'], 'tree': cand['source']['tree'],
                'workflow_sha256': digest((ROOT / '.github/workflows/paxeer-x-qualification.yml').read_bytes()),
                'contract_sha256': digest(canonical(contract)), 'cells': cells,
                'required_acceptance': contract['required_acceptance']}
    manifest.update({key: contract[key] for key in ('environment_sha256', 'inputs_sha256', 'images_sha256')})
    return manifest


def request_id(manifest):
    return digest(b'paxeer-x.logical.v1\0' + canonical(manifest))


def control(path, message):
    encoded = canonical(message)
    require(len(encoded) <= MAX_BYTES, 'control message oversized')
    sock = socket.socket(socket.AF_UNIX)
    sock.settimeout(30)
    try:
        info = os.stat(path, follow_symlinks=False)
        require(stat.S_ISSOCK(info.st_mode) and info.st_uid == os.geteuid() and not info.st_mode & 0o077,
                'unsafe controller socket')
        sock.connect(str(path))
        sock.sendall(encoded)
        sock.shutdown(socket.SHUT_WR)
        data = bytearray()
        while True:
            chunk = sock.recv(min(65536, MAX_BYTES + 1 - len(data)))
            if not chunk:
                break
            data.extend(chunk)
            require(len(data) <= MAX_BYTES, 'response oversized')
        result = json.loads(data)
        require(not result.get('error'), 'controller refused request')
        return result
    finally:
        sock.close()


def evidence(args):
    deadline = time.monotonic() + DEADLINE_SECONDS
    cand = candidate(args.candidate)
    req = load_private(args.request)
    completed = load_private(args.completed)
    require(completed['request'] == req and req['manifest']['candidate_sha256'] == digest(canonical(cand)),
            'completed evidence candidate/request mismatch')
    require(req['logical_id'] == request_id(req['manifest']), 'logical identity mismatch')
    remaining(deadline)
    result = execute([str(args.controller_binary), 'qualification-verify', str(args.completed), str(args.trust)],
                     deadline, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    require(result.returncode == 0, 'completed trusted evidence refused')
    remaining(deadline)
    write_private(args.output, {'schema': 'paxeer-x.completed-evidence-validation.v1',
                  'logical_id': req['logical_id'], 'candidate_sha256': req['manifest']['candidate_sha256'],
                  'completed_sha256': digest(canonical(completed)), 'validated': True,
                  'release_certification': False})


def workflow_request():
    encoded = os.environ.get('PAXEER_X_REQUEST', '')
    require(len(encoded) < 60000, 'workflow request oversized')
    req = json.loads(base64.b64decode(encoded, validate=True))
    require(req['logical_id'] == request_id(req['manifest']), 'workflow request identity mismatch')
    require(req['logical_id'] == os.environ.get('PAXEER_X_REQUEST_ID'), 'dispatch identity mismatch')
    require(req['manifest']['revision'] == os.environ.get('GITHUB_SHA'), 'dispatch source differs from candidate')
    require(os.environ.get('GITHUB_RUN_ATTEMPT') == '1', 'reruns require separate explicit authorization')
    return req


def workflow_material():
    directory = Path(tempfile.mkdtemp(prefix='paxeer-qualification-', dir=os.environ['RUNNER_TEMP']))
    directory.chmod(0o700)
    for name, key in [('candidate', 'PAXEER_X_CANDIDATE_JSON'), ('contract', 'PAXEER_X_CONTRACT_JSON')]:
        value = os.environ.get(key)
        require(value and len(value) <= MAX_BYTES, 'protected candidate/contract authority unavailable')
        write_private(directory / (name + '.json'), json.loads(value))
    return directory


def workflow_plan():
    req = workflow_request()
    directory = workflow_material()
    actual = prepare(directory / 'candidate.json', directory / 'contract.json', directory)
    require(actual == req['manifest'], 'protected contract differs from requested full matrix')
    cells = []
    for cell in actual['cells']:
        require(cell['timeout_seconds'] > cell['minimum_seconds'], 'invalid timeout')
        require(cell['timeout_seconds'] <= 7 * 24 * 3600 - 1800,
                'job exceeds supported runner lifetime; duration is not shortened')
        cells.append({'id': cell['id'], 'runner': cell['runner'],
                      'timeout_minutes': (cell['timeout_seconds'] + 1799) // 60})
    print(json.dumps({'include': cells}, separators=(',', ':')))


def workflow_run(args):
    req = workflow_request()
    directory = workflow_material()
    actual = prepare(directory / 'candidate.json', directory / 'contract.json', directory)
    require(actual == req['manifest'], 'protected acceptance contract mismatch')
    cells = [c for c in actual['cells'] if c['id'] == args.cell]
    require(len(cells) == 1, 'unknown matrix cell')
    cell = cells[0]
    artifacts_dir = directory / 'case-artifacts'
    artifacts_dir.mkdir(mode=0o700)
    metrics = directory / 'case-metrics.json'
    env = dict(os.environ, PAXEER_X_EVIDENCE_DIR=str(directory),
               PAXEER_X_CASE_ARTIFACT_DIR=str(artifacts_dir), PAXEER_X_CASE_METRICS=str(metrics),
               PYTHONDONTWRITEBYTECODE='1')
    for key in ('PAXEER_X_CANDIDATE_JSON', 'PAXEER_X_CONTRACT_JSON', 'GITHUB_TOKEN', 'GH_TOKEN', 'FLY_API_TOKEN'):
        env.pop(key, None)
    started = datetime.datetime.now(datetime.timezone.utc)
    monotonic = time.monotonic_ns()
    completed = subprocess.run([str(ROOT / 'tools/paxeer-x/verify-task.sh'), cell['selector'],
                                '--manifest', str(directory / 'candidate.json')], cwd=ROOT, env=env,
                               capture_output=True, timeout=cell['timeout_seconds'], check=False)
    elapsed = time.monotonic_ns() - monotonic
    ended = datetime.datetime.now(datetime.timezone.utc)
    lines = re.findall(rb'^evidence: (.+)$', completed.stdout, re.M)
    require(completed.returncode == 0 and len(lines) == 1, 'production gate did not complete successfully')
    gate_path = Path(os.fsdecode(lines[0]))
    require(gate_path.parent == directory, 'gate evidence escaped private root')
    checked = subprocess.run([str(ROOT / 'tools/paxeer-x/verify-task.sh'), '--check', str(gate_path),
                              '--manifest', str(directory / 'candidate.json')], cwd=ROOT, env=env,
                             capture_output=True, timeout=60, check=False)
    require(checked.returncode == 0, 'production gate evidence authentication failed')
    gate = load_private(gate_path)
    measurements = {}
    if cell['minimum_seconds'] or cell['thresholds']:
        measured = load_private(metrics)
        require(set(measured) == {'uninterrupted_nanoseconds', 'measurements'}, 'gate metrics incomplete')
        elapsed = measured['uninterrupted_nanoseconds']
        measurements = measured['measurements']
    artifacts = []
    for kind in cell['artifact_kinds']:
        if kind == 'gate-evidence':
            content = canonical(gate)
        elif kind == 'log':
            path = protected_path(gate['log']['path'])
            require(path.parent == directory, 'log escapes private root')
            content = path.read_bytes()
        else:
            path = protected_path(artifacts_dir / kind)
            require(path.is_file() and not path.is_symlink(), 'required case artifact missing')
            content = path.read_bytes()
        require(content and len(content) <= MAX_BYTES, 'artifact absent or oversized')
        artifacts.append({'kind': kind, 'sha256': digest(content),
                          'content': base64.b64encode(content).decode()})
    result = {'cell_id': cell['id'], 'logical_id': req['logical_id'],
              'candidate_sha256': actual['candidate_sha256'], 'manifest_sha256': digest(canonical(actual)),
              'revision': actual['revision'], 'tree': actual['tree'],
              'end_revision': gate['source']['revision'], 'end_tree': gate['source']['tree'],
              'dirty': gate['source']['dirty'], 'run_id': int(os.environ['GITHUB_RUN_ID']),
              'attempt': int(os.environ['GITHUB_RUN_ATTEMPT']), 'exit_code': gate['exit_code'],
              'tests': gate['tests'], 'skipped': gate['skipped'], 'started_at': started.isoformat(),
              'completed_at': ended.isoformat(), 'uninterrupted_nanoseconds': elapsed,
              'measurements': measurements, 'artifacts': artifacts}
    binding = req['logical_id'] + '-' + cell['id'] + '-' + os.environ['GITHUB_RUN_ATTEMPT']
    sealed = subprocess.run([str(args.controller_binary), 'qualification-seal', binding, req['recipient']],
                            input=canonical(result), capture_output=True, timeout=60, check=False)
    require(sealed.returncode == 0, 'artifact encryption failed')
    write_private(args.output, json.loads(sealed.stdout))
    payload = canonical(json.loads(sealed.stdout))
    require(len(payload) <= MAX_BYTES, 'encrypted result too large')
    encoded = base64.b64encode(payload).decode()
    chunks = [encoded[i:i + 16000] for i in range(0, len(encoded), 16000)]
    for index, chunk in enumerate(chunks):
        print('PAXEER_X_ENCRYPTED_V1 %s %d %d %s %s' %
              (binding, index, len(chunks), digest(payload), chunk))


def main():
    parser = argparse.ArgumentParser()
    commands = parser.add_subparsers(dest='command', required=True)
    plan = commands.add_parser('prepare')
    for key in ('candidate', 'contract', 'evidence', 'recipient', 'ref', 'output'):
        plan.add_argument('--' + key, required=True)
    submit = commands.add_parser('submit')
    submit.add_argument('--request', required=True)
    submit.add_argument('--controller-socket', required=True)
    status = commands.add_parser('status')
    status.add_argument('--logical-id', required=True)
    status.add_argument('--controller-socket', required=True)
    collect = commands.add_parser('evidence')
    for key in ('candidate', 'request', 'completed', 'trust', 'controller-binary', 'output'):
        collect.add_argument('--' + key, required=True)
    commands.add_parser('workflow-plan')
    worker = commands.add_parser('workflow-run')
    worker.add_argument('--cell', required=True)
    worker.add_argument('--controller-binary', required=True)
    worker.add_argument('--output', required=True)
    args = parser.parse_args()
    try:
        if args.command == 'prepare':
            manifest = prepare(args.candidate, args.contract, args.evidence)
            write_private(args.output, {'manifest': manifest, 'logical_id': request_id(manifest),
                          'ref': args.ref, 'recipient': args.recipient})
        elif args.command == 'submit':
            req = load_private(args.request)
            print(json.dumps(control(args.controller_socket, {'operation': 'submit', 'request': req})))
        elif args.command == 'status':
            print(json.dumps(control(args.controller_socket, {'operation': 'status', 'logical_id': args.logical_id})))
        elif args.command == 'evidence':
            def expired(signum, frame):
                raise Invalid('absolute evidence deadline exceeded')
            previous = signal.signal(signal.SIGALRM, expired)
            signal.setitimer(signal.ITIMER_REAL, DEADLINE_SECONDS)
            try:
                evidence(args)
            finally:
                signal.setitimer(signal.ITIMER_REAL, 0)
                signal.signal(signal.SIGALRM, previous)
        elif args.command == 'workflow-plan':
            workflow_plan()
        else:
            workflow_run(args)
        return 0
    except (Invalid, OSError, ValueError, KeyError, TypeError, subprocess.SubprocessError):
        print('qualification: refused missing, unsafe, or mismatched completed evidence or authority', file=sys.stderr)
        return 2


if __name__ == '__main__':
    sys.exit(main())
