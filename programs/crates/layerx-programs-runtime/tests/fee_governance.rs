use layerx_programs_runtime::{
    DemandPricePolicy, FeeGovernance, FeeSchedule, FeeScheduleError, FeeScheduleHistory,
    FeeScheduleParameters, Meter, ResourceBudget,
};

fn parameters(version: u32, occupancy: u64) -> FeeScheduleParameters {
    FeeScheduleParameters {
        version,
        fee_units_per_cpu_fuel: 2,
        fee_units_per_memory_byte: 3,
        fee_units_per_storage_read_byte: 5,
        fee_units_per_storage_write_byte: 7,
        fee_units_per_output_value: 11,
        fee_units_per_output_byte: 13,
        fee_units_per_occupancy_byte_batch: occupancy,
    }
}

fn policy() -> DemandPricePolicy {
    DemandPricePolicy::new(100, 1, 1, 10, 10, 1_000)
        .unwrap_or_else(|error| panic!("policy: {error}"))
}

fn coefficients(schedule: FeeSchedule) -> [u64; 6] {
    [
        schedule.cpu_price(),
        schedule.memory_byte_price(),
        schedule.storage_read_byte_price(),
        schedule.storage_write_byte_price(),
        schedule.output_value_price(),
        schedule.output_byte_price(),
    ]
}

#[test]
fn recorded_schedule_prices_real_meter_without_current_head_fallback() {
    let first = FeeSchedule::new_complete(parameters(1, 100));
    let mut second_parameters = parameters(2, 110);
    second_parameters.fee_units_per_cpu_fuel = 17;
    second_parameters.fee_units_per_storage_read_byte = 19;
    second_parameters.fee_units_per_storage_write_byte = 23;
    let second = FeeSchedule::new_complete(second_parameters);
    let mut history = FeeScheduleHistory::new(first)
        .unwrap_or_else(|error| panic!("history: {error}"));
    history.record(second).unwrap_or_else(|error| panic!("append: {error}"));
    for (version, expected) in [(1, 9 * 2 + 11 * 5 + 13 * 7), (2, 9 * 17 + 11 * 19 + 13 * 23)] {
        let selected = history.select_recorded(version)
            .unwrap_or_else(|error| panic!("selection: {error}"));
        let mut meter = Meter::new(ResourceBudget::declared(), selected);
        meter.charge_cpu(9).unwrap_or_else(|error| panic!("cpu: {error}"));
        meter.charge_storage_read(11).unwrap_or_else(|error| panic!("read: {error}"));
        meter.charge_storage_write(13).unwrap_or_else(|error| panic!("write: {error}"));
        let usage = meter.finish().unwrap_or_else(|error| panic!("finish: {error}"));
        assert_eq!(usage.cpu_fuel, 9);
        assert_eq!(usage.storage_read_bytes, 11);
        assert_eq!(usage.storage_write_bytes, 13);
        assert_eq!(usage.fee_units, expected);
    }
    for version in [0, 3, u32::MAX] {
        assert_eq!(history.select_recorded(version), Err(FeeScheduleError::UnknownVersion { version }));
    }
}

#[test]
fn occupancy_movement_is_monotonic_bounded_and_preserves_other_prices() {
    let observations = [0, 1, 50, 99, 100, 101, 105, 110, 200, u128::from(u64::MAX), u128::MAX];
    for initial_price in [10, 11, 19, 100, 991, 1_000] {
        let initial = FeeSchedule::new_complete(parameters(1, initial_price));
        let mut previous = 0;
        for observed in observations {
            let adjustment = initial.adjust_occupancy_base_price(observed, policy(), 2)
                .unwrap_or_else(|error| panic!("adjustment: {error}"));
            let result = adjustment.resulting_schedule();
            let price = result.occupancy_byte_batch_price();
            assert!(price >= previous);
            previous = price;
            assert!((10..=1_000).contains(&price));
            assert_eq!(adjustment.observed_occupancy_byte_batches(), observed);
            assert_eq!(adjustment.target_occupancy_byte_batches(), 100);
            assert_eq!(adjustment.maximum_change(), initial_price / 10);
            assert_eq!(adjustment.applied_change(), price.abs_diff(initial_price));
            assert!(adjustment.applied_change() <= initial_price / 10);
            assert_eq!(coefficients(result), coefficients(initial));
            assert_eq!(result.version(), if price == initial_price { 1 } else { 2 });
            if observed < 100 { assert!(price <= initial_price); }
            if observed > 100 { assert!(price >= initial_price); }
        }
    }
}

#[test]
fn governance_observations_append_only_effective_versions() {
    let initial = FeeSchedule::new_complete(parameters(1, 100));
    let mut governance = FeeGovernance::new(initial, policy())
        .unwrap_or_else(|error| panic!("governance: {error}"));
    assert_eq!(governance.observe_batch(100).map(|value| value.resulting_schedule()), Ok(initial));
    assert_eq!(governance.history().schedules(), &[initial]);
    let increased = governance.observe_batch(200)
        .unwrap_or_else(|error| panic!("increase: {error}")).resulting_schedule();
    assert_eq!(increased.version(), 2);
    assert_eq!(increased.occupancy_byte_batch_price(), 110);
    let decreased = governance.observe_batch(0)
        .unwrap_or_else(|error| panic!("decrease: {error}")).resulting_schedule();
    assert_eq!(decreased.version(), 3);
    assert_eq!(decreased.occupancy_byte_batch_price(), 99);
    let mut revised_parameters = parameters(4, 100);
    revised_parameters.fee_units_per_cpu_fuel = 29;
    let revised = FeeSchedule::new_complete(revised_parameters);
    governance.record_governed(revised).unwrap_or_else(|error| panic!("governed: {error}"));
    assert_eq!(governance.current(), Ok(revised));
    assert_eq!(governance.history().schedules(), &[initial, increased, decreased, revised]);
    for schedule in governance.history().schedules() {
        assert_eq!(governance.history().select_recorded(schedule.version()), Ok(*schedule));
    }
}

#[test]
fn invalid_coefficients_and_nonconsecutive_history_refuse_without_mutation() {
    let initial = FeeSchedule::new_complete(parameters(1, 100));
    let mut history = FeeScheduleHistory::new(initial)
        .unwrap_or_else(|error| panic!("history: {error}"));
    for field in 0..8 {
        let mut invalid = parameters(2, 100);
        match field {
            0 => invalid.version = 0,
            1 => invalid.fee_units_per_cpu_fuel = 0,
            2 => invalid.fee_units_per_memory_byte = 0,
            3 => invalid.fee_units_per_storage_read_byte = 0,
            4 => invalid.fee_units_per_storage_write_byte = 0,
            5 => invalid.fee_units_per_output_value = 0,
            6 => invalid.fee_units_per_output_byte = 0,
            _ => invalid.fee_units_per_occupancy_byte_batch = 0,
        }
        let invalid = FeeSchedule::new_complete(invalid);
        assert_eq!(FeeScheduleHistory::new(invalid), Err(FeeScheduleError::InvalidSchedule));
        assert_eq!(history.record(invalid), Err(FeeScheduleError::InvalidSchedule));
        assert_eq!(history.schedules(), &[initial]);
    }
    for version in [1, 3, u32::MAX] {
        assert_eq!(history.record(FeeSchedule::new_complete(parameters(version, 100))),
            Err(FeeScheduleError::NonConsecutiveVersion { previous: 1, attempted: version }));
        assert_eq!(history.schedules(), &[initial]);
    }
}

#[test]
fn invalid_demand_policies_and_wrong_next_version_refuse() {
    for values in [
        [0, 1, 1, 10, 10, 1_000], [100, 0, 1, 10, 10, 1_000],
        [100, 1, 0, 10, 10, 1_000], [100, 1, 1, 0, 10, 1_000],
        [100, 1, 11, 10, 10, 1_000], [100, 1, 1, 10, 0, 1_000],
        [100, 1, 1, 10, 1_001, 1_000],
    ] {
        assert_eq!(DemandPricePolicy::new(values[0], values[1], values[2], values[3], values[4], values[5]),
            Err(FeeScheduleError::InvalidDemandPolicy));
    }
    let initial = FeeSchedule::new_complete(parameters(1, 100));
    for attempted in [0, 1, 3, u32::MAX] {
        assert_eq!(initial.adjust_occupancy_base_price(200, policy(), attempted),
            Err(FeeScheduleError::NonConsecutiveVersion { previous: 1, attempted }));
    }
    for occupancy in [1, 1_001] {
        assert_eq!(FeeSchedule::new_complete(parameters(1, occupancy)).adjust_occupancy_base_price(200, policy(), 2),
            Err(FeeScheduleError::InvalidSchedule));
    }
}

#[test]
fn full_width_occupancy_and_fractional_rounding_are_exact() {
    let initial = FeeSchedule::new_complete(parameters(1, 101));
    for (observed, expected) in [(0, 91), (99, 100), (100, 101), (101, 102), (u128::MAX, 111)] {
        assert_eq!(initial.adjust_occupancy_base_price(observed, policy(), 2)
            .map(|value| value.resulting_schedule().occupancy_byte_batch_price()), Ok(expected));
    }
    let widest = FeeSchedule::new_complete(parameters(1, u64::MAX));
    let widest_policy = DemandPricePolicy::new(u64::MAX, u64::MAX, 1, 1, 1, u64::MAX)
        .unwrap_or_else(|error| panic!("wide policy: {error}"));
    assert_eq!(widest.adjust_occupancy_base_price(u128::MAX, widest_policy, 2)
        .map(|value| value.resulting_schedule()), Ok(widest));
}

#[test]
fn version_exhaustion_keeps_history_intact() {
    let initial = FeeSchedule::new_complete(parameters(u32::MAX, 100));
    let mut governance = FeeGovernance::new(initial, policy())
        .unwrap_or_else(|error| panic!("governance: {error}"));
    assert_eq!(governance.observe_batch(200), Err(FeeScheduleError::ArithmeticOverflow));
    assert_eq!(governance.current(), Ok(initial));
    assert_eq!(governance.history().schedules(), &[initial]);
    assert_eq!(governance.record_governed(FeeSchedule::new_complete(parameters(1, 100))),
        Err(FeeScheduleError::NonConsecutiveVersion { previous: u32::MAX, attempted: 1 }));
    assert_eq!(governance.history().schedules(), &[initial]);
}
