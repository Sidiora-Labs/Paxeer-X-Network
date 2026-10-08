use layerx_programs_ai_market::{
    codec::{self, decode_envelope, derive_market, derive_worker, encode_envelope, Envelope},
    dispatch,
    errors::*,
    policy::*,
    queries::*,
    registry::*,
    registry_ops::{self, Outcome},
    state::{self, Control, ReplayTable, Section, SharedState},
    types::*,
    workers::*,
    MAX_EVENT_BYTES, MAX_RESULT_BYTES, MAX_STATE_BYTES,
};

const CHAIN: [u8; 32] = [10; 32];
const PROGRAM: [u8; 32] = [11; 32];
const OWNER: [u8; 32] = [12; 32];
const ASSET: [u8; 32] = [13; 32];
const REFUND: [u8; 32] = [14; 32];

fn chain() -> ChainDomain {
    ChainDomain::new(CHAIN).unwrap()
}
fn program() -> ProgramId {
    ProgramId::new(PROGRAM).unwrap()
}
fn p(b: [u8; 32]) -> PrincipalId {
    PrincipalId::new(b).unwrap()
}
fn d(b: u8) -> Digest32 {
    Digest32::new([b; 32]).unwrap()
}

fn policy() -> TaskPolicyV1 {
    TaskPolicyV1::bounded_default(
        1,
        1,
        PolicyCommitments {
            model_artifact_digest: d(1),
            dataset_artifact_digest: [2; 32],
            benchmark_suite_digest: d(3),
            rubric_digest: RubricDigest::new([4; 32]).unwrap(),
            task_schema_digest: d(5),
            result_schema_digest: d(6),
            service_terms_digest: d(7),
        },
        100,
        1,
    )
    .unwrap()
}

/// Real F01 CREATE through registry_ops::apply.
fn created_state() -> Vec<u8> {
    let mut pol = vec![0; TASK_POLICY_BYTES];
    policy().encode(&mut pol).unwrap();
    let rewards = derive_rewards_account(program(), AssetId::new(ASSET).unwrap()).unwrap();
    let mut payload = OWNER.to_vec();
    payload.extend_from_slice(&ASSET);
    payload.extend_from_slice(rewards.as_bytes());
    payload.extend_from_slice(&REFUND);
    payload.push(0);
    payload.extend_from_slice(&pol);
    payload.extend_from_slice(&[16; 32]);
    let e = Envelope {
        operation: dispatch::CREATE,
        chain: chain(),
        program: program(),
        market: derive_market(chain(), program()).unwrap(),
        actor: p(OWNER),
        epoch: 0,
        config: 1,
        roster: Presence::Absent,
        sequence: 1,
        expiry: 1_000_000,
        request: RequestId::new([1; 32]).unwrap(),
        payload: &payload,
        authentication: Authentication::Native,
    };
    let mut env = vec![0; 16_384];
    let n = encode_envelope(&e, &mut env).unwrap();
    let env = decode_envelope(&env[..n]).unwrap();
    let ctx = registry_ops::CallContext {
        chain: chain(),
        program: program(),
        principal: p(OWNER),
        height: 1000,
    };
    let mut section = vec![0; F01_SECTION_CAP];
    let mut event = vec![0; MAX_EVENT_BYTES];
    match registry_ops::apply(&ctx, None, &env, &mut section, &mut event).unwrap() {
        Outcome::Applied { state, .. } => encode(&state),
        Outcome::AlreadyApplied(_) => panic!("fresh create"),
    }
}

fn encode(s: &SharedState<'_>) -> Vec<u8> {
    let mut out = vec![0; s.encoded_len().unwrap()];
    let mut scratch = vec![0; Section::Control.payload_cap()];
    let n = state::encode_shared_state(s, &mut out, &mut scratch).unwrap();
    assert_eq!(n, out.len());
    out
}

fn worker(slot: u8, state: WorkerState) -> WorkerCurrent {
    let market = derive_market(chain(), program()).unwrap();
    WorkerCurrent {
        worker: derive_worker(market, p([20; 32]), [slot + 1; 32]).unwrap(),
        owner: p([20; 32]),
        delegate: PublicKey32([slot + 50; 32]),
        metadata: MetadataDigest::new([30; 32]).unwrap(),
        generation: 2,
        key_version: 2,
        metadata_revision: 4,
        valid_from: 200,
        expiry: 400,
        revocation_sequence: 0,
        effective_epoch: 1,
        last_sequence: 0,
        last_request_id: [0; 32],
        last_request_digest: [0; 32],
        last_result_digest: [0; 32],
        state,
        slot,
        last_metadata_height: 200,
    }
}

/// Created market plus an F02 worker table in the identity section.
fn market_with_workers(count: u8) -> Vec<u8> {
    let base = created_state();
    let shared = state::decode_shared_state(&base).unwrap();
    let mut table = WorkerTable::new();
    for slot in 0..count {
        let st = if slot % 2 == 0 {
            WorkerState::Available
        } else {
            WorkerState::Retired
        };
        table.insert(worker(slot, st)).unwrap();
    }
    let mut section = vec![0; WORKER_TABLE_MAX_BYTES];
    let n = if count == 0 {
        0
    } else {
        table.encode(&mut section).unwrap()
    };
    let next = shared
        .replace_section(Section::IdentityRoster, &section[..n])
        .unwrap();
    encode(&next)
}

/// A valid common state frame at exactly MAX_STATE_BYTES (framing-level filler sections).
fn full_state() -> Vec<u8> {
    let caps: Vec<usize> = codec::STATE_SECTION_CAPS[..5]
        .iter()
        .map(|c| c - codec::STATE_SECTION_HEADER_BYTES)
        .collect();
    let fill: Vec<Vec<u8>> = caps
        .iter()
        .enumerate()
        .map(|(i, c)| vec![i as u8 + 1; *c])
        .collect();
    let mut replay = ReplayTable::new();
    replay
        .bind(state::ActorSlot::OWNER, p(OWNER), Version::new(1).unwrap())
        .unwrap();
    let empty = Control {
        replay: replay.clone(),
        feature_bytes: &[],
    };
    let spare = Section::Control.payload_cap() - empty.encoded_len().unwrap();
    let feature = vec![9u8; spare];
    let s = SharedState {
        revision: 3,
        feature_sections: [&fill[0], &fill[1], &fill[2], &fill[3], &fill[4]],
        control: Control {
            replay,
            feature_bytes: &feature,
        },
    };
    let bytes = encode(&s);
    assert_eq!(bytes.len(), MAX_STATE_BYTES);
    bytes
}

fn chunk_payload(
    revision: u64,
    digest: Option<StateDigest>,
    offset: u32,
    requested: u16,
) -> Vec<u8> {
    let mut v = revision.to_be_bytes().to_vec();
    v.extend_from_slice(&digest.map_or([0; 32], |d| d.bytes()));
    v.extend_from_slice(&offset.to_be_bytes());
    v.extend_from_slice(&requested.to_be_bytes());
    v
}

fn proof(root: u8) -> ReadProof {
    ReadProof {
        chain: chain(),
        program: program(),
        native_state_root: d(root),
        observed_sequence: 77,
        execution_height: 1010,
        batch_id: d(0xBB),
    }
}

/// Discovery read, then pinned 8192-byte reads; returns encoded chunk bodies.
fn read_all(state_bytes: &[u8]) -> Vec<Vec<u8>> {
    let mut out = vec![0; 8244];
    let n = read_state_chunk(state_bytes, &chunk_payload(0, None, 0, 8192), &mut out).unwrap();
    let first = codec::decode_chunk_response(&out[..n]).unwrap();
    let (rev, dig, total) = (first.revision, first.digest, first.total_bytes);
    let mut bodies = vec![out[..n].to_vec()];
    let mut offset = 8192u32;
    while offset < total {
        let n = read_state_chunk(
            state_bytes,
            &chunk_payload(rev, Some(dig), offset, 8192),
            &mut out,
        )
        .unwrap();
        bodies.push(out[..n].to_vec());
        offset += 8192;
    }
    bodies
}

fn capture(state_bytes: &[u8], root: u8) -> (Vec<u8>, CaptureFacts) {
    let mut buf = vec![0; MAX_STATE_BYTES];
    let mut cap = StateCapture::new(&mut buf);
    for body in read_all(state_bytes) {
        cap.accept(&proof(root), &body).unwrap();
    }
    let (bytes, facts) = cap.finish().unwrap();
    (bytes.to_vec(), facts)
}

#[test]
fn header_reports_exact_state_identity_and_explicit_absence() {
    let s = market_with_workers(3);
    let h = read_header(&s, chain(), program()).unwrap();
    let shared = state::decode_shared_state(&s).unwrap();
    assert_eq!(h.revision, shared.revision);
    assert_eq!(h.digest, codec::state_digest(&s).unwrap());
    assert_eq!(h.total_bytes as usize, s.len());
    assert_eq!(h.market, derive_market(chain(), program()).unwrap());
    assert_eq!(h.epoch, Presence::Absent);
    assert_eq!(h.roster, Presence::Absent);
    assert_eq!(h.config.get(), 1);
    let mut b = [0u8; 192];
    let n = codec::encode_read_header(&h, &mut b).unwrap();
    assert_eq!(codec::decode_read_header(&b[..n]).unwrap(), h);
    assert_eq!(
        read_header(&s, ChainDomain::new([99; 32]).unwrap(), program()),
        Err(WRONG_DOMAIN)
    );
    assert_eq!(
        read_header(&s, chain(), ProgramId::new([99; 32]).unwrap()),
        Err(WRONG_PROGRAM)
    );
    assert_eq!(policy_digest(&s).unwrap(), policy().digest().unwrap());
    let a = feature_availability(&s).unwrap();
    assert_eq!(a[0], Availability::Available);
    assert_eq!(a[1], Availability::Available);
    assert_eq!(a[2], Availability::NotYetProduced);
    assert_eq!(a[5], Availability::NotEnabled);
    assert_eq!(a[9], Availability::Available);
    assert!(read_header(&full_state(), chain(), program()).is_err());
}

#[test]
fn a02_finality_requires_rank4_evidence_bound_to_the_same_root() {
    let s = market_with_workers(2);
    let (bytes, facts) = capture(&s, 0xA1);
    assert_eq!(bytes, s);
    let unfinalized = FinalityEvidence {
        native_state_root: d(0xA1),
        checkpoint: d(0xC1),
        settlement: Presence::Absent,
        rank: 2,
    };
    let b = bind_snapshot(&bytes, &facts, &unfinalized, 5_000).unwrap();
    assert_eq!(b.require_finalized(), Err(QueryError::FinalityUnavailable));
    let other_root = FinalityEvidence {
        native_state_root: d(0xB2),
        checkpoint: d(0xC2),
        settlement: Presence::Absent,
        rank: 4,
    };
    assert_eq!(
        bind_snapshot(&bytes, &facts, &other_root, 5_000),
        Err(QueryError::BindingMismatch)
    );
    let finalized = FinalityEvidence {
        native_state_root: d(0xA1),
        checkpoint: d(0xC3),
        settlement: Presence::Present(d(0xD3)),
        rank: 4,
    };
    let f = bind_snapshot(&bytes, &facts, &finalized, 9_000).unwrap();
    assert_eq!(f.require_finalized(), Ok(()));
    assert_eq!(f.snapshot_id().unwrap(), b.snapshot_id().unwrap());
    let mut x = [0u8; BINDING_MAX_BYTES];
    let mut y = [0u8; BINDING_MAX_BYTES];
    let nx = b.encode(&mut x).unwrap();
    let ny = f.encode(&mut y).unwrap();
    assert_eq!(ny, nx + 32);
    assert_eq!(&x[..260], &y[..260]);
    assert_eq!(f.epoch, Presence::Absent);
    assert_eq!(f.state_digest, codec::state_digest(&s).unwrap());
    assert_eq!(f.policy, policy().digest().unwrap());
    let bad_rank = FinalityEvidence {
        rank: 5,
        ..finalized
    };
    assert_eq!(
        bind_snapshot(&bytes, &facts, &bad_rank, 9_000),
        Err(QueryError::Application(NON_CANONICAL))
    );
}

#[test]
fn a03_score_and_reward_fields_never_fabricate_numbers() {
    let w = ScoreField::present(7, 0).unwrap();
    assert_eq!(w.status(), ScoreStatus::Present);
    assert_eq!(w.ppm(), Presence::Present(0));
    let x = ScoreField::absent(Presence::Present(7), ScoreStatus::InsufficientCoverage).unwrap();
    assert_eq!(x.ppm(), Presence::Absent);
    assert_eq!(
        ScoreField::absent(Presence::Absent, ScoreStatus::Present),
        Err(NON_CANONICAL)
    );
    assert_eq!(ScoreField::present(7, 1_000_001), Err(NON_CANONICAL));
    assert_eq!(
        RewardField::unavailable(Availability::Available),
        Err(NON_CANONICAL)
    );
    assert_eq!(
        RewardField::entitlement(AssetId::new(ASSET).unwrap(), 5, 6),
        Err(NON_CANONICAL)
    );
    let s = market_with_workers(2);
    let mut rows = [ParticipantRow {
        kind: ParticipantKind::Worker,
        id: [1; 32],
        owner: p(OWNER),
        generation: 0,
        identity_state: 0,
        frozen_member: false,
        frozen_generation: Presence::Absent,
        eligibility: 0,
        metadata: Presence::Absent,
        metadata_revision: 0,
        score: w,
        reward: RewardField::unavailable(Availability::NotEnabled).unwrap(),
        history_status: Availability::NotYetProduced,
        history: Presence::Absent,
    }; 32];
    let n = worker_rows(&s, None, &mut rows).unwrap();
    assert_eq!(n, 2);
    for r in &rows[..n] {
        assert_eq!(r.score.status(), ScoreStatus::NotProduced);
        assert_eq!(r.score.ppm(), Presence::Absent);
        assert_eq!(r.reward.status(), Availability::NotEnabled);
        assert_eq!(r.reward.earned(), Presence::Absent);
        assert_eq!(r.reward.claimed(), Presence::Absent);
    }
}

fn evaluator_roster(workers: &[WorkerCurrent]) -> Vec<u8> {
    let mut evaluators: Vec<EvaluatorRosterEntry> = (0..8u8)
        .map(|i| EvaluatorRosterEntry {
            evaluator: EvaluatorId::new([100 + i; 32]).unwrap(),
            owner: p([60 + i; 32]),
            grant: Version::new(3).unwrap(),
            key_version: Version::new(1).unwrap(),
            public_key: PublicKey32([70 + i; 32]),
            rubric: RubricDigest::new([4; 32]).unwrap(),
        })
        .collect();
    evaluators.sort_by_key(|e| e.evaluator);
    let mut frozen: Vec<WorkerRosterEntry> = workers
        .iter()
        .map(|w| WorkerRosterEntry {
            worker: w.worker,
            owner: w.owner,
            recipient: AccountId::new([40; 32]).unwrap(),
            generation: Version::new(w.generation).unwrap(),
            key_version: Version::new(w.key_version).unwrap(),
            public_key: w.delegate,
            metadata: w.metadata,
        })
        .collect();
    frozen.sort_by_key(|w| w.worker);
    let roster = codec::Roster {
        market: derive_market(chain(), program()).unwrap(),
        epoch: 7,
        config: Version::new(1).unwrap(),
        workers: &frozen,
        evaluators: &evaluators,
    };
    let mut out = vec![0; codec::ROSTER_MAX_BYTES];
    let n = codec::encode_roster(&roster, &mut out).unwrap();
    out.truncate(n);
    out
}

fn all_rows(s: &[u8], roster: &[u8]) -> Vec<ParticipantRow> {
    let view = codec::decode_roster(roster).unwrap();
    let blank = {
        let mut one = Vec::new();
        one.resize(
            40,
            ParticipantRow {
                kind: ParticipantKind::Worker,
                id: [1; 32],
                owner: p(OWNER),
                generation: 0,
                identity_state: 0,
                frozen_member: false,
                frozen_generation: Presence::Absent,
                eligibility: 0,
                metadata: Presence::Absent,
                metadata_revision: 0,
                score: ScoreField::absent(Presence::Absent, ScoreStatus::Unavailable).unwrap(),
                reward: RewardField::unavailable(Availability::NotEnabled).unwrap(),
                history_status: Availability::NotYetProduced,
                history: Presence::Absent,
            },
        );
        one
    };
    let mut rows = blank;
    let nw = worker_rows(s, Some(&view), &mut rows).unwrap();
    let ne = evaluator_rows(&view, &mut rows[nw..]).unwrap();
    rows.truncate(nw + ne);
    rows
}

#[test]
fn a04_pagination_order_bounds_and_authenticated_cursor() {
    let s = market_with_workers(32);
    let table = {
        let shared = state::decode_shared_state(&s).unwrap();
        WorkerTable::decode(shared.section(Section::IdentityRoster).unwrap()).unwrap()
    };
    let workers: Vec<WorkerCurrent> = table.iter().copied().collect();
    let roster = evaluator_roster(&workers);
    let rows = all_rows(&s, &roster);
    assert_eq!(rows.len(), 40);
    assert!(rows.iter().all(|r| r.frozen_member));
    let secret = [0x5A; 32];
    let key = CursorKey {
        secret: &secret,
        generation: 1,
    };
    let scope = CursorScope {
        visibility: d(0x77),
        market: derive_market(chain(), program()).unwrap(),
        snapshot: d(0x88),
        filter: KindFilter::All,
        active_only: false,
    };
    let limit = parse_limit(Some("32")).unwrap();
    let mut page = [rows[0]; PAGE_MAX_ROWS];
    let (n, next) = select_page(&rows, KindFilter::All, false, 0, limit, &mut page).unwrap();
    assert_eq!((n, next), (32, Some(32)));
    assert!(page[..32].iter().all(|r| r.kind == ParticipantKind::Worker));
    assert!(page[..32].windows(2).all(|w| w[0].id < w[1].id));
    let mut token = [0u8; CURSOR_TOKEN_BYTES];
    let cursor = issue_cursor(&key, &scope, 32, 1_000, &mut token)
        .unwrap()
        .to_owned();
    assert!(cursor.len() <= CURSOR_MAX_BYTES);
    let mut frame = vec![0; PAGE_MAX_BYTES];
    let len = encode_page(d(0x88), &page[..n], Some(&cursor), &mut frame).unwrap();
    assert!(len <= PAGE_MAX_BYTES);
    let start = open_cursor(&key, &scope, &cursor, 2_000, rows.len()).unwrap();
    assert_eq!(start, 32);
    let (n2, next2) = select_page(&rows, KindFilter::All, false, start, limit, &mut page).unwrap();
    assert_eq!((n2, next2), (8, None));
    assert!(page[..8]
        .iter()
        .all(|r| r.kind == ParticipantKind::Evaluator));
    assert!(page[..8].windows(2).all(|w| w[0].id < w[1].id));

    assert_eq!(parse_limit(Some("0")), Err(NON_CANONICAL));
    assert_eq!(parse_limit(Some("33")), Err(NON_CANONICAL));
    assert_eq!(parse_limit(Some("01")), Err(NON_CANONICAL));
    assert_eq!(parse_limit(None), Ok(16));
    assert_eq!(
        select_page(&rows, KindFilter::All, false, 0, 33, &mut page),
        Err(NON_CANONICAL)
    );
    assert_eq!(KindFilter::from_u8(3), Err(NON_CANONICAL));
    let id = "ab".repeat(32);
    assert_eq!(parse_hex32(&id), Ok([0xab; 32]));
    assert_eq!(parse_hex32(&format!("{id}a")), Err(NON_CANONICAL));
    assert_eq!(parse_hex32(&id.to_uppercase()), Err(NON_CANONICAL));

    // forged next ordinal 41: re-encode the payload ordinal with the original tag
    let mut forged = cursor.clone().into_bytes();
    let ordinal_hex = 2 * (2 + 128);
    forged[ordinal_hex..ordinal_hex + 4].copy_from_slice(b"0029");
    let forged = String::from_utf8(forged).unwrap();
    assert_eq!(
        open_cursor(&key, &scope, &forged, 2_000, rows.len()),
        Err(QueryError::CursorMismatch)
    );
    let mut t41 = [0u8; CURSOR_TOKEN_BYTES];
    let c41 = issue_cursor(&key, &scope, 41, 1_000, &mut t41).unwrap();
    assert_eq!(
        open_cursor(&key, &scope, c41, 2_000, rows.len()),
        Err(QueryError::CursorMismatch)
    );
    assert_eq!(
        open_cursor(&key, &scope, &"a".repeat(1025), 2_000, rows.len()),
        Err(QueryError::Application(NON_CANONICAL))
    );
    assert_eq!(
        open_cursor(
            &key,
            &scope,
            &cursor,
            1_000 + CURSOR_LIFETIME_MS,
            rows.len()
        ),
        Err(QueryError::CursorExpired)
    );
    let other_scope = CursorScope {
        active_only: true,
        ..scope
    };
    assert_eq!(
        open_cursor(&key, &other_scope, &cursor, 2_000, rows.len()),
        Err(QueryError::CursorMismatch)
    );
    let rotated = CursorKey {
        secret: &secret,
        generation: 2,
    };
    assert_eq!(
        open_cursor(&rotated, &scope, &cursor, 2_000, rows.len()),
        Err(QueryError::CursorMismatch)
    );

    // active-only evaluator filter matches nothing: empty rows, no cursor
    let (n3, next3) = select_page(&rows, KindFilter::Evaluator, true, 0, 16, &mut page).unwrap();
    assert_eq!((n3, next3), (0, None));
    let len = encode_page(d(0x88), &page[..0], None, &mut frame).unwrap();
    assert_eq!(len, 2 + 32 + 1 + 1);
    // active-only workers: only Available rows (even slots)
    let (n4, _) = select_page(&rows, KindFilter::Worker, true, 0, 32, &mut page).unwrap();
    assert_eq!(n4, 16);
    // noncanonical row order refuses
    let mut swapped = rows.clone();
    swapped.swap(0, 1);
    assert_eq!(
        select_page(&swapped, KindFilter::All, false, 0, 16, &mut page),
        Err(NON_CANONICAL)
    );
}

#[test]
fn a13_full_state_capture_is_24_pinned_chunks_with_no_effects() {
    let s = full_state();
    let before = s.clone();
    let bodies = read_all(&s);
    assert_eq!(bodies.len(), FULL_CAPTURE_CHUNKS);
    assert_eq!(FULL_CAPTURE_CHUNKS, 24);
    let request = RequestDigest::new([0x42; 32]).unwrap();
    let revision = state::decode_shared_state(&s).unwrap().revision;
    let mut framed = vec![0; MAX_RESULT_BYTES];
    for body in &bodies {
        let c = codec::decode_chunk_response(body).unwrap();
        assert_eq!(c.bytes.len(), 8192);
        let n = frame_read_result(request, revision, body, &mut framed).unwrap();
        assert!(n <= MAX_RESULT_BYTES);
        assert_eq!(
            codec::decode_result(&framed[..n]).unwrap().payload,
            &body[..]
        );
    }
    let (bytes, facts) = capture(&s, 0xA1);
    assert_eq!(bytes, s);
    assert_eq!(facts.chunks, 24);
    assert_eq!(facts.total_bytes as usize, MAX_STATE_BYTES);
    // selectors performed no mutation of the captured value
    assert_eq!(s, before);

    // 196609 bytes violates the common state bound
    let mut over = s.clone();
    over.push(0);
    let mut out = vec![0; 8244];
    assert_eq!(
        read_state_chunk(&over, &chunk_payload(0, None, 0, 8192), &mut out),
        Err(CAPACITY)
    );
    assert_eq!(codec::state_digest(&over), Err(CAPACITY));

    // offset 8193 and offset 196608 refuse
    let dig = codec::state_digest(&s).unwrap();
    assert_eq!(
        read_state_chunk(
            &s,
            &chunk_payload(revision, Some(dig), 8193, 8192),
            &mut out
        ),
        Err(NON_CANONICAL)
    );
    assert_eq!(
        read_state_chunk(
            &s,
            &chunk_payload(revision, Some(dig), 196_608, 8192),
            &mut out
        ),
        Err(NON_CANONICAL)
    );
    // stale pin, half-pinned discovery and wrong payload length refuse
    assert_eq!(
        read_state_chunk(
            &s,
            &chunk_payload(revision + 1, Some(dig), 0, 8192),
            &mut out
        ),
        Err(CONFLICT)
    );
    assert_eq!(
        read_state_chunk(&s, &chunk_payload(revision, None, 0, 8192), &mut out),
        Err(NON_CANONICAL)
    );
    assert_eq!(
        read_state_chunk(&s, &chunk_payload(0, None, 0, 8192)[..45], &mut out),
        Err(NON_CANONICAL)
    );
    // small individual inspection is bounded and never crosses the end
    let small = market_with_workers(1);
    let mut cap = vec![0; 8244];
    let n = read_state_chunk(&small, &chunk_payload(0, None, 0, 100), &mut cap).unwrap();
    assert_eq!(
        codec::decode_chunk_response(&cap[..n]).unwrap().bytes.len(),
        100
    );

    // one chunk under a different root fails the whole capture
    let mut buf = vec![0; MAX_STATE_BYTES];
    let mut c = StateCapture::new(&mut buf);
    for (i, body) in bodies.iter().enumerate() {
        let r = c.accept(&proof(if i == 5 { 0xB2 } else { 0xA1 }), body);
        if i == 5 {
            assert_eq!(r, Err(QueryError::SnapshotConflict));
            break;
        }
        r.unwrap();
    }
    // one missing chunk fails
    let mut buf = vec![0; MAX_STATE_BYTES];
    let mut c = StateCapture::new(&mut buf);
    for (i, body) in bodies.iter().enumerate() {
        if i == 9 {
            continue;
        }
        let r = c.accept(&proof(0xA1), body);
        if i == 10 {
            assert_eq!(r, Err(QueryError::IntegrityFailure));
            break;
        }
        r.unwrap();
    }
    // truncated capture fails at finish
    let mut buf = vec![0; MAX_STATE_BYTES];
    let mut c = StateCapture::new(&mut buf);
    for body in &bodies[..23] {
        c.accept(&proof(0xA1), body).unwrap();
    }
    assert_eq!(c.finish().err(), Some(QueryError::IntegrityFailure));
    // capture buffer smaller than the declared total refuses before copying
    let mut tiny = vec![0; 8192];
    let mut c = StateCapture::new(&mut tiny);
    assert_eq!(
        c.accept(&proof(0xA1), &bodies[0]),
        Err(QueryError::Application(CAPACITY))
    );

    assert_eq!(bound_response(65_536), Ok(65_536));
    assert_eq!(bound_response(65_537), Err(QueryError::ResponseTooLarge));
}
