use std::fs;
use std::path::PathBuf;
use std::sync::atomic::{AtomicU64, Ordering};

use layerx_agentd::limits::admission::Priority;
use layerx_agentd::limits::quota::{
    ClientActivity, QuotaError, Resource, ResourceWrite, SheddingPolicy, SheddingReason,
    TenantQuota,
};
use layerx_agentd::limits::{shed, Quota};
use layerx_agentd::store::{Store, TenantId};

static NEXT_DIRECTORY: AtomicU64 = AtomicU64::new(1);

fn directory(label: &str) -> PathBuf {
    let sequence = NEXT_DIRECTORY.fetch_add(1, Ordering::Relaxed);
    std::env::temp_dir().join(format!(
        "layerx-quota-{label}-{}-{sequence}",
        std::process::id()
    ))
}

fn tenant(value: &str) -> TenantId {
    TenantId::new(value).unwrap_or_else(|error| panic!("tenant {value}: {error}"))
}

fn tenant_quota(tenant: TenantId, limit: usize) -> TenantQuota {
    TenantQuota::new(
        tenant,
        Resource::ALL.into_iter().map(|resource| (resource, limit)),
    )
    .unwrap_or_else(|error| panic!("tenant quota: {error:?}"))
}

fn policy() -> SheddingPolicy {
    SheddingPolicy {
        window_ms: 1_000,
        maximum_requests: 5,
        maximum_retries: 2,
        maximum_identical_operations: 3,
        shed_for_ms: 5_000,
    }
}

fn activity(
    tenant: TenantId,
    client_id: &str,
    digest: u8,
    retry: bool,
    observed_at_ms: u64,
) -> ClientActivity {
    ClientActivity {
        tenant,
        client_id: client_id.to_owned(),
        operation_digest: [digest; 32],
        retry,
        observed_at_ms,
    }
}

#[test]
fn every_durable_resource_refuses_creation_past_its_tenant_quota() {
    let root = directory("resources");
    let tenant = tenant("tenant-a");
    let mut store = Store::open(&root).unwrap_or_else(|error| panic!("store: {error}"));
    let quota = Quota::new([tenant_quota(tenant.clone(), 1)], policy())
        .unwrap_or_else(|error| panic!("quota: {error:?}"));

    for (index, resource) in Resource::ALL.into_iter().enumerate() {
        let index =
            u8::try_from(index).unwrap_or_else(|error| panic!("resource index {index}: {error}"));
        quota
            .create_resource(
                &mut store,
                &tenant,
                "client-a",
                ResourceWrite {
                    resource,
                    object_id: vec![index + 1],
                    bytes: vec![0xa0 + index],
                },
                100,
            )
            .unwrap_or_else(|error| panic!("first {resource:?} object: {error:?}"));
        assert!(matches!(
            quota.create_resource(
                &mut store,
                &tenant,
                "client-a",
                ResourceWrite {
                    resource,
                    object_id: vec![index + 20],
                    bytes: vec![0xb0 + index],
                },
                100,
            ),
            Err(QuotaError::Exhausted {
                resource: exhausted,
                used: 1,
                limit: 1,
                ..
            }) if exhausted == resource
        ));
    }
    drop(store);

    let reopened =
        Store::open(&root).unwrap_or_else(|error| panic!("reopen after restart: {error}"));
    let health = quota
        .health(&reopened, &tenant, 100)
        .unwrap_or_else(|error| panic!("tenant health: {error:?}"));
    assert_eq!(health.resources.len(), 5);
    assert!(health
        .resources
        .iter()
        .all(|resource| resource.used == 1 && resource.remaining == 0));
    let _ = fs::remove_dir_all(root);
}

#[test]
fn retry_storm_sheds_only_the_pathological_client_and_records_the_decision() {
    let root = directory("retry-storm");
    let tenant = tenant("tenant-a");
    let mut store = Store::open(&root).unwrap_or_else(|error| panic!("store: {error}"));
    let mut quota = Quota::new([tenant_quota(tenant.clone(), 10)], policy())
        .unwrap_or_else(|error| panic!("quota: {error:?}"));

    assert_eq!(
        shed(
            &mut quota,
            &mut store,
            activity(tenant.clone(), "bad-client", 1, true, 100)
        )
        .unwrap_or_else(|error| panic!("first retry: {error:?}")),
        None
    );
    assert_eq!(
        shed(
            &mut quota,
            &mut store,
            activity(tenant.clone(), "bad-client", 2, true, 110)
        )
        .unwrap_or_else(|error| panic!("second retry: {error:?}")),
        None
    );
    let decision = shed(
        &mut quota,
        &mut store,
        activity(tenant.clone(), "bad-client", 3, true, 120),
    )
    .unwrap_or_else(|error| panic!("third retry: {error:?}"))
    .unwrap_or_else(|| panic!("retry threshold emits no decision"));
    assert_eq!(decision.reason, SheddingReason::RetryStorm);
    assert_eq!(decision.shed_until_ms, 5_120);

    assert!(matches!(
        quota.admit_work(&store, &tenant, "bad-client", Priority::BulkRead, 121),
        Err(QuotaError::ClientShed {
            retry_after_ms: 4_999,
            ..
        })
    ));
    quota
        .admit_work(&store, &tenant, "healthy-client", Priority::BulkRead, 121)
        .unwrap_or_else(|error| panic!("sibling client admission: {error:?}"));
    drop(store);

    let reopened = Store::open(&root).unwrap_or_else(|error| panic!("reopen: {error}"));
    assert!(matches!(
        quota.admit_work(&reopened, &tenant, "bad-client", Priority::BulkRead, 122),
        Err(QuotaError::ClientShed { .. })
    ));
    let health = quota
        .health(&reopened, &tenant, 122)
        .unwrap_or_else(|error| panic!("shed-state health: {error:?}"));
    assert_eq!(health.actively_shed_clients, vec!["bad-client"]);
    let _ = fs::remove_dir_all(root);
}

#[test]
fn hot_loop_and_request_storm_have_distinct_recorded_reasons() {
    let root = directory("reasons");
    let tenant = tenant("tenant-a");
    let mut store = Store::open(&root).unwrap_or_else(|error| panic!("store: {error}"));
    let mut quota = Quota::new([tenant_quota(tenant.clone(), 10)], policy())
        .unwrap_or_else(|error| panic!("quota: {error:?}"));

    let mut hot_loop = None;
    for observed_at_ms in 100..104 {
        hot_loop = shed(
            &mut quota,
            &mut store,
            activity(tenant.clone(), "hot-client", 9, false, observed_at_ms),
        )
        .unwrap_or_else(|error| panic!("hot-loop observation: {error:?}"));
    }
    assert_eq!(
        hot_loop
            .unwrap_or_else(|| panic!("hot loop is not detected"))
            .reason,
        SheddingReason::HotLoop
    );

    let mut request_storm = None;
    for index in 0..6_u8 {
        request_storm = shed(
            &mut quota,
            &mut store,
            activity(
                tenant.clone(),
                "busy-client",
                index,
                false,
                200 + u64::from(index),
            ),
        )
        .unwrap_or_else(|error| panic!("request observation {index}: {error:?}"));
    }
    assert_eq!(
        request_storm
            .unwrap_or_else(|| panic!("request storm is not detected"))
            .reason,
        SheddingReason::PathologicalClient
    );
    let _ = fs::remove_dir_all(root);
}

#[test]
fn another_tenant_keeps_its_quota_and_capacity_during_shedding() {
    let root = directory("tenant-isolation");
    let alpha = tenant("alpha");
    let beta = tenant("beta");
    let mut store = Store::open(&root).unwrap_or_else(|error| panic!("store: {error}"));
    let mut quota = Quota::new(
        [
            tenant_quota(alpha.clone(), 1),
            tenant_quota(beta.clone(), 2),
        ],
        policy(),
    )
    .unwrap_or_else(|error| panic!("quotas: {error:?}"));

    quota
        .create_resource(
            &mut store,
            &alpha,
            "alpha-client",
            ResourceWrite {
                resource: Resource::Subscription,
                object_id: b"alpha-1".to_vec(),
                bytes: b"record".to_vec(),
            },
            1,
        )
        .unwrap_or_else(|error| panic!("alpha durable object: {error:?}"));
    assert!(matches!(
        quota.create_resource(
            &mut store,
            &alpha,
            "alpha-client",
            ResourceWrite {
                resource: Resource::Subscription,
                object_id: b"alpha-2".to_vec(),
                bytes: b"record".to_vec(),
            },
            1,
        ),
        Err(QuotaError::Exhausted { .. })
    ));

    for retry in 0..3_u8 {
        shed(
            &mut quota,
            &mut store,
            activity(
                alpha.clone(),
                "alpha-client",
                retry,
                true,
                10 + u64::from(retry),
            ),
        )
        .unwrap_or_else(|error| panic!("alpha retry {retry}: {error:?}"));
    }
    quota
        .create_resource(
            &mut store,
            &beta,
            "beta-client",
            ResourceWrite {
                resource: Resource::Subscription,
                object_id: b"beta-1".to_vec(),
                bytes: b"record".to_vec(),
            },
            20,
        )
        .unwrap_or_else(|error| panic!("beta durable object: {error:?}"));
    quota
        .admit_work(&store, &beta, "beta-client", Priority::BulkRead, 20)
        .unwrap_or_else(|error| panic!("beta admission: {error:?}"));
    let health = quota
        .health(&store, &beta, 20)
        .unwrap_or_else(|error| panic!("beta health: {error:?}"));
    assert_eq!(health.resources[0].used, 1);
    assert!(health.actively_shed_clients.is_empty());
    let _ = fs::remove_dir_all(root);
}

#[test]
fn critical_submission_and_receipt_work_remain_reserved_during_a_client_shed() {
    let root = directory("reserved");
    let tenant = tenant("tenant-a");
    let mut store = Store::open(&root).unwrap_or_else(|error| panic!("store: {error}"));
    let mut quota = Quota::new([tenant_quota(tenant.clone(), 10)], policy())
        .unwrap_or_else(|error| panic!("quota: {error:?}"));
    for retry in 0..3_u8 {
        shed(
            &mut quota,
            &mut store,
            activity(
                tenant.clone(),
                "client-a",
                retry,
                true,
                100 + u64::from(retry),
            ),
        )
        .unwrap_or_else(|error| panic!("retry {retry} observation: {error:?}"));
    }

    for priority in [Priority::Submission, Priority::ReceiptResolution] {
        quota
            .admit_work(&store, &tenant, "client-a", priority, 110)
            .unwrap_or_else(|error| panic!("{priority:?} admission: {error:?}"));
    }
    assert!(matches!(
        quota.admit_work(&store, &tenant, "client-a", Priority::Backfill, 110),
        Err(QuotaError::ClientShed { .. })
    ));
    let _ = fs::remove_dir_all(root);
}
