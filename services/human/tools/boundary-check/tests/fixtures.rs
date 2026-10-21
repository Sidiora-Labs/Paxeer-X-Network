use std::fs;
use std::path::{Path, PathBuf};
use std::sync::atomic::{AtomicUsize, Ordering};

use layerx_human_boundary_check::{
    human_boundary_policy, human_dependency_policy, human_payload_authority_gate,
    human_payload_bundle_gate,
};

static NEXT: AtomicUsize = AtomicUsize::new(0);

struct Fixture(PathBuf);

impl Fixture {
    fn new() -> Self {
        let id = NEXT.fetch_add(1, Ordering::Relaxed);
        let path = std::env::temp_dir()
            .join(format!("layerx-human-boundary-{}-{id}", std::process::id()));
        fs::create_dir_all(path.join("sample/src"))
            .unwrap_or_else(|error| panic!("create fixture: {error}"));
        fs::write(
            path.join("sample/Cargo.toml"),
            "[package]\nname = \"sample\"\nversion = \"0.1.0\"\n",
        )
        .unwrap_or_else(|error| panic!("write manifest: {error}"));
        fs::write(path.join("sample/src/lib.rs"), "pub struct Value(u64);\n")
            .unwrap_or_else(|error| panic!("write source: {error}"));
        Self(path)
    }

    fn path(&self) -> &Path {
        &self.0
    }

    fn write(&self, relative: &str, body: &str) {
        let path = self.0.join(relative);
        if let Some(parent) = path.parent() {
            fs::create_dir_all(parent).unwrap_or_else(|error| panic!("create parent: {error}"));
        }
        fs::write(path, body).unwrap_or_else(|error| panic!("write fixture file: {error}"));
    }
}

impl Drop for Fixture {
    fn drop(&mut self) {
        let _ = fs::remove_dir_all(&self.0);
    }
}

#[test]
fn clean_tree_passes_every_policy() {
    let fixture = Fixture::new();
    assert_eq!(human_dependency_policy(fixture.path()), Ok(()));
    assert_eq!(human_boundary_policy(fixture.path()), Ok(()));
    assert_eq!(human_payload_authority_gate(fixture.path()), Ok(()));
}

#[test]
fn sqlite_dependency_is_rejected() {
    let fixture = Fixture::new();
    fixture.write("sample/Cargo.toml", "[dependencies]\nrusqlite = \"1\"\n");
    assert!(human_dependency_policy(fixture.path()).is_err());
}

#[test]
fn node_private_paths_are_rejected() {
    let fixture = Fixture::new();
    fixture.write(
        "sample/src/lib.rs",
        "const DATABASE: &str = \"projection.db\";\n",
    );
    assert!(human_boundary_policy(fixture.path()).is_err());
}

#[test]
fn ambient_clock_reads_are_rejected() {
    let fixture = Fixture::new();
    fixture.write(
        "sample/src/lib.rs",
        "fn resolve() { let _ = std::time::SystemTime::now(); }\n",
    );
    assert!(human_boundary_policy(fixture.path()).is_err());
}

#[test]
fn service_crate_wire_dependency_is_rejected() {
    let fixture = Fixture::new();
    fixture.write(
        "layerx-human-service/Cargo.toml",
        "[package]\nname = \"layerx-human-service\"\n[dependencies]\nlayerx-wire = { path = \"../wire\" }\n",
    );
    let violations = human_payload_authority_gate(fixture.path())
        .err()
        .unwrap_or_else(|| panic!("wire dependency in a service crate must be caught"));
    assert!(violations
        .iter()
        .any(|violation| violation.rule == "payload-authority-dependency"));
}

#[test]
fn workspace_inherited_wire_dependency_is_rejected() {
    let fixture = Fixture::new();
    fixture.write(
        "layerx-explorer-index/Cargo.toml",
        "[dependencies]\nlayerx-wire.workspace = true\n",
    );
    assert!(human_payload_authority_gate(fixture.path()).is_err());
}

#[test]
fn encoding_entry_point_invocation_is_rejected() {
    let fixture = Fixture::new();
    fixture.write(
        "sample/src/lib.rs",
        "pub fn build() -> Vec<u8> { layerx_wire::encode::payload(&[]) }\n",
    );
    let violations = human_payload_authority_gate(fixture.path())
        .err()
        .unwrap_or_else(|| panic!("encode entry-point invocation must be caught"));
    assert!(violations
        .iter()
        .any(|violation| violation.rule == "payload-encoding-entry-point"));
}

#[test]
fn the_payload_authority_itself_may_depend_on_wire() {
    let fixture = Fixture::new();
    fixture.write(
        "layerx-intents/Cargo.toml",
        "[package]\nname = \"layerx-intents\"\n[dependencies]\nlayerx-wire = { path = \"../wire\" }\n",
    );
    fixture.write(
        "layerx-intents/src/compile.rs",
        "pub fn compile() -> Vec<u8> { layerx_wire::encode::payload(&[]) }\n",
    );
    assert_eq!(human_payload_authority_gate(fixture.path()), Ok(()));
}

#[test]
fn web_bundle_payload_marker_is_rejected() {
    let fixture = Fixture::new();
    fixture.write(
        "bundle/static/chunks/main-app.js",
        "export function encode(){return \"layerx_wire\";}",
    );
    let violations = human_payload_bundle_gate(&fixture.path().join("bundle"))
        .err()
        .unwrap_or_else(|| panic!("payload marker in the web bundle must be caught"));
    assert!(violations
        .iter()
        .any(|violation| violation.rule == "payload-encoding-in-bundle"));
}

#[test]
fn clean_web_bundle_passes() {
    let fixture = Fixture::new();
    fixture.write(
        "bundle/static/chunks/main-app.js",
        "export function render(){return \"journeys and receipts\";}",
    );
    assert_eq!(
        human_payload_bundle_gate(&fixture.path().join("bundle")),
        Ok(())
    );
}

#[test]
fn missing_web_bundle_fails_loudly() {
    let fixture = Fixture::new();
    assert!(human_payload_bundle_gate(&fixture.path().join("absent")).is_err());
}
