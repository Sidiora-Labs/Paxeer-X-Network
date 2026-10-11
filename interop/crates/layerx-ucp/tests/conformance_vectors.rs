//! UCP conformance harness. The vectors are the first-party suite under
//! `interop/specs/conformance/ucp`, which the gateway deployment pins by
//! identifier, vector count and digest; every record is run through the
//! production `Capability`, `PaymentHandler`, `MerchantProfile`,
//! `UcpIdempotencyKey` and `CheckoutStatus` types.

use std::collections::BTreeMap;

use ed25519_dalek::{Signer as _, SigningKey};
use layerx_interop_gateway::adapter::{AdapterId, ConformanceSuite, PinnedSpec, SpecVersion};
use layerx_proof::inclusion::SequencerAuthorization;
use layerx_ucp::{
    interop_ucp, ucp_adapter_descriptor, verify_sequencer_receipt, Capability, CheckoutStatus,
    CheckoutSubmission, MerchantProfile, NegotiatedCapabilities, PaymentHandler, PlatformProfile,
    UcpError, UcpIdempotencyKey, UCP_CHECKOUT_SPEC_SHA256,
};
use sha2::{Digest as _, Sha256};

const UCP_VERSION: &str = "2026-04-08";
const SUITE: &str = "layerx-ucp-conformance-v1";
const CHECKOUT_CAPABILITY: &str = "dev.ucp.shopping.checkout";
const ORDER_CAPABILITY: &str = "dev.ucp.shopping.order";

const CHECKOUT_STATUS: &str = include_str!("../../../specs/conformance/ucp/checkout-status.json");
const IDEMPOTENCY_KEYS: &str = include_str!("../../../specs/conformance/ucp/idempotency-keys.json");
const MERCHANT_PROFILES: &str =
    include_str!("../../../specs/conformance/ucp/merchant-profiles.json");
const CAPABILITIES: &str = include_str!("../../../specs/conformance/ucp/capabilities.json");
const PAYMENT_HANDLERS: &str = include_str!("../../../specs/conformance/ucp/payment-handlers.json");

/// Every vector file of the suite, by its path relative to the suite directory.
/// The order is the sorted order the deployment renderer walks, so the digest
/// below is the digest `interop/deploy/gateway/render.py` pins.
const SUITE_FILES: [(&str, &str); 5] = [
    ("capabilities.json", CAPABILITIES),
    ("checkout-status.json", CHECKOUT_STATUS),
    ("idempotency-keys.json", IDEMPOTENCY_KEYS),
    ("merchant-profiles.json", MERCHANT_PROFILES),
    ("payment-handlers.json", PAYMENT_HANDLERS),
];

fn records(suite: &str, source: &str) -> Vec<serde_json::Value> {
    let records: Vec<serde_json::Value> =
        serde_json::from_str(suite).unwrap_or_else(|error| panic!("{source}: {error}"));
    assert!(
        !records.is_empty(),
        "{source}: a suite that carries no vector is not a conformance suite"
    );
    records
}

fn field<'a>(record: &'a serde_json::Value, name: &str, source: &str) -> &'a str {
    record[name]
        .as_str()
        .unwrap_or_else(|| panic!("{source}: every vector declares {name}"))
}

fn valid(record: &serde_json::Value, source: &str) -> bool {
    record["valid"]
        .as_bool()
        .unwrap_or_else(|| panic!("{source}: every vector declares whether it must be accepted"))
}

fn refusal(record: &serde_json::Value, source: &str) -> UcpError {
    match field(record, "refusal", source) {
        "InvalidProfile" => UcpError::InvalidProfile,
        "CapabilityUnavailable" => UcpError::CapabilityUnavailable,
        "PaymentHandlerUnavailable" => UcpError::PaymentHandlerUnavailable,
        "PaymentHandlerMismatch" => UcpError::PaymentHandlerMismatch,
        "InvalidIdempotencyKey" => UcpError::InvalidIdempotencyKey,
        "InvalidCheckout" => UcpError::InvalidCheckout,
        "InvalidOrder" => UcpError::InvalidOrder,
        other => panic!("{source}: {other} is not a UCP refusal"),
    }
}

fn handler_of(record: &serde_json::Value, source: &str) -> Result<PaymentHandler, UcpError> {
    PaymentHandler::new(
        field(record, "id", source),
        field(record, "version", source),
        field(record, "spec", source),
        field(record, "schema", source),
    )
}

/// The suite digest, over each vector file's path and bytes in sorted path
/// order: the exact rule `interop/deploy/gateway/render.py` applies to the same
/// files, so the declared suite is the suite this harness runs.
fn suite_digest() -> [u8; 32] {
    let mut hasher = Sha256::new();
    for (path, content) in SUITE_FILES {
        hasher.update(path.as_bytes());
        hasher.update([0]);
        hasher.update(content.as_bytes());
        hasher.update([0]);
    }
    hasher.finalize().into()
}

fn suite_vector_count() -> u64 {
    SUITE_FILES
        .iter()
        .map(|(path, content)| records(content, path).len() as u64)
        .sum()
}

#[test]
fn ucp_adapter_declares_versioned_spec_and_conformance() {
    let vector_count = suite_vector_count();
    let version = SpecVersion::parse("20260408").unwrap_or_else(|error| panic!("version: {error}"));
    let adapter_id = AdapterId::new("ucp").unwrap_or_else(|error| panic!("adapter id: {error}"));
    let spec = PinnedSpec::new(adapter_id.clone(), version, UCP_CHECKOUT_SPEC_SHA256)
        .unwrap_or_else(|error| panic!("spec: {error}"));
    let conformance = ConformanceSuite::new(
        AdapterId::new(SUITE).unwrap_or_else(|error| panic!("conformance id: {error}")),
        vector_count,
        suite_digest(),
    )
    .unwrap_or_else(|error| panic!("conformance: {error}"));

    let descriptor = ucp_adapter_descriptor(spec, conformance)
        .unwrap_or_else(|error| panic!("descriptor: {error}"));

    assert_eq!(descriptor.id(), &adapter_id);
    assert_eq!(descriptor.spec().version().as_str(), "20260408");
    assert_eq!(descriptor.conformance().vector_count(), vector_count);
    assert_eq!(descriptor.conformance().suite().as_str(), SUITE);
    assert_ne!(descriptor.conformance().suite_digest(), [0; 32]);
}

#[test]
fn merchant_profile_validation_refuses_invalid_urls() {
    let source = "interop/specs/conformance/ucp/merchant-profiles.json";
    for record in records(MERCHANT_PROFILES, source) {
        let name = field(&record, "name", source);
        let handler = handler_of(&record["payment_handler"], source)
            .unwrap_or_else(|error| panic!("{name}: payment handler: {error}"));
        let profile = MerchantProfile::layerx(field(&record, "rest_endpoint", source), handler);
        if valid(&record, source) {
            let profile = profile.unwrap_or_else(|error| panic!("{name}: {error}"));
            assert_eq!(
                profile.version(),
                field(&record, "expected_version", source),
                "{name}: profile revision"
            );
            let declared: Vec<&str> = record["expected_capabilities"]
                .as_array()
                .unwrap_or_else(|| panic!("{name}: every valid profile declares its capabilities"))
                .iter()
                .map(|value| {
                    value
                        .as_str()
                        .unwrap_or_else(|| panic!("{name}: capability names are strings"))
                })
                .collect();
            let published: Vec<&str> = profile
                .capabilities()
                .iter()
                .map(Capability::name)
                .collect();
            assert_eq!(published, declared, "{name}: published capabilities");
            assert_eq!(profile.payment_handlers().len(), 1, "{name}: one handler");
        } else {
            assert_eq!(profile.err(), Some(refusal(&record, source)), "{name}");
        }
    }
}

#[test]
fn capability_names_follow_reverse_domain_convention() {
    let source = "interop/specs/conformance/ucp/capabilities.json";
    let mut covered = Vec::new();
    for record in records(CAPABILITIES, source) {
        let name = field(&record, "name", source);
        let declared = field(&record, "capability", source);
        let capability = Capability::new(
            declared,
            field(&record, "version", source),
            field(&record, "spec", source),
            field(&record, "schema", source),
        );
        if valid(&record, source) {
            let capability = capability.unwrap_or_else(|error| panic!("{name}: {error}"));
            let prefix = field(&record, "reverse_domain_prefix", source);
            assert!(
                declared.starts_with(prefix),
                "{name}: {declared} does not follow the {prefix} convention"
            );
            assert_eq!(capability.name(), declared, "{name}");
            assert_eq!(
                capability.version(),
                field(&record, "version", source),
                "{name}"
            );
            assert_eq!(capability.spec(), field(&record, "spec", source), "{name}");
            assert_eq!(
                capability.schema(),
                field(&record, "schema", source),
                "{name}"
            );
            covered.push(declared.to_owned());
        } else {
            assert_eq!(capability.err(), Some(refusal(&record, source)), "{name}");
        }
    }
    for capability in [CHECKOUT_CAPABILITY, ORDER_CAPABILITY] {
        assert!(
            covered.iter().any(|declared| declared == capability),
            "{source}: {capability} is not covered"
        );
    }
}

#[test]
fn idempotency_keys_parse_exact_uuid_format() {
    let source = "interop/specs/conformance/ucp/idempotency-keys.json";
    let mut gateway_keys = Vec::new();
    for record in records(IDEMPOTENCY_KEYS, source) {
        let name = field(&record, "name", source);
        let parsed = UcpIdempotencyKey::parse(field(&record, "key", source));
        if valid(&record, source) {
            let key = parsed.unwrap_or_else(|error| panic!("{name}: {error}"));
            assert_ne!(key.gateway_key(), [0; 32], "{name}");
            gateway_keys.push(key.gateway_key());
        } else {
            assert_eq!(parsed.err(), Some(refusal(&record, source)), "{name}");
        }
    }
    let distinct: std::collections::BTreeSet<[u8; 32]> = gateway_keys.iter().copied().collect();
    assert_eq!(
        distinct.len(),
        gateway_keys.len(),
        "{source}: distinct idempotency keys must not collide"
    );
}

#[test]
fn payment_handler_digest_is_stable_and_collision_resistant() {
    let source = "interop/specs/conformance/ucp/payment-handlers.json";
    let mut groups: BTreeMap<String, Vec<[u8; 32]>> = BTreeMap::new();
    for record in records(PAYMENT_HANDLERS, source) {
        let name = field(&record, "name", source);
        let handler = handler_of(&record, source);
        if valid(&record, source) {
            let handler = handler.unwrap_or_else(|error| panic!("{name}: {error}"));
            assert_eq!(handler.id(), field(&record, "id", source), "{name}");
            assert_eq!(
                handler.version(),
                field(&record, "version", source),
                "{name}"
            );
            groups
                .entry(field(&record, "digest_group", source).to_owned())
                .or_default()
                .push(handler.payment_handler_digest());
        } else {
            assert_eq!(handler.err(), Some(refusal(&record, source)), "{name}");
        }
    }
    assert!(
        groups.len() > 1,
        "{source}: collision resistance needs more than one handler"
    );
    for (group, digests) in &groups {
        let first = digests
            .first()
            .unwrap_or_else(|| panic!("{source}: {group} carries no handler"));
        for digest in digests {
            assert_eq!(
                digest, first,
                "{source}: identical handlers must produce identical digests"
            );
        }
    }
    let distinct: std::collections::BTreeSet<[u8; 32]> = groups
        .values()
        .filter_map(|digests| digests.first().copied())
        .collect();
    assert_eq!(
        distinct.len(),
        groups.len(),
        "{source}: different handlers must produce different digests"
    );
}

#[test]
fn conformance_vectors_cover_all_status_transitions() {
    let source = "interop/specs/conformance/ucp/checkout-status.json";
    let mut covered = Vec::new();
    for record in records(CHECKOUT_STATUS, source) {
        let declared = field(&record, "status", source);
        let status = match declared {
            "incomplete" => CheckoutStatus::Incomplete,
            "requires_escalation" => CheckoutStatus::RequiresEscalation,
            "ready_for_complete" => CheckoutStatus::ReadyForComplete,
            "complete_in_progress" => CheckoutStatus::CompleteInProgress,
            "completed" => CheckoutStatus::Completed,
            "canceled" => CheckoutStatus::Canceled,
            other => panic!("{source}: {other} is not a UCP checkout status"),
        };
        assert!(
            !field(&record, "meaning", source).is_empty(),
            "{source}: {declared} carries no meaning"
        );
        assert!(
            !covered.contains(&status),
            "{source}: {declared} is declared twice"
        );
        covered.push(status);
    }
    for status in [
        CheckoutStatus::Incomplete,
        CheckoutStatus::RequiresEscalation,
        CheckoutStatus::ReadyForComplete,
        CheckoutStatus::CompleteInProgress,
        CheckoutStatus::Completed,
        CheckoutStatus::Canceled,
    ] {
        assert!(
            covered.contains(&status),
            "{source}: the UCP status vocabulary is not fully covered: {status:?}"
        );
    }
    assert_eq!(
        covered.len(),
        6,
        "UCP status vocabulary declares 6 states; conformance must cover all"
    );
}

#[test]
fn ucp_codify_anchor_remains_stable() {
    assert_eq!(interop_ucp(), "ucp-2026-04-08-receipt-backed-commerce");
}

#[test]
fn the_vendored_revision_is_the_declared_capability_revision() {
    assert_eq!(UCP_VERSION, "2026-04-08");
    let capability = Capability::new(
        CHECKOUT_CAPABILITY,
        UCP_VERSION,
        "https://ucp.dev/2026-04-08/specification/checkout",
        "https://ucp.dev/2026-04-08/schemas/shopping/checkout.json",
    )
    .unwrap_or_else(|error| panic!("capability: {error}"));
    assert_eq!(capability.version(), UCP_VERSION);
}

const RECEIPT_KEY_ID: [u8; 32] = [0x31; 32];
const RECEIPT_HEIGHT: u64 = 8;
const PAYMENT_EXACT_TOPIC: [u8; 32] = [
    0xc4, 0xda, 0x5d, 0x6f, 0x75, 0x7c, 0x25, 0x1c, 0xd7, 0x09, 0x47, 0xb7, 0x52, 0x91, 0x7e, 0x56,
    0x71, 0xb6, 0x77, 0x51, 0x7e, 0x3d, 0x95, 0x3f, 0x36, 0x2a, 0x1d, 0xf6, 0x64, 0x8e, 0xf5, 0xb1,
];

fn sequencer_checkout() -> CheckoutSubmission {
    let capability_vectors = records(CAPABILITIES, "capabilities");
    let capability_vector = &capability_vectors[0];
    let capability = Capability::new(
        field(capability_vector, "capability", "capabilities"),
        field(capability_vector, "version", "capabilities"),
        field(capability_vector, "spec", "capabilities"),
        field(capability_vector, "schema", "capabilities"),
    )
    .unwrap_or_else(|error| panic!("capability: {error}"));
    let handler_vectors = records(PAYMENT_HANDLERS, "handlers");
    let handler = handler_of(&handler_vectors[0], "handlers")
        .unwrap_or_else(|error| panic!("handler: {error}"));
    let platform = PlatformProfile {
        profile_url: field(capability_vector, "spec", "capabilities").to_owned(),
        capabilities: vec![capability],
        payment_handlers: vec![handler.clone()],
    };
    let key_vectors = records(IDEMPOTENCY_KEYS, "idempotency");
    CheckoutSubmission {
        checkout_id: "receipt-checkout".to_owned(),
        currency: *b"USD",
        total_minor: 75,
        layerx_asset: [0x42; 32],
        layerx_recipient: [0x43; 32],
        idempotency_key: UcpIdempotencyKey::parse(field(&key_vectors[0], "key", "idempotency"))
            .unwrap_or_else(|error| panic!("idempotency: {error}")),
        negotiated: NegotiatedCapabilities::negotiate(&platform, &handler)
            .unwrap_or_else(|error| panic!("negotiation: {error}")),
    }
}

fn receipt_signer() -> SigningKey {
    SigningKey::from_bytes(&[0x35; 32])
}

fn receipt_key(first: u64, last: u64) -> SequencerAuthorization {
    SequencerAuthorization::new(
        RECEIPT_KEY_ID,
        receipt_signer().verifying_key().to_bytes(),
        first,
        last,
    )
}

fn wire_bytes(encoded: &mut Vec<u8>, bytes: &[u8]) {
    let length = u32::try_from(bytes.len()).unwrap_or_else(|error| panic!("wire length: {error}"));
    encoded.extend_from_slice(&length.to_be_bytes());
    encoded.extend_from_slice(bytes);
}

fn payment_log(submission: &CheckoutSubmission) -> Vec<u8> {
    let mut log = vec![0; 18];
    log.extend_from_slice(&[0x10, 0x1a, 4]);
    log.extend_from_slice(&PAYMENT_EXACT_TOPIC);
    log.extend_from_slice(&[0x41; 32]);
    log.extend_from_slice(&submission.layerx_recipient);
    log.extend_from_slice(&submission.idempotency_key.gateway_key());
    let mut data = Vec::new();
    data.extend_from_slice(&submission.layerx_asset);
    data.extend_from_slice(&[0; 16]);
    data.extend_from_slice(&submission.total_minor.to_be_bytes());
    log.extend_from_slice(&Sha256::digest(&data));
    log
}

fn wire_effect(encoded: &mut Vec<u8>, ordinal: u16, kind: u8, body: &[u8]) {
    encoded.extend_from_slice(&12_u16.to_be_bytes());
    encoded.extend_from_slice(&ordinal.to_be_bytes());
    encoded.extend_from_slice(&0_u16.to_be_bytes());
    encoded.extend_from_slice(&[kind, 0]);
    wire_bytes(encoded, &[0; 32]);
    wire_bytes(encoded, body);
}

fn unsigned_receipt(logs: &[Vec<u8>], height: u64) -> Vec<u8> {
    let activity = [0x51; 32];
    let previous = [0; 32];
    let sequence = 1_u64;
    let mut chain = Sha256::new();
    chain.update(b"PAXEER/v1/sequence\0");
    chain.update(previous);
    chain.update(sequence.to_be_bytes());
    chain.update(activity);
    let mut encoded = Vec::new();
    encoded.extend_from_slice(&2_u16.to_be_bytes());
    encoded.extend_from_slice(&0x5201_u16.to_be_bytes());
    encoded.extend_from_slice(&2_u16.to_be_bytes());
    wire_bytes(&mut encoded, &activity);
    encoded.extend_from_slice(&sequence.to_be_bytes());
    wire_bytes(&mut encoded, &previous);
    wire_bytes(&mut encoded, &chain.finalize());
    wire_bytes(&mut encoded, &Sha256::digest([]));
    encoded.extend_from_slice(&0_i32.to_be_bytes());
    let log_count = u32::try_from(logs.len()).unwrap_or_else(|error| panic!("log count: {error}"));
    encoded.extend_from_slice(&(log_count + 1).to_be_bytes());
    let mut summary = Vec::new();
    summary.extend_from_slice(&1_u64.to_be_bytes());
    summary.extend_from_slice(&1_u64.to_be_bytes());
    summary.extend_from_slice(&1_u64.to_be_bytes());
    summary.extend_from_slice(&1_u32.to_be_bytes());
    summary.extend_from_slice(&[0; 40]);
    summary.extend_from_slice(&Sha256::digest([]));
    summary.extend_from_slice(&0_u32.to_be_bytes());
    summary.extend_from_slice(&log_count.to_be_bytes());
    summary.push(4);
    summary.extend_from_slice(b"upax");
    wire_effect(&mut encoded, 0, 1, &summary);
    for (index, log) in logs.iter().enumerate() {
        let ordinal =
            u16::try_from(index + 1).unwrap_or_else(|error| panic!("log ordinal: {error}"));
        wire_effect(&mut encoded, ordinal, 3, log);
    }
    encoded.extend_from_slice(&1_u128.to_be_bytes());
    let mut batch_id = [0; 32];
    batch_id[24..].copy_from_slice(&height.to_be_bytes());
    wire_bytes(&mut encoded, &batch_id);
    encoded.extend_from_slice(&12_u16.to_be_bytes());
    encoded.extend_from_slice(&1_u32.to_be_bytes());
    encoded.extend_from_slice(&0_u32.to_be_bytes());
    encoded.push(2);
    wire_bytes(&mut encoded, &Sha256::digest(b"upax"));
    encoded.extend_from_slice(&0_u128.to_be_bytes());
    let mut sender = [0; 32];
    sender[31] = 1;
    wire_bytes(&mut encoded, &sender);
    encoded.extend_from_slice(&10_u128.to_be_bytes());
    encoded.extend_from_slice(&9_u128.to_be_bytes());
    encoded.extend_from_slice(&0_u64.to_be_bytes());
    let mut destination = [0; 32];
    destination[30..].copy_from_slice(&[0x10, 0x1a]);
    wire_bytes(&mut encoded, &destination);
    encoded.extend_from_slice(&0_u128.to_be_bytes());
    encoded.extend_from_slice(&0_u128.to_be_bytes());
    wire_bytes(&mut encoded, &[0; 32]);
    wire_bytes(&mut encoded, &[0; 32]);
    wire_bytes(&mut encoded, &RECEIPT_KEY_ID);
    encoded.extend_from_slice(&1_u64.to_be_bytes());
    encoded.push(0);
    encoded
}

fn signed_receipt(unsigned: &[u8]) -> (Vec<u8>, [u8; 32]) {
    assert_eq!(unsigned.last(), Some(&0));
    let mut hash = Sha256::new();
    hash.update(b"LXP/v1/receipt\0");
    hash.update(unsigned);
    let digest: [u8; 32] = hash.finalize().into();
    let signature = receipt_signer().sign(&digest).to_bytes();
    let mut encoded = unsigned[..unsigned.len() - 1].to_vec();
    encoded.push(1);
    wire_bytes(&mut encoded, &signature);
    (encoded, digest)
}

fn single_log_module_offset() -> usize {
    166 + 48 + 113 + 48 + 181 + 16 + 36
}

fn assert_receipt_refused(submission: &CheckoutSubmission, unsigned: &[u8]) {
    let (signed, _) = signed_receipt(unsigned);
    assert_eq!(
        verify_sequencer_receipt(submission, &signed, &[receipt_key(1, 16)]),
        Err(UcpError::ReceiptMismatch),
    );
}

#[test]
fn sequencer_receipt_accepts_exact_payment_and_returns_signature_digest() {
    let submission = sequencer_checkout();
    let unsigned = unsigned_receipt(&[payment_log(&submission)], RECEIPT_HEIGHT);
    let module = single_log_module_offset();
    for operation in [0, 1, 2, 4] {
        let mut unsigned = unsigned.clone();
        unsigned[module + 10] = operation;
        let (signed, digest) = signed_receipt(&unsigned);
        assert_eq!(
            verify_sequencer_receipt(&submission, &signed, &[receipt_key(1, 16)]),
            Ok(digest),
        );
    }
}

#[test]
fn sequencer_receipt_key_height_window_is_inclusive() {
    let submission = sequencer_checkout();
    for height in [1, 16] {
        let (signed, digest) =
            signed_receipt(&unsigned_receipt(&[payment_log(&submission)], height));
        assert_eq!(
            verify_sequencer_receipt(&submission, &signed, &[receipt_key(1, 16)]),
            Ok(digest),
        );
    }
    let (signed, _) = signed_receipt(&unsigned_receipt(
        &[payment_log(&submission)],
        RECEIPT_HEIGHT,
    ));
    for key in [receipt_key(9, 16), receipt_key(1, 7), receipt_key(9, 7)] {
        assert_eq!(
            verify_sequencer_receipt(&submission, &signed, &[key]),
            Err(UcpError::ReceiptMismatch)
        );
    }
}

#[test]
fn sequencer_receipt_refuses_missing_ambiguous_and_wrong_keys() {
    let submission = sequencer_checkout();
    let (signed, digest) = signed_receipt(&unsigned_receipt(
        &[payment_log(&submission)],
        RECEIPT_HEIGHT,
    ));
    let key = receipt_key(1, 16);
    let different = SequencerAuthorization::new([0x32; 32], key.public_key(), 1, 16);
    let wrong_public = SequencerAuthorization::new(
        RECEIPT_KEY_ID,
        SigningKey::from_bytes(&[0x36; 32])
            .verifying_key()
            .to_bytes(),
        1,
        16,
    );
    let weak_public = SequencerAuthorization::new(RECEIPT_KEY_ID, [0; 32], 1, 16);
    for keys in [
        vec![],
        vec![different],
        vec![key, key],
        vec![wrong_public],
        vec![weak_public],
    ] {
        assert_eq!(
            verify_sequencer_receipt(&submission, &signed, &keys),
            Err(UcpError::ReceiptMismatch)
        );
    }
    assert_eq!(
        verify_sequencer_receipt(&submission, &signed, &[different, key]),
        Ok(digest)
    );
}

#[test]
fn sequencer_receipt_refuses_corruption_truncation_and_unsigned_bytes() {
    let submission = sequencer_checkout();
    let unsigned = unsigned_receipt(&[payment_log(&submission)], RECEIPT_HEIGHT);
    let (signed, _) = signed_receipt(&unsigned);
    let keys = [receipt_key(1, 16)];
    for length in 0..signed.len() {
        assert_eq!(
            verify_sequencer_receipt(&submission, &signed[..length], &keys),
            Err(UcpError::ReceiptMismatch)
        );
    }
    for index in [10, 90, signed.len() - 1] {
        let mut corrupt = signed.clone();
        corrupt[index] ^= 1;
        assert_eq!(
            verify_sequencer_receipt(&submission, &corrupt, &keys),
            Err(UcpError::ReceiptMismatch)
        );
    }
    assert_eq!(
        verify_sequencer_receipt(&submission, &unsigned, &keys),
        Err(UcpError::ReceiptMismatch)
    );
    let mut extended = signed;
    extended.push(0);
    assert_eq!(
        verify_sequencer_receipt(&submission, &extended, &keys),
        Err(UcpError::ReceiptMismatch)
    );
}

#[test]
fn sequencer_receipt_refuses_reverts_and_unsupported_metadata() {
    let submission = sequencer_checkout();
    let unsigned = unsigned_receipt(&[payment_log(&submission)], RECEIPT_HEIGHT);
    let module = single_log_module_offset();
    let timestamp = unsigned.len() - 9;
    for (offset, replacement) in [
        (0, 1_u16.to_be_bytes().to_vec()),
        (4, 1_u16.to_be_bytes().to_vec()),
        (10, vec![0; 32]),
        (42, 0_u64.to_be_bytes().to_vec()),
        (90, vec![0x52; 32]),
        (158, 1_i32.to_be_bytes().to_vec()),
        (module - 32, vec![1]),
        (module - 8, 0_u64.to_be_bytes().to_vec()),
        (module, 11_u16.to_be_bytes().to_vec()),
        (module + 2, 2_u32.to_be_bytes().to_vec()),
        (module + 6, 1_u32.to_be_bytes().to_vec()),
        (module + 10, vec![3]),
        (timestamp, 0_u64.to_be_bytes().to_vec()),
    ] {
        let mut invalid = unsigned.clone();
        invalid[offset..offset + replacement.len()].copy_from_slice(&replacement);
        assert_receipt_refused(&submission, &invalid);
    }
}

#[test]
fn sequencer_receipt_refuses_invalid_summary_and_effect_metadata() {
    let submission = sequencer_checkout();
    let unsigned = unsigned_receipt(&[payment_log(&submission)], RECEIPT_HEIGHT);
    let summary_body = 166 + 48;
    let log_start = summary_body + 113;
    for (offset, replacement) in [
        (166, 13_u16.to_be_bytes().to_vec()),
        (168, 1_u16.to_be_bytes().to_vec()),
        (170, 1_u16.to_be_bytes().to_vec()),
        (172, vec![3]),
        (178, vec![1]),
        (summary_body + 104, 2_u32.to_be_bytes().to_vec()),
        (summary_body + 108, vec![0]),
        (summary_body + 108, vec![65]),
        (summary_body + 109, vec![0xff]),
        (summary_body + 109, vec![b'x']),
        (log_start, 13_u16.to_be_bytes().to_vec()),
        (log_start + 2, 0_u16.to_be_bytes().to_vec()),
        (log_start + 4, 1_u16.to_be_bytes().to_vec()),
        (log_start + 6, vec![1]),
        (log_start + 12, vec![1]),
    ] {
        let mut invalid = unsigned.clone();
        invalid[offset..offset + replacement.len()].copy_from_slice(&replacement);
        assert_receipt_refused(&submission, &invalid);
    }
}

#[test]
fn sequencer_receipt_refuses_wrong_settlement_identity() {
    let submission = sequencer_checkout();
    let log = payment_log(&submission);
    for (offset, replacement) in [
        (0, vec![1]),
        (19, vec![0x1b]),
        (20, vec![3]),
        (20, vec![5]),
        (21, vec![0]),
        (53, vec![0; 32]),
        (53, submission.layerx_recipient.to_vec()),
        (85, vec![0x44; 32]),
        (117, vec![0x45; 32]),
    ] {
        let mut invalid = log.clone();
        invalid[offset..offset + replacement.len()].copy_from_slice(&replacement);
        assert_receipt_refused(&submission, &unsigned_receipt(&[invalid], RECEIPT_HEIGHT));
    }
    let mut short = log;
    short.pop();
    assert_receipt_refused(&submission, &unsigned_receipt(&[short], RECEIPT_HEIGHT));
}

#[test]
fn sequencer_receipt_refuses_wrong_settlement_asset_and_amount() {
    let submission = sequencer_checkout();
    for (asset, amount) in [
        ([0x44; 32], submission.total_minor),
        (submission.layerx_asset, 74),
        (submission.layerx_asset, 76),
    ] {
        let mut log = payment_log(&submission);
        let mut data = Vec::new();
        data.extend_from_slice(&asset);
        data.extend_from_slice(&[0; 16]);
        data.extend_from_slice(&amount.to_be_bytes());
        log[149..181].copy_from_slice(&Sha256::digest(&data));
        assert_receipt_refused(&submission, &unsigned_receipt(&[log], RECEIPT_HEIGHT));
    }
}

#[test]
fn sequencer_receipt_requires_one_unambiguous_settlement() {
    let submission = sequencer_checkout();
    let log = payment_log(&submission);
    assert_receipt_refused(&submission, &unsigned_receipt(&[], RECEIPT_HEIGHT));
    assert_receipt_refused(
        &submission,
        &unsigned_receipt(&[log.clone(), log.clone()], RECEIPT_HEIGHT),
    );
    let mut conflicting = log.clone();
    conflicting[85..117].fill(0x44);
    assert_receipt_refused(
        &submission,
        &unsigned_receipt(&[log.clone(), conflicting], RECEIPT_HEIGHT),
    );
    let mut unrelated = log.clone();
    unrelated[117..149].fill(0x45);
    let (signed, digest) = signed_receipt(&unsigned_receipt(&[unrelated, log], RECEIPT_HEIGHT));
    assert_eq!(
        verify_sequencer_receipt(&submission, &signed, &[receipt_key(1, 16)]),
        Ok(digest)
    );
}

#[test]
fn sequencer_receipt_refuses_invalid_checkout() {
    let submission = sequencer_checkout();
    let (signed, _) = signed_receipt(&unsigned_receipt(
        &[payment_log(&submission)],
        RECEIPT_HEIGHT,
    ));
    let mut invalid = submission.clone();
    invalid.total_minor = 0;
    assert_eq!(
        verify_sequencer_receipt(&invalid, &signed, &[receipt_key(1, 16)]),
        Err(UcpError::InvalidCheckout)
    );
    invalid = submission.clone();
    invalid.layerx_asset = [0; 32];
    assert_eq!(
        verify_sequencer_receipt(&invalid, &signed, &[receipt_key(1, 16)]),
        Err(UcpError::InvalidCheckout)
    );
    invalid = submission;
    invalid.layerx_recipient = [0; 32];
    assert_eq!(
        verify_sequencer_receipt(&invalid, &signed, &[receipt_key(1, 16)]),
        Err(UcpError::InvalidCheckout)
    );
}
