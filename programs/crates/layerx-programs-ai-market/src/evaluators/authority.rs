//! F03 evaluator grant authority over the shared identity section: Owner
//! nomination, next-epoch key rotation, terminal revocation and the epoch-open
//! snapshot. The evaluator region follows the F02 worker table inside the same
//! section. Only F08 accepted membership (evaluator-owner kind0 acceptance plus
//! registered delegate proof of possession) lets a pending nomination activate.
//! Principal identifiers cannot reveal hidden common control or collusion; that
//! remains an explicit trust assumption.
use crate::{
    admission::{AdmissionMeta, AdmissionTable, Participant},
    codec::{self, Reader, Writer},
    dispatch,
    errors::{
        CodecResult, ARITHMETIC, CAPACITY, CONFLICT, EXPIRED, F03_BAD_ACTIVATION,
        F03_EVALUATOR_CAPACITY, F03_GRANT_VERSION_CONFLICT, F03_KEY_REUSED,
        F03_KEY_VERSION_CONFLICT, F03_NO_GRANT, NON_CANONICAL, NOT_FOUND, REVOKED, ROLE_CONFLICT,
        UNAUTHORIZED, UNKNOWN_OPERATION, WRONG_CONFIG, WRONG_EPOCH, WRONG_PHASE, WRONG_ROSTER,
    },
    evaluators::{
        codec::{decode_grant, encode_grant},
        model::{nonzero_key, EvaluatorGrant, GrantStatus, GrantTerms, GRANT_BYTES},
    },
    registry::{market_clock, MarketHeader},
    registry_ops::{ACTIVE, REGISTERED, SUSPENDED, WINDING_DOWN},
    state::{self, ActorSlot, ReplayDecision, ReplayRequest, RetainedResult, Section},
    types::{
        Digest32, EvaluatorId, EvaluatorRosterEntry, Presence, PrincipalId, ProgramId, PublicKey32,
        RequestDigest, RequestId, ResultDigest, RubricDigest, Version, WorkerRosterEntry,
    },
    workers::{WorkerTable, WORKER_RECORD_BYTES},
    MAX_EVALUATORS, MAX_WORKERS,
};

pub const LAST_REQUEST_BYTES: usize = 104;
pub const REKEY_BYTES: usize = 48;
pub const REVOCATION_BYTES: usize = 43;
pub const RECORD_BYTES: usize = GRANT_BYTES + LAST_REQUEST_BYTES + REKEY_BYTES + REVOCATION_BYTES;
pub const FROZEN_BYTES: usize = 152;
/// Record count, snapshot presence, snapshot epoch and frozen count.
pub const REGION_FRAMING_BYTES: usize = 11;
pub const REGION_MAX_BYTES: usize =
    REGION_FRAMING_BYTES + MAX_EVALUATORS * (RECORD_BYTES + FROZEN_BYTES);
pub const SCHEDULE_PAYLOAD_BYTES: usize = 160;
pub const ROTATE_PAYLOAD_BYTES: usize = 88;
pub const REVOKE_PAYLOAD_BYTES: usize = 74;
pub const SCHEDULE_EVENT_BYTES: usize = 64;
pub const ROTATE_EVENT_BYTES: usize = 48;
pub const REVOKE_EVENT_BYTES: usize = 75;
/// Caller scratch: one identity section payload plus one control payload.
pub const SCRATCH_BYTES: usize =
    Section::IdentityRoster.payload_cap() + Section::Control.payload_cap();
const _: () = assert!(RECORD_BYTES == 356 && REGION_MAX_BYTES <= 4_288);

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
#[repr(u16)]
pub enum RevocationReason {
    CompromisedKey = 1,
    PolicyViolation = 2,
    KnownIdentityConflict = 3,
    OperatorRemoval = 4,
}
impl RevocationReason {
    /// # Errors
    /// `NON_CANONICAL` for any value outside 1..=4.
    pub fn decode(value: u16) -> CodecResult<Self> {
        match value {
            1 => Ok(Self::CompromisedKey),
            2 => Ok(Self::PolicyViolation),
            3 => Ok(Self::KnownIdentityConflict),
            4 => Ok(Self::OperatorRemoval),
            _ => Err(NON_CANONICAL),
        }
    }
}

/// The last Owner request that changed this record (common replay identity).
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct LastRequest {
    pub sequence: u64,
    pub request: RequestId,
    pub digest: RequestDigest,
    pub result: ResultDigest,
}

/// A staged replacement key; it never changes a running snapshot.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct PendingRekey {
    pub key: PublicKey32,
    pub key_version: Version,
    pub effective_epoch: u64,
}

/// Terminal revocation. `excludes_frozen_epoch` records whether it preceded the
/// aggregate seal of the epoch whose snapshot contained the evaluator.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct Revocation {
    pub reason: RevocationReason,
    pub evidence: Digest32,
    pub height: u64,
    pub excludes_frozen_epoch: bool,
}

/// Current evaluator record: grant 161, last request 104, rekey 48, revocation 43.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct EvaluatorRecord {
    pub grant: EvaluatorGrant,
    pub last: LastRequest,
    pub rekey: Option<PendingRekey>,
    pub revocation: Option<Revocation>,
}
impl EvaluatorRecord {
    fn validate(&self) -> CodecResult<()> {
        self.grant.validate()?;
        if (self.grant.status == GrantStatus::Revoked) != self.revocation.is_some()
            || self.last.sequence == 0
        {
            return Err(NON_CANONICAL);
        }
        if let Some(rekey) = self.rekey {
            nonzero_key(rekey.key)?;
            if self.grant.status != GrantStatus::Active
                || rekey.key_version <= self.grant.key_version
                || rekey.key == self.grant.signing_key
                || rekey.effective_epoch >= self.grant.expiry_epoch_exclusive
            {
                return Err(NON_CANONICAL);
            }
        }
        Ok(())
    }
    fn write(&self, w: &mut Writer<'_>) -> CodecResult<()> {
        let mut grant = [0u8; GRANT_BYTES];
        encode_grant(&self.grant, &mut grant)?;
        w.put(&grant)?;
        w.u64(self.last.sequence)?;
        w.put(self.last.request.as_bytes())?;
        w.put(self.last.digest.as_bytes())?;
        w.put(self.last.result.as_bytes())?;
        match self.rekey {
            Some(rekey) => {
                w.put(&rekey.key.0)?;
                w.u64(rekey.key_version.get())?;
                w.u64(rekey.effective_epoch)?;
            }
            None => w.put(&[0; REKEY_BYTES])?,
        }
        match self.revocation {
            Some(revocation) => {
                w.u16(revocation.reason as u16)?;
                w.put(revocation.evidence.as_bytes())?;
                w.u64(revocation.height)?;
                w.u8(1 + u8::from(revocation.excludes_frozen_epoch))
            }
            None => w.put(&[0; REVOCATION_BYTES]),
        }
    }
    fn read(r: &mut Reader<'_>) -> CodecResult<Self> {
        let grant = decode_grant(r.take(GRANT_BYTES)?)?;
        let last = LastRequest {
            sequence: r.u64()?,
            request: RequestId::new(r.fixed()?)?,
            digest: RequestDigest::new(r.fixed()?)?,
            result: ResultDigest::new(r.fixed()?)?,
        };
        let rekey_bytes = r.take(REKEY_BYTES)?;
        let rekey = if rekey_bytes.iter().all(|b| *b == 0) {
            None
        } else {
            let mut k = Reader::new(rekey_bytes);
            let rekey = PendingRekey {
                key: PublicKey32(k.fixed()?),
                key_version: Version::new(k.u64()?)?,
                effective_epoch: k.u64()?,
            };
            k.finish()?;
            Some(rekey)
        };
        let revocation_bytes = r.take(REVOCATION_BYTES)?;
        let revocation = if revocation_bytes.iter().all(|b| *b == 0) {
            None
        } else {
            let mut k = Reader::new(revocation_bytes);
            let reason = RevocationReason::decode(k.u16()?)?;
            let evidence = Digest32::new(k.fixed()?)?;
            let height = k.u64()?;
            let excludes_frozen_epoch = match k.u8()? {
                1 => false,
                2 => true,
                _ => return Err(NON_CANONICAL),
            };
            k.finish()?;
            Some(Revocation {
                reason,
                evidence,
                height,
                excludes_frozen_epoch,
            })
        };
        let record = Self {
            grant,
            last,
            rekey,
            revocation,
        };
        record.validate()?;
        Ok(record)
    }
}

/// Frozen snapshot entry: the common roster entry plus its exclusive expiry.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct FrozenEvaluator {
    pub entry: EvaluatorRosterEntry,
    pub expiry_epoch_exclusive: u64,
}
impl FrozenEvaluator {
    fn write(&self, w: &mut Writer<'_>) -> CodecResult<()> {
        w.put(self.entry.evaluator.as_bytes())?;
        w.put(self.entry.owner.as_bytes())?;
        w.u64(self.entry.grant.get())?;
        w.u64(self.entry.key_version.get())?;
        w.put(&self.entry.public_key.0)?;
        w.put(self.entry.rubric.as_bytes())?;
        w.u64(self.expiry_epoch_exclusive)
    }
    fn read(r: &mut Reader<'_>) -> CodecResult<Self> {
        let value = Self {
            entry: EvaluatorRosterEntry {
                evaluator: EvaluatorId::new(r.fixed()?)?,
                owner: PrincipalId::new(r.fixed()?)?,
                grant: Version::new(r.u64()?)?,
                key_version: Version::new(r.u64()?)?,
                public_key: PublicKey32(r.fixed()?),
                rubric: RubricDigest::new(r.fixed()?)?,
            },
            expiry_epoch_exclusive: r.u64()?,
        };
        nonzero_key(value.entry.public_key)?;
        Ok(value)
    }
}

/// Immutable eligible evaluator list of one opened epoch, sorted by evaluator ID.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct Snapshot {
    pub epoch: u64,
    entries: [Option<FrozenEvaluator>; MAX_EVALUATORS],
    count: usize,
}
impl Snapshot {
    const fn new(epoch: u64) -> Self {
        Self {
            epoch,
            entries: [None; MAX_EVALUATORS],
            count: 0,
        }
    }
    #[must_use]
    pub const fn len(&self) -> usize {
        self.count
    }
    #[must_use]
    pub const fn is_empty(&self) -> bool {
        self.count == 0
    }
    pub fn entries(&self) -> impl Iterator<Item = &FrozenEvaluator> {
        self.entries[..self.count].iter().flatten()
    }
    #[must_use]
    pub fn get(&self, evaluator: EvaluatorId) -> Option<FrozenEvaluator> {
        self.entries()
            .find(|f| f.entry.evaluator == evaluator)
            .copied()
    }
    fn push(&mut self, value: FrozenEvaluator) -> CodecResult<()> {
        if self.count >= MAX_EVALUATORS {
            return Err(F03_EVALUATOR_CAPACITY);
        }
        if self
            .entries()
            .last()
            .is_some_and(|p| p.entry.evaluator >= value.entry.evaluator)
        {
            return Err(NON_CANONICAL);
        }
        self.entries[self.count] = Some(value);
        self.count += 1;
        Ok(())
    }
}

const fn live(status: GrantStatus) -> bool {
    matches!(status, GrantStatus::Pending | GrantStatus::Active)
}

/// Bounded F03 region: current records sorted by evaluator ID, then the
/// optional frozen snapshot of the last opened epoch.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct EvaluatorRegion {
    records: [Option<EvaluatorRecord>; MAX_EVALUATORS],
    count: usize,
    snapshot: Option<Snapshot>,
}
impl Default for EvaluatorRegion {
    fn default() -> Self {
        Self::new()
    }
}
impl EvaluatorRegion {
    #[must_use]
    pub const fn new() -> Self {
        Self {
            records: [None; MAX_EVALUATORS],
            count: 0,
            snapshot: None,
        }
    }
    #[must_use]
    pub const fn len(&self) -> usize {
        self.count
    }
    #[must_use]
    pub const fn is_empty(&self) -> bool {
        self.count == 0
    }
    pub fn records(&self) -> impl Iterator<Item = &EvaluatorRecord> {
        self.records[..self.count].iter().flatten()
    }
    #[must_use]
    pub fn get(&self, evaluator: EvaluatorId) -> Option<&EvaluatorRecord> {
        self.records().find(|r| r.grant.evaluator == evaluator)
    }
    #[must_use]
    pub const fn snapshot(&self) -> Option<&Snapshot> {
        self.snapshot.as_ref()
    }
    /// Sorted insertion of one valid record.
    ///
    /// # Errors
    /// Record validation errors, `F03_EVALUATOR_CAPACITY` and `CONFLICT` for a duplicate ID.
    pub fn insert(&mut self, record: &EvaluatorRecord) -> CodecResult<()> {
        record.validate()?;
        if self.get(record.grant.evaluator).is_some() {
            return Err(CONFLICT);
        }
        if self.count >= MAX_EVALUATORS {
            return Err(F03_EVALUATOR_CAPACITY);
        }
        let at = self.records[..self.count]
            .iter()
            .position(|r| r.is_some_and(|r| r.grant.evaluator > record.grant.evaluator))
            .unwrap_or(self.count);
        self.records.copy_within(at..self.count, at + 1);
        self.records[at] = Some(*record);
        self.count += 1;
        Ok(())
    }
    /// Replaces the record with the same evaluator ID.
    ///
    /// # Errors
    /// Record validation errors and `NOT_FOUND`.
    pub fn replace(&mut self, record: &EvaluatorRecord) -> CodecResult<()> {
        record.validate()?;
        let at = self.records[..self.count]
            .iter()
            .position(|r| r.is_some_and(|r| r.grant.evaluator == record.grant.evaluator))
            .ok_or(NOT_FOUND)?;
        self.records[at] = Some(*record);
        Ok(())
    }
    /// The live current grant, as F08 `AdmitEvaluator` and F04 must read it.
    ///
    /// # Errors
    /// `F03_NO_GRANT` when no record exists.
    pub fn stored_grant(&self, evaluator: EvaluatorId) -> CodecResult<EvaluatorGrant> {
        self.get(evaluator).map(|r| r.grant).ok_or(F03_NO_GRANT)
    }
    /// The grant frozen into the current snapshot, independent of later changes.
    ///
    /// # Errors
    /// `F03_NO_GRANT` when the evaluator is not in the snapshot.
    pub fn frozen_grant(&self, evaluator: EvaluatorId) -> CodecResult<EvaluatorGrant> {
        let frozen = self
            .snapshot
            .as_ref()
            .and_then(|s| s.get(evaluator))
            .ok_or(F03_NO_GRANT)?;
        let record = self.get(evaluator).ok_or(NON_CANONICAL)?;
        Ok(EvaluatorGrant {
            evaluator,
            principal: frozen.entry.owner,
            rubric: frozen.entry.rubric,
            grant_version: frozen.entry.grant,
            key_version: frozen.entry.key_version,
            signing_key: frozen.entry.public_key,
            effective_epoch: record.grant.effective_epoch,
            expiry_epoch_exclusive: frozen.expiry_epoch_exclusive,
            status: GrantStatus::Active,
        })
    }
    /// Live exclusion overlay for the frozen epoch: revoked before its aggregate seal.
    #[must_use]
    pub fn excluded(&self, evaluator: EvaluatorId) -> bool {
        self.get(evaluator)
            .and_then(|r| r.revocation)
            .is_some_and(|r| r.excludes_frozen_epoch)
    }
    fn key_in_use(&self, key: PublicKey32) -> bool {
        self.records()
            .any(|r| r.grant.signing_key == key || r.rekey.is_some_and(|k| k.key == key))
            || self
                .snapshot
                .as_ref()
                .is_some_and(|s| s.entries().any(|f| f.entry.public_key == key))
    }
    fn frozen(&self, evaluator: EvaluatorId) -> bool {
        self.snapshot
            .as_ref()
            .is_some_and(|s| s.get(evaluator).is_some())
    }
    fn check(&self) -> CodecResult<()> {
        if self.count > MAX_EVALUATORS {
            return Err(CAPACITY);
        }
        let mut previous: Option<EvaluatorId> = None;
        for record in self.records() {
            record.validate()?;
            if previous.is_some_and(|p| p >= record.grant.evaluator)
                || record.revocation.is_some_and(|r| {
                    r.excludes_frozen_epoch && !self.frozen(record.grant.evaluator)
                })
            {
                return Err(NON_CANONICAL);
            }
            previous = Some(record.grant.evaluator);
        }
        if let Some(snapshot) = &self.snapshot {
            for frozen in snapshot.entries() {
                let record = self.get(frozen.entry.evaluator).ok_or(NON_CANONICAL)?;
                let grant = &record.grant;
                if !matches!(grant.status, GrantStatus::Active | GrantStatus::Revoked)
                    || grant.principal != frozen.entry.owner
                    || grant.grant_version != frozen.entry.grant
                    || grant.key_version != frozen.entry.key_version
                    || grant.signing_key != frozen.entry.public_key
                    || grant.rubric != frozen.entry.rubric
                    || grant.expiry_epoch_exclusive != frozen.expiry_epoch_exclusive
                {
                    return Err(NON_CANONICAL);
                }
            }
        }
        Ok(())
    }
    #[must_use]
    pub fn encoded_len(&self) -> usize {
        if self.count == 0 && self.snapshot.is_none() {
            return 0;
        }
        let frozen = self
            .snapshot
            .as_ref()
            .map_or(0, |s| 9 + s.len() * FROZEN_BYTES);
        2 + self.count * RECORD_BYTES + frozen
    }
    /// An empty region (no record, no snapshot) encodes as zero bytes.
    ///
    /// # Errors
    /// `CAPACITY` when `out` is too small and `NON_CANONICAL` for an invalid region.
    pub fn encode(&self, out: &mut [u8]) -> CodecResult<usize> {
        self.check()?;
        let n = self.encoded_len();
        let target = out.get_mut(..n).ok_or(CAPACITY)?;
        if n == 0 {
            return Ok(0);
        }
        let mut w = Writer::new(target);
        w.u8(u8::try_from(self.count).map_err(|_| ARITHMETIC)?)?;
        for record in self.records() {
            record.write(&mut w)?;
        }
        match &self.snapshot {
            Some(snapshot) => {
                w.u8(1)?;
                w.u64(snapshot.epoch)?;
                w.u8(u8::try_from(snapshot.len()).map_err(|_| ARITHMETIC)?)?;
                for frozen in snapshot.entries() {
                    frozen.write(&mut w)?;
                }
            }
            None => w.u8(0)?,
        }
        Ok(w.len())
    }
    /// # Errors
    /// `CAPACITY` and `NON_CANONICAL` for any non-canonical or inconsistent region.
    pub fn decode(input: &[u8]) -> CodecResult<Self> {
        let mut region = Self::new();
        if input.is_empty() {
            return Ok(region);
        }
        if input.len() > REGION_MAX_BYTES {
            return Err(CAPACITY);
        }
        let mut r = Reader::new(input);
        let count = usize::from(r.u8()?);
        if count > MAX_EVALUATORS {
            return Err(CAPACITY);
        }
        for _ in 0..count {
            let record = EvaluatorRecord::read(&mut r)?;
            if region
                .records()
                .last()
                .is_some_and(|p| p.grant.evaluator >= record.grant.evaluator)
            {
                return Err(NON_CANONICAL);
            }
            region.insert(&record)?;
        }
        if r.boolean()? {
            let mut snapshot = Snapshot::new(r.u64()?);
            let frozen = usize::from(r.u8()?);
            if frozen > MAX_EVALUATORS {
                return Err(CAPACITY);
            }
            for _ in 0..frozen {
                snapshot.push(FrozenEvaluator::read(&mut r)?)?;
            }
            region.snapshot = Some(snapshot);
        } else if count == 0 {
            return Err(NON_CANONICAL);
        }
        r.finish()?;
        region.check()?;
        Ok(region)
    }
}

/// Splits the identity section into the F02 worker table prefix and the F03
/// region. A non-empty section always starts with the worker count byte.
///
/// # Errors
/// `NON_CANONICAL` when the worker prefix is truncated.
pub fn split_identity_section(section: &[u8]) -> CodecResult<(&[u8], &[u8])> {
    let Some(&count) = section.first() else {
        return Ok((section, section));
    };
    let workers = usize::from(count)
        .checked_mul(WORKER_RECORD_BYTES)
        .and_then(|n| n.checked_add(1))
        .ok_or(ARITHMETIC)?;
    if section.len() < workers {
        return Err(NON_CANONICAL);
    }
    Ok(section.split_at(workers))
}

/// Decodes the F03 region of an identity section.
///
/// # Errors
/// Split and region decode errors.
pub fn evaluator_region(section: &[u8]) -> CodecResult<EvaluatorRegion> {
    EvaluatorRegion::decode(split_identity_section(section)?.1)
}

fn compose(workers: &[u8], region: &EvaluatorRegion, out: &mut [u8]) -> CodecResult<usize> {
    let region_len = region.encoded_len();
    let prefix: &[u8] = if workers.is_empty() && region_len > 0 {
        &[0]
    } else {
        workers
    };
    let total = prefix.len().checked_add(region_len).ok_or(ARITHMETIC)?;
    if total > Section::IdentityRoster.payload_cap() || out.len() < total {
        return Err(CAPACITY);
    }
    out[..prefix.len()].copy_from_slice(prefix);
    region.encode(&mut out[prefix.len()..total])?;
    Ok(total)
}

/// Authenticated host values for one direct F03 call. `approved_rubric` is the
/// F01 active policy rubric; `aggregate_sealed` is the F05 seal status of the
/// currently frozen epoch at this activity.
#[derive(Clone, Copy, Debug)]
pub struct AuthorityContext<'m> {
    pub market: &'m MarketHeader,
    pub invoking_principal: PrincipalId,
    pub immediate_caller: Presence<ProgramId>,
    pub height: u64,
    pub approved_rubric: RubricDigest,
    pub aggregate_sealed: bool,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum Outcome {
    Applied {
        state_len: usize,
        event_len: usize,
    },
    AlreadyApplied(RetainedResult),
    /// An identical repeated revocation: no state change, event or sequence use.
    Idempotent,
}

fn check_lifecycle(lifecycle: u8, revocation: bool) -> CodecResult<()> {
    match lifecycle {
        REGISTERED | ACTIVE => Ok(()),
        SUSPENDED | WINDING_DOWN if revocation => Ok(()),
        _ => Err(WRONG_PHASE),
    }
}

fn check_epoch_binding(e: &codec::Envelope<'_>, snapshot: Option<&Snapshot>) -> CodecResult<()> {
    if snapshot.is_some() && e.roster == Presence::Absent {
        return Err(WRONG_ROSTER);
    }
    if e.epoch == snapshot.map_or(0, |s| s.epoch) {
        Ok(())
    } else {
        Err(WRONG_EPOCH)
    }
}

/// Epoch 0 before any opening, otherwise the next future clock epoch.
fn next_activation(ctx: &AuthorityContext<'_>, snapshot: Option<&Snapshot>) -> CodecResult<u64> {
    if snapshot.is_none() {
        return Ok(0);
    }
    if ctx.height < ctx.market.origin_height {
        return Err(WRONG_EPOCH);
    }
    market_clock(ctx.market.origin_height, ctx.height)?
        .epoch
        .checked_add(1)
        .ok_or(ARITHMETIC)
}

fn exact(payload: &[u8], length: usize) -> CodecResult<Reader<'_>> {
    if payload.len() == length {
        Ok(Reader::new(payload))
    } else {
        Err(NON_CANONICAL)
    }
}

fn stamp(replay: &ReplayRequest, suffix: &[u8]) -> CodecResult<LastRequest> {
    Ok(LastRequest {
        sequence: replay.sequence,
        request: replay.request_id,
        digest: replay.digest,
        result: codec::result_digest(suffix)?,
    })
}

fn schedule(
    region: &mut EvaluatorRegion,
    ctx: &AuthorityContext<'_>,
    workers: &[u8],
    replay: &ReplayRequest,
    payload: &[u8],
    suffix: &mut [u8],
) -> CodecResult<usize> {
    let mut r = exact(payload, SCHEDULE_PAYLOAD_BYTES)?;
    let principal = PrincipalId::new(r.fixed()?)?;
    let nonce: [u8; 32] = r.fixed()?;
    let rubric = RubricDigest::new(r.fixed()?)?;
    let key = PublicKey32(r.fixed()?);
    let grant_version = Version::new(r.u64()?)?;
    let key_version = Version::new(r.u64()?)?;
    let effective_epoch = r.u64()?;
    let expiry_epoch_exclusive = r.u64()?;
    r.finish()?;
    if nonce == [0; 32] {
        return Err(NON_CANONICAL);
    }
    nonzero_key(key)?;
    if rubric != ctx.approved_rubric {
        return Err(WRONG_CONFIG);
    }
    if principal == ctx.market.owner_principal
        || WorkerTable::decode(workers)?
            .iter()
            .any(|w| w.owner == principal)
    {
        return Err(ROLE_CONFLICT);
    }
    if effective_epoch != next_activation(ctx, region.snapshot())? {
        return Err(F03_BAD_ACTIVATION);
    }
    let grant = EvaluatorGrant::nominate(
        ctx.market.market_id,
        principal,
        nonce,
        GrantTerms {
            rubric,
            grant_version,
            key_version,
            signing_key: key,
            effective_epoch,
            expiry_epoch_exclusive,
        },
    )?;
    let evaluator = grant.evaluator;
    if region.records().any(|r| {
        r.grant.evaluator != evaluator && live(r.grant.status) && r.grant.principal == principal
    }) {
        return Err(CONFLICT);
    }
    let replacing = match region.get(evaluator) {
        None => false,
        Some(existing) => {
            if live(existing.grant.status) || region.frozen(evaluator) {
                return Err(CONFLICT);
            }
            if grant_version <= existing.grant.grant_version {
                return Err(F03_GRANT_VERSION_CONFLICT);
            }
            if key_version <= existing.grant.key_version {
                return Err(F03_KEY_VERSION_CONFLICT);
            }
            true
        }
    };
    if region.key_in_use(key) {
        return Err(F03_KEY_REUSED);
    }
    if !replacing && region.len() >= MAX_EVALUATORS {
        return Err(F03_EVALUATOR_CAPACITY);
    }
    let mut w = Writer::new(suffix);
    w.put(evaluator.as_bytes())?;
    w.u64(grant_version.get())?;
    w.u64(key_version.get())?;
    w.u64(effective_epoch)?;
    w.u64(expiry_epoch_exclusive)?;
    let n = w.len();
    let record = EvaluatorRecord {
        grant,
        last: stamp(replay, &suffix[..n])?,
        rekey: None,
        revocation: None,
    };
    if replacing {
        region.replace(&record)?;
    } else {
        region.insert(&record)?;
    }
    Ok(n)
}

fn rotate(
    region: &mut EvaluatorRegion,
    ctx: &AuthorityContext<'_>,
    replay: &ReplayRequest,
    payload: &[u8],
    suffix: &mut [u8],
) -> CodecResult<usize> {
    let mut r = exact(payload, ROTATE_PAYLOAD_BYTES)?;
    let evaluator = EvaluatorId::new(r.fixed()?)?;
    let key = PublicKey32(r.fixed()?);
    let key_version = Version::new(r.u64()?)?;
    let expected_grant = Version::new(r.u64()?)?;
    let effective_epoch = r.u64()?;
    r.finish()?;
    nonzero_key(key)?;
    let mut record = *region.get(evaluator).ok_or(F03_NO_GRANT)?;
    if expected_grant != record.grant.grant_version {
        return Err(F03_GRANT_VERSION_CONFLICT);
    }
    match record.grant.status {
        GrantStatus::Revoked => return Err(REVOKED),
        GrantStatus::Expired => return Err(EXPIRED),
        GrantStatus::Pending => return Err(WRONG_PHASE),
        GrantStatus::Active => {}
    }
    if effective_epoch != next_activation(ctx, region.snapshot())?
        || effective_epoch >= record.grant.expiry_epoch_exclusive
    {
        return Err(F03_BAD_ACTIVATION);
    }
    let floor = record
        .rekey
        .map_or(record.grant.key_version, |k| k.key_version);
    if key_version <= floor {
        return Err(F03_KEY_VERSION_CONFLICT);
    }
    if region.key_in_use(key) {
        return Err(F03_KEY_REUSED);
    }
    let mut w = Writer::new(suffix);
    w.put(evaluator.as_bytes())?;
    w.u64(key_version.get())?;
    w.u64(effective_epoch)?;
    let n = w.len();
    record.rekey = Some(PendingRekey {
        key,
        key_version,
        effective_epoch,
    });
    record.last = stamp(replay, &suffix[..n])?;
    region.replace(&record)?;
    Ok(n)
}

/// `Ok(None)` is an identical repeated revocation.
fn revoke(
    region: &mut EvaluatorRegion,
    ctx: &AuthorityContext<'_>,
    replay: &ReplayRequest,
    payload: &[u8],
    suffix: &mut [u8],
) -> CodecResult<Option<usize>> {
    let mut r = exact(payload, REVOKE_PAYLOAD_BYTES)?;
    let evaluator = EvaluatorId::new(r.fixed()?)?;
    let grant_version = Version::new(r.u64()?)?;
    let reason = RevocationReason::decode(r.u16()?)?;
    let evidence = Digest32::new(r.fixed()?)?;
    r.finish()?;
    let mut record = *region.get(evaluator).ok_or(F03_NO_GRANT)?;
    if grant_version != record.grant.grant_version {
        return Err(F03_GRANT_VERSION_CONFLICT);
    }
    match (record.grant.status, record.revocation) {
        (GrantStatus::Revoked, Some(prior))
            if prior.reason == reason && prior.evidence == evidence =>
        {
            return Ok(None)
        }
        (GrantStatus::Revoked, _) => return Err(REVOKED),
        (GrantStatus::Expired, _) => return Err(EXPIRED),
        _ => {}
    }
    let excludes_frozen_epoch = region.frozen(evaluator) && !ctx.aggregate_sealed;
    let mut w = Writer::new(suffix);
    w.put(evaluator.as_bytes())?;
    w.u64(grant_version.get())?;
    w.u16(reason as u16)?;
    w.put(evidence.as_bytes())?;
    w.u8(u8::from(excludes_frozen_epoch))?;
    let n = w.len();
    record.grant.status = GrantStatus::Revoked;
    record.rekey = None;
    record.revocation = Some(Revocation {
        reason,
        evidence,
        height: ctx.height,
        excludes_frozen_epoch,
    });
    record.last = stamp(replay, &suffix[..n])?;
    region.replace(&record)?;
    Ok(Some(n))
}

/// Apply one finalized Owner `ScheduleEvaluator`, `RotateEvaluatorKey` or
/// `RevokeEvaluator`. `scratch` holds at least [`SCRATCH_BYTES`]. Refusals leave
/// `state_bytes` authoritative and write no event.
///
/// # Errors
/// Envelope, domain, expiry and replay refusals; `UNKNOWN_OPERATION`,
/// `UNAUTHORIZED` for a non-direct or non-Owner call, `WRONG_PHASE` while
/// admission is suspended (revocation stays allowed), `WRONG_CONFIG`,
/// `WRONG_EPOCH`/`WRONG_ROSTER`, `ROLE_CONFLICT`, `CONFLICT`, `F03_NO_GRANT`,
/// `F03_EVALUATOR_CAPACITY`, `F03_BAD_ACTIVATION`, `F03_KEY_REUSED`,
/// `F03_KEY_VERSION_CONFLICT`, `F03_GRANT_VERSION_CONFLICT`, `REVOKED`,
/// `EXPIRED`, `NON_CANONICAL` and `CAPACITY`.
pub fn apply(
    state_bytes: &[u8],
    ctx: &AuthorityContext<'_>,
    envelope_bytes: &[u8],
    scratch: &mut [u8],
    out: &mut [u8],
    event: &mut [u8],
) -> CodecResult<Outcome> {
    let env = codec::decode_envelope(envelope_bytes)?;
    let e = env.envelope;
    let op = e.operation;
    if op != dispatch::ScheduleEvaluator
        && op != dispatch::RotateEvaluatorKey
        && op != dispatch::RevokeEvaluator
    {
        return Err(UNKNOWN_OPERATION);
    }
    codec::compare_direct_call(ctx.immediate_caller)?;
    e.check_domain(
        ctx.market.deployment_chain_domain,
        ctx.market.program_id,
        ctx.market.market_id,
    )?;
    e.check_expiry(ctx.height)?;
    check_lifecycle(ctx.market.lifecycle, op == dispatch::RevokeEvaluator)?;
    if e.config != ctx.market.active_config_version {
        return Err(WRONG_CONFIG);
    }
    codec::compare_native_principal(&e, ctx.invoking_principal)?;
    if ctx.invoking_principal != ctx.market.owner_principal {
        return Err(UNAUTHORIZED);
    }
    let state = state::decode_shared_state(state_bytes)?;
    let (workers, region_bytes) = split_identity_section(state.section(Section::IdentityRoster)?)?;
    let mut region = EvaluatorRegion::decode(region_bytes)?;
    check_epoch_binding(&e, region.snapshot())?;
    let owner = state
        .control
        .replay
        .actor(ActorSlot::OWNER)
        .ok_or(NOT_FOUND)?;
    let replay = ReplayRequest::from_envelope(ActorSlot::OWNER, owner.authority_version, &env)?;
    if let ReplayDecision::AlreadyApplied(last) = state.control.replay.check(&replay, ctx.height)? {
        return Ok(Outcome::AlreadyApplied(last));
    }
    let mut suffix = [0u8; REVOKE_EVENT_BYTES];
    let n = match op {
        dispatch::ScheduleEvaluator => {
            schedule(&mut region, ctx, workers, &replay, e.payload, &mut suffix)?
        }
        dispatch::RotateEvaluatorKey => rotate(&mut region, ctx, &replay, e.payload, &mut suffix)?,
        _ => match revoke(&mut region, ctx, &replay, e.payload, &mut suffix)? {
            Some(n) => n,
            None => return Ok(Outcome::Idempotent),
        },
    };
    let (section, control) = scratch
        .split_at_mut_checked(Section::IdentityRoster.payload_cap())
        .ok_or(CAPACITY)?;
    let section_len = compose(workers, &region, section)?;
    let mut next = state.replace_section(Section::IdentityRoster, &section[..section_len])?;
    let result = codec::result_digest(&suffix[..n])?;
    next.record_success(&replay, ctx.height, result)?;
    let common = codec::EventCommon {
        market: ctx.market.market_id,
        epoch: e.epoch,
        config: Version::new(e.config)?,
        revision: next.revision,
        request: replay.digest,
        result,
    };
    let event_len = codec::encode_event_frame(op, &common, &suffix[..n], event)?;
    let state_len = state::encode_shared_state(&next, out, control)?;
    Ok(Outcome::Applied {
        state_len,
        event_len,
    })
}

/// Authenticated inputs of one `OpenEpoch` boundary. `admission` is the F08 table
/// after its own rollover to `epoch`; `workers` is the frozen worker roster.
#[derive(Clone, Copy, Debug)]
pub struct SnapshotContext<'a> {
    pub market: &'a MarketHeader,
    pub epoch: u64,
    pub approved_rubric: RubricDigest,
    pub workers: &'a [WorkerRosterEntry],
    pub admission: &'a AdmissionTable,
}

#[derive(Clone, Copy, Debug, Default, Eq, PartialEq)]
pub struct OpenedSnapshot {
    pub members: u8,
    pub activated: u8,
    pub rekeyed: u8,
    pub expired: u8,
    pub pruned: u8,
    pub section_len: usize,
}

fn bump(counter: &mut u8) -> CodecResult<()> {
    *counter = counter.checked_add(1).ok_or(ARITHMETIC)?;
    Ok(())
}

/// Accepted F08 membership for this exact evaluator principal by `epoch`.
fn membership(
    admission: &AdmissionTable,
    grant: &EvaluatorGrant,
    epoch: u64,
) -> Option<AdmissionMeta> {
    admission
        .get(Participant::Evaluator(grant.evaluator))
        .filter(|m| {
            m.admitted()
                && !m.revoked()
                && m.owner == grant.principal
                && m.admitted_epoch.is_some_and(|a| a <= epoch)
        })
}

/// Expiry, activation and staged rekey at the epoch boundary.
fn advance(
    record: &mut EvaluatorRecord,
    ctx: &SnapshotContext<'_>,
    counts: &mut OpenedSnapshot,
) -> CodecResult<()> {
    if ctx.epoch >= record.grant.expiry_epoch_exclusive {
        record.grant.status = GrantStatus::Expired;
        record.rekey = None;
        return bump(&mut counts.expired);
    }
    match record.grant.status {
        GrantStatus::Pending
            if record.grant.effective_epoch <= ctx.epoch
                && membership(ctx.admission, &record.grant, ctx.epoch)
                    .is_some_and(|m| m.delegate_generation == record.grant.key_version.get()) =>
        {
            record.grant.status = GrantStatus::Active;
            bump(&mut counts.activated)
        }
        GrantStatus::Active => match record.rekey {
            Some(rekey) if rekey.effective_epoch <= ctx.epoch => {
                record.grant.signing_key = rekey.key;
                record.grant.key_version = rekey.key_version;
                record.rekey = None;
                bump(&mut counts.rekeyed)
            }
            _ => Ok(()),
        },
        _ => Ok(()),
    }
}

/// F03 part of `OpenEpoch`: drops terminal records, expires at the exclusive
/// expiry, activates pending grants only with F08 accepted membership, applies
/// due rekeys and freezes the eligible snapshot. A role conflict refuses the
/// whole opening; `section` is never modified and `next` must be discarded on error.
///
/// # Errors
/// `WRONG_EPOCH` unless `epoch` advances and F08 has opened it, `ROLE_CONFLICT`
/// for an eligible principal equal to the market owner, a frozen worker owner or
/// another eligible principal, `F03_KEY_REUSED`, `CAPACITY` and decode errors.
pub fn open_epoch(
    section: &[u8],
    ctx: &SnapshotContext<'_>,
    next: &mut [u8],
) -> CodecResult<OpenedSnapshot> {
    let (workers, region_bytes) = split_identity_section(section)?;
    let region = EvaluatorRegion::decode(region_bytes)?;
    if region.snapshot().is_some_and(|s| ctx.epoch <= s.epoch)
        || ctx.admission.current_epoch() != Some(ctx.epoch)
    {
        return Err(WRONG_EPOCH);
    }
    if ctx.workers.len() > MAX_WORKERS {
        return Err(CAPACITY);
    }
    let mut opened = EvaluatorRegion::new();
    let mut snapshot = Snapshot::new(ctx.epoch);
    let mut counts = OpenedSnapshot::default();
    for current in region.records() {
        if !live(current.grant.status) {
            bump(&mut counts.pruned)?;
            continue;
        }
        let mut record = *current;
        advance(&mut record, ctx, &mut counts)?;
        let grant = record.grant;
        if grant.status == GrantStatus::Active
            && grant.rubric == ctx.approved_rubric
            && membership(ctx.admission, &grant, ctx.epoch).is_some()
        {
            if grant.principal == ctx.market.owner_principal
                || ctx.workers.iter().any(|w| w.owner == grant.principal)
                || snapshot.entries().any(|f| f.entry.owner == grant.principal)
            {
                return Err(ROLE_CONFLICT);
            }
            if snapshot
                .entries()
                .any(|f| f.entry.public_key == grant.signing_key)
            {
                return Err(F03_KEY_REUSED);
            }
            snapshot.push(FrozenEvaluator {
                entry: EvaluatorRosterEntry {
                    evaluator: grant.evaluator,
                    owner: grant.principal,
                    grant: grant.grant_version,
                    key_version: grant.key_version,
                    public_key: grant.signing_key,
                    rubric: grant.rubric,
                },
                expiry_epoch_exclusive: grant.expiry_epoch_exclusive,
            })?;
        }
        opened.insert(&record)?;
    }
    counts.members = u8::try_from(snapshot.len()).map_err(|_| ARITHMETIC)?;
    opened.snapshot = Some(snapshot);
    counts.section_len = compose(workers, &opened, next)?;
    Ok(counts)
}
