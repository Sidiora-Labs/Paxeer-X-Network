use crate::{
    abi::UnavailableReceiptOracle, derive_program_account, AuthorizationContext,
    AuthorizedExecutionRequest, CandidateAuthorizedExecutionRecord, Capability, CapabilitySet,
    CompositionContext, CompositionRules, ExecutionContext, Executor, PrincipalId, ProgramCatalog,
    ProgramId, Storage, StorageNamespace, TransferSource, WasmEngine,
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
    offer_with(500, 4, 50)
}
fn offer_with(stake: u128, price: u128, expires_at: u64) -> Vec<u8> {
    let mut bytes = vec![1, 1];
    for value in [[1; 32], [2; 32], [3; 32], [9; 32], source(b"offer/stake")] {
        bytes.extend(value);
    }
    seed(&mut bytes, b"offer/stake");
    bytes.extend(stake.to_be_bytes());
    bytes.extend(price.to_be_bytes());
    for value in [100_u64, 2, 20, expires_at] {
        bytes.extend(value.to_be_bytes());
    }
    bytes.push(3);
    bytes
}
fn lease(amount: u128) -> Vec<u8> {
    lease_with([5; 32], b"lease/escrow", amount, 20)
}
fn lease_with(id: [u8; 32], escrow: &[u8], amount: u128, expires_at: u64) -> Vec<u8> {
    let mut bytes = vec![1, 2];
    for value in [id, [1; 32], [4; 32], [4; 32], source(escrow)] {
        bytes.extend(value);
    }
    seed(&mut bytes, escrow);
    bytes.extend(10_u64.to_be_bytes());
    bytes.extend(amount.to_be_bytes());
    bytes.extend(expires_at.to_be_bytes());
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
        .validate_v5(&wasm)
        .unwrap_or_else(|e| panic!("market validation: {e}"));
    let mut catalog = ProgramCatalog::new();
    catalog.insert(sandbox_program(), sandbox_module());
    Executor::declared()
        .for_abi(5)
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
                composition: CompositionContext::catalog(catalog, CompositionRules::declared()),
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

use crate::execute::{
    encode_market_step_evidence, instantiate_market_sandbox_untrusted, market_sandbox_input_digest,
    MarketSandboxRequest, MarketStepProof,
};
use crate::portable_replay::{replay_leaf_hash, replay_node_hash, PortableBoundary};
use crate::replay::{
    market_sandbox_baseline_root, market_sandbox_namespace, MarketSandboxReplayAuthority,
};
use crate::test_support::{
    code_section, export_section, func_body, function_section, module, type_section, OP_END,
    OP_I32_ADD, OP_I32_CONST, TYPE_I32,
};
use crate::{
    ArbitrationStepCommitment, FeeSchedule, ProgramReplayProfile, ResourceBudget, ValidatedModule,
    RUNTIME_VERSION,
};
use layerx_program_sdk::arbiter::{
    MarketBillingCommitment, MarketSandboxProfile, ProfileLimits, MARKET_SANDBOX_BILLING_CAPACITY,
    MARKET_SANDBOX_PROFILE_CAPACITY,
};
use sha2::{Digest, Sha256};

const PRICE: u128 = 100_000_000;
const FUNDED: u128 = 1_000_000_000;
const BOND: u128 = 3_000_000_000;
const PAYABLE: u128 = 500_000_000;
const CHALLENGE_STAKE: u128 = 250_000_000;
const MAX_BYTES: u32 = 1_048_576;
const PROVIDER: [u8; 32] = [2; 32];
const PAYOUT: [u8; 32] = [3; 32];
const TENANT: [u8; 32] = [4; 32];
const CHALLENGER: [u8; 32] = [13; 32];
const KEEPER: [u8; 32] = [0x66; 32];
const LEASE_A: [u8; 32] = [5; 32];
const LEASE_B: [u8; 32] = [6; 32];
const CHALLENGED_AT: u64 = 7;
const WINDOW: u64 = 10;
const USAGE: [u64; 6] = [5, 1, 0, 0, 0, 0];

fn sandbox_program() -> ProgramId {
    ProgramId::new([0x77; 32]).unwrap_or_else(|e| panic!("{e}"))
}
fn integer_guest() -> Vec<u8> {
    let mut body = vec![OP_I32_CONST, 1];
    for value in 2..=6 {
        body.extend([OP_I32_CONST, value, OP_I32_ADD]);
    }
    body.push(OP_END);
    module(&[
        type_section(&[(&[], &[TYPE_I32])]),
        function_section(&[0]),
        export_section(&[("compute", 0)]),
        code_section(&[func_body(&[], &body)]),
    ])
}
fn sandbox_module() -> ValidatedModule {
    WasmEngine::declared()
        .unwrap_or_else(|e| panic!("engine: {e}"))
        .validate_versioned(2, &integer_guest())
        .unwrap_or_else(|e| panic!("sandbox validation: {e}"))
}
fn budget() -> ResourceBudget {
    ResourceBudget::new_complete(100_000, 65_536, 1024, 1024, 1, 64, 0)
}
fn escrow(lease: [u8; 32]) -> &'static [u8] {
    if lease == LEASE_A {
        b"lease/escrow"
    } else {
        b"lease/escrow-b"
    }
}
fn claim_id(lease: [u8; 32]) -> [u8; 32] {
    [lease[0] + 0x60; 32]
}

/// A Merkle commitment over boundary leaves, built the way the runtime commits a trace.
struct Committed {
    leaves: Vec<Vec<u8>>,
    levels: Vec<Vec<[u8; 32]>>,
}

impl Committed {
    fn new(leaves: Vec<Vec<u8>>) -> Self {
        let mut levels = vec![leaves
            .iter()
            .enumerate()
            .map(|(index, leaf)| {
                replay_leaf_hash(u32::try_from(index).unwrap_or_else(|e| panic!("{e}")), leaf)
                    .unwrap_or_else(|e| panic!("leaf hash: {e:?}"))
            })
            .collect::<Vec<_>>()];
        while levels[levels.len() - 1].len() > 1 {
            let next = levels[levels.len() - 1]
                .chunks(2)
                .map(|pair| replay_node_hash(pair[0], *pair.get(1).unwrap_or(&pair[0])))
                .collect();
            levels.push(next);
        }
        Self { leaves, levels }
    }
    fn root(&self) -> [u8; 32] {
        self.levels[self.levels.len() - 1][0]
    }
    fn count(&self) -> u32 {
        u32::try_from(self.leaves.len()).unwrap_or_else(|e| panic!("{e}"))
    }
    fn siblings(&self, index: u32) -> Vec<[u8; 32]> {
        let mut position = index as usize;
        let mut siblings = Vec::new();
        for level in &self.levels[..self.levels.len() - 1] {
            siblings.push(*level.get(position ^ 1).unwrap_or(&level[position]));
            position /= 2;
        }
        siblings
    }
    fn evidence(&self, baseline: &[u8], indices: &[u32]) -> Vec<u8> {
        let siblings: Vec<Vec<[u8; 32]>> =
            indices.iter().map(|index| self.siblings(*index)).collect();
        let proofs: Vec<MarketStepProof<'_>> = indices
            .iter()
            .zip(&siblings)
            .map(|(index, siblings)| MarketStepProof {
                index: *index,
                leaf: &self.leaves[*index as usize],
                siblings,
            })
            .collect();
        encode_market_step_evidence(baseline, &proofs)
            .unwrap_or_else(|e| panic!("step evidence: {e:?}"))
    }
    fn commitment(&self, index: usize) -> [u8; 32] {
        let boundary = PortableBoundary::decode_untrusted(&self.leaves[index], MAX_BYTES as usize)
            .unwrap_or_else(|e| panic!("committed boundary: {e:?}"));
        ArbitrationStepCommitment::from_state(&boundary.arbitration)
            .unwrap_or_else(|e| panic!("boundary commitment: {e:?}"))
            .digest
    }
    fn last(&self) -> [u8; 32] {
        self.commitment(self.leaves.len() - 1)
    }
}

/// The genuine sandbox execution for one lease, and the trace its provider commits: the captured
/// trace, or one whose host state diverges from boundary `lie_from` onwards.
struct Execution {
    truth: Committed,
    committed: Committed,
    initial: [u8; 32],
    authority: MarketSandboxReplayAuthority,
    baseline: Vec<u8>,
}

impl Execution {
    fn capture(lease: [u8; 32], lie_from: Option<usize>) -> Self {
        let module = sandbox_module();
        let baseline = Storage::new();
        let fees = FeeSchedule::declared();
        let authority = MarketSandboxReplayAuthority {
            profile_binding: [0x31; 32],
            namespace: market_sandbox_namespace(sandbox_program(), lease)
                .unwrap_or_else(|e| panic!("namespace: {e:?}")),
            lease_id: lease,
            namespace_limit: 1024,
            program: sandbox_program(),
            tenant: PrincipalId::new(TENANT).unwrap_or_else(|e| panic!("{e}")),
            payment_account: TENANT,
            code_hash: module.code_hash(),
            input_digest: market_sandbox_input_digest("compute", &[])
                .unwrap_or_else(|e| panic!("input digest: {e:?}")),
            runtime_version: RUNTIME_VERSION,
            abi_version: 2,
            fee_schedule_version: fees.version(),
            metering_schedule_version: module.metering_schedule_version(),
            budget: budget(),
            fees,
            fee_budget: FUNDED,
            baseline_state_root: market_sandbox_baseline_root(&baseline)
                .unwrap_or_else(|e| panic!("baseline root: {e:?}")),
            baseline_storage: baseline.clone(),
        };
        let mut instance = instantiate_market_sandbox_untrusted(&module, &authority)
            .unwrap_or_else(|e| panic!("sandbox instance: {e:?}"));
        let execution = instance
            .call_market_sandbox_untrusted(MarketSandboxRequest {
                module: &module,
                entrypoint: "compute",
                args: &[],
                authority: &authority,
                replay_profile: ProgramReplayProfile::new(128, MAX_BYTES)
                    .unwrap_or_else(|e| panic!("replay profile: {e:?}")),
            })
            .unwrap_or_else(|e| panic!("captured sandbox execution: {e:?}"));
        assert!(execution.terminal_fault.is_none());
        let truth = Committed::new(execution.boundary_leaves.clone());
        assert_eq!(truth.root(), execution.record.boundary_root());
        assert_eq!(truth.count(), execution.record.boundary_count());
        assert!(truth.count() >= 4, "trace too short to bisect");
        assert_eq!(truth.commitment(0), execution.initial_commitment.digest);
        assert_eq!(truth.last(), execution.final_commitment.digest);
        let committed = Committed::new(
            execution
                .boundary_leaves
                .iter()
                .enumerate()
                .map(|(index, leaf)| {
                    if lie_from.is_some_and(|from| index >= from) {
                        let mut boundary =
                            PortableBoundary::decode_untrusted(leaf, MAX_BYTES as usize)
                                .unwrap_or_else(|e| panic!("boundary: {e:?}"));
                        boundary.arbitration.host_state_root[0] ^= 1;
                        boundary
                            .reencode_untrusted(MAX_BYTES as usize)
                            .unwrap_or_else(|e| panic!("divergent boundary: {e:?}"))
                    } else {
                        leaf.clone()
                    }
                })
                .collect(),
        );
        Self {
            truth,
            committed,
            initial: execution.initial_commitment.digest,
            baseline: baseline
                .replay_state_bytes(MAX_BYTES as usize)
                .unwrap_or_else(|e| panic!("baseline bytes: {e:?}")),
            authority,
        }
    }

    fn profile(&self, lease: [u8; 32], attested: [u8; 32]) -> MarketSandboxProfile {
        let budget = budget();
        let mut entrypoint_bytes = [0; 64];
        entrypoint_bytes[..7].copy_from_slice(b"compute");
        MarketSandboxProfile {
            network_id: 1,
            market_program: program().bytes(),
            sandbox_program: sandbox_program().bytes(),
            offer_id: [1; 32],
            lease_id: lease,
            claim_id: claim_id(lease),
            provider: PROVIDER,
            tenant: TENANT,
            code_hash: self.authority.code_hash,
            input_digest: self.authority.input_digest,
            attested_input_commitment: attested,
            namespace: self.authority.namespace,
            baseline_state_root: self.authority.baseline_state_root,
            initial_execution_state_root: self.initial,
            entrypoint_bytes,
            entrypoint_length: 7,
            runtime_version: RUNTIME_VERSION,
            abi_version: 2,
            fee_schedule_version: self.authority.fee_schedule_version,
            metering_schedule_version: self.authority.metering_schedule_version,
            limits: ProfileLimits {
                cpu_fuel: budget.cpu_fuel(),
                memory_bytes: budget.memory_bytes(),
                storage_read_bytes: budget.storage_read_bytes(),
                storage_write_bytes: budget.storage_write_bytes(),
                output_values: u64::from(budget.output_values()),
                output_bytes: budget.output_bytes(),
                table_elements: u64::from(budget.table_elements()),
                namespace_bytes: self.authority.namespace_limit,
            },
            fee_budget: FUNDED,
            interval_start: 5,
            interval_end: 6,
            response_deadline: 6 + WINDOW,
            maximum_boundaries: 128,
            maximum_bytes: MAX_BYTES,
        }
    }
}

fn input(code: u8, lease: [u8; 32], tail: &[u8]) -> Vec<u8> {
    let mut bytes = operation(code, lease);
    bytes.extend(tail);
    bytes
}
fn accepted(
    storage: &mut Storage,
    actor: [u8; 32],
    height: u64,
    call: &[u8],
    extra: Vec<Capability>,
) -> CandidateAuthorizedExecutionRecord {
    let record = execute(storage, actor, height, call, extra);
    assert!(
        record.response().is_some(),
        "operation {} at {height} refused: {record:?}",
        call[1]
    );
    record
}
fn refused(
    storage: &mut Storage,
    actor: [u8; 32],
    height: u64,
    call: &[u8],
    extra: Vec<Capability>,
) {
    let before = storage.clone();
    let record = execute(storage, actor, height, call, extra);
    assert!(
        record.effects().is_none() && record.response().is_none(),
        "operation {} at {height} was not refused",
        call[1]
    );
    assert!(
        *storage == before,
        "refused operation {} changed state",
        call[1]
    );
}
fn settlement_spends(lease: [u8; 32]) -> Vec<Capability> {
    vec![
        spend(escrow(lease), PAYOUT, PAYABLE),
        spend(escrow(lease), TENANT, FUNDED),
        spend(b"challenge/a", PAYOUT, CHALLENGE_STAKE),
        spend(b"challenge/a", CHALLENGER, CHALLENGE_STAKE),
        spend(b"offer/stake", CHALLENGER, FUNDED),
    ]
}
fn amount_at(bytes: &[u8], offset: usize) -> u128 {
    u128::from_be_bytes(
        bytes[offset..offset + 16]
            .try_into()
            .unwrap_or_else(|e| panic!("{e}")),
    )
}
fn u32_at(bytes: &[u8], offset: usize) -> u32 {
    u32::from_be_bytes(
        bytes[offset..offset + 4]
            .try_into()
            .unwrap_or_else(|e| panic!("{e}")),
    )
}
/// Stake row amounts: posted, locked, slashed.
fn stake(storage: &Storage) -> (u128, u128, u128) {
    let stake = row(storage, b"lx.market.stake/", [1; 32]);
    (
        amount_at(&stake, 129),
        amount_at(&stake, 145),
        amount_at(&stake, 161),
    )
}
fn available_capacity(storage: &Storage) -> u64 {
    let offer = row(storage, b"lx.market.offer/", [1; 32]);
    u64::from_be_bytes(offer[216..224].try_into().unwrap_or_else(|e| panic!("{e}")))
}
fn dispute_row(storage: &Storage, lease: [u8; 32]) -> Vec<u8> {
    row(storage, b"lx.market.dispute/", lease)
}
fn settlement_row(storage: &Storage, lease: [u8; 32]) -> Vec<u8> {
    row(storage, b"lx.market.settlement/", lease)
}
fn moved(record: &CandidateAuthorizedExecutionRecord) -> Vec<([u8; 32], [u8; 32], u128)> {
    record
        .effects()
        .unwrap_or_else(|| panic!("settlement effects"))
        .transfers
        .iter()
        .map(|transfer| {
            assert!(matches!(transfer.source(), TransferSource::Program(_)));
            (transfer.source().account(), transfer.to, transfer.amount)
        })
        .collect()
}

fn bonded() -> Storage {
    let mut storage = Storage::new();
    accepted(
        &mut storage,
        PROVIDER,
        1,
        &offer_with(BOND, PRICE, 1_000),
        vec![fund(b"offer/stake", BOND)],
    );
    storage
}
fn open_lease(storage: &mut Storage, lease: [u8; 32], height: u64, expires_at: u64) {
    accepted(
        storage,
        TENANT,
        height,
        &lease_with(lease, escrow(lease), FUNDED, expires_at),
        vec![fund(escrow(lease), FUNDED)],
    );
}
fn seal_inputs(storage: &mut Storage, lease: [u8; 32]) -> [u8; 32] {
    let mut attesters = 1_u64.to_be_bytes().to_vec();
    attesters.extend([1, 10]);
    attesters.extend(b"attester-a");
    attesters.extend([0x21; 32]);
    accepted(storage, TENANT, 3, &input(6, lease, &attesters), vec![]);
    accepted(storage, TENANT, 3, &operation(8, lease), vec![]);
    let policy = row(storage, b"lx.market.attesters/", lease);
    assert_eq!(
        policy[218], 1,
        "sealed policy carries its settlement commitment"
    );
    policy[219..251]
        .try_into()
        .unwrap_or_else(|e| panic!("{e}"))
}
fn commit_sandbox_claim(storage: &mut Storage, lease: [u8; 32], execution: &Execution) -> [u8; 32] {
    let attested = seal_inputs(storage, lease);
    let profile = execution.profile(lease, attested);
    let mut profile_bytes = [0; MARKET_SANDBOX_PROFILE_CAPACITY];
    let length = profile
        .encode(&mut profile_bytes)
        .unwrap_or_else(|e| panic!("profile: {e:?}"));
    let mut authorize = vec![1, 13];
    authorize.extend(&profile_bytes[..length]);
    accepted(storage, TENANT, 4, &authorize, vec![]);
    let committed = &execution.committed;
    let billing = MarketBillingCommitment {
        profile_digest: Sha256::digest(&profile_bytes[..length]).into(),
        provider_trace_root: committed.root(),
        final_execution_state_root: committed.last(),
        output_digest: [0x44; 32],
        usage: USAGE,
        payable: PAYABLE,
        challenger_stake: CHALLENGE_STAKE,
        challenge_window_batches: WINDOW,
        boundary_count: committed.count(),
    };
    let mut billing_bytes = [0; MARKET_SANDBOX_BILLING_CAPACITY];
    let length = billing
        .encode(&mut billing_bytes)
        .unwrap_or_else(|e| panic!("billing: {e:?}"));
    accepted(
        storage,
        PROVIDER,
        6,
        &input(14, lease, &billing_bytes[..length]),
        vec![],
    );
    attested
}
/// A challenge contradicting the claim's output digest, staked from `challenge/a`.
fn challenge(challenge_id: [u8; 32], attested: [u8; 32], execution_root: [u8; 32]) -> Vec<u8> {
    let mut tail = challenge_id.to_vec();
    tail.extend(source(b"challenge/a"));
    seed(&mut tail, b"challenge/a");
    tail.extend(CHALLENGE_STAKE.to_be_bytes());
    tail.extend(attested);
    tail.extend([0x45; 32]);
    tail.extend(execution_root);
    for value in USAGE {
        tail.extend(value.to_be_bytes());
    }
    tail
}

/// One open dispute over a sandbox claim on lease A, opened by the challenger at
/// `CHALLENGED_AT` against a provider trace that is honest or diverges from `lie_from`.
struct Dispute {
    storage: Storage,
    execution: Execution,
    height: u64,
    moves: u32,
}

impl Dispute {
    fn open(lie_from: Option<usize>) -> Self {
        let mut storage = bonded();
        open_lease(&mut storage, LEASE_A, 2, 400);
        let execution = Execution::capture(LEASE_A, lie_from);
        let attested = commit_sandbox_claim(&mut storage, LEASE_A, &execution);
        let challenged = accepted(
            &mut storage,
            CHALLENGER,
            CHALLENGED_AT,
            &input(
                11,
                LEASE_A,
                &challenge([0x71; 32], attested, execution.committed.last()),
            ),
            vec![fund(b"challenge/a", CHALLENGE_STAKE)],
        );
        let effects = challenged
            .effects()
            .unwrap_or_else(|| panic!("challenge effects"));
        assert_eq!(effects.transfers.len(), 1);
        assert_eq!(effects.transfers[0].amount, CHALLENGE_STAKE);
        assert_eq!(effects.transfers[0].to, source(b"challenge/a"));
        assert_eq!(row(&storage, b"lx.market.claim/", LEASE_A)[1], 2);
        let dispute = dispute_row(&storage, LEASE_A);
        assert_eq!(&dispute[..2], &[1, 1]);
        assert_eq!(u32_at(&dispute, 8), execution.committed.count());
        assert_ne!(&dispute[48..80], &[0x71; 32], "open move absorbed");
        Self {
            storage,
            execution,
            height: CHALLENGED_AT,
            moves: 1,
        }
    }

    /// (state, position, lo, hi, count) of the stored dispute row.
    fn state(&self) -> (u8, u32, u32, u32, u32) {
        let dispute = dispute_row(&self.storage, LEASE_A);
        (
            dispute[1],
            u32_at(&dispute, 2),
            u32_at(&dispute, 12),
            u32_at(&dispute, 16),
            u32_at(&dispute, 8),
        )
    }
    fn deadline(&self) -> u64 {
        let dispute = dispute_row(&self.storage, LEASE_A);
        u64::from_be_bytes(dispute[32..40].try_into().unwrap_or_else(|e| panic!("{e}")))
    }
    fn reveal_input(&self, position: u32) -> Vec<u8> {
        let mut tail = position.to_be_bytes().to_vec();
        tail.extend(
            self.execution
                .committed
                .evidence(&self.execution.baseline, &[position]),
        );
        input(15, LEASE_A, &tail)
    }
    fn reveal(&mut self, position: u32) -> CandidateAuthorizedExecutionRecord {
        self.height += 1;
        self.moves += 1;
        let call = self.reveal_input(position);
        accepted(
            &mut self.storage,
            PROVIDER,
            self.height,
            &call,
            settlement_spends(LEASE_A),
        )
    }
    fn respond(&mut self, agree: bool) {
        self.height += 1;
        self.moves += 1;
        accepted(
            &mut self.storage,
            CHALLENGER,
            self.height,
            &input(16, LEASE_A, &[u8::from(agree)]),
            vec![],
        );
    }
    fn adjudication_evidence(&self) -> Vec<u8> {
        let (state, position, _, hi, count) = self.state();
        assert_eq!(state, 3, "dispute awaits adjudication");
        let indices: &[u32] = if hi == count {
            &[position]
        } else {
            &[position, position + 1]
        };
        self.execution
            .committed
            .evidence(&self.execution.baseline, indices)
    }
    fn resolve(&mut self) -> CandidateAuthorizedExecutionRecord {
        self.height += 1;
        self.moves += 1;
        let evidence = self.adjudication_evidence();
        accepted(
            &mut self.storage,
            KEEPER,
            self.height,
            &input(17, LEASE_A, &evidence),
            settlement_spends(LEASE_A),
        )
    }
    /// Bisects until the interval collapses, with an honest reveal at every provider turn and
    /// the given challenger strategy.
    fn collapse(&mut self, agree: impl Fn(&Execution, u32) -> bool) {
        loop {
            match self.state() {
                (1, position, ..) => {
                    self.reveal(position);
                }
                (2, position, ..) => {
                    let decision = agree(&self.execution, position);
                    self.respond(decision);
                }
                (3, ..) => return,
                (state, ..) => panic!("dispute settled early in state {state}"),
            }
        }
    }
    fn play(
        &mut self,
        agree: impl Fn(&Execution, u32) -> bool,
    ) -> CandidateAuthorizedExecutionRecord {
        self.collapse(agree);
        let settled = self.resolve();
        assert_eq!(self.state().0, 4);
        settled
    }
}

fn honest_challenger(execution: &Execution, position: u32) -> bool {
    execution.truth.leaves[position as usize] == execution.committed.leaves[position as usize]
}
fn bisection_bound(count: u32) -> u32 {
    2 * (32 - (count - 1).leading_zeros()) + 3
}
fn assert_provider_won(
    record: &CandidateAuthorizedExecutionRecord,
    storage: &Storage,
    resolution: u8,
) {
    assert_eq!(
        moved(record),
        vec![
            (source(escrow(LEASE_A)), PAYOUT, PAYABLE),
            (source(escrow(LEASE_A)), TENANT, FUNDED - PAYABLE),
            (source(b"challenge/a"), PAYOUT, CHALLENGE_STAKE),
        ]
    );
    assert_eq!(row(storage, b"lx.market.claim/", LEASE_A)[1], 5);
    let settlement = settlement_row(storage, LEASE_A);
    assert_eq!(&settlement[..2], &[1, 2]);
    assert_eq!(&settlement[2..34], &LEASE_A);
    assert_eq!(&settlement[66..98], &claim_id(LEASE_A));
    assert_eq!(amount_at(&settlement, 242), PAYABLE);
    assert_eq!(amount_at(&settlement, 258), FUNDED - PAYABLE);
    let dispute = dispute_row(storage, LEASE_A);
    assert_eq!((dispute[1], dispute[6], dispute[7]), (4, 1, resolution));
    assert_eq!(stake(storage).2, 0);
}
fn assert_challenger_won(
    record: &CandidateAuthorizedExecutionRecord,
    storage: &Storage,
    resolution: u8,
) {
    assert_eq!(
        moved(record),
        vec![
            (source(escrow(LEASE_A)), TENANT, FUNDED),
            (source(b"challenge/a"), CHALLENGER, CHALLENGE_STAKE),
            (source(b"offer/stake"), CHALLENGER, FUNDED),
        ]
    );
    assert_eq!(row(storage, b"lx.market.claim/", LEASE_A)[1], 4);
    let settlement = settlement_row(storage, LEASE_A);
    assert_eq!(&settlement[..2], &[1, 2]);
    assert_eq!(&settlement[66..98], &claim_id(LEASE_A));
    assert_eq!(amount_at(&settlement, 242), 0);
    assert_eq!(amount_at(&settlement, 258), FUNDED);
    let dispute = dispute_row(storage, LEASE_A);
    assert_eq!((dispute[1], dispute[6], dispute[7]), (4, 2, resolution));
    assert_eq!(stake(storage).2, FUNDED);
}
fn close_offer(storage: &mut Storage, returned: u128) {
    let closed = accepted(
        storage,
        PROVIDER,
        900,
        &operation(5, [1; 32]),
        vec![spend(b"offer/stake", PROVIDER, returned)],
    );
    assert_eq!(
        moved(&closed),
        vec![(source(b"offer/stake"), PROVIDER, returned)]
    );
    assert_eq!(row(storage, b"lx.market.offer/", [1; 32])[1], 2);
    assert_eq!(stake(storage), (0, 0, BOND - returned));
}
fn lying_from_middle() -> usize {
    Execution::capture(LEASE_A, None).truth.leaves.len() / 2
}

#[test]
fn compiled_market_dispute_honest_provider_defeats_a_lying_challenger() {
    let mut dispute = Dispute::open(None);
    assert_eq!(stake(&dispute.storage), (BOND, FUNDED, 0));
    assert_eq!(available_capacity(&dispute.storage), 90);
    let settled = dispute.play(|_, _| false);
    assert_provider_won(&settled, &dispute.storage, 4);
    let total: u128 = moved(&settled).iter().map(|(_, _, amount)| amount).sum();
    assert_eq!(total, FUNDED + CHALLENGE_STAKE);
    assert_eq!(stake(&dispute.storage), (BOND, 0, 0));
    assert_eq!(available_capacity(&dispute.storage), 100);
    assert!(dispute.moves <= bisection_bound(dispute.execution.committed.count()));
    let height = dispute.height + 1;
    let evidence = dispute
        .execution
        .committed
        .evidence(&dispute.execution.baseline, &[0, 1]);
    refused(
        &mut dispute.storage,
        KEEPER,
        height,
        &input(17, LEASE_A, &evidence),
        settlement_spends(LEASE_A),
    );
    refused(
        &mut dispute.storage,
        KEEPER,
        500,
        &input(17, LEASE_A, &[]),
        settlement_spends(LEASE_A),
    );
    refused(
        &mut dispute.storage,
        CHALLENGER,
        height,
        &input(16, LEASE_A, &[0]),
        vec![],
    );
    refused(
        &mut dispute.storage,
        PROVIDER,
        500,
        &operation(12, LEASE_A),
        settlement_spends(LEASE_A),
    );
    close_offer(&mut dispute.storage, BOND);
}

#[test]
fn compiled_market_dispute_honest_challenger_slashes_a_lying_provider() {
    let from = lying_from_middle();
    let mut dispute = Dispute::open(Some(from));
    let settled = dispute.play(honest_challenger);
    let index = u32::try_from(from).unwrap_or_else(|e| panic!("{e}"));
    let (_, position, lo, hi, _) = dispute.state();
    assert_eq!((position, lo, hi), (index - 1, index - 1, index));
    assert_challenger_won(&settled, &dispute.storage, 4);
    assert_eq!(stake(&dispute.storage), (BOND - FUNDED, 0, FUNDED));
    assert_eq!(available_capacity(&dispute.storage), 100);
    assert!(dispute.moves <= bisection_bound(dispute.execution.committed.count()));
    close_offer(&mut dispute.storage, BOND - FUNDED);

    let mut initial = Dispute::open(Some(0));
    let settled = initial.reveal(0);
    assert_challenger_won(&settled, &initial.storage, 3);
    assert_eq!(stake(&initial.storage), (BOND - FUNDED, 0, FUNDED));
}

#[test]
fn compiled_market_dispute_timeouts_follow_the_bisection_rules() {
    let mut absent_provider = Dispute::open(None);
    let deadline = absent_provider.deadline();
    assert_eq!(deadline, CHALLENGED_AT + WINDOW);
    refused(
        &mut absent_provider.storage,
        KEEPER,
        deadline,
        &input(17, LEASE_A, &[]),
        settlement_spends(LEASE_A),
    );
    let late = absent_provider.reveal_input(0);
    refused(
        &mut absent_provider.storage,
        PROVIDER,
        deadline + 1,
        &late,
        settlement_spends(LEASE_A),
    );
    let settled = accepted(
        &mut absent_provider.storage,
        KEEPER,
        deadline + 1,
        &input(17, LEASE_A, &[]),
        settlement_spends(LEASE_A),
    );
    assert_challenger_won(&settled, &absent_provider.storage, 1);

    let mut absent_challenger = Dispute::open(None);
    absent_challenger.reveal(0);
    let (_, position, ..) = absent_challenger.state();
    absent_challenger.reveal(position);
    assert_eq!(absent_challenger.state().0, 2);
    let deadline = absent_challenger.deadline();
    assert_eq!(deadline, absent_challenger.height + WINDOW);
    refused(
        &mut absent_challenger.storage,
        KEEPER,
        deadline,
        &input(17, LEASE_A, &[]),
        settlement_spends(LEASE_A),
    );
    refused(
        &mut absent_challenger.storage,
        CHALLENGER,
        deadline + 1,
        &input(16, LEASE_A, &[0]),
        vec![],
    );
    let settled = accepted(
        &mut absent_challenger.storage,
        KEEPER,
        deadline + 1,
        &input(17, LEASE_A, &[]),
        settlement_spends(LEASE_A),
    );
    assert_provider_won(&settled, &absent_challenger.storage, 2);
    assert_eq!(stake(&absent_challenger.storage), (BOND, 0, 0));

    let mut unadjudicated = Dispute::open(None);
    unadjudicated.collapse(|_, _| false);
    let deadline = unadjudicated.deadline();
    let evidence = unadjudicated.adjudication_evidence();
    refused(
        &mut unadjudicated.storage,
        KEEPER,
        deadline + 1,
        &input(17, LEASE_A, &evidence),
        settlement_spends(LEASE_A),
    );
    let settled = accepted(
        &mut unadjudicated.storage,
        KEEPER,
        deadline + 1,
        &input(17, LEASE_A, &[]),
        settlement_spends(LEASE_A),
    );
    assert_challenger_won(&settled, &unadjudicated.storage, 6);
}

#[test]
fn compiled_market_dispute_refuses_forged_wrong_replayed_unauthorized_and_premature_moves() {
    let mut dispute = Dispute::open(None);
    let height = CHALLENGED_AT + 1;
    let baseline = dispute.execution.baseline.clone();
    let reveal = |position: u32, evidence: Vec<u8>| {
        let mut tail = position.to_be_bytes().to_vec();
        tail.extend(evidence);
        input(15, LEASE_A, &tail)
    };
    let honest = dispute.reveal_input(0);
    refused(
        &mut dispute.storage,
        PROVIDER,
        500,
        &operation(12, LEASE_A),
        settlement_spends(LEASE_A),
    );
    refused(
        &mut dispute.storage,
        KEEPER,
        height,
        &input(17, LEASE_A, &[]),
        settlement_spends(LEASE_A),
    );
    refused(
        &mut dispute.storage,
        KEEPER,
        height,
        &input(17, LEASE_A, &honest[38..]),
        settlement_spends(LEASE_A),
    );
    refused(&mut dispute.storage, CHALLENGER, height, &honest, vec![]);
    refused(&mut dispute.storage, KEEPER, height, &honest, vec![]);
    refused(
        &mut dispute.storage,
        PROVIDER,
        height,
        &input(16, LEASE_A, &[1]),
        vec![],
    );
    let out_of_turn = dispute.reveal_input(1);
    refused(&mut dispute.storage, PROVIDER, height, &out_of_turn, vec![]);
    refused(
        &mut dispute.storage,
        PROVIDER,
        height,
        &reveal(0, dispute.execution.committed.evidence(&baseline, &[1])),
        vec![],
    );
    let forged = Execution::capture(LEASE_A, Some(0));
    refused(
        &mut dispute.storage,
        PROVIDER,
        height,
        &reveal(0, forged.committed.evidence(&baseline, &[0])),
        vec![],
    );
    let mut tampered = honest.clone();
    let last = tampered.len() - 1;
    tampered[last] ^= 1;
    refused(&mut dispute.storage, PROVIDER, height, &tampered, vec![]);
    let mut foreign = Storage::new();
    let mut transaction = foreign.transaction(StorageNamespace::shared(sandbox_program()));
    transaction
        .write(b"k", b"v")
        .unwrap_or_else(|e| panic!("{e:?}"));
    let _ = transaction.commit();
    let foreign = foreign
        .replay_state_bytes(MAX_BYTES as usize)
        .unwrap_or_else(|e| panic!("{e:?}"));
    refused(
        &mut dispute.storage,
        PROVIDER,
        height,
        &reveal(0, dispute.execution.committed.evidence(&foreign, &[0])),
        vec![],
    );
    open_lease(&mut dispute.storage, LEASE_B, height, 400);
    let mut wrong_lease = honest.clone();
    wrong_lease[2..34].copy_from_slice(&LEASE_B);
    refused(&mut dispute.storage, PROVIDER, height, &wrong_lease, vec![]);

    dispute.collapse(|_, _| false);
    let height = dispute.height + 1;
    let (_, position, ..) = dispute.state();
    let genuine = dispute.adjudication_evidence();
    let liar = Execution::capture(LEASE_A, Some(1));
    refused(
        &mut dispute.storage,
        KEEPER,
        height,
        &input(
            17,
            LEASE_A,
            &liar
                .committed
                .evidence(&baseline, &[position, position + 1]),
        ),
        settlement_spends(LEASE_A),
    );
    refused(
        &mut dispute.storage,
        KEEPER,
        height,
        &input(
            17,
            LEASE_A,
            &dispute
                .execution
                .committed
                .evidence(&foreign, &[position, position + 1]),
        ),
        settlement_spends(LEASE_A),
    );
    refused(
        &mut dispute.storage,
        KEEPER,
        height,
        &input(
            17,
            LEASE_A,
            &dispute.execution.committed.evidence(&baseline, &[position]),
        ),
        settlement_spends(LEASE_A),
    );
    refused(
        &mut dispute.storage,
        KEEPER,
        height,
        &input(17, LEASE_B, &genuine),
        settlement_spends(LEASE_B),
    );
    refused(
        &mut dispute.storage,
        KEEPER,
        height,
        &input(17, LEASE_A, &genuine),
        vec![],
    );
    refused(
        &mut dispute.storage,
        KEEPER,
        height,
        &input(17, LEASE_A, &genuine),
        vec![spend(escrow(LEASE_A), PAYOUT, PAYABLE)],
    );
    let settled = accepted(
        &mut dispute.storage,
        KEEPER,
        height,
        &input(17, LEASE_A, &genuine),
        settlement_spends(LEASE_A),
    );
    assert_provider_won(&settled, &dispute.storage, 4);
    refused(
        &mut dispute.storage,
        KEEPER,
        height + 1,
        &input(17, LEASE_A, &genuine),
        settlement_spends(LEASE_A),
    );
    refused(
        &mut dispute.storage,
        KEEPER,
        500,
        &input(17, LEASE_A, &[]),
        settlement_spends(LEASE_A),
    );
    refused(
        &mut dispute.storage,
        PROVIDER,
        height + 1,
        &honest,
        settlement_spends(LEASE_A),
    );

    let mut usage_claim = bonded();
    open_lease(&mut usage_claim, LEASE_B, 2, 400);
    let attested = seal_inputs(&mut usage_claim, LEASE_B);
    let mut commitment = claim_id(LEASE_B).to_vec();
    commitment.extend(LEASE_B);
    commitment.extend(attested);
    commitment.extend([0x44; 32]);
    commitment.extend([0x46; 32]);
    for value in USAGE {
        commitment.extend(value.to_be_bytes());
    }
    commitment.extend(PAYABLE.to_be_bytes());
    commitment.extend(CHALLENGE_STAKE.to_be_bytes());
    commitment.extend(WINDOW.to_be_bytes());
    let mut committed = vec![1, 10];
    committed.extend(commitment);
    accepted(&mut usage_claim, PROVIDER, 6, &committed, vec![]);
    refused(
        &mut usage_claim,
        CHALLENGER,
        CHALLENGED_AT,
        &input(11, LEASE_B, &challenge([0x72; 32], attested, [0x46; 32])),
        vec![fund(b"challenge/a", CHALLENGE_STAKE)],
    );
}

#[test]
fn compiled_market_dispute_resumes_after_restart_without_duplicate_effects() {
    let restart = |storage: &Storage| {
        let bytes = storage
            .replay_state_bytes(MAX_BYTES as usize)
            .unwrap_or_else(|e| panic!("persisted state: {e:?}"));
        Storage::decode_untrusted_replay_state(&bytes, MAX_BYTES as usize)
            .unwrap_or_else(|e| panic!("restored state: {e:?}"))
    };
    let mut dispute = Dispute::open(Some(lying_from_middle()));
    let replayed = dispute.reveal_input(0);
    dispute.reveal(0);
    dispute.storage = restart(&dispute.storage);
    let resumed = dispute_row(&dispute.storage, LEASE_A);
    refused(
        &mut dispute.storage,
        PROVIDER,
        dispute.height,
        &replayed,
        vec![],
    );
    assert_eq!(dispute_row(&dispute.storage, LEASE_A), resumed);
    let settled = dispute.play(honest_challenger);
    assert_challenger_won(&settled, &dispute.storage, 4);
    let settlement = settlement_row(&dispute.storage, LEASE_A);
    let before = dispute.storage.clone();
    dispute.storage = restart(&dispute.storage);
    assert!(dispute.storage == before);
    assert_eq!(settlement_row(&dispute.storage, LEASE_A), settlement);
    refused(
        &mut dispute.storage,
        KEEPER,
        500,
        &input(17, LEASE_A, &[]),
        settlement_spends(LEASE_A),
    );
    refused(
        &mut dispute.storage,
        PROVIDER,
        dispute.height + 1,
        &replayed,
        settlement_spends(LEASE_A),
    );
    assert_eq!(stake(&dispute.storage), (BOND - FUNDED, 0, FUNDED));
    assert!(dispute.moves <= bisection_bound(dispute.execution.committed.count()));
}

#[test]
fn compiled_market_concurrent_leases_are_not_over_slashed_and_the_offer_closes_after_disputes() {
    let mut dispute = Dispute::open(Some(lying_from_middle()));
    open_lease(&mut dispute.storage, LEASE_B, CHALLENGED_AT, 30);
    assert_eq!(stake(&dispute.storage), (BOND, 2 * FUNDED, 0));
    assert_eq!(available_capacity(&dispute.storage), 80);
    let settled = dispute.play(honest_challenger);
    assert_challenger_won(&settled, &dispute.storage, 4);
    assert_eq!(stake(&dispute.storage), (BOND - FUNDED, FUNDED, FUNDED));
    assert_eq!(available_capacity(&dispute.storage), 90);
    refused(
        &mut dispute.storage,
        PROVIDER,
        29,
        &operation(5, [1; 32]),
        vec![spend(b"offer/stake", PROVIDER, BOND - FUNDED)],
    );
    let expired = accepted(
        &mut dispute.storage,
        TENANT,
        30,
        &operation(4, LEASE_B),
        vec![spend(escrow(LEASE_B), TENANT, FUNDED)],
    );
    assert_eq!(
        moved(&expired),
        vec![(source(escrow(LEASE_B)), TENANT, FUNDED)]
    );
    assert_eq!(stake(&dispute.storage), (BOND - FUNDED, 0, FUNDED));
    assert_eq!(available_capacity(&dispute.storage), 100);
    close_offer(&mut dispute.storage, BOND - FUNDED);
}
