//! A genuine guest-ABI-5 execution through the runtime executor emits the v5 execution
//! terminal, which decodes only under the exact guest ABI it was produced for. The guest is
//! the AI market program built exactly as `make paxai-host-boundary-build` builds it.
use layerx_programs_runtime::{
    abi::UnavailableReceiptOracle,
    terminal::{
        decode_terminal_payload, CandidateTerminalOutcome, DecodedTerminal, ExecutionTerminal,
        ExecutionValue, TerminalDecodeError, TerminalDetail,
    },
    AbiRevision, ActivityBudgetBinding, AdmittedBudget, AuthorizationContext,
    AuthorizedExecutionRequest, BudgetedAuthorizedExecutionRequest, Capability, CapabilitySet,
    CompositionContext, DeclaredBudget, Executor, FuelSchedule, PrincipalId, ProgramId, Storage,
    V2ActivityOutcome, V2AuthorizedExecutionRecord, ValidatedModule, WasmEngine, WasmValue,
    ABI_V4_VERSION, ABI_V5_VERSION, CALL_ENTRY_EXPORT, MAX_CALL_RESPONSE_BYTES,
};
use std::{fs, path::Path, process::Command};

/// The Makefile's default `PAXAI_CHAIN_DOMAIN`, `PAXAI/host-boundary/chain-domain`.
const CHAIN_DOMAIN_HEX: &str = "50415841492f686f73742d626f756e646172792f636861696e2d646f6d61696e";
const EXECUTION_V4: &[u8] = b"LXP/program-execution/v4\0";
const EXECUTION_V5: &[u8] = b"LXP/program-execution/v5\0";
const PROGRAM: [u8; 32] = [0x51; 32];
const ACTOR: [u8; 32] = [0x52; 32];
const ACTIVITY: [u8; 32] = [0x53; 32];
/// Calldata the guest decodes as a market envelope and answers through its own refusal path.
const CALLDATA: &[u8] = b"PAXAI/abi5-terminal-probe";

type Checked<T = ()> = Result<T, String>;

/// The AI market guest built as the Makefile builds it, validated as guest ABI 5 under the
/// genesis metering schedule.
fn guest_module() -> Checked<ValidatedModule> {
    let programs = Path::new(env!("CARGO_MANIFEST_DIR")).join("../..");
    let target = Path::new(env!("CARGO_TARGET_TMPDIR")).join("execution-abi5-guest");
    let status = Command::new(env!("CARGO"))
        .current_dir(&programs)
        .env("PAXAI_CHAIN_DOMAIN", CHAIN_DOMAIN_HEX)
        .env("CARGO_TARGET_DIR", &target)
        .args([
            "build",
            "--locked",
            "--release",
            "--target",
            "wasm32-unknown-unknown",
            "-p",
            "layerx-programs-ai-market",
        ])
        .status()
        .map_err(|error| format!("guest build did not start: {error}"))?;
    if !status.success() {
        return Err(format!("guest build {status}"));
    }
    let wasm =
        fs::read(target.join("wasm32-unknown-unknown/release/layerx_programs_ai_market.wasm"))
            .map_err(|error| format!("guest artifact: {error}"))?;
    let module = WasmEngine::declared()
        .map_err(|error| format!("engine: {error}"))?
        .validate_versioned_metered(ABI_V5_VERSION, &wasm, FuelSchedule::WASMI_0_31_2)
        .map_err(|error| format!("guest validation: {error}"))?;
    if module.abi_revision() != AbiRevision::V5 {
        return Err(format!("guest validated as {:?}", module.abi_revision()));
    }
    Ok(module)
}

/// One budgeted guest execution under the protocol maximum budget through the public
/// qualification route of the executor, returning the record and the admitted token.
fn execute(module: &ValidatedModule) -> Checked<(V2AuthorizedExecutionRecord, AdmittedBudget)> {
    let program = ProgramId::new(PROGRAM).map_err(|error| error.to_string())?;
    let actor = PrincipalId::new(ACTOR).map_err(|error| error.to_string())?;
    let binding = ActivityBudgetBinding::new(ACTIVITY).map_err(|error| error.to_string())?;
    let capabilities = CapabilitySet::new([
        Capability::SharedStorageRead,
        Capability::SharedStorageWrite,
        Capability::EmitEvent,
    ])
    .map_err(|error| error.to_string())?;
    let executor = Executor::declared();
    let admit = || {
        executor
            .admit_activity_budget_for_qualification(
                DeclaredBudget::protocol_maximum(),
                actor,
                binding,
                u128::MAX,
            )
            .map_err(|error| error.to_string())
    };
    let admitted = admit()?;
    let request = BudgetedAuthorizedExecutionRequest::new(
        AuthorizedExecutionRequest {
            module,
            program,
            authorization: AuthorizationContext::new(actor, capabilities),
            receipts: &UnavailableReceiptOracle,
            entrypoint: CALL_ENTRY_EXPORT,
            calldata: CALLDATA,
            composition: CompositionContext::isolated(),
            response_capacity: MAX_CALL_RESPONSE_BYTES,
        },
        admit()?,
        actor,
        binding,
    );
    let record = executor
        .execute_authorized_v2_budgeted_for_qualification(&mut Storage::new(), request)
        .map_err(|error| format!("executor refused the ABI 5 guest: {error}"))?;
    Ok((record, admitted))
}

/// The terminal availability kind the record's outcome must be published under.
const fn kind(record: &V2AuthorizedExecutionRecord) -> u8 {
    match record.outcome() {
        V2ActivityOutcome::Success { .. } => 1,
        V2ActivityOutcome::Failure(_) => 2,
        V2ActivityOutcome::Resource(_) => 3,
    }
}

/// Every field of the decoded v5 terminal is the executed record, every metered resource class
/// is the record's usage, and that usage stays inside the admitted budget.
fn check_binding(
    decoded: &DecodedTerminal,
    record: &V2AuthorizedExecutionRecord,
    admitted: &AdmittedBudget,
) -> Checked {
    assert_eq!(decoded.execution_encoding_version(), Some(5));
    assert!(decoded.attachments.is_empty());
    let TerminalDetail::Execution(ExecutionTerminal::CandidateV4 {
        runtime_version,
        fee_schedule_version,
        metering_schedule_version,
        program,
        abi_version,
        values,
        usage,
        trace,
        graph,
        outcome,
    }) = &decoded.detail
    else {
        return Err(format!("not a v5 execution terminal: {:?}", decoded.detail));
    };
    let execution = record.execution();
    assert_eq!(*runtime_version, execution.runtime_version());
    assert_eq!(*fee_schedule_version, execution.fee_schedule_version());
    assert_eq!(
        *metering_schedule_version,
        execution.metering_schedule_version()
    );
    assert_eq!(*program, PROGRAM);
    assert_eq!(*program, record.root_program().bytes());
    assert_eq!(*abi_version, ABI_V5_VERSION);
    let outputs: Vec<ExecutionValue> = execution
        .outputs()
        .iter()
        .map(|value| match value {
            WasmValue::I32(value) => ExecutionValue::I32(*value),
            WasmValue::I64(value) => ExecutionValue::I64(*value),
        })
        .collect();
    assert_eq!(*values, outputs);
    let executed = execution.usage();
    assert_eq!(usage.cpu_fuel, executed.cpu_fuel);
    assert_eq!(usage.memory_bytes, executed.memory_bytes);
    assert_eq!(usage.storage_read_bytes, executed.storage_read_bytes);
    assert_eq!(usage.storage_write_bytes, executed.storage_write_bytes);
    assert_eq!(usage.output_values, executed.output_values);
    assert_eq!(usage.output_bytes, executed.output_bytes);
    assert_eq!(usage.fee_units, executed.fee_units);
    let limits = admitted.resource_budget();
    assert!(usage.cpu_fuel <= limits.cpu_fuel());
    assert!(usage.memory_bytes <= limits.memory_bytes());
    assert!(usage.storage_read_bytes <= limits.storage_read_bytes());
    assert!(usage.storage_write_bytes <= limits.storage_write_bytes());
    assert!(usage.output_values <= limits.output_values());
    assert!(usage.output_bytes <= limits.output_bytes());
    assert!(usage.fee_units <= admitted.maximum_fee_units());
    assert!(trace.is_none());
    assert_eq!(*graph, record.call_graph().canonical_evidence());
    match (outcome, record.outcome()) {
        (
            CandidateTerminalOutcome::Success { code, response },
            V2ActivityOutcome::Success {
                response: executed, ..
            },
        ) => {
            assert_eq!(*code, executed.code);
            assert_eq!(*response, executed.bytes);
        }
        (CandidateTerminalOutcome::Failure(failure), V2ActivityOutcome::Failure(executed)) => {
            assert_eq!(failure, executed);
        }
        (CandidateTerminalOutcome::Resource(refusal), V2ActivityOutcome::Resource(executed)) => {
            assert_eq!(refusal, executed);
        }
        (decoded, executed) => {
            return Err(format!(
                "terminal outcome {decoded:?} differs from executed {executed:?}"
            ))
        }
    }
    Ok(())
}

/// Offset of the embedded guest ABI field: the 32-byte producer program precedes it.
fn abi_offset(terminal: &[u8], decoded: &DecodedTerminal) -> Checked<usize> {
    let TerminalDetail::Execution(ExecutionTerminal::CandidateV4 { values, trace, .. }) =
        &decoded.detail
    else {
        return Err("not a v5 execution terminal".to_owned());
    };
    let values: usize = values
        .iter()
        .map(|value| match value {
            ExecutionValue::I32(_) => 5,
            ExecutionValue::I64(_) => 9,
        })
        .sum();
    let trace = trace.as_ref().map_or(1, |bytes| 9 + bytes.len());
    let offset = EXECUTION_V5.len() + 2 + 4 + 4 + 8 + values + 8 * 4 + 4 + 8 + 16 + trace + 32;
    if terminal.get(offset..offset + 2) != Some(&ABI_V5_VERSION.to_be_bytes()[..]) {
        return Err(format!("no embedded guest ABI 5 at offset {offset}"));
    }
    Ok(offset)
}

#[test]
fn genuine_abi5_guest_emits_a_v5_terminal_decoding_only_under_abi5() -> Checked {
    let module = guest_module()?;
    let (record, admitted) = execute(&module)?;
    assert_eq!(record.abi_revision(), AbiRevision::V5);
    let terminal = record.canonical_evidence();
    assert!(terminal.starts_with(EXECUTION_V5));
    let kind = kind(&record);
    let decoded = decode_terminal_payload(kind, ABI_V5_VERSION, &terminal)
        .map_err(|error| format!("v5 terminal refused under ABI 5: {error:?}"))?;
    check_binding(&decoded, &record, &admitted)?;
    for other in [0, 1, 2, 3, ABI_V4_VERSION, 6, u16::MAX] {
        assert_eq!(
            decode_terminal_payload(kind, other, &terminal),
            Err(TerminalDecodeError::MismatchedAbi),
            "ABI 5 terminal admitted under expected ABI {other}"
        );
    }
    for other_kind in (1..=3).filter(|&other| other != kind) {
        assert_eq!(
            decode_terminal_payload(other_kind, ABI_V5_VERSION, &terminal),
            Err(TerminalDecodeError::MismatchedKind)
        );
    }
    let mut relabelled = terminal;
    relabelled[..EXECUTION_V4.len()].copy_from_slice(EXECUTION_V4);
    for expected in [2, ABI_V4_VERSION, ABI_V5_VERSION] {
        assert_eq!(
            decode_terminal_payload(kind, expected, &relabelled),
            Err(TerminalDecodeError::MismatchedAbi),
            "ABI 5 evidence relabelled as execution v4 admitted under ABI {expected}"
        );
    }
    Ok(())
}

#[test]
fn abi4_terminal_of_the_same_execution_is_refused_under_abi5() -> Checked {
    let module = guest_module()?;
    let (record, _) = execute(&module)?;
    let terminal = record.canonical_evidence();
    let kind = kind(&record);
    let decoded = decode_terminal_payload(kind, ABI_V5_VERSION, &terminal)
        .map_err(|error| format!("v5 terminal refused under ABI 5: {error:?}"))?;
    let offset = abi_offset(&terminal, &decoded)?;
    let mut abi4 = terminal.clone();
    abi4[offset..offset + 2].copy_from_slice(&ABI_V4_VERSION.to_be_bytes());
    let four = decode_terminal_payload(kind, ABI_V4_VERSION, &abi4)
        .map_err(|error| format!("ABI 4 terminal refused under ABI 4: {error:?}"))?;
    let TerminalDetail::Execution(ExecutionTerminal::CandidateV4 { abi_version, .. }) =
        &four.detail
    else {
        return Err("ABI 4 terminal is not a v5 execution terminal".to_owned());
    };
    assert_eq!(*abi_version, ABI_V4_VERSION);
    assert_eq!(
        decode_terminal_payload(kind, ABI_V5_VERSION, &abi4),
        Err(TerminalDecodeError::MismatchedAbi)
    );
    assert_eq!(
        decode_terminal_payload(kind, ABI_V4_VERSION, &terminal),
        Err(TerminalDecodeError::MismatchedAbi)
    );
    Ok(())
}
