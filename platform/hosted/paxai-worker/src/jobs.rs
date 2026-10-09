//! F02-R009/R017/R023/R024 job lifecycle: atomic admission with a durable signed
//! acknowledgment, dispatch only after finalized F01 acceptance under a fenced lease, runner
//! evidence validation, `UNKNOWN_EXECUTION` reconciliation, private retrieval and late results.
//!
//! `ServiceResult` body: `schema:u16=1 || market32 || worker32 || task32 || request32 ||
//! request_commitment32 || metadata32 || generation:u64 || key_version:u64 || epoch:u64 ||
//! deployment32 || model32 || outcome:u8 || output_commitment presence:u8 (+32) ||
//! output_bytes:u32 || input_units:u64 || output_units:u64 || processing_ms:u64 ||
//! started_at_ms:u64 || finished_at_ms:u64 || error_code:u16 || evidence presence:u8 (+32)`,
//! carried in a `JobResult` (0x0285) service envelope signed by the admitted delegate.
use crate::auth::{
    acknowledgment_digest, admit, decode_service, encode_service, verify_request, Admission,
    AuthenticatedRequest, JobReference, Route, ServiceContext, ServiceEnvelope, ServiceError,
    ServiceOperation, ServiceRequest,
};
use crate::discovery::{FinalizedAuthority, IdentityEvidence, Readiness, VerifiedMetadata};
use crate::runner::{
    Bounds, Completion, Description, Dispatch, ProcessRunner, Progress, Report, RunnerError,
};
use crate::store::{
    Change, Job, JobKey, JobState, JobStore, NewJob, StateRecord, Terminal, Txn,
    MAX_ENCRYPTED_BYTES, MAX_QUEUED, MAX_RUNNING,
};
use ed25519_dalek::SigningKey;
use layerx_programs_ai_market::{
    codec::{self, Reader},
    evidence::{
        check_manifest_context, decode_manifest, manifest_root, object_content_root,
        ArtifactContext, ArtifactKind, ArtifactManifest, Privacy, SubjectContext,
    },
    tasks::{TaskBinding, TaskStatus},
    types::{
        Digest32, MarketId, MetadataDigest, PrincipalId, PublicKey32, RequestId, TaskId, WorkerId,
    },
    SCHEMA_VERSION,
};

pub const RESULT_DOMAIN: &str = "PAXAI/worker-result/v1";
pub const EVIDENCE_DOMAIN: &str = "PAXAI/worker-evidence/v1";
pub const REPLAY_DOMAIN: &str = "PAXAI/worker-replay/v1";
/// Lease slack past the admitted latency budget before a RUNNING job becomes unknown.
pub const LEASE_GRACE_MS: u64 = 30_000;

/// R009 outcomes.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
#[repr(u8)]
pub enum Outcome {
    Succeeded = 1,
    Failed = 2,
    CancelledBeforeStart = 3,
    ExpiredBeforeStart = 4,
    UnknownExecution = 5,
}
impl Outcome {
    #[must_use]
    pub const fn code(self) -> u8 {
        self as u8
    }
    /// # Errors
    /// `NonCanonical` outside 1..=5.
    pub const fn from_code(code: u8) -> Result<Self, ServiceError> {
        Ok(match code {
            1 => Self::Succeeded,
            2 => Self::Failed,
            3 => Self::CancelledBeforeStart,
            4 => Self::ExpiredBeforeStart,
            5 => Self::UnknownExecution,
            _ => return Err(ServiceError::NonCanonical),
        })
    }
}

/// R009 `ServiceResult` body.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct ServiceResult {
    pub market: MarketId,
    pub worker: WorkerId,
    pub task: TaskId,
    pub request: RequestId,
    pub request_commitment: Digest32,
    pub metadata: MetadataDigest,
    pub generation: u64,
    pub key_version: u64,
    pub epoch: u64,
    pub deployment: Digest32,
    pub model: Digest32,
    pub outcome: Outcome,
    pub output_commitment: Option<Digest32>,
    pub output_bytes: u32,
    pub input_units: u64,
    pub output_units: u64,
    pub processing_ms: u64,
    pub started_at_ms: u64,
    pub finished_at_ms: u64,
    pub error_code: u16,
    pub evidence: Option<Digest32>,
}

fn put_option(out: &mut Vec<u8>, value: Option<Digest32>) {
    match value {
        None => out.push(0),
        Some(digest) => {
            out.push(1);
            out.extend_from_slice(digest.as_bytes());
        }
    }
}

fn read_option(r: &mut Reader<'_>) -> Result<Option<Digest32>, ServiceError> {
    match r.u8()? {
        0 => Ok(None),
        1 => Ok(Some(Digest32::new(r.fixed()?)?)),
        _ => Err(ServiceError::NonCanonical),
    }
}

impl ServiceResult {
    /// The admitted bindings with `outcome` and no usage, output or evidence.
    #[must_use]
    pub const fn admitted(admission: &Admission, outcome: Outcome) -> Self {
        Self {
            market: admission.context.market,
            worker: admission.worker,
            task: admission.task,
            request: admission.context.request,
            request_commitment: admission.request_commitment,
            metadata: admission.metadata,
            generation: admission.generation,
            key_version: admission.key_version,
            epoch: admission.context.epoch,
            deployment: admission.deployment,
            model: admission.model,
            outcome,
            output_commitment: None,
            output_bytes: 0,
            input_units: 0,
            output_units: 0,
            processing_ms: 0,
            started_at_ms: 0,
            finished_at_ms: 0,
            error_code: 0,
            evidence: None,
        }
    }

    #[must_use]
    pub fn encode(&self) -> Vec<u8> {
        let mut out = Vec::new();
        out.extend_from_slice(&SCHEMA_VERSION.to_be_bytes());
        for digest in [
            self.market.as_bytes(),
            self.worker.as_bytes(),
            self.task.as_bytes(),
            self.request.as_bytes(),
            self.request_commitment.as_bytes(),
            self.metadata.as_bytes(),
        ] {
            out.extend_from_slice(digest);
        }
        for value in [self.generation, self.key_version, self.epoch] {
            out.extend_from_slice(&value.to_be_bytes());
        }
        out.extend_from_slice(self.deployment.as_bytes());
        out.extend_from_slice(self.model.as_bytes());
        out.push(self.outcome.code());
        put_option(&mut out, self.output_commitment);
        out.extend_from_slice(&self.output_bytes.to_be_bytes());
        for value in [
            self.input_units,
            self.output_units,
            self.processing_ms,
            self.started_at_ms,
            self.finished_at_ms,
        ] {
            out.extend_from_slice(&value.to_be_bytes());
        }
        out.extend_from_slice(&self.error_code.to_be_bytes());
        put_option(&mut out, self.evidence);
        out
    }

    /// # Errors
    /// `UnsupportedVersion` for another schema; `NonCanonical` for malformed bytes.
    pub fn decode(bytes: &[u8]) -> Result<Self, ServiceError> {
        let mut r = Reader::new(bytes);
        if r.u16()? != SCHEMA_VERSION {
            return Err(ServiceError::UnsupportedVersion);
        }
        let value = Self {
            market: MarketId::new(r.fixed()?)?,
            worker: WorkerId::new(r.fixed()?)?,
            task: TaskId::new(r.fixed()?)?,
            request: RequestId::new(r.fixed()?)?,
            request_commitment: Digest32::new(r.fixed()?)?,
            metadata: MetadataDigest::new(r.fixed()?)?,
            generation: r.u64()?,
            key_version: r.u64()?,
            epoch: r.u64()?,
            deployment: Digest32::new(r.fixed()?)?,
            model: Digest32::new(r.fixed()?)?,
            outcome: Outcome::from_code(r.u8()?)?,
            output_commitment: read_option(&mut r)?,
            output_bytes: r.u32()?,
            input_units: r.u64()?,
            output_units: r.u64()?,
            processing_ms: r.u64()?,
            started_at_ms: r.u64()?,
            finished_at_ms: r.u64()?,
            error_code: r.u16()?,
            evidence: read_option(&mut r)?,
        };
        r.finish()?;
        Ok(value)
    }
}

/// Signs a result body with the admitted delegate; `sequence` is the next
/// `service_job_sequence`.
///
/// # Errors
/// `BadSignature` when `key` is not the admitted delegate; envelope refusals.
pub fn sign_result(
    admission: &Admission,
    sequence: u64,
    body: &ServiceResult,
    key: &SigningKey,
) -> Result<Vec<u8>, ServiceError> {
    if key.verifying_key().to_bytes() != admission.delegate.0 {
        return Err(ServiceError::BadSignature);
    }
    let context = ServiceContext {
        actor: admission.owner,
        sequence,
        expiry: admission.task_deadline,
        ..admission.context
    };
    encode_service(ServiceOperation::JobResult, &context, &body.encode(), key)
}

/// Decodes a signed result manifest and checks the delegate signed it.
///
/// # Errors
/// Envelope refusals; `NonCanonical` for another operation; `BadSignature` for another signer
/// or a request id differing from the envelope's.
pub fn decode_result(
    signed: &[u8],
    delegate: PublicKey32,
) -> Result<(ServiceResult, ServiceEnvelope), ServiceError> {
    let envelope = decode_service(signed)?;
    if envelope.operation != ServiceOperation::JobResult {
        return Err(ServiceError::NonCanonical);
    }
    if envelope.signer != delegate {
        return Err(ServiceError::BadSignature);
    }
    let body = ServiceResult::decode(&envelope.payload)?;
    if body.request != envelope.context.request {
        return Err(ServiceError::BadSignature);
    }
    Ok((body, envelope))
}

/// F01 `result_manifest_digest = H("PAXAI/worker-result/v1", complete signed result manifest)`.
///
/// # Errors
/// `NonCanonical` for an all-zero digest.
pub fn result_manifest_digest(signed: &[u8]) -> Result<Digest32, ServiceError> {
    Ok(codec::domain_hash(RESULT_DOMAIN, signed)?)
}

/// Digest of a raw runner report retained as evidence.
///
/// # Errors
/// `NonCanonical` for an all-zero digest.
pub fn evidence_digest(report: &[u8]) -> Result<Digest32, ServiceError> {
    Ok(codec::domain_hash(EVIDENCE_DOMAIN, report)?)
}

/// Transport replay marker over (actor, delegate generation, actor sequence, operation).
///
/// # Errors
/// `NonCanonical` for an all-zero digest.
pub fn replay_marker(
    context: &ServiceContext,
    generation: u64,
    operation: ServiceOperation,
) -> Result<Digest32, ServiceError> {
    let mut bytes = Vec::with_capacity(50);
    bytes.extend_from_slice(context.actor.as_bytes());
    bytes.extend_from_slice(&generation.to_be_bytes());
    bytes.extend_from_slice(&context.sequence.to_be_bytes());
    bytes.extend_from_slice(&operation.code().to_be_bytes());
    Ok(codec::domain_hash(REPLAY_DOMAIN, &bytes)?)
}

/// The private input of a submit: its F09 INPUT manifest and the encrypted payload.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct Input<'a> {
    pub manifest: &'a [u8],
    pub payload: &'a [u8],
}

/// The finalized facts one submit is admitted against.
#[derive(Clone, Copy, Debug)]
pub struct AdmissionView<'a> {
    pub authority: &'a FinalizedAuthority,
    pub customer: &'a IdentityEvidence,
    pub metadata: &'a VerifiedMetadata,
    pub task: &'a TaskBinding,
}

/// What a submit returns: the saved acknowledgment and, for a resolved duplicate, the result.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Receipt {
    pub key: JobKey,
    pub acknowledgment: Vec<u8>,
    pub result: Option<Vec<u8>>,
}

/// Execution ownership of one job under one fencing token.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct Lease {
    pub key: JobKey,
    pub fence: u64,
}

/// Where an authorized reader finds the encrypted F09 RESULT object.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct ResultLocator {
    pub output_commitment: Digest32,
    pub output_bytes: u32,
}

/// An authorized query answer.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct QueryAnswer {
    pub key: JobKey,
    pub state: JobState,
    pub acknowledgment: Vec<u8>,
    pub result: Option<Vec<u8>>,
    pub locator: Option<ResultLocator>,
}

fn check_actor(
    envelope: &ServiceEnvelope,
    actor: &IdentityEvidence,
    height: u64,
) -> Result<(), ServiceError> {
    if actor.execution_height != height {
        return Err(ServiceError::StaleAuthority);
    }
    if actor.principal != envelope.context.actor || actor.primary_key != envelope.signer {
        return Err(ServiceError::BadSignature);
    }
    if actor.frozen {
        return Err(ServiceError::IdentityFrozen);
    }
    Ok(())
}

fn artifact_context(admission: &Admission, request: &ServiceRequest) -> ArtifactContext {
    ArtifactContext {
        chain: admission.context.chain,
        program: admission.context.program,
        market: admission.context.market,
        policy: request.workload_policy,
    }
}

/// Binds an F09 object to its manifest: decoded root, kind, publisher, task context, length
/// and content root. Returns the manifest root.
fn bind_object<'a>(
    encoded: &'a [u8],
    object: &[u8],
    kind: ArtifactKind,
    publisher: PrincipalId,
    context: &ArtifactContext,
    subject: SubjectContext,
) -> Result<(Digest32, ArtifactManifest<'a>), ServiceError> {
    let manifest = decode_manifest(encoded).map_err(|_| ServiceError::NonCanonical)?;
    let root = manifest_root(encoded).map_err(|_| ServiceError::NonCanonical)?;
    if manifest.kind != kind {
        return Err(ServiceError::NonCanonical);
    }
    if manifest.publisher != publisher {
        return Err(ServiceError::AccessDenied);
    }
    check_manifest_context(&manifest, context, subject).map_err(|_| ServiceError::WrongDomain)?;
    let length = u64::try_from(object.len()).map_err(|_| ServiceError::Overflow)?;
    if length != manifest.byte_length {
        return Err(ServiceError::NonCanonical);
    }
    let chunks = usize::try_from(manifest.chunk_count).map_err(|_| ServiceError::Overflow)?;
    let mut scratch = vec![[0; 32]; chunks];
    let computed =
        object_content_root(object, &mut scratch).map_err(|_| ServiceError::NonCanonical)?;
    if computed != manifest.content_root {
        return Err(ServiceError::BadSignature);
    }
    Ok((Digest32::new(root.bytes())?, manifest))
}

/// F02-R017 private input binding: the INPUT manifest root is the signed input commitment, the
/// customer published it encrypted for this task, and the payload is exactly its object.
fn bind_input(
    envelope: &ServiceEnvelope,
    admission: &Admission,
    request: &ServiceRequest,
    input: Input<'_>,
) -> Result<(), ServiceError> {
    let declared = usize::try_from(request.payload_bytes).map_err(|_| ServiceError::Overflow)?;
    if input.payload.len() > declared || input.payload.len() > MAX_ENCRYPTED_BYTES {
        return Err(ServiceError::InputTooLarge);
    }
    if input.payload.len() < declared {
        return Err(ServiceError::NonCanonical);
    }
    let (root, manifest) = bind_object(
        input.manifest,
        input.payload,
        ArtifactKind::Input,
        envelope.context.actor,
        &artifact_context(admission, request),
        SubjectContext::Task {
            epoch: envelope.context.epoch,
            task: request.task,
        },
    )?;
    if manifest.privacy != Privacy::Encrypted {
        return Err(ServiceError::AccessDenied);
    }
    if root != request.input_commitment {
        return Err(ServiceError::BadSignature);
    }
    Ok(())
}

/// F01 acceptance matching this job and its acknowledgment commitment.
fn check_acceptance(job: &Job, task: &TaskBinding) -> Result<(), ServiceError> {
    let admission = &job.record.admission;
    let request = job.record.request()?;
    if task.task != admission.task
        || task.requester != admission.context.actor
        || task.worker != admission.worker
        || task.input != request.input_commitment
        || task.deadline != admission.task_deadline
    {
        return Err(ServiceError::StaleAuthority);
    }
    match task.status {
        TaskStatus::Admitted | TaskStatus::Cancelled => Err(ServiceError::AdmissionNotEffective),
        TaskStatus::ResultCommitted => Err(ServiceError::IdempotencyConflict),
        TaskStatus::Accepted => {
            if task.acknowledgement == Some(acknowledgment_digest(&job.record.acknowledgment)?) {
                Ok(())
            } else {
                Err(ServiceError::StaleAuthority)
            }
        }
    }
}

/// Not started before its latency budget or the canonical task deadline: it never may.
fn past_deadline(job: &Job, height: u64, now_ms: u64) -> Result<bool, ServiceError> {
    let admission = &job.record.admission;
    let budget = job
        .record
        .accepted_ms
        .checked_add(u64::from(admission.deadline_ms))
        .ok_or(ServiceError::Overflow)?;
    Ok(height >= admission.task_deadline || now_ms >= budget)
}

fn with_state(job: &Job, state: JobState, now_ms: u64) -> StateRecord {
    StateRecord {
        state,
        lease_until_ms: 0,
        changed_ms: now_ms,
        ..job.state
    }
}

/// One worker replica: the shared store, its runner and the delegate key.
pub struct JobService {
    store: JobStore,
    runner: ProcessRunner,
    key: SigningKey,
}

impl JobService {
    #[must_use]
    pub const fn new(store: JobStore, runner: ProcessRunner, key: SigningKey) -> Self {
        Self { store, runner, key }
    }
    #[must_use]
    pub const fn store(&self) -> &JobStore {
        &self.store
    }
    #[must_use]
    pub const fn runner(&self) -> &ProcessRunner {
        &self.runner
    }

    /// F02-R017/R023 `SubmitJob`. An exact duplicate returns the saved acknowledgment and any
    /// result whatever the envelope freshness, provided the customer is still authorized; new
    /// work passes every admission check and the private input binding, records the replay
    /// marker and commits atomically before any acknowledgment exists. The runner is never
    /// contacted.
    ///
    /// # Errors
    /// Admission refusals of [`admit`]; input binding refusals; `IdempotencyConflict` for
    /// another commitment, a replayed marker or a tombstoned key; `CapacityExceeded`;
    /// `ExecutionUnavailable` when the store cannot commit.
    pub fn submit(
        &self,
        signed: &[u8],
        input: Input<'_>,
        view: &AdmissionView<'_>,
        now_ms: u64,
    ) -> Result<Receipt, ServiceError> {
        let AdmissionView {
            authority,
            customer,
            metadata,
            task,
        } = *view;
        let envelope = decode_service(signed)?;
        if envelope.operation != ServiceOperation::SubmitJob {
            return Err(ServiceError::NonCanonical);
        }
        authority.worker_binding().check_domain(&envelope.context)?;
        let request = ServiceRequest::decode(&envelope.payload)?;
        let context = envelope.context;
        let key = JobKey::derive(context.market, context.actor, context.request)?;
        if let Some(saved) = self.store.find(key)? {
            authority.release_gate()?;
            check_actor(&envelope, customer, authority.height())?;
            if saved.record.admission.request_commitment != envelope.digest {
                return Err(ServiceError::IdempotencyConflict);
            }
            return Ok(Receipt {
                key,
                acknowledgment: saved.record.acknowledgment,
                result: saved.result,
            });
        }
        let admission = admit(&envelope, &request, authority, customer, metadata, task)?;
        let capability = metadata.manifest().capability(request.capability)?;
        bind_input(&envelope, &admission, &request, input)?;
        self.store.mark(
            replay_marker(&context, request.generation, ServiceOperation::SubmitJob)?,
            envelope.digest,
            context.expiry,
            now_ms,
        )?;
        let job = self.store.insert(
            &NewJob {
                key,
                admission,
                request: signed,
                input_manifest: input.manifest,
                payload: input.payload,
                unit_kind: capability.unit_kind,
                max_input_units: capability.max_input_units,
                accepted_ms: now_ms,
            },
            |sequence| admission.sign_acknowledgment(sequence, &self.key),
        )?;
        Ok(Receipt {
            key,
            acknowledgment: job.record.acknowledgment,
            result: job.result,
        })
    }

    /// F02-R024 execution ownership: only an ACCEPTED job whose finalized F01 task is Accepted
    /// with this job's acknowledgment commitment, inside its deadlines, the admitted epoch and
    /// roster, and below the running bound, gets a lease with a fresh fencing token. A job past
    /// its deadline is resolved `EXPIRED_BEFORE_START`.
    ///
    /// # Errors
    /// `IdentityFrozen`/`DelegateRevoked`; `DeadlineInvalid` after expiry; `StaleAuthority` for
    /// another epoch, roster, task or acknowledgment; `AdmissionNotEffective` while acceptance
    /// is absent; `IdempotencyConflict` when already claimed or final; `UnknownExecution`
    /// while unresolved; `CapacityExceeded` at `MAX_RUNNING`.
    pub fn claim(
        &self,
        key: JobKey,
        task: &TaskBinding,
        authority: &FinalizedAuthority,
        now_ms: u64,
    ) -> Result<Lease, ServiceError> {
        authority.release_gate()?;
        self.store.update(key, |job, txn| {
            match job.state.state {
                JobState::Accepted => {}
                JobState::Ended(Outcome::UnknownExecution) => {
                    return Err(ServiceError::UnknownExecution)
                }
                JobState::Running | JobState::Ended(_) => {
                    return Err(ServiceError::IdempotencyConflict)
                }
            }
            let admission = &job.record.admission;
            if past_deadline(job, authority.height(), now_ms)? {
                let body = ServiceResult::admitted(admission, Outcome::ExpiredBeforeStart);
                let change = self.terminal(job, txn, &body, None, None, now_ms)?;
                return Ok((change, Err(ServiceError::DeadlineInvalid)));
            }
            if Some(admission.context.epoch) != authority.current_epoch()
                || admission.context.roster != authority.roster()
            {
                return Err(ServiceError::StaleAuthority);
            }
            check_acceptance(job, task)?;
            if txn.usage()?.running >= MAX_RUNNING {
                return Err(ServiceError::CapacityExceeded);
            }
            let fence = txn.next_fence()?;
            let lease_until_ms = now_ms
                .checked_add(u64::from(admission.deadline_ms))
                .and_then(|until| until.checked_add(LEASE_GRACE_MS))
                .ok_or(ServiceError::Overflow)?;
            let state = StateRecord {
                state: JobState::Running,
                fence,
                lease_until_ms,
                changed_ms: now_ms,
                ..job.state
            };
            Ok((
                Change {
                    state: Some(state),
                    ..Change::default()
                },
                Ok(Lease { key, fence }),
            ))
        })?
    }

    /// Runs a leased job once. A runner that never started returns the job to ACCEPTED under
    /// the same identity; an ambiguous dispatch leaves `UNKNOWN_EXECUTION`.
    ///
    /// # Errors
    /// `IdempotencyConflict` for a stale lease; store and signing refusals.
    pub fn run(&self, lease: Lease, now_ms: u64) -> Result<JobState, ServiceError> {
        let job = self.store.job(lease.key)?;
        if job.state.state != JobState::Running || job.state.fence != lease.fence {
            return Err(ServiceError::IdempotencyConflict);
        }
        let request = job.record.request()?;
        let payload = self.store.payload(lease.key)?;
        let admission = &job.record.admission;
        let dispatch = Dispatch {
            job: lease.key,
            fence: lease.fence,
            model: admission.model,
            deployment: admission.deployment,
            capability: admission.capability,
            unit_kind: job.record.unit_kind,
            max_output_bytes: request.max_output_bytes,
            max_input_units: job.record.max_input_units,
            max_units: request.max_units,
            deadline_ms: admission.deadline_ms,
            result_key: request.result_key,
            input_manifest: &job.record.input_manifest,
            payload: &payload,
        };
        match self.runner.dispatch(&dispatch) {
            Ok(report) => self.settle(lease, &report, now_ms, false),
            Err(RunnerError::Unavailable) => {
                self.transition(lease, JobState::Running, JobState::Accepted, now_ms)
            }
            Err(RunnerError::Ambiguous) => self.transition(
                lease,
                JobState::Running,
                JobState::Ended(Outcome::UnknownExecution),
                now_ms,
            ),
        }
    }

    /// Claim and run.
    ///
    /// # Errors
    /// Refusals of [`Self::claim`] and [`Self::run`].
    pub fn dispatch(
        &self,
        key: JobKey,
        task: &TaskBinding,
        authority: &FinalizedAuthority,
        now_ms: u64,
    ) -> Result<JobState, ServiceError> {
        let lease = self.claim(key, task, authority, now_ms)?;
        self.run(lease, now_ms)
    }

    fn transition(
        &self,
        lease: Lease,
        from: JobState,
        to: JobState,
        now_ms: u64,
    ) -> Result<JobState, ServiceError> {
        self.store.update(lease.key, |job, _| {
            if job.state.state != from || job.state.fence != lease.fence {
                return Ok((Change::default(), job.state.state));
            }
            Ok((
                Change {
                    state: Some(with_state(job, to, now_ms)),
                    ..Change::default()
                },
                to,
            ))
        })
    }

    /// Delivers a runner report for `lease` (dispatch answer, lost callback or lookup). A
    /// finished attempt becomes one signed SUCCEEDED or FAILED result with its evidence; the
    /// same report again is redelivery; contradictory evidence is retained as a conflict.
    ///
    /// # Errors
    /// `IdempotencyConflict` for a stale lease or contradictory evidence; `NonCanonical` for a
    /// malformed report; store and signing refusals.
    pub fn complete(
        &self,
        lease: Lease,
        report: &[u8],
        now_ms: u64,
    ) -> Result<JobState, ServiceError> {
        self.settle(lease, report, now_ms, false)
    }

    fn settle(
        &self,
        lease: Lease,
        report: &[u8],
        now_ms: u64,
        retry_safe: bool,
    ) -> Result<JobState, ServiceError> {
        let decoded = Report::decode(report);
        let digest = evidence_digest(report)?;
        self.store.update(lease.key, |job, txn| {
            let conflict = Change {
                conflict: Some(report.to_vec()),
                ..Change::default()
            };
            let parsed = match decoded {
                Ok(parsed) if parsed.job == lease.key && parsed.fence == lease.fence => parsed,
                Ok(_) => return Ok((conflict, Err(ServiceError::IdempotencyConflict))),
                Err(error) => return Ok((conflict, Err(error))),
            };
            if let Some(saved) = &job.result {
                let (body, _) = decode_result(saved, job.record.admission.delegate)?;
                return Ok(if body.evidence == Some(digest) {
                    (Change::default(), Ok(job.state.state))
                } else {
                    (conflict, Err(ServiceError::IdempotencyConflict))
                });
            }
            let unknown = job.state.state == JobState::Ended(Outcome::UnknownExecution);
            if job.state.fence != lease.fence || !(unknown || job.state.state == JobState::Running)
            {
                return Ok((conflict, Err(ServiceError::IdempotencyConflict)));
            }
            match parsed.progress {
                Progress::Running => Ok((Change::default(), Ok(job.state.state))),
                Progress::NotReceived if unknown && !retry_safe => Ok((
                    Change {
                        evidence: Some(report.to_vec()),
                        ..Change::default()
                    },
                    Ok(job.state.state),
                )),
                Progress::NotReceived => Ok((
                    Change {
                        state: Some(with_state(job, JobState::Accepted, now_ms)),
                        evidence: Some(report.to_vec()),
                        ..Change::default()
                    },
                    Ok(JobState::Accepted),
                )),
                Progress::Finished(completion) => {
                    let (body, output) = Self::outcome(job, &completion, digest)?;
                    let change =
                        self.terminal(job, txn, &body, output, Some(report.to_vec()), now_ms)?;
                    Ok((change, Ok(JobState::Ended(body.outcome))))
                }
            }
        })?
    }

    /// Validated result body of a finished attempt and the output to retain.
    fn outcome(
        job: &Job,
        completion: &Completion,
        evidence: Digest32,
    ) -> Result<(ServiceResult, Option<Vec<u8>>), ServiceError> {
        let admission = &job.record.admission;
        let request = job.record.request()?;
        let mut body = ServiceResult {
            started_at_ms: completion.started_at_ms,
            finished_at_ms: completion.finished_at_ms,
            evidence: Some(evidence),
            ..ServiceResult::admitted(admission, Outcome::Failed)
        };
        let bounds = Bounds {
            unit_kind: job.record.unit_kind,
            max_output_bytes: request.max_output_bytes,
            max_input_units: job.record.max_input_units,
            max_units: request.max_units,
        };
        let usage = match completion.validate(&bounds) {
            Ok(usage) => usage,
            Err(fault) => {
                body.error_code = fault.error().code();
                return Ok((body, None));
            }
        };
        body.input_units = usage.input_units;
        body.output_units = usage.output_units;
        body.processing_ms = usage.processing_ms;
        if !completion.succeeded {
            body.error_code = completion.error_code;
            return Ok((body, None));
        }
        let bound = bind_object(
            &completion.output_manifest,
            &completion.output,
            ArtifactKind::Result,
            admission.owner,
            &artifact_context(admission, &request),
            SubjectContext::Task {
                epoch: admission.context.epoch,
                task: admission.task,
            },
        );
        match bound {
            Ok((root, _)) => {
                body.outcome = Outcome::Succeeded;
                body.output_commitment = Some(root);
                body.output_bytes =
                    u32::try_from(completion.output.len()).map_err(|_| ServiceError::Overflow)?;
                Ok((body, Some(completion.output.clone())))
            }
            Err(error) => {
                body.error_code = error.code();
                Ok((body, None))
            }
        }
    }

    fn terminal(
        &self,
        job: &Job,
        txn: &mut Txn<'_>,
        body: &ServiceResult,
        output: Option<Vec<u8>>,
        evidence: Option<Vec<u8>>,
        now_ms: u64,
    ) -> Result<Change, ServiceError> {
        let sequence = txn.next_sequence()?;
        let result = sign_result(&job.record.admission, sequence, body, &self.key)?;
        Ok(Change {
            state: Some(with_state(job, JobState::Ended(body.outcome), now_ms)),
            terminal: Some(Terminal {
                outcome: body.outcome,
                result,
                output,
            }),
            evidence,
            conflict: None,
        })
    }

    /// F02-R024 restart reconciliation: RUNNING jobs past their lease become
    /// `UNKNOWN_EXECUTION`; every unknown job is looked up under its original admission
    /// identity. Finished evidence resolves it; `NOT_RECEIVED` returns it to ACCEPTED only for a
    /// runner with admission-id idempotency or fenced acknowledgment; anything else keeps it
    /// unknown. Returns every active job with its state afterwards.
    ///
    /// # Errors
    /// Store and signing refusals.
    pub fn reconcile(&self, now_ms: u64) -> Result<Vec<(JobKey, JobState)>, ServiceError> {
        let mut description: Option<Option<Description>> = None;
        let mut out = Vec::new();
        for (key, state) in self.store.active()? {
            let lease = Lease {
                key,
                fence: state.fence,
            };
            let mut current = state.state;
            if current == JobState::Running && state.lease_until_ms <= now_ms {
                current = self.transition(
                    lease,
                    JobState::Running,
                    JobState::Ended(Outcome::UnknownExecution),
                    now_ms,
                )?;
            }
            if current == JobState::Ended(Outcome::UnknownExecution) {
                if let Ok(report) = self.runner.lookup(key) {
                    let retry_safe = description
                        .get_or_insert_with(|| self.runner.describe().ok())
                        .as_ref()
                        .is_some_and(Description::retry_safe);
                    current = match self.settle(lease, &report, now_ms, retry_safe) {
                        Ok(state) => state,
                        Err(ServiceError::IdempotencyConflict | ServiceError::NonCanonical) => {
                            current
                        }
                        Err(error) => return Err(error),
                    };
                }
            }
            out.push((key, current));
        }
        Ok(out)
    }

    /// Resolves ACCEPTED jobs past their latency budget or task deadline as
    /// `EXPIRED_BEFORE_START`. Returns how many expired.
    ///
    /// # Errors
    /// Store and signing refusals.
    pub fn expire(
        &self,
        authority: &FinalizedAuthority,
        now_ms: u64,
    ) -> Result<usize, ServiceError> {
        let mut expired = 0;
        for (key, state) in self.store.active()? {
            if state.state != JobState::Accepted {
                continue;
            }
            let done = self.store.update(key, |job, txn| {
                if job.state.state != JobState::Accepted
                    || !past_deadline(job, authority.height(), now_ms)?
                {
                    return Ok((Change::default(), false));
                }
                let body =
                    ServiceResult::admitted(&job.record.admission, Outcome::ExpiredBeforeStart);
                Ok((self.terminal(job, txn, &body, None, None, now_ms)?, true))
            })?;
            expired += usize::from(done);
        }
        Ok(expired)
    }

    fn authorized(
        &self,
        signed: &[u8],
        route: Route,
        authority: &FinalizedAuthority,
        actor: &IdentityEvidence,
        granted: &[PrincipalId],
        now_ms: u64,
    ) -> Result<Job, ServiceError> {
        let envelope = decode_service(signed)?;
        let reference: JobReference =
            match verify_request(&envelope, route, &authority.worker_binding())? {
                AuthenticatedRequest::Query(reference)
                | AuthenticatedRequest::Cancel(reference) => reference,
                AuthenticatedRequest::Submit(_) => return Err(ServiceError::NonCanonical),
            };
        check_actor(&envelope, actor, authority.height())?;
        if envelope.context.expiry <= authority.height() {
            return Err(ServiceError::DeadlineInvalid);
        }
        self.store.mark(
            replay_marker(&envelope.context, reference.generation, route.operation())?,
            envelope.digest,
            envelope.context.expiry,
            now_ms,
        )?;
        let mut refusal = ServiceError::NotFound;
        for job in self
            .store
            .by_request(envelope.context.market, reference.original)?
        {
            match job
                .record
                .admission
                .authorize(actor.principal, route, &reference, granted)
            {
                Ok(()) => return Ok(job),
                Err(ServiceError::NotFound) => {}
                Err(error) => refusal = error,
            }
        }
        Err(refusal)
    }

    /// F02-R012/R024 private retrieval: the customer or an expressly granted evaluator gets
    /// the state, acknowledgment, signed result and output locator; nothing is released while
    /// the worker owner is frozen or the worker revoked.
    ///
    /// # Errors
    /// Envelope and binding refusals; `NotFound` for an unknown request; `AccessDenied` for
    /// any other principal (no locator); `IdentityFrozen`/`DelegateRevoked`; replay refusals.
    pub fn query(
        &self,
        signed: &[u8],
        authority: &FinalizedAuthority,
        actor: &IdentityEvidence,
        granted: &[PrincipalId],
        now_ms: u64,
    ) -> Result<QueryAnswer, ServiceError> {
        let job = self.authorized(signed, Route::Query, authority, actor, granted, now_ms)?;
        authority.release_gate()?;
        let locator = match &job.result {
            Some(saved) => {
                let (body, _) = decode_result(saved, job.record.admission.delegate)?;
                body.output_commitment
                    .map(|output_commitment| ResultLocator {
                        output_commitment,
                        output_bytes: body.output_bytes,
                    })
            }
            None => None,
        };
        Ok(QueryAnswer {
            key: job.record.key,
            state: job.state.state,
            acknowledgment: job.record.acknowledgment,
            result: job.result,
            locator,
        })
    }

    /// Customer cancel: ACCEPTED becomes a signed `CANCELLED_BEFORE_START`; RUNNING records the
    /// request and asks the runner to stop but stays RUNNING, since the attempt has started;
    /// any other state is returned unchanged.
    ///
    /// # Errors
    /// Envelope, binding and authorization refusals; store and signing refusals.
    pub fn cancel(
        &self,
        signed: &[u8],
        authority: &FinalizedAuthority,
        actor: &IdentityEvidence,
        now_ms: u64,
    ) -> Result<JobState, ServiceError> {
        let job = self.authorized(signed, Route::Cancel, authority, actor, &[], now_ms)?;
        let key = job.record.key;
        let (state, fence) = self.store.update(key, |job, txn| match job.state.state {
            JobState::Accepted => {
                let body =
                    ServiceResult::admitted(&job.record.admission, Outcome::CancelledBeforeStart);
                let change = self.terminal(job, txn, &body, None, None, now_ms)?;
                Ok((change, (JobState::Ended(Outcome::CancelledBeforeStart), 0)))
            }
            JobState::Running => Ok((
                Change {
                    state: Some(StateRecord {
                        cancel_requested: true,
                        ..job.state
                    }),
                    ..Change::default()
                },
                (JobState::Running, job.state.fence),
            )),
            JobState::Ended(outcome) => Ok((Change::default(), (JobState::Ended(outcome), 0))),
        })?;
        if state == JobState::Running {
            // Cooperative only: the answer carries no authority over the job's state.
            let _ = self.runner.cancel(key, fence);
        }
        Ok(state)
    }

    /// F01 result commitment input for a final job. Reconciliation of already admitted work
    /// continues under an owner freeze. At or after the task deadline the result and output
    /// stay retained for diagnosis, the job is marked late and the commitment refuses.
    ///
    /// # Errors
    /// `UnknownExecution` while unresolved; `AdmissionNotEffective` before a final result;
    /// `DeadlineInvalid` when late; store refusals.
    pub fn commit_record(
        &self,
        key: JobKey,
        authority: &FinalizedAuthority,
    ) -> Result<Digest32, ServiceError> {
        self.store.update(key, |job, _| {
            let Some(saved) = &job.result else {
                return Err(match job.state.state {
                    JobState::Ended(Outcome::UnknownExecution) => ServiceError::UnknownExecution,
                    _ => ServiceError::AdmissionNotEffective,
                });
            };
            if authority.height() >= job.record.admission.task_deadline {
                let state = StateRecord {
                    late: true,
                    ..job.state
                };
                return Ok((
                    Change {
                        state: Some(state),
                        ..Change::default()
                    },
                    Err(ServiceError::DeadlineInvalid),
                ));
            }
            Ok((Change::default(), result_manifest_digest(saved)))
        })?
    }

    /// F02-R020 readiness for new admissions: `NOT_READY` without an admissible finalized view,
    /// with an unavailable store, a saturated queue or a runner that does not have every
    /// advertised model loaded under the manifest deployment; `transport` caps the result.
    #[must_use]
    pub fn readiness(
        &self,
        transport: Readiness,
        authority: &FinalizedAuthority,
        metadata: &VerifiedMetadata,
    ) -> Readiness {
        if authority.admission_gate().is_err() {
            return Readiness::NotReady;
        }
        match self.store.usage() {
            Ok(usage) if usage.queued < MAX_QUEUED && usage.running < MAX_RUNNING => {}
            _ => return Readiness::NotReady,
        }
        if transport != Readiness::VerifiedTransport && transport != Readiness::RunnerReady {
            return transport;
        }
        let manifest = metadata.manifest();
        let loaded = self.runner.describe().is_ok_and(|description| {
            manifest.capabilities.iter().all(|capability| {
                Digest32::new(capability.model)
                    .is_ok_and(|model| description.serves(model, manifest.deployment))
            })
        });
        if loaded {
            Readiness::RunnerReady
        } else {
            Readiness::NotReady
        }
    }

    /// Retention purge; see [`JobStore::purge`].
    ///
    /// # Errors
    /// Store refusals.
    pub fn purge(&self, now_ms: u64, height: u64) -> Result<usize, ServiceError> {
        self.store.purge(now_ms, height)
    }
}
