//! AI.F05-A05/A08/A10/A11 structural codec gate. No median implementation,
//! admission authority, signatures, runtime effects or terminalization are mocked.
use layerx_programs_ai_market::{aggregation_codec::*, codec::*, errors::*, *};
use sha2::{Digest, Sha256};

fn version() -> Version {
    Version::new(1).unwrap()
}
fn worker(id: u8) -> WorkerRosterEntry {
    WorkerRosterEntry {
        worker: WorkerId::new([id; 32]).unwrap(),
        owner: PrincipalId::new([40 + id; 32]).unwrap(),
        recipient: AccountId::new([90 + id; 32]).unwrap(),
        generation: version(),
        key_version: version(),
        public_key: PublicKey32([id; 32]),
        metadata: MetadataDigest::new([1; 32]).unwrap(),
    }
}
fn evaluator(id: u8) -> EvaluatorRosterEntry {
    EvaluatorRosterEntry {
        evaluator: EvaluatorId::new([150 + id; 32]).unwrap(),
        owner: PrincipalId::new([200 + id; 32]).unwrap(),
        grant: version(),
        key_version: version(),
        public_key: PublicKey32([150 + id; 32]),
        rubric: RubricDigest::new([9; 32]).unwrap(),
    }
}
fn roster<'a>(
    workers: &'a [WorkerRosterEntry],
    evaluators: &'a [EvaluatorRosterEntry],
) -> Roster<'a> {
    Roster {
        market: MarketId::new([3; 32]).unwrap(),
        epoch: 0,
        config: version(),
        workers,
        evaluators,
    }
}
fn binding(roster: &Roster<'_>) -> FrozenBinding {
    FrozenBinding {
        chain: ChainDomain::new([1; 32]).unwrap(),
        program: ProgramId::new([2; 32]).unwrap(),
        market: roster.market,
        epoch: roster.epoch,
        config: roster.config,
        roster: roster_digest(roster).unwrap(),
    }
}
fn report<'a>(
    b: FrozenBinding,
    e: EvaluatorRosterEntry,
    scores: &'a [ScoreEntry],
) -> ReportBody<'a> {
    ReportBody {
        binding: EvaluatorBinding {
            frozen: b,
            evaluator: e.evaluator,
            grant: e.grant,
            key_version: e.key_version,
        },
        evidence: EvidenceRoot::new([8; 32]).unwrap(),
        scores: ScoreVector::Typed(scores),
    }
}
fn cell(worker: WorkerId, value: u32) -> ScoreEntry {
    ScoreEntry {
        worker,
        score: Score::new(value).unwrap(),
    }
}
fn independent_hash(domain: &[u8], bytes: &[u8]) -> [u8; 32] {
    let mut h = Sha256::new();
    h.update(domain);
    h.update([0]);
    h.update(bytes);
    h.finalize().into()
}
fn header(b: FrozenBinding) -> Vec<u8> {
    let mut out = Vec::new();
    out.extend_from_slice(&1u16.to_be_bytes());
    out.extend_from_slice(b.chain.as_bytes());
    out.extend_from_slice(b.program.as_bytes());
    out.extend_from_slice(b.market.as_bytes());
    out.extend_from_slice(&b.epoch.to_be_bytes());
    out.extend_from_slice(&b.config.get().to_be_bytes());
    out.extend_from_slice(b.roster.as_bytes());
    out
}
fn commitments(reports: &[ReportBody<'_>]) -> Vec<ReportCommitment> {
    reports
        .iter()
        .map(|r| ReportCommitment {
            evaluator: r.binding.evaluator,
            report: report_digest(r).unwrap(),
        })
        .collect()
}

#[test]
fn aggregation_codec_contract() {
    // A05: real common report constructors and encoder, six arrival permutations.
    let workers = [worker(1)];
    let evaluators = [evaluator(1), evaluator(2), evaluator(3)];
    let r = roster(&workers, &evaluators);
    let b = binding(&r);
    let scores = [
        [cell(workers[0].worker, 111111)],
        [cell(workers[0].worker, 111111)],
        [cell(workers[0].worker, 999999)],
    ];
    let typed: Vec<_> = evaluators
        .iter()
        .zip(&scores)
        .map(|(e, s)| report(b, *e, s))
        .collect();
    let encoded: Vec<Vec<u8>> = typed
        .iter()
        .map(|r| {
            let mut bytes = vec![0; REPORT_MAX_BYTES];
            let n = encode_report(r, &mut bytes).unwrap();
            bytes.truncate(n);
            bytes
        })
        .collect();
    let reports: Vec<_> = encoded
        .iter()
        .map(|v| AggregationInputView::persisted_report(v).unwrap())
        .collect();
    let view = AggregationInputView::structural(b, r, &reports).unwrap();
    assert_eq!(view.support(workers[0].worker).unwrap(), 3);
    let votes = view.worker_votes(workers[0].worker, version()).unwrap();
    assert_eq!(votes.len(), 3);
    assert_eq!(votes.vote(0).unwrap().score.get(), 111111);
    assert_eq!(votes.vote(2).unwrap().score.get(), 999999);
    assert!(view
        .worker_votes(workers[0].worker, Version::new(2).unwrap())
        .is_err());
    let rows = commitments(&reports);
    let digest = input_digest(b, 96, &rows).unwrap();
    let mut expected_input = header(b);
    expected_input.extend_from_slice(&96u64.to_be_bytes());
    expected_input.extend_from_slice(&3u16.to_be_bytes());
    for row in &rows {
        expected_input.extend_from_slice(row.evaluator.as_bytes());
        expected_input.extend_from_slice(row.report.as_bytes());
    }
    let mut actual = [0; INPUT_PREIMAGE_MAX_BYTES];
    let n = encode_input_preimage(b, 96, &rows, &mut actual).unwrap();
    assert_eq!(&actual[..n], expected_input.as_slice());
    assert_eq!(
        digest.bytes(),
        independent_hash(b"PAXAI/aggregation-input/v1", &expected_input)
    );
    let input_view = decode_input_preimage(&actual[..n], b, digest).unwrap();
    assert_eq!(input_view.len(), 3);
    assert_eq!(input_view.seal_height(), 96);
    assert_eq!(input_view.row(0).unwrap(), rows[0]);
    for length in 0..n {
        assert!(decode_input_preimage(&actual[..length], b, digest).is_err());
    }
    let mut bad_input = actual[..n].to_vec();
    bad_input.push(0);
    assert!(decode_input_preimage(&bad_input, b, digest).is_err());
    // Expected numerical record is a codec fixture; T02 must compute it later.
    let outputs = [WorkerAggregate::new(
        workers[0].worker,
        version(),
        3,
        QualityStatus::ScoredPositive,
        111111,
        111111,
    )
    .unwrap()];
    let epoch = EpochAggregation::structural(b, digest, &workers, &outputs).unwrap();
    let mut expected_output = header(b);
    expected_output.extend_from_slice(digest.as_bytes());
    expected_output.extend_from_slice(&1u16.to_be_bytes());
    expected_output.extend_from_slice(workers[0].worker.as_bytes());
    expected_output.extend_from_slice(&1u64.to_be_bytes());
    expected_output.extend_from_slice(&[3, 2]);
    expected_output.extend_from_slice(&111111u32.to_be_bytes());
    expected_output.extend_from_slice(&111111u32.to_be_bytes());
    expected_output.extend_from_slice(&111111u64.to_be_bytes());
    let mut output_bytes = [0; OUTPUT_PREIMAGE_MAX_BYTES];
    let output_n = epoch.encode_preimage(&mut output_bytes).unwrap();
    assert_eq!(&output_bytes[..output_n], expected_output.as_slice());
    assert_eq!(
        epoch.root().bytes(),
        independent_hash(b"PAXAI/aggregation-output/v1", &expected_output)
    );
    let decoded_epoch =
        decode_epoch_preimage(&output_bytes[..output_n], b, &workers, epoch.root()).unwrap();
    assert_eq!(decoded_epoch.output(0).unwrap(), outputs[0]);
    for permutation in [
        [0, 1, 2],
        [0, 2, 1],
        [1, 0, 2],
        [1, 2, 0],
        [2, 0, 1],
        [2, 1, 0],
    ] {
        let mut arrival = permutation.map(|i| rows[i]);
        canonicalize_arrivals(&mut arrival).unwrap();
        assert_eq!(arrival.as_slice(), rows.as_slice());
        assert_eq!(input_digest(b, 96, &arrival).unwrap(), digest);
        let result = EpochAggregation::structural(b, digest, &workers, &outputs).unwrap();
        let mut bytes = [0; OUTPUT_PREIMAGE_MAX_BYTES];
        let n = result.encode_preimage(&mut bytes).unwrap();
        assert_eq!(&bytes[..n], &output_bytes[..output_n]);
        assert_eq!(result.root(), epoch.root());
    }
    let later = input_digest(b, 97, &rows).unwrap();
    assert_ne!(later, digest);
    let later_epoch = EpochAggregation::structural(b, later, &workers, &outputs).unwrap();
    assert_ne!(later_epoch.root(), epoch.root());
    assert_eq!(later_epoch.outputs(), epoch.outputs());

    // Absent quality is distinct from measured zero; explicit report zero counts.
    let absent = WorkerAggregate::new(
        workers[0].worker,
        version(),
        2,
        QualityStatus::InsufficientQuorum,
        0,
        0,
    )
    .unwrap();
    let zero = WorkerAggregate::new(
        workers[0].worker,
        version(),
        3,
        QualityStatus::ScoredZero,
        0,
        0,
    )
    .unwrap();
    assert_eq!(absent.quality(), Presence::Absent);
    assert_eq!(zero.quality(), Presence::Present(Score::new(0).unwrap()));
    let zero_cells = [cell(workers[0].worker, 0)];
    let zero_reports = [report(b, evaluators[0], &zero_cells)];
    assert_eq!(
        AggregationInputView::structural(b, r, &zero_reports)
            .unwrap()
            .support(workers[0].worker)
            .unwrap(),
        1
    );
    let zero_view = AggregationInputView::structural(b, r, &zero_reports).unwrap();
    assert_eq!(
        zero_view
            .worker_votes(workers[0].worker, version())
            .unwrap()
            .vote(0)
            .unwrap()
            .score
            .get(),
        0
    );
    assert_eq!(
        AggregationInputView::structural(b, r, &[])
            .unwrap()
            .support(workers[0].worker)
            .unwrap(),
        0
    );
    for status in [
        QualityStatus::InsufficientQuorum,
        QualityStatus::ScoredZero,
        QualityStatus::ScoredPositive,
    ] {
        assert!(WorkerAggregate::new(workers[0].worker, version(), 9, status, 0, 0).is_err());
    }
    for (support, status, score, weight) in [
        (3, QualityStatus::InsufficientQuorum, 0, 0),
        (2, QualityStatus::ScoredZero, 0, 0),
        (3, QualityStatus::ScoredPositive, 0, 0),
        (3, QualityStatus::ScoredPositive, 1, 2),
        (1, QualityStatus::InsufficientQuorum, 1, 0),
    ] {
        assert!(
            WorkerAggregate::new(workers[0].worker, version(), support, status, score, weight)
                .is_err()
        );
    }
    let mut wb = [0; 50];
    encode_worker_aggregate(outputs[0], &mut wb).unwrap();
    for record in [absent, zero] {
        let mut bytes = [0; 50];
        encode_worker_aggregate(record, &mut bytes).unwrap();
        assert_eq!(
            decode_worker_aggregate(&bytes).unwrap().quality(),
            record.quality()
        );
    }
    assert_eq!(decode_worker_aggregate(&wb).unwrap(), outputs[0]);
    let mut bad = wb;
    bad[41] = 3;
    assert!(decode_worker_aggregate(&bad).is_err());
    let mut bad = wb;
    bad[42..46].copy_from_slice(&1000001u32.to_be_bytes());
    assert!(decode_worker_aggregate(&bad).is_err());
    let mut bad = wb;
    bad[32..40].fill(0);
    assert!(decode_worker_aggregate(&bad).is_err());
    for n in 0..50 {
        assert!(decode_worker_aggregate(&wb[..n]).is_err());
    }
    let mut trailing = wb.to_vec();
    trailing.push(0);
    assert!(decode_worker_aggregate(&trailing).is_err());

    // Exact phase/presence/cursor/sum/seal/current-history structure.
    let processing = CurrentAggregation {
        phase: AggregationPhase::Processing,
        seal_height: 96,
        input: Presence::Present(digest),
        cursor: 1,
        reports: &rows,
        outputs: &outputs,
        running_weight: 111111,
        root: Presence::Absent,
    };
    processing.validate_inputs(&view).unwrap();
    processing.validate_seal_window(b, 0).unwrap();
    assert_eq!(
        CurrentAggregation {
            seal_height: 95,
            ..processing
        }
        .validate_seal_window(b, 0),
        Err(WRONG_PHASE)
    );
    let terminal = CurrentAggregation {
        phase: AggregationPhase::Terminal,
        root: Presence::Present(epoch.root()),
        ..processing
    };
    let mut state_bytes = [0; CURRENT_MAX_BYTES];
    let state_n = encode_current(&terminal, b, &workers, &mut state_bytes).unwrap();
    let decoded = decode_current(&state_bytes[..state_n], b, &workers).unwrap();
    decoded.validate_inputs(&view).unwrap();
    assert_eq!(decoded.phase(), AggregationPhase::Terminal);
    assert_eq!(decoded.cursor(), 1);
    assert_eq!(decoded.output(0).unwrap(), outputs[0]);
    assert!(decoded.output(1).is_err());
    let mut roundtrip = [0; CURRENT_MAX_BYTES];
    assert_eq!(
        decoded.encode(b, &workers, &mut roundtrip).unwrap(),
        state_n
    );
    assert_eq!(&roundtrip[..state_n], &state_bytes[..state_n]);
    for n in 0..state_n {
        assert!(decode_current(&state_bytes[..n], b, &workers).is_err());
    }
    let mut bad = state_bytes[..state_n].to_vec();
    bad.push(0);
    assert!(decode_current(&bad, b, &workers).is_err());
    let mut bad = state_bytes;
    bad[0] = 3;
    assert!(decode_current(&bad[..state_n], b, &workers).is_err());
    let mut bad = state_bytes;
    bad[41..43].copy_from_slice(&0u16.to_be_bytes());
    assert!(decode_current(&bad[..state_n], b, &workers).is_err());
    assert!(CurrentAggregation {
        running_weight: 1,
        ..processing
    }
    .validate(b, &workers)
    .is_err());
    assert!(CurrentAggregation {
        root: Presence::Present(epoch.root()),
        ..processing
    }
    .validate(b, &workers)
    .is_err());
    assert!(CurrentAggregation {
        input: Presence::Absent,
        ..processing
    }
    .validate(b, &workers)
    .is_err());
    assert!(CurrentAggregation {
        phase: AggregationPhase::Unsealed,
        ..processing
    }
    .validate(b, &workers)
    .is_err());
    let unsealed = CurrentAggregation {
        phase: AggregationPhase::Unsealed,
        seal_height: 0,
        input: Presence::Absent,
        cursor: 0,
        reports: &[],
        outputs: &[],
        running_weight: 0,
        root: Presence::Absent,
    };
    assert_eq!(
        encode_current(&unsealed, b, &workers, &mut roundtrip).unwrap(),
        56
    );
    decode_current(&roundtrip[..56], b, &workers).unwrap();
    let summary = HistorySummary {
        epoch: b.epoch,
        config: b.config,
        roster: b.roster,
        input: digest,
        root: epoch.root(),
    };
    validate_current_history(&terminal, b, &workers, &[summary]).unwrap();
    assert!(validate_current_history(&terminal, b, &workers, &[]).is_err());
    assert!(validate_current_history(&processing, b, &workers, &[summary]).is_err());

    // A10: stable identity duplicates, corrupted ordering, frozen versions/domains.
    let mut duplicate = [rows[0], rows[0]];
    assert!(canonicalize_arrivals(&mut duplicate).is_err());
    let reversed = [rows[1], rows[0]];
    assert!(input_digest(b, 96, &reversed).is_err());
    let duplicate_reports = [reports[0], reports[0]];
    assert_eq!(
        AggregationInputView::structural(b, r, &duplicate_reports).unwrap_err(),
        F05_REPORT_INVARIANT
    );
    let mut two_keys = reports[0];
    two_keys.binding.key_version = Version::new(2).unwrap();
    assert_eq!(
        AggregationInputView::structural(b, r, &[reports[0], two_keys]).unwrap_err(),
        F05_REPORT_INVARIANT
    );
    assert_eq!(
        AggregationInputView::structural(b, r, &[two_keys]).unwrap_err(),
        F05_REPORT_INVARIANT
    );
    assert_eq!(
        AggregationInputView::structural(b, r, &[reports[1], reports[0]]).unwrap_err(),
        F05_REPORT_INVARIANT
    );
    let duplicate_workers = [workers[0], workers[0]];
    assert!(roster(&duplicate_workers, &evaluators).validate().is_err());
    let wrong_generation = [WorkerRosterEntry {
        generation: Version::new(2).unwrap(),
        ..workers[0]
    }];
    assert!(EpochAggregation::structural(b, digest, &wrong_generation, &outputs).is_err());
    assert!(decode_current(&state_bytes[..state_n], b, &wrong_generation).is_err());
    for wrong in [
        FrozenBinding {
            chain: ChainDomain::new([2; 32]).unwrap(),
            ..b
        },
        FrozenBinding {
            program: ProgramId::new([3; 32]).unwrap(),
            ..b
        },
        FrozenBinding {
            market: MarketId::new([4; 32]).unwrap(),
            ..b
        },
        FrozenBinding { epoch: 1, ..b },
        FrozenBinding {
            config: Version::new(2).unwrap(),
            ..b
        },
        FrozenBinding {
            roster: RosterDigest::new([5; 32]).unwrap(),
            ..b
        },
    ] {
        assert!(check_binding(b, wrong).is_err());
        assert!(
            decode_epoch_preimage(&output_bytes[..output_n], wrong, &workers, epoch.root())
                .is_err()
        );
        let mut changed_report = reports[0];
        changed_report.binding.frozen = wrong;
        assert!(AggregationInputView::structural(b, r, &[changed_report]).is_err());
    }
    let mut bad = output_bytes;
    bad[1] = 2;
    assert_eq!(
        decode_epoch_preimage(&bad[..output_n], b, &workers, epoch.root()).unwrap_err(),
        BAD_VERSION
    );
    let mut bad_report = encoded[0].clone();
    bad_report[260..264].copy_from_slice(&1000001u32.to_be_bytes());
    assert_eq!(decode_report(&bad_report).unwrap_err(), F03_SCORE_RANGE);
    assert_eq!(
        AggregationInputView::persisted_report(&bad_report).unwrap_err(),
        F05_REPORT_INVARIANT
    );
    let mut bad_report = encoded[0].clone();
    bad_report[1] = 2;
    assert!(decode_report(&bad_report).is_err());
    let unknown = [cell(WorkerId::new([99; 32]).unwrap(), 1)];
    assert_eq!(
        AggregationInputView::structural(b, r, &[report(b, evaluators[0], &unknown)]).unwrap_err(),
        F05_REPORT_INVARIANT
    );
    let mut no_write = [0x5a; CURRENT_MAX_BYTES];
    assert!(encode_current(
        &CurrentAggregation {
            cursor: 0,
            ..terminal
        },
        b,
        &workers,
        &mut no_write
    )
    .is_err());
    assert!(no_write.iter().all(|v| *v == 0x5a));

    // A11: actual persisted ReportBody with self identity / same declared owner.
    // Frozen common codecs contain no authenticated F03 admission function; this
    // gate asserts F05 persisted invariant refusal, not a fabricated F03 success.
    let same_owner = [EvaluatorRosterEntry {
        owner: workers[0].owner,
        ..evaluators[0]
    }];
    let rr = roster(&workers, &same_owner);
    let bb = binding(&rr);
    let body = report(bb, same_owner[0], &scores[0]);
    let mut bytes = [0; REPORT_MAX_BYTES];
    let n = encode_report(&body, &mut bytes).unwrap();
    let persisted = AggregationInputView::persisted_report(&bytes[..n]).unwrap();
    assert_eq!(
        AggregationInputView::structural(bb, rr, &[persisted]).unwrap_err(),
        F05_REPORT_INVARIANT
    );
    let self_id = [EvaluatorRosterEntry {
        evaluator: EvaluatorId::new(workers[0].worker.bytes()).unwrap(),
        ..evaluators[0]
    }];
    let rr = roster(&workers, &self_id);
    let bb = binding(&rr);
    assert_eq!(
        AggregationInputView::structural(bb, rr, &[report(bb, self_id[0], &scores[0])])
            .unwrap_err(),
        F05_REPORT_INVARIANT
    );

    // A08: actual 32-worker/8-evaluator reports, 256 explicit score cells.
    let max_workers: Vec<_> = (1..=32).map(worker).collect();
    let max_evaluators: Vec<_> = (1..=8).map(evaluator).collect();
    let rr = roster(&max_workers, &max_evaluators);
    let bb = binding(&rr);
    let max_scores: Vec<_> = max_workers
        .iter()
        .map(|w| cell(w.worker, 1000000))
        .collect();
    let reports: Vec<_> = max_evaluators
        .iter()
        .map(|e| report(bb, *e, &max_scores))
        .collect();
    for report in &reports {
        let mut bytes = [0; REPORT_MAX_BYTES];
        assert_eq!(encode_report(report, &mut bytes).unwrap(), 1380);
        decode_report(&bytes).unwrap();
    }
    let vv = AggregationInputView::structural(bb, rr, &reports).unwrap();
    assert_eq!(reports.iter().map(|r| r.scores.len()).sum::<usize>(), 256);
    for w in &max_workers {
        assert_eq!(vv.support(w.worker).unwrap(), 8);
    }
    let rows = commitments(&reports);
    let dd = input_digest(bb, 96, &rows).unwrap();
    let outputs: Vec<_> = max_workers
        .iter()
        .map(|w| {
            WorkerAggregate::new(
                w.worker,
                w.generation,
                8,
                QualityStatus::ScoredPositive,
                1000000,
                1000000,
            )
            .unwrap()
        })
        .collect();
    let epoch = EpochAggregation::structural(bb, dd, &max_workers, &outputs).unwrap();
    assert_eq!(epoch.total_weight(), 32000000);
    let terminal = CurrentAggregation {
        phase: AggregationPhase::Terminal,
        seal_height: 96,
        input: Presence::Present(dd),
        cursor: 32,
        reports: &rows,
        outputs: &outputs,
        running_weight: 32000000,
        root: Presence::Present(epoch.root()),
    };
    terminal.validate_inputs(&vv).unwrap();
    let mut bytes = [0; CURRENT_MAX_BYTES];
    assert_eq!(
        encode_current(&terminal, bb, &max_workers, &mut bytes).unwrap(),
        2200
    );
    decode_current(&bytes, bb, &max_workers).unwrap();
    let mut p = [0; OUTPUT_PREIMAGE_MAX_BYTES];
    assert_eq!(epoch.encode_preimage(&mut p).unwrap(), 1788);
    decode_epoch_preimage(&p, bb, &max_workers, epoch.root()).unwrap();
    let history: Vec<_> = (0..32)
        .map(|number| HistorySummary {
            epoch: number,
            config: version(),
            roster: bb.roster,
            input: dd,
            root: epoch.root(),
        })
        .collect();
    let mut h = [0; HISTORY_MAX_BYTES];
    assert_eq!(encode_history(&history, &mut h).unwrap(), 3586);
    let decoded = decode_history(&h).unwrap();
    assert_eq!(decoded.len(), 32);
    assert_eq!(decoded.entry(31).unwrap(), history[31]);
    assert_eq!(CURRENT_MAX_BYTES + HISTORY_MAX_BYTES, F05_MAX_BYTES);
    assert_eq!(F05_MAX_BYTES, 5786);
    let mut excess_workers = max_workers.clone();
    excess_workers.push(worker(33));
    assert_eq!(
        roster(&excess_workers, &max_evaluators).validate(),
        Err(CAPACITY)
    );
    let mut excess_evaluators = max_evaluators.clone();
    excess_evaluators.push(evaluator(9));
    assert_eq!(
        roster(&max_workers, &excess_evaluators).validate(),
        Err(CAPACITY)
    );
    let mut excess_reports = reports.clone();
    excess_reports.push(reports[0]);
    assert_eq!(
        AggregationInputView::structural(bb, rr, &excess_reports).unwrap_err(),
        CAPACITY
    );
    let mut excess_scores = max_scores.clone();
    excess_scores.push(cell(worker(33).worker, 1));
    assert_eq!(ScoreVector::Typed(&excess_scores).validate(), Err(CAPACITY));
    let mut excess_rows = rows.clone();
    excess_rows.push(rows[0]);
    assert_eq!(input_digest(bb, 96, &excess_rows), Err(CAPACITY));
    let mut excess_outputs = outputs.clone();
    excess_outputs.push(outputs[0]);
    assert_eq!(
        EpochAggregation::structural(bb, dd, &max_workers, &excess_outputs).unwrap_err(),
        CAPACITY
    );
    let mut excess_history = history.clone();
    excess_history.push(HistorySummary {
        epoch: 32,
        ..history[0]
    });
    assert_eq!(encode_history(&excess_history, &mut h), Err(CAPACITY));
    let mut reversed_history = history.clone();
    reversed_history.swap(0, 1);
    assert!(encode_history(&reversed_history, &mut h).is_err());
    let mut corrupt_history = h;
    corrupt_history[2..10].copy_from_slice(&1u64.to_be_bytes());
    assert!(decode_history(&corrupt_history).is_err());
    let mut corrupt_history = h;
    corrupt_history[10..18].fill(0);
    assert!(decode_history(&corrupt_history).is_err());
    let mut history_trailing = h.to_vec();
    history_trailing.push(0);
    assert!(decode_history(&history_trailing).is_err());
    let mut excess_history_bytes = h;
    excess_history_bytes[..2].copy_from_slice(&33u16.to_be_bytes());
    assert_eq!(decode_history(&excess_history_bytes).unwrap_err(), CAPACITY);
    let mut corrupt_current = bytes;
    corrupt_current[43..45].copy_from_slice(&9u16.to_be_bytes());
    assert_eq!(
        decode_current(&corrupt_current, bb, &max_workers).unwrap_err(),
        CAPACITY
    );
    let mut corrupt_current = bytes;
    corrupt_current[557..559].copy_from_slice(&33u16.to_be_bytes());
    assert_eq!(
        decode_current(&corrupt_current, bb, &max_workers).unwrap_err(),
        CAPACITY
    );
    let mut corrupt_current = bytes;
    corrupt_current[45..109].copy_from_slice(&bytes[109..173]);
    assert!(decode_current(&corrupt_current, bb, &max_workers).is_err());
    let mut corrupt_current = bytes;
    corrupt_current[559..609].copy_from_slice(&bytes[609..659]);
    assert!(decode_current(&corrupt_current, bb, &max_workers).is_err());

    // Common real state framing charges the F05 bytes to the joint section;
    // reserved bytes have no feature-specific invented alternative layout.
    let mut combined = Vec::new();
    combined.extend_from_slice(&bytes);
    combined.extend_from_slice(&h);
    let state = StateFrame {
        revision: 1,
        sections: [&[], &[], &[], &combined, &[], &[]],
    };
    let mut frame = vec![0; MAX_STATE_BYTES];
    let n = encode_state(&state, &mut frame).unwrap();
    assert_eq!(n, 64 + 5786);
    decode_state(&frame[..n]).unwrap();
    frame[18] = 1;
    assert_eq!(decode_state(&frame[..n]).unwrap_err(), NON_CANONICAL);
    assert!(decode_current(&vec![0; 2201], bb, &max_workers).is_err());
    assert!(decode_history(&vec![0; 3587]).is_err());

    // Common framing budget only: this does not claim integrated feature payload
    // validity or host resource admission. Settlement payload cap is 81912 plus
    // its 8-byte header, and complete state includes all 64 framing bytes.
    let payloads: Vec<Vec<u8>> = [16376, 24568, 24568, 81912, 24568, 24552]
        .iter()
        .map(|&length| vec![0; length])
        .collect();
    let maximum_frame = StateFrame {
        revision: 1,
        sections: [
            &payloads[0],
            &payloads[1],
            &payloads[2],
            &payloads[3],
            &payloads[4],
            &payloads[5],
        ],
    };
    assert_eq!(maximum_frame.encoded_len().unwrap(), 196608);
    assert_eq!(encode_state(&maximum_frame, &mut frame).unwrap(), 196608);
    decode_state(&frame).unwrap();
    let over_settlement = vec![0; 81913];
    assert_eq!(
        StateFrame {
            revision: 1,
            sections: [&[], &[], &[], &over_settlement, &[], &[]]
        }
        .encoded_len(),
        Err(CAPACITY)
    );
}

struct IntegerEpoch {
    outputs: Vec<WorkerAggregate>,
    total: u64,
    display: Vec<Presence<u32>>,
    input: Digest32,
    root: Digest32,
}
fn integer_epoch(
    workers: &[WorkerRosterEntry],
    evaluators: &[EvaluatorRosterEntry],
    cells: &[Vec<ScoreEntry>],
    seal: u64,
) -> IntegerEpoch {
    let r = roster(workers, evaluators);
    let b = binding(&r);
    let reports: Vec<_> = evaluators
        .iter()
        .zip(cells)
        .filter(|(_, c)| !c.is_empty())
        .map(|(e, c)| report(b, *e, c))
        .collect();
    let view = AggregationInputView::structural(b, r, &reports).unwrap();
    let epoch = aggregation::aggregate_epoch(&view).unwrap();
    assert_eq!(epoch.len(), workers.len());
    let outputs: Vec<_> = (0..epoch.len()).map(|i| epoch.output(i).unwrap()).collect();
    assert!(epoch.output(workers.len()).is_err());
    assert_eq!(
        aggregation::total_weight(&outputs).unwrap(),
        epoch.total_weight()
    );
    for (w, o) in workers.iter().zip(&outputs) {
        assert_eq!(aggregation::aggregate_worker(&view, *w).unwrap(), *o);
        assert_eq!(o.support(), view.support(w.worker).unwrap());
    }
    let display = (0..epoch.len())
        .map(|i| epoch.display_share_ppm(i).unwrap())
        .collect();
    let input = input_digest(b, seal, &commitments(&reports)).unwrap();
    let root = EpochAggregation::structural(b, input, workers, &outputs)
        .unwrap()
        .root();
    IntegerEpoch {
        outputs,
        total: epoch.total_weight(),
        display,
        input,
        root,
    }
}
/// One worker; `None` is a missing evaluator report, never a zero vote.
fn single(scores: &[Option<u32>]) -> IntegerEpoch {
    let workers = [worker(1)];
    let evaluators: Vec<_> = (1..=u8::try_from(scores.len()).unwrap())
        .map(evaluator)
        .collect();
    let cells: Vec<Vec<ScoreEntry>> = scores
        .iter()
        .map(|s| {
            s.map(|v| vec![cell(workers[0].worker, v)])
                .unwrap_or_default()
        })
        .collect();
    integer_epoch(&workers, &evaluators, &cells, 96)
}
fn scores(values: &[u32]) -> Vec<Score> {
    values.iter().map(|v| Score::new(*v).unwrap()).collect()
}
fn permutations(values: &[u32]) -> Vec<Vec<u32>> {
    if values.len() <= 1 {
        return vec![values.to_vec()];
    }
    let mut all = Vec::new();
    for i in 0..values.len() {
        let mut rest = values.to_vec();
        let head = rest.remove(i);
        for mut tail in permutations(&rest) {
            tail.insert(0, head);
            all.push(tail);
        }
    }
    all
}
fn wire(v: WorkerAggregate) -> [u8; 50] {
    let mut bytes = [0; 50];
    assert_eq!(encode_worker_aggregate(v, &mut bytes).unwrap(), 50);
    bytes
}

#[test]
fn aggregation_integer_contract() {
    let zero = Score::new(0).unwrap();

    // A01: support 3, positive, score equals raw weight; no transfer type exists.
    let a01 = single(&[Some(210000), Some(620000), Some(970000)]);
    let v = a01.outputs[0];
    assert_eq!(v.support(), 3);
    assert_eq!(v.status(), QualityStatus::ScoredPositive);
    assert_eq!(v.score().get(), 620000);
    assert_eq!(v.weight(), 620000);
    assert_eq!(v.quality(), Presence::Present(Score::new(620000).unwrap()));
    assert_eq!(a01.total, 620000);
    assert_eq!(a01.display, vec![Presence::Present(1_000_000)]);

    // A02: even count selects the lower central value, never the average.
    let a02 = single(&[Some(10000), Some(20000), Some(900000), Some(990000)]);
    assert_eq!(a02.outputs[0].support(), 4);
    assert_eq!(a02.outputs[0].score().get(), 20000);
    assert_ne!(a02.outputs[0].score().get(), 460000);
    assert_eq!(a02.outputs[0].weight(), 20000);
    for p in permutations(&[10000, 20000, 900000, 990000]) {
        let m = aggregation::lower_median(&scores(&p)).unwrap();
        assert_eq!(m.support, 4);
        assert_eq!(m.selected, Presence::Present(Score::new(20000).unwrap()));
    }

    // A03: measured zero with quorum versus absent quality without quorum.
    let measured = single(&[Some(0), Some(0), Some(1000000)]).outputs[0];
    assert_eq!(measured.support(), 3);
    assert_eq!(measured.status(), QualityStatus::ScoredZero);
    assert_eq!(measured.weight(), 0);
    assert_eq!(measured.quality(), Presence::Present(zero));
    let absent = single(&[Some(0), Some(1000000), None, None, None, None, None, None]);
    let absent_v = absent.outputs[0];
    assert_eq!(absent_v.support(), 2);
    assert_eq!(absent_v.status(), QualityStatus::InsufficientQuorum);
    assert_eq!(absent_v.weight(), 0);
    assert_eq!(absent_v.quality(), Presence::Absent);
    assert_ne!(absent_v.quality(), measured.quality());
    assert_ne!(absent_v.status(), measured.status());
    assert_eq!(absent.total, 0);
    assert_eq!(absent.display, vec![Presence::Absent]);
    assert_eq!(
        aggregation::lower_median(&scores(&[0, 1000000])).unwrap(),
        aggregation::Median {
            support: 2,
            selected: Presence::Absent,
            comparisons: 1,
        }
    );
    assert_eq!(
        aggregation::lower_median(&[]).unwrap().selected,
        Presence::Absent
    );

    // A04: five missing evaluators add no observations.
    let a04 = single(&[
        Some(0),
        None,
        Some(300000),
        None,
        None,
        Some(700000),
        None,
        None,
    ])
    .outputs[0];
    assert_eq!(a04.support(), 3);
    assert_eq!(a04.score().get(), 300000);
    assert_eq!(a04.status(), QualityStatus::ScoredPositive);

    // A05: arrival permutations of one canonical admitted set give identical
    // output bytes and roots; a different seal height changes only the roots.
    let a05 = single(&[Some(111111), Some(111111), Some(999999)]);
    assert_eq!(a05.outputs[0].score().get(), 111111);
    {
        let workers = [worker(1)];
        let evaluators = [evaluator(1), evaluator(2), evaluator(3)];
        let r = roster(&workers, &evaluators);
        let b = binding(&r);
        let cells = [
            [cell(workers[0].worker, 111111)],
            [cell(workers[0].worker, 111111)],
            [cell(workers[0].worker, 999999)],
        ];
        let canonical: Vec<_> = evaluators
            .iter()
            .zip(&cells)
            .map(|(e, c)| report(b, *e, c))
            .collect();
        for order in permutations(&[0, 1, 2]) {
            let arrival: Vec<_> = order.iter().map(|&i| canonical[i as usize]).collect();
            let mut rows = commitments(&arrival);
            canonicalize_arrivals(&mut rows).unwrap();
            let sealed: Vec<_> = rows
                .iter()
                .map(|row| {
                    *arrival
                        .iter()
                        .find(|r| r.binding.evaluator == row.evaluator)
                        .unwrap()
                })
                .collect();
            let view = AggregationInputView::structural(b, r, &sealed).unwrap();
            let epoch = aggregation::aggregate_epoch(&view).unwrap();
            let out = [epoch.output(0).unwrap()];
            assert_eq!(wire(out[0]), wire(a05.outputs[0]));
            let input = input_digest(b, 96, &rows).unwrap();
            assert_eq!(input, a05.input);
            let root = EpochAggregation::structural(b, input, &workers, &out)
                .unwrap()
                .root();
            assert_eq!(root, a05.root);
        }
    }
    for p in permutations(&[111111, 111111, 999999]) {
        let m = aggregation::lower_median(&scores(&p)).unwrap();
        assert_eq!(m.selected, Presence::Present(Score::new(111111).unwrap()));
        let by_value = single(&[Some(p[0]), Some(p[1]), Some(p[2])]).outputs[0];
        assert_eq!(wire(by_value), wire(a05.outputs[0]));
    }
    let later = {
        let workers = [worker(1)];
        let evaluators = [evaluator(1), evaluator(2), evaluator(3)];
        let cells: Vec<_> = [111111, 111111, 999999]
            .iter()
            .map(|v| vec![cell(workers[0].worker, *v)])
            .collect();
        integer_epoch(&workers, &evaluators, &cells, 97)
    };
    assert_eq!(wire(later.outputs[0]), wire(a05.outputs[0]));
    assert_ne!(later.input, a05.input);
    assert_ne!(later.root, a05.root);

    // A06: canonical ascending ID32 worker order, W and floor display ppm.
    let ids: Vec<_> = (1u8..=3)
        .map(|n| {
            let mut id = [0u8; 32];
            id[31] = n;
            WorkerRosterEntry {
                worker: WorkerId::new(id).unwrap(),
                ..worker(n)
            }
        })
        .collect();
    let evaluators = [evaluator(1), evaluator(2), evaluator(3)];
    let row: Vec<_> = ids
        .iter()
        .zip([250000, 500000, 250000])
        .map(|(w, s)| cell(w.worker, s))
        .collect();
    let a06 = integer_epoch(&ids, &evaluators, &[row.clone(), row.clone(), row], 96);
    let order: Vec<_> = a06.outputs.iter().map(|o| o.worker().bytes()[31]).collect();
    assert_eq!(order, vec![1, 2, 3]);
    assert_eq!(a06.total, 1_000_000);
    assert_eq!(
        a06.display,
        vec![
            Presence::Present(250000),
            Presence::Present(500000),
            Presence::Present(250000)
        ]
    );

    // A07: display truncation is dust only; raw weights remain authoritative.
    let three: Vec<_> = (1..=3).map(worker).collect();
    let ones: Vec<_> = three.iter().map(|w| cell(w.worker, 1)).collect();
    let a07 = integer_epoch(&three, &evaluators, &[ones.clone(), ones.clone(), ones], 96);
    assert!(a07.outputs.iter().all(|o| o.weight() == 1));
    assert_eq!(a07.total, 3);
    assert_eq!(a07.display, vec![Presence::Present(333333); 3]);
    let shown: u32 = a07
        .display
        .iter()
        .map(|d| match d {
            Presence::Present(v) => *v,
            Presence::Absent => 0,
        })
        .sum();
    assert_eq!(shown, 999999);
    assert_eq!(
        aggregation::display_share_ppm(1, 3).unwrap(),
        Presence::Present(333333)
    );
    assert_eq!(aggregation::display_share_ppm(2, 1), Err(ARITHMETIC));
    assert_eq!(
        aggregation::display_share_ppm(1, 32_000_001),
        Err(ARITHMETIC)
    );

    // A08: 32 workers x 8 evaluators, 256 cells, four bounded chunks of eight.
    let max_workers: Vec<_> = (1..=32).map(worker).collect();
    let max_evaluators: Vec<_> = (1..=8).map(evaluator).collect();
    let full: Vec<_> = max_workers
        .iter()
        .map(|w| cell(w.worker, 1000000))
        .collect();
    let cells = vec![full; 8];
    assert_eq!(cells.iter().map(Vec::len).sum::<usize>(), 256);
    let a08 = integer_epoch(&max_workers, &max_evaluators, &cells, 96);
    assert_eq!(a08.total, 32_000_000);
    assert!(a08.outputs.iter().all(|o| o.support() == 8
        && o.weight() == 1_000_000
        && o.status() == QualityStatus::ScoredPositive));
    assert_eq!(a08.display, vec![Presence::Present(31250); 32]);
    assert_eq!(
        integer_epoch(&max_workers, &max_evaluators, &cells, 96).root,
        a08.root
    );
    {
        let r = roster(&max_workers, &max_evaluators);
        let b = binding(&r);
        let reports: Vec<_> = max_evaluators
            .iter()
            .zip(&cells)
            .map(|(e, c)| report(b, *e, c))
            .collect();
        let view = AggregationInputView::structural(b, r, &reports).unwrap();
        let mut cursor = 0u16;
        let mut sum = 0u64;
        let mut calls = 0;
        loop {
            let chunk = aggregation::aggregate_chunk(&view, cursor, sum).unwrap();
            if chunk.is_empty() {
                assert_eq!(chunk.cursor(), 32);
                break;
            }
            assert_eq!(chunk.len(), aggregation::WORKERS_PER_CHUNK);
            for i in 0..chunk.len() {
                assert_eq!(
                    chunk.output(i).unwrap(),
                    a08.outputs[usize::from(cursor) + i]
                );
            }
            assert!(chunk.output(chunk.len()).is_err());
            cursor = chunk.cursor();
            sum = chunk.running_weight();
            calls += 1;
        }
        assert_eq!(calls, 4);
        assert_eq!(sum, 32_000_000);
        assert_eq!(
            aggregation::aggregate_chunk(&view, 33, 0).unwrap_err(),
            STALE_CURSOR
        );
        assert_eq!(
            aggregation::aggregate_chunk(&view, 24, 24_000_001).unwrap_err(),
            ARITHMETIC
        );
    }
    assert_eq!(
        aggregation::add_weight(u64::MAX, a08.outputs[0]),
        Err(ARITHMETIC)
    );
    assert_eq!(
        aggregation::add_weight(31_000_000, a08.outputs[0]).unwrap(),
        32_000_000
    );
    assert_eq!(
        aggregation::add_weight(31_000_001, a08.outputs[0]),
        Err(ARITHMETIC)
    );
    assert_eq!(
        aggregation::display_share_ppm(1_000_000, 32_000_000).unwrap(),
        Presence::Present(31250)
    );
    let worst = aggregation::lower_median(&scores(&[8, 7, 6, 5, 4, 3, 2, 1])).unwrap();
    assert_eq!(worst.comparisons, aggregation::MAX_COMPARISONS_PER_WORKER);
    assert_eq!(worst.selected, Presence::Present(Score::new(4).unwrap()));
    assert_eq!(
        aggregation::lower_median(&scores(&[1; 9])).unwrap_err(),
        CAPACITY
    );
    let mut too_many = a08.outputs.clone();
    too_many.push(a08.outputs[0]);
    assert_eq!(aggregation::total_weight(&too_many), Err(CAPACITY));

    // A09: no workers or no admitted reports -> W0, no fallback shares.
    let empty = integer_epoch(&[], &max_evaluators, &[], 96);
    assert!(empty.outputs.is_empty());
    assert_eq!(empty.total, 0);
    let silent = integer_epoch(&three, &max_evaluators, &[], 96);
    assert_eq!(silent.total, 0);
    assert!(silent.outputs.iter().all(|o| o.support() == 0
        && o.status() == QualityStatus::InsufficientQuorum
        && o.weight() == 0
        && o.quality() == Presence::Absent));
    assert_eq!(silent.display, vec![Presence::Absent; 3]);
    assert_eq!(
        aggregation::display_share_ppm(0, 0).unwrap(),
        Presence::Absent
    );
    let zeros: Vec<_> = three.iter().map(|w| cell(w.worker, 0)).collect();
    let zero_epoch = integer_epoch(
        &three,
        &evaluators,
        &[zeros.clone(), zeros.clone(), zeros],
        96,
    );
    assert_eq!(zero_epoch.total, 0);
    assert!(zero_epoch
        .outputs
        .iter()
        .all(|o| o.status() == QualityStatus::ScoredZero && o.weight() == 0));
    assert_eq!(zero_epoch.display, vec![Presence::Absent; 3]);

    // A17: two low colluding reports among four select zero. This demonstrates
    // the specified lower-median vulnerability, not quality or collusion detection.
    let a17 = single(&[Some(0), Some(0), Some(900000), Some(1000000)]).outputs[0];
    assert_eq!(a17.support(), 4);
    assert_eq!(a17.score().get(), 0);
    assert_eq!(a17.status(), QualityStatus::ScoredZero);
    assert_eq!(a17.weight(), 0);
}
