import importlib.util
import json
from pathlib import Path
import unittest
from hashlib import sha256

from layerx_sdk.program_wire import decode_and_verify_program_terminal, _verify_applied_legs
from layerx_sdk.verifier import AuthorizedReceiptBatch, verify_receipt_outcome

ROOT = Path(__file__).resolve().parents[3]
spec = importlib.util.spec_from_file_location('signatures', ROOT / 'platform/integrations/fastapi/layerx_fastapi/signatures.py')
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)


class TerminalV4(unittest.TestCase):
    def test_signed_shared_vectors(self):
        for name, expected in [('executed-v4', 'reconstructed'), ('principal-v4', 'reconstructed'),
                               ('mutated-leg-v4', None), ('executed-v3', 'recorded_terminal_root_not_locally_reconstructable')]:
            with self.subTest(name=name):
                fixture = json.loads((ROOT / f'platform/sdk/conformance/fixtures/receipt-programs-{name}.json').read_text())
                source = fixture['authorized_batch']
                authority = AuthorizedReceiptBatch(**{field: bytes.fromhex(source[field + '_hex']) for field in
                    ('batch_id', 'asset', 'previous_state_root', 'resulting_state_root', 'sequencer_public_key')})
                verified = verify_receipt_outcome(bytes.fromhex(fixture['canonical_receipt_hex']), authority, module.LayerXSignatureVerifier(), protocol_version=3)
                self.assertEqual(verified.receipt_digest.hex(), fixture['receipt_digest_hex'])
                self.assertEqual(sha256(b'LXP/v1/activity-id\0' + bytes.fromhex(fixture['signed_activity_hex'])).digest(), verified.receipt.activity_id)
                args = (bytes.fromhex(fixture['terminal_payload_hex']), bytes.fromhex(fixture['call_graph_hex']), fixture['program_id_hex'], verified.receipt.program_outcome, 3)
                if expected is None:
                    with self.assertRaisesRegex(ValueError, 'applied transfer root'):
                        decode_and_verify_program_terminal(*args)
                else:
                    self.assertEqual(decode_and_verify_program_terminal(*args).transfer_verification, expected)
                if name == 'executed-v4':
                    for length in range(len(args[0])):
                        with self.assertRaises(ValueError):
                            decode_and_verify_program_terminal(args[0][:length], *args[1:])
                    with self.assertRaises(ValueError):
                        decode_and_verify_program_terminal(args[0] + b'\0', *args[1:])

    def test_empty_legs_require_zero_root(self):
        _verify_applied_legs(b'', bytes(32))
        with self.assertRaises(ValueError):
            _verify_applied_legs(b'', b'\1' * 32)


class TerminalV5(unittest.TestCase):
    def test_actual_native_v5_corpus_and_cross_profile_refusals(self):
        import os
        import stat
        from layerx_sdk.production import PlatformSdkError
        from layerx_sdk.program_wire import (
            _Reader, _AUTHORITY, _OCCUPANCY, _EXECUTION_V5,
            _decode_candidate, _decode_candidate_v5, bind_retained_program_call,
        )
        from layerx_sdk.programs import ProgramTrustContext, _execution, verify_program_receipt
        from layerx_sdk.verifier import verify_program_receipt_outcome_v5
        supplied = os.environ.get('PAXEER_X_PROGRAM_TERMINAL_V5_CORPUS')
        self.assertIsNotNone(supplied, 'genuine native signed v5 corpus required; no skipped cases')
        path = Path(supplied)
        metadata = path.lstat()
        self.assertTrue(path.is_absolute() and path.resolve() == path and stat.S_ISREG(metadata.st_mode))
        self.assertEqual((metadata.st_uid, metadata.st_nlink, stat.S_IMODE(metadata.st_mode)), (os.geteuid(), 1, 0o600))
        corpus = json.loads(path.read_bytes())
        self.assertEqual(set(corpus), {'source_revision', 'cases'})
        self.assertEqual(len(corpus['source_revision']), 40)
        expected = {f'abi{abi}-{outcome}' for abi in (3, 4) for outcome in ('success', 'failure', 'resource', 'callback', 'settlement')}
        self.assertEqual({row['name'] for row in corpus['cases']}, expected)
        self.assertEqual(len(corpus['cases']), len(expected))
        for row in corpus['cases']:
            with self.subTest(case=row['name']):
                source = row['authorized_batch']
                authority = AuthorizedReceiptBatch(**{field: bytes.fromhex(source[field + '_hex']) for field in
                    ('batch_id', 'asset', 'previous_state_root', 'resulting_state_root', 'sequencer_public_key')})
                signed = bytes.fromhex(row['signed_activity_hex'])
                canonical = bytes.fromhex(row['canonical_receipt_hex'])
                terminal = bytes.fromhex(row['terminal_payload_hex'])
                graph = bytes.fromhex(row['call_graph_hex'])
                activity_id = sha256(b'LXP/v1/activity-id\0' + signed).hexdigest()
                signatures = module.LayerXSignatureVerifier()
                payload_hash, abi, idempotency_key = bind_retained_program_call(signed, activity_id, row['program_id_hex'], 3)
                self.assertEqual(abi, row['guest_abi'])
                self.assertEqual(authority.sequencer_public_key.hex(), row['sequencer_public_key_hex'])
                verified = verify_program_receipt_outcome_v5(canonical, authority, signatures,
                    terminal, graph, row['program_id_hex'], signed)
                self.assertEqual(verified.receipt.program_outcome.abi_version, abi)
                with self.assertRaises((ValueError, PlatformSdkError)):
                    verify_receipt_outcome(canonical, authority, signatures, protocol_version=3)
                decoded = decode_and_verify_program_terminal(terminal, graph, row['program_id_hex'],
                    verified.receipt.program_outcome, 3, protocol=verified.receipt, expected_payload_hash=payload_hash)
                receipt = verified.receipt
                document = {
                    'state': 'executed' if receipt.result_code == 0 else 'refused',
                    'activity_id': receipt.activity_id.hex(), 'program_id': row['program_id_hex'],
                    'guest_abi_version': receipt.program_outcome.abi_version,
                    'module_version': receipt.module_version, 'batch_id': receipt.batch_id.hex(),
                    'global_sequence': str(receipt.global_sequence),
                    'result_code': receipt.program_outcome.result_code,
                    'state_root': receipt.resulting_state_root.hex(), 'receipt': canonical.hex(),
                    'receipt_digest': verified.receipt_digest.hex(),
                    'terminal_payload': terminal.hex(), 'call_graph': graph.hex(),
                    'authority': {field: getattr(authority, field).hex() for field in
                        ('batch_id', 'asset', 'previous_state_root', 'resulting_state_root', 'sequencer_public_key')},
                    'usage': dict(decoded.usage), 'outcome': dict(decoded.outcome),
                    'verification': 'receipt-terminal-and-call-graph-verified',
                    'idempotency_key': idempotency_key, 'retained_signed_activity': signed.hex(),
                }
                self.assertEqual(receipt.activity_id.hex(), activity_id)
                checked = verify_program_receipt(_execution(document, document['state']), authority, signatures,
                    ProgramTrustContext(authority.sequencer_public_key, protocol_version=3), expected_signed_activity=signed)
                self.assertEqual(checked.verification.canonical_bytes, canonical)
                for field, value in [('guest_abi_version', 4 if abi == 3 else 3), ('program_id', '00' * 32),
                                     ('module_version', 3), ('result_code', True)]:
                    with self.subTest(field=field), self.assertRaises((ValueError, TypeError, PlatformSdkError)):
                        verify_program_receipt({**document, field: value}, authority, signatures,
                            ProgramTrustContext(authority.sequencer_public_key, protocol_version=3), expected_signed_activity=signed)
                damaged = canonical[:-1] + bytes([canonical[-1] ^ 1])
                with self.assertRaises((ValueError, PlatformSdkError)):
                    verify_program_receipt_outcome_v5(damaged, authority, signatures,
                        terminal, graph, row['program_id_hex'], signed)
                for altered in (terminal[:-1], terminal + b'\0', terminal[:1] + bytes([terminal[1] ^ 1]) + terminal[2:]):
                    with self.assertRaises((ValueError, PlatformSdkError)):
                        verify_program_receipt_outcome_v5(canonical, authority, signatures,
                            altered, graph, row['program_id_hex'], signed)
                with self.assertRaises((ValueError, PlatformSdkError)):
                    verify_program_receipt_outcome_v5(canonical, authority, signatures,
                        terminal, graph + b'\0', row['program_id_hex'], signed)
                inner = terminal
                applied = b'LXP/programs/terminal-applied-legs/v1\0'
                if inner.startswith(applied):
                    reader = _Reader(inner[len(applied):]); inner = reader.sized_u32(1_048_576)
                    reader.sized_u32(256 * 115); reader.end()
                if inner.startswith(_AUTHORITY):
                    reader = _Reader(inner[len(_AUTHORITY):]); inner = reader.sized_u32(1_048_576)
                    reader.sized_u32(1_048_576); reader.fixed(32); reader.end()
                if inner.startswith(_OCCUPANCY):
                    reader = _Reader(inner[len(_OCCUPANCY):]); inner = reader.sized_u32(1_048_576)
                    reader.sized_u32(65_536); reader.end()
                if row['outcome'] in ('success', 'failure', 'resource'):
                    self.assertTrue(inner.startswith(_EXECUTION_V5))
                    body = inner[len(_EXECUTION_V5):]
                    self.assertEqual(_decode_candidate_v5(body, abi)['abi'], abi)
                    with self.assertRaises(ValueError):
                        _decode_candidate(body)
                    for wrong in (0, 1, 2, 4 if abi == 3 else 3, 5, 65535, True):
                        with self.assertRaises(ValueError):
                            _decode_candidate_v5(body, wrong)
                    for length in range(len(body)):
                        with self.assertRaises(ValueError):
                            _decode_candidate_v5(body[:length], abi)
                    with self.assertRaises(ValueError):
                        _decode_candidate_v5(body + b'\0', abi)


if __name__ == '__main__':
    unittest.main()
