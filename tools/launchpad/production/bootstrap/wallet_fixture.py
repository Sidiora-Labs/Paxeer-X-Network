#!/usr/bin/env python3
"""Run web tests against a newly started private Anvil process, then remove it."""
import argparse
import json
import os
from pathlib import Path
import signal
import socket
import subprocess
import sys
import time
import urllib.request

ROOT = Path(__file__).resolve().parents[3]
MNEMONIC = 'test test test test test test test test test test test junk'
ACCOUNT = '0xf39fd6e51aad88f6f4ce6ab8827279cfffb92266'


def rpc(url, method):
    data = json.dumps({'jsonrpc': '2.0', 'id': 1, 'method': method, 'params': []}).encode()
    request = urllib.request.Request(url, data, {'Content-Type': 'application/json'})
    with urllib.request.urlopen(request, timeout=1) as response:
        result = json.load(response)
    if 'error' in result:
        raise ValueError(f'local fixture RPC error for {method}')
    return result['result']


def stop(process):
    if process is None:
        return
    try:
        os.killpg(process.pid, signal.SIGTERM)
    except ProcessLookupError:
        pass
    try:
        process.wait(timeout=2)
    except subprocess.TimeoutExpired:
        try:
            os.killpg(process.pid, signal.SIGKILL)
        except ProcessLookupError:
            pass
        process.wait(timeout=2)


def interrupted(signum, frame):
    raise InterruptedError(f'wallet fixture interrupted by signal{signum}')


def start_node(log, chain_id=125):
    with socket.socket() as reservation:
        reservation.bind(('127.0.0.1', 0))
        port = reservation.getsockname()[1]
    url = f'http://127.0.0.1:{port}'
    node = subprocess.Popen(['anvil', '--host', '127.0.0.1', '--port', str(port),
                             '--chain-id', str(chain_id), '--mnemonic', MNEMONIC, '--silent'],
                            stdout=log, stderr=subprocess.STDOUT, start_new_session=True)
    try:
        ready = False
        for _ in range(100):
            if node.poll() is not None:
                raise RuntimeError('owned Anvil exited before readiness')
            try:
                if 'anvil' not in rpc(url, 'web3_clientVersion').lower():
                    raise RuntimeError('owned fixture did not identify as Anvil')
                if rpc(url, 'eth_chainId') != hex(chain_id):
                    raise RuntimeError('owned fixture chain ID mismatch')
                if rpc(url, 'eth_accounts')[0].lower() != ACCOUNT:
                    raise RuntimeError('owned fixture account mismatch')
                ready = True
                break
            except (OSError, ValueError, KeyError):
                time.sleep(0.1)
        if not ready:
            raise RuntimeError('owned Anvil readiness timed out')
        return node, url
    except BaseException:
        stop(node)
        raise


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--evidence-dir', type=Path, required=True)
    parser.add_argument('--timeout', type=int, default=300)
    args = parser.parse_args()
    if not 1 <= args.timeout <= 6600:
        parser.error('--timeout must be1..6600')
    args.evidence_dir.mkdir(parents=True, exist_ok=True)
    signal.signal(signal.SIGTERM, interrupted)
    signal.signal(signal.SIGINT, interrupted)
    node, dispatch_node, tests = None, None, None
    result = {'qualified': False, 'scope': 'actual web tests with owned private chain125 Anvil', 'errors': []}
    with (args.evidence_dir / 'wallet-anvil.log').open('wb') as log, (args.evidence_dir / 'dispatch-anvil.log').open('wb') as dispatch_log:
        try:
            node, url = start_node(log)
            dispatch_node, dispatch_url = start_node(dispatch_log, 1)
            contract_root = Path(os.environ.get('QUALIFICATION_CONTRACT_ROOT', str(Path(__file__).resolve().parents[4] / 'contracts/launchpad')))
            artifact = contract_root / 'artifacts/contracts/test/MockERC20.sol/MockERC20.json'
            if not artifact.is_file():
                raise RuntimeError('Compiled real ERC20 fixture artifact is missing')
            env = {**os.environ, 'WALLET_TEST_RPC_URL': url, 'WALLET_TEST_ALLOW_CHAIN_MUTATION': '1',
                   'DISPATCH_TEST_RPC_URL': dispatch_url, 'DISPATCH_TEST_ERC20_ARTIFACT': str(artifact.resolve())}
            argv = [sys.executable, str(ROOT / 'tools/production/bootstrap/tests/catalog.py'),
                    'run-vitest', '--root', str(ROOT), '--log', str(args.evidence_dir / 'web-vitest.json'),
                    '--timeout', str(args.timeout)]
            tests = subprocess.Popen(argv, cwd=ROOT, env=env, start_new_session=True)
            code = tests.wait(timeout=args.timeout + 30)
            result.update(exit_code=code, local_rpc_url=url, chain_id=125, dispatch_rpc_url=dispatch_url, dispatch_initial_chain_id=1)
            result['qualified'] = code == 0 and node.poll() is None and dispatch_node.poll() is None
            if not result['qualified']:
                result['errors'].append('web test catalog or owned fixture failed')
        except (OSError, RuntimeError, ValueError, subprocess.TimeoutExpired) as error:
            result['errors'].append(f'{type(error).__name__}: {error}')
        finally:
            stop(tests)
            stop(dispatch_node)
            stop(node)
    (args.evidence_dir / 'wallet-fixture-result.json').write_text(json.dumps(result, indent=2) + '\n')
    print(json.dumps(result))
    return 0 if result['qualified'] else 1


if __name__ == '__main__':
    sys.exit(main())
