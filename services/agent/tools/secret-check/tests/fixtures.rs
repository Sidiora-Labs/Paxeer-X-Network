use std::fs;
use std::path::{Path, PathBuf};
use std::process::Command;
use std::sync::atomic::{AtomicUsize, Ordering};

static NEXT: AtomicUsize = AtomicUsize::new(0);

struct Fixture(PathBuf);

impl Fixture {
    fn new(source: &str) -> Self {
        let id = NEXT.fetch_add(1, Ordering::Relaxed);
        let path =
            std::env::temp_dir().join(format!("layerx-secret-check-{}-{id}", std::process::id()));
        fs::create_dir_all(path.join("crates/sample/src")).expect("create fixture");
        fs::write(path.join("crates/sample/src/lib.rs"), source).expect("write fixture");
        Self(path)
    }

    fn path(&self) -> &Path {
        &self.0
    }

    fn run(&self) -> std::process::Output {
        Command::new(env!("CARGO_BIN_EXE_agent-secret-check"))
            .arg(self.path())
            .output()
            .expect("run secret check")
    }
}

impl Drop for Fixture {
    fn drop(&mut self) {
        let _ = fs::remove_dir_all(&self.0);
    }
}

fn rejects(source: &str, rule: &str) {
    let output = Fixture::new(source).run();
    assert!(!output.status.success());
    assert!(String::from_utf8_lossy(&output.stderr).contains(rule));
}

#[test]
fn protected_secret_storage_passes() {
    let output =
        Fixture::new("pub struct Secret<T>(T);\npub struct Holder { seed: Secret<[u8; 32]> }\n")
            .run();
    assert!(output.status.success());
}

#[test]
fn derived_debug_and_serialization_fail() {
    rejects(
        "#[derive(Debug, Serialize)]\npub struct Holder { seed: Secret<[u8; 32]> }\n",
        "secret-derived-output",
    );
}

#[test]
fn raw_secret_storage_fails() {
    rejects(
        "pub struct Holder { private_key: [u8; 32] }\n",
        "unwrapped-secret-field",
    );
    rejects(
        "use zeroize::Zeroizing; pub struct Holder(Zeroizing<Vec<u8>>);\n",
        "raw-zeroizing-secret",
    );
}

#[test]
fn logs_metrics_traces_panics_and_serializers_fail() {
    for source in [
        "fn leak(secret: &[u8]) { tracing::info!(?secret); }",
        "fn leak(api_token: &[u8]) { metrics::describe_counter!(api_token); }",
        "fn leak(password: &[u8]) { panic!(\"{password:?}\"); }",
        "fn leak(seed: &[u8]) { let _ = serde_json::to_vec(seed); }",
    ] {
        rejects(source, "secret-output-surface");
    }
}

#[test]
fn secret_bearing_errors_fail() {
    rejects(
        "pub enum LoadError { Secret(String), Io }\n",
        "secret-bearing-error",
    );
}

#[test]
fn direct_output_requires_the_redaction_boundary() {
    rejects(
        "fn leak(value: &[u8]) { tracing::info!(?value); }",
        "unwrapped-output-surface",
    );
    let output = Fixture::new(
        "struct Redacted; fn emit(value: Redacted) { tracing::info!(?Redacted); let _ = value; }\n",
    )
    .run();
    assert!(output.status.success());
}
