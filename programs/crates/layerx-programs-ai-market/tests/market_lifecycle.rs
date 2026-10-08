use layerx_programs_ai_market::{
    codec::{self, decode_envelope, derive_market, encode_envelope, Envelope},
    dispatch::{self, Operation},
    errors::*,
    policy::*,
    registry::*,
    registry_ops::*,
    state::*,
    types::*,
    MAX_EVENT_BYTES,
};

const CHAIN: [u8; 32] = [10; 32];
const PROGRAM: [u8; 32] = [11; 32];
const OWNER: [u8; 32] = [12; 32];
const ASSET: [u8; 32] = [13; 32];
const REFUND: [u8; 32] = [14; 32];
const TREASURY: [u8; 32] = [15; 32];
const OPERATOR_O: [u8; 32] = [17; 32];
const OPERATOR_P: [u8; 32] = [18; 32];
const OTHER: [u8; 32] = [21; 32];
const H: u64 = 1010;

fn p(bytes: [u8; 32]) -> PrincipalId {
    PrincipalId::new(bytes).unwrap()
}
fn market() -> MarketId {
    derive_market(
        ChainDomain::new(CHAIN).unwrap(),
        ProgramId::new(PROGRAM).unwrap(),
    )
    .unwrap()
}
fn rewards() -> AccountId {
    derive_rewards_account(
        ProgramId::new(PROGRAM).unwrap(),
        AssetId::new(ASSET).unwrap(),
    )
    .unwrap()
}
fn ctx(actor: [u8; 32], height: u64) -> CallContext {
    CallContext {
        chain: ChainDomain::new(CHAIN).unwrap(),
        program: ProgramId::new(PROGRAM).unwrap(),
        principal: p(actor),
        height,
    }
}
fn policy(version: u64, rubric: u8) -> TaskPolicyV1 {
    TaskPolicyV1::bounded_default(
        version,
        1,
        PolicyCommitments {
            model_artifact_digest: Digest32::new([1; 32]).unwrap(),
            dataset_artifact_digest: [2; 32],
            benchmark_suite_digest: Digest32::new([3; 32]).unwrap(),
            rubric_digest: RubricDigest::new([rubric; 32]).unwrap(),
            task_schema_digest: Digest32::new([5; 32]).unwrap(),
            result_schema_digest: Digest32::new([6; 32]).unwrap(),
            service_terms_digest: Digest32::new([7; 32]).unwrap(),
        },
        100,
        1,
    )
    .unwrap()
}
fn policy_bytes(value: &TaskPolicyV1) -> Vec<u8> {
    let mut out = vec![0; TASK_POLICY_BYTES];
    value.encode(&mut out).unwrap();
    out
}
fn create_payload(
    owner: [u8; 32],
    rewards: AccountId,
    treasury: &[u8],
    policy: &[u8],
    metadata: [u8; 32],
) -> Vec<u8> {
    let mut v = owner.to_vec();
    v.extend_from_slice(&ASSET);
    v.extend_from_slice(rewards.as_bytes());
    v.extend_from_slice(&REFUND);
    v.extend_from_slice(treasury);
    v.extend_from_slice(policy);
    v.extend_from_slice(&metadata);
    v
}
fn u64s(values: &[u64]) -> Vec<u8> {
    values.iter().flat_map(|v| v.to_be_bytes()).collect()
}
fn stage(revision: u64, value: &TaskPolicyV1, effective: u64) -> Vec<u8> {
    let mut v = revision.to_be_bytes().to_vec();
    v.extend_from_slice(&policy_bytes(value));
    v.extend_from_slice(&effective.to_be_bytes());
    v
}
fn digest_payload(revision: u64, digest: [u8; 32], grant: Option<u64>) -> Vec<u8> {
    let mut v = revision.to_be_bytes().to_vec();
    v.extend_from_slice(&digest);
    if let Some(grant) = grant {
        v.extend_from_slice(&grant.to_be_bytes());
    }
    v
}
fn appoint(revision: u64, principal: [u8; 32], permissions: u8, expected: u64) -> Vec<u8> {
    let mut v = revision.to_be_bytes().to_vec();
    v.extend_from_slice(&principal);
    v.push(permissions);
    v.extend_from_slice(&expected.to_be_bytes());
    v
}
#[allow(clippy::too_many_arguments)]
fn envelope_for(
    operation: Operation,
    actor: [u8; 32],
    sequence: u64,
    request: u8,
    payload: &[u8],
    config: u64,
    chain: [u8; 32],
    program: [u8; 32],
) -> Vec<u8> {
    let chain = ChainDomain::new(chain).unwrap();
    let program = ProgramId::new(program).unwrap();
    let e = Envelope {
        operation,
        chain,
        program,
        market: derive_market(chain, program).unwrap(),
        actor: p(actor),
        epoch: 0,
        config,
        roster: Presence::Absent,
        sequence,
        expiry: 1_000_000,
        request: RequestId::new([request; 32]).unwrap(),
        payload,
        authentication: Authentication::Native,
    };
    let mut out = vec![0; 16_384];
    let n = encode_envelope(&e, &mut out).unwrap();
    out.truncate(n);
    out
}
fn envelope(
    operation: Operation,
    actor: [u8; 32],
    sequence: u64,
    request: u8,
    payload: &[u8],
    config: u64,
) -> Vec<u8> {
    envelope_for(
        operation, actor, sequence, request, payload, config, CHAIN, PROGRAM,
    )
}

enum Run {
    Applied {
        state: Vec<u8>,
        receipt: Vec<u8>,
        event: Vec<u8>,
    },
    Retry(RetainedResult),
}
fn run(ctx: CallContext, state: Option<&[u8]>, envelope: &[u8]) -> CodecResult<Run> {
    let decoded = match state {
        Some(bytes) => Some(decode_shared_state(bytes)?),
        None => None,
    };
    let env = decode_envelope(envelope)?;
    let mut section = vec![0; F01_SECTION_CAP];
    let mut event = vec![0; MAX_EVENT_BYTES];
    match apply(&ctx, decoded.as_ref(), &env, &mut section, &mut event)? {
        Outcome::Applied {
            state,
            receipt,
            event_len,
        } => Ok(Run::Applied {
            state: encode_state(&state),
            receipt: receipt.bytes().to_vec(),
            event: event[..event_len].to_vec(),
        }),
        Outcome::AlreadyApplied(retained) => Ok(Run::Retry(retained)),
    }
}
fn encode_state(state: &SharedState<'_>) -> Vec<u8> {
    let mut out = vec![0; state.encoded_len().unwrap()];
    let mut scratch = vec![0; Section::Control.payload_cap()];
    let n = encode_shared_state(state, &mut out, &mut scratch).unwrap();
    assert_eq!(n, out.len());
    out
}
fn applied(result: CodecResult<Run>) -> (Vec<u8>, Vec<u8>, Vec<u8>) {
    match result {
        Ok(Run::Applied {
            state,
            receipt,
            event,
        }) => (state, receipt, event),
        Ok(Run::Retry(_)) => panic!("unexpected retry"),
        Err(error) => panic!("{error}"),
    }
}
fn retried(result: CodecResult<Run>) -> RetainedResult {
    match result {
        Ok(Run::Retry(retained)) => retained,
        Ok(Run::Applied { .. }) => panic!("unexpected novel application"),
        Err(error) => panic!("{error}"),
    }
}
fn refused(result: CodecResult<Run>) -> ApplicationError {
    match result {
        Err(error) => error,
        Ok(_) => panic!("unexpected success"),
    }
}
fn section_of(state: &[u8]) -> PolicySection<'_> {
    PolicySection::decode(codec::decode_state(state).unwrap().sections[0]).unwrap()
}
fn with_sections(state: &[u8], replace: &[(usize, &[u8])]) -> Vec<u8> {
    let mut shared = decode_shared_state(state).unwrap();
    for (index, bytes) in replace {
        shared.feature_sections[*index] = bytes;
    }
    encode_state(&shared)
}
fn section_bytes(section: &PolicySection<'_>) -> Vec<u8> {
    let mut out = vec![0; section.encoded_len().unwrap()];
    section.encode(&mut out).unwrap();
    out
}

struct Market {
    state: Vec<u8>,
    next_request: u8,
}
impl Market {
    fn create(treasury: &[u8]) -> Self {
        let payload = create_payload(
            OWNER,
            rewards(),
            treasury,
            &policy_bytes(&policy(1, 4)),
            [16; 32],
        );
        let e = envelope(dispatch::CREATE, OWNER, 1, 1, &payload, 1);
        let (state, _, _) = applied(run(ctx(OWNER, 1000), None, &e));
        Self {
            state,
            next_request: 1,
        }
    }
    fn section(&self) -> PolicySection<'_> {
        section_of(&self.state)
    }
    fn revision(&self) -> u64 {
        self.section().header.state_revision
    }
    fn envelope(
        &mut self,
        operation: Operation,
        actor: [u8; 32],
        sequence: u64,
        payload: &[u8],
    ) -> Vec<u8> {
        let config = self.section().header.active_config_version;
        self.next_request += 1;
        envelope(
            operation,
            actor,
            sequence,
            self.next_request,
            payload,
            config,
        )
    }
    fn submit(&mut self, actor: [u8; 32], envelope: &[u8], height: u64) -> CodecResult<Run> {
        let result = run(ctx(actor, height), Some(&self.state), envelope)?;
        if let Run::Applied { state, .. } = &result {
            self.state = state.clone();
        }
        Ok(result)
    }
    fn send(
        &mut self,
        operation: Operation,
        actor: [u8; 32],
        sequence: u64,
        payload: &[u8],
        height: u64,
    ) -> CodecResult<Run> {
        let e = self.envelope(operation, actor, sequence, payload);
        self.submit(actor, &e, height)
    }
}

#[test]
fn a01_create_registers_once_at_committed_origin() {
    let payload = create_payload(
        OWNER,
        rewards(),
        &[0],
        &policy_bytes(&policy(1, 4)),
        [16; 32],
    );
    let by_b = envelope(dispatch::CREATE, OTHER, 1, 1, &payload, 1);
    assert_eq!(
        refused(run(ctx(OTHER, 1000), None, &by_b)),
        F01_PRINCIPAL_MISMATCH
    );
    let e = envelope(dispatch::CREATE, OWNER, 1, 1, &payload, 1);
    assert_eq!(refused(run(ctx(OTHER, 1000), None, &e)), UNAUTHORIZED);
    let (state, receipt, event) = applied(run(ctx(OWNER, 1000), None, &e));
    let s = section_of(&state);
    let digest = policy(1, 4).digest().unwrap();
    assert_eq!(s.header.lifecycle, REGISTERED);
    assert_eq!(s.header.origin_height, 1000);
    assert_eq!(s.header.state_revision, 1);
    assert_eq!(s.header.highest_config_version, 1);
    assert_eq!(s.header.active_config_version, 1);
    assert_eq!(s.header.market_id, market());
    assert_eq!(s.header.owner_principal, p(OWNER));
    assert_eq!(s.header.rewards_account, rewards());
    assert_eq!(s.header.treasury_principal, Presence::Absent);
    assert!(!s.header.activation_scheduled);
    assert_eq!(s.header.closing_request_digest, [0; 32]);
    assert_eq!(s.current, policy(1, 4));
    assert_eq!(s.pending, Presence::Absent);
    assert_eq!(s.operator, Presence::Absent);
    assert_eq!(
        s.recent(),
        &[PolicyHistoryHeader {
            config_version: 1,
            digest,
            effective_epoch: 0,
            disposition: ACTIVATED,
        }]
    );
    assert_eq!(s.history_root, initial_policy_history_root().unwrap());
    assert_eq!(s.task_region, &EMPTY_TASK_REGION);
    let shared = decode_shared_state(&state).unwrap();
    assert_eq!(shared.revision, 1);
    assert_eq!(shared.feature_sections[1..], [&[] as &[u8]; 4]);
    let owner = shared.control.replay.actor(ActorSlot::OWNER).unwrap();
    assert_eq!(owner.principal, p(OWNER));
    let last = owner.last.unwrap();
    assert_eq!((last.sequence, last.applied_revision), (1, 1));
    assert_eq!(last.result_digest, codec::result_digest(&receipt).unwrap());
    assert!(shared.control.replay.actor(ActorSlot::TREASURY).is_none());
    assert_eq!(receipt.len(), RECEIPT_MAX_BYTES);
    assert_eq!(&receipt[..32], market().as_bytes());
    assert_eq!(&receipt[32..40], &1u64.to_be_bytes());
    assert_eq!(&receipt[40..72], &[1; 32]);
    assert_eq!(&receipt[72..74], &0x0101u16.to_be_bytes());
    assert_eq!(&receipt[74..82], &1u64.to_be_bytes());
    assert_eq!(receipt[82], 1);
    assert_eq!(&receipt[83..], digest.as_bytes());
    let (operation, common, suffix) =
        codec::decode_event_frame(b"PAXAI/v1/CREATE", &event).unwrap();
    assert_eq!(operation, dispatch::CREATE);
    assert_eq!(common.market, market());
    assert_eq!(
        (common.epoch, common.config.get(), common.revision),
        (0, 1, 1)
    );
    assert_eq!(common.result, codec::result_digest(&receipt).unwrap());
    assert_eq!(suffix.len(), EVENT_SUFFIX_BYTES);
    assert_eq!(&suffix[17..], digest.as_bytes());

    let retry = retried(run(ctx(OWNER, 1000), Some(&state), &e));
    assert_eq!(retry.applied_revision, 1);
    let fresh = envelope(dispatch::CREATE, OWNER, 2, 2, &payload, 1);
    assert_eq!(
        refused(run(ctx(OWNER, 1001), Some(&state), &fresh)),
        F01_ALREADY_CREATED
    );
    let other = envelope(
        dispatch::CREATE,
        OTHER,
        1,
        3,
        &create_payload(
            OTHER,
            rewards(),
            &[0],
            &policy_bytes(&policy(1, 4)),
            [16; 32],
        ),
        1,
    );
    assert_eq!(
        refused(run(ctx(OTHER, 1001), Some(&state), &other)),
        F01_ALREADY_CREATED
    );
    assert_eq!(section_of(&state).header.origin_height, 1000);

    let mut bad_rewards = rewards().bytes();
    bad_rewards[31] ^= 1;
    let cases: [(Vec<u8>, u64, ApplicationError); 5] = [
        (
            create_payload(
                OWNER,
                AccountId::new(bad_rewards).unwrap(),
                &[0],
                &policy_bytes(&policy(1, 4)),
                [16; 32],
            ),
            1,
            F01_ACCOUNT_BINDING_MISSING,
        ),
        (
            create_payload(
                OWNER,
                rewards(),
                &[0],
                &policy_bytes(&policy(2, 4)),
                [16; 32],
            ),
            1,
            F01_VERSION_MISMATCH,
        ),
        (payload.clone(), 2, WRONG_CONFIG),
        (
            create_payload(
                OWNER,
                rewards(),
                &[0],
                &policy_bytes(&policy(1, 4)),
                [0; 32],
            ),
            1,
            NON_CANONICAL,
        ),
        (
            {
                let mut bytes = policy_bytes(&policy(1, 4));
                bytes[255] = 65;
                create_payload(OWNER, rewards(), &[0], &bytes, [16; 32])
            },
            1,
            F01_INVALID_POLICY,
        ),
    ];
    for (payload, config, error) in cases {
        let e = envelope(dispatch::CREATE, OWNER, 1, 1, &payload, config);
        assert_eq!(refused(run(ctx(OWNER, 1000), None, &e)), error);
    }
    let foreign = envelope_for(
        dispatch::CREATE,
        OWNER,
        1,
        1,
        &payload,
        1,
        [40; 32],
        PROGRAM,
    );
    assert_eq!(refused(run(ctx(OWNER, 1000), None, &foreign)), WRONG_DOMAIN);
    let expired = envelope(dispatch::CREATE, OWNER, 1, 1, &payload, 1);
    assert_eq!(refused(run(ctx(OWNER, 1_000_000), None, &expired)), EXPIRED);
}

#[test]
fn a03_policy_staging_activation_boundary_cancellation_and_history() {
    let mut m = Market::create(&[0]);
    let early = stage(1, &policy(2, 4), 1);
    assert_eq!(
        refused(m.send(dispatch::STAGE_POLICY, OWNER, 2, &early, 1130)),
        F01_ACTIVATION_TOO_EARLY
    );
    assert_eq!(
        refused(m.send(
            dispatch::STAGE_POLICY,
            OTHER,
            1,
            &stage(1, &policy(2, 4), 2),
            1130
        )),
        UNAUTHORIZED
    );
    applied(m.send(
        dispatch::STAGE_POLICY,
        OWNER,
        2,
        &stage(1, &policy(2, 4), 2),
        1130,
    ));
    let s = m.section();
    assert_eq!(s.current.config_version, 1);
    assert_eq!(s.header.active_config_version, 1);
    assert_eq!(s.header.highest_config_version, 2);
    assert_eq!(s.header.state_revision, 2);
    let pending = match s.pending {
        Presence::Present(pending) => pending,
        Presence::Absent => panic!("pending"),
    };
    assert_eq!(pending.policy, policy(2, 4));
    assert_eq!(pending.effective_epoch, 2);
    assert_eq!(pending.proposer, p(OWNER));
    assert_eq!(pending.digest, policy(2, 4).digest().unwrap());

    assert_eq!(market_clock(1000, 1256).unwrap().epoch, 2);
    assert_eq!(
        activate_pending_policy(&s, 1),
        Err(F01_ACTIVATION_TOO_EARLY)
    );
    let opened = activate_pending_policy(&s, 2).unwrap();
    assert_eq!(opened.current, policy(2, 4));
    assert_eq!(opened.header.active_config_version, 2);
    assert_eq!(opened.header.state_revision, s.header.state_revision);
    assert_eq!(opened.pending, Presence::Absent);
    assert_eq!(
        opened.recent().last(),
        Some(&PolicyHistoryHeader {
            config_version: 2,
            digest: pending.digest,
            effective_epoch: 2,
            disposition: ACTIVATED,
        })
    );
    assert_eq!(m.section(), s);
    let opened_bytes = section_bytes(&opened);
    let mut after_open = Market {
        state: with_sections(&m.state, &[(0, &opened_bytes[..])]),
        next_request: 50,
    };
    assert_eq!(
        refused(after_open.send(dispatch::CANCEL_POLICY, OWNER, 3, &u64s(&[2, 2]), 1256)),
        F01_ALREADY_ACTIVATED
    );

    assert_eq!(
        refused(m.send(dispatch::CANCEL_POLICY, OWNER, 3, &u64s(&[2, 3]), 1200)),
        F01_VERSION_MISMATCH
    );
    applied(m.send(dispatch::CANCEL_POLICY, OWNER, 3, &u64s(&[2, 2]), 1200));
    let s = m.section();
    assert_eq!(s.pending, Presence::Absent);
    assert_eq!(s.header.highest_config_version, 2);
    assert_eq!(s.current, policy(1, 4));
    assert_eq!(
        s.recent().last(),
        Some(&PolicyHistoryHeader {
            config_version: 2,
            digest: pending.digest,
            effective_epoch: 2,
            disposition: CANCELLED,
        })
    );
    assert_eq!(
        refused(m.send(dispatch::CANCEL_POLICY, OWNER, 4, &u64s(&[3, 2]), 1200)),
        F01_NO_PENDING_POLICY
    );
    assert_eq!(
        refused(m.send(
            dispatch::STAGE_POLICY,
            OWNER,
            4,
            &stage(3, &policy(2, 4), 2),
            1200
        )),
        F01_VERSION_MISMATCH
    );
    applied(m.send(
        dispatch::STAGE_POLICY,
        OWNER,
        4,
        &stage(3, &policy(3, 4), 2),
        1200,
    ));
    assert_eq!(
        refused(m.send(
            dispatch::STAGE_POLICY,
            OWNER,
            5,
            &stage(4, &policy(4, 4), 2),
            1200
        )),
        F01_PENDING_POLICY_EXISTS
    );
    applied(m.send(dispatch::CANCEL_POLICY, OWNER, 5, &u64s(&[4, 3]), 1200));
    applied(m.send(
        dispatch::STAGE_POLICY,
        OWNER,
        6,
        &stage(5, &policy(4, 4), 2),
        1200,
    ));
    applied(m.send(dispatch::CANCEL_POLICY, OWNER, 7, &u64s(&[6, 4]), 1200));
    let full_state = m.state.clone();
    let full = section_of(&full_state);
    assert_eq!(full.recent().len(), 4);
    assert_eq!(full.history_root, initial_policy_history_root().unwrap());
    let oldest = full.recent()[0];
    applied(m.send(
        dispatch::STAGE_POLICY,
        OWNER,
        8,
        &stage(7, &policy(5, 4), 2),
        1200,
    ));
    applied(m.send(dispatch::CANCEL_POLICY, OWNER, 9, &u64s(&[8, 5]), 1200));
    let s = m.section();
    assert_eq!(s.recent().len(), 4);
    assert_eq!(s.recent()[..3], full.recent()[1..]);
    assert_eq!(s.recent()[3].config_version, 5);
    assert_eq!(
        s.history_root,
        fold_policy_history(initial_policy_history_root().unwrap(), 0, &[oldest]).unwrap()
    );
    assert_eq!(s.header.highest_config_version, 5);
    assert_eq!(s.header.state_revision, 9);
}

#[test]
fn a05_suspend_preserves_admitted_work_and_clears_schedule() {
    let mut m = Market::create(&[0]);
    assert_eq!(
        refused(m.send(
            dispatch::SUSPEND,
            OWNER,
            2,
            &digest_payload(1, [50; 32], Some(0)),
            H
        )),
        F01_WRONG_LIFECYCLE
    );
    applied(m.send(
        dispatch::APPOINT_OPERATOR,
        OWNER,
        2,
        &appoint(1, OPERATOR_O, PERMIT_SUSPEND, 0),
        H,
    ));
    assert_eq!(
        refused(m.send(dispatch::SCHEDULE_ACTIVATION, OWNER, 3, &u64s(&[2, 0]), H)),
        F01_ACTIVATION_TOO_EARLY
    );
    applied(m.send(dispatch::SCHEDULE_ACTIVATION, OWNER, 3, &u64s(&[2, 1]), H));
    assert!(m.section().header.activation_scheduled);
    assert_eq!(m.section().header.activation_epoch, 1);
    assert_eq!(
        refused(m.send(dispatch::SCHEDULE_ACTIVATION, OWNER, 4, &u64s(&[3, 2]), H)),
        CONFLICT
    );

    let mut active = m.section();
    active.header.lifecycle = ACTIVE;
    let active_bytes = section_bytes(&active);
    let reports: &[u8] = b"admitted task and evaluator commitments";
    let claims: &[u8] = b"settlement and claim rights";
    let mut m = Market {
        state: with_sections(
            &m.state,
            &[(0, &active_bytes[..]), (2, reports), (3, claims)],
        ),
        next_request: m.next_request,
    };
    assert_eq!(
        refused(m.send(dispatch::SCHEDULE_ACTIVATION, OWNER, 4, &u64s(&[3, 3]), H)),
        F01_WRONG_LIFECYCLE
    );
    let at_63 = 1000 + 128 + 63;
    assert_eq!(market_clock(1000, at_63).unwrap().position, 63);
    assert_eq!(
        refused(m.send(
            dispatch::SUSPEND,
            OPERATOR_O,
            1,
            &digest_payload(3, [0; 32], Some(1)),
            at_63
        )),
        F01_INVALID_REASON
    );
    assert_eq!(
        refused(m.send(
            dispatch::UPDATE_METADATA,
            OPERATOR_O,
            1,
            &digest_payload(3, [51; 32], Some(1)),
            at_63
        )),
        UNAUTHORIZED
    );
    let before_state = m.state.clone();
    let before = section_of(&before_state);
    let (_, _, event) = applied(m.send(
        dispatch::SUSPEND,
        OPERATOR_O,
        1,
        &digest_payload(3, [50; 32], Some(1)),
        at_63,
    ));
    let s = m.section();
    assert_eq!(s.header.lifecycle, SUSPENDED);
    assert_eq!(s.header.suspension_reason_digest, [50; 32]);
    assert!(!s.header.activation_scheduled);
    assert_eq!(s.header.activation_epoch, 0);
    assert_eq!(s.header.state_revision, 4);
    assert_eq!(s.task_region, before.task_region);
    assert_eq!(s.current, before.current);
    let shared = decode_shared_state(&m.state).unwrap();
    assert_eq!(shared.feature_sections[2], reports);
    assert_eq!(shared.feature_sections[3], claims);
    let (_, common, _) = codec::decode_event_frame(b"PAXAI/v1/SUSPEND", &event).unwrap();
    assert_eq!((common.epoch, common.revision), (1, 4));
    let windows = EpochWindows::new(1000, 1).unwrap();
    assert_eq!(windows.phase(1000 + 128 + 64), EpochPhase::Commit);
    assert_eq!(windows.phase(1000 + 128 + 80), EpochPhase::Reveal);
    assert_eq!(windows.phase(1000 + 128 + 95), EpochPhase::Reveal);
    assert_eq!(windows.phase(1000 + 128 + 96), EpochPhase::Settlement);

    assert_eq!(
        refused(m.send(
            dispatch::SUSPEND,
            OPERATOR_O,
            2,
            &digest_payload(4, [52; 32], Some(1)),
            at_63
        )),
        F01_WRONG_LIFECYCLE
    );
    applied(m.send(
        dispatch::UPDATE_METADATA,
        OWNER,
        4,
        &digest_payload(4, [53; 32], None),
        at_63,
    ));
    applied(m.send(
        dispatch::SCHEDULE_ACTIVATION,
        OWNER,
        5,
        &u64s(&[5, 2]),
        at_63,
    ));
    let s = m.section();
    assert_eq!(
        s.header.metadata_digest,
        MetadataDigest::new([53; 32]).unwrap()
    );
    assert!(s.header.activation_scheduled);
    assert_eq!(s.header.lifecycle, SUSPENDED);
}

#[test]
fn a06_concurrent_stage_stale_revision_replay_and_domain_binding() {
    let mut m = Market::create(&[0]);
    for (i, sequence) in (2..8).enumerate() {
        let revision = m.revision();
        applied(m.send(
            dispatch::UPDATE_METADATA,
            OWNER,
            sequence,
            &digest_payload(revision, [30 + i as u8; 32], Some(0)),
            H,
        ));
    }
    assert_eq!(m.revision(), 7);
    let first_payload = stage(7, &policy(2, 4), 2);
    let first = m.envelope(dispatch::STAGE_POLICY, OWNER, 8, &first_payload);
    let first_request = m.next_request;
    let second = m.envelope(
        dispatch::STAGE_POLICY,
        OWNER,
        9,
        &stage(7, &policy(2, 5), 2),
    );

    let decoded = decode_shared_state(&m.state).unwrap();
    let env = decode_envelope(&first).unwrap();
    let mut section = vec![0; F01_SECTION_CAP];
    let mut short_event = [0; 16];
    assert_eq!(
        apply(
            &ctx(OWNER, H),
            Some(&decoded),
            &env,
            &mut section,
            &mut short_event
        ),
        Err(CAPACITY)
    );
    assert_eq!(m.revision(), 7);

    let (_, receipt, _) = applied(m.submit(OWNER, &first, H));
    assert_eq!(m.revision(), 8);
    assert_eq!(m.section().header.highest_config_version, 2);
    let committed = m.state.clone();
    assert_eq!(refused(m.submit(OWNER, &second, H)), F01_STALE_REVISION);
    assert_eq!(m.state, committed);
    assert_eq!(m.section().header.highest_config_version, 2);
    assert_eq!(
        next_config_version(m.section().header.highest_config_version),
        Ok(3)
    );

    let retry = retried(m.submit(OWNER, &first, H));
    assert_eq!(retry.applied_revision, 8);
    assert_eq!(retry.result_digest, codec::result_digest(&receipt).unwrap());
    assert_eq!(m.state, committed);

    let altered = envelope(
        dispatch::STAGE_POLICY,
        OWNER,
        8,
        first_request,
        &stage(7, &policy(2, 9), 2),
        1,
    );
    assert_eq!(refused(m.submit(OWNER, &altered, H)), REPLAY_CONFLICT);
    let other_program = envelope_for(
        dispatch::STAGE_POLICY,
        OWNER,
        8,
        first_request,
        &first_payload,
        1,
        CHAIN,
        [99; 32],
    );
    assert_eq!(refused(m.submit(OWNER, &other_program, H)), WRONG_PROGRAM);
    let other_chain = envelope_for(
        dispatch::STAGE_POLICY,
        OWNER,
        8,
        first_request,
        &first_payload,
        1,
        [98; 32],
        PROGRAM,
    );
    assert_eq!(refused(m.submit(OWNER, &other_chain, H)), WRONG_DOMAIN);
    let gap = m.envelope(dispatch::CANCEL_POLICY, OWNER, 11, &u64s(&[8, 2]));
    assert_eq!(refused(m.submit(OWNER, &gap, H)), SEQUENCE_GAP);
    let consumed = m.envelope(dispatch::CANCEL_POLICY, OWNER, 7, &u64s(&[8, 2]));
    assert_eq!(refused(m.submit(OWNER, &consumed, H)), SEQUENCE_CONSUMED);
    assert_eq!(m.state, committed);
    applied(m.send(dispatch::CANCEL_POLICY, OWNER, 9, &u64s(&[8, 2]), H));
    assert_eq!(m.revision(), 9);
}

#[test]
fn a10_operator_grant_replacement_revocation_and_metadata_isolation() {
    let created = Market::create(&[0]);
    let reports: &[u8] = b"frozen scores";
    let claims: &[u8] = b"claim entitlements";
    let mut m = Market {
        state: with_sections(&created.state, &[(2, reports), (3, claims)]),
        next_request: created.next_request,
    };
    assert_eq!(
        refused(m.send(
            dispatch::APPOINT_OPERATOR,
            OWNER,
            2,
            &appoint(1, OWNER, 3, 0),
            H
        )),
        ROLE_CONFLICT
    );
    for permissions in [0, 4] {
        assert_eq!(
            refused(m.send(
                dispatch::APPOINT_OPERATOR,
                OWNER,
                2,
                &appoint(1, OPERATOR_O, permissions, 0),
                H
            )),
            NON_CANONICAL
        );
    }
    let mut sequence = 2;
    for expected in 0..4 {
        let revision = m.revision();
        applied(m.send(
            dispatch::APPOINT_OPERATOR,
            OWNER,
            sequence,
            &appoint(revision, OPERATOR_O, 3, expected),
            H,
        ));
        sequence += 1;
    }
    let grant = |m: &Market| match m.section().operator {
        Presence::Present(grant) => grant,
        Presence::Absent => panic!("operator"),
    };
    assert_eq!(grant(&m).sequence, 4);
    assert_eq!(
        refused(m.send(
            dispatch::APPOINT_OPERATOR,
            OWNER,
            sequence,
            &appoint(5, OPERATOR_P, 3, 1),
            H
        )),
        F01_VERSION_MISMATCH
    );

    let before_state = m.state.clone();
    let before = section_of(&before_state);
    let old_request = m.envelope(
        dispatch::UPDATE_METADATA,
        OPERATOR_O,
        1,
        &digest_payload(5, [60; 32], Some(4)),
    );
    applied(m.submit(OPERATOR_O, &old_request, H));
    let after = m.section();
    assert_eq!(
        after.header.metadata_digest,
        MetadataDigest::new([60; 32]).unwrap()
    );
    assert_eq!(
        after.header.state_revision,
        before.header.state_revision + 1
    );
    assert_eq!(after.current.digest(), before.current.digest());
    assert_eq!(after.current, before.current);
    assert_eq!(after.pending, before.pending);
    assert_eq!(after.recent(), before.recent());
    assert_eq!(after.history_root, before.history_root);
    let mut normalized = after;
    normalized.header.metadata_digest = before.header.metadata_digest;
    normalized.header.state_revision = before.header.state_revision;
    assert_eq!(normalized, before);

    applied(m.send(
        dispatch::APPOINT_OPERATOR,
        OWNER,
        sequence,
        &appoint(6, OPERATOR_P, 3, 4),
        H,
    ));
    sequence += 1;
    assert_eq!(grant(&m).sequence, 5);
    assert_eq!(grant(&m).principal, p(OPERATOR_P));
    assert_eq!(
        refused(m.send(
            dispatch::UPDATE_METADATA,
            OPERATOR_O,
            2,
            &digest_payload(7, [61; 32], Some(4)),
            H
        )),
        UNAUTHORIZED
    );
    assert_eq!(refused(m.submit(OPERATOR_O, &old_request, H)), UNAUTHORIZED);
    assert_eq!(
        refused(m.send(
            dispatch::UPDATE_METADATA,
            OPERATOR_P,
            1,
            &digest_payload(7, [61; 32], Some(4)),
            H
        )),
        UNAUTHORIZED
    );
    let p_request = m.envelope(
        dispatch::UPDATE_METADATA,
        OPERATOR_P,
        1,
        &digest_payload(7, [62; 32], Some(5)),
    );
    applied(m.submit(OPERATOR_P, &p_request, H));
    assert_eq!(
        refused(m.send(
            dispatch::UPDATE_METADATA,
            OWNER,
            sequence,
            &digest_payload(8, [63; 32], Some(5)),
            H
        )),
        UNAUTHORIZED
    );
    applied(m.send(
        dispatch::UPDATE_METADATA,
        OWNER,
        sequence,
        &digest_payload(8, [63; 32], None),
        H,
    ));
    sequence += 1;

    assert_eq!(
        refused(m.send(
            dispatch::REVOKE_OPERATOR,
            OWNER,
            sequence,
            &u64s(&[9, 4]),
            H
        )),
        F01_VERSION_MISMATCH
    );
    let revoke = m.envelope(dispatch::REVOKE_OPERATOR, OWNER, sequence, &u64s(&[9, 5]));
    applied(m.submit(OWNER, &revoke, H));
    sequence += 1;
    assert!(grant(&m).revoked);
    assert_eq!(m.revision(), 10);
    assert_eq!(retried(m.submit(OWNER, &revoke, H)).applied_revision, 10);
    assert_eq!(
        refused(m.send(
            dispatch::REVOKE_OPERATOR,
            OWNER,
            sequence,
            &u64s(&[10, 5]),
            H
        )),
        F01_GRANT_ALREADY_REVOKED
    );
    assert_eq!(
        refused(m.send(
            dispatch::UPDATE_METADATA,
            OPERATOR_P,
            2,
            &digest_payload(10, [64; 32], Some(5)),
            H
        )),
        REVOKED
    );
    assert_eq!(refused(m.submit(OPERATOR_P, &p_request, H)), REVOKED);
    let shared = decode_shared_state(&m.state).unwrap();
    assert_eq!(shared.feature_sections[2], reports);
    assert_eq!(shared.feature_sections[3], claims);

    applied(m.send(
        dispatch::APPOINT_OPERATOR,
        OWNER,
        sequence,
        &appoint(10, OPERATOR_O, PERMIT_METADATA, 5),
        H,
    ));
    assert_eq!(grant(&m).sequence, 6);
    let replay = decode_shared_state(&m.state)
        .unwrap()
        .control
        .replay
        .actor(ActorSlot::OPERATOR)
        .unwrap();
    assert_eq!(
        (
            replay.principal,
            replay.authority_version.get(),
            replay.last
        ),
        (p(OPERATOR_O), 6, None)
    );
    applied(m.send(
        dispatch::UPDATE_METADATA,
        OPERATOR_O,
        1,
        &digest_payload(11, [65; 32], Some(6)),
        H,
    ));
    assert_eq!(m.revision(), 12);
}

#[test]
fn a19_treasury_binding_presence_rules_and_section_worksheet() {
    let absent = Market::create(&[0]);
    assert_eq!(absent.section().header.treasury_principal, Presence::Absent);
    let mut treasury_bytes = vec![1];
    treasury_bytes.extend_from_slice(&TREASURY);
    let mut m = Market::create(&treasury_bytes);
    let s = m.section();
    assert_eq!(s.header.treasury_principal, Presence::Present(p(TREASURY)));
    assert_eq!(s.header.owner_principal, p(OWNER));
    assert_eq!(
        s.header.refund_recipient_account,
        AccountId::new(REFUND).unwrap()
    );
    assert_eq!(s.header.encoded_len(), MARKET_HEADER_PRESENT_BYTES);
    let treasury = decode_shared_state(&m.state)
        .unwrap()
        .control
        .replay
        .actor(ActorSlot::TREASURY)
        .unwrap();
    assert_eq!(
        (
            treasury.principal,
            treasury.authority_version.get(),
            treasury.last
        ),
        (p(TREASURY), 1, None)
    );

    let mut owner_as_treasury = vec![1];
    owner_as_treasury.extend_from_slice(&OWNER);
    let mut zero_treasury = vec![1];
    zero_treasury.extend_from_slice(&[0; 32]);
    let mut stray = vec![0];
    stray.extend_from_slice(&TREASURY);
    let mut presence_two = vec![2];
    presence_two.extend_from_slice(&TREASURY);
    for (raw, error) in [
        (presence_two, Some(NON_CANONICAL)),
        (zero_treasury, Some(NON_CANONICAL)),
        (owner_as_treasury, Some(F01_PRINCIPAL_MISMATCH)),
        (stray, None),
    ] {
        let payload = create_payload(
            OWNER,
            rewards(),
            &raw,
            &policy_bytes(&policy(1, 4)),
            [16; 32],
        );
        let e = envelope(dispatch::CREATE, OWNER, 1, 1, &payload, 1);
        let result = refused(run(ctx(OWNER, 1000), None, &e));
        if let Some(error) = error {
            assert_eq!(result, error);
        }
    }
    assert_eq!(
        refused(m.send(
            dispatch::APPOINT_OPERATOR,
            OWNER,
            2,
            &appoint(1, TREASURY, 3, 0),
            H
        )),
        ROLE_CONFLICT
    );
    assert_eq!(
        refused(m.send(
            dispatch::UPDATE_METADATA,
            TREASURY,
            1,
            &digest_payload(1, [70; 32], Some(0)),
            H
        )),
        UNAUTHORIZED
    );
    assert_eq!(
        refused(m.send(
            dispatch::STAGE_POLICY,
            TREASURY,
            1,
            &stage(1, &policy(2, 4), 2),
            H
        )),
        UNAUTHORIZED
    );

    applied(m.send(
        dispatch::APPOINT_OPERATOR,
        OWNER,
        2,
        &appoint(1, OPERATOR_O, 3, 0),
        H,
    ));
    let mut sequence = 3;
    for version in 2..5 {
        let revision = m.revision();
        applied(m.send(
            dispatch::STAGE_POLICY,
            OWNER,
            sequence,
            &stage(revision, &policy(version, 4), 2),
            H,
        ));
        applied(m.send(
            dispatch::CANCEL_POLICY,
            OWNER,
            sequence + 1,
            &u64s(&[revision + 1, version]),
            H,
        ));
        sequence += 2;
    }
    let revision = m.revision();
    applied(m.send(
        dispatch::STAGE_POLICY,
        OWNER,
        sequence,
        &stage(revision, &policy(5, 4), 2),
        H,
    ));
    let full = m.section();
    assert_eq!(full.recent().len(), 4);
    assert_eq!(
        full.header.treasury_principal,
        Presence::Present(p(TREASURY))
    );
    assert!(matches!(full.operator, Presence::Present(_)));
    assert!(matches!(full.pending, Presence::Present(_)));

    let tasks = vec![0; 2 + 64 * TASK_BINDING_DESIGN_BYTES + 33];
    let mut maximum = full;
    maximum.task_region = &tasks;
    assert_eq!(maximum.encoded_len(), Ok(16_335));
    assert_eq!(maximum_f01_worksheet().unwrap().section_bytes, 16_335);
    let bytes = section_bytes(&maximum);
    assert_eq!(bytes.len(), 16_335);
    assert_eq!(PolicySection::decode(&bytes), Ok(maximum));

    let over = vec![0; tasks.len() + SECTION_PAYLOAD_CAP - 16_335 + 1];
    let mut oversized = full;
    oversized.task_region = &over;
    assert_eq!(oversized.encoded_len(), Err(F01_CAPACITY_UNAVAILABLE));
    let mut output = vec![0xa5; 20_000];
    assert_eq!(oversized.encode(&mut output), Err(F01_CAPACITY_UNAVAILABLE));
    assert!(output.iter().all(|b| *b == 0xa5));
    let fits = vec![0; tasks.len() + SECTION_PAYLOAD_CAP - 16_335];
    let mut edge = full;
    edge.task_region = &fits;
    assert_eq!(edge.encoded_len(), Ok(SECTION_PAYLOAD_CAP));
}
