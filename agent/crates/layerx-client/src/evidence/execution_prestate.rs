use std::collections::BTreeMap;

use layerx_proof::receipt::VerifiedReceipt;
use layerx_proof::state::{decode_account_value, CanonicalAccount};
use layerx_proof::state_range::{
    verify_account_tree, verify_composite_roots, ModuleRangeWitness, RangeError, MAX_MODULE_LEAVES,
    MAX_UNIVERSAL_LEAVES,
};
use layerx_proof::state_witness::StateWitness;

use super::caps::{MAX_CAPS_ACCOUNT_WITNESSES, MAX_CAPS_OBJECT_BYTES};

const MAX_WITNESS_BYTES: usize = 35 + 129 + 1_048_576 + 96 * 32;

#[derive(Clone, Debug, Eq, PartialEq)]
pub enum ExecutionPrestateEvidenceError {
    Encoding,
    Bounds,
    Receipt,
    Selection,
    Range(RangeError),
    Root,
    Sequence,
    Account,
    FeeSchedule,
    Activity,
    CodeCatalogue,
}

#[derive(Clone, Debug)]
pub struct VerifiedNativeExecutionPrestate {
    network_id: u32,
    activity_id: [u8; 32],
    receipt_digest: [u8; 32],
    execution_sequence: u64,
    state_root: [u8; 32],
    accounts: BTreeMap<[u8; 32], CanonicalAccount>,
    program_records: BTreeMap<Vec<u8>, Vec<u8>>,
    canonical_bytes: Vec<u8>,
}

impl VerifiedNativeExecutionPrestate {
    pub const fn network_id(&self) -> u32 {
        self.network_id
    }
    pub const fn activity_id(&self) -> [u8; 32] {
        self.activity_id
    }
    pub const fn receipt_digest(&self) -> [u8; 32] {
        self.receipt_digest
    }
    pub const fn execution_sequence(&self) -> u64 {
        self.execution_sequence
    }
    pub const fn state_root(&self) -> [u8; 32] {
        self.state_root
    }
    pub fn all_accounts(&self) -> &BTreeMap<[u8; 32], CanonicalAccount> {
        &self.accounts
    }
    pub fn program_records(&self) -> &BTreeMap<Vec<u8>, Vec<u8>> {
        &self.program_records
    }
    pub fn canonical_bytes(&self) -> &[u8] {
        &self.canonical_bytes
    }
}

#[derive(Clone, Debug)]
pub struct VerifiedExecutionPrestate {
    network_id: u32,
    activity_id: [u8; 32],
    receipt_digest: [u8; 32],
    execution_sequence: u64,
    state_root: [u8; 32],
    accounts: BTreeMap<[u8; 32], CanonicalAccount>,
    program_records: BTreeMap<Vec<u8>, Vec<u8>>,
    selected_fee_asset: [u8; 32],
    selected_fee_schedule_version: u32,
    canonical_bytes: Vec<u8>,
}

impl VerifiedExecutionPrestate {
    pub const fn network_id(&self) -> u32 {
        self.network_id
    }
    pub const fn activity_id(&self) -> [u8; 32] {
        self.activity_id
    }
    pub const fn receipt_digest(&self) -> [u8; 32] {
        self.receipt_digest
    }
    pub const fn execution_sequence(&self) -> u64 {
        self.execution_sequence
    }
    pub const fn state_root(&self) -> [u8; 32] {
        self.state_root
    }
    pub fn all_accounts(&self) -> &BTreeMap<[u8; 32], CanonicalAccount> {
        &self.accounts
    }
    pub fn program_records(&self) -> &BTreeMap<Vec<u8>, Vec<u8>> {
        &self.program_records
    }
    pub const fn selected_fee_asset(&self) -> [u8; 32] {
        self.selected_fee_asset
    }
    pub const fn selected_fee_schedule_version(&self) -> u32 {
        self.selected_fee_schedule_version
    }
    pub fn canonical_bytes(&self) -> &[u8] {
        &self.canonical_bytes
    }
}

impl VerifiedNativeExecutionPrestate {
    pub fn for_program_call(
        self,
        anchor: &VerifiedReceipt,
    ) -> Result<VerifiedExecutionPrestate, ExecutionPrestateEvidenceError> {
        let receipt = anchor
            .receipt()
            .protocol()
            .ok_or(ExecutionPrestateEvidenceError::Receipt)?;
        let outcome = receipt
            .program_outcome()
            .ok_or(ExecutionPrestateEvidenceError::Receipt)?;
        let unsigned = layerx_wire::receipt::encode_unsigned(anchor.receipt())
            .map_err(|_| ExecutionPrestateEvidenceError::Receipt)?;
        let receipt_digest = layerx_wire::hash::receipt_digest(&unsigned)
            .map_err(|_| ExecutionPrestateEvidenceError::Receipt)?;
        if receipt.protocol_version() != 3
            || receipt.module_id() != 9
            || receipt.operation() != 3
            || outcome.encoding_version() != 4
            || receipt_digest != self.receipt_digest
            || receipt.activity_id() != self.activity_id
            || receipt.global_sequence() != self.execution_sequence
            || receipt.previous_state_root() != self.state_root
        {
            return Err(ExecutionPrestateEvidenceError::Receipt);
        }
        let selected_fee_schedule_version = outcome.fee_schedule_version();
        let mut fee_key = b"progfee/history/v1/".to_vec();
        fee_key.extend_from_slice(&selected_fee_schedule_version.to_be_bytes());
        let fee_record = self
            .program_records
            .get(&fee_key)
            .ok_or(ExecutionPrestateEvidenceError::FeeSchedule)?;
        let selected_fee_asset = fee_asset(
            fee_record,
            selected_fee_schedule_version,
            outcome.fee_schedule_prices(),
        )?;
        if outcome.occupancy_asset_id() != [0; 32]
            && outcome.occupancy_asset_id() != selected_fee_asset
        {
            return Err(ExecutionPrestateEvidenceError::FeeSchedule);
        }
        Ok(VerifiedExecutionPrestate {
            network_id: self.network_id,
            activity_id: self.activity_id,
            receipt_digest: self.receipt_digest,
            execution_sequence: self.execution_sequence,
            state_root: self.state_root,
            accounts: self.accounts,
            program_records: self.program_records,
            selected_fee_asset,
            selected_fee_schedule_version,
            canonical_bytes: self.canonical_bytes,
        })
    }
}

pub fn verify_execution_prestate_object(
    bytes: &[u8],
    anchor: &VerifiedReceipt,
    expected_network_id: u32,
) -> Result<VerifiedExecutionPrestate, ExecutionPrestateEvidenceError> {
    verify_native_execution_prestate_object(bytes, anchor, expected_network_id)?
        .for_program_call(anchor)
}

pub fn verify_native_execution_prestate_object(
    bytes: &[u8],
    anchor: &VerifiedReceipt,
    expected_network_id: u32,
) -> Result<VerifiedNativeExecutionPrestate, ExecutionPrestateEvidenceError> {
    if bytes.len() > MAX_CAPS_OBJECT_BYTES {
        return Err(ExecutionPrestateEvidenceError::Bounds);
    }
    let receipt = anchor
        .receipt()
        .protocol()
        .ok_or(ExecutionPrestateEvidenceError::Receipt)?;
    if receipt.protocol_version() != 3
        || receipt.module_id() != 9
        || receipt.global_sequence() == 0
        || receipt.activity_id() == [0; 32]
        || receipt.previous_state_root() == [0; 32]
    {
        return Err(ExecutionPrestateEvidenceError::Receipt);
    }
    let unsigned = layerx_wire::receipt::encode_unsigned(anchor.receipt())
        .map_err(|_| ExecutionPrestateEvidenceError::Receipt)?;
    let receipt_digest = layerx_wire::hash::receipt_digest(&unsigned)
        .map_err(|_| ExecutionPrestateEvidenceError::Receipt)?;
    let mut reader = Reader(bytes);
    if reader.u16()? != 1 {
        return Err(ExecutionPrestateEvidenceError::Encoding);
    }
    let network_id = reader.u32()?;
    let activity_id = reader.array()?;
    let execution_sequence = reader.u64()?;
    let state_root = reader.array()?;
    if network_id == 0
        || network_id != expected_network_id
        || activity_id != receipt.activity_id()
        || execution_sequence != receipt.global_sequence()
        || state_root != receipt.previous_state_root()
    {
        return Err(ExecutionPrestateEvidenceError::Selection);
    }
    let composite_count = reader.u16()?;
    let maximum = layerx_types::payload::ModuleId::ALL.len() + 1;
    if usize::from(composite_count) > maximum {
        return Err(ExecutionPrestateEvidenceError::Bounds);
    }
    let roots = (0..composite_count)
        .map(|_| reader.array())
        .collect::<Result<Vec<_>, _>>()?;
    verify_composite_roots(&roots, state_root).map_err(ExecutionPrestateEvidenceError::Range)?;
    let universal = reader.module(0)?;
    let programs = reader.module(9)?;
    for module in [&universal, &programs] {
        if module.composite_count != u32::from(composite_count)
            || roots.get(usize::from(module.module_id)) != Some(&module.subtree_root)
        {
            return Err(ExecutionPrestateEvidenceError::Root);
        }
    }
    universal
        .verify_prefix(state_root, b"sequence")
        .map_err(ExecutionPrestateEvidenceError::Range)?;
    let sequence = universal
        .leaves
        .iter()
        .find(|leaf| leaf.key == b"sequence")
        .ok_or(ExecutionPrestateEvidenceError::Sequence)?;
    if sequence.value.as_slice() != execution_sequence.to_be_bytes() {
        return Err(ExecutionPrestateEvidenceError::Sequence);
    }
    programs
        .verify_prefix(state_root, b"progfee/history/v1/")
        .map_err(ExecutionPrestateEvidenceError::Range)?;
    let program_records: BTreeMap<_, _> = programs
        .leaves
        .into_iter()
        .map(|leaf| (leaf.key, leaf.value))
        .collect();
    let count = reader.count(MAX_CAPS_ACCOUNT_WITNESSES)?;
    let mut account_witnesses = Vec::with_capacity(count);
    let mut accounts = BTreeMap::new();
    let mut previous = None;
    for _ in 0..count {
        let witness = reader.witness()?;
        if witness.leaf_count_b != u32::from(composite_count)
            || witness.module_id != 0
            || witness.key.len() != 33
            || witness.key[0] != 4
            || witness.account_path.is_none()
        {
            return Err(ExecutionPrestateEvidenceError::Account);
        }
        let id: [u8; 32] = witness.key[1..]
            .try_into()
            .map_err(|_| ExecutionPrestateEvidenceError::Account)?;
        if previous.is_some_and(|prior| prior >= id) {
            return Err(ExecutionPrestateEvidenceError::Account);
        }
        witness
            .verify(state_root)
            .map_err(|_| ExecutionPrestateEvidenceError::Account)?;
        let account = decode_account_value(id, &witness.value)
            .map_err(|_| ExecutionPrestateEvidenceError::Account)?;
        accounts.insert(id, account);
        previous = Some(id);
        account_witnesses.push(witness);
    }
    if !reader.0.is_empty() {
        return Err(ExecutionPrestateEvidenceError::Encoding);
    }
    verify_account_tree(&account_witnesses, &universal, state_root)
        .map_err(ExecutionPrestateEvidenceError::Range)?;
    Ok(VerifiedNativeExecutionPrestate {
        network_id,
        activity_id,
        receipt_digest,
        execution_sequence,
        state_root,
        accounts,
        program_records,
        canonical_bytes: bytes.to_vec(),
    })
}

fn fee_asset(
    bytes: &[u8],
    expected_version: u32,
    expected_prices: [u64; 7],
) -> Result<[u8; 32], ExecutionPrestateEvidenceError> {
    if bytes.len() != 217 || &bytes[..5] != b"LXFR1" || expected_version == 0 {
        return Err(ExecutionPrestateEvidenceError::FeeSchedule);
    }
    let mut reader = Reader(&bytes[5..]);
    if reader.u32()? != expected_version {
        return Err(ExecutionPrestateEvidenceError::FeeSchedule);
    }
    let mut prices = [0; 7];
    for price in &mut prices {
        *price = reader.u64()?;
    }
    let asset = reader.array()?;
    let target = reader.u64()?;
    let response_denominator = reader.u64()?;
    let maximum_change_numerator = reader.u64()?;
    let maximum_change_denominator = reader.u64()?;
    let minimum = reader.u64()?;
    let maximum = reader.u64()?;
    let activation_batch = reader.u64()?;
    let last_occupancy_batch = reader.u64()?;
    let governance_sequence = reader.u64()?;
    let governance_receipt_digest: [u8; 32] = reader.array()?;
    let _: [u8; 16] = reader.array()?;
    if prices.contains(&0)
        || prices != expected_prices
        || asset == [0; 32]
        || target == 0
        || response_denominator == 0
        || maximum_change_numerator == 0
        || maximum_change_denominator == 0
        || maximum_change_numerator > maximum_change_denominator
        || minimum == 0
        || minimum > maximum
        || prices[6] < minimum
        || prices[6] > maximum
        || activation_batch == 0
        || last_occupancy_batch == u64::MAX
        || governance_sequence == 0
        || governance_receipt_digest == [0; 32]
        || !reader.0.is_empty()
    {
        return Err(ExecutionPrestateEvidenceError::FeeSchedule);
    }
    Ok(asset)
}

struct Reader<'a>(&'a [u8]);
impl<'a> Reader<'a> {
    fn array<const N: usize>(&mut self) -> Result<[u8; N], ExecutionPrestateEvidenceError> {
        let value = self
            .0
            .get(..N)
            .ok_or(ExecutionPrestateEvidenceError::Encoding)?
            .try_into()
            .map_err(|_| ExecutionPrestateEvidenceError::Encoding)?;
        self.0 = &self.0[N..];
        Ok(value)
    }
    fn u8(&mut self) -> Result<u8, ExecutionPrestateEvidenceError> {
        Ok(self.array::<1>()?[0])
    }
    fn u16(&mut self) -> Result<u16, ExecutionPrestateEvidenceError> {
        Ok(u16::from_be_bytes(self.array()?))
    }
    fn u32(&mut self) -> Result<u32, ExecutionPrestateEvidenceError> {
        Ok(u32::from_be_bytes(self.array()?))
    }
    fn u64(&mut self) -> Result<u64, ExecutionPrestateEvidenceError> {
        Ok(u64::from_be_bytes(self.array()?))
    }
    fn count(&mut self, maximum: usize) -> Result<usize, ExecutionPrestateEvidenceError> {
        let count =
            usize::try_from(self.u32()?).map_err(|_| ExecutionPrestateEvidenceError::Bounds)?;
        if count > maximum {
            return Err(ExecutionPrestateEvidenceError::Bounds);
        }
        Ok(count)
    }
    fn bytes(&mut self, maximum: usize) -> Result<&'a [u8], ExecutionPrestateEvidenceError> {
        let length = self.count(maximum)?;
        let value = self
            .0
            .get(..length)
            .ok_or(ExecutionPrestateEvidenceError::Encoding)?;
        self.0 = &self.0[length..];
        Ok(value)
    }
    fn witness(&mut self) -> Result<StateWitness, ExecutionPrestateEvidenceError> {
        StateWitness::decode(self.bytes(MAX_WITNESS_BYTES)?)
            .map_err(|_| ExecutionPrestateEvidenceError::Account)
    }
    fn module(
        &mut self,
        expected: u16,
    ) -> Result<ModuleRangeWitness, ExecutionPrestateEvidenceError> {
        let module_id = self.u16()?;
        if module_id != expected {
            return Err(ExecutionPrestateEvidenceError::Encoding);
        }
        let subtree_root = self.array()?;
        let composite_index = self.u32()?;
        let composite_count = self.u32()?;
        let depth = self.u8()?;
        if depth > 32 {
            return Err(ExecutionPrestateEvidenceError::Bounds);
        }
        let composite_siblings = (0..depth)
            .map(|_| self.array())
            .collect::<Result<Vec<_>, _>>()?;
        let count = self.count(if module_id == 0 {
            MAX_UNIVERSAL_LEAVES
        } else {
            MAX_MODULE_LEAVES
        })?;
        let leaves = (0..count)
            .map(|_| self.witness())
            .collect::<Result<Vec<_>, _>>()?;
        Ok(ModuleRangeWitness {
            module_id,
            subtree_root,
            composite_index,
            composite_count,
            composite_siblings,
            leaves,
        })
    }
}

impl std::fmt::Display for ExecutionPrestateEvidenceError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "{self:?}")
    }
}
impl std::error::Error for ExecutionPrestateEvidenceError {}

fn verify_asset_inner(
    bytes: &[u8],
    anchor: &VerifiedReceipt,
    expected_network_id: u32,
) -> Result<VerifiedNativeExecutionPrestate, ExecutionPrestateEvidenceError> {
    if bytes.len() > MAX_CAPS_OBJECT_BYTES {
        return Err(ExecutionPrestateEvidenceError::Bounds);
    }
    let receipt = anchor
        .receipt()
        .protocol()
        .ok_or(ExecutionPrestateEvidenceError::Receipt)?;
    if receipt.protocol_version() != 3
        || receipt.module_id() != 1
        || receipt.operation() != 5
        || receipt.global_sequence() == 0
        || receipt.activity_id() == [0; 32]
        || receipt.previous_state_root() == [0; 32]
    {
        return Err(ExecutionPrestateEvidenceError::Receipt);
    }
    let unsigned = layerx_wire::receipt::encode_unsigned(anchor.receipt())
        .map_err(|_| ExecutionPrestateEvidenceError::Receipt)?;
    let receipt_digest = layerx_wire::hash::receipt_digest(&unsigned)
        .map_err(|_| ExecutionPrestateEvidenceError::Receipt)?;
    let mut reader = Reader(bytes);
    if reader.u16()? != 1 {
        return Err(ExecutionPrestateEvidenceError::Encoding);
    }
    let network_id = reader.u32()?;
    let activity_id = reader.array()?;
    let execution_sequence = reader.u64()?;
    let state_root = reader.array()?;
    if network_id == 0
        || network_id != expected_network_id
        || activity_id != receipt.activity_id()
        || execution_sequence != receipt.global_sequence()
        || state_root != receipt.previous_state_root()
    {
        return Err(ExecutionPrestateEvidenceError::Selection);
    }
    let composite_count = reader.u16()?;
    let maximum = layerx_types::payload::ModuleId::ALL.len() + 1;
    if usize::from(composite_count) > maximum {
        return Err(ExecutionPrestateEvidenceError::Bounds);
    }
    let roots = (0..composite_count)
        .map(|_| reader.array())
        .collect::<Result<Vec<_>, _>>()?;
    verify_composite_roots(&roots, state_root).map_err(ExecutionPrestateEvidenceError::Range)?;
    let universal = reader.module(0)?;
    let programs = reader.module(9)?;
    for module in [&universal, &programs] {
        if module.composite_count != u32::from(composite_count)
            || roots.get(usize::from(module.module_id)) != Some(&module.subtree_root)
        {
            return Err(ExecutionPrestateEvidenceError::Root);
        }
    }
    universal
        .verify_prefix(state_root, b"sequence")
        .map_err(ExecutionPrestateEvidenceError::Range)?;
    let sequence = universal
        .leaves
        .iter()
        .find(|leaf| leaf.key == b"sequence")
        .ok_or(ExecutionPrestateEvidenceError::Sequence)?;
    if sequence.value.as_slice() != execution_sequence.to_be_bytes() {
        return Err(ExecutionPrestateEvidenceError::Sequence);
    }
    programs
        .verify_prefix(state_root, b"progfee/history/v1/")
        .map_err(ExecutionPrestateEvidenceError::Range)?;
    let program_records: BTreeMap<_, _> = programs
        .leaves
        .into_iter()
        .map(|leaf| (leaf.key, leaf.value))
        .collect();
    let count = reader.count(MAX_CAPS_ACCOUNT_WITNESSES)?;
    let mut account_witnesses = Vec::with_capacity(count);
    let mut accounts = BTreeMap::new();
    let mut previous = None;
    for _ in 0..count {
        let witness = reader.witness()?;
        if witness.leaf_count_b != u32::from(composite_count)
            || witness.module_id != 0
            || witness.key.len() != 33
            || witness.key[0] != 4
            || witness.account_path.is_none()
        {
            return Err(ExecutionPrestateEvidenceError::Account);
        }
        let id: [u8; 32] = witness.key[1..]
            .try_into()
            .map_err(|_| ExecutionPrestateEvidenceError::Account)?;
        if previous.is_some_and(|prior| prior >= id) {
            return Err(ExecutionPrestateEvidenceError::Account);
        }
        witness
            .verify(state_root)
            .map_err(|_| ExecutionPrestateEvidenceError::Account)?;
        let account = decode_account_value(id, &witness.value)
            .map_err(|_| ExecutionPrestateEvidenceError::Account)?;
        accounts.insert(id, account);
        previous = Some(id);
        account_witnesses.push(witness);
    }
    if !reader.0.is_empty() {
        return Err(ExecutionPrestateEvidenceError::Encoding);
    }
    verify_account_tree(&account_witnesses, &universal, state_root)
        .map_err(ExecutionPrestateEvidenceError::Range)?;
    Ok(VerifiedNativeExecutionPrestate {
        network_id,
        activity_id,
        receipt_digest,
        execution_sequence,
        state_root,
        accounts,
        program_records,
        canonical_bytes: bytes.to_vec(),
    })
}

#[derive(Clone, Debug)]
pub struct VerifiedAssetFeePolicy {
    parameter_version: u32,
    encoding_version: u16,
    base_fee: u128,
    per_activity_type_unit: u128,
    per_encoded_byte: u128,
    per_execution_unit: u128,
    per_storage_unit: u128,
    multiplier_basis_points: u32,
    asset_prices: Vec<u128>,
    module_prices: Vec<u128>,
    canonical_bytes: Vec<u8>,
}

impl VerifiedAssetFeePolicy {
    pub const fn parameter_version(&self) -> u32 {
        self.parameter_version
    }
    pub const fn encoding_version(&self) -> u16 {
        self.encoding_version
    }
    pub const fn base_fee(&self) -> u128 {
        self.base_fee
    }
    pub const fn per_activity_type_unit(&self) -> u128 {
        self.per_activity_type_unit
    }
    pub const fn per_encoded_byte(&self) -> u128 {
        self.per_encoded_byte
    }
    pub const fn per_execution_unit(&self) -> u128 {
        self.per_execution_unit
    }
    pub const fn per_storage_unit(&self) -> u128 {
        self.per_storage_unit
    }
    pub const fn multiplier_basis_points(&self) -> u32 {
        self.multiplier_basis_points
    }
    pub fn asset_prices(&self) -> &[u128] {
        &self.asset_prices
    }
    pub fn module_prices(&self) -> &[u128] {
        &self.module_prices
    }
    pub fn canonical_bytes(&self) -> &[u8] {
        &self.canonical_bytes
    }
}

#[derive(Clone, Debug)]
pub struct VerifiedAssetExecutionPrestate {
    inner: VerifiedNativeExecutionPrestate,
    activity: layerx_wire::activity::Activity,
    asset_records: BTreeMap<Vec<u8>, Vec<u8>>,
    governance_records: BTreeMap<Vec<u8>, Vec<u8>>,
    fee_policy: VerifiedAssetFeePolicy,
    canonical_bytes: Vec<u8>,
}

impl VerifiedAssetExecutionPrestate {
    pub const fn network_id(&self) -> u32 {
        self.inner.network_id()
    }
    pub const fn activity_id(&self) -> [u8; 32] {
        self.inner.activity_id()
    }
    pub const fn receipt_digest(&self) -> [u8; 32] {
        self.inner.receipt_digest()
    }
    pub const fn execution_sequence(&self) -> u64 {
        self.inner.execution_sequence()
    }
    pub const fn state_root(&self) -> [u8; 32] {
        self.inner.state_root()
    }
    pub fn all_accounts(&self) -> &BTreeMap<[u8; 32], CanonicalAccount> {
        self.inner.all_accounts()
    }
    pub const fn activity(&self) -> &layerx_wire::activity::Activity {
        &self.activity
    }
    pub fn asset_records(&self) -> &BTreeMap<Vec<u8>, Vec<u8>> {
        &self.asset_records
    }
    pub fn governance_records(&self) -> &BTreeMap<Vec<u8>, Vec<u8>> {
        &self.governance_records
    }
    pub const fn fee_policy(&self) -> &VerifiedAssetFeePolicy {
        &self.fee_policy
    }
    pub fn canonical_bytes(&self) -> &[u8] {
        &self.canonical_bytes
    }
}

pub fn verify_asset_execution_prestate_object(
    bytes: &[u8],
    anchor: &VerifiedReceipt,
    expected_network_id: u32,
) -> Result<VerifiedAssetExecutionPrestate, ExecutionPrestateEvidenceError> {
    use layerx_types::payload::{ActivityType, ModuleId, ModuleRegistration, ModuleRegistry};
    if bytes.len() > MAX_CAPS_OBJECT_BYTES {
        return Err(ExecutionPrestateEvidenceError::Bounds);
    }
    let mut reader = Reader(bytes);
    if reader.u16()? != 1 {
        return Err(ExecutionPrestateEvidenceError::Encoding);
    }
    let native = reader.bytes(MAX_CAPS_OBJECT_BYTES)?;
    let inner = verify_asset_inner(native, anchor, expected_network_id)?;
    let receipt = anchor
        .receipt()
        .protocol()
        .ok_or(ExecutionPrestateEvidenceError::Receipt)?;
    let kind = ActivityType::new(ModuleId::Asset, 5)
        .map_err(|_| ExecutionPrestateEvidenceError::Activity)?;
    let registration = ModuleRegistration::new(ModuleId::Asset, &[kind])
        .map_err(|_| ExecutionPrestateEvidenceError::Activity)?;
    let registry = ModuleRegistry::new(&[registration])
        .map_err(|_| ExecutionPrestateEvidenceError::Activity)?;
    let activity_bytes = reader.bytes(layerx_wire::limits::MAX_MESSAGE_BYTES)?;
    let activity = layerx_wire::activity::decode_signed(activity_bytes, &registry)
        .map_err(|_| ExecutionPrestateEvidenceError::Activity)?;
    if layerx_wire::activity::encode_signed(&activity)
        .map_err(|_| ExecutionPrestateEvidenceError::Activity)?
        != activity_bytes
        || activity.protocol_version() != receipt.protocol_version()
        || activity.network_id() != expected_network_id
        || activity.activity_type() != kind
        || activity.actor_did().is_empty()
        || layerx_wire::hash::activity_id(&activity)
            .map_err(|_| ExecutionPrestateEvidenceError::Activity)?
            != inner.activity_id()
        || layerx_wire::hash::payload_hash(&activity)
            .map_err(|_| ExecutionPrestateEvidenceError::Activity)?
            != activity.payload_hash()
    {
        return Err(ExecutionPrestateEvidenceError::Activity);
    }
    let key: [u8; 32] = activity
        .authority()
        .try_into()
        .map_err(|_| ExecutionPrestateEvidenceError::Activity)?;
    let signature: [u8; 64] = activity
        .signature()
        .ok_or(ExecutionPrestateEvidenceError::Activity)?
        .try_into()
        .map_err(|_| ExecutionPrestateEvidenceError::Activity)?;
    let unsigned = layerx_wire::activity::signing_bytes(&activity)
        .map_err(|_| ExecutionPrestateEvidenceError::Activity)?;
    let message = layerx_crypto::SignatureMessage::new(
        layerx_wire::hash::Domain::SignaturePreimage,
        activity.protocol_version(),
        activity.network_id(),
        unsigned.as_bytes(),
    )
    .map_err(|_| ExecutionPrestateEvidenceError::Activity)?;
    layerx_crypto::ed25519::verify(&key, &signature, message)
        .map_err(|_| ExecutionPrestateEvidenceError::Activity)?;
    let mut roots_reader = Reader(
        native
            .get(78..)
            .ok_or(ExecutionPrestateEvidenceError::Encoding)?,
    );
    let composite_count = roots_reader.u16()?;
    let roots = (0..composite_count)
        .map(|_| roots_reader.array())
        .collect::<Result<Vec<[u8; 32]>, _>>()?;
    if reader.u16()? != 2 {
        return Err(ExecutionPrestateEvidenceError::Encoding);
    }
    let asset = reader.module(1)?;
    let governance = reader.module(7)?;
    if !reader.0.is_empty() {
        return Err(ExecutionPrestateEvidenceError::Encoding);
    }
    let mut inventories = Vec::with_capacity(2);
    for module in [&asset, &governance] {
        if module.composite_index != u32::from(module.module_id)
            || module.composite_count != u32::from(composite_count)
            || roots.get(usize::from(module.module_id)) != Some(&module.subtree_root)
        {
            return Err(ExecutionPrestateEvidenceError::Root);
        }
        inventories.push(
            module
                .verify_full_module(inner.state_root())
                .map_err(ExecutionPrestateEvidenceError::Range)?,
        );
    }
    let asset_records = inventories[0].records().iter().cloned().collect();
    let governance_records = inventories[1].records().iter().cloned().collect();
    let fee_policy = verified_asset_fee_policy(&governance_records, receipt.parameter_version())?;
    Ok(VerifiedAssetExecutionPrestate {
        inner,
        activity,
        asset_records,
        governance_records,
        fee_policy,
        canonical_bytes: bytes.to_vec(),
    })
}

fn verified_asset_fee_policy(
    records: &BTreeMap<Vec<u8>, Vec<u8>>,
    parameter_version: u32,
) -> Result<VerifiedAssetFeePolicy, ExecutionPrestateEvidenceError> {
    let failure = ExecutionPrestateEvidenceError::FeeSchedule;
    let mut parameter_key = [0; 32];
    parameter_key[..17].copy_from_slice(b"parameter-version");
    let parameter = records
        .get(parameter_key.as_slice())
        .ok_or(failure.clone())?;
    if parameter.len() != 32
        || parameter[..28] != [0; 28]
        || parameter_version == 0
        || parameter_version > u32::from(u16::MAX)
        || parameter[28..] != parameter_version.to_be_bytes()
    {
        return Err(failure);
    }
    fn selected<'a>(
        records: &'a BTreeMap<Vec<u8>, Vec<u8>>,
        prefix: &[u8],
    ) -> Result<Option<&'a [u8]>, ExecutionPrestateEvidenceError> {
        let mut selected = None;
        for (key, value) in records {
            if key.len() == prefix.len() && key == prefix
                || key.len() == 32
                    && key.starts_with(prefix)
                    && key[prefix.len()..].iter().all(|byte| *byte == 0)
            {
                if selected.is_some() {
                    return Err(ExecutionPrestateEvidenceError::FeeSchedule);
                }
                selected = Some(value.as_slice());
            }
        }
        Ok(selected)
    }
    let head = selected(records, b"fee.schedule")?;
    let modules = selected(records, b"fee.module-prices")?;
    let encoded = match head {
        None if parameter_version == 1 && modules.is_none() => {
            let mut bytes = vec![0; 86];
            bytes[1] = 1;
            bytes[82..86].copy_from_slice(&10000_u32.to_be_bytes());
            bytes
        }
        None => return Err(failure),
        Some(head) if head.starts_with(&[0, 4]) => {
            if head.len() != 256 || modules.is_none_or(|prices| prices.len() != 112) {
                return Err(failure);
            }
            let mut bytes = head.to_vec();
            bytes.extend_from_slice(modules.ok_or(failure.clone())?);
            bytes
        }
        Some(head) if modules.is_none() => head.to_vec(),
        Some(_) => return Err(failure),
    };
    let mut reader = Reader(&encoded);
    let version = reader.u16()?;
    let expected = match version {
        1 => 86,
        2 => 247,
        3 => 255,
        4 => 368,
        _ => return Err(failure),
    };
    if encoded.len() != expected {
        return Err(failure);
    }
    let base_fee = u128::from_be_bytes(reader.array()?);
    let per_activity_type_unit = u128::from_be_bytes(reader.array()?);
    let per_encoded_byte = u128::from_be_bytes(reader.array()?);
    let per_execution_unit = u128::from_be_bytes(reader.array()?);
    let per_storage_unit = u128::from_be_bytes(reader.array()?);
    let multiplier_basis_points = reader.u32()?;
    let mut asset_prices = Vec::new();
    if version >= 2 {
        if reader.u8()? != if version == 2 { 10 } else { 11 } {
            return Err(failure);
        }
        for _ in 0..10 {
            asset_prices.push(u128::from_be_bytes(reader.array()?));
        }
        if version >= 3 {
            asset_prices.push(u128::from(reader.u64()?));
        }
    }
    let mut module_prices = Vec::new();
    if version == 4 {
        if reader.u8()? != 7 {
            return Err(failure);
        }
        for _ in 0..7 {
            module_prices.push(u128::from_be_bytes(reader.array()?));
        }
    }
    if !reader.0.is_empty() {
        return Err(failure);
    }
    Ok(VerifiedAssetFeePolicy {
        parameter_version,
        encoding_version: version,
        base_fee,
        per_activity_type_unit,
        per_encoded_byte,
        per_execution_unit,
        per_storage_unit,
        multiplier_basis_points,
        asset_prices,
        module_prices,
        canonical_bytes: encoded,
    })
}

const MAX_REPLAY_CODE_BLOBS: usize = 512;
const MAX_REPLAY_CODE_BYTES: usize = 1_048_576;

#[derive(Clone, Debug)]
pub struct VerifiedReplayCatalogue {
    legacy: VerifiedNativeExecutionPrestate,
    code_blobs: BTreeMap<[u8; 32], Vec<u8>>,
    canonical_bytes: Vec<u8>,
}

impl VerifiedReplayCatalogue {
    pub const fn legacy(&self) -> &VerifiedNativeExecutionPrestate {
        &self.legacy
    }
    pub fn program_records(&self) -> &BTreeMap<Vec<u8>, Vec<u8>> {
        self.legacy.program_records()
    }
    pub fn code_blobs(&self) -> &BTreeMap<[u8; 32], Vec<u8>> {
        &self.code_blobs
    }
    pub fn canonical_bytes(&self) -> &[u8] {
        &self.canonical_bytes
    }
}

pub fn verify_replay_catalogue_object(
    bytes: &[u8],
    anchor: &VerifiedReceipt,
    expected_network_id: u32,
) -> Result<VerifiedReplayCatalogue, ExecutionPrestateEvidenceError> {
    use sha2::{Digest, Sha256};
    if bytes.len() > MAX_CAPS_OBJECT_BYTES {
        return Err(ExecutionPrestateEvidenceError::Bounds);
    }
    let mut reader = Reader(bytes);
    if reader.u16()? != 1 {
        return Err(ExecutionPrestateEvidenceError::Encoding);
    }
    let legacy_bytes = reader.bytes(MAX_CAPS_OBJECT_BYTES)?;
    let legacy =
        verify_native_execution_prestate_object(legacy_bytes, anchor, expected_network_id)?;
    let mut required = BTreeMap::<[u8; 32], usize>::new();
    for (key, record) in legacy.program_records() {
        if !key.starts_with(b"program\0") {
            continue;
        }
        if key.len() != 40 || record.len() != 71 {
            return Err(ExecutionPrestateEvidenceError::CodeCatalogue);
        }
        let hash: [u8; 32] = record[33..65]
            .try_into()
            .map_err(|_| ExecutionPrestateEvidenceError::CodeCatalogue)?;
        let mut manifest_key = b"progcode".to_vec();
        manifest_key.extend_from_slice(&key[8..]);
        let manifest = legacy
            .program_records()
            .get(&manifest_key)
            .ok_or(ExecutionPrestateEvidenceError::CodeCatalogue)?;
        if manifest.len() != 38 || manifest[..2] != [0, 1] || manifest[6..] != hash {
            return Err(ExecutionPrestateEvidenceError::CodeCatalogue);
        }
        let length = usize::try_from(u32::from_be_bytes(
            manifest[2..6]
                .try_into()
                .map_err(|_| ExecutionPrestateEvidenceError::CodeCatalogue)?,
        ))
        .map_err(|_| ExecutionPrestateEvidenceError::Bounds)?;
        if length == 0 || length > MAX_REPLAY_CODE_BYTES {
            return Err(ExecutionPrestateEvidenceError::Bounds);
        }
        if required
            .insert(hash, length)
            .is_some_and(|prior| prior != length)
        {
            return Err(ExecutionPrestateEvidenceError::CodeCatalogue);
        }
    }
    let count = reader.count(MAX_REPLAY_CODE_BLOBS)?;
    if count != required.len() {
        return Err(ExecutionPrestateEvidenceError::CodeCatalogue);
    }
    let mut code_blobs = BTreeMap::new();
    let mut previous = None;
    for _ in 0..count {
        let hash: [u8; 32] = reader.array()?;
        if previous.is_some_and(|prior| prior >= hash) {
            return Err(ExecutionPrestateEvidenceError::CodeCatalogue);
        }
        let raw = reader.bytes(MAX_REPLAY_CODE_BYTES)?;
        if raw.is_empty()
            || required.get(&hash) != Some(&raw.len())
            || <[u8; 32]>::from(Sha256::digest(raw)) != hash
        {
            return Err(ExecutionPrestateEvidenceError::CodeCatalogue);
        }
        code_blobs.insert(hash, raw.to_vec());
        previous = Some(hash);
    }
    if !reader.0.is_empty() {
        return Err(ExecutionPrestateEvidenceError::Encoding);
    }
    Ok(VerifiedReplayCatalogue {
        legacy,
        code_blobs,
        canonical_bytes: bytes.to_vec(),
    })
}
