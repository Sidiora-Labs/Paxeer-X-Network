use std::collections::BTreeMap;

use layerx_agentd::limits::admission::{
    AdmissionConfig, AdmissionError, BackpressureSource, BoundaryAdmission, BoundaryWork,
    CoreAvailability, CoreOutcome, Priority, QueueBound,
};

fn config(maximum_in_flight: usize, requests_per_lane: usize) -> AdmissionConfig {
    AdmissionConfig::new(
        maximum_in_flight,
        64,
        Priority::ALL.into_iter().map(|priority| {
            (
                priority,
                QueueBound {
                    requests: requests_per_lane,
                    bytes: requests_per_lane * 64,
                },
            )
        }),
    )
    .unwrap_or_else(|error| panic!("complete finite boundary configuration: {error:?}"))
}

fn work(request_id: u64, priority: Priority) -> BoundaryWork {
    BoundaryWork {
        request_id,
        tenant: "tenant-a".to_owned(),
        priority,
        bytes: request_id.to_be_bytes().to_vec(),
    }
}

#[test]
fn submission_and_receipt_dispatch_before_a_synthetic_bulk_storm() {
    let mut controller = BoundaryAdmission::new(config(2, 128));
    for request_id in 1..=128 {
        controller
            .admit(
                work(request_id, Priority::BulkRead),
                CoreAvailability::Ready,
            )
            .unwrap_or_else(|error| {
                panic!("bounded bulk lane accepts request {request_id}: {error:?}")
            });
    }
    assert!(matches!(
        controller.admit(work(129, Priority::BulkRead), CoreAvailability::Ready),
        Err(AdmissionError::Backpressure {
            source: BackpressureSource::QueueRequests,
            priority: Priority::BulkRead,
            queued_requests: 128,
            ..
        })
    ));

    controller
        .admit(
            work(1_000, Priority::ReceiptResolution),
            CoreAvailability::Ready,
        )
        .unwrap_or_else(|error| {
            panic!("receipt lane remains available during bulk saturation: {error:?}")
        });
    controller
        .admit(work(1_001, Priority::Submission), CoreAvailability::Ready)
        .unwrap_or_else(|error| {
            panic!("submission lane remains available during bulk saturation: {error:?}")
        });

    let first = controller
        .dispatch(CoreAvailability::Ready)
        .unwrap_or_else(|error| panic!("first dispatch: {error:?}"))
        .unwrap_or_else(|| panic!("submission is queued"));
    let second = controller
        .dispatch(CoreAvailability::Ready)
        .unwrap_or_else(|error| panic!("second dispatch: {error:?}"))
        .unwrap_or_else(|| panic!("receipt is queued"));
    assert_eq!(first.work.priority, Priority::Submission);
    assert_eq!(second.work.priority, Priority::ReceiptResolution);
    assert_eq!(
        controller.utilization(Priority::BulkRead).queued_requests,
        128
    );
}

#[test]
fn a_slow_node_holds_only_the_explicit_in_flight_capacity() {
    let mut controller = BoundaryAdmission::new(config(1, 4));
    controller
        .admit(work(1, Priority::Submission), CoreAvailability::Ready)
        .unwrap_or_else(|error| panic!("submission admitted: {error:?}"));
    controller
        .admit(work(2, Priority::BulkRead), CoreAvailability::Ready)
        .unwrap_or_else(|error| panic!("bulk read admitted: {error:?}"));

    let dispatched = controller
        .dispatch(CoreAvailability::Ready)
        .unwrap_or_else(|error| panic!("first dispatch: {error:?}"))
        .unwrap_or_else(|| panic!("submission is queued"));
    assert_eq!(dispatched.work.request_id, 1);
    assert_eq!(controller.in_flight(), 1);
    assert!(matches!(
        controller.dispatch(CoreAvailability::Ready),
        Err(AdmissionError::Backpressure {
            source: BackpressureSource::InFlight,
            priority: Priority::BulkRead,
            ..
        })
    ));
    assert_eq!(
        controller.utilization(Priority::BulkRead).queued_requests,
        1
    );

    controller
        .finish(1, CoreOutcome::Completed)
        .unwrap_or_else(|error| panic!("slow call eventually completes: {error:?}"));
    assert_eq!(
        controller
            .dispatch(CoreAvailability::Ready)
            .unwrap_or_else(|error| panic!("capacity is released: {error:?}"))
            .unwrap_or_else(|| panic!("bulk read remains queued"))
            .work
            .request_id,
        2
    );
}

#[test]
fn core_backpressure_and_unavailability_are_returned_without_hidden_queues() {
    let mut controller = BoundaryAdmission::new(config(1, 2));
    assert!(matches!(
        controller.admit(
            work(1, Priority::Retry),
            CoreAvailability::Backpressured { retry_after_ms: 25 }
        ),
        Err(AdmissionError::Backpressure {
            source: BackpressureSource::CoreBackpressured,
            retry_after_ms: Some(25),
            ..
        })
    ));
    assert_eq!(controller.utilization(Priority::Retry).queued_requests, 0);

    controller
        .admit(work(2, Priority::Submission), CoreAvailability::Ready)
        .unwrap_or_else(|error| panic!("work admitted while core is ready: {error:?}"));
    assert!(matches!(
        controller.dispatch(CoreAvailability::Unavailable { retry_after_ms: 80 }),
        Err(AdmissionError::Backpressure {
            source: BackpressureSource::CoreUnavailable,
            retry_after_ms: Some(80),
            ..
        })
    ));
    assert_eq!(
        controller.utilization(Priority::Submission).queued_requests,
        1
    );

    let request_id = controller
        .dispatch(CoreAvailability::Ready)
        .unwrap_or_else(|error| panic!("dispatch resumes: {error:?}"))
        .unwrap_or_else(|| panic!("submission remains owned"))
        .work
        .request_id;
    assert!(matches!(
        controller.finish(
            request_id,
            CoreOutcome::Backpressured { retry_after_ms: 40 }
        ),
        Err(AdmissionError::Backpressure {
            source: BackpressureSource::CoreBackpressured,
            retry_after_ms: Some(40),
            ..
        })
    ));
    assert_eq!(controller.in_flight(), 0);
    assert_eq!(
        controller.utilization(Priority::Submission).queued_requests,
        0
    );
}

#[test]
fn reconnecting_subscription_burst_cannot_consume_other_lanes() {
    let mut lanes = Priority::ALL
        .into_iter()
        .map(|priority| {
            (
                priority,
                QueueBound {
                    requests: 4,
                    bytes: 64,
                },
            )
        })
        .collect::<BTreeMap<_, _>>();
    lanes.insert(
        Priority::SubscriptionCatchUp,
        QueueBound {
            requests: 2,
            bytes: 16,
        },
    );
    let configuration = AdmissionConfig::new(2, 64, lanes)
        .unwrap_or_else(|error| panic!("valid lane map: {error:?}"));
    let mut controller = BoundaryAdmission::new(configuration);

    controller
        .admit(
            work(1, Priority::SubscriptionCatchUp),
            CoreAvailability::Ready,
        )
        .unwrap_or_else(|error| panic!("first reconnect admitted: {error:?}"));
    controller
        .admit(
            work(2, Priority::SubscriptionCatchUp),
            CoreAvailability::Ready,
        )
        .unwrap_or_else(|error| panic!("second reconnect admitted: {error:?}"));
    assert!(matches!(
        controller.admit(
            work(3, Priority::SubscriptionCatchUp),
            CoreAvailability::Ready
        ),
        Err(AdmissionError::Backpressure {
            source: BackpressureSource::QueueRequests,
            priority: Priority::SubscriptionCatchUp,
            ..
        })
    ));
    controller
        .admit(work(4, Priority::Submission), CoreAvailability::Ready)
        .unwrap_or_else(|error| {
            panic!("independently bounded submission lane remains available: {error:?}")
        });
    assert_eq!(
        controller
            .dispatch(CoreAvailability::Ready)
            .unwrap_or_else(|error| panic!("dispatch succeeds: {error:?}"))
            .unwrap_or_else(|| panic!("submission exists"))
            .work
            .priority,
        Priority::Submission
    );
}

#[test]
fn every_lane_and_message_has_an_explicit_finite_bound() {
    assert!(matches!(
        AdmissionConfig::new(1, 64, []),
        Err(AdmissionError::InvalidConfiguration)
    ));
    let mut controller = BoundaryAdmission::new(config(1, 1));
    let mut oversized = work(1, Priority::InteractiveRead);
    oversized.bytes = vec![0; 65];
    assert_eq!(
        controller.admit(oversized, CoreAvailability::Ready),
        Err(AdmissionError::InvalidWork)
    );

    let mut byte_bounded = BoundaryAdmission::new(
        AdmissionConfig::new(
            1,
            64,
            Priority::ALL.into_iter().map(|priority| {
                (
                    priority,
                    QueueBound {
                        requests: 2,
                        bytes: 8,
                    },
                )
            }),
        )
        .unwrap_or_else(|error| panic!("valid byte-bounded configuration: {error:?}")),
    );
    byte_bounded
        .admit(work(1, Priority::Backfill), CoreAvailability::Ready)
        .unwrap_or_else(|error| panic!("first eight-byte record fits: {error:?}"));
    assert!(matches!(
        byte_bounded.admit(work(2, Priority::Backfill), CoreAvailability::Ready),
        Err(AdmissionError::Backpressure {
            source: BackpressureSource::QueueBytes,
            priority: Priority::Backfill,
            queued_bytes: 8,
            ..
        })
    ));
}
