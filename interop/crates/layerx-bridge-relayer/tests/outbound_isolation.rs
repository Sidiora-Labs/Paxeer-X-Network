use std::collections::{BTreeMap, BTreeSet};
use std::fs::{self, File, OpenOptions};
use std::io::{Read as _, Write as _};
use std::net::IpAddr;
use std::os::unix::fs::PermissionsExt as _;
use std::os::unix::process::CommandExt as _;
use std::path::{Path, PathBuf};
use std::process::{Child, Command, ExitStatus, Stdio};
use std::time::{Duration, Instant, SystemTime, UNIX_EPOCH};
use std::sync::OnceLock;

use layerx_bridge_relayer::config::{RelayerConfig, SignerTransportConfig};
use layerx_bridge_relayer::journal::{Journal, JournalError, Observation, State};
use layerx_bridge_relayer::relayer::{Relayer, RelayerError};
use layerx_bridge_relayer::rpc::RpcFault;
use layerx_bridge_relayer::solana::SOLANA_CHAIN_ID;
use serde::Deserialize;
use serde_json::{json, Value};
use sha2::{Digest as _, Sha256};

type Result<T> = std::result::Result<T, Box<dyn std::error::Error>>;
const MANIFEST_ENV: &str = "LAYERX_BRIDGE_CAPTURE_MANIFEST";
const MANIFEST_PIN_ENV: &str = "LAYERX_BRIDGE_CAPTURE_MANIFEST_SHA256";
const MAX_DOCUMENT: u64 = 16 * 1024 * 1024;
static DEADLINE: OnceLock<Instant> = OnceLock::new();

fn deadline(seconds: u64) -> Instant {
    let limit = *DEADLINE.get_or_init(|| Instant::now() + Duration::from_secs(12 * 60));
    limit.min(Instant::now() + Duration::from_secs(seconds))
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct Artifact {
    path: PathBuf,
    sha256: String,
    kind: String,
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct Invocation {
    executable: String,
    arguments: Vec<String>,
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct Service {
    role: String,
    command: Invocation,
    ready_file: PathBuf,
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct Scenario {
    fault: String,
    config: String,
    seed_journal: Option<String>,
    failing_item: String,
    blocked_item: String,
    evm_item: String,
    solana_item: String,
    failing_chain: u64,
    ready_chain: u64,
    evm_nonce: u64,
    failing_nonce: u64,
    failure_operation: String,
    failure_requests_per_pass: usize,
    services: Vec<Service>,
    actions: BTreeMap<String, Invocation>,
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct Manifest {
    schema: String,
    capture_id: String,
    capture_kind: String,
    producer_revision: String,
    captured_at_utc: String,
    provenance: String,
    artifacts: BTreeMap<String, Artifact>,
    scenarios: Vec<Scenario>,
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct Provenance {
    capture_id: String,
    producer_revision: String,
    captured_at_utc: String,
    source: String,
    captures: Vec<String>,
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct Ready {
    capture_id: String,
    manifest_sha256: String,
    fault: String,
    role: String,
}

#[derive(Clone, Debug, Deserialize, PartialEq, Eq)]
#[serde(deny_unknown_fields)]
struct Event {
    sequence: u64,
    kind: String,
    item: String,
    chain_id: u64,
    operation: String,
    payload_hex: String,
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct Trace {
    capture_id: String,
    manifest_sha256: String,
    fault: String,
    events: Vec<Event>,
    effects: BTreeMap<String, u64>,
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct Failure {
    item: String,
    chain_id: u64,
    kind: String,
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct Report {
    observed: usize,
    submitted: usize,
    completed: usize,
    waiting: usize,
    refused: usize,
    failures: Vec<Failure>,
}

fn read(path: &Path) -> Result<Vec<u8>> {
    let file = File::open(path)?;
    if file.metadata()?.len() > MAX_DOCUMENT {
        return Err("capture document exceeds its bound".into());
    }
    let mut bytes = Vec::new();
    file.take(MAX_DOCUMENT + 1).read_to_end(&mut bytes)?;
    if bytes.len() as u64 > MAX_DOCUMENT {
        return Err("capture document grew beyond its bound".into());
    }
    Ok(bytes)
}

fn digest(path: &Path) -> Result<String> {
    let mut file = File::open(path)?;
    let mut hash = Sha256::new();
    let mut buffer = [0_u8; 65536];
    loop {
        if Instant::now() >= deadline(12 * 60) {
            return Err("capture artifact hashing exceeded the qualification deadline".into());
        }
        let count = file.read(&mut buffer)?;
        if count == 0 {
            break;
        }
        hash.update(&buffer[..count]);
    }
    Ok(layerx_bridge_relayer::hex::encode(&hash.finalize()))
}

fn relative(root: &Path, path: &Path) -> Result<PathBuf> {
    if path.as_os_str().is_empty() || path.components().any(|part| {
        !matches!(part, std::path::Component::Normal(_))
    }) {
        return Err("capture paths must be nonempty normal relative paths".into());
    }
    Ok(root.join(path))
}

fn pin(text: &str, length: usize) -> bool {
    text.len() == length && text.bytes().all(|byte| byte.is_ascii_hexdigit())
}

struct Corpus {
    root: PathBuf,
    manifest: Manifest,
    pin: String,
}

impl Corpus {
    fn load() -> Result<Self> {
        let path = PathBuf::from(std::env::var(MANIFEST_ENV).map_err(|_| {
            "missing LAYERX_BRIDGE_CAPTURE_MANIFEST; genuine captured boundary corpus is required"
        })?);
        let expected = std::env::var(MANIFEST_PIN_ENV).map_err(|_| {
            "missing independent LAYERX_BRIDGE_CAPTURE_MANIFEST_SHA256 pin"
        })?;
        if !path.is_absolute() || !pin(&expected, 64) || digest(&path)? != expected {
            return Err("capture manifest path or independent SHA256 pin refused".into());
        }
        let root = path.parent().ok_or("capture root missing")?.canonicalize()?;
        let manifest: Manifest = serde_json::from_slice(&read(&path)?)?;
        if manifest.schema != "layerx.bridge.outbound-isolation/v1"
            || manifest.capture_kind != "genuine-boundary-capture"
            || manifest.capture_id.is_empty()
            || !pin(&manifest.producer_revision, 40)
            || !manifest.captured_at_utc.ends_with('Z')
        {
            return Err("capture schema or provenance refused".into());
        }
        if manifest.artifacts.len() > 64 || manifest.artifacts.is_empty() {
            return Err("capture artifact count is out of bounds".into());
        }
        let corpus = Self { root, manifest, pin: expected };
        for (name, artifact) in &corpus.manifest.artifacts {
            if name.is_empty() || !pin(&artifact.sha256, 64)
                || !matches!(artifact.kind.as_str(), "capture" | "provenance" | "executable" | "config" | "journal")
            {
                return Err("capture artifact declaration refused".into());
            }
            corpus.artifact(name, &artifact.kind)?;
        }
        let provenance: Provenance = serde_json::from_slice(&read(
            &corpus.artifact(&corpus.manifest.provenance, "provenance")?
        )?)?;
        if provenance.capture_id != corpus.manifest.capture_id
            || provenance.producer_revision != corpus.manifest.producer_revision
            || provenance.captured_at_utc != corpus.manifest.captured_at_utc
            || provenance.source.is_empty() || provenance.captures.is_empty()
        {
            return Err("capture provenance does not bind this corpus".into());
        }
        for capture in &provenance.captures {
            corpus.artifact(capture, "capture")?;
        }
        let faults: BTreeSet<_> = corpus.manifest.scenarios.iter().map(|case| case.fault.as_str()).collect();
        if corpus.manifest.scenarios.len() != 3 || faults != BTreeSet::from(["rpc", "receipt", "signer"]) {
            return Err("RPC, receipt and signer fault scenarios are all mandatory".into());
        }
        for case in &corpus.manifest.scenarios {
            corpus.artifact(&case.config, "config")?;
            if let Some(journal) = &case.seed_journal {
                corpus.artifact(journal, "journal")?;
            }
            if case.fault == "receipt" && case.seed_journal.is_none() {
                return Err("receipt fault requires a genuine pending journal capture".into());
            }
            if case.failing_chain == case.ready_chain || case.failing_chain == SOLANA_CHAIN_ID
                || case.ready_chain == SOLANA_CHAIN_ID || case.failure_requests_per_pass == 0
                || case.failure_requests_per_pass > 8 || case.failure_operation.is_empty()
            {
                return Err("independent destination or bounded fault declaration refused".into());
            }
            let items: BTreeSet<_> = [&case.failing_item, &case.blocked_item, &case.evm_item, &case.solana_item].into_iter().collect();
            if items.len() != 4 || items.iter().any(|item| item.is_empty()) {
                return Err("four distinct captured items are required".into());
            }
            let roles: BTreeSet<_> = case.services.iter().map(|service| service.role.as_str()).collect();
            if roles != BTreeSet::from(["paxeer", "failing-evm", "ready-evm", "solana", "signer"]) || case.services.len() != 5 {
                return Err("all five production service roles are required".into());
            }
            for service in &case.services {
                corpus.artifact(&service.command.executable, "executable")?;
                relative(Path::new("/"), &service.ready_file)?;
            }
            let phases: BTreeSet<_> = case.actions.keys().map(String::as_str).collect();
            if phases != BTreeSet::from(["down", "repeat", "restart", "finalize_first", "finalize_all", "journal_failure", "snapshot"]) {
                return Err("capture scenario lacks a required bounded phase".into());
            }
            for action in case.actions.values() {
                corpus.artifact(&action.executable, "executable")?;
            }
        }
        Ok(corpus)
    }

    fn artifact(&self, name: &str, kind: &str) -> Result<PathBuf> {
        let entry = self.manifest.artifacts.get(name).ok_or("capture artifact is not declared")?;
        let path = relative(&self.root, &entry.path)?.canonicalize()?;
        let maximum = if kind == "executable" { 512 * 1024 * 1024 } else { MAX_DOCUMENT };
        if entry.kind != kind || !path.starts_with(&self.root) || !path.is_file()
            || fs::metadata(&path)?.len() > maximum || digest(&path)? != entry.sha256
        {
            return Err("capture artifact kind, confinement or SHA256 mismatch".into());
        }
        if kind == "executable" && fs::metadata(&path)?.permissions().mode() & 0o111 == 0 {
            return Err("supplied production executable is not executable".into());
        }
        Ok(path)
    }

    fn command(&self, invocation: &Invocation, dir: &Path, case: &Scenario) -> Result<Command> {
        let mut command = Command::new(self.artifact(&invocation.executable, "executable")?);
        command.process_group(0).env_clear().current_dir(dir)
            .env("PATH", "/usr/bin:/bin")
            .env("LAYERX_BRIDGE_CAPTURE_ROOT", &self.root)
            .env("LAYERX_BRIDGE_CASE_DIR", dir)
            .env("LAYERX_BRIDGE_CAPTURE_ID", &self.manifest.capture_id)
            .env("LAYERX_BRIDGE_CAPTURE_SHA256", &self.pin)
            .env("LAYERX_BRIDGE_CAPTURE_FAULT", &case.fault)
            .stdin(Stdio::null());
        for argument in &invocation.arguments {
            command.arg(argument.replace("{capture_root}", self.root.to_str().ok_or("capture root is not text")?)
                .replace("{case_dir}", dir.to_str().ok_or("case directory is not text")?)
                .replace("{fault}", &case.fault));
        }
        Ok(command)
    }
}

fn terminate(child: &mut Child) {
    let group = format!("-{}", child.id());
    let _ = Command::new("/bin/kill").args(["-KILL", "--", &group])
        .stdin(Stdio::null()).stdout(Stdio::null()).stderr(Stdio::null()).status();
    let _ = child.kill();
    let _ = child.wait();
}

fn wait(child: &mut Child, deadline: Instant) -> Result<ExitStatus> {
    loop {
        if let Some(status) = child.try_wait()? {
            return Ok(status);
        }
        if Instant::now() >= deadline {
            terminate(child);
            return Err("bounded capture child timed out".into());
        }
        std::thread::sleep(Duration::from_millis(20));
    }
}

struct Services(Vec<Child>);

impl Drop for Services {
    fn drop(&mut self) {
        for child in &mut self.0 {
            terminate(child);
        }
    }
}

fn startup(corpus: &Corpus, case: &Scenario, dir: &Path) -> Result<Services> {
    let mut children = Services(Vec::new());
    for service in &case.services {
        let ready = relative(dir, &service.ready_file)?;
        if ready.exists() {
            return Err("stale service readiness artifact refused".into());
        }
        let mut command = corpus.command(&service.command, dir, case)?;
        command.stdout(File::create(dir.join(format!("{}.stdout", service.role)))?)
            .stderr(File::create(dir.join(format!("{}.stderr", service.role)))?);
        children.0.push(command.spawn()?);
    }
    let deadline = deadline(20);
    loop {
        for child in &mut children.0 {
            if child.try_wait()?.is_some() {
                return Err("production capture service exited before readiness".into());
            }
        }
        if case.services.iter().all(|service| relative(dir, &service.ready_file).is_ok_and(|path| path.is_file())) {
            break;
        }
        if Instant::now() >= deadline {
            return Err("production capture service readiness timed out".into());
        }
        std::thread::sleep(Duration::from_millis(20));
    }
    for service in &case.services {
        let ready: Ready = serde_json::from_slice(&read(&relative(dir, &service.ready_file)?)?)?;
        if ready.capture_id != corpus.manifest.capture_id || ready.manifest_sha256 != corpus.pin
            || ready.fault != case.fault || ready.role != service.role
        {
            return Err("service readiness is not bound to this captured case".into());
        }
    }
    Ok(children)
}

fn action(corpus: &Corpus, case: &Scenario, dir: &Path, phase: &str) -> Result<()> {
    let invocation = case.actions.get(phase).ok_or("required phase missing")?;
    let mut command = corpus.command(invocation, dir, case)?;
    command.stdout(File::create(dir.join(format!("{phase}.stdout")))?)
        .stderr(File::create(dir.join(format!("{phase}.stderr")))?);
    let mut child = command.spawn()?;
    if !wait(&mut child, deadline(20))?.success() {
        return Err("capture phase producer refused; see its private log".into());
    }
    Ok(())
}

fn snapshot(corpus: &Corpus, case: &Scenario, dir: &Path) -> Result<Trace> {
    let path = dir.join("trace.json");
    if path.exists() {
        fs::remove_file(&path)?;
    }
    action(corpus, case, dir, "snapshot")?;
    let trace: Trace = serde_json::from_slice(&read(&path)?)?;
    if trace.capture_id != corpus.manifest.capture_id || trace.manifest_sha256 != corpus.pin || trace.fault != case.fault {
        return Err("transport trace is not bound to this captured case".into());
    }
    for (index, event) in trace.events.iter().enumerate() {
        if event.sequence != index as u64 || !matches!(event.kind.as_str(), "rpc" | "sign" | "send") {
            return Err("transport trace has a gap or unknown event kind".into());
        }
        layerx_bridge_relayer::hex::decode(&event.payload_hex)?;
    }
    Ok(trace)
}

fn since<'a>(before: &Trace, after: &'a Trace) -> Result<&'a [Event]> {
    if !after.events.starts_with(&before.events) {
        return Err("transport trace was truncated or rewritten".into());
    }
    Ok(&after.events[before.events.len()..])
}

fn loopback(url: &str) -> Result<()> {
    let authority = url.strip_prefix("https://").or_else(|| url.strip_prefix("http://"))
        .ok_or("non-HTTP capture endpoint refused")?.split('/').next().ok_or("endpoint authority missing")?;
    let host = if authority.starts_with('[') {
        authority.split(']').next().ok_or("IPv6 endpoint malformed")?.trim_start_matches('[')
    } else {
        authority.split(':').next().ok_or("endpoint host missing")?
    };
    if !host.parse::<IpAddr>().is_ok_and(|address| address.is_loopback()) {
        return Err("capture adapters must target numeric loopback endpoints only".into());
    }
    Ok(())
}

fn config(corpus: &Corpus, case: &Scenario, dir: &Path) -> Result<PathBuf> {
    fn expand(value: &mut Value, root: &str, dir: &str) {
        match value {
            Value::String(text) => *text = text.replace("{capture_root}", root).replace("{case_dir}", dir),
            Value::Array(values) => values.iter_mut().for_each(|value| expand(value, root, dir)),
            Value::Object(values) => values.values_mut().for_each(|value| expand(value, root, dir)),
            _ => {}
        }
    }
    let mut value: Value = serde_json::from_slice(&read(&corpus.artifact(&case.config, "config")?)?)?;
    expand(&mut value, corpus.root.to_str().ok_or("capture root not text")?, dir.to_str().ok_or("case directory not text")?);
    value["journal_path"] = json!(dir.join("journal.jsonl"));
    if !value["cosign_directory"].is_null() {
        return Err("focused capture must use a complete local remote-signer authority".into());
    }
    let config: RelayerConfig = serde_json::from_value(value.clone())?;
    if config.chains.len() != 2 || config.signer.timeout_ms > 2000 {
        return Err("capture requires two bounded EVM destinations".into());
    }
    for endpoint in &config.paxeer.endpoints {
        loopback(&endpoint.url)?;
        if endpoint.request_timeout_ms > 2000 {
            return Err("Paxeer capture request bound exceeded".into());
        }
    }
    let handles: BTreeSet<_> = config.chains.iter().map(|chain| chain.submitter.handle.as_str()).collect();
    if handles.len() != 2 {
        return Err("destination-specific signer refusal needs distinct submitter handles".into());
    }
    let solana = config.solana.as_ref().ok_or("Solana releases are required")?;
    if solana.release_mints.is_empty() {
        return Err("Solana release mint missing".into());
    }
    let chains: BTreeSet<_> = config.chains.iter().map(|chain| chain.chain_id).collect();
    if chains != BTreeSet::from([case.failing_chain, case.ready_chain]) {
        return Err("captured destination identities differ from production config".into());
    }
    for rpc in config.chains.iter().map(|chain| &chain.rpc).chain(std::iter::once(&solana.rpc)) {
        if rpc.connect_timeout_ms > 2000 || rpc.request_timeout_ms > 2000 {
            return Err("destination capture request bound exceeded".into());
        }
        for endpoint in &rpc.endpoints {
            loopback(&endpoint.url)?;
            if !endpoint.url.starts_with("https://") {
                return Err("production destination adapters require pinned HTTPS".into());
            }
        }
    }
    match &config.signer.endpoint {
        SignerTransportConfig::Uds { socket } if socket.starts_with(dir) => {}
        SignerTransportConfig::MutualTls { endpoint, .. } if endpoint.ip().is_loopback() => {}
        _ => return Err("remote signer must be confined to this local capture".into()),
    }
    let path = dir.join("relayer.json");
    fs::write(&path, serde_json::to_vec(&value)?)?;
    Ok(path)
}

fn phase(config: &Path, dir: &Path, mode: &str) -> Result<Option<Report>> {
    let executable = std::env::current_exe()?;
    let output = dir.join("step.json");
    if output.exists() {
        fs::remove_file(&output)?;
    }
    let mut command = if mode == "unwritable" {
        let mut shell = Command::new("/bin/sh");
        shell.arg("-c").arg("trap '' XFSZ; ulimit -f 0; exec \"$@\"")
            .arg("bridge-journal-limit").arg(&executable);
        shell
    } else {
        Command::new(executable)
    };
    command.process_group(0).args(["--exact", "outbound_isolation_phase_worker", "--nocapture"])
        .env_clear().env("PATH", "/usr/bin:/bin")
        .env("LAYERX_BRIDGE_PHASE_CONFIG", config)
        .env("LAYERX_BRIDGE_PHASE_OUTPUT", &output)
        .env("LAYERX_BRIDGE_PHASE_MODE", mode)
        .current_dir(dir).stdin(Stdio::null()).stdout(Stdio::null()).stderr(Stdio::piped());
    let mut child = command.spawn()?;
    let outcome = wait(&mut child, deadline(60));
    let mut diagnostic = Vec::new();
    if let Some(stderr) = child.stderr.take() {
        stderr.take(MAX_DOCUMENT).read_to_end(&mut diagnostic)?;
    }
    fs::write(dir.join(format!("phase-{mode}.stderr")), diagnostic)?;
    let status = outcome?;
    let expected = match mode { "unwritable" => 74, "corrupt" => 75, _ => 0 };
    if status.code() != Some(expected) {
        return Err(format!("real relayer phase {mode} returned {status}, expected {expected}").into());
    }
    if expected == 0 {
        Ok(Some(serde_json::from_slice(&read(&output)?)?))
    } else {
        Ok(None)
    }
}

#[test]
fn outbound_isolation_phase_worker() -> Result<()> {
    let Some(path) = std::env::var_os("LAYERX_BRIDGE_PHASE_CONFIG") else { return Ok(()); };
    let mode = std::env::var("LAYERX_BRIDGE_PHASE_MODE")?;
    let config = RelayerConfig::load(Path::new(&path))?;
    let setup = config.build();
    if mode == "corrupt" {
        assert!(matches!(setup, Err(RelayerError::Journal(JournalError::Corrupt { .. }))));
        std::process::exit(75);
    }
    let mut relayer = Relayer::new(setup?)?;
    let step = relayer.outbound_step();
    if mode == "unwritable" {
        assert!(matches!(&step, Err(RelayerError::Journal(JournalError::Io(_)))));
        assert_eq!(relayer.outbound_step(), step);
        assert_eq!(relayer.inbound_step(0), step);
        assert_eq!(relayer.solana_step(), step);
        let tick = relayer.tick();
        assert_eq!(tick.len(), 1);
        assert_eq!(tick[0].1, step);
        std::process::exit(74);
    }
    let report = step?;
    let failures: Vec<Value> = relayer.failures().iter().map(|failure| {
        let kind = match &failure.error {
            RelayerError::Rpc(RpcFault::Unavailable) => "rpc",
            RelayerError::Rpc(RpcFault::Malformed) | RelayerError::Abi(_) | RelayerError::ReceiptFailed => "receipt",
            RelayerError::Key(_) => "signer",
            _ => "unexpected",
        };
        json!({"item": failure.item, "chain_id": failure.chain_id, "kind": kind})
    }).collect();
    fs::write(PathBuf::from(std::env::var_os("LAYERX_BRIDGE_PHASE_OUTPUT").ok_or("phase output missing")?),
        serde_json::to_vec(&json!({
            "observed": report.observed, "submitted": report.submitted,
            "completed": report.completed, "waiting": report.waiting,
            "refused": report.refused, "failures": failures
        }))?)?;
    Ok(())
}

fn state(dir: &Path) -> Result<State> {
    Ok(Journal::open(&dir.join("journal.jsonl"))?.state().clone())
}

fn assert_fault(report: &Report, case: &Scenario) {
    assert_eq!(report.failures.len(), 1);
    assert_eq!(report.failures[0].item, case.failing_item);
    assert_eq!(report.failures[0].chain_id, case.failing_chain);
    assert_eq!(report.failures[0].kind, case.fault);
    assert!(report.waiting >= 2, "failed and nonce-dependent items stay visible");
    assert_eq!(report.refused, 0);
}

fn assert_bounded(before: &Trace, after: &Trace, case: &Scenario) -> Result<()> {
    let requests = since(before, after)?.iter().filter(|event| {
        event.item == case.failing_item && event.chain_id == case.failing_chain
            && event.operation == case.failure_operation
    }).count();
    assert_eq!(requests, case.failure_requests_per_pass);
    assert!(!since(before, after)?.iter().any(|event| event.item == case.blocked_item));
    Ok(())
}

fn sends<'a>(trace: &'a Trace, item: &str) -> Vec<&'a Event> {
    trace.events.iter().filter(|event| event.kind == "send" && event.item == item).collect()
}

fn assert_rebroadcast(trace: &Trace, item: &str, raw: &[u8]) -> Result<()> {
    let transactions = sends(trace, item);
    assert!(transactions.len() >= 2, "restart must actually rebroadcast");
    for sent in transactions {
        assert_eq!(layerx_bridge_relayer::hex::decode(&sent.payload_hex)?, raw);
    }
    Ok(())
}

fn run_case(corpus: &Corpus, case: &Scenario, dir: &Path) -> Result<()> {
    fs::create_dir(dir)?;
    fs::set_permissions(dir, fs::Permissions::from_mode(0o700))?;
    let config_path = config(corpus, case, dir)?;
    if let Some(seed) = &case.seed_journal {
        fs::copy(corpus.artifact(seed, "journal")?, dir.join("journal.jsonl"))?;
    }
    let _services = startup(corpus, case, dir)?;
    let initial = snapshot(corpus, case, dir)?;
    assert!(initial.events.is_empty());
    assert!(initial.effects.values().all(|count| *count == 0));
    action(corpus, case, dir, "down")?;
    let down = phase(&config_path, dir, "normal")?.ok_or("step report missing")?;
    assert_fault(&down, case);
    assert_eq!(down.submitted, 2);
    let first = state(dir)?;
    assert_eq!(first.items.len(), 4);
    for (key, chain) in [(&case.failing_item, case.failing_chain), (&case.blocked_item, case.failing_chain), (&case.evm_item, case.ready_chain), (&case.solana_item, SOLANA_CHAIN_ID)] {
        assert!(matches!(first.items[key].observation, Observation::Outbound { chain_id, .. } if chain_id == chain));
    }
    assert!(first.items[&case.failing_item].is_open());
    if case.fault == "receipt" {
        let pending = first.items[&case.failing_item].pending().ok_or("receipt fault lacks a pending transaction")?;
        assert_eq!(pending.nonce, case.failing_nonce);
    }
    assert!(first.items[&case.blocked_item].submissions.is_empty());
    assert!(first.items[&case.blocked_item].signature.is_none());
    let evm = first.items[&case.evm_item].pending().ok_or("ready EVM did not advance")?.clone();
    let solana = first.items[&case.solana_item].pending_release().ok_or("ready Solana did not advance")?.clone();
    assert_eq!(evm.nonce, case.evm_nonce);
    let first_trace = snapshot(corpus, case, dir)?;
    assert_bounded(&initial, &first_trace, case)?;
    assert!(!sends(&first_trace, &case.evm_item).is_empty());
    assert!(!sends(&first_trace, &case.solana_item).is_empty());
    for (item, operation) in [
        (&case.evm_item, "LayerX/bridge/ethereum-eip1559/v1"),
        (&case.solana_item, "LayerX/bridge/solana-tx/v1"),
    ] {
        assert!(first_trace.events.iter().any(|event| {
            event.kind == "sign" && event.item == *item && event.operation == operation
        }), "ready destination must use its real remote signer domain");
    }
    for (item, raw) in [(&case.evm_item, &evm.raw), (&case.solana_item, &solana.raw)] {
        for sent in sends(&first_trace, item) {
            assert_eq!(layerx_bridge_relayer::hex::decode(&sent.payload_hex)?, *raw);
        }
    }
    let pending_snapshot = fs::read(dir.join("journal.jsonl"))?;

    action(corpus, case, dir, "repeat")?;
    let repeat = phase(&config_path, dir, "normal")?.ok_or("repeat report missing")?;
    assert_fault(&repeat, case);
    assert_eq!(repeat.observed, 0);
    assert_eq!(repeat.submitted, 0);
    let repeat_trace = snapshot(corpus, case, dir)?;
    assert_bounded(&first_trace, &repeat_trace, case)?;
    assert!(!since(&first_trace, &repeat_trace)?.iter().any(|event| {
        matches!(event.kind.as_str(), "sign" | "send") && event.item != case.failing_item
    }));

    action(corpus, case, dir, "restart")?;
    let restart = phase(&config_path, dir, "normal")?.ok_or("restart report missing")?;
    assert!(restart.failures.is_empty());
    let replayed = state(dir)?;
    assert_eq!(replayed.items[&case.evm_item].pending(), Some(&evm));
    assert_eq!(replayed.items[&case.solana_item].pending_release(), Some(&solana));
    assert_eq!(replayed.items[&case.evm_item].submissions.len(), 1);
    assert_eq!(replayed.items[&case.solana_item].releases.len(), 1);
    let restart_trace = snapshot(corpus, case, dir)?;
    since(&repeat_trace, &restart_trace)?;
    assert_rebroadcast(&restart_trace, &case.evm_item, &evm.raw)?;
    assert_rebroadcast(&restart_trace, &case.solana_item, &solana.raw)?;
    if let Some(pending) = first.items[&case.failing_item].pending() {
        assert_eq!(replayed.items[&case.failing_item].pending(), Some(pending));
        let sends = sends(&restart_trace, &case.failing_item);
        assert!(!sends.is_empty(), "captured pending failure must be rebroadcast after recovery");
        for sent in sends {
            assert_eq!(layerx_bridge_relayer::hex::decode(&sent.payload_hex)?, pending.raw);
        }
    }
    assert!(!since(&repeat_trace, &restart_trace)?.iter().any(|event| {
        event.kind == "sign" && (event.item == case.evm_item || event.item == case.solana_item)
    }));

    for phase_name in ["finalize_first", "finalize_all"] {
        action(corpus, case, dir, phase_name)?;
        let report = phase(&config_path, dir, "normal")?.ok_or("final report missing")?;
        assert!(report.failures.is_empty());
        assert_eq!(report.refused, 0);
    }
    let final_state = state(dir)?;
    for item in [&case.failing_item, &case.blocked_item, &case.evm_item, &case.solana_item] {
        assert!(final_state.items[item].completion.is_some());
        assert!(!final_state.items[item].is_open());
        assert_eq!(final_state.items[item].transactions(), 1);
    }
    let failing = &final_state.items[&case.failing_item].submissions[0];
    let blocked = &final_state.items[&case.blocked_item].submissions[0];
    assert_eq!(failing.nonce, case.failing_nonce);
    assert_eq!(blocked.nonce, case.failing_nonce.checked_add(1).ok_or("nonce exhausted")?);
    assert_eq!(failing.submitter, blocked.submitter);
    let completed = snapshot(corpus, case, dir)?;
    let first_failed_send = completed.events.iter().position(|event| event.kind == "send" && event.item == case.failing_item).ok_or("failed destination never recovered")?;
    let first_blocked_send = completed.events.iter().position(|event| event.kind == "send" && event.item == case.blocked_item).ok_or("blocked item never advanced")?;
    assert!(first_failed_send < first_blocked_send);
    for item in [&case.failing_item, &case.blocked_item, &case.evm_item, &case.solana_item] {
        assert_eq!(completed.effects.get(item), Some(&1));
    }
    let idle = phase(&config_path, dir, "normal")?.ok_or("idle report missing")?;
    assert_eq!((idle.observed, idle.submitted, idle.completed, idle.waiting, idle.refused), (0, 0, 0, 0, 0));
    assert!(idle.failures.is_empty());
    assert_eq!(state(dir)?, final_state);
    let idle_trace = snapshot(corpus, case, dir)?;
    assert!(!since(&completed, &idle_trace)?.iter().any(|event| matches!(event.kind.as_str(), "sign" | "send")));
    assert_eq!(idle_trace.effects, completed.effects);

    OpenOptions::new().append(true).open(dir.join("journal.jsonl"))?.write_all(b"not a journal record\n")?;
    phase(&config_path, dir, "corrupt")?;
    let corrupt_trace = snapshot(corpus, case, dir)?;
    assert!(since(&idle_trace, &corrupt_trace)?.is_empty());
    fs::write(dir.join("journal.jsonl"), pending_snapshot)?;
    action(corpus, case, dir, "journal_failure")?;
    let before_failure = snapshot(corpus, case, dir)?;
    phase(&config_path, dir, "unwritable")?;
    let after_failure = snapshot(corpus, case, dir)?;
    assert!(!since(&before_failure, &after_failure)?.iter().any(|event| matches!(event.kind.as_str(), "sign" | "send")));
    assert_eq!(after_failure.effects, before_failure.effects);
    assert_eq!(state(dir)?, first);
    Ok(())
}

#[test]
fn outbound_isolation_captured_production_boundaries() -> Result<()> {
    let _ = DEADLINE.set(Instant::now() + Duration::from_secs(12 * 60));
    let corpus = Corpus::load()?;
    let root = std::env::temp_dir().join(format!("bridge-outbound-{}-{}", std::process::id(), SystemTime::now().duration_since(UNIX_EPOCH)?.as_nanos()));
    fs::create_dir(&root)?;
    fs::set_permissions(&root, fs::Permissions::from_mode(0o700))?;
    let deadline = Instant::now() + Duration::from_secs(12 * 60);
    for case in &corpus.manifest.scenarios {
        if Instant::now() >= deadline {
            return Err("captured outbound qualification exceeded its bounded pass".into());
        }
        run_case(&corpus, case, &root.join(&case.fault))?;
    }
    Ok(())
}
