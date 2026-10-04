use std::collections::BTreeMap;
use std::ffi::OsString;
use std::net::ToSocketAddrs as _;
use std::path::PathBuf;
use std::process::ExitCode;
use std::sync::{Arc, Mutex};
use std::thread::{self, JoinHandle};
use std::time::{Duration, Instant, SystemTime, UNIX_EPOCH};

use serde::Serialize;
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
use x_websearch::payment::{system_clock, GatewayRpc, PaymentGate, RpcAnswer};

#[derive(Clone, Copy, Eq, PartialEq, Serialize)]
#[serde(rename_all = "snake_case")]
enum DependencyState {
    Starting,
    Ready,
    Unavailable,
    Stale,
}

#[derive(Clone, Serialize)]
struct DependencyReadiness {
    dependency: &'static str,
    state: DependencyState,
    critical: bool,
    last_success_unix_ms: Option<u64>,
}

#[derive(Clone, Serialize)]
struct RoleReadiness {
    role: &'static str,
    state: DependencyState,
    dependencies: Vec<DependencyReadiness>,
    freshness_budget_ms: u64,
    first_use_deadline_unix_ms: u64,
}

struct Readiness {
    roles: Mutex<BTreeMap<&'static str, RoleReadiness>>,
    network_id: u32,
}

impl Readiness {
    fn new(config: &Config, evm: bool, kernel: bool) -> Self {
        let mut roles = BTreeMap::new();
        let mut add = |role, names: &[&'static str], budget| {
            roles.insert(
                role,
                RoleReadiness {
                    role,
                    state: DependencyState::Starting,
                    dependencies: names
                        .iter()
                        .map(|dependency| DependencyReadiness {
                            dependency,
                            state: DependencyState::Starting,
                            critical: true,
                            last_success_unix_ms: None,
                        })
                        .collect(),
                    freshness_budget_ms: budget,
                    first_use_deadline_unix_ms: now_ms().saturating_add(budget),
                },
            );
        };
        add(
            "paid_delivery",
            &[
                "index",
                "content_storage",
                "payment_journal",
                "settlement_authority",
            ],
            60_000,
        );
        if evm {
            add(
                "evm_attestor",
                &[
                    "evm_chain",
                    "registered_peer_quorum",
                    "attestor_progress",
                    "attestation_journal",
                ],
                60_000,
            );
        }
        if kernel {
            add(
                "kernel_relay",
                &[
                    "evm_chain",
                    "registered_peer_quorum",
                    "kernel_authority",
                    "relay_progress",
                    "kernel_journal",
                ],
                config.kernel.as_ref().map_or(60_000, |settings| {
                    settings.poll_interval_ms.saturating_mul(3).max(60_000)
                }),
            );
        }
        Self {
            roles: Mutex::new(roles),
            network_id: config.kernel_network_id,
        }
    }

    fn update(&self, role: &str, dependency: &str, usable: bool) {
        if let Ok(mut roles) = self.roles.lock() {
            if let Some(row) = roles.get_mut(role).and_then(|row| {
                row.dependencies
                    .iter_mut()
                    .find(|row| row.dependency == dependency)
            }) {
                row.state = if usable {
                    DependencyState::Ready
                } else {
                    DependencyState::Unavailable
                };
                if usable {
                    row.last_success_unix_ms = Some(now_ms());
                }
            }
        }
    }

    fn snapshot(&self) -> (bool, serde_json::Value) {
        let now = now_ms();
        let Ok(roles) = self.roles.lock() else {
            return (false, serde_json::json!({"error":"readiness_unavailable"}));
        };
        let mut roles: Vec<_> = roles.values().cloned().collect();
        for role in &mut roles {
            for dependency in &mut role.dependencies {
                if dependency.state == DependencyState::Ready
                    && !dependency.last_success_unix_ms.is_some_and(|last| {
                        now >= last && now.saturating_sub(last) <= role.freshness_budget_ms
                    })
                {
                    dependency.state = DependencyState::Stale;
                } else if dependency.state == DependencyState::Starting
                    && now > role.first_use_deadline_unix_ms
                {
                    dependency.state = DependencyState::Unavailable;
                }
            }
            role.state = if role
                .dependencies
                .iter()
                .all(|row| row.state == DependencyState::Ready)
            {
                DependencyState::Ready
            } else if role
                .dependencies
                .iter()
                .any(|row| row.state == DependencyState::Unavailable)
            {
                DependencyState::Unavailable
            } else if role
                .dependencies
                .iter()
                .any(|row| row.state == DependencyState::Stale)
            {
                DependencyState::Stale
            } else {
                DependencyState::Starting
            };
        }
        let ready =
            !roles.is_empty() && roles.iter().all(|row| row.state == DependencyState::Ready);
        (
            ready,
            serde_json::json!({"version":1,"ready":ready,"roles":roles,"checked_at_unix_ms":now,"network_id":self.network_id,"protocol_version":x_websearch::payment::PROTOCOL_VERSION}),
        )
    }
}

struct ReadinessProbe {
    config: Config,
    readiness: Arc<Readiness>,
    gate: Arc<PaymentGate>,
    index: Arc<WebIndex>,
    store: Arc<ContentStore>,
    signer: Option<[u8; 20]>,
}

fn directory_usable(path: &std::path::Path) -> bool {
    std::fs::metadata(path).is_ok_and(|row| row.is_dir() && !row.permissions().readonly())
        && std::fs::read_dir(path).is_ok()
        && std::fs::File::open(path)
            .and_then(|file| file.sync_all())
            .is_ok()
}

fn settlement_usable(config: &Config, endpoint: &str) -> bool {
    let Ok(rpc) = GatewayRpc::new(endpoint) else {
        return false;
    };
    let rpc = match config.gateway.authorization_file.as_deref() {
        Some(path) => match rpc.with_authorization_file(path) {
            Ok(rpc) => rpc,
            Err(_) => return false,
        },
        None => return false,
    };
    let Some(RpcAnswer::Result(node)) = rpc.call("lx_getNodeInfo", &serde_json::json!([])) else {
        return false;
    };
    node.get("network_id").and_then(serde_json::Value::as_u64)
        == Some(u64::from(config.kernel_network_id))
        && node
            .get("protocol_version")
            .and_then(serde_json::Value::as_u64)
            == Some(u64::from(x_websearch::payment::PROTOCOL_VERSION))
        && node
            .get("authorised_sequencer_key")
            .and_then(serde_json::Value::as_str)
            == Some(x_websearch::payment::hex(&config.gateway.sequencer.public_key).as_str())
        && layerx_wire::handover::sequencer_id(&config.gateway.sequencer.public_key).ok()
            == Some(config.gateway.sequencer.sequencer_id)
}

fn peer_quorum_usable(config: &Config, set: &attest::AttestorSet, signer: [u8; 20]) -> bool {
    use x_websearch::fetch::{HttpClient, Url};
    if set.threshold == 0 || !set.contains(&signer) || set.threshold as usize > set.signers.len() {
        return false;
    }
    let Ok(client) = HttpClient::new(Duration::from_secs(2)) else {
        return false;
    };
    let deadline = Instant::now() + Duration::from_secs(10);
    let mut reachable = std::collections::BTreeSet::from([signer]);
    for peer in &config.peers {
        if Instant::now() >= deadline {
            break;
        }
        let Ok(mut url) = Url::parse(peer) else {
            continue;
        };
        let nonce = x_websearch::watch::keccak(
            &[
                b"LayerX/xweb/readiness-nonce/v1\0".as_slice(),
                &now_ms().to_be_bytes(),
                &signer,
            ]
            .concat(),
        );
        url.target = format!(
            "{}/health?readiness_nonce={}",
            url.target.trim_end_matches('/'),
            x_websearch::payment::hex(&nonce)
        );
        let Ok(mut addresses) = (url.bare_host(), url.port).to_socket_addrs() else {
            continue;
        };
        let Some(address) = addresses.next() else {
            continue;
        };
        let Ok(response) = client.get(&url, address, deadline, 65_536, &[]) else {
            continue;
        };
        let Ok(value) = serde_json::from_slice::<serde_json::Value>(&response.body) else {
            continue;
        };
        if response.status != 200
            || value
                .get("peer_exchange_usable")
                .and_then(serde_json::Value::as_bool)
                != Some(true)
        {
            continue;
        }
        let Some(peer_signer) = value
            .get("attestor_signer")
            .and_then(serde_json::Value::as_str)
            .and_then(x_websearch::payment::unhex)
            .and_then(|bytes| <[u8; 20]>::try_from(bytes).ok())
        else {
            continue;
        };
        let checked = value["peer_readiness"]["checked_at_unix_ms"].as_u64();
        let signature = value["peer_readiness"]["signature"]
            .as_str()
            .and_then(x_websearch::payment::unhex)
            .and_then(|bytes| <[u8; 65]>::try_from(bytes).ok());
        if let (Some(checked), Some(signature)) = (checked, signature) {
            if set.contains(&peer_signer)
                && value["peer_readiness"]["nonce"] == x_websearch::payment::hex(&nonce)
                && value["peer_readiness"]["network_id"].as_u64()
                    == Some(u64::from(config.kernel_network_id))
                && now_ms() >= checked
                && now_ms().saturating_sub(checked) <= 60_000
                && attest::recover_signer(
                    &peer_readiness_digest(
                        config.kernel_network_id,
                        peer_signer,
                        nonce,
                        true,
                        checked,
                    ),
                    &signature,
                )
                .ok()
                    == Some(peer_signer)
            {
                reachable.insert(peer_signer);
            }
        }
    }
    reachable.len() >= set.threshold as usize
}

fn peer_readiness_digest(
    network: u32,
    signer: [u8; 20],
    nonce: [u8; 32],
    usable: bool,
    checked: u64,
) -> [u8; 32] {
    x_websearch::watch::keccak(
        &[
            b"LayerX/xweb/readiness/v1\0".as_slice(),
            &network.to_be_bytes(),
            &signer,
            &nonce,
            &[u8::from(usable)],
            &checked.to_be_bytes(),
        ]
        .concat(),
    )
}

impl ReadinessProbe {
    fn check(&self) {
        self.readiness.update(
            "paid_delivery",
            "index",
            directory_usable(self.index.directory())
                && self.index.num_docs() > 0
                && search::search(&self.index, "readiness").is_ok(),
        );
        self.readiness.update(
            "paid_delivery",
            "content_storage",
            directory_usable(self.store.directory()),
        );
        self.readiness.update(
            "paid_delivery",
            "payment_journal",
            directory_usable(&self.config.data_dir.join("payments"))
                && self.gate.recover_pending_deliveries().is_ok(),
        );
        self.readiness.update(
            "paid_delivery",
            "settlement_authority",
            settlement_usable(&self.config, &self.config.gateway.endpoint),
        );
        if let Some(signer) = self.signer {
            let rpc = EvmRpc::new(&self.config.evm.endpoint);
            let chain = rpc.as_ref().is_ok_and(|rpc| {
                rpc.quantity("eth_chainId", &serde_json::json!([])).ok()
                    == Some(u128::from(self.config.evm.chain_id))
                    && rpc.block_number().is_ok()
            });
            let quorum = rpc
                .as_ref()
                .ok()
                .and_then(|rpc| submit::attestor_set(rpc).ok())
                .is_some_and(|set| peer_quorum_usable(&self.config, &set, signer));
            for role in ["evm_attestor", "kernel_relay"] {
                self.readiness.update(role, "evm_chain", chain);
                self.readiness
                    .update(role, "registered_peer_quorum", quorum);
            }
            self.readiness.update(
                "evm_attestor",
                "attestation_journal",
                directory_usable(&self.config.data_dir.join("attest"))
                    && directory_usable(&self.config.data_dir.join("watch")),
            );
        }
        if let Some(kernel) = &self.config.kernel {
            self.readiness.update(
                "kernel_relay",
                "kernel_authority",
                settlement_usable(&self.config, &kernel.endpoint),
            );
            self.readiness.update(
                "kernel_relay",
                "kernel_journal",
                directory_usable(&self.config.data_dir.join("kernel"))
                    && directory_usable(&self.config.data_dir.join("kernel-attest")),
            );
        }
    }
}
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
    probe: ReadinessProbe,
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
    let readiness = Arc::new(Readiness::new(config, pipeline.is_some(), relay.is_some()));
    let eligibility = Arc::clone(&readiness);
    routes
        .set_serving_eligibility(move || eligibility.snapshot().0)
        .map_err(|error| format!("readiness eligibility: {error}"))?;
    let ready_view = Arc::clone(&readiness);
    routes
        .set_readiness(move |_| {
            let (ready, body) = ready_view.snapshot();
            x_websearch::server::Response::json(
                if ready { 200 } else { 503 },
                body.to_string().into_bytes(),
            )
        })
        .map_err(|error| format!("readiness route: {error}"))?;
    let mut health = serde_json::json!({
        "status": "ok",
        "live": true,
        "readiness_scope": "process-liveness",
        "upstream_payment_verified": false,
        "network_id": config.kernel_network_id,
        "wire_version": x_websearch::payment::PROTOCOL_VERSION.to_string(),
        "protocol_version": x_websearch::payment::PROTOCOL_VERSION,
        "source_revision": option_env!("PAXEER_X_SOURCE_REVISION"),
        "evm_attestation_enabled": pipeline.is_some(),
        "kernel_attestation_enabled": relay.is_some()
    });
    let attestor_key = keys.attestor().cloned();
    let signer = attestor_key.as_ref().map(attest::signer_address);
    health["attestor_signer"] = signer.map_or(serde_json::Value::Null, |signer| {
        serde_json::json!(x_websearch::payment::hex(&signer))
    });
    let health_view = Arc::clone(&readiness);
    let network = config.kernel_network_id;
    routes
        .set_health(move |request| {
            let mut body = health.clone();
            let (_, view) = health_view.snapshot();
            let usable = view["roles"].as_array().is_some_and(|roles| roles.iter().any(|role| {
                role["role"] == "evm_attestor" && role["dependencies"].as_array().is_some_and(|dependencies| dependencies.iter()
                    .filter(|row| row["dependency"] != "registered_peer_quorum").all(|row| row["state"] == "ready"))
            }));
            body["peer_exchange_usable"] = serde_json::json!(usable);
            if let (Some(key), Some(signer), Ok(Some(nonce))) = (&attestor_key, signer, request.query_param("readiness_nonce")) {
                if let Some(nonce) = x_websearch::payment::unhex(&nonce).and_then(|bytes| <[u8;32]>::try_from(bytes).ok()) {
                    let checked = now_ms();
                    let digest = peer_readiness_digest(network, signer, nonce, usable, checked);
                    if let Ok(signature) = attest::sign_digest(key, &digest) {
                        body["peer_readiness"] = serde_json::json!({"nonce":x_websearch::payment::hex(&nonce),"checked_at_unix_ms":checked,"network_id":network,"signature":x_websearch::payment::hex(&signature)});
                    }
                }
            }
            x_websearch::server::Response::json(200, body.to_string().into_bytes())
        })
        .map_err(|error| format!("health route: {error}"))?;
    Ok(Assembled {
        routes,
        crawler,
        index: Arc::clone(&index),
        pipeline,
        relay,
        probe: ReadinessProbe {
            config: config.clone(),
            readiness,
            gate,
            index: Arc::clone(&index),
            store,
            signer,
        },
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
    let authorization_file = config
        .gateway
        .authorization_file
        .as_deref()
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
fn attest_round(pipeline: &mut Pipeline) -> bool {
    let watched = match pipeline.watcher.poll() {
        Ok(_) => true,
        Err(error) => {
            eprintln!("x-websearch request watch failed: {error}");
            false
        }
    };
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
        match submit::request_status_at_depth(
            &pipeline.rpc,
            request_id,
            pipeline.watcher.confirmations(),
        ) {
            Ok(status) if status != submit::STATUS_PENDING => {
                retire_closed(pipeline, request_id);
                continue;
            }
            Ok(_) => {}
            Err(error) => {
                eprintln!("x-websearch request {request_id} final status: {error}");
                continue;
            }
        }
        if let Some(submitter) = &pipeline.submitter {
            match submitter.journal().load(request_id) {
                Ok(Some(row)) => {
                    if row.state == submit::JournalState::Signed {
                        if let Err(error) =
                            pipeline.watcher.transition(request_id, WorkStage::Signed)
                        {
                            eprintln!("x-websearch request {request_id}: {error}");
                            continue;
                        }
                    }
                    match submitter.resume_request(request_id) {
                        Ok(Some(outcome)) if outcome.completed() => {
                            retire_closed(pipeline, request_id);
                            continue;
                        }
                        Ok(Some(Outcome::Reverted)) => {}
                        Ok(_) => continue,
                        Err(error) => {
                            let _ = pipeline.watcher.fail(request_id, false, &error.to_string());
                            eprintln!("x-websearch restored submission {request_id}: {error}");
                            continue;
                        }
                    }
                    if row.state != submit::JournalState::Signed {
                        continue;
                    }
                    if submitter
                        .journal()
                        .load(request_id)
                        .ok()
                        .flatten()
                        .is_some()
                    {
                        continue;
                    }
                }
                Ok(None) => {}
                Err(error) => {
                    eprintln!("x-websearch submission journal {request_id}: {error}");
                    continue;
                }
            }
        }
        if let Some(answer) = pipeline.exchange.answer(request_id) {
            if !pipeline.attestor.binds(&request, &answer) {
                if pipeline
                    .watcher
                    .fail(request_id, true, "retained attestation request binding")
                    .is_ok()
                {
                    pipeline.exchange.forget(request_id);
                }
            }
            continue;
        }
        if let Err(error) = pipeline
            .watcher
            .transition(request_id, WorkStage::Attesting)
        {
            eprintln!("x-websearch request {request_id}: {error}");
            continue;
        }
        match pipeline.attestor.attest(&request) {
            Ok(answer) => {
                if pipeline.exchange.record(answer) {
                    if let Err(error) = pipeline.watcher.transition(request_id, WorkStage::Attested)
                    {
                        eprintln!("x-websearch request {request_id}: {error}");
                    }
                } else if let Err(error) =
                    pipeline
                        .watcher
                        .fail(request_id, false, "attestation persistence failed")
                {
                    eprintln!("x-websearch request {request_id}: {error}");
                }
            }
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
            return false;
        }
    };
    let journal = pipeline.watcher.journal();
    for request_id in pipeline.exchange.pending() {
        let Some(entry) = journal
            .iter()
            .find(|entry| entry.request.request_id == request_id)
        else {
            pipeline.exchange.forget(request_id);
            continue;
        };
        if !pipeline.watcher.eligible(request_id) || !still_canonical(pipeline, &entry.request) {
            continue;
        }
        if let Some(submitter) = &pipeline.submitter {
            let binding = pipeline
                .watcher
                .journal()
                .into_iter()
                .find(|row| row.request.request_id == request_id);
            let Some(binding) = binding else {
                continue;
            };
            let Some(source) = &binding.source else {
                continue;
            };
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
            None => submit::request_status_at_depth(
                &pipeline.rpc,
                request_id,
                pipeline.watcher.confirmations(),
            )
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
                let signed = pipeline.submitter.as_ref().is_some_and(|submitter| {
                    submitter
                        .journal()
                        .load(request_id)
                        .ok()
                        .flatten()
                        .is_some_and(|row| row.state == submit::JournalState::Signed)
                });
                if signed {
                    let _ = pipeline.watcher.transition(request_id, WorkStage::Signed);
                } else if pipeline.exchange.ready(request_id, &set).is_none() {
                    let _ = pipeline
                        .watcher
                        .fail(request_id, false, "peer quorum unavailable");
                }
            }
            Err(error) => {
                if pipeline.submitter.as_ref().is_some_and(|submitter| {
                    submitter
                        .journal()
                        .load(request_id)
                        .ok()
                        .flatten()
                        .is_some_and(|row| row.state == submit::JournalState::Signed)
                }) {
                    let _ = pipeline.watcher.transition(request_id, WorkStage::Signed);
                }
                let _ = pipeline.watcher.fail(request_id, false, &error);
                eprintln!("x-websearch request {request_id} with {held} signatures: {error}");
            }
        }
    }
    watched
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
    let Some(entry) = pipeline
        .watcher
        .journal()
        .into_iter()
        .find(|entry| entry.request.request_id == request_id)
    else {
        return;
    };
    if !still_canonical(pipeline, &entry.request) {
        return;
    }
    if let Some(submitter) = &pipeline.submitter {
        let Some(binding) = pipeline
            .watcher
            .journal()
            .into_iter()
            .find(|entry| entry.request.request_id == request_id)
        else {
            return;
        };
        let Some(source) = &binding.source else {
            return;
        };
        if let Err(error) = submitter.bind_source(&binding.request, source) {
            eprintln!("x-websearch request {request_id}: {error}");
            return;
        }
    }
    match submit::request_status_at_depth(
        &pipeline.rpc,
        request_id,
        pipeline.watcher.confirmations(),
    ) {
        Ok(status) if status != submit::STATUS_PENDING => {
            if let Some(submitter) = &pipeline.submitter {
                match submitter.acknowledge_closed(request_id) {
                    Ok(true) => {}
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
fn start_attestor(
    mut pipeline: Pipeline,
    stop: StopSignal,
    readiness: Arc<Readiness>,
) -> std::io::Result<JoinHandle<()>> {
    thread::Builder::new()
        .name("x-websearch-attestor".to_owned())
        .spawn(move || {
            while !stop.is_raised() {
                let started = Instant::now();
                let usable = attest_round(&mut pipeline);
                readiness.update("evm_attestor", "attestor_progress", usable);
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
fn relay_round(relay: &mut RelayLoop) -> bool {
    let set_usable = match submit::attestor_set(&relay.rpc) {
        Ok(set) => {
            relay.relay.set = set;
            true
        }
        Err(error) => {
            relay.relay.set = attest::AttestorSet::default();
            eprintln!("x-websearch could not read the attestor set: {error}");
            false
        }
    };
    let progressed = match relay.relay.step(now_ms()) {
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
            true
        }
        Err(error) => {
            eprintln!("x-websearch kernel relay step failed: {error}");
            false
        }
    };
    set_usable && progressed
}

/// Runs a relay step every interval on its own thread until `stop` is
/// raised. The journalled requests and submissions were loaded when the
/// relay opened, before this loop takes any new work.
fn start_relay(
    mut relay: RelayLoop,
    stop: StopSignal,
    readiness: Arc<Readiness>,
) -> std::io::Result<JoinHandle<()>> {
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
                let usable = relay_round(&mut relay);
                readiness.update("kernel_relay", "relay_progress", usable);
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
        probe,
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
        probe,
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
    probe: ReadinessProbe,
    stop: &StopSignal,
) -> (Vec<JoinHandle<()>>, Result<(), String>) {
    let mut loops = Vec::with_capacity(4);
    let readiness = Arc::clone(&probe.readiness);
    let probe_stop = stop.clone();
    match thread::Builder::new()
        .name("x-websearch-readiness".to_owned())
        .spawn(move || {
            while !probe_stop.is_raised() {
                probe.check();
                if probe_stop.wait_timeout(Duration::from_secs(5)) {
                    break;
                }
            }
        }) {
        Ok(handle) => loops.push(handle),
        Err(error) => return (loops, Err(format!("readiness thread: {error}"))),
    }
    match start_crawler(crawler, seeds, interval, stop.clone()) {
        Ok(handle) => loops.push(handle),
        Err(error) => return (loops, Err(format!("crawler thread: {error}"))),
    }
    if let Some(pipeline) = pipeline {
        match start_attestor(pipeline, stop.clone(), Arc::clone(&readiness)) {
            Ok(handle) => loops.push(handle),
            Err(error) => return (loops, Err(format!("attestor thread: {error}"))),
        }
    }
    if let Some(relay) = relay {
        match start_relay(relay, stop.clone(), Arc::clone(&readiness)) {
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
