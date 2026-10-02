//! Complete caps-discovery page-chain verification at one signed committed root.

use layerx_proof::state::{decode_account_value, AccountProofError, CanonicalAccount};
use layerx_proof::state_range::{verify_prefix_range, PrefixBound, PrefixRange, StateRangeError};
use layerx_proof::state_witness::StateWitness;
use layerx_types::payload::ModuleId;
use sha2::{Digest as _, Sha256};

use super::{
    verify_module_evidence, verify_module_evidence_with_history, AccountEvidencePolicy,
    EvidenceError, SignedHeader, VerificationLevel,
};
use crate::caps::{CapsModuleSpan, CapsPage, CapsSnapshotOpen};

/// Domain of the selection digest bound into every caps cursor.
pub const SELECTION_DOMAIN: &[u8] = b"LayerX/caps-selection/v1\0";
/// Canonical budget record prefix in the Budget module.
pub const BUDGET_PREFIX: &[u8] = b"budget:";
/// Canonical payer-grant record prefix in the Asset module.
pub const GRANT_PREFIX: &[u8] = b"grant:";
const CURSOR_BYTES: usize = 107;
const PREFIX_BUDGET: u8 = 1;
const PREFIX_GRANT: u8 = 2;

/// Exact failure class for caps-discovery evidence.
#[derive(Clone, Debug, Eq, PartialEq)]
pub enum CapsEvidenceError {
    Evidence(EvidenceError),
    Range(StateRangeError),
    Account(AccountProofError),
    Network,
    Selection,
    Cursor,
    Module,
    Incomplete,
}

/// Authority and selection inputs required to verify one caps snapshot.
#[derive(Clone, Copy, Debug)]
pub struct CapsAuthority<'a> {
    pub policy: AccountEvidencePolicy,
    pub history: Option<&'a crate::handover::SequencerHistory>,
    pub selector_account: [u8; 32],
}

/// Both complete prefix chains and the selector account, verified at one signed root.
#[derive(Clone, Debug)]
pub struct VerifiedCapsSnapshot {
    snapshot_id: [u8; 32],
    network_id: u32,
    state_root: [u8; 32],
    observed_sequence: u64,
    level: VerificationLevel,
    signed_header: SignedHeader,
    account: CanonicalAccount,
    budgets: Vec<StateWitness>,
    grants: Vec<StateWitness>,
}

impl VerifiedCapsSnapshot {
    #[must_use]
    pub const fn snapshot_id(&self) -> [u8; 32] {
        self.snapshot_id
    }

    #[must_use]
    pub const fn network_id(&self) -> u32 {
        self.network_id
    }

    #[must_use]
    pub const fn state_root(&self) -> [u8; 32] {
        self.state_root
    }

    #[must_use]
    pub const fn observed_sequence(&self) -> u64 {
        self.observed_sequence
    }

    #[must_use]
    pub const fn level(&self) -> VerificationLevel {
        self.level
    }

    #[must_use]
    pub const fn signed_header(&self) -> &SignedHeader {
        &self.signed_header
    }

    /// The selector account proven at the snapshot root; a selector, not a wallet principal.
    #[must_use]
    pub const fn account(&self) -> &CanonicalAccount {
        &self.account
    }

    #[must_use]
    pub const fn selector_account(&self) -> [u8; 32] {
        self.account.account_id
    }

    /// Every canonical `budget:` key and exact value, in canonical order.
    pub fn budget_entries(&self) -> impl Iterator<Item = (&[u8], &[u8])> {
        self.budgets
            .iter()
            .map(|witness| (witness.key.as_slice(), witness.value.as_slice()))
    }

    /// Every canonical `grant:` key and exact value, in canonical order.
    pub fn grant_entries(&self) -> impl Iterator<Item = (&[u8], &[u8])> {
        self.grants
            .iter()
            .map(|witness| (witness.key.as_slice(), witness.value.as_slice()))
    }
}

/// Parsed fields of one 107-byte CUR1 cursor.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct CursorFields {
    pub snapshot_id: [u8; 32],
    pub network_id: u32,
    pub state_root: [u8; 32],
    pub selection_digest: [u8; 32],
    pub prefix: u8,
    pub next_position: u32,
}

impl CursorFields {
    /// Parses the exact canonical cursor encoding.
    ///
    /// # Errors
    /// Refuses wrong lengths, versions and prefix selectors.
    pub fn parse(bytes: &[u8]) -> Result<Self, CapsEvidenceError> {
        if bytes.len() != CURSOR_BYTES || bytes[..2] != 1_u16.to_be_bytes() {
            return Err(CapsEvidenceError::Cursor);
        }
        let array = |start: usize| -> Result<[u8; 32], CapsEvidenceError> {
            bytes[start..start + 32]
                .try_into()
                .map_err(|_| CapsEvidenceError::Cursor)
        };
        let word = |start: usize| -> Result<u32, CapsEvidenceError> {
            Ok(u32::from_be_bytes(
                bytes[start..start + 4]
                    .try_into()
                    .map_err(|_| CapsEvidenceError::Cursor)?,
            ))
        };
        let prefix = bytes[102];
        if prefix != PREFIX_BUDGET && prefix != PREFIX_GRANT {
            return Err(CapsEvidenceError::Cursor);
        }
        Ok(Self {
            snapshot_id: array(2)?,
            network_id: word(34)?,
            state_root: array(38)?,
            selection_digest: array(70)?,
            prefix,
            next_position: word(103)?,
        })
    }
}

/// Computes the canonical selection digest for a network and selector account.
#[must_use]
pub fn selection_digest(network_id: u32, selector_account: [u8; 32]) -> [u8; 32] {
    let mut hasher = Sha256::new();
    hasher.update(SELECTION_DOMAIN);
    hasher.update(network_id.to_be_bytes());
    hasher.update(selector_account);
    hasher.finalize().into()
}

/// Verifies an opened snapshot and its complete page chain for both prefixes.
///
/// # Errors
/// Refuses network, selection, header, authority, selector or account mismatches;
/// cursor replay, gaps, foreign snapshots or zero progress; any omitted,
/// duplicated, reordered or unbounded prefix leaf; and an unfinished chain.
pub fn verify_caps_snapshot(
    open: &CapsSnapshotOpen,
    pages: &[CapsPage],
    expected_network: u32,
    authority: &CapsAuthority<'_>,
) -> Result<VerifiedCapsSnapshot, CapsEvidenceError> {
    if expected_network == 0
        || open.network_id != expected_network
        || authority.policy.expected_network_id != expected_network
    {
        return Err(CapsEvidenceError::Network);
    }
    if authority.selector_account == [0; 32]
        || open.selection_digest != selection_digest(expected_network, authority.selector_account)
    {
        return Err(CapsEvidenceError::Selection);
    }
    let (account_evidence, account) = verify_selector_account(open, authority)?;
    if open.budget_module.module_id != ModuleId::Budget as u16
        || open.grant_module.module_id != ModuleId::Asset as u16
    {
        return Err(CapsEvidenceError::Module);
    }
    let chain = walk_pages(open, pages)?;
    let budgets = finish_prefix(open, &open.budget_module, BUDGET_PREFIX, chain.budget)?;
    let grants = finish_prefix(open, &open.grant_module, GRANT_PREFIX, chain.grant)?;
    Ok(VerifiedCapsSnapshot {
        snapshot_id: open.snapshot_id,
        network_id: open.network_id,
        state_root: open.state_root,
        observed_sequence: open.observed_sequence,
        level: account_evidence.level(),
        signed_header: account_evidence.signed_header().clone(),
        account,
        budgets,
        grants,
    })
}

fn verify_selector_account(
    open: &CapsSnapshotOpen,
    authority: &CapsAuthority<'_>,
) -> Result<(super::VerifiedModuleEvidence, CanonicalAccount), CapsEvidenceError> {
    let mut key = vec![4_u8];
    key.extend_from_slice(&authority.selector_account);
    let evidence = match authority.history {
        Some(history) => verify_module_evidence_with_history(
            &open.account_value,
            &open.account_evidence,
            0,
            &key,
            authority.policy,
            history,
        ),
        None => verify_module_evidence(
            &open.account_value,
            &open.account_evidence,
            0,
            &key,
            authority.policy,
        ),
    }
    .map_err(CapsEvidenceError::Evidence)?;
    if evidence.state_root() != open.state_root {
        return Err(CapsEvidenceError::Evidence(EvidenceError::SelectorMismatch));
    }
    let account = decode_account_value(authority.selector_account, &open.account_value)
        .map_err(CapsEvidenceError::Account)?;
    if account.account_id != authority.selector_account {
        return Err(CapsEvidenceError::Selection);
    }
    Ok((evidence, account))
}

#[derive(Default)]
struct PrefixChain {
    items: Vec<StateWitness>,
    lower: Option<PrefixBound>,
    upper: Option<PrefixBound>,
}

#[derive(Default)]
struct Chain {
    budget: PrefixChain,
    grant: PrefixChain,
}

fn walk_pages(open: &CapsSnapshotOpen, pages: &[CapsPage]) -> Result<Chain, CapsEvidenceError> {
    let mut chain = Chain::default();
    let mut expected = Some(*open.first_cursor.as_bytes());
    for page in pages {
        let cursor = expected.take().ok_or(CapsEvidenceError::Cursor)?;
        if *page.cursor_echo.as_bytes() != cursor {
            return Err(CapsEvidenceError::Cursor);
        }
        let fields = bound_cursor(open, &cursor)?;
        let current = if fields.prefix == PREFIX_BUDGET {
            &mut chain.budget
        } else {
            &mut chain.grant
        };
        expected = apply_page(open, current, &fields, page)?;
    }
    if expected.is_some() || chain.budget.upper.is_none() || chain.grant.upper.is_none() {
        return Err(CapsEvidenceError::Incomplete);
    }
    Ok(chain)
}

fn bound_cursor(open: &CapsSnapshotOpen, cursor: &[u8]) -> Result<CursorFields, CapsEvidenceError> {
    let fields = CursorFields::parse(cursor)?;
    if fields.snapshot_id != open.snapshot_id
        || fields.network_id != open.network_id
        || fields.state_root != open.state_root
        || fields.selection_digest != open.selection_digest
    {
        return Err(CapsEvidenceError::Cursor);
    }
    Ok(fields)
}

fn apply_page(
    open: &CapsSnapshotOpen,
    chain: &mut PrefixChain,
    fields: &CursorFields,
    page: &CapsPage,
) -> Result<Option<[u8; CURSOR_BYTES]>, CapsEvidenceError> {
    let first_page = chain.lower.is_none() && chain.items.is_empty();
    if chain.upper.is_some()
        || page.lower.is_some() != first_page
        || page.exhausted != page.next_cursor.is_none()
    {
        return Err(CapsEvidenceError::Cursor);
    }
    if page
        .items
        .first()
        .is_some_and(|first| first.leaf_index_a != fields.next_position)
    {
        return Err(CapsEvidenceError::Cursor);
    }
    let served = u32::try_from(page.items.len()).map_err(|_| CapsEvidenceError::Cursor)?;
    let next_position = fields
        .next_position
        .checked_add(served)
        .ok_or(CapsEvidenceError::Cursor)?;
    if let Some(lower) = &page.lower {
        chain.lower = Some(lower.clone());
    }
    chain.items.extend(page.items.iter().cloned());
    let Some(upper) = &page.upper else {
        let next = *page
            .next_cursor
            .as_ref()
            .ok_or(CapsEvidenceError::Cursor)?
            .as_bytes();
        let next_fields = bound_cursor(open, &next)?;
        if served == 0
            || next_fields.prefix != fields.prefix
            || next_fields.next_position != next_position
        {
            return Err(CapsEvidenceError::Cursor);
        }
        return Ok(Some(next));
    };
    chain.upper = Some(upper.clone());
    match (fields.prefix, &page.next_cursor) {
        (PREFIX_GRANT, None) => Ok(None),
        (PREFIX_BUDGET, Some(next)) => {
            let next_fields = bound_cursor(open, next.as_bytes())?;
            if next_fields.prefix != PREFIX_GRANT
                || next_fields.next_position != open.grant_module.prefix_first
            {
                return Err(CapsEvidenceError::Cursor);
            }
            Ok(Some(*next.as_bytes()))
        }
        _ => Err(CapsEvidenceError::Cursor),
    }
}

fn finish_prefix(
    open: &CapsSnapshotOpen,
    module: &CapsModuleSpan,
    prefix: &[u8],
    chain: PrefixChain,
) -> Result<Vec<StateWitness>, CapsEvidenceError> {
    let (Some(lower), Some(upper)) = (chain.lower, chain.upper) else {
        return Err(CapsEvidenceError::Incomplete);
    };
    let count = u32::try_from(chain.items.len()).map_err(|_| CapsEvidenceError::Module)?;
    if count != module.prefix_count
        || chain
            .items
            .first()
            .is_some_and(|first| first.leaf_index_a != module.prefix_first)
    {
        return Err(CapsEvidenceError::Range(StateRangeError::Count));
    }
    let range = PrefixRange {
        module_id: module.module_id,
        prefix: prefix.to_vec(),
        module_leaf_count: module.module_leaf_count,
        items: chain.items,
    };
    verify_prefix_range(open.state_root, &range, &lower, &upper)
        .map_err(CapsEvidenceError::Range)?;
    Ok(range.items)
}

impl std::fmt::Display for CapsEvidenceError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "caps discovery evidence: {self:?}")
    }
}
impl std::error::Error for CapsEvidenceError {}

#[cfg(test)]
mod tests {
    use super::*;

    fn cursor(prefix: u8, position: u32) -> [u8; CURSOR_BYTES] {
        let mut bytes = [0_u8; CURSOR_BYTES];
        bytes[..2].copy_from_slice(&1_u16.to_be_bytes());
        bytes[2..34].copy_from_slice(&[7; 32]);
        bytes[34..38].copy_from_slice(&42_u32.to_be_bytes());
        bytes[38..70].copy_from_slice(&[9; 32]);
        bytes[70..102].copy_from_slice(&selection_digest(42, [5; 32]));
        bytes[102] = prefix;
        bytes[103..].copy_from_slice(&position.to_be_bytes());
        bytes
    }

    #[test]
    fn cursor_fields_parse_exactly() {
        let fields = CursorFields::parse(&cursor(PREFIX_GRANT, 17));
        assert_eq!(
            fields,
            Ok(CursorFields {
                snapshot_id: [7; 32],
                network_id: 42,
                state_root: [9; 32],
                selection_digest: selection_digest(42, [5; 32]),
                prefix: PREFIX_GRANT,
                next_position: 17,
            })
        );
    }

    #[test]
    fn malformed_cursors_refuse() {
        let mut wrong_version = cursor(PREFIX_BUDGET, 0);
        wrong_version[1] = 2;
        assert_eq!(
            CursorFields::parse(&wrong_version),
            Err(CapsEvidenceError::Cursor)
        );
        assert_eq!(
            CursorFields::parse(&cursor(3, 0)),
            Err(CapsEvidenceError::Cursor)
        );
        assert_eq!(
            CursorFields::parse(&cursor(0, 0)),
            Err(CapsEvidenceError::Cursor)
        );
        assert_eq!(
            CursorFields::parse(&cursor(PREFIX_BUDGET, 0)[..106]),
            Err(CapsEvidenceError::Cursor)
        );
    }

    #[test]
    fn selection_digest_binds_network_and_account() {
        assert_eq!(SELECTION_DOMAIN.len(), 25);
        assert_ne!(selection_digest(42, [5; 32]), selection_digest(43, [5; 32]));
        assert_ne!(selection_digest(42, [5; 32]), selection_digest(42, [6; 32]));
    }
}
