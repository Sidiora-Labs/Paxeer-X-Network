use std::path::Path;
use std::process::Command;

#[test]
fn parity_harness_passes_against_the_built_c_reference() {
    let Some(reference) = std::env::var_os("LAYERX_C_REFERENCE") else {
        panic!("LAYERX_C_REFERENCE must name the built core driver");
    };
    let repository = Path::new(env!("CARGO_MANIFEST_DIR")).join("../../../..");
    let output = Command::new(env!("CARGO_BIN_EXE_agent-wire-differential"))
        .env("LAYERX_REPOSITORY_ROOT", repository)
        .env("LAYERX_C_REFERENCE", reference)
        .output();
    let Ok(output) = output else {
        panic!("differential harness failed to start");
    };
    assert!(
        output.status.success(),
        "{}",
        String::from_utf8_lossy(&output.stderr)
    );
    assert!(String::from_utf8_lossy(&output.stdout).contains("wire parity passed:"));
}
