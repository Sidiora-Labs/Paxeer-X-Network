//! F02-R022/R024 model runner process boundary and usage evidence validation.
//!
//! The runner is an external executable run once per verb as `<program> <args..> <verb>` with
//! the request on stdin and one report on stdout. Exit 0 carries a well-formed answer, exit 75
//! means the request was refused before anything started, any other exit, a timeout or a
//! malformed answer is ambiguous. Every message starts `PAXRUN1 || version:u16=1`.
//! `dispatch`: `job32 || fence:u64 || model32 || deployment32 || capability32 || unit_kind:u8 ||
//! max_output_bytes:u32 || max_input_units:u32 || max_units:u32 || deadline_ms:u32 ||
//! result_key presence:u8 (+32) || input_manifest u32-len || payload u32-len`.
//! `lookup`: `job32`. `cancel`: `job32 || fence:u64`. All three answer with a report:
//! `job32 || fence:u64 || status:u8` (1 `NOT_RECEIVED`, 2 RUNNING, 3 SUCCEEDED, 4 FAILED), and for
//! 3 and 4 `error_code:u16 || start_ns:u64 || finish_ns:u64 || started_at_ms:u64 ||
//! finished_at_ms:u64 || segment_count:u8 || (role:u8 || unit_kind:u8 || count:u64)* ||
//! output u32-len || output_manifest u32-len`; role 1 counts input units, role 2 output units.
//! `describe` (empty request) answers `guarantees:u8 || count:u8 || (model32 || deployment32)*`;
//! guarantee bit 0 is admission-id idempotency, bit 1 durable fenced acknowledgment.
use crate::auth::{ServiceError, RESULT_KEY_BYTES};
use crate::store::{JobKey, MAX_ENCRYPTED_BYTES, MAX_REFERENCE_BYTES};
use layerx_programs_ai_market::{codec::Reader, errors::CodecResult, types::Digest32};
use std::ffi::OsString;
use std::io::{Read, Write};
use std::path::PathBuf;
use std::process::{Command, Stdio};
use std::sync::atomic::{AtomicU64, Ordering};
use std::thread;
use std::time::{Duration, Instant};

pub const MAGIC: &[u8; 7] = b"PAXRUN1";
pub const VERSION: u16 = 1;
pub const TOKENS: u8 = 1;
pub const VECTOR_ELEMENTS: u8 = 2;
pub const ITEMS: u8 = 3;
pub const ADMISSION_IDEMPOTENT: u8 = 1;
pub const FENCED_ACKNOWLEDGMENT: u8 = 2;
pub const MAX_SEGMENTS: usize = 16;
pub const MAX_MODELS: usize = 16;
pub const MAX_OUTPUT_BYTES: usize = MAX_ENCRYPTED_BYTES;
pub const MAX_REPORT_BYTES: usize = MAX_OUTPUT_BYTES + MAX_REFERENCE_BYTES + 1_024;
/// Exit status of a runner that refused a request before starting anything.
pub const EXIT_NOT_STARTED: i32 = 75;
const POLL: Duration = Duration::from_millis(5);
const NOT_RECEIVED: u8 = 1;
const RUNNING: u8 = 2;
const SUCCEEDED: u8 = 3;
const FAILED: u8 = 4;
const INPUT_ROLE: u8 = 1;
const OUTPUT_ROLE: u8 = 2;

fn header(out: &mut Vec<u8>) {
    out.extend_from_slice(MAGIC);
    out.extend_from_slice(&VERSION.to_be_bytes());
}

fn read_header(r: &mut Reader<'_>) -> CodecResult<()> {
    if r.take(MAGIC.len())? != MAGIC || r.u16()? != VERSION {
        return Err(layerx_programs_ai_market::errors::NON_CANONICAL);
    }
    Ok(())
}

fn put_bytes(out: &mut Vec<u8>, bytes: &[u8]) -> Result<(), ServiceError> {
    let length = u32::try_from(bytes.len()).map_err(|_| ServiceError::Overflow)?;
    out.extend_from_slice(&length.to_be_bytes());
    out.extend_from_slice(bytes);
    Ok(())
}

/// One dispatch under the admission identity `job` and the lease fence.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct Dispatch<'a> {
    pub job: JobKey,
    pub fence: u64,
    pub model: Digest32,
    pub deployment: Digest32,
    pub capability: Digest32,
    pub unit_kind: u8,
    pub max_output_bytes: u32,
    pub max_input_units: u32,
    pub max_units: u32,
    pub deadline_ms: u32,
    pub result_key: Option<[u8; RESULT_KEY_BYTES]>,
    pub input_manifest: &'a [u8],
    pub payload: &'a [u8],
}
impl Dispatch<'_> {
    /// # Errors
    /// `Overflow` when a byte string does not fit its length prefix.
    pub fn encode(&self) -> Result<Vec<u8>, ServiceError> {
        let mut out = Vec::new();
        header(&mut out);
        out.extend_from_slice(self.job.digest().as_bytes());
        out.extend_from_slice(&self.fence.to_be_bytes());
        for digest in [self.model, self.deployment, self.capability] {
            out.extend_from_slice(digest.as_bytes());
        }
        out.push(self.unit_kind);
        for value in [
            self.max_output_bytes,
            self.max_input_units,
            self.max_units,
            self.deadline_ms,
        ] {
            out.extend_from_slice(&value.to_be_bytes());
        }
        match self.result_key {
            None => out.push(0),
            Some(key) => {
                out.push(1);
                out.extend_from_slice(&key);
            }
        }
        put_bytes(&mut out, self.input_manifest)?;
        put_bytes(&mut out, self.payload)?;
        Ok(out)
    }
}

/// Which side of the execution a usage segment counts.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum Role {
    Input,
    Output,
}

/// One integer count of the pinned tokenizer or preprocessor, in one unit kind.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct Segment {
    pub role: Role,
    pub unit_kind: u8,
    pub count: u64,
}

/// A finished attempt as the runner reports it; nothing in it is trusted before `validate`.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Completion {
    pub succeeded: bool,
    pub error_code: u16,
    pub start_ns: u64,
    pub finish_ns: u64,
    pub started_at_ms: u64,
    pub finished_at_ms: u64,
    pub segments: Vec<Segment>,
    pub output: Vec<u8>,
    pub output_manifest: Vec<u8>,
}

/// Runner progress for one admission identity.
#[derive(Clone, Debug, Eq, PartialEq)]
pub enum Progress {
    NotReceived,
    Running,
    Finished(Completion),
}

/// One runner report.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Report {
    pub job: JobKey,
    pub fence: u64,
    pub progress: Progress,
}
impl Report {
    /// The runner side of the protocol.
    ///
    /// # Errors
    /// `NonCanonical` for more than `MAX_SEGMENTS` segments, a success with an error code, a
    /// failure without one or with output; `Overflow` for oversized byte strings.
    pub fn encode(&self) -> Result<Vec<u8>, ServiceError> {
        let mut out = Vec::new();
        header(&mut out);
        out.extend_from_slice(self.job.digest().as_bytes());
        out.extend_from_slice(&self.fence.to_be_bytes());
        let completion = match &self.progress {
            Progress::NotReceived => {
                out.push(NOT_RECEIVED);
                return Ok(out);
            }
            Progress::Running => {
                out.push(RUNNING);
                return Ok(out);
            }
            Progress::Finished(completion) => completion,
        };
        completion.check()?;
        out.push(if completion.succeeded {
            SUCCEEDED
        } else {
            FAILED
        });
        out.extend_from_slice(&completion.error_code.to_be_bytes());
        for value in [
            completion.start_ns,
            completion.finish_ns,
            completion.started_at_ms,
            completion.finished_at_ms,
        ] {
            out.extend_from_slice(&value.to_be_bytes());
        }
        out.push(u8::try_from(completion.segments.len()).map_err(|_| ServiceError::NonCanonical)?);
        for segment in &completion.segments {
            out.push(match segment.role {
                Role::Input => INPUT_ROLE,
                Role::Output => OUTPUT_ROLE,
            });
            out.push(segment.unit_kind);
            out.extend_from_slice(&segment.count.to_be_bytes());
        }
        put_bytes(&mut out, &completion.output)?;
        put_bytes(&mut out, &completion.output_manifest)?;
        Ok(out)
    }

    /// # Errors
    /// `NonCanonical` for any malformed, oversized or inconsistent report.
    pub fn decode(bytes: &[u8]) -> Result<Self, ServiceError> {
        if bytes.len() > MAX_REPORT_BYTES {
            return Err(ServiceError::NonCanonical);
        }
        Self::read(bytes).map_err(|_| ServiceError::NonCanonical)
    }

    fn read(bytes: &[u8]) -> Result<Self, ServiceError> {
        let mut r = Reader::new(bytes);
        read_header(&mut r)?;
        let job = JobKey::from_digest(Digest32::new(r.fixed()?)?);
        let fence = r.u64()?;
        let progress = match r.u8()? {
            NOT_RECEIVED => Progress::NotReceived,
            RUNNING => Progress::Running,
            status @ (SUCCEEDED | FAILED) => {
                let error_code = r.u16()?;
                let (start_ns, finish_ns) = (r.u64()?, r.u64()?);
                let (started_at_ms, finished_at_ms) = (r.u64()?, r.u64()?);
                let count = usize::from(r.u8()?);
                if count > MAX_SEGMENTS {
                    return Err(ServiceError::NonCanonical);
                }
                let mut segments = Vec::with_capacity(count);
                for _ in 0..count {
                    let role = match r.u8()? {
                        INPUT_ROLE => Role::Input,
                        OUTPUT_ROLE => Role::Output,
                        _ => return Err(ServiceError::NonCanonical),
                    };
                    segments.push(Segment {
                        role,
                        unit_kind: r.u8()?,
                        count: r.u64()?,
                    });
                }
                let completion = Completion {
                    succeeded: status == SUCCEEDED,
                    error_code,
                    start_ns,
                    finish_ns,
                    started_at_ms,
                    finished_at_ms,
                    segments,
                    output: r.bytes(MAX_OUTPUT_BYTES)?.to_vec(),
                    output_manifest: r.bytes(MAX_REFERENCE_BYTES)?.to_vec(),
                };
                completion.check()?;
                Progress::Finished(completion)
            }
            _ => return Err(ServiceError::NonCanonical),
        };
        r.finish()?;
        Ok(Self {
            job,
            fence,
            progress,
        })
    }
}

/// Admitted bounds a completion must satisfy.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct Bounds {
    pub unit_kind: u8,
    pub max_output_bytes: u32,
    pub max_input_units: u32,
    pub max_units: u32,
}

/// Validated integer usage of one completed attempt.
#[derive(Clone, Copy, Debug, Default, Eq, PartialEq)]
pub struct Usage {
    pub input_units: u64,
    pub output_units: u64,
    pub processing_ms: u64,
}

/// Why runner evidence is invalid; an invalid completion never becomes SUCCEEDED.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum EvidenceFault {
    MeasurementReversed,
    CountOverflow,
    UnitMismatch,
    OutputTooLarge,
    UnitsAboveBound,
    InputUnitsAboveBound,
}
impl EvidenceFault {
    /// The `error_code` a FAILED result carries for this fault.
    #[must_use]
    pub const fn error(self) -> ServiceError {
        match self {
            Self::MeasurementReversed => ServiceError::NonCanonical,
            Self::CountOverflow => ServiceError::Overflow,
            Self::UnitMismatch => ServiceError::CapabilityMismatch,
            Self::OutputTooLarge | Self::UnitsAboveBound => ServiceError::OutputTooLarge,
            Self::InputUnitsAboveBound => ServiceError::InputTooLarge,
        }
    }
}

/// `processing_ms = floor((finish_ns - start_ns) / 1_000_000)`; a reversed sample fails.
///
/// # Errors
/// `MeasurementReversed` when `finish_ns < start_ns`.
pub const fn processing_ms(start_ns: u64, finish_ns: u64) -> Result<u64, EvidenceFault> {
    match finish_ns.checked_sub(start_ns) {
        Some(elapsed) => Ok(elapsed / 1_000_000),
        None => Err(EvidenceFault::MeasurementReversed),
    }
}

impl Completion {
    fn check(&self) -> Result<(), ServiceError> {
        let consistent = if self.succeeded {
            self.error_code == 0 && !self.output_manifest.is_empty()
        } else {
            ServiceError::from_code(self.error_code).is_ok()
                && self.output.is_empty()
                && self.output_manifest.is_empty()
        };
        if consistent && self.segments.len() <= MAX_SEGMENTS {
            Ok(())
        } else {
            Err(ServiceError::NonCanonical)
        }
    }

    /// Checks the measurement and every count against the admitted bounds with checked u64.
    ///
    /// # Errors
    /// The first [`EvidenceFault`] found.
    pub fn validate(&self, bounds: &Bounds) -> Result<Usage, EvidenceFault> {
        let processing_ms = processing_ms(self.start_ns, self.finish_ns)?;
        let (mut input_units, mut output_units) = (0u64, 0u64);
        for segment in &self.segments {
            if segment.unit_kind != bounds.unit_kind {
                return Err(EvidenceFault::UnitMismatch);
            }
            let total = match segment.role {
                Role::Input => &mut input_units,
                Role::Output => &mut output_units,
            };
            *total = total
                .checked_add(segment.count)
                .ok_or(EvidenceFault::CountOverflow)?;
        }
        let output_bytes =
            u64::try_from(self.output.len()).map_err(|_| EvidenceFault::OutputTooLarge)?;
        if output_bytes > u64::from(bounds.max_output_bytes) {
            return Err(EvidenceFault::OutputTooLarge);
        }
        if output_units > u64::from(bounds.max_units) {
            return Err(EvidenceFault::UnitsAboveBound);
        }
        if input_units > u64::from(bounds.max_input_units) {
            return Err(EvidenceFault::InputUnitsAboveBound);
        }
        Ok(Usage {
            input_units,
            output_units,
            processing_ms,
        })
    }
}

/// Runner guarantees and the loaded (model, deployment) pairs.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Description {
    pub guarantees: u8,
    pub loaded: Vec<(Digest32, Digest32)>,
}
impl Description {
    /// # Errors
    /// `NonCanonical` for unknown guarantee bits, too many pairs or malformed bytes.
    pub fn decode(bytes: &[u8]) -> Result<Self, ServiceError> {
        let mut r = Reader::new(bytes);
        read_header(&mut r)?;
        let guarantees = r.u8()?;
        let count = usize::from(r.u8()?);
        if guarantees & !(ADMISSION_IDEMPOTENT | FENCED_ACKNOWLEDGMENT) != 0 || count > MAX_MODELS {
            return Err(ServiceError::NonCanonical);
        }
        let mut loaded = Vec::with_capacity(count);
        for _ in 0..count {
            loaded.push((Digest32::new(r.fixed()?)?, Digest32::new(r.fixed()?)?));
        }
        r.finish()?;
        Ok(Self { guarantees, loaded })
    }
    /// A runner with admission-id idempotency or fenced acknowledgment may be retried after an
    /// ambiguous dispatch it reports as never received.
    #[must_use]
    pub const fn retry_safe(&self) -> bool {
        self.guarantees != 0
    }
    #[must_use]
    pub fn serves(&self, model: Digest32, deployment: Digest32) -> bool {
        self.loaded.contains(&(model, deployment))
    }
}

/// How a runner call failed.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum RunnerError {
    /// Nothing started: the executable could not run or refused before starting.
    Unavailable,
    /// The runner may have acted; only its later evidence can tell.
    Ambiguous,
}

/// The runner executable; every call is one child process.
#[derive(Debug)]
pub struct ProcessRunner {
    program: PathBuf,
    args: Vec<OsString>,
    control_timeout: Duration,
    invocations: AtomicU64,
}
impl ProcessRunner {
    #[must_use]
    pub const fn new(program: PathBuf, args: Vec<OsString>, control_timeout: Duration) -> Self {
        Self {
            program,
            args,
            control_timeout,
            invocations: AtomicU64::new(0),
        }
    }
    /// Calls attempted through this handle, of any verb.
    #[must_use]
    pub fn invocations(&self) -> u64 {
        self.invocations.load(Ordering::SeqCst)
    }

    /// # Errors
    /// Call failures; `Ambiguous` for a malformed description.
    pub fn describe(&self) -> Result<Description, RunnerError> {
        let mut request = Vec::new();
        header(&mut request);
        let bytes = self.invoke("describe", request, self.control_timeout)?;
        Description::decode(&bytes).map_err(|_| RunnerError::Ambiguous)
    }

    /// Dispatches one job; returns the raw report, retained verbatim as evidence.
    ///
    /// # Errors
    /// `Unavailable` when nothing started; `Ambiguous` when the runner may have started.
    pub fn dispatch(&self, request: &Dispatch<'_>) -> Result<Vec<u8>, RunnerError> {
        let bytes = request.encode().map_err(|_| RunnerError::Unavailable)?;
        let timeout = self
            .control_timeout
            .saturating_add(Duration::from_millis(u64::from(request.deadline_ms)));
        self.invoke("dispatch", bytes, timeout)
    }

    /// Looks a job up under its original admission identity.
    ///
    /// # Errors
    /// Call failures.
    pub fn lookup(&self, job: JobKey) -> Result<Vec<u8>, RunnerError> {
        let mut request = Vec::new();
        header(&mut request);
        request.extend_from_slice(job.digest().as_bytes());
        self.invoke("lookup", request, self.control_timeout)
    }

    /// Requests cooperative cancellation of the attempt holding `fence`.
    ///
    /// # Errors
    /// Call failures.
    pub fn cancel(&self, job: JobKey, fence: u64) -> Result<Vec<u8>, RunnerError> {
        let mut request = Vec::new();
        header(&mut request);
        request.extend_from_slice(job.digest().as_bytes());
        request.extend_from_slice(&fence.to_be_bytes());
        self.invoke("cancel", request, self.control_timeout)
    }

    fn invoke(
        &self,
        verb: &str,
        input: Vec<u8>,
        timeout: Duration,
    ) -> Result<Vec<u8>, RunnerError> {
        self.invocations.fetch_add(1, Ordering::SeqCst);
        let mut child = Command::new(&self.program)
            .args(&self.args)
            .arg(verb)
            .stdin(Stdio::piped())
            .stdout(Stdio::piped())
            .stderr(Stdio::null())
            .spawn()
            .map_err(|_| RunnerError::Unavailable)?;
        let stdin = child.stdin.take();
        let stdout = child.stdout.take();
        let writer = thread::spawn(move || stdin.map(|mut pipe| pipe.write_all(&input)));
        let reader = thread::spawn(move || {
            let mut out = Vec::new();
            stdout.map(|pipe| {
                pipe.take(
                    u64::try_from(MAX_REPORT_BYTES)
                        .unwrap_or(u64::MAX)
                        .saturating_add(1),
                )
                .read_to_end(&mut out)
                .map(|_| out)
            })
        });
        let started = Instant::now();
        let status = loop {
            match child.try_wait() {
                Ok(Some(status)) => break status,
                Ok(None) if started.elapsed() < timeout => thread::sleep(POLL),
                _ => {
                    // The reader is detached: a runner descendant may still hold the pipe.
                    let _ = child.kill();
                    let _ = child.wait();
                    return Err(RunnerError::Ambiguous);
                }
            }
        };
        if status.code() == Some(EXIT_NOT_STARTED) {
            return Err(RunnerError::Unavailable);
        }
        let written = writer.join();
        let output = reader.join();
        match (status.success(), written, output) {
            (true, Ok(Some(Ok(()))), Ok(Some(Ok(bytes)))) if bytes.len() <= MAX_REPORT_BYTES => {
                Ok(bytes)
            }
            _ => Err(RunnerError::Ambiguous),
        }
    }
}
