#!/usr/bin/env python3
"""Qualify deployment validation through native dispatch and owned real daemons."""
import argparse
import hashlib
import json
import importlib.util
import os
from pathlib import Path
import re
import shutil
import signal
import stat
import struct
import subprocess
import sys
import tempfile
import time

ROOT = Path(__file__).resolve().parents[3]
COMMAND = ["timeout", "30m", "python3",
           "tools/qualification/paxeer-x/programs_all_form_validation.py"]
SCHEMA = "paxeer-x.programs-all-form-artifacts.v1"
PROCESS_REQUIREMENTS = (
    "layerxd-canonical-and-legacy-deploy",
    "layerxd-validator-refusals-with-artifact-and-lifecycle-rollback",
    "layerxd-admitted-program-call",
    "layerxd-restart-and-identical-replay-admission",
    "provisioned-authority-verified-deployment-and-call-receipts",
)


def require(condition, message):
    if not condition:
        raise RuntimeError(message)


def digest(path):
    with Path(path).open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def private_directory(path, create=False):
    path = Path(path).absolute()
    require(not path.is_symlink(), "private directory must not be a symlink")
    if create:
        path.mkdir(mode=0o700, parents=True, exist_ok=True)
    path = path.resolve(strict=True)
    info = path.stat()
    require(path != ROOT and ROOT not in path.parents,
            "qualification evidence must remain outside the repository")
    require(stat.S_ISDIR(info.st_mode) and info.st_uid == os.geteuid()
            and not info.st_mode & 0o077,
            "qualification directory must be private and owned by the caller")
    return path


def write_private(path, value):
    descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(descriptor, "w") as stream:
        json.dump(value, stream, indent=2, sort_keys=True)
        stream.write("\n")
        stream.flush()
        os.fsync(stream.fileno())


def load_private(path):
    private_directory(path.parent)
    descriptor = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
    with os.fdopen(descriptor) as stream:
        info = os.fstat(stream.fileno())
        require(stat.S_ISREG(info.st_mode) and info.st_nlink == 1
                and info.st_uid == os.geteuid() and not info.st_mode & 0o077,
                "artifact manifest must be a private caller-owned regular file")
        return json.load(stream)


def capture(command):
    return subprocess.check_output(command, cwd=ROOT, text=True, timeout=30).strip()


def identity():
    revision = capture(["git", "rev-parse", "HEAD^{commit}"])
    require(re.fullmatch(r"[0-9a-f]{40}", revision), "invalid candidate revision")
    require(not capture(["git", "status", "--porcelain=v1", "--untracked-files=normal"]),
            "candidate source must be clean before building or qualifying")
    return {"revision": revision, "tree": capture(["git", "rev-parse", "HEAD^{tree}"])}


def artifact(path, executable=False):
    path = Path(path).absolute()
    info = path.lstat()
    require(stat.S_ISREG(info.st_mode) and not path.is_symlink() and info.st_size > 0,
            "missing or empty regular candidate artifact")
    require(not executable or os.access(path, os.X_OK), "candidate artifact is not executable")
    return {"path": str(path), "sha256": digest(path), "bytes": info.st_size}


def logged(command, log, environment, deadline, cwd=ROOT):
    remaining = deadline - time.monotonic()
    require(remaining > 0, "qualification deadline expired")
    descriptor = os.open(log, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(descriptor, "wb") as stream:
        stream.write(("COMMAND " + json.dumps(command) + "\n").encode())
        stream.flush()
        process = subprocess.Popen(command, cwd=cwd, env=environment,
                                   stdin=subprocess.DEVNULL, stdout=stream,
                                   stderr=subprocess.STDOUT, start_new_session=True)
        try:
            status = process.wait(timeout=remaining)
        finally:
            if process.poll() is None:
                os.killpg(process.pid, signal.SIGTERM)
                try:
                    process.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    os.killpg(process.pid, signal.SIGKILL)
                    process.wait(timeout=5)
        stream.write(f"\nEXIT {status}\n".encode())
        stream.flush()
        os.fsync(stream.fileno())
    return status


def build(output):
    directory = private_directory(output, create=True)
    require(not any(directory.iterdir()), "build output must be a fresh empty private directory")
    source = identity()
    native_dir = directory / "native"
    cargo_dir = directory / "cargo"
    library = native_dir / "liblayerx.a"
    header = native_dir / "generated/lxp_checkpoint_settlement.h"
    sandbox = cargo_dir / "debug/liblayerx_programs_sandbox.a"
    binary = directory / "programs_lifecycle"
    jobs = int(os.environ.get("PAXEER_X_PROGRAM_VALIDATION_BUILD_JOBS", "4"))
    require(1 <= jobs <= 16, "build jobs must be between 1 and 16")
    environment = dict(os.environ, CARGO_TARGET_DIR=str(cargo_dir),
                       CARGO_BUILD_JOBS=str(jobs), CARGO_PROFILE_DEV_DEBUG="0",
                       CARGO_INCREMENTAL="0", PYTHONDONTWRITEBYTECODE="1")
    commands = [
        ["make", "--no-print-directory", "-j" + str(jobs),
         "BUILD_DIR=" + str(native_dir), "LXP_REVISION=" + source["revision"],
         str(library), str(header)],
        ["cargo", "build", "--locked", "--manifest-path", str(ROOT / "programs/Cargo.toml"),
         "-p", "layerx-programs-sandbox", "--features", "host-ffi"],
        ["cc", "-Iinclude", "-I" + str(header.parent), "-std=c17", "-pedantic", "-Werror",
         "-Wall", "-Wextra", "-Wconversion", "-Wshadow", "-Wvla", "-fno-strict-aliasing",
         "-ffp-contract=off", "-O2", "tests/programs/test_lifecycle.c",
         "-Wl,--start-group", str(library), str(sandbox), "-Wl,--end-group",
         "-lcrypto", "-lsqlite3", "-pthread", "-ldl", "-lm", "-o", str(binary)],
    ]
    driver = directory / "runtime_client"
    commands.append(commands[-1][:])
    commands[-1][commands[-1].index("tests/programs/test_lifecycle.c")] = "tests/daemon/lxp_test_runtime_fixture.c"
    commands[-1][-1] = str(driver)
    commands.append(["cargo", "test", "--locked", "--manifest-path", str(ROOT / "programs/Cargo.toml"),
                     "-p", "layerx-programs-runtime", "--lib", "--no-run", "--message-format=json",
                     "deployment_admission_"])
    record = {"schema": SCHEMA, "source": source, "commands": commands,
              "features": {"sandbox": ["host-ffi"]}, "logs": [], "exit_code": None}
    write_private(directory / "build-inputs.json", record)
    deadline = time.monotonic() + 1740
    try:
        for index, command in enumerate(commands):
            log = directory / f"build-{index + 1}.log"
            cwd = ROOT / "programs" if index in (1, 4) else ROOT
            status = logged(command, log, environment, deadline, cwd=cwd)
            record["logs"].append({"command": command, "cwd": str(cwd),
                                   "exit_code": status, "log": artifact(log)})
            require(status == 0, "focused artifact build failed; inspect private build log")
        require(identity() == source, "candidate source changed during artifact production")
        executables = []
        build_finished = False
        for line in (directory / "build-5.log").read_text().splitlines():
            if not line.startswith("{"):
                continue
            event = json.loads(line)
            if event.get("reason") == "build-finished":
                build_finished = event.get("success") is True
            if (event.get("reason") == "compiler-artifact"
                    and event.get("target", {}).get("name") == "layerx_programs_runtime"
                    and event.get("profile", {}).get("test") is True and event.get("executable")):
                require(event.get("features") == [], "unexpected runtime test features")
                executables.append(event["executable"])
        require(build_finished and len(executables) == 1, "missing unique compiled runtime test artifact")
        runtime_tests = directory / "runtime_tests"
        shutil.copyfile(executables[0], runtime_tests)
        runtime_tests.chmod(0o700)
        record["artifacts"] = {"native": artifact(binary, executable=True),
                               "runtime-tests": artifact(runtime_tests, executable=True),
                               "runtime-client": artifact(driver, executable=True),
                               "native-library": artifact(library),
                               "sandbox-staticlib": artifact(sandbox),
                               "generated-header": artifact(header)}
        record["exit_code"] = 0
        write_private(directory / "manifest.json", record)
        print("PROGRAM_VALIDATION_NATIVE_MANIFEST " + str(directory / "manifest.json"))
    except (OSError, ValueError, RuntimeError, subprocess.SubprocessError):
        record["exit_code"] = 1
        write_private(directory / "build-failure.json", record)
        raise


def required_native_cases():
    required = {}
    for phase in ("initial", "snapshot_replay"):
        for form in ("legacy", "framed"):
            for abi in range(1, 5):
                for name in ("malformed_body", "float_type", "ambient_import", "valid"):
                    required[(name, form, abi, phase)] = 0 if name == "valid" else -3
            required[("unsupported_abi", form, 5, phase)] = -101
            for name in ("module_bytes", "function_count", "local_stack", "operand_stack", "call_depth"):
                required[(name, form, 1, phase)] = -3
        required[("invalid_capability", "framed", 1, phase)] = -3
    return required


def case_identity(case):
    return case["name"], case["form"], case["abi"], case["phase"]


def native_cases(output):
    cases = [json.loads(line[len("ALL_FORM_CASE "):]) for line in output.splitlines()
             if line.startswith("ALL_FORM_CASE ")]
    summaries = re.findall(r"^ALL_FORM_VALIDATION cases=(\d+) skipped=0$", output, re.M)
    required = required_native_cases()
    require(summaries == [str(len(required))] and len(cases) == len(required),
            "native case accounting is absent, inconsistent or skipped")
    identities = [case_identity(case) for case in cases]
    require(len(identities) == len(set(identities)) and set(identities) == set(required),
            "native matrix is incomplete or duplicated")
    for case in cases:
        expected = required[case_identity(case)]
        require(type(case["result"]) is int and type(case["expected"]) is int
                and case["result"] == case["expected"] == expected,
                "native result differs from canonical expected result")
        if expected != 0:
            require(case["lifecycle_present"] is False and case["new_artifacts"] == 0
                    and case["staged"] == 0 and case["effects"] == 0
                    and case["state_unchanged"] is True,
                    "refused deployment retained new executable artifact or lifecycle state")
        else:
            require(case["artifact_present"] is True and case["lifecycle_present"] is True,
                    "valid deployment did not persist its executable artifact and recorded ABI")
    return cases


def payload_name(case):
    return f"{case['phase']}.{case['name']}.{case['form']}.abi{case['abi']}.bin"


def call_payload(program, abi):
    entrypoint = b"layerx_call"
    capabilities = b"\0\0"
    access = b"LayerX/programs/access-declaration/v1\0\0"
    budgets = (1000000, 16777216, 1048576, 1048576, 64, 1048576, 4096)
    header = program + struct.pack(">HHIHII7Q", abi, len(entrypoint), 0,
                                   len(capabilities), len(access), 16, *budgets)
    require(len(header) == 106, "native call fixed header differs from source contract")
    return header + entrypoint + capabilities + access


def write_bytes(path, value):
    descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(descriptor, "wb") as stream:
        stream.write(value)
        stream.flush()
        os.fsync(stream.fileno())


def process_worker(directory, manifest_path):
    directory = private_directory(directory)
    record = {"cases": [], "commands": [], "exit_code": 1, "unrun_process_cases": list(PROCESS_REQUIREMENTS)}
    runtime = None
    sys.dont_write_bytecode = True
    try:
        require(os.geteuid() == 0 and os.getegid() == 4020,
                "real LNI qualification requires isolated UID0/GID4020")
        for name in ("net", "pid", "mnt"):
            require(os.readlink("/proc/self/ns/" + name) != os.environ["PAXEER_X_VALIDATION_PARENT_" + name.upper()],
                    "owned runtime namespace isolation is missing")
        saved = load_private(Path(manifest_path))
        require(saved["source"] == identity(), "process artifacts differ from clean candidate")
        sys.path.insert(0, str(ROOT / "tests/daemon"))
        import paxeer_x_runtime_fixture as fixture
        bundle = fixture.artifacts(os.environ["PAXEER_X_RUNTIME_ARTIFACTS"])
        client = saved["artifacts"]["runtime-client"]
        require(artifact(client["path"], executable=True) == client, "runtime client artifact changed")
        fee = "100000000"
        require(fee is not None and re.fullmatch(r"[0-9]+", fee)
                and int(fee) <= (1 << 64) - 1, "declared program fee allowance is invalid")
        def command(argv):
            record["commands"].append([str(value) for value in argv])
            subprocess.run(argv, check=True, stdin=subprocess.DEVNULL, timeout=30)
        command(["ip", "link", "set", "lo", "up"])
        source = Path(tempfile.mkdtemp(prefix="program-validation-source-", dir="/var/tmp"))
        source.chmod(0o755)
        command(["mount", "--bind", str(ROOT), str(source)])
        command(["mount", "-o", "remount,bind,ro,nosuid,nodev", str(source)])
        fixture.ROOT = source
        python_root = Path(tempfile.mkdtemp(prefix="program-validation-python-", dir="/var/tmp"))
        python_root.chmod(0o755)
        command(["mount", "--bind", sys.prefix, str(python_root)])
        command(["mount", "-o", "remount,bind,ro,nosuid,nodev", str(python_root)])
        os.environ["PATH"] = str(python_root / "bin") + ":/usr/local/bin:/usr/bin:/bin"
        class RecordedRuntime(fixture.RuntimeFixture):
            def produce(self, label, argv, env=None, timeout=120):
                record["commands"].append([str(value) for value in argv])
                return super().produce(label, argv, env=env, timeout=timeout)
            def launch(self, name, argv, env=None):
                record["commands"].append([str(value) for value in argv])
                return super().launch(name, argv, env=env)
        runtime_directory = Path(tempfile.mkdtemp(prefix="px-program-runtime-", dir="/var/tmp"))
        runtime_directory.rmdir()
        record["runtime_directory"] = str(runtime_directory)
        runtime = RecordedRuntime(runtime_directory, bundle, client)
        runtime.generate()
        record["authority"] = {"source": "owned production chain genesis and node bootstrap",
                               "fixture": artifact(runtime_directory / "fixture.json")}
        cases = native_cases((directory / "native.log").read_text())
        sequence = 1
        deadline = time.monotonic() + 900
        originals = []
        admitted_programs = []
        def invoke(operation, payload, expected, label, account_sequence, module=9, profile=None):
            evidence = directory / label
            evidence.mkdir(mode=0o700)
            argv = [client["path"], str(runtime_directory / "run/layerxd.lni.sock"),
                    str(profile or runtime_directory / "salt"), operation, str(account_sequence),
                    "wait", str(payload), ("encoding:" if operation == "program-envelope" else "receipt:") + str(expected), str(evidence)]
            environment = runtime.env | {"PAXEER_X_FIXTURE_KEYS": str(runtime_directory / "keys"),
                                         "PAXEER_X_PROGRAM_FEE_LIMIT": fee}
            record["commands"].append(argv)
            log = evidence / "client.log"
            status = logged(argv, log, environment, min(deadline, time.monotonic() + 45))
            require(status == 0, "real program client refused; inspect private client.log")
            lines = [json.loads(line) for line in log.read_text().splitlines() if line.startswith("{")]
            require(len(lines) == 1, "real program client emitted no unique result")
            outcome = lines[0]
            if operation == "program-envelope":
                require(outcome["stage"] == "encoding" and outcome["result"] == -105
                        and outcome["payload_path"] == "payload.bin"
                        and outcome["payload_digest"] == digest(payload),
                        "oversized payload did not produce canonical encoder refusal")
                outcome.update(label=label, command=argv, log=artifact(log),
                               payload=artifact(evidence / "payload.bin"))
                record["cases"].append(outcome)
                return outcome
            require(outcome["stage"] == "receipt" and outcome["result"] == expected
                    and outcome["class"] == 0 and outcome["module_id"] == module
                    and (outcome["module_version"] == 4 if module == 9 else outcome["module_version"] > 0) and outcome["global_sequence"] > 0,
                    "verified receipt does not bind expected Programs result")
            require(re.fullmatch(r"[0-9a-f]{64}", outcome["activity_id"]), "invalid verified activity identity")
            require(outcome["activity_path"] == "activity.bin" and outcome["receipt_path"] == "receipt.bin",
                    "client evidence paths differ from production driver contract")
            for field in ("batch_id", "receipt_digest"):
                require(re.fullmatch(r"[0-9a-f]{64}", outcome[field]), "invalid verified receipt authority binding")
            outcome.update(label=label, command=argv, log=artifact(log),
                           activity=artifact(evidence / "activity.bin"),
                           receipt=artifact(evidence / "receipt.bin"), account_sequence=account_sequence)
            record["cases"].append(outcome)
            return outcome
        treasury = json.loads((runtime_directory / "node/treasury.json").read_text())
        owner_key = treasury["public_key"]
        require(re.fullmatch(r"[0-9a-f]{64}", owner_key)
                and treasury["network_id"] == fixture.NETWORK and treasury["asset"] == fixture.ASSET
                and treasury["did"] == "did:layerx:" + owner_key,
                "provisioned treasury public binding differs from the owned chain")
        account = treasury["account"].encode()
        require(account == ("agent:did:layerx:" + owner_key + ":main").encode(),
                "treasury account differs from funded actor")
        beneficiary = hashlib.sha256(b"LX:ACCOUNT:v1" + len(account).to_bytes(4, "big") + account).hexdigest()
        amount = 1000000000000
        custody = "0x0000000000000000000000000000000000001013"
        deposit_command = [sys.executable, str(source / "platform/hosted/paxeer/evm.py"),
                           "send", "--rpc", runtime.rpc_url, "--chain", "125", "--key-file",
                           str(runtime_directory / "keys/deployer.key"), "--value", str(amount * 10**12),
                           custody, "deposit(bytes32)", "0x" + beneficiary]
        record["commands"].append(deposit_command)
        deposit_log = directory / "custody-deposit.log"
        require(logged(deposit_command, deposit_log, runtime.env, min(deadline, time.monotonic() + 150)) == 0,
                "real custody deposit failed")
        deposits = [json.loads(line) for line in deposit_log.read_text().splitlines() if line.startswith("{")]
        require(len(deposits) == 1 and deposits[0]["status"] == "0x1", "custody deposit has no successful receipt")
        evm_spec = importlib.util.spec_from_file_location("program_validation_evm", source / "platform/hosted/paxeer/evm.py")
        evm = importlib.util.module_from_spec(evm_spec)
        evm_spec.loader.exec_module(evm)
        topic = "0x" + evm.keccak(b"CustodyDeposit(bytes32,bytes32,address,bytes32,uint256,uint64)").hex()
        events = [event for event in deposits[0]["logs"]
                  if event["address"].lower() == custody and event["topics"] and event["topics"][0].lower() == topic]
        require(len(events) == 1 and len(events[0]["topics"]) == 4,
                "custody deposit emitted no unique canonical event")
        event = events[0]
        data = evm.unhex(event["data"], 96)
        require(event["topics"][2].lower() == "0x" + fixture.ASSET
                and data[:32].hex() == beneficiary and int.from_bytes(data[32:64], "big") == amount,
                "custody funding receipt asset, beneficiary or amount mismatch")
        deposit_height = int(deposits[0]["blockNumber"], 16)
        runtime.wait(lambda: int(runtime.rpc("status", comet=True)["sync_info"]["latest_block_height"]) >= deposit_height + 3)
        credit = directory / "funding.credit"
        credit_command = [runtime.binary("layerx-custody-proof"), "light-credit", "--rpc",
                          "http://127.0.0.1:" + str(runtime.ports[2]), "--profile",
                          str(runtime_directory / "custody.profile"), "--deposit-id", event["topics"][1],
                          "--owner-key", owner_key, "--output", str(credit)]
        record["commands"].append(credit_command)
        credit_log = directory / "custody-credit.log"
        require(logged(credit_command, credit_log, runtime.env, min(deadline, time.monotonic() + 120)) == 0,
                "production custody light-proof producer refused funding")
        credit.chmod(0o600)
        Path(str(credit) + ".nullifier").chmod(0o600)
        funded = invoke("funding-credit", credit, 0, "funding", 0, module=8,
                        profile=runtime_directory / "custody.profile")
        funding_proofs = runtime.catch_up([{"id": funded["activity_id"], "batch": funded["batch_id"],
                                          "digest": funded["receipt_digest"],
                                          "raw": Path(funded["receipt"]["path"]).read_bytes().hex()}])
        require(len(funding_proofs) == 1, "funding receipt inclusion is unverifiable")
        record["funding"] = {"deposit_log": artifact(deposit_log), "credit_log": artifact(credit_log),
                             "credit": artifact(credit), "amount": amount, "asset": fixture.ASSET}
        for phase in ("initial", "snapshot_replay"):
            if phase == "snapshot_replay":
                proofs = runtime.catch_up([
                    {"id": item["activity_id"], "batch": item["batch_id"],
                     "digest": item["receipt_digest"],
                     "raw": Path(item["receipt"]["path"]).read_bytes().hex()}
                    for item in originals])
                require(len(proofs) == len(originals), "missing authenticated replica inclusion proofs")
                record["initial_receipt_inclusion_proofs"] = len(proofs)
                prior = runtime.restart()
                record["restart"] = {"old_pids": prior,
                                     "new_pids": {name: process.pid for name, process in runtime.processes.items()}}
                for index, original in enumerate(originals):
                    recovered = invoke("program-replay", Path(original["activity"]["path"]),
                                       original["result"], f"recovered-{index}", original["account_sequence"])
                    require(recovered["activity_id"] == original["activity_id"]
                            and recovered["global_sequence"] == original["global_sequence"]
                            and recovered["receipt"]["sha256"] == original["receipt"]["sha256"],
                            "restart did not preserve identical verified receipt bytes")
                require(len(admitted_programs) == 8, "initial accepted program inventory is incomplete")
                for index, (program, abi) in enumerate(admitted_programs):
                    call = directory / f"restored-call-{index}.bin"
                    write_bytes(call, call_payload(program, abi))
                    invoke("program-call", call, 0, f"restored-call-{index}", sequence)
                    sequence += 1
            for case in (item for item in cases if item["phase"] == phase):
                native_input = directory / "state" / payload_name(case)
                encoded = bytearray(native_input.read_bytes())
                require(len(encoded) >= 104 and encoded[32:34] == struct.pack(">H", case["abi"]),
                        "native producer payload ABI mismatch")
                encoded[34] = 0
                encoded[36:68] = bytes(32)
                payload = directory / ("process-" + payload_name(case))
                write_bytes(payload, encoded)
                if case["name"] == "module_bytes":
                    invoke("program-envelope", payload, -105, "envelope-" + payload.stem, sequence)
                    continue
                label = "deploy-" + payload.stem
                admitted = invoke("program-deploy", payload, case["expected"], label, sequence)
                sequence += 1
                if phase == "initial":
                    originals.append(admitted)
                if case["name"] == "valid":
                    if phase == "initial":
                        admitted_programs.append((bytes(encoded[:32]), case["abi"]))
                    call = directory / ("call-" + payload_name(case))
                    write_bytes(call, call_payload(bytes(encoded[:32]), case["abi"]))
                    called = invoke("program-call", call, 0, "call-" + payload.stem, sequence)
                    sequence += 1
                    if phase == "initial":
                        originals.append(called)
        final_receipts = [item for item in record["cases"]
                          if ("snapshot_replay" in item["label"] or item["label"].startswith("restored-call-"))
                          and item["stage"] == "receipt"]
        proofs = runtime.catch_up([
            {"id": item["activity_id"], "batch": item["batch_id"],
             "digest": item["receipt_digest"],
             "raw": Path(item["receipt"]["path"]).read_bytes().hex()}
            for item in final_receipts])
        require(len(proofs) == 59, "missing post-restart authenticated receipt inclusion proofs")
        record["post_restart_receipt_inclusion_proofs"] = len(proofs)
        expected_process_cases = 2 * (45 + 8) + (45 - 2 + 8) + 1 + 8
        require(len(record["cases"]) == expected_process_cases,
                "real deployment/call/recovery matrix is incomplete")
        record["module_bytes_boundary"] = "Both forms run native deterministic validation and the actual activity encoder refusal at its 524288-byte payload bound; oversized modules cannot reach daemon Deploy."
        record["unrun_process_cases"] = []
        record["exit_code"] = 0
    except (OSError, ValueError, KeyError, TypeError, RuntimeError, subprocess.SubprocessError, ImportError) as error:
        record["failure"] = str(error)
    finally:
        if runtime is not None:
            try:
                runtime.cleanup()
            except (OSError, RuntimeError, subprocess.SubprocessError) as error:
                record["cleanup_failure"] = str(error)
                record["exit_code"] = 1
        write_private(directory / "process-result.json", record)
    return record["exit_code"]


def qualify_process(directory, manifest):
    environment = dict(os.environ)
    for name in ("net", "pid", "mnt"):
        environment["PAXEER_X_VALIDATION_PARENT_" + name.upper()] = os.readlink("/proc/self/ns/" + name)
    command = ["setpriv", "--regid=4020", "--clear-groups", "unshare", "--mount", "--net", "--pid",
               "--fork", "--kill-child=KILL", "--mount-proc", "--propagation", "private",
               sys.executable, str(Path(__file__).resolve()), "--process-worker", str(directory),
               "--manifest", str(manifest)]
    log = directory / "process.log"
    status = logged(command, log, environment, time.monotonic() + 1500)
    path = directory / "process-result.json"
    require(path.is_file(), "real process worker produced no result; inspect private process.log")
    record = load_private(path)
    require(status == 0 and record["exit_code"] == 0 and not record["unrun_process_cases"],
            "real process qualification failed; inspect private process-result.json")
    return record, {"command": command, "exit_code": status, "log": artifact(log)}


def qualify(manifest):
    base = os.environ.get("PAXEER_X_EVIDENCE_DIR")
    if base:
        base = private_directory(base)
    directory = Path(tempfile.mkdtemp(prefix="programs-all-form-", dir=base or "/var/tmp"))
    private_directory(directory)
    result = {"task": "6.5", "command": COMMAND, "cases": [], "tests": 0,
              "skipped": 0, "exit_code": 1, "status": "incomplete",
              "qualification_complete": False, "boundary": "registered native kernel dispatch",
              "unrun_process_cases": list(PROCESS_REQUIREMENTS),
              "logs": [], "evidence_directory": str(directory)}
    try:
        result["candidate_revision"] = capture(["git", "rev-parse", "HEAD^{commit}"])
        result["source"] = identity()
        require(manifest, "PAXEER_X_PROGRAM_VALIDATION_MANIFEST is required; produce candidate artifacts explicitly")
        path = Path(manifest).absolute()
        saved = load_private(path)
        require(saved.get("schema") == SCHEMA and saved.get("source") == result["source"],
                "artifact manifest does not bind the exact clean candidate")
        require(saved.get("exit_code") == 0 and saved.get("features") == {"sandbox": ["host-ffi"]},
                "successful source-bound sandbox artifact production is required")
        require(set(saved["artifacts"]) == {"native", "runtime-client", "runtime-tests", "native-library", "sandbox-staticlib", "generated-header"},
                "candidate artifact inventory is incomplete")
        require(len(saved["logs"]) == 5 and len(saved["commands"]) == 5,
                "candidate build command evidence is incomplete")
        for index, row in enumerate(saved["logs"]):
            require(row["exit_code"] == 0 and row["command"] == saved["commands"][index]
                    and artifact(row["log"]["path"]) == row["log"],
                    "candidate build log evidence does not match the producer")
        for name, row in saved["artifacts"].items():
            require(artifact(row["path"], executable=name in ("native", "runtime-client", "runtime-tests")) == row,
                    "candidate artifact changed since production")
        result["manifest"] = artifact(path)
        staged = directory / "programs_lifecycle"
        shutil.copyfile(saved["artifacts"]["native"]["path"], staged)
        staged.chmod(0o700)
        require(digest(staged) == saved["artifacts"]["native"]["sha256"], "staged candidate differs")
        state = directory / "state"
        state.mkdir(mode=0o700)
        command = [str(staged), "--all-form-validation"]
        environment = {key: value for key, value in os.environ.items()
                       if not key.startswith(("LAYERX_", "PAXEER_X_"))}
        environment.update(TMPDIR=str(state), PYTHONDONTWRITEBYTECODE="1",
                           PAXEER_X_PROGRAM_VALIDATION_INPUTS=str(state))
        log = directory / "native.log"
        status = logged(command, log, environment, time.monotonic() + 180, cwd=state)
        result["logs"].append({"command": command, "exit_code": status, "log": artifact(log)})
        require(status == 0, "native admission cases failed; inspect private native.log")
        result["cases"] = native_cases(log.read_text())
        result["tests"] = len(result["cases"])
        require(identity() == result["source"], "candidate source changed during native qualification")
        require(digest(staged) == saved["artifacts"]["native"]["sha256"], "candidate executable changed during run")
        result["native_checks_passed"] = True
        expected_files = {payload_name(case) for case in result["cases"]}
        require({file.name for file in state.glob("*.bin")} == expected_files,
                "native payload producer omitted or duplicated a case")
        result["native_inputs"] = [artifact(state / name) for name in sorted(expected_files)]
        runtime_command = [saved["artifacts"]["runtime-tests"]["path"], "deployment_admission_", "--test-threads=1"]
        runtime_log = directory / "runtime-tests.log"
        runtime_status = logged(runtime_command, runtime_log, environment, time.monotonic() + 90)
        result["logs"].append({"command": runtime_command, "exit_code": runtime_status, "log": artifact(runtime_log)})
        runtime_output = runtime_log.read_text()
        runtime_passed = re.findall(r"^test ([A-Za-z0-9_:]+) \.\.\. ok$", runtime_output, re.M)
        expected_runtime = {"engine::deployment_admission_tests::deployment_admission_calls_recorded_abis",
                            "engine::deployment_admission_tests::deployment_admission_bounds_and_recursive_runtime_guard"}
        require(runtime_status == 0 and len(runtime_passed) == 2 and set(runtime_passed) == expected_runtime
                and re.findall(r"test result: ok\. (\d+) passed; (\d+) failed; (\d+) ignored;", runtime_output) == [("2", "0", "0")],
                "focused runtime execution or recursion guard cases are missing, failed or skipped")
        result["runtime_cases"] = runtime_passed
        result["tests"] += len(runtime_passed)
        process, process_log = qualify_process(directory, path)
        result["process"] = process
        result["logs"].append(process_log)
        result["tests"] += len(process["cases"])
        result["unrun_process_cases"] = []
        require(identity() == result["source"], "source changed during process qualification")
        for name, row in saved["artifacts"].items():
            require(artifact(row["path"], executable=name in ("native", "runtime-client", "runtime-tests")) == row,
                    "candidate artifact changed during qualification")
        result["qualification_complete"] = True
        result["status"] = "passed"
        result["exit_code"] = 0
    except (OSError, ValueError, KeyError, TypeError, RuntimeError, subprocess.SubprocessError) as error:
        result["failure"] = str(error)
    write_private(directory / "result.json", result)
    print(f"PAXEER_X_GATE tests={result['tests']} skipped=0 complete={str(result['qualification_complete']).lower()}")
    print("Evidence: " + str(directory / "result.json"))
    return result["exit_code"]


def main():
    os.umask(0o077)
    def interrupted(_signum, _frame):
        raise RuntimeError("qualification interrupted before completion")
    signal.signal(signal.SIGTERM, interrupted)
    signal.signal(signal.SIGINT, interrupted)
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--process-worker", help=argparse.SUPPRESS)
    parser.add_argument("--manifest", default=os.environ.get("PAXEER_X_PROGRAM_VALIDATION_MANIFEST"))
    parser.add_argument("--build-output", help="explicitly build the focused native candidate into a new private directory")
    arguments = parser.parse_args()
    try:
        if arguments.process_worker:
            return process_worker(arguments.process_worker, arguments.manifest)
        if arguments.build_output:
            build(arguments.build_output)
            return 0
        return qualify(arguments.manifest)
    except (OSError, ValueError, RuntimeError, subprocess.SubprocessError):
        print("program validation refused before evidence initialization or during explicit build", file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
