//! F02 worker identity and enrollment lifecycle over the common shared state cell.
//! Transitions are pure over authenticated host values supplied by the adapter.
use crate::{
    codec::{self, Reader, Writer},
    dispatch::{self, Operation},
    errors::{
        CodecResult, ARITHMETIC, BAD_VERSION, CAPACITY, CONFLICT, EVIDENCE_BINDING, EXPIRED,
        F02_ADMISSION_NOT_EFFECTIVE, F02_DEADLINE_INVALID, F02_DELEGATE_CONSENT_REQUIRED,
        F02_DELEGATE_REVOKED, F02_MARKET_PAUSED, F02_METADATA_EXPIRED,
        F02_METADATA_INTEGRITY_FAILURE, F02_OWNER_REQUIRED, F02_RATE_LIMITED, F02_WRONG_GENERATION,
        F02_WRONG_REVISION, KEY_MISMATCH, NON_CANONICAL, NOT_FOUND, UNAUTHORIZED,
        UNKNOWN_OPERATION, WRONG_PHASE,
    },
    evaluators::{
        authority::split_identity_section, codec::verify_digest, model::VerificationError,
    },
    registry::{market_clock, MarketHeader},
    state::{self, ActorSlot, ReplayDecision, ReplayRequest, RetainedResult, Section, SharedState},
    types::{
        Authentication, Digest32, MarketId, MetadataDigest, PrincipalId, PublicKey32, Signature64,
        Version, WorkerId, WorkerRosterEntry,
    },
    MAX_WORKERS,
};

pub const MAX_MANIFEST_BYTES: usize = 8_192;
pub const MAX_URI_BYTES: usize = 2_048;
pub const DEFAULT_PROPOSAL_LIFETIME: u64 = 128;
pub const MAX_METADATA_LIFETIME: u64 = 256;
pub const METADATA_COOLDOWN: u64 = 8;
pub const WORKER_RECORD_BYTES: usize = 298;
pub const WORKER_TABLE_MAX_BYTES: usize = 1 + MAX_WORKERS * WORKER_RECORD_BYTES;
pub const RESERVED_UNAVAILABLE: u16 = 0x0208;
/// Minimum `control_scratch` length accepted by [`apply`]: one identity section payload
/// (worker table followed by the F03 evaluator region) plus one control payload.
pub const CONTROL_SCRATCH_BYTES: usize =
    Section::IdentityRoster.payload_cap() + Section::Control.payload_cap();

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum WorkerState {
    Enrolled = 1,
    Available = 2,
    Draining = 3,
    Revoked = 4,
    Retired = 5,
    PendingOwner = 6,
}
impl WorkerState {
    /// Decodes a stored worker state byte.
    ///
    /// # Errors
    /// Returns `NON_CANONICAL` when the byte is outside 1..=6.
    pub fn from_u8(value: u8) -> CodecResult<Self> {
        Ok(match value {
            1 => Self::Enrolled,
            2 => Self::Available,
            3 => Self::Draining,
            4 => Self::Revoked,
            5 => Self::Retired,
            6 => Self::PendingOwner,
            _ => return Err(NON_CANONICAL),
        })
    }
    const fn serving(self) -> bool {
        matches!(self, Self::Enrolled | Self::Available | Self::Draining)
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum RevocationReason {
    Compromised = 1,
    OwnerAction = 2,
    IdentityFreeze = 3,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct WorkerCurrent {
    pub worker: WorkerId,
    pub owner: PrincipalId,
    pub delegate: PublicKey32,
    pub metadata: MetadataDigest,
    pub generation: u64,
    pub key_version: u64,
    pub metadata_revision: u64,
    pub valid_from: u64,
    pub expiry: u64,
    pub revocation_sequence: u64,
    pub effective_epoch: u64,
    pub last_sequence: u64,
    pub last_request_id: [u8; 32],
    pub last_request_digest: [u8; 32],
    pub last_result_digest: [u8; 32],
    pub state: WorkerState,
    pub slot: u8,
    pub last_metadata_height: u64,
}
impl WorkerCurrent {
    fn validate(&self) -> CodecResult<()> {
        if self.delegate.0 == [0; 32]
            || self.generation == 0
            || self.key_version == 0
            || self.metadata_revision == 0
            || self.valid_from >= self.expiry
            || usize::from(self.slot) >= MAX_WORKERS
        {
            return Err(NON_CANONICAL);
        }
        Ok(())
    }
    fn write(&self, w: &mut Writer<'_>) -> CodecResult<()> {
        w.put(self.worker.as_bytes())?;
        w.put(self.owner.as_bytes())?;
        w.put(&self.delegate.0)?;
        w.put(self.metadata.as_bytes())?;
        for v in [
            self.generation,
            self.key_version,
            self.metadata_revision,
            self.valid_from,
            self.expiry,
            self.revocation_sequence,
            self.effective_epoch,
            self.last_sequence,
        ] {
            w.u64(v)?;
        }
        w.put(&self.last_request_id)?;
        w.put(&self.last_request_digest)?;
        w.put(&self.last_result_digest)?;
        w.u8(self.state as u8)?;
        w.u8(self.slot)?;
        w.u64(self.last_metadata_height)
    }
    fn read(r: &mut Reader<'_>) -> CodecResult<Self> {
        let v = Self {
            worker: WorkerId::new(r.fixed()?)?,
            owner: PrincipalId::new(r.fixed()?)?,
            delegate: PublicKey32(r.fixed()?),
            metadata: MetadataDigest::new(r.fixed()?)?,
            generation: r.u64()?,
            key_version: r.u64()?,
            metadata_revision: r.u64()?,
            valid_from: r.u64()?,
            expiry: r.u64()?,
            revocation_sequence: r.u64()?,
            effective_epoch: r.u64()?,
            last_sequence: r.u64()?,
            last_request_id: r.fixed()?,
            last_request_digest: r.fixed()?,
            last_result_digest: r.fixed()?,
            state: WorkerState::from_u8(r.u8()?)?,
            slot: r.u8()?,
            last_metadata_height: r.u64()?,
        };
        v.validate()?;
        Ok(v)
    }
    /// Derived from the immutable proposal fields; never stored.
    ///
    /// # Errors
    /// Returns `NON_CANONICAL` when the hash is all zero.
    pub fn proposal_digest(&self, market: &MarketHeader) -> CodecResult<Digest32> {
        let mut buf = [0u8; 288];
        let mut w = Writer::new(&mut buf);
        w.put(market.deployment_chain_domain.as_bytes())?;
        w.put(market.program_id.as_bytes())?;
        w.put(market.market_id.as_bytes())?;
        w.put(self.worker.as_bytes())?;
        w.put(self.owner.as_bytes())?;
        w.put(&self.delegate.0)?;
        w.put(self.metadata.as_bytes())?;
        w.u64(self.generation)?;
        w.u64(self.key_version)?;
        w.u64(self.valid_from)?;
        w.u64(self.expiry)?;
        let n = w.len();
        codec::domain_hash("PAXAI/worker-enrollment-proposal/v1", &buf[..n])
    }
}

/// Bounded F02 current-record table: count:u8 then records sorted by `WorkerId`.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct WorkerTable {
    records: [Option<WorkerCurrent>; MAX_WORKERS],
    count: usize,
}
impl Default for WorkerTable {
    fn default() -> Self {
        Self::new()
    }
}
impl WorkerTable {
    #[must_use]
    pub const fn new() -> Self {
        Self {
            records: [None; MAX_WORKERS],
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
    pub fn iter(&self) -> impl Iterator<Item = &WorkerCurrent> {
        self.records[..self.count].iter().flatten()
    }
    #[must_use]
    pub fn get(&self, worker: WorkerId) -> Option<WorkerCurrent> {
        self.iter().find(|r| r.worker == worker).copied()
    }
    fn position(&self, worker: WorkerId) -> CodecResult<usize> {
        self.records[..self.count]
            .iter()
            .position(|r| r.is_some_and(|r| r.worker == worker))
            .ok_or(NOT_FOUND)
    }
    /// Inserts a new record in `WorkerId` order.
    ///
    /// # Errors
    /// Returns `NON_CANONICAL` when the record is invalid; `CAPACITY` when the table is full; `CONFLICT` when the worker or slot is already present.
    pub fn insert(&mut self, record: &WorkerCurrent) -> CodecResult<()> {
        record.validate()?;
        if self.count >= MAX_WORKERS {
            return Err(CAPACITY);
        }
        if self
            .iter()
            .any(|r| r.worker == record.worker || r.slot == record.slot)
        {
            return Err(CONFLICT);
        }
        let at = self.records[..self.count]
            .iter()
            .position(|r| r.is_some_and(|r| r.worker > record.worker))
            .unwrap_or(self.count);
        self.records.copy_within(at..self.count, at + 1);
        self.records[at] = Some(*record);
        self.count += 1;
        Ok(())
    }
    /// Replaces the stored record of the same worker.
    ///
    /// # Errors
    /// Returns `NON_CANONICAL` when the record is invalid; `NOT_FOUND` when the worker is absent.
    pub fn replace(&mut self, record: &WorkerCurrent) -> CodecResult<()> {
        record.validate()?;
        let at = self.position(record.worker)?;
        self.records[at] = Some(*record);
        Ok(())
    }
    /// Removes and returns the worker's record.
    ///
    /// # Errors
    /// Returns `NOT_FOUND` when the worker is absent.
    pub fn remove(&mut self, worker: WorkerId) -> CodecResult<WorkerCurrent> {
        let at = self.position(worker)?;
        let record = self.records[at].ok_or(NOT_FOUND)?;
        self.records.copy_within(at + 1..self.count, at);
        self.count -= 1;
        self.records[self.count] = None;
        Ok(record)
    }
    /// Returns the lowest slot not held by any record.
    ///
    /// # Errors
    /// Returns `CAPACITY` when every slot is taken.
    pub fn free_slot(&self) -> CodecResult<u8> {
        (0..u8::try_from(MAX_WORKERS).map_err(|_| ARITHMETIC)?)
            .find(|s| !self.iter().any(|r| r.slot == *s))
            .ok_or(CAPACITY)
    }
    #[must_use]
    pub const fn encoded_len(&self) -> usize {
        1 + self.count * WORKER_RECORD_BYTES
    }
    /// Writes the canonical table bytes into `out`.
    ///
    /// # Errors
    /// Returns `CAPACITY` when `out` is shorter than `encoded_len`.
    pub fn encode(&self, out: &mut [u8]) -> CodecResult<usize> {
        let n = self.encoded_len();
        if out.len() < n {
            return Err(CAPACITY);
        }
        let mut w = Writer::new(&mut out[..n]);
        w.u8(u8::try_from(self.count).map_err(|_| ARITHMETIC)?)?;
        for r in self.iter() {
            r.write(&mut w)?;
        }
        Ok(w.len())
    }
    /// An empty section decodes as an empty table (no worker ever enrolled).
    ///
    /// # Errors
    /// Returns `CAPACITY` when the input or record count exceeds the table bound; `NON_CANONICAL` when the input is short, has trailing bytes, holds an invalid or out-of-order record; `CONFLICT` when two records share a slot.
    pub fn decode(input: &[u8]) -> CodecResult<Self> {
        let mut table = Self::new();
        if input.is_empty() {
            return Ok(table);
        }
        if input.len() > WORKER_TABLE_MAX_BYTES {
            return Err(CAPACITY);
        }
        let mut r = Reader::new(input);
        let count = usize::from(r.u8()?);
        if count > MAX_WORKERS {
            return Err(CAPACITY);
        }
        for _ in 0..count {
            let record = WorkerCurrent::read(&mut r)?;
            if table.count > 0
                && table.records[table.count - 1].is_some_and(|p| p.worker >= record.worker)
            {
                return Err(NON_CANONICAL);
            }
            table.insert(&record)?;
        }
        r.finish()?;
        Ok(table)
    }
}

/// Authenticated host values for one direct native call.
#[derive(Clone, Copy, Debug)]
pub struct CallContext<'m> {
    pub market: &'m MarketHeader,
    pub invoking_principal: PrincipalId,
    pub height: u64,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum Applied {
    Applied { state_len: usize, event_len: usize },
    AlreadyApplied(RetainedResult),
}

/// Unknown, reserved and service-only codes never reach an F02 transition.
///
/// # Errors
/// Returns `UNKNOWN_OPERATION` when the selector is reserved, service-only, unknown, or not an F02 operation.
pub fn admit_selector(selector: u16) -> CodecResult<Operation> {
    if selector == RESERVED_UNAVAILABLE || (0x0281..=0x0286).contains(&selector) {
        return Err(UNKNOWN_OPERATION);
    }
    let op = Operation::decode(selector)?;
    if op.metadata().feature != 2 {
        return Err(UNKNOWN_OPERATION);
    }
    Ok(op)
}

/// Digest the worker owner signs to consent to a delegate key.
///
/// # Errors
/// Returns `NON_CANONICAL` when the hash is all zero.
#[allow(clippy::too_many_arguments)]
pub fn consent_digest(
    market: &MarketHeader,
    worker: WorkerId,
    owner: PrincipalId,
    key: PublicKey32,
    generation: u64,
    key_version: u64,
    metadata: MetadataDigest,
    expiry: u64,
) -> CodecResult<Digest32> {
    let mut buf = [0u8; 264];
    let mut w = Writer::new(&mut buf);
    w.put(market.deployment_chain_domain.as_bytes())?;
    w.put(market.program_id.as_bytes())?;
    w.put(market.market_id.as_bytes())?;
    w.put(worker.as_bytes())?;
    w.put(owner.as_bytes())?;
    w.put(&key.0)?;
    w.u64(generation)?;
    w.u64(key_version)?;
    w.put(metadata.as_bytes())?;
    w.u64(expiry)?;
    let n = w.len();
    codec::domain_hash("PAXAI/worker-delegate-consent/v1", &buf[..n])
}

fn verify(key: PublicKey32, signature: Signature64, digest: Digest32) -> CodecResult<()> {
    verify_digest(key, signature, digest.bytes()).map_err(|e| match e {
        VerificationError::Application(a) => a,
        #[cfg(target_arch = "wasm32")]
        VerificationError::Host(_) => crate::errors::HOST_CAPABILITY,
    })
}

/// Bounded offchain manifest summary after full grammar validation.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct ManifestSummary {
    pub market: MarketId,
    pub worker: WorkerId,
    pub owner: PrincipalId,
    pub generation: u64,
    pub key_version: u64,
    pub revision: u64,
    pub valid_from: u64,
    pub expiry: u64,
    pub digest: MetadataDigest,
}

fn ranged(value: u32, max: u32) -> CodecResult<()> {
    if value == 0 || value > max {
        Err(NON_CANONICAL)
    } else {
        Ok(())
    }
}

fn check_host(host: &[u8]) -> CodecResult<()> {
    if host.is_empty() || host.len() > 253 {
        return Err(NON_CANONICAL);
    }
    let mut last_numeric = false;
    for label in host.split(|b| *b == b'.') {
        if label.is_empty()
            || label.len() > 63
            || label[0] == b'-'
            || label[label.len() - 1] == b'-'
            || !label
                .iter()
                .all(|b| b.is_ascii_lowercase() || b.is_ascii_digit() || *b == b'-')
        {
            return Err(NON_CANONICAL);
        }
        last_numeric = label.iter().all(u8::is_ascii_digit);
    }
    // A numeric final label is an IP literal, not a DNS hostname.
    if last_numeric {
        Err(NON_CANONICAL)
    } else {
        Ok(())
    }
}

/// Checks that `uri` is a canonical `https://<host>[:port]/paxai/v1` endpoint.
///
/// # Errors
/// Returns `NON_CANONICAL` when the URI is too long, non-ASCII, or not in that canonical form.
pub fn check_uri(uri: &[u8]) -> CodecResult<()> {
    if uri.len() > MAX_URI_BYTES || !uri.is_ascii() {
        return Err(NON_CANONICAL);
    }
    let rest = uri.strip_prefix(b"https://").ok_or(NON_CANONICAL)?;
    if rest.iter().any(|b| matches!(b, b'@' | b'?' | b'#')) {
        return Err(NON_CANONICAL);
    }
    let authority = rest.strip_suffix(b"/paxai/v1").ok_or(NON_CANONICAL)?;
    let (host, port) = match authority.iter().position(|b| *b == b':') {
        Some(i) => (&authority[..i], Some(&authority[i + 1..])),
        None => (authority, None),
    };
    check_host(host)?;
    if let Some(port) = port {
        if port.is_empty()
            || port.len() > 5
            || port[0] == b'0'
            || !port.iter().all(u8::is_ascii_digit)
        {
            return Err(NON_CANONICAL);
        }
        let value = port.iter().fold(0u32, |n, d| n * 10 + u32::from(d - b'0'));
        if value == 0 || value > 65_535 {
            return Err(NON_CANONICAL);
        }
    }
    Ok(())
}

/// Decodes and checks a worker metadata manifest.
///
/// # Errors
/// Returns `CAPACITY` when the input exceeds `MAX_MANIFEST_BYTES`; `BAD_VERSION` when the version is not 1; `ARITHMETIC` when a URI length does not fit `usize`; `NON_CANONICAL` when the input is short, has trailing bytes, or any field, window, capability, or endpoint is out of bounds or out of order.
pub fn decode_manifest(input: &[u8]) -> CodecResult<ManifestSummary> {
    if input.len() > MAX_MANIFEST_BYTES {
        return Err(CAPACITY);
    }
    let mut r = Reader::new(input);
    if r.u16()? != 1 {
        return Err(BAD_VERSION);
    }
    let market = MarketId::new(r.fixed()?)?;
    let worker = WorkerId::new(r.fixed()?)?;
    let owner = PrincipalId::new(r.fixed()?)?;
    let generation = r.u64()?;
    let key_version = r.u64()?;
    let revision = r.u64()?;
    let valid_from = r.u64()?;
    let expiry = r.u64()?;
    Digest32::new(r.fixed()?)?;
    if generation == 0 || key_version == 0 || revision == 0 {
        return Err(NON_CANONICAL);
    }
    check_window(valid_from, expiry)?;
    let capabilities = r.u16()?;
    if !(1..=8).contains(&capabilities) {
        return Err(NON_CANONICAL);
    }
    let mut previous: Option<(u8, [u8; 32], [u8; 32])> = None;
    for _ in 0..capabilities {
        let kind = r.u8()?;
        let mode = r.u8()?;
        if !(1..=3).contains(&kind) || !(1..=2).contains(&mode) {
            return Err(NON_CANONICAL);
        }
        let model: [u8; 32] = r.fixed()?;
        let _manifest: [u8; 32] = r.fixed()?;
        let _tokenizer: [u8; 32] = r.fixed()?;
        let input_schema: [u8; 32] = r.fixed()?;
        let _output_schema: [u8; 32] = r.fixed()?;
        for _ in 0..4 {
            ranged(r.u32()?, 1_048_576)?;
        }
        if !(1..=3).contains(&r.u8()?) {
            return Err(NON_CANONICAL);
        }
        ranged(r.u32()?, 3_600_000)?;
        ranged(u32::from(r.u16()?), 32)?;
        if !(1..=2).contains(&r.u8()?) {
            return Err(NON_CANONICAL);
        }
        let key = (kind, model, input_schema);
        if previous.is_some_and(|p| p >= key) {
            return Err(NON_CANONICAL);
        }
        previous = Some(key);
    }
    let endpoints = r.u16()?;
    if !(1..=2).contains(&endpoints) {
        return Err(NON_CANONICAL);
    }
    let mut previous_id = 0u8;
    let mut first_uri: &[u8] = &[];
    for i in 0..endpoints {
        let id = r.u8()?;
        if !(1..=2).contains(&id) || id <= previous_id {
            return Err(NON_CANONICAL);
        }
        previous_id = id;
        let length = usize::try_from(r.u32()?).map_err(|_| ARITHMETIC)?;
        if length > MAX_URI_BYTES {
            return Err(NON_CANONICAL);
        }
        let uri = r.take(length)?;
        check_uri(uri)?;
        if i == 1 && uri == first_uri {
            return Err(NON_CANONICAL);
        }
        first_uri = uri;
        Digest32::new(r.fixed()?)?;
        if r.u8()? != 1 || r.u16()? != 1 || r.u16()? != 1 {
            return Err(NON_CANONICAL);
        }
    }
    Digest32::new(r.fixed()?)?;
    Digest32::new(r.fixed()?)?;
    if r.u32()? != 0 {
        return Err(NON_CANONICAL);
    }
    r.finish()?;
    Ok(ManifestSummary {
        market,
        worker,
        owner,
        generation,
        key_version,
        revision,
        valid_from,
        expiry,
        digest: MetadataDigest::new(
            codec::domain_hash("PAXAI/worker-metadata/v1", input)?.bytes(),
        )?,
    })
}

fn check_window(valid_from: u64, expiry: u64) -> CodecResult<()> {
    if expiry <= valid_from || expiry - valid_from > MAX_METADATA_LIFETIME {
        Err(NON_CANONICAL)
    } else {
        Ok(())
    }
}

/// Inclusive at `valid_from`, exclusive at expiry.
///
/// # Errors
/// Returns `F02_ADMISSION_NOT_EFFECTIVE` when `height` is before `valid_from`; `F02_METADATA_EXPIRED` when it is at or past expiry.
pub fn check_metadata_window(record: &WorkerCurrent, height: u64) -> CodecResult<()> {
    if height < record.valid_from {
        Err(F02_ADMISSION_NOT_EFFECTIVE)
    } else if height >= record.expiry {
        Err(F02_METADATA_EXPIRED)
    } else {
        Ok(())
    }
}

/// New service admission under the frozen roster entry: the frozen generation and
/// key version must equal the unrevoked current versions; staged rotations block.
///
/// # Errors
/// Returns `EVIDENCE_BINDING` when worker or owner differ; `F02_DELEGATE_REVOKED` when revoked; `F02_ADMISSION_NOT_EFFECTIVE` when not enrolled or available; `F02_WRONG_GENERATION` when generation, key version, or key differ; `F02_METADATA_EXPIRED` when `height` is at or past expiry.
pub fn check_new_admission(
    record: &WorkerCurrent,
    frozen: &WorkerRosterEntry,
    height: u64,
) -> CodecResult<()> {
    if frozen.worker != record.worker || frozen.owner != record.owner {
        return Err(EVIDENCE_BINDING);
    }
    match record.state {
        WorkerState::Revoked => return Err(F02_DELEGATE_REVOKED),
        WorkerState::Enrolled | WorkerState::Available => {}
        _ => return Err(F02_ADMISSION_NOT_EFFECTIVE),
    }
    if frozen.generation.get() != record.generation
        || frozen.key_version.get() != record.key_version
        || frozen.public_key != record.delegate
    {
        return Err(F02_WRONG_GENERATION);
    }
    if height >= record.expiry {
        return Err(F02_METADATA_EXPIRED);
    }
    Ok(())
}

fn check_market(ctx: &CallContext<'_>, env: &codec::Envelope<'_>) -> CodecResult<()> {
    env.check_domain(
        ctx.market.deployment_chain_domain,
        ctx.market.program_id,
        ctx.market.market_id,
    )?;
    env.check_expiry(ctx.height)?;
    match ctx.market.lifecycle {
        1 | 2 => Ok(()),
        3 => Err(F02_MARKET_PAUSED),
        _ => Err(WRONG_PHASE),
    }
}

fn worker_id(payload: &[u8]) -> CodecResult<WorkerId> {
    let bytes: [u8; 32] = payload
        .get(..32)
        .and_then(|b| b.try_into().ok())
        .ok_or(NON_CANONICAL)?;
    WorkerId::new(bytes)
}

fn exact(payload: &[u8], length: usize) -> CodecResult<Reader<'_>> {
    if payload.len() != length {
        return Err(NON_CANONICAL);
    }
    Ok(Reader::new(payload))
}

#[derive(Clone, Copy)]
enum SlotChange {
    None,
    Bind(ActorSlot, PrincipalId),
    Retire(ActorSlot),
}

struct Commit<'a> {
    table: WorkerTable,
    slot_change: SlotChange,
    suffix: &'a [u8],
}

/// Decoded, admitted facts shared by every step of one native call.
struct Call<'a> {
    state: &'a SharedState<'a>,
    /// F03 evaluator region that follows the worker table; carried byte-for-byte.
    region: &'a [u8],
    ctx: &'a CallContext<'a>,
    env: &'a codec::ValidatedEnvelope<'a>,
    next_epoch: u64,
}

/// Caller-owned output and scratch buffers of one native call.
struct Buffers<'b> {
    out: &'b mut [u8],
    event: &'b mut [u8],
    control: &'b mut [u8],
}

fn finish(
    call: &Call<'_>,
    replay: &ReplayRequest,
    commit: &Commit<'_>,
    buffers: Buffers<'_>,
) -> CodecResult<Applied> {
    let Buffers {
        out,
        event,
        control,
    } = buffers;
    let (section, control) = control
        .split_at_mut_checked(Section::IdentityRoster.payload_cap())
        .ok_or(CAPACITY)?;
    let workers = commit.table.encode(section)?;
    let total = workers.checked_add(call.region.len()).ok_or(ARITHMETIC)?;
    section
        .get_mut(workers..total)
        .ok_or(CAPACITY)?
        .copy_from_slice(call.region);
    let mut next = call
        .state
        .replace_section(Section::IdentityRoster, &section[..total])?;
    match commit.slot_change {
        SlotChange::None => {}
        SlotChange::Bind(slot, principal) => {
            next.control
                .replay
                .bind(slot, principal, Version::new(1)?)?;
        }
        SlotChange::Retire(slot) => next.control.replay.retire(slot)?,
    }
    let result = codec::result_digest(commit.suffix)?;
    next.record_success(replay, call.ctx.height, result)?;
    let envelope = &call.env.envelope;
    let common = codec::EventCommon {
        market: call.ctx.market.market_id,
        epoch: envelope.epoch,
        config: Version::new(envelope.config)?,
        revision: next.revision,
        request: replay.digest,
        result,
    };
    let event_len = codec::encode_event_frame(envelope.operation, &common, commit.suffix, event)?;
    let state_len = state::encode_shared_state(&next, out, control)?;
    Ok(Applied::Applied {
        state_len,
        event_len,
    })
}

fn stamp(record: &mut WorkerCurrent, replay: &ReplayRequest, suffix: &[u8]) -> CodecResult<()> {
    record.last_sequence = replay.sequence;
    record.last_request_id = replay.request_id.bytes();
    record.last_request_digest = replay.digest.bytes();
    record.last_result_digest = codec::result_digest(suffix)?.bytes();
    Ok(())
}

/// Apply one finalized F02 native mutation. Refusals leave `state_bytes` authoritative;
/// nothing is written to the caller unless the whole candidate state is valid. The worker
/// table is the prefix of the identity section; the F03 region after it is kept unchanged.
///
/// # Errors
/// Returns the F02 refusal code of the first failed check, `CAPACITY` when the next worker
/// table followed by the region exceeds the identity section cap, or when `out`, `event` or
/// `control_scratch` (which needs [`CONTROL_SCRATCH_BYTES`]) cannot hold its encoding.
pub fn apply(
    state_bytes: &[u8],
    ctx: &CallContext<'_>,
    envelope_bytes: &[u8],
    out: &mut [u8],
    event: &mut [u8],
    control_scratch: &mut [u8],
) -> CodecResult<Applied> {
    let env = codec::decode_envelope(envelope_bytes)?;
    let e = env.envelope;
    admit_selector(e.operation.selector())?;
    check_market(ctx, &e)?;
    let state = state::decode_shared_state(state_bytes)?;
    let (workers, region) = split_identity_section(state.section(Section::IdentityRoster)?)?;
    let table = WorkerTable::decode(workers)?;
    let epoch = market_clock(ctx.market.origin_height, ctx.height)?.epoch;
    let next_epoch = epoch.checked_add(1).ok_or(ARITHMETIC)?;
    let call = Call {
        state: &state,
        region,
        ctx,
        env: &env,
        next_epoch,
    };
    let buffers = Buffers {
        out,
        event,
        control: control_scratch,
    };
    let op = e.operation;
    if op == dispatch::EnrollWorker || op == dispatch::ExpireEnrollment {
        apply_owner(&call, &table, buffers)
    } else {
        apply_record(&call, table, buffers)
    }
}

fn apply_owner(call: &Call<'_>, table: &WorkerTable, buffers: Buffers<'_>) -> CodecResult<Applied> {
    let ctx = call.ctx;
    let e = &call.env.envelope;
    codec::compare_native_principal(e, ctx.invoking_principal)?;
    if ctx.invoking_principal != ctx.market.owner_principal {
        return Err(UNAUTHORIZED);
    }
    let replay = ReplayRequest::from_envelope(
        ActorSlot::OWNER,
        call.state
            .control
            .replay
            .actor(ActorSlot::OWNER)
            .ok_or(NOT_FOUND)?
            .authority_version,
        call.env,
    )?;
    if let ReplayDecision::AlreadyApplied(last) =
        call.state.control.replay.check(&replay, ctx.height)?
    {
        return Ok(Applied::AlreadyApplied(last));
    }
    if e.operation == dispatch::EnrollWorker {
        enroll_worker(call, &replay, *table, buffers)
    } else {
        expire_enrollment(call, &replay, *table, buffers)
    }
}

fn enroll_worker(
    call: &Call<'_>,
    replay: &ReplayRequest,
    mut table: WorkerTable,
    buffers: Buffers<'_>,
) -> CodecResult<Applied> {
    let ctx = call.ctx;
    let mut r = exact(call.env.envelope.payload, 136)?;
    let nominee = PrincipalId::new(r.fixed()?)?;
    let nonce: [u8; 32] = r.fixed()?;
    let key = PublicKey32(r.fixed()?);
    let metadata = MetadataDigest::new(r.fixed()?)?;
    let consent_expiry = r.u64()?;
    if nonce == [0; 32] || key.0 == [0; 32] {
        return Err(NON_CANONICAL);
    }
    if consent_expiry <= ctx.height || consent_expiry - ctx.height > DEFAULT_PROPOSAL_LIFETIME {
        return Err(F02_DEADLINE_INVALID);
    }
    let worker = codec::derive_worker(ctx.market.market_id, nominee, nonce)?;
    if table.get(worker).is_some() {
        return Err(CONFLICT);
    }
    if table.len() >= MAX_WORKERS {
        return Err(CAPACITY);
    }
    let slot = table.free_slot()?;
    let record = WorkerCurrent {
        worker,
        owner: nominee,
        delegate: key,
        metadata,
        generation: 1,
        key_version: 1,
        metadata_revision: 1,
        valid_from: ctx.height,
        expiry: consent_expiry,
        revocation_sequence: 0,
        effective_epoch: call.next_epoch,
        last_sequence: 0,
        last_request_id: [0; 32],
        last_request_digest: [0; 32],
        last_result_digest: [0; 32],
        state: WorkerState::PendingOwner,
        slot,
        last_metadata_height: ctx.height,
    };
    let mut suffix = [0u8; 168];
    let mut w = Writer::new(&mut suffix);
    w.put(worker.as_bytes())?;
    w.put(nominee.as_bytes())?;
    w.u64(1)?;
    w.u64(call.next_epoch)?;
    w.put(metadata.as_bytes())?;
    w.put(&nonce)?;
    let n = w.len();
    table.insert(&record)?;
    finish(
        call,
        replay,
        &Commit {
            table,
            slot_change: SlotChange::Bind(ActorSlot::worker(usize::from(slot))?, nominee),
            suffix: &suffix[..n],
        },
        buffers,
    )
}

fn expire_enrollment(
    call: &Call<'_>,
    replay: &ReplayRequest,
    mut table: WorkerTable,
    buffers: Buffers<'_>,
) -> CodecResult<Applied> {
    let ctx = call.ctx;
    let mut r = exact(call.env.envelope.payload, 72)?;
    let worker = WorkerId::new(r.fixed()?)?;
    let expected_digest: [u8; 32] = r.fixed()?;
    let expected_expiry = r.u64()?;
    let record = table.get(worker).ok_or(NOT_FOUND)?;
    if record.state != WorkerState::PendingOwner || ctx.height < record.expiry {
        return Err(WRONG_PHASE);
    }
    let digest = record.proposal_digest(ctx.market)?;
    if digest.bytes() != expected_digest || expected_expiry != record.expiry {
        return Err(CONFLICT);
    }
    let slot = ActorSlot::worker(usize::from(record.slot))?;
    if call
        .state
        .control
        .replay
        .actor(slot)
        .is_some_and(|a| a.last.is_some() || a.principal != record.owner)
    {
        return Err(CONFLICT);
    }
    let mut suffix = [0u8; 168];
    let mut w = Writer::new(&mut suffix);
    w.put(worker.as_bytes())?;
    w.put(digest.as_bytes())?;
    w.u64(record.expiry)?;
    let n = w.len();
    table.remove(worker)?;
    finish(
        call,
        replay,
        &Commit {
            table,
            slot_change: SlotChange::Retire(slot),
            suffix: &suffix[..n],
        },
        buffers,
    )
}

fn apply_record(
    call: &Call<'_>,
    mut table: WorkerTable,
    buffers: Buffers<'_>,
) -> CodecResult<Applied> {
    let ctx = call.ctx;
    let op = call.env.envelope.operation;
    let worker = worker_id(call.env.envelope.payload)?;
    let mut record = table.get(worker).ok_or(NOT_FOUND)?;
    authenticate(call, &record)?;
    let slot = ActorSlot::worker(usize::from(record.slot))?;
    let actor = call.state.control.replay.actor(slot).ok_or(NOT_FOUND)?;
    if actor.principal != record.owner {
        return Err(UNAUTHORIZED);
    }
    let replay = ReplayRequest::from_envelope(slot, actor.authority_version, call.env)?;
    if let ReplayDecision::AlreadyApplied(last) =
        call.state.control.replay.check(&replay, ctx.height)?
    {
        return Ok(Applied::AlreadyApplied(last));
    }
    let pending = record.state == WorkerState::PendingOwner;
    if pending && op != dispatch::AcceptEnrollment {
        return Err(WRONG_PHASE);
    }
    transition(call, &mut record, pending)?;
    let mut suffix = [0u8; 57];
    let mut w = Writer::new(&mut suffix);
    w.put(record.worker.as_bytes())?;
    w.u8(record.state as u8)?;
    w.u64(record.generation)?;
    w.u64(record.metadata_revision)?;
    w.u64(record.effective_epoch)?;
    let n = w.len();
    stamp(&mut record, &replay, &suffix[..n])?;
    table.replace(&record)?;
    finish(
        call,
        &replay,
        &Commit {
            table,
            slot_change: SlotChange::None,
            suffix: &suffix[..n],
        },
        buffers,
    )
}

/// Delegate-signed metadata publication, or a native call by the worker owner.
fn authenticate(call: &Call<'_>, record: &WorkerCurrent) -> CodecResult<()> {
    let e = &call.env.envelope;
    if e.operation == dispatch::PublishMetadata {
        match e.authentication {
            Authentication::Delegate { key, signature } => {
                if e.actor != record.owner {
                    return Err(UNAUTHORIZED);
                }
                if record.state == WorkerState::Revoked {
                    return Err(F02_DELEGATE_REVOKED);
                }
                if key != record.delegate {
                    return Err(KEY_MISMATCH);
                }
                verify(
                    key,
                    signature,
                    Digest32::new(call.env.request_digest()?.bytes())?,
                )
            }
            Authentication::Native => Err(F02_DELEGATE_CONSENT_REQUIRED),
        }
    } else {
        codec::compare_native_principal(e, call.ctx.invoking_principal)?;
        if call.ctx.invoking_principal != record.owner {
            return Err(F02_OWNER_REQUIRED);
        }
        Ok(())
    }
}

fn transition(call: &Call<'_>, record: &mut WorkerCurrent, pending: bool) -> CodecResult<()> {
    let payload = call.env.envelope.payload;
    match call.env.envelope.operation {
        dispatch::AcceptEnrollment => accept_enrollment(call, record, pending)?,
        dispatch::PublishMetadata => publish_metadata(call, record)?,
        dispatch::SetDraining => {
            exact(payload, 32)?;
            if !matches!(record.state, WorkerState::Enrolled | WorkerState::Available) {
                return Err(WRONG_PHASE);
            }
            record.state = WorkerState::Draining;
        }
        dispatch::UndoDrain => {
            exact(payload, 32)?;
            if record.state != WorkerState::Draining {
                return Err(WRONG_PHASE);
            }
            record.state = WorkerState::Available;
            record.effective_epoch = call.next_epoch;
        }
        dispatch::RetireWorker => {
            exact(payload, 32)?;
            if record.state == WorkerState::Retired {
                return Err(WRONG_PHASE);
            }
            record.state = WorkerState::Retired;
        }
        dispatch::RotateDelegate => rotate_delegate(call, record)?,
        dispatch::RevokeDelegate => revoke_delegate(payload, record)?,
        _ => return Err(UNKNOWN_OPERATION),
    }
    Ok(())
}

fn accept_enrollment(
    call: &Call<'_>,
    record: &mut WorkerCurrent,
    pending: bool,
) -> CodecResult<()> {
    let ctx = call.ctx;
    let mut r = exact(call.env.envelope.payload, 144)?;
    r.take(32)?;
    let generation = r.u64()?;
    let key_version = r.u64()?;
    let metadata: [u8; 32] = r.fixed()?;
    let signature = Signature64(r.fixed()?);
    if !pending {
        return Err(WRONG_PHASE);
    }
    if ctx.height >= record.expiry {
        return Err(EXPIRED);
    }
    if generation != 1
        || key_version != 1
        || generation != record.generation
        || key_version != record.key_version
        || metadata != record.metadata.bytes()
    {
        return Err(CONFLICT);
    }
    let consent = consent_digest(
        ctx.market,
        record.worker,
        record.owner,
        record.delegate,
        1,
        1,
        record.metadata,
        record.expiry,
    )?;
    verify(record.delegate, signature, consent)?;
    record.state = WorkerState::Enrolled;
    record.effective_epoch = call.next_epoch;
    Ok(())
}

fn publish_metadata(call: &Call<'_>, record: &mut WorkerCurrent) -> CodecResult<()> {
    let ctx = call.ctx;
    if !record.state.serving() {
        return Err(WRONG_PHASE);
    }
    let mut r = Reader::new(call.env.envelope.payload);
    r.take(32)?;
    let expected = r.u64()?;
    let revision = r.u64()?;
    let digest = MetadataDigest::new(r.fixed()?)?;
    let valid_from = r.u64()?;
    let expiry = r.u64()?;
    let manifest = r.bytes(MAX_MANIFEST_BYTES)?;
    r.finish()?;
    let m = decode_manifest(manifest)?;
    if m.market != ctx.market.market_id
        || m.worker != record.worker
        || m.owner != record.owner
        || m.generation != record.generation
        || m.key_version != record.key_version
        || m.digest != digest
        || m.revision != revision
        || m.valid_from != valid_from
        || m.expiry != expiry
    {
        return Err(F02_METADATA_INTEGRITY_FAILURE);
    }
    if expected != record.metadata_revision {
        return Err(F02_WRONG_REVISION);
    }
    let next = record.metadata_revision.checked_add(1).ok_or(ARITHMETIC)?;
    if revision != next {
        return Err(F02_WRONG_REVISION);
    }
    if expiry <= ctx.height {
        return Err(F02_METADATA_EXPIRED);
    }
    if ctx.height
        < record
            .last_metadata_height
            .saturating_add(METADATA_COOLDOWN)
    {
        return Err(F02_RATE_LIMITED);
    }
    record.metadata = digest;
    record.metadata_revision = next;
    record.valid_from = valid_from;
    record.expiry = expiry;
    record.last_metadata_height = ctx.height;
    record.effective_epoch = call.next_epoch;
    Ok(())
}

fn rotate_delegate(call: &Call<'_>, record: &mut WorkerCurrent) -> CodecResult<()> {
    let ctx = call.ctx;
    let mut r = exact(call.env.envelope.payload, 184)?;
    r.take(32)?;
    let generation = r.u64()?;
    let key_version = r.u64()?;
    let key = PublicKey32(r.fixed()?);
    let metadata = MetadataDigest::new(r.fixed()?)?;
    let consent_expiry = r.u64()?;
    let signature = Signature64(r.fixed()?);
    if record.state == WorkerState::Retired {
        return Err(WRONG_PHASE);
    }
    if generation != record.generation || key_version != record.key_version {
        return Err(F02_WRONG_GENERATION);
    }
    if key.0 == [0; 32] || key == record.delegate {
        return Err(NON_CANONICAL);
    }
    if ctx.height >= consent_expiry {
        return Err(EXPIRED);
    }
    let next_generation = generation.checked_add(1).ok_or(ARITHMETIC)?;
    let next_key_version = key_version.checked_add(1).ok_or(ARITHMETIC)?;
    let consent = consent_digest(
        ctx.market,
        record.worker,
        record.owner,
        key,
        next_generation,
        next_key_version,
        metadata,
        consent_expiry,
    )?;
    verify(key, signature, consent)?;
    record.generation = next_generation;
    record.key_version = next_key_version;
    record.delegate = key;
    record.metadata = metadata;
    if record.state == WorkerState::Revoked {
        record.state = WorkerState::Enrolled;
    }
    record.effective_epoch = call.next_epoch;
    Ok(())
}

fn revoke_delegate(payload: &[u8], record: &mut WorkerCurrent) -> CodecResult<()> {
    let mut r = exact(payload, 49)?;
    r.take(32)?;
    let generation = r.u64()?;
    let reason = r.u8()?;
    let sequence = r.u64()?;
    if !(1..=3).contains(&reason) {
        return Err(NON_CANONICAL);
    }
    if !record.state.serving() {
        return Err(WRONG_PHASE);
    }
    if generation != record.generation {
        return Err(F02_WRONG_GENERATION);
    }
    if sequence
        != record
            .revocation_sequence
            .checked_add(1)
            .ok_or(ARITHMETIC)?
    {
        return Err(CONFLICT);
    }
    record.revocation_sequence = sequence;
    record.state = WorkerState::Revoked;
    Ok(())
}
