use ed25519_dalek::SigningKey;
use layerx_programs_ai_market::{
    codec::{self, derive_market, encode_envelope, Envelope, ResultStatus},
    dispatch::{self, Buffers, Operation, Routed, SCRATCH_BYTES},
    errors::*,
    policy::*,
    registry::*,
    registry_ops::{CallContext, PolicySection, REGISTERED},
    state::{decode_shared_state, Section},
    types::*,
    MAX_EVENT_BYTES, MAX_RESULT_BYTES, MAX_STATE_BYTES,
};
use layerx_programs_runtime::terminal::{
    decode_terminal_payload, CandidateTerminalOutcome, ExecutionTerminal, TerminalDetail,
};
use layerx_programs_runtime::RefusalClass;
use std::io::{BufRead, BufReader, Write};
use std::path::PathBuf;
use std::process::{Child, ChildStdin, ChildStdout, Command, Stdio};

const OWNER: usize = 0;
const TREASURY: usize = 1;
const OUTSIDER: usize = 2;
const ASSET: [u8; 32] = {
    let mut asset = [0; 32];
    asset[0] = 9;
    asset
};
const REFUND: [u8; 32] = [14; 32];
const EXPIRY: u64 = 1_000_000;
const EVENTS_DOMAIN: &[u8] = b"LayerX/programs/events/v1\0";
const PROGRAM_REFUSED: i64 = -736;

enum Failure {
    Application(ApplicationError),
    Conversion(core::num::TryFromIntError),
    Io(std::io::Error),
    Harness(String),
}
impl core::fmt::Debug for Failure {
    fn fmt(&self, f: &mut core::fmt::Formatter<'_>) -> core::fmt::Result {
        match self {
            Self::Application(error) => write!(f, "application refusal {error:?}"),
            Self::Conversion(error) => write!(f, "integer conversion {error}"),
            Self::Io(error) => write!(f, "native harness io {error}"),
            Self::Harness(what) => write!(f, "native harness {what}"),
        }
    }
}
impl From<ApplicationError> for Failure {
    fn from(error: ApplicationError) -> Self {
        Self::Application(error)
    }
}
impl From<core::num::TryFromIntError> for Failure {
    fn from(error: core::num::TryFromIntError) -> Self {
        Self::Conversion(error)
    }
}
impl From<std::io::Error> for Failure {
    fn from(error: std::io::Error) -> Self {
        Self::Io(error)
    }
}
type Checked<T = ()> = Result<T, Failure>;

fn harness(what: impl Into<String>) -> Failure {
    Failure::Harness(what.into())
}

fn hex(bytes: &[u8]) -> String {
    const DIGITS: &[u8; 16] = b"0123456789abcdef";
    bytes
        .iter()
        .flat_map(|b| [DIGITS[usize::from(b >> 4)], DIGITS[usize::from(b & 15)]])
        .map(char::from)
        .collect()
}

fn unhex(text: &str) -> Checked<Vec<u8>> {
    if text == "-" {
        return Ok(Vec::new());
    }
    if !text.len().is_multiple_of(2) {
        return Err(harness(format!("odd hex length {}", text.len())));
    }
    (0..text.len())
        .step_by(2)
        .map(|i| u8::from_str_radix(&text[i..i + 2], 16).map_err(|e| harness(format!("hex {e}"))))
        .collect()
}

fn fixed(bytes: &[u8]) -> Checked<[u8; 32]> {
    bytes
        .try_into()
        .map_err(|_| harness(format!("expected 32 bytes, got {}", bytes.len())))
}

fn number<T: core::str::FromStr>(token: &str) -> Checked<T> {
    token
        .parse()
        .map_err(|_| harness(format!("bad number {token}")))
}

struct Manifest {
    wasm: String,
    harness: String,
    chain: [u8; 32],
}

fn manifest() -> Checked<Manifest> {
    let path = std::env::var_os("PAXAI_HOST_BOUNDARY_MANIFEST").map_or_else(
        || {
            PathBuf::from(env!("CARGO_MANIFEST_DIR"))
                .join("../../../build/paxai-host-boundary/manifest")
        },
        PathBuf::from,
    );
    let text = std::fs::read_to_string(&path).map_err(|e| {
        harness(format!(
            "{}: {e}; run make paxai-host-boundary-build",
            path.display()
        ))
    })?;
    let lines: Vec<&str> = text.lines().collect();
    let [wasm, binary, chain] = lines[..] else {
        return Err(harness(format!("manifest has {} lines", lines.len())));
    };
    Ok(Manifest {
        wasm: wasm.to_owned(),
        harness: binary.to_owned(),
        chain: fixed(&unhex(chain)?)?,
    })
}

struct NativeEvent {
    producer: Vec<u8>,
    principal: Vec<u8>,
    frame: Vec<u8>,
    topic: Vec<u8>,
    data: Vec<u8>,
}

struct Cursor<'a> {
    bytes: &'a [u8],
    at: usize,
}
impl<'a> Cursor<'a> {
    fn take(&mut self, n: usize) -> Checked<&'a [u8]> {
        let end = self
            .at
            .checked_add(n)
            .ok_or_else(|| harness("event overflow"))?;
        let out = self
            .bytes
            .get(self.at..end)
            .ok_or_else(|| harness("short event envelope"))?;
        self.at = end;
        Ok(out)
    }
    fn be32(&mut self) -> Checked<usize> {
        let bytes: [u8; 4] = self
            .take(4)?
            .try_into()
            .map_err(|_| harness("event length"))?;
        Ok(usize::try_from(u32::from_be_bytes(bytes))?)
    }
}

fn native_events(raw: &[u8]) -> Checked<Vec<NativeEvent>> {
    if raw.is_empty() {
        return Ok(Vec::new());
    }
    let mut r = Cursor { bytes: raw, at: 0 };
    if r.take(EVENTS_DOMAIN.len())? != EVENTS_DOMAIN {
        return Err(harness("event envelope domain"));
    }
    let count = r.be32()?;
    let mut events = Vec::new();
    for _ in 0..count {
        let producer = r.take(32)?.to_vec();
        let principal = r.take(32)?.to_vec();
        let frame = r.take(9)?.to_vec();
        let topic_len = r.be32()?;
        let topic = r.take(topic_len)?.to_vec();
        let data_len = r.be32()?;
        let data = r.take(data_len)?.to_vec();
        events.push(NativeEvent {
            producer,
            principal,
            frame,
            topic,
            data,
        });
    }
    if r.at != raw.len() {
        return Err(harness("trailing event envelope bytes"));
    }
    Ok(events)
}

struct Line {
    status: i64,
    result_code: i64,
    kind: u8,
    abi: u16,
    terminal: Vec<u8>,
    state: Vec<u8>,
    blobs_before: usize,
    blobs_after: usize,
    application_blobs: usize,
    kv_after: usize,
    witness: usize,
    balance_before: u64,
    balance_after: u64,
    fee: u64,
    rewards_balance: u64,
    events: Vec<NativeEvent>,
}

fn parse_line(text: &str) -> Checked<Line> {
    let t: Vec<&str> = text.split_whitespace().collect();
    if t.len() != 18 || t[0] != "call" {
        return Err(harness(format!("unexpected line {text}")));
    }
    Ok(Line {
        status: number(t[1])?,
        result_code: number(t[2])?,
        kind: number(t[3])?,
        abi: number(t[4])?,
        terminal: unhex(t[5])?,
        state: unhex(t[6])?,
        blobs_before: number(t[7])?,
        blobs_after: number(t[8])?,
        application_blobs: number(t[9])?,
        kv_after: number(t[11])?,
        witness: number(t[12])?,
        balance_before: number(t[13])?,
        balance_after: number(t[14])?,
        fee: number(t[15])?,
        rewards_balance: number(t[16])?,
        events: native_events(&unhex(t[17])?)?,
    })
}

enum Terminal {
    Success(Vec<u8>),
    Rejected(Vec<u8>),
}

fn terminal(line: &Line, program: ProgramId) -> Checked<Terminal> {
    let decoded = decode_terminal_payload(line.kind, line.abi, &line.terminal)
        .map_err(|e| harness(format!("terminal {e:?}")))?;
    let TerminalDetail::Execution(ExecutionTerminal::CandidateV4 {
        program: producer,
        outcome,
        ..
    }) = decoded.detail
    else {
        return Err(harness("terminal is not a CandidateV4 execution"));
    };
    assert_eq!(producer, program.bytes());
    match outcome {
        CandidateTerminalOutcome::Success { code: 0, response } => Ok(Terminal::Success(response)),
        CandidateTerminalOutcome::Failure(failure) if failure.class() == RefusalClass::Rejected => {
            Ok(Terminal::Rejected(failure.reason().bytes().to_vec()))
        }
        _ => Err(harness(
            "terminal is neither success code 0 nor a Rejected refusal",
        )),
    }
}

struct Native {
    child: Child,
    input: Option<ChildStdin>,
    output: BufReader<ChildStdout>,
    chain: ChainDomain,
    program: ProgramId,
    principals: [PrincipalId; 3],
    rewards: [u8; 32],
    blob_cap: usize,
    staged_cap: usize,
    kv_cap: usize,
    state: Option<Vec<u8>>,
    rewards_balance: Option<u64>,
    next_request: u8,
}

impl Drop for Native {
    fn drop(&mut self) {
        if self.input.is_some() {
            drop(self.child.kill());
            drop(self.child.wait());
        }
    }
}

impl Native {
    fn start() -> Checked<Self> {
        let m = manifest()?;
        let mut child = Command::new(&m.harness)
            .arg(&m.wasm)
            .stdin(Stdio::piped())
            .stdout(Stdio::piped())
            .spawn()?;
        let input = child.stdin.take().ok_or_else(|| harness("stdin"))?;
        let stdout = child.stdout.take().ok_or_else(|| harness("stdout"))?;
        let mut output = BufReader::new(stdout);
        let mut ready = String::new();
        output.read_line(&mut ready)?;
        let t: Vec<&str> = ready.split_whitespace().collect();
        if t.len() != 9 || t[0] != "ready" {
            return Err(harness(format!("bring-up line {ready}")));
        }
        let principal =
            |i: usize| -> Checked<PrincipalId> { Ok(PrincipalId::new(fixed(&unhex(t[i])?)?)?) };
        Ok(Self {
            child,
            input: Some(input),
            output,
            chain: ChainDomain::new(m.chain)?,
            program: ProgramId::new(fixed(&unhex(t[1])?)?)?,
            principals: [principal(2)?, principal(3)?, principal(4)?],
            rewards: fixed(&unhex(t[5])?)?,
            blob_cap: number(t[6])?,
            staged_cap: number(t[7])?,
            kv_cap: number(t[8])?,
            state: None,
            rewards_balance: None,
            next_request: 0,
        })
    }

    fn finish(mut self) -> Checked {
        drop(self.input.take());
        let status = self.child.wait()?;
        assert!(status.success(), "native harness exit {status}");
        Ok(())
    }

    fn market(&self) -> CodecResult<MarketId> {
        derive_market(self.chain, self.program)
    }

    fn rewards(&self) -> CodecResult<AccountId> {
        derive_rewards_account(self.program, AssetId::new(ASSET)?)
    }

    fn base<'a>(
        &mut self,
        operation: Operation,
        actor: usize,
        sequence: u64,
        payload: &'a [u8],
        config: u64,
    ) -> Checked<Envelope<'a>> {
        self.next_request = self.next_request.wrapping_add(1).max(1);
        Ok(Envelope {
            operation,
            chain: self.chain,
            program: self.program,
            market: self.market()?,
            actor: self.principals[actor],
            epoch: 0,
            config,
            roster: Presence::Absent,
            sequence,
            expiry: EXPIRY,
            request: RequestId::new([self.next_request; 32])?,
            payload,
            authentication: Authentication::Native,
        })
    }

    fn envelope(
        &mut self,
        operation: Operation,
        actor: usize,
        sequence: u64,
        payload: &[u8],
        config: u64,
    ) -> Checked<Vec<u8>> {
        encode(&self.base(operation, actor, sequence, payload, config)?)
    }

    fn expected(&self, actor: usize, height: u64, envelope: &[u8]) -> Checked<Expected> {
        let ctx = CallContext {
            chain: self.chain,
            program: self.program,
            principal: self.principals[actor],
            height,
        };
        let mut next = vec![0; MAX_STATE_BYTES];
        let mut scratch = vec![0; SCRATCH_BYTES];
        let mut event = vec![0; MAX_EVENT_BYTES];
        let mut result = vec![0; MAX_RESULT_BYTES];
        let routed = dispatch::route(
            &ctx,
            envelope,
            self.state.as_deref(),
            Buffers {
                next: &mut next,
                scratch: &mut scratch,
                event: &mut event,
                result: &mut result,
            },
        )?;
        Ok(match routed {
            Routed::Applied {
                operation,
                state_len,
                event_len,
                result_len,
                ..
            } => Expected {
                applied: Some((
                    operation,
                    next[..state_len].to_vec(),
                    event[..event_len].to_vec(),
                )),
                refused: false,
                result: result[..result_len].to_vec(),
            },
            Routed::Unchanged { result_len } => Expected {
                applied: None,
                refused: false,
                result: result[..result_len].to_vec(),
            },
            Routed::Refused { result_len } => Expected {
                applied: None,
                refused: true,
                result: result[..result_len].to_vec(),
            },
        })
    }

    fn submit(&mut self, actor: usize, height: u64, envelope: &[u8]) -> Checked<Vec<u8>> {
        let expected = self.expected(actor, height, envelope)?;
        let input = self.input.as_mut().ok_or_else(|| harness("closed"))?;
        writeln!(input, "{actor} {height} {}", hex(envelope))?;
        input.flush()?;
        let mut text = String::new();
        if self.output.read_line(&mut text)? == 0 {
            return Err(harness("native harness stopped"));
        }
        let line = parse_line(&text)?;
        self.check_common(&line);
        let terminal = terminal(&line, self.program)?;
        match (&expected.applied, expected.refused, terminal) {
            (Some((operation, state, event)), false, Terminal::Success(response)) => {
                assert_eq!(line.result_code, 0);
                assert_eq!(response, expected.result);
                assert_eq!(&line.state, state);
                assert!(line.state.len() <= MAX_STATE_BYTES);
                assert_eq!(line.application_blobs, 2);
                assert!(line.witness > 0);
                self.check_event(&line, actor, *operation, event)?;
                self.state = Some(line.state);
            }
            (None, false, Terminal::Success(response)) => {
                assert_eq!(line.result_code, 0);
                assert_eq!(response, expected.result);
                self.check_unchanged(&line);
            }
            (None, true, Terminal::Rejected(reason)) => {
                assert_eq!(line.result_code, PROGRAM_REFUSED);
                assert_eq!(reason, expected.result);
                self.check_unchanged(&line);
            }
            _ => panic!("native terminal disagrees with the in-process route"),
        }
        Ok(expected.result)
    }

    fn check_common(&mut self, line: &Line) {
        assert_eq!(line.status, 0);
        assert!(line.blobs_after >= line.blobs_before);
        assert!(line.blobs_after - line.blobs_before <= self.staged_cap);
        assert!(line.blobs_after <= self.blob_cap);
        assert!(line.kv_after <= self.kv_cap);
        assert_eq!(line.balance_after, line.balance_before - line.fee);
        let rewards = *self.rewards_balance.get_or_insert(line.rewards_balance);
        assert_eq!(line.rewards_balance, rewards);
    }

    fn check_unchanged(&self, line: &Line) {
        assert_eq!(line.state, self.state.clone().unwrap_or_default());
        assert_eq!(line.application_blobs, 0);
        assert_eq!(line.blobs_after, line.blobs_before);
        assert!(line.events.is_empty());
    }

    fn check_event(&self, line: &Line, actor: usize, operation: Operation, body: &[u8]) -> Checked {
        let mut topic = [0; 64];
        let topic_len = codec::event_topic(operation, &mut topic)?;
        let [event] = &line.events[..] else {
            return Err(harness(format!("{} native events", line.events.len())));
        };
        assert_eq!(event.producer, self.program.bytes());
        assert_eq!(event.principal, self.principals[actor].bytes());
        assert_eq!(event.frame, [0; 9]);
        assert_eq!(event.topic, &topic[..topic_len]);
        assert_eq!(event.data, body);
        Ok(())
    }

    fn send(
        &mut self,
        operation: Operation,
        actor: usize,
        sequence: u64,
        payload: &[u8],
        config: u64,
        height: u64,
    ) -> Checked<Vec<u8>> {
        let envelope = self.envelope(operation, actor, sequence, payload, config)?;
        self.submit(actor, height, &envelope)
    }

    fn section(&self) -> Checked<PolicySection<'_>> {
        let state = self
            .state
            .as_deref()
            .ok_or_else(|| harness("no committed state"))?;
        Ok(PolicySection::decode(
            decode_shared_state(state)?.feature_sections[Section::PolicyLifecycle.index()],
        )?)
    }

    fn revision(&self) -> Checked<u64> {
        let state = self
            .state
            .as_deref()
            .ok_or_else(|| harness("no committed state"))?;
        Ok(decode_shared_state(state)?.revision)
    }

    fn create_payload(&self, owner: usize, treasury: &[u8], policy: &[u8]) -> Checked<Vec<u8>> {
        let mut v = self.principals[owner].bytes().to_vec();
        v.extend_from_slice(&ASSET);
        v.extend_from_slice(self.rewards()?.as_bytes());
        v.extend_from_slice(&REFUND);
        v.extend_from_slice(treasury);
        v.extend_from_slice(policy);
        v.extend_from_slice(&[16; 32]);
        Ok(v)
    }

    fn create(&mut self, treasury: &[u8], policy: &[u8]) -> Checked<Vec<u8>> {
        let payload = self.create_payload(OWNER, treasury, policy)?;
        self.send(dispatch::CREATE, OWNER, 1, &payload, 1, 1000)
    }
}

struct Expected {
    applied: Option<(Operation, Vec<u8>, Vec<u8>)>,
    refused: bool,
    result: Vec<u8>,
}

fn encode(envelope: &Envelope<'_>) -> Checked<Vec<u8>> {
    let mut out = vec![0; 16_384];
    let n = encode_envelope(envelope, &mut out)?;
    out.truncate(n);
    Ok(out)
}

fn status(frame: &[u8]) -> Checked<(ResultStatus, Option<ApplicationError>, u64)> {
    let r = codec::decode_result(frame)?;
    Ok((r.status, r.error, r.revision))
}

fn refusal(frame: &[u8]) -> Checked<ApplicationError> {
    match status(frame)? {
        (ResultStatus::Error, Some(error), _) => Ok(error),
        other => Err(harness(format!("expected a refusal, got {other:?}"))),
    }
}

fn applied_at(frame: &[u8]) -> Checked<u64> {
    match status(frame)? {
        (ResultStatus::Ok, None, revision) => Ok(revision),
        other => Err(harness(format!("expected Ok, got {other:?}"))),
    }
}

fn commitments(rubric: u8) -> CodecResult<PolicyCommitments> {
    Ok(PolicyCommitments {
        model_artifact: Digest32::new([1; 32])?,
        dataset_artifact: [2; 32],
        benchmark_suite: Digest32::new([3; 32])?,
        rubric: RubricDigest::new([rubric; 32])?,
        task_schema: Digest32::new([5; 32])?,
        result_schema: Digest32::new([6; 32])?,
        service_terms: Digest32::new([7; 32])?,
    })
}

fn policy(version: u64) -> CodecResult<TaskPolicyV1> {
    TaskPolicyV1::bounded_default(version, 1, commitments(4)?, 100, 1)
}

fn policy_bytes(value: &TaskPolicyV1) -> CodecResult<Vec<u8>> {
    let mut out = vec![0; TASK_POLICY_BYTES];
    value.encode(&mut out)?;
    Ok(out)
}

fn u64s(values: &[u64]) -> Vec<u8> {
    values.iter().flat_map(|v| v.to_be_bytes()).collect()
}

fn stage(revision: u64, value: &TaskPolicyV1, effective: u64) -> CodecResult<Vec<u8>> {
    let mut v = revision.to_be_bytes().to_vec();
    v.extend_from_slice(&policy_bytes(value)?);
    v.extend_from_slice(&effective.to_be_bytes());
    Ok(v)
}

#[test]
fn a01_native_create_binds_principal_origin_and_refuses_duplicates() -> Checked {
    let mut n = Native::start()?;
    let rewards = n.rewards()?;
    assert_eq!(rewards.bytes(), n.rewards);
    let encoded = policy_bytes(&policy(1)?)?;
    let owner_payload = n.create_payload(OWNER, &[0], &encoded)?;
    let by_outsider = n.envelope(dispatch::CREATE, OUTSIDER, 1, &owner_payload, 1)?;
    assert_eq!(
        refusal(&n.submit(OUTSIDER, 1000, &by_outsider)?)?,
        F01_PRINCIPAL_MISMATCH
    );
    let e = n.envelope(dispatch::CREATE, OWNER, 1, &owner_payload, 1)?;
    assert_eq!(refusal(&n.submit(OUTSIDER, 1000, &e)?)?, UNAUTHORIZED);
    assert_eq!(n.state, None);
    assert_eq!(applied_at(&n.submit(OWNER, 1000, &e)?)?, 1);
    let s = n.section()?;
    assert_eq!(s.header.lifecycle, REGISTERED);
    assert_eq!(s.header.origin_height, 1000);
    assert_eq!(s.header.state_revision, 1);
    assert_eq!(n.revision()?, 1);
    assert_eq!(s.header.market_id, n.market()?);
    assert_eq!(s.header.deployment_chain_domain, n.chain);
    assert_eq!(s.header.program_id, n.program);
    assert_eq!(s.header.owner_principal, n.principals[OWNER]);
    assert_eq!(s.header.rewards_account, rewards);
    assert_eq!(s.current, policy(1)?);
    assert_eq!(
        status(&n.submit(OWNER, 1000, &e)?)?,
        (ResultStatus::AlreadyApplied, None, 1)
    );
    let fresh = n.send(dispatch::CREATE, OWNER, 2, &owner_payload, 1, 1001)?;
    assert_eq!(refusal(&fresh)?, F01_ALREADY_CREATED);
    assert_eq!(n.section()?.header.origin_height, 1000);
    n.finish()
}

#[test]
fn a02_native_activation_schedule_rebinds_worker_revision_and_needs_readiness() -> Checked {
    let mut n = Native::start()?;
    applied_at(&n.create(&[0], &policy_bytes(&policy(1)?)?)?)?;
    let too_early = n.send(
        dispatch::SCHEDULE_ACTIVATION,
        OWNER,
        2,
        &u64s(&[1, 0]),
        1,
        1001,
    )?;
    assert_eq!(refusal(&too_early)?, F01_ACTIVATION_TOO_EARLY);
    let delegate = SigningKey::from_bytes(&[0x41; 32])
        .verifying_key()
        .to_bytes();
    let mut enroll = n.principals[OUTSIDER].bytes().to_vec();
    enroll.extend_from_slice(&[21; 32]);
    enroll.extend_from_slice(&delegate);
    enroll.extend_from_slice(&[31; 32]);
    enroll.extend_from_slice(&1101u64.to_be_bytes());
    assert_eq!(
        applied_at(&n.send(dispatch::EnrollWorker, OWNER, 2, &enroll, 1, 1001)?)?,
        2
    );
    assert_eq!(n.section()?.header.state_revision, n.revision()?);
    assert_eq!(n.revision()?, 2);
    let schedule = n.send(
        dispatch::SCHEDULE_ACTIVATION,
        OWNER,
        3,
        &u64s(&[2, 1]),
        1,
        1001,
    )?;
    assert_eq!(applied_at(&schedule)?, 3);
    let header = n.section()?.header;
    assert_eq!(
        (
            header.lifecycle,
            header.activation_scheduled,
            header.activation_epoch
        ),
        (REGISTERED, true, 1)
    );
    let advance = u64s(&[3, 1]);
    let early = n.send(dispatch::ADVANCE_ACTIVATION, OUTSIDER, 0, &advance, 1, 1127)?;
    assert_eq!(refusal(&early)?, WRONG_PHASE);
    let unready = n.send(dispatch::ADVANCE_ACTIVATION, OUTSIDER, 0, &advance, 1, 1128)?;
    assert_eq!(refusal(&unready)?, F01_ACTIVATION_NOT_READY);
    assert_eq!(n.revision()?, 3);
    n.finish()
}

#[test]
fn a03_native_stage_open_refusal_cancel_and_restage() -> Checked {
    let mut n = Native::start()?;
    applied_at(&n.create(&[0], &policy_bytes(&policy(1)?)?)?)?;
    let early = n.send(
        dispatch::STAGE_POLICY,
        OWNER,
        2,
        &stage(1, &policy(2)?, 1)?,
        1,
        1130,
    )?;
    assert_eq!(refusal(&early)?, F01_ACTIVATION_TOO_EARLY);
    let staged = n.send(
        dispatch::STAGE_POLICY,
        OWNER,
        2,
        &stage(1, &policy(2)?, 2)?,
        1,
        1130,
    )?;
    assert_eq!(applied_at(&staged)?, 2);
    let before = n.state.clone();
    let mut open = n.base(dispatch::OPEN_EPOCH, OUTSIDER, 0, &[], 2)?;
    open.epoch = 2;
    open.roster = Presence::Present(RosterDigest::new([9; 32])?);
    let opened = n.submit(OUTSIDER, 1256, &encode(&open)?)?;
    assert_eq!(refusal(&opened)?, READINESS_BLOCKED);
    assert_eq!(n.state, before);
    let s = n.section()?;
    assert_eq!(
        (s.current.config_version, s.header.active_config_version),
        (1, 1)
    );
    let Presence::Present(pending) = s.pending else {
        return Err(harness("pending policy 2 was dropped"));
    };
    assert_eq!((pending.policy, pending.effective_epoch), (policy(2)?, 2));
    let mismatch = n.send(dispatch::CANCEL_POLICY, OWNER, 3, &u64s(&[2, 3]), 1, 1256)?;
    assert_eq!(refusal(&mismatch)?, F01_VERSION_MISMATCH);
    assert_eq!(
        applied_at(&n.send(dispatch::CANCEL_POLICY, OWNER, 3, &u64s(&[2, 2]), 1, 1256)?)?,
        3
    );
    assert_eq!(n.section()?.pending, Presence::Absent);
    let reused = n.send(
        dispatch::STAGE_POLICY,
        OWNER,
        4,
        &stage(3, &policy(2)?, 3)?,
        1,
        1256,
    )?;
    assert_eq!(refusal(&reused)?, F01_VERSION_MISMATCH);
    let next = n.send(
        dispatch::STAGE_POLICY,
        OWNER,
        4,
        &stage(3, &policy(3)?, 3)?,
        1,
        1256,
    )?;
    assert_eq!(applied_at(&next)?, 4);
    assert_eq!(n.section()?.header.highest_config_version, 3);
    n.finish()
}

#[test]
fn a09_native_policy_variants_and_maxima() -> Checked {
    let mut n = Native::start()?;
    let valid = policy_bytes(&policy(1)?)?;
    let mutations: [&[(usize, &[u8])]; 11] = [
        &[(234, &[33])],
        &[(235, &[9])],
        &[(236, &[0, 65])],
        &[(252, &[0, 15, 66, 63])],
        &[(9, &[0])],
        &[(9, &[3])],
        &[(291, &[1])],
        &[(106, &[0; 32])],
        &[(8, &[2]), (42, &[0; 32])],
        &[(272, &[0; 16]), (287, &[101])],
        &[(238, &[1, 0, 0, 1])],
    ];
    for mutation in mutations {
        let mut bytes = valid.clone();
        for (offset, replacement) in mutation {
            bytes[*offset..offset + replacement.len()].copy_from_slice(replacement);
        }
        assert_eq!(refusal(&n.create(&[0], &bytes)?)?, F01_INVALID_POLICY);
        assert_eq!(n.state, None);
    }
    let mut maxima = policy(1)?;
    maxima.max_workers = 32;
    maxima.max_evaluators = 8;
    maxima.max_tasks_per_epoch = 64;
    maxima.max_input_bytes = 16_777_216;
    maxima.max_output_bytes = 16_777_216;
    assert_eq!(applied_at(&n.create(&[0], &policy_bytes(&maxima)?)?)?, 1);
    assert_eq!(n.section()?.current, maxima);
    n.finish()
}

#[test]
fn a16_native_policy_width_and_assessment_modes() -> Checked {
    let mut n = Native::start()?;
    let objective = policy(1)?;
    let encoded = policy_bytes(&objective)?;
    assert_eq!(encoded.len(), 307);
    for mode in [0, 3] {
        let mut bytes = encoded.clone();
        bytes[9] = mode;
        assert_eq!(refusal(&n.create(&[0], &bytes)?)?, F01_INVALID_POLICY);
    }
    let mut subjective = objective;
    subjective.assessment_mode = 2;
    assert_eq!(
        applied_at(&n.create(&[0], &policy_bytes(&subjective)?)?)?,
        1
    );
    let current = n.section()?.current;
    assert_eq!(current, subjective);
    assert_ne!(current.digest()?, objective.digest()?);
    n.finish()
}

#[test]
fn a19_native_treasury_presence_and_later_setters() -> Checked {
    let mut n = Native::start()?;
    let encoded = policy_bytes(&policy(1)?)?;
    let treasury = n.principals[TREASURY].bytes();
    let owner = n.principals[OWNER].bytes();
    let variants: [(Vec<u8>, Option<ApplicationError>); 4] = [
        ([&[2][..], &treasury].concat(), Some(NON_CANONICAL)),
        ([&[1][..], &[0; 32]].concat(), Some(NON_CANONICAL)),
        ([&[1][..], &owner].concat(), Some(F01_PRINCIPAL_MISMATCH)),
        ([&[0][..], &treasury].concat(), None),
    ];
    for (raw, error) in variants {
        let refused = refusal(&n.create(&raw, &encoded)?)?;
        if let Some(error) = error {
            assert_eq!(refused, error);
        }
        assert_eq!(n.state, None);
    }
    applied_at(&n.create(&[&[1][..], &treasury].concat(), &encoded)?)?;
    let s = n.section()?;
    assert_eq!(
        s.header.treasury_principal,
        Presence::Present(n.principals[TREASURY])
    );
    assert_eq!(s.header.owner_principal, n.principals[OWNER]);
    assert_eq!(s.header.refund_recipient_account, AccountId::new(REFUND)?);
    let section_len = s.encoded_len()?;
    assert!(section_len <= maximum_f01_worksheet()?.section_bytes);
    assert_eq!(maximum_f01_worksheet()?.section_bytes, 16_335);
    let mut appoint = 1u64.to_be_bytes().to_vec();
    appoint.extend_from_slice(&treasury);
    appoint.push(3);
    appoint.extend_from_slice(&0u64.to_be_bytes());
    let conflict = n.send(dispatch::APPOINT_OPERATOR, OWNER, 2, &appoint, 1, 1010)?;
    assert_eq!(refusal(&conflict)?, ROLE_CONFLICT);
    let mut metadata = 1u64.to_be_bytes().to_vec();
    metadata.extend_from_slice(&[70; 32]);
    metadata.extend_from_slice(&0u64.to_be_bytes());
    let setter = n.send(dispatch::UPDATE_METADATA, TREASURY, 1, &metadata, 1, 1010)?;
    assert_eq!(refusal(&setter)?, UNAUTHORIZED);
    assert_eq!(n.revision()?, 1);
    n.finish()
}

#[test]
fn native_context_binds_chain_program_and_height() -> Checked {
    let mut n = Native::start()?;
    let payload = n.create_payload(OWNER, &[0], &policy_bytes(&policy(1)?)?)?;
    let mut foreign = n.base(dispatch::CREATE, OWNER, 1, &payload, 1)?;
    foreign.chain = ChainDomain::new([40; 32])?;
    foreign.market = derive_market(foreign.chain, n.program)?;
    assert_eq!(
        refusal(&n.submit(OWNER, 1000, &encode(&foreign)?)?)?,
        WRONG_DOMAIN
    );
    let mut other = n.base(dispatch::CREATE, OWNER, 1, &payload, 1)?;
    other.program = ProgramId::new([41; 32])?;
    other.market = derive_market(n.chain, other.program)?;
    assert_eq!(
        refusal(&n.submit(OWNER, 1000, &encode(&other)?)?)?,
        WRONG_PROGRAM
    );
    let mut expired = n.base(dispatch::CREATE, OWNER, 1, &payload, 1)?;
    expired.expiry = 1000;
    assert_eq!(
        refusal(&n.submit(OWNER, 1000, &encode(&expired)?)?)?,
        EXPIRED
    );
    assert_eq!(n.state, None);
    n.finish()
}

#[test]
fn native_selectors_without_a_landed_transition_refuse_unknown_operation() -> Checked {
    let mut n = Native::start()?;
    applied_at(&n.create(&[0], &policy_bytes(&policy(1)?)?)?)?;
    let roster = Presence::Present(RosterDigest::new([9; 32])?);
    let mut chunk = vec![0; 46];
    chunk[45] = 1;
    for selector in [
        0x010A, 0x010B, 0x0501, 0x0502, 0x0503, 0x0601, 0x0602, 0x0603, 0x0604, 0x0605, 0x0701,
        0x0702, 0x0703, 0x0801, 0x0802, 0x0803, 0x0804, 0x0805, 0x0806, 0x0807, 0x0808, 0x0809,
        0x0A01, 0x0A02,
    ] {
        let operation = Operation::decode(selector)?;
        let m = operation.metadata();
        let sequence = u64::from(m.sequence == dispatch::SequencePolicy::Role);
        let payload = if selector == 0x0A02 {
            chunk.clone()
        } else {
            vec![1; m.payload_min]
        };
        let read = m.boundary == dispatch::CallBoundary::ProgramRead;
        let mut e = n.base(operation, OUTSIDER, sequence, &payload, u64::from(!read))?;
        if !read
            && !matches!(
                selector,
                0x010A | 0x010B | 0x0601 | 0x0604 | 0x0801..=0x0804
            )
        {
            e.epoch = 1;
            e.roster = roster;
        }
        let frame = n.submit(OUTSIDER, 1001, &encode(&e)?)?;
        assert_eq!(
            status(&frame)?,
            (ResultStatus::Error, Some(UNKNOWN_OPERATION), 1),
            "selector {selector:#06x}"
        );
    }
    assert_eq!(n.revision()?, 1);
    n.finish()
}
