//! Canonical prefix membership, nonmembership and completeness over native module state.

use layerx_types::payload::ModuleId;
use sha2::{Digest as _, Sha256};

use crate::state_witness::{StateProofError, StateWitness};

const LEAF: &[u8] = b"LXP/v1/state-leaf\0";
const NODE: &[u8] = b"LXP/v1/state-node\0";
const MAX_KEY: usize = 129;
const MAX_DEPTH: usize = 32;
const MIN_COMPOSITE_LEAVES: u32 = ModuleId::Bridge as u32 + 1;
const MAX_COMPOSITE_LEAVES: u32 = ModuleId::ALL[ModuleId::ALL.len() - 1] as u32 + 1;

/// Exact failure class for prefix range verification.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum StateRangeError {
    Proof(StateProofError),
    Order,
    Gap,
    Duplicate,
    Boundary,
    Count,
    Padding,
    EmptyModule,
    Module,
}

/// Every leaf of one module whose key starts with `prefix`, in canonical order.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct PrefixRange {
    pub module_id: u16,
    pub prefix: Vec<u8>,
    pub module_leaf_count: u32,
    pub items: Vec<StateWitness>,
}

/// Authenticated lower or upper end of a prefix range.
#[derive(Clone, Debug, Eq, PartialEq)]
pub enum PrefixBound {
    /// The canonical tree edge: leaf index 0 (lower) or `module_leaf_count - 1` (upper).
    Edge,
    /// The immediately adjacent leaf outside the prefix.
    Witness(StateWitness),
    /// Composite inclusion of the canonical empty module root.
    EmptyModule(EmptyModuleProof),
}

/// Composite-tree path proving a module subtree is the canonical empty root.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct EmptyModuleProof {
    pub module_id: u16,
    pub composite_leaf_count: u32,
    pub siblings: Vec<[u8; 32]>,
}

impl EmptyModuleProof {
    /// Recomputes the composite state root committing an empty module subtree.
    ///
    /// # Errors
    /// Refuses unknown or out-of-range modules and noncanonical composite paths.
    pub fn root(&self) -> Result<[u8; 32], StateRangeError> {
        if module_id(self.module_id).is_err()
            || !(MIN_COMPOSITE_LEAVES..=MAX_COMPOSITE_LEAVES).contains(&self.composite_leaf_count)
            || u32::from(self.module_id) >= self.composite_leaf_count
        {
            return Err(StateRangeError::Module);
        }
        let module_leaf = leaf(&self.module_id.to_be_bytes(), &empty_module_root());
        fold(
            module_leaf,
            u32::from(self.module_id),
            self.composite_leaf_count,
            &self.siblings,
        )
        .ok_or(StateRangeError::EmptyModule)
    }
}

/// The subtree root of a module without leaves, as committed by the native core.
#[must_use]
pub fn empty_module_root() -> [u8; 32] {
    hash(&[LEAF])
}

/// Verifies that `range.items` is exactly the complete, ordered set of prefix leaves.
///
/// # Errors
/// Refuses witnesses at another root or module, missing, duplicate, reordered or
/// padded leaves, unauthenticated or mispositioned boundaries, inconsistent leaf
/// counts and empty-module claims without composite inclusion of the empty root.
pub fn verify_prefix_range(
    state_root: [u8; 32],
    range: &PrefixRange,
    lower: &PrefixBound,
    upper: &PrefixBound,
) -> Result<(), StateRangeError> {
    module_id(range.module_id)?;
    if range.prefix.is_empty() || range.prefix.len() > MAX_KEY {
        return Err(StateRangeError::Boundary);
    }
    match (lower, upper) {
        (PrefixBound::EmptyModule(low), PrefixBound::EmptyModule(high)) => {
            verify_empty_module(state_root, range, low, high)
        }
        (PrefixBound::EmptyModule(_), _) | (_, PrefixBound::EmptyModule(_)) => {
            Err(StateRangeError::EmptyModule)
        }
        _ => verify_populated_module(state_root, range, lower, upper),
    }
}

fn verify_empty_module(
    state_root: [u8; 32],
    range: &PrefixRange,
    lower: &EmptyModuleProof,
    upper: &EmptyModuleProof,
) -> Result<(), StateRangeError> {
    if lower != upper
        || lower.module_id != range.module_id
        || range.module_leaf_count != 0
        || !range.items.is_empty()
    {
        return Err(StateRangeError::EmptyModule);
    }
    if lower.root()? == state_root {
        Ok(())
    } else {
        Err(StateRangeError::EmptyModule)
    }
}

struct Walk<'a> {
    state_root: [u8; 32],
    range: &'a PrefixRange,
    composite_count: Option<u32>,
    next_index: u32,
    previous: Option<&'a StateWitness>,
}

impl<'a> Walk<'a> {
    fn check(&mut self, witness: &'a StateWitness) -> Result<(), StateRangeError> {
        if witness.module_id != self.range.module_id || witness.account_path.is_some() {
            return Err(StateRangeError::Module);
        }
        if witness.leaf_count_a != self.range.module_leaf_count {
            return Err(StateRangeError::Count);
        }
        if *self.composite_count.get_or_insert(witness.leaf_count_b) != witness.leaf_count_b {
            return Err(StateRangeError::Count);
        }
        witness
            .verify(self.state_root)
            .map_err(StateRangeError::Proof)?;
        if let Some(previous) = self.previous {
            if previous.key == witness.key {
                return Err(if previous.value == witness.value {
                    StateRangeError::Padding
                } else {
                    StateRangeError::Duplicate
                });
            }
            if previous.key > witness.key {
                return Err(StateRangeError::Order);
            }
        }
        match witness.leaf_index_a.cmp(&self.next_index) {
            std::cmp::Ordering::Less => return Err(StateRangeError::Duplicate),
            std::cmp::Ordering::Greater => return Err(StateRangeError::Gap),
            std::cmp::Ordering::Equal => {}
        }
        self.next_index = self
            .next_index
            .checked_add(1)
            .ok_or(StateRangeError::Count)?;
        self.previous = Some(witness);
        Ok(())
    }
}

fn verify_populated_module(
    state_root: [u8; 32],
    range: &PrefixRange,
    lower: &PrefixBound,
    upper: &PrefixBound,
) -> Result<(), StateRangeError> {
    if range.module_leaf_count == 0
        || u32::try_from(range.items.len())
            .ok()
            .is_none_or(|len| len > range.module_leaf_count)
    {
        return Err(StateRangeError::Count);
    }
    let mut walk = Walk {
        state_root,
        range,
        composite_count: None,
        next_index: 0,
        previous: None,
    };
    if let PrefixBound::Witness(witness) = lower {
        if witness.key.as_slice() >= range.prefix.as_slice() {
            return Err(StateRangeError::Boundary);
        }
        walk.next_index = witness.leaf_index_a;
        walk.check(witness)?;
    }
    for item in &range.items {
        if !item.key.starts_with(&range.prefix) {
            return Err(StateRangeError::Boundary);
        }
        walk.check(item)?;
    }
    match upper {
        PrefixBound::Witness(witness) => {
            if witness.key.starts_with(&range.prefix) || witness.key <= range.prefix {
                return Err(StateRangeError::Boundary);
            }
            walk.check(witness)?;
        }
        PrefixBound::Edge if walk.next_index != range.module_leaf_count => {
            return Err(StateRangeError::Gap);
        }
        PrefixBound::Edge | PrefixBound::EmptyModule(_) => {}
    }
    if walk.previous.is_none() {
        return Err(StateRangeError::Count);
    }
    Ok(())
}

fn module_id(value: u16) -> Result<ModuleId, StateRangeError> {
    if value == 0 {
        return Err(StateRangeError::Module);
    }
    ModuleId::from_u16(value).map_err(|_| StateRangeError::Module)
}

fn leaf(key: &[u8], value: &[u8]) -> [u8; 32] {
    let key_len = u32::try_from(key.len()).unwrap_or(u32::MAX);
    let value_len = u32::try_from(value.len()).unwrap_or(u32::MAX);
    hash(&[
        LEAF,
        &key_len.to_be_bytes(),
        &value_len.to_be_bytes(),
        key,
        value,
    ])
}

fn hash(parts: &[&[u8]]) -> [u8; 32] {
    let mut hasher = Sha256::new();
    for part in parts {
        hasher.update(part);
    }
    hasher.finalize().into()
}

fn fold(
    mut node: [u8; 32],
    mut index: u32,
    mut count: u32,
    siblings: &[[u8; 32]],
) -> Option<[u8; 32]> {
    if count == 0 || index >= count || siblings.len() > MAX_DEPTH {
        return None;
    }
    for sibling in siblings {
        if count <= 1 || ((index ^ 1) >= count && sibling != &node) {
            return None;
        }
        node = if index & 1 == 0 {
            hash(&[NODE, &node, sibling])
        } else {
            hash(&[NODE, sibling, &node])
        };
        index /= 2;
        count = count.div_ceil(2);
    }
    (count == 1).then_some(node)
}

impl std::fmt::Display for StateRangeError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "native state range: {self:?}")
    }
}
impl std::error::Error for StateRangeError {}

#[cfg(test)]
mod tests {
    use super::*;

    const MODULE: u16 = ModuleId::Budget as u16;
    const COMPOSITE: u32 = MIN_COMPOSITE_LEAVES;

    fn path(leaves: &[[u8; 32]], mut index: usize) -> (Vec<[u8; 32]>, [u8; 32]) {
        let mut level = leaves.to_vec();
        let mut siblings = Vec::new();
        while level.len() > 1 {
            let sibling = if index ^ 1 < level.len() {
                index ^ 1
            } else {
                index
            };
            siblings.push(level[sibling]);
            level = level
                .chunks(2)
                .map(|pair| hash(&[NODE, &pair[0], pair.get(1).unwrap_or(&pair[0])]))
                .collect();
            index /= 2;
        }
        (siblings, level[0])
    }

    struct Tree {
        root: [u8; 32],
        witnesses: Vec<StateWitness>,
        empty: EmptyModuleProof,
    }

    fn tree(entries: &[(&[u8], &[u8])], module_empty: bool) -> Tree {
        let mut sorted = entries.to_vec();
        sorted.sort_by(|left, right| left.0.cmp(right.0));
        let hashes: Vec<[u8; 32]> = sorted.iter().map(|(k, v)| leaf(k, v)).collect();
        let subtree = if module_empty {
            empty_module_root()
        } else {
            path(&hashes, 0).1
        };
        let composite: Vec<[u8; 32]> = (0..COMPOSITE)
            .map(|id| {
                let id = u16::try_from(id).unwrap_or(u16::MAX);
                let root = if id == MODULE {
                    subtree
                } else {
                    empty_module_root()
                };
                leaf(&id.to_be_bytes(), &root)
            })
            .collect();
        let (siblings_b, root) = path(&composite, usize::from(MODULE));
        let witnesses = if module_empty {
            Vec::new()
        } else {
            sorted
                .iter()
                .enumerate()
                .map(|(index, (key, value))| StateWitness {
                    module_id: MODULE,
                    key: key.to_vec(),
                    value: value.to_vec(),
                    account_path: None,
                    leaf_index_a: u32::try_from(index).unwrap_or(u32::MAX),
                    leaf_count_a: u32::try_from(sorted.len()).unwrap_or(u32::MAX),
                    siblings_a: path(&hashes, index).0,
                    leaf_count_b: COMPOSITE,
                    siblings_b: siblings_b.clone(),
                })
                .collect()
        };
        let empty = EmptyModuleProof {
            module_id: MODULE,
            composite_leaf_count: COMPOSITE,
            siblings: siblings_b,
        };
        Tree {
            root,
            witnesses,
            empty,
        }
    }

    fn range(tree: &Tree, prefix: &[u8], items: &[usize]) -> PrefixRange {
        PrefixRange {
            module_id: MODULE,
            prefix: prefix.to_vec(),
            module_leaf_count: u32::try_from(tree.witnesses.len()).unwrap_or(u32::MAX),
            items: items
                .iter()
                .map(|index| tree.witnesses[*index].clone())
                .collect(),
        }
    }

    fn mixed() -> Tree {
        tree(
            &[
                (b"alpha", b"1"),
                (b"budget:a", b"2"),
                (b"budget:b", b"3"),
                (b"budget:c", b"4"),
                (b"grant:x", b"5"),
            ],
            false,
        )
    }

    fn bound(tree: &Tree, index: usize) -> PrefixBound {
        PrefixBound::Witness(tree.witnesses[index].clone())
    }

    #[test]
    fn complete_interior_range_verifies() {
        let t = mixed();
        assert_eq!(
            verify_prefix_range(
                t.root,
                &range(&t, b"budget:", &[1, 2, 3]),
                &bound(&t, 0),
                &bound(&t, 4)
            ),
            Ok(())
        );
    }

    #[test]
    fn lower_and_upper_tree_edges_verify() {
        let t = mixed();
        assert_eq!(
            verify_prefix_range(
                t.root,
                &range(&t, b"alpha", &[0]),
                &PrefixBound::Edge,
                &bound(&t, 1)
            ),
            Ok(())
        );
        assert_eq!(
            verify_prefix_range(
                t.root,
                &range(&t, b"grant:", &[4]),
                &bound(&t, 3),
                &PrefixBound::Edge
            ),
            Ok(())
        );
    }

    #[test]
    fn empty_prefix_requires_adjacent_bounds() {
        let t = mixed();
        assert_eq!(
            verify_prefix_range(t.root, &range(&t, b"c", &[]), &bound(&t, 3), &bound(&t, 4)),
            Ok(())
        );
        assert_eq!(
            verify_prefix_range(t.root, &range(&t, b"c", &[]), &bound(&t, 2), &bound(&t, 4)),
            Err(StateRangeError::Gap)
        );
        assert_eq!(
            verify_prefix_range(
                t.root,
                &range(&t, b"c", &[]),
                &PrefixBound::Edge,
                &PrefixBound::Edge
            ),
            Err(StateRangeError::Gap)
        );
        assert_eq!(
            verify_prefix_range(
                t.root,
                &range(&t, b"zz", &[]),
                &bound(&t, 4),
                &PrefixBound::Edge
            ),
            Ok(())
        );
    }

    #[test]
    fn omissions_reorders_and_duplicates_fail() {
        let t = mixed();
        let (low, high) = (bound(&t, 0), bound(&t, 4));
        for (items, error) in [
            (&[2, 3][..], StateRangeError::Gap),
            (&[1, 3][..], StateRangeError::Gap),
            (&[1, 2][..], StateRangeError::Gap),
            (&[1, 1, 2, 3][..], StateRangeError::Padding),
            (&[1, 3, 2][..], StateRangeError::Gap),
        ] {
            assert_eq!(
                verify_prefix_range(t.root, &range(&t, b"budget:", items), &low, &high),
                Err(error)
            );
        }
        assert_eq!(
            verify_prefix_range(
                t.root,
                &range(&t, b"budget:", &[1, 2, 3]),
                &bound(&t, 1),
                &high
            ),
            Err(StateRangeError::Boundary)
        );
        assert_eq!(
            verify_prefix_range(
                t.root,
                &range(&t, b"budget:", &[1, 2, 3]),
                &PrefixBound::Edge,
                &high
            ),
            Err(StateRangeError::Gap)
        );
    }

    #[test]
    fn root_count_and_padding_manipulation_fail() {
        let t = mixed();
        let mut wrong_root = t.root;
        wrong_root[0] ^= 1;
        assert!(matches!(
            verify_prefix_range(
                wrong_root,
                &range(&t, b"budget:", &[1, 2, 3]),
                &bound(&t, 0),
                &bound(&t, 4)
            ),
            Err(StateRangeError::Proof(_))
        ));
        let mut counted = range(&t, b"budget:", &[1, 2, 3]);
        counted.module_leaf_count = 6;
        assert_eq!(
            verify_prefix_range(t.root, &counted, &bound(&t, 0), &bound(&t, 4)),
            Err(StateRangeError::Count)
        );
        let mut padded = t.witnesses[4].clone();
        padded.leaf_index_a = 5;
        padded.leaf_count_a = 6;
        padded.siblings_a[0] = leaf(&padded.key, &padded.value);
        let mut grown = t.witnesses[4].clone();
        grown.leaf_count_a = 6;
        grown.siblings_a[0] = leaf(&grown.key, &grown.value);
        let claim = PrefixRange {
            module_id: MODULE,
            prefix: b"grant:".to_vec(),
            module_leaf_count: 6,
            items: vec![grown, padded],
        };
        let mut lower = t.witnesses[3].clone();
        lower.leaf_count_a = 6;
        assert_eq!(
            verify_prefix_range(
                t.root,
                &claim,
                &PrefixBound::Witness(lower),
                &PrefixBound::Edge
            ),
            Err(StateRangeError::Padding)
        );
    }

    #[test]
    fn empty_module_requires_composite_empty_root() {
        let t = tree(&[], true);
        let empty = PrefixBound::EmptyModule(t.empty.clone());
        let r = PrefixRange {
            module_id: MODULE,
            prefix: b"budget:".to_vec(),
            module_leaf_count: 0,
            items: Vec::new(),
        };
        assert_eq!(verify_prefix_range(t.root, &r, &empty, &empty), Ok(()));
        let populated = mixed();
        let other = PrefixBound::EmptyModule(populated.empty.clone());
        assert_eq!(
            verify_prefix_range(populated.root, &r, &other, &other),
            Err(StateRangeError::EmptyModule)
        );
        assert_eq!(
            verify_prefix_range(t.root, &r, &PrefixBound::Edge, &PrefixBound::Edge),
            Err(StateRangeError::Count)
        );
        assert_eq!(
            verify_prefix_range(t.root, &r, &empty, &PrefixBound::Edge),
            Err(StateRangeError::EmptyModule)
        );
    }
}
