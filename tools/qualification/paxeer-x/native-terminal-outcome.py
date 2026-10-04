#!/usr/bin/env python3
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[3]
SOURCE = (
    "agent/crates/layerx-proof/src/receipt.rs",
    "agent/crates/layerx-proof/tests/native_terminal_outcome.rs",
    "tools/qualification/paxeer-x/native-terminal-outcome.py",
)
REQUIRED = {"serial-empty-0", "scheduled-0", "scheduled-1", "terminal-0"}


def digest(path):
    with Path(path).open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def main():
    inventory = Path(os.environ["LAYERX_ARBITER_PRESTATE_INPUTS"]).resolve(strict=True)
    manifest = json.loads(inventory.read_text())
    captures = manifest["captures"]
    if len(captures) != 4 or {item["name"] for item in captures} != REQUIRED:
        raise RuntimeError("all genuine serial, scheduled and terminal native captures required")
    evidence = Path(os.environ.get("PAXEER_X_NATIVE_TERMINAL_EVIDENCE",
                    "/root/lx-ops/paxeer-x-integration-2026-10-03/qualification/task1043514"))
    evidence.mkdir(mode=0o700, parents=True, exist_ok=True)
    evidence = evidence.resolve(strict=True)
    info = evidence.stat()
    if ROOT == evidence or ROOT in evidence.parents or info.st_uid != os.geteuid() or info.st_mode & 0o077:
        raise RuntimeError("private owned evidence outside checkout required")
    paths = {inventory, *(ROOT / path for path in SOURCE)}
    pins = [manifest["sequencer_id"], manifest["sequencer_public_key"],
            str(manifest["first_batch_number"]), str(manifest["last_batch_number"])]
    lines = ["\t".join(pins)]
    for capture in captures:
        keys = ("receipt_path", "proof_path", "header_path", "header_signature_path",
                "maintenance_path", "maintenance_proof_path")
        files = [Path(capture[key]).resolve(strict=True) for key in keys]
        receipts = [Path(path).resolve(strict=True) for path in capture["receipts"]]
        if not receipts or len(receipts) > 64:
            raise RuntimeError("complete bounded native receipt chain required")
        for path in [*files, *receipts]:
            if not path.is_file() or inventory.parent not in path.parents:
                raise RuntimeError("fixture path must identify retained native capture bytes")
            if any(character in str(path) for character in "\t\n\r|"):
                raise RuntimeError("ambiguous native fixture path")
            paths.add(path)
        lines.append("\t".join([capture["name"], *map(str, files),
                                 "|".join(map(str, receipts)), str(len(receipts))]))
    before = {str(path): digest(path) for path in sorted(paths)}
    records = evidence / "retained-native-records.tsv"
    records.write_text("\n".join(lines) + "\n")
    records.chmod(0o600)
    environment = dict(os.environ, LAYERX_NATIVE_TERMINAL_RECORDS=str(records),
                       CARGO_TARGET_DIR="/root/lx-target/agent", CARGO_BUILD_JOBS="4")
    command = ["/root/.cargo/bin/cargo", "test", "--locked", "--manifest-path", "agent/Cargo.toml",
               "-p", "layerx-proof", "--test", "native_terminal_outcome", "--", "--test-threads=1"]
    log = evidence / "verify-rust.log"
    with log.open("w") as stream:
        result = subprocess.run(command, cwd=ROOT, env=environment, stdin=subprocess.DEVNULL,
                                stdout=stream, stderr=subprocess.STDOUT, timeout=1100)
    print("EXIT", result.returncode, "LOG", log, flush=True)
    if result.returncode:
        raise subprocess.CalledProcessError(result.returncode, command)
    if "test result: ok. 4 passed; 0 failed; 0 ignored; 0 measured; 0 filtered out" not in log.read_text():
        raise RuntimeError("all four genuine native outcome cases must execute")
    if before != {str(path): digest(path) for path in sorted(paths)}:
        raise RuntimeError("native evidence or authored source changed during qualification")
    record = {"task": "104.35.14", "command": command, "exit": 0, "inputs": before,
              "revision": subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=ROOT, text=True).strip(),
              "log": str(log)}
    output = evidence / "result.json"
    output.write_text(json.dumps(record, indent=2, sort_keys=True) + "\n")
    output.chmod(0o600)


if __name__ == "__main__":
    try:
        main()
    except (KeyError, OSError, ValueError, RuntimeError, subprocess.SubprocessError) as error:
        print("FAILED", str(error), file=sys.stderr)
        sys.exit(error.returncode if isinstance(error, subprocess.CalledProcessError) else 1)
