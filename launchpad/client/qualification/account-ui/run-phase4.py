import argparse
import concurrent.futures
import hashlib
import json
import math
import os
import pathlib
import re
import signal
import subprocess
import sys
import time
import urllib.error
import urllib.parse
import urllib.request

ROOT = pathlib.Path(__file__).resolve().parents[3]
SOURCE = pathlib.Path(__file__).resolve().parent


def digest(path):
    return hashlib.sha256(pathlib.Path(path).read_bytes()).hexdigest()


def require(condition, message):
    if not condition:
        raise ValueError(message)


def save(path, value):
    path = pathlib.Path(path)
    path.write_text(json.dumps(value, sort_keys=True, indent=2) + '\n')
    path.chmod(0o600)


def endpoint(value):
    url = urllib.parse.urlsplit(value)
    require(url.scheme == 'https' or url.scheme == 'http' and url.hostname in ['127.0.0.1', 'localhost', '::1'], 'HTTPS or isolated loopback endpoint required')
    require(not url.username and not url.password and not url.query and not url.fragment, 'Endpoint must not contain credentials, query or fragment')
    return value.rstrip('/')


def raw(value, signed=False):
    require(isinstance(value, str) and len(value) <= 157 and re.fullmatch(r'-?(0|[1-9][0-9]*)' if signed else r'(0|[1-9][0-9]*)', value), 'Exact integer accounting amount required')
    return int(value)


def exact(value, decimals=6, unit='USDL'):
    if value is None:
        return 'Unavailable'
    amount = raw(value, True)
    magnitude = str(abs(amount)).zfill(decimals + 1)
    rendered = magnitude[:-decimals] + '.' + magnitude[-decimals:] if decimals else magnitude
    return ('-' if amount < 0 else '') + rendered + ' ' + unit


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, request, file, code, message, headers, target):
        return None


class ObservedHTTP:
    def __init__(self, output, timeout):
        self.output = output
        self.timeout = timeout
        self.samples = []
        self.opener = urllib.request.build_opener(NoRedirect())

    def get(self, url, token, label, expected=200):
        request_id = os.urandom(16).hex()
        headers = {'Accept': 'application/json', 'X-Request-ID': request_id}
        if token:
            headers['Authorization'] = 'Bearer ' + token
        started = time.time_ns()
        before = time.monotonic_ns()
        request = urllib.request.Request(url, headers=headers)
        try:
            with self.opener.open(request, timeout=self.timeout) as response:
                status, body = response.status, response.read(4 * 1024 * 1024 + 1)
        except urllib.error.HTTPError as failure:
            status, body = failure.code, failure.read(4 * 1024 * 1024 + 1)
        duration = (time.monotonic_ns() - before) / 1e6
        require(len(body) <= 4 * 1024 * 1024, 'Account response exceeded capture bound')
        record = {'requestId': request_id, 'label': label, 'startedUnixNs': started, 'durationMs': duration, 'method': 'GET', 'url': url, 'status': status, 'bytes': len(body), 'bodySha256': hashlib.sha256(body).hexdigest()}
        path = self.output / ('http-' + request_id + '.json')
        path.write_bytes(body)
        path.chmod(0o600)
        record['bodyPath'] = path.name
        self.samples.append(record)
        require(status == expected, f'{label}: actual status {status}, expected {expected}')
        return json.loads(body) if body else None


def token(account):
    name = account['tokenEnv']
    require(re.fullmatch(r'[A-Z][A-Z0-9_]*', name) and bool(os.environ.get(name)), 'Missing genuine account token environment input')
    return os.environ[name]


def portfolio_checks(value, wallet):
    summary, rows = value['data']['summary'], value['data']['positions']
    require(summary['wallet'].lower() == wallet and len(rows) <= 50, 'Portfolio ownership or first-page50 bound violated')
    require(summary['watermark']['chainId'] == '125' and re.fullmatch(r'(0|[1-9][0-9]*)', summary['watermark']['projectionRevision']), 'Authoritative portfolio revision required')
    for row in rows:
        require(row['wallet'].lower() == wallet and row['chainId'] == '125', 'Cross-account position in snapshot')
        for field in ['balanceRaw', 'knownQuantityRaw', 'unknownQuantityRaw', 'knownRemainingCostUsdlRaw']:
            raw(row[field])
        require(raw(row['knownQuantityRaw']) + raw(row['unknownQuantityRaw']) == raw(row['balanceRaw']), 'Inventory coverage does not reconcile')
        for field in ['remainingCostUsdlRaw', 'realizedPnlRaw', 'unrealizedPnlRaw']:
            if row[field] is not None:
                raw(row[field], True)
    for field in ['pricedSubtotalUsdlRaw', 'knownRealizedSubtotalUsdlRaw']:
        raw(summary[field], field != 'pricedSubtotalUsdlRaw')
    if summary['pricingQuality'] != 'complete':
        require(summary['completeValueUsdlRaw'] is None, 'Partial marks must not fabricate a complete total')
    if summary['basisQuality'] != 'complete':
        require(summary['realizedPnlRaw'] is None, 'Partial basis must not fabricate complete realized PnL')
    return {'snapshotId': summary['snapshotId'], 'revision': summary['watermark']['projectionRevision'], 'rows': len(rows), 'summary': summary}


def percentile(samples, fraction):
    require(samples and all(isinstance(value, (int, float)) and math.isfinite(value) and value >= 0 for value in samples), 'Actual finite timing observations required')
    return sorted(samples)[max(0, math.ceil(len(samples) * fraction) - 1)]


def bind_source(config, output):
    binding_path = pathlib.Path(config['releaseBindingPath']).resolve()
    binding = json.loads(binding_path.read_text())
    require(binding['schemaVersion'] == 1 and binding['task'] == '4.31' and binding['environment'] == 'staging', 'Current release binding required')
    require(pathlib.Path(binding['sourceRoot']).resolve() == ROOT, 'Release binding points to a different checkout')
    rows = binding['sourceFiles']
    require(isinstance(rows, list) and len(rows) > 0, 'Whole candidate source manifest required')
    seen = set()
    for row in rows:
        relative = pathlib.PurePosixPath(row['path'])
        require(not relative.is_absolute() and '..' not in relative.parts and row['path'] not in seen, 'Invalid or duplicate source identity')
        seen.add(row['path'])
        path = ROOT / relative
        require(path.is_file() and not path.is_symlink() and digest(path) == row['sha256'], 'Current source differs: ' + row['path'])
    actual = subprocess.run(['git', 'ls-files', '-z', '--cached', '--others', '--exclude-standard'], cwd=ROOT, capture_output=True, check=True).stdout
    present = {name.decode() for name in actual.split(b'\0') if name and (ROOT / name.decode()).is_file()}
    require(present == seen, 'Release binding must contain the complete current candidate, including untracked source')
    tree_sha = hashlib.sha256(json.dumps(sorted(rows, key=lambda row: row['path']), separators=(',', ':'), sort_keys=True).encode()).hexdigest()
    require(tree_sha == binding['sourceManifestSha256'], 'Source manifest digest mismatch')
    require(re.fullmatch(r'[a-f0-9]{64}', binding['interfaceSha256']) and re.fullmatch(r'[a-f0-9]{64}', binding['networkRevisionSha256']), 'Admitted interface and runtime revision digests required')
    for artifact in binding['artifacts']:
        require(pathlib.Path(artifact['path']).is_file() and digest(artifact['path']) == artifact['sha256'], 'Actual release artifact digest mismatch')
    require(any(item['kind'] == 'web' for item in binding['artifacts']) and any(item['kind'] == 'native' for item in binding['artifacts']), 'Actual web and native release artifacts required')
    save(output / 'candidate-binding.json', binding)
    return binding


def run_child(command, output, label, deadline):
    log = output / (label + '.log')
    with log.open('wb') as stream:
        process = subprocess.Popen(command, cwd=ROOT, stdout=stream, stderr=subprocess.STDOUT, start_new_session=True)
        try:
            code = process.wait(timeout=max(1, deadline - time.monotonic()))
        except subprocess.TimeoutExpired:
            os.killpg(process.pid, signal.SIGTERM)
            try:
                process.wait(timeout=5)
            except subprocess.TimeoutExpired:
                os.killpg(process.pid, signal.SIGKILL)
                process.wait()
            code = 124
    log.chmod(0o600)
    return {'command': command, 'exitCode': code, 'logPath': log.name}


def validate_browser(result, binding):
    require(result['sourceManifestSha256'] == binding['sourceManifestSha256'], 'Browser evidence candidate mismatch')
    required = {'profile-edit', 'portfolio', 'watchlist', 'rewards', 'share', 'recovery', 'dependency-loss', 'signout'}
    observed = {item['scenario'] for item in result['checks'] if item['passed'] is True}
    require(required <= observed, 'Missing actual supported browser journey: ' + ','.join(sorted(required - observed)))
    require(result['deviceResults'] and all(item['passed'] for item in result['deviceResults']), 'Browser device matrix failed')
    require(all(item['violations'] == [] for item in result['accessibility']), 'Actual accessibility violations remain')
    require(result['financialReconciliations'] and all(item['passed'] and item['snapshotId'] and item['revision'] for item in result['financialReconciliations']), 'Missing exact DOM/backend accounting reconciliation')
    require(result['recoveryMeasurements'], 'Actual recovery request aggregation trace required')
    for recovery in result['recoveryMeasurements']:
        require(recovery['authorizationRequests'] <= 1, 'Authorization failure retried')
        require(all(count <= 3 for count in recovery['requestCounts'].values()), 'Transient account read retry cap3 exceeded')
        require(recovery['aggregateReads'] <= 2, 'Portfolio and interval recovery must use aggregate requests')
    require(any(row['state'] == 'confirmed' for row in result['claimLifecycle']) and any(row['state'] == 'uncertain' for row in result['claimLifecycle']) and any(row['state'] in ['failed', 'reverted'] for row in result['claimLifecycle']), 'Actual supported entitlement success/failure/unknown lifecycle missing')
    require(result['crossDevice']['passed'] and result['crossDevice']['nativeEditReceiptSha256'], 'Concurrent native/web watchlist recovery missing')


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--config', required=True)
    parser.add_argument('--output', required=True)
    args = parser.parse_args()
    output = pathlib.Path(args.output).resolve()
    require(not output.exists() and ROOT not in output.parents, 'Fresh private output directory outside source checkout required')
    output.mkdir(parents=True, mode=0o700)
    os.umask(0o077)
    config = json.loads(pathlib.Path(args.config).read_text())
    result = {'schemaVersion': 1, 'task': '4.31', 'startedUnixNs': time.time_ns(), 'checks': [], 'passed': False, 'globalLoadRestoreAcceptance': 'deferred_to_serial9.1', 'children': []}
    http = ObservedHTTP(output, 15)
    deadline = time.monotonic() + 1500
    try:
        require(config['schemaVersion'] == 1 and config['environment'] == 'staging' and config['approvedMutationScope'] == 'dedicated-qualification-accounts', 'Approved isolated staging qualification scope required')
        require(config['measurementEnvironment']['region'] and config['measurementEnvironment']['network'] and config['measurementEnvironment']['observerIdentity'], 'Actual measurement region/network/observer provenance required')
        result['measurementEnvironment'] = config['measurementEnvironment']
        binding = bind_source(config, output)
        result['sourceManifestSha256'] = binding['sourceManifestSha256']
        origins = {name: endpoint(config['origins'][name]) for name in ['web', 'gateway', 'user', 'pnl']}
        authority = http.get(origins['web'] + '/v2/network/authority', None, 'actual-runtime-authority')
        revision = authority['serialExport']['revision']
        require(revision['revisionSha256'] == binding['networkRevisionSha256'] and revision['serialInterfaceSha256'] == binding['interfaceSha256'] and authority['serialExport']['network']['chainId'] == 125, 'Running runtime differs from admitted release binding')
        require(authority['attestation']['eligible'] is True, 'Runtime activation unavailable')
        accounts = config['accounts']
        require(len(accounts) >= 2 and len({item['wallet'].lower() for item in accounts}) >= 2, 'Two distinct real qualification wallets required')
        snapshots = []
        for account in accounts:
            wallet = account['wallet'].lower()
            require(re.fullmatch(r'0x[0-9a-f]{40}', wallet), 'Valid chain125 account required')
            sessions = http.get(origins['gateway'] + '/v2/auth/sessions', token(account), 'real-session-' + wallet)
            require(any(session['sessionId'] == account['sessionId'] and session['wallet'].lower() == wallet and not session['revokedAt'] for session in sessions['data']['items']), 'Token does not authorize configured real session')
            if wallet == config['crossDevice']['wallet'].lower():
                require(any(session['sessionId'] == config['nativeDevice']['accountSessionId'] and session['wallet'].lower() == wallet and not session['revokedAt'] for session in sessions['data']['items']), 'Native device lacks a distinct actual owner-authorized account session')
            value = http.get(origins['pnl'] + '/v3/me/portfolio?limit=50', token(account), 'portfolio-first-page-' + wallet)
            snapshots.append(portfolio_checks(value, wallet))
            interval = config['interval']
            query = urllib.parse.urlencode({**interval, 'limit': '1000'})
            history = http.get(origins['pnl'] + '/v3/me/pnl/history?' + query, token(account), 'history-' + wallet)
            require(history['wallet'].lower() == wallet and len(history['entries']) <= 2000, 'History ownership or2000-point bound violated')
            require(history['watermark']['projectionRevision'] == value['data']['summary']['watermark']['projectionRevision'], 'History and portfolio revisions differ; candidate is not stable for reconciliation')
            for entry in history['entries']:
                raw(entry['knownNetGainUsdlRaw'], True)
                if entry['realizedPnlUsdlRaw'] is not None:
                    raw(entry['realizedPnlUsdlRaw'], True)
            cursor, count, cursors, revision_seen = None, 0, set(), None
            while True:
                query = urllib.parse.urlencode({'limit': '50', **({'cursor': cursor} if cursor else {})})
                page = http.get(origins['gateway'] + '/v1/me/watchlist?' + query, token(account), 'bounded-watchlist-' + wallet)
                require(len(page['data']['items']) <= 50, 'Watchlist page exceeds50')
                require(revision_seen is None or revision_seen == page['revision'], 'Watchlist revision changed while measuring pages')
                revision_seen = page['revision']
                count += len(page['data']['items'])
                require(count <= 500, 'Frozen server watchlist capacity500 exceeded')
                cursor = page['nextCursor']
                if not cursor:
                    break
                require(cursor not in cursors, 'Repeated watchlist cursor')
                cursors.add(cursor)
            http.get(origins['pnl'] + '/v2/me/rewards?limit=50', token(account), 'reward-entitlements-' + wallet)
        save(output / 'authoritative-snapshots.json', snapshots)
        effective = output / 'effective-config.json'
        config['outputRoot'] = str(output)
        config['sourceManifestSha256'] = binding['sourceManifestSha256']
        save(effective, config)
        children = [(['node', str(SOURCE / 'phase4-browser.mjs'), '--config', str(effective)], 'browser'), ([sys.executable, str(SOURCE / 'phase4-native.py'), '--config', str(effective)], 'native')]
        with concurrent.futures.ThreadPoolExecutor(max_workers=2) as executor:
            jobs = [executor.submit(run_child, command, output, label, deadline) for command, label in children]
            result['children'] = [job.result() for job in jobs]
        require(all(child['exitCode'] == 0 for child in result['children']), 'Actual browser/native journey failed; inspect independent child logs')
        browser = json.loads((output / 'browser-result.json').read_text())
        native = json.loads((output / 'native-result.json').read_text())
        validate_browser(browser, binding)
        require(native['passed'] and native['sourceManifestSha256'] == binding['sourceManifestSha256'] and native['physicalDevice'] is True and native['installedArtifactSha256'] in [artifact['sha256'] for artifact in binding['artifacts'] if artifact['kind'] == 'native'], 'Actual native device/binary provenance failed')
        require(native['watchlistReconciliation']['passed'] and native['financialReconciliation']['passed'] and native['networkSamples'], 'Real native watchlist/backend/network evidence missing')
        samples = [sample['durationMs'] for sample in http.samples if sample['status'] == 200]
        result['measurement'] = {'successfulRequests': len(samples), 'responseBytes': sum(sample['bytes'] for sample in http.samples), 'p50Ms': percentile(samples, .5), 'p95Ms': percentile(samples, .95), 'p99Ms': percentile(samples, .99), 'maxMs': max(samples), 'qualificationCapacityClaim': False, 'timingScope': 'complete harness HTTP response including client network; no origin-only SLO claim'}
        result['checks'] = browser['checks']
        result['unsupportedSettlementBlockers'] = browser['unsupportedSettlementBlockers']
        require(result['unsupportedSettlementBlockers'], 'Unsupported settlement inventory must remain explicit')
        result['passed'] = True
        return 0
    except Exception as failure:
        result['failure'] = {'type': type(failure).__name__, 'message': str(failure)}
        return 1
    finally:
        save(output / 'http-samples.json', http.samples)
        result['finishedUnixNs'] = time.time_ns()
        artifacts = []
        for path in sorted(output.rglob('*')):
            if path.is_file() and path.name not in ['result.json', 'artifact-manifest.json', 'effective-config.json']:
                artifacts.append({'path': str(path.relative_to(output)), 'sha256': digest(path), 'bytes': path.stat().st_size})
        save(output / 'artifact-manifest.json', artifacts)
        result['artifactManifestSha256'] = digest(output / 'artifact-manifest.json')
        save(output / 'result.json', result)


if __name__ == '__main__':
    raise SystemExit(main())
