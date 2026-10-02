#!/usr/bin/env python3
import sys

sys.dont_write_bytecode = True

import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import socket
import subprocess
import tempfile
import time
import urllib.error
import urllib.request

ROOT = Path(__file__).resolve().parents[3]
CASES = ("rate-publication-recovery",)
RATE_GAS_LIMIT = 60_000
PUBLICATION_VERSION = 2
INPUTS = ("PAXEER_X_GAS_STATION_SERVICE_CONFIG", "PAXEER_X_GAS_STATION_PUBLISHER_CONFIG",
          "PAXEER_X_GAS_STATION_RATE_FILE", "PAXEER_X_GAS_STATION_QUOTE_REQUEST")


class Failure(Exception):
    pass


def require(condition, message):
    if not condition:
        raise Failure(message)


def unused_port():
    with socket.socket() as connection:
        connection.bind(("127.0.0.1", 0))
        return connection.getsockname()[1]


def write_json(path, value):
    path.write_text(json.dumps(value, sort_keys=True) + "\n")
    path.chmod(0o600)


def hexed(value):
    require(isinstance(value, list) and all(isinstance(b, int) and 0 <= b < 256 for b in value),
            f"journal byte field is not a byte array: {value!r}")
    return "0x" + bytes(value).hex()


def load_candidate(path):
    sys.path.insert(0, str(ROOT / "tools/paxeer-x"))
    from candidate import Invalid, catalogue, load_private, validate
    try:
        data = load_private(path)
        validate(data, catalogue(ROOT / "spec/paxeer-x/spec.kvx"))
    except (Invalid, OSError, ValueError, KeyError, TypeError) as error:
        raise Failure(f"candidate manifest is absent or invalid: {error}") from error
    services = {service["id"]: service for service in data["services"]}
    require("gas" in services, "candidate manifest does not declare the gas service")
    gas = services["gas"]
    return {
        "sha256": hashlib.sha256(json.dumps(data, sort_keys=True, separators=(",", ":")).encode()).hexdigest(),
        "source_revision": data["source"]["revision"],
        "gas_action": gas["action"],
        "gas_bindings": gas["bindings"],
        "gas_observations": len(gas["observations"]),
    }


def journal_entries(path):
    if not path.exists():
        return []
    entries = []
    for line in path.read_bytes().splitlines():
        if line.strip():
            entries.append(json.loads(line))
    return entries


class Contract:
    def __init__(self, binary, candidate, inputs):
        self.binary = binary
        self.candidate = candidate
        self.work = Path(tempfile.mkdtemp(prefix="layerx-gas-station-recovery-"))
        self.work.chmod(0o700)
        self.state = self.work / "state"
        self.state.mkdir(mode=0o700)
        self.children = []
        self.logs = []
        self.cases = []
        self.refusals = []
        self.service_config = json.loads(Path(inputs["PAXEER_X_GAS_STATION_SERVICE_CONFIG"]).read_text())
        self.publisher_config = json.loads(Path(inputs["PAXEER_X_GAS_STATION_PUBLISHER_CONFIG"]).read_text())
        self.rate_text = Path(inputs["PAXEER_X_GAS_STATION_RATE_FILE"]).read_text()
        self.quote_request = Path(inputs["PAXEER_X_GAS_STATION_QUOTE_REQUEST"]).read_bytes()
        for config, key in ((self.service_config, "relayer_key_env"), (self.publisher_config, "rate_owner_key_env")):
            name = config.get(key)
            require(isinstance(name, str) and os.environ.get(name), f"key source {key} names an unset environment variable")
        require(self.publisher_config["rate_owner_key_env"] != self.publisher_config["relayer_key_env"],
                "sponsor and paymaster-owner key sources are not distinct")
        for name in ("chain_id", "paymaster"):
            require(self.publisher_config[name] == self.service_config[name], f"station and publisher disagree on {name}")
        self.publisher_path = self.work / "rate.json"
        write_json(self.publisher_path, self.publisher_config)
        self.rate_file = self.state / "rate.toml"
        self.rate_file.write_text(self.rate_text)
        self.journal = self.state / "rate.jsonl"
        self.station_journal = self.state / "sponsorship.jsonl"
        self.port = unused_port()
        station = dict(self.service_config, listen=f"127.0.0.1:{self.port}")
        self.station_path = self.work / "station.json"
        write_json(self.station_path, station)
        self.max_rate_age = int(self.publisher_config["max_rate_age"])
        self.cadence = int(self.publisher_config["rate_cadence_seconds"])

    def passed(self, name, assertions):
        self.cases.append({"case": name, "assertions": assertions})
        print("PASS " + name, flush=True)

    def refused(self, name, exit_code, detail):
        require(exit_code != 0, f"{name} was not refused")
        self.refusals.append({"case": name, "exit": exit_code, "detail": detail})

    def start(self, name, args):
        output = (self.work / (name + ".log")).open("ab")
        process = subprocess.Popen([str(arg) for arg in args], stdout=output, stderr=subprocess.STDOUT,
                                   cwd=self.state)
        self.children.append(process)
        self.logs.append(output)
        return process

    def log(self, name):
        path = self.work / (name + ".log")
        return path.read_text(errors="replace") if path.exists() else ""

    def stop(self, process):
        if process.poll() is None:
            process.terminate()
            try:
                process.wait(timeout=15)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait(timeout=15)

    def close(self):
        for process in reversed(self.children):
            self.stop(process)
        for output in self.logs:
            output.close()

    def until(self, condition, label, seconds):
        deadline = time.monotonic() + seconds
        last = None
        while time.monotonic() < deadline:
            try:
                value = condition()
                if value:
                    return value
                last = f"condition returned {value!r}"
            except (OSError, ValueError, KeyError, TypeError, Failure) as error:
                last = error
            time.sleep(0.002)
        raise Failure(f"timed out: {label}; last: {last}")

    def publisher(self, name, *, config=None, journal=None, rate_file=None):
        return self.start(name, [self.binary, "rate", "--config", config or self.publisher_path,
                                 "--journal", journal or self.journal, "--rate-file", rate_file or self.rate_file])

    def run_to_exit(self, name, args, seconds=60):
        process = self.start(name, args)
        try:
            return process.wait(timeout=seconds)
        except subprocess.TimeoutExpired:
            self.stop(process)
            raise Failure(f"{name} did not exit within {seconds} s")

    def quote(self):
        request = urllib.request.Request(f"http://127.0.0.1:{self.port}/quote", data=self.quote_request,
                                          headers={"Content-Type": "application/json"}, method="POST")
        try:
            response = urllib.request.urlopen(request, timeout=10)
        except urllib.error.HTTPError as error:
            response = error
        with response:
            return response.status, json.loads(response.read() or b"null")

    def kinds(self, path=None):
        return [entry["kind"] for entry in journal_entries(path or self.journal)]

    def check_prepared(self, prepared):
        require(prepared["version"] == PUBLICATION_VERSION, "prepared publication is not version 2")
        require(prepared["chain_id"] == int(self.publisher_config["chain_id"]), "prepared chain differs from config")
        require(hexed(prepared["paymaster"]).lower() == self.publisher_config["paymaster"].lower(),
                "prepared paymaster differs from config")
        publication = prepared["publication"]
        raw = bytes(prepared["raw"])
        require(raw[:1] == b"\x02", "prepared bytes are not a typed EIP-1559 transaction")
        for field in ("owner", "nonce", "hash", "rate", "gas_limit", "max_fee_per_gas", "signed_at"):
            require(field in publication, f"prepared publication lacks {field}")
        require(publication["gas_limit"] == RATE_GAS_LIMIT, "prepared gas limit is not the publisher limit")
        require(int(publication["max_fee_per_gas"]) <= int(self.publisher_config["rate_max_fee_per_gas"]),
                "prepared fee exceeds the configured ceiling")
        text = json.dumps(prepared).lower()
        for name in (self.publisher_config["rate_owner_key_env"], self.service_config["relayer_key_env"]):
            secret = os.environ[name].lower().removeprefix("0x")
            require(secret not in text, "a key entered the journal record")
        return hexed(publication["hash"]), publication["nonce"]

    def families(self):
        families = {}
        settled = {}
        for entry in journal_entries(self.journal):
            kind = entry["kind"]
            if kind in ("rate_prepared", "rate_replaced"):
                prepared = entry["prepared"]
                tx_hash, nonce = self.check_prepared(prepared)
                key = (hexed(prepared["publication"]["owner"]), nonce)
                family = families.setdefault(key, {"hashes": [], "finalized": None, "cancelled": False})
                if kind == "rate_replaced":
                    require(hexed(entry["previous"]) in family["hashes"], "replacement names a hash outside its family")
                else:
                    require(not family["hashes"] or family["finalized"] is not None,
                            "second prepared family at one unresolved owner nonce")
                family["hashes"].append(tx_hash)
            elif kind == "rate_cancelled":
                previous = hexed(entry["previous"])
                owners = [key for key, family in families.items() if previous in family["hashes"]]
                require(len(owners) == 1, "cancellation names a hash outside one family")
                families[owners[0]]["cancelled"] = True
            elif kind == "rate_settled":
                settled[hexed(entry["hash"])] = entry["settlement"]
        for key, family in families.items():
            finals = [h for h in family["hashes"] if h in settled]
            require(len(finals) <= 1, f"nonce family {key} finalized more than once")
            if finals:
                family["finalized"] = finals[0]
        return families

    def config_refusals(self):
        bad_cadence = dict(self.publisher_config, rate_cadence_seconds=self.max_rate_age)
        path = self.work / "bad-cadence.json"
        write_json(path, bad_cadence)
        code = self.run_to_exit("refuse-cadence", [self.binary, "rate", "--config", path, "--journal",
                                                   self.work / "unused.jsonl", "--rate-file", self.rate_file])
        require(code == 2, f"cadence at max_rate_age exited {code}, expected 2")
        self.refused("cadence-not-below-max-age", code, self.log("refuse-cadence").strip()[-300:])
        same_key = dict(self.publisher_config, rate_owner_key_env=self.publisher_config["relayer_key_env"])
        path = self.work / "same-key.json"
        write_json(path, same_key)
        code = self.run_to_exit("refuse-same-key", [self.binary, "rate", "--config", path, "--journal",
                                                    self.work / "unused.jsonl", "--rate-file", self.rate_file])
        require(code == 2, f"shared sponsor/owner key exited {code}, expected 2")
        self.refused("sponsor-equals-owner-key", code, self.log("refuse-same-key").strip()[-300:])
        self.passed("publisher configuration refuses cadence at max age and a shared key source",
                    ["rate_cadence_seconds=max_rate_age exit=2", "rate_owner_key_env=relayer_key_env exit=2"])

    def rate_input_refusals(self):
        future = self.work / "future.toml"
        lines = [line for line in self.rate_text.splitlines() if not line.strip().startswith(("set_at", "not_after"))]
        future.write_text("\n".join(lines + ["set_at = 18446744073709551614"]) + "\n")
        for name, rate_file, marker in (("future-rate", future, "NotYetSet"),
                                        ("missing-rate", self.work / "absent.toml", "RateFileMissing")):
            journal = self.work / (name + ".jsonl")
            process = self.publisher(name, journal=journal, rate_file=rate_file)
            self.until(lambda: marker in self.log(name), f"{name} refusal", 120)
            self.stop(process)
            require(not any(kind in ("rate_prepared", "rate_published") for kind in self.kinds(journal)),
                    f"{name} prepared a publication")
            self.refusals.append({"case": name, "refusal": marker})
        self.passed("future and missing owner rates refuse publication without preparing a transaction",
                    ["future set_at -> NotYetSet, no rate_prepared", "missing rate file -> RateFileMissing"])

    def crash_between_append_and_send(self):
        process = self.start("publisher-1", [self.binary, "rate", "--config", self.publisher_path, "--journal",
                                             self.journal, "--rate-file", self.rate_file,
                                             "--test-only-exit-before-rate-broadcast"])
        try:
            code = process.wait(timeout=self.cadence + 120)
        except subprocess.TimeoutExpired:
            self.stop(process)
            raise Failure("publisher did not reach the test-only exit before broadcast")
        require(code == 3, f"test-only crash point exited {code}, expected 3")
        require("rate publisher test-only exit before broadcast" in self.log("publisher-1"),
                "test-only crash point line absent")
        entries = journal_entries(self.journal)
        prepared = [e for e in entries if e["kind"] == "rate_prepared"]
        require(len(prepared) == 1, f"expected one prepared record at the crash, found {len(prepared)}")
        require(not any(e["kind"] == "rate_broadcast" for e in entries),
                "a broadcast was recorded before the test-only exit")
        tx_hash, nonce = self.check_prepared(prepared[0]["prepared"])
        before = self.journal.read_bytes()
        self.crash = {"hash": tx_hash, "nonce": nonce, "journal_bytes": len(before),
                      "signed_at": prepared[0]["prepared"]["publication"]["signed_at"]}
        process = self.publisher("publisher-2")
        self.until(lambda: f"rate publisher recovering hash={tx_hash} nonce={nonce}" in self.log("publisher-2"),
                   "restart reports recovery of the prepared transaction", 60)
        require(self.journal.read_bytes().startswith(before), "restart rewrote journal history")

        def finalized():
            family = self.families().get((hexed(prepared[0]["prepared"]["publication"]["owner"]), nonce))
            return family is not None and family["finalized"] is not None and family

        family = self.until(finalized, "recovered family finalized", self.max_rate_age)
        require(family["hashes"][0] == tx_hash, "recovered family does not start at the crashed hash")
        later = [e for e in journal_entries(self.journal) if e["kind"] == "rate_prepared"][1:]
        require(all(e["prepared"]["publication"]["nonce"] > nonce for e in later),
                "a new publication was prepared at or below the unresolved nonce")
        order = self.kinds()
        first_new = next((i for i, e in enumerate(journal_entries(self.journal)) if e["kind"] == "rate_prepared"
                          and e["prepared"]["publication"]["nonce"] > nonce), None)
        settled_at = next(i for i, e in enumerate(journal_entries(self.journal))
                          if e["kind"] == "rate_settled" and hexed(e["hash"]) in family["hashes"])
        require(first_new is None or settled_at < first_new, "new publication constructed before recovery finalized")
        self.publisher_process = process
        self.passed("crash after rate_prepared and before broadcast recovers the same signed transaction first",
                    [f"test-only exit 3 with prepared hash={tx_hash} nonce={nonce} and no rate_broadcast",
                     "restart prints recovering line for that hash", "history is an unchanged prefix",
                     f"finalized hash in family {family['hashes']}", "no new nonce prepared before finalization",
                     f"journal kinds {order}"])

    def history_coherence(self):
        families = self.families()
        require(families, "no prepared publication families were journalled")
        ceiling = int(self.publisher_config["rate_max_fee_per_gas"]) * RATE_GAS_LIMIT
        for key, family in families.items():
            require(len(set(family["hashes"])) == len(family["hashes"]), f"family {key} repeats a hash")
        replaced = sum(len(f["hashes"]) - 1 for f in families.values())
        cancelled = sum(1 for f in families.values() if f["cancelled"])
        self.history = {"families": len(families), "replacements": replaced, "cancellations": cancelled}
        self.passed("replacement and cancellation keep one nonce family with a bounded reservation",
                    [f"families={len(families)} replacements={replaced} cancellations={cancelled}",
                     "every member shares owner/chain/paymaster/nonce", "at most one finalized hash per nonce",
                     f"every member fee*gas <= {ceiling}"])

    def station_quotes(self):
        station = self.start("station", [self.binary, "--config", self.station_path, "--journal", self.station_journal])
        self.until(lambda: "serving POST /quote" in self.log("station"), "station listening", 30)
        status, body = self.quote()
        require(status == 200, f"fresh governed rate did not quote: {status} {body}")
        self.stop(self.publisher_process)
        stale_after = self.crash["signed_at"] + self.max_rate_age
        self.until(lambda: self.quote()[0] != 200, "quote refused once the rate exceeds max_rate_age",
                   self.max_rate_age + self.cadence + 60)
        status, body = self.quote()
        require(status in (422, 503), f"stale rate quote returned {status} {body}")
        self.refusals.append({"case": "stale-rate-quote", "status": status, "body": body})
        restart = time.monotonic()
        self.publisher_process = self.publisher("publisher-3")
        self.until(lambda: self.quote()[0] == 200, "quotes resume after publisher restart", self.max_rate_age)
        self.stop(station)
        self.passed("stale governed rate refuses quotes and a publisher restart restores them",
                    ["fresh quote 200", f"stale quote {status} after signed_at+{self.max_rate_age} ({stale_after})",
                     f"quote 200 again {time.monotonic() - restart:.1f} s after restart"])

    def fail_closed(self):
        self.stop(self.publisher_process)
        history = self.journal.read_bytes()
        cases = []
        for name, content in (("interrupted-write", history + history.splitlines()[-1][: len(history.splitlines()[-1]) // 2]),
                              ("malformed-bytes", history + b"\x00\xff{not json\n")):
            journal = self.work / (name + ".jsonl")
            journal.write_bytes(content)
            journal.chmod(0o600)
            code = self.run_to_exit(name, [self.binary, "rate", "--config", self.publisher_path, "--journal", journal,
                                           "--rate-file", self.rate_file])
            require(code == 1, f"{name} exited {code}, expected 1")
            require(journal.read_bytes() == content, f"{name} altered the journal")
            self.refused(name, code, self.log(name).strip()[-300:])
            cases.append(f"{name} exit=1 journal unchanged")
        changed = dict(self.publisher_config, chain_id=int(self.publisher_config["chain_id"]) + 1)
        path = self.work / "changed-chain.json"
        write_json(path, changed)
        copy = self.work / "changed-chain.jsonl"
        shutil.copyfile(self.journal, copy)
        code = self.run_to_exit("changed-chain", [self.binary, "rate", "--config", path, "--journal", copy,
                                                  "--rate-file", self.rate_file])
        require(code != 0 and copy.read_bytes() == history, f"changed chain exited {code} or altered history")
        self.refused("changed-chain", code, self.log("changed-chain").strip()[-300:])
        cases.append(f"changed chain exit={code} journal unchanged")
        owner = hexed(journal_entries(self.journal)[0]["prepared"]["publication"]["owner"])
        changed = dict(self.publisher_config, paymaster=owner)
        path = self.work / "changed-paymaster.json"
        write_json(path, changed)
        copy = self.work / "changed-paymaster.jsonl"
        shutil.copyfile(self.journal, copy)
        process = self.publisher("changed-paymaster", config=path, journal=copy)
        try:
            code = process.wait(timeout=self.cadence + 30)
        except subprocess.TimeoutExpired:
            self.stop(process)
            code = None
        require(copy.read_bytes().startswith(history), "changed paymaster rewrote history")
        added = [e for e in journal_entries(copy)[len(journal_entries(self.journal)):]
                 if e["kind"] in ("rate_prepared", "rate_replaced")]
        require(not any(hexed(e["prepared"]["paymaster"]).lower() == owner.lower() for e in added),
                "a publication was prepared for the changed paymaster")
        self.refusals.append({"case": "changed-paymaster", "exit": code, "detail": self.log("changed-paymaster").strip()[-300:]})
        cases.append(f"changed paymaster exit={code} no publication to it, history kept")
        self.passed("interrupted write, malformed bytes and changed chain or paymaster fail closed", cases)

    def run(self):
        self.config_refusals()
        self.rate_input_refusals()
        self.crash_between_append_and_send()
        self.history_coherence()
        self.station_quotes()
        self.fail_closed()


def revision():
    result = subprocess.run(["git", "--no-optional-locks", "-C", str(ROOT), "rev-parse", "HEAD"],
                            capture_output=True, text=True, timeout=30)
    return result.stdout.strip() if result.returncode == 0 else "unknown"


def main():
    parser = argparse.ArgumentParser(prog="gas-station-contract")
    parser.add_argument("--case", required=True, choices=CASES)
    parser.add_argument("--candidate-manifest", required=True)
    arguments = parser.parse_args()
    contract = None
    try:
        require(bool(arguments.candidate_manifest), "candidate manifest path is empty")
        candidate = load_candidate(arguments.candidate_manifest)
        target = os.environ.get("CARGO_TARGET_DIR", str(ROOT / "target"))
        binary = Path(target) / "release/paxeer-gas-station"
        require(binary.is_file() and os.access(binary, os.X_OK),
                f"required built executable {binary} is absent; build -p layerx-gas-station --release")
        inputs = {}
        for name in INPUTS:
            value = os.environ.get(name, "")
            require(value and Path(value).is_file(), f"required input {name} is unset or not a file")
            inputs[name] = value
        contract = Contract(binary, candidate, inputs)
        contract.run()
        require(len(contract.cases) == 6, f"expected 6 executed cases, ran {len(contract.cases)}")
        require(len(contract.refusals) >= 8, f"expected at least 8 recorded refusals, got {len(contract.refusals)}")
        write_json(contract.work / "result.json", {
            "status": "passed", "case": arguments.case, "revision": revision(), "candidate": candidate,
            "crash": contract.crash, "history": contract.history, "cases": contract.cases,
            "refusals": contract.refusals})
        print(f"RESULT passed case={arguments.case} cases={len(contract.cases)}", flush=True)
        print(f"PAXEER_X_GATE tests={len(contract.cases)} skipped=0", flush=True)
        return 0
    except (Failure, OSError, ValueError, KeyError, TypeError, subprocess.SubprocessError) as error:
        print("FAIL " + str(error), flush=True)
        executed = 0 if contract is None else len(contract.cases)
        if contract is not None:
            write_json(contract.work / "result.json", {"status": "failed", "case": arguments.case,
                       "revision": revision(), "cases": contract.cases, "refusals": contract.refusals,
                       "failure": str(error)})
        print(f"PAXEER_X_GATE tests={executed} skipped=0 failed=1", flush=True)
        return 1
    finally:
        if contract is not None:
            contract.close()
            print("Evidence: " + str(contract.work), flush=True)


if __name__ == "__main__":
    raise SystemExit(main())
