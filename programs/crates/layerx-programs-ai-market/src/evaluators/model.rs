//! F03 value representations only: nomination is not accepted F08 membership.
use crate::{
    codec::{derive_evaluator, ReportBody},
    errors::*,
    types::*,
};

pub const GRANT_BYTES: usize = 161;
pub const CONSENT_BYTES: usize = 362;
pub const SIGNED_CONSENT_BYTES: usize = 426;
pub const SIGNED_REPORT_MIN_BYTES: usize = 328;
pub const SIGNED_REPORT_MAX_BYTES: usize = 1444;
pub const MAX_GRANT_EPOCHS: u64 = 32;

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
#[repr(u8)]
pub enum GrantStatus {
    Pending = 1,
    Active = 2,
    Revoked = 3,
    Expired = 4,
}
impl GrantStatus {
    pub fn decode(value: u8) -> CodecResult<Self> {
        match value {
            1 => Ok(Self::Pending),
            2 => Ok(Self::Active),
            3 => Ok(Self::Revoked),
            4 => Ok(Self::Expired),
            _ => Err(NON_CANONICAL),
        }
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct EvaluatorGrant {
    pub evaluator: EvaluatorId,
    pub principal: PrincipalId,
    pub rubric: RubricDigest,
    pub grant_version: Version,
    pub key_version: Version,
    pub signing_key: PublicKey32,
    pub effective_epoch: u64,
    pub expiry_epoch_exclusive: u64,
    pub status: GrantStatus,
}
impl EvaluatorGrant {
    pub fn validate(&self) -> CodecResult<()> {
        nonzero_key(self.signing_key)?;
        let span = self
            .expiry_epoch_exclusive
            .checked_sub(self.effective_epoch)
            .ok_or(F03_BAD_ACTIVATION)?;
        if span == 0 || span > MAX_GRANT_EPOCHS {
            return Err(F03_BAD_ACTIVATION);
        }
        Ok(())
    }
    /// Constructs only an Owner nomination, without activating or accepting it.
    pub fn nominate(
        market: MarketId,
        principal: PrincipalId,
        nonce: [u8; 32],
        rubric: RubricDigest,
        grant_version: Version,
        key_version: Version,
        signing_key: PublicKey32,
        effective_epoch: u64,
        expiry_epoch_exclusive: u64,
    ) -> CodecResult<Self> {
        let value = Self {
            evaluator: derive_evaluator(market, principal, nonce)?,
            principal,
            rubric,
            grant_version,
            key_version,
            signing_key,
            effective_epoch,
            expiry_epoch_exclusive,
            status: GrantStatus::Pending,
        };
        value.validate()?;
        Ok(value)
    }
}
pub fn default_expiry(effective_epoch: u64) -> CodecResult<u64> {
    effective_epoch
        .checked_add(MAX_GRANT_EPOCHS)
        .ok_or(ARITHMETIC)
}
pub fn nonzero_key(key: PublicKey32) -> CodecResult<()> {
    if key.0 == [0; 32] {
        Err(NON_CANONICAL)
    } else {
        Ok(())
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct SignedReport<'a> {
    pub body: ReportBody<'a>,
    pub signature: Signature64,
}

/// A compact reference to a sealed F09 registration, not the remote manifest.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct RegisteredEvidence {
    pub binding: EvaluatorBinding,
    pub root: EvidenceRoot,
    pub rubric: RubricDigest,
}

/// Borrowed frozen facts for pure statement validation. The caller must obtain
/// them from authoritative state; this value does not authenticate that state,
/// establish F08 membership, consume a commit or expose a direct-report operation.
#[derive(Clone, Copy, Debug)]
pub struct ReportContext<'a> {
    pub binding: EvaluatorBinding,
    pub frozen_grant: EvaluatorGrant,
    pub live_grant: EvaluatorGrant,
    pub approved_rubric: RubricDigest,
    pub market_owner: PrincipalId,
    pub workers: &'a [WorkerRosterEntry],
    pub evidence: Presence<RegisteredEvidence>,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct EvaluatorAdmissionConsentV1 {
    pub chain: ChainDomain,
    pub program: ProgramId,
    pub market: MarketId,
    pub evaluator: EvaluatorId,
    pub owner: PrincipalId,
    pub delegate_key: PublicKey32,
    pub enrollment_nonce: [u8; 32],
    pub rubric: RubricDigest,
    pub approval: Digest32,
    pub request: RequestId,
    pub grant_version: Version,
    pub key_version: Version,
    pub effective_epoch: u64,
    pub config_version: Version,
    pub expiry_height: u64,
}
impl EvaluatorAdmissionConsentV1 {
    pub fn validate(&self) -> CodecResult<()> {
        nonzero_key(self.delegate_key)?;
        if self.expiry_height == 0 {
            return Err(NON_CANONICAL);
        }
        if derive_evaluator(self.market, self.owner, self.enrollment_nonce)? != self.evaluator {
            return Err(F08_BAD_CONSENT);
        }
        Ok(())
    }
}
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct SignedEvaluatorConsent {
    pub consent: EvaluatorAdmissionConsentV1,
    pub signature: Signature64,
}
/// Exact expected permit/nomination statement. Equality does not authenticate
/// owner acceptance: F08 must separately enforce its actual kind0 Context.
#[derive(Clone, Copy, Debug)]
pub struct ConsentContext {
    pub expected: EvaluatorAdmissionConsentV1,
    pub nomination: EvaluatorGrant,
    pub executing_height: u64,
}

/// Host refusal remains distinguishable from typed application errors.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum VerificationError {
    Application(ApplicationError),
    #[cfg(target_arch = "wasm32")]
    Host(layerx_program_sdk::ProgramError),
}
impl From<ApplicationError> for VerificationError {
    fn from(error: ApplicationError) -> Self {
        Self::Application(error)
    }
}
