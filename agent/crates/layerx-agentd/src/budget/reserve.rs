//! Atomic multi-scope budget reservations.

use std::collections::BTreeMap;
use std::sync::Mutex;

use sha2::{Digest as _, Sha256};

const RESERVATION_DIGEST_DOMAIN: &[u8] = b"layerx:budget-reservation:v1\0";

#[derive(Clone, Copy, Debug, Eq, Ord, PartialEq, PartialOrd)]
pub struct LimitId(pub [u8; 16]);

/// Core batch time in milliseconds, the clock core budget expiry is compared against.
#[derive(Clone, Copy, Debug, Eq, Ord, PartialEq, PartialOrd)]
pub struct CoreTimestampMs(pub u64);

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum LimitScope {
    Tenant([u8; 32]),
    Agent([u8; 32]),
    Session([u8; 32]),
    Capability([u8; 32]),
    Counterparty([u8; 32]),
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct LimitConfig {
    pub id: LimitId,
    pub name: String,
    pub scope: LimitScope,
    pub ceiling: u128,
    pub consumed: u128,
}

#[derive(Clone, Debug, Eq, PartialEq)]
struct LimitState {
    config: LimitConfig,
    held: BTreeMap<[u8; 32], Hold>,
    retired: bool,
    successor: Option<LimitId>,
    denomination: Option<ProgramLimitDenomination>,
    scalar_allowed: bool,
}

/// One held amount with its head-sequence bound and, for time-bounded
/// preparations, the core-clock deadline at which it lapses.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
struct Hold {
    allocated_program: bool,
    amount: u128,
    expiry_sequence: u64,
    core_deadline: Option<CoreTimestampMs>,
}

#[derive(Debug)]
pub struct BudgetLimiter {
    limits: Mutex<BTreeMap<LimitId, LimitState>>,
}

impl BudgetLimiter {
    /// Whether an exact durable approval reservation is present in any configured scope.
    ///
    /// # Errors
    ///
    /// Returns a limit refusal if the reservation registry lock is unavailable.
    pub fn has_reservation(&self, reservation_id: [u8; 32]) -> Result<bool, LimitRefusal> {
        let limits = self.limits.lock().map_err(|_| LimitRefusal::Poisoned)?;
        Ok(limits
            .values()
            .any(|limit| limit.held.contains_key(&reservation_id)))
    }

    pub fn remaining_after_reservation(
        &self,
        reservation_id: [u8; 32],
        expected_amount: u128,
        current_sequence: u64,
        core_now: CoreTimestampMs,
    ) -> Result<u128, LimitRefusal> {
        self.remaining_after_reservation_inner(
            reservation_id,
            expected_amount,
            current_sequence,
            core_now,
            None,
        )
    }

    pub fn remaining_after_reservation_bound(
        &self,
        reservation_id: [u8; 32],
        expected_amount: u128,
        current_sequence: u64,
        core_now: CoreTimestampMs,
        verified_protocol_remaining: u128,
    ) -> Result<u128, LimitRefusal> {
        self.remaining_after_reservation_inner(
            reservation_id,
            expected_amount,
            current_sequence,
            core_now,
            Some(verified_protocol_remaining),
        )
    }

    fn remaining_after_reservation_inner(
        &self,
        reservation_id: [u8; 32],
        expected_amount: u128,
        current_sequence: u64,
        core_now: CoreTimestampMs,
        verified_protocol_remaining: Option<u128>,
    ) -> Result<u128, LimitRefusal> {
        if reservation_id == [0; 32] || expected_amount == 0 || core_now.0 == 0 {
            return Err(LimitRefusal::InvalidRequest);
        }
        let limits = self.limits.lock().map_err(|_| LimitRefusal::Poisoned)?;
        let mut remaining: Option<u128> = None;
        let mut greatest_held = 0_u128;
        for (id, limit) in &*limits {
            let Some(hold) = limit.held.get(&reservation_id) else {
                continue;
            };
            if hold.amount != expected_amount
                || hold.expiry_sequence <= current_sequence
                || hold.allocated_program
                || hold.core_deadline.is_some_and(|deadline| deadline <= core_now)
                || !limit.scalar_allowed
            {
                return Err(LimitRefusal::InvalidRequest);
            }
            let head_id = live_head(&limits, *id)?;
            let head = limits.get(&head_id).ok_or(LimitRefusal::UnknownLimit(head_id))?;
            if head.retired {
                return Err(LimitRefusal::Retired(head_id));
            }
            if !head.scalar_allowed || head.denomination != limit.denomination {
                return Err(LimitRefusal::InvalidConfiguration);
            }
            let held = lineage_held(&limits, head_id)?;
            greatest_held = greatest_held.max(held);
            let exposure = head.config.consumed.checked_add(held).ok_or(LimitRefusal::Arithmetic)?;
            let available = head.config.ceiling.checked_sub(exposure).ok_or_else(|| LimitRefusal::Exceeded {
                limit: head_id,
                name: head.config.name.clone(),
                ceiling: head.config.ceiling,
                consumed: head.config.consumed,
                held,
                requested: 0,
            })?;
            remaining = Some(remaining.map_or(available, |current| current.min(available)));
        }
        let remaining = remaining.ok_or(LimitRefusal::InvalidRequest)?;
        match verified_protocol_remaining {
            Some(protocol_remaining) => Ok(remaining.min(
                protocol_remaining.checked_sub(greatest_held).ok_or(LimitRefusal::Arithmetic)?,
            )),
            None => Ok(remaining),
        }
    }

    /// Builds a limiter from a complete set of limit configurations.
    ///
    /// # Errors
    ///
    /// Refuses a zero ceiling, an already-consumed amount above its ceiling, and a repeated
    /// limit identifier.
    pub fn new(configs: Vec<LimitConfig>) -> Result<Self, LimitRefusal> {
        let mut limits = BTreeMap::new();
        for config in configs {
            if config.ceiling == 0 || config.consumed > config.ceiling {
                return Err(LimitRefusal::InvalidConfiguration);
            }
            if limits
                .insert(
                    config.id,
                    LimitState {
                        config,
                        held: BTreeMap::new(),
                        retired: false,
                        successor: None,
                        denomination: None,
                scalar_allowed: true,
                    },
                )
                .is_some()
            {
                return Err(LimitRefusal::InvalidConfiguration);
            }
        }
        Ok(Self {
            limits: Mutex::new(limits),
        })
    }

    /// Counts the reservations currently held across every limit.
    ///
    /// # Errors
    ///
    /// Returns `Poisoned` when the limit state was left poisoned by a panicking holder.
    pub fn held_reservations(&self) -> Result<usize, LimitRefusal> {
        let limits = self.limits.lock().map_err(|_| LimitRefusal::Poisoned)?;
        Ok(limits.values().map(|limit| limit.held.len()).sum())
    }

    /// Installs one durable dynamic limit after it was published.
    ///
    /// # Errors
    ///
    /// Refuses a zero ceiling, consumed above ceiling, or an identifier already configured.
    pub fn install(&self, config: LimitConfig) -> Result<(), LimitRefusal> {
        if config.ceiling == 0 || config.consumed > config.ceiling {
            return Err(LimitRefusal::InvalidConfiguration);
        }
        let mut limits = self.limits.lock().map_err(|_| LimitRefusal::Poisoned)?;
        if limits.contains_key(&config.id) {
            return Err(LimitRefusal::InvalidConfiguration);
        }
        limits.insert(
            config.id,
            LimitState {
                config,
                held: BTreeMap::new(),
                retired: false,
                successor: None,
                denomination: None,
                scalar_allowed: true,
            },
        );
        Ok(())
    }

    /// Records that `predecessor` was renewed into `successor`. Outstanding holds still keyed to
    /// the predecessor then count against the successor's ceiling, and their executed amounts
    /// also charge the successor; the holds themselves are never moved or redigested.
    ///
    /// # Errors
    ///
    /// Returns `UnknownLimit` for an unconfigured identifier, `InvalidConfiguration` for a self
    /// link, a scope change, a predecessor already linked to another successor or a link that
    /// would close a cycle, or `Poisoned`.
    pub fn link_successor(
        &self,
        predecessor: LimitId,
        successor: LimitId,
    ) -> Result<(), LimitRefusal> {
        let mut limits = self.limits.lock().map_err(|_| LimitRefusal::Poisoned)?;
        let scope = limits
            .get(&successor)
            .ok_or(LimitRefusal::UnknownLimit(successor))?
            .config
            .scope;
        let limit = limits
            .get(&predecessor)
            .ok_or(LimitRefusal::UnknownLimit(predecessor))?;
        if predecessor == successor
            || limit.config.scope != scope
            || limit.denomination != limits.get(&successor).and_then(|limit| limit.denomination)
            || limits.get(&successor).is_some_and(|successor| limit.scalar_allowed != successor.scalar_allowed)
            || limit.successor.is_some_and(|current| current != successor)
            || live_head(&limits, successor)? == predecessor
        {
            return Err(LimitRefusal::InvalidConfiguration);
        }
        if let Some(limit) = limits.get_mut(&predecessor) {
            limit.successor = Some(successor);
        }
        Ok(())
    }

    /// Renews `predecessor` into `successor` with admission excluded throughout: under one
    /// limiter lock it carries the larger consumed total, checks it plus every outstanding hold of
    /// the predecessor lineage, counted once, against the successor ceiling, runs `persist` with
    /// the carried total, and publishes the successor, the link and the retirement only after
    /// `persist` succeeds. A refused check or a failed `persist` leaves the limiter unchanged. The
    /// caller holds the store lock, so the order is store then limiter.
    ///
    /// # Errors
    ///
    /// Returns `Exceeded` (with `requested` zero) when the full exposure passes the successor
    /// ceiling, `InvalidConfiguration` for a self renewal, a scope change, a successor already
    /// configured or a predecessor already retired or linked, `Arithmetic`, `Poisoned`, or the
    /// error of `persist`, and `UnknownLimit` when the predecessor is not installed: its holds
    /// are restored into an installed limit before any renewal is checked.
    pub fn renew_locked<E: From<LimitRefusal>>(
        &self,
        predecessor: &LimitConfig,
        successor: &LimitConfig,
        persist: impl FnOnce(u128) -> Result<(), E>,
    ) -> Result<(), E> {
        let mut limits = self.limits.lock().map_err(|_| LimitRefusal::Poisoned)?;
        if predecessor.id == successor.id
            || predecessor.scope != successor.scope
            || limits.contains_key(&successor.id)
        {
            return Err(LimitRefusal::InvalidConfiguration.into());
        }
        let mut renewed = limits.clone();
        let consumed = {
            let previous = renewed
                .get_mut(&predecessor.id)
                .ok_or(LimitRefusal::UnknownLimit(predecessor.id))?;
            if previous.retired
                || previous.successor.is_some()
                || previous.config.scope != successor.scope
            {
                return Err(LimitRefusal::InvalidConfiguration.into());
            }
            previous.config.consumed = previous.config.consumed.max(predecessor.consumed);
            previous.config.consumed.max(successor.consumed)
        };
        let held = lineage_held(&renewed, predecessor.id)?;
        if consumed.checked_add(held).ok_or(LimitRefusal::Arithmetic)? > successor.ceiling {
            return Err(LimitRefusal::Exceeded {
                limit: successor.id,
                name: successor.name.clone(),
                ceiling: successor.ceiling,
                consumed,
                held,
                requested: 0,
            }
            .into());
        }
        if let Some(previous) = renewed.get_mut(&predecessor.id) {
            previous.successor = Some(successor.id);
            previous.retired = true;
        }
        renewed.insert(
            successor.id,
            LimitState {
                config: LimitConfig {
                    consumed,
                    ..successor.clone()
                },
                held: BTreeMap::new(),
                retired: false,
                successor: None,
                denomination: limits.get(&predecessor.id).and_then(|limit| limit.denomination),
                scalar_allowed: limits.get(&predecessor.id).is_some_and(|limit| limit.scalar_allowed),
            },
        );
        persist(consumed)?;
        *limits = renewed;
        Ok(())
    }

    /// Reserves against every applicable scope and publishes the holds only after `persist`
    /// succeeds. The limiter lock is held across `persist`, so admission is excluded until the
    /// holds are published, and a failed `persist` leaves the limiter unchanged.
    ///
    /// # Errors
    ///
    /// Returns every refusal of `reserve`, `InvalidRequest` for a `core_deadline` at or before
    /// `core_now`, or the error of `persist`.
    pub fn reserve_locked<E: From<LimitRefusal>>(
        &self,
        request: &ReservationRequest,
        core_deadline: Option<CoreTimestampMs>,
        core_now: CoreTimestampMs,
        persist: impl FnOnce(&BudgetReservation) -> Result<(), E>,
    ) -> Result<BudgetReservation, E> {
        if core_deadline.is_some_and(|deadline| deadline <= core_now) {
            return Err(LimitRefusal::InvalidRequest.into());
        }
        let mut limits = self.limits.lock().map_err(|_| LimitRefusal::Poisoned)?;
        let mut reserved = limits.clone();
        let reservation = reserve_into(&mut reserved, request, core_deadline, false)?;
        persist(&reservation)?;
        *limits = reserved;
        Ok(reservation)
    }

    /// Raises one limit's cached consumed total to a refreshed persisted total. A lower persisted
    /// total keeps the cached one: a settled execution is persisted before it is released.
    ///
    /// # Errors
    ///
    /// Returns `UnknownLimit` for an unconfigured identifier, `InvalidConfiguration` when the
    /// refreshed total passes the ceiling, or `Poisoned`.
    pub fn refresh_consumed(&self, id: LimitId, consumed: u128) -> Result<(), LimitRefusal> {
        let mut limits = self.limits.lock().map_err(|_| LimitRefusal::Poisoned)?;
        let limit = limits.get_mut(&id).ok_or(LimitRefusal::UnknownLimit(id))?;
        if consumed > limit.config.ceiling {
            return Err(LimitRefusal::InvalidConfiguration);
        }
        if consumed > limit.config.consumed {
            limit.config.consumed = consumed;
        }
        Ok(())
    }

    /// Blocks every new reservation against one limit while keeping its existing holds and
    /// consumed total for the outcomes that still resolve against it.
    ///
    /// # Errors
    ///
    /// Returns `UnknownLimit` for an unconfigured identifier or `Poisoned`.
    pub fn retire(&self, id: LimitId) -> Result<(), LimitRefusal> {
        let mut limits = self.limits.lock().map_err(|_| LimitRefusal::Poisoned)?;
        let limit = limits.get_mut(&id).ok_or(LimitRefusal::UnknownLimit(id))?;
        limit.retired = true;
        Ok(())
    }

    /// Whether a limit no longer admits new reservations.
    ///
    /// # Errors
    ///
    /// Returns `UnknownLimit` for an unconfigured identifier or `Poisoned`.
    pub fn is_retired(&self, id: LimitId) -> Result<bool, LimitRefusal> {
        let limits = self.limits.lock().map_err(|_| LimitRefusal::Poisoned)?;
        limits
            .get(&id)
            .map(|limit| limit.retired)
            .ok_or(LimitRefusal::UnknownLimit(id))
    }

    /// Lists every limit currently holding one reservation.
    ///
    /// # Errors
    ///
    /// Returns `Poisoned` when the limit state was left poisoned by a panicking holder.
    pub fn held_limits(&self, reservation_id: [u8; 32]) -> Result<Vec<LimitId>, LimitRefusal> {
        let limits = self.limits.lock().map_err(|_| LimitRefusal::Poisoned)?;
        Ok(limits
            .iter()
            .filter(|(_, limit)| limit.held.contains_key(&reservation_id))
            .map(|(id, _)| *id)
            .collect())
    }

    /// Every outstanding hold that counts against `id`: its own and those of every predecessor
    /// whose lineage ends at it.
    ///
    /// # Errors
    ///
    /// Returns `UnknownLimit`, `Arithmetic`, or `Poisoned`.
    pub fn held_exposure(&self, id: LimitId) -> Result<u128, LimitRefusal> {
        let limits = self.limits.lock().map_err(|_| LimitRefusal::Poisoned)?;
        if !limits.contains_key(&id) {
            return Err(LimitRefusal::UnknownLimit(id));
        }
        lineage_held(&limits, id)
    }

    /// Computes one settlement's limiter effect without publishing it. `Executed` charges each
    /// released hold to its own limit and to the live head of that limit's lineage.
    ///
    /// # Errors
    ///
    /// Returns `Arithmetic` on overflow, `UnknownLimit` for a broken lineage, or `Poisoned`.
    pub fn stage_release(
        &self,
        reservation_id: [u8; 32],
        kind: ReleaseKind,
        current_sequence: u64,
    ) -> Result<StagedRelease<'_>, LimitRefusal> {
        let limits = self.limits.lock().map_err(|_| LimitRefusal::Poisoned)?;
        if kind == ReleaseKind::Unknown {
            return Ok(StagedRelease {
                limits,
                released: None,
                found: false,
            });
        }
        let mut released = limits.clone();
        let mut successor_charges = Vec::new();
        let mut found = false;
        for (id, limit) in &mut released {
            let Some(hold) = limit.held.get(&reservation_id).copied() else {
                continue;
            };
            if kind == ReleaseKind::Expired && current_sequence < hold.expiry_sequence {
                continue;
            }
            if hold.allocated_program && matches!(kind, ReleaseKind::Executed | ReleaseKind::Failed) {
                return Err(LimitRefusal::InvalidRequest);
            }
            limit.held.remove(&reservation_id);
            if kind == ReleaseKind::Executed {
                limit.config.consumed = limit
                    .config
                    .consumed
                    .checked_add(hold.amount)
                    .ok_or(LimitRefusal::Arithmetic)?;
                if limit.successor.is_some() {
                    successor_charges.push((*id, hold.amount));
                }
            }
            found = true;
        }
        for (id, amount) in successor_charges {
            let head = live_head(&released, id)?;
            let limit = released
                .get_mut(&head)
                .ok_or(LimitRefusal::UnknownLimit(head))?;
            limit.config.consumed = limit
                .config
                .consumed
                .checked_add(amount)
                .ok_or(LimitRefusal::Arithmetic)?;
        }
        Ok(StagedRelease {
            limits,
            released: Some(released),
            found,
        })
    }

    /// Returns the amount already consumed against one limit.
    ///
    /// # Errors
    ///
    /// Returns `UnknownLimit` for an unconfigured identifier, or `Poisoned` when the limit state
    /// was left poisoned by a panicking holder.
    pub fn consumed(&self, id: LimitId) -> Result<u128, LimitRefusal> {
        let limits = self.limits.lock().map_err(|_| LimitRefusal::Poisoned)?;
        limits
            .get(&id)
            .map(|limit| limit.config.consumed)
            .ok_or(LimitRefusal::UnknownLimit(id))
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ReservationRequest {
    pub id: [u8; 32],
    pub amount: u128,
    pub expiry_sequence: u64,
    pub current_sequence: u64,
    pub applicable_limits: Vec<LimitId>,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct BudgetReservation {
    pub id: [u8; 32],
    pub amount: u128,
    pub applied_limits: Vec<LimitId>,
    pub durable: Vec<DurableBudgetReservation>,
}

/// Canonical restart record for one reservation applied to one verified limit.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct DurableBudgetReservation {
    pub reservation_id: [u8; 32],
    pub limit_id: LimitId,
    pub scope: LimitScope,
    pub amount: u128,
    pub ceiling: u128,
    pub expiry_sequence: u64,
    pub digest: [u8; 32],
}

impl DurableBudgetReservation {
    #[must_use]
    pub fn canonical_digest(&self) -> [u8; 32] {
        reservation_digest(
            self.reservation_id,
            self.limit_id,
            self.scope,
            self.amount,
            self.ceiling,
            self.expiry_sequence,
        )
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum ReleaseKind {
    Executed,
    Failed,
    Expired,
    Unknown,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub enum LimitRefusal {
    Exceeded {
        limit: LimitId,
        name: String,
        ceiling: u128,
        consumed: u128,
        held: u128,
        requested: u128,
    },
    UnknownLimit(LimitId),
    Retired(LimitId),
    InvalidConfiguration,
    InvalidRequest,
    Arithmetic,
    Poisoned,
}

pub(crate) fn reserve_all(
    limiter: &BudgetLimiter,
    request: &ReservationRequest,
) -> Result<BudgetReservation, LimitRefusal> {
    reserve_bounded(limiter, request, None)
}

pub(crate) fn reserve_until(
    limiter: &BudgetLimiter,
    request: &ReservationRequest,
    deadline: CoreTimestampMs,
    core_now: CoreTimestampMs,
) -> Result<BudgetReservation, LimitRefusal> {
    if deadline <= core_now {
        return Err(LimitRefusal::InvalidRequest);
    }
    reserve_bounded(limiter, request, Some(deadline))
}

fn reserve_bounded(
    limiter: &BudgetLimiter,
    request: &ReservationRequest,
    core_deadline: Option<CoreTimestampMs>,
) -> Result<BudgetReservation, LimitRefusal> {
    let mut limits = limiter.limits.lock().map_err(|_| LimitRefusal::Poisoned)?;
    reserve_into(&mut limits, request, core_deadline, false)
}

/// Checks and inserts one reservation into `limits`; every refusal is returned before the map
/// is changed.
fn reserve_into(
    limits: &mut BTreeMap<LimitId, LimitState>,
    request: &ReservationRequest,
    core_deadline: Option<CoreTimestampMs>,
    program: bool,
) -> Result<BudgetReservation, LimitRefusal> {
    if request.amount == 0
        || request.expiry_sequence <= request.current_sequence
        || request.applicable_limits.is_empty()
    {
        return Err(LimitRefusal::InvalidRequest);
    }
    let mut applicable = request.applicable_limits.clone();
    applicable.sort_unstable();
    applicable.dedup();
    for id in &applicable {
        let limit = limits.get(id).ok_or(LimitRefusal::UnknownLimit(*id))?;
        if limit.retired {
            return Err(LimitRefusal::Retired(*id));
        }
        if !program && !limit.scalar_allowed { return Err(LimitRefusal::InvalidConfiguration); }
        if limit.held.contains_key(&request.id) {
            return Err(LimitRefusal::InvalidRequest);
        }
        let held = lineage_held(limits, *id)?;
        let projected = limit
            .config
            .consumed
            .checked_add(held)
            .and_then(|value| value.checked_add(request.amount))
            .ok_or(LimitRefusal::Arithmetic)?;
        if projected > limit.config.ceiling {
            return Err(LimitRefusal::Exceeded {
                limit: *id,
                name: limit.config.name.clone(),
                ceiling: limit.config.ceiling,
                consumed: limit.config.consumed,
                held,
                requested: request.amount,
            });
        }
    }
    for id in &applicable {
        if let Some(limit) = limits.get_mut(id) {
            limit.held.insert(
                request.id,
                Hold {
                    allocated_program: false,
                    amount: request.amount,
                    expiry_sequence: request.expiry_sequence,
                    core_deadline,
                },
            );
        }
    }
    let durable = applicable
        .iter()
        .map(|id| {
            let limit = limits.get(id).ok_or(LimitRefusal::UnknownLimit(*id))?;
            let mut record = DurableBudgetReservation {
                reservation_id: request.id,
                limit_id: *id,
                scope: limit.config.scope,
                amount: request.amount,
                ceiling: limit.config.ceiling,
                expiry_sequence: request.expiry_sequence,
                digest: [0; 32],
            };
            record.digest = record.canonical_digest();
            Ok(record)
        })
        .collect::<Result<Vec<_>, LimitRefusal>>()?;
    Ok(BudgetReservation {
        id: request.id,
        amount: request.amount,
        applied_limits: applicable,
        durable,
    })
}

pub(crate) fn restore_all(
    limiter: &BudgetLimiter,
    records: &[DurableBudgetReservation],
) -> Result<(), LimitRefusal> {
    let bounded: Vec<_> = records
        .iter()
        .map(|record| (record.clone(), None))
        .collect();
    restore_bounded(limiter, &bounded)
}

pub(crate) fn restore_bounded(
    limiter: &BudgetLimiter,
    records: &[(DurableBudgetReservation, Option<CoreTimestampMs>)],
) -> Result<(), LimitRefusal> {
    let mut limits = limiter.limits.lock().map_err(|_| LimitRefusal::Poisoned)?;
    let mut restored = limits.clone();
    for (record, core_deadline) in records {
        if record.reservation_id == [0; 32]
            || record.amount == 0
            || record.expiry_sequence == 0
            || record.digest != record.canonical_digest()
        {
            return Err(LimitRefusal::InvalidRequest);
        }
        let limit = restored
            .get_mut(&record.limit_id)
            .ok_or(LimitRefusal::UnknownLimit(record.limit_id))?;
        if !limit.scalar_allowed || limit.config.scope != record.scope
            || limit.config.ceiling != record.ceiling
            || limit.held.contains_key(&record.reservation_id)
        {
            return Err(LimitRefusal::InvalidConfiguration);
        }
        limit.held.insert(
            record.reservation_id,
            Hold {
                allocated_program: false,
                amount: record.amount,
                expiry_sequence: record.expiry_sequence,
                core_deadline: *core_deadline,
            },
        );
    }
    for limit in restored.values() {
        let held = held_total(limit)?;
        if limit
            .config
            .consumed
            .checked_add(held)
            .ok_or(LimitRefusal::Arithmetic)?
            > limit.config.ceiling
        {
            return Err(LimitRefusal::InvalidConfiguration);
        }
    }
    *limits = restored;
    Ok(())
}

pub(crate) fn release_all(
    limiter: &BudgetLimiter,
    reservation_id: [u8; 32],
    kind: ReleaseKind,
    current_sequence: u64,
) -> Result<bool, LimitRefusal> {
    Ok(limiter
        .stage_release(reservation_id, kind, current_sequence)?
        .publish())
}

/// One settlement's limiter effect, computed in full while the limiter lock is held. Admission
/// stays excluded until it is published; dropping it leaves the limiter unchanged.
pub struct StagedRelease<'a> {
    limits: std::sync::MutexGuard<'a, BTreeMap<LimitId, LimitState>>,
    released: Option<BTreeMap<LimitId, LimitState>>,
    found: bool,
}

impl StagedRelease<'_> {
    /// Whether the settlement releases at least one hold.
    #[must_use]
    pub fn found(&self) -> bool {
        self.found
    }

    /// Publishes the staged release into the limiter; this cannot fail.
    #[must_use]
    pub fn publish(mut self) -> bool {
        if let Some(released) = self.released.take() {
            *self.limits = released;
        }
        self.found
    }
}

/// Releases a time-bounded hold whose core deadline has been reached; equality is expired,
/// matching core. Holds without a core deadline are untouched.
pub(crate) fn release_core_expired(
    limiter: &BudgetLimiter,
    reservation_id: [u8; 32],
    core_now: CoreTimestampMs,
) -> Result<bool, LimitRefusal> {
    let mut limits = limiter.limits.lock().map_err(|_| LimitRefusal::Poisoned)?;
    let mut found = false;
    for limit in limits.values_mut() {
        let lapsed = limit
            .held
            .get(&reservation_id)
            .and_then(|hold| hold.core_deadline)
            .is_some_and(|deadline| deadline <= core_now);
        if lapsed {
            limit.held.remove(&reservation_id);
            found = true;
        }
    }
    Ok(found)
}

/// Follows a renewal chain to the identity that currently admits reservations.
fn live_head(limits: &BTreeMap<LimitId, LimitState>, id: LimitId) -> Result<LimitId, LimitRefusal> {
    let mut current = id;
    for _ in 0..=limits.len() {
        let limit = limits
            .get(&current)
            .ok_or(LimitRefusal::UnknownLimit(current))?;
        match limit.successor {
            Some(next) => current = next,
            None => return Ok(current),
        }
    }
    Err(LimitRefusal::InvalidConfiguration)
}

/// Sums the holds of one identity and of every predecessor whose renewal chain ends at it.
fn lineage_held(limits: &BTreeMap<LimitId, LimitState>, id: LimitId) -> Result<u128, LimitRefusal> {
    let mut total = 0_u128;
    for (key, limit) in limits {
        if *key != id && (limit.successor.is_none() || live_head(limits, *key)? != id) {
            continue;
        }
        total = total
            .checked_add(held_total(limit)?)
            .ok_or(LimitRefusal::Arithmetic)?;
    }
    Ok(total)
}

fn held_total(limit: &LimitState) -> Result<u128, LimitRefusal> {
    limit
        .held
        .values()
        .try_fold(0_u128, |total, hold| total.checked_add(hold.amount))
        .ok_or(LimitRefusal::Arithmetic)
}

fn reservation_digest(
    reservation_id: [u8; 32],
    limit_id: LimitId,
    scope: LimitScope,
    amount: u128,
    ceiling: u128,
    expiry_sequence: u64,
) -> [u8; 32] {
    let mut hasher = Sha256::new();
    hasher.update(RESERVATION_DIGEST_DOMAIN);
    hasher.update(reservation_id);
    hasher.update(limit_id.0);
    let (tag, identity) = match scope {
        LimitScope::Tenant(value) => (0_u8, value),
        LimitScope::Agent(value) => (1_u8, value),
        LimitScope::Session(value) => (2_u8, value),
        LimitScope::Capability(value) => (3_u8, value),
        LimitScope::Counterparty(value) => (4_u8, value),
    };
    hasher.update([tag]);
    hasher.update(identity);
    hasher.update(amount.to_be_bytes());
    hasher.update(ceiling.to_be_bytes());
    hasher.update(expiry_sequence.to_be_bytes());
    hasher.finalize().into()
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct ProgramLimitDenomination {
    pub asset: [u8; 32],
    pub source: Option<[u8; 32]>,
}

impl ProgramLimitDenomination {
    pub(crate) fn valid(self) -> bool {
        self.asset != [0; 32] && self.source != Some([0; 32])
    }

    pub(crate) fn matches(self, source: [u8; 32], asset: [u8; 32]) -> bool {
        self.asset == asset && self.source.is_none_or(|expected| expected == source)
    }
}

#[derive(Clone, Copy, Debug, Eq, Ord, PartialEq, PartialOrd)]
pub enum ProgramChargeKind { Principal = 1, ProgramSpend = 2, Fee = 3 }

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ProgramBudgetCharge {
    pub kind: ProgramChargeKind,
    pub source: [u8; 32],
    pub asset: [u8; 32],
    pub amount: u128,
    pub applicable_limits: Vec<LimitId>,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ProgramReservationRequest {
    pub id: [u8; 32],
    pub charges: Vec<ProgramBudgetCharge>,
    pub expiry_sequence: u64,
    pub current_sequence: u64,
    pub core_deadline: Option<CoreTimestampMs>,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ProgramBudgetHold {
    pub denomination: ProgramLimitDenomination,
    pub reservation: DurableBudgetReservation,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ProgramBudgetReservation {
    pub id: [u8; 32],
    pub charges: Vec<ProgramBudgetCharge>,
    pub expiry_sequence: u64,
    pub core_deadline: Option<CoreTimestampMs>,
    pub holds: Vec<ProgramBudgetHold>,
    allocation: Option<ProgramBudgetAllocationRecord>,
}

fn program_totals(charges: &[ProgramBudgetCharge]) -> Result<BTreeMap<LimitId, u128>, LimitRefusal> {
    if charges.is_empty() || charges.len() > 256 {
        return Err(LimitRefusal::InvalidRequest);
    }
    let mut totals = BTreeMap::new();
    let mut assets = BTreeMap::new();
    let mut previous = None;
    for charge in charges {
        let key = (charge.asset, charge.source, charge.kind, charge.applicable_limits.clone());
        if charge.source == [0; 32] || charge.asset == [0; 32] || charge.amount == 0
            || previous.as_ref().is_some_and(|old| old >= &key)
            || charge.applicable_limits.is_empty() || charge.applicable_limits.len() > 1024
            || charge.applicable_limits.windows(2).any(|pair| pair[0] >= pair[1])
        {
            return Err(LimitRefusal::InvalidRequest);
        }
        previous = Some(key);
        for id in &charge.applicable_limits {
            if assets.insert(*id, charge.asset).is_some_and(|old| old != charge.asset) {
                return Err(LimitRefusal::InvalidConfiguration);
            }
            let total = totals.entry(*id).or_insert(0_u128);
            *total = total.checked_add(charge.amount).ok_or(LimitRefusal::Arithmetic)?;
        }
    }
    if totals.len() > 1024 { return Err(LimitRefusal::InvalidRequest); }
    Ok(totals)
}

impl ProgramBudgetReservation {
    pub fn validate(&self) -> Result<(), LimitRefusal> {
        if self.id == [0; 32] || self.expiry_sequence == 0 || self.core_deadline == Some(CoreTimestampMs(0)) {
            return Err(LimitRefusal::InvalidRequest);
        }
        let totals = program_totals(&self.charges)?;
        if let Some(allocation) = &self.allocation {
            allocation.validate(&self.charges)?;
            if allocation.sequence >= self.expiry_sequence { return Err(LimitRefusal::InvalidRequest); }
        }
        if totals.len() != self.holds.len()
            || self.holds.windows(2).any(|pair| pair[0].reservation.limit_id >= pair[1].reservation.limit_id)
        {
            return Err(LimitRefusal::InvalidRequest);
        }
        for hold in &self.holds {
            let record = &hold.reservation;
            if !hold.denomination.valid() || record.reservation_id != self.id
                || record.expiry_sequence != self.expiry_sequence || record.ceiling == 0 || record.amount > record.ceiling
                || record.digest != record.canonical_digest()
                || totals.get(&record.limit_id) != Some(&record.amount)
                || self.charges.iter().filter(|charge| charge.applicable_limits.contains(&record.limit_id))
                    .any(|charge| !hold.denomination.matches(charge.source, charge.asset))
            {
                return Err(LimitRefusal::InvalidRequest);
            }
        }
        Ok(())
    }

    pub fn encode(&self) -> Result<Vec<u8>, LimitRefusal> {
        self.validate()?;
        if self.allocation.is_some() { return self.encode_allocated(); }
        let mut bytes = b"LXPB\x01".to_vec();
        bytes.extend(self.id);
        bytes.extend(self.expiry_sequence.to_be_bytes());
        bytes.push(u8::from(self.core_deadline.is_some()));
        bytes.extend(self.core_deadline.map_or(0, |time| time.0).to_be_bytes());
        bytes.extend(u16::try_from(self.charges.len()).map_err(|_| LimitRefusal::InvalidRequest)?.to_be_bytes());
        for charge in &self.charges {
            bytes.push(charge.kind as u8);
            bytes.extend(charge.source); bytes.extend(charge.asset); bytes.extend(charge.amount.to_be_bytes());
            bytes.extend(u16::try_from(charge.applicable_limits.len()).map_err(|_| LimitRefusal::InvalidRequest)?.to_be_bytes());
            for id in &charge.applicable_limits { bytes.extend(id.0); }
        }
        bytes.extend(u16::try_from(self.holds.len()).map_err(|_| LimitRefusal::InvalidRequest)?.to_be_bytes());
        for hold in &self.holds {
            let record = &hold.reservation;
            bytes.extend(record.limit_id.0);
            bytes.extend(super::scope_bytes(record.scope));
            bytes.extend(record.amount.to_be_bytes()); bytes.extend(record.ceiling.to_be_bytes());
            bytes.extend(record.digest);
            bytes.extend(hold.denomination.asset);
            bytes.push(u8::from(hold.denomination.source.is_some()));
            bytes.extend(hold.denomination.source.unwrap_or([0; 32]));
        }
        let digest: [u8; 32] = Sha256::new().chain_update(b"layerx:program-budget:v1\0").chain_update(&bytes).finalize().into();
        bytes.extend(digest);
        Ok(bytes)
    }

    pub fn decode(bytes: &[u8]) -> Result<Self, LimitRefusal> {
        if bytes.starts_with(b"LXPB\x02") { return Self::decode_allocated(bytes); }
        use layerx_wire::decode::Decoder;
        fn fixed<const N: usize>(decoder: &mut Decoder<'_>) -> Result<[u8; N], LimitRefusal> {
            decoder.fixed(N).map_err(|_| LimitRefusal::InvalidRequest)?.try_into().map_err(|_| LimitRefusal::InvalidRequest)
        }
        if bytes.len() < 32 || bytes.len() > 5_000_000 { return Err(LimitRefusal::InvalidRequest); }
        let (body, digest) = bytes.split_at(bytes.len() - 32);
        let expected: [u8; 32] = Sha256::new().chain_update(b"layerx:program-budget:v1\0").chain_update(body).finalize().into();
        if digest != expected { return Err(LimitRefusal::InvalidRequest); }
        let mut decoder = Decoder::new(body, 0);
        if fixed::<5>(&mut decoder)? != *b"LXPB\x01" { return Err(LimitRefusal::InvalidRequest); }
        let id = fixed::<32>(&mut decoder)?;
        let expiry_sequence = u64::from_be_bytes(fixed::<8>(&mut decoder)?);
        let tag = fixed::<1>(&mut decoder)?[0];
        let deadline = u64::from_be_bytes(fixed::<8>(&mut decoder)?);
        let core_deadline = match (tag, deadline) {
            (0, 0) => None,
            (1, value) if value != 0 => Some(CoreTimestampMs(value)),
            _ => return Err(LimitRefusal::InvalidRequest),
        };
        let count = usize::from(u16::from_be_bytes(fixed::<2>(&mut decoder)?));
        if count == 0 || count > 256 { return Err(LimitRefusal::InvalidRequest); }
        let mut charges = Vec::with_capacity(count);
        for _ in 0..count {
            let kind = match fixed::<1>(&mut decoder)?[0] {
                1 => ProgramChargeKind::Principal,
                2 => ProgramChargeKind::ProgramSpend,
                3 => ProgramChargeKind::Fee,
                _ => return Err(LimitRefusal::InvalidRequest),
            };
            let source = fixed::<32>(&mut decoder)?; let asset = fixed::<32>(&mut decoder)?;
            let amount = u128::from_be_bytes(fixed::<16>(&mut decoder)?);
            let count = usize::from(u16::from_be_bytes(fixed::<2>(&mut decoder)?));
            if count == 0 || count > 1024 { return Err(LimitRefusal::InvalidRequest); }
            let mut applicable_limits = Vec::with_capacity(count);
            for _ in 0..count { applicable_limits.push(LimitId(fixed::<16>(&mut decoder)?)); }
            charges.push(ProgramBudgetCharge { kind, source, asset, amount, applicable_limits });
        }
        let count = usize::from(u16::from_be_bytes(fixed::<2>(&mut decoder)?));
        if count == 0 || count > 1024 { return Err(LimitRefusal::InvalidRequest); }
        let mut holds = Vec::with_capacity(count);
        for _ in 0..count {
            let limit_id = LimitId(fixed::<16>(&mut decoder)?);
            let scope = fixed::<33>(&mut decoder)?;
            let scope = super::scope_from(scope[0], scope[1..].try_into().map_err(|_| LimitRefusal::InvalidRequest)?)
                .map_err(|_| LimitRefusal::InvalidRequest)?;
            let amount = u128::from_be_bytes(fixed::<16>(&mut decoder)?);
            let ceiling = u128::from_be_bytes(fixed::<16>(&mut decoder)?);
            let digest = fixed::<32>(&mut decoder)?;
            let asset = fixed::<32>(&mut decoder)?;
            let tag = fixed::<1>(&mut decoder)?[0]; let source = fixed::<32>(&mut decoder)?;
            let source = match (tag, source) {
                (0, source) if source == [0; 32] => None,
                (1, source) if source != [0; 32] => Some(source),
                _ => return Err(LimitRefusal::InvalidRequest),
            };
            holds.push(ProgramBudgetHold { denomination: ProgramLimitDenomination { asset, source },
                reservation: DurableBudgetReservation { reservation_id: id, limit_id, scope, amount, ceiling, expiry_sequence, digest } });
        }
        decoder.finish().map_err(|_| LimitRefusal::InvalidRequest)?;
        let record = Self { id, charges, expiry_sequence, core_deadline, holds, allocation: None };
        record.validate()?;
        Ok(record)
    }
}

pub struct StagedProgramReservation<'a> {
    limits: std::sync::MutexGuard<'a, BTreeMap<LimitId, LimitState>>,
    reserved: BTreeMap<LimitId, LimitState>,
    record: ProgramBudgetReservation,
}

impl StagedProgramReservation<'_> {
    #[must_use]
    pub fn record(&self) -> &ProgramBudgetReservation { &self.record }

    #[must_use]
    pub fn publish(mut self) -> ProgramBudgetReservation {
        *self.limits = self.reserved;
        self.record
    }
}

impl BudgetLimiter {
    pub(super) fn bind_program_denominations<E: From<LimitRefusal>>(
        &self,
        bindings: &[(LimitId, ProgramLimitDenomination, bool, bool)],
        persist: impl FnOnce() -> Result<(), E>,
    ) -> Result<(), E> {
        let mut limits = self.limits.lock().map_err(|_| LimitRefusal::Poisoned)?;
        let mut bound = limits.clone();
        for (id, denomination, historical, scalar_allowed) in bindings {
            let state = bound.get_mut(id).ok_or(LimitRefusal::UnknownLimit(*id))?;
            if !denomination.valid() || state.denomination.is_some_and(|old| old != *denomination || state.scalar_allowed != *scalar_allowed)
                || (state.denomination.is_none() && !*historical && (state.config.consumed != 0 || !state.held.is_empty()))
            {
                return Err(LimitRefusal::InvalidConfiguration.into());
            }
            state.denomination = Some(*denomination);
            state.scalar_allowed = *scalar_allowed;
        }
        for (id, state) in &bound {
            if state.successor.is_some() {
                let head = bound.get(&live_head(&bound, *id)?).ok_or(LimitRefusal::InvalidConfiguration)?;
                if state.denomination != head.denomination || state.scalar_allowed != head.scalar_allowed {
                    return Err(LimitRefusal::InvalidConfiguration.into());
                }
            }
        }
        persist()?;
        *limits = bound;
        Ok(())
    }

    pub fn stage_program_reservation(
        &self, request: &ProgramReservationRequest, core_now: CoreTimestampMs,
    ) -> Result<StagedProgramReservation<'_>, LimitRefusal> {
        if request.id == [0; 32] || request.expiry_sequence <= request.current_sequence
            || request.core_deadline.is_some_and(|deadline| deadline <= core_now)
        { return Err(LimitRefusal::InvalidRequest); }
        let totals = program_totals(&request.charges)?;
        let limits = self.limits.lock().map_err(|_| LimitRefusal::Poisoned)?;
        if limits.values().any(|limit| limit.held.contains_key(&request.id)) { return Err(LimitRefusal::InvalidRequest); }
        let mut reserved = limits.clone();
        let mut holds = Vec::new();
        for (id, amount) in totals {
            let denomination = reserved.get(&id).ok_or(LimitRefusal::UnknownLimit(id))?
                .denomination.ok_or(LimitRefusal::InvalidConfiguration)?;
            if request.charges.iter().filter(|charge| charge.applicable_limits.contains(&id))
                .any(|charge| !denomination.matches(charge.source, charge.asset))
            { return Err(LimitRefusal::InvalidConfiguration); }
            let held = reserve_into(&mut reserved, &ReservationRequest {
                id: request.id, amount, expiry_sequence: request.expiry_sequence,
                current_sequence: request.current_sequence, applicable_limits: vec![id],
            }, request.core_deadline, true)?;
            for reservation in held.durable { holds.push(ProgramBudgetHold { denomination, reservation }); }
        }
        let record = ProgramBudgetReservation { id: request.id, charges: request.charges.clone(),
            expiry_sequence: request.expiry_sequence, core_deadline: request.core_deadline, holds, allocation: None };
        record.validate()?;
        Ok(StagedProgramReservation { limits, reserved, record })
    }

    pub fn restore_program_reservation(&self, record: &ProgramBudgetReservation) -> Result<(), LimitRefusal> {
        record.validate()?;
        let mut limits = self.limits.lock().map_err(|_| LimitRefusal::Poisoned)?;
        let mut restored = limits.clone();
        if restored.values().any(|limit| limit.held.contains_key(&record.id)) { return Err(LimitRefusal::InvalidRequest); }
        for held in &record.holds {
            let row = &held.reservation;
            let limit = restored.get_mut(&row.limit_id).ok_or(LimitRefusal::UnknownLimit(row.limit_id))?;
            if limit.denomination != Some(held.denomination) || limit.config.scope != row.scope || limit.config.ceiling != row.ceiling {
                return Err(LimitRefusal::InvalidConfiguration);
            }
            limit.held.insert(record.id, Hold { allocated_program: record.allocation.is_some(), amount: row.amount, expiry_sequence: row.expiry_sequence, core_deadline: record.core_deadline });
        }
        for (id, limit) in &restored {
            if limit.config.consumed.checked_add(lineage_held(&restored, *id)?).ok_or(LimitRefusal::Arithmetic)? > limit.config.ceiling {
                return Err(LimitRefusal::InvalidConfiguration);
            }
        }
        *limits = restored;
        Ok(())
    }
}


#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ProgramBudgetAllocation {
    pub kind: ProgramChargeKind,
    pub source: [u8; 32],
    pub asset: [u8; 32],
    pub destination: Option<[u8; 32]>,
    pub maximum_amount: u128,
    pub applicable_limits: Vec<LimitId>,
}

type AllocationKey = ([u8; 32], [u8; 32], ProgramChargeKind, Option<[u8; 32]>);

impl ProgramBudgetAllocation {
    fn key(&self) -> AllocationKey { (self.asset, self.source, self.kind, self.destination) }
}

#[derive(Clone, Debug, Eq, PartialEq)]
struct ProgramBudgetAllocationRecord {
    preparation_digest: [u8; 32],
    actor: [u8; 32],
    state_root: [u8; 32],
    batch_number: u64,
    sequence: u64,
    rows: Vec<ProgramBudgetAllocation>,
}

pub(super) fn allocation_charges(rows: &[ProgramBudgetAllocation]) -> Result<Vec<ProgramBudgetCharge>, LimitRefusal> {
    if rows.is_empty() || rows.len() > 256 { return Err(LimitRefusal::InvalidRequest); }
    let mut previous = None;
    let mut charges: BTreeMap<_, ProgramBudgetCharge> = BTreeMap::new();
    for row in rows {
        let key = row.key();
        if row.source == [0; 32] || row.asset == [0; 32] || row.maximum_amount == 0
            || previous.is_some_and(|old| old >= key)
            || row.applicable_limits.is_empty() || row.applicable_limits.len() > 1024
            || row.applicable_limits.windows(2).any(|pair| pair[0] >= pair[1])
            || match row.kind {
                ProgramChargeKind::Fee => row.destination.is_some(),
                _ => row.destination.is_none_or(|destination| destination == [0; 32]),
            }
        { return Err(LimitRefusal::InvalidRequest); }
        previous = Some(key);
        let key = (row.asset, row.source, row.kind, row.applicable_limits.clone());
        if let Some(charge) = charges.get_mut(&key) {
            charge.amount = charge.amount.checked_add(row.maximum_amount).ok_or(LimitRefusal::Arithmetic)?;
        } else {
            charges.insert(key, ProgramBudgetCharge { kind: row.kind, source: row.source, asset: row.asset,
                amount: row.maximum_amount, applicable_limits: row.applicable_limits.clone() });
        }
    }
    let charges: Vec<_> = charges.into_values().collect();
    program_totals(&charges)?;
    Ok(charges)
}

impl ProgramBudgetAllocationRecord {
    fn validate(&self, charges: &[ProgramBudgetCharge]) -> Result<(), LimitRefusal> {
        if self.preparation_digest == [0; 32] || self.actor == [0; 32] || self.state_root == [0; 32]
            || allocation_charges(&self.rows)?.as_slice() != charges
        { return Err(LimitRefusal::InvalidRequest); }
        Ok(())
    }
}

impl ProgramBudgetReservation {
    #[must_use]
    pub fn allocations(&self) -> Option<&[ProgramBudgetAllocation]> { self.allocation.as_ref().map(|value| value.rows.as_slice()) }
    #[must_use]
    pub fn allocation_preparation_digest(&self) -> Option<[u8; 32]> { self.allocation.as_ref().map(|value| value.preparation_digest) }
    #[must_use]
    pub fn allocation_actor(&self) -> Option<[u8; 32]> { self.allocation.as_ref().map(|value| value.actor) }
    #[must_use]
    pub fn allocation_state_root(&self) -> Option<[u8; 32]> { self.allocation.as_ref().map(|value| value.state_root) }
    #[must_use]
    pub fn allocation_sequence(&self) -> Option<u64> { self.allocation.as_ref().map(|value| value.sequence) }
    #[must_use]
    pub fn allocation_batch_number(&self) -> Option<u64> { self.allocation.as_ref().map(|value| value.batch_number) }

    pub fn settlement_binding(&self) -> Result<[u8; 32], LimitRefusal> {
        if self.allocation.is_none() { return Err(LimitRefusal::InvalidRequest); }
        Ok(Sha256::new().chain_update(b"layerx:program-budget-settlement-binding:v1\0")
            .chain_update(self.encode()?).finalize().into())
    }

    fn encode_allocated(&self) -> Result<Vec<u8>, LimitRefusal> {
        let allocation = self.allocation.as_ref().ok_or(LimitRefusal::InvalidRequest)?;
        let mut legacy = self.clone(); legacy.allocation = None;
        let legacy = legacy.encode()?;
        let mut bytes = b"LXPB\x02".to_vec();
        bytes.extend(u32::try_from(legacy.len()).map_err(|_| LimitRefusal::InvalidRequest)?.to_be_bytes());
        bytes.extend(legacy);
        bytes.extend(allocation.preparation_digest); bytes.extend(allocation.actor); bytes.extend(allocation.state_root);
        bytes.extend(allocation.batch_number.to_be_bytes()); bytes.extend(allocation.sequence.to_be_bytes());
        bytes.extend(u16::try_from(allocation.rows.len()).map_err(|_| LimitRefusal::InvalidRequest)?.to_be_bytes());
        for row in &allocation.rows {
            bytes.push(row.kind as u8); bytes.extend(row.source); bytes.extend(row.asset);
            bytes.push(u8::from(row.destination.is_some())); bytes.extend(row.destination.unwrap_or([0; 32]));
            bytes.extend(row.maximum_amount.to_be_bytes());
            bytes.extend(u16::try_from(row.applicable_limits.len()).map_err(|_| LimitRefusal::InvalidRequest)?.to_be_bytes());
            for id in &row.applicable_limits { bytes.extend(id.0); }
        }
        let digest: [u8; 32] = Sha256::new().chain_update(b"layerx:program-budget:v2\0").chain_update(&bytes).finalize().into();
        bytes.extend(digest);
        if bytes.len() > 5_000_000 { return Err(LimitRefusal::InvalidRequest); }
        Ok(bytes)
    }

    fn decode_allocated(bytes: &[u8]) -> Result<Self, LimitRefusal> {
        use layerx_wire::decode::Decoder;
        fn fixed<const N: usize>(decoder: &mut Decoder<'_>) -> Result<[u8; N], LimitRefusal> {
            decoder.fixed(N).map_err(|_| LimitRefusal::InvalidRequest)?.try_into().map_err(|_| LimitRefusal::InvalidRequest)
        }
        if bytes.len() < 32 || bytes.len() > 5_000_000 { return Err(LimitRefusal::InvalidRequest); }
        let (body, digest) = bytes.split_at(bytes.len() - 32);
        let expected: [u8; 32] = Sha256::new().chain_update(b"layerx:program-budget:v2\0").chain_update(body).finalize().into();
        if digest != expected { return Err(LimitRefusal::InvalidRequest); }
        let mut decoder = Decoder::new(body, 0);
        if fixed::<5>(&mut decoder)? != *b"LXPB\x02" { return Err(LimitRefusal::InvalidRequest); }
        let size = usize::try_from(u32::from_be_bytes(fixed::<4>(&mut decoder)?)).map_err(|_| LimitRefusal::InvalidRequest)?;
        if size > 5_000_000 { return Err(LimitRefusal::InvalidRequest); }
        let legacy = decoder.fixed(size).map_err(|_| LimitRefusal::InvalidRequest)?;
        if !legacy.starts_with(b"LXPB\x01") { return Err(LimitRefusal::InvalidRequest); }
        let mut record = Self::decode(legacy)?;
        let preparation_digest = fixed::<32>(&mut decoder)?;
        let actor = fixed::<32>(&mut decoder)?; let state_root = fixed::<32>(&mut decoder)?;
        let batch_number = u64::from_be_bytes(fixed::<8>(&mut decoder)?);
        let sequence = u64::from_be_bytes(fixed::<8>(&mut decoder)?);
        let count = usize::from(u16::from_be_bytes(fixed::<2>(&mut decoder)?));
        if count == 0 || count > 256 { return Err(LimitRefusal::InvalidRequest); }
        let mut rows = Vec::with_capacity(count);
        for _ in 0..count {
            let kind = match fixed::<1>(&mut decoder)?[0] {
                1 => ProgramChargeKind::Principal, 2 => ProgramChargeKind::ProgramSpend, 3 => ProgramChargeKind::Fee,
                _ => return Err(LimitRefusal::InvalidRequest),
            };
            let source = fixed::<32>(&mut decoder)?; let asset = fixed::<32>(&mut decoder)?;
            let tag = fixed::<1>(&mut decoder)?[0]; let destination = fixed::<32>(&mut decoder)?;
            let destination = match (tag, destination) {
                (0, value) if value == [0; 32] => None,
                (1, value) if value != [0; 32] => Some(value),
                _ => return Err(LimitRefusal::InvalidRequest),
            };
            let maximum_amount = u128::from_be_bytes(fixed::<16>(&mut decoder)?);
            let count = usize::from(u16::from_be_bytes(fixed::<2>(&mut decoder)?));
            if count == 0 || count > 1024 { return Err(LimitRefusal::InvalidRequest); }
            let mut applicable_limits = Vec::with_capacity(count);
            for _ in 0..count { applicable_limits.push(LimitId(fixed::<16>(&mut decoder)?)); }
            rows.push(ProgramBudgetAllocation { kind, source, asset, destination, maximum_amount, applicable_limits });
        }
        decoder.finish().map_err(|_| LimitRefusal::InvalidRequest)?;
        record.allocation = Some(ProgramBudgetAllocationRecord { preparation_digest, actor, state_root, batch_number, sequence, rows });
        record.validate()?;
        if record.encode()? != bytes { return Err(LimitRefusal::InvalidRequest); }
        Ok(record)
    }
}

impl BudgetLimiter {
    pub fn stage_program_allocation(
        &self, request: &ProgramReservationRequest,
        allocations: &super::VerifiedProgramBudgetAllocations, core_now: CoreTimestampMs,
    ) -> Result<StagedProgramReservation<'_>, LimitRefusal> {
        if request.charges.as_slice() != allocations.charges() || request.current_sequence != allocations.global_sequence() {
            return Err(LimitRefusal::InvalidRequest);
        }
        let mut staged = self.stage_program_reservation(request, core_now)?;
        staged.record.allocation = Some(ProgramBudgetAllocationRecord {
            preparation_digest: allocations.preparation_digest(), actor: allocations.actor(),
            state_root: allocations.state_root(), batch_number: allocations.batch_number(),
            sequence: allocations.global_sequence(), rows: allocations.allocations().to_vec(),
        });
        staged.record.validate()?;
        for state in staged.reserved.values_mut() {
            if let Some(hold) = state.held.get_mut(&request.id) { hold.allocated_program = true; }
        }
        Ok(staged)
    }
}

pub(super) type ExpectedProgramLimit = (LimitId, LimitScope, u128, u128, ProgramLimitDenomination);

pub struct StagedProgramSettlement<'a> {
    limits: std::sync::MutexGuard<'a, BTreeMap<LimitId, LimitState>>,
    settled: Option<BTreeMap<LimitId, LimitState>>,
    replayed: bool,
    actual_holds: Vec<DurableBudgetReservation>,
    expected_limits: Vec<ExpectedProgramLimit>,
    marker: Vec<u8>,
    updates: Vec<(crate::store::TenantKey, Vec<u8>)>,
    inserts: Vec<(crate::store::TenantKey, Vec<u8>)>,
}

impl StagedProgramSettlement<'_> {
    #[must_use]
    pub fn replayed(&self) -> bool { self.replayed }
    #[must_use]
    pub fn updates(&self) -> &[(crate::store::TenantKey, Vec<u8>)] { &self.updates }
    #[must_use]
    pub fn inserts(&self) -> &[(crate::store::TenantKey, Vec<u8>)] { &self.inserts }
    #[must_use]
    pub fn publish(mut self) -> bool {
        if let Some(settled) = self.settled.take() { *self.limits = settled; }
        !self.replayed
    }
    pub(super) fn actual_holds(&self) -> &[DurableBudgetReservation] { &self.actual_holds }
    pub(super) fn expected_limits(&self) -> &[ExpectedProgramLimit] { &self.expected_limits }
    pub(super) fn marker(&self) -> &[u8] { &self.marker }
    pub(super) fn set_writes(&mut self, updates: Vec<(crate::store::TenantKey, Vec<u8>)>,
        inserts: Vec<(crate::store::TenantKey, Vec<u8>)>) {
        self.updates = updates; self.inserts = inserts;
    }
}

fn program_actual_totals(
    record: &ProgramBudgetReservation,
    debits: &[super::program_settlement::ProgramExecutedDebit],
) -> Result<(BTreeMap<LimitId, u128>, Vec<u128>), LimitRefusal> {
    record.validate()?;
    let allocations = record.allocations().ok_or(LimitRefusal::InvalidRequest)?;
    if debits.len() > 256 { return Err(LimitRefusal::InvalidRequest); }
    let mut observed: BTreeMap<AllocationKey, u128> = BTreeMap::new();
    for debit in debits {
        let key = (debit.asset, debit.source, debit.kind, debit.destination);
        let allocation = allocations.binary_search_by_key(&key, ProgramBudgetAllocation::key)
            .ok().and_then(|index| allocations.get(index)).ok_or(LimitRefusal::InvalidRequest)?;
        if debit.actual_amount == 0 { return Err(LimitRefusal::InvalidRequest); }
        let value = observed.entry(key).or_insert(0);
        *value = value.checked_add(debit.actual_amount).ok_or(LimitRefusal::Arithmetic)?;
        if *value > allocation.maximum_amount { return Err(LimitRefusal::InvalidRequest); }
    }
    let mut totals: BTreeMap<_, _> = record.holds.iter().map(|hold| (hold.reservation.limit_id, 0_u128)).collect();
    let mut actual = Vec::with_capacity(allocations.len());
    for allocation in allocations {
        let amount = observed.get(&allocation.key()).copied().unwrap_or(0);
        actual.push(amount);
        for id in &allocation.applicable_limits {
            let total = totals.get_mut(id).ok_or(LimitRefusal::InvalidRequest)?;
            *total = total.checked_add(amount).ok_or(LimitRefusal::Arithmetic)?;
        }
    }
    Ok((totals, actual))
}

fn program_settlement_marker(record: &ProgramBudgetReservation, receipt: [u8; 32], actual: &[u128])
    -> Result<Vec<u8>, LimitRefusal>
{
    if receipt == [0; 32] || record.allocations().is_none_or(|rows| rows.len() != actual.len()) {
        return Err(LimitRefusal::InvalidRequest);
    }
    let mut marker = b"LXPS\x01".to_vec();
    marker.extend(record.id); marker.extend(record.settlement_binding()?); marker.extend(receipt);
    marker.extend(u16::try_from(actual.len()).map_err(|_| LimitRefusal::InvalidRequest)?.to_be_bytes());
    for amount in actual { marker.extend(amount.to_be_bytes()); }
    let digest: [u8; 32] = Sha256::new().chain_update(b"layerx:program-budget-settlement:v1\0")
        .chain_update(&marker).finalize().into();
    marker.extend(digest);
    Ok(marker)
}

impl BudgetLimiter {
    pub(super) fn stage_program_settlement_inner(
        &self, record: &ProgramBudgetReservation,
        witness: &super::program_settlement::VerifiedProgramDebitSettlement,
        prior: Option<&[u8]>,
    ) -> Result<StagedProgramSettlement<'_>, LimitRefusal> {
        let binding = record.settlement_binding()?;
        if witness.reservation_id() != record.id || witness.reservation_digest() != binding
            || witness.terminal_receipt() == [0; 32]
        { return Err(LimitRefusal::InvalidRequest); }
        let (totals, actual) = program_actual_totals(record, witness.debits())?;
        let marker = program_settlement_marker(record, witness.terminal_receipt(), &actual)?;
        self.stage_program_actuals(record, &totals, marker, prior)
    }

    fn stage_program_actuals(
        &self, record: &ProgramBudgetReservation, totals: &BTreeMap<LimitId, u128>,
        marker: Vec<u8>, prior: Option<&[u8]>,
    ) -> Result<StagedProgramSettlement<'_>, LimitRefusal> {
        record.validate()?;
        if record.allocation.is_none() || totals.len() != record.holds.len() {
            return Err(LimitRefusal::InvalidRequest);
        }
        let limits = self.limits.lock().map_err(|_| LimitRefusal::Poisoned)?;
        let held_count = limits.values().filter(|limit| limit.held.contains_key(&record.id)).count();
        if let Some(prior) = prior {
            if prior != marker.as_slice() || held_count != 0 { return Err(LimitRefusal::InvalidRequest); }
            return Ok(StagedProgramSettlement { limits, settled: None, replayed: true,
                actual_holds: Vec::new(), expected_limits: Vec::new(), marker, updates: Vec::new(), inserts: Vec::new() });
        }
        if held_count != record.holds.len() { return Err(LimitRefusal::InvalidRequest); }
        let mut settled = limits.clone();
        let mut deltas: BTreeMap<LimitId, u128> = BTreeMap::new();
        let mut actual_holds = Vec::with_capacity(record.holds.len());
        for reserved in &record.holds {
            let original = &reserved.reservation;
            let limit = settled.get_mut(&original.limit_id).ok_or(LimitRefusal::UnknownLimit(original.limit_id))?;
            let held = limit.held.get(&record.id).ok_or(LimitRefusal::InvalidRequest)?;
            let amount = *totals.get(&original.limit_id).ok_or(LimitRefusal::InvalidRequest)?;
            if amount > original.amount || !held.allocated_program || held.amount != original.amount
                || held.expiry_sequence != original.expiry_sequence || held.core_deadline != record.core_deadline
                || limit.denomination != Some(reserved.denomination) || limit.config.scope != original.scope
                || limit.config.ceiling != original.ceiling
            { return Err(LimitRefusal::InvalidRequest); }
            limit.held.remove(&record.id);
            let head = live_head(&settled, original.limit_id)?;
            for id in [Some(original.limit_id), (head != original.limit_id).then_some(head)].into_iter().flatten() {
                let delta = deltas.entry(id).or_insert(0);
                *delta = delta.checked_add(amount).ok_or(LimitRefusal::Arithmetic)?;
            }
            let mut actual = original.clone(); actual.amount = amount; actual.digest = actual.canonical_digest();
            actual_holds.push(actual);
        }
        let mut expected_limits = Vec::with_capacity(deltas.len());
        for (id, amount) in &deltas {
            let limit = settled.get_mut(id).ok_or(LimitRefusal::UnknownLimit(*id))?;
            expected_limits.push((*id, limit.config.scope, limit.config.ceiling, limit.config.consumed,
                limit.denomination.ok_or(LimitRefusal::InvalidConfiguration)?));
            limit.config.consumed = limit.config.consumed.checked_add(*amount)
                .filter(|value| *value <= limit.config.ceiling).ok_or(LimitRefusal::Arithmetic)?;
        }
        for id in deltas.keys() {
            let limit = settled.get(id).ok_or(LimitRefusal::UnknownLimit(*id))?;
            if limit.config.consumed.checked_add(lineage_held(&settled, *id)?).ok_or(LimitRefusal::Arithmetic)? > limit.config.ceiling {
                return Err(LimitRefusal::InvalidConfiguration);
            }
        }
        Ok(StagedProgramSettlement { limits, settled: Some(settled), replayed: false,
            actual_holds, expected_limits, marker, updates: Vec::new(), inserts: Vec::new() })
    }
}

impl BudgetLimiter {
    pub(crate) fn stage_program_unsigned_cancellation(
        &self, record: &ProgramBudgetReservation,
        proof: &crate::approval::native_program::VerifiedUnsignedProgramCancellation,
    ) -> Result<StagedRelease<'_>, LimitRefusal> {
        if proof.reservation_id() != record.id || proof.reservation_digest() != record.settlement_binding()? {
            return Err(LimitRefusal::InvalidRequest);
        }
        self.stage_program_core_expired_allocation(record, proof.core_now())
    }

    fn stage_program_core_expired_allocation(
        &self, record: &ProgramBudgetReservation, core_now: CoreTimestampMs,
    ) -> Result<StagedRelease<'_>, LimitRefusal> {
        if core_now.0 == 0 || record.core_deadline.is_none_or(|deadline| deadline > core_now) {
            return Err(LimitRefusal::InvalidRequest);
        }
        self.stage_program_unsigned_allocation(record)
    }

    pub(crate) fn stage_program_unsigned_rejection(
        &self, record: &ProgramBudgetReservation,
        proof: &crate::approval::native_program::VerifiedUnsignedProgramRejection,
    ) -> Result<StagedRelease<'_>, LimitRefusal> {
        if proof.reservation_id() != record.id || proof.reservation_digest() != record.settlement_binding()? {
            return Err(LimitRefusal::InvalidRequest);
        }
        self.stage_program_unsigned_allocation(record)
    }

    fn stage_program_unsigned_allocation(&self, record: &ProgramBudgetReservation) -> Result<StagedRelease<'_>, LimitRefusal> {
        record.validate()?;
        if record.allocation.is_none() { return Err(LimitRefusal::InvalidRequest); }
        let limits = self.limits.lock().map_err(|_| LimitRefusal::Poisoned)?;
        if limits.values().filter(|limit| limit.held.contains_key(&record.id)).count() != record.holds.len() {
            return Err(LimitRefusal::InvalidRequest);
        }
        let mut released = limits.clone();
        for reserved in &record.holds {
            let original = &reserved.reservation;
            let limit = released.get_mut(&original.limit_id).ok_or(LimitRefusal::UnknownLimit(original.limit_id))?;
            let hold = limit.held.get(&record.id).ok_or(LimitRefusal::InvalidRequest)?;
            if !hold.allocated_program || hold.amount != original.amount
                || hold.expiry_sequence != original.expiry_sequence || hold.core_deadline != record.core_deadline
                || limit.denomination != Some(reserved.denomination) || limit.config.scope != original.scope
                || limit.config.ceiling != original.ceiling
            { return Err(LimitRefusal::InvalidRequest); }
            limit.held.remove(&record.id);
        }
        Ok(StagedRelease { limits, released: Some(released), found: true })
    }
}

#[cfg(test)]
mod renewal_publication_tests {
    use std::cell::Cell;

    use super::{
        BudgetLimiter, LimitConfig, LimitId, LimitRefusal, LimitScope, ReservationRequest,
    };
    use crate::budget::{reserve, DaemonLimitError};
    use crate::store::{ObjectKind, Store, StoreError, TenantId, TenantKey};

    fn must<T, E: std::fmt::Debug>(value: Result<T, E>) -> T {
        value.unwrap_or_else(|error| panic!("renewal publication: {error:?}"))
    }

    fn limit(id: u8, ceiling: u128) -> LimitConfig {
        LimitConfig {
            id: LimitId([id; 16]),
            name: format!("renewal limit {id}"),
            scope: LimitScope::Tenant([1; 32]),
            ceiling,
            consumed: 0,
        }
    }

    fn renew(
        limiter: &BudgetLimiter,
        store: &mut Store,
        key: &TenantKey,
        calls: &Cell<u32>,
    ) -> Result<(), DaemonLimitError> {
        limiter.renew_locked(&limit(1, 100), &limit(2, 30), |consumed| {
            calls.set(calls.get() + 1);
            store.put_local(key.clone(), consumed.to_be_bytes().to_vec())?;
            Ok(())
        })
    }

    #[test]
    fn renewal_with_a_failed_durable_write_publishes_nothing_and_a_written_one_publishes_once() {
        let root =
            std::env::temp_dir().join(format!("lxp-renewal-publication-{}", std::process::id()));
        let _ = std::fs::remove_dir_all(&root);
        let mut store = must(Store::open(&root));
        let key = must(TenantKey::new(
            must(TenantId::new("tenant-a")),
            ObjectKind::Configuration,
            b"renewal-publication".to_vec(),
        ));
        let limiter = must(BudgetLimiter::new(vec![limit(1, 100)]));
        must(reserve(
            &limiter,
            &ReservationRequest {
                id: [5; 32],
                amount: 20,
                expiry_sequence: 40,
                current_sequence: 1,
                applicable_limits: vec![LimitId([1; 16])],
            },
        ));
        let calls = Cell::new(0);

        must(std::fs::remove_dir_all(&root));
        assert!(matches!(
            renew(&limiter, &mut store, &key, &calls),
            Err(DaemonLimitError::Store(StoreError::Io(_)))
        ));
        assert_eq!(calls.get(), 1);
        assert_eq!(limiter.is_retired(LimitId([1; 16])), Ok(false));
        assert_eq!(
            limiter.consumed(LimitId([2; 16])),
            Err(LimitRefusal::UnknownLimit(LimitId([2; 16])))
        );
        assert_eq!(limiter.held_limits([5; 32]), Ok(vec![LimitId([1; 16])]));
        assert_eq!(limiter.held_exposure(LimitId([1; 16])), Ok(20));
        assert_eq!(limiter.consumed(LimitId([1; 16])), Ok(0));
        assert!(store.get(&key).is_none());
        assert!(!root.exists());

        must(std::fs::create_dir_all(&root));
        must(renew(&limiter, &mut store, &key, &calls));
        assert_eq!(calls.get(), 2);
        assert_eq!(limiter.is_retired(LimitId([1; 16])), Ok(true));
        assert_eq!(limiter.consumed(LimitId([2; 16])), Ok(0));
        assert_eq!(limiter.held_exposure(LimitId([2; 16])), Ok(20));
        assert_eq!(limiter.held_reservations(), Ok(1));
        assert!(matches!(
            renew(&limiter, &mut store, &key, &calls),
            Err(DaemonLimitError::Limit(LimitRefusal::InvalidConfiguration))
        ));
        assert_eq!(calls.get(), 2);
        assert_eq!(limiter.held_exposure(LimitId([2; 16])), Ok(20));
        let reopened = must(Store::open(&root));
        assert_eq!(
            reopened.get(&key).map(|value| value.bytes().to_vec()),
            Some(0_u128.to_be_bytes().to_vec())
        );
        let _ = std::fs::remove_dir_all(root);
    }
}

#[cfg(test)]
mod program_budget_tests {
    use super::*;
    use crate::store::{ObjectKind, Store, TenantId, TenantKey};

    fn must<T, E: std::fmt::Debug>(result: Result<T, E>) -> T {
        result.unwrap_or_else(|error| panic!("program budget: {error:?}"))
    }

    fn limiter() -> BudgetLimiter {
        must(BudgetLimiter::new((1..=2).map(|id| LimitConfig {
            id: LimitId([id; 16]), name: format!("asset-{id}"),
            scope: LimitScope::Tenant([9; 32]), ceiling: 100, consumed: 0,
        }).collect()))
    }

    fn bind(limiter: &BudgetLimiter) {
        must(limiter.bind_program_denominations(&[
            (LimitId([1; 16]), ProgramLimitDenomination { asset: [1; 32], source: None }, false, false),
            (LimitId([2; 16]), ProgramLimitDenomination { asset: [2; 32], source: None }, false, false),
        ], || Ok::<(), LimitRefusal>(())));
    }

    fn request() -> ProgramReservationRequest {
        ProgramReservationRequest {
            id: [3; 32], expiry_sequence: 90, current_sequence: 10,
            core_deadline: Some(CoreTimestampMs(900)),
            charges: vec![
                ProgramBudgetCharge { kind: ProgramChargeKind::Principal, source: [7; 32], asset: [1; 32], amount: 20, applicable_limits: vec![LimitId([1; 16])] },
                ProgramBudgetCharge { kind: ProgramChargeKind::Fee, source: [7; 32], asset: [1; 32], amount: 5, applicable_limits: vec![LimitId([1; 16])] },
                ProgramBudgetCharge { kind: ProgramChargeKind::ProgramSpend, source: [8; 32], asset: [2; 32], amount: 80, applicable_limits: vec![LimitId([2; 16])] },
            ],
        }
    }

    #[test]
    fn denominations_refuse_legacy_and_cross_asset_sums_without_publishing() {
        let budgets = limiter();
        assert!(matches!(budgets.stage_program_reservation(&request(), CoreTimestampMs(1)), Err(LimitRefusal::InvalidConfiguration)));
        assert_eq!(budgets.held_reservations(), Ok(0));
        bind(&budgets);
        let mut invalid = request();
        invalid.charges[2].applicable_limits = vec![LimitId([1; 16])];
        assert!(matches!(budgets.stage_program_reservation(&invalid, CoreTimestampMs(1)), Err(LimitRefusal::InvalidConfiguration)));
        assert_eq!(budgets.held_reservations(), Ok(0));
        invalid = request();
        invalid.charges[2].amount = 101;
        assert!(matches!(budgets.stage_program_reservation(&invalid, CoreTimestampMs(1)), Err(LimitRefusal::Exceeded { .. })));
        assert_eq!(budgets.held_reservations(), Ok(0));
        let stage = must(budgets.stage_program_reservation(&request(), CoreTimestampMs(1)));
        assert_eq!(stage.record().holds[0].reservation.amount, 25);
        assert_eq!(stage.record().holds[1].reservation.amount, 80);
        drop(stage);
        assert_eq!(budgets.held_reservations(), Ok(0));
    }

    #[test]
    fn one_asset_source_preserves_distinct_counterparty_limits_and_global_total() {
        let budgets = limiter();
        must(budgets.bind_program_denominations(&[
            (LimitId([1;16]),ProgramLimitDenomination{asset:[1;32],source:None},false,false),
            (LimitId([2;16]),ProgramLimitDenomination{asset:[1;32],source:None},false,false),
        ],||Ok::<(),LimitRefusal>(())));
        let request = ProgramReservationRequest { id:[7;32],expiry_sequence:90,current_sequence:1,core_deadline:None,
            charges:vec![
                ProgramBudgetCharge{kind:ProgramChargeKind::Principal,source:[8;32],asset:[1;32],amount:30,applicable_limits:vec![LimitId([1;16])]},
                ProgramBudgetCharge{kind:ProgramChargeKind::Principal,source:[8;32],asset:[1;32],amount:20,applicable_limits:vec![LimitId([1;16]),LimitId([2;16])]},
            ]};
        let stage=must(budgets.stage_program_reservation(&request,CoreTimestampMs(1)));
        assert_eq!(stage.record().holds[0].reservation.amount,50);
        assert_eq!(stage.record().holds[1].reservation.amount,20);
        assert_eq!(must(ProgramBudgetReservation::decode(&must(stage.record().encode()))),stage.record().clone());
        drop(stage);
        assert_eq!(budgets.held_reservations(),Ok(0));
    }

    #[test]
    fn typed_limits_refuse_scalar_reuse_and_source_deadline_changes() {
        let budgets = limiter();
        let legacy_request = ReservationRequest { id: [4; 32], amount: 1,
            expiry_sequence: 90, current_sequence: 1, applicable_limits: vec![LimitId([1; 16])] };
        let legacy = must(super::reserve_all(&budgets, &legacy_request));
        assert!(budgets.bind_program_denominations(&[
            (LimitId([1; 16]), ProgramLimitDenomination { asset: [1; 32], source: None }, false, false),
        ], || Ok::<(), LimitRefusal>(())).is_err());
        assert!(must(budgets.stage_release(legacy_request.id, ReleaseKind::Failed, 1)).publish());
        bind(&budgets);
        assert!(super::reserve_all(&budgets, &legacy_request).is_err());
        assert!(super::restore_all(&budgets, &legacy.durable).is_err());
        assert!(budgets.bind_program_denominations(&[
            (LimitId([1; 16]), ProgramLimitDenomination { asset: [1; 32], source: None }, true, true),
        ], || Ok::<(), LimitRefusal>(())).is_err());
        let mut expired = request(); expired.core_deadline = Some(CoreTimestampMs(1));
        assert!(matches!(budgets.stage_program_reservation(&expired, CoreTimestampMs(1)), Err(LimitRefusal::InvalidRequest)));
        expired = request(); expired.expiry_sequence = expired.current_sequence;
        assert!(matches!(budgets.stage_program_reservation(&expired, CoreTimestampMs(1)), Err(LimitRefusal::InvalidRequest)));
        let exact = limiter();
        must(exact.bind_program_denominations(&[
            (LimitId([1; 16]), ProgramLimitDenomination { asset: [1; 32], source: Some([9; 32]) }, false, false),
            (LimitId([2; 16]), ProgramLimitDenomination { asset: [2; 32], source: None }, false, false),
        ], || Ok::<(), LimitRefusal>(())));
        assert!(matches!(exact.stage_program_reservation(&request(), CoreTimestampMs(1)), Err(LimitRefusal::InvalidConfiguration)));
        assert_eq!(exact.held_reservations(), Ok(0));
    }

    #[test]
    fn typed_batch_persists_and_recovers_identical_asset_source_fee_and_deadline() {
        let suffix = must(std::time::SystemTime::now().duration_since(std::time::UNIX_EPOCH)).as_nanos();
        let root = std::env::temp_dir().join(format!("program-budget-{}-{suffix}", std::process::id()));
        let mut store = must(Store::open(&root));
        let tenant = must(TenantId::new("program-budget"));
        let key = must(TenantKey::new(tenant.clone(), ObjectKind::Configuration, b"prepared-program".to_vec()));
        let absent = must(TenantKey::new(tenant, ObjectKind::Configuration, b"absent".to_vec()));
        let budgets = limiter(); bind(&budgets);
        let stage = must(budgets.stage_program_reservation(&request(), CoreTimestampMs(1)));
        let bytes = must(stage.record().encode());
        assert!(store.update_local_batch(vec![(absent, bytes.clone())]).is_err());
        drop(stage);
        assert_eq!(budgets.held_reservations(), Ok(0));
        let stage = must(budgets.stage_program_reservation(&request(), CoreTimestampMs(1)));
        must(store.put_local(key.clone(), must(stage.record().encode())));
        let record = stage.publish();
        assert_eq!(budgets.held_exposure(LimitId([1; 16])), Ok(25));
        assert_eq!(budgets.held_exposure(LimitId([2; 16])), Ok(80));
        drop(store); drop(budgets);
        let store = must(Store::open(&root));
        let bytes = store.get(&key).unwrap_or_else(|| panic!("durable Program record missing")).bytes();
        let restored_record = must(ProgramBudgetReservation::decode(bytes));
        assert_eq!(restored_record, record);
        let restarted = limiter(); bind(&restarted);
        must(restarted.restore_program_reservation(&restored_record));
        assert_eq!(restarted.held_exposure(LimitId([1; 16])), Ok(25));
        assert_eq!(restarted.held_exposure(LimitId([2; 16])), Ok(80));
        assert!(restarted.restore_program_reservation(&restored_record).is_err());
        assert!(!must(restarted.stage_release(record.id, ReleaseKind::Unknown, 90)).publish());
        assert_eq!(restarted.held_reservations(), Ok(2));
        let mut corrupt = bytes.to_vec(); corrupt[10] ^= 1;
        assert!(ProgramBudgetReservation::decode(&corrupt).is_err());
        assert!(ProgramBudgetReservation::decode(b"legacy scalar reservation").is_err());
        assert!(must(restarted.stage_release(record.id, ReleaseKind::Failed, 90)).publish());
        assert_eq!(restarted.consumed(LimitId([1; 16])), Ok(0));
        drop(store); must(std::fs::remove_dir_all(root));
    }
}

#[cfg(test)]
mod program_actual_allocation_tests {
    use super::*;
    use crate::budget::program_settlement::ProgramExecutedDebit;
    use crate::store::{ObjectKind, Store, TenantId, TenantKey};
    use std::os::unix::fs::PermissionsExt;

    fn must<T, E: std::fmt::Debug>(value: Result<T, E>) -> T {
        value.unwrap_or_else(|error| panic!("actual Program allocation accounting: {error:?}"))
    }

    fn rows(ids: [LimitId; 4]) -> Vec<ProgramBudgetAllocation> {
        let mut first = vec![ids[0], ids[1]]; first.sort_unstable();
        let mut second = vec![ids[0], ids[2]]; second.sort_unstable();
        vec![
            ProgramBudgetAllocation { kind: ProgramChargeKind::Principal, source: [1;32], asset:[1;32],
                destination:Some([2;32]), maximum_amount:30, applicable_limits:first },
            ProgramBudgetAllocation { kind: ProgramChargeKind::Principal, source: [1;32], asset:[1;32],
                destination:Some([3;32]), maximum_amount:50, applicable_limits:second },
            ProgramBudgetAllocation { kind: ProgramChargeKind::Fee, source: [1;32], asset:[1;32],
                destination:None, maximum_amount:10, applicable_limits:vec![ids[0]] },
            ProgramBudgetAllocation { kind: ProgramChargeKind::ProgramSpend, source: [4;32], asset:[2;32],
                destination:Some([5;32]), maximum_amount:70, applicable_limits:vec![ids[3]] },
        ]
    }

    fn configured() -> BudgetLimiter {
        let limits = must(BudgetLimiter::new((1..=4).map(|id| LimitConfig {
            id:LimitId([id;16]), name:format!("actual-{id}"), scope:LimitScope::Tenant([id;32]), ceiling:200, consumed:0,
        }).collect()));
        let bindings: Vec<_> = (1..=4).map(|id| (LimitId([id;16]),
            ProgramLimitDenomination {asset:if id==4 {[2;32]} else {[1;32]},source:None},false,false)).collect();
        must(limits.bind_program_denominations(&bindings,||Ok::<(),LimitRefusal>(())));
        limits
    }

    fn codec_record(limiter: &BudgetLimiter, rows: Vec<ProgramBudgetAllocation>) -> ProgramBudgetReservation {
        let request=ProgramReservationRequest {id:[7;32],charges:must(allocation_charges(&rows)),
            expiry_sequence:90,current_sequence:1,core_deadline:Some(CoreTimestampMs(500))};
        let stage=must(limiter.stage_program_reservation(&request,CoreTimestampMs(1)));
        let mut record=stage.record().clone(); drop(stage);
        record.allocation=Some(ProgramBudgetAllocationRecord {preparation_digest:[8;32],actor:[9;32],state_root:[10;32],
            batch_number:1,sequence:1,rows});
        must(ProgramBudgetReservation::decode(&must(record.encode())))
    }

    fn debits() -> Vec<ProgramExecutedDebit> {
        vec![
            ProgramExecutedDebit{kind:ProgramChargeKind::Principal,source:[1;32],asset:[1;32],destination:Some([2;32]),actual_amount:7},
            ProgramExecutedDebit{kind:ProgramChargeKind::Principal,source:[1;32],asset:[1;32],destination:Some([3;32]),actual_amount:11},
            ProgramExecutedDebit{kind:ProgramChargeKind::Fee,source:[1;32],asset:[1;32],destination:None,actual_amount:2},
            ProgramExecutedDebit{kind:ProgramChargeKind::ProgramSpend,source:[4;32],asset:[2;32],destination:Some([5;32]),actual_amount:19},
        ]
    }

    #[test]
    fn program_allocation_codec_retains_destinations_context_and_legacy_bytes() {
        let limiter=configured();
        let record=codec_record(&limiter,rows([LimitId([1;16]),LimitId([2;16]),LimitId([3;16]),LimitId([4;16])]));
        let bytes=must(record.encode()); assert!(bytes.starts_with(b"LXPB\x02"));
        assert_eq!(must(ProgramBudgetReservation::decode(&bytes)),record);
        let mut changed=record.clone(); changed.allocation.as_mut().unwrap_or_else(|| panic!("missing allocation")).preparation_digest=[99;32];
        assert_ne!(must(changed.settlement_binding()),must(record.settlement_binding()));
        let mut legacy=record.clone();legacy.allocation=None;
        let old=must(legacy.encode());assert!(old.starts_with(b"LXPB\x01"));
        let old=must(ProgramBudgetReservation::decode(&old));
        assert_eq!(old,legacy);assert_eq!(must(old.encode()),must(legacy.encode()));
        assert!(old.settlement_binding().is_err());
        assert!(program_actual_totals(&old,&debits()).is_err());
        let mut tampered=bytes.clone();let tail=tampered.len()-1;tampered[tail]^=1;
        assert!(ProgramBudgetReservation::decode(&tampered).is_err());
        let mut duplicate=record.clone();let allocations=&mut duplicate.allocation.as_mut().unwrap_or_else(|| panic!("missing allocation")).rows;
        allocations.insert(1,allocations[0].clone());assert!(duplicate.encode().is_err());
        let mut wrong=record.clone();wrong.allocation.as_mut().unwrap_or_else(|| panic!("missing allocation")).rows[2].destination=Some([3;32]);
        assert!(wrong.encode().is_err());
    }

    #[test]
    fn program_actual_debits_preserve_every_counterparty_asset_and_fee_limit() {
        let limiter=configured();
        let record=codec_record(&limiter,rows([LimitId([1;16]),LimitId([2;16]),LimitId([3;16]),LimitId([4;16])]));
        must(limiter.restore_program_reservation(&record));
        let (totals,actual)=must(program_actual_totals(&record,&debits()));
        assert_eq!(totals,BTreeMap::from([(LimitId([1;16]),20),(LimitId([2;16]),7),(LimitId([3;16]),11),(LimitId([4;16]),19)]));
        let marker=must(program_settlement_marker(&record,[11;32],&actual));
        let stage=must(limiter.stage_program_actuals(&record,&totals,marker.clone(),None));
        drop(stage);assert_eq!(limiter.held_exposure(LimitId([1;16])),Ok(90));
        assert!(limiter.stage_release(record.id,ReleaseKind::Executed,2).is_err());
        assert!(limiter.stage_release(record.id,ReleaseKind::Failed,2).is_err());
        let stage=must(limiter.stage_program_actuals(&record,&totals,marker.clone(),None));
        assert!(stage.publish());assert_eq!(limiter.consumed(LimitId([1;16])),Ok(20));
        assert_eq!(limiter.consumed(LimitId([4;16])),Ok(19));assert_eq!(limiter.held_reservations(),Ok(0));
        let replay=must(limiter.stage_program_actuals(&record,&totals,marker.clone(),Some(&marker)));
        assert!(replay.replayed());assert!(!replay.publish());
        assert!(limiter.stage_program_actuals(&record,&totals,marker.clone(),None).is_err());
        let other=must(program_settlement_marker(&record,[12;32],&actual));
        assert!(limiter.stage_program_actuals(&record,&totals,other,Some(&marker)).is_err());
    }

    #[test]
    fn program_actual_debits_refuse_unreserved_overdrawn_or_wrong_source_rows() {
        let limiter=configured();
        let record=codec_record(&limiter,rows([LimitId([1;16]),LimitId([2;16]),LimitId([3;16]),LimitId([4;16])]));
        must(limiter.restore_program_reservation(&record));
        for bad in [
            ProgramExecutedDebit{actual_amount:31,..debits()[0].clone()},
            ProgramExecutedDebit{source:[88;32],..debits()[0].clone()},
            ProgramExecutedDebit{asset:[88;32],..debits()[0].clone()},
            ProgramExecutedDebit{destination:Some([88;32]),..debits()[0].clone()},
            ProgramExecutedDebit{kind:ProgramChargeKind::Fee,..debits()[0].clone()},
            ProgramExecutedDebit{actual_amount:0,..debits()[0].clone()},
            ProgramExecutedDebit{actual_amount:11,..debits()[2].clone()},
        ] { assert!(program_actual_totals(&record,&[bad]).is_err()); }
        let repeated=vec![ProgramExecutedDebit{actual_amount:20,..debits()[0].clone()};2];
        assert!(program_actual_totals(&record,&repeated).is_err());
        let (zero,actual)=must(program_actual_totals(&record,&[]));
        assert!(zero.values().all(|amount| *amount==0));assert_eq!(actual,vec![0;4]);
        assert_eq!(limiter.held_exposure(LimitId([1;16])),Ok(90));
        assert_eq!(limiter.consumed(LimitId([1;16])),Ok(0));
    }

    #[test]
    fn program_actual_store_commit_is_atomic_and_restart_replays_without_double_charge() {
        let stamp=must(std::time::SystemTime::now().duration_since(std::time::UNIX_EPOCH)).as_nanos();
        let root=std::env::temp_dir().join(format!("program-actual-budget-{}-{stamp}",std::process::id()));
        must(std::fs::create_dir_all(&root));
        let tenant=must(TenantId::new("actual-budget"));
        let configs:Vec<_>=(1..=4).map(|id| LimitConfig{id:LimitId([id;16]),name:format!("limit-{id}"),
            scope:match id {2=>LimitScope::Counterparty([2;32]),3=>LimitScope::Counterparty([3;32]),_=>LimitScope::Tenant([id;32])},
            ceiling:200,consumed:0}).collect();
        let mut store=must(Store::open(root.join("store")));
        let limiter=must(BudgetLimiter::new(Vec::new()));
        let installed=must(crate::budget::install_enrolment_limits(&mut store,&limiter,&tenant,&configs));
        let declaration=serde_json::json!({"version":1,"tenant":tenant.as_str(),"limits":installed.iter().enumerate()
            .map(|(index,record)|serde_json::json!({"stable_id":hex::encode(record.stable_id),
                "asset":hex::encode(if index==3 {[2_u8;32]} else {[1_u8;32]}),"source":"*"})).collect::<Vec<_>>()});
        let source=root.join("denominations.json");must(std::fs::write(&source,must(serde_json::to_vec(&declaration))));
        must(std::fs::set_permissions(&source,std::fs::Permissions::from_mode(0o600)));
        let source=must(crate::config::load_program_budget_denominations(&source,&tenant));
        must(crate::budget::install_program_enrolment_denominations(&mut store,&limiter,&source));
        let ids=[installed[0].limit_id,installed[1].limit_id,installed[2].limit_id,installed[3].limit_id];
        let principal=crate::budget::ResolvedProgramCharge {source:crate::budget::ResolvedProgramSource::Principal{account:[1;32]},
            asset:[1;32],destination:Some([2;32]),maximum_amount:30};
        let selected=must(crate::budget::program_allocation_charge_limits(&store,&tenant,&configs,&[],&principal));
        let mut expected=vec![ids[0],ids[1]];expected.sort_unstable();assert_eq!(selected,expected);
        let fee=crate::budget::ResolvedProgramCharge {source:crate::budget::ResolvedProgramSource::Fee{account:[1;32]},
            asset:[1;32],destination:None,maximum_amount:10};
        assert_eq!(must(crate::budget::program_allocation_charge_limits(&store,&tenant,&configs,&[],&fee)),vec![ids[0]]);
        let record=codec_record(&limiter,rows(ids));
        assert!(crate::budget::program_consumption_updates(&store,&tenant,&record).is_err());
        let companion=must(TenantKey::new(tenant.clone(),ObjectKind::Configuration,b"allocation-companion".to_vec()));
        must(store.put_local(companion.clone(),must(record.encode())));
        must(limiter.restore_program_reservation(&record));
        let mut live_configs=configs.clone();
        live_configs[0]=LimitConfig{id:LimitId([6;16]),name:"renewed-global".to_owned(),scope:configs[0].scope,ceiling:150,consumed:0};
        let renewed=must(crate::budget::install_enrolment_renewals(&mut store,&limiter,&tenant,&live_configs,
            &[crate::budget::EnrolmentPredecessor{successor:live_configs[0].id,predecessor:configs[0].id}]));
        let successor=renewed.iter().find(|row|row.enrolment==live_configs[0].id)
            .unwrap_or_else(||panic!("missing successor")).limit_id;
        assert_eq!(limiter.held_exposure(successor),Ok(90));
        assert_eq!(must(crate::budget::program_settlement_recorded(&store,&tenant,record.id)),false);
        let (totals,actual)=must(program_actual_totals(&record,&debits()));
        let marker=must(program_settlement_marker(&record,[11;32],&actual));
        let key=must(crate::budget::program_settlement_key(&tenant,record.id));
        let stage=must(limiter.stage_program_actuals(&record,&totals,marker.clone(),None));
        let stage=must(crate::budget::finish_program_settlement_stage(&store,&tenant,key.clone(),stage));
        let mut updates=stage.updates().to_vec();
        let missing=must(TenantKey::new(tenant.clone(),ObjectKind::Configuration,b"missing-companion".to_vec()));
        updates.push((missing,b"settled".to_vec()));
        assert!(store.apply_program_approval_batch(updates,stage.inserts().to_vec(),Vec::new()).is_err());
        drop(stage);assert!(store.get(&key).is_none());assert_eq!(limiter.consumed(ids[0]),Ok(0));
        assert_eq!(limiter.held_exposure(ids[0]),Ok(90));
        let stage=must(limiter.stage_program_actuals(&record,&totals,marker.clone(),None));
        let stage=must(crate::budget::finish_program_settlement_stage(&store,&tenant,key.clone(),stage));
        let mut updates=stage.updates().to_vec();updates.push((companion.clone(),b"settled".to_vec()));
        must(store.apply_program_approval_batch(updates,stage.inserts().to_vec(),Vec::new()));
        drop(stage);drop(limiter);drop(store);
        let mut store=must(Store::open(root.join("store")));
        let restarted=must(BudgetLimiter::new(Vec::new()));
        must(crate::budget::install_enrolment_limits(&mut store,&restarted,&tenant,&live_configs));
        assert_eq!(restarted.consumed(successor),Ok(20));
        assert_eq!(must(crate::budget::program_settlement_recorded(&store,&tenant,record.id)),true);
        assert_eq!(restarted.consumed(ids[0]),Ok(20));assert_eq!(restarted.consumed(ids[1]),Ok(7));
        assert_eq!(restarted.consumed(ids[2]),Ok(11));assert_eq!(restarted.consumed(ids[3]),Ok(19));
        assert_eq!(store.get(&companion).map(|value|value.bytes()),Some(b"settled".as_slice()));
        let prior=store.get(&key).map(|value|value.bytes());
        let replay=must(restarted.stage_program_actuals(&record,&totals,marker,prior));
        let replay=must(crate::budget::finish_program_settlement_stage(&store,&tenant,key,replay));
        assert!(replay.replayed());assert!(replay.updates().is_empty());assert!(replay.inserts().is_empty());
        assert!(!replay.publish());assert_eq!(restarted.consumed(ids[0]),Ok(20));
        drop(restarted);drop(store);must(std::fs::remove_dir_all(root));
    }
    #[test]
    fn program_unsigned_core_expiry_accounting_preserves_early_and_unresolved_holds() {
        let limiter=configured();
        let record=codec_record(&limiter,rows([LimitId([1;16]),LimitId([2;16]),LimitId([3;16]),LimitId([4;16])]));
        must(limiter.restore_program_reservation(&record));
        assert!(limiter.stage_program_core_expired_allocation(&record,CoreTimestampMs(499)).is_err());
        assert!(limiter.stage_program_core_expired_allocation(&record,CoreTimestampMs(0)).is_err());
        assert!(limiter.stage_release(record.id,ReleaseKind::Failed,2).is_err());
        let staged=must(limiter.stage_program_core_expired_allocation(&record,CoreTimestampMs(500)));
        drop(staged);assert_eq!(limiter.held_exposure(LimitId([1;16])),Ok(90));
        assert_eq!(limiter.consumed(LimitId([1;16])),Ok(0));
        let staged=must(limiter.stage_program_core_expired_allocation(&record,CoreTimestampMs(500)));
        assert!(staged.publish());assert_eq!(limiter.held_reservations(),Ok(0));
        assert_eq!(limiter.consumed(LimitId([1;16])),Ok(0));
        assert!(limiter.stage_program_core_expired_allocation(&record,CoreTimestampMs(500)).is_err());
    }

    #[test]
    fn program_unsigned_rejection_accounting_releases_only_the_exact_complete_hold() {
        let limiter=configured();
        let record=codec_record(&limiter,rows([LimitId([1;16]),LimitId([2;16]),LimitId([3;16]),LimitId([4;16])]));
        must(limiter.restore_program_reservation(&record));
        let mut wrong=record.clone();wrong.id=[99;32];
        assert!(limiter.stage_program_unsigned_allocation(&wrong).is_err());
        let staged=must(limiter.stage_program_unsigned_allocation(&record));
        drop(staged);assert_eq!(limiter.held_exposure(LimitId([1;16])),Ok(90));
        assert!(limiter.stage_release(record.id,ReleaseKind::Failed,2).is_err());
        let staged=must(limiter.stage_program_unsigned_allocation(&record));
        assert!(staged.publish());assert_eq!(limiter.held_reservations(),Ok(0));
        for id in 1..=4 { assert_eq!(limiter.consumed(LimitId([id;16])),Ok(0)); }
        assert!(limiter.stage_program_unsigned_allocation(&record).is_err());
    }

}
