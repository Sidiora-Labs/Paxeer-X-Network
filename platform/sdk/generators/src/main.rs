use std::collections::{BTreeMap, BTreeSet};
use std::env;
use std::fmt::Write as _;
use std::fs;
use std::io::Write as IoWrite;
use std::path::{Path, PathBuf};
use std::process::{Command, Stdio};

use sha2::{Digest, Sha256};

const LOCK_PATH: &str = "platform/sdk/pipeline.kvx";
const RUST_OPERATION_GENERATED_PATH: &str =
    "agent/crates/layerx-agent-api/src/operation_generated.rs";
const GO_GENERATED_PATH: &str = "platform/sdk/go/generated.go";
const JVM_GENERATED_PATH: &str =
    "platform/sdk/jvm/src/main/java/com/sidiora/layerx/sdk/GeneratedContract.java";
const JVM_CONFORMANCE_PATH: &str = "platform/sdk/conformance/jvm.kvx";
const RECEIPT_CONTRACT_PATH: &str = "platform/sdk/generators/receipt.kvx";
const RUST_RECEIPT_GENERATED_PATH: &str = "agent/crates/layerx-sdk/src/receipt_generated.rs";
const TYPESCRIPT_RECEIPT_GENERATED_PATH: &str = "agent/sdk/typescript/src/generated/receipt.ts";
const PYTHON_RECEIPT_GENERATED_PATH: &str = "agent/sdk/python/layerx_sdk/generated/receipt.py";
const PYTHON_RECEIPT_STUB_GENERATED_PATH: &str =
    "agent/sdk/python/layerx_sdk/generated/receipt.pyi";
const GO_RECEIPT_GENERATED_PATH: &str = "platform/sdk/go/receipt_generated.go";
const JVM_RECEIPT_GENERATED_PATH: &str =
    "platform/sdk/jvm/src/main/java/com/sidiora/layerx/sdk/verify/GeneratedReceiptContract.java";
const SWIFT_RECEIPT_GENERATED_PATH: &str =
    "platform/sdk/swift/Sources/LayerXSDK/Generated/ReceiptContract.swift";
const DOTNET_RECEIPT_GENERATED_PATH: &str = "platform/sdk/dotnet/Generated/ReceiptContract.cs";

const SOURCES: [(&str, &str); 3] = [
    ("agent-api", "agent/schema/agent-api"),
    ("human-api", "human/schema/human-api"),
    ("mirror-v2", "platform/sdk/schema"),
];

const GENERATORS: [(&str, &str, &[&str]); 3] = [
    (
        "platform-sdkgen",
        "platform/sdk/generators",
        &[
            "Cargo.toml",
            "generate_jvm.py",
            "generate_lifecycle.py",
            "generate_portable.py",
            "receipt.kvx",
            "src",
        ],
    ),
    (
        "agent-sdk-gen",
        "agent/tools/sdk-gen",
        &["Cargo.lock", "Cargo.toml", "src", "templates"],
    ),
    (
        "human-api-gen",
        "human/tools/api-gen",
        &["Cargo.lock", "Cargo.toml", "src"],
    ),
];

const HANDWRITTEN: &str = "handwritten";

pub const JVM_FILES: &[&str] = &[
    "pom.xml",
    "src/main/java/com/sidiora/layerx/sdk/HttpProductionTransport.java",
    "src/main/java/com/sidiora/layerx/sdk/ProgramLifecycleRoutes.java",
    "src/main/java/com/sidiora/layerx/sdk/GeneratedContract.java",
    "src/main/java/com/sidiora/layerx/sdk/GeneratedSchema.java",
    "src/main/java/com/sidiora/layerx/sdk/GeneratedMirror.java",
    "src/main/java/com/sidiora/layerx/sdk/IdempotencyKey.java",
    "src/main/java/com/sidiora/layerx/sdk/OperationCatalog.java",
    "src/main/java/com/sidiora/layerx/sdk/PlatformSdk.java",
    "src/main/java/com/sidiora/layerx/sdk/PlatformSdkException.java",
    "src/main/java/com/sidiora/layerx/sdk/ProductionClient.java",
    "src/main/java/com/sidiora/layerx/sdk/ProductionTransport.java",
    "src/main/java/com/sidiora/layerx/sdk/ProtocolAmount.java",
    "src/main/java/com/sidiora/layerx/sdk/ResumableStream.java",
    "src/main/java/com/sidiora/layerx/sdk/SchemaErrors.java",
    "src/main/java/com/sidiora/layerx/sdk/SchemaTypes.java",
    "src/main/java/com/sidiora/layerx/sdk/SecretBytes.java",
    "src/main/java/com/sidiora/layerx/sdk/verify/LocalVerifier.java",
    "src/main/java/com/sidiora/layerx/sdk/verify/GeneratedReceiptContract.java",
    "src/conformance/java/com/sidiora/layerx/sdk/ConformanceMain.java",
    "src/main/kotlin/com/sidiora/layerx/sdk/LayerX.kt",
    "src/test/java/com/sidiora/layerx/sdk/GoldenVectorTest.java",
];

const OUTPUTS: [(&str, &str, &str, Option<&[&str]>); 12] = [
    (
        "agent-api-rust",
        "rust",
        "agent/crates/layerx-agent-api/src",
        Some(&["operation_generated.rs", "generated.rs"]),
    ),
    (
        "agent-rust",
        "rust",
        "agent/crates/layerx-sdk/src",
        Some(&["mirror_generated.rs", "receipt_generated.rs"]),
    ),
    (
        "agent-typescript",
        "typescript",
        "agent/sdk/typescript/src/generated",
        None,
    ),
    (
        "agent-python",
        "python",
        "agent/sdk/python/layerx_sdk/generated",
        None,
    ),
    (
        "agent-compatibility",
        "markdown",
        "agent/sdk",
        Some(&["COMPATIBILITY.md"]),
    ),
    (
        "human-typescript",
        "typescript",
        "human/apps/web/src/api/generated",
        None,
    ),
    (
        "platform-go",
        "go",
        "platform/sdk/go",
        Some(&[
            "generated.go",
            "mirror_generated.go",
            "receipt_generated.go",
            "program_lifecycle_generated.go",
        ]),
    ),
    ("platform-jvm", "jvm", "platform/sdk/jvm", Some(JVM_FILES)),
    (
        "platform-conformance",
        "kvx",
        "platform/sdk/conformance",
        Some(&["jvm.kvx", "run-jvm.sh", "mirror-v2.json"]),
    ),
    (
        "platform-swift",
        "swift",
        "platform/sdk/swift/Sources/LayerXSDK/Generated",
        Some(&[
            "OperationCatalog.swift",
            "MirrorSchema.swift",
            "ReceiptContract.swift",
            "ProgramLifecycleRoutes.swift",
        ]),
    ),
    (
        "platform-dotnet",
        "csharp",
        "platform/sdk/dotnet/Generated",
        Some(&[
            "OperationCatalog.cs",
            "MirrorSchema.cs",
            "ReceiptContract.cs",
            "ProgramLifecycleRoutes.cs",
        ]),
    ),
    (
        "platform-portable-conformance",
        "json",
        "platform/sdk/conformance",
        Some(&["operations.json"]),
    ),
];

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct SourceState {
    pub name: String,
    pub root: String,
    pub digest: String,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct OutputState {
    pub name: String,
    pub language: String,
    pub root: String,
    pub files: Vec<(String, String)>,
    pub handwritten: Vec<String>,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Pipeline {
    pub sources: Vec<SourceState>,
    pub generators: Vec<SourceState>,
    pub outputs: Vec<OutputState>,
}

#[derive(Clone, Debug, Eq, PartialEq)]
struct ReceiptContract {
    programs_module_id: u16,
    program_outcome_tags: [u32; 4],
    required_nonzero: Vec<String>,
    failure_checks: Vec<String>,
    program_abis: [u16; 4],
}

fn hex_digest(bytes: &[u8]) -> Result<String, String> {
    let mut rendered = String::with_capacity(64);
    for byte in Sha256::digest(bytes) {
        write!(rendered, "{byte:02x}").map_err(|error| format!("render digest: {error}"))?;
    }
    Ok(rendered)
}

fn walk_files(root: &Path, prefix: &str, files: &mut Vec<String>) -> Result<(), String> {
    let entries =
        fs::read_dir(root).map_err(|error| format!("read {}: {error}", root.display()))?;
    for entry in entries {
        let entry = entry.map_err(|error| format!("read {}: {error}", root.display()))?;
        let name = entry.file_name().into_string().map_err(|name| {
            format!(
                "non-unicode name {} under {}",
                name.display(),
                root.display()
            )
        })?;
        let relative = if prefix.is_empty() {
            name.clone()
        } else {
            format!("{prefix}/{name}")
        };
        let kind = entry
            .file_type()
            .map_err(|error| format!("stat {}: {error}", entry.path().display()))?;
        if kind.is_dir() {
            if name == "__pycache__" {
                continue;
            }
            walk_files(&entry.path(), &relative, files)?;
        } else if kind.is_file() {
            files.push(relative);
        } else {
            return Err(format!(
                "unsupported entry {} in a pipeline tree",
                entry.path().display()
            ));
        }
    }
    Ok(())
}

fn tree_files(root: &Path) -> Result<Vec<String>, String> {
    let mut files = Vec::new();
    walk_files(root, "", &mut files)?;
    files.sort();
    Ok(files)
}

fn file_digest(path: &Path) -> Result<String, String> {
    let bytes = fs::read(path).map_err(|error| format!("read {}: {error}", path.display()))?;
    hex_digest(&bytes)
}

fn source_digest(root: &Path) -> Result<String, String> {
    let mut manifest = String::new();
    for relative in tree_files(root)? {
        let digest = file_digest(&root.join(&relative))?;
        writeln!(manifest, "{relative}\n{digest}")
            .map_err(|error| format!("render source manifest: {error}"))?;
    }
    hex_digest(manifest.as_bytes())
}

fn generator_digest(root: &Path, inputs: &[&str]) -> Result<String, String> {
    let mut manifest = String::new();
    for input in inputs {
        let path = root.join(input);
        let files = if path.is_dir() {
            tree_files(&path)?
                .into_iter()
                .map(|relative| format!("{input}/{relative}"))
                .collect()
        } else if path.is_file() {
            vec![(*input).to_owned()]
        } else {
            return Err(format!("generator input missing: {}", path.display()));
        };
        for relative in files {
            let digest = file_digest(&root.join(&relative))?;
            writeln!(manifest, "{relative}\n{digest}")
                .map_err(|error| format!("render generator manifest: {error}"))?;
        }
    }
    hex_digest(manifest.as_bytes())
}

fn shipped_files(repo_root: &Path, roots: &[&str]) -> Result<Vec<String>, String> {
    let output = Command::new("git")
        .arg("-C")
        .arg(repo_root)
        .args([
            "ls-files",
            "-z",
            "--cached",
            "--others",
            "--exclude-standard",
            "--",
        ])
        .args(roots)
        .output()
        .map_err(|error| format!("list shipped files under generated roots: {error}"))?;
    if !output.status.success() {
        return Err(format!(
            "list shipped files under generated roots: {}",
            String::from_utf8_lossy(&output.stderr).trim()
        ));
    }
    let listing = String::from_utf8(output.stdout)
        .map_err(|_| "non-unicode path under a generated root".to_owned())?;
    let mut files = listing
        .split('\0')
        .filter(|path| !path.is_empty() && repo_root.join(path).is_file())
        .map(str::to_owned)
        .collect::<Vec<_>>();
    files.sort();
    files.dedup();
    Ok(files)
}

fn classify(repo_root: &Path, outputs: &mut [OutputState]) -> Result<(), String> {
    let generated = outputs
        .iter()
        .flat_map(|output| {
            output
                .files
                .iter()
                .map(move |(relative, _)| format!("{}/{relative}", output.root))
        })
        .collect::<BTreeSet<_>>();
    let explicit = OUTPUTS
        .iter()
        .enumerate()
        .filter(|(_, (_, _, _, list))| list.is_some())
        .map(|(index, (_, _, root, _))| (index, *root))
        .collect::<Vec<_>>();
    let roots = explicit.iter().map(|(_, root)| *root).collect::<Vec<_>>();
    for path in shipped_files(repo_root, &roots)? {
        if generated.contains(&path) {
            continue;
        }
        let mut owner: Option<(usize, &str)> = None;
        for &(index, root) in &explicit {
            if path.starts_with(&format!("{root}/"))
                && owner.is_none_or(|(_, held)| root.len() > held.len())
            {
                owner = Some((index, root));
            }
        }
        let (index, root) =
            owner.ok_or_else(|| format!("shipped file outside every generated root: {path}"))?;
        outputs[index]
            .handwritten
            .push(path[root.len() + 1..].to_owned());
    }
    Ok(())
}

type Sections = BTreeMap<String, BTreeMap<String, String>>;

fn schema_sections(root: &Path) -> Result<Sections, String> {
    let version_path = root.join("v1.kvx");
    let version_source = fs::read_to_string(&version_path)
        .map_err(|error| format!("read {}: {error}", version_path.display()))?;
    let version = layerx_platform_kvx::parse(&version_source)?;
    let mut files = vec!["v1.kvx".to_owned()];
    files.extend(layerx_platform_kvx::string_list(
        version.required("schema", "includes")?,
    )?);
    let mut sections = Sections::new();
    for file in files {
        let path = root.join(&file);
        let source = fs::read_to_string(&path)
            .map_err(|error| format!("read {}: {error}", path.display()))?;
        let mut section = String::new();
        for (line_number, raw) in source.lines().enumerate() {
            let line = raw.trim();
            if line.is_empty() || line.starts_with('#') || line.starts_with("//") {
                continue;
            }
            if let Some(name) = line
                .strip_prefix('[')
                .and_then(|value| value.strip_suffix(']'))
            {
                name.clone_into(&mut section);
                sections.entry(section.clone()).or_default();
                continue;
            }
            let Some((key, value)) = line.split_once('=') else {
                return Err(format!(
                    "{}:{} is not a key/value declaration",
                    path.display(),
                    line_number + 1
                ));
            };
            if section.is_empty() {
                return Err(format!(
                    "{}:{} is outside a section",
                    path.display(),
                    line_number + 1
                ));
            }
            sections
                .entry(section.clone())
                .or_default()
                .insert(key.trim().to_owned(), value.trim().to_owned());
        }
    }
    Ok(sections)
}

fn receipt_contract(repo_root: &Path) -> Result<ReceiptContract, String> {
    let path = repo_root.join(RECEIPT_CONTRACT_PATH);
    let source =
        fs::read_to_string(&path).map_err(|error| format!("read {}: {error}", path.display()))?;
    let document = layerx_platform_kvx::parse(&source)?;
    if layerx_platform_kvx::unquote(document.required("receipt", "program_outcome")?)? != "optional"
    {
        return Err("receipt.program_outcome must remain optional".to_owned());
    }
    let programs_module_id = document
        .required("receipt", "programs_module_id")?
        .parse::<u16>()
        .map_err(|error| format!("receipt.programs_module_id: {error}"))?;
    if programs_module_id == 0 {
        return Err("receipt.programs_module_id must be non-zero".to_owned());
    }
    let tags =
        layerx_platform_kvx::string_list(document.required("receipt", "program_outcome_tags")?)?;
    let program_outcome_tags: [u32; 4] = tags
        .iter()
        .map(|tag| {
            u32::from_str_radix(tag, 16)
                .map_err(|error| format!("receipt program outcome tag {tag}: {error}"))
        })
        .collect::<Result<Vec<_>, _>>()?
        .try_into()
        .map_err(|values: Vec<u32>| {
            format!(
                "receipt.program_outcome_tags requires exactly four tags, found {}",
                values.len()
            )
        })?;
    let required_nonzero =
        layerx_platform_kvx::string_list(document.required("receipt", "required_nonzero")?)?;
    let expected_nonzero = [
        "global-sequence",
        "module-id",
        "module-version",
        "timestamp",
        "activity-id",
        "resulting-state-root",
    ];
    if required_nonzero
        != expected_nonzero
            .into_iter()
            .map(str::to_owned)
            .collect::<Vec<_>>()
    {
        return Err(format!(
            "receipt.required_nonzero must be the protocol decoder order: {}",
            expected_nonzero.join(", ")
        ));
    }
    let failure_checks =
        layerx_platform_kvx::string_list(document.required("receipt", "failure_checks")?)?;
    let unique = failure_checks.iter().collect::<BTreeSet<_>>();
    if failure_checks.is_empty() || unique.len() != failure_checks.len() {
        return Err("receipt.failure_checks must be non-empty and unique".to_owned());
    }
    for required in &required_nonzero {
        if !failure_checks.contains(required) {
            return Err(format!(
                "receipt.failure_checks is missing required invariant {required}"
            ));
        }
    }
    let program_source = fs::read_to_string(repo_root.join("agent/schema/agent-api/programs.kvx"))
        .map_err(|error| format!("read Programs schema: {error}"))?;
    let programs = layerx_platform_kvx::parse(&program_source)?;
    let names =
        layerx_platform_kvx::string_list(programs.required("type.ProgramGuestAbi", "variants")?)?;
    let expected_names = [
        "ABI_V1_VERSION",
        "ABI_V2_VERSION",
        "ABI_V3_VERSION",
        "ABI_V4_VERSION",
    ];
    if names != expected_names.map(str::to_owned) {
        return Err("Programs guest ABI variants must match the canonical named policy".to_owned());
    }
    let policy_path =
        layerx_platform_kvx::unquote(programs.required("type.ProgramGuestAbi", "source_policy")?)?;
    if policy_path != "programs/sdk/rust/src/abi_policy.rs" {
        return Err("Programs ABI policy provenance changed".to_owned());
    }
    let policy = fs::read_to_string(repo_root.join(policy_path))
        .map_err(|error| format!("read canonical ABI policy: {error}"))?;
    let values = layerx_platform_kvx::string_list(
        programs.required("type.ProgramGuestAbi", "wire_values")?,
    )?;
    let program_abis: [u16; 4] = values
        .iter()
        .map(|value| value.parse::<u16>().map_err(|error| error.to_string()))
        .collect::<Result<Vec<_>, _>>()?
        .try_into()
        .map_err(|_| "Programs ABI policy requires four named versions")?;
    for (name, value) in names.iter().zip(program_abis) {
        if !policy
            .lines()
            .any(|line| line.trim() == format!("pub const {name}: u16 = {value};"))
        {
            return Err(format!(
                "Programs ABI {name} differs from the canonical policy"
            ));
        }
    }
    let encodings = layerx_platform_kvx::string_list(
        programs.required("type.ProgramGuestAbi", "capability_encoding")?,
    )?;
    if encodings != ["V1", "V2", "V2", "V2"].map(str::to_owned) {
        return Err(
            "Programs ABI capability encoding differs from the canonical policy".to_owned(),
        );
    }
    let terminal_source = fs::read_to_string(
        repo_root.join("programs/crates/layerx-programs-runtime/src/terminal.rs"),
    )
    .map_err(|error| format!("read native terminal codec: {error}"))?;
    for (section, version, allowed) in [
        (
            "type.ProgramExecutionV4",
            4,
            vec!["ABI_V2_VERSION".to_owned()],
        ),
        (
            "type.ProgramExecutionV5",
            5,
            vec!["ABI_V3_VERSION".to_owned(), "ABI_V4_VERSION".to_owned()],
        ),
    ] {
        let domain = layerx_platform_kvx::unquote(programs.required(section, "domain")?)?;
        if domain != format!("LXP/program-execution/v{version}")
            || programs.required(section, "encoding_version")? != version.to_string()
            || layerx_platform_kvx::unquote(programs.required(section, "domain_terminator_hex")?)?
                != "00"
            || layerx_platform_kvx::string_list(programs.required(section, "allowed_guest_abis")?)?
                != allowed
        {
            return Err(format!("{section} violates the frozen execution profile"));
        }
        let declaration = format!("const EXECUTION_V{version}: &[u8] = b\"{domain}\\0\";");
        if !terminal_source
            .lines()
            .any(|line| line.trim() == declaration)
        {
            return Err(format!(
                "{section} has no matching genuine terminal domain producer"
            ));
        }
    }
    Ok(ReceiptContract {
        programs_module_id,
        program_outcome_tags,
        required_nonzero,
        failure_checks,
        program_abis,
    })
}

fn variants(sections: &Sections, section: &str) -> Result<Vec<String>, String> {
    let value = sections
        .get(section)
        .and_then(|entries| entries.get("variants"))
        .ok_or_else(|| format!("missing {section}.variants"))?;
    layerx_platform_kvx::string_list(value)
}

fn go_identifier(value: &str) -> String {
    let mut output = String::new();
    let mut upper = true;
    for character in value.chars() {
        if !character.is_ascii_alphanumeric() {
            upper = true;
            continue;
        }
        if upper {
            output.push(character.to_ascii_uppercase());
            upper = false;
        } else {
            output.push(character);
        }
    }
    output
}

fn rust_identifier(value: &str) -> String {
    let mut output = String::new();
    let mut upper = true;
    for character in value.chars() {
        if !character.is_ascii_alphanumeric() {
            upper = true;
            continue;
        }
        if upper {
            output.push(character.to_ascii_uppercase());
            upper = false;
        } else {
            output.push(character);
        }
    }
    output
}

fn lower_camel_identifier(value: &str) -> String {
    let pascal = rust_identifier(value);
    let mut characters = pascal.chars();
    match characters.next() {
        Some(first) => format!("{}{}", first.to_ascii_lowercase(), characters.as_str()),
        None => String::new(),
    }
}

fn go_receipt_identifier(value: &str) -> String {
    let identifier = rust_identifier(value);
    identifier
        .strip_suffix("Id")
        .map_or(identifier.clone(), |prefix| format!("{prefix}ID"))
}

fn screaming_identifier(value: &str) -> String {
    value
        .chars()
        .map(|character| {
            if character.is_ascii_alphanumeric() {
                character.to_ascii_uppercase()
            } else {
                '_'
            }
        })
        .collect()
}

fn render_program_abi_policy(contract: &ReceiptContract, language: &str) -> Result<String, String> {
    let template = match language {
        "rust" => {
            r#"
pub const PROGRAM_ABI_V1: u16 = {V1};
pub const PROGRAM_ABI_V2: u16 = {V2};
pub const PROGRAM_ABI_V3: u16 = {V3};
pub const PROGRAM_ABI_V4: u16 = {V4};
#[must_use]
pub const fn supports_program_guest_abi(version: u16) -> bool {
    matches!(version, PROGRAM_ABI_V1 | PROGRAM_ABI_V2 | PROGRAM_ABI_V3 | PROGRAM_ABI_V4)
}
"#
        }
        "typescript" => {
            r#"
export const PROGRAM_ABI_V1 = {V1};
export const PROGRAM_ABI_V2 = {V2};
export const PROGRAM_ABI_V3 = {V3};
export const PROGRAM_ABI_V4 = {V4};
export function supportsProgramGuestAbi(version: number): boolean {
  switch (version) {
    case PROGRAM_ABI_V1: case PROGRAM_ABI_V2: case PROGRAM_ABI_V3: case PROGRAM_ABI_V4: return true;
    default: return false;
  }
}
"#
        }
        "python" => {
            r#"
PROGRAM_ABI_V1 = {V1}
PROGRAM_ABI_V2 = {V2}
PROGRAM_ABI_V3 = {V3}
PROGRAM_ABI_V4 = {V4}
def supports_program_guest_abi(version: int) -> bool:
    return type(version) is int and version in (PROGRAM_ABI_V1, PROGRAM_ABI_V2, PROGRAM_ABI_V3, PROGRAM_ABI_V4)
"#
        }
        "python-stub" => {
            r#"
PROGRAM_ABI_V1: int
PROGRAM_ABI_V2: int
PROGRAM_ABI_V3: int
PROGRAM_ABI_V4: int
def supports_program_guest_abi(version: int) -> bool: ...
"#
        }
        "go" => {
            r#"
const ProgramAbiV1 uint16 = {V1}
const ProgramAbiV2 uint16 = {V2}
const ProgramAbiV3 uint16 = {V3}
const ProgramAbiV4 uint16 = {V4}
func SupportsProgramGuestAbi(version uint16) bool {
    switch version {
    case ProgramAbiV1, ProgramAbiV2, ProgramAbiV3, ProgramAbiV4:
        return true
    default:
        return false
    }
}
"#
        }
        "jvm" => {
            r#"
    public static final int PROGRAM_ABI_V1 = {V1};
    public static final int PROGRAM_ABI_V2 = {V2};
    public static final int PROGRAM_ABI_V3 = {V3};
    public static final int PROGRAM_ABI_V4 = {V4};
    public static boolean supportsProgramGuestAbi(int version) {
        return switch (version) {
            case PROGRAM_ABI_V1, PROGRAM_ABI_V2, PROGRAM_ABI_V3, PROGRAM_ABI_V4 -> true;
            default -> false;
        };
    }
"#
        }
        "swift" => {
            r#"
let programAbiV1: UInt16 = {V1}
let programAbiV2: UInt16 = {V2}
let programAbiV3: UInt16 = {V3}
let programAbiV4: UInt16 = {V4}
func supportsProgramGuestAbi(_ version: UInt16) -> Bool {
    switch version {
    case programAbiV1, programAbiV2, programAbiV3, programAbiV4: return true
    default: return false
    }
}
"#
        }
        "dotnet" => {
            r#"
    public const ushort ProgramAbiV1 = {V1};
    public const ushort ProgramAbiV2 = {V2};
    public const ushort ProgramAbiV3 = {V3};
    public const ushort ProgramAbiV4 = {V4};
    public static bool SupportsProgramGuestAbi(ushort version) => version switch
    {
        ProgramAbiV1 or ProgramAbiV2 or ProgramAbiV3 or ProgramAbiV4 => true,
        _ => false,
    };
"#
        }
        _ => return Err("unknown Programs ABI projection".to_owned()),
    };
    let mut output = template.to_owned();
    for (index, value) in contract.program_abis.iter().enumerate() {
        output = output.replace(&format!("{{V{}}}", index + 1), &value.to_string());
    }
    Ok(output)
}

fn render_rust_receipt_contract(contract: &ReceiptContract) -> Result<String, String> {
    let mut output = String::from(
        "//! Code generated from platform/sdk/generators/receipt.kvx. DO NOT EDIT.\n\n",
    );
    writeln!(
        output,
        "pub const PROGRAMS_MODULE_ID: u16 = {};",
        contract.programs_module_id
    )
    .map_err(|error| error.to_string())?;
    writeln!(
        output,
        "pub const PROGRAM_OUTCOME_TAGS: [u32; 4] = [0x{:04x}_{:04x}, 0x{:04x}_{:04x}, 0x{:04x}_{:04x}, 0x{:04x}_{:04x}];\n",
        contract.program_outcome_tags[0] >> 16,
        contract.program_outcome_tags[0] & 0xffff,
        contract.program_outcome_tags[1] >> 16,
        contract.program_outcome_tags[1] & 0xffff,
        contract.program_outcome_tags[2] >> 16,
        contract.program_outcome_tags[2] & 0xffff,
        contract.program_outcome_tags[3] >> 16,
        contract.program_outcome_tags[3] & 0xffff
    )
    .map_err(|error| error.to_string())?;
    output
        .push_str("#[derive(Clone, Copy, Debug, Eq, PartialEq)]\npub enum ReceiptFailureCode {\n");
    for check in &contract.failure_checks {
        writeln!(output, "    {},", rust_identifier(check)).map_err(|error| error.to_string())?;
    }
    output.push_str("}\n\nimpl ReceiptFailureCode {\n    #[must_use]\n    pub const fn as_str(self) -> &'static str {\n        match self {\n");
    for check in &contract.failure_checks {
        writeln!(
            output,
            "            Self::{} => {},",
            rust_identifier(check),
            quoted(check)
        )
        .map_err(|error| error.to_string())?;
    }
    output.push_str(
        "        }\n    }\n}\n\npub const REQUIRED_NONZERO_CHECKS: &[ReceiptFailureCode] = &[\n",
    );
    for check in &contract.required_nonzero {
        writeln!(
            output,
            "    ReceiptFailureCode::{},",
            rust_identifier(check)
        )
        .map_err(|error| error.to_string())?;
    }
    output.push_str("];\n");
    output.push_str(&render_program_abi_policy(contract, "rust")?);
    Ok(output)
}

fn render_typescript_receipt_contract(contract: &ReceiptContract) -> Result<String, String> {
    let mut output = String::from(
        "// Code generated from platform/sdk/generators/receipt.kvx. DO NOT EDIT.\n\n",
    );
    writeln!(
        output,
        "export const PROGRAMS_MODULE_ID = {};",
        contract.programs_module_id
    )
    .map_err(|error| error.to_string())?;
    writeln!(
        output,
        "export const PROGRAM_OUTCOME_TAGS = [0x{:08x}, 0x{:08x}, 0x{:08x}, 0x{:08x}] as const;\n",
        contract.program_outcome_tags[0],
        contract.program_outcome_tags[1],
        contract.program_outcome_tags[2],
        contract.program_outcome_tags[3]
    )
    .map_err(|error| error.to_string())?;
    output.push_str("export enum ReceiptFailureCode {\n");
    for check in &contract.failure_checks {
        writeln!(output, "  {} = {},", rust_identifier(check), quoted(check))
            .map_err(|error| error.to_string())?;
    }
    output.push_str("}\n\nexport const REQUIRED_NONZERO_CHECKS = Object.freeze([\n");
    for check in &contract.required_nonzero {
        writeln!(output, "  ReceiptFailureCode::{},", rust_identifier(check))
            .map_err(|error| error.to_string())?;
    }
    output = output.replace("ReceiptFailureCode::", "ReceiptFailureCode.");
    output.push_str("]);\n");
    output.push_str(&render_program_abi_policy(contract, "typescript")?);
    Ok(output)
}

fn render_python_receipt_contract(
    contract: &ReceiptContract,
    stub: bool,
) -> Result<String, String> {
    let mut output = String::from(
        "# Code generated from platform/sdk/generators/receipt.kvx. DO NOT EDIT.\n\nfrom enum import Enum\n",
    );
    if stub {
        output.push_str(
            "\nPROGRAMS_MODULE_ID: int\nPROGRAM_OUTCOME_TAGS: tuple[int, int, int, int]\n\n",
        );
    } else {
        writeln!(
            output,
            "\nPROGRAMS_MODULE_ID = {}\nPROGRAM_OUTCOME_TAGS = ({}, {}, {}, {})\n",
            contract.programs_module_id,
            contract.program_outcome_tags[0],
            contract.program_outcome_tags[1],
            contract.program_outcome_tags[2],
            contract.program_outcome_tags[3]
        )
        .map_err(|error| error.to_string())?;
    }
    output.push_str("class ReceiptFailureCode(str, Enum):\n");
    for check in &contract.failure_checks {
        if stub {
            writeln!(output, "    {}: str", screaming_identifier(check))
                .map_err(|error| error.to_string())?;
        } else {
            writeln!(
                output,
                "    {} = {}",
                screaming_identifier(check),
                quoted(check)
            )
            .map_err(|error| error.to_string())?;
        }
    }
    if stub {
        output.push_str("\nREQUIRED_NONZERO_CHECKS: tuple[ReceiptFailureCode, ...]\n");
    } else {
        output.push_str("\nREQUIRED_NONZERO_CHECKS = (\n");
        for check in &contract.required_nonzero {
            writeln!(
                output,
                "    ReceiptFailureCode.{},",
                screaming_identifier(check)
            )
            .map_err(|error| error.to_string())?;
        }
        output.push_str(")\n");
    }
    output.push_str(&render_program_abi_policy(
        contract,
        if stub { "python-stub" } else { "python" },
    )?);
    Ok(output)
}

fn render_go_receipt_contract(contract: &ReceiptContract) -> Result<String, String> {
    let mut output = String::from(
        "// Code generated from platform/sdk/generators/receipt.kvx. DO NOT EDIT.\n\npackage layerx\n\n",
    );
    writeln!(
        output,
        "const ProgramsModuleID uint16 = {}\n",
        contract.programs_module_id
    )
    .map_err(|error| error.to_string())?;
    writeln!(
        output,
        "const (\n\tProgramOutcomeTagV1 uint32 = 0x{:08x}\n\tProgramOutcomeTagV2 uint32 = 0x{:08x}\n\tProgramOutcomeTagV3 uint32 = 0x{:08x}\n\tProgramOutcomeTagV4 uint32 = 0x{:08x}\n)\n",
        contract.program_outcome_tags[0],
        contract.program_outcome_tags[1],
        contract.program_outcome_tags[2],
        contract.program_outcome_tags[3]
    )
    .map_err(|error| error.to_string())?;
    output.push_str("type ReceiptCheck string\n\nconst (\n");
    for check in &contract.failure_checks {
        writeln!(
            output,
            "\tReceiptCheck{} ReceiptCheck = {}",
            go_receipt_identifier(check),
            quoted(check)
        )
        .map_err(|error| error.to_string())?;
    }
    output.push_str(")\n\nvar RequiredNonzeroChecks = [...]ReceiptCheck{\n");
    for check in &contract.required_nonzero {
        writeln!(output, "\tReceiptCheck{},", go_receipt_identifier(check))
            .map_err(|error| error.to_string())?;
    }
    output.push_str("}\n");
    output.push_str(&render_program_abi_policy(contract, "go")?);
    format_go(&output)
}

fn render_jvm_receipt_contract(contract: &ReceiptContract) -> Result<String, String> {
    let mut output = String::from(
        "// Code generated from platform/sdk/generators/receipt.kvx. DO NOT EDIT.\n\npackage com.sidiora.layerx.sdk.verify;\n\nimport java.util.List;\n\npublic final class GeneratedReceiptContract {\n    private GeneratedReceiptContract() {}\n",
    );
    writeln!(
        output,
        "    public static final int PROGRAMS_MODULE_ID = {};",
        contract.programs_module_id
    )
    .map_err(|error| error.to_string())?;
    writeln!(
        output,
        "    public static final long PROGRAM_OUTCOME_V1 = 0x{:08x}L;\n    public static final long PROGRAM_OUTCOME_V2 = 0x{:08x}L;\n    public static final long PROGRAM_OUTCOME_V3 = 0x{:08x}L;\n    public static final long PROGRAM_OUTCOME_V4 = 0x{:08x}L;\n",
        contract.program_outcome_tags[0],
        contract.program_outcome_tags[1],
        contract.program_outcome_tags[2],
        contract.program_outcome_tags[3]
    )
    .map_err(|error| error.to_string())?;
    output.push_str("    public enum ReceiptCheck {\n");
    for (index, check) in contract.failure_checks.iter().enumerate() {
        writeln!(
            output,
            "        {}({}){}",
            screaming_identifier(check),
            quoted(check),
            if index + 1 == contract.failure_checks.len() {
                ";"
            } else {
                ","
            }
        )
        .map_err(|error| error.to_string())?;
    }
    output.push_str("        private final String wire;\n        ReceiptCheck(String wire) { this.wire = wire; }\n        public String wire() { return wire; }\n    }\n\n    public static final List<ReceiptCheck> REQUIRED_NONZERO_CHECKS = List.of(\n");
    for (index, check) in contract.required_nonzero.iter().enumerate() {
        writeln!(
            output,
            "        ReceiptCheck.{}{}",
            screaming_identifier(check),
            if index + 1 == contract.required_nonzero.len() {
                ");"
            } else {
                ","
            }
        )
        .map_err(|error| error.to_string())?;
    }
    output.push_str("}\n");
    output.truncate(output.len() - 2);
    output.push_str(&render_program_abi_policy(contract, "jvm")?);
    output.push_str("}\n");
    Ok(output)
}

fn render_swift_receipt_contract(contract: &ReceiptContract) -> Result<String, String> {
    let mut output = String::from(
        "// Code generated from platform/sdk/generators/receipt.kvx. DO NOT EDIT.\n\n",
    );
    writeln!(
        output,
        "let programsModuleID: UInt16 = {}",
        contract.programs_module_id
    )
    .map_err(|error| error.to_string())?;
    writeln!(
        output,
        "let programOutcomeV1: UInt32 = 0x{:08x}\nlet programOutcomeV2: UInt32 = 0x{:08x}\nlet programOutcomeV3: UInt32 = 0x{:08x}\nlet programOutcomeV4: UInt32 = 0x{:08x}\n",
        contract.program_outcome_tags[0],
        contract.program_outcome_tags[1],
        contract.program_outcome_tags[2],
        contract.program_outcome_tags[3]
    )
    .map_err(|error| error.to_string())?;
    output.push_str("public enum ReceiptCheck: String, Sendable, CaseIterable {\n");
    for check in &contract.failure_checks {
        writeln!(
            output,
            "    case {} = {}",
            lower_camel_identifier(check),
            quoted(check)
        )
        .map_err(|error| error.to_string())?;
    }
    output.push_str("}\n\nlet requiredNonzeroChecks: [ReceiptCheck] = [\n");
    for check in &contract.required_nonzero {
        writeln!(output, "    .{},", lower_camel_identifier(check))
            .map_err(|error| error.to_string())?;
    }
    output.push_str("]\n");
    output.push_str(&render_program_abi_policy(contract, "swift")?);
    Ok(output)
}

fn render_dotnet_receipt_contract(contract: &ReceiptContract) -> Result<String, String> {
    let mut output = String::from(
        "// Code generated from platform/sdk/generators/receipt.kvx. DO NOT EDIT.\n#nullable enable\n\nnamespace LayerX.Sdk;\n\n",
    );
    output.push_str("public enum ReceiptCheck\n{\n");
    for check in &contract.failure_checks {
        writeln!(output, "    {},", rust_identifier(check)).map_err(|error| error.to_string())?;
    }
    output.push_str("}\n\npublic static class GeneratedReceiptContract\n{\n");
    writeln!(
        output,
        "    public const ushort ProgramsModuleId = {};",
        contract.programs_module_id
    )
    .map_err(|error| error.to_string())?;
    writeln!(
        output,
        "    public const uint ProgramOutcomeV1 = 0x{:08x};\n    public const uint ProgramOutcomeV2 = 0x{:08x};\n    public const uint ProgramOutcomeV3 = 0x{:08x};\n    public const uint ProgramOutcomeV4 = 0x{:08x};",
        contract.program_outcome_tags[0],
        contract.program_outcome_tags[1],
        contract.program_outcome_tags[2],
        contract.program_outcome_tags[3]
    )
    .map_err(|error| error.to_string())?;
    output.push_str("    public static readonly ReceiptCheck[] RequiredNonzeroChecks = [\n");
    for check in &contract.required_nonzero {
        writeln!(output, "        ReceiptCheck.{},", rust_identifier(check))
            .map_err(|error| error.to_string())?;
    }
    output.push_str("    ];\n\n    public static string MachineCode(this ReceiptCheck check) => check switch\n    {\n");
    for check in &contract.failure_checks {
        writeln!(
            output,
            "        ReceiptCheck.{} => {},",
            rust_identifier(check),
            quoted(check)
        )
        .map_err(|error| error.to_string())?;
    }
    output.push_str(
        "        _ => throw new ArgumentOutOfRangeException(nameof(check)),\n    };\n}\n",
    );
    output.truncate(output.len() - 2);
    output.push_str(&render_program_abi_policy(contract, "dotnet")?);
    output.push_str("}\n");
    Ok(output)
}

fn generated_receipt_contracts(repo_root: &Path) -> Result<Vec<(&'static str, String)>, String> {
    let contract = receipt_contract(repo_root)?;
    Ok(vec![
        (
            RUST_RECEIPT_GENERATED_PATH,
            render_rust_receipt_contract(&contract)?,
        ),
        (
            TYPESCRIPT_RECEIPT_GENERATED_PATH,
            render_typescript_receipt_contract(&contract)?,
        ),
        (
            PYTHON_RECEIPT_GENERATED_PATH,
            render_python_receipt_contract(&contract, false)?,
        ),
        (
            PYTHON_RECEIPT_STUB_GENERATED_PATH,
            render_python_receipt_contract(&contract, true)?,
        ),
        (
            GO_RECEIPT_GENERATED_PATH,
            render_go_receipt_contract(&contract)?,
        ),
        (
            JVM_RECEIPT_GENERATED_PATH,
            render_jvm_receipt_contract(&contract)?,
        ),
        (
            SWIFT_RECEIPT_GENERATED_PATH,
            render_swift_receipt_contract(&contract)?,
        ),
        (
            DOTNET_RECEIPT_GENERATED_PATH,
            render_dotnet_receipt_contract(&contract)?,
        ),
    ])
}

fn check_receipt_contracts(repo_root: &Path) -> Result<(), String> {
    for (relative, expected) in generated_receipt_contracts(repo_root)? {
        let path = repo_root.join(relative);
        let actual = fs::read_to_string(&path).map_err(|error| {
            format!(
                "generated receipt contract missing {}: {error}",
                path.display()
            )
        })?;
        if actual != expected {
            return Err(format!(
                "generated receipt contract {} is stale or hand-edited; run make platform-sdk-generate",
                path.display()
            ));
        }
    }
    Ok(())
}

fn write_receipt_contracts(repo_root: &Path) -> Result<(), String> {
    for (relative, source) in generated_receipt_contracts(repo_root)? {
        let path = repo_root.join(relative);
        let parent = path
            .parent()
            .ok_or_else(|| format!("generated path has no parent: {}", path.display()))?;
        fs::create_dir_all(parent)
            .map_err(|error| format!("create {}: {error}", parent.display()))?;
        fs::write(&path, source).map_err(|error| format!("write {}: {error}", path.display()))?;
    }
    Ok(())
}

fn quoted(value: &str) -> String {
    format!("{value:?}")
}

fn render_string_enum(
    output: &mut String,
    type_name: &str,
    constant_prefix: &str,
    values: &[String],
) -> Result<(), String> {
    writeln!(output, "type {type_name} string\n").map_err(|error| error.to_string())?;
    writeln!(output, "const (").map_err(|error| error.to_string())?;
    for value in values {
        writeln!(
            output,
            "\t{constant_prefix}{} {type_name} = {}",
            go_identifier(value),
            quoted(value)
        )
        .map_err(|error| error.to_string())?;
    }
    writeln!(output, ")\n").map_err(|error| error.to_string())?;
    let cases = values
        .iter()
        .map(|value| format!("{constant_prefix}{}", go_identifier(value)))
        .collect::<Vec<_>>()
        .join(", ");
    writeln!(
        output,
        "func (value {type_name}) Valid() bool {{\n\tswitch value {{\n\tcase {cases}:\n\t\treturn true\n\tdefault:\n\t\treturn false\n\t}}\n}}\n"
    )
    .map_err(|error| error.to_string())
}

fn render_operation_type(
    output: &mut String,
    type_name: &str,
    prefix: &str,
    operations: &[String],
    mutations: &BTreeSet<String>,
) -> Result<(), String> {
    writeln!(output, "type {type_name} string\n").map_err(|error| error.to_string())?;
    writeln!(output, "const (").map_err(|error| error.to_string())?;
    for operation in operations {
        writeln!(
            output,
            "\t{prefix}{} {type_name} = {}",
            go_identifier(operation),
            quoted(operation)
        )
        .map_err(|error| error.to_string())?;
    }
    writeln!(output, ")\n").map_err(|error| error.to_string())?;
    writeln!(output, "func All{type_name}s() []{type_name} {{")
        .map_err(|error| error.to_string())?;
    writeln!(output, "\treturn []{type_name}{{").map_err(|error| error.to_string())?;
    for operation in operations {
        writeln!(output, "\t\t{prefix}{},", go_identifier(operation))
            .map_err(|error| error.to_string())?;
    }
    writeln!(output, "\t}}\n}}\n").map_err(|error| error.to_string())?;
    writeln!(output, "func (operation {type_name}) Valid() bool {{")
        .map_err(|error| error.to_string())?;
    writeln!(output, "\tswitch operation {{").map_err(|error| error.to_string())?;
    let valid_cases = operations
        .iter()
        .map(|operation| format!("{prefix}{}", go_identifier(operation)))
        .collect::<Vec<_>>()
        .join(", ");
    writeln!(output, "\tcase {valid_cases}:").map_err(|error| error.to_string())?;
    writeln!(
        output,
        "\t\treturn true\n\tdefault:\n\t\treturn false\n\t}}\n}}\n"
    )
    .map_err(|error| error.to_string())?;
    writeln!(
        output,
        "func (operation {type_name}) RequiresIdempotency() bool {{"
    )
    .map_err(|error| error.to_string())?;
    if mutations.is_empty() {
        writeln!(output, "\treturn false\n}}\n").map_err(|error| error.to_string())?;
    } else {
        writeln!(output, "\tswitch operation {{").map_err(|error| error.to_string())?;
        let mutation_cases = mutations
            .iter()
            .map(|operation| format!("{prefix}{}", go_identifier(operation)))
            .collect::<Vec<_>>()
            .join(", ");
        writeln!(output, "\tcase {mutation_cases}:").map_err(|error| error.to_string())?;
        writeln!(
            output,
            "\t\treturn true\n\tdefault:\n\t\treturn false\n\t}}\n}}\n"
        )
        .map_err(|error| error.to_string())?;
    }
    Ok(())
}

fn format_go(source: &str) -> Result<String, String> {
    let mut child = Command::new("gofmt")
        .stdin(Stdio::piped())
        .stdout(Stdio::piped())
        .stderr(Stdio::piped())
        .spawn()
        .map_err(|error| format!("start gofmt for generated Go SDK: {error}"))?;
    child
        .stdin
        .as_mut()
        .ok_or_else(|| "gofmt stdin unavailable".to_owned())?
        .write_all(source.as_bytes())
        .map_err(|error| format!("write generated Go SDK to gofmt: {error}"))?;
    let output = child
        .wait_with_output()
        .map_err(|error| format!("wait for gofmt: {error}"))?;
    if !output.status.success() {
        return Err(format!(
            "gofmt rejected generated Go SDK: {}",
            String::from_utf8_lossy(&output.stderr).trim()
        ));
    }
    String::from_utf8(output.stdout)
        .map_err(|error| format!("gofmt returned non-UTF-8 output: {error}"))
}

fn schema_agent_operations(agent: &Sections) -> (Vec<String>, BTreeSet<String>) {
    let operations = agent
        .keys()
        .filter_map(|section| section.strip_prefix("operation.").map(str::to_owned))
        .collect::<Vec<_>>();
    let mut mutations = agent
        .keys()
        .filter_map(|section| section.strip_prefix("mutation.").map(str::to_owned))
        .collect::<BTreeSet<_>>();
    for operation in &operations {
        let has_idempotency_field = agent
            .get(&format!("operation.{operation}"))
            .and_then(|entries| entries.get("required"))
            .is_some_and(|value| value.contains("idempotency_key"));
        if has_idempotency_field {
            mutations.insert(operation.clone());
        }
    }
    (operations, mutations)
}

fn generate_rust_operation_catalog(repo_root: &Path) -> Result<String, String> {
    let agent = schema_sections(&repo_root.join(SOURCES[0].1))?;
    let (operations, mutations) = schema_agent_operations(&agent);
    if operations.is_empty() {
        return Err("Rust SDK generation found an empty operation catalogue".to_owned());
    }

    let mut output = String::from(
        "//! Code generated from the `LayerX` Agent API schema. DO NOT EDIT.\n\n#[derive(Clone, Copy, Debug, Eq, PartialEq)]\npub enum Operation {\n",
    );
    for operation in &operations {
        writeln!(output, "    {},", rust_identifier(operation))
            .map_err(|error| error.to_string())?;
    }
    writeln!(
        output,
        "}}\n\nimpl Operation {{\n    pub const ALL: &'static [Self] = &["
    )
    .map_err(|error| error.to_string())?;
    for operation in &operations {
        writeln!(output, "        Self::{},", rust_identifier(operation))
            .map_err(|error| error.to_string())?;
    }
    writeln!(output, "    ];\n\n    #[must_use]\n    pub const fn name(self) -> &'static str {{\n        match self {{")
        .map_err(|error| error.to_string())?;
    for operation in &operations {
        writeln!(
            output,
            "            Self::{} => {},",
            rust_identifier(operation),
            quoted(operation)
        )
        .map_err(|error| error.to_string())?;
    }
    writeln!(
        output,
        "        }}\n    }}\n\n    #[must_use]\n    pub const fn mutating(self) -> bool {{"
    )
    .map_err(|error| error.to_string())?;
    if mutations.is_empty() {
        writeln!(output, "        false").map_err(|error| error.to_string())?;
    } else {
        writeln!(output, "        matches!(\n            self,")
            .map_err(|error| error.to_string())?;
        for (index, operation) in mutations.iter().enumerate() {
            if index == 0 {
                writeln!(output, "            Self::{}", rust_identifier(operation))
                    .map_err(|error| error.to_string())?;
            } else {
                writeln!(
                    output,
                    "                | Self::{}",
                    rust_identifier(operation)
                )
                .map_err(|error| error.to_string())?;
            }
        }
        writeln!(output, "        )").map_err(|error| error.to_string())?;
    }
    writeln!(output, "    }}\n}}").map_err(|error| error.to_string())?;
    Ok(output)
}

fn check_rust_operation_catalog(repo_root: &Path) -> Result<(), String> {
    let path = repo_root.join(RUST_OPERATION_GENERATED_PATH);
    let actual = fs::read_to_string(&path)
        .map_err(|error| format!("generated Rust file missing {}: {error}", path.display()))?;
    let expected = generate_rust_operation_catalog(repo_root)?;
    if actual != expected {
        return Err(format!(
            "generated Rust file {} is stale or hand-edited; run make platform-sdk-generate",
            path.display()
        ));
    }
    Ok(())
}

fn write_rust_operation_catalog(repo_root: &Path) -> Result<(), String> {
    let path = repo_root.join(RUST_OPERATION_GENERATED_PATH);
    let parent = path
        .parent()
        .ok_or_else(|| format!("generated path has no parent: {}", path.display()))?;
    fs::create_dir_all(parent).map_err(|error| format!("create {}: {error}", parent.display()))?;
    fs::write(&path, generate_rust_operation_catalog(repo_root)?)
        .map_err(|error| format!("write {}: {error}", path.display()))
}

fn go_human_operations(human: &Sections) -> (Vec<String>, BTreeSet<String>) {
    let operations = human
        .keys()
        .filter_map(|section| section.strip_prefix("operation.").map(str::to_owned))
        .collect::<Vec<_>>();
    let mutations = operations
        .iter()
        .filter(|operation| {
            human
                .get(&format!("operation.{operation}"))
                .and_then(|entries| entries.get("idempotency"))
                .is_some_and(|value| value == "true")
        })
        .cloned()
        .collect::<BTreeSet<_>>();
    (operations, mutations)
}

fn render_go_scalars(output: &mut String, agent: &Sections) -> Result<(), String> {
    for (section, entries) in agent {
        let Some(name) = section.strip_prefix("scalar.") else {
            continue;
        };
        let rust = entries
            .get("rust")
            .ok_or_else(|| format!("missing {section}.rust"))
            .and_then(|value| layerx_platform_kvx::unquote(value))?;
        let go_type = match rust.as_str() {
            "u128" => "Uint128",
            "u64" => "uint64",
            "u32" => "uint32",
            "u16" => "uint16",
            "u8" => "uint8",
            _ => {
                return Err(format!(
                    "unsupported Go scalar mapping for {section}: {rust}"
                ))
            }
        };
        writeln!(output, "type {name} = {go_type}").map_err(|error| error.to_string())?;
    }
    writeln!(output).map_err(|error| error.to_string())
}

fn render_go_human_metadata(
    output: &mut String,
    human: &Sections,
    operations: &[String],
) -> Result<(), String> {
    writeln!(
        output,
        "type HumanOperationMetadata struct {{\n\tMethod string\n\tPath string\n\tRequest string\n\tResponse string\n}}\n"
    )
    .map_err(|error| error.to_string())?;
    writeln!(
        output,
        "func (operation HumanOperation) Metadata() (HumanOperationMetadata, bool) {{\n\tswitch operation {{"
    )
    .map_err(|error| error.to_string())?;
    for operation in operations {
        let entries = human
            .get(&format!("operation.{operation}"))
            .ok_or_else(|| format!("missing operation.{operation}"))?;
        let field = |name: &str| -> Result<String, String> {
            entries
                .get(name)
                .ok_or_else(|| format!("missing operation.{operation}.{name}"))
                .and_then(|value| layerx_platform_kvx::unquote(value))
        };
        writeln!(
            output,
            "\tcase HumanOperation{}:\n\t\treturn HumanOperationMetadata{{Method: {}, Path: {}, Request: {}, Response: {}}}, true",
            go_identifier(operation),
            quoted(&field("method")?),
            quoted(&field("path")?),
            quoted(&field("request")?),
            quoted(&field("response")?),
        )
        .map_err(|error| error.to_string())?;
    }
    writeln!(
        output,
        "\tdefault:\n\t\treturn HumanOperationMetadata{{}}, false\n\t}}\n}}\n"
    )
    .map_err(|error| error.to_string())
}

fn render_go_enums(output: &mut String, agent: &Sections, human: &Sections) -> Result<(), String> {
    render_string_enum(
        output,
        "AgentErrorClass",
        "AgentError",
        &variants(agent, "type.ErrorClass")?,
    )?;
    render_string_enum(
        output,
        "HumanErrorCode",
        "HumanError",
        &variants(human, "type.ErrorCode")?,
    )?;
    render_string_enum(
        output,
        "JourneyKind",
        "Journey",
        &variants(human, "type.JourneyKind")?,
    )?;
    render_string_enum(
        output,
        "JourneyState",
        "JourneyState",
        &variants(human, "type.JourneyState")?,
    )?;
    render_string_enum(
        output,
        "HumanVerificationLevel",
        "HumanVerification",
        &variants(human, "type.VerificationLevel")?,
    )?;
    render_string_enum(
        output,
        "HumanRetriability",
        "HumanRetry",
        &variants(human, "type.Retriability")?,
    )?;
    render_string_enum(
        output,
        "HumanApprovalState",
        "HumanApproval",
        &variants(human, "type.ApprovalState")?,
    )?;
    render_string_enum(
        output,
        "HumanStreamEventKind",
        "HumanStreamEvent",
        &variants(human, "type.StreamEventKind")?,
    )?;
    render_string_enum(
        output,
        "AgentApprovalEventKind",
        "AgentApprovalEvent",
        &variants(agent, "type.ApprovalLifecycleEvent")?,
    )?;
    render_string_enum(
        output,
        "AgentApprovalState",
        "AgentApprovalState",
        &variants(agent, "type.ApprovalState")?,
    )?;
    render_string_enum(
        output,
        "AgentApprovalDecisionOutcome",
        "AgentApprovalOutcome",
        &variants(agent, "type.ApprovalDecisionOutcome")?,
    )?;
    render_string_enum(
        output,
        "AgentRetriability",
        "AgentRetry",
        &variants(agent, "type.Retriability")?,
    )?;
    render_string_enum(
        output,
        "AgentDeliveryKind",
        "AgentDelivery",
        &variants(agent, "type.Delivery")?,
    )?;
    Ok(())
}

fn generate_go(repo_root: &Path) -> Result<String, String> {
    let agent = schema_sections(&repo_root.join(SOURCES[0].1))?;
    let human = schema_sections(&repo_root.join(SOURCES[1].1))?;
    let (agent_operations, agent_mutations) = schema_agent_operations(&agent);
    let (human_operations, human_mutations) = go_human_operations(&human);
    if agent_operations.is_empty() || human_operations.is_empty() {
        return Err("Go SDK generation found an empty operation catalogue".to_owned());
    }

    let mut output = String::from(
        "// Code generated from the LayerX Agent API and Human API schemas. DO NOT EDIT.\n\npackage layerx\n\n",
    );
    render_go_scalars(&mut output, &agent)?;
    render_operation_type(
        &mut output,
        "AgentOperation",
        "AgentOperation",
        &agent_operations,
        &agent_mutations,
    )?;
    render_operation_type(
        &mut output,
        "HumanOperation",
        "HumanOperation",
        &human_operations,
        &human_mutations,
    )?;
    render_go_human_metadata(&mut output, &human, &human_operations)?;
    render_go_enums(&mut output, &agent, &human)?;
    format_go(&output)
}

fn check_go(repo_root: &Path) -> Result<(), String> {
    let path = repo_root.join(GO_GENERATED_PATH);
    let actual = fs::read_to_string(&path)
        .map_err(|error| format!("generated Go file missing {}: {error}", path.display()))?;
    let expected = generate_go(repo_root)?;
    if actual != expected {
        return Err(format!(
            "generated Go file {} is stale or hand-edited; run make platform-sdk-generate",
            path.display()
        ));
    }
    Ok(())
}

fn write_go(repo_root: &Path) -> Result<(), String> {
    let path = repo_root.join(GO_GENERATED_PATH);
    let parent = path
        .parent()
        .ok_or_else(|| format!("generated path has no parent: {}", path.display()))?;
    fs::create_dir_all(parent).map_err(|error| format!("create {}: {error}", parent.display()))?;
    fs::write(&path, generate_go(repo_root)?)
        .map_err(|error| format!("write {}: {error}", path.display()))
}

fn render_jvm_set(output: &mut String, name: &str, values: &[String]) -> Result<(), String> {
    writeln!(output, "    static final Set<String> {name} = Set.of(")
        .map_err(|error| error.to_string())?;
    for (index, value) in values.iter().enumerate() {
        writeln!(
            output,
            "        {}{}",
            quoted(value),
            if index + 1 == values.len() { ");" } else { "," }
        )
        .map_err(|error| error.to_string())?;
    }
    Ok(())
}

fn generate_jvm_contract(repo_root: &Path) -> Result<String, String> {
    let agent = schema_sections(&repo_root.join(SOURCES[0].1))?;
    let human = schema_sections(&repo_root.join(SOURCES[1].1))?;
    let operations = |sections: &Sections| {
        sections
            .keys()
            .filter_map(|section| section.strip_prefix("operation.").map(str::to_owned))
            .collect::<Vec<_>>()
    };
    let agent_operations = operations(&agent);
    let human_operations = operations(&human);
    if agent_operations.is_empty() || human_operations.is_empty() {
        return Err("JVM SDK generation found an empty operation catalogue".to_owned());
    }
    let mut agent_mutations = agent
        .keys()
        .filter_map(|section| section.strip_prefix("mutation.").map(str::to_owned))
        .collect::<BTreeSet<_>>();
    for operation in &agent_operations {
        if agent
            .get(&format!("operation.{operation}"))
            .and_then(|entries| entries.get("required"))
            .is_some_and(|value| value.contains("idempotency_key"))
        {
            agent_mutations.insert(operation.clone());
        }
    }
    let mut output = String::from(
        "// Code generated from the LayerX Agent API and Human API schemas. DO NOT EDIT.\n\npackage com.sidiora.layerx.sdk;\n\nimport java.util.List;\nimport java.util.Map;\nimport java.util.Set;\n\nfinal class GeneratedContract {\n    private GeneratedContract() {}\n",
    );
    render_jvm_set(&mut output, "AGENT_OPERATIONS", &agent_operations)?;
    render_jvm_set(
        &mut output,
        "AGENT_IDEMPOTENT",
        &agent_mutations.into_iter().collect::<Vec<_>>(),
    )?;
    render_jvm_set(
        &mut output,
        "AGENT_ERROR_CLASSES",
        &variants(&agent, "type.ErrorClass")?,
    )?;
    render_jvm_set(
        &mut output,
        "AGENT_RETRIABILITY",
        &variants(&agent, "type.Retriability")?,
    )?;
    render_jvm_set(
        &mut output,
        "HUMAN_ERROR_CODES",
        &variants(&human, "type.ErrorCode")?,
    )?;
    render_jvm_set(
        &mut output,
        "HUMAN_RETRIABILITY",
        &variants(&human, "type.Retriability")?,
    )?;
    writeln!(
        output,
        "    static final Map<String, OperationCatalog.Route> HUMAN_ROUTES = Map.ofEntries("
    )
    .map_err(|error| error.to_string())?;
    for (index, operation) in human_operations.iter().enumerate() {
        let entries = human
            .get(&format!("operation.{operation}"))
            .ok_or_else(|| format!("missing operation.{operation}"))?;
        let field = |name: &str| -> Result<String, String> {
            entries
                .get(name)
                .ok_or_else(|| format!("missing operation.{operation}.{name}"))
                .and_then(|value| layerx_platform_kvx::unquote(value))
        };
        let path = field("path")?;
        let mut path_parameters = Vec::new();
        let mut rest = path.as_str();
        while let Some(open) = rest.find('{') {
            let after = &rest[open + 1..];
            let close = after
                .find('}')
                .ok_or_else(|| format!("unclosed path parameter in operation.{operation}.path"))?;
            path_parameters.push(after[..close].to_owned());
            rest = &after[close + 1..];
        }
        let params = path_parameters
            .iter()
            .map(|value| quoted(value))
            .collect::<Vec<_>>()
            .join(", ");
        let idempotency = entries
            .get("idempotency")
            .is_some_and(|value| value == "true");
        let bodyless = field("request")? == "Empty";
        writeln!(
            output,
            "        Map.entry({}, new OperationCatalog.Route({}, {}, List.of({params}), {idempotency}, {bodyless})){}",
            quoted(operation), quoted(&field("method")?), quoted(&path),
            if index + 1 == human_operations.len() { ");" } else { "," },
        )
        .map_err(|error| error.to_string())?;
    }
    output.push_str("}\n");
    Ok(output)
}

fn check_jvm_contract(repo_root: &Path) -> Result<(), String> {
    let path = repo_root.join(JVM_GENERATED_PATH);
    let actual = fs::read_to_string(&path)
        .map_err(|error| format!("generated JVM file missing {}: {error}", path.display()))?;
    if actual != generate_jvm_contract(repo_root)? {
        return Err(format!(
            "generated JVM file {} is stale or hand-edited; run make platform-sdk-generate",
            path.display()
        ));
    }
    Ok(())
}

fn write_jvm_contract(repo_root: &Path) -> Result<(), String> {
    let path = repo_root.join(JVM_GENERATED_PATH);
    let parent = path
        .parent()
        .ok_or_else(|| format!("generated path has no parent: {}", path.display()))?;
    fs::create_dir_all(parent).map_err(|error| format!("create {}: {error}", parent.display()))?;
    fs::write(&path, generate_jvm_contract(repo_root)?)
        .map_err(|error| format!("write {}: {error}", path.display()))
}

fn generate_jvm_conformance(repo_root: &Path) -> Result<String, String> {
    let operation_count = |root: &Path| -> Result<usize, String> {
        Ok(schema_sections(root)?
            .keys()
            .filter(|section| section.starts_with("operation."))
            .count())
    };
    let agent_operations = operation_count(&repo_root.join(SOURCES[0].1))?;
    let human_operations = operation_count(&repo_root.join(SOURCES[1].1))?;
    Ok(format!(
        "# GENERATED by layerx-platform-sdkgen from the Agent API and Human API schemas.\n\
[sdk]\n\
name = \"jvm\"\n\
artifact = \"com.sidiora.layerx:layerx-sdk\"\n\
root = \"platform/sdk/jvm\"\n\
agent_schema = \"agent/schema/agent-api\"\n\
human_schema = \"human/schema/human-api\"\n\
protocol_version = 2\n\
agent_operations = {agent_operations}\n\
human_operations = {human_operations}\n\
money_type = \"java.math.BigInteger\"\n\
typed_operations = \"com.sidiora.layerx.sdk.SchemaTypes.Operation\"\n\
typed_requests = \"com.sidiora.layerx.sdk.SchemaTypes.GeneratedRequest\"\n\
typed_responses = \"com.sidiora.layerx.sdk.SchemaTypes.GeneratedResponse\"\n\
typed_events = \"com.sidiora.layerx.sdk.SchemaTypes.GeneratedEvent\"\n\
schema_errors = \"com.sidiora.layerx.sdk.SchemaErrors\"\n\
stream = \"atomic cursor chain with duplicate rejection\"\n\
\n\
[verification]\n\
receipt = \"com.sidiora.layerx.sdk.verify.LocalVerifier.verifyReceipt\"\n\
receipt_outcome = \"com.sidiora.layerx.sdk.verify.LocalVerifier.verifyReceiptOutcome\"\n\
batch_inclusion = \"com.sidiora.layerx.sdk.verify.LocalVerifier.verifyBatchInclusion\"\n\
checkpoint = \"com.sidiora.layerx.sdk.verify.LocalVerifier.verifyCheckpoint\"\n\
receipt_in_batch = \"com.sidiora.layerx.sdk.verify.LocalVerifier.verifyReceiptInBatch\"\n\
merkle = \"com.sidiora.layerx.sdk.verify.LocalVerifier.verifyMerkleInclusion\"\n\
\n\
[golden]\n\
agent_request = \"agent/schema/agent-api/golden/version-request.hex\"\n\
agent_response = \"agent/schema/agent-api/golden/version-response.hex\"\n\
agent_schema_goldens = \"agent/schema/agent-api/golden/*.kvx\"\n\
human_schema_goldens = \"human/schema/human-api/golden/*.json\"\n\
codec_valid = \"tests/vectors/codec/valid.lxv\"\n\
codec_adversarial = \"tests/vectors/codec/adversarial.lxv\"\n"
    ))
}

fn check_jvm_conformance(repo_root: &Path) -> Result<(), String> {
    let path = repo_root.join(JVM_CONFORMANCE_PATH);
    let actual = fs::read_to_string(&path).map_err(|error| {
        format!(
            "generated JVM conformance file missing {}: {error}",
            path.display()
        )
    })?;
    if actual != generate_jvm_conformance(repo_root)? {
        return Err(format!(
            "generated JVM conformance file {} is stale or hand-edited; run make platform-sdk-generate",
            path.display()
        ));
    }
    Ok(())
}

fn write_jvm_conformance(repo_root: &Path) -> Result<(), String> {
    let path = repo_root.join(JVM_CONFORMANCE_PATH);
    let parent = path
        .parent()
        .ok_or_else(|| format!("generated path has no parent: {}", path.display()))?;
    fs::create_dir_all(parent).map_err(|error| format!("create {}: {error}", parent.display()))?;
    fs::write(&path, generate_jvm_conformance(repo_root)?)
        .map_err(|error| format!("write {}: {error}", path.display()))
}

/// Hashes the schema sources, the generator revision and every generated SDK tree as they exist
/// on disk, and classifies every other shipped file inside an explicit output root as handwritten.
///
/// # Errors
///
/// Fails when a schema root or generated root is missing or unreadable.
pub fn capture(repo_root: &Path) -> Result<Pipeline, String> {
    let mut sources = Vec::new();
    for (name, root) in SOURCES {
        let path = repo_root.join(root);
        if !path.is_dir() {
            return Err(format!("schema source missing: {}", path.display()));
        }
        sources.push(SourceState {
            name: name.to_owned(),
            root: root.to_owned(),
            digest: source_digest(&path)?,
        });
    }
    let mut generators = Vec::new();
    for (name, root, inputs) in GENERATORS {
        generators.push(SourceState {
            name: name.to_owned(),
            root: root.to_owned(),
            digest: generator_digest(&repo_root.join(root), inputs)?,
        });
    }
    let mut outputs = Vec::new();
    for (name, language, root, explicit) in OUTPUTS {
        let path = repo_root.join(root);
        let names = if let Some(list) = explicit {
            list.iter().map(|item| (*item).to_owned()).collect()
        } else {
            if !path.is_dir() {
                return Err(format!("generated root missing: {}", path.display()));
            }
            tree_files(&path)?
        };
        let mut files = Vec::new();
        for relative in names {
            let file = path.join(&relative);
            if !file.is_file() {
                return Err(format!("generated file missing: {}", file.display()));
            }
            let digest = file_digest(&file)?;
            files.push((relative, digest));
        }
        outputs.push(OutputState {
            name: name.to_owned(),
            language: language.to_owned(),
            root: root.to_owned(),
            files,
            handwritten: Vec::new(),
        });
    }
    classify(repo_root, &mut outputs)?;
    Ok(Pipeline {
        sources,
        generators,
        outputs,
    })
}

/// Renders the pipeline lock document.
///
/// # Errors
///
/// Fails only when formatting into the lock text fails.
pub fn render(pipeline: &Pipeline) -> Result<String, String> {
    let mut text = String::new();
    let fail = |error| format!("render lock: {error}");
    writeln!(text, "[pipeline]").map_err(fail)?;
    let sources = pipeline
        .sources
        .iter()
        .map(|source| layerx_platform_kvx::quote(&source.name))
        .collect::<Vec<_>>()
        .join(", ");
    let generators = pipeline
        .generators
        .iter()
        .map(|generator| layerx_platform_kvx::quote(&generator.name))
        .collect::<Vec<_>>()
        .join(", ");
    let outputs = pipeline
        .outputs
        .iter()
        .map(|output| layerx_platform_kvx::quote(&output.name))
        .collect::<Vec<_>>()
        .join(", ");
    writeln!(text, "sources = [{sources}]").map_err(fail)?;
    writeln!(text, "generators = [{generators}]").map_err(fail)?;
    writeln!(text, "outputs = [{outputs}]").map_err(fail)?;
    for (kind, states) in [
        ("source", &pipeline.sources),
        ("generator", &pipeline.generators),
    ] {
        for state in states {
            writeln!(text, "\n[{kind}.{}]", state.name).map_err(fail)?;
            writeln!(text, "root = {}", layerx_platform_kvx::quote(&state.root)).map_err(fail)?;
            writeln!(
                text,
                "digest = {}",
                layerx_platform_kvx::quote(&state.digest)
            )
            .map_err(fail)?;
        }
    }
    for output in &pipeline.outputs {
        writeln!(text, "\n[output.{}]", output.name).map_err(fail)?;
        writeln!(
            text,
            "language = {}",
            layerx_platform_kvx::quote(&output.language)
        )
        .map_err(fail)?;
        writeln!(text, "root = {}", layerx_platform_kvx::quote(&output.root)).map_err(fail)?;
        writeln!(text, "\n[files.{}]", output.name).map_err(fail)?;
        for (relative, digest) in &output.files {
            writeln!(
                text,
                "{} = {}",
                layerx_platform_kvx::quote(relative),
                layerx_platform_kvx::quote(digest)
            )
            .map_err(fail)?;
        }
        if !output.handwritten.is_empty() {
            writeln!(text, "\n[handwritten.{}]", output.name).map_err(fail)?;
            for relative in &output.handwritten {
                writeln!(
                    text,
                    "{} = {}",
                    layerx_platform_kvx::quote(relative),
                    layerx_platform_kvx::quote(HANDWRITTEN)
                )
                .map_err(fail)?;
            }
        }
    }
    Ok(text)
}

/// Parses a committed pipeline lock document.
///
/// # Errors
///
/// Refuses malformed lock documents and missing declarations.
pub fn parse_lock(source: &str) -> Result<Pipeline, String> {
    let document = layerx_platform_kvx::parse(source)?;
    let source_names = layerx_platform_kvx::string_list(document.required("pipeline", "sources")?)?;
    let generator_names =
        layerx_platform_kvx::string_list(document.required("pipeline", "generators")?)?;
    let output_names = layerx_platform_kvx::string_list(document.required("pipeline", "outputs")?)?;
    for section in document.sections() {
        if let Some(name) = section.strip_prefix("handwritten.") {
            if !output_names.iter().any(|output| output == name) {
                return Err(structure_error(&format!(
                    "handwritten declarations for undeclared output {name}"
                )));
            }
        }
    }
    let states = |kind: &str, names: Vec<String>| -> Result<Vec<SourceState>, String> {
        let mut states = Vec::new();
        for name in names {
            let section = format!("{kind}.{name}");
            states.push(SourceState {
                root: layerx_platform_kvx::unquote(document.required(&section, "root")?)?,
                digest: layerx_platform_kvx::unquote(document.required(&section, "digest")?)?,
                name,
            });
        }
        Ok(states)
    };
    let sources = states("source", source_names)?;
    let generators = states("generator", generator_names)?;
    let mut outputs = Vec::new();
    for name in output_names {
        let section = format!("output.{name}");
        let language = layerx_platform_kvx::unquote(document.required(&section, "language")?)?;
        let root = layerx_platform_kvx::unquote(document.required(&section, "root")?)?;
        let mut files = Vec::new();
        for (relative, digest) in document.section_entries(&format!("files.{name}")) {
            files.push((relative.to_owned(), layerx_platform_kvx::unquote(digest)?));
        }
        let handwritten = handwritten_declarations(&document, &name)?;
        outputs.push(OutputState {
            name,
            language,
            root,
            files,
            handwritten,
        });
    }
    Ok(Pipeline {
        sources,
        generators,
        outputs,
    })
}

fn handwritten_declarations(
    document: &layerx_platform_kvx::Document,
    output: &str,
) -> Result<Vec<String>, String> {
    let section = format!("handwritten.{output}");
    let mut files = Vec::new();
    for (relative, value) in document.section_entries(&section) {
        if layerx_platform_kvx::unquote(value)? != HANDWRITTEN {
            return Err(format!(
                "{section}.{relative} must be declared {}",
                layerx_platform_kvx::quote(HANDWRITTEN)
            ));
        }
        files.push(relative.to_owned());
    }
    Ok(files)
}

fn structure_error(detail: &str) -> String {
    format!("pipeline lock does not match the wired pipeline ({detail}); run make platform-sdk-generate")
}

/// Fails when any generated SDK output is stale against its schemas or hand-edited.
///
/// # Errors
///
/// Names the first stale schema source or drifted generated file.
pub fn drift_gate(committed: &Pipeline, live: &Pipeline) -> Result<(), String> {
    if committed.sources.len() != live.sources.len() {
        return Err(structure_error("schema source list changed"));
    }
    for (committed_source, live_source) in committed.sources.iter().zip(&live.sources) {
        if committed_source.name != live_source.name || committed_source.root != live_source.root {
            return Err(structure_error("schema source list changed"));
        }
        if committed_source.digest != live_source.digest {
            return Err(format!(
                "stale generated SDKs: schema {} at {} changed after the last generation; run make platform-sdk-generate",
                live_source.name, live_source.root
            ));
        }
    }
    if committed.generators.len() != live.generators.len() {
        return Err(structure_error("generator list changed"));
    }
    for (committed_generator, live_generator) in committed.generators.iter().zip(&live.generators) {
        if committed_generator.name != live_generator.name
            || committed_generator.root != live_generator.root
        {
            return Err(structure_error("generator list changed"));
        }
        if committed_generator.digest != live_generator.digest {
            return Err(format!(
                "stale generated SDKs: generator {} at {} changed after the last generation; run make platform-sdk-generate",
                live_generator.name, live_generator.root
            ));
        }
    }
    if committed.outputs.len() != live.outputs.len() {
        return Err(structure_error("generated output list changed"));
    }
    for (committed_output, live_output) in committed.outputs.iter().zip(&live.outputs) {
        if committed_output.name != live_output.name
            || committed_output.language != live_output.language
            || committed_output.root != live_output.root
        {
            return Err(structure_error("generated output list changed"));
        }
        for (relative, digest) in &committed_output.files {
            match live_output
                .files
                .iter()
                .find(|(live_relative, _)| live_relative == relative)
            {
                None => {
                    return Err(format!(
                        "generated {} file missing: {}/{relative}",
                        live_output.language, live_output.root
                    ));
                }
                Some((_, live_digest)) if live_digest != digest => {
                    return Err(format!(
                        "generated {} file {}/{relative} is stale or hand-edited; run make platform-sdk-generate",
                        live_output.language, live_output.root
                    ));
                }
                Some(_) => {}
            }
        }
        for (relative, _) in &live_output.files {
            if !committed_output
                .files
                .iter()
                .any(|(committed_relative, _)| committed_relative == relative)
            {
                return Err(format!(
                    "untracked file in generated {} root: {}/{relative}; run make platform-sdk-generate",
                    live_output.language, live_output.root
                ));
            }
        }
        for relative in &live_output.handwritten {
            if !committed_output.handwritten.contains(relative) {
                return Err(format!(
                    "untracked file in generated {} root: {}/{relative}; generate it or declare it under [handwritten.{}] in the pipeline lock",
                    live_output.language, live_output.root, live_output.name
                ));
            }
        }
        for relative in &committed_output.handwritten {
            if !live_output.handwritten.contains(relative) {
                return Err(format!(
                    "declared handwritten file {}/{relative} is not a shipped handwritten file of the {} root; run make platform-sdk-generate",
                    live_output.root, live_output.language
                ));
            }
        }
    }
    Ok(())
}

/// Runs the drift gate against the committed lock.
///
/// # Errors
///
/// Fails when the lock is missing, stale or any generated output drifted.
pub fn check(repo_root: &Path, lock_path: &Path) -> Result<(), String> {
    let committed = fs::read_to_string(lock_path).map_err(|error| {
        format!(
            "pipeline lock missing at {}: {error}; run make platform-sdk-generate",
            lock_path.display()
        )
    })?;
    let committed = parse_lock(&committed)?;
    lifecycle_sources(repo_root, true)?;
    let live = capture(repo_root)?;
    drift_gate(&committed, &live)?;
    check_rust_operation_catalog(repo_root)?;
    check_go(repo_root)?;
    check_jvm_contract(repo_root)?;
    check_jvm_conformance(repo_root)?;
    check_receipt_contracts(repo_root)
}

/// Captures the live schema, generator revision and generated-tree state into the lock in one
/// atomic replacement, preserving the explicit handwritten declarations.
///
/// # Errors
///
/// Fails when a tree is unreadable, a shipped file inside an explicit output root is neither
/// generated nor declared handwritten, or the lock cannot be written.
pub fn write_lock(repo_root: &Path, lock_path: &Path) -> Result<(), String> {
    lifecycle_sources(repo_root, false)?;
    write_rust_operation_catalog(repo_root)?;
    write_go(repo_root)?;
    write_jvm_contract(repo_root)?;
    write_jvm_conformance(repo_root)?;
    write_receipt_contracts(repo_root)?;
    replace_lock(lock_path, &classified_lock(repo_root, lock_path)?)
}

fn declared_handwritten(lock_path: &Path) -> Result<BTreeMap<String, Vec<String>>, String> {
    let source = match fs::read_to_string(lock_path) {
        Ok(source) => source,
        Err(error) if error.kind() == std::io::ErrorKind::NotFound => return Ok(BTreeMap::new()),
        Err(error) => return Err(format!("read {}: {error}", lock_path.display())),
    };
    let document = layerx_platform_kvx::parse(&source)?;
    let mut declared = BTreeMap::new();
    for (name, _, _, _) in OUTPUTS {
        declared.insert(name.to_owned(), handwritten_declarations(&document, name)?);
    }
    Ok(declared)
}

fn classified_lock(repo_root: &Path, lock_path: &Path) -> Result<String, String> {
    let declared = declared_handwritten(lock_path)?;
    let pipeline = capture(repo_root)?;
    for output in &pipeline.outputs {
        for relative in &output.handwritten {
            if !declared
                .get(&output.name)
                .is_some_and(|files| files.contains(relative))
            {
                return Err(format!(
                    "unclassified file in generated {} root: {}/{relative}; generate it or declare it under [handwritten.{}] in {}",
                    output.language,
                    output.root,
                    output.name,
                    lock_path.display()
                ));
            }
        }
    }
    render(&pipeline)
}

fn replace_lock(lock_path: &Path, text: &str) -> Result<(), String> {
    if let Some(parent) = lock_path.parent() {
        fs::create_dir_all(parent)
            .map_err(|error| format!("create {}: {error}", parent.display()))?;
    }
    let mut staging = lock_path.as_os_str().to_owned();
    staging.push(".partial");
    let staging = PathBuf::from(staging);
    fs::write(&staging, text).map_err(|error| format!("write {}: {error}", staging.display()))?;
    fs::rename(&staging, lock_path)
        .map_err(|error| format!("replace {}: {error}", lock_path.display()))
}

fn lifecycle_sources(repo_root: &Path, check: bool) -> Result<(), String> {
    let mut command = Command::new("python3");
    command.arg(repo_root.join("platform/sdk/generators/generate_lifecycle.py"));
    command.arg(repo_root);
    if check {
        command.arg("--check");
    }
    let output = command
        .output()
        .map_err(|error| format!("lifecycle generator: {error}"))?;
    if !output.status.success() {
        return Err(format!(
            "lifecycle generator failed: {}",
            String::from_utf8_lossy(&output.stderr)
        ));
    }
    Ok(())
}

fn programs_contracts(repo_root: &Path, lock_path: &Path, write: bool) -> Result<(), String> {
    let source_path = repo_root.join("agent/schema/agent-api/programs.kvx");
    let golden_path = repo_root.join("agent/schema/agent-api/golden/programs.kvx");
    let source = fs::read(&source_path).map_err(|error| error.to_string())?;
    if write {
        write_receipt_contracts(repo_root)?;
        fs::write(golden_path, source).map_err(|error| error.to_string())?;
        replace_lock(lock_path, &classified_lock(repo_root, lock_path)?)
    } else {
        if fs::read(golden_path).map_err(|error| error.to_string())? != source {
            return Err("Programs schema golden drift".to_owned());
        }
        check_receipt_contracts(repo_root)?;
        let committed = fs::read_to_string(lock_path).map_err(|error| error.to_string())?;
        drift_gate(&parse_lock(&committed)?, &capture(repo_root)?)
    }
}

fn run() -> Result<(), String> {
    let arguments = env::args().skip(1).collect::<Vec<_>>();
    let mode = arguments.first().map_or("--check", String::as_str);
    let repo_root = arguments
        .get(1)
        .map_or_else(|| PathBuf::from("."), PathBuf::from);
    let lock_path = arguments
        .get(2)
        .map_or_else(|| repo_root.join(LOCK_PATH), PathBuf::from);
    match mode {
        "--write-rust-operations" => {
            if arguments.len() != 2 {
                return Err(
                    "usage: layerx-platform-sdkgen --write-rust-operations <repo-root>".to_owned(),
                );
            }
            write_rust_operation_catalog(&repo_root)
        }
        "--write-program-contracts" => programs_contracts(&repo_root, &lock_path, true),
        "--check-program-contracts" => programs_contracts(&repo_root, &lock_path, false),
        "--write" => write_lock(&repo_root, &lock_path),
        "--check" => check(&repo_root, &lock_path),
        _ => Err(
            "usage: layerx-platform-sdkgen [--write|--check] [repo-root] [lock-path]".to_owned(),
        ),
    }
}

fn main() {
    if let Err(error) = run() {
        eprintln!("platform-sdkgen: {error}");
        std::process::exit(1);
    }
}
