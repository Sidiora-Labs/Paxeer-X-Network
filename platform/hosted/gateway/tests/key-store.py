#!/usr/bin/env python3
import hashlib
import json
import os
from pathlib import Path
import re
import secrets
import shutil
import subprocess
import sys
import tempfile
import time

ROOT = Path(__file__).resolve().parents[4]
SOURCE = ROOT / "platform/hosted/gateway/src/store.rs"
NAMES = ("ISSUE_SCRIPT", "ROTATE_SCRIPT", "REVOKE_SCRIPT", "RESERVE_SCRIPT", "COMPLETE_SCRIPT", "CONSUME_SCRIPT")


def digest(*parts):
    return hashlib.sha256(b"".join(len(part.encode()).to_bytes(8, "big") + part.encode() for part in parts)).hexdigest()


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
        print("PASS gateway-key-store " + name, flush=True)

    def command(self, *arguments):
        result = subprocess.run([self.client, "-2", "-s", str(self.socket), "--json", *map(str, arguments)],
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

    def audit(self, principal="owner", stale=False):
        head = self.command("GET", "gateway:audit:head") or ""
        event = digest(principal) + ":" + digest(secrets.token_hex(32))
        return event, "stale-head" if stale else head, digest(head, event)

    def issue(self, key_id, principal, limit=128, stale=False):
        secret = "lxp_live_" + secrets.token_hex(32)
        salt = digest("gateway-key-salt-v1", key_id)
        record = [key_id, principal, salt, digest("gateway-key-v1", salt, secret), secrets.token_hex(32),
                  "activity:write,receipt:read", "2", "120", "1"]
        event, head, chain = self.audit(principal, stale)
        result = self.eval("ISSUE_SCRIPT", ["gateway:key:" + key_id, "gateway:principal:" + principal + ":keys", "gateway:audit", "gateway:audit:head"],
                           [*record, limit, event, head, chain])
        return result, record, secret

    def rotate(self, old, replacement, principal=None, limit=128):
        principal = old[1] if principal is None else principal
        event, head, chain = self.audit(principal)
        return self.eval("ROTATE_SCRIPT", ["gateway:key:" + old[0], "gateway:key:" + replacement[0],
            "gateway:principal:" + principal + ":keys", "gateway:audit", "gateway:audit:head"],
            [principal, old[8], replacement[0], *replacement[2:], event, head, chain, limit])

    def revoke(self, record, principal=None):
        principal = record[1] if principal is None else principal
        event, head, chain = self.audit(principal)
        return self.eval("REVOKE_SCRIPT", ["gateway:key:" + record[0], "gateway:audit", "gateway:audit:head"],
                         [principal, event, head, chain])

    def reservation(self, record, scope, request, activity, principal=None):
        principal = record[1] if principal is None else principal
        event, head, chain = self.audit(principal)
        keys = ["gateway:key:" + record[0], "gateway:quota:" + record[0] + ":1", "gateway:idem:" + scope,
                "gateway:audit", "gateway:audit:head", "gateway:pending", "gateway:activity:" + activity,
                "gateway:activity-operation:" + activity]
        args = [record[8], record[6], "120", "3600", request, event, "120", head, chain, principal,
                digest("protocol", scope), "retained-continuation-bytes"]
        return self.eval("RESERVE_SCRIPT", keys, args), keys

    def complete(self, keys, request, principal, response):
        event, head, chain = self.audit(principal)
        return self.eval("COMPLETE_SCRIPT", [keys[2], "gateway:pending", "gateway:audit", "gateway:audit:head",
            keys[6], "gateway:events:pending", "gateway:events:queue", "gateway:events:observations", "gateway:events:sequences"],
            [request, "completed", response, "", event, head, chain, "0", principal, "", "", "4096", "", "0", "0"])

    def consume(self, record, window=2):
        event, head, chain = self.audit(record[1])
        return self.eval("CONSUME_SCRIPT", ["gateway:key:" + record[0], f"gateway:quota:{record[0]}:{window}", "gateway:audit", "gateway:audit:head"],
                         [record[8], record[6], "120", event, head, chain])

    def run(self):
        self.start()
        principal, foreign = digest("owner"), digest("foreign")
        result, owner, secret = self.issue("owner-key", principal)
        self.check("production Lua issues durable salted key", result == ["issued"] and self.command("HGET", "gateway:key:owner-key", "secret_digest") == owner[3])
        result, other, other_secret = self.issue("foreign-key", foreign)
        self.check("key lists isolate principal ownership", result == ["issued"] and self.command("SMEMBERS", "gateway:principal:" + principal + ":keys") == [owner[0]] and self.command("SMEMBERS", "gateway:principal:" + foreign + ":keys") == [other[0]])
        result, _, _ = self.issue("owner-key", principal)
        self.check("key identifier conflict does not overwrite", result == ["conflict"] and self.command("HGET", "gateway:key:owner-key", "secret_digest") == owner[3])
        result, _, _ = self.issue("limited-key", principal, limit=1)
        self.check("per-principal key bound refuses insertion", result == ["limit"] and self.command("EXISTS", "gateway:key:limited-key") == 0)
        length = self.command("XLEN", "gateway:audit")
        result, _, _ = self.issue("stale-key", principal, stale=True)
        self.check("audit contention causes no key mutation", result == ["audit_retry"] and self.command("EXISTS", "gateway:key:stale-key") == 0 and self.command("XLEN", "gateway:audit") == length)
        request, activity = digest("request"), digest("activity")
        reserved, keys = self.reservation(owner, "first", request, activity)
        self.check("reservation durably binds activity and principal", reserved == ["reserved"] and self.command("GET", keys[6]) == principal and self.command("GET", keys[7]) == keys[2] and self.command("SISMEMBER", "gateway:pending", keys[2]) == 1)
        self.check("reservation increments quota once", self.command("GET", keys[1]) == "1" and 0 < self.command("TTL", keys[1]) <= 120)
        retry, _ = self.reservation(owner, "first", request, activity)
        self.check("pending idempotency replay does not consume quota", retry == ["existing", request, "pending", "", "", principal] and self.command("GET", keys[1]) == "1")
        conflict, _ = self.reservation(owner, "first", digest("changed-request"), activity)
        self.check("conflicting retry exposes original digest", conflict[0:2] == ["existing", request] and conflict[1] != digest("changed-request") and self.command("GET", keys[1]) == "1")
        foreign_attempt, _ = self.reservation(other, "foreign-scope", request, activity)
        self.check("foreign principal cannot take retained activity", foreign_attempt == ["conflict"] and self.command("GET", keys[6]) == principal)
        operation_attempt, _ = self.reservation(owner, "other-scope", request, activity)
        self.check("activity operation cannot fork idempotency scope", operation_attempt == ["conflict"] and self.command("GET", keys[7]) == keys[2])
        self.stop(crash=True)
        self.start()
        retry, _ = self.reservation(owner, "first", request, activity)
        self.check("AOF restart retains pending continuation", retry[0:3] == ["existing", request, "pending"] and self.command("HGET", keys[2], "continuation_0") == "retained-continuation-bytes" and self.command("GET", keys[1]) == "1")
        self.check("completion rejects changed request digest", self.complete(keys, digest("wrong"), principal, "7b7d") == ["conflict"] and self.command("HGET", keys[2], "state") == "pending")
        self.check("completion retains real KV response bytes", self.complete(keys, request, principal, "7b7d") == ["completed"] and self.command("HGET", keys[2], "response") == "7b7d" and self.command("SISMEMBER", "gateway:pending", keys[2]) == 0)
        length = self.command("XLEN", "gateway:audit")
        self.check("completion replay cannot replace durable response", self.complete(keys, request, principal, "7b226368616e676564223a747275657d") == ["completed"] and self.command("HGET", keys[2], "response") == "7b7d" and self.command("XLEN", "gateway:audit") == length)
        reserved, second = self.reservation(owner, "second", digest("second-request"), digest("second-activity"))
        self.check("second request reaches exact key quota", reserved == ["reserved"] and self.command("GET", second[1]) == "2")
        denied, third = self.reservation(owner, "third", digest("third-request"), digest("third-activity"))
        self.check("per-key limit returns bounded retry metadata", denied == ["rate_limited", "120"] and self.command("EXISTS", third[2]) == 0 and self.command("GET", second[1]) == "2")
        self.check("read-side quota consumes and refuses exact boundary", [self.consume(other) for _ in range(3)] == [["consumed"], ["consumed"], ["rate_limited"]])
        replacement = list(owner)
        replacement[0] = "replacement-key"
        replacement[2] = digest("gateway-key-salt-v1", replacement[0])
        replacement_secret = "lxp_live_" + secrets.token_hex(32)
        replacement[3] = digest("gateway-key-v1", replacement[2], replacement_secret)
        self.check("rotation forbids foreign principal", self.rotate(owner, replacement, foreign) == ["conflict"] and self.command("HGET", "gateway:key:owner-key", "disabled") == "0")
        self.check("rotation respects principal key bound", self.rotate(owner, replacement, limit=1) == ["limit"] and self.command("EXISTS", "gateway:key:replacement-key") == 0)
        self.check("rotation atomically disables predecessor", self.rotate(owner, replacement) == ["rotated"] and self.command("HGET", "gateway:key:owner-key", "disabled") == "1" and self.command("HGET", "gateway:key:owner-key", "epoch") == "2" and self.command("HGET", "gateway:key:replacement-key", "disabled") == "0")
        self.check("stale predecessor cannot reserve or consume", self.reservation(owner, "revoked", request, digest("revoked"))[0] == ["revoked"] and self.consume(owner) == ["revoked"])
        self.check("rotation retry cannot overwrite replacement", self.rotate(owner, replacement) == ["conflict"])
        self.check("revocation forbids foreign principal", self.revoke(replacement, foreign) == ["forbidden"] and self.command("HGET", "gateway:key:replacement-key", "disabled") == "0")
        self.check("revocation is durable and idempotent", self.revoke(replacement) == ["revoked"] and self.revoke(replacement) == ["already_revoked"])
        self.stop(crash=True)
        self.start()
        completed, _ = self.reservation(other, "separate", digest("separate"), digest("separate-activity"))
        self.check("restart preserves revoked epochs and quota isolation", self.consume(replacement) == ["revoked"] and self.command("HGET", "gateway:key:replacement-key", "epoch") == "2" and completed == ["reserved"])
        self.check("restart preserves completed response without reexecution", self.command("HGET", "gateway:idem:first", "state") == "completed" and self.command("HGET", "gateway:idem:first", "response") == "7b7d")
        audit = self.command("XRANGE", "gateway:audit", "-", "+")
        serialized = json.dumps(audit)
        self.check("actual KV audit contains no raw credential", all(value not in serialized for value in (secret, other_secret, replacement_secret)) and "secret_digest" not in serialized and "salt" not in serialized)
        previous = ""
        for _, entries in audit:
            values = dict(zip(entries[::2], entries[1::2]))
            if values["previous"] != previous or values["chain"] != digest(previous, values["event"]):
                raise AssertionError("actual audit hash chain is discontinuous")
            previous = values["chain"]
        self.check("durable audit chain agrees with retained head", previous == self.command("GET", "gateway:audit:head"))
        self.check("production Lua source remains unchanged", hashlib.sha256(SOURCE.read_bytes()).hexdigest() == self.source_digest)
        print("GATEWAY_KEY_STORE_RESULT " + json.dumps({"tests": self.tests, "skipped": 0}), flush=True)


def main():
    server, client = shutil.which("redis-server"), shutil.which("redis-cli")
    if not server or not client:
        print("missing prerequisite: genuine redis-server and redis-cli", file=sys.stderr)
        return 78
    with tempfile.TemporaryDirectory(prefix="gateway-key-store-") as directory:
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
        print("FAIL gateway-key-store: " + str(error), file=sys.stderr)
        sys.exit(1)
