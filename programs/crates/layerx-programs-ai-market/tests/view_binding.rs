use layerx_programs_ai_market::{
    codec::{self, decode_envelope, derive_market, derive_worker, encode_envelope, Envelope},
    dispatch,
    errors::{
        ApplicationError, CAPACITY, CONFLICT, NON_CANONICAL, NOT_FOUND, WRONG_DOMAIN, WRONG_MARKET,
        WRONG_PROGRAM,
    },
    policy::{PolicyCommitments, TaskPolicyV1, TASK_POLICY_BYTES},
    queries::{
        bind_snapshot, bound_response, encode_page, feature_availability, frame_read_result,
        issue_cursor, open_cursor, parse_hex32, parse_limit, participant_rows, policy_digest,
        read_header, read_state_chunk, select_page, Availability, CaptureFacts, CursorKey,
        CursorScope, FinalityEvidence, KindFilter, ParticipantKind, ParticipantRow, QueryError,
        ReadProof, RewardField, ScoreField, ScoreStatus, StateCapture, BINDING_MAX_BYTES,
        CURSOR_LIFETIME_MS, CURSOR_MAX_BYTES, CURSOR_TOKEN_BYTES, FULL_CAPTURE_CHUNKS,
        PAGE_MAX_BYTES, PAGE_MAX_ROWS,
    },
    registry::{derive_rewards_account, F01_SECTION_CAP},
    registry_ops::{self, Outcome},
    state::{self, ActorSlot, Control, ReplayTable, Section, SharedState},
    types::{
        AccountId, AssetId, Authentication, ChainDomain, Digest32, EvaluatorId,
        EvaluatorRosterEntry, MetadataDigest, PolicyDigest, Presence, PrincipalId, ProgramId,
        PublicKey32, RequestDigest, RequestId, RubricDigest, StateDigest, Version,
        WorkerRosterEntry,
    },
    workers::{WorkerCurrent, WorkerState, WorkerTable, WORKER_TABLE_MAX_BYTES},
    MAX_EVENT_BYTES, MAX_RESULT_BYTES, MAX_STATE_BYTES,
};

const CHAIN: [u8; 32] = [10; 32];
const PROGRAM: [u8; 32] = [11; 32];
const OWNER: [u8; 32] = [12; 32];
const ASSET: [u8; 32] = [13; 32];
const REFUND: [u8; 32] = [14; 32];
const CHUNK_RESPONSE_MAX: usize = 8_244;
/// Encoded content-identity prefix with epoch and roster absent.
const ABSENT_PREFIX_BYTES: usize = 260;

enum Failure {
    Application(ApplicationError),
    Query(QueryError),
    Unexpected(&'static str),
}
impl core::fmt::Debug for Failure {
    fn fmt(&self, f: &mut core::fmt::Formatter<'_>) -> core::fmt::Result {
        match self {
            Self::Application(error) => write!(f, "application refusal {error:?}"),
            Self::Query(error) => write!(f, "query refusal {error:?}"),
            Self::Unexpected(what) => write!(f, "unexpected {what}"),
        }
    }
}
impl From<ApplicationError> for Failure {
    fn from(error: ApplicationError) -> Self {
        Self::Application(error)
    }
}
impl From<QueryError> for Failure {
    fn from(error: QueryError) -> Self {
        Self::Query(error)
    }
}
type Checked<T = ()> = Result<T, Failure>;

fn chain() -> Checked<ChainDomain> {
    Ok(ChainDomain::new(CHAIN)?)
}
fn program() -> Checked<ProgramId> {
    Ok(ProgramId::new(PROGRAM)?)
}
fn principal(bytes: [u8; 32]) -> Checked<PrincipalId> {
    Ok(PrincipalId::new(bytes)?)
}
fn digest(byte: u8) -> Checked<Digest32> {
    Ok(Digest32::new([byte; 32])?)
}

fn policy() -> Checked<TaskPolicyV1> {
    Ok(TaskPolicyV1::bounded_default(
        1,
        1,
        PolicyCommitments {
            model_artifact: digest(1)?,
            dataset_artifact: [2; 32],
            benchmark_suite: digest(3)?,
            rubric: RubricDigest::new([4; 32])?,
            task_schema: digest(5)?,
            result_schema: digest(6)?,
            service_terms: digest(7)?,
        },
        100,
        1,
    )?)
}
fn policy_id() -> Checked<PolicyDigest> {
    Ok(policy()?.digest()?)
}

fn encode_shared(shared: &SharedState<'_>) -> Checked<Vec<u8>> {
    let mut out = vec![0; shared.encoded_len()?];
    let mut scratch = vec![0; Section::Control.payload_cap()];
    let n = state::encode_shared_state(shared, &mut out, &mut scratch)?;
    out.truncate(n);
    Ok(out)
}

/// Real F01 CREATE through `registry_ops::apply`.
fn created_state() -> Checked<Vec<u8>> {
    let mut encoded_policy = vec![0; TASK_POLICY_BYTES];
    policy()?.encode(&mut encoded_policy)?;
    let rewards = derive_rewards_account(program()?, AssetId::new(ASSET)?)?;
    let mut payload = OWNER.to_vec();
    payload.extend_from_slice(&ASSET);
    payload.extend_from_slice(rewards.as_bytes());
    payload.extend_from_slice(&REFUND);
    payload.push(0);
    payload.extend_from_slice(&encoded_policy);
    payload.extend_from_slice(&[16; 32]);
    let envelope = Envelope {
        operation: dispatch::CREATE,
        chain: chain()?,
        program: program()?,
        market: derive_market(chain()?, program()?)?,
        actor: principal(OWNER)?,
        epoch: 0,
        config: 1,
        roster: Presence::Absent,
        sequence: 1,
        expiry: 1_000_000,
        request: RequestId::new([1; 32])?,
        payload: &payload,
        authentication: Authentication::Native,
    };
    let mut encoded = vec![0; 16_384];
    let n = encode_envelope(&envelope, &mut encoded)?;
    let validated = decode_envelope(encoded.get(..n).ok_or(Failure::Unexpected("envelope"))?)?;
    let ctx = registry_ops::CallContext {
        chain: chain()?,
        program: program()?,
        principal: principal(OWNER)?,
        height: 1000,
    };
    let mut section = vec![0; F01_SECTION_CAP];
    let mut event = vec![0; MAX_EVENT_BYTES];
    match registry_ops::apply(&ctx, None, &validated, &mut section, &mut event)? {
        Outcome::Applied { state, .. } => encode_shared(&state),
        Outcome::AlreadyApplied(_) => Err(Failure::Unexpected("fresh create retried")),
    }
}

fn worker(slot: u8, state: WorkerState) -> Checked<WorkerCurrent> {
    let market = derive_market(chain()?, program()?)?;
    Ok(WorkerCurrent {
        worker: derive_worker(market, principal([20; 32])?, [slot + 1; 32])?,
        owner: principal([20; 32])?,
        delegate: PublicKey32([slot + 50; 32]),
        metadata: MetadataDigest::new([30; 32])?,
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
    })
}

/// Created market plus an F02 worker table; even slots available, odd slots retired.
fn market_with_workers(count: u8) -> Checked<Vec<u8>> {
    let base = created_state()?;
    let shared = state::decode_shared_state(&base)?;
    let mut table = WorkerTable::new();
    for slot in 0..count {
        let status = if slot % 2 == 0 {
            WorkerState::Available
        } else {
            WorkerState::Retired
        };
        table.insert(&worker(slot, status)?)?;
    }
    let mut section = vec![0; WORKER_TABLE_MAX_BYTES];
    let n = if count == 0 {
        0
    } else {
        table.encode(&mut section)?
    };
    let identity = section
        .get(..n)
        .ok_or(Failure::Unexpected("worker table"))?;
    encode_shared(&shared.replace_section(Section::IdentityRoster, identity)?)
}

fn owner_replay() -> Checked<ReplayTable> {
    let mut replay = ReplayTable::new();
    replay.bind(ActorSlot::OWNER, principal(OWNER)?, Version::new(1)?)?;
    Ok(replay)
}

/// A valid common state frame at exactly `MAX_STATE_BYTES` (framing-level filler sections).
fn full_state() -> Checked<Vec<u8>> {
    let fill: Vec<Vec<u8>> = codec::STATE_SECTION_CAPS[..5]
        .iter()
        .zip(1u8..)
        .map(|(cap, byte)| vec![byte; cap - codec::STATE_SECTION_HEADER_BYTES])
        .collect();
    let empty = Control {
        replay: owner_replay()?,
        feature_bytes: &[],
    };
    let feature = vec![9u8; Section::Control.payload_cap() - empty.encoded_len()?];
    let shared = SharedState {
        revision: 3,
        feature_sections: [&fill[0], &fill[1], &fill[2], &fill[3], &fill[4]],
        control: Control {
            replay: owner_replay()?,
            feature_bytes: &feature,
        },
    };
    let bytes = encode_shared(&shared)?;
    assert_eq!(bytes.len(), MAX_STATE_BYTES);
    Ok(bytes)
}

fn chunk_payload(
    revision: u64,
    pinned: Option<StateDigest>,
    offset: u32,
    requested: u16,
) -> Vec<u8> {
    let mut payload = revision.to_be_bytes().to_vec();
    payload.extend_from_slice(&pinned.map_or([0; 32], StateDigest::bytes));
    payload.extend_from_slice(&offset.to_be_bytes());
    payload.extend_from_slice(&requested.to_be_bytes());
    payload
}

fn proof(root: u8) -> Checked<ReadProof> {
    Ok(ReadProof {
        chain: chain()?,
        program: program()?,
        native_state_root: digest(root)?,
        observed_sequence: 77,
        execution_height: 1010,
        batch_id: digest(0xBB)?,
    })
}

/// Discovery read, then pinned 8192-byte reads; returns the encoded chunk bodies.
fn read_all(state_bytes: &[u8]) -> Checked<Vec<Vec<u8>>> {
    let mut out = vec![0; CHUNK_RESPONSE_MAX];
    let n = read_state_chunk(state_bytes, &chunk_payload(0, None, 0, 8192), &mut out)?;
    let first = codec::decode_chunk_response(&out[..n])?;
    let (revision, pinned, total) = (first.revision, first.digest, first.total_bytes);
    let mut bodies = vec![out[..n].to_vec()];
    for offset in (8192..total).step_by(8192) {
        let payload = chunk_payload(revision, Some(pinned), offset, 8192);
        let n = read_state_chunk(state_bytes, &payload, &mut out)?;
        bodies.push(out[..n].to_vec());
    }
    Ok(bodies)
}

fn capture(state_bytes: &[u8], root: u8) -> Checked<(Vec<u8>, CaptureFacts)> {
    let mut buffer = vec![0; MAX_STATE_BYTES];
    let mut capture = StateCapture::new(&mut buffer);
    for body in read_all(state_bytes)? {
        capture.accept(&proof(root)?, &body)?;
    }
    let (bytes, facts) = capture.finish()?;
    Ok((bytes.to_vec(), facts))
}

#[test]
fn header_reports_exact_state_identity_and_explicit_absence() -> Checked {
    let s = market_with_workers(3)?;
    let header = read_header(&s, chain()?, program()?)?;
    assert_eq!(header.revision, state::decode_shared_state(&s)?.revision);
    assert_eq!(header.digest, codec::state_digest(&s)?);
    assert_eq!(usize::try_from(header.total_bytes).ok(), Some(s.len()));
    assert_eq!(header.market, derive_market(chain()?, program()?)?);
    assert_eq!(header.epoch, Presence::Absent);
    assert_eq!(header.roster, Presence::Absent);
    assert_eq!(header.config.get(), 1);
    let mut encoded = [0u8; 192];
    let n = codec::encode_read_header(&header, &mut encoded)?;
    assert_eq!(codec::decode_read_header(&encoded[..n])?, header);
    assert_eq!(
        read_header(&s, ChainDomain::new([99; 32])?, program()?),
        Err(WRONG_DOMAIN)
    );
    assert_eq!(
        read_header(&s, chain()?, ProgramId::new([99; 32])?),
        Err(WRONG_PROGRAM)
    );
    assert_eq!(policy_digest(&s)?, policy_id()?);
    let availability = feature_availability(&s)?;
    assert_eq!(availability[0], Availability::Available);
    assert_eq!(availability[1], Availability::Available);
    assert_eq!(availability[2], Availability::NotYetProduced);
    assert_eq!(availability[5], Availability::NotEnabled);
    assert_eq!(availability[9], Availability::Available);

    let uncreated = encode_shared(&SharedState {
        revision: 1,
        feature_sections: [&[], &[], &[], &[], &[]],
        control: Control {
            replay: owner_replay()?,
            feature_bytes: &[],
        },
    })?;
    assert_eq!(
        read_header(&uncreated, chain()?, program()?),
        Err(NOT_FOUND)
    );
    assert_eq!(policy_digest(&uncreated), Err(NOT_FOUND));
    assert_eq!(
        feature_availability(&uncreated)?[0],
        Availability::NotYetProduced
    );
    Ok(())
}

#[test]
fn a02_finality_requires_rank4_evidence_bound_to_the_same_root() -> Checked {
    let s = market_with_workers(2)?;
    let (bytes, facts) = capture(&s, 0xA1)?;
    assert_eq!(bytes, s);
    let unfinalized = FinalityEvidence {
        native_state_root: digest(0xA1)?,
        checkpoint: digest(0xC1)?,
        settlement: Presence::Absent,
        rank: 2,
    };
    let sequenced = bind_snapshot(&bytes, &facts, &unfinalized, 5_000)?;
    assert_eq!(
        sequenced.require_finalized(),
        Err(QueryError::FinalityUnavailable)
    );
    let other_root = FinalityEvidence {
        native_state_root: digest(0xB2)?,
        checkpoint: digest(0xC2)?,
        settlement: Presence::Absent,
        rank: 4,
    };
    assert_eq!(
        bind_snapshot(&bytes, &facts, &other_root, 5_000),
        Err(QueryError::BindingMismatch)
    );
    let finalized = FinalityEvidence {
        native_state_root: digest(0xA1)?,
        checkpoint: digest(0xC3)?,
        settlement: Presence::Present(digest(0xD3)?),
        rank: 4,
    };
    let published = bind_snapshot(&bytes, &facts, &finalized, 9_000)?;
    assert_eq!(published.require_finalized(), Ok(()));
    assert_eq!(published.snapshot_id()?, sequenced.snapshot_id()?);
    let mut weak = [0u8; BINDING_MAX_BYTES];
    let mut strong = [0u8; BINDING_MAX_BYTES];
    let weak_len = sequenced.encode(&mut weak)?;
    let strong_len = published.encode(&mut strong)?;
    assert_eq!(strong_len, weak_len + 32);
    assert_eq!(&weak[..ABSENT_PREFIX_BYTES], &strong[..ABSENT_PREFIX_BYTES]);
    assert_eq!(published.epoch, Presence::Absent);
    assert_eq!(published.state_digest, codec::state_digest(&s)?);
    assert_eq!(published.policy, policy_id()?);
    let bad_rank = FinalityEvidence {
        rank: 5,
        ..finalized
    };
    assert_eq!(
        bind_snapshot(&bytes, &facts, &bad_rank, 9_000),
        Err(QueryError::Application(NON_CANONICAL))
    );
    let mut stale = facts;
    stale.revision += 1;
    assert_eq!(
        bind_snapshot(&bytes, &stale, &finalized, 9_000),
        Err(QueryError::BindingMismatch)
    );
    Ok(())
}

fn blank_row() -> Checked<ParticipantRow> {
    Ok(ParticipantRow {
        kind: ParticipantKind::Worker,
        id: [1; 32],
        owner: principal(OWNER)?,
        generation: 0,
        identity_state: 0,
        frozen_member: false,
        frozen_generation: Presence::Absent,
        eligibility: 0,
        metadata: Presence::Absent,
        metadata_revision: 0,
        score: ScoreField::absent(Presence::Absent, ScoreStatus::Unavailable)?,
        reward: RewardField::unavailable(Availability::NotEnabled)?,
        history_status: Availability::NotYetProduced,
        history: Presence::Absent,
    })
}

#[test]
fn a03_score_and_reward_fields_never_fabricate_numbers() -> Checked {
    let zero = ScoreField::present(7, 0)?;
    assert_eq!(zero.status(), ScoreStatus::Present);
    assert_eq!(zero.ppm(), Presence::Present(0));
    assert_eq!(zero.epoch(), Presence::Present(7));
    let thin = ScoreField::absent(Presence::Present(7), ScoreStatus::InsufficientCoverage)?;
    assert_eq!(thin.ppm(), Presence::Absent);
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
        RewardField::entitlement(AssetId::new(ASSET)?, 5, 6),
        Err(NON_CANONICAL)
    );
    let funded = RewardField::entitlement(AssetId::new(ASSET)?, 6, 5)?;
    assert_eq!(funded.asset(), Presence::Present(AssetId::new(ASSET)?));

    let s = market_with_workers(2)?;
    let mut rows = vec![blank_row()?; 2];
    assert_eq!(participant_rows(&s, None, &mut rows)?, 2);
    for row in &rows {
        assert_eq!(row.score.status(), ScoreStatus::NotProduced);
        assert_eq!(row.score.ppm(), Presence::Absent);
        assert_eq!(row.reward.status(), Availability::NotEnabled);
        assert_eq!(row.reward.asset(), Presence::Absent);
        assert_eq!(row.reward.earned(), Presence::Absent);
        assert_eq!(row.reward.claimed(), Presence::Absent);
        assert!(!row.frozen_member);
    }
    assert_eq!(participant_rows(&s, None, &mut rows[..1]), Err(CAPACITY));
    Ok(())
}

fn frozen_roster(workers: &[WorkerCurrent], market_program: [u8; 32]) -> Checked<Vec<u8>> {
    let mut evaluators = Vec::new();
    for i in 0..8u8 {
        evaluators.push(EvaluatorRosterEntry {
            evaluator: EvaluatorId::new([100 + i; 32])?,
            owner: principal([60 + i; 32])?,
            grant: Version::new(3)?,
            key_version: Version::new(1)?,
            public_key: PublicKey32([70 + i; 32]),
            rubric: RubricDigest::new([4; 32])?,
        });
    }
    let mut frozen = Vec::new();
    for w in workers {
        frozen.push(WorkerRosterEntry {
            worker: w.worker,
            owner: w.owner,
            recipient: AccountId::new([40; 32])?,
            generation: Version::new(w.generation)?,
            key_version: Version::new(w.key_version)?,
            public_key: w.delegate,
            metadata: w.metadata,
        });
    }
    evaluators.sort_by_key(|entry| entry.evaluator);
    frozen.sort_by_key(|entry| entry.worker);
    let roster = codec::Roster {
        market: derive_market(chain()?, ProgramId::new(market_program)?)?,
        epoch: 7,
        config: Version::new(1)?,
        workers: &frozen,
        evaluators: &evaluators,
    };
    let mut out = vec![0; codec::ROSTER_MAX_BYTES];
    let n = codec::encode_roster(&roster, &mut out)?;
    out.truncate(n);
    Ok(out)
}

/// 32 current workers and 8 frozen evaluators projected from real state and roster codecs.
fn forty_rows() -> Checked<Vec<ParticipantRow>> {
    let s = market_with_workers(32)?;
    let shared = state::decode_shared_state(&s)?;
    let table = WorkerTable::decode(shared.section(Section::IdentityRoster)?)?;
    let workers: Vec<WorkerCurrent> = table.iter().copied().collect();
    let roster = frozen_roster(&workers, PROGRAM)?;
    let view = codec::decode_roster(&roster)?;
    let mut rows = vec![blank_row()?; 40];
    assert_eq!(participant_rows(&s, Some(&view), &mut rows)?, 40);
    let foreign = frozen_roster(&workers, [98; 32])?;
    assert_eq!(
        participant_rows(
            &s,
            Some(&codec::decode_roster(&foreign)?),
            &mut rows.clone()
        ),
        Err(WRONG_MARKET)
    );
    Ok(rows)
}

fn scope(filter: KindFilter, active_only: bool) -> Checked<CursorScope> {
    Ok(CursorScope {
        visibility: digest(0x77)?,
        market: derive_market(chain()?, program()?)?,
        snapshot: digest(0x88)?,
        filter,
        active_only,
    })
}
const SECRET: [u8; 32] = [0x5A; 32];
const KEY: CursorKey<'static> = CursorKey {
    secret: &SECRET,
    generation: 1,
};

#[test]
fn a04_forty_rows_page_as_32_then_8_in_kind_then_id_order() -> Checked {
    let rows = forty_rows()?;
    assert!(rows.iter().all(|row| row.frozen_member));
    let all = scope(KindFilter::All, false)?;
    let limit = parse_limit(Some("32"))?;
    let mut page = [blank_row()?; PAGE_MAX_ROWS];
    let (n, next) = select_page(&rows, KindFilter::All, false, 0, limit, &mut page)?;
    assert_eq!((n, next), (32, Some(32)));
    assert!(page.iter().all(|row| row.kind == ParticipantKind::Worker));
    assert!(page.windows(2).all(|pair| pair[0].id < pair[1].id));
    let mut token = [0u8; CURSOR_TOKEN_BYTES];
    let cursor = issue_cursor(&KEY, &all, 32, 1_000, &mut token)?.to_owned();
    assert!(cursor.len() <= CURSOR_MAX_BYTES);
    let mut frame = vec![0; PAGE_MAX_BYTES];
    let len = encode_page(digest(0x88)?, &page[..n], Some(&cursor), &mut frame)?;
    assert!(len <= PAGE_MAX_BYTES);
    let start = open_cursor(&KEY, &all, &cursor, 2_000, rows.len())?;
    assert_eq!(start, 32);
    let (n, next) = select_page(&rows, KindFilter::All, false, start, limit, &mut page)?;
    assert_eq!((n, next), (8, None));
    assert!(page[..8]
        .iter()
        .all(|row| row.kind == ParticipantKind::Evaluator));
    assert!(page[..8].windows(2).all(|pair| pair[0].id < pair[1].id));
    let (n, _) = select_page(&rows, KindFilter::Worker, true, 0, 32, &mut page)?;
    assert_eq!(n, 16);
    let (n, next) = select_page(&rows, KindFilter::Evaluator, true, 0, 16, &mut page)?;
    assert_eq!((n, next), (0, None));
    let len = encode_page(digest(0x88)?, &page[..0], None, &mut frame)?;
    assert_eq!(len, 2 + 32 + 1 + 1);
    Ok(())
}

#[test]
fn a04_malformed_inputs_refuse_before_row_work() -> Checked {
    let rows = forty_rows()?;
    let mut page = [blank_row()?; PAGE_MAX_ROWS];
    assert_eq!(parse_limit(Some("0")), Err(NON_CANONICAL));
    assert_eq!(parse_limit(Some("33")), Err(NON_CANONICAL));
    assert_eq!(parse_limit(Some("01")), Err(NON_CANONICAL));
    assert_eq!(parse_limit(None), Ok(16));
    assert_eq!(
        select_page(&rows, KindFilter::All, false, 0, 0, &mut page),
        Err(NON_CANONICAL)
    );
    assert_eq!(
        select_page(&rows, KindFilter::All, false, 0, 33, &mut page),
        Err(NON_CANONICAL)
    );
    assert_eq!(KindFilter::from_u8(3), Err(NON_CANONICAL));
    let id = "ab".repeat(32);
    assert_eq!(parse_hex32(&id), Ok([0xab; 32]));
    assert_eq!(parse_hex32(&format!("{id}a")), Err(NON_CANONICAL));
    assert_eq!(parse_hex32(&id.to_uppercase()), Err(NON_CANONICAL));
    let mut swapped = rows.clone();
    swapped.swap(0, 1);
    assert_eq!(
        select_page(&swapped, KindFilter::All, false, 0, 16, &mut page),
        Err(NON_CANONICAL)
    );
    Ok(())
}

#[test]
fn a04_cursor_is_authenticated_scoped_and_expiring() -> Checked {
    let total = forty_rows()?.len();
    let all = scope(KindFilter::All, false)?;
    let mut token = [0u8; CURSOR_TOKEN_BYTES];
    let cursor = issue_cursor(&KEY, &all, 32, 1_000, &mut token)?.to_owned();
    let ordinal_hex = 2 * (2 + 4 * 32);
    let mut forged = cursor.clone().into_bytes();
    forged[ordinal_hex..ordinal_hex + 4].copy_from_slice(b"0029");
    let forged = String::from_utf8(forged).map_err(|_| Failure::Unexpected("utf8"))?;
    assert_eq!(
        open_cursor(&KEY, &all, &forged, 2_000, total),
        Err(QueryError::CursorMismatch)
    );
    let mut beyond = [0u8; CURSOR_TOKEN_BYTES];
    let ordinal_41 = issue_cursor(&KEY, &all, 41, 1_000, &mut beyond)?;
    assert_eq!(
        open_cursor(&KEY, &all, ordinal_41, 2_000, total),
        Err(QueryError::CursorMismatch)
    );
    assert_eq!(
        open_cursor(&KEY, &all, &"a".repeat(CURSOR_MAX_BYTES + 1), 2_000, total),
        Err(QueryError::Application(NON_CANONICAL))
    );
    assert_eq!(
        open_cursor(&KEY, &all, &cursor.to_uppercase(), 2_000, total),
        Err(QueryError::Application(NON_CANONICAL))
    );
    assert_eq!(
        open_cursor(&KEY, &all, &cursor, 1_000 + CURSOR_LIFETIME_MS, total),
        Err(QueryError::CursorExpired)
    );
    assert_eq!(
        open_cursor(&KEY, &scope(KindFilter::All, true)?, &cursor, 2_000, total),
        Err(QueryError::CursorMismatch)
    );
    let rotated = CursorKey {
        secret: &SECRET,
        generation: 2,
    };
    assert_eq!(
        open_cursor(&rotated, &all, &cursor, 2_000, total),
        Err(QueryError::CursorMismatch)
    );
    let other_secret = [0x5B; 32];
    let other = CursorKey {
        secret: &other_secret,
        generation: 1,
    };
    assert_eq!(
        open_cursor(&other, &all, &cursor, 2_000, total),
        Err(QueryError::CursorMismatch)
    );
    Ok(())
}

#[test]
fn a13_full_state_reconstructs_from_24_pinned_chunks_without_effects() -> Checked {
    let s = full_state()?;
    let before = s.clone();
    let bodies = read_all(&s)?;
    assert_eq!(FULL_CAPTURE_CHUNKS, 24);
    assert_eq!(bodies.len(), FULL_CAPTURE_CHUNKS);
    let request = RequestDigest::new([0x42; 32])?;
    let revision = state::decode_shared_state(&s)?.revision;
    let mut framed = vec![0; MAX_RESULT_BYTES];
    for body in &bodies {
        assert_eq!(codec::decode_chunk_response(body)?.bytes.len(), 8192);
        let n = frame_read_result(request, revision, body, &mut framed)?;
        assert!(n <= MAX_RESULT_BYTES);
        assert_eq!(codec::decode_result(&framed[..n])?.payload, body.as_slice());
    }
    let (bytes, facts) = capture(&s, 0xA1)?;
    assert_eq!(bytes, s);
    assert_eq!(facts.chunks, 24);
    assert_eq!(
        usize::try_from(facts.total_bytes).ok(),
        Some(MAX_STATE_BYTES)
    );
    assert_eq!(s, before);
    let small = market_with_workers(1)?;
    let mut out = vec![0; CHUNK_RESPONSE_MAX];
    let n = read_state_chunk(&small, &chunk_payload(0, None, 0, 100), &mut out)?;
    assert_eq!(codec::decode_chunk_response(&out[..n])?.bytes.len(), 100);
    assert_eq!(bound_response(PAGE_MAX_BYTES), Ok(PAGE_MAX_BYTES));
    assert_eq!(
        bound_response(PAGE_MAX_BYTES + 1),
        Err(QueryError::ResponseTooLarge)
    );
    Ok(())
}

#[test]
fn a13_selector_refuses_oversize_misaligned_and_stale_reads() -> Checked {
    let s = full_state()?;
    let revision = state::decode_shared_state(&s)?.revision;
    let pinned = codec::state_digest(&s)?;
    let mut out = vec![0; CHUNK_RESPONSE_MAX];
    let mut over = s.clone();
    over.push(0);
    assert_eq!(
        read_state_chunk(&over, &chunk_payload(0, None, 0, 8192), &mut out),
        Err(CAPACITY)
    );
    assert_eq!(codec::state_digest(&over), Err(CAPACITY));
    for offset in [8193, 196_608] {
        assert_eq!(
            read_state_chunk(
                &s,
                &chunk_payload(revision, Some(pinned), offset, 8192),
                &mut out
            ),
            Err(NON_CANONICAL)
        );
    }
    let small = market_with_workers(1)?;
    assert_eq!(
        read_state_chunk(&small, &chunk_payload(0, None, 8192, 8192), &mut out),
        Err(NON_CANONICAL)
    );
    assert_eq!(
        read_state_chunk(
            &s,
            &chunk_payload(revision + 1, Some(pinned), 0, 8192),
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
    assert_eq!(
        read_state_chunk(&s, &chunk_payload(0, None, 0, 0), &mut out),
        Err(NON_CANONICAL)
    );
    Ok(())
}

#[test]
fn a13_capture_fails_whole_on_root_change_gap_or_truncation() -> Checked {
    let s = full_state()?;
    let bodies = read_all(&s)?;
    let mut buffer = vec![0; MAX_STATE_BYTES];
    let mut mixed = StateCapture::new(&mut buffer);
    for body in &bodies[..5] {
        mixed.accept(&proof(0xA1)?, body)?;
    }
    assert_eq!(
        mixed.accept(&proof(0xB2)?, &bodies[5]),
        Err(QueryError::SnapshotConflict)
    );
    assert_eq!(
        mixed.accept(&proof(0xA1)?, &bodies[5]),
        Err(QueryError::IntegrityFailure)
    );
    assert_eq!(mixed.finish().err(), Some(QueryError::IntegrityFailure));

    let mut buffer = vec![0; MAX_STATE_BYTES];
    let mut gap = StateCapture::new(&mut buffer);
    for body in &bodies[..9] {
        gap.accept(&proof(0xA1)?, body)?;
    }
    assert_eq!(
        gap.accept(&proof(0xA1)?, &bodies[10]),
        Err(QueryError::IntegrityFailure)
    );

    let mut buffer = vec![0; MAX_STATE_BYTES];
    let mut truncated = StateCapture::new(&mut buffer);
    for body in &bodies[..23] {
        truncated.accept(&proof(0xA1)?, body)?;
    }
    assert_eq!(truncated.finish().err(), Some(QueryError::IntegrityFailure));

    let mut tiny = vec![0; 8192];
    let mut short = StateCapture::new(&mut tiny);
    assert_eq!(
        short.accept(&proof(0xA1)?, &bodies[0]),
        Err(QueryError::Application(CAPACITY))
    );
    Ok(())
}
