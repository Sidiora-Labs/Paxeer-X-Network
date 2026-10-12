from __future__ import annotations

import base64
import importlib.util
import json
import unittest
from pathlib import Path

SCRIPT = Path(__file__).resolve().parent / "paxeer-x" / "registry-router-bootstrap.py"
SPEC = importlib.util.spec_from_file_location("registry_router_bootstrap", SCRIPT)
bootstrap = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(bootstrap)

# LegacyAmino.MarshalAsJSON of a keeper.ReceiptKeys list holding two entries:
# key id 32 x 0x01 with the ed25519 public key of seed 32 x 0x01 over [0, 99],
# and key id 32 x 0x02 with the ed25519 public key of seed 32 x 0x02 over
# [100, 2^64 - 1].
AMINO_RECEIPT_KEYS = (
    '[{"key_id":"AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE=",'
    '"public_key":"iojj3XQJ8ZX9UtstPLpdcspnCb8dlBIb83SIAbQPb1w=",'
    '"first_height":"0","last_height":"99"},'
    '{"key_id":"AgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgI=",'
    '"public_key":"gTl3Dqh9F19Wo1Rmw0x+zMuNipG07jeiXfYPW4/Js5Q=",'
    '"first_height":"100","last_height":"18446744073709551615"}]'
)
KEY_ONE = bytes([1]) * 32
KEY_TWO = bytes([2]) * 32
PUBLIC_ONE = bytes.fromhex(
    "8a88e3dd7409f195fd52db2d3cba5d72ca6709bf1d94121bf3748801b40f6f5c"
)
PUBLIC_TWO = bytes.fromhex(
    "8139770ea87d175f56a35466c34c7ecccb8d8a91b4ee37a25df60f5b8fc9b394"
)

FIELDS = "governed receipt key fields differ"
ENCODING = "governed receipt key encoding differs"
BYTES = "governed receipt key bytes differ"
RANGE = "governed receipt key height range differs"
EMPTY = "governed receipt key list is empty"
DUPLICATE = "governed receipt key id is duplicated"
UNGOVERNED = "receipt key is not governed at the receipt height"
OUTSIDE = "receipt height is outside its governed key range"


def fixture_entries():
    return json.loads(AMINO_RECEIPT_KEYS)


def encoded(entries):
    return json.dumps(entries, separators=(",", ":"))


class GovernedReceiptKeysTest(unittest.TestCase):
    def assert_refused(self, value, reason):
        with self.assertRaises(bootstrap.Refused) as raised:
            bootstrap.governed_receipt_keys(value)
        self.assertEqual(str(raised.exception), reason)

    def assert_entry_refused(self, field, value, reason):
        entries = fixture_entries()
        entries[0][field] = value
        self.assert_refused(encoded(entries), reason)

    def test_governed_receipt_keys_amino_fixture(self):
        pinned = bootstrap.governed_receipt_keys(AMINO_RECEIPT_KEYS)
        self.assertEqual(
            pinned,
            {
                KEY_ONE: (PUBLIC_ONE, 0, 99),
                KEY_TWO: (PUBLIC_TWO, 100, (1 << 64) - 1),
            },
        )
        self.assertEqual(bootstrap.governed_receipt_key(pinned, KEY_ONE, 0), PUBLIC_ONE)
        self.assertEqual(
            bootstrap.governed_receipt_key(pinned, KEY_ONE, 99), PUBLIC_ONE
        )
        self.assertEqual(
            bootstrap.governed_receipt_key(pinned, KEY_TWO, 100), PUBLIC_TWO
        )
        self.assertEqual(
            bootstrap.governed_receipt_key(pinned, KEY_TWO, (1 << 64) - 1), PUBLIC_TWO
        )

    def test_governed_receipt_keys_equal_first_and_last(self):
        entries = fixture_entries()
        entries[0]["last_height"] = "0"
        pinned = bootstrap.governed_receipt_keys(encoded(entries))
        self.assertEqual(pinned[KEY_ONE], (PUBLIC_ONE, 0, 0))

    def test_governed_receipt_keys_refuses_bare_int(self):
        self.assert_entry_refused("first_height", 0, RANGE)
        self.assert_entry_refused("last_height", 99, RANGE)

    def test_governed_receipt_keys_refuses_bool(self):
        self.assert_entry_refused("first_height", False, RANGE)
        self.assert_entry_refused("last_height", True, RANGE)

    def test_governed_receipt_keys_refuses_null_and_float(self):
        self.assert_entry_refused("first_height", None, RANGE)
        self.assert_entry_refused("last_height", 99.0, RANGE)

    def test_governed_receipt_keys_refuses_sign(self):
        self.assert_entry_refused("first_height", "+0", RANGE)
        self.assert_entry_refused("last_height", "+99", RANGE)

    def test_governed_receipt_keys_refuses_negative(self):
        self.assert_entry_refused("first_height", "-1", RANGE)
        self.assert_entry_refused("first_height", "-0", RANGE)

    def test_governed_receipt_keys_refuses_leading_zero(self):
        self.assert_entry_refused("first_height", "00", RANGE)
        self.assert_entry_refused("last_height", "099", RANGE)

    def test_governed_receipt_keys_refuses_non_decimal_text(self):
        self.assert_entry_refused("first_height", "", RANGE)
        self.assert_entry_refused("last_height", " 99", RANGE)
        self.assert_entry_refused("last_height", "99\n", RANGE)
        self.assert_entry_refused("last_height", "0x63", RANGE)
        self.assert_entry_refused("last_height", "9_9", RANGE)
        self.assert_entry_refused("last_height", "٩٩", RANGE)

    def test_governed_receipt_keys_refuses_value_at_two_to_sixty_four(self):
        self.assert_entry_refused("last_height", str(1 << 64), RANGE)
        entries = fixture_entries()
        entries[1]["first_height"] = str(1 << 64)
        entries[1]["last_height"] = str(1 << 64)
        self.assert_refused(encoded(entries), RANGE)

    def test_governed_receipt_keys_refuses_first_above_last(self):
        entries = fixture_entries()
        entries[0]["first_height"] = "100"
        self.assert_refused(encoded(entries), RANGE)

    def test_governed_receipt_keys_refuses_field_set(self):
        entries = fixture_entries()
        del entries[0]["last_height"]
        self.assert_refused(encoded(entries), FIELDS)
        entries = fixture_entries()
        entries[0]["context_hash"] = entries[0]["key_id"]
        self.assert_refused(encoded(entries), FIELDS)
        self.assert_refused(encoded([[]]), FIELDS)
        self.assert_refused(encoded(["key"]), FIELDS)

    def test_governed_receipt_keys_refuses_empty_list(self):
        self.assert_refused("[]", EMPTY)
        self.assert_refused("{}", EMPTY)
        self.assert_refused('"ReceiptKeys"', EMPTY)

    def test_governed_receipt_keys_refuses_non_text_key(self):
        self.assert_entry_refused("key_id", list(KEY_ONE), ENCODING)
        self.assert_entry_refused("public_key", None, ENCODING)

    def test_governed_receipt_keys_refuses_base64(self):
        self.assert_entry_refused(
            "key_id", "AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE", BYTES
        )
        self.assert_entry_refused(
            "public_key", "iojj3XQJ8ZX9UtstPLpdcspnCb8dlBIb83SIAbQPb1w*", BYTES
        )
        self.assert_entry_refused(
            "key_id", base64.urlsafe_b64encode(bytes([0xFB]) * 32).decode(), BYTES
        )
        self.assert_entry_refused(
            "key_id", "AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQF=", BYTES
        )

    def test_governed_receipt_keys_refuses_wrong_length(self):
        self.assert_entry_refused(
            "key_id", base64.b64encode(KEY_ONE[:31]).decode(), BYTES
        )
        self.assert_entry_refused(
            "public_key", base64.b64encode(PUBLIC_ONE + b"\0").decode(), BYTES
        )

    def test_governed_receipt_keys_refuses_zero_key(self):
        zero = base64.b64encode(bytes(32)).decode()
        self.assert_entry_refused("key_id", zero, BYTES)
        self.assert_entry_refused("public_key", zero, BYTES)

    def test_governed_receipt_keys_refuses_duplicate_id(self):
        entries = fixture_entries()
        entries[1]["key_id"] = entries[0]["key_id"]
        self.assert_refused(encoded(entries), DUPLICATE)

    def test_governed_receipt_keys_refuses_ungoverned_key(self):
        pinned = bootstrap.governed_receipt_keys(AMINO_RECEIPT_KEYS)
        with self.assertRaises(bootstrap.Refused) as raised:
            bootstrap.governed_receipt_key(pinned, bytes([3]) * 32, 50)
        self.assertEqual(str(raised.exception), UNGOVERNED)

    def test_governed_receipt_keys_refuses_wrong_height(self):
        pinned = bootstrap.governed_receipt_keys(AMINO_RECEIPT_KEYS)
        for key_id, height in ((KEY_ONE, 100), (KEY_TWO, 99), (KEY_TWO, 0)):
            with self.assertRaises(bootstrap.Refused) as raised:
                bootstrap.governed_receipt_key(pinned, key_id, height)
            self.assertEqual(str(raised.exception), OUTSIDE)


if __name__ == "__main__":
    unittest.main()
