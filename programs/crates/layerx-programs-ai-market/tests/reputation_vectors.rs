// Pure source vectors; these do not qualify unavailable F05/F06 or native wiring.
pub use layerx_programs_ai_market::{codec, errors, types};
#[path = "../src/reputation.rs"]
pub mod reputation;
#[path = "../src/reputation_codec.rs"]
mod reputation_codec;
#[path = "../src/reputation_transition.rs"]
mod reputation_transition;
use errors::*;
use reputation::*;
use reputation_codec::*;
use reputation_transition::*;
use sha2::{Digest, Sha256};
use types::*;

enum Failure {
    Application(ApplicationError),
    Unexpected(&'static str),
}
impl core::fmt::Debug for Failure {
    fn fmt(&self, f: &mut core::fmt::Formatter<'_>) -> core::fmt::Result {
        match self {
            Self::Application(error) => write!(f, "application refusal {error:?}"),
            Self::Unexpected(what) => write!(f, "unexpected {what}"),
        }
    }
}
impl From<ApplicationError> for Failure {
    fn from(error: ApplicationError) -> Self {
        Self::Application(error)
    }
}
type Checked<T = ()> = Result<T, Failure>;

fn digest(n: u8) -> CodecResult<Digest32> {
    Digest32::new([n; 32])
}
fn key(n: u8) -> CodecResult<SegmentKey> {
    Ok(SegmentKey {
        market: MarketId::new([1; 32])?,
        worker: WorkerId::new([n; 32])?,
        config: Version::new(1)?,
        policy: digest(3)?,
        model: digest(4)?,
        reset_generation: Version::new(1)?,
    })
}
fn worker(n: u8) -> CodecResult<WorkerRosterEntry> {
    Ok(WorkerRosterEntry {
        worker: key(n)?.worker,
        owner: PrincipalId::new([9; 32])?,
        recipient: AccountId::new([10; 32])?,
        generation: Version::new(1)?,
        key_version: Version::new(1)?,
        public_key: PublicKey32([11; 32]),
        metadata: MetadataDigest::new([12; 32])?,
    })
}
fn frozen(epoch: u64) -> CodecResult<FrozenBinding> {
    Ok(FrozenBinding {
        chain: ChainDomain::new([13; 32])?,
        program: ProgramId::new([14; 32])?,
        market: key(2)?.market,
        epoch,
        config: Version::new(1)?,
        roster: RosterDigest::new([15; 32])?,
    })
}
fn fresh(n: u8) -> CodecResult<ReputationCurrent> {
    ReputationCurrent::bootstrap(key(n)?, worker(n)?.owner, 1)
}
fn observed(q: u32) -> CodecResult<ReputationCurrent> {
    let mut r = fresh(2)?;
    r.quality = Score::new(q)?;
    r.qualifying_count = 1;
    r.last_applied = Presence::Present(6);
    r.last_observed = Presence::Present(Observation {
        epoch: 6,
        height: 90,
    });
    r.last_transition_height = 90;
    Ok(r)
}
fn apply(
    r: &ReputationCurrent,
    epoch: u64,
    median: Presence<Score>,
    n: u8,
    e: u8,
    allowed: bool,
) -> CodecResult<ReputationCurrent> {
    update_record(
        r,
        key(2)?,
        frozen(epoch)?,
        &worker(2)?,
        EpochTally {
            median,
            support: n,
            eligible: e,
        },
        allowed,
        100 + epoch,
    )
}
fn score(n: u32) -> CodecResult<Presence<Score>> {
    Ok(Presence::Present(Score::new(n)?))
}
fn complete(
    state: &ReputationState,
    epoch: u64,
    result: Digest32,
    median: Presence<Score>,
    n: u8,
) -> CodecResult<(ReputationState, CompletedHistory)> {
    match CompletionDraft::begin(state, frozen(epoch)?, result, 200 + epoch, &[worker(2)?])? {
        CompletionStart::Pending(mut draft) => {
            draft.apply_worker(key(2)?, median, n, 6, true)?;
            draft.finish()
        }
        CompletionStart::Retained(r) => Ok((state.clone(), r)),
    }
}
#[test]
fn a01_continuity_and_count() -> Checked {
    let first = apply(&fresh(2)?, 7, score(800_000)?, 4, 6, true)?;
    assert_eq!(first.quality.get(), 100_000);
    assert_eq!(first.qualifying_count, 1);
    assert_eq!(first.confidence()?.get(), 125_000);
    let mut rotated = worker(2)?;
    rotated.key_version = Version::new(2)?;
    rotated.public_key = PublicKey32([22; 32]);
    let second = update_record(
        &first,
        key(2)?,
        frozen(8)?,
        &rotated,
        EpochTally {
            median: score(800_000)?,
            support: 4,
            eligible: 6,
        },
        true,
        108,
    )?;
    assert_eq!(second.quality.get(), 187_500);
    assert_eq!(second.confidence()?.get(), 250_000);
    assert_eq!(first.segment, second.segment);
    assert_eq!(second.reset_generation.get(), 1);
    Ok(())
}
#[test]
fn a02_floor_and_a03_zero_is_observed() -> Checked {
    assert_eq!(
        apply(&observed(123_457)?, 7, score(765_433)?, 3, 6, true)?
            .quality
            .get(),
        203_704
    );
    assert_eq!(
        apply(&observed(123_457)?, 7, Presence::Absent, 0, 6, true)?
            .quality
            .get(),
        121_527
    );
    let zero = apply(&observed(800_000)?, 7, score(0)?, 3, 6, true)?;
    let missing = apply(&observed(800_000)?, 7, Presence::Absent, 0, 6, true)?;
    assert_eq!(zero.quality.get(), 700_000);
    assert_eq!(zero.qualifying_count, 2);
    assert_eq!(missing.quality.get(), 787_500);
    assert_eq!(missing.qualifying_count, 1);
    assert_eq!(
        zero.last_observed,
        Presence::Present(Observation {
            epoch: 7,
            height: 107
        })
    );
    assert_eq!(missing.last_observed, observed(800_000)?.last_observed);
    assert_eq!(
        apply(&fresh(2)?, 0, Presence::Absent, 0, 0, true)?
            .quality
            .get(),
        0
    );
    Ok(())
}
#[test]
fn a04_bounds_saturation_and_generation_overflow() -> Checked {
    let mut r = observed(1_000_000)?;
    r.qualifying_count = 31;
    let r = apply(&r, 7, score(1_000_000)?, 3, 6, true)?;
    assert_eq!(r.quality.get(), 1_000_000);
    assert_eq!(r.qualifying_count, 32);
    assert_eq!(
        apply(&r, 8, score(1_000_000)?, 3, 6, true)?.qualifying_count,
        32
    );
    assert_eq!(confidence(32)?.get(), 1_000_000);
    assert_eq!(Score::new(1_000_001), Err(F03_SCORE_RANGE));
    assert_eq!(next_count(33, true), Err(NON_CANONICAL));
    let mut k = key(2)?;
    k.reset_generation = Version::new(u64::MAX)?;
    let mut r = fresh(2)?;
    r.reset_generation = k.reset_generation;
    r.segment = segment_digest(k)?;
    r.previous_segment = Presence::Present(digest(19)?);
    let bytes = encode_current(&r)?;
    assert_eq!(
        reset_segment(
            &r,
            k,
            SegmentBinding {
                config: k.config,
                policy: k.policy,
                model: k.model,
            },
            ClosureReason::OwnerReset,
            Presence::Absent,
            2
        ),
        Err(ARITHMETIC)
    );
    assert_eq!(encode_current(&r)?, bytes);
    Ok(())
}
#[test]
fn a05_independent_coverage_and_sealed_history_permission() -> Checked {
    assert_eq!(evidence_coverage(6, 4)?, score(666_666)?);
    let insufficient = apply(&observed(800_000)?, 7, Presence::Absent, 2, 8, true)?;
    assert_eq!(insufficient.coverage, score(250_000)?);
    assert_eq!(insufficient.quality.get(), 787_500);
    assert_eq!(evidence_coverage(0, 0)?, Presence::Absent);
    assert_eq!(evidence_coverage(0, 1), Err(F07_BINDING_MISMATCH));
    assert_eq!(evidence_coverage(9, 1), Err(F07_BINDING_MISMATCH));
    let mut suspended = observed(800_000)?;
    suspended.status = HistoryStatus::Suspended;
    assert_eq!(
        apply(&suspended, 7, score(0)?, 3, 6, true)?.quality.get(),
        700_000
    );
    let disallowed = apply(&observed(800_000)?, 7, score(800_000)?, 4, 6, false)?;
    assert_eq!(disallowed.quality.get(), 787_500);
    assert_eq!(disallowed.coverage, score(666_666)?);
    assert_eq!(
        apply(&fresh(2)?, 7, score(1)?, 2, 8, true),
        Err(F07_BINDING_MISMATCH)
    );
    // Evaluator deduplication/owner rejection belong to the missing real producer bridge.
    Ok(())
}
#[test]
fn a06_internal_staging_retry_order_and_gaps() -> Checked {
    let mut state = ReputationState::new(key(2)?.market);
    let mut r = observed(100_000)?;
    r.last_applied = Presence::Present(6);
    state.completed_through = Presence::Present(6);
    state.insert(r)?;
    let before = state.clone();
    let (next, receipt) = complete(&state, 12, digest(20)?, score(800_000)?, 4)?;
    assert_eq!(state, before);
    assert_eq!(
        next.records()
            .next()
            .ok_or(Failure::Unexpected("reputation record"))?
            .quality
            .get(),
        187_500
    );
    assert_eq!(next.completed().count(), 1);
    let (retry, retained) = complete(&next, 12, digest(20)?, score(800_000)?, 4)?;
    assert_eq!(retry, next);
    assert_eq!(retained, receipt);
    assert_eq!(next.assess_completion(12, digest(21)?), Err(CONFLICT));
    assert_eq!(next.assess_completion(11, digest(20)?), Err(WRONG_EPOCH));
    let (gap, _) = complete(&next, 22, digest(22)?, Presence::Absent, 0)?;
    assert_eq!(
        gap.records()
            .next()
            .ok_or(Failure::Unexpected("reputation record"))?
            .quality
            .get(),
        184_570
    );
    assert_eq!(gap.completed().count(), 2);
    let mut draft =
        match CompletionDraft::begin(&next, frozen(13)?, digest(23)?, 213, &[worker(2)?])? {
            CompletionStart::Pending(d) => d,
            CompletionStart::Retained(_) => panic!("new epoch must stage"),
        };
    assert_eq!(
        draft.apply_worker(key(2)?, score(1)?, 2, 6, true),
        Err(F07_BINDING_MISMATCH)
    );
    assert!(matches!(draft.finish(), Err(F07_EPOCH_NOT_SEALED)));
    assert_eq!(
        next.records()
            .next()
            .ok_or(Failure::Unexpected("reputation record"))?
            .quality
            .get(),
        187_500
    );
    Ok(())
}
#[test]
fn staged_multiworker_failure_cannot_publish_partial_history() -> Checked {
    let mut state = ReputationState::new(key(2)?.market);
    state.insert(fresh(2)?)?;
    state.insert(fresh(3)?)?;
    let before = state.clone();
    let mut draft = match CompletionDraft::begin(
        &state,
        frozen(1)?,
        digest(60)?,
        201,
        &[worker(2)?, worker(3)?],
    )? {
        CompletionStart::Pending(d) => d,
        CompletionStart::Retained(_) => panic!("new epoch must stage"),
    };
    draft.apply_worker(key(2)?, score(800_000)?, 3, 6, true)?;
    assert_eq!(
        draft.apply_worker(key(3)?, score(800_000)?, 2, 6, true),
        Err(F07_BINDING_MISMATCH)
    );
    assert!(matches!(draft.finish(), Err(F07_EPOCH_NOT_SEALED)));
    assert_eq!(state, before);
    assert_eq!(state.completed().count(), 0);
    assert!(state
        .records()
        .all(|r| r.quality.get() == 0 && r.last_applied == Presence::Absent));
    Ok(())
}
#[test]
fn a10_exact_segment_binding_reset_and_rollover() -> Checked {
    let mut state = ReputationState::new(key(2)?.market);
    state.insert(fresh(2)?)?;
    for change in 0..3 {
        let mut k = key(2)?;
        match change {
            0 => k.market = MarketId::new([99; 32])?,
            1 => k.policy = digest(99)?,
            _ => k.model = digest(99)?,
        }
        assert!(state.worker(k.worker, k).is_err());
    }
    let mut r = observed(600_000)?;
    r.status = HistoryStatus::Suspended;
    let (k, reset) = reset_segment(
        &r,
        key(2)?,
        SegmentBinding {
            config: Version::new(2)?,
            policy: digest(30)?,
            model: digest(4)?,
        },
        ClosureReason::PolicyChanged,
        Presence::Present(6),
        100,
    )?;
    assert_eq!(reset.quality.get(), 0);
    assert_eq!(reset.qualifying_count, 0);
    assert_eq!(reset.last_observed, Presence::Absent);
    assert_eq!(reset.last_applied, Presence::Present(6));
    assert_eq!(reset.status, HistoryStatus::Suspended);
    assert_eq!(k.reset_generation.get(), 2);
    assert_eq!(
        reset.previous_segment,
        Presence::Present(closure_digest(
            key(2)?.market,
            &r,
            ClosureReason::PolicyChanged
        )?)
    );
    assert_eq!(
        rollover_segment(
            &reset,
            k,
            k.config,
            k.policy,
            k.model,
            Presence::Present(6),
            101
        )?,
        Presence::Absent
    );
    assert!(matches!(
        rollover_segment(
            &reset,
            k,
            k.config,
            k.policy,
            digest(31)?,
            Presence::Present(6),
            101
        )?,
        Presence::Present(_)
    ));
    let mut wrong_owner = worker(2)?;
    wrong_owner.owner = PrincipalId::new([100; 32])?;
    assert_eq!(
        update_record(
            &r,
            key(2)?,
            frozen(7)?,
            &wrong_owner,
            EpochTally {
                median: score(1_000_000)?,
                support: 3,
                eligible: 6,
            },
            true,
            107
        ),
        Err(F07_BINDING_MISMATCH)
    );
    Ok(())
}
#[test]
fn a11_capacity_budgets_and_synchronised_retention_primitive() -> Checked {
    let mut state = ReputationState::new(key(2)?.market);
    for n in 2..34 {
        state.insert(fresh(n)?)?;
    }
    let full = state.clone();
    assert_eq!(state.insert(fresh(34)?), Err(CAPACITY));
    assert_eq!(state, full);
    for epoch in 0..32 {
        state.append(CompletedHistory {
            epoch,
            execution_height: 100 + epoch,
            config: Version::new(1)?,
            result: digest(40)?,
            root: digest(41)?,
            observed_workers: 0,
            total_workers: 32,
            covered_workers: 0,
        })?;
    }
    let size = state.encoded_len()?;
    assert_eq!(size, 8896);
    let mut bytes = vec![0; size];
    assert_eq!(encode_section(&state, &mut bytes)?, size);
    assert_eq!(decode_section(&bytes)?, state);
    check_storage_budget(size, 15_488, 172_032)?;
    assert_eq!(check_storage_budget(9089, 0, 0), Err(F07_RESOURCE_LIMIT));
    assert_eq!(
        check_storage_budget(9088, 15_489, 0),
        Err(F07_RESOURCE_LIMIT)
    );
    assert_eq!(
        check_storage_budget(9088, 15_488, 172_033),
        Err(F07_RESOURCE_LIMIT)
    );
    assert_eq!(check_storage_budget(1, usize::MAX, 0), Err(ARITHMETIC));
    let before = state.clone();
    let new = CompletedHistory {
        epoch: 32,
        execution_height: 132,
        config: Version::new(1)?,
        result: digest(42)?,
        root: digest(43)?,
        observed_workers: 0,
        total_workers: 32,
        covered_workers: 0,
    };
    assert_eq!(state.append(new), Err(RETENTION_FULL));
    assert_eq!(state, before);
    assert_eq!(state.prune_oldest(1), Err(WRONG_EPOCH));
    assert_eq!(state, before);
    // The primitive is called only after real F06 safe-prune authorisation; no fake claim rows.
    state.prune_oldest(0)?;
    state.append(new)?;
    assert_eq!(
        state.lookup_epoch(0),
        Err(HistoryLookupError::HistoryOutsideRetention)
    );
    assert_eq!(
        state
            .completed()
            .next()
            .ok_or(Failure::Unexpected("completed history"))?
            .epoch,
        1
    );
    assert_eq!(state.completed().count(), 32);
    assert_eq!(state.assess_completion(0, digest(40)?), Err(WRONG_EPOCH));
    assert_eq!(state.records().count(), 32);
    Ok(())
}
#[test]
fn epoch_zero_absence_codec_and_malformed_current() -> Checked {
    let r = apply(&fresh(2)?, 0, score(0)?, 3, 6, true)?;
    let bytes = encode_current(&r)?;
    assert_eq!(bytes.len(), 184);
    assert_eq!(bytes[181], 7);
    assert_eq!(&bytes[116..124], &[0; 8]);
    assert_eq!(decode_current(&bytes)?, r);
    assert_eq!(
        decode_current(&encode_current(&fresh(2)?)?)?.last_observed,
        Presence::Absent
    );
    for (offset, value) in [(180, 0), (180, 4), (181, 128), (182, 1), (183, 1)] {
        let mut bad = bytes;
        bad[offset] = value;
        assert!(decode_current(&bad).is_err());
    }
    let mut bad = bytes;
    bad[181] &= !1;
    assert!(decode_current(&bad).is_err());
    let mut bad = bytes;
    bad[181] &= !2;
    assert!(decode_current(&bad).is_err());
    let mut bad = bytes;
    bad[132..140].fill(0);
    assert!(decode_current(&bad).is_err());
    let mut bad = bytes;
    bad[108..112].copy_from_slice(&33u32.to_be_bytes());
    assert!(decode_current(&bad).is_err());
    let mut bad = bytes;
    bad[104..108].copy_from_slice(&1_000_001u32.to_be_bytes());
    assert!(decode_current(&bad).is_err());
    let mut bad = bytes;
    bad[0..32].fill(0);
    assert!(decode_current(&bad).is_err());
    let mut trailing = bytes.to_vec();
    trailing.push(0);
    assert!(decode_current(&trailing).is_err());
    for end in 0..184 {
        assert!(decode_current(&bytes[..end]).is_err());
    }
    Ok(())
}
#[test]
fn history_and_section_malformed_order_presence_version() -> Checked {
    let h = CompletedHistory {
        epoch: 0,
        execution_height: 1,
        config: Version::new(1)?,
        result: digest(50)?,
        root: digest(51)?,
        observed_workers: 1,
        total_workers: 1,
        covered_workers: 1,
    };
    let wire = encode_history(&h)?;
    assert_eq!(wire.len(), 92);
    assert_eq!(decode_history(&wire)?, h);
    for (offset, value) in [(88, 2), (89, 33), (90, 2), (91, 1)] {
        let mut bad = wire;
        bad[offset] = value;
        assert!(decode_history(&bad).is_err());
    }
    let mut state = ReputationState::new(key(2)?.market);
    state.insert(fresh(2)?)?;
    state.insert(fresh(3)?)?;
    let mut bytes = vec![0; state.encoded_len()?];
    encode_section(&state, &mut bytes)?;
    for (offset, value) in [(5, 2), (6, 33), (40, 2), (49, 1), (48, 1)] {
        let mut bad = bytes.clone();
        bad[offset] = value;
        assert!(decode_section(&bad).is_err());
    }
    let mut duplicate = bytes.clone();
    duplicate[248..432].copy_from_slice(&bytes[64..248]);
    assert!(decode_section(&duplicate).is_err());
    let mut descending = bytes.clone();
    descending[64..248].copy_from_slice(&bytes[248..432]);
    descending[248..432].copy_from_slice(&bytes[64..248]);
    assert!(decode_section(&descending).is_err());
    bytes.push(0);
    assert!(decode_section(&bytes).is_err());
    Ok(())
}
#[test]
fn canonical_digest_inputs_domain_status_and_order() -> Checked {
    let k = key(2)?;
    let mut canonical = b"PAXAI/reputation-segment/v1\0".to_vec();
    canonical.extend_from_slice(&[1; 32]);
    canonical.extend_from_slice(&[2; 32]);
    canonical.extend_from_slice(&[0, 0, 0, 0, 0, 0, 0, 1]);
    canonical.extend_from_slice(&[3; 32]);
    canonical.extend_from_slice(&[4; 32]);
    canonical.extend_from_slice(&[0, 0, 0, 0, 0, 0, 0, 1]);
    assert_eq!(
        segment_digest(k)?.bytes(),
        <[u8; 32]>::from(Sha256::digest(&canonical))
    );
    let r = fresh(2)?;
    let wire = encode_current(&r)?;
    let mut root_bytes = b"PAXAI/reputation-root/v1\0".to_vec();
    root_bytes.extend_from_slice(&[1; 32]);
    root_bytes.extend_from_slice(&[0; 8]);
    root_bytes.extend_from_slice(&[0, 1]);
    root_bytes.extend_from_slice(&wire);
    let root = reputation_root(k.market, 0, &[r])?;
    assert_eq!(root.bytes(), <[u8; 32]>::from(Sha256::digest(&root_bytes)));
    let mut suspended = r;
    suspended.status = HistoryStatus::Suspended;
    assert_ne!(root, reputation_root(k.market, 0, &[suspended])?);
    let mut applied0 = r;
    applied0.last_applied = Presence::Present(0);
    assert_ne!(root, reputation_root(k.market, 0, &[applied0])?);
    assert_eq!(reputation_root(k.market, 0, &[r, r]), Err(NON_CANONICAL));
    assert_eq!(
        reputation_root(k.market, 0, &[fresh(3)?, fresh(2)?]),
        Err(NON_CANONICAL)
    );
    let mut closure = b"PAXAI/reputation-close/v1\0".to_vec();
    closure.extend_from_slice(&[1; 32]);
    closure.extend_from_slice(&[2; 32]);
    closure.extend_from_slice(r.segment.as_bytes());
    closure.extend_from_slice(&[0, 0, 0, 0, 0, 0, 0, 1]);
    closure.extend_from_slice(&[0; 12]);
    closure.extend_from_slice(&[0; 8]);
    closure.push(1);
    closure.extend_from_slice(&[0; 32]);
    assert_eq!(
        closure_digest(k.market, &r, ClosureReason::OwnerReset)?.bytes(),
        <[u8; 32]>::from(Sha256::digest(&closure))
    );
    assert_ne!(
        closure_digest(k.market, &r, ClosureReason::OwnerReset)?,
        closure_digest(k.market, &r, ClosureReason::AdminIntegrity)?
    );
    assert!(ClosureReason::decode(0).is_err());
    assert!(ClosureReason::decode(5).is_err());
    Ok(())
}
