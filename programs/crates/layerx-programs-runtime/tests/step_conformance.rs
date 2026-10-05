use layerx_programs_runtime::test_support::{
    code_section, export_section, func_body, function_section, module, raw_section, type_section,
    TYPE_I32, TYPE_I64,
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
    replay_trap_in_engine(bytes, expected, &Engine::default());
}

fn replay_trap_in_engine(bytes: &[u8], expected: TrapCode, engine: &Engine) {
    let compiled = Module::new(engine, bytes).unwrap_or_else(|error| panic!("real engine module: {error}"));
    let instantiate = || {
        let mut store = Store::new(engine, ());
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

#[test]
fn fixed_indirect_signature_and_stack_exhaustion_traps_replay_exactly() {
    let wrong_signature = module(&[
        type_section(&[(&[], &[TYPE_I32]), (&[], &[TYPE_I64])]),
        function_section(&[0, 1]), raw_section(4, &[1, 0x70, 1, 1, 1]),
        export_section(&[("run", 0)]),
        raw_section(9, &[1, 0, 0x41, 0, 0x0b, 1, 1]),
        code_section(&[func_body(&[], &[0x41, 0, 0x11, 0, 0, 0x0b]),
            func_body(&[], &[0x42, 0, 0x0b])]),
    ]);
    replay_trap(&wrong_signature, TrapCode::BadSignature);
    let mut config = wasmi::Config::default();
    config.set_stack_limits(wasmi::StackLimits::new(128, 4096, 8).unwrap());
    let engine = Engine::new(&config);
    replay_trap_in_engine(&guest(&[0x10, 0, 0x0b], false, false),
        TrapCode::StackOverflow, &engine);
}

#[test]
fn fixed_bulk_memory_and_passive_element_bounds_traps_replay_exactly() {
    for body in [
        &[0x41, 0x7f, 0x41, 0, 0x41, 1, 0xfc, 11, 0, 0x41, 0, 0x0b][..],
        &[0x41, 0, 0x41, 0x7f, 0x41, 1, 0xfc, 10, 0, 0, 0x41, 0, 0x0b],
    ] {
        replay_trap(&guest(body, true, false), TrapCode::MemoryOutOfBounds);
    }
    let dropped_data = module(&[
        type_section(&[(&[], &[TYPE_I32])]), function_section(&[0]),
        raw_section(5, &[1, 1, 1, 1]), export_section(&[("run", 0)]),
        raw_section(12, &[1]),
        code_section(&[func_body(&[], &[0xfc, 9, 0, 0x41, 0, 0x41, 0, 0x41, 1,
            0xfc, 8, 0, 0, 0x41, 0, 0x0b])]),
        raw_section(11, &[1, 1, 1, 42]),
    ]);
    replay_trap(&dropped_data, TrapCode::MemoryOutOfBounds);
    let dropped_element = module(&[
        type_section(&[(&[], &[TYPE_I32])]), function_section(&[0, 0]),
        raw_section(4, &[1, 0x70, 1, 1, 1]), export_section(&[("run", 0)]),
        raw_section(9, &[1, 1, 0, 1, 1]),
        code_section(&[func_body(&[], &[0xfc, 13, 0, 0x41, 0, 0x41, 0, 0x41, 1,
            0xfc, 12, 0, 0, 0x41, 0, 0x0b]), func_body(&[], &[0x41, 42, 0x0b])]),
    ]);
    replay_trap(&dropped_element, TrapCode::TableOutOfBounds);
    replay_trap(&guest(&[0x41, 1, 0x41, 0, 0x41, 1, 0xfc, 14, 0, 0,
        0x41, 0, 0x0b], false, true), TrapCode::TableOutOfBounds);
}

#[test]
fn reference_stack_table_operations_preserve_declared_integer_profile_refusals() {
    let engine = WasmEngine::declared().unwrap();
    for body in [
        &[0xd0, 0x70, 0x1a, 0x41, 0, 0x0b][..],
        &[0x41, 0, 0x25, 0, 0x1a, 0x41, 0, 0x0b],
        &[0x41, 0, 0xd0, 0x70, 0x26, 0, 0x41, 0, 0x0b],
        &[0xd0, 0x70, 0x41, 0, 0xfc, 15, 0, 0x0b],
        &[0x41, 0, 0xd0, 0x70, 0x41, 1, 0xfc, 17, 0, 0x41, 0, 0x0b],
    ] {
        assert!(matches!(engine.validate_v2(&guest(body, false, true)),
            Err(ValidationRefusal::RejectedByEngine { .. })));
    }
    let declared_reference = module(&[
        type_section(&[(&[], &[TYPE_I32])]), function_section(&[0]),
        export_section(&[("run", 0)]), raw_section(9, &[1, 3, 0, 1, 0]),
        code_section(&[func_body(&[], &[0xd2, 0, 0x1a, 0x41, 0, 0x0b])]),
    ]);
    assert!(matches!(engine.validate_v2(&declared_reference),
        Err(ValidationRefusal::RejectedByEngine { .. })));
}

#[test]
fn actual_runtime_meter_exhaustion_before_capture_preserves_resource_refusal() {
    use layerx_programs_runtime::{
        AuthorizationContext, AuthorizedExecutionRequest, CapabilitySet, CompositionContext,
        CompositionRules, Executor, FeeSchedule, PrincipalId, ProgramCatalog, ProgramId,
        ProgramReplayProfile, ResourceBudget, Storage, UnavailableReceiptOracle, ExecutionError,
        ExecutionFault, MeterRefusal, ResourceKind,
    };
    let mut exports = export_section(&[("layerx_reserve", 0), ("layerx_call", 1)]);
    exports[1] += 9;
    exports[2] += 1;
    exports.extend_from_slice(&[6, b'm', b'e', b'm', b'o', b'r', b'y', 2, 0]);
    let bytes = module(&[
        type_section(&[(&[TYPE_I32], &[TYPE_I32]), (&[TYPE_I32, TYPE_I32], &[TYPE_I32])]),
        function_section(&[0, 1]), raw_section(5, &[1, 1, 1, 1]), exports,
        code_section(&[func_body(&[], &[0x41, 0, 0x0b]),
            func_body(&[], &[0x03, 0x40, 0x0c, 0, 0x0b, 0x41, 0, 0x0b])]),
    ]);
    let engine = WasmEngine::declared().unwrap();
    let validated = engine.validate_v2(&bytes).unwrap();
    let declared = ResourceBudget::declared();
    let budget = ResourceBudget::new_complete(2, declared.memory_bytes(),
        declared.storage_read_bytes(), declared.storage_write_bytes(), declared.output_values(),
        declared.output_bytes(), declared.table_elements());
    let executor = Executor::new(budget, FeeSchedule::declared())
        .with_program_replay_profile(ProgramReplayProfile::new(128, 1_048_576).unwrap());
    let mut storage = Storage::new();
    let original = storage.replay_state_bytes(1_048_576).unwrap();
    let result = executor.execute_authorized_v2(&mut storage, AuthorizedExecutionRequest {
        module: &validated, program: ProgramId::new([1; 32]).unwrap(),
        authorization: AuthorizationContext::new(PrincipalId::new([2; 32]).unwrap(), CapabilitySet::empty()),
        receipts: &UnavailableReceiptOracle, entrypoint: "layerx_call", calldata: &[],
        composition: CompositionContext::new(std::rc::Rc::new(ProgramCatalog::new()), CompositionRules::declared()),
        response_capacity: 0,
    });
    assert!(matches!(result, Err(ExecutionError::Fault(ExecutionFault::Resource {
        refusal: MeterRefusal::BudgetExceeded { resource: ResourceKind::Cpu, limit: 2, attempted }
    })) if attempted > 2));
    assert_eq!(storage.replay_state_bytes(1_048_576).unwrap(), original);
}

#[test]
fn metered_fuel_exhaustion_golden_replays_every_step_and_its_host_trap_exactly() {
    use layerx_programs_runtime::execute::{
        instantiate_market_sandbox_untrusted, market_sandbox_input_digest,
        observe_market_sandbox_step, MarketSandboxRequest,
    };
    use layerx_programs_runtime::portable_replay::PortableBoundary;
    use layerx_programs_runtime::replay::{
        market_sandbox_baseline_root, market_sandbox_namespace, MarketSandboxReplayAuthority,
    };
    use layerx_programs_runtime::{
        ExecutionFault, FeeSchedule, PrincipalId, ProgramId, ProgramReplayProfile,
        ResourceBudget, Storage, RUNTIME_VERSION,
    };
    use sha2::{Digest, Sha256};
    let mut body = [0x41, 0, 0x1a].repeat(20_000);
    body.extend_from_slice(&[0x41, 0, 0x0b]);
    let wasm = module(&[
        type_section(&[(&[], &[TYPE_I32])]),
        function_section(&[0]),
        export_section(&[("compute", 0)]),
        code_section(&[func_body(&[], &body)]),
    ]);
    let validated = WasmEngine::declared().unwrap().validate_versioned(2, &wasm).unwrap();
    let program = ProgramId::new(Sha256::digest(&wasm).into()).unwrap();
    let tenant = PrincipalId::new(Sha256::digest(b"fuel-golden-tenant").into()).unwrap();
    let lease: [u8; 32] = Sha256::digest(b"fuel-golden-lease").into();
    let baseline = Storage::new();
    let fees = FeeSchedule::declared();
    let authority = MarketSandboxReplayAuthority {
        profile_binding: Sha256::digest(b"fuel-golden-profile").into(),
        namespace: market_sandbox_namespace(program, lease).unwrap(),
        lease_id: lease,
        namespace_limit: 1024,
        program,
        tenant,
        payment_account: tenant.bytes(),
        code_hash: validated.code_hash(),
        input_digest: market_sandbox_input_digest("compute", &[]).unwrap(),
        runtime_version: RUNTIME_VERSION,
        abi_version: 2,
        fee_schedule_version: fees.version(),
        metering_schedule_version: validated.metering_schedule_version(),
        budget: ResourceBudget::new_complete(20_000, 65_536, 1024, 1024, 1, 64, 0),
        fees,
        fee_budget: 1_000_000_000,
        baseline_state_root: market_sandbox_baseline_root(&baseline).unwrap(),
        baseline_storage: baseline,
    };
    let execute = || {
        instantiate_market_sandbox_untrusted(&validated, &authority)
            .unwrap_or_else(|error| panic!("market sandbox instance: {error:?}"))
            .call_market_sandbox_untrusted(MarketSandboxRequest {
                module: &validated,
                entrypoint: "compute",
                args: &[],
                authority: &authority,
                replay_profile: ProgramReplayProfile::new(128, 1_048_576).unwrap(),
            })
            .unwrap_or_else(|error| panic!("metered exhaustion capture: {error:?}"))
    };
    let execution = execute();
    assert_eq!(execution.terminal_fault, Some(ExecutionFault::OutOfFuel));
    assert_eq!(execution.record.terminal_status(), 2);
    assert!(execution.values.is_empty());
    assert!(execution.usage.cpu_fuel <= 20_000);
    assert_eq!(execution.record.boundary_count() as usize, execution.boundary_leaves.len());
    assert_eq!(execute().record.canonical_bytes(), execution.record.canonical_bytes());
    let boundaries: Vec<PortableBoundary> = execution.boundary_leaves.iter()
        .map(|leaf| PortableBoundary::decode_untrusted(leaf, 1_048_576).unwrap())
        .collect();
    assert!(boundaries.len() >= 2);
    for pair in boundaries.windows(2) {
        assert!(pair[0].trap.is_none());
        let observed = observe_market_sandbox_step(&validated, &pair[0], &authority, 1_048_576)
            .unwrap_or_else(|error| panic!("single metered step: {error:?}"));
        let mut post = pair[1].clone();
        if observed.trap.is_none() {
            post.trap = None;
        }
        assert_eq!(observed.reencode_untrusted(1_048_576).unwrap(),
            post.reencode_untrusted(1_048_576).unwrap());
    }
    let terminal = boundaries.last().unwrap();
    let trap = terminal.trap.as_ref().unwrap_or_else(|| panic!("recorded fuel trap"));
    assert!(matches!(trap.code, Some(TrapCode::OutOfFuel)) && trap.host_trap);
    let observed = observe_market_sandbox_step(&validated, terminal, &authority, 1_048_576)
        .unwrap_or_else(|error| panic!("single fuel trap step: {error:?}"));
    let replayed = observed.trap.as_ref().unwrap_or_else(|| panic!("replayed fuel trap"));
    assert!(matches!(replayed.code, Some(TrapCode::OutOfFuel)) && replayed.host_trap);
    assert_eq!(observed.reencode_untrusted(1_048_576).unwrap(),
        terminal.reencode_untrusted(1_048_576).unwrap());
}
