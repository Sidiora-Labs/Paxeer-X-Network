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
