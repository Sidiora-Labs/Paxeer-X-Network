#!/usr/bin/env python3
"""Focused gate: escrow replay success is bound to the authorized operation.

Builds and runs the real escrow module tests (routed capture/replay binding,
restart, release/timeout and epoch sweep replay) against the production
library. Fails on any missing prerequisite; never substitutes results."""
import os
import shutil
import subprocess
import sys

ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
TARGETS = ["test-escrow-open", "test-escrow-capture", "test-escrow-timeout",
           "test-escrow-dispute"]


def revision():
    out = subprocess.run(["git", "-C", ROOT, "rev-parse", "HEAD"],
                         capture_output=True, text=True)
    return out.stdout.strip() if out.returncode == 0 else "unknown"


def main():
    evidence = os.environ.get("PAXEER_X_EVIDENCE_DIR", "")
    for tool in ("make", "cc"):
        if shutil.which(tool) is None:
            print(f"missing prerequisite: {tool}", file=sys.stderr)
            return 2
    command = ["make", "-C", ROOT] + TARGETS
    print(f"revision={revision()}")
    print(f"command={' '.join(command)}")
    result = subprocess.run(command)
    print(f"exit_code={result.returncode}")
    print(f"evidence={evidence or 'unset'}")
    if result.returncode != 0:
        return 1
    print(f"PAXEER_X_GATE tests={len(TARGETS)} skipped=0")
    return 0


if __name__ == "__main__":
    sys.exit(main())
