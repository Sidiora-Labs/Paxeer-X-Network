use layerx_programs_runtime::meter::inject::GENESIS_METERING_SCHEDULE_VERSION;
use layerx_programs_runtime::test_support::add_module;
use layerx_programs_runtime::{
    replay_recorded_execution_with_fee_history, DemandPricePolicy, FeeGovernance, FeeSchedule,
    FeeScheduleError, FeeScheduleHistory, FeeScheduleParameters, Meter, RecordedExecution,
    ReplayRefusal, ResourceBudget, WasmValue, ABI_V2_VERSION, RUNTIME_VERSION,
};

fn schedule(version: u32, occupancy_price: u64) -> FeeSchedule {
    FeeSchedule::new_complete(FeeScheduleParameters {
        version,
        fee_units_per_cpu_fuel: 3,
        fee_units_per_memory_byte: 5,
        fee_units_per_storage_read_byte: 7,
        fee_units_per_storage_write_byte: 11,
        fee_units_per_output_value: 13,
        fee_units_per_output_byte: 17,
        fee_units_per_occupancy_byte_batch: occupancy_price,
    })
}

fn policy() -> DemandPricePolicy {
    DemandPricePolicy::new(100, 1, 1, 8, 1, 1_000_000).expect("valid policy")
}

fn priced_execution(prices: FeeSchedule) -> u128 {
    let mut meter = Meter::new(ResourceBudget::declared(), prices);
    meter.charge_cpu(250).expect("cpu within budget");
    meter.charge_storage_read(64).expect("read within budget");
    meter.charge_storage_write(32).expect("write within budget");
    meter.finish().expect("metered usage").fee_units
}

#[test]
fn versioned_schedule_names_every_coefficient_and_unit() {
    let declared = FeeSchedule::declared();
    assert!(declared.is_valid());
    assert_eq!(declared.version(), 1);
    let governed = schedule(4, 19);
    assert_eq!(governed.version(), 4);
    assert_eq!(governed.cpu_price(), 3);
    assert_eq!(governed.memory_byte_price(), 5);
    assert_eq!(governed.storage_read_byte_price(), 7);
    assert_eq!(governed.storage_write_byte_price(), 11);
    assert_eq!(governed.output_value_price(), 13);
    assert_eq!(governed.output_byte_price(), 17);
    assert_eq!(governed.occupancy_byte_batch_price(), 19);
    assert!(!schedule(0, 19).is_valid());
    assert!(!schedule(2, 0).is_valid());
    assert_eq!(
        FeeScheduleHistory::new(schedule(0, 19)),
        Err(FeeScheduleError::InvalidSchedule)
    );
    assert_history_is_append_only();
}

fn assert_history_is_append_only() {
    let mut history = FeeScheduleHistory::new(schedule(1, 10)).expect("genesis");
    history.record(schedule(2, 20)).expect("next version");
    assert_eq!(
        history.record(schedule(2, 30)),
        Err(FeeScheduleError::NonConsecutiveVersion {
            previous: 2,
            attempted: 2
        })
    );
    assert_eq!(
        history.record(schedule(4, 30)),
        Err(FeeScheduleError::NonConsecutiveVersion {
            previous: 2,
            attempted: 4
        })
    );
    assert_eq!(
        history.record(schedule(1, 30)),
        Err(FeeScheduleError::NonConsecutiveVersion {
            previous: 2,
            attempted: 1
        })
    );
    assert_eq!(
        history.record(schedule(3, 0)),
        Err(FeeScheduleError::InvalidSchedule)
    );
    assert_eq!(history.schedules(), &[schedule(1, 10), schedule(2, 20)]);
}

#[test]
fn pending_schedule_visible_before_exact_activation_without_retroactive_change() {
    let mut governance = FeeGovernance::new(schedule(1, 10), policy()).expect("governance");
    let before = governance.current().expect("current");
    let charged_under_one = priced_execution(before);
    assert_eq!(before.version(), 1);

    governance
        .record_governed(schedule(2, 10).with_output_byte_price(29))
        .expect("governed change");
    let after = governance.current().expect("current");
    assert_eq!(after.version(), 2);
    assert_eq!(after.output_byte_price(), 29);

    let historical = governance.history().select_recorded(1).expect("v1");
    assert_eq!(historical, before);
    assert_eq!(priced_execution(historical), charged_under_one);
    assert_eq!(
        governance.record_governed(schedule(2, 11)),
        Err(FeeScheduleError::NonConsecutiveVersion {
            previous: 2,
            attempted: 2
        })
    );
    assert_eq!(governance.history().schedules().len(), 2);
}

#[test]
fn receipt_version_selects_historical_schedule_and_refuses_unknown() {
    let mut history = FeeScheduleHistory::new(schedule(1, 10)).expect("genesis");
    history.record(schedule(2, 20)).expect("v2");
    history.record(schedule(3, 30)).expect("v3");
    for version in 1..=3 {
        let selected = history.select_recorded(version).expect("recorded");
        assert_eq!(selected.version(), version);
        assert_eq!(
            selected.occupancy_byte_batch_price(),
            u64::from(version) * 10
        );
    }
    assert_eq!(
        history.select_recorded(0),
        Err(FeeScheduleError::UnknownVersion { version: 0 })
    );
    assert_eq!(
        history.select_recorded(4),
        Err(FeeScheduleError::UnknownVersion { version: 4 })
    );

    let wasm = add_module();
    let args = [WasmValue::I32(20), WasmValue::I32(22)];
    let receipt = |fee_schedule_version| RecordedExecution {
        runtime_version: RUNTIME_VERSION,
        abi_version: ABI_V2_VERSION,
        fee_schedule_version,
        metering_schedule_version: GENESIS_METERING_SCHEDULE_VERSION,
        wasm: &wasm,
        export: "add",
        args: &args,
    };
    let first = replay_recorded_execution_with_fee_history(&receipt(1), &history)
        .unwrap_or_else(|error| panic!("v1 replay refused: {error}"));
    let again = replay_recorded_execution_with_fee_history(&receipt(1), &history)
        .unwrap_or_else(|error| panic!("v1 replay refused: {error}"));
    assert_eq!(first, again);
    assert_eq!(
        replay_recorded_execution_with_fee_history(&receipt(4), &history),
        Err(ReplayRefusal::UnknownFeeScheduleVersion { version: 4 })
    );
}

#[test]
fn demand_price_from_occupancy_is_bounded_per_batch() {
    let base = schedule(1, 1_000);
    let at_target = base.adjust_occupancy_base_price(100, policy(), 2).expect("target");
    assert_eq!(at_target.applied_change(), 0);
    assert_eq!(at_target.resulting_schedule(), base);
    assert_eq!(at_target.maximum_change(), 125);

    let high = base.adjust_occupancy_base_price(110, policy(), 2).expect("high");
    assert_eq!(high.observed_occupancy_byte_batches(), 110);
    assert_eq!(high.target_occupancy_byte_batches(), 100);
    assert_eq!(high.applied_change(), 100);
    assert_eq!(high.resulting_schedule().version(), 2);
    assert_eq!(high.resulting_schedule().occupancy_byte_batch_price(), 1_100);
    assert_eq!(high.resulting_schedule().cpu_price(), base.cpu_price());

    let low = base.adjust_occupancy_base_price(90, policy(), 2).expect("low");
    assert_eq!(low.applied_change(), 100);
    assert_eq!(low.resulting_schedule().occupancy_byte_batch_price(), 900);

    let zero = base.adjust_occupancy_base_price(0, policy(), 2).expect("zero");
    assert_eq!(zero.applied_change(), 125);
    assert_eq!(zero.resulting_schedule().occupancy_byte_batch_price(), 875);

    let full = base
        .adjust_occupancy_base_price(u128::MAX, policy(), 2)
        .expect("full range");
    assert_eq!(full.applied_change(), 125);
    assert_eq!(full.resulting_schedule().occupancy_byte_batch_price(), 1_125);
    assert_bounded_movement_across_consecutive_batches();
}

#[test]
fn demand_price_cap_floor_and_overflow_are_refused() {
    let bounded = DemandPricePolicy::new(100, 1, 1, 8, 950, 1_050).expect("bounded");
    let base = schedule(1, 1_000);
    let capped = base.adjust_occupancy_base_price(u128::MAX, bounded, 2).expect("cap");
    assert_eq!(capped.resulting_schedule().occupancy_byte_batch_price(), 1_050);
    assert_eq!(capped.applied_change(), 50);
    let floored = base.adjust_occupancy_base_price(0, bounded, 2).expect("floor");
    assert_eq!(floored.resulting_schedule().occupancy_byte_batch_price(), 950);

    assert_eq!(
        schedule(1, 1_100).adjust_occupancy_base_price(100, bounded, 2),
        Err(FeeScheduleError::InvalidSchedule)
    );
    assert_eq!(
        base.adjust_occupancy_base_price(110, bounded, 3),
        Err(FeeScheduleError::NonConsecutiveVersion {
            previous: 1,
            attempted: 3
        })
    );
    assert_eq!(
        DemandPricePolicy::new(0, 1, 1, 8, 1, 10),
        Err(FeeScheduleError::InvalidDemandPolicy)
    );
    assert_eq!(
        DemandPricePolicy::new(100, 1, 9, 8, 1, 10),
        Err(FeeScheduleError::InvalidDemandPolicy)
    );
    assert_eq!(
        DemandPricePolicy::new(100, 1, 1, 8, 11, 10),
        Err(FeeScheduleError::InvalidDemandPolicy)
    );

    let mut exhausted =
        FeeGovernance::new(schedule(u32::MAX, 1_000), policy()).expect("last version");
    assert_eq!(
        exhausted.observe_batch(110),
        Err(FeeScheduleError::ArithmeticOverflow)
    );
    assert_eq!(exhausted.history().schedules().len(), 1);
}

fn assert_bounded_movement_across_consecutive_batches() {
    let mut governance = FeeGovernance::new(schedule(1, 1_000), policy()).expect("governance");
    for occupancy in [0_u128, 400, 100, 7, u128::MAX, 100, 99, 101] {
        let before = governance.current().expect("current");
        let adjustment = governance.observe_batch(occupancy).expect("observe");
        let after = governance.current().expect("current");
        let moved = before
            .occupancy_byte_batch_price()
            .abs_diff(after.occupancy_byte_batch_price());
        assert_eq!(moved, adjustment.applied_change());
        assert!(moved <= before.occupancy_byte_batch_price() / 8);
        if moved == 0 {
            assert_eq!(after.version(), before.version());
        } else {
            assert_eq!(after.version(), before.version() + 1);
        }
    }
}

#[test]
fn multi_schedule_history_replay_reprices_every_activity() {
    let mut governance = FeeGovernance::new(schedule(1, 1_000), policy()).expect("governance");
    let mut receipts = Vec::new();
    let current = governance.current().expect("v1");
    receipts.push((current.version(), priced_execution(current)));
    governance.observe_batch(150).expect("demand v2");
    let current = governance.current().expect("v2");
    receipts.push((current.version(), priced_execution(current)));
    let next = current.version() + 1;
    governance
        .record_governed(
            FeeSchedule::new_complete(FeeScheduleParameters {
                version: next,
                fee_units_per_cpu_fuel: 9,
                fee_units_per_memory_byte: 5,
                fee_units_per_storage_read_byte: 7,
                fee_units_per_storage_write_byte: 11,
                fee_units_per_output_value: 13,
                fee_units_per_output_byte: 17,
                fee_units_per_occupancy_byte_batch: current.occupancy_byte_batch_price(),
            }),
        )
        .expect("governed v3");
    let current = governance.current().expect("v3");
    receipts.push((current.version(), priced_execution(current)));

    assert_eq!(
        receipts.iter().map(|(version, _)| *version).collect::<Vec<_>>(),
        vec![1, 2, 3]
    );
    assert_ne!(receipts[1].1, receipts[2].1);
    for (version, charged) in &receipts {
        let recorded = governance
            .history()
            .select_recorded(*version)
            .expect("recorded version");
        assert_eq!(priced_execution(recorded), *charged);
    }
    let replayed = governance.history().clone();
    assert_eq!(&replayed, governance.history());

    let wasm = add_module();
    let args = [WasmValue::I32(20), WasmValue::I32(22)];
    let mut evidence = Vec::new();
    for (version, _) in &receipts {
        let record = RecordedExecution {
            runtime_version: RUNTIME_VERSION,
            abi_version: ABI_V2_VERSION,
            fee_schedule_version: *version,
            metering_schedule_version: GENESIS_METERING_SCHEDULE_VERSION,
            wasm: &wasm,
            export: "add",
            args: &args,
        };
        let first = replay_recorded_execution_with_fee_history(&record, governance.history())
            .unwrap_or_else(|error| panic!("replay of v{version} refused: {error}"));
        let second = replay_recorded_execution_with_fee_history(&record, &replayed)
            .unwrap_or_else(|error| panic!("replay of v{version} refused: {error}"));
        assert_eq!(first, second);
        evidence.push(first);
    }
    assert_ne!(evidence[1], evidence[2]);
}
