//! F08 bounded admission: approvals, quotas, cooldowns, heartbeat presence and exits.
//! Membership is permission to participate only; it never carries quality or rewards.
use crate::{
    codec::{self, Reader, Writer},
    errors::{
        CodecResult, ARITHMETIC, BAD_VERSION, CAPACITY, CONFLICT,
        F08_ADMINISTRATOR_APPROVAL_REQUIRED, F08_ADMISSION_WINDOW_FULL, F08_BAD_CONSENT,
        F08_CANDIDATE_CHANGED, F08_CAPACITY_EXCEEDED, F08_DELEGATE_REVOKED, F08_DUPLICATE_IDENTITY,
        F08_IDEMPOTENCY_CONFLICT, F08_MARKET_PAUSED, F08_OWNER_CAPACITY_EXCEEDED,
        F08_OWNER_REQUIRED, F08_PERMIT_CONSUMED, F08_PERMIT_EXPIRED, F08_RATE_LIMITED,
        F08_STALE_STATE, F08_WRONG_GENERATION, NON_CANONICAL, NOT_FOUND, ROLE_CONFLICT,
        WRONG_CONFIG, WRONG_DOMAIN, WRONG_EPOCH, WRONG_PHASE,
    },
    evaluators::{
        codec::verify_digest,
        model::{
            EvaluatorGrant, GrantStatus, VerificationError, CONSENT_BYTES, SIGNED_CONSENT_BYTES,
        },
    },
    registry::{market_clock, MarketHeader},
    state::HeightWindow,
    types::{
        ChainDomain, Digest32, EvaluatorId, MarketId, PrincipalId, ProgramId, PublicKey32,
        RequestId, RubricDigest, Signature64, WorkerId,
    },
    MAX_EVALUATORS, MAX_WORKERS,
};

pub const FLAG_ADMITTED: u8 = 1;
pub const FLAG_DRAINING: u8 = 2;
pub const FLAG_REVOKED: u8 = 4;
pub const MAX_MEMBERS: usize = MAX_WORKERS + MAX_EVALUATORS;
/// The 57 R005 bytes plus one exit-cause byte.
const R005_BYTES: usize = 1 + 9 + 9 + 9 + 8 + 10 + 8 + 1 + 1 + 2;
/// Bindings the crate cannot read back from a stored F02/F03 record: the 32-byte
/// stable ID, the 32-byte owner and the 8-byte frozen key generation (R007).
const BINDING_BYTES: usize = 32 + 32 + 8;
/// Admitted member: R005, bindings, owner admission watermark (R006), absent approval.
pub const ADMITTED_META_MAX_BYTES: usize = R005_BYTES + BINDING_BYTES + 9 + 1;
/// Approval-only record: absent liveness/exit/watermark fields plus the 56-byte approval.
pub const APPROVAL_META_BYTES: usize =
    (1 + 1 + 1 + 1 + 8 + 1 + 8 + 1 + 1 + 2) + BINDING_BYTES + 1 + 1 + 56;
pub const META_MAX_BYTES: usize = if ADMITTED_META_MAX_BYTES > APPROVAL_META_BYTES {
    ADMITTED_META_MAX_BYTES
} else {
    APPROVAL_META_BYTES
};
pub const TABLE_HEADER_MAX_BYTES: usize = 2 + 9 + 1 + 1;
pub const TABLE_MAX_BYTES: usize = TABLE_HEADER_MAX_BYTES + MAX_MEMBERS * META_MAX_BYTES;

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct AdmissionPolicyV1 {
    pub worker_capacity: u8,
    pub evaluator_capacity: u8,
    pub worker_per_owner: u8,
    pub evaluator_per_owner: u8,
    pub successful_enrollments_per_epoch: u8,
    pub minimum_owner_admission_distance: u16,
    pub heartbeat_distance: u16,
    pub missed_epoch_threshold: u8,
    pub new_member_immunity_epochs: u8,
    pub score_quorum: u8,
}
/// Immutable within v1; never supplied by a caller.
pub const POLICY_V1: AdmissionPolicyV1 = AdmissionPolicyV1 {
    worker_capacity: 32,
    evaluator_capacity: 8,
    worker_per_owner: 2,
    evaluator_per_owner: 1,
    successful_enrollments_per_epoch: 4,
    minimum_owner_admission_distance: 16,
    heartbeat_distance: 16,
    missed_epoch_threshold: 2,
    new_member_immunity_epochs: 1,
    score_quorum: 3,
};

#[derive(Clone, Copy, Debug, Eq, PartialEq, Ord, PartialOrd)]
#[repr(u8)]
pub enum Role {
    Worker = 1,
    Evaluator = 2,
}
impl Role {
    /// # Errors
    /// `NON_CANONICAL` for any value other than WORKER=1 or EVALUATOR=2.
    pub fn decode(value: u8) -> CodecResult<Self> {
        match value {
            1 => Ok(Self::Worker),
            2 => Ok(Self::Evaluator),
            _ => Err(NON_CANONICAL),
        }
    }
    const fn capacity(self) -> usize {
        match self {
            Self::Worker => POLICY_V1.worker_capacity as usize,
            Self::Evaluator => POLICY_V1.evaluator_capacity as usize,
        }
    }
    const fn per_owner(self) -> usize {
        match self {
            Self::Worker => POLICY_V1.worker_per_owner as usize,
            Self::Evaluator => POLICY_V1.evaluator_per_owner as usize,
        }
    }
}

/// Stable role identity; ordering is role first, then unsigned ID bytes.
#[derive(Clone, Copy, Debug, Eq, PartialEq, Ord, PartialOrd)]
pub enum Participant {
    Worker(WorkerId),
    Evaluator(EvaluatorId),
}
impl Participant {
    #[must_use]
    pub const fn role(self) -> Role {
        match self {
            Self::Worker(_) => Role::Worker,
            Self::Evaluator(_) => Role::Evaluator,
        }
    }
    #[must_use]
    pub const fn bytes(self) -> [u8; 32] {
        match self {
            Self::Worker(id) => id.bytes(),
            Self::Evaluator(id) => id.bytes(),
        }
    }
    fn write(self, w: &mut Writer<'_>) -> CodecResult<()> {
        w.u8(self.role() as u8)?;
        w.put(&self.bytes())
    }
    fn read(r: &mut Reader<'_>) -> CodecResult<Self> {
        Ok(match Role::decode(r.u8()?)? {
            Role::Worker => Self::Worker(WorkerId::new(r.fixed()?)?),
            Role::Evaluator => Self::Evaluator(EvaluatorId::new(r.fixed()?)?),
        })
    }
}

/// `RequestExit` reason payload values.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum ExitReason {
    Voluntary = 1,
    Retire = 2,
}
impl ExitReason {
    /// # Errors
    /// `NON_CANONICAL` for any value other than VOLUNTARY=1 or RETIRE=2.
    pub fn decode(value: u8) -> CodecResult<Self> {
        match value {
            1 => Ok(Self::Voluntary),
            2 => Ok(Self::Retire),
            _ => Err(NON_CANONICAL),
        }
    }
}

/// `AdministrativeRemove` reason payload values.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum RemovalReason {
    Security = 1,
    Terms = 2,
    OperatorDecision = 3,
}
impl RemovalReason {
    /// # Errors
    /// `NON_CANONICAL` for any value outside `SECURITY=1..OPERATOR_DECISION=3`.
    pub fn decode(value: u8) -> CodecResult<Self> {
        match value {
            1 => Ok(Self::Security),
            2 => Ok(Self::Terms),
            3 => Ok(Self::OperatorDecision),
            _ => Err(NON_CANONICAL),
        }
    }
}

/// Who staged a pending exit; only owner-requested exits can be cancelled.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum ExitCause {
    Voluntary = 1,
    Retire = 2,
    Inactivity = 3,
    Security = 4,
    Terms = 5,
    OperatorDecision = 6,
}
impl ExitCause {
    fn decode(value: u8) -> CodecResult<Self> {
        Ok(match value {
            1 => Self::Voluntary,
            2 => Self::Retire,
            3 => Self::Inactivity,
            4 => Self::Security,
            5 => Self::Terms,
            6 => Self::OperatorDecision,
            _ => return Err(NON_CANONICAL),
        })
    }
    const fn owner_requested(self) -> bool {
        matches!(self, Self::Voluntary | Self::Retire)
    }
}
impl From<ExitReason> for ExitCause {
    fn from(reason: ExitReason) -> Self {
        match reason {
            ExitReason::Voluntary => Self::Voluntary,
            ExitReason::Retire => Self::Retire,
        }
    }
}
impl From<RemovalReason> for ExitCause {
    fn from(reason: RemovalReason) -> Self {
        match reason {
            RemovalReason::Security => Self::Security,
            RemovalReason::Terms => Self::Terms,
            RemovalReason::OperatorDecision => Self::OperatorDecision,
        }
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct PendingExit {
    pub epoch: u64,
    pub cause: ExitCause,
}

/// One-use administrator approval: exactly 56 bytes inside the member record.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct Approval {
    pub digest: Digest32,
    pub expiry_height: u64,
    pub effective_epoch: u64,
    pub config_version: u64,
}

/// Bound fields hashed into an approval digest.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct ApprovalTerms {
    pub participant: Participant,
    pub owner: PrincipalId,
    pub enrollment_nonce_commitment: Digest32,
    pub delegate: PublicKey32,
    pub delegate_generation: u64,
    pub identity_commitment: Digest32,
    pub effective_epoch: u64,
    pub config_version: u64,
    pub request: RequestId,
    pub expiry_height: u64,
}
impl ApprovalTerms {
    /// # Errors
    /// Propagates codec and host hash failures.
    pub fn digest(&self, market: &MarketHeader) -> CodecResult<Digest32> {
        let mut buf = [0u8; 361];
        let mut w = Writer::new(&mut buf);
        w.put(market.deployment_chain_domain.as_bytes())?;
        w.put(market.program_id.as_bytes())?;
        w.put(market.market_id.as_bytes())?;
        self.participant.write(&mut w)?;
        w.put(self.owner.as_bytes())?;
        w.put(self.enrollment_nonce_commitment.as_bytes())?;
        w.put(&self.delegate.0)?;
        w.u64(self.delegate_generation)?;
        w.put(self.identity_commitment.as_bytes())?;
        w.u64(self.effective_epoch)?;
        w.u64(self.config_version)?;
        w.put(self.request.as_bytes())?;
        w.u64(self.expiry_height)?;
        let n = w.len();
        codec::domain_hash("PAXAI/admission-approval/v1", &buf[..n])
    }
}

/// `AdmissionMeta` (F08-R005) plus the bound owner, frozen key generation, the owner
/// admission watermark and the optional unconsumed approval. While a member is
/// admitted but not yet included, `immunity_until_epoch` holds its scheduled epoch.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct AdmissionMeta {
    pub participant: Participant,
    pub owner: PrincipalId,
    pub admitted_epoch: Option<u64>,
    pub last_heartbeat_epoch: Option<u64>,
    pub last_heartbeat_height: Option<u64>,
    pub immunity_until_epoch: u64,
    pub pending_exit: Option<PendingExit>,
    pub membership_generation: u64,
    pub complete_missed_opened_epochs: u8,
    pub membership_flags: u8,
    pub delegate_generation: u64,
    pub admission_height: Option<u64>,
    pub approval: Option<Approval>,
}
impl AdmissionMeta {
    #[must_use]
    pub const fn admitted(&self) -> bool {
        self.membership_flags & FLAG_ADMITTED != 0
    }
    #[must_use]
    pub const fn draining(&self) -> bool {
        self.membership_flags & FLAG_DRAINING != 0
    }
    #[must_use]
    pub const fn revoked(&self) -> bool {
        self.membership_flags & FLAG_REVOKED != 0
    }
    /// A(x): last accepted heartbeat epoch, else the actual admission epoch.
    #[must_use]
    pub const fn last_activity(&self) -> Option<u64> {
        match self.last_heartbeat_epoch {
            Some(epoch) => Some(epoch),
            None => self.admitted_epoch,
        }
    }
    fn validate(&self) -> CodecResult<()> {
        let flags_valid = self.membership_flags & !(FLAG_ADMITTED | FLAG_DRAINING | FLAG_REVOKED)
            == 0
            && (self.admitted() || self.membership_flags == 0);
        let shape_valid = if self.admitted() {
            self.approval.is_none() && self.admission_height.is_some()
        } else {
            self.approval.is_some()
                && self.admitted_epoch.is_none()
                && self.last_heartbeat_epoch.is_none()
                && self.pending_exit.is_none()
                && self.admission_height.is_none()
        };
        if !flags_valid
            || !shape_valid
            || self.membership_generation == 0
            || self.complete_missed_opened_epochs > POLICY_V1.missed_epoch_threshold
            || (self.last_heartbeat_height.is_some() && self.last_heartbeat_epoch.is_none())
            || (self.last_heartbeat_epoch.is_some() && self.admitted_epoch.is_none())
        {
            return Err(NON_CANONICAL);
        }
        Ok(())
    }
    fn write(&self, w: &mut Writer<'_>) -> CodecResult<()> {
        fn optional(w: &mut Writer<'_>, value: Option<u64>) -> CodecResult<()> {
            w.boolean(value.is_some())?;
            value.map_or(Ok(()), |v| w.u64(v))
        }
        self.participant.write(w)?;
        optional(w, self.admitted_epoch)?;
        optional(w, self.last_heartbeat_epoch)?;
        optional(w, self.last_heartbeat_height)?;
        w.u64(self.immunity_until_epoch)?;
        w.boolean(self.pending_exit.is_some())?;
        if let Some(exit) = self.pending_exit {
            w.u64(exit.epoch)?;
            w.u8(exit.cause as u8)?;
        }
        w.u64(self.membership_generation)?;
        w.u8(self.complete_missed_opened_epochs)?;
        w.u8(self.membership_flags)?;
        w.u16(0)?;
        w.put(self.owner.as_bytes())?;
        w.u64(self.delegate_generation)?;
        optional(w, self.admission_height)?;
        w.boolean(self.approval.is_some())?;
        if let Some(a) = self.approval {
            w.put(a.digest.as_bytes())?;
            w.u64(a.expiry_height)?;
            w.u64(a.effective_epoch)?;
            w.u64(a.config_version)?;
        }
        Ok(())
    }
    fn read(r: &mut Reader<'_>) -> CodecResult<Self> {
        fn optional(r: &mut Reader<'_>) -> CodecResult<Option<u64>> {
            Ok(if r.boolean()? { Some(r.u64()?) } else { None })
        }
        let participant = Participant::read(r)?;
        let admitted_epoch = optional(r)?;
        let last_heartbeat_epoch = optional(r)?;
        let last_heartbeat_height = optional(r)?;
        let immunity_until_epoch = r.u64()?;
        let pending_exit = if r.boolean()? {
            Some(PendingExit {
                epoch: r.u64()?,
                cause: ExitCause::decode(r.u8()?)?,
            })
        } else {
            None
        };
        let membership_generation = r.u64()?;
        let complete_missed_opened_epochs = r.u8()?;
        let membership_flags = r.u8()?;
        r.reserved(2)?;
        let owner = PrincipalId::new(r.fixed()?)?;
        let delegate_generation = r.u64()?;
        let admission_height = optional(r)?;
        let approval = if r.boolean()? {
            Some(Approval {
                digest: Digest32::new(r.fixed()?)?,
                expiry_height: r.u64()?,
                effective_epoch: r.u64()?,
                config_version: r.u64()?,
            })
        } else {
            None
        };
        let value = Self {
            participant,
            owner,
            admitted_epoch,
            last_heartbeat_epoch,
            last_heartbeat_height,
            immunity_until_epoch,
            pending_exit,
            membership_generation,
            complete_missed_opened_epochs,
            membership_flags,
            delegate_generation,
            admission_height,
            approval,
        };
        value.validate()?;
        Ok(value)
    }
    /// # Errors
    /// `NON_CANONICAL` for an invalid record; codec errors when `out` is too small.
    pub fn encode(&self, out: &mut [u8]) -> CodecResult<usize> {
        self.validate()?;
        let mut w = Writer::new(out);
        self.write(&mut w)?;
        Ok(w.len())
    }
}

/// Authenticated host values for one direct F08 call.
#[derive(Clone, Copy, Debug)]
pub struct AdmissionContext<'m> {
    pub market: &'m MarketHeader,
    pub invoking_principal: PrincipalId,
    pub height: u64,
}
impl AdmissionContext<'_> {
    fn require_market_owner(&self) -> CodecResult<()> {
        if self.invoking_principal == self.market.owner_principal {
            Ok(())
        } else {
            Err(F08_ADMINISTRATOR_APPROVAL_REQUIRED)
        }
    }
    fn require_live(&self) -> CodecResult<()> {
        match self.market.lifecycle {
            1 | 2 => Ok(()),
            3 => Err(F08_MARKET_PAUSED),
            _ => Err(WRONG_PHASE),
        }
    }
    /// Authenticated clock epoch; a height before origin cannot name an epoch.
    fn clock_epoch(&self) -> CodecResult<u64> {
        if self.height < self.market.origin_height {
            return Err(WRONG_EPOCH);
        }
        Ok(market_clock(self.market.origin_height, self.height)?.epoch)
    }
}

/// Owner acceptance of a stored approval.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct Admission {
    pub participant: Participant,
    pub delegate_generation: u64,
    pub effective_epoch: u64,
    pub config_version: u64,
    pub approval_digest: Digest32,
}

/// Bounded F08 table in the shared F07/F08 section, sorted by participant.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct AdmissionTable {
    current_epoch: Option<u64>,
    enrollments_this_epoch: u8,
    entries: [Option<AdmissionMeta>; MAX_MEMBERS],
    count: usize,
}
impl Default for AdmissionTable {
    fn default() -> Self {
        Self::new()
    }
}
impl AdmissionTable {
    #[must_use]
    pub const fn new() -> Self {
        Self {
            current_epoch: None,
            enrollments_this_epoch: 0,
            entries: [None; MAX_MEMBERS],
            count: 0,
        }
    }
    /// The last actually opened epoch; `None` until the first `OpenEpoch`.
    #[must_use]
    pub const fn current_epoch(&self) -> Option<u64> {
        self.current_epoch
    }
    #[must_use]
    pub const fn enrollments_this_epoch(&self) -> u8 {
        self.enrollments_this_epoch
    }
    #[must_use]
    pub const fn len(&self) -> usize {
        self.count
    }
    #[must_use]
    pub const fn is_empty(&self) -> bool {
        self.count == 0
    }
    pub fn iter(&self) -> impl Iterator<Item = &AdmissionMeta> {
        self.entries[..self.count].iter().flatten()
    }
    #[must_use]
    pub fn get(&self, participant: Participant) -> Option<AdmissionMeta> {
        self.iter().find(|m| m.participant == participant).copied()
    }
    #[must_use]
    pub fn role_count(&self, role: Role) -> usize {
        self.iter().filter(|m| m.participant.role() == role).count()
    }
    fn member(&self, participant: Participant) -> CodecResult<AdmissionMeta> {
        self.get(participant)
            .filter(AdmissionMeta::admitted)
            .ok_or(NOT_FOUND)
    }
    fn owner_count(&self, role: Role, owner: PrincipalId) -> usize {
        self.iter()
            .filter(|m| m.participant.role() == role && m.owner == owner && m.admitted())
            .count()
    }
    fn owner_watermark(&self, owner: PrincipalId) -> Option<u64> {
        self.iter()
            .filter(|m| m.owner == owner)
            .filter_map(|m| m.admission_height)
            .max()
    }
    fn position(&self, participant: Participant) -> CodecResult<usize> {
        self.entries[..self.count]
            .iter()
            .position(|m| m.is_some_and(|m| m.participant == participant))
            .ok_or(NOT_FOUND)
    }
    /// # Errors
    /// `NON_CANONICAL` for an invalid record, `F08_DUPLICATE_IDENTITY` for an existing
    /// participant and `F08_CAPACITY_EXCEEDED` when the role capacity is full.
    pub fn insert(&mut self, meta: AdmissionMeta) -> CodecResult<()> {
        meta.validate()?;
        if self.iter().any(|m| m.participant == meta.participant) {
            return Err(F08_DUPLICATE_IDENTITY);
        }
        let role = meta.participant.role();
        if self.count >= MAX_MEMBERS || self.role_count(role) >= role.capacity() {
            return Err(F08_CAPACITY_EXCEEDED);
        }
        let at = self.entries[..self.count]
            .iter()
            .position(|m| m.is_some_and(|m| m.participant > meta.participant))
            .unwrap_or(self.count);
        self.entries.copy_within(at..self.count, at + 1);
        self.entries[at] = Some(meta);
        self.count += 1;
        Ok(())
    }
    /// # Errors
    /// `NON_CANONICAL` for an invalid record and `NOT_FOUND` for an unknown participant.
    pub fn replace(&mut self, meta: AdmissionMeta) -> CodecResult<()> {
        meta.validate()?;
        let at = self.position(meta.participant)?;
        self.entries[at] = Some(meta);
        Ok(())
    }
    pub(crate) fn remove(&mut self, participant: Participant) -> CodecResult<AdmissionMeta> {
        let at = self.position(participant)?;
        let meta = self.entries[at].ok_or(NOT_FOUND)?;
        self.entries.copy_within(at + 1..self.count, at);
        self.count -= 1;
        self.entries[self.count] = None;
        Ok(meta)
    }
    pub(crate) fn begin_epoch(&mut self, epoch: u64) {
        self.current_epoch = Some(epoch);
        self.enrollments_this_epoch = 0;
    }
    /// # Errors
    /// Codec errors when `out` is too small or a record is invalid.
    pub fn encode(&self, out: &mut [u8]) -> CodecResult<usize> {
        let mut w = Writer::new(out);
        w.u16(crate::SCHEMA_VERSION)?;
        w.boolean(self.current_epoch.is_some())?;
        if let Some(epoch) = self.current_epoch {
            w.u64(epoch)?;
        }
        w.u8(self.enrollments_this_epoch)?;
        w.u8(u8::try_from(self.count).map_err(|_| ARITHMETIC)?)?;
        for m in self.iter() {
            m.validate()?;
            m.write(&mut w)?;
        }
        Ok(w.len())
    }
    /// An empty section is the initialized market: no epoch opened, no members.
    ///
    /// # Errors
    /// `BAD_VERSION`, `CAPACITY`, `NON_CANONICAL` for unsorted, duplicate, trailing or
    /// invalid bytes, and `F08_CAPACITY_EXCEEDED` above role capacity.
    pub fn decode(input: &[u8]) -> CodecResult<Self> {
        let mut table = Self::new();
        if input.is_empty() {
            return Ok(table);
        }
        if input.len() > TABLE_MAX_BYTES {
            return Err(CAPACITY);
        }
        let mut r = Reader::new(input);
        if r.u16()? != crate::SCHEMA_VERSION {
            return Err(BAD_VERSION);
        }
        table.current_epoch = if r.boolean()? { Some(r.u64()?) } else { None };
        table.enrollments_this_epoch = r.u8()?;
        let count = usize::from(r.u8()?);
        if count > MAX_MEMBERS
            || table.enrollments_this_epoch > POLICY_V1.successful_enrollments_per_epoch
        {
            return Err(NON_CANONICAL);
        }
        let mut previous: Option<Participant> = None;
        for _ in 0..count {
            let meta = AdmissionMeta::read(&mut r)?;
            if previous.is_some_and(|p| p >= meta.participant) {
                return Err(NON_CANONICAL);
            }
            previous = Some(meta.participant);
            table.insert(meta)?;
        }
        r.finish()?;
        Ok(table)
    }

    /// Bootstrap rule: 0 before any opening, else `clock_epoch + 1` (never backdated).
    ///
    /// # Errors
    /// `WRONG_EPOCH` before origin and `ARITHMETIC` on epoch overflow.
    pub fn required_effective_epoch(&self, ctx: &AdmissionContext<'_>) -> CodecResult<u64> {
        if self.current_epoch.is_none() {
            return Ok(0);
        }
        ctx.clock_epoch()?.checked_add(1).ok_or(ARITHMETIC)
    }

    /// Market-owner kind0 approval for an existing current identity record
    /// (F02 ENROLLED worker or F03 PENDING grant). Reapproval replaces the old one.
    ///
    /// # Errors
    /// `F08_MARKET_PAUSED`/`WRONG_PHASE`, `F08_ADMINISTRATOR_APPROVAL_REQUIRED`, `WRONG_CONFIG`,
    /// `WRONG_EPOCH`,
    /// `F08_PERMIT_EXPIRED`, `F08_DUPLICATE_IDENTITY`, `CONFLICT` for a changed owner and
    /// `F08_CAPACITY_EXCEEDED`.
    pub fn approve(
        &mut self,
        ctx: &AdmissionContext<'_>,
        terms: &ApprovalTerms,
    ) -> CodecResult<Digest32> {
        ctx.require_live()?;
        ctx.require_market_owner()?;
        if terms.config_version != ctx.market.active_config_version {
            return Err(WRONG_CONFIG);
        }
        if terms.effective_epoch != self.required_effective_epoch(ctx)? {
            return Err(WRONG_EPOCH);
        }
        let work_end =
            HeightWindow::epoch(ctx.market.origin_height, terms.effective_epoch, 0, 64)?.end;
        if terms.expiry_height <= ctx.height || terms.expiry_height > work_end {
            return Err(F08_PERMIT_EXPIRED);
        }
        let digest = terms.digest(ctx.market)?;
        let approval = Approval {
            digest,
            expiry_height: terms.expiry_height,
            effective_epoch: terms.effective_epoch,
            config_version: terms.config_version,
        };
        match self.get(terms.participant) {
            Some(existing) if existing.admitted() => Err(F08_DUPLICATE_IDENTITY),
            Some(existing) if existing.owner != terms.owner => Err(CONFLICT),
            Some(existing) => self.replace(AdmissionMeta {
                approval: Some(approval),
                delegate_generation: terms.delegate_generation,
                ..existing
            }),
            None => self.insert(AdmissionMeta {
                participant: terms.participant,
                owner: terms.owner,
                admitted_epoch: None,
                last_heartbeat_epoch: None,
                last_heartbeat_height: None,
                immunity_until_epoch: terms.effective_epoch,
                pending_exit: None,
                membership_generation: 1,
                complete_missed_opened_epochs: 0,
                membership_flags: 0,
                delegate_generation: terms.delegate_generation,
                admission_height: None,
                approval: Some(approval),
            }),
        }?;
        Ok(digest)
    }

    /// Clears only an unconsumed approval; never removes a live member.
    ///
    /// # Errors
    /// `F08_ADMINISTRATOR_APPROVAL_REQUIRED`, `NOT_FOUND` and `F08_PERMIT_CONSUMED` for an
    /// admitted member.
    pub fn revoke_approval(
        &mut self,
        ctx: &AdmissionContext<'_>,
        participant: Participant,
    ) -> CodecResult<()> {
        ctx.require_market_owner()?;
        if self.get(participant).ok_or(NOT_FOUND)?.admitted() {
            return Err(F08_PERMIT_CONSUMED);
        }
        self.remove(participant).map(|_| ())
    }

    /// Owner acceptance consuming the stored approval. Delegate consent bound to
    /// `approval_digest` is verified by the caller (`admit_evaluator` for evaluators,
    /// F02 `AcceptEnrollment` for workers).
    ///
    /// # Errors
    /// `F08_MARKET_PAUSED`/`WRONG_PHASE`, `F08_ADMINISTRATOR_APPROVAL_REQUIRED`,
    /// `F08_OWNER_REQUIRED`, `F08_PERMIT_CONSUMED`, `F08_BAD_CONSENT`,
    /// `F08_WRONG_GENERATION`, `WRONG_CONFIG`, `F08_PERMIT_EXPIRED`, `WRONG_EPOCH`,
    /// `F08_OWNER_CAPACITY_EXCEEDED`, `ROLE_CONFLICT`, `F08_ADMISSION_WINDOW_FULL`,
    /// `F08_RATE_LIMITED` and `ARITHMETIC`.
    pub fn admit(
        &mut self,
        ctx: &AdmissionContext<'_>,
        request: &Admission,
    ) -> CodecResult<AdmissionMeta> {
        ctx.require_live()?;
        let meta = self
            .get(request.participant)
            .ok_or(F08_ADMINISTRATOR_APPROVAL_REQUIRED)?;
        if ctx.invoking_principal != meta.owner {
            return Err(F08_OWNER_REQUIRED);
        }
        let approval = meta.approval.ok_or(F08_PERMIT_CONSUMED)?;
        if approval.digest != request.approval_digest {
            return Err(F08_BAD_CONSENT);
        }
        if request.delegate_generation != meta.delegate_generation {
            return Err(F08_WRONG_GENERATION);
        }
        if request.config_version != approval.config_version
            || request.config_version != ctx.market.active_config_version
        {
            return Err(WRONG_CONFIG);
        }
        if ctx.height >= approval.expiry_height {
            return Err(F08_PERMIT_EXPIRED);
        }
        let required = self.required_effective_epoch(ctx)?;
        if request.effective_epoch != required || approval.effective_epoch != required {
            return Err(WRONG_EPOCH);
        }
        let role = request.participant.role();
        if self.owner_count(role, meta.owner) >= role.per_owner() {
            return Err(F08_OWNER_CAPACITY_EXCEEDED);
        }
        let conflicting_role = match role {
            Role::Worker => Role::Evaluator,
            Role::Evaluator => Role::Worker,
        };
        if (role == Role::Evaluator && meta.owner == ctx.market.owner_principal)
            || self.owner_count(conflicting_role, meta.owner) > 0
        {
            return Err(ROLE_CONFLICT);
        }
        if self.enrollments_this_epoch >= POLICY_V1.successful_enrollments_per_epoch {
            return Err(F08_ADMISSION_WINDOW_FULL);
        }
        if let Some(last) = self.owner_watermark(meta.owner) {
            let distance = ctx.height.checked_sub(last).ok_or(ARITHMETIC)?;
            if distance < u64::from(POLICY_V1.minimum_owner_admission_distance) {
                return Err(F08_RATE_LIMITED);
            }
        }
        let enrollments = self
            .enrollments_this_epoch
            .checked_add(1)
            .ok_or(ARITHMETIC)?;
        let admitted = AdmissionMeta {
            approval: None,
            membership_flags: FLAG_ADMITTED,
            immunity_until_epoch: request.effective_epoch,
            admission_height: Some(ctx.height),
            ..meta
        };
        self.replace(admitted)?;
        self.enrollments_this_epoch = enrollments;
        Ok(admitted)
    }

    fn opened_epoch(&self) -> CodecResult<u64> {
        self.current_epoch.ok_or(WRONG_EPOCH)
    }
    fn next_epoch(&self) -> CodecResult<u64> {
        self.opened_epoch()?.checked_add(1).ok_or(ARITHMETIC)
    }

    /// Delegate-authenticated key activity in the current opened epoch. It never
    /// changes quality, score evidence, job completion or rewards.
    ///
    /// # Errors
    /// `WRONG_EPOCH`, `NOT_FOUND` for a non-included participant, `F08_DELEGATE_REVOKED`,
    /// `F08_WRONG_GENERATION`, `F08_RATE_LIMITED` and `ARITHMETIC`.
    pub fn heartbeat(
        &mut self,
        ctx: &AdmissionContext<'_>,
        participant: Participant,
        membership_generation: u64,
        delegate_generation: u64,
        epoch: u64,
    ) -> CodecResult<AdmissionMeta> {
        let current = self.opened_epoch()?;
        let meta = self.member(participant)?;
        if meta.admitted_epoch.is_none() {
            return Err(NOT_FOUND);
        }
        if meta.revoked() {
            return Err(F08_DELEGATE_REVOKED);
        }
        if membership_generation != meta.membership_generation
            || delegate_generation != meta.delegate_generation
        {
            return Err(F08_WRONG_GENERATION);
        }
        if epoch != current || ctx.clock_epoch()? != current {
            return Err(WRONG_EPOCH);
        }
        if let Some(last) = meta.last_heartbeat_height {
            let distance = ctx.height.checked_sub(last).ok_or(ARITHMETIC)?;
            if distance < u64::from(POLICY_V1.heartbeat_distance) {
                return Err(F08_RATE_LIMITED);
            }
        }
        let active = AdmissionMeta {
            last_heartbeat_epoch: Some(current),
            last_heartbeat_height: Some(ctx.height),
            complete_missed_opened_epochs: 0,
            ..meta
        };
        self.replace(active)?;
        Ok(active)
    }

    /// Owner-only exit effective at the next opened snapshot; draining is immediate.
    ///
    /// # Errors
    /// `NOT_FOUND`, `F08_OWNER_REQUIRED`, `F08_WRONG_GENERATION`, `WRONG_EPOCH`,
    /// `ARITHMETIC`, `F08_IDEMPOTENCY_CONFLICT` for a different owner exit and
    /// `WRONG_PHASE` when an inactivity or administrative exit is already staged.
    pub fn request_exit(
        &mut self,
        ctx: &AdmissionContext<'_>,
        participant: Participant,
        membership_generation: u64,
        expected_effective_epoch: u64,
        reason: ExitReason,
    ) -> CodecResult<AdmissionMeta> {
        let meta = self.member(participant)?;
        if ctx.invoking_principal != meta.owner {
            return Err(F08_OWNER_REQUIRED);
        }
        if membership_generation != meta.membership_generation {
            return Err(F08_WRONG_GENERATION);
        }
        let exit = PendingExit {
            epoch: self.next_epoch()?,
            cause: reason.into(),
        };
        if expected_effective_epoch != exit.epoch {
            return Err(WRONG_EPOCH);
        }
        match meta.pending_exit {
            Some(staged) if staged == exit => Ok(meta),
            Some(staged) if staged.cause.owner_requested() => Err(F08_IDEMPOTENCY_CONFLICT),
            Some(_) => Err(WRONG_PHASE),
            None => {
                let draining = AdmissionMeta {
                    pending_exit: Some(exit),
                    membership_flags: meta.membership_flags | FLAG_DRAINING,
                    ..meta
                };
                self.replace(draining)?;
                Ok(draining)
            }
        }
    }

    /// Owner-only cancellation of an owner-requested exit before removal. Draining
    /// stays until the following snapshot restores availability; admission, immunity
    /// and heartbeat history are untouched.
    ///
    /// # Errors
    /// `NOT_FOUND`, `F08_OWNER_REQUIRED`, `F08_WRONG_GENERATION` and `WRONG_PHASE` when
    /// no owner-requested exit is pending.
    pub fn cancel_exit(
        &mut self,
        ctx: &AdmissionContext<'_>,
        participant: Participant,
        membership_generation: u64,
    ) -> CodecResult<AdmissionMeta> {
        let meta = self.member(participant)?;
        if ctx.invoking_principal != meta.owner {
            return Err(F08_OWNER_REQUIRED);
        }
        if membership_generation != meta.membership_generation {
            return Err(F08_WRONG_GENERATION);
        }
        if !meta
            .pending_exit
            .is_some_and(|exit| exit.cause.owner_requested())
        {
            return Err(WRONG_PHASE);
        }
        let restored = AdmissionMeta {
            pending_exit: None,
            ..meta
        };
        self.replace(restored)?;
        Ok(restored)
    }

    /// Visibly discretionary administrator removal at the next snapshot; SECURITY also
    /// revokes immediately, including during immunity.
    ///
    /// # Errors
    /// `F08_ADMINISTRATOR_APPROVAL_REQUIRED`, `NOT_FOUND`, `F08_WRONG_GENERATION`,
    /// `WRONG_EPOCH` and `ARITHMETIC`.
    pub fn administrative_remove(
        &mut self,
        ctx: &AdmissionContext<'_>,
        participant: Participant,
        membership_generation: u64,
        reason: RemovalReason,
    ) -> CodecResult<AdmissionMeta> {
        ctx.require_market_owner()?;
        let meta = self.member(participant)?;
        if membership_generation != meta.membership_generation {
            return Err(F08_WRONG_GENERATION);
        }
        let mut flags = meta.membership_flags | FLAG_DRAINING;
        if reason == RemovalReason::Security {
            flags |= FLAG_REVOKED;
        }
        let removed = AdmissionMeta {
            pending_exit: Some(PendingExit {
                epoch: self.next_epoch()?,
                cause: reason.into(),
            }),
            membership_flags: flags,
            ..meta
        };
        self.replace(removed)?;
        Ok(removed)
    }

    /// Permissionless deterministic inactivity prune (F08-R013/R016).
    ///
    /// # Errors
    /// `F08_STALE_STATE`, `WRONG_EPOCH`, `F08_NO_PRUNABLE_MEMBER`,
    /// `F08_CANDIDATE_CHANGED` and `ARITHMETIC`.
    pub fn prune_inactive(
        &mut self,
        expected_candidate: Participant,
        expected_revision: u64,
        state_revision: u64,
    ) -> CodecResult<AdmissionMeta> {
        if expected_revision != state_revision {
            return Err(F08_STALE_STATE);
        }
        let epoch = self.opened_epoch()?;
        let candidate = crate::roster::prune_candidate(self, expected_candidate.role(), epoch)?;
        if candidate.participant != expected_candidate {
            return Err(F08_CANDIDATE_CHANGED);
        }
        let pruned = AdmissionMeta {
            pending_exit: Some(PendingExit {
                epoch: self.next_epoch()?,
                cause: ExitCause::Inactivity,
            }),
            membership_flags: candidate.membership_flags | FLAG_DRAINING,
            ..candidate
        };
        self.replace(pruned)?;
        Ok(pruned)
    }
}

/// `EvaluatorAdmissionConsentV1`: exactly 362 canonical bytes.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct EvaluatorConsent {
    pub chain: ChainDomain,
    pub program: ProgramId,
    pub market: MarketId,
    pub evaluator: EvaluatorId,
    pub owner: PrincipalId,
    pub signing_key: PublicKey32,
    pub enrollment_nonce: [u8; 32],
    pub rubric: RubricDigest,
    pub approval_digest: Digest32,
    pub request: RequestId,
    pub grant_version: u64,
    pub key_version: u64,
    pub effective_epoch: u64,
    pub config_version: u64,
    pub expiry_height: u64,
}
impl EvaluatorConsent {
    /// # Errors
    /// Codec errors only; the layout always fills exactly 362 bytes.
    pub fn encode(&self, out: &mut [u8; CONSENT_BYTES]) -> CodecResult<()> {
        let mut w = Writer::new(out);
        w.u16(1)?;
        w.put(self.chain.as_bytes())?;
        w.put(self.program.as_bytes())?;
        w.put(self.market.as_bytes())?;
        w.put(self.evaluator.as_bytes())?;
        w.put(self.owner.as_bytes())?;
        w.put(&self.signing_key.0)?;
        w.put(&self.enrollment_nonce)?;
        w.put(self.rubric.as_bytes())?;
        w.put(self.approval_digest.as_bytes())?;
        w.put(self.request.as_bytes())?;
        for value in [
            self.grant_version,
            self.key_version,
            self.effective_epoch,
            self.config_version,
            self.expiry_height,
        ] {
            w.u64(value)?;
        }
        Ok(())
    }
    /// # Errors
    /// `NON_CANONICAL` for a wrong length or zero identifier and `BAD_VERSION`.
    pub fn decode(input: &[u8]) -> CodecResult<Self> {
        if input.len() != CONSENT_BYTES {
            return Err(NON_CANONICAL);
        }
        let mut r = Reader::new(input);
        if r.u16()? != 1 {
            return Err(BAD_VERSION);
        }
        let value = Self {
            chain: ChainDomain::new(r.fixed()?)?,
            program: ProgramId::new(r.fixed()?)?,
            market: MarketId::new(r.fixed()?)?,
            evaluator: EvaluatorId::new(r.fixed()?)?,
            owner: PrincipalId::new(r.fixed()?)?,
            signing_key: PublicKey32(r.fixed()?),
            enrollment_nonce: r.fixed()?,
            rubric: RubricDigest::new(r.fixed()?)?,
            approval_digest: Digest32::new(r.fixed()?)?,
            request: RequestId::new(r.fixed()?)?,
            grant_version: r.u64()?,
            key_version: r.u64()?,
            effective_epoch: r.u64()?,
            config_version: r.u64()?,
            expiry_height: r.u64()?,
        };
        r.finish()?;
        Ok(value)
    }
    /// # Errors
    /// Propagates codec and host hash failures.
    pub fn digest(&self) -> CodecResult<Digest32> {
        let mut buf = [0u8; CONSENT_BYTES];
        self.encode(&mut buf)?;
        codec::domain_hash("PAXAI/evaluator-admission-consent/v1", &buf)
    }
}

/// `AdmitEvaluator`: kind0 owner acceptance of the F03 PENDING grant plus registered-key
/// proof of possession over the exact 426-byte payload. Consumes the stored approval;
/// the grant stays PENDING until a valid snapshot activates it.
///
/// # Errors
/// `NON_CANONICAL`/`BAD_VERSION` for malformed payloads, `F08_OWNER_REQUIRED`,
/// `WRONG_PHASE` for a non-pending grant, `WRONG_DOMAIN`, `F08_WRONG_GENERATION`,
/// `F08_BAD_CONSENT`, `F08_PERMIT_EXPIRED`, host capability failures and every
/// [`AdmissionTable::admit`] refusal.
pub fn admit_evaluator(
    table: &mut AdmissionTable,
    ctx: &AdmissionContext<'_>,
    grant: &EvaluatorGrant,
    request: RequestId,
    payload: &[u8],
) -> CodecResult<AdmissionMeta> {
    if payload.len() != SIGNED_CONSENT_BYTES {
        return Err(NON_CANONICAL);
    }
    let (consent_bytes, signature_bytes) = payload.split_at(CONSENT_BYTES);
    let consent = EvaluatorConsent::decode(consent_bytes)?;
    let signature = Signature64(signature_bytes.try_into().map_err(|_| NON_CANONICAL)?);
    if ctx.invoking_principal != grant.principal {
        return Err(F08_OWNER_REQUIRED);
    }
    grant.validate()?;
    if grant.status != GrantStatus::Pending {
        return Err(WRONG_PHASE);
    }
    if consent.chain != ctx.market.deployment_chain_domain
        || consent.program != ctx.market.program_id
        || consent.market != ctx.market.market_id
    {
        return Err(WRONG_DOMAIN);
    }
    if consent.grant_version != grant.grant_version.get()
        || consent.key_version != grant.key_version.get()
    {
        return Err(F08_WRONG_GENERATION);
    }
    let derived = codec::derive_evaluator(
        ctx.market.market_id,
        grant.principal,
        consent.enrollment_nonce,
    )?;
    if consent.owner != grant.principal
        || consent.evaluator != grant.evaluator
        || derived != grant.evaluator
        || consent.signing_key != grant.signing_key
        || consent.rubric != grant.rubric
        || consent.effective_epoch != grant.effective_epoch
        || consent.request != request
    {
        return Err(F08_BAD_CONSENT);
    }
    if ctx.height >= consent.expiry_height {
        return Err(F08_PERMIT_EXPIRED);
    }
    verify_digest(grant.signing_key, signature, consent.digest()?.bytes()).map_err(|error| {
        match error {
            VerificationError::Application(_) => F08_BAD_CONSENT,
            #[cfg(target_arch = "wasm32")]
            VerificationError::Host(_) => crate::errors::HOST_CAPABILITY,
        }
    })?;
    table.admit(
        ctx,
        &Admission {
            participant: Participant::Evaluator(grant.evaluator),
            delegate_generation: grant.key_version.get(),
            effective_epoch: consent.effective_epoch,
            config_version: consent.config_version,
            approval_digest: consent.approval_digest,
        },
    )
}
