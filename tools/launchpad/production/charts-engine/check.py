#!/usr/bin/env python3
"""Exact chart primitive qualification; requires an authorized PostgreSQL fixture."""
import os
from pathlib import Path
import subprocess
import sys
root = Path(__file__).resolve().parents[4]
if not os.environ.get('L10_TEST_POSTGRES'):
    sys.exit('L10_TEST_POSTGRES is required; transactional proof cannot be skipped')
env = dict(os.environ, GOWORK='off', GOMAXPROCS='2', GOFLAGS='-mod=readonly')
sys.exit(subprocess.call(['go', 'test', '-p', '1', '-count=1', '-json', './internal/engine', '-run', '^TestExact'], cwd=root / 'services/launchpad/trading-charts', env=env))
