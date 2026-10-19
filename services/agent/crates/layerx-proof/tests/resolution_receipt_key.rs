use ed25519_dalek::{Signer as _, SigningKey};
use layerx_proof::inclusion::SequencerAuthorization;
use layerx_proof::receipt::{
    verify_sequencer_signature, ReceiptCheck, ReceiptKeys, VerificationFailure,
};
use layerx_wire::encode::Encoder;
use layerx_wire::hash::receipt_digest;
use layerx_wire::limits::{
    LEGACY_PROTOCOL_VERSION, MAX_MESSAGE_BYTES, PROTOCOL_VERSION, STATE_COMMITMENT_PROTOCOL_VERSION,
};
use layerx_wire::receipt::{decode, encode, encode_unsigned, Receipt};
use sha2::{Digest as _, Sha256};

const FIRST_ID: [u8; 32] = [1; 32];
const SECOND_ID: [u8; 32] = [2; 32];
const SEQUENCER_MODULE_ID: u16 = 12;

#[derive(Clone)]
struct Fields {
    version: u16,
    activity_id: [u8; 32],
    batch_id: [u8; 32],
    key_id: [u8; 32],
    module_id: u16,
    result_code: i32,
}

fn fields(height: u64, key_id: [u8; 32]) -> Fields {
    let mut batch_id = [0; 32];
    batch_id[24..].copy_from_slice(&height.to_be_bytes());
    Fields {
        version: PROTOCOL_VERSION,
        activity_id: [3; 32],
        batch_id,
        key_id,
        module_id: SEQUENCER_MODULE_ID,
        result_code: 0,
    }
}

fn encode_fields(fields: &Fields, signature: Option<[u8; 64]>) -> Vec<u8> {
    let previous = [4; 32];
    let sequence = 7_u64;
    let resulting: [u8; 32] = Sha256::digest(
        [
            b"PAXEER/v1/sequence\0".as_slice(),
            &previous,
            &sequence.to_be_bytes(),
            &fields.activity_id,
        ]
        .concat(),
    )
    .into();
    let empty_digest: [u8; 32] = Sha256::digest([]).into();
    let fee_asset: [u8; 32] = Sha256::digest(b"upax").into();
    let mut from = [0; 32];
    from[12..].fill(6);
    let mut to = [0; 32];
    to[12..].fill(7);
    let mut summary = Encoder::new(256);
    assert_eq!(summary.u64(21_000), Ok(()));
    assert_eq!(summary.u64(21_000), Ok(()));
    assert_eq!(summary.u64(1), Ok(()));
    assert_eq!(summary.u32(1), Ok(()));
    assert_eq!(summary.fixed(&[0; 20]), Ok(()));
    assert_eq!(summary.fixed(&[0; 20]), Ok(()));
    assert_eq!(summary.fixed(&empty_digest), Ok(()));
    assert_eq!(summary.u32(0), Ok(()));
    assert_eq!(summary.u32(0), Ok(()));
    assert_eq!(summary.u8(4), Ok(()));
    assert_eq!(summary.fixed(b"upax"), Ok(()));

    let mut encoder = Encoder::new(MAX_MESSAGE_BYTES);
    assert_eq!(
        encoder.structure_header_version(0x5201, fields.version),
        Ok(())
    );
    assert_eq!(encoder.u16(fields.version), Ok(()));
    assert_eq!(encoder.bytes(&fields.activity_id, 32), Ok(()));
    assert_eq!(encoder.u64(sequence), Ok(()));
    assert_eq!(encoder.bytes(&previous, 32), Ok(()));
    assert_eq!(encoder.bytes(&resulting, 32), Ok(()));
    assert_eq!(encoder.bytes(&empty_digest, 32), Ok(()));
    assert_eq!(encoder.i32(fields.result_code), Ok(()));
    assert_eq!(encoder.sequence_length(1, 512), Ok(()));
    assert_eq!(encoder.u16(fields.module_id), Ok(()));
    assert_eq!(encoder.u16(0), Ok(()));
    assert_eq!(encoder.u16(0), Ok(()));
    assert_eq!(encoder.u8(1), Ok(()));
    assert_eq!(encoder.u8(0), Ok(()));
    assert_eq!(encoder.bytes(&[0; 32], 32), Ok(()));
    assert_eq!(encoder.bytes(&summary.finish(), 256), Ok(()));
    assert_eq!(encoder.u128(21_000), Ok(()));
    assert_eq!(encoder.bytes(&fields.batch_id, 32), Ok(()));
    assert_eq!(encoder.u16(fields.module_id), Ok(()));
    assert_eq!(encoder.u32(1), Ok(()));
    assert_eq!(encoder.u32(0), Ok(()));
    assert_eq!(encoder.u8(2), Ok(()));
    assert_eq!(encoder.bytes(&fee_asset, 32), Ok(()));
    assert_eq!(encoder.u128(1), Ok(()));
    assert_eq!(encoder.bytes(&from, 32), Ok(()));
    assert_eq!(encoder.u128(21_002), Ok(()));
    assert_eq!(
        encoder.u128(if fields.result_code == 0 { 1 } else { 2 }),
        Ok(())
    );
    assert_eq!(encoder.u64(0), Ok(()));
    assert_eq!(encoder.bytes(&to, 32), Ok(()));
    assert_eq!(encoder.u128(0), Ok(()));
    assert_eq!(encoder.u128(u128::from(fields.result_code == 0)), Ok(()));
    assert_eq!(encoder.bytes(&[0; 32], 32), Ok(()));
    assert_eq!(encoder.bytes(&[0; 32], 32), Ok(()));
    assert_eq!(encoder.bytes(&fields.key_id, 32), Ok(()));
    assert_eq!(encoder.u64(1_000), Ok(()));
    assert_eq!(encoder.u8(u8::from(signature.is_some())), Ok(()));
    if let Some(signature) = signature {
        assert_eq!(encoder.bytes(&signature, 64), Ok(()));
    }
    encoder.finish()
}

fn sign(fields: &Fields, key: &SigningKey) -> Vec<u8> {
    let unsigned = encode_fields(fields, None);
    let digest = receipt_digest(&unsigned).unwrap_or_else(|error| panic!("{error:?}"));
    encode_fields(fields, Some(key.sign(&digest).to_bytes()))
}

fn pin(id: [u8; 32], key: &SigningKey, first: u64, last: u64) -> SequencerAuthorization {
    SequencerAuthorization::new(id, key.verifying_key().to_bytes(), first, last)
}

fn refusal(check: ReceiptCheck) -> Result<Receipt, VerificationFailure> {
    Err(VerificationFailure { check })
}

#[test]
fn refuses_key_outside_range() {
    let first = SigningKey::from_bytes(&[8; 32]);
    let second = SigningKey::from_bytes(&[9; 32]);
    let pins = [
        pin(FIRST_ID, &first, 100, 199),
        pin(SECOND_ID, &second, 200, u64::MAX),
    ];
    for (height, id, signer) in [
        (99, FIRST_ID, &first),
        (200, FIRST_ID, &first),
        (150, SECOND_ID, &second),
        (199, SECOND_ID, &second),
    ] {
        let bytes = sign(&fields(height, id), signer);
        assert_eq!(
            verify_sequencer_signature(&bytes, ReceiptKeys::from(&pins)),
            refusal(ReceiptCheck::SequencerSignature),
            "height {height}"
        );
    }
}

#[test]
fn accepts_key_inside_range() {
    let first = SigningKey::from_bytes(&[8; 32]);
    let second = SigningKey::from_bytes(&[9; 32]);
    let pins = [
        pin(FIRST_ID, &first, 100, 199),
        pin(SECOND_ID, &second, 200, u64::MAX),
    ];
    let keys = ReceiptKeys::from(&pins);
    for (height, id, signer) in [
        (100, FIRST_ID, &first),
        (150, FIRST_ID, &first),
        (199, FIRST_ID, &first),
        (200, SECOND_ID, &second),
        (u64::MAX, SECOND_ID, &second),
    ] {
        let bytes = sign(&fields(height, id), signer);
        let expected = decode(&bytes).unwrap_or_else(|error| panic!("{error:?}"));
        let verified =
            verify_sequencer_signature(&bytes, keys).unwrap_or_else(|error| panic!("{error:?}"));
        assert_eq!(verified, expected);
        assert_eq!(encode(&verified), Ok(bytes));
        let protocol = verified
            .protocol()
            .unwrap_or_else(|| panic!("protocol receipt"));
        assert_eq!(protocol.batch_id()[..24], [0; 24]);
        assert_eq!(protocol.batch_id()[24..], height.to_be_bytes());
        assert_eq!(protocol.context_hash(), id);
        let unsigned = encode_unsigned(&verified).unwrap_or_else(|error| panic!("{error:?}"));
        let digest = receipt_digest(&unsigned).unwrap_or_else(|error| panic!("{error:?}"));
        assert_eq!(
            protocol.sequencer_signature(),
            Some(signer.sign(&digest).to_bytes())
        );
    }
}

#[test]
fn accepts_overlapping_host_ranges_in_either_list_order() {
    let first = SigningKey::from_bytes(&[8; 32]);
    let second = SigningKey::from_bytes(&[9; 32]);
    let pins = [
        pin(FIRST_ID, &first, 100, 199),
        pin(SECOND_ID, &second, 100, 199),
    ];
    for pins in [pins, [pins[1], pins[0]]] {
        for (id, signer) in [(FIRST_ID, &first), (SECOND_ID, &second)] {
            let bytes = sign(&fields(150, id), signer);
            let expected = decode(&bytes).unwrap_or_else(|error| panic!("{error:?}"));
            assert_eq!(verify_sequencer_signature(&bytes, &pins), Ok(expected));
        }
    }
}

#[test]
fn refuses_unknown_or_wrong_host_key_identity() {
    let first = SigningKey::from_bytes(&[8; 32]);
    let second = SigningKey::from_bytes(&[9; 32]);
    let pins = [
        pin(FIRST_ID, &first, 100, 199),
        pin(SECOND_ID, &second, 100, 199),
    ];
    for id in [[0; 32], [10; 32], SECOND_ID] {
        assert_eq!(
            verify_sequencer_signature(&sign(&fields(150, id), &first), &pins),
            refusal(ReceiptCheck::SequencerSignature)
        );
    }
}

#[test]
fn refuses_empty_pins_and_reversed_range() {
    let key = SigningKey::from_bytes(&[8; 32]);
    let bytes = sign(&fields(150, FIRST_ID), &key);
    let empty: [SequencerAuthorization; 0] = [];
    assert_eq!(
        verify_sequencer_signature(&bytes, &empty),
        refusal(ReceiptCheck::SequencerSignature)
    );
    let reversed = [pin(FIRST_ID, &key, 200, 100)];
    assert_eq!(
        verify_sequencer_signature(&bytes, &reversed),
        refusal(ReceiptCheck::SequencerSignature)
    );
}

#[test]
fn refuses_duplicate_key_ids_even_when_only_one_range_contains_height() {
    let first = SigningKey::from_bytes(&[8; 32]);
    let second = SigningKey::from_bytes(&[9; 32]);
    let bytes = sign(&fields(150, FIRST_ID), &first);
    let original = pin(FIRST_ID, &first, 100, 199);
    for duplicate in [
        original,
        pin(FIRST_ID, &second, 100, 199),
        pin(FIRST_ID, &second, 200, u64::MAX),
    ] {
        for pins in [[original, duplicate], [duplicate, original]] {
            assert_eq!(
                verify_sequencer_signature(&bytes, &pins),
                refusal(ReceiptCheck::SequencerSignature)
            );
        }
    }
}

#[test]
fn refuses_nonzero_height_prefix_without_truncating_it() {
    let key = SigningKey::from_bytes(&[8; 32]);
    let pins = [pin(FIRST_ID, &key, 100, 199)];
    for index in 0..24 {
        let mut changed = fields(150, FIRST_ID);
        changed.batch_id[index] = 1;
        assert_eq!(
            verify_sequencer_signature(&sign(&changed, &key), &pins),
            refusal(ReceiptCheck::BatchId)
        );
    }
}

#[test]
fn height_and_key_identity_remain_bound_to_the_signature() {
    let key = SigningKey::from_bytes(&[8; 32]);
    let pins = [
        pin(FIRST_ID, &key, 100, 199),
        pin(SECOND_ID, &key, 100, 199),
    ];
    let original = sign(&fields(150, FIRST_ID), &key);
    let signature = decode(&original)
        .unwrap_or_else(|error| panic!("{error:?}"))
        .protocol()
        .and_then(layerx_wire::receipt::ProtocolReceipt::sequencer_signature);
    let digest = receipt_digest(&encode_fields(&fields(150, FIRST_ID), None))
        .unwrap_or_else(|error| panic!("{error:?}"));
    assert_eq!(signature, Some(key.sign(&digest).to_bytes()));
    for changed in [fields(151, FIRST_ID), fields(150, SECOND_ID)] {
        assert_eq!(
            verify_sequencer_signature(&encode_fields(&changed, signature), &pins),
            refusal(ReceiptCheck::SequencerSignature)
        );
    }
}

#[test]
fn preserves_missing_signature_activity_and_protocol_refusals() {
    let key = SigningKey::from_bytes(&[8; 32]);
    let pins = [pin(FIRST_ID, &key, 100, 199)];
    assert_eq!(
        verify_sequencer_signature(&encode_fields(&fields(150, FIRST_ID), None), &pins),
        refusal(ReceiptCheck::MissingSignature)
    );
    let mut zero_activity = fields(150, FIRST_ID);
    zero_activity.activity_id = [0; 32];
    assert_eq!(
        verify_sequencer_signature(&sign(&zero_activity, &key), &pins),
        refusal(ReceiptCheck::ActivityId)
    );
    let mut legacy = fields(150, FIRST_ID);
    legacy.version = LEGACY_PROTOCOL_VERSION;
    assert_eq!(
        verify_sequencer_signature(&sign(&legacy, &key), &pins),
        refusal(ReceiptCheck::ProtocolVersion)
    );
}

#[test]
fn preserves_canonical_decode_and_shape_refusals() {
    let key = SigningKey::from_bytes(&[8; 32]);
    let pins = [pin(FIRST_ID, &key, 100, 199)];
    let bytes = sign(&fields(150, FIRST_ID), &key);
    let mut extended = bytes.clone();
    extended.push(0);
    for malformed in [Vec::new(), bytes[..bytes.len() - 1].to_vec(), extended] {
        assert_eq!(
            verify_sequencer_signature(&malformed, &pins),
            refusal(ReceiptCheck::Decode)
        );
    }
    let mut replay = Encoder::new(106);
    assert_eq!(replay.u16(LEGACY_PROTOCOL_VERSION), Ok(()));
    assert_eq!(replay.u64(7), Ok(()));
    for value in [[3; 32], [4; 32], [5; 32]] {
        assert_eq!(replay.fixed(&value), Ok(()));
    }
    assert_eq!(
        verify_sequencer_signature(&replay.finish(), &pins),
        refusal(ReceiptCheck::ReceiptShape)
    );
}

#[test]
fn refuses_weak_pinned_keys_and_altered_signatures() {
    let key = SigningKey::from_bytes(&[8; 32]);
    let bytes = sign(&fields(150, FIRST_ID), &key);
    let weak = [SequencerAuthorization::new(FIRST_ID, [0; 32], 100, 199)];
    assert_eq!(
        verify_sequencer_signature(&bytes, &weak),
        refusal(ReceiptCheck::SequencerSignature)
    );
    let pins = [pin(FIRST_ID, &key, 100, 199)];
    let mut altered = bytes;
    let last = altered.len() - 1;
    altered[last] ^= 1;
    assert_eq!(
        verify_sequencer_signature(&altered, &pins),
        refusal(ReceiptCheck::SequencerSignature)
    );
}

#[test]
fn refuses_signatures_over_the_wrong_preimage() {
    let key = SigningKey::from_bytes(&[8; 32]);
    let pins = [pin(FIRST_ID, &key, 100, 199)];
    let fields = fields(150, FIRST_ID);
    let unsigned = encode_fields(&fields, None);
    let undomained: [u8; 32] = Sha256::digest(&unsigned).into();
    for signature in [
        key.sign(&unsigned).to_bytes(),
        key.sign(&undomained).to_bytes(),
    ] {
        assert_eq!(
            verify_sequencer_signature(&encode_fields(&fields, Some(signature)), &pins),
            refusal(ReceiptCheck::SequencerSignature)
        );
    }
}

#[test]
fn accepts_a_single_height_and_zero_height_when_explicitly_pinned() {
    let key = SigningKey::from_bytes(&[8; 32]);
    for height in [0, 150, u64::MAX] {
        let pins = [pin(FIRST_ID, &key, height, height)];
        let bytes = sign(&fields(height, FIRST_ID), &key);
        let expected = decode(&bytes).unwrap_or_else(|error| panic!("{error:?}"));
        assert_eq!(verify_sequencer_signature(&bytes, &pins), Ok(expected));
    }
}

#[test]
fn preserves_protocol_three_and_rejected_outcomes() {
    let key = SigningKey::from_bytes(&[8; 32]);
    let pins = [pin(FIRST_ID, &key, 100, 199)];
    for version in [PROTOCOL_VERSION, STATE_COMMITMENT_PROTOCOL_VERSION] {
        for result_code in [0, 1] {
            let mut fields = fields(150, FIRST_ID);
            fields.version = version;
            fields.result_code = result_code;
            let bytes = sign(&fields, &key);
            let receipt = verify_sequencer_signature(&bytes, &pins)
                .unwrap_or_else(|error| panic!("{error:?}"));
            assert_eq!(encode(&receipt), Ok(bytes));
            let protocol = receipt
                .protocol()
                .unwrap_or_else(|| panic!("protocol receipt"));
            assert_eq!(protocol.protocol_version(), version);
            assert_eq!(protocol.result_code(), result_code);
        }
    }
}

#[test]
fn bare_keys_cannot_bypass_ranges_for_chain_receipts() {
    let key = SigningKey::from_bytes(&[8; 32]);
    assert_eq!(
        verify_sequencer_signature(
            &sign(&fields(150, FIRST_ID), &key),
            key.verifying_key().to_bytes()
        ),
        refusal(ReceiptCheck::SequencerSignature)
    );
    let mut malformed_height = fields(150, FIRST_ID);
    malformed_height.batch_id = [10; 32];
    assert_eq!(
        verify_sequencer_signature(
            &sign(&malformed_height, &key),
            key.verifying_key().to_bytes()
        ),
        refusal(ReceiptCheck::SequencerSignature)
    );
}

#[test]
fn bare_key_compatibility_is_limited_to_the_existing_kernel_modules() {
    let key = SigningKey::from_bytes(&[8; 32]);
    let header = include_str!("../../../../../include/layerx/lxp_module.h");
    let count = header
        .split_once("LXP_MODULE_RESERVED_COUNT = ")
        .and_then(|(_, suffix)| suffix.split_once(','))
        .and_then(|(value, _)| value.parse::<u16>().ok())
        .unwrap_or_else(|| panic!("kernel module count"));
    assert_eq!(SEQUENCER_MODULE_ID, count + 1);
    for module_id in 0..=SEQUENCER_MODULE_ID {
        let mut fields = fields(150, FIRST_ID);
        fields.module_id = module_id;
        fields.batch_id = [10; 32];
        let bytes = sign(&fields, &key);
        let expected = if (1..=count).contains(&module_id) {
            Ok(decode(&bytes).unwrap_or_else(|error| panic!("{error:?}")))
        } else {
            refusal(ReceiptCheck::SequencerSignature)
        };
        assert_eq!(
            verify_sequencer_signature(&bytes, key.verifying_key().to_bytes()),
            expected
        );
    }
}
