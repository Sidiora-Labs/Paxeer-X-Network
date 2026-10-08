use ed25519_dalek::{Signer, SigningKey};
use layerx_programs_ai_market::{
    admission::{
        admit_evaluator, Admission, AdmissionContext, AdmissionMeta, AdmissionTable, Approval,
        ApprovalTerms, EvaluatorConsent, ExitCause, ExitReason, Participant, PendingExit,
        RemovalReason, Role, ADMITTED_META_MAX_BYTES, APPROVAL_META_BYTES, FLAG_ADMITTED,
        TABLE_HEADER_MAX_BYTES, TABLE_MAX_BYTES,
    },
    codec::{derive_evaluator, derive_market},
    errors::{
        CodecResult, ARITHMETIC, CONFLICT, F08_ADMINISTRATOR_APPROVAL_REQUIRED,
        F08_ADMISSION_WINDOW_FULL, F08_BAD_CONSENT, F08_CANDIDATE_CHANGED, F08_CAPACITY_EXCEEDED,
        F08_DELEGATE_REVOKED, F08_DUPLICATE_IDENTITY, F08_IDEMPOTENCY_CONFLICT, F08_MARKET_PAUSED,
        F08_NO_PRUNABLE_MEMBER, F08_OWNER_CAPACITY_EXCEEDED, F08_OWNER_REQUIRED,
        F08_PERMIT_CONSUMED, F08_PERMIT_EXPIRED, F08_QUORUM_UNAVAILABLE, F08_RATE_LIMITED,
        F08_RETENTION_BLOCKED, F08_STALE_STATE, F08_WRONG_GENERATION, NON_CANONICAL, NOT_FOUND,
        ROLE_CONFLICT, WRONG_DOMAIN, WRONG_EPOCH, WRONG_PHASE,
    },
    evaluators::model::{EvaluatorGrant, GrantStatus, GrantTerms},
    registry::{derive_rewards_account, MarketHeader},
    roster,
    state::{self, ActorSlot, Control, ReplayTable, Section, SharedState},
    types::{
        AccountId, AssetId, ChainDomain, Digest32, EvaluatorId, MetadataDigest, Presence,
        PrincipalId, ProgramId, PublicKey32, RequestId, RubricDigest, Version, WorkerId,
    },
    MAX_STATE_BYTES,
};

type TestResult = CodecResult<()>;

const OWNER: [u8; 32] = [12; 32];

fn header(origin: u64) -> CodecResult<MarketHeader> {
    let chain = ChainDomain::new([10; 32])?;
    let program = ProgramId::new([11; 32])?;
    let asset = AssetId::new([13; 32])?;
    Ok(MarketHeader {
        format_version: 1,
        market_id: derive_market(chain, program)?,
        deployment_chain_domain: chain,
        program_id: program,
        owner_principal: PrincipalId::new(OWNER)?,
        funding_asset: asset,
        rewards_account: derive_rewards_account(program, asset)?,
        refund_recipient_account: AccountId::new([14; 32])?,
        treasury_principal: Presence::Absent,
        origin_height: origin,
        lifecycle: 2,
        state_revision: 1,
        highest_config_version: 1,
        active_config_version: 1,
        activation_epoch: 0,
        activation_scheduled: false,
        closure_requested_at: 0,
        close_phase: 0,
        close_cursor: 0,
        suspension_reason_digest: [0; 32],
        metadata_digest: MetadataDigest::new([16; 32])?,
        closing_request_digest: [0; 32],
        reserved: [0; 8],
    })
}
fn p(b: u8) -> CodecResult<PrincipalId> {
    PrincipalId::new([b; 32])
}
fn id(n: u8, role: u8) -> [u8; 32] {
    let mut v = [n; 32];
    v[0] = role;
    v[31] = n.wrapping_add(1);
    v
}
fn worker(n: u8) -> CodecResult<Participant> {
    Ok(Participant::Worker(WorkerId::new(id(n, 1))?))
}
fn evaluator(n: u8) -> CodecResult<Participant> {
    Ok(Participant::Evaluator(EvaluatorId::new(id(n, 2))?))
}
fn ctx(market: &MarketHeader, who: PrincipalId, height: u64) -> AdmissionContext<'_> {
    AdmissionContext {
        market,
        invoking_principal: who,
        height,
    }
}
fn admin(market: &MarketHeader, height: u64) -> CodecResult<AdmissionContext<'_>> {
    Ok(ctx(market, p(OWNER[0])?, height))
}
fn terms(
    participant: Participant,
    owner: PrincipalId,
    effective: u64,
    expiry: u64,
) -> CodecResult<ApprovalTerms> {
    Ok(ApprovalTerms {
        participant,
        owner,
        enrollment_nonce_commitment: Digest32::new([7; 32])?,
        delegate: PublicKey32([8; 32]),
        delegate_generation: 1,
        identity_commitment: Digest32::new([9; 32])?,
        effective_epoch: effective,
        config_version: 1,
        request: RequestId::new([participant.bytes()[31]; 32])?,
        expiry_height: expiry,
    })
}
fn admission(participant: Participant, effective: u64, digest: Digest32) -> Admission {
    Admission {
        participant,
        delegate_generation: 1,
        effective_epoch: effective,
        config_version: 1,
        approval_digest: digest,
    }
}
fn no_retention(_: &AdmissionMeta) -> bool {
    false
}

/// Approve and admit a member at `height`, both bound to the required effective epoch.
fn admit_role(
    table: &mut AdmissionTable,
    market: &MarketHeader,
    participant: Participant,
    owner: PrincipalId,
    height: u64,
) -> CodecResult<AdmissionMeta> {
    let effective = table.required_effective_epoch(&ctx(market, owner, height))?;
    let work_end = market.origin_height + effective * 128 + 64;
    let tm = terms(participant, owner, effective, work_end.max(height + 1))?;
    let d = table.approve(&admin(market, height)?, &tm)?;
    table.admit(
        &ctx(market, owner, height),
        &admission(participant, effective, d),
    )
}
fn enroll(
    table: &mut AdmissionTable,
    market: &MarketHeader,
    n: u8,
    owner: PrincipalId,
    height: u64,
) -> CodecResult<AdmissionMeta> {
    admit_role(table, market, worker(n)?, owner, height)
}

fn opened(epoch: u64) -> CodecResult<AdmissionTable> {
    let mut table = AdmissionTable::new();
    roster::open_epoch(&mut table, epoch, no_retention)?;
    Ok(table)
}
fn next_epoch(table: &AdmissionTable) -> CodecResult<u64> {
    Ok(table.current_epoch().ok_or(WRONG_EPOCH)? + 1)
}

fn advance(table: &mut AdmissionTable) -> CodecResult<roster::Rollover> {
    let epoch = next_epoch(table)?;
    roster::open_epoch(table, epoch, no_retention)
}

fn persist(table: &AdmissionTable) -> CodecResult<Vec<u8>> {
    let mut section = vec![0u8; TABLE_MAX_BYTES];
    let n = table.encode(&mut section)?;
    let mut replay = ReplayTable::new();
    replay.bind(ActorSlot::OWNER, p(OWNER[0])?, Version::new(1)?)?;
    let shared = SharedState {
        revision: 1,
        feature_sections: [&[], &[], &[], &[], &section[..n]],
        control: Control {
            replay,
            feature_bytes: &[],
        },
    };
    let mut out = vec![0u8; MAX_STATE_BYTES];
    let mut scratch = vec![0u8; 24_576];
    let len = state::encode_shared_state(&shared, &mut out, &mut scratch)?;
    out.truncate(len);
    Ok(out)
}
fn restore(bytes: &[u8]) -> CodecResult<AdmissionTable> {
    let s = state::decode_shared_state(bytes)?;
    AdmissionTable::decode(s.section(Section::ReputationAdmission)?)
}
fn frozen(table: &AdmissionTable) -> usize {
    table.iter().filter(|m| m.admitted_epoch.is_some()).count()
}
fn member(n: u8, last: u64, admitted: u64) -> CodecResult<AdmissionMeta> {
    Ok(AdmissionMeta {
        participant: worker(n)?,
        owner: p(n + 100)?,
        admitted_epoch: Some(admitted),
        last_heartbeat_epoch: Some(last),
        last_heartbeat_height: Some(last * 128),
        immunity_until_epoch: admitted,
        pending_exit: None,
        membership_generation: 1,
        complete_missed_opened_epochs: 2,
        membership_flags: FLAG_ADMITTED,
        delegate_generation: 1,
        admission_height: Some(admitted * 128),
        approval: None,
    })
}

#[test]
fn a01_enroll_stages_next_epoch_then_rollover_includes_once() -> TestResult {
    let market = header(0)?;
    let mut table = opened(4)?;
    let m = enroll(&mut table, &market, 1, p(40)?, 520)?;
    assert_eq!(m.immunity_until_epoch, 5);
    assert_eq!(table.role_count(Role::Worker), 1);
    assert_eq!(frozen(&table), 0);
    assert_eq!(m.admitted_epoch, None);
    assert_eq!(m.last_heartbeat_epoch, None);
    let r = roster::open_epoch(&mut table, 5, no_retention)?;
    assert_eq!((r.installed, r.members), (1, 1));
    assert_eq!(
        table.get(worker(1)?).map(|m| m.admitted_epoch),
        Some(Some(5))
    );
    assert_eq!(
        roster::open_epoch(&mut table, 5, no_retention),
        Err(WRONG_EPOCH)
    );
    Ok(())
}

#[test]
fn a02_capacity_refuses_without_consumption_until_exit_and_rollover() -> TestResult {
    let market = header(0)?;
    let mut table = opened(0)?;
    let mut height = 10;
    for n in 0..32u8 {
        if n % 4 == 0 && n > 0 {
            advance(&mut table)?;
            height = next_epoch(&table)? * 128 - 118;
        }
        enroll(&mut table, &market, n, p(100 + n)?, height)?;
    }
    advance(&mut table)?;
    height = next_epoch(&table)? * 128 - 118;
    let before = table;
    let e = table.required_effective_epoch(&ctx(&market, p(200)?, height))?;
    let tm = terms(worker(200)?, p(200)?, e, height + 20)?;
    assert_eq!(
        table.approve(&admin(&market, height)?, &tm),
        Err(F08_CAPACITY_EXCEEDED)
    );
    assert_eq!(table, before);
    table.request_exit(
        &ctx(&market, p(103)?, height),
        worker(3)?,
        1,
        next_epoch(&table)?,
        ExitReason::Voluntary,
    )?;
    let frozen_before = frozen(&table);
    assert_eq!(
        enroll(&mut table, &market, 200, p(200)?, height),
        Err(F08_CAPACITY_EXCEEDED)
    );
    assert_eq!(frozen(&table), frozen_before);
    let r = advance(&mut table)?;
    assert_eq!(r.removed, 1);
    height = next_epoch(&table)? * 128 - 118;
    let staged = enroll(&mut table, &market, 200, p(200)?, height)?;
    assert_eq!(staged.immunity_until_epoch, next_epoch(&table)?);
    assert_eq!(staged.admitted_epoch, None);
    Ok(())
}

#[test]
fn a03_per_owner_limits_and_role_conflict() -> TestResult {
    let market = header(0)?;
    let mut table = opened(0)?;
    enroll(&mut table, &market, 1, p(50)?, 10)?;
    enroll(&mut table, &market, 2, p(50)?, 30)?;
    roster::open_epoch(&mut table, 1, no_retention)?;
    assert_eq!(
        enroll(&mut table, &market, 3, p(50)?, 140),
        Err(F08_OWNER_CAPACITY_EXCEEDED)
    );
    admit_role(&mut table, &market, evaluator(10)?, p(60)?, 160)?;
    assert_eq!(
        admit_role(&mut table, &market, evaluator(11)?, p(60)?, 180),
        Err(F08_OWNER_CAPACITY_EXCEEDED)
    );
    assert_eq!(
        admit_role(&mut table, &market, evaluator(12)?, p(50)?, 180),
        Err(ROLE_CONFLICT)
    );
    assert_eq!(
        admit_role(&mut table, &market, worker(14)?, p(60)?, 200),
        Err(ROLE_CONFLICT)
    );
    assert_eq!(
        admit_role(&mut table, &market, evaluator(15)?, p(OWNER[0])?, 200),
        Err(ROLE_CONFLICT)
    );
    admit_role(&mut table, &market, evaluator(13)?, p(61)?, 180)?;
    Ok(())
}

#[test]
fn a04_owner_cooldown_and_epoch_window() -> TestResult {
    let market = header(0)?;
    let mut table = opened(4)?;
    enroll(&mut table, &market, 1, p(70)?, 600)?;
    assert_eq!(
        enroll(&mut table, &market, 2, p(70)?, 615),
        Err(F08_RATE_LIMITED)
    );
    assert_eq!(table.enrollments_this_epoch(), 1);
    enroll(&mut table, &market, 2, p(70)?, 616)?;
    let tm = terms(worker(3)?, p(71)?, 5, 640)?;
    table.approve(&admin(&market, 617)?, &tm)?;
    assert_eq!(
        table.admit(
            &ctx(&market, p(71)?, 617),
            &admission(worker(3)?, 5, Digest32::new([1; 32])?)
        ),
        Err(F08_BAD_CONSENT)
    );
    assert_eq!(table.enrollments_this_epoch(), 2);
    enroll(&mut table, &market, 3, p(71)?, 618)?;
    enroll(&mut table, &market, 4, p(72)?, 619)?;
    assert_eq!(
        enroll(&mut table, &market, 5, p(73)?, 620),
        Err(F08_ADMISSION_WINDOW_FULL)
    );
    assert_eq!(table.enrollments_this_epoch(), 4);
    Ok(())
}

#[test]
fn a05_inactivity_needs_two_complete_opened_absences() -> TestResult {
    let market = header(0)?;
    let mut table = opened(6)?;
    enroll(&mut table, &market, 1, p(80)?, 6 * 128 + 5)?;
    roster::open_epoch(&mut table, 7, no_retention)?;
    let mut skipped = table;
    roster::open_epoch(&mut table, 8, no_retention)?;
    assert_eq!(
        roster::prune_candidate(&table, Role::Worker, 8),
        Err(F08_NO_PRUNABLE_MEMBER)
    );
    roster::open_epoch(&mut table, 9, no_retention)?;
    assert_eq!(
        roster::prune_candidate(&table, Role::Worker, 9),
        Err(F08_NO_PRUNABLE_MEMBER)
    );
    roster::open_epoch(&mut table, 10, no_retention)?;
    assert_eq!(
        roster::prune_candidate(&table, Role::Worker, 10)?.participant,
        worker(1)?
    );
    let mut live = table;
    live.heartbeat(&ctx(&market, p(80)?, 10 * 128 + 1), worker(1)?, 1, 1, 10)?;
    assert_eq!(
        roster::prune_candidate(&live, Role::Worker, 10),
        Err(F08_NO_PRUNABLE_MEMBER)
    );
    roster::open_epoch(&mut skipped, 10, no_retention)?;
    assert_eq!(
        skipped
            .get(worker(1)?)
            .map(|m| m.complete_missed_opened_epochs),
        Some(0)
    );
    assert_eq!(
        roster::prune_candidate(&skipped, Role::Worker, 10),
        Err(F08_NO_PRUNABLE_MEMBER)
    );
    let pruned = table.prune_inactive(worker(1)?, 3, 3)?;
    assert!(pruned.draining());
    assert_eq!(
        pruned.pending_exit,
        Some(PendingExit {
            epoch: 11,
            cause: ExitCause::Inactivity
        })
    );
    assert_eq!(table.prune_inactive(worker(1)?, 3, 4), Err(F08_STALE_STATE));
    assert_eq!(
        table.prune_inactive(worker(1)?, 3, 3),
        Err(F08_NO_PRUNABLE_MEMBER)
    );
    Ok(())
}

#[test]
fn a06_deterministic_longest_idle_then_oldest_then_id() -> TestResult {
    let mut table = AdmissionTable::new();
    table.insert(member(30, 4, 1)?)?;
    table.insert(member(20, 3, 2)?)?;
    table.insert(member(10, 3, 1)?)?;
    assert_eq!(
        roster::prune_candidate(&table, Role::Worker, 7)?.participant,
        worker(10)?
    );
    let mut u = opened(7)?;
    u.insert(member(9, 3, 1)?)?;
    u.insert(member(5, 3, 1)?)?;
    assert_eq!(
        roster::prune_candidate(&u, Role::Worker, 7)?.participant,
        worker(5)?
    );
    assert_eq!(
        u.prune_inactive(worker(9)?, 1, 1),
        Err(F08_CANDIDATE_CHANGED)
    );
    Ok(())
}

#[test]
fn a07_no_fallback_and_security_revocation_during_immunity() -> TestResult {
    let market = header(0)?;
    let mut table = opened(0)?;
    enroll(&mut table, &market, 1, p(OWNER[0] + 1)?, 10)?;
    roster::open_epoch(&mut table, 1, no_retention)?;
    assert_eq!(
        table.prune_inactive(worker(1)?, 1, 1),
        Err(F08_NO_PRUNABLE_MEMBER)
    );
    assert_eq!(
        table.administrative_remove(
            &ctx(&market, p(13)?, 130),
            worker(1)?,
            1,
            RemovalReason::Terms
        ),
        Err(F08_ADMINISTRATOR_APPROVAL_REQUIRED)
    );
    let m = table.administrative_remove(
        &admin(&market, 130)?,
        worker(1)?,
        1,
        RemovalReason::Security,
    )?;
    assert!(m.revoked());
    assert_eq!(
        table.heartbeat(&ctx(&market, p(13)?, 150), worker(1)?, 1, 1, 1),
        Err(F08_DELEGATE_REVOKED)
    );
    let r = roster::open_epoch(&mut table, 2, no_retention)?;
    assert_eq!((r.removed, r.members), (1, 0));
    Ok(())
}

#[test]
fn a08_heartbeat_distance_and_bindings() -> TestResult {
    let market = header(0)?;
    let mut table = opened(4)?;
    enroll(&mut table, &market, 1, p(90)?, 520)?;
    let w = worker(1)?;
    assert_eq!(
        table.heartbeat(&ctx(&market, p(90)?, 600), w, 1, 1, 4),
        Err(NOT_FOUND)
    );
    roster::open_epoch(&mut table, 5, no_retention)?;
    table.heartbeat(&ctx(&market, p(90)?, 700), w, 1, 1, 5)?;
    let before = table;
    assert_eq!(
        table.heartbeat(&ctx(&market, p(90)?, 715), w, 1, 1, 5),
        Err(F08_RATE_LIMITED)
    );
    assert_eq!(
        table.heartbeat(&ctx(&market, p(90)?, 716), w, 1, 1, 6),
        Err(WRONG_EPOCH)
    );
    assert_eq!(
        table.heartbeat(&ctx(&market, p(90)?, 716), w, 2, 1, 5),
        Err(F08_WRONG_GENERATION)
    );
    assert_eq!(
        table.heartbeat(&ctx(&market, p(90)?, 716), worker(2)?, 1, 1, 5),
        Err(NOT_FOUND)
    );
    assert_eq!(table, before);
    let m = table.heartbeat(&ctx(&market, p(90)?, 716), w, 1, 1, 5)?;
    assert_eq!(m.last_heartbeat_height, Some(716));
    assert_eq!(m.membership_generation, 1);
    Ok(())
}

#[test]
fn a09_last_vacancy_one_winner_and_restart_same_root() -> TestResult {
    let market = header(0)?;
    let mut table = opened(0)?;
    for n in 0..31u8 {
        let mut m = member(n, 0, 0)?;
        m.complete_missed_opened_epochs = 0;
        table.insert(m)?;
    }
    let a = terms(worker(200)?, p(201)?, 1, 60)?;
    let b = terms(worker(210)?, p(211)?, 1, 60)?;
    let da = table.approve(&admin(&market, 20)?, &a)?;
    assert_eq!(
        table.approve(&admin(&market, 20)?, &b),
        Err(F08_CAPACITY_EXCEEDED)
    );
    table.admit(&ctx(&market, p(201)?, 21), &admission(worker(200)?, 1, da))?;
    assert_eq!(table.role_count(Role::Worker), 32);
    assert!(table.get(worker(210)?).is_none());
    let back = restore(&persist(&table)?)?;
    assert_eq!(back, table);
    assert_eq!(
        roster::roster_digest(&back, 0)?,
        roster::roster_digest(&table, 0)?
    );
    Ok(())
}

#[test]
fn a10_rotation_keeps_history_and_old_delegate_refused() -> TestResult {
    let market = header(0)?;
    let mut table = opened(0)?;
    enroll(&mut table, &market, 1, p(95)?, 10)?;
    roster::open_epoch(&mut table, 1, no_retention)?;
    table.heartbeat(&ctx(&market, p(95)?, 130), worker(1)?, 1, 1, 1)?;
    let mut m = table.get(worker(1)?).ok_or(NOT_FOUND)?;
    let history = (m.admitted_epoch, m.immunity_until_epoch, m.owner);
    m.delegate_generation = 2;
    table.replace(m)?;
    assert_eq!(
        table.heartbeat(&ctx(&market, p(95)?, 150), worker(1)?, 1, 1, 1),
        Err(F08_WRONG_GENERATION)
    );
    let m = table.heartbeat(&ctx(&market, p(95)?, 150), worker(1)?, 1, 2, 1)?;
    assert_eq!((m.admitted_epoch, m.immunity_until_epoch, m.owner), history);
    Ok(())
}

#[test]
fn a11_quorum_health_drops_on_revocation() -> TestResult {
    let market = header(0)?;
    let mut table = AdmissionTable::new();
    for (n, owner) in [(1u8, 61u8), (2, 62), (3, 63)] {
        admit_role(
            &mut table,
            &market,
            evaluator(n)?,
            p(owner)?,
            u64::from(n) * 20,
        )?;
    }
    let r = roster::open_epoch(&mut table, 0, no_retention)?;
    assert!(r.health.quorum_ready);
    roster::require_quorum(&table)?;
    table.administrative_remove(
        &admin(&market, 70)?,
        evaluator(1)?,
        1,
        RemovalReason::Security,
    )?;
    let health = roster::health(&table)?;
    assert_eq!(
        (health.roster_evaluators, health.eligible_evaluators),
        (3, 2)
    );
    assert_eq!(roster::require_quorum(&table), Err(F08_QUORUM_UNAVAILABLE));
    let staged = admit_role(&mut table, &market, evaluator(4)?, p(64)?, 80)?;
    assert_eq!(staged.admitted_epoch, None);
    assert_eq!(roster::health(&table)?.roster_evaluators, 3);
    Ok(())
}

#[test]
fn a14_overflow_and_noncanonical_refuse_before_mutation() -> TestResult {
    let market = header(0)?;
    let mut table = opened(u64::MAX)?;
    let mut m = member(1, 0, 0)?;
    m.complete_missed_opened_epochs = 0;
    table.insert(m)?;
    let before = table;
    assert_eq!(
        table.request_exit(
            &ctx(&market, p(101)?, 10),
            worker(1)?,
            1,
            0,
            ExitReason::Voluntary
        ),
        Err(ARITHMETIC)
    );
    assert_eq!(table, before);
    assert_eq!(Role::decode(3), Err(NON_CANONICAL));
    assert_eq!(ExitReason::decode(0), Err(NON_CANONICAL));
    assert_eq!(RemovalReason::decode(4), Err(NON_CANONICAL));
    let mut buf = vec![0u8; TABLE_MAX_BYTES];
    let n = before.encode(&mut buf)?;
    assert_eq!(AdmissionTable::decode(&buf[..n])?, before);
    let mut trailing = buf[..n].to_vec();
    trailing.push(0);
    assert_eq!(AdmissionTable::decode(&trailing), Err(NON_CANONICAL));
    let reserved_at = TABLE_HEADER_MAX_BYTES + 33 + 3 * 9 + 8 + 1 + 8 + 2;
    let mut reserved = buf[..n].to_vec();
    reserved[reserved_at] = 1;
    assert_eq!(AdmissionTable::decode(&reserved), Err(NON_CANONICAL));
    let mut flags = buf[..n].to_vec();
    flags[reserved_at - 1] = 8 | FLAG_ADMITTED;
    assert_eq!(AdmissionTable::decode(&flags), Err(NON_CANONICAL));
    let mut u = opened(4)?;
    let tm = terms(worker(2)?, p(2)?, 5, 600)?;
    let d = u.approve(&admin(&market, 520)?, &tm)?;
    let snap = u;
    assert_eq!(
        u.admit(&ctx(&market, p(3)?, 521), &admission(worker(2)?, 5, d)),
        Err(F08_OWNER_REQUIRED)
    );
    assert_eq!(
        u.admit(&ctx(&market, p(2)?, 521), &admission(worker(2)?, 6, d)),
        Err(WRONG_EPOCH)
    );
    assert_eq!(u, snap);
    Ok(())
}

#[test]
fn a15_maximum_population_fits_state_bounds() -> TestResult {
    let mut table = opened(9)?;
    for n in 0..32u8 {
        table.insert(AdmissionMeta {
            participant: worker(n)?,
            owner: p(n + 100)?,
            admitted_epoch: None,
            last_heartbeat_epoch: None,
            last_heartbeat_height: None,
            immunity_until_epoch: 10,
            pending_exit: None,
            membership_generation: 1,
            complete_missed_opened_epochs: 0,
            membership_flags: 0,
            delegate_generation: 1,
            admission_height: None,
            approval: Some(Approval {
                digest: Digest32::new([1; 32])?,
                expiry_height: 9 * 128 + 64,
                effective_epoch: 10,
                config_version: 1,
            }),
        })?;
    }
    for n in 0..8u8 {
        let mut m = member(n, 3, 1)?;
        m.participant = evaluator(n)?;
        m.pending_exit = Some(PendingExit {
            epoch: 10,
            cause: ExitCause::OperatorDecision,
        });
        table.insert(m)?;
    }
    let mut extra = member(99, 3, 1)?;
    extra.participant = evaluator(99)?;
    assert_eq!(table.insert(extra), Err(F08_CAPACITY_EXCEEDED));
    let mut buf = vec![0u8; TABLE_MAX_BYTES];
    let n = table.encode(&mut buf)?;
    assert_eq!(
        n,
        TABLE_HEADER_MAX_BYTES + 32 * APPROVAL_META_BYTES + 8 * ADMITTED_META_MAX_BYTES
    );
    assert!(n <= TABLE_MAX_BYTES);
    assert!(n <= Section::ReputationAdmission.payload_cap());
    let bytes = persist(&table)?;
    assert!(bytes.len() <= MAX_STATE_BYTES);
    assert_eq!(restore(&bytes)?, table);
    Ok(())
}

#[test]
fn a17_bootstrap_one_worker_three_evaluators() -> TestResult {
    let market = header(1000)?;
    let mut table = AdmissionTable::new();
    admit_role(&mut table, &market, worker(1)?, p(41)?, 1000)?;
    for (n, owner) in [(1u8, 51u8), (2, 52), (3, 53)] {
        admit_role(
            &mut table,
            &market,
            evaluator(n)?,
            p(owner)?,
            1000 + u64::from(n) * 2,
        )?;
    }
    assert_eq!(table.current_epoch(), None);
    assert_eq!(
        enroll(&mut table, &market, 9, p(49)?, 1007),
        Err(F08_ADMISSION_WINDOW_FULL)
    );
    let r = roster::open_epoch(&mut table, 0, no_retention)?;
    assert_eq!((r.members, r.installed, r.expired), (4, 4, 0));
    assert!(r.health.quorum_ready);
    assert_eq!(table.current_epoch(), Some(0));
    assert_eq!(table.enrollments_this_epoch(), 0);
    for m in table.iter().filter(|m| m.admitted()) {
        assert_eq!(m.admitted_epoch, Some(0));
        assert_eq!(m.last_activity(), Some(0));
        assert_eq!(m.last_heartbeat_height, None);
    }
    Ok(())
}

#[test]
fn a18_epoch_presence_rules_and_expired_approval_purge() -> TestResult {
    let market = header(0)?;
    let mut table = AdmissionTable::new();
    let tm = terms(worker(1)?, p(41)?, 1, 60)?;
    assert_eq!(table.approve(&admin(&market, 5)?, &tm), Err(WRONG_EPOCH));
    assert!(table.is_empty());
    let staged = terms(worker(2)?, p(42)?, 0, 60)?;
    let d = table.approve(&admin(&market, 5)?, &staged)?;
    assert_eq!(
        table.admit(&ctx(&market, p(42)?, 6), &admission(worker(2)?, 1, d)),
        Err(WRONG_EPOCH)
    );
    assert_eq!(table.enrollments_this_epoch(), 0);
    assert!(table.get(worker(2)?).is_some_and(|m| m.approval.is_some()));
    roster::open_epoch(&mut table, 0, no_retention)?;
    let late = terms(worker(3)?, p(43)?, 0, 60)?;
    assert_eq!(table.approve(&admin(&market, 10)?, &late), Err(WRONG_EPOCH));
    let one = enroll(&mut table, &market, 4, p(44)?, 20)?;
    assert_eq!(one.immunity_until_epoch, 1);
    let r = roster::open_epoch(&mut table, 2, no_retention)?;
    assert_eq!((r.installed, r.expired), (1, 1));
    assert!(table.get(worker(2)?).is_none());
    let m = table.get(worker(4)?).ok_or(NOT_FOUND)?;
    assert_eq!((m.admitted_epoch, m.immunity_until_epoch), (Some(2), 2));
    assert_eq!(m.last_heartbeat_height, None);
    let hb = table.heartbeat(&ctx(&market, p(44)?, 2 * 128), worker(4)?, 1, 1, 2)?;
    assert_eq!(hb.last_heartbeat_height, Some(256));
    Ok(())
}

fn sign(consent: &EvaluatorConsent, key: &SigningKey) -> CodecResult<Vec<u8>> {
    let mut buf = [0u8; 362];
    consent.encode(&mut buf)?;
    let mut payload = buf.to_vec();
    payload.extend_from_slice(&key.sign(consent.digest()?.as_bytes()).to_bytes());
    Ok(payload)
}

/// Pending F03 grant, its stored F08 approval and the matching owner-signed consent.
fn pending_evaluator(
    market: &MarketHeader,
    owner: PrincipalId,
    key: &SigningKey,
) -> CodecResult<(EvaluatorGrant, AdmissionTable, EvaluatorConsent)> {
    let nonce = [5; 32];
    let signing_key = PublicKey32(key.verifying_key().to_bytes());
    let rubric = RubricDigest::new([6; 32])?;
    let grant = EvaluatorGrant::nominate(
        market.market_id,
        owner,
        nonce,
        GrantTerms {
            rubric,
            grant_version: Version::new(1)?,
            key_version: Version::new(1)?,
            signing_key,
            effective_epoch: 0,
            expiry_epoch_exclusive: 32,
        },
    )?;
    let mut table = AdmissionTable::new();
    let mut tm = terms(Participant::Evaluator(grant.evaluator), owner, 0, 60)?;
    tm.delegate = signing_key;
    let approval = table.approve(&admin(market, 5)?, &tm)?;
    let consent = EvaluatorConsent {
        chain: market.deployment_chain_domain,
        program: market.program_id,
        market: market.market_id,
        evaluator: grant.evaluator,
        owner,
        signing_key,
        enrollment_nonce: nonce,
        rubric,
        approval_digest: approval,
        request: RequestId::new([2; 32])?,
        grant_version: 1,
        key_version: 1,
        effective_epoch: 0,
        config_version: 1,
        expiry_height: 60,
    };
    Ok((grant, table, consent))
}

#[test]
fn a19_evaluator_consent_over_pending_grant() -> TestResult {
    let market = header(0)?;
    let owner = p(77)?;
    let k = SigningKey::from_bytes(&[3; 32]);
    let k2 = SigningKey::from_bytes(&[4; 32]);
    let (grant, mut table, consent) = pending_evaluator(&market, owner, &k)?;
    let request = consent.request;
    let nonce = consent.enrollment_nonce;
    let good = sign(&consent, &k)?;
    assert_eq!(good.len(), 426);
    assert_eq!(EvaluatorConsent::decode(&good[..362])?, consent);
    let before = table;
    let run = |table: &mut AdmissionTable,
               who: PrincipalId,
               height: u64,
               req: RequestId,
               bytes: &[u8]| {
        admit_evaluator(table, &ctx(&market, who, height), &grant, req, bytes)
    };
    assert_eq!(
        run(&mut table, p(78)?, 6, request, &good),
        Err(F08_OWNER_REQUIRED)
    );
    assert_eq!(
        run(&mut table, owner, 6, request, &sign(&consent, &k2)?),
        Err(F08_BAD_CONSENT)
    );
    assert_eq!(
        run(&mut table, owner, 6, RequestId::new([3; 32])?, &good),
        Err(F08_BAD_CONSENT)
    );
    assert_eq!(
        run(&mut table, owner, 6, request, &good[..425]),
        Err(NON_CANONICAL)
    );
    let mut altered = consent;
    altered.enrollment_nonce = [9; 32];
    assert_eq!(
        run(&mut table, owner, 6, request, &sign(&altered, &k)?),
        Err(F08_BAD_CONSENT)
    );
    let mut v2 = consent;
    v2.grant_version = 2;
    assert_eq!(
        run(&mut table, owner, 6, request, &sign(&v2, &k)?),
        Err(F08_WRONG_GENERATION)
    );
    let mut foreign = consent;
    foreign.chain = ChainDomain::new([99; 32])?;
    assert_eq!(
        run(&mut table, owner, 6, request, &sign(&foreign, &k)?),
        Err(WRONG_DOMAIN)
    );
    let mut stale = consent;
    stale.expiry_height = 6;
    assert_eq!(
        run(&mut table, owner, 6, request, &sign(&stale, &k)?),
        Err(F08_PERMIT_EXPIRED)
    );
    assert_eq!(table, before);
    assert_eq!(
        derive_evaluator(market.market_id, owner, nonce)?,
        grant.evaluator
    );
    let m = run(&mut table, owner, 6, request, &good)?;
    assert!(m.admitted());
    assert_eq!(m.approval, None);
    assert_eq!(grant.status, GrantStatus::Pending);
    assert_eq!(table.len(), 1);
    assert_eq!(roster::health(&table)?.roster_evaluators, 0);
    assert_eq!(
        run(&mut table, owner, 7, request, &good),
        Err(F08_PERMIT_CONSUMED)
    );
    let mut early = AdmissionTable::new();
    let r = roster::open_epoch(&mut early, 0, no_retention)?;
    assert!(!r.health.quorum_ready);
    Ok(())
}

#[test]
fn a20_revoke_approval_cancel_exit_and_retention() -> TestResult {
    let market = header(0)?;
    let mut table = opened(0)?;
    let tm = terms(worker(1)?, p(41)?, 1, 100)?;
    let first = table.approve(&admin(&market, 10)?, &tm)?;
    let renewed = table.approve(&admin(&market, 11)?, &terms(worker(1)?, p(41)?, 1, 120)?)?;
    assert_ne!(first, renewed);
    assert_eq!(table.len(), 1);
    assert_eq!(
        table.admit(&ctx(&market, p(41)?, 12), &admission(worker(1)?, 1, first)),
        Err(F08_BAD_CONSENT)
    );
    assert_eq!(
        table.approve(&admin(&market, 12)?, &terms(worker(1)?, p(45)?, 1, 100)?),
        Err(CONFLICT)
    );
    assert_eq!(
        table.revoke_approval(&ctx(&market, p(41)?, 12), worker(1)?),
        Err(F08_ADMINISTRATOR_APPROVAL_REQUIRED)
    );
    table.revoke_approval(&admin(&market, 12)?, worker(1)?)?;
    assert!(table.is_empty());
    assert_eq!(
        table.revoke_approval(&admin(&market, 12)?, worker(1)?),
        Err(NOT_FOUND)
    );
    enroll(&mut table, &market, 2, p(42)?, 20)?;
    assert_eq!(
        table.revoke_approval(&admin(&market, 21)?, worker(2)?),
        Err(F08_PERMIT_CONSUMED)
    );
    assert_eq!(
        table.approve(&admin(&market, 21)?, &terms(worker(2)?, p(42)?, 1, 100)?),
        Err(F08_DUPLICATE_IDENTITY)
    );
    roster::open_epoch(&mut table, 1, no_retention)?;
    let owner = ctx(&market, p(42)?, 130);
    let w = worker(2)?;
    assert_eq!(
        table.request_exit(&owner, w, 1, 3, ExitReason::Voluntary),
        Err(WRONG_EPOCH)
    );
    let exiting = table.request_exit(&owner, w, 1, 2, ExitReason::Voluntary)?;
    assert!(exiting.draining());
    assert_eq!(
        table.request_exit(&owner, w, 1, 2, ExitReason::Voluntary)?,
        exiting
    );
    assert_eq!(
        table.request_exit(&owner, w, 1, 2, ExitReason::Retire),
        Err(F08_IDEMPOTENCY_CONFLICT)
    );
    assert_eq!(
        table.cancel_exit(&ctx(&market, p(43)?, 131), w, 1),
        Err(F08_OWNER_REQUIRED)
    );
    let cancelled = table.cancel_exit(&owner, w, 1)?;
    assert_eq!(cancelled.pending_exit, None);
    assert!(cancelled.draining());
    assert_eq!(
        (cancelled.admitted_epoch, cancelled.immunity_until_epoch),
        (exiting.admitted_epoch, exiting.immunity_until_epoch)
    );
    assert_eq!(table.cancel_exit(&owner, w, 1), Err(WRONG_PHASE));
    roster::open_epoch(&mut table, 2, no_retention)?;
    assert!(table.get(w).is_some_and(|m| !m.draining()));
    let removal = table.administrative_remove(&admin(&market, 261)?, w, 1, RemovalReason::Terms)?;
    assert!(!removal.revoked());
    let later = ctx(&market, p(42)?, 262);
    assert_eq!(table.cancel_exit(&later, w, 1), Err(WRONG_PHASE));
    assert_eq!(
        table.request_exit(&later, w, 1, 3, ExitReason::Voluntary),
        Err(WRONG_PHASE)
    );
    let held = table;
    assert_eq!(
        roster::open_epoch(&mut table, 3, |m: &AdmissionMeta| m.participant == w),
        Err(F08_RETENTION_BLOCKED)
    );
    assert_eq!(table, held);
    let r = roster::open_epoch(&mut table, 3, no_retention)?;
    assert_eq!((r.removed, r.members), (1, 0));
    assert_eq!(
        table.request_exit(&later, w, 1, 4, ExitReason::Voluntary),
        Err(NOT_FOUND)
    );
    Ok(())
}

#[test]
fn a21_paused_market_expiry_and_origin_bounds() -> TestResult {
    let mut market = header(1000)?;
    let mut table = opened(0)?;
    assert_eq!(
        table.required_effective_epoch(&admin(&market, 999)?),
        Err(WRONG_EPOCH)
    );
    let w = worker(1)?;
    let work_end = 1000 + 128 + 64;
    for expiry in [1010, work_end + 1] {
        assert_eq!(
            table.approve(&admin(&market, 1010)?, &terms(w, p(41)?, 1, expiry)?),
            Err(F08_PERMIT_EXPIRED)
        );
    }
    let d = table.approve(&admin(&market, 1010)?, &terms(w, p(41)?, 1, 1030)?)?;
    assert_eq!(
        table.admit(&ctx(&market, p(41)?, 1030), &admission(w, 1, d)),
        Err(F08_PERMIT_EXPIRED)
    );
    market.lifecycle = 3;
    assert_eq!(
        table.admit(&ctx(&market, p(41)?, 1020), &admission(w, 1, d)),
        Err(F08_MARKET_PAUSED)
    );
    assert_eq!(
        table.approve(&admin(&market, 1020)?, &terms(worker(2)?, p(42)?, 1, 1100)?),
        Err(F08_MARKET_PAUSED)
    );
    market.lifecycle = 4;
    assert_eq!(
        table.admit(&ctx(&market, p(41)?, 1020), &admission(w, 1, d)),
        Err(WRONG_PHASE)
    );
    market.lifecycle = 2;
    let m = table.admit(&ctx(&market, p(41)?, 1020), &admission(w, 1, d))?;
    assert_eq!(m.admission_height, Some(1020));
    Ok(())
}
