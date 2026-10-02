//! Agent-daemon enrolment of one model context protocol session and the binding document
//! the served path reads.
//!
//! Enrolment opens a capability-grant session for a registered identity and publishes the
//! binding document beside two operator-protected secret files: the session token and the
//! daemon bearer. The served path holds no other credential. The document also declares the
//! protected Unix listener the socket server binds and the peer user ids it admits, and the
//! search sidecar reference when the web tools are enabled.

use std::collections::BTreeSet;
use std::fmt;
use std::fs::{self, DirBuilder, OpenOptions};
use std::io::{self, Write as _};
use std::os::unix::fs::{
    DirBuilderExt as _, MetadataExt as _, OpenOptionsExt as _, PermissionsExt as _,
};
use std::path::{Component, Path, PathBuf};
use std::time::Duration;

use layerx_programs::hex;
use serde_json::{json, Value};
use zeroize::Zeroizing;

use crate::budget::{LimitConfig, LimitScope};
use crate::capability::{Capability, CapabilityId};
use crate::identity::{IdentityRecord, ProtocolAuthority};
use crate::session::{self, OpenRequest, SessionError, SessionId, SessionRegistry};
use crate::store::{Store, TenantId};

/// File name of the binding document inside the binding directory.
pub const BINDING_FILE: &str = "binding.json";
/// File name of the session token the served path reads.
pub const SESSION_TOKEN_FILE: &str = "session-token";
/// File name of the daemon bearer the served path presents to the agent daemon.
pub const DAEMON_BEARER_FILE: &str = "daemon-bearer";

const MINIMUM_BEARER_BYTES: usize = 32;
const MAX_TEXT_BYTES: usize = 255;
const MAX_DOCUMENT_BYTES: usize = 65_536;
const TOKEN_ATTEMPTS: usize = 8;
const MAX_SOCKET_PATH_BYTES: usize = 107;
const MAX_ADMITTED_PEERS: usize = 64;
const LISTENER_MODE_MASK: u32 = 0o660;
const LISTENER_OWNER_ACCESS: u32 = 0o600;

/// Deployment mode the binding document declares.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum BindingMode {
    Full,
    ReadOnly,
}

impl BindingMode {
    /// Returns the label the binding document carries.
    #[must_use]
    pub const fn label(self) -> &'static str {
        match self {
            Self::Full => "full",
            Self::ReadOnly => "read-only",
        }
    }
}

/// Typed refusal of one enrolment. It never carries secret material.
#[derive(Debug)]
pub enum EnrolmentError {
    InvalidSurface(&'static str),
    InvalidPath(&'static str),
    InvalidLimit(&'static str),
    MissingCapability,
    InvalidListener(&'static str),
    InvalidWeb(&'static str),
    AlreadyPublished(PathBuf),
    Session(SessionError),
    OrphanedSession(SessionId, Box<EnrolmentError>),
    Capability(crate::capability::CapabilityError),
    Io(io::Error),
    Encoding(&'static str),
}

impl fmt::Display for EnrolmentError {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::InvalidSurface(reason) => {
                write!(formatter, "the daemon surface is invalid: {reason}")
            }
            Self::InvalidPath(reason) => write!(formatter, "a binding path is invalid: {reason}"),
            Self::InvalidLimit(reason) => write!(formatter, "the limit is invalid: {reason}"),
            Self::MissingCapability => {
                formatter.write_str("the capability is not persisted for this tenant")
            }
            Self::InvalidListener(reason) => {
                write!(formatter, "the protocol listener is invalid: {reason}")
            }
            Self::InvalidWeb(reason) => {
                write!(formatter, "the web tool reference is invalid: {reason}")
            }
            Self::AlreadyPublished(path) => write!(
                formatter,
                "a different binding already occupies {}",
                path.display()
            ),
            Self::Session(error) => write!(formatter, "the session was refused: {error:?}"),
            Self::OrphanedSession(session, cause) => write!(
                formatter,
                "session {} stayed open after publication failed: {cause}",
                hex::encode(&session.0)
            ),
            Self::Capability(error) => write!(formatter, "the capability is unusable: {error:?}"),
            Self::Io(error) => write!(formatter, "binding publication failed: {error}"),
            Self::Encoding(reason) => write!(formatter, "the binding cannot be encoded: {reason}"),
        }
    }
}

impl std::error::Error for EnrolmentError {}

impl From<io::Error> for EnrolmentError {
    fn from(value: io::Error) -> Self {
        Self::Io(value)
    }
}

/// The loopback agent daemon surface the served path authorizes against.
pub struct DaemonSurface {
    endpoint: String,
    bearer: Zeroizing<String>,
    probe_program: [u8; 32],
}

impl fmt::Debug for DaemonSurface {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter
            .debug_struct("DaemonSurface")
            .field("endpoint", &self.endpoint)
            .field("bearer", &"[redacted]")
            .field("probe_program", &hex::encode(&self.probe_program))
            .finish()
    }
}

impl DaemonSurface {
    /// Validates the daemon endpoint, bearer, and probe program the binding will name.
    ///
    /// # Errors
    ///
    /// Refuses a non-loopback endpoint, a bearer shorter than the daemon accepts, an empty
    /// probe program id, and any text the binding document cannot carry.
    pub fn new(
        endpoint: &str,
        bearer: String,
        probe_program: [u8; 32],
    ) -> Result<Self, EnrolmentError> {
        if !endpoint.starts_with("127.0.0.1:") || endpoint.len() > MAX_TEXT_BYTES {
            return Err(EnrolmentError::InvalidSurface(
                "the agent daemon endpoint is not a loopback endpoint",
            ));
        }
        let bearer = Zeroizing::new(bearer);
        if bearer.len() < MINIMUM_BEARER_BYTES || bearer.trim() != bearer.as_str() {
            return Err(EnrolmentError::InvalidSurface(
                "the agent daemon bearer is shorter than the daemon accepts or carries whitespace",
            ));
        }
        if probe_program == [0; 32] {
            return Err(EnrolmentError::InvalidSurface(
                "the probe program is not a program id",
            ));
        }
        Ok(Self {
            endpoint: endpoint.to_owned(),
            bearer,
            probe_program,
        })
    }
}

/// The protected Unix listener the socket server binds and the peer user ids it admits.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ListenerDeclaration {
    socket: PathBuf,
    owner_uid: u32,
    owner_gid: u32,
    mode: u32,
    admitted_uids: Vec<u32>,
}

impl ListenerDeclaration {
    /// Validates the socket path, its owner and mode, and the admitted peer user ids.
    ///
    /// # Errors
    ///
    /// Refuses a relative, non-UTF-8, non-normal or over-long socket path, a socket directly in
    /// the root directory, a mode that grants more than owner and group read and write or
    /// withholds owner read and write, and an admission set that is
    /// empty, larger than the served path accepts, or names one user id twice.
    pub fn new(
        socket: PathBuf,
        owner_uid: u32,
        owner_gid: u32,
        mode: u32,
        admitted_uids: Vec<u32>,
    ) -> Result<Self, EnrolmentError> {
        if !socket.is_absolute() {
            return Err(EnrolmentError::InvalidListener(
                "the socket path is not absolute",
            ));
        }
        let text = socket.to_str().ok_or(EnrolmentError::InvalidListener(
            "the socket path is not UTF-8",
        ))?;
        if text.len() > MAX_SOCKET_PATH_BYTES {
            return Err(EnrolmentError::InvalidListener(
                "the socket path is longer than a Unix socket address carries",
            ));
        }
        if socket
            .components()
            .any(|component| !matches!(component, Component::RootDir | Component::Normal(_)))
            || socket.file_name().is_none()
            || socket.parent().is_none_or(|parent| parent == Path::new("/"))
            || text.ends_with('/')
        {
            return Err(EnrolmentError::InvalidListener(
                "the socket path is not a normal file path inside a dedicated directory",
            ));
        }
        if mode & !LISTENER_MODE_MASK != 0 {
            return Err(EnrolmentError::InvalidListener(
                "the socket mode grants more than owner and group read and write",
            ));
        }
        if mode & LISTENER_OWNER_ACCESS != LISTENER_OWNER_ACCESS {
            return Err(EnrolmentError::InvalidListener(
                "the socket mode withholds owner read and write",
            ));
        }
        if admitted_uids.is_empty() || admitted_uids.len() > MAX_ADMITTED_PEERS {
            return Err(EnrolmentError::InvalidListener(
                "the admitted peers must name 1 to 64 user ids",
            ));
        }
        let mut seen = BTreeSet::new();
        for uid in &admitted_uids {
            if !seen.insert(*uid) {
                return Err(EnrolmentError::InvalidListener(
                    "the admitted peers name one user id twice",
                ));
            }
        }
        Ok(Self {
            socket,
            owner_uid,
            owner_gid,
            mode,
            admitted_uids,
        })
    }

    /// Confirms the socket directory exists as a canonical directory owned by the declared
    /// owner and group with no access for others, the same directory the socket server
    /// validates before it binds.
    fn check_directory(&self) -> Result<(), EnrolmentError> {
        let parent = self.socket.parent().ok_or(EnrolmentError::InvalidListener(
            "the socket path has no directory",
        ))?;
        let metadata = match fs::symlink_metadata(parent) {
            Ok(metadata) => metadata,
            Err(error) if error.kind() == io::ErrorKind::NotFound => {
                return Err(EnrolmentError::InvalidListener(
                    "the socket directory does not exist",
                ))
            }
            Err(error) => return Err(EnrolmentError::Io(error)),
        };
        if metadata.file_type().is_symlink() || !metadata.is_dir() {
            return Err(EnrolmentError::InvalidListener(
                "the socket directory is not a directory",
            ));
        }
        if fs::canonicalize(parent)? != parent {
            return Err(EnrolmentError::InvalidListener(
                "the socket directory path is not canonical",
            ));
        }
        if metadata.uid() != self.owner_uid
            || metadata.gid() != self.owner_gid
            || metadata.mode() & 0o007 != 0
        {
            return Err(EnrolmentError::InvalidListener(
                "the socket directory is not owned by the declared owner and group or admits others",
            ));
        }
        Ok(())
    }

    fn document(&self) -> Result<Value, EnrolmentError> {
        let socket = self.socket.to_str().ok_or(EnrolmentError::InvalidListener(
            "the socket path is not UTF-8",
        ))?;
        Ok(json!({
            "socket": socket,
            "owner_uid": self.owner_uid,
            "owner_gid": self.owner_gid,
            "mode": format!("{:04o}", self.mode),
            "admitted_uids": self.admitted_uids,
        }))
    }
}

/// The search sidecar reference a session with the web scopes is served through. It carries
/// no payer or key material; paid tools still require the separately attached payer.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct WebDeclaration {
    endpoint: String,
    network: String,
    sequencer_public_key: [u8; 32],
    timeout_ms: u64,
    pending_attempts: u8,
    approval_threshold: u128,
}

impl WebDeclaration {
    /// Validates the sidecar reference the binding document will carry.
    ///
    /// # Errors
    ///
    /// Refuses empty or over-long text, an all-zero sequencer key, a zero timeout, and zero
    /// pending attempts.
    pub fn new(
        endpoint: String,
        network: String,
        sequencer_public_key: [u8; 32],
        timeout_ms: u64,
        pending_attempts: u8,
        approval_threshold: u128,
    ) -> Result<Self, EnrolmentError> {
        for value in [&endpoint, &network] {
            if value.is_empty() || value.len() > MAX_TEXT_BYTES {
                return Err(EnrolmentError::InvalidWeb(
                    "the web endpoint and network must be 1 to 255 bytes",
                ));
            }
        }
        if sequencer_public_key == [0; 32] {
            return Err(EnrolmentError::InvalidWeb(
                "the sequencer public key is not a key",
            ));
        }
        if timeout_ms == 0 {
            return Err(EnrolmentError::InvalidWeb("the web timeout is zero"));
        }
        if pending_attempts == 0 {
            return Err(EnrolmentError::InvalidWeb(
                "the web pending attempts are zero",
            ));
        }
        Ok(Self {
            endpoint,
            network,
            sequencer_public_key,
            timeout_ms,
            pending_attempts,
            approval_threshold,
        })
    }

    fn document(&self) -> Value {
        json!({
            "endpoint": self.endpoint,
            "network": self.network,
            "sequencer_public_key": hex::encode(&self.sequencer_public_key),
            "timeout_ms": self.timeout_ms,
            "pending_attempts": self.pending_attempts,
            "approval_threshold": self.approval_threshold.to_string(),
        })
    }
}

/// One opened session together with the coordinates the binding document records.
pub struct Enrolment {
    tenant: TenantId,
    session_id: SessionId,
    session_generation: u64,
    capability_id: CapabilityId,
    core_sequence: u64,
    token: Zeroizing<[u8; 32]>,
}

impl fmt::Debug for Enrolment {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter
            .debug_struct("Enrolment")
            .field("tenant", &self.tenant)
            .field("session_id", &hex::encode(&self.session_id.0))
            .field("session_generation", &self.session_generation)
            .field("capability_id", &hex::encode(&self.capability_id.0))
            .field("core_sequence", &self.core_sequence)
            .field("token", &"[redacted]")
            .finish()
    }
}

/// The published binding and the protected files beside it.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct PublishedBinding {
    pub binding: PathBuf,
    pub session_token_file: PathBuf,
    pub daemon_bearer_file: PathBuf,
    pub session_id: SessionId,
    pub session_generation: u64,
    pub created: bool,
}

/// Everything one enrolment needs beyond the identity and the store.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct EnrolmentRequest {
    pub session_id: SessionId,
    pub capability_id: CapabilityId,
    pub permitted_activity_types: BTreeSet<u16>,
    pub scopes: BTreeSet<String>,
    pub expiry_sequence: u64,
    pub opening_client: String,
    pub policy_version: String,
    pub core_sequence: u64,
}

/// Writes binding documents for one daemon into one binding directory.
#[derive(Debug)]
pub struct BindingPublisher {
    root: PathBuf,
    store: PathBuf,
    audit_root: PathBuf,
    surface: DaemonSurface,
    limit: LimitConfig,
    deadline: Duration,
    mode: BindingMode,
    listener: Option<ListenerDeclaration>,
    web: Option<WebDeclaration>,
}

impl BindingPublisher {
    /// Validates the binding directory, the daemon records it names, and the limit it declares.
    ///
    /// # Errors
    ///
    /// Refuses a relative or over-long path, a zero deadline, and a limit without a name.
    pub fn new(
        root: PathBuf,
        store: PathBuf,
        audit_root: PathBuf,
        surface: DaemonSurface,
        limit: LimitConfig,
        deadline: Duration,
        mode: BindingMode,
    ) -> Result<Self, EnrolmentError> {
        for path in [&root, &store, &audit_root] {
            path_text(path)?;
        }
        for file in [BINDING_FILE, SESSION_TOKEN_FILE, DAEMON_BEARER_FILE] {
            path_text(&root.join(file))?;
        }
        if deadline.is_zero() {
            return Err(EnrolmentError::InvalidSurface(
                "the transport deadline is zero",
            ));
        }
        u64::try_from(deadline.as_millis()).map_err(|_| {
            EnrolmentError::Encoding("the transport deadline exceeds u64 milliseconds")
        })?;
        if limit.name.is_empty() || limit.name.len() > MAX_TEXT_BYTES {
            return Err(EnrolmentError::InvalidLimit(
                "the limit name must be 1 to 255 bytes",
            ));
        }
        Ok(Self {
            root,
            store,
            audit_root,
            surface,
            limit,
            deadline,
            mode,
            listener: None,
            web: None,
        })
    }

    /// Declares the protected listener the socket server binds and the peers it admits.
    #[must_use]
    pub fn with_listener(mut self, listener: ListenerDeclaration) -> Self {
        self.listener = Some(listener);
        self
    }

    /// Declares the search sidecar reference the web tools are served through.
    #[must_use]
    pub fn with_web(mut self, web: WebDeclaration) -> Self {
        self.web = Some(web);
        self
    }

    /// Returns the path of the binding document this publisher writes.
    #[must_use]
    pub fn binding_path(&self) -> PathBuf {
        self.root.join(BINDING_FILE)
    }

    /// Publishes the binding document, the session token, and the daemon bearer for one
    /// enrolment as owner-only files inside an owner-only directory.
    ///
    /// An identical binding already at the path is left in place and reported as not created;
    /// a different binding at the path is refused rather than replaced.
    ///
    /// # Errors
    ///
    /// Refuses a declared socket path inside the binding directory or in a socket directory
    /// that is missing, non-canonical, or not owned by the declared owner and group, a binding directory that is not a canonical directory, a different published
    /// binding, a pre-existing secret file, and any I/O failure while writing.
    pub fn publish(&self, enrolment: &Enrolment) -> Result<PublishedBinding, EnrolmentError> {
        self.check_listener()?;
        let document = self.document(enrolment)?;
        prepare_root(&self.root)?;
        let binding = self.binding_path();
        let session_token_file = self.root.join(SESSION_TOKEN_FILE);
        let daemon_bearer_file = self.root.join(DAEMON_BEARER_FILE);
        if let Some(existing) = read_document(&binding)? {
            if existing != document {
                return Err(EnrolmentError::AlreadyPublished(binding));
            }
            for secret in [&session_token_file, &daemon_bearer_file] {
                if !fs::symlink_metadata(secret)?.is_file() {
                    return Err(EnrolmentError::InvalidPath(
                        "a published binding lost one of its secret files",
                    ));
                }
            }
            return Ok(PublishedBinding {
                binding,
                session_token_file,
                daemon_bearer_file,
                session_id: enrolment.session_id,
                session_generation: enrolment.session_generation,
                created: false,
            });
        }
        let encoded = serde_json::to_vec_pretty(&document)
            .map_err(|_| EnrolmentError::Encoding("the binding document is not serializable"))?;
        if encoded.len() > MAX_DOCUMENT_BYTES {
            return Err(EnrolmentError::Encoding(
                "the binding document exceeds the served path's bound",
            ));
        }
        let token = Zeroizing::new(hex::encode(enrolment.token.as_slice()));
        let mut written: Vec<&Path> = Vec::with_capacity(3);
        let result = (|| {
            create_private(&session_token_file, token.as_bytes())?;
            written.push(&session_token_file);
            create_private(&daemon_bearer_file, self.surface.bearer.as_bytes())?;
            written.push(&daemon_bearer_file);
            create_private(&binding, &encoded)?;
            written.push(&binding);
            sync_directory(&self.root)
        })();
        if let Err(error) = result {
            for path in written.iter().rev() {
                if let Err(removal) = fs::remove_file(path) {
                    if removal.kind() != io::ErrorKind::NotFound {
                        return Err(EnrolmentError::Io(removal));
                    }
                }
            }
            return Err(error);
        }
        Ok(PublishedBinding {
            binding,
            session_token_file,
            daemon_bearer_file,
            session_id: enrolment.session_id,
            session_generation: enrolment.session_generation,
            created: true,
        })
    }

    fn check_listener(&self) -> Result<(), EnrolmentError> {
        let Some(listener) = &self.listener else {
            return Ok(());
        };
        if listener.socket.starts_with(&self.root) {
            return Err(EnrolmentError::InvalidListener(
                "the socket path lies inside the protected binding directory",
            ));
        }
        listener.check_directory()
    }

    fn document(&self, enrolment: &Enrolment) -> Result<Value, EnrolmentError> {
        let (scope, scope_id) = match self.limit.scope {
            LimitScope::Tenant(id) => ("tenant", id),
            LimitScope::Agent(id) => ("agent", id),
            LimitScope::Session(id) => ("session", id),
            LimitScope::Capability(id) => ("capability", id),
            LimitScope::Counterparty(id) => ("counterparty", id),
        };
        let deadline_ms = u64::try_from(self.deadline.as_millis()).map_err(|_| {
            EnrolmentError::Encoding("the transport deadline exceeds u64 milliseconds")
        })?;
        let mut document = json!({
            "mode": self.mode.label(),
            "tenant": enrolment.tenant.as_str(),
            "store": path_text(&self.store)?,
            "audit_root": path_text(&self.audit_root)?,
            "session_id": hex::encode(&enrolment.session_id.0),
            "session_token_file": path_text(&self.root.join(SESSION_TOKEN_FILE))?,
            "session_generation": enrolment.session_generation,
            "capability_id": hex::encode(&enrolment.capability_id.0),
            "core_sequence": enrolment.core_sequence,
            "deadline_ms": deadline_ms,
            "agent": {
                "endpoint": self.surface.endpoint,
                "bearer_file": path_text(&self.root.join(DAEMON_BEARER_FILE))?,
                "probe_program": hex::encode(&self.surface.probe_program),
            },
            "limit": {
                "id": hex::encode(&self.limit.id.0),
                "name": self.limit.name,
                "scope": scope,
                "scope_id": hex::encode(&scope_id),
                "ceiling": self.limit.ceiling.to_string(),
                "consumed": self.limit.consumed.to_string(),
            },
        });
        if let Some(object) = document.as_object_mut() {
            if let Some(listener) = &self.listener {
                object.insert("listener".to_owned(), listener.document()?);
            }
            if let Some(web) = &self.web {
                object.insert("web".to_owned(), web.document());
            }
        }
        Ok(document)
    }
}

/// Opens one capability-grant session for a registered identity and publishes its binding.
///
/// The capability must already be persisted for the identity's tenant and the identity must
/// carry the matching capability grant; the session token is minted here and reaches disk only
/// through the publisher. A session whose binding cannot be published is closed again.
///
/// # Errors
///
/// Refuses an unusable declared listener before any session is opened, a missing capability, a
/// session the registry or store refuses, and any binding publication failure; `OrphanedSession` names a session that stayed open because closing it
/// after a failed publication also failed.
pub fn enrol(
    store: &mut Store,
    sessions: &mut SessionRegistry,
    identity: &IdentityRecord,
    request: EnrolmentRequest,
    publisher: &BindingPublisher,
) -> Result<PublishedBinding, EnrolmentError> {
    publisher.check_listener()?;
    let tenant = identity.tenant().clone();
    Capability::restore(store, tenant.clone(), request.capability_id)
        .map_err(EnrolmentError::Capability)?
        .ok_or(EnrolmentError::MissingCapability)?;
    let token = fresh_token()?;
    let open = OpenRequest {
        session_id: request.session_id,
        token_id: *token,
        tenant: tenant.clone(),
        agent: identity.did().clone(),
        authority: ProtocolAuthority::CapabilityGrant(request.capability_id.0),
        permitted_activity_types: request.permitted_activity_types,
        scopes: request.scopes,
        expiry_sequence: request.expiry_sequence,
        expiry_seconds: None,
        opening_client: request.opening_client,
        policy_version: request.policy_version,
    };
    let issued = session::open(store, sessions, identity, open, request.core_sequence)
        .map_err(EnrolmentError::Session)?;
    let enrolment = Enrolment {
        tenant: tenant.clone(),
        session_id: request.session_id,
        session_generation: issued.generation(),
        capability_id: request.capability_id,
        core_sequence: request.core_sequence,
        token: Zeroizing::new(issued.token_id()),
    };
    drop(issued);
    match publisher.publish(&enrolment) {
        Ok(binding) => Ok(binding),
        Err(error) => match session::close(store, sessions, &tenant, request.session_id) {
            Ok(()) => Err(error),
            Err(_) => Err(EnrolmentError::OrphanedSession(
                request.session_id,
                Box::new(error),
            )),
        },
    }
}

/// Returns the session a published binding document names, when one is already published.
///
/// A daemon that restarts after a successful enrolment reads the session back from its store
/// rather than opening a second one; this reports which session the published document belongs
/// to so that boot can refuse a document that names a different one.
///
/// # Errors
///
/// Returns `AlreadyPublished` for a path that is not a regular file, a document larger than the
/// served path's bound, or one that is not JSON; `Io` for a read failure; and `Encoding` when the
/// document carries no `session_id` of 32 hexadecimal bytes.
pub fn published_session(path: &Path) -> Result<Option<SessionId>, EnrolmentError> {
    let Some(document) = read_document(path)? else {
        return Ok(None);
    };
    let encoded =
        document
            .get("session_id")
            .and_then(Value::as_str)
            .ok_or(EnrolmentError::Encoding(
                "the published binding names no session",
            ))?;
    let session = hex::decode_digest(encoded)
        .map_err(|_| EnrolmentError::Encoding("the published binding names an invalid session"))?;
    Ok(Some(SessionId(session)))
}

fn fresh_token() -> Result<Zeroizing<[u8; 32]>, EnrolmentError> {
    let mut token = Zeroizing::new([0_u8; 32]);
    for _ in 0..TOKEN_ATTEMPTS {
        getrandom::fill(token.as_mut_slice())
            .map_err(|_| EnrolmentError::Encoding("the platform entropy source is unavailable"))?;
        if *token != [0; 32] {
            return Ok(token);
        }
    }
    Err(EnrolmentError::Encoding(
        "the platform entropy source returned only zero tokens",
    ))
}

fn path_text(path: &Path) -> Result<&str, EnrolmentError> {
    if !path.is_absolute() {
        return Err(EnrolmentError::InvalidPath("the path is not absolute"));
    }
    let text = path
        .to_str()
        .ok_or(EnrolmentError::InvalidPath("the path is not UTF-8"))?;
    if text.is_empty() || text.len() > MAX_TEXT_BYTES {
        return Err(EnrolmentError::InvalidPath(
            "the path is longer than the binding document carries",
        ));
    }
    Ok(text)
}

fn prepare_root(root: &Path) -> Result<(), EnrolmentError> {
    match fs::symlink_metadata(root) {
        Ok(metadata) => {
            if metadata.file_type().is_symlink() || !metadata.is_dir() {
                return Err(EnrolmentError::InvalidPath(
                    "the binding directory is not a directory",
                ));
            }
            fs::set_permissions(root, fs::Permissions::from_mode(0o700))?;
        }
        Err(error) if error.kind() == io::ErrorKind::NotFound => {
            DirBuilder::new().recursive(true).mode(0o700).create(root)?;
            fs::set_permissions(root, fs::Permissions::from_mode(0o700))?;
        }
        Err(error) => return Err(EnrolmentError::Io(error)),
    }
    if fs::canonicalize(root)? != root {
        return Err(EnrolmentError::InvalidPath(
            "the binding directory path is not canonical",
        ));
    }
    Ok(())
}

fn read_document(path: &Path) -> Result<Option<Value>, EnrolmentError> {
    let metadata = match fs::symlink_metadata(path) {
        Ok(metadata) => metadata,
        Err(error) if error.kind() == io::ErrorKind::NotFound => return Ok(None),
        Err(error) => return Err(EnrolmentError::Io(error)),
    };
    if !metadata.is_file() {
        return Err(EnrolmentError::AlreadyPublished(path.to_path_buf()));
    }
    if usize::try_from(metadata.len()).unwrap_or(usize::MAX) > MAX_DOCUMENT_BYTES {
        return Err(EnrolmentError::AlreadyPublished(path.to_path_buf()));
    }
    let bytes = fs::read(path)?;
    serde_json::from_slice::<Value>(&bytes)
        .map(Some)
        .map_err(|_| EnrolmentError::AlreadyPublished(path.to_path_buf()))
}

fn create_private(path: &Path, contents: &[u8]) -> Result<(), EnrolmentError> {
    let mut file = OpenOptions::new()
        .write(true)
        .create_new(true)
        .mode(0o600)
        .open(path)
        .map_err(|error| {
            if error.kind() == io::ErrorKind::AlreadyExists {
                EnrolmentError::AlreadyPublished(path.to_path_buf())
            } else {
                EnrolmentError::Io(error)
            }
        })?;
    file.write_all(contents)?;
    file.write_all(b"\n")?;
    file.sync_all()?;
    Ok(())
}

fn sync_directory(root: &Path) -> Result<(), EnrolmentError> {
    fs::File::open(root)?.sync_all()?;
    Ok(())
}
