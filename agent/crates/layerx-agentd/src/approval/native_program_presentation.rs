use std::collections::BTreeMap;

use layerx_crypto::disclosure::{DisclosedNativeOperation, DisclosedProgramCall,
    DisclosedProgramDeploy, DisclosedProgramUpgrade, DisclosedProgramWindDown};
use layerx_types::payload::{ModuleId, ModuleRegistry};
use sha2::{Digest as _, Sha256};

use crate::budget::{ProgramBudgetAllocation, ProgramBudgetReservation, ProgramChargeKind};
use crate::capability::{derive_effects, ProgramValueSource, VerifiedInputs};
use crate::human::HumanPeer;
use crate::store::Store;
use super::native_program::{CarrierError, NativeApprovalState, NativeProgramApprovalCarrier};

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum ProgramPresentationError { Missing, Binding, Corrupt, MissingAuthority, Unsupported }

impl From<CarrierError> for ProgramPresentationError {
    fn from(error: CarrierError) -> Self {
        match error {
            CarrierError::Missing => Self::Missing,
            CarrierError::Binding => Self::Binding,
            CarrierError::Policy | CarrierError::Budget | CarrierError::Corrupt => Self::Corrupt,
        }
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum ProgramPresentationState { Awaiting, Granted, Rejected, Expired, Defective, NotRequired }

#[derive(Clone, Debug, Eq, PartialEq)]
pub enum ProgramPresentationOperation {
    Deploy(DisclosedProgramDeploy),
    Upgrade(DisclosedProgramUpgrade),
    Call(DisclosedProgramCall),
    WindDown(DisclosedProgramWindDown),
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub enum ProgramPresentationSemantics {
    OperationOnly,
    AuthorizedLimits(Vec<ProgramBudgetAllocation>),
}

pub struct OwnedNativeProgramPresentation {
    id: [u8; 32],
    held_digest: [u8; 32],
    actor: Vec<u8>,
    owner: String,
    state: ProgramPresentationState,
    activity_ordinal: u16,
    canonical_unsigned_bytes: Vec<u8>,
    immutable_carrier_bytes: Vec<u8>,
    reservation: ProgramBudgetReservation,
    created_at_sequence: u64,
    budget_expiry_sequence: u64,
    created_at_unix_seconds: u64,
    activity_expires_at_unix_milliseconds: u64,
    release_ref: Option<[u8; 32]>,
    fee_asset: Option<[u8; 32]>,
    operation: ProgramPresentationOperation,
    semantics: ProgramPresentationSemantics,
}

impl OwnedNativeProgramPresentation {
    pub fn id(&self) -> [u8; 32] { self.id }
    pub fn held_digest(&self) -> [u8; 32] { self.held_digest }
    pub fn actor(&self) -> &[u8] { &self.actor }
    pub fn owner(&self) -> &str { &self.owner }
    pub fn state(&self) -> ProgramPresentationState { self.state }
    pub fn activity_module(&self) -> u16 { 9 }
    pub fn activity_ordinal(&self) -> u16 { self.activity_ordinal }
    pub fn reservation(&self) -> &ProgramBudgetReservation { &self.reservation }
    pub fn canonical_unsigned_bytes(&self) -> &[u8] { &self.canonical_unsigned_bytes }
    pub fn immutable_carrier_bytes(&self) -> &[u8] { &self.immutable_carrier_bytes }
    pub fn created_at_sequence(&self) -> u64 { self.created_at_sequence }
    pub fn budget_expiry_sequence(&self) -> u64 { self.budget_expiry_sequence }
    pub fn created_at_unix_seconds(&self) -> u64 { self.created_at_unix_seconds }
    pub fn activity_expires_at_unix_milliseconds(&self) -> u64 { self.activity_expires_at_unix_milliseconds }
    pub fn release_ref(&self) -> Option<[u8; 32]> { self.release_ref }
    pub fn fee_asset(&self) -> Option<[u8; 32]> { self.fee_asset }
    pub fn operation(&self) -> &ProgramPresentationOperation { &self.operation }
    pub fn semantics(&self) -> &ProgramPresentationSemantics { &self.semantics }
}

type AllocationKey = (ProgramChargeKind, [u8; 32], [u8; 32], Option<[u8; 32]>);

pub fn read_owned(store: &Store, peer: &HumanPeer, id: [u8; 32], registry: &ModuleRegistry)
    -> Result<OwnedNativeProgramPresentation, ProgramPresentationError> {
    let carrier = NativeProgramApprovalCarrier::read_for_human(store, peer, id)?;
    let presentation = carrier.presentation(store)?;
    let prepared = carrier.restore_prepared(registry)?;
    crate::prepare::verify_disclosure_binding(&prepared).map_err(|_| ProgramPresentationError::Binding)?;
    if prepared.envelope.activity_type().module() != ModuleId::Programs
        || prepared.envelope.actor_did().as_bytes() != carrier.retained_actor() {
        return Err(ProgramPresentationError::Binding);
    }
    let operation = match &prepared.disclosure.native_operation {
        Some(DisclosedNativeOperation::ProgramDeploy(value)) => ProgramPresentationOperation::Deploy((**value).clone()),
        Some(DisclosedNativeOperation::ProgramUpgrade(value)) => ProgramPresentationOperation::Upgrade((**value).clone()),
        Some(DisclosedNativeOperation::ProgramCall(value)) => ProgramPresentationOperation::Call((**value).clone()),
        Some(DisclosedNativeOperation::ProgramWindDown(value)) => ProgramPresentationOperation::WindDown((**value).clone()),
        _ => return Err(ProgramPresentationError::Unsupported),
    };
    let plan = derive_effects(&prepared.disclosure, &VerifiedInputs { revoke_balance: None })
        .map_err(|error| match error {
            crate::capability::EffectsError::ProgramExitContextRequired
                | crate::capability::EffectsError::UnboundedProgramExit => ProgramPresentationError::MissingAuthority,
            _ => ProgramPresentationError::Corrupt,
        })?;
    let reservation = carrier.budget()?;
    let allocations = reservation.allocations().ok_or(ProgramPresentationError::MissingAuthority)?;
    let mut actual = BTreeMap::<AllocationKey, u128>::new();
    let mut fee_asset = None;
    let mut limits = Vec::new();
    for allocation in allocations {
        if allocation.kind == ProgramChargeKind::Fee {
            if fee_asset.replace(allocation.asset).is_some() || allocation.destination.is_some()
                || allocation.maximum_amount != prepared.envelope.fee_limit().value()
                || !principal_source_matches(&prepared, allocation.source, allocation.asset)? {
                return Err(ProgramPresentationError::Binding);
            }
        } else {
            let key = (allocation.kind, allocation.source, allocation.asset, allocation.destination);
            if actual.insert(key, allocation.maximum_amount).is_some() {
                return Err(ProgramPresentationError::Binding);
            }
            limits.push(allocation.clone());
        }
    }
    if fee_asset.is_some() != (prepared.envelope.fee_limit().value() != 0) {
        return Err(ProgramPresentationError::Binding);
    }
    let mut expected = BTreeMap::<AllocationKey, u128>::new();
    for bound in plan.program_spend_bounds() {
        let (kind, source) = match &bound.source {
            ProgramValueSource::Principal => {
                let mut sources = limits.iter().filter(|allocation| allocation.kind == ProgramChargeKind::Principal
                    && allocation.asset == bound.asset && allocation.destination == Some(bound.destination));
                let source = sources.next().ok_or(ProgramPresentationError::Binding)?.source;
                if sources.next().is_some() || !principal_source_matches(&prepared, source, bound.asset)? {
                    return Err(ProgramPresentationError::Binding);
                }
                (ProgramChargeKind::Principal, source)
            }
            ProgramValueSource::Program { owner_program, seed, source_account } => {
                let program = layerx_programs_runtime::ProgramId::new(*owner_program)
                    .map_err(|_| ProgramPresentationError::Binding)?;
                let derived = layerx_programs_runtime::accounts::derive_program_account(
                    program, seed)
                    .map_err(|_| ProgramPresentationError::Binding)?;
                if !derived.matches(source_account) { return Err(ProgramPresentationError::Binding); }
                (ProgramChargeKind::ProgramSpend, *source_account)
            }
        };
        let amount = expected.entry((kind, source, bound.asset, Some(bound.destination))).or_default();
        *amount = amount.checked_add(bound.maximum_amount).ok_or(ProgramPresentationError::Corrupt)?;
    }
    if expected != actual { return Err(ProgramPresentationError::Binding); }
    let immutable_carrier_bytes = carrier.immutable_hold_bytes()?;
    let held_digest = carrier.held_digest()?;
    if <[u8; 32]>::from(Sha256::digest(&immutable_carrier_bytes)) != held_digest
        || <[u8; 32]>::from(Sha256::digest(&prepared.canonical_bytes)) != id {
        return Err(ProgramPresentationError::Binding);
    }
    let state = match carrier.state() {
        NativeApprovalState::Awaiting => ProgramPresentationState::Awaiting,
        NativeApprovalState::Granted => ProgramPresentationState::Granted,
        NativeApprovalState::Rejected => ProgramPresentationState::Rejected,
        NativeApprovalState::Expired => ProgramPresentationState::Expired,
        NativeApprovalState::Defective => ProgramPresentationState::Defective,
        NativeApprovalState::NotRequired => ProgramPresentationState::NotRequired,
    };
    Ok(OwnedNativeProgramPresentation {
        id, held_digest, actor: carrier.retained_actor().to_vec(),
        owner: peer.subject.as_ref().ok_or(ProgramPresentationError::Binding)?.owner.clone(),
        state, activity_ordinal: prepared.envelope.activity_type().ordinal(),
        canonical_unsigned_bytes: prepared.canonical_bytes, immutable_carrier_bytes, reservation,
        created_at_sequence: presentation.created_at_sequence,
        budget_expiry_sequence: presentation.budget_expiry_sequence,
        created_at_unix_seconds: presentation.created_at_unix_seconds,
        activity_expires_at_unix_milliseconds: presentation.activity_expires_at_unix_milliseconds,
        release_ref: carrier.response()?.submission_ref, fee_asset, operation,
        semantics: if limits.is_empty() { ProgramPresentationSemantics::OperationOnly }
            else { ProgramPresentationSemantics::AuthorizedLimits(limits) },
    })
}

fn principal_source_matches(prepared: &crate::prepare::Prepared, source: [u8; 32], asset: [u8; 32])
    -> Result<bool, ProgramPresentationError> {
    let actor = prepared.envelope.actor_did();
    match prepared.envelope.protocol_version() {
        1 | 2 => Ok(layerx_wire::hash::did_id_for_protocol(actor, prepared.envelope.protocol_version())
            .map_err(|_| ProgramPresentationError::Binding)? == source),
        3 => {
            let did = std::str::from_utf8(actor.as_bytes()).map_err(|_| ProgramPresentationError::Binding)?;
            let main = layerx_types::account::AccountId::parse(&format!("agent:{did}:main"))
                .map_err(|_| ProgramPresentationError::Binding)?;
            let asset_hex: String = asset.iter().map(|byte| format!("{byte:02x}")).collect();
            let asset_account = layerx_types::account::AccountId::parse(&format!("agent:{did}:asset:{asset_hex}"))
                .map_err(|_| ProgramPresentationError::Binding)?;
            Ok(layerx_wire::hash::account_id_for_protocol(&main, 3).map_err(|_| ProgramPresentationError::Binding)? == source
                || layerx_wire::hash::account_id_for_protocol(&asset_account, 3)
                    .map_err(|_| ProgramPresentationError::Binding)? == source)
        }
        _ => Err(ProgramPresentationError::Unsupported),
    }
}
