//! Exact signed and unsigned protocol activity envelopes.

use crate::amount::Amount;
use crate::ids::{Did, IdempotencyKey};
use crate::limits::{IDENTIFIER_BYTES, MAX_AUTHORITY_BYTES, MAX_SIGNATURE_BYTES};
use crate::payload::{ActivityType, Payload};

/// The exact ordered protocol field names for a signed activity envelope.
pub const ENVELOPE_FIELDS: [&str; 12] = [
    "protocol_version",
    "network_id",
    "activity_type",
    "actor_did",
    "authority",
    "account_sequence",
    "timestamp_bound",
    "idempotency_key",
    "fee_limit",
    "payload_hash",
    "payload",
    "signature",
];

/// A protocol timestamp validity interval.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct TimestampBound {
    not_before: u64,
    not_after: u64,
}

impl TimestampBound {
    /// Constructs a non-inverted validity interval.
    ///
    /// # Errors
    ///
    /// Returns [`ActivityBuildError::InvalidTimestampBound`] when the end is
    /// earlier than the start.
    pub const fn new(not_before: u64, not_after: u64) -> Result<Self, ActivityBuildError> {
        if not_after < not_before {
            return Err(ActivityBuildError::InvalidTimestampBound);
        }
        Ok(Self {
            not_before,
            not_after,
        })
    }

    /// Returns the inclusive lower timestamp bound.
    #[must_use]
    pub const fn not_before(self) -> u64 {
        self.not_before
    }

    /// Returns the inclusive upper timestamp bound.
    #[must_use]
    pub const fn not_after(self) -> u64 {
        self.not_after
    }
}

/// The six and only six protocol authorization representations.
#[derive(Clone, Debug, Eq, PartialEq)]
pub enum Authority {
    /// Direct owner authorization.
    Owner(Box<[u8]>),
    /// Time- and scope-bounded session key authorization.
    SessionKey(Box<[u8]>),
    /// Delegated capability authorization.
    DelegatedCapability(Box<[u8]>),
    /// Budget allowance authorization.
    BudgetAllowance(Box<[u8]>),
    /// Escrow-controlled authorization.
    Escrow(Box<[u8]>),
    /// Protocol module authorization.
    ProtocolModule(Box<[u8]>),
}

impl Authority {
    /// Constructs a bounded authorization byte representation for a known kind.
    ///
    /// # Errors
    ///
    /// Returns [`ActivityBuildError::AuthorityLength`] before allocation when
    /// bytes exceed the protocol maximum.
    pub fn owner(bytes: &[u8]) -> Result<Self, ActivityBuildError> {
        Self::bounded(bytes).map(Self::Owner)
    }

    /// Constructs session-key authorization bytes.
    ///
    /// # Errors
    ///
    /// Returns an error when bytes exceed the protocol maximum.
    pub fn session_key(bytes: &[u8]) -> Result<Self, ActivityBuildError> {
        Self::bounded(bytes).map(Self::SessionKey)
    }

    /// Constructs delegated-capability authorization bytes.
    ///
    /// # Errors
    ///
    /// Returns an error when bytes exceed the protocol maximum.
    pub fn delegated_capability(bytes: &[u8]) -> Result<Self, ActivityBuildError> {
        Self::bounded(bytes).map(Self::DelegatedCapability)
    }

    /// Constructs budget-allowance authorization bytes.
    ///
    /// # Errors
    ///
    /// Returns an error when bytes exceed the protocol maximum.
    pub fn budget_allowance(bytes: &[u8]) -> Result<Self, ActivityBuildError> {
        Self::bounded(bytes).map(Self::BudgetAllowance)
    }

    /// Constructs escrow authorization bytes.
    ///
    /// # Errors
    ///
    /// Returns an error when bytes exceed the protocol maximum.
    pub fn escrow(bytes: &[u8]) -> Result<Self, ActivityBuildError> {
        Self::bounded(bytes).map(Self::Escrow)
    }

    /// Constructs protocol-module authorization bytes.
    ///
    /// # Errors
    ///
    /// Returns an error when bytes exceed the protocol maximum.
    pub fn protocol_module(bytes: &[u8]) -> Result<Self, ActivityBuildError> {
        Self::bounded(bytes).map(Self::ProtocolModule)
    }

    fn bounded(bytes: &[u8]) -> Result<Box<[u8]>, ActivityBuildError> {
        if bytes.len() > MAX_AUTHORITY_BYTES {
            return Err(ActivityBuildError::AuthorityLength(bytes.len()));
        }
        Ok(Box::<[u8]>::from(bytes))
    }

    /// Borrows the exact canonical authorization bytes.
    #[must_use]
    pub fn as_bytes(&self) -> &[u8] {
        match self {
            Self::Owner(bytes)
            | Self::SessionKey(bytes)
            | Self::DelegatedCapability(bytes)
            | Self::BudgetAllowance(bytes)
            | Self::Escrow(bytes)
            | Self::ProtocolModule(bytes) => bytes,
        }
    }
}

/// A bounded signature to attach to one exact unsigned envelope.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Signature(Box<[u8]>);

impl Signature {
    /// Constructs signature bytes accepted by the canonical envelope codec.
    ///
    /// # Errors
    ///
    /// Returns [`ActivityBuildError::SignatureLength`] before allocation when
    /// bytes exceed the protocol maximum.
    pub fn new(bytes: &[u8]) -> Result<Self, ActivityBuildError> {
        if bytes.len() > MAX_SIGNATURE_BYTES {
            return Err(ActivityBuildError::SignatureLength(bytes.len()));
        }
        Ok(Self(Box::<[u8]>::from(bytes)))
    }

    /// Borrows the exact signature bytes.
    #[must_use]
    pub fn as_bytes(&self) -> &[u8] {
        &self.0
    }
}

/// The unsigned eleven-field activity form. It cannot be submitted as signed.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct UnsignedEnvelope {
    protocol_version: u16,
    network_id: u32,
    activity_type: ActivityType,
    actor_did: Did,
    authority: Authority,
    account_sequence: u64,
    timestamp_bound: TimestampBound,
    idempotency_key: IdempotencyKey,
    fee_limit: Amount,
    payload_hash: [u8; IDENTIFIER_BYTES],
    payload: Payload,
}

impl UnsignedEnvelope {
    #[must_use]
    pub const fn protocol_version(&self) -> u16 {
        self.protocol_version
    }

    #[must_use]
    pub const fn network_id(&self) -> u32 {
        self.network_id
    }

    #[must_use]
    pub const fn activity_type(&self) -> ActivityType {
        self.activity_type
    }

    #[must_use]
    pub const fn actor_did(&self) -> &Did {
        &self.actor_did
    }

    #[must_use]
    pub const fn authority(&self) -> &Authority {
        &self.authority
    }

    #[must_use]
    pub const fn account_sequence(&self) -> u64 {
        self.account_sequence
    }

    #[must_use]
    pub const fn timestamp_bound(&self) -> TimestampBound {
        self.timestamp_bound
    }

    #[must_use]
    pub const fn idempotency_key(&self) -> IdempotencyKey {
        self.idempotency_key
    }

    #[must_use]
    pub const fn fee_limit(&self) -> Amount {
        self.fee_limit
    }

    #[must_use]
    pub const fn payload_hash(&self) -> [u8; IDENTIFIER_BYTES] {
        self.payload_hash
    }

    #[must_use]
    pub const fn payload(&self) -> &Payload {
        &self.payload
    }

    /// Consumes this exact unsigned value and attaches its signature, producing
    /// the non-interchangeable signed envelope type.
    #[must_use]
    pub fn attach_signature(self, signature: Signature) -> Envelope {
        Envelope {
            protocol_version: self.protocol_version,
            network_id: self.network_id,
            activity_type: self.activity_type,
            actor_did: self.actor_did,
            authority: self.authority,
            account_sequence: self.account_sequence,
            timestamp_bound: self.timestamp_bound,
            idempotency_key: self.idempotency_key,
            fee_limit: self.fee_limit,
            payload_hash: self.payload_hash,
            payload: self.payload,
            signature,
        }
    }
}

/// The signed protocol activity with exactly its twelve mandatory fields.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Envelope {
    protocol_version: u16,
    network_id: u32,
    activity_type: ActivityType,
    actor_did: Did,
    authority: Authority,
    account_sequence: u64,
    timestamp_bound: TimestampBound,
    idempotency_key: IdempotencyKey,
    fee_limit: Amount,
    payload_hash: [u8; IDENTIFIER_BYTES],
    payload: Payload,
    signature: Signature,
}

impl Envelope {
    /// Returns the protocol version field.
    #[must_use]
    pub const fn protocol_version(&self) -> u16 {
        self.protocol_version
    }

    /// Returns the network identifier field.
    #[must_use]
    pub const fn network_id(&self) -> u32 {
        self.network_id
    }

    /// Returns the activity type field.
    #[must_use]
    pub const fn activity_type(&self) -> ActivityType {
        self.activity_type
    }

    /// Returns the actor DID field.
    #[must_use]
    pub const fn actor_did(&self) -> &Did {
        &self.actor_did
    }

    /// Returns the authority field.
    #[must_use]
    pub const fn authority(&self) -> &Authority {
        &self.authority
    }

    /// Returns the account sequence field.
    #[must_use]
    pub const fn account_sequence(&self) -> u64 {
        self.account_sequence
    }

    /// Returns the timestamp bound field.
    #[must_use]
    pub const fn timestamp_bound(&self) -> TimestampBound {
        self.timestamp_bound
    }

    /// Returns the idempotency key field.
    #[must_use]
    pub const fn idempotency_key(&self) -> IdempotencyKey {
        self.idempotency_key
    }

    /// Returns the fee limit field.
    #[must_use]
    pub const fn fee_limit(&self) -> Amount {
        self.fee_limit
    }

    /// Returns the payload hash field.
    #[must_use]
    pub const fn payload_hash(&self) -> [u8; IDENTIFIER_BYTES] {
        self.payload_hash
    }

    /// Returns the payload field.
    #[must_use]
    pub const fn payload(&self) -> &Payload {
        &self.payload
    }

    /// Returns the signature field.
    #[must_use]
    pub const fn signature(&self) -> &Signature {
        &self.signature
    }
}

/// A pre-construction field accumulator that rejects duplicate and missing
/// protocol fields. Its optional storage is not an envelope representation.
#[derive(Clone, Debug, Default, Eq, PartialEq)]
pub struct EnvelopeBuilder {
    protocol_version: Option<u16>,
    network_id: Option<u32>,
    activity_type: Option<ActivityType>,
    actor_did: Option<Did>,
    authority: Option<Authority>,
    account_sequence: Option<u64>,
    timestamp_bound: Option<TimestampBound>,
    idempotency_key: Option<IdempotencyKey>,
    fee_limit: Option<Amount>,
    payload_hash: Option<[u8; IDENTIFIER_BYTES]>,
    payload: Option<Payload>,
}

impl EnvelopeBuilder {
    /// Creates an empty pre-construction accumulator.
    #[must_use]
    pub const fn new() -> Self {
        Self {
            protocol_version: None,
            network_id: None,
            activity_type: None,
            actor_did: None,
            authority: None,
            account_sequence: None,
            timestamp_bound: None,
            idempotency_key: None,
            fee_limit: None,
            payload_hash: None,
            payload: None,
        }
    }

    /// Sets the protocol version exactly once.
    ///
    /// # Errors
    ///
    /// Returns a repeated-field error when already set.
    pub fn protocol_version(&mut self, value: u16) -> Result<&mut Self, ActivityBuildError> {
        set_once(&mut self.protocol_version, value, "protocol_version")?;
        Ok(self)
    }

    /// Sets the network identifier exactly once.
    ///
    /// # Errors
    ///
    /// Returns a repeated-field error when already set.
    pub fn network_id(&mut self, value: u32) -> Result<&mut Self, ActivityBuildError> {
        set_once(&mut self.network_id, value, "network_id")?;
        Ok(self)
    }

    /// Sets the activity type exactly once.
    ///
    /// # Errors
    ///
    /// Returns a repeated-field error when already set.
    pub fn activity_type(&mut self, value: ActivityType) -> Result<&mut Self, ActivityBuildError> {
        set_once(&mut self.activity_type, value, "activity_type")?;
        Ok(self)
    }

    /// Sets the actor DID exactly once.
    ///
    /// # Errors
    ///
    /// Returns a repeated-field error when already set.
    pub fn actor_did(&mut self, value: Did) -> Result<&mut Self, ActivityBuildError> {
        set_once(&mut self.actor_did, value, "actor_did")?;
        Ok(self)
    }

    /// Sets the authority exactly once.
    ///
    /// # Errors
    ///
    /// Returns a repeated-field error when already set.
    pub fn authority(&mut self, value: Authority) -> Result<&mut Self, ActivityBuildError> {
        set_once(&mut self.authority, value, "authority")?;
        Ok(self)
    }

    /// Sets the account sequence exactly once.
    ///
    /// # Errors
    ///
    /// Returns a repeated-field error when already set.
    pub fn account_sequence(&mut self, value: u64) -> Result<&mut Self, ActivityBuildError> {
        set_once(&mut self.account_sequence, value, "account_sequence")?;
        Ok(self)
    }

    /// Sets the timestamp bound exactly once.
    ///
    /// # Errors
    ///
    /// Returns a repeated-field error when already set.
    pub fn timestamp_bound(
        &mut self,
        value: TimestampBound,
    ) -> Result<&mut Self, ActivityBuildError> {
        set_once(&mut self.timestamp_bound, value, "timestamp_bound")?;
        Ok(self)
    }

    /// Sets the idempotency key exactly once.
    ///
    /// # Errors
    ///
    /// Returns a repeated-field error when already set.
    pub fn idempotency_key(
        &mut self,
        value: IdempotencyKey,
    ) -> Result<&mut Self, ActivityBuildError> {
        set_once(&mut self.idempotency_key, value, "idempotency_key")?;
        Ok(self)
    }

    /// Sets the fee limit exactly once.
    ///
    /// # Errors
    ///
    /// Returns a repeated-field error when already set.
    pub fn fee_limit(&mut self, value: Amount) -> Result<&mut Self, ActivityBuildError> {
        set_once(&mut self.fee_limit, value, "fee_limit")?;
        Ok(self)
    }

    /// Sets the payload hash exactly once.
    ///
    /// # Errors
    ///
    /// Returns a repeated-field error when already set.
    pub fn payload_hash(
        &mut self,
        value: [u8; IDENTIFIER_BYTES],
    ) -> Result<&mut Self, ActivityBuildError> {
        set_once(&mut self.payload_hash, value, "payload_hash")?;
        Ok(self)
    }

    /// Sets the payload exactly once.
    ///
    /// # Errors
    ///
    /// Returns a repeated-field error when already set.
    pub fn payload(&mut self, value: Payload) -> Result<&mut Self, ActivityBuildError> {
        set_once(&mut self.payload, value, "payload")?;
        Ok(self)
    }

    /// Validates all mandatory unsigned fields and their activity/payload tag.
    ///
    /// # Errors
    ///
    /// Returns a typed missing-field or activity-tag mismatch error.
    pub fn build(self) -> Result<UnsignedEnvelope, ActivityBuildError> {
        let activity_type = required(self.activity_type, "activity_type")?;
        let payload = required(self.payload, "payload")?;
        if payload.activity_type() != activity_type {
            return Err(ActivityBuildError::PayloadActivityMismatch);
        }
        Ok(UnsignedEnvelope {
            protocol_version: required(self.protocol_version, "protocol_version")?,
            network_id: required(self.network_id, "network_id")?,
            activity_type,
            actor_did: required(self.actor_did, "actor_did")?,
            authority: required(self.authority, "authority")?,
            account_sequence: required(self.account_sequence, "account_sequence")?,
            timestamp_bound: required(self.timestamp_bound, "timestamp_bound")?,
            idempotency_key: required(self.idempotency_key, "idempotency_key")?,
            fee_limit: required(self.fee_limit, "fee_limit")?,
            payload_hash: required(self.payload_hash, "payload_hash")?,
            payload,
        })
    }
}

fn set_once<T>(
    slot: &mut Option<T>,
    value: T,
    name: &'static str,
) -> Result<(), ActivityBuildError> {
    if slot.is_some() {
        return Err(ActivityBuildError::RepeatedField(name));
    }
    *slot = Some(value);
    Ok(())
}

fn required<T>(slot: Option<T>, name: &'static str) -> Result<T, ActivityBuildError> {
    slot.ok_or(ActivityBuildError::MissingField(name))
}

/// Failure to construct a complete protocol activity.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum ActivityBuildError {
    /// A mandatory field was absent.
    MissingField(&'static str),
    /// A field was supplied more than once.
    RepeatedField(&'static str),
    /// The payload's registered activity tag differed from the envelope field.
    PayloadActivityMismatch,
    /// The timestamp interval was inverted.
    InvalidTimestampBound,
    /// Authority bytes exceeded the protocol maximum.
    AuthorityLength(usize),
    /// Signature bytes exceeded the protocol maximum.
    SignatureLength(usize),
}
