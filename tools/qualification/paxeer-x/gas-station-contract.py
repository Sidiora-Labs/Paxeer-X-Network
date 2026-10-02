#!/usr/bin/env python3
"""Gas-station contract qualification harness.

Runs the real paxeer-gas-station binary against a real EVM node (anvil, Prague
rules, chain id from the candidate manifest) carrying the compiled
BatchCallAndSponsor paymaster and an ERC-20 at the Sidiora address, behind TLS
endpoints the binary trusts through SSL_CERT_FILE, and drives the agent SDK's
gas-station contract. Every case records its assertions; any absent input,
skipped execution or unknown case fails the run.

Inputs:
  --case NAME                 station-autonomous-recovery
  --candidate-manifest PATH   paxeer-x.candidate.v1 manifest
  PAXEER_GAS_STATION_BIN      the built binary (default: $CARGO_TARGET_DIR/release/paxeer-gas-station)
  PAXEER_X_SOLIDITY_LIBS      directory holding openzeppelin-contracts (bootstrap-libs.sh layout)
  PAXEER_X_EVIDENCE_DIR       private evidence directory outside the tree
"""
import argparse
import http.server
import json
import os
import shutil
import signal
import socket
import ssl
import subprocess
import sys
import tempfile
import threading
import time
import urllib.request

ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", "..", ".."))
SIDIORA = "0x21f7b20a555199fa73A238B1a91FD0f549068fEe"
OWNER_RATE = 3_114_000  # 3.114 SID per PAX, the owner's initial published rate, in SID base units
GAS_LIMIT = 300_000
MAX_FEE = 2_000_000_000
PRIORITY_FEE = 1_000_000_000
INTERVAL = 20
CASES = ("station-autonomous-recovery",)


class Failure(Exception):
    pass


class Record:
    def __init__(self, evidence):
        self.evidence = evidence
        self.assertions = []

    def check(self, name, condition, detail=""):
        self.assertions.append({"assertion": name, "passed": bool(condition), "detail": str(detail)[:400]})
        print(("ok   " if condition else "FAIL ") + name + (f" ({detail})" if detail and not condition else ""), flush=True)
        if not condition:
            raise Failure(name)

    def write(self, outcome):
        path = os.path.join(self.evidence, "station-autonomous-recovery.json")
        with open(path, "w", encoding="utf-8") as handle:
            json.dump({"case": "station-autonomous-recovery", "outcome": outcome, "assertions": self.assertions}, handle, indent=1)
        os.chmod(path, 0o600)


def free_port():
    with socket.socket() as s:
        s.bind(("127.0.0.1", 0))
        return s.getsockname()[1]


def rpc(url, method, params):
    body = json.dumps({"jsonrpc": "2.0", "id": 1, "method": method, "params": params}).encode()
    request = urllib.request.Request(url, body, {"content-type": "application/json"})
    with urllib.request.urlopen(request, timeout=30) as response:
        reply = json.load(response)
    if "error" in reply:
        raise Failure(f"{method}: {reply['error']}")
    return reply["result"]


def post(url, body):
    request = urllib.request.Request(url, json.dumps(body).encode(), {"content-type": "application/json"})
    try:
        with urllib.request.urlopen(request, timeout=60) as response:
            return response.status, json.load(response)
    except urllib.error.HTTPError as error:
        return error.code, json.loads(error.read() or b"{}")


def run(command, **kwargs):
    return subprocess.run(command, check=True, capture_output=True, text=True, **kwargs).stdout.strip()


class TlsProxy:
    """A TLS endpoint forwarding JSON-RPC bodies unchanged to one node."""

    def __init__(self, upstream, cert, key):
        self.port = free_port()
        target = upstream

        class Handler(http.server.BaseHTTPRequestHandler):
            def do_POST(self):
                body = self.rfile.read(int(self.headers["content-length"]))
                request = urllib.request.Request(target, body, {"content-type": "application/json"})
                with urllib.request.urlopen(request, timeout=30) as response:
                    reply = response.read()
                self.send_response(200)
                self.send_header("content-type", "application/json")
                self.send_header("content-length", str(len(reply)))
                self.end_headers()
                self.wfile.write(reply)

            def log_message(self, *args):
                pass

        self.server = http.server.ThreadingHTTPServer(("127.0.0.1", self.port), Handler)
        context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
        context.load_cert_chain(cert, key)
        self.server.socket = context.wrap_socket(self.server.socket, server_side=True)
        threading.Thread(target=self.server.serve_forever, daemon=True).start()
        self.url = f"https://localhost:{self.port}"


class Harness:
    def __init__(self, args, record, work):
        self.args, self.record, self.work = args, record, work
        self.processes = []

    def spawn(self, command, log, env=None):
        handle = open(log, "a", encoding="utf-8")
        process = subprocess.Popen(command, stdout=handle, stderr=subprocess.STDOUT, env=env, start_new_session=True)
        self.processes.append(process)
        return process

    def stop_all(self):
        for process in self.processes:
            if process.poll() is None:
                os.killpg(process.pid, signal.SIGKILL)
                process.wait()

    def anvil(self, chain_id, name):
        port = free_port()
        self.spawn([self.anvil_bin, "--port", str(port), "--chain-id", str(chain_id), "--hardfork", "prague",
                    "--slots-in-an-epoch", "1", "--silent"], os.path.join(self.work, f"{name}.log"))
        url = f"http://127.0.0.1:{port}"
        for _ in range(100):
            try:
                rpc(url, "eth_chainId", [])
                return url
            except OSError:
                time.sleep(0.1)
        raise Failure(f"anvil {name} did not start")

    def deploy(self, url, sender, bytecode):
        hash_ = rpc(url, "eth_sendTransaction", [{"from": sender, "data": bytecode, "gas": hex(8_000_000)}])
        receipt = rpc(url, "eth_getTransactionReceipt", [hash_])
        if receipt is None or receipt["status"] != "0x1":
            raise Failure("deployment failed")
        return receipt["contractAddress"]

    def station(self, config, journal, log):
        env = dict(os.environ, SSL_CERT_FILE=self.cert, PAXEER_STATION_RELAYER_KEY=self.sponsor_key)
        return self.spawn([self.binary, "--config", config, "--journal", journal], log, env)

    def wait_ready(self, process, log, port):
        for _ in range(600):
            if process.poll() is not None:
                return False
            with open(log, encoding="utf-8") as handle:
                if f"ready, serving POST /quote, /submit, /status and /retry on 127.0.0.1:{port}" in handle.read():
                    return True
            time.sleep(0.1)
        return False

    def config(self, endpoints, port):
        path = os.path.join(self.work, f"station-{port}.json")
        with open(path, "w", encoding="utf-8") as handle:
            json.dump({"chain_id": self.chain_id, "endpoints": endpoints, "paymaster": self.paymaster, "token": SIDIORA,
                       "decimals": 6, "max_rate_age": 300, "spread_bps": 500, "margin_bps": 100,
                       "per_account_limit": 100_000_000, "per_interval_limit": 1_000_000_000, "per_quote_limit": 10_000_000,
                       "interval_seconds": INTERVAL, "balance_floor": 1, "relayer_key_env": "PAXEER_STATION_RELAYER_KEY",
                       "listen": f"127.0.0.1:{port}", "gas_limit": GAS_LIMIT, "max_priority_fee_per_gas": PRIORITY_FEE}, handle)
        return path

    def sdk(self, operation, payload):
        script = os.path.join(self.work, "sdk.mjs")
        return json.loads(run(["node", script, operation, json.dumps(payload)]))

    def sign(self, key, digest):
        return run([self.cast, "wallet", "sign", "--no-hash", "--private-key", key, digest])

    def journal(self, path):
        with open(path, encoding="utf-8") as handle:
            return [json.loads(line) for line in handle]

    def sid_balance(self, owner):
        data = "0x70a08231" + owner[2:].lower().rjust(64, "0")
        return int(rpc(self.node, "eth_call", [{"to": SIDIORA, "data": data}, "latest"]), 16)

    def nonce(self, address, block="latest"):
        return int(rpc(self.node, "eth_getTransactionCount", [address, block]), 16)

    def submit_identity(self, station_url, transfer):
        account, key = self.account, self.account_key
        calls = [{"to": SIDIORA, "value": "0", "data": "0xa9059cbb" + self.recipient[2:].lower().rjust(64, "0") + hex(transfer)[2:].rjust(64, "0")}]
        batch_nonce = int(rpc(self.node, "eth_call", [{"to": account, "data": "0xaffed0e0"}, "pending"]) or "0x0", 16) \
            if rpc(self.node, "eth_getCode", [account, "pending"]) not in ("0x", None) else 0
        quoted = self.sdk("quote", {"url": station_url + "/quote", "config": self.sdk_config, "account": account,
                                    "nonce": str(batch_nonce), "calls": calls, "maxTokenAmount": "10000000",
                                    "gasCost": str(GAS_LIMIT * MAX_FEE)})
        self.record.check("sdk quote accepted from the served binary", quoted.get("ok") is True, quoted)
        quote = quoted["value"]["quote"]
        batch = {"chainId": str(self.chain_id), "account": account, "nonce": str(batch_nonce), "calls": calls, "quote": quote}
        digest = self.sdk("batchDigest", {"batch": batch})
        account_signature = self.sign(key, digest)
        auth_nonce = self.nonce(account, "pending")
        auth_digest = self.sdk("authDigest", {"chainId": str(self.chain_id), "address": self.paymaster, "nonce": str(auth_nonce)})
        authorization = self.sdk("assembleAuth", {"config": self.sdk_config, "account": account, "nonce": str(auth_nonce),
                                                   "signature": self.sign(key, auth_digest)})
        call = self.sdk("call", {"config": self.sdk_config, "batch": batch, "accountSignature": account_signature,
                                 "relayerSignature": quoted["value"]["relayerSignature"]})
        body = {"call": call, "authorization": authorization, "batch": batch, "accountSignature": account_signature,
                "relayerSignature": quoted["value"]["relayerSignature"]}
        identity = {"sponsor": quote["sponsor"], "quoteNonce": quote["quoteNonce"], "account": account,
                    "relayerSignature": quoted["value"]["relayerSignature"]}
        return body, identity, quote

    def wait_completion(self, journal, quote_nonce, outcome, seconds):
        deadline = time.time() + seconds
        while time.time() < deadline:
            rpc(self.node, "evm_mine", [])
            for entry in self.journal(journal):
                key = entry.get("key", {})
                if entry["kind"] == "completed" and entry["completion"]["outcome"] == outcome and str(int.from_bytes(bytes(key["quote_nonce"]), "big")) == quote_nonce:
                    return entry
                if entry["kind"] == "cancelled" and outcome == "cancelled" and str(int.from_bytes(bytes(key["quote_nonce"]), "big")) == quote_nonce:
                    return entry
            time.sleep(1)
        return None


SDK_SCRIPT = r"""
import * as sdk from %s;
const [operation, raw] = process.argv.slice(2);
const input = JSON.parse(raw);
const big = (v) => BigInt(v);
const config = input.config && { ...input.config, chainId: big(input.config.chainId) };
const quoteOf = (q) => ({ ...q, maxTokenAmount: big(q.maxTokenAmount), tokenAmount: big(q.tokenAmount), deadline: big(q.deadline), quoteNonce: big(q.quoteNonce), gasCost: big(q.gasCost) });
const batchOf = (b) => ({ chainId: big(b.chainId), account: b.account, nonce: big(b.nonce), calls: b.calls.map((c) => ({ ...c, value: big(c.value) })), quote: quoteOf(b.quote) });
const out = (v) => console.log(JSON.stringify(v, (_k, x) => typeof x === "bigint" ? x.toString() : x));
const value = (r) => { if (!r.ok) { console.error(JSON.stringify(r.refusal)); process.exit(3); } return r.value; };
switch (operation) {
  case "quote": out(await sdk.requestGasQuote({ ...config, quoteUrl: input.url }, { account: input.account, nonce: big(input.nonce), calls: input.calls.map((c) => ({ ...c, value: big(c.value) })), maxTokenAmount: big(input.maxTokenAmount), gasCost: big(input.gasCost) })); break;
  case "batchDigest": out(value(sdk.sponsoredBatchDigest(batchOf(input.batch)))); break;
  case "authDigest": out(value(sdk.eip7702AuthorizationDigest({ chainId: big(input.chainId), address: input.address, nonce: big(input.nonce) }))); break;
  case "assembleAuth": { const a = value(sdk.assembleEip7702Authorization(config, input.account, big(input.nonce), input.signature)); out({ chainId: a.chainId.toString(), address: a.address, nonce: a.nonce.toString(), yParity: a.yParity, r: a.r, s: a.s }); break; }
  case "call": { const c = value(sdk.sponsoredBatchCall(config, batchOf(input.batch), input.accountSignature, input.relayerSignature, 0n)); out({ to: c.to, value: c.value.toString(), data: c.data }); break; }
  case "status": out(await sdk.requestGasSubmissionStatus({ ...config, quoteUrl: input.url }, { ...input.identity, quoteNonce: big(input.identity.quoteNonce) })); break;
  case "retry": out(await sdk.retryGasSubmission({ ...config, quoteUrl: input.url }, { ...input.identity, quoteNonce: big(input.identity.quoteNonce) })); break;
  default: process.exit(2);
}
"""


def required_inputs(args):
    missing = []
    target = os.environ.get("CARGO_TARGET_DIR", os.path.join(ROOT, "target"))
    binary = os.environ.get("PAXEER_GAS_STATION_BIN", os.path.join(target, "release", "paxeer-gas-station"))
    if not os.access(binary, os.X_OK):
        missing.append(f"station binary {binary}")
    for tool in ("anvil", "forge", "cast", "node", "openssl"):
        if shutil.which(tool) is None:
            missing.append(f"tool {tool}")
    libs = os.environ.get("PAXEER_X_SOLIDITY_LIBS", os.path.join(ROOT, "contracts", "lib"))
    if not os.path.isfile(os.path.join(libs, "openzeppelin-contracts", "contracts", "token", "ERC20", "ERC20.sol")):
        missing.append(f"openzeppelin-contracts under {libs}")
    sdk = os.path.join(ROOT, "agent", "sdk", "typescript", "dist", "src", "index.js")
    if not os.path.isfile(sdk):
        missing.append(f"built agent SDK {sdk}")
    evidence = os.environ.get("PAXEER_X_EVIDENCE_DIR")
    if not evidence:
        missing.append("PAXEER_X_EVIDENCE_DIR")
    elif os.path.abspath(evidence).startswith(ROOT + os.sep):
        missing.append("PAXEER_X_EVIDENCE_DIR outside the source tree")
    manifest = None
    try:
        with open(args.candidate_manifest, encoding="utf-8") as handle:
            manifest = json.load(handle)
    except (OSError, ValueError, TypeError):
        missing.append(f"candidate manifest {args.candidate_manifest}")
    if manifest is not None:
        if manifest.get("schema") != "paxeer-x.candidate.v1":
            missing.append("candidate manifest schema paxeer-x.candidate.v1")
        if not isinstance(manifest.get("foundation", {}).get("chain_id"), int):
            missing.append("candidate manifest foundation.chain_id")
        if not any(s.get("id") == "gas" for s in manifest.get("services", [])):
            missing.append("candidate manifest service gas")
    return missing, binary, libs, sdk, evidence, manifest


def autonomous_recovery(h):
    record = h.record
    work = h.work
    h.cert, key_file = os.path.join(work, "cert.pem"), os.path.join(work, "key.pem")
    run(["openssl", "req", "-x509", "-newkey", "rsa:2048", "-nodes", "-days", "1", "-subj", "/CN=localhost",
         "-addext", "subjectAltName=DNS:localhost", "-keyout", key_file, "-out", h.cert])
    os.chmod(key_file, 0o600)
    h.node = h.anvil(h.chain_id, "node-a")
    other = h.anvil(h.chain_id, "node-b")
    proxies = [TlsProxy(h.node, h.cert, key_file) for _ in range(2)]
    divergent = TlsProxy(other, h.cert, key_file)
    unreachable = f"https://localhost:{free_port()}"
    accounts = rpc(h.node, "eth_accounts", [])
    owner = accounts[0]
    h.sponsor_key = "0x59c6995e998f97a5a0044966f0945389dc9e86dae88c7a8412f4603b6b78690d"  # anvil dev account 1
    h.sponsor = run([h.cast, "wallet", "address", "--private-key", h.sponsor_key]).lower()
    h.account_key = "0x" + os.urandom(32).hex()
    h.account = run([h.cast, "wallet", "address", "--private-key", h.account_key]).lower()
    h.recipient = "0x" + os.urandom(20).hex()
    out = os.path.join(work, "out")
    config = os.path.join(work, "foundry.toml")
    shutil.copyfile(os.path.join(ROOT, "foundry.paxeer.toml"), config)
    run([h.forge, "build",
         os.path.join(ROOT, "contracts", "src", "BatchCallAndSponsor.sol"), os.path.join(ROOT, "contracts", "src", "TestToken.sol"),
         "--root", ROOT, "--config-path", config,
         "--contracts", os.path.join(ROOT, "contracts", "src"), "--lib-paths", h.libs,
         "--remappings", f"@openzeppelin/contracts/={h.libs}/openzeppelin-contracts/contracts/",
         "--out", out, "--cache-path", os.path.join(work, "cache"), "--skip", "test", "--skip", "script"])
    with open(os.path.join(out, "BatchCallAndSponsor.sol", "BatchCallAndSponsor.json"), encoding="utf-8") as handle:
        paymaster_code = json.load(handle)["bytecode"]["object"]
    with open(os.path.join(out, "TestToken.sol", "TestToken.json"), encoding="utf-8") as handle:
        token_code = json.load(handle)["bytecode"]["object"]
    constructor = run([h.cast, "abi-encode", "constructor(address,uint256,uint256)", owner, str(OWNER_RATE), "300"])
    h.paymaster = h.deploy(h.node, owner, paymaster_code + constructor[2:])
    token = h.deploy(h.node, owner, token_code + run([h.cast, "abi-encode", "constructor(string,string)", "Sidiora", "SID"])[2:])
    rpc(h.node, "anvil_setCode", [SIDIORA, rpc(h.node, "eth_getCode", [token, "latest"])])
    slot = run([h.cast, "index", "address", h.account, "0"])
    rpc(h.node, "anvil_setStorageAt", [SIDIORA, slot, "0x" + hex(50_000_000)[2:].rjust(64, "0")])
    record.check("account holds SID at the Sidiora address", h.sid_balance(h.account) == 50_000_000)
    h.sdk_config = {"chainId": str(h.chain_id), "sponsor": h.sponsor, "token": SIDIORA, "decimals": 6, "paymaster": h.paymaster}
    with open(os.path.join(work, "sdk.mjs"), "w", encoding="utf-8") as handle:
        handle.write(SDK_SCRIPT % json.dumps("file://" + h.sdk_path))

    journal = os.path.join(work, "sponsorship.jsonl")
    port = free_port()
    endpoints = [proxies[0].url, proxies[1].url, unreachable]
    config = h.config(endpoints, port)
    url = f"http://127.0.0.1:{port}"
    log1 = os.path.join(work, "station-1.log")
    first = h.station(config, journal, log1)
    record.check("startup readiness reported with one endpoint unreachable", h.wait_ready(first, log1, port))
    with open(log1, encoding="utf-8") as handle:
        record.check("startup recovery pass ran over an empty journal", "startup recovery unresolved=0 completed=0" in handle.read())
    rival = subprocess.run([h.binary, "--config", h.config(endpoints, free_port()), "--journal", journal], capture_output=True,
                           text=True, timeout=60, env=dict(os.environ, SSL_CERT_FILE=h.cert, PAXEER_STATION_RELAYER_KEY=h.sponsor_key))
    record.check("competing writer refused", rival.returncode != 0 and "Locked" in rival.stderr, rival.stderr)

    # Crash after broadcast, before finality: the restarted binary settles it without a client request.
    rpc(h.node, "evm_setAutomine", [False])
    sponsor_sid = h.sid_balance(h.sponsor)
    sponsor_nonce = h.nonce(h.sponsor)
    body, identity, quote = h.submit_identity(url, 123)
    status, reply = post(url + "/submit", body)
    record.check("submit broadcast the prepared transaction", status == 200 and "transactionHash" in reply, reply)
    hash_ = reply["transactionHash"]
    prepared = [e for e in h.journal(journal) if e["kind"] == "prepared"]
    record.check("prepared bytes journalled before broadcast", len(prepared) == 1 and prepared[0]["submission"]["nonce"] == sponsor_nonce)
    os.killpg(first.pid, signal.SIGKILL)
    first.wait()
    pending = rpc(h.node, "eth_getTransactionByHash", [hash_])
    record.check("exact durable transaction is in the node's pool after the crash", pending is not None and pending["hash"] == hash_)
    rpc(h.node, "evm_setAutomine", [True])
    for _ in range(4):
        rpc(h.node, "evm_mine", [])
    snapshot = os.path.join(work, "divergent.jsonl")
    shutil.copyfile(journal, snapshot)
    divergent_port = free_port()
    fork = subprocess.run([h.binary, "--config", h.config([proxies[0].url, divergent.url], divergent_port), "--journal", snapshot],
                          capture_output=True, text=True, timeout=120,
                          env=dict(os.environ, SSL_CERT_FILE=h.cert, PAXEER_STATION_RELAYER_KEY=h.sponsor_key))
    record.check("divergent observations fail closed at startup", fork.returncode != 0 and "startup recovery" in fork.stderr, fork.stderr)
    record.check("divergent startup acted on nothing", h.journal(snapshot) == h.journal(journal))
    corrupt = os.path.join(work, "corrupt.jsonl")
    shutil.copyfile(journal, corrupt)
    with open(corrupt, "a", encoding="utf-8") as handle:
        handle.write("{")
    broken = subprocess.run([h.binary, "--config", h.config(endpoints, free_port()), "--journal", corrupt], capture_output=True,
                            text=True, timeout=60, env=dict(os.environ, SSL_CERT_FILE=h.cert, PAXEER_STATION_RELAYER_KEY=h.sponsor_key))
    record.check("corrupt journal fails closed", broken.returncode != 0 and "Corrupt" in broken.stderr, broken.stderr)
    log2 = os.path.join(work, "station-2.log")
    second = h.station(config, journal, log2)
    record.check("restarted binary ready", h.wait_ready(second, log2, port))
    included = h.wait_completion(journal, quote["quoteNonce"], "included", 120)
    record.check("restart settled the submission without a client request", included is not None)
    receipt = rpc(h.node, "eth_getTransactionReceipt", [hash_])
    record.check("finalized receipt succeeded for the exact durable hash", receipt["status"] == "0x1")
    record.check("SID repaid exactly once", h.sid_balance(h.sponsor) - sponsor_sid == int(quote["tokenAmount"]))
    record.check("account calls executed once", h.sid_balance(h.recipient) == 123)
    record.check("sponsor nonce advanced by one", h.nonce(h.sponsor) == sponsor_nonce + 1)
    record.check("no duplicate sponsored transaction",
                 sum(1 for e in h.journal(journal) if e["kind"] == "prepared") == 1)
    while int(time.time()) <= int(quote["deadline"]):
        time.sleep(1)
    done = h.sdk("status", {"url": url + "/quote", "config": h.sdk_config, "identity": identity})
    record.check("status by identity after quote expiry", done.get("ok") and done["value"]["completion"]["outcome"] == "included", done)
    again = h.sdk("retry", {"url": url + "/quote", "config": h.sdk_config, "identity": identity})
    record.check("same-identity retry after expiry returns the settlement", again.get("ok") and again["value"]["state"] == "completed", again)
    record.check("retry signed and broadcast nothing", h.nonce(h.sponsor) == sponsor_nonce + 1)
    forged = h.sdk("status", {"url": url + "/quote", "config": h.sdk_config, "identity": dict(identity, relayerSignature="0x" + "11" * 65)})
    record.check("status refused without the station's quote signature", forged.get("ok") is False, forged)

    # Dropped submission past its deadline: the driver cancels its nonce and the next submission takes the next one.
    rpc(h.node, "evm_setAutomine", [False])
    sponsor_sid = h.sid_balance(h.sponsor)
    body, identity, quote = h.submit_identity(url, 7)
    status, reply = post(url + "/submit", body)
    record.check("second submit broadcast", status == 200, reply)
    dropped_nonce = h.nonce(h.sponsor)
    rpc(h.node, "anvil_dropTransaction", [reply["transactionHash"]])
    os.killpg(second.pid, signal.SIGKILL)
    second.wait()
    rpc(h.node, "evm_setAutomine", [True])
    while int(time.time()) <= int(quote["deadline"]):
        time.sleep(1)
    unsubmitted_body, unsubmitted, unsubmitted_quote = None, None, None
    log3 = os.path.join(work, "station-3.log")
    third = h.station(config, journal, log3)
    record.check("third start ready", h.wait_ready(third, log3, port))
    cancelled = h.wait_completion(journal, quote["quoteNonce"], "cancelled", 120)
    record.check("driver cancelled the expired dropped submission at its nonce", cancelled is not None)
    record.check("cancellation repaid no SID", h.sid_balance(h.sponsor) == sponsor_sid)
    record.check("dropped call never executed", h.sid_balance(h.recipient) == 123)
    record.check("cancellation consumed the sponsor nonce", h.nonce(h.sponsor) == dropped_nonce + 1)
    state = h.sdk("status", {"url": url + "/quote", "config": h.sdk_config, "identity": identity})
    record.check("status reports verified cancellation", state.get("ok") and state["value"]["completion"]["outcome"] == "cancelled", state)

    unsubmitted_body, unsubmitted, unsubmitted_quote = h.submit_identity(url, 5)
    body, identity, quote = h.submit_identity(url, 11)
    sponsor_sid = h.sid_balance(h.sponsor)
    status, reply = post(url + "/submit", body)
    record.check("next submission accepted", status == 200, reply)
    included = h.wait_completion(journal, quote["quoteNonce"], "included", 120)
    record.check("next safe sponsor nonce unblocked and settled", included is not None and
                 [e for e in h.journal(journal) if e["kind"] == "prepared"][-1]["submission"]["nonce"] == dropped_nonce + 1)
    record.check("next settlement repaid exactly its quote", h.sid_balance(h.sponsor) - sponsor_sid == int(quote["tokenAmount"]))
    while int(time.time()) <= int(unsubmitted_quote["deadline"]):
        time.sleep(1)
    status, reply = post(url + "/submit", unsubmitted_body)
    record.check("new expired authorization refused", status == 422, (status, reply))
    status, reply = post(url + "/retry", unsubmitted)
    record.check("retry of an unsubmitted expired identity refused", status == 422, (status, reply))
    os.killpg(third.pid, signal.SIGKILL)
    third.wait()


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--case", required=True)
    parser.add_argument("--candidate-manifest", required=True)
    args = parser.parse_args()
    if args.case not in CASES:
        print(f"unknown case {args.case}", file=sys.stderr)
        return 2
    missing, binary, libs, sdk, evidence, manifest = required_inputs(args)
    if missing:
        for item in missing:
            print(f"required input absent: {item}", file=sys.stderr)
        return 2
    os.makedirs(evidence, mode=0o700, exist_ok=True)
    record = Record(evidence)
    work = tempfile.mkdtemp(prefix="gas-station-contract-", dir=evidence)
    harness = Harness(args, record, work)
    harness.binary, harness.libs, harness.sdk_path = binary, libs, sdk
    harness.anvil_bin, harness.forge, harness.cast = shutil.which("anvil"), shutil.which("forge"), shutil.which("cast")
    harness.chain_id = manifest["foundation"]["chain_id"]
    try:
        autonomous_recovery(harness)
    except (Failure, subprocess.SubprocessError, OSError, KeyError, ValueError) as error:
        record.write("failed")
        print(f"case station-autonomous-recovery failed: {error}", file=sys.stderr)
        print(f"cases=1 executed=1 passed=0 assertions={len(record.assertions)}")
        return 1
    finally:
        harness.stop_all()
    record.write("passed")
    print(f"cases=1 executed=1 passed=1 assertions={len(record.assertions)}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
