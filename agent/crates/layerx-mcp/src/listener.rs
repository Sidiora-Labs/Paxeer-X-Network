//! Peer-credential admitted Unix listener for the daemon-bound protocol transport.

use std::fs;
use std::io::BufReader;
use std::os::unix::fs::{FileTypeExt as _, MetadataExt as _, PermissionsExt as _};
use std::os::unix::net::UnixListener;
use std::path::{Component, Path, PathBuf};
use std::os::unix::ffi::OsStrExt as _;
use std::time::Duration;

use crate::boundary::ToolBoundary;
use crate::stdio::Session;

/// Complete admission configuration. Every field is enforced before a connection is served.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ListenerConfig {
    pub endpoint: PathBuf,
    pub owner_uid: u32,
    pub owner_gid: u32,
    pub mode: u32,
    pub admitted_uids: Vec<u32>,
    pub deadline: Duration,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum ListenerError {
    RelativeEndpoint,
    EndpointExists,
    ParentUnowned,
    ModeTooBroad,
    NoAdmittedPeer,
    ZeroDeadline,
    Bind,
    Permissions,
    Accept,
}

impl ListenerError {
    /// Renders the refusal without exposing filesystem contents.
    #[must_use]
    pub const fn detail(self) -> &'static str {
        match self {
            Self::RelativeEndpoint => "the protocol socket path must be absolute",
            Self::EndpointExists => "the protocol socket path is already present",
            Self::ParentUnowned => "the protocol socket directory is not owned by the daemon",
            Self::ModeTooBroad => "the protocol socket mode admits more than owner and group",
            Self::NoAdmittedPeer => "no peer identity is admitted",
            Self::ZeroDeadline => "the protocol socket deadline is zero",
            Self::Bind => "the protocol socket could not be bound",
            Self::Permissions => "the protocol socket permissions could not be enforced",
            Self::Accept => "the protocol socket stopped accepting connections",
        }
    }
}

/// Owns one bound socket. The socket is removed when the listener is dropped.
pub struct Listener {
    listener: UnixListener,
    config: ListenerConfig,
    identity: (u64, u64),
}

impl Listener {
    /// Binds the socket and enforces its ownership, mode and admission set.
    ///
    /// # Errors
    ///
    /// Refuses a relative or already-present path, a directory the daemon does not own, a
    /// world-reachable mode, an empty admission set, and a zero deadline.
    pub fn bind(config: ListenerConfig) -> Result<Self, ListenerError> {
        if !config.endpoint.is_absolute()
            || config.endpoint.as_os_str().as_bytes().len() > 107
            || config.endpoint.as_os_str().as_bytes().contains(&0)
            || config.endpoint.as_os_str().as_bytes().ends_with(b"/")
            || config.endpoint.components().any(|part| !matches!(part, Component::RootDir | Component::Normal(_)))
        {
            return Err(ListenerError::RelativeEndpoint);
        }
        if config.mode & !0o660 != 0 || config.mode & 0o600 != 0o600 {
            return Err(ListenerError::ModeTooBroad);
        }
        if config.admitted_uids.is_empty() || config.admitted_uids.len() > 64
            || config.admitted_uids.iter().collect::<std::collections::BTreeSet<_>>().len() != config.admitted_uids.len()
        {
            return Err(ListenerError::NoAdmittedPeer);
        }
        if config.deadline.is_zero() {
            return Err(ListenerError::ZeroDeadline);
        }
        let parent = config
            .endpoint
            .parent()
            .ok_or(ListenerError::ParentUnowned)?;
        if parent == Path::new("/")
            || fs::canonicalize(parent).map_err(|_| ListenerError::ParentUnowned)? != parent
            || fs::metadata("/proc/self").map_err(|_| ListenerError::ParentUnowned)?.uid() != config.owner_uid
        {
            return Err(ListenerError::ParentUnowned);
        }
        validate(parent, config.owner_uid, config.owner_gid, true)?;
        match fs::symlink_metadata(&config.endpoint) {
            Ok(_) => return Err(ListenerError::EndpointExists),
            Err(error) if error.kind() == std::io::ErrorKind::NotFound => {},
            Err(_) => return Err(ListenerError::Bind),
        }
        let listener = UnixListener::bind(&config.endpoint).map_err(|_| ListenerError::Bind)?;
        let metadata = fs::symlink_metadata(&config.endpoint).map_err(|_| ListenerError::Permissions)?;
        let bound = Self { listener, identity: (metadata.dev(), metadata.ino()), config };
        if metadata.gid() != bound.config.owner_gid {
            std::os::unix::fs::chown(&bound.config.endpoint, None, Some(bound.config.owner_gid))
                .map_err(|_| ListenerError::Permissions)?;
        }
        fs::set_permissions(&bound.config.endpoint, fs::Permissions::from_mode(bound.config.mode))
            .map_err(|_| ListenerError::Permissions)?;
        validate(&bound.config.endpoint, bound.config.owner_uid, bound.config.owner_gid, false)?;
        Ok(bound)
    }

    /// Serves admitted connections one at a time against the bound daemon session.
    ///
    /// # Errors
    ///
    /// Returns an accept failure. A refused peer and a transport failure on one connection
    /// close that connection without stopping the listener.
    pub fn serve<B: ToolBoundary>(&self, session: &mut Session<B>) -> Result<(), ListenerError> {
        loop {
            let (stream, _) = self.listener.accept().map_err(|_| ListenerError::Accept)?;
            let Ok(credentials) = rustix::net::sockopt::socket_peercred(&stream) else {
                continue;
            };
            if !self
                .config
                .admitted_uids
                .contains(&credentials.uid.as_raw())
            {
                continue;
            }
            if stream.set_read_timeout(Some(self.config.deadline)).is_err()
                || stream
                    .set_write_timeout(Some(self.config.deadline))
                    .is_err()
            {
                continue;
            }
            let Ok(writable) = stream.try_clone() else {
                continue;
            };
            let mut reader = BufReader::new(stream);
            let mut writer = writable;
            let _ = session.serve(&mut reader, &mut writer);
        }
    }
}

impl Drop for Listener {
    fn drop(&mut self) {
        if fs::symlink_metadata(&self.config.endpoint).is_ok_and(|metadata| {
            metadata.file_type().is_socket() && (metadata.dev(), metadata.ino()) == self.identity
        }) {
            let _ = fs::remove_file(&self.config.endpoint);
        }
    }
}

fn validate(path: &Path, uid: u32, gid: u32, directory: bool) -> Result<(), ListenerError> {
    let metadata = fs::symlink_metadata(path).map_err(|_| ListenerError::ParentUnowned)?;
    if metadata.file_type().is_symlink()
        || (directory && !metadata.is_dir())
        || (!directory && !metadata.file_type().is_socket())
        || metadata.uid() != uid
        || metadata.gid() != gid
        || metadata.mode() & 0o007 != 0
        || (directory && metadata.mode() & 0o020 != 0)
    {
        return Err(if directory {
            ListenerError::ParentUnowned
        } else {
            ListenerError::Permissions
        });
    }
    Ok(())
}
