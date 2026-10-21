#!/usr/bin/env python3
"""Run exact L11 HTTP primitives; PostgreSQL fixture must be available."""
import os
from pathlib import Path
import subprocess
import sys

root = Path(__file__).resolve().parents[3]
env = dict(os.environ, GOWORK="off", GOMAXPROCS="2", GOFLAGS="-mod=readonly")
command = ["go", "test", "-p", "1", "-count=1", "-json", "./internal/httpapi", "-run", "Test(UDF|History|Resolution|Canonical|As)|^FuzzHistoryValidatedInputs$"]
try:
    result = subprocess.run(command, cwd=root / "core/trading-charts", env=env, timeout=240)
except subprocess.TimeoutExpired:
    sys.exit(124)
sys.exit(result.returncode)
