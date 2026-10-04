use layerx_programs_runtime::test_support::{
    code_section, export_section, func_body, function_section, module, raw_section, type_section,
    TYPE_I32,
};
use layerx_programs_runtime::{ValidationRefusal, WasmEngine};
use wasmi::{AsContextMut, Engine, ExecutionReplayContext, ExecutionStepOutcome, Linker, Module, Store};
use wasmi::core::TrapCode;

fn guest(body: &[u8], memory: bool, table: bool) -> Vec<u8> {
    let mut sections = vec![type_section(&[(&[], &[TYPE_I32])]), function_section(&[0])];
    if table { sections.push(raw_section(4, &[1, 0x70, 1, 1, 1])); }
    if memory { sections.push(raw_section(5, &[1, 1, 1, 1])); }
    sections.push(export_section(&[("run", 0)]));
    sections.push(code_section(&[func_body(&[], body)]));
    module(&sections)
}

fn replay_trap(bytes: &[u8], expected: TrapCode) {
    let engine = Engine::default();
    let compiled = Module::new(&engine, bytes).unwrap_or_else(|error| panic!("real engine module: {error}"));
    let instantiate = || {
        let mut store = Store::new(&engine, ());
        store.enable_execution_replay_observer_with_limits(128, 64 * 1024 * 1024, 64 * 1024 * 1024);
        let instance = Linker::<()>::new(&engine).instantiate(&mut store, &compiled)
            .unwrap_or_else(|error| panic!("real instantiate: {error}"))
            .start(&mut store).unwrap_or_else(|error| panic!("real start: {error}"));
        (store, instance)
    };
    let (mut recorded, instance) = instantiate();
    let result = instance.get_typed_func::<(), i32>(&recorded, "run")
        .unwrap_or_else(|error| panic!("real entrypoint: {error}"))
        .call(&mut recorded, ());
    assert!(result.is_err());
    assert_eq!(recorded.execution_observer_error(), Some(wasmi::ExecutionObserverError::UnsupportedState));
    let trap = recorded.take_execution_trap_record().unwrap_or_else(|| panic!("real terminal trap record"));
    assert_eq!(trap.trap_code.as_ref().map(std::mem::discriminant), Some(std::mem::discriminant(&expected)));
    assert!(!trap.host_trap);
    let (mut restored, instance) = instantiate();
    let replayed = engine.execute_step(ExecutionReplayContext::new(restored.as_context_mut(), instance), &trap.pre)
        .unwrap_or_else(|error| panic!("actual single trap instruction: {error:?}"));
    assert_eq!(replayed, ExecutionStepOutcome::Trapped(trap));
}

#[test]
fn fixed_unreachable_memory_division_overflow_and_indirect_call_traps_replay_exactly() {
    replay_trap(&guest(&[0, 0x0b], false, false), TrapCode::UnreachableCodeReached);
    replay_trap(&guest(&[0x41, 0x7f, 0x28, 0, 0, 0x0b], true, false), TrapCode::MemoryOutOfBounds);
    replay_trap(&guest(&[0x41, 1, 0x41, 0, 0x6d, 0x0b], false, false), TrapCode::IntegerDivisionByZero);
    replay_trap(&guest(&[0x41, 0x80, 0x80, 0x80, 0x80, 0x78, 0x41, 0x7f, 0x6d, 0x0b], false, false),
        TrapCode::IntegerOverflow);
    replay_trap(&guest(&[0x41, 1, 0x11, 0, 0, 0x0b], false, true), TrapCode::TableOutOfBounds);
    replay_trap(&guest(&[0x41, 0, 0x11, 0, 0, 0x0b], false, true), TrapCode::IndirectCallToNull);
}

#[test]
fn reference_operand_and_float_instructions_preserve_real_admission_refusals() {
    let engine = WasmEngine::declared().unwrap_or_else(|error| panic!("declared engine: {error}"));
    let reference = guest(&[0xd0, 0x70, 0xd1, 0x0b], false, false);
    assert!(matches!(engine.validate_v2(&reference), Err(ValidationRefusal::RejectedByEngine { .. })));
    let float = guest(&[0x43, 0, 0, 0, 0, 0x1a, 0x41, 0, 0x0b], false, false);
    assert!(matches!(engine.validate_v2(&float), Err(ValidationRefusal::ForbiddenFloatInstruction)));
}
