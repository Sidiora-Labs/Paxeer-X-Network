use layerx_agentd::budget::{reserve, reserve_until_core_time, BudgetLimiter, CoreTimestampMs, LimitConfig, LimitId, LimitScope, ReservationRequest};

fn limiter() -> BudgetLimiter {
    BudgetLimiter::new(vec![
        LimitConfig {id:LimitId([1;16]),name:"tenant spending".into(),scope:LimitScope::Tenant([1;32]),ceiling:1000,consumed:100},
        LimitConfig {id:LimitId([2;16]),name:"agent spending".into(),scope:LimitScope::Agent([2;32]),ceiling:500,consumed:50},
    ]).expect("real spending limit configuration")
}
fn request(id:u8,amount:u128) -> ReservationRequest {
    ReservationRequest {id:[id;32],amount,expiry_sequence:20,current_sequence:10,
        applicable_limits:vec![LimitId([1;16]),LimitId([2;16])]}
}
#[test]
fn budget_after_counts_the_exact_held_activity_once() {
    let limits=limiter();reserve(&limits,&request(3,100)).expect("reserve actual spending");
    assert_eq!(limits.remaining_after_reservation([3;32],100,10,CoreTimestampMs(100)),Ok(350));
    reserve(&limits,&request(4,50)).expect("reserve additional actual spending");
    assert_eq!(limits.remaining_after_reservation([3;32],100,10,CoreTimestampMs(100)),Ok(300));
}
#[test]
fn budget_after_refuses_foreign_changed_and_expired_holds() {
    let limits=limiter();reserve(&limits,&request(3,100)).expect("reserve actual spending");
    assert!(limits.remaining_after_reservation([4;32],100,10,CoreTimestampMs(100)).is_err());
    assert!(limits.remaining_after_reservation([3;32],99,10,CoreTimestampMs(100)).is_err());
    assert!(limits.remaining_after_reservation([3;32],100,20,CoreTimestampMs(100)).is_err());
    assert!(limits.remaining_after_reservation([3;32],100,10,CoreTimestampMs(0)).is_err());
}
#[test]
fn budget_after_retains_the_authenticated_core_deadline() {
    let limits=limiter();reserve_until_core_time(&limits,&request(3,100),CoreTimestampMs(200),CoreTimestampMs(100))
        .expect("reserve a real time bounded activity");
    assert_eq!(limits.remaining_after_reservation([3;32],100,10,CoreTimestampMs(199)),Ok(350));
    assert!(limits.remaining_after_reservation([3;32],100,10,CoreTimestampMs(200)).is_err());
}

#[test]
fn protocol_remaining_counts_the_outstanding_reservation_inventory() {
    let limits=limiter();reserve(&limits,&request(3,100)).expect("reserve actual spending");
    reserve(&limits,&request(4,50)).expect("reserve additional actual spending");
    assert_eq!(limits.remaining_after_reservation_bound([3;32],100,10,CoreTimestampMs(100),400),Ok(250));
    assert!(limits.remaining_after_reservation_bound([3;32],100,10,CoreTimestampMs(100),149).is_err());
}
