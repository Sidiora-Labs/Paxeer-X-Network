use std::cell::Cell;
use std::collections::BTreeSet;

use layerx_agentd::audit::{redact, DataClass, Decision, Entry, EventClass, OutputSurface};
use layerx_agentd::identity::ProtocolAuthority;
use layerx_agentd::obs::health::{
    evaluate, guard_write, BoundaryConnectivity, DegradedMode, HealthInput, WriteBlocker,
    WriteReadiness,
};
use layerx_agentd::obs::metrics::{MetricKind, MetricLabel, Metrics, MetricsError};
use layerx_agentd::obs::trace::{Trace, TraceError, TraceOutcome, TraceStage};
use layerx_agentd::store::TenantId;
use layerx_agentd::tenant::{Config, RedactionPolicy, Retention};
use layerx_types::ids::{Did, IdempotencyKey};
use layerx_types::verify::VerificationLevel;

fn tenant(value: &str) -> TenantId {
    TenantId::new(value).unwrap_or_else(|error| panic!("tenant: {error}"))
}

fn label(kind: MetricKind) -> MetricLabel {
    match kind {
        MetricKind::SubmissionOutcome => MetricLabel::Executed,
        MetricKind::UnknownPopulation => MetricLabel::Unknown,
        MetricKind::UnknownAge => MetricLabel::AgeAtLeastMinute,
        MetricKind::VerificationLevel => MetricLabel::StateProven,
        MetricKind::BoundaryLatency | MetricKind::DegradedState => MetricLabel::BoundaryReady,
        MetricKind::ErrorClass => MetricLabel::InternalFailure,
        MetricKind::PolicyDecision => MetricLabel::Allowed,
        MetricKind::CapabilityDecision => MetricLabel::Denied,
        MetricKind::BudgetUtilization => MetricLabel::UtilizationHigh,
        MetricKind::SubscriptionLag => MetricLabel::Lagging,
        MetricKind::RateLimitRefusal => MetricLabel::RateExceeded,
    }
}

#[test]
fn metric_schema_covers_every_required_signal_with_bounded_labels() {
    let alpha = tenant("alpha");
    let beta = tenant("beta");
    let mut metrics = Metrics::new(2).unwrap_or_else(|error| panic!("metrics: {error}"));
    metrics
        .register_tenant(alpha.clone())
        .unwrap_or_else(|error| panic!("alpha: {error}"));
    metrics
        .register_tenant(beta.clone())
        .unwrap_or_else(|error| panic!("beta: {error}"));
    for (index, kind) in MetricKind::ALL.into_iter().enumerate() {
        metrics
            .observe(&alpha, kind, label(kind), index as u64 + 1)
            .unwrap_or_else(|error| panic!("observe {kind:?}: {error}"));
    }
    let snapshot = metrics.snapshot(&alpha);
    assert_eq!(snapshot.len(), MetricKind::ALL.len());
    assert!(snapshot.iter().all(|(key, _)| key.tenant == alpha));
    assert!(metrics.snapshot(&beta).is_empty());
    assert_eq!(
        metrics.observe(
            &alpha,
            MetricKind::RateLimitRefusal,
            MetricLabel::Executed,
            1,
        ),
        Err(MetricsError::InvalidLabel)
    );
    assert_eq!(
        metrics.register_tenant(tenant("gamma")),
        Err(MetricsError::TenantCapacityExceeded)
    );
    let schema = include_str!("../src/obs/metrics.rs");
    assert!(!schema.contains("activity_id"));
    assert!(!schema.contains("request_id"));
    assert!(!schema.contains("idempotency"));
}

#[test]
fn trace_spans_the_full_write_path_with_one_correlation_identifier() {
    let request_id = [9; 32];
    let mut trace = Trace::start(tenant("alpha"), request_id);
    for stage in TraceStage::ALL {
        let outcome = match stage {
            TraceStage::Policy => TraceOutcome::Allowed,
            TraceStage::ReceiptResolution => TraceOutcome::Completed,
            _ => TraceOutcome::Started,
        };
        trace
            .enter(stage, outcome)
            .unwrap_or_else(|error| panic!("trace {stage:?}: {error:?}"));
    }
    let trace = trace
        .finish()
        .unwrap_or_else(|error| panic!("finish trace: {error:?}"));
    assert_eq!(trace.correlation_id().bytes(), request_id);
    assert_eq!(trace.spans().len(), TraceStage::ALL.len());
    assert_eq!(trace.tenant(), &tenant("alpha"));
    assert_eq!(trace.correlation_id().to_string(), "09".repeat(32));

    let config = Config {
        tenant: tenant("alpha"),
        policy_version: "policy-v1".to_owned(),
        redaction: RedactionPolicy::Strict,
        retention: Retention {
            events: 10,
            audit: 10,
            receipts: 10,
        },
        verification_default: VerificationLevel::STATE_PROVEN,
        approval_required_for: BTreeSet::new(),
    };
    let reason = redact(
        &config,
        &config.tenant,
        OutputSurface::Audit,
        DataClass::PublicText,
        b"policy allowed",
        1,
    )
    .unwrap_or_else(|error| panic!("redact correlation reason: {error}"))
    .value;
    let audit_entry = Entry {
        class: EventClass::PolicyDecision,
        observed_at_ms: 1_000,
        tenant: tenant("alpha"),
        agent: Did::new(b"did:layerx:alpha").unwrap_or_else(|error| panic!("DID: {error:?}")),
        session: None,
        capability: None,
        policy_version: "policy-v1".to_owned(),
        request_id,
        idempotency_key: Some(IdempotencyKey::new([8; 32])),
        decision: Decision::Allowed,
        reason,
        resulting_activity_id: None,
        verification_level: VerificationLevel::UNVERIFIED,
        protocol_authority: Some(ProtocolAuthority::SessionKey([7; 32])),
        submitted_bytes: None,
        receipt_id: None,
    };
    assert!(trace.correlates(&audit_entry));

    let mut invalid = Trace::start(tenant("alpha"), request_id);
    assert_eq!(
        invalid.enter(TraceStage::Policy, TraceOutcome::Allowed),
        Err(TraceError::StageOutOfOrder)
    );
}

fn ready_input() -> HealthInput {
    HealthInput {
        live: true,
        boundary: BoundaryConnectivity::Ready,
        audit_writable: true,
        recovery_complete: true,
        verification_backlog: 0,
        maximum_verification_backlog: 10,
        unknown_backlog: 0,
        maximum_unknown_backlog: 10,
        degraded_modes: BTreeSet::new(),
    }
}

#[test]
fn every_delivery_blocker_reports_not_ready_and_refuses_the_write() {
    let ready = evaluate(ready_input());
    assert_eq!(ready.write_readiness, WriteReadiness::Ready);
    assert_eq!(guard_write(&ready, || 7), Ok(7));

    let cases = [
        (
            HealthInput {
                live: false,
                ..ready_input()
            },
            WriteBlocker::NotLive,
        ),
        (
            HealthInput {
                boundary: BoundaryConnectivity::Backpressured,
                ..ready_input()
            },
            WriteBlocker::BoundaryBackpressured,
        ),
        (
            HealthInput {
                boundary: BoundaryConnectivity::Unavailable,
                ..ready_input()
            },
            WriteBlocker::BoundaryUnavailable,
        ),
        (
            HealthInput {
                boundary: BoundaryConnectivity::VersionMismatch,
                ..ready_input()
            },
            WriteBlocker::BoundaryVersionMismatch,
        ),
        (
            HealthInput {
                audit_writable: false,
                ..ready_input()
            },
            WriteBlocker::AuditUnavailable,
        ),
        (
            HealthInput {
                recovery_complete: false,
                ..ready_input()
            },
            WriteBlocker::RecoveryIncomplete,
        ),
        (
            HealthInput {
                verification_backlog: 11,
                ..ready_input()
            },
            WriteBlocker::VerificationBacklog,
        ),
        (
            HealthInput {
                unknown_backlog: 11,
                ..ready_input()
            },
            WriteBlocker::UnknownBacklog,
        ),
        (
            HealthInput {
                degraded_modes: BTreeSet::from([DegradedMode::CoreHalted]),
                ..ready_input()
            },
            WriteBlocker::Degraded,
        ),
    ];
    for (input, expected) in cases {
        let status = evaluate(input);
        let operation_ran = Cell::new(false);
        let Err(refusal) = guard_write(&status, || operation_ran.set(true)) else {
            panic!("blocked health must refuse writes")
        };
        assert!(refusal.blockers.contains(&expected));
        assert!(!operation_ran.get());
    }
}
