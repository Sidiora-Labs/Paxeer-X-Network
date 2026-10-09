use crate::{
    codec::{self, Reader, StateFrame, Writer},
    errors::{
        CodecResult, ARITHMETIC, BAD_VERSION, CAPACITY, CONFLICT, EXPIRED, NON_CANONICAL,
        NOT_FOUND, REPLAY_CONFLICT, SEQUENCE_CONSUMED, SEQUENCE_GAP, UNAUTHORIZED, WRONG_EPOCH,
        WRONG_PHASE,
    },
    rewards::{decode_reward_state, RewardState, REWARD_STATE_BYTES},
    types::{PrincipalId, RequestDigest, RequestId, ResultDigest, Version},
    MAX_STATE_BYTES, SCHEMA_VERSION,
};

pub const ACTOR_SLOTS: usize = 43;
pub const CONTROL_FIXED_BYTES: usize = 10;
pub const REPLAY_RESULT_BYTES: usize = 120;
pub const MAX_ACTOR_RECORD_BYTES: usize = 163;

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum Section {
    PolicyLifecycle,
    IdentityRoster,
    CurrentReports,
    SettlementClaims,
    ReputationAdmission,
    Control,
}
impl Section {
    #[must_use]
    pub const fn index(self) -> usize {
        match self {
            Self::PolicyLifecycle => 0,
            Self::IdentityRoster => 1,
            Self::CurrentReports => 2,
            Self::SettlementClaims => 3,
            Self::ReputationAdmission => 4,
            Self::Control => 5,
        }
    }
    #[must_use]
    pub const fn payload_cap(self) -> usize {
        codec::STATE_SECTION_CAPS[self.index()]
            - codec::STATE_SECTION_HEADER_BYTES
            - if matches!(self, Self::Control) {
                codec::STATE_GLOBAL_BYTES
            } else {
                0
            }
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct ActorSlot(u16);
impl ActorSlot {
    pub const OWNER: Self = Self(0);
    pub const TREASURY: Self = Self(1);
    pub const OPERATOR: Self = Self(2);
    /// Slot of worker `index` (slots 3..35).
    ///
    /// # Errors
    /// Returns `CAPACITY` when `index` is 32 or more.
    pub fn worker(index: usize) -> CodecResult<Self> {
        if index >= 32 {
            return Err(CAPACITY);
        }
        Ok(Self(3 + u16::try_from(index).map_err(|_| ARITHMETIC)?))
    }
    /// Slot of evaluator `index` (slots 35..43).
    ///
    /// # Errors
    /// Returns `CAPACITY` when `index` is 8 or more.
    pub fn evaluator(index: usize) -> CodecResult<Self> {
        if index >= 8 {
            return Err(CAPACITY);
        }
        Ok(Self(35 + u16::try_from(index).map_err(|_| ARITHMETIC)?))
    }
    /// Slot at a raw table index.
    ///
    /// # Errors
    /// Returns `NON_CANONICAL` when `index` is not below `ACTOR_SLOTS`.
    pub fn from_index(index: u16) -> CodecResult<Self> {
        if usize::from(index) >= ACTOR_SLOTS {
            Err(NON_CANONICAL)
        } else {
            Ok(Self(index))
        }
    }
    #[must_use]
    pub const fn index(self) -> u16 {
        self.0
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct RetainedResult {
    pub sequence: u64,
    pub request_id: RequestId,
    pub request_digest: RequestDigest,
    pub result_digest: ResultDigest,
    pub applied_revision: u64,
    pub expiry_height: u64,
}
impl RetainedResult {
    fn validate(self) -> CodecResult<()> {
        if self.sequence == 0 || self.applied_revision == 0 || self.expiry_height == 0 {
            Err(NON_CANONICAL)
        } else {
            Ok(())
        }
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct ActorReplay {
    pub principal: PrincipalId,
    pub authority_version: Version,
    pub last: Option<RetainedResult>,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ReplayTable {
    actors: [Option<ActorReplay>; ACTOR_SLOTS],
}
impl Default for ReplayTable {
    fn default() -> Self {
        Self::new()
    }
}
impl ReplayTable {
    #[must_use]
    pub const fn new() -> Self {
        Self {
            actors: [None; ACTOR_SLOTS],
        }
    }
    #[must_use]
    pub fn actor(&self, slot: ActorSlot) -> Option<ActorReplay> {
        self.actors[usize::from(slot.0)]
    }
    /// Binds an empty slot to `principal` at `authority_version`.
    ///
    /// # Errors
    /// Returns `CONFLICT` when the slot is already bound.
    pub fn bind(
        &mut self,
        slot: ActorSlot,
        principal: PrincipalId,
        authority_version: Version,
    ) -> CodecResult<()> {
        if self.actor(slot).is_some() {
            return Err(CONFLICT);
        }
        self.actors[usize::from(slot.0)] = Some(ActorReplay {
            principal,
            authority_version,
            last: None,
        });
        Ok(())
    }
    /// Clears a bound worker or evaluator slot.
    ///
    /// # Errors
    /// Returns `UNAUTHORIZED` when `slot` is the owner, treasury or operator slot; `NOT_FOUND` when
    /// the slot is unbound.
    pub fn retire(&mut self, slot: ActorSlot) -> CodecResult<()> {
        if slot == ActorSlot::OWNER || slot == ActorSlot::TREASURY || slot == ActorSlot::OPERATOR {
            return Err(UNAUTHORIZED);
        }
        if self.actor(slot).is_none() {
            return Err(NOT_FOUND);
        }
        self.actors[usize::from(slot.0)] = None;
        Ok(())
    }
    /// Rebinds the operator slot under a newer grant, dropping its retained result.
    ///
    /// # Errors
    /// Returns `NOT_FOUND` when no operator is bound; `CONFLICT` when `grant` is not newer than the
    /// current authority version.
    pub fn replace_operator(&mut self, principal: PrincipalId, grant: Version) -> CodecResult<()> {
        let previous = self.actor(ActorSlot::OPERATOR).ok_or(NOT_FOUND)?;
        if grant <= previous.authority_version {
            return Err(CONFLICT);
        }
        self.actors[2] = Some(ActorReplay {
            principal,
            authority_version: grant,
            last: None,
        });
        Ok(())
    }
    /// Encoded byte length of the table.
    ///
    /// # Errors
    /// Returns `NON_CANONICAL` when a retained result has a zero sequence, applied revision or
    /// expiry; `ARITHMETIC` when the length overflows.
    pub fn encoded_len(&self) -> CodecResult<usize> {
        let mut n = 2usize;
        for actor in self.actors.iter().flatten() {
            if let Some(last) = actor.last {
                last.validate()?;
            }
            n = n
                .checked_add(
                    43 + if actor.last.is_some() {
                        REPLAY_RESULT_BYTES
                    } else {
                        0
                    },
                )
                .ok_or(ARITHMETIC)?;
        }
        Ok(n)
    }
    /// Classifies `request` against the slot's retained result without mutating.
    ///
    /// # Errors
    /// Propagates `ReplayRequest::validate` refusals (`NON_CANONICAL`, `EXPIRED`); returns
    /// `NOT_FOUND` when the slot is unbound, `UNAUTHORIZED` when the principal or authority version
    /// differs, `REPLAY_CONFLICT` when the retained sequence is reused by a different request,
    /// `SEQUENCE_CONSUMED` when the sequence is behind, `SEQUENCE_GAP` when it skips ahead,
    /// `ARITHMETIC` when the next sequence overflows.
    pub fn check(&self, request: &ReplayRequest, height: u64) -> CodecResult<ReplayDecision> {
        request.validate(height)?;
        let actor = self.actor(request.slot).ok_or(NOT_FOUND)?;
        if actor.principal != request.principal
            || actor.authority_version != request.authority_version
        {
            return Err(UNAUTHORIZED);
        }
        let previous = actor.last.map_or(0, |last| last.sequence);
        if let Some(last) = actor.last {
            if request.sequence == last.sequence {
                if request.request_id != last.request_id
                    || request.digest != last.request_digest
                    || request.expiry_height != last.expiry_height
                {
                    return Err(REPLAY_CONFLICT);
                }
                return Ok(ReplayDecision::AlreadyApplied(last));
            }
        }
        if request.sequence <= previous {
            return Err(SEQUENCE_CONSUMED);
        }
        let next = previous.checked_add(1).ok_or(ARITHMETIC)?;
        if request.sequence != next {
            return Err(SEQUENCE_GAP);
        }
        Ok(ReplayDecision::Apply)
    }
    /// Checks `request` and, when new, retains `result` at the next revision.
    ///
    /// # Errors
    /// Propagates `check` refusals; returns `NON_CANONICAL` when `revision` is zero or behind the
    /// retained applied revision, `ARITHMETIC` when the revision overflows.
    pub fn record_success(
        &mut self,
        request: &ReplayRequest,
        height: u64,
        revision: &mut u64,
        result: ResultDigest,
    ) -> CodecResult<ReplayDecision> {
        let decision = self.check(request, height)?;
        if matches!(decision, ReplayDecision::AlreadyApplied(_)) {
            return Ok(decision);
        }
        if *revision == 0 {
            return Err(NON_CANONICAL);
        }
        let next_revision = revision.checked_add(1).ok_or(ARITHMETIC)?;
        let actor = self.actor(request.slot).ok_or(NOT_FOUND)?;
        if actor
            .last
            .is_some_and(|last| last.applied_revision > *revision)
        {
            return Err(NON_CANONICAL);
        }
        let retained = RetainedResult {
            sequence: request.sequence,
            request_id: request.request_id,
            request_digest: request.digest,
            result_digest: result,
            applied_revision: next_revision,
            expiry_height: request.expiry_height,
        };
        self.actors[usize::from(request.slot.0)] = Some(ActorReplay {
            last: Some(retained),
            ..actor
        });
        *revision = next_revision;
        Ok(ReplayDecision::Apply)
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct ReplayRequest {
    pub slot: ActorSlot,
    pub principal: PrincipalId,
    pub authority_version: Version,
    pub sequence: u64,
    pub request_id: RequestId,
    pub digest: RequestDigest,
    pub expiry_height: u64,
}
impl ReplayRequest {
    /// Replay request for a role-sequenced validated envelope.
    ///
    /// # Errors
    /// Returns `NON_CANONICAL` when the operation is not role-sequenced or the request digest is
    /// zero; propagates the request-digest hashing refusals.
    pub fn from_envelope(
        slot: ActorSlot,
        authority_version: Version,
        envelope: &codec::ValidatedEnvelope<'_>,
    ) -> CodecResult<Self> {
        if envelope.envelope.operation.metadata().sequence != crate::dispatch::SequencePolicy::Role
        {
            return Err(NON_CANONICAL);
        }
        Ok(Self {
            slot,
            principal: envelope.envelope.actor,
            authority_version,
            sequence: envelope.envelope.sequence,
            request_id: envelope.envelope.request,
            digest: envelope.request_digest()?,
            expiry_height: envelope.envelope.expiry,
        })
    }
    /// Checks the request is canonical and unexpired at `height`.
    ///
    /// # Errors
    /// Returns `NON_CANONICAL` when the sequence or expiry is zero; `EXPIRED` when `height` is at
    /// or past the expiry.
    pub fn validate(&self, height: u64) -> CodecResult<()> {
        if self.sequence == 0 || self.expiry_height == 0 {
            return Err(NON_CANONICAL);
        }
        if height >= self.expiry_height {
            return Err(EXPIRED);
        }
        Ok(())
    }
}
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum ReplayDecision {
    Apply,
    AlreadyApplied(RetainedResult),
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct HeightWindow {
    pub start: u64,
    pub end: u64,
}
impl HeightWindow {
    /// Window `[start_offset, end_offset)` inside 128-block `epoch` from `origin`.
    ///
    /// # Errors
    /// Returns `NON_CANONICAL` when the offsets are not an ordered window within 128 blocks;
    /// `ARITHMETIC` when a window height overflows.
    pub fn epoch(origin: u64, epoch: u64, start_offset: u64, end_offset: u64) -> CodecResult<Self> {
        if start_offset >= end_offset || end_offset > 128 {
            return Err(NON_CANONICAL);
        }
        let start = epoch
            .checked_mul(128)
            .and_then(|n| origin.checked_add(n))
            .ok_or(ARITHMETIC)?;
        Ok(Self {
            start: start.checked_add(start_offset).ok_or(ARITHMETIC)?,
            end: start.checked_add(end_offset).ok_or(ARITHMETIC)?,
        })
    }
    /// Checks `height` lies inside the window.
    ///
    /// # Errors
    /// Returns `NON_CANONICAL` when the window is empty; `WRONG_PHASE` when `height` is outside it.
    pub fn check(self, height: u64) -> CodecResult<()> {
        if self.start >= self.end {
            return Err(NON_CANONICAL);
        }
        if height < self.start || height >= self.end {
            Err(WRONG_PHASE)
        } else {
            Ok(())
        }
    }
}

/// Encodes the replay table into `out`, returning the bytes written.
///
/// # Errors
/// Propagates `ReplayTable::encoded_len` refusals; returns `CAPACITY` when `out` is shorter than
/// the encoding.
pub fn encode_replay(table: &ReplayTable, out: &mut [u8]) -> CodecResult<usize> {
    let n = table.encoded_len()?;
    if out.len() < n {
        return Err(CAPACITY);
    }
    let mut w = Writer::new(out);
    let count = table.actors.iter().filter(|actor| actor.is_some()).count();
    w.u16(u16::try_from(count).map_err(|_| ARITHMETIC)?)?;
    for (slot, actor) in table.actors.iter().enumerate() {
        if let Some(actor) = actor {
            w.u16(u16::try_from(slot).map_err(|_| ARITHMETIC)?)?;
            w.put(actor.principal.as_bytes())?;
            w.u64(actor.authority_version.get())?;
            w.boolean(actor.last.is_some())?;
            if let Some(last) = actor.last {
                w.u64(last.sequence)?;
                w.put(last.request_id.as_bytes())?;
                w.put(last.request_digest.as_bytes())?;
                w.put(last.result_digest.as_bytes())?;
                w.u64(last.applied_revision)?;
                w.u64(last.expiry_height)?;
            }
        }
    }
    Ok(w.len())
}
fn read_replay(r: &mut Reader<'_>) -> CodecResult<ReplayTable> {
    let count = usize::from(r.u16()?);
    if count > ACTOR_SLOTS {
        return Err(CAPACITY);
    }
    let mut table = ReplayTable::new();
    let mut previous = None;
    for _ in 0..count {
        let slot = ActorSlot::from_index(r.u16()?)?;
        if previous.is_some_and(|p| p >= slot.0) {
            return Err(NON_CANONICAL);
        }
        previous = Some(slot.0);
        let principal = PrincipalId::new(r.fixed()?)?;
        let authority_version = Version::new(r.u64()?)?;
        let last = if r.boolean()? {
            let last = RetainedResult {
                sequence: r.u64()?,
                request_id: RequestId::new(r.fixed()?)?,
                request_digest: RequestDigest::new(r.fixed()?)?,
                result_digest: ResultDigest::new(r.fixed()?)?,
                applied_revision: r.u64()?,
                expiry_height: r.u64()?,
            };
            last.validate()?;
            Some(last)
        } else {
            None
        };
        table.actors[usize::from(slot.0)] = Some(ActorReplay {
            principal,
            authority_version,
            last,
        });
    }
    Ok(table)
}
/// Strictly decodes a complete replay table.
///
/// # Errors
/// Returns `CAPACITY` when the input or actor count exceeds the table bound; `NON_CANONICAL` for
/// out-of-range or unordered slots, zero identities or retained fields, invalid booleans,
/// truncation or trailing bytes.
pub fn decode_replay(input: &[u8]) -> CodecResult<ReplayTable> {
    if input.len() > 2 + ACTOR_SLOTS * MAX_ACTOR_RECORD_BYTES {
        return Err(CAPACITY);
    }
    let mut r = Reader::new(input);
    let table = read_replay(&mut r)?;
    r.finish()?;
    Ok(table)
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Control<'a> {
    pub replay: ReplayTable,
    pub feature_bytes: &'a [u8],
}
impl Control<'_> {
    /// Encoded byte length of the control payload.
    ///
    /// # Errors
    /// Propagates `ReplayTable::encoded_len` refusals; returns `CAPACITY` when the payload exceeds
    /// the control section cap, `ARITHMETIC` when the length overflows.
    pub fn encoded_len(&self) -> CodecResult<usize> {
        let n = 8usize
            .checked_add(self.replay.encoded_len()?)
            .and_then(|n| n.checked_add(self.feature_bytes.len()))
            .ok_or(ARITHMETIC)?;
        if n > Section::Control.payload_cap() {
            Err(CAPACITY)
        } else {
            Ok(n)
        }
    }
}
/// Encodes the control payload into `out`, returning the bytes written.
///
/// # Errors
/// Propagates `Control::encoded_len` refusals; returns `CAPACITY` when `out` is shorter than the
/// encoding.
pub fn encode_control(control: &Control<'_>, out: &mut [u8]) -> CodecResult<usize> {
    let n = control.encoded_len()?;
    if out.len() < n {
        return Err(CAPACITY);
    }
    out[..2].copy_from_slice(&SCHEMA_VERSION.to_be_bytes());
    out[2..4].fill(0);
    let replay_len = encode_replay(&control.replay, &mut out[4..])?;
    let mut w = Writer::new(&mut out[4 + replay_len..n]);
    w.bytes(control.feature_bytes, Section::Control.payload_cap())?;
    Ok(n)
}
/// Strictly decodes a control payload.
///
/// # Errors
/// Returns `CAPACITY` when the input, actor count or feature bytes exceed their caps; `BAD_VERSION`
/// for another schema version; `NON_CANONICAL` for nonzero reserved bytes, a malformed replay
/// table, truncation or trailing bytes.
pub fn decode_control(input: &[u8]) -> CodecResult<Control<'_>> {
    if input.len() > Section::Control.payload_cap() {
        return Err(CAPACITY);
    }
    let mut r = Reader::new(input);
    if r.u16()? != SCHEMA_VERSION {
        return Err(BAD_VERSION);
    }
    r.reserved(2)?;
    let replay = read_replay(&mut r)?;
    let feature_bytes = r.bytes(Section::Control.payload_cap())?;
    r.finish()?;
    let control = Control {
        replay,
        feature_bytes,
    };
    control.encoded_len()?;
    Ok(control)
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct SharedState<'a> {
    pub revision: u64,
    pub feature_sections: [&'a [u8]; 5],
    pub control: Control<'a>,
}
impl SharedState<'_> {
    /// Records a successful role request; the state is unchanged on any refusal.
    ///
    /// # Errors
    /// Propagates `SharedState::encoded_len` refusals for the current and candidate state and the
    /// replay table's `check`/`record_success` refusals.
    pub fn record_success(
        &mut self,
        request: &ReplayRequest,
        height: u64,
        result: ResultDigest,
    ) -> CodecResult<ReplayDecision> {
        self.encoded_len()?;
        let decision = self.control.replay.check(request, height)?;
        if matches!(decision, ReplayDecision::AlreadyApplied(_)) {
            return Ok(decision);
        }
        let mut candidate = self.clone();
        candidate.control.replay.record_success(
            request,
            height,
            &mut candidate.revision,
            result,
        )?;
        candidate.encoded_len()?;
        *self = candidate;
        Ok(ReplayDecision::Apply)
    }
    /// Encoded byte length of the full state frame.
    ///
    /// # Errors
    /// Returns `NON_CANONICAL` when the revision is zero or a retained result was applied after it;
    /// `CAPACITY` when a section or the whole state exceeds its cap; `ARITHMETIC` on overflow;
    /// propagates `Control::encoded_len` refusals.
    pub fn encoded_len(&self) -> CodecResult<usize> {
        if self.revision == 0 {
            return Err(NON_CANONICAL);
        }
        let control_len = self.control.encoded_len()?;
        for actor in self.control.replay.actors.iter().flatten() {
            if actor
                .last
                .is_some_and(|last| last.applied_revision > self.revision)
            {
                return Err(NON_CANONICAL);
            }
        }
        let mut total = codec::STATE_FRAMING_BYTES
            .checked_add(control_len)
            .ok_or(ARITHMETIC)?;
        for (i, bytes) in self.feature_sections.iter().enumerate() {
            if bytes.len() > codec::STATE_SECTION_CAPS[i] - codec::STATE_SECTION_HEADER_BYTES {
                return Err(CAPACITY);
            }
            total = total.checked_add(bytes.len()).ok_or(ARITHMETIC)?;
        }
        if total > MAX_STATE_BYTES {
            Err(CAPACITY)
        } else {
            Ok(total)
        }
    }
    /// Bytes of a feature section.
    ///
    /// # Errors
    /// Returns `NON_CANONICAL` for `Section::Control`, which is not a feature section.
    pub fn section(&self, section: Section) -> CodecResult<&[u8]> {
        self.feature_sections
            .get(section.index())
            .copied()
            .ok_or(NON_CANONICAL)
    }
    /// Copy of the state with one feature section replaced.
    ///
    /// # Errors
    /// Returns `NON_CANONICAL` for `Section::Control`; propagates `SharedState::encoded_len`
    /// refusals for the result.
    pub fn replace_section<'a>(
        &'a self,
        section: Section,
        bytes: &'a [u8],
    ) -> CodecResult<SharedState<'a>> {
        if section == Section::Control {
            return Err(NON_CANONICAL);
        }
        let mut next = SharedState {
            revision: self.revision,
            feature_sections: self.feature_sections,
            control: self.control.clone(),
        };
        next.feature_sections[section.index()] = bytes;
        next.encoded_len()?;
        Ok(next)
    }
}
/// Encodes the state frame into `out`, using `control_scratch` for the control payload.
///
/// # Errors
/// Propagates `SharedState::encoded_len` refusals; returns `CAPACITY` when `out` or
/// `control_scratch` is too short; propagates the control and state-frame encoder refusals.
pub fn encode_shared_state(
    state: &SharedState<'_>,
    out: &mut [u8],
    control_scratch: &mut [u8],
) -> CodecResult<usize> {
    let n = state.encoded_len()?;
    if out.len() < n || control_scratch.len() < state.control.encoded_len()? {
        return Err(CAPACITY);
    }
    let control_len = encode_control(&state.control, control_scratch)?;
    codec::encode_state(
        &StateFrame {
            revision: state.revision,
            sections: [
                state.feature_sections[0],
                state.feature_sections[1],
                state.feature_sections[2],
                state.feature_sections[3],
                state.feature_sections[4],
                &control_scratch[..control_len],
            ],
        },
        out,
    )
}
/// The validated F06 reward state at the head of the settlement section of `state`. `known` is
/// a view an earlier step of the same call already validated; it is reused only when it borrows
/// exactly these bytes, so the result equals a fresh `decode_reward_state`.
///
/// # Errors
/// Returns `WRONG_EPOCH` when the section is shorter than the reward state; propagates
/// `decode_reward_state` refusals.
pub fn settlement_rewards<'a>(
    state: &SharedState<'a>,
    known: Option<RewardState<'a>>,
) -> CodecResult<RewardState<'a>> {
    let bytes = state.feature_sections[Section::SettlementClaims.index()]
        .get(..REWARD_STATE_BYTES)
        .ok_or(WRONG_EPOCH)?;
    match known {
        Some(rewards) if core::ptr::eq(rewards.bytes(), bytes) => Ok(rewards),
        _ => decode_reward_state(bytes),
    }
}
/// Strictly decodes a state frame and its control payload.
///
/// # Errors
/// Propagates the state-frame and control decoder refusals and `SharedState::encoded_len` refusals.
pub fn decode_shared_state(input: &[u8]) -> CodecResult<SharedState<'_>> {
    let frame = codec::decode_state(input)?;
    let state = SharedState {
        revision: frame.revision,
        feature_sections: [
            frame.sections[0],
            frame.sections[1],
            frame.sections[2],
            frame.sections[3],
            frame.sections[4],
        ],
        control: decode_control(frame.sections[5])?,
    };
    state.encoded_len()?;
    Ok(state)
}
