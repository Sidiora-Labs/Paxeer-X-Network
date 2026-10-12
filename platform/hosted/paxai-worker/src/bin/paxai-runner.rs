//! Model runner executable serving the PAXRUN1 protocol of `layerx_paxai_worker::runner`.
//!
//! Run as `<program> <verb>` with the request on stdin; one report goes to stdout, diagnostics
//! to stderr. Exit 75 means the request was refused before anything started.
//!
//! One durable journal per admission identity lives under `PAXAI_RUNNER_STATE`, replaced
//! atomically (write, sync, rename, directory sync) under a per-identity lock. A journaled
//! dispatch answers its recorded report and never executes again.
//!
//! The pinned directory of a (model, deployment) pair is
//! `PAXAI_RUNNER_MODELS/<model hex>/<deployment hex>`; its `engine` executable runs once per
//! dispatch with the directory as working directory, the dispatch request on stdin and the
//! engine answer on stdout, killed at the dispatch deadline.
use layerx_paxai_worker::{
    auth::ServiceError,
    runner::{
        Completion, Description, Dispatch, Execution, Progress, Report, ADMISSION_IDEMPOTENT,
        EXIT_NOT_STARTED, FENCED_ACKNOWLEDGMENT, MAGIC, MAX_DISPATCH_BYTES, MAX_MODELS,
        MAX_REPORT_BYTES, VERSION,
    },
    store::JobKey,
};
use layerx_programs_ai_market::{codec::Reader, queries::parse_hex32, types::Digest32};
use std::ffi::OsString;
use std::fmt::Write as _;
use std::fs::{self, File, OpenOptions};
use std::io::{self, ErrorKind, Read, Write};
use std::path::{Path, PathBuf};
use std::process::{Command, ExitCode, Stdio};
use std::thread;
use std::time::{Duration, Instant, SystemTime, UNIX_EPOCH};

const STATE_ENV: &str = "PAXAI_RUNNER_STATE";
const MODELS_ENV: &str = "PAXAI_RUNNER_MODELS";
const ENGINE: &str = "engine";
const POLL: Duration = Duration::from_millis(5);
const HEADER_BYTES: usize = 9;
const MAX_ENGINE_ANSWER_BYTES: usize = MAX_REPORT_BYTES;
const NO_DISPATCH: u8 = 0;
const JOURNAL_RUNNING: u8 = 1;
const JOURNAL_FINISHED: u8 = 2;

/// How a verb ended without a report.
enum Exit {
    /// Nothing started; the worker may dispatch again.
    NotStarted(&'static str),
    /// Something may have started; the worker treats the call as ambiguous.
    Failed(&'static str),
}

fn main() -> ExitCode {
    match run() {
        Ok(report) => {
            let mut stdout = io::stdout().lock();
            if stdout
                .write_all(&report)
                .and_then(|()| stdout.flush())
                .is_ok()
            {
                ExitCode::SUCCESS
            } else {
                ExitCode::FAILURE
            }
        }
        Err(Exit::NotStarted(reason)) => {
            let _ = writeln!(io::stderr(), "paxai-runner: refused: {reason}");
            ExitCode::from(u8::try_from(EXIT_NOT_STARTED).unwrap_or(u8::MAX))
        }
        Err(Exit::Failed(reason)) => {
            let _ = writeln!(io::stderr(), "paxai-runner: failed: {reason}");
            ExitCode::FAILURE
        }
    }
}

fn run() -> Result<Vec<u8>, Exit> {
    let mut args = std::env::args_os().skip(1);
    let (Some(verb), None) = (args.next(), args.next()) else {
        return Err(Exit::NotStarted("usage: <program> <verb>"));
    };
    let state = State::open()?;
    let mut request = Vec::new();
    io::stdin()
        .lock()
        .take(u64::try_from(MAX_DISPATCH_BYTES).map_or(u64::MAX, |max| max.saturating_add(1)))
        .read_to_end(&mut request)
        .map_err(|_| Exit::NotStarted("request unreadable"))?;
    match verb.to_str() {
        Some("describe") => describe(&request),
        Some("dispatch") => dispatch(&state, &request),
        Some("lookup") => lookup(&state, &request),
        Some("cancel") => cancel(&state, &request),
        _ => Err(Exit::NotStarted("unknown verb")),
    }
}

fn malformed() -> Exit {
    Exit::NotStarted("malformed request")
}

/// Reads `PAXRUN1 || version || job32 [|| fence:u64]` exactly.
fn job_request(request: &[u8], with_fence: bool) -> Result<(JobKey, u64), Exit> {
    let mut r = Reader::new(request);
    if r.take(MAGIC.len()).map_err(|_| malformed())? != MAGIC
        || r.u16().map_err(|_| malformed())? != VERSION
    {
        return Err(malformed());
    }
    let job = Digest32::new(r.fixed().map_err(|_| malformed())?).map_err(|_| malformed())?;
    let fence = if with_fence {
        r.u64().map_err(|_| malformed())?
    } else {
        0
    };
    r.finish().map_err(|_| malformed())?;
    Ok((JobKey::from_digest(job), fence))
}

fn encoded(report: &Report) -> Result<Vec<u8>, Exit> {
    report
        .encode()
        .map_err(|_| Exit::Failed("report does not encode"))
}

fn hex(bytes: &[u8]) -> String {
    bytes.iter().fold(String::new(), |mut out, byte| {
        let _ = write!(out, "{byte:02x}");
        out
    })
}

fn describe(request: &[u8]) -> Result<Vec<u8>, Exit> {
    if request.len() != HEADER_BYTES
        || &request[..MAGIC.len()] != MAGIC
        || request[MAGIC.len()..] != VERSION.to_be_bytes()
    {
        return Err(malformed());
    }
    Description {
        guarantees: ADMISSION_IDEMPOTENT | FENCED_ACKNOWLEDGMENT,
        loaded: loaded_pairs()?,
    }
    .encode()
    .map_err(|_| Exit::NotStarted("more pinned model directories than the protocol carries"))
}

fn models_root() -> Option<PathBuf> {
    std::env::var_os(MODELS_ENV).map(PathBuf::from)
}

fn pinned_directory(root: &Path, model: Digest32, deployment: Digest32) -> PathBuf {
    root.join(hex(model.as_bytes()))
        .join(hex(deployment.as_bytes()))
}

/// Sub-directories of `dir` named by a non-zero digest in lowercase hex.
fn digest_directories(dir: &Path) -> Result<Vec<Digest32>, Exit> {
    let entries = match fs::read_dir(dir) {
        Ok(entries) => entries,
        Err(error) if error.kind() == ErrorKind::NotFound => return Ok(Vec::new()),
        Err(_) => return Err(Exit::NotStarted("model directory unreadable")),
    };
    let mut out = Vec::new();
    for entry in entries {
        let entry = entry.map_err(|_| Exit::NotStarted("model directory unreadable"))?;
        let digest = entry
            .file_name()
            .to_str()
            .and_then(|name| parse_hex32(name).ok())
            .and_then(|bytes| Digest32::new(bytes).ok());
        if let Some(digest) = digest {
            if entry.path().is_dir() {
                out.push(digest);
            }
        }
    }
    out.sort_unstable();
    Ok(out)
}

fn loaded_pairs() -> Result<Vec<(Digest32, Digest32)>, Exit> {
    let Some(root) = models_root() else {
        return Ok(Vec::new());
    };
    let mut pairs = Vec::new();
    for model in digest_directories(&root)? {
        for deployment in digest_directories(&root.join(hex(model.as_bytes())))? {
            pairs.push((model, deployment));
            if pairs.len() > MAX_MODELS {
                return Err(Exit::NotStarted(
                    "more pinned model directories than the protocol carries",
                ));
            }
        }
    }
    Ok(pairs)
}

/// Journal: `kind:u8 || fence:u64 || cancel_fence:u64` then, when finished, the report.
#[derive(Default)]
struct Journal {
    kind: u8,
    fence: u64,
    cancel_fence: u64,
    report: Vec<u8>,
}
impl Journal {
    fn encode(&self) -> Vec<u8> {
        let mut out = Vec::with_capacity(17 + self.report.len());
        out.push(self.kind);
        out.extend_from_slice(&self.fence.to_be_bytes());
        out.extend_from_slice(&self.cancel_fence.to_be_bytes());
        out.extend_from_slice(&self.report);
        out
    }
    fn decode(job: JobKey, bytes: &[u8]) -> Result<Self, Exit> {
        let corrupt = || Exit::Failed("journal corrupt");
        let mut r = Reader::new(bytes);
        let kind = r.u8().map_err(|_| corrupt())?;
        let fence = r.u64().map_err(|_| corrupt())?;
        let cancel_fence = r.u64().map_err(|_| corrupt())?;
        let report = r.take(r.remaining()).map_err(|_| corrupt())?.to_vec();
        let consistent = match kind {
            NO_DISPATCH | JOURNAL_RUNNING => report.is_empty(),
            JOURNAL_FINISHED => Report::decode(&report)
                .is_ok_and(|recorded| recorded.job == job && recorded.fence == fence),
            _ => false,
        };
        if !consistent {
            return Err(corrupt());
        }
        Ok(Self {
            kind,
            fence,
            cancel_fence,
            report,
        })
    }
    /// The answer a lookup, cancel or repeated dispatch gets from this record.
    fn answer(&self, job: JobKey) -> Result<Vec<u8>, Exit> {
        match self.kind {
            JOURNAL_FINISHED => Ok(self.report.clone()),
            JOURNAL_RUNNING => encoded(&Report {
                job,
                fence: self.fence,
                progress: Progress::Running,
            }),
            _ => encoded(&Report {
                job,
                fence: self.cancel_fence,
                progress: Progress::NotReceived,
            }),
        }
    }
}

struct State {
    root: PathBuf,
}
impl State {
    /// An absent or unwritable state directory refuses every verb before anything starts.
    fn open() -> Result<Self, Exit> {
        let root = std::env::var_os(STATE_ENV)
            .filter(|value| !value.is_empty())
            .map(PathBuf::from)
            .ok_or(Exit::NotStarted("PAXAI_RUNNER_STATE is not set"))?;
        if !root.is_dir() {
            return Err(Exit::NotStarted("state directory absent"));
        }
        OpenOptions::new()
            .create(true)
            .append(true)
            .open(root.join("probe"))
            .and_then(|probe| probe.sync_all())
            .map_err(|_| Exit::NotStarted("state directory unwritable"))?;
        Ok(Self { root })
    }

    fn path(&self, job: JobKey, suffix: &str) -> PathBuf {
        let mut name = OsString::from(hex(job.digest().as_bytes()));
        name.push(suffix);
        self.root.join(name)
    }

    /// Exclusive per-identity lock, held until the returned file drops.
    fn lock(&self, job: JobKey) -> Result<File, Exit> {
        let file = OpenOptions::new()
            .create(true)
            .write(true)
            .truncate(false)
            .open(self.path(job, ".lock"))
            .map_err(|_| Exit::NotStarted("state directory unwritable"))?;
        file.lock()
            .map_err(|_| Exit::NotStarted("journal lock unavailable"))?;
        Ok(file)
    }

    fn read(&self, job: JobKey) -> Result<Option<Journal>, Exit> {
        match fs::read(self.path(job, "")) {
            Ok(bytes) => Journal::decode(job, &bytes).map(Some),
            Err(error) if error.kind() == ErrorKind::NotFound => Ok(None),
            Err(_) => Err(Exit::Failed("journal unreadable")),
        }
    }

    /// Write, sync, rename, directory sync.
    fn write(&self, job: JobKey, journal: &Journal) -> io::Result<()> {
        let path = self.path(job, "");
        let temp = self.path(job, ".tmp");
        let mut file = File::create(&temp)?;
        file.write_all(&journal.encode())?;
        file.sync_all()?;
        fs::rename(&temp, &path)?;
        File::open(&self.root)?.sync_all()
    }
}

fn lookup(state: &State, request: &[u8]) -> Result<Vec<u8>, Exit> {
    let (job, _) = job_request(request, false)?;
    let _lock = state.lock(job)?;
    match state.read(job)? {
        Some(journal) => journal.answer(job),
        None => encoded(&Report {
            job,
            fence: 0,
            progress: Progress::NotReceived,
        }),
    }
}

fn cancel(state: &State, request: &[u8]) -> Result<Vec<u8>, Exit> {
    let (job, fence) = job_request(request, true)?;
    let _lock = state.lock(job)?;
    let mut journal = state.read(job)?.unwrap_or_default();
    if journal.kind != JOURNAL_FINISHED && fence > journal.cancel_fence {
        journal.cancel_fence = fence;
        state
            .write(job, &journal)
            .map_err(|_| Exit::NotStarted("cancel fence not recorded"))?;
    }
    if journal.kind == NO_DISPATCH {
        return encoded(&Report {
            job,
            fence,
            progress: Progress::NotReceived,
        });
    }
    journal.answer(job)
}

/// Wall clock in nanoseconds and milliseconds since the Unix epoch.
fn wall_clock() -> Result<(u64, u64), Exit> {
    let since = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map_err(|_| Exit::Failed("clock before the epoch"))?;
    let ns = u64::try_from(since.as_nanos()).map_err(|_| Exit::Failed("clock overflow"))?;
    let ms = u64::try_from(since.as_millis()).map_err(|_| Exit::Failed("clock overflow"))?;
    Ok((ns, ms))
}

/// One measured attempt: wall clock at start plus the monotonic elapsed time.
struct Measurement {
    start_ns: u64,
    started_at_ms: u64,
    started: Instant,
}
impl Measurement {
    fn start() -> Result<Self, Exit> {
        let (start_ns, started_at_ms) = wall_clock()?;
        Ok(Self {
            start_ns,
            started_at_ms,
            started: Instant::now(),
        })
    }
    fn finish(&self, outcome: Result<Execution, ServiceError>) -> Result<Completion, Exit> {
        let elapsed = self.started.elapsed();
        let overflow = || Exit::Failed("measurement overflow");
        let finish_ns = u64::try_from(elapsed.as_nanos())
            .ok()
            .and_then(|ns| self.start_ns.checked_add(ns))
            .ok_or_else(overflow)?;
        let finished_at_ms = u64::try_from(elapsed.as_millis())
            .ok()
            .and_then(|ms| self.started_at_ms.checked_add(ms))
            .ok_or_else(overflow)?;
        let base = Completion {
            succeeded: false,
            error_code: 0,
            start_ns: self.start_ns,
            finish_ns,
            started_at_ms: self.started_at_ms,
            finished_at_ms,
            segments: Vec::new(),
            output: Vec::new(),
            output_manifest: Vec::new(),
        };
        Ok(match outcome {
            Ok(execution) => Completion {
                succeeded: true,
                segments: execution.segments,
                output: execution.output,
                output_manifest: execution.output_manifest,
                ..base
            },
            Err(error) => Completion {
                error_code: error.code(),
                ..base
            },
        })
    }
}

fn dispatch(state: &State, request: &[u8]) -> Result<Vec<u8>, Exit> {
    let dispatch = Dispatch::decode(request).map_err(|_| malformed())?;
    let job = dispatch.job;
    let lock = state.lock(job)?;
    let mut journal = state.read(job)?.unwrap_or_default();
    if journal.kind != NO_DISPATCH {
        return journal.answer(job);
    }
    if dispatch.fence <= journal.cancel_fence {
        return Err(Exit::NotStarted("attempt fenced by a recorded cancel"));
    }
    journal.fence = dispatch.fence;
    let directory = models_root()
        .map(|root| pinned_directory(&root, dispatch.model, dispatch.deployment))
        .filter(|directory| directory.is_dir());
    let Some(directory) = directory else {
        let measurement = Measurement::start()?;
        let completion = measurement.finish(Err(ServiceError::CapabilityMismatch))?;
        return finish(state, job, journal, completion, false);
    };
    journal.kind = JOURNAL_RUNNING;
    state
        .write(job, &journal)
        .map_err(|_| Exit::NotStarted("dispatch not journaled"))?;
    drop(lock);
    let measurement = Measurement::start()?;
    let outcome = execute(state, &dispatch, &directory, request);
    let completion = measurement.finish(outcome)?;
    let _lock = state
        .lock(job)
        .map_err(|_| Exit::Failed("journal lock lost"))?;
    let current = state.read(job)?.unwrap_or_default();
    journal.cancel_fence = current.cancel_fence;
    finish(state, job, journal, completion, true)
}

fn finish(
    state: &State,
    job: JobKey,
    mut journal: Journal,
    completion: Completion,
    executed: bool,
) -> Result<Vec<u8>, Exit> {
    let report = encoded(&Report {
        job,
        fence: journal.fence,
        progress: Progress::Finished(completion),
    })?;
    journal.kind = JOURNAL_FINISHED;
    journal.report.clone_from(&report);
    state.write(job, &journal).map_err(|_| {
        if executed {
            Exit::Failed("report not journaled")
        } else {
            Exit::NotStarted("refusal not journaled")
        }
    })?;
    Ok(report)
}

/// Runs the pinned engine under the dispatch deadline; a recorded cancel at or above the
/// attempt's fence stops it.
fn execute(
    state: &State,
    dispatch: &Dispatch<'_>,
    directory: &Path,
    request: &[u8],
) -> Result<Execution, ServiceError> {
    let mut child = Command::new(directory.join(ENGINE))
        .current_dir(directory)
        .stdin(Stdio::piped())
        .stdout(Stdio::piped())
        .stderr(Stdio::inherit())
        .spawn()
        .map_err(|_| ServiceError::ExecutionUnavailable)?;
    let stdin = child.stdin.take();
    let stdout = child.stdout.take();
    let input = request.to_vec();
    let writer = thread::spawn(move || stdin.map(|mut pipe| pipe.write_all(&input)));
    let reader = thread::spawn(move || {
        let mut out = Vec::new();
        stdout.map(|pipe| {
            pipe.take(
                u64::try_from(MAX_ENGINE_ANSWER_BYTES)
                    .map_or(u64::MAX, |max| max.saturating_add(1)),
            )
            .read_to_end(&mut out)
            .map(|_| out)
        })
    });
    let deadline = Duration::from_millis(u64::from(dispatch.deadline_ms));
    let started = Instant::now();
    let live = || {
        started.elapsed() < deadline
            && !state
                .read(dispatch.job)
                .is_ok_and(|journal| journal.is_some_and(|j| j.cancel_fence >= dispatch.fence))
    };
    let status = loop {
        let live = live();
        match child.try_wait() {
            Ok(Some(status)) => break status,
            Ok(None) if live => thread::sleep(POLL),
            _ => {
                let _ = child.kill();
                let _ = child.wait();
                return Err(ServiceError::ExecutionUnavailable);
            }
        }
    };
    while !(writer.is_finished() && reader.is_finished()) {
        if !live() {
            return Err(ServiceError::ExecutionUnavailable);
        }
        thread::sleep(POLL);
    }
    let written = writer.join();
    let answer = reader.join();
    if !status.success() {
        return Err(ServiceError::ExecutionUnavailable);
    }
    match (written, answer) {
        (Ok(Some(Ok(()))), Ok(Some(Ok(bytes)))) => {
            if bytes.len() > MAX_ENGINE_ANSWER_BYTES {
                return Err(ServiceError::OutputTooLarge);
            }
            Execution::decode(&bytes)
        }
        _ => Err(ServiceError::ExecutionUnavailable),
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::os::unix::fs::PermissionsExt;
    use std::sync::OnceLock;

    const DESCENDANT_SECONDS: u64 = 30;
    const BOUND: Duration = Duration::from_secs(5);
    const MODEL: u8 = 0x11;
    const DEADLINE_DEPLOYMENT: u8 = 0x22;
    const CANCEL_DEPLOYMENT: u8 = 0x23;
    const EXITED: &str = "exited";

    fn digest(byte: u8) -> Result<Digest32, String> {
        Digest32::new([byte; 32]).map_err(|_| "zero digest".to_string())
    }

    fn reason(exit: &Exit) -> String {
        match exit {
            Exit::NotStarted(reason) | Exit::Failed(reason) => (*reason).to_string(),
        }
    }

    fn scratch(name: &str) -> Result<PathBuf, String> {
        let (ns, _) = wall_clock().map_err(|e| reason(&e))?;
        let dir =
            std::env::temp_dir().join(format!("paxai-runner-{}-{name}-{ns}", std::process::id()));
        fs::create_dir_all(&dir).map_err(|e| e.to_string())?;
        Ok(dir)
    }

    /// The pinned engine directories of every test, written before any test spawns a process
    /// so no forked child holds an engine's write descriptor at exec time. Each engine starts
    /// a background sleep inheriting stdout, marks its exit and exits at once.
    fn engine_root() -> Result<&'static PathBuf, String> {
        static ROOT: OnceLock<Result<PathBuf, String>> = OnceLock::new();
        ROOT.get_or_init(|| {
            let root = scratch("models")?;
            for deployment in [DEADLINE_DEPLOYMENT, CANCEL_DEPLOYMENT] {
                let directory = pinned_directory(&root, digest(MODEL)?, digest(deployment)?);
                fs::create_dir_all(&directory).map_err(|e| e.to_string())?;
                let engine = directory.join(ENGINE);
                fs::write(
                    &engine,
                    format!("#!/bin/sh\nsleep {DESCENDANT_SECONDS} &\n: > {EXITED}\nexit 0\n"),
                )
                .map_err(|e| e.to_string())?;
                fs::set_permissions(&engine, fs::Permissions::from_mode(0o755))
                    .map_err(|e| e.to_string())?;
            }
            std::env::set_var(MODELS_ENV, &root);
            Ok(root)
        })
        .as_ref()
        .map_err(Clone::clone)
    }

    fn engine_directory(deployment: u8) -> Result<PathBuf, String> {
        Ok(pinned_directory(
            engine_root()?,
            digest(MODEL)?,
            digest(deployment)?,
        ))
    }

    fn request(job: u8, deployment: u8, fence: u64, deadline_ms: u32) -> Result<Vec<u8>, String> {
        Dispatch {
            job: JobKey::from_digest(digest(job)?),
            fence,
            model: digest(MODEL)?,
            deployment: digest(deployment)?,
            capability: digest(0x33)?,
            unit_kind: layerx_paxai_worker::runner::TOKENS,
            max_output_bytes: 1_024,
            max_input_units: 1_024,
            max_units: 1_024,
            deadline_ms,
            result_key: None,
            input_manifest: &[],
            payload: b"prompt",
        }
        .encode()
        .map_err(|e| format!("{e:?}"))
    }

    fn job_request_bytes(job: u8, fence: Option<u64>) -> Vec<u8> {
        let mut out = MAGIC.to_vec();
        out.extend_from_slice(&VERSION.to_be_bytes());
        out.extend_from_slice(&[job; 32]);
        if let Some(fence) = fence {
            out.extend_from_slice(&fence.to_be_bytes());
        }
        out
    }

    /// Records `fence` through `cancel` once the cancel engine has exited and left its
    /// descendant holding stdout; consumes the exit mark for the next run.
    fn cancel_after_exit(state: &State, job: u8, fence: u64) -> Result<(), String> {
        let mark = engine_directory(CANCEL_DEPLOYMENT)?.join(EXITED);
        let waiting = Instant::now();
        while fs::remove_file(&mark).is_err() {
            if waiting.elapsed() > BOUND {
                return Err("engine never exited".to_string());
            }
            thread::sleep(POLL);
        }
        thread::sleep(Duration::from_millis(100));
        cancel(state, &job_request_bytes(job, Some(fence)))
            .map(drop)
            .map_err(|e| reason(&e))
    }

    /// A following dispatch of `job`, cancelled after the engine exits when `cancelled`,
    /// answers a finished execution-unavailable report well before the descendant ends, and a
    /// lookup answers the same report.
    fn following_dispatch_finishes(
        state: &State,
        job: u8,
        deployment: u8,
        fence: u64,
        deadline_ms: u32,
        cancelled: bool,
    ) -> Result<(), String> {
        let bytes = request(job, deployment, fence, deadline_ms)?;
        let started = Instant::now();
        let dispatched = thread::scope(|scope| {
            let dispatched = scope.spawn(|| dispatch(state, &bytes).map_err(|e| reason(&e)));
            if cancelled {
                cancel_after_exit(state, job, fence)?;
            }
            dispatched
                .join()
                .map_err(|_| "dispatch panicked".to_string())?
        })?;
        assert!(started.elapsed() < BOUND, "dispatch outlived the bound");
        let report = Report::decode(&dispatched).map_err(|e| format!("{e:?}"))?;
        assert_eq!(report.job, JobKey::from_digest(digest(job)?));
        assert_eq!(report.fence, fence);
        let Progress::Finished(completion) = report.progress else {
            return Err(format!("dispatch answered {:?}", report.progress));
        };
        assert!(!completion.succeeded);
        assert_eq!(
            completion.error_code,
            ServiceError::ExecutionUnavailable.code()
        );
        let looked_up = lookup(state, &job_request_bytes(job, None)).map_err(|e| reason(&e))?;
        assert_eq!(looked_up, dispatched);
        Ok(())
    }

    #[test]
    fn execute_ends_at_deadline_while_a_descendant_holds_stdout() -> Result<(), String> {
        let directory = engine_directory(DEADLINE_DEPLOYMENT)?;
        let state = State {
            root: scratch("deadline")?,
        };
        let bytes = request(0xa1, DEADLINE_DEPLOYMENT, 1, 200)?;
        let attempt = Dispatch::decode(&bytes).map_err(|e| format!("{e:?}"))?;
        let started = Instant::now();
        let outcome = execute(&state, &attempt, &directory, &bytes);
        assert!(started.elapsed() < BOUND, "execute outlived the bound");
        assert_eq!(outcome, Err(ServiceError::ExecutionUnavailable));
        following_dispatch_finishes(&state, 0xa1, DEADLINE_DEPLOYMENT, 2, 200, false)
    }

    #[test]
    fn execute_ends_on_cancel_while_a_descendant_holds_stdout() -> Result<(), String> {
        let directory = engine_directory(CANCEL_DEPLOYMENT)?;
        let state = State {
            root: scratch("cancel")?,
        };
        let deadline_ms = 60_000;
        let bytes = request(0xb2, CANCEL_DEPLOYMENT, 1, deadline_ms)?;
        let attempt = Dispatch::decode(&bytes).map_err(|e| format!("{e:?}"))?;
        let started = Instant::now();
        let outcome = thread::scope(|scope| {
            let executed = scope.spawn(|| execute(&state, &attempt, &directory, &bytes));
            cancel_after_exit(&state, 0xb2, 1)?;
            executed.join().map_err(|_| "execute panicked".to_string())
        })?;
        assert!(started.elapsed() < BOUND, "execute outlived the bound");
        assert_eq!(outcome, Err(ServiceError::ExecutionUnavailable));
        following_dispatch_finishes(&state, 0xb2, CANCEL_DEPLOYMENT, 2, deadline_ms, true)
    }
}
