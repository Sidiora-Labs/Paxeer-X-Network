//! Domain-separated Merkle construction and index-aware path verification.

use layerx_wire::hash::{merkle_internal, merkle_leaf};

/// Maximum proof depth published by the C17 protocol implementation.
pub const MAX_DEPTH: usize = 32;
const PROOF_VERSION: u8 = 1;
const PROOF_PREFIX_BYTES: usize = 10;

/// One canonical index-aware Merkle proof.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Proof {
    leaf_index: u32,
    leaf_count: u32,
    siblings: Vec<[u8; 32]>,
}

impl Proof {
    /// Constructs a proof only when its index and exact path depth are valid.
    ///
    /// # Errors
    ///
    /// Returns a typed structural failure for an empty tree, out-of-range
    /// index, excessive path, or path whose length does not match the tree.
    pub fn new(
        leaf_index: u32,
        leaf_count: u32,
        siblings: Vec<[u8; 32]>,
    ) -> Result<Self, MerkleError> {
        if leaf_count == 0 {
            return Err(MerkleError::EmptyTree);
        }
        if leaf_index >= leaf_count {
            return Err(MerkleError::LeafIndex {
                index: leaf_index,
                count: leaf_count,
            });
        }
        let expected = proof_depth(leaf_count);
        if siblings.len() > MAX_DEPTH || siblings.len() != expected {
            return Err(MerkleError::PathLength {
                expected,
                actual: siblings.len(),
            });
        }
        Ok(Self {
            leaf_index,
            leaf_count,
            siblings,
        })
    }

    /// Returns the committed leaf index.
    #[must_use]
    pub const fn leaf_index(&self) -> u32 {
        self.leaf_index
    }

    /// Returns the committed leaf count.
    #[must_use]
    pub const fn leaf_count(&self) -> u32 {
        self.leaf_count
    }

    /// Borrows the exact ordered sibling path.
    #[must_use]
    pub fn siblings(&self) -> &[[u8; 32]] {
        &self.siblings
    }
}

/// Exact failure class for Merkle construction or verification.
#[derive(Clone, Debug, Eq, PartialEq)]
pub enum MerkleError {
    /// Canonical proof bytes had an unknown version, wrong length or trailing data.
    Encoding,
    /// Inclusion proofs cannot target an empty tree.
    EmptyTree,
    /// The proof index lies outside the committed tree.
    LeafIndex { index: u32, count: u32 },
    /// The path does not have the unique depth implied by the leaf count.
    PathLength { expected: usize, actual: usize },
    /// An odd-node promotion supplied anything except the node itself.
    PromotionSibling { level: usize },
    /// The recomputed root differs from the claimed commitment.
    RootMismatch,
    /// The tree is too large for the protocol proof representation.
    TreeTooLarge,
    /// Domain-separated hashing rejected the input length.
    Hash,
}

/// Encodes one proof into the bounded canonical representation used by public
/// evidence exchange.
#[must_use]
pub fn encode_proof(proof: &Proof) -> Vec<u8> {
    let mut bytes = Vec::with_capacity(
        PROOF_PREFIX_BYTES.saturating_add(proof.siblings.len().saturating_mul(32)),
    );
    bytes.push(PROOF_VERSION);
    bytes.extend_from_slice(&proof.leaf_index.to_be_bytes());
    bytes.extend_from_slice(&proof.leaf_count.to_be_bytes());
    bytes.push(u8::try_from(proof.siblings.len()).unwrap_or(u8::MAX));
    for sibling in &proof.siblings {
        bytes.extend_from_slice(sibling);
    }
    bytes
}

/// Decodes the bounded canonical public proof representation.
///
/// # Errors
///
/// Refuses unknown versions, excessive paths, structural mismatches, truncated
/// fields and trailing bytes.
pub fn decode_proof(bytes: &[u8]) -> Result<Proof, MerkleError> {
    if bytes.len() < PROOF_PREFIX_BYTES || bytes[0] != PROOF_VERSION {
        return Err(MerkleError::Encoding);
    }
    let leaf_index = u32::from_be_bytes(bytes[1..5].try_into().map_err(|_| MerkleError::Encoding)?);
    let leaf_count = u32::from_be_bytes(bytes[5..9].try_into().map_err(|_| MerkleError::Encoding)?);
    let sibling_count = usize::from(bytes[9]);
    let expected = PROOF_PREFIX_BYTES
        .checked_add(sibling_count.checked_mul(32).ok_or(MerkleError::Encoding)?)
        .ok_or(MerkleError::Encoding)?;
    if sibling_count > MAX_DEPTH || bytes.len() != expected {
        return Err(MerkleError::Encoding);
    }
    let mut siblings = Vec::with_capacity(sibling_count);
    for chunk in bytes[PROOF_PREFIX_BYTES..].chunks_exact(32) {
        siblings.push(chunk.try_into().map_err(|_| MerkleError::Encoding)?);
    }
    Proof::new(leaf_index, leaf_count, siblings)
}

fn proof_depth(mut count: u32) -> usize {
    let mut depth = 0;
    while count > 1 {
        count = count.div_ceil(2);
        depth += 1;
    }
    depth
}

/// Hashes canonical leaf bytes under the exact protocol leaf domain.
///
/// # Errors
///
/// Returns [`MerkleError::Hash`] if the byte length cannot be hashed safely.
pub fn leaf_hash(bytes: &[u8]) -> Result<[u8; 32], MerkleError> {
    merkle_leaf(bytes).map_err(|_| MerkleError::Hash)
}

/// Hashes two child digests under the exact protocol internal-node domain.
///
/// # Errors
///
/// Returns [`MerkleError::Hash`] if hashing cannot be completed.
pub fn node_hash(left: &[u8; 32], right: &[u8; 32]) -> Result<[u8; 32], MerkleError> {
    merkle_internal(left, right).map_err(|_| MerkleError::Hash)
}

/// Computes the protocol root, duplicating an unpaired rightmost node.
///
/// The empty-tree root is the leaf-domain hash of the empty byte string,
/// matching `lxp_merkle_build`.
///
/// # Errors
///
/// Returns a typed hashing failure.
pub fn root(leaves: &[&[u8]]) -> Result<[u8; 32], MerkleError> {
    if leaves.is_empty() {
        return leaf_hash(&[]);
    }
    let mut level = leaves
        .iter()
        .map(|leaf| leaf_hash(leaf))
        .collect::<Result<Vec<_>, _>>()?;
    reduce(&mut level)
}

/// Computes a protocol Merkle root from already domain-separated leaf hashes.
///
/// # Errors
///
/// Returns an empty-tree or hashing failure. Empty pre-hashed trees are
/// rejected because their leaf domain cannot be inferred.
pub fn root_from_leaf_hashes(leaves: &[[u8; 32]]) -> Result<[u8; 32], MerkleError> {
    if leaves.is_empty() {
        return Err(MerkleError::EmptyTree);
    }
    let mut level = leaves.to_vec();
    reduce(&mut level)
}

fn reduce(level: &mut Vec<[u8; 32]>) -> Result<[u8; 32], MerkleError> {
    while level.len() > 1 {
        let next_len = level.len().div_ceil(2);
        for index in 0..next_len {
            let left = level[index * 2];
            let right = level.get(index * 2 + 1).copied().unwrap_or(left);
            level[index] = node_hash(&left, &right)?;
        }
        level.truncate(next_len);
    }
    level.first().copied().ok_or(MerkleError::EmptyTree)
}

/// Builds the canonical proof for one leaf and returns the same tree root.
///
/// # Errors
///
/// Returns a typed failure for an empty or over-large tree, an out-of-range
/// index, or hashing failure.
pub fn build_proof(leaves: &[&[u8]], leaf_index: usize) -> Result<(Proof, [u8; 32]), MerkleError> {
    let leaf_count = u32::try_from(leaves.len()).map_err(|_| MerkleError::TreeTooLarge)?;
    let proof_index = u32::try_from(leaf_index).map_err(|_| MerkleError::TreeTooLarge)?;
    if leaf_count == 0 {
        return Err(MerkleError::EmptyTree);
    }
    if proof_index >= leaf_count {
        return Err(MerkleError::LeafIndex {
            index: proof_index,
            count: leaf_count,
        });
    }
    let mut level = leaves
        .iter()
        .map(|leaf| leaf_hash(leaf))
        .collect::<Result<Vec<_>, _>>()?;
    let mut index = leaf_index;
    let mut siblings = Vec::with_capacity(proof_depth(leaf_count));
    while level.len() > 1 {
        let sibling = index ^ 1;
        siblings.push(level.get(sibling).copied().unwrap_or(level[index]));
        let next_len = level.len().div_ceil(2);
        for node in 0..next_len {
            let left = level[node * 2];
            let right = level.get(node * 2 + 1).copied().unwrap_or(left);
            level[node] = node_hash(&left, &right)?;
        }
        level.truncate(next_len);
        index /= 2;
    }
    let proof = Proof::new(proof_index, leaf_count, siblings)?;
    let tree_root = level.first().copied().ok_or(MerkleError::EmptyTree)?;
    Ok((proof, tree_root))
}

/// Builds an index-aware proof over already domain-separated leaf hashes.
///
/// # Errors
///
/// Returns a typed failure for an empty or over-large tree, an out-of-range
/// index, or hashing failure.
pub fn build_leaf_hash_proof(
    leaf_hashes: &[[u8; 32]],
    leaf_index: usize,
) -> Result<(Proof, [u8; 32]), MerkleError> {
    let leaf_count = u32::try_from(leaf_hashes.len()).map_err(|_| MerkleError::TreeTooLarge)?;
    let proof_index = u32::try_from(leaf_index).map_err(|_| MerkleError::TreeTooLarge)?;
    if leaf_count == 0 {
        return Err(MerkleError::EmptyTree);
    }
    if proof_index >= leaf_count {
        return Err(MerkleError::LeafIndex {
            index: proof_index,
            count: leaf_count,
        });
    }
    let mut level = leaf_hashes.to_vec();
    let mut index = leaf_index;
    let mut siblings = Vec::with_capacity(proof_depth(leaf_count));
    while level.len() > 1 {
        let sibling = index ^ 1;
        siblings.push(level.get(sibling).copied().unwrap_or(level[index]));
        let next_len = level.len().div_ceil(2);
        for node in 0..next_len {
            let left = level[node * 2];
            let right = level.get(node * 2 + 1).copied().unwrap_or(left);
            level[node] = node_hash(&left, &right)?;
        }
        level.truncate(next_len);
        index /= 2;
    }
    let proof = Proof::new(proof_index, leaf_count, siblings)?;
    let tree_root = level.first().copied().ok_or(MerkleError::EmptyTree)?;
    Ok((proof, tree_root))
}

/// Recomputes and checks a root from canonical leaf bytes and an index path.
///
/// # Errors
///
/// Rejects inconsistent tree geometry, invalid odd-node promotion, hashing
/// failure, and a root mismatch with distinct error classes.
pub fn verify_path(
    leaf: &[u8],
    proof: &Proof,
    expected_root: &[u8; 32],
) -> Result<(), MerkleError> {
    let current = leaf_hash(leaf)?;
    verify_leaf_hash(&current, proof, expected_root)
}

/// Verifies a path when the caller already holds the leaf-domain digest.
///
/// # Errors
///
/// Returns the same exact structural and root failures as [`verify_path`].
pub fn verify_leaf_hash(
    leaf: &[u8; 32],
    proof: &Proof,
    expected_root: &[u8; 32],
) -> Result<(), MerkleError> {
    let expected_depth = proof_depth(proof.leaf_count);
    if proof.siblings.len() != expected_depth || proof.siblings.len() > MAX_DEPTH {
        return Err(MerkleError::PathLength {
            expected: expected_depth,
            actual: proof.siblings.len(),
        });
    }
    if proof.leaf_index >= proof.leaf_count {
        return Err(MerkleError::LeafIndex {
            index: proof.leaf_index,
            count: proof.leaf_count,
        });
    }
    let mut current = *leaf;
    let mut index = proof.leaf_index;
    let mut level_count = proof.leaf_count;
    for (level, sibling_hash) in proof.siblings.iter().enumerate() {
        let sibling_index = index ^ 1;
        if sibling_index >= level_count && sibling_hash != &current {
            return Err(MerkleError::PromotionSibling { level });
        }
        current = if index & 1 == 0 {
            node_hash(&current, sibling_hash)?
        } else {
            node_hash(sibling_hash, &current)?
        };
        index /= 2;
        level_count = level_count.div_ceil(2);
    }
    if &current == expected_root {
        Ok(())
    } else {
        Err(MerkleError::RootMismatch)
    }
}
