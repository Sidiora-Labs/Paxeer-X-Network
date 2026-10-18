#!/usr/bin/env python3
import os
from pathlib import Path
import subprocess
import sys
root = Path(__file__).resolve().parents[3]
faults = sys.argv[1:] == ['--faults']
if sys.argv[1:] and not faults:
    sys.exit('Only --faults is supported')
required = ('DELIVERY_TEST_DSN', 'DELIVERY_TEST_SCHEMA', 'L10_ARCHIVE_CHART_SCHEMA', 'L10_ARCHIVE_MANIFESTS', 'L10_ARCHIVE_POOL', 'L10_ARCHIVE_FROM', 'L10_ARCHIVE_TO', 'L10_ARCHIVE_EXPECTED_DIGEST', 'L10_TEST_RPC_URL', 'L10_TEST_S3_ENDPOINT', 'L10_TEST_S3_BUCKET', 'L10_TEST_S3_ACCESS_KEY_ID', 'L10_TEST_S3_SECRET_ACCESS_KEY')
if faults:
    required = ('DELIVERY_TEST_DSN', 'DELIVERY_TEST_SCHEMA', 'L10_TEST_RPC_URL')
missing = [name for name in required if not os.environ.get(name)]
if missing:
    sys.exit('Actual PostgreSQL, approved96-hour archive, RPC and object fixture required: ' + ', '.join(missing))
env = dict(os.environ, GOWORK='off', GOMAXPROCS='2', GOFLAGS='-mod=readonly')
sys.exit(subprocess.call(['go', 'test', '-p', '1', '-count=1', '-json', './internal/engine', './internal/store', './internal/consumer', '-timeout', '25m', '-run', '^TestFault' if faults else '^(TestSeries|TestArchive)'], cwd=root / 'core/trading-charts', env=env))
