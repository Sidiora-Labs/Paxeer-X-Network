use layerx_agentd::budget::{
    reserve, reserve_until_core_time, BudgetLimiter, CoreTimestampMs, LimitConfig, LimitId,
    LimitScope, ReservationRequest,
};

fn limiter() -> BudgetLimiter {
    BudgetLimiter::new(vec![
        LimitConfig {
            id: LimitId([1; 16]),
            name: "tenant spending".into(),
            scope: LimitScope::Tenant([1; 32]),
            ceiling: 1000,
            consumed: 100,
        },
        LimitConfig {
            id: LimitId([2; 16]),
            name: "agent spending".into(),
            scope: LimitScope::Agent([2; 32]),
            ceiling: 500,
            consumed: 50,
        },
    ])
    .expect("real spending limit configuration")
}
fn request(id: u8, amount: u128) -> ReservationRequest {
    ReservationRequest {
        id: [id; 32],
        amount,
        expiry_sequence: 20,
        current_sequence: 10,
        applicable_limits: vec![LimitId([1; 16]), LimitId([2; 16])],
    }
}
#[test]
fn budget_after_counts_the_exact_held_activity_once() {
    let limits = limiter();
    reserve(&limits, &request(3, 100)).expect("reserve actual spending");
    assert_eq!(
        limits.remaining_after_reservation([3; 32], 100, 10, CoreTimestampMs(100)),
        Ok(350)
    );
    reserve(&limits, &request(4, 50)).expect("reserve additional actual spending");
    assert_eq!(
        limits.remaining_after_reservation([3; 32], 100, 10, CoreTimestampMs(100)),
        Ok(300)
    );
}
#[test]
fn budget_after_refuses_foreign_changed_and_expired_holds() {
    let limits = limiter();
    reserve(&limits, &request(3, 100)).expect("reserve actual spending");
    assert!(limits
        .remaining_after_reservation([4; 32], 100, 10, CoreTimestampMs(100))
        .is_err());
    assert!(limits
        .remaining_after_reservation([3; 32], 99, 10, CoreTimestampMs(100))
        .is_err());
    assert!(limits
        .remaining_after_reservation([3; 32], 100, 20, CoreTimestampMs(100))
        .is_err());
    assert!(limits
        .remaining_after_reservation([3; 32], 100, 10, CoreTimestampMs(0))
        .is_err());
}
#[test]
fn budget_after_retains_the_authenticated_core_deadline() {
    let limits = limiter();
    reserve_until_core_time(
        &limits,
        &request(3, 100),
        CoreTimestampMs(200),
        CoreTimestampMs(100),
    )
    .expect("reserve a real time bounded activity");
    assert_eq!(
        limits.remaining_after_reservation([3; 32], 100, 10, CoreTimestampMs(199)),
        Ok(350)
    );
    assert!(limits
        .remaining_after_reservation([3; 32], 100, 10, CoreTimestampMs(200))
        .is_err());
}

#[test]
fn protocol_remaining_counts_the_outstanding_reservation_inventory() {
    let limits = limiter();
    reserve(&limits, &request(3, 100)).expect("reserve actual spending");
    reserve(&limits, &request(4, 50)).expect("reserve additional actual spending");
    assert_eq!(
        limits.remaining_after_reservation_bound([3; 32], 100, 10, CoreTimestampMs(100), 400),
        Ok(250)
    );
    assert!(limits
        .remaining_after_reservation_bound([3; 32], 100, 10, CoreTimestampMs(100), 149)
        .is_err());
}

fn projection_fixture() -> serde_json::Value {
    use std::os::unix::fs::PermissionsExt;
    let path = std::env::var_os("PAXEER_X_HUMAN_PROJECTION_FACTS_FIXTURE")
        .expect("genuine approval and managed receipt fixture required");
    let metadata = std::fs::symlink_metadata(&path).expect("genuine fixture metadata");
    assert!(
        metadata.is_file()
            && metadata.len() > 0
            && metadata.len() <= 65536
            && metadata.permissions().mode() & 0o077 == 0
    );
    let fixture: serde_json::Value =
        serde_json::from_slice(&std::fs::read(path).expect("protected real fixture"))
            .expect("real fixture JSON");
    assert_eq!(fixture["schema"], "paxeer-x.human-projection-facts.v1");
    fixture
}

fn fixture_text<'a>(fixture: &'a serde_json::Value, key: &str) -> &'a str {
    fixture[key]
        .as_str()
        .expect("required genuine fixture field")
}

fn fixture_id(fixture: &serde_json::Value, key: &str) -> [u8; 32] {
    layerx_programs::hex::decode(fixture_text(fixture, key))
        .expect("canonical fixture identity")
        .try_into()
        .expect("32-byte identity")
}

fn scoped_projection_exchange(
    fixture: &serde_json::Value,
    operation: &[u8],
    principal: &str,
) -> Vec<u8> {
    use layerx_client::lni::transport::{ConnectionGate, FrameTransport, Limits, Uds};
    fn field(output: &mut Vec<u8>, value: &[u8]) {
        output.extend_from_slice(
            &u32::try_from(value.len())
                .expect("bounded field")
                .to_be_bytes(),
        );
        output.extend_from_slice(value);
    }
    let mut request = b"LXHAGT01".to_vec();
    request.push(44);
    field(&mut request, principal.as_bytes());
    field(
        &mut request,
        fixture_text(fixture, "subject_owner").as_bytes(),
    );
    field(
        &mut request,
        fixture_text(fixture, "subject_account").as_bytes(),
    );
    request.extend_from_slice(&fixture_id(fixture, "asset_id"));
    field(&mut request, operation);
    let mut transport = Uds::connect(
        std::path::Path::new(fixture_text(fixture, "human_socket")),
        &ConnectionGate::new(1),
        Limits {
            maximum_frame_bytes: 1_048_576,
            maximum_connections: 1,
            maximum_streams: 1,
            maximum_queued_bytes: 1_048_576,
            deadline: std::time::Duration::from_secs(8),
        },
    )
    .expect("real authenticated Human peer");
    transport
        .send(&request)
        .expect("bounded genuine projection request");
    transport.receive().expect("actual projection response")
}

fn projection_operation(tag: u8, payload: &[u8]) -> Vec<u8> {
    let mut operation = b"LXHAGT01".to_vec();
    operation.push(tag);
    operation.extend_from_slice(payload);
    operation
}

fn successful_payload(response: &[u8]) -> &[u8] {
    assert_eq!(response.get(..8), Some(b"LXHAGT01".as_slice()));
    assert_eq!(response.get(8), Some(&0));
    &response[9..]
}

#[test]
fn real_authenticated_approval_and_managed_exports_preserve_facts_refusals_and_reopen() {
    use layerx_agentd::store::{ObjectKind, StorageClass, Store, TenantId, TenantKey};
    use layerx_proof::receipt::verify_sequencer_signature;
    use sha2::{Digest as _, Sha256};
    let fixture = projection_fixture();
    let principal = fixture_text(&fixture, "subject_principal");
    let sequence = fixture["current_sequence"]
        .as_u64()
        .expect("actual native sequence");
    let approval = fixture_id(&fixture, "approval_id");
    let mut get = approval.to_vec();
    get.extend_from_slice(&sequence.to_be_bytes());
    let get = projection_operation(47, &get);
    let approval_response = scoped_projection_exchange(&fixture, &get, principal);
    let facts = successful_payload(&approval_response);
    assert_eq!(facts.get(..2), Some(2u16.to_be_bytes().as_slice()));
    assert_eq!(facts.get(2..34), Some(approval.as_slice()));
    assert!(facts.len() >= 50);
    let created = u64::from_be_bytes(
        facts[facts.len() - 16..facts.len() - 8]
            .try_into()
            .expect("creation fact"),
    );
    let expiry = u64::from_be_bytes(
        facts[facts.len() - 8..]
            .try_into()
            .expect("wall deadline fact"),
    );
    assert_eq!(
        created,
        fixture["created_at_unix_seconds"]
            .as_u64()
            .expect("actual admission time")
    );
    assert_eq!(
        expiry,
        fixture["activity_expires_at_unix_seconds"]
            .as_u64()
            .expect("actual held deadline")
    );
    assert!(created > 0 && created < expiry);
    let rejected =
        scoped_projection_exchange(&fixture, &get, fixture_text(&fixture, "foreign_principal"));
    assert_eq!(rejected, b"LXHAGT01\x01");
    let mut legacy = fixture_id(&fixture, "legacy_approval_id").to_vec();
    legacy.extend_from_slice(&sequence.to_be_bytes());
    assert_eq!(
        scoped_projection_exchange(&fixture, &projection_operation(47, &legacy), principal),
        b"LXHAGT01\x02"
    );
    let legacy_response =
        scoped_projection_exchange(&fixture, &projection_operation(10, &legacy), principal);
    assert_eq!(
        successful_payload(&legacy_response).get(..32),
        Some(legacy[..32].as_ref())
    );
    let held_digest = fixture_id(&fixture, "held_digest");
    let mut budget = approval.to_vec();
    budget.extend_from_slice(&held_digest);
    budget.extend_from_slice(&sequence.to_be_bytes());
    let response =
        scoped_projection_exchange(&fixture, &projection_operation(48, &budget), principal);
    let budget_facts = successful_payload(&response);
    assert_eq!(budget_facts.len(), 155);
    assert_eq!(&budget_facts[..2], &2u16.to_be_bytes());
    assert_eq!(&budget_facts[2..34], &approval);
    assert_eq!(&budget_facts[34..66], &held_digest);
    let remaining = u128::from_be_bytes(
        budget_facts[66..82]
            .try_into()
            .expect("exact budget remaining"),
    );
    assert_eq!(
        remaining,
        fixture_text(&fixture, "remaining_after")
            .parse::<u128>()
            .expect("actual post-hold bound")
    );
    assert!((4..=5).contains(&budget_facts[82]));
    assert!(budget_facts[83..115].iter().any(|byte| *byte != 0));
    assert_eq!(&budget_facts[115..123], &sequence.to_be_bytes());
    assert_eq!(&budget_facts[123..155], &fixture_id(&fixture, "asset_id"));
    let mut stale_budget = budget.clone();
    stale_budget[64..72].copy_from_slice(
        &sequence
            .checked_add(1)
            .expect("next sequence")
            .to_be_bytes(),
    );
    assert_eq!(
        scoped_projection_exchange(
            &fixture,
            &projection_operation(48, &stale_budget),
            principal
        ),
        b"LXHAGT01\x01"
    );
    budget[32] ^= 1;
    assert_eq!(
        scoped_projection_exchange(&fixture, &projection_operation(48, &budget), principal),
        b"LXHAGT01\x01"
    );

    let root = std::path::Path::new(fixture_text(&fixture, "retained_store"));
    let tenant = TenantId::new(fixture_text(&fixture, "agent_tenant")).expect("real tenant");
    let agent_id = fixture_text(&fixture, "agent_id");
    let digest = fixture_id(&fixture, "receipt_digest");
    let store = Store::open(root).expect("actual retained store");
    let hold_key = TenantKey::new(
        tenant.clone(),
        ObjectKind::PreparedActivity,
        [b"approval-hold-v1:".as_slice(), &approval].concat(),
    )
    .expect("actual hold owner key");
    let retained_hold = store
        .get(&hold_key)
        .expect("actual trusted-clock hold producer record");
    assert_eq!(retained_hold.class(), StorageClass::LocalOnly);
    let retained_hold = retained_hold.bytes().to_vec();
    assert_eq!(&retained_hold[..8], b"LXAPHLD3");
    let legacy_length = u32::from_be_bytes(
        retained_hold[8..12]
            .try_into()
            .expect("canonical legacy length"),
    ) as usize;
    assert_eq!(retained_hold.len(), 12 + legacy_length + 16);
    assert_eq!(&retained_hold[12..20], b"LXAPHLD2");
    assert_eq!(
        &retained_hold[retained_hold.len() - 16..],
        &facts[facts.len() - 16..]
    );
    let exported = layerx_agentd::managed_agent::evidence_export(&store, &tenant, agent_id, digest)
        .expect("real managed-agent evidence store export");
    let decoded = verify_sequencer_signature(
        &exported.canonical_bytes,
        fixture_id(&fixture, "sequencer_public_key"),
    )
    .expect("independent native sequencer signature");
    let protocol = decoded.protocol().expect("genuine native receipt");
    let mut altered_receipt = exported.canonical_bytes.clone();
    let last = altered_receipt
        .len()
        .checked_sub(1)
        .expect("nonempty real receipt");
    altered_receipt[last] ^= 1;
    assert!(verify_sequencer_signature(
        &altered_receipt,
        fixture_id(&fixture, "sequencer_public_key")
    )
    .is_err());
    assert_eq!(
        <[u8; 32]>::from(Sha256::digest(&exported.canonical_bytes)),
        digest
    );
    assert_eq!(protocol.activity_id(), exported.metadata.activity_id);
    assert_eq!(
        protocol.global_sequence(),
        exported.metadata.global_sequence
    );
    let mut export_request = u32::try_from(agent_id.len())
        .expect("agent id bound")
        .to_be_bytes()
        .to_vec();
    export_request.extend_from_slice(agent_id.as_bytes());
    export_request.extend_from_slice(&digest);
    let response = scoped_projection_exchange(
        &fixture,
        &projection_operation(49, &export_request),
        principal,
    );
    let wire = successful_payload(&response);
    assert_eq!(&wire[..2], &2u16.to_be_bytes());
    assert_eq!(&wire[2..34], &digest);
    assert_eq!(&wire[34..66], &exported.metadata.activity_id);
    assert_eq!(
        &wire[66..74],
        &exported.metadata.global_sequence.to_be_bytes()
    );
    assert_eq!(wire[74], exported.metadata.verification_level.wire_rank());
    let length =
        u32::from_be_bytes(wire[75..79].try_into().expect("exact receipt length")) as usize;
    assert_eq!(wire.len(), 79 + length);
    assert_eq!(&wire[79..], exported.canonical_bytes);
    assert!(
        layerx_agentd::managed_agent::evidence_export(&store, &tenant, agent_id, [0; 32]).is_err()
    );
    let foreign =
        TenantId::new(fixture_text(&fixture, "foreign_agent_tenant")).expect("distinct tenant");
    assert_ne!(tenant, foreign);
    assert!(
        layerx_agentd::managed_agent::evidence_export(&store, &foreign, agent_id, digest).is_err()
    );
    let mut changed = digest;
    changed[0] ^= 1;
    assert!(
        layerx_agentd::managed_agent::evidence_export(&store, &tenant, agent_id, changed).is_err()
    );
    drop(store);
    let reopened = Store::open(root).expect("actual reopened store");
    assert_eq!(
        reopened
            .get(&hold_key)
            .expect("recovered authentic creation record")
            .bytes(),
        retained_hold
    );
    assert_eq!(
        layerx_agentd::managed_agent::evidence_export(&reopened, &tenant, agent_id, digest)
            .expect("recovered managed receipt export"),
        exported
    );
    assert_eq!(
        scoped_projection_exchange(&fixture, &get, principal),
        approval_response
    );
}
