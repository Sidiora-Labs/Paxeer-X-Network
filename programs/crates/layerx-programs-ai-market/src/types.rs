//! Checked common identities, immutable bindings, and bounded records.
use crate::errors::{CodecResult, ARITHMETIC, F03_SCORE_RANGE, NON_CANONICAL};

macro_rules! id32 {
    ($($name:ident),+ $(,)?) => {$ (
        #[derive(Clone, Copy, Debug, Eq, PartialEq, Ord, PartialOrd, Hash)]
        pub struct $name([u8; 32]);
        impl $name {
            /// Builds the identity from its 32 bytes.
            ///
            /// # Errors
            /// Returns `NON_CANONICAL` when the bytes are all zero.
            pub fn new(bytes: [u8; 32]) -> CodecResult<Self> {
                if bytes == [0; 32] { Err(NON_CANONICAL) } else { Ok(Self(bytes)) }
            }
            pub const fn bytes(self) -> [u8; 32] { self.0 }
            pub const fn as_bytes(&self) -> &[u8; 32] { &self.0 }
        }
    )+};
}
id32!(
    PrincipalId,
    ProgramId,
    MarketId,
    WorkerId,
    EvaluatorId,
    AccountId,
    AssetId,
    RequestId,
    TaskId
);

#[derive(Clone, Copy, Debug, Eq, PartialEq, Ord, PartialOrd, Hash)]
pub struct Digest32([u8; 32]);
impl Digest32 {
    /// Builds the digest from its 32 bytes.
    ///
    /// # Errors
    /// Returns `NON_CANONICAL` when the bytes are all zero.
    pub fn new(bytes: [u8; 32]) -> CodecResult<Self> {
        if bytes == [0; 32] {
            Err(NON_CANONICAL)
        } else {
            Ok(Self(bytes))
        }
    }
    #[must_use]
    pub const fn bytes(self) -> [u8; 32] {
        self.0
    }
    #[must_use]
    pub const fn as_bytes(&self) -> &[u8; 32] {
        &self.0
    }
}
macro_rules! digest {
    ($($name:ident),+ $(,)?) => {$ (
        #[derive(Clone, Copy, Debug, Eq, PartialEq, Ord, PartialOrd)]
        pub struct $name(Digest32);
        impl $name {
            /// Builds the typed digest from its 32 bytes.
            ///
            /// # Errors
            /// Returns `NON_CANONICAL` when the bytes are all zero.
            pub fn new(bytes: [u8; 32]) -> CodecResult<Self> { Ok(Self(Digest32::new(bytes)?)) }
            pub const fn bytes(self) -> [u8; 32] { self.0.bytes() }
            pub const fn as_bytes(&self) -> &[u8; 32] { self.0.as_bytes() }
        }
    )+};
}
digest!(
    ChainDomain,
    RequestDigest,
    ReportDigest,
    AttestationDigest,
    CommitmentDigest,
    RosterDigest,
    ResultDigest,
    StateDigest,
    EvidenceRoot,
    MetadataDigest,
    RubricDigest,
    PolicyDigest
);

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum Presence<T> {
    Absent,
    Present(T),
}
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct PublicKey32(pub [u8; 32]);
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct Signature64(pub [u8; 64]);
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct Salt32([u8; 32]);
impl Salt32 {
    /// Builds the salt from its 32 bytes.
    ///
    /// # Errors
    /// Returns `F04_SALT_INVALID` when the bytes are all zero.
    pub fn new(bytes: [u8; 32]) -> CodecResult<Self> {
        if bytes == [0; 32] {
            Err(crate::errors::F04_SALT_INVALID)
        } else {
            Ok(Self(bytes))
        }
    }
    #[must_use]
    pub const fn bytes(self) -> [u8; 32] {
        self.0
    }
}
#[derive(Clone, Copy, Debug, Eq, PartialEq, Ord, PartialOrd)]
pub struct Version(u64);
impl Version {
    /// Builds a version from its value.
    ///
    /// # Errors
    /// Returns `NON_CANONICAL` when the value is zero.
    pub fn new(value: u64) -> CodecResult<Self> {
        if value == 0 {
            Err(NON_CANONICAL)
        } else {
            Ok(Self(value))
        }
    }
    #[must_use]
    pub const fn get(self) -> u64 {
        self.0
    }
    /// Returns the following version.
    ///
    /// # Errors
    /// Returns `ARITHMETIC` when the value is `u64::MAX`.
    pub fn next(self) -> CodecResult<Self> {
        Self::new(self.0.checked_add(1).ok_or(ARITHMETIC)?)
    }
}
#[derive(Clone, Copy, Debug, Eq, PartialEq, Ord, PartialOrd)]
pub struct Score(u32);
impl Score {
    /// Builds a score from its value.
    ///
    /// # Errors
    /// Returns `F03_SCORE_RANGE` when the value exceeds 1,000,000.
    pub fn new(value: u32) -> CodecResult<Self> {
        if value > 1_000_000 {
            Err(F03_SCORE_RANGE)
        } else {
            Ok(Self(value))
        }
    }
    #[must_use]
    pub const fn get(self) -> u32 {
        self.0
    }
}
pub type Amount = u128;

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct FrozenBinding {
    pub chain: ChainDomain,
    pub program: ProgramId,
    pub market: MarketId,
    pub epoch: u64,
    pub config: Version,
    pub roster: RosterDigest,
}
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct EvaluatorBinding {
    pub frozen: FrozenBinding,
    pub evaluator: EvaluatorId,
    pub grant: Version,
    pub key_version: Version,
}
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct WorkerRosterEntry {
    pub worker: WorkerId,
    pub owner: PrincipalId,
    pub recipient: AccountId,
    pub generation: Version,
    pub key_version: Version,
    pub public_key: PublicKey32,
    pub metadata: MetadataDigest,
}
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct EvaluatorRosterEntry {
    pub evaluator: EvaluatorId,
    pub owner: PrincipalId,
    pub grant: Version,
    pub key_version: Version,
    pub public_key: PublicKey32,
    pub rubric: RubricDigest,
}
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct ScoreEntry {
    pub worker: WorkerId,
    pub score: Score,
}
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum Authentication {
    Native,
    Delegate {
        key: PublicKey32,
        signature: Signature64,
    },
}

/// Replay assessment only; this does not authenticate callers or commit state.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct RoleReplay {
    pub sequence: u64,
    pub request: Presence<RequestId>,
    pub digest: Presence<RequestDigest>,
    pub result: Presence<ResultDigest>,
}
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum ReplayDecision {
    New,
    AlreadyApplied(ResultDigest),
}
impl RoleReplay {
    /// Classifies a role request against the recorded replay slot.
    ///
    /// # Errors
    /// Returns `NON_CANONICAL` when the stored slot is malformed; `SEQUENCE_CONSUMED` when the sequence is zero or already passed; `REPLAY_CONFLICT` when the current sequence is reused for a different request or digest; `ARITHMETIC` when the stored sequence cannot advance; `SEQUENCE_GAP` when the sequence skips ahead.
    pub fn assess(
        &self,
        sequence: u64,
        request: RequestId,
        digest: RequestDigest,
    ) -> CodecResult<ReplayDecision> {
        use crate::errors::{REPLAY_CONFLICT, SEQUENCE_CONSUMED, SEQUENCE_GAP};
        if self.sequence == 0 {
            if self.request != Presence::Absent
                || self.digest != Presence::Absent
                || self.result != Presence::Absent
            {
                return Err(NON_CANONICAL);
            }
        } else if !matches!(
            (self.request, self.digest, self.result),
            (
                Presence::Present(_),
                Presence::Present(_),
                Presence::Present(_)
            )
        ) {
            return Err(NON_CANONICAL);
        }
        if sequence == 0 || sequence < self.sequence {
            return Err(SEQUENCE_CONSUMED);
        }
        if sequence == self.sequence {
            if self.request == Presence::Present(request)
                && self.digest == Presence::Present(digest)
            {
                if let Presence::Present(result) = self.result {
                    return Ok(ReplayDecision::AlreadyApplied(result));
                }
            }
            return Err(REPLAY_CONFLICT);
        }
        if sequence != self.sequence.checked_add(1).ok_or(ARITHMETIC)? {
            return Err(SEQUENCE_GAP);
        }
        Ok(ReplayDecision::New)
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct EpochWindows {
    pub start: u64,
    pub commit: u64,
    pub reveal: u64,
    pub settlement: u64,
    pub end: u64,
}
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum EpochPhase {
    Before,
    Work,
    Commit,
    Reveal,
    Settlement,
    After,
}
impl EpochWindows {
    /// Computes the windows of `epoch` counted from `origin`.
    ///
    /// # Errors
    /// Returns `ARITHMETIC` when any window bound overflows `u64`.
    pub fn new(origin: u64, epoch: u64) -> CodecResult<Self> {
        let start = origin
            .checked_add(epoch.checked_mul(128).ok_or(ARITHMETIC)?)
            .ok_or(ARITHMETIC)?;
        Ok(Self {
            start,
            commit: start.checked_add(64).ok_or(ARITHMETIC)?,
            reveal: start.checked_add(80).ok_or(ARITHMETIC)?,
            settlement: start.checked_add(96).ok_or(ARITHMETIC)?,
            end: start.checked_add(128).ok_or(ARITHMETIC)?,
        })
    }
    #[must_use]
    pub const fn phase(self, height: u64) -> EpochPhase {
        if height < self.start {
            EpochPhase::Before
        } else if height < self.commit {
            EpochPhase::Work
        } else if height < self.reveal {
            EpochPhase::Commit
        } else if height < self.settlement {
            EpochPhase::Reveal
        } else if height < self.end {
            EpochPhase::Settlement
        } else {
            EpochPhase::After
        }
    }
}
