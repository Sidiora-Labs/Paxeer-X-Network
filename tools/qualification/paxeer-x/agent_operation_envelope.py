#!/usr/bin/env python3
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import socket
import ssl
import stat
import subprocess
import sys
import tempfile
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

ROOT = Path(__file__).resolve().parents[3]
sys.path.insert(0, str(ROOT / 'tests/daemon'))
import paxeer_x_runtime_fixture as fixture
from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey
from cryptography.hazmat.primitives.serialization import Encoding, PublicFormat

SCHEMA = 'paxeer-x.agent-envelope-artifacts.v1'
CONFIG_SCHEMA = 'paxeer-x.agent-envelope-qualification.v1'
ROUTE = '/v1/agent/rpc'
MAX_BODY = 1_048_576
LANGUAGES = ('rust', 'typescript', 'python', 'go', 'java', 'kotlin', 'swift', 'csharp')
LANGUAGE_CASES = ('read', 'program_read', 'approval_list')
RUST_PRE_RESTART = ('allowed_mutation', 'mutation_duplicate_same_result', 'wrong_scope', 'wrong_tenant',
                    'wrong_generation', 'wrong_session', 'wrong_token', 'revoked_session',
                    'missing_idempotency_key', 'changed_body_same_key', 'restart_unknown_pending')
RUST_POST_RESTART = ('restart_unknown_reconcile',)

W20_WAIT_PRE = ('wait_bounded',)
W20_WAIT_SECONDS = 30

W20_EXPIRY_POST = ('credential_expiry',)
W20_EXPIRY_POLL_SECONDS = 240

W20_REVOCATION_PRE = ('session_close_revocation',)
W20_INVALIDATED_STATES = ('Failed', 'Expired')

W20_IDEMPOTENCY_PRE = ('restart_replay_idempotent_record',)
W20_IDEMPOTENCY_POST = ('restart_replay_idempotent',)

W20_ROTATION_PRE = ('session_refresh_credential',)
W20_SESSION_FIELDS = ('tenant', 'session_id', 'token_id', 'generation')

W7_OWNER_DIRECT = ('session_list_provisioned', 'availability_fetch_provisioned')
W7_AVAILABILITY_SECONDS = 60
W7_REQUEST_LEVELS = ('unverified', 'sequencer-signed', 'batch-included', 'state-proven', 'checkpoint-finalised', 'settlement-anchored')
W7_AVAILABILITY_CLASSES = ('activities', 'receipts', 'oracle', 'state_diff', 'recovery')

W20_COMPLETED_DIRECT = ('history_cursor_round_trip', 'subscription_lifecycle')

W7_SIGN_DIRECT = ('sign_signed_unverified', 'sign_submit_composite')
W7_SIGN_NEGATIVE = ('submit_foreign_key_refused',)
W8_CAPABILITY_DIRECT = ('capability_create_provisioned', 'capability_attenuate_narrows', 'capability_list_timed',
                        'capability_revoke_subtree')
W8_CAPABILITY_REFUSALS = ('capability_refuse_duplicate', 'capability_refuse_zero_window',
                          'capability_refuse_ceiling_outside_assets', 'capability_refuse_expired',
                          'capability_refuse_uppercase_reference')
W8_CAPABILITY_RECORD = ('capability_id', 'parent_id', 'tenant', 'agent_did', 'dimensions', 'state', 'created_at_ms',
                        'created_at_sequence', 'revoked_at_ms', 'revoked_at_sequence')
W8_DIMENSIONS = ('activity_types', 'counterparties', 'assets', 'amount_ceilings', 'rate_ceilings', 'purpose_constraints', 'expiry')
W8_DECIMAL = '(0|[1-9][0-9]{0,38})'
SERVED_READ_DIRECT = ('project_fee_projection', 'policy_dry_run_explained', 'export_offline_bounded', 'program_discover_registry')
SERVED_READ_REFUSALS = ('project_refuse_over_bound', 'project_refuse_legacy_payload', 'policy_dry_run_refuse_unknown_capability',
                        'export_refuse_seventeen_facts', 'export_refuse_duplicate_fact', 'export_refuse_uppercase_fact',
                        'export_refuse_settlement_anchored', 'prepare_refuse_uppercase_capability_id')
PROJECT_REQUEST = ('protocol_activity_type', 'canonical_bytes', 'execution_units', 'storage_units')
PROJECTED = ('request', 'parameter_version', 'fee', 'canonical_schedule', 'snapshot_sequence', 'snapshot_state_root')
POLICY_DRY_RUN_REQUEST = ('tenant', 'agent_did', 'session_id', 'capability_id', 'activity_type', 'counterparty', 'asset',
                          'amount', 'purpose', 'core_sequence')
POLICY_DRY_RUN_RESULT = ('outcome', 'policy_version', 'matched_rules', 'deciding_rule', 'reason', 'mode', 'authority_statement')
POLICY_REASONS = ('permitted_by_rule', 'explicit_deny', 'approval_required', 'no_permitting_rule', 'invalid_context',
                  'evaluation_failure')
FRESHNESS = ('chain_head', 'latest_sealed_batch', 'latest_finalised_checkpoint', 'value_sequence', 'relative_to')
EXPORT_BUCKETS = ('receipts', 'proofs', 'certificates', 'headers')
NONZERO_HEX32 = '(?!0{64})[0-9a-f]{64}'
FACT_REF = '(receipt|activity):' + NONZERO_HEX32 + '|state:' + NONZERO_HEX32 + ':' + NONZERO_HEX32 + '|checkpoint:' + NONZERO_HEX32 + ':[1-9][0-9]{0,19}'
MAX_FACTS = 16
DISCOVERY = ('program_id', 'lifecycle', 'version', 'code_hash', 'abi_version', 'receipt_digest', 'state_root',
             'observed_sequence', 'observed_at', 'valid_through', 'verification')
DECIMAL_U64 = '(0|[1-9][0-9]{0,19})'
W7_SUBMITTED_STATES = ('Queued', 'Submitted', 'Acknowledged', 'Executed')
W20_SUBSCRIPTION_RECORD = ('subscription_id', 'scope', 'filter', 'start', 'last_acknowledged', 'delivery_target', 'paused')
PYTHON_PRE_RESTART = ('allowed_mutation', 'mutation_duplicate_same_result', 'restart_unknown_pending', 'restart_retry_same_result')
PYTHON_POST_RESTART = ('restart_unknown_reconcile', 'restart_retry_same_result')
DECODE_CASES = ('read_decode_failure', 'mutation_decode_unknown')
DIRECT_CASES = ('read_account', 'program_read', 'approval_list', 'program_bearer_alone', 'gateway_key_alone_write',
                'forged_principal_header', 'malformed_body', 'truncated_body', 'oversized_body', 'unknown_operation',
                'unknown_field', 'bad_version', 'noncanonical_integer', 'faucet_retired', 'mtls_no_client_cert',
                'mtls_wrong_peer', 'mtls_wrong_ca', 'no_plaintext_fallback', 'native_rpc_unchanged',
                'programs_route_unchanged', 'idempotent_replay_same_body', 'idempotency_changed_body_conflict',
                'health_ready', 'health_binding_wrong_network', 'health_binding_ready_false', 'coordinate_mismatch')
HEALTH_PATH = '/healthz'

RULED_REFUSALS = {'faucet.claim': (503, 'UnavailableCapability', 'unavailable_capability.faucet.claim'),
                  'agent.register': (403, 'PolicyRefusal', 'refused_pending_bootstrap_artifact'),
                  'session.open': (403, 'PolicyRefusal', 'refused_pending_bootstrap_artifact'),
                  'program.interface': (503, 'UnavailableCapability', 'unmatched_by_ruling'),
                  'read.proof_bundle': (503, 'UnavailableCapability', 'unmatched_by_ruling')}
CLASSES = {'TransportFailure', 'Deadline', 'ProtocolIncompatibility', 'UnavailableCapability', 'CoreRejection',
           'VerificationFailure', 'PolicyRefusal', 'CapabilityRefusal', 'BudgetRefusal', 'RateLimit',
           'IdempotencyConflict', 'InternalFault'}
LEVELS = ('Unverified', 'SequencerSigned', 'BatchIncluded', 'StateProven', 'CheckpointFinalised', 'SettlementAnchored')
SOURCES = ('agent/crates', 'agent/Cargo.toml', 'agent/Cargo.lock', 'agent/schema/agent-api', 'agent/sdk/typescript/src',
           'agent/sdk/typescript/test', 'agent/sdk/typescript/package.json', 'agent/sdk/typescript/package-lock.json',
           'agent/sdk/typescript/tsconfig.json', 'agent/sdk/python', 'platform/hosted/gateway', 'platform/Cargo.toml',
           'platform/Cargo.lock', 'platform/sdk/go', 'platform/sdk/jvm', 'platform/sdk/swift', 'platform/sdk/dotnet',
           'tools/paxeer-x/route-catalogue.json', 'tools/qualification/paxeer-x/agent_operation_envelope.py')
CASE_LINE = re.compile(r'^PAXEER_X_AGENT_ENVELOPE_CASE ([a-z0-9_.\-]+) passed$', re.M)
COUNT_LINE = re.compile(r'^PAXEER_X_AGENT_ENVELOPE_CASES=(\d+)$', re.M)


def require(condition, reason):
    if not condition:
        raise RuntimeError('agent operation envelope refused: ' + reason)


def digest(path):
    with Path(path).open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def capture(argv, cwd=ROOT):
    return subprocess.run([str(a) for a in argv], cwd=cwd, check=True, capture_output=True, text=True).stdout.strip()


def private_dir(raw, name):
    require(bool(raw), name + ' is required')
    path = Path(raw).absolute()
    require(not path.is_symlink(), name + ' must not be a symlink')
    path.mkdir(mode=0o700, parents=True, exist_ok=True)
    info = path.stat()
    require(info.st_uid == os.geteuid() and not info.st_mode & 0o077, name + ' must be private and owned by caller')
    require(path.resolve() != ROOT and ROOT not in path.resolve().parents, name + ' must be outside the repository')
    return path.resolve()


def load_private(path, name):
    require(bool(path), name + ' is required')
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
    with os.fdopen(fd) as stream:
        info = os.fstat(stream.fileno())
        require(stat.S_ISREG(info.st_mode) and info.st_uid == os.geteuid() and not info.st_mode & 0o077,
                name + ' must be a private regular file')
        return json.load(stream)


def write_private(path, value):
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, 'w') as stream:
        json.dump(value, stream, sort_keys=True, indent=2)
        stream.write('\n')
        stream.flush()
        os.fsync(stream.fileno())


def operations():
    text = (ROOT / 'agent/crates/layerx-agent-api/src/operation_generated.rs').read_text()
    body = text[text.index('pub const fn name(self)'):]
    body = body[:body.index('\n    }\n')]
    names = re.findall(r'=> "([a-z_.\-]+)"', body)
    require(len(names) == 51 and len(set(names)) == 51, 'generated catalogue must name exactly 51 operations')
    require('faucet.claim' in names, 'generated catalogue lost faucet.claim')
    require('policy.dry_run' in names, 'generated catalogue lacks policy.dry_run')
    return sorted(names)


def mutating_operations():
    text = (ROOT / 'agent/crates/layerx-agent-api/src/operation_generated.rs').read_text()
    names = dict(re.findall(r'Self::([A-Za-z]+) => "([a-z_.\-]+)"', text[text.index('pub const fn name(self)'):]))
    body = text[text.index('pub const fn mutating(self) -> bool'):]
    body = body[body.index('matches!('):body.index('\n        )\n')]
    variants = set(re.findall(r'Self::([A-Za-z]+)', body))
    require(variants and variants <= set(names), 'generated mutating classification unreadable')
    return {names[variant] for variant in variants}


def source_identity():
    revision = capture(['git', 'rev-parse', 'HEAD'])
    require(not capture(['git', 'status', '--porcelain', '--', *SOURCES]), 'candidate source tree is dirty')
    tracked = [n for n in capture(['git', 'ls-files', '-z', '--', *SOURCES]).split('\0') if n]
    return {'revision': revision, 'tree': capture(['git', 'rev-parse', 'HEAD^{tree}']),
            'sources': {name: digest(ROOT / name) for name in sorted(tracked)},
            'operations': operations()}


def cargo_executables(stdout, want):
    found = {}
    for line in stdout.splitlines():
        if not line.startswith('{'):
            continue
        row = json.loads(line)
        if row.get('reason') == 'compiler-artifact' and row.get('executable'):
            key = (row['target']['name'], 'test' if row['profile'].get('test') else 'bin')
            if key in want:
                found[want[key]] = row['executable']
    require(set(found) == set(want.values()), 'cargo did not report every required executable')
    return found


def record(path):
    path = Path(path).resolve(strict=True)
    require(path.is_file() and path.stat().st_size > 0, 'empty artifact ' + str(path))
    return {'path': str(path), 'sha256': digest(path), 'bytes': path.stat().st_size}


def build(manifest):
    manifest = Path(manifest).absolute()
    out = private_dir(str(manifest.parent), 'manifest directory')
    require(not manifest.exists(), 'refusing to replace a retained artifact manifest')
    identity = source_identity()
    bundle = out / ('agent-envelope-' + identity['revision'])
    bundle.mkdir(mode=0o700)
    log = (bundle / 'build.log').open('w')
    commands = []

    def step(argv, cwd=ROOT, env=None):
        commands.append({'cwd': str(Path(cwd).relative_to(ROOT)) if Path(cwd) != ROOT else '.', 'argv': [str(a) for a in argv]})
        result = subprocess.run([str(a) for a in argv], cwd=cwd, env=env, capture_output=True, text=True, timeout=1500)
        log.write('$ ' + ' '.join(str(a) for a in argv) + '\n' + result.stderr + '\n')
        log.flush()
        require(result.returncode == 0, 'build step failed: ' + ' '.join(str(a) for a in argv[:4]))
        return result.stdout

    artifacts = {}
    agent = step(['cargo', 'build', '--locked', '--release', '--manifest-path', 'agent/Cargo.toml', '-p', 'layerx-agentd',
                  '--bins', '--message-format=json'])
    artifacts.update(cargo_executables(agent, {('layerx-agentd', 'bin'): 'agentd'}))
    probe = step(['cargo', 'test', '--locked', '--release', '--manifest-path', 'agent/Cargo.toml', '-p', 'layerx-sdk',
                  '--test', 'agent_operation_envelope', '--no-run', '--message-format=json'])
    artifacts.update(cargo_executables(probe, {('agent_operation_envelope', 'test'): 'rust_probe'}))
    gateway = step(['cargo', 'build', '--locked', '--release', '--manifest-path', 'platform/hosted/gateway/Cargo.toml',
                    '--bin', 'layerx-gateway', '--message-format=json'])
    artifacts.update(cargo_executables(gateway, {('layerx-gateway', 'bin'): 'gateway'}))
    ts = ROOT / 'agent/sdk/typescript'
    step(['npm', 'ci', '--offline', '--ignore-scripts'], cwd=ts)
    step(['npm', 'run', 'build'], cwd=ts)
    artifacts['typescript_probe'] = str(ts / 'dist/test/agent-operation-envelope.test.js')
    go_probe = bundle / 'go-agent-envelope.test'
    step(['go', 'test', '-c', '-o', go_probe, '.'], cwd=ROOT / 'platform/sdk/go')
    artifacts['go_probe'] = str(go_probe)
    jvm = ROOT / 'platform/sdk/jvm'
    classpath_file = bundle / 'jvm-classpath.txt'
    step(['mvn', '-q', '-o', '-B', 'test-compile', 'dependency:build-classpath',
          '-Dmdep.outputFile=' + str(classpath_file), '-Dmdep.includeScope=test'], cwd=jvm)
    artifacts['java_probe'] = str(jvm / 'target/test-classes/com/sidiora/layerx/sdk/AgentOperationEnvelopeProbe.class')
    artifacts['kotlin_probe'] = str(jvm / 'target/test-classes/com/sidiora/layerx/sdk/AgentOperationEnvelopeProbeKt.class')
    artifacts['jvm_main_classes'] = str(jvm / 'target/classes/com/sidiora/layerx/sdk/HttpProductionTransport.class')
    artifacts['jvm_classpath'] = str(classpath_file)
    swift = ROOT / 'platform/sdk/swift'
    step(['swift', 'build', '--build-tests', '-c', 'debug'], cwd=swift)
    swift_bin = Path(step(['swift', 'build', '--show-bin-path', '-c', 'debug'], cwd=swift).strip().splitlines()[-1])
    tests = sorted(swift_bin.glob('*PackageTests.xctest'))
    require(len(tests) == 1, 'swift test bundle not unique')
    artifacts['swift_probe'] = str(tests[0] / 'Contents/MacOS' / tests[0].stem) if (tests[0] / 'Contents').exists() else str(tests[0])
    dotnet = ROOT / 'platform/sdk/dotnet/tests/LayerX.Sdk.Tests'
    step(['dotnet', 'build', '-c', 'Release', '--no-restore', 'LayerX.Sdk.Tests.csproj'], cwd=dotnet)
    dlls = sorted(dotnet.glob('bin/Release/*/LayerX.Sdk.Tests.dll'))
    require(len(dlls) == 1, 'dotnet test assembly not unique')
    artifacts['csharp_probe'] = str(dlls[0])
    for name in ('agent/sdk/python/layerx_sdk/agent_http.py', 'agent/sdk/python/tests/test_agent_operation_envelope.py',
                 'tools/qualification/paxeer-x/agent_operation_envelope.py'):
        step([sys.executable, '-m', 'py_compile', name])
    artifacts['python_probe'] = str(ROOT / 'agent/sdk/python/tests/test_agent_operation_envelope.py')
    log.close()
    toolchains = {}
    for name, argv in (('rustc', ['rustc', '-vV']), ('cargo', ['cargo', '--version']), ('node', ['node', '--version']),
                       ('go', ['go', 'version']), ('java', ['java', '-version']), ('mvn', ['mvn', '-v']),
                       ('swift', ['swift', '--version']), ('dotnet', ['dotnet', '--version']),
                       ('python', [sys.executable, '--version'])):
        result = subprocess.run(argv, cwd=ROOT, capture_output=True, text=True)
        require(result.returncode == 0, 'toolchain identity ' + name)
        toolchains[name] = (result.stdout + result.stderr).strip()
    require(source_identity() == identity, 'source changed during build')
    value = {'schema': SCHEMA, 'source_revision': identity['revision'], 'source_tree': identity['tree'],
             'sources': identity['sources'], 'operations': identity['operations'], 'build_exit': 0,
             'commands': commands, 'toolchains': toolchains, 'build_log': str(bundle / 'build.log'),
             'artifacts': {name: record(path) for name, path in artifacts.items()}}
    write_private(manifest, value)
    print('PAXEER_X_AGENT_ENVELOPE_BUILD revision=' + identity['revision'] + ' artifacts=' + str(len(value['artifacts'])), flush=True)
    return 0


def load_manifest():
    built = load_private(os.environ.get('PAXEER_X_AGENT_ENVELOPE_ARTIFACT_MANIFEST'), 'PAXEER_X_AGENT_ENVELOPE_ARTIFACT_MANIFEST')
    require(built.get('schema') == SCHEMA and built.get('build_exit') == 0, 'artifact manifest schema or build exit')
    identity = source_identity()
    require(built['source_revision'] == identity['revision'] and built['source_tree'] == identity['tree']
            and built['sources'] == identity['sources'] and built['operations'] == identity['operations'],
            'artifact manifest does not bind the candidate revision')
    for name in ('agentd', 'rust_probe', 'gateway', 'typescript_probe', 'go_probe', 'java_probe', 'kotlin_probe',
                 'jvm_main_classes', 'jvm_classpath', 'swift_probe', 'csharp_probe', 'python_probe'):
        row = built['artifacts'].get(name)
        require(row is not None, 'missing candidate artifact ' + name)
        target = Path(row['path'])
        require(target.is_absolute() and target.is_file() and not target.is_symlink() and digest(target) == row['sha256'],
                'candidate artifact digest ' + name)
    for name in ('agentd', 'rust_probe', 'gateway', 'go_probe'):
        require(os.access(built['artifacts'][name]['path'], os.X_OK), 'candidate artifact not executable ' + name)
    return built


def load_config():
    config = load_private(os.environ.get('PAXEER_X_AGENT_ENVELOPE_CONFIG'), 'PAXEER_X_AGENT_ENVELOPE_CONFIG')
    require(config.get('schema') == CONFIG_SCHEMA, 'qualification configuration schema')
    for key in ('agentd_env', 'gateway_env', 'requests', 'credential_file', 'gateway_api_key_file',
                'program_bearer_file', 'gateway_peer_identity', 'program_route', 'native_rpc_body',
                'gateway_network_id', 'wire_version', 'route_bindings', 'agentd_binding_pointer', 'signer_key_file'):
        require(key in config, 'qualification configuration lacks ' + key)
    require(isinstance(config['gateway_network_id'], str) and config['gateway_network_id']
            and isinstance(config['wire_version'], str) and config['wire_version'], 'health identity values must be strings')
    expected = {'read': 'read.account', 'program_read': 'program.interface', 'approval_list': 'approval.list'}
    names = operations()
    expected.update({'operation.' + name: name for name in names})
    for case, operation in expected.items():
        row = config['requests'].get(case)
        require(isinstance(row, dict) and row.get('operation') == operation and isinstance(row.get('request'), dict),
                'provisioned request lacks ' + case)
    for case in ('allowed_mutation', 'changed_body'):
        row = config['requests'].get(case)
        require(isinstance(row, dict) and row.get('operation') in names and isinstance(row.get('request'), dict),
                'provisioned request lacks ' + case)
    require(config['requests']['changed_body']['operation'] == config['requests']['allowed_mutation']['operation']
            and config['requests']['changed_body']['request'] != config['requests']['allowed_mutation']['request'],
            'changed_body must change the allowed_mutation request body')
    require(config['requests']['allowed_mutation']['operation'] in mutating_operations(), 'allowed_mutation must be a mutating operation')
    row = config['requests'].get('wrong_scope')
    require(isinstance(row, dict) and set(row) == {'operation', 'request'} and row['operation'] in mutating_operations()
            and isinstance(row['request'], dict), 'provisioned request lacks wrong_scope mutation')
    row = config['requests'].get('revoked_session')
    require(isinstance(row, dict) and set(row) == {'credential_file'}, 'provisioned request lacks revoked_session credential')
    revoked = load_private(row['credential_file'], 'revoked_session credential_file')
    require(set(revoked) == {'tenant', 'session_id', 'token_id', 'generation'} and all(isinstance(v, str) for v in revoked.values())
            and 0 < len(revoked['tenant'].encode()) <= 255 and '\0' not in revoked['tenant']
            and re.fullmatch('[0-9a-f]{64}', revoked['session_id']) and re.fullmatch('[0-9a-f]{64}', revoked['token_id'])
            and re.fullmatch('(0|[1-9][0-9]{0,19})', revoked['generation']) and int(revoked['generation']) < 2**64,
            'revoked_session credential encoding')
    require(all('idempotency_key' not in row for row in config['requests'].values()), 'harness owns idempotency keys')

    require(isinstance(config['signer_key_file'], str) and Path(config['signer_key_file']).is_absolute(),
            'signer_key_file must be an absolute path')
    for case in W7_SIGN_DIRECT:
        row = config['requests'].get(case)
        require(isinstance(row, dict) and set(row) == {'prepare'} and isinstance(row['prepare'], dict)
                and set(row['prepare']) == {'operation', 'request'} and row['prepare']['operation'] == 'prepare'
                and isinstance(row['prepare']['request'], dict), 'provisioned request lacks ' + case)
    require(config['requests'][W7_SIGN_DIRECT[0]]['prepare']['request'] != config['requests'][W7_SIGN_DIRECT[1]]['prepare']['request'],
            'sign cases must prepare distinct activities')

    row = config['requests']['operation.availability.fetch']
    require(set(row['request']) <= {'tenant', 'agent', 'selector', 'requested_verification_level', 'maximum_bytes', 'maximum_chunks'}
            and {'selector', 'requested_verification_level', 'maximum_bytes', 'maximum_chunks'} <= set(row['request'])
            and row['request']['requested_verification_level'] in W7_REQUEST_LEVELS
            and W7_REQUEST_LEVELS.index(row['request']['requested_verification_level']) <= W7_REQUEST_LEVELS.index('batch-included')
            and all(isinstance(row['request'][k], str) and re.fullmatch('[1-9][0-9]{0,19}', row['request'][k])
                    for k in ('selector', 'maximum_bytes', 'maximum_chunks')),
            'provisioned request lacks operation.availability.fetch (harness owns the deadline)')
    row = config['requests']['operation.capability.create']
    dims = row['request'].get('dimensions')
    require(set(row['request']) == {'tenant', 'agent_did', 'dimensions'} and isinstance(dims, dict) and set(dims) == set(W8_DIMENSIONS)
            and row['request']['tenant'] == load_private(config['credential_file'], 'credential_file')['tenant']
            and isinstance(row['request']['agent_did'], str) and row['request']['agent_did']
            and all(isinstance(dims[k], list) for k in W8_DIMENSIONS if k != 'expiry')
            and dims['assets'] and dims['amount_ceilings'] and dims['rate_ceilings']
            and all(isinstance(a, str) and re.fullmatch('[0-9a-f]{64}', a) for a in dims['assets'] + dims['counterparties'])
            and len(set(dims['assets'])) == len(dims['assets'])
            and all(isinstance(c, dict) and set(c) == {'asset', 'amount'} and c['asset'] in dims['assets']
                    and isinstance(c['amount'], str) and re.fullmatch(W8_DECIMAL, c['amount']) for c in dims['amount_ceilings'])
            and len({c['asset'] for c in dims['amount_ceilings']}) == len(dims['amount_ceilings'])
            and all(isinstance(r, dict) and set(r) == {'window_seconds', 'maximum_actions'}
                    and isinstance(r['window_seconds'], str) and re.fullmatch('[1-9][0-9]{0,19}', r['window_seconds'])
                    and isinstance(r['maximum_actions'], str) and re.fullmatch('(0|[1-9][0-9]{0,19})', r['maximum_actions'])
                    for r in dims['rate_ceilings'])
            and len({r['window_seconds'] for r in dims['rate_ceilings']}) == len(dims['rate_ceilings'])
            and isinstance(dims['expiry'], str) and re.fullmatch('[1-9][0-9]{0,19}', dims['expiry']),
            'provisioned operation.capability.create needs non-empty lowercase-hex assets, amount_ceilings within assets and non-zero rate windows')
    row = config['requests']['operation.project']
    req = row['request']
    require(set(PROJECT_REQUEST) <= set(req) <= set(PROJECT_REQUEST) | {'tenant', 'agent'}
            and all(isinstance(req[k], str) and req[k] for k in req)
            and all(re.fullmatch(DECIMAL_U64, req[k]) and int(req[k]) < 2**64 for k in PROJECT_REQUEST)
            and int(req['protocol_activity_type']) < 2**32 and int(req['canonical_bytes']) <= MAX_BODY,
            'provisioned operation.project needs the four canonical fee projection decimals within their bounds')
    row = config['requests']['operation.policy.dry_run']
    req = row['request']
    require(set(req) == set(POLICY_DRY_RUN_REQUEST) and all(isinstance(req[k], str) for k in req)
            and req['tenant'] == load_private(config['credential_file'], 'credential_file')['tenant'] and req['agent_did']
            and all(re.fullmatch('[0-9a-f]{64}', req[k]) for k in ('session_id', 'capability_id', 'counterparty', 'asset'))
            and re.fullmatch(DECIMAL_U64, req['activity_type']) and int(req['activity_type']) < 2**16
            and re.fullmatch(W8_DECIMAL, req['amount']) and int(req['amount']) < 2**128
            and re.fullmatch(DECIMAL_U64, req['core_sequence']) and int(req['core_sequence']) < 2**64,
            'provisioned operation.policy.dry_run needs the ten typed dry-run fields for the provisioned tenant')
    row = config['requests']['operation.export.offline']
    req = row['request']
    facts = req.get('fact_set')
    require({'fact_set', 'requested_verification_level'} <= set(req) <= {'tenant', 'agent', 'fact_set', 'requested_verification_level'}
            and isinstance(facts, list) and 1 <= len(facts) <= MAX_FACTS and len(set(map(str, facts))) == len(facts)
            and all(isinstance(f, str) and len(f) <= 135 and re.fullmatch(FACT_REF, f) for f in facts)
            and req['requested_verification_level'] in LEVELS[:-1],
            'provisioned operation.export.offline needs 1..16 unique strict fact references below SettlementAnchored')
    row = config['requests']['operation.program.discover']
    req = row['request']
    require(set(req) == {'program_id', 'requested_verification_level'} and isinstance(req['program_id'], str)
            and re.fullmatch(NONZERO_HEX32, req['program_id']) and req['requested_verification_level'] == 'sequencer-signed',
            'provisioned operation.program.discover needs a non-zero lowercase program_id at sequencer-signed')
    row = config['requests']['operation.session.list']
    require(set(row['request']) == {'context'} and isinstance(row['request']['context'], dict)
            and row['request']['context'].get('tenant') == load_private(config['credential_file'], 'credential_file')['tenant']
            and isinstance(row['request']['context'].get('agent_did'), str) and row['request']['context']['agent_did'],
            'provisioned request lacks operation.session.list for the provisioned tenant')

    row = config['requests'].get('wait_bounded')
    require(isinstance(row, dict) and set(row) == {'operation', 'request'} and row['operation'] == 'wait'
            and isinstance(row['request'], dict) and set(row['request']) == {'submission_ref', 'requested_verification_level'}
            and row['request']['requested_verification_level'] in LEVELS,
            'provisioned request lacks wait_bounded (harness owns the deadline)')

    row = config['requests'].get('credential_expiry')
    require(isinstance(row, dict) and set(row) == {'credential_file', 'list'} and isinstance(row['list'], dict)
            and set(row['list']) == {'operation', 'request'} and row['list']['operation'] == 'session.list'
            and isinstance(row['list']['request'], dict), 'provisioned request lacks credential_expiry')
    expiring = load_private(row['credential_file'], 'credential_expiry credential_file')
    require(set(expiring) == {'tenant', 'session_id', 'token_id', 'generation'} and all(isinstance(v, str) for v in expiring.values())
            and re.fullmatch('[0-9a-f]{64}', expiring['session_id']) and re.fullmatch('[0-9a-f]{64}', expiring['token_id'])
            and re.fullmatch('(0|[1-9][0-9]{0,19})', expiring['generation']) and int(expiring['generation']) < 2**64,
            'credential_expiry credential encoding')
    require(expiring['session_id'] != json.loads(Path(config['credential_file']).read_text())['session_id'],
            'credential_expiry must use its own dedicated provisioned session')

    row = config['requests'].get('session_close_revocation')
    require(isinstance(row, dict) and set(row) == {'credential_file', 'operation', 'request', 'prepare'}
            and row['operation'] == 'session.close' and isinstance(row['request'], dict)
            and isinstance(row['prepare'], dict) and set(row['prepare']) == {'operation', 'request'}
            and row['prepare']['operation'] == 'prepare' and isinstance(row['prepare']['request'], dict),
            'provisioned request lacks session_close_revocation')
    closing = load_private(row['credential_file'], 'session_close_revocation credential_file')
    require(set(closing) == {'tenant', 'session_id', 'token_id', 'generation'} and all(isinstance(v, str) for v in closing.values())
            and re.fullmatch('[0-9a-f]{64}', closing['session_id']) and re.fullmatch('[0-9a-f]{64}', closing['token_id'])
            and re.fullmatch('(0|[1-9][0-9]{0,19})', closing['generation']) and int(closing['generation']) < 2**64
            and row['request'].get('session_id') == closing['session_id'],
            'session_close_revocation must target its own dedicated provisioned session')
    require(closing['session_id'] != json.loads(Path(config['credential_file']).read_text())['session_id'],
            'session_close_revocation must not close the shared provisioned session')

    row = config['requests'].get('session_refresh_credential')
    require(isinstance(row, dict) and set(row) == {'credential_file', 'operation', 'request'}
            and row['operation'] == 'session.refresh' and isinstance(row['request'], dict),
            'provisioned request lacks session_refresh_credential')
    rotated = load_private(row['credential_file'], 'session_refresh_credential credential_file')
    require(set(rotated) == set(W20_SESSION_FIELDS) and all(isinstance(v, str) for v in rotated.values())
            and re.fullmatch('[0-9a-f]{64}', rotated['session_id']) and re.fullmatch('[0-9a-f]{64}', rotated['token_id'])
            and re.fullmatch('(0|[1-9][0-9]{0,19})', rotated['generation']) and int(rotated['generation']) < 2**64
            and row['request'].get('session_id') == rotated['session_id'],
            'session_refresh_credential must target its own dedicated provisioned session')
    require(rotated['session_id'] != json.loads(Path(config['credential_file']).read_text())['session_id'],
            'session_refresh_credential must not rotate the shared provisioned session')
    require('{route_bindings_file}' in json.dumps(config['gateway_env']), 'gateway_env must load the generated route bindings')
    key_line = Path(config['gateway_api_key_file']).read_text()
    require(re.fullmatch(r'[^:\s]+:[^:\s]+\n?', key_line), 'gateway API key file must be one line <key_id>:<secret>')
    for key in ('credential_file', 'gateway_api_key_file', 'program_bearer_file'):
        load_private(config[key], key) if key == 'credential_file' else require(
            Path(config[key]).is_file() and not Path(config[key]).stat().st_mode & 0o077, 'provisioned authority ' + key)
    credential = load_private(config['credential_file'], 'credential_file')
    require(set(credential) == {'tenant', 'session_id', 'token_id', 'generation'}
            and all(isinstance(value, str) for value in credential.values()), 'provisioned credential coordinates')
    require(0 < len(credential['tenant'].encode()) <= 255 and '\0' not in credential['tenant']
            and re.fullmatch('[0-9a-f]{64}', credential['session_id']) and re.fullmatch('[0-9a-f]{64}', credential['token_id']),
            'provisioned credential encoding')
    require(re.fullmatch('(0|[1-9][0-9]{0,19})', credential['generation']) and int(credential['generation']) < 2**64,
            'provisioned generation is not canonical')
    for key in ('agentd_env', 'gateway_env'):
        require(isinstance(config[key], dict) and all(isinstance(k, str) and isinstance(v, str) for k, v in config[key].items()),
                key + ' must be a string map')
    require('LAYERX_AGENTD_RPC_LISTEN' not in config['agentd_env'], 'harness owns the agent RPC listener configuration')
    return config, credential


def tls_material(directory, peer):
    tls = directory / 'tls'
    tls.mkdir(mode=0o700)
    log = (directory / 'tls-producer.log').open('wb')

    def openssl(*argv):
        subprocess.run(['openssl', *[str(a) for a in argv]], check=True, stdout=log, stderr=log)

    for ca in ('ca', 'wrong-ca'):
        openssl('req', '-x509', '-newkey', 'ec', '-pkeyopt', 'ec_paramgen_curve:P-256', '-nodes', '-days', '1',
                '-subj', '/CN=paxeer-x-' + ca, '-addext', 'basicConstraints=critical,CA:TRUE',
                '-addext', 'keyUsage=critical,keyCertSign', '-keyout', tls / (ca + '.key'), '-out', tls / (ca + '.pem'))
    for name, ca, san, usage in (('agentd', 'ca', 'localhost', 'serverAuth'), ('gateway', 'ca', 'localhost', 'serverAuth'),
                                 ('gateway-client', 'ca', peer, 'clientAuth'), ('wrong-peer', 'ca', 'wrong-' + peer, 'clientAuth'),
                                 ('untrusted-client', 'wrong-ca', peer, 'clientAuth'), ('wrong-server', 'wrong-ca', 'localhost', 'serverAuth')):
        openssl('req', '-newkey', 'ec', '-pkeyopt', 'ec_paramgen_curve:P-256', '-nodes', '-subj', '/CN=' + san,
                '-keyout', tls / (name + '.key'), '-out', tls / (name + '.csr'))
        ext = tls / (name + '.ext')
        ext.write_text('subjectAltName=DNS:' + san + '\nextendedKeyUsage=' + usage + '\nbasicConstraints=CA:FALSE\n')
        openssl('x509', '-req', '-in', tls / (name + '.csr'), '-CA', tls / (ca + '.pem'), '-CAkey', tls / (ca + '.key'),
                '-CAcreateserial', '-days', '1', '-extfile', ext, '-out', tls / (name + '.pem'))
    for name in ('ca', 'wrong-ca', 'gateway', 'agentd'):
        openssl('x509', '-in', tls / (name + '.pem'), '-outform', 'DER', '-out', tls / (name + '.der'))
    openssl('pkcs8', '-topk8', '-nocrypt', '-in', tls / 'gateway.key', '-outform', 'DER', '-out', tls / 'gateway-key.der')
    password = tls / 'client-identity.password'
    password.write_text(os.urandom(16).hex())
    password.chmod(0o600)
    for name in ('gateway-client', 'wrong-peer'):
        openssl('pkcs12', '-export', '-in', tls / (name + '.pem'), '-inkey', tls / (name + '.key'), '-certfile', tls / 'ca.pem',
                '-passout', 'file:' + str(password), '-out', tls / (name + '.p12'))
    log.close()
    return tls


class Qualification:
    def __init__(self, directory, built, config, credential, runtime):
        self.d, self.built, self.config, self.credential, self.runtime = directory, built, config, credential, runtime
        self.tls = tls_material(directory, config['gateway_peer_identity'])
        ports = fixture.reserve_ports(2)
        self.agent_port, self.gateway_port = (s.getsockname()[1] for s in ports)
        for sock in ports:
            sock.close()
        self.agentd = self.gateway = None
        self.results = []
        self.request_id = 1000
        self.key_nonce = os.urandom(32)
        self.effects = []

        self.operation_evidence_rows = []
        self.probe_tls = self.export_probe_tls()

    def export_probe_tls(self):
        evidence = private_dir(os.environ.get('PAXEER_X_EVIDENCE_DIR'), 'PAXEER_X_EVIDENCE_DIR')
        target = Path(tempfile.mkdtemp(prefix='probe-tls-', dir=evidence))
        files = {'client_cert_file': 'gateway-client.pem', 'client_key_file': 'gateway-client.key', 'server_ca_file': 'ca.pem'}
        out = {}
        for key, name in files.items():
            shutil.copyfile(self.tls / name, target / name)
            os.chmod(target / name, 0o600)
            out[key] = str(target / name)
        return out

    def key(self, language, case):
        return hashlib.sha256(self.key_nonce + language.encode() + b'\0' + case.encode()).hexdigest()

    def effect(self, language, case, operation, key):
        self.effects.append({'language': language, 'case': case, 'operation': operation, 'idempotency_key': key})

    def operation_evidence(self, language, case, operation, row, record_path, mutating):
        if not record_path.is_file():
            return 'absent', False
        try:
            record = json.loads(record_path.read_text())
        except (OSError, ValueError):
            return 'malformed_record', False
        if not isinstance(record, dict) or set(record) != {'status', 'body'}:
            return 'malformed_record', False
        status, body = record['status'], record['body']
        if not isinstance(status, int) or isinstance(status, bool) or not isinstance(body, dict):
            return 'no_response', False
        label = language + ' ' + case
        if set(body) == {'request_id', 'value', 'verification_status'}:
            observed = 'success_record'
        else:
            observed = 'refusal_record:' + str(body.get('class')) + ':' + str(body.get('reason'))
        if operation in RULED_REFUSALS:
            http_status, klass, reason = RULED_REFUSALS[operation]
            try:
                error = self.refusal(status, json.dumps(body), http_status, klass, reason, label)
            except (RuntimeError, TypeError, KeyError):
                return observed, False
            if error['retriability'] != 'Terminal' or error['protocol_result_code'] is not None:
                return observed, False
            return 'ruled_' + observed, True
        if observed != 'success_record':
            return observed, False
        try:
            self.success(status, json.dumps(body), body['request_id'], label)
        except (RuntimeError, TypeError, KeyError, AttributeError):
            return 'malformed_success_record', False
        if not isinstance(body['request_id'], str) or not re.fullmatch('(0|[1-9][0-9]{0,19})', body['request_id']):
            return 'malformed_success_record', False
        if not mutating:
            return observed, True
        key = row.get('idempotency_key')
        effect = {'language': language, 'case': case, 'operation': operation, 'idempotency_key': key}
        if not isinstance(key, str) or not re.fullmatch('[0-9a-f]{64}', key) or effect not in self.effects:
            return 'success_record_without_effect', False
        return 'success_record+effect', True

    def judge_cases(self, language, run, phase, cases, requests):
        mutating = mutating_operations()
        directory = self.d / 'probes' / (language + '-' + run)
        failed = []
        for case in cases:
            if not (case.startswith('operation.') or (phase == 'read' and case in LANGUAGE_CASES)):
                continue
            row = requests[case]
            operation = row['operation']
            kind, ok = self.operation_evidence(language, case, operation, row, directory / (case + '.json'),
                                               operation in mutating)
            self.operation_evidence_rows.append({'language': language, 'run': run, 'case': case, 'operation': operation,
                                                 'evidence': kind, 'verdict': 'PASS' if ok else 'FAIL'})
            print('PAXEER_X_AGENT_ENVELOPE_EVIDENCE language=' + language + ' operation=' + operation + ' case=' + case
                  + ' evidence=' + kind + ' verdict=' + ('PASS' if ok else 'FAIL'), flush=True)
            if not ok:
                failed.append(case)
        require(not failed, language + ' catalogue cases lack process evidence: ' + ', '.join(failed))

    def substitute(self, env, extra=None):
        values = {'tls_dir': str(self.tls), 'state_dir': str(self.d / 'agent-state'), 'runtime_dir': str(self.runtime.directory),
                  'agent_rpc_port': str(self.agent_port), 'gateway_port': str(self.gateway_port),
                  'node_rpc_url': self.runtime.rpc_url, 'replica_url': 'http://127.0.0.1:' + str(self.runtime.ports[8]),
                  'node_socket': self.runtime.manifest['node_socket'],
                  'route_bindings_file': str(self.d / 'route-bindings.json')}
        values.update(extra or {})
        return {key: value.format(**values) for key, value in env.items()}

    def start_agentd(self, overrides=None, expect_up=True):
        if self.agentd is not None and self.agentd.poll() is None:
            self.agentd.terminate()
            self.agentd.wait(timeout=15)
        (self.d / 'agent-state').mkdir(mode=0o700, exist_ok=True)
        env = dict(self.runtime.env)
        env.update(self.substitute(self.config['agentd_env']))
        env.update({'LAYERX_AGENTD_RPC_LISTEN': '127.0.0.1:' + str(self.agent_port),
                    'LAYERX_AGENTD_RPC_TLS_CERT': str(self.tls / 'agentd.pem'),
                    'LAYERX_AGENTD_RPC_TLS_KEY': str(self.tls / 'agentd.key'),
                    'LAYERX_AGENTD_RPC_TLS_CLIENT_CA': str(self.tls / 'ca.pem'),
                    'LAYERX_AGENTD_RPC_PEER': self.config['gateway_peer_identity']})
        env.update(overrides or {})
        with (self.d / 'agentd.log').open('ab') as log:
            self.agentd = subprocess.Popen([self.built['artifacts']['agentd']['path']], env=env, stdout=log, stderr=log)
        if expect_up:
            self.wait_port(self.agent_port, self.agentd, 'agentd')
        return self.agentd

    def start_gateway(self, client_identity='gateway-client', outbound_ca='ca'):
        if self.gateway is not None and self.gateway.poll() is None:
            self.gateway.terminate()
            self.gateway.wait(timeout=15)
        env = dict(self.runtime.env)
        env.update(self.substitute(self.config['gateway_env']))
        env.update({'LAYERX_GATEWAY_LISTEN': '127.0.0.1:' + str(self.gateway_port),
                    'LAYERX_GATEWAY_TLS_CERT_DER': str(self.tls / 'gateway.der'),
                    'LAYERX_GATEWAY_TLS_KEY_DER': str(self.tls / 'gateway-key.der'),
                    'LAYERX_GATEWAY_OUTBOUND_CA_DER': str(self.tls / (outbound_ca + '.der'))})
        if client_identity is None:
            env.pop('LAYERX_GATEWAY_CLIENT_IDENTITY_PKCS12', None)
            env.pop('LAYERX_GATEWAY_CLIENT_IDENTITY_PASSWORD_FILE', None)
        else:
            env['LAYERX_GATEWAY_CLIENT_IDENTITY_PKCS12'] = str(self.tls / (client_identity + '.p12'))
            env['LAYERX_GATEWAY_CLIENT_IDENTITY_PASSWORD_FILE'] = str(self.tls / 'client-identity.password')
        with (self.d / 'gateway.log').open('ab') as log:
            self.gateway = subprocess.Popen([self.built['artifacts']['gateway']['path']], env=env, stdout=log, stderr=log)
        self.wait_port(self.gateway_port, self.gateway, 'gateway')

    def wait_port(self, port, process, name):
        def up():
            require(process.poll() is None, name + ' exited during boot')
            with socket.create_connection(('127.0.0.1', port), timeout=.3):
                return True
        self.runtime.wait(up)

    def stop(self):
        for process in (self.gateway, self.agentd):
            if process is not None and process.poll() is None:
                process.terminate()
                process.wait(timeout=15)

    def passed(self, case, evidence):
        require(case not in [row['case'] for row in self.results], 'case counted twice: ' + case)
        self.results.append({'case': case, 'result': 'passed', 'evidence': str(evidence)})
        print('PAXEER_X_PROGRESS cases=' + str(len(self.results)) + ' last=' + case, flush=True)

    def context(self, verify=True, client=None, server_ca='ca'):
        ctx = ssl.SSLContext(ssl.PROTOCOL_TLS_CLIENT)
        ctx.minimum_version = ssl.TLSVersion.TLSv1_2
        ctx.load_verify_locations(str(self.tls / (server_ca + '.pem')))
        if client:
            ctx.load_cert_chain(str(self.tls / (client + '.pem')), str(self.tls / (client + '.key')))
        return ctx

    def raw(self, port, payload, ctx, case):
        with socket.create_connection(('127.0.0.1', port), timeout=20) as sock:
            with ctx.wrap_socket(sock, server_hostname='localhost') as tls:
                tls.sendall(payload)
                chunks = []
                while True:
                    chunk = tls.recv(65536)
                    if not chunk:
                        break
                    chunks.append(chunk)
        data = b''.join(chunks)
        (self.d / 'responses' / (case + '.http')).write_bytes(data)
        head, _, body = data.partition(b'\r\n\r\n')
        status = int(head.split(b' ', 2)[1])
        return status, body

    def http(self, body, path=ROUTE, headers=None, method='POST', case='case', port=None, ctx=None):
        headers = dict(headers or {})
        headers.setdefault('Host', 'localhost')
        headers.setdefault('Content-Type', 'application/json')
        headers.setdefault('Connection', 'close')
        if 'Content-Length' not in headers and body is not None:
            headers['Content-Length'] = str(len(body))
        head = method + ' ' + path + ' HTTP/1.1\r\n' + ''.join(k + ': ' + v + '\r\n' for k, v in headers.items()) + '\r\n'
        return self.raw(port or self.gateway_port, head.encode() + (body or b''), ctx or self.context(), case)

    def api_key(self):
        return {'Authorization': 'LayerX-Key ' + Path(self.config['gateway_api_key_file']).read_text().strip()}

    def envelope(self, operation, request, credential=True, idempotency=None, **extra):
        self.request_id += 1
        value = {'version': 1, 'request_id': str(self.request_id), 'operation': operation, 'request': request,
                 'credential': dict(self.credential) if credential else None}
        if idempotency is not None:
            value['idempotency_key'] = idempotency
        value.update(extra)
        return value

    def encode(self, value):
        return json.dumps(value, separators=(',', ':')).encode()

    def success(self, status, body, request_id, case):
        require(status == 200, case + ': expected 200, got ' + str(status))
        value = json.loads(body)
        require(set(value) == {'request_id', 'value', 'verification_status'}, case + ': success envelope fields')
        require(value['request_id'] == str(request_id), case + ': request_id not preserved')
        vs = value['verification_status']
        if vs.get('state') == 'achieved':
            require(set(vs) == {'state', 'level'} and vs['level'] in LEVELS, case + ': verification status')
        else:
            require(set(vs) == {'state', 'requested', 'achieved', 'reason'} and vs['state'] == 'unverified'
                    and vs['requested'] in LEVELS and vs['achieved'] in LEVELS
                    and LEVELS.index(vs['achieved']) < LEVELS.index(vs['requested']), case + ': unverified status')
        return value

    def refusal(self, status, body, http_status, klass, reason, case, request_id=None):
        require(status == http_status, case + ': expected HTTP ' + str(http_status) + ', got ' + str(status))
        value = json.loads(body)
        require(set(value) == {'class', 'protocol_result_code', 'retriability', 'request_id', 'reason'}, case + ': error envelope fields')
        require(value['class'] in CLASSES and value['retriability'] in ('Terminal', 'Retriable'), case + ': error lattice')
        require(value['class'] == klass, case + ': class ' + value['class'] + ' != ' + klass)
        require(reason is None or value['reason'] == reason, case + ': reason ' + value['reason'])
        require(request_id is None or value['request_id'] == str(request_id), case + ': error request_id')
        require(value['protocol_result_code'] is None or isinstance(value['protocol_result_code'], int), case + ': result code')
        return value

    def call(self, case, operation, request, **kwargs):
        value = self.envelope(operation, request, **kwargs)
        status, body = self.http(self.encode(value), headers=self.api_key(), case=case)
        return value, status, body

    def direct_cases(self):
        req = self.config['requests']
        for case, key in (('read_account', 'read'), ('program_read', 'program_read'), ('approval_list', 'approval_list')):
            value, status, body = self.call(case, req[key]['operation'], req[key]['request'])
            self.success(status, body, value['request_id'], case)
            self.passed(case, self.d / 'responses' / (case + '.http'))
        mutation = req['allowed_mutation']
        key = os.urandom(32).hex()
        value = self.envelope(mutation['operation'], mutation['request'], credential=False, idempotency=key)
        status, body = self.http(self.encode(value), headers={'Authorization': 'Bearer ' + Path(self.config['program_bearer_file']).read_text().strip()},
                                 case='program_bearer_alone')
        require(status in (400, 401, 403), 'program_bearer_alone: bearer authorized a catalogue write')
        edge = json.loads(body)
        require(edge.get('class') in ('ProtocolIncompatibility', 'PolicyRefusal') or (edge.get('ok') is False and 'error' in edge),
                'program_bearer_alone: refusal shape')
        self.passed('program_bearer_alone', self.d / 'responses/program_bearer_alone.http')
        status, body = self.http(self.encode(value), headers=self.api_key(), case='gateway_key_alone_write')
        require(status in (400, 401, 403), 'gateway_key_alone_write: gateway key authorized a catalogue write')
        require(json.loads(body)['class'] in ('ProtocolIncompatibility', 'PolicyRefusal'), 'gateway_key_alone_write: class')
        self.passed('gateway_key_alone_write', self.d / 'responses/gateway_key_alone_write.http')
        value = self.envelope('read.account', req['read']['request'])
        forged = dict(self.api_key(), **{'LayerX-Tenant': 'forged', 'LayerX-Agent': 'forged', 'X-Forwarded-For': '203.0.113.1'})
        status, body = self.http(self.encode(value), headers=forged, case='forged_principal_header')
        self.success(status, body, value['request_id'], 'forged_principal_header')
        self.direct_daemon_header_principal(value)
        self.passed('forged_principal_header', self.d / 'responses/forged_principal_header.http')
        bad = [('malformed_body', b'{"version":1,"version":1}', 400, 'ProtocolIncompatibility', 'envelope.malformed'),
               ('truncated_body', self.encode(self.envelope('read.account', req['read']['request']))[:-7], 400,
                'ProtocolIncompatibility', 'envelope.malformed')]
        for case, body, http_status, klass, reason in bad:
            status, response = self.http(body, headers=self.api_key(), case=case)
            self.refusal(status, response, http_status, klass, reason, case, request_id=0)
            self.passed(case, self.d / 'responses' / (case + '.http'))
        status, response = self.http(b'\xff\xfe' + b' ' * 16, headers=self.api_key(), case='malformed_body_utf8')
        self.refusal(status, response, 400, 'ProtocolIncompatibility', 'envelope.malformed', 'malformed_body_utf8', request_id=0)
        status, response = self.http(b'{"version":1}', headers=dict(self.api_key(), **{'Content-Length': '200'}), case='truncated_framing')
        require(status in (400, 408) or not response, 'truncated_framing accepted a short body')
        over = self.envelope('read.account', req['read']['request'])
        over['request'] = dict(req['read']['request'])
        body = self.encode(over)
        body = body[:-1] + b',"padding":"' + b'a' * (MAX_BODY - len(body) + 16) + b'"}'
        require(len(body) > MAX_BODY, 'oversized case is not over the bound')
        status, response = self.http(body, headers=self.api_key(), case='oversized_body')
        self.refusal(status, response, 413, 'ProtocolIncompatibility', 'envelope.oversized', 'oversized_body')
        self.passed('oversized_body', self.d / 'responses/oversized_body.http')
        value = self.envelope('read.unknown_operation', req['read']['request'])
        status, response = self.http(self.encode(value), headers=self.api_key(), case='unknown_operation')
        self.refusal(status, response, 404, 'ProtocolIncompatibility', 'envelope.unknown_operation', 'unknown_operation', value['request_id'])
        self.passed('unknown_operation', self.d / 'responses/unknown_operation.http')
        value = self.envelope('read.account', req['read']['request'], principal='forged')
        status, response = self.http(self.encode(value), headers=self.api_key(), case='unknown_field')
        self.refusal(status, response, 400, 'ProtocolIncompatibility', 'envelope.unknown_field', 'unknown_field')
        value = self.envelope('read.account', req['read']['request'])
        value['credential']['agent'] = 'forged'
        status, response = self.http(self.encode(value), headers=self.api_key(), case='unknown_credential_field')
        self.refusal(status, response, 400, 'ProtocolIncompatibility', 'envelope.unknown_field', 'unknown_credential_field')
        self.passed('unknown_field', self.d / 'responses/unknown_field.http')
        for case, version in (('bad_version', 2), ('bad_version_string', '1')):
            value = self.envelope('read.account', req['read']['request'])
            value['version'] = version
            status, response = self.http(self.encode(value), headers=self.api_key(), case=case)
            self.refusal(status, response, 400, 'ProtocolIncompatibility', 'envelope.version', case)
        self.passed('bad_version', self.d / 'responses/bad_version.http')
        for case, generation in (('noncanonical_leading_zero', '0' + self.credential['generation']),
                                 ('noncanonical_overflow', str(2**64)), ('noncanonical_sign', '+' + self.credential['generation'])):
            value = self.envelope('read.account', req['read']['request'])
            value['credential']['generation'] = generation
            status, response = self.http(self.encode(value), headers=self.api_key(), case=case)
            self.refusal(status, response, 400, 'ProtocolIncompatibility', 'envelope.noncanonical_integer', case)
        value = self.envelope('read.account', req['read']['request'])
        value['credential']['generation'] = int(self.credential['generation'])
        status, response = self.http(self.encode(value), headers=self.api_key(), case='noncanonical_number')
        require(status == 400 and json.loads(response)['class'] == 'ProtocolIncompatibility', 'noncanonical_number accepted')
        value = self.envelope('read.account', req['read']['request'])
        value['request_id'] = '0' + value['request_id']
        status, response = self.http(self.encode(value), headers=self.api_key(), case='noncanonical_request_id')
        self.refusal(status, response, 400, 'ProtocolIncompatibility', 'envelope.noncanonical_integer', 'noncanonical_request_id')
        self.passed('noncanonical_integer', self.d / 'responses/noncanonical_overflow.http')
        value, status, response = self.call('faucet_retired', 'faucet.claim', req['operation.faucet.claim']['request'],
                                            idempotency=os.urandom(32).hex())
        error = self.refusal(status, response, 503, 'UnavailableCapability', 'unavailable_capability.faucet.claim',
                             'faucet_retired', value['request_id'])
        require(error['retriability'] == 'Terminal' and error['protocol_result_code'] is None, 'faucet_retired: retriability')
        self.passed('faucet_retired', self.d / 'responses/faucet_retired.http')
        for field in ('tenant', 'agent'):
            request = dict(req['read']['request'], **{field: 'coordinate-mismatch'})
            value, status, response = self.call('coordinate_mismatch_' + field, req['read']['operation'], request)
            self.refusal(status, response, 403, 'PolicyRefusal', 'envelope.coordinate_mismatch',
                         'coordinate_mismatch_' + field, value['request_id'])
        self.passed('coordinate_mismatch', self.d / 'responses/coordinate_mismatch_tenant.http')
        status, response = self.http(self.config['native_rpc_body'].encode(), path='/rpc', headers=self.api_key(), case='native_rpc_unchanged')
        value = json.loads(response)
        require(status == 200 and value.get('jsonrpc') == '2.0' and 'request_id' not in value, 'native_rpc_unchanged: /rpc changed contract')
        status, response = self.http(None, path='/rpc/schema', method='GET', headers={'Content-Type': 'application/json'},
                                     case='native_rpc_schema_unchanged')
        require(status == 200 and 'verification_status' not in json.loads(response), 'native /rpc/schema changed contract')
        self.passed('native_rpc_unchanged', self.d / 'responses/native_rpc_unchanged.http')
        route = self.config['program_route']
        status, response = self.http(None if route['method'] == 'GET' else route['body'].encode(), path=route['path'],
                                     method=route['method'], headers=self.api_key(), case='programs_route_unchanged')
        require(status == route['status'] and 'verification_status' not in json.loads(response), 'programs route changed contract')
        status, response = self.http(self.encode(self.envelope('read.account', req['read']['request'])), path=ROUTE + '/',
                                     headers=self.api_key(), case='route_prefix_refused')
        require(status in (404, 405), 'agent route admitted a prefix match')
        status, response = self.http(None, path=ROUTE, method='GET', headers=self.api_key(), case='route_method_refused')
        require(status == 405, 'agent route admitted GET')
        self.passed('programs_route_unchanged', self.d / 'responses/programs_route_unchanged.http')

    def direct_daemon_header_principal(self, value):
        status, response = self.http(self.encode(value), path='/rpc', headers={'LayerX-Tenant': 'forged'},
                                     case='daemon_header_principal', port=self.agent_port,
                                     ctx=self.context(client='gateway-client'))
        self.refusal(status, response, 403, 'PolicyRefusal', 'envelope.header_principal', 'daemon_header_principal')

    def mtls_cases(self):
        body = self.encode(self.envelope('read.account', self.config['requests']['read']['request']))
        def refused(case, ctx, port=None):
            try:
                status, _ = self.http(body, path='/rpc', case=case, port=port or self.agent_port, ctx=ctx)
            except (ssl.SSLError, ConnectionError, OSError):
                return
            raise RuntimeError('agent operation envelope refused: ' + case + ' answered HTTP ' + str(status))
        refused('mtls_no_client_cert', self.context())
        refused('mtls_untrusted_client', self.context(client='untrusted-client'))
        refused('mtls_wrong_peer_direct', self.context(client='wrong-peer'))
        self.passed('mtls_no_client_cert', self.d / 'agentd.log')
        self.start_gateway(client_identity='wrong-peer')
        status, response = self.http(body, headers=self.api_key(), case='mtls_wrong_peer')
        require(status in (502, 503) and b'verification_status' not in response, 'gateway with wrong peer identity reached the daemon')
        self.passed('mtls_wrong_peer', self.d / 'responses/mtls_wrong_peer.http')
        self.start_gateway(outbound_ca='wrong-ca')
        status, response = self.http(body, headers=self.api_key(), case='mtls_wrong_ca')
        require(status in (502, 503) and b'verification_status' not in response, 'gateway trusted a wrong daemon CA')
        self.start_gateway(client_identity=None)
        status, response = self.http(body, headers=self.api_key(), case='mtls_absent_identity')
        require(status in (502, 503) and b'verification_status' not in response, 'gateway without client identity reached the daemon')
        self.start_gateway()
        try:
            self.http(body, case='client_wrong_server_ca', ctx=self.context(server_ca='wrong-ca'))
            raise RuntimeError('agent operation envelope refused: gateway certificate verified under a wrong CA')
        except ssl.SSLCertVerificationError:
            pass
        self.passed('mtls_wrong_ca', self.d / 'responses/mtls_wrong_ca.http')
        with socket.create_connection(('127.0.0.1', self.agent_port), timeout=10) as sock:
            sock.sendall(b'POST /rpc HTTP/1.1\r\nHost: localhost\r\nContent-Type: application/json\r\nContent-Length: '
                         + str(len(body)).encode() + b'\r\nConnection: close\r\n\r\n' + body)
            sock.settimeout(10)
            try:
                data = sock.recv(65536)
            except (ConnectionError, socket.timeout):
                data = b''
        require(not data.startswith(b'HTTP/'), 'agent RPC listener answered plaintext HTTP')
        broken = self.start_agentd({'LAYERX_AGENTD_RPC_TLS_CLIENT_CA': str(self.d / 'absent-ca.pem')}, expect_up=False)
        require(broken.wait(timeout=60) != 0, 'agentd booted the RPC listener without its trusted client CA')
        self.start_agentd()
        self.start_gateway()
        self.passed('no_plaintext_fallback', self.d / 'agentd.log')

    def probe_requests(self, language):
        mutating = mutating_operations()
        requests = json.loads(json.dumps(self.config['requests']))
        requests['read_decode_failure'] = json.loads(json.dumps(requests['read']))
        requests['mutation_decode_unknown'] = json.loads(json.dumps(requests['allowed_mutation']))
        cases = ['allowed_mutation', 'wrong_scope', 'mutation_decode_unknown']
        cases += ['operation.' + name for name in self.built['operations'] if name in mutating]
        for case in cases:
            requests[case]['idempotency_key'] = self.key(language, case)
        return requests

    def idempotency_cases(self):
        req = self.config['requests']
        key = self.key('harness', 'idempotent_replay_same_body')
        self.effect('harness', 'idempotent_replay_same_body', req['allowed_mutation']['operation'], key)
        first = self.envelope(req['allowed_mutation']['operation'], req['allowed_mutation']['request'], idempotency=key)
        status, body = self.http(self.encode(first), headers=self.api_key(), case='idempotent_first')
        first_value = self.success(status, body, first['request_id'], 'idempotent_first')
        replay = self.envelope(req['allowed_mutation']['operation'], req['allowed_mutation']['request'], idempotency=key)
        status, body = self.http(self.encode(replay), headers=self.api_key(), case='idempotent_replay_same_body')
        replay_value = self.success(status, body, replay['request_id'], 'idempotent_replay_same_body')
        require(replay_value['value'] == first_value['value']
                and replay_value['verification_status'] == first_value['verification_status'],
                'idempotent replay returned a different recorded outcome')
        self.passed('idempotent_replay_same_body', self.d / 'responses/idempotent_replay_same_body.http')
        changed = self.envelope(req['changed_body']['operation'], req['changed_body']['request'], idempotency=key)
        status, body = self.http(self.encode(changed), headers=self.api_key(), case='idempotency_changed_body_conflict')
        self.refusal(status, body, 409, 'IdempotencyConflict', None, 'idempotency_changed_body_conflict', changed['request_id'])
        self.passed('idempotency_changed_body_conflict', self.d / 'responses/idempotency_changed_body_conflict.http')

    def bindings(self, network=None, ready=None):
        document = json.loads(json.dumps(self.config['route_bindings']))
        def fill(value):
            if isinstance(value, str):
                return self.substitute({'v': value})['v']
            if isinstance(value, list):
                return [fill(item) for item in value]
            if isinstance(value, dict):
                return {key: fill(item) for key, item in value.items()}
            return value
        document = fill(document)
        binding = document
        for part in self.config['agentd_binding_pointer'].lstrip('/').split('/'):
            part = part.replace('~1', '/').replace('~0', '~')
            binding = binding[int(part)] if isinstance(binding, list) else binding[part]
        require(binding.get('health_path') == HEALTH_PATH and binding.get('identity_path') == HEALTH_PATH
                and binding.get('ready_pointer') == '/ready' and binding.get('ready_value') is True
                and binding.get('network_pointer') == '/network_id'
                and binding.get('network_value') == self.config['gateway_network_id']
                and binding.get('version_pointer') == '/wire_version'
                and binding.get('version_value') == self.config['wire_version'], 'agentd route binding is not the frozen health binding')
        if network is not None:
            binding['network_value'] = network
        if ready is not None:
            binding['ready_value'] = ready
        path = self.d / 'route-bindings.json'
        if path.exists():
            path.unlink()
        fixture.write_json(path, document)

    def health_cases(self):
        status, body = self.http(None, path=HEALTH_PATH, method='GET', headers={'Content-Type': 'application/json'},
                                 case='health_ready', port=self.agent_port, ctx=self.context(client='gateway-client'))
        value = json.loads(body)
        require(status == 200 and set(value) == {'ready', 'network_id', 'wire_version'} and value['ready'] is True
                and value['network_id'] == self.config['gateway_network_id']
                and value['wire_version'] == self.config['wire_version'], 'agentd /healthz identity')
        try:
            self.http(None, path=HEALTH_PATH, method='GET', headers={'Content-Type': 'application/json'},
                      case='health_no_client_cert', port=self.agent_port, ctx=self.context())
            raise RuntimeError('agent operation envelope refused: /healthz answered without a client certificate')
        except (ssl.SSLError, ConnectionError, OSError):
            pass
        value = self.envelope(self.config['requests']['read']['operation'], self.config['requests']['read']['request'])
        status, body = self.http(self.encode(value), headers=self.api_key(), case='health_admitted_route')
        self.success(status, body, value['request_id'], 'health_admitted_route')
        self.passed('health_ready', self.d / 'responses/health_ready.http')
        for case, change in (('health_binding_wrong_network', {'network': 'wrong-' + self.config['gateway_network_id']}),
                             ('health_binding_ready_false', {'ready': False})):
            self.bindings(**change)
            self.start_gateway()
            value = self.envelope(self.config['requests']['read']['operation'], self.config['requests']['read']['request'])
            status, body = self.http(self.encode(value), headers=self.api_key(), case=case)
            require(status == 503 and b'route_unavailable' in body and b'verification_status' not in body,
                    case + ': gateway admitted an agent route whose health identity does not match')
            self.passed(case, self.d / 'responses' / (case + '.http'))
        self.bindings()
        self.start_gateway()

    def decode_server(self):
        observed = []
        mutating = mutating_operations()
        class Handler(BaseHTTPRequestHandler):
            def do_POST(handler):
                body = handler.rfile.read(int(handler.headers.get('Content-Length', '0')))
                value = json.loads(body)
                observed.append({'operation': value.get('operation'), 'request_id': value.get('request_id'),
                                 'idempotency_key': value.get('idempotency_key')})
                if value.get('operation') in mutating:
                    reply = {'request_id': value.get('request_id'), 'value': {}}
                else:
                    reply = {'request_id': 'mismatch-' + str(value.get('request_id')), 'value': {},
                             'verification_status': {'state': 'achieved', 'level': LEVELS[0]}}
                data = json.dumps(reply, separators=(',', ':')).encode()
                handler.send_response(200)
                handler.send_header('Content-Type', 'application/json')
                handler.send_header('Content-Length', str(len(data)))
                handler.send_header('Connection', 'close')
                handler.end_headers()
                handler.wfile.write(data)
            def log_message(handler, *args):
                pass
        server = ThreadingHTTPServer(('127.0.0.1', 0), Handler)
        ctx = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
        ctx.minimum_version = ssl.TLSVersion.TLSv1_2
        ctx.load_cert_chain(str(self.tls / 'gateway.pem'), str(self.tls / 'gateway.key'))
        server.socket = ctx.wrap_socket(server.socket, server_side=True)
        threading.Thread(target=server.serve_forever, daemon=True).start()
        return server, observed

    def read_cases(self, language):
        server, observed = self.decode_server()
        requests = self.probe_requests(language)
        endpoint = 'https://localhost:' + str(server.server_address[1]) + ROUTE
        for case in DECODE_CASES:
            requests[case]['endpoint'] = endpoint
        state = self.d / 'probes' / (language + '.state')
        try:
            if language == 'csharp':
                self.probe(language, LANGUAGE_CASES, 'read', state, requests)
                self.probe(language, DECODE_CASES, 'read', state, requests, decode_endpoint=endpoint,
                           run='read-decode', csharp_class='AgentOperationEnvelopeDecodeTests')
            else:
                self.probe(language, LANGUAGE_CASES + DECODE_CASES, 'read', state, requests, decode_endpoint=endpoint)
        finally:
            server.shutdown()
            server.server_close()
        read_op = requests['read_decode_failure']['operation']
        mutation = requests['mutation_decode_unknown']
        reads = [row for row in observed if row['operation'] == read_op]
        writes = [row for row in observed if row['operation'] == mutation['operation']]
        require(len(observed) == 2 and len(reads) == 1, language + ' read_decode_failure retried a DecodeFailure')
        require(len(writes) == 1 and writes[0]['idempotency_key'] == mutation['idempotency_key'],
                language + ' mutation_decode_unknown resent an Unknown mutation')
        self.effect(language, 'mutation_decode_unknown', mutation['operation'], mutation['idempotency_key'])
        self.decode_observed.append({'language': language, 'observed_requests': observed})

    def w20_wait(self):
        run = 'pre-restart-w20-wait'
        requests = json.loads(json.dumps(self.config['requests']))
        deadline = int(time.time()) + W20_WAIT_SECONDS
        requests['wait_bounded']['request']['deadline'] = str(deadline)
        self.probe('rust', W20_WAIT_PRE, 'pre-restart', self.d / 'probes/rust-w20-wait.state', requests, run=run)
        waited, _ = self.w20_success(run, 'wait_bounded')
        value = waited['value']
        requested = requests['wait_bounded']['request']['requested_verification_level']
        require(isinstance(value, dict) and set(value) == {'submission', 'actual_verification_level', 'deadline_elapsed'},
                'wait_bounded: AMEND 28b wait payload fields')
        require(value['deadline_elapsed'] is True, 'wait_bounded: deadline did not elapse')
        actual = value['actual_verification_level']
        require(actual in LEVELS and LEVELS.index(actual) < LEVELS.index(requested),
                'wait_bounded: achieved level must be reported and below the requested level')
        vs = waited['verification_status']
        require((vs['level'] if vs['state'] == 'achieved' else vs['achieved']) == actual,
                'wait_bounded: verification status disagrees with the reported achieved level')
        timing = json.loads((self.d / 'probes' / ('rust-' + run) / 'wait_bounded.timing.json').read_text())
        require(set(timing) == {'sent_unix', 'received_unix'} and all(isinstance(v, int) for v in timing.values())
                and timing['sent_unix'] <= deadline <= timing['received_unix'] and timing['received_unix'] - timing['sent_unix'] < 300,
                'wait_bounded: wait did not return at its deadline within the probe bound')

    def w20_expiry(self):
        run = 'post-restart-w20-expiry'
        case = 'credential_expiry'
        requests = json.loads(json.dumps(self.config['requests']))
        requests[case]['poll_seconds'] = W20_EXPIRY_POLL_SECONDS
        self.probe('rust', W20_EXPIRY_POST, 'post-restart', self.d / 'probes/rust-w20-expiry.state', requests, run=run)
        expiring = load_private(self.config['requests'][case]['credential_file'], case + ' credential_file')
        listed, _ = self.w20_success(run, case + '.list')
        sessions = listed['value'].get('sessions') if isinstance(listed['value'], dict) else None
        own = [s for s in sessions or [] if isinstance(s, dict) and s.get('session_id') == expiring['session_id']]
        require(len(own) == 1 and re.fullmatch('(0|[1-9][0-9]{0,19})', str(own[0].get('expiry_sequence'))),
                case + ': daemon did not report the session expiry')
        self.w20_success(run, case + '.before')
        self.w20_session_refused(run, case, 'session.expired')

    def w20_revocation(self):
        run = 'pre-restart-w20-revocation'
        case = 'session_close_revocation'
        requests = json.loads(json.dumps(self.config['requests']))
        mutating = mutating_operations()
        requests[case]['idempotency_key'] = self.key('rust', case)
        if 'prepare' in mutating:
            requests[case]['prepare']['idempotency_key'] = self.key('rust', case + '.prepare')
            self.effect('rust', case + '.prepare', 'prepare', requests[case]['prepare']['idempotency_key'])
        self.probe('rust', W20_REVOCATION_PRE, 'pre-restart', self.d / 'probes/rust-w20-revocation.state', requests, run=run)
        closing = load_private(self.config['requests'][case]['credential_file'], case + ' credential_file')
        self.w20_success(run, case + '.prepare')
        closed, _ = self.w20_success(run, case)
        record = closed['value']
        require(isinstance(record, dict) and record.get('session_id') == closing['session_id'] and record.get('open') is False,
                case + ': close did not durably close the dedicated session')
        self.w20_session_refused(run, case + '.old', 'session.revoked')
        tracked, _ = self.w20_success(run, case + '.track')
        require(isinstance(tracked['value'], dict) and tracked['value'].get('state') in W20_INVALIDATED_STATES,
                case + ': queued preparation under the closed generation was not reported invalidated')

    def w20_idempotency_requests(self):
        requests = json.loads(json.dumps(self.config['requests']))
        requests['restart_replay_idempotent_record'] = json.loads(json.dumps(requests['allowed_mutation']))
        requests['restart_replay_idempotent'] = {'replay_of': 'restart_replay_idempotent_record'}
        requests['restart_replay_idempotent_record']['idempotency_key'] = self.key('rust', 'restart_replay_idempotent_record')
        return requests

    def w20_idempotency_pre(self):
        requests = self.w20_idempotency_requests()
        self.probe('rust', W20_IDEMPOTENCY_PRE, 'pre-restart', self.d / 'probes/rust-w20-idempotency.state', requests,
                   run='pre-restart-w20-idempotency')
        self.w20_success('pre-restart-w20-idempotency', 'restart_replay_idempotent_record')

    def w20_idempotency_post(self):
        requests = self.w20_idempotency_requests()
        self.probe('rust', W20_IDEMPOTENCY_POST, 'post-restart', self.d / 'probes/rust-w20-idempotency.state', requests,
                   run='post-restart-w20-idempotency')
        first, _ = self.w20_success('pre-restart-w20-idempotency', 'restart_replay_idempotent_record')
        replay, _ = self.w20_success('post-restart-w20-idempotency', 'restart_replay_idempotent')
        require(replay['request_id'] != first['request_id'], 'restart_replay_idempotent: replay reused the request_id')
        require(replay['value'] == first['value'] and replay['verification_status'] == first['verification_status'],
                'restart_replay_idempotent: replay across the restart returned a different recorded effect')
        key = requests['restart_replay_idempotent_record']['idempotency_key']
        require([e['case'] for e in self.effects if e['idempotency_key'] == key] == ['restart_replay_idempotent_record'],
                'restart_replay_idempotent: the replay was recorded as a second effect')

    def w20_requests(self):
        requests = self.probe_requests('rust')
        mutating = mutating_operations()
        for case in ('session_refresh_credential', 'session_close_revocation', 'restart_replay_idempotent_record'):
            if case in requests and requests[case].get('operation') in mutating:
                requests[case]['idempotency_key'] = self.key('rust', case)
        return requests

    def w20_doc(self, run, name):
        path = self.d / 'probes' / ('rust-' + run) / (name + '.json')
        require(path.is_file(), 'probe did not record ' + name + ' in ' + run)
        doc = json.loads(path.read_text())
        require(set(doc) == {'status', 'body'} and isinstance(doc['status'], int) and isinstance(doc['body'], dict),
                name + ': recorded response shape')
        return doc['status'], doc['body'], path

    def w20_success(self, run, name):
        status, body, path = self.w20_doc(run, name)
        return self.success(status, json.dumps(body).encode(), body.get('request_id'), name), path

    def w20_session_refused(self, run, name, reason=None):
        status, body, path = self.w20_doc(run, name)
        value = self.refusal(status, json.dumps(body).encode(), 401 if body.get('reason') == 'session.not_authorized' else 403,
                             'PolicyRefusal', reason, name)
        return value, path

    def w20_no_bearer(self, run):
        directory = self.d / 'probes' / ('rust-' + run)
        for path in [*directory.glob('*.json'), self.d / 'probes' / ('rust-' + run + '.log')]:
            require('"bearer"' not in path.read_text() and 'Bearer ' not in path.read_text(),
                    'session bearer leaked into probe record ' + path.name)

    def w20_rotation(self):
        run = 'pre-restart-w20-rotation'
        requests = self.w20_requests()
        self.probe('rust', W20_ROTATION_PRE, 'pre-restart', self.d / 'probes/rust-w20-rotation.state', requests, run=run)
        case = 'session_refresh_credential'
        old = load_private(self.config['requests'][case]['credential_file'], case + ' credential_file')
        refreshed, path = self.w20_success(run, case)
        record = refreshed['value']
        require(isinstance(record, dict) and record.get('session_id') == old['session_id'] and record.get('open') is True
                and re.fullmatch('[0-9a-f]{64}', str(record.get('token_id'))) and record['token_id'] != old['token_id']
                and re.fullmatch('(0|[1-9][0-9]{0,19})', str(record.get('generation')))
                and int(record['generation']) > int(old['generation']), case + ': refresh did not rotate token and generation')
        replacement = json.loads((self.d / 'probes' / ('rust-' + run) / (case + '.credential.json')).read_text())
        require(set(replacement) == set(W20_SESSION_FIELDS) and replacement['tenant'] == old['tenant']
                and replacement['session_id'] == old['session_id'] and replacement['token_id'] == record['token_id']
                and replacement['generation'] == record['generation'], case + ': probe did not adopt the replacement credential')
        self.w20_success(run, case + '.new')
        self.w20_session_refused(run, case + '.old')
        self.w20_no_bearer(run)
        effects = json.dumps(self.effects)
        require('"bearer"' not in effects, case + ': bearer entered the effect manifest')

    def record_effects(self, language, cases, requests):
        for case in cases:
            row = requests.get(case)
            if case in DECODE_CASES or not row or 'idempotency_key' not in row:
                continue
            if any(e['language'] == language and e['case'] == case for e in self.effects):
                continue
            self.effect(language, case, row['operation'], row['idempotency_key'])

    def probe(self, language, cases, phase, state, requests=None, retry_state=None, prefix=False, decode_endpoint=None,
              run=None, csharp_class='AgentOperationEnvelopeTests'):
        run = phase if run is None else run
        case_file = self.d / 'probes' / (language + '-' + run + '.json')
        requests = self.probe_requests(language) if requests is None else requests
        fixture.write_json(case_file, {
            'endpoint': 'https://localhost:' + str(self.gateway_port) + ROUTE, 'server_name': 'localhost',
            'ca_pem': str(self.tls / 'ca.pem'), 'ca_der': str(self.tls / 'ca.der'),
            'gateway_api_key_file': self.config['gateway_api_key_file'],
            'program_bearer_file': self.config['program_bearer_file'],
            'credential_file': self.config['credential_file'],
            'requests': requests,
            'daemon_endpoint': 'https://localhost:' + str(self.agent_port) + '/rpc', **self.probe_tls,
            **({} if decode_endpoint is None else {'decode_endpoint': decode_endpoint}),
            'operations': self.built['operations'], 'cases': list(cases), 'phase': phase,
            'state_file': str(state), 'response_dir': str(self.d / 'probes' / (language + '-' + run)),
            **({} if retry_state is None else {'retry_state_file': str(retry_state)})})
        (self.d / 'probes' / (language + '-' + run)).mkdir(mode=0o700)
        art = self.built['artifacts']
        env = dict(self.runtime.env, PAXEER_X_AGENT_ENVELOPE_CASE=str(case_file))
        jvm_classpath = ':'.join([str(Path(art['jvm_main_classes']['path']).parents[4]),
                                  str(Path(art['java_probe']['path']).parents[4]),
                                  Path(art['jvm_classpath']['path']).read_text().strip()])
        argv = {
            'rust': [art['rust_probe']['path'], '--exact', 'agent_operation_envelope_process_cases', '--include-ignored', '--nocapture', '--test-threads=1'],
            'typescript': ['node', art['typescript_probe']['path']],
            'python': [sys.executable, art['python_probe']['path']],
            'go': [art['go_probe']['path'], '-test.run', '^TestAgentOperationEnvelopeProcessCases$', '-test.v', '-test.count=1'],
            'java': ['java', '-cp', jvm_classpath, 'com.sidiora.layerx.sdk.AgentOperationEnvelopeProbe'],
            'kotlin': ['java', '-cp', jvm_classpath, 'com.sidiora.layerx.sdk.AgentOperationEnvelopeProbeKt'],
            'swift': ['swift', 'test', '--skip-build', '--package-path', str(ROOT / 'platform/sdk/swift'),
                      '--filter', 'AgentOperationEnvelopeTests'],
            'csharp': ['dotnet', 'test', art['csharp_probe']['path'], '--no-build', '--filter',
                       'FullyQualifiedName~' + csharp_class, '--logger', 'console;verbosity=detailed'],
        }[language]
        result = subprocess.run([str(a) for a in argv], env=env, cwd=ROOT, capture_output=True, text=True, timeout=300)
        log = self.d / 'probes' / (language + '-' + run + '.log')
        log.write_text(result.stdout + result.stderr)
        require(result.returncode == 0, language + ' probe failed in ' + phase + '; inspect private probe log')
        reported = CASE_LINE.findall(result.stdout)
        counts = COUNT_LINE.findall(result.stdout)
        require(len(counts) == 1 and int(counts[0]) == len(reported) == len(set(reported)), language + ' probe case accounting')
        require(set(reported) == set(cases), language + ' probe reported ' + str(sorted(set(cases) ^ set(reported))))
        self.record_effects(language, cases, requests)

        self.judge_cases(language, run, phase, cases, requests)
        for case in cases:
            self.passed(case if phase != 'read' and not prefix else 'sdk_' + language + '_' + case
                        + ('_post_restart' if prefix and phase == 'post-restart' and case in PYTHON_PRE_RESTART else ''), log)

    def w20_completed_mutation(self, case, operation, request):
        key = self.key('direct', case)
        value, status, body = self.call(case, operation, request, idempotency=key)
        self.effect('direct', case, operation, key)
        return self.success(status, body, value['request_id'], case)

    def w20_subscription_record(self, value, case, paused=None):
        require(isinstance(value, dict) and set(value) == set(W20_SUBSCRIPTION_RECORD), case + ': SubscriptionRecord fields')
        require(isinstance(value['subscription_id'], str) and value['subscription_id'], case + ': subscription_id')
        require(isinstance(value['paused'], bool) and (paused is None or value['paused'] is paused), case + ': paused')
        require(isinstance(value['start'], str) and re.fullmatch('(0|[1-9][0-9]{0,19})', value['start']), case + ': start')
        require(value['last_acknowledged'] is None or (isinstance(value['last_acknowledged'], str)
                and re.fullmatch('(0|[1-9][0-9]{0,19})', value['last_acknowledged'])), case + ': last_acknowledged')
        return value

    def w7_session_list(self):
        case = 'session_list_provisioned'
        row = self.config['requests']['operation.session.list']
        value, status, body = self.call(case, row['operation'], row['request'])
        listed = self.success(status, body, value['request_id'], case)['value']
        require(isinstance(listed, dict) and set(listed) == {'sessions'} and isinstance(listed['sessions'], list),
                case + ': response is not {sessions: [...]}')
        fields = {'session_id', 'token_id', 'agent_did', 'generation', 'open', 'expiry_sequence', 'sequence'}
        previous = None
        for record in listed['sessions']:
            require(isinstance(record, dict) and set(record) == fields, case + ': session record fields')
            require(isinstance(record['session_id'], str) and re.fullmatch('[0-9a-f]{64}', record['session_id'])
                    and isinstance(record['token_id'], str) and re.fullmatch('[0-9a-f]{64}', record['token_id']),
                    case + ': session_id/token_id encoding')
            require(isinstance(record['agent_did'], str) and record['agent_did'], case + ': agent_did')
            require(isinstance(record['open'], bool), case + ': open')
            require(all(isinstance(record[k], str) and re.fullmatch('(0|[1-9][0-9]{0,19})', record[k]) and int(record[k]) < 2**64
                        for k in ('generation', 'expiry_sequence', 'sequence')), case + ': canonical u64 decimals')
            require(previous is None or previous < record['session_id'], case + ': sessions not strictly ordered by session_id')
            previous = record['session_id']
        own = [s for s in listed['sessions'] if s['session_id'] == self.credential['session_id']]
        require(len(own) == 1, case + ': provisioned session not listed exactly once')
        require(own[0]['open'] is True and own[0]['token_id'] == self.credential['token_id']
                and own[0]['generation'] == self.credential['generation']
                and own[0]['agent_did'] == row['request']['context']['agent_did'],
                case + ': provisioned session record does not match the provisioned credential')
        closed = load_private(self.config['requests']['session_close_revocation']['credential_file'], 'session_close_revocation credential_file')
        require(all(s['open'] is False for s in listed['sessions'] if s['session_id'] == closed['session_id']),
                case + ': closed session still listed open')
        self.passed(case, self.d / 'responses' / (case + '.http'))

    def w7_availability_class(self, item, case):
        require(isinstance(item, dict) and set(item) == {'class', 'complete', 'verified_chunks', 'verified_bytes', 'failure'}
                and item['class'] in W7_AVAILABILITY_CLASSES and isinstance(item['complete'], bool)
                and all(isinstance(item[k], str) and re.fullmatch('(0|[1-9][0-9]{0,19})', item[k]) for k in ('verified_chunks', 'verified_bytes'))
                and (item['failure'] is None or (isinstance(item['failure'], str) and item['failure'])), case + ': class record')
        return item

    def w7_availability_fetch(self):
        case = 'availability_fetch_provisioned'
        row = self.config['requests']['operation.availability.fetch']
        request = dict(row['request'], deadline=str(int(time.time()) + W7_AVAILABILITY_SECONDS))
        value, status, body = self.call(case, row['operation'], request)
        envelope = self.success(status, body, value['request_id'], case)
        fetched = envelope['value']
        require(isinstance(fetched, dict) and set(fetched) == {'completion', 'classes', 'providers'}, case + ': availability fields')
        completion = fetched['completion']
        require(isinstance(completion, dict) and completion == {'state': 'complete', 'provider': completion.get('provider')}
                and isinstance(completion['provider'], str) and completion['provider'], case + ': provisioned batch not completely retrieved')
        require(envelope['verification_status'] == {'state': 'achieved', 'level': 'BatchIncluded'},
                case + ': complete retrieval not verified against the sequencer-signed batch header')
        require(isinstance(fetched['classes'], list) and fetched['classes'], case + ': classes')
        classes = [self.w7_availability_class(item, case) for item in fetched['classes']]
        require(len({item['class'] for item in classes}) == len(classes), case + ': class reported twice')
        for item in classes:
            require((item['complete'] and item['failure'] is None) or (not item['complete'] and item['failure'] == 'missing_class'),
                    case + ': class completion and failure disagree')
        obtained = [item for item in classes if item['complete']]
        require(obtained and all(int(item['verified_chunks']) > 0 and int(item['verified_bytes']) > 0 for item in obtained),
                case + ': no verified chunk retrieved')
        require(sum(int(item['verified_bytes']) for item in classes) <= int(request['maximum_bytes'])
                and sum(int(item['verified_chunks']) for item in classes) <= int(request['maximum_chunks']),
                case + ': retrieval exceeded the requested limits')
        providers = fetched['providers']
        require(isinstance(providers, list) and len(providers) == 1 and isinstance(providers[0], dict)
                and set(providers[0]) == {'provider', 'classes', 'failure'} and providers[0]['provider'] == completion['provider']
                and providers[0]['failure'] is None and isinstance(providers[0]['classes'], list)
                and [self.w7_availability_class(item, case) for item in providers[0]['classes']] == classes,
                case + ': complete provider record')
        self.passed(case, self.d / 'responses' / (case + '.http'))

    def w20_history_cursor(self):
        case = 'history_cursor_round_trip'
        row = self.config['requests']['operation.read.history']
        first = dict(row['request'], cursor=None)
        value, status, body = self.call(case + '.first', row['operation'], first)
        page = self.success(status, body, value['request_id'], case + '.first')['value']
        require(isinstance(page, dict) and 'cursor' in page, case + ': history page lacks cursor')
        cursor = page['cursor']
        require(isinstance(cursor, str) and len(cursor) == 112 and re.fullmatch('[0-9a-f]{112}', cursor),
                case + ': cursor is not 112 lowercase hex')
        value, status, body = self.call(case + '.next', row['operation'], dict(row['request'], cursor=cursor))
        page = self.success(status, body, value['request_id'], case + '.next')['value']
        require(isinstance(page, dict) and 'cursor' in page and (page['cursor'] is None or (
                isinstance(page['cursor'], str) and len(page['cursor']) == 112 and re.fullmatch('[0-9a-f]{112}', page['cursor']))),
                case + ': continued cursor is not 112 lowercase hex or null')
        self.passed(case, self.d / 'responses' / (case + '.next.http'))

    def w7_signer(self, case, authority):
        try:
            fd = os.open(self.config['signer_key_file'], os.O_RDONLY | os.O_NOFOLLOW)
        except OSError:
            fd = None
        require(fd is not None, case + ': signer_key_file is absent or unreadable')
        with os.fdopen(fd, 'rb') as stream:
            info = os.fstat(stream.fileno())
            require(stat.S_ISREG(info.st_mode) and info.st_uid == os.geteuid() and not info.st_mode & 0o077,
                    case + ': signer_key_file must be a private regular file')
            seed = stream.read(33)
        require(len(seed) == 32, case + ': signer_key_file is not a 32-byte Ed25519 seed')
        signer = Ed25519PrivateKey.from_private_bytes(seed)
        public = signer.public_key().public_bytes(Encoding.Raw, PublicFormat.Raw).hex()
        require(isinstance(authority, str) and authority.removeprefix('did:layerx:') == public,
                case + ': signer key does not match the provisioned Owner authority')
        return signer, public

    def w7_submit_foreign_key_refused(self, prepared, owner):
        case = W7_SIGN_NEGATIVE[0]
        foreign = Ed25519PrivateKey.generate()
        public = foreign.public_key().public_bytes(Encoding.Raw, PublicFormat.Raw).hex()
        require(public != owner, case + ': generated key equals the provisioned Owner key')
        signature = foreign.sign(bytes.fromhex(prepared['signing_preimage'])).hex()
        key = self.key('direct', case)
        value, status, body = self.call(case, 'submit', {
            'preparation_ref': prepared['preparation_ref'], 'signature': signature, 'signer_public_key': public,
            'approval_release_ref': None}, idempotency=key)
        self.effect('direct', case, 'submit', key)
        self.refusal(status, body, 403, 'PolicyRefusal', 'owner.refused', case, value['request_id'])
        self.passed(case, self.d / 'responses' / (case + '.http'))

    def w7_prepare_sign(self, case, refuse_foreign=False):
        row = self.config['requests'][case]['prepare']
        prepared = self.w20_completed_mutation(case + '.prepare', row['operation'], row['request'])['value']
        require(isinstance(prepared, dict) and isinstance(prepared.get('preparation_ref'), str) and prepared['preparation_ref']
                and isinstance(prepared.get('signing_preimage'), str) and re.fullmatch('[0-9a-f]{64}', prepared['signing_preimage']),
                case + '.prepare: preparation_ref and signing_preimage')
        signer, public = self.w7_signer(case, prepared.get('authority'))
        if refuse_foreign:
            self.w7_submit_foreign_key_refused(prepared, public)
        signature = signer.sign(bytes.fromhex(prepared['signing_preimage'])).hex()
        signed = self.w20_completed_mutation(case + '.sign', 'sign',
                                             {'preparation_ref': prepared['preparation_ref'], 'signature': signature})
        value = signed['value']
        require(isinstance(value, dict) and set(value) == {'activity_id', 'submission', 'receipt'}
                and isinstance(value['activity_id'], str) and re.fullmatch('[0-9a-f]{64}', value['activity_id'])
                and value['activity_id'] != '0' * 64 and isinstance(value['submission'], dict)
                and value['submission'].get('state') == 'Signed' and value['receipt'] is None,
                case + '.sign: retained Signed observation without receipt')
        vs = signed['verification_status']
        require((vs['level'] if vs['state'] == 'achieved' else vs['achieved']) == 'Unverified', case + '.sign: not Unverified')
        return prepared, signature, public, value

    def w7_sign_cases(self):
        case = W7_SIGN_DIRECT[0]
        self.w7_prepare_sign(case, refuse_foreign=True)
        self.passed(case, self.d / 'responses' / (case + '.sign.http'))
        case = W7_SIGN_DIRECT[1]
        prepared, signature, public, signed = self.w7_prepare_sign(case)
        submitted = self.w20_completed_mutation(case + '.submit', 'submit', {
            'preparation_ref': prepared['preparation_ref'], 'signature': signature, 'signer_public_key': public,
            'approval_release_ref': None})['value']
        require(isinstance(submitted, dict) and submitted.get('activity_id') == signed['activity_id']
                and isinstance(submitted.get('submission'), dict) and submitted['submission'].get('state') in W7_SUBMITTED_STATES,
                case + '.submit: submit did not carry the signed activity forward')
        self.passed(case, self.d / 'responses' / (case + '.submit.http'))

    def w20_subscription_lifecycle(self):
        case = 'subscription_lifecycle'
        create = self.config['requests']['operation.subscription.create']['request']
        scope = create['scope']
        record = self.w20_subscription_record(
            self.w20_completed_mutation(case + '.create', 'subscription.create', create)['value'], case + '.create', False)
        target = {'scope': scope, 'subscription_id': record['subscription_id']}
        value, status, body = self.call(case + '.list', 'subscription.list', {'scope': scope})
        listed = self.success(status, body, value['request_id'], case + '.list')['value']
        require(isinstance(listed, list), case + '.list: response is not a JSON array')
        for row in listed:
            self.w20_subscription_record(row, case + '.list')
        require([row for row in listed if row['subscription_id'] == record['subscription_id']] == [record],
                case + '.list: created record not listed exactly once')
        value, status, body = self.call(case + '.health', 'subscription.health', target)
        health = self.success(status, body, value['request_id'], case + '.health')['value']
        require(isinstance(health, dict) and set(health) == {'target', 'last_acknowledged', 'last_delivery_at', 'pending_backfill'}
                and health['target'] == target, case + '.health: health fields')
        require(health['last_delivery_at'] is None or (isinstance(health['last_delivery_at'], str)
                and re.fullmatch('(0|[1-9][0-9]{0,19})', health['last_delivery_at'])), case + '.health: last_delivery_at')
        gap = health['pending_backfill']
        require(gap is None or (isinstance(gap, dict) and set(gap) == {'missing_first', 'missing_last', 'backfill_cursor', 'backfill_attempted'}
                and all(isinstance(gap[k], str) and re.fullmatch('(0|[1-9][0-9]{0,19})', gap[k])
                        for k in ('missing_first', 'missing_last', 'backfill_cursor'))
                and isinstance(gap['backfill_attempted'], bool)), case + '.health: pending_backfill GapNotice')
        acknowledged = self.w20_subscription_record(self.w20_completed_mutation(
            case + '.acknowledge', 'subscription.acknowledge', dict(target, cursor=record['start']))['value'], case + '.acknowledge')
        require(acknowledged['subscription_id'] == record['subscription_id'] and acknowledged['last_acknowledged'] == record['start'],
                case + '.acknowledge: cursor not recorded')
        paused = self.w20_subscription_record(
            self.w20_completed_mutation(case + '.pause', 'subscription.pause', target)['value'], case + '.pause', True)
        resumed = self.w20_subscription_record(
            self.w20_completed_mutation(case + '.resume', 'subscription.resume', target)['value'], case + '.resume', False)
        require(paused['subscription_id'] == resumed['subscription_id'] == record['subscription_id'], case + ': record identity changed')
        deleted = self.w20_completed_mutation(case + '.delete', 'subscription.delete', target)
        require(deleted['value'] is None, case + '.delete: delete did not return null')
        value, status, body = self.call(case + '.list_after_delete', 'subscription.list', {'scope': scope})
        listed = self.success(status, body, value['request_id'], case + '.list_after_delete')['value']
        require(isinstance(listed, list) and all(row.get('subscription_id') != record['subscription_id'] for row in listed),
                case + ': deleted subscription still listed')
        self.passed(case, self.d / 'responses' / (case + '.delete.http'))

    def w8_capability_authority(self, value, case):
        require(isinstance(value, dict) and set(value) == {'authority', 'value'}, case + ': AuthorityResponse fields')
        authority = value['authority']
        require(isinstance(authority, dict) and set(authority) == {'tenant', 'agent_did', 'authority_ref', 'protocol_authority'}
                and all(isinstance(authority[k], str) and authority[k] for k in authority)
                and re.fullmatch('([0-9a-f]{2})+', authority['protocol_authority']), case + ': authority description')
        create = self.config['requests']['operation.capability.create']['request']
        require(authority['tenant'] == create['tenant'] and authority['agent_did'] == create['agent_did'],
                case + ': authority names another tenant or agent')
        return value['value']

    def w8_dimensions_equal(self, echoed, requested):
        if not isinstance(echoed, dict) or set(echoed) != set(W8_DIMENSIONS):
            return False
        sets = ('activity_types', 'counterparties', 'assets', 'purpose_constraints')
        return (all(sorted(echoed[k]) == sorted(requested[k]) for k in sets)
                and sorted((c['asset'], int(c['amount'])) for c in echoed['amount_ceilings'])
                == sorted((c['asset'], int(c['amount'])) for c in requested['amount_ceilings'])
                and sorted((int(r['window_seconds']), int(r['maximum_actions'])) for r in echoed['rate_ceilings'])
                == sorted((int(r['window_seconds']), int(r['maximum_actions'])) for r in requested['rate_ceilings'])
                and echoed['expiry'] == requested['expiry'])

    def w8_capability_record(self, value, case, state, parent_id=None, dimensions=None):
        require(isinstance(value, dict) and set(value) == set(W8_CAPABILITY_RECORD), case + ': CapabilityRecord fields')
        require(isinstance(value['capability_id'], str) and re.fullmatch('[0-9a-f]{64}', value['capability_id']),
                case + ': capability_id is not 64 lowercase hex')
        require(value['parent_id'] == parent_id, case + ': parent_id')
        require(value['state'] == state, case + ': state ' + str(value['state']) + ' != ' + state)
        require(all(isinstance(value[k], str) and re.fullmatch('(0|[1-9][0-9]{0,19})', value[k])
                    for k in ('created_at_ms', 'created_at_sequence')), case + ': created_at decimals')
        revoked = [value[k] for k in ('revoked_at_ms', 'revoked_at_sequence')]
        require(all(v is None for v in revoked) if state != 'revoked' else
                all(isinstance(v, str) and re.fullmatch('(0|[1-9][0-9]{0,19})', v) for v in revoked), case + ': revoked_at')
        require(dimensions is None or self.w8_dimensions_equal(value['dimensions'], dimensions), case + ': dimensions not echoed')
        return value

    def w8_capability_list(self, case):
        create = self.config['requests']['operation.capability.create']['request']
        value, status, body = self.call(case, 'capability.list', {'tenant': create['tenant'], 'agent_did': create['agent_did']})
        listed = self.w8_capability_authority(self.success(status, body, value['request_id'], case)['value'], case)
        require(isinstance(listed, dict) and set(listed) == {'capabilities'} and isinstance(listed['capabilities'], list),
                case + ': response is not {capabilities: [...]}')
        ids = [row.get('capability_id') if isinstance(row, dict) else None for row in listed['capabilities']]
        require(all(isinstance(i, str) for i in ids) and all(a < b for a, b in zip(ids, ids[1:])),
                case + ': capabilities not strictly ascending by capability_id')
        return {row['capability_id']: row for row in listed['capabilities']}

    def w8_capability_refusal(self, case, operation, request, http_status, klass, reason):
        key = self.key('direct', case)
        value, status, body = self.call(case, operation, request, idempotency=key)
        self.effect('direct', case, operation, key)
        return self.refusal(status, body, http_status, klass, reason, case, value['request_id'])

    def w8_capability_refused(self, case, operation, request, http_status, klass, reason):
        self.w8_capability_refusal(case, operation, request, http_status, klass, reason)
        self.passed(case, self.d / 'responses' / (case + '.http'))

    def w8_capability_cases(self):
        create = self.config['requests']['operation.capability.create']['request']
        dims = create['dimensions']
        case = W8_CAPABILITY_DIRECT[0]
        parent = self.w8_capability_record(self.w8_capability_authority(
            self.w20_completed_mutation(case, 'capability.create', create)['value'], case), case, 'active', None, dims)
        self.passed(case, self.d / 'responses' / (case + '.http'))

        case = W8_CAPABILITY_DIRECT[1]
        first = dims['amount_ceilings'][0]
        narrow = dict(dims, activity_types=dims['activity_types'][:1], counterparties=dims['counterparties'][:1],
                      assets=[first['asset']], amount_ceilings=[first], purpose_constraints=dims['purpose_constraints'][:1])
        wider = dict(narrow, amount_ceilings=[dict(first, amount=str(int(first['amount']) + 1))])
        self.w8_capability_refusal(case + '.wider', 'capability.attenuate',
                                   {'tenant': create['tenant'], 'agent_did': create['agent_did'],
                                    'parent_id': parent['capability_id'], 'dimensions': wider},
                                   403, 'CapabilityRefusal', 'capability.amount')
        child = self.w8_capability_record(self.w8_capability_authority(self.w20_completed_mutation(
            case, 'capability.attenuate', {'tenant': create['tenant'], 'agent_did': create['agent_did'],
                                           'parent_id': parent['capability_id'], 'dimensions': narrow})['value'], case),
            case, 'active', parent['capability_id'], narrow)
        require(child['capability_id'] != parent['capability_id'], case + ': child reused the parent id')
        self.passed(case, self.d / 'responses' / (case + '.http'))

        case = W8_CAPABILITY_DIRECT[2]
        listed = self.w8_capability_list(case)
        require(listed.get(parent['capability_id']) == parent and listed.get(child['capability_id']) == child,
                case + ': created records not listed as created')
        self.passed(case, self.d / 'responses' / (case + '.http'))

        base = {'tenant': create['tenant'], 'agent_did': create['agent_did']}
        asset = dims['assets'][0]
        self.w8_capability_refused(W8_CAPABILITY_REFUSALS[0], 'capability.create',
                                   dict(base, dimensions=dict(dims, assets=dims['assets'] + [asset])),
                                   403, 'CapabilityRefusal', 'capability.asset')
        window = dims['rate_ceilings'][0]
        self.w8_capability_refused(W8_CAPABILITY_REFUSALS[1], 'capability.create',
                                   dict(base, dimensions=dict(dims, rate_ceilings=[dict(window, window_seconds='0')])),
                                   403, 'CapabilityRefusal', 'capability.rate')
        self.w8_capability_refused(W8_CAPABILITY_REFUSALS[2], 'capability.create',
                                   dict(base, dimensions=dict(dims, assets=[a for a in dims['assets'] if a != first['asset']])),
                                   403, 'CapabilityRefusal', 'capability.amount')
        self.w8_capability_refused(W8_CAPABILITY_REFUSALS[3], 'capability.create', dict(base, dimensions=dict(dims, expiry='1')),
                                   403, 'CapabilityRefusal', 'capability.expiry')
        require(parent['capability_id'].upper() != parent['capability_id'], 'capability_id has no hex letter to uppercase')
        self.w8_capability_refused(W8_CAPABILITY_REFUSALS[4], 'capability.revoke',
                                   dict(base, capability_id=parent['capability_id'].upper()),
                                   403, 'PolicyRefusal', 'owner.refused')

        case = W8_CAPABILITY_DIRECT[3]
        revoked = self.w8_capability_record(self.w8_capability_authority(self.w20_completed_mutation(
            case, 'capability.revoke', dict(base, capability_id=parent['capability_id']))['value'], case), case, 'revoked', None, dims)
        require(revoked['capability_id'] == parent['capability_id'], case + ': revoke returned another record')
        listed = self.w8_capability_list(case + '.list')
        for record, parent_id in ((parent, None), (child, parent['capability_id'])):
            row = listed.get(record['capability_id'])
            require(row is not None, case + ': revoked record no longer listed')
            self.w8_capability_record(row, case + '.list', 'revoked', parent_id)
            require(row['revoked_at_ms'] == revoked['revoked_at_ms'] and row['revoked_at_sequence'] == revoked['revoked_at_sequence'],
                    case + ': subtree not revoked at one instant')
        self.passed(case, self.d / 'responses' / (case + '.list.http'))

    def served_freshness(self, value, case):
        require(isinstance(value, dict) and set(value) == set(FRESHNESS), case + ': freshness fields')
        require(all(isinstance(value[k], str) and re.fullmatch(DECIMAL_U64, value[k]) for k in ('chain_head', 'value_sequence'))
                and all(isinstance(value[k], str) and value[k] for k in ('latest_sealed_batch', 'latest_finalised_checkpoint')),
                case + ': freshness coordinates')
        relative = value['relative_to']
        require(isinstance(relative, dict) and len(relative) == 1 and set(relative) <= {'batch', 'checkpoint'}
                and all(isinstance(v, str) and v for v in relative.values()), case + ': relative_to is not one batch or checkpoint')
        return value

    def served_unverified(self, response, case):
        require(response['verification_status'] == {'state': 'achieved', 'level': 'Unverified'},
                case + ': response claims a verification level')
        return response['value']

    def served_read_refused(self, case, operation, request, http_status, klass, reason):
        value, status, body = self.call(case, operation, request)
        error = self.refusal(status, body, http_status, klass, reason, case, value['request_id'])
        require(error['retriability'] == 'Terminal' and error['protocol_result_code'] is None, case + ': refusal not terminal')
        self.passed(case, self.d / 'responses' / (case + '.http'))

    def served_read_cases(self):
        req = self.config['requests']
        project = req['operation.project']['request']
        case = SERVED_READ_DIRECT[0]
        sent = dict(project, canonical_bytes=str(MAX_BODY))
        value, status, body = self.call(case, 'project', sent)
        result = self.served_unverified(self.success(status, body, value['request_id'], case), case)
        require(isinstance(result, dict) and set(result) == {'projected', 'rationale', 'observed_freshness'},
                case + ': ProjectionResult fields')
        projected = result['projected']
        require(isinstance(projected, dict) and set(projected) == set(PROJECTED), case + ': FeeProjection fields')
        require(projected['request'] == {k: sent[k] for k in PROJECT_REQUEST}, case + ': projected request not echoed exactly')
        require(isinstance(projected['parameter_version'], str) and re.fullmatch('[1-9][0-9]{0,9}', projected['parameter_version'])
                and int(projected['parameter_version']) < 2**32, case + ': parameter_version')
        require(isinstance(projected['fee'], str) and re.fullmatch(W8_DECIMAL, projected['fee']) and int(projected['fee']) < 2**128,
                case + ': fee')
        require(isinstance(projected['canonical_schedule'], str) and re.fullmatch('([0-9a-f]{2})+', projected['canonical_schedule']),
                case + ': canonical_schedule')
        require(isinstance(projected['snapshot_state_root'], str) and re.fullmatch(NONZERO_HEX32, projected['snapshot_state_root']),
                case + ': snapshot_state_root')
        require(isinstance(result['rationale'], str) and result['rationale'], case + ': rationale')
        freshness = self.served_freshness(result['observed_freshness'], case)
        require(projected['snapshot_sequence'] == freshness['chain_head'] == freshness['value_sequence'],
                case + ': snapshot_sequence differs from the observed head')
        self.passed(case, self.d / 'responses' / (case + '.http'))
        self.served_read_refused(SERVED_READ_REFUSALS[0], 'project', dict(project, canonical_bytes=str(MAX_BODY + 1)),
                                 400, 'ProtocolIncompatibility', 'envelope.malformed')
        self.served_read_refused(SERVED_READ_REFUSALS[1], 'project',
                                 {'context': req['operation.session.list']['request']['context'], 'canonical_intent': '00' * 32},
                                 403, 'PolicyRefusal', 'policy.legacy_project_payload')

        dry = req['operation.policy.dry_run']['request']
        case = SERVED_READ_DIRECT[1]
        value, status, body = self.call(case, 'policy.dry_run', dry)
        result = self.served_unverified(self.success(status, body, value['request_id'], case), case)
        require(isinstance(result, dict) and set(result) == set(POLICY_DRY_RUN_RESULT), case + ': dry-run result fields')
        require(result['mode'] == 'dry_run' and result['outcome'] in ('allow', 'deny') and result['reason'] in POLICY_REASONS,
                case + ': mode, outcome or reason')
        require(isinstance(result['policy_version'], str) and result['policy_version']
                and isinstance(result['authority_statement'], str) and result['authority_statement'],
                case + ': policy_version or authority_statement')
        require(isinstance(result['matched_rules'], list) and all(isinstance(r, str) and r for r in result['matched_rules'])
                and (result['deciding_rule'] is None or isinstance(result['deciding_rule'], str) and result['deciding_rule']),
                case + ': matched_rules or deciding_rule')
        self.passed(case, self.d / 'responses' / (case + '.http'))
        unknown = hashlib.sha256(self.key_nonce + b'policy.dry_run.unknown_capability').hexdigest()
        require(unknown != dry['capability_id'], 'derived capability id equals the provisioned one')
        self.served_read_refused(SERVED_READ_REFUSALS[2], 'policy.dry_run', dict(dry, capability_id=unknown),
                                 403, 'PolicyRefusal', 'policy.unknown_capability')

        export = req['operation.export.offline']['request']
        case = SERVED_READ_DIRECT[2]
        value, status, body = self.call(case, 'export.offline', export)
        response = self.success(status, body, value['request_id'], case)
        result = response['value']
        require(isinstance(result, dict) and set(result) == {'value', 'achieved_verification_level', 'freshness'},
                case + ': VerifiedRead fields')
        level = result['achieved_verification_level']
        require(level in LEVELS and LEVELS.index(level) >= LEVELS.index(export['requested_verification_level']),
                case + ': achieved level below the requested level')
        require(response['verification_status'] == {'state': 'achieved', 'level': level}, case + ': envelope level differs')
        offline = result['value']
        require(isinstance(offline, dict) and set(offline) == {'facts', *EXPORT_BUCKETS}, case + ': OfflineExport fields')
        require(offline['facts'] == export['fact_set'], case + ': facts are not exactly the requested references in order')
        require(all(isinstance(offline[b], list) and all(isinstance(r, str) and re.fullmatch('([0-9a-f]{2})+', r) for r in offline[b])
                    for b in EXPORT_BUCKETS), case + ': record buckets are not lowercase hex')
        self.served_freshness(result['freshness'], case)
        require(len(body) <= MAX_BODY, case + ': export exceeds the transport body bound')
        self.passed(case, self.d / 'responses' / (case + '.http'))
        fact = export['fact_set'][0]
        seventeen = ['receipt:' + hashlib.sha256(self.key_nonce + bytes([i])).hexdigest() for i in range(MAX_FACTS + 1)]
        require(len(set(seventeen)) == MAX_FACTS + 1 and all(re.fullmatch(FACT_REF, f) for f in seventeen),
                'derived fact references are not distinct strict references')
        self.served_read_refused(SERVED_READ_REFUSALS[3], 'export.offline', dict(export, fact_set=seventeen),
                                 400, 'ProtocolIncompatibility', 'envelope.malformed')
        self.served_read_refused(SERVED_READ_REFUSALS[4], 'export.offline', dict(export, fact_set=[fact, fact]),
                                 400, 'ProtocolIncompatibility', 'envelope.malformed')
        kind, _, rest = fact.partition(':')
        require(rest.upper() != rest, 'provisioned fact reference has no hex letter to uppercase')
        self.served_read_refused(SERVED_READ_REFUSALS[5], 'export.offline', dict(export, fact_set=[kind + ':' + rest.upper()]),
                                 400, 'ProtocolIncompatibility', 'envelope.malformed')
        self.served_read_refused(SERVED_READ_REFUSALS[6], 'export.offline',
                                 dict(export, requested_verification_level='SettlementAnchored'),
                                 503, 'UnavailableCapability', 'export.settlement_anchoring_unavailable')

        discover = req['operation.program.discover']['request']
        case = SERVED_READ_DIRECT[3]
        value, status, body = self.call(case, 'program.discover', discover)
        response = self.success(status, body, value['request_id'], case)
        vs = response['verification_status']
        require((vs['level'] if vs['state'] == 'achieved' else vs['achieved']) == 'Unverified', case + ': discovery claims a level')
        result = response['value']
        require(isinstance(result, dict) and set(result) == set(DISCOVERY), case + ': discovery fields')
        require(result['program_id'] == discover['program_id'], case + ': discovery names another program')
        require(result['lifecycle'] in ('active', 'deprecated', 'tombstoned')
                and result['verification'] == 'registry-receipt-and-current-head-verified', case + ': lifecycle or verification')
        require(all(isinstance(result[k], int) and not isinstance(result[k], bool) for k in ('version', 'abi_version'))
                and 0 <= result['version'] < 2**32 and 0 <= result['abi_version'] < 2**16, case + ': version or abi_version')
        require(all(isinstance(result[k], str) and re.fullmatch('[0-9a-f]{64}', result[k])
                    for k in ('code_hash', 'receipt_digest', 'state_root')), case + ': digests')
        require(all(isinstance(result[k], str) and re.fullmatch(DECIMAL_U64, result[k]) and int(result[k]) < 2**64
                    for k in ('observed_sequence', 'observed_at', 'valid_through'))
                and int(result['observed_at']) <= int(result['valid_through']), case + ': observation window')
        self.passed(case, self.d / 'responses' / (case + '.http'))

        case = SERVED_READ_REFUSALS[7]
        prepare = req[W7_SIGN_DIRECT[0]]['prepare']['request']
        bound = hashlib.sha256(self.key_nonce + b'prepare.capability_id').hexdigest()
        require(bound.upper() != bound, case + ': derived capability id has no hex letter to uppercase')
        self.w8_capability_refused(case, 'prepare', dict(prepare, capability_id=bound.upper()),
                                   400, 'ProtocolIncompatibility', 'envelope.malformed')

    def run(self):
        (self.d / 'responses').mkdir(mode=0o700)
        (self.d / 'probes').mkdir(mode=0o700)
        self.start_agentd()
        self.bindings()
        self.start_gateway()
        self.health_cases()
        self.direct_cases()
        self.idempotency_cases()
        self.mtls_cases()
        self.decode_observed = []
        for language in LANGUAGES:
            self.read_cases(language)
        state = self.d / 'probes/rust-mutation.state'
        operation_cases = tuple('operation.' + name for name in self.built['operations'])
        self.probe('rust', RUST_PRE_RESTART + operation_cases, 'pre-restart', state)

        self.w20_wait()

        self.w20_revocation()

        self.w20_idempotency_pre()

        self.w20_rotation()

        self.w20_history_cursor()

        self.w7_session_list()
        self.w7_availability_fetch()
        self.w20_subscription_lifecycle()
        self.w8_capability_cases()
        self.served_read_cases()

        self.w7_sign_cases()
        python_state = self.d / 'probes/python-mutation.state'
        python_retry = self.d / 'probes/python-retry.state'
        python_requests = self.probe_requests('python')
        self.probe('python', PYTHON_PRE_RESTART, 'pre-restart', python_state, python_requests, python_retry, True)
        old = self.agentd.pid
        self.agentd.kill()
        self.agentd.wait(timeout=15)
        self.start_agentd()
        require(self.agentd.pid != old, 'agentd restart reused the process')
        self.probe('rust', RUST_POST_RESTART, 'post-restart', state)

        self.w20_expiry()

        self.w20_idempotency_post()
        self.probe('python', PYTHON_POST_RESTART, 'post-restart', python_state, python_requests, python_retry, True)
        keys = [row['idempotency_key'] for row in self.effects]
        require(len(keys) == len(set(keys)), 'idempotency keys collide across languages or cases')
        write_private(self.d / 'effect-manifest.json', {
            'key_derivation': 'sha256(run_nonce || language || NUL || case_id)', 'effects': self.effects,
            'effect_count': len(self.effects), 'decode_requests': self.decode_observed})

        write_private(self.d / 'operation-evidence.json', {'rows': self.operation_evidence_rows})


def worker(directory):
    built = load_manifest()
    config, credential = load_config()
    bundle = fixture.artifacts(os.environ.get('PAXEER_X_RUNTIME_ARTIFACTS'))
    client = fixture.client_artifact(os.environ.get('PAXEER_X_RUNTIME_CLIENT_MANIFEST'))
    for name in ('net', 'pid', 'mnt'):
        require(os.readlink('/proc/self/ns/' + name) != os.environ['PAXEER_X_PARENT_' + name.upper()], 'namespace isolation')
    subprocess.run(['ip', 'link', 'set', 'lo', 'up'], check=True)
    runtime = fixture.RuntimeFixture(directory, bundle, client)
    qualification = None
    try:
        runtime.generate()
        runtime.start()
        runtime.readiness()
        qualification = Qualification(runtime.directory, built, config, credential, runtime)
        qualification.run()
        expected = (len(DIRECT_CASES) + len(LANGUAGES) * len(LANGUAGE_CASES) + len(RUST_PRE_RESTART)
                    + len(built['operations']) + len(RUST_POST_RESTART)
                    + len(PYTHON_PRE_RESTART) + len(PYTHON_POST_RESTART) + len(LANGUAGES) * len(DECODE_CASES))

        expected += len(W20_WAIT_PRE)

        expected += len(W20_EXPIRY_POST)

        expected += len(W20_REVOCATION_PRE)

        expected += len(W20_IDEMPOTENCY_PRE) + len(W20_IDEMPOTENCY_POST)

        expected += len(W20_ROTATION_PRE) + len(W7_OWNER_DIRECT)

        expected += len(W20_COMPLETED_DIRECT) + len(W7_SIGN_DIRECT) + len(W7_SIGN_NEGATIVE)

        expected += len(W8_CAPABILITY_DIRECT) + len(W8_CAPABILITY_REFUSALS)
        expected += len(SERVED_READ_DIRECT) + len(SERVED_READ_REFUSALS)
        require(len(qualification.results) == expected, 'case count ' + str(len(qualification.results)) + ' != ' + str(expected))
        write_private(runtime.directory / 'case-results.json', qualification.results)
        print(f'PAXEER_X_GATE tests={len(qualification.results)} skipped=0', flush=True)
    finally:
        if qualification is not None:
            qualification.stop()
        runtime.cleanup()


def main():
    os.umask(0o077)
    if len(sys.argv) == 4 and sys.argv[1] == '--build' and sys.argv[2] == '--manifest':
        return build(sys.argv[3])
    if len(sys.argv) == 3 and sys.argv[1] == '--worker':
        worker(sys.argv[2])
        return 0
    require(len(sys.argv) == 1, 'usage: agent_operation_envelope.py [--build --manifest PATH]')
    require(os.geteuid() == 0, 'disposable runtime requires root for distinct peer credentials')
    load_manifest()
    load_config()
    for name in ('openssl', 'unshare', 'ip', 'node', 'java', 'swift', 'dotnet'):
        require(shutil.which(name), 'required tool absent: ' + name)
    evidence = private_dir(os.environ.get('PAXEER_X_EVIDENCE_DIR'), 'PAXEER_X_EVIDENCE_DIR')
    revision = capture(['git', 'rev-parse', 'HEAD'])
    directory = Path(tempfile.mkdtemp(prefix='px-agent-envelope-', dir='/var/tmp'))
    directory.rmdir()
    command = 'timeout 30m python3 tools/qualification/paxeer-x/agent_operation_envelope.py'
    started = time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime())
    env = dict(os.environ)
    for name in ('net', 'pid', 'mnt'):
        env['PAXEER_X_PARENT_' + name.upper()] = os.readlink('/proc/self/ns/' + name)
    log_path = evidence / 'worker.log'
    with log_path.open('wb') as log:
        result = subprocess.run(['unshare', '--mount', '--net', '--pid', '--fork', '--kill-child=KILL', '--mount-proc',
                                 '--propagation', 'private', sys.executable, str(Path(__file__).resolve()), '--worker', str(directory)],
                                env=env, stdout=log, stderr=log, timeout=1680)
    text = log_path.read_text()
    markers = re.findall(r'^PAXEER_X_GATE tests=(\d+) skipped=0$', text, re.M)
    progress = re.findall(r'^PAXEER_X_PROGRESS cases=(\d+)', text, re.M)
    count = int(markers[0]) if len(markers) == 1 else int(progress[-1]) if progress else 0
    write_private(evidence / 'result.json', {
        'candidate_revision': revision, 'command': command, 'started_utc': started,
        'finished_utc': time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime()), 'exit_code': result.returncode,
        'cases_passed': count, 'gate_marker': len(markers) == 1, 'worker_log': str(log_path),
        'runtime_directory': str(directory), 'case_results': str(directory / 'case-results.json'),
        'effect_manifest': str(directory / 'effect-manifest.json')})
    print(f'PAXEER_X_GATE tests={count} skipped=0' if len(markers) == 1 else
          f'PAXEER_X_GATE tests={count} skipped=0 incomplete=1', flush=True)
    require(result.returncode == 0 and len(markers) == 1 and count > 0, 'worker failed; inspect private worker.log')
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
