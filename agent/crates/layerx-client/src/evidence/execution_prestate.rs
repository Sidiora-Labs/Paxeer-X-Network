use std::collections::BTreeMap;

use layerx_proof::receipt::VerifiedReceipt;
use layerx_proof::state::{decode_account_value, CanonicalAccount};
use layerx_proof::state_range::{verify_account_tree, verify_composite_roots, ModuleRangeWitness, RangeError, MAX_MODULE_LEAVES, MAX_UNIVERSAL_LEAVES};
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
    pub const fn network_id(&self) -> u32 { self.network_id }
    pub const fn activity_id(&self) -> [u8; 32] { self.activity_id }
    pub const fn receipt_digest(&self) -> [u8; 32] { self.receipt_digest }
    pub const fn execution_sequence(&self) -> u64 { self.execution_sequence }
    pub const fn state_root(&self) -> [u8; 32] { self.state_root }
    pub fn all_accounts(&self) -> &BTreeMap<[u8; 32], CanonicalAccount> { &self.accounts }
    pub fn program_records(&self) -> &BTreeMap<Vec<u8>, Vec<u8>> { &self.program_records }
    pub fn canonical_bytes(&self) -> &[u8] { &self.canonical_bytes }
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
    pub const fn network_id(&self) -> u32 { self.network_id }
    pub const fn activity_id(&self) -> [u8; 32] { self.activity_id }
    pub const fn receipt_digest(&self) -> [u8; 32] { self.receipt_digest }
    pub const fn execution_sequence(&self) -> u64 { self.execution_sequence }
    pub const fn state_root(&self) -> [u8; 32] { self.state_root }
    pub fn all_accounts(&self) -> &BTreeMap<[u8; 32], CanonicalAccount> { &self.accounts }
    pub fn program_records(&self) -> &BTreeMap<Vec<u8>, Vec<u8>> { &self.program_records }
    pub const fn selected_fee_asset(&self) -> [u8; 32] { self.selected_fee_asset }
    pub const fn selected_fee_schedule_version(&self) -> u32 { self.selected_fee_schedule_version }
    pub fn canonical_bytes(&self) -> &[u8] { &self.canonical_bytes }
}


impl VerifiedNativeExecutionPrestate {
    pub fn for_program_call(self, anchor: &VerifiedReceipt) -> Result<VerifiedExecutionPrestate, ExecutionPrestateEvidenceError> {
        let receipt = anchor.receipt().protocol().ok_or(ExecutionPrestateEvidenceError::Receipt)?;
        let outcome = receipt.program_outcome().ok_or(ExecutionPrestateEvidenceError::Receipt)?;
        let unsigned = layerx_wire::receipt::encode_unsigned(anchor.receipt()).map_err(|_| ExecutionPrestateEvidenceError::Receipt)?;
        let receipt_digest = layerx_wire::hash::receipt_digest(&unsigned).map_err(|_| ExecutionPrestateEvidenceError::Receipt)?;
        if receipt.protocol_version() != 3 || receipt.module_id() != 9 || receipt.operation() != 3
            || outcome.encoding_version() != 4 || receipt_digest != self.receipt_digest
            || receipt.activity_id() != self.activity_id || receipt.global_sequence() != self.execution_sequence
            || receipt.previous_state_root() != self.state_root
        { return Err(ExecutionPrestateEvidenceError::Receipt); }
        let selected_fee_schedule_version = outcome.fee_schedule_version();
        let mut fee_key = b"progfee/history/v1/".to_vec();
        fee_key.extend_from_slice(&selected_fee_schedule_version.to_be_bytes());
        let fee_record = self.program_records.get(&fee_key).ok_or(ExecutionPrestateEvidenceError::FeeSchedule)?;
        let selected_fee_asset = fee_asset(fee_record, selected_fee_schedule_version, outcome.fee_schedule_prices())?;
        if outcome.occupancy_asset_id() != [0; 32] && outcome.occupancy_asset_id() != selected_fee_asset {
            return Err(ExecutionPrestateEvidenceError::FeeSchedule);
        }
        Ok(VerifiedExecutionPrestate { network_id: self.network_id, activity_id: self.activity_id,
            receipt_digest: self.receipt_digest, execution_sequence: self.execution_sequence, state_root: self.state_root,
            accounts: self.accounts, program_records: self.program_records, selected_fee_asset,
            selected_fee_schedule_version, canonical_bytes: self.canonical_bytes })
    }
}

pub fn verify_execution_prestate_object(bytes: &[u8], anchor: &VerifiedReceipt,
    expected_network_id: u32) -> Result<VerifiedExecutionPrestate, ExecutionPrestateEvidenceError> {
    verify_native_execution_prestate_object(bytes, anchor, expected_network_id)?.for_program_call(anchor)
}

pub fn verify_native_execution_prestate_object(bytes: &[u8], anchor: &VerifiedReceipt,
    expected_network_id: u32) -> Result<VerifiedNativeExecutionPrestate, ExecutionPrestateEvidenceError> {
    if bytes.len() > MAX_CAPS_OBJECT_BYTES { return Err(ExecutionPrestateEvidenceError::Bounds); }
    let receipt = anchor.receipt().protocol().ok_or(ExecutionPrestateEvidenceError::Receipt)?;
    if receipt.protocol_version() != 3 || receipt.module_id() != 9 || receipt.global_sequence() == 0
        || receipt.activity_id() == [0; 32] || receipt.previous_state_root() == [0; 32]
    { return Err(ExecutionPrestateEvidenceError::Receipt); }
    let unsigned = layerx_wire::receipt::encode_unsigned(anchor.receipt()).map_err(|_| ExecutionPrestateEvidenceError::Receipt)?;
    let receipt_digest = layerx_wire::hash::receipt_digest(&unsigned).map_err(|_| ExecutionPrestateEvidenceError::Receipt)?;
    let mut reader = Reader(bytes);
    if reader.u16()? != 1 { return Err(ExecutionPrestateEvidenceError::Encoding); }
    let network_id = reader.u32()?;
    let activity_id = reader.array()?;
    let execution_sequence = reader.u64()?;
    let state_root = reader.array()?;
    if network_id == 0 || network_id != expected_network_id || activity_id != receipt.activity_id()
        || execution_sequence != receipt.global_sequence() || state_root != receipt.previous_state_root()
    { return Err(ExecutionPrestateEvidenceError::Selection); }
    let composite_count = reader.u16()?;
    let maximum = layerx_types::payload::ModuleId::ALL.len() + 1;
    if usize::from(composite_count) > maximum { return Err(ExecutionPrestateEvidenceError::Bounds); }
    let roots = (0..composite_count).map(|_| reader.array()).collect::<Result<Vec<_>, _>>()?;
    verify_composite_roots(&roots, state_root).map_err(ExecutionPrestateEvidenceError::Range)?;
    let universal = reader.module(0)?;
    let programs = reader.module(9)?;
    for module in [&universal, &programs] {
        if module.composite_count != u32::from(composite_count)
            || roots.get(usize::from(module.module_id)) != Some(&module.subtree_root)
        { return Err(ExecutionPrestateEvidenceError::Root); }
    }
    universal.verify_prefix(state_root, b"sequence").map_err(ExecutionPrestateEvidenceError::Range)?;
    let sequence = universal.leaves.iter().find(|leaf| leaf.key == b"sequence")
        .ok_or(ExecutionPrestateEvidenceError::Sequence)?;
    if sequence.value.as_slice() != execution_sequence.to_be_bytes() {
        return Err(ExecutionPrestateEvidenceError::Sequence);
    }
    programs.verify_prefix(state_root, b"progfee/history/v1/").map_err(ExecutionPrestateEvidenceError::Range)?;
    let program_records: BTreeMap<_, _> = programs.leaves.into_iter().map(|leaf| (leaf.key, leaf.value)).collect();
    let count = reader.count(MAX_CAPS_ACCOUNT_WITNESSES)?;
    let mut account_witnesses = Vec::with_capacity(count);
    let mut accounts = BTreeMap::new();
    let mut previous = None;
    for _ in 0..count {
        let witness = reader.witness()?;
        if witness.leaf_count_b != u32::from(composite_count) || witness.module_id != 0
            || witness.key.len() != 33 || witness.key[0] != 4 || witness.account_path.is_none()
        { return Err(ExecutionPrestateEvidenceError::Account); }
        let id: [u8; 32] = witness.key[1..].try_into().map_err(|_| ExecutionPrestateEvidenceError::Account)?;
        if previous.is_some_and(|prior| prior >= id) { return Err(ExecutionPrestateEvidenceError::Account); }
        witness.verify(state_root).map_err(|_| ExecutionPrestateEvidenceError::Account)?;
        let account = decode_account_value(id, &witness.value).map_err(|_| ExecutionPrestateEvidenceError::Account)?;
        accounts.insert(id, account);
        previous = Some(id);
        account_witnesses.push(witness);
    }
    if !reader.0.is_empty() { return Err(ExecutionPrestateEvidenceError::Encoding); }
    verify_account_tree(&account_witnesses, &universal, state_root).map_err(ExecutionPrestateEvidenceError::Range)?;
    Ok(VerifiedNativeExecutionPrestate { network_id, activity_id, receipt_digest, execution_sequence, state_root,
        accounts, program_records, canonical_bytes: bytes.to_vec() })
}

fn fee_asset(bytes: &[u8], expected_version: u32, expected_prices: [u64; 7])
    -> Result<[u8; 32], ExecutionPrestateEvidenceError> {
    if bytes.len() != 217 || &bytes[..5] != b"LXFR1" || expected_version == 0 {
        return Err(ExecutionPrestateEvidenceError::FeeSchedule);
    }
    let mut reader = Reader(&bytes[5..]);
    if reader.u32()? != expected_version { return Err(ExecutionPrestateEvidenceError::FeeSchedule); }
    let mut prices = [0; 7];
    for price in &mut prices { *price = reader.u64()?; }
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
    if prices.contains(&0) || prices != expected_prices || asset == [0; 32] || target == 0
        || response_denominator == 0 || maximum_change_numerator == 0 || maximum_change_denominator == 0
        || maximum_change_numerator > maximum_change_denominator || minimum == 0 || minimum > maximum
        || prices[6] < minimum || prices[6] > maximum || activation_batch == 0
        || last_occupancy_batch == u64::MAX || governance_sequence == 0
        || governance_receipt_digest == [0; 32] || !reader.0.is_empty()
    { return Err(ExecutionPrestateEvidenceError::FeeSchedule); }
    Ok(asset)
}

struct Reader<'a>(&'a [u8]);
impl<'a> Reader<'a> {
    fn array<const N: usize>(&mut self) -> Result<[u8; N], ExecutionPrestateEvidenceError> {
        let value = self.0.get(..N).ok_or(ExecutionPrestateEvidenceError::Encoding)?.try_into()
            .map_err(|_| ExecutionPrestateEvidenceError::Encoding)?;
        self.0 = &self.0[N..]; Ok(value)
    }
    fn u8(&mut self) -> Result<u8, ExecutionPrestateEvidenceError> { Ok(self.array::<1>()?[0]) }
    fn u16(&mut self) -> Result<u16, ExecutionPrestateEvidenceError> { Ok(u16::from_be_bytes(self.array()?)) }
    fn u32(&mut self) -> Result<u32, ExecutionPrestateEvidenceError> { Ok(u32::from_be_bytes(self.array()?)) }
    fn u64(&mut self) -> Result<u64, ExecutionPrestateEvidenceError> { Ok(u64::from_be_bytes(self.array()?)) }
    fn count(&mut self, maximum: usize) -> Result<usize, ExecutionPrestateEvidenceError> {
        let count = usize::try_from(self.u32()?).map_err(|_| ExecutionPrestateEvidenceError::Bounds)?;
        if count > maximum { return Err(ExecutionPrestateEvidenceError::Bounds); } Ok(count)
    }
    fn bytes(&mut self, maximum: usize) -> Result<&'a [u8], ExecutionPrestateEvidenceError> {
        let length = self.count(maximum)?;
        let value = self.0.get(..length).ok_or(ExecutionPrestateEvidenceError::Encoding)?;
        self.0 = &self.0[length..]; Ok(value)
    }
    fn witness(&mut self) -> Result<StateWitness, ExecutionPrestateEvidenceError> {
        StateWitness::decode(self.bytes(MAX_WITNESS_BYTES)?).map_err(|_| ExecutionPrestateEvidenceError::Account)
    }
    fn module(&mut self, expected: u16) -> Result<ModuleRangeWitness, ExecutionPrestateEvidenceError> {
        let module_id = self.u16()?;
        if module_id != expected { return Err(ExecutionPrestateEvidenceError::Encoding); }
        let subtree_root = self.array()?; let composite_index = self.u32()?; let composite_count = self.u32()?;
        let depth = self.u8()?;
        if depth > 32 { return Err(ExecutionPrestateEvidenceError::Bounds); }
        let composite_siblings = (0..depth).map(|_| self.array()).collect::<Result<Vec<_>, _>>()?;
        let count = self.count(if module_id == 0 { MAX_UNIVERSAL_LEAVES } else { MAX_MODULE_LEAVES })?;
        let leaves = (0..count).map(|_| self.witness()).collect::<Result<Vec<_>, _>>()?;
        Ok(ModuleRangeWitness { module_id, subtree_root, composite_index, composite_count, composite_siblings, leaves })
    }
}

impl std::fmt::Display for ExecutionPrestateEvidenceError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result { write!(f, "{self:?}") }
}
impl std::error::Error for ExecutionPrestateEvidenceError {}
