import copy
import hashlib
import json
import os
import unittest
from pathlib import Path

from layerx_sdk import NativeEffectPrepareRequestV1, encode_native_effect_prepare_request
from layerx_sdk.agent_http import (
    AgentDaemonEnvelopeTransport,
    AgentEnvelopeTransport,
    AgentSessionCredential,
    LayerXKeyCredential,
)
from layerx_sdk.generated.client import encode_native_prepare_request
from layerx_sdk.production import IdempotencyKey, PlatformSdkError, SdkErrorCode, SecretBytes


class NativeEffectTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        path = os.environ.get("NATIVE_EFFECT_CROSS_LANGUAGE_REQUEST")
        if not path:
            raise RuntimeError("genuine Rust signed native-effect request is required")
        cls.raw = Path(path).read_bytes()
        cls.request = json.loads(cls.raw)
        cls.payload = (
            Path(__file__).resolve().parents[4]
            / "agent/crates/layerx-crypto/tests/fixtures/payments/native-1-5.hex"
        ).read_text().strip()

    def reject(self, path, value):
        request = copy.deepcopy(self.request)
        parent = request
        for part in path[:-1]:
            parent = parent[part]
        parent[path[-1]] = value
        with self.assertRaises((ValueError, TypeError, OverflowError)):
            encode_native_effect_prepare_request(request)

    def test_genuine_rust_request_and_native_c_payload_are_lossless(self):
        encoded = encode_native_effect_prepare_request(self.request)
        self.assertEqual(encoded, self.request)
        self.assertEqual(encoded["activity"], {"version": "1", "module": "1", "ordinal": "5"})
        self.assertEqual(encoded["payload"], self.payload)
        self.assertEqual(encoded["payload_hash"], hashlib.sha256(bytes.fromhex(self.payload)).hexdigest())
        canonical = json.dumps(encoded, sort_keys=True, separators=(",", ":"), ensure_ascii=False).encode("utf-8")
        self.assertEqual(canonical, self.raw)
        self.assertNotEqual(
            hashlib.sha256(b"LXP/agent/native-effect-prepare/v1\0" + canonical).digest(),
            hashlib.sha256(b"LXP/agent/native-prepare/v1\0" + canonical).digest(),
        )
        output = os.environ.get("NATIVE_EFFECT_PYTHON_CANONICAL_OUTPUT")
        if output:
            Path(output).write_bytes(canonical)
        self.assertIn("variant", NativeEffectPrepareRequestV1.__annotations__)

    def test_profiles_remain_separate_and_native_u32_activity_survives(self):
        with self.assertRaises(ValueError):
            encode_native_prepare_request(self.request)
        legacy = copy.deepcopy(self.request)
        legacy["variant"] = "native_v1"
        legacy["activity"]["module"] = "9"
        self.assertEqual(encode_native_prepare_request(legacy)["activity"]["module"], "9")
        with self.assertRaises(ValueError):
            encode_native_effect_prepare_request(legacy)
        for module in range(1, 12):
            request = copy.deepcopy(self.request)
            request["activity"] = {"version": "1", "module": str(module), "ordinal": "65535"}
            if module == 9:
                with self.assertRaises(ValueError):
                    encode_native_effect_prepare_request(request)
                continue
            activity = encode_native_effect_prepare_request(request)["activity"]
            self.assertEqual((int(activity["module"]) << 16) | int(activity["ordinal"]), (module << 16) | 65535)
            request["variant"] = "native_v1"
            with self.assertRaises(ValueError):
                encode_native_prepare_request(request)

    def test_exact_fields_at_every_request_level(self):
        paths = ((), ("activity",), ("purpose",), ("purpose", "purpose"))
        for path in paths:
            original = self.request
            for part in path:
                original = original[part]
            for field in original:
                if not path and field == "local_grant":
                    continue
                request = copy.deepcopy(self.request)
                record = request
                for part in path:
                    record = record[part]
                del record[field]
                with self.subTest(path=path, missing=field), self.assertRaises(ValueError):
                    encode_native_effect_prepare_request(request)
            request = copy.deepcopy(self.request)
            record = request
            for part in path:
                record = record[part]
            record["extra"] = "refused"
            with self.subTest(path=path, extra=True), self.assertRaises(ValueError):
                encode_native_effect_prepare_request(request)
        request = copy.deepcopy(self.request)
        request.pop("local_grant", None)
        self.assertIsNone(encode_native_effect_prepare_request(request)["local_grant"])
        for value in (None, [], "native_effect_v1", {1: "invalid"}):
            with self.assertRaises(ValueError):
                encode_native_effect_prepare_request(value)

    def test_canonical_numeric_and_hex_refusals(self):
        for field in ("account_sequence", "not_before", "not_after", "fee_limit"):
            for value in (1, True, "", "01", "+1", "-1", " 1", "1 ", "1.0", "١", str(1 << (128 if field == "fee_limit" else 64))):
                with self.subTest(field=field, value=value):
                    self.reject((field,), value)
        for field in ("idempotency_key", "payload_hash", "capability_id"):
            for value in (None, "", "00", "ab" * 31, "ab" * 33, "AB" * 32, "gg" * 32, 0):
                with self.subTest(field=field, value=value):
                    self.reject((field,), value)
        for value in (None, "", "0", "0A", "gg", "00" * 524289):
            self.reject(("payload",), value)
        for field, values in {
            "version": (1, "0", "2"),
            "module": (1, "0", "9", "12", "65536", "01"),
            "ordinal": (5, "0", "65536", "05"),
        }.items():
            for value in values:
                self.reject(("activity", field), value)

    def test_purpose_grant_and_timestamp_bindings_are_closed(self):
        self.reject(("actor",), "did:layerx:different")
        self.reject(("capability_id",), "fa" * 32 if self.request["capability_id"] != "fa" * 32 else "fb" * 32)
        self.reject(("not_before",), str(int(self.request["not_after"]) + 1))
        for field in ("generation", "expires_at_ms"):
            for value in ("0", "01", 1, str(1 << 64)):
                self.reject(("purpose", "purpose", field), value)
        for field in ("tenant", "agent_did"):
            for value in ("", "x\0y", "é" * 128, "\ud800"):
                self.reject(("purpose", "purpose", field), value)
        for field in ("session_id", "capability_id", "preparation_id", "canonical_digest", "commitment"):
            for value in ("AB" * 32, "00", "a" * 63):
                self.reject(("purpose", "purpose", field), value)
        self.reject(("purpose", "owner_public_key"), "ab" * 31)
        self.reject(("purpose", "signature"), "ab" * 63)
        self.reject(("purpose", "signature"), "AB" * 64)
        for value in ({}, [], "", {"version": "1"}, {"extra": None}):
            self.reject(("local_grant",), value)
        for field, values in {
            "actor": ("", "x\0y", "é" * 128),
            "authority": ("", "x\0y", "x" * 524289),
        }.items():
            for value in values:
                self.reject((field,), value)

    def test_exact_unsigned_integer_limits_and_utf8_bounds(self):
        request = copy.deepcopy(self.request)
        for field in ("account_sequence", "not_before", "not_after"):
            request[field] = str((1 << 64) - 1)
        request["fee_limit"] = str((1 << 128) - 1)
        request["actor"] = "é" * 127 + "x"
        request["purpose"]["purpose"]["agent_did"] = request["actor"]
        request["authority"] = "x" * 524288
        request["payload"] = "00" * 524288
        encoded = encode_native_effect_prepare_request(request)
        self.assertEqual(encoded, request)

    def test_real_transport_refuses_session_and_idempotency_mismatches_before_io(self):
        purpose = self.request["purpose"]["purpose"]
        credential = LayerXKeyCredential("codec-test", SecretBytes(b"lxp_live_" + b"a" * 64))
        token = "bb" * 32
        session = AgentSessionCredential(purpose["tenant"], purpose["session_id"], token, int(purpose["generation"]))
        key = IdempotencyKey(self.request["idempotency_key"])
        transport = AgentEnvelopeTransport("http://127.0.0.1:1", gateway_key=credential, session=session, timeout=0.01)
        for field, value in {
            "tenant": purpose["tenant"] + "-other",
            "session_id": "ff" * 32 if purpose["session_id"] != "ff" * 32 else "fe" * 32,
            "generation": str(int(purpose["generation"]) + 1),
        }.items():
            request = copy.deepcopy(self.request)
            request["purpose"]["purpose"][field] = value
            with self.subTest(field=field), self.assertRaises(PlatformSdkError) as raised:
                transport.prepare_native_effect(request, key)
            self.assertEqual(raised.exception.code, SdkErrorCode.INVALID_ARGUMENT)
        other_key = IdempotencyKey("ff" * 32 if str(key) != "ff" * 32 else "fe" * 32)
        with self.assertRaises(PlatformSdkError) as raised:
            transport.prepare_native_effect(self.request, other_key)
        self.assertEqual(raised.exception.code, SdkErrorCode.INVALID_ARGUMENT)
        without_session = AgentEnvelopeTransport("http://127.0.0.1:1", gateway_key=credential, session=None, timeout=0.01)
        with self.assertRaises(PlatformSdkError) as raised:
            without_session.prepare_native_effect(self.request, key)
        self.assertEqual(raised.exception.code, SdkErrorCode.INVALID_ARGUMENT)
        self.assertIs(AgentDaemonEnvelopeTransport.prepare_native_effect, AgentEnvelopeTransport.prepare_native_effect)
