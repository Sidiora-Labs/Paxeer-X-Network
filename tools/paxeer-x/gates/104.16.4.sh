#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/../../.."
if [[ ${1:-} == build && $# == 1 ]]; then
  npm run build --workspace @sidiora/layerx-sdk --workspace @sidiora/layerx-buyer-middleware --workspace @sidiora/layerx-seller-middleware --workspace @sidiora/layerx-agent-middleware --workspace @sidiora/layerx-next --workspace @sidiora/layerx-agent-integrations
  swift build --disable-automatic-resolution --package-path platform/integrations/ios
  mvn -o -q -f platform/sdk/jvm/pom.xml -DskipTests install
  mvn -o -q -f platform/integrations/android/pom.xml -Psample -DskipTests package
  exit 0
fi
if (($#)); then
  printf 'usage: %s [build]\n' "$0" >&2
  exit 2
fi
exec python3 - <<'PY'
import hashlib
import json
import os
from pathlib import Path
import re
import signal
import stat
import subprocess
import tempfile
import urllib.parse

ROOT = Path.cwd()
COUNT = 0
CHILDREN = []
FRAMEWORKS = ('mcp', 'a2a', 'openai', 'anthropic', 'langchain', 'vercel-ai')

def require(value, reason):
    if not value:
        raise RuntimeError(reason)

def admitted_path(path):
    require(not any(part == '.env' or part.startswith('.env.') for part in path.parts),
            'credential path refused')
    return path

def digest(path):
    admitted_path(path)
    h = hashlib.sha256()
    with path.open('rb') as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b''):
            h.update(chunk)
    return h.hexdigest()

def private_json(raw):
    path = admitted_path(Path(raw))
    require(path.is_absolute() and not path.is_symlink(), 'protected absolute input required')
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
    with os.fdopen(fd, 'rb') as stream:
        info = os.fstat(stream.fileno())
        require(stat.S_ISREG(info.st_mode) and info.st_uid == os.geteuid()
                and info.st_nlink == 1 and info.st_mode & 0o077 == 0 and info.st_size <= 16 * 1024 * 1024,
                'protected bounded owner input required')
        return json.load(stream)

def artifact(value, revision_bound=True):
    require(isinstance(value, dict), 'artifact descriptor required')
    path = admitted_path(Path(value['path']))
    require(path.is_absolute() and path.is_file() and not path.is_symlink(), 'regular artifact required')
    require(digest(path) == value['sha256'], 'artifact digest mismatch')
    if revision_bound:
        require(value['source_revision'] == REVISION, 'artifact source revision mismatch')
    return str(path)

def directory_artifact(value):
    path = admitted_path(Path(value['path']))
    require(path.is_absolute() and path.is_dir() and not path.is_symlink(), 'compiled directory required')
    require(value['source_revision'] == REVISION, 'compiled directory source revision mismatch')
    files = value['files']
    require(isinstance(files, dict) and files, 'complete compiled directory manifest required')
    actual = set()
    for leaf in path.rglob('*'):
        require(not leaf.is_symlink(), 'compiled artifact symlinks refused')
        if leaf.is_file():
            relative = str(leaf.relative_to(path))
            require(not leaf.name.startswith('.env'), 'credential path refused')
            actual.add(relative)
            require(files.get(relative) == digest(leaf), 'compiled artifact digest mismatch')
    require(actual == set(files), 'compiled artifact closure mismatch')
    return str(path)

def environment(raw):
    supplied = private_json(raw)
    require(isinstance(supplied, dict) and all(isinstance(k, str) and isinstance(v, str)
            and '\0' not in k + v for k, v in supplied.items()), 'typed process environment required')
    for name, value in supplied.items():
        if name.endswith('_URL') and value:
            endpoint = urllib.parse.urlsplit(value)
            require(endpoint.scheme in ('http', 'https') and endpoint.hostname in ('localhost', '127.0.0.1', '::1')
                    and endpoint.username is None and endpoint.password is None,
                    'funded integration endpoints must be isolated loopback services')
    clean = {name: value for name, value in os.environ.items()
             if not name.startswith(('LAYERX_', 'ATTESTOR_', 'CEREMONY_', 'PAX_', 'DATABASE_', 'PG'))}
    clean.update(supplied)
    return clean

def run(argv, env, label, timeout=120):
    global COUNT
    output = STATE / (label + '.log')
    fd = os.open(output, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, 'wb') as stream:
        child = subprocess.Popen(argv, cwd=ROOT, env=env, stdin=subprocess.DEVNULL,
                                 stdout=stream, stderr=subprocess.STDOUT, start_new_session=True)
        CHILDREN.append(child)
        try:
            code = child.wait(timeout=timeout)
        except subprocess.TimeoutExpired:
            os.killpg(child.pid, signal.SIGKILL)
            child.wait()
            raise RuntimeError('bounded process timeout: ' + label) from None
    require(code == 0, 'actual integration refused: ' + label)
    COUNT += 1
    return output.read_text()

def last_json(output):
    lines = output.splitlines()
    require(lines, 'actual integration emitted no report')
    for line in reversed(lines):
        try:
            value = json.loads(line)
        except ValueError:
            continue
        if isinstance(value, dict):
            return value
    raise RuntimeError('actual integration emitted no typed report')

def source_binding(fixture):
    require(fixture['source_revision'] == REVISION, 'fixture source revision mismatch')
    manifest = private_json(fixture['source_manifest'])
    prefixes = ('platform/integrations/ios/', 'platform/integrations/android/', 'platform/integrations/agents/',
                'platform/integrations/next/', 'platform/sdk/swift/', 'platform/sdk/jvm/',
                'platform/middleware/agent/', 'platform/middleware/buyer/', 'platform/middleware/seller/',
                'agent/sdk/typescript/')
    tracked = subprocess.check_output(['git', 'ls-files', '-z', '--', *prefixes,
        'package.json', 'package-lock.json', '.github/workflows/platform.yml',
        'tools/paxeer-x/gates/104.16.4.sh']).split(b'\0')
    required = {item.decode() for item in tracked if item and not any(
                part == '.env' or part.startswith('.env.') for part in Path(item.decode()).parts)}
    require(set(manifest) == required, 'complete integration and SDK source closure required')
    for relative, expected in manifest.items():
        require(digest(ROOT / relative) == expected, 'supplied artifact source closure mismatch')

def main():
    global REVISION, STATE
    raw = os.environ.get('LAYERX_MOBILE_AGENT_INTEGRATION_FIXTURE')
    require(raw, 'prerequisite: protected LAYERX_MOBILE_AGENT_INTEGRATION_FIXTURE required')
    fixture = private_json(raw)
    require(fixture['version'] == 'layerx-mobile-agent-integration-v1'
            and fixture['execution_domain'] == 'isolated-real-process'
            and fixture['authorize_funded_layerx'] is True, 'explicit isolated funded qualification required')
    REVISION = subprocess.check_output(['git', 'rev-parse', 'HEAD'], text=True).strip()
    source_binding(fixture)
    evidence = Path(os.environ['PAXEER_X_EVIDENCE_DIR'])
    require(evidence.is_absolute() and evidence.is_dir() and evidence.stat().st_mode & 0o077 == 0,
            'private evidence directory required')
    STATE = Path(tempfile.mkdtemp(prefix='104.16.4-', dir=evidence))
    STATE.chmod(0o700)
    binaries = fixture['artifacts']
    node = artifact(fixture['toolchains']['node'], False)
    java = artifact(fixture['toolchains']['java'], False)
    ios_sample = artifact(binaries['ios_sample'])
    ios_webhooks = artifact(binaries['ios_webhooks'])
    ios_scan = artifact(binaries['ios_scan'])
    android_classpath = []
    for entry in fixture['android_classpath']:
        android_classpath.append(directory_artifact(entry) if 'files' in entry else artifact(entry, entry.get('first_party', False)))
    require(android_classpath and any(entry.get('source_revision') == REVISION
                for entry in fixture['android_classpath']), 'source-bound compiled Android and SDK classpath required')
    classpath = os.pathsep.join(android_classpath)
    compiled = private_json(fixture['typescript_artifacts'])
    dist_roots = ('agent/sdk/typescript/dist', 'platform/middleware/buyer/dist',
                  'platform/middleware/seller/dist', 'platform/middleware/agent/dist',
                  'platform/integrations/next/dist', 'platform/integrations/agents/dist')
    expected = set()
    for root in dist_roots:
        directory = ROOT / root
        require(directory.is_dir(), 'actual compiled TypeScript dependency missing')
        for path in directory.rglob('*'):
            require(not path.is_symlink(), 'compiled TypeScript symlinks refused')
            if path.is_file():
                relative = str(path.relative_to(ROOT))
                expected.add(relative)
                require(compiled.get(relative) == digest(path), 'compiled TypeScript digest mismatch')
    require(set(compiled) == expected, 'compiled TypeScript closure mismatch')
    ios_env = environment(fixture['ios_environment'])
    android_env = environment(fixture['android_environment'])
    agent_env = environment(fixture['agent_environment'])
    for platform, env in (('ios', ios_env), ('android', android_env)):
        require(env.get('LAYERX_SAMPLE_WEBHOOK_DELIVERY_PATH'), platform + ' genuine signed delivery required')
        env['LAYERX_SAMPLE_DELIVERY_STORE_PATH'] = str(STATE / (platform + '-journey-deliveries.json'))
    ios = last_json(run([ios_sample], ios_env, 'ios-real-journey'))
    require(ios.get('settlement') == 'sequencer-signed'
            and ios.get('receipt_digest') == fixture['ios_receipt_digest'] and ios.get('event') == 'verified'
            and ios.get('event_tamper') == 'rejected' and ios.get('event_replay') == 'duplicate',
            'iOS real receipt and signed webhook evidence missing')
    android = last_json(run([java, '-cp', classpath,
        'com.sidiora.layerx.android.sample.ConsoleSampleMain'], android_env, 'android-real-journey'))
    require(android.get('settlement') == 'sequencer-signed'
            and android.get('receipt_digest') == fixture['android_receipt_digest'] and android.get('event') == 'verified'
            and android.get('event_tamper') == 'rejected' and android.get('event_replay') == 'duplicate',
            'Android real receipt and signed webhook evidence missing')
    for platform, command, public in (
        ('ios', [ios_webhooks], fixture['ios_public_configuration']),
        ('android', [java, '-cp', classpath, 'com.sidiora.layerx.android.sample.WebhookConformanceMain'],
         fixture['android_public_configuration'])):
        capture = fixture[platform + '_webhook_capture']
        ledger = str(STATE / (platform + '-durable-deliveries.json'))
        for expect in ('processed', 'duplicate'):
            report = run([*command, '--configuration', public, '--capture', capture,
                          '--ledger', ledger, '--expect', expect], {}, platform + '-webhook-' + expect)
            if platform == 'ios':
                require('outcome=' + expect in report and 'handled=' + ('1' if expect == 'processed' else '0') in report
                        and all(field in report for field in ('tamper=rejected', 'missing=rejected',
                            'duplicate-header=rejected', 'malformed-signature=rejected',
                            'replay=duplicate', 'reopen=duplicate')),
                        'iOS durable default webhook result missing')
            else:
                parsed = last_json(report)
                require(parsed.get('event') == expect and parsed.get('effects') == (1 if expect == 'processed' else 0)
                        and parsed.get('event_replay') == 'duplicate' and parsed.get('event_tamper') == 'rejected'
                        and parsed.get('event_timestamp') == 'rejected' and parsed.get('event_headers') == 'rejected',
                        'Android durable default webhook result missing')
    for framework in FRAMEWORKS:
        env = dict(agent_env, LAYERX_AGENT_FRAMEWORK=framework,
                   LAYERX_WEBHOOK_DELIVERY_STORE_PATH=str(STATE / (framework + '-journey-deliveries.json')))
        report = last_json(run([node, 'platform/integrations/agents/examples/agent-runtime/index.mjs'],
                              env, framework + '-real-journey'))
        spend = report.get('spend', {})
        result = spend.get('result', {})
        require(report.get('framework') == framework and spend.get('ok') is True
                and result.get('kind') in ('verified', 'owner-budget')
                and result.get('level') == 'sequencer-signed'
                and result.get('receiptDigest') == fixture['framework_receipt_digests'][framework],
                framework + ' actual verified receipt evidence missing')
        webhook = report.get('webhook', {})
        require(webhook.get('rejected', {}).get('status') == 401
                and webhook.get('first', {}).get('body', {}).get('outcome') == 'processed'
                and webhook.get('second', {}).get('body', {}).get('outcome') == 'duplicate'
                and len(webhook.get('handled', [])) == 1, framework + ' signed delivery evidence missing')
        env['LAYERX_WEBHOOK_DELIVERY_STORE_PATH'] = str(STATE / (framework + '-durable-deliveries.json'))
        for expect in ('processed', 'duplicate'):
            env['LAYERX_WEBHOOK_EXPECT'] = expect
            report = last_json(run([node, 'platform/integrations/agents/examples/agent-runtime/webhook-conformance.mjs'],
                                  env, framework + '-webhook-' + expect))
            require(report.get('framework') == framework and report.get('expected') == expect
                    and report.get('handled') == (1 if expect == 'processed' else 0)
                    and report.get('tamper', {}).get('status') == 401
                    and report.get('missing', {}).get('status') == 401
                    and report.get('duplicateHeaders', {}).get('status') == 400
                    and report.get('stale', {}).get('status') == 401
                    and report.get('first', {}).get('outcome') == expect
                    and report.get('second', {}).get('outcome') == 'duplicate'
                    and report.get('handlerFailures') == (1 if expect == 'processed' else 0)
                    and report.get('lockRecovery') == {'signal': 'SIGKILL', 'recovered': True},
                    framework + ' durable default webhook result missing')
    ios_app = directory_artifact(fixture['ios_application'])
    require(Path(ios_app).suffix == '.app', 'actual compiled iOS application required')
    android_apk = artifact(fixture['android_application'])
    require(Path(android_apk).suffix == '.apk', 'actual compiled Android application required')
    run([ios_scan, ios_app], {}, 'ios-application-secret-scan')
    run([java, '-cp', classpath, 'com.sidiora.layerx.android.EmbeddedSecretScan', android_apk],
        {}, 'android-application-secret-scan')
    browser_artifacts = [directory_artifact(entry) if 'files' in entry else artifact(entry)
                         for entry in fixture['browser_artifacts']]
    require(browser_artifacts, 'source-bound compiled browser artifacts required')
    run([node, 'platform/integrations/next/bin/layerx-scan-bundle.mjs', *browser_artifacts],
        agent_env, 'client-bundle-secret-scan')
    print(json.dumps({'status': 'passed', 'revision': REVISION, 'cases': COUNT, 'evidence': str(STATE)}))

try:
    main()
except (Exception, KeyboardInterrupt) as error:
    print(json.dumps({'status': 'refused', 'reason': str(error) if isinstance(error, RuntimeError)
                      else type(error).__name__}))
    raise SystemExit(78)
finally:
    for child in CHILDREN:
        if child.poll() is None:
            os.killpg(child.pid, signal.SIGKILL)
            child.wait()
    print('PAXEER_X_GATE tests=' + str(COUNT) + ' skipped=0')
PY
