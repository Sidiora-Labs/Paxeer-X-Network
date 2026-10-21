use std::fs;
use std::path::Path;

use layerx_intents::{inspect_intent, IntentErrorReason, RejectReason};
use layerx_types::account::{AccountError, AccountId};
use layerx_types::activity::{ActivityBuildError, Signature};
use layerx_types::ids::Did;
use layerx_types::limits::{MAX_ACCOUNT_NAME_BYTES, MAX_DID_BYTES, MAX_SIGNATURE_BYTES};
use proptest::prelude::*;

proptest! {
    #[test]
    fn every_overlong_did_is_rejected_without_truncation(
        bytes in prop::collection::vec(any::<u8>(), (MAX_DID_BYTES + 1)..=(MAX_DID_BYTES + 512)),
    ) {
        let error = Did::new(&bytes).err().ok_or_else(|| TestCaseError::fail("overlong DID accepted"))?;
        prop_assert_eq!(error.actual, bytes.len());
        prop_assert_eq!(error.maximum, MAX_DID_BYTES);
    }

    #[test]
    fn every_overlong_signature_is_rejected_without_truncation(
        bytes in prop::collection::vec(any::<u8>(), (MAX_SIGNATURE_BYTES + 1)..=(MAX_SIGNATURE_BYTES + 512)),
    ) {
        prop_assert_eq!(
            Signature::new(&bytes),
            Err(ActivityBuildError::SignatureLength(bytes.len()))
        );
    }

    #[test]
    fn every_overlong_account_is_rejected_without_normalisation(
        extra in prop::collection::vec(prop::char::range('a', 'z'), 1..=512),
    ) {
        let mut value = "agent:".to_owned();
        value.extend(std::iter::repeat_n('a', MAX_ACCOUNT_NAME_BYTES));
        value.extend(extra);
        prop_assert_eq!(AccountId::parse(&value), Err(AccountError::Length));
    }
}

#[test]
fn unknown_discriminants_are_deterministic_and_input_preserving() {
    let unknown_version = [0xff, 0xff, 0, 1, 9, 8, 7];
    let first = inspect_intent(&unknown_version)
        .err()
        .unwrap_or_else(|| panic!("unknown version accepted"));
    let second = inspect_intent(&unknown_version)
        .err()
        .unwrap_or_else(|| panic!("unknown version accepted on second run"));
    assert_eq!(first, second);
    assert_eq!(first.input(), unknown_version);
    assert_eq!(first.reason(), RejectReason::UnknownVersion(u16::MAX));
}

#[test]
fn intent_crate_contains_no_floating_point_types_or_arithmetic() {
    let source = Path::new(env!("CARGO_MANIFEST_DIR")).join("src");
    for entry in fs::read_dir(source).unwrap_or_else(|error| panic!("read source: {error}")) {
        let path = entry
            .unwrap_or_else(|error| panic!("source entry: {error}"))
            .path();
        if path.extension().and_then(|value| value.to_str()) != Some("rs") {
            continue;
        }
        let text = fs::read_to_string(&path)
            .unwrap_or_else(|error| panic!("read {}: {error}", path.display()));
        for forbidden in ["f32", "f64", "as f32", "as f64", "from_f32", "from_f64"] {
            assert!(
                !text
                    .split(|character: char| !character.is_ascii_alphanumeric() && character != '_')
                    .any(|token| token == forbidden),
                "{} contains forbidden floating-point token {forbidden}",
                path.display()
            );
        }
    }
}

#[test]
fn typed_range_reasons_remain_closed() {
    assert_ne!(IntentErrorReason::Zero, IntentErrorReason::InvalidRange);
}
