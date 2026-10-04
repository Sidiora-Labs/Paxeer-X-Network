#!/usr/bin/env python3
import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[3]
EVIDENCE = Path(os.environ.get("LAYERX_MARKET_SANDBOX_EVIDENCE", "/root/lx-ops/paxeer-x-integration-2026-10-03/qualification/task1043533"))
DECLARED = (
    "programs/sdk/rust/src/arbiter.rs",
    "programs/crates/layerx-programs-market/src/arbitration.rs",
    "programs/crates/layerx-programs-runtime/src/execute.rs",
    "programs/crates/layerx-programs-runtime/src/replay.rs",
    "programs/crates/layerx-programs-runtime/src/abi/host_state.rs",
    "programs/crates/layerx-programs-runtime/src/host/mod.rs",
    "programs/crates/layerx-programs-arbiter/src/market.rs",
    "programs/crates/layerx-programs-arbiter/tests/market_sandbox_profile.rs",
    "tools/qualification/paxeer-x/market-sandbox-profile.py",
)


def sha(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def sources():
    output = subprocess.run([
        "git", "ls-files", "-z", "--", "programs", "agent/crates/layerx-client",
        "agent/crates/layerx-proof", "agent/crates/layerx-wire", "agent/crates/layerx-types",
        "agent/crates/layerx-crypto",
    ], cwd=ROOT, check=True, capture_output=True).stdout
    names = {os.fsdecode(value) for value in output.split(b"\0") if value} | set(DECLARED)
    return {name: sha(ROOT / name) for name in sorted(names) if Path(name).suffix in {".rs", ".toml", ".lock", ".json", ".py"}}


def execute(command, log, environment, timeout):
    with log.open("w") as stream:
        try:
            result = subprocess.run(command, cwd=ROOT, env=environment, stdout=stream, stderr=subprocess.STDOUT, timeout=timeout)
        except subprocess.TimeoutExpired:
            log.with_suffix(log.suffix + ".exit").write_text("124\n")
            print("exit=124 log=" + str(log), flush=True)
            raise
    log.with_suffix(log.suffix + ".exit").write_text(str(result.returncode) + "\n")
    print("exit=" + str(result.returncode) + " log=" + str(log), flush=True)
    if result.returncode:
        raise subprocess.CalledProcessError(result.returncode, command)
    return log.read_text()


def artifact(output):
    paths = set()
    for line in output.splitlines():
        if line.startswith("{"):
            item = json.loads(line)
            if item.get("reason") == "compiler-artifact" and item.get("target", {}).get("name") == "market_sandbox_profile" and item.get("executable"):
                paths.add(item["executable"])
    if len(paths) != 1:
        raise RuntimeError("actual focused Market profile test artifact missing")
    return str(Path(paths.pop()).resolve(strict=True))


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--build", action="store_true")
    args = parser.parse_args()
    EVIDENCE.mkdir(mode=0o700, parents=True, exist_ok=True)
    if EVIDENCE.stat().st_mode & 0o077:
        raise RuntimeError("private evidence required")
    environment = dict(os.environ)
    environment["PATH"] = "/root/.cargo/bin:" + environment.get("PATH", "")
    environment["CARGO_BUILD_JOBS"] = "4"
    environment["CARGO_TARGET_DIR"] = environment.get("LAYERX_MARKET_SANDBOX_TARGET", "/root/lx-target/arbiter-prestate/rust")
    if args.build:
        inputs = sources()
        output = execute([
            "/root/.cargo/bin/cargo", "test", "--locked", "--manifest-path", "programs/Cargo.toml",
            "-p", "layerx-programs-arbiter", "--test", "market_sandbox_profile", "--no-run", "--message-format=json",
        ], EVIDENCE / "build.log", environment, 1140)
        binary = artifact(output)
        if inputs != sources():
            raise RuntimeError("source changed during single Market profile build")
        (EVIDENCE / "artifacts.json").write_text(json.dumps({
            "revision": subprocess.run(["git", "rev-parse", "HEAD"], cwd=ROOT, check=True, capture_output=True, text=True).stdout.strip(),
            "inputs": inputs, "binary": {"path": binary, "sha256": sha(binary)},
        }, indent=2))
        return 0
    manifest = json.loads((EVIDENCE / "artifacts.json").read_text())
    if manifest["inputs"] != sources() or sha(manifest["binary"]["path"]) != manifest["binary"]["sha256"]:
        raise RuntimeError("Market profile source/artifact mismatch")
    execute([manifest["binary"]["path"], "--nocapture", "--test-threads=1"], EVIDENCE / "verify.log", environment, 540)
    if manifest["inputs"] != sources():
        raise RuntimeError("source changed during Market profile qualification")
    fixture = Path(environment["LAYERX_MARKET_SANDBOX_INPUTS"]).resolve(strict=True)
    (EVIDENCE / "result.json").write_text(json.dumps({
        "revision": manifest["revision"],
        "command": "timeout 10m python3 tools/qualification/paxeer-x/market-sandbox-profile.py", "exit": 0,
        "fixture_manifest": str(fixture), "fixture_sha256": sha(fixture), "log": str(EVIDENCE / "verify.log"),
    }, indent=2))
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (OSError, ValueError, KeyError, RuntimeError, subprocess.SubprocessError) as error:
        print(str(error), file=sys.stderr)
        sys.exit(1)
