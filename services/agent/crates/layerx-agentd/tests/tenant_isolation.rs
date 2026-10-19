use std::collections::BTreeSet;
use std::fs;
use std::path::PathBuf;
use std::sync::atomic::{AtomicU64, Ordering};

use layerx_agentd::limits::admission::Priority;
use layerx_agentd::limits::quota::{
    ClientActivity, Resource, ResourceWrite, SheddingPolicy, TenantQuota,
};
use layerx_agentd::limits::{shed, Quota};
use layerx_agentd::store::{Store, TenantId};
use layerx_agentd::tenant::{
    ChannelBinding, ChannelKind, Config, IsolationError, RedactionPolicy, Retention, SignerBinding,
    SignerMaterial, TenantIsolation,
};
use layerx_types::verify::VerificationLevel;

static NEXT_DIRECTORY: AtomicU64 = AtomicU64::new(1);

fn directory(label: &str) -> PathBuf {
    let sequence = NEXT_DIRECTORY.fetch_add(1, Ordering::Relaxed);
    std::env::temp_dir().join(format!(
        "layerx-tenant-isolation-{label}-{}-{sequence}",
        std::process::id()
    ))
}

fn tenant(value: &str) -> TenantId {
    TenantId::new(value).unwrap_or_else(|error| panic!("tenant {value}: {error:?}"))
}

fn quota_config(tenant: TenantId, limit: usize) -> TenantQuota {
    TenantQuota::new(
        tenant,
        Resource::ALL.into_iter().map(|resource| (resource, limit)),
    )
    .unwrap_or_else(|error| panic!("tenant quota: {error:?}"))
}

#[test]
fn signer_bindings_and_key_references_cannot_cross_tenants() {
    let alpha = tenant("alpha");
    let beta = tenant("beta");
    let mut isolation = TenantIsolation::default();
    isolation
        .bind_signer(
            SignerBinding::new(
                alpha.clone(),
                "primary",
                SignerMaterial::LocalEncryptedReference("keystore://alpha/primary".to_owned()),
            )
            .unwrap_or_else(|error| panic!("alpha binding is valid: {error:?}")),
        )
        .unwrap_or_else(|error| panic!("alpha signer binds: {error:?}"));
    isolation
        .bind_signer(
            SignerBinding::new(
                beta.clone(),
                "primary",
                SignerMaterial::External {
                    endpoint: "unix:///run/layerx/beta-signer.sock".to_owned(),
                    public_key: [9; 32],
                },
            )
            .unwrap_or_else(|error| panic!("beta binding is valid: {error:?}")),
        )
        .unwrap_or_else(|error| panic!("beta signer binds: {error:?}"));
    assert_eq!(
        isolation.bind_signer(
            SignerBinding::new(
                alpha.clone(),
                "primary",
                SignerMaterial::External {
                    endpoint: "unix:///run/layerx/replacement.sock".to_owned(),
                    public_key: [0xff; 32],
                },
            )
            .unwrap_or_else(|error| panic!("replacement binding is structurally valid: {error:?}"))
        ),
        Err(IsolationError::Duplicate)
    );

    assert_eq!(
        isolation
            .signer(&alpha, "primary")
            .map(SignerBinding::tenant),
        Ok(&alpha)
    );
    assert_eq!(
        isolation.signer(&beta, "alpha-only"),
        Err(IsolationError::NotAuthorized)
    );
    assert_eq!(
        isolation.signer(&beta, "missing"),
        Err(IsolationError::NotAuthorized)
    );
    assert_ne!(
        isolation
            .signer(&alpha, "primary")
            .unwrap_or_else(|error| panic!("alpha signer: {error:?}"))
            .material(),
        isolation
            .signer(&beta, "primary")
            .unwrap_or_else(|error| panic!("beta signer: {error:?}"))
            .material()
    );
    assert_eq!(
        isolation
            .signer(&alpha, "primary")
            .unwrap_or_else(|error| panic!("original alpha signer remains: {error:?}"))
            .material(),
        &SignerMaterial::LocalEncryptedReference("keystore://alpha/primary".to_owned())
    );
}

#[test]
fn quota_exhaustion_and_shedding_do_not_starve_another_tenant_or_critical_work() {
    let root = directory("capacity");
    let alpha = tenant("alpha");
    let beta = tenant("beta");
    let mut store = Store::open(&root).unwrap_or_else(|error| panic!("store: {error}"));
    let mut quota = Quota::new(
        [
            quota_config(alpha.clone(), 1),
            quota_config(beta.clone(), 1),
        ],
        SheddingPolicy {
            window_ms: 1_000,
            maximum_requests: 10,
            maximum_retries: 2,
            maximum_identical_operations: 10,
            shed_for_ms: 5_000,
        },
    )
    .unwrap_or_else(|error| panic!("quota config is valid: {error:?}"));
    quota
        .create_resource(
            &mut store,
            &alpha,
            "alpha-client",
            ResourceWrite {
                resource: Resource::OutboxEntry,
                object_id: b"alpha-outbox".to_vec(),
                bytes: b"queued".to_vec(),
            },
            10,
        )
        .unwrap_or_else(|error| panic!("alpha uses its quota: {error:?}"));
    for retry in 0..3_u8 {
        shed(
            &mut quota,
            &mut store,
            ClientActivity {
                tenant: alpha.clone(),
                client_id: "alpha-client".to_owned(),
                operation_digest: [retry; 32],
                retry: true,
                observed_at_ms: 20 + u64::from(retry),
            },
        )
        .unwrap_or_else(|error| panic!("alpha activity {retry} is observed: {error:?}"));
    }

    quota
        .create_resource(
            &mut store,
            &beta,
            "beta-client",
            ResourceWrite {
                resource: Resource::OutboxEntry,
                object_id: b"beta-outbox".to_vec(),
                bytes: b"queued".to_vec(),
            },
            30,
        )
        .unwrap_or_else(|error| panic!("beta retains its own capacity: {error:?}"));
    quota
        .admit_work(&store, &beta, "beta-client", Priority::BulkRead, 30)
        .unwrap_or_else(|error| panic!("alpha shedding does not affect beta: {error:?}"));
    for priority in [Priority::Submission, Priority::ReceiptResolution] {
        quota
            .admit_work(&store, &alpha, "alpha-client", priority, 30)
            .unwrap_or_else(|error| {
                panic!("critical lane {priority:?} stays reserved for shed tenant: {error:?}")
            });
    }
    let _ = fs::remove_dir_all(root);
}

#[test]
fn subscriptions_streams_and_mcp_bindings_hide_cross_tenant_existence() {
    let alpha = tenant("alpha");
    let beta = tenant("beta");
    let mut isolation = TenantIsolation::default();
    for kind in [
        ChannelKind::Subscription,
        ChannelKind::Stream,
        ChannelKind::McpServer,
    ] {
        isolation
            .bind_channel(
                ChannelBinding::new(alpha.clone(), kind, "shared-opaque-id")
                    .unwrap_or_else(|error| panic!("binding {kind:?} is valid: {error:?}")),
            )
            .unwrap_or_else(|error| panic!("channel {kind:?} binds: {error:?}"));
        assert_eq!(
            isolation.channel(&beta, kind, "shared-opaque-id"),
            Err(IsolationError::NotAuthorized)
        );
        assert_eq!(
            isolation.channel(&beta, kind, "missing"),
            Err(IsolationError::NotAuthorized)
        );
    }
    assert_eq!(
        isolation.validate_filter(&alpha, [&alpha, &beta]),
        Err(IsolationError::NotAuthorized)
    );
    assert_eq!(isolation.validate_filter(&alpha, [&alpha, &alpha]), Ok(()));
    assert_eq!(
        format!("{:?}", IsolationError::NotAuthorized),
        "NotAuthorized"
    );
}

#[test]
fn policy_redaction_retention_verification_and_approvals_are_per_tenant() {
    let alpha = tenant("alpha");
    let beta = tenant("beta");
    let mut isolation = TenantIsolation::default();
    isolation
        .set_config(Config {
            tenant: alpha.clone(),
            policy_version: "alpha-policy".to_owned(),
            redaction: RedactionPolicy::Strict,
            retention: Retention {
                events: 100,
                audit: 200,
                receipts: 300,
            },
            verification_default: VerificationLevel::CHECKPOINT_FINALISED,
            approval_required_for: BTreeSet::from([7]),
        })
        .unwrap_or_else(|error| panic!("alpha config is valid: {error:?}"));
    isolation
        .set_config(Config {
            tenant: beta.clone(),
            policy_version: "beta-policy".to_owned(),
            redaction: RedactionPolicy::ReceiptOnly,
            retention: Retention {
                events: 10,
                audit: 20,
                receipts: 30,
            },
            verification_default: VerificationLevel::STATE_PROVEN,
            approval_required_for: BTreeSet::new(),
        })
        .unwrap_or_else(|error| panic!("beta config is valid: {error:?}"));

    let alpha_config = isolation
        .config(&alpha)
        .unwrap_or_else(|error| panic!("alpha config exists: {error:?}"));
    let beta_config = isolation
        .config(&beta)
        .unwrap_or_else(|error| panic!("beta config exists: {error:?}"));
    assert_eq!(alpha_config.policy_version, "alpha-policy");
    assert_eq!(beta_config.policy_version, "beta-policy");
    assert_eq!(alpha_config.redaction, RedactionPolicy::Strict);
    assert_eq!(beta_config.redaction, RedactionPolicy::ReceiptOnly);
    assert!(alpha_config.approval_required_for.contains(&7));
    assert!(!beta_config.approval_required_for.contains(&7));
}
