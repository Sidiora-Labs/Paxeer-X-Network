#!/usr/bin/env bash
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)
work=$(mktemp -d)
trap 'rm -rf -- "$work"' EXIT
umask 077
mkdir "$work/bin"
if [ -d /usr/local/go/bin ]; then PATH="/usr/local/go/bin:$PATH"; fi
if ! (cd "$root" && go build -p 2 -o "$work/bin/paxd" ./daemon/paxd); then
    echo 'FAIL archive_holds_whole_data: cannot build real paxd'
    echo 'snapshot: 0 passed, 1 failed'
    exit 1
fi
python3 - "$root" "$work" <<'PY'
import hashlib
import json
import os
from pathlib import Path
import re
import shlex
import shutil
import socket
import subprocess
import sys
import tempfile
import time
import tomllib

root, work = map(Path, sys.argv[1:])
paxd = work / "bin/paxd"
script = root / "tools/release/snapshot.sh"
validator, fullnode = work / "validator", work / "fullnode"
snapshots = work / "snapshots"
env_file = work / "rollout.env"
receipt = work / "receipt.json"
processes = []
reservations = []
logs = []
# Far enough past genesis for the full node to leave block sync and receive H by consensus.
HALT = 50


def run(*args, **kwargs):
    return subprocess.run([str(arg) for arg in args], check=True, **kwargs)


def daemon(home, *args):
    return run(
        paxd, *args, "--home", home, stdout=subprocess.PIPE, stderr=subprocess.PIPE
    )


def set_toml(path, section, key, value):
    text = path.read_text()
    current = ""
    lines = []
    changed = 0
    for line in text.splitlines():
        if line.startswith("["):
            current = line.strip()[1:-1]
        if current == section and re.match(rf"^{re.escape(key)}\s*=", line):
            line = f"{key} = {json.dumps(value)}"
            changed += 1
        lines.append(line)
    assert changed == 1, (path.name, section, key, changed)
    path.write_text("\n".join(lines) + "\n")
    tomllib.loads(path.read_text())


def port():
    reservation = socket.socket()
    reservation.bind(("127.0.0.1", 0))
    reservations.append(reservation)
    return reservation.getsockname()[1]


def write_env(validators=None, fullnodes=None, directory=None, height=str(HALT)):
    values = {
        "VALIDATOR_HOMES": str(validator) if validators is None else validators,
        "FULLNODE_HOMES": str(fullnode) if fullnodes is None else fullnodes,
        "SNAPSHOT_DIR": str(snapshots if directory is None else directory),
        "HALT_HEIGHT": height,
    }
    env_file.write_text(
        "\n".join(f"{key}={shlex.quote(value)}" for key, value in values.items()) + "\n"
    )


def snapshot(*args, expected=0, message=None, count=None):
    result = subprocess.run(
        [
            "bash",
            str(script),
            "--env-file",
            str(env_file),
            "--receipt",
            str(receipt),
            *args,
        ],
        capture_output=True,
        text=True,
    )
    assert result.returncode == expected, (
        result.returncode,
        result.stdout,
        result.stderr,
    )
    if message is not None:
        assert message in result.stderr, result.stderr
    value = json.loads(receipt.read_text())
    assert set(value) == {"homes_archived", "result"}, value
    assert value["result"] == ("pass" if expected == 0 else "fail"), value
    if count is not None:
        assert value["homes_archived"] == count, value
    assert receipt.stat().st_mode & 0o777 == 0o600
    return result


def manifest(directory):
    result = {}
    for entry in sorted(directory.rglob("*")):
        relative = str(entry.relative_to(directory))
        if entry.is_symlink():
            result[relative] = ("symlink", os.readlink(entry))
        else:
            result[relative] = (
                (
                    "directory"
                    if entry.is_dir()
                    else hashlib.sha256(entry.read_bytes()).hexdigest()
                ),
                entry.stat().st_mode & 0o777,
            )
    return result


def archive_holds_whole_data():
    chain = "hyperpax_125-1"
    for home, mode in ((validator, "validator"), (fullnode, "full")):
        daemon(home, "init", home.name, "--chain-id", chain, "--mode", mode)
    daemon(
        validator,
        "keys",
        "add",
        "validator",
        "--keyring-backend",
        "test",
        "--output",
        "json",
    )
    daemon(
        validator,
        "add-genesis-account",
        "validator",
        "100000000000000000000uhpx",
        "--keyring-backend",
        "test",
    )
    validator_p2p, fullnode_p2p = port(), port()
    daemon(
        validator,
        "gentx",
        "validator",
        "7000000000000000uhpx",
        "--chain-id",
        chain,
        "--keyring-backend",
        "test",
        "--ip",
        "127.0.0.1",
        "--p2p-port",
        validator_p2p,
    )
    genesis_path = validator / "config/genesis.json"
    genesis = json.loads(genesis_path.read_text())
    public_key = json.loads((validator / "config/priv_validator_key.json").read_text())[
        "pub_key"
    ]
    genesis["validators"] = [{"power": "7000000000", "pub_key": public_key}]
    genesis["app_state"]["staking"]["params"][
        "max_voting_power_ratio"
    ] = "1.000000000000000000"
    genesis["consensus_params"]["timeout"]["commit"] = "500000000"
    genesis["consensus_params"]["timeout"]["bypass_commit_timeout"] = False
    genesis_path.write_text(json.dumps(genesis))
    daemon(validator, "collect-gentxs")
    daemon(validator, "validate-genesis")
    shutil.copyfile(genesis_path, fullnode / "config/genesis.json")
    node_id = daemon(validator, "tendermint", "show-node-id").stdout.decode().strip()
    for home, p2p in ((validator, validator_p2p), (fullnode, fullnode_p2p)):
        config, app = home / "config/config.toml", home / "config/app.toml"
        set_toml(config, "rpc", "laddr", f"tcp://127.0.0.1:{port()}")
        set_toml(config, "p2p", "laddr", f"tcp://127.0.0.1:{p2p}")
        set_toml(config, "p2p", "external-address", "")
        set_toml(config, "p2p", "pex", False)
        set_toml(config, "p2p", "allow-duplicate-ip", True)
        if home == fullnode:
            set_toml(
                config,
                "p2p",
                "persistent-peers",
                f"{node_id}@127.0.0.1:{validator_p2p}",
            )
        for section in ("api", "grpc", "grpc-web"):
            set_toml(app, section, "enable", False)
        set_toml(app, "evm", "http_enabled", False)
        set_toml(app, "evm", "ws_enabled", False)
        set_toml(app, "", "minimum-gas-prices", "0.01uhpx")
    for reservation in reservations:
        reservation.close()
    write_env()
    for home in (fullnode, validator):
        output = (work / f"{home.name}.log").open("w")
        logs.append(output)
        processes.append(
            subprocess.Popen(
                [str(paxd), "start", "--home", str(home), "--halt-height", str(HALT)],
                stdout=output,
                stderr=output,
            )
        )
    deadline = time.monotonic() + 180
    while not all(
        (home / "data/state_commit/memiavl").is_dir() for home in (validator, fullnode)
    ):
        assert all(
            p.poll() is None for p in processes
        ), "node exited before opening application store"
        assert time.monotonic() < deadline, "node startup timeout"
        time.sleep(0.1)
    snapshot(expected=1, message="node still running", count=0)
    for process in processes:
        assert (
            process.wait(timeout=max(1, deadline - time.monotonic())) == 0
        ), "node did not halt cleanly"
    for home in (validator, fullnode):
        text = (work / f"{home.name}.log").read_text()
        assert re.search(
            rf"halting node per configuration[^\n]*height[=\": ]+{HALT}\b", text
        ), text
    state = json.loads((validator / "data/priv_validator_state.json").read_text())
    assert state["height"] == str(HALT), state
    snapshot(count=2)
    snapshot("--verify", count=2)
    for home in (validator, fullnode):
        archive = snapshots / f"{home.name}-pre-v6.12.0.tar.zst"
        assert archive.stat().st_mode & 0o777 == 0o600
        with tempfile.TemporaryDirectory(dir=work) as restore:
            run("tar", "--zstd", "-xf", archive, "-C", restore)
            assert manifest(home / "data") == manifest(Path(restore) / "data")
            assert sorted(p.name for p in Path(restore).iterdir()) == ["data"]
    before = manifest(snapshots)
    snapshot(expected=1, message="snapshot already exists", count=0)
    assert manifest(snapshots) == before


def refuses_incomplete_archives():
    bad = work / "incomplete"
    bad.mkdir()
    write_env(fullnodes="", directory=bad)
    archive = bad / "validator-pre-v6.12.0.tar.zst"
    for member, message in (
        (
            "data/priv_validator_state.json",
            "archive lacks data/priv_validator_state.json",
        ),
        ("data/state_commit", "archive lacks application store"),
    ):
        run(
            "tar",
            "--zstd",
            "--exclude=" + member,
            "-cf",
            archive,
            "-C",
            validator,
            "data",
        )
        snapshot("--verify", expected=1, message=message, count=0)
    archive.write_bytes(b"invalid zstd archive")
    snapshot("--verify", expected=1, count=0)
    archive.unlink()
    snapshot("--verify", expected=1, message="missing snapshot archive", count=0)


def refuses_unsafe_inputs():
    write_env(validators="", fullnodes="")
    snapshot(expected=1, message="no validator or full-node homes", count=0)
    write_env(fullnodes=str(validator))
    snapshot(expected=1, message="duplicate home archive name", count=0)
    write_env(directory=validator / "data/snapshots")
    snapshot(expected=1, message="SNAPSHOT_DIR must be outside", count=0)
    write_env(height=str(HALT - 1), directory=work / "wrong-height")
    snapshot(
        expected=1, message="validator signing state is not at HALT_HEIGHT", count=0
    )
    assert not (work / "wrong-height").exists()


def accepts_single_role():
    for validators, fullnodes, name in (
        (str(validator), "", "validator-only"),
        ("", str(fullnode), "fullnode-only"),
    ):
        write_env(validators=validators, fullnodes=fullnodes, directory=work / name)
        snapshot(count=1)
        snapshot("--verify", count=1)


passed = 0
try:
    for test in (
        archive_holds_whole_data,
        refuses_incomplete_archives,
        refuses_unsafe_inputs,
        accepts_single_role,
    ):
        test()
        print(f"PASS {test.__name__}", flush=True)
        passed += 1
except Exception as error:
    print(f"FAIL {test.__name__}: {error}", file=sys.stderr)
    if isinstance(error, subprocess.CalledProcessError):
        print(error.stdout, error.stderr, file=sys.stderr)
    for home in (validator, fullnode):
        path = work / f"{home.name}.log"
        if path.exists():
            print(path.read_text(), file=sys.stderr)
    print(f"snapshot: {passed} passed, 1 failed")
    sys.exit(1)
finally:
    for process in processes:
        if process.poll() is None:
            process.terminate()
            try:
                process.wait(timeout=15)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait()
    for output in logs:
        output.close()
    for reservation in reservations:
        reservation.close()
print(f"snapshot: {passed} passed, 0 failed")
PY
