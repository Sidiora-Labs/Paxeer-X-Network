use crate::{
    abi::UnavailableReceiptOracle, derive_program_account, AuthorizationContext,
    AuthorizedExecutionRequest, CandidateAuthorizedExecutionRecord, Capability, CapabilitySet,
    CompositionContext, ExecutionContext, Executor, PrincipalId, ProgramId, Storage,
    StorageNamespace, TransferSource, WasmEngine,
};

fn program() -> ProgramId {
    ProgramId::new([0x55; 32]).unwrap_or_else(|e| panic!("{e}"))
}
fn source(seed: &[u8]) -> [u8; 32] {
    derive_program_account(program(), seed)
        .unwrap_or_else(|e| panic!("{e}"))
        .bytes()
}
fn seed(bytes: &mut Vec<u8>, value: &[u8]) {
    bytes.extend(
        u16::try_from(value.len())
            .unwrap_or_else(|e| panic!("{e}"))
            .to_be_bytes(),
    );
    bytes.extend(value);
}
fn offer() -> Vec<u8> {
    let mut bytes = vec![1, 1];
    for value in [[1; 32], [2; 32], [3; 32], [9; 32], source(b"offer/stake")] {
        bytes.extend(value);
    }
    seed(&mut bytes, b"offer/stake");
    bytes.extend(500_u128.to_be_bytes());
    bytes.extend(4_u128.to_be_bytes());
    for value in [100_u64, 2, 20, 50] {
        bytes.extend(value.to_be_bytes());
    }
    bytes.push(3);
    bytes
}
fn lease(amount: u128) -> Vec<u8> {
    let mut bytes = vec![1, 2];
    for value in [[5; 32], [1; 32], [4; 32], [4; 32], source(b"lease/escrow")] {
        bytes.extend(value);
    }
    seed(&mut bytes, b"lease/escrow");
    bytes.extend(10_u64.to_be_bytes());
    bytes.extend(amount.to_be_bytes());
    bytes.extend(20_u64.to_be_bytes());
    bytes
}
fn operation(operation: u8, id: [u8; 32]) -> Vec<u8> {
    let mut bytes = vec![1, operation];
    bytes.extend(id);
    bytes
}
fn grants() -> Vec<Capability> {
    vec![
        Capability::SharedStorageRead,
        Capability::SharedStorageWrite,
        Capability::EmitEvent,
    ]
}
fn fund(seed: &[u8], amount: u128) -> Capability {
    Capability::Transfer402 {
        asset: [9; 32],
        to: source(seed),
        maximum_amount: amount,
    }
}
fn spend(seed: &[u8], to: [u8; 32], amount: u128) -> Capability {
    Capability::ProgramSpend {
        owner_program: program(),
        seed: seed.to_vec(),
        source_account: source(seed),
        asset: [9; 32],
        to,
        maximum_amount: amount,
    }
}
fn execute(
    storage: &mut Storage,
    actor: [u8; 32],
    height: u64,
    input: &[u8],
    extra: Vec<Capability>,
) -> CandidateAuthorizedExecutionRecord {
    let mut authority = grants();
    authority.extend(extra);
    execute_with_authority(storage, actor, height, input, authority)
}

fn execute_with_authority(
    storage: &mut Storage,
    actor: [u8; 32],
    height: u64,
    input: &[u8],
    authority: Vec<Capability>,
) -> CandidateAuthorizedExecutionRecord {
    let path = std::env::var("LAYERX_MARKET_WASM")
        .unwrap_or_else(|e| panic!("actual compiled market: {e}"));
    let wasm = std::fs::read(path).unwrap_or_else(|e| panic!("compiled market: {e}"));
    let module = WasmEngine::declared()
        .unwrap_or_else(|e| panic!("engine: {e}"))
        .validate_v2(&wasm)
        .unwrap_or_else(|e| panic!("market validation: {e}"));
    Executor::declared()
        .for_abi(2)
        .execute_authorized_v2_with_budget(
            storage,
            AuthorizedExecutionRequest {
                module: &module,
                program: program(),
                authorization: AuthorizationContext::new(
                    PrincipalId::new(actor).unwrap_or_else(|e| panic!("{e}")),
                    CapabilitySet::new(authority).unwrap_or_else(|e| panic!("{e}")),
                ),
                receipts: &UnavailableReceiptOracle,
                entrypoint: "layerx_call",
                calldata: input,
                composition: CompositionContext::isolated(),
                response_capacity: 0,
            },
            crate::ResourceBudget::declared(),
            None,
            Some(
                ExecutionContext::authenticated(height, height, 1, 2, 1)
                    .unwrap_or_else(|e| panic!("{e:?}")),
            ),
            crate::AccessDeclaration::absent(),
            None,
        )
        .unwrap_or_else(|e| panic!("actual market execution: {e}"))
}
fn row(storage: &Storage, prefix: &[u8], id: [u8; 32]) -> Vec<u8> {
    let mut key = prefix.to_vec();
    key.extend(id);
    storage
        .protocol_state_value(StorageNamespace::shared(program()), &key)
        .unwrap_or_else(|e| panic!("public state: {e}"))
        .unwrap_or_else(|| panic!("missing public state"))
}
fn initialized() -> Storage {
    let mut storage = Storage::new();
    let registered = execute(
        &mut storage,
        [2; 32],
        1,
        &offer(),
        vec![fund(b"offer/stake", 500)],
    );
    assert!(
        registered.response().is_some(),
        "registered outcome: {registered:?}"
    );
    let effects = registered
        .effects()
        .unwrap_or_else(|| panic!("offer effects"));
    assert_eq!(effects.transfers.len(), 1);
    assert_eq!(effects.transfers[0].amount, 500);
    assert!(matches!(
        effects.transfers[0].source(),
        TransferSource::ProgramFunding { .. }
    ));
    assert_eq!(effects.transfers[0].to, source(b"offer/stake"));
    assert_eq!(
        &row(&storage, b"lx.market.offer/", [1; 32])[..3],
        &[1, 1, 3]
    );
    storage
}
#[test]
fn compiled_market_funding_expiry_public_settlement_and_close() {
    let mut storage = initialized();
    let opened = execute(
        &mut storage,
        [4; 32],
        2,
        &lease(40),
        vec![fund(b"lease/escrow", 40)],
    );
    assert!(opened.response().is_some(), "opened outcome: {opened:?}");
    let effects = opened.effects().unwrap_or_else(|| panic!("lease effects"));
    assert_eq!(effects.transfers.len(), 1);
    assert_eq!(effects.transfers[0].amount, 40);
    assert_eq!(effects.transfers[0].to, source(b"lease/escrow"));
    assert!(matches!(
        effects.transfers[0].source(),
        TransferSource::ProgramFunding { .. }
    ));
    assert_eq!(
        &row(&storage, b"lx.market.lease/", [5; 32])[..3],
        &[1, 1, 3]
    );
    let before = storage.clone();
    let premature = execute(
        &mut storage,
        [4; 32],
        19,
        &operation(4, [5; 32]),
        vec![spend(b"lease/escrow", [4; 32], 40)],
    );
    assert!(premature.effects().is_none());
    assert_eq!(storage, before);
    let expired = execute(
        &mut storage,
        [4; 32],
        20,
        &operation(4, [5; 32]),
        vec![spend(b"lease/escrow", [4; 32], 40)],
    );
    assert!(expired.response().is_some(), "expired outcome: {expired:?}");
    let effects = expired
        .effects()
        .unwrap_or_else(|| panic!("refund effects"));
    assert_eq!(effects.transfers.len(), 1);
    assert_eq!(effects.transfers[0].amount, 40);
    assert_eq!(effects.transfers[0].to, [4; 32]);
    assert_eq!(effects.transfers[0].asset, [9; 32]);
    let TransferSource::Program(authority) = effects.transfers[0].source() else {
        panic!("owner refund authority");
    };
    assert_eq!(authority.owner_program(), program());
    assert_eq!(authority.seed(), b"lease/escrow");
    assert_eq!(authority.source_account(), source(b"lease/escrow"));
    let settlement = row(&storage, b"lx.market.settlement/", [5; 32]);
    assert_eq!(settlement.len(), 282);
    assert_eq!(&settlement[..2], &[1, 3]);
    assert_eq!(&settlement[2..34], &[5; 32]);
    assert_eq!(&settlement[34..66], &[1; 32]);
    assert_eq!(&settlement[66..98], &[0; 32]);
    assert_eq!(&settlement[98..130], &source(b"lease/escrow"));
    assert_eq!(&settlement[226..242], &40_u128.to_be_bytes());
    assert_eq!(&settlement[242..258], &0_u128.to_be_bytes());
    assert_eq!(&settlement[258..274], &40_u128.to_be_bytes());
    assert_eq!(&settlement[274..282], &20_u64.to_be_bytes());
    let before = storage.clone();
    let replay = execute(
        &mut storage,
        [4; 32],
        21,
        &operation(4, [5; 32]),
        vec![spend(b"lease/escrow", [4; 32], 40)],
    );
    assert!(replay.effects().is_none());
    assert_eq!(storage, before);
    let closed = execute(
        &mut storage,
        [2; 32],
        22,
        &operation(5, [1; 32]),
        vec![spend(b"offer/stake", [2; 32], 500)],
    );
    assert!(closed.response().is_some(), "closed outcome: {closed:?}");
    let effects = closed.effects().unwrap_or_else(|| panic!("stake return"));
    assert_eq!(effects.transfers.len(), 1);
    assert_eq!(effects.transfers[0].amount, 500);
    assert_eq!(effects.transfers[0].to, [2; 32]);
    assert_eq!(
        &row(&storage, b"lx.market.offer/", [1; 32])[..3],
        &[1, 2, 3]
    );
}
#[test]
fn compiled_market_refuses_unfunded_and_unauthorized_payments_atomically() {
    let mut storage = Storage::new();
    let before = storage.clone();
    let refused = execute_with_authority(
        &mut storage,
        [2; 32],
        1,
        &offer(),
        vec![
            Capability::SharedStorageRead,
            Capability::SharedStorageWrite,
            fund(b"offer/stake", 500),
        ],
    );
    assert!(refused.effects().is_none());
    assert!(refused.response().is_none());
    assert_eq!(storage, before);
    let mut storage = initialized();
    let before = storage.clone();
    for (amount, funding) in [
        (0, vec![]),
        (39, vec![fund(b"lease/escrow", 39)]),
        (40, vec![]),
        (40, vec![fund(b"lease/escrow", 39)]),
        (41, vec![fund(b"lease/escrow", 41)]),
    ] {
        let refused = execute(&mut storage, [4; 32], 2, &lease(amount), funding);
        assert!(refused.effects().is_none());
        assert_eq!(storage, before);
    }
    let opened = execute(
        &mut storage,
        [4; 32],
        2,
        &lease(40),
        vec![fund(b"lease/escrow", 40)],
    );
    assert!(opened.response().is_some());
    let before = storage.clone();
    for authority in [
        vec![],
        vec![spend(b"lease/escrow", [4; 32], 39)],
        vec![spend(b"lease/escrow", [3; 32], 40)],
        vec![spend(b"offer/stake", [4; 32], 40)],
    ] {
        let refused = execute(&mut storage, [4; 32], 20, &operation(4, [5; 32]), authority);
        assert!(refused.effects().is_none());
        assert_eq!(storage, before);
    }
    let locked = execute(
        &mut storage,
        [2; 32],
        3,
        &operation(5, [1; 32]),
        vec![spend(b"offer/stake", [2; 32], 500)],
    );
    assert!(locked.effects().is_none());
    assert_eq!(storage, before);
    for input in [
        vec![],
        vec![1],
        vec![0, 1],
        vec![1, 0],
        vec![1, 3],
        vec![1, 15],
        vec![1, 255],
    ] {
        let refused = execute(&mut storage, [4; 32], 20, &input, vec![]);
        assert!(refused.effects().is_none());
        assert_eq!(storage, before);
    }
}
