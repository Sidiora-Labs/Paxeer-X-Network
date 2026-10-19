//! Signed operational health observations for AI market workers.
//!
//! A `HealthObservation` is off-chain data signed by an admitted observer. It
//! never carries a native state root, never raises eligibility and never feeds
//! the canonical snapshot tuple: it lives in its own table, is published only
//! as an operational label beside the finalized canonical evidence, and its
//! per-worker latency and success aggregates stay withheld below twenty
//! samples. Readiness for routing, signing and runner release additionally
//! requires finalized membership whose authority lags the independently
//! verified head by at most eight heights.

use std::collections::BTreeMap;
use std::fmt;
use std::path::Path;

use rusqlite::{params, Connection, OptionalExtension as _, TransactionBehavior};
use rustls::SignatureScheme;
use serde_json::{json, Value};
use sha2::{Digest as _, Sha256};

use crate::codec::hex;

/// Schema version of the canonical observation body.
pub const SCHEMA_VERSION: u16 = 1;
/// Hash domain of the observation signing digest.
pub const OBSERVATION_DOMAIN: &str = "PAXAI/health-observation/v1";
/// Readiness lifetime after the observation time, matching F02.
pub const HEALTH_TTL_MS: u64 = 30_000;
/// Smallest sample count whose per-worker aggregates are published.
pub const MINIMUM_PUBLIC_SAMPLES: u32 = 20;
/// Largest finalized-authority lag behind the verified head, in heights.
pub const AUTHORITY_LAG_LIMIT_HEIGHTS: u64 = 8;
/// Largest latency a sample or percentile may state, matching the F02 capability bound.
pub const MAX_LATENCY_MS: u32 = 3_600_000;
/// Largest raw sample window an observer summarizes at once.
pub const MAX_WINDOW_SAMPLES: usize = 4096;
/// Largest untrusted display name accepted before escaping.
pub const MAX_DISPLAY_NAME_BYTES: usize = 128;
/// Largest number of stored observer rows per market.
pub const MAX_ROWS_PER_MARKET: u32 = 128;
/// Largest number of metric series kept by one projection.
pub const MAX_METRIC_SERIES: usize = 256;
/// Largest number of undelivered operator alerts kept by one projection.
pub const MAX_PENDING_ALERTS: usize = 64;
/// Inclusive upper bounds, in milliseconds, of the latency histogram buckets.
pub const LATENCY_BUCKETS_MS: [u32; 12] = [
    10, 25, 50, 100, 250, 500, 1_000, 2_500, 5_000, 10_000, 30_000, 60_000,
];
/// Largest signed observation: body with both percentiles plus the signature.
pub const MAX_SIGNED_BYTES: usize = 208 + SIGNATURE_BYTES;

const SIGNATURE_BYTES: usize = 64;
const MIN_SIGNED_BYTES: usize = 200 + SIGNATURE_BYTES;

const SCHEMA: &str = "
CREATE TABLE IF NOT EXISTS ai_health_observations(
    market_id BLOB NOT NULL CHECK(length(market_id) = 32),
    worker_id BLOB NOT NULL CHECK(length(worker_id) = 32),
    observer_principal BLOB NOT NULL CHECK(length(observer_principal) = 32),
    observer_sequence INTEGER NOT NULL CHECK(observer_sequence > 0),
    observed_at_ms INTEGER NOT NULL CHECK(observed_at_ms >= 0),
    signed BLOB NOT NULL,
    PRIMARY KEY(market_id, worker_id, observer_principal)
) WITHOUT ROWID;
";

/// Every refusal of the health projection. Codes carry no request content.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum HealthError {
    /// Malformed bytes, zero identifiers, unknown enums or trailing bytes.
    NonCanonical,
    /// The schema version is not 1.
    UnsupportedVersion,
    /// More successes than samples.
    SuccessExceedsSamples,
    /// Latency presence, order or range disagrees with the sample count.
    InconsistentLatency,
    /// Expiry is not after the observation or exceeds the readiness lifetime.
    InvalidExpiry,
    /// Checked arithmetic or a width conversion overflowed.
    Overflow,
    /// The observation names another market, worker or observer than admitted.
    ObserverMismatch,
    /// The Ed25519 signature does not verify under the admitted key.
    BadSignature,
    /// A clock or observation time runs backwards.
    TimestampReversal,
    /// The observation reached its expiry.
    Expired,
    /// The observer sequence is older than the stored one.
    SequenceConsumed,
    /// The same observer sequence arrived with different bytes.
    ReplayConflict,
    /// A configured bound is exhausted.
    CapacityExceeded,
    /// No finalized canonical membership evidence is available.
    FinalityUnavailable,
    /// The canonical evidence names another market or worker.
    BindingMismatch,
    /// Finalized authority lags the verified head beyond the limit.
    StaleAuthority,
    /// The verified head is below the finalized snapshot height.
    AuthorityRegression,
    /// The worker is not listed in the finalized membership.
    NotMember,
    /// The observed readiness is below runner readiness.
    NotReady,
    /// The caller holds no current diagnostics authorization.
    AccessDenied,
    /// An untrusted display name exceeds its byte bound.
    DisplayNameTooLong,
    /// The durable projection store failed.
    Store(String),
}

impl HealthError {
    /// Stable refusal code; the only error text alerts and public bodies carry.
    #[must_use]
    pub fn code(&self) -> &'static str {
        match self {
            Self::NonCanonical => "non_canonical",
            Self::UnsupportedVersion => "unsupported_version",
            Self::SuccessExceedsSamples => "success_exceeds_samples",
            Self::InconsistentLatency => "inconsistent_latency",
            Self::InvalidExpiry => "invalid_expiry",
            Self::Overflow => "overflow",
            Self::ObserverMismatch => "observer_mismatch",
            Self::BadSignature => "bad_signature",
            Self::TimestampReversal => "timestamp_reversal",
            Self::Expired => "expired",
            Self::SequenceConsumed => "sequence_consumed",
            Self::ReplayConflict => "replay_conflict",
            Self::CapacityExceeded => "capacity_exceeded",
            Self::FinalityUnavailable => "finality_unavailable",
            Self::BindingMismatch => "binding_mismatch",
            Self::StaleAuthority => "stale_authority",
            Self::AuthorityRegression => "authority_regression",
            Self::NotMember => "not_member",
            Self::NotReady => "not_ready",
            Self::AccessDenied => "access_denied",
            Self::DisplayNameTooLong => "display_name_too_long",
            Self::Store(_) => "store_unavailable",
        }
    }
}

impl fmt::Display for HealthError {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::Store(detail) => write!(formatter, "store_unavailable: {detail}"),
            other => formatter.write_str(other.code()),
        }
    }
}

impl std::error::Error for HealthError {}

fn store_error(error: &rusqlite::Error) -> HealthError {
    HealthError::Store(error.to_string())
}

/// Outcome of one observer check.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
#[repr(u8)]
pub enum CheckStatus {
    /// The check ran and passed.
    Passed = 1,
    /// The check ran and failed.
    Failed = 2,
    /// The check did not run.
    NotChecked = 3,
}

impl CheckStatus {
    fn from_code(code: u8) -> Result<Self, HealthError> {
        match code {
            1 => Ok(Self::Passed),
            2 => Ok(Self::Failed),
            3 => Ok(Self::NotChecked),
            _ => Err(HealthError::NonCanonical),
        }
    }

    fn label(self) -> &'static str {
        match self {
            Self::Passed => "passed",
            Self::Failed => "failed",
            Self::NotChecked => "not-checked",
        }
    }
}

/// Allowlisted error category; the only failure detail an observation carries.
#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord)]
#[repr(u8)]
pub enum ErrorCategory {
    /// No failure observed.
    None = 0,
    /// The market is paused.
    MarketPaused = 1,
    /// The worker's finalized authority view is stale.
    StaleAuthority = 2,
    /// The worker's durable store is unavailable.
    StoreUnavailable = 3,
    /// The served certificate does not match the manifest pin.
    TlsPinMismatch = 4,
    /// The pinned model is not loaded.
    ModelUnavailable = 5,
    /// The job queue is saturated.
    QueueSaturated = 6,
    /// Requests exceeded their deadline.
    Timeout = 7,
    /// The runner failed requests.
    RunnerFailure = 8,
}

impl ErrorCategory {
    fn from_code(code: u8) -> Result<Self, HealthError> {
        match code {
            0 => Ok(Self::None),
            1 => Ok(Self::MarketPaused),
            2 => Ok(Self::StaleAuthority),
            3 => Ok(Self::StoreUnavailable),
            4 => Ok(Self::TlsPinMismatch),
            5 => Ok(Self::ModelUnavailable),
            6 => Ok(Self::QueueSaturated),
            7 => Ok(Self::Timeout),
            8 => Ok(Self::RunnerFailure),
            _ => Err(HealthError::NonCanonical),
        }
    }

    /// Label used in public views and metric series.
    #[must_use]
    pub fn label(self) -> &'static str {
        match self {
            Self::None => "none",
            Self::MarketPaused => "market_paused",
            Self::StaleAuthority => "stale_authority",
            Self::StoreUnavailable => "store_unavailable",
            Self::TlsPinMismatch => "tls_pin_mismatch",
            Self::ModelUnavailable => "model_unavailable",
            Self::QueueSaturated => "queue_saturated",
            Self::Timeout => "timeout",
            Self::RunnerFailure => "runner_failure",
        }
    }

    fn forces_not_ready(self) -> bool {
        matches!(
            self,
            Self::MarketPaused
                | Self::StaleAuthority
                | Self::StoreUnavailable
                | Self::TlsPinMismatch
                | Self::ModelUnavailable
                | Self::QueueSaturated
        )
    }
}

/// F02 service readiness implied by an observation.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum ReadinessLevel {
    /// Valid signed manifest only.
    Advertised,
    /// Authenticated transport and API version checked.
    VerifiedTransport,
    /// Runner loaded with pinned artifacts and qualified on a bounded workload.
    RunnerReady,
    /// No readiness, or a failure that forces it off.
    NotReady,
}

impl ReadinessLevel {
    fn label(self) -> &'static str {
        match self {
            Self::Advertised => "advertised",
            Self::VerifiedTransport => "verified-transport",
            Self::RunnerReady => "runner-ready",
            Self::NotReady => "not-ready",
        }
    }
}

/// One signed health observation.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct HealthObservation {
    /// Observed market.
    pub market_id: [u8; 32],
    /// Observed worker.
    pub worker_id: [u8; 32],
    /// Model the worker serves.
    pub model_digest: [u8; 32],
    /// Deployment the worker runs.
    pub deployment_digest: [u8; 32],
    /// Principal of the observer that signs.
    pub observer_principal: [u8; 32],
    /// Strictly increasing observer sequence, starting at 1.
    pub observer_sequence: u64,
    /// Observation wall-clock time.
    pub observed_at_ms: u64,
    /// Exclusive end of readiness, at most `HEALTH_TTL_MS` after the observation.
    pub expires_at_ms: u64,
    /// Signed manifest check.
    pub advertised: CheckStatus,
    /// Transport and API version check.
    pub transport: CheckStatus,
    /// Runner qualification check.
    pub runner: CheckStatus,
    /// Number of request samples behind the figures.
    pub sample_count: u32,
    /// Number of successful samples.
    pub success_count: u32,
    /// Nearest-rank 50th percentile latency, absent with zero samples.
    pub latency_p50_ms: Option<u32>,
    /// Nearest-rank 95th percentile latency, absent with zero samples.
    pub latency_p95_ms: Option<u32>,
    /// Allowlisted failure category.
    pub error_category: ErrorCategory,
    /// Ed25519 signature over `signing_digest`.
    pub signature: [u8; 64],
}

/// SHA-256 of the observation domain, a zero byte and the canonical body.
#[must_use]
pub fn observation_digest(body: &[u8]) -> [u8; 32] {
    let mut hasher = Sha256::new();
    hasher.update(OBSERVATION_DOMAIN.as_bytes());
    hasher.update([0]);
    hasher.update(body);
    hasher.finalize().into()
}

struct Reader<'a> {
    bytes: &'a [u8],
}

impl Reader<'_> {
    fn take<const N: usize>(&mut self) -> Result<[u8; N], HealthError> {
        let (head, rest) = self
            .bytes
            .split_first_chunk::<N>()
            .ok_or(HealthError::NonCanonical)?;
        self.bytes = rest;
        Ok(*head)
    }

    fn u8(&mut self) -> Result<u8, HealthError> {
        self.take().map(u8::from_be_bytes)
    }

    fn u16(&mut self) -> Result<u16, HealthError> {
        self.take().map(u16::from_be_bytes)
    }

    fn u32(&mut self) -> Result<u32, HealthError> {
        self.take().map(u32::from_be_bytes)
    }

    fn u64(&mut self) -> Result<u64, HealthError> {
        self.take().map(u64::from_be_bytes)
    }

    fn optional_u32(&mut self) -> Result<Option<u32>, HealthError> {
        match self.u8()? {
            0 => Ok(None),
            1 => self.u32().map(Some),
            _ => Err(HealthError::NonCanonical),
        }
    }

    fn finish(&self) -> Result<(), HealthError> {
        if self.bytes.is_empty() {
            Ok(())
        } else {
            Err(HealthError::NonCanonical)
        }
    }
}

fn push_optional(out: &mut Vec<u8>, value: Option<u32>) {
    match value {
        None => out.push(0),
        Some(value) => {
            out.push(1);
            out.extend_from_slice(&value.to_be_bytes());
        }
    }
}

impl HealthObservation {
    /// Decodes and structurally validates signed bytes, refusing any length
    /// outside the canonical bounds before reading a field.
    ///
    /// # Errors
    /// `NonCanonical`, `UnsupportedVersion` or any `validate` refusal.
    pub fn decode(signed: &[u8]) -> Result<Self, HealthError> {
        if !(MIN_SIGNED_BYTES..=MAX_SIGNED_BYTES).contains(&signed.len()) {
            return Err(HealthError::NonCanonical);
        }
        let (body, signature) = signed.split_at(signed.len() - SIGNATURE_BYTES);
        let mut reader = Reader { bytes: body };
        if reader.u16()? != SCHEMA_VERSION {
            return Err(HealthError::UnsupportedVersion);
        }
        let observation = Self {
            market_id: reader.take()?,
            worker_id: reader.take()?,
            model_digest: reader.take()?,
            deployment_digest: reader.take()?,
            observer_principal: reader.take()?,
            observer_sequence: reader.u64()?,
            observed_at_ms: reader.u64()?,
            expires_at_ms: reader.u64()?,
            advertised: CheckStatus::from_code(reader.u8()?)?,
            transport: CheckStatus::from_code(reader.u8()?)?,
            runner: CheckStatus::from_code(reader.u8()?)?,
            sample_count: reader.u32()?,
            success_count: reader.u32()?,
            latency_p50_ms: reader.optional_u32()?,
            latency_p95_ms: reader.optional_u32()?,
            error_category: ErrorCategory::from_code(reader.u8()?)?,
            signature: signature
                .try_into()
                .map_err(|_| HealthError::NonCanonical)?,
        };
        reader.finish()?;
        observation.validate()?;
        Ok(observation)
    }

    /// Checks every field bound and cross-field rule.
    ///
    /// # Errors
    /// `NonCanonical` for zero identifiers, a zero sequence or a status chain
    /// that skips a level; `Overflow`, `InvalidExpiry`,
    /// `SuccessExceedsSamples` or `InconsistentLatency` otherwise.
    pub fn validate(&self) -> Result<(), HealthError> {
        let identifiers = [
            &self.market_id,
            &self.worker_id,
            &self.model_digest,
            &self.deployment_digest,
            &self.observer_principal,
        ];
        if identifiers.iter().any(|id| **id == [0; 32]) || self.observer_sequence == 0 {
            return Err(HealthError::NonCanonical);
        }
        let ceiling = self
            .observed_at_ms
            .checked_add(HEALTH_TTL_MS)
            .ok_or(HealthError::Overflow)?;
        if self.expires_at_ms <= self.observed_at_ms || self.expires_at_ms > ceiling {
            return Err(HealthError::InvalidExpiry);
        }
        if self.success_count > self.sample_count {
            return Err(HealthError::SuccessExceedsSamples);
        }
        match (self.sample_count, self.latency_p50_ms, self.latency_p95_ms) {
            (0, None, None) => {}
            (count, Some(p50), Some(p95)) if count > 0 && p50 <= p95 && p95 <= MAX_LATENCY_MS => {}
            _ => return Err(HealthError::InconsistentLatency),
        }
        let transport_chained =
            self.transport != CheckStatus::Passed || self.advertised == CheckStatus::Passed;
        let runner_chained =
            self.runner != CheckStatus::Passed || self.transport == CheckStatus::Passed;
        if transport_chained && runner_chained {
            Ok(())
        } else {
            Err(HealthError::NonCanonical)
        }
    }

    /// Canonical body: every field in order, big-endian, optional values behind
    /// a presence byte, without the signature.
    #[must_use]
    pub fn body(&self) -> Vec<u8> {
        let mut out = Vec::with_capacity(MAX_SIGNED_BYTES - SIGNATURE_BYTES);
        out.extend_from_slice(&SCHEMA_VERSION.to_be_bytes());
        for id in [
            &self.market_id,
            &self.worker_id,
            &self.model_digest,
            &self.deployment_digest,
            &self.observer_principal,
        ] {
            out.extend_from_slice(id);
        }
        for value in [
            self.observer_sequence,
            self.observed_at_ms,
            self.expires_at_ms,
        ] {
            out.extend_from_slice(&value.to_be_bytes());
        }
        out.extend_from_slice(&[
            self.advertised as u8,
            self.transport as u8,
            self.runner as u8,
        ]);
        out.extend_from_slice(&self.sample_count.to_be_bytes());
        out.extend_from_slice(&self.success_count.to_be_bytes());
        push_optional(&mut out, self.latency_p50_ms);
        push_optional(&mut out, self.latency_p95_ms);
        out.push(self.error_category as u8);
        out
    }

    /// The 32 bytes the observer signs with Ed25519.
    #[must_use]
    pub fn signing_digest(&self) -> [u8; 32] {
        observation_digest(&self.body())
    }

    /// Body followed by the signature.
    #[must_use]
    pub fn encode(&self) -> Vec<u8> {
        let mut out = self.body();
        out.extend_from_slice(&self.signature);
        out
    }

    /// Readiness the observation states; a forcing failure category wins.
    #[must_use]
    pub fn readiness(&self) -> ReadinessLevel {
        if self.error_category.forces_not_ready() {
            return ReadinessLevel::NotReady;
        }
        match (self.advertised, self.transport, self.runner) {
            (CheckStatus::Passed, CheckStatus::Passed, CheckStatus::Passed) => {
                ReadinessLevel::RunnerReady
            }
            (CheckStatus::Passed, CheckStatus::Passed, _) => ReadinessLevel::VerifiedTransport,
            (CheckStatus::Passed, _, _) => ReadinessLevel::Advertised,
            _ => ReadinessLevel::NotReady,
        }
    }

    fn check_live(&self, now_ms: u64) -> Result<(), HealthError> {
        if now_ms < self.observed_at_ms {
            Err(HealthError::TimestampReversal)
        } else if now_ms >= self.expires_at_ms {
            Err(HealthError::Expired)
        } else {
            Ok(())
        }
    }
}

/// An observer admitted by finalized authority for one market and worker.
///
/// The caller resolves `public_key` from the finalized roster, for a
/// worker-produced observation the frozen worker delegate key of the F02
/// transport identity.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct AdmittedObserver {
    /// Market the observer may report on.
    pub market_id: [u8; 32],
    /// Worker the observer may report on.
    pub worker_id: [u8; 32],
    /// Observer principal the observation must name.
    pub principal: [u8; 32],
    /// Ed25519 public key that must sign.
    pub public_key: [u8; 32],
}

fn verify_ed25519(
    public_key: &[u8; 32],
    digest: &[u8; 32],
    signature: &[u8; 64],
) -> Result<(), HealthError> {
    let algorithms = rustls::crypto::ring::default_provider().signature_verification_algorithms;
    let algorithm = algorithms
        .mapping
        .iter()
        .find(|(scheme, _)| *scheme == SignatureScheme::ED25519)
        .and_then(|(_, candidates)| candidates.first())
        .ok_or(HealthError::BadSignature)?;
    algorithm
        .verify_signature(public_key, digest, signature)
        .map_err(|_| HealthError::BadSignature)
}

/// An observation whose structure, observer, signature and liveness were
/// checked at admission.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct VerifiedObservation {
    observation: HealthObservation,
    signed: Vec<u8>,
}

impl VerifiedObservation {
    /// Verifies signed bytes from `observer` at `now_ms`: bounds and structure
    /// first, then binding, then the signature, then liveness.
    ///
    /// # Errors
    /// Any decode refusal, `ObserverMismatch`, `BadSignature`,
    /// `TimestampReversal` or `Expired`.
    pub fn verify(
        signed: &[u8],
        observer: &AdmittedObserver,
        now_ms: u64,
    ) -> Result<Self, HealthError> {
        let observation = HealthObservation::decode(signed)?;
        if observation.market_id != observer.market_id
            || observation.worker_id != observer.worker_id
            || observation.observer_principal != observer.principal
        {
            return Err(HealthError::ObserverMismatch);
        }
        verify_ed25519(
            &observer.public_key,
            &observation.signing_digest(),
            &observation.signature,
        )?;
        observation.check_live(now_ms)?;
        Ok(Self {
            observation,
            signed: signed.to_vec(),
        })
    }

    fn from_projection(signed: Vec<u8>) -> Result<Self, HealthError> {
        Ok(Self {
            observation: HealthObservation::decode(&signed)?,
            signed,
        })
    }

    /// The verified observation.
    #[must_use]
    pub fn observation(&self) -> &HealthObservation {
        &self.observation
    }

    /// The exact signed bytes as admitted.
    #[must_use]
    pub fn signed_bytes(&self) -> &[u8] {
        &self.signed
    }
}

/// One raw request sample as the observer measured it.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct Sample {
    /// Request latency in milliseconds.
    pub latency_ms: u32,
    /// Whether the request succeeded.
    pub succeeded: bool,
}

/// Integer summary of a sample window.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct SampleSummary {
    /// Number of samples.
    pub sample_count: u32,
    /// Number of successful samples.
    pub success_count: u32,
    /// Nearest-rank 50th percentile, absent with zero samples.
    pub latency_p50_ms: Option<u32>,
    /// Nearest-rank 95th percentile, absent with zero samples.
    pub latency_p95_ms: Option<u32>,
}

fn nearest_rank(sorted: &[u32], percentile: u64) -> Result<Option<u32>, HealthError> {
    if sorted.is_empty() {
        return Ok(None);
    }
    let count = u64::try_from(sorted.len()).map_err(|_| HealthError::Overflow)?;
    let rank = percentile
        .checked_mul(count)
        .and_then(|product| product.checked_add(99))
        .ok_or(HealthError::Overflow)?
        / 100;
    let index = rank
        .checked_sub(1)
        .and_then(|index| usize::try_from(index).ok())
        .ok_or(HealthError::Overflow)?;
    sorted
        .get(index)
        .copied()
        .map(Some)
        .ok_or(HealthError::Overflow)
}

/// Summarizes a bounded window: counts plus nearest-rank p50 and p95, where
/// the p-th percentile of n sorted latencies is `sorted[ceil(p*n/100)-1]`.
///
/// # Errors
/// `CapacityExceeded` above `MAX_WINDOW_SAMPLES`, `InconsistentLatency` for a
/// sample above `MAX_LATENCY_MS`, `Overflow` on checked arithmetic.
pub fn summarize(samples: &[Sample]) -> Result<SampleSummary, HealthError> {
    if samples.len() > MAX_WINDOW_SAMPLES {
        return Err(HealthError::CapacityExceeded);
    }
    let sample_count = u32::try_from(samples.len()).map_err(|_| HealthError::Overflow)?;
    let mut success_count: u32 = 0;
    let mut latencies = Vec::with_capacity(samples.len());
    for sample in samples {
        if sample.latency_ms > MAX_LATENCY_MS {
            return Err(HealthError::InconsistentLatency);
        }
        if sample.succeeded {
            success_count = success_count.checked_add(1).ok_or(HealthError::Overflow)?;
        }
        latencies.push(sample.latency_ms);
    }
    latencies.sort_unstable();
    Ok(SampleSummary {
        sample_count,
        success_count,
        latency_p50_ms: nearest_rank(&latencies, 50)?,
        latency_p95_ms: nearest_rank(&latencies, 95)?,
    })
}

/// `floor(1000000 * success_count / sample_count)` in checked u128; no metric
/// for zero samples.
///
/// # Errors
/// `SuccessExceedsSamples` or `Overflow`.
pub fn success_ppm(success_count: u32, sample_count: u32) -> Result<Option<u32>, HealthError> {
    if success_count > sample_count {
        return Err(HealthError::SuccessExceedsSamples);
    }
    if sample_count == 0 {
        return Ok(None);
    }
    let ppm = 1_000_000_u128
        .checked_mul(u128::from(success_count))
        .ok_or(HealthError::Overflow)?
        / u128::from(sample_count);
    u32::try_from(ppm)
        .map(Some)
        .map_err(|_| HealthError::Overflow)
}

/// Per-worker aggregates as the public may see them.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum PublicMetrics {
    /// At least `MINIMUM_PUBLIC_SAMPLES` samples back the figures.
    Published {
        /// Number of samples.
        sample_count: u32,
        /// Success ratio in parts per million.
        success_ppm: u32,
        /// Nearest-rank 50th percentile latency.
        latency_p50_ms: u32,
        /// Nearest-rank 95th percentile latency.
        latency_p95_ms: u32,
    },
    /// Fewer samples: every figure is withheld.
    InsufficientSamples,
}

/// Public aggregates of a validated observation.
///
/// # Errors
/// `SuccessExceedsSamples`, `InconsistentLatency` or `Overflow` for an
/// observation that never passed `validate`.
pub fn public_metrics(observation: &HealthObservation) -> Result<PublicMetrics, HealthError> {
    if observation.sample_count < MINIMUM_PUBLIC_SAMPLES {
        return Ok(PublicMetrics::InsufficientSamples);
    }
    match (
        success_ppm(observation.success_count, observation.sample_count)?,
        observation.latency_p50_ms,
        observation.latency_p95_ms,
    ) {
        (Some(success_ppm), Some(latency_p50_ms), Some(latency_p95_ms)) => {
            Ok(PublicMetrics::Published {
                sample_count: observation.sample_count,
                success_ppm,
                latency_p50_ms,
                latency_p95_ms,
            })
        }
        _ => Err(HealthError::InconsistentLatency),
    }
}

/// Finalized canonical membership of one worker, taken from the verified
/// finalized market snapshot.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CanonicalMembership {
    /// Finalized snapshot the membership comes from.
    pub snapshot_id: [u8; 32],
    /// Market of the snapshot.
    pub market_id: [u8; 32],
    /// Worker the row describes.
    pub worker_id: [u8; 32],
    /// Execution height of the finalized snapshot.
    pub finalized_height: u64,
    /// Whether the worker is a current listed member.
    pub worker_listed: bool,
}

fn authority_lag(
    observation: &HealthObservation,
    canonical: &CanonicalMembership,
    verified_head_height: u64,
) -> Result<u64, HealthError> {
    if canonical.market_id != observation.market_id || canonical.worker_id != observation.worker_id
    {
        return Err(HealthError::BindingMismatch);
    }
    verified_head_height
        .checked_sub(canonical.finalized_height)
        .ok_or(HealthError::AuthorityRegression)
}

/// Gate for routing, signing and runner release: finalized membership, fresh
/// authority, a live observation and runner readiness, checked in that order.
///
/// # Errors
/// `FinalityUnavailable`, `BindingMismatch`, `AuthorityRegression`,
/// `StaleAuthority`, `NotMember`, `TimestampReversal`, `Expired` or `NotReady`.
pub fn routing_readiness(
    observation: &VerifiedObservation,
    canonical: Option<&CanonicalMembership>,
    verified_head_height: u64,
    now_ms: u64,
) -> Result<(), HealthError> {
    let canonical = canonical.ok_or(HealthError::FinalityUnavailable)?;
    let observation = observation.observation();
    if authority_lag(observation, canonical, verified_head_height)? > AUTHORITY_LAG_LIMIT_HEIGHTS {
        return Err(HealthError::StaleAuthority);
    }
    if !canonical.worker_listed {
        return Err(HealthError::NotMember);
    }
    observation.check_live(now_ms)?;
    if observation.readiness() == ReadinessLevel::RunnerReady {
        Ok(())
    } else {
        Err(HealthError::NotReady)
    }
}

/// Escapes untrusted display text so it renders as plain text, never markup.
///
/// # Errors
/// `DisplayNameTooLong` above `MAX_DISPLAY_NAME_BYTES`, checked before allocating.
pub fn escape_display_text(untrusted: &str) -> Result<String, HealthError> {
    if untrusted.len() > MAX_DISPLAY_NAME_BYTES {
        return Err(HealthError::DisplayNameTooLong);
    }
    let mut out = String::with_capacity(untrusted.len() * 6);
    for character in untrusted.chars() {
        match character {
            '&' => out.push_str("&amp;"),
            '<' => out.push_str("&lt;"),
            '>' => out.push_str("&gt;"),
            '"' => out.push_str("&quot;"),
            '\'' => out.push_str("&#39;"),
            control if control.is_control() => out.push('\u{fffd}'),
            other => out.push(other),
        }
    }
    Ok(out)
}

fn canonical_json(
    observation: &HealthObservation,
    canonical: Option<&CanonicalMembership>,
    verified_head_height: u64,
) -> Result<Value, HealthError> {
    let Some(canonical) = canonical else {
        return Ok(json!({
            "status": "unavailable",
            "snapshot_id": null,
            "finalized_height": null,
            "worker_listed": null,
            "authority": null,
        }));
    };
    let lag = authority_lag(observation, canonical, verified_head_height)?;
    let authority = if lag > AUTHORITY_LAG_LIMIT_HEIGHTS {
        "stale"
    } else {
        "fresh"
    };
    Ok(json!({
        "status": "finalized",
        "snapshot_id": hex(&canonical.snapshot_id),
        "finalized_height": canonical.finalized_height.to_string(),
        "worker_listed": canonical.worker_listed,
        "authority": {
            "status": authority,
            "lag_heights": lag.to_string(),
            "limit_heights": AUTHORITY_LAG_LIMIT_HEIGHTS.to_string(),
        },
    }))
}

/// Public view: canonical finalized evidence and the operational observation
/// side by side, never merged. Aggregates below the sample minimum are null.
///
/// # Errors
/// `BindingMismatch`, `AuthorityRegression`, `TimestampReversal`,
/// `DisplayNameTooLong` or a `public_metrics` refusal.
pub fn public_view(
    observation: &VerifiedObservation,
    canonical: Option<&CanonicalMembership>,
    verified_head_height: u64,
    display_name: Option<&str>,
    now_ms: u64,
) -> Result<Value, HealthError> {
    let observation = observation.observation();
    let canonical = canonical_json(observation, canonical, verified_head_height)?;
    let display_name = display_name.map(escape_display_text).transpose()?;
    let age_ms = now_ms
        .checked_sub(observation.observed_at_ms)
        .ok_or(HealthError::TimestampReversal)?;
    let readiness = if now_ms >= observation.expires_at_ms {
        "expired"
    } else {
        observation.readiness().label()
    };
    let metrics = match public_metrics(observation)? {
        PublicMetrics::Published {
            sample_count,
            success_ppm,
            latency_p50_ms,
            latency_p95_ms,
        } => json!({
            "status": "published",
            "sample_count": sample_count,
            "success_ppm": success_ppm,
            "latency_p50_ms": latency_p50_ms,
            "latency_p95_ms": latency_p95_ms,
        }),
        PublicMetrics::InsufficientSamples => json!({
            "status": "insufficient-samples",
            "sample_count": null,
            "success_ppm": null,
            "latency_p50_ms": null,
            "latency_p95_ms": null,
        }),
    };
    Ok(json!({
        "schema_version": SCHEMA_VERSION,
        "market_id": hex(&observation.market_id),
        "worker_id": hex(&observation.worker_id),
        "model_digest": hex(&observation.model_digest),
        "deployment_digest": hex(&observation.deployment_digest),
        "display_name": display_name,
        "canonical": canonical,
        "operational": {
            "kind": "signed-health-observation",
            "authoritative": false,
            "observer_principal": hex(&observation.observer_principal),
            "observed_at_ms": observation.observed_at_ms.to_string(),
            "expires_at_ms": observation.expires_at_ms.to_string(),
            "age_ms": age_ms.to_string(),
            "readiness": readiness,
            "error_category": observation.error_category.label(),
            "minimum_samples": MINIMUM_PUBLIC_SAMPLES,
            "metrics": metrics,
        },
    }))
}

/// A diagnostics grant held by an owner or operator for one market.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct DiagnosticsGrant {
    /// Market the grant covers.
    pub market_id: [u8; 32],
    /// Authorization generation the grant was issued under.
    pub generation: u64,
}

/// Private operational diagnostics, including figures below the public sample
/// minimum. `current_generation` is the grant generation the authority service
/// reports for the caller at access time; `None` means revoked.
///
/// # Errors
/// `AccessDenied` for another market, a revoked or superseded grant;
/// `Overflow` from the success ratio.
pub fn operator_diagnostics(
    observation: &VerifiedObservation,
    grant: &DiagnosticsGrant,
    current_generation: Option<u64>,
) -> Result<Value, HealthError> {
    let observation = observation.observation();
    if grant.market_id != observation.market_id || current_generation != Some(grant.generation) {
        return Err(HealthError::AccessDenied);
    }
    Ok(json!({
        "market_id": hex(&observation.market_id),
        "worker_id": hex(&observation.worker_id),
        "model_digest": hex(&observation.model_digest),
        "deployment_digest": hex(&observation.deployment_digest),
        "observer_principal": hex(&observation.observer_principal),
        "observer_sequence": observation.observer_sequence.to_string(),
        "observed_at_ms": observation.observed_at_ms.to_string(),
        "expires_at_ms": observation.expires_at_ms.to_string(),
        "advertised": observation.advertised.label(),
        "transport": observation.transport.label(),
        "runner": observation.runner.label(),
        "readiness": observation.readiness().label(),
        "sample_count": observation.sample_count,
        "success_count": observation.success_count,
        "success_ppm": success_ppm(observation.success_count, observation.sample_count)?,
        "latency_p50_ms": observation.latency_p50_ms,
        "latency_p95_ms": observation.latency_p95_ms,
        "error_category": observation.error_category.label(),
    }))
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord)]
struct SeriesKey {
    market_id: [u8; 32],
    worker_id: [u8; 32],
    model_digest: [u8; 32],
    error_category: ErrorCategory,
}

impl SeriesKey {
    fn labels(&self) -> String {
        format!(
            "market=\"{}\",worker=\"{}\",model=\"{}\",error_category=\"{}\"",
            hex(&self.market_id),
            hex(&self.worker_id),
            hex(&self.model_digest),
            self.error_category.label()
        )
    }
}

#[derive(Debug, Default)]
struct Series {
    success_ppm: u32,
    buckets: [u64; LATENCY_BUCKETS_MS.len()],
    count: u64,
    sum_ms: u64,
}

/// Metric registry with allowlisted labels (market, worker, model, error
/// category) and fixed integer latency buckets. Only observations meeting the
/// public sample minimum enter a per-worker series; the rest count per market.
#[derive(Debug, Default)]
pub struct HealthMetrics {
    series: BTreeMap<SeriesKey, Series>,
    withheld: BTreeMap<[u8; 32], u64>,
}

impl HealthMetrics {
    fn record(&mut self, observation: &HealthObservation) -> Result<(), HealthError> {
        match public_metrics(observation)? {
            PublicMetrics::InsufficientSamples => {
                if !self.withheld.contains_key(&observation.market_id)
                    && self.withheld.len() >= MAX_METRIC_SERIES
                {
                    return Err(HealthError::CapacityExceeded);
                }
                let withheld = self.withheld.entry(observation.market_id).or_insert(0);
                *withheld = withheld.checked_add(1).ok_or(HealthError::Overflow)?;
            }
            PublicMetrics::Published {
                success_ppm,
                latency_p95_ms,
                ..
            } => {
                let key = SeriesKey {
                    market_id: observation.market_id,
                    worker_id: observation.worker_id,
                    model_digest: observation.model_digest,
                    error_category: observation.error_category,
                };
                if !self.series.contains_key(&key) && self.series.len() >= MAX_METRIC_SERIES {
                    return Err(HealthError::CapacityExceeded);
                }
                let series = self.series.entry(key).or_default();
                let count = series.count.checked_add(1).ok_or(HealthError::Overflow)?;
                let sum_ms = series
                    .sum_ms
                    .checked_add(u64::from(latency_p95_ms))
                    .ok_or(HealthError::Overflow)?;
                for (bucket, bound) in series.buckets.iter_mut().zip(LATENCY_BUCKETS_MS) {
                    if latency_p95_ms <= bound {
                        *bucket += 1;
                    }
                }
                series.count = count;
                series.sum_ms = sum_ms;
                series.success_ppm = success_ppm;
            }
        }
        Ok(())
    }
}

impl fmt::Display for HealthMetrics {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        writeln!(formatter, "# TYPE paxai_health_success_ppm gauge")?;
        for (key, series) in &self.series {
            writeln!(
                formatter,
                "paxai_health_success_ppm{{{}}} {}",
                key.labels(),
                series.success_ppm
            )?;
        }
        writeln!(formatter, "# TYPE paxai_health_latency_p95_ms histogram")?;
        for (key, series) in &self.series {
            let labels = key.labels();
            for (bound, count) in LATENCY_BUCKETS_MS.iter().zip(series.buckets) {
                writeln!(
                    formatter,
                    "paxai_health_latency_p95_ms_bucket{{{labels},le=\"{bound}\"}} {count}"
                )?;
            }
            writeln!(
                formatter,
                "paxai_health_latency_p95_ms_bucket{{{labels},le=\"+Inf\"}} {}",
                series.count
            )?;
            writeln!(
                formatter,
                "paxai_health_latency_p95_ms_sum{{{labels}}} {}",
                series.sum_ms
            )?;
            writeln!(
                formatter,
                "paxai_health_latency_p95_ms_count{{{labels}}} {}",
                series.count
            )?;
        }
        writeln!(
            formatter,
            "# TYPE paxai_health_withheld_observations_total counter"
        )?;
        for (market_id, count) in &self.withheld {
            writeln!(
                formatter,
                "paxai_health_withheld_observations_total{{market=\"{}\"}} {count}",
                hex(market_id)
            )?;
        }
        Ok(())
    }
}

/// A bounded operator alert: identifiers and a refusal code, nothing else.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct OperatorAlert {
    /// Market concerned.
    pub market_id: [u8; 32],
    /// Worker concerned.
    pub worker_id: [u8; 32],
    /// Refusal code.
    pub code: &'static str,
}

impl fmt::Display for OperatorAlert {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            formatter,
            "paxai_health_alert market={} worker={} category={}",
            hex(&self.market_id),
            hex(&self.worker_id),
            self.code
        )
    }
}

/// Result of a successful admission.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Admission {
    /// The observation became the observer's latest row.
    Stored,
    /// The identical observation was already stored.
    AlreadyApplied,
}

fn check_replay(
    existing: Option<(i64, i64, Vec<u8>)>,
    sequence: i64,
    observed_at_ms: i64,
    signed: &[u8],
) -> Result<Option<Admission>, HealthError> {
    match existing {
        Some((stored, _, bytes)) if stored == sequence => {
            if bytes == signed {
                Ok(Some(Admission::AlreadyApplied))
            } else {
                Err(HealthError::ReplayConflict)
            }
        }
        Some((stored, _, _)) if sequence < stored => Err(HealthError::SequenceConsumed),
        Some((_, stored_at, _)) if observed_at_ms < stored_at => {
            Err(HealthError::TimestampReversal)
        }
        _ => Ok(None),
    }
}

/// The operational health projection: its own durable table, metrics and
/// bounded alert queue, separate from every canonical projection table.
pub struct HealthProjection {
    connection: Connection,
    metrics: HealthMetrics,
    alerts: Vec<OperatorAlert>,
    suppressed_alerts: u64,
}

impl HealthProjection {
    /// Opens or creates the projection in a `SQLite` file.
    ///
    /// # Errors
    /// `Store` when the file or schema cannot be opened.
    pub fn open(path: &Path) -> Result<Self, HealthError> {
        Self::with_connection(Connection::open(path).map_err(|error| store_error(&error))?)
    }

    /// Opens a projection in memory.
    ///
    /// # Errors
    /// `Store` when the schema cannot be created.
    pub fn open_in_memory() -> Result<Self, HealthError> {
        Self::with_connection(Connection::open_in_memory().map_err(|error| store_error(&error))?)
    }

    fn with_connection(connection: Connection) -> Result<Self, HealthError> {
        connection
            .execute_batch(SCHEMA)
            .map_err(|error| store_error(&error))?;
        Ok(Self {
            connection,
            metrics: HealthMetrics::default(),
            alerts: Vec::new(),
            suppressed_alerts: 0,
        })
    }

    /// Verifies and stores one signed observation. The observer sequence must
    /// increase and its observation time must not run backwards; an identical
    /// retry is `AlreadyApplied`. Every refusal raises an operator alert.
    ///
    /// # Errors
    /// Any `VerifiedObservation::verify` refusal, `SequenceConsumed`,
    /// `ReplayConflict`, `TimestampReversal`, `CapacityExceeded`, `Overflow`
    /// or `Store`; a refusal stores nothing.
    pub fn admit(
        &mut self,
        signed: &[u8],
        observer: &AdmittedObserver,
        now_ms: u64,
    ) -> Result<Admission, HealthError> {
        match self.store(signed, observer, now_ms) {
            Ok((Admission::Stored, verified)) => {
                if let Err(error) = self.metrics.record(verified.observation()) {
                    self.raise(observer, &error);
                }
                Ok(Admission::Stored)
            }
            Ok((admission, _)) => Ok(admission),
            Err(error) => {
                self.raise(observer, &error);
                Err(error)
            }
        }
    }

    fn store(
        &mut self,
        signed: &[u8],
        observer: &AdmittedObserver,
        now_ms: u64,
    ) -> Result<(Admission, VerifiedObservation), HealthError> {
        let verified = VerifiedObservation::verify(signed, observer, now_ms)?;
        let observation = verified.observation();
        let sequence =
            i64::try_from(observation.observer_sequence).map_err(|_| HealthError::Overflow)?;
        let observed_at_ms =
            i64::try_from(observation.observed_at_ms).map_err(|_| HealthError::Overflow)?;
        let transaction = self
            .connection
            .transaction_with_behavior(TransactionBehavior::Immediate)
            .map_err(|error| store_error(&error))?;
        let existing = transaction
            .query_row(
                "SELECT observer_sequence, observed_at_ms, signed FROM ai_health_observations
                 WHERE market_id = ?1 AND worker_id = ?2 AND observer_principal = ?3",
                params![
                    &observation.market_id[..],
                    &observation.worker_id[..],
                    &observation.observer_principal[..]
                ],
                |row| Ok((row.get(0)?, row.get(1)?, row.get(2)?)),
            )
            .optional()
            .map_err(|error| store_error(&error))?;
        let is_new = existing.is_none();
        if let Some(admission) = check_replay(existing, sequence, observed_at_ms, signed)? {
            return Ok((admission, verified));
        }
        if is_new {
            let rows: i64 = transaction
                .query_row(
                    "SELECT COUNT(*) FROM ai_health_observations WHERE market_id = ?1",
                    params![&observation.market_id[..]],
                    |row| row.get(0),
                )
                .map_err(|error| store_error(&error))?;
            if rows >= i64::from(MAX_ROWS_PER_MARKET) {
                return Err(HealthError::CapacityExceeded);
            }
        }
        transaction
            .execute(
                "INSERT INTO ai_health_observations(
                     market_id, worker_id, observer_principal, observer_sequence, observed_at_ms, signed)
                 VALUES (?1, ?2, ?3, ?4, ?5, ?6)
                 ON CONFLICT(market_id, worker_id, observer_principal) DO UPDATE SET
                     observer_sequence = excluded.observer_sequence,
                     observed_at_ms = excluded.observed_at_ms,
                     signed = excluded.signed",
                params![
                    &observation.market_id[..],
                    &observation.worker_id[..],
                    &observation.observer_principal[..],
                    sequence,
                    observed_at_ms,
                    signed
                ],
            )
            .map_err(|error| store_error(&error))?;
        transaction.commit().map_err(|error| store_error(&error))?;
        Ok((Admission::Stored, verified))
    }

    /// Latest stored observation of a worker across its observers.
    ///
    /// # Errors
    /// `Store`, or a decode refusal if the stored bytes were altered.
    pub fn latest(
        &self,
        market_id: &[u8; 32],
        worker_id: &[u8; 32],
    ) -> Result<Option<VerifiedObservation>, HealthError> {
        let signed: Option<Vec<u8>> = self
            .connection
            .query_row(
                "SELECT signed FROM ai_health_observations
                 WHERE market_id = ?1 AND worker_id = ?2
                 ORDER BY observed_at_ms DESC, observer_principal ASC LIMIT 1",
                params![&market_id[..], &worker_id[..]],
                |row| row.get(0),
            )
            .optional()
            .map_err(|error| store_error(&error))?;
        signed.map(VerifiedObservation::from_projection).transpose()
    }

    /// Prometheus text exposition of the allowlisted metrics.
    #[must_use]
    pub fn metrics_text(&self) -> String {
        self.metrics.to_string()
    }

    /// Takes the pending operator alerts, oldest first.
    pub fn drain_alerts(&mut self) -> Vec<OperatorAlert> {
        std::mem::take(&mut self.alerts)
    }

    /// Number of alerts dropped because the queue was full.
    #[must_use]
    pub fn suppressed_alerts(&self) -> u64 {
        self.suppressed_alerts
    }

    fn raise(&mut self, observer: &AdmittedObserver, error: &HealthError) {
        if self.alerts.len() >= MAX_PENDING_ALERTS {
            self.suppressed_alerts = self.suppressed_alerts.saturating_add(1);
            return;
        }
        self.alerts.push(OperatorAlert {
            market_id: observer.market_id,
            worker_id: observer.worker_id,
            code: error.code(),
        });
    }
}
