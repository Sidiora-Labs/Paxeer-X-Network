use super::*;

fn checked<T, E: std::fmt::Debug>(result: Result<T, E>) -> T {
    result.unwrap_or_else(|error| panic!("native time-window evidence: {error:?}"))
}

fn captured_batch() -> (EvidenceAuthority, Did, RawTimeWindowBatch, AuthenticatedCoreTime) {
    let root = std::path::Path::new(env!("CARGO_MANIFEST_DIR"))
        .join("../../../tests/fixtures/custody/daemon-light-credit-receipt");
    let read = |name: &str| checked(std::fs::read(root.join(name)));
    let header_bytes = read("header");
    let header = checked(decode_batch_header(&header_bytes));
    let signature = checked(read("header.signature").try_into());
    let public = checked(read("sequencer.public").try_into());
    let authority = checked(EvidenceAuthority::pinned_to_handshake(
        header.protocol_version(), header.network_id(), public));
    let observed = checked(authority.authenticate_core_time(&header_bytes, &signature,
        header.batch_number(), header.last_sequence()));
    let kind = checked(ActivityType::new(ModuleId::Bridge, 1));
    let registration = checked(ModuleRegistration::new(ModuleId::Bridge, &[kind]));
    let registry = checked(ModuleRegistry::new(&[registration]));
    let activity_bytes = read("activity");
    let activity = checked(decode_signed(&activity_bytes, &registry));
    let actor = checked(Did::new(activity.actor_did()));
    let receipt = RawReceiptEvidence::new(read("credit.receipt"),
        checked(layerx_proof::merkle::decode_proof(&read("receipt.proof"))),
        header_bytes.clone(), signature);
    let maintenance = RawReceiptEvidence::new(read("maintenance.receipt"),
        checked(layerx_proof::merkle::decode_proof(&read("maintenance.proof"))),
        header_bytes, signature);
    (authority, actor, RawTimeWindowBatch::new(
        vec![RawActivityReceiptEvidence::from_signed_inclusion(activity_bytes, receipt)], maintenance), observed)
}

#[test]
fn actual_native_batch_proves_time_count_and_exact_receipt_denomination() {
    let (authority, actor, batch, observed) = captured_batch();
    assert_eq!(observed.header.batch_number(), 1);
    assert_eq!(observed.header.first_sequence(), 1);
    let seconds = observed.observed_core_ms() / 1_000 + 1;
    let facts = checked(authority.authenticate_cumulative_time_use(&actor, seconds, &observed,
        std::slice::from_ref(&batch)));
    assert_eq!(facts.actor(), &actor);
    assert_eq!(facts.count(), 1);
    assert_eq!(facts.successful_count(), 1);
    assert_eq!(facts.observed_core_ms(), observed.header.timestamp_ms());
    assert_eq!(facts.observed_batch_id(), observed.observed_batch_id());
    assert_eq!(facts.first_covered_sequence(), 1);
    assert_eq!(facts.through_sequence(), observed.header.last_sequence());
    assert_eq!(facts.window_seconds(), seconds);
    let decoded = checked(decode(batch.activities[0].receipt().canonical_receipt()));
    let receipt = decoded.protocol().unwrap_or_else(|| panic!("native protocol receipt"));
    assert_eq!(facts.receipt_amounts().len(), 1);
    assert_eq!(facts.receipt_amounts()[0].asset(), receipt.asset());
    assert_eq!(facts.receipt_amounts()[0].source_account(), receipt.from());
    assert_eq!(facts.receipt_amounts()[0].amount(), receipt.amount());
    let other = checked(Did::new(b"did:layerx:time-window-other"));
    let empty = checked(authority.authenticate_cumulative_time_use(&other, seconds, &observed, &[batch]));
    assert_eq!(empty.count(), 0);
    assert_eq!(empty.successful_count(), 0);
    assert!(empty.receipt_amounts().is_empty());
}

#[test]
fn captured_native_time_evidence_refuses_missing_duplicate_and_altered_material() {
    let (authority, actor, batch, observed) = captured_batch();
    let seconds = observed.observed_core_ms() / 1_000 + 1;
    let refused = |batches: &[RawTimeWindowBatch]| {
        assert!(authority.authenticate_cumulative_time_use(&actor, seconds, &observed, batches).is_err());
    };
    refused(&[]);
    refused(&[batch.clone(), batch.clone()]);
    let mut missing = batch.clone();
    missing.activities.clear();
    refused(&[missing]);
    let mut duplicate = batch.clone();
    duplicate.activities.push(duplicate.activities[0].clone());
    refused(&[duplicate]);
    let mut bad = batch.clone();
    bad.maintenance.canonical_receipt[0] ^= 1;
    refused(&[bad]);
    let mut bad = batch.clone();
    bad.maintenance.header_signature[0] ^= 1;
    refused(&[bad]);
    let mut bad = batch.clone();
    bad.activities[0].canonical_activity[0] ^= 1;
    refused(&[bad]);
    let mut bad = batch.clone();
    match &mut bad.activities[0].receipt {
        CumulativeReceiptEvidence::SignedInclusion(raw) | CumulativeReceiptEvidence::Outcome(raw) => {
            raw.canonical_receipt[0] ^= 1;
        }
    }
    refused(&[bad]);
    let mut bad = batch.clone();
    bad.maintenance.proof = checked(Proof::new(0, batch.maintenance.proof.leaf_count(),
        batch.maintenance.proof.siblings().to_vec()));
    refused(&[bad]);
    assert!(authority.authenticate_cumulative_time_use(&actor, 0, &observed,
        std::slice::from_ref(&batch)).is_err());
    assert!(authority.authenticate_cumulative_time_use(&actor, u64::MAX, &observed, &[batch]).is_err());
}

#[test]
fn observed_native_clock_requires_exact_authority_and_head_coordinate() {
    let (authority, actor, batch, observed) = captured_batch();
    assert!(authority.authenticate_core_time(&observed.canonical_header, &observed.signature,
        observed.header.batch_number() + 1, observed.through_sequence()).is_err());
    assert!(authority.authenticate_core_time(&observed.canonical_header, &observed.signature,
        observed.header.batch_number(), observed.through_sequence() + 1).is_err());
    let wrong_network = checked(EvidenceAuthority::pinned_to_handshake(
        observed.header.protocol_version(), observed.header.network_id() + 1,
        authority.verifier.handshake_pin.unwrap_or_else(|| panic!("fixture pin"))));
    assert!(wrong_network.authenticate_cumulative_time_use(&actor, 1, &observed,
        std::slice::from_ref(&batch)).is_err());
    let wrong_key = checked(EvidenceAuthority::pinned_to_handshake(
        observed.header.protocol_version(), observed.header.network_id(), [1; 32]));
    assert!(wrong_key.authenticate_cumulative_time_use(&actor, 1, &observed, &[batch]).is_err());
}
