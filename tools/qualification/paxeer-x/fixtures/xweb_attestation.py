#!/usr/bin/env python3
"""Qualify the paid web and web attestor contract against a candidate manifest."""
import argparse
import importlib.util
import json
import os
from pathlib import Path
import re
import shlex
import shutil
from types import SimpleNamespace
from urllib.parse import urlsplit
import signal
import stat
import subprocess
import sys
import time
import urllib.error
import urllib.request

sys.dont_write_bytecode = True

ROOT = Path(__file__).resolve().parents[4]
FIXTURE = Path(__file__).resolve().parent / "xweb_fixture.py"
SPEC = ROOT / "spec/paxeer-x/spec.kvx"
CANDIDATE = ROOT / "tools/paxeer-x/candidate.py"
UNKNOWN = "unknown"
ATTACHMENT_FIELDS = (
    "schema", "task_id", "candidate_source_revision", "candidate_manifest_ref", "scope",
    "runtime_binary_or_image_refs", "config_refs_and_digests",
    "isolated_chain_id_and_genesis_identity", "evm", "web_attestors", "web_threshold",
    "authority_refs", "funded_accounts_and_registration_receipt_refs",
    "required_case_inventory", "evidence_output_directory",
)
ATTESTOR_FIELDS = ("public_signer", "payout", "key_handle_ref", "data_dir", "peer_endpoint")
STAGES = ("scanned", "fetching", "content-fetched", "attested", "before-quorum", "signatures-collected", "before-signed-journal", "submitted", "submitted-no-answer", "broadcast")
BINDINGS = ("source_revision", "config_digest", "membership_ref", "storage_ref", "funded_accounts_ref")
BINDING_PATTERNS = {
    "source_revision": re.compile(r"[0-9a-f]{40}|[0-9a-f]{64}"),
    "config_digest": re.compile(r"sha256:[0-9a-f]{64}"),
    "membership_ref": re.compile(r"(?:private:/|secure://)[A-Za-z0-9_./:#@+-]+"),
    "storage_ref": re.compile(r"(?:private:/|secure://)[A-Za-z0-9_./:#@+-]+"),
    "funded_accounts_ref": re.compile(r"(?:private:/|secure://)[A-Za-z0-9_./:#@+-]+"),
}
DOMAIN = b"PAXEERX_WEB_V1"
FULFIL_SIGNATURE = b"fulfil(uint64,bytes,bytes32,uint32,bytes[])"
XWEB_PRECOMPILE = "0x" + "00" * 18 + "1019"
STATUS_FULFILLED = 1
STAGE_TIMEOUT = 600
_ROUND = (
    0x0000000000000001, 0x0000000000008082, 0x800000000000808A, 0x8000000080008000,
    0x000000000000808B, 0x0000000080000001, 0x8000000080008081, 0x8000000000008009,
    0x000000000000008A, 0x0000000000000088, 0x0000000080008009, 0x000000008000000A,
    0x000000008000808B, 0x800000000000008B, 0x8000000000008089, 0x8000000000008003,
    0x8000000000008002, 0x8000000000000080, 0x000000000000800A, 0x800000008000000A,
    0x8000000080008081, 0x8000000000008080, 0x0000000080000001, 0x8000000080008008,
)
_ROTATE = (
    (0, 36, 3, 41, 18), (1, 44, 10, 45, 2), (62, 6, 43, 15, 61),
    (28, 55, 25, 21, 56), (27, 20, 39, 8, 14),
)


class Refusal(Exception):
    pass


def require(condition, message):
    if not condition:
        raise Refusal(message)


def load_manifest(path):
    path = Path(path)
    require(path.is_file(), f"candidate manifest absent: {path}")
    mode = path.stat().st_mode
    require(not mode & (stat.S_IRWXG | stat.S_IRWXO), "candidate manifest is not owner-only")
    candidate = json.loads(path.read_text(encoding="utf-8"))
    mainline = candidate.get("source", {}).get("mainline_revision")
    require(isinstance(mainline, str) and mainline, "candidate mainline binding absent")
    checked = subprocess.run(
        [sys.executable, str(CANDIDATE), "validate", str(path), "--repo", str(ROOT),
         "--spec", str(SPEC), "--mainline", mainline],
        cwd=ROOT, capture_output=True, text=True,
        env=dict(os.environ, PYTHONDONTWRITEBYTECODE="1"))
    require(checked.returncode == 0,
            "candidate manifest refused by candidate.py validate: "
            + (checked.stderr.strip() or checked.stdout.strip()))
    return json.loads(path.read_text(encoding="utf-8"))


def service(manifest, name):
    found = [entry for entry in manifest["services"] if entry["id"] == name]
    require(len(found) == 1, f"candidate manifest has no service {name}")
    return found[0]


def attachment(manifest, task):
    evidence = os.environ.get("PAXEER_X_EVIDENCE_DIR")
    require(evidence, "PAXEER_X_EVIDENCE_DIR is not set")
    evidence = Path(evidence)
    require(evidence.is_dir() and not evidence.stat().st_mode & (stat.S_IRWXG | stat.S_IRWXO),
            "evidence directory absent or not owner-only")
    path = evidence / "fixtures" / f"{task}.json"
    require(path.is_file(),
            f"isolated fixture attachment absent: {path.name} under the evidence directory")
    require(not path.stat().st_mode & (stat.S_IRWXG | stat.S_IRWXO),
            "fixture attachment is not owner-only")
    data = json.loads(path.read_text(encoding="utf-8"))
    missing = [field for field in ATTACHMENT_FIELDS if field not in data]
    require(not missing, "fixture attachment lacks " + ", ".join(missing))
    require(data["task_id"] == task, "fixture attachment is for another task")
    require(data["scope"] == "isolated-real-process", "fixture attachment scope is not isolated-real-process")
    require(data["candidate_source_revision"] == manifest["source"]["revision"],
            "fixture attachment names another candidate revision")
    return data


def keccak256(data):
    """Keccak-256 as Ethereum uses it (original padding, not SHA3)."""
    mask = (1 << 64) - 1
    rotl = lambda value, shift: ((value << shift) | (value >> (64 - shift))) & mask if shift else value
    rate = 136
    data = bytearray(data) + b"\x01" + b"\x00" * ((-len(data) - 1) % rate)
    data[-1] |= 0x80
    state = [[0] * 5 for _ in range(5)]
    for offset in range(0, len(data), rate):
        for index in range(rate // 8):
            state[index % 5][index // 5] ^= int.from_bytes(data[offset + 8 * index:offset + 8 * index + 8], "little")
        for constant in _ROUND:
            column = [state[x][0] ^ state[x][1] ^ state[x][2] ^ state[x][3] ^ state[x][4] for x in range(5)]
            delta = [column[(x - 1) % 5] ^ rotl(column[(x + 1) % 5], 1) for x in range(5)]
            state = [[state[x][y] ^ delta[x] for y in range(5)] for x in range(5)]
            moved = [[0] * 5 for _ in range(5)]
            for x in range(5):
                for y in range(5):
                    moved[y][(2 * x + 3 * y) % 5] = rotl(state[x][y], _ROTATE[x][y])
            state = [[moved[x][y] ^ (~moved[(x + 1) % 5][y] & moved[(x + 2) % 5][y]) for y in range(5)]
                     for x in range(5)]
            state[0][0] ^= constant
    return b"".join(state[index % 5][index // 5].to_bytes(8, "little") for index in range(4))


def rpc(endpoint, method, params):
    body = json.dumps({"jsonrpc": "2.0", "id": 1, "method": method, "params": params}).encode()
    request = urllib.request.Request(endpoint, body, {"content-type": "application/json"})
    with urllib.request.urlopen(request, timeout=30) as answer:
        reply = json.load(answer)
    require("result" in reply, f"{method} refused by the isolated chain: {reply.get('error')}")
    return reply["result"]


def request_status(endpoint, request_id):
    data = "0x" + keccak256(b"getRequest(uint64)")[:4].hex() + request_id.to_bytes(32, "big").hex()
    answer = bytes.fromhex(rpc(endpoint, "eth_call", [{"to": XWEB_PRECOMPILE, "data": data}, "latest"])[2:])
    require(len(answer) >= 9 * 32, f"getRequest({request_id}) answer malformed")
    require(int.from_bytes(answer[:32], "big") == request_id, f"getRequest({request_id}) names another request")
    return answer[9 * 32 - 1]


def journalled(slot, request_id):
    path = Path(slot["data_dir"]) / "watch" / "pending.json"
    if not path.is_file():
        return None
    progress = json.loads(path.read_text(encoding="utf-8"))
    require(isinstance(progress, dict) and progress.get("version") == 1, "atomic progress journal absent")
    require(progress["chain_id"] == 125, "watcher journal chain binding differs")
    for entry in progress["entries"]:
        require(entry["request"]["block_number"] < progress["next_block"], "cursor passed uncommitted work")
        if entry["request"]["request_id"] == request_id:
            source = entry["source"]
            require(source["chain_id"] == 125 and len(source["block_hash"]) == 32
                    and len(source["transaction_hash"]) == 32, "canonical source identity absent")
            return entry
    return None


def submitted(slot, request_id):
    path = Path(slot["data_dir"]) / "submit" / f"{request_id}.json"
    return json.loads(path.read_text(encoding="utf-8")) if path.is_file() else None


def attested(slot, request_id):
    url = slot["peer_endpoint"].rstrip("/") + f"/attestations/{request_id}"
    try:
        with urllib.request.urlopen(url, timeout=10) as answer:
            return answer.status == 200
    except (urllib.error.URLError, OSError):
        return False


def wait(condition, what):
    deadline = time.monotonic() + STAGE_TIMEOUT
    while time.monotonic() < deadline:
        if condition():
            return
        time.sleep(1)
    raise Refusal(f"timed out waiting for {what}")


class Sidecar:
    def __init__(self, binary, slot, config, log_dir, index):
        self.binary, self.slot, self.config = binary, slot, config
        spec = importlib.util.spec_from_file_location("paid_web_process_driver", ROOT / "tools/qualification/paxeer-x/paid-web-contract.py")
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        handles_path = module.private(slot["key_handle_ref"])
        handles = json.loads(handles_path.read_text())
        require(set(handles).issubset({"X_WEBSEARCH_ATTESTOR_KEY_FILE", "X_WEBSEARCH_RECEIVER_KEY_FILE", "X_WEBSEARCH_SUBMITTER_KEY_FILE"})
                and {"X_WEBSEARCH_ATTESTOR_KEY_FILE", "X_WEBSEARCH_RECEIVER_KEY_FILE"}.issubset(handles),
                "explicit private attestor key handles required")
        self.environment = {name: str(module.private(path)) for name, path in handles.items()}
        self.url = urlsplit(slot["peer_endpoint"])
        require(self.url.scheme == "http" and self.url.hostname == "127.0.0.1" and self.url.port,
                "owned loopback attestor endpoint required")
        evidence = log_dir / ("attestor-" + str(index + 1))
        evidence.mkdir(mode=0o700)
        settings = SimpleNamespace(binary=binary.resolve(), config_path=config.resolve(), isolated=log_dir.parent.resolve(),
            evidence=evidence, launches=0, url=self.url, runtime_env=self.environment,
            debugger_nonstop=True, breakpoint=self.breakpoint)
        self.driver = module.Candidate(settings)
        self.driver_refusal = module.Refusal

    def breakpoint(self, stage):
        boundaries = {
            "scanned": ("main.rs", "if request.timeout_height < head {"),
            "fetching": ("attest.rs", "let page = self.fetcher.fetch(payload).map_err(AttestError::Fetch)?;"),
            "content-fetched": ("attest.rs", "let digest = self.store.put(&canonical).map_err(|_| AttestError::Store)?;"),
            "attested": ("main.rs", "if let Err(error) = pipeline.watcher.transition(request_id, WorkStage::Attested) {"),
            "before-quorum": ("main.rs", "if pipeline.exchange.ready(request_id, &set).is_some() {"),
            "signatures-collected": ("main.rs", "if let Err(error) = pipeline.watcher.transition(request_id, WorkStage::Quorum) {"),
            "before-signed-journal": ("submit.rs", "let signed = transaction.sign(&self.key)?;"),
            "submitted": ("submit.rs", "self.broadcast(request_id, &signed)"),
            "submitted-no-answer": ("submit.rs", "self.broadcast(request_id, &signed)"),
            "broadcast": ("submit.rs", "Ok(Outcome::Sent { hash: signed.hash })"),
        }
        name, needle = boundaries[stage]
        path = ROOT / "interop/crates/x-websearch/src" / name
        source = path.read_text()
        pattern = r"\s*".join(re.escape(character) for character in "".join(needle.split()))
        matches = list(re.finditer(r"(?m)^[ \t]*(?P<statement>" + pattern + r")[ \t]*$", source))
        lines = [source.count("\n", 0, match.start("statement")) + 1 for match in matches]
        require(len(lines) == 1, "exact EVM production interruption boundary absent")
        return str(path) + ":" + str(lines[0])

    def start(self, stage=None):
        try:
            self.driver.start(stage)
        except self.driver_refusal as error:
            raise Refusal(str(error)) from error

    def boundary(self, stage, request_id):
        deadline = time.monotonic() + STAGE_TIMEOUT
        while time.monotonic() < deadline:
            require(self.driver.process.poll() is None, "attestor exited before interruption")
            if self.driver.stopped.wait(1):
                if stage != "before-quorum" or len((retained(self.slot, request_id) or {}).get("signatures", [])) == 2:
                    return
                self.driver.stopped.clear()
                self.driver.command("-exec-continue --all")
        raise Refusal("production interruption boundary not reached")

    def kill(self):
        pid = self.driver.pid
        require(pid is not None and Path("/proc/" + str(pid) + "/exe").resolve() == self.binary.resolve(),
                "refuse signalling foreign attestor")
        self.driver.stop(crash=True)
        require(not Path("/proc/" + str(pid)).exists(), "attestor survived SIGKILL")

    def stop(self):
        self.driver.stop()


def produce_request(producer):
    made = subprocess.run(producer, capture_output=True, text=True, timeout=STAGE_TIMEOUT)
    require(made.returncode == 0, f"request producer exited {made.returncode}")
    lines = made.stdout.strip().splitlines()
    require(lines and lines[-1].isdigit(), "request producer printed no request id")
    return int(lines[-1])


def served(slot, request_id):
    url = slot["peer_endpoint"].rstrip("/") + f"/attestations/{request_id}"
    try:
        with urllib.request.urlopen(url, timeout=10) as answer:
            return json.load(answer) if answer.status == 200 else None
    except (urllib.error.URLError, OSError):
        return None


def retained(slot, request_id):
    path = Path(slot["data_dir"]) / "attest" / "answers" / f"{request_id}.json"
    return json.loads(path.read_text(encoding="utf-8")) if path.is_file() else None


def load_fixture():
    spec = importlib.util.spec_from_file_location("xweb_fixture", FIXTURE)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def resolve_bindings(declared, authenticated):
    """The manifest's binding where it is known, the fixture-authenticated one
    where it is unknown; a disagreement or a field neither resolves refuses."""
    resolved, unknown = {}, []
    for field in BINDINGS:
        value, own = declared[field], authenticated.get(field)
        if value != UNKNOWN:
            require(own is None or own == value, f"xweb-attestors {field} disagrees with the fixture")
            resolved[field] = value
        elif isinstance(own, str) and BINDING_PATTERNS[field].fullmatch(own):
            resolved[field] = own
        else:
            unknown.append(field)
    require(not unknown, "xweb-attestors bindings unresolved: " + ", ".join(sorted(unknown)))
    return resolved


def eth_call_reverts(endpoint, sender, data):
    body = json.dumps({"jsonrpc": "2.0", "id": 1, "method": "eth_call", "params": [
        {"from": sender, "to": XWEB_PRECOMPILE, "data": data, "gas": hex(10_000_000)}, "latest"]}).encode()
    request = urllib.request.Request(endpoint, body, {"content-type": "application/json"})
    with urllib.request.urlopen(request, timeout=30) as answer:
        reply = json.load(answer)
    require(("result" in reply) != ("error" in reply), "eth_call answer is neither a result nor an error")
    if "error" not in reply:
        return False, None
    error = reply["error"]
    require(isinstance(error, dict) and error.get("code") in (3, -32000)
            and "revert" in error.get("message", "").lower(), "negative case failed outside native execution")
    return True, error["code"]


def fulfil_calldata(request_id, response, content_digest, full_length, signatures):
    word = lambda value: value.to_bytes(32, "big")
    padded = lambda raw: word(len(raw)) + raw + bytes(-len(raw) % 32)
    tail_response = padded(response)
    elements = [padded(signature) for signature in signatures]
    offsets, at = [], 32 * len(elements)
    for element in elements:
        offsets.append(word(at))
        at += len(element)
    tail_signatures = word(len(elements)) + b"".join(offsets) + b"".join(elements)
    head = (word(request_id) + word(5 * 32) + content_digest + word(full_length)
            + word(5 * 32 + len(tail_response)))
    return "0x" + (keccak256(FULFIL_SIGNATURE)[:4] + head + tail_response + tail_signatures).hex()


def signature_authority(evm, endpoint, request_id, answer, keys, sender):
    """Binds the retained answer to the request on chain and proves that a wrong
    digest, a repeated or unsorted signer, too few signers or a non-member never
    authorizes fulfil, while the exact threshold set does."""
    data = "0x" + keccak256(b"getRequest(uint64)")[:4].hex() + request_id.to_bytes(32, "big").hex()
    view = bytes.fromhex(rpc(endpoint, "eth_call", [{"to": XWEB_PRECOMPILE, "data": data}, "latest"])[2:])
    require(int.from_bytes(view[:32], "big") == request_id, "getRequest names another request")
    response = bytes.fromhex(answer["response"][2:])
    content = bytes.fromhex(answer["content_digest"][2:])
    full_length = answer["full_length"]
    preimage = lambda content_digest: (
        DOMAIN + b"\x01" + (125).to_bytes(32, "big") + view[32:64] + request_id.to_bytes(8, "big")
        + view[95:96] + view[96:128] + content_digest + keccak256(response) + full_length.to_bytes(4, "big"))
    digest = keccak256(preimage(content))
    require(len(preimage(content)) == 188, "origin-1 preimage is not 188 bytes")
    require("0x" + digest.hex() == answer["digest"],
            "retained answer is not bound to the on-chain origin/domain/request/content digest")
    wrong = bytes(byte ^ 0xFF for byte in content)

    def sign(secret, over):
        parity, r, s = evm.sign_digest(secret, over)
        return r.to_bytes(32, "big") + s.to_bytes(32, "big") + bytes([27 + parity])

    members = sorted((evm.address_of(secret), secret) for secret in keys)
    stranger = int.from_bytes(os.urandom(32), "big") % (evm.N - 1) + 1
    mixed = sorted(members[:2] + [(evm.address_of(stranger), stranger)])
    valid = [sign(secret, digest) for _, secret in members[:3]]
    cases = {
        "control-threshold-set": (content, valid),
        "wrong-digest": (content, [sign(secret, keccak256(preimage(wrong))) for _, secret in members[:3]]),
        "wrong-content-digest": (wrong, valid),
        "wrong-origin": (content, [sign(secret, keccak256(DOMAIN + b"\x02" + preimage(content)[len(DOMAIN) + 1:])) for _, secret in members[:3]]),
        "wrong-domain": (content, [sign(secret, keccak256(b"PAXEERX_WEB_V2" + preimage(content)[len(DOMAIN):])) for _, secret in members[:3]]),
        "wrong-request": (content, [sign(secret, keccak256(preimage(content)[:len(DOMAIN) + 65] + (request_id + 1).to_bytes(8, "big") + preimage(content)[len(DOMAIN) + 73:])) for _, secret in members[:3]]),
        "duplicate-signer": (content, [valid[0], valid[0], valid[1]]),
        "unsorted-signers": (content, [valid[1], valid[0], valid[2]]),
        "insufficient-threshold": (content, valid[:2]),
        "unregistered-signer": (content, [sign(secret, digest) for _, secret in mixed]),
    }
    results = []
    for name, (claimed, signatures) in cases.items():
        reverted, error_code = eth_call_reverts(endpoint, sender,
                                    fulfil_calldata(request_id, response, claimed, full_length, signatures))
        require(reverted == (name != "control-threshold-set"),
                f"fulfil {name} {'reverted' if reverted else 'was authorized'}")
        results.append({"case": name, "reverted": reverted, "rpc_error_code": error_code})
    return results


def interrupt_at(stage, sidecars, threshold, endpoint, producer, authority):
    """Emits one real request, SIGKILLs the first attestor at `stage`,
    restarts it over the same directories and asserts exactly-once settlement."""
    target = sidecars[0]
    held = sidecars[2:] if stage == "before-quorum" else []
    for sidecar in held:
        sidecar.stop()
    target.stop()
    target.start(stage)
    request_id = produce_request(producer)
    target.boundary(stage, request_id)
    reached = {
        "scanned": lambda: journalled(target.slot, request_id) is not None,
        "fetching": lambda: journalled(target.slot, request_id)["stage"] == "attesting",
        "content-fetched": lambda: journalled(target.slot, request_id)["stage"] == "attesting"
            and retained(target.slot, request_id) is None,
        "attested": lambda: attested(target.slot, request_id),
        "before-quorum": lambda: (attested(sidecars[1].slot, request_id)
                                  and len((retained(target.slot, request_id) or {})
                                          .get("signatures", [])) == 2),
        "signatures-collected": lambda: sum(attested(sidecar.slot, request_id)
                                            for sidecar in sidecars) >= threshold,
        "before-signed-journal": lambda: journalled(target.slot, request_id)["stage"] == "quorum"
            and submitted(target.slot, request_id) is None,
        "submitted": lambda: submitted(target.slot, request_id) is not None,
        "submitted-no-answer": lambda: submitted(target.slot, request_id) is not None,
        "broadcast": lambda: submitted(target.slot, request_id) is not None,
    }[stage]
    wait(reached, f"request {request_id} to reach {stage}")
    record = served(target.slot, request_id)
    if stage == "before-quorum":
        require(not any(attested(sidecar.slot, request_id) for sidecar in held),
                "a held attestor answered while stopped")
        require(request_status(endpoint, request_id) != STATUS_FULFILLED,
                f"request {request_id} fulfilled below threshold")
    source_before = journalled(target.slot, request_id)["source"]
    target.kill()
    require(journalled(target.slot, request_id)["source"] == source_before, "crash changed canonical source identity")
    before = submitted(target.slot, request_id)
    if stage in ("submitted", "submitted-no-answer", "broadcast"):
        require(before is not None, f"request {request_id} signed bytes not journalled before the kill")
    elif request_status(endpoint, request_id) != STATUS_FULFILLED:
        entry = journalled(target.slot, request_id)
        require(entry is not None, f"request {request_id} lost from the journal at {stage}")
        require(entry["refused"] is None, f"request {request_id} refused at {stage}: {entry['refused']}")
    kept = retained(target.slot, request_id)
    if record is not None and before is None:
        require(kept is not None and kept["digest"] == record["digest"]
                and kept["signature"] == record["signature"],
                f"request {request_id} answer not retained across the kill at {stage}")
    checks = authority(request_id, kept) if stage == "before-quorum" else []
    if stage == "before-quorum":
        path = Path(target.slot["data_dir"]) / "attest" / "answers" / (str(request_id) + ".json")
        original = path.read_bytes()
        changed = json.loads(original)
        changed["content_digest"] = "0x" + (bytes.fromhex(changed["content_digest"][2:])[0] ^ 1).to_bytes(1, "big").hex() + changed["content_digest"][4:]
        path.write_text(json.dumps(changed))
        refused_startup = False
        try:
            target.start()
        except Refusal:
            log = target.driver.h.evidence / ("process-" + str(target.driver.h.launches - 1) + ".log")
            refused_startup = "retained answer refused" in log.read_text()
        finally:
            target.stop()
            path.write_bytes(original)
        require(refused_startup, "tampered durable digest did not refuse real process startup")
        checks.append({"case": "tampered-durable-digest", "refused": True})
    if stage == "submitted-no-answer":
        require(before is not None and before["state"] == "signed", "real signed recovery boundary absent")
        path = Path(target.slot["data_dir"]) / "attest" / "answers" / (str(request_id) + ".json")
        require(path.is_file(), "signed request lacks retained answer before crash")
        path.unlink()
    target.start()
    if stage == "before-quorum":
        wait(lambda: served(target.slot, request_id) == record,
             f"request {request_id} restored record after restart")
        require(len(retained(target.slot, request_id)["signatures"]) == 2,
                f"request {request_id} peer signature progress lost across restart")
        require(request_status(endpoint, request_id) != STATUS_FULFILLED,
                f"request {request_id} fulfilled below threshold after restart")
        for sidecar in held:
            sidecar.start()
    wait(lambda: request_status(endpoint, request_id) == STATUS_FULFILLED,
         f"request {request_id} fulfilment after restart at {stage}")
    after = submitted(target.slot, request_id)
    if before is not None:
        require(after is not None and after.get("hash") == before.get("hash")
                and after.get("raw") == before.get("raw") and after.get("nonce") == before.get("nonce"),
                f"request {request_id} recovery changed the journalled transaction identity")
    wait(lambda: submitted(target.slot, request_id) is not None
         and submitted(target.slot, request_id)["state"] in ("fulfilled", "already_fulfilled"),
         "finality-bound submission acknowledgement")
    wait(lambda: all(journalled(sidecar.slot, request_id) is None for sidecar in sidecars),
         f"request {request_id} retirement from every journal")
    stable = submitted(target.slot, request_id)
    require(stable.get("hash") and stable.get("raw"), "completion discarded signed transaction identity")
    receipt = rpc(endpoint, "eth_getTransactionReceipt", [stable["hash"]])
    require(receipt and receipt["status"] == "0x1" and receipt["transactionHash"] == stable["hash"],
            "completed transaction canonical receipt absent")
    mined = int(receipt["blockNumber"], 16)
    require(int(rpc(endpoint, "eth_blockNumber", []), 16) >= mined + 12
            and rpc(endpoint, "eth_getBlockByNumber", [receipt["blockNumber"], False])["hash"] == receipt["blockHash"],
            "completion acknowledged before declared canonical finality")
    topic = "0x" + keccak256(b"XWebFulfilled(uint64,address,bytes32,uint32,uint8,uint8,uint64)").hex()
    query = [{"address": XWEB_PRECOMPILE, "fromBlock": "0x0", "toBlock": "latest",
              "topics": [topic, "0x" + request_id.to_bytes(32, "big").hex()]}]
    require(len(rpc(endpoint, "eth_getLogs", query)) == 1, "observation did not commit exactly once")
    nonce = rpc(endpoint, "eth_getTransactionCount", [authority.sender, "latest"])
    restart_head = int(rpc(endpoint, "eth_blockNumber", []), 16)
    target.kill()
    target.start()
    wait(lambda: int(rpc(endpoint, "eth_blockNumber", []), 16) >= restart_head + 4,
         "completed restart through real attestation rounds")
    require(not attested(target.slot, request_id) and journalled(target.slot, request_id) is None,
            "completed request returned to pending work")
    require(submitted(target.slot, request_id) == stable
            and rpc(endpoint, "eth_getTransactionCount", [authority.sender, "latest"]) == nonce,
            "completed recovery changed transaction or charged another nonce")
    return {"stage": stage, "request_id": request_id, "source": source_before, "signature_authority": checks}


def attestation_failures(owned, sidecars):
    target = sidecars[0]
    for peer in sidecars[1:]:
        peer.stop()
    content = Path(target.slot["data_dir"]) / "content"
    held = content.with_name("content-during-retry")
    require(content.is_dir() and not held.exists(), "genuine content store failure boundary absent")
    content.rename(held)
    try:
        request_id = owned.request_payload(owned.url.encode())
        wait(lambda: (journalled(target.slot, request_id) or {}).get("last_error")
             == "attestation refused: content store", "real retryable store failure")
        failed = journalled(target.slot, request_id)
        require(failed["stage"] == "retry" and 0 < failed["attempts"] < 8
                and failed["next_retry_block"] > failed["request"]["block_number"], "bounded retry state absent")
        target.kill()
        require(journalled(target.slot, request_id) == failed, "retry progress lost on crash")
    finally:
        held.rename(content)
    target.start()
    wait(lambda: retained(target.slot, request_id) is not None, "retry restores independent attestation")
    for peer in sidecars[1:]:
        peer.start()
    wait(lambda: request_status(owned.endpoint, request_id) == STATUS_FULFILLED, "retry reaches genuine fulfilment")
    wait(lambda: all(journalled(peer.slot, request_id) is None for peer in sidecars), "retried request acknowledgement")
    for peer in sidecars[1:]:
        peer.stop()
    owned.stop_origin()
    try:
        fetch_id = owned.request_payload(owned.url.encode())
        wait(lambda: (journalled(target.slot, fetch_id) or {}).get("stage") == "retry"
             and ((journalled(target.slot, fetch_id) or {}).get("last_error") or "").startswith("attestation refused: fetch "),
             "real unavailable origin retry")
        fetch_failed = journalled(target.slot, fetch_id)
        target.kill()
        require(journalled(target.slot, fetch_id) == fetch_failed, "fetch retry disappeared at crash")
    finally:
        owned.start_origin()
    target.start()
    wait(lambda: retained(target.slot, fetch_id) is not None, "fetch retry restored")
    peer_id = owned.request_payload(owned.url.encode())
    wait(lambda: (journalled(target.slot, peer_id) or {}).get("last_error") == "peer quorum unavailable",
         "real unavailable peer retry")
    partial = retained(target.slot, peer_id)
    require(partial and len(partial["signatures"]) == 1 and submitted(target.slot, peer_id) is None,
            "unavailable peers authorized a fulfilment")
    peer_failed = journalled(target.slot, peer_id)
    target.kill()
    require(journalled(target.slot, peer_id) == peer_failed and retained(target.slot, peer_id) == partial,
            "peer retry or partial signature progress disappeared")
    target.start()
    for peer in sidecars[1:]:
        peer.start()
    wait(lambda: all(request_status(owned.endpoint, value) == STATUS_FULFILLED for value in (fetch_id, peer_id)),
         "origin and peer restoration reaches real fulfilment")
    wait(lambda: all(journalled(peer.slot, value) is None for peer in sidecars for value in (fetch_id, peer_id)),
         "origin and peer retry finality acknowledgement")
    refused_id = owned.request_payload(b"\xff")
    wait(lambda: (journalled(target.slot, refused_id) or {}).get("refused")
         == "attestation refused: payload is not UTF-8", "terminal invalid request refusal")
    refused = journalled(target.slot, refused_id)
    target.kill()
    target.start()
    wait(lambda: journalled(target.slot, refused_id) == refused, "explicit refusal survives restart")
    require(submitted(target.slot, refused_id) is None, "invalid request signed an economic action")
    wait(lambda: int(rpc(owned.endpoint, "eth_blockNumber", []), 16) > refused["request"]["timeout_height"],
         "real refundable timeout")
    receipt = owned.rpc.send(owned.evm_key(), 125, XWEB_PRECOMPILE,
        owned.refund_calldata(refused_id))
    require(receipt.get("status") == "0x1", "real native refund failed")
    wait(lambda: request_status(owned.endpoint, refused_id) == 2, "native refunded state")
    wait(lambda: all(journalled(peer.slot, refused_id) is None for peer in sidecars), "finalized refund acknowledgement")
    refund = submitted(target.slot, refused_id)
    require(refund and refund["state"] == "refunded" and "raw" not in refund,
            "refund reconciliation signed an unnecessary transaction")
    target.kill()
    target.start()
    require(submitted(target.slot, refused_id) == refund, "refunded state changed after restart")
    return [{"stage": "retryable-attestation-restart", "request_id": request_id},
            {"stage": "transient-fetch-restart", "request_id": fetch_id},
            {"stage": "transient-peer-restart", "request_id": peer_id},
            {"stage": "terminal-invalid-request-restart", "request_id": refused_id},
            {"stage": "already-refunded-reconciliation", "request_id": refused_id}]


def evm_attestation_recovery(manifest, manifest_path, report):
    attestors = service(manifest, "xweb-attestors")["bindings"]
    evidence = os.environ.get("PAXEER_X_EVIDENCE_DIR")
    require(evidence, "PAXEER_X_EVIDENCE_DIR is not set")
    xweb = load_fixture()
    owned = xweb.XWebFixture(manifest, evidence)
    sidecars = []
    try:
        owned.start()
        report["bindings"] = resolve_bindings(attestors, owned.bindings())
        owned.attach(manifest_path)
        fixture = attachment(manifest, "19.1")
        require(fixture["evm"].get("confirmations") == 12, "fixture EVM confirmations are not 12")
        slots = fixture["web_attestors"]
        require(isinstance(slots, list) and len(slots) == 4, "fixture does not declare four web attestors")
        for slot in slots:
            missing = [field for field in ATTESTOR_FIELDS if not slot.get(field)]
            require(not missing, "web attestor slot lacks " + ", ".join(missing))
        require(len({slot["public_signer"] for slot in slots}) == 4, "web attestor signers are not distinct")
        require(len({slot["data_dir"] for slot in slots}) == 4, "web attestor data paths are not distinct")
        require(fixture["web_threshold"] == 3, "fixture web threshold is not 3 of 4")
        binaries = [Path(ref) for ref in fixture["runtime_binary_or_image_refs"]]
        absent = [ref.name for ref in binaries if not ref.is_file()]
        require(binaries and not absent, "fixture runtime binaries absent: " + ", ".join(absent))
        inventory = fixture["required_case_inventory"].get("evm-attestation-recovery")
        require(isinstance(inventory, dict), "fixture inventory lacks evm-attestation-recovery")
        producer = inventory.get("request_producer")
        require(isinstance(producer, list) and producer, "fixture names no real request producer")
        configs = [Path(ref) for ref in inventory.get("attestor_configs", [])]
        require(len(configs) == 4 and all(path.is_file() for path in configs),
                "fixture lacks four attestor configs")
        keys = [Path(ref) for ref in inventory.get("attestor_keys", [])]
        require(len(keys) == 4 and all(path.is_file() for path in keys), "fixture lacks four attestor keys")
        endpoint = fixture["evm"].get("endpoint")
        require(endpoint, "fixture names no isolated EVM endpoint")
        rpc(endpoint, "eth_blockNumber", [])
        sender = fixture["funded_accounts_and_registration_receipt_refs"]["submitter-1"]["address"]
        secrets = [xweb.evm.read_key(path) for path in keys]
        authority = lambda request_id, answer: signature_authority(xweb.evm, endpoint, request_id, answer,
                                                                   secrets, sender)
        authority.sender = sender
        log_dir = Path(fixture["evidence_output_directory"])
        log_dir.mkdir(parents=True, exist_ok=True, mode=0o700)
        report["evidence"] = str(log_dir / "evm-attestation-recovery.json")
        sidecars = [Sidecar(binaries[0], slot, config, log_dir, index)
                    for index, (slot, config) in enumerate(zip(slots, configs))]
        require(shutil.which("gdb"), "real process interruption requires debugger-enabled artifact and GDB")
        for sidecar in sidecars:
            sidecar.start()
        for stage in STAGES:
            report["results"].append(interrupt_at(stage, sidecars, fixture["web_threshold"],
                                                  endpoint, producer, authority))
        report["results"].extend(attestation_failures(owned, sidecars))
        try:
            owned.canonical_reorg()
        except RuntimeError as unavailable:
            raise Refusal(f"reorg reconciliation below the 12-block depth not exercised: {unavailable}")
        report["results"].append({"stage": "reorg-below-depth", "reconciled": True})
    except RuntimeError as refused:
        raise Refusal(str(refused))
    finally:
        for sidecar in sidecars:
            sidecar.stop()
        owned.close()
        if report.get("evidence"):
            Path(report["evidence"]).write_text(json.dumps(report, indent=2) + "\n")
    require(len(report["results"]) == len(STAGES) + 6, "required EVM recovery cases absent")
    return len(report["results"]) + sum(len(result.get("signature_authority", [])) for result in report["results"])


CASES = {"evm-attestation-recovery": evm_attestation_recovery}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--case", required=True, choices=sorted(CASES))
    parser.add_argument("--candidate-manifest", required=True)
    args = parser.parse_args()
    report = {"case": args.case, "results": []}
    code, tests = 0, 0
    try:
        require(args.candidate_manifest, "candidate manifest path is empty")
        manifest = load_manifest(args.candidate_manifest)
        report["revision"] = manifest["source"]["revision"]
        tests = CASES[args.case](manifest, Path(args.candidate_manifest).resolve(), report)
    except Refusal as refusal:
        print(f"REFUSED {args.case}: {refusal}", file=sys.stderr)
        code = 3
    print(f"revision {report.get('revision', 'unbound')}")
    print("command " + shlex.join([sys.executable, *sys.argv]))
    print(f"exit {code}")
    print(f"evidence {report.get('evidence', 'none')}")
    if code == 0:
        print(f"PAXEER_X_GATE tests={tests} skipped=0")
    return code


if __name__ == "__main__":
    sys.exit(main())
