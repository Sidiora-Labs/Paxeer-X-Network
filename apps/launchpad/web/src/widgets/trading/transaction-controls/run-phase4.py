#!/usr/bin/env python3
import argparse
import hashlib
import importlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import time
from urllib.parse import urlparse


def require(value, reason):
    if not value:
        raise ValueError(reason)


def sha(path):
    path = Path(path)
    require(path.is_file() and not path.is_symlink(), 'Missing or unsafe bound input: ' + str(path))
    return hashlib.sha256(path.read_bytes()).hexdigest()


def bound(configuration, key):
    path = Path(configuration[key + '_file'])
    require(path.is_absolute() and sha(path) == configuration[key + '_sha256'], 'Bound input mismatch: ' + key)
    value = json.loads(path.read_text())
    require(isinstance(value, dict), 'Object input required: ' + key)
    return value


def write(path, value):
    with Path(path).open('x') as output:
        json.dump(value, output, sort_keys=True, indent=2, allow_nan=False)
        output.write('\n')
    os.chmod(path, 0o600)


def configure(path, digest, root):
    require(sha(path) == digest, 'Measurement configuration hash mismatch')
    c = json.loads(Path(path).read_text())
    require(c.get('schema_version') == 1 and c.get('realm') == 'private-anvil125' and c.get('allow_fixture_mutation') is True, 'Explicit isolated measurement authority required')
    sys.path.insert(0, str(root / 'tools/production'))
    evidence = importlib.import_module('dispatcher.evidence')
    current = evidence.source_digest(root)
    binding = c.get('binding', {})
    require(set(binding) == {'baseline', 'candidate', 'interface', 'layout', 'topology'}, 'Full current source/interface/topology binding required')
    require(binding['candidate'] == current and all(re.fullmatch('[0-9a-f]{64}', v) for v in binding.values()), 'Current candidate digest mismatch')
    gate = bound(c, 'prior_gate')
    require(gate.get('task') == '3.99' and gate.get('qualified') is True and gate.get('canonical_integrated') is True, 'Actually qualified serial3.99 required at execution')
    require(gate.get('canonical_sha256') == binding['baseline'] and gate.get('output_interface_sha256') == binding['interface'], 'Prior gate source/interface does not match')
    require(c.get('spec_sha256') == sha(root / 'spec/production-system/spec.kvx'), 'Current spec digest required')
    topology = bound(c, 'topology')
    require(c['topology_sha256'] == binding['topology'], 'Topology artifact differs from current binding')
    bundle = bound(c, 'browser_bundle')
    require(bundle.get('candidate_sha256') == current and bundle.get('origin') == c['browser_origin'] and isinstance(bundle.get('assets'), dict) and bundle['assets'], 'Actual current browser bundle authority required')
    chain = bound(c, 'fixture')
    require(chain.get('productionActivation') is False and chain['manifest']['chainId'] == 125, 'Actual isolated deployment fixture required')
    require(chain.get('genesisHash') and chain.get('account') and chain.get('pool'), 'Actual fixture genesis/account/pool required')
    endpoint = urlparse(c['rpc_url'])
    require(endpoint.scheme == 'http' and endpoint.hostname in ('127.0.0.1', 'localhost', '::1') and endpoint.port and not endpoint.username and not endpoint.password, 'Owned RPC authority required')
    require(sha(c['compiler']) == c['compiler_sha256'], 'Compiler identity mismatch')
    require(sha(c['node']) == c['node_sha256'], 'Node identity mismatch')
    require(sha(c['playwright_entry']) == c['playwright_entry_sha256'] and sha(c['browser_executable']) == c['browser_executable_sha256'], 'Actual browser/tool identity required')
    origin = urlparse(c['browser_origin'])
    require(origin.scheme == 'https' and origin.hostname in ('127.0.0.1', 'localhost', '::1') and not origin.username and not origin.password, 'Actual isolated application origin required')
    require(sha(root / 'client/apps/web/node_modules/vitest/vitest.mjs') == c['vitest_entry_sha256'], 'Actual native runner identity required')
    require(isinstance(c.get('mobile_drawer_selector'), str) and 0 < len(c['mobile_drawer_selector']) <= 512, 'Actual mobile drawer selector required')
    require(isinstance(c.get('browser_steps'), list) and c['browser_steps'], 'Actual current application steps required')
    require(isinstance(c.get('capture_budget_bytes'), int) and 1048576 <= c['capture_budget_bytes'] <= 536870912, 'Explicit bounded raw capture envelope required')
    stages = c.get('measurement_stages', [])
    require(len(stages) == 2 and {stage['profile'] for stage in stages} == {'peak', 'surge'}, 'Separate actual peak and surge lane measurements required')
    for stage in stages:
        budget = 2000 if stage['profile'] == 'peak' else 4000
        require(type(stage['rate']) is int and type(stage['allocated_quote_rps']) is int and 1 <= stage['rate'] <= stage['allocated_quote_rps'] <= budget, 'Quote/simulation allocation exceeds shared profile')
        require(isinstance(stage['duration_ms'], int) and 1000 <= stage['duration_ms'] <= 60000, 'Explicit bounded measurement duration required')
        require(isinstance(stage['warmup_ms'], int) and 0 <= stage['warmup_ms'] < stage['duration_ms'], 'Explicit warmup exclusion required')
        require(stage['rate'] * stage['duration_ms'] // 1000 <= 100000, 'Measurement sample envelope exceeded')
        require(stage['rate'] * 3 <= stage['allocated_quote_rps'] * 2, 'Quote plus simulation workload exceeds shared allocation')
        require(stage['rate'] * (stage['duration_ms'] - stage['warmup_ms']) >= 101000, 'At least100 measured valid requests required after warmup')
        require(stage.get('quote_ratio') == 1 and stage.get('simulation_ratio') == 1, 'Measured workload must include quote and exact-context simulation')
    faults = c.get('faults', {})
    for name, control in faults.items():
        require(re.fullmatch('[a-z][a-z0-9_]{0,63}', name), 'Invalid fault identity')
        require(control.get('source_sha256') == current and control.get('inputs'), 'Current real fault-control source required')
        require(isinstance(control.get('argv'), list) and control['argv'] and all(isinstance(item, str) and item and '\x00' not in item for item in control['argv']), 'Bound fault argv required')
        require(sha(control['argv'][0]) == control['executable_sha256'], 'Fault executable identity mismatch')
        for file, expected in control['inputs'].items():
            require(Path(file).is_absolute() and sha(file) == expected, 'Fault implementation identity mismatch')
    return c, current, evidence


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--config', required=True)
    parser.add_argument('--config-sha256', required=True)
    parser.add_argument('--output-dir', required=True)
    args = parser.parse_args()
    root = Path(__file__).resolve().parents[7]
    c, current, evidence = configure(args.config, args.config_sha256, root)
    output = Path(args.output_dir).absolute()
    require(not output.exists() and not output.is_symlink(), 'Fresh measurement output namespace required')
    output.mkdir(parents=True, mode=0o700)
    started = time.time()
    write(output / 'binding.json', {'schema_version': 1, 'task': '4.26', 'binding': c['binding'], 'configuration_sha256': args.config_sha256, 'spec_sha256': c['spec_sha256'], 'started_epoch': started, 'shared_acceptance_gate': '9.1'})
    source = Path(__file__).parent / 'fixtures/ZeroResetToken.sol'
    compiler_input = {'language': 'Solidity', 'sources': {'ZeroResetToken.sol': {'content': source.read_text()}}, 'settings': {'outputSelection': {'*': {'*': ['abi', 'evm.bytecode.object']}}}}
    compiled = subprocess.run([c['compiler'], '--standard-json'], input=json.dumps(compiler_input), text=True, capture_output=True, timeout=60)
    (output / 'compiler.log').write_text(compiled.stdout + '\n' + compiled.stderr)
    require(compiled.returncode == 0, 'Actual fixture compiler failed; compiler.log retained')
    compiler_output = json.loads(compiled.stdout)
    require(not any(item.get('severity') == 'error' for item in compiler_output.get('errors', [])), 'Real fixture source compilation failed')
    token = compiler_output['contracts']['ZeroResetToken.sol']['ZeroResetToken']
    artifact = output / 'ZeroResetToken.json'
    write(artifact, {'abi': token['abi'], 'bytecode': '0x' + token['evm']['bytecode']['object'], 'source_sha256': sha(source)})
    web = root / 'client/apps/web'
    env = {**os.environ, 'L26_PHASE4': '1', 'L26_MEASUREMENT_CONFIG': str(Path(args.config).resolve()), 'L26_MEASUREMENT_CONFIG_SHA256': args.config_sha256, 'L26_MEASUREMENT_OUTPUT': str(output), 'L26_FIXTURE_JSON': c['fixture_file'], 'L26_FIXTURE_SHA256': c['fixture_sha256'], 'L26_RPC_URL': c['rpc_url'], 'L26_ZERO_RESET_ARTIFACT': str(artifact), 'L26_CACHE_DIR': str(output / 'vite-cache'), 'WALLET_TEST_RPC_URL': c['rpc_url'], 'WALLET_TEST_ALLOW_CHAIN_MUTATION': '1'}
    command = [c['node'], str(web / 'node_modules/vitest/vitest.mjs'), 'run', '--config', 'src/widgets/trading/transaction-controls/vitest.phase2.config.ts', '--reporter=tap', '--reporter=json', '--outputFile=' + str(output / 'vitest.json')]
    with (output / 'native.log').open('xb') as log:
        process = subprocess.run(command, cwd=web, env=env, stdout=log, stderr=subprocess.STDOUT, timeout=1200)
    required_artifacts = {'measurement.json', 'rpc.ndjson', 'operations.ndjson', 'recovery.ndjson', 'journey.ndjson', 'browser.ndjson', 'browser-trace.zip', 'trade-desktop.png', 'trade-mobile.png', 'browser-assets.json', 'analytics-binding.json', 'minimum-boundary.json', 'optical-boundary.json', 'lane-peak.json', 'lane-surge.json', 'recovery-summary.json'}
    missing = sorted(name for name in required_artifacts if not (output / name).is_file() or (output / name).stat().st_size == 0)
    unchanged = evidence.source_digest(root) == current
    artifacts = {path.name: sha(path) for path in output.iterdir() if path.is_file()}
    result = {'schema_version': 1, 'task': '4.26', 'binding': c['binding'], 'configuration_sha256': args.config_sha256, 'started_epoch': started, 'finished_epoch': time.time(), 'command': command, 'exit_code': process.returncode, 'source_unchanged': unchanged, 'missing_artifacts': missing, 'artifacts': artifacts, 'lane_measurement_executed': (output / 'operations.ndjson').is_file() and (output / 'operations.ndjson').stat().st_size > 0 and (output / 'rpc.ndjson').is_file() and (output / 'rpc.ndjson').stat().st_size > 0, 'shared_capacity_qualified': False, 'shared_restore_qualified': False, 'production_qualified': False, 'shared_acceptance_gate': '9.1'}
    write(output / 'result.json', result)
    require(process.returncode != 0 or not missing, 'Actual measurement artifacts missing: ' + ','.join(missing))
    require(unchanged, 'Current source changed during measurement')
    return process.returncode


if __name__ == '__main__':
    sys.exit(main())
