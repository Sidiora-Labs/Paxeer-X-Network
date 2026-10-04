use layerx_programs_runtime::test_support::{
    code_section, export_section, func_body, function_section, module, raw_section, type_section,
    OP_END, OP_I32_CONST, TYPE_I32,
};
use layerx_programs_runtime::{
    AuthorizationContext, AuthorizedExecutionRequest, CapabilitySet, CompositionContext,
    CompositionRules, Executor, FeeSchedule, PrincipalId, ProgramCatalog, ProgramId,
    ProgramReplayProfile, ResourceBudget, Storage, UnavailableReceiptOracle, WasmEngine,
};
use std::rc::Rc;

fn guest(trap: bool, start: bool) -> Vec<u8> {
    let mut sections = vec![
        type_section(&[
            (&[TYPE_I32], &[TYPE_I32]),
            (&[TYPE_I32, TYPE_I32], &[TYPE_I32]),
            (&[], &[]),
        ]),
        function_section(if start { &[0, 1, 2] } else { &[0, 1] }),
        raw_section(5, &[1, 1, 1, 1]),
        export_section(&[("layerx_reserve", 0), ("layerx_call", 1)]),
    ];
    let mut exports = sections.pop().unwrap();
    exports[1] += 9;
    exports[2] += 1;
    exports.extend_from_slice(&[6, b'm', b'e', b'm', b'o', b'r', b'y', 2, 0]);
    sections.push(exports);
    if start {
        sections.push(raw_section(8, &[2]));
    }
    let reserve = func_body(&[], &[OP_I32_CONST, 0, OP_END]);
    let entry = func_body(
        &[],
        if trap {
            &[0, OP_END]
        } else {
            &[OP_I32_CONST, 0, OP_END]
        },
    );
    let initializer = func_body(&[], &[OP_I32_CONST, 7, 0x1a, OP_END]);
    let bodies = if start {
        vec![reserve, entry, initializer]
    } else {
        vec![reserve, entry]
    };
    sections.push(code_section(&bodies));
    module(&sections)
}

fn run(
    trap: bool,
    start: bool,
    profile: bool,
) -> layerx_programs_runtime::V2AuthorizedExecutionRecord {
    let engine = WasmEngine::declared().unwrap();
    let module = engine.validate_v2(&guest(trap, start)).unwrap();
    let executor = Executor::new(ResourceBudget::declared(), FeeSchedule::declared());
    let executor = if profile {
        executor.with_program_replay_profile(ProgramReplayProfile::new(128, 1_048_576).unwrap())
    } else {
        executor
    };
    executor
        .execute_authorized_v2(
            &mut Storage::new(),
            AuthorizedExecutionRequest {
                module: &module,
                program: ProgramId::new([1; 32]).unwrap(),
                authorization: AuthorizationContext::new(
                    PrincipalId::new([2; 32]).unwrap(),
                    CapabilitySet::empty(),
                ),
                receipts: &UnavailableReceiptOracle,
                entrypoint: "layerx_call",
                calldata: &[],
                composition: CompositionContext::new(
                    Rc::new(ProgramCatalog::new()),
                    CompositionRules::declared(),
                ),
                response_capacity: 0,
            },
        )
        .unwrap_or_else(|error| panic!("real production producer: {error:?}"))
}

#[test]
fn actual_production_capture_is_opt_in_and_bounded() {
    assert!(run(false, false, false).replay_record().is_none());
    let result = run(false, false, true);
    let record = result
        .replay_record()
        .expect("actual opt-in boundary producer");
    assert_ne!(record.boundary_root(), [0; 32]);
    assert!(!record.canonical_bytes().is_empty());
    assert!(record.canonical_bytes().len() <= 1_048_576);
}
#[test]
fn actual_module_start_is_retained() {
    let started = run(false, true, true);
    let ordinary = run(false, false, true);
    assert_ne!(
        started.replay_record().unwrap().boundary_root(),
        ordinary.replay_record().unwrap().boundary_root()
    );
}
#[test]
fn actual_unreachable_has_a_distinct_retained_terminal_record() {
    let result = run(true, false, true);
    assert!(result.replay_record().is_some());
    assert!(run(true, false, false).replay_record().is_none());
}
#[test]
fn invalid_signed_profile_bounds_are_refused() {
    for bounds in [
        (0, 1),
        (1, 1),
        (128, 0),
        (128, 1_048_577),
        (u32::MAX, 1_048_576),
    ] {
        assert!(ProgramReplayProfile::new(bounds.0, bounds.1).is_err());
    }
}
