#!/usr/bin/env python3
import base64
import hashlib
import importlib.util
import os
from pathlib import Path
import re
import stat
import subprocess
import sys

sys.dont_write_bytecode = True
ROOT = Path(__file__).resolve().parents[3]
spec = importlib.util.spec_from_file_location(
    "real_ramp_source_settlement", ROOT / "tools/qualification/paxeer-x/ramp-source-settlement.py")
ramp = importlib.util.module_from_spec(spec)
spec.loader.exec_module(ramp)


def fixed_hex(value, size):
    ramp.require(isinstance(value, str) and len(value) == size * 2
                 and re.fullmatch("[0-9a-f]+", value) is not None
                 and bytes.fromhex(value) != bytes(size), "canonical nonzero address required")
    return value


class MigrationContract(ramp.Contract):
    def __init__(self, document):
        ramp.closed(document, ("version", "execution_domain", "ramp_fixture_file",
                               "accepted_mappings", "refused_mappings", "pending_mapping"),
                    "connected migration fixture")
        ramp.require(document["version"] == "layerx-migration-connected-fixture-v2"
                     and document["execution_domain"] == "isolated-real-process",
                     "isolated genuine connected fixture required")
        super().__init__(ramp.document(document["ramp_fixture_file"]))
        self.migration = document
        self.mapping_ran = False
        self.profile = ramp.document(self.interop_env["LAYERX_INTEROP_MIGRATION_V2_CONFIG"])
        self.mapping_journal = self.profile["mapping_journal"]
        self.mapping_path = Path(self.mapping_journal["directory"]) / self.mapping_journal["namespace"]
        ramp.require(self.mapping_path.is_absolute()
                     and self.mapping_path.resolve() == self.mapping_path,
                     "canonical mapping journal required")
        for chain in ("ethereum", "solana"):
            source = self.profile[chain]["journal"]
            ramp.require(Path(source["directory"]) / source["namespace"] != self.mapping_path,
                         "mapping and source journal ownership must be separate")
        if self.mapping_path.exists():
            ramp.require(not list(self.mapping_path.iterdir()), "fresh isolated mapping namespace required")
        pending = document["pending_mapping"]
        ramp.closed(pending, ("chain", "evidence_file", "authorization_file"), "pending mapping")
        self.auth["unbound_mapping"] = ramp.credential(pending["authorization_file"])
        ramp.require(self.auth["unbound_mapping"] not in
                     (self.auth["customer"], self.auth["other_customer"]),
                     "distinct genuinely unbound wallet authority required")

    def snapshot(self):
        info = self.mapping_path.stat()
        ramp.require(stat.S_ISDIR(info.st_mode) and info.st_uid == os.geteuid()
                     and not info.st_mode & 0o077 and self.mapping_path.resolve() == self.mapping_path,
                     "protected actual mapping journal required")
        paths = list(self.mapping_path.iterdir())
        ramp.require(len(paths) <= 4096, "isolated mapping journal bound refused")
        return {path.name: ramp.private(path, 256 * 1024) for path in paths}

    def mapping_request(self, case):
        ramp.require(case["chain"] in ("ethereum", "solana"), "declared source chain required")
        proof = ramp.private(case["evidence_file"], 1024 * 1024)
        return {"chain": case["chain"], "source_evidence": base64.b64encode(proof).decode("ascii")}, proof

    def mapping_post(self, request, authority="customer", key=None):
        return self.http("POST", "/v2/migration/accounts", request, authority,
                         base=self.interop_base, idempotency=key)

    def mapping_refusal(self, name, request, authority="customer"):
        before = self.snapshot()
        status, response = self.mapping_post(request, authority, "mapping-refused-" + name)
        ramp.require(status in (400, 401, 403, 409, 422) and response.get("ok") is False
                     and isinstance(response.get("error"), dict)
                     and isinstance(response["error"].get("code"), str)
                     and bool(response["error"]["code"]), "actual mapping refusal required")
        ramp.require(self.snapshot() == before, "refused mapping changed durable association")
        self.passed("mapping-" + name)

    def assert_mapping(self, result, case, proof):
        ramp.closed(result, ("version", "principal", "source_network", "source_address", "layerx_identity",
                             "evm_address", "paxeer_block_number", "paxeer_block_hash", "evidence_digest",
                             "provenance", "layerx_receipt"), "mapping result")
        source = self.profile[case["chain"]]
        network = ("ethereum:" + str(source["chain_id"]) if case["chain"] == "ethereum"
                   else "solana:" + bytes(source["genesis_hash"]).hex())
        ramp.require(result["version"] == "account-mapping-v2"
                     and result["source_network"] == network
                     and result["source_address"] == case["expected_source_address"]
                     and result["layerx_identity"] == list(bytes.fromhex(self.fixture["expected_did"]))
                     and result["evm_address"] == list(bytes.fromhex(case["expected_evm_address"]))
                     and result["evidence_digest"] == list(hashlib.sha256(proof).digest())
                     and result["provenance"] == "paxeer-binding" and result["layerx_receipt"] is False
                     and isinstance(result["principal"], str) and result["principal"]
                     and type(result["paxeer_block_number"]) is int and result["paxeer_block_number"] > 0,
                     "actual finalized protocol mapping mismatch")
        ramp.byte32(result["paxeer_block_hash"])

    def mapping_cases(self):
        cases = self.migration["accepted_mappings"]
        ramp.require(isinstance(cases, list) and len(cases) == 2
                     and {case["chain"] for case in cases} == {"ethereum", "solana"},
                     "genuine Ethereum and Solana ownership captures required")
        accepted = []
        for case in cases:
            ramp.closed(case, ("chain", "evidence_file", "expected_source_address", "expected_evm_address"),
                        "accepted mapping")
            fixed_hex(case["expected_source_address"], 20 if case["chain"] == "ethereum" else 32)
            fixed_hex(case["expected_evm_address"], 20)
            request, proof = self.mapping_request(case)
            self.mapping_refusal(case["chain"] + "-tenant-isolation", request, "other_customer")
            for field, value in (("principal", "caller-invented-owner"), ("layerx_identity", [1] * 32),
                                 ("layerx_receipt", True), ("receipt_digest", [1] * 32)):
                self.mapping_refusal(case["chain"] + "-forged-" + field, dict(request, **{field: value}))
            status, response = self.mapping_post(request, key="mapping-accepted-" + case["chain"])
            ramp.require(status == 200 and response.get("ok") is True,
                         "actual authenticated finalized mapping refused")
            self.assert_mapping(response["result"], case, proof)
            before = self.snapshot()
            ramp.require(before and any(name.endswith(".record") for name in before),
                         "genuine mapping did not create durable evidence")
            for key in ("mapping-accepted-" + case["chain"], "mapping-reverified-" + case["chain"]):
                status, repeated = self.mapping_post(request, key=key)
                ramp.require(status == 200 and repeated.get("ok") is True
                             and repeated["result"] == response["result"] and self.snapshot() == before,
                             "real mapping replay changed durable association")
            operation = response.get("operation")
            ramp.require(isinstance(operation, str) and operation and "/" not in operation,
                         "real mapping operation required")
            status, isolated = self.http("GET", "/v1/operations/" + operation,
                                         authority="other_customer", base=self.interop_base)
            ramp.require(status in (403, 404) and isolated.get("ok") is False,
                         "other tenant read mapping operation")
            self.passed(case["chain"] + "-authenticated-finalized-mapping-and-replay")
            accepted.append((case, request, proof, response["result"]))
        refused = self.migration["refused_mappings"]
        ramp.require(isinstance(refused, list) and len(refused) == 2
                     and {case["name"] for case in refused} == {"ownership-signature", "source-network"},
                     "genuine invalid signature and wrong-network captures required")
        for case in refused:
            ramp.closed(case, ("name", "chain", "evidence_file"), "refused mapping")
            request, _ = self.mapping_request(case)
            self.mapping_refusal(case["name"], request)
        request, _ = self.mapping_request(self.migration["pending_mapping"])
        before = self.snapshot()
        status, pending = self.mapping_post(request, "unbound_mapping", "mapping-unbound")
        ramp.require(status in (202, 503) and pending.get("ok") is False
                     and pending.get("error", {}).get("code") in ("source_pending", "rpc_unavailable")
                     and self.snapshot() == before,
                     "genuine unbound protocol wallet claimed a confirmed mapping")
        self.passed("unbound-paxeer-wallet-retained-without-mapping")
        self.interop_child.terminate()
        self.interop_child.wait(timeout=10)
        self.interop_child = None
        self.interop_log.close()
        self.interop_log = None
        super().start_interop()
        ramp.require(self.snapshot() == before, "mapping journal changed on real process restart")
        for case, request, proof, expected in accepted:
            status, response = self.mapping_post(request, key="mapping-recovered-" + case["chain"])
            ramp.require(status == 200 and response.get("ok") is True and response["result"] == expected
                         and self.snapshot() == before, "durable mapping lost or duplicated on recovery")
            self.assert_mapping(response["result"], case, proof)
            self.passed(case["chain"] + "-mapping-process-restart-recovery")

    def start_interop(self):
        super().start_interop()
        if not self.mapping_ran:
            self.mapping_ran = True
            self.mapping_cases()


def main():
    contract = None
    try:
        path = os.environ.get("LAYERX_MIGRATION_V2_FIXTURE")
        ramp.require(path, "genuine connected migration fixture absent")
        contract = MigrationContract(ramp.document(path))
        contract.run()
        print("MIGRATION_V2_CONNECTED_COMPLETE ethereum=1 solana=1 funded=2 skipped=0")
        print(f"PAXEER_X_GATE tests={contract.count} skipped=0")
        return 0
    except (ramp.Refusal, OSError, ValueError, KeyError, TypeError, subprocess.SubprocessError):
        print("migration-v2: genuine connected qualification refused", file=sys.stderr)
        print(f"PAXEER_X_GATE tests={contract.count if contract else 0} skipped=0")
        return 1
    finally:
        if contract is not None:
            contract.close()


if __name__ == "__main__":
    sys.exit(main())
