#!/usr/bin/env python3
"""Owner policy and deployment evidence assembled into a relocatable kernel bundle.

Drives the production producer manifest, bundle assembly, bundle verification, kernel
consumption and the kernel restart function against externally supplied real owner, native
and naming evidence. Refusal cases (missing inputs, dummy owner data, unpaired or
out-of-bounds journals, tampered bytes, producer-local paths, network/chain mismatch,
changed authority or genesis) run on private copies of that evidence. Exits nonzero when
any prerequisite or acceptance is unmet; never synthesizes owner data.
"""
import hashlib
import importlib.util
import json
import os
import re
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path

ROOT = Path(__file__).resolve().parents[3]
HUMAN = ROOT / "platform/hosted/human"
PROVISION = HUMAN / "provision.py"
MATERIAL = HUMAN / "material.py"
INIT = ROOT / "docker/kernel/init.sh"
SCHEMA = "layerx.human.owner-bundle.v2"
PRODUCERS_SCHEMA = "layerx.human.owner-producers.v1"
PREFIX = "Human owner bundle refused: "
JOURNAL_NAME = re.compile(r"[0-9a-f]{64}\.(admission|deployment)")
EVIDENCE_FILES = ("components.json", "agent.json", "purpose-catalog.json", "authority.json",
                  "principal-policy.json", "recovery-policy.json", "movement-policy.json")
PRODUCER_INPUTS = {"owner-identity", "owner-result", "purpose-catalog", "authority", "principal-policy",
                   "recovery-policy", "movement-policy", "module-registry", "admission-journal",
                   "deployment-journal", "native-owner-registration", "naming-evidence"}
LAYOUT = ("components", "kms", "config", "agent-config", "movement-config", "authority-config",
          "authority", "identity")


class Case(Exception):
    pass


def check(condition, reason):
    if not condition:
        raise Case(reason)


def env():
    return dict(os.environ, PYTHONDONTWRITEBYTECODE="1")


def run(command, cwd=None):
    proc = subprocess.run([str(c) for c in command], cwd=cwd or ROOT, env=env(),
                          stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, timeout=300)
    print(f"   $ {' '.join(str(c) for c in command)} -> exit {proc.returncode}", flush=True)
    for stream in (proc.stdout, proc.stderr):
        for line in stream.strip().splitlines()[-6:]:
            print(f"     {line}", flush=True)
    return proc


def sha(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def tree(directory):
    directory = Path(directory)
    result = {}
    for path in sorted(directory.rglob("*")):
        info = path.lstat()
        key = str(path.relative_to(directory))
        if path.is_symlink():
            result[key] = ("link", os.readlink(path))
        elif path.is_dir():
            result[key] = ("dir", info.st_mode & 0o7777)
        else:
            result[key] = ("file", info.st_mode & 0o7777, info.st_mtime_ns, sha(path))
    return result


def private_copy(source, destination):
    """Byte-preserving private copy: directories 0700, regular files 0600, symlinks refused."""
    source, destination = Path(source), Path(destination)
    check(source.is_dir() and not source.is_symlink(), f"{source}: evidence directory required")
    destination.mkdir(mode=0o700)
    for path in sorted(source.rglob("*")):
        target = destination / path.relative_to(source)
        check(not path.is_symlink(), f"{path}: symlink in supplied evidence")
        if path.is_dir():
            target.mkdir(mode=0o700)
        else:
            fd = os.open(target, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
            with os.fdopen(fd, "wb") as output:
                output.write(path.read_bytes())
            check(sha(target) == sha(path), f"{target}: copy changed bytes")
    return destination


def rewrite(path, data):
    path = Path(path)
    path.unlink()
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, "wb") as output:
        output.write(data)


def refused(proc, needle=PREFIX):
    check(proc.returncode != 0, "accepted where a refusal was required")
    check(needle in proc.stderr, f"refusal does not state {needle!r}: {proc.stderr.strip()[-200:]}")


class Harness:
    def __init__(self, state, evidence, deployment, registry, genesis):
        self.state = state
        self.evidence = evidence
        self.deployment = deployment
        self.registry = registry
        self.genesis = genesis
        self.asset = (genesis / "asset-id").read_text().strip()
        check(re.fullmatch("[0-9a-f]{64}", self.asset), "real genesis asset-id required")
        self.deployment_value = json.loads(deployment.read_text())
        self.network = int(self.deployment_value["network_id"])
        self.chain = int(self.deployment_value["chain_id"])
        self.counter = 0

    def scratch(self, label):
        self.counter += 1
        path = self.state / f"{self.counter:02d}-{label}"
        path.mkdir(mode=0o700)
        return path

    def evidence_copy(self, label):
        return private_copy(self.evidence, self.scratch(label) / "human-evidence")

    def assemble(self, evidence, bundle, network=None, chain=None):
        bundle.mkdir(mode=0o700, exist_ok=True)
        return run([sys.executable, MATERIAL, "--assemble", evidence, self.deployment, self.registry,
                    bundle / "policy.json", self.network if network is None else network,
                    self.chain if chain is None else chain])

    def verify(self, bundle, network=None, chain=None):
        return run([sys.executable, MATERIAL, "--verify-bundle", bundle,
                    self.network if network is None else network, self.chain if chain is None else chain])

    def bundle(self, label):
        bundle = self.scratch(label) / "human-policy"
        proc = self.assemble(self.evidence, bundle)
        check(proc.returncode == 0, f"assembly of the supplied evidence refused: {proc.stderr.strip()[-200:]}")
        return bundle

    def kernel_root(self, label):
        work = self.scratch(label) / "material"
        work.mkdir(mode=0o700)
        (work / "human").mkdir(mode=0o700)
        for name in LAYOUT:
            (work / "human" / name).mkdir(mode=0o700)
        private_file_bytes(work / "receipt-authority-replica-id", (self.genesis / "replica-id").read_bytes())
        return work

    def consume(self, root, policy):
        return run([sys.executable, MATERIAL, root / "human", self.network, self.chain, policy,
                    "https://paxportwallet.com"])

    def producer_work(self, label):
        work = self.scratch(label) / "work"
        work.mkdir(mode=0o700)
        names = ["identity/source-binding.json", "human-owner-result.json", "naming-deployment-result.json"]
        names += ["human-evidence-input/" + name for name in (
            "owner-request.json", "recovery-policy.json", "owner-registration.json", "treasury.json",
            "sequencer.json", "movement-source.json", "account-head-result.json", "owner-native.json")]
        for optional in ("owner-kms.json", "onboarding-configuration.json"):
            if (self.evidence.parent / "human-evidence-input" / optional).exists():
                names.append("human-evidence-input/" + optional)
        names += ["human-evidence-input/owner-native-run/" + label + suffix
                  for label in ("credit", "identity", "rotation", "recovery") for suffix in (".activity", ".receipt")]
        from provision import protected_bytes
        for name in names:
            source = self.evidence.parent / name
            destination = work / name
            destination.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
            private_file_bytes(destination, protected_bytes(source))
        private_copy(self.evidence / "journal", work / "registry-journal")
        return work

    def produce(self, work):
        return run([sys.executable, PROVISION, "--assemble", "--work-dir", work,
                    "--registry", self.registry, "--asset", self.asset, "--journal", work / "registry-journal"])

    # ac_1
    def producer_manifest_complete(self):
        work = self.producer_work("producers")
        output = work.parent / "human-producers.json"
        proc = run([sys.executable, PROVISION, "--producer-manifest", "--work-dir", work,
                    "--registry", self.registry, "--journal", work / "registry-journal",
                    "--output", output])
        check(proc.returncode == 0, f"producer manifest blocked real evidence: {proc.stderr.strip()[-300:]}")
        check(output.stat().st_mode & 0o777 == 0o600, "producer manifest is not 0600")
        manifest = json.loads(output.read_text())
        check(manifest.get("schema") == PRODUCERS_SCHEMA, "producer manifest schema")
        names = [entry["input"] for entry in manifest["inputs"]]
        check(set(names) == PRODUCER_INPUTS and len(names) == len(PRODUCER_INPUTS),
              f"producer manifest inputs {sorted(names)}")
        for entry in manifest["inputs"]:
            check(entry["status"] == "present" and entry["reason"] is None
                  and re.fullmatch("[0-9a-f]{64}", entry["sha256"] or ""),
                  f"producer input {entry['input']} not present with a digest")
            if entry["path"].startswith("secrets:"):
                check(entry["sha256"] == sha(self.registry), "module registry digest differs")
            else:
                path = work / entry["path"]
                check(not Path(entry["path"]).is_absolute(), "producer-local manifest path")
                if entry["input"] in ("admission-journal", "deployment-journal"):
                    suffix = ".admission" if entry["input"] == "admission-journal" else ".deployment"
                    aggregate = hashlib.sha256()
                    for record in sorted(path.iterdir()):
                        if record.name.endswith(suffix):
                            aggregate.update(record.name.encode() + b"\n" + hashlib.sha256(record.read_bytes()).digest())
                    check(entry["sha256"] == aggregate.hexdigest(), "paired journal aggregate digest")
                else:
                    check(path.is_file() and entry["sha256"] == sha(path),
                          f"producer input {entry['input']} digest differs")
        check(self.produce(work).returncode == 0, "real producer evidence assembly refused")
        produced = work / "human-evidence"
        before = tree(produced)
        check(self.produce(work).returncode == 0 and tree(produced) == before,
              "repeat producer assembly changed original identity or custody evidence")
        self.evidence = produced
        return work

    def producer_manifest_blocks_missing(self):
        work = self.producer_work("producers-missing")
        (work / "identity/source-binding.json").unlink()
        output = work.parent / "human-producers.json"
        proc = run([sys.executable, PROVISION, "--producer-manifest", "--work-dir", work,
                    "--registry", self.registry, "--journal", work / "registry-journal",
                    "--output", output])
        check(proc.returncode == 3, f"removed owner identity exit {proc.returncode}, expected 3")
        check(re.search(r"^blocked: owner-identity: ", proc.stderr, re.M), "blocked owner-identity line missing")
        manifest = json.loads(output.read_text())
        entry = [e for e in manifest["inputs"] if e["input"] == "owner-identity"]
        check(len(entry) == 1 and entry[0]["status"] == "blocked" and entry[0]["sha256"] is None
              and entry[0]["reason"], "blocked owner identity not recorded in the manifest")

    # ac_2
    def relocated_bundle_verifies_and_is_consumed(self):
        bundle = self.bundle("assemble")
        manifest = json.loads((bundle / "bundle-manifest.json").read_text())
        policy = json.loads((bundle / "policy.json").read_text())
        check(manifest["schema"] == SCHEMA, "bundle schema")
        check(policy["journal_directory"] == "journal", "policy journal_directory is not the relative 'journal'")
        check(manifest["network_id"] == self.network and manifest["chain_id"] == self.chain, "bundle binding")
        check(manifest["policy_sha256"] == sha(bundle / "policy.json"), "policy digest")
        check(manifest["authority_sha256"] == hashlib.sha256(json.dumps(
            policy["authority"], sort_keys=True, separators=(",", ":")).encode()).hexdigest(), "authority digest")
        source = sorted(p.name for p in (self.evidence / "journal").iterdir())
        copied = sorted(p.name for p in (bundle / "journal").iterdir())
        check(source == copied and 1 <= len(copied) <= 128, f"journal records {len(copied)} differ from evidence")
        for name in copied:
            check(sha(bundle / "journal" / name) == sha(self.evidence / "journal" / name), f"journal {name} bytes changed")
            check((bundle / "journal" / name).stat().st_mode & 0o777 == 0o600, f"journal {name} mode")
        check([e["name"] for e in manifest["journal"]] == copied, "manifest journal list")
        inputs = {e["name"]: e["sha256"] for e in manifest["inputs"]}
        for name in EVIDENCE_FILES:
            check(inputs.get(name) == sha(self.evidence / name), f"manifest input {name} digest")
        check(inputs.get("deployment.json") == sha(self.deployment), "deployment digest")
        check(inputs.get("module-registry.json") == sha(self.registry), "module registry digest")
        relocated = self.scratch("relocated") / "kernel-policy"
        proc = run([sys.executable, MATERIAL, "--relocate-bundle", bundle, relocated, self.network, self.chain])
        check(proc.returncode == 0, "production byte-preserving bundle relocation refused")
        check(tree(relocated).keys() == tree(bundle).keys(), "relocated tree differs")
        shutil.rmtree(bundle.parent)
        proc = self.verify(relocated)
        check(proc.returncode == 0, f"relocated bundle refused: {proc.stderr.strip()[-200:]}")
        lines = proc.stdout.strip().splitlines()
        check(len(lines) == 1, "verify-bundle must print exactly one line")
        binding = json.loads(lines[0])
        check(binding == {"network_id": self.network, "chain_id": self.chain,
                          "authority_sha256": manifest["authority_sha256"],
                          "policy_sha256": manifest["policy_sha256"], "records": len(copied),
                          "bundle_sha256": sha(relocated / "bundle-manifest.json")},
              f"verify-bundle binding {binding}")
        check(lines[0] == json.dumps(binding, sort_keys=True), "verify-bundle line is not canonical")
        root = self.kernel_root("consume")
        proc = self.consume(root, relocated / "policy.json")
        check(proc.returncode == 0, f"kernel consumer refused relocated bundle: {proc.stderr.strip()[-200:]}")
        kernel = root / "human/journal"
        check(sorted(p.name for p in kernel.iterdir()) == copied, "kernel journal records differ")
        for name in copied:
            check(sha(kernel / name) == sha(relocated / "journal" / name), f"kernel journal {name} bytes changed")
        config = root / "human/config"
        check((config / "LAYERX_HUMAN_NETWORK_ID").read_text() == str(self.network), "kernel network binding")
        check((config / "LAYERX_HUMAN_PAXEER_CHAIN_ID").read_text() == str(self.chain), "kernel chain binding")
        for key, value in policy["authority"].items():
            check((root / "human/authority-config" / key).read_text() == str(value), f"kernel authority {key}")
        registry = json.loads((root / "human/kms/registry.json").read_text()) \
            if (root / "human/kms/registry.json").exists() else None
        if "onboarding_configuration" not in policy:
            check(registry == policy["registry"], "kernel module registry binding")
        for section in ("inputs", "onboarding"):
            for item in manifest[section]:
                path = relocated / section / item["name"]
                check(sha(path) == item["sha256"] and path.stat().st_size == item["size"],
                      "relocated original producer bytes differ")
        if "onboarding_configuration" in policy:
            check(policy["onboarding_configuration"]["directory"] == "onboarding", "onboarding not relocated")
            for name in ("LAYERX_HUMAN_TENANCY_DIGEST", "LAYERX_HUMAN_AUTH_INDEX_KEY", "LAYERX_HUMAN_STREAM_CURSOR_KEY"):
                check((config / name).read_bytes() == (relocated / "onboarding" / name).read_bytes(),
                      "original onboarding key replaced")
            for source, target in (("registry.json", "kms/registry.json"), ("recovery-policy.json", "identity/recovery-policy.json")):
                check((root / "human" / target).read_bytes() == (relocated / "onboarding" / source).read_bytes(),
                      "original KMS registry or recovery policy replaced")
        return relocated

    def projected_bundle_and_consumers(self):
        bundle = self.bundle("projection-source")
        base = self.scratch("projection")
        flat = base / "flat"
        flat.mkdir(mode=0o700)
        proc = subprocess.run([sys.executable, str(MATERIAL), "--secret-arguments", str(bundle),
                               str(self.network), str(self.chain)], env=env(), cwd=ROOT,
                              stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=300)
        check(proc.returncode == 0, "actual Secret argument producer refused")
        for argument in proc.stdout.split(b"\0"):
            if not argument:
                continue
            prefix, key, source = argument.decode().split("=", 2)
            check(prefix == "--from-file" and re.fullmatch('[A-Za-z0-9_.-]+', key), "Secret argument encoding")
            source = Path(source)
            check(bundle in source.parents, "Secret producer source outside bundle")
            private_file_bytes(flat / key, source.read_bytes())
        imported = base / "imported"
        command = [sys.executable, MATERIAL, "--import-secret", flat, imported, self.network, self.chain]
        check(run(command).returncode == 0, "production projected bundle import refused")
        before = tree(imported)
        check(run(command).returncode == 0 and tree(imported) == before, "identical projection replaced owner bytes")
        check({key: value[-1] for key, value in tree(bundle).items() if value[0] == "file"}
              == {key: value[-1] for key, value in tree(imported).items() if value[0] == "file"},
              "projected bundle changed source bytes")
        root = self.kernel_root("projection-consumer")
        check(self.consume(root, imported / "policy.json").returncode == 0, "projected bundle consumer refused")
        projections = base / "consumers"
        projections.mkdir(mode=0o700)
        for folder, source in (("components", "components"), ("kms", "kms"), ("identity", "identity"),
                               ("authority", "authority"), ("components-config", "config"),
                               ("agent-config", "agent-config"), ("authority-config", "authority-config"),
                               ("movement-config", "movement-config"), ("journal", "journal")):
            private_copy(root / "human" / source, projections / folder)
        (projections / "module-registry").mkdir(mode=0o700)
        private_file_bytes(projections / "module-registry/registry.json", (imported / "inputs/module-registry.json").read_bytes())
        command = [sys.executable, MATERIAL, "--verify-projected-material", imported, projections,
                   self.network, self.chain]
        check(run(command).returncode == 0, "actual generated consumer material differs from verified bundle")
        field = projections / "components-config/LAYERX_HUMAN_AGENT_AUTHORITY"
        raw = bytearray(field.read_bytes())
        raw[0] ^= 1
        rewrite(field, raw)
        refused(run(command))
        private_file_bytes(flat / "undeclared", b"negative projection key")
        refused(run([sys.executable, MATERIAL, "--import-secret", flat, base / "refused", self.network, self.chain]))
        check(not (base / "refused").exists(), "unlisted projected key published a bundle")

    def absolute_journal_refused(self):
        bundle = self.bundle("absolute")
        policy = json.loads((bundle / "policy.json").read_text())
        policy["journal_directory"] = str(bundle / "journal")
        rewrite(bundle / "policy.json", json.dumps(policy).encode())
        manifest = json.loads((bundle / "bundle-manifest.json").read_text())
        manifest["policy_sha256"] = sha(bundle / "policy.json")
        rewrite(bundle / "bundle-manifest.json", json.dumps(manifest).encode())
        refused(self.consume(self.kernel_root("absolute-consume"), bundle / "policy.json"))
        refused(self.verify(bundle))

    def tampered_journal_refused(self):
        bundle = self.bundle("tamper")
        record = sorted((bundle / "journal").iterdir())[0]
        data = bytearray(record.read_bytes())
        data[0] ^= 1
        rewrite(record, bytes(data))
        refused(self.verify(bundle))
        refused(self.consume(self.kernel_root("tamper-consume"), bundle / "policy.json"))

    def producer_bytes_and_signatures_refused(self):
        for name in ("producer-records/source-binding.json", "producer-records/rotation.activity",
                     "producer-records/recovery.receipt", "module-registry.json"):
            bundle = self.bundle("producer-tamper")
            path = bundle / "inputs" / name
            data = bytearray(path.read_bytes())
            data[-1] ^= 1
            rewrite(path, data)
            refused(self.verify(bundle))
            manifest = json.loads((bundle / "bundle-manifest.json").read_text())
            for item in manifest["inputs"]:
                if item["name"] == name:
                    item["sha256"] = sha(path)
            rewrite(bundle / "bundle-manifest.json", json.dumps(manifest).encode())
            refused(self.verify(bundle))
        bundle = self.bundle("undeclared")
        private_file_bytes(bundle / "unlisted", b"unlisted negative input")
        refused(self.verify(bundle))
        bundle = self.bundle("symlink")
        path = bundle / "inputs/authority.json"
        data = path.read_bytes()
        path.unlink()
        target = bundle.parent / "authority.json"
        private_file_bytes(target, data)
        path.symlink_to(target)
        refused(self.verify(bundle))
        bundle = self.bundle("interrupted")
        (bundle / "bundle-manifest.json").unlink()
        before = tree(bundle)
        refused(self.assemble(self.evidence, bundle), "reconciliation required")
        check(tree(bundle) == before, "interrupted bundle was silently replaced")

    def journal_bounds_refused(self):
        empty = self.evidence_copy("journal-empty")
        for record in (empty / "journal").iterdir():
            record.unlink()
        refused(self.assemble(empty, empty.parent / "human-policy"))
        check(not (empty.parent / "human-policy/policy.json").exists(), "empty journal produced a policy")
        large = self.evidence_copy("journal-large")
        records = sorted((large / "journal").iterdir())
        pair = [p for p in records if p.suffix == ".admission"][0]
        admission = pair.read_bytes()
        deployment = (large / "journal" / (pair.stem + ".deployment")).read_bytes()
        index = 0
        while len(list((large / "journal").iterdir())) < 129:
            stem = hashlib.sha256(b"bound-%d" % index).hexdigest()
            index += 1
            if (large / "journal" / (stem + ".admission")).exists():
                continue
            private_file_bytes(large / "journal" / (stem + ".admission"), admission)
            private_file_bytes(large / "journal" / (stem + ".deployment"), deployment)
        check(len(list((large / "journal").iterdir())) > 128, "out-of-bounds journal not built")
        refused(self.assemble(large, large.parent / "human-policy"))
        check(not (large.parent / "human-policy/policy.json").exists(), "oversized journal produced a policy")

    # ac_3
    def unpaired_record_refused(self):
        copy = self.evidence_copy("unpaired")
        deployment = sorted((copy / "journal").glob("*.deployment"))[0]
        deployment.unlink()
        refused(self.assemble(copy, copy.parent / "human-policy"))
        check(not (copy.parent / "human-policy/policy.json").exists(), "unpaired journal produced a policy")

    def dummy_owner_refused(self):
        for name, value in (("authority.json", b"{}"), ("principal-policy.json", b"{}"),
                            ("components.json", b"{}")):
            copy = self.evidence_copy("dummy-" + name.split(".")[0])
            rewrite(copy / name, value)
            refused(self.assemble(copy, copy.parent / "human-policy"))
            check(not (copy.parent / "human-policy/policy.json").exists(), f"dummy {name} produced a policy")
        copy = self.evidence_copy("unprotected")
        (copy / "authority.json").chmod(0o644)
        refused(self.assemble(copy, copy.parent / "human-policy"))

    def binding_mismatch_refused(self):
        for label, network, chain in (("network", self.network + 1, None), ("chain", None, self.chain + 1)):
            bundle = self.scratch("mismatch-" + label) / "human-policy"
            refused(self.assemble(self.evidence, bundle, network, chain))
            check(not (bundle / "policy.json").exists(), f"{label} mismatch produced a policy")
        bundle = self.bundle("verify-mismatch")
        refused(self.verify(bundle, network=self.network + 1))
        refused(self.verify(bundle, chain=self.chain + 1))

    # ac_4
    def repeated_assembly_and_changed_authority(self):
        bundle = self.bundle("repeat")
        before = tree(bundle)
        proc = self.assemble(self.evidence, bundle)
        check(proc.returncode == 0, f"identical reassembly refused: {proc.stderr.strip()[-200:]}")
        check(tree(bundle) == before, "identical reassembly rewrote the bundle")
        changed = self.evidence_copy("changed-authority")
        authority = json.loads((changed / "authority.json").read_text())
        authority["core-clock-horizon"] = int(authority["core-clock-horizon"]) + 1
        rewrite(changed / "authority.json", json.dumps(authority).encode())
        refused(self.assemble(changed, bundle), "reconciliation required")
        check(tree(bundle) == before, "changed authority overwrote protected bindings")
        return changed

    def kernel_restart(self, changed):
        check(os.geteuid() == 0, "kernel material function installs root-owned files; run as root")
        text = INIT.read_text()
        match = re.search(r"^human_policy_bundle_install\(\) \{\n.*?^human_material_generate\(\) \{\n.*?^\}\n", text, re.M | re.S)
        check(match is not None, "docker/kernel/init.sh human_material_generate not found")
        function = match.group(0).replace("/usr/local/lib/layerx-human/material.py", str(MATERIAL))
        base = self.scratch("kernel")
        keys, genesis, human_state = base / "keys", base / "genesis", base / "state"
        for path in (keys, genesis, human_state):
            path.mkdir(mode=0o700)
        bundle = self.bundle("kernel-bundle")
        shutil.copytree(bundle, keys / "human-policy", copy_function=shutil.copy2)
        for name in ("metadata.lxgb", "asset-id", "replica-id"):
            private_file_bytes(genesis / name, (self.genesis / name).read_bytes())
        script = (function + "\nset -euo pipefail\numask 077\nhuman_material_generate\n")
        variables = dict(env(), keys=str(keys), genesis=str(genesis), human_state=str(human_state),
                         human_policy=str(keys / "human-policy/policy.json"),
                         LAYERX_NODE_NETWORK_ID=str(self.network), LAYERX_NODE_PAXEER_CHAIN_ID=str(self.chain))
        prelude = "".join(f"{k}={shlex_quote(variables[k])}\n" for k in
                          ("keys", "genesis", "human_state", "human_policy",
                           "LAYERX_NODE_NETWORK_ID", "LAYERX_NODE_PAXEER_CHAIN_ID"))

        def generate():
            proc = subprocess.run(["bash", "-c", prelude + script], env=variables, cwd=base,
                                  stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, timeout=300)
            print(f"   $ bash human_material_generate -> exit {proc.returncode}", flush=True)
            for line in (proc.stdout + proc.stderr).strip().splitlines()[-6:]:
                print(f"     {line}", flush=True)
            return proc

        proc = generate()
        material = human_state / "material"
        check(proc.returncode == 0 and material.is_dir(), f"first kernel generation failed: {proc.stderr.strip()[-200:]}")
        check((material / "bundle-binding").is_file(), "retained bundle-binding missing")
        binding = json.loads((material / "bundle-binding").read_text())
        check(binding["records"] == len(list((bundle / "journal").iterdir())), "retained binding record count")
        retained = tree(material)
        proc = generate()
        check(proc.returncode == 0, f"restart with identical bundle and genesis failed: {proc.stderr.strip()[-200:]}")
        check(tree(material) == retained, "restart changed retained onboarding/custody state")
        for name in ("metadata.lxgb", "asset-id", "replica-id"):
            original = (genesis / name).read_bytes()
            changed_bytes = bytearray(original)
            check(bool(changed_bytes), "real genesis file empty")
            changed_bytes[0] ^= 1
            rewrite(genesis / name, changed_bytes)
            proc = generate()
            check(proc.returncode != 0 and "human owner bundle: reconciliation required" in proc.stderr,
                  "changed genesis did not require reconciliation")
            check(tree(material) == retained, "changed genesis overwrote retained state")
            rewrite(genesis / name, original)
        target = material / "human/config/LAYERX_HUMAN_TENANCY_DIGEST"
        original = target.read_bytes()
        corrupt = bytearray(original)
        corrupt[0] ^= 1
        rewrite(target, corrupt)
        proc = generate()
        check(proc.returncode != 0, "retained onboarding tamper accepted")
        rewrite(target, original)
        retained = tree(material)
        shutil.rmtree(keys / "human-policy")
        other = self.scratch("kernel-changed") / "human-policy"
        proc = self.assemble(changed, other)
        check(proc.returncode == 0, f"changed-authority bundle refused in a fresh directory: {proc.stderr.strip()[-200:]}")
        shutil.copytree(other, keys / "human-policy", copy_function=shutil.copy2)
        proc = generate()
        check(proc.returncode != 0 and "human owner bundle: reconciliation required" in proc.stderr,
              "changed authority did not require reconciliation at restart")
        check(tree(material) == retained, "changed authority overwrote retained state")


def private_file_bytes(path, data):
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, "wb") as output:
        output.write(data)


def shlex_quote(value):
    return "'" + value.replace("'", "'\"'\"'") + "'"


def main():
    if sys.version_info < (3, 10):
        print("prerequisite missing: python3>=3.10", flush=True)
        return 2
    if shutil.which("bash") is None:
        print("prerequisite missing: bash", flush=True)
        return 2
    for path in (PROVISION, MATERIAL, INIT):
        if not path.is_file():
            print(f"prerequisite missing: {path.relative_to(ROOT)}", flush=True)
            return 2
    sys.dont_write_bytecode = True
    spec = importlib.util.spec_from_file_location("material", MATERIAL)
    material = importlib.util.module_from_spec(spec)
    sys.path.insert(0, str(HUMAN))
    spec.loader.exec_module(material)
    if getattr(material, "BUNDLE_SCHEMA", None) != SCHEMA or not callable(getattr(material, "verify_bundle", None)):
        print("prerequisite missing: material.py BUNDLE_SCHEMA/verify_bundle bundle contract", flush=True)
        return 2
    inputs = {}
    for name in ("PAXEER_X_HUMAN_EVIDENCE_DIR", "PAXEER_X_HUMAN_DEPLOYMENT", "PAXEER_X_MODULE_REGISTRY", "PAXEER_X_HUMAN_GENESIS_DIR"):
        value = os.environ.get(name, "")
        path = Path(value) if value else None
        if path is None or not path.is_absolute() or not path.exists():
            print(f"blocked: external owner evidence {name}", flush=True)
            return 3
        if path.resolve() != path:
            print(f"blocked: external owner evidence {name} must be a canonical path", flush=True)
            return 3
        inputs[name] = path
    evidence = inputs["PAXEER_X_HUMAN_EVIDENCE_DIR"]
    if not evidence.is_dir() or not (evidence / "journal").is_dir():
        print("blocked: external owner evidence PAXEER_X_HUMAN_EVIDENCE_DIR", flush=True)
        return 3

    genesis = inputs["PAXEER_X_HUMAN_GENESIS_DIR"]
    try:
        material.genesis_binding(genesis)
    except (OSError, ValueError, KeyError):
        print("blocked: protected real genesis metadata.lxgb, asset-id and replica-id required", flush=True)
        return 3
    state = Path(tempfile.mkdtemp(prefix="human-owner-evidence-")).resolve()
    if state == ROOT or ROOT in state.parents:
        print("FAIL prerequisite: private state directory resolves inside the source tree", flush=True)
        return 1
    print(f"== private state {state}", flush=True)
    harness = Harness(state, evidence, inputs["PAXEER_X_HUMAN_DEPLOYMENT"], inputs["PAXEER_X_MODULE_REGISTRY"], inputs["PAXEER_X_HUMAN_GENESIS_DIR"])
    changed = []
    cases = [
        ("ac1 producer manifest names every real input", harness.producer_manifest_complete),
        ("ac1 producer manifest blocks a missing owner identity", harness.producer_manifest_blocks_missing),
        ("ac2 relocated bundle verifies and is consumed by the kernel", harness.relocated_bundle_verifies_and_is_consumed),
        ("ac2 actual Secret projection imports unchanged and binds generated consumers", harness.projected_bundle_and_consumers),
        ("ac2 producer-local absolute journal path refused", harness.absolute_journal_refused),
        ("ac2 tampered journal byte refused", harness.tampered_journal_refused),
        ("ac2 producer bytes, signatures, unlisted files, symlinks and interrupted publication refused",
         harness.producer_bytes_and_signatures_refused),
        ("ac2 zero and more than 128 journal records refused", harness.journal_bounds_refused),
        ("ac3 unpaired journal record refused", harness.unpaired_record_refused),
        ("ac3 dummy owner data refused", harness.dummy_owner_refused),
        ("ac3 network and chain binding mismatch refused", harness.binding_mismatch_refused),
        ("ac4 repeated assembly unchanged, changed authority requires reconciliation",
         lambda: changed.append(harness.repeated_assembly_and_changed_authority())),
        ("ac4 kernel restart preserves retained state, changed genesis or authority requires reconciliation",
         lambda: harness.kernel_restart(changed[0] if changed else harness.repeated_assembly_and_changed_authority())),
    ]
    tests = 0
    failures = []
    for name, case in cases:
        print(f"== {name}", flush=True)
        tests += 1
        try:
            case()
            print(f"PASS {name}", flush=True)
        except Case as error:
            failures.append(f"{name}: {error}")
        except (OSError, ValueError, KeyError, TypeError, IndexError, subprocess.TimeoutExpired) as error:
            failures.append(f"{name}: {type(error).__name__}: {error}")
    for failure in failures:
        print(f"FAIL {failure}", flush=True)
    print(f"evidence {state}", flush=True)
    print(f"PAXEER_X_GATE tests={tests} skipped=0", flush=True)
    return 1 if failures else 0


if __name__ == "__main__":
    os.umask(0o077)
    sys.exit(main())
