#!/usr/bin/env python3
"""Owned disposable xweb fixture: a source-bound paxd chain with four real web attestors."""
from functools import partial
import hashlib
from http.server import SimpleHTTPRequestHandler, ThreadingHTTPServer
import importlib.util
import json
import os
from pathlib import Path
import secrets
import subprocess
import sys
import threading
import time

ROOT = Path(__file__).resolve().parents[4]
CHAIN_ID = 125
CONFIRMATIONS = 12
THRESHOLD = 3
ATTESTORS = 4
CALLBACK_GAS = 100_000
XWEB_PRECOMPILE = "0x" + "00" * 18 + "1019"
TEMPLATE = ROOT / "interop/crates/x-websearch/tests/fixtures/binary/config.json"
CHAIN_PORTS = ("EVM", "EVM_WS", "RPC", "P2P", "GRPC", "GRPC_WEB", "API")


def load(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    loaded = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(loaded)
    return loaded


runtime = load("paxeer_x_runtime_fixture", ROOT / "tests/daemon/paxeer_x_runtime_fixture.py")
evm = load("paxeer_evm", ROOT / "platform/hosted/paxeer/evm.py")


def require(condition, reason):
    if not condition:
        raise RuntimeError("xweb fixture refused: " + reason)


def private_write(path, text):
    descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(descriptor, "w", encoding="utf-8") as stream:
        stream.write(text)
    return path


def new_key(path):
    while True:
        secret = int.from_bytes(secrets.token_bytes(32), "big")
        if 0 < secret < evm.N:
            private_write(path, "0x%064x\n" % secret)
            return secret


def git(*argv):
    return subprocess.run(["git", *argv], cwd=ROOT, check=True, capture_output=True,
                          text=True).stdout.strip()


def produce(endpoint, key_path, url, fee):
    """Sends one real request(kind 1, url) from the requester and returns its id."""
    receipt = evm.Rpc(endpoint).send(
        evm.read_key(key_path), CHAIN_ID, XWEB_PRECOMPILE,
        evm.calldata("request(uint8,bytes,uint64)", 1, url.encode(), CALLBACK_GAS), fee)
    topic = "0x" + evm.keccak(
        b"XWebRequested(uint64,address,uint8,bytes,uint64,uint256,uint64)").hex()
    logs = [entry for entry in receipt.get("logs", [])
            if entry.get("address", "").lower() == XWEB_PRECOMPILE
            and entry.get("topics", [None])[0] == topic]
    require(len(logs) == 1, "request receipt carries no single XWebRequested log")
    return int(logs[0]["topics"][1], 16)


class Site(SimpleHTTPRequestHandler):
    def log_message(self, *args):
        pass


class XWebFixture:
    """paxd from the fresh canonical bundle with xweb genesis membership, and
    four x-websearch attestor configurations at threshold three."""

    def __init__(self, manifest, evidence):
        self.revision = manifest["source"]["revision"]
        require(git("rev-parse", "HEAD") == self.revision, "checkout is not the candidate revision")
        self.bundle = runtime.artifacts(os.environ.get("PAXEER_X_RUNTIME_ARTIFACTS", ""))
        require(self.bundle["source_revision"] == self.revision,
                "runtime bundle is bound to another source revision")
        binary = os.environ.get("PAXEER_X_XWEB_BINARY", "")
        require(binary, "PAXEER_X_XWEB_BINARY is not set")
        self.binary = Path(binary).resolve()
        require(self.binary.is_file() and os.access(self.binary, os.X_OK),
                "x-websearch binary absent or not executable")
        require(self.binary.stat().st_mtime >= int(git("log", "-1", "--format=%ct")),
                "x-websearch binary predates the candidate commit")
        self.binary_sha256 = runtime.digest(self.binary)
        self.directory = Path(evidence) / ("xweb-19.1-" + time.strftime("%Y%m%dT%H%M%SZ", time.gmtime()))
        self.directory.mkdir(mode=0o700)
        self.keys = self.directory / "keys"
        self.keys.mkdir(mode=0o700)
        self.processes, self.server = [], None
        self.env = {key: value for key, value in os.environ.items()
                    if not key.startswith(("LAYERX_", "PAXEER_X_", "X_WEBSEARCH_"))}
        self.env.update(HOME=str(self.directory), PYTHONDONTWRITEBYTECODE="1")
        self.env["LD_LIBRARY_PATH"] = ":".join(sorted(
            {str(Path(row["path"]).parent) for row in self.bundle.get("runtime_libraries", [])}))

    def paxd(self):
        return self.bundle["artifacts"]["paxd"]["path"]

    def run(self, label, argv, env=None, timeout=180):
        with (self.directory / (label + ".log")).open("ab") as log:
            done = subprocess.run([str(item) for item in argv], cwd=ROOT, env=self.env | (env or {}),
                                  stdout=log, stderr=log, timeout=timeout)
        require(done.returncode == 0, f"{label} exited {done.returncode}")

    def cast(self, signer_hex):
        answer = subprocess.run([self.paxd(), "debug", "addr", signer_hex, "--home", str(self.chain)],
                                env=self.env, capture_output=True, text=True, timeout=60)
        found = [line.removeprefix("Bech32 Acc: ").strip() for line in answer.stdout.splitlines()
                 if line.startswith("Bech32 Acc: ")]
        require(answer.returncode == 0 and len(found) == 1, "paxd debug addr gave no account")
        return found[0]

    def start(self):
        deployer = new_key(self.keys / "deployer.key")
        self.requester_key = self.keys / "requester.key"
        requester = new_key(self.requester_key)
        self.slots = []
        for index in range(ATTESTORS):
            slot = {"attestor": self.keys / f"attestor-{index + 1}.key",
                    "receiver": self.keys / f"receiver-{index + 1}.key"}
            slot["secret"] = new_key(slot["attestor"])
            new_key(slot["receiver"])
            if index == 0:
                slot["submitter"] = self.keys / "submitter-1.key"
                slot["submitter_secret"] = new_key(slot["submitter"])
            slot["signer"] = evm.address_of(slot["secret"]).hex()
            self.slots.append(slot)
        reservations = runtime.reserve_ports(len(CHAIN_PORTS) + ATTESTORS + 2)
        ports = [sock.getsockname()[1] for sock in reservations]
        self.endpoint = f"http://127.0.0.1:{ports[0]}"
        self.chain = self.directory / "chain"
        chain_env = {"PAXD": self.paxd(), "LAYERX_PAXEER_HOME": str(self.chain),
                     "LAYERX_PAXEER_CHAIN_ID": str(CHAIN_ID),
                     "LAYERX_PAXEER_DEPLOYER_ADDRESS": evm.checksum(evm.address_of(deployer)),
                     "LAYERX_PAXEER_COMMIT_TIMEOUT_NANOSECONDS": "1000000000"}
        for name, port in zip(CHAIN_PORTS, ports):
            chain_env["LAYERX_PAXEER_" + name + "_PORT"] = str(port)
        self.run("chain-init", ["bash", ROOT / "platform/hosted/paxeer/init-chain.sh"], chain_env)
        genesis_path = self.chain / "config/genesis.json"
        genesis = json.loads(genesis_path.read_text(encoding="utf-8"))
        section = genesis["app_state"].get("xweb")
        require(isinstance(section, dict) and "paused" in section and "attestors" in section,
                "genesis carries no xweb section")
        section["paused"] = False
        section["attestors"] = {
            "attestors": [{"signer": slot["signer"], "payout": self.cast(slot["signer"])}
                          for slot in self.slots],
            "threshold": THRESHOLD}
        genesis_path.write_text(json.dumps(genesis, indent=1) + "\n", encoding="utf-8")
        self.run("validate-genesis", [self.paxd(), "validate-genesis", genesis_path, "--home", self.chain])
        self.genesis_sha256 = runtime.digest(genesis_path)
        for sock in reservations:
            sock.close()
        log = (self.directory / "paxd.log").open("ab")
        self.processes.append(subprocess.Popen(
            [self.paxd(), "start", "--home", str(self.chain), "--consensus.create-empty-blocks-interval=1s"],
            cwd=ROOT, env=self.env | chain_env, stdin=subprocess.DEVNULL, stdout=log, stderr=log,
            start_new_session=True))
        self.rpc = evm.Rpc(self.endpoint)
        self.wait(lambda: self.rpc.request("eth_chainId", []) == hex(CHAIN_ID), "the owned chain")
        self.membership = self.read_membership()
        self.fee = self.rpc.eth_call(XWEB_PRECOMPILE, "fee()(uint256)")[0]
        require(self.fee > 0, "xweb fee is zero")
        funding = self.fee * 64
        self.funded = {}
        for name, secret in (("requester", requester), ("submitter-1", self.slots[0]["submitter_secret"])):
            receipt = self.rpc.send(deployer, CHAIN_ID, evm.address_of(secret), value=funding)
            self.funded[name] = {"address": "0x" + evm.address_of(secret).hex(),
                                 "funding_receipt": receipt["transactionHash"]}
        site = self.directory / "site"
        site.mkdir(mode=0o700)
        (site / "robots.txt").write_text("User-agent: *\nAllow: /\n", encoding="utf-8")
        (site / "page.html").write_text(
            "<html><head><title>Paxeer X web attestation</title></head><body><p>"
            "Every attestor fetches this owned page independently and signs its digest."
            "</p></body></html>\n", encoding="utf-8")
        self.server = ThreadingHTTPServer(("127.0.0.1", ports[-1]), partial(Site, directory=str(site)))
        threading.Thread(target=self.server.serve_forever, daemon=True).start()
        self.url = f"http://127.0.0.1:{ports[-1]}/page.html"
        self.write_configs(ports[len(CHAIN_PORTS):len(CHAIN_PORTS) + ATTESTORS], ports[-2])

    def wait(self, probe, what, seconds=180):
        end = time.monotonic() + seconds
        while time.monotonic() < end:
            require(all(process.poll() is None for process in self.processes), "paxd exited")
            try:
                if probe():
                    return
            except (OSError, ValueError):
                pass
            time.sleep(0.5)
        raise RuntimeError("xweb fixture refused: timed out waiting for " + what)

    def read_membership(self):
        """getAttestors() and threshold() read natively from 0x1019."""
        answer = evm.unhex(self.rpc.request(
            "eth_call", [{"to": XWEB_PRECOMPILE,
                          "data": "0x" + evm.selector("getAttestors()").hex()}, "latest"]))
        word = lambda at: int.from_bytes(answer[at:at + 32], "big")
        start = word(0)
        count = word(start)
        signers = [answer[start + 32 + word(start + 32 + 32 * index) + 12:
                          start + 32 + word(start + 32 + 32 * index) + 32].hex()
                   for index in range(count)]
        threshold = self.rpc.eth_call(XWEB_PRECOMPILE, "threshold()(uint32)")[0]
        require(sorted(signers) == sorted(slot["signer"] for slot in self.slots),
                "native attestor set differs from the generated keys")
        require(word(32) == threshold == THRESHOLD, "native threshold is not 3 of 4")
        return {"signers": sorted(signers), "threshold": threshold}

    def write_configs(self, listen_ports, gateway_port):
        template = json.loads(TEMPLATE.read_text(encoding="utf-8"))
        peers = [f"http://127.0.0.1:{port}" for port in listen_ports]
        for index, slot in enumerate(self.slots):
            data_dir = self.directory / f"attestor-{index + 1}"
            data_dir.mkdir(mode=0o700)
            config = json.loads(json.dumps(template))
            config.update(listen=f"127.0.0.1:{listen_ports[index]}", data_dir=str(data_dir),
                          seeds=[self.url], crawl_interval_seconds=86_400,
                          peers=[peer for position, peer in enumerate(peers) if position != index])
            config["gateway"]["endpoint"] = f"http://127.0.0.1:{gateway_port}/rpc"
            config["evm"] = {"endpoint": self.endpoint, "chain_id": CHAIN_ID,
                             "confirmations": CONFIRMATIONS}
            slot["config"] = private_write(self.directory / f"attestor-{index + 1}.json",
                                           json.dumps(config, indent=2) + "\n")
            handles = {"X_WEBSEARCH_ATTESTOR_KEY_FILE": str(slot["attestor"]),
                       "X_WEBSEARCH_RECEIVER_KEY_FILE": str(slot["receiver"])}
            if "submitter" in slot:
                handles["X_WEBSEARCH_SUBMITTER_KEY_FILE"] = str(slot["submitter"])
            slot["handles"] = private_write(self.keys / f"attestor-{index + 1}.handles.json",
                                            json.dumps(handles) + "\n")
            slot["data_dir"], slot["peer_endpoint"] = data_dir, peers[index]

    def config_digest(self):
        combined = hashlib.sha256()
        for slot in self.slots:
            combined.update(bytes.fromhex(runtime.digest(slot["config"])))
        combined.update(bytes.fromhex(self.genesis_sha256))
        return "sha256:" + combined.hexdigest()

    def bindings(self):
        """xweb-attestors bindings this fixture authenticated itself."""
        return {"source_revision": self.revision, "config_digest": self.config_digest(),
                "membership_ref": "private:/fixtures/19.1.json#native-0x1019-membership",
                "storage_ref": "private:/fixtures/19.1.json#web_attestors",
                "funded_accounts_ref": "private:/fixtures/19.1.json#funded"}

    def attach(self, manifest_path):
        """Writes the owner-only fixtures/19.1.json attachment the harness reads."""
        attachments = self.directory.parent / "fixtures"
        attachments.mkdir(mode=0o700, exist_ok=True)
        path = attachments / "19.1.json"
        if path.exists():
            path.unlink()
        producer = [sys.executable, str(Path(__file__).resolve()), "produce", self.endpoint,
                    str(self.requester_key), self.url, str(self.fee)]
        private_write(path, json.dumps({
            "schema": 1, "task_id": "19.1", "candidate_source_revision": self.revision,
            "candidate_manifest_ref": str(manifest_path), "scope": "isolated-real-process",
            "runtime_binary_or_image_refs": [str(self.binary), self.paxd()],
            "config_refs_and_digests": {str(slot["config"]): runtime.digest(slot["config"])
                                        for slot in self.slots},
            "isolated_chain_id_and_genesis_identity": {"chain_id": CHAIN_ID,
                                                       "genesis_sha256": self.genesis_sha256},
            "evm": {"endpoint": self.endpoint, "chain_id": CHAIN_ID, "confirmations": CONFIRMATIONS},
            "web_attestors": [{"public_signer": "0x" + slot["signer"], "payout": self.cast(slot["signer"]),
                               "key_handle_ref": str(slot["handles"]), "data_dir": str(slot["data_dir"]),
                               "peer_endpoint": slot["peer_endpoint"],
                               "submitter": "submitter" in slot} for slot in self.slots],
            "web_threshold": THRESHOLD,
            "authority_refs": {"native-0x1019-membership": self.membership},
            "funded_accounts_and_registration_receipt_refs": self.funded,
            "required_case_inventory": {"evm-attestation-recovery": {
                "request_producer": producer,
                "attestor_configs": [str(slot["config"]) for slot in self.slots],
                "attestor_keys": [str(slot["attestor"]) for slot in self.slots],
                "binary_sha256": self.binary_sha256}},
            "evidence_output_directory": str(self.directory / "evidence"),
        }, indent=2) + "\n")
        return path

    def canonical_reorg(self):
        """A reorg of the owned chain below the declared depth. The chain is a
        single CometBFT validator with instant finality: a committed block is
        never replaced without equivocating, which the fixture policy forbids,
        so no genuine canonical reorg path exists here."""
        raise RuntimeError("xweb fixture refused: no genuine canonical reorg path on a "
                           "single-validator CometBFT chain with instant finality; "
                           "reorg reconciliation below 12 confirmations is not exercised")

    def close(self):
        if self.server:
            self.server.shutdown()
        for process in self.processes:
            if process.poll() is None:
                process.terminate()
                try:
                    process.wait(timeout=30)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait()


if __name__ == "__main__":
    sys.dont_write_bytecode = True
    require(len(sys.argv) == 6 and sys.argv[1] == "produce",
            "usage: xweb_fixture.py produce <endpoint> <requester key> <url> <fee wei>")
    print(produce(sys.argv[2], sys.argv[3], sys.argv[4], int(sys.argv[5])))
