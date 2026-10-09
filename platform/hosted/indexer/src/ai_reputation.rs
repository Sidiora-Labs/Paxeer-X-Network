//! Finalized, monotonic projection of the F07 reputation section.
//!
//! A projection advances only on a whole `ProgramRead` capture bound to a
//! threshold checkpoint certificate for the same root and batch height. The
//! accepted height, state commitment and reputation root persist per market;
//! a lower height never replaces them and two roots at one height halt the
//! market until an operator resolves the evidence conflict.

use std::path::Path;

use layerx_programs_ai_market::{
    errors::{
        ApplicationError, ARITHMETIC, F07_BINDING_MISMATCH, F07_EPOCH_NOT_SEALED,
        F07_FINALITY_UNAVAILABLE, F07_UNKNOWN_WORKER, WRONG_DOMAIN,
    },
    queries::{
        bind_snapshot, FinalityEvidence, QueryError, ReadProof, StateCapture, BINDING_MAX_BYTES,
    },
    reputation::{
        CompletedHistory, HistoryLookupError, ReputationCurrent, ReputationState, CURRENT_BYTES,
        HEADER_BYTES, HISTORY_BYTES,
    },
    reputation_codec::{decode_section, encode_history, reputation_root},
    state::{decode_shared_state, Section},
    types::{ChainDomain, Digest32, MarketId, Presence, ProgramId, WorkerId},
    MAX_STATE_BYTES,
};
use layerx_proof::checkpoint::{verify_declared_certificate, Certificate, CheckpointError};
use layerx_proof::settlement::declared_domain;
use rusqlite::{params, Connection, OptionalExtension as _, TransactionBehavior};

/// Executing heights after `last_observed_height` at which quality turns stale.
pub const STALE_AFTER_HEIGHTS: u64 = 4096;

const SCHEMA: &str = "
CREATE TABLE IF NOT EXISTS ai_reputation_cursor(
    market BLOB PRIMARY KEY,
    height INTEGER NOT NULL,
    native_state_root BLOB NOT NULL,
    snapshot_id BLOB NOT NULL,
    checkpoint_id BLOB NOT NULL,
    reputation_root BLOB,
    rank INTEGER NOT NULL,
    binding BLOB NOT NULL,
    section BLOB NOT NULL
);
CREATE TABLE IF NOT EXISTS ai_reputation_conflict(
    market BLOB PRIMARY KEY,
    height INTEGER NOT NULL,
    accepted_root BLOB NOT NULL,
    conflicting_root BLOB NOT NULL,
    conflicting_checkpoint BLOB NOT NULL
);
CREATE TABLE IF NOT EXISTS ai_reputation_archive(
    market BLOB NOT NULL,
    epoch INTEGER NOT NULL,
    row BLOB NOT NULL,
    height INTEGER NOT NULL,
    native_state_root BLOB NOT NULL,
    checkpoint_id BLOB NOT NULL,
    snapshot_id BLOB NOT NULL,
    PRIMARY KEY(market, epoch)
);
";

#[derive(Clone, Debug, Eq, PartialEq)]
pub enum ReputationError {
    /// A typed F07/common refusal under its frozen application code.
    Refused(ApplicationError),
    /// The chunked `ProgramRead` capture was inconsistent or incomplete.
    Capture(QueryError),
    /// The checkpoint certificate failed threshold verification.
    Certificate(CheckpointError),
    /// The finalized height precedes a height the projected state commits to.
    InvalidProof {
        finalized_height: u64,
        committed_height: u64,
    },
    /// A proof below the accepted finalized height cannot replace it.
    StaleProof { accepted: u64, offered: u64 },
    /// Two certified roots at one height; projection for the market is halted.
    Conflict(Box<RootConflict>),
    /// The epoch completed but left the retained ring; the archive locator is separate.
    HistoryOutsideRetention {
        oldest_retained: Option<u64>,
        archive: Option<Box<ArchiveLocator>>,
    },
    /// The durable projection store failed.
    Store(String),
}

impl From<ApplicationError> for ReputationError {
    fn from(error: ApplicationError) -> Self {
        Self::Refused(error)
    }
}

impl From<QueryError> for ReputationError {
    fn from(error: QueryError) -> Self {
        match error {
            QueryError::Application(code) => Self::Refused(code),
            QueryError::BindingMismatch => Self::Refused(F07_BINDING_MISMATCH),
            QueryError::FinalityUnavailable => Self::Refused(F07_FINALITY_UNAVAILABLE),
            other => Self::Capture(other),
        }
    }
}

impl From<rusqlite::Error> for ReputationError {
    fn from(error: rusqlite::Error) -> Self {
        Self::Store(error.to_string())
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct RootConflict {
    pub market: MarketId,
    pub height: u64,
    pub accepted_root: Digest32,
    pub conflicting_root: Digest32,
    pub conflicting_checkpoint: [u8; 32],
}

/// The certified snapshot in which an evicted completion row was last proven retained.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct ArchiveLocator {
    pub epoch: u64,
    pub finalized_height: u64,
    pub native_state_root: Digest32,
    pub checkpoint_id: [u8; 32],
    pub snapshot_id: Digest32,
}

/// The accepted finalized projection point of one market.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct FinalizedPoint {
    pub height: u64,
    pub native_state_root: Digest32,
    pub snapshot_id: Digest32,
    pub checkpoint_id: [u8; 32],
    pub reputation_root: Presence<Digest32>,
    pub rank: u8,
    /// Canonical `SnapshotBindingV1` bytes of the accepted capture.
    pub binding: Vec<u8>,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum Freshness {
    Fresh { age: u64 },
    Stale { age: u64 },
    Unobserved,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum Admission {
    Advanced { height: u64 },
    AlreadyAccepted { height: u64 },
}

/// One `ProgramRead` capture with the checkpoint evidence claimed for its root.
pub struct FinalizedRead<'a> {
    pub proof: ReadProof,
    pub chunks: &'a [Vec<u8>],
    pub certificate: &'a Certificate,
    pub registered_checkpoint_id: [u8; 32],
    pub publication_time_ms: u64,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct HistoryRequest {
    pub market: MarketId,
    pub worker: WorkerId,
    pub epoch: Option<u64>,
    pub minimum_finalized_height: u64,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct HistoryView {
    pub finalized: FinalizedPoint,
    pub current: ReputationCurrent,
    pub freshness: Freshness,
    /// Retained completion rows in ascending epoch order.
    pub history: Vec<CompletedHistory>,
}

pub struct ReputationProjector {
    connection: Connection,
    domain: String,
    chain: ChainDomain,
    program: ProgramId,
}

/// R021 freshness of `current` at finalized height `finalized_height`.
///
/// # Errors
/// `InvalidProof` when the finalized height precedes the last observation.
pub fn freshness(
    finalized_height: u64,
    current: &ReputationCurrent,
) -> Result<Freshness, ReputationError> {
    let Presence::Present(observed) = current.last_observed else {
        return Ok(Freshness::Unobserved);
    };
    let age =
        finalized_height
            .checked_sub(observed.height)
            .ok_or(ReputationError::InvalidProof {
                finalized_height,
                committed_height: observed.height,
            })?;
    Ok(if age >= STALE_AFTER_HEIGHTS {
        Freshness::Stale { age }
    } else {
        Freshness::Fresh { age }
    })
}

/// The F07 region leads the joint reputation/admission section as one self-delimiting RP07 frame.
fn reputation_region(section: &[u8]) -> &[u8] {
    let length = match section.get(6..8) {
        Some(&[current, history]) => {
            HEADER_BYTES
                + usize::from(current) * CURRENT_BYTES
                + usize::from(history) * HISTORY_BYTES
        }
        _ => section.len(),
    };
    section.get(..length).unwrap_or(section)
}

fn committed_height(state: &ReputationState) -> Option<u64> {
    state
        .records()
        .map(|record| record.last_transition_height)
        .chain(state.completed().map(|row| row.execution_height))
        .max()
}

fn stored(value: u64) -> Result<i64, ReputationError> {
    i64::try_from(value).map_err(|_| ReputationError::Refused(ARITHMETIC))
}

fn loaded(value: i64) -> Result<u64, ReputationError> {
    u64::try_from(value).map_err(|_| ReputationError::Store(format!("stored {value} is negative")))
}

fn fixed(bytes: &[u8]) -> Result<[u8; 32], ReputationError> {
    bytes
        .try_into()
        .map_err(|_| ReputationError::Store("stored digest is not 32 bytes".to_owned()))
}

fn digest(bytes: &[u8]) -> Result<Digest32, ReputationError> {
    Ok(Digest32::new(fixed(bytes)?)?)
}

fn load_conflict(
    connection: &Connection,
    market: MarketId,
) -> Result<Option<RootConflict>, ReputationError> {
    let row = connection
        .query_row(
            "SELECT height, accepted_root, conflicting_root, conflicting_checkpoint
             FROM ai_reputation_conflict WHERE market = ?1",
            params![&market.as_bytes()[..]],
            |row| {
                Ok((
                    row.get::<_, i64>(0)?,
                    row.get::<_, Vec<u8>>(1)?,
                    row.get::<_, Vec<u8>>(2)?,
                    row.get::<_, Vec<u8>>(3)?,
                ))
            },
        )
        .optional()?;
    row.map(|(height, accepted, conflicting, checkpoint)| {
        Ok(RootConflict {
            market,
            height: loaded(height)?,
            accepted_root: digest(&accepted)?,
            conflicting_root: digest(&conflicting)?,
            conflicting_checkpoint: fixed(&checkpoint)?,
        })
    })
    .transpose()
}

fn load_cursor(
    connection: &Connection,
    market: MarketId,
) -> Result<Option<(FinalizedPoint, Vec<u8>)>, ReputationError> {
    let row = connection
        .query_row(
            "SELECT height, native_state_root, snapshot_id, checkpoint_id, reputation_root,
                    rank, binding, section
             FROM ai_reputation_cursor WHERE market = ?1",
            params![&market.as_bytes()[..]],
            |row| {
                Ok((
                    row.get::<_, i64>(0)?,
                    row.get::<_, Vec<u8>>(1)?,
                    row.get::<_, Vec<u8>>(2)?,
                    row.get::<_, Vec<u8>>(3)?,
                    row.get::<_, Option<Vec<u8>>>(4)?,
                    row.get::<_, i64>(5)?,
                    row.get::<_, Vec<u8>>(6)?,
                    row.get::<_, Vec<u8>>(7)?,
                ))
            },
        )
        .optional()?;
    row.map(
        |(height, root, snapshot, checkpoint, reputation, rank, binding, section)| {
            Ok((
                FinalizedPoint {
                    height: loaded(height)?,
                    native_state_root: digest(&root)?,
                    snapshot_id: digest(&snapshot)?,
                    checkpoint_id: fixed(&checkpoint)?,
                    reputation_root: match reputation {
                        Some(bytes) => Presence::Present(digest(&bytes)?),
                        None => Presence::Absent,
                    },
                    rank: u8::try_from(rank)
                        .map_err(|_| ReputationError::Store(format!("stored rank {rank}")))?,
                    binding,
                },
                section,
            ))
        },
    )
    .transpose()
}

fn load_archive(
    connection: &Connection,
    market: MarketId,
    epoch: u64,
) -> Result<Option<ArchiveLocator>, ReputationError> {
    let row = connection
        .query_row(
            "SELECT height, native_state_root, checkpoint_id, snapshot_id
             FROM ai_reputation_archive WHERE market = ?1 AND epoch = ?2",
            params![&market.as_bytes()[..], stored(epoch)?],
            |row| {
                Ok((
                    row.get::<_, i64>(0)?,
                    row.get::<_, Vec<u8>>(1)?,
                    row.get::<_, Vec<u8>>(2)?,
                    row.get::<_, Vec<u8>>(3)?,
                ))
            },
        )
        .optional()?;
    row.map(|(height, root, checkpoint, snapshot)| {
        Ok(ArchiveLocator {
            epoch,
            finalized_height: loaded(height)?,
            native_state_root: digest(&root)?,
            checkpoint_id: fixed(&checkpoint)?,
            snapshot_id: digest(&snapshot)?,
        })
    })
    .transpose()
}

impl ReputationProjector {
    /// Opens (or recovers) the durable projection at `path` for one program's markets,
    /// verifying certificates against the declared settlement `domain`.
    ///
    /// # Errors
    /// `Certificate(Configuration)` for an undeclared domain; `Store` when the store fails.
    pub fn open(
        path: &Path,
        domain: &str,
        chain: ChainDomain,
        program: ProgramId,
    ) -> Result<Self, ReputationError> {
        declared_domain(domain)
            .map_err(|error| ReputationError::Certificate(CheckpointError::Configuration(error)))?;
        let connection = Connection::open(path)?;
        connection.pragma_update(None, "journal_mode", "WAL")?;
        connection.pragma_update(None, "synchronous", "FULL")?;
        connection.execute_batch(SCHEMA)?;
        Ok(Self {
            connection,
            domain: domain.to_owned(),
            chain,
            program,
        })
    }

    /// Admits one certified capture, advancing the market's projection monotonically.
    ///
    /// # Errors
    /// `Refused(WRONG_DOMAIN)` for another chain or program; `Certificate` for a certificate
    /// that fails threshold verification; `Refused(F07_BINDING_MISMATCH)` when the certified
    /// root or batch differs from the read; `Capture` or `Refused` for an inconsistent capture,
    /// state or F07 section; `InvalidProof` when the state commits to a later height;
    /// `StaleProof` below the accepted height; `Conflict` for another root at the accepted
    /// height or a halted market; `Store` when the store fails.
    pub fn admit(&mut self, read: &FinalizedRead<'_>) -> Result<Admission, ReputationError> {
        if read.proof.chain != self.chain || read.proof.program != self.program {
            return Err(ReputationError::Refused(WRONG_DOMAIN));
        }
        let report = verify_declared_certificate(
            read.certificate,
            &self.domain,
            &read.registered_checkpoint_id,
            None,
        )
        .map_err(ReputationError::Certificate)?;
        if report.resulting_state_root() != read.proof.native_state_root.bytes()
            || report.batch_number() != read.proof.execution_height
        {
            return Err(ReputationError::Refused(F07_BINDING_MISMATCH));
        }
        let checkpoint_id = report
            .evidence()
            .checkpoint_id()
            .ok_or(ReputationError::Refused(F07_FINALITY_UNAVAILABLE))?;
        let mut buffer = vec![0; MAX_STATE_BYTES];
        let mut capture = StateCapture::new(&mut buffer);
        for chunk in read.chunks {
            capture.accept(&read.proof, chunk)?;
        }
        let (state_bytes, facts) = capture.finish()?;
        let binding = bind_snapshot(
            state_bytes,
            &facts,
            &FinalityEvidence {
                native_state_root: read.proof.native_state_root,
                checkpoint: Digest32::new(checkpoint_id)?,
                settlement: Presence::Absent,
                rank: report.level().wire_rank(),
            },
            read.publication_time_ms,
        )?;
        binding.require_finalized()?;
        let shared = decode_shared_state(state_bytes)?;
        let section = reputation_region(shared.section(Section::ReputationAdmission)?);
        let state = decode_section(section)?;
        if state.market != binding.market {
            return Err(ReputationError::Refused(F07_BINDING_MISMATCH));
        }
        let height = binding.execution_height;
        if let Some(committed) = committed_height(&state).filter(|c| *c > height) {
            return Err(ReputationError::InvalidProof {
                finalized_height: height,
                committed_height: committed,
            });
        }
        let records: Vec<ReputationCurrent> = state.records().copied().collect();
        let reputation = match state.latest_completed() {
            Presence::Present(epoch) => Some(reputation_root(state.market, epoch, &records)?),
            Presence::Absent => None,
        };
        let mut encoded = [0; BINDING_MAX_BYTES];
        let encoded_len = binding.encode(&mut encoded)?;
        let point = FinalizedPoint {
            height,
            native_state_root: binding.native_state_root,
            snapshot_id: binding.snapshot_id()?,
            checkpoint_id,
            reputation_root: reputation.map_or(Presence::Absent, Presence::Present),
            rank: binding.rank,
            binding: encoded
                .get(..encoded_len)
                .ok_or(ReputationError::Refused(ARITHMETIC))?
                .to_vec(),
        };
        self.record(state.market, &point, section, &state)
    }

    fn record(
        &mut self,
        market: MarketId,
        point: &FinalizedPoint,
        section: &[u8],
        state: &ReputationState,
    ) -> Result<Admission, ReputationError> {
        let transaction = self
            .connection
            .transaction_with_behavior(TransactionBehavior::Immediate)?;
        if let Some(conflict) = load_conflict(&transaction, market)? {
            return Err(ReputationError::Conflict(Box::new(conflict)));
        }
        match load_cursor(&transaction, market)? {
            Some((accepted, _)) if point.height < accepted.height => {
                return Err(ReputationError::StaleProof {
                    accepted: accepted.height,
                    offered: point.height,
                });
            }
            Some((accepted, _))
                if point.height == accepted.height
                    && point.native_state_root == accepted.native_state_root =>
            {
                return if point.snapshot_id == accepted.snapshot_id {
                    Ok(Admission::AlreadyAccepted {
                        height: point.height,
                    })
                } else {
                    Err(ReputationError::Capture(QueryError::SnapshotConflict))
                };
            }
            Some((accepted, _)) if point.height == accepted.height => {
                let conflict = RootConflict {
                    market,
                    height: point.height,
                    accepted_root: accepted.native_state_root,
                    conflicting_root: point.native_state_root,
                    conflicting_checkpoint: point.checkpoint_id,
                };
                transaction.execute(
                    "INSERT INTO ai_reputation_conflict(market, height, accepted_root,
                         conflicting_root, conflicting_checkpoint)
                     VALUES (?1, ?2, ?3, ?4, ?5)",
                    params![
                        &market.as_bytes()[..],
                        stored(point.height)?,
                        &conflict.accepted_root.as_bytes()[..],
                        &conflict.conflicting_root.as_bytes()[..],
                        &conflict.conflicting_checkpoint[..],
                    ],
                )?;
                transaction.commit()?;
                return Err(ReputationError::Conflict(Box::new(conflict)));
            }
            _ => {}
        }
        let reputation_root = match point.reputation_root {
            Presence::Present(root) => Some(root.bytes()),
            Presence::Absent => None,
        };
        transaction.execute(
            "INSERT INTO ai_reputation_cursor(market, height, native_state_root, snapshot_id,
                 checkpoint_id, reputation_root, rank, binding, section)
             VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?9)
             ON CONFLICT(market) DO UPDATE SET height = excluded.height,
                 native_state_root = excluded.native_state_root,
                 snapshot_id = excluded.snapshot_id, checkpoint_id = excluded.checkpoint_id,
                 reputation_root = excluded.reputation_root, rank = excluded.rank,
                 binding = excluded.binding, section = excluded.section",
            params![
                &market.as_bytes()[..],
                stored(point.height)?,
                &point.native_state_root.as_bytes()[..],
                &point.snapshot_id.as_bytes()[..],
                &point.checkpoint_id[..],
                reputation_root.as_ref().map(|root| &root[..]),
                i64::from(point.rank),
                &point.binding[..],
                section,
            ],
        )?;
        for row in state.completed() {
            transaction.execute(
                "INSERT OR IGNORE INTO ai_reputation_archive(market, epoch, row, height,
                     native_state_root, checkpoint_id, snapshot_id)
                 VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7)",
                params![
                    &market.as_bytes()[..],
                    stored(row.epoch)?,
                    &encode_history(row)?[..],
                    stored(point.height)?,
                    &point.native_state_root.as_bytes()[..],
                    &point.checkpoint_id[..],
                    &point.snapshot_id.as_bytes()[..],
                ],
            )?;
        }
        transaction.commit()?;
        Ok(Admission::Advanced {
            height: point.height,
        })
    }

    /// `ReadHistory` over the accepted finalized projection.
    ///
    /// # Errors
    /// `Conflict` for a halted market; `Refused(F07_FINALITY_UNAVAILABLE)` without an accepted
    /// projection at `minimum_finalized_height`; `Refused(F07_UNKNOWN_WORKER)`;
    /// `Refused(F07_EPOCH_NOT_SEALED)` for an epoch after the latest completion;
    /// `HistoryOutsideRetention` for a completed epoch no longer retained; `InvalidProof`
    /// when the finalized height precedes the worker's observation; `Store` when the store fails.
    pub fn read_history(&self, request: &HistoryRequest) -> Result<HistoryView, ReputationError> {
        if let Some(conflict) = load_conflict(&self.connection, request.market)? {
            return Err(ReputationError::Conflict(Box::new(conflict)));
        }
        let (finalized, section) = load_cursor(&self.connection, request.market)?
            .filter(|(point, _)| point.height >= request.minimum_finalized_height)
            .ok_or(ReputationError::Refused(F07_FINALITY_UNAVAILABLE))?;
        let state = decode_section(&section)?;
        let current = *state
            .records()
            .find(|record| record.worker == request.worker)
            .ok_or(ReputationError::Refused(F07_UNKNOWN_WORKER))?;
        let freshness = freshness(finalized.height, &current)?;
        let history = match request.epoch {
            None => state.completed().copied().collect(),
            Some(epoch) => match state.lookup_epoch(epoch) {
                Ok(row) => vec![row],
                Err(HistoryLookupError::HistoryOutsideRetention) => {
                    return Err(match state.latest_completed() {
                        Presence::Present(latest) if epoch <= latest => {
                            ReputationError::HistoryOutsideRetention {
                                oldest_retained: state.completed().next().map(|row| row.epoch),
                                archive: load_archive(&self.connection, request.market, epoch)?
                                    .map(Box::new),
                            }
                        }
                        _ => ReputationError::Refused(F07_EPOCH_NOT_SEALED),
                    });
                }
            },
        };
        Ok(HistoryView {
            finalized,
            current,
            freshness,
            history,
        })
    }
}
