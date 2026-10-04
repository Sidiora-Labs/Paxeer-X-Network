use std::collections::BTreeMap;

use layerx_client::evidence::VerifiedCaps;
use layerx_crypto::disclosure::DisclosedNativeOperation;
use layerx_programs::{ProgramId, ProgramLifecycle, VerifiedProgramBalanceRead};
use layerx_proof::state::CanonicalAccount;
use layerx_types::ids::Did;
use layerx_wire::hash::did_id_for_protocol;
use sha2::{Digest as _, Sha256};

use crate::capability::{derive_effects, ProgramValueSource, VerifiedInputs};
use crate::ops::program::ProgramOperations;
use crate::prepare::{verify_disclosure_binding, Prepared};

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum ProgramSourceError {
    Preparation,
    Unsupported,
    Snapshot,
    MissingSource,
    AmbiguousSource,
    SourceOwnership,
    SourceAsset,
    FrozenSource,
    ProgramBinding,
    ProgramUnavailable,
    FeeUnavailable,
    Arithmetic,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub enum ResolvedProgramSource {
    Principal { account: [u8; 32] },
    Program { owner_program: [u8; 32], seed: Vec<u8>, account: [u8; 32] },
    Fee { account: [u8; 32] },
}

impl ResolvedProgramSource {
    #[must_use]
    pub const fn account(&self) -> [u8; 32] {
        match self {
            Self::Principal { account } | Self::Program { account, .. } | Self::Fee { account } => *account,
        }
    }

    #[must_use]
    pub const fn kind(&self) -> super::ProgramChargeKind {
        match self {
            Self::Principal { .. } => super::ProgramChargeKind::Principal,
            Self::Program { .. } => super::ProgramChargeKind::ProgramSpend,
            Self::Fee { .. } => super::ProgramChargeKind::Fee,
        }
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ResolvedProgramCharge {
    pub source: ResolvedProgramSource,
    pub asset: [u8; 32],
    pub destination: Option<[u8; 32]>,
    pub maximum_amount: u128,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct VerifiedResolvedProgramCharges {
    actor: [u8; 32],
    state_root: [u8; 32],
    batch_number: u64,
    global_sequence: u64,
    preparation_digest: [u8; 32],
    charges: Vec<ResolvedProgramCharge>,
}

impl VerifiedResolvedProgramCharges {
    #[must_use]
    pub const fn actor(&self) -> [u8; 32] { self.actor }
    #[must_use]
    pub const fn state_root(&self) -> [u8; 32] { self.state_root }
    #[must_use]
    pub const fn batch_number(&self) -> u64 { self.batch_number }
    #[must_use]
    pub const fn global_sequence(&self) -> u64 { self.global_sequence }
    #[must_use]
    pub fn charges(&self) -> &[ResolvedProgramCharge] { &self.charges }
    #[must_use]
    pub fn matches_prepared(&self, prepared: &Prepared) -> bool {
        validate_prepared(prepared).is_ok()
            && self.global_sequence == prepared.observed_head_sequence
            && self.preparation_digest == <[u8; 32]>::from(Sha256::digest(&prepared.canonical_bytes))
    }

    pub fn budget_allocations(
        &self,
        applicable: impl FnMut(&ResolvedProgramCharge) -> Result<Vec<super::LimitId>, super::DaemonLimitError>,
    ) -> Result<VerifiedProgramBudgetAllocations, super::DaemonLimitError> {
        let allocations = budget_allocation_rows(&self.charges, applicable)?;
        let charges = super::reservations::allocation_charges(&allocations)?;
        Ok(VerifiedProgramBudgetAllocations { actor: self.actor, state_root: self.state_root,
            batch_number: self.batch_number, global_sequence: self.global_sequence,
            preparation_digest: self.preparation_digest, allocations, charges })
    }

    pub fn budget_charges(
        &self,
        applicable: impl FnMut(&ResolvedProgramCharge) -> Result<Vec<super::LimitId>, super::DaemonLimitError>,
    ) -> Result<Vec<super::ProgramBudgetCharge>, super::DaemonLimitError> {
        budget_charge_rows(&self.charges, applicable)
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct VerifiedProgramBudgetAllocations {
    actor: [u8; 32],
    state_root: [u8; 32],
    batch_number: u64,
    global_sequence: u64,
    preparation_digest: [u8; 32],
    allocations: Vec<super::ProgramBudgetAllocation>,
    charges: Vec<super::ProgramBudgetCharge>,
}

impl VerifiedProgramBudgetAllocations {
    #[must_use]
    pub const fn actor(&self) -> [u8; 32] { self.actor }
    #[must_use]
    pub const fn state_root(&self) -> [u8; 32] { self.state_root }
    #[must_use]
    pub const fn batch_number(&self) -> u64 { self.batch_number }
    #[must_use]
    pub const fn global_sequence(&self) -> u64 { self.global_sequence }
    #[must_use]
    pub const fn preparation_digest(&self) -> [u8; 32] { self.preparation_digest }
    #[must_use]
    pub fn allocations(&self) -> &[super::ProgramBudgetAllocation] { &self.allocations }
    #[must_use]
    pub fn charges(&self) -> &[super::ProgramBudgetCharge] { &self.charges }
}

fn budget_allocation_rows(
    charges: &[ResolvedProgramCharge],
    mut applicable: impl FnMut(&ResolvedProgramCharge) -> Result<Vec<super::LimitId>, super::DaemonLimitError>,
) -> Result<Vec<super::ProgramBudgetAllocation>, super::DaemonLimitError> {
    let mut rows: BTreeMap<_, super::ProgramBudgetAllocation> = BTreeMap::new();
    for charge in charges {
        let mut limits = applicable(charge)?;
        limits.sort_unstable(); limits.dedup();
        if limits.is_empty() { return Err(super::DaemonLimitError::AmbiguousDenomination); }
        let key = (charge.asset, charge.source.account(), charge.source.kind(), charge.destination);
        if let Some(row) = rows.get_mut(&key) {
            if row.applicable_limits != limits { return Err(super::DaemonLimitError::Conflict); }
            row.maximum_amount = row.maximum_amount.checked_add(charge.maximum_amount)
                .ok_or(super::DaemonLimitError::Arithmetic)?;
        } else {
            rows.insert(key, super::ProgramBudgetAllocation { kind: charge.source.kind(), source: charge.source.account(),
                asset: charge.asset, destination: charge.destination, maximum_amount: charge.maximum_amount, applicable_limits: limits });
        }
    }
    let rows: Vec<_> = rows.into_values().collect();
    super::reservations::allocation_charges(&rows)?;
    Ok(rows)
}

fn budget_charge_rows(
    charges: &[ResolvedProgramCharge],
    mut applicable: impl FnMut(&ResolvedProgramCharge) -> Result<Vec<super::LimitId>, super::DaemonLimitError>,
) -> Result<Vec<super::ProgramBudgetCharge>, super::DaemonLimitError> {
        let mut rows: BTreeMap<([u8; 32], [u8; 32], super::ProgramChargeKind, Vec<super::LimitId>), super::ProgramBudgetCharge> = BTreeMap::new();
        for charge in charges {
            let mut limits = applicable(charge)?;
            limits.sort_unstable(); limits.dedup();
            if limits.is_empty() { return Err(super::DaemonLimitError::AmbiguousDenomination); }
            let key = (charge.asset, charge.source.account(), charge.source.kind(), limits.clone());
            if let Some(row) = rows.get_mut(&key) {
                row.amount = row.amount.checked_add(charge.maximum_amount).ok_or(super::DaemonLimitError::Arithmetic)?;
            } else {
                rows.insert(key, super::ProgramBudgetCharge { kind: charge.source.kind(), source: charge.source.account(),
                    asset: charge.asset, amount: charge.maximum_amount, applicable_limits: limits });
            }
        }
        Ok(rows.into_values().collect())
}

pub fn read_program_budget_sources(
    node: &mut layerx_client::Client,
    programs: &mut ProgramOperations,
    prepared: &Prepared,
    caps: &VerifiedCaps,
    now: u64,
    fee_correlation_id: u64,
) -> Result<VerifiedResolvedProgramCharges, ProgramSourceError> {
    validate_prepared(prepared)?;
    if !matches!(prepared.disclosure.native_operation,
        Some(DisclosedNativeOperation::ProgramDeploy(_)
            | DisclosedNativeOperation::ProgramUpgrade(_)
            | DisclosedNativeOperation::ProgramCall(_)
            | DisclosedNativeOperation::ProgramWindDown(_)
            | DisclosedNativeOperation::LegacyProgramCall(_))) {
        return Err(ProgramSourceError::Unsupported);
    }
    let actor = did_id_for_protocol(prepared.envelope.actor_did(), 3).map_err(|_| ProgramSourceError::Preparation)?;
    let header = layerx_wire::receipt::decode_batch_header(&caps.signed_header().canonical_bytes)
        .map_err(|_| ProgramSourceError::Snapshot)?;
    let freshness = caps.freshness();
    if now == 0 || caps.did() != actor || caps.state_root() == [0; 32]
        || caps.level() < layerx_types::verify::VerificationLevel::STATE_PROVEN
        || header.resulting_state_root() != caps.state_root()
        || header.protocol_version() != 3 || header.network_id() != prepared.envelope.network_id()
        || header.last_sequence() != prepared.observed_head_sequence
        || freshness.global_sequence != prepared.observed_head_sequence
        || freshness.observed_head_sequence != prepared.observed_head_sequence
        || freshness.batch_number != header.batch_number()
        || node.head().chain_sequence != prepared.observed_head_sequence
        || node.head().sealed_batch != header.batch_number()
    { return Err(ProgramSourceError::Snapshot); }
    let plan = derive_effects(&prepared.disclosure, &VerifiedInputs::default())
        .map_err(|_| ProgramSourceError::Preparation)?;
    let mut charges = Vec::new();
    let mut program_reads = BTreeMap::new();
    let mut gross = BTreeMap::new();
    for bound in plan.program_spend_bounds() {
        if bound.asset == [0; 32] || bound.destination == [0; 32] || bound.maximum_amount == 0 {
            return Err(ProgramSourceError::Preparation);
        }
        let source = match &bound.source {
            ProgramValueSource::Principal => {
                let account = principal_source(caps.all_accounts(), prepared.envelope.actor_did(),
                    prepared.envelope.protocol_version(), bound.asset)?;
                ResolvedProgramSource::Principal { account }
            }
            ProgramValueSource::Program { owner_program, seed, source_account } => {
                if !program_reads.contains_key(owner_program) {
                    let program = ProgramId::new(*owner_program).map_err(|_| ProgramSourceError::ProgramBinding)?;
                    let read = programs.current_value_accounts(program, now).map_err(|_| ProgramSourceError::ProgramUnavailable)?;
                    program_reads.insert(*owner_program, read);
                }
                let read = program_reads.get(owner_program).ok_or(ProgramSourceError::ProgramBinding)?;
                program_source(caps, read, *owner_program, seed, *source_account, bound.asset)?;
                ResolvedProgramSource::Program { owner_program: *owner_program, seed: seed.clone(), account: *source_account }
            }
        };
        let amount = gross.entry(bound.asset).or_insert(0_u128);
        *amount = amount.checked_add(bound.maximum_amount).ok_or(ProgramSourceError::Arithmetic)?;
        charges.push(ResolvedProgramCharge { source, asset: bound.asset,
            destination: Some(bound.destination), maximum_amount: bound.maximum_amount });
    }
    if &gross != plan.gross_per_asset() { return Err(ProgramSourceError::Preparation); }
    let maximum_fee = prepared.envelope.fee_limit().value();
    if maximum_fee != 0 {
        let fee = node.native_fee_policy(fee_correlation_id).map_err(|_| ProgramSourceError::FeeUnavailable)?;
        if fee.state_root != caps.state_root() || fee.observed_sequence != prepared.observed_head_sequence {
            return Err(ProgramSourceError::Snapshot);
        }
        let asset = fee.value.asset.asset_id;
        let account = principal_source(caps.all_accounts(), prepared.envelope.actor_did(),
            prepared.envelope.protocol_version(), asset)?;
        charges.push(ResolvedProgramCharge { source: ResolvedProgramSource::Fee { account }, asset,
            destination: None, maximum_amount: maximum_fee });
    }
    if node.head().chain_sequence != prepared.observed_head_sequence || node.head().sealed_batch != header.batch_number() {
        return Err(ProgramSourceError::Snapshot);
    }
    Ok(VerifiedResolvedProgramCharges { actor, state_root: caps.state_root(), batch_number: header.batch_number(),
        global_sequence: prepared.observed_head_sequence,
        preparation_digest: Sha256::digest(&prepared.canonical_bytes).into(), charges })
}

pub fn read_native_effect_budget_sources(
    node: &mut layerx_client::Client,
    prepared: &Prepared,
    caps: &VerifiedCaps,
    now: u64,
    fee_correlation_id: u64,
) -> Result<VerifiedResolvedProgramCharges, ProgramSourceError> {
    validate_prepared(prepared)?;
    if prepared.envelope.activity_type().module() == layerx_types::payload::ModuleId::Programs { return Err(ProgramSourceError::Unsupported); }
    let actor = did_id_for_protocol(prepared.envelope.actor_did(), 3).map_err(|_| ProgramSourceError::Preparation)?;
    let header = layerx_wire::receipt::decode_batch_header(&caps.signed_header().canonical_bytes)
        .map_err(|_| ProgramSourceError::Snapshot)?;
    let freshness = caps.freshness();
    if now == 0 || caps.did() != actor || caps.state_root() == [0; 32]
        || caps.level() < layerx_types::verify::VerificationLevel::STATE_PROVEN
        || header.resulting_state_root() != caps.state_root()
        || header.protocol_version() != 3 || header.network_id() != prepared.envelope.network_id()
        || header.last_sequence() != prepared.observed_head_sequence
        || freshness.global_sequence != prepared.observed_head_sequence
        || freshness.observed_head_sequence != prepared.observed_head_sequence
        || freshness.batch_number != header.batch_number()
        || node.head().chain_sequence != prepared.observed_head_sequence
        || node.head().sealed_batch != header.batch_number()
    { return Err(ProgramSourceError::Snapshot); }
    let plan = crate::capability::derive_native_effects(&prepared.disclosure, &VerifiedInputs::default())
        .map_err(|_| ProgramSourceError::Preparation)?;
    let mut charges = Vec::new();
    let mut gross = BTreeMap::new();
    for effect in plan.effects() {
        match effect {
            crate::capability::Effect::Transfer { from, to, asset, amount } => {
                let account = principal_source(caps.all_accounts(), prepared.envelope.actor_did(), prepared.envelope.protocol_version(), *asset)?;
                if account != *from || *to == [0; 32] || *amount == 0 { return Err(ProgramSourceError::SourceOwnership); }
                let total = gross.entry(*asset).or_insert(0_u128);
                *total = total.checked_add(*amount).ok_or(ProgramSourceError::Arithmetic)?;
                charges.push(ResolvedProgramCharge { source: ResolvedProgramSource::Principal { account }, asset: *asset, destination: Some(*to), maximum_amount: *amount });
            }
            crate::capability::Effect::Destruction { account, asset, amount } => {
                let source = principal_source(caps.all_accounts(), prepared.envelope.actor_did(), prepared.envelope.protocol_version(), *asset)?;
                if source != *account || *amount == 0 { return Err(ProgramSourceError::SourceOwnership); }
                let total = gross.entry(*asset).or_insert(0_u128);
                *total = total.checked_add(*amount).ok_or(ProgramSourceError::Arithmetic)?;
                charges.push(ResolvedProgramCharge { source: ResolvedProgramSource::Principal { account: source }, asset: *asset, destination: None, maximum_amount: *amount });
            }
            crate::capability::Effect::Issuance { .. } => return Err(ProgramSourceError::Unsupported),
            crate::capability::Effect::Authorization { .. } => {}
        }
    }
    if &gross != plan.gross_per_asset() { return Err(ProgramSourceError::Preparation); }
    let maximum_fee = prepared.envelope.fee_limit().value();
    if maximum_fee != 0 {
        let fee = node.native_fee_policy(fee_correlation_id).map_err(|_| ProgramSourceError::FeeUnavailable)?;
        if fee.state_root != caps.state_root() || fee.observed_sequence != prepared.observed_head_sequence {
            return Err(ProgramSourceError::Snapshot);
        }
        let asset = fee.value.asset.asset_id;
        let account = principal_source(caps.all_accounts(), prepared.envelope.actor_did(),
            prepared.envelope.protocol_version(), asset)?;
        charges.push(ResolvedProgramCharge { source: ResolvedProgramSource::Fee { account }, asset,
            destination: None, maximum_amount: maximum_fee });
    }
    if node.head().chain_sequence != prepared.observed_head_sequence || node.head().sealed_batch != header.batch_number() {
        return Err(ProgramSourceError::Snapshot);
    }
    Ok(VerifiedResolvedProgramCharges { actor, state_root: caps.state_root(), batch_number: header.batch_number(),
        global_sequence: prepared.observed_head_sequence,
        preparation_digest: Sha256::digest(&prepared.canonical_bytes).into(), charges })
}

fn validate_prepared(prepared: &Prepared) -> Result<(), ProgramSourceError> {
    verify_disclosure_binding(prepared).map_err(|_| ProgramSourceError::Preparation)?;
    if layerx_wire::activity::encode_unsigned_envelope(&prepared.envelope).map_err(|_| ProgramSourceError::Preparation)? != prepared.canonical_bytes
        || layerx_wire::sign::preimage_unsigned(&prepared.envelope).map_err(|_| ProgramSourceError::Preparation)?.as_bytes() != &prepared.signing_preimage
        || prepared.audit.observed_head_sequence != prepared.observed_head_sequence {
        return Err(ProgramSourceError::Preparation);
    }
    Ok(())
}

fn principal_source(
    accounts: &BTreeMap<[u8; 32], CanonicalAccount>, actor: &Did, protocol: u16, asset: [u8; 32],
) -> Result<[u8; 32], ProgramSourceError> {
    if asset == [0; 32] { return Err(ProgramSourceError::SourceAsset); }
    let principal = did_id_for_protocol(actor, protocol).map_err(|_| ProgramSourceError::Preparation)?;
    let mut selected = None;
    match protocol {
        1 | 2 => {
            if let Some(account) = accounts.get(&principal).filter(|account| account.kind == 1) {
                selected = Some(account);
            }
        }
        3 => {
            for account in accounts.values() {
                if !matches!(account.kind, 1 | 14) || account.asset_id() != asset { continue; }
                if account_owner(account)? != principal { continue; }
                if selected.replace(account).is_some() { return Err(ProgramSourceError::AmbiguousSource); }
            }
        }
        _ => return Err(ProgramSourceError::Unsupported),
    }
    let account = selected.ok_or(ProgramSourceError::MissingSource)?;
    if !account.has_asset() || account.asset_id() != asset { return Err(ProgramSourceError::SourceAsset); }
    if account.frozen { return Err(ProgramSourceError::FrozenSource); }
    Ok(account.account_id)
}

fn account_owner(account: &CanonicalAccount) -> Result<[u8; 32], ProgramSourceError> {
    let name = &account.name;
    let end = match account.kind {
        1 if name.starts_with(b"agent:") && name.ends_with(b":main") => name.len().checked_sub(5),
        14 if name.starts_with(b"agent:") && name.len() > 77
            && &name[name.len()-71..name.len()-64] == b":asset:" => name.len().checked_sub(71),
        _ => None,
    }.ok_or(ProgramSourceError::SourceOwnership)?;
    let did = Did::new(name.get(6..end).ok_or(ProgramSourceError::SourceOwnership)?)
        .map_err(|_| ProgramSourceError::SourceOwnership)?;
    did_id_for_protocol(&did, 3).map_err(|_| ProgramSourceError::SourceOwnership)
}

fn program_source(
    caps: &VerifiedCaps, read: &VerifiedProgramBalanceRead, owner: [u8; 32], seed: &[u8],
    source: [u8; 32], asset: [u8; 32],
) -> Result<(), ProgramSourceError> {
    if read.program().bytes() != owner || read.lifecycle() != ProgramLifecycle::Active
        || read.state_root() != caps.state_root()
        || read.freshness().observed_sequence != caps.freshness().global_sequence {
        return Err(ProgramSourceError::ProgramBinding);
    }
    let mut bindings = read.bindings().iter().filter(|binding| binding.program.bytes() == owner && binding.seed == seed);
    let binding = bindings.next().ok_or(ProgramSourceError::ProgramBinding)?;
    if bindings.next().is_some() || binding.account_id != source || binding.asset_id != asset {
        return Err(ProgramSourceError::ProgramBinding);
    }
    let mut values = read.value_accounts().iter().filter(|value| value.seed == seed && value.account_id == source);
    let value = values.next().ok_or(ProgramSourceError::ProgramBinding)?;
    if values.next().is_some() || value.asset_id != asset || value.state_root != caps.state_root()
        || value.observed_sequence != caps.freshness().global_sequence {
        return Err(ProgramSourceError::ProgramBinding);
    }
    let account = caps.all_accounts().get(&source).ok_or(ProgramSourceError::MissingSource)?;
    if account.kind != 13 || account.authority_key.is_some() { return Err(ProgramSourceError::SourceOwnership); }
    if !account.has_asset() || account.asset_id() != asset || account.balance() != value.balance {
        return Err(ProgramSourceError::SourceAsset);
    }
    if account.frozen || value.frozen { return Err(ProgramSourceError::FrozenSource); }
    Ok(())
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub(super) struct VerifiedExecutionProgramSources {
    actor: [u8; 32],
    state_root: [u8; 32],
    execution_sequence: u64,
    preparation_digest: [u8; 32],
    receipt_ref: [u8; 32],
    charges: Vec<ResolvedProgramCharge>,
}

impl VerifiedExecutionProgramSources {
    pub(super) fn charges(&self) -> &[ResolvedProgramCharge] { &self.charges }
    pub(super) fn matches(&self, prepared: &Prepared, receipt: &crate::protocol_evidence::VerifiedReceiptEvidence) -> bool {
        self.preparation_digest == <[u8; 32]>::from(Sha256::digest(&prepared.canonical_bytes))
            && self.execution_sequence == receipt.global_sequence()
            && self.receipt_ref == receipt.receipt_ref()
            && did_id_for_protocol(prepared.envelope.actor_did(), 3).ok() == Some(self.actor)
            && layerx_wire::receipt::decode(receipt.canonical_receipt()).ok()
                .and_then(|value| value.protocol().map(|protocol| protocol.previous_state_root())) == Some(self.state_root)
    }
}

pub(super) fn execution_program_sources(
    prepared: &Prepared,
    submission: &crate::sign::VerifiedSubmission,
    receipt: &crate::protocol_evidence::VerifiedReceiptEvidence,
    prestate: &layerx_client::evidence::VerifiedExecutionPrestate,
) -> Result<VerifiedExecutionProgramSources, ProgramSourceError> {
    validate_prepared(prepared)?;
    let decoded = layerx_wire::receipt::decode(receipt.canonical_receipt()).map_err(|_| ProgramSourceError::Preparation)?;
    let protocol = decoded.protocol().ok_or(ProgramSourceError::Preparation)?;
    let unsigned_receipt = layerx_wire::receipt::encode_unsigned(&decoded).map_err(|_| ProgramSourceError::Preparation)?;
    let receipt_digest = layerx_wire::hash::receipt_digest(&unsigned_receipt).map_err(|_| ProgramSourceError::Preparation)?;
    let outcome = protocol.program_outcome().ok_or(ProgramSourceError::Unsupported)?;
    if protocol.protocol_version() != 3 || prepared.envelope.protocol_version() != 3
        || receipt.level() < layerx_types::verify::VerificationLevel::BATCH_INCLUDED
        || submission.activity_id() != receipt.activity_id()
        || prestate.network_id() != prepared.envelope.network_id()
        || prestate.activity_id() != receipt.activity_id() || prestate.receipt_digest() != receipt_digest
        || prestate.execution_sequence() != receipt.global_sequence()
        || prestate.state_root() != protocol.previous_state_root()
        || prestate.selected_fee_schedule_version() != outcome.fee_schedule_version()
        || prestate.execution_sequence() <= prepared.observed_head_sequence
    { return Err(ProgramSourceError::Snapshot); }
    let plan = derive_effects(&prepared.disclosure, &VerifiedInputs::default())
        .map_err(|_| ProgramSourceError::Preparation)?;
    let mut charges = Vec::new();
    let mut gross = BTreeMap::new();
    for bound in plan.program_spend_bounds() {
        if bound.asset == [0; 32] || bound.destination == [0; 32] || bound.maximum_amount == 0 {
            return Err(ProgramSourceError::Preparation);
        }
        let source = match &bound.source {
            ProgramValueSource::Principal => ResolvedProgramSource::Principal {
                account: principal_source(prestate.all_accounts(), prepared.envelope.actor_did(), 3, bound.asset)?,
            },
            ProgramValueSource::Program { owner_program, seed, source_account } => {
                execution_program_binding(prestate, *owner_program, seed, *source_account, bound.asset)?;
                ResolvedProgramSource::Program { owner_program: *owner_program, seed: seed.clone(), account: *source_account }
            }
        };
        let total = gross.entry(bound.asset).or_insert(0_u128);
        *total = total.checked_add(bound.maximum_amount).ok_or(ProgramSourceError::Arithmetic)?;
        charges.push(ResolvedProgramCharge { source, asset: bound.asset,
            destination: Some(bound.destination), maximum_amount: bound.maximum_amount });
    }
    if &gross != plan.gross_per_asset() { return Err(ProgramSourceError::Preparation); }
    let maximum_fee = prepared.envelope.fee_limit().value();
    if maximum_fee != 0 {
        let asset = prestate.selected_fee_asset();
        let account = principal_source(prestate.all_accounts(), prepared.envelope.actor_did(), 3, asset)?;
        charges.push(ResolvedProgramCharge { source: ResolvedProgramSource::Fee { account }, asset,
            destination: None, maximum_amount: maximum_fee });
    }
    Ok(VerifiedExecutionProgramSources {
        actor: did_id_for_protocol(prepared.envelope.actor_did(), 3).map_err(|_| ProgramSourceError::Preparation)?,
        state_root: prestate.state_root(), execution_sequence: prestate.execution_sequence(),
        preparation_digest: Sha256::digest(&prepared.canonical_bytes).into(), receipt_ref: receipt.receipt_ref(), charges,
    })
}

fn execution_program_binding(
    prestate: &layerx_client::evidence::VerifiedExecutionPrestate,
    owner: [u8; 32], seed: &[u8], source: [u8; 32], asset: [u8; 32],
) -> Result<(), ProgramSourceError> {
    let mut primary_key = b"program-account\0p".to_vec();
    primary_key.extend_from_slice(&owner);
    primary_key.extend_from_slice(&Sha256::digest(seed));
    let value = prestate.program_records().get(&primary_key).ok_or(ProgramSourceError::ProgramBinding)?;
    if value.len() < 139 || value[0] != 2 { return Err(ProgramSourceError::ProgramBinding); }
    let number = |range: std::ops::Range<usize>| -> Result<[u8; 32], ProgramSourceError> {
        value.get(range).ok_or(ProgramSourceError::ProgramBinding)?.try_into().map_err(|_| ProgramSourceError::ProgramBinding)
    };
    let seed_length = usize::from(u16::from_be_bytes(value[97..99].try_into().map_err(|_| ProgramSourceError::ProgramBinding)?));
    if value.len() != 139_usize.checked_add(seed_length).ok_or(ProgramSourceError::Arithmetic)? {
        return Err(ProgramSourceError::ProgramBinding);
    }
    let binding = layerx_programs::ProgramValueAccountBinding {
        record_version: value[0],
        program: ProgramId::new(number(1..33)?).map_err(|_| ProgramSourceError::ProgramBinding)?,
        account_id: number(33..65)?, asset_id: number(65..97)?,
        registered_sequence: u64::from_be_bytes(value[99..107].try_into().map_err(|_| ProgramSourceError::ProgramBinding)?),
        registration_event_digest: number(107..139)?, seed: value[139..].to_vec(),
    };
    if binding.program.bytes() != owner || binding.seed != seed || binding.account_id != source
        || binding.asset_id != asset || binding.registered_sequence >= prestate.execution_sequence()
        || binding.primary_key() != primary_key
        || binding.primary_value().map_err(|_| ProgramSourceError::ProgramBinding)? != *value {
        return Err(ProgramSourceError::ProgramBinding);
    }
    let mut reverse_key = b"program-account\0r".to_vec(); reverse_key.extend_from_slice(&source);
    if prestate.program_records().get(&reverse_key) != Some(value) { return Err(ProgramSourceError::ProgramBinding); }
    let account = prestate.all_accounts().get(&source).ok_or(ProgramSourceError::MissingSource)?;
    if account.kind != 13 || account.authority_key.is_some() { return Err(ProgramSourceError::SourceOwnership); }
    if !account.has_asset() || account.asset_id() != asset { return Err(ProgramSourceError::SourceAsset); }
    if account.frozen { return Err(ProgramSourceError::FrozenSource); }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    fn must<T, E: std::fmt::Debug>(value: Result<T, E>) -> T {
        value.unwrap_or_else(|error| panic!("Program source: {error:?}"))
    }

    fn canonical_account(did: &str, asset: [u8; 32], kind: u8, frozen: bool) -> CanonicalAccount {
        let name = if kind == 1 { format!("agent:{did}:main") }
            else { format!("agent:{did}:asset:{}", hex::encode(asset)) };
        let mut identity = b"LX:ACCOUNT:v1".to_vec();
        identity.extend(must(u32::try_from(name.len())).to_be_bytes()); identity.extend(name.as_bytes());
        let id = Sha256::digest(identity).into();
        let mut bytes = must(u16::try_from(name.len())).to_be_bytes().to_vec();
        bytes.extend(name.as_bytes()); bytes.push(kind); bytes.extend(100_u128.to_be_bytes());
        bytes.extend(asset); bytes.push(1); bytes.extend(1_u64.to_be_bytes()); bytes.extend(1_u64.to_be_bytes());
        bytes.push(u8::from(frozen)); bytes.push(0); bytes.extend([7_u8;32]); bytes.push(1);
        must(layerx_proof::state::decode_account_value(id,&bytes))
    }

    #[test]
    fn native_principal_selection_uses_committed_ownership_asset_and_unique_namespace() {
        let actor = must(Did::new(b"did:layerx:alice"));
        let asset = [1;32];
        let main = canonical_account("did:layerx:alice",asset,1,false);
        let other = canonical_account("did:layerx:other",asset,1,false);
        let foreign_asset = canonical_account("did:layerx:alice",[2;32],14,false);
        let mut accounts = BTreeMap::from([(main.account_id,main.clone()),
            (other.account_id,other),(foreign_asset.account_id,foreign_asset)]);
        assert_eq!(principal_source(&accounts,&actor,3,asset),Ok(main.account_id));
        assert_eq!(principal_source(&accounts,&actor,3,[3;32]),Err(ProgramSourceError::MissingSource));
        let second = canonical_account("did:layerx:alice",asset,14,true);
        accounts.insert(second.account_id,second.clone());
        assert_eq!(principal_source(&accounts,&actor,3,asset),Err(ProgramSourceError::AmbiguousSource));
        accounts.remove(&main.account_id);
        assert_eq!(principal_source(&accounts,&actor,3,asset),Err(ProgramSourceError::FrozenSource));
        accounts.remove(&second.account_id);
        assert_eq!(principal_source(&accounts,&actor,3,asset),Err(ProgramSourceError::MissingSource));
    }

    #[test]
    fn legacy_lookup_does_not_guess_a_main_account_and_asset_zero_refuses() {
        let actor = must(Did::new(b"did:layerx:alice"));
        let main = canonical_account("did:layerx:alice",[1;32],1,false);
        let accounts = BTreeMap::from([(main.account_id,main)]);
        assert_eq!(principal_source(&accounts,&actor,1,[1;32]),Err(ProgramSourceError::MissingSource));
        assert_eq!(principal_source(&accounts,&actor,2,[1;32]),Err(ProgramSourceError::MissingSource));
        assert_eq!(principal_source(&accounts,&actor,3,[0;32]),Err(ProgramSourceError::SourceAsset));
        assert_eq!(principal_source(&accounts,&actor,4,[1;32]),Err(ProgramSourceError::Preparation));
    }

    #[test]
    fn denomination_grouping_retains_counterparty_scope_and_separate_fee_exposure() {
        let row = |destination,amount,source| ResolvedProgramCharge {
            source,asset:[1;32],destination,maximum_amount:amount,
        };
        let rows = vec![
                row(Some([5;32]),20,ResolvedProgramSource::Principal{account:[6;32]}),
                row(Some([7;32]),30,ResolvedProgramSource::Principal{account:[6;32]}),
                row(None,4,ResolvedProgramSource::Fee{account:[6;32]}),
        ];
        let charges = must(budget_charge_rows(&rows,|charge| {
            let mut limits=vec![super::super::LimitId([1;16])];
            if charge.destination==Some([5;32]) { limits.push(super::super::LimitId([2;16])); }
            Ok(limits)
        }));
        assert_eq!(charges.len(),3);
        assert_eq!(charges.iter().filter(|row|row.kind==super::super::ProgramChargeKind::Principal).map(|row|row.amount).sum::<u128>(),50);
        assert_eq!(charges.iter().find(|row|row.kind==super::super::ProgramChargeKind::Fee).map(|row|row.amount),Some(4));
        assert_eq!(charges.iter().find(|row|row.applicable_limits.len()==2).map(|row|row.amount),Some(20));
        assert!(budget_charge_rows(&rows,|_|Ok(Vec::new())).is_err());
    }
    #[test]
    fn program_allocations_keep_destinations_when_grouped_hold_limits_are_identical() {
        let row = |destination, amount| ResolvedProgramCharge {
            source: ResolvedProgramSource::Principal { account: [1; 32] },
            asset: [2; 32], destination: Some(destination), maximum_amount: amount,
        };
        let input = vec![row([3;32],7),row([4;32],11),row([3;32],2)];
        let rows = must(budget_allocation_rows(&input, |_| Ok(vec![super::super::LimitId([5;16])])));
        assert_eq!(rows.len(),2);
        assert_eq!(rows[0].destination,Some([3;32]));assert_eq!(rows[0].maximum_amount,9);
        assert_eq!(rows[1].destination,Some([4;32]));assert_eq!(rows[1].maximum_amount,11);
        let grouped = must(super::super::reservations::allocation_charges(&rows));
        assert_eq!(grouped.len(),1);assert_eq!(grouped[0].amount,20);
        let mut ordinal = 0_u8;
        assert!(budget_allocation_rows(&input, |_| {
            ordinal += 1; Ok(vec![super::super::LimitId([ordinal;16])])
        }).is_err());
        assert!(budget_allocation_rows(&[row([3;32],u128::MAX),row([4;32],1)],
            |_|Ok(vec![super::super::LimitId([5;16])])).is_err());
    }

}
