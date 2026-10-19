use std::sync::Arc;
use std::thread;

use layerx_agent_api::identity::{ActivityType, AgentDid, Asset, AuthorityRef, ExplicitSet};
use layerx_agent_api::prepare::{
    CanonicalBytes, DisclosedAmount, Disclosure, IdempotencyRef, PreparationRef, Prepared,
    SigningPreimage,
};
use layerx_agent_api::{Amount, TimestampSeconds};
use layerx_agentd::capability::CapabilityId;
use layerx_agentd::policy::approval::{
    decide, expire, hold, ApprovalChoice, ApprovalContext, ApprovalError, ApprovalRegistry,
    ApprovalState, ApproverId,
};
use layerx_agentd::session::SessionId;
use layerx_agentd::store::TenantId;
use layerx_types::ids::Did;
use sha2::{Digest as _, Sha256};

fn digest(bytes: &[u8]) -> [u8; 32] {
    let mut hasher = Sha256::new();
    hasher.update(bytes);
    hasher.finalize().into()
}

fn prepared(bytes: &[u8]) -> Prepared {
    let actor = AgentDid::new("did:layerx:approval-agent")
        .unwrap_or_else(|error| panic!("actor: {error:?}"));
    Prepared {
        preparation_ref: PreparationRef::new("prepared-1")
            .unwrap_or_else(|error| panic!("preparation: {error:?}")),
        unsigned_canonical_bytes: CanonicalBytes::new(bytes.to_vec())
            .unwrap_or_else(|error| panic!("canonical: {error:?}")),
        signing_preimage: SigningPreimage::new(b"signing-preimage".to_vec())
            .unwrap_or_else(|error| panic!("preimage: {error:?}")),
        disclosure: Disclosure {
            canonical_digest: digest(bytes),
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
            idempotency_key: IdempotencyRef::new("idempotency-1")
                .unwrap_or_else(|error| panic!("idempotency: {error:?}")),
        },
        expiry: TimestampSeconds(500),
    }
}

fn context(id: u8) -> ApprovalContext {
    ApprovalContext {
        tenant: TenantId::new("tenant-a").unwrap_or_else(|error| panic!("tenant: {error}")),
        agent: Did::new(b"did:layerx:approval-agent")
            .unwrap_or_else(|error| panic!("agent: {error:?}")),
        session: SessionId([2; 32]),
        capability: CapabilityId([3; 32]),
        policy_version: "policy-v2".to_owned(),
        request_id: [id; 32],
    }
}

fn approver(value: &str) -> ApproverId {
    ApproverId::new(value).unwrap_or_else(|error| panic!("approver: {error:?}"))
}

#[test]
fn expiry_is_deterministic_and_never_approves() {
    let registry = ApprovalRegistry::default();
    let ticket = hold(&registry, context(1), prepared(b"canonical-1"), 10, 20)
        .unwrap_or_else(|error| panic!("hold: {error:?}"));
    assert_eq!(ticket.state, ApprovalState::AwaitingApproval);
    assert_eq!(expire(&registry, 19).map(|entries| entries.len()), Ok(0));
    let expired = expire(&registry, 20).unwrap_or_else(|error| panic!("expire: {error:?}"));
    assert_eq!(expired.len(), 1);
    assert_eq!(expired[0].decision, ApprovalState::Expired);
    assert_eq!(expired[0].verification_level.wire_rank(), 0);
    assert_eq!(
        decide(
            &registry,
            ticket.hold_id,
            approver("operator-a"),
            ApprovalChoice::Approve,
            ticket.disclosure_digest,
            20,
        ),
        Err(ApprovalError::AlreadyDecided(ApprovalState::Expired))
    );
}

#[test]
fn approval_is_bound_to_the_disclosure_from_prepared_bytes() {
    let registry = ApprovalRegistry::default();
    let ticket = hold(&registry, context(2), prepared(b"canonical-2"), 10, 20)
        .unwrap_or_else(|error| panic!("hold: {error:?}"));
    assert_eq!(ticket.disclosure.activity_type, ActivityType(7));
    assert_eq!(ticket.disclosure.amounts.values()[0].amount, Amount(50));
    assert_eq!(
        decide(
            &registry,
            ticket.hold_id,
            approver("operator-a"),
            ApprovalChoice::Approve,
            digest(b"altered-request"),
            11,
        ),
        Err(ApprovalError::DisclosureChanged)
    );
    let audit = decide(
        &registry,
        ticket.hold_id,
        approver("operator-a"),
        ApprovalChoice::Approve,
        ticket.disclosure_digest,
        11,
    )
    .unwrap_or_else(|error| panic!("approve: {error:?}"));
    assert_eq!(audit.decision, ApprovalState::Approved);
    assert_eq!(
        audit.approver.as_ref().map(ApproverId::as_str),
        Some("operator-a")
    );
    assert_eq!(audit.disclosure_digest, digest(b"canonical-2"));
    assert_eq!(audit.idempotency_key, "idempotency-1");
}

#[test]
fn concurrent_approvals_have_exactly_one_winner() {
    let registry = Arc::new(ApprovalRegistry::default());
    let ticket = hold(&registry, context(3), prepared(b"canonical-3"), 10, 30)
        .unwrap_or_else(|error| panic!("hold: {error:?}"));
    let disclosure_digest = ticket.disclosure_digest;
    let mut workers = Vec::new();
    for index in 0..24 {
        let registry = Arc::clone(&registry);
        workers.push(thread::spawn(move || {
            decide(
                &registry,
                [3; 32],
                approver(&format!("operator-{index}")),
                ApprovalChoice::Approve,
                disclosure_digest,
                11,
            )
        }));
    }
    let accepted = workers
        .into_iter()
        .map(thread::JoinHandle::join)
        .filter(|result| result.as_ref().is_ok_and(Result::is_ok))
        .count();
    assert_eq!(accepted, 1);
    let audit = registry
        .audit_entry([3; 32])
        .unwrap_or_else(|error| panic!("audit: {error:?}"))
        .unwrap_or_else(|| panic!("audit entry missing"));
    assert_eq!(audit.decision, ApprovalState::Approved);
}

#[test]
fn unbound_disclosure_digest_is_refused_before_hold() {
    let registry = ApprovalRegistry::default();
    let mut altered = prepared(b"canonical-4");
    altered.disclosure.canonical_digest = [0; 32];
    assert_eq!(
        hold(&registry, context(4), altered, 10, 20),
        Err(ApprovalError::InvalidDisclosureDigest)
    );
}
