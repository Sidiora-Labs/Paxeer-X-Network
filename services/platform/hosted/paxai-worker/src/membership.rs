//! F08 membership recovered from finalized authority before the worker executes anything.
//!
//! One durable record per worker keeps the last finalized observation, the retained
//! economic and replay context, the delegate generation history and at most one ambiguous
//! enrollment. A restart reloads the record but never readiness: until a new finalized
//! observation binds through [`FinalizedAuthority::bind`], the worker is NOT READY.
//!
//! Seam: no managed durable store is provisioned, so the record is one local file replaced
//! atomically (write, sync, rename, directory sync).
use crate::auth::ServiceError;
use crate::discovery::{AuthorityEvidence, FinalizedAuthority};
use layerx_programs_ai_market::{
    admission::{AdmissionMeta, AdmissionTable, Participant, PendingExit},
    errors::{ApplicationError, ARITHMETIC, NON_CANONICAL},
    rewards::{decode_reward_state, Disposition, EpochStatus, REWARD_STATE_BYTES},
    state::{self, ActorSlot, RetainedResult, Section, SharedState},
    types::{
        Amount, ChainDomain, Digest32, MarketId, ProgramId, PublicKey32, RequestDigest, RequestId,
        ResultDigest, WorkerId,
    },
    workers::{WorkerCurrent, WorkerState},
};
use std::fmt;
use std::fs::{self, File};
use std::io::{self, Write};
use std::path::{Path, PathBuf};

const MAGIC: &[u8; 8] = b"PXMEMB01";
const RECORD_FILE: &str = "membership.record";
/// Bound on the retained delegate generation history; a longer history refuses rather than
/// forgetting a generation that old evidence may still name.
pub const MAX_GENERATIONS: usize = 1_024;

/// The one worker a membership record belongs to.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct Subject {
    pub chain: ChainDomain,
    pub program: ProgramId,
    pub market: MarketId,
    pub worker: WorkerId,
}
impl Subject {
    fn bytes(&self) -> [[u8; 32]; 4] {
        [
            self.chain.bytes(),
            self.program.bytes(),
            self.market.bytes(),
            self.worker.bytes(),
        ]
    }
}

/// The finalized height and content identity of the last bound observation.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct Observed {
    pub height: u64,
    pub snapshot: Digest32,
}

/// Obligations that outlive membership: unclaimed terminal F06 entitlements of the worker
/// and the unexpired result its replay slot still retains.
#[derive(Clone, Copy, Debug, Default, Eq, PartialEq)]
pub struct Retained {
    pub entitlements: u16,
    pub amount: Amount,
    pub unresolved: Option<RetainedResult>,
}

/// One delegate generation the worker has served under, kept after rotation or revocation
/// so evidence signed under it stays attributable.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct DelegateGeneration {
    pub generation: u64,
    pub key_version: u64,
    pub delegate: PublicKey32,
    pub first_height: u64,
    pub superseded_height: Option<u64>,
    pub revoked: bool,
}

/// An enrollment submitted without a known outcome: the approval it consumes and the
/// request identity of the original journaled submission.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct PendingEnrollment {
    pub approval: Digest32,
    pub request: RequestId,
}

/// How an ambiguous enrollment resolves against finalized membership.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum EnrollmentResolution {
    /// Finalized membership shows the worker admitted.
    Admitted,
    /// The approval is still unconsumed and unexpired: resend the original request
    /// byte for byte, never a new enrollment.
    ResendOriginal { request: RequestId },
    /// The approval was replaced, expired or discarded; the original can never apply.
    Lapsed,
}

/// The durable part of the observer.
#[derive(Clone, Debug, Default, Eq, PartialEq)]
pub struct MembershipRecord {
    pub observed: Option<Observed>,
    pub retained: Retained,
    pub generations: Vec<DelegateGeneration>,
    pub enrollment: Option<PendingEnrollment>,
}

/// F08 standing of the worker at one finalized height.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum Standing {
    Absent,
    Approved {
        effective_epoch: u64,
        expiry_height: u64,
    },
    Staged {
        effective_epoch: u64,
    },
    Active {
        admitted_epoch: u64,
    },
    Draining {
        admitted_epoch: Option<u64>,
        exit: Option<PendingExit>,
    },
    Revoked {
        exit: Option<PendingExit>,
    },
}
impl Standing {
    /// Standing of one F08 member record, or `Absent` without one.
    #[must_use]
    pub fn of(meta: Option<&AdmissionMeta>) -> Self {
        let Some(meta) = meta else {
            return Self::Absent;
        };
        if let (false, Some(approval)) = (meta.admitted(), meta.approval) {
            return Self::Approved {
                effective_epoch: approval.effective_epoch,
                expiry_height: approval.expiry_height,
            };
        }
        if meta.revoked() {
            Self::Revoked {
                exit: meta.pending_exit,
            }
        } else if meta.draining() || meta.pending_exit.is_some() {
            Self::Draining {
                admitted_epoch: meta.admitted_epoch,
                exit: meta.pending_exit,
            }
        } else {
            match meta.admitted_epoch {
                Some(admitted_epoch) => Self::Active { admitted_epoch },
                None => Self::Staged {
                    effective_epoch: meta.immunity_until_epoch,
                },
            }
        }
    }
}

/// The current finalized view: authority, F08 member record and retained obligations.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct FinalizedMembership {
    pub authority: FinalizedAuthority,
    pub meta: Option<AdmissionMeta>,
    pub retained: Retained,
}
impl FinalizedMembership {
    #[must_use]
    pub fn standing(&self) -> Standing {
        Standing::of(self.meta.as_ref())
    }
}

/// Observer refusals: a service refusal, a journal failure or a record that is not this
/// worker's canonical record.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum MembershipError {
    Service(ServiceError),
    Journal(io::ErrorKind),
    CorruptRecord,
}
impl fmt::Display for MembershipError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::Service(error) => error.fmt(f),
            Self::Journal(kind) => write!(f, "membership journal failure: {kind}"),
            Self::CorruptRecord => f.write_str("membership record is corrupt or foreign"),
        }
    }
}
impl std::error::Error for MembershipError {}
impl From<ServiceError> for MembershipError {
    fn from(error: ServiceError) -> Self {
        Self::Service(error)
    }
}
impl From<ApplicationError> for MembershipError {
    fn from(error: ApplicationError) -> Self {
        Self::Service(error.into())
    }
}
impl From<io::Error> for MembershipError {
    fn from(error: io::Error) -> Self {
        Self::Journal(error.kind())
    }
}

/// Recovers and tracks one worker's finalized F08 membership.
#[derive(Debug)]
pub struct MembershipObserver {
    path: PathBuf,
    subject: Subject,
    record: MembershipRecord,
    current: Option<FinalizedMembership>,
}
impl MembershipObserver {
    /// Opens the record in `directory`, creating neither readiness nor a record: the worker
    /// stays NOT READY until [`Self::observe`] binds finalized authority.
    ///
    /// # Errors
    /// `Journal` for an unreadable directory or file; `CorruptRecord` for a record of
    /// another worker or one that does not decode exactly.
    pub fn open(directory: &Path, subject: Subject) -> Result<Self, MembershipError> {
        fs::create_dir_all(directory)?;
        let path = directory.join(RECORD_FILE);
        let record = match fs::read(&path) {
            Ok(bytes) => decode(&bytes, &subject)?,
            Err(error) if error.kind() == io::ErrorKind::NotFound => MembershipRecord::default(),
            Err(error) => return Err(error.into()),
        };
        Ok(Self {
            path,
            subject,
            record,
            current: None,
        })
    }

    #[must_use]
    pub const fn record(&self) -> &MembershipRecord {
        &self.record
    }
    #[must_use]
    pub const fn current(&self) -> Option<&FinalizedMembership> {
        self.current.as_ref()
    }

    /// Binds one finalized observation, records it durably and only then makes it current.
    /// Any refusal, including absent evidence, leaves the worker NOT READY and the durable
    /// record unchanged.
    ///
    /// # Errors
    /// `StaleAuthority` for absent evidence, a binding refusal of
    /// [`FinalizedAuthority::bind`], an observation below the recorded height, another
    /// snapshot at the recorded height or a regressing delegate generation; `NotFound` once
    /// the worker left the market; `CapacityExceeded` above [`MAX_GENERATIONS`]; state
    /// decoding refusals; `Journal` when the record cannot be written.
    pub fn observe(
        &mut self,
        evidence: Option<&AuthorityEvidence<'_>>,
    ) -> Result<Standing, MembershipError> {
        self.current = None;
        let evidence = evidence.ok_or(ServiceError::StaleAuthority)?;
        let subject = self.subject;
        let authority = FinalizedAuthority::bind(
            evidence,
            subject.chain,
            subject.program,
            subject.market,
            subject.worker,
        )?;
        let observed = Observed {
            height: authority.height(),
            snapshot: authority.snapshot().snapshot_id()?,
        };
        if let Some(last) = self.record.observed {
            if observed.height < last.height
                || (observed.height == last.height && observed.snapshot != last.snapshot)
            {
                return Err(ServiceError::StaleAuthority.into());
            }
        }
        let shared = state::decode_shared_state(evidence.state)?;
        let meta = AdmissionTable::decode(shared.section(Section::ReputationAdmission)?)?
            .get(Participant::Worker(subject.worker));
        let record = authority.record();
        let retained = retained(&shared, subject.worker, record.slot, observed.height)?;
        let mut next = self.record.clone();
        next.observed = Some(observed);
        next.retained = retained;
        advance(&mut next.generations, record, observed.height)?;
        if next != self.record {
            self.store(next)?;
        }
        let membership = FinalizedMembership {
            authority,
            meta,
            retained,
        };
        self.current = Some(membership);
        Ok(membership.standing())
    }

    /// Readiness for NEW work: a current finalized observation whose authority passes the
    /// full admission gate, including current non-draining, non-revoked F08 membership.
    ///
    /// # Errors
    /// `StaleAuthority` without a current observation; every
    /// [`FinalizedAuthority::admission_gate`] refusal.
    pub fn new_work(&self) -> Result<&FinalizedAuthority, ServiceError> {
        let current = self.current.as_ref().ok_or(ServiceError::StaleAuthority)?;
        current.authority.admission_gate()?;
        Ok(&current.authority)
    }

    /// Readiness to finish or release already admitted work: draining members may, revoked
    /// ones may not.
    ///
    /// # Errors
    /// `StaleAuthority` without a current observation; `IdentityFrozen`; `DelegateRevoked`
    /// for a revoked F02 record or revoked F08 membership.
    pub fn admitted_work(&self) -> Result<&FinalizedAuthority, ServiceError> {
        let current = self.current.as_ref().ok_or(ServiceError::StaleAuthority)?;
        current.authority.release_gate()?;
        if current.meta.is_some_and(|meta| meta.revoked()) {
            return Err(ServiceError::DelegateRevoked);
        }
        Ok(&current.authority)
    }

    /// The recorded generation that signed old evidence, current or not, revoked or not.
    ///
    /// # Errors
    /// `WrongGeneration` when no recorded generation has exactly these versions and key.
    pub fn attribute(
        &self,
        generation: u64,
        key_version: u64,
        delegate: PublicKey32,
    ) -> Result<DelegateGeneration, ServiceError> {
        self.record
            .generations
            .iter()
            .find(|g| {
                (g.generation, g.key_version, g.delegate) == (generation, key_version, delegate)
            })
            .copied()
            .ok_or(ServiceError::WrongGeneration)
    }

    /// Journals an enrollment before its submission; the same one again is a no-op.
    ///
    /// # Errors
    /// `IdempotencyConflict` while a different enrollment is unresolved; `Journal`.
    pub fn record_enrollment(&mut self, pending: PendingEnrollment) -> Result<(), MembershipError> {
        match self.record.enrollment {
            Some(existing) if existing == pending => Ok(()),
            Some(_) => Err(ServiceError::IdempotencyConflict.into()),
            None => {
                let mut next = self.record.clone();
                next.enrollment = Some(pending);
                self.store(next)
            }
        }
    }

    /// Resolves the journaled enrollment by its original identity against the current
    /// finalized membership; a final outcome clears it durably.
    ///
    /// # Errors
    /// `NotFound` without a journaled enrollment; `StaleAuthority` without a current
    /// observation; `Journal`.
    pub fn reconcile_enrollment(&mut self) -> Result<EnrollmentResolution, MembershipError> {
        let pending = self.record.enrollment.ok_or(ServiceError::NotFound)?;
        let current = self.current.as_ref().ok_or(ServiceError::StaleAuthority)?;
        let height = current.authority.height();
        let resolution = match current.meta {
            Some(meta) if meta.admitted() => EnrollmentResolution::Admitted,
            Some(AdmissionMeta {
                approval: Some(approval),
                ..
            }) if approval.digest == pending.approval && height < approval.expiry_height => {
                EnrollmentResolution::ResendOriginal {
                    request: pending.request,
                }
            }
            _ => EnrollmentResolution::Lapsed,
        };
        if !matches!(resolution, EnrollmentResolution::ResendOriginal { .. }) {
            let mut next = self.record.clone();
            next.enrollment = None;
            self.store(next)?;
        }
        Ok(resolution)
    }

    /// Replaces the record on disk atomically, then in memory.
    fn store(&mut self, next: MembershipRecord) -> Result<(), MembershipError> {
        let bytes = encode(&next, &self.subject)?;
        let partial = self.path.with_extension("partial");
        let mut file = File::create(&partial)?;
        file.write_all(&bytes)?;
        file.sync_all()?;
        fs::rename(&partial, &self.path)?;
        if let Some(directory) = self.path.parent() {
            File::open(directory)?.sync_all()?;
        }
        self.record = next;
        Ok(())
    }
}

/// Unclaimed TERMINAL F06 entitlements of `worker` and its slot's unexpired replay result.
fn retained(
    shared: &SharedState<'_>,
    worker: WorkerId,
    slot: u8,
    height: u64,
) -> Result<Retained, ApplicationError> {
    let mut retained = Retained::default();
    let section = shared.section(Section::SettlementClaims)?;
    if !section.is_empty() {
        let rewards = decode_reward_state(section.get(..REWARD_STATE_BYTES).ok_or(NON_CANONICAL)?)?;
        let dictionary = rewards.dictionary();
        for row in rewards.rows().records() {
            let row = row?;
            if row.status != EpochStatus::Terminal {
                continue;
            }
            for entry in row.entries() {
                if entry.disposition == Disposition::Unclaimed
                    && entry.entitlement > 0
                    && dictionary.slot(entry.slot)?.worker == worker
                {
                    retained.entitlements =
                        retained.entitlements.checked_add(1).ok_or(ARITHMETIC)?;
                    retained.amount = retained
                        .amount
                        .checked_add(entry.entitlement)
                        .ok_or(ARITHMETIC)?;
                }
            }
        }
    }
    retained.unresolved = shared
        .control
        .replay
        .actor(ActorSlot::worker(usize::from(slot))?)
        .and_then(|actor| actor.last)
        .filter(|last| last.expiry_height > height);
    Ok(retained)
}

/// Appends the record's generation when it changed; revocation is sticky.
fn advance(
    history: &mut Vec<DelegateGeneration>,
    record: &WorkerCurrent,
    height: u64,
) -> Result<(), ServiceError> {
    let revoked = record.state == WorkerState::Revoked;
    let full = history.len() >= MAX_GENERATIONS;
    if let Some(last) = history.last_mut() {
        if (last.generation, last.key_version, last.delegate)
            == (record.generation, record.key_version, record.delegate)
        {
            last.revoked |= revoked;
            return Ok(());
        }
        if record.generation < last.generation
            || record.key_version < last.key_version
            || (record.generation, record.key_version) == (last.generation, last.key_version)
        {
            return Err(ServiceError::StaleAuthority);
        }
        if full {
            return Err(ServiceError::CapacityExceeded);
        }
        last.superseded_height = Some(height);
    }
    history.push(DelegateGeneration {
        generation: record.generation,
        key_version: record.key_version,
        delegate: record.delegate,
        first_height: height,
        superseded_height: None,
        revoked,
    });
    Ok(())
}

fn flag(out: &mut Vec<u8>, present: bool) {
    out.push(u8::from(present));
}

fn encode(record: &MembershipRecord, subject: &Subject) -> Result<Vec<u8>, MembershipError> {
    let mut out = MAGIC.to_vec();
    for id in subject.bytes() {
        out.extend_from_slice(&id);
    }
    flag(&mut out, record.observed.is_some());
    if let Some(observed) = record.observed {
        out.extend_from_slice(&observed.height.to_be_bytes());
        out.extend_from_slice(observed.snapshot.as_bytes());
    }
    let retained = record.retained;
    out.extend_from_slice(&retained.entitlements.to_be_bytes());
    out.extend_from_slice(&retained.amount.to_be_bytes());
    flag(&mut out, retained.unresolved.is_some());
    if let Some(result) = retained.unresolved {
        out.extend_from_slice(&result.sequence.to_be_bytes());
        out.extend_from_slice(result.request_id.as_bytes());
        out.extend_from_slice(result.request_digest.as_bytes());
        out.extend_from_slice(result.result_digest.as_bytes());
        out.extend_from_slice(&result.applied_revision.to_be_bytes());
        out.extend_from_slice(&result.expiry_height.to_be_bytes());
    }
    flag(&mut out, record.enrollment.is_some());
    if let Some(pending) = record.enrollment {
        out.extend_from_slice(pending.approval.as_bytes());
        out.extend_from_slice(pending.request.as_bytes());
    }
    let count = u16::try_from(record.generations.len())
        .map_err(|_| MembershipError::Service(ServiceError::CapacityExceeded))?;
    out.extend_from_slice(&count.to_be_bytes());
    for generation in &record.generations {
        out.extend_from_slice(&generation.generation.to_be_bytes());
        out.extend_from_slice(&generation.key_version.to_be_bytes());
        out.extend_from_slice(&generation.delegate.0);
        out.extend_from_slice(&generation.first_height.to_be_bytes());
        flag(&mut out, generation.superseded_height.is_some());
        if let Some(height) = generation.superseded_height {
            out.extend_from_slice(&height.to_be_bytes());
        }
        flag(&mut out, generation.revoked);
    }
    Ok(out)
}

struct Reader<'a>(&'a [u8]);
impl Reader<'_> {
    fn take<const N: usize>(&mut self) -> Result<[u8; N], MembershipError> {
        let (head, rest) = self
            .0
            .split_first_chunk::<N>()
            .ok_or(MembershipError::CorruptRecord)?;
        self.0 = rest;
        Ok(*head)
    }
    fn flag(&mut self) -> Result<bool, MembershipError> {
        match self.take::<1>()? {
            [0] => Ok(false),
            [1] => Ok(true),
            _ => Err(MembershipError::CorruptRecord),
        }
    }
    fn u16(&mut self) -> Result<u16, MembershipError> {
        Ok(u16::from_be_bytes(self.take()?))
    }
    fn u64(&mut self) -> Result<u64, MembershipError> {
        Ok(u64::from_be_bytes(self.take()?))
    }
    fn u128(&mut self) -> Result<u128, MembershipError> {
        Ok(u128::from_be_bytes(self.take()?))
    }
    fn id<T>(
        &mut self,
        make: fn([u8; 32]) -> Result<T, ApplicationError>,
    ) -> Result<T, MembershipError> {
        make(self.take()?).map_err(|_| MembershipError::CorruptRecord)
    }
}

fn decode(bytes: &[u8], subject: &Subject) -> Result<MembershipRecord, MembershipError> {
    let mut r = Reader(bytes);
    if &r.take::<8>()? != MAGIC {
        return Err(MembershipError::CorruptRecord);
    }
    for id in subject.bytes() {
        if r.take::<32>()? != id {
            return Err(MembershipError::CorruptRecord);
        }
    }
    let observed = if r.flag()? {
        Some(Observed {
            height: r.u64()?,
            snapshot: r.id(Digest32::new)?,
        })
    } else {
        None
    };
    let entitlements = r.u16()?;
    let amount = r.u128()?;
    let unresolved = if r.flag()? {
        Some(RetainedResult {
            sequence: r.u64()?,
            request_id: r.id(RequestId::new)?,
            request_digest: r.id(RequestDigest::new)?,
            result_digest: r.id(ResultDigest::new)?,
            applied_revision: r.u64()?,
            expiry_height: r.u64()?,
        })
    } else {
        None
    };
    let enrollment = if r.flag()? {
        Some(PendingEnrollment {
            approval: r.id(Digest32::new)?,
            request: r.id(RequestId::new)?,
        })
    } else {
        None
    };
    let count = usize::from(r.u16()?);
    if count > MAX_GENERATIONS {
        return Err(MembershipError::CorruptRecord);
    }
    let mut generations = Vec::with_capacity(count);
    for _ in 0..count {
        generations.push(DelegateGeneration {
            generation: r.u64()?,
            key_version: r.u64()?,
            delegate: PublicKey32(r.take()?),
            first_height: r.u64()?,
            superseded_height: if r.flag()? { Some(r.u64()?) } else { None },
            revoked: r.flag()?,
        });
    }
    if !r.0.is_empty() {
        return Err(MembershipError::CorruptRecord);
    }
    Ok(MembershipRecord {
        observed,
        retained: Retained {
            entitlements,
            amount,
            unresolved,
        },
        generations,
        enrollment,
    })
}
