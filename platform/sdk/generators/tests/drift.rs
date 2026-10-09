#[allow(dead_code)]
#[path = "../src/main.rs"]
mod pipeline;

use std::fs;
use std::path::{Path, PathBuf};
use std::process::Command;
use std::sync::atomic::{AtomicU64, Ordering};

use pipeline::{capture, check, parse_lock, render, write_lock};

static NEXT_DIRECTORY: AtomicU64 = AtomicU64::new(1);

fn directory(label: &str) -> PathBuf {
    let sequence = NEXT_DIRECTORY.fetch_add(1, Ordering::Relaxed);
    std::env::temp_dir().join(format!(
        "layerx-platform-sdkgen-{label}-{}-{sequence}",
        std::process::id()
    ))
}

fn place(root: &Path, relative: &str, contents: &str) {
    let path = root.join(relative);
    let parent = path
        .parent()
        .unwrap_or_else(|| panic!("no parent for {relative}"));
    fs::create_dir_all(parent).unwrap_or_else(|error| panic!("create {relative}: {error}"));
    fs::write(&path, contents).unwrap_or_else(|error| panic!("write {relative}: {error}"));
}

fn repo_fixture(label: &str) -> PathBuf {
    let root = directory(label);
    fs::create_dir_all(&root).unwrap_or_else(|error| panic!("create fixture: {error}"));
    let status = Command::new("git")
        .arg("-C")
        .arg(&root)
        .args(["init", "-q"])
        .status()
        .unwrap_or_else(|error| panic!("git init fixture: {error}"));
    assert!(status.success(), "git init fixture failed");
    place(
        &root,
        "platform/sdk/generators/Cargo.toml",
        include_str!("../Cargo.toml"),
    );
    place(
        &root,
        "platform/sdk/generators/src/main.rs",
        include_str!("../src/main.rs"),
    );
    place(
        &root,
        "platform/sdk/generators/generate_jvm.py",
        include_str!("../generate_jvm.py"),
    );
    place(
        &root,
        "platform/sdk/generators/generate_portable.py",
        include_str!("../generate_portable.py"),
    );
    place(
        &root,
        "agent/tools/sdk-gen/Cargo.toml",
        "[package]\nname = \"layerx-sdk-gen\"\n",
    );
    place(&root, "agent/tools/sdk-gen/Cargo.lock", "version = 4\n");
    place(&root, "agent/tools/sdk-gen/src/main.rs", "fn main() {}\n");
    place(
        &root,
        "agent/tools/sdk-gen/templates/typescript.tpl",
        "// typescript template\n",
    );
    place(
        &root,
        "human/tools/api-gen/Cargo.toml",
        "[package]\nname = \"layerx-human-api-gen\"\n",
    );
    place(&root, "human/tools/api-gen/Cargo.lock", "version = 4\n");
    place(&root, "human/tools/api-gen/src/main.rs", "fn main() {}\n");
    place(
        &root,
        "programs/sdk/rust/src/abi_policy.rs",
        "pub const ABI_V1_VERSION: u16 = 1;\npub const ABI_V2_VERSION: u16 = 2;\npub const ABI_V3_VERSION: u16 = 3;\npub const ABI_V4_VERSION: u16 = 4;\npub const ABI_V5_VERSION: u16 = 5;\n",
    );
    place(
        &root,
        "programs/crates/layerx-programs-runtime/src/terminal.rs",
        "const EXECUTION_V4: &[u8] = b\"LXP/program-execution/v4\\0\";\nconst EXECUTION_V5: &[u8] = b\"LXP/program-execution/v5\\0\";\n",
    );
    place(
        &root,
        "platform/sdk/generators/generate_lifecycle.py",
        include_str!("../generate_lifecycle.py"),
    );
    place(
        &root,
        "platform/sdk/generators/receipt.kvx",
        "[receipt]\nprogram_outcome = \"optional\"\nprograms_module_id = 9\nprogram_outcome_tags = [\"50524731\", \"50524732\", \"50524733\", \"50524734\"]\nrequired_nonzero = [\"global-sequence\", \"module-id\", \"module-version\", \"timestamp\", \"activity-id\", \"resulting-state-root\"]\nfailure_checks = [\"decode\", \"canonical-encoding\", \"receipt-shape\", \"missing-signature\", \"protocol-version\", \"result-code\", \"operation\", \"activity-id\", \"global-sequence\", \"module-id\", \"module-version\", \"timestamp\", \"batch-id\", \"asset\", \"previous-state-root\", \"resulting-state-root\", \"debit-balance\", \"credit-balance\", \"program-outcome\", \"sequencer-signature\"]\n",
    );
    place(&root, "agent/schema/agent-api/v1.kvx", "[schema]\nincludes = [\"errors.kvx\",\"approval.kvx\",\"stream.kvx\",\"programs.kvx\"]\n\n[scalar.Amount]\nrust = \"u128\"\n");
    place(&root, "agent/schema/agent-api/errors.kvx", "[operation.agent.register]\nrequest = \"Register\"\nresponse = \"Registered\"\n\n[mutation.agent.register]\nenvelope = \"IdempotentMutation\"\n\n[type.ErrorClass]\nvariants = [\"TransportFailure\"]\n\n[type.Retriability]\nvariants = [\"Terminal\"]\n");
    place(
        &root,
        "agent/schema/agent-api/approval.kvx",
        "[type.ApprovalLifecycleEvent]\nvariants = [\"Created\"]\n\n[type.ApprovalState]\nvariants = [\"Held\"]\n\n[type.ApprovalDecisionOutcome]\nvariants = [\"Granted\"]\n",
    );
    place(
        &root,
        "agent/schema/agent-api/stream.kvx",
        "[type.Delivery]\nvariants = [\"Event\"]\n",
    );
    place(
        &root,
        "agent/schema/agent-api/programs.kvx",
        "[operation.program.discover]\nrequest = \"ProgramSelector\"\nresponse = \"VerifiedProgramDiscovery\"\n\n[operation.program.interface]\nrequest = \"ProgramSelector\"\nresponse = \"VerifiedProgramInterface\"\n\n[operation.program.simulate]\nrequest = \"ProgramCallRequest\"\nresponse = \"ProgramSimulation\"\n\n[operation.program.call]\nrequest = \"ProgramCallRequest\"\nrequired = [\"idempotency_key\"]\nresponse = \"ProgramSubmission\"\n\n[operation.program.receipt]\nrequest = \"ProgramReceiptSelector\"\nresponse = \"ProgramSubmission\"\n\n[operation.program.activity]\nrequest = \"ProgramActivitySelector\"\nresponse = \"ProgramSubmission\"\n\n[type.ProgramGuestAbi]\nvariants = [\"ABI_V1_VERSION\",\"ABI_V2_VERSION\",\"ABI_V3_VERSION\",\"ABI_V4_VERSION\",\"ABI_V5_VERSION\"]\nwire_values = [\"1\",\"2\",\"3\",\"4\",\"5\"]\nsource_policy = \"programs/sdk/rust/src/abi_policy.rs\"\ncapability_encoding = [\"V1\",\"V2\",\"V2\",\"V2\",\"V2\"]\n\n[type.ProgramExecutionV4]\nencoding_version = 4\ndomain = \"LXP/program-execution/v4\"\ndomain_terminator_hex = \"00\"\nallowed_guest_abis = [\"ABI_V2_VERSION\"]\n\n[type.ProgramExecutionV5]\nencoding_version = 5\ndomain = \"LXP/program-execution/v5\"\ndomain_terminator_hex = \"00\"\nallowed_guest_abis = [\"ABI_V3_VERSION\",\"ABI_V4_VERSION\"]\n",
    );
    place(
        &root,
        "agent/schema/agent-api/golden/version-request.hex",
        "00ff\n",
    );
    place(&root, "human/schema/human-api/v1.kvx", "[schema]\nincludes = [\"errors.kvx\",\"journeys.kvx\",\"stream.kvx\"]\n\n[operation.version]\nmethod = \"GET\"\npath = \"/v1/version\"\nrequest = \"Empty\"\nresponse = \"VersionInfo\"\n");
    place(
        &root,
        "human/schema/human-api/errors.kvx",
        "[type.ErrorCode]\nvariants = [\"unavailable\"]\n\n[type.Retriability]\nvariants = [\"retriable\"]\n",
    );
    place(
        &root,
        "human/schema/human-api/journeys.kvx",
        "[type.JourneyKind]\nvariants = [\"onboarding\"]\n\n[type.JourneyState]\nvariants = [\"processing\"]\n\n[type.VerificationLevel]\nvariants = [\"unverified\"]\n\n[type.ApprovalState]\nvariants = [\"pending\"]\n",
    );
    place(
        &root,
        "human/schema/human-api/stream.kvx",
        "[type.StreamEventKind]\nvariants = [\"journey-progress\"]\n",
    );
    place(
        &root,
        "human/schema/human-api/golden/account.create.request.json",
        "{}\n",
    );
    place_generated_fixture(&root);
    root
}

fn place_generated_fixture(root: &Path) {
    place(
        root,
        "agent/sdk/typescript/src/generated/client.ts",
        "export const generated = true;\n",
    );
    place(
        root,
        "agent/sdk/typescript/src/generated/guarantees.md",
        "guarantees\n",
    );
    place(
        root,
        "agent/sdk/python/layerx_sdk/generated/client.py",
        "GENERATED = True\n",
    );
    place(
        root,
        "agent/sdk/python/layerx_sdk/generated/client.pyi",
        "GENERATED: bool\n",
    );
    place(
        root,
        "agent/sdk/python/layerx_sdk/generated/guarantees.md",
        "guarantees\n",
    );
    place(root, "agent/sdk/COMPATIBILITY.md", "compatibility\n");
    place(
        root,
        "human/apps/web/src/api/generated/index.ts",
        "export const humanApi = true;\n",
    );
    place(
        root,
        "agent/crates/layerx-agent-api/src/operation_generated.rs",
        "// generated Rust operations\n",
    );
    place(
        root,
        "agent/crates/layerx-agent-api/src/generated.rs",
        "// generated Rust contract\n",
    );
    place(
        root,
        "agent/crates/layerx-sdk/src/mirror_generated.rs",
        "// generated Rust mirror\n",
    );
    place(root, "platform/sdk/go/generated.go", "package layerx\n");
    place(
        root,
        "platform/sdk/go/mirror_generated.go",
        "package layerx\n",
    );
    for relative in pipeline::JVM_FILES {
        place(root, &format!("platform/sdk/jvm/{relative}"), "jvm\n");
    }
    place(
        root,
        "platform/sdk/conformance/jvm.kvx",
        "[sdk]\nname = \"jvm\"\n",
    );
    place(root, "platform/sdk/conformance/run-jvm.sh", "#!/bin/sh\n");
    place(root, "platform/sdk/conformance/mirror-v2.json", "{}\n");
    place(
        root,
        "platform/sdk/schema/mirror-v2.kvx",
        "[schema]\nversion = 2\n",
    );
    place(
        root,
        "platform/sdk/swift/Sources/LayerXSDK/Generated/OperationCatalog.swift",
        "// generated Swift\n",
    );
    place(
        root,
        "platform/sdk/swift/Sources/LayerXSDK/Generated/MirrorSchema.swift",
        "// generated Swift mirror\n",
    );
    place(
        root,
        "platform/sdk/dotnet/Generated/OperationCatalog.cs",
        "// generated C#\n",
    );
    place(
        root,
        "platform/sdk/dotnet/Generated/MirrorSchema.cs",
        "// generated C# mirror\n",
    );
    place(
        root,
        "platform/sdk/conformance/operations.json",
        "{\"schema\":1,\"operations\":[]}\n",
    );
    place(
        root,
        "platform/sdk/generators/receipt.kvx",
        include_str!("../receipt.kvx"),
    );
}

#[test]
fn lifecycle_sources_participate_in_normal_drift_check() {
    let root = repo_fixture("lifecycle-drift");
    let lock = lock_path(&root);
    write_lock(&root, &lock).unwrap_or_else(|error| panic!("generate lifecycle: {error}"));
    check(&root, &lock).unwrap_or_else(|error| panic!("initial lifecycle check: {error}"));
    place(
        &root,
        "platform/sdk/go/program_lifecycle_generated.go",
        "package layerx\n",
    );
    let error = check(&root, &lock)
        .err()
        .unwrap_or_else(|| panic!("lifecycle drift must fail the normal gate"));
    assert!(error.contains("Lifecycle SDK drift"), "{error}");
    fs::remove_dir_all(root).unwrap_or_else(|error| panic!("remove lifecycle fixture: {error}"));
}

fn lock_path(root: &Path) -> PathBuf {
    root.join("platform/sdk/pipeline.kvx")
}

fn generate(root: &Path) {
    write_lock(root, &lock_path(root)).unwrap_or_else(|error| panic!("write lock: {error}"));
}

fn expect_failure(root: &Path, needle: &str) {
    let error = check(root, &lock_path(root))
        .err()
        .unwrap_or_else(|| panic!("drift gate passed but expected failure about {needle}"));
    assert!(
        error.contains(needle),
        "expected failure mentioning {needle}, got: {error}"
    );
}

fn cleanup(root: &Path) {
    fs::remove_dir_all(root).unwrap_or_else(|error| panic!("cleanup: {error}"));
}

#[test]
fn freshly_generated_pipeline_passes_the_gate() {
    let root = repo_fixture("fresh");
    generate(&root);
    check(&root, &lock_path(&root)).unwrap_or_else(|error| panic!("gate failed: {error}"));
    cleanup(&root);
}

#[test]
fn rust_operation_catalogue_is_derived_from_programs_schema() {
    let root = repo_fixture("rust-programs");
    generate(&root);
    let generated =
        fs::read_to_string(root.join("agent/crates/layerx-agent-api/src/operation_generated.rs"))
            .unwrap_or_else(|error| panic!("read generated Rust operation catalogue: {error}"));
    assert!(generated.contains("ProgramDiscover"));
    assert!(generated.contains("ProgramInterface"));
    assert!(generated.contains("ProgramSimulate"));
    assert!(generated.contains("ProgramCall"));
    assert!(generated.contains("ProgramReceipt"));
    assert!(generated.contains("ProgramActivity"));
    let mutation = generated
        .split("pub const fn mutating")
        .nth(1)
        .unwrap_or_else(|| panic!("generated mutation classifier missing"));
    assert!(mutation.contains("Self::ProgramCall"));
    assert!(!mutation.contains("Self::ProgramSimulate"));
    cleanup(&root);
}

#[test]
fn lock_round_trips_through_render_and_parse() {
    let root = repo_fixture("roundtrip");
    generate(&root);
    let live = capture(&root).unwrap_or_else(|error| panic!("capture: {error}"));
    let text = render(&live).unwrap_or_else(|error| panic!("render: {error}"));
    let parsed = parse_lock(&text).unwrap_or_else(|error| panic!("parse: {error}"));
    assert_eq!(parsed, live);
    cleanup(&root);
}

#[test]
fn missing_lock_fails_the_gate() {
    let root = repo_fixture("missing-lock");
    expect_failure(&root, "pipeline lock missing");
    cleanup(&root);
}

#[test]
fn schema_edit_fails_the_gate_as_stale() {
    let root = repo_fixture("stale-schema");
    generate(&root);
    place(
        &root,
        "agent/schema/agent-api/v1.kvx",
        "[schema]\nversion = \"2\"\n",
    );
    expect_failure(&root, "stale generated SDKs: schema agent-api");
    cleanup(&root);
}

#[test]
fn human_schema_edit_fails_the_gate_as_stale() {
    let root = repo_fixture("stale-human-schema");
    generate(&root);
    place(
        &root,
        "human/schema/human-api/golden/account.create.request.json",
        "{\"edited\":true}\n",
    );
    expect_failure(&root, "stale generated SDKs: schema human-api");
    cleanup(&root);
}

#[test]
fn hand_edited_typescript_output_fails_the_gate() {
    let root = repo_fixture("edit-ts");
    generate(&root);
    place(
        &root,
        "agent/sdk/typescript/src/generated/client.ts",
        "export const generated = false;\n",
    );
    expect_failure(&root, "agent/sdk/typescript/src/generated/client.ts");
    cleanup(&root);
}

#[test]
fn hand_edited_python_output_fails_the_gate() {
    let root = repo_fixture("edit-py");
    generate(&root);
    place(
        &root,
        "agent/sdk/python/layerx_sdk/generated/client.py",
        "GENERATED = False\n",
    );
    expect_failure(&root, "agent/sdk/python/layerx_sdk/generated/client.py");
    cleanup(&root);
}

#[test]
fn deleted_generated_file_fails_the_gate() {
    let root = repo_fixture("deleted");
    generate(&root);
    fs::remove_file(root.join("human/apps/web/src/api/generated/index.ts"))
        .unwrap_or_else(|error| panic!("remove: {error}"));
    expect_failure(&root, "human/apps/web/src/api/generated");
    cleanup(&root);
}

#[test]
fn untracked_file_in_a_generated_root_fails_the_gate() {
    let root = repo_fixture("untracked");
    generate(&root);
    place(
        &root,
        "agent/sdk/typescript/src/generated/extra.ts",
        "export const extra = true;\n",
    );
    expect_failure(&root, "untracked file in generated typescript root");
    cleanup(&root);
}

#[test]
fn lock_missing_an_output_fails_the_gate() {
    let root = repo_fixture("tampered-lock");
    generate(&root);
    let path = lock_path(&root);
    let text = fs::read_to_string(&path).unwrap_or_else(|error| panic!("read lock: {error}"));
    let tampered = if text.contains("\"platform-conformance\", ") {
        text.replace("\"platform-conformance\", ", "")
    } else {
        text.replace(", \"platform-conformance\"", "")
    };
    assert_ne!(tampered, text);
    fs::write(&path, tampered).unwrap_or_else(|error| panic!("tamper lock: {error}"));
    expect_failure(&root, "does not match the wired pipeline");
    cleanup(&root);
}

fn declare_handwritten(root: &Path, output: &str, relative: &str) {
    let path = lock_path(root);
    let mut text = fs::read_to_string(&path).unwrap_or_else(|error| panic!("read lock: {error}"));
    text.push_str(&format!(
        "\n[handwritten.{output}]\n\"{relative}\" = \"handwritten\"\n"
    ));
    fs::write(&path, text).unwrap_or_else(|error| panic!("declare handwritten: {error}"));
}

#[test]
fn undeclared_file_in_an_explicit_output_root_fails_the_gate() {
    let root = repo_fixture("explicit-untracked");
    generate(&root);
    place(
        &root,
        "platform/sdk/jvm/src/main/java/com/sidiora/layerx/sdk/Unlisted.java",
        "final class Unlisted {}\n",
    );
    expect_failure(
        &root,
        "untracked file in generated jvm root: platform/sdk/jvm/src/main/java/com/sidiora/layerx/sdk/Unlisted.java",
    );
    cleanup(&root);
}

#[test]
fn regeneration_refuses_an_unclassified_file() {
    let root = repo_fixture("explicit-unclassified");
    generate(&root);
    place(&root, "platform/sdk/go/client.go", "package layerx\n");
    let error = write_lock(&root, &lock_path(&root))
        .err()
        .unwrap_or_else(|| panic!("regeneration must refuse an unclassified file"));
    assert!(
        error.contains("unclassified file in generated go root: platform/sdk/go/client.go"),
        "{error}"
    );
    cleanup(&root);
}

#[test]
fn declared_handwritten_file_passes_and_survives_regeneration() {
    let root = repo_fixture("explicit-handwritten");
    generate(&root);
    place(&root, "platform/sdk/conformance/README.md", "conformance\n");
    expect_failure(
        &root,
        "untracked file in generated kvx root: platform/sdk/conformance/README.md",
    );
    declare_handwritten(&root, "platform-conformance", "README.md");
    check(&root, &lock_path(&root)).unwrap_or_else(|error| panic!("declared file: {error}"));
    generate(&root);
    let lock = fs::read_to_string(lock_path(&root))
        .unwrap_or_else(|error| panic!("read regenerated lock: {error}"));
    assert!(
        lock.contains("[handwritten.platform-conformance]\n\"README.md\" = \"handwritten\"\n"),
        "{lock}"
    );
    check(&root, &lock_path(&root)).unwrap_or_else(|error| panic!("regenerated: {error}"));
    place(
        &root,
        "platform/sdk/conformance/README.md",
        "edited freely\n",
    );
    check(&root, &lock_path(&root)).unwrap_or_else(|error| panic!("edited handwritten: {error}"));
    fs::remove_file(root.join("platform/sdk/conformance/README.md"))
        .unwrap_or_else(|error| panic!("remove handwritten: {error}"));
    expect_failure(
        &root,
        "declared handwritten file platform/sdk/conformance/README.md",
    );
    cleanup(&root);
}

#[test]
fn generated_file_declared_handwritten_fails_the_gate() {
    let root = repo_fixture("explicit-overlap");
    generate(&root);
    declare_handwritten(&root, "platform-jvm", "pom.xml");
    expect_failure(&root, "declared handwritten file platform/sdk/jvm/pom.xml");
    cleanup(&root);
}

#[test]
fn handwritten_declaration_for_an_unknown_output_fails_the_gate() {
    let root = repo_fixture("explicit-unknown");
    generate(&root);
    declare_handwritten(&root, "platform-unknown", "README.md");
    expect_failure(&root, "does not match the wired pipeline");
    cleanup(&root);
}

#[test]
fn ignored_build_output_is_not_a_shipped_file() {
    let root = repo_fixture("explicit-ignored");
    place(&root, ".gitignore", "/platform/sdk/jvm/target/\n");
    generate(&root);
    place(
        &root,
        "platform/sdk/jvm/target/classes/com/sidiora/layerx/sdk/PlatformSdk.class",
        "class\n",
    );
    check(&root, &lock_path(&root)).unwrap_or_else(|error| panic!("ignored output: {error}"));
    cleanup(&root);
}

#[test]
fn generator_edit_fails_the_gate_as_stale() {
    let root = repo_fixture("stale-generator");
    generate(&root);
    let lock =
        fs::read_to_string(lock_path(&root)).unwrap_or_else(|error| panic!("read lock: {error}"));
    assert!(lock.contains("[generator.platform-sdkgen]"), "{lock}");
    place(
        &root,
        "platform/sdk/generators/generate_portable.py",
        "raise SystemExit(1)\n",
    );
    expect_failure(&root, "stale generated SDKs: generator platform-sdkgen");
    cleanup(&root);
}

#[test]
fn receipt_contracts_admit_the_fifth_guest_abi_in_every_language() {
    let root = repo_fixture("guest-abi-five");
    generate(&root);
    for (relative, declaration, mentions) in [
        (
            "agent/crates/layerx-sdk/src/receipt_generated.rs",
            "pub const PROGRAM_ABI_V5: u16 = 5;",
            2,
        ),
        (
            "agent/sdk/typescript/src/generated/receipt.ts",
            "export const PROGRAM_ABI_V5 = 5;",
            2,
        ),
        (
            "agent/sdk/python/layerx_sdk/generated/receipt.py",
            "PROGRAM_ABI_V5 = 5",
            2,
        ),
        (
            "agent/sdk/python/layerx_sdk/generated/receipt.pyi",
            "PROGRAM_ABI_V5: int",
            1,
        ),
        (
            "platform/sdk/go/receipt_generated.go",
            "const ProgramAbiV5 uint16 = 5",
            2,
        ),
        (
            "platform/sdk/jvm/src/main/java/com/sidiora/layerx/sdk/verify/GeneratedReceiptContract.java",
            "public static final int PROGRAM_ABI_V5 = 5;",
            2,
        ),
        (
            "platform/sdk/swift/Sources/LayerXSDK/Generated/ReceiptContract.swift",
            "let programAbiV5: UInt16 = 5",
            2,
        ),
        (
            "platform/sdk/dotnet/Generated/ReceiptContract.cs",
            "public const ushort ProgramAbiV5 = 5;",
            2,
        ),
    ] {
        let generated = fs::read_to_string(root.join(relative))
            .unwrap_or_else(|error| panic!("read {relative}: {error}"));
        assert!(
            generated.lines().any(|line| line.trim() == declaration),
            "{relative} lacks {declaration}"
        );
        let admitted = generated
            .lines()
            .filter(|line| line.contains("ABI_V5") || line.contains("AbiV5"))
            .count();
        assert_eq!(
            admitted, mentions,
            "{relative} must declare and admit the fifth ABI"
        );
    }
    check(&root, &lock_path(&root)).unwrap_or_else(|error| panic!("gate failed: {error}"));
    cleanup(&root);
}

#[test]
fn programs_schema_without_the_fifth_guest_abi_is_refused() {
    let root = repo_fixture("guest-abi-four");
    let path = root.join("agent/schema/agent-api/programs.kvx");
    let schema = fs::read_to_string(&path).unwrap_or_else(|error| panic!("read schema: {error}"));
    let four = schema
        .replace(",\"ABI_V5_VERSION\"]", "]")
        .replace(",\"5\"]", "]")
        .replace(",\"V2\",\"V2\",\"V2\",\"V2\"]", ",\"V2\",\"V2\",\"V2\"]");
    assert_ne!(four, schema, "fixture schema must name the fifth ABI");
    fs::write(&path, four).unwrap_or_else(|error| panic!("write schema: {error}"));
    let error = write_lock(&root, &lock_path(&root))
        .err()
        .unwrap_or_else(|| panic!("a four-version schema must not generate"));
    assert_eq!(
        error,
        "Programs guest ABI variants must match the canonical named policy"
    );
    cleanup(&root);
}

#[test]
fn programs_schema_value_differing_from_the_canonical_policy_is_refused() {
    let root = repo_fixture("guest-abi-value");
    let path = root.join("agent/schema/agent-api/programs.kvx");
    let schema = fs::read_to_string(&path).unwrap_or_else(|error| panic!("read schema: {error}"));
    let changed = schema.replace("\"4\",\"5\"]", "\"4\",\"6\"]");
    assert_ne!(changed, schema, "fixture schema must carry wire value 5");
    fs::write(&path, changed).unwrap_or_else(|error| panic!("write schema: {error}"));
    let error = write_lock(&root, &lock_path(&root))
        .err()
        .unwrap_or_else(|| panic!("a non-canonical fifth value must not generate"));
    assert_eq!(
        error,
        "Programs ABI ABI_V5_VERSION differs from the canonical policy"
    );
    cleanup(&root);
}
