//! F08 bounded admission: approvals, quotas, cooldowns, heartbeat presence and exits.
//! Membership is permission to participate only; it never carries quality or rewards.
use crate::{
    codec::{self, Reader, Writer},
    errors::*,
    evaluators::{
        codec::verify_digest,
        model::{EvaluatorGrant, GrantStatus, VerificationError, CONSENT_BYTES},
    },
    registry::{market_clock, MarketHeader},
    types::*,
    MAX_EVALUATORS, MAX_WORKERS,
};

pub const ROLE_WORKER: u8 = 1;
pub const ROLE_EVALUATOR: u8 = 2;
pub const FLAG_ADMITTED: u8 = 1;
pub const FLAG_DRAINING: u8 = 2;
pub const FLAG_REVOKED: u8 = 4;
pub const EXIT_VOLUNTARY: u8 = 1;
pub const EXIT_RETIRE: u8 = 2;
pub const REMOVE_SECURITY: u8 = 1;
pub const REMOVE_TERMS: u8 = 2;
pub const REMOVE_OPERATOR_DECISION: u8 = 3;
pub const MAX_MEMBERS: usize = MAX_WORKERS + MAX_EVALUATORS;
pub const META_MAX_BYTES: usize = 203;
pub const TABLE_HEADER_BYTES: usize = 20;
pub const TABLE_MAX_BYTES: usize = TABLE_HEADER_BYTES + MAX_MEMBERS * META_MAX_BYTES;

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
impl AdmissionPolicyV1 {
    pub fn digest(&self) -> CodecResult<Digest32> {
        let mut buf = [0u8; 18];
        let mut w = Writer::new(&mut buf);
        for v in [
            self.worker_capacity,
            self.evaluator_capacity,
            self.worker_per_owner,
            self.evaluator_per_owner,
            self.successful_enrollments_per_epoch,
        ] {
            w.u8(v)?;
        }
        w.u16(self.minimum_owner_admission_distance)?;
        w.u16(self.heartbeat_distance)?;
        w.u8(self.missed_epoch_threshold)?;
        w.u8(self.new_member_immunity_epochs)?;
        w.u8(self.score_quorum)?;
        w.u32(0)?;
        let n = w.len();
        codec::domain_hash("PAXAI/admission-policy/v1", &buf[..n])
    }
    const fn capacity(&self, role: u8) -> usize {
        if role == ROLE_WORKER {
            self.worker_capacity as usize
        } else {
            self.evaluator_capacity as usize
        }
    }
    const fn per_owner(&self, role: u8) -> usize {
        if role == ROLE_WORKER {
            self.worker_per_owner as usize
        } else {
            self.evaluator_per_owner as usize
        }
    }
}

pub fn check_role(role: u8) -> CodecResult<u8> {
    if role == ROLE_WORKER || role == ROLE_EVALUATOR {
        Ok(role)
    } else {
        Err(NON_CANONICAL)
    }
}

/// One-use administrator approval: exactly 56 bytes inside the member record.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct Approval {
    pub digest: [u8; 32],
    pub expiry_height: u64,
    pub effective_epoch: u64,
    pub config_version: u64,
}

/// Bound fields hashed into an approval digest.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct ApprovalTerms {
    pub role: u8,
    pub participant: [u8; 32],
    pub owner: PrincipalId,
    pub enrollment_nonce_commitment: [u8; 32],
    pub delegate: PublicKey32,
    pub delegate_generation: u64,
    pub identity_commitment: [u8; 32],
    pub effective_epoch: u64,
    pub config_version: u64,
    pub request: RequestId,
    pub expiry_height: u64,
}
impl ApprovalTerms {
    pub fn digest(&self, market: &MarketHeader) -> CodecResult<Digest32> {
        let mut buf = [0u8; 361];
        let mut w = Writer::new(&mut buf);
        w.put(market.deployment_chain_domain.as_bytes())?;
        w.put(market.program_id.as_bytes())?;
        w.put(market.market_id.as_bytes())?;
        w.u8(check_role(self.role)?)?;
        w.put(&self.participant)?;
        w.put(self.owner.as_bytes())?;
        w.put(&self.enrollment_nonce_commitment)?;
        w.put(&self.delegate.0)?;
        w.u64(self.delegate_generation)?;
        w.put(&self.identity_commitment)?;
        w.u64(self.effective_epoch)?;
        w.u64(self.config_version)?;
        w.put(self.request.as_bytes())?;
        w.u64(self.expiry_height)?;
        let n = w.len();
        codec::domain_hash("PAXAI/admission-approval/v1", &buf[..n])
    }
}

/// AdmissionMeta (F08-R005) plus the bound owner, staged effective epoch,
/// owner admission watermark and the optional unconsumed approval.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct AdmissionMeta {
    pub participant: [u8; 32],
    pub owner: PrincipalId,
    pub role: u8,
    pub admitted_epoch: Option<u64>,
    pub last_heartbeat_epoch: Option<u64>,
    pub last_heartbeat_height: Option<u64>,
    pub immunity_until_epoch: u64,
    pub pending_exit_epoch: Option<u64>,
    pub membership_generation: u64,
    pub complete_missed_opened_epochs: u8,
    pub membership_flags: u8,
    pub effective_epoch: u64,
    pub delegate_generation: u64,
    pub admission_height: Option<u64>,
    pub approval: Option<Approval>,
}
impl AdmissionMeta {
    pub const fn admitted(&self) -> bool {
        self.membership_flags & FLAG_ADMITTED != 0
    }
    pub const fn draining(&self) -> bool {
        self.membership_flags & FLAG_DRAINING != 0
    }
    pub const fn revoked(&self) -> bool {
        self.membership_flags & FLAG_REVOKED != 0
    }
    /// A(x): last accepted heartbeat epoch, else the actual admission epoch.
    pub fn last_activity(&self) -> Option<u64> {
        match (self.last_heartbeat_epoch, self.admitted_epoch) {
            (Some(h), Some(a)) => Some(h.max(a)),
            (h, a) => h.or(a),
        }
    }
    fn validate(&self) -> CodecResult<()> {
        check_role(self.role)?;
        if self.participant == [0; 32]
            || self.membership_generation == 0
            || self.complete_missed_opened_epochs > 2
            || self.membership_flags & !(FLAG_ADMITTED | FLAG_DRAINING | FLAG_REVOKED) != 0
            || (self.last_heartbeat_height.is_some() && self.last_heartbeat_epoch.is_none())
        {
            return Err(NON_CANONICAL);
        }
        Ok(())
    }
    fn write(&self, w: &mut Writer<'_>) -> CodecResult<()> {
        fn opt(w: &mut Writer<'_>, v: Option<u64>) -> CodecResult<()> {
            w.boolean(v.is_some())?;
            if let Some(v) = v {
                w.u64(v)?;
            }
            Ok(())
        }
        w.put(&self.participant)?;
        w.put(self.owner.as_bytes())?;
        w.u8(self.role)?;
        opt(w, self.admitted_epoch)?;
        opt(w, self.last_heartbeat_epoch)?;
        opt(w, self.last_heartbeat_height)?;
        w.u64(self.immunity_until_epoch)?;
        opt(w, self.pending_exit_epoch)?;
        w.u64(self.membership_generation)?;
        w.u8(self.complete_missed_opened_epochs)?;
        w.u8(self.membership_flags)?;
        w.u16(0)?;
        w.u64(self.effective_epoch)?;
        w.u64(self.delegate_generation)?;
        opt(w, self.admission_height)?;
        w.boolean(self.approval.is_some())?;
        if let Some(a) = self.approval {
            w.put(&a.digest)?;
            w.u64(a.expiry_height)?;
            w.u64(a.effective_epoch)?;
            w.u64(a.config_version)?;
        }
        Ok(())
    }
    fn read(r: &mut Reader<'_>) -> CodecResult<Self> {
        fn opt(r: &mut Reader<'_>) -> CodecResult<Option<u64>> {
            Ok(if r.boolean()? { Some(r.u64()?) } else { None })
        }
        let participant: [u8; 32] = r.fixed()?;
        let owner = PrincipalId::new(r.fixed()?)?;
        let role = r.u8()?;
        let admitted_epoch = opt(r)?;
        let last_heartbeat_epoch = opt(r)?;
        let last_heartbeat_height = opt(r)?;
        let immunity_until_epoch = r.u64()?;
        let pending_exit_epoch = opt(r)?;
        let membership_generation = r.u64()?;
        let complete_missed_opened_epochs = r.u8()?;
        let membership_flags = r.u8()?;
        r.reserved(2)?;
        let effective_epoch = r.u64()?;
        let delegate_generation = r.u64()?;
        let admission_height = opt(r)?;
        let approval = if r.boolean()? {
            Some(Approval {
                digest: r.fixed()?,
                expiry_height: r.u64()?,
                effective_epoch: r.u64()?,
                config_version: r.u64()?,
            })
        } else {
            None
        };
        let v = Self {
            participant,
            owner,
            role,
            admitted_epoch,
            last_heartbeat_epoch,
            last_heartbeat_height,
            immunity_until_epoch,
            pending_exit_epoch,
            membership_generation,
            complete_missed_opened_epochs,
            membership_flags,
            effective_epoch,
            delegate_generation,
            admission_height,
            approval,
        };
        v.validate()?;
        Ok(v)
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
    fn live(&self) -> CodecResult<()> {
        match self.market.lifecycle {
            1 | 2 => Ok(()),
            3 => Err(F08_MARKET_PAUSED),
            _ => Err(WRONG_PHASE),
        }
    }
}

/// Bounded F08 table in the shared F07/F08 section. Entries sorted by (role, id).
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct AdmissionTable {
    pub epoch_present: bool,
    pub current_epoch: u64,
    pub enrollments_this_epoch: u8,
    entries: [Option<AdmissionMeta>; MAX_MEMBERS],
    count: usize,
}
impl Default for AdmissionTable {
    fn default() -> Self {
        Self::new()
    }
}
impl AdmissionTable {
    pub const fn new() -> Self {
        Self {
            epoch_present: false,
            current_epoch: 0,
            enrollments_this_epoch: 0,
            entries: [None; MAX_MEMBERS],
            count: 0,
        }
    }
    pub const fn len(&self) -> usize {
        self.count
    }
    pub const fn is_empty(&self) -> bool {
        self.count == 0
    }
    pub fn iter(&self) -> impl Iterator<Item = &AdmissionMeta> {
        self.entries[..self.count].iter().flatten()
    }
    pub fn get(&self, participant: [u8; 32]) -> Option<AdmissionMeta> {
        self.iter().find(|m| m.participant == participant).copied()
    }
    pub fn role_count(&self, role: u8) -> usize {
        self.iter().filter(|m| m.role == role).count()
    }
    fn owner_count(&self, role: u8, owner: PrincipalId) -> usize {
        self.iter()
            .filter(|m| m.role == role && m.owner == owner && m.admitted())
            .count()
    }
    fn owner_watermark(&self, owner: PrincipalId) -> Option<u64> {
        self.iter()
            .filter(|m| m.owner == owner)
            .filter_map(|m| m.admission_height)
            .max()
    }
    fn position(&self, participant: [u8; 32]) -> CodecResult<usize> {
        self.entries[..self.count]
            .iter()
            .position(|m| m.is_some_and(|m| m.participant == participant))
            .ok_or(NOT_FOUND)
    }
    pub fn insert(&mut self, meta: AdmissionMeta) -> CodecResult<()> {
        meta.validate()?;
        if self.iter().any(|m| m.participant == meta.participant) {
            return Err(F08_DUPLICATE_IDENTITY);
        }
        if self.count >= MAX_MEMBERS || self.role_count(meta.role) >= POLICY_V1.capacity(meta.role)
        {
            return Err(F08_CAPACITY_EXCEEDED);
        }
        let key = (meta.role, meta.participant);
        let at = self.entries[..self.count]
            .iter()
            .position(|m| m.is_some_and(|m| (m.role, m.participant) > key))
            .unwrap_or(self.count);
        self.entries.copy_within(at..self.count, at + 1);
        self.entries[at] = Some(meta);
        self.count += 1;
        Ok(())
    }
    pub fn replace(&mut self, meta: AdmissionMeta) -> CodecResult<()> {
        meta.validate()?;
        let at = self.position(meta.participant)?;
        self.entries[at] = Some(meta);
        Ok(())
    }
    pub fn remove(&mut self, participant: [u8; 32]) -> CodecResult<AdmissionMeta> {
        let at = self.position(participant)?;
        let meta = self.entries[at].ok_or(NOT_FOUND)?;
        self.entries.copy_within(at + 1..self.count, at);
        self.count -= 1;
        self.entries[self.count] = None;
        Ok(meta)
    }
    pub fn encode(&self, out: &mut [u8]) -> CodecResult<usize> {
        let mut w = Writer::new(out);
        w.u16(crate::SCHEMA_VERSION)?;
        w.boolean(self.epoch_present)?;
        w.u64(self.current_epoch)?;
        w.u8(self.enrollments_this_epoch)?;
        w.u8(u8::try_from(self.count).map_err(|_| ARITHMETIC)?)?;
        w.put(&[0; 7])?;
        for m in self.iter() {
            m.write(&mut w)?;
        }
        Ok(w.len())
    }
    /// An empty section is the initialized market: no epoch opened, no members.
    pub fn decode(input: &[u8]) -> CodecResult<Self> {
        let mut t = Self::new();
        if input.is_empty() {
            return Ok(t);
        }
        if input.len() > TABLE_MAX_BYTES {
            return Err(CAPACITY);
        }
        let mut r = Reader::new(input);
        if r.u16()? != crate::SCHEMA_VERSION {
            return Err(BAD_VERSION);
        }
        t.epoch_present = r.boolean()?;
        t.current_epoch = r.u64()?;
        t.enrollments_this_epoch = r.u8()?;
        let count = usize::from(r.u8()?);
        r.reserved(7)?;
        if count > MAX_MEMBERS || (!t.epoch_present && t.current_epoch != 0) {
            return Err(NON_CANONICAL);
        }
        for _ in 0..count {
            let m = AdmissionMeta::read(&mut r)?;
            if t.count > 0
                && t.entries[t.count - 1]
                    .is_some_and(|p| (p.role, p.participant) >= (m.role, m.participant))
            {
                return Err(NON_CANONICAL);
            }
            t.insert(m)?;
        }
        r.finish()?;
        Ok(t)
    }

    /// Bootstrap rule: 0 before any opening, else clock_epoch+1 (never backdated).
    pub fn required_effective_epoch(&self, ctx: &AdmissionContext<'_>) -> CodecResult<u64> {
        if !self.epoch_present {
            return Ok(0);
        }
        let clock = market_clock(ctx.market.origin_height, ctx.height).map_err(|_| WRONG_EPOCH)?;
        clock.epoch.checked_add(1).ok_or(ARITHMETIC)
    }

    /// Market-owner kind0 approval for an existing current identity record
    /// (F02 ENROLLED worker or F03 PENDING grant). Reapproval replaces.
    pub fn approve(
        &mut self,
        ctx: &AdmissionContext<'_>,
        terms: &ApprovalTerms,
        identity_count: usize,
    ) -> CodecResult<Digest32> {
        ctx.live()?;
        ctx.require_market_owner()?;
        check_role(terms.role)?;
        if terms.config_version != ctx.market.active_config_version {
            return Err(WRONG_CONFIG);
        }
        let required = self.required_effective_epoch(ctx)?;
        if terms.effective_epoch != required {
            return Err(WRONG_EPOCH);
        }
        let work_end = crate::state::HeightWindow::epoch(
            ctx.market.origin_height,
            terms.effective_epoch,
            0,
            64,
        )?
        .end;
        if terms.expiry_height <= ctx.height || terms.expiry_height > work_end {
            return Err(F08_PERMIT_EXPIRED);
        }
        if identity_count > POLICY_V1.capacity(terms.role) {
            return Err(F08_CAPACITY_EXCEEDED);
        }
        let digest = terms.digest(ctx.market)?;
        let approval = Approval {
            digest: digest.bytes(),
            expiry_height: terms.expiry_height,
            effective_epoch: terms.effective_epoch,
            config_version: terms.config_version,
        };
        match self.get(terms.participant) {
            Some(m) if m.admitted() => Err(F08_DUPLICATE_IDENTITY),
            Some(mut m) => {
                if m.owner != terms.owner || m.role != terms.role {
                    return Err(CONFLICT);
                }
                m.approval = Some(approval);
                m.delegate_generation = terms.delegate_generation;
                self.replace(m)?;
                Ok(digest)
            }
            None => {
                self.insert(AdmissionMeta {
                    participant: terms.participant,
                    owner: terms.owner,
                    role: terms.role,
                    admitted_epoch: None,
                    last_heartbeat_epoch: None,
                    last_heartbeat_height: None,
                    immunity_until_epoch: 0,
                    pending_exit_epoch: None,
                    membership_generation: 1,
                    complete_missed_opened_epochs: 0,
                    membership_flags: 0,
                    effective_epoch: terms.effective_epoch,
                    delegate_generation: terms.delegate_generation,
                    admission_height: None,
                    approval: Some(approval),
                })?;
                Ok(digest)
            }
        }
    }

    /// Clears only an unconsumed approval; never removes a live member.
    pub fn revoke_approval(
        &mut self,
        ctx: &AdmissionContext<'_>,
        participant: [u8; 32],
    ) -> CodecResult<()> {
        ctx.require_market_owner()?;
        let m = self.get(participant).ok_or(NOT_FOUND)?;
        if m.admitted() {
            return Err(F08_PERMIT_CONSUMED);
        }
        if m.approval.is_none() {
            return Err(NOT_FOUND);
        }
        self.remove(participant)?;
        Ok(())
    }

    /// Owner acceptance consuming the stored approval. The caller has already
    /// verified the delegate consent bound to `approval_digest`.
    #[allow(clippy::too_many_arguments)]
    pub fn admit(
        &mut self,
        ctx: &AdmissionContext<'_>,
        role: u8,
        participant: [u8; 32],
        delegate_generation: u64,
        effective_epoch: u64,
        config_version: u64,
        approval_digest: [u8; 32],
    ) -> CodecResult<AdmissionMeta> {
        ctx.live()?;
        check_role(role)?;
        let mut m = self
            .get(participant)
            .ok_or(F08_ADMINISTRATOR_APPROVAL_REQUIRED)?;
        if ctx.invoking_principal != m.owner {
            return Err(F08_OWNER_REQUIRED);
        }
        if m.role != role {
            return Err(NON_CANONICAL);
        }
        if m.admitted() {
            return Err(F08_DUPLICATE_IDENTITY);
        }
        let approval = m.approval.ok_or(F08_ADMINISTRATOR_APPROVAL_REQUIRED)?;
        if approval.digest != approval_digest {
            return Err(F08_BAD_CONSENT);
        }
        if delegate_generation != m.delegate_generation {
            return Err(F08_WRONG_GENERATION);
        }
        if config_version != approval.config_version
            || config_version != ctx.market.active_config_version
        {
            return Err(WRONG_CONFIG);
        }
        if ctx.height >= approval.expiry_height {
            return Err(F08_PERMIT_EXPIRED);
        }
        let required = self.required_effective_epoch(ctx)?;
        if effective_epoch != required || approval.effective_epoch != required {
            return Err(WRONG_EPOCH);
        }
        if self.owner_count(role, m.owner) >= POLICY_V1.per_owner(role) {
            return Err(F08_OWNER_CAPACITY_EXCEEDED);
        }
        if role == ROLE_EVALUATOR
            && (m.owner == ctx.market.owner_principal
                || self
                    .iter()
                    .any(|w| w.role == ROLE_WORKER && w.admitted() && w.owner == m.owner))
        {
            return Err(ROLE_CONFLICT);
        }
        if self.enrollments_this_epoch >= POLICY_V1.successful_enrollments_per_epoch {
            return Err(F08_ADMISSION_WINDOW_FULL);
        }
        if let Some(last) = self.owner_watermark(m.owner) {
            let distance = ctx.height.checked_sub(last).ok_or(ARITHMETIC)?;
            if distance < u64::from(POLICY_V1.minimum_owner_admission_distance) {
                return Err(F08_RATE_LIMITED);
            }
        }
        let enrolled = self
            .enrollments_this_epoch
            .checked_add(1)
            .ok_or(ARITHMETIC)?;
        m.approval = None;
        m.membership_flags = FLAG_ADMITTED;
        m.effective_epoch = effective_epoch;
        m.immunity_until_epoch = effective_epoch;
        m.admission_height = Some(ctx.height);
        self.replace(m)?;
        self.enrollments_this_epoch = enrolled;
        Ok(m)
    }

    fn current_epoch(&self) -> CodecResult<u64> {
        if self.epoch_present {
            Ok(self.current_epoch)
        } else {
            Err(WRONG_EPOCH)
        }
    }

    /// Delegate-signed key activity in the current opened epoch.
    pub fn heartbeat(
        &mut self,
        ctx: &AdmissionContext<'_>,
        participant: [u8; 32],
        role: u8,
        generation: u64,
        delegate_generation: u64,
        epoch: u64,
    ) -> CodecResult<AdmissionMeta> {
        check_role(role)?;
        let current = self.current_epoch()?;
        let mut m = self.get(participant).ok_or(NOT_FOUND)?;
        if m.role != role || m.admitted_epoch.is_none() || !m.admitted() {
            return Err(NOT_FOUND);
        }
        if m.revoked() {
            return Err(F08_DELEGATE_REVOKED);
        }
        if generation != m.membership_generation || delegate_generation != m.delegate_generation {
            return Err(F08_WRONG_GENERATION);
        }
        let clock = market_clock(ctx.market.origin_height, ctx.height).map_err(|_| WRONG_EPOCH)?;
        if epoch != current || clock.epoch != current {
            return Err(WRONG_EPOCH);
        }
        if m.pending_exit_epoch.is_some_and(|e| e <= current) {
            return Err(WRONG_PHASE);
        }
        if let Some(last) = m.last_heartbeat_height {
            let distance = ctx.height.checked_sub(last).ok_or(ARITHMETIC)?;
            if distance < u64::from(POLICY_V1.heartbeat_distance) {
                return Err(F08_RATE_LIMITED);
            }
        }
        m.last_heartbeat_epoch = Some(current);
        m.last_heartbeat_height = Some(ctx.height);
        m.complete_missed_opened_epochs = 0;
        self.replace(m)?;
        Ok(m)
    }

    /// Owner-only voluntary exit effective at the next opened snapshot.
    pub fn request_exit(
        &mut self,
        ctx: &AdmissionContext<'_>,
        participant: [u8; 32],
        generation: u64,
        expected_effective_epoch: u64,
        reason: u8,
    ) -> CodecResult<AdmissionMeta> {
        if reason != EXIT_VOLUNTARY && reason != EXIT_RETIRE {
            return Err(NON_CANONICAL);
        }
        let mut m = self.get(participant).ok_or(NOT_FOUND)?;
        if ctx.invoking_principal != m.owner {
            return Err(F08_OWNER_REQUIRED);
        }
        if generation != m.membership_generation {
            return Err(F08_WRONG_GENERATION);
        }
        let exit = self.next_epoch()?;
        if expected_effective_epoch != exit {
            return Err(WRONG_EPOCH);
        }
        if m.pending_exit_epoch == Some(exit) && m.draining() {
            return Ok(m);
        }
        m.membership_flags |= FLAG_DRAINING;
        m.pending_exit_epoch = Some(exit);
        self.replace(m)?;
        Ok(m)
    }

    /// Owner-only, before removal; never resets admission, immunity or heartbeat history.
    pub fn cancel_exit(
        &mut self,
        ctx: &AdmissionContext<'_>,
        participant: [u8; 32],
        generation: u64,
    ) -> CodecResult<AdmissionMeta> {
        let mut m = self.get(participant).ok_or(NOT_FOUND)?;
        if ctx.invoking_principal != m.owner {
            return Err(F08_OWNER_REQUIRED);
        }
        if generation != m.membership_generation {
            return Err(F08_WRONG_GENERATION);
        }
        if m.revoked() || m.pending_exit_epoch.is_none() {
            return Err(WRONG_PHASE);
        }
        m.pending_exit_epoch = None;
        m.membership_flags &= !FLAG_DRAINING;
        self.replace(m)?;
        Ok(m)
    }

    /// Visibly discretionary administrator removal; SECURITY revokes immediately.
    pub fn administrative_remove(
        &mut self,
        ctx: &AdmissionContext<'_>,
        participant: [u8; 32],
        generation: u64,
        reason: u8,
    ) -> CodecResult<AdmissionMeta> {
        ctx.require_market_owner()?;
        if !(REMOVE_SECURITY..=REMOVE_OPERATOR_DECISION).contains(&reason) {
            return Err(NON_CANONICAL);
        }
        let mut m = self.get(participant).ok_or(NOT_FOUND)?;
        if generation != m.membership_generation {
            return Err(F08_WRONG_GENERATION);
        }
        m.membership_flags |= FLAG_DRAINING;
        if reason == REMOVE_SECURITY {
            m.membership_flags |= FLAG_REVOKED;
        }
        m.pending_exit_epoch = Some(self.next_epoch()?);
        self.replace(m)?;
        Ok(m)
    }

    /// Permissionless deterministic inactivity prune (F08-R013/R016).
    pub fn prune_inactive(
        &mut self,
        role: u8,
        expected_candidate: [u8; 32],
        expected_revision: u64,
        state_revision: u64,
    ) -> CodecResult<AdmissionMeta> {
        check_role(role)?;
        if expected_revision != state_revision {
            return Err(F08_STALE_STATE);
        }
        let epoch = self.current_epoch()?;
        let candidate = crate::roster::prune_candidate(self, role, epoch)?;
        if candidate.participant != expected_candidate {
            return Err(F08_CANDIDATE_CHANGED);
        }
        let mut m = candidate;
        m.membership_flags |= FLAG_DRAINING;
        m.pending_exit_epoch = Some(self.next_epoch()?);
        self.replace(m)?;
        Ok(m)
    }

    fn next_epoch(&self) -> CodecResult<u64> {
        self.current_epoch()?.checked_add(1).ok_or(ARITHMETIC)
    }
}

/// EvaluatorAdmissionConsentV1: exactly 362 canonical bytes.
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
    pub approval_digest: [u8; 32],
    pub request: RequestId,
    pub grant_version: u64,
    pub key_version: u64,
    pub effective_epoch: u64,
    pub config_version: u64,
    pub expiry_height: u64,
}
impl EvaluatorConsent {
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
        w.put(&self.approval_digest)?;
        w.put(self.request.as_bytes())?;
        for v in [
            self.grant_version,
            self.key_version,
            self.effective_epoch,
            self.config_version,
            self.expiry_height,
        ] {
            w.u64(v)?;
        }
        if w.len() == CONSENT_BYTES {
            Ok(())
        } else {
            Err(NON_CANONICAL)
        }
    }
    pub fn decode(input: &[u8]) -> CodecResult<Self> {
        if input.len() != CONSENT_BYTES {
            return Err(NON_CANONICAL);
        }
        let mut r = Reader::new(input);
        if r.u16()? != 1 {
            return Err(BAD_VERSION);
        }
        let v = Self {
            chain: ChainDomain::new(r.fixed()?)?,
            program: ProgramId::new(r.fixed()?)?,
            market: MarketId::new(r.fixed()?)?,
            evaluator: EvaluatorId::new(r.fixed()?)?,
            owner: PrincipalId::new(r.fixed()?)?,
            signing_key: PublicKey32(r.fixed()?),
            enrollment_nonce: r.fixed()?,
            rubric: RubricDigest::new(r.fixed()?)?,
            approval_digest: r.fixed()?,
            request: RequestId::new(r.fixed()?)?,
            grant_version: r.u64()?,
            key_version: r.u64()?,
            effective_epoch: r.u64()?,
            config_version: r.u64()?,
            expiry_height: r.u64()?,
        };
        r.finish()?;
        Ok(v)
    }
    pub fn digest(&self) -> CodecResult<Digest32> {
        let mut buf = [0u8; CONSENT_BYTES];
        self.encode(&mut buf)?;
        codec::domain_hash("PAXAI/evaluator-admission-consent/v1", &buf)
    }
}

/// AdmitEvaluator: kind0 owner acceptance of the F03 PENDING grant plus registered
/// key proof of possession over the exact 426-byte payload. Consumes the approval.
pub fn admit_evaluator(
    table: &mut AdmissionTable,
    ctx: &AdmissionContext<'_>,
    grant: &EvaluatorGrant,
    payload: &[u8],
) -> CodecResult<AdmissionMeta> {
    if payload.len() != CONSENT_BYTES + 64 {
        return Err(NON_CANONICAL);
    }
    let consent = EvaluatorConsent::decode(&payload[..CONSENT_BYTES])?;
    let signature = Signature64(
        payload[CONSENT_BYTES..]
            .try_into()
            .map_err(|_| NON_CANONICAL)?,
    );
    if ctx.invoking_principal != grant.principal {
        return Err(F08_OWNER_REQUIRED);
    }
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
        || ctx.height >= consent.expiry_height
    {
        return Err(F08_BAD_CONSENT);
    }
    let digest = consent.digest()?;
    verify_digest(grant.signing_key, signature, digest.bytes()).map_err(|e| match e {
        VerificationError::Application(_) => F08_BAD_CONSENT,
        _ => HOST_CAPABILITY,
    })?;
    table.admit(
        ctx,
        ROLE_EVALUATOR,
        grant.evaluator.bytes(),
        grant.key_version.get(),
        consent.effective_epoch,
        consent.config_version,
        consent.approval_digest,
    )
}
