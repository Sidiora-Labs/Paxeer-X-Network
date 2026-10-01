#!/usr/bin/env python3
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[4]
sys.dont_write_bytecode = True
sys.path.insert(0, str(ROOT / "tests/daemon"))
from custody_chain import artifact_manifest


def main():
    if os.geteuid() != 0:
        raise RuntimeError("real native UID separation requires root")
    artifact_manifest(os.environ["LAYERX_CUSTODY_ARTIFACT_MANIFEST"])
    manifest = Path(os.environ["PAXEER_X_CORE_BUILD_MANIFEST"])
    info = manifest.stat()
    if info.st_uid != os.geteuid() or info.st_mode & 0o077:
        raise RuntimeError("core build manifest must be private")
    bound = json.loads(manifest.read_text())
    revision = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=ROOT, text=True).strip()
    if bound["source_revision"] != revision or bound["build_exit"] != 0:
        raise RuntimeError("core build source mismatch")
    for name in ("layerxd", "layerx-genesis-build", "layerx-handover", "layerx-core-boundary"):
        row = bound["artifacts"][name]
        path = Path(row["path"])
        with path.open("rb") as stream:
            digest = hashlib.file_digest(stream, "sha256").hexdigest()
        if digest != row["sha256"] or not os.access(path, os.X_OK):
            raise RuntimeError("built executable mismatch: " + name)
    environment = dict(os.environ, LAYERX_TEST_NATIVE_BIN_DIR=str(Path(bound["artifacts"]["layerxd"]["path"]).parent),
                       LAYERX_TEST_PYTHON=sys.executable, PYTHONDONTWRITEBYTECODE="1")
    cargo = ["rustup", "run", "1.91.1", "cargo"]
    command = [*cargo, "test", "--locked", "--manifest-path", "platform/Cargo.toml", "-p", "layerx-platform-core", "--", "--test-threads=1"]
    result = subprocess.run(["unshare", "--net", "--mount", "--pid", "--fork", "--mount-proc", "bash", "-c",
        'set -e; mount --make-rprivate /; ip link set lo up; exec "$@"', "core-real-suite", *command],
        cwd=ROOT, env=environment, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)
    print(result.stdout, end="", flush=True)
    if result.returncode:
        return result.returncode
    cases = re.findall(r"test result: ok\. (\d+) passed; 0 failed; (\d+) ignored;", result.stdout)
    if not cases or sum(int(passed) for passed, _ in cases) == 0 or any(int(skipped) for _, skipped in cases):
        raise RuntimeError("empty or skipped core acceptance")
    lint = subprocess.run([*cargo, "clippy", "--locked", "--manifest-path", "platform/Cargo.toml", "-p", "layerx-platform-core", "--all-targets", "--", "-D", "warnings"], cwd=ROOT, env=environment)
    if lint.returncode:
        return lint.returncode
    print("PAXEER_X_GATE tests=" + str(sum(int(passed) for passed, _ in cases)) + " skipped=0")
    return 0


if __name__ == "__main__":
    sys.exit(main())
