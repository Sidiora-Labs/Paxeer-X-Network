#!/usr/bin/env python3
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[3]
EVIDENCE = Path(os.environ.get("LAYERX_BOUNDARY_EVIDENCE", "/root/lx-ops/paxeer-x-integration-2026-10-03/qualification/task1043516"))

def main():
    manifest = json.loads((EVIDENCE / "source.json").read_text())
    for relative, expected in manifest["sources"].items():
        actual = hashlib.sha256((ROOT / relative).read_bytes()).hexdigest()
        if actual != expected:
            raise RuntimeError("source changed after build admission: " + relative)
    build = (EVIDENCE / "build.log").read_text()
    matches = re.findall(r"Executable tests/boundary_witness\.rs \(([^)]+)\)", build)
    if len(matches) != 1 or not (EVIDENCE / "build.exit").read_text().strip() == "0":
        raise RuntimeError("genuine successful no-run build artifact required")
    binary = Path(matches[0])
    if not binary.is_absolute():
        binary = ROOT / binary
    if not binary.is_file():
        raise RuntimeError("no-run test executable missing")
    with (EVIDENCE / "verify.log").open("w") as log:
        result = subprocess.run([str(binary), "--nocapture", "--test-threads=1"], cwd=ROOT, stdout=log, stderr=subprocess.STDOUT, timeout=540)
    (EVIDENCE / "verify.exit").write_text(str(result.returncode) + "\n")
    print("exit=" + str(result.returncode) + " log=" + str(EVIDENCE / "verify.log"))
    return result.returncode

if __name__ == "__main__":
    try:
        sys.exit(main())
    except (OSError, ValueError, RuntimeError, subprocess.TimeoutExpired) as error:
        print(str(error), file=sys.stderr)
        sys.exit(1)
