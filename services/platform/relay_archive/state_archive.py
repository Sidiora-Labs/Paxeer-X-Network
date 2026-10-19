"""Native state archive: immutable inventories, 2-of-2 availability certificates and restoration."""
from __future__ import annotations

import json
import os
import re
import shutil
import socket
import sqlite3
import ssl
import stat
import struct
import sys
import threading
import urllib.error
import urllib.request
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from typing import Any, Callable, Mapping, Sequence

from cryptography.exceptions import InvalidSignature
from cryptography.hazmat.primitives import serialization
from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey, Ed25519PublicKey

if __package__ in (None, ""):
    sys.path.insert(0, str(Path(__file__).resolve().parent))
    from protocol import (
        CodecError,
        IntegrityError,
        MissingBatch,
        NativeCodec,
        ProtocolError,
        RelayArchiveError,
        RelayConfig,
        StateArchiveRefusal,
        canonical_json_bytes,
        decode_hex,
        require_decimal,
        sha256_hex,
    )
    from store import StateInventoryStore
else:
    from .protocol import (
        CodecError,
        IntegrityError,
        MissingBatch,
        NativeCodec,
        ProtocolError,
        RelayArchiveError,
        RelayConfig,
        StateArchiveRefusal,
        canonical_json_bytes,
        decode_hex,
        require_decimal,
        sha256_hex,
    )
    from .store import StateInventoryStore

MANIFEST_DOMAIN = "layerx/paxai/archive-unit/v1"
CERTIFICATE_DOMAIN = "layerx/paxai/archive-availability/v1"
PROFILE_DOMAIN = "layerx/paxai/storage-profile/v1"
SEGMENT_KINDS = ("genesis_manifest", "snapshot", "batch", "log", "witness", "metadata_proof", "node_file")
FILE_KINDS = ("genesis_manifest", "snapshot", "log", "witness", "node_file")
ARCHIVE_ROOTS = ("checkpoints", "genesis", "logs", "replica")
GENESIS_MANIFEST_PATH = "genesis/genesis.manifest"
GENESIS_SNAPSHOT_PATH = "genesis/00000000000000000000.lxs"
AVAILABILITY_LOG_PATH = "checkpoints/da-bodies.log"
CHECKPOINT_NAME = re.compile(r"^checkpoints/([0-9]{20})\.lxs$")
PATH_PART = re.compile(r"^(?!\.\.?$)[A-Za-z0-9._][A-Za-z0-9._-]{0,127}$")
MAX_SEGMENTS = 4096
MAX_DIRECTORIES = 1024
MAX_SEGMENT_BYTES = 1 << 30
MAX_UNIT_BYTES = 1 << 34
MAX_MANIFEST_BYTES = 16 << 20
MAX_PROOF_PART_BYTES = 16 << 20
U64_LIMIT = 1 << 64
PROOF_MAGIC = b"LXPB"
PROOF_KINDS = (1, 3)
EVIDENCE_WIRE_VERSION = 1
LXS_HEADER_BYTES = {b"LXS2": 116, b"LXS3": 116 + 315}
LNI_MAJOR = 1
LNI_MINOR = 8
LNI_FIXED_BYTES = 22
LNI_MAX_FRAME_BYTES = 64 << 20
LNI_NODE_INFO_REQUEST = 1
LNI_NODE_INFO_RESPONSE = 2
LNI_SUBMIT_REQUEST = 3
LNI_SUBMIT_RESPONSE = 4
LNI_MAX_ACTIVITY_BYTES = 1 << 20
LNI_PROOF_BUNDLE_REQUEST = 16
LNI_PROOF_BUNDLE_RESPONSE = 17
LNI_ERROR_RESPONSE = 25
HTTP_STATUS = {"malformed": 400, "policy": 403, "missing": 404, "conflict": 409, "corrupt": 422,
               "unavailable": 503}


def _refuse(code: str, message: str) -> StateArchiveRefusal:
    return StateArchiveRefusal(code, message)


def _hex32(value: Any, name: str) -> str:
    if not isinstance(value, str) or re.fullmatch(r"[0-9a-f]{64}", value) is None:
        raise _refuse("malformed", f"{name} must be 32-byte lowercase hexadecimal")
    return value


def _uint(value: Any, name: str, maximum: int = U64_LIMIT - 1) -> int:
    if isinstance(value, bool) or not isinstance(value, int) or not 0 <= value <= maximum:
        raise _refuse("malformed", f"{name} must be an integer from 0 through {maximum}")
    return value


def _fields(value: Any, expected: set[str], name: str) -> Mapping[str, Any]:
    if not isinstance(value, dict) or set(value) != expected:
        raise _refuse("malformed", f"{name} must contain exactly {', '.join(sorted(expected))}")
    return value


def profile_digest(profile: Mapping[str, Any]) -> str:
    if profile.get("domain") != PROFILE_DOMAIN:
        raise _refuse("malformed", "storage profile domain is not the supported version")
    return sha256_hex(canonical_json_bytes(dict(profile)))


def manifest_digest(manifest: Mapping[str, Any]) -> str:
    return sha256_hex(canonical_json_bytes(dict(manifest)))


def inventory_root(segments: Sequence[Mapping[str, Any]], directories: Sequence[Mapping[str, Any]]) -> str:
    leaves = [sha256_hex(b"\x02" + canonical_json_bytes(dict(entry))) for entry in directories]
    leaves += [sha256_hex(b"\x00" + canonical_json_bytes(dict(entry))) for entry in segments]
    level = [bytes.fromhex(leaf) for leaf in leaves]
    while len(level) > 1:
        if len(level) % 2:
            level.append(level[-1])
        level = [bytes.fromhex(sha256_hex(b"\x01" + level[index] + level[index + 1]))
                 for index in range(0, len(level), 2)]
    return level[0].hex()


def _safe_path(value: Any, name: str) -> str:
    if not isinstance(value, str) or not value:
        raise _refuse("malformed", f"{name} must be a non-empty relative path")
    parts = value.split("/")
    if parts[0] not in ARCHIVE_ROOTS or any(PATH_PART.fullmatch(part) is None for part in parts):
        raise _refuse("malformed", f"{name} is not a safe archived node path")
    return value


def _segment_sort_key(segment: Mapping[str, Any]) -> tuple[Any, ...]:
    kind = segment["kind"]
    if kind == "batch":
        return (1, segment["first"], "", "")
    if kind == "snapshot":
        return (2, segment["first"], "", "")
    if kind == "metadata_proof":
        return (4, segment["first"], segment["subject"], segment["sha256"])
    return (3, 0, segment["path"], "")


def validate_manifest(manifest: Any) -> str:
    """Validate a unit manifest's canonical structure and bounds; return its immutable digest."""
    manifest = _fields(manifest, {"domain", "network_id", "genesis_sha256", "sequencer_id",
                                  "sequencer_public_key", "profile_digest", "genesis", "head",
                                  "checkpoint", "directories", "segments", "inventory_root"}, "manifest")
    if manifest["domain"] != MANIFEST_DOMAIN:
        raise _refuse("malformed", "manifest domain is not the supported version")
    _uint(manifest["network_id"], "network_id", (1 << 32) - 1)
    for name in ("genesis_sha256", "sequencer_id", "sequencer_public_key", "profile_digest", "inventory_root"):
        _hex32(manifest[name], name)
    genesis = _fields(manifest["genesis"], {"state_root", "receipt_state_root", "snapshot_digest",
                                            "protocol_version"}, "genesis")
    for name in ("state_root", "receipt_state_root", "snapshot_digest"):
        _hex32(genesis[name], f"genesis.{name}")
    _uint(genesis["protocol_version"], "genesis.protocol_version", 0xFFFF)
    head = _fields(manifest["head"], {"global_sequence", "batch_number", "receipt_state_root"}, "head")
    _uint(head["global_sequence"], "head.global_sequence")
    _uint(head["batch_number"], "head.batch_number")
    _hex32(head["receipt_state_root"], "head.receipt_state_root")
    checkpoint = manifest["checkpoint"]
    if checkpoint is not None:
        checkpoint = _fields(checkpoint, {"global_sequence", "canonical_state_root", "receipt_state_root",
                                          "snapshot_digest"}, "checkpoint")
        if not 1 <= _uint(checkpoint["global_sequence"], "checkpoint.global_sequence") <= head["global_sequence"]:
            raise _refuse("malformed", "checkpoint sequence lies outside the archived head range")
        for name in ("canonical_state_root", "receipt_state_root", "snapshot_digest"):
            _hex32(checkpoint[name], f"checkpoint.{name}")
    directories = manifest["directories"]
    if not isinstance(directories, list) or not 1 <= len(directories) <= MAX_DIRECTORIES:
        raise _refuse("malformed", "directory inventory size is out of bounds")
    directory_paths = []
    for entry in directories:
        entry = _fields(entry, {"path", "mode"}, "directory")
        directory_paths.append(_safe_path(entry["path"], "directory.path"))
        _uint(entry["mode"], "directory.mode", 0o777)
    if directory_paths != sorted(set(directory_paths)):
        raise _refuse("malformed", "directory inventory is not sorted and unique")
    if any("/" in path and path.rsplit("/", 1)[0] not in directory_paths for path in directory_paths):
        raise _refuse("malformed", "directory inventory omits a parent directory")
    segments = manifest["segments"]
    if not isinstance(segments, list) or not 2 <= len(segments) <= MAX_SEGMENTS:
        raise _refuse("malformed", "segment inventory size is out of bounds")
    paths: set[str] = set()
    total = 0
    batches = 0
    checkpoints: list[int] = []
    for index, segment in enumerate(segments):
        segment = _fields(segment, {"index", "kind", "path", "mode", "sha256", "length", "first", "last",
                                    "subject"}, "segment")
        kind = segment["kind"]
        if segment["index"] != index or kind not in SEGMENT_KINDS or (kind == "genesis_manifest") != (index == 0):
            raise _refuse("malformed", f"segment {index} has an invalid index or kind")
        _hex32(segment["sha256"], "segment.sha256")
        length = _uint(segment["length"], "segment.length", MAX_SEGMENT_BYTES)
        total += length
        first, last = _uint(segment["first"], "segment.first"), _uint(segment["last"], "segment.last")
        if first != last:
            raise _refuse("malformed", f"segment {index} must name exactly one batch or sequence")
        if kind in FILE_KINDS:
            path = _safe_path(segment["path"], "segment.path")
            if path in paths or path.rsplit("/", 1)[0] not in directory_paths:
                raise _refuse("malformed", f"segment {index} path repeats or lacks its directory")
            paths.add(path)
            _uint(segment["mode"], "segment.mode", 0o777)
        elif segment["path"] != "" or segment["mode"] != 0:
            raise _refuse("malformed", f"segment {index} is not a node file and cannot carry a path")
        if kind in ("snapshot", "batch", "metadata_proof") and length == 0:
            raise _refuse("malformed", f"segment {index} must not be empty")
        if kind == "metadata_proof":
            _hex32(segment["subject"], "segment.subject")
            if first == 0:
                raise _refuse("malformed", "metadata proof must name an activity sequence")
        elif segment["subject"] != "":
            raise _refuse("malformed", f"segment {index} carries a subject it cannot bind")
        if kind == "batch":
            batches += 1
            if first != batches:
                raise _refuse("missing", "canonical batch segments are not contiguous from the first batch")
        if kind == "snapshot" and index > 1:
            match = CHECKPOINT_NAME.fullmatch(segment["path"])
            if match is None or int(match.group(1)) != first or first == 0:
                raise _refuse("malformed", "checkpoint snapshot path does not name its sequence")
            checkpoints.append(first)
        if kind in ("genesis_manifest", "log", "witness", "node_file") and first != 0:
            raise _refuse("malformed", f"segment {index} cannot carry a sequence range")
    if total > MAX_UNIT_BYTES:
        raise _refuse("malformed", "archive unit exceeds the total byte bound")
    head_segments = segments[:2]
    if (head_segments[0]["kind"], head_segments[0]["path"]) != ("genesis_manifest", GENESIS_MANIFEST_PATH) or \
            (head_segments[1]["kind"], head_segments[1]["path"], head_segments[1]["first"]) != \
            ("snapshot", GENESIS_SNAPSHOT_PATH, 0):
        raise _refuse("malformed", "unit must begin with the genesis manifest and genesis snapshot")
    if [_segment_sort_key(segment) for segment in segments[2:]] != \
            sorted(_segment_sort_key(segment) for segment in segments[2:]):
        raise _refuse("malformed", "segments are not in canonical inventory order")
    if head["batch_number"] != batches:
        raise _refuse("conflict", "head batch number does not match the archived canonical batches")
    if checkpoints != sorted(set(checkpoints)):
        raise _refuse("malformed", "checkpoint snapshots repeat")
    if (checkpoint is None) != (not checkpoints) or \
            (checkpoint is not None and checkpoint["global_sequence"] != checkpoints[-1]):
        raise _refuse("conflict", "checkpoint identity does not name the latest archived snapshot")
    if manifest["genesis_sha256"] != segments[0]["sha256"]:
        raise _refuse("conflict", "genesis digest does not name the archived genesis manifest")
    if manifest["inventory_root"] != inventory_root(segments, directories):
        raise _refuse("conflict", "inventory root does not commit to the directory and segment inventory")
    return manifest_digest(manifest)


def encode_proof(kind: int, value: bytes, proof: bytes) -> bytes:
    if kind not in PROOF_KINDS or not 0 < len(value) <= MAX_PROOF_PART_BYTES or \
            not 0 < len(proof) <= MAX_PROOF_PART_BYTES:
        raise _refuse("malformed", "proof bundle kind or size is out of bounds")
    return PROOF_MAGIC + bytes([kind]) + struct.pack(">I", len(value)) + value + proof


def decode_proof(data: bytes) -> tuple[int, bytes, bytes]:
    if len(data) < 9 or data[:4] != PROOF_MAGIC or data[4] not in PROOF_KINDS:
        raise _refuse("corrupt", "metadata proof segment framing is invalid")
    length = struct.unpack(">I", data[5:9])[0]
    if not 0 < length <= MAX_PROOF_PART_BYTES or not 0 < len(data) - 9 - length <= MAX_PROOF_PART_BYTES:
        raise _refuse("corrupt", "metadata proof segment lengths are invalid")
    return data[4], data[9:9 + length], data[9 + length:]


def parse_checkpoint_header(data: bytes) -> dict[str, Any]:
    header_bytes = LXS_HEADER_BYTES.get(data[:4])
    if header_bytes is None or len(data) < header_bytes:
        raise _refuse("corrupt", "checkpoint snapshot header is invalid")
    length = struct.unpack(">Q", data[108:116])[0]
    if len(data) != header_bytes + length:
        raise _refuse("corrupt", "checkpoint snapshot length does not match its header")
    return {
        "global_sequence": struct.unpack(">Q", data[4:12])[0],
        "canonical_state_root": data[12:44].hex(),
        "receipt_state_root": data[44:76].hex(),
        "snapshot_digest": data[76:108].hex(),
    }


def _codec_refusal(error: Exception, subject: str) -> StateArchiveRefusal:
    return _refuse("corrupt", f"native verification refused {subject}: {error}")


def verify_unit(config: RelayConfig, codec: NativeCodec, manifest: Mapping[str, Any],
                blobs: Sequence[bytes], work: Path) -> dict[str, Any]:
    """Verify every archived byte against the native codec, canonical continuity and the manifest."""
    digest = validate_manifest(manifest)
    segments = manifest["segments"]
    if len(blobs) != len(segments):
        raise _refuse("missing", "segment bytes do not cover the inventory")
    for segment, blob in zip(segments, blobs):
        if len(blob) != segment["length"] or sha256_hex(blob) != segment["sha256"]:
            raise _refuse("corrupt", f"segment {segment['index']} bytes are corrupt or truncated")
    if (manifest["network_id"], manifest["genesis_sha256"], manifest["sequencer_id"],
            manifest["sequencer_public_key"]) != (config.network_id, config.genesis_sha256,
                                                  config.sequencer_id, config.sequencer_public_key):
        raise _refuse("conflict", "unit network, genesis or sequencer identity differs from the pinned domain")
    work.mkdir(mode=0o700, parents=True, exist_ok=True)
    genesis_manifest, genesis_snapshot = work / "genesis.manifest", work / "genesis.snapshot"
    genesis_manifest.write_bytes(blobs[0])
    genesis_snapshot.write_bytes(blobs[1])
    try:
        genesis = codec.genesis(genesis_manifest, genesis_snapshot)
    except (CodecError, IntegrityError, ProtocolError) as error:
        raise _codec_refusal(error, "the genesis snapshot") from error
    expected = manifest["genesis"]
    if (genesis["state_root"], genesis["receipt_state_root"], genesis["snapshot_digest"],
            genesis["protocol_version"], require_decimal(genesis["global_sequence"], "genesis.global_sequence")) != \
            (expected["state_root"], expected["receipt_state_root"], expected["snapshot_digest"],
             expected["protocol_version"], 0):
        raise _refuse("conflict", "native genesis identity differs from the manifest")
    next_sequence = 1
    previous_root = expected["receipt_state_root"]
    boundaries: dict[int, str] = {}
    activities: dict[int, dict[str, Any]] = {}
    batch_blobs: list[bytes] = []
    for segment, blob in zip(segments, blobs):
        if segment["kind"] != "batch":
            continue
        try:
            batch = codec.verify(blob)
        except (CodecError, IntegrityError, ProtocolError) as error:
            raise _codec_refusal(error, f"canonical batch {segment['first']}") from error
        first = require_decimal(batch["first_sequence"], "batch.first_sequence")
        last = require_decimal(batch["last_sequence"], "batch.last_sequence")
        if require_decimal(batch["batch_number"], "batch.batch_number") != segment["first"] or \
                first != next_sequence or batch["previous_state_root"] != previous_root:
            raise _refuse("missing", f"canonical batch {segment['first']} does not continue the archived history")
        header = decode_hex(batch["header_hex"], "batch.header_hex", config.max_batch_bytes)
        for activity in batch["activities"]:
            activities[require_decimal(activity["sequence"], "activity.sequence")] = {
                "activity_id": activity["activity_id"],
                1: decode_hex(activity["canonical_hex"], "activity.canonical_hex", config.max_activity_bytes),
                3: decode_hex(activity["receipt_hex"], "activity.receipt_hex", config.max_batch_bytes),
                "header": header,
            }
        next_sequence, previous_root = last + 1, batch["resulting_state_root"]
        boundaries[last] = previous_root
        batch_blobs.append(blob)
    head = manifest["head"]
    if (head["global_sequence"], head["receipt_state_root"]) != (next_sequence - 1, previous_root):
        raise _refuse("conflict", "head sequence or receipt root differs from the verified canonical history")
    log_index = next((index for index, segment in enumerate(segments)
                      if segment["path"] == AVAILABILITY_LOG_PATH), None)
    if batch_blobs and log_index is None:
        raise _refuse("missing", "canonical availability log is absent from a unit with batches")
    if log_index is not None:
        if segments[log_index]["kind"] != "log":
            raise _refuse("malformed", "canonical availability log has the wrong segment kind")
        log_path = work / "da-bodies.log"
        log_path.write_bytes(blobs[log_index])
        for number in range(1, len(batch_blobs) + 2):
            try:
                exported = codec.export(log_path, number)
            except MissingBatch:
                exported = None
            except (CodecError, ProtocolError) as error:
                raise _codec_refusal(error, "the canonical availability log") from error
            wanted = batch_blobs[number - 1] if number <= len(batch_blobs) else None
            if exported != wanted:
                raise _refuse("conflict", f"availability log batch {number} differs from the archived batch")
    latest = None
    for segment, blob in zip(segments[2:], blobs[2:]):
        if segment["kind"] != "snapshot":
            continue
        header = parse_checkpoint_header(blob)
        if header["global_sequence"] != segment["first"] or header["global_sequence"] > head["global_sequence"]:
            raise _refuse("conflict", "checkpoint snapshot sequence differs from its inventory entry")
        boundary = boundaries.get(header["global_sequence"])
        if boundary is not None and boundary != header["receipt_state_root"]:
            raise _refuse("conflict", "checkpoint receipt root differs from the canonical batch boundary")
        latest = header
    if latest != manifest["checkpoint"]:
        raise _refuse("conflict", "latest checkpoint identity differs from the manifest")
    proofs = 0
    for segment, blob in zip(segments, blobs):
        if segment["kind"] != "metadata_proof":
            continue
        kind, value, proof = decode_proof(blob)
        activity = activities.get(segment["first"])
        if activity is None or activity["activity_id"] != segment["subject"]:
            raise _refuse("missing", "metadata proof names an activity outside the archived history")
        if value != activity[kind] or len(proof) < 35 or \
                proof[:3] != struct.pack(">HB", EVIDENCE_WIRE_VERSION, kind) or \
                proof[3:35] != bytes.fromhex(segment["subject"]) or activity["header"] not in proof:
            raise _refuse("conflict", "metadata proof does not bind the archived activity, receipt and header")
        proofs += 1
    return {"manifest_digest": digest, "genesis": genesis, "head": dict(head), "checkpoint": latest,
            "batches": len(batch_blobs), "proofs": proofs}


def certificate_body(digest: str, manifest: Mapping[str, Any], archive: Mapping[str, Any]) -> bytes:
    return canonical_json_bytes({
        "domain": CERTIFICATE_DOMAIN,
        "manifest_digest": digest,
        "network_id": manifest["network_id"],
        "genesis_sha256": manifest["genesis_sha256"],
        "head": manifest["head"],
        "checkpoint": manifest["checkpoint"],
        "inventory_root": manifest["inventory_root"],
        "profile_digest": manifest["profile_digest"],
        "archive_id": archive["archive_id"],
        "public_key": archive["public_key"],
        "key_generation": archive["key_generation"],
        "retention": archive["retention"],
    })


def _validated_pins(pinned: Sequence[Mapping[str, Any]]) -> dict[str, Mapping[str, Any]]:
    if len(pinned) != 2:
        raise _refuse("policy", "the availability policy requires exactly two enrolled archives")
    keys = {}
    for entry in pinned:
        entry = _fields(entry, {"archive_id", "public_key", "key_generation", "retention"}, "archive pin")
        _hex32(entry["public_key"], "archive pin public_key")
        keys[entry["archive_id"]] = entry
    if len(keys) != 2 or len({entry["public_key"] for entry in keys.values()}) != 2:
        raise _refuse("policy", "the two enrolled archives must have distinct identities and keys")
    return keys


def verify_certificate(certificate: Any, manifest: Mapping[str, Any], pinned: Sequence[Mapping[str, Any]]) -> str:
    """Accept only a certificate signed by both distinct pinned archives over this exact manifest."""
    digest = validate_manifest(manifest)
    keys = _validated_pins(pinned)
    certificate = _fields(certificate, {"domain", "manifest_digest", "signatures"}, "certificate")
    if certificate["domain"] != CERTIFICATE_DOMAIN or certificate["manifest_digest"] != digest:
        raise _refuse("conflict", "certificate does not bind this manifest")
    signatures = certificate["signatures"]
    if not isinstance(signatures, list) or len(signatures) != 2:
        raise _refuse("policy", "certificate must carry exactly two archive signatures")
    seen = set()
    for signature in signatures:
        signature = _fields(signature, {"archive_id", "public_key", "key_generation", "retention", "signature"},
                            "certificate signature")
        pin = keys.get(signature["archive_id"])
        if pin is None or signature["archive_id"] in seen or \
                any(signature[name] != pin[name] for name in ("public_key", "key_generation", "retention")):
            raise _refuse("policy", "certificate signer is not a distinct enrolled archive under its pinned terms")
        seen.add(signature["archive_id"])
        try:
            Ed25519PublicKey.from_public_bytes(bytes.fromhex(pin["public_key"])).verify(
                bytes.fromhex(signature["signature"]), certificate_body(digest, manifest, pin))
        except (InvalidSignature, TypeError, ValueError) as error:
            raise _refuse("policy", f"archive {signature['archive_id']} signature is invalid") from error
    return digest


class StateArchive:
    """One independently administered archive process."""

    def __init__(self, config: RelayConfig):
        settings = config.state_archive
        if settings is None:
            raise _refuse("policy", "configuration has no state_archive section")
        self.config = config
        self.settings = settings
        self.codec = NativeCodec(config)
        root = config.data_dir / "state"
        self.store = StateInventoryStore(root, {
            "network_id": str(config.network_id),
            "genesis_sha256": config.genesis_sha256,
            "sequencer_id": config.sequencer_id,
            "sequencer_public_key": config.sequencer_public_key,
            "archive_id": settings.archive_id,
            "key_generation": str(settings.key_generation),
            "retention": settings.retention,
            "profile_digest": settings.profile_digest,
        })
        self.work = root / "work"
        self.key = self._load_key(root / f"archive-key-{settings.key_generation}")
        self.admission = threading.Lock()

    @staticmethod
    def _load_key(path: Path) -> Ed25519PrivateKey:
        try:
            descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
        except FileExistsError:
            pass
        else:
            with os.fdopen(descriptor, "wb") as handle:
                handle.write(os.urandom(32))
                handle.flush()
                os.fsync(handle.fileno())
        information = path.lstat()
        if not stat.S_ISREG(information.st_mode) or stat.S_IMODE(information.st_mode) != 0o600 or \
                information.st_uid != os.geteuid() or information.st_size != 32:
            raise _refuse("policy", "archive signing key must be a private 32-byte regular file")
        return Ed25519PrivateKey.from_private_bytes(path.read_bytes())

    def identity(self) -> dict[str, Any]:
        public = self.key.public_key().public_bytes(serialization.Encoding.Raw, serialization.PublicFormat.Raw)
        return {"archive_id": self.settings.archive_id, "public_key": public.hex(),
                "key_generation": self.settings.key_generation, "retention": self.settings.retention,
                "profile_digest": self.settings.profile_digest, "network_id": self.config.network_id,
                "genesis_sha256": self.config.genesis_sha256}

    def admit(self, body: bytes) -> dict[str, Any]:
        try:
            manifest = json.loads(body)
        except (UnicodeDecodeError, json.JSONDecodeError) as error:
            raise _refuse("malformed", "manifest is not JSON") from error
        if not isinstance(manifest, dict) or canonical_json_bytes(manifest) != body:
            raise _refuse("malformed", "manifest bytes are not canonical")
        digest = validate_manifest(manifest)
        if manifest["profile_digest"] != self.settings.profile_digest:
            raise _refuse("policy", "manifest storage profile differs from this archive's commitment")
        with self.admission:
            existing = self.store.unit(manifest["network_id"], manifest["head"]["global_sequence"])
            if existing is not None and existing[0] != digest:
                raise _refuse("conflict", "a different manifest already holds this immutable unit identity")
            blobs = [self.store.segment(segment["sha256"], segment["length"]) for segment in manifest["segments"]]
            work = self.work / digest
            shutil.rmtree(work, ignore_errors=True)
            try:
                verify_unit(self.config, self.codec, manifest, blobs, work)
            finally:
                shutil.rmtree(work, ignore_errors=True)
            identity = self.identity()
            signature = self.key.sign(certificate_body(digest, manifest, identity)).hex()
            signature = self.store.record_unit(manifest["network_id"], manifest["head"]["global_sequence"],
                                               digest, body, signature)
        return {"manifest_digest": digest, "signature": signature, **identity}

    def serve(self) -> None:
        archive = self
        maximum = MAX_SEGMENT_BYTES

        class Handler(BaseHTTPRequestHandler):
            protocol_version = "HTTP/1.1"

            def log_message(self, *_arguments: Any) -> None:
                return

            def _reply(self, status: int, body: bytes, kind: str = "application/json") -> None:
                self.send_response(status)
                self.send_header("Content-Type", kind)
                self.send_header("Content-Length", str(len(body)))
                self.send_header("Cache-Control", "no-store")
                self.end_headers()
                self.wfile.write(body)

            def _body(self, limit: int) -> bytes:
                value = self.headers.get("Content-Length")
                if value is None or not value.isdigit() or int(value) > limit:
                    raise _refuse("malformed", "request body length is absent or out of bounds")
                body = self.rfile.read(int(value))
                if len(body) != int(value):
                    raise _refuse("malformed", "request body is truncated")
                return body

            def _dispatch(self, action: Callable[[], tuple[int, bytes, str]]) -> None:
                try:
                    status, body, kind = action()
                except StateArchiveRefusal as refusal:
                    status, kind = HTTP_STATUS[refusal.code], "application/json"
                    body = canonical_json_bytes({"error": refusal.code, "message": str(refusal)})
                except (RelayArchiveError, OSError, ValueError, sqlite3.Error) as error:
                    status, kind = 503, "application/json"
                    body = canonical_json_bytes({"error": "unavailable", "message": str(error)})
                self._reply(status, body, kind)

            def _route(self) -> tuple[str, str]:
                prefix, _, name = self.path.rpartition("/")
                return prefix, name

            def do_GET(self) -> None:
                def action() -> tuple[int, bytes, str]:
                    if self.path == "/v1/state-archive/identity":
                        return 200, canonical_json_bytes(archive.identity()), "application/json"
                    if self.path == "/v1/state-archive/units":
                        return 200, canonical_json_bytes(archive.store.units()), "application/json"
                    prefix, name = self._route()
                    if prefix == "/v1/state-archive/units":
                        return 200, archive.store.manifest(_hex32(name, "manifest digest")), "application/json"
                    if prefix == "/v1/state-archive/segments":
                        return 200, archive.store.segment(name), "application/octet-stream"
                    raise _refuse("missing", "unknown state archive resource")
                self._dispatch(action)

            def do_PUT(self) -> None:
                def action() -> tuple[int, bytes, str]:
                    prefix, name = self._route()
                    if prefix != "/v1/state-archive/segments":
                        raise _refuse("missing", "unknown state archive resource")
                    repaired = archive.store.put_segment(name, self._body(maximum))
                    return 201 if repaired else 200, canonical_json_bytes({"sha256": name}), "application/json"
                self._dispatch(action)

            def do_POST(self) -> None:
                def action() -> tuple[int, bytes, str]:
                    if self.path != "/v1/state-archive/units":
                        raise _refuse("missing", "unknown state archive resource")
                    reply = archive.admit(self._body(MAX_MANIFEST_BYTES))
                    return 200, canonical_json_bytes(reply), "application/json"
                self._dispatch(action)

        server = ThreadingHTTPServer((self.config.listen_host, self.config.listen_port), Handler)
        server.daemon_threads = True
        if self.config.tls_cert is not None and self.config.tls_key is not None:
            context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
            context.minimum_version = ssl.TLSVersion.TLSv1_2
            context.load_cert_chain(str(self.config.tls_cert), str(self.config.tls_key))
            server.socket = context.wrap_socket(server.socket, server_side=True)
        try:
            server.serve_forever()
        finally:
            server.server_close()


class ArchiveClient:
    def __init__(self, origin: str, timeout: float = 120.0, context: ssl.SSLContext | None = None):
        self.origin = origin.rstrip("/")
        self.timeout = timeout
        self.context = context

    def _request(self, method: str, path: str, body: bytes | None = None) -> bytes:
        request = urllib.request.Request(self.origin + path, data=body, method=method)
        try:
            with urllib.request.urlopen(request, timeout=self.timeout, context=self.context) as response:
                return response.read()
        except urllib.error.HTTPError as error:
            raw = error.read()
            try:
                detail = json.loads(raw)
                code, message = detail["error"], detail["message"]
            except (ValueError, KeyError, TypeError):
                code, message = "unavailable", f"archive answered HTTP {error.code}"
            raise StateArchiveRefusal(code if code in StateArchiveRefusal.CODES else "unavailable",
                                      message) from error
        except (urllib.error.URLError, OSError) as error:
            raise _refuse("unavailable", f"archive {self.origin} is unreachable: {error}") from error

    def identity(self) -> dict[str, Any]:
        return json.loads(self._request("GET", "/v1/state-archive/identity"))

    def units(self) -> list[dict[str, Any]]:
        return json.loads(self._request("GET", "/v1/state-archive/units"))

    def put_segment(self, data: bytes, digest: str | None = None) -> None:
        self._request("PUT", f"/v1/state-archive/segments/{digest or sha256_hex(data)}", data)

    def segment(self, digest: str) -> bytes:
        return self._request("GET", f"/v1/state-archive/segments/{digest}")

    def manifest(self, digest: str) -> bytes:
        return self._request("GET", f"/v1/state-archive/units/{digest}")

    def admit(self, manifest: Mapping[str, Any]) -> dict[str, Any]:
        return json.loads(self._request("POST", "/v1/state-archive/units", canonical_json_bytes(dict(manifest))))


def issue_certificate(manifest: Mapping[str, Any], blobs: Sequence[bytes], archives: Sequence[ArchiveClient],
                      pinned: Sequence[Mapping[str, Any]]) -> dict[str, Any]:
    """Persist and admit the unit at both archives; a certificate exists only with both signatures."""
    digest = validate_manifest(manifest)
    keys = _validated_pins(pinned)
    if len(archives) != 2:
        raise _refuse("policy", "the availability policy requires exactly two archive processes")
    signatures = []
    for client in archives:
        identity = client.identity()
        pin = keys.get(identity.get("archive_id"))
        if pin is None or identity.get("public_key") != pin["public_key"]:
            raise _refuse("policy", "archive process identity differs from its enrollment pin")
        for segment, blob in zip(manifest["segments"], blobs):
            client.put_segment(blob, segment["sha256"])
        reply = client.admit(manifest)
        if reply.get("manifest_digest") != digest:
            raise _refuse("conflict", "archives admitted different manifests")
        signatures.append({name: reply[name] for name in
                           ("archive_id", "public_key", "key_generation", "retention", "signature")})
    certificate = {"domain": CERTIFICATE_DOMAIN, "manifest_digest": digest,
                   "signatures": sorted(signatures, key=lambda entry: entry["archive_id"])}
    verify_certificate(certificate, manifest, pinned)
    return certificate


def fetch_segments(manifest: Mapping[str, Any],
                   archives: Sequence[ArchiveClient]) -> tuple[list[bytes], list[dict[str, Any]]]:
    """Fetch every segment from the first archive returning exact bytes; record each refused copy."""
    blobs, refusals = [], []
    for segment in manifest["segments"]:
        data = None
        codes = []
        for client in archives:
            try:
                candidate = client.segment(segment["sha256"])
            except StateArchiveRefusal as refusal:
                codes.append(refusal.code)
                refusals.append({"index": segment["index"], "archive": client.origin, "code": refusal.code})
                continue
            if len(candidate) == segment["length"] and sha256_hex(candidate) == segment["sha256"]:
                data = candidate
                break
            codes.append("corrupt")
            refusals.append({"index": segment["index"], "archive": client.origin, "code": "corrupt"})
        if data is None:
            code = next((name for name in ("corrupt", "missing", "unavailable") if name in codes), "unavailable")
            raise _refuse(code, f"segment {segment['index']} has no intact retained copy: {codes}")
        blobs.append(data)
    return blobs, refusals


def _write_private(path: Path, data: bytes, mode: int) -> None:
    descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    try:
        view = memoryview(data)
        while view:
            view = view[os.write(descriptor, view):]
        os.fchmod(descriptor, mode)
        os.fsync(descriptor)
    finally:
        os.close(descriptor)


def _sync_directory(path: Path) -> None:
    descriptor = os.open(path, os.O_RDONLY | os.O_DIRECTORY)
    try:
        os.fsync(descriptor)
    finally:
        os.close(descriptor)


def restore_unit(manifest: Mapping[str, Any], certificate: Mapping[str, Any],
                 pinned: Sequence[Mapping[str, Any]], archives: Sequence[ArchiveClient],
                 config: RelayConfig, codec: NativeCodec, target: Path, work: Path) -> dict[str, Any]:
    """Independently re-verify a certified unit and materialize its exact node files under target."""
    digest = verify_certificate(certificate, manifest, pinned)
    blobs, refusals = fetch_segments(manifest, archives)
    summary = verify_unit(config, codec, manifest, blobs, work / digest)
    shutil.rmtree(work / digest, ignore_errors=True)
    for root in ARCHIVE_ROOTS:
        if (target / root).exists() or (target / root).is_symlink():
            raise _refuse("conflict", f"restore target {root} already exists; restoration never merges")
    for entry in manifest["directories"]:
        path = target / entry["path"]
        path.mkdir(mode=0o700)
        os.chmod(path, entry["mode"])
    for segment, blob in zip(manifest["segments"], blobs):
        if segment["kind"] in FILE_KINDS:
            _write_private(target / segment["path"], blob, segment["mode"])
    for entry in reversed(manifest["directories"]):
        _sync_directory(target / entry["path"])
    _sync_directory(target)
    summary["refused_copies"] = refusals
    return summary


def _classify(path: str) -> str:
    if path == GENESIS_MANIFEST_PATH:
        return "genesis_manifest"
    if path == GENESIS_SNAPSHOT_PATH or CHECKPOINT_NAME.fullmatch(path):
        return "snapshot"
    if path.endswith("receipt-authority.log") or path == "logs/evidence.log":
        return "witness"
    if path == AVAILABILITY_LOG_PATH or path.startswith("logs/"):
        return "log"
    return "node_file"


def build_unit(config: RelayConfig, codec: NativeCodec, data_dir: Path, profile: str,
               proofs: Sequence[tuple[int, str, bytes, bytes]]) -> tuple[dict[str, Any], list[bytes]]:
    """Inventory a quiesced node data directory, its canonical batches and its metadata proofs."""
    directories, files = [], []
    for root in ARCHIVE_ROOTS:
        base = data_dir / root
        if not base.exists():
            continue
        for current, children, names in os.walk(base, followlinks=False):
            current_path = Path(current)
            information = current_path.lstat()
            if not stat.S_ISDIR(information.st_mode):
                raise _refuse("malformed", f"{current_path} is not a directory")
            directories.append({"path": current_path.relative_to(data_dir).as_posix(),
                                "mode": stat.S_IMODE(information.st_mode)})
            for name in children:
                if (current_path / name).is_symlink():
                    raise _refuse("malformed", f"{current_path / name} is a symbolic link")
            children.sort()
            for name in sorted(names):
                path = current_path / name
                information = path.lstat()
                if not stat.S_ISREG(information.st_mode):
                    raise _refuse("malformed", f"{path} is not a regular file")
                files.append((path.relative_to(data_dir).as_posix(), stat.S_IMODE(information.st_mode),
                              path.read_bytes()))
    directories.sort(key=lambda entry: entry["path"])
    by_path = {path: (mode, data) for path, mode, data in files}
    for required in (GENESIS_MANIFEST_PATH, GENESIS_SNAPSHOT_PATH):
        if required not in by_path:
            raise _refuse("missing", f"node data directory lacks {required}")
    genesis = codec.genesis(data_dir / GENESIS_MANIFEST_PATH, data_dir / GENESIS_SNAPSHOT_PATH)
    batches: list[bytes] = []
    sequences: dict[str, int] = {}
    head = {"global_sequence": 0, "batch_number": 0, "receipt_state_root": genesis["receipt_state_root"]}
    if AVAILABILITY_LOG_PATH in by_path:
        while True:
            try:
                raw = codec.export(data_dir / AVAILABILITY_LOG_PATH, len(batches) + 1)
            except MissingBatch:
                break
            metadata = codec.verify(raw)
            batches.append(raw)
            for activity in metadata["activities"]:
                sequences[activity["activity_id"]] = require_decimal(activity["sequence"], "activity.sequence")
            head = {"global_sequence": require_decimal(metadata["last_sequence"], "batch.last_sequence"),
                    "batch_number": len(batches), "receipt_state_root": metadata["resulting_state_root"]}

    def entry(kind: str, data: bytes, path: str = "", mode: int = 0, sequence: int = 0,
              subject: str = "") -> dict[str, Any]:
        return {"index": 0, "kind": kind, "path": path, "mode": mode, "sha256": sha256_hex(data),
                "length": len(data), "first": sequence, "last": sequence, "subject": subject}

    items = [(entry("genesis_manifest", by_path[GENESIS_MANIFEST_PATH][1], GENESIS_MANIFEST_PATH,
                    by_path[GENESIS_MANIFEST_PATH][0]), by_path[GENESIS_MANIFEST_PATH][1]),
             (entry("snapshot", by_path[GENESIS_SNAPSHOT_PATH][1], GENESIS_SNAPSHOT_PATH,
                    by_path[GENESIS_SNAPSHOT_PATH][0]), by_path[GENESIS_SNAPSHOT_PATH][1])]
    rest = [(entry("batch", raw, sequence=number), raw) for number, raw in enumerate(batches, 1)]
    checkpoint = None
    for path, (mode, data) in by_path.items():
        if path in (GENESIS_MANIFEST_PATH, GENESIS_SNAPSHOT_PATH):
            continue
        match = CHECKPOINT_NAME.fullmatch(path)
        sequence = int(match.group(1)) if match else 0
        rest.append((entry(_classify(path), data, path, mode, sequence), data))
        if match and (checkpoint is None or sequence > checkpoint["global_sequence"]):
            checkpoint = parse_checkpoint_header(data)
    for kind, activity_id, value, proof in proofs:
        if activity_id not in sequences:
            raise _refuse("missing", f"proof subject {activity_id} is not in the archived canonical history")
        data = encode_proof(kind, value, proof)
        rest.append((entry("metadata_proof", data, sequence=sequences[activity_id], subject=activity_id), data))
    rest.sort(key=lambda item: _segment_sort_key(item[0]))
    ordered = items + rest
    segments = []
    for index, (segment, _data) in enumerate(ordered):
        segment["index"] = index
        segments.append(segment)
    manifest = {
        "domain": MANIFEST_DOMAIN, "network_id": config.network_id, "genesis_sha256": config.genesis_sha256,
        "sequencer_id": config.sequencer_id, "sequencer_public_key": config.sequencer_public_key,
        "profile_digest": profile,
        "genesis": {"state_root": genesis["state_root"], "receipt_state_root": genesis["receipt_state_root"],
                    "snapshot_digest": genesis["snapshot_digest"],
                    "protocol_version": genesis["protocol_version"]},
        "head": head, "checkpoint": checkpoint, "directories": directories, "segments": segments,
        "inventory_root": inventory_root(segments, directories),
    }
    validate_manifest(manifest)
    return manifest, [data for _segment, data in ordered]


class LniClient:
    """Minimal local node interface client for node identity, submission and historical proof bundles."""

    def __init__(self, path: Path, timeout: float = 15.0):
        self.connection = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        self.connection.settimeout(timeout)
        self.connection.connect(str(path))
        self.correlation = 0
        tag, payload, _proof = self._call(LNI_NODE_INFO_REQUEST, b"", correlation=0)
        if tag != LNI_NODE_INFO_RESPONSE:
            raise _refuse("unavailable", f"node refused the interface handshake: {payload.hex()}")
        self.info = self._node_info(payload)

    def close(self) -> None:
        self.connection.close()

    def _exact(self, length: int) -> bytes:
        data = bytearray()
        while len(data) < length:
            chunk = self.connection.recv(length - len(data))
            if not chunk:
                raise _refuse("unavailable", "node interface closed the connection")
            data += chunk
        return bytes(data)

    def _call(self, tag: int, payload: bytes, correlation: int | None = None) -> tuple[int, bytes, bytes]:
        if correlation is None:
            self.correlation += 1
            correlation = self.correlation
        body = struct.pack(">HHHQI", LNI_MAJOR, LNI_MINOR, tag, correlation, len(payload)) + payload + \
            struct.pack(">I", 0)
        self.connection.sendall(struct.pack(">I", len(body)) + body)
        length = struct.unpack(">I", self._exact(4))[0]
        if not LNI_FIXED_BYTES <= length <= LNI_MAX_FRAME_BYTES:
            raise _refuse("corrupt", "node interface frame length is out of bounds")
        frame = self._exact(length)
        major, _minor, reply, echoed, payload_length = struct.unpack(">HHHQI", frame[:18])
        if major != LNI_MAJOR or echoed != correlation or payload_length > length - LNI_FIXED_BYTES:
            raise _refuse("corrupt", "node interface reply does not match the request")
        proof_offset = 18 + payload_length
        proof_length = struct.unpack(">I", frame[proof_offset:proof_offset + 4])[0]
        if proof_offset + 4 + proof_length != length:
            raise _refuse("corrupt", "node interface reply framing is invalid")
        return reply, frame[18:proof_offset], frame[proof_offset + 4:]

    @staticmethod
    def _node_info(payload: bytes) -> dict[str, Any]:
        if len(payload) < 93:
            raise _refuse("corrupt", "node information reply is truncated")
        major, minor, protocol, network = struct.unpack(">HHHI", payload[:10])
        head, batch = struct.unpack(">QQ", payload[11:27])
        count = struct.unpack(">H", payload[91:93])[0]
        capabilities, cursor = [], 93
        for _index in range(count):
            size = struct.unpack(">H", payload[cursor:cursor + 2])[0]
            capabilities.append(payload[cursor + 2:cursor + 2 + size].decode("ascii"))
            cursor += 2 + size
        if cursor != len(payload):
            raise _refuse("corrupt", "node information reply has trailing bytes")
        return {"major": major, "minor": minor, "protocol_version": protocol, "network_id": network,
                "role": payload[10], "head_sequence": head, "published_batch": batch,
                "checkpoint_id": payload[27:59].hex(), "sequencer_public_key": payload[59:91].hex(),
                "capabilities": capabilities}

    def submit(self, canonical: bytes, activity_id: str) -> None:
        identifier = bytes.fromhex(_hex32(activity_id, "activity"))
        if not 0 < len(canonical) <= LNI_MAX_ACTIVITY_BYTES:
            raise _refuse("malformed", "canonical activity is empty or oversized")
        tag, payload, proof = self._call(LNI_SUBMIT_REQUEST, canonical)
        if tag == LNI_ERROR_RESPONSE and len(payload) == 5:
            raise _refuse("policy", f"node refused the activity: class {payload[0]} result "
                                    f"{struct.unpack('>i', payload[1:5])[0]}")
        if tag != LNI_SUBMIT_RESPONSE or payload != canonical or proof != identifier:
            raise _refuse("corrupt", "node returned an invalid submission acknowledgement")

    def proof_bundle(self, kind: int, activity_id: str) -> tuple[bytes, bytes]:
        if kind not in PROOF_KINDS:
            raise _refuse("malformed", "unsupported proof bundle kind")
        tag, payload, proof = self._call(LNI_PROOF_BUNDLE_REQUEST,
                                         struct.pack(">HB", 1, kind) + bytes.fromhex(_hex32(activity_id, "activity")))
        if tag == LNI_ERROR_RESPONSE and len(payload) == 5:
            raise _refuse("missing", f"node refused the proof bundle: class {payload[0]} result "
                                     f"{struct.unpack('>i', payload[1:5])[0]}")
        if tag != LNI_PROOF_BUNDLE_RESPONSE or not payload or not proof:
            raise _refuse("corrupt", "node returned an invalid proof bundle reply")
        return payload, proof


def _lni_main(arguments: Sequence[str]) -> int:
    arity = {"node-info": 2, "submit": 3, "proof": 4}
    if not arguments or arity.get(arguments[0]) != len(arguments):
        print("usage: state_archive.py lni node-info SOCKET | lni submit SOCKET ACTIVITY_ID < ACTIVITY"
              " | lni proof SOCKET KIND ACTIVITY_ID", file=sys.stderr)
        return 2
    client = LniClient(Path(arguments[1]))
    try:
        if arguments[0] == "node-info":
            print(json.dumps(client.info, sort_keys=True))
            return 0
        if arguments[0] == "submit":
            client.submit(sys.stdin.buffer.read(LNI_MAX_ACTIVITY_BYTES + 1), arguments[2])
            print(json.dumps({"activity_id": arguments[2], "state": "acknowledged"}, sort_keys=True))
            return 0
        if len(arguments) != 4:
            raise _refuse("malformed", "proof requires a kind and an activity identifier")
        value, proof = client.proof_bundle(int(arguments[2]), arguments[3])
        print(json.dumps({"value_hex": value.hex(), "proof_hex": proof.hex()}, sort_keys=True))
        return 0
    finally:
        client.close()


if __name__ == "__main__":
    if len(sys.argv) < 2 or sys.argv[1] != "lni":
        print("usage: state_archive.py lni ...", file=sys.stderr)
        raise SystemExit(2)
    try:
        raise SystemExit(_lni_main(sys.argv[2:]))
    except (StateArchiveRefusal, OSError, ValueError) as error:
        print(f"state-archive-lni: {error}", file=sys.stderr)
        raise SystemExit(1)
