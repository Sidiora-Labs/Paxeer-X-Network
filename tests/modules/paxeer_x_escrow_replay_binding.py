#!/usr/bin/env python3
"""Qualify escrow replay binding to the original authorized operation context."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile

sys.dont_write_bytecode = True

ROOT = Path(__file__).resolve().parents[2]
COMMAND = ["timeout", "1800s", "python3", "tests/modules/paxeer_x_escrow_replay_binding.py"]
COMPONENTS = ("test_escrow_open", "test_escrow_capture", "test_escrow_timeout", "test_escrow_dispute",
              "lxp_test_epoch_escrow_timeout")
SOURCES = ("include/layerx/lx_escrow.h", "src/modules/escrow", "tests/modules/test_escrow_open.c",
           "tests/modules/test_escrow_capture.c", "tests/modules/test_escrow_timeout.c",
           "tests/modules/test_escrow_dispute.c", "tests/daemon/lxp_test_epoch_escrow_timeout.c",
           "tests/daemon/lxp_test_escrow_replay_client.c", "tests/modules/paxeer_x_escrow_replay_binding.py")
CONTEXT_MISMATCH = -213
RESULT_V2_BYTES = 276
RESULT_LEGACY_BYTES = 243


def digest(path):
    with Path(path).open("rb") as source:
        return hashlib.file_digest(source, "sha256").hexdigest()


def git(*argv):
    return subprocess.run(["git", *argv], cwd=ROOT, check=True, capture_output=True, text=True).stdout


def runtime_import():
    sys.path.insert(0, str(ROOT / "tests/daemon"))
    import paxeer_x_runtime_fixture as fixture
    return fixture


def rows(raw, prefix):
    return [dict(x.split("=", 1) for x in line.split()[1:]) for line in raw.decode().splitlines()
            if line.startswith(prefix + " ")]


def components(evidence):
    """Run the prebuilt component binaries of this candidate directly."""
    fixture = runtime_import()
    manifest_path = os.environ["PAXEER_X_ESCROW_COMPONENT_MANIFEST"]
    fixture.private(manifest_path)
    manifest = json.loads(Path(manifest_path).read_text())
    revision = git("rev-parse", "HEAD").strip()
    fixture.require(manifest.get("version") == 1 and manifest.get("source_revision") == revision,
                    "component manifest source revision")
    fixture.require(set(manifest.get("artifacts", {})) == set(COMPONENTS), "component artifact set")
    cases = []
    for name in COMPONENTS:
        binary = ROOT / "build/tests" / name
        row = manifest["artifacts"][name]
        fixture.require(binary.is_file() and not binary.is_symlink() and os.access(binary, os.X_OK),
                        "missing prebuilt " + str(binary))
        fixture.require(row.get("source_revision") == revision and row.get("sha256") == digest(binary),
                        "component source/digest mismatch: " + name)
        log = evidence / (name + ".log")
        with log.open("wb") as stream:
            completed = subprocess.run([str(binary)], cwd=ROOT, stdout=stream, stderr=subprocess.STDOUT,
                                       timeout=600, check=False)
        cases.append({"name": "component-" + name, "status": "ok" if completed.returncode == 0 else "failed",
                      "exit_code": completed.returncode, "sha256": digest(binary), "log": str(log)})
        if completed.returncode != 0:
            raise RuntimeError(name + " exited " + str(completed.returncode) + "; see " + str(log))
    return cases


def bound_runtime_artifacts():
    fixture = runtime_import()
    revision = git("rev-parse", "HEAD").strip()
    bundle = fixture.artifacts(os.environ["PAXEER_X_RUNTIME_ARTIFACTS"])
    client = fixture.client_artifact(os.environ["PAXEER_X_RUNTIME_CLIENT_MANIFEST"])
    fixture.require(bundle["source_revision"] == revision and client["source_revision"] == revision,
                    "fresh escrow runtime and client must bind the selected revision")
    fixture.require(client.get("source_path") == "tests/daemon/lxp_test_escrow_replay_client.c" and
                    client.get("source_sha256") == digest(ROOT / client["source_path"]),
                    "escrow driver source binding")
    return bundle, client


def runtime_worker(directory):
    fixture = runtime_import()
    bundle, client = bound_runtime_artifacts()
    for name in ("net", "pid", "mnt"):
        fixture.require(os.readlink("/proc/self/ns/" + name) != os.environ["PAXEER_X_PARENT_" + name.upper()],
                        "namespace isolation")
    fixture.run(["ip", "link", "set", "lo", "up"])
    fixture.run(["mount", "-t", "tmpfs", "-o", "mode=0755,nosuid,nodev", "escrow-fixture", "/tmp"])
    source = Path("/tmp/escrow-source"); source.mkdir(mode=0o755)
    fixture.run(["mount", "--bind", ROOT, source])
    fixture.run(["mount", "-o", "remount,bind,ro,nosuid,nodev", source])
    fixture.ROOT = source
    python_root = Path("/tmp/escrow-python"); python_root.mkdir(mode=0o755)
    fixture.run(["mount", "--bind", sys.prefix, python_root])
    fixture.run(["mount", "-o", "remount,bind,ro,nosuid,nodev", python_root])
    os.environ["PATH"] = str(python_root / "bin") + ":/usr/local/bin:/usr/bin:/bin"
    os.setgroups([])
    os.setgid(fixture.UID)
    runtime = fixture.RuntimeFixture(directory, bundle, client)
    retained, cases = [], []
    sequences = {"alice": 0, "bob": 0}
    labels = ["alice", "bob"]
    try:
        runtime.generate()
        activities = directory / "activities"; activities.mkdir(mode=0o700)
        counter = [0]

        def call(operation, actor, expect, hold="a", amount=0, key=0, bps=0, replay=None, credit=None):
            counter[0] += 1
            seq = 0 if replay else sequences[actor]
            label = "%03d-%s-%s%s" % (counter[0], "replay" if replay else operation, actor, hold)
            wire = replay or activities / (label + ".wire")
            env = runtime.env | {"PAXEER_X_FIXTURE_KEYS": str(directory / "keys"), "ESCROW_ACTOR": actor,
                "ESCROW_HOLD": hold, "ESCROW_AMOUNT": str(amount), "ESCROW_KEY": str(key), "ESCROW_BPS": str(bps),
                "ESCROW_ASSET": fixture.ASSET, "ESCROW_WIRE": str(wire), "ESCROW_EXPECT": str(expect),
                "ESCROW_CREDIT_PROFILE": str(directory / "custody.profile"), "ESCROW_CREDIT_FILE": str(credit or "")}
            completed = subprocess.run([client["path"], str(directory / "run/layerxd.lni.sock"), str(directory / "salt"),
                "replay" if replay else operation, str(seq), "poll"], env=env, capture_output=True, timeout=45)
            (directory / (label + ".log")).write_bytes(completed.stdout + completed.stderr)
            fixture.require(completed.returncode == 0, "escrow client failed: " + label)
            got = rows(completed.stdout, "receipt")
            fixture.require(len(got) == 1 and int(got[0]["result"]) == expect, "unexpected escrow receipt: " + label)
            if replay is None:
                retained.extend(got)
                sequences[actor] += 1
            return got[0], wire

        def state():
            env = runtime.env | {"PAXEER_X_FIXTURE_KEYS": str(directory / "keys"), "ESCROW_READ": ",".join(labels)}
            completed = subprocess.run([client["path"], str(directory / "run/layerxd.lni.sock"), str(directory / "salt"),
                "read", "0", "poll"], env=env, capture_output=True, timeout=30)
            counter[0] += 1
            (directory / ("%03d-state.log" % counter[0])).write_bytes(completed.stdout + completed.stderr)
            fixture.require(completed.returncode == 0, "authenticated escrow state read failed")
            got = rows(completed.stdout, "state")
            fixture.require(len(got) == len(labels) and len({r["root"] for r in got}) == 1,
                            "state witnesses do not share one signed head")
            return {r["label"]: bytes.fromhex(r["raw"]) for r in got}

        def balance(raw):
            n = int.from_bytes(raw[:2], "big")
            return int.from_bytes(raw[3 + n:19 + n], "big")

        def transition(before, after, row, actor, deltas=None, changed=()):
            """Only the listed balances and records move; the actor additionally pays its own fee."""
            deltas = deltas or {}
            fee = int(row["fee"], 16)
            for label in before:
                if label in changed:
                    fixture.require(after[label] != before[label], label + " did not change")
                    continue
                delta = deltas.get(label, 0) - (fee if label == actor else 0)
                if delta == 0 and label != actor:
                    fixture.require(after[label] == before[label], label + " changed unexpectedly")
                else:
                    fixture.require(balance(after[label]) == balance(before[label]) + delta,
                                    label + " balance moved by the wrong amount")

        def result_record(raw):
            fixture.require(len(raw) == RESULT_V2_BYTES and raw[RESULT_LEGACY_BYTES] == 2,
                            "replay result is not the context-bound v2 record")

        from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey
        from cryptography.hazmat.primitives.serialization import Encoding, PublicFormat
        for actor, seed in (("alice", "treasury"), ("bob", "bob")):
            public = Ed25519PrivateKey.from_private_bytes((directory / ("keys/" + seed + ".seed")).read_bytes()) \
                .public_key().public_bytes(Encoding.Raw, PublicFormat.Raw)
            account_name = b"agent:" + ("did:layerx:" + public.hex()).encode() + b":main"
            beneficiary = hashlib.sha256(b"LX:ACCOUNT:v1" + len(account_name).to_bytes(4, "big") + account_name).digest()
            amount = 1000000
            runtime.produce(actor + "-deposit", ["python3", fixture.ROOT / "platform/hosted/paxeer/evm.py", "send",
                "--rpc", runtime.rpc_url, "--chain", "125", "--key-file", directory / "keys/deployer.key",
                "--value", str(amount * 10**12), "0x0000000000000000000000000000000000001013", "deposit(bytes32)",
                "0x" + beneficiary.hex()])
            deposited = json.loads((directory / (actor + "-deposit.log")).read_text())
            fixture.require(int(deposited["status"], 16) == 1, "real fee deposit reverted")
            logs = [e for e in deposited["logs"] if e["address"].lower() == "0x0000000000000000000000000000000000001013"
                    and len(e["topics"]) == 4]
            fixture.require(len(logs) == 1, "exactly one native custody deposit")
            entry = logs[0]; data = bytes.fromhex(entry["data"].removeprefix("0x"))
            fixture.require(entry["topics"][2].removeprefix("0x").lower() == fixture.ASSET and data[:32] == beneficiary
                            and int.from_bytes(data[32:64], "big") == amount, "custody deposit identity/amount")
            runtime.wait(lambda: int(runtime.rpc("eth_blockNumber"), 16) >= int(deposited["blockNumber"], 16) + 2)
            credit = directory / (actor + ".credit")
            runtime.produce(actor + "-proof", [runtime.binary("layerx-custody-proof"), "light-credit",
                "--rpc", "http://127.0.0.1:" + str(runtime.ports[2]), "--profile", directory / "custody.profile",
                "--deposit-id", entry["topics"][1], "--owner-key", "0x" + public.hex(), "--output", credit])
            raw = credit.read_bytes()
            fixture.require(len(raw) > 363 and raw[:5] == b"LXDC3" and raw[43:75] == bytes.fromhex(entry["topics"][1][2:])
                            and raw[75:107].hex() == fixture.ASSET and raw[107:139] == beneficiary and raw[139:171] == public
                            and int.from_bytes(raw[191:207], "big") == amount, "native credit proof differs from real deposit")
            call("credit", actor, 0, credit=credit)
        funded = state()
        fixture.require(balance(funded["alice"]) > 0 and balance(funded["bob"]) > 0, "native fee funding")
        cases.append("runtime-real-native-fee-funding")

        def positive(operation, actor, hold, deltas, changed, amount=0, key=0, bps=0, ordinal=None):
            before = state()
            row, wire = call(operation, actor, 0, hold, amount, key, bps)
            fixture.require(int(row["fee"], 16) > 0, "escrow operation was not fee-charged")
            fixture.require(row["events"] == str(ordinal), "canonical event does not describe " + operation)
            if key:
                labels.append("result-" + str(key))
            after = state()
            transition(before, {k: after[k] for k in before}, row, actor, deltas, changed)
            same, _ = call(operation, actor, 0, hold, replay=wire)
            fixture.require(same["raw"] == row["raw"] and state() == after, "identical Activity replay changed state")
            return row, wire, after

        def opened(hold, amount):
            before = state()
            row, wire = call("open", "alice", 0, hold, amount)
            fixture.require(row["events"] == "1", "open event")
            labels.extend(["acct-" + hold, "hold-" + hold])
            after = state()
            transition(before, {k: after[k] for k in before}, row, "alice", {"alice": -amount})
            fixture.require(balance(after["acct-" + hold]) == amount and len(after["hold-" + hold]) > 0, "hold custody")
            return row, wire

        for hold, amount in (("a", 100), ("b", 50), ("c", 40)):
            opened(hold, amount)
        cases.append("runtime-holds-opened")
        k1 = positive("partial", "bob", "a", {"bob": 30, "acct-a": -30}, ("hold-a",), 30, 1, ordinal=3)
        k4 = positive("capture", "bob", "c", {"bob": 40, "acct-c": -40}, ("hold-c",), 40, 4, ordinal=2)
        k2 = positive("release", "alice", "b", {"alice": 50, "acct-b": -50}, ("hold-b",), 0, 2, ordinal=4)
        positive("dispute", "alice", "a", {}, ("hold-a",), ordinal=6)
        k3 = positive("resolve", "alice", "a", {"alice": 35, "bob": 35, "acct-a": -70}, ("hold-a",),
                      0, 3, 5000, ordinal=7)
        settled = k3[2]
        for key in (1, 2, 3, 4):
            result_record(settled["result-" + str(key)])
        cases.append("runtime-context-bound-result-record-v2")

        retries = (("partial", "bob", "a", 30, 1, 0, k1[0]), ("capture", "bob", "c", 40, 4, 0, k4[0]),
                   ("release", "alice", "b", 0, 2, 0, k2[0]), ("resolve", "alice", "a", 0, 3, 5000, k3[0]))
        refusals = (
            ("partial", "bob", "a", 31, 1, 0, "changed-amount"),
            ("partial", "bob", "b", 30, 1, 0, "cross-hold"),
            ("capture", "bob", "a", 30, 1, 0, "capture-as-full-capture"),
            ("release", "alice", "a", 0, 1, 0, "capture-as-release"),
            ("resolve", "alice", "a", 0, 1, 5000, "capture-as-resolution"),
            ("timeout", "bob", "a", 0, 1, 0, "capture-as-timeout"),
            ("partial", "alice", "a", 30, 1, 0, "changed-actor"),
            ("partial", "bob", "b", 30, 2, 0, "release-as-capture"),
            ("release", "bob", "b", 0, 2, 0, "release-changed-actor"),
            ("release", "alice", "c", 0, 2, 0, "release-cross-hold"),
            ("resolve", "alice", "a", 0, 3, 6000, "resolution-changed-recipient-split"),
            ("resolve", "bob", "a", 0, 3, 5000, "resolution-changed-authority"),
            ("partial", "bob", "c", 40, 4, 0, "full-capture-as-partial"),
            ("capture", "bob", "c", 39, 4, 0, "full-capture-changed-amount"),
        )

        def exercise(stage):
            outcomes, wires = {}, []
            for operation, actor, hold, amount, key, bps, original in retries:
                before = state()
                row, wire = call(operation, actor, 0, hold, amount, key, bps)
                wires.append((row, wire))
                fixture.require(row["events"] == original["events"] and row["event_bytes"] == original["event_bytes"]
                                and row["id"] != original["id"],
                                "exact retry did not return the original operation")
                transition(before, state(), row, actor)
                cases.append("runtime-%s-exact-retry-%s-%s" % (stage, operation, actor))
            for operation, actor, hold, amount, key, bps, name in refusals:
                before = state()
                row, _ = call(operation, actor, CONTEXT_MISMATCH, hold, amount, key, bps)
                fixture.require(row["events"] == "none", name + " emitted an escrow event")
                transition(before, state(), row, actor)
                outcomes[name] = (row["result"], row["events"])
                cases.append("runtime-%s-refused-%s" % (stage, name))
            return outcomes, wires

        live, recent = exercise("live")

        def durable(stage, kill, recent):
            current = state()
            runtime.catch_up(retained)
            runtime.restart(kill=kill)
            fixture.require(state() == current, stage + " restart changed balances, holds or result records")
            for key in (1, 2, 3, 4):
                result_record(current["result-" + str(key)])
            # The newest signed Activities are resubmitted byte for byte while still inside their time bound.
            for row, wire in recent:
                same, _ = call("replay", "alice", int(row["result"]), "a", replay=wire)
                fixture.require(same["raw"] == row["raw"], stage + " restart lost the original receipt")
            fixture.require(state() == current, stage + " identical replay after restart moved funds")
            for row in retained:
                fixture.require(runtime.invoke("receipt", row["id"], "receipt-" + row["id"]).stdout.decode().strip()
                                == row["raw"], "retained receipt bytes changed")
            cases.append("runtime-%s-restart-durable-context-and-receipts" % stage)
            outcomes, wires = exercise(stage)
            fixture.require(outcomes == live, stage + " restart did not reproduce identical refusals")
            cases.append("runtime-%s-restart-identical-refusals" % stage)
            return wires

        recent = durable("graceful", False, recent)
        durable("forced", True, recent)
        reports = runtime.catch_up(retained)
        fixture.require(len(reports) == len(retained), "replica missing canonical outcomes")
        cases.append("runtime-authenticated-independent-replica")
        fixture.write_json(directory / "escrow-runtime.json", {"cases": cases, "receipts": len(retained),
            "graceful_restart": True, "forced_restart": True, "same_disk": True, "real_chain_id": 125, "skipped": 0})
    finally:
        runtime.cleanup()


def launch_runtime(evidence):
    fixture = runtime_import()
    bound_runtime_artifacts()
    directory = Path(tempfile.mkdtemp(prefix="px-escrow-", dir="/var/tmp")); directory.rmdir()
    env = dict(os.environ)
    for name in ("net", "pid", "mnt"):
        env["PAXEER_X_PARENT_" + name.upper()] = os.readlink("/proc/self/ns/" + name)
    with (evidence / "runtime.log").open("wb") as log:
        result = subprocess.run(["unshare", "--mount", "--net", "--pid", "--fork", "--kill-child=KILL", "--mount-proc",
            "--propagation", "private", "python3", str(Path(__file__).resolve()), "--worker", str(directory)],
            env=env, stdout=log, stderr=log, timeout=1500)
    (evidence / "runtime-directory").write_text(str(directory) + "\n")
    fixture.require(result.returncode == 0, "real escrow replay runtime failed: " + str(directory))
    value = json.loads((directory / "escrow-runtime.json").read_text())
    fixture.require(value["skipped"] == 0 and value["graceful_restart"] and value["forced_restart"],
                    "incomplete durable escrow runtime")
    return value


def qualify():
    evidence = Path(os.environ.get("PAXEER_X_RUNTIME_EVIDENCE") or tempfile.mkdtemp(prefix="paxeer-x-escrow-replay-"))
    evidence.mkdir(parents=True, exist_ok=True, mode=0o700)
    os.chmod(evidence, 0o700)
    destination = evidence / "escrow-replay-binding.json"
    result = {"task": "3.3", "tests": 0, "skipped": 0, "cases": [], "command": COMMAND}
    code = 1
    try:
        result["revision"] = git("rev-parse", "HEAD").strip()
        dirty = git("status", "--porcelain", "--", *SOURCES).splitlines()
        if dirty:
            raise RuntimeError("candidate sources are not committed: " + "; ".join(dirty))
        bound_runtime_artifacts()
        result["cases"] = components(evidence)
        runtime_result = launch_runtime(evidence)
        result["runtime"] = runtime_result
        result["cases"].extend({"name": name, "status": "ok"} for name in runtime_result["cases"])
        names = [case["name"] for case in result["cases"]]
        if len(names) != len(set(names)):
            raise RuntimeError("duplicate case names")
        result["tests"] = len(result["cases"])
        code = 0
    except Exception as error:  # every failure is reported with its evidence path
        result["failure"] = str(error)
        result["tests"] = len(result["cases"])
        print(str(error), file=sys.stderr)
    result["exit_code"] = code
    destination.write_text(json.dumps(result, indent=2) + "\n")
    os.chmod(destination, 0o600)
    print(f"revision={result.get('revision', 'unknown')} command={' '.join(COMMAND)} exit_code={code}")
    print(f"Evidence: {destination}")
    print(f"PAXEER_X_GATE tests={result['tests']} skipped=0")
    return code


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--worker")
    arguments = parser.parse_args()
    os.umask(0o077)
    if arguments.worker:
        runtime_worker(Path(arguments.worker))
        return 0
    return qualify()


if __name__ == "__main__":
    sys.exit(main())
