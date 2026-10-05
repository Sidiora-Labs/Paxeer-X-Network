//! Deterministic protocol-owned expiry and terminal reclamation.

use core::fmt::{self, Display};

use layerx_programs::ProtocolDeploymentVerifier;
use layerx_programs_runtime::{hash_bytes, HashAlgorithm};
#[cfg(any(feature = "host-ffi", test))]
use layerx_programs_runtime::{Meter, Storage};
use layerx_proof::merkle::Proof;
use layerx_types::payload::ModuleId;
use layerx_wire::receipt::decode as decode_receipt;

#[cfg(any(feature = "host-ffi", test))]
use crate::usage::record_expiry_occupancy_settlement;
#[cfg(any(feature = "host-ffi", test))]
use crate::{DurableUsageState, UsageReceipt};
use crate::{EscrowRefusal, Lease, LeaseId, LeaseRefusal, LeaseState, UsageRefusal};

const TERMINAL_DOMAIN: &[u8] = b"LayerX/programs/sandbox/terminal/v1\0";
#[cfg(any(feature = "host-ffi", test))]
const DESTROY_RECEIPT_DOMAIN: &[u8] = b"LayerX/programs/sandbox/destroy/v1\0";
const SWEEP_STATE_DOMAIN: &[u8] = b"LayerX/programs/sandbox/expiry-queue/v1\0";
const TERMINAL_EVENT: u16 = 0x090a;
const TERMINAL_EVENT_BYTES: usize = 352;
pub const MAX_SWEEP_LEASES_PER_BATCH: u32 = 6;
pub const MAX_EXPIRY_QUEUE_ENTRIES: usize = 4096;

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct TerminalReceiptEvidence {
    pub canonical_receipt: Vec<u8>,
    pub receipt_proof: Proof,
    pub canonical_header: Vec<u8>,
    pub header_signature: [u8; 64],
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct AuthenticatedTerminalRecord {
    canonical: [u8; TERMINAL_EVENT_BYTES],
    receipt_activity_id: [u8; 32],
    lease: LeaseId,
}

impl AuthenticatedTerminalRecord {
    /// # Errors
    ///
    /// Returns a refusal when terminal evidence or its authenticated receipt is invalid.
    pub fn verify(
        verifier: &ProtocolDeploymentVerifier,
        evidence: &TerminalReceiptEvidence,
        now_ms: u64,
        expected_lease: LeaseId,
    ) -> Result<Self, ExpiryRefusal> {
        let head = verifier
            .verify_current_protocol_head(
                &evidence.canonical_receipt,
                &evidence.receipt_proof,
                &evidence.canonical_header,
                &evidence.header_signature,
                now_ms,
            )
            .map_err(|_| ExpiryRefusal::InvalidProtocolReceipt)?;
        let receipt = decode_receipt(&evidence.canonical_receipt)
            .map_err(|_| ExpiryRefusal::InvalidProtocolReceipt)?;
        let protocol = receipt
            .protocol()
            .ok_or(ExpiryRefusal::InvalidProtocolReceipt)?;
        if protocol.activity_id() != head.activity_id() {
            return Err(ExpiryRefusal::InvalidProtocolReceipt);
        }
        let mut chunks: [Option<Vec<u8>>; 2] = [None, None];
        for effect in protocol.effects().iter().filter(|effect| {
            effect.module_id() == ModuleId::Programs as u16 && effect.event_type() == TERMINAL_EVENT
        }) {
            let body = effect.body();
            if body.len() < 8
                || &body[..4] != b"LXDT"
                || body[5] != 2
                || body[6] != 1
                || body[7] != 0
                || usize::from(body[4]) >= chunks.len()
                || chunks[usize::from(body[4])].is_some()
            {
                return Err(ExpiryRefusal::InvalidProtocolReceipt);
            }
            chunks[usize::from(body[4])] = Some(body[8..].to_vec());
        }
        let mut bytes = Vec::with_capacity(TERMINAL_EVENT_BYTES);
        for chunk in chunks {
            bytes.extend_from_slice(&chunk.ok_or(ExpiryRefusal::InvalidProtocolReceipt)?);
        }
        if bytes.len() != TERMINAL_EVENT_BYTES
            || &bytes[..5] != b"LXSD1"
            || bytes[5..37] != expected_lease.bytes()
        {
            return Err(ExpiryRefusal::InvalidProtocolReceipt);
        }
        let mut canonical = [0u8; TERMINAL_EVENT_BYTES];
        canonical.copy_from_slice(&bytes);
        if canonical[37..69] == [0; 32]
            || canonical[117..149] == [0; 32]
            || u64::from_be_bytes(
                canonical[69..77]
                    .try_into()
                    .map_err(|_| ExpiryRefusal::InvalidProtocolReceipt)?,
            ) == 0
            || u64::from_be_bytes(
                canonical[77..85]
                    .try_into()
                    .map_err(|_| ExpiryRefusal::InvalidProtocolReceipt)?,
            ) == 0
        {
            return Err(ExpiryRefusal::InvalidProtocolReceipt);
        }
        Ok(Self {
            canonical,
            receipt_activity_id: head.activity_id(),
            lease: expected_lease,
        })
    }
    #[must_use]
    pub fn canonical_bytes(&self) -> &[u8; TERMINAL_EVENT_BYTES] {
        &self.canonical
    }
    #[must_use]
    pub const fn receipt_activity_id(&self) -> [u8; 32] {
        self.receipt_activity_id
    }
    #[must_use]
    pub fn destroy_activity_id(&self) -> [u8; 32] {
        let mut id = [0u8; 32];
        id.copy_from_slice(&self.canonical[37..69]);
        id
    }
    #[must_use]
    pub const fn lease(&self) -> LeaseId {
        self.lease
    }
}

/// The protocol-owned sweep ordinal that destroys one lease at a batch boundary.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct DestroyAuthority {
    pub activity_id: [u8; 32],
    pub expected_lease_root: [u8; 32],
    pub expected_sequence: u64,
    pub boundary: u64,
}

/// Exact kernel transfers one teardown requires, derived only from protocol state.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct TeardownPlan {
    authority: DestroyAuthority,
    occupancy_charge: u128,
    refund: u128,
}

impl TeardownPlan {
    #[must_use]
    pub const fn authority(self) -> DestroyAuthority {
        self.authority
    }
    #[must_use]
    pub const fn occupancy_charge(self) -> u128 {
        self.occupancy_charge
    }
    #[must_use]
    pub const fn refund(self) -> u128 {
        self.refund
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct TeardownSettlement {
    receipt_digest: [u8; 32],
    final_usage_receipt: [u8; 32],
    usage_lease_state: Vec<u8>,
    ledger_state: Vec<u8>,
    refund_root: [u8; 32],
}

impl TeardownSettlement {
    #[must_use]
    pub const fn receipt_digest(&self) -> [u8; 32] {
        self.receipt_digest
    }
    #[must_use]
    pub const fn final_usage_receipt(&self) -> [u8; 32] {
        self.final_usage_receipt
    }
    #[must_use]
    pub fn usage_lease_state(&self) -> &[u8] {
        &self.usage_lease_state
    }
    #[must_use]
    pub fn ledger_state(&self) -> &[u8] {
        &self.ledger_state
    }
    #[must_use]
    pub const fn refund_root(&self) -> [u8; 32] {
        self.refund_root
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct TerminalLeaseRecord {
    lease: LeaseId,
    tenant: [u8; 32],
    host_program: [u8; 32],
    expiry: u64,
    destroyed_at: u64,
    reclaimed_cells: u64,
    reclaimed_bytes: u64,
    metered_cleanup_work: u64,
    occupancy_charged: u128,
    refunded: u128,
    refund_transfer_root: [u8; 32],
    expiry_receipt_digest: [u8; 32],
    destroy_receipt_digest: [u8; 32],
    prior_lease_digest: [u8; 32],
    terminal_digest: [u8; 32],
}

impl TerminalLeaseRecord {
    #[must_use]
    pub const fn lease(&self) -> LeaseId {
        self.lease
    }
    #[must_use]
    pub const fn destroyed_at(&self) -> u64 {
        self.destroyed_at
    }
    #[must_use]
    pub const fn occupancy_charged(&self) -> u128 {
        self.occupancy_charged
    }
    #[must_use]
    pub const fn refunded(&self) -> u128 {
        self.refunded
    }
    #[must_use]
    pub const fn reclaimed_cells(&self) -> u64 {
        self.reclaimed_cells
    }
    #[must_use]
    pub const fn reclaimed_bytes(&self) -> u64 {
        self.reclaimed_bytes
    }
    #[must_use]
    pub const fn terminal_digest(&self) -> [u8; 32] {
        self.terminal_digest
    }

    #[must_use]
    pub fn canonical_bytes(&self) -> Vec<u8> {
        let mut bytes = Vec::with_capacity(TERMINAL_DOMAIN.len() + 304);
        bytes.extend_from_slice(TERMINAL_DOMAIN);
        bytes.extend_from_slice(&self.lease.bytes());
        bytes.extend_from_slice(&self.tenant);
        bytes.extend_from_slice(&self.host_program);
        bytes.extend_from_slice(&self.expiry.to_be_bytes());
        bytes.extend_from_slice(&self.destroyed_at.to_be_bytes());
        bytes.extend_from_slice(&self.reclaimed_cells.to_be_bytes());
        bytes.extend_from_slice(&self.reclaimed_bytes.to_be_bytes());
        bytes.extend_from_slice(&self.metered_cleanup_work.to_be_bytes());
        bytes.extend_from_slice(&self.occupancy_charged.to_be_bytes());
        bytes.extend_from_slice(&self.refunded.to_be_bytes());
        bytes.extend_from_slice(&self.refund_transfer_root);
        bytes.extend_from_slice(&self.expiry_receipt_digest);
        bytes.extend_from_slice(&self.destroy_receipt_digest);
        bytes.extend_from_slice(&self.prior_lease_digest);
        bytes
    }

    /// # Errors
    ///
    /// Returns a refusal when terminal evidence or its authenticated receipt is invalid.
    pub fn verify(&self) -> Result<(), ExpiryRefusal> {
        let digest = hash_bytes(HashAlgorithm::Sha256, &self.canonical_bytes())
            .map_err(|_| ExpiryRefusal::HashRefusal)?;
        if digest != self.terminal_digest
            || self.destroyed_at < self.expiry
            || self.metered_cleanup_work
                != self
                    .reclaimed_cells
                    .checked_add(self.reclaimed_bytes)
                    .ok_or(ExpiryRefusal::AccountingOverflow)?
        {
            return Err(ExpiryRefusal::InvalidTerminalRecord);
        }
        Ok(())
    }
}

/// Derives the exact final occupancy charge and refund a teardown settles at its boundary.
///
/// # Errors
///
/// Returns a refusal for invalid authority, a lease not yet due or already destroyed, a stale
/// lease root, an unverifiable usage ledger, or an escrow that cannot cover final occupancy.
#[cfg(any(feature = "host-ffi", test))]
pub fn plan_teardown(
    state: &DurableUsageState,
    authority: DestroyAuthority,
) -> Result<TeardownPlan, ExpiryRefusal> {
    let lease = &state.lease;
    if authority.activity_id == [0; 32] || authority.expected_sequence == 0 {
        return Err(ExpiryRefusal::InvalidEvidence);
    }
    if lease.state() == LeaseState::Destroyed {
        return Err(ExpiryRefusal::Replay);
    }
    if authority.boundary < lease.expiry() {
        return Err(ExpiryRefusal::NotDue);
    }
    if lease.state_digest().map_err(ExpiryRefusal::Lease)? != authority.expected_lease_root {
        return Err(ExpiryRefusal::InvalidEvidence);
    }
    state
        .ledger
        .verify(lease, &state.escrow)
        .map_err(ExpiryRefusal::Usage)?;
    let prior_batch = state
        .ledger
        .latest()
        .map_or(lease.opened_at(), UsageReceipt::observed_batch);
    let occupancy_charge = lease
        .expiry()
        .checked_sub(prior_batch)
        .and_then(|batches| {
            u128::from(lease.usage().namespace_bytes).checked_mul(u128::from(batches))
        })
        .and_then(|byte_batches| {
            byte_batches.checked_mul(u128::from(
                lease.fee_schedule().occupancy_byte_batch_price(),
            ))
        })
        .ok_or(ExpiryRefusal::AccountingOverflow)?;
    let remaining = state.escrow.remaining().map_err(ExpiryRefusal::Escrow)?;
    let refund = remaining
        .checked_sub(occupancy_charge)
        .ok_or(ExpiryRefusal::Escrow(EscrowRefusal::EscrowExhausted {
            requested: occupancy_charge,
            remaining,
        }))?;
    Ok(TeardownPlan {
        authority,
        occupancy_charge,
        refund,
    })
}

/// Settles a planned teardown for the host: final occupancy, terminal transition and refund.
///
/// # Errors
///
/// Returns a refusal when the plan is stale, a kernel root does not match its amount, or any
/// settlement step fails; the state is then left exactly as committed.
#[cfg(any(feature = "host-ffi", test))]
pub fn settle_teardown(
    state: &mut DurableUsageState,
    plan: TeardownPlan,
    occupancy_root: [u8; 32],
    refund_root: [u8; 32],
) -> Result<TeardownSettlement, ExpiryRefusal> {
    let DestroyAuthority {
        activity_id,
        boundary,
        ..
    } = plan.authority;
    let (next, settlement) =
        settle_with(state, plan, occupancy_root, refund_root, |lease, digest| {
            lease
                .terminalize_by_sweep(activity_id, digest, boundary)
                .map_err(ExpiryRefusal::Lease)
        })?;
    *state = next;
    Ok(settlement)
}

/// Destroys one due lease atomically: drops its namespace and snapshot cells, settles final
/// occupancy, refunds the remainder and returns its terminal record.
///
/// # Errors
///
/// Returns a refusal when the plan is stale, a kernel root does not match its amount, or
/// reclamation, settlement or terminal accounting fails; nothing is then mutated.
#[cfg(any(feature = "host-ffi", test))]
pub fn destroy(
    storage: &mut Storage,
    meter: &mut Meter,
    state: &mut DurableUsageState,
    plan: TeardownPlan,
    occupancy_root: [u8; 32],
    refund_root: [u8; 32],
) -> Result<TerminalLeaseRecord, ExpiryRefusal> {
    let DestroyAuthority {
        activity_id,
        expected_lease_root,
        boundary,
        ..
    } = plan.authority;
    let mut candidate_storage = storage.clone();
    let mut candidate_meter = meter.clone();
    let mut reclaimed = (0u64, 0u64);
    let (next, settlement) =
        settle_with(state, plan, occupancy_root, refund_root, |lease, digest| {
            if lease.state() != LeaseState::Expired {
                lease
                    .expire_by_sweep(activity_id, digest, boundary)
                    .map_err(ExpiryRefusal::Lease)?;
            }
            let (_, cells, bytes) = lease
                .destroy_by_sweep(
                    &mut candidate_storage,
                    &mut candidate_meter,
                    flipped(activity_id),
                    flipped(digest),
                    boundary,
                )
                .map_err(ExpiryRefusal::Lease)?;
            reclaimed = (cells, bytes);
            Ok(())
        })?;
    let (reclaimed_cells, reclaimed_bytes) = reclaimed;
    let mut record = TerminalLeaseRecord {
        lease: next.lease.id(),
        tenant: next.lease.tenant().bytes(),
        host_program: next.lease.host_program().bytes(),
        expiry: next.lease.expiry(),
        destroyed_at: boundary,
        reclaimed_cells,
        reclaimed_bytes,
        metered_cleanup_work: reclaimed_cells
            .checked_add(reclaimed_bytes)
            .ok_or(ExpiryRefusal::AccountingOverflow)?,
        occupancy_charged: plan.occupancy_charge,
        refunded: plan.refund,
        refund_transfer_root: refund_root,
        expiry_receipt_digest: settlement.receipt_digest,
        destroy_receipt_digest: flipped(settlement.receipt_digest),
        prior_lease_digest: expected_lease_root,
        terminal_digest: [0; 32],
    };
    record.terminal_digest = hash_bytes(HashAlgorithm::Sha256, &record.canonical_bytes())
        .map_err(|_| ExpiryRefusal::HashRefusal)?;
    record.verify()?;
    *state = next;
    *storage = candidate_storage;
    *meter = candidate_meter;
    Ok(record)
}

#[cfg(any(feature = "host-ffi", test))]
fn settle_with<F>(
    state: &DurableUsageState,
    plan: TeardownPlan,
    occupancy_root: [u8; 32],
    refund_root: [u8; 32],
    terminate: F,
) -> Result<(DurableUsageState, TeardownSettlement), ExpiryRefusal>
where
    F: FnOnce(&mut Lease, [u8; 32]) -> Result<(), ExpiryRefusal>,
{
    let authority = plan.authority;
    if plan_teardown(state, authority)? != plan {
        return Err(ExpiryRefusal::InvalidEvidence);
    }
    if (plan.occupancy_charge == 0) != (occupancy_root == [0; 32])
        || (plan.refund == 0) != (refund_root == [0; 32])
    {
        return Err(ExpiryRefusal::RefundMismatch);
    }
    let mut next = state.clone();
    let mut usage_lease_state = next
        .lease
        .canonical_state_bytes()
        .map_err(ExpiryRefusal::Lease)?;
    let mut final_usage_receipt = [0; 32];
    if plan.occupancy_charge != 0 {
        let mut receipt_bytes = Vec::new();
        let receipt = record_expiry_occupancy_settlement(
            &mut next,
            authority.activity_id,
            occupancy_root,
            &mut usage_lease_state,
            &mut receipt_bytes,
        )
        .map_err(ExpiryRefusal::Usage)?;
        if receipt.charged() != plan.occupancy_charge {
            return Err(ExpiryRefusal::RefundMismatch);
        }
        final_usage_receipt = receipt.digest();
    }
    let ledger_state = next
        .ledger
        .canonical_state()
        .map_err(ExpiryRefusal::Usage)?;
    let receipt_digest = destroy_receipt_digest(next.lease.id(), authority, final_usage_receipt)?;
    terminate(&mut next.lease, receipt_digest)?;
    next.escrow
        .finalize_refund(&next.lease, plan.refund, refund_root)
        .map_err(ExpiryRefusal::Escrow)?;
    Ok((
        next,
        TeardownSettlement {
            receipt_digest,
            final_usage_receipt,
            usage_lease_state,
            ledger_state,
            refund_root,
        },
    ))
}

#[cfg(any(feature = "host-ffi", test))]
fn destroy_receipt_digest(
    lease: LeaseId,
    authority: DestroyAuthority,
    final_usage_receipt: [u8; 32],
) -> Result<[u8; 32], ExpiryRefusal> {
    let mut preimage = Vec::with_capacity(DESTROY_RECEIPT_DOMAIN.len() + 144);
    preimage.extend_from_slice(DESTROY_RECEIPT_DOMAIN);
    preimage.extend_from_slice(&authority.activity_id);
    preimage.extend_from_slice(&lease.bytes());
    preimage.extend_from_slice(&authority.expected_lease_root);
    preimage.extend_from_slice(&authority.expected_sequence.to_be_bytes());
    preimage.extend_from_slice(&authority.boundary.to_be_bytes());
    preimage.extend_from_slice(&final_usage_receipt);
    let mut digest =
        hash_bytes(HashAlgorithm::Sha256, &preimage).map_err(|_| ExpiryRefusal::HashRefusal)?;
    digest[0] ^= 0x40;
    Ok(digest)
}

#[cfg(any(feature = "host-ffi", test))]
const fn flipped(mut value: [u8; 32]) -> [u8; 32] {
    value[0] ^= 0x80;
    value
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ExpiryQueue {
    scheduled: Vec<(u64, LeaseId)>,
    terminal: Vec<TerminalLeaseRecord>,
}

impl ExpiryQueue {
    #[must_use]
    pub const fn new() -> Self {
        Self {
            scheduled: Vec::new(),
            terminal: Vec::new(),
        }
    }

    /// # Errors
    ///
    /// Returns a refusal for an invalid lease, duplicate entry or exceeded queue bound.
    pub fn schedule(&mut self, lease: &Lease) -> Result<(), ExpiryRefusal> {
        let key = (lease.expiry(), lease.id());
        if lease.state() == LeaseState::Destroyed
            || self.terminal.iter().any(|v| v.lease == lease.id())
            || self.scheduled.binary_search(&key).is_ok()
        {
            return Err(ExpiryRefusal::Replay);
        }
        if self.scheduled.len() == MAX_EXPIRY_QUEUE_ENTRIES {
            return Err(ExpiryRefusal::QueueFull);
        }
        let at = self.scheduled.binary_search(&key).unwrap_or_else(|at| at);
        self.scheduled.insert(at, key);
        Ok(())
    }

    /// # Errors
    ///
    /// Returns a refusal when the sweep limit is invalid or the due count overflows.
    pub fn due(&self, boundary: u64, limit: u32) -> Result<SweepPage, ExpiryRefusal> {
        if limit == 0 || limit > MAX_SWEEP_LEASES_PER_BATCH {
            return Err(ExpiryRefusal::InvalidLimit);
        }
        let mut leases = Vec::with_capacity(limit as usize);
        for (expiry, lease) in &self.scheduled {
            if *expiry > boundary {
                break;
            }
            if leases.len() == limit as usize {
                break;
            }
            leases.push(*lease);
        }
        let remaining_due = self
            .scheduled
            .iter()
            .filter(|(expiry, _)| *expiry <= boundary)
            .count()
            .checked_sub(leases.len())
            .ok_or(ExpiryRefusal::AccountingOverflow)?;
        Ok(SweepPage {
            boundary,
            leases,
            remaining_due: u64::try_from(remaining_due)
                .map_err(|_| ExpiryRefusal::AccountingOverflow)?,
        })
    }

    /// # Errors
    ///
    /// Returns a refusal when terminal verification or the scheduled lease binding fails.
    pub fn record_destroyed(&mut self, record: TerminalLeaseRecord) -> Result<(), ExpiryRefusal> {
        record.verify()?;
        let key = (record.expiry, record.lease);
        let at = self
            .scheduled
            .binary_search(&key)
            .map_err(|_| ExpiryRefusal::Replay)?;
        if self.terminal.len() == MAX_EXPIRY_QUEUE_ENTRIES
            || self.terminal.iter().any(|v| v.lease == record.lease)
        {
            return Err(ExpiryRefusal::Replay);
        }
        self.terminal.push(record);
        self.scheduled.remove(at);
        Ok(())
    }

    #[must_use]
    pub fn terminal(&self, lease: LeaseId) -> Option<&TerminalLeaseRecord> {
        self.terminal.iter().find(|record| record.lease == lease)
    }

    /// # Errors
    ///
    /// Returns an accounting overflow when a queue or record length cannot be encoded.
    pub fn canonical_state(&self) -> Result<Vec<u8>, ExpiryRefusal> {
        let mut bytes = Vec::new();
        bytes.extend_from_slice(SWEEP_STATE_DOMAIN);
        bytes.extend_from_slice(
            &u32::try_from(self.scheduled.len())
                .map_err(|_| ExpiryRefusal::AccountingOverflow)?
                .to_be_bytes(),
        );
        for (expiry, lease) in &self.scheduled {
            bytes.extend_from_slice(&expiry.to_be_bytes());
            bytes.extend_from_slice(&lease.bytes());
        }
        bytes.extend_from_slice(
            &u32::try_from(self.terminal.len())
                .map_err(|_| ExpiryRefusal::AccountingOverflow)?
                .to_be_bytes(),
        );
        for record in &self.terminal {
            let record_bytes = record.canonical_bytes();
            bytes.extend_from_slice(&record.lease.bytes());
            bytes.extend_from_slice(
                &u32::try_from(record_bytes.len())
                    .map_err(|_| ExpiryRefusal::AccountingOverflow)?
                    .to_be_bytes(),
            );
            bytes.extend_from_slice(&record_bytes);
            bytes.extend_from_slice(&record.terminal_digest);
        }
        Ok(bytes)
    }

    /// # Errors
    ///
    /// Returns a refusal when canonical encoding, hashing or chunk-count conversion fails.
    pub fn canonical_chunks(&self) -> Result<Vec<Vec<u8>>, ExpiryRefusal> {
        const CHUNK: usize = 1000;
        let state = self.canonical_state()?;
        let root =
            hash_bytes(HashAlgorithm::Sha256, &state).map_err(|_| ExpiryRefusal::HashRefusal)?;
        let count = state.len().div_ceil(CHUNK);
        if count > u16::MAX as usize {
            return Err(ExpiryRefusal::AccountingOverflow);
        }
        state
            .chunks(CHUNK)
            .enumerate()
            .map(|(index, body)| {
                let mut chunk = Vec::with_capacity(40 + body.len());
                chunk.extend_from_slice(b"LXSQ1");
                chunk.extend_from_slice(&root);
                chunk.extend_from_slice(
                    &u16::try_from(index)
                        .map_err(|_| ExpiryRefusal::AccountingOverflow)?
                        .to_be_bytes(),
                );
                chunk.extend_from_slice(
                    &u16::try_from(count)
                        .map_err(|_| ExpiryRefusal::AccountingOverflow)?
                        .to_be_bytes(),
                );
                chunk.extend_from_slice(body);
                Ok(chunk)
            })
            .collect()
    }
}

impl Default for ExpiryQueue {
    fn default() -> Self {
        Self::new()
    }
}

/// # Errors
///
/// Returns a refusal when due-lease selection, destruction or terminal recording fails.
pub fn sweep<F>(
    queue: &mut ExpiryQueue,
    boundary: u64,
    limit: u32,
    mut destroy_due: F,
) -> Result<SweepPage, ExpiryRefusal>
where
    F: FnMut(LeaseId, u64) -> Result<TerminalLeaseRecord, ExpiryRefusal>,
{
    let page = queue.due(boundary, limit)?;
    for lease in page.leases.iter().copied() {
        let record = destroy_due(lease, boundary)?;
        if record.lease() != lease || record.destroyed_at() != boundary {
            return Err(ExpiryRefusal::InvalidTerminalRecord);
        }
        queue.record_destroyed(record)?;
    }
    queue.due(boundary, limit)
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct SweepPage {
    boundary: u64,
    leases: Vec<LeaseId>,
    remaining_due: u64,
}
impl SweepPage {
    #[must_use]
    pub const fn boundary(&self) -> u64 {
        self.boundary
    }
    #[must_use]
    pub fn leases(&self) -> &[LeaseId] {
        &self.leases
    }
    #[must_use]
    pub const fn remaining_due(&self) -> u64 {
        self.remaining_due
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub enum ExpiryRefusal {
    NotDue,
    InvalidLimit,
    Replay,
    InvalidEvidence,
    InvalidTerminalRecord,
    InvalidProtocolReceipt,
    AccountingOverflow,
    RefundMismatch,
    HashRefusal,
    QueueFull,
    Lease(LeaseRefusal),
    Escrow(EscrowRefusal),
    Usage(UsageRefusal),
}
impl Display for ExpiryRefusal {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(formatter, "{self:?}")
    }
}
impl std::error::Error for ExpiryRefusal {}

#[cfg(test)]
mod source_cases {
    use super::*;
    use crate::LeaseLimits;
    use layerx_programs_runtime::{PrincipalId, ProgramId};

    fn lease(id: u8, expiry: u64) -> Lease {
        Lease::request(
            LeaseId::new([id; 32]).unwrap_or_else(|error| panic!("lease: {error:?}")),
            PrincipalId::new([id.wrapping_add(64); 32])
                .unwrap_or_else(|error| panic!("tenant: {error:?}")),
            ProgramId::new([3; 32]).unwrap_or_else(|error| panic!("host: {error:?}")),
            [4; 32],
            [5; 32],
            100,
            LeaseLimits {
                cpu_fuel: 10,
                memory_bytes: 10,
                storage_read_bytes: 10,
                storage_write_bytes: 10,
                output_values: 10,
                output_bytes: 10,
                table_elements: 10,
                namespace_bytes: 10,
            },
            1,
            expiry,
        )
        .unwrap_or_else(|error| panic!("lease: {error:?}"))
    }

    fn terminal(lease: &Lease, boundary: u64) -> TerminalLeaseRecord {
        let mut record = TerminalLeaseRecord {
            lease: lease.id(),
            tenant: lease.tenant().bytes(),
            host_program: lease.host_program().bytes(),
            expiry: lease.expiry(),
            destroyed_at: boundary,
            reclaimed_cells: 0,
            reclaimed_bytes: 0,
            metered_cleanup_work: 0,
            occupancy_charged: 0,
            refunded: 100,
            refund_transfer_root: [7; 32],
            expiry_receipt_digest: [8; 32],
            destroy_receipt_digest: [9; 32],
            prior_lease_digest: lease
                .state_digest()
                .unwrap_or_else(|error| panic!("digest: {error:?}")),
            terminal_digest: [0; 32],
        };
        record.terminal_digest = hash_bytes(HashAlgorithm::Sha256, &record.canonical_bytes())
            .unwrap_or_else(|error| panic!("terminal digest: {error:?}"));
        record
    }

    #[test]
    fn cohort_is_ordered_bounded_and_carried_across_batches() {
        let mut queue = ExpiryQueue::new();
        for id in 1..=14 {
            queue
                .schedule(&lease(id, 20))
                .unwrap_or_else(|error| panic!("schedule: {error:?}"));
        }
        let first = queue
            .due(19, MAX_SWEEP_LEASES_PER_BATCH)
            .unwrap_or_else(|error| panic!("page: {error:?}"));
        assert!(first.leases().is_empty());
        let first = queue
            .due(20, MAX_SWEEP_LEASES_PER_BATCH)
            .unwrap_or_else(|error| panic!("page: {error:?}"));
        assert_eq!(first.leases().len(), 6);
        assert_eq!(first.remaining_due(), 8);
        assert_eq!(
            first.leases()[0],
            LeaseId::new([1; 32]).unwrap_or_else(|error| panic!("id: {error:?}"))
        );
        assert_eq!(
            first.leases()[5],
            LeaseId::new([6; 32]).unwrap_or_else(|error| panic!("id: {error:?}"))
        );
        for id in 1..=6 {
            let value = lease(id, 20);
            queue
                .record_destroyed(terminal(&value, 20))
                .unwrap_or_else(|error| panic!("first batch: {error:?}"));
        }
        let second = queue
            .due(21, MAX_SWEEP_LEASES_PER_BATCH)
            .unwrap_or_else(|error| panic!("second page: {error:?}"));
        assert_eq!(second.leases().len(), 6);
        assert_eq!(second.remaining_due(), 2);
        for id in 7..=12 {
            let value = lease(id, 20);
            queue
                .record_destroyed(terminal(&value, 21))
                .unwrap_or_else(|error| panic!("second batch: {error:?}"));
        }
        let third = queue
            .due(22, MAX_SWEEP_LEASES_PER_BATCH)
            .unwrap_or_else(|error| panic!("third page: {error:?}"));
        assert_eq!(third.leases().len(), 2);
        assert_eq!(third.remaining_due(), 0);
    }

    #[test]
    fn queue_refuses_duplicate_and_unbounded_sweep_admission() {
        let mut queue = ExpiryQueue::new();
        let lease = lease(1, 20);
        queue
            .schedule(&lease)
            .unwrap_or_else(|error| panic!("schedule: {error:?}"));
        assert_eq!(queue.schedule(&lease), Err(ExpiryRefusal::Replay));
        assert_eq!(queue.due(20, 0), Err(ExpiryRefusal::InvalidLimit));
        assert_eq!(
            queue.due(20, MAX_SWEEP_LEASES_PER_BATCH + 1),
            Err(ExpiryRefusal::InvalidLimit)
        );
    }
}

#[cfg(test)]
mod teardown {
    use super::*;
    use crate::{EphemeralNamespace, Escrow, LeaseActivity, LeaseLimits, LeaseUsage, UsageLedger};
    use layerx_programs_runtime::transfer::SandboxEscrowCharge;
    use layerx_programs_runtime::{
        sandbox_escrow_charge_root, FeeSchedule, FeeScheduleParameters, PrincipalId, ProgramId,
        ResourceBudget,
    };

    const EXPIRY: u64 = 20;
    const OCCUPIED_BATCHES: u128 = 19;

    struct Fixture {
        state: DurableUsageState,
        storage: Storage,
        meter: Meter,
    }

    fn fixture(id: u8, cells: u8, spare: u128) -> Fixture {
        let mut storage = Storage::new();
        let schedule = FeeSchedule::new_complete(FeeScheduleParameters {
            version: 1,
            fee_units_per_cpu_fuel: 1,
            fee_units_per_memory_byte: 1,
            fee_units_per_storage_read_byte: 1,
            fee_units_per_storage_write_byte: 1,
            fee_units_per_output_value: 1,
            fee_units_per_output_byte: 1,
            fee_units_per_occupancy_byte_batch: 1,
        });
        let lease_id = LeaseId::new([id; 32]).unwrap_or_else(|error| panic!("lease: {error:?}"));
        let host = ProgramId::new([3; 32]).unwrap_or_else(|error| panic!("program: {error:?}"));
        let namespace = EphemeralNamespace::derive(host, lease_id)
            .unwrap_or_else(|error| panic!("namespace: {error:?}"));
        let live: Vec<(Vec<u8>, Vec<u8>)> = (0..cells)
            .map(|cell| (vec![b'k', cell], vec![id; 8]))
            .collect();
        storage
            .replace_protocol_namespace(
                namespace
                    .storage_namespace()
                    .unwrap_or_else(|error| panic!("namespace: {error:?}")),
                &live,
            )
            .unwrap_or_else(|error| panic!("seed namespace: {error:?}"));
        if cells != 0 {
            storage
                .replace_protocol_prefix(
                    namespace.snapshot_storage_namespace(),
                    b"snapshot",
                    &[(b"snapshot/0".to_vec(), vec![id; 8])],
                )
                .unwrap_or_else(|error| panic!("seed snapshot: {error:?}"));
        }
        let bytes = storage
            .namespace_persistent_bytes(
                namespace
                    .storage_namespace()
                    .unwrap_or_else(|error| panic!("namespace: {error:?}")),
            )
            .unwrap_or_else(|error| panic!("bytes: {error:?}"));
        let mut lease = Lease::request_with_schedule(
            lease_id,
            PrincipalId::new([id.wrapping_add(64); 32])
                .unwrap_or_else(|error| panic!("tenant: {error:?}")),
            host,
            [4; 32],
            [5; 32],
            OCCUPIED_BATCHES * u128::from(bytes) + spare,
            LeaseLimits {
                cpu_fuel: 10,
                memory_bytes: 10,
                storage_read_bytes: 10,
                storage_write_bytes: 10,
                output_values: 10,
                output_bytes: 10,
                table_elements: 10,
                namespace_bytes: 4096,
            },
            1,
            EXPIRY,
            schedule,
        )
        .unwrap_or_else(|error| panic!("lease: {error:?}"));
        lease
            .apply_host_activity(LeaseActivity::Fund, [6; 32], 1)
            .unwrap_or_else(|error| panic!("fund: {error:?}"));
        let escrow = Escrow::funded_genesis(&lease, [7; 32])
            .unwrap_or_else(|error| panic!("escrow: {error:?}"));
        lease
            .apply_host_activity(LeaseActivity::Activate, [8; 32], 2)
            .unwrap_or_else(|error| panic!("activate: {error:?}"));
        lease
            .record_usage(
                LeaseUsage {
                    namespace_bytes: bytes,
                    ..LeaseUsage::default()
                },
                0,
                2,
                None,
            )
            .unwrap_or_else(|error| panic!("usage: {error:?}"));
        Fixture {
            state: DurableUsageState {
                lease,
                escrow,
                ledger: UsageLedger::new(),
            },
            storage,
            meter: Meter::new(ResourceBudget::declared(), FeeSchedule::declared()),
        }
    }

    fn authority(state: &DurableUsageState, boundary: u64) -> DestroyAuthority {
        let mut seed = state.lease.id().bytes().to_vec();
        seed.extend_from_slice(&boundary.to_be_bytes());
        DestroyAuthority {
            activity_id: hash_bytes(HashAlgorithm::Sha256, &seed)
                .unwrap_or_else(|error| panic!("activity: {error:?}")),
            expected_lease_root: state
                .lease
                .state_digest()
                .unwrap_or_else(|error| panic!("digest: {error:?}")),
            expected_sequence: boundary,
            boundary,
        }
    }

    fn kernel_root(state: &DurableUsageState, plan: TeardownPlan, refund: bool) -> [u8; 32] {
        let amount = if refund {
            plan.refund()
        } else {
            plan.occupancy_charge()
        };
        if amount == 0 {
            return [0; 32];
        }
        sandbox_escrow_charge_root(&SandboxEscrowCharge {
            host_program: state.lease.host_program(),
            execution_principal: state
                .lease
                .namespace()
                .execution_principal()
                .unwrap_or_else(|error| panic!("principal: {error:?}")),
            invocation_authority: plan.authority().activity_id,
            lease_id: state.lease.id().bytes(),
            expected_lease_digest: plan.authority().expected_lease_root,
            escrow_account: state.lease.escrow_account(),
            asset: state.lease.escrow_asset(),
            fee_destination: if refund {
                state.lease.tenant().bytes()
            } else {
                state.lease.fee_destination()
            },
            amount,
        })
        .unwrap_or_else(|error| panic!("root: {error:?}"))
    }

    fn teardown(
        fixture: &mut Fixture,
        boundary: u64,
    ) -> Result<TerminalLeaseRecord, ExpiryRefusal> {
        let plan = plan_teardown(&fixture.state, authority(&fixture.state, boundary))?;
        let occupancy_root = kernel_root(&fixture.state, plan, false);
        let refund_root = kernel_root(&fixture.state, plan, true);
        destroy(
            &mut fixture.storage,
            &mut fixture.meter,
            &mut fixture.state,
            plan,
            occupancy_root,
            refund_root,
        )
    }

    fn live_cells(fixture: &Fixture) -> usize {
        let namespace = fixture.state.lease.namespace();
        let snapshot = fixture
            .storage
            .protocol_namespace_entries(namespace.snapshot_storage_namespace())
            .unwrap_or_else(|error| panic!("snapshot: {error:?}"));
        fixture.storage.namespace_cell_count(
            namespace
                .storage_namespace()
                .unwrap_or_else(|error| panic!("namespace: {error:?}")),
        ) + snapshot.len()
    }

    fn assert_conserved(fixture: &Fixture) {
        let escrow = fixture.state.escrow;
        assert_eq!(escrow.funded(), escrow.spent() + escrow.refunded());
        assert_eq!(escrow.remaining(), Ok(0));
        assert_eq!(fixture.state.ledger.running_total(), escrow.spent());
        assert_eq!(fixture.state.lease.state(), LeaseState::Destroyed);
    }

    #[test]
    fn full_escrow_destruction_drops_the_namespace_and_refunds_every_unit() {
        let mut fixture = fixture(1, 0, 100);
        let funded = fixture.state.escrow.funded();
        let record =
            teardown(&mut fixture, EXPIRY).unwrap_or_else(|error| panic!("destroy: {error:?}"));
        assert_eq!(record.occupancy_charged(), 0);
        assert_eq!(record.refunded(), funded);
        assert_eq!(fixture.state.escrow.refunded(), funded);
        assert_eq!(fixture.state.escrow.spent(), 0);
        assert_eq!(fixture.state.ledger.receipt_count(), 0);
        assert_eq!(live_cells(&fixture), 0);
        assert_eq!(record.verify(), Ok(()));
        assert_conserved(&fixture);
    }

    #[test]
    fn exhausted_escrow_destruction_refunds_nothing_and_refuses_a_refund_root() {
        let mut fixture = fixture(2, 3, 0);
        let plan = plan_teardown(&fixture.state, authority(&fixture.state, EXPIRY))
            .unwrap_or_else(|error| panic!("plan: {error:?}"));
        assert_eq!(plan.refund(), 0);
        assert_eq!(plan.occupancy_charge(), fixture.state.escrow.funded());
        let occupancy_root = kernel_root(&fixture.state, plan, false);
        let before = fixture.state.clone();
        assert_eq!(
            destroy(
                &mut fixture.storage,
                &mut fixture.meter,
                &mut fixture.state,
                plan,
                occupancy_root,
                [0x5a; 32],
            ),
            Err(ExpiryRefusal::RefundMismatch)
        );
        assert_eq!(fixture.state, before);
        let record =
            teardown(&mut fixture, EXPIRY).unwrap_or_else(|error| panic!("destroy: {error:?}"));
        assert_eq!(record.refunded(), 0);
        assert_eq!(record.occupancy_charged(), fixture.state.escrow.funded());
        assert_eq!(fixture.state.escrow.refunded(), 0);
        assert_eq!(record.reclaimed_cells(), 4);
        assert_eq!(live_cells(&fixture), 0);
        assert_conserved(&fixture);
    }

    #[test]
    fn active_at_expiry_destruction_settles_final_occupancy_before_refund() {
        let mut fixture = fixture(3, 2, 40);
        assert_eq!(fixture.state.lease.state(), LeaseState::Active);
        let bytes = u128::from(fixture.state.lease.usage().namespace_bytes);
        let funded = fixture.state.escrow.funded();
        let boundary = EXPIRY + 3;
        let record =
            teardown(&mut fixture, boundary).unwrap_or_else(|error| panic!("destroy: {error:?}"));
        assert_eq!(record.destroyed_at(), boundary);
        assert_eq!(record.occupancy_charged(), OCCUPIED_BATCHES * bytes);
        assert_eq!(record.refunded(), 40);
        assert_eq!(record.occupancy_charged() + record.refunded(), funded);
        assert_eq!(fixture.state.ledger.receipt_count(), 1);
        let receipt = fixture
            .state
            .ledger
            .latest()
            .unwrap_or_else(|| panic!("final usage receipt"));
        assert_eq!(receipt.observed_batch(), EXPIRY);
        assert_eq!(receipt.charged(), record.occupancy_charged());
        assert_eq!(record.reclaimed_cells(), 3);
        assert_eq!(
            fixture
                .meter
                .finish()
                .map(|usage| usage.storage_write_bytes),
            Ok(record.reclaimed_cells() + record.reclaimed_bytes())
        );
        assert_eq!(live_cells(&fixture), 0);
        assert_conserved(&fixture);
    }

    #[test]
    fn cohort_spanning_several_batches_is_destroyed_deterministically_with_carry_forward() {
        let run = || {
            let mut fixtures: Vec<Fixture> = (1..=14)
                .map(|id| fixture(id, id % 3, u128::from(id)))
                .collect();
            let mut queue = ExpiryQueue::new();
            for entry in &fixtures {
                queue
                    .schedule(&entry.state.lease)
                    .unwrap_or_else(|error| panic!("schedule: {error:?}"));
            }
            assert!(queue
                .due(EXPIRY - 1, MAX_SWEEP_LEASES_PER_BATCH)
                .unwrap_or_else(|error| panic!("early: {error:?}"))
                .leases()
                .is_empty());
            let mut pages = Vec::new();
            for boundary in EXPIRY..EXPIRY + 3 {
                let page = sweep(
                    &mut queue,
                    boundary,
                    MAX_SWEEP_LEASES_PER_BATCH,
                    |lease, at| {
                        let entry = fixtures
                            .iter_mut()
                            .find(|entry| entry.state.lease.id() == lease)
                            .ok_or(ExpiryRefusal::InvalidEvidence)?;
                        teardown(entry, at)
                    },
                )
                .unwrap_or_else(|error| panic!("sweep {boundary}: {error:?}"));
                let destroyed = fixtures
                    .iter()
                    .filter(|entry| entry.state.lease.state() == LeaseState::Destroyed)
                    .count();
                let untouched = fixtures
                    .iter()
                    .filter(|entry| entry.state.lease.state() == LeaseState::Active)
                    .count();
                pages.push((
                    page.leases().len(),
                    page.remaining_due(),
                    destroyed,
                    untouched,
                ));
            }
            for entry in &fixtures {
                assert_eq!(live_cells(entry), 0);
                assert_conserved(entry);
            }
            (
                pages,
                queue
                    .canonical_state()
                    .unwrap_or_else(|error| panic!("queue: {error:?}")),
            )
        };
        let (pages, queue_state) = run();
        assert_eq!(pages, vec![(6, 2, 6, 8), (2, 0, 12, 2), (0, 0, 14, 0)]);
        assert_eq!(run(), (pages, queue_state));
    }

    #[test]
    fn destroyed_lease_is_final_and_only_its_terminal_record_is_readable() {
        let mut fixture = fixture(5, 2, 9);
        let mut queue = ExpiryQueue::new();
        queue
            .schedule(&fixture.state.lease)
            .unwrap_or_else(|error| panic!("schedule: {error:?}"));
        let lease = fixture.state.lease.id();
        sweep(&mut queue, EXPIRY, MAX_SWEEP_LEASES_PER_BATCH, |_, at| {
            teardown(&mut fixture, at)
        })
        .unwrap_or_else(|error| panic!("sweep: {error:?}"));
        let record = queue
            .terminal(lease)
            .unwrap_or_else(|| panic!("terminal record"))
            .clone();
        assert_eq!(record.verify(), Ok(()));
        assert_eq!(live_cells(&fixture), 0);
        let settled = fixture.state.clone();
        assert_eq!(
            plan_teardown(&fixture.state, authority(&fixture.state, EXPIRY + 1)),
            Err(ExpiryRefusal::Replay)
        );
        assert_eq!(
            queue.schedule(&fixture.state.lease),
            Err(ExpiryRefusal::Replay)
        );
        assert_eq!(
            queue.record_destroyed(record.clone()),
            Err(ExpiryRefusal::Replay)
        );
        for activity in [LeaseActivity::Fund, LeaseActivity::Activate] {
            assert!(fixture
                .state
                .lease
                .apply_host_activity(activity, [0x35; 32], EXPIRY + 1)
                .is_err());
        }
        assert_eq!(fixture.state, settled);
        assert_eq!(queue.terminal(lease), Some(&record));
    }

    #[test]
    fn refused_teardown_leaves_state_storage_and_meter_untouched() {
        let mut fixture = fixture(6, 2, 7);
        let cells = live_cells(&fixture);
        let before = fixture.state.clone();
        let metered = fixture.meter.finish();
        assert_eq!(
            plan_teardown(&fixture.state, authority(&fixture.state, EXPIRY - 1)),
            Err(ExpiryRefusal::NotDue)
        );
        let mut stale = authority(&fixture.state, EXPIRY);
        stale.expected_lease_root[0] ^= 1;
        assert_eq!(
            plan_teardown(&fixture.state, stale),
            Err(ExpiryRefusal::InvalidEvidence)
        );
        let plan = plan_teardown(&fixture.state, authority(&fixture.state, EXPIRY))
            .unwrap_or_else(|error| panic!("plan: {error:?}"));
        let refund_root = kernel_root(&fixture.state, plan, true);
        assert_eq!(
            destroy(
                &mut fixture.storage,
                &mut fixture.meter,
                &mut fixture.state,
                plan,
                [0; 32],
                refund_root,
            ),
            Err(ExpiryRefusal::RefundMismatch)
        );
        let forged = TeardownPlan {
            occupancy_charge: plan.occupancy_charge() + 1,
            refund: plan.refund() - 1,
            ..plan
        };
        assert_eq!(
            settle_teardown(
                &mut fixture.state,
                forged,
                kernel_root(&before, forged, false),
                kernel_root(&before, forged, true),
            ),
            Err(ExpiryRefusal::InvalidEvidence)
        );
        assert_eq!(fixture.state, before);
        assert_eq!(live_cells(&fixture), cells);
        assert_eq!(fixture.meter.finish(), metered);
    }

    #[test]
    fn host_teardown_and_storage_teardown_settle_identical_protocol_state() {
        let mut host = fixture(7, 2, 11);
        let mut swept = fixture(7, 2, 11);
        let plan = plan_teardown(&host.state, authority(&host.state, EXPIRY))
            .unwrap_or_else(|error| panic!("plan: {error:?}"));
        let settlement = settle_teardown(
            &mut host.state,
            plan,
            kernel_root(&swept.state, plan, false),
            kernel_root(&swept.state, plan, true),
        )
        .unwrap_or_else(|error| panic!("host: {error:?}"));
        let record =
            teardown(&mut swept, EXPIRY).unwrap_or_else(|error| panic!("destroy: {error:?}"));
        assert_eq!(host.state, swept.state);
        assert_eq!(settlement.refund_root(), record.refund_transfer_root);
        assert_ne!(settlement.final_usage_receipt(), [0; 32]);
        assert_eq!(
            settlement.ledger_state(),
            swept
                .state
                .ledger
                .canonical_state()
                .unwrap_or_else(|error| panic!("ledger: {error:?}"))
                .as_slice()
        );
        assert_eq!(record.refunded(), plan.refund());
        assert_eq!(record.occupancy_charged(), plan.occupancy_charge());
        let receipt = swept
            .state
            .ledger
            .latest()
            .unwrap_or_else(|| panic!("final usage receipt"));
        assert_eq!(settlement.final_usage_receipt(), receipt.digest());
        assert_eq!(
            hash_bytes(HashAlgorithm::Sha256, settlement.usage_lease_state()),
            Ok(receipt.resulting_lease_digest())
        );
    }
}
