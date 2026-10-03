#![forbid(unsafe_code)]

use std::collections::{BTreeMap, BTreeSet};
use std::env;
use std::fs;
use std::io::{Read, Write};
use std::net::{TcpListener, TcpStream};
use std::path::{Path, PathBuf};
use std::sync::{mpsc, Arc, Mutex};
use std::thread;
use std::time::{Duration, Instant, SystemTime, UNIX_EPOCH};

use layerx_agent_api::error::ErrorClass;
use layerx_agentd::agent_rpc::{self, AgentRpcResponse};
use layerx_agentd::agent_rpc_tls::{AgentRpcTls, AgentRpcTlsPaths};
use layerx_agentd::audit::Redacted;
use layerx_agentd::budget::{LimitConfig, LimitId, LimitScope};
use layerx_agentd::capability::CapabilityId;
use layerx_agentd::enrolment::{
    self, BindingMode, BindingPublisher, DaemonSurface, EnrolmentRequest, ListenerDeclaration,
    WebDeclaration,
};
use layerx_agentd::human::{HumanListenerConfig, HumanPeer, HumanUnixServer};
use layerx_agentd::human_runtime::{
    HumanAuthorityBoundary, ProductionHumanOperations, RemoteHumanAuthority, SharedAgentOwner,
    UnifiedAgentOwner,
};
use layerx_agentd::identity::{self, CoreIdentity, IdentityError, IdentityResolver};
use layerx_agentd::ops::program::ProgramOperations;
use layerx_agentd::read::{
    LayerxdProgramBalanceReader, NativeReadRoute, ProgramAuthority, ProgramBalanceRead,
    ProgramBalanceReadRoute,
};
use layerx_agentd::session::{SessionId, SessionRegistry};
use layerx_agentd::session_keys::SessionKeyRegistry;
use layerx_agentd::store::{Store, TenantId};
use layerx_client::client::{ClientConfig, ReconnectPolicy};
use layerx_client::lni::handshake::HandshakeConfig;
use layerx_client::lni::schema::Version;
use layerx_client::lni::transport::Limits;
use layerx_client::Client;
use layerx_programs::{
    hex, DeploymentProof, DeploymentRecord, ProgramId, ProgramLifecycle,
    ProtocolDeploymentVerifier, Registry,
};
use layerx_types::ids::Did;

mod human_owner_mode;
mod human_peer_config;

const HEADER_LIMIT: usize = 16 * 1024;
const BODY_LIMIT: usize = 0;
const PROGRAM_WORKERS: usize = 8;
const PROGRAM_QUEUE: usize = 16;
const REQUEST_DEADLINE: Duration = Duration::from_secs(5);
const IO_CEILING: Duration = Duration::from_secs(10);
const SUPERVISION_INTERVAL: Duration = Duration::from_millis(25);

struct DeadlineStream {
    socket: TcpStream,
    expires: Instant,
}

impl DeadlineStream {
    fn new(socket: TcpStream) -> Self {
        Self {
            socket,
            expires: Instant::now() + REQUEST_DEADLINE,
        }
    }

    fn remaining(&self) -> std::io::Result<Duration> {
        let remaining = self.expires.saturating_duration_since(Instant::now());
        if remaining.is_zero() {
            Err(std::io::Error::new(
                std::io::ErrorKind::TimedOut,
                "request deadline elapsed",
            ))
        } else {
            Ok(remaining.min(IO_CEILING))
        }
    }
}

impl Read for DeadlineStream {
    fn read(&mut self, bytes: &mut [u8]) -> std::io::Result<usize> {
        self.socket.set_read_timeout(Some(self.remaining()?))?;
        self.socket.read(bytes)
    }
}

impl Write for DeadlineStream {
    fn write(&mut self, bytes: &[u8]) -> std::io::Result<usize> {
        self.socket.set_write_timeout(Some(self.remaining()?))?;
        self.socket.write(bytes)
    }

    fn flush(&mut self) -> std::io::Result<()> {
        self.socket.set_write_timeout(Some(self.remaining()?))?;
        self.socket.flush()
    }
}

struct HttpAdmissions {
    stopping: bool,
    busy: Vec<bool>,
    pending: Vec<Option<DeadlineStream>>,
    active: Vec<Option<TcpStream>>,
    queue: std::collections::VecDeque<DeadlineStream>,
}

struct HttpPool {
    admissions: Arc<(Mutex<HttpAdmissions>, std::sync::Condvar)>,
    threads: Vec<thread::JoinHandle<()>>,
}

impl HttpPool {
    fn start<F>(listener: TcpListener, handle: F) -> Result<Self, String>
    where
        F: Fn(DeadlineStream) + Send + Sync + 'static,
    {
        listener
            .set_nonblocking(true)
            .map_err(|error| error.to_string())?;
        let handle = Arc::new(handle);
        let mut pool = Self {
            admissions: Arc::new((
                Mutex::new(HttpAdmissions {
                    stopping: false,
                    busy: vec![false; PROGRAM_WORKERS],
                    pending: (0..PROGRAM_WORKERS).map(|_| None).collect(),
                    active: (0..PROGRAM_WORKERS).map(|_| None).collect(),
                    queue: std::collections::VecDeque::with_capacity(PROGRAM_QUEUE),
                }),
                std::sync::Condvar::new(),
            )),
            threads: Vec::with_capacity(PROGRAM_WORKERS + 1),
        };
        for index in 0..PROGRAM_WORKERS {
            let admissions = Arc::clone(&pool.admissions);
            let handle = Arc::clone(&handle);
            let worker = thread::Builder::new()
                .name(format!("layerx-http-{index}"))
                .spawn(move || loop {
                    let stream = {
                        let Ok(mut state) = admissions.0.lock() else {
                            return;
                        };
                        while state.pending[index].is_none() && !state.stopping {
                            state = match admissions.1.wait(state) {
                                Ok(state) => state,
                                Err(_) => return,
                            };
                        }
                        if state.stopping {
                            return;
                        }
                        let Some(stream) = state.pending[index].take() else {
                            return;
                        };
                        match stream.socket.try_clone() {
                            Ok(socket) => state.active[index] = Some(socket),
                            Err(_) => {
                                state.pending[index] = state.queue.pop_front();
                                state.busy[index] = state.pending[index].is_some();
                                continue;
                            }
                        }
                        stream
                    };
                    if stream.remaining().is_ok() {
                        handle(stream);
                    }
                    let Ok(mut state) = admissions.0.lock() else {
                        return;
                    };
                    state.active[index] = None;
                    state.pending[index] = state.queue.pop_front();
                    state.busy[index] = state.pending[index].is_some();
                })
                .map_err(|error| format!("HTTP worker failed: {error}"))?;
            pool.threads.push(worker);
        }
        let admissions = Arc::clone(&pool.admissions);
        pool.threads.push(
            thread::Builder::new()
                .name("layerx-http-accept".to_owned())
                .spawn(move || loop {
                    {
                        let Ok(mut state) = admissions.0.lock() else {
                            return;
                        };
                        if state.stopping {
                            return;
                        }
                        state.queue.retain(|stream| stream.remaining().is_ok());
                    }
                    match listener.accept() {
                        Ok((socket, _)) => {
                            let stream = DeadlineStream::new(socket);
                            let Ok(mut state) = admissions.0.lock() else {
                                return;
                            };
                            if state.stopping {
                                return;
                            }
                            if let Some(index) = state.busy.iter().position(|busy| !busy) {
                                state.busy[index] = true;
                                state.pending[index] = Some(stream);
                                admissions.1.notify_all();
                            } else if state.queue.len() < PROGRAM_QUEUE {
                                state.queue.push_back(stream);
                            }
                        }
                        Err(error) if error.kind() == std::io::ErrorKind::WouldBlock => {
                            thread::sleep(SUPERVISION_INTERVAL)
                        }
                        Err(error) if error.kind() == std::io::ErrorKind::Interrupted => {}
                        Err(_) => return,
                    }
                })
                .map_err(|error| format!("HTTP accept thread failed: {error}"))?,
        );
        Ok(pool)
    }

    fn check(&self) -> Result<(), String> {
        if self.threads.iter().any(thread::JoinHandle::is_finished) {
            Err("HTTP listener or worker terminated".to_owned())
        } else {
            Ok(())
        }
    }
}

impl Drop for HttpPool {
    fn drop(&mut self) {
        if let Ok(mut state) = self.admissions.0.lock() {
            state.stopping = true;
            state.queue.clear();
            for stream in &mut state.pending {
                *stream = None;
            }
            for socket in state.active.iter().flatten() {
                let _ = socket.shutdown(std::net::Shutdown::Both);
            }
            self.admissions.1.notify_all();
        }
    }
}

struct Config {
    listen: String,
    bearer: String,
    node_endpoint: String,
    node_bearer: String,
    authority_endpoint: String,
    authority_bearer: String,
    authority_ca_der: Vec<u8>,
    authority_replica_id: [u8; 32],
    sequencer_trust_history: String,
    staleness_ms: u64,
    deployment_journal: String,
    probe_program: ProgramId,
    policy_sources: BTreeMap<TenantId, PathBuf>,
    native_policy_sources: BTreeMap<TenantId, PathBuf>,
    program_budget_denomination_sources: BTreeMap<TenantId, PathBuf>,
}

fn optional(name: &str) -> Option<String> {
    env::var(name).ok().filter(|value| !value.is_empty())
}

fn required(name: &str) -> Result<String, String> {
    optional(name).ok_or_else(|| format!("{name} is required"))
}

fn absolute_path(name: &str) -> Result<PathBuf, String> {
    let path = PathBuf::from(required(name)?);
    if path.is_absolute() {
        Ok(path)
    } else {
        Err(format!("{name} must be an absolute path"))
    }
}

fn read_ca(name: &str) -> Result<Vec<u8>, String> {
    let bytes = fs::read(required(name)?).map_err(|_| format!("{name} is unreadable"))?;
    if bytes.is_empty() {
        return Err(format!("{name} is empty"));
    }
    Ok(bytes)
}

fn parse_u64(name: &str) -> Result<u64, String> {
    required(name)?
        .parse()
        .map_err(|_| format!("{name} must be an unsigned integer"))
}

fn parse_digest(name: &str) -> Result<[u8; 32], String> {
    hex::decode_digest(&required(name)?).map_err(|error| format!("{name} is invalid: {error}"))
}

fn parse_hex<const N: usize>(name: &str) -> Result<[u8; N], String> {
    let value = required(name)?;
    if value.len() != N * 2 {
        return Err(format!("{name} has the wrong width"));
    }
    let mut bytes = [0; N];
    for (index, byte) in bytes.iter_mut().enumerate() {
        *byte = u8::from_str_radix(&value[index * 2..index * 2 + 2], 16)
            .map_err(|_| format!("{name} is not hexadecimal"))?;
    }
    Ok(bytes)
}

fn human_peers() -> Result<BTreeMap<u32, (String, String)>, String> {
    human_peer_config::parse(&required("LAYERX_AGENT_HUMAN_PEERS")?)
        .map_err(|error| error.to_string())
}

fn verified_limit() -> Result<LimitConfig, String> {
    let scope_bytes = parse_hex("LAYERX_AGENT_HUMAN_LIMIT_SCOPE_ID")?;
    let scope = match required("LAYERX_AGENT_HUMAN_LIMIT_SCOPE")?.as_str() {
        "tenant" => LimitScope::Tenant(scope_bytes),
        "agent" => LimitScope::Agent(scope_bytes),
        "session" => LimitScope::Session(scope_bytes),
        "capability" => LimitScope::Capability(scope_bytes),
        "counterparty" => LimitScope::Counterparty(scope_bytes),
        _ => return Err("human limit scope is invalid".to_owned()),
    };
    Ok(LimitConfig {
        id: LimitId(parse_hex("LAYERX_AGENT_HUMAN_LIMIT_ID")?),
        name: required("LAYERX_AGENT_HUMAN_LIMIT_NAME")?,
        scope,
        ceiling: required("LAYERX_AGENT_HUMAN_LIMIT_CEILING")?
            .parse()
            .map_err(|_| "human limit ceiling is invalid")?,
        consumed: required("LAYERX_AGENT_HUMAN_LIMIT_CONSUMED")?
            .parse()
            .map_err(|_| "human limit consumed is invalid")?,
    })
}

struct McpEnrolment {
    root: PathBuf,
    audit_root: PathBuf,
    peer_uid: u32,
    did: Did,
    request: EnrolmentRequest,
    limit: LimitConfig,
    deadline: Duration,
    mode: BindingMode,
    listener: ListenerDeclaration,
    web: Option<WebDeclaration>,
}

struct McpBoot {
    surface: DaemonSurface,
    enrolment: McpEnrolment,
}

struct ResolvedIdentity {
    did: Did,
    observation: CoreIdentity,
}

impl IdentityResolver for ResolvedIdentity {
    fn resolve(&mut self, did: &Did) -> Result<Option<CoreIdentity>, IdentityError> {
        if did == &self.did {
            Ok(Some(self.observation.clone()))
        } else {
            Ok(None)
        }
    }
}

fn mcp_activity_types() -> Result<BTreeSet<u16>, String> {
    let mut values = BTreeSet::new();
    for entry in required("LAYERX_AGENT_MCP_ACTIVITY_TYPES")?.split(',') {
        let value = entry
            .trim()
            .parse()
            .map_err(|_| "LAYERX_AGENT_MCP_ACTIVITY_TYPES lists an invalid activity type")?;
        if !values.insert(value) {
            return Err("LAYERX_AGENT_MCP_ACTIVITY_TYPES repeats an activity type".to_owned());
        }
    }
    Ok(values)
}

fn mcp_scopes() -> Result<BTreeSet<String>, String> {
    let mut values = BTreeSet::new();
    for entry in required("LAYERX_AGENT_MCP_SCOPES")?.split(',') {
        let value = entry.trim();
        if value.is_empty() {
            return Err("LAYERX_AGENT_MCP_SCOPES lists an empty scope".to_owned());
        }
        if !values.insert(value.to_owned()) {
            return Err("LAYERX_AGENT_MCP_SCOPES repeats a scope".to_owned());
        }
    }
    Ok(values)
}

fn mcp_limit() -> Result<LimitConfig, String> {
    let scope_bytes = parse_hex("LAYERX_AGENT_MCP_LIMIT_SCOPE_ID")?;
    let scope = match required("LAYERX_AGENT_MCP_LIMIT_SCOPE")?.as_str() {
        "tenant" => LimitScope::Tenant(scope_bytes),
        "agent" => LimitScope::Agent(scope_bytes),
        "session" => LimitScope::Session(scope_bytes),
        "capability" => LimitScope::Capability(scope_bytes),
        "counterparty" => LimitScope::Counterparty(scope_bytes),
        _ => return Err("LAYERX_AGENT_MCP_LIMIT_SCOPE is invalid".to_owned()),
    };
    Ok(LimitConfig {
        id: LimitId(parse_hex("LAYERX_AGENT_MCP_LIMIT_ID")?),
        name: required("LAYERX_AGENT_MCP_LIMIT_NAME")?,
        scope,
        ceiling: required("LAYERX_AGENT_MCP_LIMIT_CEILING")?
            .parse()
            .map_err(|_| "LAYERX_AGENT_MCP_LIMIT_CEILING is invalid")?,
        consumed: required("LAYERX_AGENT_MCP_LIMIT_CONSUMED")?
            .parse()
            .map_err(|_| "LAYERX_AGENT_MCP_LIMIT_CONSUMED is invalid")?,
    })
}

fn mcp_admitted_uids() -> Result<Vec<u32>, String> {
    let mut values = Vec::new();
    for entry in required("LAYERX_AGENT_MCP_LISTENER_ADMITTED_UIDS")?.split(',') {
        let value = entry
            .trim()
            .parse()
            .map_err(|_| "LAYERX_AGENT_MCP_LISTENER_ADMITTED_UIDS lists an invalid uid")?;
        if values.contains(&value) {
            return Err("LAYERX_AGENT_MCP_LISTENER_ADMITTED_UIDS repeats a uid".to_owned());
        }
        values.push(value);
    }
    Ok(values)
}

/// Reads the protected protocol socket the published binding declares. A binding is never
/// published without one, and the enrolled peer must be one of the admitted peers.
fn mcp_listener(peer_uid: u32) -> Result<ListenerDeclaration, String> {
    let admitted_uids = mcp_admitted_uids()?;
    if !admitted_uids.contains(&peer_uid) {
        return Err(
            "LAYERX_AGENT_MCP_LISTENER_ADMITTED_UIDS does not admit LAYERX_AGENT_MCP_PEER_UID"
                .to_owned(),
        );
    }
    ListenerDeclaration::new(
        absolute_path("LAYERX_AGENT_MCP_LISTENER_SOCKET")?,
        required("LAYERX_AGENT_MCP_LISTENER_OWNER_UID")?
            .parse()
            .map_err(|_| "LAYERX_AGENT_MCP_LISTENER_OWNER_UID is invalid")?,
        required("LAYERX_AGENT_MCP_LISTENER_OWNER_GID")?
            .parse()
            .map_err(|_| "LAYERX_AGENT_MCP_LISTENER_OWNER_GID is invalid")?,
        u32::from_str_radix(&required("LAYERX_AGENT_MCP_LISTENER_MODE")?, 8)
            .map_err(|_| "LAYERX_AGENT_MCP_LISTENER_MODE is not an octal mode")?,
        admitted_uids,
    )
    .map_err(|error| format!("the MCP listener is invalid: {error}"))
}

/// Reads the web sidecar the published binding references, when `LAYERX_AGENT_MCP_WEB_ENDPOINT`
/// enables it; every other web key is then required.
fn mcp_web() -> Result<Option<WebDeclaration>, String> {
    let Some(endpoint) = optional("LAYERX_AGENT_MCP_WEB_ENDPOINT") else {
        return Ok(None);
    };
    WebDeclaration::new(
        endpoint,
        required("LAYERX_AGENT_MCP_WEB_NETWORK")?,
        parse_digest("LAYERX_AGENT_MCP_WEB_SEQUENCER_PUBLIC_KEY")?,
        parse_u64("LAYERX_AGENT_MCP_WEB_TIMEOUT_MS")?,
        required("LAYERX_AGENT_MCP_WEB_PENDING_ATTEMPTS")?
            .parse()
            .map_err(|_| "LAYERX_AGENT_MCP_WEB_PENDING_ATTEMPTS is invalid")?,
        required("LAYERX_AGENT_MCP_WEB_APPROVAL_THRESHOLD")?
            .parse()
            .map_err(|_| "LAYERX_AGENT_MCP_WEB_APPROVAL_THRESHOLD is invalid")?,
    )
    .map(Some)
    .map_err(|error| format!("the MCP web sidecar is invalid: {error}"))
}

/// Reads the model context protocol enrolment this daemon publishes at boot, when one is
/// configured. `LAYERX_AGENT_MCP_BINDING_ROOT` selects the binding directory and every other
/// key is then required.
fn mcp_enrolment() -> Result<Option<McpEnrolment>, String> {
    if optional("LAYERX_AGENT_MCP_BINDING_ROOT").is_none() {
        return Ok(None);
    }
    let mode = match required("LAYERX_AGENT_MCP_MODE")?.as_str() {
        "full" => BindingMode::Full,
        "read-only" => BindingMode::ReadOnly,
        _ => return Err("LAYERX_AGENT_MCP_MODE is invalid".to_owned()),
    };
    let peer_uid = required("LAYERX_AGENT_MCP_PEER_UID")?
        .parse()
        .map_err(|_| "LAYERX_AGENT_MCP_PEER_UID is invalid")?;
    Ok(Some(McpEnrolment {
        root: absolute_path("LAYERX_AGENT_MCP_BINDING_ROOT")?,
        audit_root: absolute_path("LAYERX_AGENT_MCP_AUDIT_ROOT")?,
        peer_uid,
        did: Did::new(required("LAYERX_AGENT_MCP_AGENT_DID")?.as_bytes())
            .map_err(|_| "LAYERX_AGENT_MCP_AGENT_DID is invalid")?,
        request: EnrolmentRequest {
            session_id: SessionId(parse_digest("LAYERX_AGENT_MCP_SESSION_ID")?),
            capability_id: CapabilityId(parse_digest("LAYERX_AGENT_MCP_CAPABILITY_ID")?),
            permitted_activity_types: mcp_activity_types()?,
            scopes: mcp_scopes()?,
            expiry_sequence: parse_u64("LAYERX_AGENT_MCP_EXPIRY_SEQUENCE")?,
            opening_client: required("LAYERX_AGENT_MCP_OPENING_CLIENT")?,
            policy_version: required("LAYERX_AGENT_MCP_POLICY_VERSION")?,
            core_sequence: parse_u64("LAYERX_AGENT_MCP_CORE_SEQUENCE")?,
        },
        limit: mcp_limit()?,
        deadline: Duration::from_millis(parse_u64("LAYERX_AGENT_MCP_DEADLINE_MS")?),
        mode,
        listener: mcp_listener(peer_uid)?,
        web: mcp_web()?,
    }))
}

fn mcp_boot(config: &Config, enrolment: McpEnrolment) -> Result<McpBoot, String> {
    let surface = DaemonSurface::new(
        &config.listen,
        config.bearer.clone(),
        config.probe_program.bytes(),
    )
    .map_err(|error| format!("the MCP daemon surface is invalid: {error}"))?;
    Ok(McpBoot { surface, enrolment })
}

/// Publishes the binding document one model context protocol session reads.
///
/// The agent identity is resolved through the configured human authority, registered against
/// the daemon store, and enrolled into a capability-grant session whose token reaches disk only
/// inside the operator-protected files beside the document. A daemon that already opened the
/// configured session keeps the document it published rather than opening a second session.
fn publish_mcp_binding(
    authority: &mut RemoteHumanAuthority,
    peers: &BTreeMap<u32, (String, String)>,
    shared_store: &Arc<Mutex<Store>>,
    store_path: &Path,
    boot: McpBoot,
) -> Result<(EnrolmentRequest, Did, HumanPeer), String> {
    let McpBoot {
        surface,
        enrolment: configured,
    } = boot;
    let (principal, tenant) = peers
        .get(&configured.peer_uid)
        .ok_or("LAYERX_AGENT_MCP_PEER_UID names no configured human peer")?;
    let peer = HumanPeer {
        subject: None,
        uid: configured.peer_uid,
        principal: principal.clone(),
        tenant: tenant.clone(),
    };
    let tenant_id = TenantId::new(tenant.clone())
        .map_err(|error| format!("the MCP peer tenant is invalid: {error:?}"))?;
    let observation = authority
        .core_identity(&peer, &configured.did)
        .map_err(|error| format!("the MCP agent identity is unverified: {error:?}"))?;
    let capability_id = configured.request.capability_id;
    let verified = if layerx_agentd::human_runtime::has_completed_capability_install(
        shared_store,
        &tenant_id,
        capability_id,
    )
    .map_err(|error| format!("the MCP capability install record is unreadable: {error:?}"))?
    {
        Some(
            authority
                .verified_enrolment_expiry(shared_store, &peer, &configured.did, capability_id)
                .map_err(|error| {
                    format!("the MCP capability grant expiry is unverified: {error:?}")
                })?,
        )
    } else {
        None
    };
    let publisher = BindingPublisher::new(
        configured.root,
        store_path.to_path_buf(),
        configured.audit_root,
        surface,
        configured.limit,
        configured.deadline,
        configured.mode,
    )
    .map_err(|error| format!("the MCP binding publisher is invalid: {error}"))?
    .with_listener(configured.listener);
    let publisher = match configured.web {
        Some(web) => publisher.with_web(web),
        None => publisher,
    };
    let mut resolver = ResolvedIdentity {
        did: configured.did.clone(),
        observation,
    };
    let mut store = shared_store
        .lock()
        .map_err(|_| "the agent store is unavailable".to_owned())?;
    let identity = identity::register(&mut store, tenant_id.clone(), configured.did, &mut resolver)
        .map_err(|error| format!("the MCP agent identity is unusable: {error:?}"))?;
    let mut sessions = SessionRegistry::default();
    sessions
        .restore_tenant(&store, &tenant_id)
        .map_err(|error| format!("the agent sessions are unrestorable: {error:?}"))?;
    let request = configured.request;
    let republish = (request.clone(), identity.did().clone(), peer);
    let session_id = request.session_id;
    if let Some(existing) = sessions.get(&tenant_id, session_id) {
        let advertised = existing.request.expiry_seconds.is_some();
        publisher
            .validate_existing(&sessions, &identity, &request)
            .map_err(|error| format!("the published MCP binding is unusable: {error}"))?;
        if let (Some(expiry), false) = (verified, advertised) {
            enrolment::republish_with_verified_expiry(
                &mut store,
                &mut sessions,
                &tenant_id,
                identity.did(),
                enrolment::VerifiedExpiryEnrolment::new(request, expiry),
            )
            .map_err(|error| format!("MCP expiry republication failed: {error}"))?;
        }
        return Ok(republish);
    }
    match verified {
        Some(expiry) => enrolment::enrol_with_verified_expiry(
            &mut store,
            &mut sessions,
            &identity,
            enrolment::VerifiedExpiryEnrolment::new(request, expiry),
            &publisher,
        ),
        None => enrolment::enrol(&mut store, &mut sessions, &identity, request, &publisher),
    }
    .map_err(|error| format!("MCP enrolment failed: {error}"))?;
    Ok(republish)
}

fn human_lni_limits(deadline: Duration) -> Result<Limits, String> {
    Limits {
        maximum_frame_bytes: required("LAYERX_AGENT_HUMAN_MAX_FRAME_BYTES")?
            .parse()
            .map_err(|_| "human max frame is invalid")?,
        maximum_connections: required("LAYERX_AGENT_HUMAN_MAX_CONNECTIONS")?
            .parse()
            .map_err(|_| "human max connections is invalid")?,
        maximum_streams: required("LAYERX_AGENT_HUMAN_MAX_STREAMS")?
            .parse()
            .map_err(|_| "human max streams is invalid")?,
        maximum_queued_bytes: required("LAYERX_AGENT_HUMAN_MAX_QUEUED_BYTES")?
            .parse()
            .map_err(|_| "human max queue is invalid")?,
        deadline,
    }
    .validate()
    .map_err(|_| "human LNI limits are invalid".to_owned())
}

fn connect_human_node(node_path: PathBuf, node_limits: Limits) -> Result<Client, String> {
    let execution_prestate = match env::var("LAYERX_AGENT_EXECUTION_PRESTATE") {
        Err(env::VarError::NotPresent) => false,
        Ok(value) if value == "0" => false,
        Ok(value) if value == "1" => true,
        _ => return Err("LAYERX_AGENT_EXECUTION_PRESTATE must be 0 or 1".to_owned()),
    };
    let human_protocol_version = required("LAYERX_AGENT_HUMAN_PROTOCOL_VERSION")?
        .parse()
        .map_err(|_| "human protocol version is invalid")?;
    if !layerx_wire::limits::protocol_version_uses_occupancy(human_protocol_version) {
        return Err("human protocol version is not the current beta protocol".to_owned());
    }
    let node = Client::connect(ClientConfig {
        endpoint: node_path,
        handshake: HandshakeConfig {
            built_interface_version: if execution_prestate {
                Version::V1_9
            } else {
                Version::V1_3
            },
            expected_protocol_version: human_protocol_version,
            expected_network_id: required("LAYERX_AGENT_HUMAN_NETWORK_ID")?
                .parse()
                .map_err(|_| "human network id is invalid")?,
        },
        limits: node_limits,
        reconnect: ReconnectPolicy {
            maximum_attempts: required("LAYERX_AGENT_HUMAN_RECONNECT_ATTEMPTS")?
                .parse()
                .map_err(|_| "human reconnect attempts are invalid")?,
            base_delay: Duration::from_millis(parse_u64("LAYERX_AGENT_HUMAN_RECONNECT_BASE_MS")?),
            maximum_delay: Duration::from_millis(parse_u64("LAYERX_AGENT_HUMAN_RECONNECT_MAX_MS")?),
            jitter_percent: required("LAYERX_AGENT_HUMAN_RECONNECT_JITTER_PERCENT")?
                .parse()
                .map_err(|_| "human reconnect jitter is invalid")?,
        },
    })
    .map_err(|error| format!("human node LNI is unavailable: {error:?}"))?;
    if execution_prestate {
        let handshake = node.handshake();
        let version = handshake.node().interface_version;
        if version.major != 1
            || version.minor < 9
            || !handshake
                .capabilities()
                .contains(layerx_client::lni::schema::Capability::CapsDiscovery)
            || !handshake
                .capabilities()
                .contains(layerx_client::lni::schema::Capability::ExecutionPrestate)
        {
            return Err("LAYERX_AGENT_EXECUTION_PRESTATE requires negotiated LNI1.9 and execution_prestate capability".to_owned());
        }
    }
    Ok(node)
}

fn configured_registry_source(
) -> Result<Option<layerx_agentd::registry_source::RegistrySourceProvider>, String> {
    let path = match env::var_os("LAYERX_AGENT_REGISTRY_SOURCE_CONFIG") {
        None => return Ok(None),
        Some(path) => PathBuf::from(path),
    };
    if !path.is_absolute() {
        return Err(
            "LAYERX_AGENT_REGISTRY_SOURCE_CONFIG must be an absolute protected file".to_owned(),
        );
    }
    layerx_agentd::registry_source::RegistrySourceProvider::from_protected_config(&path)
        .map(Some)
        .map_err(|error| format!("registry source provider configuration refused: {error:?}"))
}

fn connect_human_authority(
    deadline: Duration,
    peers: &BTreeMap<u32, (String, String)>,
) -> Result<RemoteHumanAuthority, String> {
    let authority = RemoteHumanAuthority::connect(
        &required("LAYERX_AGENT_HUMAN_AUTHORITY_ENDPOINT")?,
        required("LAYERX_AGENT_HUMAN_AUTHORITY_BEARER")?,
        deadline,
        required("LAYERX_AGENT_HUMAN_AUTHORITY_MAX_BYTES")?
            .parse()
            .map_err(|_| "human authority bound is invalid")?,
        &read_ca("LAYERX_AGENT_HUMAN_AUTHORITY_CA_DER")?,
    )
    .map_err(|error| format!("human authority is invalid: {error:?}"))?;
    for (uid, (principal, tenant)) in peers {
        authority
            .registry(&HumanPeer {
                subject: None,
                uid: *uid,
                principal: principal.clone(),
                tenant: tenant.clone(),
            })
            .map_err(|error| format!("human authority readiness failed: {error:?}"))?;
    }
    Ok(authority)
}

/// Selects export trust only from explicit deployment configuration and the
/// genesis-anchored signed authority history; nothing in a request selects it.
fn export_trust_source(
    history: layerx_proof::signed_authority::SignedAuthorityHistory,
) -> Result<layerx_agentd::export::ExportTrustSource, String> {
    let domain = layerx_proof::settlement::declared_domain(&required(
        "LAYERX_AGENT_HUMAN_EXPORT_SETTLEMENT_DOMAIN",
    )?)
    .map_err(|error| format!("export settlement domain is invalid: {error:?}"))?;
    layerx_agentd::export::ExportTrustSource::new(
        history,
        domain,
        parse_u64("LAYERX_AGENT_HUMAN_EXPORT_SET_VERSION")?,
    )
    .map_err(|error| format!("export trust is invalid: {error:?}"))
}

type OwnerStatus = mpsc::Receiver<Result<(), String>>;

fn start_human_owner(mcp: Option<McpBoot>) -> Result<OwnerStatus, String> {
    let runtime_clock = layerx_client::runtime_clock::RuntimeClock::from_environment()
        .map_err(|error| format!("runtime clock unavailable: {error}"))?;
    let tenants = human_policy_tenants(&human_peers()?)?;
    let native_policy_sources = native_policy_sources(&tenants)?;
    let denomination_sources = program_budget_denomination_sources(&tenants)?;
    start_shared_owner(
        mcp,
        None,
        None,
        None,
        &native_policy_sources,
        &denomination_sources,
        runtime_clock,
    )
    .map(|(receiver, _, _)| receiver)
}

fn start_shared_owner(
    mcp: Option<McpBoot>,
    programs: Option<ProgramOperations>,

    export_trust: Option<layerx_agentd::export::ExportTrustSource>,

    policy_sources: Option<&BTreeMap<TenantId, PathBuf>>,
    native_policy_sources: &BTreeMap<TenantId, PathBuf>,
    program_budget_denomination_sources: &BTreeMap<TenantId, PathBuf>,

    clock: Arc<dyn layerx_types::clock::Clock>,
) -> Result<
    (
        OwnerStatus,
        SharedAgentOwner<RemoteHumanAuthority>,
        mpsc::SyncSender<Result<(), String>>,
    ),
    String,
> {
    let peers = human_peers()?;
    let tenants = human_policy_tenants(&peers)?;
    if !native_policy_sources.keys().eq(tenants.iter()) {
        return Err(
            "LAYERX_NATIVE_POLICY_SOURCES must cover exactly the configured Human tenants"
                .to_owned(),
        );
    }
    if program_budget_denomination_sources
        .keys()
        .any(|tenant| !tenants.contains(tenant))
    {
        return Err(
            "Program budget denomination sources name an unconfigured Human tenant".to_owned(),
        );
    }
    let deadline = Duration::from_millis(parse_u64("LAYERX_AGENT_HUMAN_DEADLINE_MS")?);
    let human_limits = human_lni_limits(deadline)?;
    let node_limits = Limits {
        maximum_frame_bytes: human_limits
            .maximum_frame_bytes
            .max(layerx_client::evidence::MINIMUM_FINALITY_FRAME_BYTES),
        ..human_limits
    };
    let node_path = PathBuf::from(required("LAYERX_AGENT_HUMAN_NODE_LNI")?);
    let store_path = PathBuf::from(required("LAYERX_AGENT_HUMAN_STORE")?);
    let socket_path = PathBuf::from(required("LAYERX_AGENT_HUMAN_SOCKET")?);
    let session_key_root = PathBuf::from(required("LAYERX_AGENT_HUMAN_SESSION_KEY_ROOT")?);
    let session_secret_path =
        PathBuf::from(required("LAYERX_AGENT_HUMAN_SESSION_OPERATOR_SECRET_FILE")?);
    if !node_path.is_absolute()
        || !store_path.is_absolute()
        || !socket_path.is_absolute()
        || !session_key_root.is_absolute()
        || !session_secret_path.is_absolute()
    {
        return Err("human daemon paths must be absolute".to_owned());
    }
    let node = connect_human_node(node_path, node_limits)?;
    let mut authority = connect_human_authority(deadline, &peers)?;
    let shared_store =
        Arc::new(Mutex::new(Store::open(&store_path).map_err(|error| {
            format!("human store is unavailable: {error}")
        })?));
    let mut mcp_republish = None;
    if let Some(boot) = mcp {
        mcp_republish = Some(publish_mcp_binding(
            &mut authority,
            &peers,
            &shared_store,
            &store_path,
            boot,
        )?);
    }
    let mut operations = ProductionHumanOperations::new(
        authority,
        node,
        Arc::clone(&shared_store),
        &peers,
        required("LAYERX_AGENT_HUMAN_MAX_PAYLOAD_BYTES")?
            .parse()
            .map_err(|_| "human payload bound is invalid")?,
        parse_u64("LAYERX_AGENT_HUMAN_TIMESTAMP_SPAN")?,
        clock,
    )
    .map_err(|error| format!("human operations are invalid: {error:?}"))?;
    if let Some(source) = export_trust {
        operations.install_export_trust(source, deadline);
    }

    if let Some(sources) = policy_sources {
        operations
            .attach_policies(sources)
            .map_err(|error| format!("tenant policies are invalid: {error:?}"))?;
    }

    operations
        .attach_native_policies(native_policy_sources)
        .map_err(|error| format!("native tenant policies are invalid: {error}"))?;

    operations
        .attach_program_budget_denominations(program_budget_denomination_sources)
        .map_err(|error| format!("Program budget denominations are invalid: {error}"))?;

    let socket_uid = required("LAYERX_AGENT_HUMAN_SOCKET_UID")?
        .parse()
        .map_err(|_| "human socket uid is invalid")?;
    let operator_secret = layerx_agentd::config::read_protected_source(&session_secret_path, 4096)
        .map_err(|error| format!("human session operator secret is unavailable: {error:?}"))?;
    let session_keys = SessionKeyRegistry::open(
        session_key_root,
        operator_secret,
        required("LAYERX_AGENT_HUMAN_NETWORK_ID")?
            .parse()
            .map_err(|_| "human network id is invalid")?,
        socket_uid,
    )
    .map_err(|error| format!("human session key registry is invalid: {error:?}"))?;
    let mut unified = UnifiedAgentOwner::new(
        operations,
        shared_store,
        &peers,
        vec![verified_limit()?],
        session_keys,
    )
    .map_err(|error| format!("human owner is invalid: {error:?}"))?;
    unified.programs = programs;
    if let Some(provider) = configured_registry_source()? {
        unified
            .attach_registry_source(provider)
            .map_err(|error| format!("registry source provider attachment refused: {error:?}"))?;
    }
    unified.mcp_enrolment = mcp_republish;
    let owner = SharedAgentOwner::new(unified);
    let server = HumanUnixServer::bind(
        HumanListenerConfig {
            endpoint: socket_path,
            owner_uid: socket_uid,
            owner_gid: required("LAYERX_AGENT_HUMAN_SOCKET_GID")?
                .parse()
                .map_err(|_| "human socket gid is invalid")?,
            mode: u32::from_str_radix(&required("LAYERX_AGENT_HUMAN_SOCKET_MODE")?, 8)
                .map_err(|_| "human socket mode is invalid")?,
            maximum_frame_bytes: human_limits.maximum_frame_bytes,
            deadline,
            peers,
        },
        owner.clone(),
    )
    .map_err(|error| format!("human listener is invalid: {error:?}"))?;
    let (sender, receiver) = mpsc::sync_channel(2);
    let status = sender.clone();
    thread::Builder::new()
        .name("layerx-agent-human".to_owned())
        .spawn(move || {
            let _ = status.send(
                server
                    .serve()
                    .map_err(|error| format!("human listener stopped: {error:?}")),
            );
        })
        .map_err(|error| format!("human listener thread failed: {error}"))?;
    Ok((receiver, owner, sender))
}

/// Starts the dedicated mutually authenticated agent RPC listener when it is
/// configured. A configured listener without its complete TLS material refuses boot.
fn start_agent_rpc(
    owner: SharedAgentOwner<RemoteHumanAuthority>,
) -> Result<Option<HttpPool>, String> {
    let Some(listen) = optional("LAYERX_AGENTD_RPC_LISTEN") else {
        return Ok(None);
    };
    let owner = owner
        .with_idempotency(
            absolute_path("LAYERX_AGENTD_RPC_IDEMPOTENCY_ROOT")?,
            parse_u64("LAYERX_AGENTD_RPC_IDEMPOTENCY_DAEMON_SEQUENCES")?,
            parse_u64("LAYERX_AGENTD_RPC_IDEMPOTENCY_PROTOCOL_SEQUENCES")?,
        )
        .map_err(|error| format!("agent rpc idempotency is invalid: {error:?}"))?;
    let tls = AgentRpcTls::from_paths(&AgentRpcTlsPaths {
        cert: absolute_path("LAYERX_AGENTD_RPC_TLS_CERT")?,
        key: absolute_path("LAYERX_AGENTD_RPC_TLS_KEY")?,
        client_ca: absolute_path("LAYERX_AGENTD_RPC_TLS_CLIENT_CA")?,
        peer: required("LAYERX_AGENTD_RPC_PEER")?,
    })
    .map_err(|error| format!("agent rpc tls is invalid: {error:?}"))?;
    let network = required("LAYERX_NODE_NETWORK_NAME")?;
    if network.is_empty()
        || network.len() > 64
        || !network
            .bytes()
            .all(|byte| byte.is_ascii_alphanumeric() || matches!(byte, b'-' | b'_'))
    {
        return Err("agent rpc network name is invalid".to_owned());
    }

    let listener = TcpListener::bind(&listen)
        .map_err(|error| format!("agent rpc listener failed: {error}"))?;
    HttpPool::start(listener, move |stream| {
        let Ok(mut stream) = tls.accept(stream) else {
            return;
        };
        let response = agent_rpc_exchange(&mut stream, &network, &owner);
        let _ = write_rpc_response(&mut stream, &response);
        stream.conn.send_close_notify();
        let _ = stream.flush();
    })
    .map(Some)
}

fn agent_rpc_exchange<S: Read>(
    stream: &mut S,
    network: &str,
    owner: &SharedAgentOwner<RemoteHumanAuthority>,
) -> AgentRpcResponse {
    let malformed = || {
        agent_rpc::refusal(
            400,
            ErrorClass::ProtocolIncompatibility,
            "envelope.malformed",
        )
    };
    let mut bytes = vec![0_u8; HEADER_LIMIT];
    let mut length = 0_usize;
    let head_end = loop {
        if let Some(end) = bytes[..length]
            .windows(4)
            .position(|value| value == b"\r\n\r\n")
        {
            break end + 4;
        }
        if length == bytes.len() {
            return agent_rpc::refusal(
                431,
                ErrorClass::ProtocolIncompatibility,
                "envelope.malformed",
            );
        }
        match stream.read(&mut bytes[length..]) {
            Ok(0) | Err(_) => return malformed(),
            Ok(count) => length += count,
        }
    };
    let Ok(head) = std::str::from_utf8(&bytes[..head_end - 4]) else {
        return malformed();
    };
    let mut lines = head.split("\r\n");
    let mut parts = lines.next().unwrap_or_default().split(' ');
    let (method, path, version) = (parts.next(), parts.next(), parts.next());
    if version != Some("HTTP/1.1") || parts.next().is_some() {
        return malformed();
    }
    let health = path == Some("/healthz");
    if path != Some("/rpc") && !health {
        return agent_rpc::refusal(
            404,
            ErrorClass::ProtocolIncompatibility,
            "envelope.malformed",
        );
    }
    if method != Some(if health { "GET" } else { "POST" }) {
        return agent_rpc::refusal(
            405,
            ErrorClass::ProtocolIncompatibility,
            "envelope.malformed",
        );
    }
    let mut content_length = None;
    for line in lines {
        let Some((name, value)) = line.split_once(':') else {
            return malformed();
        };
        let value = value.trim_matches(|c| c == ' ' || c == '\t');
        if name.eq_ignore_ascii_case("transfer-encoding") {
            return malformed();
        }
        if [
            "authorization",
            "layerx-key",
            "layerx-tenant",
            "layerx-agent",
        ]
        .iter()
        .any(|principal| name.eq_ignore_ascii_case(principal))
        {
            return agent_rpc::refusal(403, ErrorClass::PolicyRefusal, "envelope.header_principal");
        }
        if name.eq_ignore_ascii_case("content-length") {
            let canonical = !value.is_empty()
                && value.bytes().all(|byte| byte.is_ascii_digit())
                && (value == "0" || !value.starts_with('0'));
            let parsed = value.parse::<usize>().ok().filter(|_| canonical);
            if content_length.is_some() || parsed.is_none() {
                return malformed();
            }
            content_length = parsed;
        }
    }
    if health {
        if content_length.unwrap_or(0) != 0 || length != head_end {
            return malformed();
        }
        return agent_rpc_health(owner, network);
    }
    let Some(content_length) = content_length else {
        return malformed();
    };
    if content_length > agent_rpc::MAX_BODY_BYTES {
        return agent_rpc::refusal(
            413,
            ErrorClass::ProtocolIncompatibility,
            "envelope.oversized",
        );
    }
    let mut body = bytes[head_end..length].to_vec();
    if body.len() > content_length {
        return malformed();
    }
    let missing = content_length - body.len();
    body.resize(content_length, 0);
    if stream
        .read_exact(&mut body[content_length - missing..])
        .is_err()
    {
        return malformed();
    }
    agent_rpc::handle_rpc(owner, &body)
}

/// Readiness and negotiated node identity for the gateway's binding health check.
fn agent_rpc_health(
    owner: &SharedAgentOwner<RemoteHumanAuthority>,
    network: &str,
) -> AgentRpcResponse {
    let health = owner.lock().and_then(|guard| guard.rpc_health());
    match health {
        Ok((ready, _, protocol_version)) => AgentRpcResponse {
            status: 200,
            body: format!(
                "{{\"ready\":{ready},\"network_id\":\"{network}\",\"wire_version\":\"{protocol_version}\"}}"
            )
            .into_bytes(),
        },
        Err(_) => agent_rpc::refusal(503, ErrorClass::InternalFault, "owner.unavailable"),
    }
}

fn write_rpc_response<S: Write>(stream: &mut S, response: &AgentRpcResponse) -> Result<(), String> {
    let reason = if response.status < 300 {
        "OK"
    } else {
        "Refused"
    };
    let header = format!(
        "HTTP/1.1 {} {reason}\r\nContent-Type: application/json\r\nCache-Control: no-store\r\nContent-Length: {}\r\nConnection: close\r\n\r\n",
        response.status,
        response.body.len()
    );
    stream
        .write_all(header.as_bytes())
        .and_then(|()| stream.write_all(&response.body))
        .map_err(|error| format!("agent rpc response failed: {error}"))
}

fn human_policy_tenants(
    peers: &BTreeMap<u32, (String, String)>,
) -> Result<BTreeSet<TenantId>, String> {
    peers
        .values()
        .map(|(_, tenant)| TenantId::new(tenant.clone()))
        .collect::<Result<BTreeSet<_>, _>>()
        .map_err(|error| format!("human policy tenant is invalid: {error:?}"))
}

fn native_policy_sources(
    tenants: &BTreeSet<TenantId>,
) -> Result<BTreeMap<TenantId, PathBuf>, String> {
    layerx_agentd::config::parse_policy_sources(&required("LAYERX_NATIVE_POLICY_SOURCES")?, tenants)
        .map_err(|error| format!("LAYERX_NATIVE_POLICY_SOURCES is invalid: {error}"))
}

fn program_budget_denomination_sources(
    tenants: &BTreeSet<TenantId>,
) -> Result<BTreeMap<TenantId, PathBuf>, String> {
    const SETTING: &str = "LAYERX_PROGRAM_BUDGET_DENOMINATION_SOURCES";
    let value = match env::var(SETTING) {
        Ok(value) => value,
        Err(env::VarError::NotPresent) => return Ok(BTreeMap::new()),
        Err(env::VarError::NotUnicode(_)) => return Err(format!("{SETTING} is not UTF-8")),
    };
    let mut opted_in = BTreeSet::new();
    for declaration in value.split(',') {
        let (tenant, _) = declaration
            .split_once(':')
            .ok_or_else(|| format!("{SETTING} requires tenant:absolute-path entries"))?;
        let tenant = TenantId::new(tenant.trim().to_owned())
            .map_err(|_| format!("{SETTING} contains an invalid tenant"))?;
        if !tenants.contains(&tenant) || !opted_in.insert(tenant) {
            return Err(format!("{SETTING} contains an unknown or duplicate tenant"));
        }
    }
    layerx_agentd::config::parse_policy_sources(&value, &opted_in)
        .map_err(|error| format!("{SETTING} is invalid: {error}"))
}

fn config() -> Result<Config, String> {
    let listen = required("LAYERX_AGENT_PROGRAM_LISTEN")?;
    let bearer = required("LAYERX_AGENT_PROGRAM_BEARER_TOKEN")?;
    let node_bearer = required("LAYERX_AGENT_NODE_BEARER_TOKEN")?;
    let authority_bearer = required("LAYERX_AGENT_AUTHORITY_BEARER_TOKEN")?;
    if !listen.starts_with("127.0.0.1:")
        || bearer.len() < 32
        || node_bearer.len() < 32
        || authority_bearer.len() < 32
        || bearer == node_bearer
        || bearer == authority_bearer
    {
        return Err(
            "agent program reads require loopback and distinct bounded credentials".to_owned(),
        );
    }
    let staleness_ms = parse_u64("LAYERX_AGENT_PROGRAM_MAX_STALENESS_MS")?;
    if staleness_ms == 0 {
        return Err("agent staleness bound is non-canonical".to_owned());
    }
    let tenants = human_policy_tenants(&human_peers()?)?;
    let policy_sources =
        layerx_agentd::config::parse_policy_sources(&required("LAYERX_POLICY_SOURCES")?, &tenants)
            .map_err(|error| format!("human policy sources are invalid: {error}"))?;
    let native_policy_sources = native_policy_sources(&tenants)?;
    let program_budget_denomination_sources = program_budget_denomination_sources(&tenants)?;
    Ok(Config {
        listen,
        policy_sources,
        native_policy_sources,
        program_budget_denomination_sources,
        bearer,
        node_endpoint: required("LAYERX_AGENT_NODE_ENDPOINT")?,
        node_bearer,
        authority_endpoint: required("LAYERX_AGENT_AUTHORITY_ENDPOINT")?,
        authority_bearer,
        authority_ca_der: read_ca("LAYERX_AGENT_AUTHORITY_CA_DER")?,
        authority_replica_id: parse_digest("LAYERX_AGENT_AUTHORITY_REPLICA_ID")?,
        sequencer_trust_history: required("LAYERX_AGENT_SEQUENCER_TRUST_HISTORY")?,
        staleness_ms,
        deployment_journal: required("LAYERX_AGENT_DEPLOYMENT_JOURNAL")?,
        probe_program: ProgramId::new(parse_digest("LAYERX_AGENT_PROGRAM_PROBE_ID")?)
            .map_err(|error| format!("LAYERX_AGENT_PROGRAM_PROBE_ID is invalid: {error}"))?,
    })
}

fn load_registry(root: &Path, verifier: &ProtocolDeploymentVerifier) -> Result<Registry, String> {
    let mut paths = fs::read_dir(root)
        .map_err(|error| format!("deployment journal is unavailable: {error}"))?
        .map(|entry| entry.map(|value| value.path()))
        .collect::<Result<Vec<_>, _>>()
        .map_err(|error| format!("deployment journal is unreadable: {error}"))?;
    paths.retain(|path| path.extension().is_some_and(|value| value == "admission"));
    paths.sort();
    let mut registry = Registry::new();
    for path in paths {
        let bytes = fs::read(&path)
            .map_err(|error| format!("{} is unreadable: {error}", path.display()))?;
        let proof = DeploymentProof::decode(&bytes)
            .map_err(|error| format!("{} is corrupt: {error}", path.display()))?;
        let evidence = verifier
            .verify_historical_deployment(&proof)
            .map_err(|error| format!("{} is unverified: {error}", path.display()))?;
        let expected = hex::encode(&evidence.receipt_digest());
        if path.file_stem().and_then(|value| value.to_str()) != Some(expected.as_str()) {
            return Err(format!(
                "{} is filed under the wrong receipt",
                path.display()
            ));
        }
        let record_path = root.join(format!("{expected}.deployment"));
        let record = DeploymentRecord::decode(
            &fs::read(&record_path)
                .map_err(|error| format!("{} is unreadable: {error}", record_path.display()))?,
        )
        .map_err(|error| format!("{} is corrupt: {error}", record_path.display()))?;
        record
            .validate()
            .map_err(|error| format!("{} is inadmissible: {error}", record_path.display()))?;
        if &record != evidence.record() {
            return Err(format!(
                "{} disagrees with protocol evidence",
                record_path.display()
            ));
        }
        registry
            .record_verified_deployment(&evidence)
            .map_err(|error| format!("verified deployment replay failed: {error}"))?;
    }
    if registry.program_ids().is_empty() {
        return Err("deployment journal contains no verified admissions".to_owned());
    }
    Ok(registry)
}

fn now_ms() -> Result<u64, String> {
    let value = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map_err(|_| "system time precedes the Unix epoch".to_owned())?;
    u64::try_from(value.as_millis()).map_err(|_| "system time is out of range".to_owned())
}

fn lifecycle(value: ProgramLifecycle) -> &'static str {
    match value {
        ProgramLifecycle::Active => "active",
        ProgramLifecycle::Deprecated => "deprecated",
        ProgramLifecycle::Tombstoned => "tombstoned",
    }
}

fn balance_json(read: &ProgramBalanceRead) -> String {
    let accounts = read
        .accounts
        .iter()
        .map(|account| {
            format!(
                "{{\"account\":\"{}\",\"asset\":\"{}\",\"amount\":\"{}\",\"frozen\":{}}}",
                hex::encode(&account.account),
                hex::encode(&account.asset),
                account.amount,
                account.frozen
            )
        })
        .collect::<Vec<_>>()
        .join(",");
    format!(
        "{{\"program\":\"{}\",\"lifecycle\":\"{}\",\"accounts\":[{}],\"freshness\":{{\"observed_sequence\":{},\"observed_at\":{},\"receipt_digest\":\"{}\",\"state_root\":\"{}\",\"valid_through\":{}}}}}",
        hex::encode(&read.program),
        lifecycle(read.lifecycle),
        accounts,
        read.freshness.observed_sequence,
        read.freshness.observed_at,
        hex::encode(&read.freshness.receipt_digest),
        hex::encode(&read.freshness.state_root),
        read.freshness.valid_through
    )
}

fn response<S: Write>(stream: &mut S, status: u16, body: &str) -> Result<(), String> {
    let reason = if status < 300 { "OK" } else { "Refused" };
    let header = format!(
        "HTTP/1.1 {status} {reason}\r\nContent-Type: application/json\r\nCache-Control: no-store\r\nContent-Length: {}\r\nConnection: close\r\n\r\n",
        body.len()
    );
    stream
        .write_all(header.as_bytes())
        .and_then(|()| stream.write_all(body.as_bytes()))
        .map_err(|error| format!("agent response failed: {error}"))
}

fn refresh_program_authority(
    route: &mut ProgramBalanceReadRoute,
    native: &mut Option<NativeReadRoute>,
) -> Result<(), String> {
    if let Some(native) = native.as_mut() {
        if let Some(history) = native
            .signed_authority()
            .map_err(|error| format!("program authority history unavailable: {error:?}"))?
        {
            route
                .refresh_authority(&history)
                .map_err(|error| format!("program authority policy refused: {error:?}"))?;
        }
    }
    Ok(())
}

enum Parsed {
    Admitted(String),
    Refused(u16, &'static str),
    Closed,
}

fn declared_body(request: &str) -> Result<usize, ()> {
    let mut length = None;
    for header in request.split("\r\n").skip(1) {
        let (name, value) = header.split_once(':').ok_or(())?;
        if name.eq_ignore_ascii_case("transfer-encoding") {
            return Err(());
        }
        if name.eq_ignore_ascii_case("content-length") {
            let value = value.trim();
            if length.is_some()
                || value.is_empty()
                || !value.bytes().all(|byte| byte.is_ascii_digit())
            {
                return Err(());
            }
            length = Some(value.parse().map_err(|_| ())?);
        }
    }
    Ok(length.unwrap_or(BODY_LIMIT))
}

/// Reads one request head under a single absolute deadline. Every read is also capped by the
/// per-I/O ceiling, so neither a silent peer nor a peer trickling partial bytes can hold a
/// connection worker beyond `REQUEST_DEADLINE`.
fn parse_request(stream: &mut DeadlineStream, bearer: &str) -> Parsed {
    let deadline = stream.expires;
    let mut bytes = [0_u8; HEADER_LIMIT];
    let mut length = 0_usize;
    while length < bytes.len() && !bytes[..length].windows(4).any(|value| value == b"\r\n\r\n") {
        let remaining = deadline.saturating_duration_since(Instant::now());
        if remaining.is_zero() {
            return Parsed::Refused(408, "{\"error\":\"request_timeout\"}");
        }
        match stream.read(&mut bytes[length..]) {
            Ok(0) => return Parsed::Closed,
            Ok(count) => length += count,
            Err(error)
                if matches!(
                    error.kind(),
                    std::io::ErrorKind::WouldBlock
                        | std::io::ErrorKind::TimedOut
                        | std::io::ErrorKind::Interrupted
                ) => {}
            Err(_) => return Parsed::Closed,
        }
    }
    let Some(end) = bytes[..length]
        .windows(4)
        .position(|value| value == b"\r\n\r\n")
    else {
        return Parsed::Refused(431, "{\"error\":\"headers_too_large\"}");
    };
    let Ok(request) = std::str::from_utf8(&bytes[..end]) else {
        return Parsed::Refused(400, "{\"error\":\"invalid_request\"}");
    };
    let line = request.lines().next().unwrap_or_default();
    let mut parts = line.split_ascii_whitespace();
    let method = parts.next().unwrap_or_default();
    let path = parts.next().unwrap_or_default();
    if parts.next() != Some("HTTP/1.1") || parts.next().is_some() || method != "GET" {
        return Parsed::Refused(400, "{\"error\":\"invalid_request\"}");
    }
    match declared_body(request) {
        Ok(BODY_LIMIT) => {}
        Ok(_) => return Parsed::Refused(413, "{\"error\":\"body_too_large\"}"),
        Err(()) => return Parsed::Refused(400, "{\"error\":\"invalid_request\"}"),
    }
    if length != end + 4 {
        return Parsed::Refused(413, "{\"error\":\"body_too_large\"}");
    }
    let mut credentials = request.lines().filter_map(|header| {
        let (name, value) = header.split_once(':')?;
        name.eq_ignore_ascii_case("authorization")
            .then_some(value.trim())
    });
    let authorized = credentials
        .next()
        .and_then(|value| value.strip_prefix("Bearer "))
        == Some(bearer)
        && credentials.next().is_none();
    if !authorized {
        return Parsed::Refused(401, "{\"error\":\"unauthorized\"}");
    }
    Parsed::Admitted(path.to_owned())
}

fn route_request(
    path: &str,
    probe_program: ProgramId,
    route: &mut ProgramBalanceReadRoute,
    native: &mut Option<NativeReadRoute>,
) -> (u16, String) {
    let reply = |status: u16, body: &str| (status, body.to_owned());
    if path == "/healthz" {
        if refresh_program_authority(route, native).is_err() {
            return reply(503, "{\"ready\":false}");
        }
        return match now_ms().map(|now| route.read(probe_program, now)) {
            Ok(Ok(_)) => reply(200, "{\"ready\":true}"),
            _ => reply(503, "{\"ready\":false}"),
        };
    }
    if path.starts_with("/v1/reads/") {
        let Some(reader) = native.as_mut() else {
            return reply(404, "{\"error\":\"not_found\"}");
        };
        return match reader.read(path) {
            Ok(value) => (200, value.to_string()),
            Err(
                layerx_agentd::read::NativeReadError::InvalidRequest
                | layerx_agentd::read::NativeReadError::CursorMismatch,
            ) => reply(400, "{\"error\":\"invalid_read\"}"),
            Err(layerx_agentd::read::NativeReadError::ResultTooLarge) => {
                reply(413, "{\"error\":\"read_too_large\"}")
            }
            Err(_) => reply(503, "{\"error\":\"verified_read_unavailable\"}"),
        };
    }
    let Some(program_text) = path
        .strip_prefix("/v1/programs/")
        .and_then(|value| value.strip_suffix("/balances"))
    else {
        return reply(404, "{\"error\":\"not_found\"}");
    };
    let program = hex::decode_digest(program_text)
        .ok()
        .and_then(|bytes| ProgramId::new(bytes).ok());
    let Some(program) = program else {
        return reply(400, "{\"error\":\"invalid_program\"}");
    };
    if refresh_program_authority(route, native).is_err() {
        return reply(503, "{\"error\":\"program_state_unavailable\"}");
    }
    match now_ms().map(|now| route.read(program, now)) {
        Ok(Ok(read)) => (200, balance_json(&read)),
        _ => reply(503, "{\"error\":\"program_state_unavailable\"}"),
    }
}

fn native_handover_sources() -> Result<Option<(PathBuf, PathBuf)>, String> {
    let genesis_trust = match std::env::var("LAYERX_AGENT_GENESIS_TRUST") {
        Ok(path) => Some(path),
        Err(std::env::VarError::NotPresent) => None,
        Err(std::env::VarError::NotUnicode(_)) => {
            return Err("native genesis trust path is not UTF-8".to_owned())
        }
    };
    let handover_finality = match std::env::var("LAYERX_AGENT_HANDOVER_FINALITY") {
        Ok(path) => Some(path),
        Err(std::env::VarError::NotPresent) => None,
        Err(std::env::VarError::NotUnicode(_)) => {
            return Err("handover finality policy path is not UTF-8".to_owned())
        }
    };
    match (genesis_trust, handover_finality) {
        (None, None) => Ok(None),
        (Some(genesis), Some(finality)) => {
            Ok(Some((PathBuf::from(genesis), PathBuf::from(finality))))
        }
        _ => Err(
            "native genesis trust and handover finality policy must be configured together"
                .to_owned(),
        ),
    }
}

struct ProgramAuthorityBoot {
    verifier: ProtocolDeploymentVerifier,
    registry: Registry,
    signed_history: Option<layerx_proof::signed_authority::SignedAuthorityHistory>,
}

fn program_authority_boot(
    config: &Config,
    native: &mut Option<NativeReadRoute>,
) -> Result<ProgramAuthorityBoot, String> {
    let verifier = ProtocolDeploymentVerifier::from_protected_history(
        Path::new(&config.sequencer_trust_history),
        config.staleness_ms,
    )
    .map_err(|error| format!("agent deployment verifier is invalid: {error}"))?;
    let signed_history = native
        .as_mut()
        .map(NativeReadRoute::signed_authority)
        .transpose()
        .map_err(|error| format!("program history unavailable: {error:?}"))?
        .flatten();
    let admission_verifier = signed_history
        .as_ref()
        .map(|history| verifier.with_signed_history(history))
        .transpose()
        .map_err(|error| format!("program admission authority refused: {error:?}"))?;
    let registry = load_registry(
        Path::new(&config.deployment_journal),
        admission_verifier.as_ref().unwrap_or(&verifier),
    )?;
    Ok(ProgramAuthorityBoot {
        verifier,
        registry,
        signed_history,
    })
}

/// Connects one authenticated protocol reader from the configured node and authority,
/// bound to the protected verifier and refreshed with the signed authority history.
fn program_reader(
    config: &Config,
    verifier: ProtocolDeploymentVerifier,
    registry: Registry,
    signed_history: Option<&layerx_proof::signed_authority::SignedAuthorityHistory>,
) -> Result<LayerxdProgramBalanceReader, String> {
    let mut reader = LayerxdProgramBalanceReader::connect(
        &config.node_endpoint,
        config.node_bearer.clone(),
        ProgramAuthority {
            endpoint: &config.authority_endpoint,
            authorization: config.authority_bearer.clone(),
            replica_id: config.authority_replica_id,
            ca_der: &config.authority_ca_der,
        },
        verifier,
        registry,
    )
    .map_err(|error| format!("agent protocol reader configuration failed: {error:?}"))?;
    if let Some(history) = signed_history {
        reader
            .refresh_authority(history)
            .map_err(|error| format!("program authority refused: {error:?}"))?;
    }
    Ok(reader)
}

fn serve(config: Config) -> Result<(), String> {
    let mcp = mcp_enrolment()?
        .map(|enrolment| mcp_boot(&config, enrolment))
        .transpose()?;
    let handover_sources = native_handover_sources()?;
    if handover_sources.is_some() && mcp.is_none() {
        return Err("native genesis trust requires the configured native read boundary".to_owned());
    }
    let runtime_clock = layerx_client::runtime_clock::RuntimeClock::from_environment()
        .map_err(|error| format!("runtime clock unavailable: {error}"))?;

    let mut native = mcp
        .as_ref()
        .map(|boot| {
            let limits = human_lni_limits(boot.enrolment.deadline)?;
            let limits = Limits {
                maximum_frame_bytes: limits
                    .maximum_frame_bytes
                    .max(layerx_client::evidence::MINIMUM_FINALITY_FRAME_BYTES),
                ..limits
            };
            let client = connect_human_node(
                PathBuf::from(required("LAYERX_AGENT_HUMAN_NODE_LNI")?),
                limits,
            )?;
            let route = NativeReadRoute::new(
                client,
                boot.enrolment.did.clone(),
                config.bearer.clone(),
                runtime_clock.clone(),
            )
            .map_err(|error| format!("native read route is invalid: {error:?}"))?;
            match handover_sources.as_ref() {
                Some((genesis, finality)) => route
                    .with_protected_finality(finality)
                    .and_then(|route| route.with_protected_genesis(genesis))
                    .map_err(|error| format!("native genesis trust is invalid: {error:?}")),
                None => Ok(route),
            }
        })
        .transpose()?;
    let ProgramAuthorityBoot {
        verifier,
        registry,
        signed_history,
    } = program_authority_boot(&config, &mut native)?;
    let mut route = ProgramBalanceReadRoute::new(program_reader(
        &config,
        verifier.clone(),
        registry.clone(),
        signed_history.as_ref(),
    )?);
    let programs = ProgramOperations::new(program_reader(
        &config,
        verifier,
        registry,
        signed_history.as_ref(),
    )?);
    let export_trust = signed_history.map(export_trust_source).transpose()?;
    let (human, owner, status) = start_shared_owner(
        mcp,
        Some(programs),
        export_trust,
        Some(&config.policy_sources),
        &config.native_policy_sources,
        &config.program_budget_denomination_sources,
        runtime_clock,
    )?;
    let rpc = start_agent_rpc(owner)?;
    drop(status);
    route
        .read(config.probe_program, now_ms()?)
        .map_err(|error| format!("agent protocol reader is not ready: {error:?}"))?;
    let listener = TcpListener::bind(&config.listen)
        .map_err(|error| format!("agent program listener failed: {error}"))?;
    let shared_reads = Arc::new(Mutex::new((route, native)));
    let program = HttpPool::start(listener, move |mut stream| {
        let answer = match parse_request(&mut stream, &config.bearer) {
            Parsed::Closed => return,
            Parsed::Refused(status, body) => (status, body.to_owned()),
            Parsed::Admitted(path) => loop {
                if stream.remaining().is_err() {
                    return;
                }
                match shared_reads.try_lock() {
                    Ok(mut reads) => {
                        let (route, native) = &mut *reads;
                        break route_request(&path, config.probe_program, route, native);
                    }
                    Err(std::sync::TryLockError::WouldBlock) => thread::sleep(SUPERVISION_INTERVAL),
                    Err(std::sync::TryLockError::Poisoned(_)) => return,
                }
            },
        };
        let _ = response(&mut stream, answer.0, &answer.1);
    })?;
    loop {
        match human.try_recv() {
            Ok(result) => return result,
            Err(mpsc::TryRecvError::Disconnected) => {
                return Err("human listener terminated without status".to_owned())
            }
            Err(mpsc::TryRecvError::Empty) => {}
        }
        program.check()?;
        if let Some(rpc) = &rpc {
            rpc.check()?;
        }
        thread::sleep(SUPERVISION_INTERVAL);
    }
}

fn main() {
    if let Err(error) = human_owner_mode::run() {
        eprintln!("layerx-agentd: {}", Redacted::boot_diagnostic(&error));
        std::process::exit(2);
    }
}
