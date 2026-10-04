use layerx_programs_runtime::test_support::{
    code_section, export_section, func_body, function_section, module, raw_section, type_section,
    OP_END, OP_I32_CONST, TYPE_I32,
};
use layerx_programs_runtime::{
    AuthorizationContext, AuthorizedExecutionRequest, CapabilitySet, CompositionContext,
    CompositionRules, Executor, FeeSchedule, PrincipalId, ProgramCatalog, ProgramId,
    ProgramReplayProfile, ProgramReplayRecord, ResourceBudget, Storage, UnavailableReceiptOracle,
    V2ActivityOutcome, WasmEngine,
};
use sha2::{Digest, Sha256};
use std::rc::Rc;

fn take<'a>(bytes: &mut &'a [u8], length: usize) -> &'a [u8] {
    let (value, remaining) = bytes.split_at(length);
    *bytes = remaining;
    value
}

fn u32_field(bytes: &mut &[u8]) -> u32 {
    u32::from_be_bytes(take(bytes, 4).try_into().unwrap())
}

fn actual_leaves(record: &ProgramReplayRecord) -> Vec<&[u8]> {
    use layerx_programs_runtime::replay_record::{
        PROGRAM_REPLAY_RECORD_DOMAIN, PROGRAM_REPLAY_WITNESS_DOMAIN,
    };
    let mut bytes = record.canonical_bytes();
    assert_eq!(
        take(&mut bytes, PROGRAM_REPLAY_RECORD_DOMAIN.len()),
        PROGRAM_REPLAY_RECORD_DOMAIN
    );
    take(&mut bytes, 2 + 64 + 4 + 8 + 8 + 1 + 4 + 64);
    let witness_length = u32_field(&mut bytes) as usize;
    let witness = take(&mut bytes, witness_length);
    assert!(bytes.is_empty());
    assert_eq!(
        <[u8; 32]>::from(Sha256::digest(witness)),
        record.witness_digest()
    );
    let mut bytes = witness;
    assert_eq!(
        take(&mut bytes, PROGRAM_REPLAY_WITNESS_DOMAIN.len()),
        PROGRAM_REPLAY_WITNESS_DOMAIN
    );
    assert_eq!(u32_field(&mut bytes), record.boundary_count());
    let mut leaves = Vec::new();
    let mut hashes = Vec::new();
    for index in 0..record.boundary_count() {
        let length = u32_field(&mut bytes);
        let leaf = take(&mut bytes, length as usize);
        let mut hash = Sha256::new();
        hash.update(b"LXP/program-replay-leaf/v1\0");
        hash.update(index.to_be_bytes());
        hash.update(length.to_be_bytes());
        hash.update(leaf);
        hashes.push(<[u8; 32]>::from(hash.finalize()));
        leaves.push(leaf);
    }
    assert!(bytes.is_empty());
    while hashes.len() > 1 {
        hashes = hashes
            .chunks(2)
            .map(|pair| {
                let mut hash = Sha256::new();
                hash.update(b"LXP/program-replay-node/v1\0");
                hash.update(pair[0]);
                hash.update(*pair.get(1).unwrap_or(&pair[0]));
                <[u8; 32]>::from(hash.finalize())
            })
            .collect();
    }
    assert_eq!(hashes[0], record.boundary_root());
    leaves
}

fn captured_functions(leaf: &[u8]) -> Vec<u32> {
    let mut bytes = leaf;
    let domain = b"LXP/program-replay-boundary/v1\0";
    assert_eq!(take(&mut bytes, domain.len()), domain);
    let state_length = u32_field(&mut bytes) as usize;
    let state = take(&mut bytes, state_length);
    assert!(
        !layerx_programs_runtime::replay_record::decode_portable_state_bytes_untrusted(
            state,
            layerx_programs_runtime::MAX_ARBITRATION_STATE_BYTES,
        )
        .unwrap()
        .is_empty()
    );
    let count = u32_field(&mut bytes);
    let mut functions = Vec::new();
    for _ in 0..count {
        functions.push(u32_field(&mut bytes));
        take(&mut bytes, 8);
        let operand_count = u32_field(&mut bytes) as usize;
        take(&mut bytes, operand_count);
    }
    functions
}

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
    assert_eq!(record.terminal_status(), 0);
    assert!(matches!(
        result.outcome(),
        V2ActivityOutcome::Success { .. }
    ));
    assert!(!actual_leaves(record).is_empty());
    assert_eq!(
        record.canonical_bytes(),
        run(false, false, true)
            .replay_record()
            .unwrap()
            .canonical_bytes()
    );
}
#[test]
fn actual_module_start_is_retained() {
    let started = run(false, true, true);
    let ordinary = run(false, false, true);
    let engine = WasmEngine::declared().unwrap();
    let module = engine.validate_v2(&guest(false, true)).unwrap();
    let initializer = wasmparser_nostd::Parser::new(0)
        .parse_all(module.meter_injection().instrumented_wasm())
        .find_map(|payload| match payload.unwrap() {
            wasmparser_nostd::Payload::StartSection { func, .. } => Some(func),
            _ => None,
        })
        .expect("actual instrumented initializer");
    assert!(actual_leaves(started.replay_record().unwrap())
        .iter()
        .any(|leaf| captured_functions(leaf).contains(&initializer)));
    assert!(
        started.replay_record().unwrap().boundary_count()
            > ordinary.replay_record().unwrap().boundary_count()
    );
    assert_ne!(
        started.replay_record().unwrap().boundary_root(),
        ordinary.replay_record().unwrap().boundary_root()
    );
}
#[test]
fn actual_unreachable_has_a_distinct_retained_terminal_record() {
    let result = run(true, false, true);
    assert!(result.replay_record().is_some());
    assert_eq!(result.replay_record().unwrap().terminal_status(), 1);
    assert!(matches!(result.outcome(), V2ActivityOutcome::Failure(_)));
    assert!(result.execution().trace().is_none());
    actual_leaves(result.replay_record().unwrap());
    assert!(run(true, false, false).replay_record().is_none());
}
#[test]
fn invalid_signed_profile_bounds_are_refused() {
    for bounds in [
        (0, 1),
        (1, 1),
        (128, 0),
        (128, 511),
        (4097, 1_048_576),
        (128, 1_048_577),
        (u32::MAX, 1_048_576),
    ] {
        assert!(ProgramReplayProfile::new(bounds.0, bounds.1).is_err());
    }
}
