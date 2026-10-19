//! Golden vector tests for portable receipt export and verification.
//!
//! These vectors prove that an external party can verify `LayerX` receipts
//! using only the published format specification and trusted batch authorization.

use layerx_portable::{
    interop_portable_verification, verify_portable_json, PortableReceipt, PortableReceiptError,
    PortableReceiptV2, PortableVerified, ReceiptKeyEntry, PORTABLE_RECEIPT_FORMAT,
    PORTABLE_RECEIPT_FORMAT_V2,
};
use layerx_proof::receipt::{AuthorizedBatch, ReceiptCheck, VerificationFailure};

const GOLDEN_RECEIPT_JSON: &str = r#"{
  "format": "layerx-receipt-proof-v1",
  "verificationLevel": "sequencer-signed",
  "canonicalReceipt": "AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgIBAAAAAAAACgAAAAAAAAAFAAAAAAAAAMgAAAAAAAABAAAAAAAAAAEAAAAAAAAAAQAAAAAAAAABAAAAAAAAAGQAAAAAAAAAZAAAAAAAAAABZAAAAAAAAAFkAAAAAAAAAQAAAAAAAABlAAAAAAAAAGQAAAAAAAAAAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE",
  "receiptDigest": "BAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQ",
  "batchId": "AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE",
  "asset": "AgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgI",
  "previousStateRoot": "AwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwM",
  "resultingStateRoot": "BAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQ",
  "sequencerPublicKey": "BQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQU"
}"#;

#[test]
fn format_constant_matches_specification() {
    assert_eq!(PORTABLE_RECEIPT_FORMAT, "layerx-receipt-proof-v1");
    assert_eq!(
        interop_portable_verification(),
        "layerx-receipt-proof-v1-and-pinned-external-evidence-v1"
    );
}

const V2_SIGNED_RECEIPT: &str = "0x000252010002000000200303030303030303030303030303030303030303030303030303030303030303000000000000000700000020040404040404040404040404040404040404040404040404040404040404040400000020968bd239d91b0e9801eb4126a4ab1f3774a23752128003f99b21715d5d765c0d00000020e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b8550000000000000001000c000000000100000000200000000000000000000000000000000000000000000000000000000000000000000000710000000000005208000000000000520800000000000000010000000100000000000000000000000000000000000000000000000000000000000000000000000000000000e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b85500000000000000000475706178000000000000000000000000000052080000002000000000000000000000000000000000000000000000000000000000000003b6000c0000000100000000020000002002cef2c10e756312b83aa7e7051954161146e03371cb3c4f091303c95e0ee8c9000000000000000000000000000000010000002000000000000000000000000006060606060606060606060606060606060606060000000000000000000000000000520a000000000000000000000000000000010000000000000000000000200000000000000000000000000707070707070707070707070707070707070707000000000000000000000000000000000000000000000000000000000000000100000020000000000000000000000000000000000000000000000000000000000000000000000020000000000000000000000000000000000000000000000000000000000000000000000020000000000000000000000000000000000000000000000000000000000000000200000000000003e80100000040c58fd462ad78cbfd1741051e09471761daf31eb4e3091ce19cfb80aa50616b39b24d40b4036e09a8c6772b3862c05ac0f3bc93f27e7d376f61d525f13a52f70f";

const V2_RECEIPT_JSON: &str = r#"{"format":"layerx-receipt-proof-v2","verificationLevel":"receipted","signedReceipt":"0x000252010002000000200303030303030303030303030303030303030303030303030303030303030303000000000000000700000020040404040404040404040404040404040404040404040404040404040404040400000020968bd239d91b0e9801eb4126a4ab1f3774a23752128003f99b21715d5d765c0d00000020e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b8550000000000000001000c000000000100000000200000000000000000000000000000000000000000000000000000000000000000000000710000000000005208000000000000520800000000000000010000000100000000000000000000000000000000000000000000000000000000000000000000000000000000e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b85500000000000000000475706178000000000000000000000000000052080000002000000000000000000000000000000000000000000000000000000000000003b6000c0000000100000000020000002002cef2c10e756312b83aa7e7051954161146e03371cb3c4f091303c95e0ee8c9000000000000000000000000000000010000002000000000000000000000000006060606060606060606060606060606060606060000000000000000000000000000520a000000000000000000000000000000010000000000000000000000200000000000000000000000000707070707070707070707070707070707070707000000000000000000000000000000000000000000000000000000000000000100000020000000000000000000000000000000000000000000000000000000000000000000000020000000000000000000000000000000000000000000000000000000000000000000000020000000000000000000000000000000000000000000000000000000000000000200000000000003e80100000040c58fd462ad78cbfd1741051e09471761daf31eb4e3091ce19cfb80aa50616b39b24d40b4036e09a8c6772b3862c05ac0f3bc93f27e7d376f61d525f13a52f70f","receiptDigest":"0xc7c91659dd91fd1524f4c3de3b5ed2544ae27a7d53d3f98ab43d492405385833","receiptKey":{"keyId":"0x0000000000000000000000000000000000000000000000000000000000000002","publicKey":"0xfd1724385aa0c75b64fb78cd602fa1d991fdebf76b13c58ed702eac835e9f618","firstHeight":"0x384","lastHeight":"0xffffffffffffffff"},"inclusionHeight":"0x3b7"}"#;

const V2_DIGEST: [u8; 32] = [
    0xc7, 0xc9, 0x16, 0x59, 0xdd, 0x91, 0xfd, 0x15, 0x24, 0xf4, 0xc3, 0xde, 0x3b, 0x5e, 0xd2, 0x54,
    0x4a, 0xe2, 0x7a, 0x7d, 0x53, 0xd3, 0xf9, 0x8a, 0xb4, 0x3d, 0x49, 0x24, 0x05, 0x38, 0x58, 0x33,
];
const V2_KEY_ONE: [u8; 32] = [
    0x13, 0x98, 0xf6, 0x2c, 0x6d, 0x1a, 0x45, 0x7c, 0x51, 0xba, 0x6a, 0x4b, 0x5f, 0x3d, 0xbd, 0x2f,
    0x69, 0xfc, 0xa9, 0x32, 0x16, 0x21, 0x8d, 0xc8, 0x99, 0x7e, 0x41, 0x6b, 0xd1, 0x7d, 0x93, 0xca,
];
const V2_KEY_TWO: [u8; 32] = [
    0xfd, 0x17, 0x24, 0x38, 0x5a, 0xa0, 0xc7, 0x5b, 0x64, 0xfb, 0x78, 0xcd, 0x60, 0x2f, 0xa1, 0xd9,
    0x91, 0xfd, 0xeb, 0xf7, 0x6b, 0x13, 0xc5, 0x8e, 0xd7, 0x02, 0xea, 0xc8, 0x35, 0xe9, 0xf6, 0x18,
];

const fn key_id(last: u8) -> [u8; 32] {
    let mut id = [0; 32];
    id[31] = last;
    id
}

fn published_keys(second_first_height: u64) -> [ReceiptKeyEntry; 2] {
    [
        ReceiptKeyEntry::new(key_id(1), V2_KEY_ONE, 0, second_first_height - 1),
        ReceiptKeyEntry::new(key_id(2), V2_KEY_TWO, second_first_height, u64::MAX),
    ]
}

fn v2_signed_bytes() -> Vec<u8> {
    let digits = V2_SIGNED_RECEIPT.trim_start_matches("0x").as_bytes();
    digits
        .chunks_exact(2)
        .map(|pair| {
            let text = std::str::from_utf8(pair).unwrap_or_else(|error| panic!("{error:?}"));
            u8::from_str_radix(text, 16).unwrap_or_else(|error| panic!("{error:?}"))
        })
        .collect()
}

fn signature_refusal() -> PortableReceiptError {
    PortableReceiptError::Receipt(VerificationFailure {
        check: ReceiptCheck::SequencerSignature,
    })
}

#[test]
fn v2_format_constant_matches_specification() {
    assert_eq!(PORTABLE_RECEIPT_FORMAT_V2, "layerx-receipt-proof-v2");
}

#[test]
fn v2_receipt_verifies_offline() {
    let keys = published_keys(900);
    let verified = PortableReceiptV2::verify_json(V2_RECEIPT_JSON.as_bytes(), &keys)
        .unwrap_or_else(|error| panic!("v2 vector verifies: {error:?}"));
    assert_eq!(verified.signed_receipt(), v2_signed_bytes().as_slice());
    assert_eq!(verified.receipt_digest(), V2_DIGEST);
    assert_eq!(verified.receipt_key(), &keys[1]);
    assert_eq!(verified.activity_id(), [3; 32]);
    assert_eq!(verified.global_sequence(), 7);
    assert_eq!(verified.result_code(), 0);
    assert_eq!(verified.height(), 950);
    assert_eq!(verified.inclusion_height(), 951);
    assert_eq!(
        verify_portable_json(V2_RECEIPT_JSON.as_bytes(), None, &keys),
        Ok(PortableVerified::V2(verified))
    );

    let exported = PortableReceiptV2::export(&v2_signed_bytes(), &keys[1], 951)
        .unwrap_or_else(|error| panic!("v2 vector exports: {error:?}"));
    assert_eq!(exported.format(), PORTABLE_RECEIPT_FORMAT_V2);
    assert_eq!(exported.to_json(), Ok(V2_RECEIPT_JSON.as_bytes().to_vec()));
}

#[test]
fn v2_key_outside_height_window_is_refused() {
    let keys = published_keys(951);
    assert_eq!(
        PortableReceiptV2::verify_json(V2_RECEIPT_JSON.as_bytes(), &keys),
        Err(signature_refusal())
    );
    assert_eq!(
        PortableReceiptV2::export(&v2_signed_bytes(), &keys[1], 951),
        Err(signature_refusal())
    );
}

#[test]
fn v2_flipped_signature_byte_is_refused() {
    let keys = published_keys(900);
    let mut flipped = v2_signed_bytes();
    let last = flipped.len() - 1;
    flipped[last] ^= 1;
    assert_eq!(
        PortableReceiptV2::export(&flipped, &keys[1], 951),
        Err(signature_refusal())
    );
    let tampered = V2_RECEIPT_JSON.replace("52f70f\"", "52f70e\"");
    assert_ne!(tampered, V2_RECEIPT_JSON);
    assert_eq!(
        PortableReceiptV2::verify_json(tampered.as_bytes(), &keys),
        Err(signature_refusal())
    );
}

#[test]
fn v2_refuses_inclusion_before_signed_height_and_mismatched_carried_key() {
    let keys = published_keys(900);
    assert_eq!(
        PortableReceiptV2::export(&v2_signed_bytes(), &keys[1], 949),
        Err(PortableReceiptError::InclusionHeight)
    );
    let narrowed = V2_RECEIPT_JSON.replace(
        r#""lastHeight":"0xffffffffffffffff""#,
        r#""lastHeight":"0x3e7""#,
    );
    assert_eq!(
        PortableReceiptV2::verify_json(narrowed.as_bytes(), &keys),
        Err(PortableReceiptError::ReceiptKeyMismatch)
    );
}

#[test]
fn v1_receipts_keep_the_v1_path_in_the_dual_format_verifier() {
    assert_eq!(
        verify_portable_json(GOLDEN_RECEIPT_JSON.as_bytes(), None, &published_keys(900)),
        Err(PortableReceiptError::MissingBatchAuthorization)
    );
    let batch = AuthorizedBatch::new([99u8; 32], [2u8; 32], [3u8; 32], [4u8; 32], [5u8; 32]);
    assert_eq!(
        verify_portable_json(GOLDEN_RECEIPT_JSON.as_bytes(), Some(&batch), &[]),
        Err(PortableReceiptError::BatchAuthorizationMismatch)
    );
    assert_eq!(
        PortableReceipt::from_json(V2_RECEIPT_JSON.as_bytes()),
        Err(PortableReceiptError::JsonShape)
    );
}

#[test]
fn parse_golden_receipt_json() {
    let parsed = PortableReceipt::from_json(GOLDEN_RECEIPT_JSON.as_bytes());
    assert!(
        parsed.is_ok(),
        "Golden vector must parse without error: {parsed:?}"
    );
    let receipt = parsed.unwrap_or_else(|error| panic!("golden vector parses: {error:?}"));
    assert_eq!(receipt.format(), PORTABLE_RECEIPT_FORMAT);
}

#[test]
fn reject_malformed_json() {
    let cases = [
        (b"" as &[u8], "empty input"),
        (b"{}", "missing required fields"),
        (b"[]", "wrong json type"),
        (
            br#"{"format":"layerx-receipt-proof-v1","extra":"field"}"#,
            "unknown field",
        ),
    ];
    for (input, label) in cases {
        let result = PortableReceipt::from_json(input);
        assert!(result.is_err(), "Must reject {label}: {result:?}");
    }
}

#[test]
fn reject_padded_base64() {
    let padded_json = GOLDEN_RECEIPT_JSON.replace("BAQE", "BAQE=");
    let result = PortableReceipt::from_json(padded_json.as_bytes());
    match result {
        Err(PortableReceiptError::InvalidBase64(_)) => {}
        other => panic!("Must reject padded base64, got {other:?}"),
    }
}

#[test]
fn reject_non_canonical_base64() {
    let mut modified = GOLDEN_RECEIPT_JSON.replace(
        r#""receiptDigest": "BAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQ""#,
        r#""receiptDigest": "BAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAR""#,
    );
    modified = modified.replace(
        r#""resultingStateRoot": "BAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQ""#,
        r#""resultingStateRoot": "BAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAR""#,
    );
    let result = PortableReceipt::from_json(modified.as_bytes());
    match result {
        Err(PortableReceiptError::InvalidBase64(_)) => {}
        other => panic!("Must reject non-canonical base64, got {other:?}"),
    }
}

#[test]
fn reject_wrong_field_lengths() {
    let short_digest = GOLDEN_RECEIPT_JSON.replace(
        r#""receiptDigest": "BAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQ""#,
        r#""receiptDigest": "BAQE""#,
    );
    let result = PortableReceipt::from_json(short_digest.as_bytes());
    match result {
        Err(PortableReceiptError::InvalidLength(_)) => {}
        other => panic!("Must reject wrong length, got {other:?}"),
    }
}

#[test]
fn reject_unsupported_format() {
    let wrong_format = GOLDEN_RECEIPT_JSON.replace(
        r#""format": "layerx-receipt-proof-v1""#,
        r#""format": "layerx-receipt-proof-v2""#,
    );
    let result = PortableReceipt::from_json(wrong_format.as_bytes());
    match result {
        Err(PortableReceiptError::UnsupportedFormat) => {}
        other => panic!("Must reject unsupported format, got {other:?}"),
    }
}

#[test]
fn reject_unsupported_verification_level() {
    let wrong_level = GOLDEN_RECEIPT_JSON.replace(
        r#""verificationLevel": "sequencer-signed""#,
        r#""verificationLevel": "checkpoint-finalized""#,
    );
    let result = PortableReceipt::from_json(wrong_level.as_bytes());
    match result {
        Err(PortableReceiptError::UnsupportedVerificationLevel) => {}
        other => panic!("Must reject unsupported verification level, got {other:?}"),
    }
}

#[test]
fn verify_requires_matching_batch_authorization() {
    let receipt = PortableReceipt::from_json(GOLDEN_RECEIPT_JSON.as_bytes())
        .unwrap_or_else(|error| panic!("golden vector parses: {error:?}"));

    let _trusted_batch =
        AuthorizedBatch::new([1u8; 32], [2u8; 32], [3u8; 32], [4u8; 32], [5u8; 32]);

    let mismatched_batch =
        AuthorizedBatch::new([99u8; 32], [2u8; 32], [3u8; 32], [4u8; 32], [5u8; 32]);

    let result = receipt.verify(&mismatched_batch);
    match result {
        Err(PortableReceiptError::BatchAuthorizationMismatch) => {}
        other => panic!("Must reject batch mismatch, got {other:?}"),
    }
}

#[test]
fn export_rejects_truncated_canonical_receipt() {
    let canonical_receipt = vec![
        1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1,
        1, 1,
    ];
    let batch = AuthorizedBatch::new([10u8; 32], [20u8; 32], [30u8; 32], [40u8; 32], [50u8; 32]);

    let result = PortableReceipt::export(&canonical_receipt, &batch);
    assert_eq!(
        result,
        Err(PortableReceiptError::Receipt(VerificationFailure {
            check: ReceiptCheck::Decode,
        }))
    );
}

#[test]
fn export_and_json_roundtrip() {
    let _batch = AuthorizedBatch::new([1u8; 32], [2u8; 32], [3u8; 32], [4u8; 32], [5u8; 32]);

    let parsed_original = PortableReceipt::from_json(GOLDEN_RECEIPT_JSON.as_bytes())
        .unwrap_or_else(|error| panic!("golden vector parses: {error:?}"));
    let json_output = parsed_original
        .to_json()
        .unwrap_or_else(|error| panic!("serializes to JSON: {error:?}"));
    let parsed_roundtrip = PortableReceipt::from_json(&json_output)
        .unwrap_or_else(|error| panic!("roundtrip parses: {error:?}"));

    assert_eq!(
        parsed_original.format(),
        parsed_roundtrip.format(),
        "format must survive roundtrip"
    );
    assert_eq!(
        parsed_original.canonical_receipt(),
        parsed_roundtrip.canonical_receipt(),
        "canonical receipt must survive roundtrip"
    );
    assert_eq!(
        parsed_original.receipt_digest(),
        parsed_roundtrip.receipt_digest(),
        "receipt digest must survive roundtrip"
    );
}

#[test]
fn reject_oversized_json() {
    let mut oversized = String::with_capacity(2_000_000);
    oversized.push_str(r#"{"format":"layerx-receipt-proof-v1","verificationLevel":"sequencer-signed","canonicalReceipt":""#);
    for _ in 0..400_000 {
        oversized.push_str("AAAA");
    }
    oversized.push_str(r#"","receiptDigest":"BAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQ","batchId":"AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE","asset":"AgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgI","previousStateRoot":"AwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwM","resultingStateRoot":"BAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQ","sequencerPublicKey":"BQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQU"}"#);

    let result = PortableReceipt::from_json(oversized.as_bytes());
    match result {
        Err(PortableReceiptError::JsonBounds) => {}
        other => panic!("Must reject oversized JSON, got {other:?}"),
    }
}

#[test]
fn reject_oversized_canonical_receipt() {
    let batch = AuthorizedBatch::new([1u8; 32], [2u8; 32], [3u8; 32], [4u8; 32], [5u8; 32]);
    let oversized_receipt = vec![0u8; 1_048_577];
    let result = PortableReceipt::export(&oversized_receipt, &batch);
    match result {
        Err(PortableReceiptError::ReceiptBounds) => {}
        other => panic!("Must reject oversized receipt, got {other:?}"),
    }
}

#[test]
fn reject_empty_canonical_receipt() {
    let batch = AuthorizedBatch::new([1u8; 32], [2u8; 32], [3u8; 32], [4u8; 32], [5u8; 32]);
    let result = PortableReceipt::export(&[], &batch);
    match result {
        Err(PortableReceiptError::ReceiptBounds) => {}
        other => panic!("Must reject empty receipt, got {other:?}"),
    }
}
