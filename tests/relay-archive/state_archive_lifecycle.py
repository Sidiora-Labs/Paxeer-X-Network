#!/usr/bin/env python3
import argparse
import copy
import hashlib
import json
import os
from pathlib import Path
import select
import shutil
import socket
import stat
import subprocess
import sys
import tempfile
import time
import urllib.request

ROOT = Path(__file__).resolve().parents[2]
sys.dont_write_bytecode = True
sys.path.insert(0, str(ROOT / "tests/support"))
sys.path.insert(0, str(ROOT / "platform/relay_archive"))
from lxgb_metadata import metadata
from cryptography.hazmat.primitives import serialization
from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey
import state_archive as archive
from protocol import NativeCodec, StateArchiveRefusal, load_config, sha256_hex

LNI_UID = 4021
NETWORK_ID = 77
PAXEER_CHAIN_ID = 125
SEQUENCER_SEED = bytes([0x22]) * 32
ARCHIVE_UIDS = {"archive-a": 4031, "archive-b": 4032}
ASSET = "b5a32b12029f8ddfb905f90f280f664b46390de0fc62770fc197dd87b18cd898"


def require(condition, message):
    if not condition:
        raise AssertionError(message)


def unused_port():
    with socket.socket() as connection:
        connection.bind(("127.0.0.1", 0))
        return connection.getsockname()[1]


def execute(args, *, data=None, env=None, uid=None, success=True, timeout=90):
    identity = {} if uid is None else {"user": uid, "group": uid, "extra_groups": []}
    result = subprocess.run([str(arg) for arg in args], input=data, env=env,
                            stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                            timeout=timeout, cwd=ROOT, **identity)
    if success and result.returncode:
        raise AssertionError(f"{Path(str(args[0])).name} failed ({result.returncode}): "
                             + result.stderr.decode(errors="replace")[-6000:])
    return result


def write_json(path, value):
    path.write_text(json.dumps(value, sort_keys=True) + "\n")
    path.chmod(0o644)


def env_file(path):
    return dict(line.split("=", 1) for line in path.read_text().splitlines() if line)


def expect_refusal(code, action, label):
    try:
        action()
    except StateArchiveRefusal as refusal:
        require(refusal.code == code, f"{label}: expected refusal {code}, got {refusal.code}: {refusal}")
        return refusal
    raise AssertionError(f"{label}: accepted instead of refusing with {code}")


def reinventory(manifest, blobs, edits):
    """Return a self-consistent variant whose segment bytes are replaced (bytes) or withdrawn (None)."""
    manifest = copy.deepcopy(manifest)
    kept = []
    for index, (segment, blob) in enumerate(zip(manifest["segments"], blobs)):
        if index in edits:
            if edits[index] is None:
                continue
            blob = edits[index]
            segment["sha256"], segment["length"] = sha256_hex(blob), len(blob)
        kept.append((segment, blob))
    for index, (segment, _blob) in enumerate(kept):
        segment["index"] = index
    manifest["segments"] = [segment for segment, _blob in kept]
    manifest["inventory_root"] = archive.inventory_root(manifest["segments"], manifest["directories"])
    return manifest, [blob for _segment, blob in kept]


def segment_index(manifest, predicate, label):
    matches = [segment["index"] for segment in manifest["segments"] if predicate(segment)]
    require(bool(matches), f"archive unit has no {label} segment")
    return matches


def tree(root):
    entries = {}
    for name in archive.ARCHIVE_ROOTS:
        base = root / name
        if not base.exists():
            continue
        for current, _children, names in os.walk(base):
            path = Path(current)
            entries[path.relative_to(root).as_posix()] = ("dir", stat.S_IMODE(path.lstat().st_mode), b"")
            for file_name in names:
                item = path / file_name
                entries[item.relative_to(root).as_posix()] = (
                    "file", stat.S_IMODE(item.lstat().st_mode), item.read_bytes())
    return entries


class Scenario:
    def __init__(self, arguments):
        self.arguments = arguments
        self.work = Path(tempfile.mkdtemp(prefix="layerx-state-archive-lifecycle-"))
        self.work.chmod(0o755)
        self.children = []
        self.logs = []
        self.checks = []
        self.archives = {}

    def passed(self, message):
        self.checks.append(message)
        print("PASS " + message, flush=True)

    def start(self, name, args, *, env=None, uid=None, pass_fds=()):
        output = (self.work / (name + ".log")).open("ab")
        identity = {} if uid is None else {"user": uid, "group": uid, "extra_groups": []}
        process = subprocess.Popen([str(arg) for arg in args], stdout=output,
                                   stderr=subprocess.STDOUT, env=env, cwd=ROOT,
                                   pass_fds=pass_fds, **identity)
        self.children.append(process)
        self.logs.append(output)
        return process

    def stop(self, process):
        if process.poll() is None:
            process.terminate()
            try:
                process.wait(timeout=20)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait(timeout=10)

    def close(self):
        for process in reversed(self.children):
            self.stop(process)
        for output in self.logs:
            output.close()
        print("Evidence: " + str(self.work), flush=True)

    def until(self, condition, label, seconds=60):
        deadline = time.monotonic() + seconds
        last = None
        while time.monotonic() < deadline:
            try:
                value = condition()
                if value:
                    return value
                last = f"condition returned {value!r}"
            except (OSError, AssertionError, StateArchiveRefusal, subprocess.TimeoutExpired) as error:
                last = error
            time.sleep(0.2)
        raise AssertionError(f"timed out: {label}; last error: {last}")

    def resolve_companions(self):
        node = Path(self.arguments.native_node).resolve()
        require(node.is_file() and os.access(node, os.X_OK), f"native node {node} is not an executable file")
        build_root = node.parent.parent
        build_text = os.path.relpath(build_root, ROOT) if build_root.is_relative_to(ROOT) else str(build_root)
        companions = {
            "layerx-genesis-build": (self.arguments.genesis_build, node.parent / "layerx-genesis-build",
                                     "layerx-genesis-build"),
            "layerx-archive-codec": (self.arguments.archive_codec, node.parent / "layerx-archive-codec",
                                     "layerx-archive-codec"),
            "sign": (self.arguments.sign_helper, build_root / "tests/relay-archive-sign",
                     f"{build_text}/tests/relay-archive-sign"),
            "paxd": (self.arguments.paxd, ROOT / "build/paxd", "paxeer-build"),
        }
        resolved, targets = {"layerxd": node}, []
        for name, (override, sibling, target) in companions.items():
            path = Path(override).resolve() if override else sibling
            if override:
                require(path.is_file() and os.access(path, os.X_OK), f"{name} override {path} is not executable")
            elif not path.is_file():
                targets.append(target)
            resolved[name] = path
        if targets:
            go = Path("/usr/local/go/bin")
            make_env = dict(os.environ, PATH=f"{go}:{os.environ['PATH']}") if go.is_dir() else None
            result = subprocess.run(["make", f"-j{os.cpu_count() or 1}", f"BUILD_DIR={build_text}", *targets], cwd=ROOT,
                                    env=make_env, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
            (self.work / "companion-build.log").write_bytes(result.stdout)
            require(result.returncode == 0, f"make {' '.join(targets)} failed ({result.returncode}); "
                                            f"see {self.work / 'companion-build.log'}")
        for name, path in resolved.items():
            require(path.is_file() and os.access(path, os.X_OK), f"make did not produce {name} at {path}")
        return resolved

    def evm(self, method):
        request = urllib.request.Request(self.rpc_url, json.dumps(
            {"jsonrpc": "2.0", "id": 1, "method": method, "params": []}).encode(),
            {"Content-Type": "application/json"})
        with urllib.request.urlopen(request, timeout=3) as response:
            value = json.load(response)
        require("result" in value and "error" not in value, f"Paxeer {method} refused: {value}")
        return value["result"]

    def start_chain(self):
        keys = self.work / "chain-keys"
        keys.mkdir(mode=0o700)
        deployer = keys / "deployer.key"
        deployer.write_text("0x" + os.urandom(32).hex())
        deployer.chmod(0o600)
        address = execute(["python3", ROOT / "platform/hosted/paxeer/evm.py", "address", deployer]).stdout.decode().strip()
        public = Ed25519PrivateKey.from_private_bytes(SEQUENCER_SEED).public_key().public_bytes(
            serialization.Encoding.Raw, serialization.PublicFormat.Raw).hex()
        self.sequencer_id = hashlib.sha256(("layerx-sequencer:" + public).encode()).hexdigest()
        policy = json.loads((ROOT / "contracts/config/checkpoint-settlement.json").read_text())
        execute(["python3", ROOT / "platform/hosted/paxeer/anchor-genesis.py", "--network-id", NETWORK_ID,
                 "--sequencer-id", self.sequencer_id, "--sequencer-public-key", public,
                 "--authority-evm", address, "--paxeer-chain-id", PAXEER_CHAIN_ID,
                 "--threshold", policy["finality_policy"]["certificate_threshold"],
                 "--output", self.work / "anchor.json"])
        chain_home = self.work / "chain-home"
        chain_home.mkdir(mode=0o700)
        chain_env = dict(os.environ, HOME=str(chain_home), PAXD=str(self.paxd),
                         LAYERX_PAXEER_HOME=str(self.work / "chain"),
                         LAYERX_PAXEER_CHAIN_ID=str(PAXEER_CHAIN_ID),
                         LAYERX_PAXEER_DEPLOYER_ADDRESS=address,
                         LAYERX_PAXEER_ANCHOR_GENESIS_FILE=str(self.work / "anchor.json"),
                         LAYERX_PAXEER_COMMIT_TIMEOUT_NANOSECONDS="1000000000")
        ports = {name: unused_port() for name in ("EVM", "EVM_WS", "RPC", "P2P", "GRPC", "GRPC_WEB", "API")}
        chain_env.update({f"LAYERX_PAXEER_{name}_PORT": str(port) for name, port in ports.items()})
        execute(["bash", ROOT / "platform/hosted/paxeer/init-chain.sh"], env=chain_env, timeout=180)
        self.rpc_url = f"http://127.0.0.1:{ports['EVM']}"
        chain = self.start("paxeer-chain", [self.paxd, "start", "--home", self.work / "chain",
                                            "--consensus.create-empty-blocks-interval=1s"], env=chain_env)
        self.until(lambda: chain.poll() is None and self.evm("eth_chainId") == hex(PAXEER_CHAIN_ID)
                   and int(self.evm("eth_blockNumber"), 16) >= 1, "Paxeer chain JSON-RPC", 120)

    def setup(self):
        require(os.geteuid() == 0, "native LNI and archive UID isolation scenario requires root")
        self.bin = self.work / "bin"
        self.bin.mkdir(mode=0o755)
        companions = self.resolve_companions()
        self.paxd = companions.pop("paxd")
        for name, path in companions.items():
            shutil.copy2(path, self.bin / name)
            (self.bin / name).chmod(0o755)
        self.runtime = self.work / "runtime"
        shutil.copytree(ROOT / "platform/relay_archive", self.runtime,
                        ignore=shutil.ignore_patterns("__pycache__"))
        for name, seed in (("sequencer", SEQUENCER_SEED), ("treasury", bytes([0x11]) * 32)):
            path = self.work / name
            path.write_bytes(seed)
            path.chmod(0o600)
        self.start_chain()
        issuer = Ed25519PrivateKey.from_private_bytes(bytes([0x11]) * 32).public_key()
        issuer = issuer.public_bytes(serialization.Encoding.Raw, serialization.PublicFormat.Raw)
        (self.work / "metadata").write_bytes(metadata(bytes.fromhex(ASSET), issuer, os.urandom(32)))
        self.data = self.work / "native"
        self.run_dir = self.work / "run"
        environment = dict(os.environ,
                           LAYERX_NODE_PAXEER_CHAIN_ID=str(PAXEER_CHAIN_ID),
                           LAYERX_NODE_PAXEER_RPC_URL=self.rpc_url)
        execute(["bash", ROOT / "platform/hosted/node/bootstrap.sh",
                 "--data-dir", self.data, "--run-dir", self.run_dir, "--network-id", str(NETWORK_ID),
                 "--genesis-metadata", self.work / "metadata",
                 "--sequencer-key", self.work / "sequencer", "--treasury-key", self.work / "treasury",
                 "--lni-uid", str(LNI_UID), "--lni-gid", str(LNI_UID),
                 "--program-port", str(unused_port()), "--replica-port", str(unused_port()),
                 "--layerxd", self.bin / "layerxd", "--genesis-build", self.bin / "layerx-genesis-build"],
                env=environment, timeout=300)
        self.node = env_file(self.data / "node.env")
        require(self.node["LAYERX_NODE_SEQUENCER_ID"] == self.sequencer_id,
                "bootstrap sequencer differs from the sequencer the Paxeer anchor genesis authorizes")
        self.socket = self.run_dir / "layerxd.lni.sock"
        self.pins = {
            "network_id": NETWORK_ID,
            "genesis_sha256": sha256_hex((self.data / archive.GENESIS_MANIFEST_PATH).read_bytes()),
            "sequencer_id": self.node["LAYERX_NODE_SEQUENCER_ID"],
            "sequencer_public_key": self.node["LAYERX_NODE_SEQUENCER_PUBLIC_KEY"],
            "sequencer_first_batch": 1,
            "allow_loopback_dev": True,
            "listener": "plain",
            "codec": str(self.bin / "layerx-archive-codec"),
        }
        self.profile = archive.profile_digest({
            "domain": archive.PROFILE_DOMAIN, "archives": 2, "threshold": 2, "signature": "ed25519",
            "segment_digest": "sha256", "retention": "indefinite", "approval": "unapproved-local-gate",
        })
        restorer = self.work / "restorer"
        restorer.mkdir(mode=0o700)
        port = unused_port()
        write_json(self.work / "restorer.json", dict(self.pins, data_dir=str(restorer),
                                                     listen=f"127.0.0.1:{port}",
                                                     public_url=f"http://127.0.0.1:{port}"))
        self.config = load_config(self.work / "restorer.json")
        self.codec = NativeCodec(self.config)

    def start_archive(self, name):
        uid = ARCHIVE_UIDS[name]
        directory = self.work / name
        if name not in self.archives:
            directory.mkdir(mode=0o700)
            os.chown(directory, uid, uid)
            port = unused_port()
            write_json(self.work / (name + ".json"), dict(
                self.pins, data_dir=str(directory), listen=f"127.0.0.1:{port}",
                public_url=f"http://127.0.0.1:{port}",
                state_archive={"archive_id": name, "key_generation": 1, "retention": "indefinite",
                               "profile_digest": self.profile}))
            self.archives[name] = {"client": archive.ArchiveClient(f"http://127.0.0.1:{port}", timeout=180)}
        process = self.start(name, [self.bin / "layerxd", "--relay-archive", self.work / (name + ".json")],
                             uid=uid, env=dict(os.environ,
                                               LAYERX_RELAY_ARCHIVE_RUNTIME=str(self.runtime / "runtime.py")))
        client = self.archives[name]["client"]
        identity = self.until(client.identity, name + " identity")
        self.archives[name]["process"] = process
        return identity

    def clients(self, *names):
        return [self.archives[name]["client"] for name in names]

    def lni(self, *arguments, data=None):
        return json.loads(execute([sys.executable, "-B", self.runtime / "state_archive.py", "lni",
                                   *arguments], data=data, uid=LNI_UID).stdout)

    def info(self):
        return self.lni("node-info", self.socket)

    def proof(self, kind, activity_id):
        value = self.lni("proof", self.socket, str(kind), activity_id)
        return bytes.fromhex(value["value_hex"]), bytes.fromhex(value["proof_hex"])

    def start_replica(self):
        ready_read, ready_write = os.pipe()
        replica_env = dict(os.environ, **env_file(self.data / "replica.env"),
                           LAYERX_AUTHORITY_READY_FD=str(ready_write))
        replica = self.start("native-replica", [self.bin / "layerxd", "--authority-replica",
                                                self.data / "replica.conf"], env=replica_env,
                             pass_fds=(ready_write,))
        os.close(ready_write)
        try:
            require(bool(select.select([ready_read], [], [], 30)[0]) and os.read(ready_read, 1) == b"R",
                    "native authority replica failed readiness")
        finally:
            os.close(ready_read)
        return replica

    def start_sequencer(self):
        script = 'set -e; source "$1"; layerx_sequencer_environment "$2"; exec "$3" --serve "$4"'
        return self.start("native-sequencer", ["bash", "-c", script, "state-archive-lifecycle",
                                               ROOT / "platform/hosted/node/sequencer-env.sh",
                                               self.data / "sequencer.env", self.bin / "layerxd",
                                               self.data / "sequencer.conf"])

    def start_node(self):
        replica = self.start_replica()
        sequencer = self.start_sequencer()
        info = self.until(lambda: sequencer.poll() is None and self.info(), "native node interface", 120)
        require(sequencer.poll() is None, "native sequencer exited during startup")
        return replica, sequencer, info

    def stop_node(self, replica, sequencer):
        self.stop(sequencer)
        self.stop(replica)
        require(sequencer.returncode == 0, f"native sequencer stopped with {sequencer.returncode}")

    def submit(self, number, batch):
        activity = execute([self.bin / "sign", str(number)]).stdout
        activity_id = json.loads(execute([self.bin / "layerx-archive-codec", "activity"],
                                         data=activity).stdout)["activity_id"]
        ack = self.lni("submit", self.socket, activity_id, data=activity)
        require(ack == {"activity_id": activity_id, "state": "acknowledged"},
                f"native submission {number} was not acknowledged")
        self.until(lambda: self.info()["published_batch"] == batch, f"canonical batch {batch} published", 120)
        return ack["activity_id"]

    def proofs(self, activity_ids):
        return [(kind, activity_id, *self.proof(kind, activity_id))
                for activity_id in activity_ids for kind in archive.PROOF_KINDS]

    def set_aside(self, name):
        holder = self.work / name
        holder.mkdir(mode=0o700)
        for item in [*archive.ARCHIVE_ROOTS, *(path.name for path in self.data.glob("history.sqlite*"))]:
            if (self.data / item).exists():
                os.replace(self.data / item, holder / item)
        return holder

    def clear_restored(self):
        for name in archive.ARCHIVE_ROOTS:
            shutil.rmtree(self.data / name)
        for path in self.data.glob("history.sqlite*"):
            path.unlink()

    def restore(self, unit, names, target, label):
        manifest, _blobs, certificate = unit
        return archive.restore_unit(manifest, certificate, self.enrolled, self.clients(*names), self.config,
                                    self.codec, target, self.work / ("restore-work-" + label))

    def proof_segments(self, manifest, blobs):
        return {(segment["subject"], archive.decode_proof(blob)[0]): archive.decode_proof(blob)[1:]
                for segment, blob in zip(manifest["segments"], blobs) if segment["kind"] == "metadata_proof"}

    def archives_up(self):
        first, second = self.start_archive("archive-a"), self.start_archive("archive-b")
        require(first["public_key"] != second["public_key"], "archives share a signing key")
        for name, identity in (("archive-a", first), ("archive-b", second)):
            uid, directory = ARCHIVE_UIDS[name], self.work / name
            key = directory / "state/archive-key-1"
            for path, mode in ((directory, 0o700), (directory / "state", 0o700), (key, 0o600)):
                information = path.lstat()
                require(information.st_uid == uid and stat.S_IMODE(information.st_mode) == mode,
                        f"{path} is not private to its archive administrator")
            public = Ed25519PrivateKey.from_private_bytes(key.read_bytes()).public_key().public_bytes(
                serialization.Encoding.Raw, serialization.PublicFormat.Raw).hex()
            require(public == identity["public_key"], f"{name} serves a key other than its local key")
            require(identity["retention"] == "indefinite" and identity["profile_digest"] == self.profile,
                    f"{name} does not commit to the storage profile and retention")
        other = execute(["cat", self.work / "archive-a/state/archive-key-1"], uid=ARCHIVE_UIDS["archive-b"],
                        success=False)
        require(other.returncode != 0, "one archive administrator can read the other's signing key")
        self.enrolled = [{name: identity[name] for name in ("archive_id", "public_key", "key_generation",
                                                             "retention")} for identity in (first, second)]
        self.passed("two archive processes run under separate administrators, directories and local keys")

    def genesis_unit(self):
        manifest, blobs = archive.build_unit(self.config, self.codec, self.data, self.profile, [])
        require(manifest["head"]["global_sequence"] == 0 and manifest["checkpoint"] is None,
                "genesis unit does not stand at sequence zero")
        truncated = reinventory(manifest, blobs, {1: blobs[1][:-1]})
        expect_refusal("corrupt", lambda: archive.issue_certificate(*truncated, self.clients("archive-a",
                       "archive-b"), self.enrolled), "truncated genesis snapshot")
        flipped = bytearray(blobs[1])
        flipped[-1] ^= 1
        corrupt = reinventory(manifest, blobs, {1: bytes(flipped)})
        expect_refusal("corrupt", lambda: archive.issue_certificate(*corrupt, self.clients("archive-a",
                       "archive-b"), self.enrolled), "genesis snapshot with a damaged root")
        for client in self.clients("archive-a", "archive-b"):
            require(client.units() == [], "an archive recorded a unit the native verifier refused")
        certificate = archive.issue_certificate(manifest, blobs, self.clients("archive-a", "archive-b"),
                                                self.enrolled)
        require(archive.verify_certificate(certificate, manifest, self.enrolled) ==
                archive.manifest_digest(manifest), "genesis certificate does not bind its manifest")
        self.genesis = (manifest, blobs, certificate)
        self.passed("native genesis root verified, damaged snapshots refused, genesis unit 2-of-2 certified")

    def live_history(self):
        replica, sequencer, _info = self.start_node()
        self.ids = [self.submit(0, 1), self.submit(1, 2)]
        self.head_info = self.info()
        require(self.head_info["published_batch"] == 2 and self.head_info["role"] == 1,
                "sequencer did not publish two batches")
        self.live_proofs = self.proofs(self.ids)
        self.stop_node(replica, sequencer)
        manifest, blobs = archive.build_unit(self.config, self.codec, self.data, self.profile, self.live_proofs)
        kinds = [segment["kind"] for segment in manifest["segments"]]
        require(manifest["head"]["batch_number"] == 2 and kinds.count("batch") == 2 and
                kinds.count("metadata_proof") == 4 and "witness" in kinds and "log" in kinds,
                "archive unit does not inventory batches, logs, witnesses and metadata proofs")
        checkpoints = segment_index(manifest, lambda segment: segment["kind"] == "snapshot" and
                                    segment["index"] > 1, "checkpoint snapshot")
        require(len(checkpoints) >= 2, "node wrote fewer than two checkpoints, replay cannot be exercised")
        require(manifest["head"]["global_sequence"] == self.head_info["head_sequence"],
                "archived head differs from the native node head")
        self.history = (manifest, blobs)
        self.passed(f"live native node archived to sequence {manifest['head']['global_sequence']} with "
                    f"{len(checkpoints)} checkpoints and {kinds.count('metadata_proof')} historical proofs")

    def admission_refusals(self):
        manifest, blobs = self.history
        both = self.clients("archive-a", "archive-b")
        second = segment_index(manifest, lambda segment: segment["kind"] == "batch" and segment["first"] == 2,
                               "second batch")[0]
        damaged = bytearray(blobs[second])
        damaged[-1] ^= 1
        expect_refusal("corrupt", lambda: archive.issue_certificate(
            *reinventory(manifest, blobs, {second: bytes(damaged)}), both, self.enrolled),
            "canonical batch with a broken signature")
        first = segment_index(manifest, lambda segment: segment["kind"] == "batch" and segment["first"] == 1,
                              "first batch")[0]
        gap, _gap_blobs = reinventory(manifest, blobs, {first: None})
        expect_refusal("missing", lambda: both[0].admit(gap), "archive admitting a batch gap")
        expect_refusal("missing", lambda: archive.validate_manifest(gap), "batch gap inventory")
        proof = segment_index(manifest, lambda segment: segment["kind"] == "metadata_proof", "proof")[-1]
        for segment, blob in zip(manifest["segments"], blobs):
            if segment["index"] != proof:
                both[0].put_segment(blob, segment["sha256"])
        expect_refusal("missing", lambda: both[0].admit(manifest), "archive admitting a missing segment")
        wrong = reinventory(manifest, blobs, {})[0]
        wrong["head"] = dict(wrong["head"], receipt_state_root="00" * 32)
        expect_refusal("conflict", lambda: archive.issue_certificate(wrong, blobs, both, self.enrolled),
                       "head root that differs from native history")
        expect_refusal("corrupt", lambda: both[1].put_segment(b"segment", sha256_hex(b"other")),
                       "segment upload with a mismatched digest")
        for client in both:
            require([unit["global_sequence"] for unit in client.units()] == [0],
                    "an archive recorded a refused history unit")
        certificate = archive.issue_certificate(manifest, blobs, both, self.enrolled)
        require(archive.issue_certificate(manifest, blobs, both, self.enrolled) == certificate,
                "exact repeat of an archive unit was not idempotent")
        node_file = segment_index(manifest, lambda segment: segment["kind"] == "node_file", "node file")[-1]
        conflicting, conflicting_blobs = reinventory(manifest, blobs, {node_file: None})
        for client in both:
            expect_refusal("conflict", lambda: client.admit(conflicting), "conflicting unit at the same identity")
        expect_refusal("conflict", lambda: archive.issue_certificate(conflicting, conflicting_blobs, both,
                                                                     self.enrolled), "conflicting issuance")
        self.history = (manifest, blobs, certificate)
        self.passed("corrupt, missing, divergent and conflicting units refused; exact repeats idempotent")

    def certificate_refusals(self):
        manifest, _blobs, certificate = self.history
        signatures = certificate["signatures"]
        forged_key = Ed25519PrivateKey.generate()
        forged = dict(signatures[0], signature=forged_key.sign(archive.certificate_body(
            certificate["manifest_digest"], manifest, signatures[0])).hex())
        cases = (
            ("single signature", dict(certificate, signatures=signatures[:1])),
            ("repeated signer", dict(certificate, signatures=[signatures[0], signatures[0]])),
            ("wrong signing key", dict(certificate, signatures=[forged, signatures[1]])),
            ("altered retention", dict(certificate, signatures=[dict(signatures[0], retention="bounded"),
                                                                signatures[1]])),
            ("advanced key generation", dict(certificate, signatures=[dict(signatures[0], key_generation=2),
                                                                      signatures[1]])),
        )
        for label, value in cases:
            expect_refusal("policy", lambda: archive.verify_certificate(value, manifest, self.enrolled), label)
        expect_refusal("conflict", lambda: archive.verify_certificate(self.genesis[2], manifest, self.enrolled),
                       "certificate for another unit")
        expect_refusal("policy", lambda: archive.verify_certificate(certificate, manifest, self.enrolled[:1]),
                       "single-archive policy")
        self.passed("availability certificates accepted only with both enrolled archive signatures")

    def corrupt_at_rest(self, name, segment):
        path = self.work / name / "state/segments" / segment["sha256"]
        descriptor = os.open(path, os.O_RDWR)
        try:
            byte = os.pread(descriptor, 1, segment["length"] // 2)
            os.pwrite(descriptor, bytes([byte[0] ^ 0xFF]), segment["length"] // 2)
            os.fsync(descriptor)
        finally:
            os.close(descriptor)

    def restore_history(self):
        manifest, blobs, _certificate = self.history
        self.batch_two = manifest["segments"][segment_index(
            manifest, lambda segment: segment["kind"] == "batch" and segment["first"] == 2, "second batch")[0]]
        self.corrupt_at_rest("archive-a", self.batch_two)
        original = self.set_aside("original")
        summary = self.restore(self.history, ("archive-a", "archive-b"), self.data, "full")
        require(summary["refused_copies"] == [{"index": self.batch_two["index"],
                                               "archive": self.archives["archive-a"]["client"].origin,
                                               "code": "corrupt"}],
                "restoration did not refuse the damaged copy and fall back to the second archive")
        require(summary["proofs"] == 4 and summary["batches"] == 2, "restoration skipped archived history")
        require(tree(self.data) == tree(original), "restored files, bytes or modes differ from the node")
        replica, sequencer, info = self.start_node()
        require(info == self.head_info, f"restored node reports {info}, archived node reported {self.head_info}")
        require(self.proofs(self.ids) == self.live_proofs, "restored node serves different historical proofs")
        archived = self.proof_segments(manifest, blobs)
        for kind, activity_id, value, proof in self.live_proofs:
            require(archived[(activity_id, kind)] == (value, proof), "archived proof differs from the node")
        self.stop_node(replica, sequencer)
        self.passed("damaged archive copy refused; native root, logs and historical proofs restored exactly")

    def restore_replay(self):
        manifest, _blobs, _certificate = self.history
        self.clear_restored()
        self.restore(self.history, ("archive-a", "archive-b"), self.data, "replay")
        checkpoints = sorted(segment["first"] for segment in manifest["segments"]
                             if segment["kind"] == "snapshot" and segment["index"] > 1)
        for sequence in checkpoints[1:]:
            for suffix in (".lxs", ".lxi"):
                (self.data / "checkpoints" / f"{sequence:020d}{suffix}").unlink(missing_ok=True)
        replica, sequencer, info = self.start_node()
        require(info["head_sequence"] == self.head_info["head_sequence"] and
                info["published_batch"] == self.head_info["published_batch"] and
                info["checkpoint_id"] == self.head_info["checkpoint_id"],
                f"replay from checkpoint {checkpoints[0]} reached {info}, expected {self.head_info}")
        require(self.proofs(self.ids) == self.live_proofs, "replayed node serves different historical proofs")
        self.stop_node(replica, sequencer)
        self.passed(f"native node replayed archived history from checkpoint {checkpoints[0]} to the archived head")

    def restore_tampered(self):
        manifest, _blobs, _certificate = self.history
        self.clear_restored()
        self.restore(self.history, ("archive-a", "archive-b"), self.data, "tamper")
        latest = self.data / "checkpoints" / f"{manifest['checkpoint']['global_sequence']:020d}.lxs"
        data = bytearray(latest.read_bytes())
        data[-1] ^= 1
        latest.write_bytes(bytes(data))
        replica = self.start_replica()
        sequencer = self.start_sequencer()
        try:
            sequencer.wait(timeout=120)
        except subprocess.TimeoutExpired as error:
            raise AssertionError("native node started from a tampered restored checkpoint") from error
        require(sequencer.returncode != 0, "native node accepted a tampered restored checkpoint")
        self.stop(replica)
        self.passed("native node refuses a restored checkpoint whose snapshot root does not verify")

    def continue_history(self):
        self.clear_restored()
        self.restore(self.history, ("archive-a", "archive-b"), self.data, "continue")
        replica, sequencer, _info = self.start_node()
        self.ids.append(self.submit(2, 3))
        self.next_info = self.info()
        self.next_proofs = self.proofs(self.ids)
        require(self.next_proofs[:4] == self.live_proofs, "restored node changed earlier historical proofs")
        self.stop_node(replica, sequencer)
        self.next = archive.build_unit(self.config, self.codec, self.data, self.profile, self.next_proofs)
        require(self.next[0]["head"]["batch_number"] == 3, "continued history was not archived")
        self.passed("restored node extends canonical history and the inventory grows with it")

    def outage(self):
        both = ("archive-a", "archive-b")
        self.stop(self.archives["archive-b"]["process"])
        expect_refusal("corrupt", lambda: self.restore(self.history, both, self.work / "never", "outage"),
                       "restore with one damaged archive and one archive offline")
        target = self.work / "restore-genesis"
        target.mkdir(mode=0o700)
        self.restore(self.genesis, both, target, "genesis")
        for segment, blob in zip(self.genesis[0]["segments"], self.genesis[1]):
            if segment["kind"] in archive.FILE_KINDS:
                require((target / segment["path"]).read_bytes() == blob, "genesis restored from one archive differs")
        manifest, blobs = self.next
        expect_refusal("unavailable", lambda: archive.issue_certificate(
            manifest, blobs, self.clients(*both), self.enrolled), "issuance during a one-archive outage")
        reply = self.archives["archive-a"]["client"].admit(manifest)
        partial = {"domain": archive.CERTIFICATE_DOMAIN, "manifest_digest": reply["manifest_digest"],
                   "signatures": [{name: reply[name] for name in ("archive_id", "public_key", "key_generation",
                                                                  "retention", "signature")}]}
        expect_refusal("policy", lambda: archive.verify_certificate(partial, manifest, self.enrolled),
                       "certificate from the surviving archive alone")
        self.passed("one-archive outage: no certificate issued, certified units still restore from one copy")
        identity = self.start_archive("archive-b")
        require(identity["public_key"] == self.enrolled[1]["public_key"], "restarted archive changed its key")
        certificate = archive.issue_certificate(manifest, blobs, self.clients(*both), self.enrolled)
        segment_path = self.work / "archive-a/state/segments" / self.batch_two["sha256"]
        require(sha256_hex(segment_path.read_bytes()) == self.batch_two["sha256"],
                "archive did not repair its damaged copy from the issuance upload")
        for client in self.clients(*both):
            require([unit["global_sequence"] for unit in client.units()] ==
                    [0, self.head_info["head_sequence"], self.next_info["head_sequence"]],
                    "archive inventory is not the immutable sequence of certified units")
        target = self.work / "restore-next"
        target.mkdir(mode=0o700)
        summary = self.restore((manifest, blobs, certificate), ("archive-a",), target, "next")
        require(summary["refused_copies"] == [] and summary["batches"] == 3 and summary["proofs"] == 6,
                "continued unit did not restore from the repaired archive alone")
        require(tree(target) == tree(self.data), "continued unit restored different files")
        self.passed("recovered archive rejoins with its key; continued unit 2-of-2 certified and restored")

    def run_all(self):
        self.setup()
        self.archives_up()
        self.genesis_unit()
        self.live_history()
        self.admission_refusals()
        self.certificate_refusals()
        self.restore_history()
        self.restore_replay()
        self.restore_tampered()
        self.continue_history()
        self.outage()
        print(f"{len(self.checks)} passed, 0 failed", flush=True)


def arguments():
    parser = argparse.ArgumentParser(description="Native state archive lifecycle gate")
    parser.add_argument("--native-node", required=True)
    parser.add_argument("--genesis-build")
    parser.add_argument("--archive-codec")
    parser.add_argument("--sign-helper")
    parser.add_argument("--paxd")
    return parser.parse_args()


if __name__ == "__main__":
    scenario = Scenario(arguments())
    try:
        scenario.run_all()
    except Exception:
        print(f"{len(scenario.checks)} passed, 1 failed", flush=True)
        raise
    finally:
        scenario.close()
