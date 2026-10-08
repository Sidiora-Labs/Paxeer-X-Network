use crate::{
    codec::{self, Reader, StateFrame, Writer},
    errors::*,
    types::*,
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
    pub fn worker(index: usize) -> CodecResult<Self> {
        if index >= 32 {
            return Err(CAPACITY);
        }
        Ok(Self(3 + u16::try_from(index).map_err(|_| ARITHMETIC)?))
    }
    pub fn evaluator(index: usize) -> CodecResult<Self> {
        if index >= 8 {
            return Err(CAPACITY);
        }
        Ok(Self(35 + u16::try_from(index).map_err(|_| ARITHMETIC)?))
    }
    pub fn from_index(index: u16) -> CodecResult<Self> {
        if usize::from(index) >= ACTOR_SLOTS {
            Err(NON_CANONICAL)
        } else {
            Ok(Self(index))
        }
    }
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
    pub const fn new() -> Self {
        Self {
            actors: [None; ACTOR_SLOTS],
        }
    }
    pub fn actor(&self, slot: ActorSlot) -> Option<ActorReplay> {
        self.actors[usize::from(slot.0)]
    }
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
    pub fn section(&self, section: Section) -> CodecResult<&[u8]> {
        self.feature_sections
            .get(section.index())
            .copied()
            .ok_or(NON_CANONICAL)
    }
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
