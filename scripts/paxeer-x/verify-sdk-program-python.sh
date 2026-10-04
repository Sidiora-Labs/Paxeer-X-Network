#!/bin/sh
set -eu
python_sdk_task_root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
export PYTHONPATH="$python_sdk_task_root/agent/sdk/python${PYTHONPATH:+:$PYTHONPATH}"
export PYTHONDONTWRITEBYTECODE=1
exec python3 - "$python_sdk_task_root" <<'PY'
import importlib.util
import json
import os
from pathlib import Path
import sys
import unittest

root = Path(sys.argv[1])
if not os.environ.get('LAYERX_PYTHON_PROGRAM_HTTP_FIXTURE') or not os.environ.get('PAXEER_X_PROGRAM_TERMINAL_V5_CORPUS'):
    print(json.dumps({'exit_code': 78, 'status': 'prerequisite-unavailable',
                      'reason': 'protected real gateway/native cluster HTTP fixture and genuine signed native v5 corpus required'}))
    sys.exit(78)
suite = unittest.TestSuite()
paths = ['platform/sdk/conformance/terminal-v4.test.py',
         'tests/agent/sdk/python/test_native_program_call.py',
         'tests/agent/sdk/python/test_native_program_binding.py',
         'tests/agent/sdk/python/test_program_lifecycle.py',
         'tests/agent/sdk/python/test_program_executed_v3.py',
         'tests/agent/sdk/python/test_program_http.py']
for index, relative in enumerate(paths):
    spec = importlib.util.spec_from_file_location('python_program_corpus_' + str(index), root / relative)
    if spec is None or spec.loader is None:
        raise RuntimeError('actual Python Programs corpus unavailable')
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    suite.addTests(unittest.defaultTestLoader.loadTestsFromModule(module))
result = unittest.TextTestRunner(verbosity=2).run(suite)
if result.skipped or result.expectedFailures or result.unexpectedSuccesses:
    sys.exit(1)
sys.exit(0 if result.wasSuccessful() else 1)
PY
