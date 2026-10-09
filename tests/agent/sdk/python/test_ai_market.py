from __future__ import annotations

import importlib.util
import json
import secrets
import shutil
import struct
import sys
import tempfile
import threading
import unittest
from dataclasses import replace
from hashlib import sha256
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.parse import parse_qsl, urlsplit

from cryptography.hazmat.primitives import hashes
from cryptography.hazmat.primitives.asymmetric import ec, utils
from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey
from cryptography.hazmat.primitives.serialization import Encoding, PublicFormat

ROOT = Path(__file__).resolve().parents[4]
sys.path.insert(0, str(ROOT / "agent/sdk/python"))

from layerx_sdk.agent_http import AgentHttpTransport, LayerXKeyCredential  # noqa: E402
from layerx_sdk.ai_market import (  # noqa: E402
    AUTHORITY_FRESHNESS_HEIGHTS,
    BAD_VERSION,
    CAPACITY,
    CLAIM,
    CLIENT_ERRORS,
    CURSOR_MAX_BYTES,
    DEFAULT_PAGE_ROWS,
    ENTRYPOINT,
    ENVELOPE_MAX_BYTES,
    F06_CONTRIBUTION_CONSENT_REQUIRED,
    F06_FUNDING_POLICY_MISMATCH,
    F06_INVALID_AMOUNT,
    F06_NOTHING_TO_CLAIM,
    FINALIZED_RANK,
    FUND,
    GUEST_ABI,
    MAX_PAGE_ROWS,
    MISMATCH_FIELDS,
    NATIVE_CALL_PROTOCOL_VERSION,
    NON_CANONICAL,
    OPERATIONS,
    PAYLOAD_MAX_BYTES,
    PROGRAM_CALL_ORDINAL,
    PROGRAMS_MODULE,
    QUERY_ERRORS,
    SDK_TRANSITIONS,
    SETTLEMENT_RANK,
    UNAUTHORIZED,
    UNKNOWN_OPERATION,
    WRONG_DOMAIN,
    WRONG_MARKET,
    WRONG_PROGRAM,
    WRONG_ROSTER,
    AiMarketError,
    AiMarketViews,
    ApprovalTerms,
    Availability,
    DomainStatus,
    Effect,
    EpochEntry,
    EpochPage,
    EpochStatus,
    Freshness,
    FreshnessLabel,
    KindFilter,
    NativeTerms,
    OperationJournal,
    OperationRecord,
    OperationRequest,
    ParticipantKind,
    PaxaiEnvelope,
    PreparedOperation,
    ProjectionState,
    Review,
    Reward,
    Score,
    ScoreStatus,
    SdkState,
    SnapshotBinding,
    _unsigned_activity,
    decode_envelope,
    first_difference,
)
from layerx_sdk.native_program_call import NativeProgramCall, decode_native_program_call, encode_native_program_call  # noqa: E402
from layerx_sdk.production import ProductionClient, SecretBytes  # noqa: E402
from layerx_sdk.program_wire import _signed_call_envelope  # noqa: E402
from layerx_sdk.programs import ProgramOperations, ProgramTrustContext  # noqa: E402
from layerx_sdk.verifier import (  # noqa: E402
    AuthorizedReceiptBatch,
    CheckpointAttestation,
    CheckpointCertificate,
    CheckpointVerificationInput,
    GuarantorKey,
    _attestation_message,
    decode_batch_header,
    verify_checkpoint,
)

SIGNATURES_SPEC = importlib.util.spec_from_file_location(
    "ai_market_signatures", ROOT / "platform/integrations/fastapi/layerx_fastapi/signatures.py")
if SIGNATURES_SPEC is None or SIGNATURES_SPEC.loader is None:
    raise RuntimeError("real signature verifier is unavailable")
SIGNATURES = importlib.util.module_from_spec(SIGNATURES_SPEC)
SIGNATURES_SPEC.loader.exec_module(SIGNATURES)
VERIFIER = SIGNATURES.LayerXSignatureVerifier()

MULTICALL = ROOT / "tests/fixtures/programs/maintained-multicall"
CUSTODY = ROOT / "tests/fixtures/custody/daemon-credit-receipt"
FIXTURE_KEY = bytes.fromhex("8461a06f7c3c0cf1111ad70da871ba9b00ffb601073e7b5705dfabcfad043cd5")

B1 = SnapshotBinding(b"\xc1" * 32, b"\x9a" * 32, b"\x4d" * 32, 900, 1200, b"\xb1" * 32, b"\x5e" * 32, 7, b"\x5d" * 32,
                     3, 2, b"\x90" * 32, b"\x70" * 32, b"\xcc" * 32, None, 4, 1_700_000_000_000)
B1_ID = "f53745e693dda92c64b6bec6cfc82ab138cb7aa279bb9e4b260b7d6e3041323f"
B1_ENC = ("0001" + "c1" * 32 + "9a" * 32 + "4d" * 32 + "0000000000000384" + "00000000000004b0" + "b1" * 32 + "5e" * 32
          + "0000000000000007" + "5d" * 32 + "01" + "0000000000000003" + "0000000000000002" + "90" * 32 + "01"
          + "70" * 32 + "cc" * 32 + "00" + "04" + "0000018bcfe56800" + "0000000000000000")
B2 = replace(B1, epoch=None, roster=None, observed_sequence=901, revision=8, settlement=b"\x5c" * 32)
B2_ID = "641ca6547dc3077a1c8b922da3cc5498cecc2b7218d5245ee57e46cc1f3f9b8e"
B2_ENC = ("0001" + "c1" * 32 + "9a" * 32 + "4d" * 32 + "0000000000000385" + "00000000000004b0" + "b1" * 32 + "5e" * 32
          + "0000000000000008" + "5d" * 32 + "00" + "0000000000000002" + "90" * 32 + "00" + "cc" * 32 + "01"
          + "5c" * 32 + "04" + "0000018bcfe56800" + "0000000000000000")
ACTOR = b"\xa1" * 32
REQUEST = b"\xe1" * 32
WORKER = b"\x11" * 32
PAYEE = b"\x22" * 32
ASSET = b"\xa5" * 32
CLAIM_PAYLOAD = WORKER + PAYEE + (5_000_000_000_000_000_000_000).to_bytes(16, "big")
CLAIM_ENV = ("5041584149310001" + "0602" + "c1" * 32 + "9a" * 32 + "4d" * 32 + "a1" * 32 + "0000000000000003"
             + "0000000000000002" + "70" * 32 + "0000000000000000" + "0000000000000514" + "e1" * 32 + "00000050"
             + "11" * 32 + "22" * 32 + "000000000000010f0cf064dd59200000" + "00")
CLAIM_DIGEST = "0c6f479413d014e5e434c743d4446fcbb78b28b24c7ee6a633a0a2d03fb07877"
FUND_PAYLOAD = b"\xff" * 16 + b"\x33" * 32 + struct.pack(">Q", 1) + b"\x01"
FUND_ENV = ("5041584149310001" + "0601" + "c1" * 32 + "9a" * 32 + "4d" * 32 + "a1" * 32 + "0000000000000000"
            + "0000000000000002" + "00" * 32 + "0000000000000004" + "0000000000000514" + "e1" * 32 + "00000039"
            + "ff" * 16 + "33" * 32 + "0000000000000001" + "01" + "00")
FUND_DIGEST = "83cf5c4dea749cbfd2c65da32225138a74017bfb27024ba707af06d27bf72f47"
RESOURCES = (1_000_000, 16_777_216, 1_048_576, 1_048_576, 64, 1_048_576, 4096)
COMPONENTS = {f"F{number:02d}": "available" for number in range(1, 11)}


def claim_envelope(**changes: object) -> PaxaiEnvelope:
    return replace(PaxaiEnvelope(CLAIM, B1.chain, B1.program, B1.market, ACTOR, 3, 2, B1.roster, 0, 1300, REQUEST,
                                 CLAIM_PAYLOAD), **changes)


def fund_envelope(**changes: object) -> PaxaiEnvelope:
    return replace(PaxaiEnvelope(FUND, B1.chain, B1.program, B1.market, ACTOR, 0, 2, None, 4, 1300, REQUEST,
                                 FUND_PAYLOAD), **changes)


def schema() -> dict[str, dict[str, object]]:
    sections: dict[str, dict[str, object]] = {}
    current: dict[str, object] = {}
    for line in (ROOT / "platform/sdk/schema/paxai-v1.kvx").read_text().splitlines():
        if not line or line.startswith("#"):
            continue
        if line.startswith("["):
            current = sections.setdefault(line[1:-1], {})
            continue
        key, value = line.split(" = ", 1)
        current[key] = json.loads(value)
    return sections


def hx(value: bytes) -> str:
    return "0x" + value.hex()


def binding_json(binding: SnapshotBinding) -> dict[str, object]:
    return {
        "chain": hx(binding.chain), "program": hx(binding.program), "market": hx(binding.market),
        "observed_sequence": str(binding.observed_sequence), "execution_height": str(binding.execution_height),
        "batch_id": hx(binding.batch_id), "native_state_root": hx(binding.native_state_root),
        "revision": str(binding.revision), "state_digest": hx(binding.state_digest),
        "epoch": None if binding.epoch is None else str(binding.epoch), "config": str(binding.config),
        "policy": hx(binding.policy), "roster": None if binding.roster is None else hx(binding.roster),
        "checkpoint": hx(binding.checkpoint), "settlement": None if binding.settlement is None else hx(binding.settlement),
        "rank": binding.rank, "publication_time_ms": str(binding.publication_time_ms),
    }


def snapshot_result(binding: SnapshotBinding, components: dict[str, str] = COMPONENTS) -> dict[str, object]:
    return {"snapshot_id": hx(binding.snapshot_id()), "projection": "finalized-publishable",
            "binding": binding_json(binding), "components": dict(components), "source_activity": hx(b"\x5a" * 32),
            "freshness": {"label": "current"}}


def page_result(binding: SnapshotBinding, rows: list[dict[str, object]], cursor: str | None = None,
                components: dict[str, str] = COMPONENTS) -> dict[str, object]:
    return {"snapshot_id": hx(binding.snapshot_id()), "binding": binding_json(binding), "components": dict(components),
            "rows": rows, "cursor": cursor}


def row_json(kind: str, ident: bytes, *, eligibility: int = 1, score: dict[str, object] | None = None,
             reward: dict[str, object] | None = None, history: dict[str, object] | None = None) -> dict[str, object]:
    return {
        "kind": kind, "id": hx(ident), "owner": hx(b"\x0a" * 32), "generation": "1", "identity_state": 2,
        "frozen_member": True, "frozen_generation": "1", "eligibility": eligibility, "metadata": None,
        "metadata_revision": "0",
        "score": score or {"status": "not-produced", "epoch": None, "ppm": None},
        "reward": reward or {"status": "not-yet-produced", "asset": None, "earned": None, "claimed": None},
        "history": history or {"status": "not-yet-produced", "digest": None},
    }


def credential(seed: str) -> LayerXKeyCredential:
    return LayerXKeyCredential(f"reader-{seed}", SecretBytes(("lxp_live_" + seed * 64).encode()))


class _Http:
    def __init__(self, handle_get: object = None, handle_post: object = None) -> None:
        class Handler(BaseHTTPRequestHandler):
            def do_GET(self) -> None:
                handle_get(self)  # type: ignore[operator]

            def do_POST(self) -> None:
                handle_post(self)  # type: ignore[operator]

            def log_message(self, _format: str, *args: object) -> None:
                del _format, args

        self.server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)
        self.thread.start()
        self.endpoint = f"http://127.0.0.1:{self.server.server_port}"

    def close(self) -> None:
        self.server.shutdown()
        self.server.server_close()
        self.thread.join()


def send_json(handler: BaseHTTPRequestHandler, status: int, body: object, content_type: str = "application/json") -> None:
    data = body if isinstance(body, bytes) else json.dumps(body).encode()
    handler.send_response(status)
    handler.send_header("Content-Type", content_type)
    handler.send_header("Content-Length", str(len(data)))
    handler.end_headers()
    handler.wfile.write(data)


class Gateway:
    def __init__(self) -> None:
        self.views: dict[str, dict[str, object]] = {}
        self.cursors: dict[str, tuple[str, str, str, str, int]] = {}
        self.requests: list[tuple[str, dict[str, str], str | None]] = []
        self.replies: list[tuple[int, object, str]] = []
        self.http = _Http(handle_get=self.handle)
        self.endpoint = self.http.endpoint

    def close(self) -> None:
        self.http.close()

    def publish(self, binding: SnapshotBinding, rows: list[dict[str, object]],
                components: dict[str, str] = COMPONENTS) -> None:
        view = self.views.setdefault(binding.market.hex(), {"snapshots": {}, "epochs": ("available", [])})
        snapshot_id = binding.snapshot_id().hex()
        view["snapshots"][snapshot_id] = (binding, rows, components)  # type: ignore[index]
        view["current"] = snapshot_id

    def reply(self, status: int, body: object, content_type: str = "application/json") -> None:
        self.replies.append((status, body, content_type))

    def handle(self, handler: BaseHTTPRequestHandler) -> None:
        url = urlsplit(handler.path)
        pairs = parse_qsl(url.query, keep_blank_values=True)
        query = dict(pairs)
        authorization = handler.headers.get("Authorization")
        self.requests.append((url.path, query, authorization))
        if self.replies:
            status, body, content_type = self.replies.pop(0)
            send_json(handler, status, body, content_type)
            return
        parts = url.path.split("/")
        if (len(parts) != 6 or parts[:4] != ["", "v1", "ai", "markets"] or len(query) != len(pairs)
                or parts[4] not in self.views):
            send_json(handler, 400, {"ok": False, "error": {"code": "invalid-encoding"}})
            return
        view = self.views[parts[4]]
        snapshots: dict[str, tuple[SnapshotBinding, list[dict[str, object]], dict[str, str]]] = view["snapshots"]  # type: ignore[assignment]
        if parts[5] == "epochs":
            component, entries = view["epochs"]  # type: ignore[misc]
            start, limit = int(query["from"]), int(query["limit"])
            selected = [entry for entry in entries if int(entry["epoch"]) >= start][:limit]
            send_json(handler, 200, {"ok": True, "result": {"component": component, "entries": selected}})
            return
        if parts[5] == "snapshot":
            snapshot_id = query.get("snapshot", view["current"])
            if snapshot_id not in snapshots:
                oldest_id, (oldest, _, _) = min(snapshots.items(), key=lambda item: item[1][0].observed_sequence)
                send_json(handler, 410, {"ok": False, "error": {"code": "snapshot-pruned", "oldest": {
                    "snapshot_id": "0x" + oldest_id, "observed_sequence": str(oldest.observed_sequence)}}})
                return
            binding, _, components = snapshots[snapshot_id]
            send_json(handler, 200, {"ok": True, "result": snapshot_result(binding, components)})
            return
        principal = (authorization or "").split(":")[0]
        kind, active, limit = query["kind"], query["active_only"], int(query["limit"])
        cursor = query.get("cursor")
        if cursor is not None:
            scope = self.cursors.get(cursor)
            if scope is None or scope[:4] != (principal, query.get("snapshot"), kind, active):
                send_json(handler, 400, {"ok": False, "error": {"code": "cursor-mismatch"}})
                return
            del self.cursors[cursor]
            snapshot_id, offset = scope[1], scope[4]
        else:
            snapshot_id, offset = query.get("snapshot", view["current"]), 0
        binding, rows, components = snapshots[snapshot_id]
        codes = {"worker": 1, "evaluator": 2}
        selected = sorted(
            (row for row in rows if (kind == "all" or row["kind"] == kind)
             and (active == "false" or int(row["eligibility"]) & 1)),
            key=lambda row: (codes[row["kind"]], bytes.fromhex(row["id"][2:])))
        page = selected[offset:offset + limit]
        issued = None
        if offset + limit < len(selected):
            issued = secrets.token_hex(176)
            self.cursors[issued] = (principal, snapshot_id, kind, active, offset + limit)
        send_json(handler, 200, {"ok": True, "result": page_result(binding, page, issued, components)})


def checkpoint_for(directory: Path):
    canonical = (directory / "header").read_bytes()
    header = decode_batch_header(canonical)
    checkpoint = sha256(b"LXP/v2/checkpoint-certificate\0" + canonical + bytes(4)).digest()
    key = ec.generate_private_key(ec.SECP256K1())
    public = key.public_key().public_bytes(Encoding.X962, PublicFormat.CompressedPoint)
    uncompressed = key.public_key().public_bytes(Encoding.X962, PublicFormat.UncompressedPoint)
    signer = SIGNATURES._keccak256(uncompressed[1:])[-20:]
    attestation = CheckpointAttestation(
        3, header.network_id, 125, bytes.fromhex("11" * 20), header.epoch, checkpoint, checkpoint,
        sha256(public).digest(), header.batch_number, header.data_availability_root, True, True, 31,
        header.timestamp_ms, signer, b"", 27)
    digest = sha256(b"LXP/v2/guarantor-attestation\0" + _attestation_message(attestation)).digest()
    r, s = utils.decode_dss_signature(key.sign(digest, ec.ECDSA(utils.Prehashed(hashes.SHA256()))))
    order = 0xFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFEBAAEDCE6AF48A03BBFD25E8CD0364141
    signature = r.to_bytes(32, "big") + min(s, order - s).to_bytes(32, "big")
    recovery = next(v for v in (27, 28) if VERIFIER.verify_recoverable_secp256k1(public, signature, v, signer, digest))
    attestation = replace(attestation, signature=signature, signature_v=recovery)
    return verify_checkpoint(CheckpointVerificationInput(
        CheckpointCertificate(canonical, b"", (attestation,), 1), (GuarantorKey(attestation.guarantor_id, public, True),),
        checkpoint, 125, attestation.settlement_contract, None, True), VERIFIER, protocol_version=3)


class Ed25519KeySigner:
    def __init__(self) -> None:
        self._key = Ed25519PrivateKey.generate()

    def public_key(self) -> bytes:
        return self._key.public_key().public_bytes(Encoding.Raw, PublicFormat.Raw)

    def sign(self, digest: bytes) -> bytes:
        return self._key.sign(digest)


class ZeroSigner(Ed25519KeySigner):
    def sign(self, digest: bytes) -> bytes:
        del digest
        return bytes(64)


class AiMarketCase(unittest.TestCase):
    def assertRefused(self, code: str, detail: object, call: object, *args: object, **kwargs: object) -> None:
        with self.assertRaises(AiMarketError) as raised:
            call(*args, **kwargs)  # type: ignore[operator]
        self.assertEqual((raised.exception.code, raised.exception.detail), (code, detail))

    def assertApplication(self, code: int, call: object, *args: object, **kwargs: object) -> None:
        self.assertRefused("Application", code, call, *args, **kwargs)


class SchemaContractTests(AiMarketCase):
    def test_constants_enums_transitions_and_errors_match_the_frozen_schema(self) -> None:
        contract = schema()
        limits = contract["limits"]
        self.assertEqual(
            (FINALIZED_RANK, SETTLEMENT_RANK, DEFAULT_PAGE_ROWS, MAX_PAGE_ROWS, CURSOR_MAX_BYTES,
             AUTHORITY_FRESHNESS_HEIGHTS, ENVELOPE_MAX_BYTES, PAYLOAD_MAX_BYTES),
            (limits["finalized_rank"], limits["settlement_rank"], limits["default_page_rows"], limits["max_page_rows"],
             limits["cursor_max_bytes"], limits["authority_freshness_heights"], limits["envelope_max_bytes"],
             limits["payload_max_bytes"]))
        for section, enum in (("projection_state", ProjectionState), ("freshness", FreshnessLabel),
                              ("epoch_status", EpochStatus), ("availability", Availability),
                              ("participant_kind", ParticipantKind), ("score_status", ScoreStatus),
                              ("kind_filter", KindFilter), ("sdk_state", SdkState), ("domain_status", DomainStatus)):
            with self.subTest(section=section):
                self.assertEqual({member.name.lower(): int(member) for member in enum}, contract[section])
        self.assertEqual({target.name.lower(): sorted(source.name.lower() for source in sources)
                          for target, sources in SDK_TRANSITIONS.items()},
                         {target: sorted(sources) for target, sources in contract["sdk_transition"].items()})
        self.assertEqual(list(QUERY_ERRORS), contract["errors"]["query"])
        self.assertEqual(list(CLIENT_ERRORS), contract["errors"]["client"])
        self.assertEqual(list(MISMATCH_FIELDS), contract["approval"]["mismatch_fields"])
        native = contract["native_call"]
        self.assertEqual((NATIVE_CALL_PROTOCOL_VERSION, PROGRAMS_MODULE, PROGRAM_CALL_ORDINAL, GUEST_ABI, ENTRYPOINT),
                         (native["protocol_version"], native["module"], native["activity_ordinal"], native["guest_abi"],
                          native["entrypoint"]))
        self.assertEqual(ProjectionState.EVIDENCE_VERIFIED_UNFINALIZED.wire, "evidence-verified-unfinalized")

    def test_operation_table_matches_every_schema_row(self) -> None:
        rows = schema()["operations"]
        self.assertEqual(len(rows), 55)
        self.assertEqual(len(OPERATIONS), 55)
        for name, row in rows.items():
            operation = OPERATIONS[int(row[0], 16)]
            with self.subTest(operation=name):
                self.assertEqual(
                    (operation.name, operation.feature, "mutation" if operation.mutation else "program-read",
                     "object-local" if operation.object_local else "role",
                     "delegate" if operation.delegate else "native-only", operation.payload_min, operation.payload_max),
                    (name, int(row[1]), row[2], row[3], row[4], int(row[5]), int(row[6])))


class SnapshotBindingTests(AiMarketCase):
    def test_binding_identity_and_encoding_match_the_rust_program_vectors(self) -> None:
        self.assertEqual(B1.snapshot_id().hex(), B1_ID)
        self.assertEqual(B1.encode().hex(), B1_ENC)
        self.assertEqual(B2.snapshot_id().hex(), B2_ID)
        self.assertEqual(B2.encode().hex(), B2_ENC)
        republished = replace(B1, checkpoint=b"\xcd" * 32, publication_time_ms=1)
        self.assertEqual(republished.snapshot_id().hex(), B1_ID)
        self.assertNotEqual(republished.encode(), B1.encode())
        self.assertApplication(NON_CANONICAL, replace(B1, revision=0).encode)
        self.assertApplication(NON_CANONICAL, replace(B1, rank=5).encode)

    def test_binding_requires_its_own_checkpoint_root_and_sequence_range(self) -> None:
        verification = checkpoint_for(MULTICALL)
        header = verification.header
        self.assertEqual((verification.level, header.first_sequence, header.last_sequence),
                         ("checkpoint-finalised", 5, 7))
        bound = replace(B1, checkpoint=verification.checkpoint_id, native_state_root=header.resulting_state_root,
                        observed_sequence=6)
        self.assertIsNone(bound.require_checkpoint(verification))
        for changed in (replace(bound, observed_sequence=8), replace(bound, observed_sequence=4),
                        replace(bound, native_state_root=b"\x5e" * 32), replace(bound, checkpoint=b"\xcc" * 32)):
            with self.subTest(changed=changed):
                self.assertRefused("BindingMismatch", None, changed.require_checkpoint, verification)

    def test_freshness_labels_follow_the_authority_window(self) -> None:
        self.assertEqual(Freshness.of(1200, None), Freshness(FreshnessLabel.UNKNOWN))
        self.assertEqual(Freshness.of(1200, 1208), Freshness(FreshnessLabel.CURRENT))
        self.assertEqual(Freshness.of(1200, 1209), Freshness(FreshnessLabel.STALE, 9))


class EnvelopeTests(AiMarketCase):
    def test_claim_and_fund_envelopes_match_the_rust_codec_vectors(self) -> None:
        claim = claim_envelope()
        self.assertEqual(claim.encode().hex(), CLAIM_ENV)
        self.assertEqual(claim.request_digest().hex(), CLAIM_DIGEST)
        self.assertEqual(decode_envelope(bytes.fromhex(CLAIM_ENV)), claim)
        fund = fund_envelope()
        self.assertEqual(fund.encode().hex(), FUND_ENV)
        self.assertEqual(fund.request_digest().hex(), FUND_DIGEST)
        self.assertEqual(decode_envelope(bytes.fromhex(FUND_ENV)), fund)
        read = PaxaiEnvelope(0x0A01, B1.chain, B1.program, B1.market, ACTOR, 0, 0, None, 0, 1300, REQUEST, b"")
        self.assertEqual(decode_envelope(read.encode()), read)

    def test_envelope_validation_refuses_with_the_program_codes_in_order(self) -> None:
        self.assertApplication(NON_CANONICAL, claim_envelope(sequence=1).encode)
        self.assertApplication(NON_CANONICAL, fund_envelope(sequence=0).encode)
        self.assertApplication(UNAUTHORIZED, claim_envelope(delegate=(b"\x44" * 32, b"\x55" * 64)).encode)
        self.assertApplication(NON_CANONICAL, claim_envelope(payload=CLAIM_PAYLOAD[:-1]).encode)
        self.assertApplication(NON_CANONICAL, claim_envelope(expiry=0).encode)
        self.assertApplication(NON_CANONICAL, claim_envelope(config=0).encode)
        self.assertApplication(WRONG_ROSTER, claim_envelope(roster=None).encode)
        self.assertApplication(WRONG_ROSTER, fund_envelope(epoch=3).encode)
        self.assertApplication(UNKNOWN_OPERATION, claim_envelope(selector=0x0208).encode)
        self.assertApplication(NON_CANONICAL, claim_envelope(selector=0x0106, sequence=1, payload=bytes(44)).encode)
        self.assertApplication(NON_CANONICAL, claim_envelope(actor=bytes(32)).encode)
        self.assertApplication(NON_CANONICAL, PaxaiEnvelope(0x0A01, B1.chain, B1.program, B1.market, ACTOR, 0, 2, None,
                                                            0, 1300, REQUEST, b"").encode)
        self.assertApplication(CAPACITY, claim_envelope(selector=0x0101, sequence=1, roster=None, epoch=0,
                                                        payload=bytes(PAYLOAD_MAX_BYTES + 1)).encode)

    def test_decode_refuses_noncanonical_bytes_with_the_program_codes(self) -> None:
        encoded = bytes.fromhex(CLAIM_ENV)

        def patched(offset: int, value: bytes) -> bytes:
            return encoded[:offset] + value + encoded[offset + len(value):]

        self.assertApplication(CAPACITY, decode_envelope, encoded + bytes(ENVELOPE_MAX_BYTES))
        self.assertApplication(NON_CANONICAL, decode_envelope, b"PAXAI2" + encoded[6:])
        self.assertApplication(BAD_VERSION, decode_envelope, patched(6, b"\x00\x02"))
        self.assertApplication(UNKNOWN_OPERATION, decode_envelope, patched(8, b"\x02\x08"))
        self.assertApplication(NON_CANONICAL, decode_envelope, patched(10, bytes(32)))
        self.assertApplication(NON_CANONICAL, decode_envelope, patched(202, bytes(32)))
        self.assertApplication(CAPACITY, decode_envelope, patched(234, struct.pack(">I", PAYLOAD_MAX_BYTES + 1)))
        self.assertApplication(NON_CANONICAL, decode_envelope, encoded[:-1] + b"\x02")
        self.assertApplication(NON_CANONICAL, decode_envelope, encoded + b"\x00")
        self.assertApplication(NON_CANONICAL, decode_envelope, encoded[:-2])
        self.assertApplication(NON_CANONICAL, decode_envelope, patched(186, struct.pack(">Q", 1)))

    def test_domain_check_order_is_chain_then_program_then_market(self) -> None:
        claim = claim_envelope()
        self.assertIsNone(claim.check_domain(B1.chain, B1.program, B1.market))
        self.assertApplication(WRONG_DOMAIN, claim.check_domain, b"\xc2" * 32, b"\x9b" * 32, b"\x4e" * 32)
        self.assertApplication(WRONG_PROGRAM, claim.check_domain, B1.chain, b"\x9b" * 32, b"\x4e" * 32)
        self.assertApplication(WRONG_MARKET, claim.check_domain, B1.chain, B1.program, b"\x4e" * 32)

    def test_unsigned_activity_reproduces_the_signed_program_call_fixture(self) -> None:
        signed = (MULTICALL / "activity-0").read_bytes()
        activity = _signed_call_envelope(signed)
        terms = NativeTerms(7, b"did:lxp:program-call", FIXTURE_KEY, 1, bytes(31) + b"\x03", 1, 100, (1 << 64) - 1,
                            b"", b"", 16, RESOURCES)
        self.assertEqual((activity.network_id, activity.public_key, activity.not_before, activity.not_after,
                          activity.idempotency, activity.fee_limit),
                         (terms.network_id, terms.owner_public_key, terms.not_before, terms.not_after,
                          terms.idempotency_key, terms.fee_limit))
        expected = signed[:4] + b"\x0b" + signed[5:-(1 + 4 + 64)]
        self.assertEqual(_unsigned_activity(terms, activity.payload), expected)


class ViewTestCase(AiMarketCase):
    def setUp(self) -> None:
        self.gateway = Gateway()
        self.addCleanup(self.gateway.close)
        self.views = AiMarketViews(self.gateway.endpoint, credential=credential("a"), timeout=15)


class SnapshotViewTests(ViewTestCase):
    def test_snapshot_is_recomputed_pinned_and_refused_on_drift(self) -> None:
        self.gateway.publish(B1, [])
        current = self.views.snapshot(B1.market)
        self.assertEqual((current.snapshot_id.hex(), current.binding, current.projection, current.freshness),
                         (B1_ID, B1, ProjectionState.FINALIZED_PUBLISHABLE, Freshness(FreshnessLabel.CURRENT)))
        self.assertEqual(current.components, (Availability.AVAILABLE,) * 10)
        self.assertEqual(current.source_activity, b"\x5a" * 32)
        self.gateway.publish(B2, [])
        self.assertEqual(self.views.snapshot(B1.market).snapshot_id.hex(), B2_ID)
        pinned = self.views.snapshot(B1.market, snapshot=bytes.fromhex(B1_ID))
        self.assertEqual((pinned.snapshot_id.hex(), pinned.binding), (B1_ID, B1))
        self.assertEqual(self.gateway.requests[-1][1], {"snapshot": B1_ID})
        self.assertEqual(self.gateway.requests[-1][2], "LayerX-Key reader-a:lxp_live_" + "a" * 64)
        self.assertRefused("ViewRefused", ("snapshot-pruned", (bytes.fromhex(B1_ID), 900)),
                           self.views.snapshot, B1.market, snapshot=b"\x99" * 32)
        self.gateway.reply(200, {"ok": True, "result": snapshot_result(B2)})
        self.assertRefused("SnapshotConflict", None, self.views.snapshot, B1.market, snapshot=bytes.fromhex(B1_ID))
        wrong_id = snapshot_result(B1)
        wrong_id["snapshot_id"] = hx(bytes.fromhex(B2_ID))
        self.gateway.reply(200, {"ok": True, "result": wrong_id})
        self.assertRefused("BindingMismatch", None, self.views.snapshot, B1.market)
        self.gateway.reply(200, {"ok": True, "result": snapshot_result(replace(B1, market=b"\x4e" * 32))})
        self.assertRefused("BindingMismatch", None, self.views.snapshot, B1.market)
        self.gateway.reply(200, {"ok": True, "result": snapshot_result(replace(B1, rank=3))})
        self.assertRefused("FinalityUnavailable", None, self.views.snapshot, B1.market)
        unfinalized = snapshot_result(B1)
        unfinalized["projection"] = "evidence-verified-unfinalized"
        self.gateway.reply(200, {"ok": True, "result": unfinalized})
        self.assertRefused("FinalityUnavailable", None, self.views.snapshot, B1.market)
        stale = snapshot_result(B1)
        stale["freshness"] = {"label": "stale", "lag": "9"}
        self.gateway.reply(200, {"ok": True, "result": stale})
        self.assertEqual(self.views.snapshot(B1.market).freshness, Freshness(FreshnessLabel.STALE, 9))

    def test_gateway_refusals_map_to_typed_errors_and_malformed_bodies_fail_integrity(self) -> None:
        self.gateway.publish(B1, [])
        for status, body, expected in (
            (503, {"ok": False, "error": {"code": "authority-stale", "lag": "12"}}, ("StaleAuthority", 12)),
            (429, {"ok": False, "error": {"code": "rate-limited"}}, ("ViewRefused", ("rate-limited", None))),
            (410, {"ok": False, "error": {"code": "snapshot-pruned", "oldest": None}},
             ("ViewRefused", ("snapshot-pruned", None))),
            (409, {"ok": False, "error": {"code": "snapshot-conflict"}}, ("SnapshotConflict", None)),
            (400, {"ok": False, "error": {"code": "cursor-expired"}}, ("CursorExpired", None)),
            (418, {"ok": False, "error": {"code": "teapot"}}, ("IntegrityFailure", None)),
            (200, {"ok": False, "error": {"code": "rate-limited"}}, ("IntegrityFailure", None)),
            (500, {"ok": True, "result": snapshot_result(B1)}, ("IntegrityFailure", None)),
            (200, b'{"ok":true,"ok":true}', ("IntegrityFailure", None)),
            (200, b'{"ok":true,"result":1.5}', ("IntegrityFailure", None)),
            (200, b'{"ok":true,"result":NaN}', ("IntegrityFailure", None)),
        ):
            with self.subTest(body=body):
                self.gateway.reply(status, body)
                self.assertRefused(*expected, self.views.snapshot, B1.market)
        self.gateway.reply(200, {"ok": True, "result": snapshot_result(B1)}, "text/plain")
        self.assertRefused("IntegrityFailure", None, self.views.snapshot, B1.market)

    def test_epoch_history_is_contiguous_and_source_presence_follows_status(self) -> None:
        self.gateway.publish(B1, [])
        self.gateway.views[B1.market.hex()]["epochs"] = ("available", [
            {"epoch": "1", "status": "retained-terminal", "snapshot_id": hx(b"\x61" * 32)},
            {"epoch": "2", "status": "retained", "snapshot_id": hx(b"\x62" * 32)},
            {"epoch": "3", "status": "never-opened", "snapshot_id": None},
            {"epoch": "4", "status": "archive-unavailable", "snapshot_id": hx(b"\x64" * 32)},
        ])
        self.assertEqual(self.views.epochs(B1.market, start=2, limit=3), EpochPage(Availability.AVAILABLE, (
            EpochEntry(2, EpochStatus.RETAINED, b"\x62" * 32), EpochEntry(3, EpochStatus.NEVER_OPENED, None),
            EpochEntry(4, EpochStatus.ARCHIVE_UNAVAILABLE, b"\x64" * 32))))
        self.assertEqual(self.gateway.requests[-1][1], {"from": "2", "limit": "3"})
        for entries in ([{"epoch": "3", "status": "retained", "snapshot_id": hx(b"\x63" * 32)}],
                        [{"epoch": "2", "status": "never-opened", "snapshot_id": hx(b"\x62" * 32)}],
                        [{"epoch": "2", "status": "retained", "snapshot_id": None}]):
            with self.subTest(entries=entries):
                self.gateway.reply(200, {"ok": True, "result": {"component": "available", "entries": entries}})
                self.assertRefused("IntegrityFailure", None, self.views.epochs, B1.market, start=2)
        self.gateway.reply(200, {"ok": True, "result": {"component": "not-enabled", "entries": []}})
        self.assertEqual(self.views.epochs(B1.market), EpochPage(Availability.NOT_ENABLED, ()))
        self.gateway.reply(200, {"ok": True, "result": {"component": "not-enabled", "entries": [
            {"epoch": "0", "status": "never-opened", "snapshot_id": None}]}})
        self.assertRefused("IntegrityFailure", None, self.views.epochs, B1.market)


class ParticipantViewTests(ViewTestCase):
    def test_a01_pages_stay_on_one_snapshot_and_cursors_are_bound_to_filter_and_principal(self) -> None:
        w1, w2, w3 = b"\x21" * 32, b"\x22" * 32, b"\x23" * 32
        self.gateway.publish(B1, [row_json("worker", w2), row_json("worker", w1)])
        first = self.views.participants(B1.market, limit=1)
        self.assertEqual((first.snapshot_id.hex(), [row.id for row in first.rows]), (B1_ID, [w1]))
        self.assertEqual(len(first.cursor or ""), 352)
        self.gateway.publish(B2, [row_json("worker", w1), row_json("worker", w2), row_json("worker", w3)])
        second = self.views.next_page(first)
        assert second is not None
        self.assertEqual((second.snapshot_id.hex(), second.binding, [row.id for row in second.rows], second.cursor),
                         (B1_ID, B1, [w2], None))
        self.assertEqual(self.gateway.requests[-1][1], {"snapshot": B1_ID, "kind": "all", "active_only": "false",
                                                        "limit": "1", "cursor": first.cursor})
        self.assertIsNone(self.views.next_page(second))
        latest = self.views.participants(B1.market, limit=MAX_PAGE_ROWS)
        self.assertEqual((latest.snapshot_id.hex(), [row.id for row in latest.rows]), (B2_ID, [w1, w2, w3]))

        again = self.views.participants(B1.market, limit=1)
        requests = len(self.gateway.requests)
        for changes in ({"kind": KindFilter.WORKER}, {"active_only": True}, {"snapshot": bytes.fromhex(B1_ID)}):
            with self.subTest(changes=changes):
                self.assertRefused("CursorMismatch", None, self.views.participants, B1.market, limit=1,
                                   cursor=again.cursor, **changes)
        self.assertRefused("CursorMismatch", None, self.views.participants, b"\x4e" * 32, limit=1, cursor=again.cursor)
        self.assertEqual(len(self.gateway.requests), requests)
        other = AiMarketViews(self.gateway.endpoint, credential=credential("b"), timeout=15)
        self.assertRefused("CursorMismatch", None, other.participants, B1.market, limit=1, cursor=again.cursor)
        self.assertEqual(len(self.gateway.requests), requests + 1)
        following = self.views.next_page(again)
        assert following is not None
        self.assertEqual((following.snapshot_id.hex(), [row.id for row in following.rows]), (B2_ID, [w2]))
        self.assertRefused("CursorMismatch", None, self.views.participants, B1.market, limit=1, cursor=again.cursor)

    def test_a03_scores_and_rewards_keep_explicit_absence(self) -> None:
        worker_w, worker_x = b"\x57" * 32, b"\x58" * 32
        maximum = str((1 << 128) - 1)
        self.gateway.publish(B1, [
            row_json("worker", worker_w, score={"status": "present", "epoch": "7", "ppm": 0},
                     reward={"status": "available", "asset": hx(ASSET), "earned": maximum, "claimed": "0"},
                     history={"status": "available", "digest": hx(b"\x68" * 32)}),
            row_json("worker", worker_x, eligibility=0,
                     score={"status": "insufficient-coverage", "epoch": "7", "ppm": None},
                     reward={"status": "available", "asset": hx(ASSET), "earned": "10", "claimed": "10"}),
        ])
        page = self.views.participants(B1.market)
        w, x = page.rows
        self.assertEqual((w.id, w.score, w.reward, w.history_status, w.history, w.active),
                         (worker_w, Score(ScoreStatus.PRESENT, 7, 0),
                          Reward(Availability.AVAILABLE, ASSET, (1 << 128) - 1, 0), Availability.AVAILABLE,
                          b"\x68" * 32, True))
        self.assertEqual((x.id, x.score, x.reward, x.history_status, x.history, x.active),
                         (worker_x, Score(ScoreStatus.INSUFFICIENT_COVERAGE, 7, None),
                          Reward(Availability.AVAILABLE, ASSET, 10, 10), Availability.NOT_YET_PRODUCED, None, False))
        self.assertEqual([row.id for row in self.views.participants(B1.market, active_only=True).rows], [worker_w])

        unfunded = replace(B1, market=b"\x4b" * 32)
        self.gateway.publish(unfunded, [row_json(
            "worker", worker_w, score={"status": "present", "epoch": "7", "ppm": 0},
            reward={"status": "not-enabled", "asset": None, "earned": None, "claimed": None})],
            {**COMPONENTS, "F06": "not-enabled"})
        absent = self.views.participants(unfunded.market)
        self.assertEqual(absent.components[5], Availability.NOT_ENABLED)
        self.assertEqual(absent.rows[0].reward, Reward(Availability.NOT_ENABLED, None, None, None))

        for score, reward in (
            ({"status": "present", "epoch": "7", "ppm": None}, None),
            ({"status": "present", "epoch": None, "ppm": 5}, None),
            ({"status": "present", "epoch": "7", "ppm": 1_000_001}, None),
            ({"status": "not-produced", "epoch": None, "ppm": 0}, None),
            ({"status": "present", "epoch": "07", "ppm": 1}, None),
            (None, {"status": "not-enabled", "asset": None, "earned": "0", "claimed": None}),
            (None, {"status": "available", "asset": hx(ASSET), "earned": "10", "claimed": "11"}),
            (None, {"status": "available", "asset": hx(ASSET), "earned": str(1 << 128), "claimed": "0"}),
            (None, {"status": "available", "asset": hx(bytes(32)), "earned": "1", "claimed": "0"}),
        ):
            with self.subTest(score=score, reward=reward):
                self.gateway.reply(200, {"ok": True, "result": page_result(unfunded, [
                    row_json("worker", worker_w, score=score, reward=reward)])})
                self.assertRefused("IntegrityFailure", None, self.views.participants, unfunded.market)

    def test_a04_bounded_pages_kind_order_and_refusals_before_any_read(self) -> None:
        market = b"\x4e" * 32
        binding = replace(B1, market=market)
        workers = [b"\x30" + index.to_bytes(31, "big") for index in range(1, 33)]
        evaluators = [b"\x10" + index.to_bytes(31, "big") for index in range(1, 9)]
        rows = [row_json("evaluator", ident) for ident in reversed(evaluators)]
        rows += [row_json("worker", ident) for ident in reversed(workers)]
        self.gateway.publish(binding, rows)
        first = self.views.participants(market, limit=32)
        self.assertEqual([(row.kind, row.id) for row in first.rows], [(ParticipantKind.WORKER, ident) for ident in workers])
        second = self.views.next_page(first)
        assert second is not None
        self.assertEqual([(row.kind, row.id) for row in second.rows],
                         [(ParticipantKind.EVALUATOR, ident) for ident in evaluators])
        self.assertIsNone(second.cursor)
        evaluators_only = self.views.participants(market, kind=KindFilter.EVALUATOR, limit=8)
        self.assertEqual(([row.id for row in evaluators_only.rows], evaluators_only.cursor), (evaluators, None))
        self.assertEqual(self.gateway.requests[-1][1]["kind"], "evaluator")

        requests = len(self.gateway.requests)
        for limit in (0, 33, -1, True, "01", 1.0):
            with self.subTest(limit=limit), self.assertRaises(ValueError):
                self.views.participants(market, limit=limit)  # type: ignore[arg-type]
        for bad_market in (market + b"\x4e", bytes(32), market.hex()):
            with self.subTest(market=bad_market), self.assertRaises(ValueError):
                self.views.participants(bad_market)  # type: ignore[arg-type]
        with self.assertRaises(ValueError):
            self.views.participants(market, snapshot=b"\x01" * 31)
        for cursor in ("AB" * 176, "abc", "", "zz" * 176):
            with self.subTest(cursor=cursor), self.assertRaises(ValueError):
                self.views.participants(market, cursor=cursor)
        self.assertRefused("ResponseTooLarge", None, self.views.participants, market, cursor="ab" * 513)
        with self.assertRaises(ValueError):
            self.views.epochs(market, limit=33)
        self.assertEqual(len(self.gateway.requests), requests)

        issued = self.views.participants(market, limit=32).cursor or ""
        forged = issued[:-2] + ("01" if issued[-2:] != "01" else "02")
        self.assertRefused("CursorMismatch", None, self.views.participants, market, limit=32, cursor=forged)
        self.assertRefused("CursorMismatch", None, self.views.participants, market, limit=32,
                           cursor=secrets.token_hex(176))

        inactive = replace(B1, market=b"\x4f" * 32)
        self.gateway.publish(inactive, [row_json("worker", ident, eligibility=0) for ident in workers[:3]])
        empty = self.views.participants(inactive.market, active_only=True)
        self.assertEqual((empty.rows, empty.cursor), ((), None))

    def test_pages_that_break_order_filter_bounds_or_snapshot_are_refused(self) -> None:
        w1, w2 = b"\x21" * 32, b"\x22" * 32
        self.gateway.publish(B1, [row_json("worker", w1), row_json("worker", w2)])
        for rows, cursor, kwargs in (
            ([row_json("worker", w2), row_json("worker", w1)], None, {}),
            ([row_json("worker", w1), row_json("worker", w1)], None, {}),
            ([row_json("evaluator", w1)], None, {"kind": KindFilter.WORKER}),
            ([row_json("worker", w1, eligibility=2)], None, {"active_only": True}),
            ([row_json("worker", w1), row_json("worker", w2)], None, {"limit": 1}),
            ([row_json("worker", w1)], "ab" * 176, {"limit": 2}),
            ([row_json("worker", w1)], "AB" * 176, {"limit": 1}),
        ):
            with self.subTest(rows=rows, cursor=cursor, kwargs=kwargs):
                self.gateway.reply(200, {"ok": True, "result": page_result(B1, rows, cursor)})
                self.assertRefused("IntegrityFailure", None, self.views.participants, B1.market, **kwargs)
        first = self.views.participants(B1.market, limit=1)
        self.gateway.reply(200, {"ok": True, "result": page_result(B2, [row_json("worker", w2)])})
        self.assertRefused("SnapshotConflict", None, self.views.next_page, first)
        first = self.views.participants(B1.market, limit=1)
        self.gateway.reply(200, {"ok": True, "result": page_result(B1, [row_json("worker", w1)])})
        self.assertRefused("IntegrityFailure", None, self.views.next_page, first)


def native_terms(owner: bytes, capabilities: bytes, access: bytes) -> NativeTerms:
    return NativeTerms(7, b"did:lxp:ai-market-owner", owner, 1, b"\x0e" * 32, 1, 100, 1_000_000, capabilities, access,
                       16, RESOURCES)


def fixture_call() -> NativeProgramCall:
    return decode_native_program_call(_signed_call_envelope((MULTICALL / "activity-0").read_bytes()).payload)


class ApprovalTests(ViewTestCase):
    def setUp(self) -> None:
        super().setUp()
        self.gateway.publish(B1, [])
        self.snapshot = self.views.snapshot(B1.market)
        self.signer = Ed25519KeySigner()
        call = fixture_call()
        self.terms = native_terms(self.signer.public_key(), call.capabilities, call.access_declaration)
        self.claim = OperationRequest(CLAIM, WORKER + PAYEE + (10).to_bytes(16, "big"), ACTOR, 0, 1300, REQUEST, True,
                                      ASSET)

    def test_a07_prepare_refuses_stale_authority_unfinalized_reads_and_program_preview_codes(self) -> None:
        prepare = self.snapshot.prepare
        self.assertRefused("StaleAuthority", 9, prepare, 1209, self.claim, self.terms)
        self.assertRefused("FinalityUnavailable", None, prepare, 1200, replace(self.claim, required_rank=5), self.terms)
        self.assertApplication(NON_CANONICAL, prepare, 1200, replace(self.claim, required_rank=3), self.terms)
        self.assertRefused("NotMutation", None, prepare, 1200,
                           OperationRequest(0x0A01, b"", ACTOR, 0, 1300, REQUEST, False), self.terms)
        self.assertApplication(UNKNOWN_OPERATION, prepare, 1200, replace(self.claim, operation=0x0208), self.terms)
        self.assertApplication(F06_NOTHING_TO_CLAIM, prepare, 1200,
                               replace(self.claim, payload=WORKER + PAYEE + bytes(16)), self.terms)
        self.assertApplication(NON_CANONICAL, prepare, 1200, replace(self.claim, sequence=1), self.terms)
        self.assertApplication(WRONG_ROSTER, prepare, 1200, replace(self.claim, roster_bound=False), self.terms)
        fund = OperationRequest(FUND, FUND_PAYLOAD, ACTOR, 4, 1300, REQUEST, False, ASSET)
        self.assertApplication(F06_CONTRIBUTION_CONSENT_REQUIRED, prepare, 1200,
                               replace(fund, payload=FUND_PAYLOAD[:-1] + b"\x00"), self.terms)
        self.assertApplication(F06_FUNDING_POLICY_MISMATCH, prepare, 1200,
                               replace(fund, payload=FUND_PAYLOAD[:48] + struct.pack(">Q", 2) + b"\x01"), self.terms)
        self.assertApplication(F06_INVALID_AMOUNT, prepare, 1200, replace(fund, payload=bytes(16) + FUND_PAYLOAD[16:]),
                               self.terms)
        self.assertApplication(NON_CANONICAL, prepare, 1200, replace(fund, payload=FUND_PAYLOAD[:-1] + b"\x02"),
                               self.terms)
        with self.assertRaises(ValueError):
            prepare(1200, replace(self.claim, asset=None), self.terms)
        self.gateway.publish(B2, [])
        unbound = self.views.snapshot(B1.market)
        self.assertRefused("FinalityUnavailable", None, unbound.prepare, 1200, replace(self.claim, required_rank=5),
                           self.terms)
        self.assertApplication(WRONG_ROSTER, unbound.prepare, 1200, self.claim, self.terms)

    def test_a07_claim_is_reviewed_approved_and_signed_as_a_native_program_call(self) -> None:
        prepared = self.snapshot.prepare(1200, self.claim, self.terms)
        review = prepared.review()
        terms = review.terms
        self.assertEqual(terms.effect, Effect(CLAIM, amount=10, asset=ASSET, payee=PAYEE, worker=WORKER))
        self.assertEqual((terms.actor, terms.market, terms.snapshot, terms.authority, terms.fee_limit),
                         (ACTOR, B1.market, bytes.fromhex(B1_ID), self.signer.public_key(), 1_000_000))
        self.assertEqual(terms.commitment, sha256(b"LXP/v1/signature-preimage\0" + prepared.canonical).digest())
        for field, altered in (
            ("amount", replace(terms, effect=replace(terms.effect, amount=11))),
            ("actor", replace(terms, actor=b"\xa2" * 32)),
            ("market", replace(terms, market=b"\x4e" * 32)),
            ("validity", replace(terms, not_after=101)),
            ("access_declaration", replace(terms, access_declaration=terms.access_declaration + b"\x00")),
            ("response_capacity", replace(terms, response_capacity=17)),
            ("fee_limit", replace(terms, fee_limit=1_000_001)),
        ):
            with self.subTest(field=field):
                self.assertRefused("ReviewMismatch", field, review.approve, altered, self.signer.public_key())
        self.assertRefused("UnauthorizedKey", None, review.approve, terms, Ed25519KeySigner().public_key())
        record = review.approve(terms, self.signer.public_key()).sign(self.signer, VERIFIER, 1200)
        activity = _signed_call_envelope(record.signed_bytes)
        call = decode_native_program_call(activity.payload)
        self.assertEqual((call.guest_abi, call.entrypoint, call.program_id, decode_envelope(call.calldata).selector),
                         (GUEST_ABI, ENTRYPOINT, B1.program, CLAIM))
        self.assertEqual((record.state, record.attempt, record.intent), (SdkState.SIGNED, 0, prepared.intent))


class ApprovalGateTests(ViewTestCase):
    def setUp(self) -> None:
        super().setUp()
        self.signer = Ed25519KeySigner()
        call = fixture_call()
        self.terms = native_terms(self.signer.public_key(), call.capabilities, call.access_declaration)
        envelope = claim_envelope(payload=WORKER + PAYEE + (10).to_bytes(16, "big"))
        self.effect = Effect(CLAIM, amount=10, asset=ASSET, payee=PAYEE, worker=WORKER)
        legacy_call = encode_native_program_call(NativeProgramCall(
            B1.program, 1, ENTRYPOINT, envelope.encode(), call.capabilities, call.access_declaration, 16, RESOURCES))
        self.canonical = _unsigned_activity(self.terms, legacy_call)
        self.prepared = PreparedOperation(self.canonical, B1, bytes.fromhex(B1_ID), envelope.request_digest(), self.effect)
        self.approved = ApprovalTerms(
            CLAIM, B1.chain, B1.program, B1.market, ACTOR, 3, 2, B1.roster, B1.policy, bytes.fromhex(B1_ID), self.effect,
            call.capabilities, call.access_declaration, 16, RESOURCES, 1_000_000, 1, 100, 1300, b"\x0e" * 32,
            self.signer.public_key(), sha256(b"LXP/v1/signature-preimage\0" + self.canonical).digest())

    def test_review_refuses_a_call_that_is_not_the_ai_market_native_call(self) -> None:
        self.assertRefused("NotNativeProgramCall", None, self.prepared.review)

    def test_first_difference_reports_the_earliest_schema_field(self) -> None:
        base = self.approved
        self.assertIsNone(first_difference(base, base))
        for field, altered in (
            ("action", replace(base, action=FUND)),
            ("chain", replace(base, chain=b"\xc2" * 32)),
            ("epoch", replace(base, epoch=4)),
            ("roster", replace(base, roster=None)),
            ("policy", replace(base, policy=b"\x91" * 32)),
            ("snapshot", replace(base, snapshot=bytes.fromhex(B2_ID))),
            ("amount", replace(base, effect=replace(self.effect, amount=11))),
            ("asset", replace(base, effect=replace(self.effect, asset=b"\xa6" * 32))),
            ("payee", replace(base, effect=replace(self.effect, payee=b"\x23" * 32))),
            ("worker", replace(base, effect=replace(self.effect, worker=b"\x12" * 32))),
            ("capabilities", replace(base, capabilities=b"")),
            ("resources", replace(base, resources=RESOURCES[:-1] + (4097,))),
            ("validity", replace(base, not_before=2)),
            ("expiry", replace(base, expiry=1301)),
            ("idempotency_key", replace(base, idempotency_key=b"\x0f" * 32)),
            ("authority", replace(base, authority=b"\x01" * 32)),
            ("commitment", replace(base, commitment=b"\x02" * 32)),
            ("actor", replace(base, actor=b"\xa2" * 32, effect=replace(self.effect, amount=11))),
        ):
            with self.subTest(field=field):
                self.assertEqual(first_difference(base, altered), field)

    def test_approval_binds_the_owner_key_freshness_and_a_verified_signature(self) -> None:
        review = Review(self.prepared, self.approved)
        self.assertRefused("ReviewMismatch", "amount", review.approve,
                           replace(self.approved, effect=replace(self.effect, amount=11)), self.signer.public_key())
        self.assertRefused("UnauthorizedKey", None, review.approve, self.approved, Ed25519KeySigner().public_key())
        approval = review.approve(self.approved, self.signer.public_key())
        self.assertRefused("UnauthorizedKey", None, approval.sign, Ed25519KeySigner(), VERIFIER, 1200)
        self.assertRefused("StaleAuthority", 9, approval.sign, self.signer, VERIFIER, 1209)
        zero = ZeroSigner()
        zero_review = Review(self.prepared, replace(self.approved, authority=zero.public_key()))
        self.assertRefused("Signature", None, zero_review.approve(zero_review.terms, zero.public_key()).sign, zero,
                           VERIFIER, 1200)
        record = approval.sign(self.signer, VERIFIER, 1200)
        signature = record.signed_bytes[-64:]
        self.assertEqual(record.signed_bytes, self.canonical[:4] + b"\x0c" + self.canonical[5:] + b"\x0c"
                         + struct.pack(">I", 64) + signature)
        self.assertTrue(VERIFIER.verify_ed25519(self.signer.public_key(), signature, self.approved.commitment))
        self.assertEqual((record.state, record.domain, record.attempt, record.network_id, record.not_after,
                          record.idempotency_key, record.intent),
                         (SdkState.SIGNED, DomainStatus.PENDING, 0, 7, 100, b"\x0e" * 32, self.prepared.intent))
        self.assertEqual(record.activity_id, sha256(b"LXP/v1/activity-id\0" + record.signed_bytes).digest())
        self.assertEqual(OperationRecord.decode(record.encode(), VERIFIER), record)


class ProgramEndpoint:
    def __init__(self, actions: list[str]) -> None:
        self.actions = actions
        self.bodies: list[tuple[bytes, str | None]] = []
        self.http = _Http(handle_post=self.handle)

    def close(self) -> None:
        self.http.close()

    def operations(self, network_id: int = 7) -> ProgramOperations:
        return ProgramOperations(
            ProductionClient(AgentHttpTransport(self.http.endpoint, timeout=15)), VERIFIER,
            ProgramTrustContext((MULTICALL / "sequencer.public").read_bytes(), protocol_version=3, network_id=network_id))

    def handle(self, handler: BaseHTTPRequestHandler) -> None:
        body = handler.rfile.read(int(handler.headers["Content-Length"]))
        key = handler.headers.get("Idempotency-Key")
        self.bodies.append((body, key))
        action = self.actions.pop(0)
        if action == "drop":
            handler.close_connection = True
            return
        if action == "unknown":
            send_json(handler, 200, {
                "request_id": "r1",
                "value": {"state": "unknown", "activity_id": sha256(b"LXP/v1/activity-id\0" + body).hexdigest(),
                          "idempotency_key": key},
                "verification_status": {"state": "Unverified", "requested": "SequencerSigned", "achieved": "Unverified",
                                        "reason": "receipt_pending"}})
            return
        send_json(handler, 422, {"class": "CoreRejection", "protocol_result_code": 17, "retriability": "Terminal",
                                 "reason": "refused", "request_id": "r1"})


def receipt_batch(previous: str, resulting: str) -> AuthorizedReceiptBatch:
    return AuthorizedReceiptBatch(
        bytes.fromhex("4449191b1fca9bf266bab78c75b0498afc0094b2b1b4ed63fb91aee3c11246e8"), bytes(32),
        bytes.fromhex(previous), bytes.fromhex(resulting), (MULTICALL / "sequencer.public").read_bytes())


RECEIPT_0 = receipt_batch("ac6b2b4a387db1e32856545c23d456622a1b8d1f6825b06591ccc7abc829256d",
                          "69a397d65515222f1cbfae02e338736777a4d9c6b51e90af8c0ea85d79259f57")
RECEIPT_1 = receipt_batch("69a397d65515222f1cbfae02e338736777a4d9c6b51e90af8c0ea85d79259f57",
                          "a9568e7288c71702535f9028295b523da0973f3ea7532bc86bb3d72196b2284c")


class JournalTests(AiMarketCase):
    def setUp(self) -> None:
        directory = Path(tempfile.mkdtemp())
        self.addCleanup(shutil.rmtree, directory)
        self.journal = OperationJournal(directory / "journal", VERIFIER)
        self.intent = claim_envelope().request_digest()

    def signed(self, name: str) -> OperationRecord:
        record = OperationRecord.signed((MULTICALL / name).read_bytes(), FIXTURE_KEY, self.intent, VERIFIER)
        self.journal.record_signed(record)
        return record

    def on_disk(self, record: OperationRecord) -> OperationRecord:
        return OperationRecord.decode(self.journal.record_path(record.activity_id).read_bytes(), VERIFIER)

    def test_a08_lost_acknowledgment_stays_unknown_until_receipt_and_checkpoint(self) -> None:
        endpoint = ProgramEndpoint(["drop", "unknown"])
        self.addCleanup(endpoint.close)
        operations = endpoint.operations()
        record = self.signed("activity-0")
        signed_bytes = record.signed_bytes
        self.assertEqual(record.activity_id.hex(), "39ac968f609c1b3610e73456504b46dd200feb01408be9fe2db55bf75ace611e")
        self.assertEqual(self.journal.submit(record, operations), SdkState.UNKNOWN)
        self.assertEqual((record.attempt, record.domain, record.signed_bytes), (1, DomainStatus.PENDING, signed_bytes))
        self.assertEqual(endpoint.bodies, [(signed_bytes, (bytes(31) + b"\x03").hex())])
        self.assertEqual(self.on_disk(record), record)
        self.assertEqual(self.journal.resolve(record, None, RECEIPT_0), SdkState.UNKNOWN)
        self.assertRefused("InvalidTransition", SdkState.UNKNOWN, self.journal.submit, record, operations)
        self.assertRefused("InvalidTransition", SdkState.UNKNOWN, self.journal.expire, record, 101)
        self.assertRefused("InvalidTransition", SdkState.UNKNOWN, self.journal.finalize, record, checkpoint_for(MULTICALL))

        self.journal.persist(replace(record, state=SdkState.SUBMITTING))
        recovered = self.journal.load(record.activity_id)
        self.assertEqual((recovered.state, self.on_disk(record).state), (SdkState.UNKNOWN, SdkState.UNKNOWN))
        self.assertEqual(recovered, record)

        self.assertEqual(self.journal.resend_exact(record, operations), SdkState.UNKNOWN)
        self.assertEqual(record.attempt, 2)
        self.assertEqual(endpoint.bodies[1], endpoint.bodies[0])
        self.assertRefused("ReceiptMismatch", None, self.journal.resolve, record,
                           (MULTICALL / "receipt-1").read_bytes(), RECEIPT_1)
        self.assertEqual(self.journal.resolve(record, (MULTICALL / "receipt-0").read_bytes(), RECEIPT_0),
                         SdkState.EXECUTED)
        self.assertEqual((record.result_code, record.global_sequence, record.checkpoint, record.domain),
                         (0, 5, None, DomainStatus.PENDING))
        self.assertRefused("FinalityMismatch", None, self.journal.finalize, record, checkpoint_for(CUSTODY))
        verification = checkpoint_for(MULTICALL)
        self.assertEqual(self.journal.finalize(record, verification), SdkState.FINALIZED)
        self.assertEqual((record.checkpoint, record.domain), (verification.checkpoint_id, DomainStatus.PENDING))
        self.assertEqual(self.journal.load(record.activity_id), record)
        self.assertEqual(record.signed_bytes, signed_bytes)

    def test_records_refuse_corruption_and_foreign_keys(self) -> None:
        record = self.signed("activity-0")
        record.state, record.attempt, record.result_code, record.global_sequence = SdkState.EXECUTED, 1, 0, 5
        encoded = record.encode()
        self.assertEqual(OperationRecord.decode(encoded, VERIFIER), record)
        for corrupt in (
            encoded[:-1] + bytes([encoded[-1] ^ 1]),
            encoded + b"\x00",
            encoded[:8] + b"\x00" + encoded[9:],
            encoded[:12] + struct.pack(">I", 8) + encoded[16:],
            replace(record, state=SdkState.FINALIZED).encode(),
            replace(record, result_code=1).encode(),
            replace(record, state=SdkState.SIGNED).encode(),
            replace(record, state=SdkState.PREPARED).encode(),
            b"PAXAIOP2" + encoded[8:],
        ):
            with self.subTest(corrupt=corrupt[:16]):
                self.assertRefused("CorruptRecord", None, OperationRecord.decode, corrupt, VERIFIER)
        self.assertRefused("CorruptRecord", None, OperationRecord.signed, record.signed_bytes, b"\x01" * 32,
                           self.intent, VERIFIER)

    def test_pre_send_refusal_restores_and_terminal_core_refusal_fails_on_first_attempt(self) -> None:
        endpoint = ProgramEndpoint(["refuse"])
        self.addCleanup(endpoint.close)
        record = self.signed("activity-1")
        with self.assertRaises(ValueError):
            self.journal.submit(record, endpoint.operations(network_id=8))
        self.assertEqual((record.state, record.attempt, endpoint.bodies), (SdkState.SIGNED, 0, []))
        self.assertEqual(self.on_disk(record), record)
        self.assertEqual(self.journal.submit(record, endpoint.operations()), SdkState.FAILED)
        self.assertEqual((record.attempt, record.result_code, record.global_sequence, record.checkpoint),
                         (1, 17, None, None))
        self.assertEqual(self.journal.load(record.activity_id), record)
        self.assertRefused("InvalidTransition", SdkState.FAILED, self.journal.resend_exact, record, endpoint.operations())

    def test_signed_records_fail_only_after_their_validity_window(self) -> None:
        record = self.signed("activity-0")
        self.assertEqual(self.journal.expire(record, 100), SdkState.SIGNED)
        self.assertEqual(self.journal.expire(record, 101), SdkState.FAILED)
        self.assertEqual(self.on_disk(record).state, SdkState.FAILED)
        self.assertRefused("InvalidTransition", SdkState.FAILED, self.journal.resolve, record, None, RECEIPT_0)


if __name__ == "__main__":
    unittest.main()
