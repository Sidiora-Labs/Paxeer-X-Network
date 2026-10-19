use std::fs;
use std::path::PathBuf;
use std::sync::atomic::{AtomicU64, Ordering};

use layerx_agentd::events::{
    ingest, CoreEvent, EventAttributes, EventIngestor, IngestError, Watermark,
};
use layerx_agentd::store::{ObjectKind, StorageClass, Store, TenantId, TenantKey};
use layerx_types::verify::VerificationLevel;

static NEXT_DIRECTORY: AtomicU64 = AtomicU64::new(1);

fn test_directory(name: &str) -> PathBuf {
    let sequence = NEXT_DIRECTORY.fetch_add(1, Ordering::Relaxed);
    std::env::temp_dir().join(format!(
        "layerx-agentd-ingest-{name}-{}-{sequence}",
        std::process::id()
    ))
}

fn tenant(name: &str) -> TenantId {
    match TenantId::new(name) {
        Ok(value) => value,
        Err(error) => panic!("test tenant must be valid: {error}"),
    }
}

fn event(sequence: u64, marker: u8) -> CoreEvent {
    CoreEvent {
        global_sequence: sequence,
        canonical_bytes: vec![0x41, marker, 0x7f],
        receipt_reference: Some([marker; 32]),
        receipt_verification_level: VerificationLevel::SEQUENCER_SIGNED,
        attributes: EventAttributes {
            agent: "agent-a".to_owned(),
            account: "account-a".to_owned(),
            activity_type: 9,
            module: "asset".to_owned(),
            asset: "LXR".to_owned(),
            counterparty: "counterparty-a".to_owned(),
            result_code: 0,
        },
    }
}

fn key(tenant_id: &TenantId, kind: ObjectKind, object_id: Vec<u8>) -> TenantKey {
    match TenantKey::new(tenant_id.clone(), kind, object_id) {
        Ok(value) => value,
        Err(error) => panic!("test key must be valid: {error}"),
    }
}

#[test]
fn exact_core_events_are_durable_and_buffer_backpressure_drops_nothing() {
    let root = test_directory("ordered");
    let tenant_id = tenant("tenant-a");
    let store = match Store::open(&root) {
        Ok(value) => value,
        Err(error) => panic!("store open failed: {error}"),
    };
    let mut ingestor = match EventIngestor::open(store, tenant_id.clone(), 2, 10) {
        Ok(value) => value,
        Err(error) => panic!("ingestor open failed: {error}"),
    };

    if let Err(error) = ingest(&mut ingestor, event(10, 0x10)) {
        panic!("first ingest failed: {error}");
    }
    if let Err(error) = ingest(&mut ingestor, event(11, 0x11)) {
        panic!("second ingest failed: {error}");
    }
    assert!(matches!(
        ingest(&mut ingestor, event(12, 0x12)),
        Err(IngestError::Backpressure { capacity: 2 })
    ));
    assert_eq!(
        ingestor.watermark(),
        Watermark {
            last_ingested: Some(11),
            next_expected: 12,
        }
    );
    assert_eq!(
        ingestor
            .store()
            .list_object_ids(&tenant_id, ObjectKind::Event)
            .len(),
        2
    );

    assert_eq!(ingestor.take_next(), Some(event(10, 0x10)));
    if let Err(error) = ingest(&mut ingestor, event(12, 0x12)) {
        panic!("ingest after capacity became available failed: {error}");
    }
    assert_eq!(ingestor.take_next(), Some(event(11, 0x11)));
    assert_eq!(ingestor.take_next(), Some(event(12, 0x12)));
    assert!(ingestor.take_next().is_none());

    let event_key = key(&tenant_id, ObjectKind::Event, 10_u64.to_be_bytes().to_vec());
    let Some(stored) = ingestor.store().get(&event_key) else {
        panic!("exact event bytes were not persisted")
    };
    assert_eq!(stored.class(), StorageClass::CoreProducedCache);
    assert_eq!(stored.bytes(), event(10, 0x10).canonical_bytes);

    let mut metadata_id = b"event-evidence:".to_vec();
    metadata_id.extend_from_slice(&10_u64.to_be_bytes());
    let metadata_key = key(&tenant_id, ObjectKind::Configuration, metadata_id);
    let Some(metadata) = ingestor.store().get(&metadata_key) else {
        panic!("event receipt evidence was not persisted")
    };
    let mut expected_metadata = b"LXEM".to_vec();
    expected_metadata.extend_from_slice(&[1, 1]);
    expected_metadata.extend_from_slice(&[0x10; 32]);
    assert_eq!(metadata.class(), StorageClass::LocalOnly);
    assert_eq!(
        &metadata.bytes()[..expected_metadata.len()],
        expected_metadata
    );
    let _ = fs::remove_dir_all(root);
}

#[test]
fn restart_resumes_from_durable_watermark_and_refuses_repeat_or_gap() {
    let root = test_directory("restart");
    let tenant_id = tenant("tenant-a");
    let store = match Store::open(&root) {
        Ok(value) => value,
        Err(error) => panic!("store open failed: {error}"),
    };
    let mut ingestor = match EventIngestor::open(store, tenant_id.clone(), 4, 20) {
        Ok(value) => value,
        Err(error) => panic!("ingestor open failed: {error}"),
    };
    if let Err(error) = ingest(&mut ingestor, event(20, 0x20)) {
        panic!("initial ingest failed: {error}");
    }
    drop(ingestor);

    let restarted_store = match Store::open(&root) {
        Ok(value) => value,
        Err(error) => panic!("store reopen failed: {error}"),
    };
    let mut restarted = match EventIngestor::open(restarted_store, tenant_id.clone(), 4, 0) {
        Ok(value) => value,
        Err(error) => panic!("ingestor restart failed: {error}"),
    };
    assert_eq!(
        restarted.watermark(),
        Watermark {
            last_ingested: Some(20),
            next_expected: 21,
        }
    );
    assert!(matches!(
        ingest(&mut restarted, event(20, 0x20)),
        Err(IngestError::Repeated { sequence: 20 })
    ));
    assert!(matches!(
        ingest(&mut restarted, event(22, 0x22)),
        Err(IngestError::OutOfOrder {
            expected: 21,
            received: 22,
        })
    ));
    assert_eq!(restarted.buffered_len(), 0);
    if let Err(error) = ingest(&mut restarted, event(21, 0x21)) {
        panic!("resumed ingest failed: {error}");
    }
    assert_eq!(restarted.take_next(), Some(event(21, 0x21)));
    assert_eq!(
        restarted
            .store()
            .list_object_ids(&tenant_id, ObjectKind::Event),
        vec![20_u64.to_be_bytes().to_vec(), 21_u64.to_be_bytes().to_vec()]
    );
    let _ = fs::remove_dir_all(root);
}

#[test]
fn invalid_inputs_never_advance_or_persist_the_stream() {
    let root = test_directory("invalid");
    let tenant_id = tenant("tenant-a");
    let store = match Store::open(&root) {
        Ok(value) => value,
        Err(error) => panic!("store open failed: {error}"),
    };
    assert!(matches!(
        EventIngestor::open(store, tenant_id.clone(), 0, 7),
        Err(IngestError::InvalidCapacity)
    ));

    let store = match Store::open(&root) {
        Ok(value) => value,
        Err(error) => panic!("store reopen failed: {error}"),
    };
    let mut ingestor = match EventIngestor::open(store, tenant_id.clone(), 1, 7) {
        Ok(value) => value,
        Err(error) => panic!("ingestor open failed: {error}"),
    };
    let mut empty = event(7, 0x07);
    empty.canonical_bytes.clear();
    assert!(matches!(
        ingest(&mut ingestor, empty),
        Err(IngestError::EmptyCoreEvent)
    ));
    let mut missing_receipt = event(7, 0x07);
    missing_receipt.receipt_reference = None;
    assert!(matches!(
        ingest(&mut ingestor, missing_receipt),
        Err(IngestError::ReceiptEvidenceMismatch)
    ));
    assert_eq!(
        ingestor.watermark(),
        Watermark {
            last_ingested: None,
            next_expected: 7,
        }
    );
    assert!(ingestor
        .store()
        .list_object_ids(&tenant_id, ObjectKind::Event)
        .is_empty());
    let _ = fs::remove_dir_all(root);
}
