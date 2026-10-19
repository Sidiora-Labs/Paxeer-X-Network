use layerx_agentd::limits::{
    CounterLedger, LimitConfig, LimitId, LimitScope, RateLimiter, RateRequest, Refusal,
    COUNTER_CONSISTENCY_MODEL,
};

fn id(value: &str) -> LimitId {
    LimitId::new(value).unwrap_or_else(|error| panic!("limit identifier {value}: {error:?}"))
}

fn request(at_ms: u64, cost: u64) -> RateRequest {
    RateRequest {
        tenant: "tenant-a".to_owned(),
        agent: "agent-a".to_owned(),
        session: "session-a".to_owned(),
        capability: "orders.submit".to_owned(),
        operation_class: "write".to_owned(),
        logical_time_ms: at_ms,
        cost,
    }
}

fn config(id_value: &str, scope: LimitScope, limit: u64) -> LimitConfig {
    LimitConfig {
        id: id(id_value),
        scope,
        limit,
        window_ms: 1_000,
    }
}

fn all_scope_configs() -> Vec<LimitConfig> {
    vec![
        config(
            "01-tenant",
            LimitScope::Tenant {
                tenant: "tenant-a".to_owned(),
            },
            100,
        ),
        config(
            "02-agent",
            LimitScope::Agent {
                tenant: "tenant-a".to_owned(),
                agent: "agent-a".to_owned(),
            },
            10,
        ),
        config(
            "03-session",
            LimitScope::Session {
                tenant: "tenant-a".to_owned(),
                session: "session-a".to_owned(),
            },
            8,
        ),
        config(
            "04-capability",
            LimitScope::Capability {
                tenant: "tenant-a".to_owned(),
                capability: "orders.submit".to_owned(),
            },
            6,
        ),
        config(
            "05-operation",
            LimitScope::OperationClass {
                tenant: "tenant-a".to_owned(),
                operation_class: "write".to_owned(),
            },
            4,
        ),
    ]
}

#[test]
fn applies_every_matching_scope_and_refuses_atomically() {
    let limiter = RateLimiter::new(all_scope_configs(), CounterLedger::shared())
        .unwrap_or_else(|error| panic!("layered configuration: {error:?}"));

    let admitted = limiter
        .admit(&request(100, 4))
        .unwrap_or_else(|refusal| panic!("first burst: {refusal:?}"));
    assert_eq!(
        admitted
            .applied_limits
            .iter()
            .map(LimitId::as_str)
            .collect::<Vec<_>>(),
        vec![
            "01-tenant",
            "02-agent",
            "03-session",
            "04-capability",
            "05-operation"
        ]
    );
    assert_eq!(admitted.ledger_revision, 1);

    let Err(refusal) = limiter.admit(&request(100, 1)) else {
        panic!("operation-class ceiling must refuse immediately");
    };
    match refusal {
        Refusal::Exceeded {
            limit,
            window,
            remaining,
            retry_after_ms,
        } => {
            assert_eq!(limit.id.as_str(), "05-operation");
            assert_eq!(window.start_ms, 0);
            assert_eq!(window.end_ms, 1_000);
            assert_eq!(remaining, 0);
            assert_eq!(retry_after_ms, 900);
        }
        other => panic!("expected typed exceeded refusal, got {other:?}"),
    }

    let utilization = limiter
        .utilization("tenant-a", 100)
        .unwrap_or_else(|refusal| panic!("utilization: {refusal:?}"));
    assert_eq!(utilization.len(), 5);
    assert!(utilization.iter().all(|entry| entry.used == 4));
    assert!(utilization.iter().all(|entry| entry.ledger_revision == 1));
}

#[test]
fn each_scope_is_independently_selected_by_authenticated_dimensions() {
    let limiter = RateLimiter::new(all_scope_configs(), CounterLedger::shared())
        .unwrap_or_else(|error| panic!("layered configuration: {error:?}"));
    let mut mismatched = request(10, 1);
    mismatched.agent = "agent-b".to_owned();
    mismatched.session = "session-b".to_owned();
    mismatched.capability = "orders.read".to_owned();
    mismatched.operation_class = "read".to_owned();

    let admitted = limiter
        .admit(&mismatched)
        .unwrap_or_else(|refusal| panic!("tenant limit still applies: {refusal:?}"));
    assert_eq!(
        admitted
            .applied_limits
            .iter()
            .map(LimitId::as_str)
            .collect::<Vec<_>>(),
        vec!["01-tenant"]
    );

    let utilization = limiter
        .utilization("tenant-a", 10)
        .unwrap_or_else(|refusal| panic!("tenant utilization: {refusal:?}"));
    let used = utilization
        .iter()
        .map(|entry| (entry.config.id.as_str(), entry.used))
        .collect::<Vec<_>>();
    assert_eq!(
        used,
        vec![
            ("01-tenant", 1),
            ("02-agent", 0),
            ("03-session", 0),
            ("04-capability", 0),
            ("05-operation", 0)
        ]
    );
}

#[test]
fn boundary_bursts_reset_only_at_the_next_logical_window() {
    let limiter = RateLimiter::new(
        vec![config(
            "tenant",
            LimitScope::Tenant {
                tenant: "tenant-a".to_owned(),
            },
            3,
        )],
        CounterLedger::shared(),
    )
    .unwrap_or_else(|refusal| panic!("tenant limit: {refusal:?}"));

    for _ in 0..3 {
        limiter
            .admit(&request(999, 1))
            .unwrap_or_else(|refusal| panic!("burst within the first window: {refusal:?}"));
    }
    assert_eq!(
        limiter.admit(&request(999, 1)),
        Err(Refusal::Exceeded {
            limit: Box::new(config(
                "tenant",
                LimitScope::Tenant {
                    tenant: "tenant-a".to_owned(),
                },
                3,
            )),
            window: layerx_agentd::limits::Window {
                start_ms: 0,
                end_ms: 1_000,
            },
            remaining: 0,
            retry_after_ms: 1,
        })
    );

    limiter
        .admit(&request(1_000, 1))
        .unwrap_or_else(|refusal| panic!("next logical window: {refusal:?}"));
    let utilization = limiter
        .utilization("tenant-a", 1_000)
        .unwrap_or_else(|refusal| panic!("new-window utilization: {refusal:?}"));
    assert_eq!(utilization[0].used, 1);
    assert_eq!(utilization[0].remaining, 2);
}

#[test]
fn shared_ledger_prevents_two_limiter_instances_from_diverging() {
    assert!(COUNTER_CONSISTENCY_MODEL.contains("Independent ledgers are unsupported"));
    let ledger = CounterLedger::shared();
    let configs = vec![config(
        "tenant",
        LimitScope::Tenant {
            tenant: "tenant-a".to_owned(),
        },
        3,
    )];
    let first = RateLimiter::new(configs.clone(), ledger.clone())
        .unwrap_or_else(|refusal| panic!("first instance: {refusal:?}"));
    let second = RateLimiter::new(configs, ledger)
        .unwrap_or_else(|refusal| panic!("second instance: {refusal:?}"));

    assert_eq!(
        first
            .admit(&request(500, 2))
            .unwrap_or_else(|refusal| panic!("first instance admits: {refusal:?}"))
            .ledger_revision,
        1
    );
    assert_eq!(
        second
            .admit(&request(500, 1))
            .unwrap_or_else(|refusal| panic!("second instance shared usage: {refusal:?}"))
            .ledger_revision,
        2
    );
    assert!(matches!(
        first.admit(&request(500, 1)),
        Err(Refusal::Exceeded { remaining: 0, .. })
    ));

    let first_view = first
        .utilization("tenant-a", 500)
        .unwrap_or_else(|refusal| panic!("first view: {refusal:?}"));
    let second_view = second
        .utilization("tenant-a", 500)
        .unwrap_or_else(|refusal| panic!("second view: {refusal:?}"));
    assert_eq!(first_view, second_view);
    assert_eq!(first_view[0].used, 3);
    assert_eq!(first_view[0].ledger_revision, 2);
}

#[test]
fn invalid_and_unmatched_requests_return_typed_refusals() {
    assert!(matches!(
        RateLimiter::new(Vec::new(), CounterLedger::shared()),
        Err(Refusal::InvalidConfiguration)
    ));

    let limiter = RateLimiter::new(
        vec![config(
            "tenant",
            LimitScope::Tenant {
                tenant: "tenant-a".to_owned(),
            },
            1,
        )],
        CounterLedger::shared(),
    )
    .unwrap_or_else(|refusal| panic!("tenant limit: {refusal:?}"));
    assert_eq!(
        limiter.admit(&RateRequest {
            cost: 0,
            ..request(0, 1)
        }),
        Err(Refusal::InvalidRequest)
    );
    assert_eq!(
        limiter.admit(&RateRequest {
            tenant: "tenant-b".to_owned(),
            ..request(0, 1)
        }),
        Err(Refusal::NoApplicableLimits)
    );
}
