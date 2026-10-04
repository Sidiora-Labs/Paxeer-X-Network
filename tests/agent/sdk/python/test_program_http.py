from __future__ import annotations

import importlib.util
import json
import os
import stat
import time
import unittest
from dataclasses import replace
from hashlib import sha256
from pathlib import Path

from layerx_sdk.agent_http import AgentHttpTransport, LayerXKeyCredential
from layerx_sdk.native_program_call import decode_native_program_call, encode_native_program_call
from layerx_sdk.production import IdempotencyKey, PlatformSdkError, ProductionClient, SecretBytes
from layerx_sdk.program_wire import decode_signed_program_call, verify_native_program_call_signature
from layerx_sdk.programs import NativeProgramRequest, ProgramOperations, ProgramTrustContext, _discovery

ROOT = Path(__file__).resolve().parents[4]


def protected(path: str | Path, maximum: int = 1_048_576) -> bytes:
    path = Path(path)
    info = path.lstat()
    parent = path.parent.lstat()
    if (not path.is_absolute() or path.resolve() != path or not stat.S_ISREG(info.st_mode)
            or info.st_uid != os.geteuid() or info.st_nlink != 1 or stat.S_IMODE(info.st_mode) != 0o600
            or parent.st_uid != os.geteuid() or stat.S_IMODE(parent.st_mode) != 0o700
            or not 0 < info.st_size <= maximum):
        raise ValueError("protected real Programs fixture input required")
    return path.read_bytes()


def unique(items: list[tuple[str, object]]) -> dict[str, object]:
    result = {}
    for key, value in items:
        if key in result:
            raise ValueError("duplicate Programs fixture field")
        result[key] = value
    return result


class ProgramHttp(unittest.TestCase):
    @classmethod
    def setUpClass(cls) -> None:
        path = os.environ.get("LAYERX_PYTHON_PROGRAM_HTTP_FIXTURE")
        if not path:
            raise RuntimeError("actual protected gateway/native cluster fixture is required; no skipped HTTP evidence")
        cls.fixture = json.loads(protected(path), object_pairs_hook=unique)
        required = {"version", "isolated", "approved_program_calls", "endpoint", "network_id",
                    "sequencer_public_key", "gateway_key_id", "gateway_key_file", "gateway_pid",
                    "gateway_source_manifest", "programs", "refused_call", "unknown_call"}
        if (set(cls.fixture) != required or cls.fixture["version"] != 1 or cls.fixture["isolated"] is not True
                or cls.fixture["approved_program_calls"] is not True
                or type(cls.fixture["network_id"]) is not int or not 0 < cls.fixture["network_id"] < 1 << 32
                or type(cls.fixture["gateway_pid"]) is not int or cls.fixture["gateway_pid"] <= 0):
            raise ValueError("exact approved disposable real HTTP fixture contract required")
        manifest = json.loads(protected(cls.fixture["gateway_source_manifest"]), object_pairs_hook=unique)
        executable = Path(os.readlink(f'/proc/{cls.fixture["gateway_pid"]}/exe'))
        if (executable.name != "layerx-gateway" or not executable.is_file()
                or manifest["binary_sha256"] != sha256(executable.read_bytes()).hexdigest()):
            raise ValueError("actual prebuilt production gateway process required")
        for relative in ("platform/hosted/gateway/src/main.rs", "platform/hosted/gateway/src/native_call.rs"):
            if manifest["sources"][relative] != sha256((ROOT / relative).read_bytes()).hexdigest():
                raise ValueError("gateway process source differs from actual candidate")
        spec = importlib.util.spec_from_file_location("program_http_signatures", ROOT / "platform/integrations/fastapi/layerx_fastapi/signatures.py")
        if spec is None or spec.loader is None:
            raise ValueError("actual signature verifier unavailable")
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        cls.signatures = module.LayerXSignatureVerifier()
        cls.key = SecretBytes(protected(cls.fixture["gateway_key_file"], 128))
        cls.transport = AgentHttpTransport(cls.fixture["endpoint"], credential=LayerXKeyCredential(cls.fixture["gateway_key_id"], cls.key), timeout=15)
        cls.client = ProductionClient(cls.transport)
        cls.trust = ProgramTrustContext(bytes.fromhex(cls.fixture["sequencer_public_key"]), protocol_version=3, network_id=cls.fixture["network_id"])
        cls.operations = ProgramOperations(cls.client, cls.signatures, cls.trust)
        if not isinstance(cls.fixture["programs"], list) or {row["guest_abi"] for row in cls.fixture["programs"]} != {1, 2, 3, 4} or len(cls.fixture["programs"]) != 4:
            raise ValueError("all four real registered ABI paths required")

    @classmethod
    def tearDownClass(cls) -> None:
        cls.key.destroy()

    def request(self, row: dict[str, object]) -> NativeProgramRequest:
        self.assertEqual(set(row), {"guest_abi", "payload_file", "signed_activity_file", "fee_limit", "expected_result_code"})
        payload = protected(row["payload_file"])
        call = decode_native_program_call(payload)
        self.assertEqual(encode_native_program_call(call), payload)
        self.assertEqual(call.guest_abi, row["guest_abi"])
        fee = row["fee_limit"]
        self.assertIsInstance(fee, str)
        self.assertEqual(str(int(fee)), fee)
        request = NativeProgramRequest(call, int(fee), protected(row["signed_activity_file"]))
        verify_native_program_call_signature(request.signed_activity, self.signatures, self.trust.network_id)
        decode_signed_program_call(request)
        return request

    def test_real_unified_http_discovery_interface_simulation_call_and_recovery(self) -> None:
        for row in self.fixture["programs"]:
            with self.subTest(abi=row["guest_abi"]):
                request = self.request(row)
                discovered = self.operations.discover(request.program_id)
                self.assertEqual(discovered.abi_version, request.native_call.guest_abi)
                self.assertTrue(discovered.sequencer_signature_verified)
                interface = self.operations.interface(request.program_id)
                self.assertEqual(interface.abi_version, discovered.abi_version)
                self.assertEqual(interface.code_hash, discovered.code_hash)
                self.assertEqual(interface.receipt_digest, discovered.deployment_receipt_digest)
                self.assertEqual(sha256(interface.interface).hexdigest(), interface.interface_digest)
                simulated = self.operations.simulate(request)
                self.assertIs(simulated["committed"], False)
                self.assertEqual(simulated["execution"]["result_code"], row["expected_result_code"])
                self.assertEqual(simulated["execution"]["authority"]["previous_state_root"], discovered.state_root)
                bound = decode_signed_program_call(request)
                result = self.operations.submit(request, IdempotencyKey(bound.idempotency_key))
                self.assertEqual(result["state"], "executed")
                self.assertEqual(result["result_code"], row["expected_result_code"])
                self.assertEqual(result["activity_id"], bound.activity_id)
                recovered = self.operations.receipt(bound.idempotency_key, bound.activity_id)
                self.assertEqual(recovered["receipt"], result["receipt"])
                replay = self.operations.submit(request, IdempotencyKey(bound.idempotency_key))
                self.assertEqual(replay["receipt"], result["receipt"])

    def test_real_discovery_signature_pin_and_signed_call_refusals(self) -> None:
        row = next(row for row in self.fixture["programs"] if row["guest_abi"] == 3)
        request = self.request(row)
        document = self.client.agent("program.discover", {"program_id": request.program_id, "requested_verification_level": "sequencer-signed"})
        _discovery(document, request.program_id, self.trust.now_milliseconds(), self.signatures, self.trust)
        for field in ("code_hash", "state_root", "discovery_public_key", "discovery_signature"):
            changed = dict(document)
            original = changed[field]
            changed[field] = ("0" if original[0] != "0" else "1") + original[1:]
            with self.subTest(field=field), self.assertRaises(ValueError):
                _discovery(changed, request.program_id, self.trust.now_milliseconds(), self.signatures, self.trust)
        for field in ("discovery_public_key", "discovery_signature"):
            changed = dict(document)
            del changed[field]
            with self.assertRaises(ValueError):
                _discovery(changed, request.program_id, self.trust.now_milliseconds(), self.signatures, self.trust)
        with self.assertRaises(ValueError):
            _discovery(document, request.program_id, int(document["valid_through"]) + 1, self.signatures, self.trust)
        signed = request.signed_activity
        corrupted = replace(request, signed_activity=signed[:-1] + bytes([signed[-1] ^ 1]))
        with self.assertRaises(ValueError):
            self.operations.simulate(corrupted)
        with self.assertRaises(ValueError):
            self.operations.simulate(replace(request, fee_limit=request.fee_limit + 1))
        with self.assertRaises(ValueError):
            self.operations.simulate(replace(request, native_call=replace(request.native_call, calldata=request.calldata + b"\0")))
        for abi in (0, 5, 65535, True, 3.0):
            with self.subTest(abi=abi), self.assertRaises(ValueError):
                encode_native_program_call(replace(request.native_call, guest_abi=abi))
        wrong_network = ProgramOperations(self.client, self.signatures, replace(self.trust, network_id=self.trust.network_id + 1))
        with self.assertRaises(ValueError):
            wrong_network.simulate(request)
        with self.assertRaises(ValueError):
            self.operations.submit(request, IdempotencyKey("00" * 32))
        anonymous = ProgramOperations(ProductionClient(AgentHttpTransport(self.fixture["endpoint"], timeout=15)), self.signatures, self.trust)
        with self.assertRaises(PlatformSdkError):
            anonymous.discover(request.program_id)

    def test_real_native_refused_and_unknown_calls_keep_exact_binding(self) -> None:
        refused = self.request(self.fixture["refused_call"])
        self.assertLess(self.fixture["refused_call"]["expected_result_code"], 0)
        self.operations.discover(refused.program_id)
        bound = decode_signed_program_call(refused)
        result = self.operations.submit(refused, IdempotencyKey(bound.idempotency_key))
        self.assertEqual(result["state"], "refused")
        self.assertEqual(result["activity_id"], bound.activity_id)
        self.assertEqual(result["result_code"], self.fixture["refused_call"]["expected_result_code"])
        unknown = self.request(self.fixture["unknown_call"])
        self.operations.discover(unknown.program_id)
        bound = decode_signed_program_call(unknown)
        control_path = os.environ.get("PAXEER_X_PROGRAM_HTTP_CONTROL")
        if not control_path:
            raise RuntimeError("genuine disposable native disconnect controller required")
        control = json.loads(protected(control_path), object_pairs_hook=unique)
        self.assertEqual(set(control), {"refused_activity_id", "node_pid", "node_disconnect_request_file", "node_disconnected_file"})
        self.assertEqual(control["refused_activity_id"], result["activity_id"])
        requested = Path(control["node_disconnect_request_file"])
        acknowledged = Path(control["node_disconnected_file"])
        self.assertTrue(requested.is_absolute() and requested.resolve() == requested)
        self.assertEqual((requested.parent.stat().st_uid, stat.S_IMODE(requested.parent.stat().st_mode)), (os.geteuid(), 0o700))
        fd = os.open(requested, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
        with os.fdopen(fd, "wb") as stream:
            stream.write(bytes.fromhex(result["activity_id"]))
            stream.flush()
            os.fsync(stream.fileno())
        deadline = time.monotonic() + 30
        while not acknowledged.exists():
            if time.monotonic() >= deadline:
                raise RuntimeError("actual native disconnection was not acknowledged")
            time.sleep(0.05)
        acknowledgment = json.loads(protected(acknowledged), object_pairs_hook=unique)
        self.assertEqual(set(acknowledgment), {"node_pid", "refused_activity_id", "refused_receipt_file", "state"})
        self.assertEqual(acknowledgment["node_pid"], control["node_pid"])
        self.assertEqual(acknowledgment["refused_activity_id"], result["activity_id"])
        self.assertEqual(acknowledgment["state"], "native-node-disconnected-after-verified-refusal")
        self.assertEqual(protected(acknowledgment["refused_receipt_file"]).hex(), result["receipt"])
        self.assertFalse(Path(f'/proc/{control["node_pid"]}').exists())
        result = self.operations.submit(unknown, IdempotencyKey(bound.idempotency_key))
        self.assertEqual(result, {"state": "unknown", "activity_id": bound.activity_id, "idempotency_key": bound.idempotency_key,
                                  "retained_signed_activity": bound.canonical_bytes.hex()})
        recovered = self.operations.receipt(bound.idempotency_key, bound.activity_id)
        self.assertEqual(recovered["state"], "unknown")
        self.assertEqual(recovered["retained_signed_activity"], bound.canonical_bytes.hex())


def load_tests(loader, tests, pattern):
    del loader, tests, pattern
    return unittest.TestSuite(ProgramHttp(name) for name in (
        "test_real_discovery_signature_pin_and_signed_call_refusals",
        "test_real_unified_http_discovery_interface_simulation_call_and_recovery",
        "test_real_native_refused_and_unknown_calls_keep_exact_binding",
    ))


if __name__ == "__main__":
    unittest.main()
