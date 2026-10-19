use std::fs;
use std::sync::atomic::{AtomicU64, Ordering};

use layerx_agent_api::identity::{ActivityType, AgentDid, Asset, AuthorityRef, ExplicitSet};
use layerx_agent_api::prepare::{
    CanonicalBytes, DisclosedAmount, Disclosure, IdempotencyRef, PreparationRef, Prepared,
    SigningPreimage,
};
use layerx_agent_api::{Amount, TimestampSeconds};
use layerx_agentd::capability::CapabilityId;
use layerx_agentd::policy::approval::{
    ApprovalContext, ApprovalError as DaemonApprovalError, ApprovalRegistry, ApprovalState,
    ApproverId,
};
use layerx_agentd::session::SessionId;
use layerx_agentd::store::TenantId;
use layerx_mcp::approval::{approve, expire, require, ApprovalError, ApprovalPolicy, Requirement};
use layerx_types::ids::Did;
use sha2::{Digest, Sha256};

static NEXT_DIRECTORY: AtomicU64 = AtomicU64::new(1);

fn directory(label: &str) -> std::path::PathBuf {
    let sequence = NEXT_DIRECTORY.fetch_add(1, Ordering::Relaxed);
    std::env::temp_dir().join(format!(
        "layerx-mcp-approval-{label}-{}-{sequence}",
        std::process::id()
    ))
}

fn prepared(amount: u128, request_id: u8) -> Prepared {
    let canonical = format!("canonical-preparation-{amount}-{request_id}").into_bytes();
    let digest: [u8; 32] = Sha256::digest(&canonical).into();
    let actor = AgentDid::new("did:layerx:approver-test")
        .unwrap_or_else(|error| panic!("agent: {error:?}"));
    Prepared {
        preparation_ref: PreparationRef::new(format!("preparation-{request_id}"))
            .unwrap_or_else(|error| panic!("preparation: {error:?}")),
        unsigned_canonical_bytes: CanonicalBytes::new(canonical)
            .unwrap_or_else(|error| panic!("canonical: {error:?}")),
        signing_preimage: SigningPreimage::new(vec![request_id; 32])
            .unwrap_or_else(|error| panic!("preimage: {error:?}")),
        disclosure: Disclosure {
            canonical_digest: digest,
            activity_type: ActivityType(7),
            actor: actor.clone(),
            authority: AuthorityRef::new("capability:9")
                .unwrap_or_else(|error| panic!("authority: {error:?}")),
            counterparties: ExplicitSet::allow(vec![AgentDid::new("did:layerx:counterparty")
                .unwrap_or_else(|error| panic!("counterparty: {error:?}"))]),
            amounts: ExplicitSet::allow(vec![DisclosedAmount {
                counterparty: AgentDid::new("did:layerx:counterparty")
                    .unwrap_or_else(|error| panic!("counterparty: {error:?}")),
                amount: Amount(amount),
            }]),
            asset: Asset::new("LXP").unwrap_or_else(|error| panic!("asset: {error:?}")),
            fee_limit: Amount(2),
            expiry: TimestampSeconds(120),
            idempotency_key: IdempotencyRef::new(format!("idempotency-{request_id}"))
                .unwrap_or_else(|error| panic!("idempotency: {error:?}")),
        },
        expiry: TimestampSeconds(120),
    }
}

fn context(request_id: u8) -> ApprovalContext {
    ApprovalContext {
        tenant: TenantId::new("tenant-a").unwrap_or_else(|error| panic!("tenant: {error}")),
        agent: Did::new(b"did:layerx:approver-test")
            .unwrap_or_else(|error| panic!("DID: {error:?}")),
        session: SessionId([7; 32]),
        capability: CapabilityId([9; 32]),
        policy_version: "policy-v1".to_owned(),
        request_id: [request_id; 32],
    }
}

#[test]
fn amount_at_threshold_proceeds_and_one_unit_over_is_held() {
    let registry = ApprovalRegistry::default();
    let at = require(
        &registry,
        ApprovalPolicy {
            amount_threshold: 100,
        },
        context(1),
        prepared(100, 1),
        50,
    )
    .unwrap_or_else(|error| panic!("threshold: {error:?}"));
    assert!(matches!(
        at,
        Requirement::NotRequired {
            disclosed_amount: 100,
            ..
        }
    ));
    let above = require(
        &registry,
        ApprovalPolicy {
            amount_threshold: 100,
        },
        context(2),
        prepared(101, 2),
        50,
    )
    .unwrap_or_else(|error| panic!("above threshold: {error:?}"));
    assert!(matches!(above, Requirement::Required(_)));
}

#[test]
fn approver_sees_the_disclosure_and_cannot_approve_an_altered_request() {
    let registry = ApprovalRegistry::default();
    let Requirement::Required(ticket) = require(
        &registry,
        ApprovalPolicy {
            amount_threshold: 100,
        },
        context(3),
        prepared(101, 3),
        50,
    )
    .unwrap_or_else(|error| panic!("hold: {error:?}")) else {
        panic!("approval was not required");
    };
    let mut altered = ticket.disclosure.clone();
    altered.fee_limit = Amount(1);
    let approver =
        ApproverId::new("human:alice").unwrap_or_else(|error| panic!("approver: {error:?}"));
    assert_eq!(
        approve(&registry, ticket.hold_id, approver, &altered, 60),
        Err(ApprovalError::DisclosureChanged)
    );
    let audit = approve(
        &registry,
        ticket.hold_id,
        ApproverId::new("human:alice").unwrap_or_else(|error| panic!("approver: {error:?}")),
        &ticket.disclosure,
        60,
    )
    .unwrap_or_else(|error| panic!("approval: {error:?}"));
    assert_eq!(audit.decision, ApprovalState::Approved);
    assert_eq!(
        audit
            .approver
            .as_ref()
            .unwrap_or_else(|| panic!("approver absent"))
            .as_str(),
        "human:alice"
    );
    assert_eq!(audit.disclosure_digest, ticket.disclosure_digest);
}

#[test]
fn expiry_at_the_declared_sequence_is_terminal_and_never_auto_approves() {
    let root = directory("expiry");
    fs::create_dir_all(&root).unwrap_or_else(|error| panic!("directory: {error}"));
    let registry = ApprovalRegistry::default();
    let Requirement::Required(ticket) = require(
        &registry,
        ApprovalPolicy {
            amount_threshold: 1,
        },
        context(4),
        prepared(2, 4),
        50,
    )
    .unwrap_or_else(|error| panic!("hold: {error:?}")) else {
        panic!("approval was not required");
    };
    assert!(expire(&registry, 119)
        .unwrap_or_else(|error| panic!("early expiry: {error:?}"))
        .is_empty());
    let expired = expire(&registry, 120).unwrap_or_else(|error| panic!("expiry: {error:?}"));
    assert_eq!(expired.len(), 1);
    assert_eq!(expired[0].decision, ApprovalState::Expired);
    assert!(expired[0].approver.is_none());
    assert_eq!(
        approve(
            &registry,
            ticket.hold_id,
            ApproverId::new("human:late").unwrap_or_else(|error| panic!("approver: {error:?}")),
            &ticket.disclosure,
            120,
        ),
        Err(ApprovalError::Daemon(DaemonApprovalError::AlreadyDecided(
            ApprovalState::Expired
        )))
    );
    let _ = fs::remove_dir_all(root);
}
