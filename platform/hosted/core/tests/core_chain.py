#!/usr/bin/env python3
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import signal
import subprocess
import sys
import time

ROOT = Path(__file__).resolve().parents[4]
sys.path.insert(0, str(ROOT / "tests/daemon"))
from custody_chain import artifact_manifest, owned_chain
from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey
from cryptography.hazmat.primitives.serialization import Encoding, PublicFormat


def run(*arguments):
    subprocess.run([str(a) for a in arguments], cwd=ROOT, check=True)


def stop(_signal, _frame):
    raise SystemExit(0)


def main():
    work, network, seed_file, asset, treasury_file = sys.argv[1:]
    work = Path(work)
    bundle = artifact_manifest(os.environ["LAYERX_CUSTODY_ARTIFACT_MANIFEST"])
    os.environ["PAXD"] = bundle["executables"]["paxd"]["path"]
    os.environ["LAYERX_CUSTODY_PROOF_BIN"] = bundle["executables"]["layerx-custody-proof"]["path"]
    public = Ed25519PrivateKey.from_private_bytes(Path(seed_file).read_bytes()).public_key().public_bytes(Encoding.Raw, PublicFormat.Raw)
    authority = Ed25519PrivateKey.generate().public_key().public_bytes(Encoding.Raw, PublicFormat.Raw)
    genesis = work / "custody-genesis.json"
    run(sys.executable, ROOT / "platform/hosted/paxeer/custody-genesis.py", "--network-id", network,
        "--sequencer-id", hashlib.sha256(b"layerx-sequencer:" + public.hex().encode()).hexdigest(),
        "--sequencer-public-key", public.hex(), "--deposit-root-authority", authority.hex(),
        "--asset", asset + ":uhpx", "--output", genesis)
    with owned_chain(work, Path(bundle["contract_directory"]), genesis) as chain:
        if treasury_file != "-":
            specification = importlib.util.spec_from_file_location("core_custody_export", ROOT / "tests/daemon/withdraw-custody.py")
            exporter = importlib.util.module_from_spec(specification)
            specification.loader.exec_module(exporter)
            exporter.ASSET = asset
            (work / "recipient.seed").write_bytes(os.urandom(32))
            for label, source, amount in (("custody", Path(treasury_file), 1000000), ("recipient", work / "recipient.seed", 100)):
                seed = source.read_bytes()
                key = Ed25519PrivateKey.from_private_bytes(seed).public_key().public_bytes(Encoding.Raw, PublicFormat.Raw)
                did = "did:layerx:" + key.hex()
                name = ("agent:" + did + ":main").encode()
                beneficiary = hashlib.sha256(b"LX:ACCOUNT:v1" + len(name).to_bytes(4, "big") + name).hexdigest()
                receipt = chain.transaction(exporter.calldata("deposit(bytes32)", "0x" + beneficiary), exporter.CUSTODY_ADDRESS, value=amount * 10 ** 12)
                logs = [entry for entry in receipt["logs"] if entry["address"].lower() == exporter.CUSTODY_ADDRESS.lower() and entry["topics"][0].lower() == exporter.DEPOSIT_TOPIC]
                assert len(logs) == 1 and len(logs[0]["topics"]) == 4
                assert bytes.fromhex(logs[0]["topics"][2][2:]) == bytes.fromhex(asset)
                data = bytes.fromhex(logs[0]["data"][2:])
                assert len(data) == 96 and data[:32].hex() == beneficiary and int.from_bytes(data[32:64], "big") == amount
                deadline = time.monotonic() + 30
                while int(chain.rpc("eth_blockNumber", []), 16) < int(receipt["blockNumber"], 16) + 2:
                    assert time.monotonic() < deadline
                    time.sleep(.1)
                comet = json.loads(chain.identity_path.read_text())["comet_url"]
                proof = bundle["executables"]["layerx-custody-proof"]["path"]
                if label == "custody":
                    run(proof, "light-profile", "--rpc", comet, "--asset", "0x" + asset,
                        "--network-id", network, "--trusted-height", "1", "--trusting-period-seconds", "1209600", "--output", work / "custody.profile")
                run(proof, "light-credit", "--rpc", comet, "--profile", work / "custody.profile",
                    "--deposit-id", logs[0]["topics"][1], "--owner-key", "0x" + key.hex(), "--output", work / (label + ".credit"))
                run(bundle["executables"]["sign-credit"]["path"], work / "custody.profile", work / (label + ".credit"),
                    did, source, "0", int(time.time() * 1000), work / (label + ".activity"))
        (work / "ready.json").write_text(json.dumps({"port": chain.port, "chain_id": 125}))
        while True:
            time.sleep(1)


if __name__ == "__main__":
    signal.signal(signal.SIGTERM, stop)
    signal.signal(signal.SIGINT, stop)
    main()
