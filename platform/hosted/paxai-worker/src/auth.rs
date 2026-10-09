//! F02-R008/R010/R018 signed service transport and the delegate-signed metadata scheme.
//!
//! Service envelope (roster zero means absent):
//! `PAXAIS1 || schema:u16=1 || operation:u16 || chain32 || program32 || market32 || actor32 ||
//! epoch:u64 || config:u64 || roster32 || sequence:u64 || expiry:u64 || request32 ||
//! payload_len:u32 || payload || signer32 || signature64`.
//! Ed25519 signs `H("PAXAI/service-request/v1", magic..end of payload)`.
//!
//! Signed metadata: the canonical manifest travels in the payload of a native C03
//! `PublishMetadata` (0x0202) envelope authenticated by the worker delegate
//! (authentication kind 1, actor = owner principal); the delegate signs the common
//! `PAXAI/request/v1` digest. The payload, exactly as the native transition reads it, is
//! `worker32 || expected_revision:u64 || revision:u64 || metadata_digest32 || valid_from:u64 ||
//! expiry:u64 || manifest_len:u32 || manifest`.
use crate::discovery::{FinalizedAuthority, IdentityEvidence, VerifiedMetadata};
use crate::metadata::{Capability, Manifest, API_VERSION};
use ed25519_dalek::{Signer, SigningKey};
use layerx_programs_ai_market::{
    codec::{self, Envelope, Reader},
    dispatch,
    errors::{self as app, offchain_error, ApplicationError, OffchainSpace},
    evaluators::{codec::verify_digest, model::VerificationError},
    tasks::{TaskBinding, TaskStatus},
    types::{
        Authentication, ChainDomain, Digest32, MarketId, MetadataDigest, PolicyDigest, Presence,
        PrincipalId, ProgramId, PublicKey32, RequestId, RosterDigest, Signature64, TaskId,
        WorkerId,
    },
    workers::MAX_MANIFEST_BYTES,
    MAX_ENVELOPE_BYTES, MAX_PAYLOAD_BYTES, SCHEMA_VERSION,
};
use std::fmt;

pub const SERVICE_MAGIC: &[u8; 7] = b"PAXAIS1";
pub const SERVICE_DOMAIN: &str = "PAXAI/service-request/v1";
pub const ACKNOWLEDGMENT_DOMAIN: &str = "PAXAI/worker-ack/v1";
pub const SERVICE_PREFIX_BYTES: usize = 239;
pub const SERVICE_SUFFIX_BYTES: usize = 96;
pub const ERROR_BODY_BYTES: usize = 34;
pub const SERVICE_REQUEST_BYTES: usize = 275;
pub const RESULT_KEY_BYTES: usize = 32;
pub const JOB_REFERENCE_BYTES: usize = 122;
pub const ACKNOWLEDGMENT_BYTES: usize = 234;
pub const STATUS_CHALLENGE_BYTES: usize = 88;
pub const STATUS_REPORT_BYTES: usize = 60;
pub const DEFAULT_DEADLINE_MS: u32 = 60_000;
pub const MAX_DEADLINE_MS: u32 = 3_600_000;
pub const SUBMIT_PATH: &str = "/paxai/v1/jobs";
pub const QUERY_PATH: &str = "/paxai/v1/jobs/query";
pub const CANCEL_PATH: &str = "/paxai/v1/jobs/cancel";
pub const STATUS_PATH: &str = "/paxai/v1/status";
const PUBLISH_METADATA_FIXED_BYTES: usize = 32 + 8 + 8 + 32 + 8 + 8 + 4;
const NATIVE_DELEGATE_SUFFIX_BYTES: usize = 97;

/// Worker-service error space (`OffchainSpace::WorkerService`), codes 1..=29.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
#[repr(u16)]
pub enum ServiceError {
    NonCanonical = 1,
    UnsupportedVersion,
    WrongDomain,
    BadSignature,
    OwnerRequired,
    DelegateConsentRequired,
    DelegateRevoked,
    IdentityFrozen,
    StaleAuthority,
    WrongGeneration,
    WrongRevision,
    MetadataExpired,
    MetadataUnavailable,
    MetadataIntegrityFailure,
    CapabilityMismatch,
    AdmissionNotEffective,
    MarketPaused,
    RateLimited,
    CapacityExceeded,
    InputTooLarge,
    OutputTooLarge,
    DeadlineInvalid,
    IdempotencyConflict,
    SequenceGap,
    Overflow,
    NotFound,
    AccessDenied,
    ExecutionUnavailable,
    UnknownExecution,
}

pub const SERVICE_ERRORS: [ServiceError; 29] = [
    ServiceError::NonCanonical,
    ServiceError::UnsupportedVersion,
    ServiceError::WrongDomain,
    ServiceError::BadSignature,
    ServiceError::OwnerRequired,
    ServiceError::DelegateConsentRequired,
    ServiceError::DelegateRevoked,
    ServiceError::IdentityFrozen,
    ServiceError::StaleAuthority,
    ServiceError::WrongGeneration,
    ServiceError::WrongRevision,
    ServiceError::MetadataExpired,
    ServiceError::MetadataUnavailable,
    ServiceError::MetadataIntegrityFailure,
    ServiceError::CapabilityMismatch,
    ServiceError::AdmissionNotEffective,
    ServiceError::MarketPaused,
    ServiceError::RateLimited,
    ServiceError::CapacityExceeded,
    ServiceError::InputTooLarge,
    ServiceError::OutputTooLarge,
    ServiceError::DeadlineInvalid,
    ServiceError::IdempotencyConflict,
    ServiceError::SequenceGap,
    ServiceError::Overflow,
    ServiceError::NotFound,
    ServiceError::AccessDenied,
    ServiceError::ExecutionUnavailable,
    ServiceError::UnknownExecution,
];

impl ServiceError {
    #[must_use]
    pub const fn code(self) -> u16 {
        self as u16
    }
    /// # Errors
    /// `NonCanonical` for code 0 or any code outside the worker-service table.
    pub fn from_code(code: u16) -> Result<Self, Self> {
        SERVICE_ERRORS
            .iter()
            .copied()
            .find(|e| e.code() == code)
            .ok_or(Self::NonCanonical)
    }
    /// Registered name in the shared worker-service error table.
    ///
    /// # Errors
    /// `NonCanonical` when the shared table does not register this code.
    pub fn name(self) -> Result<&'static str, Self> {
        offchain_error(OffchainSpace::WorkerService, self.code()).map_err(|_| Self::NonCanonical)
    }
    /// Refusal body: code:u16 || request32 (zero when the request is not yet known). It never
    /// carries inputs, keys, signatures, locators or output.
    #[must_use]
    pub fn body(self, request: Presence<RequestId>) -> [u8; ERROR_BODY_BYTES] {
        let mut out = [0; ERROR_BODY_BYTES];
        out[..2].copy_from_slice(&self.code().to_be_bytes());
        if let Presence::Present(id) = request {
            out[2..].copy_from_slice(id.as_bytes());
        }
        out
    }
    /// # Errors
    /// `NonCanonical` for a body of the wrong size or an unknown code.
    pub fn decode_body(body: &[u8]) -> Result<(Self, Presence<RequestId>), Self> {
        let mut r = Reader::new(body);
        let error = Self::from_code(r.u16()?)?;
        let id: [u8; 32] = r.fixed()?;
        r.finish()?;
        let request = if id == [0; 32] {
            Presence::Absent
        } else {
            Presence::Present(RequestId::new(id)?)
        };
        Ok((error, request))
    }
}
impl fmt::Display for ServiceError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self.name() {
            Ok(name) => write!(f, "worker service refusal {name}"),
            Err(_) => write!(f, "worker service refusal {}", self.code()),
        }
    }
}
impl std::error::Error for ServiceError {}

impl From<ApplicationError> for ServiceError {
    fn from(error: ApplicationError) -> Self {
        match error {
            app::BAD_VERSION => Self::UnsupportedVersion,
            app::WRONG_DOMAIN | app::WRONG_PROGRAM | app::WRONG_MARKET => Self::WrongDomain,
            app::BAD_SIGNATURE | app::KEY_MISMATCH => Self::BadSignature,
            app::UNAUTHORIZED | app::F02_ACCESS_DENIED => Self::AccessDenied,
            app::REVOKED | app::F02_DELEGATE_REVOKED => Self::DelegateRevoked,
            app::EXPIRED | app::F02_DEADLINE_INVALID => Self::DeadlineInvalid,
            app::WRONG_EPOCH
            | app::WRONG_CONFIG
            | app::WRONG_ROSTER
            | app::EVIDENCE_BINDING
            | app::F02_STALE_AUTHORITY => Self::StaleAuthority,
            app::SEQUENCE_GAP => Self::SequenceGap,
            app::REPLAY_CONFLICT | app::CONFLICT => Self::IdempotencyConflict,
            app::NOT_FOUND => Self::NotFound,
            app::CAPACITY => Self::CapacityExceeded,
            app::ARITHMETIC => Self::Overflow,
            app::READINESS_BLOCKED | app::F02_ADMISSION_NOT_EFFECTIVE => {
                Self::AdmissionNotEffective
            }
            app::F02_OWNER_REQUIRED => Self::OwnerRequired,
            app::F02_DELEGATE_CONSENT_REQUIRED => Self::DelegateConsentRequired,
            app::F02_IDENTITY_FROZEN => Self::IdentityFrozen,
            app::F02_WRONG_GENERATION => Self::WrongGeneration,
            app::F02_WRONG_REVISION => Self::WrongRevision,
            app::F02_METADATA_EXPIRED => Self::MetadataExpired,
            app::F02_METADATA_UNAVAILABLE => Self::MetadataUnavailable,
            app::F02_METADATA_INTEGRITY_FAILURE => Self::MetadataIntegrityFailure,
            app::F02_CAPABILITY_MISMATCH => Self::CapabilityMismatch,
            app::F02_MARKET_PAUSED => Self::MarketPaused,
            app::F02_RATE_LIMITED => Self::RateLimited,
            app::F02_INPUT_TOO_LARGE => Self::InputTooLarge,
            app::F02_OUTPUT_TOO_LARGE => Self::OutputTooLarge,
            app::F02_UNKNOWN_EXECUTION => Self::UnknownExecution,
            _ => Self::NonCanonical,
        }
    }
}

fn verify(key: PublicKey32, signature: Signature64, digest: Digest32) -> Result<(), ServiceError> {
    verify_digest(key, signature, digest.bytes())
        .map_err(|VerificationError::Application(error)| ServiceError::from(error))
}

/// External service-only operation codes; none of them is a native F02 selector.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
#[repr(u16)]
pub enum ServiceOperation {
    SubmitJob = 0x0281,
    QueryJob = 0x0282,
    CancelJob = 0x0283,
    Acknowledgment = 0x0284,
    JobResult = 0x0285,
    Status = 0x0286,
}
impl ServiceOperation {
    #[must_use]
    pub const fn code(self) -> u16 {
        self as u16
    }
    /// # Errors
    /// `NonCanonical` for any code outside 0x0281..=0x0286.
    pub fn from_code(code: u16) -> Result<Self, ServiceError> {
        Ok(match code {
            0x0281 => Self::SubmitJob,
            0x0282 => Self::QueryJob,
            0x0283 => Self::CancelJob,
            0x0284 => Self::Acknowledgment,
            0x0285 => Self::JobResult,
            0x0286 => Self::Status,
            _ => return Err(ServiceError::NonCanonical),
        })
    }
}

/// Signed job routes: `method_code` and `route_code` share these values.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
#[repr(u8)]
pub enum Route {
    Submit = 1,
    Query = 2,
    Cancel = 3,
}
impl Route {
    #[must_use]
    pub const fn code(self) -> u8 {
        self as u8
    }
    #[must_use]
    pub const fn path(self) -> &'static str {
        match self {
            Self::Submit => SUBMIT_PATH,
            Self::Query => QUERY_PATH,
            Self::Cancel => CANCEL_PATH,
        }
    }
    #[must_use]
    pub const fn operation(self) -> ServiceOperation {
        match self {
            Self::Submit => ServiceOperation::SubmitJob,
            Self::Query => ServiceOperation::QueryJob,
            Self::Cancel => ServiceOperation::CancelJob,
        }
    }
    #[must_use]
    pub fn from_path(path: &str) -> Option<Self> {
        [Self::Submit, Self::Query, Self::Cancel]
            .into_iter()
            .find(|r| r.path() == path)
    }
    fn check(self, method: u8, route: u8) -> Result<(), ServiceError> {
        if method == self.code() && route == self.code() {
            Ok(())
        } else {
            Err(ServiceError::BadSignature)
        }
    }
}

/// The shared signed-operation context of one service message.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct ServiceContext {
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
}
impl ServiceContext {
    fn write(&self, out: &mut Vec<u8>) {
        out.extend_from_slice(self.chain.as_bytes());
        out.extend_from_slice(self.program.as_bytes());
        out.extend_from_slice(self.market.as_bytes());
        out.extend_from_slice(self.actor.as_bytes());
        out.extend_from_slice(&self.epoch.to_be_bytes());
        out.extend_from_slice(&self.config.to_be_bytes());
        out.extend_from_slice(&presence_bytes(self.roster));
        out.extend_from_slice(&self.sequence.to_be_bytes());
        out.extend_from_slice(&self.expiry.to_be_bytes());
        out.extend_from_slice(self.request.as_bytes());
    }
    fn read(r: &mut Reader<'_>) -> Result<Self, ServiceError> {
        Ok(Self {
            chain: ChainDomain::new(r.fixed()?)?,
            program: ProgramId::new(r.fixed()?)?,
            market: MarketId::new(r.fixed()?)?,
            actor: PrincipalId::new(r.fixed()?)?,
            epoch: r.u64()?,
            config: r.u64()?,
            roster: read_roster(r)?,
            sequence: r.u64()?,
            expiry: r.u64()?,
            request: RequestId::new(r.fixed()?)?,
        })
    }
}

fn presence_bytes(roster: Presence<RosterDigest>) -> [u8; 32] {
    match roster {
        Presence::Absent => [0; 32],
        Presence::Present(digest) => digest.bytes(),
    }
}

fn read_roster(r: &mut Reader<'_>) -> Result<Presence<RosterDigest>, ServiceError> {
    let bytes: [u8; 32] = r.fixed()?;
    Ok(if bytes == [0; 32] {
        Presence::Absent
    } else {
        Presence::Present(RosterDigest::new(bytes)?)
    })
}

/// A decoded service envelope whose signature already verified under `signer`.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ServiceEnvelope {
    pub operation: ServiceOperation,
    pub context: ServiceContext,
    pub payload: Vec<u8>,
    pub signer: PublicKey32,
    pub signature: Signature64,
    pub digest: Digest32,
}

/// Encodes and signs one service envelope.
///
/// # Errors
/// `InputTooLarge` above the payload cap; `NonCanonical` for a zero expiry or config.
pub fn encode_service(
    operation: ServiceOperation,
    context: &ServiceContext,
    payload: &[u8],
    key: &SigningKey,
) -> Result<Vec<u8>, ServiceError> {
    if payload.len() > MAX_PAYLOAD_BYTES {
        return Err(ServiceError::InputTooLarge);
    }
    if context.expiry == 0 || context.config == 0 {
        return Err(ServiceError::NonCanonical);
    }
    let length = u32::try_from(payload.len()).map_err(|_| ServiceError::InputTooLarge)?;
    let mut out = Vec::with_capacity(SERVICE_PREFIX_BYTES + payload.len() + SERVICE_SUFFIX_BYTES);
    out.extend_from_slice(SERVICE_MAGIC);
    out.extend_from_slice(&SCHEMA_VERSION.to_be_bytes());
    out.extend_from_slice(&operation.code().to_be_bytes());
    context.write(&mut out);
    out.extend_from_slice(&length.to_be_bytes());
    out.extend_from_slice(payload);
    let digest = codec::domain_hash(SERVICE_DOMAIN, &out)?;
    let signature = key.sign(digest.as_bytes());
    out.extend_from_slice(key.verifying_key().as_bytes());
    out.extend_from_slice(&signature.to_bytes());
    Ok(out)
}

/// Decodes one service envelope and verifies its signature under the embedded signer.
/// Whether that signer is authorized is decided by the caller against finalized identity.
///
/// # Errors
/// `InputTooLarge` above the envelope or payload cap; `NonCanonical` for a bad magic, unknown
/// operation, zero identity, zero expiry or config, short or trailing bytes;
/// `UnsupportedVersion` for a schema other than 1; `BadSignature` when the signature fails.
pub fn decode_service(input: &[u8]) -> Result<ServiceEnvelope, ServiceError> {
    if input.len() > MAX_ENVELOPE_BYTES {
        return Err(ServiceError::InputTooLarge);
    }
    let mut r = Reader::new(input);
    if r.take(SERVICE_MAGIC.len())? != SERVICE_MAGIC {
        return Err(ServiceError::NonCanonical);
    }
    if r.u16()? != SCHEMA_VERSION {
        return Err(ServiceError::UnsupportedVersion);
    }
    let operation = ServiceOperation::from_code(r.u16()?)?;
    let context = ServiceContext::read(&mut r)?;
    let length = usize::try_from(r.u32()?).map_err(|_| ServiceError::InputTooLarge)?;
    if length > MAX_PAYLOAD_BYTES {
        return Err(ServiceError::InputTooLarge);
    }
    let payload = r.take(length)?.to_vec();
    let unsigned = input.get(..r.offset()).ok_or(ServiceError::NonCanonical)?;
    let signer = PublicKey32(r.fixed()?);
    let signature = Signature64(r.fixed()?);
    r.finish()?;
    if context.expiry == 0 || context.config == 0 {
        return Err(ServiceError::NonCanonical);
    }
    let digest = codec::domain_hash(SERVICE_DOMAIN, unsigned)?;
    verify(signer, signature, digest)?;
    Ok(ServiceEnvelope {
        operation,
        context,
        payload,
        signer,
        signature,
        digest,
    })
}

/// F02-R008 `ServiceRequest` payload of a `SubmitJob` envelope.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct ServiceRequest {
    pub receiver: WorkerId,
    pub task: TaskId,
    pub generation: u64,
    pub key_version: u64,
    pub metadata_revision: u64,
    pub method: u8,
    pub route: u8,
    pub capability: Digest32,
    pub workload_policy: PolicyDigest,
    pub model: Digest32,
    pub input_commitment: Digest32,
    pub payload_bytes: u32,
    pub max_output_bytes: u32,
    pub max_units: u32,
    pub deadline_ms: u32,
    pub task_expiry: u64,
    pub evaluation_access: Digest32,
    pub result_key: Option<[u8; RESULT_KEY_BYTES]>,
}
impl ServiceRequest {
    /// # Errors
    /// `NonCanonical` for a zero version, size or expiry, or a zero result key.
    pub fn encode(&self) -> Result<Vec<u8>, ServiceError> {
        self.validate()?;
        let mut out = Vec::with_capacity(SERVICE_REQUEST_BYTES + RESULT_KEY_BYTES);
        out.extend_from_slice(self.receiver.as_bytes());
        out.extend_from_slice(self.task.as_bytes());
        out.extend_from_slice(&self.generation.to_be_bytes());
        out.extend_from_slice(&self.key_version.to_be_bytes());
        out.extend_from_slice(&self.metadata_revision.to_be_bytes());
        out.push(self.method);
        out.push(self.route);
        out.extend_from_slice(self.capability.as_bytes());
        out.extend_from_slice(self.workload_policy.as_bytes());
        out.extend_from_slice(self.model.as_bytes());
        out.extend_from_slice(self.input_commitment.as_bytes());
        for value in [
            self.payload_bytes,
            self.max_output_bytes,
            self.max_units,
            self.deadline_ms,
        ] {
            out.extend_from_slice(&value.to_be_bytes());
        }
        out.extend_from_slice(&self.task_expiry.to_be_bytes());
        out.extend_from_slice(self.evaluation_access.as_bytes());
        match self.result_key {
            None => out.push(0),
            Some(key) => {
                out.push(1);
                out.extend_from_slice(&key);
            }
        }
        Ok(out)
    }
    /// # Errors
    /// `NonCanonical` for short, trailing or noncanonical bytes.
    pub fn decode(payload: &[u8]) -> Result<Self, ServiceError> {
        let mut r = Reader::new(payload);
        let value = Self {
            receiver: WorkerId::new(r.fixed()?)?,
            task: TaskId::new(r.fixed()?)?,
            generation: r.u64()?,
            key_version: r.u64()?,
            metadata_revision: r.u64()?,
            method: r.u8()?,
            route: r.u8()?,
            capability: Digest32::new(r.fixed()?)?,
            workload_policy: PolicyDigest::new(r.fixed()?)?,
            model: Digest32::new(r.fixed()?)?,
            input_commitment: Digest32::new(r.fixed()?)?,
            payload_bytes: r.u32()?,
            max_output_bytes: r.u32()?,
            max_units: r.u32()?,
            deadline_ms: r.u32()?,
            task_expiry: r.u64()?,
            evaluation_access: Digest32::new(r.fixed()?)?,
            result_key: match r.u8()? {
                0 => None,
                1 => Some(r.fixed()?),
                _ => return Err(ServiceError::NonCanonical),
            },
        };
        r.finish()?;
        value.validate()?;
        Ok(value)
    }
    fn validate(&self) -> Result<(), ServiceError> {
        if self.generation == 0
            || self.key_version == 0
            || self.metadata_revision == 0
            || self.payload_bytes == 0
            || self.max_output_bytes == 0
            || self.max_units == 0
            || self.task_expiry == 0
            || self.result_key == Some([0; RESULT_KEY_BYTES])
        {
            return Err(ServiceError::NonCanonical);
        }
        Ok(())
    }
}

/// Payload of a signed `QueryJob` or `CancelJob`: it names the original request and
/// cannot change any execution parameter.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct JobReference {
    pub receiver: WorkerId,
    pub task: TaskId,
    pub generation: u64,
    pub key_version: u64,
    pub metadata_revision: u64,
    pub method: u8,
    pub route: u8,
    pub original: RequestId,
}
impl JobReference {
    #[must_use]
    pub fn encode(&self) -> Vec<u8> {
        let mut out = Vec::with_capacity(JOB_REFERENCE_BYTES);
        out.extend_from_slice(self.receiver.as_bytes());
        out.extend_from_slice(self.task.as_bytes());
        out.extend_from_slice(&self.generation.to_be_bytes());
        out.extend_from_slice(&self.key_version.to_be_bytes());
        out.extend_from_slice(&self.metadata_revision.to_be_bytes());
        out.push(self.method);
        out.push(self.route);
        out.extend_from_slice(self.original.as_bytes());
        out
    }
    /// # Errors
    /// `NonCanonical` for short, trailing or zero-identity bytes.
    pub fn decode(payload: &[u8]) -> Result<Self, ServiceError> {
        let mut r = Reader::new(payload);
        let value = Self {
            receiver: WorkerId::new(r.fixed()?)?,
            task: TaskId::new(r.fixed()?)?,
            generation: r.u64()?,
            key_version: r.u64()?,
            metadata_revision: r.u64()?,
            method: r.u8()?,
            route: r.u8()?,
            original: RequestId::new(r.fixed()?)?,
        };
        r.finish()?;
        Ok(value)
    }
}

/// The receiving worker's binding: market domain, identity, delegate and versions.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct WorkerBinding {
    pub chain: ChainDomain,
    pub program: ProgramId,
    pub market: MarketId,
    pub worker: WorkerId,
    pub owner: PrincipalId,
    pub delegate: PublicKey32,
    pub generation: u64,
    pub key_version: u64,
    pub metadata_revision: u64,
}
impl WorkerBinding {
    /// # Errors
    /// `WrongDomain` when the chain, program or market differs.
    pub fn check_domain(&self, context: &ServiceContext) -> Result<(), ServiceError> {
        if context.chain == self.chain
            && context.program == self.program
            && context.market == self.market
        {
            Ok(())
        } else {
            Err(ServiceError::WrongDomain)
        }
    }
    fn check_versions(
        &self,
        generation: u64,
        key_version: u64,
        revision: u64,
    ) -> Result<(), ServiceError> {
        if generation != self.generation || key_version != self.key_version {
            Err(ServiceError::WrongGeneration)
        } else if revision != self.metadata_revision {
            Err(ServiceError::WrongRevision)
        } else {
            Ok(())
        }
    }
    fn check_receiver(&self, receiver: WorkerId) -> Result<(), ServiceError> {
        if receiver == self.worker {
            Ok(())
        } else {
            Err(ServiceError::WrongDomain)
        }
    }
}

/// An authenticated job message, bound to the delivered route and receiving worker.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum AuthenticatedRequest {
    Submit(ServiceRequest),
    Query(JobReference),
    Cancel(JobReference),
}

/// Binds a signature-verified envelope to the route it arrived on and to this worker.
///
/// # Errors
/// `BadSignature` when the signed operation, method or route differs from the delivered
/// route; `WrongDomain` for another chain, program, market or receiver; `WrongGeneration` or
/// `WrongRevision` for a submit bound to other delegate versions or metadata revision; payload
/// decoding refusals.
pub fn verify_request(
    envelope: &ServiceEnvelope,
    delivered: Route,
    binding: &WorkerBinding,
) -> Result<AuthenticatedRequest, ServiceError> {
    if envelope.operation != delivered.operation() {
        return Err(ServiceError::BadSignature);
    }
    binding.check_domain(&envelope.context)?;
    if delivered == Route::Submit {
        let request = ServiceRequest::decode(&envelope.payload)?;
        delivered.check(request.method, request.route)?;
        binding.check_receiver(request.receiver)?;
        binding.check_versions(
            request.generation,
            request.key_version,
            request.metadata_revision,
        )?;
        return Ok(AuthenticatedRequest::Submit(request));
    }
    let reference = JobReference::decode(&envelope.payload)?;
    delivered.check(reference.method, reference.route)?;
    binding.check_receiver(reference.receiver)?;
    Ok(if delivered == Route::Query {
        AuthenticatedRequest::Query(reference)
    } else {
        AuthenticatedRequest::Cancel(reference)
    })
}

/// Immutable admission record: the bindings an acknowledgment and any later result keep,
/// whatever metadata or model the worker publishes afterwards.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct Admission {
    pub context: ServiceContext,
    pub worker: WorkerId,
    pub owner: PrincipalId,
    pub delegate: PublicKey32,
    pub task: TaskId,
    pub request_commitment: Digest32,
    pub metadata: MetadataDigest,
    pub metadata_revision: u64,
    pub capability: Digest32,
    pub model: Digest32,
    pub deployment: Digest32,
    pub generation: u64,
    pub key_version: u64,
    pub deadline_ms: u32,
    pub task_deadline: u64,
    pub admitted_height: u64,
}

/// F02-R017 admission checks over one finalized admission view, before any runner contact.
///
/// # Errors
/// Readiness refusals of [`FinalizedAuthority::admission_gate`]; `NonCanonical` for an
/// operation other than `SubmitJob`; binding refusals of [`verify_request`]; `StaleAuthority`
/// when the signed epoch, config or roster or the customer evidence height differs from the
/// view; `DeadlineInvalid` for an expired envelope, a deadline above the capability latency or
/// a task expiry outside the open Work window; `BadSignature` when the signer is not the
/// customer's finalized primary key or the request does not match the F01 task;
/// `IdentityFrozen`, `MetadataIntegrityFailure`, `CapabilityMismatch`, `InputTooLarge`,
/// `OutputTooLarge`, `IdempotencyConflict` and `AdmissionNotEffective` as named.
pub fn admit(
    envelope: &ServiceEnvelope,
    request: &ServiceRequest,
    authority: &FinalizedAuthority,
    customer: &IdentityEvidence,
    metadata: &VerifiedMetadata,
    task: &TaskBinding,
) -> Result<Admission, ServiceError> {
    authority.admission_gate()?;
    if envelope.operation != ServiceOperation::SubmitJob {
        return Err(ServiceError::NonCanonical);
    }
    let binding = authority.worker_binding();
    if verify_request(envelope, Route::Submit, &binding)? != AuthenticatedRequest::Submit(*request)
    {
        return Err(ServiceError::BadSignature);
    }
    let context = envelope.context;
    if Some(context.epoch) != authority.current_epoch()
        || context.config != authority.config()
        || context.roster != authority.roster()
    {
        return Err(ServiceError::StaleAuthority);
    }
    let height = authority.height();
    if context.expiry <= height {
        return Err(ServiceError::DeadlineInvalid);
    }
    check_customer(envelope, customer, height)?;
    if metadata.digest() != authority.record().metadata {
        return Err(ServiceError::MetadataIntegrityFailure);
    }
    let capability = metadata.manifest().capability(request.capability)?;
    if capability.model != request.model.bytes() || request.workload_policy != authority.policy() {
        return Err(ServiceError::CapabilityMismatch);
    }
    let deadline_ms = check_bounds(request, capability)?;
    check_task(request, &context, &binding, task)?;
    if request.task_expiry > authority.work_close()? || request.task_expiry <= height {
        return Err(ServiceError::DeadlineInvalid);
    }
    Ok(Admission {
        context,
        worker: binding.worker,
        owner: binding.owner,
        delegate: binding.delegate,
        task: request.task,
        request_commitment: envelope.digest,
        metadata: metadata.digest(),
        metadata_revision: binding.metadata_revision,
        capability: request.capability,
        model: request.model,
        deployment: metadata.manifest().deployment,
        generation: binding.generation,
        key_version: binding.key_version,
        deadline_ms,
        task_deadline: request.task_expiry,
        admitted_height: height,
    })
}

fn check_customer(
    envelope: &ServiceEnvelope,
    customer: &IdentityEvidence,
    height: u64,
) -> Result<(), ServiceError> {
    if customer.execution_height != height {
        return Err(ServiceError::StaleAuthority);
    }
    if customer.principal != envelope.context.actor || customer.primary_key != envelope.signer {
        return Err(ServiceError::BadSignature);
    }
    if customer.frozen {
        return Err(ServiceError::IdentityFrozen);
    }
    Ok(())
}

fn check_bounds(request: &ServiceRequest, capability: &Capability) -> Result<u32, ServiceError> {
    if request.payload_bytes > capability.max_input_bytes {
        return Err(ServiceError::InputTooLarge);
    }
    if request.max_output_bytes > capability.max_output_bytes
        || request.max_units > capability.max_output_units
    {
        return Err(ServiceError::OutputTooLarge);
    }
    let ceiling = capability.latency_ms.min(MAX_DEADLINE_MS);
    let deadline = if request.deadline_ms == 0 {
        DEFAULT_DEADLINE_MS.min(ceiling)
    } else {
        request.deadline_ms
    };
    if deadline > ceiling {
        return Err(ServiceError::DeadlineInvalid);
    }
    Ok(deadline)
}

fn check_task(
    request: &ServiceRequest,
    context: &ServiceContext,
    binding: &WorkerBinding,
    task: &TaskBinding,
) -> Result<(), ServiceError> {
    if task.task != request.task
        || task.requester != context.actor
        || task.worker != binding.worker
        || task.input != request.input_commitment
    {
        return Err(ServiceError::BadSignature);
    }
    if task.deadline != request.task_expiry {
        return Err(ServiceError::DeadlineInvalid);
    }
    match task.status {
        TaskStatus::Admitted => Ok(()),
        TaskStatus::Accepted => Err(ServiceError::IdempotencyConflict),
        TaskStatus::ResultCommitted | TaskStatus::Cancelled => {
            Err(ServiceError::AdmissionNotEffective)
        }
    }
}

/// Body of a `ServiceAcknowledgment` (0x0284).
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct Acknowledgment {
    pub task: TaskId,
    pub request: RequestId,
    pub request_commitment: Digest32,
    pub metadata: MetadataDigest,
    pub model: Digest32,
    pub deployment: Digest32,
    pub generation: u64,
    pub key_version: u64,
    pub sequence: u64,
    pub task_deadline: u64,
    pub admitted_height: u64,
}
impl Acknowledgment {
    #[must_use]
    pub fn encode(&self) -> Vec<u8> {
        let mut out = Vec::with_capacity(ACKNOWLEDGMENT_BYTES);
        out.extend_from_slice(&SCHEMA_VERSION.to_be_bytes());
        for digest in [
            self.task.as_bytes(),
            self.request.as_bytes(),
            self.request_commitment.as_bytes(),
            self.metadata.as_bytes(),
            self.model.as_bytes(),
            self.deployment.as_bytes(),
        ] {
            out.extend_from_slice(digest);
        }
        for value in [
            self.generation,
            self.key_version,
            self.sequence,
            self.task_deadline,
            self.admitted_height,
        ] {
            out.extend_from_slice(&value.to_be_bytes());
        }
        out
    }
    /// # Errors
    /// `UnsupportedVersion` for another schema; `NonCanonical` for short, trailing or zero bytes.
    pub fn decode(body: &[u8]) -> Result<Self, ServiceError> {
        let mut r = Reader::new(body);
        if r.u16()? != SCHEMA_VERSION {
            return Err(ServiceError::UnsupportedVersion);
        }
        let value = Self {
            task: TaskId::new(r.fixed()?)?,
            request: RequestId::new(r.fixed()?)?,
            request_commitment: Digest32::new(r.fixed()?)?,
            metadata: MetadataDigest::new(r.fixed()?)?,
            model: Digest32::new(r.fixed()?)?,
            deployment: Digest32::new(r.fixed()?)?,
            generation: r.u64()?,
            key_version: r.u64()?,
            sequence: r.u64()?,
            task_deadline: r.u64()?,
            admitted_height: r.u64()?,
        };
        r.finish()?;
        if value.sequence == 0 {
            return Err(ServiceError::NonCanonical);
        }
        Ok(value)
    }
}

impl Admission {
    #[must_use]
    pub const fn acknowledgment(&self, sequence: u64) -> Acknowledgment {
        Acknowledgment {
            task: self.task,
            request: self.context.request,
            request_commitment: self.request_commitment,
            metadata: self.metadata,
            model: self.model,
            deployment: self.deployment,
            generation: self.generation,
            key_version: self.key_version,
            sequence,
            task_deadline: self.task_deadline,
            admitted_height: self.admitted_height,
        }
    }
    /// Signs the acknowledgment with the frozen delegate key; `sequence` is the durable
    /// monotonically increasing `service_job_sequence`.
    ///
    /// # Errors
    /// `NonCanonical` for sequence 0; `BadSignature` when `key` is not the admitted delegate.
    pub fn sign_acknowledgment(
        &self,
        sequence: u64,
        key: &SigningKey,
    ) -> Result<Vec<u8>, ServiceError> {
        if sequence == 0 {
            return Err(ServiceError::NonCanonical);
        }
        if key.verifying_key().to_bytes() != self.delegate.0 {
            return Err(ServiceError::BadSignature);
        }
        let context = ServiceContext {
            actor: self.owner,
            sequence,
            expiry: self.task_deadline,
            ..self.context
        };
        encode_service(
            ServiceOperation::Acknowledgment,
            &context,
            &self.acknowledgment(sequence).encode(),
            key,
        )
    }
    /// Query and cancel authorization over this admission. Query admits the original
    /// customer or an expressly granted evaluator; cancel admits only the customer.
    ///
    /// # Errors
    /// `NotFound` when the reference names another request; `AccessDenied` for any other
    /// principal; `IdempotencyConflict` when the reference changes an admitted parameter.
    pub fn authorize(
        &self,
        actor: PrincipalId,
        route: Route,
        reference: &JobReference,
        granted: &[PrincipalId],
    ) -> Result<(), ServiceError> {
        if reference.original != self.context.request || reference.receiver != self.worker {
            return Err(ServiceError::NotFound);
        }
        let customer = actor == self.context.actor;
        let allowed = match route {
            Route::Query => customer || granted.contains(&actor),
            Route::Cancel => customer,
            Route::Submit => false,
        };
        if !allowed {
            return Err(ServiceError::AccessDenied);
        }
        if reference.task != self.task
            || reference.generation != self.generation
            || reference.key_version != self.key_version
            || reference.metadata_revision != self.metadata_revision
        {
            return Err(ServiceError::IdempotencyConflict);
        }
        Ok(())
    }
}

/// Decodes a signed acknowledgment and checks it was signed by the frozen delegate.
///
/// # Errors
/// Envelope refusals of [`decode_service`]; `NonCanonical` for another operation;
/// `BadSignature` for another signer or a request id differing from the envelope's.
pub fn decode_acknowledgment(
    bytes: &[u8],
    delegate: PublicKey32,
) -> Result<(Acknowledgment, ServiceEnvelope), ServiceError> {
    let envelope = decode_service(bytes)?;
    if envelope.operation != ServiceOperation::Acknowledgment {
        return Err(ServiceError::NonCanonical);
    }
    if envelope.signer != delegate {
        return Err(ServiceError::BadSignature);
    }
    let acknowledgment = Acknowledgment::decode(&envelope.payload)?;
    if acknowledgment.request != envelope.context.request
        || acknowledgment.sequence != envelope.context.sequence
    {
        return Err(ServiceError::BadSignature);
    }
    Ok((acknowledgment, envelope))
}

/// `acknowledgment_digest = H("PAXAI/worker-ack/v1", complete signed acknowledgment bytes)`.
///
/// # Errors
/// `NonCanonical` when the digest is all zero.
pub fn acknowledgment_digest(signed: &[u8]) -> Result<Digest32, ServiceError> {
    Ok(codec::domain_hash(ACKNOWLEDGMENT_DOMAIN, signed)?)
}

/// Status probe body posted to the status route.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct StatusChallenge {
    pub epoch: u64,
    pub config: u64,
    pub roster: Presence<RosterDigest>,
    pub expiry: u64,
    pub challenge: RequestId,
}
impl StatusChallenge {
    #[must_use]
    pub fn encode(&self) -> Vec<u8> {
        let mut out = Vec::with_capacity(STATUS_CHALLENGE_BYTES);
        out.extend_from_slice(&self.epoch.to_be_bytes());
        out.extend_from_slice(&self.config.to_be_bytes());
        out.extend_from_slice(&presence_bytes(self.roster));
        out.extend_from_slice(&self.expiry.to_be_bytes());
        out.extend_from_slice(self.challenge.as_bytes());
        out
    }
    /// # Errors
    /// `NonCanonical` for short, trailing or zero bytes, or a zero config or expiry.
    pub fn decode(body: &[u8]) -> Result<Self, ServiceError> {
        let mut r = Reader::new(body);
        let value = Self {
            epoch: r.u64()?,
            config: r.u64()?,
            roster: read_roster(&mut r)?,
            expiry: r.u64()?,
            challenge: RequestId::new(r.fixed()?)?,
        };
        r.finish()?;
        if value.config == 0 || value.expiry == 0 {
            return Err(ServiceError::NonCanonical);
        }
        Ok(value)
    }
}

/// Signed `ServiceStatus` (0x0286) payload: the worker's binding and API version range.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct StatusReport {
    pub worker: WorkerId,
    pub generation: u64,
    pub key_version: u64,
    pub metadata_revision: u64,
    pub min_api: u16,
    pub max_api: u16,
}
impl StatusReport {
    fn encode(&self) -> Vec<u8> {
        let mut out = Vec::with_capacity(STATUS_REPORT_BYTES);
        out.extend_from_slice(self.worker.as_bytes());
        out.extend_from_slice(&self.generation.to_be_bytes());
        out.extend_from_slice(&self.key_version.to_be_bytes());
        out.extend_from_slice(&self.metadata_revision.to_be_bytes());
        out.extend_from_slice(&self.min_api.to_be_bytes());
        out.extend_from_slice(&self.max_api.to_be_bytes());
        out
    }
    fn decode(body: &[u8]) -> Result<Self, ServiceError> {
        let mut r = Reader::new(body);
        let value = Self {
            worker: WorkerId::new(r.fixed()?)?,
            generation: r.u64()?,
            key_version: r.u64()?,
            metadata_revision: r.u64()?,
            min_api: r.u16()?,
            max_api: r.u16()?,
        };
        r.finish()?;
        Ok(value)
    }
}

/// Worker side of the status probe: echoes the challenge context, signed by the delegate.
///
/// # Errors
/// `BadSignature` when `key` is not the bound delegate; envelope encoding refusals.
pub fn sign_status(
    binding: &WorkerBinding,
    challenge: &StatusChallenge,
    key: &SigningKey,
) -> Result<Vec<u8>, ServiceError> {
    if key.verifying_key().to_bytes() != binding.delegate.0 {
        return Err(ServiceError::BadSignature);
    }
    let context = ServiceContext {
        chain: binding.chain,
        program: binding.program,
        market: binding.market,
        actor: binding.owner,
        epoch: challenge.epoch,
        config: challenge.config,
        roster: challenge.roster,
        sequence: 0,
        expiry: challenge.expiry,
        request: challenge.challenge,
    };
    let report = StatusReport {
        worker: binding.worker,
        generation: binding.generation,
        key_version: binding.key_version,
        metadata_revision: binding.metadata_revision,
        min_api: API_VERSION,
        max_api: API_VERSION,
    };
    encode_service(ServiceOperation::Status, &context, &report.encode(), key)
}

/// Client side of the status probe: the authenticated API version check of `VERIFIED_TRANSPORT`.
///
/// # Errors
/// Envelope refusals; `NonCanonical` for another operation; `BadSignature` for another signer
/// or actor; `WrongDomain` for another market or worker; `StaleAuthority` when the echoed
/// challenge differs; `WrongGeneration`, `WrongRevision`; `UnsupportedVersion` when the API
/// range excludes version 1.
pub fn verify_status(
    signed: &[u8],
    binding: &WorkerBinding,
    challenge: &StatusChallenge,
) -> Result<StatusReport, ServiceError> {
    let envelope = decode_service(signed)?;
    if envelope.operation != ServiceOperation::Status {
        return Err(ServiceError::NonCanonical);
    }
    if envelope.signer != binding.delegate || envelope.context.actor != binding.owner {
        return Err(ServiceError::BadSignature);
    }
    binding.check_domain(&envelope.context)?;
    let c = envelope.context;
    if c.request != challenge.challenge
        || c.epoch != challenge.epoch
        || c.config != challenge.config
        || c.roster != challenge.roster
        || c.expiry != challenge.expiry
    {
        return Err(ServiceError::StaleAuthority);
    }
    let report = StatusReport::decode(&envelope.payload)?;
    binding.check_receiver(report.worker)?;
    binding.check_versions(
        report.generation,
        report.key_version,
        report.metadata_revision,
    )?;
    if !(report.min_api..=report.max_api).contains(&API_VERSION) {
        return Err(ServiceError::UnsupportedVersion);
    }
    Ok(report)
}

/// The context a signed manifest must verify against.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct MetadataContext {
    pub chain: ChainDomain,
    pub program: ProgramId,
    pub market: MarketId,
    pub worker: WorkerId,
    pub owner: PrincipalId,
    pub delegate: PublicKey32,
}

/// The common C03 fields of one `PublishMetadata` submission.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct MetadataPublication<'a> {
    pub manifest: &'a [u8],
    pub expected_revision: u64,
    pub epoch: u64,
    pub config: u64,
    pub roster: Presence<RosterDigest>,
    pub sequence: u64,
    pub expiry: u64,
    pub request: RequestId,
}

/// A delegate-signed manifest after full grammar, context and signature verification.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct SignedMetadata {
    pub context: MetadataContext,
    pub expected_revision: u64,
    pub manifest: Manifest,
    pub digest: MetadataDigest,
    pub request: RequestId,
}
impl SignedMetadata {
    /// The worker binding this signed manifest declares.
    #[must_use]
    pub const fn binding(&self) -> WorkerBinding {
        WorkerBinding {
            chain: self.context.chain,
            program: self.context.program,
            market: self.context.market,
            worker: self.context.worker,
            owner: self.context.owner,
            delegate: self.context.delegate,
            generation: self.manifest.generation,
            key_version: self.manifest.key_version,
            metadata_revision: self.manifest.revision,
        }
    }
}

fn publish_payload(
    context: &MetadataContext,
    publication: &MetadataPublication<'_>,
) -> Result<Vec<u8>, ServiceError> {
    let (manifest, digest) = Manifest::decode(publication.manifest)?;
    let length =
        u32::try_from(publication.manifest.len()).map_err(|_| ServiceError::CapacityExceeded)?;
    let mut payload = Vec::with_capacity(PUBLISH_METADATA_FIXED_BYTES + publication.manifest.len());
    payload.extend_from_slice(context.worker.as_bytes());
    payload.extend_from_slice(&publication.expected_revision.to_be_bytes());
    payload.extend_from_slice(&manifest.revision.to_be_bytes());
    payload.extend_from_slice(digest.as_bytes());
    payload.extend_from_slice(&manifest.valid_from.to_be_bytes());
    payload.extend_from_slice(&manifest.expiry.to_be_bytes());
    payload.extend_from_slice(&length.to_be_bytes());
    payload.extend_from_slice(publication.manifest);
    Ok(payload)
}

/// Delegate-signs a canonical manifest as a native `PublishMetadata` C03 envelope.
///
/// # Errors
/// `BadSignature` when `key` is not the context delegate; manifest grammar refusals;
/// envelope validation refusals of the common codec.
pub fn sign_metadata(
    context: &MetadataContext,
    publication: &MetadataPublication<'_>,
    key: &SigningKey,
) -> Result<Vec<u8>, ServiceError> {
    let delegate = PublicKey32(key.verifying_key().to_bytes());
    if delegate != context.delegate {
        return Err(ServiceError::BadSignature);
    }
    let payload = publish_payload(context, publication)?;
    let mut envelope = Envelope {
        operation: dispatch::PublishMetadata,
        chain: context.chain,
        program: context.program,
        market: context.market,
        actor: context.owner,
        epoch: publication.epoch,
        config: publication.config,
        roster: publication.roster,
        sequence: publication.sequence,
        expiry: publication.expiry,
        request: publication.request,
        payload: &payload,
        authentication: Authentication::Delegate {
            key: delegate,
            signature: Signature64([0; 64]),
        },
    };
    let mut out =
        vec![0; codec::ENVELOPE_PREFIX_BYTES + payload.len() + NATIVE_DELEGATE_SUFFIX_BYTES];
    let n = codec::encode_envelope(&envelope, &mut out)?;
    let digest = codec::decode_envelope(out.get(..n).ok_or(ServiceError::NonCanonical)?)?
        .request_digest()?;
    envelope.authentication = Authentication::Delegate {
        key: delegate,
        signature: Signature64(key.sign(digest.as_bytes()).to_bytes()),
    };
    let n = codec::encode_envelope(&envelope, &mut out)?;
    out.truncate(n);
    Ok(out)
}

/// Verifies a delegate-signed `PublishMetadata` envelope against `context`.
///
/// # Errors
/// Common envelope refusals; `NonCanonical` for another operation; `WrongDomain` for another
/// chain, program, market or worker; `DelegateConsentRequired` for native authentication;
/// `OwnerRequired` when the actor is not the owner; `BadSignature` for another key or a failed
/// signature; `CapacityExceeded` above the manifest cap; manifest grammar refusals;
/// `MetadataIntegrityFailure` when the manifest disagrees with the signed payload or context;
/// `WrongRevision` when the revision is not exactly the expected revision plus one.
pub fn verify_signed_metadata(
    signed: &[u8],
    context: &MetadataContext,
) -> Result<SignedMetadata, ServiceError> {
    let validated = codec::decode_envelope(signed)?;
    let envelope = validated.envelope;
    if envelope.operation != dispatch::PublishMetadata {
        return Err(ServiceError::NonCanonical);
    }
    envelope.check_domain(context.chain, context.program, context.market)?;
    let Authentication::Delegate { key, signature } = envelope.authentication else {
        return Err(ServiceError::DelegateConsentRequired);
    };
    if envelope.actor != context.owner {
        return Err(ServiceError::OwnerRequired);
    }
    if key != context.delegate {
        return Err(ServiceError::BadSignature);
    }
    verify(
        key,
        signature,
        Digest32::new(validated.request_digest()?.bytes())?,
    )?;
    let mut r = Reader::new(envelope.payload);
    if WorkerId::new(r.fixed()?)? != context.worker {
        return Err(ServiceError::WrongDomain);
    }
    let expected_revision = r.u64()?;
    let revision = r.u64()?;
    let digest = MetadataDigest::new(r.fixed()?)?;
    let valid_from = r.u64()?;
    let expiry = r.u64()?;
    let bytes = r.bytes(MAX_MANIFEST_BYTES)?;
    r.finish()?;
    let (manifest, actual) = Manifest::decode(bytes)?;
    if actual != digest
        || manifest.market != context.market
        || manifest.worker != context.worker
        || manifest.owner != context.owner
        || manifest.revision != revision
        || manifest.valid_from != valid_from
        || manifest.expiry != expiry
    {
        return Err(ServiceError::MetadataIntegrityFailure);
    }
    if expected_revision.checked_add(1) != Some(revision) {
        return Err(ServiceError::WrongRevision);
    }
    Ok(SignedMetadata {
        context: *context,
        expected_revision,
        manifest,
        digest,
        request: envelope.request,
    })
}
