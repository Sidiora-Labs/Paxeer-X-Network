#!/usr/bin/env python3
"""Build fresh private Solidity fixtures and own the complete Anvil test lifecycle.
Invoke through the coordinator's bounded frontend pool. Never use a running chain.
"""
import argparse
import datetime
import hashlib
import json
import os
from pathlib import Path
import shutil
import socket
import subprocess
import sys
import tempfile
import time
import urllib.request


def main():
    fixtures = Path(__file__).resolve().parent
    web = fixtures.parents[3]
    workspace = web.parents[2]
    parser = argparse.ArgumentParser()
    parser.add_argument('--output-root', type=Path, default=workspace / 'evidence/production-system/L25/phase1')
    args = parser.parse_args()
    output_root = args.output_root.resolve()
    output_root.mkdir(parents=True, exist_ok=True)
    output = Path(tempfile.mkdtemp(prefix='lifecycle-', dir=output_root))
    summary = {'observedAt': datetime.datetime.now(datetime.timezone.utc).isoformat(), 'chainId': 125,
               'rpc': 'http://127.0.0.1:22500', 'output': str(output), 'checks': [], 'exitCode': 1}
    child = None
    anvil_log = None
    exit_code = 1

    def command(argv, name, cwd, env=None, timeout=120):
        log = output / f'{name}.log'
        with log.open('w') as stream:
            completed = subprocess.run(argv, cwd=cwd, env=env, stdout=stream, stderr=subprocess.STDOUT, timeout=timeout, check=False)
        summary['checks'].append({'command': argv, 'cwd': str(cwd), 'exitCode': completed.returncode, 'log': str(log)})
        if completed.returncode:
            raise RuntimeError(f'{name} failed with exit {completed.returncode}; see {log}')

    try:
        # Refuse any occupied endpoint. No RPC reset, reuse or peer-process cleanup.
        with socket.socket() as probe:
            probe.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
            probe.bind(('127.0.0.1', 22500))
        private_fixture = output / 'fixture'
        shutil.copytree(fixtures / 'src', private_fixture / 'src')
        shutil.copyfile(fixtures / 'foundry.toml', private_fixture / 'foundry.toml')
        sources = [fixtures / 'foundry.toml', fixtures / 'src/FundingToken.sol']
        summary['fixtureSources'] = {str(p.relative_to(workspace)): hashlib.sha256(p.read_bytes()).hexdigest() for p in sources}
        command(['forge', 'build', '--root', str(private_fixture)], 'fixture-build', workspace)
        anvil_log = (output / 'anvil.log').open('w')
        child = subprocess.Popen(['anvil', '--host', '127.0.0.1', '--port', '22500', '--chain-id', '125', '--silent'],
                                 cwd=workspace, stdout=anvil_log, stderr=subprocess.STDOUT)
        summary['anvilPid'] = child.pid
        ready = False
        deadline = time.monotonic() + 15
        while time.monotonic() < deadline:
            if child.poll() is not None:
                raise RuntimeError(f'Owned Anvil exited before readiness: {child.returncode}')
            try:
                request = urllib.request.Request(summary['rpc'], data=json.dumps({'jsonrpc': '2.0', 'id': 1, 'method': 'eth_chainId', 'params': []}).encode(), headers={'Content-Type': 'application/json'})
                with urllib.request.urlopen(request, timeout=1) as response:
                    body = json.load(response)
                if body.get('result') != '0x7d':
                    raise RuntimeError('Owned Anvil did not report chain125')
                if child.poll() is not None:
                    raise RuntimeError('Owned Anvil exited during readiness')
                ready = True
                break
            except (OSError, ValueError):
                time.sleep(0.05)
        if not ready:
            raise RuntimeError('Owned Anvil readiness timed out')
        env = os.environ.copy()
        env.update(WALLET_TEST_RPC_URL=summary['rpc'], WALLET_TEST_ALLOW_CHAIN_MUTATION='1',
                   WALLET_TEST_RECEIPTS_PATH=str(output / 'native-receipts.json'),
                   L25_FIXTURE_ARTIFACTS=str(private_fixture / 'out'),
                   L25_VITEST_CACHE_DIR=str(output / 'vite-cache'))
        tests_json = output / 'vitest.json'
        command(['node', 'node_modules/vitest/vitest.mjs', 'run', '--config', 'src/core/wallet-sdk/vitest.l25.config.ts',
                 'src/core/wallet-sdk/client.integration.test.ts', 'src/core/wallet-sdk/primitives.test.ts',
                 'src/core/wallet-sdk/funding.integration.test.ts', '--reporter=default', '--reporter=json',
                 '--outputFile=' + str(tests_json)], 'vitest', web, env)
        report = json.loads(tests_json.read_text())
        if not report.get('success') or report.get('numTotalTests', 0) < 17 or report.get('numFailedTests', 0) or report.get('numPendingTests', 0):
            raise RuntimeError('Native test report was unsuccessful, incomplete or skipped')
        receipts = json.loads((output / 'native-receipts.json').read_text())
        required = {'USDL_deposit', 'reviewed_native_transfer', 'reviewed_erc20_transfer', 'mined_revert', 'pending_then_committed_no_resubmission', 'delayed_committed_funding_no_duplicate_credit', 'disconnect_preserves_real_transfer_hash'}
        if receipts.get('chainId') != 125 or not required.issubset({r.get('kind') for r in receipts.get('receipts', [])}):
            raise RuntimeError('Authentic local receipt evidence is incomplete')
        summary.update(testsPassed=report['numPassedTests'], testsTotal=report['numTotalTests'], receipts=str(output / 'native-receipts.json'))
        exit_code = 0
    except Exception as error:
        summary['error'] = f'{type(error).__name__}: {error}'
    finally:
        if child is not None:
            if child.poll() is None:
                child.terminate()
                try:
                    child.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    child.kill()
                    child.wait(timeout=5)
            summary['cleanup'] = {'pid': child.pid, 'returnCode': child.returncode, 'exactChildOnly': True}
        if anvil_log:
            anvil_log.close()
        summary['exitCode'] = exit_code
        summary['finishedAt'] = datetime.datetime.now(datetime.timezone.utc).isoformat()
        (output / 'summary.json').write_text(json.dumps(summary, indent=2) + '\n')
        print(json.dumps(summary))
    return exit_code


if __name__ == '__main__':
    sys.exit(main())
