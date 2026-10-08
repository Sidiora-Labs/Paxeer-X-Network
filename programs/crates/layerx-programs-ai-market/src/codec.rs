//! Canonical, allocation-free big-endian codecs. Decode validates before exposing a view.
//! Caller-provided output buffers are transport scratch, never authoritative state.
use crate::{
    dispatch::{CallBoundary, Operation, SequencePolicy},
    errors::{
        ApplicationError, CodecResult, ARITHMETIC, BAD_VERSION, CAPACITY, CONFLICT, EXPIRED,
        F03_NO_SCORES, NON_CANONICAL, UNAUTHORIZED, UNKNOWN_OPERATION, WRONG_CONFIG, WRONG_DOMAIN,
        WRONG_EPOCH, WRONG_MARKET, WRONG_PROGRAM, WRONG_ROSTER,
    },
    types::{
        AccountId, AttestationDigest, Authentication, ChainDomain, CommitmentDigest, Digest32,
        EvaluatorBinding, EvaluatorId, EvaluatorRosterEntry, EvidenceRoot, FrozenBinding, MarketId,
        MetadataDigest, Presence, PrincipalId, ProgramId, PublicKey32, ReportDigest, RequestDigest,
        RequestId, ResultDigest, RosterDigest, RubricDigest, Salt32, Score, ScoreEntry,
        Signature64, StateDigest, TaskId, Version, WorkerId, WorkerRosterEntry,
    },
    MAX_CHUNK_BYTES, MAX_ENVELOPE_BYTES, MAX_EVENT_BYTES, MAX_PAYLOAD_BYTES, MAX_RESULT_BYTES,
    MAX_RESULT_PAYLOAD_BYTES, MAX_STATE_BYTES, MAX_WORKERS, SCHEMA_VERSION,
};
use sha2::{Digest, Sha256};

pub const ENVELOPE_PREFIX_BYTES: usize = 238;
pub const REPORT_FIXED_BYTES: usize = 228;
pub const REPORT_MAX_BYTES: usize = 1380;
pub const WORKER_ROSTER_BYTES: usize = 176;
pub const EVALUATOR_ROSTER_BYTES: usize = 144;
pub const EVENT_COMMON_BYTES: usize = 122;
pub const STATE_SECTION_CAPS: [usize; 6] = [16384, 24576, 24576, 81920, 24576, 24576];
pub const STATE_GLOBAL_BYTES: usize = 16;
pub const STATE_SECTION_HEADER_BYTES: usize = 8;
pub const STATE_FRAMING_BYTES: usize = 64;

#[derive(Clone, Copy, Debug)]
pub struct Reader<'a> {
    input: &'a [u8],
    offset: usize,
}
impl<'a> Reader<'a> {
    #[must_use]
    pub const fn new(input: &'a [u8]) -> Self {
        Self { input, offset: 0 }
    }
    #[must_use]
    pub const fn offset(&self) -> usize {
        self.offset
    }
    #[must_use]
    pub fn remaining(&self) -> usize {
        self.input.len() - self.offset
    }
    /// Consumes exactly `length` bytes.
    ///
    /// # Errors
    /// Returns `ARITHMETIC` when the end offset overflows; `NON_CANONICAL` when fewer than `length` bytes remain.
    pub fn take(&mut self, length: usize) -> CodecResult<&'a [u8]> {
        let end = self.offset.checked_add(length).ok_or(ARITHMETIC)?;
        let bytes = self.input.get(self.offset..end).ok_or(NON_CANONICAL)?;
        self.offset = end;
        Ok(bytes)
    }
    /// Consumes a fixed `N`-byte array.
    ///
    /// # Errors
    /// Propagates `take`'s `ARITHMETIC`/`NON_CANONICAL` refusals.
    pub fn fixed<const N: usize>(&mut self) -> CodecResult<[u8; N]> {
        self.take(N)?.try_into().map_err(|_| NON_CANONICAL)
    }
    /// Reads one byte.
    ///
    /// # Errors
    /// Propagates `take`'s `ARITHMETIC`/`NON_CANONICAL` refusals.
    pub fn u8(&mut self) -> CodecResult<u8> {
        Ok(self.fixed::<1>()?[0])
    }
    /// Reads a big-endian `u16`.
    ///
    /// # Errors
    /// Propagates `take`'s `ARITHMETIC`/`NON_CANONICAL` refusals.
    pub fn u16(&mut self) -> CodecResult<u16> {
        Ok(u16::from_be_bytes(self.fixed()?))
    }
    /// Reads a big-endian `u32`.
    ///
    /// # Errors
    /// Propagates `take`'s `ARITHMETIC`/`NON_CANONICAL` refusals.
    pub fn u32(&mut self) -> CodecResult<u32> {
        Ok(u32::from_be_bytes(self.fixed()?))
    }
    /// Reads a big-endian `u64`.
    ///
    /// # Errors
    /// Propagates `take`'s `ARITHMETIC`/`NON_CANONICAL` refusals.
    pub fn u64(&mut self) -> CodecResult<u64> {
        Ok(u64::from_be_bytes(self.fixed()?))
    }
    /// Reads a big-endian `u128`.
    ///
    /// # Errors
    /// Propagates `take`'s `ARITHMETIC`/`NON_CANONICAL` refusals.
    pub fn u128(&mut self) -> CodecResult<u128> {
        Ok(u128::from_be_bytes(self.fixed()?))
    }
    /// Reads a canonical boolean byte.
    ///
    /// # Errors
    /// Returns `NON_CANONICAL` when the byte is neither 0 nor 1; propagates `take`'s refusals.
    pub fn boolean(&mut self) -> CodecResult<bool> {
        match self.u8()? {
            0 => Ok(false),
            1 => Ok(true),
            _ => Err(NON_CANONICAL),
        }
    }
    /// Reads a presence flag, then the value with `decode` when present.
    ///
    /// # Errors
    /// Propagates the flag's `boolean` refusals and any refusal from `decode`.
    pub fn presence<T>(
        &mut self,
        decode: impl FnOnce(&mut Self) -> CodecResult<T>,
    ) -> CodecResult<Presence<T>> {
        if self.boolean()? {
            Ok(Presence::Present(decode(self)?))
        } else {
            Ok(Presence::Absent)
        }
    }
    /// Consumes `length` reserved bytes that must be zero.
    ///
    /// # Errors
    /// Returns `NON_CANONICAL` when any reserved byte is nonzero; propagates `take`'s refusals.
    pub fn reserved(&mut self, length: usize) -> CodecResult<()> {
        if self.take(length)?.iter().any(|b| *b != 0) {
            Err(NON_CANONICAL)
        } else {
            Ok(())
        }
    }
    /// Reads a `u32` length-prefixed byte string of at most `cap` bytes.
    ///
    /// # Errors
    /// Returns `CAPACITY` when the length exceeds `cap`; `ARITHMETIC` when it does not fit `usize`; propagates `take`'s refusals.
    pub fn bytes(&mut self, cap: usize) -> CodecResult<&'a [u8]> {
        let length = usize::try_from(self.u32()?).map_err(|_| ARITHMETIC)?;
        if length > cap {
            return Err(CAPACITY);
        }
        self.take(length)
    }
    /// Reads a `u16` count, then `count * element_bytes` bytes.
    ///
    /// # Errors
    /// Returns `NON_CANONICAL` when the count is below `minimum`; `CAPACITY` when above `maximum`; `ARITHMETIC` when the byte length overflows; propagates `take`'s refusals.
    pub fn vector_bytes(
        &mut self,
        minimum: usize,
        maximum: usize,
        element_bytes: usize,
    ) -> CodecResult<&'a [u8]> {
        let count = usize::from(self.u16()?);
        if count < minimum {
            return Err(NON_CANONICAL);
        }
        if count > maximum {
            return Err(CAPACITY);
        }
        self.take(count.checked_mul(element_bytes).ok_or(ARITHMETIC)?)
    }
    /// Requires that all input was consumed.
    ///
    /// # Errors
    /// Returns `NON_CANONICAL` when unread input remains.
    pub fn finish(self) -> CodecResult<()> {
        if self.offset == self.input.len() {
            Ok(())
        } else {
            Err(NON_CANONICAL)
        }
    }
}

pub struct Writer<'a> {
    output: &'a mut [u8],
    offset: usize,
}
impl<'a> Writer<'a> {
    pub fn new(output: &'a mut [u8]) -> Self {
        Self { output, offset: 0 }
    }
    #[must_use]
    pub const fn len(&self) -> usize {
        self.offset
    }
    #[must_use]
    pub const fn is_empty(&self) -> bool {
        self.offset == 0
    }
    /// Appends `bytes`.
    ///
    /// # Errors
    /// Returns `ARITHMETIC` when the end offset overflows; `CAPACITY` when the output buffer is too small.
    pub fn put(&mut self, bytes: &[u8]) -> CodecResult<()> {
        let end = self.offset.checked_add(bytes.len()).ok_or(ARITHMETIC)?;
        self.output
            .get_mut(self.offset..end)
            .ok_or(CAPACITY)?
            .copy_from_slice(bytes);
        self.offset = end;
        Ok(())
    }
    /// Writes one byte.
    ///
    /// # Errors
    /// Propagates `put`'s `ARITHMETIC`/`CAPACITY` refusals.
    pub fn u8(&mut self, value: u8) -> CodecResult<()> {
        self.put(&[value])
    }
    /// Writes a big-endian `u16`.
    ///
    /// # Errors
    /// Propagates `put`'s `ARITHMETIC`/`CAPACITY` refusals.
    pub fn u16(&mut self, value: u16) -> CodecResult<()> {
        self.put(&value.to_be_bytes())
    }
    /// Writes a big-endian `u32`.
    ///
    /// # Errors
    /// Propagates `put`'s `ARITHMETIC`/`CAPACITY` refusals.
    pub fn u32(&mut self, value: u32) -> CodecResult<()> {
        self.put(&value.to_be_bytes())
    }
    /// Writes a big-endian `u64`.
    ///
    /// # Errors
    /// Propagates `put`'s `ARITHMETIC`/`CAPACITY` refusals.
    pub fn u64(&mut self, value: u64) -> CodecResult<()> {
        self.put(&value.to_be_bytes())
    }
    /// Writes a big-endian `u128`.
    ///
    /// # Errors
    /// Propagates `put`'s `ARITHMETIC`/`CAPACITY` refusals.
    pub fn u128(&mut self, value: u128) -> CodecResult<()> {
        self.put(&value.to_be_bytes())
    }
    /// Writes a canonical boolean byte.
    ///
    /// # Errors
    /// Propagates `put`'s `ARITHMETIC`/`CAPACITY` refusals.
    pub fn boolean(&mut self, value: bool) -> CodecResult<()> {
        self.u8(u8::from(value))
    }
    /// Writes a presence flag, then the value with `encode` when present.
    ///
    /// # Errors
    /// Propagates `put`'s refusals and any refusal from `encode`.
    pub fn presence<T>(
        &mut self,
        value: &Presence<T>,
        encode: impl FnOnce(&mut Self, &T) -> CodecResult<()>,
    ) -> CodecResult<()> {
        match value {
            Presence::Absent => self.boolean(false),
            Presence::Present(v) => {
                self.boolean(true)?;
                encode(self, v)
            }
        }
    }
    /// Writes a `u32` length-prefixed byte string of at most `cap` bytes.
    ///
    /// # Errors
    /// Returns `CAPACITY` when `bytes` exceeds `cap` or the output is too small; `ARITHMETIC` when the length does not fit `u32` or the offset overflows.
    pub fn bytes(&mut self, bytes: &[u8], cap: usize) -> CodecResult<()> {
        if bytes.len() > cap {
            return Err(CAPACITY);
        }
        self.u32(u32::try_from(bytes.len()).map_err(|_| ARITHMETIC)?)?;
        self.put(bytes)
    }
}
fn output_size(output: &[u8], size: usize, cap: usize) -> CodecResult<()> {
    if size > cap || output.len() < size {
        Err(CAPACITY)
    } else {
        Ok(())
    }
}
fn schema(r: &mut Reader<'_>) -> CodecResult<()> {
    if r.u16()? == SCHEMA_VERSION {
        Ok(())
    } else {
        Err(BAD_VERSION)
    }
}
/// Parses canonical unsigned decimal text.
///
/// # Errors
/// Returns `NON_CANONICAL` for empty, over-39-digit, leading-zero or non-digit text; `ARITHMETIC` when the value overflows `u128`.
pub fn decimal_u128(text: &str) -> CodecResult<u128> {
    let b = text.as_bytes();
    if b.is_empty() || b.len() > 39 || (b.len() > 1 && b[0] == b'0') {
        return Err(NON_CANONICAL);
    }
    let mut value = 0_u128;
    for digit in b {
        if !digit.is_ascii_digit() {
            return Err(NON_CANONICAL);
        }
        value = value
            .checked_mul(10)
            .and_then(|v| v.checked_add(u128::from(*digit - b'0')))
            .ok_or(ARITHMETIC)?;
    }
    Ok(value)
}
/// Parses canonical unsigned decimal text into a `u64`.
///
/// # Errors
/// Returns `NON_CANONICAL` for text over 20 bytes or rejected by `decimal_u128`; `ARITHMETIC` when the value overflows.
pub fn decimal_u64(text: &str) -> CodecResult<u64> {
    if text.len() > 20 {
        return Err(NON_CANONICAL);
    }
    u64::try_from(decimal_u128(text)?).map_err(|_| ARITHMETIC)
}

/// Exact H(D,B). Domain must be nonempty ASCII without an embedded NUL.
///
/// # Errors
/// Returns `NON_CANONICAL` when the domain is empty, non-ASCII or contains NUL, or the digest is all zero.
pub fn domain_hash(domain: &str, bytes: &[u8]) -> CodecResult<Digest32> {
    if domain.is_empty() || !domain.is_ascii() || domain.as_bytes().contains(&0) {
        return Err(NON_CANONICAL);
    }
    let mut h = Sha256::new();
    h.update(domain.as_bytes());
    h.update([0]);
    h.update(bytes);
    Digest32::new(h.finalize().into())
}
fn hash_parts(domain: &str, parts: &[&[u8]]) -> CodecResult<Digest32> {
    let mut h = Sha256::new();
    h.update(domain.as_bytes());
    h.update([0]);
    for part in parts {
        h.update(part);
    }
    Digest32::new(h.finalize().into())
}
/// Derives the market identity from chain and program.
///
/// # Errors
/// Returns `NON_CANONICAL` when the derived digest is all zero.
pub fn derive_market(chain: ChainDomain, program: ProgramId) -> CodecResult<MarketId> {
    MarketId::new(hash_parts("PAXAI/market/v1", &[chain.as_bytes(), program.as_bytes()])?.bytes())
}
/// Derives a worker identity.
///
/// # Errors
/// Returns `NON_CANONICAL` when the derived digest is all zero.
pub fn derive_worker(
    market: MarketId,
    owner: PrincipalId,
    nonce: [u8; 32],
) -> CodecResult<WorkerId> {
    WorkerId::new(
        hash_parts(
            "PAXAI/worker/v1",
            &[market.as_bytes(), owner.as_bytes(), &nonce],
        )?
        .bytes(),
    )
}
/// Derives an evaluator identity.
///
/// # Errors
/// Returns `NON_CANONICAL` when the derived digest is all zero.
pub fn derive_evaluator(
    market: MarketId,
    owner: PrincipalId,
    nonce: [u8; 32],
) -> CodecResult<EvaluatorId> {
    EvaluatorId::new(
        hash_parts(
            "PAXAI/evaluator/v1",
            &[market.as_bytes(), owner.as_bytes(), &nonce],
        )?
        .bytes(),
    )
}
/// Derives a task identity.
///
/// # Errors
/// Returns `NON_CANONICAL` when the derived digest is all zero.
pub fn derive_task(
    market: MarketId,
    epoch: u64,
    requester: PrincipalId,
    nonce: [u8; 32],
) -> CodecResult<TaskId> {
    TaskId::new(
        hash_parts(
            "PAXAI/task/v1",
            &[
                market.as_bytes(),
                &epoch.to_be_bytes(),
                requester.as_bytes(),
                &nonce,
            ],
        )?
        .bytes(),
    )
}
/// State owner calls this before inserting a derived identity. No retry nonce is invented.
///
/// # Errors
/// Returns `NON_CANONICAL` when `candidate` is all zero; `CONFLICT` when it already exists.
pub fn refuse_identity_collision(candidate: &[u8; 32], existing: &[[u8; 32]]) -> CodecResult<()> {
    if *candidate == [0; 32] {
        return Err(NON_CANONICAL);
    }
    if existing.contains(candidate) {
        Err(CONFLICT)
    } else {
        Ok(())
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct Envelope<'a> {
    pub operation: Operation,
    pub chain: ChainDomain,
    pub program: ProgramId,
    pub market: MarketId,
    pub actor: PrincipalId,
    pub epoch: u64,
    pub config: u64,
    pub roster: Presence<RosterDigest>,
    pub sequence: u64,
    pub expiry: u64,
    pub request: RequestId,
    pub payload: &'a [u8],
    pub authentication: Authentication,
}
#[derive(Clone, Copy, Debug)]
pub struct ValidatedEnvelope<'a> {
    pub envelope: Envelope<'a>,
    unsigned: &'a [u8],
}
impl<'a> ValidatedEnvelope<'a> {
    #[must_use]
    pub const fn unsigned_bytes(&self) -> &'a [u8] {
        self.unsigned
    }
    /// Request digest over the unsigned envelope bytes.
    ///
    /// # Errors
    /// Returns `NON_CANONICAL` when the digest is all zero.
    pub fn request_digest(&self) -> CodecResult<RequestDigest> {
        RequestDigest::new(domain_hash("PAXAI/request/v1", self.unsigned)?.bytes())
    }
}
fn read_optional_roster(r: &mut Reader<'_>) -> CodecResult<Presence<RosterDigest>> {
    let b = r.fixed()?;
    if b == [0; 32] {
        Ok(Presence::Absent)
    } else {
        Ok(Presence::Present(RosterDigest::new(b)?))
    }
}
impl Envelope<'_> {
    /// Validates envelope structure and the fixed payloads it owns.
    ///
    /// # Errors
    /// Returns `NON_CANONICAL` for a bad payload length, zero expiry, or sequence, read-boundary or config violations; `UNAUTHORIZED` for a delegate on a non-delegable operation; `WRONG_ROSTER` for a missing roster outside pre-epoch registry calls; propagates chunk-request, score payload and binding refusals.
    pub fn validate(&self) -> CodecResult<()> {
        let m = self.operation.metadata();
        self.operation.validate_payload_length(self.payload.len())?;
        if self.payload.len() > MAX_PAYLOAD_BYTES || self.expiry == 0 {
            return Err(NON_CANONICAL);
        }
        if m.sequence == SequencePolicy::Role && self.sequence == 0 {
            return Err(NON_CANONICAL);
        }
        if m.sequence == SequencePolicy::ObjectLocal && self.sequence != 0 {
            return Err(NON_CANONICAL);
        }
        if matches!(self.authentication, Authentication::Delegate { .. }) && !m.delegate_allowed {
            return Err(UNAUTHORIZED);
        }
        if m.boundary == CallBoundary::ProgramRead {
            if self.epoch != 0
                || self.config != 0
                || self.roster != Presence::Absent
                || self.authentication != Authentication::Native
            {
                return Err(NON_CANONICAL);
            }
        } else if self.config == 0 {
            return Err(NON_CANONICAL);
        }
        if self.roster == Presence::Absent
            && (self.epoch != 0 || !registry_before_epoch(self.operation.selector()))
        {
            return Err(WRONG_ROSTER);
        }
        match self.operation.selector() {
            0x0a02 => {
                decode_chunk_request(self.payload)?;
            }
            0x0401 => {
                let c = decode_commit_score(self.payload)?;
                c.binding.matches_envelope(self)?;
            }
            0x0402 => {
                let v = decode_reveal_score(self.payload)?;
                v.report.binding.matches_envelope(self)?;
            }
            _ => {}
        }
        Ok(())
    }
    /// Pure comparison. `height` is not represented as authenticated Context.
    ///
    /// # Errors
    /// Returns `EXPIRED` when `expiry` is zero or `height` has reached it.
    pub fn check_expiry(&self, height: u64) -> CodecResult<()> {
        if self.expiry == 0 || height >= self.expiry {
            Err(EXPIRED)
        } else {
            Ok(())
        }
    }
    /// Pure binding comparison; the host adapter must obtain authoritative values.
    ///
    /// # Errors
    /// Returns `WRONG_DOMAIN`, `WRONG_PROGRAM` or `WRONG_MARKET` for the first mismatch, in that order.
    pub fn check_domain(
        &self,
        chain: ChainDomain,
        program: ProgramId,
        market: MarketId,
    ) -> CodecResult<()> {
        if self.chain != chain {
            Err(WRONG_DOMAIN)
        } else if self.program != program {
            Err(WRONG_PROGRAM)
        } else if self.market != market {
            Err(WRONG_MARKET)
        } else {
            Ok(())
        }
    }
}
fn registry_before_epoch(selector: u16) -> bool {
    matches!(selector,0x0101..=0x010b|0x0111|0x0201..=0x0207|0x0209..=0x020a|0x0301..=0x0303|0x0601|0x0604|0x0801..=0x0804|0x0a01..=0x0a02)
}
/// Decodes and validates a request envelope.
///
/// # Errors
/// Returns `CAPACITY` when the input or payload exceeds its cap; `NON_CANONICAL` for a bad magic, zero identity, unknown authentication kind, short or trailing input; `BAD_VERSION` for a wrong schema; `UNKNOWN_OPERATION` for an unknown selector; propagates `Envelope::validate` refusals.
pub fn decode_envelope(input: &[u8]) -> CodecResult<ValidatedEnvelope<'_>> {
    if input.len() > MAX_ENVELOPE_BYTES {
        return Err(CAPACITY);
    }
    let mut r = Reader::new(input);
    if r.take(6)? != b"PAXAI1" {
        return Err(NON_CANONICAL);
    }
    schema(&mut r)?;
    let operation = Operation::decode(r.u16()?)?;
    let chain = ChainDomain::new(r.fixed()?)?;
    let program = ProgramId::new(r.fixed()?)?;
    let market = MarketId::new(r.fixed()?)?;
    let actor = PrincipalId::new(r.fixed()?)?;
    let epoch = r.u64()?;
    let config = r.u64()?;
    let roster = read_optional_roster(&mut r)?;
    let sequence = r.u64()?;
    let expiry = r.u64()?;
    let request = RequestId::new(r.fixed()?)?;
    let payload = r.bytes(MAX_PAYLOAD_BYTES)?;
    let unsigned = &input[..r.offset()];
    let authentication = match r.u8()? {
        0 => Authentication::Native,
        1 => Authentication::Delegate {
            key: PublicKey32(r.fixed()?),
            signature: Signature64(r.fixed()?),
        },
        _ => return Err(NON_CANONICAL),
    };
    r.finish()?;
    let envelope = Envelope {
        operation,
        chain,
        program,
        market,
        actor,
        epoch,
        config,
        roster,
        sequence,
        expiry,
        request,
        payload,
        authentication,
    };
    envelope.validate()?;
    Ok(ValidatedEnvelope { envelope, unsigned })
}
/// Encodes a validated envelope into `output`.
///
/// # Errors
/// Propagates `Envelope::validate` refusals; returns `ARITHMETIC` on size overflow; `CAPACITY` when the size exceeds `MAX_ENVELOPE_BYTES` or `output` is too small.
pub fn encode_envelope(value: &Envelope<'_>, output: &mut [u8]) -> CodecResult<usize> {
    value.validate()?;
    let size = ENVELOPE_PREFIX_BYTES
        .checked_add(value.payload.len())
        .and_then(|n| {
            n.checked_add(if matches!(value.authentication, Authentication::Native) {
                1
            } else {
                97
            })
        })
        .ok_or(ARITHMETIC)?;
    output_size(output, size, MAX_ENVELOPE_BYTES)?;
    let mut w = Writer::new(output);
    w.put(b"PAXAI1")?;
    w.u16(SCHEMA_VERSION)?;
    w.u16(value.operation.selector())?;
    w.put(value.chain.as_bytes())?;
    w.put(value.program.as_bytes())?;
    w.put(value.market.as_bytes())?;
    w.put(value.actor.as_bytes())?;
    w.u64(value.epoch)?;
    w.u64(value.config)?;
    match value.roster {
        Presence::Absent => w.put(&[0; 32])?,
        Presence::Present(d) => w.put(d.as_bytes())?,
    }
    w.u64(value.sequence)?;
    w.u64(value.expiry)?;
    w.put(value.request.as_bytes())?;
    w.bytes(value.payload, MAX_PAYLOAD_BYTES)?;
    match value.authentication {
        Authentication::Native => w.u8(0)?,
        Authentication::Delegate { key, signature } => {
            w.u8(1)?;
            w.put(&key.0)?;
            w.put(&signature.0)?;
        }
    }
    Ok(w.len())
}

fn write_evaluator_binding(w: &mut Writer<'_>, b: &EvaluatorBinding) -> CodecResult<()> {
    w.put(b.frozen.chain.as_bytes())?;
    w.put(b.frozen.program.as_bytes())?;
    w.put(b.frozen.market.as_bytes())?;
    w.u64(b.frozen.epoch)?;
    w.u64(b.frozen.config.get())?;
    w.put(b.frozen.roster.as_bytes())?;
    w.put(b.evaluator.as_bytes())?;
    w.u64(b.grant.get())?;
    w.u64(b.key_version.get())
}
fn read_evaluator_binding(r: &mut Reader<'_>) -> CodecResult<EvaluatorBinding> {
    let chain = ChainDomain::new(r.fixed()?)?;
    let program = ProgramId::new(r.fixed()?)?;
    let market = MarketId::new(r.fixed()?)?;
    let epoch = r.u64()?;
    let config = Version::new(r.u64()?)?;
    let roster = RosterDigest::new(r.fixed()?)?;
    Ok(EvaluatorBinding {
        frozen: FrozenBinding {
            chain,
            program,
            market,
            epoch,
            config,
            roster,
        },
        evaluator: EvaluatorId::new(r.fixed()?)?,
        grant: Version::new(r.u64()?)?,
        key_version: Version::new(r.u64()?)?,
    })
}
impl EvaluatorBinding {
    /// Checks the envelope against this frozen binding.
    ///
    /// # Errors
    /// Propagates `check_domain` refusals; returns `WRONG_EPOCH`, `WRONG_CONFIG` or `WRONG_ROSTER` for the first mismatch, in that order.
    pub fn matches_envelope(&self, e: &Envelope<'_>) -> CodecResult<()> {
        e.check_domain(self.frozen.chain, self.frozen.program, self.frozen.market)?;
        if e.epoch != self.frozen.epoch {
            Err(WRONG_EPOCH)
        } else if e.config != self.frozen.config.get() {
            Err(WRONG_CONFIG)
        } else if e.roster != Presence::Present(self.frozen.roster) {
            Err(WRONG_ROSTER)
        } else {
            Ok(())
        }
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum ScoreVector<'a> {
    Typed(&'a [ScoreEntry]),
    Encoded(&'a [u8]),
}
impl ScoreVector<'_> {
    #[must_use]
    pub fn len(&self) -> usize {
        match self {
            Self::Typed(v) => v.len(),
            Self::Encoded(v) => v.len() / 36,
        }
    }
    #[must_use]
    pub fn is_empty(&self) -> bool {
        self.len() == 0
    }
    /// Validates a nonempty, bounded, strictly worker-ordered score vector.
    ///
    /// # Errors
    /// Returns `NON_CANONICAL` for a ragged encoding, zero worker or unordered workers; `F03_NO_SCORES` when empty; `CAPACITY` above `MAX_WORKERS`; `F03_SCORE_RANGE` for an out-of-range score.
    pub fn validate(&self) -> CodecResult<()> {
        if let Self::Encoded(v) = self {
            if v.len() % 36 != 0 {
                return Err(NON_CANONICAL);
            }
        }
        if self.is_empty() {
            return Err(F03_NO_SCORES);
        }
        if self.len() > MAX_WORKERS {
            return Err(CAPACITY);
        }
        let mut last = None;
        for entry in self.entries() {
            let e = entry?;
            if last.is_some_and(|p| p >= e.worker) {
                return Err(NON_CANONICAL);
            }
            last = Some(e.worker);
        }
        Ok(())
    }
    #[must_use]
    pub fn entries(&self) -> ScoreEntries<'_> {
        ScoreEntries {
            vector: *self,
            index: 0,
        }
    }
}
pub struct ScoreEntries<'a> {
    vector: ScoreVector<'a>,
    index: usize,
}
impl Iterator for ScoreEntries<'_> {
    type Item = CodecResult<ScoreEntry>;
    fn next(&mut self) -> Option<Self::Item> {
        if self.index >= self.vector.len() {
            return None;
        }
        let index = self.index;
        self.index += 1;
        Some(match self.vector {
            ScoreVector::Typed(v) => Ok(v[index]),
            ScoreVector::Encoded(v) => {
                let mut r = Reader::new(&v[index * 36..(index + 1) * 36]);
                read_score_entry(&mut r)
            }
        })
    }
}
fn read_score_entry(r: &mut Reader<'_>) -> CodecResult<ScoreEntry> {
    Ok(ScoreEntry {
        worker: WorkerId::new(r.fixed()?)?,
        score: Score::new(r.u32()?)?,
    })
}
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct ReportBody<'a> {
    pub binding: EvaluatorBinding,
    pub evidence: EvidenceRoot,
    pub scores: ScoreVector<'a>,
}
/// Encodes a score report into `output`.
///
/// # Errors
/// Propagates `ScoreVector::validate` refusals; returns `CAPACITY` when `output` is too small.
pub fn encode_report(report: &ReportBody<'_>, output: &mut [u8]) -> CodecResult<usize> {
    report.scores.validate()?;
    let size = REPORT_FIXED_BYTES + 36 * report.scores.len();
    output_size(output, size, REPORT_MAX_BYTES)?;
    let mut w = Writer::new(output);
    w.u16(SCHEMA_VERSION)?;
    write_evaluator_binding(&mut w, &report.binding)?;
    w.put(report.evidence.as_bytes())?;
    w.u16(u16::try_from(report.scores.len()).map_err(|_| ARITHMETIC)?)?;
    for entry in report.scores.entries() {
        let e = entry?;
        w.put(e.worker.as_bytes())?;
        w.u32(e.score.get())?;
    }
    Ok(w.len())
}
/// Decodes and validates a score report.
///
/// # Errors
/// Returns `CAPACITY` when the input exceeds `REPORT_MAX_BYTES` or the count exceeds `MAX_WORKERS`; `BAD_VERSION` for a wrong schema; `F03_NO_SCORES` for a zero count; `NON_CANONICAL` for a zero identity, digest or version, short or trailing input; propagates `ScoreVector::validate` refusals.
pub fn decode_report(input: &[u8]) -> CodecResult<ReportBody<'_>> {
    if input.len() > REPORT_MAX_BYTES {
        return Err(CAPACITY);
    }
    let mut r = Reader::new(input);
    schema(&mut r)?;
    let binding = read_evaluator_binding(&mut r)?;
    let evidence = EvidenceRoot::new(r.fixed()?)?;
    let count = usize::from(r.u16()?);
    if count == 0 {
        return Err(F03_NO_SCORES);
    }
    if count > MAX_WORKERS {
        return Err(CAPACITY);
    }
    let scores = ScoreVector::Encoded(r.take(count.checked_mul(36).ok_or(ARITHMETIC)?)?);
    r.finish()?;
    scores.validate()?;
    Ok(ReportBody {
        binding,
        evidence,
        scores,
    })
}
/// Digest of the canonical report encoding.
///
/// # Errors
/// Propagates `encode_report` refusals; returns `NON_CANONICAL` when the digest is all zero.
pub fn report_digest(report: &ReportBody<'_>) -> CodecResult<ReportDigest> {
    let mut bytes = [0; REPORT_MAX_BYTES];
    let n = encode_report(report, &mut bytes)?;
    ReportDigest::new(domain_hash("PAXAI/score-report/v1", &bytes[..n])?.bytes())
}
/// Attestation digest over a report digest.
///
/// # Errors
/// Returns `NON_CANONICAL` when the digest is all zero.
pub fn attestation_digest(report: ReportDigest) -> CodecResult<AttestationDigest> {
    AttestationDigest::new(domain_hash("PAXAI/score-attestation/v1", report.as_bytes())?.bytes())
}
/// Encodes the 256-byte commitment preimage.
///
/// # Errors
/// Returns `CAPACITY` when `output` is shorter than 256 bytes.
pub fn encode_commitment_preimage(
    binding: &EvaluatorBinding,
    report: ReportDigest,
    salt: Salt32,
    output: &mut [u8],
) -> CodecResult<usize> {
    output_size(output, 256, 256)?;
    let mut w = Writer::new(output);
    write_evaluator_binding(&mut w, binding)?;
    w.put(report.as_bytes())?;
    w.put(&salt.bytes())?;
    Ok(w.len())
}
/// Commitment digest over the binding, report digest and salt.
///
/// # Errors
/// Returns `NON_CANONICAL` when the digest is all zero.
pub fn commitment_digest(
    binding: &EvaluatorBinding,
    report: ReportDigest,
    salt: Salt32,
) -> CodecResult<CommitmentDigest> {
    let mut b = [0; 256];
    let n = encode_commitment_preimage(binding, report, salt, &mut b)?;
    CommitmentDigest::new(domain_hash("PAXAI/score-commit/v1", &b[..n])?.bytes())
}
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct CommitScorePayload {
    pub binding: EvaluatorBinding,
    pub commitment: CommitmentDigest,
}
/// Encodes a commit-score payload.
///
/// # Errors
/// Returns `CAPACITY` when `out` is shorter than 224 bytes.
pub fn encode_commit_score(v: &CommitScorePayload, out: &mut [u8]) -> CodecResult<usize> {
    output_size(out, 224, 224)?;
    let mut w = Writer::new(out);
    write_evaluator_binding(&mut w, &v.binding)?;
    w.put(v.commitment.as_bytes())?;
    Ok(w.len())
}
/// Decodes a commit-score payload.
///
/// # Errors
/// Returns `NON_CANONICAL` for a zero identity, digest or version, short or trailing input.
pub fn decode_commit_score(input: &[u8]) -> CodecResult<CommitScorePayload> {
    let mut r = Reader::new(input);
    let binding = read_evaluator_binding(&mut r)?;
    let commitment = CommitmentDigest::new(r.fixed()?)?;
    r.finish()?;
    Ok(CommitScorePayload {
        binding,
        commitment,
    })
}
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct RevealScorePayload<'a> {
    pub report: ReportBody<'a>,
    pub signature: Signature64,
    pub salt: Salt32,
}
/// Decodes a reveal-score payload.
///
/// # Errors
/// Returns `CAPACITY` when the input exceeds 1480 bytes or the report exceeds `REPORT_MAX_BYTES`; `F04_SALT_INVALID` for a zero salt; `NON_CANONICAL` for short or trailing input; propagates `decode_report` refusals.
pub fn decode_reveal_score(input: &[u8]) -> CodecResult<RevealScorePayload<'_>> {
    if input.len() > 1480 {
        return Err(CAPACITY);
    }
    let mut r = Reader::new(input);
    let report = decode_report(r.bytes(REPORT_MAX_BYTES)?)?;
    let signature = Signature64(r.fixed()?);
    let salt = Salt32::new(r.fixed()?)?;
    r.finish()?;
    Ok(RevealScorePayload {
        report,
        signature,
        salt,
    })
}
/// Encodes a reveal-score payload.
///
/// # Errors
/// Propagates `ScoreVector::validate` and `encode_report` refusals; returns `CAPACITY` when `out` is too small.
pub fn encode_reveal_score(v: &RevealScorePayload<'_>, out: &mut [u8]) -> CodecResult<usize> {
    v.report.scores.validate()?;
    let size = 4 + REPORT_FIXED_BYTES + v.report.scores.len() * 36 + 96;
    output_size(out, size, 1480)?;
    let mut body = [0; REPORT_MAX_BYTES];
    let n = encode_report(&v.report, &mut body)?;
    let mut w = Writer::new(out);
    w.bytes(&body[..n], REPORT_MAX_BYTES)?;
    w.put(&v.signature.0)?;
    w.put(&v.salt.bytes())?;
    Ok(w.len())
}

/// Encodes one worker roster entry.
///
/// # Errors
/// Returns `CAPACITY` when `out` is shorter than 176 bytes.
pub fn encode_worker_roster(v: &WorkerRosterEntry, out: &mut [u8]) -> CodecResult<usize> {
    output_size(out, 176, 176)?;
    let mut w = Writer::new(out);
    write_worker_roster(&mut w, v)?;
    Ok(w.len())
}
fn write_worker_roster(w: &mut Writer<'_>, v: &WorkerRosterEntry) -> CodecResult<()> {
    w.put(v.worker.as_bytes())?;
    w.put(v.owner.as_bytes())?;
    w.put(v.recipient.as_bytes())?;
    w.u64(v.generation.get())?;
    w.u64(v.key_version.get())?;
    w.put(&v.public_key.0)?;
    w.put(v.metadata.as_bytes())
}
fn read_worker_roster(r: &mut Reader<'_>) -> CodecResult<WorkerRosterEntry> {
    Ok(WorkerRosterEntry {
        worker: WorkerId::new(r.fixed()?)?,
        owner: PrincipalId::new(r.fixed()?)?,
        recipient: AccountId::new(r.fixed()?)?,
        generation: Version::new(r.u64()?)?,
        key_version: Version::new(r.u64()?)?,
        public_key: PublicKey32(r.fixed()?),
        metadata: MetadataDigest::new(r.fixed()?)?,
    })
}
/// Decodes one worker roster entry.
///
/// # Errors
/// Returns `NON_CANONICAL` for a zero identity, version or digest, short or trailing input.
pub fn decode_worker_roster(input: &[u8]) -> CodecResult<WorkerRosterEntry> {
    let mut r = Reader::new(input);
    let v = read_worker_roster(&mut r)?;
    r.finish()?;
    Ok(v)
}
/// Encodes one evaluator roster entry.
///
/// # Errors
/// Returns `CAPACITY` when `out` is shorter than 144 bytes.
pub fn encode_evaluator_roster(v: &EvaluatorRosterEntry, out: &mut [u8]) -> CodecResult<usize> {
    output_size(out, 144, 144)?;
    let mut w = Writer::new(out);
    write_evaluator_roster(&mut w, v)?;
    Ok(w.len())
}
fn write_evaluator_roster(w: &mut Writer<'_>, v: &EvaluatorRosterEntry) -> CodecResult<()> {
    w.put(v.evaluator.as_bytes())?;
    w.put(v.owner.as_bytes())?;
    w.u64(v.grant.get())?;
    w.u64(v.key_version.get())?;
    w.put(&v.public_key.0)?;
    w.put(v.rubric.as_bytes())
}
fn read_evaluator_roster(r: &mut Reader<'_>) -> CodecResult<EvaluatorRosterEntry> {
    Ok(EvaluatorRosterEntry {
        evaluator: EvaluatorId::new(r.fixed()?)?,
        owner: PrincipalId::new(r.fixed()?)?,
        grant: Version::new(r.u64()?)?,
        key_version: Version::new(r.u64()?)?,
        public_key: PublicKey32(r.fixed()?),
        rubric: RubricDigest::new(r.fixed()?)?,
    })
}
/// Decodes one evaluator roster entry.
///
/// # Errors
/// Returns `NON_CANONICAL` for a zero identity, version or digest, short or trailing input.
pub fn decode_evaluator_roster(input: &[u8]) -> CodecResult<EvaluatorRosterEntry> {
    let mut r = Reader::new(input);
    let v = read_evaluator_roster(&mut r)?;
    r.finish()?;
    Ok(v)
}
#[derive(Clone, Copy, Debug)]
pub struct Roster<'a> {
    pub market: MarketId,
    pub epoch: u64,
    pub config: Version,
    pub workers: &'a [WorkerRosterEntry],
    pub evaluators: &'a [EvaluatorRosterEntry],
}
impl Roster<'_> {
    /// Validates roster bounds and strict ordering.
    ///
    /// # Errors
    /// Returns `CAPACITY` above 32 workers or 8 evaluators; `NON_CANONICAL` for unordered or duplicate entries.
    pub fn validate(&self) -> CodecResult<()> {
        if self.workers.len() > 32 || self.evaluators.len() > 8 {
            return Err(CAPACITY);
        }
        if self.workers.windows(2).any(|p| p[0].worker >= p[1].worker)
            || self
                .evaluators
                .windows(2)
                .any(|p| p[0].evaluator >= p[1].evaluator)
        {
            return Err(NON_CANONICAL);
        }
        Ok(())
    }
}
pub const ROSTER_MAX_BYTES: usize = 54 + 32 * 176 + 8 * 144;
/// Encodes a roster into `out`.
///
/// # Errors
/// Propagates `Roster::validate` refusals; returns `CAPACITY` when `out` is too small.
pub fn encode_roster(v: &Roster<'_>, out: &mut [u8]) -> CodecResult<usize> {
    v.validate()?;
    let n = 54 + v.workers.len() * 176 + v.evaluators.len() * 144;
    output_size(out, n, ROSTER_MAX_BYTES)?;
    let mut w = Writer::new(out);
    w.u16(SCHEMA_VERSION)?;
    w.put(v.market.as_bytes())?;
    w.u64(v.epoch)?;
    w.u64(v.config.get())?;
    w.u16(u16::try_from(v.workers.len()).map_err(|_| ARITHMETIC)?)?;
    for entry in v.workers {
        write_worker_roster(&mut w, entry)?;
    }
    w.u16(u16::try_from(v.evaluators.len()).map_err(|_| ARITHMETIC)?)?;
    for entry in v.evaluators {
        write_evaluator_roster(&mut w, entry)?;
    }
    Ok(w.len())
}
#[derive(Clone, Copy, Debug)]
pub struct RosterView<'a> {
    pub market: MarketId,
    pub epoch: u64,
    pub config: Version,
    workers: &'a [u8],
    evaluators: &'a [u8],
    canonical: &'a [u8],
}
impl RosterView<'_> {
    #[must_use]
    pub fn worker_count(&self) -> usize {
        self.workers.len() / 176
    }
    #[must_use]
    pub fn evaluator_count(&self) -> usize {
        self.evaluators.len() / 144
    }
    /// Decodes the worker entry at `index`.
    ///
    /// # Errors
    /// Returns `ARITHMETIC` when the offset overflows; `NON_CANONICAL` when `index` is out of range; propagates `decode_worker_roster` refusals.
    pub fn worker(&self, index: usize) -> CodecResult<WorkerRosterEntry> {
        let start = index.checked_mul(176).ok_or(ARITHMETIC)?;
        let end = start.checked_add(176).ok_or(ARITHMETIC)?;
        decode_worker_roster(self.workers.get(start..end).ok_or(NON_CANONICAL)?)
    }
    /// Decodes the evaluator entry at `index`.
    ///
    /// # Errors
    /// Returns `ARITHMETIC` when the offset overflows; `NON_CANONICAL` when `index` is out of range; propagates `decode_evaluator_roster` refusals.
    pub fn evaluator(&self, index: usize) -> CodecResult<EvaluatorRosterEntry> {
        let start = index.checked_mul(144).ok_or(ARITHMETIC)?;
        let end = start.checked_add(144).ok_or(ARITHMETIC)?;
        decode_evaluator_roster(self.evaluators.get(start..end).ok_or(NON_CANONICAL)?)
    }
    /// Roster digest over the canonical bytes.
    ///
    /// # Errors
    /// Returns `NON_CANONICAL` when the digest is all zero.
    pub fn digest(&self) -> CodecResult<RosterDigest> {
        RosterDigest::new(domain_hash("PAXAI/roster/v1", self.canonical)?.bytes())
    }
}
/// Decodes and validates a roster.
///
/// # Errors
/// Returns `CAPACITY` when the input exceeds `ROSTER_MAX_BYTES` or a count exceeds its cap; `BAD_VERSION` for a wrong schema; `NON_CANONICAL` for a zero identity or version, malformed or unordered entries, short or trailing input.
pub fn decode_roster(input: &[u8]) -> CodecResult<RosterView<'_>> {
    if input.len() > ROSTER_MAX_BYTES {
        return Err(CAPACITY);
    }
    let mut r = Reader::new(input);
    schema(&mut r)?;
    let market = MarketId::new(r.fixed()?)?;
    let epoch = r.u64()?;
    let config = Version::new(r.u64()?)?;
    let workers = r.vector_bytes(0, 32, 176)?;
    let evaluators = r.vector_bytes(0, 8, 144)?;
    r.finish()?;
    let v = RosterView {
        market,
        epoch,
        config,
        workers,
        evaluators,
        canonical: input,
    };
    let mut last = None;
    for i in 0..v.worker_count() {
        let e = v.worker(i)?;
        if last.is_some_and(|p| p >= e.worker) {
            return Err(NON_CANONICAL);
        }
        last = Some(e.worker);
    }
    let mut last = None;
    for i in 0..v.evaluator_count() {
        let e = v.evaluator(i)?;
        if last.is_some_and(|p| p >= e.evaluator) {
            return Err(NON_CANONICAL);
        }
        last = Some(e.evaluator);
    }
    Ok(v)
}
/// Roster digest over the canonical encoding.
///
/// # Errors
/// Propagates `encode_roster` refusals; returns `NON_CANONICAL` when the digest is all zero.
pub fn roster_digest(v: &Roster<'_>) -> CodecResult<RosterDigest> {
    let mut b = [0; ROSTER_MAX_BYTES];
    let n = encode_roster(v, &mut b)?;
    RosterDigest::new(domain_hash("PAXAI/roster/v1", &b[..n])?.bytes())
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
#[repr(u16)]
pub enum ResultStatus {
    Ok = 0,
    AlreadyApplied = 1,
    Error = 2,
}
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct ApplicationResult<'a> {
    pub status: ResultStatus,
    pub error: Option<ApplicationError>,
    pub request: Presence<RequestDigest>,
    pub revision: u64,
    pub digest: ResultDigest,
    pub payload: &'a [u8],
}
/// Result digest over a success payload.
///
/// # Errors
/// Returns `CAPACITY` when `payload` exceeds `MAX_RESULT_PAYLOAD_BYTES`; `NON_CANONICAL` when the digest is all zero.
pub fn result_digest(payload: &[u8]) -> CodecResult<ResultDigest> {
    if payload.len() > MAX_RESULT_PAYLOAD_BYTES {
        return Err(CAPACITY);
    }
    ResultDigest::new(domain_hash("PAXAI/result/v1", payload)?.bytes())
}
fn request_bytes(request: Presence<RequestDigest>) -> [u8; 32] {
    match request {
        Presence::Absent => [0; 32],
        Presence::Present(d) => d.bytes(),
    }
}
/// Result digest for an error outcome.
///
/// # Errors
/// Returns `NON_CANONICAL` when the digest is all zero.
pub fn error_digest(
    error: ApplicationError,
    request: Presence<RequestDigest>,
) -> CodecResult<ResultDigest> {
    ResultDigest::new(
        hash_parts(
            "PAXAI/error/v1",
            &[&error.code().to_be_bytes(), &request_bytes(request)],
        )?
        .bytes(),
    )
}
impl<'a> ApplicationResult<'a> {
    /// Builds a success result.
    ///
    /// # Errors
    /// Returns `NON_CANONICAL` when `status` is `Error`; propagates `result_digest` refusals.
    pub fn success(
        status: ResultStatus,
        request: RequestDigest,
        revision: u64,
        payload: &'a [u8],
    ) -> CodecResult<Self> {
        if status == ResultStatus::Error {
            return Err(NON_CANONICAL);
        }
        Ok(Self {
            status,
            error: None,
            request: Presence::Present(request),
            revision,
            digest: result_digest(payload)?,
            payload,
        })
    }
    /// Builds an error result, hiding the revision when required.
    ///
    /// # Errors
    /// Propagates `error_digest` refusals.
    pub fn failure(
        error: ApplicationError,
        request: Presence<RequestDigest>,
        visible_revision: u64,
    ) -> CodecResult<Self> {
        let revision = if request == Presence::Absent || error == UNAUTHORIZED {
            0
        } else {
            visible_revision
        };
        Ok(Self {
            status: ResultStatus::Error,
            error: Some(error),
            request,
            revision,
            digest: error_digest(error, request)?,
            payload: &[],
        })
    }
    /// Validates status, error, revision, payload and digest consistency.
    ///
    /// # Errors
    /// Returns `CAPACITY` when the payload exceeds its cap; `NON_CANONICAL` for a status/error mismatch, error payload, unhidden revision, absent success request or digest mismatch; propagates digest refusals.
    pub fn validate(&self) -> CodecResult<()> {
        if self.payload.len() > MAX_RESULT_PAYLOAD_BYTES {
            return Err(CAPACITY);
        }
        let expected = match (self.status, self.error) {
            (ResultStatus::Error, Some(error)) => {
                if !self.payload.is_empty()
                    || ((self.request == Presence::Absent || error == UNAUTHORIZED)
                        && self.revision != 0)
                {
                    return Err(NON_CANONICAL);
                }
                error_digest(error, self.request)?
            }
            (ResultStatus::Ok | ResultStatus::AlreadyApplied, None) => {
                if self.request == Presence::Absent {
                    return Err(NON_CANONICAL);
                }
                result_digest(self.payload)?
            }
            _ => return Err(NON_CANONICAL),
        };
        if expected == self.digest {
            Ok(())
        } else {
            Err(NON_CANONICAL)
        }
    }
}
/// Encodes a validated result into `out`.
///
/// # Errors
/// Propagates `ApplicationResult::validate` refusals; returns `CAPACITY` when `out` is too small.
pub fn encode_result(v: &ApplicationResult<'_>, out: &mut [u8]) -> CodecResult<usize> {
    v.validate()?;
    output_size(out, 82 + v.payload.len(), MAX_RESULT_BYTES)?;
    let mut w = Writer::new(out);
    w.u16(SCHEMA_VERSION)?;
    w.u16(v.status as u16)?;
    w.u16(v.error.map_or(0, ApplicationError::code))?;
    w.put(&request_bytes(v.request))?;
    w.u64(v.revision)?;
    w.put(v.digest.as_bytes())?;
    w.bytes(v.payload, MAX_RESULT_PAYLOAD_BYTES)?;
    Ok(w.len())
}
/// Decodes and validates a result.
///
/// # Errors
/// Returns `CAPACITY` when the input or payload exceeds its cap; `BAD_VERSION` for a wrong schema; `NON_CANONICAL` for an unknown status or error code, zero digest, short or trailing input; propagates `ApplicationResult::validate` refusals.
pub fn decode_result(input: &[u8]) -> CodecResult<ApplicationResult<'_>> {
    if input.len() > MAX_RESULT_BYTES {
        return Err(CAPACITY);
    }
    let mut r = Reader::new(input);
    schema(&mut r)?;
    let status = match r.u16()? {
        0 => ResultStatus::Ok,
        1 => ResultStatus::AlreadyApplied,
        2 => ResultStatus::Error,
        _ => return Err(NON_CANONICAL),
    };
    let code = r.u16()?;
    let error = if code == 0 {
        None
    } else {
        Some(ApplicationError::from_code(code)?)
    };
    let bytes = r.fixed()?;
    let request = if bytes == [0; 32] {
        Presence::Absent
    } else {
        Presence::Present(RequestDigest::new(bytes)?)
    };
    let revision = r.u64()?;
    let digest = ResultDigest::new(r.fixed()?)?;
    let payload = r.bytes(MAX_RESULT_PAYLOAD_BYTES)?;
    r.finish()?;
    let v = ApplicationResult {
        status,
        error,
        request,
        revision,
        digest,
        payload,
    };
    v.validate()?;
    Ok(v)
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct EventCommon {
    pub market: MarketId,
    pub epoch: u64,
    pub config: Version,
    pub revision: u64,
    pub request: RequestDigest,
    pub result: ResultDigest,
}
fn write_event_common(w: &mut Writer<'_>, v: &EventCommon) -> CodecResult<()> {
    w.u16(SCHEMA_VERSION)?;
    w.put(v.market.as_bytes())?;
    w.u64(v.epoch)?;
    w.u64(v.config.get())?;
    w.u64(v.revision)?;
    w.put(v.request.as_bytes())?;
    w.put(v.result.as_bytes())
}
fn read_event_common(r: &mut Reader<'_>) -> CodecResult<EventCommon> {
    schema(r)?;
    Ok(EventCommon {
        market: MarketId::new(r.fixed()?)?,
        epoch: r.u64()?,
        config: Version::new(r.u64()?)?,
        revision: r.u64()?,
        request: RequestDigest::new(r.fixed()?)?,
        result: ResultDigest::new(r.fixed()?)?,
    })
}
/// Encodes the common event header.
///
/// # Errors
/// Returns `CAPACITY` when `out` is shorter than 122 bytes.
pub fn encode_event_common(v: &EventCommon, out: &mut [u8]) -> CodecResult<usize> {
    output_size(out, 122, 122)?;
    let mut w = Writer::new(out);
    write_event_common(&mut w, v)?;
    Ok(w.len())
}
/// Decodes the common event header.
///
/// # Errors
/// Returns `BAD_VERSION` for a wrong schema; `NON_CANONICAL` for a zero identity, version or digest, short or trailing input.
pub fn decode_event_common(input: &[u8]) -> CodecResult<EventCommon> {
    let mut r = Reader::new(input);
    let v = read_event_common(&mut r)?;
    r.finish()?;
    Ok(v)
}
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct CommitAccepted {
    pub common: EventCommon,
    pub evaluator: EvaluatorId,
    pub commitment: CommitmentDigest,
    pub accepted_height: u64,
}
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct RevealScoreEvent {
    pub common: EventCommon,
    pub evaluator: EvaluatorId,
    pub report: ReportDigest,
    pub evidence: EvidenceRoot,
    pub vector_count: u16,
    pub admitted_height: u64,
}
/// Encodes a commit-accepted event.
///
/// # Errors
/// Returns `CAPACITY` when `out` is shorter than 194 bytes.
pub fn encode_commit_event(v: &CommitAccepted, out: &mut [u8]) -> CodecResult<usize> {
    output_size(out, 194, MAX_EVENT_BYTES)?;
    let mut w = Writer::new(out);
    write_event_common(&mut w, &v.common)?;
    w.put(v.evaluator.as_bytes())?;
    w.put(v.commitment.as_bytes())?;
    w.u64(v.accepted_height)?;
    Ok(w.len())
}
/// Decodes a commit-accepted event.
///
/// # Errors
/// Returns `BAD_VERSION` for a wrong schema; `NON_CANONICAL` for a zero identity, version or digest, short or trailing input.
pub fn decode_commit_event(input: &[u8]) -> CodecResult<CommitAccepted> {
    let mut r = Reader::new(input);
    let common = read_event_common(&mut r)?;
    let evaluator = EvaluatorId::new(r.fixed()?)?;
    let commitment = CommitmentDigest::new(r.fixed()?)?;
    let accepted_height = r.u64()?;
    r.finish()?;
    Ok(CommitAccepted {
        common,
        evaluator,
        commitment,
        accepted_height,
    })
}
/// Encodes a reveal-score event.
///
/// # Errors
/// Returns `NON_CANONICAL` when `vector_count` is outside 1..=32; `CAPACITY` when `out` is shorter than 228 bytes.
pub fn encode_reveal_event(v: &RevealScoreEvent, out: &mut [u8]) -> CodecResult<usize> {
    if !(1..=32).contains(&v.vector_count) {
        return Err(NON_CANONICAL);
    }
    output_size(out, 228, MAX_EVENT_BYTES)?;
    let mut w = Writer::new(out);
    write_event_common(&mut w, &v.common)?;
    w.put(v.evaluator.as_bytes())?;
    w.put(v.report.as_bytes())?;
    w.put(v.evidence.as_bytes())?;
    w.u16(v.vector_count)?;
    w.u64(v.admitted_height)?;
    Ok(w.len())
}
/// Decodes a reveal-score event.
///
/// # Errors
/// Returns `BAD_VERSION` for a wrong schema; `NON_CANONICAL` for a zero identity, version or digest, a vector count outside 1..=32, short or trailing input.
pub fn decode_reveal_event(input: &[u8]) -> CodecResult<RevealScoreEvent> {
    let mut r = Reader::new(input);
    let common = read_event_common(&mut r)?;
    let evaluator = EvaluatorId::new(r.fixed()?)?;
    let report = ReportDigest::new(r.fixed()?)?;
    let evidence = EvidenceRoot::new(r.fixed()?)?;
    let vector_count = r.u16()?;
    if !(1..=32).contains(&vector_count) {
        return Err(NON_CANONICAL);
    }
    let admitted_height = r.u64()?;
    r.finish()?;
    Ok(RevealScoreEvent {
        common,
        evaluator,
        report,
        evidence,
        vector_count,
        admitted_height,
    })
}
/// Encodes the event topic for `operation`.
///
/// # Errors
/// Returns `CAPACITY` when the topic exceeds 64 bytes or `out` is too small.
pub fn event_topic(operation: Operation, out: &mut [u8]) -> CodecResult<usize> {
    let name = operation.metadata().name.as_bytes();
    let n = 9 + name.len();
    output_size(out, n, 64)?;
    let mut w = Writer::new(out);
    w.put(b"PAXAI/v1/")?;
    w.put(name)?;
    Ok(w.len())
}
/// Validate the common frame before a feature owner decodes its exact suffix.
///
/// # Errors
/// Returns `CAPACITY` for an oversized or non-ASCII topic or oversized body; `NON_CANONICAL` for a missing topic prefix; `UNKNOWN_OPERATION` when no mutation matches; propagates common, commit and reveal event decode refusals.
pub fn decode_event_frame<'a>(
    topic: &[u8],
    body: &'a [u8],
) -> CodecResult<(Operation, EventCommon, &'a [u8])> {
    if topic.len() > 64 || body.len() > MAX_EVENT_BYTES || !topic.is_ascii() {
        return Err(CAPACITY);
    }
    let name = topic.strip_prefix(b"PAXAI/v1/").ok_or(NON_CANONICAL)?;
    let operation = crate::dispatch::OPERATIONS
        .iter()
        .find(|m| m.name.as_bytes() == name && m.boundary == CallBoundary::Mutation)
        .map(|m| m.operation)
        .ok_or(UNKNOWN_OPERATION)?;
    let mut r = Reader::new(body);
    let common = read_event_common(&mut r)?;
    let suffix = r.take(r.remaining())?;
    match operation.selector() {
        0x0401 => {
            decode_commit_event(body)?;
        }
        0x0402 => {
            decode_reveal_event(body)?;
        }
        _ => {}
    }
    Ok((operation, common, suffix))
}
/// Encodes an event frame and validates fixed common-owned suffixes.
///
/// # Errors
/// Returns `NON_CANONICAL` for a non-mutation operation or a wrong fixed suffix length; `ARITHMETIC` on size overflow; `CAPACITY` when the size exceeds `MAX_EVENT_BYTES` or `out` is too small; propagates commit and reveal event decode refusals.
pub fn encode_event_frame(
    operation: Operation,
    common: &EventCommon,
    suffix: &[u8],
    out: &mut [u8],
) -> CodecResult<usize> {
    if operation.metadata().boundary != CallBoundary::Mutation {
        return Err(NON_CANONICAL);
    }
    let size = EVENT_COMMON_BYTES
        .checked_add(suffix.len())
        .ok_or(ARITHMETIC)?;
    output_size(out, size, MAX_EVENT_BYTES)?;
    if operation.selector() == 0x0401 && suffix.len() != 72
        || operation.selector() == 0x0402 && suffix.len() != 106
    {
        return Err(NON_CANONICAL);
    }
    let mut w = Writer::new(out);
    write_event_common(&mut w, common)?;
    w.put(suffix)?;
    let n = w.len();
    // Fixed common-owned event suffixes are validated, including nonzero digests/counts.
    match operation.selector() {
        0x0401 => {
            decode_commit_event(&out[..n])?;
        }
        0x0402 => {
            decode_reveal_event(&out[..n])?;
        }
        _ => {}
    }
    Ok(n)
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct ReadHeader {
    pub revision: u64,
    pub digest: StateDigest,
    pub total_bytes: u32,
    pub chain: ChainDomain,
    pub program: ProgramId,
    pub market: MarketId,
    pub epoch: Presence<u64>,
    pub config: Version,
    pub roster: Presence<RosterDigest>,
}
impl ReadHeader {
    fn validate(&self) -> CodecResult<()> {
        let framing = u32::try_from(STATE_FRAMING_BYTES).map_err(|_| ARITHMETIC)?;
        let max_state = u32::try_from(MAX_STATE_BYTES).map_err(|_| ARITHMETIC)?;
        if self.revision == 0 || self.total_bytes < framing || self.total_bytes > max_state {
            return Err(NON_CANONICAL);
        }
        if matches!(self.epoch, Presence::Absent) && self.roster != Presence::Absent
            || matches!(self.epoch, Presence::Present(_)) && self.roster == Presence::Absent
        {
            return Err(NON_CANONICAL);
        }
        Ok(())
    }
}
/// Encodes a read header into `out`.
///
/// # Errors
/// Returns `NON_CANONICAL` for a zero revision, out-of-range `total_bytes` or epoch/roster presence mismatch; `CAPACITY` when `out` is too small.
pub fn encode_read_header(v: &ReadHeader, out: &mut [u8]) -> CodecResult<usize> {
    v.validate()?;
    let n =
        152 + if matches!(v.epoch, Presence::Present(_)) {
            8
        } else {
            0
        } + if matches!(v.roster, Presence::Present(_)) {
            32
        } else {
            0
        };
    output_size(out, n, 192)?;
    let mut w = Writer::new(out);
    w.u16(SCHEMA_VERSION)?;
    w.u64(v.revision)?;
    w.put(v.digest.as_bytes())?;
    w.u32(v.total_bytes)?;
    w.put(v.chain.as_bytes())?;
    w.put(v.program.as_bytes())?;
    w.put(v.market.as_bytes())?;
    w.presence(&v.epoch, |w, e| w.u64(*e))?;
    w.u64(v.config.get())?;
    w.presence(&v.roster, |w, d| w.put(d.as_bytes()))?;
    Ok(w.len())
}
/// Decodes and validates a read header.
///
/// # Errors
/// Returns `CAPACITY` when the input exceeds 192 bytes; `BAD_VERSION` for a wrong schema; `NON_CANONICAL` for a zero digest, identity or version, bad presence flag, short or trailing input, or a failed header check.
pub fn decode_read_header(input: &[u8]) -> CodecResult<ReadHeader> {
    if input.len() > 192 {
        return Err(CAPACITY);
    }
    let mut r = Reader::new(input);
    schema(&mut r)?;
    let revision = r.u64()?;
    let digest = StateDigest::new(r.fixed()?)?;
    let total_bytes = r.u32()?;
    let chain = ChainDomain::new(r.fixed()?)?;
    let program = ProgramId::new(r.fixed()?)?;
    let market = MarketId::new(r.fixed()?)?;
    let epoch = r.presence(Reader::u64)?;
    let config = Version::new(r.u64()?)?;
    let roster = r.presence(|r| RosterDigest::new(r.fixed()?))?;
    r.finish()?;
    let v = ReadHeader {
        revision,
        digest,
        total_bytes,
        chain,
        program,
        market,
        epoch,
        config,
        roster,
    };
    v.validate()?;
    Ok(v)
}
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct ChunkRequest {
    pub revision: u64,
    pub digest: Presence<StateDigest>,
    pub offset: u32,
    pub requested: u16,
}
impl ChunkRequest {
    /// Validates chunk request pinning, size and alignment.
    ///
    /// # Errors
    /// Returns `NON_CANONICAL` when revision and digest presence disagree, `requested` is zero or above `MAX_CHUNK_BYTES`, or `offset` is unaligned or beyond `MAX_STATE_BYTES`.
    pub fn validate(&self) -> CodecResult<()> {
        let max_state = u32::try_from(MAX_STATE_BYTES).map_err(|_| ARITHMETIC)?;
        if (self.revision == 0) != (self.digest == Presence::Absent)
            || self.requested == 0
            || usize::from(self.requested) > MAX_CHUNK_BYTES
            || !self.offset.is_multiple_of(8192)
            || self.offset >= max_state
        {
            return Err(NON_CANONICAL);
        }
        Ok(())
    }
}
/// Encodes a validated chunk request.
///
/// # Errors
/// Propagates `ChunkRequest::validate` refusals; returns `CAPACITY` when `out` is shorter than 46 bytes.
pub fn encode_chunk_request(v: &ChunkRequest, out: &mut [u8]) -> CodecResult<usize> {
    v.validate()?;
    output_size(out, 46, 46)?;
    let mut w = Writer::new(out);
    w.u64(v.revision)?;
    match v.digest {
        Presence::Absent => w.put(&[0; 32])?,
        Presence::Present(d) => w.put(d.as_bytes())?,
    }
    w.u32(v.offset)?;
    w.u16(v.requested)?;
    Ok(w.len())
}
/// Decodes and validates a chunk request.
///
/// # Errors
/// Returns `NON_CANONICAL` for short or trailing input; propagates `ChunkRequest::validate` refusals.
pub fn decode_chunk_request(input: &[u8]) -> CodecResult<ChunkRequest> {
    let mut r = Reader::new(input);
    let revision = r.u64()?;
    let bytes = r.fixed()?;
    let digest = if bytes == [0; 32] {
        Presence::Absent
    } else {
        Presence::Present(StateDigest::new(bytes)?)
    };
    let offset = r.u32()?;
    let requested = r.u16()?;
    r.finish()?;
    let v = ChunkRequest {
        revision,
        digest,
        offset,
        requested,
    };
    v.validate()?;
    Ok(v)
}
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct ChunkResponse<'a> {
    pub revision: u64,
    pub digest: StateDigest,
    pub total_bytes: u32,
    pub offset: u32,
    pub bytes: &'a [u8],
}
impl ChunkResponse<'_> {
    /// Validates chunk response bounds and alignment.
    ///
    /// # Errors
    /// Returns `NON_CANONICAL` for a zero revision, out-of-range `total_bytes`, unaligned or out-of-range offset, empty or oversized bytes, or a chunk ending past `total_bytes`.
    pub fn validate(&self) -> CodecResult<()> {
        let framing = u32::try_from(STATE_FRAMING_BYTES).map_err(|_| ARITHMETIC)?;
        let max_state = u32::try_from(MAX_STATE_BYTES).map_err(|_| ARITHMETIC)?;
        if self.revision == 0
            || self.total_bytes < framing
            || self.total_bytes > max_state
            || !self.offset.is_multiple_of(8192)
            || self.offset >= self.total_bytes
            || self.bytes.is_empty()
            || self.bytes.len() > 8192
        {
            return Err(NON_CANONICAL);
        }
        let end = usize::try_from(self.offset)
            .map_err(|_| ARITHMETIC)?
            .checked_add(self.bytes.len())
            .ok_or(ARITHMETIC)?;
        if end > usize::try_from(self.total_bytes).map_err(|_| ARITHMETIC)? {
            Err(NON_CANONICAL)
        } else {
            Ok(())
        }
    }
}
/// Encodes a validated chunk response.
///
/// # Errors
/// Propagates `ChunkResponse::validate` refusals; returns `CAPACITY` when `out` is too small.
pub fn encode_chunk_response(v: &ChunkResponse<'_>, out: &mut [u8]) -> CodecResult<usize> {
    v.validate()?;
    output_size(out, 52 + v.bytes.len(), 8244)?;
    let mut w = Writer::new(out);
    w.u16(SCHEMA_VERSION)?;
    w.u64(v.revision)?;
    w.put(v.digest.as_bytes())?;
    w.u32(v.total_bytes)?;
    w.u32(v.offset)?;
    w.u16(u16::try_from(v.bytes.len()).map_err(|_| ARITHMETIC)?)?;
    w.put(v.bytes)?;
    Ok(w.len())
}
/// Decodes and validates a chunk response.
///
/// # Errors
/// Returns `CAPACITY` when the input exceeds 8244 bytes or the chunk exceeds 8192 bytes; `BAD_VERSION` for a wrong schema; `NON_CANONICAL` for a zero digest, short or trailing input; propagates `ChunkResponse::validate` refusals.
pub fn decode_chunk_response(input: &[u8]) -> CodecResult<ChunkResponse<'_>> {
    if input.len() > 8244 {
        return Err(CAPACITY);
    }
    let mut r = Reader::new(input);
    schema(&mut r)?;
    let revision = r.u64()?;
    let digest = StateDigest::new(r.fixed()?)?;
    let total_bytes = r.u32()?;
    let offset = r.u32()?;
    let n = usize::from(r.u16()?);
    if n > 8192 {
        return Err(CAPACITY);
    }
    let bytes = r.take(n)?;
    r.finish()?;
    let v = ChunkResponse {
        revision,
        digest,
        total_bytes,
        offset,
        bytes,
    };
    v.validate()?;
    Ok(v)
}
/// Consistency of this inner response only. Outer native-root/finality proof remains mandatory.
///
/// # Errors
/// Propagates both `validate` refusals; returns `CONFLICT` when a pinned revision or digest differs; `NON_CANONICAL` for an offset mismatch or wrong byte count.
pub fn check_chunk_response(
    request: &ChunkRequest,
    response: &ChunkResponse<'_>,
) -> CodecResult<()> {
    request.validate()?;
    response.validate()?;
    if request.revision != 0
        && (request.revision != response.revision
            || request.digest != Presence::Present(response.digest))
    {
        return Err(CONFLICT);
    }
    let expected = usize::from(request.requested)
        .min(usize::try_from(response.total_bytes - response.offset).map_err(|_| ARITHMETIC)?);
    if request.offset != response.offset || response.bytes.len() != expected {
        Err(NON_CANONICAL)
    } else {
        Ok(())
    }
}

/// ORIGINAL IMPLEMENTATION DECISION: global PAXAS1/schema/revision then exactly six
/// ordered section frames (index:u16, reserved:u16=0, `bytes_length:u32`, bytes).
/// No padding. Global16 charged to control, section headers8 to their own caps.
/// This validates framing only; section owners must validate complete feature semantics.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct StateFrame<'a> {
    pub revision: u64,
    pub sections: [&'a [u8]; 6],
}
impl StateFrame<'_> {
    /// Encoded size, enforcing per-section and total caps.
    ///
    /// # Errors
    /// Returns `NON_CANONICAL` for a zero revision; `CAPACITY` when a section exceeds its cap or the total exceeds `MAX_STATE_BYTES`.
    pub fn encoded_len(&self) -> CodecResult<usize> {
        if self.revision == 0 {
            return Err(NON_CANONICAL);
        }
        let mut total = STATE_GLOBAL_BYTES;
        for (index, section) in self.sections.iter().enumerate() {
            let size = section
                .len()
                .checked_add(STATE_SECTION_HEADER_BYTES)
                .ok_or(ARITHMETIC)?;
            let charge = size
                .checked_add(if index == 5 { STATE_GLOBAL_BYTES } else { 0 })
                .ok_or(ARITHMETIC)?;
            if charge > STATE_SECTION_CAPS[index] {
                return Err(CAPACITY);
            }
            total = total.checked_add(size).ok_or(ARITHMETIC)?;
        }
        if total > MAX_STATE_BYTES {
            Err(CAPACITY)
        } else {
            Ok(total)
        }
    }
}
/// Encodes a state frame into `out`.
///
/// # Errors
/// Propagates `StateFrame::encoded_len` refusals; returns `CAPACITY` when `out` is too small.
pub fn encode_state(v: &StateFrame<'_>, out: &mut [u8]) -> CodecResult<usize> {
    let n = v.encoded_len()?;
    output_size(out, n, MAX_STATE_BYTES)?;
    let mut w = Writer::new(out);
    w.put(b"PAXAS1")?;
    w.u16(SCHEMA_VERSION)?;
    w.u64(v.revision)?;
    for (i, section) in v.sections.iter().enumerate() {
        w.u16(u16::try_from(i + 1).map_err(|_| ARITHMETIC)?)?;
        w.u16(0)?;
        w.bytes(
            section,
            STATE_SECTION_CAPS[i] - 8 - if i == 5 { 16 } else { 0 },
        )?;
    }
    Ok(w.len())
}
/// Decodes and validates a state frame.
///
/// # Errors
/// Returns `CAPACITY` when the input or a section exceeds its cap; `BAD_VERSION` for a wrong schema; `NON_CANONICAL` for a bad magic, wrong section index, nonzero reserved bytes, short or trailing input; propagates `StateFrame::encoded_len` refusals.
pub fn decode_state(input: &[u8]) -> CodecResult<StateFrame<'_>> {
    if input.len() > MAX_STATE_BYTES {
        return Err(CAPACITY);
    }
    let mut r = Reader::new(input);
    if r.take(6)? != b"PAXAS1" {
        return Err(NON_CANONICAL);
    }
    schema(&mut r)?;
    let revision = r.u64()?;
    let mut sections: [&[u8]; 6] = [&[]; 6];
    for (i, section) in sections.iter_mut().enumerate() {
        if usize::from(r.u16()?) != i + 1 {
            return Err(NON_CANONICAL);
        }
        r.reserved(2)?;
        *section = r.bytes(STATE_SECTION_CAPS[i] - 8 - if i == 5 { 16 } else { 0 })?;
    }
    r.finish()?;
    let v = StateFrame { revision, sections };
    v.encoded_len()?;
    Ok(v)
}
/// Digest of a valid state frame.
///
/// # Errors
/// Propagates `decode_state` refusals; returns `NON_CANONICAL` when the digest is all zero.
pub fn state_digest(input: &[u8]) -> CodecResult<StateDigest> {
    decode_state(input)?;
    StateDigest::new(domain_hash("PAXAI/view-state/v1", input)?.bytes())
}

/// Pure direct-call predicate; only a real host adapter can supply Context facts.
///
/// # Errors
/// Returns `UNAUTHORIZED` when an immediate caller program is present.
pub fn compare_direct_call(immediate_caller: Presence<ProgramId>) -> CodecResult<()> {
    if immediate_caller == Presence::Absent {
        Ok(())
    } else {
        Err(UNAUTHORIZED)
    }
}
/// Pure principal equality for kind0. Does not turn an ID into authenticated authority.
///
/// # Errors
/// Returns `UNAUTHORIZED` when authentication is not native or the actor differs.
pub fn compare_native_principal(
    envelope: &Envelope<'_>,
    invoking_principal: PrincipalId,
) -> CodecResult<()> {
    if envelope.authentication != Authentication::Native || envelope.actor != invoking_principal {
        Err(UNAUTHORIZED)
    } else {
        Ok(())
    }
}
/// Exact bytes for ordinary Ed25519 signing, without ph/EIP/JSON transformation.
#[must_use]
pub const fn request_signing_message(digest: RequestDigest) -> [u8; 32] {
    digest.bytes()
}
#[must_use]
pub const fn report_signing_message(digest: AttestationDigest) -> [u8; 32] {
    digest.bytes()
}
