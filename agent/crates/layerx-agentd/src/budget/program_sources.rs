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

    pub fn budget_charges(
        &self,
        applicable: impl FnMut(&ResolvedProgramCharge) -> Result<Vec<super::LimitId>, super::DaemonLimitError>,
    ) -> Result<Vec<super::ProgramBudgetCharge>, super::DaemonLimitError> {
        budget_charge_rows(&self.charges, applicable)
    }
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
}
