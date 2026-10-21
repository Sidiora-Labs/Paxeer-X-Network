#!/usr/bin/env python3
"""Run isolated L18 fixtures; refuse peers and never reuse historical evidence."""
import argparse
import json
import os
import pathlib
import signal
import socket
import subprocess
import sys
import time

PORT = 21800


def stop(child):
    """Bound termination of this exact owned process group, including helpers."""
    if child.poll() is None:
        try:
            os.killpg(child.pid, signal.SIGTERM)
        except ProcessLookupError:
            child.wait(timeout=10)
            return
        try:
            child.wait(timeout=10)
        except subprocess.TimeoutExpired:
            try:
                os.killpg(child.pid, signal.SIGKILL)
            except ProcessLookupError:
                pass
            child.wait(timeout=10)


def refuse_occupied_port():
    with socket.socket() as probe:
        probe.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
        try:
            probe.bind(('127.0.0.1', PORT))
        except OSError as exc:
            raise RuntimeError('lane port21800 occupied; refusing to reuse or reset peer') from exc


def owns_listener(child):
    """Linux fixture ownership check: listener inode must belong to this child."""
    inodes = set()
    try:
        for fd in pathlib.Path(f'/proc/{child.pid}/fd').iterdir():
            try:
                target = os.readlink(fd)
            except FileNotFoundError:
                continue
            if target.startswith('socket:['):
                inodes.add(target[8:-1])
        for row in pathlib.Path('/proc/net/tcp').read_text().splitlines()[1:]:
            fields = row.split()
            if fields[1] == f'0100007F:{PORT:04X}' and fields[3] == '0A' and fields[9] in inodes:
                return True
    except (FileNotFoundError, ProcessLookupError):
        return False
    return False


def process_start(pid):
    return pathlib.Path(f'/proc/{pid}/stat').read_text().rsplit(')', 1)[1].split()[19]


def assert_owned(child):
    if child.poll() is not None or not owns_listener(child):
        raise RuntimeError('exact owned Anvil child is not alive and listening; mutation refused')


def wait_ready(child, attempts=100):
    for _ in range(attempts):
        if child.poll() is not None:
            raise RuntimeError('owned Anvil child exited before readiness')
        if owns_listener(child):
            with socket.socket() as probe:
                probe.settimeout(.1)
                if probe.connect_ex(('127.0.0.1', PORT)) == 0:
                    assert_owned(child)
                    return
        time.sleep(.05)
    raise RuntimeError('owned Anvil readiness deadline exhausted; mutation refused')


def run_bounded(cmd, **kwargs):
    child = subprocess.Popen(cmd, start_new_session=True, **kwargs)
    try:
        return child.wait(timeout=930)
    finally:
        stop(child)


def install_signal_handlers():
    def interrupted(signum, _frame):
        raise SystemExit(128 + signum)
    signal.signal(signal.SIGTERM, interrupted)
    signal.signal(signal.SIGINT, interrupted)


def main():
    install_signal_handlers()
    parser = argparse.ArgumentParser()
    parser.add_argument('--run', default='qualification')
    parser.add_argument('--output-dir', type=pathlib.Path,
                        help='Private evidence directory; existing run names are refused')
    args = parser.parse_args()
    if len(args.run) > 64 or not args.run.replace('-', '').isalnum():
        raise RuntimeError('invalid run name')
    base = pathlib.Path(__file__).resolve().parents[6]
    evidence = args.output_dir or base / 'launchpad/evidence/production-system/L18'
    evidence.mkdir(parents=True, exist_ok=True)
    evidence = evidence.resolve()
    refuse_occupied_port()
    if list(evidence.glob(args.run + '-*')):
        raise RuntimeError('run name already has evidence; historical output will not be reused')
    # Exclusive marker prevents concurrent callers using the same evidence label.
    with (evidence / (args.run + '-run.json')).open('x') as marker:
        json.dump({'run': args.run, 'started_ns': time.time_ns()}, marker)
    runner = '/tmp/kindle-implementation-20261009/wave1/run-bounded-check.py'
    results = []
    child = None
    try:
        with (evidence / (args.run + '-anvil.log')).open('x') as log:
            child = subprocess.Popen(['anvil', '--host', '127.0.0.1', '--port', str(PORT),
                                      '--chain-id', '125', '--silent'],
                                     stdout=log, stderr=subprocess.STDOUT, start_new_session=True)
            wait_ready(child)
            fixture = evidence / (args.run + '-fixture.json')
            identity = json.dumps({'pid': child.pid, 'starttime': process_start(child.pid)})
            fixture_env = os.environ.copy()
            fixture_env['L18_ANVIL_IDENTITY'] = identity
            cmd = ['python3', runner, '--pool', 'frontend', '--timeout', '900', '--',
                   'node', str(pathlib.Path(__file__).with_name('fixture.cjs'))]
            with fixture.open('x') as out, (evidence / (args.run + '-fixture-error.log')).open('x') as err:
                # Last check immediately before starting the mutating fixture.
                assert_owned(child)
                code = run_bounded(cmd, cwd=base, env=fixture_env, stdout=out, stderr=err)
            results.append({'command': cmd, 'exit': code})
            if code:
                raise RuntimeError('fixture failed')
            env = os.environ.copy()
            env.update(GOWORK='off', GOMAXPROCS='2', GOFLAGS='-mod=readonly',
                       L18_POSTGRES_DSN='postgres://kindle_fixture:kindle_fixture_password@127.0.0.1:25432/kindle_fixture?sslmode=disable',
                       L18_CHAIN_FIXTURE=str(fixture), L18_ANVIL_IDENTITY=identity)
            cmd = ['python3', runner, '--pool', 'go', '--timeout', '900', '--', 'go', 'test',
                   '-p', '1', '-count=1', '-json', './internal/production/l18', './internal/referral']
            with (evidence / (args.run + '-go.jsonl')).open('x') as out:
                assert_owned(child)
                code = run_bounded(cmd, cwd=base / 'services/launchpad/pnl-tracker', env=env,
                                   stdout=out, stderr=subprocess.STDOUT)
            results.append({'command': cmd, 'cwd': str(base / 'services/launchpad/pnl-tracker'),
                            'env': {k: env[k] for k in ['GOWORK', 'GOMAXPROCS', 'GOFLAGS',
                                                       'L18_POSTGRES_DSN', 'L18_CHAIN_FIXTURE', 'L18_ANVIL_IDENTITY']}, 'exit': code})
    except BaseException as exc:
        results.append({'lifecycle_error': str(exc), 'exit': 1})
        raise
    finally:
        if child is not None:
            stop(child)
        with (evidence / (args.run + '-commands.json')).open('x') as report:
            json.dump(results, report, indent=2)
            report.write('\n')
    if not results or results[-1]['exit']:
        return 1
    print('L18 focused qualification passed; production integration and capacity remain pending')
    return 0


if __name__ == '__main__':
    sys.exit(main())
