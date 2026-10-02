use std::ffi::OsString;
use std::path::PathBuf;
use std::process::ExitCode;
use std::sync::Arc;
use std::thread::{self, JoinHandle};
use std::time::{Duration, Instant, SystemTime, UNIX_EPOCH};

use signal_hook::consts::{SIGINT, SIGTERM};
use signal_hook::iterator::{Handle, Signals};
use x_websearch::attest::{self, Attestor, SignatureExchange};
use x_websearch::content::{self, ContentStore};
use x_websearch::crawl::{CrawlBudget, CrawlReport, Crawler, StopSignal};
use x_websearch::fetch::Fetcher;
use x_websearch::index::WebIndex;
use x_websearch::kernel::{
    self, KernelAttestor, KernelRelay, KernelWatcher, ObservationSubmitter, ProgramExchange, Step,
};
use x_websearch::keys::ATTESTOR_KEY_FILE;
use x_websearch::payment::{system_clock, PaymentGate};
use x_websearch::server::Stopper;
use x_websearch::submit::{self, Outcome, Submitter};
use x_websearch::watch::{Canonical, EvmRpc, RequestWatcher, WebRequest, WorkStage};
use x_websearch::{search, Config, KeyFiles, Keys, Limits, RouteTable, Server};

/// How long after the start of one attestation round the next one starts.
const ATTEST_INTERVAL: Duration = Duration::from_secs(2);

/// The attestor's loops: the request watcher, the attestor, the signature
/// exchange and, with a submitter key, the fulfil submitter.
struct Pipeline {
    rpc: EvmRpc,
    watcher: RequestWatcher,
    attestor: Attestor,
    exchange: Arc<SignatureExchange>,
    submitter: Option<Submitter>,
}

fn config_path(arguments: impl IntoIterator<Item = OsString>) -> Option<PathBuf> {
    let mut arguments = arguments.into_iter();
    let flag = arguments.next()?;
    let path = arguments.next()?;
    (flag == "--config" && !path.is_empty() && arguments.next().is_none())
        .then(|| PathBuf::from(path))
}

/// The kernel relay and what it runs beside: the EVM endpoint it reads the
/// registered attestor set from and the time between its steps.
struct RelayLoop {
    relay: KernelRelay,
    rpc: EvmRpc,
    interval: Duration,
}

/// What the configuration and the keys assemble into: the routes, the
/// crawler, the index it writes, the attestor's loops and the kernel relay.
struct Assembled {
    routes: RouteTable,
    crawler: Crawler,
    index: Arc<WebIndex>,
    pipeline: Option<Pipeline>,
    relay: Option<RelayLoop>,
}

/// The routes and the crawler built from the configuration and the keys.
/// Every refusal names the configuration field or key it concerns.
fn assemble(config: &Config, keys: &Keys) -> Result<Assembled, String> {
    let conformance = x_websearch::conformance_suite()
        .map_err(|error| format!("payment conformance suite: {error}"))?;
    let gate = Arc::new(
        PaymentGate::new(config, keys.receiver(), conformance, system_clock)
            .map_err(|error| format!("assets, gateway or data_dir: {error}"))?,
    );
    gate.recover_pending_deliveries()
        .map_err(|error| format!("payment delivery recovery in data_dir: {error}"))?;
    let store = Arc::new(
        ContentStore::open(&config.data_dir, &config.peers)
            .map_err(|error| format!("peers or data_dir: {error}"))?,
    );
    let index =
        Arc::new(WebIndex::open(&config.data_dir).map_err(|error| format!("data_dir: {error}"))?);
    let fetcher = Arc::new(Fetcher::new(config.fetch).map_err(|error| format!("fetch: {error}"))?);
    let crawler = Crawler::new(
        CrawlBudget::from_config(&config.crawl),
        Arc::clone(&fetcher),
        Arc::clone(&index),
    )
    .map_err(|error| format!("crawl: {error}"))?;
    let mut routes = RouteTable::new();
    search::register(&mut routes, &gate, &index, &store)
        .map_err(|error| format!("route /search: {error}"))?;
    content::register(&mut routes, &gate, &fetcher, &store)
        .map_err(|error| format!("route /fetch or /content: {error}"))?;
    let pipeline = pipeline(config, keys, &mut routes, &fetcher, &index, &store)?;
    let relay = relay(config, keys, &mut routes, &fetcher, &index, &store)?;
    let health = serde_json::to_vec(&serde_json::json!({
        "status": "ok",
        "ready": true,
        "readiness_scope": "initialized-local-resources",
        "upstream_payment_verified": false,
        "network_id": config.kernel_network_id,
        "wire_version": x_websearch::payment::PROTOCOL_VERSION.to_string(),
        "protocol_version": x_websearch::payment::PROTOCOL_VERSION,
        "source_revision": option_env!("PAXEER_X_SOURCE_REVISION"),
        "evm_attestation_enabled": pipeline.is_some(),
        "kernel_attestation_enabled": relay.is_some()
    }))
    .map_err(|error| format!("health identity: {error}"))?;
    routes
        .set_health(move |_| x_websearch::server::Response::json(200, health.clone()))
        .map_err(|error| format!("health route: {error}"))?;
    Ok(Assembled {
        routes,
        crawler,
        index,
        pipeline,
        relay,
    })
}

/// The kernel relay when the `kernel` settings are present, with the program
/// signature-exchange route registered; none without them. The relay signs
/// with the attestor key and posts as the receiver key. Its journal under
/// `<data_dir>/kernel` is loaded before any new work, so requests awaiting
/// quorum, submissions with their exact signed bytes and recorded refusals
/// survive a restart; a journal that cannot be read refuses startup.
fn relay(
    config: &Config,
    keys: &Keys,
    routes: &mut RouteTable,
    fetcher: &Arc<Fetcher>,
    index: &Arc<WebIndex>,
    store: &Arc<ContentStore>,
) -> Result<Option<RelayLoop>, String> {
    let Some(settings) = &config.kernel else {
        return Ok(None);
    };
    let attestor_key = keys
        .attestor()
        .ok_or_else(|| format!("kernel: the kernel relay needs {ATTESTOR_KEY_FILE}"))?;
    let rpc =
        EvmRpc::new(&config.evm.endpoint).map_err(|error| format!("evm.endpoint: {error}"))?;
    let authorization_file = config.gateway.authorization_file.as_deref()
        .ok_or_else(|| "gateway.authorization_file is required for kernel relay".to_owned())?;
    let topics: Vec<&[u8]> = settings.topics.iter().map(String::as_bytes).collect();
    let watcher = KernelWatcher::open(&settings.endpoint, &config.data_dir.join("kernel"), 0)
        .map_err(|error| format!("kernel.endpoint or data_dir: {error}"))?
        .with_authorization_file(authorization_file)
        .map_err(|error| format!("gateway.authorization_file: {error}"))?
        .with_topics(&topics)
        .map_err(|error| format!("kernel.topics: {error}"))?;
    let exchange = Arc::new(
        ProgramExchange::open(&config.data_dir.join("kernel-attest"), &config.peers)
            .map_err(|error| format!("peers or data_dir: {error}"))?,
    );
    kernel::register(routes, &exchange)
        .map_err(|error| format!("route /program-attestations: {error}"))?;
    let attestor = KernelAttestor::new(
        attestor_key.clone(),
        config.kernel_network_id,
        Arc::clone(fetcher),
        Arc::clone(index),
        Arc::clone(store),
    );
    let submitter = ObservationSubmitter::new(
        &settings.endpoint,
        keys.receiver().clone(),
        settings.submitter_did.clone(),
        config.kernel_network_id,
        settings.fee_limit,
    )
    .map_err(|error| format!("kernel.endpoint: {error}"))?
    .with_authorization_file(authorization_file)
    .map_err(|error| format!("gateway.authorization_file: {error}"))?
    .with_receipt_key(config.gateway.sequencer.public_key);
    let relay = KernelRelay::open(
        watcher,
        attestor,
        exchange,
        attest::AttestorSet::default(),
        submitter,
        &config.data_dir.join("kernel"),
    )
    .map_err(|error| format!("kernel journal: {error}"))?;
    Ok(Some(RelayLoop {
        relay,
        rpc,
        interval: settings.poll_interval(),
    }))
}

/// The attestor's loops when an attestor key is present, with the
/// signature-exchange route registered; none without one.
fn pipeline(
    config: &Config,
    keys: &Keys,
    routes: &mut RouteTable,
    fetcher: &Arc<Fetcher>,
    index: &Arc<WebIndex>,
    store: &Arc<ContentStore>,
) -> Result<Option<Pipeline>, String> {
    let Some(attestor_key) = keys.attestor() else {
        return Ok(None);
    };
    let rpc =
        EvmRpc::new(&config.evm.endpoint).map_err(|error| format!("evm.endpoint: {error}"))?;
    let watcher = RequestWatcher::open(
        rpc.clone(),
        config.evm.confirmations,
        &config.data_dir.join("watch"),
        None,
    )
    .map_err(|error| format!("data_dir: {error}"))?
    .with_chain_id(config.evm.chain_id)
    .map_err(|error| format!("evm.chain_id: {error}"))?;
    let exchange = Arc::new(
        SignatureExchange::open(&config.data_dir.join("attest"), &config.peers)
            .map_err(|error| format!("peers or data_dir: {error}"))?,
    );
    attest::register(routes, &exchange).map_err(|error| format!("route /attestations: {error}"))?;
    let attestor = Attestor::new(
        attestor_key.clone(),
        config.evm.chain_id,
        Arc::clone(fetcher),
        Arc::clone(index),
        Arc::clone(store),
    );
    let submitter = keys
        .submitter()
        .map(|key| {
            Submitter::open(
                rpc.clone(),
                key.clone(),
                config.evm.chain_id,
                &config.data_dir.join("submit"),
            )
            .map(|submitter| submitter.with_confirmations(u64::from(config.evm.confirmations)))
        })
        .transpose()
        .map_err(|error| format!("data_dir: {error}"))?;
    Ok(Some(Pipeline {
        rpc,
        watcher,
        attestor,
        exchange,
        submitter,
    }))
}

/// One attestation round: answer the newly confirmed requests, drop the
/// timed-out ones, exchange signatures and submit or settle each request.
fn attest_round(pipeline: &mut Pipeline) {
    if let Err(error) = pipeline.watcher.poll() {
        eprintln!("x-websearch request watch failed: {error}");
    }
    let head = pipeline.watcher.last_head();
    for request in pipeline.watcher.work() {
        let request_id = request.request_id;
        if request.timeout_height < head {
            retire_closed(pipeline, request_id);
            continue;
        }
        if !pipeline.watcher.eligible(request_id) || !still_canonical(pipeline, &request) {
            continue;
        }
        match submit::request_status_at_depth(&pipeline.rpc, request_id, pipeline.watcher.confirmations()) {
            Ok(status) if status != submit::STATUS_PENDING => {
                retire_closed(pipeline, request_id);
                continue;
            }
            Ok(_) => {},
            Err(error) => {
                eprintln!("x-websearch request {request_id} final status: {error}");
                continue;
            }
        }
        if let Some(answer) = pipeline.exchange.answer(request_id) {
            if !pipeline.attestor.binds(&request, &answer) {
                if pipeline.watcher.fail(request_id, true, "retained attestation request binding").is_ok() {
                    pipeline.exchange.forget(request_id);
                }
            }
            continue;
        }
        if let Err(error) = pipeline.watcher.transition(request_id, WorkStage::Attesting) {
            eprintln!("x-websearch request {request_id}: {error}");
            continue;
        }
        match pipeline.attestor.attest(&request) {
            Ok(answer) => {
                if pipeline.exchange.record(answer) {
                    if let Err(error) = pipeline.watcher.transition(request_id, WorkStage::Attested) {
                        eprintln!("x-websearch request {request_id}: {error}");
                    }
                } else if let Err(error) = pipeline.watcher.fail(request_id, false, "attestation persistence failed") {
                    eprintln!("x-websearch request {request_id}: {error}");
                }
            },
            Err(error) => {
                match pipeline
                    .watcher
                    .fail(request_id, error.is_terminal(), &error.to_string())
                {
                    Ok(true) => eprintln!("x-websearch refused request {request_id}: {error}"),
                    Ok(false) => eprintln!("x-websearch will retry request {request_id}: {error}"),
                    Err(journal) => eprintln!("x-websearch request {request_id}: {journal}"),
                }
            }
        }
    }
    for entry in pipeline.watcher.journal() {
        if entry.refused.is_some() && entry.stage != WorkStage::Reorged {
            retire_closed(pipeline, entry.request.request_id);
        }
    }
    pipeline.exchange.expire(head);
    let set = match submit::attestor_set(&pipeline.rpc) {
        Ok(set) => set,
        Err(error) => {
            eprintln!("x-websearch could not read the attestor set: {error}");
            return;
        }
    };
    let journal = pipeline.watcher.journal();
    for request_id in pipeline.exchange.pending() {
        let Some(entry) = journal.iter().find(|entry| entry.request.request_id == request_id) else {
            pipeline.exchange.forget(request_id);
            continue;
        };
        if !pipeline.watcher.eligible(request_id) || !still_canonical(pipeline, &entry.request) {
            continue;
        }
        if let Some(submitter) = &pipeline.submitter {
            let binding = pipeline.watcher.journal().into_iter().find(|row| row.request.request_id == request_id);
            let Some(binding) = binding else { continue; };
            let Some(source) = &binding.source else { continue; };
            if let Err(error) = submitter.bind_source(&binding.request, source) {
                eprintln!("x-websearch request {request_id}: {error}");
                continue;
            }
        }
        let held = pipeline.exchange.collect(request_id, &set);
        if pipeline.exchange.ready(request_id, &set).is_some() {
            if let Err(error) = pipeline.watcher.transition(request_id, WorkStage::Quorum) {
                eprintln!("x-websearch request {request_id}: {error}");
                continue;
            }
        }
        let settled = match &pipeline.submitter {
            Some(submitter) => settle(submitter, &pipeline.exchange, request_id, &set),
            None => submit::request_status_at_depth(&pipeline.rpc, request_id, pipeline.watcher.confirmations())
                .map(|status| status != submit::STATUS_PENDING)
                .map_err(|error| error.to_string()),
        };
        match settled {
            Ok(true) => {
                pipeline.exchange.forget(request_id);
                if let Err(error) = pipeline.watcher.retire(request_id) {
                    eprintln!("x-websearch request {request_id}: {error}");
                }
            }
            Ok(false) => {
                let signed = pipeline.submitter.as_ref().is_some_and(|submitter|
                    submitter.journal().load(request_id).ok().flatten()
                        .is_some_and(|row| row.state == submit::JournalState::Signed));
                if signed {
                    let _ = pipeline.watcher.transition(request_id, WorkStage::Signed);
                } else if pipeline.exchange.ready(request_id, &set).is_none() {
                    let _ = pipeline.watcher.fail(request_id, false, "peer quorum unavailable");
                }
            }
            Err(error) => {
                if pipeline.submitter.as_ref().is_some_and(|submitter|
                    submitter.journal().load(request_id).ok().flatten()
                        .is_some_and(|row| row.state == submit::JournalState::Signed)) {
                    let _ = pipeline.watcher.transition(request_id, WorkStage::Signed);
                }
                let _ = pipeline.watcher.fail(request_id, false, &error);
                eprintln!("x-websearch request {request_id} with {held} signatures: {error}");
            }
        }
    }
}

/// Re-validates a journalled request against the current head before any
/// further work or economic action. A request whose log is gone from its
/// block past the confirmation depth was reorged out: its signed transaction
/// is quarantined and its work record is explicitly refused.
fn still_canonical(pipeline: &mut Pipeline, request: &WebRequest) -> bool {
    let request_id = request.request_id;
    match pipeline.watcher.canonical(request) {
        Ok(Canonical::Present) => true,
        Ok(Canonical::NotFinal) => false,
        Ok(Canonical::Absent) => {
            eprintln!("x-websearch request {request_id} is no longer canonical; retiring it");
            if let Some(submitter) = &pipeline.submitter {
                if let Err(error) = submitter.abandon(request_id) {
                    eprintln!("x-websearch request {request_id}: {error}");
                    return false;
                }
            }
            if let Err(error) = pipeline.watcher.reorged(request_id) {
                eprintln!("x-websearch request {request_id}: {error}");
                return false;
            }
            pipeline.exchange.forget(request_id);
            false
        }
        Err(error) => {
            eprintln!("x-websearch request {request_id} canonical check: {error}");
            false
        }
    }
}

/// Drops a journalled request from the watcher once the chain closed it,
/// fulfilled or refunded; an open one stays journalled.
fn retire_closed(pipeline: &mut Pipeline, request_id: u64) {
    let Some(entry) = pipeline.watcher.journal().into_iter().find(|entry| entry.request.request_id == request_id) else { return; };
    if !still_canonical(pipeline, &entry.request) { return; }
    if let Some(submitter) = &pipeline.submitter {
        let Some(binding) = pipeline.watcher.journal().into_iter().find(|entry| entry.request.request_id == request_id) else { return; };
        let Some(source) = &binding.source else { return; };
        if let Err(error) = submitter.bind_source(&binding.request, source) {
            eprintln!("x-websearch request {request_id}: {error}");
            return;
        }
    }
    match submit::request_status_at_depth(&pipeline.rpc, request_id, pipeline.watcher.confirmations()) {
        Ok(status) if status != submit::STATUS_PENDING => {
            if let Some(submitter) = &pipeline.submitter {
                match submitter.acknowledge_closed(request_id) {
                    Ok(true) => {},
                    Ok(false) => return,
                    Err(error) => {
                        eprintln!("x-websearch request {request_id}: {error}");
                        return;
                    }
                }
            }
            pipeline.exchange.forget(request_id);
            if let Err(error) = pipeline.watcher.retire(request_id) {
                eprintln!("x-websearch request {request_id}: {error}");
            }
        }
        Ok(_) => {}
        Err(error) => eprintln!("x-websearch request {request_id} status: {error}"),
    }
}

/// Confirms a journalled fulfil, or submits one once the threshold is
/// reached. `true` means the request needs nothing more.
fn settle(
    submitter: &Submitter,
    exchange: &SignatureExchange,
    request_id: u64,
    set: &attest::AttestorSet,
) -> Result<bool, String> {
    if let Some(outcome) = submitter
        .confirm(request_id)
        .map_err(|error| error.to_string())?
    {
        if outcome.completed() {
            return Ok(true);
        }
    }
    let Some(ready) = exchange.ready(request_id, set) else {
        return Ok(false);
    };
    submitter
        .submit(&ready)
        .map(Outcome::completed)
        .map_err(|error| error.to_string())
}

/// Reconciles canonical requests and journalled fulfilments in each attestation
/// round, every [`ATTEST_INTERVAL`] until `stop` is raised.
fn start_attestor(mut pipeline: Pipeline, stop: StopSignal) -> std::io::Result<JoinHandle<()>> {
    thread::Builder::new()
        .name("x-websearch-attestor".to_owned())
        .spawn(move || {
            while !stop.is_raised() {
                let started = Instant::now();
                attest_round(&mut pipeline);
                if stop.wait_timeout(ATTEST_INTERVAL.saturating_sub(started.elapsed())) {
                    break;
                }
            }
        })
}

/// Milliseconds since the Unix epoch.
fn now_ms() -> u64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map_or(0, |elapsed| {
            u64::try_from(elapsed.as_millis()).unwrap_or(u64::MAX)
        })
}

/// One relay step: refresh the registered attestor set, then answer, exchange
/// and post the program requests.
fn relay_round(relay: &mut RelayLoop) {
    match submit::attestor_set(&relay.rpc) {
        Ok(set) => relay.relay.set = set,
        Err(error) => {
            relay.relay.set = attest::AttestorSet::default();
            eprintln!("x-websearch could not read the attestor set: {error}");
        },
    }
    match relay.relay.step(now_ms()) {
        Ok(steps) => {
            for step in steps {
                match step {
                    Step::Posted {
                        program_id,
                        request_id,
                        ..
                    } => eprintln!(
                        "x-websearch posted the observation of program {} request {request_id}",
                        x_websearch::payment::hex(&program_id)
                    ),
                    Step::Refused {
                        program_id,
                        request_id,
                        reason,
                    } => eprintln!(
                        "x-websearch could not answer program {} request {request_id}: {reason}",
                        x_websearch::payment::hex(&program_id)
                    ),
                    Step::Committed {
                        program_id,
                        request_id,
                        activity_id,
                        ..
                    } => eprintln!(
                        "x-websearch committed the observation of program {} request {request_id} as activity {}",
                        x_websearch::payment::hex(&program_id),
                        x_websearch::payment::hex(&activity_id)
                    ),
                    Step::Unknown {
                        program_id,
                        request_id,
                        activity_id,
                    } => eprintln!(
                        "x-websearch observation of program {} request {request_id} awaits its receipt (activity {})",
                        x_websearch::payment::hex(&program_id),
                        x_websearch::payment::hex(&activity_id)
                    ),
                    Step::Rejected {
                        program_id,
                        request_id,
                        code,
                    } => eprintln!(
                        "x-websearch gateway refused the observation of program {} request {request_id} with {code}",
                        x_websearch::payment::hex(&program_id)
                    ),
                }
            }
        }
        Err(error) => eprintln!("x-websearch kernel relay step failed: {error}"),
    }
}

/// Runs a relay step every interval on its own thread until `stop` is
/// raised. The journalled requests and submissions were loaded when the
/// relay opened, before this loop takes any new work.
fn start_relay(mut relay: RelayLoop, stop: StopSignal) -> std::io::Result<JoinHandle<()>> {
    thread::Builder::new()
        .name("x-websearch-kernel-relay".to_owned())
        .spawn(move || {
            eprintln!(
                "x-websearch kernel relay started from sequence {} with {} journalled requests in {}",
                relay.relay.watcher.next_sequence(),
                relay.relay.entries().len(),
                relay
                    .relay
                    .journal_path()
                    .unwrap_or_else(|| std::path::Path::new(""))
                    .display()
            );
            while !stop.is_raised() {
                let started = Instant::now();
                relay_round(&mut relay);
                if stop.wait_timeout(relay.interval.saturating_sub(started.elapsed())) {
                    break;
                }
            }
            eprintln!("x-websearch kernel relay stopped");
        })
}

fn describe(report: &CrawlReport) -> String {
    format!(
        "x-websearch crawl cycle finished: {} visited, {} indexed, {} deferred",
        report.pages.len(),
        report.indexed().count(),
        report.deferred
    )
}

/// Runs a crawl cycle over the seeds every `interval`, the first at once,
/// on its own thread until `stop` is raised.
fn start_crawler(
    mut crawler: Crawler,
    seeds: Vec<String>,
    interval: Duration,
    stop: StopSignal,
) -> std::io::Result<JoinHandle<()>> {
    thread::Builder::new()
        .name("x-websearch-crawler".to_owned())
        .spawn(move || {
            crawler.run_every(&seeds, interval, &stop, |result| match result {
                Ok(report) => eprintln!("{}", describe(&report)),
                Err(error) => eprintln!("x-websearch crawl cycle failed: {error}"),
            });
        })
}

/// Raises `stop` and stops the server on the first SIGTERM or SIGINT, on
/// its own thread. Every other signal keeps its default action.
fn start_signals(
    mut signals: Signals,
    stop: StopSignal,
    server: Stopper,
) -> std::io::Result<JoinHandle<Option<i32>>> {
    thread::Builder::new()
        .name("x-websearch-signals".to_owned())
        .spawn(move || {
            let received = signals.forever().next();
            stop.raise();
            server.stop();
            received
        })
}

fn signal_name(signal: i32) -> &'static str {
    match signal {
        SIGTERM => "SIGTERM",
        SIGINT => "SIGINT",
        _ => "a signal",
    }
}

/// Stops the loops, waits for them, and commits the index. `true` when
/// every step succeeded.
fn wind_down(
    stop: &StopSignal,
    signals: &Handle,
    watcher: JoinHandle<Option<i32>>,
    loops: Vec<JoinHandle<()>>,
    index: &WebIndex,
) -> bool {
    stop.raise();
    signals.close();
    let mut clean = true;
    match watcher.join() {
        Ok(Some(signal)) => eprintln!("x-websearch stopping on {}", signal_name(signal)),
        Ok(None) => {}
        Err(_) => {
            eprintln!("x-websearch stopped: the signal thread panicked");
            clean = false;
        }
    }
    for handle in loops {
        if handle.join().is_err() {
            eprintln!("x-websearch stopped: a sidecar thread panicked");
            clean = false;
        }
    }
    if let Err(error) = index.commit() {
        eprintln!("x-websearch stopped: index commit failed: {error}");
        clean = false;
    }
    clean
}

fn main() -> ExitCode {
    let Some(path) = config_path(std::env::args_os().skip(1)) else {
        eprintln!("Paxeer X Network web search sidecar");
        eprintln!("usage: x-websearch --config PATH");
        return ExitCode::from(2);
    };
    let config = match x_websearch::load(&path) {
        Ok(config) => config,
        Err(error) => {
            eprintln!("{error}");
            return ExitCode::from(2);
        }
    };
    let keys = match KeyFiles::from_env().and_then(|files| files.load()) {
        Ok(keys) => keys,
        Err(error) => {
            eprintln!("{error}");
            return ExitCode::from(2);
        }
    };
    let assembled = assemble(&config, &keys);
    drop(keys);
    let Assembled {
        routes,
        crawler,
        index,
        pipeline,
        relay,
    } = match assembled {
        Ok(assembled) => assembled,
        Err(error) => {
            eprintln!("x-websearch refused startup: {error}");
            return ExitCode::from(2);
        }
    };
    let signals = match Signals::new([SIGTERM, SIGINT]) {
        Ok(signals) => signals,
        Err(error) => {
            eprintln!("x-websearch refused startup: signal handling: {error}");
            return ExitCode::FAILURE;
        }
    };
    let server =
        match Server::bind(config.listen, Limits::default(), routes).and_then(Server::spawn) {
            Ok(server) => server,
            Err(error) => {
                eprintln!("x-websearch refused startup: listen: {error}");
                return ExitCode::FAILURE;
            }
        };
    let stop = StopSignal::new();
    let handle = signals.handle();
    let watcher = match start_signals(signals, stop.clone(), server.stopper()) {
        Ok(watcher) => watcher,
        Err(error) => {
            eprintln!("x-websearch refused startup: signal thread: {error}");
            drop(server.shutdown());
            return ExitCode::FAILURE;
        }
    };
    let (loops, started) = start_loops(
        crawler,
        pipeline,
        relay,
        config.seeds.clone(),
        config.crawl_interval(),
        &stop,
    );
    match &started {
        Ok(()) => eprintln!("x-websearch listening on {}", config.listen),
        Err(error) => {
            eprintln!("x-websearch refused startup: {error}");
            server.stopper().stop();
        }
    }
    let outcome = server.wait();
    if let Err(error) = &outcome {
        eprintln!("x-websearch stopped: {error}");
    }
    let clean = wind_down(&stop, &handle, watcher, loops, &index);
    if started.is_ok() && outcome.is_ok() && clean {
        eprintln!("x-websearch stopped cleanly");
        ExitCode::SUCCESS
    } else {
        ExitCode::FAILURE
    }
}

/// Starts the crawler, with an attestor key the attestor loop, and with the
/// kernel settings the kernel relay. The handles of the threads that started
/// come back with the first failure.
fn start_loops(
    crawler: Crawler,
    pipeline: Option<Pipeline>,
    relay: Option<RelayLoop>,
    seeds: Vec<String>,
    interval: Duration,
    stop: &StopSignal,
) -> (Vec<JoinHandle<()>>, Result<(), String>) {
    let mut loops = Vec::with_capacity(3);
    match start_crawler(crawler, seeds, interval, stop.clone()) {
        Ok(handle) => loops.push(handle),
        Err(error) => return (loops, Err(format!("crawler thread: {error}"))),
    }
    if let Some(pipeline) = pipeline {
        match start_attestor(pipeline, stop.clone()) {
            Ok(handle) => loops.push(handle),
            Err(error) => return (loops, Err(format!("attestor thread: {error}"))),
        }
    }
    if let Some(relay) = relay {
        match start_relay(relay, stop.clone()) {
            Ok(handle) => loops.push(handle),
            Err(error) => return (loops, Err(format!("kernel relay thread: {error}"))),
        }
    }
    (loops, Ok(()))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn exact_config_arguments_required() {
        assert_eq!(
            config_path(["--config".into(), "x-websearch.json".into()]),
            Some(PathBuf::from("x-websearch.json"))
        );
        for args in [
            vec![],
            vec!["--config"],
            vec!["--other", "x-websearch.json"],
            vec!["--config", ""],
            vec!["--config", "x-websearch.json", "extra"],
        ] {
            assert_eq!(config_path(args.into_iter().map(OsString::from)), None);
        }
    }

    #[test]
    fn a_settled_journal_entry_completes_the_request_before_any_call(
    ) -> Result<(), Box<dyn std::error::Error>> {
        let scratch =
            std::env::temp_dir().join(format!("x-websearch-main-settle-{}", std::process::id()));
        let _ = std::fs::remove_dir_all(&scratch);
        let mut secret = [0_u8; 32];
        secret[0] = 0x5a;
        secret[31] = 1;
        let key = k256::ecdsa::SigningKey::from_slice(&secret)?;
        let unreachable = EvmRpc::new("http://127.0.0.1:1")?;
        let submitter = Submitter::open(unreachable, key, 713_714, &scratch.join("submit"))?;
        submitter.journal().store(&submit::JournalEntry {
            request_id: 7,
            state: submit::JournalState::AlreadyFulfilled,
            transaction: None,
        })?;
        let exchange = SignatureExchange::open(&scratch.join("attest"), &[])?;
        let set = attest::AttestorSet::default();
        assert_eq!(settle(&submitter, &exchange, 7, &set), Ok(true));
        assert_eq!(settle(&submitter, &exchange, 8, &set), Ok(false));
        std::fs::remove_dir_all(&scratch)?;
        Ok(())
    }

    #[test]
    fn a_crawl_report_is_described_by_its_counts() {
        let report = CrawlReport {
            pages: Vec::new(),
            deferred: 3,
        };
        assert_eq!(
            describe(&report),
            "x-websearch crawl cycle finished: 0 visited, 0 indexed, 3 deferred"
        );
    }
}
