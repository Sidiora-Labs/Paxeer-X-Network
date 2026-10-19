use std::collections::{BTreeMap, BTreeSet};
use std::fs;
use std::path::PathBuf;
use std::sync::atomic::{AtomicU64, Ordering};

use layerx_agentd::audit::{
    protect_payload, read_entries, reconstruct_session, record, redact, Coverage, DataClass,
    Decision, Entry, EventClass, Log, OutputSurface, RecordError, StoredReceiptEvidence,
};
use layerx_agentd::identity::ProtocolAuthority;
use layerx_agentd::session::SessionId;
use layerx_agentd::store::TenantId;
use layerx_agentd::tenant::{Config, RedactionPolicy, Retention};
use layerx_types::ids::{ActivityId, Did, IdempotencyKey};
use layerx_types::verify::VerificationLevel;

static NEXT_DIRECTORY: AtomicU64 = AtomicU64::new(1);

fn root(label: &str) -> PathBuf {
    let sequence = NEXT_DIRECTORY.fetch_add(1, Ordering::Relaxed);
    std::env::temp_dir().join(format!(
        "layerx-audit-coverage-{label}-{}-{sequence}",
        std::process::id()
    ))
}

fn tenant() -> TenantId {
    TenantId::new("tenant-a").unwrap_or_else(|error| panic!("tenant: {error}"))
}

fn config() -> Config {
    Config {
        tenant: tenant(),
        policy_version: "policy-v7".to_owned(),
        redaction: RedactionPolicy::Standard,
        retention: Retention {
            events: 100,
            audit: 100,
            receipts: 100,
        },
        verification_default: VerificationLevel::STATE_PROVEN,
        approval_required_for: BTreeSet::default(),
    }
}

fn entry(class: EventClass) -> Entry {
    let decision = match class {
        EventClass::CapabilityDecision | EventClass::PolicyDecision => Decision::Allowed,
        EventClass::Submission => Decision::Submitted,
        EventClass::TerminalOutcome => Decision::Executed,
        EventClass::ConfigurationChange
        | EventClass::AdministrativeAction
        | EventClass::SubscriptionChange => Decision::Changed,
        EventClass::SignatureRequest => Decision::Requested,
        _ => Decision::Observed,
    };
    let reason = format!("{class:?}-evidence");
    let reason = redact(
        &config(),
        &tenant(),
        OutputSurface::Audit,
        DataClass::PublicText,
        reason.as_bytes(),
        10,
    )
    .unwrap_or_else(|error| panic!("reason redaction: {error}"))
    .value;
    Entry {
        class,
        observed_at_ms: 1_000,
        tenant: tenant(),
        agent: Did::new(b"did:layerx:agent-a")
            .unwrap_or_else(|error| panic!("agent DID: {error:?}")),
        session: Some(SessionId([1; 32])),
        capability: Some([2; 32]),
        policy_version: "policy-v7".to_owned(),
        request_id: [3; 32],
        idempotency_key: Some(IdempotencyKey::new([4; 32])),
        decision,
        reason,
        resulting_activity_id: (class == EventClass::TerminalOutcome)
            .then(|| ActivityId::new([5; 32])),
        verification_level: if class == EventClass::TerminalOutcome {
            VerificationLevel::STATE_PROVEN
        } else {
            VerificationLevel::UNVERIFIED
        },
        protocol_authority: (class == EventClass::Submission)
            .then_some(ProtocolAuthority::CapabilityGrant([6; 32])),
        submitted_bytes: (class == EventClass::Submission)
            .then(|| protect_payload(&config(), 10, 10, b"exact-canonical-signed-activity")),
        receipt_id: (class == EventClass::TerminalOutcome).then_some([7; 32]),
    }
}

#[test]
fn every_required_event_class_is_recorded_and_round_trips_every_field() {
    let root = root("all-classes");
    let mut log = Log::open(&root, &tenant()).unwrap_or_else(|error| panic!("open: {error}"));
    let path = log.path().to_path_buf();
    let mut coverage = Coverage::default();
    let mut operations = 0_u64;
    let expected: Vec<_> = EventClass::ALL.into_iter().map(entry).collect();
    for item in &expected {
        record(&mut log, &mut coverage, item, || operations += 1)
            .unwrap_or_else(|error| panic!("record {:?}: {error}", item.class));
    }
    assert_eq!(operations, EventClass::ALL.len() as u64);
    coverage
        .require_complete()
        .unwrap_or_else(|error| panic!("coverage: {error}"));
    assert!(EventClass::ALL
        .into_iter()
        .all(|class| coverage.count(class) == 1));
    drop(log);
    let restored = read_entries(&path).unwrap_or_else(|error| panic!("read entries: {error}"));
    assert_eq!(restored, expected);
    Coverage::from_entries(&restored)
        .require_complete()
        .unwrap_or_else(|error| panic!("restored coverage: {error}"));
    let _ = fs::remove_dir_all(root);
}

#[test]
fn recorded_session_reconstructs_decision_bytes_authority_and_core_receipt() {
    let root = root("reconstruct");
    let mut log = Log::open(&root, &tenant()).unwrap_or_else(|error| panic!("open: {error}"));
    let path = log.path().to_path_buf();
    let mut coverage = Coverage::default();
    for class in [
        EventClass::CapabilityDecision,
        EventClass::PolicyDecision,
        EventClass::Preparation,
        EventClass::SignatureRequest,
        EventClass::Submission,
        EventClass::TerminalOutcome,
    ] {
        record(&mut log, &mut coverage, &entry(class), || ())
            .unwrap_or_else(|error| panic!("record {class:?}: {error}"));
    }
    drop(log);
    let restored = read_entries(&path).unwrap_or_else(|error| panic!("read entries: {error}"));
    let receipts = BTreeMap::from([(
        [7; 32],
        StoredReceiptEvidence {
            submitted_bytes: b"exact-canonical-signed-activity".to_vec(),
            core_receipt_bytes: b"exact-core-produced-receipt".to_vec(),
        },
    )]);
    let replay = reconstruct_session(
        &restored,
        &receipts,
        SessionId([1; 32]),
        IdempotencyKey::new([4; 32]),
    )
    .unwrap_or_else(|error| panic!("reconstruct: {error}"));
    assert_eq!(replay.decisions.len(), 6);
    assert_eq!(replay.submitted_bytes, b"exact-canonical-signed-activity");
    assert_eq!(
        replay.protocol_authority,
        ProtocolAuthority::CapabilityGrant([6; 32])
    );
    assert_eq!(replay.resulting_activity_id, ActivityId::new([5; 32]));
    assert_eq!(replay.core_receipt_bytes, b"exact-core-produced-receipt");
    assert_eq!(replay.verification_level, VerificationLevel::STATE_PROVEN);
    let _ = fs::remove_dir_all(root);
}

#[test]
fn missing_event_class_and_invalid_submission_are_build_breaking() {
    let entries: Vec<_> = EventClass::ALL
        .into_iter()
        .filter(|class| *class != EventClass::AdministrativeAction)
        .map(entry)
        .collect();
    assert!(matches!(
        Coverage::from_entries(&entries).require_complete(),
        Err(RecordError::IncompleteCoverage(missing))
            if missing == vec![EventClass::AdministrativeAction]
    ));

    let root = root("invalid");
    let mut log = Log::open(&root, &tenant()).unwrap_or_else(|error| panic!("open: {error}"));
    let mut coverage = Coverage::default();
    let mut invalid = entry(EventClass::Submission);
    invalid.protocol_authority = None;
    let operation_ran = std::cell::Cell::new(false);
    assert!(matches!(
        record(&mut log, &mut coverage, &invalid, || operation_ran
            .set(true)),
        Err(RecordError::Invalid(_))
    ));
    assert!(!operation_ran.get());
    assert_eq!(log.entries(), 0);
    let _ = fs::remove_dir_all(root);
}
