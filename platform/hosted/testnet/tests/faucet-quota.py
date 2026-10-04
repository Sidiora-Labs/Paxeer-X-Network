#!/usr/bin/env python3
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import tempfile
import time

ROOT = Path(__file__).resolve().parents[4]
SOURCE = ROOT / "platform/hosted/faucet/src/main.rs"
NAMES = ("RESERVE_SCRIPT", "COMPLETE_SCRIPT", "ROLLBACK_SCRIPT", "NETWORK_ADMISSION_SCRIPT")


def digest(*parts):
    return hashlib.sha256(b"".join(part.encode() + b"\0" for part in parts)).hexdigest()


class Corpus:
    def __init__(self, directory, server, client):
        self.directory = Path(directory)
        self.socket = self.directory / "redis.sock"
        self.server, self.client = server, client
        self.process = None
        self.log = (self.directory / "redis.log").open("ab")
        self.tests = 0
        self.idempotencies = {}
        text = SOURCE.read_text()
        self.source_digest = hashlib.sha256(text.encode()).hexdigest()
        self.scripts = {}
        for name in NAMES:
            matches = re.findall(r'const ' + name + r': &str = r"([\s\S]*?)";', text)
            if len(matches) != 1:
                raise RuntimeError("exact production Lua constant unavailable: " + name)
            self.scripts[name] = matches[0]
        self.attempts = int(re.search(r"const AUDIT_ATTEMPTS: usize = (\d+);", text)[1])

    def check(self, name, condition):
        if not condition:
            raise AssertionError(name)
        self.tests += 1
        print("PASS faucet-quota " + name, flush=True)

    def command(self, *arguments):
        result = subprocess.run([self.client, "-s", str(self.socket), "--json", *map(str, arguments)],
                                capture_output=True, text=True, timeout=10)
        if result.returncode:
            raise RuntimeError("real Redis command failed: " + result.stderr.strip())
        return json.loads(result.stdout)

    def start(self):
        if self.process is not None:
            raise RuntimeError("Redis already running")
        self.process = subprocess.Popen([self.server, "--port", "0", "--unixsocket", str(self.socket),
            "--unixsocketperm", "700", "--dir", str(self.directory), "--appendonly", "yes",
            "--appendfsync", "always", "--save", "", "--daemonize", "no"], stdout=self.log, stderr=self.log)
        deadline = time.monotonic() + 10
        while time.monotonic() < deadline:
            if self.process.poll() is not None:
                raise RuntimeError("real Redis exited during private startup")
            if self.socket.exists():
                try:
                    if self.command("PING") == "PONG":
                        return
                except (RuntimeError, ValueError):
                    pass
            time.sleep(0.05)
        raise RuntimeError("real Redis private socket did not become ready")

    def stop(self, crash=False):
        if self.process is None:
            return
        if self.process.poll() is None:
            self.process.kill() if crash else self.process.terminate()
        self.process.wait(timeout=10)
        self.process = None
        self.socket.unlink(missing_ok=True)

    def close(self):
        self.stop()
        self.log.close()

    def eval(self, name, keys, arguments):
        return self.command("EVAL", self.scripts[name], len(keys), *keys, *arguments)

    def keys(self, window, identity, address, network, idempotency):
        idem_key = "faucet:idem:" + digest(idempotency)
        self.idempotencies[idem_key] = idempotency
        return [idem_key,
                f"faucet:quota:{window}:identity:" + digest(identity),
                f"faucet:quota:{window}:address:" + digest(address.lower()),
                f"faucet:quota:{window}:network:" + digest(network), "faucet:audit", "faucet:audit:head"]

    def reserve(self, keys, identity, amount=10, limits=(100, 100, 100), stale_head=None):
        funding = digest(self.idempotencies[keys[0]], identity)
        for _ in range(self.attempts):
            head = self.command("GET", keys[5]) or ""
            event = digest(keys[0], identity, str(time.time_ns()))
            arguments = [identity, amount, *limits, 120, 3600, funding, event,
                         head if stale_head is None else stale_head, digest(head, event)]
            result = self.eval("RESERVE_SCRIPT", keys, arguments)
            if result[0] != "audit_retry" or stale_head is not None:
                return result, funding
        raise RuntimeError("real Redis audit contention exceeded production bound")

    def quotas(self, keys):
        return [self.command("GET", key) for key in keys[1:4]]

    def run(self):
        self.start()
        keys = self.keys(1, "did:quota:owner", "ab" * 32, "192.0.2.0/24", "first")
        request = digest("first-request")
        reserved, funding = self.reserve(keys, request)
        self.check("atomic reservation charges all three buckets", reserved == ["reserved", funding] and self.quotas(keys) == ["10"] * 3)
        self.check("bounded quota and idempotency expiry", all(0 < self.command("TTL", key) <= 120 for key in keys[1:4]) and 0 < self.command("TTL", keys[0]) <= 3600)
        retry, _ = self.reserve(keys, request)
        self.check("pending replay preserves funding identity and charge", retry[:4] == ["existing", request, "reserved", funding] and self.quotas(keys) == ["10"] * 3)
        conflict, _ = self.reserve(keys, digest("different-request"))
        self.check("idempotency conflict retains original request", conflict[0:2] == ["existing", request] and conflict[1] != digest("different-request") and self.quotas(keys) == ["10"] * 3)
        self.stop(crash=True)
        self.start()
        recovered_pending, _ = self.reserve(keys, request)
        self.check("AOF crash restart preserves pending reservation", recovered_pending == retry and self.quotas(keys) == ["10"] * 3)
        length = self.command("XLEN", "faucet:audit")
        stale, _ = self.reserve(self.keys(1, "other", "bc" * 32, "198.51.100.0/24", "stale"), request, stale_head="invalid-head")
        self.check("audit contention refuses mutation", stale == ["audit_retry"] and self.command("XLEN", "faucet:audit") == length)
        body = json.dumps({"funding_id": funding}, separators=(",", ":"))
        bad = self.eval("COMPLETE_SCRIPT", [keys[0], keys[4]], [digest("wrong"), funding, body, digest("complete")])
        bad_funding = self.eval("COMPLETE_SCRIPT", [keys[0], keys[4]], [request, digest("wrong-funding"), body, digest("complete")])
        self.check("completion authenticates both durable identities", bad == ["conflict"] and bad_funding == ["conflict"] and self.command("HGET", keys[0], "state") == "reserved")
        completed = self.eval("COMPLETE_SCRIPT", [keys[0], keys[4]], [request, funding, body, digest("complete")])
        funded, _ = self.reserve(keys, request)
        self.check("completed replay retains byte-identical response", completed == ["funded"] and funded == ["existing", request, "funded", funding, body])
        length = self.command("XLEN", "faucet:audit")
        duplicate = self.eval("COMPLETE_SCRIPT", [keys[0], keys[4]], [request, funding, "changed-response", digest("duplicate")])
        self.check("completion retry does not replace response or audit", duplicate == ["funded"] and self.command("HGET", keys[0], "response") == body and self.command("XLEN", "faucet:audit") == length)
        rollback_args = [request, funding, "identity_key", "address_key", "network_key", digest("rollback")]
        self.check("funded reservation cannot roll back", self.eval("ROLLBACK_SCRIPT", [keys[0], keys[4]], rollback_args) == ["unchanged"] and self.quotas(keys) == ["10"] * 3)
        self.stop(crash=True)
        self.start()
        recovered, _ = self.reserve(keys, request)
        self.check("AOF crash restart preserves completion and quota", recovered == funded and self.quotas(keys) == ["10"] * 3)
        for index, expected in enumerate(("identity_quota", "address_quota", "network_quota")):
            window = index + 2
            first = self.keys(window, "owner", "ab" * 32, "192.0.2.0/24", f"quota-{index}-first")
            limits = [100, 100, 100]
            limits[index] = 10
            result, _ = self.reserve(first, digest("quota-first"), limits=limits)
            self.check(expected + " initial reservation", result[0] == "reserved")
            second = self.keys(window, "owner" if index == 0 else "other", ("AB" * 32) if index == 1 else "cd" * 32,
                               "192.0.2.0/24" if index == 2 else "198.51.100.0/24", f"quota-{index}-second")
            denied, _ = self.reserve(second, digest("quota-second"), limits=limits)
            self.check(expected + " refuses across claims and identities", denied == ["quota", expected] and self.command("EXISTS", second[0]) == 0 and self.quotas(first) == ["10"] * 3)
        rollback_keys = self.keys(8, "rollback-owner", "ef" * 32, "203.0.113.0/24", "rollback")
        rollback_request = digest("rollback-request")
        result, rollback_funding = self.reserve(rollback_keys, rollback_request)
        self.check("rollback starts from real reserved state", result == ["reserved", rollback_funding])
        args = [rollback_request, rollback_funding, "identity_key", "address_key", "network_key", digest("rejected")]
        wrong = self.eval("ROLLBACK_SCRIPT", [rollback_keys[0], rollback_keys[4]], [digest("wrong"), *args[1:]])
        self.check("rollback refuses mismatched identity", wrong == ["unchanged"] and self.quotas(rollback_keys) == ["10"] * 3)
        result = self.eval("ROLLBACK_SCRIPT", [rollback_keys[0], rollback_keys[4]], args)
        self.check("rollback returns exactly the reserved quota", result == ["rolled_back"] and self.quotas(rollback_keys) == [None] * 3 and self.command("EXISTS", rollback_keys[0]) == 0)
        self.check("repeated rollback cannot create negative quota", self.eval("ROLLBACK_SCRIPT", [rollback_keys[0], rollback_keys[4]], args) == ["unchanged"] and self.quotas(rollback_keys) == [None] * 3)
        admission = "faucet:admission:1:" + digest("192.0.2.0/24")
        admitted = [self.eval("NETWORK_ADMISSION_SCRIPT", [admission], [120, 2]) for _ in range(3)]
        self.check("network request admission precedes excess claims", admitted == [["admitted"], ["admitted"], ["rate_limited"]] and 0 < self.command("TTL", admission) <= 120)
        other_admission = "faucet:admission:1:" + digest("198.51.100.0/24")
        self.check("network request admission keeps independent scopes", self.eval("NETWORK_ADMISSION_SCRIPT", [other_admission], [120, 2]) == ["admitted"])
        self.stop(crash=True)
        self.start()
        self.check("network admission survives restart", self.eval("NETWORK_ADMISSION_SCRIPT", [admission], [120, 2]) == ["rate_limited"])
        maximum = (1 << 53) - 1
        exact = self.keys(9, "exact-owner", "12" * 32, "192.0.2.0/24", "exact-first")
        result, _ = self.reserve(exact, digest("exact-first"), amount=maximum - 1, limits=[maximum] * 3)
        self.check("exact Lua maximum-minus-one reservation", result[0] == "reserved" and self.quotas(exact) == [str(maximum - 1)] * 3)
        final = self.keys(9, "exact-owner", "12" * 32, "192.0.2.0/24", "exact-final")
        result, _ = self.reserve(final, digest("exact-final"), amount=1, limits=[maximum] * 3)
        self.check("exact quota boundary admits its final unit", result[0] == "reserved" and self.quotas(exact) == [str(maximum)] * 3)
        denied = self.keys(9, "exact-owner", "12" * 32, "192.0.2.0/24", "exact-denied")
        result, _ = self.reserve(denied, digest("exact-denied"), amount=1, limits=[maximum] * 3)
        self.check("subtractive bound refuses one unit over maximum", result == ["quota", "identity_quota"] and self.quotas(exact) == [str(maximum)] * 3)
        self.check("production Lua source remains unchanged", hashlib.sha256(SOURCE.read_bytes()).hexdigest() == self.source_digest)
        print("FAUCET_QUOTA_RESULT " + json.dumps({"tests": self.tests, "skipped": 0}), flush=True)


def main():
    server, client = shutil.which("redis-server"), shutil.which("redis-cli")
    if not server or not client:
        print("missing prerequisite: genuine redis-server and redis-cli", file=sys.stderr)
        return 78
    with tempfile.TemporaryDirectory(prefix="faucet-quota-") as directory:
        os.chmod(directory, 0o700)
        corpus = Corpus(directory, server, client)
        try:
            corpus.run()
        finally:
            corpus.close()
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (AssertionError, OSError, RuntimeError, ValueError, subprocess.SubprocessError) as error:
        print("FAIL faucet-quota: " + str(error), file=sys.stderr)
        sys.exit(1)
