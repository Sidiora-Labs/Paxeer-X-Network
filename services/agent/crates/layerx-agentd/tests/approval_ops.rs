use layerx_agent_api::identity::{ActivityType, AgentDid, Asset, AuthorityRef, ExplicitSet};
use layerx_agent_api::prepare::{
    CanonicalBytes, DisclosedAmount, Disclosure, IdempotencyRef, PreparationRef, Prepared,
    SigningPreimage,
};
use layerx_agent_api::{Amount, TimestampSeconds};
use layerx_agentd::approval::{
    ApprovalEnforcement, ApprovalExpiry, ApprovalOperationError, ApprovalOutcome, ApprovalService,
    ApprovalSubmissionQueue, DecisionKey, DecisionRequest, APPROVAL_ENFORCEMENT_NOTICE,
};
use layerx_agentd::budget::{
    reserve, BudgetLimiter, LimitConfig, LimitId, LimitScope, ReservationRequest,
};
use layerx_agentd::capability::CapabilityId;
use layerx_agentd::policy::approval::{
    hold, ApprovalContext, ApprovalError, ApprovalRegistry, ApprovalState, ApproverId,
};
use layerx_agentd::session::SessionId;
use layerx_agentd::store::TenantId;
use layerx_types::ids::Did;
use sha2::{Digest as _, Sha256};

static NEXT_DIRECTORY: AtomicU64 = AtomicU64::new(1);

struct DurableFixture {
    root: PathBuf,
    expiry: ApprovalExpiry,
}

impl DurableFixture {
    fn new(label: &str) -> Self {
        let sequence = NEXT_DIRECTORY.fetch_add(1, Ordering::Relaxed);
        let root = std::env::temp_dir().join(format!(
            "layerx-approval-ops-{label}-{}-{sequence}",
            std::process::id()
        ));
        fs::create_dir_all(&root).unwrap_or_else(|error| panic!("fixture root: {error}"));
        let expiry =
            ApprovalExpiry::open(&root).unwrap_or_else(|error| panic!("expiry: {error:?}"));
        Self { root, expiry }
    }
}

impl Drop for DurableFixture {
    fn drop(&mut self) {
        let _ = fs::remove_dir_all(&self.root);
    }
}

fn digest(bytes: &[u8]) -> [u8; 32] {
    Sha256::digest(bytes).into()
}

fn tenant(value: &str) -> TenantId {
    TenantId::new(value).unwrap_or_else(|error| panic!("tenant: {error}"))
}

fn prepared(id: u8) -> Prepared {
    let canonical = format!("canonical-approved-activity-{id}").into_bytes();
    let actor = AgentDid::new("did:layerx:approval-agent")
        .unwrap_or_else(|error| panic!("actor: {error:?}"));
    Prepared {
        preparation_ref: PreparationRef::new(format!("prepared-{id}"))
            .unwrap_or_else(|error| panic!("preparation: {error:?}")),
        unsigned_canonical_bytes: CanonicalBytes::new(canonical.clone())
            .unwrap_or_else(|error| panic!("canonical: {error:?}")),
        signing_preimage: SigningPreimage::new(vec![id; 32])
            .unwrap_or_else(|error| panic!("preimage: {error:?}")),
        disclosure: Disclosure {
            canonical_digest: digest(&canonical),
            activity_type: ActivityType(7),
            actor: actor.clone(),
            authority: AuthorityRef::new("session-key-1")
                .unwrap_or_else(|error| panic!("authority: {error:?}")),
            counterparties: ExplicitSet::allow(vec![AgentDid::new("did:layerx:merchant")
                .unwrap_or_else(|error| panic!("counterparty: {error:?}"))]),
            amounts: ExplicitSet::allow(vec![DisclosedAmount {
                counterparty: AgentDid::new("did:layerx:merchant")
                    .unwrap_or_else(|error| panic!("counterparty: {error:?}")),
                amount: Amount(50),
            }]),
            asset: Asset::new("LXP").unwrap_or_else(|error| panic!("asset: {error:?}")),
            fee_limit: Amount(2),
            expiry: TimestampSeconds(500),
            idempotency_key: IdempotencyRef::new(format!("idempotency-{id}"))
                .unwrap_or_else(|error| panic!("idempotency: {error:?}")),
        },
        expiry: TimestampSeconds(500),
    }
}

fn context(tenant_id: &TenantId, id: u8) -> ApprovalContext {
    ApprovalContext {
        tenant: tenant_id.clone(),
        agent: Did::new(b"did:layerx:approval-agent")
            .unwrap_or_else(|error| panic!("agent: {error:?}")),
        session: SessionId([2; 32]),
        capability: CapabilityId([3; 32]),
        policy_version: "policy-v2".to_owned(),
        request_id: [id; 32],
    }
}

fn limiter() -> BudgetLimiter {
    BudgetLimiter::new(vec![LimitConfig {
        id: LimitId([9; 16]),
        name: "approval-limit".to_owned(),
        scope: LimitScope::Tenant([1; 32]),
        ceiling: 1_000,
        consumed: 0,
    }])
    .unwrap_or_else(|error| panic!("limiter: {error:?}"))
}

fn approver() -> ApproverId {
    ApproverId::new("human:operator").unwrap_or_else(|error| panic!("approver: {error:?}"))
}

fn decision_key(value: &str) -> DecisionKey {
    DecisionKey::new(value).unwrap_or_else(|error| panic!("decision key: {error:?}"))
}

#[test]
fn approval_service_lists_and_gets_only_the_authenticated_tenant() {
    let fixture = DurableFixture::new("scope");
    let registry = ApprovalRegistry::default();
    let limiter = limiter();
    let alpha = tenant("tenant-alpha");
    let beta = tenant("tenant-beta");
    for id in [1, 2, 3] {
        hold(&registry, context(&alpha, id), prepared(id), 10, 100)
            .unwrap_or_else(|error| panic!("alpha hold: {error:?}"));
    }
    hold(&registry, context(&beta, 4), prepared(4), 10, 100)
        .unwrap_or_else(|error| panic!("beta hold: {error:?}"));

    let service = ApprovalService::new(&registry, &limiter, &fixture.expiry);
    let first = service
        .list(&alpha, None, 2, 11)
        .unwrap_or_else(|error| panic!("first page: {error:?}"));
    assert_eq!(first.approvals.len(), 2);
    assert_eq!(first.next_cursor, Some([2; 32]));
    assert!(first.approvals.iter().all(|record| record.tenant == alpha));
    let second = service
        .list(&alpha, first.next_cursor, 2, 11)
        .unwrap_or_else(|error| panic!("second page: {error:?}"));
    assert_eq!(second.approvals.len(), 1);
    assert_eq!(second.next_cursor, None);

    let record = service
        .get(&alpha, [1; 32], 11)
        .unwrap_or_else(|error| panic!("get: {error:?}"));
    assert_eq!(record.held_activity, prepared(1).disclosure);
    assert_eq!(
        record.canonical_bytes_digest,
        digest(b"canonical-approved-activity-1")
    );
    assert_eq!(record.state, ApprovalState::AwaitingApproval);
    assert_eq!(record.enforcement, ApprovalEnforcement::DaemonOnly);
    assert_eq!(record.authority_notice, APPROVAL_ENFORCEMENT_NOTICE);
    assert!(record.authority_notice.contains("no protocol authority"));
    assert_eq!(
        service.get(&beta, [1; 32], 11),
        Err(ApprovalOperationError::Registry(ApprovalError::NotFound))
    );
}

#[test]
fn approval_releases_only_the_exact_held_preparation_once() {
    let fixture = DurableFixture::new("approve");
    let registry = ApprovalRegistry::default();
    let limiter = limiter();
    let tenant = tenant("tenant-alpha");
    let expected = prepared(5);
    hold(&registry, context(&tenant, 5), expected.clone(), 10, 100)
        .unwrap_or_else(|error| panic!("hold: {error:?}"));
    let service = ApprovalService::new(&registry, &limiter, &fixture.expiry);
    let submissions = ApprovalSubmissionQueue::default();

    let granted = service
        .approve(
            DecisionRequest {
                tenant: &tenant,
                approval_id: [5; 32],
                idempotency_key: &decision_key("approve-5"),
                approver: approver(),
                current_sequence: 11,
            },
            &expected,
            &submissions,
        )
        .unwrap_or_else(|error| panic!("approve: {error:?}"));
    assert_eq!(granted.outcome, ApprovalOutcome::Granted);
    assert_eq!(granted.enforcement, ApprovalEnforcement::DaemonOnly);
    assert_eq!(granted.authority_notice, APPROVAL_ENFORCEMENT_NOTICE);
    let submission_ref = granted
        .submission_ref
        .unwrap_or_else(|| panic!("submission reference missing"));
    assert_eq!(submissions.prepared(submission_ref), Some(expected.clone()));
    assert_eq!(submissions.len(), 1);

    let repeated = service
        .approve(
            DecisionRequest {
                tenant: &tenant,
                approval_id: [5; 32],
                idempotency_key: &decision_key("approve-5"),
                approver: approver(),
                current_sequence: 12,
            },
            &expected,
            &submissions,
        )
        .unwrap_or_else(|error| panic!("repeat: {error:?}"));
    assert_eq!(repeated.outcome, ApprovalOutcome::Granted);
    assert_eq!(submissions.len(), 1);
}

#[test]
fn rejection_is_final_and_releases_the_matching_reservation() {
    let fixture = DurableFixture::new("reject");
    let registry = ApprovalRegistry::default();
    let limiter = limiter();
    let tenant = tenant("tenant-alpha");
    reserve(
        &limiter,
        &ReservationRequest {
            id: [6; 32],
            amount: 50,
            expiry_sequence: 100,
            current_sequence: 10,
            applicable_limits: vec![LimitId([9; 16])],
        },
    )
    .unwrap_or_else(|error| panic!("reserve: {error:?}"));
    hold(&registry, context(&tenant, 6), prepared(6), 10, 100)
        .unwrap_or_else(|error| panic!("hold: {error:?}"));
    assert_eq!(limiter.held_reservations(), Ok(1));

    let service = ApprovalService::new(&registry, &limiter, &fixture.expiry);
    let rejected = service
        .reject(DecisionRequest {
            tenant: &tenant,
            approval_id: [6; 32],
            idempotency_key: &decision_key("reject-6"),
            approver: approver(),
            current_sequence: 11,
        })
        .unwrap_or_else(|error| panic!("reject: {error:?}"));
    assert_eq!(rejected.outcome, ApprovalOutcome::Rejected);
    assert_eq!(limiter.held_reservations(), Ok(0));
    let repeated = service
        .reject(DecisionRequest {
            tenant: &tenant,
            approval_id: [6; 32],
            idempotency_key: &decision_key("reject-6"),
            approver: approver(),
            current_sequence: 12,
        })
        .unwrap_or_else(|error| panic!("repeat: {error:?}"));
    assert_eq!(repeated.outcome, ApprovalOutcome::Rejected);
    assert_eq!(limiter.consumed(LimitId([9; 16])), Ok(0));
}

#[test]
fn deterministic_expiry_returns_a_typed_non_success_outcome() {
    let fixture = DurableFixture::new("expiry");
    let registry = ApprovalRegistry::default();
    let limiter = limiter();
    let tenant = tenant("tenant-alpha");
    hold(&registry, context(&tenant, 7), prepared(7), 10, 20)
        .unwrap_or_else(|error| panic!("hold: {error:?}"));
    let service = ApprovalService::new(&registry, &limiter, &fixture.expiry);
    let submissions = ApprovalSubmissionQueue::default();
    let current = prepared(7);
    let expired = service
        .approve(
            DecisionRequest {
                tenant: &tenant,
                approval_id: [7; 32],
                idempotency_key: &decision_key("late-7"),
                approver: approver(),
                current_sequence: 20,
            },
            &current,
            &submissions,
        )
        .unwrap_or_else(|error| panic!("expiry: {error:?}"));
    assert_eq!(expired.outcome, ApprovalOutcome::Expired);
    assert!(submissions.is_empty());
    assert_eq!(
        service.get(&tenant, [7; 32], 20).map(|record| record.state),
        Ok(ApprovalState::Expired)
    );
}

#[test]
fn changed_underlying_activity_voids_the_hold_as_defective() {
    let fixture = DurableFixture::new("defective");
    let registry = ApprovalRegistry::default();
    let limiter = limiter();
    let tenant = tenant("tenant-alpha");
    let held = prepared(8);
    hold(&registry, context(&tenant, 8), held, 10, 100)
        .unwrap_or_else(|error| panic!("hold: {error:?}"));
    let service = ApprovalService::new(&registry, &limiter, &fixture.expiry);
    let submissions = ApprovalSubmissionQueue::default();

    let defective = service
        .approve(
            DecisionRequest {
                tenant: &tenant,
                approval_id: [8; 32],
                idempotency_key: &decision_key("approve-8"),
                approver: approver(),
                current_sequence: 11,
            },
            &prepared(9),
            &submissions,
        )
        .unwrap_or_else(|error| panic!("changed preparation: {error:?}"));
    assert_eq!(defective.outcome, ApprovalOutcome::Defective);
    assert!(submissions.is_empty());
    assert_eq!(
        service.get(&tenant, [8; 32], 12).map(|record| record.state),
        Ok(ApprovalState::Defective)
    );
}
use std::fs;
use std::path::PathBuf;
use std::sync::atomic::{AtomicU64, Ordering};
