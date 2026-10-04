#!/usr/bin/env bash
set -euo pipefail
if (($#)); then
    echo "usage: $0 (no arguments)" >&2
    exit 2
fi
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
cd "$root"
exec python3 - "$root" <<'PY'
import os
import base64
import importlib.util
import json
from pathlib import Path
import re
import stat
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.parse

sys.dont_write_bytecode = True
root = Path(sys.argv[1]).resolve()
count = 0

class Refusal(Exception):
    pass

def require(condition, reason):
    if not condition:
        raise Refusal(reason)

def protected(path, directory=False):
    info = path.lstat()
    require(path.is_absolute() and path.resolve() == path and info.st_uid == os.geteuid()
            and not info.st_mode & 0o077
            and (stat.S_ISDIR(info.st_mode) if directory else stat.S_ISREG(info.st_mode)),
            "protected genuine qualification inputs required")
    if not directory:
        require(0 < info.st_size <= 16 * 1024 * 1024 and info.st_nlink == 1,
                "bounded genuine qualification input required")

def connected_history():
    specification = importlib.util.spec_from_file_location("migration_history_real_process",
        root / "tools/qualification/paxeer-x/ramp-source-settlement.py")
    ramp = importlib.util.module_from_spec(specification)
    specification.loader.exec_module(ramp)
    fixture = ramp.document(os.environ["LAYERX_MIGRATION_HISTORY_FIXTURE"])
    ramp.closed(fixture, ("version", "execution_domain", "ramp_fixture_file", "unified_gateway", "accepted", "empty"), "history fixture")
    ramp.require(fixture["version"] == "external-history-connected-fixture-v2"
        and fixture["execution_domain"] == "isolated-real-process", "genuine isolated history fixture required")
    contract = ramp.Contract(ramp.document(fixture["ramp_fixture_file"]))
    gateway = fixture["unified_gateway"]
    ramp.closed(gateway, ("artifact", "environment_file", "url"), "unified gateway")
    binary = ramp.artifact(gateway["artifact"], contract.fixture["source_revision"])
    environment = ramp.document(gateway["environment_file"])
    ramp.require(isinstance(environment, dict) and environment
        and all(key.startswith("LAYERX_GATEWAY_") and isinstance(value, str)
                for key, value in environment.items()), "genuine gateway environment required")
    parsed = urllib.parse.urlsplit(gateway["url"])
    ramp.require(parsed.scheme == "https" and parsed.hostname in ("localhost", "127.0.0.1")
        and parsed.port and parsed.path in ("", "/") and not parsed.username and not parsed.password
        and not parsed.query and not parsed.fragment
        and environment.get("LAYERX_GATEWAY_LISTEN") == "127.0.0.1:" + str(parsed.port),
        "isolated unified gateway listener required")
    profile = ramp.document(contract.interop_env["LAYERX_INTEROP_MIGRATION_V2_CONFIG"])
    journal = profile["history_journal"]
    journal_path = Path(journal["directory"]) / journal["namespace"]
    ramp.require(journal_path.is_absolute() and journal_path.resolve() == journal_path
        and (not journal_path.exists() or not list(journal_path.iterdir())), "fresh history namespace required")
    child = None
    log = None
    def post(path, body, authority="customer", key=None):
        return contract.http("POST", path, body, authority=authority, base=gateway["url"],
            idempotency=key or "history-" + str(time.time_ns()))
    def snapshot():
        return {p.name: ramp.private(p, 256 * 1024) for p in journal_path.iterdir()}
    def source_snapshots():
        return {chain:{p.name:ramp.private(p, 256 * 1024)
            for p in (Path(profile[chain]["journal"]["directory"]) / profile[chain]["journal"]["namespace"]).iterdir()}
            for chain in ("ethereum", "solana")}
    def page(authority="customer", cursor=None, limit=1):
        status, response = post("/v2/migration/history/read", {"cursor":cursor, "limit":limit}, authority)
        ramp.require(status == 200 and response.get("ok") is True, "authenticated unified history read refused")
        result = response["result"]
        ramp.closed(result, ("state", "records", "next_cursor"), "history page")
        ramp.require(result["state"] == "external-history" and isinstance(result["records"], list)
            and len(result["records"]) <= limit, "bounded external history page required")
        return result
    def records():
        result = []
        cursor = None
        for _ in range(512):
            current = page(cursor=cursor)
            for record in current["records"]:
                ramp.closed(record, ("network", "transaction", "address", "kind", "timestamp", "asset", "amount", "provenance", "layerx_receipt"), "external history row")
                ramp.require(record["layerx_receipt"] is False and record["provenance"] in ("ethereum-external", "solana-external")
                    and record not in result, "external provenance and duplicate refusal required")
                result.append(record)
            following = current["next_cursor"]
            if following is None:
                return result
            ramp.require(isinstance(following, str) and re.fullmatch("[0-9a-f]{64}", following)
                and following != cursor, "actual advancing history cursor required")
            cursor = following
        raise ramp.Refusal("bounded history pagination exhausted")
    def refused(path, body, authority="customer"):
        before = (snapshot(), source_snapshots())
        status, response = post(path, body, authority)
        ramp.require(status in (400, 401, 403, 409, 422) and response.get("ok") is False
            and isinstance(response.get("error", {}).get("code"), str)
            and (snapshot(), source_snapshots()) == before, "history refusal changed durable source associations")
        contract.passed("unified-history-refusal")
    try:
        contract.start_interop()
        log = (contract.work / "unified-gateway.log").open("wb")
        actual_env = {key:value for key,value in os.environ.items() if not key.startswith("LAYERX_GATEWAY_")}
        actual_env.update(environment)
        child = subprocess.Popen([binary], cwd=root, env=actual_env, stdout=log, stderr=subprocess.STDOUT)
        deadline = time.monotonic() + 30
        while time.monotonic() < deadline:
            ramp.require(child.poll() is None, "actual unified gateway startup refused")
            try:
                status, _ = contract.http("GET", "/livez", base=gateway["url"])
                if status == 200:
                    break
            except (OSError, urllib.error.URLError):
                pass
            time.sleep(0.1)
        else:
            raise ramp.Refusal("unified gateway readiness timeout")
        ramp.require(records() == [] and page("other_customer")["records"] == [], "fresh principal history required")
        accepted = fixture["accepted"]
        empty = fixture["empty"]
        for cases in (accepted, empty):
            ramp.require(isinstance(cases, list) and len(cases) == 2
                and {case["chain"] for case in cases} == {"ethereum", "solana"}, "both genuine source history captures required")
        expected = []
        for case in accepted + empty:
            ramp.closed(case, ("chain", "ownership_evidence_file", "source_evidence_file", "expected_records"), "owned source history case")
            ramp.require(isinstance(case["expected_records"], list)
                and (bool(case["expected_records"]) if case in accepted else not case["expected_records"]), "actual nonempty and empty captures required")
            ownership_proof = ramp.private(case["ownership_evidence_file"], 1024 * 1024)
            history_proof = ramp.private(case["source_evidence_file"], 1024 * 1024)
            body = {"chain":case["chain"],
                "ownership_evidence":base64.b64encode(ownership_proof).decode("ascii"),
                "source_evidence":base64.b64encode(history_proof).decode("ascii")}
            refused("/v2/migration/history", body, "other_customer")
            refused("/v2/migration/history", dict(body, principal="other"))
            refused("/v2/migration/history", dict(body, layerx_receipt=True))
            corrupted = ownership_proof[:-1] + bytes([ownership_proof[-1] ^ 0x80])
            refused("/v2/migration/history", dict(body, ownership_evidence=base64.b64encode(corrupted).decode("ascii")))
            domain, network_bytes = ((b"LXM/ETH/HISTORY/1\0", 8) if case["chain"] == "ethereum"
                else (b"LXM/SOL/HISTORY/1\0", 32))
            offset = len(domain) + network_bytes
            ramp.require(history_proof.startswith(domain) and len(history_proof) > offset, "genuine canonical history claim required")
            mismatched = bytearray(history_proof)
            mismatched[offset] ^= 1
            refused("/v2/migration/history", dict(body, source_evidence=base64.b64encode(mismatched).decode("ascii")))
            status, response = post("/v2/migration/history", body)
            ramp.require(status == 200 and response.get("ok") is True
                and response["result"]["state"] == "external-history-imported"
                and response["result"]["layerx_receipt"] is False
                and response["result"]["record_count"] == len(case["expected_records"]), "verified owned source import refused")
            before = snapshot()
            status, replay = post("/v2/migration/history", body)
            ramp.require(status == 200 and replay.get("ok") is True and replay["result"] == response["result"]
                and snapshot() == before, "real history replay duplicated durable association")
            expected.extend(case["expected_records"])
            actual = records()
            ramp.require(len(actual) == len(expected) and all(record in actual for record in expected)
                and page("other_customer")["records"] == [], "actual history or principal isolation mismatch")
            contract.passed("unified-owned-" + case["chain"] + "-history-and-replay")
        for body in ({"cursor":None,"limit":0}, {"cursor":None,"limit":257},
                     {"cursor":"00" * 32,"limit":1}, {"cursor":None,"limit":1,"principal":"other"}):
            refused("/v2/migration/history/read", body)
        before = snapshot()
        contract.interop_child.terminate()
        contract.interop_child.wait(timeout=10)
        contract.interop_child = None
        contract.interop_log.close()
        contract.start_interop()
        actual = records()
        ramp.require(snapshot() == before and len(actual) == len(expected)
            and all(record in actual for record in expected) and page("other_customer")["records"] == [],
            "history recovery lost provenance or principal ownership")
        contract.passed("unified-history-real-restart-recovery")
        return contract.count
    except ramp.Refusal as error:
        raise Refusal("genuine unified history corpus refused: " + str(error)) from None
    finally:
        if child is not None:
            child.terminate()
            try:
                child.wait(timeout=10)
            except subprocess.TimeoutExpired:
                child.kill()
                child.wait(timeout=10)
        if log is not None:
            log.close()
        contract.close()

def main():
    global count
    evidence = Path(os.environ.get("PAXEER_X_EVIDENCE_DIR", ""))
    protected(evidence, True)
    require(root != evidence and root not in evidence.parents, "private evidence directory required")
    secret_directory = Path(os.environ.get("LAYERX_MIGRATION_SECRET_DIR", ""))
    protected(secret_directory, True)
    required = ["LAYERX_MIGRATION_V2_FIXTURE", "LAYERX_MIGRATION_FUNDED_OPERATOR_FIXTURE", "LAYERX_MIGRATION_HISTORY_FIXTURE"]
    for chain in ("ETHEREUM", "SOLANA"):
        required.extend("LAYERX_" + chain + suffix for suffix in (
            "_CONFIG", "_OWNERSHIP_EVIDENCE", "_ASSET_EVIDENCE", "_HISTORY_EVIDENCE", "_HISTORY_STORE_CONFIG"))
    for variable in required:
        raw = os.environ.get(variable)
        require(bool(raw), "missing genuine input " + variable)
        protected(Path(raw))
    target = Path(os.environ.get("CARGO_TARGET_DIR", "/root/lx-target/interop")).resolve() / "debug" / "deps"
    require(target.is_dir(), "compiled migration test targets required")
    families = {"layerx_migrate": 2, "operator": 1, "funded_operator": 1,
                "migration_v2_connected": 1, "testnets": 1}
    executables = {}
    for family, expected in families.items():
        paths = [p for p in target.iterdir() if re.fullmatch(re.escape(family) + r"-[0-9a-f]+", p.name)
                 and p.is_file() and not p.is_symlink() and os.access(p, os.X_OK)]
        require(len(paths) == expected, "exact compiled migration target set required: " + family)
        executables[family] = sorted(paths)
    deadline = time.monotonic() + 29 * 60
    for family, paths in executables.items():
        family_count = 0
        for executable in paths:
            listing = subprocess.run([str(executable), "--list"], stdin=subprocess.DEVNULL,
                                     capture_output=True, text=True, timeout=30)
            require(listing.returncode == 0, "actual test discovery refused")
            names = re.findall(r"^(.+): test$", listing.stdout, re.M)
            require(len(names) == len(set(names)), "duplicate migration test identity refused")
            if family != "layerx_migrate":
                require(bool(names), "empty migration target refused")
            remaining = deadline - time.monotonic()
            require(remaining > 0, "bounded migration gate expired")
            descriptor, log_path = tempfile.mkstemp(prefix="migration-104.24.1-" + executable.name + "-", suffix=".log", dir=evidence)
            path = Path(log_path)
            with os.fdopen(descriptor, "wb") as log:
                result = subprocess.run([str(executable), "--include-ignored", "--nocapture", "--test-threads=1"],
                                        stdin=subprocess.DEVNULL, stdout=log, stderr=subprocess.STDOUT,
                                        timeout=remaining, cwd=root)
            print("MIGRATION_SCOPED_TARGET name=" + family + " exit=" + str(result.returncode)
                  + " log=" + str(path))
            require(result.returncode == 0, "actual migration target refused")
            lines = path.read_text(encoding="utf-8").splitlines()
            summaries = [re.fullmatch(r"test result: ok\. ([0-9]+) passed; 0 failed; 0 ignored; 0 measured; 0 filtered out; finished in .+", line)
                         for line in lines if line.startswith("test result:")]
            require(len(summaries) == 1 and summaries[0] is not None
                    and int(summaries[0].group(1)) == len(names), "complete zero-skipped migration coverage required")
            if family == "migration_v2_connected":
                require(lines.count("MIGRATION_V2_CONNECTED_COMPLETE ethereum=1 solana=1 funded=2 skipped=0") == 1,
                        "genuine connected Ethereum and Solana mapping/funding required")
            if family == "funded_operator":
                require(lines.count("MIGRATION_FUNDED_OPERATOR_COMPLETE ethereum=1 solana=1 funded=2 pending=1 skipped=0") == 1,
                        "genuine funded operator coverage required")
            family_count += len(names)
        require(family_count > 0, "empty retained migration contract refused")
        if family == "testnets":
            require(family_count == 6, "both real source and history testnet contracts required")
        count += family_count
    count += connected_history()

try:
    main()
    print(f"PAXEER_X_GATE tests={count} skipped=0")
except (Refusal, OSError, ValueError, KeyError, TypeError, subprocess.SubprocessError) as error:
    print("migration scoped acceptance refused: " + (str(error) if isinstance(error, Refusal) else "genuine qualification inputs or processes unavailable"), file=sys.stderr)
    print(f"PAXEER_X_GATE tests={count} skipped=0")
    sys.exit(1)
PY
