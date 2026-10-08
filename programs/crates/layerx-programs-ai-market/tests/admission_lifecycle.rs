use ed25519_dalek::{Signer, SigningKey};
use layerx_programs_ai_market::{
    admission::*,
    codec::{derive_evaluator, derive_market},
    errors::*,
    evaluators::model::{EvaluatorGrant, GrantStatus},
    registry::*,
    roster,
    state::{self, ActorSlot, Control, ReplayTable, Section, SharedState},
    types::*,
    MAX_STATE_BYTES,
};

const OWNER: [u8; 32] = [12; 32];

fn header(origin: u64) -> MarketHeader {
    let chain = ChainDomain::new([10; 32]).unwrap();
    let program = ProgramId::new([11; 32]).unwrap();
    let asset = AssetId::new([13; 32]).unwrap();
    MarketHeader {
        format_version: 1,
        market_id: derive_market(chain, program).unwrap(),
        deployment_chain_domain: chain,
        program_id: program,
        owner_principal: PrincipalId::new(OWNER).unwrap(),
        funding_asset: asset,
        rewards_account: derive_rewards_account(program, asset).unwrap(),
        refund_recipient_account: AccountId::new([14; 32]).unwrap(),
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
        metadata_digest: MetadataDigest::new([16; 32]).unwrap(),
        closing_request_digest: [0; 32],
        reserved: [0; 8],
    }
}
fn p(b: u8) -> PrincipalId {
    PrincipalId::new([b; 32]).unwrap()
}
fn id(n: u8, role: u8) -> [u8; 32] {
    let mut v = [n; 32];
    v[0] = role;
    v[31] = n.wrapping_add(1);
    v
}
fn ctx(h: &MarketHeader, who: PrincipalId, height: u64) -> AdmissionContext<'_> {
    AdmissionContext {
        market: h,
        invoking_principal: who,
        height,
    }
}
fn terms(
    role: u8,
    participant: [u8; 32],
    owner: PrincipalId,
    effective: u64,
    expiry: u64,
) -> ApprovalTerms {
    ApprovalTerms {
        role,
        participant,
        owner,
        enrollment_nonce_commitment: [7; 32],
        delegate: PublicKey32([8; 32]),
        delegate_generation: 1,
        identity_commitment: [9; 32],
        effective_epoch: effective,
        config_version: 1,
        request: RequestId::new([participant[31]; 32]).unwrap(),
        expiry_height: expiry,
    }
}
fn no_retention(_: &AdmissionMeta) -> bool {
    false
}

/// Approve and admit a worker at `height`, both bound to the required effective epoch.
fn enroll(
    t: &mut AdmissionTable,
    h: &MarketHeader,
    n: u8,
    owner: PrincipalId,
    height: u64,
) -> CodecResult<AdmissionMeta> {
    let effective = t.required_effective_epoch(&ctx(h, owner, height))?;
    let work_end = h.origin_height + effective * 128 + 64;
    let tm = terms(
        ROLE_WORKER,
        id(n, 1),
        owner,
        effective,
        work_end.max(height + 1),
    );
    let d = t.approve(
        &ctx(h, p(OWNER[0]), height),
        &tm,
        t.role_count(ROLE_WORKER) + 1,
    )?;
    t.admit(
        &ctx(h, owner, height),
        ROLE_WORKER,
        id(n, 1),
        1,
        effective,
        1,
        d.bytes(),
    )
}

fn opened(_h: &MarketHeader, epoch: u64) -> AdmissionTable {
    let mut t = AdmissionTable::new();
    roster::open_epoch(&mut t, epoch, &no_retention).unwrap();
    t
}

fn persist(t: &AdmissionTable) -> Vec<u8> {
    let mut section = vec![0u8; TABLE_MAX_BYTES];
    let n = t.encode(&mut section).unwrap();
    let mut replay = ReplayTable::new();
    replay
        .bind(ActorSlot::OWNER, p(OWNER[0]), Version::new(1).unwrap())
        .unwrap();
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
    let len = state::encode_shared_state(&shared, &mut out, &mut scratch).unwrap();
    out.truncate(len);
    out
}
fn restore(bytes: &[u8]) -> AdmissionTable {
    let s = state::decode_shared_state(bytes).unwrap();
    AdmissionTable::decode(s.section(Section::ReputationAdmission).unwrap()).unwrap()
}
fn frozen(t: &AdmissionTable) -> usize {
    t.iter().filter(|m| m.admitted_epoch.is_some()).count()
}

#[test]
fn a01_enroll_stages_next_epoch_then_rollover_includes_once() {
    let h = header(0);
    let mut t = opened(&h, 4);
    let m = enroll(&mut t, &h, 1, p(40), 520).unwrap();
    assert_eq!(m.effective_epoch, 5);
    assert_eq!(t.role_count(ROLE_WORKER), 1);
    assert_eq!(frozen(&t), 0);
    assert_eq!(m.admitted_epoch, None);
    assert_eq!(m.last_heartbeat_epoch, None);
    let r = roster::open_epoch(&mut t, 5, &no_retention).unwrap();
    assert_eq!((r.installed, r.members), (1, 1));
    assert_eq!(t.get(id(1, 1)).unwrap().admitted_epoch, Some(5));
    assert_eq!(
        roster::open_epoch(&mut t, 5, &no_retention),
        Err(WRONG_EPOCH)
    );
}

#[test]
fn a02_capacity_refuses_without_consumption_until_exit_and_rollover() {
    let h = header(0);
    let mut t = opened(&h, 0);
    let mut height = 10;
    for n in 0..32u8 {
        if n % 4 == 0 && n > 0 {
            let next = t.current_epoch + 1;
            roster::open_epoch(&mut t, next, &no_retention).unwrap();
            height = t.current_epoch * 128 + 10;
        }
        enroll(&mut t, &h, n, p(100 + n), height).unwrap();
    }
    let next = t.current_epoch + 1;
    roster::open_epoch(&mut t, next, &no_retention).unwrap();
    height = t.current_epoch * 128 + 10;
    let before = t;
    let e = t
        .required_effective_epoch(&ctx(&h, p(200), height))
        .unwrap();
    let tm = terms(ROLE_WORKER, id(200, 1), p(200), e, height + 20);
    assert_eq!(
        t.approve(&ctx(&h, p(OWNER[0]), height), &tm, 33),
        Err(F08_CAPACITY_EXCEEDED)
    );
    assert_eq!(t, before);
    let gone = id(3, 1);
    t.request_exit(
        &ctx(&h, p(103), height),
        gone,
        1,
        t.current_epoch + 1,
        EXIT_VOLUNTARY,
    )
    .unwrap();
    let frozen_before = frozen(&t);
    let new = enroll(&mut t, &h, 200, p(200), height);
    assert_eq!(new, Err(F08_CAPACITY_EXCEEDED));
    assert_eq!(frozen(&t), frozen_before);
    let next = t.current_epoch + 1;
    roster::open_epoch(&mut t, next, &no_retention).unwrap();
    height = t.current_epoch * 128 + 10;
    let staged = enroll(&mut t, &h, 200, p(200), height).unwrap();
    assert_eq!(staged.effective_epoch, t.current_epoch + 1);
    assert_eq!(staged.admitted_epoch, None);
}

#[test]
fn a03_per_owner_limits() {
    let h = header(0);
    let mut t = opened(&h, 0);
    enroll(&mut t, &h, 1, p(50), 10).unwrap();
    enroll(&mut t, &h, 2, p(50), 30).unwrap();
    roster::open_epoch(&mut t, 1, &no_retention).unwrap();
    assert_eq!(
        enroll(&mut t, &h, 3, p(50), 140),
        Err(F08_OWNER_CAPACITY_EXCEEDED)
    );
    let ev = |t: &mut AdmissionTable, n: u8, owner: u8, height: u64| {
        let e = t
            .required_effective_epoch(&ctx(&h, p(owner), height))
            .unwrap();
        let tm = terms(ROLE_EVALUATOR, id(n, 2), p(owner), e, height + 20);
        let d = t.approve(&ctx(&h, p(OWNER[0]), height), &tm, 1)?;
        t.admit(
            &ctx(&h, p(owner), height),
            ROLE_EVALUATOR,
            id(n, 2),
            1,
            e,
            1,
            d.bytes(),
        )
    };
    ev(&mut t, 10, 60, 160).unwrap();
    assert_eq!(ev(&mut t, 11, 60, 180), Err(F08_OWNER_CAPACITY_EXCEEDED));
    assert_eq!(ev(&mut t, 12, 50, 180), Err(ROLE_CONFLICT));
    ev(&mut t, 13, 61, 180).unwrap();
}

#[test]
fn a04_owner_cooldown_and_epoch_window() {
    let h = header(0);
    let mut t = opened(&h, 4);
    enroll(&mut t, &h, 1, p(70), 600).unwrap();
    assert_eq!(enroll(&mut t, &h, 2, p(70), 615), Err(F08_RATE_LIMITED));
    assert_eq!(t.enrollments_this_epoch, 1);
    enroll(&mut t, &h, 2, p(70), 616).unwrap();
    // bad consent does not consume a window slot
    let tm = terms(ROLE_WORKER, id(3, 1), p(71), 5, 640);
    t.approve(&ctx(&h, p(OWNER[0]), 617), &tm, 3).unwrap();
    assert_eq!(
        t.admit(
            &ctx(&h, p(71), 617),
            ROLE_WORKER,
            id(3, 1),
            1,
            5,
            1,
            [1; 32]
        ),
        Err(F08_BAD_CONSENT)
    );
    assert_eq!(t.enrollments_this_epoch, 2);
    enroll(&mut t, &h, 3, p(71), 618).unwrap();
    enroll(&mut t, &h, 4, p(72), 619).unwrap();
    assert_eq!(
        enroll(&mut t, &h, 5, p(73), 620),
        Err(F08_ADMISSION_WINDOW_FULL)
    );
    assert_eq!(t.enrollments_this_epoch, 4);
}

#[test]
fn a05_inactivity_needs_two_complete_opened_absences() {
    let h = header(0);
    let mut t = opened(&h, 6);
    enroll(&mut t, &h, 1, p(80), 6 * 128 + 5).unwrap();
    roster::open_epoch(&mut t, 7, &no_retention).unwrap();
    let mut skipped = t;
    roster::open_epoch(&mut t, 8, &no_retention).unwrap();
    assert_eq!(
        roster::prune_candidate(&t, ROLE_WORKER, 8),
        Err(F08_NO_PRUNABLE_MEMBER)
    );
    roster::open_epoch(&mut t, 9, &no_retention).unwrap();
    assert_eq!(
        roster::prune_candidate(&t, ROLE_WORKER, 9),
        Err(F08_NO_PRUNABLE_MEMBER)
    );
    roster::open_epoch(&mut t, 10, &no_retention).unwrap();
    assert_eq!(
        roster::prune_candidate(&t, ROLE_WORKER, 10)
            .unwrap()
            .participant,
        id(1, 1)
    );
    let mut live = t;
    live.heartbeat(
        &ctx(&h, p(80), 10 * 128 + 1),
        id(1, 1),
        ROLE_WORKER,
        1,
        1,
        10,
    )
    .unwrap();
    assert_eq!(
        roster::prune_candidate(&live, ROLE_WORKER, 10),
        Err(F08_NO_PRUNABLE_MEMBER)
    );
    roster::open_epoch(&mut skipped, 10, &no_retention).unwrap();
    assert_eq!(
        skipped.get(id(1, 1)).unwrap().complete_missed_opened_epochs,
        0
    );
    assert_eq!(
        roster::prune_candidate(&skipped, ROLE_WORKER, 10),
        Err(F08_NO_PRUNABLE_MEMBER)
    );
    let pruned = t.prune_inactive(ROLE_WORKER, id(1, 1), 3, 3).unwrap();
    assert!(pruned.draining());
    assert_eq!(pruned.pending_exit_epoch, Some(11));
    assert_eq!(
        t.prune_inactive(ROLE_WORKER, id(1, 1), 3, 4),
        Err(F08_STALE_STATE)
    );
}

fn member(n: u8, last: u64, admitted: u64) -> AdmissionMeta {
    AdmissionMeta {
        participant: id(n, 1),
        owner: p(n + 100),
        role: ROLE_WORKER,
        admitted_epoch: Some(admitted),
        last_heartbeat_epoch: Some(last),
        last_heartbeat_height: Some(last * 128),
        immunity_until_epoch: admitted,
        pending_exit_epoch: None,
        membership_generation: 1,
        complete_missed_opened_epochs: 2,
        membership_flags: FLAG_ADMITTED,
        effective_epoch: admitted,
        delegate_generation: 1,
        admission_height: Some(admitted * 128),
        approval: None,
    }
}

#[test]
fn a06_deterministic_longest_idle_then_oldest_then_id() {
    let mut t = AdmissionTable::new();
    t.insert(member(30, 4, 1)).unwrap();
    t.insert(member(20, 3, 2)).unwrap();
    t.insert(member(10, 3, 1)).unwrap();
    assert_eq!(
        roster::prune_candidate(&t, ROLE_WORKER, 7)
            .unwrap()
            .participant,
        id(10, 1)
    );
    let mut u = AdmissionTable::new();
    u.epoch_present = true;
    u.current_epoch = 7;
    u.insert(member(9, 3, 1)).unwrap();
    u.insert(member(5, 3, 1)).unwrap();
    assert_eq!(
        roster::prune_candidate(&u, ROLE_WORKER, 7)
            .unwrap()
            .participant,
        id(5, 1)
    );
    assert_eq!(
        u.prune_inactive(ROLE_WORKER, id(9, 1), 1, 1),
        Err(F08_CANDIDATE_CHANGED)
    );
}

#[test]
fn a07_no_fallback_and_security_revocation_during_immunity() {
    let h = header(0);
    let mut t = opened(&h, 0);
    enroll(&mut t, &h, 1, p(OWNER[0] + 1), 10).unwrap();
    roster::open_epoch(&mut t, 1, &no_retention).unwrap();
    assert_eq!(
        t.prune_inactive(ROLE_WORKER, id(1, 1), 1, 1),
        Err(F08_NO_PRUNABLE_MEMBER)
    );
    let m = t
        .administrative_remove(&ctx(&h, p(OWNER[0]), 130), id(1, 1), 1, REMOVE_SECURITY)
        .unwrap();
    assert!(m.revoked());
    assert_eq!(
        t.heartbeat(&ctx(&h, p(13), 150), id(1, 1), ROLE_WORKER, 1, 1, 1),
        Err(F08_DELEGATE_REVOKED)
    );
    assert_eq!(
        t.administrative_remove(&ctx(&h, p(13), 130), id(1, 1), 1, REMOVE_TERMS),
        Err(F08_ADMINISTRATOR_APPROVAL_REQUIRED)
    );
}

#[test]
fn a08_heartbeat_distance_and_bindings() {
    let h = header(0);
    let mut t = opened(&h, 4);
    enroll(&mut t, &h, 1, p(90), 520).unwrap();
    roster::open_epoch(&mut t, 5, &no_retention).unwrap();
    let w = id(1, 1);
    t.heartbeat(&ctx(&h, p(90), 700), w, ROLE_WORKER, 1, 1, 5)
        .unwrap();
    let before = t;
    assert_eq!(
        t.heartbeat(&ctx(&h, p(90), 715), w, ROLE_WORKER, 1, 1, 5),
        Err(F08_RATE_LIMITED)
    );
    assert_eq!(
        t.heartbeat(&ctx(&h, p(90), 716), w, ROLE_WORKER, 1, 1, 6),
        Err(WRONG_EPOCH)
    );
    assert_eq!(
        t.heartbeat(&ctx(&h, p(90), 716), w, ROLE_WORKER, 2, 1, 5),
        Err(F08_WRONG_GENERATION)
    );
    assert_eq!(t, before);
    let m = t
        .heartbeat(&ctx(&h, p(90), 716), w, ROLE_WORKER, 1, 1, 5)
        .unwrap();
    assert_eq!(m.last_heartbeat_height, Some(716));
    assert_eq!(m.membership_generation, 1);
}

#[test]
fn a09_last_vacancy_one_winner_and_restart_same_root() {
    let h = header(0);
    let mut t = opened(&h, 0);
    for n in 0..31u8 {
        let mut m = member(n, 0, 0);
        m.complete_missed_opened_epochs = 0;
        t.insert(m).unwrap();
    }
    let a = terms(ROLE_WORKER, id(200, 1), p(201), 1, 60);
    let b = terms(ROLE_WORKER, id(210, 1), p(211), 1, 60);
    let da = t.approve(&ctx(&h, p(OWNER[0]), 20), &a, 32).unwrap();
    assert_eq!(
        t.approve(&ctx(&h, p(OWNER[0]), 20), &b, 33),
        Err(F08_CAPACITY_EXCEEDED)
    );
    t.admit(
        &ctx(&h, p(201), 21),
        ROLE_WORKER,
        id(200, 1),
        1,
        1,
        1,
        da.bytes(),
    )
    .unwrap();
    assert_eq!(t.role_count(ROLE_WORKER), 32);
    assert!(t.get(id(210, 1)).is_none());
    let bytes = persist(&t);
    let back = restore(&bytes);
    assert_eq!(back, t);
    assert_eq!(
        roster::roster_digest(&back, 0).unwrap(),
        roster::roster_digest(&t, 0).unwrap()
    );
}

#[test]
fn a10_rotation_keeps_history_and_old_delegate_refused() {
    let h = header(0);
    let mut t = opened(&h, 0);
    enroll(&mut t, &h, 1, p(95), 10).unwrap();
    roster::open_epoch(&mut t, 1, &no_retention).unwrap();
    t.heartbeat(&ctx(&h, p(95), 130), id(1, 1), ROLE_WORKER, 1, 1, 1)
        .unwrap();
    let mut m = t.get(id(1, 1)).unwrap();
    let history = (
        m.admitted_epoch,
        m.immunity_until_epoch,
        m.last_heartbeat_epoch,
        m.owner,
    );
    m.delegate_generation = 2;
    t.replace(m).unwrap();
    assert_eq!(
        t.heartbeat(&ctx(&h, p(95), 150), id(1, 1), ROLE_WORKER, 1, 1, 1),
        Err(F08_WRONG_GENERATION)
    );
    let m = t
        .heartbeat(&ctx(&h, p(95), 150), id(1, 1), ROLE_WORKER, 1, 2, 1)
        .unwrap();
    assert_eq!(
        (m.admitted_epoch, m.immunity_until_epoch, m.owner),
        (history.0, history.1, history.3)
    );
}

#[test]
fn a11_quorum_health_drops_on_revocation() {
    let h = header(0);
    let mut t = AdmissionTable::new();
    for (n, owner) in [(1u8, 61u8), (2, 62), (3, 63)] {
        let tm = terms(ROLE_EVALUATOR, id(n, 2), p(owner), 0, 64);
        let d = t
            .approve(&ctx(&h, p(OWNER[0]), u64::from(n) * 20), &tm, 1)
            .unwrap();
        t.admit(
            &ctx(&h, p(owner), u64::from(n) * 20),
            ROLE_EVALUATOR,
            id(n, 2),
            1,
            0,
            1,
            d.bytes(),
        )
        .unwrap();
    }
    let r = roster::open_epoch(&mut t, 0, &no_retention).unwrap();
    assert!(r.health.quorum_ready);
    t.administrative_remove(&ctx(&h, p(OWNER[0]), 70), id(1, 2), 1, REMOVE_SECURITY)
        .unwrap();
    let health = roster::health(&t);
    assert_eq!(
        (health.roster_evaluators, health.eligible_evaluators),
        (3, 2)
    );
    assert_eq!(roster::require_quorum(&t), Err(F08_QUORUM_UNAVAILABLE));
    let tm = terms(ROLE_EVALUATOR, id(4, 2), p(64), 1, 100);
    let d = t.approve(&ctx(&h, p(OWNER[0]), 80), &tm, 4).unwrap();
    let staged = t
        .admit(
            &ctx(&h, p(64), 80),
            ROLE_EVALUATOR,
            id(4, 2),
            1,
            1,
            1,
            d.bytes(),
        )
        .unwrap();
    assert_eq!(staged.admitted_epoch, None);
    assert_eq!(roster::health(&t).roster_evaluators, 3);
}

#[test]
fn a14_overflow_and_noncanonical_refuse_before_mutation() {
    let h = header(0);
    let mut t = AdmissionTable::new();
    t.epoch_present = true;
    t.current_epoch = u64::MAX;
    let mut m = member(1, 0, 0);
    m.complete_missed_opened_epochs = 0;
    t.insert(m).unwrap();
    let before = t;
    assert_eq!(
        t.request_exit(&ctx(&h, p(101), 10), id(1, 1), 1, 0, EXIT_VOLUNTARY),
        Err(ARITHMETIC)
    );
    assert_eq!(t, before);
    assert_eq!(check_role(3), Err(NON_CANONICAL));
    let mut buf = vec![0u8; TABLE_MAX_BYTES];
    let n = before.encode(&mut buf).unwrap();
    let mut trailing = buf[..n].to_vec();
    trailing.push(0);
    assert!(AdmissionTable::decode(&trailing).is_err());
    let mut reserved = buf[..n].to_vec();
    reserved[13] = 1;
    assert_eq!(AdmissionTable::decode(&reserved), Err(NON_CANONICAL));
    let mut u = opened(&h, 4);
    let tm = terms(ROLE_WORKER, id(2, 1), p(2), 5, 600);
    let d = u.approve(&ctx(&h, p(OWNER[0]), 520), &tm, 1).unwrap();
    let snap = u;
    assert_eq!(
        u.admit(
            &ctx(&h, p(3), 521),
            ROLE_WORKER,
            id(2, 1),
            1,
            5,
            1,
            d.bytes()
        ),
        Err(F08_OWNER_REQUIRED)
    );
    assert_eq!(
        u.admit(
            &ctx(&h, p(2), 521),
            ROLE_WORKER,
            id(2, 1),
            1,
            6,
            1,
            d.bytes()
        ),
        Err(WRONG_EPOCH)
    );
    assert_eq!(u, snap);
}

#[test]
fn a15_maximum_population_fits_state_bounds() {
    let mut t = AdmissionTable::new();
    t.epoch_present = true;
    t.current_epoch = 9;
    for n in 0..32u8 {
        let mut m = member(n, 3, 1);
        m.pending_exit_epoch = Some(10);
        m.approval = Some(Approval {
            digest: [1; 32],
            expiry_height: 9,
            effective_epoch: 9,
            config_version: 1,
        });
        t.insert(m).unwrap();
    }
    for n in 0..8u8 {
        let mut m = member(n, 3, 1);
        m.participant = id(n, 2);
        m.role = ROLE_EVALUATOR;
        t.insert(m).unwrap();
    }
    let mut extra = member(99, 3, 1);
    extra.role = ROLE_EVALUATOR;
    assert_eq!(t.insert(extra), Err(F08_CAPACITY_EXCEEDED));
    let mut buf = vec![0u8; TABLE_MAX_BYTES];
    let n = t.encode(&mut buf).unwrap();
    assert!(n <= Section::ReputationAdmission.payload_cap());
    let bytes = persist(&t);
    assert!(bytes.len() <= MAX_STATE_BYTES);
    assert_eq!(restore(&bytes), t);
}

#[test]
fn a17_bootstrap_one_worker_three_evaluators() {
    let h = header(1000);
    let mut t = AdmissionTable::new();
    let mut height = 1000;
    let w = terms(ROLE_WORKER, id(1, 1), p(41), 0, 1050);
    let d = t.approve(&ctx(&h, p(OWNER[0]), height), &w, 1).unwrap();
    t.admit(
        &ctx(&h, p(41), height),
        ROLE_WORKER,
        id(1, 1),
        1,
        0,
        1,
        d.bytes(),
    )
    .unwrap();
    for (n, owner) in [(1u8, 51u8), (2, 52), (3, 53)] {
        height += 2;
        let tm = terms(ROLE_EVALUATOR, id(n, 2), p(owner), 0, 1050);
        let d = t.approve(&ctx(&h, p(OWNER[0]), height), &tm, 1).unwrap();
        t.admit(
            &ctx(&h, p(owner), height),
            ROLE_EVALUATOR,
            id(n, 2),
            1,
            0,
            1,
            d.bytes(),
        )
        .unwrap();
    }
    assert!(!t.epoch_present);
    assert_eq!(
        enroll(&mut t, &h, 9, p(49), 1007),
        Err(F08_ADMISSION_WINDOW_FULL)
    );
    let r = roster::open_epoch(&mut t, 0, &no_retention).unwrap();
    assert_eq!((r.members, r.installed), (4, 4));
    assert!(r.health.quorum_ready);
    assert!(t.epoch_present);
    assert_eq!(t.enrollments_this_epoch, 0);
    for m in t.iter().filter(|m| m.admitted()) {
        assert_eq!(m.admitted_epoch, Some(0));
        assert_eq!(m.last_activity(), Some(0));
        assert_eq!(m.last_heartbeat_height, None);
    }
}

#[test]
fn a18_epoch_presence_rules() {
    let h = header(0);
    let mut t = AdmissionTable::new();
    let tm = terms(ROLE_WORKER, id(1, 1), p(41), 1, 60);
    assert_eq!(
        t.approve(&ctx(&h, p(OWNER[0]), 5), &tm, 1),
        Err(WRONG_EPOCH)
    );
    assert!(t.is_empty());
    let staged = terms(ROLE_WORKER, id(2, 1), p(42), 0, 60);
    let d = t.approve(&ctx(&h, p(OWNER[0]), 5), &staged, 1).unwrap();
    assert_eq!(
        t.admit(
            &ctx(&h, p(42), 6),
            ROLE_WORKER,
            id(2, 1),
            1,
            1,
            1,
            d.bytes()
        ),
        Err(WRONG_EPOCH)
    );
    assert_eq!(t.enrollments_this_epoch, 0);
    assert!(t.get(id(2, 1)).unwrap().approval.is_some());
    roster::open_epoch(&mut t, 0, &no_retention).unwrap();
    let late = terms(ROLE_WORKER, id(3, 1), p(43), 0, 60);
    assert_eq!(
        t.approve(&ctx(&h, p(OWNER[0]), 10), &late, 1),
        Err(WRONG_EPOCH)
    );
    let one = enroll(&mut t, &h, 4, p(44), 20).unwrap();
    assert_eq!(one.effective_epoch, 1);
    roster::open_epoch(&mut t, 2, &no_retention).unwrap();
    let m = t.get(id(4, 1)).unwrap();
    assert_eq!((m.admitted_epoch, m.immunity_until_epoch), (Some(2), 2));
    assert_eq!(m.last_heartbeat_height, None);
    let hb = t
        .heartbeat(&ctx(&h, p(44), 2 * 128), id(4, 1), ROLE_WORKER, 1, 1, 2)
        .unwrap();
    assert_eq!(hb.last_heartbeat_height, Some(256));
}

#[test]
fn a19_evaluator_consent_over_pending_grant() {
    let h = header(0);
    let owner = p(77);
    let nonce = [5; 32];
    let k = SigningKey::from_bytes(&[3; 32]);
    let k2 = SigningKey::from_bytes(&[4; 32]);
    let key = PublicKey32(k.verifying_key().to_bytes());
    let rubric = RubricDigest::new([6; 32]).unwrap();
    let grant = EvaluatorGrant::nominate(
        h.market_id,
        owner,
        nonce,
        rubric,
        Version::new(1).unwrap(),
        Version::new(1).unwrap(),
        key,
        0,
        32,
    )
    .unwrap();
    let ev = grant.evaluator.bytes();
    let mut t = AdmissionTable::new();
    let mut tm = terms(ROLE_EVALUATOR, ev, owner, 0, 60);
    tm.delegate = key;
    let approval = t.approve(&ctx(&h, p(OWNER[0]), 5), &tm, 1).unwrap();
    let consent = EvaluatorConsent {
        chain: h.deployment_chain_domain,
        program: h.program_id,
        market: h.market_id,
        evaluator: grant.evaluator,
        owner,
        signing_key: key,
        enrollment_nonce: nonce,
        rubric,
        approval_digest: approval.bytes(),
        request: RequestId::new([2; 32]).unwrap(),
        grant_version: 1,
        key_version: 1,
        effective_epoch: 0,
        config_version: 1,
        expiry_height: 60,
    };
    let sign = |c: &EvaluatorConsent, s: &SigningKey| {
        let mut buf = [0u8; 362];
        c.encode(&mut buf).unwrap();
        let mut payload = buf.to_vec();
        payload.extend_from_slice(&s.sign(c.digest().unwrap().as_bytes()).to_bytes());
        payload
    };
    let good = sign(&consent, &k);
    assert_eq!(good.len(), 426);
    let before = t;
    assert_eq!(
        admit_evaluator(&mut t, &ctx(&h, p(78), 6), &grant, &good),
        Err(F08_OWNER_REQUIRED)
    );
    assert_eq!(
        admit_evaluator(&mut t, &ctx(&h, owner, 6), &grant, &sign(&consent, &k2)),
        Err(F08_BAD_CONSENT)
    );
    let mut altered = consent;
    altered.enrollment_nonce = [9; 32];
    assert_eq!(
        admit_evaluator(&mut t, &ctx(&h, owner, 6), &grant, &sign(&altered, &k)),
        Err(F08_BAD_CONSENT)
    );
    let mut v2 = consent;
    v2.grant_version = 2;
    assert_eq!(
        admit_evaluator(&mut t, &ctx(&h, owner, 6), &grant, &sign(&v2, &k)),
        Err(F08_WRONG_GENERATION)
    );
    assert_eq!(t, before);
    assert_eq!(
        derive_evaluator(h.market_id, owner, nonce).unwrap(),
        grant.evaluator
    );
    let m = admit_evaluator(&mut t, &ctx(&h, owner, 6), &grant, &good).unwrap();
    assert!(m.admitted());
    assert_eq!(m.approval, None);
    assert_eq!(grant.status, GrantStatus::Pending);
    assert_eq!(t.len(), 1);
    assert_eq!(roster::health(&t).roster_evaluators, 0);
    let mut early = AdmissionTable::new();
    let r = roster::open_epoch(&mut early, 0, &no_retention).unwrap();
    assert!(!r.health.quorum_ready);
}
