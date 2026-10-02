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
}

/// One held amount with its head-sequence bound and, for time-bounded
/// preparations, the core-clock deadline at which it lapses.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
struct Hold {
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
            },
        );
        persist(consumed)?;
        *limits = renewed;
        Ok(())
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
    if request.amount == 0
        || request.expiry_sequence <= request.current_sequence
        || request.applicable_limits.is_empty()
    {
        return Err(LimitRefusal::InvalidRequest);
    }
    let mut applicable = request.applicable_limits.clone();
    applicable.sort_unstable();
    applicable.dedup();
    let mut limits = limiter.limits.lock().map_err(|_| LimitRefusal::Poisoned)?;
    for id in &applicable {
        let limit = limits.get(id).ok_or(LimitRefusal::UnknownLimit(*id))?;
        if limit.retired {
            return Err(LimitRefusal::Retired(*id));
        }
        if limit.held.contains_key(&request.id) {
            return Err(LimitRefusal::InvalidRequest);
        }
        let held = lineage_held(&limits, *id)?;
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
        if limit.config.scope != record.scope
            || limit.config.ceiling != record.ceiling
            || limit.held.contains_key(&record.reservation_id)
        {
            return Err(LimitRefusal::InvalidConfiguration);
        }
        limit.held.insert(
            record.reservation_id,
            Hold {
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
