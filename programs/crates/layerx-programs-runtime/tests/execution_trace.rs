use layerx_programs_runtime::test_support::{
    code_section, export_section, func_body, function_section, module, raw_section, type_section,
    unsigned_leb, OP_CALL, OP_DROP, OP_END, OP_I32_ADD, OP_I32_CONST, OP_LOCAL_GET, TYPE_I32,
};
use layerx_programs_runtime::{
    ArbitrationStepCommitment, ExecutionTrace, StepCommitment, ARBITRATION_STEP_COMMITMENT_DOMAIN,
    STEP_COMMITMENT_DOMAIN,
};
use layerx_programs_runtime::{
    ExecutionError, ExecutionFault, Executor, FeeSchedule, ResourceBudget, TracePolicy,
    ValidatedModule, WasmEngine, WasmValue,
};

use sha2::{Digest, Sha256};

const STATE_RICH_TRACED_CPU_FUEL: u64 = 10_654_360;
const TRACE_TEST_CPU_HEADROOM: u64 = STATE_RICH_TRACED_CPU_FUEL * 2;

fn state_rich_module() -> Vec<u8> {
    let memory_section = raw_section(5, &[1, 0, 1]);
    let global_section = raw_section(
        6,
        &[
            2,
            TYPE_I32,
            1,
            OP_I32_CONST,
            0,
            OP_END,
            TYPE_I32,
            0,
            OP_I32_CONST,
            9,
            OP_END,
        ],
    );
    module(&[
        type_section(&[(&[TYPE_I32], &[TYPE_I32]), (&[TYPE_I32], &[TYPE_I32])]),
        function_section(&[0, 1]),
        memory_section,
        global_section,
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

fn validated(wasm: &[u8]) -> ValidatedModule {
    let engine = WasmEngine::declared()
        .unwrap_or_else(|error| panic!("engine construction refused: {error}"));
    engine
        .validate(wasm)
        .unwrap_or_else(|error| panic!("module validation refused: {error}"))
}

fn trace_executor(policy: TracePolicy) -> Executor {
    let declared = ResourceBudget::declared();
    Executor::new(
        ResourceBudget::new_complete(
            TRACE_TEST_CPU_HEADROOM,
            declared.memory_bytes(),
            declared.storage_read_bytes(),
            declared.storage_write_bytes(),
            declared.output_values(),
            declared.output_bytes(),
            declared.table_elements(),
        ),
        FeeSchedule::declared(),
    )
    .with_trace_policy(policy)
}

fn traced_state_rich_call() -> layerx_programs_runtime::TracedExecutionRecord {
    let policy =
        TracePolicy::new(3, 256).unwrap_or_else(|error| panic!("trace policy refused: {error}"));
    let record = trace_executor(policy)
        .execute_traced(
            &validated(&state_rich_module()),
            "run",
            &[WasmValue::I32(7)],
        )
        .unwrap_or_else(|error| panic!("traced execution refused: {error}"));
    assert_eq!(record.execution.usage.cpu_fuel, STATE_RICH_TRACED_CPU_FUEL);
    record
}

#[test]
fn traced_execution_captures_complete_integer_runtime_state() {
    let record = traced_state_rich_call();
    assert_eq!(record.execution.outputs, vec![WasmValue::I32(18)]);
    assert_eq!(record.trace.policy().interval(), 3);
    assert!(!record.trace.steps().is_empty());
    assert!(!record.trace.commitments().is_empty());
    let mut expected_commitments = Vec::new();
    for step in record.trace.steps() {
        for commitment in [step.pre_commitment, step.post_commitment] {
            if expected_commitments
                .last()
                .map(|prior: &layerx_programs_runtime::StepCommitment| prior.step_index)
                != Some(commitment.step_index)
            {
                expected_commitments.push(commitment);
            }
        }
    }
    assert_eq!(record.trace.commitments(), expected_commitments);
    let mut expected_arbitration_commitments = Vec::new();
    for step in record.trace.arbitration_steps() {
        for commitment in [step.pre_commitment, step.post_commitment] {
            if expected_arbitration_commitments
                .last()
                .map(|prior: &layerx_programs_runtime::ArbitrationStepCommitment| prior.step_index)
                != Some(commitment.step_index)
            {
                expected_arbitration_commitments.push(commitment);
            }
        }
    }
    assert_eq!(
        record.trace.arbitration_commitments(),
        expected_arbitration_commitments
    );
    assert!(record.trace.steps().iter().any(|step| {
        step.pre_state.globals.iter().any(|global| {
            global.mutable
                && matches!(
                    global.value,
                    layerx_programs_runtime::ExecutionValue::I32(7)
                )
        })
    }));
    assert!(record.trace.steps().iter().all(|step| {
        step.pre_state.globals.iter().any(|global| {
            !global.mutable
                && matches!(
                    global.value,
                    layerx_programs_runtime::ExecutionValue::I32(9)
                )
        })
    }));
    assert!(record.trace.steps().iter().any(|step| {
        step.post_state.linear_memory.len() > 65_536 || step.memory_expansion_bytes >= 65_536
    }));
    assert!(record.trace.steps().iter().any(|step| {
        step.pre_state.call_frames.len() > 1
            && step
                .pre_state
                .call_frames
                .iter()
                .any(|frame| !frame.locals.is_empty())
    }));
    assert!(record.trace.steps().iter().any(|step| {
        step.pre_state.call_frames.len() > 1
            && step.pre_state.value_stack.len()
                > step
                    .pre_state
                    .call_frames
                    .iter()
                    .map(|frame| frame.locals.len())
                    .sum::<usize>()
    }));
}

#[test]
fn traced_execution_evidence_is_canonical_and_repeatable() {
    let first = traced_state_rich_call();
    let second = traced_state_rich_call();
    let first_evidence = first
        .canonical_evidence()
        .unwrap_or_else(|error| panic!("first evidence refused: {error}"));
    let second_evidence = second
        .canonical_evidence()
        .unwrap_or_else(|error| panic!("second evidence refused: {error}"));
    assert_eq!(first.trace, second.trace);
    assert_eq!(first_evidence, second_evidence);
}

#[test]
fn receipt_commitment_total_equals_the_metered_trace_delta() {
    let wasm = state_rich_module();
    let module = validated(&wasm);
    let plain = Executor::declared()
        .execute(&module, "run", &[WasmValue::I32(7)])
        .unwrap_or_else(|error| panic!("plain execution refused: {error}"));
    let traced = traced_state_rich_call();
    let charged = traced
        .execution
        .usage
        .cpu_fuel
        .checked_sub(plain.usage.cpu_fuel)
        .unwrap_or_else(|| panic!("traced execution consumed less fuel than plain execution"));
    let total_trace_fuel = traced
        .trace
        .total_commitment_fuel()
        .checked_add(traced.trace.total_arbitration_commitment_fuel())
        .unwrap_or_else(|| panic!("trace fuel total overflowed"));
    assert_eq!(charged, total_trace_fuel);
    assert_eq!(
        traced.trace.total_commitment_fuel(),
        traced
            .trace
            .commitments()
            .iter()
            .map(|commitment| commitment.commitment_fuel)
            .sum(),
    );
    assert_eq!(
        traced.trace.total_arbitration_commitment_fuel(),
        traced
            .trace
            .arbitration_commitments()
            .iter()
            .map(|commitment| commitment.commitment_fuel)
            .sum(),
    );
}

#[test]
fn trace_identity_distinguishes_code_and_inputs() {
    let first = traced_state_rich_call();
    let policy =
        TracePolicy::new(3, 256).unwrap_or_else(|error| panic!("trace policy refused: {error}"));
    let different_input = trace_executor(policy)
        .execute_traced(
            &validated(&state_rich_module()),
            "run",
            &[WasmValue::I32(8)],
        )
        .unwrap_or_else(|error| panic!("traced execution refused: {error}"));
    let mut distinct_code = state_rich_module();
    let custom_name = b"distinct-module-identity";
    let mut custom_payload = unsigned_leb(custom_name.len() as u64);
    custom_payload.extend_from_slice(custom_name);
    distinct_code.extend(raw_section(0, &custom_payload));
    let different_code = trace_executor(policy)
        .execute_traced(&validated(&distinct_code), "run", &[WasmValue::I32(7)])
        .unwrap_or_else(|error| panic!("traced execution refused: {error}"));
    assert_ne!(
        first.trace.commitments()[0].digest,
        different_input.trace.commitments()[0].digest
    );
    assert_ne!(
        first.trace.commitments()[0].digest,
        different_code.trace.commitments()[0].digest
    );
}

#[test]
fn ordinary_observer_trace_is_the_complete_canonical_record() {
    let record = traced_state_rich_call();
    let evidence = record
        .canonical_evidence()
        .unwrap_or_else(|error| panic!("ordinary trace evidence refused: {error}"));
    let execution = record.execution.canonical_evidence();
    let trace = record
        .trace
        .canonical_arbitration_bytes()
        .unwrap_or_else(|error| panic!("ordinary trace encoding refused: {error}"));
    let mut complete = b"LXP/program-traced-execution/v2\0".to_vec();
    complete.extend_from_slice(
        &u32::try_from(execution.len())
            .unwrap_or_else(|_| panic!("execution evidence exceeds u32"))
            .to_be_bytes(),
    );
    complete.extend_from_slice(&execution);
    complete.extend_from_slice(
        &u32::try_from(trace.len())
            .unwrap_or_else(|_| panic!("trace evidence exceeds u32"))
            .to_be_bytes(),
    );
    complete.extend_from_slice(&trace);
    assert_eq!(evidence, complete);
}

#[test]
fn ordinary_observer_emits_complete_v2_arbitration_state() {
    let record = traced_state_rich_call();
    assert!(record.trace.is_arbitration_eligible());
    assert_eq!(
        record.trace.arbitration_steps().len(),
        record.trace.steps().len(),
    );
    for step in record.trace.arbitration_steps() {
        assert!(step.pre_commitment.arbitration_eligible());
        assert!(step.post_commitment.arbitration_eligible());
        assert!(!step.pre_state.engine_state.is_empty());
        assert!(!step.post_state.engine_state.is_empty());
        assert_ne!(step.pre_state.identity.module_code_hash, [0; 32]);
        assert_ne!(step.pre_state.identity.input_digest, [0; 32]);
        assert_eq!(
            step.pre_commitment,
            layerx_programs_runtime::ArbitrationStepCommitment::from_state(
                step.pre_state.as_ref(),
            ).unwrap_or_else(|error| panic!("pre-state v2 commitment refused: {error}")),
        );
        assert_eq!(
            step.post_commitment,
            layerx_programs_runtime::ArbitrationStepCommitment::from_state(
                step.post_state.as_ref(),
            )
            .unwrap_or_else(|error| panic!("post-state v2 commitment refused: {error}")),
        );
    }
}

#[test]
fn trapped_execution_refuses_partial_trace_evidence() {
    let policy =
        TracePolicy::new(1, 64).unwrap_or_else(|error| panic!("trace policy refused: {error}"));
    let result = trace_executor(policy).execute_traced(&validated(&trapping_module()), "run", &[]);
    match result {
        Err(ExecutionError::Fault(ExecutionFault::EngineFault { reason })) => {
            assert!(reason.contains("execution observer refused"));
        }
        other => panic!("trapped trace did not fail closed: {other:?}"),
    }
}

#[test]
fn receipt_commitment_bound_refuses_an_incomplete_chain() {
    let policy =
        TracePolicy::new(1, 1).unwrap_or_else(|error| panic!("trace policy refused: {error}"));
    let result = trace_executor(policy).execute_traced(
        &validated(&state_rich_module()),
        "run",
        &[WasmValue::I32(7)],
    );
    match result {
        Err(ExecutionError::Fault(ExecutionFault::EngineFault { reason })) => {
            assert_eq!(
                reason,
                "deterministic execution commitment refused: execution trace exceeds commitment limit 1"
            );
        }
        other => panic!("bounded trace returned partial evidence: {other:?}"),
    }
}

/// Golden vectors for the frozen v1 state/trace and v2 arbitration
/// state/commitment/trace encodings of `traced_state_rich_call` (module
/// `state_rich_module`, entry `run`, input `I32(7)`, `TracePolicy::new(3, 256)`).
/// Provenance: produced by `ExecutionState::canonical_bytes`,
/// `StepCommitment::from_state`, `ExecutionTrace::canonical_bytes`,
/// `ArbitrationExecutionState::canonical_bytes`,
/// `ArbitrationStepCommitment::from_state`,
/// `ExecutionTrace::canonical_arbitration_bytes` and
/// `TracedExecutionRecord::canonical_evidence` at revision 742131dda with every
/// production encoder unchanged; digests are SHA-256 of the exact bytes.
struct EncodingVector {
    length: usize,
    sha256: &'static str,
}

const V1_FIRST_STATE: EncodingVector = EncodingVector {
    length: 65_819,
    sha256: "c99ddaec30aff416fc76c53fccffbe367abb6f83a217aa103074368229b4cc3e",
};
const V1_LAST_STATE: EncodingVector = EncodingVector {
    length: 131_335,
    sha256: "fdc202fa257b432adcc451c9ceb39d76c716a8717dfc014d17f344fd22052ceb",
};
const V1_TRACE: EncodingVector = EncodingVector {
    length: 1_698,
    sha256: "cd2458a964eec9ee06a8a39e635e95897cfef83b42af1fabe4759ffd0cf1702f",
};
const V1_TRACE_PREFIX: &str = "000100000000000000030000010000000020";
const V2_FIRST_STATE: EncodingVector = EncodingVector {
    length: 131_682,
    sha256: "626efdd7f844f767b7941f69fc66f3619f2e5fd008c68bf84824adbe2f432ff3",
};
const V2_LAST_STATE: EncodingVector = EncodingVector {
    length: 262_734,
    sha256: "ee3e85ca7d94e65210c4a069ba2ed5c42be844cc251a9f704f885969e54f7e96",
};
const V2_TRACE: EncodingVector = EncodingVector {
    length: 3_452,
    sha256: "06aeb50b0c188b04c4139c1f7d7d0fc282d311524960cde6092ce272d69bd5b7",
};
const TRACED_EVIDENCE: EncodingVector = EncodingVector {
    length: 7_059,
    sha256: "7a60515c8ccb89b0d9ee7cec7cb9393e1d296eb6ee43ef70fc77ab8c59150a3f",
};
const MODULE_CODE_HASH: &str = "2ccd158cb5eed8b5fbe9438bd4704faf6d107be34f5859f6593faac71b0753e2";
const INPUT_DIGEST: &str = "bce001aa0a14abd3729d4e4dbc4019c0e27c77127296eb4d4c8d9af21c238b59";

fn assert_vector(name: &str, bytes: &[u8], expected: &EncodingVector) {
    assert_eq!(bytes.len(), expected.length, "{name} length");
    assert_eq!(
        hex::encode(Sha256::digest(bytes)),
        expected.sha256,
        "{name} digest"
    );
}

fn assert_v1_commitment(
    commitment: StepCommitment,
    state: &[u8],
    step_index: u64,
    digest: &str,
    fuel: u64,
) {
    let mut hasher = Sha256::new();
    hasher.update(STEP_COMMITMENT_DOMAIN);
    hasher.update(state);
    let recomputed: [u8; 32] = hasher.finalize().into();
    assert_eq!(commitment.digest, recomputed);
    assert_eq!(commitment.step_index, step_index);
    assert_eq!(hex::encode(commitment.digest), digest);
    assert_eq!(
        usize::try_from(commitment.encoded_state_bytes).ok(),
        Some(state.len())
    );
    assert_eq!(commitment.commitment_fuel, fuel);
}

fn assert_v2_commitment(
    commitment: ArbitrationStepCommitment,
    state: &[u8],
    step_index: u64,
    digest: &str,
    fuel: u64,
) {
    let mut hasher = Sha256::new();
    hasher.update(ARBITRATION_STEP_COMMITMENT_DOMAIN);
    hasher.update(state);
    let recomputed: [u8; 32] = hasher.finalize().into();
    assert_eq!(commitment.version, 2);
    assert_eq!(commitment.digest, recomputed);
    assert_eq!(commitment.step_index, step_index);
    assert_eq!(hex::encode(commitment.digest), digest);
    assert_eq!(
        usize::try_from(commitment.encoded_state_bytes).ok(),
        Some(state.len())
    );
    assert_eq!(commitment.commitment_fuel, fuel);
}

#[test]
fn legacy_v1_and_v2_encodings_match_pinned_golden_vectors() {
    let record = traced_state_rich_call();
    let trace = &record.trace;
    assert_eq!(trace.steps().len(), 16);
    assert_eq!(trace.commitments().len(), 32);
    assert_eq!(trace.arbitration_steps().len(), 16);
    assert_eq!(trace.arbitration_commitments().len(), 32);
    assert_eq!(trace.total_commitment_fuel(), 3_550_082);
    assert_eq!(trace.total_state_bytes(), 3_549_086);
    assert_eq!(trace.total_arbitration_commitment_fuel(), 7_103_234);
    assert_eq!(trace.total_arbitration_state_bytes(), 7_098_494);

    let encode_v1 = |state: &layerx_programs_runtime::ExecutionState| {
        state
            .canonical_bytes()
            .unwrap_or_else(|error| panic!("v1 state encoding refused: {error}"))
    };
    let first_steps = &trace.steps()[0];
    let last_steps = &trace.steps()[trace.steps().len() - 1];
    let v1_first = encode_v1(&first_steps.pre_state);
    let v1_last = encode_v1(&last_steps.post_state);
    assert_vector("v1 first state", &v1_first, &V1_FIRST_STATE);
    assert_vector("v1 last state", &v1_last, &V1_LAST_STATE);
    assert_v1_commitment(
        trace.commitments()[0],
        &v1_first,
        0,
        "dd7e2c64cb0f5d7ed9a607e62c40453c43b3596dd5360f20ed8e9b8a91d262aa",
        65_851,
    );
    assert_v1_commitment(
        trace.commitments()[31],
        &v1_last,
        46,
        "b684caba2d3954f5533433f2e98181b7a4b2e3e0549119b0ff395be9ac8e8b6d",
        131_367,
    );
    let v1_trace = trace
        .canonical_bytes()
        .unwrap_or_else(|error| panic!("v1 trace encoding refused: {error}"));
    assert_vector("v1 trace", &v1_trace, &V1_TRACE);
    assert_eq!(hex::encode(&v1_trace[..18]), V1_TRACE_PREFIX);

    let encode_v2 = |state: &layerx_programs_runtime::ArbitrationExecutionState| {
        state
            .canonical_bytes()
            .unwrap_or_else(|error| panic!("v2 state encoding refused: {error}"))
    };
    let first_arbitration = &trace.arbitration_steps()[0];
    let last_arbitration = &trace.arbitration_steps()[trace.arbitration_steps().len() - 1];
    assert_eq!(
        hex::encode(first_arbitration.pre_state.identity.module_code_hash),
        MODULE_CODE_HASH
    );
    assert_eq!(
        hex::encode(first_arbitration.pre_state.identity.input_digest),
        INPUT_DIGEST
    );
    assert_eq!(first_arbitration.pre_state.engine_state.len(), 65_597);
    assert_eq!(first_arbitration.pre_state.host_state_bytes, 39);
    let v2_first = encode_v2(&first_arbitration.pre_state);
    let v2_last = encode_v2(&last_arbitration.post_state);
    assert_vector("v2 first state", &v2_first, &V2_FIRST_STATE);
    assert_vector("v2 last state", &v2_last, &V2_LAST_STATE);
    assert_v2_commitment(
        trace.arbitration_commitments()[0],
        &v2_first,
        0,
        "08e8ae5225691dfc372821b8c2543c1dcc932ee1b61e2ceb4590960fadc87a49",
        131_831,
    );
    assert_v2_commitment(
        trace.arbitration_commitments()[31],
        &v2_last,
        46,
        "cc98c0dad8e9ec39a8614e856ee5d6d917219c04e02e84bf8d1d907d64e026ee",
        262_883,
    );
    let v2_trace = trace
        .canonical_arbitration_bytes()
        .unwrap_or_else(|error| panic!("v2 trace encoding refused: {error}"));
    assert_vector("v2 trace", &v2_trace, &V2_TRACE);
    assert_eq!(&v2_trace[..2], &2_u16.to_be_bytes());
    assert_eq!(&v2_trace[6..6 + v1_trace.len()], v1_trace.as_slice());
    ExecutionTrace::verify_canonical_arbitration_bytes(&v2_trace)
        .unwrap_or_else(|error| panic!("pinned v2 trace refused: {error}"));

    let evidence = record
        .canonical_evidence()
        .unwrap_or_else(|error| panic!("traced evidence refused: {error}"));
    assert_vector("traced evidence", &evidence, &TRACED_EVIDENCE);
}
