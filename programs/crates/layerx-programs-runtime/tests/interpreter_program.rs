use std::path::PathBuf;

use layerx_programs_runtime::{
    AuthorizationContext, AuthorizedExecutionRequest, CandidateActivityOutcome, Capability,
    CapabilitySet, CompositionContext, Executor, FeeSchedule, PrincipalId, ProgramId,
    ResourceBudget, Storage, StorageNamespace, UnavailableReceiptOracle, WasmEngine,
    CALL_ENTRY_EXPORT,
};

const SUCCESS_VECTORS: &[u8] =
    include_bytes!("../../../crates/layerx-programs-interpreter/vectors/v1-arithmetic.hex");
const REFUSAL_VECTORS: &[u8] =
    include_bytes!("../../../crates/layerx-programs-interpreter/vectors/v1-refusals.hex");
const ASSET: [u8; 32] = [1; 32];
const RECIPIENT: [u8; 32] = [2; 32];

#[derive(Clone, Copy, Debug)]
enum RefusalStage {
    StepCeiling,
    NonCanonicalRepeat,
    NestingDepth,
    ArithmeticOverflow,
    DivisionByZero,
    InvalidTransferAmount,
}

fn vectors(source: &[u8]) -> Vec<Vec<u8>> {
    fn nibble(byte: u8) -> u8 {
        match byte {
            b'0'..=b'9' => byte - b'0',
            b'a'..=b'f' => byte - b'a' + 10,
            _ => panic!("non-hex interpreter vector"),
        }
    }
    std::str::from_utf8(source)
        .unwrap_or_else(|error| panic!("vector utf8: {error}"))
        .lines()
        .filter(|line| !line.is_empty() && !line.starts_with('#'))
        .map(|line| {
            assert_eq!(line.len() % 2, 0);
            line.as_bytes()
                .chunks_exact(2)
                .map(|pair| (nibble(pair[0]) << 4) | nibble(pair[1]))
                .collect()
        })
        .collect()
}

fn artifact() -> Vec<u8> {
    let path = std::env::var_os("LAYERX_INTERPRETER_WASM").map_or_else(
        || panic!("LAYERX_INTERPRETER_WASM must name the built interpreter Wasm"),
        PathBuf::from,
    );
    std::fs::read(&path).unwrap_or_else(|error| panic!("read {}: {error}", path.display()))
}

fn execute(
    wasm: &[u8],
    calldata: &[u8],
    storage: &mut Storage,
) -> layerx_programs_runtime::CandidateAuthorizedExecutionRecord {
    let program = ProgramId::new([0x33; 32]).unwrap_or_else(|error| panic!("program: {error}"));
    let principal =
        PrincipalId::new([0x44; 32]).unwrap_or_else(|error| panic!("principal: {error}"));
    let capabilities = CapabilitySet::new([
        Capability::StorageRead,
        Capability::StorageWrite,
        Capability::Transfer402 {
            asset: ASSET,
            to: RECIPIENT,
            maximum_amount: 100,
        },
    ])
    .unwrap_or_else(|error| panic!("capabilities: {error}"));
    execute_with(
        wasm,
        calldata,
        storage,
        program,
        principal,
        capabilities,
        budget(),
    )
}

fn budget() -> ResourceBudget {
    ResourceBudget::new_complete(
        10_000_000,
        16 * 1_024 * 1_024,
        1_048_576,
        1_048_576,
        64,
        1_048_576,
        4_096,
    )
}

fn execute_with(
    wasm: &[u8],
    calldata: &[u8],
    storage: &mut Storage,
    program: ProgramId,
    principal: PrincipalId,
    capabilities: CapabilitySet,
    budget: ResourceBudget,
) -> layerx_programs_runtime::CandidateAuthorizedExecutionRecord {
    let engine = WasmEngine::declared().unwrap_or_else(|error| panic!("engine: {error}"));
    let module = engine
        .validate_v2(wasm)
        .unwrap_or_else(|error| panic!("interpreter validation: {error}"));
    Executor::new(budget, FeeSchedule::declared())
        .execute_authorized_candidate(
            storage,
            AuthorizedExecutionRequest {
                module: &module,
                program,
                authorization: AuthorizationContext::new(principal, capabilities),
                receipts: &UnavailableReceiptOracle,
                entrypoint: CALL_ENTRY_EXPORT,
                calldata,
                composition: CompositionContext::isolated(),
                response_capacity: 4,
            },
        )
        .unwrap_or_else(|error| panic!("interpreter execution: {error}"))
}

fn namespace() -> StorageNamespace {
    StorageNamespace::principal(
        ProgramId::new([0x33; 32]).unwrap_or_else(|error| panic!("program: {error}")),
        PrincipalId::new([0x44; 32]).unwrap_or_else(|error| panic!("principal: {error}")),
    )
}

#[test]
fn built_interpreter_runs_success_vectors_through_the_real_candidate_runtime() {
    let wasm = artifact();
    let scripts = vectors(SUCCESS_VECTORS);
    assert_eq!(scripts.len(), 4);
    for (index, script) in scripts.iter().enumerate() {
        let mut storage = Storage::new();
        let record = execute(&wasm, script, &mut storage);
        let CandidateActivityOutcome::Success { response, effects } = record.outcome() else {
            panic!("success vector {index} refused");
        };
        let expected_steps = [5_u32, 17, 13, 5][index];
        assert_eq!(response.bytes.as_slice(), expected_steps.to_be_bytes());
        let transaction = storage.transaction(namespace());
        match index {
            0 => assert_eq!(
                transaction.read(b"sum"),
                Ok(Some(12_i64.to_be_bytes().to_vec()))
            ),
            1 => {
                assert_eq!(transaction.read(b"a"), Ok(None));
                assert_eq!(effects.transfers.len(), 1);
                assert_eq!(effects.transfers[0].asset, ASSET);
                assert_eq!(effects.transfers[0].to, RECIPIENT);
                assert_eq!(effects.transfers[0].amount, 8);
            }
            2 => {
                for (key, value) in [
                    (b"sub".as_slice(), 6_i64),
                    (b"mul", 27),
                    (b"div", 3),
                    (b"eq", 0),
                    (b"lt", 1),
                ] {
                    assert_eq!(
                        transaction.read(key),
                        Ok(Some(value.to_be_bytes().to_vec()))
                    );
                }
            }
            3 => assert!(effects.transfers.is_empty()),
            _ => unreachable!(),
        }
    }
}

#[test]
fn built_interpreter_refusals_leave_real_runtime_state_and_effects_empty() {
    let wasm = artifact();
    let expected = [
        RefusalStage::StepCeiling,
        RefusalStage::NonCanonicalRepeat,
        RefusalStage::ArithmeticOverflow,
        RefusalStage::DivisionByZero,
        RefusalStage::ArithmeticOverflow,
        RefusalStage::InvalidTransferAmount,
        RefusalStage::NestingDepth,
        RefusalStage::ArithmeticOverflow,
    ];
    for (index, (script, expected_stage)) in
        vectors(REFUSAL_VECTORS).iter().zip(expected).enumerate()
    {
        if matches!(
            expected_stage,
            RefusalStage::ArithmeticOverflow | RefusalStage::DivisionByZero
        ) {
            assert_eq!(
                script[5], 3,
                "{expected_stage:?} vector {index} register cardinality"
            );
        }
        let mut storage = Storage::new();
        let before = storage.clone();
        let record = execute(&wasm, script, &mut storage);
        assert!(
            matches!(record.outcome(), CandidateActivityOutcome::Failure(_)),
            "{expected_stage:?} vector {index}"
        );
        assert_eq!(storage, before, "{expected_stage:?} vector {index}");
    }
}

fn storage_grants() -> CapabilitySet {
    CapabilitySet::new([Capability::StorageRead, Capability::StorageWrite])
        .unwrap_or_else(|error| panic!("storage capabilities: {error}"))
}

#[test]
fn built_interpreter_refuses_absent_foreign_and_insufficient_transfer_grants() {
    let wasm = artifact();
    let scripts = vectors(SUCCESS_VECTORS);
    let program = ProgramId::new([0x33; 32]).unwrap_or_else(|error| panic!("program: {error}"));
    let principal =
        PrincipalId::new([0x44; 32]).unwrap_or_else(|error| panic!("principal: {error}"));
    for grant in [
        None,
        Some(Capability::Transfer402 {
            asset: [3; 32],
            to: RECIPIENT,
            maximum_amount: 100,
        }),
        Some(Capability::Transfer402 {
            asset: ASSET,
            to: [3; 32],
            maximum_amount: 100,
        }),
        Some(Capability::Transfer402 {
            asset: ASSET,
            to: RECIPIENT,
            maximum_amount: 7,
        }),
    ] {
        let mut grants = vec![Capability::StorageRead, Capability::StorageWrite];
        grants.extend(grant);
        let capabilities =
            CapabilitySet::new(grants).unwrap_or_else(|error| panic!("capabilities: {error}"));
        let mut storage = Storage::new();
        let mut transaction = storage.transaction(namespace());
        transaction
            .write(b"a", &91_i64.to_be_bytes())
            .unwrap_or_else(|error| panic!("seed: {error}"));
        let _ = transaction.commit();
        let before = storage.clone();
        let record = execute_with(
            &wasm,
            &scripts[1],
            &mut storage,
            program,
            principal,
            capabilities,
            budget(),
        );
        assert!(matches!(
            record.outcome(),
            CandidateActivityOutcome::Failure(_)
        ));
        assert!(record.effects().is_none());
        assert_eq!(storage, before);
    }
}

#[test]
fn built_interpreter_storage_reads_and_writes_stay_in_program_principal_namespace() {
    let wasm = artifact();
    let program = ProgramId::new([0x33; 32]).unwrap_or_else(|error| panic!("program: {error}"));
    let principal =
        PrincipalId::new([0x44; 32]).unwrap_or_else(|error| panic!("principal: {error}"));
    let other_program =
        ProgramId::new([0x55; 32]).unwrap_or_else(|error| panic!("program: {error}"));
    let other_principal =
        PrincipalId::new([0x66; 32]).unwrap_or_else(|error| panic!("principal: {error}"));
    let owned = StorageNamespace::principal(program, principal);
    let adjacent = [
        StorageNamespace::principal(program, other_principal),
        StorageNamespace::principal(other_program, principal),
    ];
    let mut storage = Storage::new();
    for namespace in adjacent {
        let mut transaction = storage.transaction(namespace);
        transaction
            .write(b"sum", &99_i64.to_be_bytes())
            .unwrap_or_else(|error| panic!("seed: {error}"));
        transaction
            .write(b"seen", &77_i64.to_be_bytes())
            .unwrap_or_else(|error| panic!("seed: {error}"));
        let _ = transaction.commit();
    }
    let script = vectors(b"4c58534901010003000e08000373756d0900047365656e00");
    let record = execute_with(
        &wasm,
        &script[0],
        &mut storage,
        program,
        principal,
        storage_grants(),
        budget(),
    );
    assert!(matches!(
        record.outcome(),
        CandidateActivityOutcome::Success { .. }
    ));
    assert_eq!(
        storage.transaction(owned).read(b"seen"),
        Ok(Some(0_i64.to_be_bytes().to_vec()))
    );
    assert_eq!(storage.transaction(owned).read(b"sum"), Ok(None));
    for namespace in adjacent {
        let transaction = storage.transaction(namespace);
        assert_eq!(
            transaction.read(b"sum"),
            Ok(Some(99_i64.to_be_bytes().to_vec()))
        );
        assert_eq!(
            transaction.read(b"seen"),
            Ok(Some(77_i64.to_be_bytes().to_vec()))
        );
    }
}

#[test]
fn built_interpreter_repeated_wasm_execution_has_identical_records_and_resource_usage() {
    let wasm = artifact();
    for script in vectors(SUCCESS_VECTORS)
        .into_iter()
        .chain(vectors(REFUSAL_VECTORS))
    {
        let mut first_storage = Storage::new();
        let mut second_storage = Storage::new();
        let first = execute(&wasm, &script, &mut first_storage);
        let second = execute(&wasm, &script, &mut second_storage);
        assert_eq!(first, second);
        assert_eq!(first.canonical_evidence(), second.canonical_evidence());
        assert_eq!(
            first.receipt_projection().canonical_encode(),
            second.receipt_projection().canonical_encode()
        );
        assert_eq!(first.execution().usage(), second.execution().usage());
        assert_eq!(first_storage, second_storage);
    }
}
