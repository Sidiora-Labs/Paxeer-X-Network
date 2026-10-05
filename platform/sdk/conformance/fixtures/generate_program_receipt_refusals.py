#!/usr/bin/env python3
"""Derive the shared Programs receipt refusal vectors from the signed Programs golden receipt."""
import json
import struct
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
SOURCE = HERE / "receipt-programs-positive-v2.json"
TARGET = HERE / "receipt-programs-refusals-v2.json"
SIGNATURE_TAIL = 69


def layout(receipt: bytes) -> dict:
    offset = 6

    def array() -> int:
        nonlocal offset
        start = offset
        if struct.unpack_from(">I", receipt, offset)[0] != 32:
            raise ValueError("non-canonical 32-byte field")
        offset += 36
        return start + 4

    if receipt[2:4] != b"\x52\x01":
        raise ValueError("Programs golden receipt must use the protocol receipt tag")
    array()
    global_sequence = offset
    offset += 8
    array()
    resulting_state_root = array()
    array()
    offset += 4
    effects = struct.unpack_from(">I", receipt, offset)[0]
    offset += 4
    for _ in range(effects):
        offset += 8
        array()
        offset += 4 + struct.unpack_from(">I", receipt, offset)[0]
    offset += 16
    array()
    module_id = offset
    offset += 2 + 4 + 4 + 1
    array()
    offset += 16
    array()
    offset += 40
    array()
    offset += 32
    for _ in range(3):
        array()
    offset += 8
    signature_marker = len(receipt) - SIGNATURE_TAIL
    if receipt[signature_marker:signature_marker + 5] != b"\x01\x00\x00\x00\x40":
        raise ValueError("Programs golden receipt must carry one sequencer signature")
    if signature_marker - offset <= 0:
        raise ValueError("Programs golden receipt must carry a Programs outcome")
    return {
        "global_sequence": global_sequence,
        "resulting_state_root": resulting_state_root,
        "module_id": module_id,
        "program_outcome": offset,
        "signature_marker": signature_marker,
    }


def patched(receipt: bytes, offset: int, value: bytes) -> bytes:
    return receipt[:offset] + value + receipt[offset + len(value):]


def main() -> int:
    source = json.loads(SOURCE.read_text(encoding="utf-8"))
    receipt = bytes.fromhex(source["canonical_receipt_hex"])
    at = layout(receipt)
    outcome = at["program_outcome"]
    vectors = [
        ("unknown-program-outcome-tag", "program-outcome",
         patched(receipt, outcome, struct.pack(">I", 0x50524739))),
        ("zero-program-outcome-terminal-kind", "program-outcome",
         patched(receipt, outcome + 4, b"\x00")),
        ("program-outcome-outside-programs-module", "program-outcome",
         patched(receipt, at["module_id"], struct.pack(">H", 1))),
        ("zero-global-sequence-with-program-outcome", "global-sequence",
         patched(receipt, at["global_sequence"], bytes(8))),
        ("zero-resulting-state-root-with-program-outcome", "resulting-state-root",
         patched(receipt, at["resulting_state_root"], bytes(32))),
        ("unsigned-program-outcome-receipt", "missing-signature",
         receipt[:at["signature_marker"]] + b"\x00"),
        ("flipped-signature-byte-with-program-outcome", "sequencer-signature",
         receipt[:-1] + bytes([receipt[-1] ^ 1])),
    ]
    fixture = {
        "name": "receipt-programs-refusals-v2",
        "provenance": {
            "generator": "platform/sdk/conformance/fixtures/generate_program_receipt_refusals.py",
            "command": "python3 platform/sdk/conformance/fixtures/generate_program_receipt_refusals.py",
            "description": "Refusal vectors derived byte-exactly from receipt-programs-positive-v2: malformed or unbound optional Programs outcomes, required non-zero invariants with the outcome present, the canonical unsigned receipt and a flipped sequencer signature byte. Every SDK verifier must refuse each vector with the same typed receipt check.",
        },
        "source": SOURCE.name,
        "authorized_batch": source["authorized_batch"],
        "vectors": [
            {"name": name, "expected_check": check, "canonical_receipt_hex": value.hex()}
            for name, check, value in vectors
        ],
    }
    rendered = json.dumps(fixture, indent=2) + "\n"
    if sys.argv[1:] == ["--check"]:
        return 0 if TARGET.read_text(encoding="utf-8") == rendered else 1
    TARGET.write_text(rendered, encoding="utf-8")
    return 0


if __name__ == "__main__":
    sys.exit(main())
