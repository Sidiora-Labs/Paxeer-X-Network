use layerx_programs_runtime::abi::response::CANDIDATE_ABI_MODULE;
use layerx_programs_runtime::test_support::{
    code_section, func_body, function_section, import_section, module, type_section, unsigned_leb,
    OP_CALL, OP_END, OP_I32_CONST, TYPE_I32, TYPE_I64,
};
use layerx_programs_runtime::{
    AbiError, AuthorizationContext, CallFrameId, Capability, CapabilitySet, PrincipalId, ProgramId,
    Storage, TransferSource,
};
use layerx_programs_runtime::{
    ActivityBudgetBinding, BudgetedAuthorizedExecutionRequest, DeclaredBudget,
};
use layerx_programs_runtime::{
    AuthorizedExecutionRequest, BudgetMeterRefusal, BudgetResourceKind, CompositionContext,
    Executor, PreparedAuthorizedActivityOutcome, WasmEngine, ABI_MODULE, CALL_ENTRY_EXPORT,
};

use layerx_programs_runtime::abi::UnavailableReceiptOracle as NoReceipts;

fn section(id: u8, payload: &[u8]) -> Vec<u8> {
    let mut encoded = vec![id];
    encoded.extend(unsigned_leb(payload.len() as u64));
    encoded.extend_from_slice(payload);
    encoded
}

fn exports(entries: &[(&str, u8, u8)]) -> Vec<u8> {
    let mut payload = unsigned_leb(entries.len() as u64);
    for (name, kind, index) in entries {
        payload.extend(unsigned_leb(name.len() as u64));
        payload.extend_from_slice(name.as_bytes());
        payload.extend_from_slice(&[*kind, *index]);
    }
    section(7, &payload)
}

fn data_section(entries: &[(u32, &[u8])]) -> Vec<u8> {
    let mut payload = unsigned_leb(entries.len() as u64);
    for (offset, bytes) in entries {
        payload.extend([0, OP_I32_CONST]);
        payload.extend(unsigned_leb(u64::from(*offset)));
        payload.push(OP_END);
        payload.extend(unsigned_leb(bytes.len() as u64));
        payload.extend_from_slice(bytes);
    }
    section(11, &payload)
}

fn transfer_module() -> Vec<u8> {
    let asset = [3; 32];
    let recipient = [4; 32];
    module(&[
        type_section(&[
            (
                &[TYPE_I64, TYPE_I64, TYPE_I32, TYPE_I32, TYPE_I32, TYPE_I32],
                &[TYPE_I32],
            ),
            (&[TYPE_I32, TYPE_I32], &[TYPE_I32]),
        ]),
        import_section(&[(ABI_MODULE, "transfer_402", 0)]),
        function_section(&[1]),
        section(5, &[1, 1, 1, 1]),
        exports(&[("run", 0, 1), ("memory", 2, 0)]),
        code_section(&[func_body(
            &[],
            &[
                0x42,
                0,
                0x42,
                1,
                OP_I32_CONST,
                0,
                OP_I32_CONST,
                32,
                OP_I32_CONST,
                32,
                OP_I32_CONST,
                32,
                OP_CALL,
                0,
                OP_END,
            ],
        )]),
        data_section(&[(0, &asset), (32, &recipient)]),
    ])
}

fn candidate_program_transfer_module(
    seed: &[u8],
    source: [u8; 32],
    asset: [u8; 32],
    recipient: [u8; 32],
) -> Vec<u8> {
    repeated_program_transfer_module(seed, source, asset, recipient, 1)
}

fn repeated_program_transfer_module(
    seed: &[u8],
    source: [u8; 32],
    asset: [u8; 32],
    recipient: [u8; 32],
    repetitions: usize,
) -> Vec<u8> {
    const SOURCE_OFFSET: u32 = 128;
    const ASSET_OFFSET: u32 = 160;
    const RECIPIENT_OFFSET: u32 = 192;
    let mut entries = unsigned_leb(3);
    for (name, kind, index) in [
        ("layerx_reserve", 0_u8, 1_u8),
        (CALL_ENTRY_EXPORT, 0, 2),
        ("memory", 2, 0),
    ] {
        entries.extend(unsigned_leb(name.len() as u64));
        entries.extend_from_slice(name.as_bytes());
        entries.extend_from_slice(&[kind, index]);
    }
    let mut entry = vec![0x42, 0, 0x42, 5, OP_I32_CONST, 0, OP_I32_CONST];
    entry.extend(unsigned_leb(seed.len() as u64));
    for value in [SOURCE_OFFSET, 32, ASSET_OFFSET, 32, RECIPIENT_OFFSET, 32] {
        entry.push(OP_I32_CONST);
        entry.extend(unsigned_leb(u64::from(value)));
    }
    entry.extend([OP_CALL, 0]);
    let call = entry;
    let mut entry = Vec::new();
    for index in 0..repetitions {
        entry.extend_from_slice(&call);
        if index + 1 < repetitions {
            entry.push(0x1a);
        }
    }
    entry.push(OP_END);
    module(&[
        type_section(&[
            (
                &[
                    TYPE_I64, TYPE_I64, TYPE_I32, TYPE_I32, TYPE_I32, TYPE_I32, TYPE_I32, TYPE_I32,
                    TYPE_I32, TYPE_I32,
                ],
                &[TYPE_I32],
            ),
            (&[TYPE_I32], &[TYPE_I32]),
            (&[TYPE_I32, TYPE_I32], &[TYPE_I32]),
        ]),
        import_section(&[(CANDIDATE_ABI_MODULE, "transfer_program_402", 0)]),
        function_section(&[1, 2]),
        section(5, &[1, 1, 1, 1]),
        section(7, &entries),
        code_section(&[
            func_body(&[], &[OP_I32_CONST, 0, OP_END]),
            func_body(&[], &entry),
        ]),
        data_section(&[
            (0, seed),
            (SOURCE_OFFSET, &source),
            (ASSET_OFFSET, &asset),
            (RECIPIENT_OFFSET, &recipient),
        ]),
    ])
}

fn no_effect_module() -> Vec<u8> {
    module(&[
        type_section(&[(&[TYPE_I32, TYPE_I32], &[TYPE_I32])]),
        function_section(&[0]),
        section(5, &[1, 1, 1, 1]),
        exports(&[("run", 0, 0), ("memory", 2, 0)]),
        code_section(&[func_body(&[], &[OP_I32_CONST, 0, OP_END])]),
    ])
}

fn candidate_with_entry(entry: &[u8]) -> Vec<u8> {
    let functions = function_section(&[0, 1]);
    let memory = section(5, &[1, 1, 1, 1]);
    let mut entries = unsigned_leb(3);
    for (name, kind, index) in [
        ("layerx_reserve", 0_u8, 0_u8),
        (CALL_ENTRY_EXPORT, 0, 1),
        ("memory", 2, 0),
    ] {
        entries.extend(unsigned_leb(name.len() as u64));
        entries.extend_from_slice(name.as_bytes());
        entries.extend_from_slice(&[kind, index]);
    }
    module(&[
        type_section(&[
            (&[TYPE_I32], &[TYPE_I32]),
            (&[TYPE_I32, TYPE_I32], &[TYPE_I32]),
        ]),
        functions,
        memory,
        section(7, &entries),
        code_section(&[
            func_body(&[], &[OP_I32_CONST, 0, OP_END]),
            func_body(&[], entry),
        ]),
    ])
}

fn looping_module() -> Vec<u8> {
    candidate_with_entry(&[0x03, 0x40, 0x0c, 0, OP_END, OP_I32_CONST, 0, OP_END])
}

fn admitted_request<'a>(
    executor: &Executor,
    request: AuthorizedExecutionRequest<'a>,
    payer: PrincipalId,
    declared: DeclaredBudget,
    binding: ActivityBudgetBinding,
) -> BudgetedAuthorizedExecutionRequest<'a> {
    let admitted = executor
        .admit_activity_budget_for_qualification(declared, payer, binding, u128::MAX)
        .unwrap_or_else(|error| panic!("admitted: {error}"));
    BudgetedAuthorizedExecutionRequest::new(request, admitted, payer, binding)
}

fn generous_budget() -> DeclaredBudget {
    DeclaredBudget::new(1_000_000, 1_048_576, 1_048_576, 1_048_576, 64, 0, 64)
        .unwrap_or_else(|error| panic!("declared: {error}"))
}

#[test]
fn real_wasm_budgeted_preparation_seals_transfer_or_zero_transfer_without_kernel() {
    let program = ProgramId::new([1; 32]).unwrap_or_else(|error| panic!("program: {error}"));
    let payer = PrincipalId::new([2; 32]).unwrap_or_else(|error| panic!("payer: {error}"));
    let executor = Executor::declared();
    for (wasm, grants, has_transfer) in [
        (
            transfer_module(),
            CapabilitySet::new([Capability::Transfer402 {
                asset: [3; 32],
                to: [4; 32],
                maximum_amount: 1,
            }])
            .unwrap_or_else(|error| panic!("grants: {error}")),
            true,
        ),
        (no_effect_module(), CapabilitySet::empty(), false),
    ] {
        let module = WasmEngine::declared()
            .unwrap_or_else(|error| panic!("engine: {error}"))
            .validate(&wasm)
            .unwrap_or_else(|error| panic!("module: {error}"));
        let storage = Storage::default();
        let outcome = executor
            .prepare_authorized_activity_budgeted(
                &storage,
                admitted_request(
                    &executor,
                    AuthorizedExecutionRequest {
                        module: &module,
                        program,
                        authorization: AuthorizationContext::new(payer, grants),
                        receipts: &NoReceipts,
                        entrypoint: "run",
                        calldata: &[],
                        composition: CompositionContext::isolated(),
                        response_capacity: 0,
                    },
                    payer,
                    generous_budget(),
                    ActivityBudgetBinding::new([9; 32])
                        .unwrap_or_else(|error| panic!("binding: {error}")),
                ),
            )
            .unwrap_or_else(|error| panic!("prepared: {error}"));
        let PreparedAuthorizedActivityOutcome::Success(prepared) = outcome else {
            panic!("real wasm preparation must succeed")
        };
        assert!(prepared.execution().usage.cpu_fuel > 0);
        assert_eq!(prepared.execution().usage.memory_bytes, 65_536);
        assert!(prepared.execution().usage.fee_units > 0);
        assert_eq!(prepared.has_monetary_effects(), has_transfer);
        let summary = prepared.monetary_summary();
        if has_transfer {
            let summary = summary.unwrap_or_else(|| panic!("missing monetary summary"));
            assert_eq!(summary.program(), program);
            assert_eq!(summary.principal(), payer);
            assert_eq!(summary.invocation_authority(), [9; 32]);
            assert_eq!(summary.total_amount(), 1);
            assert_eq!(summary.legs().len(), 1);
            let leg = &summary.legs()[0];
            assert_eq!(leg.program(), program);
            assert_eq!(leg.principal(), payer);
            assert_eq!(leg.frame(), CallFrameId::root());
            assert_eq!(leg.asset(), [3; 32]);
            assert_eq!(leg.to(), [4; 32]);
            assert_eq!(leg.amount(), 1);
        } else {
            assert_eq!(summary, None);
        }
    }
}

#[test]
fn candidate_program_transfer_host_issues_exact_owner_frame_authority() {
    let program = ProgramId::new([31; 32]).unwrap_or_else(|error| panic!("program: {error}"));
    let payer = PrincipalId::new([32; 32]).unwrap_or_else(|error| panic!("payer: {error}"));
    let seed = b"merchant/settlement";
    let source = layerx_programs_runtime::derive_program_account(program, seed)
        .unwrap_or_else(|error| panic!("source: {error}"))
        .bytes();
    let asset = [33; 32];
    let recipient = [34; 32];
    let grants = CapabilitySet::new([Capability::ProgramSpend {
        owner_program: program,
        seed: seed.to_vec(),
        source_account: source,
        asset,
        to: recipient,
        maximum_amount: 5,
    }])
    .unwrap_or_else(|error| panic!("grants: {error}"));
    let module = WasmEngine::declared()
        .unwrap_or_else(|error| panic!("engine: {error}"))
        .validate_candidate_v2(&candidate_program_transfer_module(
            seed, source, asset, recipient,
        ))
        .unwrap_or_else(|error| panic!("module: {error}"));
    let record = Executor::declared()
        .execute_authorized_candidate(
            &mut Storage::default(),
            AuthorizedExecutionRequest {
                module: &module,
                program,
                authorization: AuthorizationContext::new(payer, grants),
                receipts: &NoReceipts,
                entrypoint: CALL_ENTRY_EXPORT,
                calldata: &[],
                composition: CompositionContext::isolated(),
                response_capacity: 0,
            },
        )
        .unwrap_or_else(|error| panic!("candidate execution: {error}"));
    let effects = record
        .effects()
        .unwrap_or_else(|| panic!("candidate effects missing"));
    assert_eq!(effects.transfers.len(), 1);
    let transfer = &effects.transfers[0];
    assert_eq!(transfer.program, program);
    assert_eq!(transfer.principal, payer);
    assert_eq!(transfer.frame, CallFrameId::root());
    assert_eq!(transfer.asset, asset);
    assert_eq!(transfer.to, recipient);
    assert_eq!(transfer.amount, 5);
    let TransferSource::Program(authority) = transfer.source() else {
        panic!("candidate transfer must carry program authority")
    };
    assert_eq!(authority.owner_program(), program);
    assert_eq!(authority.seed(), seed);
    assert_eq!(authority.source_account(), source);
    assert_eq!(authority.staging_frame(), CallFrameId::root());
    assert_eq!(authority.asset(), asset);
    assert_eq!(authority.to(), recipient);
    assert_eq!(authority.amount(), 5);
}

#[test]
fn real_wasm_program_leg_from_underivable_account_is_refused() {
    let program = ProgramId::new([41; 32]).unwrap_or_else(|error| panic!("program: {error}"));
    let foreign = ProgramId::new([45; 32]).unwrap_or_else(|error| panic!("foreign: {error}"));
    let payer = PrincipalId::new([42; 32]).unwrap_or_else(|error| panic!("payer: {error}"));
    let seed = b"merchant/settlement";
    let foreign_source = layerx_programs_runtime::derive_program_account(foreign, seed)
        .unwrap_or_else(|error| panic!("foreign source: {error}"))
        .bytes();
    let asset = [43; 32];
    let recipient = [44; 32];
    let grants = CapabilitySet::new([Capability::ProgramSpend {
        owner_program: foreign,
        seed: seed.to_vec(),
        source_account: foreign_source,
        asset,
        to: recipient,
        maximum_amount: 5,
    }])
    .unwrap_or_else(|error| panic!("grants: {error}"));
    let module = WasmEngine::declared()
        .unwrap_or_else(|error| panic!("engine: {error}"))
        .validate_candidate_v2(&candidate_program_transfer_module(
            seed,
            foreign_source,
            asset,
            recipient,
        ))
        .unwrap_or_else(|error| panic!("module: {error}"));
    let mut storage = Storage::default();
    let refused = Executor::declared().execute_authorized_candidate(
        &mut storage,
        AuthorizedExecutionRequest {
            module: &module,
            program,
            authorization: AuthorizationContext::new(payer, grants),
            receipts: &NoReceipts,
            entrypoint: CALL_ENTRY_EXPORT,
            calldata: &[],
            composition: CompositionContext::isolated(),
            response_capacity: 0,
        },
    );
    assert_eq!(
        refused,
        Err(layerx_programs_runtime::ExecutionError::Composition(
            layerx_programs_runtime::CompositionRefusal::Authority(AbiError::CapabilityEscalation),
        ))
    );
    assert_eq!(storage, Storage::default());
}

fn candidate_mixed_transfer_module(
    seed: &[u8],
    source: [u8; 32],
    asset: [u8; 32],
    recipient: [u8; 32],
) -> Vec<u8> {
    const SOURCE_OFFSET: u32 = 128;
    const ASSET_OFFSET: u32 = 160;
    const RECIPIENT_OFFSET: u32 = 192;
    let mut entries = unsigned_leb(3);
    for (name, kind, index) in [
        ("layerx_reserve", 0_u8, 2_u8),
        (CALL_ENTRY_EXPORT, 0, 3),
        ("memory", 2, 0),
    ] {
        entries.extend(unsigned_leb(name.len() as u64));
        entries.extend_from_slice(name.as_bytes());
        entries.extend_from_slice(&[kind, index]);
    }
    let mut entry = vec![0x42, 0, 0x42, 3];
    for value in [ASSET_OFFSET, 32, RECIPIENT_OFFSET, 32] {
        entry.push(OP_I32_CONST);
        entry.extend(unsigned_leb(u64::from(value)));
    }
    entry.extend([OP_CALL, 0, 0x1a]);
    entry.extend([0x42, 0, 0x42, 5, OP_I32_CONST, 0, OP_I32_CONST]);
    entry.extend(unsigned_leb(seed.len() as u64));
    for value in [SOURCE_OFFSET, 32, ASSET_OFFSET, 32, RECIPIENT_OFFSET, 32] {
        entry.push(OP_I32_CONST);
        entry.extend(unsigned_leb(u64::from(value)));
    }
    entry.extend([OP_CALL, 1, OP_END]);
    module(&[
        type_section(&[
            (
                &[TYPE_I64, TYPE_I64, TYPE_I32, TYPE_I32, TYPE_I32, TYPE_I32],
                &[TYPE_I32],
            ),
            (
                &[
                    TYPE_I64, TYPE_I64, TYPE_I32, TYPE_I32, TYPE_I32, TYPE_I32, TYPE_I32, TYPE_I32,
                    TYPE_I32, TYPE_I32,
                ],
                &[TYPE_I32],
            ),
            (&[TYPE_I32], &[TYPE_I32]),
            (&[TYPE_I32, TYPE_I32], &[TYPE_I32]),
        ]),
        import_section(&[
            (ABI_MODULE, "transfer_402", 0),
            (CANDIDATE_ABI_MODULE, "transfer_program_402", 1),
        ]),
        function_section(&[2, 3]),
        section(5, &[1, 1, 1, 1]),
        section(7, &entries),
        code_section(&[
            func_body(&[], &[OP_I32_CONST, 0, OP_END]),
            func_body(&[], &entry),
        ]),
        data_section(&[
            (0, seed),
            (SOURCE_OFFSET, &source),
            (ASSET_OFFSET, &asset),
            (RECIPIENT_OFFSET, &recipient),
        ]),
    ])
}

#[test]
fn real_wasm_mixed_principal_and_program_legs_seal_one_canonical_set() {
    let program = ProgramId::new([51; 32]).unwrap_or_else(|error| panic!("program: {error}"));
    let payer = PrincipalId::new([52; 32]).unwrap_or_else(|error| panic!("payer: {error}"));
    let seed = b"merchant/settlement";
    let source = layerx_programs_runtime::derive_program_account(program, seed)
        .unwrap_or_else(|error| panic!("source: {error}"))
        .bytes();
    let asset = [53; 32];
    let recipient = [54; 32];
    let grants = CapabilitySet::new([
        Capability::Transfer402 {
            asset,
            to: recipient,
            maximum_amount: 3,
        },
        Capability::ProgramSpend {
            owner_program: program,
            seed: seed.to_vec(),
            source_account: source,
            asset,
            to: recipient,
            maximum_amount: 5,
        },
    ])
    .unwrap_or_else(|error| panic!("grants: {error}"));
    let module = WasmEngine::declared()
        .unwrap_or_else(|error| panic!("engine: {error}"))
        .validate_candidate_v2(&candidate_mixed_transfer_module(
            seed, source, asset, recipient,
        ))
        .unwrap_or_else(|error| panic!("module: {error}"));
    let execute = || {
        Executor::declared()
            .execute_authorized_candidate(
                &mut Storage::default(),
                AuthorizedExecutionRequest {
                    module: &module,
                    program,
                    authorization: AuthorizationContext::new(payer, grants.clone()),
                    receipts: &NoReceipts,
                    entrypoint: CALL_ENTRY_EXPORT,
                    calldata: &[],
                    composition: CompositionContext::isolated(),
                    response_capacity: 0,
                },
            )
            .unwrap_or_else(|error| panic!("mixed candidate execution: {error}"))
    };
    let record = execute();
    let effects = record
        .effects()
        .unwrap_or_else(|| panic!("mixed candidate effects missing"));
    assert_eq!(effects.transfers.len(), 2);
    let principal_leg = &effects.transfers[0];
    assert_eq!(principal_leg.program, program);
    assert_eq!(principal_leg.principal, payer);
    assert_eq!(principal_leg.frame, CallFrameId::root());
    assert_eq!(principal_leg.amount, 3);
    assert_eq!(principal_leg.source(), &TransferSource::Principal(payer));
    let program_leg = &effects.transfers[1];
    assert_eq!(program_leg.program, program);
    assert_eq!(program_leg.principal, payer);
    assert_eq!(program_leg.frame, CallFrameId::root());
    assert_eq!(program_leg.amount, 5);
    let TransferSource::Program(authority) = program_leg.source() else {
        panic!("second leg must carry program authority")
    };
    assert_eq!(authority.owner_program(), program);
    assert_eq!(authority.seed(), seed);
    assert_eq!(authority.source_account(), source);
    assert_eq!(authority.staging_frame(), CallFrameId::root());
    let replay = execute();
    assert_eq!(record.receipt_projection(), replay.receipt_projection());
}

#[test]
fn real_wasm_budgeted_preparation_retains_failure_and_resource_diagnostics() {
    let executor = Executor::declared();
    let payer = PrincipalId::new([2; 32]).unwrap_or_else(|error| panic!("payer: {error}"));
    let program = ProgramId::new([1; 32]).unwrap_or_else(|error| panic!("program: {error}"));
    let engine = WasmEngine::declared().unwrap_or_else(|error| panic!("engine: {error}"));

    let failed_module = engine
        .validate(&candidate_with_entry(&[OP_I32_CONST, 0x7f, OP_END]))
        .unwrap_or_else(|error| panic!("failed module: {error}"));
    let failure = executor
        .prepare_authorized_activity_budgeted(
            &Storage::default(),
            admitted_request(
                &executor,
                AuthorizedExecutionRequest {
                    module: &failed_module,
                    program,
                    authorization: AuthorizationContext::new(payer, CapabilitySet::empty()),
                    receipts: &NoReceipts,
                    entrypoint: CALL_ENTRY_EXPORT,
                    calldata: &[],
                    composition: CompositionContext::isolated(),
                    response_capacity: 0,
                },
                payer,
                generous_budget(),
                ActivityBudgetBinding::new([10; 32])
                    .unwrap_or_else(|error| panic!("binding: {error}")),
            ),
        )
        .unwrap_or_else(|error| panic!("failure preparation: {error}"));
    let PreparedAuthorizedActivityOutcome::Failure(failure) = failure else {
        panic!("expected receipt-ready failure")
    };
    assert!(failure.usage().cpu_fuel > 0);
    assert!(failure.call_graph().edges().is_empty());

    let looping = engine
        .validate(&looping_module())
        .unwrap_or_else(|error| panic!("looping module: {error}"));
    let resource = executor
        .prepare_authorized_activity_budgeted(
            &Storage::default(),
            admitted_request(
                &executor,
                AuthorizedExecutionRequest {
                    module: &looping,
                    program,
                    authorization: AuthorizationContext::new(payer, CapabilitySet::empty()),
                    receipts: &NoReceipts,
                    entrypoint: CALL_ENTRY_EXPORT,
                    calldata: &[],
                    composition: CompositionContext::isolated(),
                    response_capacity: 0,
                },
                payer,
                DeclaredBudget::new(100, 65_536, 0, 0, 2, 0, 0)
                    .unwrap_or_else(|error| panic!("cpu budget: {error}")),
                ActivityBudgetBinding::new([11; 32])
                    .unwrap_or_else(|error| panic!("binding: {error}")),
            ),
        )
        .unwrap_or_else(|error| panic!("resource preparation: {error}"));
    let PreparedAuthorizedActivityOutcome::Resource(resource) = resource else {
        panic!("expected receipt-ready resource refusal")
    };
    assert_eq!(
        resource.refusal(),
        BudgetMeterRefusal::BudgetExceeded {
            resource: BudgetResourceKind::Cpu,
            limit: 100,
            attempted: 101,
        }
    );
    assert_eq!(resource.usage().cpu_fuel, 99);
    assert!(resource.call_graph().edges().is_empty());
}

fn candidate_forwarding_module(callee: ProgramId, requested: &CapabilitySet) -> Vec<u8> {
    let encoded = requested.canonical_encoding();
    let mut entry = Vec::new();
    for mut value in [0_i64, 32, 32, 0, 32, encoded.len() as i64, 512, 0] {
        entry.push(OP_I32_CONST);
        loop {
            let byte = (value & 0x7f) as u8;
            value >>= 7;
            let done = (value == 0 && byte & 0x40 == 0) || (value == -1 && byte & 0x40 != 0);
            entry.push(if done { byte } else { byte | 0x80 });
            if done {
                break;
            }
        }
    }
    entry.extend([OP_CALL, 0, 0x1a, OP_I32_CONST, 0, OP_END]);
    module(&[
        type_section(&[
            (&[TYPE_I32; 8], &[TYPE_I64]),
            (&[TYPE_I32], &[TYPE_I32]),
            (&[TYPE_I32, TYPE_I32], &[TYPE_I32]),
        ]),
        import_section(&[(CANDIDATE_ABI_MODULE, "program_call_response", 0)]),
        function_section(&[1, 2]),
        section(5, &[1, 1, 1, 1]),
        exports(&[
            ("layerx_reserve", 0, 1),
            (CALL_ENTRY_EXPORT, 0, 2),
            ("memory", 2, 0),
        ]),
        code_section(&[
            func_body(&[], &[OP_I32_CONST, 0, OP_END]),
            func_body(&[], &entry),
        ]),
        data_section(&[(0, &callee.bytes()), (32, &encoded)]),
    ])
}

#[test]
fn real_wasm_program_leg_staged_by_callee_frame_is_refused() {
    use layerx_programs_runtime::{
        CompositionRefusal, CompositionRules, ExecutionError, ProgramCatalog,
    };
    let owner = ProgramId::new([61; 32]).unwrap_or_else(|error| panic!("owner: {error}"));
    let callee = ProgramId::new([62; 32]).unwrap_or_else(|error| panic!("callee: {error}"));
    let payer = PrincipalId::new([63; 32]).unwrap_or_else(|error| panic!("payer: {error}"));
    let seed = b"owner-only";
    let source = layerx_programs_runtime::derive_program_account(owner, seed)
        .unwrap_or_else(|error| panic!("source: {error}"))
        .bytes();
    let asset = [64; 32];
    let recipient = [65; 32];
    let spend = Capability::ProgramSpend {
        owner_program: owner,
        seed: seed.to_vec(),
        source_account: source,
        asset,
        to: recipient,
        maximum_amount: 5,
    };
    let requested =
        CapabilitySet::new([spend.clone()]).unwrap_or_else(|error| panic!("requested: {error}"));
    let grants = CapabilitySet::new([Capability::Call { program: callee }, spend])
        .unwrap_or_else(|error| panic!("grants: {error}"));
    let engine = WasmEngine::declared().unwrap_or_else(|error| panic!("engine: {error}"));
    let root = engine
        .validate_candidate_v2(&candidate_forwarding_module(callee, &requested))
        .unwrap_or_else(|error| panic!("root module: {error}"));
    let child = engine
        .validate_candidate_v2(&candidate_program_transfer_module(
            seed, source, asset, recipient,
        ))
        .unwrap_or_else(|error| panic!("child module: {error}"));
    let mut catalog = ProgramCatalog::new();
    assert!(catalog.insert(callee, child).is_none());
    let mut storage = Storage::default();
    let before = storage.clone();
    let refused = Executor::declared().execute_authorized_candidate(
        &mut storage,
        AuthorizedExecutionRequest {
            module: &root,
            program: owner,
            authorization: AuthorizationContext::new(payer, grants),
            receipts: &NoReceipts,
            entrypoint: CALL_ENTRY_EXPORT,
            calldata: &[],
            composition: CompositionContext::catalog(catalog, CompositionRules::declared()),
            response_capacity: 0,
        },
    );
    assert_eq!(
        refused,
        Err(ExecutionError::Composition(CompositionRefusal::Authority(
            AbiError::CapabilityEscalation
        ),))
    );
    assert_eq!(storage, before);
}

#[test]
fn real_wasm_cumulative_program_legs_refuse_one_past_grant_atomically() {
    use layerx_programs_runtime::{CompositionRefusal, ExecutionError};
    let owner = ProgramId::new([71; 32]).unwrap_or_else(|error| panic!("owner: {error}"));
    let payer = PrincipalId::new([72; 32]).unwrap_or_else(|error| panic!("payer: {error}"));
    let seed = b"cumulative";
    let source = layerx_programs_runtime::derive_program_account(owner, seed)
        .unwrap_or_else(|error| panic!("source: {error}"))
        .bytes();
    let asset = [73; 32];
    let recipient = [74; 32];
    let module = WasmEngine::declared()
        .unwrap_or_else(|error| panic!("engine: {error}"))
        .validate_candidate_v2(&repeated_program_transfer_module(
            seed, source, asset, recipient, 2,
        ))
        .unwrap_or_else(|error| panic!("module: {error}"));
    for maximum_amount in [9, 10] {
        let grants = CapabilitySet::new([Capability::ProgramSpend {
            owner_program: owner,
            seed: seed.to_vec(),
            source_account: source,
            asset,
            to: recipient,
            maximum_amount,
        }])
        .unwrap_or_else(|error| panic!("grants: {error}"));
        let mut storage = Storage::default();
        let before = storage.clone();
        let outcome = Executor::declared().execute_authorized_candidate(
            &mut storage,
            AuthorizedExecutionRequest {
                module: &module,
                program: owner,
                authorization: AuthorizationContext::new(payer, grants),
                receipts: &NoReceipts,
                entrypoint: CALL_ENTRY_EXPORT,
                calldata: &[],
                composition: CompositionContext::isolated(),
                response_capacity: 0,
            },
        );
        if maximum_amount == 9 {
            assert_eq!(
                outcome,
                Err(ExecutionError::Composition(CompositionRefusal::Authority(
                    AbiError::CapabilityEscalation
                ),))
            );
        } else {
            let record = outcome.unwrap_or_else(|error| panic!("at bound: {error}"));
            let effects = record
                .effects()
                .unwrap_or_else(|| panic!("at-bound effects"));
            assert_eq!(effects.transfers.len(), 2);
            assert_eq!(
                effects.transfers.iter().map(|leg| leg.amount).sum::<u128>(),
                10
            );
        }
        assert_eq!(storage, before);
    }
}

#[test]
fn monetary_law_raw_balance_and_issuance_imports_refuse_every_supported_abi() {
    use layerx_programs_runtime::ValidationRefusal;
    let engine = WasmEngine::declared().unwrap_or_else(|error| panic!("engine: {error}"));
    for abi_version in 1..=4 {
        for module_name in [ABI_MODULE, CANDIDATE_ABI_MODULE, "env", "kernel", "ledger"] {
            for import_name in [
                "balance_write",
                "balance_set",
                "balance_add",
                "balance_sub",
                "ledger_apply",
                "apply_transfer_set",
                "mint",
                "burn",
                "transfer_from",
            ] {
                let wasm = module(&[
                    type_section(&[(&[], &[TYPE_I32])]),
                    import_section(&[(module_name, import_name, 0)]),
                ]);
                let refusal = match engine.validate_versioned(abi_version, &wasm) {
                    Ok(_) => panic!(
                        "raw monetary import validated: {abi_version}/{module_name}/{import_name}"
                    ),
                    Err(refusal) => refusal,
                };
                assert_eq!(
                    refusal,
                    ValidationRefusal::ForbiddenImport {
                        import_module: module_name.into(),
                        import_name: import_name.into(),
                    },
                    "ABI {abi_version}/{module_name}/{import_name}",
                );
            }
        }
    }
}

#[test]
fn monetary_law_principal_transfer_without_invoker_grant_refuses_atomically() {
    use layerx_programs_runtime::{EntrypointRefusal, ExecutionError};
    let program = ProgramId::new([81; 32]).unwrap_or_else(|error| panic!("program: {error}"));
    let payer = PrincipalId::new([82; 32]).unwrap_or_else(|error| panic!("payer: {error}"));
    let module = WasmEngine::declared()
        .unwrap_or_else(|error| panic!("engine: {error}"))
        .validate(&transfer_module())
        .unwrap_or_else(|error| panic!("module: {error}"));
    let mut storage = Storage::default();
    let before = storage.clone();
    let refused = Executor::declared().execute_authorized(
        &mut storage,
        AuthorizedExecutionRequest {
            module: &module,
            program,
            authorization: AuthorizationContext::new(payer, CapabilitySet::empty()),
            receipts: &NoReceipts,
            entrypoint: "run",
            calldata: &[],
            composition: CompositionContext::isolated(),
            response_capacity: 0,
        },
    );
    assert_eq!(
        refused,
        Err(ExecutionError::Entrypoint(
            EntrypointRefusal::GuestRefused { code: -1 }
        )),
    );
    assert_eq!(storage, before);
}
