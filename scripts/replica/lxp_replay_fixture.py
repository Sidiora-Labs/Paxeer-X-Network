#!/usr/bin/env python3
import hashlib
import pathlib
import struct

ROOT = pathlib.Path(__file__).resolve().parents[2]
OUTPUT = ROOT / "tests" / "vectors" / "replay_corpus.lxb"

ACTIVITY_TYPES = tuple(
    (module << 16) | ordinal
    for module, maximum in ((1, 8), (2, 7), (3, 7), (4, 7), (5, 13), (6, 11))
    for ordinal in range(1, maximum + 1)
)


def domain(tag: bytes, payload: bytes) -> bytes:
    return hashlib.sha256(tag + b"\0" + payload).digest()


def sized(payload: bytes) -> bytes:
    return struct.pack(">I", len(payload)) + payload


def activity(activity_type: int, sequence: int) -> bytes:
    actor = b"did:lx:crossarch"
    authority = b"\xa1\x01"
    payload = struct.pack(">IQ", activity_type, sequence)
    idempotency = hashlib.sha256(
        b"LayerX replay idempotency" + struct.pack(">I", activity_type)
    ).digest()
    signature_seed = hashlib.sha256(
        b"LayerX replay signature" + struct.pack(">I", activity_type)
    ).digest()
    parts = [struct.pack(">HHB", 1, 0x1001, 12)]
    parts += [b"\x01" + struct.pack(">H", 1)]
    parts += [b"\x02" + struct.pack(">I", 77)]
    parts += [b"\x03" + struct.pack(">I", activity_type)]
    parts += [b"\x04" + sized(actor)]
    parts += [b"\x05" + sized(authority)]
    parts += [b"\x06" + struct.pack(">Q", sequence)]
    parts += [b"\x07" + struct.pack(">QQ", 1_700_000_000_000,
                                     1_700_000_100_000)]
    parts += [b"\x08" + sized(idempotency)]
    parts += [b"\x09" + (1).to_bytes(16, "big")]
    parts += [b"\x0a" + sized(domain(b"LXP/v1/payload-hash", payload))]
    parts += [b"\x0b" + sized(payload)]
    parts += [b"\x0c" + sized(signature_seed + signature_seed)]
    return b"".join(parts)


def build() -> bytes:
    records = []
    previous_state = bytes(32)
    previous_batch = bytes(32)
    previous_digest = bytes(32)
    for sequence, activity_type in enumerate(ACTIVITY_TYPES, 1):
        encoded = activity(activity_type, sequence)
        activity_id = domain(b"LXP/v1/activity-id", encoded)
        state = domain(
            b"LXP/v1/state-root-chain",
            previous_state + struct.pack(">Q", sequence) + activity_id,
        )
        receipt = (
            struct.pack(">HQ", 1, sequence)
            + activity_id
            + previous_state
            + state
        )
        event = struct.pack(">I", activity_type) + activity_id
        boundary = sequence % 7 == 0 or sequence == len(ACTIVITY_TYPES)
        batch = bytes(32)
        if boundary:
            batch = domain(
                b"LXP/v1/batch-header",
                previous_batch + state + struct.pack(">Q", sequence),
            )
        digest_input = (
            previous_digest
            + sized(receipt)
            + sized(event)
            + bytes((int(boundary),))
            + (batch if boundary else b"")
        )
        digest = domain(b"LXP/v1/state-root-chain", digest_input)
        record = (
            struct.pack(">QB", sequence, int(boundary))
            + sized(encoded)
            + state
            + sized(receipt)
            + sized(event)
            + batch
        )
        records.append(record)
        previous_state = state
        if boundary:
            previous_batch = batch
        previous_digest = digest
    header = (
        b"LXPRP001"
        + struct.pack(">II", 1, len(records))
        + previous_state
        + previous_digest
    )
    return header + b"".join(records)


def main() -> None:
    payload = build()
    OUTPUT.parent.mkdir(parents=True, exist_ok=True)
    OUTPUT.write_bytes(payload)
    print(f"{OUTPUT.relative_to(ROOT)} {len(ACTIVITY_TYPES)} {len(payload)}")


if __name__ == "__main__":
    main()
