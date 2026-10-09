//! Finality-bound delivery of AI market events.
//!
//! The producer side admits one captured market outbox row only after a
//! checkpoint certificate signed by the bonded guarantor set reaches rank 4 and
//! binds the exact view root and batch on the configured chain, maps it onto
//! the existing program event family,
//! and keeps a durable per-market outbox whose event identifiers, bodies and
//! subject sequences survive restart. The receiver side verifies the published
//! signature scheme and applies each event once, in subject order, durably.

use std::collections::BTreeMap;
use std::fs::{self, File, OpenOptions};
use std::io::{ErrorKind, Write};
use std::path::{Path, PathBuf};

use ed25519_dalek::{Signer, SigningKey};
use layerx_programs_ai_market::codec::state_digest;
use layerx_programs_ai_market::queries::{SnapshotBinding, BINDING_MAX_BYTES, FINALIZED_RANK};
use layerx_programs_ai_market::{Digest32, Presence};
use layerx_proof::checkpoint::{verify_certificate, Certificate, GuarantorKey, SettlementDomain};
use layerx_wire::hash::program_execution_batch_id;
use layerx_wire::receipt::decode_batch_header;
use serde::de::DeserializeOwned;
use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};
use subtle::ConstantTimeEq;
use zeroize::Zeroizing;

use crate::encoding::{digest, hex_encode};
use crate::error::WebhookError;
use crate::events::{
    EndpointId, EventDraft, EventId, EventKind, Principal, ProtocolEvent, ProtocolFact, SubjectId,
    Verification,
};
use crate::http::MAXIMUM_REQUEST_BYTES;
use crate::scheme::{self, Presentation};
use crate::trusted::IngressRole;

/// Domain of the stable market event identifier.
pub const VIEW_EVENT_DOMAIN: &[u8] = b"PAXAI/view-event/v1";
/// Prefix of every market event identifier.
pub const EVENT_ID_PREFIX: &str = "0x";
/// Prefix of every per-market ordering subject.
pub const SUBJECT_PREFIX: &str = "paxai_market_";
/// The only capture decoder version this adapter admits.
pub const DECODER_VERSION: u16 = 1;
/// Weakest verification rank that permits finalized publication.
pub const MINIMUM_PUBLICATION_RANK: u8 = FINALIZED_RANK;
/// Largest exact serialized market state a snapshot binds.
pub const MAXIMUM_STATE_BYTES: usize = 262_144;
/// Largest checkpoint validity proof admitted per row.
pub const MAXIMUM_EVIDENCE_BYTES: usize = 1_048_576;
/// Largest registered mutation operation name carried in an event topic.
pub const MAXIMUM_ACTION_BYTES: usize = 55;
/// Largest resume page.
pub const MAXIMUM_PAGE_ROWS: usize = 32;
/// Unpublished rows a market stream holds before capture stops.
pub const MAXIMUM_UNPUBLISHED_ROWS: usize = 4_096;

const MAXIMUM_ALERTS: usize = 256;
const MAXIMUM_APPLIED: usize = 4_096;
const MAXIMUM_SUBJECTS: usize = 1_024;
const MAXIMUM_STORE_BYTES: u64 = 64 * 1024 * 1024;
const CURSOR_PREFIX: &str = "paxaiwc1_";
const CURSOR_DOMAIN: &[u8] = b"PAXAI/webhook-cursor/v1";
const CURSOR_BODY_BYTES: usize = 2 + 16 + 32;
const STATE_FILE: &str = "state.json";
const STAGED_FILE: &str = "state.json.staged";
const LOCK_FILE: &str = "state.lock";

/// Returns the stable event identifier of one market effect.
///
/// # Errors
/// Returns [`WebhookError::InvalidRequest`] if the identifier violates the
/// webhook token rule.
pub fn market_event_id(
    market_id: &[u8; 32],
    source_activity_id: &[u8; 32],
    effect_ordinal: u16,
    ai_event_tag: u16,
) -> Result<EventId, WebhookError> {
    let mut hasher = Sha256::new();
    hasher.update(VIEW_EVENT_DOMAIN);
    hasher.update([0]);
    hasher.update(market_id);
    hasher.update(source_activity_id);
    hasher.update(effect_ordinal.to_be_bytes());
    hasher.update(ai_event_tag.to_be_bytes());
    let value: [u8; 32] = hasher.finalize().into();
    EventId::new(format!("{EVENT_ID_PREFIX}{}", hex_encode(&value)))
}

/// Returns the ordering subject of one market stream.
///
/// # Errors
/// Returns [`WebhookError::InvalidRequest`] if the subject violates the token rule.
pub fn market_subject(market_id: &[u8; 32]) -> Result<SubjectId, WebhookError> {
    SubjectId::new(format!("{SUBJECT_PREFIX}{}", hex_encode(market_id)))
}

/// Upstream manifest material that rides with a capture but is never public.
#[derive(Clone, Debug, Default, Eq, PartialEq)]
pub struct UpstreamManifest {
    pub display_name: Option<String>,
    pub result_locator: Option<String>,
}

/// One durable capture outbox row written with its canonical snapshot binding.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct CapturedEvent {
    pub binding: SnapshotBinding,
    pub snapshot_id: [u8; 32],
    pub event_id: String,
    pub decoder_version: u16,
    pub action: String,
    pub ai_event_tag: u16,
    pub source_activity_id: [u8; 32],
    pub effect_ordinal: u16,
    pub receipt_digest: [u8; 32],
    pub manifest: UpstreamManifest,
}

/// The exact state bytes a capture row binds and, once the binding claims
/// finalized rank, the checkpoint proof for its root.
#[derive(Clone, Copy, Debug)]
pub struct Evidence<'a> {
    pub state: &'a [u8],
    pub proof: Option<CheckpointProof<'a>>,
}

/// A checkpoint certificate with the identifier and settlement reference
/// registered for it on the settlement chain.
#[derive(Clone, Copy, Debug)]
pub struct CheckpointProof<'a> {
    pub certificate: &'a Certificate,
    pub registered_checkpoint_id: [u8; 32],
    pub registered_settlement_reference: Option<&'a [u8]>,
}

/// The deployment a publisher is pinned to: chain, network, settlement domain
/// and the bonded guarantor set that signs its checkpoints.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct FinalityPolicy {
    pub chain_domain: [u8; 32],
    pub network_id: u32,
    pub settlement: SettlementDomain,
    pub guarantors: Vec<GuarantorKey>,
}

/// Proof that the caller presented a verified internal producer leaf.
#[derive(Debug)]
pub struct ProducerGrant(());

impl ProducerGrant {
    /// Accepts only a client leaf holding the producer role.
    ///
    /// # Errors
    /// Returns [`MarketEventError::ProducerRoleRequired`] for any other leaf.
    pub fn from_certificate(leaf: &[u8]) -> Result<Self, MarketEventError> {
        match IngressRole::from_certificate(leaf) {
            Ok(IngressRole::Producer) => Ok(Self(())),
            Ok(IngressRole::Operator) | Err(_) => Err(MarketEventError::ProducerRoleRequired),
        }
    }
}

/// Why a market stream stopped finalized publication.
#[derive(Clone, Copy, Debug, Eq, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum QuarantineCause {
    DecoderVersion,
    ChainDomain,
    StateDigest,
    SnapshotId,
    EventId,
    CheckpointNetwork,
    FinalityConflict,
    EventConflict,
    FinalityBoundary,
}

impl QuarantineCause {
    /// Returns the operator error category.
    #[must_use]
    pub const fn as_str(self) -> &'static str {
        match self {
            Self::DecoderVersion => "unsupported_decoder_version",
            Self::ChainDomain => "chain_domain_mismatch",
            Self::StateDigest => "state_digest_mismatch",
            Self::SnapshotId => "snapshot_id_mismatch",
            Self::EventId => "event_id_mismatch",
            Self::CheckpointNetwork => "checkpoint_network_mismatch",
            Self::FinalityConflict => "finality_conflict",
            Self::EventConflict => "event_conflict",
            Self::FinalityBoundary => "finality_boundary_crossed",
        }
    }
}

/// An operator alert carrying identifiers and an error category only.
#[derive(Clone, Debug, Eq, PartialEq, Serialize, Deserialize)]
pub struct OperatorAlert {
    pub market_id: String,
    pub event_id: Option<String>,
    pub snapshot_id: Option<String>,
    pub category: String,
}

/// Exact refusals of the market event surface.
#[derive(Debug)]
pub enum MarketEventError {
    /// A row, cursor or argument violated a declared bound or shape.
    InvalidRequest,
    /// The caller does not hold the internal producer role.
    ProducerRoleRequired,
    /// The checkpoint certificate did not verify against the bonded guarantor
    /// set or proves another checkpoint or batch; nothing was admitted.
    CertificateRejected,
    /// This call quarantined the market stream.
    Quarantined(QuarantineCause),
    /// The market stream is quarantined and refuses finalized publication.
    StreamQuarantined,
    /// Unpublished rows reached their bound; capture stops and alerts.
    CaptureStopped,
    /// The tenant holds no current authorization for the market stream.
    Revoked,
    /// The webhook surface refused the event or its durable store failed.
    Webhook(WebhookError),
}

impl From<WebhookError> for MarketEventError {
    fn from(value: WebhookError) -> Self {
        Self::Webhook(value)
    }
}

impl From<std::io::Error> for MarketEventError {
    fn from(value: std::io::Error) -> Self {
        Self::Webhook(WebhookError::Io(value))
    }
}

trait Persisted {
    fn persists(&self) -> bool;
}

impl Persisted for MarketEventError {
    fn persists(&self) -> bool {
        matches!(self, Self::Quarantined(_) | Self::CaptureStopped)
    }
}

impl Persisted for WebhookError {
    fn persists(&self) -> bool {
        false
    }
}

struct Store {
    directory: PathBuf,
}

impl Store {
    fn open(directory: &Path) -> Result<Self, WebhookError> {
        fs::create_dir_all(directory)?;
        Ok(Self {
            directory: directory.to_path_buf(),
        })
    }

    fn lock(&self) -> Result<File, WebhookError> {
        Ok(OpenOptions::new()
            .create(true)
            .truncate(false)
            .write(true)
            .open(self.directory.join(LOCK_FILE))?)
    }

    fn load<T: DeserializeOwned + Default>(&self) -> Result<T, WebhookError> {
        let path = self.directory.join(STATE_FILE);
        match fs::metadata(&path) {
            Ok(metadata) if metadata.len() > MAXIMUM_STORE_BYTES => Err(WebhookError::CorruptStore),
            Ok(_) => {
                serde_json::from_slice(&fs::read(&path)?).map_err(|_| WebhookError::CorruptStore)
            }
            Err(error) if error.kind() == ErrorKind::NotFound => Ok(T::default()),
            Err(error) => Err(WebhookError::Io(error)),
        }
    }

    fn read<T: DeserializeOwned + Default>(&self) -> Result<T, WebhookError> {
        let lock = self.lock()?;
        lock.lock_shared()?;
        self.load()
    }

    fn transact<T, R, E>(&self, apply: impl FnOnce(&mut T) -> Result<R, E>) -> Result<R, E>
    where
        T: Serialize + DeserializeOwned + Default,
        E: From<WebhookError> + Persisted,
    {
        let lock = self.lock()?;
        lock.lock().map_err(WebhookError::Io)?;
        let mut state: T = self.load()?;
        let outcome = apply(&mut state);
        let durable = match &outcome {
            Ok(_) => true,
            Err(error) => error.persists(),
        };
        if durable {
            self.commit(&state)?;
        }
        outcome
    }

    fn commit<T: Serialize>(&self, state: &T) -> Result<(), WebhookError> {
        let bytes = serde_json::to_vec(state).map_err(|_| WebhookError::CorruptStore)?;
        if u64::try_from(bytes.len()).unwrap_or(u64::MAX) > MAXIMUM_STORE_BYTES {
            return Err(WebhookError::Unavailable);
        }
        let staged = self.directory.join(STAGED_FILE);
        let mut file = File::create(&staged)?;
        file.write_all(&bytes)?;
        file.sync_all()?;
        fs::rename(&staged, self.directory.join(STATE_FILE))?;
        File::open(&self.directory)?.sync_all()?;
        Ok(())
    }
}

#[derive(Clone, Debug, Serialize, Deserialize)]
struct OutboxRow {
    event: ProtocolEvent,
    observed_sequence: u64,
    published: bool,
}

#[derive(Clone, Debug, Serialize, Deserialize)]
struct Admitted {
    sequence: u64,
    identity: [u8; 32],
}

#[derive(Clone, Debug, Serialize, Deserialize)]
struct Provisional {
    identity: [u8; 32],
    observed_sequence: u64,
}

#[derive(Debug, Default, Serialize, Deserialize)]
struct MarketStream {
    granted: bool,
    quarantine: Option<QuarantineCause>,
    last_sequence: u64,
    pruned_through: u64,
    finality_boundary: u64,
    latest_snapshot: Option<[u8; 32]>,
    rows: BTreeMap<u64, OutboxRow>,
    admitted: BTreeMap<String, Admitted>,
    provisional: BTreeMap<String, Provisional>,
}

#[derive(Debug, Default, Serialize, Deserialize)]
struct OutboxState {
    streams: BTreeMap<String, MarketStream>,
    alerts: Vec<OperatorAlert>,
}

impl OutboxState {
    fn stop(
        &mut self,
        market: &str,
        event_id: Option<&str>,
        snapshot_id: Option<&[u8; 32]>,
        stop: Stop,
    ) -> MarketEventError {
        let (category, error) = match stop {
            Stop::Quarantine(cause) => {
                if let Some(stream) = self.streams.get_mut(market) {
                    stream.quarantine = Some(cause);
                }
                (cause.as_str(), MarketEventError::Quarantined(cause))
            }
            Stop::Capacity => ("outbox_capacity", MarketEventError::CaptureStopped),
            Stop::Refuse(error) => return error,
        };
        if self.alerts.len() >= MAXIMUM_ALERTS {
            self.alerts.remove(0);
        }
        self.alerts.push(OperatorAlert {
            market_id: market.to_owned(),
            event_id: event_id.map(str::to_owned),
            snapshot_id: snapshot_id.map(|value| hex_encode(value)),
            category: category.to_owned(),
        });
        error
    }
}

enum Stop {
    Quarantine(QuarantineCause),
    Capacity,
    Refuse(MarketEventError),
}

/// The result of admitting one captured row.
#[derive(Clone, Debug, Eq, PartialEq)]
pub enum Admission {
    /// Matching finality evidence is not yet available; publication waits.
    Delayed,
    /// The row holds its durable subject sequence and immutable event.
    Finalized {
        event: ProtocolEvent,
        duplicate: bool,
    },
}

/// One resume answer for a webhook endpoint.
#[derive(Clone, Debug, Eq, PartialEq)]
pub enum ResumePage {
    /// Events strictly after the cursor position, in subject order.
    Events {
        events: Vec<ProtocolEvent>,
        next_cursor: String,
        has_more: bool,
    },
    /// The cursor is no longer resumable; the receiver must resynchronize
    /// from the named finalized snapshot through the named sequence.
    Resync {
        snapshot_id: Option<[u8; 32]>,
        through_sequence: u64,
        next_cursor: String,
    },
}

/// Webhook resume cursor keys with bounded rotation overlap.
pub struct CursorKeys {
    current: u8,
    keys: BTreeMap<u8, Zeroizing<[u8; 32]>>,
}

impl CursorKeys {
    /// Issues cursors under `current` and still accepts the retained keys.
    ///
    /// # Errors
    /// Returns [`MarketEventError::InvalidRequest`] when a key identifier repeats.
    pub fn new(
        current: (u8, [u8; 32]),
        retained: &[(u8, [u8; 32])],
    ) -> Result<Self, MarketEventError> {
        let mut keys = BTreeMap::new();
        for (id, key) in std::iter::once(&current).chain(retained) {
            if keys.insert(*id, Zeroizing::new(*key)).is_some() {
                return Err(MarketEventError::InvalidRequest);
            }
        }
        Ok(Self {
            current: current.0,
            keys,
        })
    }

    fn encode(
        &self,
        principal: &Principal,
        endpoint: &EndpointId,
        market: &str,
        position: u64,
    ) -> String {
        self.keys
            .get(&self.current)
            .map_or_else(String::new, |key| {
                cursor_text(self.current, key, principal, endpoint, market, position)
            })
    }

    fn decode(
        &self,
        principal: &Principal,
        endpoint: &EndpointId,
        market: &str,
        cursor: &str,
    ) -> Result<Option<u64>, MarketEventError> {
        let body = cursor
            .strip_prefix(CURSOR_PREFIX)
            .filter(|body| body.len() == CURSOR_BODY_BYTES && body.is_ascii())
            .ok_or(WebhookError::InvalidCursor)?;
        let key_id = u8::from_str_radix(&body[..2], 16).map_err(|_| WebhookError::InvalidCursor)?;
        let position =
            u64::from_str_radix(&body[2..18], 16).map_err(|_| WebhookError::InvalidCursor)?;
        let Some(key) = self.keys.get(&key_id) else {
            return Ok(None);
        };
        let expected = cursor_text(key_id, key, principal, endpoint, market, position);
        if expected.as_bytes().ct_eq(cursor.as_bytes()).unwrap_u8() != 1 {
            return Err(WebhookError::InvalidCursor.into());
        }
        Ok(Some(position))
    }
}

fn cursor_text(
    key_id: u8,
    key: &[u8; 32],
    principal: &Principal,
    endpoint: &EndpointId,
    market: &str,
    position: u64,
) -> String {
    let tag = cursor_tag(key, principal, endpoint, market, position);
    format!(
        "{CURSOR_PREFIX}{key_id:02x}{position:016x}{}",
        hex_encode(&tag[..16])
    )
}

fn cursor_tag(
    key: &[u8; 32],
    principal: &Principal,
    endpoint: &EndpointId,
    market: &str,
    position: u64,
) -> [u8; 32] {
    let mut inner_pad = Zeroizing::new([0x36_u8; 64]);
    let mut outer_pad = Zeroizing::new([0x5c_u8; 64]);
    for (index, byte) in key.iter().enumerate() {
        inner_pad[index] ^= byte;
        outer_pad[index] ^= byte;
    }
    let mut inner = Sha256::new();
    inner.update(*inner_pad);
    inner.update(CURSOR_DOMAIN);
    inner.update([0]);
    inner.update(principal.as_str().as_bytes());
    inner.update([0]);
    inner.update(endpoint.as_str().as_bytes());
    inner.update([0]);
    inner.update(market.as_bytes());
    inner.update(position.to_be_bytes());
    let mut outer = Sha256::new();
    outer.update(*outer_pad);
    outer.update(inner.finalize());
    outer.finalize().into()
}

/// The durable per-tenant outbox of finalized AI market events.
pub struct MarketOutbox {
    store: Store,
    principal: Principal,
    policy: FinalityPolicy,
    cursor_keys: CursorKeys,
}

impl MarketOutbox {
    /// Opens the tenant outbox stored under `directory`.
    ///
    /// # Errors
    /// Returns [`WebhookError::Io`] when the directory cannot be created.
    pub fn open(
        directory: &Path,
        principal: Principal,
        policy: FinalityPolicy,
        cursor_keys: CursorKeys,
    ) -> Result<Self, MarketEventError> {
        Ok(Self {
            store: Store::open(directory)?,
            principal,
            policy,
            cursor_keys,
        })
    }

    /// Records the tenant's current authorization for one market stream.
    ///
    /// # Errors
    /// Returns store failures.
    pub fn grant(&self, market_id: &[u8; 32]) -> Result<(), MarketEventError> {
        self.set_grant(market_id, true)
    }

    /// Revokes the tenant's authorization; later admission, delivery and
    /// resume for the market refuse.
    ///
    /// # Errors
    /// Returns store failures.
    pub fn revoke(&self, market_id: &[u8; 32]) -> Result<(), MarketEventError> {
        self.set_grant(market_id, false)
    }

    fn set_grant(&self, market_id: &[u8; 32], granted: bool) -> Result<(), MarketEventError> {
        let market = hex_encode(market_id);
        self.store.transact(|state: &mut OutboxState| {
            state.streams.entry(market).or_default().granted = granted;
            Ok::<(), MarketEventError>(())
        })
    }

    /// Admits one captured row. Rows below rank 4 wait; rows whose evidence
    /// does not bind the configured chain, exact state, snapshot, identifier,
    /// decoder and checkpoint root quarantine the market stream.
    ///
    /// # Errors
    /// Returns [`MarketEventError::InvalidRequest`] for an unbounded or
    /// malformed row, [`MarketEventError::Revoked`] without a current grant,
    /// [`MarketEventError::Quarantined`] for the quarantining cause,
    /// [`MarketEventError::StreamQuarantined`] once quarantined,
    /// [`MarketEventError::CertificateRejected`] for a certificate that does
    /// not verify or proves another checkpoint or batch,
    /// [`MarketEventError::CaptureStopped`] when unpublished rows are full, and
    /// store failures.
    pub fn admit(
        &self,
        _producer: &ProducerGrant,
        row: &CapturedEvent,
        evidence: &Evidence<'_>,
    ) -> Result<Admission, MarketEventError> {
        well_formed(row, evidence)?;
        let examined = examine(&self.policy, row, evidence);
        let market = hex_encode(row.binding.market.as_bytes());
        self.store.transact(|state: &mut OutboxState| {
            let stream = state.streams.entry(market.clone()).or_default();
            let outcome = admit_into(stream, &self.principal, row, examined);
            outcome.map_err(|stop| {
                state.stop(&market, Some(&row.event_id), Some(&row.snapshot_id), stop)
            })
        })
    }

    /// Removes provisional rows observed after `observed_through`. A rollback
    /// below the verified finality boundary is refused and quarantines.
    ///
    /// # Errors
    /// Returns [`MarketEventError::Quarantined`] with
    /// [`QuarantineCause::FinalityBoundary`], [`MarketEventError::StreamQuarantined`],
    /// and store failures.
    pub fn rollback(
        &self,
        market_id: &[u8; 32],
        observed_through: u64,
    ) -> Result<usize, MarketEventError> {
        let market = hex_encode(market_id);
        self.store.transact(|state: &mut OutboxState| {
            let stream = state.streams.entry(market.clone()).or_default();
            if stream.quarantine.is_some() {
                return Err(MarketEventError::StreamQuarantined);
            }
            if observed_through < stream.finality_boundary {
                return Err(state.stop(
                    &market,
                    None,
                    None,
                    Stop::Quarantine(QuarantineCause::FinalityBoundary),
                ));
            }
            let before = stream.provisional.len();
            stream
                .provisional
                .retain(|_, row| row.observed_sequence <= observed_through);
            Ok(before.saturating_sub(stream.provisional.len()))
        })
    }

    /// Returns unpublished finalized events in subject order.
    ///
    /// # Errors
    /// Returns [`MarketEventError::Revoked`] without a current grant and store failures.
    pub fn pending(&self, market_id: &[u8; 32]) -> Result<Vec<ProtocolEvent>, MarketEventError> {
        let state: OutboxState = self.store.read()?;
        let stream = granted(&state, &hex_encode(market_id))?;
        Ok(stream
            .rows
            .values()
            .filter(|row| !row.published)
            .map(|row| row.event.clone())
            .collect())
    }

    /// Records that `event` reached an accepting endpoint.
    ///
    /// # Errors
    /// Returns [`WebhookError::UnknownDelivery`] for an event this outbox did not
    /// admit and store failures.
    pub fn acknowledge(
        &self,
        market_id: &[u8; 32],
        event: &EventId,
    ) -> Result<(), MarketEventError> {
        let market = hex_encode(market_id);
        self.store.transact(|state: &mut OutboxState| {
            let stream = state
                .streams
                .get_mut(&market)
                .ok_or(WebhookError::UnknownDelivery)?;
            let sequence = stream
                .admitted
                .get(event.as_str())
                .map(|admitted| admitted.sequence)
                .ok_or(WebhookError::UnknownDelivery)?;
            if let Some(row) = stream.rows.get_mut(&sequence) {
                row.published = true;
            }
            Ok(())
        })
    }

    /// Releases published rows through `sequence`; unpublished rows are never
    /// released to meet retention.
    ///
    /// # Errors
    /// Returns [`MarketEventError::InvalidRequest`] when an unpublished row
    /// would be released and store failures.
    pub fn prune(&self, market_id: &[u8; 32], sequence: u64) -> Result<usize, MarketEventError> {
        let market = hex_encode(market_id);
        self.store.transact(|state: &mut OutboxState| {
            let stream = state
                .streams
                .get_mut(&market)
                .ok_or(MarketEventError::InvalidRequest)?;
            if sequence > stream.last_sequence
                || stream
                    .rows
                    .range(..=sequence)
                    .any(|(_, row)| !row.published)
            {
                return Err(MarketEventError::InvalidRequest);
            }
            let before = stream.rows.len();
            stream.rows.retain(|position, _| *position > sequence);
            stream.pruned_through = stream.pruned_through.max(sequence);
            Ok(before.saturating_sub(stream.rows.len()))
        })
    }

    /// Resumes one endpoint after an authenticated cursor. The caller passes
    /// the principal its current session authenticated; the cursor grants no
    /// access of its own. An expired cursor answers an explicit resync.
    ///
    /// # Errors
    /// Returns [`MarketEventError::InvalidRequest`] for an unbounded page,
    /// [`MarketEventError::Revoked`] for another principal or a revoked grant,
    /// [`WebhookError::InvalidCursor`] for a forged or foreign cursor, and
    /// store failures.
    pub fn resume(
        &self,
        authorized: &Principal,
        endpoint: &EndpointId,
        market_id: &[u8; 32],
        cursor: Option<&str>,
        limit: usize,
    ) -> Result<ResumePage, MarketEventError> {
        if limit == 0 || limit > MAXIMUM_PAGE_ROWS || cursor.is_some_and(|value| value.len() > 64) {
            return Err(MarketEventError::InvalidRequest);
        }
        if authorized != &self.principal {
            return Err(MarketEventError::Revoked);
        }
        let market = hex_encode(market_id);
        let state: OutboxState = self.store.read()?;
        let stream = granted(&state, &market)?;
        let position = match cursor {
            None => Some(stream.pruned_through),
            Some(value) => self
                .cursor_keys
                .decode(authorized, endpoint, &market, value)?,
        };
        let Some(position) = position.filter(|position| *position >= stream.pruned_through) else {
            return Ok(ResumePage::Resync {
                snapshot_id: stream.latest_snapshot,
                through_sequence: stream.last_sequence,
                next_cursor: self.cursor_keys.encode(
                    authorized,
                    endpoint,
                    &market,
                    stream.last_sequence,
                ),
            });
        };
        if position > stream.last_sequence {
            return Err(WebhookError::InvalidCursor.into());
        }
        let mut selected = stream
            .rows
            .range(position.saturating_add(1)..)
            .take(limit.saturating_add(1));
        let events: Vec<(u64, ProtocolEvent)> = selected
            .by_ref()
            .take(limit)
            .map(|(sequence, row)| (*sequence, row.event.clone()))
            .collect();
        let has_more = selected.next().is_some();
        let reached = events.last().map_or(position, |(sequence, _)| *sequence);
        Ok(ResumePage::Events {
            events: events.into_iter().map(|(_, event)| event).collect(),
            next_cursor: self
                .cursor_keys
                .encode(authorized, endpoint, &market, reached),
            has_more,
        })
    }

    /// Returns the operator alerts recorded by this outbox.
    ///
    /// # Errors
    /// Returns store failures.
    pub fn alerts(&self) -> Result<Vec<OperatorAlert>, MarketEventError> {
        let state: OutboxState = self.store.read()?;
        Ok(state.alerts)
    }

    /// Returns the quarantine cause of a market stream, if any.
    ///
    /// # Errors
    /// Returns store failures.
    pub fn quarantine(
        &self,
        market_id: &[u8; 32],
    ) -> Result<Option<QuarantineCause>, MarketEventError> {
        let state: OutboxState = self.store.read()?;
        Ok(state
            .streams
            .get(&hex_encode(market_id))
            .and_then(|stream| stream.quarantine))
    }
}

fn granted<'a>(state: &'a OutboxState, market: &str) -> Result<&'a MarketStream, MarketEventError> {
    state
        .streams
        .get(market)
        .filter(|stream| stream.granted)
        .ok_or(MarketEventError::Revoked)
}

fn nonzero(value: &[u8; 32]) -> bool {
    value.iter().any(|byte| *byte != 0)
}

fn well_formed(row: &CapturedEvent, evidence: &Evidence<'_>) -> Result<(), MarketEventError> {
    let mut canonical = [0_u8; BINDING_MAX_BYTES];
    let finalized = row.binding.rank >= MINIMUM_PUBLICATION_RANK;
    let shaped = row.binding.encode(&mut canonical).is_ok()
        && evidence.state.len() <= MAXIMUM_STATE_BYTES
        && evidence.proof.map_or(!finalized, |proof| {
            proof.certificate.checkpoint().validity_proof().len() <= MAXIMUM_EVIDENCE_BYTES
        })
        && nonzero(&row.source_activity_id)
        && nonzero(&row.receipt_digest)
        && !row.action.is_empty()
        && row.action.len() <= MAXIMUM_ACTION_BYTES
        && row
            .action
            .bytes()
            .all(|byte| byte.is_ascii_uppercase() || byte.is_ascii_digit() || byte == b'_');
    if !shaped {
        return Err(MarketEventError::InvalidRequest);
    }
    EventId::new(row.event_id.as_str())?;
    Ok(())
}

struct Examined {
    identity: [u8; 32],
    finalized: bool,
}

fn examine(
    policy: &FinalityPolicy,
    row: &CapturedEvent,
    evidence: &Evidence<'_>,
) -> Result<Examined, Stop> {
    let binding = &row.binding;
    if row.decoder_version != DECODER_VERSION {
        return Err(Stop::Quarantine(QuarantineCause::DecoderVersion));
    }
    if binding.chain.bytes() != policy.chain_domain {
        return Err(Stop::Quarantine(QuarantineCause::ChainDomain));
    }
    if state_digest(evidence.state).ok() != Some(binding.state_digest) {
        return Err(Stop::Quarantine(QuarantineCause::StateDigest));
    }
    if binding.snapshot_id().ok().map(Digest32::bytes) != Some(row.snapshot_id) {
        return Err(Stop::Quarantine(QuarantineCause::SnapshotId));
    }
    let expected = market_event_id(
        binding.market.as_bytes(),
        &row.source_activity_id,
        row.effect_ordinal,
        row.ai_event_tag,
    )
    .map_err(|_| Stop::Quarantine(QuarantineCause::EventId))?;
    if expected.as_str() != row.event_id {
        return Err(Stop::Quarantine(QuarantineCause::EventId));
    }
    let identity = identity(row);
    match evidence.proof {
        Some(proof) if binding.rank >= MINIMUM_PUBLICATION_RANK => {
            certify(policy, binding, &proof)?;
            Ok(Examined {
                identity,
                finalized: true,
            })
        }
        _ => Ok(Examined {
            identity,
            finalized: false,
        }),
    }
}

fn certify(
    policy: &FinalityPolicy,
    binding: &SnapshotBinding,
    proof: &CheckpointProof<'_>,
) -> Result<(), Stop> {
    let rejected = || Stop::Refuse(MarketEventError::CertificateRejected);
    let header = decode_batch_header(proof.certificate.checkpoint().header_bytes())
        .map_err(|_| rejected())?;
    if header.network_id() != policy.network_id {
        return Err(Stop::Quarantine(QuarantineCause::CheckpointNetwork));
    }
    let report = verify_certificate(
        proof.certificate,
        &policy.guarantors,
        &proof.registered_checkpoint_id,
        policy.settlement,
        proof.registered_settlement_reference,
    )
    .map_err(|_| rejected())?;
    if report.level().wire_rank() < MINIMUM_PUBLICATION_RANK
        || report.evidence().checkpoint_id() != Some(binding.checkpoint.bytes())
    {
        return Err(rejected());
    }
    let root = binding.native_state_root.bytes();
    if header.last_sequence() == binding.observed_sequence && report.resulting_state_root() != root
    {
        return Err(Stop::Quarantine(QuarantineCause::FinalityConflict));
    }
    let batch = program_execution_batch_id(
        header.previous_state_root(),
        header.activity_merkle_root(),
        header.first_sequence(),
        header.last_sequence(),
        header.batch_number(),
    )
    .map_err(|_| rejected())?;
    if header.last_sequence() != binding.observed_sequence
        || report.resulting_state_root() != root
        || batch != binding.batch_id.bytes()
    {
        return Err(rejected());
    }
    Ok(())
}

fn identity(row: &CapturedEvent) -> [u8; 32] {
    let binding = &row.binding;
    let mut hasher = Sha256::new();
    hasher.update(row.snapshot_id);
    hasher.update(row.event_id.as_bytes());
    hasher.update([0]);
    hasher.update(row.action.as_bytes());
    hasher.update([0]);
    hasher.update(row.ai_event_tag.to_be_bytes());
    hasher.update(row.decoder_version.to_be_bytes());
    hasher.update(row.source_activity_id);
    hasher.update(row.effect_ordinal.to_be_bytes());
    hasher.update(row.receipt_digest);
    hasher.update(binding.checkpoint.bytes());
    hasher.finalize().into()
}

fn admit_into(
    stream: &mut MarketStream,
    principal: &Principal,
    row: &CapturedEvent,
    examined: Result<Examined, Stop>,
) -> Result<Admission, Stop> {
    if !stream.granted {
        return Err(Stop::Refuse(MarketEventError::Revoked));
    }
    if stream.quarantine.is_some() {
        return Err(Stop::Refuse(MarketEventError::StreamQuarantined));
    }
    let examined = examined?;
    if let Some(admitted) = stream.admitted.get(&row.event_id) {
        if admitted.identity != examined.identity {
            return Err(Stop::Quarantine(QuarantineCause::EventConflict));
        }
        let event = match stream.rows.get(&admitted.sequence) {
            Some(stored) => stored.event.clone(),
            None => market_event(principal, row, admitted.sequence)
                .map_err(|error| Stop::Refuse(error.into()))?,
        };
        return Ok(Admission::Finalized {
            event,
            duplicate: true,
        });
    }
    if stream
        .provisional
        .get(&row.event_id)
        .is_some_and(|provisional| provisional.identity != examined.identity)
    {
        return Err(Stop::Quarantine(QuarantineCause::EventConflict));
    }
    if !examined.finalized {
        stream.provisional.insert(
            row.event_id.clone(),
            Provisional {
                identity: examined.identity,
                observed_sequence: row.binding.observed_sequence,
            },
        );
        return Ok(Admission::Delayed);
    }
    if stream
        .rows
        .values()
        .filter(|stored| !stored.published)
        .count()
        >= MAXIMUM_UNPUBLISHED_ROWS
    {
        return Err(Stop::Capacity);
    }
    let sequence = stream
        .last_sequence
        .checked_add(1)
        .ok_or(Stop::Refuse(MarketEventError::InvalidRequest))?;
    let event =
        market_event(principal, row, sequence).map_err(|error| Stop::Refuse(error.into()))?;
    stream.provisional.remove(&row.event_id);
    stream.last_sequence = sequence;
    stream.admitted.insert(
        row.event_id.clone(),
        Admitted {
            sequence,
            identity: examined.identity,
        },
    );
    stream.rows.insert(
        sequence,
        OutboxRow {
            event: event.clone(),
            observed_sequence: row.binding.observed_sequence,
            published: false,
        },
    );
    if row.binding.observed_sequence >= stream.finality_boundary {
        stream.finality_boundary = row.binding.observed_sequence;
        stream.latest_snapshot = Some(row.snapshot_id);
    }
    Ok(Admission::Finalized {
        event,
        duplicate: false,
    })
}

fn market_event(
    principal: &Principal,
    row: &CapturedEvent,
    sequence: u64,
) -> Result<ProtocolEvent, WebhookError> {
    let binding = &row.binding;
    let label = Verification::from_wire_rank(binding.rank);
    let receipt = hex_encode(&row.receipt_digest);
    let epoch = match binding.epoch {
        Presence::Present(epoch) => epoch.to_string(),
        Presence::Absent => "absent".to_owned(),
    };
    let values = [
        ("ai_action", row.action.clone()),
        ("ai_event_tag", row.ai_event_tag.to_string()),
        ("market_id", hex_encode(binding.market.as_bytes())),
        ("snapshot_id", hex_encode(&row.snapshot_id)),
        ("epoch", epoch),
        ("config_version", binding.config.get().to_string()),
        ("market_state_revision", binding.revision.to_string()),
        ("source_activity_id", hex_encode(&row.source_activity_id)),
        ("effect_ordinal", row.effect_ordinal.to_string()),
        ("chain_domain", hex_encode(binding.chain.as_bytes())),
        ("program_id", hex_encode(binding.program.as_bytes())),
        ("observed_sequence", binding.observed_sequence.to_string()),
        ("execution_height", binding.execution_height.to_string()),
        ("batch_id", hex_encode(binding.batch_id.as_bytes())),
        (
            "native_state_root",
            hex_encode(binding.native_state_root.as_bytes()),
        ),
        ("checkpoint_id", hex_encode(binding.checkpoint.as_bytes())),
        ("achieved_rank", binding.rank.to_string()),
    ];
    let facts = values
        .into_iter()
        .map(|(name, value)| ProtocolFact::verified(name, value, label, receipt.as_str()))
        .collect::<Result<Vec<_>, _>>()?;
    ProtocolEvent::new(EventDraft {
        id: EventId::new(row.event_id.as_str())?,
        kind: EventKind::Program,
        principal: principal.clone(),
        subject: market_subject(binding.market.as_bytes())?,
        subject_sequence: sequence,
        occurred_at: binding.publication_time_ms / 1_000,
        facts,
    })
}

#[derive(Serialize, Deserialize)]
struct Envelope {
    scheme: String,
    endpoint_id: String,
    receiver_obligation: String,
    event: ProtocolEvent,
}

/// One signed delivery under the published webhook scheme.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct SignedDelivery {
    pub id: String,
    pub timestamp: String,
    pub key_id: String,
    pub signature: String,
    pub body: Vec<u8>,
}

impl SignedDelivery {
    /// Signs the exact delivery body of `event` for one endpoint. Every retry
    /// of one event carries the same identifier over a byte-identical body.
    ///
    /// # Errors
    /// Returns [`WebhookError::InvalidRequest`] for an invalid key identifier
    /// and [`WebhookError::CorruptStore`] when the body cannot be encoded.
    pub fn sign(
        event: &ProtocolEvent,
        endpoint: &EndpointId,
        key_id: &str,
        key: &SigningKey,
        timestamp: u64,
    ) -> Result<Self, WebhookError> {
        if !scheme::valid_key_id(key_id) {
            return Err(WebhookError::InvalidRequest);
        }
        let body = serde_json::to_vec(&Envelope {
            scheme: scheme::SCHEME_VERSION.to_owned(),
            endpoint_id: endpoint.as_str().to_owned(),
            receiver_obligation: scheme::RECEIVER_OBLIGATION.to_owned(),
            event: event.clone(),
        })
        .map_err(|_| WebhookError::CorruptStore)?;
        let signature = key.sign(&scheme::canonical_message(
            event.id().as_str(),
            timestamp,
            &body,
        ));
        Ok(Self {
            id: event.id().as_str().to_owned(),
            timestamp: timestamp.to_string(),
            key_id: key_id.to_owned(),
            signature: scheme::signature_header(&signature.to_bytes()),
            body,
        })
    }

    /// Presents this delivery to a receiver clock.
    #[must_use]
    pub fn presentation(&self, now: u64) -> Presentation<'_> {
        Presentation {
            id: &self.id,
            timestamp: &self.timestamp,
            key_id: &self.key_id,
            signature: &self.signature,
            payload: &self.body,
            now,
            tolerance_seconds: scheme::DEFAULT_TOLERANCE_SECONDS,
        }
    }
}

/// The receiver's answer to one verified delivery.
#[derive(Clone, Debug, Eq, PartialEq)]
pub enum Consumed {
    /// The local effect was applied and recorded with the sequence.
    Applied { sequence: u64, result: String },
    /// The event was already applied; the saved result is returned.
    Duplicate { sequence: u64, result: String },
    /// A sequence gap suspends ordered application until the missing event
    /// arrives or the receiver resynchronizes from a finalized snapshot.
    Suspended { expected: u64, received: u64 },
}

#[derive(Debug, Serialize, Deserialize)]
struct Applied {
    sequence: u64,
    body_digest: [u8; 32],
    result: String,
}

#[derive(Debug, Default, Serialize, Deserialize)]
struct SubjectLedger {
    last_sequence: u64,
    applied: BTreeMap<String, Applied>,
}

#[derive(Debug, Default, Serialize, Deserialize)]
struct ConsumerState {
    subjects: BTreeMap<String, SubjectLedger>,
}

/// A durable receiver that applies each finalized market event exactly once.
pub struct MarketConsumer {
    store: Store,
    keys: BTreeMap<String, [u8; 32]>,
}

impl MarketConsumer {
    /// Opens the receiver ledger under `directory` accepting `keys`.
    ///
    /// # Errors
    /// Returns [`WebhookError::InvalidRequest`] without keys and store failures.
    pub fn open(directory: &Path, keys: BTreeMap<String, [u8; 32]>) -> Result<Self, WebhookError> {
        if keys.is_empty() {
            return Err(WebhookError::InvalidRequest);
        }
        Ok(Self {
            store: Store::open(directory)?,
            keys,
        })
    }

    /// Verifies one delivery and applies `effect` once, in subject order,
    /// recording its result in the same durable transaction as the sequence.
    ///
    /// # Errors
    /// Returns [`WebhookError::InvalidRequest`] for an oversized body,
    /// [`WebhookError::StaleTimestamp`] or [`WebhookError::SignatureRejected`]
    /// from the published scheme, [`WebhookError::VerificationRequired`] for
    /// an event below checkpoint finality, [`WebhookError::EventConflict`] for
    /// a changed body under an applied identifier,
    /// [`WebhookError::OrderViolation`] for a decreasing sequence,
    /// [`WebhookError::ReplayCapacity`] when the ledger is full, and store failures.
    pub fn receive(
        &self,
        presentation: &Presentation<'_>,
        effect: impl FnOnce(&ProtocolEvent) -> String,
    ) -> Result<Consumed, WebhookError> {
        if presentation.payload.len() > MAXIMUM_REQUEST_BYTES {
            return Err(WebhookError::InvalidRequest);
        }
        let verified = scheme::verify(presentation, &self.keys)?;
        let envelope: Envelope = serde_json::from_slice(presentation.payload)
            .map_err(|_| WebhookError::SignatureRejected)?;
        let event = envelope.event;
        if envelope.scheme != scheme::SCHEME_VERSION || event.id().as_str() != verified.id {
            return Err(WebhookError::SignatureRejected);
        }
        event.validate()?;
        if event.kind() != EventKind::Program
            || !event
                .verification()
                .at_least(Verification::CheckpointFinalised)
        {
            return Err(WebhookError::VerificationRequired);
        }
        let body_digest = digest(presentation.payload);
        self.store.transact(|state: &mut ConsumerState| {
            if !state.subjects.contains_key(event.subject().as_str())
                && state.subjects.len() >= MAXIMUM_SUBJECTS
            {
                return Err(WebhookError::ReplayCapacity);
            }
            let ledger = state
                .subjects
                .entry(event.subject().as_str().to_owned())
                .or_default();
            apply_once(ledger, &event, body_digest, effect)
        })
    }

    /// Resynchronizes one subject from a finalized snapshot that covers every
    /// event through `through_sequence`.
    ///
    /// # Errors
    /// Returns [`WebhookError::OrderViolation`] for a resync behind the
    /// applied sequence and store failures.
    pub fn resynchronize(
        &self,
        subject: &SubjectId,
        through_sequence: u64,
    ) -> Result<(), WebhookError> {
        self.store.transact(|state: &mut ConsumerState| {
            let ledger = state
                .subjects
                .entry(subject.as_str().to_owned())
                .or_default();
            if through_sequence < ledger.last_sequence {
                return Err(WebhookError::OrderViolation);
            }
            ledger.last_sequence = through_sequence;
            Ok(())
        })
    }

    /// Returns the last applied sequence of `subject`.
    ///
    /// # Errors
    /// Returns store failures.
    pub fn last_sequence(&self, subject: &SubjectId) -> Result<u64, WebhookError> {
        let state: ConsumerState = self.store.read()?;
        Ok(state
            .subjects
            .get(subject.as_str())
            .map_or(0, |ledger| ledger.last_sequence))
    }

    /// Returns how many effects `subject` recorded.
    ///
    /// # Errors
    /// Returns store failures.
    pub fn applied(&self, subject: &SubjectId) -> Result<usize, WebhookError> {
        let state: ConsumerState = self.store.read()?;
        Ok(state
            .subjects
            .get(subject.as_str())
            .map_or(0, |ledger| ledger.applied.len()))
    }
}

fn apply_once(
    ledger: &mut SubjectLedger,
    event: &ProtocolEvent,
    body_digest: [u8; 32],
    effect: impl FnOnce(&ProtocolEvent) -> String,
) -> Result<Consumed, WebhookError> {
    if let Some(applied) = ledger.applied.get(event.id().as_str()) {
        if applied.body_digest != body_digest {
            return Err(WebhookError::EventConflict);
        }
        return Ok(Consumed::Duplicate {
            sequence: applied.sequence,
            result: applied.result.clone(),
        });
    }
    let sequence = event.subject_sequence();
    if sequence <= ledger.last_sequence {
        return Err(WebhookError::OrderViolation);
    }
    let expected = ledger.last_sequence.saturating_add(1);
    if sequence != expected {
        return Ok(Consumed::Suspended {
            expected,
            received: sequence,
        });
    }
    let result = effect(event);
    if ledger.applied.len() >= MAXIMUM_APPLIED {
        let oldest = ledger
            .applied
            .iter()
            .min_by_key(|(_, applied)| applied.sequence)
            .map(|(id, _)| id.clone());
        if let Some(oldest) = oldest {
            ledger.applied.remove(&oldest);
        }
    }
    ledger.applied.insert(
        event.id().as_str().to_owned(),
        Applied {
            sequence,
            body_digest,
            result: result.clone(),
        },
    );
    ledger.last_sequence = sequence;
    Ok(Consumed::Applied { sequence, result })
}
