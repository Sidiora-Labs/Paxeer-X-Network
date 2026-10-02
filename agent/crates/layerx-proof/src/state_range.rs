use layerx_types::payload::ModuleId;
use sha2::{Digest, Sha256};

use crate::state_witness::{StateProofError, StateWitness};

const LEAF: &[u8] = b"LXP/v1/state-leaf\0";
const NODE: &[u8] = b"LXP/v1/state-node\0";
pub const MAX_MODULE_LEAVES: usize = 1024;
pub const MAX_UNIVERSAL_LEAVES: usize = 1058;
pub const MAX_ACCOUNT_LEAVES: usize = 4096;

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ModuleRangeWitness {
    pub module_id: u16,
    pub subtree_root: [u8; 32],
    pub composite_index: u32,
    pub composite_count: u32,
    pub composite_siblings: Vec<[u8; 32]>,
    pub leaves: Vec<StateWitness>,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct VerifiedPrefix {
    module_id: u16,
    state_root: [u8; 32],
    records: Vec<(Vec<u8>, Vec<u8>)>,
}

impl VerifiedPrefix {
    pub const fn module_id(&self) -> u16 { self.module_id }
    pub const fn state_root(&self) -> [u8; 32] { self.state_root }
    pub fn records(&self) -> &[(Vec<u8>, Vec<u8>)] { &self.records }
    pub fn is_empty(&self) -> bool { self.records.is_empty() }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum RangeError {
    Bounds,
    Module,
    Order,
    Position,
    Root,
    Witness(StateProofError),
}

impl ModuleRangeWitness {
    pub fn verify_prefix(&self, state_root: [u8; 32], prefix: &[u8]) -> Result<VerifiedPrefix, RangeError> {
        if prefix.is_empty() || prefix.len() > 129 || self.leaves.len() > (if self.module_id == 0 { MAX_UNIVERSAL_LEAVES } else { MAX_MODULE_LEAVES }) {
            return Err(RangeError::Bounds);
        }
        let maximum = ModuleId::ALL[ModuleId::ALL.len() - 1] as u32 + 1;
        if (self.module_id != 0 && ModuleId::from_u16(self.module_id).is_err())
            || self.composite_index != u32::from(self.module_id)
            || !(ModuleId::Bridge as u32 + 1..=maximum).contains(&self.composite_count)
        { return Err(RangeError::Module); }
        let mut hashes = Vec::with_capacity(self.leaves.len());
        let count = u32::try_from(self.leaves.len()).map_err(|_| RangeError::Bounds)?;
        let mut previous: Option<&[u8]> = None;
        let mut records = Vec::new();
        for (index, witness) in self.leaves.iter().enumerate() {
            if witness.module_id != self.module_id || witness.account_path.is_some() {
                return Err(RangeError::Module);
            }
            if previous.is_some_and(|key| key >= witness.key.as_slice()) {
                return Err(RangeError::Order);
            }
            if witness.leaf_count_a != count
                || witness.leaf_index_a != u32::try_from(index).map_err(|_| RangeError::Bounds)?
                || witness.leaf_count_b != self.composite_count
                || witness.siblings_b != self.composite_siblings
            { return Err(RangeError::Position); }
            witness.verify(state_root).map_err(RangeError::Witness)?;
            hashes.push(leaf(&witness.key, &witness.value)?);
            previous = Some(&witness.key);
            if witness.key.starts_with(prefix) {
                records.push((witness.key.clone(), witness.value.clone()));
            }
        }
        let rebuilt = if hashes.is_empty() {
            hash(&[LEAF])
        } else {
            while hashes.len() > 1 {
                let mut next = Vec::with_capacity(hashes.len().div_ceil(2));
                for pair in hashes.chunks(2) {
                    next.push(hash(&[NODE, &pair[0], pair.get(1).unwrap_or(&pair[0])]));
                }
                hashes = next;
            }
            hashes[0]
        };
        if rebuilt != self.subtree_root { return Err(RangeError::Root); }
        let root = fold(leaf(&self.module_id.to_be_bytes(), &rebuilt)?, self.composite_index,
            self.composite_count, &self.composite_siblings)?;
        if root != state_root { return Err(RangeError::Root); }
        Ok(VerifiedPrefix { module_id: self.module_id, state_root, records })
    }
}

pub fn verify_account_tree(witnesses: &[StateWitness], universal: &ModuleRangeWitness,
    composite_root: [u8; 32]) -> Result<(), RangeError> {
    if witnesses.len() > MAX_ACCOUNT_LEAVES || universal.module_id != 0 {
        return Err(RangeError::Bounds);
    }
    universal.verify_prefix(composite_root, b"account-tree")?;
    let anchor = universal.leaves.iter().find(|leaf| leaf.key == b"account-tree").ok_or(RangeError::Root)?;
    if anchor.value.len() != 32 { return Err(RangeError::Root); }
    let count = u32::try_from(witnesses.len()).map_err(|_| RangeError::Bounds)?;
    let mut previous: Option<&[u8]> = None;
    let mut hashes = Vec::with_capacity(witnesses.len());
    for (index, witness) in witnesses.iter().enumerate() {
        let path = witness.account_path.as_ref().ok_or(RangeError::Position)?;
        if witness.module_id != 0 || witness.key.len() != 33 || witness.key[0] != 4
            || path.index != u32::try_from(index).map_err(|_| RangeError::Bounds)? || path.count != count
            || witness.leaf_count_a != anchor.leaf_count_a || witness.leaf_index_a != anchor.leaf_index_a
            || witness.siblings_a != anchor.siblings_a || witness.leaf_count_b != anchor.leaf_count_b
            || witness.siblings_b != anchor.siblings_b
        { return Err(RangeError::Position); }
        if previous.is_some_and(|key| key >= witness.key.as_slice()) { return Err(RangeError::Order); }
        witness.verify(composite_root).map_err(RangeError::Witness)?;
        hashes.push(leaf(&witness.key, &witness.value)?); previous = Some(&witness.key);
    }
    let root = if hashes.is_empty() { hash(&[LEAF]) } else {
        while hashes.len() > 1 {
            hashes = hashes.chunks(2).map(|pair| hash(&[NODE, &pair[0], pair.get(1).unwrap_or(&pair[0])])).collect();
        }
        hashes[0]
    };
    if root.as_slice() != anchor.value.as_slice() { return Err(RangeError::Root); }
    Ok(())
}

pub fn verify_composite_roots(roots: &[[u8; 32]], expected_root: [u8; 32]) -> Result<(), RangeError> {
    let maximum = ModuleId::ALL[ModuleId::ALL.len() - 1] as usize + 1;
    if !(ModuleId::Bridge as usize + 1..=maximum).contains(&roots.len()) {
        return Err(RangeError::Module);
    }
    let mut hashes = roots.iter().enumerate().map(|(index, root)| {
        let module = u16::try_from(index).map_err(|_| RangeError::Module)?;
        leaf(&module.to_be_bytes(), root)
    }).collect::<Result<Vec<_>, _>>()?;
    while hashes.len() > 1 {
        hashes = hashes.chunks(2).map(|pair| hash(&[NODE, &pair[0], pair.get(1).unwrap_or(&pair[0])])).collect();
    }
    if hashes.first() != Some(&expected_root) { return Err(RangeError::Root); }
    Ok(())
}

fn leaf(key: &[u8], value: &[u8]) -> Result<[u8; 32], RangeError> {
    let key_length = u32::try_from(key.len()).map_err(|_| RangeError::Bounds)?;
    let value_length = u32::try_from(value.len()).map_err(|_| RangeError::Bounds)?;
    Ok(hash(&[LEAF, &key_length.to_be_bytes(), &value_length.to_be_bytes(), key, value]))
}

fn hash(parts: &[&[u8]]) -> [u8; 32] {
    let mut hasher = Sha256::new();
    for part in parts { hasher.update(part); }
    hasher.finalize().into()
}

fn fold(mut node: [u8; 32], mut index: u32, mut count: u32, siblings: &[[u8; 32]]) -> Result<[u8; 32], RangeError> {
    if count == 0 || index >= count || siblings.len() > 32 { return Err(RangeError::Position); }
    for sibling in siblings {
        if count <= 1 || ((index ^ 1) >= count && sibling != &node) { return Err(RangeError::Position); }
        node = if index & 1 == 0 { hash(&[NODE, &node, sibling]) } else { hash(&[NODE, sibling, &node]) };
        index /= 2;
        count = count.div_ceil(2);
    }
    if count != 1 { return Err(RangeError::Position); }
    Ok(node)
}

impl std::fmt::Display for RangeError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result { write!(f, "{self:?}") }
}
impl std::error::Error for RangeError {}
