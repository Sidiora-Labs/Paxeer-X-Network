use std::collections::BTreeSet;

use layerx_agent_api::identity::{ContractError, PolicyVersion};
use layerx_agent_api::policy::{PolicyDecisionReason, PolicyDryRunResult, PolicyOutcome};
use layerx_agent_api::prepare::CanonicalBytes;
use layerx_agent_api::read::{
    BatchRef, CheckpointRef, FeeProjection, FeeProjectionRequest, Freshness, RelativeTo,
};
use layerx_agent_api::{Amount, Sequence};
use layerx_agentd::capability::{Capability, CapabilityDimensions, CapabilityId, RateCeiling};
use layerx_agentd::identity::ProtocolAuthority;
use layerx_agentd::policy::{
    dry_run_request_id, dry_run_with_context, DecisionReason, EvaluationMode, Explanation,
    Outcome, PolicyDryRunRefusal, PolicyRegistry, PolicyRequest, PolicySet, Rule,
    RuleConstraints, RuleEffect, VerifiedPolicyContext,
};
use layerx_agentd::session::{OpenRequest, SessionId, SessionRecord};
use layerx_agentd::store::TenantId;
use layerx_client::client::MAX_FEE_METER_CANONICAL_BYTES;
use layerx_client::evidence::{
    EvidenceError, ProgramStateSelector, MAX_PROGRAM_STATE_BUNDLE_BYTES, PROGRAM_STATE_DOMAIN,
    PROGRAM_STATE_REQUEST_BYTES,
};
use layerx_sdk::rpc::{RpcClient, RpcError};
use layerx_types::ids::Did;

const PROGRAM: [u8; 32] = [0x31; 32];
const DIGEST: [u8; 32] = [0x42; 32];
const ROOT: [u8; 32] = [0x53; 32];
const SEQUENCE: u64 = 0x0102_0304_0506_0708;

fn meter(canonical_bytes: u64) -> FeeProjectionRequest {
    FeeProjectionRequest {
        protocol_activity_type: 0x0003_0001,
        canonical_bytes,
        execution_units: u64::MAX,
        storage_units: u64::MAX - 1,
    }
}

fn freshness(chain_head: u64, value_sequence: u64) -> Freshness {
    let batch = BatchRef::new("batch-42").unwrap_or_else(|error| panic!("batch: {error:?}"));
    Freshness {
        chain_head: Sequence(chain_head),
        latest_sealed_batch: batch.clone(),
        latest_finalised_checkpoint: CheckpointRef::new("checkpoint-3")
            .unwrap_or_else(|error| panic!("checkpoint: {error:?}")),
        value_sequence: Sequence(value_sequence),
        relative_to: RelativeTo::Batch(batch),
    }
}

fn projection() -> FeeProjection {
    FeeProjection {
        request: meter(0),
        parameter_version: 7,
        fee: Amount(u128::MAX - 7),
        canonical_schedule: CanonicalBytes::new(vec![0, 1, 2, 3])
            .unwrap_or_else(|error| panic!("schedule: {error:?}")),
        snapshot_sequence: Sequence(42),
        snapshot_state_root: ROOT,
    }
}

fn contract_selector_bytes(version: u16, kind: u8, program: [u8; 32], sequence: u64) -> Vec<u8> {
    let mut bytes = Vec::with_capacity(PROGRAM_STATE_REQUEST_BYTES);
    bytes.extend_from_slice(&version.to_be_bytes());
    bytes.push(kind);
    bytes.extend_from_slice(&program);
    bytes.extend_from_slice(&sequence.to_be_bytes());
    bytes.extend_from_slice(&DIGEST);
    bytes.extend_from_slice(&ROOT);
    bytes
}

#[test]
fn meter_zero_is_a_valid_lni_meter_while_empty_encoded_activity_stays_refused() {
    let zero = meter(0)
        .validate()
        .unwrap_or_else(|error| panic!("zero meter: {error:?}"));
    let fee_meter = zero.meter();
    assert_eq!(fee_meter.canonical_bytes, 0);
    assert_eq!(fee_meter.activity_type, 0x0003_0001);
    assert_eq!(fee_meter.execution_units, u64::MAX);
    assert_eq!(fee_meter.storage_units, u64::MAX - 1);

    let rpc = RpcClient::connect("http://127.0.0.1:1", None)
        .unwrap_or_else(|error| panic!("loopback rpc: {error:?}"));
    assert!(matches!(rpc.estimate_fee(&[]), Err(RpcError::InvalidRequest)));
    assert!(matches!(
        rpc.estimate_fee(&vec![0; 524_289]),
        Err(RpcError::InvalidRequest)
    ));
}

#[test]
fn meter_bound_is_the_native_inclusive_limit_and_is_never_clamped() {
    assert_eq!(MAX_FEE_METER_CANONICAL_BYTES, 1_048_576);
    let edge = meter(1_048_576)
        .validate()
        .unwrap_or_else(|error| panic!("edge meter: {error:?}"));
    assert_eq!(edge.meter().canonical_bytes, 1_048_576);
    let wider_than_sdk = meter(524_289)
        .validate()
        .unwrap_or_else(|error| panic!("native meter above sdk bound: {error:?}"));
    assert_eq!(wider_than_sdk.meter().canonical_bytes, 524_289);
    for above in [1_048_577, 2_097_152, u64::MAX] {
        assert!(matches!(
            meter(above).validate(),
            Err(ContractError::OutOfRange("canonical_bytes"))
        ));
    }
}

#[test]
fn fee_projection_is_a_typed_projection_bound_to_its_own_snapshot() {
    let result = projection()
        .into_projection("native fee schedule at the captured head", freshness(42, 42))
        .unwrap_or_else(|error| panic!("projection: {error:?}"));
    assert_eq!(result.projected.fee, Amount(u128::MAX - 7));
    assert_eq!(result.projected.parameter_version, 7);
    assert_eq!(result.projected.canonical_schedule.as_bytes(), &[0, 1, 2, 3]);
    assert_eq!(result.projected.snapshot_state_root, ROOT);
    assert_eq!(result.observed_freshness, freshness(42, 42));
    assert_eq!(result.rationale, "native fee schedule at the captured head");

    for (chain_head, value_sequence) in [(41, 42), (42, 41), (43, 43), (41, 41)] {
        assert!(matches!(
            projection().into_projection("stale", freshness(chain_head, value_sequence)),
            Err(ContractError::Mismatch(_))
        ));
    }
    assert!(matches!(
        projection().into_projection("", freshness(42, 42)),
        Err(ContractError::Empty("projection_rationale"))
    ));
}

#[test]
fn kind5_selector_is_exactly_the_107_byte_contract_request() {
    assert_eq!(PROGRAM_STATE_REQUEST_BYTES, 107);
    assert_eq!(PROGRAM_STATE_DOMAIN, b"LayerX/programs/state-proof/v1\0");
    assert_eq!(PROGRAM_STATE_DOMAIN.len(), 31);
    assert_eq!(MAX_PROGRAM_STATE_BUNDLE_BYTES, 40 * 1024 * 1024);

    let selector = ProgramStateSelector::new(PROGRAM, SEQUENCE, DIGEST, ROOT)
        .unwrap_or_else(|error| panic!("selector: {error:?}"));
    let expected = contract_selector_bytes(1, 5, PROGRAM, SEQUENCE);
    assert_eq!(expected.len(), 107);
    assert_eq!(selector.encode().as_slice(), expected.as_slice());
    assert_eq!(&expected[..3], &[0, 1, 5]);
    assert_eq!(&expected[35..43], &[1, 2, 3, 4, 5, 6, 7, 8]);
    let decoded = ProgramStateSelector::decode(&expected)
        .unwrap_or_else(|error| panic!("contract selector: {error:?}"));
    assert_eq!(decoded, selector);
    assert_eq!(decoded.encode().as_slice(), expected.as_slice());
    assert_eq!(selector.program_id(), PROGRAM);
    assert_eq!(selector.global_sequence(), SEQUENCE);
    assert_eq!(selector.receipt_digest(), DIGEST);
    assert_eq!(selector.state_root(), ROOT);
}

#[test]
fn kind5_selector_refuses_every_noncanonical_request() {
    assert!(matches!(
        ProgramStateSelector::new([0; 32], SEQUENCE, DIGEST, ROOT),
        Err(EvidenceError::Malformed)
    ));
    assert!(matches!(
        ProgramStateSelector::new(PROGRAM, 0, DIGEST, ROOT),
        Err(EvidenceError::Malformed)
    ));
    for bytes in [
        contract_selector_bytes(0, 5, PROGRAM, SEQUENCE),
        contract_selector_bytes(2, 5, PROGRAM, SEQUENCE),
        contract_selector_bytes(1, 4, PROGRAM, SEQUENCE),
        contract_selector_bytes(1, 6, PROGRAM, SEQUENCE),
        contract_selector_bytes(1, 5, [0; 32], SEQUENCE),
        contract_selector_bytes(1, 5, PROGRAM, 0),
    ] {
        assert!(matches!(
            ProgramStateSelector::decode(&bytes),
            Err(EvidenceError::Malformed)
        ));
    }
    let canonical = contract_selector_bytes(1, 5, PROGRAM, SEQUENCE);
    let mut trailing = canonical.clone();
    trailing.push(0);
    for bytes in [&canonical[..106], trailing.as_slice(), &[][..]] {
        assert!(matches!(
            ProgramStateSelector::decode(bytes),
            Err(EvidenceError::Malformed)
        ));
    }
}

fn tenant() -> TenantId {
    TenantId::new("tenant-a").unwrap_or_else(|error| panic!("tenant: {error}"))
}

fn session() -> SessionRecord {
    SessionRecord {
        request: OpenRequest {
            session_id: SessionId([3; 32]),
            token_id: [2; 32],
            tenant: tenant(),
            agent: Did::new(b"did:layerx:dry-run").unwrap_or_else(|error| panic!("did: {error:?}")),
            authority: ProtocolAuthority::SessionKey([1; 32]),
            permitted_activity_types: BTreeSet::from([7]),
            scopes: BTreeSet::from(["prepare".to_owned()]),
            expiry_sequence: 200,
            opening_client: "dry-run-test".to_owned(),
            policy_version: "v1".to_owned(),
        },
        open: true,
        sequence: 0,
        budget_reserved: 0,
        subscription_cursor: 0,
        generation: 1,
        retired_token_ids: BTreeSet::new(),
    }
}

fn capability(id: u8) -> Capability {
    Capability::new(
        CapabilityId([id; 32]),
        tenant(),
        CapabilityDimensions {
            activity_types: BTreeSet::from([7]),
            counterparties: BTreeSet::from([[8; 32]]),
            assets: BTreeSet::from([[9; 32]]),
            amount_ceiling: 500,
            rate_ceiling: RateCeiling {
                maximum_uses: 10,
                window_sequences: 100,
            },
            purposes: BTreeSet::from(["research".to_owned()]),
            expiry_sequence: 200,
        },
    )
    .unwrap_or_else(|error| panic!("capability: {error:?}"))
}

fn policy_request() -> PolicyRequest {
    PolicyRequest {
        activity_type: 7,
        counterparty: [8; 32],
        asset: [9; 32],
        amount: 100,
        purpose: "research".to_owned(),
        core_sequence: 120,
    }
}

#[test]
fn policy_dry_run_stays_a_local_policy_decision_and_never_a_fee_estimate() {
    let mut registry = PolicyRegistry::new(PolicySet {
        version: "v1".to_owned(),
        rules: vec![Rule {
            id: "permit-research".to_owned(),
            effect: RuleEffect::Permit,
            constraints: RuleConstraints::default(),
        }],
        evaluation_step_limit: 10,
    })
    .unwrap_or_else(|error| panic!("registry: {error:?}"));
    let request = policy_request();
    let session = session();
    let generation = registry.begin_request().generation();
    let request_id = dry_run_request_id(
        &tenant(),
        SessionId([3; 32]),
        CapabilityId([4; 32]),
        generation,
        &request,
    );
    assert_eq!(
        request_id,
        dry_run_request_id(
            &tenant(),
            SessionId([3; 32]),
            CapabilityId([4; 32]),
            generation,
            &request
        )
    );
    assert_ne!(
        request_id,
        dry_run_request_id(
            &tenant(),
            SessionId([3; 32]),
            CapabilityId([5; 32]),
            generation,
            &request
        )
    );

    let result = dry_run_with_context(
        &mut registry,
        request_id,
        &request,
        &session,
        &capability(4),
        VerifiedPolicyContext::Unavailable,
    );
    assert_eq!(result.decision.outcome, Outcome::Deny);
    assert_eq!(result.decision.reason, DecisionReason::InvalidContext);
    assert_eq!(result.explanation.mode, EvaluationMode::DryRun);
    assert_eq!(
        registry.audit_entry(request_id).map(|entry| &entry.decision),
        Some(&result.decision)
    );

    let bytes = result.explanation.machine_bytes();
    let text = String::from_utf8(bytes.clone()).unwrap_or_else(|error| panic!("utf8: {error}"));
    assert!(text.contains("mode=7:dry_run\n"));
    assert!(text.contains("reason=15:invalid_context\n"));
    for absent in ["fee", "parameter_version", "schedule", "observed_sequence", "state_root", "freshness"] {
        assert!(!text.contains(absent), "{absent} in policy explanation");
    }
    assert_eq!(Explanation::from_machine_bytes(&bytes), Ok(result.explanation.clone()));
    let mut trailing = bytes.clone();
    trailing.push(b'\n');
    assert!(Explanation::from_machine_bytes(&trailing).is_err());

    let typed = PolicyDryRunResult {
        outcome: PolicyOutcome::Deny,
        policy_version: PolicyVersion::new(result.explanation.policy_version.clone())
            .unwrap_or_else(|error| panic!("policy version: {error:?}")),
        matched_rules: result.explanation.matched_rules.clone(),
        deciding_rule: result.explanation.deciding_rule.clone(),
        reason: PolicyDecisionReason::InvalidContext,
        authority_statement: result.explanation.authority_statement.to_owned(),
    }
    .validate()
    .unwrap_or_else(|error| panic!("typed dry-run result: {error:?}"));
    assert!(typed.authority_statement.contains("local restriction"));
    let mut unstated = typed;
    unstated.authority_statement.clear();
    assert!(matches!(unstated.validate(), Err(ContractError::Empty(_))));

    assert_eq!(
        PolicyDryRunRefusal::LegacyProjectPayload.code(),
        "policy.legacy_project_payload"
    );
}
