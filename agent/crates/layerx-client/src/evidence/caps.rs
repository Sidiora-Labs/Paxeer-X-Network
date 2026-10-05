use std::collections::{BTreeMap, BTreeSet};

use layerx_proof::state::{decode_account_value, CanonicalAccount};
use layerx_proof::state_range::{
    verify_account_tree, verify_composite_roots, ModuleRangeWitness, RangeError,
    VerifiedModuleInventory, MAX_MODULE_LEAVES, MAX_UNIVERSAL_LEAVES,
};
use layerx_proof::state_witness::StateWitness;
use layerx_types::verify::VerificationLevel;
use layerx_wire::receipt::decode_batch_header;
use sha2::{Digest, Sha256};

use super::{
    verify_module_evidence, verify_module_evidence_with_history, AccountEvidencePolicy,
    AssetEvidenceError, EvidenceError, RootSelector, SignedHeader, VerifiedEffectiveAsset,
};
use crate::budget::ProtocolBudgetRecord;
use crate::grants::CommittedGrant;
use crate::handover::SequencerHistory;
use crate::read::{Freshness, ReadContext};

pub const MAX_CAPS_OBJECT_BYTES: usize = 64 * 1024 * 1024;
pub const MAX_CAPS_ACCOUNT_WITNESSES: usize = 4096;
const MAX_WITNESS_BYTES: usize = 35 + 129 + 1_048_576 + 96 * 32;
const MAX_ANCHOR_PROOF_BYTES: usize = 2 * 1024 * 1024;

#[derive(Clone, Debug, Eq, PartialEq)]
pub enum CapsEvidenceError {
    Encoding,
    Bounds,
    Range(RangeError),
    Authority(EvidenceError),
    Root,
    Freshness,
    Level,
    Account,
    Ownership,
    Budget,
    Grant,
}

#[derive(Clone, Debug)]
pub struct VerifiedCaps {
    did: [u8; 32],
    state_root: [u8; 32],
    budgets: Vec<ProtocolBudgetRecord>,
    grants: Vec<CommittedGrant>,
    accounts: BTreeMap<[u8; 32], CanonicalAccount>,
    asset_inventory: VerifiedModuleInventory,
    level: VerificationLevel,
    freshness: Freshness,
    signed_header: SignedHeader,
}

impl VerifiedCaps {
    pub const fn did(&self) -> [u8; 32] {
        self.did
    }
    pub const fn state_root(&self) -> [u8; 32] {
        self.state_root
    }
    pub fn budgets(&self) -> &[ProtocolBudgetRecord] {
        &self.budgets
    }
    pub fn grants(&self) -> &[CommittedGrant] {
        &self.grants
    }
    pub fn all_accounts(&self) -> &BTreeMap<[u8; 32], CanonicalAccount> {
        &self.accounts
    }
    pub const fn level(&self) -> VerificationLevel {
        self.level
    }
    pub const fn freshness(&self) -> Freshness {
        self.freshness
    }
    pub const fn signed_header(&self) -> &SignedHeader {
        &self.signed_header
    }
    pub fn is_empty(&self) -> bool {
        self.budgets.is_empty() && self.grants.is_empty()
    }
    pub fn effective_asset(
        &self,
        asset_id: [u8; 32],
    ) -> Result<VerifiedEffectiveAsset, AssetEvidenceError> {
        super::asset_record::resolve(
            &self.asset_inventory,
            asset_id,
            self.state_root,
            self.level,
            self.freshness,
        )
    }
    pub fn issuance(
        &self,
        asset_id: [u8; 32],
    ) -> Result<super::VerifiedIssuance, AssetEvidenceError> {
        super::asset_record::issuance(self.effective_asset(asset_id)?, self.did)
    }
}

pub fn verify_caps_object(
    bytes: &[u8],
    did: [u8; 32],
    expected_root: [u8; 32],
    context: ReadContext,
    history: Option<&SequencerHistory>,
) -> Result<VerifiedCaps, CapsEvidenceError> {
    if bytes.len() > MAX_CAPS_OBJECT_BYTES || did == [0; 32] {
        return Err(CapsEvidenceError::Bounds);
    }
    let mut reader = Reader(bytes);
    if reader.u16()? != 1 {
        return Err(CapsEvidenceError::Encoding);
    }
    let value = reader.bytes(8)?;
    if value.len() != 8 {
        return Err(CapsEvidenceError::Encoding);
    }
    let proof = reader.bytes(MAX_ANCHOR_PROOF_BYTES)?;
    let policy = AccountEvidencePolicy {
        expected_protocol_version: context.expected_protocol_version,
        expected_network_id: context.expected_network_id,
        handshake_sequencer_key: context.handshake_sequencer_key,
        root_selector: context.root_selector,
    };
    let anchor = if let Some(history) = history {
        verify_module_evidence_with_history(value, proof, 0, b"sequence", policy, history)
    } else {
        verify_module_evidence(value, proof, 0, b"sequence", policy)
    }
    .map_err(CapsEvidenceError::Authority)?;
    if anchor.state_root() != expected_root {
        return Err(CapsEvidenceError::Root);
    }
    if history.is_none()
        && anchor.signed_header().response_authorization() != context.sequencer_authorization
    {
        return Err(CapsEvidenceError::Authority(
            EvidenceError::SequencerMismatch,
        ));
    }
    if anchor.level() < context.requested.level() {
        return Err(CapsEvidenceError::Level);
    }
    let header = decode_batch_header(&anchor.signed_header().canonical_bytes)
        .map_err(|_| CapsEvidenceError::Encoding)?;
    let next_sequence =
        u64::from_be_bytes(value.try_into().map_err(|_| CapsEvidenceError::Encoding)?);
    if header.last_sequence().checked_add(1) != Some(next_sequence)
        || (context.root_selector == RootSelector::Latest
            && (header.batch_number() != context.head.sealed_batch
                || header.last_sequence() != context.head.chain_sequence))
    {
        return Err(CapsEvidenceError::Freshness);
    }
    let composite_count = reader.u16()?;
    let maximum = layerx_types::payload::ModuleId::ALL.len() + 1;
    if usize::from(composite_count) > maximum {
        return Err(CapsEvidenceError::Bounds);
    }
    let roots = (0..composite_count)
        .map(|_| reader.array())
        .collect::<Result<Vec<_>, _>>()?;
    verify_composite_roots(&roots, expected_root).map_err(CapsEvidenceError::Range)?;
    let mut anchor_reader = super::Reader::new(proof);
    if anchor_reader.u16().map_err(CapsEvidenceError::Authority)? != 1
        || anchor_reader.u8().map_err(CapsEvidenceError::Authority)? != 4
    {
        return Err(CapsEvidenceError::Encoding);
    }
    RootSelector::decode(&mut anchor_reader).map_err(CapsEvidenceError::Authority)?;
    let anchor_witness = StateWitness::decode(
        anchor_reader
            .length_prefixed(MAX_WITNESS_BYTES)
            .map_err(CapsEvidenceError::Authority)?,
    )
    .map_err(|_| CapsEvidenceError::Account)?;
    if anchor_witness.leaf_count_b != u32::from(composite_count) {
        return Err(CapsEvidenceError::Root);
    }
    let universal = reader.module(0)?;
    if universal.composite_count != u32::from(composite_count)
        || roots.first() != Some(&universal.subtree_root)
    {
        return Err(CapsEvidenceError::Root);
    }
    universal
        .verify_prefix(expected_root, b"sequence")
        .map_err(CapsEvidenceError::Range)?;
    let sequence_witness = universal
        .leaves
        .iter()
        .find(|leaf| leaf.key == b"sequence")
        .ok_or(CapsEvidenceError::Root)?;
    if sequence_witness != &anchor_witness {
        return Err(CapsEvidenceError::Root);
    }
    if reader.u8()? != 2 {
        return Err(CapsEvidenceError::Encoding);
    }
    let budget_module = reader.module(3)?;
    let grant_module = reader.module(1)?;
    for module in [&budget_module, &grant_module] {
        if module.composite_count != u32::from(composite_count)
            || roots.get(usize::from(module.module_id)) != Some(&module.subtree_root)
        {
            return Err(CapsEvidenceError::Root);
        }
    }
    let budget_prefix = budget_module
        .verify_prefix(expected_root, b"budget:")
        .map_err(CapsEvidenceError::Range)?;
    let grant_prefix = grant_module
        .verify_prefix(expected_root, b"grant:")
        .map_err(CapsEvidenceError::Range)?;
    let asset_inventory = grant_module
        .verify_full_module(expected_root)
        .map_err(CapsEvidenceError::Range)?;
    let budgets = budget_prefix
        .records()
        .iter()
        .map(|(key, value)| {
            ProtocolBudgetRecord::decode_state(key, value).map_err(|_| CapsEvidenceError::Budget)
        })
        .collect::<Result<Vec<_>, _>>()?;
    let grants = grant_prefix
        .records()
        .iter()
        .map(|(key, value)| {
            CommittedGrant::decode(key, value).map_err(|_| CapsEvidenceError::Grant)
        })
        .collect::<Result<Vec<_>, _>>()?;
    let mut required = BTreeSet::new();
    for budget in &budgets {
        required.insert(budget.owner);
        required.insert(budget.budget_account);
        if let Some(source) = budget.source_account {
            required.insert(source);
        }
    }
    for grant in &grants {
        required.insert(grant.grant.from);
        required.insert(grant.grant.recipient);
    }
    let count = reader.count(MAX_CAPS_ACCOUNT_WITNESSES)?;
    if count < required.len() {
        return Err(CapsEvidenceError::Account);
    }
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
            return Err(CapsEvidenceError::Account);
        }
        let id: [u8; 32] = witness.key[1..]
            .try_into()
            .map_err(|_| CapsEvidenceError::Account)?;
        if previous.is_some_and(|prior| prior >= id) {
            return Err(CapsEvidenceError::Account);
        }
        witness
            .verify(expected_root)
            .map_err(|_| CapsEvidenceError::Account)?;
        let account =
            decode_account_value(id, &witness.value).map_err(|_| CapsEvidenceError::Account)?;
        accounts.insert(id, account);
        previous = Some(id);
        account_witnesses.push(witness);
    }
    if !reader.0.is_empty() {
        return Err(CapsEvidenceError::Encoding);
    }
    verify_account_tree(&account_witnesses, &universal, expected_root)
        .map_err(CapsEvidenceError::Range)?;
    if required.iter().any(|id| !accounts.contains_key(id)) {
        return Err(CapsEvidenceError::Account);
    }
    let account = |id: &[u8; 32]| accounts.get(id).ok_or(CapsEvidenceError::Account);
    let mut owned_budgets = Vec::new();
    for budget in budgets {
        let owner = account(&budget.owner)?;
        let custody = account(&budget.budget_account)?;
        let owner_did = account_owner(owner)?.ok_or(CapsEvidenceError::Ownership)?;
        if owner.kind != 1
            || custody.kind != 2
            || account_owner(custody)? != Some(owner_did)
            || custody.asset_id() != budget.asset_id
        {
            return Err(CapsEvidenceError::Ownership);
        }
        if let Some(source) = budget.source_account {
            let source = account(&source)?;
            if !matches!(source.kind, 1 | 14)
                || account_owner(source)? != Some(owner_did)
                || source.asset_id() != budget.asset_id
                || source.authority_key.is_none()
                || source.authority_key != owner.authority_key
            {
                return Err(CapsEvidenceError::Ownership);
            }
        } else if owner.asset_id() != budget.asset_id {
            return Err(CapsEvidenceError::Ownership);
        }
        if owner_did == did {
            owned_budgets.push(budget);
        }
    }
    let mut owned_grants = Vec::new();
    for grant in grants {
        let payer = account(&grant.grant.from)?;
        let _receiver = account(&grant.grant.recipient)?;
        if payer.asset_id() != grant.grant.asset {
            return Err(CapsEvidenceError::Ownership);
        }
        let owner = account_owner(payer)?.ok_or(CapsEvidenceError::Ownership)?;
        if owner == did {
            owned_grants.push(grant);
        }
    }
    Ok(VerifiedCaps {
        did,
        state_root: expected_root,
        budgets: owned_budgets,
        grants: owned_grants,
        accounts,
        asset_inventory,
        level: anchor.level(),
        freshness: Freshness {
            global_sequence: header.last_sequence(),
            batch_number: header.batch_number(),
            observed_head_sequence: context.head.chain_sequence,
            observed_checkpoint: anchor
                .checkpoint_id()
                .unwrap_or(context.head.finalised_checkpoint),
        },
        signed_header: anchor.signed_header().clone(),
    })
}

fn account_owner(account: &CanonicalAccount) -> Result<Option<[u8; 32]>, CapsEvidenceError> {
    if !matches!(account.kind, 1..=5 | 14) {
        return Ok(None);
    }
    let name = &account.name;
    if !name.starts_with(b"agent:") {
        return Err(CapsEvidenceError::Ownership);
    }
    let end = if account.kind == 1 && name.ends_with(b":main") {
        name.len().checked_sub(5)
    } else if account.kind == 14
        && name.len() > 77
        && &name[name.len() - 71..name.len() - 64] == b":asset:"
    {
        name.len().checked_sub(71)
    } else {
        let marker: &[u8] = match account.kind {
            2 => b":budget:",
            3 => b":escrow:",
            4 => b":stream:",
            5 => b":margin:",
            _ => return Err(CapsEvidenceError::Ownership),
        };
        name.windows(marker.len())
            .enumerate()
            .filter(|(offset, value)| {
                *offset >= 7 && *value == marker && *offset + marker.len() < name.len()
            })
            .map(|(offset, _)| offset)
            .last()
    }
    .ok_or(CapsEvidenceError::Ownership)?;
    let did = name.get(6..end).ok_or(CapsEvidenceError::Ownership)?;
    if did.is_empty() || did.len() > 255 {
        return Err(CapsEvidenceError::Ownership);
    }
    let mut hash = Sha256::new();
    hash.update(b"LXP/v1/did-id\0");
    hash.update(
        u16::try_from(did.len())
            .map_err(|_| CapsEvidenceError::Ownership)?
            .to_be_bytes(),
    );
    hash.update(did);
    Ok(Some(hash.finalize().into()))
}

struct Reader<'a>(&'a [u8]);
impl<'a> Reader<'a> {
    fn array<const N: usize>(&mut self) -> Result<[u8; N], CapsEvidenceError> {
        let value = self
            .0
            .get(..N)
            .ok_or(CapsEvidenceError::Encoding)?
            .try_into()
            .map_err(|_| CapsEvidenceError::Encoding)?;
        self.0 = &self.0[N..];
        Ok(value)
    }
    fn u8(&mut self) -> Result<u8, CapsEvidenceError> {
        Ok(self.array::<1>()?[0])
    }
    fn u16(&mut self) -> Result<u16, CapsEvidenceError> {
        Ok(u16::from_be_bytes(self.array()?))
    }
    fn u32(&mut self) -> Result<u32, CapsEvidenceError> {
        Ok(u32::from_be_bytes(self.array()?))
    }
    fn count(&mut self, maximum: usize) -> Result<usize, CapsEvidenceError> {
        let count = usize::try_from(self.u32()?).map_err(|_| CapsEvidenceError::Bounds)?;
        if count > maximum {
            return Err(CapsEvidenceError::Bounds);
        }
        Ok(count)
    }
    fn bytes(&mut self, maximum: usize) -> Result<&'a [u8], CapsEvidenceError> {
        let length = self.count(maximum)?;
        let value = self.0.get(..length).ok_or(CapsEvidenceError::Encoding)?;
        self.0 = &self.0[length..];
        Ok(value)
    }
    fn witness(&mut self) -> Result<StateWitness, CapsEvidenceError> {
        StateWitness::decode(self.bytes(MAX_WITNESS_BYTES)?).map_err(|_| CapsEvidenceError::Account)
    }
    fn module(&mut self, expected: u16) -> Result<ModuleRangeWitness, CapsEvidenceError> {
        let module_id = self.u16()?;
        if module_id != expected {
            return Err(CapsEvidenceError::Encoding);
        }
        let subtree_root = self.array()?;
        let composite_index = self.u32()?;
        let composite_count = self.u32()?;
        let depth = self.u8()?;
        if depth > 32 {
            return Err(CapsEvidenceError::Bounds);
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

impl std::fmt::Display for CapsEvidenceError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "{self:?}")
    }
}
impl std::error::Error for CapsEvidenceError {}
