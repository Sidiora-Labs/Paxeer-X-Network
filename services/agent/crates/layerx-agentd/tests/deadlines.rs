use layerx_agentd::limits::cancel;
use layerx_agentd::limits::deadline::{
    DeadlineError, DeadlineOutcome, DisconnectOutcome, RequestDeadline, RequestTracker,
    TrackedWork, WorkOwner, WriteStage,
};

fn deadline() -> RequestDeadline {
    RequestDeadline::new(1_000, 2_000)
        .unwrap_or_else(|error| panic!("finite request deadline: {error:?}"))
}

fn submission_bytes(request_id: u64) -> [u8; 32] {
    let marker = u8::try_from(request_id)
        .unwrap_or_else(|error| panic!("request {request_id} must fit one byte: {error}"));
    [marker; 32]
}

fn tracker_at(stage: WriteStage, request_id: u64) -> RequestTracker {
    let mut tracker = RequestTracker::default();
    tracker
        .begin_write(request_id, submission_bytes(request_id), deadline())
        .unwrap_or_else(|error| panic!("write {request_id} starts: {error:?}"));
    for next in [
        WriteStage::Signing,
        WriteStage::DurableQueued,
        WriteStage::Transmitting,
        WriteStage::Acknowledged,
        WriteStage::UnknownResolving,
    ] {
        if next.ordinal_for_test() > stage.ordinal_for_test() {
            break;
        }
        tracker
            .advance_write(request_id, next, 1_100)
            .unwrap_or_else(|error| panic!("advance {request_id} to {next:?}: {error:?}"));
    }
    tracker
}

trait StageOrder {
    fn ordinal_for_test(self) -> u8;
}

impl StageOrder for WriteStage {
    fn ordinal_for_test(self) -> u8 {
        match self {
            Self::Preparing => 0,
            Self::Signing => 1,
            Self::DurableQueued => 2,
            Self::Transmitting => 3,
            Self::Acknowledged => 4,
            Self::UnknownResolving => 5,
        }
    }
}

#[test]
fn every_request_and_boundary_call_has_a_finite_deadline() {
    assert_eq!(
        RequestDeadline::new(1_000, 1_000),
        Err(DeadlineError::InvalidDeadline)
    );
    let request = deadline();
    assert_eq!(
        request
            .boundary_call(1_200, 250)
            .unwrap_or_else(|error| panic!("short call deadline: {error:?}"))
            .expires_at_ms,
        1_450
    );
    assert_eq!(
        request
            .boundary_call(1_900, 500)
            .unwrap_or_else(|error| panic!("capped boundary call: {error:?}"))
            .expires_at_ms,
        2_000
    );
    assert_eq!(request.boundary_call(2_000, 1), Err(DeadlineError::Elapsed));
}

#[test]
fn disconnect_cancels_every_write_stage_before_transmission() {
    for (index, stage) in [
        WriteStage::Preparing,
        WriteStage::Signing,
        WriteStage::DurableQueued,
    ]
    .into_iter()
    .enumerate()
    {
        let request_id = index as u64 + 1;
        let mut tracker = tracker_at(stage, request_id);
        let token = tracker
            .begin_read(100 + request_id, deadline())
            .unwrap_or_else(|error| {
                panic!("independent read alongside {request_id} supplies a token: {error:?}")
            });
        assert_eq!(
            cancel(&mut tracker, 100 + request_id, 1_200),
            Ok(DisconnectOutcome::Cancelled)
        );
        assert!(token.is_cancelled());

        assert_eq!(
            cancel(&mut tracker, request_id, 1_200),
            Ok(DisconnectOutcome::Cancelled),
            "stage {stage:?} must be cancellable"
        );
        assert!(tracker.view(request_id).is_none());
    }
}

#[test]
fn disconnect_mid_submission_transfers_ownership_to_receipt_resolution() {
    for (index, stage) in [
        WriteStage::Transmitting,
        WriteStage::Acknowledged,
        WriteStage::UnknownResolving,
    ]
    .into_iter()
    .enumerate()
    {
        let request_id = index as u64 + 10;
        let mut tracker = tracker_at(stage, request_id);
        assert_eq!(
            cancel(&mut tracker, request_id, 1_300),
            Ok(DisconnectOutcome::ResolutionContinues {
                submission_id: submission_bytes(request_id)
            })
        );
        let view = tracker
            .view(request_id)
            .unwrap_or_else(|| panic!("resolver retains ownership of {request_id}"));
        assert_eq!(view.owner, WorkOwner::DaemonResolver);
        assert!(!view.caller_connected);
        let expected_unknown_since = if stage == WriteStage::UnknownResolving {
            1_100
        } else {
            1_300
        };
        assert_eq!(view.unknown_since_ms, Some(expected_unknown_since));
        assert!(matches!(
            view.work,
            TrackedWork::Write {
                stage: WriteStage::UnknownResolving,
                reservation_held: true,
                ..
            }
        ));
        assert_eq!(tracker.complete(request_id), Err(DeadlineError::OrphanRisk));
        tracker
            .resolved_by_receipt(request_id)
            .unwrap_or_else(|error| panic!("receipt resolution for {request_id}: {error:?}"));
        assert!(tracker.view(request_id).is_none());
    }
}

#[test]
fn request_deadline_reports_unknown_but_does_not_cancel_its_resolver() {
    let mut tracker = tracker_at(WriteStage::UnknownResolving, 42);
    let outcomes = tracker
        .expire(2_000)
        .unwrap_or_else(|error| panic!("deadline processing: {error:?}"));
    assert_eq!(
        outcomes,
        vec![DeadlineOutcome::ReportedUnknown {
            request_id: 42,
            submission_id: [42; 32],
        }]
    );
    let view = tracker
        .view(42)
        .unwrap_or_else(|| panic!("unknown resolver remains owned"));
    assert_eq!(view.owner, WorkOwner::DaemonResolver);
    assert!(!view.cancelled);
    assert!(matches!(
        view.work,
        TrackedWork::Write {
            reservation_held: true,
            ..
        }
    ));
}

#[test]
fn in_flight_and_unresolved_population_expose_age_distribution() {
    let mut tracker = RequestTracker::default();
    tracker
        .begin_read(
            1,
            RequestDeadline::new(70_000, 200_000)
                .unwrap_or_else(|error| panic!("read deadline: {error:?}")),
        )
        .unwrap_or_else(|error| panic!("read begins: {error:?}"));
    tracker
        .begin_write(
            2,
            [2; 32],
            RequestDeadline::new(1_000, 200_000)
                .unwrap_or_else(|error| panic!("write deadline: {error:?}")),
        )
        .unwrap_or_else(|error| panic!("write begins: {error:?}"));
    for stage in [
        WriteStage::Signing,
        WriteStage::DurableQueued,
        WriteStage::Transmitting,
        WriteStage::Acknowledged,
        WriteStage::UnknownResolving,
    ] {
        tracker
            .advance_write(2, stage, 10_000)
            .unwrap_or_else(|error| panic!("advance to {stage:?}: {error:?}"));
    }

    let metrics = tracker
        .metrics(70_500)
        .unwrap_or_else(|error| panic!("deterministic metrics: {error:?}"));
    assert_eq!(metrics.in_flight, 2);
    assert_eq!(metrics.unresolved, 1);
    assert_eq!(metrics.in_flight_age.under_one_second, 1);
    assert_eq!(metrics.in_flight_age.at_least_one_minute, 1);
    assert_eq!(metrics.in_flight_age.oldest_ms, 69_500);
    assert_eq!(metrics.unresolved_age.at_least_one_minute, 1);
    assert_eq!(metrics.unresolved_age.oldest_ms, 60_500);
}
