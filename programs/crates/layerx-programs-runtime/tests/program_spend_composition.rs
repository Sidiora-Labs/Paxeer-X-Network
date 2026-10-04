use layerx_programs_runtime::{
    derive_program_account, AbiError, Capability, CapabilitySet, ProgramId,
    AuthorizationContext, AuthorizedExecutionRequest, CompositionContext,
    CompositionRules, ProgramCatalog, PrincipalId, Storage, Executor, WasmEngine,
    ExecutionError, CompositionRefusal, CALL_ENTRY_EXPORT,
};

fn spend(owner: ProgramId, asset: [u8; 32], to: [u8; 32], maximum: u128) -> Capability {
    let seed = b"composition/retained";
    Capability::ProgramSpend {
        owner_program: owner,
        seed: seed.to_vec(),
        source_account: derive_program_account(owner, seed).expect("derived account").bytes(),
        asset,
        to,
        maximum_amount: maximum,
    }
}

#[test]
fn canonical_program_grants_narrow_at_every_depth_and_repeated_visit() {
    let owner = ProgramId::new([1; 32]).expect("owner");
    let mut inherited = CapabilitySet::new([spend(owner, [2; 32], [3; 32], 100)])
        .expect("root grant");
    for maximum in (1..100).rev() {
        let bytes = inherited.canonical_encoding();
        let decoded = CapabilitySet::new(CapabilitySet::decode_v2_canonical(&bytes)
            .expect("canonical grant")).expect("decoded grant");
        assert_eq!(decoded, inherited);
        assert_eq!(decoded.narrow([spend(owner, [2; 32], [3; 32], maximum + 2)]),
            Err(AbiError::CapabilityEscalation));
        assert_eq!(decoded.narrow([spend(owner, [4; 32], [3; 32], maximum)]),
            Err(AbiError::CapabilityEscalation));
        assert_eq!(decoded.narrow([spend(owner, [2; 32], [4; 32], maximum)]),
            Err(AbiError::CapabilityEscalation));
        inherited = decoded.narrow([spend(owner, [2; 32], [3; 32], maximum)])
            .expect("strictly downward grant");
    }
}

#[test]
fn fanout_does_not_merge_distinct_principal_or_program_authority() {
    let owner = ProgramId::new([5; 32]).expect("owner");
    let child = ProgramId::new([6; 32]).expect("child");
    let root = CapabilitySet::new([
        Capability::Transfer402 { asset: [7; 32], to: [8; 32], maximum_amount: 200 },
        spend(owner, [7; 32], [8; 32], 100),
    ]).expect("distinct grants");
    for maximum in [20, 30, 40] {
        let branch = root.narrow([spend(owner, [7; 32], [8; 32], maximum)])
            .expect("branch");
        assert_eq!(branch.narrow([spend(owner, [7; 32], [8; 32], maximum + 1)]),
            Err(AbiError::CapabilityEscalation));
        assert_eq!(branch.narrow([spend(child, [7; 32], [8; 32], maximum)]),
            Err(AbiError::CapabilityEscalation));
        assert_eq!(branch.narrow([Capability::Transfer402 {
            asset: [7; 32], to: [8; 32], maximum_amount: maximum,
        }]), Err(AbiError::CapabilityDenied));
    }
    assert!(root.narrow([spend(owner, [7; 32], [8; 32], 100)]).is_ok());
}

#[test]
fn encoded_program_grants_never_change_legacy_or_accept_unknown_tags() {
    let owner = ProgramId::new([9; 32]).expect("owner");
    let legacy = CapabilitySet::new([Capability::StorageRead,
        Capability::Transfer402 { asset: [10; 32], to: [11; 32], maximum_amount: 5 }])
        .expect("legacy").canonical_encoding();
    assert_eq!(CapabilitySet::decode_canonical(&legacy),
        CapabilitySet::decode_v2_canonical(&legacy));
    let encoded = CapabilitySet::new([spend(owner, [10; 32], [11; 32], 5)])
        .expect("program grant").canonical_encoding();
    assert_eq!(CapabilitySet::decode_canonical(&encoded), Err(AbiError::InvalidEncoding));
    for tag in [0, 11, 255] {
        let mut unknown = encoded.clone(); unknown[2] = tag;
        assert_eq!(CapabilitySet::decode_v2_canonical(&unknown), Err(AbiError::InvalidEncoding));
    }
    for end in 0..encoded.len() {
        assert!(CapabilitySet::decode_v2_canonical(&encoded[..end]).is_err());
    }
    let mut trailing = encoded; trailing.push(0);
    assert_eq!(CapabilitySet::decode_v2_canonical(&trailing), Err(AbiError::InvalidEncoding));
}

fn actual_guest(variant: u8) -> (Result<layerx_programs_runtime::V2AuthorizedExecutionRecord, ExecutionError>, Storage) {
    let directory = std::path::PathBuf::from(std::env::var("PAXEER_X_SPEND_GUESTS")
        .expect("source-bound native guest producer is required"));
    let mut owner_bytes = [0; 32]; owner_bytes[0] = 0x61;
    let mut child_bytes = [0; 32]; child_bytes[0] = 0x62;
    let mut descendant_bytes = [0; 32]; descendant_bytes[0] = 0x63;
    let owner = ProgramId::new(owner_bytes).expect("owner");
    let child = ProgramId::new(child_bytes).expect("child");
    let descendant = ProgramId::new(descendant_bytes).expect("descendant");
    let mut asset = [0; 32]; asset[0] = 9;
    let payee: [u8; 32] = std::fs::read(directory.join("payee.bin"))
        .expect("actual native payee").try_into().expect("payee width");
    let seed = b"composition/vault";
    let grants = CapabilitySet::new([Capability::Call { program: child }, Capability::Call { program: descendant }, Capability::ProgramSpend {
        owner_program: owner, seed: seed.to_vec(),
        source_account: derive_program_account(owner, seed).expect("source").bytes(),
        asset, to: payee, maximum_amount: 20,
    }]).expect("grants");
    let engine = WasmEngine::declared().expect("real engine");
    let module = engine.validate_candidate_v2(&std::fs::read(directory.join(
        format!("case{variant}.owner.wasm"))).expect("owner guest")).expect("owner module");
    let callee = engine.validate_candidate_v2(&std::fs::read(directory.join(
        format!("case{variant}.child.wasm"))).expect("child guest")).expect("child module");
    let mut catalog = ProgramCatalog::new();
    assert!(catalog.insert(child, callee).is_none());
    let descendant_module = engine.validate_candidate_v2(&std::fs::read(directory.join(
        format!("case{variant}.descendant.wasm"))).expect("descendant guest")).expect("descendant module");
    assert!(catalog.insert(descendant, descendant_module).is_none());
    let mut storage = Storage::default();
    let result = Executor::declared().execute_authorized_candidate(&mut storage,
        AuthorizedExecutionRequest { module: &module, program: owner,
            authorization: AuthorizationContext::new(PrincipalId::new([3; 32]).expect("principal"), grants),
            receipts: &layerx_programs_runtime::abi::UnavailableReceiptOracle,
            entrypoint: CALL_ENTRY_EXPORT, calldata: &[],
            composition: CompositionContext::catalog(catalog, CompositionRules::declared()),
            response_capacity: 0,
        });
    (result, storage)
}

#[test]
fn actual_guest_narrowing_preserves_owner_leg_and_repeated_visits() {
    for variant in [0, 4] {
        let (result, storage) = actual_guest(variant);
        let record = result.expect("real composed guest succeeds");
        let layerx_programs_runtime::V2ActivityOutcome::Success { effects, .. } = record.outcome() else {
            panic!("actual guest did not succeed");
        };
        assert_eq!(effects.transfers.len(), 1);
        assert_eq!(effects.transfers[0].amount, 7);
        assert_eq!(effects.calls.len(), if variant == 4 { 8 } else { 1 });
        assert_eq!(storage, Storage::default());
    }
}

#[test]
fn actual_guest_escalation_rolls_back_the_preceding_owner_leg() {
    for variant in [1, 2, 3, 5, 6, 7] {
        let (result, storage) = actual_guest(variant);
        assert_eq!(result, Err(ExecutionError::Composition(
            CompositionRefusal::Authority(AbiError::CapabilityEscalation))));
        assert_eq!(storage, Storage::default());
    }
}

#[test]
fn actual_native_failure_terminal_retains_class_reason_and_rejecting_frame() {
    use layerx_programs_runtime::terminal::{decode_terminal_payload, TerminalDetail,
        ExecutionTerminal, CandidateTerminalOutcome};
    let directory = std::path::PathBuf::from(std::env::var("PAXEER_X_SPEND_RESULTS")
        .expect("actual native signed receipt terminal evidence is required"));
    for variant in [1, 2, 3, 5, 6, 7] {
        let encoded = std::fs::read(directory.join(format!("case{variant}.terminal.bin")))
            .expect("actual native terminal");
        let decoded = decode_terminal_payload(2, 2, &encoded).expect("canonical failure terminal");
        let TerminalDetail::Execution(ExecutionTerminal::CandidateV4 {
            outcome: CandidateTerminalOutcome::Failure(failure), ..
        }) = decoded.detail else { panic!("actual native terminal has the wrong outcome"); };
        let mut frame = [0; 32]; frame[0] = if variant == 5 { 0x62 } else { 0x61 };
        assert_eq!(failure.program(), ProgramId::new(frame).expect("rejecting frame"));
        assert_eq!(failure.class(), layerx_programs_runtime::RefusalClass::Unauthorized);
        assert_eq!(failure.reason().bytes(), b"LXP/programs/authority-refusal/v1\0\x05");
    }
}
