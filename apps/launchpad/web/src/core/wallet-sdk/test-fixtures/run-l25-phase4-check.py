#!/usr/bin/env python3
import argparse
from collections import Counter, defaultdict
from datetime import datetime, timezone
import hashlib
import json
import math
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import time
import urllib.request


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def load(path):
    if not path.is_file() or path.is_symlink() or path.stat().st_size > 20_000_000:
        raise ValueError('Bounded actual regular artifact required: ' + str(path))
    return json.loads(path.read_bytes())


def rpc(endpoint, method, params, output):
    started = time.monotonic_ns()
    request = urllib.request.Request(endpoint, data=json.dumps({'jsonrpc': '2.0', 'id': started, 'method': method, 'params': params}).encode(), headers={'Content-Type': 'application/json'})
    with urllib.request.urlopen(request, timeout=15) as response:
        wire = response.read(2_000_001)
        if len(wire) > 2_000_000:
            raise ValueError('RPC result exceeded the explicit raw capture bound')
        value = json.loads(wire)
    with (output / 'rpc-observations.jsonl').open('a') as stream:
        stream.write(json.dumps({'method': method, 'startedMonotonicNs': started, 'endedMonotonicNs': time.monotonic_ns(), 'observedAt': datetime.now(timezone.utc).isoformat(), 'response': value}) + '\n')
    if value.get('error') or 'result' not in value:
        raise ValueError('Actual RPC read failed: ' + method)
    return value['result']


def percentile(values, probability):
    if not values:
        return None
    ordered = sorted(values)
    return ordered[math.ceil(len(ordered) * probability) - 1]


def measurement(raw, expected):
    if raw.get('schemaVersion') != 1 or raw.get('dropped') != 0 or raw['identity']['sourceSha256'] != expected['sourceSha256'] or raw['identity']['deploymentManifestHash'] != expected['deploymentManifestHash']:
        raise ValueError('Raw runtime source/deployment binding or capture completeness failed')
    samples = raw.get('samples')
    if not isinstance(samples, list) or len(samples) > 50000:
        raise ValueError('Bounded raw operation samples required')
    started = raw['startedMonotonicMs']
    ended = raw['endedMonotonicMs']
    if not isinstance(started, (int, float)) or not isinstance(ended, (int, float)) or ended < started:
        raise ValueError('Actual monotonic capture interval required')
    pending, in_flight, maxima = {}, Counter(), Counter()
    durations, events, counts = defaultdict(list), defaultdict(list), Counter()
    last = started
    for sequence, sample in enumerate(samples, 1):
        at = sample['monotonicMs']
        if sample['sequence'] != sequence or not math.isfinite(at) or at < last or at > ended:
            raise ValueError('Raw sample sequence or monotonic clock invalid')
        last = at
        kind, scope, operation = sample['kind'], sample['scope'], sample['operationId']
        phase = sample['phase']
        if phase == 'event':
            events[kind].append(at)
            continue
        key = (kind, scope)
        if phase == 'start':
            if operation in pending:
                raise ValueError('Duplicate operation start')
            pending[operation] = sample
            in_flight[key] += 1
            maxima[key] = max(maxima[key], in_flight[key])
        elif phase == 'end':
            origin = pending.pop(operation, None)
            if origin is None or (origin['kind'], origin['scope']) != key or sample.get('outcome') not in ['resolved', 'rejected']:
                raise ValueError('Unmatched actual operation completion')
            in_flight[key] -= 1
            outcome = sample['outcome']
            counts[(kind, outcome)] += 1
            durations[(kind, outcome)].append(at - origin['monotonicMs'])
        else:
            raise ValueError('Unknown raw measurement phase')
    if pending:
        raise ValueError('In-flight operations remain; capture cannot claim completion')
    for (kind, scope), maximum in maxima.items():
        if kind in ['balance.refresh', 'auth.challenge', 'auth.login', 'auth.refresh', 'connector.connect', 'connector.initialize', 'wallet.sign', 'transfer.confirm'] and maximum > 1:
            raise ValueError('Concurrent account/asset operation exceeded one: ' + kind + ':' + scope)
    metadata_counts = Counter(sample['scope'] for sample in samples if sample['kind'] == 'asset.metadata' and sample['phase'] == 'start')
    if any(count > 1 for count in metadata_counts.values()):
        raise ValueError('Immutable token decimals are reread instead of cached by chain/address')
    provider_events = events['wallet.event']
    for index, first in enumerate(provider_events):
        if index and first - provider_events[index - 1] < 50:
            continue
        last_event = first
        for next_event in provider_events[index + 1:]:
            if next_event - last_event >= 50:
                break
            last_event = next_event
        clear_count = sum(first <= at <= last_event + 50 for at in events['wallet.cache_clear'])
        refresh_count = sum(first <= at <= last_event + 100 for at in events['wallet.refresh_batch'])
        if clear_count > 1 or refresh_count > 1:
            raise ValueError('Provider burst exceeded one invalidation/refresh batch in a settled 50ms epoch')
    duration_ms = ended - started
    rates = {kind + ':' + outcome: count * 1000 / duration_ms if duration_ms else None for (kind, outcome), count in counts.items()}
    for key, rate in rates.items():
        if key.startswith(('auth.challenge:', 'auth.login:', 'auth.refresh:')) and rate is not None and rate > 2000:
            raise ValueError('Measured local auth subset exceeds the global 2000 writes/s peak budget')
    histograms = {kind + ':' + outcome: {'count': len(values), 'p50Ms': percentile(values, .5), 'p95Ms': percentile(values, .95), 'p99Ms': percentile(values, .99), 'maximumMs': max(values)} for (kind, outcome), values in durations.items()}
    return {'rawSamples': len(samples), 'operationCounts': {kind + ':' + outcome: value for (kind, outcome), value in counts.items()}, 'histograms': histograms, 'ratesPerSecond': rates, 'maximumConcurrent': {kind + ':' + scope: value for (kind, scope), value in maxima.items()}, 'eventCounts': {key: len(value) for key, value in events.items()}, 'globalCapacityAccepted': False}


def chain_evidence(config, output):
    endpoint = config['network']['publicRPC']
    if not endpoint.startswith('https://') or config['network']['chainId'] != 125:
        raise ValueError('Admitted HTTPS chain125 RPC authority required')
    if int(rpc(endpoint, 'eth_chainId', [], output), 16) != 125:
        raise ValueError('Actual RPC chain mismatch')
    head = config['committedHead']
    canonical = rpc(endpoint, 'eth_getBlockByNumber', [hex(int(head['number'])), False], output)
    if canonical is None or canonical['hash'].lower() != head['hash'].lower():
        raise ValueError('Current committed canonical head proof unavailable')
    token = config['network']['addresses']['USDL'].lower()
    code = rpc(endpoint, 'eth_getCode', [token, hex(int(head['number']))], output)
    if rpc(endpoint, 'web3_sha3', [code], output).lower() != config['usdlRuntimeHash'].lower():
        raise ValueError('Actual USDL runtime differs from admitted deployment')
    if not code or code == '0x':
        raise ValueError('Actual approved USDL runtime unavailable')
    decimals = rpc(endpoint, 'eth_call', [{'to': token, 'data': '0x313ce567'}, hex(int(head['number']))], output)
    if int(decimals, 16) != 6:
        raise ValueError('Actual approved USDL decimals mismatch')
    precision_wallet = config['precisionWallet']
    if not isinstance(precision_wallet, str) or len(precision_wallet) != 42:
        raise ValueError('Actual precision scenario wallet required')
    native_balance = rpc(endpoint, 'eth_getBalance', [precision_wallet, hex(int(head['number']))], output)
    token_balance = rpc(endpoint, 'eth_call', [{'to': token, 'data': '0x70a08231' + precision_wallet[2:].lower().rjust(64, '0')}, hex(int(head['number']))], output)
    if int(native_balance, 16) != 1000000000000000000 or int(token_balance, 16) != 1000000:
        raise ValueError('Actual simultaneous one-PAX/one-USDL balance scenario not present')
    receipts = config.get('receiptProofs')
    if not isinstance(receipts, list) or not receipts:
        raise ValueError('Actual wallet-authorized transfer and deposit hashes required')
    verified = []
    for proof in receipts:
        transaction_hash = proof['transactionHash']
        receipt = rpc(endpoint, 'eth_getTransactionReceipt', [transaction_hash], output)
        transaction = rpc(endpoint, 'eth_getTransactionByHash', [transaction_hash], output)
        if receipt is None or transaction is None or receipt['transactionHash'].lower() != transaction_hash.lower() or transaction['hash'].lower() != transaction_hash.lower() or int(receipt['blockNumber'], 16) > int(head['number']):
            raise ValueError('Receipt is absent, foreign or above committed head')
        block = rpc(endpoint, 'eth_getBlockByNumber', [receipt['blockNumber'], False], output)
        if block['hash'].lower() != receipt['blockHash'].lower() or transaction['from'].lower() != proof['wallet'].lower():
            raise ValueError('Receipt canonical block or authorized sender mismatch')
        if proof['kind'] == 'native_transfer':
            if transaction['to'].lower() != proof['recipient'].lower() or int(transaction['value'], 16) != int(proof['amountRaw']):
                raise ValueError('Actual native transfer differs from reviewed amount/recipient')
        elif proof['kind'] == 'usdl_deposit':
            incoming = sum(int(log['data'], 16) for log in receipt['logs'] if log['address'].lower() == token and len(log['topics']) == 3 and log['topics'][0].lower() == '0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef' and log['topics'][2].lower() == '0x' + proof['recipient'][2:].lower().rjust(64, '0'))
            if incoming != 1000000 or proof['amountRaw'] != '1000000':
                raise ValueError('Actual one-USDL deposit transfer evidence mismatch')
            balance = rpc(endpoint, 'eth_call', [{'to': token, 'data': '0x70a08231' + proof['recipient'][2:].lower().rjust(64, '0')}, hex(int(head['number']))], output)
            if int(balance, 16) < 1000000:
                raise ValueError('Deposit is not present in committed on-chain balance')
        else:
            raise ValueError('Unknown admitted receipt evidence kind')
        if receipt['status'] != '0x1':
            raise ValueError('Actual successful funding/transfer receipt required')
        verified.append({'hash': transaction_hash, 'amountRaw': proof['amountRaw'], 'kind': proof['kind'], 'explorerUrl': config['network']['explorerBase'].rstrip('/') + '/tx/' + transaction_hash})
    if {'native_transfer', 'usdl_deposit'} - {entry['kind'] for entry in verified}:
        raise ValueError('Both actual native transfer and USDL deposit proof required')
    if rpc(endpoint, 'eth_getBlockByNumber', [hex(int(head['number'])), False], output)['hash'].lower() != head['hash'].lower():
        raise ValueError('Committed head changed during independent receipt checks')
    return verified


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--config', type=Path, required=True)
    parser.add_argument('--output-dir', type=Path, required=True)
    args = parser.parse_args()
    args.output_dir.mkdir(parents=True, exist_ok=True)
    output = Path(tempfile.mkdtemp(prefix='l25-phase4-', dir=args.output_dir))
    fixtures = Path(__file__).resolve().parent
    root = fixtures.parents[6]
    report = {'task': '4.25', 'qualified': False, 'globalCapacityAccepted': False, 'globalEvidenceDependency': 'serial9.1', 'startedAt': datetime.now(timezone.utc).isoformat(), 'output': str(output)}
    code = 1
    try:
        config = load(args.config)
        expected = os.environ.get('L25_PHASE4_CONFIG_SHA256')
        if not expected or digest(args.config) != expected or config.get('schemaVersion') != 1:
            raise ValueError('Admitted current Phase4 configuration and digest required')
        if config['sourceSha256'] != os.environ.get('L25_ADMITTED_SOURCE_SHA256'):
            raise ValueError('Exact admitted source identity missing or changed')
        owned = config.get('ownedSourceDigests')
        if not isinstance(owned, dict) or not owned:
            raise ValueError('Exact current owned source manifest required')
        expected_paths = set()
        for directory in ['client/apps/web/src/core/wallet-sdk', 'client/apps/web/src/widgets/funding', 'client/apps/web/src/widgets/wallet-recovery']:
            expected_paths.update(str(path.relative_to(root)) for path in (root / directory).rglob('*') if path.is_file() and path.suffix in ['.ts','.tsx','.py','.cjs','.toml','.sol'])
        expected_paths.update(['client/apps/web/src/core/wagmi-config.ts','client/apps/web/src/shell/wallet-button.tsx','client/apps/web/src/shell/wallet-connect-modal.tsx','client/apps/web/src/shell/wallet-modal.tsx'])
        if set(owned) != expected_paths:
            raise ValueError('Owned source binding omits or adds actual lane files')
        for name, expected_digest in owned.items():
            if digest(root / name) != expected_digest:
                raise ValueError('Current source bytes changed: ' + name)
        report['sourceDigests'] = owned
        report['configSha256'] = expected
        command = ['node', str(fixtures / 'phase4-browser.mjs'), str(args.config.resolve()), str(output / 'browser')]
        log = output / 'browser.log'
        with log.open('wb') as stream:
            completed = subprocess.run(command, cwd=root, stdout=stream, stderr=subprocess.STDOUT, timeout=900, check=False)
        report['browserCommand'] = command
        report['browserExitCode'] = completed.returncode
        report['browserLog'] = str(log)
        if completed.returncode:
            raise ValueError('Actual external-wallet browser measurement failed')
        browser = load(output / 'browser/browser-result.json')
        if set(browser['scenarios']) != set(range(1, 9)) or browser['sourceSha256'] != config['sourceSha256'] or browser['configSha256'] != expected:
            raise ValueError('Actual browser scenario/source binding incomplete')
        required = {'ssr_hydration','one_selected_account','reload_reconnect','pax_exact','usdl_exact','stale_allowance_discarded','foreign_domain_rejected','signature_rejected_unsigned','fresh_same_wallet_other_device','wrong_account_chain_rejected','usdl_committed_balance','unrelated_recipient_unchanged','zero_native_maximum','rejected_transfer_no_dispatch','mined_revert_not_success','provider_authorization_not_settlement','key_custody_wallet_owned','wrong_network_no_auto_recovery'}
        if required - {assertion.get('check') for assertion in browser['assertions']}:
            raise ValueError('Required actual UI assertions were not observed')
        captures = sorted((output / 'browser').glob('measurement-*.json'))
        if not captures:
            raise ValueError('Actual runtime raw captures absent')
        report['measurements'] = [{'file': str(path), 'sha256': digest(path), 'derived': measurement(load(path), config)} for path in captures]
        if not any(entry['derived']['rawSamples'] for entry in report['measurements']):
            raise ValueError('No actual operation measurement samples captured')
        report['chainEvidence'] = chain_evidence(config, output)
        report['remainingAuthority'] = ['Real funding vendor quota/duplicate signed callback/checkout retry-storm provider contract is absent; D09_008 remains a release blocker', 'Physical supported-device proof and configured WalletConnect qualification remain serial9.1 evidence inputs', '100000 peak sockets and aggregate 2000 writes/s acceptance require the final shared workload']
        report['measurementCaptureComplete'] = True
        raise ValueError('D09_008 vendor authority unavailable; capture is retained and task qualification remains pending')
    except Exception as error:
        report['error'] = type(error).__name__ + ': ' + str(error)
    report['exitCode'] = code
    report['finishedAt'] = datetime.now(timezone.utc).isoformat()
    report['artifacts'] = {str(path.relative_to(output)): digest(path) for path in output.rglob('*') if path.is_file()}
    (output / 'result.json').write_text(json.dumps(report, indent=2) + '\n')
    print(json.dumps({'exitCode': code, 'report': str(output / 'result.json')}))
    return code


if __name__ == '__main__':
    sys.exit(main())
