use std::sync::Arc;
use std::thread;

use layerx_agentd::budget::{
    release, reserve, BudgetLimiter, LimitConfig, LimitId, LimitRefusal, LimitScope, ReleaseKind,
    ReservationRequest,
};

fn configs(ceiling: u128) -> Vec<LimitConfig> {
    [
        LimitScope::Tenant([1; 32]),
        LimitScope::Agent([2; 32]),
        LimitScope::Session([3; 32]),
        LimitScope::Capability([4; 32]),
        LimitScope::Counterparty([5; 32]),
    ]
    .into_iter()
    .enumerate()
    .map(|(index, scope)| LimitConfig {
        id: LimitId([u8::try_from(index).unwrap_or(0); 16]),
        name: format!("limit-{index}"),
        scope,
        ceiling,
        consumed: 0,
    })
    .collect()
}

fn request(id: u8, amount: u128) -> ReservationRequest {
    ReservationRequest {
        id: [id; 32],
        amount,
        expiry_sequence: 100,
        current_sequence: 1,
        applicable_limits: (0_u8..5).map(|id| LimitId([id; 16])).collect(),
    }
}

#[test]
fn concurrent_requests_obey_all_five_scopes() {
    let limiter = Arc::new(
        BudgetLimiter::new(configs(1_000)).unwrap_or_else(|error| panic!("limiter: {error:?}")),
    );
    let workers: Vec<_> = (1_u8..=20)
        .map(|id| {
            let limiter = Arc::clone(&limiter);
            thread::spawn(move || reserve(&limiter, &request(id, 100)))
        })
        .collect();
    let accepted = workers
        .into_iter()
        .map(thread::JoinHandle::join)
        .filter(|result| result.as_ref().is_ok_and(std::result::Result::is_ok))
        .count();
    assert_eq!(accepted, 10);
    assert_eq!(limiter.held_reservations(), Ok(50));
}

#[test]
fn refusal_names_limit_and_leaks_no_reservation() {
    let limiter =
        BudgetLimiter::new(configs(100)).unwrap_or_else(|error| panic!("limiter: {error:?}"));
    for id in 0_u16..1_000 {
        let mut request = request(u8::try_from(id % 251).unwrap_or(0), 101);
        request.id[..2].copy_from_slice(&id.to_be_bytes());
        let Err(error) = reserve(&limiter, &request) else {
            panic!("reservation {id} was accepted past the ceiling");
        };
        match error {
            LimitRefusal::Exceeded {
                name,
                ceiling,
                consumed,
                requested,
                ..
            } => {
                assert_eq!(name, "limit-0");
                assert_eq!(ceiling, 100);
                assert_eq!(consumed, 0);
                assert_eq!(requested, 101);
            }
            other => panic!("unexpected refusal: {other:?}"),
        }
    }
    assert_eq!(limiter.held_reservations(), Ok(0));
}

#[test]
fn terminal_and_expiry_release_are_deterministic_unknown_is_held() {
    let limiter =
        BudgetLimiter::new(configs(1_000)).unwrap_or_else(|error| panic!("limiter: {error:?}"));
    reserve(&limiter, &request(1, 100)).unwrap_or_else(|error| panic!("reserve: {error:?}"));
    assert_eq!(
        release(&limiter, [1; 32], ReleaseKind::Unknown, 200),
        Ok(false)
    );
    assert_eq!(
        release(&limiter, [1; 32], ReleaseKind::Executed, 2),
        Ok(true)
    );
    assert_eq!(limiter.consumed(LimitId([0; 16])), Ok(100));
}
