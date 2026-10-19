//! Minimal F10 AI market projection: exact-root snapshot capture, checkpoint promotion,
//! atomic snapshot/rows/cursor/outbox commits, pinned participant paging and quarantine.
//!
//! State bytes come from a whole same-root chunk capture and are decoded only with the
//! `layerx-programs-ai-market` codecs. Promotion to finalized needs an actual checkpoint
//! certificate verified by `layerx_proof::checkpoint::verify_certificate` whose header
//! commits the snapshot's exact batch, sequence and native root. Elapsed time, depth or a
//! matching height never promotes. Components whose producers are absent keep their
//! explicit availability status.

use std::collections::HashMap;
use std::path::Path;
use std::sync::Mutex;

use layerx_programs_ai_market::{
    codec,
    errors::{
        ApplicationError, BAD_VERSION, CAPACITY, NOT_FOUND, WRONG_DOMAIN, WRONG_MARKET,
        WRONG_PROGRAM,
    },
    queries::{
        self, CaptureFacts, CursorKey, CursorScope, FinalityEvidence, QueryError, ReadProof,
        RewardField, ScoreField, BINDING_MAX_BYTES, CURSOR_LIFETIME_MS, CURSOR_MAX_BYTES,
        CURSOR_TOKEN_BYTES, FINALIZED_RANK, PAGE_MAX_BYTES, PAGE_MAX_ROWS,
    },
    types::{ChainDomain, Digest32, MarketId, PrincipalId, ProgramId, StateDigest},
    MAX_STATE_BYTES,
};
use layerx_proof::checkpoint::{verify_certificate, Certificate, GuarantorKey, SettlementDomain};
use layerx_types::verify::VerificationLevel;
use layerx_wire::hash::program_execution_batch_id;
use layerx_wire::receipt::decode_batch_header;
use rusqlite::{params, Connection, OptionalExtension, Transaction};
use sha2::{Digest, Sha256};

use crate::codec::hex0x;

pub use layerx_programs_ai_market::queries::{
    parse_hex32, parse_limit, Availability, KindFilter, ParticipantKind, ParticipantRow,
    ScoreStatus, SnapshotBinding, DEFAULT_LIMIT,
};
pub use layerx_programs_ai_market::types::Presence;

/// Canonical publication minimum checkpoint rank.
pub const MINIMUM_PUBLICATION_RANK: u8 = FINALIZED_RANK;
/// F02 authority freshness limit in execution heights.
pub const AUTHORITY_FRESHNESS_HEIGHTS: u64 = 8;
/// Recent finalized snapshot bodies retained per market.
pub const CACHED_SNAPSHOTS: usize = 64;
/// Largest snapshot body admitted to the recent cache.
pub const SNAPSHOT_BODY_MAX_BYTES: usize = 262_144;
/// Recent view cache per market.
pub const MARKET_CACHE_MAX_BYTES: usize = 16_777_216;
/// Recent epoch index headers per market.
pub const EPOCH_HEADERS_MAX: u64 = 4_096;
/// Current worker rows per market.
pub const WORKER_ROWS_MAX: usize = 32;
/// Frozen evaluator rows per market.
pub const EVALUATOR_ROWS_MAX: usize = 8;
/// Feature availability entries, F01..F10.
pub const FEATURES: usize = 10;
/// `ai_event_tag` of the finalized snapshot fact in the existing program event family.
pub const SNAPSHOT_EVENT_TAG: u16 = 0x0A10;
/// Default authenticated query rate per principal.
pub const DEFAULT_RATE: RateConfig = RateConfig {
    per_minute: 60,
    burst: 10,
    concurrent: 4,
};

const PARTICIPANT_ROWS_MAX: usize = WORKER_ROWS_MAX + EVALUATOR_ROWS_MAX;
const ROW_BYTES_MAX: usize = 400;
const EVENT_BODY_BYTES: usize = 2 + 2 + 32 * 3 + 8 + 8 + 32;
const TOKEN_SNAPSHOT_HEX: core::ops::Range<usize> = 2 * (2 + 64)..2 * (2 + 96);
const TOKEN_GENERATION_HEX: core::ops::Range<usize> = 2 * (2 + 128 + 2 + 8)..2 * (2 + 128 + 14);
const MINUTE_MS: u64 = 60_000;

const SCHEMA: &str = "
CREATE TABLE IF NOT EXISTS ai_stream (
    market BLOB PRIMARY KEY,
    observed_sequence INTEGER NOT NULL,
    observed_root BLOB,
    finalized_sequence INTEGER,
    finalized_snapshot BLOB,
    next_subject INTEGER NOT NULL,
    quarantine TEXT
);
CREATE TABLE IF NOT EXISTS ai_snapshot (
    snapshot_id BLOB PRIMARY KEY,
    market BLOB NOT NULL,
    chain BLOB NOT NULL,
    program BLOB NOT NULL,
    observed_sequence INTEGER NOT NULL,
    execution_height INTEGER NOT NULL,
    batch_id BLOB NOT NULL,
    native_root BLOB NOT NULL,
    revision INTEGER NOT NULL,
    state_digest BLOB NOT NULL,
    total_bytes INTEGER NOT NULL,
    chunks INTEGER NOT NULL,
    read_verification INTEGER NOT NULL,
    projection INTEGER NOT NULL,
    state BLOB,
    availability BLOB NOT NULL,
    source_activity BLOB NOT NULL,
    epoch INTEGER,
    binding BLOB,
    checkpoint BLOB,
    rank INTEGER NOT NULL,
    publication_time_ms INTEGER,
    observed_at_ms INTEGER NOT NULL,
    UNIQUE (market, observed_sequence)
);
CREATE TABLE IF NOT EXISTS ai_participant (
    snapshot_id BLOB NOT NULL REFERENCES ai_snapshot(snapshot_id) ON DELETE CASCADE,
    ordinal INTEGER NOT NULL,
    kind INTEGER NOT NULL,
    participant BLOB NOT NULL,
    row BLOB NOT NULL,
    PRIMARY KEY (snapshot_id, ordinal)
);
CREATE TABLE IF NOT EXISTS ai_outbox (
    event_id TEXT PRIMARY KEY,
    market BLOB NOT NULL,
    subject_sequence INTEGER NOT NULL,
    snapshot_id BLOB NOT NULL REFERENCES ai_snapshot(snapshot_id) ON DELETE CASCADE,
    body BLOB NOT NULL,
    published_at_ms INTEGER,
    UNIQUE (market, subject_sequence)
);
CREATE TABLE IF NOT EXISTS ai_lease (
    snapshot_id BLOB PRIMARY KEY REFERENCES ai_snapshot(snapshot_id) ON DELETE CASCADE,
    expiry_ms INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS ai_alert (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    market BLOB NOT NULL,
    subject BLOB NOT NULL,
    category TEXT NOT NULL
);
";

/// Projection failures. Capture refusals keep the `CAPTURE_FINALIZED_SNAPSHOT` names.
#[derive(Clone, Debug, Eq, PartialEq)]
pub enum ViewError {
    InvalidEncoding,
    UnsupportedVersion,
    WrongDomain,
    IntegrityFailure,
    BindingMismatch,
    FinalityUnavailable,
    SnapshotConflict,
    ProjectionUnavailable,
    CapacityExceeded,
    CursorExpired,
    CursorMismatch,
    /// Unknown or prohibited market; never reveals which.
    AccessRefused,
    RateLimited,
    /// Empty rate configuration: the endpoint is disabled, not unlimited.
    EndpointDisabled,
    /// Retained binding without retained content; carries the oldest retained snapshot.
    SnapshotPruned {
        oldest: Option<(Digest32, u64)>,
    },
    ResponseTooLarge,
    Quarantined,
    RollbackRefused,
    AuthorityStale {
        lag: u64,
    },
    Store(String),
}

impl ViewError {
    /// Stable error category; operator alerts and responses carry only this and IDs.
    #[must_use]
    pub const fn category(&self) -> &'static str {
        match self {
            Self::InvalidEncoding => "invalid-encoding",
            Self::UnsupportedVersion => "unsupported-version",
            Self::WrongDomain => "wrong-domain",
            Self::IntegrityFailure => "integrity-failure",
            Self::BindingMismatch => "binding-mismatch",
            Self::FinalityUnavailable => "finality-unavailable",
            Self::SnapshotConflict => "snapshot-conflict",
            Self::ProjectionUnavailable => "projection-unavailable",
            Self::CapacityExceeded => "capacity-exceeded",
            Self::CursorExpired => "cursor-expired",
            Self::CursorMismatch => "cursor-mismatch",
            Self::AccessRefused => "access-refused",
            Self::RateLimited => "rate-limited",
            Self::EndpointDisabled => "endpoint-disabled",
            Self::SnapshotPruned { .. } => "snapshot-pruned",
            Self::ResponseTooLarge => "response-too-large",
            Self::Quarantined => "quarantined",
            Self::RollbackRefused => "rollback-refused",
            Self::AuthorityStale { .. } => "authority-stale",
            Self::Store(_) => "projection-store",
        }
    }
}

impl From<ApplicationError> for ViewError {
    fn from(error: ApplicationError) -> Self {
        match error {
            BAD_VERSION => Self::UnsupportedVersion,
            WRONG_DOMAIN | WRONG_PROGRAM | WRONG_MARKET => Self::WrongDomain,
            CAPACITY => Self::CapacityExceeded,
            NOT_FOUND => Self::ProjectionUnavailable,
            _ => Self::InvalidEncoding,
        }
    }
}

impl From<QueryError> for ViewError {
    fn from(error: QueryError) -> Self {
        match error {
            QueryError::Application(e) => e.into(),
            QueryError::BindingMismatch => Self::BindingMismatch,
            QueryError::FinalityUnavailable => Self::FinalityUnavailable,
            QueryError::SnapshotConflict => Self::SnapshotConflict,
            QueryError::IntegrityFailure => Self::IntegrityFailure,
            QueryError::CursorExpired => Self::CursorExpired,
            QueryError::CursorMismatch => Self::CursorMismatch,
            QueryError::ResponseTooLarge => Self::ResponseTooLarge,
        }
    }
}

impl From<rusqlite::Error> for ViewError {
    fn from(error: rusqlite::Error) -> Self {
        Self::Store(error.to_string())
    }
}

pub type ViewResult<T> = Result<T, ViewError>;

/// Projection states. Only verified checkpoint evidence reaches `FinalizedPublishable`.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
#[repr(u8)]
pub enum ProjectionState {
    ObservedUnverified = 1,
    EvidenceVerifiedUnfinalized = 2,
    FinalizedPublishable = 3,
    /// Finalized, content moved out of the recent cache; binding and rows retained.
    Archived = 4,
    Quarantined = 5,
}

impl ProjectionState {
    fn from_db(value: i64) -> ViewResult<Self> {
        Ok(match value {
            1 => Self::ObservedUnverified,
            2 => Self::EvidenceVerifiedUnfinalized,
            3 => Self::FinalizedPublishable,
            4 => Self::Archived,
            5 => Self::Quarantined,
            _ => return Err(ViewError::IntegrityFailure),
        })
    }
    #[must_use]
    pub const fn finalized(self) -> bool {
        matches!(self, Self::FinalizedPublishable | Self::Archived)
    }
}

/// How the `ProgramRead` that produced the captured chunks was authenticated.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
#[repr(u8)]
pub enum ReadVerification {
    Unverified = 1,
    /// The read was accepted by the existing signed `ProgramRead` verification.
    SequencerSigned = 2,
}

/// One complete same-root capture offered to the projection.
#[derive(Clone, Copy, Debug)]
pub struct Observation<'a> {
    pub state: &'a [u8],
    pub facts: CaptureFacts,
    pub read: ReadVerification,
    /// Activity ID of the canonical source receipt at the observed sequence.
    pub source_activity: Digest32,
    pub observed_at_ms: u64,
}

/// Configured finality authority: network, settlement domain and bonded guarantor set.
#[derive(Clone, Debug)]
pub struct FinalityAuthority {
    pub network_id: u32,
    pub settlement: SettlementDomain,
    pub guarantors: Vec<GuarantorKey>,
}

/// Checkpoint material with the identifier registered for it on the settlement chain.
#[derive(Clone, Copy, Debug)]
pub struct CheckpointProof<'a> {
    pub certificate: &'a Certificate,
    pub registered_checkpoint_id: [u8; 32],
    pub registered_settlement_reference: Option<&'a [u8]>,
}

/// One stored snapshot, as committed.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct SnapshotRecord {
    pub snapshot_id: Digest32,
    pub market: MarketId,
    pub chain: ChainDomain,
    pub program: ProgramId,
    pub observed_sequence: u64,
    pub execution_height: u64,
    pub batch_id: Digest32,
    pub native_state_root: Digest32,
    pub revision: u64,
    pub state_digest: StateDigest,
    pub total_bytes: u32,
    pub chunks: usize,
    pub read: ReadVerification,
    pub projection: ProjectionState,
    pub availability: [Availability; FEATURES],
    pub source_activity: Digest32,
    pub epoch: Presence<u64>,
    /// Canonical `SnapshotBindingV1` bytes once finalized.
    pub binding: Option<Vec<u8>>,
    pub checkpoint: Option<Digest32>,
    pub rank: u8,
    pub publication_time_ms: Option<u64>,
    pub observed_at_ms: u64,
    pub retained: bool,
}

impl SnapshotRecord {
    fn facts(&self) -> CaptureFacts {
        CaptureFacts {
            proof: ReadProof {
                chain: self.chain,
                program: self.program,
                native_state_root: self.native_state_root,
                observed_sequence: self.observed_sequence,
                execution_height: self.execution_height,
                batch_id: self.batch_id,
            },
            revision: self.revision,
            digest: self.state_digest,
            total_bytes: self.total_bytes,
            chunks: self.chunks,
        }
    }
}

/// Per-market projection cursor.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct StreamCursor {
    pub market: MarketId,
    pub observed_sequence: u64,
    /// Absent only for a stream quarantined before its first committed capture.
    pub observed_root: Option<Digest32>,
    pub finalized_sequence: Option<u64>,
    pub finalized_snapshot: Option<Digest32>,
    pub next_subject: u64,
    pub quarantine: Option<String>,
}

/// One durable outbox row whose snapshot is finalized and ready for publication.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct OutboxEvent {
    pub event_id: String,
    pub market: MarketId,
    pub subject_sequence: u64,
    pub snapshot_id: Digest32,
    pub body: Vec<u8>,
    /// Truthful label: the finalized snapshot's canonical binding with its achieved rank.
    pub binding: Vec<u8>,
    pub rank: u8,
    pub published: bool,
}

/// Operator alert: identifiers and error category only.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Alert {
    pub market: [u8; 32],
    pub subject: [u8; 32],
    pub category: String,
}

/// Freshness of the latest finalized snapshot against independently verified authority.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum Freshness {
    Current,
    Stale {
        lag: u64,
    },
    /// No verified authority height was supplied.
    Unknown,
}

/// `GET_MARKET_VIEW` result.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct MarketView {
    pub snapshot_id: Digest32,
    pub binding: SnapshotBinding,
    pub projection: ProjectionState,
    pub availability: [Availability; FEATURES],
    pub source_activity: Digest32,
    pub freshness: Freshness,
}

/// `LIST_PARTICIPANTS` result: rows of one pinned snapshot.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ParticipantPage {
    pub snapshot_id: Digest32,
    pub binding: SnapshotBinding,
    pub availability: [Availability; FEATURES],
    pub rows: Vec<ParticipantRow>,
    pub cursor: Option<String>,
    /// Exact framed page length (bounded at 65536 bytes).
    pub encoded_bytes: usize,
}

/// Requested-epoch absence statuses and the retained case.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
#[repr(u8)]
pub enum EpochStatus {
    Retained = 1,
    NeverOpened = 2,
    RetainedTerminal = 3,
    ArchiveRequired = 4,
    ArchiveUnavailable = 5,
    UnsupportedVersion = 6,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct EpochEntry {
    pub epoch: u64,
    pub status: EpochStatus,
    pub snapshot_id: Option<Digest32>,
}

/// `GET_HISTORY` result. `component` is the epoch index availability; entries are empty
/// unless it is available.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct EpochPage {
    pub component: Availability,
    pub entries: Vec<EpochEntry>,
}

/// `PageRequestV1` after strict parsing.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct PageRequest {
    pub market: MarketId,
    pub snapshot: Option<Digest32>,
    pub filter: KindFilter,
    pub active_only: bool,
    pub limit: u8,
    pub cursor: Option<String>,
}

impl PageRequest {
    /// Parses the market path segment and query. Every refusal happens before any store read
    /// or cryptographic work: unknown or repeated keys, noncanonical decimals or hex, limits
    /// outside 1..=32 and cursors above 1024 bytes.
    ///
    /// # Errors
    /// `InvalidEncoding` for any noncanonical input, `ResponseTooLarge` for an oversized cursor.
    pub fn parse(market: &str, query: Option<&str>) -> ViewResult<Self> {
        let market = MarketId::new(parse_hex32(market)?)?;
        let [snapshot, filter, active_only, limit, cursor] = query_pairs(
            query,
            &["snapshot", "kind", "active_only", "limit", "cursor"],
        )?[..] else {
            return Err(ViewError::InvalidEncoding);
        };
        if cursor.is_some_and(|c| c.len() > CURSOR_MAX_BYTES) {
            return Err(ViewError::ResponseTooLarge);
        }
        let filter = match filter {
            None | Some("all") => KindFilter::All,
            Some("worker") => KindFilter::Worker,
            Some("evaluator") => KindFilter::Evaluator,
            Some(_) => return Err(ViewError::InvalidEncoding),
        };
        let active_only = match active_only {
            None | Some("false") => false,
            Some("true") => true,
            Some(_) => return Err(ViewError::InvalidEncoding),
        };
        if cursor.is_some_and(|c| c.len() != CURSOR_TOKEN_BYTES) {
            return Err(ViewError::InvalidEncoding);
        }
        Ok(Self {
            market,
            snapshot: snapshot
                .map(|s| Digest32::new(parse_hex32(s)?))
                .transpose()?,
            filter,
            active_only,
            limit: parse_limit(limit)?,
            cursor: cursor.map(str::to_owned),
        })
    }
}

fn query_pairs<'q>(query: Option<&'q str>, keys: &[&str]) -> ViewResult<Vec<Option<&'q str>>> {
    let mut values = vec![None; keys.len()];
    for pair in query
        .filter(|q| !q.is_empty())
        .into_iter()
        .flat_map(|q| q.split('&'))
    {
        let (key, value) = pair.split_once('=').ok_or(ViewError::InvalidEncoding)?;
        let slot = keys
            .iter()
            .position(|k| *k == key)
            .and_then(|i| values.get_mut(i))
            .ok_or(ViewError::InvalidEncoding)?;
        if slot.replace(value).is_some() {
            return Err(ViewError::InvalidEncoding);
        }
    }
    Ok(values)
}

/// Strict `GET_MARKET_VIEW` query: only an optional exact `snapshot`.
///
/// # Errors
/// `InvalidEncoding` for any other key, a repeated key or noncanonical hex.
pub fn parse_snapshot_query(
    market: &str,
    query: Option<&str>,
) -> ViewResult<(MarketId, Option<Digest32>)> {
    let market = MarketId::new(parse_hex32(market)?)?;
    let [snapshot] = query_pairs(query, &["snapshot"])?[..] else {
        return Err(ViewError::InvalidEncoding);
    };
    Ok((
        market,
        snapshot
            .map(|s| Digest32::new(parse_hex32(s)?))
            .transpose()?,
    ))
}

/// Strict `GET_HISTORY` query: canonical decimal `from` (default 0) and `limit` (default 16).
///
/// # Errors
/// `InvalidEncoding` for any other key, a repeated key or a noncanonical decimal or limit.
pub fn parse_epoch_query(market: &str, query: Option<&str>) -> ViewResult<(MarketId, u64, u8)> {
    let market = MarketId::new(parse_hex32(market)?)?;
    let [from, limit] = query_pairs(query, &["from", "limit"])?[..] else {
        return Err(ViewError::InvalidEncoding);
    };
    Ok((
        market,
        from.map(codec::decimal_u64).transpose()?.unwrap_or(0),
        parse_limit(limit)?,
    ))
}

/// Authenticated reader. Built only after the current session check succeeded for this
/// request; cursors are bound to its visibility scope and grant no access of their own.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct Viewer {
    visibility: Digest32,
}

impl Viewer {
    /// # Errors
    /// `InvalidEncoding` for an empty principal digest.
    pub fn for_principal(principal_digest: &str) -> ViewResult<Self> {
        if principal_digest.is_empty() {
            return Err(ViewError::InvalidEncoding);
        }
        Ok(Self {
            visibility: codec::domain_hash(
                "PAXAI/view-visibility/v1",
                principal_digest.as_bytes(),
            )?,
        })
    }
    #[must_use]
    pub const fn visibility(&self) -> Digest32 {
        self.visibility
    }
}

/// Cursor MAC keys. A retired key stays accepted for one full cursor lifetime after
/// rotation; an unknown or expired-retired generation yields `CursorExpired`.
pub struct CursorKeyring {
    current: (u32, [u8; 32]),
    retired: Vec<(u32, [u8; 32], u64)>,
}

impl CursorKeyring {
    #[must_use]
    pub const fn new(generation: u32, secret: [u8; 32]) -> Self {
        Self {
            current: (generation, secret),
            retired: Vec::new(),
        }
    }
    /// # Errors
    /// `InvalidEncoding` unless the new generation is strictly higher.
    pub fn rotate(&mut self, generation: u32, secret: [u8; 32], now_ms: u64) -> ViewResult<()> {
        if generation <= self.current.0 {
            return Err(ViewError::InvalidEncoding);
        }
        let (old, old_secret) = self.current;
        self.retired
            .retain(|(_, _, at)| at.saturating_add(CURSOR_LIFETIME_MS) > now_ms);
        self.retired.push((old, old_secret, now_ms));
        self.current = (generation, secret);
        Ok(())
    }
    fn issuing(&self) -> CursorKey<'_> {
        CursorKey {
            secret: &self.current.1,
            generation: self.current.0,
        }
    }
    fn for_generation(&self, generation: u32, now_ms: u64) -> Option<CursorKey<'_>> {
        if generation == self.current.0 {
            return Some(self.issuing());
        }
        self.retired
            .iter()
            .find(|(g, _, at)| *g == generation && at.saturating_add(CURSOR_LIFETIME_MS) > now_ms)
            .map(|(g, secret, _)| CursorKey {
                secret,
                generation: *g,
            })
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct RateConfig {
    pub per_minute: u32,
    pub burst: u32,
    pub concurrent: u32,
}

struct Bucket {
    scaled_tokens: u64,
    last_ms: u64,
    active: u32,
}

/// Per-principal token bucket plus concurrency bound, checked before any store work.
pub struct QueryLimiter {
    config: Option<RateConfig>,
    buckets: Mutex<HashMap<Digest32, Bucket>>,
}

/// Admission for one in-flight query; releases its concurrency slot when dropped.
pub struct Permit<'a> {
    limiter: &'a QueryLimiter,
    visibility: Digest32,
}

impl Drop for Permit<'_> {
    fn drop(&mut self) {
        if let Ok(mut buckets) = self.limiter.buckets.lock() {
            if let Some(bucket) = buckets.get_mut(&self.visibility) {
                bucket.active = bucket.active.saturating_sub(1);
            }
        }
    }
}

impl QueryLimiter {
    /// `None` or a zero rate means the endpoint is disabled.
    #[must_use]
    pub fn new(config: Option<RateConfig>) -> Self {
        Self {
            config: config.filter(|c| c.per_minute > 0 && c.burst > 0 && c.concurrent > 0),
            buckets: Mutex::new(HashMap::new()),
        }
    }
    /// # Errors
    /// `EndpointDisabled` without a rate configuration, `RateLimited` when the principal's
    /// bucket or concurrency bound is exhausted.
    pub fn admit(&self, viewer: &Viewer, now_ms: u64) -> ViewResult<Permit<'_>> {
        let config = self.config.ok_or(ViewError::EndpointDisabled)?;
        let capacity = u64::from(config.burst) * MINUTE_MS;
        let mut buckets = self
            .buckets
            .lock()
            .map_err(|_| ViewError::Store("limiter poisoned".to_owned()))?;
        let bucket = buckets.entry(viewer.visibility).or_insert(Bucket {
            scaled_tokens: capacity,
            last_ms: now_ms,
            active: 0,
        });
        let refill = now_ms
            .saturating_sub(bucket.last_ms)
            .saturating_mul(u64::from(config.per_minute));
        bucket.scaled_tokens = bucket.scaled_tokens.saturating_add(refill).min(capacity);
        bucket.last_ms = bucket.last_ms.max(now_ms);
        if bucket.active >= config.concurrent || bucket.scaled_tokens < MINUTE_MS {
            return Err(ViewError::RateLimited);
        }
        bucket.scaled_tokens -= MINUTE_MS;
        bucket.active += 1;
        Ok(Permit {
            limiter: self,
            visibility: viewer.visibility,
        })
    }
}

/// Durable AI market projection over one `SQLite` file.
pub struct ProjectionStore {
    connection: Mutex<Connection>,
}

fn signed(value: u64) -> ViewResult<i64> {
    i64::try_from(value).map_err(|_| ViewError::CapacityExceeded)
}
fn unsigned(value: i64) -> ViewResult<u64> {
    u64::try_from(value).map_err(|_| ViewError::IntegrityFailure)
}
fn array32(bytes: &[u8]) -> ViewResult<[u8; 32]> {
    bytes.try_into().map_err(|_| ViewError::IntegrityFailure)
}
fn digest(bytes: &[u8]) -> ViewResult<Digest32> {
    Digest32::new(array32(bytes)?).map_err(|_| ViewError::IntegrityFailure)
}
fn availability_from(value: u8) -> ViewResult<Availability> {
    Ok(match value {
        1 => Availability::Available,
        2 => Availability::NotEnabled,
        3 => Availability::NotYetProduced,
        4 => Availability::ContentUnavailable,
        5 => Availability::UnsupportedVersion,
        _ => return Err(ViewError::IntegrityFailure),
    })
}

/// Stable event ID: `0x` + hex `SHA256("PAXAI/view-event/v1" || 00 || MarketId32 ||
/// source_activity_id32 || effect_ordinal:u16 || ai_event_tag:u16)`.
#[must_use]
pub fn event_id(
    market: MarketId,
    source_activity: Digest32,
    effect_ordinal: u16,
    tag: u16,
) -> String {
    let digest: [u8; 32] = Sha256::new()
        .chain_update(b"PAXAI/view-event/v1")
        .chain_update([0])
        .chain_update(market.as_bytes())
        .chain_update(source_activity.as_bytes())
        .chain_update(effect_ordinal.to_be_bytes())
        .chain_update(tag.to_be_bytes())
        .finalize()
        .into();
    hex0x(&digest)
}

fn event_body(
    market: MarketId,
    snapshot: Digest32,
    source_activity: Digest32,
    subject: u64,
    facts: &CaptureFacts,
) -> ViewResult<Vec<u8>> {
    let mut out = [0u8; EVENT_BODY_BYTES];
    let mut w = codec::Writer::new(&mut out);
    w.u16(layerx_programs_ai_market::SCHEMA_VERSION)?;
    w.u16(SNAPSHOT_EVENT_TAG)?;
    w.put(market.as_bytes())?;
    w.put(snapshot.as_bytes())?;
    w.put(source_activity.as_bytes())?;
    w.u64(subject)?;
    w.u64(facts.proof.observed_sequence)?;
    w.put(facts.proof.native_state_root.as_bytes())?;
    Ok(out.to_vec())
}

fn blank_row() -> ViewResult<ParticipantRow> {
    Ok(ParticipantRow {
        kind: ParticipantKind::Worker,
        id: [0; 32],
        owner: PrincipalId::new([1; 32])?,
        generation: 0,
        identity_state: 0,
        frozen_member: false,
        frozen_generation: Presence::Absent,
        eligibility: 0,
        metadata: Presence::Absent,
        metadata_revision: 0,
        score: ScoreField::absent(Presence::Absent, ScoreStatus::NotProduced)?,
        reward: RewardField::unavailable(Availability::NotEnabled)?,
        history_status: Availability::NotYetProduced,
        history: Presence::Absent,
    })
}

/// Current F02 worker rows of the exact state. Evaluator rows join only through a frozen
/// roster whose digest the binding carries; until a producer records it the binding's roster
/// is absent, so no evaluator row is joined and F03 stays not-yet-produced.
fn rows_of(state: &[u8]) -> ViewResult<Vec<ParticipantRow>> {
    let mut rows = vec![blank_row()?; PARTICIPANT_ROWS_MAX];
    let n = queries::participant_rows(state, None, &mut rows)?;
    rows.truncate(n);
    Ok(rows)
}

/// Content identity of a capture. `SnapshotId` excludes checkpoint, settlement, rank and
/// publication time, so it is derived before any finality evidence exists.
fn content_identity(state: &[u8], facts: &CaptureFacts) -> ViewResult<(Digest32, SnapshotBinding)> {
    let provisional = FinalityEvidence {
        native_state_root: facts.proof.native_state_root,
        checkpoint: facts.proof.batch_id,
        settlement: Presence::Absent,
        rank: 0,
    };
    let binding = queries::bind_snapshot(state, facts, &provisional, 0)?;
    Ok((binding.snapshot_id()?, binding))
}

fn token_field(token: &str, range: core::ops::Range<usize>) -> ViewResult<&str> {
    token.get(range).ok_or(ViewError::InvalidEncoding)
}

fn token_generation(token: &str) -> ViewResult<u32> {
    let text = token_field(token, TOKEN_GENERATION_HEX)?.as_bytes();
    let mut value = 0u32;
    for c in text {
        let nibble = match c {
            b'0'..=b'9' => c - b'0',
            b'a'..=b'f' => c - b'a' + 10,
            _ => return Err(ViewError::InvalidEncoding),
        };
        value = (value << 4) | u32::from(nibble);
    }
    Ok(value)
}

const SNAPSHOT_COLUMNS: &str = "snapshot_id, market, chain, program, observed_sequence, \
    execution_height, batch_id, native_root, revision, state_digest, total_bytes, chunks, \
    read_verification, projection, state IS NOT NULL, availability, source_activity, epoch, \
    binding, checkpoint, rank, publication_time_ms, observed_at_ms";

fn record_from(row: &rusqlite::Row<'_>) -> rusqlite::Result<RawRecord> {
    Ok(RawRecord {
        snapshot_id: row.get(0)?,
        market: row.get(1)?,
        chain: row.get(2)?,
        program: row.get(3)?,
        observed_sequence: row.get(4)?,
        execution_height: row.get(5)?,
        batch_id: row.get(6)?,
        native_root: row.get(7)?,
        revision: row.get(8)?,
        state_digest: row.get(9)?,
        total_bytes: row.get(10)?,
        chunks: row.get(11)?,
        read: row.get(12)?,
        projection: row.get(13)?,
        retained: row.get(14)?,
        availability: row.get(15)?,
        source_activity: row.get(16)?,
        epoch: row.get(17)?,
        binding: row.get(18)?,
        checkpoint: row.get(19)?,
        rank: row.get(20)?,
        publication_time_ms: row.get(21)?,
        observed_at_ms: row.get(22)?,
    })
}

struct RawRecord {
    snapshot_id: Vec<u8>,
    market: Vec<u8>,
    chain: Vec<u8>,
    program: Vec<u8>,
    observed_sequence: i64,
    execution_height: i64,
    batch_id: Vec<u8>,
    native_root: Vec<u8>,
    revision: i64,
    state_digest: Vec<u8>,
    total_bytes: i64,
    chunks: i64,
    read: i64,
    projection: i64,
    retained: bool,
    availability: Vec<u8>,
    source_activity: Vec<u8>,
    epoch: Option<i64>,
    binding: Option<Vec<u8>>,
    checkpoint: Option<Vec<u8>>,
    rank: i64,
    publication_time_ms: Option<i64>,
    observed_at_ms: i64,
}

impl RawRecord {
    fn decode(self) -> ViewResult<SnapshotRecord> {
        let bad = |_| ViewError::IntegrityFailure;
        let mut availability = [Availability::NotYetProduced; FEATURES];
        if self.availability.len() != FEATURES {
            return Err(ViewError::IntegrityFailure);
        }
        for (slot, byte) in availability.iter_mut().zip(&self.availability) {
            *slot = availability_from(*byte)?;
        }
        Ok(SnapshotRecord {
            snapshot_id: digest(&self.snapshot_id)?,
            market: MarketId::new(array32(&self.market)?).map_err(bad)?,
            chain: ChainDomain::new(array32(&self.chain)?).map_err(bad)?,
            program: ProgramId::new(array32(&self.program)?).map_err(bad)?,
            observed_sequence: unsigned(self.observed_sequence)?,
            execution_height: unsigned(self.execution_height)?,
            batch_id: digest(&self.batch_id)?,
            native_state_root: digest(&self.native_root)?,
            revision: unsigned(self.revision)?,
            state_digest: StateDigest::new(array32(&self.state_digest)?).map_err(bad)?,
            total_bytes: u32::try_from(self.total_bytes)
                .map_err(|_| ViewError::IntegrityFailure)?,
            chunks: usize::try_from(self.chunks).map_err(|_| ViewError::IntegrityFailure)?,
            read: match self.read {
                1 => ReadVerification::Unverified,
                2 => ReadVerification::SequencerSigned,
                _ => return Err(ViewError::IntegrityFailure),
            },
            projection: ProjectionState::from_db(self.projection)?,
            availability,
            source_activity: digest(&self.source_activity)?,
            epoch: match self.epoch {
                Some(e) => Presence::Present(unsigned(e)?),
                None => Presence::Absent,
            },
            binding: self.binding,
            checkpoint: self.checkpoint.as_deref().map(digest).transpose()?,
            rank: u8::try_from(self.rank).map_err(|_| ViewError::IntegrityFailure)?,
            publication_time_ms: self.publication_time_ms.map(unsigned).transpose()?,
            observed_at_ms: unsigned(self.observed_at_ms)?,
            retained: self.retained,
        })
    }
}

fn load_record(tx: &Connection, snapshot: &Digest32) -> ViewResult<Option<SnapshotRecord>> {
    tx.query_row(
        &format!("SELECT {SNAPSHOT_COLUMNS} FROM ai_snapshot WHERE snapshot_id = ?1"),
        params![snapshot.as_bytes().as_slice()],
        record_from,
    )
    .optional()?
    .map(RawRecord::decode)
    .transpose()
}

fn load_state(tx: &Connection, snapshot: &Digest32) -> ViewResult<Option<Vec<u8>>> {
    Ok(tx
        .query_row(
            "SELECT state FROM ai_snapshot WHERE snapshot_id = ?1",
            params![snapshot.as_bytes().as_slice()],
            |row| row.get::<_, Option<Vec<u8>>>(0),
        )
        .optional()?
        .flatten())
}

fn load_stream(tx: &Connection, market: &MarketId) -> ViewResult<Option<StreamCursor>> {
    let raw = tx
        .query_row(
            "SELECT observed_sequence, observed_root, finalized_sequence, finalized_snapshot, \
             next_subject, quarantine FROM ai_stream WHERE market = ?1",
            params![market.as_bytes().as_slice()],
            |row| {
                Ok((
                    row.get::<_, i64>(0)?,
                    row.get::<_, Option<Vec<u8>>>(1)?,
                    row.get::<_, Option<i64>>(2)?,
                    row.get::<_, Option<Vec<u8>>>(3)?,
                    row.get::<_, i64>(4)?,
                    row.get::<_, Option<String>>(5)?,
                ))
            },
        )
        .optional()?;
    raw.map(|(observed, root, finalized, snapshot, next, quarantine)| {
        Ok(StreamCursor {
            market: *market,
            observed_sequence: unsigned(observed)?,
            observed_root: root.as_deref().map(digest).transpose()?,
            finalized_sequence: finalized.map(unsigned).transpose()?,
            finalized_snapshot: snapshot.as_deref().map(digest).transpose()?,
            next_subject: unsigned(next)?,
            quarantine,
        })
    })
    .transpose()
}

fn record_alert(
    conn: &Connection,
    market: &[u8; 32],
    subject: &[u8; 32],
    error: &ViewError,
) -> ViewResult<()> {
    conn.execute(
        "INSERT INTO ai_alert (market, subject, category) VALUES (?1, ?2, ?3)",
        params![market.as_slice(), subject.as_slice(), error.category()],
    )?;
    Ok(())
}

/// Quarantines the market stream in its own transaction: evidence stays, finalized
/// publication stops, and one alert records IDs and category.
fn quarantine(
    conn: &mut Connection,
    market: &MarketId,
    subject: &[u8; 32],
    error: ViewError,
) -> ViewError {
    let outcome = (|| -> ViewResult<()> {
        let tx = conn.transaction()?;
        tx.execute(
            "INSERT INTO ai_stream (market, observed_sequence, observed_root, next_subject, quarantine) \
             VALUES (?1, 0, NULL, 1, ?2) \
             ON CONFLICT(market) DO UPDATE SET quarantine = COALESCE(quarantine, excluded.quarantine)",
            params![market.as_bytes().as_slice(), error.category()],
        )?;
        record_alert(&tx, market.as_bytes(), subject, &error)?;
        tx.commit()?;
        Ok(())
    })();
    match outcome {
        Ok(()) => error,
        Err(store) => store,
    }
}

fn refuse_quarantined(stream: Option<&StreamCursor>) -> ViewResult<()> {
    if stream.is_some_and(|s| s.quarantine.is_some()) {
        Err(ViewError::Quarantined)
    } else {
        Ok(())
    }
}

/// Moves the oldest finalized, unleased, non-latest bodies out of the recent cache until
/// one more body fits within both the count and byte bounds.
fn make_room(
    tx: &Transaction<'_>,
    market: &MarketId,
    incoming: usize,
    now_ms: u64,
) -> ViewResult<()> {
    let market_bytes = market.as_bytes().as_slice();
    loop {
        let (count, bytes): (i64, i64) = tx.query_row(
            "SELECT COUNT(*), COALESCE(SUM(length(state)), 0) FROM ai_snapshot \
             WHERE market = ?1 AND state IS NOT NULL",
            params![market_bytes],
            |row| Ok((row.get(0)?, row.get(1)?)),
        )?;
        let count = usize::try_from(count).map_err(|_| ViewError::IntegrityFailure)?;
        let bytes = usize::try_from(bytes).map_err(|_| ViewError::IntegrityFailure)?;
        if count < CACHED_SNAPSHOTS && bytes.saturating_add(incoming) <= MARKET_CACHE_MAX_BYTES {
            return Ok(());
        }
        let victim: Option<Vec<u8>> = tx
            .query_row(
                "SELECT s.snapshot_id FROM ai_snapshot s \
                 LEFT JOIN ai_stream t ON t.market = s.market \
                 LEFT JOIN ai_lease l ON l.snapshot_id = s.snapshot_id AND l.expiry_ms > ?2 \
                 WHERE s.market = ?1 AND s.state IS NOT NULL AND s.projection = 3 \
                 AND l.snapshot_id IS NULL \
                 AND (t.finalized_snapshot IS NULL OR t.finalized_snapshot != s.snapshot_id) \
                 ORDER BY s.observed_sequence LIMIT 1",
                params![market_bytes, signed(now_ms)?],
                |row| row.get(0),
            )
            .optional()?;
        let Some(victim) = victim else {
            return Err(ViewError::CapacityExceeded);
        };
        tx.execute(
            "UPDATE ai_snapshot SET state = NULL, projection = 4 WHERE snapshot_id = ?1",
            params![victim],
        )?;
    }
}

fn insert_rows(
    tx: &Transaction<'_>,
    snapshot_id: Digest32,
    rows: &[ParticipantRow],
) -> ViewResult<()> {
    for (ordinal, row) in rows.iter().enumerate() {
        let mut encoded = [0u8; ROW_BYTES_MAX];
        let mut w = codec::Writer::new(&mut encoded);
        row.encode(&mut w)?;
        let n = w.len();
        tx.execute(
            "INSERT INTO ai_participant (snapshot_id, ordinal, kind, participant, row) \
                 VALUES (?1, ?2, ?3, ?4, ?5)",
            params![
                snapshot_id.as_bytes().as_slice(),
                i64::try_from(ordinal).map_err(|_| ViewError::CapacityExceeded)?,
                row.kind as u8,
                row.id.as_slice(),
                encoded.get(..n).ok_or(ViewError::CapacityExceeded)?,
            ],
        )?;
    }
    Ok(())
}

/// Writes the snapshot's not-yet-publishable outbox row. A reused event ID is a conflict.
fn insert_outbox(
    tx: &Transaction<'_>,
    market: MarketId,
    snapshot_id: Digest32,
    observation: &Observation<'_>,
    subject: u64,
) -> ViewResult<()> {
    let facts = observation.facts;
    let id = event_id(market, observation.source_activity, 0, SNAPSHOT_EVENT_TAG);
    let body = event_body(
        market,
        snapshot_id,
        observation.source_activity,
        subject,
        &facts,
    )?;
    let prior: Option<Vec<u8>> = tx
        .query_row(
            "SELECT body FROM ai_outbox WHERE event_id = ?1",
            params![id],
            |row| row.get(0),
        )
        .optional()?;
    if prior.is_some() {
        return Err(ViewError::SnapshotConflict);
    }
    tx.execute(
        "INSERT INTO ai_outbox (event_id, market, subject_sequence, snapshot_id, body) \
             VALUES (?1, ?2, ?3, ?4, ?5)",
        params![
            id,
            market.as_bytes().as_slice(),
            signed(subject)?,
            snapshot_id.as_bytes().as_slice(),
            body
        ],
    )?;
    Ok(())
}

impl ProjectionStore {
    /// Opens (creating if absent) the projection file with WAL and full synchronous commits.
    ///
    /// # Errors
    /// Store failures.
    pub fn open(path: &Path) -> ViewResult<Self> {
        let connection = Connection::open(path)?;
        connection.pragma_update(None, "journal_mode", "WAL")?;
        Self::initialise(connection)
    }

    /// # Errors
    /// Store failures.
    pub fn open_in_memory() -> ViewResult<Self> {
        Self::initialise(Connection::open_in_memory()?)
    }

    fn initialise(connection: Connection) -> ViewResult<Self> {
        connection.pragma_update(None, "synchronous", "FULL")?;
        connection.pragma_update(None, "foreign_keys", "ON")?;
        connection.execute_batch(SCHEMA)?;
        Ok(Self {
            connection: Mutex::new(connection),
        })
    }

    fn lock(&self) -> ViewResult<std::sync::MutexGuard<'_, Connection>> {
        self.connection
            .lock()
            .map_err(|_| ViewError::Store("projection store poisoned".to_owned()))
    }

    /// Records one complete capture as observed (unfinalized) content: snapshot, rows,
    /// projection cursor and its not-yet-publishable outbox row, in one transaction.
    /// A repeated identical capture is idempotent. The same `SnapshotId` with different
    /// stored content, another root at an already-captured sequence, an unsupported codec
    /// or a reused event ID with another body quarantines the stream.
    ///
    /// # Errors
    /// The `CAPTURE_FINALIZED_SNAPSHOT` refusals; failure commits nothing of the capture.
    pub fn observe(&self, observation: &Observation<'_>) -> ViewResult<SnapshotRecord> {
        let facts = observation.facts;
        let market = codec::derive_market(facts.proof.chain, facts.proof.program)?;
        let mut conn = self.lock()?;
        refuse_quarantined(load_stream(&conn, &market)?.as_ref())?;
        let checked = (|| -> ViewResult<_> {
            if observation.state.len() > MAX_STATE_BYTES.min(SNAPSHOT_BODY_MAX_BYTES) {
                return Err(ViewError::CapacityExceeded);
            }
            if usize::try_from(facts.total_bytes).ok() != Some(observation.state.len())
                || codec::state_digest(observation.state)? != facts.digest
            {
                return Err(ViewError::IntegrityFailure);
            }
            let (snapshot_id, binding) = content_identity(observation.state, &facts)?;
            if binding.market != market {
                return Err(ViewError::WrongDomain);
            }
            let availability = queries::feature_availability(observation.state)?;
            let rows = rows_of(observation.state)?;
            Ok((snapshot_id, binding, availability, rows))
        })();
        let (snapshot_id, binding, availability, rows) = match checked {
            Ok(v) => v,
            Err(e @ ViewError::UnsupportedVersion) => {
                return Err(quarantine(&mut conn, &market, facts.digest.as_bytes(), e))
            }
            Err(e) => return Err(e),
        };
        let tx = conn.transaction()?;
        let outcome = Self::insert_observation(
            &tx,
            observation,
            market,
            snapshot_id,
            &binding,
            &availability,
            &rows,
        );
        match outcome {
            Ok(record) => {
                tx.commit()?;
                Ok(record)
            }
            Err(e @ ViewError::SnapshotConflict) => {
                drop(tx);
                Err(quarantine(&mut conn, &market, snapshot_id.as_bytes(), e))
            }
            Err(ViewError::CapacityExceeded) => {
                drop(tx);
                record_alert(
                    &conn,
                    market.as_bytes(),
                    snapshot_id.as_bytes(),
                    &ViewError::CapacityExceeded,
                )?;
                Err(ViewError::CapacityExceeded)
            }
            Err(e) => Err(e),
        }
    }

    fn insert_observation(
        tx: &Transaction<'_>,
        observation: &Observation<'_>,
        market: MarketId,
        snapshot_id: Digest32,
        binding: &SnapshotBinding,
        availability: &[Availability; FEATURES],
        rows: &[ParticipantRow],
    ) -> ViewResult<SnapshotRecord> {
        let facts = observation.facts;
        let proof = facts.proof;
        if let Some(existing) = load_record(tx, &snapshot_id)? {
            let stored = load_state(tx, &snapshot_id)?;
            let same_content = existing.facts() == facts
                && existing.source_activity == observation.source_activity
                && stored
                    .as_deref()
                    .is_none_or(|bytes| bytes == observation.state);
            return if same_content {
                Ok(existing)
            } else {
                Err(ViewError::SnapshotConflict)
            };
        }
        let at_sequence: Option<Vec<u8>> = tx
            .query_row(
                "SELECT snapshot_id FROM ai_snapshot WHERE market = ?1 AND observed_sequence = ?2",
                params![
                    market.as_bytes().as_slice(),
                    signed(proof.observed_sequence)?
                ],
                |row| row.get(0),
            )
            .optional()?;
        if at_sequence.is_some() {
            return Err(ViewError::SnapshotConflict);
        }
        make_room(
            tx,
            &market,
            observation.state.len(),
            observation.observed_at_ms,
        )?;
        let stream = load_stream(tx, &market)?;
        let subject = stream.as_ref().map_or(1, |s| s.next_subject);
        let projection = match observation.read {
            ReadVerification::Unverified => ProjectionState::ObservedUnverified,
            ReadVerification::SequencerSigned => ProjectionState::EvidenceVerifiedUnfinalized,
        };
        let availability_bytes: Vec<u8> = availability.iter().map(|a| *a as u8).collect();
        tx.execute(
            "INSERT INTO ai_snapshot (snapshot_id, market, chain, program, observed_sequence, \
             execution_height, batch_id, native_root, revision, state_digest, total_bytes, chunks, \
             read_verification, projection, state, availability, source_activity, epoch, binding, \
             checkpoint, rank, publication_time_ms, observed_at_ms) VALUES (?1, ?2, ?3, ?4, ?5, \
             ?6, ?7, ?8, ?9, ?10, ?11, ?12, ?13, ?14, ?15, ?16, ?17, NULL, NULL, NULL, 0, NULL, ?18)",
            params![
                snapshot_id.as_bytes().as_slice(),
                market.as_bytes().as_slice(),
                proof.chain.as_bytes().as_slice(),
                proof.program.as_bytes().as_slice(),
                signed(proof.observed_sequence)?,
                signed(proof.execution_height)?,
                proof.batch_id.as_bytes().as_slice(),
                proof.native_state_root.as_bytes().as_slice(),
                signed(binding.revision)?,
                facts.digest.as_bytes().as_slice(),
                i64::from(facts.total_bytes),
                i64::try_from(facts.chunks).map_err(|_| ViewError::CapacityExceeded)?,
                observation.read as u8,
                projection as u8,
                observation.state,
                availability_bytes,
                observation.source_activity.as_bytes().as_slice(),
                signed(observation.observed_at_ms)?,
            ],
        )?;
        insert_rows(tx, snapshot_id, rows)?;
        insert_outbox(tx, market, snapshot_id, observation, subject)?;
        let advance = stream
            .as_ref()
            .is_none_or(|s| proof.observed_sequence > s.observed_sequence);
        let (head_sequence, head_root) = match (&stream, advance) {
            (Some(s), false) => (s.observed_sequence, s.observed_root),
            _ => (proof.observed_sequence, Some(proof.native_state_root)),
        };
        tx.execute(
            "INSERT INTO ai_stream (market, observed_sequence, observed_root, next_subject) \
             VALUES (?1, ?2, ?3, ?4) ON CONFLICT(market) DO UPDATE SET \
             observed_sequence = excluded.observed_sequence, observed_root = excluded.observed_root, \
             next_subject = excluded.next_subject",
            params![
                market.as_bytes().as_slice(),
                signed(head_sequence)?,
                head_root.map(Digest32::bytes).map(Vec::from),
                signed(subject.checked_add(1).ok_or(ViewError::CapacityExceeded)?)?,
            ],
        )?;
        load_record(tx, &snapshot_id)?.ok_or(ViewError::ProjectionUnavailable)
    }

    /// Promotes a saved capture to finalized-publishable with an actual checkpoint certificate
    /// for its exact root. The saved state is re-bound; latest mutable state is never reread.
    ///
    /// # Errors
    /// `FinalityUnavailable` for a certificate that does not verify at rank 4, `WrongDomain`
    /// for another network (alerted), `BindingMismatch` for another batch, sequence or root,
    /// `ProjectionUnavailable` for an unknown or archived snapshot.
    pub fn promote(
        &self,
        snapshot_id: &Digest32,
        proof: &CheckpointProof<'_>,
        authority: &FinalityAuthority,
        publication_time_ms: u64,
    ) -> ViewResult<SnapshotRecord> {
        let mut conn = self.lock()?;
        let record = load_record(&conn, snapshot_id)?.ok_or(ViewError::ProjectionUnavailable)?;
        refuse_quarantined(load_stream(&conn, &record.market)?.as_ref())?;
        if record.projection.finalized() {
            return Ok(record);
        }
        let state = load_state(&conn, snapshot_id)?.ok_or(ViewError::ProjectionUnavailable)?;
        if record.read != ReadVerification::SequencerSigned {
            return Err(ViewError::BindingMismatch);
        }
        let report = verify_certificate(
            proof.certificate,
            &authority.guarantors,
            &proof.registered_checkpoint_id,
            authority.settlement,
            proof.registered_settlement_reference,
        )
        .map_err(|_| ViewError::FinalityUnavailable)?;
        if report.level().wire_rank() < VerificationLevel::CHECKPOINT_FINALISED.wire_rank() {
            return Err(ViewError::FinalityUnavailable);
        }
        let checkpoint = report
            .evidence()
            .checkpoint_id()
            .ok_or(ViewError::FinalityUnavailable)?;
        if report.network_id() != authority.network_id {
            let error = ViewError::WrongDomain;
            record_alert(&conn, record.market.as_bytes(), &checkpoint, &error)?;
            return Err(error);
        }
        let header = decode_batch_header(proof.certificate.checkpoint().header_bytes())
            .map_err(|_| ViewError::FinalityUnavailable)?;
        let root = record.native_state_root.bytes();
        let batch = program_execution_batch_id(
            header.previous_state_root(),
            header.activity_merkle_root(),
            header.first_sequence(),
            header.last_sequence(),
            header.batch_number(),
        )
        .map_err(|_| ViewError::BindingMismatch)?;
        if header.last_sequence() != record.observed_sequence
            || header.resulting_state_root() != root
            || batch != record.batch_id.bytes()
        {
            return Err(ViewError::BindingMismatch);
        }
        let finality = FinalityEvidence {
            native_state_root: record.native_state_root,
            checkpoint: Digest32::new(checkpoint)?,
            settlement: Presence::Absent,
            rank: MINIMUM_PUBLICATION_RANK,
        };
        let binding =
            queries::bind_snapshot(&state, &record.facts(), &finality, publication_time_ms)?;
        binding.require_finalized()?;
        if binding.snapshot_id()? != *snapshot_id {
            return Err(ViewError::IntegrityFailure);
        }
        let mut encoded = [0u8; BINDING_MAX_BYTES];
        let n = binding.encode(&mut encoded)?;
        let tx = conn.transaction()?;
        tx.execute(
            "UPDATE ai_snapshot SET projection = 3, binding = ?2, checkpoint = ?3, rank = ?4, \
             publication_time_ms = ?5, epoch = ?6 WHERE snapshot_id = ?1",
            params![
                snapshot_id.as_bytes().as_slice(),
                encoded.get(..n).ok_or(ViewError::CapacityExceeded)?,
                checkpoint.as_slice(),
                binding.rank,
                signed(publication_time_ms)?,
                match binding.epoch {
                    Presence::Present(e) => Some(signed(e)?),
                    Presence::Absent => None,
                },
            ],
        )?;
        tx.execute(
            "UPDATE ai_stream SET finalized_sequence = ?2, finalized_snapshot = ?3 \
             WHERE market = ?1 AND (finalized_sequence IS NULL OR finalized_sequence < ?2)",
            params![
                record.market.as_bytes().as_slice(),
                signed(record.observed_sequence)?,
                snapshot_id.as_bytes().as_slice(),
            ],
        )?;
        tx.commit()?;
        load_record(&conn, snapshot_id)?.ok_or(ViewError::ProjectionUnavailable)
    }

    /// Removes provisional snapshots above `to_sequence` with their rows and unpublished
    /// outbox rows atomically. Crossing the verified finality boundary is refused and
    /// quarantines the stream; finalized history is never rewritten.
    ///
    /// # Errors
    /// `RollbackRefused` below the finalized boundary, `Quarantined` for a quarantined stream.
    pub fn rollback(&self, market: &MarketId, to_sequence: u64) -> ViewResult<usize> {
        let mut conn = self.lock()?;
        let stream = load_stream(&conn, market)?;
        refuse_quarantined(stream.as_ref())?;
        let Some(stream) = stream else {
            return Ok(0);
        };
        if stream.finalized_sequence.is_some_and(|f| to_sequence < f) {
            let subject = stream.finalized_snapshot.map_or([0; 32], Digest32::bytes);
            return Err(quarantine(
                &mut conn,
                market,
                &subject,
                ViewError::RollbackRefused,
            ));
        }
        let tx = conn.transaction()?;
        let m = market.as_bytes().as_slice();
        let removed = tx.execute(
            "DELETE FROM ai_snapshot WHERE market = ?1 AND observed_sequence > ?2 \
             AND projection NOT IN (3, 4)",
            params![m, signed(to_sequence)?],
        )?;
        let head: Option<(i64, Vec<u8>)> = tx
            .query_row(
                "SELECT observed_sequence, native_root FROM ai_snapshot WHERE market = ?1 \
                 ORDER BY observed_sequence DESC LIMIT 1",
                params![m],
                |row| Ok((row.get(0)?, row.get(1)?)),
            )
            .optional()?;
        let next: i64 = tx.query_row(
            "SELECT COALESCE(MAX(subject_sequence), 0) + 1 FROM ai_outbox WHERE market = ?1",
            params![m],
            |row| row.get(0),
        )?;
        match head {
            Some((sequence, root)) => tx.execute(
                "UPDATE ai_stream SET observed_sequence = ?2, observed_root = ?3, next_subject = ?4 \
                 WHERE market = ?1",
                params![m, sequence, root, next],
            )?,
            None => tx.execute("DELETE FROM ai_stream WHERE market = ?1", params![m])?,
        };
        tx.commit()?;
        Ok(removed)
    }

    /// Finalized, unpublished outbox rows in contiguous subject order; publication stops at
    /// the first row whose snapshot is not finalized and for a quarantined stream.
    ///
    /// # Errors
    /// Store failures.
    pub fn pending_events(&self, market: &MarketId, limit: usize) -> ViewResult<Vec<OutboxEvent>> {
        let conn = self.lock()?;
        if load_stream(&conn, market)?.is_some_and(|s| s.quarantine.is_some()) {
            return Ok(Vec::new());
        }
        let mut statement = conn.prepare(
            "SELECT o.event_id, o.subject_sequence, o.snapshot_id, o.body, s.binding, s.rank, \
             s.projection FROM ai_outbox o JOIN ai_snapshot s ON s.snapshot_id = o.snapshot_id \
             WHERE o.market = ?1 AND o.published_at_ms IS NULL ORDER BY o.subject_sequence",
        )?;
        let rows = statement.query_map(params![market.as_bytes().as_slice()], |row| {
            Ok((
                row.get::<_, String>(0)?,
                row.get::<_, i64>(1)?,
                row.get::<_, Vec<u8>>(2)?,
                row.get::<_, Vec<u8>>(3)?,
                row.get::<_, Option<Vec<u8>>>(4)?,
                row.get::<_, i64>(5)?,
                row.get::<_, i64>(6)?,
            ))
        })?;
        let mut out = Vec::new();
        for row in rows {
            let (event_id, subject, snapshot, body, binding, rank, projection) = row?;
            let rank = u8::try_from(rank).map_err(|_| ViewError::IntegrityFailure)?;
            let (Some(binding), true) = (
                binding,
                ProjectionState::from_db(projection)?.finalized()
                    && rank >= MINIMUM_PUBLICATION_RANK,
            ) else {
                break;
            };
            if out.len() == limit {
                break;
            }
            out.push(OutboxEvent {
                event_id,
                market: *market,
                subject_sequence: unsigned(subject)?,
                snapshot_id: digest(&snapshot)?,
                body,
                binding,
                rank,
                published: false,
            });
        }
        Ok(out)
    }

    /// Acknowledges one publication. Retrying with the identical ID and body is idempotent;
    /// another body under the same ID is refused.
    ///
    /// # Errors
    /// `ProjectionUnavailable` for an unknown event, `FinalityUnavailable` before promotion,
    /// `SnapshotConflict` for a changed body.
    pub fn mark_published(&self, event_id: &str, body: &[u8], now_ms: u64) -> ViewResult<bool> {
        let conn = self.lock()?;
        let row: Option<(Vec<u8>, Option<i64>, i64)> = conn
            .query_row(
                "SELECT o.body, o.published_at_ms, s.projection FROM ai_outbox o \
                 JOIN ai_snapshot s ON s.snapshot_id = o.snapshot_id WHERE o.event_id = ?1",
                params![event_id],
                |row| Ok((row.get(0)?, row.get(1)?, row.get(2)?)),
            )
            .optional()?;
        let (stored, published, projection) = row.ok_or(ViewError::ProjectionUnavailable)?;
        if !ProjectionState::from_db(projection)?.finalized() {
            return Err(ViewError::FinalityUnavailable);
        }
        if stored != body {
            return Err(ViewError::SnapshotConflict);
        }
        if published.is_some() {
            return Ok(false);
        }
        conn.execute(
            "UPDATE ai_outbox SET published_at_ms = ?2 WHERE event_id = ?1",
            params![event_id, signed(now_ms)?],
        )?;
        Ok(true)
    }

    /// One stored snapshot record.
    ///
    /// # Errors
    /// Store or integrity failures.
    pub fn snapshot(&self, snapshot_id: &Digest32) -> ViewResult<Option<SnapshotRecord>> {
        load_record(&*self.lock()?, snapshot_id)
    }

    /// The committed participant rows of one snapshot, as canonical encoded bytes in ordinal
    /// order.
    ///
    /// # Errors
    /// Store failures.
    pub fn committed_rows(&self, snapshot_id: &Digest32) -> ViewResult<Vec<Vec<u8>>> {
        let conn = self.lock()?;
        let mut statement =
            conn.prepare("SELECT row FROM ai_participant WHERE snapshot_id = ?1 ORDER BY ordinal")?;
        let rows = statement
            .query_map(params![snapshot_id.as_bytes().as_slice()], |row| row.get(0))?
            .collect::<Result<Vec<Vec<u8>>, _>>()?;
        Ok(rows)
    }

    /// The projection cursor of one market.
    ///
    /// # Errors
    /// Store or integrity failures.
    pub fn stream(&self, market: &MarketId) -> ViewResult<Option<StreamCursor>> {
        load_stream(&*self.lock()?, market)
    }

    /// Operator alerts in insertion order.
    ///
    /// # Errors
    /// Store or integrity failures.
    pub fn alerts(&self) -> ViewResult<Vec<Alert>> {
        let conn = self.lock()?;
        let mut statement =
            conn.prepare("SELECT market, subject, category FROM ai_alert ORDER BY id")?;
        let rows = statement.query_map([], |row| {
            Ok((
                row.get::<_, Vec<u8>>(0)?,
                row.get::<_, Vec<u8>>(1)?,
                row.get::<_, String>(2)?,
            ))
        })?;
        let mut out = Vec::new();
        for row in rows {
            let (market, subject, category) = row?;
            out.push(Alert {
                market: array32(&market)?,
                subject: array32(&subject)?,
                category,
            });
        }
        Ok(out)
    }

    /// Operator action: moves a finalized, unleased, non-latest snapshot body to the external
    /// digest-verified archive. Binding, rows and outbox rows remain.
    ///
    /// # Errors
    /// `ProjectionUnavailable` for an unknown or unfinalized snapshot or the latest pointer,
    /// `CapacityExceeded` while a cursor lease pins it.
    pub fn archive(&self, snapshot_id: &Digest32, now_ms: u64) -> ViewResult<()> {
        let conn = self.lock()?;
        let record = load_record(&conn, snapshot_id)?.ok_or(ViewError::ProjectionUnavailable)?;
        let latest = load_stream(&conn, &record.market)?.and_then(|s| s.finalized_snapshot);
        if record.projection != ProjectionState::FinalizedPublishable
            || latest == Some(*snapshot_id)
        {
            return Err(ViewError::ProjectionUnavailable);
        }
        let leased: i64 = conn.query_row(
            "SELECT COUNT(*) FROM ai_lease WHERE snapshot_id = ?1 AND expiry_ms > ?2",
            params![snapshot_id.as_bytes().as_slice(), signed(now_ms)?],
            |row| row.get(0),
        )?;
        if leased != 0 {
            return Err(ViewError::CapacityExceeded);
        }
        conn.execute(
            "UPDATE ai_snapshot SET state = NULL, projection = 4 WHERE snapshot_id = ?1",
            params![snapshot_id.as_bytes().as_slice()],
        )?;
        Ok(())
    }

    /// Restores archived content after verifying it against the retained state digest and
    /// committed binding. Nothing else is ever substituted for missing content.
    ///
    /// # Errors
    /// `IntegrityFailure` for any digest, binding or row difference (alerted),
    /// `ProjectionUnavailable` for a snapshot that is not archived.
    pub fn restore_archived(&self, snapshot_id: &Digest32, state: &[u8]) -> ViewResult<()> {
        let conn = self.lock()?;
        let record = load_record(&conn, snapshot_id)?.ok_or(ViewError::ProjectionUnavailable)?;
        if record.projection != ProjectionState::Archived {
            return Err(ViewError::ProjectionUnavailable);
        }
        let verified = (|| -> ViewResult<()> {
            if codec::state_digest(state)? != record.state_digest {
                return Err(ViewError::IntegrityFailure);
            }
            let binding = rebind(&record, state)?;
            if binding.snapshot_id()? != *snapshot_id {
                return Err(ViewError::IntegrityFailure);
            }
            Ok(())
        })();
        if let Err(error) = verified {
            let error = match error {
                ViewError::Store(_) => error,
                _ => ViewError::IntegrityFailure,
            };
            record_alert(
                &conn,
                record.market.as_bytes(),
                snapshot_id.as_bytes(),
                &error,
            )?;
            return Err(error);
        }
        conn.execute(
            "UPDATE ai_snapshot SET state = ?2, projection = 3 WHERE snapshot_id = ?1",
            params![snapshot_id.as_bytes().as_slice(), state],
        )?;
        Ok(())
    }

    fn resolve(
        conn: &Connection,
        market: &MarketId,
        selector: Option<&Digest32>,
    ) -> ViewResult<(SnapshotRecord, Vec<u8>, SnapshotBinding)> {
        let stream = load_stream(conn, market)?.ok_or(ViewError::AccessRefused)?;
        let id = match selector {
            Some(id) => *id,
            None => stream
                .finalized_snapshot
                .ok_or(ViewError::FinalityUnavailable)?,
        };
        let record = load_record(conn, &id)?
            .filter(|r| r.market == *market)
            .ok_or(ViewError::AccessRefused)?;
        if !record.projection.finalized() {
            return Err(ViewError::FinalityUnavailable);
        }
        let Some(state) = load_state(conn, &id)? else {
            let oldest: Option<(Vec<u8>, i64)> = conn
                .query_row(
                    "SELECT snapshot_id, observed_sequence FROM ai_snapshot WHERE market = ?1 \
                     AND state IS NOT NULL AND projection = 3 ORDER BY observed_sequence LIMIT 1",
                    params![market.as_bytes().as_slice()],
                    |row| Ok((row.get(0)?, row.get(1)?)),
                )
                .optional()?;
            return Err(ViewError::SnapshotPruned {
                oldest: oldest
                    .map(|(id, seq)| Ok::<_, ViewError>((digest(&id)?, unsigned(seq)?)))
                    .transpose()?,
            });
        };
        let binding = rebind(&record, &state)?;
        Ok((record, state, binding))
    }

    /// `GET_MARKET_VIEW` over one exact snapshot or the latest finalized one. Historical
    /// inspection stays available with an explicit stale label.
    ///
    /// # Errors
    /// `AccessRefused` for an unknown market or snapshot, `FinalityUnavailable` for an
    /// unfinalized snapshot, `SnapshotPruned` for archived content.
    pub fn market_view(
        &self,
        _viewer: &Viewer,
        market: &MarketId,
        selector: Option<&Digest32>,
        verified_authority_height: Option<u64>,
    ) -> ViewResult<MarketView> {
        let conn = self.lock()?;
        let (record, _, binding) = Self::resolve(&conn, market, selector)?;
        let freshness = verified_authority_height.map_or(Freshness::Unknown, |height| match height
            .saturating_sub(record.execution_height)
        {
            lag if lag > AUTHORITY_FRESHNESS_HEIGHTS => Freshness::Stale { lag },
            _ => Freshness::Current,
        });
        Ok(MarketView {
            snapshot_id: record.snapshot_id,
            binding,
            projection: record.projection,
            availability: record.availability,
            source_activity: record.source_activity,
            freshness,
        })
    }

    /// Fail-closed authority gate for routing, signing and runner release: the latest
    /// finalized snapshot must be within the F02 freshness limit of the independently
    /// verified authority height. It never alters any epoch clock or evidence.
    ///
    /// # Errors
    /// `FinalityUnavailable` without a finalized snapshot, `AuthorityStale` above the limit.
    pub fn require_fresh_authority(
        &self,
        market: &MarketId,
        verified_authority_height: u64,
    ) -> ViewResult<SnapshotRecord> {
        let conn = self.lock()?;
        let stream = load_stream(&conn, market)?;
        refuse_quarantined(stream.as_ref())?;
        let id = stream
            .and_then(|s| s.finalized_snapshot)
            .ok_or(ViewError::FinalityUnavailable)?;
        let record = load_record(&conn, &id)?.ok_or(ViewError::FinalityUnavailable)?;
        let lag = verified_authority_height.saturating_sub(record.execution_height);
        if lag > AUTHORITY_FRESHNESS_HEIGHTS {
            return Err(ViewError::AuthorityStale { lag });
        }
        Ok(record)
    }

    /// `LIST_PARTICIPANTS` over one pinned immutable rowset. The cursor is authenticated before
    /// any row work and is bound to the viewer, market, snapshot and exact filters.
    ///
    /// # Errors
    /// `CursorExpired`, `CursorMismatch`, `AccessRefused`, `FinalityUnavailable`,
    /// `SnapshotPruned`, `ResponseTooLarge`.
    pub fn participants(
        &self,
        viewer: &Viewer,
        keys: &CursorKeyring,
        request: &PageRequest,
        now_ms: u64,
    ) -> ViewResult<ParticipantPage> {
        if request.limit == 0 || usize::from(request.limit) > PAGE_MAX_ROWS {
            return Err(ViewError::InvalidEncoding);
        }
        let scope = |snapshot| CursorScope {
            visibility: viewer.visibility,
            market: request.market,
            snapshot,
            filter: request.filter,
            active_only: request.active_only,
        };
        let (selector, start) = match &request.cursor {
            None => (request.snapshot, 0),
            Some(token) => {
                if token.len() != CURSOR_TOKEN_BYTES {
                    return Err(ViewError::InvalidEncoding);
                }
                let pinned = Digest32::new(parse_hex32(token_field(token, TOKEN_SNAPSHOT_HEX)?)?)?;
                if request.snapshot.is_some_and(|s| s != pinned) {
                    return Err(ViewError::CursorMismatch);
                }
                let key = keys
                    .for_generation(token_generation(token)?, now_ms)
                    .ok_or(ViewError::CursorExpired)?;
                let start = queries::open_cursor(
                    &key,
                    &scope(pinned),
                    token,
                    now_ms,
                    usize::from(u16::MAX),
                )?;
                (Some(pinned), start)
            }
        };
        let conn = self.lock()?;
        let (record, state, binding) = Self::resolve(&conn, &request.market, selector.as_ref())?;
        let rows = rows_of(&state)?;
        let selected = rows
            .iter()
            .filter(|r| {
                let kind = match request.filter {
                    KindFilter::All => true,
                    KindFilter::Worker => r.kind == ParticipantKind::Worker,
                    KindFilter::Evaluator => r.kind == ParticipantKind::Evaluator,
                };
                kind && (!request.active_only || r.eligibility & queries::ELIGIBLE_SERVING != 0)
            })
            .count();
        if start != 0 && usize::from(start) >= selected {
            return Err(ViewError::CursorMismatch);
        }
        let mut page = [blank_row()?; PAGE_MAX_ROWS];
        let (written, next) = queries::select_page(
            &rows,
            request.filter,
            request.active_only,
            start,
            request.limit,
            &mut page,
        )?;
        let page_rows = page.get(..written).ok_or(ViewError::CapacityExceeded)?;
        let cursor = match next {
            None => None,
            Some(next) => {
                let mut token = [0u8; CURSOR_TOKEN_BYTES];
                let issued = queries::issue_cursor(
                    &keys.issuing(),
                    &scope(record.snapshot_id),
                    next,
                    now_ms,
                    &mut token,
                )?
                .to_owned();
                let expiry = now_ms
                    .checked_add(CURSOR_LIFETIME_MS)
                    .ok_or(ViewError::CapacityExceeded)?;
                conn.execute(
                    "INSERT INTO ai_lease (snapshot_id, expiry_ms) VALUES (?1, ?2) \
                     ON CONFLICT(snapshot_id) DO UPDATE SET expiry_ms = MAX(expiry_ms, excluded.expiry_ms)",
                    params![record.snapshot_id.as_bytes().as_slice(), signed(expiry)?],
                )?;
                Some(issued)
            }
        };
        let mut frame = vec![0u8; PAGE_MAX_BYTES];
        let encoded_bytes =
            queries::encode_page(record.snapshot_id, page_rows, cursor.as_deref(), &mut frame)
                .map_err(|e| match e {
                    QueryError::Application(CAPACITY) => ViewError::ResponseTooLarge,
                    other => other.into(),
                })?;
        Ok(ParticipantPage {
            snapshot_id: record.snapshot_id,
            binding,
            availability: record.availability,
            rows: page_rows.to_vec(),
            cursor,
            encoded_bytes,
        })
    }

    /// `GET_HISTORY` in ascending epoch order, default 16 and at most 32 entries. Each entry
    /// names its own finalized snapshot; absence is typed, never filled from current policy.
    ///
    /// # Errors
    /// `InvalidEncoding` for a limit outside 1..=32, `AccessRefused` for an unknown market.
    pub fn epochs(&self, market: &MarketId, from_epoch: u64, limit: u8) -> ViewResult<EpochPage> {
        if limit == 0 || usize::from(limit) > PAGE_MAX_ROWS {
            return Err(ViewError::InvalidEncoding);
        }
        let conn = self.lock()?;
        let (latest, _, binding) = Self::resolve(&conn, market, None)?;
        if latest.availability[0] == Availability::UnsupportedVersion {
            return Ok(EpochPage {
                component: Availability::UnsupportedVersion,
                entries: Vec::new(),
            });
        }
        let Presence::Present(highest) = binding.epoch else {
            return Ok(EpochPage {
                component: Availability::NotYetProduced,
                entries: Vec::new(),
            });
        };
        let lowest = highest.saturating_sub(EPOCH_HEADERS_MAX - 1);
        let mut entries = Vec::new();
        for epoch in from_epoch..from_epoch.saturating_add(u64::from(limit)) {
            let entry = if epoch > highest {
                EpochEntry {
                    epoch,
                    status: EpochStatus::NeverOpened,
                    snapshot_id: None,
                }
            } else {
                let found: Option<(Vec<u8>, bool)> = conn
                    .query_row(
                        "SELECT snapshot_id, state IS NOT NULL FROM ai_snapshot WHERE market = ?1 \
                         AND epoch = ?2 AND projection IN (3, 4) \
                         ORDER BY observed_sequence DESC LIMIT 1",
                        params![market.as_bytes().as_slice(), signed(epoch)?],
                        |row| Ok((row.get(0)?, row.get(1)?)),
                    )
                    .optional()?;
                match found {
                    Some((id, true)) if epoch >= lowest => EpochEntry {
                        epoch,
                        status: EpochStatus::Retained,
                        snapshot_id: Some(digest(&id)?),
                    },
                    Some((id, _)) => EpochEntry {
                        epoch,
                        status: EpochStatus::ArchiveRequired,
                        snapshot_id: Some(digest(&id)?),
                    },
                    None => EpochEntry {
                        epoch,
                        status: EpochStatus::ArchiveUnavailable,
                        snapshot_id: None,
                    },
                }
            };
            entries.push(entry);
        }
        Ok(EpochPage {
            component: Availability::Available,
            entries,
        })
    }
}

/// Re-binds saved content to its saved finality evidence and checks the committed bytes.
fn rebind(record: &SnapshotRecord, state: &[u8]) -> ViewResult<SnapshotBinding> {
    let checkpoint = record.checkpoint.ok_or(ViewError::FinalityUnavailable)?;
    let finality = FinalityEvidence {
        native_state_root: record.native_state_root,
        checkpoint,
        settlement: Presence::Absent,
        rank: record.rank,
    };
    let binding = queries::bind_snapshot(
        state,
        &record.facts(),
        &finality,
        record
            .publication_time_ms
            .ok_or(ViewError::FinalityUnavailable)?,
    )?;
    let mut encoded = [0u8; BINDING_MAX_BYTES];
    let n = binding.encode(&mut encoded)?;
    if record.binding.as_deref() != encoded.get(..n) {
        return Err(ViewError::IntegrityFailure);
    }
    Ok(binding)
}
