use super::{
    verify_arbiter_prestate_v2, ArbiterPrestateEvidenceError, VerifiedArbiterPrestate,
    MAX_ARBITER_PRESTATE_BYTES,
};
use layerx_proof::receipt::VerifiedReceipt;
use layerx_proof::state_range::{ModuleRangeWitness, RangeError, MAX_MODULE_LEAVES};
use layerx_proof::state_witness::{StateProofError, StateWitness};
use layerx_types::payload::{ActivityType, ModuleId, ModuleRegistration, ModuleRegistry};
use layerx_wire::activity::Activity;
use sha2::{Digest, Sha256};
use std::collections::BTreeMap;
const MAX_WITNESS_BYTES: usize = 35 + 129 + 1_048_576 + 96 * 32;
pub const MAX_ADMISSION_PRESTATE_BYTES: usize = MAX_ARBITER_PRESTATE_BYTES;
#[derive(Clone, Debug, Eq, PartialEq)]
pub enum AdmissionEvidenceError {
    Encoding,
    Bounds,
    Legacy(ArbiterPrestateEvidenceError),
    Activity,
    Root,
    Range(RangeError),
    Witness(StateProofError),
}
#[derive(Clone, Debug)]
pub struct VerifiedAdmissionPrestate {
    legacy: VerifiedArbiterPrestate,
    activity: Activity,
    asset_records: BTreeMap<Vec<u8>, Vec<u8>>,
    budget_records: BTreeMap<Vec<u8>, Vec<u8>>,
    governance_records: BTreeMap<Vec<u8>, Vec<u8>>,
    canonical_bytes: Vec<u8>,
    commitment: [u8; 32],
}
impl VerifiedAdmissionPrestate {
    pub const fn legacy(&self) -> &VerifiedArbiterPrestate {
        &self.legacy
    }
    pub const fn activity(&self) -> &Activity {
        &self.activity
    }
    pub fn asset_records(&self) -> &BTreeMap<Vec<u8>, Vec<u8>> {
        &self.asset_records
    }
    pub fn budget_records(&self) -> &BTreeMap<Vec<u8>, Vec<u8>> {
        &self.budget_records
    }
    pub fn governance_records(&self) -> &BTreeMap<Vec<u8>, Vec<u8>> {
        &self.governance_records
    }
    pub fn canonical_bytes(&self) -> &[u8] {
        &self.canonical_bytes
    }
    pub const fn commitment(&self) -> [u8; 32] {
        self.commitment
    }
    pub const fn network_id(&self) -> u32 {
        self.legacy.network_id()
    }
    pub const fn activity_id(&self) -> [u8; 32] {
        self.legacy.activity_id()
    }
    pub const fn execution_sequence(&self) -> u64 {
        self.legacy.execution_sequence()
    }
    pub const fn receipt_digest(&self) -> [u8; 32] {
        self.legacy.receipt_digest()
    }
    pub const fn state_root(&self) -> [u8; 32] {
        self.legacy.state_root()
    }
}
pub fn verify_arbiter_admission_v3(
    bytes: &[u8],
    anchor: &VerifiedReceipt,
    expected_network_id: u32,
) -> Result<VerifiedAdmissionPrestate, AdmissionEvidenceError> {
    verify_arbiter_admission_v3_bounded(
        bytes,
        anchor,
        expected_network_id,
        MAX_ADMISSION_PRESTATE_BYTES,
    )
}
pub fn verify_arbiter_admission_v3_bounded(
    bytes: &[u8],
    anchor: &VerifiedReceipt,
    expected_network_id: u32,
    maximum_bytes: usize,
) -> Result<VerifiedAdmissionPrestate, AdmissionEvidenceError> {
    if maximum_bytes > MAX_ADMISSION_PRESTATE_BYTES || bytes.len() > maximum_bytes {
        return Err(AdmissionEvidenceError::Bounds);
    }
    let mut reader = Reader(bytes);
    if reader.u16()? != 3 {
        return Err(AdmissionEvidenceError::Encoding);
    }
    let v2 = reader.bytes(MAX_ARBITER_PRESTATE_BYTES)?;
    let legacy = verify_arbiter_prestate_v2(v2, anchor, expected_network_id)
        .map_err(AdmissionEvidenceError::Legacy)?;
    let receipt = anchor
        .receipt()
        .protocol()
        .ok_or(AdmissionEvidenceError::Activity)?;
    let ordinal_count = match receipt.module_version() {
        1 => 5,
        2 => 8,
        3 => 9,
        4 => 10,
        _ => return Err(AdmissionEvidenceError::Activity),
    };
    let activities = (1..=ordinal_count)
        .map(|ordinal| {
            ActivityType::new(ModuleId::Programs, ordinal)
                .map_err(|_| AdmissionEvidenceError::Activity)
        })
        .collect::<Result<Vec<_>, _>>()?;
    let registration = ModuleRegistration::new(ModuleId::Programs, &activities)
        .map_err(|_| AdmissionEvidenceError::Activity)?;
    let registry =
        ModuleRegistry::new(&[registration]).map_err(|_| AdmissionEvidenceError::Activity)?;
    let activity_bytes = reader.bytes(layerx_wire::limits::MAX_MESSAGE_BYTES)?;
    let activity = layerx_wire::activity::decode_signed(activity_bytes, &registry)
        .map_err(|_| AdmissionEvidenceError::Activity)?;
    if layerx_wire::activity::encode_signed(&activity)
        .map_err(|_| AdmissionEvidenceError::Activity)?
        != activity_bytes
        || activity.protocol_version() != receipt.protocol_version()
        || activity.network_id() != expected_network_id
        || activity.activity_type().module() != ModuleId::Programs
        || activity.activity_type().ordinal() != u16::from(receipt.operation())
        || activity.actor_did().is_empty()
        || layerx_wire::hash::activity_id(&activity)
            .map_err(|_| AdmissionEvidenceError::Activity)?
            != receipt.activity_id()
        || layerx_wire::hash::payload_hash(&activity)
            .map_err(|_| AdmissionEvidenceError::Activity)?
            != activity.payload_hash()
    {
        return Err(AdmissionEvidenceError::Activity);
    }
    let key: [u8; 32] = activity
        .authority()
        .try_into()
        .map_err(|_| AdmissionEvidenceError::Activity)?;
    let signature: [u8; 64] = activity
        .signature()
        .ok_or(AdmissionEvidenceError::Activity)?
        .try_into()
        .map_err(|_| AdmissionEvidenceError::Activity)?;
    let unsigned = layerx_wire::activity::signing_bytes(&activity)
        .map_err(|_| AdmissionEvidenceError::Activity)?;
    let message = layerx_crypto::SignatureMessage::new(
        layerx_wire::hash::Domain::SignaturePreimage,
        activity.protocol_version(),
        activity.network_id(),
        unsigned.as_bytes(),
    )
    .map_err(|_| AdmissionEvidenceError::Activity)?;
    layerx_crypto::ed25519::verify(&key, &signature, message)
        .map_err(|_| AdmissionEvidenceError::Activity)?;
    let native = legacy.legacy().canonical_bytes();
    let mut inventory = Reader(native.get(78..).ok_or(AdmissionEvidenceError::Encoding)?);
    let composite_count = inventory.u16()?;
    let roots = (0..composite_count)
        .map(|_| inventory.array())
        .collect::<Result<Vec<[u8; 32]>, _>>()?;
    if reader.u16()? != 3 {
        return Err(AdmissionEvidenceError::Encoding);
    }
    let asset = reader.module(1)?;
    let budget = reader.module(3)?;
    let governance = reader.module(7)?;
    if !reader.0.is_empty() {
        return Err(AdmissionEvidenceError::Encoding);
    }
    let mut inventories = Vec::with_capacity(3);
    for module in [&asset, &budget, &governance] {
        if module.composite_index != u32::from(module.module_id)
            || module.composite_count != u32::from(composite_count)
            || roots.get(usize::from(module.module_id)) != Some(&module.subtree_root)
        {
            return Err(AdmissionEvidenceError::Root);
        }
        inventories.push(
            module
                .verify_full_module(legacy.state_root())
                .map_err(AdmissionEvidenceError::Range)?,
        );
    }
    let mut hasher = Sha256::new();
    hasher.update(b"LayerX/programs/arbiter-admission/v3\0");
    hasher.update(bytes);
    Ok(VerifiedAdmissionPrestate {
        legacy,
        activity,
        asset_records: inventories[0].records().iter().cloned().collect(),
        budget_records: inventories[1].records().iter().cloned().collect(),
        governance_records: inventories[2].records().iter().cloned().collect(),
        canonical_bytes: bytes.to_vec(),
        commitment: hasher.finalize().into(),
    })
}
struct Reader<'a>(&'a [u8]);
impl<'a> Reader<'a> {
    fn array<const N: usize>(&mut self) -> Result<[u8; N], AdmissionEvidenceError> {
        let value = self
            .0
            .get(..N)
            .ok_or(AdmissionEvidenceError::Encoding)?
            .try_into()
            .map_err(|_| AdmissionEvidenceError::Encoding)?;
        self.0 = &self.0[N..];
        Ok(value)
    }
    fn u8(&mut self) -> Result<u8, AdmissionEvidenceError> {
        Ok(self.array::<1>()?[0])
    }
    fn u16(&mut self) -> Result<u16, AdmissionEvidenceError> {
        Ok(u16::from_be_bytes(self.array()?))
    }
    fn u32(&mut self) -> Result<u32, AdmissionEvidenceError> {
        Ok(u32::from_be_bytes(self.array()?))
    }
    fn count(&mut self, maximum: usize) -> Result<usize, AdmissionEvidenceError> {
        let count = usize::try_from(self.u32()?).map_err(|_| AdmissionEvidenceError::Bounds)?;
        if count > maximum {
            return Err(AdmissionEvidenceError::Bounds);
        }
        Ok(count)
    }
    fn bytes(&mut self, maximum: usize) -> Result<&'a [u8], AdmissionEvidenceError> {
        let length = self.count(maximum)?;
        let value = self
            .0
            .get(..length)
            .ok_or(AdmissionEvidenceError::Encoding)?;
        self.0 = &self.0[length..];
        Ok(value)
    }
    fn module(&mut self, expected: u16) -> Result<ModuleRangeWitness, AdmissionEvidenceError> {
        let module_id = self.u16()?;
        if module_id != expected {
            return Err(AdmissionEvidenceError::Encoding);
        }
        let subtree_root = self.array()?;
        let composite_index = self.u32()?;
        let composite_count = self.u32()?;
        let depth = self.u8()?;
        if depth > 32 {
            return Err(AdmissionEvidenceError::Bounds);
        }
        let composite_siblings = (0..depth)
            .map(|_| self.array())
            .collect::<Result<Vec<_>, _>>()?;
        let count = self.count(MAX_MODULE_LEAVES)?;
        let mut leaves = Vec::with_capacity(count);
        for _ in 0..count {
            let encoded = self.bytes(MAX_WITNESS_BYTES)?;
            let witness = StateWitness::decode(encoded).map_err(AdmissionEvidenceError::Witness)?;
            if witness.encode().map_err(AdmissionEvidenceError::Witness)? != encoded {
                return Err(AdmissionEvidenceError::Encoding);
            }
            leaves.push(witness);
        }
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

impl std::fmt::Display for AdmissionEvidenceError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "{self:?}")
    }
}
impl std::error::Error for AdmissionEvidenceError {}
