//! Prepare, sign, submit, track, and wait contract types.

use crate::identity::{
    strict_hex32, ActivityType, AgentDid, Asset, AuthorityRef, CapabilityId, ContractError,
    ExplicitSet,
};
use crate::{Amount, Sequence, TimestampSeconds};
use layerx_types::result::ResultCode;
use layerx_types::rung::Rung;

macro_rules! required_bytes {
    ($name:ident, $field:literal) => {
        required_bytes!($name, $field, Vec<u8>, |value: Vec<u8>| {
            if value.is_empty() {
                return Err(ContractError::Empty($field));
            }
            Ok(value)
        });
    };
    ($name:ident, $field:literal, $length:literal) => {
        required_bytes!($name, $field, [u8; $length], |value: Vec<u8>| {
            value
                .try_into()
                .map_err(|_| ContractError::Malformed($field))
        });
    };
    ($name:ident, $field:literal, $storage:ty, $construct:expr) => {
        #[derive(Clone, Debug, Eq, PartialEq)]
        pub struct $name($storage);

        impl $name {
            /// Constructs a required byte string in its canonical length.
            ///
            /// # Errors
            /// Returns [`ContractError::Empty`] for an empty variable-length value or
            /// [`ContractError::Malformed`] for an invalid fixed-length value.
            pub fn new(value: Vec<u8>) -> Result<Self, ContractError> {
                let construct: fn(Vec<u8>) -> Result<$storage, ContractError> = $construct;
                construct(value).map(Self)
            }

            #[must_use]
            pub fn as_bytes(&self) -> &[u8] {
                &self.0
            }
        }
    };
}

macro_rules! required_reference {
    ($name:ident, $field:literal) => {
        #[derive(Clone, Debug, Eq, Hash, Ord, PartialEq, PartialOrd)]
        pub struct $name(String);

        impl $name {
            /// Constructs a required non-empty reference.
            ///
            /// # Errors
            /// Returns [`ContractError::Empty`] for an empty reference.
            pub fn new(value: impl Into<String>) -> Result<Self, ContractError> {
                let value = value.into();
                if value.is_empty() {
                    return Err(ContractError::Empty($field));
                }
                Ok(Self(value))
            }

            #[must_use]
            pub fn as_str(&self) -> &str {
                &self.0
            }
        }
    };
}

required_bytes!(CanonicalBytes, "canonical_bytes");
required_bytes!(SigningPreimage, "signing_preimage");
required_bytes!(SignatureBytes, "signature", 65);

impl core::str::FromStr for SignatureBytes {
    type Err = ContractError;

    fn from_str(text: &str) -> Result<Self, Self::Err> {
        if text.len() != 130
            || !text
                .bytes()
                .all(|byte| matches!(byte, b'0'..=b'9' | b'a'..=b'f'))
        {
            return Err(ContractError::Malformed("signature"));
        }
        let mut signature = [0_u8; 65];
        signature[..32].copy_from_slice(&strict_hex32(&text[..64], "signature")?);
        signature[32..64].copy_from_slice(&strict_hex32(&text[64..128], "signature")?);
        signature[64] = u8::from_str_radix(&text[128..], 16)
            .map_err(|_| ContractError::Malformed("signature"))?;
        Ok(Self(signature))
    }
}

required_bytes!(PayloadBytes, "payload");
required_reference!(PreparationRef, "preparation_ref");
required_reference!(SubmissionRef, "submission_ref");
required_reference!(ReceiptRef, "receipt_ref");
required_reference!(IdempotencyRef, "idempotency_key");

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct TimestampBound {
    pub not_before: TimestampSeconds,
    pub not_after: TimestampSeconds,
}

impl TimestampBound {
    /// Validates a non-inverted bound.
    ///
    /// # Errors
    /// Returns [`ContractError::Zero`] when the end precedes the start.
    pub const fn validate(self) -> Result<Self, ContractError> {
        if self.not_after.0 < self.not_before.0 {
            return Err(ContractError::Zero("timestamp_bound"));
        }
        Ok(self)
    }
}

/// Complete request from which the daemon builds canonical unsigned bytes.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct PrepareRequest {
    /// Full protocol module and ordinal packed into the canonical u32 form.
    pub protocol_activity_type: u32,
    pub actor: AgentDid,
    pub authority: AuthorityRef,
    pub account_sequence: Sequence,
    pub timestamp_bound: TimestampBound,
    pub idempotency_key: IdempotencyRef,
    pub fee_limit: Amount,
    pub payload: PayloadBytes,
    pub payload_hash: [u8; 32],
}

/// A preparation request bound to one owner-issued capability record.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct PrepareWithCapability {
    pub request: PrepareRequest,
    pub capability_id: CapabilityId,
}

impl PrepareWithCapability {
    /// Validates the capability identifier and the request timestamp bound.
    ///
    /// # Errors
    /// Returns [`ContractError::Malformed`] for a non-canonical capability identifier or the
    /// timestamp bound refusal.
    pub fn validate(self) -> Result<Self, ContractError> {
        self.capability_id.to_bytes()?;
        self.request.timestamp_bound.validate()?;
        Ok(self)
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct DisclosedAmount {
    pub counterparty: AgentDid,
    pub amount: Amount,
}

/// Meaning decoded from, and digest-bound to, the returned canonical bytes.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Disclosure {
    pub canonical_digest: [u8; 32],
    pub activity_type: ActivityType,
    pub actor: AgentDid,
    pub authority: AuthorityRef,
    pub counterparties: ExplicitSet<AgentDid>,
    pub amounts: ExplicitSet<DisclosedAmount>,
    pub asset: Asset,
    pub fee_limit: Amount,
    pub expiry: TimestampSeconds,
    pub idempotency_key: IdempotencyRef,
}

/// Byte-exact preparation and its disclosure from the same decoded bytes.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Prepared {
    pub preparation_ref: PreparationRef,
    pub unsigned_canonical_bytes: CanonicalBytes,
    pub signing_preimage: SigningPreimage,
    pub disclosure: Disclosure,
    pub expiry: TimestampSeconds,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct SignRequest {
    pub preparation_ref: PreparationRef,
    pub signature: SignatureBytes,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct SubmitRequest {
    pub preparation_ref: PreparationRef,
    pub signature: SignatureBytes,
    pub approval_release_ref: Option<[u8; 32]>,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct TrackRequest {
    pub submission_ref: SubmissionRef,
}

/// Exactly the durable submission state machine. Executed carries a mandatory receipt.
#[derive(Clone, Debug, Eq, PartialEq)]
pub enum SubmissionState {
    Prepared,
    Signed,
    Queued,
    Submitted,
    Unknown,
    Executed { receipt_ref: ReceiptRef },
    Committed { receipt_ref: ReceiptRef },
    Failed { result: ResultCode },
    Expired,
}

impl SubmissionState {
    #[must_use]
    pub const fn name(&self) -> &'static str {
        match self {
            Self::Prepared => "prepared",
            Self::Signed => "signed",
            Self::Queued => "queued",
            Self::Submitted => "submitted",
            Self::Unknown => "unknown",
            Self::Executed { .. } => "executed",
            Self::Committed { .. } => "committed",
            Self::Failed { .. } => "failed",
            Self::Expired => "expired",
        }
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Transition {
    pub from: SubmissionState,
    pub to: SubmissionState,
    pub cause: String,
    pub at: TimestampSeconds,
}

impl Transition {
    /// Rejects a transition without a recorded cause.
    ///
    /// # Errors
    /// Returns [`ContractError::Empty`] when cause is empty.
    pub fn validate(self) -> Result<Self, ContractError> {
        if self.cause.is_empty() {
            return Err(ContractError::Empty("transition_cause"));
        }
        Ok(self)
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct EvidenceRef {
    pub kind: String,
    pub digest: [u8; 32],
}

/// Current state and only the rung actually justified by attached evidence.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct TrackedSubmission {
    pub submission_ref: SubmissionRef,
    pub state: SubmissionState,
    pub evidence: Vec<EvidenceRef>,
    pub rung: Rung,
    pub transitions: Vec<Transition>,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct WaitRequest {
    pub submission_ref: SubmissionRef,
    pub requested_rung: Rung,
    pub deadline: TimestampSeconds,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct WaitResult {
    pub submission: TrackedSubmission,
    pub actual_rung: Rung,
    pub deadline_elapsed: bool,
}
