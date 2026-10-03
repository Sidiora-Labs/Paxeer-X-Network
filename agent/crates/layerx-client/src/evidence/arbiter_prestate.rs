use std::collections::BTreeMap;

use layerx_proof::receipt::VerifiedReceipt;
use layerx_proof::state_range::{ModuleRangeWitness, RangeError, MAX_MODULE_LEAVES};
use layerx_proof::state_witness::{StateProofError, StateWitness};
use sha2::{Digest, Sha256};

use super::execution_prestate::{
    verify_native_execution_prestate_object, ExecutionPrestateEvidenceError,
    VerifiedNativeExecutionPrestate,
};

pub const MAX_ARBITER_PRESTATE_BYTES: usize = 64 * 1024 * 1024;
const MAX_WITNESS_BYTES: usize = 35 + 129 + 1_048_576 + 96 * 32;
const COMMITMENT_DOMAIN: &[u8] = b"LayerX/programs/arbiter-prestate/v2\0";

#[derive(Clone, Debug, Eq, PartialEq)]
pub enum ArbiterPrestateEvidenceError {
    Encoding,
    Bounds,
    Legacy(ExecutionPrestateEvidenceError),
    Root,
    Range(RangeError),
    Witness(StateProofError),
}

#[derive(Clone, Debug)]
pub struct VerifiedArbiterPrestate {
    legacy: VerifiedNativeExecutionPrestate,
    perps_records: BTreeMap<Vec<u8>, Vec<u8>>,
    web_records: BTreeMap<Vec<u8>, Vec<u8>>,
    canonical_bytes: Vec<u8>,
    commitment: [u8; 32],
}

impl VerifiedArbiterPrestate {
    pub const fn network_id(&self) -> u32 {
        self.legacy.network_id()
    }
    pub const fn activity_id(&self) -> [u8; 32] {
        self.legacy.activity_id()
    }
    pub const fn receipt_digest(&self) -> [u8; 32] {
        self.legacy.receipt_digest()
    }
    pub const fn execution_sequence(&self) -> u64 {
        self.legacy.execution_sequence()
    }
    pub const fn state_root(&self) -> [u8; 32] {
        self.legacy.state_root()
    }
    pub const fn legacy(&self) -> &VerifiedNativeExecutionPrestate {
        &self.legacy
    }
    pub fn perps_records(&self) -> &BTreeMap<Vec<u8>, Vec<u8>> {
        &self.perps_records
    }
    pub fn web_records(&self) -> &BTreeMap<Vec<u8>, Vec<u8>> {
        &self.web_records
    }
    pub fn canonical_bytes(&self) -> &[u8] {
        &self.canonical_bytes
    }
    pub const fn commitment(&self) -> [u8; 32] {
        self.commitment
    }
}

pub fn verify_arbiter_prestate_v2(
    bytes: &[u8],
    anchor: &VerifiedReceipt,
    expected_network_id: u32,
) -> Result<VerifiedArbiterPrestate, ArbiterPrestateEvidenceError> {
    verify_arbiter_prestate_v2_bounded(
        bytes,
        anchor,
        expected_network_id,
        MAX_ARBITER_PRESTATE_BYTES,
    )
}

pub fn verify_arbiter_prestate_v2_bounded(
    bytes: &[u8],
    anchor: &VerifiedReceipt,
    expected_network_id: u32,
    maximum_bytes: usize,
) -> Result<VerifiedArbiterPrestate, ArbiterPrestateEvidenceError> {
    if maximum_bytes > MAX_ARBITER_PRESTATE_BYTES || bytes.len() > maximum_bytes {
        return Err(ArbiterPrestateEvidenceError::Bounds);
    }
    let mut reader = Reader(bytes);
    if reader.u16()? != 2 {
        return Err(ArbiterPrestateEvidenceError::Encoding);
    }
    let legacy_bytes = reader.bytes(MAX_ARBITER_PRESTATE_BYTES)?;
    let legacy = verify_native_execution_prestate_object(legacy_bytes, anchor, expected_network_id)
        .map_err(ArbiterPrestateEvidenceError::Legacy)?;
    let mut inventory = Reader(
        legacy_bytes
            .get(78..)
            .ok_or(ArbiterPrestateEvidenceError::Encoding)?,
    );
    let composite_count = inventory.u16()?;
    let roots = (0..composite_count)
        .map(|_| inventory.array())
        .collect::<Result<Vec<[u8; 32]>, _>>()?;
    if roots.get(11).is_none() {
        return Err(ArbiterPrestateEvidenceError::Root);
    }
    if reader.u16()? != 2 {
        return Err(ArbiterPrestateEvidenceError::Encoding);
    }
    let perps = reader.module(6)?;
    let web = reader.module(11)?;
    if !reader.0.is_empty() {
        return Err(ArbiterPrestateEvidenceError::Encoding);
    }
    for module in [&perps, &web] {
        if module.composite_index != u32::from(module.module_id)
            || module.composite_count != u32::from(composite_count)
            || roots.get(usize::from(module.module_id)) != Some(&module.subtree_root)
        {
            return Err(ArbiterPrestateEvidenceError::Root);
        }
        module
            .verify_prefix(
                legacy.state_root(),
                if module.module_id == 6 {
                    b"oracle:"
                } else {
                    b"web/answer"
                },
            )
            .map_err(ArbiterPrestateEvidenceError::Range)?;
    }
    let perps_records = perps
        .leaves
        .into_iter()
        .map(|leaf| (leaf.key, leaf.value))
        .collect();
    let web_records = web
        .leaves
        .into_iter()
        .map(|leaf| (leaf.key, leaf.value))
        .collect();
    let mut hasher = Sha256::new();
    hasher.update(COMMITMENT_DOMAIN);
    hasher.update(bytes);
    Ok(VerifiedArbiterPrestate {
        legacy,
        perps_records,
        web_records,
        canonical_bytes: bytes.to_vec(),
        commitment: hasher.finalize().into(),
    })
}

struct Reader<'a>(&'a [u8]);
impl<'a> Reader<'a> {
    fn array<const N: usize>(&mut self) -> Result<[u8; N], ArbiterPrestateEvidenceError> {
        let value = self
            .0
            .get(..N)
            .ok_or(ArbiterPrestateEvidenceError::Encoding)?
            .try_into()
            .map_err(|_| ArbiterPrestateEvidenceError::Encoding)?;
        self.0 = &self.0[N..];
        Ok(value)
    }
    fn u8(&mut self) -> Result<u8, ArbiterPrestateEvidenceError> {
        Ok(self.array::<1>()?[0])
    }
    fn u16(&mut self) -> Result<u16, ArbiterPrestateEvidenceError> {
        Ok(u16::from_be_bytes(self.array()?))
    }
    fn u32(&mut self) -> Result<u32, ArbiterPrestateEvidenceError> {
        Ok(u32::from_be_bytes(self.array()?))
    }
    fn count(&mut self, maximum: usize) -> Result<usize, ArbiterPrestateEvidenceError> {
        let count =
            usize::try_from(self.u32()?).map_err(|_| ArbiterPrestateEvidenceError::Bounds)?;
        if count > maximum {
            return Err(ArbiterPrestateEvidenceError::Bounds);
        }
        Ok(count)
    }
    fn bytes(&mut self, maximum: usize) -> Result<&'a [u8], ArbiterPrestateEvidenceError> {
        let length = self.count(maximum)?;
        let value = self
            .0
            .get(..length)
            .ok_or(ArbiterPrestateEvidenceError::Encoding)?;
        self.0 = &self.0[length..];
        Ok(value)
    }
    fn module(
        &mut self,
        expected: u16,
    ) -> Result<ModuleRangeWitness, ArbiterPrestateEvidenceError> {
        let module_id = self.u16()?;
        if module_id != expected {
            return Err(ArbiterPrestateEvidenceError::Encoding);
        }
        let subtree_root = self.array()?;
        let composite_index = self.u32()?;
        let composite_count = self.u32()?;
        let depth = self.u8()?;
        if depth > 32 {
            return Err(ArbiterPrestateEvidenceError::Bounds);
        }
        let composite_siblings = (0..depth)
            .map(|_| self.array())
            .collect::<Result<Vec<_>, _>>()?;
        let count = self.count(MAX_MODULE_LEAVES)?;
        let mut leaves = Vec::with_capacity(count);
        for _ in 0..count {
            let encoded = self.bytes(MAX_WITNESS_BYTES)?;
            let witness =
                StateWitness::decode(encoded).map_err(ArbiterPrestateEvidenceError::Witness)?;
            if witness
                .encode()
                .map_err(ArbiterPrestateEvidenceError::Witness)?
                != encoded
            {
                return Err(ArbiterPrestateEvidenceError::Encoding);
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

impl std::fmt::Display for ArbiterPrestateEvidenceError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "{self:?}")
    }
}
impl std::error::Error for ArbiterPrestateEvidenceError {}
