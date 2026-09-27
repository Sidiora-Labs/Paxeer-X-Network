#!/usr/bin/env bash
# The x-websearch dry run.
#
# One run starts anvil, a loopback web server for the committed pages under
# fixtures/site, a loopback api server that refuses a call without its
# credential, and three sidecars with run-local attestor keys, deploys
# contracts/src/xweb/XWebConsumer.sol and takes three requests end to end:
# a fetch and an api call under the majority level, and an api call under the
# single level answered by the one sidecar it names. Each leg asserts the
# sidecars signed the same origin-1 digest, that the submitter's fulfil
# calldata decodes against precompiles/xweb/abi.json, and that the consumer
# callback, delivered from the impersonated precompile address under the
# request's callback gas, stored the attested answer. The exchange is written
# to fixtures/dry-run.json and replayed with no node and no sidecar.
#
#   dry-run-check.sh            run live, then replay the run and the fixture
#   dry-run-check.sh --record   run live and write the fixture from the run
#   dry-run-check.sh --replay   replay the committed fixture alone
#
# Every port is picked free at start, every process the run starts is stopped
# on exit, nothing leaves the loopback interface and no step is skipped: a
# missing tool stops the run by name.

set -euo pipefail

MODE=live
case "${1-}" in
    "") ;;
    --record) MODE=record ;;
    --replay) MODE=replay ;;
    *)
        printf 'usage: dry-run-check.sh [--record|--replay]\n' >&2
        exit 2
        ;;
esac
if [ "$#" -gt 1 ]; then
    printf 'usage: dry-run-check.sh [--record|--replay]\n' >&2
    exit 2
fi

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
REPO_ROOT=$(CDPATH= cd -- "$SCRIPT_DIR/../../../.." && pwd)
FIXTURE=$SCRIPT_DIR/fixtures/dry-run.json
SITE=$SCRIPT_DIR/fixtures/site
ABI=$REPO_ROOT/precompiles/xweb/abi.json
CONSUMER=contracts/src/xweb/XWebConsumer.sol
CHAIN_ID=1337
CALLBACK_GAS=400000
FEE=1000000000000000
THRESHOLD=2

fail() {
    printf 'dry-run-check: error: %s\n' "$*" >&2
    exit 1
}

note() {
    printf 'dry-run-check: %s\n' "$*"
}

pass() {
    printf 'dry-run-check: ok: %s\n' "$*"
}

unset "${!X_WEBSEARCH_@}" 2>/dev/null || true
unset ETH_RPC_URL http_proxy https_proxy all_proxy HTTP_PROXY HTTPS_PROXY ALL_PROXY 2>/dev/null || true
export no_proxy=127.0.0.1,localhost
export NO_PROXY=127.0.0.1,localhost

require_tools() {
    for tool in "$@"; do
        command -v "$tool" >/dev/null 2>&1 ||
            fail "$tool is required for the dry run and is not on the PATH"
    done
}

require_modules() {
    for module in "$@"; do
        python3 -c "import $module" >/dev/null 2>&1 ||
            fail "the python module $module is required for the dry run and is not installed"
    done
}

require_files() {
    for file in "$@"; do
        [ -e "$file" ] || fail "$file is required for the dry run and is missing"
    done
}

PIDS=()
WORK=

stop_all() {
    if [ "${#PIDS[@]}" -gt 0 ]; then
        for pid in "${PIDS[@]}"; do
            kill "$pid" 2>/dev/null || true
        done
        for pid in "${PIDS[@]}"; do
            wait "$pid" 2>/dev/null || true
        done
    fi
    PIDS=()
}

cleanup() {
    stop_all
    if [ -n "$WORK" ]; then
        rm -rf "$WORK"
    fi
}

free_port() {
    python3 -c 'import socket
sock = socket.socket()
sock.bind(("127.0.0.1", 0))
print(sock.getsockname()[1])
sock.close()'
}

wait_for_port() {
    python3 - "$1" "$2" <<'PORT'
import socket
import sys
import time

port = int(sys.argv[1])
deadline = time.time() + float(sys.argv[2])
while time.time() < deadline:
    try:
        with socket.create_connection(("127.0.0.1", port), timeout=1):
            sys.exit(0)
    except OSError:
        time.sleep(0.2)
sys.exit(1)
PORT
}

wait_for_line() {
    python3 - "$1" "$2" "$3" <<'LINE'
import sys
import time

path, needle = sys.argv[1], sys.argv[2]
deadline = time.time() + float(sys.argv[3])
while time.time() < deadline:
    try:
        with open(path, "r", encoding="utf-8", errors="replace") as handle:
            if needle in handle.read():
                sys.exit(0)
    except OSError:
        pass
    time.sleep(0.2)
sys.exit(1)
LINE
}

require_tools python3
require_modules eth_keys eth_utils cryptography
require_files "$ABI" "$REPO_ROOT/$CONSUMER" "$SITE/index.html" "$SITE/guide.html" \
    "$SITE/notes.txt" "$SITE/robots.txt" "$REPO_ROOT/agent/sdk/python/layerx_sdk/xweb_api.py"

trap cleanup EXIT
WORK=$(mktemp -d)
chmod 0700 "$WORK"
HARNESS=$WORK/harness.py

cat > "$HARNESS" <<'PY'
#!/usr/bin/env python3
"""The dry run's loopback servers, chain front, request legs and replay.

dry-run-check.sh writes this file into the run's work directory and passes it
one command at a time. Nothing here touches anything outside that directory
and the committed fixtures.
"""

from __future__ import annotations

import hashlib
import http.server
import json
import os
import re
import secrets
import socket
import ssl
import sys
import threading
import time
import urllib.error
import urllib.request

ZERO_ADDRESS = "0x" + "00" * 20
PRECOMPILE = "0x0000000000000000000000000000000000001019"
REQUESTED_EVENT = "XWebRequested(uint64,address,uint8,bytes,uint64,uint256,uint64)"
FULFIL_SIGNATURE = "fulfil(uint64,bytes,bytes32,uint32,bytes[])"
CALLBACK_SIGNATURE = "onXWebResponse(uint64,bytes32,uint32,bytes)"
ANSWER_SIGNATURE = "answer(uint64)"
ISSUED_SIGNATURE = "issued(uint64)"
DOMAIN = b"PAXEERX_WEB_V1"
ORIGIN_EVM = 1
PREIMAGE_LENGTH = 188
MAX_RESPONSE_BYTES = 4096
API_ROUTE = "/v1/quote"
API_ANSWER = '["SID/PAX","3.114"]'
PAGE_MARKERS = (
    "Paxeer X web reader dry run",
    "PAX & SID",
    "First landmark: attested fetch",
    "Read the guide page",
)
PAGE_DROPPED = ("dropped element text", "marker-style-drop", "markerScriptDrop", "<p>")


class Failure(Exception):
    """An assertion of the dry run that did not hold."""


def check(condition, message):
    if not condition:
        raise Failure(message)


def opener():
    return urllib.request.build_opener(urllib.request.ProxyHandler({}))


def sdk(root):
    path = os.path.join(root, "agent", "sdk", "python")
    if path not in sys.path:
        sys.path.insert(0, path)
    import layerx_sdk.xweb_api as api

    return api


def keccak(data):
    from eth_utils import keccak as digest

    return digest(bytes(data))


def hex0x(data):
    return "0x" + bytes(data).hex()


def unhex0x(text):
    check(isinstance(text, str) and text.startswith("0x"), f"not 0x hex: {text!r}")
    return bytes.fromhex(text[2:])


def word(value):
    return int(value).to_bytes(32, "big")


def address_word(address):
    raw = unhex0x(address)
    check(len(raw) == 20, f"not an address: {address}")
    return bytes(12) + raw


def dynamic(data):
    return word(len(data)) + bytes(data) + bytes((-len(data)) % 32)


def selector(signature):
    return keccak(signature.encode("ascii"))[:4]


def read_word(data, index):
    check(len(data) >= (index + 1) * 32, f"word {index} is past the end of {len(data)} bytes")
    return data[index * 32 : (index + 1) * 32]


def read_uint(data, index):
    return int.from_bytes(read_word(data, index), "big")


def read_bytes(data, offset):
    check(len(data) >= offset + 32, "a dynamic field starts past the end")
    length = int.from_bytes(data[offset : offset + 32], "big")
    check(len(data) >= offset + 32 + length, "a dynamic field runs past the end")
    return data[offset + 32 : offset + 32 + length]


def preimage(chain_id, requester, request_id, kind, payload, content_digest, response, full_length):
    """The origin-1 attestation preimage modules/xweb/ATTESTATION.md lays out."""
    raw = b"".join(
        [
            DOMAIN,
            bytes([ORIGIN_EVM]),
            word(chain_id),
            address_word(requester),
            int(request_id).to_bytes(8, "big"),
            bytes([kind]),
            keccak(payload),
            bytes(content_digest),
            keccak(response),
            int(full_length).to_bytes(4, "big"),
        ]
    )
    check(len(raw) == PREIMAGE_LENGTH, f"the preimage is {len(raw)} bytes, want {PREIMAGE_LENGTH}")
    return raw


def recover(digest, signature):
    from eth_keys import keys

    check(len(signature) == 65, f"a signature is {len(signature)} bytes, want 65")
    check(signature[64] in (27, 28), f"a signature v is {signature[64]}, want 27 or 28")
    order = 0xFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFEBAAEDCE6AF48A03BBFD25E8CD0364141
    s = int.from_bytes(signature[32:64], "big")
    check(0 < s <= order // 2, "a signature s is above half the group order")
    public = keys.Signature(signature[:64] + bytes([signature[64] - 27]))
    return public.recover_public_key_from_msg_hash(bytes(digest)).to_canonical_address()


def encode_attestors(attestors, threshold):
    """getAttestors() as precompiles/xweb/abi.json declares it."""
    heads = b""
    tails = b""
    base = 32 * len(attestors)
    for attestor in attestors:
        heads += word(base + len(tails))
        payout = attestor["payout"].encode("ascii")
        public_key = unhex0x(attestor["public_key"])
        tails += (
            address_word(attestor["signer"])
            + word(3 * 32)
            + word(3 * 32 + len(dynamic(payout)))
            + dynamic(payout)
            + dynamic(public_key)
        )
    return word(0x40) + word(threshold) + word(len(attestors)) + heads + tails


def encode_request(request):
    """getRequest(uint64) as precompiles/xweb/abi.json declares it."""
    return b"".join(
        [
            word(request["request_id"]),
            address_word(request["requester"]),
            word(request["kind"]),
            keccak(unhex0x(request["payload"])),
            word(request["callback_gas"]),
            word(request["paid"]),
            word(request["block_number"]),
            word(request["timeout_height"]),
            word(request.get("status", 0)),
            word(request.get("level", 0)),
            address_word(request.get("attestor", ZERO_ADDRESS)),
        ]
    )


def request_log(request):
    """The XWebRequested log of a request, in the event's abi encoding."""
    data = b"".join(
        [
            word(request["kind"]),
            word(5 * 32),
            word(request["callback_gas"]),
            word(request["paid"]),
            word(request["timeout_height"]),
            dynamic(unhex0x(request["payload"])),
        ]
    )
    return {
        "address": PRECOMPILE,
        "topics": [
            hex0x(keccak(REQUESTED_EVENT.encode("ascii"))),
            hex0x(word(request["request_id"])),
            hex0x(address_word(request["requester"])),
        ],
        "data": hex0x(data),
        "blockNumber": hex(request["block_number"]),
        "logIndex": "0x0",
        "removed": False,
    }


def abi_function(root, name):
    with open(os.path.join(root, "precompiles", "xweb", "abi.json"), "r", encoding="utf-8") as handle:
        abi = json.load(handle)
    for entry in abi:
        if entry.get("type") == "function" and entry.get("name") == name:
            return entry
    raise Failure(f"precompiles/xweb/abi.json declares no function {name}")


def decode_fulfil(root, calldata):
    """Decodes a fulfil call against the types precompiles/xweb/abi.json declares."""
    entry = abi_function(root, "fulfil")
    types = [field["type"] for field in entry["inputs"]]
    check(
        types == ["uint64", "bytes", "bytes32", "uint32", "bytes[]"],
        f"precompiles/xweb/abi.json declares fulfil{tuple(types)}, which this decoder does not cover",
    )
    signature = entry["name"] + "(" + ",".join(types) + ")"
    check(signature == FULFIL_SIGNATURE, f"the abi declares {signature}")
    check(calldata[:4] == selector(signature), "the calldata is not a fulfil call")
    body = calldata[4:]
    request_id = read_uint(body, 0)
    response_at = read_uint(body, 1)
    content_digest = read_word(body, 2)
    full_length = read_uint(body, 3)
    signatures_at = read_uint(body, 4)
    check(request_id < 1 << 64, "the fulfil request id is not a uint64")
    check(full_length < 1 << 32, "the fulfil full length is not a uint32")
    check(response_at % 32 == 0 and signatures_at % 32 == 0, "a fulfil offset is not word aligned")
    response = read_bytes(body, response_at)
    count = int.from_bytes(body[signatures_at : signatures_at + 32], "big")
    signatures = []
    for index in range(count):
        at = signatures_at + 32 + index * 32
        offset = int.from_bytes(body[at : at + 32], "big")
        check(offset % 32 == 0, "a fulfil signature offset is not word aligned")
        signatures.append(read_bytes(body, signatures_at + 32 + offset))
    check(len(signatures) == count and count > 0, "the fulfil carries no signature")
    return {
        "request_id": request_id,
        "response": response,
        "content_digest": content_digest,
        "full_length": full_length,
        "signatures": signatures,
    }


def decode_request_view(root, answer):
    """Decodes a getRequest answer against the tuple the abi declares."""
    entry = abi_function(root, "getRequest")
    fields = entry["outputs"][0]["components"]
    check(
        [field["type"] for field in fields]
        == ["uint64", "address", "uint8", "bytes32", "uint64", "uint256", "uint64", "uint64", "uint8", "uint8", "address"],
        "precompiles/xweb/abi.json declares a getRequest tuple this decoder does not cover",
    )
    check(
        len(answer) == 32 * len(fields),
        f"the getRequest answer is {len(answer)} bytes, want {32 * len(fields)} for {len(fields)} fields",
    )
    out = {}
    for index, field in enumerate(fields):
        raw = read_word(answer, index)
        if field["type"] == "address":
            check(not any(raw[:12]), f"the getRequest {field['name']} is not an address")
            out[field["name"]] = hex0x(raw[12:])
        elif field["type"] == "bytes32":
            out[field["name"]] = hex0x(raw)
        else:
            out[field["name"]] = int.from_bytes(raw, "big")
    return out


# --- loopback servers -------------------------------------------------------


MEDIA_TYPES = {".html": "text/html; charset=utf-8", ".txt": "text/plain; charset=utf-8"}


class Reply(http.server.BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def reply(self, status, body, media_type):
        self.send_response(status)
        self.send_header("Content-Type", media_type)
        self.send_header("Content-Length", str(len(body)))
        self.send_header("Connection", "close")
        self.end_headers()
        self.wfile.write(body)
        self.close_connection = True

    def read_body(self):
        length = int(self.headers.get("Content-Length") or 0)
        return self.rfile.read(length)

    def log_message(self, fmt, *args):
        sys.stderr.write("%s %s\n" % (self.server.label, fmt % args))
        sys.stderr.flush()


def serve(handler, port, label, **attributes):
    server = http.server.ThreadingHTTPServer(("127.0.0.1", port), handler)
    server.label = label
    for name, value in attributes.items():
        setattr(server, name, value)
    return server


class Site(Reply):
    def do_GET(self):
        target = self.path.split("?", 1)[0]
        if target == "/":
            target = "/index.html"
        root = os.path.abspath(self.server.root)
        full = os.path.abspath(os.path.join(root, target.lstrip("/")))
        if not full.startswith(root + os.sep) or not os.path.isfile(full):
            self.reply(404, b"not found\n", "text/plain; charset=utf-8")
            return
        with open(full, "rb") as handle:
            body = handle.read()
        self.reply(200, body, MEDIA_TYPES.get(os.path.splitext(full)[1], "application/octet-stream"))


class Api(Reply):
    """Answers only a call that carries the credential header, and logs no value."""

    def do_GET(self):
        target = self.path.split("?", 1)[0]
        if target != API_ROUTE:
            self.reply(404, b'{"error":"unknown_route"}', "application/json")
            return
        offered = self.headers.get(self.server.credential_header)
        with self.server.lock:
            self.server.calls += 1
            sequence = self.server.calls
            served = offered == self.server.credential_value
            if served:
                self.server.served += 1
            else:
                self.server.refused += 1
        sys.stderr.write(
            "api call %d: %s\n" % (sequence, "served" if served else "refused without the credential")
        )
        sys.stderr.flush()
        if not served:
            self.reply(401, b'{"error":"credential_required"}', "application/json")
            return
        body = json.dumps(
            {"pair": "SID/PAX", "rate": "3.114", "sequence": sequence, "served": self.server.served},
            separators=(",", ":"),
        ).encode("ascii")
        self.reply(200, body, "application/json")


class Front(Reply):
    """The chain the sidecars read: the precompile's logs and views from the
    requests the run published, every other method proxied to anvil."""

    def do_POST(self):
        raw = self.read_body()
        if self.path == "/control":
            self.reply(200, json.dumps(self.control(json.loads(raw))).encode(), "application/json")
            return
        try:
            call = json.loads(raw)
        except ValueError:
            self.reply(400, b'{"error":"not_json"}', "application/json")
            return
        answer = self.dispatch(call)
        with self.server.lock:
            method = call.get("method")
            self.server.counts[method] = self.server.counts.get(method, 0) + 1
        self.reply(200, json.dumps(answer).encode(), "application/json")

    def control(self, message):
        server = self.server
        operation = message["op"]
        with server.lock:
            if operation == "phase":
                server.phase = message["name"]
                return {"phase": server.phase}
            if operation == "publish":
                request = dict(message["request"])
                server.requests[request["request_id"]] = request
                return {"request": request}
            if operation == "status":
                server.requests[message["request_id"]]["status"] = message["status"]
                return {"status": message["status"]}
            if operation == "dump":
                return {"views": server.views, "counts": server.counts, "sent": server.sent}
        raise Failure(f"unknown control operation {operation!r}")

    def result(self, call, value):
        return {"jsonrpc": "2.0", "id": call.get("id"), "result": value}

    def dispatch(self, call):
        method = call.get("method")
        params = call.get("params") or []
        if method == "eth_getLogs":
            return self.result(call, self.logs(params[0]))
        if method == "eth_call" and (params[0].get("to") or "").lower() == PRECOMPILE:
            return self.result(call, hex0x(self.view(unhex0x(params[0]["data"]))))
        if method == "eth_sendRawTransaction":
            with self.server.lock:
                if params[0] not in self.server.sent:
                    self.server.sent.append(params[0])
        return self.proxy(call)

    def proxy(self, call):
        request = urllib.request.Request(
            self.server.anvil,
            data=json.dumps(call).encode(),
            headers={"Content-Type": "application/json"},
        )
        with opener().open(request, timeout=30) as answer:
            return json.loads(answer.read())

    def logs(self, query):
        if (query.get("address") or "").lower() != PRECOMPILE:
            return []
        topics = query.get("topics") or []
        if topics and topics[0] != hex0x(keccak(REQUESTED_EVENT.encode("ascii"))):
            return []
        low = int(query["fromBlock"], 16)
        high = int(query["toBlock"], 16)
        with self.server.lock:
            requests = sorted(self.server.requests.values(), key=lambda item: item["request_id"])
        return [request_log(request) for request in requests if low <= request["block_number"] <= high]

    def record_view(self, call, request_id, answer):
        with self.server.lock:
            for view in self.server.views:
                if view["call"] == call and view["request_id"] == request_id:
                    return
            self.server.views.append(
                {"phase": self.server.phase, "call": call, "request_id": request_id, "answer": hex0x(answer)}
            )

    def view(self, data):
        if data[:4] == selector("getAttestors()"):
            answer = encode_attestors(self.server.attestors, self.server.threshold)
            self.record_view("getAttestors", 0, answer)
            return answer
        if data[:4] == selector("getRequest(uint64)"):
            request_id = int.from_bytes(data[4:36], "big")
            with self.server.lock:
                request = self.server.requests.get(request_id)
            check(request is not None, f"the front was asked for the unknown request {request_id}")
            answer = encode_request(request)
            self.record_view("getRequest", request_id, answer)
            return answer
        raise Failure(f"the front was asked for an unknown precompile view 0x{data[:4].hex()}")


# --- chain access -----------------------------------------------------------


class Rpc:
    def __init__(self, url):
        self.url = url
        self.next = 0

    def __call__(self, method, params=None):
        self.next += 1
        body = json.dumps(
            {"jsonrpc": "2.0", "id": self.next, "method": method, "params": params or []}
        ).encode()
        request = urllib.request.Request(
            self.url, data=body, headers={"Content-Type": "application/json"}
        )
        with opener().open(request, timeout=60) as answer:
            out = json.loads(answer.read())
        check("error" not in out, f"{method} failed: {out.get('error')}")
        return out["result"]


def receipt_of(rpc, transaction, what):
    for _ in range(240):
        receipt = rpc("eth_getTransactionReceipt", [transaction])
        if receipt is not None:
            check(receipt["status"] == "0x1", f"{what} reverted: {json.dumps(receipt)}")
            return receipt
        time.sleep(0.25)
    raise Failure(f"{what} was not mined")


def context(path):
    with open(path, "r", encoding="utf-8") as handle:
        return json.load(handle)


def get(url, timeout=15, cafile=None):
    handlers = [urllib.request.ProxyHandler({})]
    if cafile is not None:
        handlers.append(urllib.request.HTTPSHandler(context=ssl.create_default_context(cafile=cafile)))
    try:
        request = urllib.request.Request(url, headers={"Accept": "application/json"})
        with urllib.request.build_opener(*handlers).open(request, timeout=timeout) as answer:
            return answer.status, answer.read()
    except urllib.error.HTTPError as error:
        return error.code, error.read()
    except urllib.error.URLError as error:
        raise Failure(f"{url} could not be reached: {error.reason}") from None


def control(ctx, message):
    request = urllib.request.Request(
        ctx["front_url"] + "/control",
        data=json.dumps(message).encode(),
        headers={"Content-Type": "application/json"},
    )
    with opener().open(request, timeout=60) as answer:
        return json.loads(answer.read())


def publish(ctx, rpc, request_id, kind, payload, level, named):
    """The request the precompile would have stored, emitted and marked issued."""
    height = int(rpc("eth_blockNumber"), 16)
    request = {
        "request_id": request_id,
        "requester": ctx["consumer"],
        "kind": kind,
        "payload": hex0x(payload),
        "callback_gas": ctx["callback_gas"],
        "paid": ctx["fee"],
        "block_number": height,
        "timeout_height": height + 1000,
        "status": 0,
        "level": 0 if level == "majority" else 1,
        "attestor": ZERO_ADDRESS if named is None else named,
    }
    control(ctx, {"op": "publish", "request": request})
    slot = keccak(word(request_id) + word(0))
    rpc("anvil_setStorageAt", [ctx["consumer"], hex0x(slot), hex0x(word(1))])
    rpc("anvil_mine", [hex(2)])
    return request


def attestation(ctx, index, request_id):
    status, body = get(f"{ctx['sidecars'][index]['url']}/attestations/{request_id}")
    return status, (json.loads(body) if status == 200 else body.decode("utf-8", "replace"))


def last_errors(ctx):
    lines = []
    for sidecar in ctx["sidecars"]:
        try:
            with open(sidecar["log"], "r", encoding="utf-8", errors="replace") as handle:
                found = [
                    line.strip()
                    for line in handle
                    if "refused" in line or "could not" in line or "x-websearch request" in line
                ]
        except OSError:
            found = []
        if found:
            lines.append(f"{sidecar['name']}: {found[-1]}")
    return ("\n  " + "\n  ".join(lines)) if lines else ""


def wait_records(ctx, request_id, wanted, seconds=150):
    deadline = time.time() + seconds
    records = {}
    while time.time() < deadline:
        for index in wanted:
            if index in records:
                continue
            status, body = attestation(ctx, index, request_id)
            if status == 200:
                records[index] = body
        if len(records) == len(wanted):
            return records
        time.sleep(1)
    missing = [ctx["sidecars"][index]["name"] for index in wanted if index not in records]
    raise Failure(
        f"request {request_id}: {', '.join(missing)} served no attestation within {seconds}s"
        + last_errors(ctx)
    )


def wait_fulfil(ctx, rpc, request_id, seconds=150):
    """The fulfil transaction the submitting sidecar signed and broadcast."""
    deadline = time.time() + seconds
    while time.time() < deadline:
        for raw in control(ctx, {"op": "dump"})["sent"]:
            transaction = rpc("eth_getTransactionByHash", [hex0x(keccak(unhex0x(raw)))])
            if transaction is None:
                continue
            data = unhex0x(transaction["input"])
            if data[:4] != selector(FULFIL_SIGNATURE):
                continue
            if int.from_bytes(data[4:36], "big") != request_id:
                continue
            check(
                (transaction["to"] or "").lower() == PRECOMPILE,
                f"the fulfil of request {request_id} went to {transaction['to']}",
            )
            receipt = receipt_of(rpc, transaction["hash"], f"the fulfil of request {request_id}")
            return transaction, receipt
        time.sleep(1)
    raise Failure(
        f"request {request_id}: no fulfil transaction reached the chain within {seconds}s"
        + last_errors(ctx)
    )


def check_answers(ctx, request, records, fulfilled, level, named):
    """One digest, one answer, valid ascending signatures over the origin-1 preimage."""
    for name in ("digest", "content_digest", "response_hash", "full_length"):
        values = {index: record[name] for index, record in records.items()}
        check(len(set(values.values())) == 1, f"the sidecars disagree on {name}: {values}")
    first = next(iter(records.values()))
    check(
        unhex0x(first["content_digest"]) == fulfilled["content_digest"],
        "the fulfil carries another content digest than the attestations",
    )
    check(
        first["full_length"] == fulfilled["full_length"],
        "the fulfil carries another full length than the attestations",
    )
    check(
        unhex0x(first["response_hash"]) == keccak(fulfilled["response"]),
        "the fulfil carries another response than the attestations",
    )
    check(
        len(fulfilled["response"]) <= MAX_RESPONSE_BYTES,
        f"the fulfil carries {len(fulfilled['response'])} response bytes, bound {MAX_RESPONSE_BYTES}",
    )
    digest = keccak(
        preimage(
            ctx["chain_id"],
            request["requester"],
            request["request_id"],
            request["kind"],
            unhex0x(request["payload"]),
            fulfilled["content_digest"],
            fulfilled["response"],
            fulfilled["full_length"],
        )
    )
    check(
        digest == unhex0x(first["digest"]),
        "the origin-1 preimage of the fulfilled answer does not hash to the signed digest",
    )
    for index, record in records.items():
        signer = recover(unhex0x(record["digest"]), unhex0x(record["signature"]))
        check(
            hex0x(signer) == record["signer"],
            f"{ctx['sidecars'][index]['name']} served a signature of {hex0x(signer)} claiming {record['signer']}",
        )
    registered = [attestor["signer"] for attestor in ctx["attestors"]]
    signers = [hex0x(recover(digest, signature)) for signature in fulfilled["signatures"]]
    check(
        all(signers[index] < signers[index + 1] for index in range(len(signers) - 1)),
        f"the fulfil signers are not in strictly ascending order: {signers}",
    )
    for signer in signers:
        check(signer in registered, f"the fulfil carries the unregistered signer {signer}")
    if level == "majority":
        check(
            len(signers) >= ctx["threshold"],
            f"the majority fulfil carries {len(signers)} signatures, threshold {ctx['threshold']}",
        )
    else:
        check(len(signers) == 1, f"the single level fulfil carries {len(signers)} signatures")
        check(signers[0] == named, f"the single level fulfil was signed by {signers[0]}, not {named}")
    return signers


def deliver(ctx, rpc, request, fulfilled):
    """The consumer callback the precompile makes, from its address, under the
    request's callback gas."""
    data = selector(CALLBACK_SIGNATURE) + b"".join(
        [
            word(fulfilled["request_id"]),
            fulfilled["content_digest"],
            word(fulfilled["full_length"]),
            word(4 * 32),
            dynamic(fulfilled["response"]),
        ]
    )
    rpc("anvil_setBalance", [PRECOMPILE, hex(10**18)])
    rpc("anvil_impersonateAccount", [PRECOMPILE])
    transaction = rpc(
        "eth_sendTransaction",
        [
            {
                "from": PRECOMPILE,
                "to": ctx["consumer"],
                "data": hex0x(data),
                "gas": hex(request["callback_gas"]),
            }
        ],
    )
    receipt = receipt_of(rpc, transaction, f"the callback of request {request['request_id']}")
    rpc("anvil_stopImpersonatingAccount", [PRECOMPILE])
    used = int(receipt["gasUsed"], 16)
    check(
        used <= request["callback_gas"],
        f"the callback used {used} gas, above the request's {request['callback_gas']}",
    )
    return {
        "from": PRECOMPILE,
        "to": ctx["consumer"],
        "data": hex0x(data),
        "gas": request["callback_gas"],
        "gas_used": used,
        "status": receipt["status"],
    }


def stored_answer(ctx, rpc, request_id):
    raw = unhex0x(
        rpc(
            "eth_call",
            [{"to": ctx["consumer"], "data": hex0x(selector(ANSWER_SIGNATURE) + word(request_id))}, "latest"],
        )
    )
    check(read_uint(raw, 0) == 32, "the consumer answer is not one dynamic tuple")
    body = raw[32:]
    issued = unhex0x(
        rpc(
            "eth_call",
            [{"to": ctx["consumer"], "data": hex0x(selector(ISSUED_SIGNATURE) + word(request_id))}, "latest"],
        )
    )
    return {
        "issued": int.from_bytes(issued, "big") == 1,
        "answered": read_uint(body, 0) == 1,
        "content_digest": hex0x(read_word(body, 1)),
        "full_length": read_uint(body, 2),
        "response": hex0x(read_bytes(body, read_uint(body, 3))),
    }


def check_stored(stored, fulfilled):
    check(stored["issued"], "the consumer does not hold the request as issued")
    check(stored["answered"], "the consumer stored no answer for the request")
    check(
        unhex0x(stored["content_digest"]) == fulfilled["content_digest"],
        "the consumer stored another content digest than the fulfil carried",
    )
    check(
        stored["full_length"] == fulfilled["full_length"],
        "the consumer stored another full length than the fulfil carried",
    )
    check(
        unhex0x(stored["response"]) == fulfilled["response"],
        "the consumer stored another response than the fulfil carried",
    )


def leg(ctx, rpc, name, request_id, kind, payload, level, named, wanted):
    control(ctx, {"op": "phase", "name": name})
    request = publish(ctx, rpc, request_id, kind, payload, level, named)
    records = wait_records(ctx, request_id, wanted)
    transaction, receipt = wait_fulfil(ctx, rpc, request_id)
    fulfilled = decode_fulfil(ctx["repo_root"], unhex0x(transaction["input"]))
    check(fulfilled["request_id"] == request_id, "the fulfil answers another request")
    signers = check_answers(ctx, request, records, fulfilled, level, named)
    control(ctx, {"op": "status", "request_id": request_id, "status": 1})
    callback = deliver(ctx, rpc, request, fulfilled)
    stored = stored_answer(ctx, rpc, request_id)
    check_stored(stored, fulfilled)
    silent = [
        ctx["sidecars"][index]["name"] for index in range(len(ctx["sidecars"])) if index not in wanted
    ]
    for index in range(len(ctx["sidecars"])):
        if index in wanted:
            continue
        status, _ = attestation(ctx, index, request_id)
        check(
            status == 404,
            f"{ctx['sidecars'][index]['name']} attested a request the single level names another sidecar for",
        )
    return {
        "phase": name,
        "level": level,
        "named": named,
        "request": request,
        "log": request_log(request),
        "attestations": [records[index] for index in sorted(records)],
        "silent": silent,
        "fulfil": {
            "from": transaction["from"],
            "to": transaction["to"],
            "gas": transaction["gas"],
            "calldata": transaction["input"],
            "status": receipt["status"],
            "signers": signers,
        },
        "callback": callback,
        "stored": stored,
        "response_text": fulfilled["response"].decode("utf-8", "replace"),
    }


def check_page(text):
    for marker in PAGE_MARKERS:
        check(marker in text, f"the attested page text is missing {marker!r}")
    for dropped in PAGE_DROPPED:
        check(dropped not in text, f"the attested page text carries the dropped {dropped!r}")


def api_request(ctx, single=None):
    api = sdk(ctx["repo_root"])
    attestors = api.XWebAttestorSet(
        tuple(
            api.XWebAttestor(entry["signer"], entry["payout"], unhex0x(entry["public_key"]))
            for entry in ctx["attestors"]
        ),
        ctx["threshold"],
    )
    call = api.XWebApiCall("GET", ctx["api_url"] + API_ROUTE, (), b"", ("/pair", "/rate"), single)
    built = api.build_xweb_api_request(
        call,
        attestors,
        credential=[api.XWebApiHeader(ctx["credential_header"], ctx["credential_value"])],
    )
    wanted = 1 if single else len(ctx["attestors"])
    check(
        len(built.envelopes) == wanted,
        f"the api payload carries {len(built.envelopes)} envelopes, want {wanted}",
    )
    check(
        ctx["credential_value"].encode("utf-8") not in built.payload,
        "the api payload carries the credential in the clear",
    )
    return built


def credential_free(ctx, text):
    """No file a sidecar wrote, no line it logged and nothing recorded carries
    the credential."""
    secret = ctx["credential_value"]
    token = secret.split(" ")[-1]
    files = 0
    for sidecar in ctx["sidecars"]:
        for base, _, names in os.walk(sidecar["data_dir"]):
            for name in names:
                path = os.path.join(base, name)
                with open(path, "rb") as handle:
                    body = handle.read()
                files += 1
                for needle in (secret, token):
                    check(needle.encode("utf-8") not in body, f"{path} carries the credential")
        with open(sidecar["log"], "rb") as handle:
            log = handle.read()
        for needle in (secret, token):
            check(needle.encode("utf-8") not in log, f"{sidecar['log']} carries the credential")
    for needle in (secret, token):
        check(needle not in text, "the recorded exchange carries the credential")
    check(files > 0, "the sidecars wrote no file to scan")
    return files


def publication_safe(ctx, text):
    """The record carries no key material, no date, no host name and no
    endpoint but the loopback ones the run started."""
    for secret in ctx["secrets"]:
        check(secret.lower() not in text.lower(), "the recorded exchange carries key material")
    check(re.search(r"(19|20)\d{2}-\d{2}-\d{2}", text) is None, "the recorded exchange carries a date")
    check(re.search(r"\b\d{2}:\d{2}:\d{2}\b", text) is None, "the recorded exchange carries a time of day")
    host = socket.gethostname()
    check(host.lower() not in text.lower(), "the recorded exchange carries the host name")
    for endpoint in set(re.findall(r"https?://[^\"' \\]+", text)):
        check(
            endpoint.startswith("http://127.0.0.1:") or endpoint.startswith("https://127.0.0.1:"),
            f"the recorded exchange carries the endpoint {endpoint}",
        )
    for path in set(re.findall(r"/(?:root|home|tmp|var|usr|etc)/[A-Za-z0-9_./-]+", text)):
        raise Failure(f"the recorded exchange carries the local path {path}")


def run(ctx_path):
    ctx = context(ctx_path)
    rpc = Rpc(ctx["anvil_url"])

    status, body = get(ctx["api_url"] + API_ROUTE, cafile=ctx["api_cert"])
    check(status == 401, f"the api server answered {status} without the credential, want 401")
    check(b"credential_required" in body, "the api server refused without naming the credential")

    legs = []
    fetch = leg(
        ctx, rpc, "fetch", 1, 1, (ctx["site_url"] + "/index.html").encode("ascii"), "majority", None, [0, 1, 2]
    )
    check_page(fetch["response_text"])
    legs.append(fetch)

    majority = api_request(ctx)
    api_majority = leg(ctx, rpc, "api-majority", 2, 3, majority.payload, "majority", None, [0, 1, 2])
    check(
        api_majority["response_text"] == API_ANSWER,
        f"the api answer is {api_majority['response_text']!r}, want {API_ANSWER!r}",
    )
    legs.append(api_majority)

    named = ctx["sidecars"][0]["attestor"]
    single = api_request(ctx, single=named)
    api_single = leg(ctx, rpc, "api-single", 3, 3, single.payload, "single", named, [0])
    check(
        api_single["response_text"] == API_ANSWER,
        f"the single level api answer is {api_single['response_text']!r}, want {API_ANSWER!r}",
    )
    legs.append(api_single)

    pages = {}
    for name in sorted(os.listdir(ctx["site"])):
        with open(os.path.join(ctx["site"], name), "rb") as handle:
            pages[name] = hex0x(hashlib.sha256(handle.read()).digest())

    dump = control(ctx, {"op": "dump"})
    recording = {
        "version": 1,
        "chain_id": ctx["chain_id"],
        "precompile": PRECOMPILE,
        "consumer": ctx["consumer"],
        "threshold": ctx["threshold"],
        "callback_gas": ctx["callback_gas"],
        "attestors": ctx["attestors"],
        "submitter": ctx["submitter"],
        "credential_header": ctx["credential_header"],
        "pages": pages,
        "legs": legs,
        "views": dump["views"],
        "calls": dump["counts"],
    }
    text = json.dumps(recording, indent=2, sort_keys=True) + "\n"
    files = credential_free(ctx, text)
    publication_safe(ctx, text)
    recording["credential_scan"] = {"files": files, "logs": len(ctx["sidecars"]), "hits": 0}
    text = json.dumps(recording, indent=2, sort_keys=True) + "\n"
    with open(ctx["recording"], "w", encoding="utf-8") as handle:
        handle.write(text)
    print(f"dry-run-check: recorded {len(legs)} legs and {len(dump['views'])} precompile views")
    print(f"dry-run-check: scanned {files} sidecar files and {len(ctx['sidecars'])} logs for the credential")


def replay(fixture_path, root, site):
    """Checks the recorded exchange again with no node and no sidecar."""
    from eth_keys import keys

    with open(fixture_path, "r", encoding="utf-8") as handle:
        text = handle.read()
    recording = json.loads(text)
    check(recording["version"] == 1, f"the record is version {recording['version']}, want 1")
    check(recording["precompile"] == PRECOMPILE, "the record names another precompile")
    for name, digest in recording["pages"].items():
        with open(os.path.join(site, name), "rb") as handle:
            actual = hex0x(hashlib.sha256(handle.read()).digest())
        check(actual == digest, f"the committed page {name} no longer hashes to the recorded digest")
    registered = [attestor["signer"] for attestor in recording["attestors"]]
    for attestor in recording["attestors"]:
        public = keys.PublicKey.from_compressed_bytes(unhex0x(attestor["public_key"]))
        check(
            hex0x(public.to_canonical_address()) == attestor["signer"],
            f"the recorded public key of {attestor['signer']} belongs to another address",
        )
    check(
        sorted(registered) == registered and len(set(registered)) == len(registered),
        "the recorded attestor set is not a set in ascending order",
    )
    for view in recording["views"]:
        answer = unhex0x(view["answer"])
        if view["call"] == "getAttestors":
            check(
                answer == encode_attestors(recording["attestors"], recording["threshold"]),
                "a recorded getAttestors answer does not encode the recorded set",
            )
        else:
            decoded = decode_request_view(root, answer)
            check(decoded["id"] == view["request_id"], "a recorded getRequest answer holds another id")
    for record in recording["legs"]:
        request = record["request"]
        check(
            record["log"] == request_log(request),
            f"the recorded log of request {request['request_id']} does not encode the request",
        )
        fulfilled = decode_fulfil(root, unhex0x(record["fulfil"]["calldata"]))
        check(fulfilled["request_id"] == request["request_id"], "a recorded fulfil answers another request")
        check(record["fulfil"]["to"].lower() == PRECOMPILE, "a recorded fulfil went elsewhere")
        check(record["fulfil"]["status"] == "0x1", "a recorded fulfil did not succeed")
        digest = keccak(
            preimage(
                recording["chain_id"],
                request["requester"],
                request["request_id"],
                request["kind"],
                unhex0x(request["payload"]),
                fulfilled["content_digest"],
                fulfilled["response"],
                fulfilled["full_length"],
            )
        )
        for attestation_record in record["attestations"]:
            check(
                attestation_record["request_id"] == request["request_id"],
                "a recorded attestation answers another request",
            )
            check(unhex0x(attestation_record["digest"]) == digest, "a recorded attestation signed another digest")
            check(
                unhex0x(attestation_record["response_hash"]) == keccak(fulfilled["response"]),
                "a recorded attestation hashed another response",
            )
            check(
                attestation_record["full_length"] == fulfilled["full_length"],
                "a recorded attestation carries another full length",
            )
            signer = recover(digest, unhex0x(attestation_record["signature"]))
            check(hex0x(signer) == attestation_record["signer"], "a recorded attestation claims another signer")
            check(hex0x(signer) in registered, "a recorded attestation is from an unregistered signer")
        signers = [hex0x(recover(digest, signature)) for signature in fulfilled["signatures"]]
        check(signers == record["fulfil"]["signers"], "a recorded fulfil recovers other signers than recorded")
        check(
            all(signers[index] < signers[index + 1] for index in range(len(signers) - 1)),
            "a recorded fulfil is not in ascending signer order",
        )
        for signer in signers:
            check(signer in registered, f"a recorded fulfil carries the unregistered signer {signer}")
        if record["level"] == "majority":
            check(len(signers) >= recording["threshold"], "a recorded majority fulfil is below the threshold")
            check(record["named"] is None, "a recorded majority fulfil names an attestor")
        else:
            check(
                len(signers) == 1 and signers[0] == record["named"],
                "a recorded single level fulfil was not signed by the named attestor alone",
            )
            check(record["silent"], "a recorded single level leg has no silent sidecar")
        callback = unhex0x(record["callback"]["data"])
        check(callback[:4] == selector(CALLBACK_SIGNATURE), "a recorded callback is not an onXWebResponse call")
        body = callback[4:]
        check(read_uint(body, 0) == request["request_id"], "a recorded callback answers another request")
        check(read_word(body, 1) == fulfilled["content_digest"], "a recorded callback carries another content digest")
        check(read_uint(body, 2) == fulfilled["full_length"], "a recorded callback carries another full length")
        check(
            read_bytes(body, read_uint(body, 3)) == fulfilled["response"],
            "a recorded callback carries another response",
        )
        check(record["callback"]["from"] == PRECOMPILE, "a recorded callback came from another address")
        check(record["callback"]["to"] == recording["consumer"], "a recorded callback went to another contract")
        check(record["callback"]["status"] == "0x1", "a recorded callback did not succeed")
        check(
            record["callback"]["gas_used"] <= record["callback"]["gas"] == recording["callback_gas"],
            "a recorded callback used more than the request's bounded gas",
        )
        stored = record["stored"]
        check(stored["issued"] and stored["answered"], "a recorded consumer answer was not stored")
        check(unhex0x(stored["response"]) == fulfilled["response"], "a recorded consumer answer holds another response")
        check(len(fulfilled["response"]) <= MAX_RESPONSE_BYTES, "a recorded response is above the stored bound")
    check(
        re.search(r"(19|20)\d{2}-\d{2}-\d{2}", text) is None
        and re.search(r"\b\d{2}:\d{2}:\d{2}\b", text) is None,
        "the record carries a date or a time of day",
    )
    for endpoint in set(re.findall(r"https?://[^\"' \\]+", text)):
        check(
            endpoint.startswith("http://127.0.0.1:") or endpoint.startswith("https://127.0.0.1:"),
            f"the record carries the endpoint {endpoint}",
        )
    check(recording["credential_scan"]["hits"] == 0, "the recorded credential scan found the credential")
    check(recording["credential_scan"]["files"] > 0, "the recorded credential scan read no file")
    print(f"dry-run-check: replayed {len(recording['legs'])} legs and {len(recording['views'])} views")


# --- setup ------------------------------------------------------------------


def keygen(out_path):
    from cryptography.hazmat.primitives import serialization
    from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey
    from eth_keys import keys

    def secp():
        raw = secrets.token_bytes(32)
        key = keys.PrivateKey(raw)
        return {
            "secret": raw.hex(),
            "address": hex0x(key.public_key.to_canonical_address()),
            "public_key": hex0x(key.public_key.to_compressed_bytes()),
        }

    material = {
        "attestors": sorted((secp() for _ in range(3)), key=lambda entry: entry["address"]),
        "submitter": secp(),
        "deployer": secp(),
        "receivers": [secrets.token_bytes(32).hex() for _ in range(3)],
        "credential": "bearer " + secrets.token_hex(24),
        "sequencer_id": secrets.token_hex(32),
        "sequencer_public_key": Ed25519PrivateKey.generate()
        .public_key()
        .public_bytes(serialization.Encoding.Raw, serialization.PublicFormat.Raw)
        .hex(),
    }
    with open(out_path, "w", encoding="utf-8") as handle:
        json.dump(material, handle, indent=2)
    os.chmod(out_path, 0o600)


def build_context(keys_path, out_path):
    keys = context(keys_path)
    work = os.environ["DRY_WORK"]
    ports = os.environ["DRY_SIDECAR_PORTS"].split()
    sidecars = []
    for index, port in enumerate(ports):
        directory = os.path.join(work, f"sidecar-{index + 1}")
        sidecars.append(
            {
                "name": f"sidecar-{index + 1}",
                "listen": f"127.0.0.1:{port}",
                "url": f"http://127.0.0.1:{port}",
                "data_dir": os.path.join(directory, "data"),
                "log": os.path.join(directory, "sidecar.log"),
                "attestor": keys["attestors"][index]["address"],
                "submitter": index == 0,
            }
        )
    ctx = {
        "repo_root": os.environ["DRY_REPO_ROOT"],
        "site": os.environ["DRY_SITE"],
        "work": work,
        "chain_id": int(os.environ["DRY_CHAIN_ID"]),
        "callback_gas": int(os.environ["DRY_CALLBACK_GAS"]),
        "fee": int(os.environ["DRY_FEE"]),
        "threshold": int(os.environ["DRY_THRESHOLD"]),
        "anvil_url": "http://127.0.0.1:" + os.environ["DRY_ANVIL_PORT"],
        "front_port": int(os.environ["DRY_FRONT_PORT"]),
        "front_url": "http://127.0.0.1:" + os.environ["DRY_FRONT_PORT"],
        "site_url": "http://127.0.0.1:" + os.environ["DRY_SITE_PORT"],
        "api_port": int(os.environ["DRY_API_PORT"]),
        "api_url": "https://127.0.0.1:" + os.environ["DRY_API_PORT"],
        "api_cert": os.environ["DRY_API_CERT"],
        "api_key": os.environ["DRY_API_KEY"],
        "recording": os.environ["DRY_RECORDING"],
        "credential_header": "authorization",
        "credential_value": keys["credential"],
        "sequencer_id": keys["sequencer_id"],
        "sequencer_public_key": keys["sequencer_public_key"],
        "attestors": [
            {
                "signer": entry["address"],
                "payout": f"pax1dryrun{index + 1}",
                "public_key": entry["public_key"],
            }
            for index, entry in enumerate(keys["attestors"])
        ],
        "submitter": keys["submitter"]["address"],
        "deployer": keys["deployer"]["address"],
        "sidecars": sidecars,
        "consumer": ZERO_ADDRESS,
        "secrets": [entry["secret"] for entry in keys["attestors"]]
        + [keys["submitter"]["secret"], keys["deployer"]["secret"]]
        + keys["receivers"],
    }
    with open(out_path, "w", encoding="utf-8") as handle:
        json.dump(ctx, handle, indent=2)
    os.chmod(out_path, 0o600)


def keyfiles(keys_path, index, directory):
    keys = context(keys_path)
    written = {
        "attestor.key": keys["attestors"][index]["secret"],
        "receiver.key": keys["receivers"][index],
    }
    if index == 0:
        written["submitter.key"] = keys["submitter"]["secret"]
    for name, secret in written.items():
        path = os.path.join(directory, name)
        with open(path, "w", encoding="ascii") as handle:
            handle.write(secret)
        os.chmod(path, 0o600)


def config(ctx_path, index, out_path):
    ctx = context(ctx_path)
    sidecar = ctx["sidecars"][index]
    settings = {
        "listen": sidecar["listen"],
        "data_dir": sidecar["data_dir"],
        "seeds": [ctx["site_url"] + "/index.html"],
        "crawl": {
            "pages_per_cycle": 8,
            "pages_per_host": 8,
            "max_depth": 1,
            "politeness_delay_ms": 100,
        },
        "crawl_interval_seconds": 3600,
        "fetch": {
            "connect_timeout_ms": 3000,
            "total_timeout_ms": 10000,
            "max_body_bytes": 262144,
            "max_redirects": 3,
            "allow_loopback": True,
        },
        "assets": {
            name: {"asset_id": hashlib.sha256(name.encode("ascii")).hexdigest(), "price": price}
            for name, price in (
                ("SID", "3114"),
                ("PAX", "1000000000000000"),
                ("USDC", "1000"),
                ("USDL", "1000"),
            )
        },
        "gateway": {
            "endpoint": ctx["front_url"] + "/gateway",
            "sequencer_id": ctx["sequencer_id"],
            "sequencer_public_key": ctx["sequencer_public_key"],
        },
        "evm": {"endpoint": ctx["front_url"], "chain_id": ctx["chain_id"], "confirmations": 1},
        "kernel_network_id": 1,
        "peers": [peer["url"] for other, peer in enumerate(ctx["sidecars"]) if other != index],
    }
    with open(out_path, "w", encoding="utf-8") as handle:
        json.dump(settings, handle, indent=2)


def deploy(ctx_path, bytecode_path):
    ctx = context(ctx_path)
    rpc = Rpc(ctx["anvil_url"])
    with open(bytecode_path, "r", encoding="utf-8") as handle:
        bytecode = handle.read().strip()
    rpc("anvil_setBalance", [ctx["deployer"], hex(10**20)])
    rpc("anvil_setBalance", [ctx["submitter"], hex(10**20)])
    rpc("anvil_impersonateAccount", [ctx["deployer"]])
    transaction = rpc(
        "eth_sendTransaction", [{"from": ctx["deployer"], "data": bytecode, "gas": hex(3_000_000)}]
    )
    receipt = receipt_of(rpc, transaction, "the consumer deployment")
    rpc("anvil_stopImpersonatingAccount", [ctx["deployer"]])
    consumer = receipt["contractAddress"]
    check(len(rpc("eth_getCode", [consumer, "latest"])) > 2, "the deployed consumer carries no code")
    check(
        rpc("eth_getCode", [PRECOMPILE, "latest"]) == "0x",
        "the precompile address carries code on the throwaway chain",
    )
    ctx["consumer"] = consumer
    with open(ctx_path, "w", encoding="utf-8") as handle:
        json.dump(ctx, handle, indent=2)
    print(consumer)


def site_server(root, port):
    serve(Site, port, "site", root=root).serve_forever()


def api_server(ctx_path):
    ctx = context(ctx_path)
    server = serve(
        Api,
        ctx["api_port"],
        "api",
        credential_header=ctx["credential_header"],
        credential_value=ctx["credential_value"],
        lock=threading.Lock(),
        calls=0,
        served=0,
        refused=0,
    )
    tls = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
    tls.load_cert_chain(ctx["api_cert"], ctx["api_key"])
    server.socket = tls.wrap_socket(server.socket, server_side=True)
    server.serve_forever()


def front_server(ctx_path):
    ctx = context(ctx_path)
    serve(
        Front,
        ctx["front_port"],
        "front",
        anvil=ctx["anvil_url"],
        attestors=ctx["attestors"],
        threshold=ctx["threshold"],
        requests={},
        views=[],
        counts={},
        sent=[],
        phase="setup",
        lock=threading.Lock(),
    ).serve_forever()


def main(argv):
    command = argv[1]
    if command == "keygen":
        keygen(argv[2])
    elif command == "context":
        build_context(argv[2], argv[3])
    elif command == "keyfiles":
        keyfiles(argv[2], int(argv[3]), argv[4])
    elif command == "config":
        config(argv[2], int(argv[3]), argv[4])
    elif command == "deploy":
        deploy(argv[2], argv[3])
    elif command == "site":
        site_server(argv[2], int(argv[3]))
    elif command == "api":
        api_server(argv[2])
    elif command == "front":
        front_server(argv[2])
    elif command == "run":
        run(argv[2])
    elif command == "replay":
        replay(argv[2], argv[3], argv[4])
    else:
        raise Failure(f"unknown harness command {command!r}")
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main(sys.argv))
    except Failure as failure:
        sys.stderr.write(f"dry-run-check: error: {failure}\n")
        sys.exit(1)
PY

if [ "$MODE" = replay ]; then
    require_files "$FIXTURE"
    python3 "$HARNESS" replay "$FIXTURE" "$REPO_ROOT" "$SITE"
    pass "the committed exchange replays with no node and no sidecar"
    exit 0
fi

require_tools anvil forge cargo openssl

: "${CARGO_TARGET_DIR:=$REPO_ROOT/interop/target}"
export CARGO_TARGET_DIR
note "building the sidecar"
if ! cargo build --locked --manifest-path "$REPO_ROOT/interop/Cargo.toml" \
    --package x-websearch --bin x-websearch >"$WORK/build.log" 2>&1; then
    cat "$WORK/build.log" >&2
    fail "the x-websearch binary did not build"
fi
BINARY=$CARGO_TARGET_DIR/debug/x-websearch
[ -x "$BINARY" ] || fail "$BINARY was not built"

note "compiling the consumer"
if ! (
    cd "$REPO_ROOT" &&
        FOUNDRY_CONFIG=$REPO_ROOT/foundry.paxeer.toml \
            FOUNDRY_OUT=$WORK/out FOUNDRY_CACHE_PATH=$WORK/cache \
            forge inspect "$CONSUMER:XWebConsumer" bytecode --evm-version prague
) >"$WORK/consumer.hex" 2>"$WORK/forge.log"; then
    cat "$WORK/forge.log" >&2
    fail "$CONSUMER did not compile"
fi
grep -q '^0x[0-9a-f]\{64,\}$' "$WORK/consumer.hex" || fail "$CONSUMER produced no creation bytecode"

openssl req -x509 -newkey rsa:2048 -nodes -days 1 -subj '/CN=127.0.0.1' \
    -addext 'subjectAltName=DNS:127.0.0.1,IP:127.0.0.1' \
    -addext 'basicConstraints=critical,CA:TRUE' \
    -addext 'keyUsage=critical,digitalSignature,keyEncipherment,keyCertSign' \
    -addext 'extendedKeyUsage=serverAuth' \
    -keyout "$WORK/api.key" -out "$WORK/api.crt" >"$WORK/openssl.log" 2>&1 ||
    { cat "$WORK/openssl.log" >&2; fail "the api server certificate could not be made"; }
chmod 0600 "$WORK/api.key"

python3 "$HARNESS" keygen "$WORK/keys.json"

ANVIL_PORT=$(free_port)
FRONT_PORT=$(free_port)
SITE_PORT=$(free_port)
API_PORT=$(free_port)
SIDECAR_PORTS="$(free_port) $(free_port) $(free_port)"

export DRY_REPO_ROOT=$REPO_ROOT
export DRY_SITE=$SITE
export DRY_WORK=$WORK
export DRY_CHAIN_ID=$CHAIN_ID
export DRY_CALLBACK_GAS=$CALLBACK_GAS
export DRY_FEE=$FEE
export DRY_THRESHOLD=$THRESHOLD
export DRY_ANVIL_PORT=$ANVIL_PORT
export DRY_FRONT_PORT=$FRONT_PORT
export DRY_SITE_PORT=$SITE_PORT
export DRY_API_PORT=$API_PORT
export DRY_SIDECAR_PORTS=$SIDECAR_PORTS
export DRY_API_CERT=$WORK/api.crt
export DRY_API_KEY=$WORK/api.key
export DRY_RECORDING=$WORK/recording.json
CONTEXT=$WORK/context.json
python3 "$HARNESS" context "$WORK/keys.json" "$CONTEXT"

note "starting anvil on the loopback interface"
anvil --host 127.0.0.1 --port "$ANVIL_PORT" --chain-id "$CHAIN_ID" \
    >"$WORK/anvil.log" 2>&1 &
PIDS+=("$!")
wait_for_port "$ANVIL_PORT" 60 || { cat "$WORK/anvil.log" >&2; fail "anvil did not start"; }

CONSUMER_ADDRESS=$(python3 "$HARNESS" deploy "$CONTEXT" "$WORK/consumer.hex")
pass "the consumer is deployed at $CONSUMER_ADDRESS and the precompile address holds no code"

python3 "$HARNESS" site "$SITE" "$SITE_PORT" >"$WORK/site.log" 2>&1 &
PIDS+=("$!")
python3 "$HARNESS" api "$CONTEXT" >"$WORK/api.log" 2>&1 &
PIDS+=("$!")
python3 "$HARNESS" front "$CONTEXT" >"$WORK/front.log" 2>&1 &
PIDS+=("$!")
wait_for_port "$SITE_PORT" 30 || { cat "$WORK/site.log" >&2; fail "the site server did not start"; }
wait_for_port "$API_PORT" 30 || { cat "$WORK/api.log" >&2; fail "the api server did not start"; }
wait_for_port "$FRONT_PORT" 30 || { cat "$WORK/front.log" >&2; fail "the chain front did not start"; }

index=0
for port in $SIDECAR_PORTS; do
    dir=$WORK/sidecar-$((index + 1))
    mkdir -p "$dir/data"
    chmod 0700 "$dir"
    python3 "$HARNESS" keyfiles "$WORK/keys.json" "$index" "$dir"
    python3 "$HARNESS" config "$CONTEXT" "$index" "$dir/config.json"
    export X_WEBSEARCH_ATTESTOR_KEY_FILE="$dir/attestor.key"
    export X_WEBSEARCH_RECEIVER_KEY_FILE="$dir/receiver.key"
    if [ "$index" = 0 ]; then
        export X_WEBSEARCH_SUBMITTER_KEY_FILE="$dir/submitter.key"
    else
        unset X_WEBSEARCH_SUBMITTER_KEY_FILE
    fi
    export SSL_CERT_FILE="$WORK/api.crt"
    "$BINARY" --config "$dir/config.json" >"$dir/sidecar.log" 2>&1 &
    PIDS+=("$!")
    index=$((index + 1))
done
unset X_WEBSEARCH_ATTESTOR_KEY_FILE X_WEBSEARCH_RECEIVER_KEY_FILE
unset X_WEBSEARCH_SUBMITTER_KEY_FILE SSL_CERT_FILE

index=0
for port in $SIDECAR_PORTS; do
    dir=$WORK/sidecar-$((index + 1))
    wait_for_line "$dir/sidecar.log" "listening on 127.0.0.1:$port" 60 ||
        { cat "$dir/sidecar.log" >&2; fail "sidecar $((index + 1)) did not start"; }
    index=$((index + 1))
done
pass "three sidecars are answering with run-local attestor keys"

python3 "$HARNESS" run "$CONTEXT"
pass "the fetch, the majority api call and the single level api call were attested, fulfilled and delivered"

stop_all
python3 "$HARNESS" replay "$WORK/recording.json" "$REPO_ROOT" "$SITE"
pass "the recorded exchange replays with no node and no sidecar"

if [ "$MODE" = record ]; then
    mkdir -p "$(dirname "$FIXTURE")"
    cp "$WORK/recording.json" "$FIXTURE"
    pass "wrote $FIXTURE"
else
    [ -f "$FIXTURE" ] || fail "$FIXTURE is missing: record it with dry-run-check.sh --record"
    python3 "$HARNESS" replay "$FIXTURE" "$REPO_ROOT" "$SITE"
    pass "the committed exchange replays with no node and no sidecar"
fi
