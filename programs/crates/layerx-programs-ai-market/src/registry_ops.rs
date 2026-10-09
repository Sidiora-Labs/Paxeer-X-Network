use crate::{
    codec::{self, derive_market, EventCommon, Reader, ValidatedEnvelope, Writer},
    dispatch::{self, Operation},
    errors::{
        CodecResult, ARITHMETIC, CAPACITY, CONFLICT, F01_ACCOUNT_BINDING_MISSING,
        F01_ACTIVATION_TOO_EARLY, F01_ALREADY_ACTIVATED, F01_ALREADY_CREATED,
        F01_CAPACITY_UNAVAILABLE, F01_GRANT_ALREADY_REVOKED, F01_INVALID_REASON,
        F01_LIFECYCLE_CLOSED, F01_NO_PENDING_POLICY, F01_PENDING_POLICY_EXISTS,
        F01_PRINCIPAL_MISMATCH, F01_STALE_REVISION, F01_VERSION_MISMATCH, F01_WRONG_LIFECYCLE,
        NON_CANONICAL, NOT_FOUND, REVOKED, ROLE_CONFLICT, UNAUTHORIZED, UNKNOWN_OPERATION,
        WRONG_CONFIG, WRONG_MARKET,
    },
    policy::{
        fold_policy_history, initial_policy_history_root, next_config_version,
        validate_policy_records, PendingPolicy, PolicyHistoryHeader, TaskPolicyV1,
        MAX_RECENT_POLICY_HEADERS, PENDING_POLICY_BYTES, POLICY_HISTORY_HEADER_BYTES,
        TASK_POLICY_BYTES,
    },
    registry::{
        check_f01_capacity, derive_rewards_account, market_clock, MarketHeader, OperatorGrant,
        COMMON_SECTION_HEADER_BYTES, F01_SECTION_CAP, MARKET_HEADER_ABSENT_BYTES,
        MARKET_HEADER_PRESENT_BYTES, OPERATOR_GRANT_BYTES,
    },
    state::{
        decode_replay, ActorSlot, Control, ReplayDecision, ReplayRequest, ReplayTable,
        RetainedResult, Section, SharedState,
    },
    types::{
        AccountId, AssetId, ChainDomain, Digest32, MarketId, MetadataDigest, PolicyDigest,
        Presence, PrincipalId, ProgramId, RequestId, ResultDigest, Version,
    },
};

pub const REGISTERED: u8 = 1;
pub const ACTIVE: u8 = 2;
pub const SUSPENDED: u8 = 3;
pub const WINDING_DOWN: u8 = 4;
pub const CLOSED: u8 = 5;
pub const PERMIT_METADATA: u8 = 1;
pub const PERMIT_SUSPEND: u8 = 2;
pub const ACTIVATED: u8 = 1;
pub const CANCELLED: u8 = 2;
/// Task count zero and task-set root absent; the task records themselves belong to task admission.
pub const EMPTY_TASK_REGION: [u8; 3] = [0; 3];
pub const RECEIPT_MAX_BYTES: usize = 115;
pub const EVENT_SUFFIX_BYTES: usize = 49;
pub const SECTION_PAYLOAD_CAP: usize = F01_SECTION_CAP - COMMON_SECTION_HEADER_BYTES;
const OWNER_AUTHORITY: u64 = 1;
const TREASURY_PRESENCE_OFFSET: usize = 226;

/// The whole F01 policy-lifecycle section. Unused history slots always repeat the first header.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct PolicySection<'a> {
    pub header: MarketHeader,
    pub operator: Presence<OperatorGrant>,
    pub current: TaskPolicyV1,
    pub pending: Presence<PendingPolicy>,
    recent: [PolicyHistoryHeader; MAX_RECENT_POLICY_HEADERS],
    recent_len: usize,
    pub history_root: Digest32,
    pub task_region: &'a [u8],
}

impl<'a> PolicySection<'a> {
    #[must_use]
    pub fn recent(&self) -> &[PolicyHistoryHeader] {
        &self.recent[..self.recent_len]
    }
    fn push_history(&mut self, header: PolicyHistoryHeader) -> CodecResult<()> {
        if self.recent_len == MAX_RECENT_POLICY_HEADERS {
            self.history_root = fold_policy_history(self.history_root, 0, &self.recent[..1])?;
            self.recent.copy_within(1.., 0);
            self.recent[MAX_RECENT_POLICY_HEADERS - 1] = header;
        } else {
            self.recent[self.recent_len] = header;
            self.recent_len += 1;
        }
        Ok(())
    }
    fn activated(&self, version: u64) -> bool {
        version == self.header.active_config_version
            || self
                .recent()
                .iter()
                .any(|h| h.config_version == version && h.disposition == ACTIVATED)
    }
    /// Checks the section's header, policy records, operator grant, and pending proposer.
    ///
    /// # Errors
    /// Propagates header, grant, and `validate_policy_records` refusals; returns `NON_CANONICAL` when the recent-header count is outside 1..=`MAX_RECENT_POLICY_HEADERS`; `F01_VERSION_MISMATCH` when the active version differs from `current`; `ROLE_CONFLICT` when the operator is the owner or treasury; `F01_PRINCIPAL_MISMATCH` when the pending proposer is not the owner.
    pub fn validate(&self) -> CodecResult<()> {
        self.header.validate()?;
        self.validate_records()
    }
    /// [`Self::validate`] after the header, for a header `MarketHeader::decode` validated.
    fn validate_records(&self) -> CodecResult<()> {
        if self.recent_len == 0 || self.recent_len > MAX_RECENT_POLICY_HEADERS {
            return Err(NON_CANONICAL);
        }
        validate_policy_records(
            &self.current,
            &self.pending,
            self.recent(),
            self.header.highest_config_version,
        )?;
        if self.header.active_config_version != self.current.config_version {
            return Err(F01_VERSION_MISMATCH);
        }
        if let Presence::Present(grant) = self.operator {
            grant.validate()?;
            if grant.principal == self.header.owner_principal
                || self.header.treasury_principal == Presence::Present(grant.principal)
            {
                return Err(ROLE_CONFLICT);
            }
        }
        if let Presence::Present(pending) = self.pending {
            if pending.proposer != self.header.owner_principal {
                return Err(F01_PRINCIPAL_MISMATCH);
            }
        }
        Ok(())
    }
    /// Returns the encoded section length.
    ///
    /// # Errors
    /// Returns `F01_CAPACITY_UNAVAILABLE` when the length overflows or exceeds the F01 section or state cap.
    pub fn encoded_len(&self) -> CodecResult<usize> {
        let mut n = self.header.encoded_len();
        for bytes in [
            1,
            if matches!(self.operator, Presence::Present(_)) {
                OPERATOR_GRANT_BYTES
            } else {
                0
            },
            TASK_POLICY_BYTES,
            1,
            if matches!(self.pending, Presence::Present(_)) {
                PENDING_POLICY_BYTES
            } else {
                0
            },
            2,
            self.recent_len
                .checked_mul(POLICY_HISTORY_HEADER_BYTES)
                .ok_or(F01_CAPACITY_UNAVAILABLE)?,
            32,
            4,
            self.task_region.len(),
        ] {
            n = n.checked_add(bytes).ok_or(F01_CAPACITY_UNAVAILABLE)?;
        }
        check_f01_capacity(n, crate::MAX_STATE_BYTES)?;
        Ok(n)
    }
    /// Writes the canonical section bytes into `output`.
    ///
    /// # Errors
    /// Propagates `validate`, `encoded_len`, and record `encode` refusals; returns `CAPACITY` when `output` is shorter than the encoded length; `ARITHMETIC` when a length prefix does not fit its width.
    pub fn encode(&self, output: &mut [u8]) -> CodecResult<usize> {
        self.validate()?;
        let n = self.encoded_len()?;
        if output.len() < n {
            return Err(CAPACITY);
        }
        let h = self.header.encode(output)?;
        let mut w = Writer::new(&mut output[h..n]);
        w.presence(&self.operator, |w, grant| {
            let mut b = [0; OPERATOR_GRANT_BYTES];
            grant.encode(&mut b)?;
            w.put(&b)
        })?;
        let mut policy = [0; TASK_POLICY_BYTES];
        self.current.encode(&mut policy)?;
        w.put(&policy)?;
        w.presence(&self.pending, |w, pending| {
            let mut b = [0; PENDING_POLICY_BYTES];
            pending.encode(&mut b)?;
            w.put(&b)
        })?;
        w.u16(u16::try_from(self.recent_len).map_err(|_| ARITHMETIC)?)?;
        for header in self.recent() {
            let mut b = [0; POLICY_HISTORY_HEADER_BYTES];
            header.encode(&mut b)?;
            w.put(&b)?;
        }
        w.put(self.history_root.as_bytes())?;
        w.u32(u32::try_from(self.task_region.len()).map_err(|_| ARITHMETIC)?)?;
        w.put(self.task_region)?;
        Ok(h + w.len())
    }
    /// Reads a section from its exact canonical bytes, borrowing the task region.
    ///
    /// # Errors
    /// Returns `F01_CAPACITY_UNAVAILABLE` when the input exceeds the section payload cap; `NON_CANONICAL` when the input is short, has trailing bytes, or holds a bad recent-header count; `ARITHMETIC` when the task length does not fit `usize`; propagates record `decode`, `validate`, and `encoded_len` refusals.
    pub fn decode(input: &'a [u8]) -> CodecResult<Self> {
        if input.len() > SECTION_PAYLOAD_CAP {
            return Err(F01_CAPACITY_UNAVAILABLE);
        }
        let header_len = match input.get(TREASURY_PRESENCE_OFFSET) {
            Some(1) => MARKET_HEADER_PRESENT_BYTES,
            _ => MARKET_HEADER_ABSENT_BYTES,
        };
        let mut r = Reader::new(input);
        let header = MarketHeader::decode(r.take(header_len)?)?;
        let operator = r.presence(|r| OperatorGrant::decode(r.take(OPERATOR_GRANT_BYTES)?))?;
        let current = TaskPolicyV1::decode(r.take(TASK_POLICY_BYTES)?)?;
        let pending = r.presence(|r| PendingPolicy::decode(r.take(PENDING_POLICY_BYTES)?))?;
        let recent_len = usize::from(r.u16()?);
        if recent_len == 0 || recent_len > MAX_RECENT_POLICY_HEADERS {
            return Err(NON_CANONICAL);
        }
        let first = PolicyHistoryHeader::decode(r.take(POLICY_HISTORY_HEADER_BYTES)?)?;
        let mut recent = [first; MAX_RECENT_POLICY_HEADERS];
        for slot in recent.iter_mut().take(recent_len).skip(1) {
            *slot = PolicyHistoryHeader::decode(r.take(POLICY_HISTORY_HEADER_BYTES)?)?;
        }
        let history_root = Digest32::new(r.fixed()?)?;
        let task_len = usize::try_from(r.u32()?).map_err(|_| ARITHMETIC)?;
        let task_region = r.take(task_len)?;
        r.finish()?;
        let section = Self {
            header,
            operator,
            current,
            pending,
            recent,
            recent_len,
            history_root,
            task_region,
        };
        section.validate_records()?;
        section.encoded_len()?;
        Ok(section)
    }
}

/// Section transform for the epoch-opening owner: promotes an eligible pending policy without
/// a revision increment of its own. Readiness checks and the commit belong to the caller.
///
/// # Errors
/// Returns `F01_LIFECYCLE_CLOSED` when the market is winding down or closed; `F01_NO_PENDING_POLICY` when nothing is staged; `F01_ACTIVATION_TOO_EARLY` when `epoch` precedes the effective epoch; propagates history-fold and `validate` refusals.
pub fn activate_pending_policy<'a>(
    section: &PolicySection<'a>,
    epoch: u64,
) -> CodecResult<PolicySection<'a>> {
    if section.header.lifecycle >= WINDING_DOWN {
        return Err(F01_LIFECYCLE_CLOSED);
    }
    let pending = match section.pending {
        Presence::Present(pending) => pending,
        Presence::Absent => return Err(F01_NO_PENDING_POLICY),
    };
    if epoch < pending.effective_epoch {
        return Err(F01_ACTIVATION_TOO_EARLY);
    }
    let mut next = *section;
    next.current = pending.policy;
    next.header.active_config_version = pending.policy.config_version;
    next.pending = Presence::Absent;
    next.push_history(PolicyHistoryHeader {
        config_version: pending.policy.config_version,
        digest: pending.digest,
        effective_epoch: epoch,
        disposition: ACTIVATED,
    })?;
    next.validate()?;
    Ok(next)
}

/// Authoritative call context obtained from the host (executing Program, invoking principal,
/// committed height) plus the deployment chain domain.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct CallContext {
    pub chain: ChainDomain,
    pub program: ProgramId,
    pub principal: PrincipalId,
    pub height: u64,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct Receipt {
    bytes: [u8; RECEIPT_MAX_BYTES],
    len: usize,
    pub digest: ResultDigest,
}
impl Receipt {
    #[must_use]
    pub fn bytes(&self) -> &[u8] {
        &self.bytes[..self.len]
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
#[allow(
    clippy::large_enum_variant,
    reason = "no_std without alloc: Box is unavailable, and Applied is the primary result"
)]
pub enum Outcome<'a> {
    Applied {
        state: SharedState<'a>,
        receipt: Receipt,
        event_len: usize,
    },
    AlreadyApplied(RetainedResult),
}

#[derive(Clone, Copy)]
#[allow(
    clippy::large_enum_variant,
    reason = "no_std without alloc: Box is unavailable for the staged policy payload"
)]
enum Action {
    Stage(TaskPolicyV1, u64),
    Cancel(u64),
    Schedule(u64),
    Suspend([u8; 32], u64),
    Metadata(MetadataDigest, u64),
    Appoint(PrincipalId, u8, u64),
    Revoke(u64),
}

fn parse(operation: Operation, payload: &[u8]) -> CodecResult<(u64, Action)> {
    operation.validate_payload_length(payload.len())?;
    let mut r = Reader::new(payload);
    let expected = r.u64()?;
    let action = match operation {
        dispatch::STAGE_POLICY => {
            let policy = TaskPolicyV1::decode(r.take(TASK_POLICY_BYTES)?)?;
            Action::Stage(policy, r.u64()?)
        }
        dispatch::CANCEL_POLICY => Action::Cancel(r.u64()?),
        dispatch::SCHEDULE_ACTIVATION => Action::Schedule(r.u64()?),
        dispatch::SUSPEND => {
            let reason = r.fixed()?;
            Action::Suspend(reason, if r.remaining() == 8 { r.u64()? } else { 0 })
        }
        dispatch::UPDATE_METADATA => {
            let digest = MetadataDigest::new(r.fixed()?)?;
            Action::Metadata(digest, if r.remaining() == 8 { r.u64()? } else { 0 })
        }
        dispatch::APPOINT_OPERATOR => {
            let principal = PrincipalId::new(r.fixed()?)?;
            let permissions = r.u8()?;
            Action::Appoint(principal, permissions, r.u64()?)
        }
        dispatch::REVOKE_OPERATOR => Action::Revoke(r.u64()?),
        _ => return Err(UNKNOWN_OPERATION),
    };
    r.finish()?;
    Ok((expected, action))
}

fn build_receipt(
    market: MarketId,
    revision: u64,
    request: RequestId,
    operation: Operation,
    config: u64,
    policy: Presence<PolicyDigest>,
) -> CodecResult<Receipt> {
    let mut bytes = [0; RECEIPT_MAX_BYTES];
    let mut w = Writer::new(&mut bytes);
    w.put(market.as_bytes())?;
    w.u64(revision)?;
    w.put(request.as_bytes())?;
    w.u16(operation.selector())?;
    w.u64(config)?;
    w.presence(&policy, |w, digest| w.put(digest.as_bytes()))?;
    let len = w.len();
    Ok(Receipt {
        bytes,
        len,
        digest: codec::result_digest(&bytes[..len])?,
    })
}

struct Effect {
    config: u64,
    policy: Presence<PolicyDigest>,
    aux: u64,
    subject: [u8; 32],
}

fn emit(
    operation: Operation,
    section: &PolicySection<'_>,
    clock_epoch: u64,
    request: &ReplayRequest,
    receipt: &Receipt,
    effect: &Effect,
    event_out: &mut [u8],
) -> CodecResult<usize> {
    let mut suffix = [0; EVENT_SUFFIX_BYTES];
    let mut w = Writer::new(&mut suffix);
    w.u8(section.header.lifecycle)?;
    w.u64(effect.config)?;
    w.u64(effect.aux)?;
    w.put(&effect.subject)?;
    codec::encode_event_frame(
        operation,
        &EventCommon {
            market: section.header.market_id,
            epoch: clock_epoch,
            config: Version::new(section.header.active_config_version)?,
            revision: section.header.state_revision,
            request: request.digest,
            result: receipt.digest,
        },
        &suffix,
        event_out,
    )
}

fn created_replay(
    owner: PrincipalId,
    treasury: Presence<PrincipalId>,
    request: &ReplayRequest,
    result: ResultDigest,
) -> CodecResult<ReplayTable> {
    let mut bytes = [0; 2 + 2 * crate::state::MAX_ACTOR_RECORD_BYTES];
    let mut w = Writer::new(&mut bytes);
    w.u16(if matches!(treasury, Presence::Present(_)) {
        2
    } else {
        1
    })?;
    w.u16(ActorSlot::OWNER.index())?;
    w.put(owner.as_bytes())?;
    w.u64(OWNER_AUTHORITY)?;
    w.boolean(true)?;
    w.u64(request.sequence)?;
    w.put(request.request_id.as_bytes())?;
    w.put(request.digest.as_bytes())?;
    w.put(result.as_bytes())?;
    w.u64(1)?;
    w.u64(request.expiry_height)?;
    if let Presence::Present(treasury) = treasury {
        w.u16(ActorSlot::TREASURY.index())?;
        w.put(treasury.as_bytes())?;
        w.u64(OWNER_AUTHORITY)?;
        w.boolean(false)?;
    }
    let n = w.len();
    decode_replay(&bytes[..n])
}

/// A create payload after field decoding, before binding checks.
#[derive(Clone, Copy)]
struct CreateRequest {
    owner: PrincipalId,
    asset: AssetId,
    rewards: AccountId,
    refund: AccountId,
    treasury: Presence<PrincipalId>,
    policy: TaskPolicyV1,
    metadata: MetadataDigest,
}
impl CreateRequest {
    fn parse(payload: &[u8]) -> CodecResult<Self> {
        let mut r = Reader::new(payload);
        let owner = PrincipalId::new(r.fixed()?)?;
        let asset = AssetId::new(r.fixed()?)?;
        let rewards = AccountId::new(r.fixed()?)?;
        let refund = AccountId::new(r.fixed()?)?;
        let treasury = r.presence(|r| PrincipalId::new(r.fixed()?))?;
        let policy = TaskPolicyV1::decode(r.take(TASK_POLICY_BYTES)?)?;
        let metadata = MetadataDigest::new(r.fixed()?)?;
        r.finish()?;
        Ok(Self {
            owner,
            asset,
            rewards,
            refund,
            treasury,
            policy,
            metadata,
        })
    }
    fn header(&self, ctx: &CallContext, market: MarketId) -> MarketHeader {
        MarketHeader {
            format_version: 1,
            market_id: market,
            deployment_chain_domain: ctx.chain,
            program_id: ctx.program,
            owner_principal: self.owner,
            funding_asset: self.asset,
            rewards_account: self.rewards,
            refund_recipient_account: self.refund,
            treasury_principal: self.treasury,
            origin_height: ctx.height,
            lifecycle: REGISTERED,
            state_revision: 1,
            highest_config_version: 1,
            active_config_version: 1,
            activation_epoch: 0,
            activation_scheduled: false,
            closure_requested_at: 0,
            close_phase: 0,
            close_cursor: 0,
            suspension_reason_digest: [0; 32],
            metadata_digest: self.metadata,
            closing_request_digest: [0; 32],
            reserved: [0; 8],
        }
    }
}

/// A create against an existing market: the owner's exact retry returns its retained
/// result; anything else is `F01_ALREADY_CREATED`.
fn retried_create<'a>(
    ctx: &CallContext,
    state: &SharedState<'a>,
    envelope: &ValidatedEnvelope<'_>,
    owner_slot: Version,
) -> CodecResult<Outcome<'a>> {
    let existing = PolicySection::decode(state.section(Section::PolicyLifecycle)?)?;
    if envelope.envelope.actor == existing.header.owner_principal {
        let request = ReplayRequest::from_envelope(ActorSlot::OWNER, owner_slot, envelope)?;
        if let Ok(ReplayDecision::AlreadyApplied(retained)) =
            state.control.replay.check(&request, ctx.height)
        {
            return Ok(Outcome::AlreadyApplied(retained));
        }
    }
    Err(F01_ALREADY_CREATED)
}

fn create<'a>(
    ctx: &CallContext,
    market: MarketId,
    current: Option<&SharedState<'a>>,
    envelope: &ValidatedEnvelope<'_>,
    section_out: &'a mut [u8],
    event_out: &mut [u8],
) -> CodecResult<Outcome<'a>> {
    let e = &envelope.envelope;
    let owner_slot = Version::new(OWNER_AUTHORITY)?;
    if let Some(state) = current {
        return retried_create(ctx, state, envelope, owner_slot);
    }
    let created = CreateRequest::parse(e.payload)?;
    let CreateRequest {
        owner,
        asset,
        rewards,
        treasury,
        policy,
        ..
    } = created;
    if owner != e.actor {
        return Err(F01_PRINCIPAL_MISMATCH);
    }
    if e.config != 1 {
        return Err(WRONG_CONFIG);
    }
    if policy.config_version != 1 {
        return Err(F01_VERSION_MISMATCH);
    }
    if rewards != derive_rewards_account(ctx.program, asset)? {
        return Err(F01_ACCOUNT_BINDING_MISSING);
    }
    let digest = policy.digest()?;
    let initial = PolicyHistoryHeader {
        config_version: 1,
        digest,
        effective_epoch: 0,
        disposition: ACTIVATED,
    };
    let section = PolicySection {
        header: created.header(ctx, market),
        operator: Presence::Absent,
        current: policy,
        pending: Presence::Absent,
        recent: [initial; MAX_RECENT_POLICY_HEADERS],
        recent_len: 1,
        history_root: initial_policy_history_root()?,
        task_region: &EMPTY_TASK_REGION,
    };
    let request = ReplayRequest::from_envelope(ActorSlot::OWNER, owner_slot, envelope)?;
    let mut probe = ReplayTable::new();
    probe.bind(ActorSlot::OWNER, owner, owner_slot)?;
    if probe.check(&request, ctx.height)? != ReplayDecision::Apply {
        return Err(NON_CANONICAL);
    }
    let n = section.encode(section_out)?;
    let frozen: &'a [u8] = section_out;
    let receipt = build_receipt(
        market,
        1,
        e.request,
        e.operation,
        1,
        Presence::Present(digest),
    )?;
    let state = SharedState {
        revision: 1,
        feature_sections: [&frozen[..n], &[], &[], &[], &[]],
        control: Control {
            replay: created_replay(owner, treasury, &request, receipt.digest)?,
            feature_bytes: &[],
        },
    };
    check_f01_capacity(n, state.encoded_len()?)?;
    let effect = Effect {
        config: 1,
        policy: Presence::Present(digest),
        aux: ctx.height,
        subject: digest.bytes(),
    };
    let event_len = emit(
        e.operation,
        &section,
        0,
        &request,
        &receipt,
        &effect,
        event_out,
    )?;
    Ok(Outcome::Applied {
        state,
        receipt,
        event_len,
    })
}

/// Applies one authenticated F01 administration request to the committed shared state.
/// Nothing in `current` changes; the caller commits the returned state, receipt and event
/// together or not at all.
///
/// # Errors
/// Returns `UNAUTHORIZED`, `WRONG_DOMAIN`, `WRONG_PROGRAM`, `WRONG_MARKET`, or `EXPIRED` when the envelope does not match the call context; `NOT_FOUND` when a non-create request has no state; `NON_CANONICAL` when the stored section or replay record is inconsistent; `WRONG_CONFIG` when the envelope config is not active; `UNAUTHORIZED` or `REVOKED` when the actor lacks authority; `F01_STALE_REVISION` when the expected revision is stale; `ARITHMETIC` on counter overflow; propagates payload-parse, create, replay, transition, capacity, and event refusals.
pub fn apply<'a>(
    ctx: &CallContext,
    current: Option<&SharedState<'a>>,
    envelope: &ValidatedEnvelope<'_>,
    section_out: &'a mut [u8],
    event_out: &mut [u8],
) -> CodecResult<Outcome<'a>> {
    let e = &envelope.envelope;
    codec::compare_native_principal(e, ctx.principal)?;
    let market = derive_market(ctx.chain, ctx.program)?;
    e.check_domain(ctx.chain, ctx.program, market)?;
    e.check_expiry(ctx.height)?;
    if e.operation == dispatch::CREATE {
        return create(ctx, market, current, envelope, section_out, event_out);
    }
    let state = current.ok_or(NOT_FOUND)?;
    let section = PolicySection::decode(state.section(Section::PolicyLifecycle)?)?;
    if section.header.market_id != market {
        return Err(WRONG_MARKET);
    }
    if section.header.state_revision != state.revision {
        return Err(NON_CANONICAL);
    }
    if e.config != section.header.active_config_version {
        return Err(WRONG_CONFIG);
    }
    let (expected, action) = parse(e.operation, e.payload)?;
    let (slot, authority) = authorize(e.actor, &section, &action)?;
    let request = ReplayRequest::from_envelope(slot, Version::new(authority)?, envelope)?;
    if let ReplayDecision::AlreadyApplied(retained) =
        state.control.replay.check(&request, ctx.height)?
    {
        return Ok(Outcome::AlreadyApplied(retained));
    }
    if expected != section.header.state_revision {
        return Err(F01_STALE_REVISION);
    }
    let clock = market_clock(section.header.origin_height, ctx.height)?;
    let earliest = clock.epoch.checked_add(1).ok_or(ARITHMETIC)?;
    let mut next = section;
    next.header.state_revision = section
        .header
        .state_revision
        .checked_add(1)
        .ok_or(ARITHMETIC)?;
    let mut effect = Effect {
        config: section.header.active_config_version,
        policy: Presence::Absent,
        aux: 0,
        subject: [0; 32],
    };
    let operator_change = transition(&action, &section, &mut next, &mut effect, e.actor, earliest)?;
    let n = next.encode(section_out)?;
    let frozen: &'a [u8] = section_out;
    let receipt = build_receipt(
        market,
        next.header.state_revision,
        e.request,
        e.operation,
        effect.config,
        effect.policy,
    )?;
    let mut feature_sections = state.feature_sections;
    feature_sections[Section::PolicyLifecycle.index()] = &frozen[..n];
    let mut candidate = SharedState {
        revision: state.revision,
        feature_sections,
        control: state.control.clone(),
    };
    if let Some((principal, grant)) = operator_change {
        bind_operator(&mut candidate.control.replay, principal, grant)?;
    }
    if candidate.record_success(&request, ctx.height, receipt.digest)? != ReplayDecision::Apply
        || candidate.revision != next.header.state_revision
    {
        return Err(NON_CANONICAL);
    }
    check_f01_capacity(n, candidate.encoded_len()?)?;
    let event_len = emit(
        e.operation,
        &next,
        clock.epoch,
        &request,
        &receipt,
        &effect,
        event_out,
    )?;
    Ok(Outcome::Applied {
        state: candidate,
        receipt,
        event_len,
    })
}

/// The replay slot and authority version of an owner or operator request.
fn authorize(
    actor: PrincipalId,
    section: &PolicySection<'_>,
    action: &Action,
) -> CodecResult<(ActorSlot, u64)> {
    if actor == section.header.owner_principal {
        if matches!(action, Action::Suspend(_, v) | Action::Metadata(_, v) if *v != 0) {
            return Err(UNAUTHORIZED);
        }
        return Ok((ActorSlot::OWNER, OWNER_AUTHORITY));
    }
    let (claimed, permission) = match action {
        Action::Suspend(_, v) => (*v, PERMIT_SUSPEND),
        Action::Metadata(_, v) => (*v, PERMIT_METADATA),
        _ => return Err(UNAUTHORIZED),
    };
    let grant = match section.operator {
        Presence::Present(grant) if grant.principal == actor => grant,
        _ => return Err(UNAUTHORIZED),
    };
    if grant.revoked {
        return Err(REVOKED);
    }
    if claimed != grant.sequence || grant.permissions & permission == 0 {
        return Err(UNAUTHORIZED);
    }
    Ok((ActorSlot::OPERATOR, grant.sequence))
}

/// Applies one authorized action to `next` and `effect`; returns the operator binding change.
fn transition(
    action: &Action,
    section: &PolicySection<'_>,
    next: &mut PolicySection<'_>,
    effect: &mut Effect,
    actor: PrincipalId,
    earliest: u64,
) -> CodecResult<Option<(PrincipalId, Version)>> {
    let lifecycle = section.header.lifecycle;
    match *action {
        Action::Stage(ref policy, effective_epoch) => {
            stage(
                policy,
                effective_epoch,
                section,
                next,
                effect,
                actor,
                earliest,
            )?;
        }
        Action::Cancel(version) => cancel(version, section, next, effect)?,
        Action::Schedule(epoch) => {
            match lifecycle {
                REGISTERED | SUSPENDED => {}
                WINDING_DOWN | CLOSED => return Err(F01_LIFECYCLE_CLOSED),
                _ => return Err(F01_WRONG_LIFECYCLE),
            }
            if section.header.activation_scheduled {
                return Err(CONFLICT);
            }
            if epoch < earliest {
                return Err(F01_ACTIVATION_TOO_EARLY);
            }
            next.header.activation_scheduled = true;
            next.header.activation_epoch = epoch;
            effect.aux = epoch;
        }
        Action::Suspend(reason, _) => {
            if reason == [0; 32] {
                return Err(F01_INVALID_REASON);
            }
            if lifecycle != ACTIVE {
                return Err(F01_WRONG_LIFECYCLE);
            }
            next.header.lifecycle = SUSPENDED;
            next.header.suspension_reason_digest = reason;
            next.header.activation_scheduled = false;
            next.header.activation_epoch = 0;
            effect.subject = reason;
        }
        Action::Metadata(digest, _) => {
            if lifecycle == CLOSED {
                return Err(F01_LIFECYCLE_CLOSED);
            }
            next.header.metadata_digest = digest;
            effect.subject = digest.bytes();
        }
        Action::Appoint(principal, permissions, expected_grant) => {
            return appoint(
                principal,
                permissions,
                expected_grant,
                section,
                next,
                effect,
            )
            .map(Some);
        }
        Action::Revoke(expected_grant) => revoke(expected_grant, section, next, effect)?,
    }
    Ok(None)
}

fn stage(
    policy: &TaskPolicyV1,
    effective_epoch: u64,
    section: &PolicySection<'_>,
    next: &mut PolicySection<'_>,
    effect: &mut Effect,
    actor: PrincipalId,
    earliest: u64,
) -> CodecResult<()> {
    if section.header.lifecycle >= WINDING_DOWN {
        return Err(F01_LIFECYCLE_CLOSED);
    }
    if matches!(section.pending, Presence::Present(_)) {
        return Err(F01_PENDING_POLICY_EXISTS);
    }
    if policy.config_version != next_config_version(section.header.highest_config_version)? {
        return Err(F01_VERSION_MISMATCH);
    }
    if effective_epoch < earliest {
        return Err(F01_ACTIVATION_TOO_EARLY);
    }
    let digest = policy.digest()?;
    next.pending = Presence::Present(PendingPolicy {
        policy: *policy,
        digest,
        effective_epoch,
        proposer: actor,
    });
    next.header.highest_config_version = policy.config_version;
    *effect = Effect {
        config: policy.config_version,
        policy: Presence::Present(digest),
        aux: effective_epoch,
        subject: digest.bytes(),
    };
    Ok(())
}

fn cancel(
    version: u64,
    section: &PolicySection<'_>,
    next: &mut PolicySection<'_>,
    effect: &mut Effect,
) -> CodecResult<()> {
    if section.header.lifecycle == CLOSED {
        return Err(F01_LIFECYCLE_CLOSED);
    }
    match section.pending {
        Presence::Present(pending) if pending.policy.config_version == version => {
            next.pending = Presence::Absent;
            next.push_history(PolicyHistoryHeader {
                config_version: version,
                digest: pending.digest,
                effective_epoch: pending.effective_epoch,
                disposition: CANCELLED,
            })?;
            *effect = Effect {
                config: version,
                policy: Presence::Present(pending.digest),
                aux: pending.effective_epoch,
                subject: pending.digest.bytes(),
            };
            Ok(())
        }
        _ if section.activated(version) => Err(F01_ALREADY_ACTIVATED),
        Presence::Present(_) => Err(F01_VERSION_MISMATCH),
        Presence::Absent => Err(F01_NO_PENDING_POLICY),
    }
}

fn appoint(
    principal: PrincipalId,
    permissions: u8,
    expected_grant: u64,
    section: &PolicySection<'_>,
    next: &mut PolicySection<'_>,
    effect: &mut Effect,
) -> CodecResult<(PrincipalId, Version)> {
    match section.header.lifecycle {
        REGISTERED | ACTIVE | SUSPENDED => {}
        CLOSED => return Err(F01_LIFECYCLE_CLOSED),
        _ => return Err(F01_WRONG_LIFECYCLE),
    }
    let previous = match section.operator {
        Presence::Present(grant) => grant.sequence,
        Presence::Absent => 0,
    };
    if expected_grant != previous {
        return Err(F01_VERSION_MISMATCH);
    }
    if principal == section.header.owner_principal
        || section.header.treasury_principal == Presence::Present(principal)
    {
        return Err(ROLE_CONFLICT);
    }
    let grant = OperatorGrant {
        principal,
        permissions,
        sequence: previous.checked_add(1).ok_or(ARITHMETIC)?,
        revoked: false,
        reserved: [0; 8],
    };
    grant.validate()?;
    next.operator = Presence::Present(grant);
    let change = (principal, Version::new(grant.sequence)?);
    effect.aux = grant.sequence;
    effect.subject = principal.bytes();
    Ok(change)
}

fn revoke(
    expected_grant: u64,
    section: &PolicySection<'_>,
    next: &mut PolicySection<'_>,
    effect: &mut Effect,
) -> CodecResult<()> {
    if section.header.lifecycle == CLOSED {
        return Err(F01_LIFECYCLE_CLOSED);
    }
    let grant = match section.operator {
        Presence::Present(grant) => grant,
        Presence::Absent => return Err(NOT_FOUND),
    };
    if expected_grant != grant.sequence {
        return Err(F01_VERSION_MISMATCH);
    }
    if grant.revoked {
        return Err(F01_GRANT_ALREADY_REVOKED);
    }
    next.operator = Presence::Present(OperatorGrant {
        revoked: true,
        ..grant
    });
    effect.aux = grant.sequence;
    effect.subject = grant.principal.bytes();
    Ok(())
}

/// Replaces the bound operator slot, or binds it on first appointment.
fn bind_operator(
    replay: &mut ReplayTable,
    principal: PrincipalId,
    grant: Version,
) -> CodecResult<()> {
    if replay.actor(ActorSlot::OPERATOR).is_some() {
        replay.replace_operator(principal, grant)
    } else {
        replay.bind(ActorSlot::OPERATOR, principal, grant)
    }
}
