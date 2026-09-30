mod support;

use layerx_client::runtime_clock::RuntimeClock;
use layerx_human_security_provider::{serve, Config};
use layerx_types::clock::Deadline;
use std::fs::{self, OpenOptions};
use std::io::Write as _;
use std::os::unix::fs::{DirBuilderExt as _, OpenOptionsExt as _};
use std::path::{Path, PathBuf};
use std::process::{Command, Output};
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::Arc;
use std::time::Duration;

struct Directory(PathBuf);

impl Drop for Directory {
    fn drop(&mut self) {
        let _ = fs::remove_dir_all(&self.0);
    }
}

fn directory(name: &str) -> Directory {
    let root = std::env::temp_dir().join(format!("lxsp-probe-{name}-{}", std::process::id()));
    let _ = fs::remove_dir_all(&root);
    fs::DirBuilder::new().mode(0o700).create(&root).unwrap();
    Directory(root)
}

fn probe(socket: &Path) -> Output {
    Command::new(env!("CARGO_BIN_EXE_layerx-human-security-provider"))
        .arg("probe")
        .env("LAYERX_HUMAN_SECURITY_PROVIDER_SOCKET", socket)
        .env("LAYERX_HUMAN_SECURITY_PROVIDER_DEADLINE_SECONDS", "5")
        .output()
        .unwrap()
}

#[test]
fn probe_exits_zero_only_on_the_serving_providers_healthy_answer() {
    let directory = directory("serve");
    let socket = directory.0.join("security.sock");
    let trust = directory.0.join("trust");

    let absent = probe(&socket);
    assert_eq!(absent.status.code(), Some(1));
    assert!(absent.stdout.is_empty());

    let (history, _) = support::evidence();
    let mut file = OpenOptions::new()
        .write(true)
        .create_new(true)
        .mode(0o600)
        .open(&trust)
        .unwrap();
    file.write_all(&history).unwrap();
    file.sync_all().unwrap();

    let stop = Arc::new(AtomicBool::new(false));
    let config = Config {
        state_root: directory.0.join("state"),
        trust_history: trust,
        socket: socket.clone(),
        allowed_uid: rustix::process::geteuid().as_raw(),
        deadline: Duration::from_secs(1),
    };
    let shutdown = Arc::clone(&stop);
    let clock = RuntimeClock::from_environment().unwrap();
    let service_clock = clock.clone();
    let server = std::thread::spawn(move || serve(config, shutdown, service_clock));
    let mut deadline = Deadline::start(clock.as_ref(), Duration::from_secs(5)).unwrap();
    while !socket.exists() {
        assert!(
            !server.is_finished(),
            "security provider exited before binding"
        );
        assert!(!deadline.remaining(clock.as_ref()).unwrap().is_zero());
        std::thread::sleep(Duration::from_millis(10));
    }

    let ready = probe(&socket);
    assert_eq!(ready.status.code(), Some(0));
    assert!(ready.stdout.is_empty());

    stop.store(true, Ordering::Relaxed);
    server.join().unwrap().unwrap();

    let stopped = probe(&socket);
    assert_eq!(stopped.status.code(), Some(1));
}

#[test]
fn probe_refuses_a_missing_or_relative_socket() {
    let directory = directory("missing");
    let missing = probe(&directory.0.join("absent.sock"));
    assert_eq!(missing.status.code(), Some(1));
    assert!(missing.stdout.is_empty());

    let unset = Command::new(env!("CARGO_BIN_EXE_layerx-human-security-provider"))
        .arg("probe")
        .env_remove("LAYERX_HUMAN_SECURITY_PROVIDER_SOCKET")
        .output()
        .unwrap();
    assert_eq!(unset.status.code(), Some(1));

    let relative = Command::new(env!("CARGO_BIN_EXE_layerx-human-security-provider"))
        .arg("probe")
        .env("LAYERX_HUMAN_SECURITY_PROVIDER_SOCKET", "security.sock")
        .output()
        .unwrap();
    assert_eq!(relative.status.code(), Some(1));
}
