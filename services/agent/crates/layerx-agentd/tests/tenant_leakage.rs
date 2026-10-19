use layerx_agentd::store::{StoreError, TenantId};
use layerx_agentd::tenant::{
    normalize_error, BoundedMetricKey, BoundedMetrics, ErrorClass, InternalError, MetricKind,
    MetricLabel, Surface, TIMING_MITIGATION,
};

fn tenant(value: &str) -> TenantId {
    TenantId::new(value).unwrap_or_else(|error| panic!("tenant id {value}: {error}"))
}

#[test]
fn existing_cross_tenant_and_missing_objects_have_identical_shape_and_work() {
    assert!(TIMING_MITIGATION.contains("64 SHA-256 rounds"));
    let caller = tenant("caller-tenant");
    for surface in [
        Surface::Contract,
        Surface::RustSdk,
        Surface::TypeScriptSdk,
        Surface::PythonSdk,
        Surface::Mcp,
        Surface::Subscription,
        Surface::Export,
    ] {
        let cross_tenant = InternalError::not_authorized(
            "secret-foreign-tenant",
            b"secret-existing-object-identifier".to_vec(),
        );
        let missing = InternalError::missing(b"different-missing-identifier".to_vec());
        let mut cross_metrics = BoundedMetrics::default();
        let mut missing_metrics = BoundedMetrics::default();
        let cross = normalize_error(&cross_tenant, &caller, surface, &mut cross_metrics);
        let absent = normalize_error(&missing, &caller, surface, &mut missing_metrics);

        assert_eq!(cross, absent);
        assert_eq!(cross.status, 404);
        assert_eq!(cross.code, "not_authorized");
        assert_eq!(cross.class, ErrorClass::AccessDenied);
        assert_eq!(cross_metrics.counters(), missing_metrics.counters());
        assert_eq!(cross_metrics.traces(), missing_metrics.traces());
        assert_eq!(cross_metrics.traces()[0].normalization_work_units, 64);
        assert_eq!(
            cross_metrics.traces()[0].normalization_tag,
            missing_metrics.traces()[0].normalization_tag
        );
    }
}

#[test]
fn errors_traces_and_metric_labels_never_render_foreign_context() {
    let foreign_tenant = "tenant-super-secret";
    let raw_identifier = "activity-0123456789abcdef";
    let internal =
        InternalError::not_authorized(foreign_tenant, raw_identifier.as_bytes().to_vec());
    let debug = format!("{internal:?}");
    assert!(!debug.contains(foreign_tenant));
    assert!(!debug.contains(raw_identifier));
    assert!(debug.contains("[redacted]"));

    let caller = tenant("caller");
    let mut metrics = BoundedMetrics::default();
    let public = normalize_error(&internal, &caller, Surface::Mcp, &mut metrics);
    let rendered = format!(
        "{} {} {} {:?} {:?}",
        public.code,
        public.message,
        public.status,
        metrics.counters(),
        metrics.traces()
    );
    assert!(!rendered.contains(foreign_tenant));
    assert!(!rendered.contains(raw_identifier));
    assert!(rendered.contains("caller"));
}

#[test]
fn metric_vocabulary_covers_required_operations_with_bounded_labels_only() {
    let tenant = tenant("tenant-a");
    let mut metrics = BoundedMetrics::default();
    let coverage = [
        (MetricKind::SubmissionOutcome, MetricLabel::Executed),
        (MetricKind::UnknownPopulation, MetricLabel::Unknown),
        (MetricKind::UnknownAge, MetricLabel::AgeAtLeastMinute),
        (MetricKind::VerificationLevel, MetricLabel::StateProven),
        (
            MetricKind::BoundaryLatency,
            MetricLabel::BoundaryBackpressured,
        ),
        (MetricKind::ErrorClass, MetricLabel::AccessDenied),
        (MetricKind::PolicyDecision, MetricLabel::Allowed),
        (MetricKind::CapabilityDecision, MetricLabel::Denied),
        (MetricKind::BudgetUtilization, MetricLabel::UtilizationHigh),
        (MetricKind::SubscriptionLag, MetricLabel::Lagging),
        (MetricKind::RateLimitRefusal, MetricLabel::RateExceeded),
    ];
    for (kind, label) in coverage {
        metrics.record(
            BoundedMetricKey {
                tenant: tenant.clone(),
                surface: Surface::Contract,
                kind,
                label,
            },
            1,
        );
    }
    assert_eq!(metrics.counters().len(), coverage.len());
    let rendered = format!("{:?}", metrics.counters());
    assert!(!rendered.contains("activity_id"));
    assert!(!rendered.contains("object_id"));
}

#[test]
fn internal_failure_diagnostics_are_discarded_before_observability() {
    let caller = tenant("caller");
    for internal in [
        InternalError::storage("disk path /secret/foreign/object"),
        InternalError::boundary("peer tenant-b object=hidden"),
        InternalError::internal("panic while reading private-activity-id"),
    ] {
        let mut metrics = BoundedMetrics::default();
        let public = normalize_error(&internal, &caller, Surface::Export, &mut metrics);
        let rendered = format!("{internal:?} {public:?} {:?}", metrics.traces());
        assert!(!rendered.contains("/secret/foreign/object"));
        assert!(!rendered.contains("tenant-b"));
        assert!(!rendered.contains("private-activity-id"));
    }
    assert!(matches!(TenantId::new(""), Err(StoreError::InvalidTenant)));
}
