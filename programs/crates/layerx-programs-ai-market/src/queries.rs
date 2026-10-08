//! F10 read-only selectors, exact snapshot binding and bounded off-chain page framing.
//! Selectors read one captured common state value; they never write, emit, pay or stage blobs.
//! Host ProgramRead facts and finality evidence are inputs; nothing here establishes them.
use crate::{
    codec::{self, ChunkRequest, ChunkResponse, ReadHeader, Writer},
    dispatch,
    errors::*,
    registry_ops::PolicySection,
    state::{self, Section},
    types::*,
    workers::{WorkerState, WorkerTable},
    MAX_CHUNK_BYTES, MAX_STATE_BYTES, SCHEMA_VERSION,
};
use sha2::{Digest, Sha256};

pub const FULL_CAPTURE_CHUNKS: usize = MAX_STATE_BYTES / MAX_CHUNK_BYTES;
pub const FINALIZED_RANK: u8 = 4;
pub const PAGE_MAX_ROWS: usize = 32;
pub const PAGE_MAX_BYTES: usize = 65_536;
pub const DEFAULT_LIMIT: u8 = 16;
pub const CURSOR_MAX_BYTES: usize = 1_024;
pub const CURSOR_LIFETIME_MS: u64 = 900_000;
const CURSOR_PAYLOAD_BYTES: usize = 144;
pub const CURSOR_TOKEN_BYTES: usize = 2 * (CURSOR_PAYLOAD_BYTES + 32);
pub const BINDING_MAX_BYTES: usize =
    2 + 32 * 3 + 16 + 32 * 2 + 8 + 32 + 9 + 8 + 32 + 33 + 32 + 33 + 1 + 8 + 8;

/// Off-chain view failures keep their own names, separate from on-chain application codes.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum QueryError {
    Application(ApplicationError),
    BindingMismatch,
    FinalityUnavailable,
    SnapshotConflict,
    IntegrityFailure,
    CursorExpired,
    CursorMismatch,
    ResponseTooLarge,
}
impl From<ApplicationError> for QueryError {
    fn from(error: ApplicationError) -> Self {
        Self::Application(error)
    }
}
pub type QueryResult<T> = Result<T, QueryError>;

fn policy_section(state_bytes: &[u8]) -> CodecResult<PolicySection<'_>> {
    let frame = codec::decode_state(state_bytes)?;
    if frame.sections[Section::PolicyLifecycle.index()].is_empty() {
        return Err(NOT_FOUND);
    }
    PolicySection::decode(frame.sections[Section::PolicyLifecycle.index()])
}

/// READ_HEADER over the exact captured state. Epoch/roster stay absent until a frozen-roster
/// producer records them in state; absence is never reported as epoch 0.
pub fn read_header(
    state_bytes: &[u8],
    chain: ChainDomain,
    program: ProgramId,
) -> CodecResult<ReadHeader> {
    let shared = state::decode_shared_state(state_bytes)?;
    let section = policy_section(state_bytes)?;
    let h = section.header;
    if h.deployment_chain_domain != chain {
        return Err(WRONG_DOMAIN);
    }
    if h.program_id != program {
        return Err(WRONG_PROGRAM);
    }
    Ok(ReadHeader {
        revision: shared.revision,
        digest: codec::state_digest(state_bytes)?,
        total_bytes: u32::try_from(state_bytes.len()).map_err(|_| ARITHMETIC)?,
        chain,
        program,
        market: h.market_id,
        epoch: Presence::Absent,
        config: Version::new(h.active_config_version)?,
        roster: Presence::Absent,
    })
}

pub fn policy_digest(state_bytes: &[u8]) -> CodecResult<PolicyDigest> {
    policy_section(state_bytes)?.current.digest()
}

/// READ_STATE_CHUNK: exact 46-byte payload, pinned revision/digest after discovery, chunk never
/// crosses the state end. Returns the encoded chunk response body length written to `out`.
pub fn read_state_chunk(state_bytes: &[u8], payload: &[u8], out: &mut [u8]) -> CodecResult<usize> {
    dispatch::READ_STATE_CHUNK.validate_payload_length(payload.len())?;
    let request = codec::decode_chunk_request(payload)?;
    let shared = state::decode_shared_state(state_bytes)?;
    let digest = codec::state_digest(state_bytes)?;
    if request.revision != 0
        && (request.revision != shared.revision || request.digest != Presence::Present(digest))
    {
        return Err(CONFLICT);
    }
    let total = state_bytes.len();
    let offset = usize::try_from(request.offset).map_err(|_| ARITHMETIC)?;
    if offset >= total {
        return Err(NON_CANONICAL);
    }
    let end = offset + usize::from(request.requested).min(total - offset);
    let response = ChunkResponse {
        revision: shared.revision,
        digest,
        total_bytes: u32::try_from(total).map_err(|_| ARITHMETIC)?,
        offset: request.offset,
        bytes: &state_bytes[offset..end],
    };
    codec::check_chunk_response(&request, &response)?;
    codec::encode_chunk_response(&response, out)
}

/// Wraps a read body in the common application result (<=16384 bytes).
pub fn frame_read_result(
    request: RequestDigest,
    revision: u64,
    body: &[u8],
    out: &mut [u8],
) -> CodecResult<usize> {
    let result =
        codec::ApplicationResult::success(codec::ResultStatus::Ok, request, revision, body)?;
    codec::encode_result(&result, out)
}

/// Outer ProgramRead facts returned with each chunk by the host read interface.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct ReadProof {
    pub chain: ChainDomain,
    pub program: ProgramId,
    pub native_state_root: Digest32,
    pub observed_sequence: u64,
    pub execution_height: u64,
    pub batch_id: Digest32,
}

/// Facts of one complete capture: every chunk proved the same root and state identity.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct CaptureFacts {
    pub proof: ReadProof,
    pub revision: u64,
    pub digest: StateDigest,
    pub total_bytes: u32,
    pub chunks: usize,
}

/// Whole-capture reassembly into a caller buffer. Any head change, gap or short nonfinal
/// chunk fails the whole capture; retry restarts from a new root.
pub struct StateCapture<'a> {
    buffer: &'a mut [u8],
    facts: Option<CaptureFacts>,
    next_offset: usize,
}
impl<'a> StateCapture<'a> {
    pub fn new(buffer: &'a mut [u8]) -> Self {
        Self {
            buffer,
            facts: None,
            next_offset: 0,
        }
    }
    pub fn accept(&mut self, proof: &ReadProof, body: &[u8]) -> QueryResult<()> {
        let chunk = codec::decode_chunk_response(body)?;
        let total = usize::try_from(chunk.total_bytes).map_err(|_| ARITHMETIC)?;
        match &mut self.facts {
            None => {
                if total > self.buffer.len() {
                    return Err(CAPACITY.into());
                }
                self.facts = Some(CaptureFacts {
                    proof: *proof,
                    revision: chunk.revision,
                    digest: chunk.digest,
                    total_bytes: chunk.total_bytes,
                    chunks: 0,
                });
            }
            Some(f) => {
                if f.proof != *proof
                    || f.revision != chunk.revision
                    || f.digest != chunk.digest
                    || f.total_bytes != chunk.total_bytes
                {
                    return Err(QueryError::SnapshotConflict);
                }
            }
        }
        let offset = usize::try_from(chunk.offset).map_err(|_| ARITHMETIC)?;
        let end = offset + chunk.bytes.len();
        if offset != self.next_offset || (chunk.bytes.len() != MAX_CHUNK_BYTES && end != total) {
            return Err(QueryError::IntegrityFailure);
        }
        self.buffer[offset..end].copy_from_slice(chunk.bytes);
        self.next_offset = end;
        if let Some(f) = &mut self.facts {
            f.chunks += 1;
        }
        Ok(())
    }
    pub fn finish(self) -> QueryResult<(&'a [u8], CaptureFacts)> {
        let facts = self.facts.ok_or(QueryError::IntegrityFailure)?;
        let total = usize::try_from(facts.total_bytes).map_err(|_| ARITHMETIC)?;
        if self.next_offset != total || facts.chunks != total.div_ceil(MAX_CHUNK_BYTES) {
            return Err(QueryError::IntegrityFailure);
        }
        let bytes = &self.buffer[..total];
        if codec::state_digest(bytes)? != facts.digest {
            return Err(QueryError::IntegrityFailure);
        }
        Ok((bytes, facts))
    }
}

/// Finality evidence supplied by the native checkpoint verifier for one root.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct FinalityEvidence {
    pub native_state_root: Digest32,
    pub checkpoint: Digest32,
    pub settlement: Presence<Digest32>,
    pub rank: u8,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct SnapshotBinding {
    pub chain: ChainDomain,
    pub program: ProgramId,
    pub market: MarketId,
    pub observed_sequence: u64,
    pub execution_height: u64,
    pub batch_id: Digest32,
    pub native_state_root: Digest32,
    pub revision: u64,
    pub state_digest: StateDigest,
    pub epoch: Presence<u64>,
    pub config: Version,
    pub policy: PolicyDigest,
    pub roster: Presence<RosterDigest>,
    pub checkpoint: Digest32,
    pub settlement: Presence<Digest32>,
    pub rank: u8,
    pub publication_time_ms: u64,
}

/// Binds a verified capture to its header and the finality evidence for the SAME root.
pub fn bind_snapshot(
    state_bytes: &[u8],
    facts: &CaptureFacts,
    finality: &FinalityEvidence,
    publication_time_ms: u64,
) -> QueryResult<SnapshotBinding> {
    let header = read_header(state_bytes, facts.proof.chain, facts.proof.program)?;
    if header.revision != facts.revision || header.digest != facts.digest {
        return Err(QueryError::BindingMismatch);
    }
    if finality.native_state_root != facts.proof.native_state_root {
        return Err(QueryError::BindingMismatch);
    }
    if finality.rank > FINALIZED_RANK {
        return Err(NON_CANONICAL.into());
    }
    Ok(SnapshotBinding {
        chain: header.chain,
        program: header.program,
        market: header.market,
        observed_sequence: facts.proof.observed_sequence,
        execution_height: facts.proof.execution_height,
        batch_id: facts.proof.batch_id,
        native_state_root: facts.proof.native_state_root,
        revision: header.revision,
        state_digest: header.digest,
        epoch: header.epoch,
        config: header.config,
        policy: policy_digest(state_bytes)?,
        roster: header.roster,
        checkpoint: finality.checkpoint,
        settlement: finality.settlement,
        rank: finality.rank,
        publication_time_ms,
    })
}

impl SnapshotBinding {
    fn write_prefix(&self, w: &mut Writer<'_>) -> CodecResult<()> {
        w.u16(SCHEMA_VERSION)?;
        w.put(self.chain.as_bytes())?;
        w.put(self.program.as_bytes())?;
        w.put(self.market.as_bytes())?;
        w.u64(self.observed_sequence)?;
        w.u64(self.execution_height)?;
        w.put(self.batch_id.as_bytes())?;
        w.put(self.native_state_root.as_bytes())?;
        w.u64(self.revision)?;
        w.put(self.state_digest.as_bytes())?;
        w.presence(&self.epoch, |w, e| w.u64(*e))?;
        w.u64(self.config.get())?;
        w.put(self.policy.as_bytes())?;
        w.presence(&self.roster, |w, r| w.put(r.as_bytes()))
    }
    pub fn encode(&self, out: &mut [u8]) -> CodecResult<usize> {
        if self.revision == 0 || self.rank > FINALIZED_RANK {
            return Err(NON_CANONICAL);
        }
        let mut w = Writer::new(out);
        self.write_prefix(&mut w)?;
        w.put(self.checkpoint.as_bytes())?;
        w.presence(&self.settlement, |w, s| w.put(s.as_bytes()))?;
        w.u8(self.rank)?;
        w.u64(self.publication_time_ms)?;
        w.put(&[0; 8])?;
        Ok(w.len())
    }
    /// Content identity excludes checkpoint/settlement evidence, rank and publication time.
    pub fn snapshot_id(&self) -> CodecResult<Digest32> {
        let mut b = [0u8; BINDING_MAX_BYTES];
        let mut w = Writer::new(&mut b);
        self.write_prefix(&mut w)?;
        let n = w.len();
        codec::domain_hash("PAXAI/view/v1", &b[..n])
    }
    /// Finalized endpoints and webhook publication require actual rank-4 evidence.
    pub fn require_finalized(&self) -> QueryResult<()> {
        if self.rank == FINALIZED_RANK {
            Ok(())
        } else {
            Err(QueryError::FinalityUnavailable)
        }
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
#[repr(u8)]
pub enum Availability {
    Available = 1,
    NotEnabled = 2,
    NotYetProduced = 3,
    ContentUnavailable = 4,
    UnsupportedVersion = 5,
}

/// F01..F10 order. Only the F01 policy/header and F02 worker declarations are projected here;
/// F06 rewards are not enabled and other producers have not yet produced view content.
pub fn feature_availability(state_bytes: &[u8]) -> CodecResult<[Availability; 10]> {
    let shared = state::decode_shared_state(state_bytes)?;
    let mut out = [Availability::NotYetProduced; 10];
    out[0] = match policy_section(state_bytes) {
        Ok(_) => Availability::Available,
        Err(NOT_FOUND) => Availability::ContentUnavailable,
        Err(e) => return Err(e),
    };
    WorkerTable::decode(shared.section(Section::IdentityRoster)?)?;
    out[1] = Availability::Available;
    out[5] = Availability::NotEnabled;
    out[9] = Availability::Available;
    Ok(out)
}

#[derive(Clone, Copy, Debug, Eq, PartialEq, Ord, PartialOrd)]
#[repr(u8)]
pub enum ParticipantKind {
    Worker = 1,
    Evaluator = 2,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
#[repr(u8)]
pub enum ScoreStatus {
    Present = 1,
    NoAdmissibleScore = 2,
    InsufficientCoverage = 3,
    NotProduced = 4,
    Unavailable = 5,
    Unsupported = 6,
}

/// Present carries an actual number (zero included); every other status carries none.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct ScoreField {
    epoch: Presence<u64>,
    status: ScoreStatus,
    ppm: Presence<u32>,
}
impl ScoreField {
    pub fn present(epoch: u64, ppm: u32) -> CodecResult<Self> {
        if ppm > 1_000_000 {
            return Err(NON_CANONICAL);
        }
        Ok(Self {
            epoch: Presence::Present(epoch),
            status: ScoreStatus::Present,
            ppm: Presence::Present(ppm),
        })
    }
    pub fn absent(epoch: Presence<u64>, status: ScoreStatus) -> CodecResult<Self> {
        if status == ScoreStatus::Present {
            return Err(NON_CANONICAL);
        }
        Ok(Self {
            epoch,
            status,
            ppm: Presence::Absent,
        })
    }
    pub const fn status(&self) -> ScoreStatus {
        self.status
    }
    pub const fn ppm(&self) -> Presence<u32> {
        self.ppm
    }
    pub const fn epoch(&self) -> Presence<u64> {
        self.epoch
    }
}

/// F06-backed entitlement. Only an F06 producer may supply Available values.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct RewardField {
    status: Availability,
    asset: Presence<AssetId>,
    earned: Presence<u128>,
    claimed: Presence<u128>,
}
impl RewardField {
    pub fn entitlement(asset: AssetId, earned: u128, claimed: u128) -> CodecResult<Self> {
        if claimed > earned {
            return Err(NON_CANONICAL);
        }
        Ok(Self {
            status: Availability::Available,
            asset: Presence::Present(asset),
            earned: Presence::Present(earned),
            claimed: Presence::Present(claimed),
        })
    }
    pub fn unavailable(status: Availability) -> CodecResult<Self> {
        if status == Availability::Available {
            return Err(NON_CANONICAL);
        }
        Ok(Self {
            status,
            asset: Presence::Absent,
            earned: Presence::Absent,
            claimed: Presence::Absent,
        })
    }
    pub const fn status(&self) -> Availability {
        self.status
    }
    pub const fn earned(&self) -> Presence<u128> {
        self.earned
    }
    pub const fn claimed(&self) -> Presence<u128> {
        self.claimed
    }
}

pub const ELIGIBLE_SERVING: u8 = 1;
pub const ELIGIBLE_NEW_WORK: u8 = 2;
/// Evaluator rows come from the frozen roster; no current evaluator record exists in state.
pub const IDENTITY_STATE_UNAVAILABLE: u8 = 0;

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct ParticipantRow {
    pub kind: ParticipantKind,
    pub id: [u8; 32],
    pub owner: PrincipalId,
    pub generation: u64,
    pub identity_state: u8,
    pub frozen_member: bool,
    pub frozen_generation: Presence<u64>,
    pub eligibility: u8,
    pub metadata: Presence<MetadataDigest>,
    pub metadata_revision: u64,
    pub score: ScoreField,
    pub reward: RewardField,
    pub history_status: Availability,
    pub history: Presence<Digest32>,
}
pub const ROW_MAX_BYTES: usize =
    1 + 32 + 32 + 8 + 1 + 1 + 9 + 1 + 33 + 8 + 9 + 1 + 5 + 1 + 33 + 17 + 17 + 1 + 33;
impl ParticipantRow {
    pub fn encode(&self, w: &mut Writer<'_>) -> CodecResult<()> {
        w.u8(self.kind as u8)?;
        w.put(&self.id)?;
        w.put(self.owner.as_bytes())?;
        w.u64(self.generation)?;
        w.u8(self.identity_state)?;
        w.boolean(self.frozen_member)?;
        w.presence(&self.frozen_generation, |w, g| w.u64(*g))?;
        w.u8(self.eligibility)?;
        w.presence(&self.metadata, |w, m| w.put(m.as_bytes()))?;
        w.u64(self.metadata_revision)?;
        w.presence(&self.score.epoch, |w, e| w.u64(*e))?;
        w.u8(self.score.status as u8)?;
        w.presence(&self.score.ppm, |w, s| w.u32(*s))?;
        w.u8(self.reward.status as u8)?;
        w.presence(&self.reward.asset, |w, a| w.put(a.as_bytes()))?;
        w.presence(&self.reward.earned, |w, v| w.u128(*v))?;
        w.presence(&self.reward.claimed, |w, v| w.u128(*v))?;
        w.u8(self.history_status as u8)?;
        w.presence(&self.history, |w, h| w.put(h.as_bytes()))
    }
    const fn active(&self) -> bool {
        self.eligibility & ELIGIBLE_SERVING != 0
    }
}

/// Worker rows from the F02 current table in the captured state, ascending WorkerId.
/// Scores and rewards stay explicitly unproduced/not-enabled until their producers exist.
pub fn worker_rows(
    state_bytes: &[u8],
    frozen: Option<&codec::RosterView<'_>>,
    out: &mut [ParticipantRow],
) -> CodecResult<usize> {
    let shared = state::decode_shared_state(state_bytes)?;
    let table = WorkerTable::decode(shared.section(Section::IdentityRoster)?)?;
    if out.len() < table.len() {
        return Err(CAPACITY);
    }
    for (slot, r) in out.iter_mut().zip(table.iter()) {
        let mut frozen_generation = Presence::Absent;
        if let Some(view) = frozen {
            for i in 0..view.worker_count() {
                let entry = view.worker(i)?;
                if entry.worker == r.worker {
                    frozen_generation = Presence::Present(entry.generation.get());
                }
            }
        }
        let eligibility = match r.state {
            WorkerState::Available => ELIGIBLE_SERVING | ELIGIBLE_NEW_WORK,
            WorkerState::Enrolled | WorkerState::Draining => ELIGIBLE_SERVING,
            _ => 0,
        };
        *slot = ParticipantRow {
            kind: ParticipantKind::Worker,
            id: r.worker.bytes(),
            owner: r.owner,
            generation: r.generation,
            identity_state: r.state as u8,
            frozen_member: frozen_generation != Presence::Absent,
            frozen_generation,
            eligibility,
            metadata: Presence::Present(r.metadata),
            metadata_revision: r.metadata_revision,
            score: ScoreField::absent(Presence::Absent, ScoreStatus::NotProduced)?,
            reward: RewardField::unavailable(Availability::NotEnabled)?,
            history_status: Availability::NotYetProduced,
            history: Presence::Absent,
        };
    }
    Ok(table.len())
}

/// Evaluator rows from an exact frozen roster, ascending EvaluatorId.
pub fn evaluator_rows(
    frozen: &codec::RosterView<'_>,
    out: &mut [ParticipantRow],
) -> CodecResult<usize> {
    let n = frozen.evaluator_count();
    if out.len() < n {
        return Err(CAPACITY);
    }
    for (i, slot) in out.iter_mut().enumerate().take(n) {
        let e = frozen.evaluator(i)?;
        *slot = ParticipantRow {
            kind: ParticipantKind::Evaluator,
            id: e.evaluator.bytes(),
            owner: e.owner,
            generation: e.grant.get(),
            identity_state: IDENTITY_STATE_UNAVAILABLE,
            frozen_member: true,
            frozen_generation: Presence::Present(e.grant.get()),
            eligibility: 0,
            metadata: Presence::Absent,
            metadata_revision: 0,
            score: ScoreField::absent(Presence::Absent, ScoreStatus::Unsupported)?,
            reward: RewardField::unavailable(Availability::NotEnabled)?,
            history_status: Availability::NotYetProduced,
            history: Presence::Absent,
        };
    }
    Ok(n)
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
#[repr(u8)]
pub enum KindFilter {
    All = 0,
    Worker = 1,
    Evaluator = 2,
}
impl KindFilter {
    pub fn from_u8(value: u8) -> CodecResult<Self> {
        Ok(match value {
            0 => Self::All,
            1 => Self::Worker,
            2 => Self::Evaluator,
            _ => return Err(NON_CANONICAL),
        })
    }
    fn admits(self, row: &ParticipantRow, active_only: bool) -> bool {
        let kind = match self {
            Self::All => true,
            Self::Worker => row.kind == ParticipantKind::Worker,
            Self::Evaluator => row.kind == ParticipantKind::Evaluator,
        };
        kind && (!active_only || row.active())
    }
}

/// Exactly 64 lower-case hexadecimal characters.
pub fn parse_hex32(text: &str) -> CodecResult<[u8; 32]> {
    let b = text.as_bytes();
    if b.len() != 64 {
        return Err(NON_CANONICAL);
    }
    let mut out = [0u8; 32];
    hex_decode(b, &mut out)?;
    Ok(out)
}
fn nibble(c: u8) -> CodecResult<u8> {
    match c {
        b'0'..=b'9' => Ok(c - b'0'),
        b'a'..=b'f' => Ok(c - b'a' + 10),
        _ => Err(NON_CANONICAL),
    }
}
fn hex_decode(text: &[u8], out: &mut [u8]) -> CodecResult<()> {
    if text.len() != out.len() * 2 {
        return Err(NON_CANONICAL);
    }
    for (o, pair) in out.iter_mut().zip(text.chunks_exact(2)) {
        *o = nibble(pair[0])? << 4 | nibble(pair[1])?;
    }
    Ok(())
}

/// Canonical decimal limit, default 16, range 1..=32.
pub fn parse_limit(text: Option<&str>) -> CodecResult<u8> {
    let limit = match text {
        None => return Ok(DEFAULT_LIMIT),
        Some(t) => codec::decimal_u64(t)?,
    };
    if limit == 0 || limit > PAGE_MAX_ROWS as u64 {
        return Err(NON_CANONICAL);
    }
    u8::try_from(limit).map_err(|_| ARITHMETIC)
}

/// Server key for cursor authentication; never placed in a snapshot.
#[derive(Clone, Copy)]
pub struct CursorKey<'k> {
    pub secret: &'k [u8; 32],
    pub generation: u32,
}
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct CursorScope {
    pub visibility: Digest32,
    pub market: MarketId,
    pub snapshot: Digest32,
    pub filter: KindFilter,
    pub active_only: bool,
}
fn filter_hash(scope: &CursorScope) -> CodecResult<Digest32> {
    codec::domain_hash(
        "PAXAI/view-filter/v1",
        &[scope.filter as u8, u8::from(scope.active_only)],
    )
}
fn hmac(secret: &[u8; 32], payload: &[u8]) -> [u8; 32] {
    let mut ipad = [0x36u8; 64];
    let mut opad = [0x5cu8; 64];
    for (i, k) in secret.iter().enumerate() {
        ipad[i] ^= k;
        opad[i] ^= k;
    }
    let inner: [u8; 32] = Sha256::new()
        .chain_update(ipad)
        .chain_update(payload)
        .finalize()
        .into();
    Sha256::new()
        .chain_update(opad)
        .chain_update(inner)
        .finalize()
        .into()
}
fn cursor_payload(
    key: &CursorKey<'_>,
    scope: &CursorScope,
    next_ordinal: u16,
    expiry_ms: u64,
) -> CodecResult<[u8; CURSOR_PAYLOAD_BYTES]> {
    let mut p = [0u8; CURSOR_PAYLOAD_BYTES];
    let mut w = Writer::new(&mut p);
    w.u16(SCHEMA_VERSION)?;
    w.put(scope.visibility.as_bytes())?;
    w.put(scope.market.as_bytes())?;
    w.put(scope.snapshot.as_bytes())?;
    w.put(filter_hash(scope)?.as_bytes())?;
    w.u16(next_ordinal)?;
    w.u64(expiry_ms)?;
    w.u32(key.generation)?;
    Ok(p)
}

/// Issues an opaque lower-case hex token: payload || HMAC-SHA256(secret, payload).
pub fn issue_cursor<'o>(
    key: &CursorKey<'_>,
    scope: &CursorScope,
    next_ordinal: u16,
    now_ms: u64,
    out: &'o mut [u8; CURSOR_TOKEN_BYTES],
) -> CodecResult<&'o str> {
    let expiry = now_ms.checked_add(CURSOR_LIFETIME_MS).ok_or(ARITHMETIC)?;
    let payload = cursor_payload(key, scope, next_ordinal, expiry)?;
    let tag = hmac(key.secret, &payload);
    const HEX: &[u8; 16] = b"0123456789abcdef";
    for (i, b) in payload.iter().chain(tag.iter()).enumerate() {
        out[2 * i] = HEX[usize::from(b >> 4)];
        out[2 * i + 1] = HEX[usize::from(b & 15)];
    }
    core::str::from_utf8(out).map_err(|_| NON_CANONICAL)
}

/// Authenticates a cursor before any row work; returns the next ordinal.
pub fn open_cursor(
    key: &CursorKey<'_>,
    scope: &CursorScope,
    token: &str,
    now_ms: u64,
    total_rows: usize,
) -> QueryResult<u16> {
    if token.len() > CURSOR_MAX_BYTES || token.len() != CURSOR_TOKEN_BYTES {
        return Err(NON_CANONICAL.into());
    }
    let mut raw = [0u8; CURSOR_PAYLOAD_BYTES + 32];
    hex_decode(token.as_bytes(), &mut raw)?;
    let (payload, tag) = raw.split_at(CURSOR_PAYLOAD_BYTES);
    let expected = hmac(key.secret, payload);
    if expected.iter().zip(tag).fold(0u8, |a, (x, y)| a | (x ^ y)) != 0 {
        return Err(QueryError::CursorMismatch);
    }
    let mut r = codec::Reader::new(payload);
    let next = {
        let schema = r.u16()?;
        let rest: [u8; 128] = r.fixed()?;
        let next = r.u16()?;
        let expiry = r.u64()?;
        let generation = r.u32()?;
        r.finish()?;
        let mine = cursor_payload(key, scope, next, expiry)?;
        if schema != SCHEMA_VERSION || generation != key.generation || rest[..] != mine[2..130] {
            return Err(QueryError::CursorMismatch);
        }
        if expiry <= now_ms {
            return Err(QueryError::CursorExpired);
        }
        next
    };
    if next == 0 || usize::from(next) >= total_rows {
        return Err(QueryError::CursorMismatch);
    }
    Ok(next)
}

/// Rows must be in kind-then-ID order. Returns rows written and the next ordinal, if any.
pub fn select_page(
    rows: &[ParticipantRow],
    filter: KindFilter,
    active_only: bool,
    start: u16,
    limit: u8,
    out: &mut [ParticipantRow; PAGE_MAX_ROWS],
) -> CodecResult<(usize, Option<u16>)> {
    if limit == 0 || usize::from(limit) > PAGE_MAX_ROWS {
        return Err(NON_CANONICAL);
    }
    if rows
        .windows(2)
        .any(|p| (p[0].kind, p[0].id) >= (p[1].kind, p[1].id))
    {
        return Err(NON_CANONICAL);
    }
    let mut ordinal = 0usize;
    let mut n = 0usize;
    for row in rows.iter().filter(|r| filter.admits(r, active_only)) {
        if ordinal >= usize::from(start) {
            if n == usize::from(limit) {
                return Ok((n, Some(u16::try_from(ordinal).map_err(|_| ARITHMETIC)?)));
            }
            out[n] = *row;
            n += 1;
        }
        ordinal += 1;
    }
    Ok((n, None))
}

pub fn bound_response(length: usize) -> QueryResult<usize> {
    if length > PAGE_MAX_BYTES {
        Err(QueryError::ResponseTooLarge)
    } else {
        Ok(length)
    }
}

/// Page frame: schema, snapshot id, row count, rows, optional cursor token.
pub fn encode_page(
    snapshot: Digest32,
    rows: &[ParticipantRow],
    cursor: Option<&str>,
    out: &mut [u8],
) -> QueryResult<usize> {
    if rows.len() > PAGE_MAX_ROWS || cursor.is_some_and(|c| c.len() > CURSOR_MAX_BYTES) {
        return Err(QueryError::ResponseTooLarge);
    }
    let cap = out.len().min(PAGE_MAX_BYTES);
    let mut w = Writer::new(&mut out[..cap]);
    w.u16(SCHEMA_VERSION)?;
    w.put(snapshot.as_bytes())?;
    w.u8(u8::try_from(rows.len()).map_err(|_| ARITHMETIC)?)?;
    for row in rows {
        row.encode(&mut w)?;
    }
    match cursor {
        None => w.boolean(false)?,
        Some(c) => {
            w.boolean(true)?;
            w.u16(u16::try_from(c.len()).map_err(|_| ARITHMETIC)?)?;
            w.put(c.as_bytes())?;
        }
    }
    bound_response(w.len())
}
