// Pure source vectors; these do not qualify unavailable F05/F06 or native wiring.
pub use layerx_programs_ai_market::{codec, errors, types};
#[path = "../src/reputation.rs"]
mod reputation;
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

fn digest(n: u8) -> Digest32 {
    Digest32::new([n; 32]).unwrap()
}
fn key(n: u8) -> SegmentKey {
    SegmentKey {
        market: MarketId::new([1; 32]).unwrap(),
        worker: WorkerId::new([n; 32]).unwrap(),
        config: Version::new(1).unwrap(),
        policy: digest(3),
        model: digest(4),
        reset_generation: Version::new(1).unwrap(),
    }
}
fn worker(n: u8) -> WorkerRosterEntry {
    WorkerRosterEntry {
        worker: key(n).worker,
        owner: PrincipalId::new([9; 32]).unwrap(),
        recipient: AccountId::new([10; 32]).unwrap(),
        generation: Version::new(1).unwrap(),
        key_version: Version::new(1).unwrap(),
        public_key: PublicKey32([11; 32]),
        metadata: MetadataDigest::new([12; 32]).unwrap(),
    }
}
fn frozen(epoch: u64) -> FrozenBinding {
    FrozenBinding {
        chain: ChainDomain::new([13; 32]).unwrap(),
        program: ProgramId::new([14; 32]).unwrap(),
        market: key(2).market,
        epoch,
        config: Version::new(1).unwrap(),
        roster: RosterDigest::new([15; 32]).unwrap(),
    }
}
fn fresh(n: u8) -> ReputationCurrent {
    ReputationCurrent::bootstrap(key(n), worker(n).owner, 1).unwrap()
}
fn observed(q: u32) -> ReputationCurrent {
    let mut r = fresh(2);
    r.quality = Score::new(q).unwrap();
    r.qualifying_count = 1;
    r.last_applied = Presence::Present(6);
    r.last_observed = Presence::Present(Observation {
        epoch: 6,
        height: 90,
    });
    r.last_transition_height = 90;
    r
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
        key(2),
        frozen(epoch),
        &worker(2),
        median,
        n,
        e,
        allowed,
        100 + epoch,
    )
}
fn score(n: u32) -> Presence<Score> {
    Presence::Present(Score::new(n).unwrap())
}
fn complete(
    state: &ReputationState,
    epoch: u64,
    result: Digest32,
    median: Presence<Score>,
    n: u8,
) -> CodecResult<(ReputationState, CompletedHistory)> {
    match CompletionDraft::begin(state, frozen(epoch), result, 200 + epoch, &[worker(2)])? {
        CompletionStart::Pending(mut draft) => {
            draft.apply_worker(key(2), median, n, 6, true)?;
            draft.finish()
        }
        CompletionStart::Retained(r) => Ok((state.clone(), r)),
    }
}
#[test]
fn a01_continuity_and_count() {
    let first = apply(&fresh(2), 7, score(800000), 4, 6, true).unwrap();
    assert_eq!(first.quality.get(), 100000);
    assert_eq!(first.qualifying_count, 1);
    assert_eq!(first.confidence().unwrap().get(), 125000);
    let mut rotated = worker(2);
    rotated.key_version = Version::new(2).unwrap();
    rotated.public_key = PublicKey32([22; 32]);
    let second = update_record(
        &first,
        key(2),
        frozen(8),
        &rotated,
        score(800000),
        4,
        6,
        true,
        108,
    )
    .unwrap();
    assert_eq!(second.quality.get(), 187500);
    assert_eq!(second.confidence().unwrap().get(), 250000);
    assert_eq!(first.segment, second.segment);
    assert_eq!(second.reset_generation.get(), 1);
}
#[test]
fn a02_floor_and_a03_zero_is_observed() {
    assert_eq!(
        apply(&observed(123457), 7, score(765433), 3, 6, true)
            .unwrap()
            .quality
            .get(),
        203704
    );
    assert_eq!(
        apply(&observed(123457), 7, Presence::Absent, 0, 6, true)
            .unwrap()
            .quality
            .get(),
        121527
    );
    let zero = apply(&observed(800000), 7, score(0), 3, 6, true).unwrap();
    let missing = apply(&observed(800000), 7, Presence::Absent, 0, 6, true).unwrap();
    assert_eq!(zero.quality.get(), 700000);
    assert_eq!(zero.qualifying_count, 2);
    assert_eq!(missing.quality.get(), 787500);
    assert_eq!(missing.qualifying_count, 1);
    assert_eq!(
        zero.last_observed,
        Presence::Present(Observation {
            epoch: 7,
            height: 107
        })
    );
    assert_eq!(missing.last_observed, observed(800000).last_observed);
    assert_eq!(
        apply(&fresh(2), 0, Presence::Absent, 0, 0, true)
            .unwrap()
            .quality
            .get(),
        0
    );
}
#[test]
fn a04_bounds_saturation_and_generation_overflow() {
    let mut r = observed(1000000);
    r.qualifying_count = 31;
    let r = apply(&r, 7, score(1000000), 3, 6, true).unwrap();
    assert_eq!(r.quality.get(), 1000000);
    assert_eq!(r.qualifying_count, 32);
    assert_eq!(
        apply(&r, 8, score(1000000), 3, 6, true)
            .unwrap()
            .qualifying_count,
        32
    );
    assert_eq!(confidence(32).unwrap().get(), 1000000);
    assert_eq!(Score::new(1000001), Err(F03_SCORE_RANGE));
    assert_eq!(next_count(33, true), Err(NON_CANONICAL));
    let mut k = key(2);
    k.reset_generation = Version::new(u64::MAX).unwrap();
    let mut r = fresh(2);
    r.reset_generation = k.reset_generation;
    r.segment = segment_digest(k).unwrap();
    r.previous_segment = Presence::Present(digest(19));
    let bytes = encode_current(&r).unwrap();
    assert_eq!(
        reset_segment(
            &r,
            k,
            k.config,
            k.policy,
            k.model,
            ClosureReason::OwnerReset,
            Presence::Absent,
            2
        ),
        Err(ARITHMETIC)
    );
    assert_eq!(encode_current(&r).unwrap(), bytes);
}
#[test]
fn a05_independent_coverage_and_sealed_history_permission() {
    assert_eq!(evidence_coverage(6, 4).unwrap(), score(666666));
    let insufficient = apply(&observed(800000), 7, Presence::Absent, 2, 8, true).unwrap();
    assert_eq!(insufficient.coverage, score(250000));
    assert_eq!(insufficient.quality.get(), 787500);
    assert_eq!(evidence_coverage(0, 0).unwrap(), Presence::Absent);
    assert_eq!(evidence_coverage(0, 1), Err(F07_BINDING_MISMATCH));
    assert_eq!(evidence_coverage(9, 1), Err(F07_BINDING_MISMATCH));
    let mut suspended = observed(800000);
    suspended.status = HistoryStatus::Suspended;
    assert_eq!(
        apply(&suspended, 7, score(0), 3, 6, true)
            .unwrap()
            .quality
            .get(),
        700000
    );
    let disallowed = apply(&observed(800000), 7, score(800000), 4, 6, false).unwrap();
    assert_eq!(disallowed.quality.get(), 787500);
    assert_eq!(disallowed.coverage, score(666666));
    assert_eq!(
        apply(&fresh(2), 7, score(1), 2, 8, true),
        Err(F07_BINDING_MISMATCH)
    );
    // Evaluator deduplication/owner rejection belong to the missing real producer bridge.
}
#[test]
fn a06_internal_staging_retry_order_and_gaps() {
    let mut state = ReputationState::new(key(2).market);
    let mut r = observed(100000);
    r.last_applied = Presence::Present(6);
    state.completed_through = Presence::Present(6);
    state.insert(r).unwrap();
    let before = state.clone();
    let (next, receipt) = complete(&state, 12, digest(20), score(800000), 4).unwrap();
    assert_eq!(state, before);
    assert_eq!(next.records().next().unwrap().quality.get(), 187500);
    assert_eq!(next.completed().count(), 1);
    let (retry, retained) = complete(&next, 12, digest(20), score(800000), 4).unwrap();
    assert_eq!(retry, next);
    assert_eq!(retained, receipt);
    assert_eq!(next.assess_completion(12, digest(21)), Err(CONFLICT));
    assert_eq!(next.assess_completion(11, digest(20)), Err(WRONG_EPOCH));
    let (gap, _) = complete(&next, 22, digest(22), Presence::Absent, 0).unwrap();
    assert_eq!(gap.records().next().unwrap().quality.get(), 184570);
    assert_eq!(gap.completed().count(), 2);
    let mut draft =
        match CompletionDraft::begin(&next, frozen(13), digest(23), 213, &[worker(2)]).unwrap() {
            CompletionStart::Pending(d) => d,
            CompletionStart::Retained(_) => panic!("new epoch must stage"),
        };
    assert_eq!(
        draft.apply_worker(key(2), score(1), 2, 6, true),
        Err(F07_BINDING_MISMATCH)
    );
    assert!(matches!(draft.finish(), Err(F07_EPOCH_NOT_SEALED)));
    assert_eq!(next.records().next().unwrap().quality.get(), 187500);
}
#[test]
fn staged_multiworker_failure_cannot_publish_partial_history() {
    let mut state = ReputationState::new(key(2).market);
    state.insert(fresh(2)).unwrap();
    state.insert(fresh(3)).unwrap();
    let before = state.clone();
    let mut draft =
        match CompletionDraft::begin(&state, frozen(1), digest(60), 201, &[worker(2), worker(3)])
            .unwrap()
        {
            CompletionStart::Pending(d) => d,
            CompletionStart::Retained(_) => panic!("new epoch must stage"),
        };
    draft
        .apply_worker(key(2), score(800000), 3, 6, true)
        .unwrap();
    assert_eq!(
        draft.apply_worker(key(3), score(800000), 2, 6, true),
        Err(F07_BINDING_MISMATCH)
    );
    assert!(matches!(draft.finish(), Err(F07_EPOCH_NOT_SEALED)));
    assert_eq!(state, before);
    assert_eq!(state.completed().count(), 0);
    assert!(state
        .records()
        .all(|r| r.quality.get() == 0 && r.last_applied == Presence::Absent));
}
#[test]
fn a10_exact_segment_binding_reset_and_rollover() {
    let mut state = ReputationState::new(key(2).market);
    state.insert(fresh(2)).unwrap();
    for change in 0..3 {
        let mut k = key(2);
        match change {
            0 => k.market = MarketId::new([99; 32]).unwrap(),
            1 => k.policy = digest(99),
            _ => k.model = digest(99),
        }
        assert!(state.worker(k.worker, k).is_err());
    }
    let mut r = observed(600000);
    r.status = HistoryStatus::Suspended;
    let (k, reset) = reset_segment(
        &r,
        key(2),
        Version::new(2).unwrap(),
        digest(30),
        digest(4),
        ClosureReason::PolicyChanged,
        Presence::Present(6),
        100,
    )
    .unwrap();
    assert_eq!(reset.quality.get(), 0);
    assert_eq!(reset.qualifying_count, 0);
    assert_eq!(reset.last_observed, Presence::Absent);
    assert_eq!(reset.last_applied, Presence::Present(6));
    assert_eq!(reset.status, HistoryStatus::Suspended);
    assert_eq!(k.reset_generation.get(), 2);
    assert_eq!(
        reset.previous_segment,
        Presence::Present(closure_digest(key(2).market, &r, ClosureReason::PolicyChanged).unwrap())
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
        )
        .unwrap(),
        Presence::Absent
    );
    assert!(matches!(
        rollover_segment(
            &reset,
            k,
            k.config,
            k.policy,
            digest(31),
            Presence::Present(6),
            101
        )
        .unwrap(),
        Presence::Present(_)
    ));
    let mut wrong_owner = worker(2);
    wrong_owner.owner = PrincipalId::new([100; 32]).unwrap();
    assert_eq!(
        update_record(
            &r,
            key(2),
            frozen(7),
            &wrong_owner,
            score(1000000),
            3,
            6,
            true,
            107
        ),
        Err(F07_BINDING_MISMATCH)
    );
}
#[test]
fn a11_capacity_budgets_and_synchronised_retention_primitive() {
    let mut state = ReputationState::new(key(2).market);
    for n in 2..34 {
        state.insert(fresh(n)).unwrap();
    }
    let full = state.clone();
    assert_eq!(state.insert(fresh(34)), Err(CAPACITY));
    assert_eq!(state, full);
    for epoch in 0..32 {
        state
            .append(CompletedHistory {
                epoch,
                execution_height: 100 + epoch,
                config: Version::new(1).unwrap(),
                result: digest(40),
                root: digest(41),
                observed_workers: 0,
                total_workers: 32,
                covered_workers: 0,
            })
            .unwrap();
    }
    let size = state.encoded_len().unwrap();
    assert_eq!(size, 8896);
    let mut bytes = vec![0; size];
    assert_eq!(encode_section(&state, &mut bytes).unwrap(), size);
    assert_eq!(decode_section(&bytes).unwrap(), state);
    check_storage_budget(size, 15488, 172032).unwrap();
    assert_eq!(check_storage_budget(9089, 0, 0), Err(F07_RESOURCE_LIMIT));
    assert_eq!(
        check_storage_budget(9088, 15489, 0),
        Err(F07_RESOURCE_LIMIT)
    );
    assert_eq!(
        check_storage_budget(9088, 15488, 172033),
        Err(F07_RESOURCE_LIMIT)
    );
    assert_eq!(check_storage_budget(1, usize::MAX, 0), Err(ARITHMETIC));
    let before = state.clone();
    let new = CompletedHistory {
        epoch: 32,
        execution_height: 132,
        config: Version::new(1).unwrap(),
        result: digest(42),
        root: digest(43),
        observed_workers: 0,
        total_workers: 32,
        covered_workers: 0,
    };
    assert_eq!(state.append(new), Err(RETENTION_FULL));
    assert_eq!(state, before);
    assert_eq!(state.prune_oldest(1), Err(WRONG_EPOCH));
    assert_eq!(state, before);
    // The primitive is called only after real F06 safe-prune authorisation; no fake claim rows.
    state.prune_oldest(0).unwrap();
    state.append(new).unwrap();
    assert_eq!(
        state.lookup_epoch(0),
        Err(HistoryLookupError::HistoryOutsideRetention)
    );
    assert_eq!(state.completed().next().unwrap().epoch, 1);
    assert_eq!(state.completed().count(), 32);
    assert_eq!(state.assess_completion(0, digest(40)), Err(WRONG_EPOCH));
    assert_eq!(state.records().count(), 32);
}
#[test]
fn epoch_zero_absence_codec_and_malformed_current() {
    let r = apply(&fresh(2), 0, score(0), 3, 6, true).unwrap();
    let bytes = encode_current(&r).unwrap();
    assert_eq!(bytes.len(), 184);
    assert_eq!(bytes[181], 7);
    assert_eq!(&bytes[116..124], &[0; 8]);
    assert_eq!(decode_current(&bytes).unwrap(), r);
    assert_eq!(
        decode_current(&encode_current(&fresh(2)).unwrap())
            .unwrap()
            .last_observed,
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
    bad[104..108].copy_from_slice(&1000001u32.to_be_bytes());
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
}
#[test]
fn history_and_section_malformed_order_presence_version() {
    let h = CompletedHistory {
        epoch: 0,
        execution_height: 1,
        config: Version::new(1).unwrap(),
        result: digest(50),
        root: digest(51),
        observed_workers: 1,
        total_workers: 1,
        covered_workers: 1,
    };
    let wire = encode_history(&h).unwrap();
    assert_eq!(wire.len(), 92);
    assert_eq!(decode_history(&wire).unwrap(), h);
    for (offset, value) in [(88, 2), (89, 33), (90, 2), (91, 1)] {
        let mut bad = wire;
        bad[offset] = value;
        assert!(decode_history(&bad).is_err());
    }
    let mut state = ReputationState::new(key(2).market);
    state.insert(fresh(2)).unwrap();
    state.insert(fresh(3)).unwrap();
    let mut bytes = vec![0; state.encoded_len().unwrap()];
    encode_section(&state, &mut bytes).unwrap();
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
}
#[test]
fn canonical_digest_inputs_domain_status_and_order() {
    let k = key(2);
    let mut canonical = b"PAXAI/reputation-segment/v1\0".to_vec();
    canonical.extend_from_slice(&[1; 32]);
    canonical.extend_from_slice(&[2; 32]);
    canonical.extend_from_slice(&[0, 0, 0, 0, 0, 0, 0, 1]);
    canonical.extend_from_slice(&[3; 32]);
    canonical.extend_from_slice(&[4; 32]);
    canonical.extend_from_slice(&[0, 0, 0, 0, 0, 0, 0, 1]);
    assert_eq!(
        segment_digest(k).unwrap().bytes(),
        <[u8; 32]>::from(Sha256::digest(&canonical))
    );
    let r = fresh(2);
    let wire = encode_current(&r).unwrap();
    let mut root_bytes = b"PAXAI/reputation-root/v1\0".to_vec();
    root_bytes.extend_from_slice(&[1; 32]);
    root_bytes.extend_from_slice(&[0; 8]);
    root_bytes.extend_from_slice(&[0, 1]);
    root_bytes.extend_from_slice(&wire);
    let root = reputation_root(k.market, 0, &[r]).unwrap();
    assert_eq!(root.bytes(), <[u8; 32]>::from(Sha256::digest(&root_bytes)));
    let mut suspended = r;
    suspended.status = HistoryStatus::Suspended;
    assert_ne!(root, reputation_root(k.market, 0, &[suspended]).unwrap());
    let mut applied0 = r;
    applied0.last_applied = Presence::Present(0);
    assert_ne!(root, reputation_root(k.market, 0, &[applied0]).unwrap());
    assert_eq!(reputation_root(k.market, 0, &[r, r]), Err(NON_CANONICAL));
    assert_eq!(
        reputation_root(k.market, 0, &[fresh(3), fresh(2)]),
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
        closure_digest(k.market, &r, ClosureReason::OwnerReset)
            .unwrap()
            .bytes(),
        <[u8; 32]>::from(Sha256::digest(&closure))
    );
    assert_ne!(
        closure_digest(k.market, &r, ClosureReason::OwnerReset).unwrap(),
        closure_digest(k.market, &r, ClosureReason::AdminIntegrity).unwrap()
    );
    assert!(ClosureReason::decode(0).is_err());
    assert!(ClosureReason::decode(5).is_err());
}
