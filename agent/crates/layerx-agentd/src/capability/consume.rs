//! Serialised capability-ceiling reservations and receipt-only consumption.

use std::collections::{BTreeMap, BTreeSet};
use std::sync::Mutex;

use super::timed::TimedCapability;
use super::{Dimension, SemanticPlan};
use crate::budget::ReconciliationState;
use crate::protocol_evidence::{
    EvidenceAuthority, RawReceiptEvidence, ReceiptReplayError, ReceiptReplayGuard,
    VerifiedReceiptEvidence,
};
use crate::store::{ObjectKind, Store, StoreError, TenantId, TenantKey};

/// One held amount awaiting a verified terminal receipt.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Reservation {
    pub id: [u8; 32],
    pub expected_activity_id: [u8; 32],
    pub amount: u128,
    pub expiry_sequence: u64,
    pub unknown: bool,
}

#[derive(Clone, Debug, Eq, PartialEq)]
struct State {
    consumed: u128,
    reservations: BTreeMap<[u8; 32], Reservation>,
    receipt_replay: ReceiptReplayGuard,
    protocol_reconciliation: Option<ReconciliationState>,
}

/// Thread-safe capability ceiling.
#[derive(Debug)]
pub struct Ceiling {
    maximum: u128,
    verifier: EvidenceAuthority,
    state: Mutex<State>,
}

impl Ceiling {
    /// Creates an empty, explicitly unreconciled cache.
    ///
    /// No reservation is admitted until canonical protocol budget evidence can
    /// issue an opaque reconciliation result.
    #[must_use]
    pub fn new(maximum: u128, verifier: EvidenceAuthority) -> Self {
        let receipt_replay = EvidenceAuthority::receipt_replay_guard();
        Self {
            maximum,
            verifier,
            state: Mutex::new(State {
                consumed: 0,
                reservations: BTreeMap::new(),
                receipt_replay,
                protocol_reconciliation: None,
            }),
        }
    }

    /// Rebuilds a receipt-derived cache without claiming protocol reconciliation.
    ///
    /// # Errors
    ///
    /// Returns `UnverifiedReceipt` for any unverified receipt, typed identity or
    /// replay failures for mismatched and repeated evidence, `Overflow` when the
    /// executed amounts do not sum, and `Exceeded` when the rebuilt total passes
    /// the ceiling. A receipt-only rebuild remains unreconciled and cannot admit
    /// a new reservation without canonical protocol budget state.
    pub fn rebuild(
        maximum: u128,
        verifier: EvidenceAuthority,
        receipts: &[ReceiptApplication],
    ) -> Result<Self, CeilingError> {
        let mut consumed = 0_u128;
        let mut receipt_replay = EvidenceAuthority::receipt_replay_guard();
        let mut settled_reservations = BTreeSet::new();
        for receipt in receipts {
            if receipt.reservation_id == [0; 32] || receipt.expected_activity_id == [0; 32] {
                return Err(CeilingError::InvalidIdentity);
            }
            if !settled_reservations.insert(receipt.reservation_id) {
                return Err(CeilingError::Duplicate);
            }
            let verified_receipt = verifier
                .verify_receipt(&receipt.evidence)
                .map_err(|_| CeilingError::UnverifiedReceipt)?;
            if verified_receipt.activity_id() != receipt.expected_activity_id {
                return Err(CeilingError::ActivityMismatch);
            }
            receipt_replay
                .admit(&verified_receipt)
                .map_err(map_replay_error)?;
            if verified_receipt.result_code() == 0 {
                let amount = verified_receipt.amount();
                consumed = consumed.checked_add(amount).ok_or(CeilingError::Overflow)?;
            }
        }
        if consumed > maximum {
            return Err(CeilingError::Exceeded);
        }
        Ok(Self {
            maximum,
            verifier,
            state: Mutex::new(State {
                consumed,
                reservations: BTreeMap::new(),
                receipt_replay,
                protocol_reconciliation: None,
            }),
        })
    }

    /// Applies only a verified terminal receipt; failure consumes nothing.
    ///
    /// # Errors
    ///
    /// Returns `UnverifiedReceipt` for an unverified receipt, `MissingReservation`
    /// when no held reservation matches, typed identity or replay failures when
    /// evidence does not belong to that reservation, `AmountMismatch` when the
    /// executed amount differs from the held amount, `Overflow` when consumption
    /// does not sum, and `Poisoned` when the state lock is poisoned.
    pub fn apply_receipt(&self, receipt: &ReceiptApplication) -> Result<(), CeilingError> {
        let verified = self
            .verifier
            .verify_receipt(&receipt.evidence)
            .map_err(|_| CeilingError::UnverifiedReceipt)?;
        let mut state = self.state.lock().map_err(|_| CeilingError::Poisoned)?;
        let reservation = state
            .reservations
            .get(&receipt.reservation_id)
            .ok_or(CeilingError::MissingReservation)?;
        if receipt.expected_activity_id != reservation.expected_activity_id
            || verified.activity_id() != reservation.expected_activity_id
        {
            return Err(CeilingError::ActivityMismatch);
        }
        let updated_consumed = if verified.result_code() == 0 {
            let amount = verified.amount();
            if amount != reservation.amount {
                return Err(CeilingError::AmountMismatch);
            }
            state
                .consumed
                .checked_add(amount)
                .ok_or(CeilingError::Overflow)?
        } else {
            state.consumed
        };
        state
            .receipt_replay
            .admit(&verified)
            .map_err(map_replay_error)?;
        state.reservations.remove(&receipt.reservation_id);
        state.consumed = updated_consumed;
        Ok(())
    }

    /// Cancels a reservation only while its activity is known not to have been submitted.
    ///
    /// # Errors
    ///
    /// Returns `MissingReservation` when the identifier is not held, `Indeterminate` when the
    /// reservation has crossed into unknown outcome state, and `Poisoned` for a poisoned lock.
    pub fn cancel_unsubmitted(&self, id: [u8; 32]) -> Result<(), CeilingError> {
        let mut state = self.state.lock().map_err(|_| CeilingError::Poisoned)?;
        if state
            .reservations
            .get(&id)
            .ok_or(CeilingError::MissingReservation)?
            .unknown
        {
            return Err(CeilingError::Indeterminate);
        }
        state.reservations.remove(&id);
        Ok(())
    }

    /// Marks an indeterminate outcome; it remains held across expiry.
    ///
    /// # Errors
    ///
    /// Returns `MissingReservation` when no reservation holds the identifier and
    /// `Poisoned` when the state lock is poisoned.
    pub fn mark_unknown(&self, id: [u8; 32]) -> Result<(), CeilingError> {
        let mut state = self.state.lock().map_err(|_| CeilingError::Poisoned)?;
        let reservation = state
            .reservations
            .get_mut(&id)
            .ok_or(CeilingError::MissingReservation)?;
        reservation.unknown = true;
        Ok(())
    }

    /// Releases only expired reservations whose outcome is not unknown.
    ///
    /// # Errors
    ///
    /// Returns `Poisoned` when the state lock is poisoned.
    pub fn release_expired(&self, current_sequence: u64) -> Result<usize, CeilingError> {
        let mut state = self.state.lock().map_err(|_| CeilingError::Poisoned)?;
        let before = state.reservations.len();
        state
            .reservations
            .retain(|_, value| value.unknown || current_sequence < value.expiry_sequence);
        Ok(before - state.reservations.len())
    }

    /// Records the opaque protocol reconciliation result that admits reservations.
    ///
    /// `ReconciliationState` has no public constructor: a value exists only after
    /// verified protocol budget state and receipt evidence reconciled, so this is
    /// the sole path from an unreconciled ceiling to one that admits reservations.
    ///
    /// # Errors
    ///
    /// Returns `Poisoned` when the state lock is poisoned.
    pub fn reconcile(&self, reconciliation: ReconciliationState) -> Result<(), CeilingError> {
        let mut state = self.state.lock().map_err(|_| CeilingError::Poisoned)?;
        state.protocol_reconciliation = Some(reconciliation);
        Ok(())
    }

    /// Returns the ceiling totals observed under one lock acquisition.
    ///
    /// # Errors
    ///
    /// Returns `Overflow` when the held amounts do not sum and `Poisoned` when the
    /// state lock is poisoned.
    pub fn snapshot(&self) -> Result<CeilingSnapshot, CeilingError> {
        let state = self.state.lock().map_err(|_| CeilingError::Poisoned)?;
        let held = state
            .reservations
            .values()
            .try_fold(0_u128, |total, value| total.checked_add(value.amount))
            .ok_or(CeilingError::Overflow)?;
        Ok(CeilingSnapshot {
            maximum: self.maximum,
            consumed: state.consumed,
            held,
            reservations: state.reservations.len(),
            reconciled: state.protocol_reconciliation.is_some(),
        })
    }
}

/// Raw boundary receipt paired with the reservation it is expected to settle.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ReceiptApplication {
    pub reservation_id: [u8; 32],
    pub expected_activity_id: [u8; 32],
    pub evidence: RawReceiptEvidence,
}

/// Atomic ceiling snapshot.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct CeilingSnapshot {
    pub maximum: u128,
    pub consumed: u128,
    pub held: u128,
    pub reservations: usize,
    pub reconciled: bool,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum CeilingError {
    ZeroAmount,
    InvalidIdentity,
    Expired,
    Duplicate,
    Exceeded,
    Unreconciled,
    UnverifiedReceipt,
    DuplicateReceipt,
    DuplicateActivity,
    MissingReservation,
    Indeterminate,
    AmountMismatch,
    ActivityMismatch,
    Overflow,
    Poisoned,
}

pub(crate) fn reserve(
    ceiling: &Ceiling,
    reservation_id: [u8; 32],
    expected_activity_id: [u8; 32],
    amount: u128,
    expiry_sequence: u64,
    current_sequence: u64,
) -> Result<Reservation, CeilingError> {
    if amount == 0 {
        return Err(CeilingError::ZeroAmount);
    }
    if reservation_id == [0; 32] || expected_activity_id == [0; 32] {
        return Err(CeilingError::InvalidIdentity);
    }
    if expiry_sequence <= current_sequence {
        return Err(CeilingError::Expired);
    }
    let mut state = ceiling.state.lock().map_err(|_| CeilingError::Poisoned)?;
    if state.protocol_reconciliation.is_none() {
        return Err(CeilingError::Unreconciled);
    }
    if state.reservations.contains_key(&reservation_id) {
        return Err(CeilingError::Duplicate);
    }
    if state
        .reservations
        .values()
        .any(|reservation| reservation.expected_activity_id == expected_activity_id)
    {
        return Err(CeilingError::DuplicateActivity);
    }
    let held = state
        .reservations
        .values()
        .try_fold(0_u128, |total, value| total.checked_add(value.amount))
        .ok_or(CeilingError::Overflow)?;
    let projected = state
        .consumed
        .checked_add(held)
        .and_then(|value| value.checked_add(amount))
        .ok_or(CeilingError::Overflow)?;
    if projected > ceiling.maximum {
        return Err(CeilingError::Exceeded);
    }
    let reservation = Reservation {
        id: reservation_id,
        expected_activity_id,
        amount,
        expiry_sequence,
        unknown: false,
    };
    state
        .reservations
        .insert(reservation_id, reservation.clone());
    Ok(reservation)
}

const fn map_replay_error(error: ReceiptReplayError) -> CeilingError {
    match error {
        ReceiptReplayError::DuplicateReceipt => CeilingError::DuplicateReceipt,
        ReceiptReplayError::DuplicateActivity => CeilingError::DuplicateActivity,
    }
}

pub(crate) const RESERVATION_TAG: &[u8; 4] = b"LXCU";
const RESERVATION_VERSION: u8 = 1;
const RESERVATION_PREFIX: &[u8] = b"lxcu-v1:";

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum ReservationState {
    Held,
    Unknown,
    Consumed,
    Released,
}

impl ReservationState {
    const fn byte(self) -> u8 {
        match self {
            Self::Held => 1,
            Self::Unknown => 2,
            Self::Consumed => 3,
            Self::Released => 4,
        }
    }

    const fn from_byte(value: u8) -> Result<Self, ConsumeError> {
        match value {
            1 => Ok(Self::Held),
            2 => Ok(Self::Unknown),
            3 => Ok(Self::Consumed),
            4 => Ok(Self::Released),
            _ => Err(ConsumeError::Corrupt),
        }
    }

    const fn holds(self) -> bool {
        matches!(self, Self::Held | Self::Unknown)
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ChainReservationRecord {
    pub preparation_id: [u8; 32],
    pub ancestors: Vec<[u8; 32]>,
    pub amounts: BTreeMap<[u8; 32], u128>,
    pub rate_action: bool,
    pub observed_head_sequence: u64,
    pub state: ReservationState,
}

impl ChainReservationRecord {
    #[must_use]
    pub fn held(&self, ancestor: &[u8; 32], asset: &[u8; 32]) -> u128 {
        if self.state.holds() {
            self.charged(ancestor, asset)
        } else {
            0
        }
    }

    #[must_use]
    pub fn consumed(&self, ancestor: &[u8; 32], asset: &[u8; 32]) -> u128 {
        if self.state == ReservationState::Consumed {
            self.charged(ancestor, asset)
        } else {
            0
        }
    }

    fn charged(&self, ancestor: &[u8; 32], asset: &[u8; 32]) -> u128 {
        if self.ancestors.contains(ancestor) {
            self.amounts.get(asset).copied().unwrap_or(0)
        } else {
            0
        }
    }

    /// # Errors
    ///
    /// Returns `SizeOverflow` when the ancestor or asset count exceeds its `u16` prefix.
    pub fn encode(&self) -> Result<Vec<u8>, ConsumeError> {
        let mut out = RESERVATION_TAG.to_vec();
        out.push(RESERVATION_VERSION);
        out.extend_from_slice(&self.preparation_id);
        out.push(self.state.byte());
        out.push(u8::from(self.rate_action));
        out.extend_from_slice(&self.observed_head_sequence.to_be_bytes());
        let ancestors =
            u16::try_from(self.ancestors.len()).map_err(|_| ConsumeError::SizeOverflow)?;
        out.extend_from_slice(&ancestors.to_be_bytes());
        for ancestor in &self.ancestors {
            out.extend_from_slice(ancestor);
        }
        let assets = u16::try_from(self.amounts.len()).map_err(|_| ConsumeError::SizeOverflow)?;
        out.extend_from_slice(&assets.to_be_bytes());
        for (asset, amount) in &self.amounts {
            out.extend_from_slice(asset);
            out.extend_from_slice(&amount.to_be_bytes());
        }
        Ok(out)
    }

    /// # Errors
    ///
    /// Returns `Corrupt` for a wrong tag or version, an invalid state or flag byte, an empty, zero or repeated ancestor, unordered assets, truncation or trailing bytes.
    pub fn decode(bytes: &[u8]) -> Result<Self, ConsumeError> {
        let mut reader = Reader { bytes, offset: 0 };
        if reader.take(4)? != RESERVATION_TAG || reader.byte()? != RESERVATION_VERSION {
            return Err(ConsumeError::Corrupt);
        }
        let preparation_id = reader.id()?;
        let state = ReservationState::from_byte(reader.byte()?)?;
        let rate_action = match reader.byte()? {
            0 => false,
            1 => true,
            _ => return Err(ConsumeError::Corrupt),
        };
        let observed_head_sequence = u64::from_be_bytes(reader.array()?);
        let count = u16::from_be_bytes(reader.array()?);
        let mut ancestors = Vec::with_capacity(usize::from(count));
        for _ in 0..count {
            let ancestor = reader.id()?;
            if ancestor == [0; 32] || ancestors.contains(&ancestor) {
                return Err(ConsumeError::Corrupt);
            }
            ancestors.push(ancestor);
        }
        let count = u16::from_be_bytes(reader.array()?);
        let mut amounts = BTreeMap::new();
        for _ in 0..count {
            let asset = reader.id()?;
            if amounts
                .last_key_value()
                .is_some_and(|(last, _)| *last >= asset)
            {
                return Err(ConsumeError::Corrupt);
            }
            amounts.insert(asset, u128::from_be_bytes(reader.array()?));
        }
        if preparation_id == [0; 32] || ancestors.is_empty() || reader.offset != bytes.len() {
            return Err(ConsumeError::Corrupt);
        }
        Ok(Self {
            preparation_id,
            ancestors,
            amounts,
            rate_action,
            observed_head_sequence,
            state,
        })
    }

    fn same_charge(&self, other: &Self) -> bool {
        self.ancestors == other.ancestors
            && self.amounts == other.amounts
            && self.rate_action == other.rate_action
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ChainReservation {
    pub record: ChainReservationRecord,
    pub companion: Option<(TenantKey, Vec<u8>)>,
}

#[derive(Clone, Copy, Debug)]
pub enum SettleOutcome<'a> {
    Verified(&'a VerifiedReceiptEvidence),
    Failed,
    Unknown,
}

#[derive(Debug)]
pub enum ConsumeError {
    InvalidIdentity,
    EmptyChain,
    Refused {
        capability_id: [u8; 32],
        dimension: Dimension,
    },
    Conflict,
    Indeterminate,
    MissingReservation,
    Overflow,
    Corrupt,
    SizeOverflow,
    Store(StoreError),
}

impl From<StoreError> for ConsumeError {
    fn from(value: StoreError) -> Self {
        Self::Store(value)
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
enum Settlement {
    Consume,
    VerifiedRelease,
    Release,
    Unknown,
}

/// # Errors
///
/// Refuses a zero preparation id, an empty, unlinked or foreign chain, a replay with a different charge, and the first ancestor whose consumed plus held plus gross amount exceeds its ceiling for an asset.
pub(crate) fn plan_chain(
    store: &Store,
    tenant: &TenantId,
    preparation_id: [u8; 32],
    chain: &[TimedCapability],
    plan: &SemanticPlan,
    observed_head_sequence: u64,
) -> Result<ChainReservation, ConsumeError> {
    plan_amounts(
        store,
        tenant,
        preparation_id,
        chain,
        plan.gross_per_asset(),
        plan.rate_actions(),
        observed_head_sequence,
    )
}

/// # Errors
///
/// Returns every `plan_chain` refusal and the store error raised while writing the reservation.
pub(crate) fn reserve_chain(
    store: &mut Store,
    tenant: &TenantId,
    preparation_id: [u8; 32],
    chain: &[TimedCapability],
    plan: &SemanticPlan,
    observed_head_sequence: u64,
) -> Result<ChainReservation, ConsumeError> {
    let reservation = plan_chain(
        store,
        tenant,
        preparation_id,
        chain,
        plan,
        observed_head_sequence,
    )?;
    commit(store, &reservation)?;
    Ok(reservation)
}

/// # Errors
///
/// Returns `MissingReservation`, `Indeterminate` for an unverified failure after an unknown outcome, `Conflict` for a contradicting terminal state, `Corrupt` and store errors.
pub(crate) fn settle_chain(
    store: &mut Store,
    tenant: &TenantId,
    preparation_id: [u8; 32],
    outcome: SettleOutcome<'_>,
) -> Result<(), ConsumeError> {
    let settlement = match outcome {
        SettleOutcome::Verified(receipt) if receipt.result_code() == 0 => Settlement::Consume,
        SettleOutcome::Verified(_) => Settlement::VerifiedRelease,
        SettleOutcome::Failed => Settlement::Release,
        SettleOutcome::Unknown => Settlement::Unknown,
    };
    apply(store, tenant, preparation_id, settlement)
}

/// # Errors
///
/// Returns `Corrupt` for any reservation record that does not decode strictly or does not match its key, and store key errors.
pub(crate) fn restore(
    store: &Store,
    tenant: &TenantId,
) -> Result<Vec<ChainReservationRecord>, ConsumeError> {
    let mut out = Vec::new();
    for object in store.list_object_ids(tenant, ObjectKind::Capability) {
        let Some(id) = object.strip_prefix(RESERVATION_PREFIX) else {
            continue;
        };
        let id: [u8; 32] = id.try_into().map_err(|_| ConsumeError::Corrupt)?;
        out.push(load(store, tenant, id)?.ok_or(ConsumeError::Corrupt)?);
    }
    Ok(out)
}

fn reservation_key(tenant: &TenantId, preparation_id: [u8; 32]) -> Result<TenantKey, ConsumeError> {
    let mut object = RESERVATION_PREFIX.to_vec();
    object.extend_from_slice(&preparation_id);
    Ok(TenantKey::new(
        tenant.clone(),
        ObjectKind::Capability,
        object,
    )?)
}

fn load(
    store: &Store,
    tenant: &TenantId,
    preparation_id: [u8; 32],
) -> Result<Option<ChainReservationRecord>, ConsumeError> {
    let Some(value) = store.get(&reservation_key(tenant, preparation_id)?) else {
        return Ok(None);
    };
    let record = ChainReservationRecord::decode(value.bytes())?;
    if record.preparation_id != preparation_id {
        return Err(ConsumeError::Corrupt);
    }
    Ok(Some(record))
}

fn plan_amounts(
    store: &Store,
    tenant: &TenantId,
    preparation_id: [u8; 32],
    chain: &[TimedCapability],
    gross: &BTreeMap<[u8; 32], u128>,
    rate_actions: u32,
    observed_head_sequence: u64,
) -> Result<ChainReservation, ConsumeError> {
    if preparation_id == [0; 32] {
        return Err(ConsumeError::InvalidIdentity);
    }
    let Some(root) = chain.last() else {
        return Err(ConsumeError::EmptyChain);
    };
    if root.parent.is_some()
        || chain.iter().any(|member| member.tenant != *tenant)
        || chain
            .windows(2)
            .any(|pair| pair[0].parent != Some(pair[1].id))
    {
        return Err(ConsumeError::InvalidIdentity);
    }
    let record = ChainReservationRecord {
        preparation_id,
        ancestors: chain.iter().map(|member| member.id).collect(),
        amounts: gross.clone(),
        rate_action: rate_actions > 0,
        observed_head_sequence,
        state: ReservationState::Held,
    };
    if let Some(existing) = load(store, tenant, preparation_id)? {
        if !existing.same_charge(&record) {
            return Err(ConsumeError::Conflict);
        }
        return Ok(ChainReservation {
            record: existing,
            companion: None,
        });
    }
    let records = restore(store, tenant)?;
    for member in chain {
        for (asset, amount) in gross {
            let refuse = |dimension| ConsumeError::Refused {
                capability_id: member.id,
                dimension,
            };
            if !member.assets.contains(asset) {
                return Err(refuse(Dimension::Asset));
            }
            let ceiling = *member
                .amount_ceilings
                .get(asset)
                .ok_or_else(|| refuse(Dimension::Amount))?;
            let used = records.iter().try_fold(0_u128, |total, other| {
                total
                    .checked_add(other.held(&member.id, asset))
                    .and_then(|total| total.checked_add(other.consumed(&member.id, asset)))
            });
            let projected = used
                .and_then(|used| used.checked_add(*amount))
                .ok_or(ConsumeError::Overflow)?;
            if projected > ceiling {
                return Err(refuse(Dimension::Amount));
            }
        }
    }
    let companion = Some((reservation_key(tenant, preparation_id)?, record.encode()?));
    Ok(ChainReservation { record, companion })
}

fn commit(store: &mut Store, reservation: &ChainReservation) -> Result<(), ConsumeError> {
    if let Some((key, bytes)) = reservation.companion.clone() {
        store.put_local(key, bytes)?;
    }
    Ok(())
}

fn apply(
    store: &mut Store,
    tenant: &TenantId,
    preparation_id: [u8; 32],
    settlement: Settlement,
) -> Result<(), ConsumeError> {
    let mut record =
        load(store, tenant, preparation_id)?.ok_or(ConsumeError::MissingReservation)?;
    let next = match (record.state, settlement) {
        (ReservationState::Held | ReservationState::Unknown, Settlement::Consume) => {
            ReservationState::Consumed
        }
        (ReservationState::Held | ReservationState::Unknown, Settlement::VerifiedRelease)
        | (ReservationState::Held, Settlement::Release) => ReservationState::Released,
        (ReservationState::Unknown, Settlement::Release) => {
            return Err(ConsumeError::Indeterminate)
        }
        (ReservationState::Held, Settlement::Unknown) => ReservationState::Unknown,
        (ReservationState::Unknown, Settlement::Unknown)
        | (ReservationState::Consumed, Settlement::Consume)
        | (ReservationState::Released, Settlement::VerifiedRelease | Settlement::Release) => {
            return Ok(())
        }
        (ReservationState::Consumed | ReservationState::Released, _) => {
            return Err(ConsumeError::Conflict)
        }
    };
    record.state = next;
    let key = reservation_key(tenant, preparation_id)?;
    store.update_local_batch(vec![(key, record.encode()?)])?;
    Ok(())
}

struct Reader<'a> {
    bytes: &'a [u8],
    offset: usize,
}

impl<'a> Reader<'a> {
    fn take(&mut self, length: usize) -> Result<&'a [u8], ConsumeError> {
        let end = self
            .offset
            .checked_add(length)
            .ok_or(ConsumeError::Corrupt)?;
        let value = self
            .bytes
            .get(self.offset..end)
            .ok_or(ConsumeError::Corrupt)?;
        self.offset = end;
        Ok(value)
    }

    fn array<const N: usize>(&mut self) -> Result<[u8; N], ConsumeError> {
        self.take(N)?.try_into().map_err(|_| ConsumeError::Corrupt)
    }

    fn id(&mut self) -> Result<[u8; 32], ConsumeError> {
        self.array()
    }

    fn byte(&mut self) -> Result<u8, ConsumeError> {
        Ok(self.array::<1>()?[0])
    }
}

#[cfg(test)]
mod chain_tests {
    use std::collections::{BTreeMap, BTreeSet};

    use super::*;
    use crate::identity::ProtocolAuthority;

    const X: [u8; 32] = [7; 32];
    const Y: [u8; 32] = [6; 32];
    const ROOT: [u8; 32] = [1; 32];
    const LEAF: [u8; 32] = [2; 32];

    fn must<T, E: std::fmt::Debug>(value: Result<T, E>) -> T {
        value.unwrap_or_else(|error| panic!("capability consumption: {error:?}"))
    }

    fn open(name: &str) -> (std::path::PathBuf, Store) {
        let root = std::env::temp_dir().join(format!("lxp-cb2-{name}-{}", std::process::id()));
        let _ = std::fs::remove_dir_all(&root);
        let store = must(Store::open(&root));
        (root, store)
    }

    fn tenant() -> TenantId {
        must(TenantId::new("tenant-a"))
    }

    fn record(id: [u8; 32], parent: Option<[u8; 32]>, ceiling: u128) -> TimedCapability {
        TimedCapability {
            id,
            parent,
            tenant: tenant(),
            agent: "did:layerx:agent".to_owned(),
            authority: ProtocolAuthority::CapabilityGrant([9; 32]),
            activity_types: BTreeSet::from([5]),
            counterparties: BTreeSet::from([[8; 32]]),
            assets: BTreeSet::from([X, Y]),
            amount_ceilings: BTreeMap::from([(X, ceiling), (Y, 50)]),
            rate_ceilings: BTreeMap::from([(60, 10)]),
            purposes: BTreeSet::from(["pay".to_owned()]),
            expiry_seconds: 10_000,
            grant_not_after_ms: 10_000_000,
            created_at_ms: 1,
            created_at_sequence: 1,
            revoked: None,
        }
    }

    fn chain() -> Vec<TimedCapability> {
        vec![record(LEAF, Some(ROOT), 100), record(ROOT, None, 100)]
    }

    fn reserve(
        store: &mut Store,
        preparation: u8,
        gross: &[([u8; 32], u128)],
    ) -> Result<ChainReservation, ConsumeError> {
        let reservation = plan_amounts(
            store,
            &tenant(),
            [preparation; 32],
            &chain(),
            &gross.iter().copied().collect(),
            1,
            3,
        )?;
        commit(store, &reservation)?;
        Ok(reservation)
    }

    fn totals(store: &Store, ancestor: &[u8; 32], asset: &[u8; 32]) -> (u128, u128) {
        must(restore(store, &tenant()))
            .iter()
            .fold((0, 0), |(held, consumed), record| {
                (
                    held + record.held(ancestor, asset),
                    consumed + record.consumed(ancestor, asset),
                )
            })
    }

    #[test]
    fn two_preparations_hold_per_asset_on_every_ancestor() {
        let (root, mut store) = open("sum");
        must(reserve(&mut store, 10, &[(X, 30)]));
        must(reserve(&mut store, 11, &[(X, 40), (Y, 50)]));
        for ancestor in [LEAF, ROOT] {
            assert_eq!(totals(&store, &ancestor, &X), (70, 0));
            assert_eq!(totals(&store, &ancestor, &Y), (50, 0));
        }
        assert!(matches!(
            reserve(&mut store, 12, &[(X, 31)]),
            Err(ConsumeError::Refused {
                capability_id: LEAF,
                dimension: Dimension::Amount
            })
        ));
        assert!(matches!(
            reserve(&mut store, 13, &[(Y, 1)]),
            Err(ConsumeError::Refused {
                capability_id: LEAF,
                dimension: Dimension::Amount
            })
        ));
        must(reserve(&mut store, 14, &[(X, 30)]));
        let reopened = must(Store::open(&root));
        assert_eq!(totals(&reopened, &ROOT, &X), (100, 0));
        let _ = std::fs::remove_dir_all(root);
    }

    #[test]
    fn verified_settlement_moves_held_to_consumed() {
        let (root, mut store) = open("verified");
        must(reserve(&mut store, 20, &[(X, 60)]));
        must(apply(&mut store, &tenant(), [20; 32], Settlement::Consume));
        let reopened = must(Store::open(&root));
        assert_eq!(totals(&reopened, &LEAF, &X), (0, 60));
        assert_eq!(totals(&reopened, &ROOT, &X), (0, 60));
        assert!(matches!(
            reserve(&mut store, 21, &[(X, 41)]),
            Err(ConsumeError::Refused {
                dimension: Dimension::Amount,
                ..
            })
        ));
        assert!(matches!(
            apply(&mut store, &tenant(), [20; 32], Settlement::VerifiedRelease),
            Err(ConsumeError::Conflict)
        ));
        let _ = std::fs::remove_dir_all(root);
    }

    #[test]
    fn failed_settlement_releases_the_hold() {
        let (root, mut store) = open("failed");
        must(reserve(&mut store, 30, &[(X, 100)]));
        must(settle_chain(
            &mut store,
            &tenant(),
            [30; 32],
            SettleOutcome::Failed,
        ));
        assert_eq!(totals(&store, &ROOT, &X), (0, 0));
        must(reserve(&mut store, 31, &[(X, 100)]));
        must(apply(
            &mut store,
            &tenant(),
            [31; 32],
            Settlement::VerifiedRelease,
        ));
        assert_eq!(totals(&store, &LEAF, &X), (0, 0));
        let _ = std::fs::remove_dir_all(root);
    }

    #[test]
    fn unknown_outcome_retains_the_hold() {
        let (root, mut store) = open("unknown");
        must(reserve(&mut store, 40, &[(X, 70)]));
        must(settle_chain(
            &mut store,
            &tenant(),
            [40; 32],
            SettleOutcome::Unknown,
        ));
        assert_eq!(totals(&store, &ROOT, &X), (70, 0));
        assert!(matches!(
            settle_chain(&mut store, &tenant(), [40; 32], SettleOutcome::Failed),
            Err(ConsumeError::Indeterminate)
        ));
        assert!(matches!(
            reserve(&mut store, 41, &[(X, 31)]),
            Err(ConsumeError::Refused { .. })
        ));
        let reopened = must(Store::open(&root));
        let records = must(restore(&reopened, &tenant()));
        assert_eq!(records.len(), 1);
        assert_eq!(records[0].state, ReservationState::Unknown);
        let _ = std::fs::remove_dir_all(root);
    }

    #[test]
    fn replay_of_a_preparation_charges_nothing_more() {
        let (root, mut store) = open("replay");
        let first = must(reserve(&mut store, 50, &[(X, 40)]));
        assert!(first.companion.is_some());
        let replay = must(reserve(&mut store, 50, &[(X, 40)]));
        assert!(replay.companion.is_none());
        assert_eq!(replay.record, first.record);
        assert_eq!(totals(&store, &ROOT, &X), (40, 0));
        assert!(matches!(
            reserve(&mut store, 50, &[(X, 41)]),
            Err(ConsumeError::Conflict)
        ));
        let _ = std::fs::remove_dir_all(root);
    }

    #[test]
    fn rate_counts_one_action_per_preparation_per_ancestor() {
        let (root, mut store) = open("rate");
        must(reserve(&mut store, 60, &[(X, 10), (Y, 10)]));
        must(reserve(&mut store, 61, &[(X, 0)]));
        must(reserve(&mut store, 61, &[(X, 0)]));
        let records = must(restore(&store, &tenant()));
        for ancestor in [LEAF, ROOT] {
            let actions = records
                .iter()
                .filter(|record| record.rate_action && record.ancestors.contains(&ancestor))
                .count();
            assert_eq!(actions, 2);
        }
        let _ = std::fs::remove_dir_all(root);
    }

    #[test]
    fn decoding_is_strict() {
        let record = ChainReservationRecord {
            preparation_id: [70; 32],
            ancestors: vec![LEAF, ROOT],
            amounts: BTreeMap::from([(Y, 1), (X, 2)]),
            rate_action: true,
            observed_head_sequence: 9,
            state: ReservationState::Held,
        };
        let bytes = must(record.encode());
        assert_eq!(must(ChainReservationRecord::decode(&bytes)), record);
        let mut trailing = bytes.clone();
        trailing.push(0);
        assert!(ChainReservationRecord::decode(&trailing).is_err());
        let mut version = bytes;
        version[4] = 2;
        assert!(ChainReservationRecord::decode(&version).is_err());
    }
}
