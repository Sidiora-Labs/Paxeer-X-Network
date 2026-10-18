"""Real local timeout and coordinator-termination qualification."""
from pathlib import Path
import json
import os
import signal
import subprocess
import sys
import tempfile
import time
from .workspace import run_process

def _running(pid):
    path = Path(f'/proc/{pid}/stat')
    try:
        return path.read_text().split(') ', 1)[1].split()[0] != 'Z'
    except (FileNotFoundError, ProcessLookupError):
        return False

def qualify_timeout(rootpath):
    root = Path(rootpath).resolve()
    with tempfile.TemporaryDirectory(prefix='dispatcher-timeout-') as directory:
        work = Path(directory)
        pids = work / 'pids.json'
        # The child writes both identities before the coordinator is signalled.
        child_code = ('import subprocess,sys,time,json,os; '
                      'p=subprocess.Popen([sys.executable,"-c","import time; time.sleep(60)"]); '
                      f'open({str(pids)!r},"w").write(json.dumps([os.getpid(),p.pid])); '
                      'time.sleep(60)')
        runner_code = ('import sys; '
                       f'sys.path.insert(0,{str(root)!r}); '
                       'from tools.production.dispatcher.workspace import run_process; '
                       f'run_process([sys.executable,"-c",{child_code!r}],{str(work)!r},60)')
        survivor = subprocess.Popen([sys.executable, '-c', 'import time; time.sleep(60)'])
        coordinator = subprocess.Popen([sys.executable, '-c', runner_code], stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        identities = []
        try:
            deadline = time.monotonic() + 10
            while time.monotonic() < deadline:
                if pids.exists():
                    try:
                        identities = json.loads(pids.read_text())
                        break
                    except json.JSONDecodeError:
                        pass
                if coordinator.poll() is not None:
                    raise AssertionError('Coordinator exited before children started')
                time.sleep(.01)
            if len(identities) != 2:
                raise AssertionError('Children did not report startup')
            coordinator.send_signal(signal.SIGTERM)
            coordinator.communicate(timeout=10)
            deadline = time.monotonic() + 5
            while any(_running(pid) for pid in identities) and time.monotonic() < deadline:
                time.sleep(.01)
            assert coordinator.returncode == 128 + signal.SIGTERM
            assert not any(_running(pid) for pid in identities), 'Owned descendants survived SIGTERM'
            assert survivor.poll() is None, 'Unrelated process was terminated'
            timeout = run_process([sys.executable, '-c', 'import time; time.sleep(60)'], work, .1)
            assert timeout['timed_out'] and timeout['cleanup'] and timeout['exit_code'] < 0
            return {'passed': True, 'sigterm_exit_code': coordinator.returncode,
                    'owned_pids': identities, 'owned_descendants_stopped': True,
                    'unrelated_process_survived': True, 'timeout': timeout}
        finally:
            if coordinator.poll() is None:
                coordinator.kill()
                coordinator.communicate(timeout=5)
            for pid in identities:
                if _running(pid):
                    try:
                        os.kill(pid, signal.SIGKILL)
                    except ProcessLookupError:
                        pass
            survivor.kill()
            survivor.wait(timeout=5)
