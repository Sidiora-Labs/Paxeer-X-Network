use layerx_programs_runtime::test_support::{
    add_module, code_section, export_section, func_body, function_section, import_section, module,
    raw_section, type_section, OP_CALL, OP_DROP, OP_END, OP_I32_ADD, OP_I32_CONST, OP_LOCAL_GET,
    TYPE_I32,
};
use layerx_programs_runtime::{
    Abi, AuthorizationContext, Capability, CapabilitySet, FeeSchedule, Meter, PrincipalId,
    ProgramId, ResourceBudget, Storage, TracePolicy, UnavailableReceiptOracle, ValidatedModule,
    WasmEngine, WasmValue, ABI_V2_VERSION,
};

const WITNESS_BYTES: usize = 64 * 1_024 * 1_024;

fn validated(wasm: &[u8]) -> ValidatedModule {
    WasmEngine::declared()
        .unwrap_or_else(|error| panic!("engine: {error}"))
        .validate_v2(wasm)
        .unwrap_or_else(|error| panic!("validation: {error}"))
}

fn policy(interval: u64) -> TracePolicy {
    TracePolicy::new(interval, 512).unwrap_or_else(|error| panic!("trace policy: {error}"))
}

fn sandbox(module: &ValidatedModule) -> layerx_programs_runtime::ProgramInstance {
    sandbox_with_capabilities(module, CapabilitySet::empty())
}

fn sandbox_with_capabilities(
    module: &ValidatedModule,
    capabilities: CapabilitySet,
) -> layerx_programs_runtime::ProgramInstance {
    let declared = ResourceBudget::declared();
    let meter = Meter::new(
        ResourceBudget::new_complete(
            200_000_000,
            declared.memory_bytes(),
            declared.storage_read_bytes(),
            declared.storage_write_bytes(),
            declared.output_values(),
            declared.output_bytes(),
            declared.table_elements(),
        ),
        FeeSchedule::declared(),
    );
    let abi = Abi::new(
        ABI_V2_VERSION,
        ProgramId::new([0x11; 32]).unwrap_or_else(|error| panic!("program: {error}")),
        AuthorizationContext::new(
            PrincipalId::new([0x22; 32]).unwrap_or_else(|error| panic!("principal: {error}")),
            capabilities,
        ),
        Storage::new(),
        &UnavailableReceiptOracle,
    )
    .unwrap_or_else(|error| panic!("ABI: {error}"));
    module
        .instantiate_sandbox(meter, abi)
        .unwrap_or_else(|error| panic!("sandbox: {error}"))
}

fn state_rich_module() -> Vec<u8> {
    module(&[
        type_section(&[(&[TYPE_I32], &[TYPE_I32])]),
        function_section(&[0, 0]),
        raw_section(5, &[1, 0, 1]),
        raw_section(6, &[1, TYPE_I32, 1, OP_I32_CONST, 0, OP_END]),
        export_section(&[("run", 1)]),
        code_section(&[
            func_body(
                &[(1, TYPE_I32)],
                &[
                    OP_LOCAL_GET,
                    0,
                    OP_I32_CONST,
                    2,
                    OP_I32_ADD,
                    0x22,
                    1,
                    OP_LOCAL_GET,
                    1,
                    OP_I32_ADD,
                    OP_END,
                ],
            ),
            func_body(
                &[],
                &[
                    OP_I32_CONST,
                    1,
                    0x40,
                    0,
                    OP_DROP,
                    OP_I32_CONST,
                    0,
                    OP_LOCAL_GET,
                    0,
                    0x36,
                    2,
                    0,
                    OP_LOCAL_GET,
                    0,
                    0x24,
                    0,
                    OP_LOCAL_GET,
                    0,
                    OP_CALL,
                    0,
                    OP_END,
                ],
            ),
        ]),
    ])
}

fn trapping_module() -> Vec<u8> {
    module(&[
        type_section(&[(&[], &[])]),
        function_section(&[0]),
        export_section(&[("run", 0)]),
        code_section(&[func_body(&[], &[0x00, OP_END])]),
    ])
}

fn storage_writer_module() -> Vec<u8> {
    module(&[
        type_section(&[(&[TYPE_I32; 4], &[TYPE_I32]), (&[], &[TYPE_I32])]),
        import_section(&[("layerx_v1", "storage_write", 0)]),
        function_section(&[1]),
        raw_section(5, &[1, 0, 1]),
        raw_section(
            7,
            &[
                2, 3, b'r', b'u', b'n', 0, 1, 6, b'm', b'e', b'm', b'o', b'r', b'y', 2, 0,
            ],
        ),
        code_section(&[func_body(
            &[],
            &[
                OP_I32_CONST,
                0,
                OP_I32_CONST,
                1,
                OP_I32_CONST,
                1,
                OP_I32_CONST,
                1,
                OP_CALL,
                0,
                OP_END,
            ],
        )]),
        raw_section(11, &[1, 0, OP_I32_CONST, 0, OP_END, 2, b'k', b'v']),
    ])
}

#[test]
fn integer_boundaries_restore_and_execute_the_real_engine_step() {
    let module = validated(&add_module());
    let capture = sandbox(&module)
        .call_with_boundary_witnesses(
            &module,
            "add",
            &[WasmValue::I32(20), WasmValue::I32(22)],
            policy(1),
            WITNESS_BYTES,
        )
        .unwrap_or_else(|error| panic!("boundary capture: {error:?}"));
    assert_eq!(capture.values, vec![WasmValue::I32(42)]);
    assert!(!capture.boundaries.is_empty());
    assert_eq!(
        capture.boundaries.len(),
        capture.trace.arbitration_steps().len()
    );
    assert!(capture
        .boundaries
        .windows(2)
        .all(|pair| pair[0].step_index() < pair[1].step_index()));
    for boundary in &capture.boundaries {
        boundary
            .replay(&module)
            .unwrap_or_else(|error| panic!("real integer boundary replay: {error:?}"));
    }
}

#[test]
fn nested_frames_locals_memory_growth_and_globals_restore_exactly() {
    let module = validated(&state_rich_module());
    let capture = sandbox(&module)
        .call_with_boundary_witnesses(
            &module,
            "run",
            &[WasmValue::I32(19)],
            policy(1),
            WITNESS_BYTES,
        )
        .unwrap_or_else(|error| panic!("state-rich capture: {error:?}"));
    assert_eq!(capture.values, vec![WasmValue::I32(42)]);
    assert_eq!(
        capture.boundaries.len(),
        capture.trace.arbitration_steps().len()
    );
    assert!(capture.boundaries.len() > 10);
    for boundary in &capture.boundaries {
        boundary
            .replay(&module)
            .unwrap_or_else(|error| panic!("state-rich boundary replay: {error:?}"));
    }
}

#[test]
fn independent_captures_have_identical_commitments_and_metering() {
    let module = validated(&add_module());
    let first = sandbox(&module)
        .call_with_boundary_witnesses(
            &module,
            "add",
            &[WasmValue::I32(20), WasmValue::I32(22)],
            policy(1),
            WITNESS_BYTES,
        )
        .unwrap_or_else(|error| panic!("first capture: {error:?}"));
    let second = sandbox(&module)
        .call_with_boundary_witnesses(
            &module,
            "add",
            &[WasmValue::I32(20), WasmValue::I32(22)],
            policy(1),
            WITNESS_BYTES,
        )
        .unwrap_or_else(|error| panic!("second capture: {error:?}"));
    assert_eq!(first.trace.commitments(), second.trace.commitments());
    assert_eq!(
        first.trace.arbitration_commitments(),
        second.trace.arbitration_commitments()
    );
    assert_eq!(
        first.trace.total_commitment_fuel(),
        second.trace.total_commitment_fuel()
    );
    assert_eq!(
        first.trace.total_arbitration_commitment_fuel(),
        second.trace.total_arbitration_commitment_fuel()
    );
}

#[test]
fn genuine_abi_storage_write_replays_with_the_matching_semantic_snapshot() {
    let module = validated(&storage_writer_module());
    let capabilities = CapabilitySet::new([Capability::StorageRead, Capability::StorageWrite])
        .unwrap_or_else(|error| panic!("storage capabilities: {error}"));
    let capture = sandbox_with_capabilities(&module, capabilities)
        .call_with_boundary_witnesses(&module, "run", &[], policy(1), WITNESS_BYTES)
        .unwrap_or_else(|error| panic!("storage-write capture: {error:?}"));
    assert_eq!(capture.values, vec![WasmValue::I32(0)]);
    assert!(!capture.boundaries.is_empty());
    for boundary in &capture.boundaries {
        boundary
            .replay(&module)
            .unwrap_or_else(|error| panic!("storage-write boundary replay: {error:?}"));
    }
}

#[test]
fn a_boundary_rejects_a_different_validated_module() {
    let module = validated(&add_module());
    let capture = sandbox(&module)
        .call_with_boundary_witnesses(
            &module,
            "add",
            &[WasmValue::I32(20), WasmValue::I32(22)],
            policy(1),
            WITNESS_BYTES,
        )
        .unwrap_or_else(|error| panic!("capture: {error:?}"));
    let different = validated(&state_rich_module());
    assert!(capture.boundaries[0].replay(&different).is_err());
}

#[test]
fn insufficient_retained_bytes_and_sparse_sampling_are_refused() {
    let module = validated(&add_module());
    assert!(sandbox(&module)
        .call_with_boundary_witnesses(
            &module,
            "add",
            &[WasmValue::I32(20), WasmValue::I32(22)],
            policy(1),
            1,
        )
        .is_err());
    assert!(sandbox(&module)
        .call_with_boundary_witnesses(
            &module,
            "add",
            &[WasmValue::I32(20), WasmValue::I32(22)],
            policy(2),
            WITNESS_BYTES,
        )
        .is_err());
}

#[test]
fn isolated_execution_cannot_manufacture_semantic_authority() {
    let module = validated(&add_module());
    let mut instance = module
        .instantiate()
        .unwrap_or_else(|error| panic!("isolated instance: {error}"));
    assert!(instance
        .call_with_boundary_witnesses(
            &module,
            "add",
            &[WasmValue::I32(20), WasmValue::I32(22)],
            policy(1),
            WITNESS_BYTES,
        )
        .is_err());
}

#[test]
fn unreachable_preserves_the_original_trap_refusal() {
    let module = validated(&trapping_module());
    assert!(sandbox(&module)
        .call_with_boundary_witnesses(&module, "run", &[], policy(1), WITNESS_BYTES)
        .is_err());
}
