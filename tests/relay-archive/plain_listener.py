#!/usr/bin/env python3
import hashlib
import json
import os
from pathlib import Path
import socket
import ssl
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.request

ROOT = Path(__file__).resolve().parents[2]
BUILD = Path(sys.argv[1] if len(sys.argv) > 1 else ROOT / "build").resolve()
sys.path.insert(0, str(ROOT / "tests/support"))
from lxgb_metadata import metadata
from cryptography.hazmat.primitives import serialization
from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey

RUNTIME = ROOT / "platform/relay_archive/runtime.py"


def require(condition, message):
    if not condition:
        raise AssertionError(message)


def unused_port():
    with socket.socket() as connection:
        connection.bind(("127.0.0.1", 0))
        return connection.getsockname()[1]


def execute(args, env=None):
    result = subprocess.run([str(arg) for arg in args], env=env, stdout=subprocess.PIPE,
                            stderr=subprocess.PIPE, timeout=120, cwd=ROOT)
    if result.returncode:
        raise AssertionError(f"{Path(args[0]).name} failed ({result.returncode}): "
                             + result.stderr.decode(errors="replace")[-6000:])
    return result


def env_file(path):
    return dict(line.split("=", 1) for line in path.read_text().splitlines() if line)


def genesis(work):
    for name, seed in (("sequencer", 0x22), ("treasury", 0x11)):
        path = work / name
        path.write_bytes(bytes([seed]) * 32)
        path.chmod(0o600)
    issuer = Ed25519PrivateKey.from_private_bytes(bytes([0x11]) * 32).public_key()
    issuer = issuer.public_bytes(serialization.Encoding.Raw, serialization.PublicFormat.Raw)
    asset = bytes.fromhex("b5a32b12029f8ddfb905f90f280f664b46390de0fc62770fc197dd87b18cd898")
    (work / "metadata").write_bytes(metadata(asset, issuer, os.urandom(32)))
    data = work / "native"
    environment = dict(os.environ,
        LAYERX_NODE_PAXEER_CHAIN_ID="31337",
        LAYERX_NODE_SETTLEMENT_CONTRACT="0x0000000000000000000000000000000000001014",
        LAYERX_NODE_CHECKPOINT_REGISTRY="0x0000000000000000000000000000000000001014",
        LAYERX_NODE_PAXEER_RPC_ADDRESS="127.0.0.1",
        LAYERX_NODE_PAXEER_RPC_PORT=str(unused_port()))
    execute(["bash", ROOT / "platform/hosted/node/bootstrap.sh",
             "--data-dir", data, "--run-dir", work / "run", "--network-id", "77",
             "--genesis-metadata", work / "metadata",
             "--sequencer-key", work / "sequencer", "--treasury-key", work / "treasury",
             "--lni-uid", "4021", "--lni-gid", "4021", "--program-port", str(unused_port()),
             "--replica-port", str(unused_port()), "--layerxd", BUILD / "bin/layerxd",
             "--genesis-build", BUILD / "bin/layerx-genesis-build"], env=environment)
    node = env_file(data / "node.env")
    manifest = data / "genesis/genesis.manifest"
    return {
        "network_id": 77,
        "genesis_sha256": hashlib.sha256(manifest.read_bytes()).hexdigest(),
        "sequencer_id": node["LAYERX_NODE_SEQUENCER_ID"],
        "sequencer_public_key": node["LAYERX_NODE_SEQUENCER_PUBLIC_KEY"],
        "genesis_manifest": str(manifest),
        "genesis_snapshot": str(data / "genesis/00000000000000000000.lxs"),
        "codec": str(BUILD / "bin/layerx-archive-codec"),
        "poll_interval_seconds": 0.2,
    }


def configure(work, pins, name, scheme, **extra):
    port = unused_port()
    config = dict(pins, data_dir=str(work / name), listen=f"127.0.0.1:{port}",
                  public_url=f"{scheme}://127.0.0.1:{port}", **extra)
    path = work / (name + ".json")
    path.write_text(json.dumps(config, sort_keys=True) + "\n")
    return path, f"{scheme}://127.0.0.1:{port}"


def ready(path, url, context):
    process = subprocess.Popen([sys.executable, RUNTIME, "--config", path],
                               stdout=subprocess.DEVNULL, stderr=subprocess.PIPE)
    try:
        deadline = time.monotonic() + 60
        last = None
        while time.monotonic() < deadline:
            if process.poll() is not None:
                raise AssertionError("relay archive exited: "
                                     + process.stderr.read().decode(errors="replace")[-4000:])
            try:
                with urllib.request.urlopen(url + "/readyz", timeout=5, context=context) as response:
                    status = json.loads(response.read())
                    require(response.status == 200 and status["ready"] is True, f"not ready: {status}")
                    return
            except (OSError, urllib.error.URLError) as error:
                last = error
            time.sleep(0.1)
        raise AssertionError(f"timed out waiting for {url}/readyz; last error: {last}")
    finally:
        process.terminate()
        try:
            process.wait(timeout=10)
        except subprocess.TimeoutExpired:
            process.kill()
            process.wait()


def main():
    with tempfile.TemporaryDirectory(prefix="relay-plain-") as directory:
        work = Path(directory)
        pins = genesis(work)
        path, url = configure(work, pins, "plain", "http", listener="plain")
        ready(path, url, None)
        execute(["openssl", "req", "-x509", "-newkey", "rsa:2048", "-nodes",
                 "-keyout", work / "tls.key", "-out", work / "tls.crt", "-days", "1",
                 "-subj", "/CN=localhost", "-addext", "subjectAltName=DNS:localhost,IP:127.0.0.1"])
        context = ssl.create_default_context(cafile=str(work / "tls.crt"))
        tls = {"tls_cert": str(work / "tls.crt"), "tls_key": str(work / "tls.key")}
        path, url = configure(work, pins, "tls", "https", listener="tls", **tls)
        ready(path, url, context)
        path, url = configure(work, pins, "absent", "https", **tls)
        ready(path, url, context)
        for name, extra in (("tls_cert", {"tls_cert": tls["tls_cert"]}),
                            ("tls_key", {"tls_key": tls["tls_key"]})):
            path, _ = configure(work, pins, "refused-" + name, "http", listener="plain", **extra)
            result = subprocess.run([sys.executable, RUNTIME, "--config", path, "--once"],
                                    stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=60)
            error = result.stderr.decode(errors="replace")
            require(result.returncode != 0 and f"{name} is set with listener plain" in error,
                    f"plain listener with {name} was not refused: {result.returncode} {error}")
        path, _ = configure(work, pins, "unknown", "http", listener="quic")
        result = subprocess.run([sys.executable, RUNTIME, "--config", path, "--once"],
                                stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=60)
        require(result.returncode != 0 and "listener must be tls or plain" in result.stderr.decode(),
                "unknown listener mode was not refused")
    print("relay archive plain listener: plain, tls and refusal paths passed")


if __name__ == "__main__":
    main()
