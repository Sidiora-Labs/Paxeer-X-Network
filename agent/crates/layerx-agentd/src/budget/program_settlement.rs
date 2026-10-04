use std::collections::BTreeMap;

use layerx_programs_runtime::terminal::TerminalAttachment;
use layerx_programs_runtime::OccupancySettlement;
use layerx_proof::program::{
    verify_authorized_program_execution_with_payers, AuthorizedProgramExecutionExpectation,
    OccupancyPayer,
};
use layerx_types::activity::Authority;
use layerx_types::payload::{ModuleId, ModuleRegistry};
use layerx_types::program_call::NativeProgramCall;
use layerx_types::verify::VerificationLevel;
use sha2::{Digest as _, Sha256};

use crate::ops::program::ProgramOperations;
use crate::prepare::{verify_disclosure_binding, Prepared};
use crate::protocol_evidence::VerifiedReceiptEvidence;
use crate::store::{Store, TenantId};
use layerx_proof::receipt::AuthorizedBatch;
use crate::sign::VerifiedSubmission;

use super::program_sources::VerifiedResolvedProgramCharges;
use super::{ProgramBudgetReservation, ProgramChargeKind};

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum ProgramSettlementError {
    Preparation,
    UnsupportedOperation,
    Receipt,
    ArtifactsUnavailable,
    Terminal,
    SourceSnapshot,
    MissingAllocation,
    Allocation,
    UnreservedDebit,
    FeeProvenance,
    Occupancy,
    Arithmetic,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum ProgramPresentationBudgetError { Ownership, Binding, Missing, Lineage }

pub struct VerifiedProgramApprovalBudgetRow {
    approval_id: [u8; 32], held_digest: [u8; 32], budget_id: [u8; 32],
    asset: [u8; 32], source: [u8; 32], observed_sequence: u64,
    remaining_after_reservations: u128, terminal: bool,
    verification: VerificationLevel, evidence_digest: [u8; 32], receipt_digest: [u8; 32],
    checkpoint_digest: [u8; 32], age_sequences: u64, maximum_age_sequences: u64,
    proof_digest: [u8; 32], proof_bytes: Vec<u8>,
}

impl VerifiedProgramApprovalBudgetRow {
    pub const fn approval_id(&self) -> [u8; 32] { self.approval_id }
    pub const fn held_digest(&self) -> [u8; 32] { self.held_digest }
    pub const fn budget_id(&self) -> [u8; 32] { self.budget_id }
    pub const fn asset(&self) -> [u8; 32] { self.asset }
    pub const fn source(&self) -> [u8; 32] { self.source }
    pub const fn observed_sequence(&self) -> u64 { self.observed_sequence }
    pub const fn remaining_after_reservations(&self) -> u128 { self.remaining_after_reservations }
    pub const fn terminal(&self) -> bool { self.terminal }
    pub const fn verification(&self) -> VerificationLevel { self.verification }
    pub const fn evidence_digest(&self) -> [u8; 32] { self.evidence_digest }
    pub const fn receipt_digest(&self) -> [u8; 32] { self.receipt_digest }
    pub const fn checkpoint_digest(&self) -> [u8; 32] { self.checkpoint_digest }
    pub const fn age_sequences(&self) -> u64 { self.age_sequences }
    pub const fn maximum_age_sequences(&self) -> u64 { self.maximum_age_sequences }
    pub const fn proof_digest(&self) -> [u8; 32] { self.proof_digest }
    pub fn verified_proof_bytes(&self) -> &[u8] { &self.proof_bytes }
}

pub fn read_owned_program_budget(
    store: &Store, peer: &crate::human::HumanPeer, id: [u8; 32], held_digest: [u8; 32],
    registry: &ModuleRegistry, budgets: &super::BudgetLimiter,
    proof: &super::budget_proof::VerifiedBudgetProof, expected_sequence: u64,
) -> Result<VerifiedProgramApprovalBudgetRow, ProgramPresentationBudgetError> {
    use crate::approval::native_program_presentation::read_owned;
    use crate::prepare::DurablePreparation;
    let held = read_owned(store, peer, id, registry).map_err(|_| ProgramPresentationBudgetError::Ownership)?;
    if held.held_digest() != held_digest { return Err(ProgramPresentationBudgetError::Binding); }
    let tenant = TenantId::new(peer.tenant.clone()).map_err(|_| ProgramPresentationBudgetError::Ownership)?;
    let owners = crate::managed_agent::budget_owners(store, &tenant)
        .map_err(|_| ProgramPresentationBudgetError::Ownership)?;
    let actor = std::str::from_utf8(held.actor()).map_err(|_| ProgramPresentationBudgetError::Ownership)?;
    let mut selected = owners.iter().filter(|owner| owner.agent_did == actor);
    let (Some(owner), None) = (selected.next(), selected.next()) else {
        return Err(ProgramPresentationBudgetError::Ownership);
    };
    if owner.active_budget_id != proof.budget_id() || owner.agent_did != proof.owner() {
        return Err(ProgramPresentationBudgetError::Binding);
    }
    super::program_sources::bind_program_presentation_budget_proof(held.reservation(), held.actor(), proof, expected_sequence)
        .map_err(|_| ProgramPresentationBudgetError::Binding)?;
    let key = DurablePreparation::store_key(&tenant, id).map_err(|_| ProgramPresentationBudgetError::Missing)?;
    let raw = store.get(&key).ok_or(ProgramPresentationBudgetError::Missing)?;
    if raw.class() != crate::store::StorageClass::LocalOnly { return Err(ProgramPresentationBudgetError::Binding); }
    let durable = DurablePreparation::decode(tenant, raw.bytes()).map_err(|_| ProgramPresentationBudgetError::Binding)?;
    let encoded = held.reservation().encode().map_err(|_| ProgramPresentationBudgetError::Binding)?;
    if durable.preparation_id != id || durable.extensions.get(&6) != Some(&encoded)
        || durable.extensions.get(&7).map(Vec::as_slice) != Some(held_digest.as_slice())
    { return Err(ProgramPresentationBudgetError::Binding); }
    let terminal = durable.terminal();
    if !terminal && held.reservation().expiry_sequence <= expected_sequence {
        return Err(ProgramPresentationBudgetError::Binding);
    }
    let remaining = budgets.remaining_after_allocation_bound(held.reservation(), proof.asset(), proof.source_account(), proof.remaining(), terminal)
        .map_err(|_| ProgramPresentationBudgetError::Lineage)?;
    Ok(VerifiedProgramApprovalBudgetRow {
        approval_id: id, held_digest, budget_id: proof.budget_id(), asset: proof.asset(),
        source: proof.source_account(), observed_sequence: expected_sequence,
        remaining_after_reservations: remaining, terminal, verification: proof.verification(),
        evidence_digest: proof.evidence_digest(), receipt_digest: proof.receipt_digest(),
        checkpoint_digest: proof.checkpoint_digest(), age_sequences: proof.age_sequences(),
        maximum_age_sequences: proof.maximum_age_sequences(), proof_digest: proof.digest(),
        proof_bytes: proof.canonical_export_bytes().to_vec(),
    })
}

pub fn read_owned_native_effect_budget(
    store: &Store, peer: &crate::human::HumanPeer, id: [u8; 32], held_digest: [u8; 32],
    registry: &ModuleRegistry, budgets: &super::BudgetLimiter,
    proof: &super::budget_proof::VerifiedBudgetProof, expected_sequence: u64,
) -> Result<VerifiedProgramApprovalBudgetRow, ProgramPresentationBudgetError> {
    use crate::approval::native_effect::{NativeEffectApprovalCarrier, DURABLE_EXTENSION};
    use crate::prepare::DurablePreparation;
    let held = NativeEffectApprovalCarrier::read_for_human(store, peer, id)
        .map_err(|_| ProgramPresentationBudgetError::Ownership)?;
    if held.held_digest().map_err(|_| ProgramPresentationBudgetError::Binding)? != held_digest {
        return Err(ProgramPresentationBudgetError::Binding);
    }
    let prepared = held.restore_prepared(registry)
        .map_err(|_| ProgramPresentationBudgetError::Binding)?;
    let tenant = TenantId::new(peer.tenant.clone()).map_err(|_| ProgramPresentationBudgetError::Ownership)?;
    let owners = crate::managed_agent::budget_owners(store, &tenant)
        .map_err(|_| ProgramPresentationBudgetError::Ownership)?;
    let actor = std::str::from_utf8(held.actor()).map_err(|_| ProgramPresentationBudgetError::Ownership)?;
    let mut selected = owners.iter().filter(|owner| owner.agent_did == actor);
    let (Some(owner), None) = (selected.next(), selected.next()) else {
        return Err(ProgramPresentationBudgetError::Ownership);
    };
    if owner.active_budget_id != proof.budget_id() || owner.agent_did != proof.owner()
        || prepared.envelope.actor_did().as_bytes() != held.actor()
    { return Err(ProgramPresentationBudgetError::Binding); }
    let reservation = held.budget().map_err(|_| ProgramPresentationBudgetError::Binding)?;
    super::program_sources::bind_program_presentation_budget_proof(&reservation,
        held.actor(), proof, expected_sequence).map_err(|_| ProgramPresentationBudgetError::Binding)?;
    let key = DurablePreparation::store_key(&tenant, id).map_err(|_| ProgramPresentationBudgetError::Missing)?;
    let raw = store.get(&key).ok_or(ProgramPresentationBudgetError::Missing)?;
    if raw.class() != crate::store::StorageClass::LocalOnly { return Err(ProgramPresentationBudgetError::Binding); }
    let durable = DurablePreparation::decode(tenant, raw.bytes()).map_err(|_| ProgramPresentationBudgetError::Binding)?;
    if durable.preparation_id != id
        || durable.extensions.get(&6) != Some(&reservation.encode().map_err(|_| ProgramPresentationBudgetError::Binding)?)
        || durable.extensions.get(&DURABLE_EXTENSION).map(Vec::as_slice) != Some(held_digest.as_slice())
    { return Err(ProgramPresentationBudgetError::Binding); }
    let terminal = durable.terminal();
    if !terminal && reservation.expiry_sequence <= expected_sequence { return Err(ProgramPresentationBudgetError::Binding); }
    let remaining = budgets.remaining_after_allocation_bound(&reservation, proof.asset(),
        proof.source_account(), proof.remaining(), terminal).map_err(|_| ProgramPresentationBudgetError::Lineage)?;
    Ok(VerifiedProgramApprovalBudgetRow {
        approval_id: id, held_digest, budget_id: proof.budget_id(), asset: proof.asset(),
        source: proof.source_account(), observed_sequence: expected_sequence,
        remaining_after_reservations: remaining, terminal, verification: proof.verification(),
        evidence_digest: proof.evidence_digest(), receipt_digest: proof.receipt_digest(),
        checkpoint_digest: proof.checkpoint_digest(), age_sequences: proof.age_sequences(),
        maximum_age_sequences: proof.maximum_age_sequences(), proof_digest: proof.digest(),
        proof_bytes: proof.canonical_export_bytes().to_vec(),
    })
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ProgramExecutedDebit {
    pub kind: ProgramChargeKind,
    pub source: [u8; 32],
    pub asset: [u8; 32],
    pub destination: Option<[u8; 32]>,
    pub actual_amount: u128,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct VerifiedProgramDebitSettlement {
    reservation_id: [u8; 32],
    reservation_digest: [u8; 32],
    terminal_receipt: [u8; 32],
    activity_id: [u8; 32],
    global_sequence: u64,
    debits: Vec<ProgramExecutedDebit>,
}

impl VerifiedProgramDebitSettlement {
    pub const fn reservation_id(&self) -> [u8; 32] { self.reservation_id }
    pub const fn reservation_digest(&self) -> [u8; 32] { self.reservation_digest }
    pub const fn terminal_receipt(&self) -> [u8; 32] { self.terminal_receipt }
    pub const fn activity_id(&self) -> [u8; 32] { self.activity_id }
    pub const fn global_sequence(&self) -> u64 { self.global_sequence }
    pub fn debits(&self) -> &[ProgramExecutedDebit] { &self.debits }
}

#[path = "program_lifecycle_settlement.rs"]
mod lifecycle;

type DebitKey = ([u8; 32], [u8; 32], ProgramChargeKind, Option<[u8; 32]>);

pub fn read_program_debit_settlement(
    programs: &ProgramOperations,
    registry: &ModuleRegistry,
    prepared: &Prepared,
    submission: &VerifiedSubmission,
    receipt: &VerifiedReceiptEvidence,
    authority: &AuthorizedBatch,
    sources: &VerifiedResolvedProgramCharges,
    reservation: &ProgramBudgetReservation,
) -> Result<VerifiedProgramDebitSettlement, ProgramSettlementError> {
    if !sources.matches_prepared(prepared)
        || reservation.allocation_state_root() != Some(sources.state_root())
        || reservation.allocation_sequence() != Some(sources.global_sequence())
        || reservation.allocation_actor() != Some(sources.actor())
    { return Err(ProgramSettlementError::SourceSnapshot); }
    let mut expected = BTreeMap::<DebitKey, u128>::new();
    for row in sources.charges() {
        add(&mut expected, (row.asset, row.source.account(), row.source.kind(), row.destination), row.maximum_amount)?;
    }
    if retained_allocations(reservation)? != expected { return Err(ProgramSettlementError::Allocation); }
    verify_program_debit_settlement(programs, registry, prepared, submission, receipt, authority, reservation, None)
}

pub fn read_retained_program_debit_settlement(
    programs: &ProgramOperations,
    registry: &ModuleRegistry,
    prepared: &Prepared,
    submission: &VerifiedSubmission,
    receipt: &VerifiedReceiptEvidence,
    authority: &AuthorizedBatch,
    store: &Store,
    tenant: &TenantId,
) -> Result<(ProgramBudgetReservation, VerifiedProgramDebitSettlement), ProgramSettlementError> {
    read_retained_program_debit_settlement_inner(programs, registry, prepared, submission, receipt, authority, store, tenant, None)
}

pub fn read_retained_program_debit_settlement_at_execution(
    programs: &ProgramOperations,
    registry: &ModuleRegistry,
    prepared: &Prepared,
    submission: &VerifiedSubmission,
    receipt: &VerifiedReceiptEvidence,
    authority: &AuthorizedBatch,
    store: &Store,
    tenant: &TenantId,
    prestate: &layerx_client::evidence::VerifiedExecutionPrestate,
) -> Result<(ProgramBudgetReservation, VerifiedProgramDebitSettlement), ProgramSettlementError> {
    let sources = super::program_sources::execution_program_sources(prepared, submission, receipt, prestate)
        .map_err(|_| ProgramSettlementError::SourceSnapshot)?;
    read_retained_program_debit_settlement_inner(programs, registry, prepared, submission, receipt, authority, store, tenant, Some(&sources))
}

pub fn read_retained_wind_down_debit_settlement_at_execution(
    registry: &ModuleRegistry,
    prepared: &Prepared,
    submission: &VerifiedSubmission,
    receipt: &VerifiedReceiptEvidence,
    store: &Store,
    tenant: &TenantId,
    prestate: &layerx_client::evidence::VerifiedNativeExecutionPrestate,
) -> Result<(ProgramBudgetReservation, VerifiedProgramDebitSettlement), ProgramSettlementError> {
    use crate::prepare::{DurablePreparation, LifecycleState};
    let id: [u8; 32] = Sha256::digest(&prepared.canonical_bytes).into();
    let key = DurablePreparation::store_key(tenant, id).map_err(|_| ProgramSettlementError::Preparation)?;
    let stored = store.get(&key).ok_or(ProgramSettlementError::MissingAllocation)?;
    if stored.class() != crate::store::StorageClass::LocalOnly { return Err(ProgramSettlementError::Preparation); }
    let durable = DurablePreparation::decode(tenant.clone(), stored.bytes())
        .map_err(|_| ProgramSettlementError::Preparation)?;
    if durable.preparation_id != id || durable.payload_hash != prepared.envelope.payload_hash()
        || durable.activity_id != Some(submission.activity_id()) || !durable.holds.is_empty()
        || !matches!(durable.state, LifecycleState::Signed | LifecycleState::Submitted
            | LifecycleState::Acknowledged | LifecycleState::Unknown)
        || durable.signed_bytes().map_err(|_| ProgramSettlementError::Preparation)?.as_deref()
            != Some(submission.exact_bytes())
    { return Err(ProgramSettlementError::Preparation); }
    let encoded = durable.extensions.get(&6).ok_or(ProgramSettlementError::MissingAllocation)?;
    let reservation = ProgramBudgetReservation::decode(encoded).map_err(|_| ProgramSettlementError::Allocation)?;
    if reservation.id != id { return Err(ProgramSettlementError::Allocation); }
    let carrier = crate::approval::native_program::NativeProgramApprovalCarrier::retained_for_tenant(store, tenant)
        .map_err(|_| ProgramSettlementError::Preparation)?.into_iter()
        .find(|carrier| carrier.preparation_id() == id).ok_or(ProgramSettlementError::Preparation)?;
    let restored = carrier.restore_prepared(registry).map_err(|_| ProgramSettlementError::Preparation)?;
    if restored.canonical_bytes != prepared.canonical_bytes
        || restored.observed_head_sequence != prepared.observed_head_sequence
        || restored.envelope.authority() != prepared.envelope.authority()
        || carrier.budget().map_err(|_| ProgramSettlementError::Allocation)? != reservation
    { return Err(ProgramSettlementError::Preparation); }
    let debits = lifecycle::verify_wind_down_debits(prepared, submission, receipt, registry, prestate, &reservation)?;
    let witness = VerifiedProgramDebitSettlement {
        reservation_id: reservation.id,
        reservation_digest: reservation.settlement_binding().map_err(|_| ProgramSettlementError::Allocation)?,
        terminal_receipt: Sha256::digest(receipt.canonical_receipt()).into(),
        activity_id: submission.activity_id(), global_sequence: receipt.global_sequence(), debits,
    };
    Ok((reservation, witness))
}

pub fn read_retained_lifecycle_debit_settlement_at_execution(
    registry: &ModuleRegistry,
    prepared: &Prepared,
    submission: &VerifiedSubmission,
    receipt: &VerifiedReceiptEvidence,
    store: &Store,
    tenant: &TenantId,
    prestate: &layerx_client::evidence::VerifiedNativeExecutionPrestate,
) -> Result<(ProgramBudgetReservation, VerifiedProgramDebitSettlement), ProgramSettlementError> {
    use crate::prepare::{DurablePreparation, LifecycleState};
    let id: [u8; 32] = Sha256::digest(&prepared.canonical_bytes).into();
    let key = DurablePreparation::store_key(tenant, id).map_err(|_| ProgramSettlementError::Preparation)?;
    let stored = store.get(&key).ok_or(ProgramSettlementError::MissingAllocation)?;
    if stored.class() != crate::store::StorageClass::LocalOnly { return Err(ProgramSettlementError::Preparation); }
    let durable = DurablePreparation::decode(tenant.clone(), stored.bytes())
        .map_err(|_| ProgramSettlementError::Preparation)?;
    if durable.preparation_id != id || durable.payload_hash != prepared.envelope.payload_hash()
        || durable.activity_id != Some(submission.activity_id()) || !durable.holds.is_empty()
        || !matches!(durable.state, LifecycleState::Signed | LifecycleState::Submitted
            | LifecycleState::Acknowledged | LifecycleState::Unknown)
        || durable.signed_bytes().map_err(|_| ProgramSettlementError::Preparation)?.as_deref()
            != Some(submission.exact_bytes())
    { return Err(ProgramSettlementError::Preparation); }
    let encoded = durable.extensions.get(&6).ok_or(ProgramSettlementError::MissingAllocation)?;
    let reservation = ProgramBudgetReservation::decode(encoded).map_err(|_| ProgramSettlementError::Allocation)?;
    if reservation.id != id { return Err(ProgramSettlementError::Allocation); }
    let carrier = crate::approval::native_program::NativeProgramApprovalCarrier::retained_for_tenant(store, tenant)
        .map_err(|_| ProgramSettlementError::Preparation)?.into_iter()
        .find(|carrier| carrier.preparation_id() == id).ok_or(ProgramSettlementError::Preparation)?;
    let restored = carrier.restore_prepared(registry).map_err(|_| ProgramSettlementError::Preparation)?;
    if restored.canonical_bytes != prepared.canonical_bytes
        || restored.observed_head_sequence != prepared.observed_head_sequence
        || restored.envelope.authority() != prepared.envelope.authority()
        || carrier.budget().map_err(|_| ProgramSettlementError::Allocation)? != reservation
    { return Err(ProgramSettlementError::Preparation); }
    let debits = lifecycle::verify_remaining_lifecycle_debits(prepared, submission, receipt, registry, prestate, &reservation)?;
    let witness = VerifiedProgramDebitSettlement {
        reservation_id: reservation.id,
        reservation_digest: reservation.settlement_binding().map_err(|_| ProgramSettlementError::Allocation)?,
        terminal_receipt: Sha256::digest(receipt.canonical_receipt()).into(),
        activity_id: submission.activity_id(), global_sequence: receipt.global_sequence(), debits,
    };
    Ok((reservation, witness))
}

fn read_retained_program_debit_settlement_inner(
    programs: &ProgramOperations,
    registry: &ModuleRegistry,
    prepared: &Prepared,
    submission: &VerifiedSubmission,
    receipt: &VerifiedReceiptEvidence,
    authority: &AuthorizedBatch,
    store: &Store,
    tenant: &TenantId,
    execution_sources: Option<&super::program_sources::VerifiedExecutionProgramSources>,
) -> Result<(ProgramBudgetReservation, VerifiedProgramDebitSettlement), ProgramSettlementError> {
    use crate::prepare::{DurablePreparation, LifecycleState};
    let id: [u8; 32] = Sha256::digest(&prepared.canonical_bytes).into();
    let key = DurablePreparation::store_key(tenant, id).map_err(|_| ProgramSettlementError::Preparation)?;
    let stored = store.get(&key).ok_or(ProgramSettlementError::MissingAllocation)?;
    if stored.class() != crate::store::StorageClass::LocalOnly { return Err(ProgramSettlementError::Preparation); }
    let durable = DurablePreparation::decode(tenant.clone(), stored.bytes())
        .map_err(|_| ProgramSettlementError::Preparation)?;
    if durable.preparation_id != id || durable.payload_hash != prepared.envelope.payload_hash()
        || durable.activity_id != Some(submission.activity_id()) || !durable.holds.is_empty()
        || !matches!(durable.state, LifecycleState::Signed | LifecycleState::Submitted
            | LifecycleState::Acknowledged | LifecycleState::Unknown)
        || durable.signed_bytes().map_err(|_| ProgramSettlementError::Preparation)?.as_deref()
            != Some(submission.exact_bytes())
    { return Err(ProgramSettlementError::Preparation); }
    let encoded = durable.extensions.get(&6).ok_or(ProgramSettlementError::MissingAllocation)?;
    let reservation = ProgramBudgetReservation::decode(encoded).map_err(|_| ProgramSettlementError::Allocation)?;
    if reservation.id != id { return Err(ProgramSettlementError::Allocation); }
    let carrier = crate::approval::native_program::NativeProgramApprovalCarrier::retained_for_tenant(store, tenant)
        .map_err(|_| ProgramSettlementError::Preparation)?.into_iter()
        .find(|carrier| carrier.preparation_id() == id).ok_or(ProgramSettlementError::Preparation)?;
    let restored = carrier.restore_prepared(registry).map_err(|_| ProgramSettlementError::Preparation)?;
    if restored.canonical_bytes != prepared.canonical_bytes
        || restored.observed_head_sequence != prepared.observed_head_sequence
        || restored.envelope.authority() != prepared.envelope.authority()
        || carrier.budget().map_err(|_| ProgramSettlementError::Allocation)? != reservation
    { return Err(ProgramSettlementError::Preparation); }
    let witness = verify_program_debit_settlement(programs, registry, prepared, submission, receipt, authority, &reservation, execution_sources)?;
    Ok((reservation, witness))
}

pub(crate) fn read_retained_native_effect_debit_settlement(
    registry:&ModuleRegistry, prepared:&Prepared, submission:&VerifiedSubmission,
    receipt:&VerifiedReceiptEvidence, authority:&AuthorizedBatch,store:&Store,tenant:&TenantId,
)->Result<(ProgramBudgetReservation,VerifiedProgramDebitSettlement),ProgramSettlementError>{
    read_retained_native_effect_debit_settlement_inner(registry, prepared, submission,
        receipt, authority, store, tenant, None)
}

pub(crate) fn read_retained_native_effect_debit_settlement_at_execution(
    registry: &ModuleRegistry, prepared: &Prepared, submission: &VerifiedSubmission,
    receipt: &VerifiedReceiptEvidence, authority: &AuthorizedBatch, store: &Store,
    tenant: &TenantId, prestate: &layerx_client::evidence::VerifiedAssetExecutionPrestate,
) -> Result<(ProgramBudgetReservation, VerifiedProgramDebitSettlement), ProgramSettlementError> {
    read_retained_native_effect_debit_settlement_inner(registry, prepared, submission,
        receipt, authority, store, tenant, Some(prestate))
}

fn read_retained_native_effect_debit_settlement_inner(
    registry: &ModuleRegistry, prepared: &Prepared, submission: &VerifiedSubmission,
    receipt: &VerifiedReceiptEvidence, authority: &AuthorizedBatch, store: &Store,
    tenant: &TenantId, prestate: Option<&layerx_client::evidence::VerifiedAssetExecutionPrestate>,
) -> Result<(ProgramBudgetReservation, VerifiedProgramDebitSettlement), ProgramSettlementError> {
    use crate::prepare::{DurablePreparation,LifecycleState};
    verify_disclosure_binding(prepared).map_err(|_|ProgramSettlementError::Preparation)?;
    let activity=layerx_wire::activity::decode_signed(submission.exact_bytes(),registry).map_err(|_|ProgramSettlementError::Preparation)?;
    if activity.protocol_version()!=3||activity.activity_type().module()!=ModuleId::Asset||activity.activity_type().ordinal()!=5
        ||layerx_wire::activity::encode_unsigned(&activity).map_err(|_|ProgramSettlementError::Preparation)?!=prepared.canonical_bytes
        ||layerx_wire::hash::activity_id(&activity).map_err(|_|ProgramSettlementError::Preparation)?!=submission.activity_id(){return Err(ProgramSettlementError::UnsupportedOperation)}
    let id:[u8;32]=Sha256::digest(&prepared.canonical_bytes).into();
    let carrier=crate::approval::native_effect::NativeEffectApprovalCarrier::read(store,tenant,id).map_err(|_|ProgramSettlementError::Preparation)?;
    let restored=carrier.restore_prepared(registry).map_err(|_|ProgramSettlementError::Preparation)?;
    if restored.canonical_bytes!=prepared.canonical_bytes||restored.disclosure_digest!=prepared.disclosure_digest{return Err(ProgramSettlementError::Preparation)}
    let key=DurablePreparation::store_key(tenant,id).map_err(|_|ProgramSettlementError::Preparation)?;
    let raw=store.get(&key).ok_or(ProgramSettlementError::MissingAllocation)?;
    let durable=DurablePreparation::decode(tenant.clone(),raw.bytes()).map_err(|_|ProgramSettlementError::Preparation)?;
    if raw.class()!=crate::store::StorageClass::LocalOnly||durable.activity_id!=Some(submission.activity_id())
        ||!matches!(durable.state,LifecycleState::Signed|LifecycleState::Submitted|LifecycleState::Acknowledged|LifecycleState::Unknown)
        ||durable.signed_bytes().map_err(|_|ProgramSettlementError::Preparation)?.as_deref()!=Some(submission.exact_bytes()){
        return Err(ProgramSettlementError::Preparation)
    }
    let reservation=carrier.budget().map_err(|_|ProgramSettlementError::Allocation)?;
    let decoded=layerx_wire::receipt::decode(receipt.canonical_receipt()).map_err(|_|ProgramSettlementError::Receipt)?;
    let protocol=decoded.protocol().ok_or(ProgramSettlementError::Receipt)?;
    if receipt.level()<VerificationLevel::BATCH_INCLUDED||receipt.activity_id()!=submission.activity_id()
        ||protocol.activity_id()!=submission.activity_id()||protocol.protocol_version()!=3||protocol.module_id()!=1
        ||protocol.global_sequence()!=receipt.global_sequence()||protocol.batch_id()!=authority.batch_id()
        ||protocol.program_outcome().is_some(){return Err(ProgramSettlementError::Receipt)}
    if let Some(prestate) = prestate {
        let unsigned = layerx_wire::receipt::encode_unsigned(&decoded)
            .map_err(|_| ProgramSettlementError::Receipt)?;
        let digest = layerx_wire::hash::receipt_digest(&unsigned)
            .map_err(|_| ProgramSettlementError::Receipt)?;
        if prestate.network_id() != prepared.envelope.network_id()
            || prestate.activity_id() != submission.activity_id()
            || prestate.receipt_digest() != digest
            || prestate.execution_sequence() != protocol.global_sequence()
            || prestate.state_root() != protocol.previous_state_root()
            || prestate.execution_sequence() <= prepared.observed_head_sequence
            || prestate.fee_policy().parameter_version() != protocol.parameter_version()
            || layerx_wire::activity::encode_signed(prestate.activity())
                .map_err(|_| ProgramSettlementError::Preparation)? != submission.exact_bytes()
        { return Err(ProgramSettlementError::SourceSnapshot); }
    } else {
        if reservation.allocation_state_root()!=Some(protocol.previous_state_root())
            ||prepared.observed_head_sequence.checked_add(1)!=Some(protocol.global_sequence()){
            return Err(ProgramSettlementError::SourceSnapshot)
        }
    }
    let plan=crate::capability::derive_native_effects(&prepared.disclosure,&crate::capability::VerifiedInputs::default()).map_err(|_|ProgramSettlementError::Preparation)?;
    let [crate::capability::Effect::Transfer{from,to,asset,amount}]=plan.effects() else{return Err(ProgramSettlementError::UnsupportedOperation)};
    let rows=reservation.allocations().ok_or(ProgramSettlementError::MissingAllocation)?;
    let principal=rows.iter().find(|row|row.kind==ProgramChargeKind::Principal&&row.source==*from&&row.destination==Some(*to)&&row.asset==*asset)
        .ok_or(ProgramSettlementError::Allocation)?;
    if principal.maximum_amount!=*amount||rows.iter().filter(|row|row.kind!=ProgramChargeKind::Fee).count()!=1{return Err(ProgramSettlementError::Allocation)}
    if let Some(prestate) = prestate {
        reservation.validate().map_err(|_| ProgramSettlementError::Allocation)?;
        let actor = layerx_wire::hash::did_id_for_protocol(prepared.envelope.actor_did(), 3)
            .map_err(|_| ProgramSettlementError::Preparation)?;
        if reservation.id != id || reservation.allocation_actor() != Some(actor)
            || reservation.allocation_preparation_digest() != Some(id)
            || reservation.allocation_sequence() != Some(prepared.observed_head_sequence)
            || super::program_sources::principal_source(prestate.all_accounts(),
                prepared.envelope.actor_did(), 3, *asset)
                .map_err(|_| ProgramSettlementError::SourceSnapshot)? != *from
        { return Err(ProgramSettlementError::SourceSnapshot); }
        let mut fees = rows.iter().filter(|row| row.kind == ProgramChargeKind::Fee);
        match (fees.next(), fees.next()) {
            (Some(fee), None) if prepared.envelope.fee_limit().value() != 0 => {
                let active_asset = lifecycle::active_fee_asset(prestate.program_records())?;
                let account = super::program_sources::principal_source(prestate.all_accounts(),
                    prepared.envelope.actor_did(), 3, active_asset)
                    .map_err(|_| ProgramSettlementError::FeeProvenance)?;
                if fee.asset != active_asset || fee.asset != carrier.fee_asset()
                    || fee.source != account || fee.destination.is_some()
                    || fee.maximum_amount != prepared.envelope.fee_limit().value()
                { return Err(ProgramSettlementError::FeeProvenance); }
            }
            (None, None) if prepared.envelope.fee_limit().value() == 0 => {}
            _ => return Err(ProgramSettlementError::FeeProvenance),
        }
    }
    let mut debits=Vec::new();
    if protocol.result_code()==0 {
        if protocol.from()!=*from||protocol.to()!=*to||protocol.asset()!=*asset||protocol.amount()!=*amount{return Err(ProgramSettlementError::Terminal)}
        let mut leg=Vec::with_capacity(115);leg.push(0);leg.extend(from);leg.extend(to);leg.extend(asset);leg.extend(amount.to_be_bytes());leg.extend(1_u16.to_be_bytes());
        layerx_programs_runtime::transfer::verify_applied_kernel_legs(&leg,protocol.transfer_set_root()).map_err(|_|ProgramSettlementError::Terminal)?;
        debits.push(ProgramExecutedDebit{kind:ProgramChargeKind::Principal,source:*from,asset:*asset,destination:Some(*to),actual_amount:*amount});
    }else if protocol.transfer_set_root()!=[0;32]||protocol.effects().iter().any(|effect|effect.monetary()) {return Err(ProgramSettlementError::Terminal)}
    if protocol.fee_charged()>prepared.envelope.fee_limit().value(){return Err(ProgramSettlementError::FeeProvenance)}
    if protocol.fee_charged()!=0 {
        let mut fees=rows.iter().filter(|row|row.kind==ProgramChargeKind::Fee);
        let (Some(fee),None)=(fees.next(),fees.next())else{return Err(ProgramSettlementError::FeeProvenance)};
        if fee.asset!=carrier.fee_asset()||fee.destination.is_some()||protocol.fee_charged()>fee.maximum_amount{return Err(ProgramSettlementError::FeeProvenance)}
        debits.push(ProgramExecutedDebit{kind:ProgramChargeKind::Fee,source:fee.source,asset:fee.asset,destination:None,actual_amount:protocol.fee_charged()});
    }
    let witness=VerifiedProgramDebitSettlement{reservation_id:id,reservation_digest:reservation.settlement_binding().map_err(|_|ProgramSettlementError::Allocation)?,
        terminal_receipt:Sha256::digest(receipt.canonical_receipt()).into(),activity_id:submission.activity_id(),global_sequence:receipt.global_sequence(),debits};
    Ok((reservation,witness))
}

fn retained_allocations(reservation: &ProgramBudgetReservation) -> Result<BTreeMap<DebitKey, u128>, ProgramSettlementError> {
    let mut retained = BTreeMap::<DebitKey, u128>::new();
    for row in reservation.allocations().ok_or(ProgramSettlementError::MissingAllocation)? {
        add(&mut retained, (row.asset, row.source, row.kind, row.destination), row.maximum_amount)?;
    }
    Ok(retained)
}

fn verify_program_debit_settlement(
    programs: &ProgramOperations,
    registry: &ModuleRegistry,
    prepared: &Prepared,
    submission: &VerifiedSubmission,
    receipt: &VerifiedReceiptEvidence,
    authority: &AuthorizedBatch,
    reservation: &ProgramBudgetReservation,
    execution_sources: Option<&super::program_sources::VerifiedExecutionProgramSources>,
) -> Result<VerifiedProgramDebitSettlement, ProgramSettlementError> {
    verify_disclosure_binding(prepared).map_err(|_| ProgramSettlementError::Preparation)?;
    if !matches!(prepared.envelope.authority(), Authority::Owner(_)) {
        return Err(ProgramSettlementError::Preparation);
    }
    let activity = layerx_wire::activity::decode_signed(submission.exact_bytes(), registry)
        .map_err(|_| ProgramSettlementError::Preparation)?;
    if layerx_wire::activity::encode_unsigned(&activity)
        .map_err(|_| ProgramSettlementError::Preparation)? != prepared.canonical_bytes
        || layerx_wire::hash::activity_id(&activity)
            .map_err(|_| ProgramSettlementError::Preparation)? != submission.activity_id()
    { return Err(ProgramSettlementError::Preparation); }
    if activity.protocol_version() != 3 || activity.activity_type().module() != ModuleId::Programs
        || activity.activity_type().ordinal() != 3
    { return Err(ProgramSettlementError::UnsupportedOperation); }
    let call = NativeProgramCall::decode(activity.payload())
        .map_err(|_| ProgramSettlementError::UnsupportedOperation)?;
    let canonical_receipt = receipt.canonical_receipt();
    let decoded = layerx_wire::receipt::decode(canonical_receipt)
        .map_err(|_| ProgramSettlementError::Receipt)?;
    let protocol = decoded.protocol().ok_or(ProgramSettlementError::Receipt)?;
    if receipt.level() < VerificationLevel::BATCH_INCLUDED
        || receipt.activity_id() != submission.activity_id()
        || protocol.activity_id() != submission.activity_id()
        || protocol.protocol_version() != activity.protocol_version()
        || receipt.global_sequence() != protocol.global_sequence()
    { return Err(ProgramSettlementError::Receipt); }
    let actor = layerx_wire::hash::did_id_for_protocol(prepared.envelope.actor_did(), 3)
        .map_err(|_| ProgramSettlementError::Preparation)?;
    reservation.validate().map_err(|_| ProgramSettlementError::Allocation)?;
    let preparation_digest: [u8; 32] = Sha256::digest(&prepared.canonical_bytes).into();
    if reservation.id != preparation_digest
        || reservation.allocation_sequence() != Some(prepared.observed_head_sequence)
        || reservation.allocation_actor() != Some(actor)
        || reservation.allocation_preparation_digest() != Some(preparation_digest)
    { return Err(ProgramSettlementError::SourceSnapshot); }
    let expected = retained_allocations(reservation)?;
    if let Some(sources) = execution_sources {
        if !sources.matches(prepared, receipt) { return Err(ProgramSettlementError::SourceSnapshot); }
        let mut executed_sources = BTreeMap::<DebitKey, u128>::new();
        for row in sources.charges() {
            add(&mut executed_sources, (row.asset, row.source.account(), row.source.kind(), row.destination), row.maximum_amount)?;
        }
        if executed_sources != expected { return Err(ProgramSettlementError::UnreservedDebit); }
    } else if reservation.allocation_state_root() != Some(protocol.previous_state_root())
        || prepared.observed_head_sequence.checked_add(1) != Some(protocol.global_sequence()) {
        return Err(ProgramSettlementError::SourceSnapshot);
    }
    let outcome = protocol.program_outcome().ok_or(ProgramSettlementError::Terminal)?;
    if outcome.encoding_version() != 4 { return Err(ProgramSettlementError::Terminal); }
    let artifacts = programs.activity_execution(registry, submission.exact_bytes(), canonical_receipt,
        *authority).map_err(|_| ProgramSettlementError::ArtifactsUnavailable)?;
    let verified = verify_authorized_program_execution_with_payers(canonical_receipt,
        &artifacts.terminal_payload, artifacts.execution.call_graph(),
        &AuthorizedProgramExecutionExpectation {
            authority: *authority, activity_id: submission.activity_id(),
            payload_hash: layerx_wire::hash::payload_hash(&activity)
                .map_err(|_| ProgramSettlementError::Preparation)?,
            program_id: call.program_id.bytes(), guest_abi_version: call.guest_abi,
        }, &[OccupancyPayer { did: activity.actor_did(), account: None }])
        .map_err(|_| ProgramSettlementError::Terminal)?;
    let mut actual = BTreeMap::<DebitKey, u128>::new();
    if protocol.result_code() == 0 {
        let (_, legs) = layerx_wire::receipt::decode_applied_terminal(&artifacts.terminal_payload)
            .map_err(|_| ProgramSettlementError::Terminal)?;
        layerx_programs_runtime::transfer::verify_applied_kernel_legs(legs, outcome.transfer_root())
            .map_err(|_| ProgramSettlementError::Terminal)?;
        for leg in legs.chunks_exact(115) {
            let source = array(&leg[1..33])?;
            let destination = array(&leg[33..65])?;
            let asset = array(&leg[65..97])?;
            let amount = u128::from_be_bytes(leg[97..113].try_into()
                .map_err(|_| ProgramSettlementError::Terminal)?);
            let mut matching = expected.keys().filter(|(a, s, k, d)| *a == asset && *s == source
                && *k != ProgramChargeKind::Fee && *d == Some(destination));
            let key = *matching.next().ok_or(ProgramSettlementError::UnreservedDebit)?;
            if matching.next().is_some() { return Err(ProgramSettlementError::UnreservedDebit); }
            add(&mut actual, key, amount)?;
        }
    }
    let fee = protocol.fee_charged();
    if fee > activity.fee_limit() { return Err(ProgramSettlementError::FeeProvenance); }
    if fee != 0 {
        let mut fee_rows = expected.keys().filter(|(_, _, kind, destination)|
            *kind == ProgramChargeKind::Fee && destination.is_none());
        let key = *fee_rows.next().ok_or(ProgramSettlementError::FeeProvenance)?;
        if fee_rows.next().is_some()
            || (protocol.result_code() == 0 && key.0 != outcome.occupancy_asset_id())
        { return Err(ProgramSettlementError::FeeProvenance); }
        add(&mut actual, key, fee)?;
    }
    let mut occupancy_seen = false;
    for attachment in &verified.terminal().attachments {
        if let TerminalAttachment::Occupancy(bytes) = attachment {
            if occupancy_seen { return Err(ProgramSettlementError::Occupancy); }
            occupancy_seen = true;
            let settlement = OccupancySettlement::canonical_decode(bytes)
                .map_err(|_| ProgramSettlementError::Occupancy)?;
            for (payer, (_, paid, _, _)) in settlement.payer_dispositions()
                .map_err(|_| ProgramSettlementError::Occupancy)? {
                if paid == 0 { continue; }
                let mut accounts = verified.occupancy_payment_accounts().iter().filter(|account|
                    account.payer() == payer && account.asset_id() == outcome.occupancy_asset_id());
                let account = accounts.next().ok_or(ProgramSettlementError::Occupancy)?;
                if accounts.next().is_some() { return Err(ProgramSettlementError::Occupancy); }
                let key = (account.asset_id(), account.account(), ProgramChargeKind::Fee, None);
                if !expected.contains_key(&key) { return Err(ProgramSettlementError::UnreservedDebit); }
                add(&mut actual, key, paid)?;
            }
        }
    }
    if !occupancy_seen && outcome.occupancy_fee_units() != 0 {
        return Err(ProgramSettlementError::Occupancy);
    }
    let mut debits = Vec::with_capacity(actual.len());
    for ((asset, source, kind, destination), actual_amount) in actual {
        if actual_amount > *expected.get(&(asset, source, kind, destination))
            .ok_or(ProgramSettlementError::UnreservedDebit)?
        { return Err(ProgramSettlementError::UnreservedDebit); }
        debits.push(ProgramExecutedDebit { kind, source, asset, destination, actual_amount });
    }
    Ok(VerifiedProgramDebitSettlement {
        reservation_id: reservation.id,
        reservation_digest: reservation.settlement_binding().map_err(|_| ProgramSettlementError::Allocation)?,
        terminal_receipt: Sha256::digest(canonical_receipt).into(),
        activity_id: submission.activity_id(), global_sequence: protocol.global_sequence(), debits,
    })
}

fn add(rows: &mut BTreeMap<DebitKey, u128>, key: DebitKey, amount: u128)
    -> Result<(), ProgramSettlementError> {
    if amount == 0 { return Ok(()); }
    let total = rows.entry(key).or_default();
    *total = total.checked_add(amount).ok_or(ProgramSettlementError::Arithmetic)?;
    Ok(())
}

fn array(bytes: &[u8]) -> Result<[u8; 32], ProgramSettlementError> {
    bytes.try_into().map_err(|_| ProgramSettlementError::Terminal)
}
