use std::fs;
use std::path::PathBuf;
use std::sync::atomic::{AtomicU64, Ordering};

use layerx_agentd::store::{key, ObjectKind, Store, StoreError, TenantId, TenantScoped};

static NEXT_DIRECTORY: AtomicU64 = AtomicU64::new(1);

fn directory(label: &str) -> PathBuf {
    let sequence = NEXT_DIRECTORY.fetch_add(1, Ordering::Relaxed);
    std::env::temp_dir().join(format!(
        "layerx-tenant-store-{label}-{}-{sequence}",
        std::process::id()
    ))
}

fn tenant(value: &str) -> TenantId {
    TenantId::new(value).unwrap_or_else(|error| panic!("tenant {value}: {error}"))
}

#[test]
fn every_required_durable_object_is_structurally_tenant_scoped() {
    let root = directory("objects");
    let alpha = tenant("alpha");
    let beta = tenant("beta");
    let mut store = Store::open(&root).unwrap_or_else(|error| panic!("store: {error}"));
    let required = [
        ObjectKind::Identity,
        ObjectKind::Session,
        ObjectKind::Capability,
        ObjectKind::Budget,
        ObjectKind::Policy,
        ObjectKind::PreparedActivity,
        ObjectKind::Outbox,
        ObjectKind::Receipt,
        ObjectKind::Subscription,
        ObjectKind::Audit,
    ];

    for (index, kind) in required.into_iter().enumerate() {
        let marker =
            u8::try_from(index).unwrap_or_else(|error| panic!("object index {index}: {error}"));
        let alpha_key = key(alpha.clone(), kind, vec![marker + 1])
            .unwrap_or_else(|error| panic!("alpha key {index}: {error}"));
        let beta_key = key(beta.clone(), kind, vec![marker + 1])
            .unwrap_or_else(|error| panic!("beta key {index}: {error}"));
        assert_ne!(alpha_key, beta_key);
        assert_eq!(alpha_key.tenant(), &alpha);
        assert_eq!(beta_key.tenant(), &beta);
        if kind == ObjectKind::Receipt {
            store
                .put_core_cache(alpha_key.clone(), vec![marker])
                .unwrap_or_else(|error| panic!("receipt persists: {error}"));
        } else {
            store
                .put_local(alpha_key.clone(), vec![marker])
                .unwrap_or_else(|error| panic!("local object persists: {error}"));
        }
        assert!(store.get(&alpha_key).is_some());
        assert!(store.get(&beta_key).is_none());
    }
    let _ = fs::remove_dir_all(root);
}

#[test]
fn a_key_or_scope_without_one_valid_tenant_is_rejected() {
    assert!(matches!(TenantId::new(""), Err(StoreError::InvalidTenant)));
    assert!(matches!(
        TenantId::new("tenant\0spoof"),
        Err(StoreError::InvalidTenant)
    ));
    assert!(matches!(
        key(tenant("alpha"), ObjectKind::Policy, Vec::new()),
        Err(StoreError::InvalidObjectId)
    ));

    let scoped = TenantScoped::new(tenant("alpha"), b"policy".to_vec());
    let (scope, value) = scoped.into_parts();
    assert_eq!(scope.as_str(), "alpha");
    assert_eq!(value, b"policy");
}

#[test]
fn canonical_key_encoding_delimits_tenant_kind_and_object_without_collision() {
    let left = key(
        tenant("tenant:a"),
        ObjectKind::Capability,
        b"b:object".to_vec(),
    )
    .unwrap_or_else(|error| panic!("left key: {error}"));
    let right = key(
        tenant("tenant"),
        ObjectKind::Capability,
        b"a:b:object".to_vec(),
    )
    .unwrap_or_else(|error| panic!("right key: {error}"));
    assert_ne!(left.canonical_bytes(), right.canonical_bytes());
    assert_eq!(&left.canonical_bytes()[..2], &8_u16.to_be_bytes());
    assert_eq!(&right.canonical_bytes()[..2], &6_u16.to_be_bytes());
}

#[test]
fn deterministic_key_fuzz_preserves_tenant_and_object_delimiters() {
    let mut state = 0x9e37_79b9_7f4a_7c15_u64;
    for case in 0..10_000_u64 {
        state ^= state << 7;
        state ^= state >> 9;
        state ^= state << 8;
        let tenant_text = format!("tenant:{}:{}", state, case % 17);
        let tenant = tenant(&tenant_text);
        let mut object_id = state.to_be_bytes().to_vec();
        object_id.extend_from_slice(&case.to_be_bytes());
        object_id.push(b':');
        let key = key(
            tenant.clone(),
            ObjectKind::PreparedActivity,
            object_id.clone(),
        )
        .unwrap_or_else(|error| panic!("fuzzed key {case}: {error}"));
        let encoded = key.canonical_bytes();
        let tenant_length = usize::from(u16::from_be_bytes([encoded[0], encoded[1]]));
        assert_eq!(tenant_length, tenant_text.len());
        assert_eq!(&encoded[2..2 + tenant_length], tenant_text.as_bytes());
        assert_eq!(
            encoded[2 + tenant_length],
            ObjectKind::PreparedActivity as u8
        );
        let length_offset = 3 + tenant_length;
        let object_length = u32::from_be_bytes(
            encoded[length_offset..length_offset + 4]
                .try_into()
                .unwrap_or_else(|error| panic!("four-byte object length: {error}")),
        ) as usize;
        assert_eq!(object_length, object_id.len());
        assert_eq!(&encoded[length_offset + 4..], object_id);
        assert_eq!(key.tenant(), &tenant);
    }
}
