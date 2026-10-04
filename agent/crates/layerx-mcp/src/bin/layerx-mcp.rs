//! Daemon-bound model context protocol server for the `LayerX` agent plane.

use std::env;
use std::fs;
use std::io::ErrorKind;
use std::os::unix::fs::{FileTypeExt as _, MetadataExt as _};
use std::os::unix::net::UnixStream;
use std::path::PathBuf;
use std::process::ExitCode;

use layerx_mcp::binding::Binding;
use layerx_mcp::listener::{Listener, ListenerConfig};

const USAGE: &str = "usage: layerx-mcp <absolute path to the daemon binding document>";

fn binding_path() -> Result<PathBuf, String> {
    let mut arguments = env::args_os().skip(1);
    let path = arguments.next().ok_or_else(|| USAGE.to_owned())?;
    if arguments.next().is_some() {
        return Err(USAGE.to_owned());
    }
    Ok(PathBuf::from(path))
}

/// Removes the socket a previous run of this server left behind when it was stopped without
/// unwinding, so a restart can bind again. Only a socket inside the daemon-owned directory,
/// owned by the declared owner, that no process accepts on is removed; a live socket and any
/// other file stay in place and the bind refuses them.
fn clear_stale_socket(configuration: &ListenerConfig) -> Result<(), String> {
    let Ok(metadata) = fs::symlink_metadata(&configuration.endpoint) else {
        return Ok(());
    };
    let Some(parent) = configuration.endpoint.parent() else {
        return Ok(());
    };
    let Ok(directory) = fs::symlink_metadata(parent) else {
        return Ok(());
    };
    if !directory.is_dir()
        || directory.uid() != configuration.owner_uid
        || directory.gid() != configuration.owner_gid
        || directory.mode() & 0o027 != 0
        || fs::canonicalize(parent).map_or(true, |canonical| canonical != parent)
        || fs::metadata("/proc/self")
            .map_or(true, |process| process.uid() != configuration.owner_uid)
        || !metadata.file_type().is_socket()
        || metadata.uid() != configuration.owner_uid
        || metadata.gid() != configuration.owner_gid
        || metadata.mode() & 0o777 != configuration.mode
    {
        return Ok(());
    }
    match UnixStream::connect(&configuration.endpoint) {
        Ok(_) => Err("another server already serves the protocol socket".to_owned()),
        Err(error) if error.kind() == ErrorKind::ConnectionRefused => {
            let current = fs::symlink_metadata(&configuration.endpoint)
                .map_err(|_| "the stale protocol socket changed".to_owned())?;
            if current.dev() != metadata.dev() || current.ino() != metadata.ino() {
                return Err("the stale protocol socket changed".to_owned());
            }
            fs::remove_file(&configuration.endpoint).map_err(|error| {
                format!(
                    "the stale protocol socket could not be removed: {}",
                    error.kind()
                )
            })
        }
        Err(_) => Ok(()),
    }
}

fn run() -> Result<(), String> {
    let path = binding_path()?;
    let binding = Binding::open(&path).map_err(|error| error.detail())?;
    let mut session = binding
        .open_daemon_client()
        .map_err(|error| error.detail())?;
    let Some(configuration) = binding.listener().cloned() else {
        return session.serve(&mut std::io::stdin().lock(), &mut std::io::stdout().lock());
    };
    clear_stale_socket(&configuration)?;
    let listener = Listener::bind(configuration)
        .map_err(|error| format!("the protocol socket was refused: {}", error.detail()))?;
    listener
        .serve_daemon_client(&mut session)
        .map_err(|error| format!("the protocol socket stopped: {}", error.detail()))
}

fn main() -> ExitCode {
    match run() {
        Ok(()) => ExitCode::SUCCESS,
        Err(error) => {
            eprintln!("layerx-mcp: {error}");
            ExitCode::FAILURE
        }
    }
}
