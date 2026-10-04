use clap::Parser;
use rustix::fs::{openat, Mode, OFlags, CWD};
use serde::{Deserialize, Serialize};
use serde_json::Value;
use sha2::{Digest, Sha256};
use std::collections::{BTreeMap, BTreeSet};
use std::fs::{self, DirBuilder, File, OpenOptions};
use std::io::{Read, Write};
use std::os::unix::fs::{DirBuilderExt, MetadataExt, OpenOptionsExt};
use std::path::{Component, Path, PathBuf};
use std::process::{Command, Stdio};
use std::thread;
use std::time::{Duration, Instant};

type Result<T> = std::result::Result<T, &'static str>;
const LIMIT: usize = 10;
const MAXIMUM_INPUT: u64 = 4 * 1024 * 1024;
const BEGIN: &str = "// layerx:begin integration";
const END: &str = "// layerx:end integration";
const SAMPLES: [(&str, &str); 2] = [
    ("seller", "platform/docs/samples/paid-endpoint-express"),
    ("buyer", "platform/docs/samples/first-payment-typescript"),
];

#[derive(Parser)]
struct Arguments {
    #[arg(long)]
    repository: PathBuf,
    #[arg(
        long,
        required_unless_present = "capture_snapshot",
        conflicts_with = "capture_snapshot"
    )]
    snapshot: Option<PathBuf>,
    #[arg(long, requires = "registry_url", conflicts_with = "snapshot")]
    capture_snapshot: bool,
    #[arg(long, requires = "capture_snapshot")]
    registry_url: Option<String>,
    #[arg(long)]
    output: PathBuf,
    #[arg(long, default_value = "npm")]
    npm: PathBuf,
    #[arg(long, default_value = "node")]
    node: PathBuf,
}

#[derive(Deserialize, Serialize)]
#[serde(deny_unknown_fields)]
struct Snapshot {
    version: String,
    registry_url: String,
    snapshot_id: String,
    samples: BTreeMap<String, SnapshotSample>,
}

#[derive(Deserialize, Serialize)]
#[serde(deny_unknown_fields)]
struct SnapshotSample {
    lock_file: PathBuf,
    lock_sha256: String,
    quickstart_sha256: String,
}

#[derive(Serialize)]
struct Measurement {
    version: &'static str,
    registry_url: String,
    snapshot_id: String,
    snapshot_sha256: String,
    counting_rule: &'static str,
    limit: usize,
    passed: bool,
    samples: Vec<SampleMeasurement>,
}

#[derive(Serialize)]
struct SampleMeasurement {
    name: String,
    source: String,
    source_sha256: String,
    first_integration_line: usize,
    last_integration_line: usize,
    integration_lines: usize,
    within_limit: bool,
    lock_sha256: String,
    package_versions: BTreeMap<String, String>,
    registry_packages: usize,
    published_imports_checked: bool,
}

struct Integration {
    code: String,
    lines: usize,
    first: usize,
    last: usize,
    imports: BTreeMap<String, Vec<String>>,
}

fn require(value: bool, reason: &'static str) -> Result<()> {
    if value {
        Ok(())
    } else {
        Err(reason)
    }
}

fn admitted(path: &Path) -> Result<()> {
    require(
        !path.components().any(|part| match part {
            Component::Normal(value) => value
                .to_str()
                .is_some_and(|name| name == ".env" || name.starts_with(".env.")),
            _ => false,
        }),
        "credential-path-refused",
    )
}

fn read(path: &Path, private: bool) -> Result<Vec<u8>> {
    admitted(path)?;
    let fd = openat(
        CWD,
        path,
        OFlags::RDONLY | OFlags::NOFOLLOW | OFlags::CLOEXEC,
        Mode::empty(),
    )
    .map_err(|_| "input-open-refused")?;
    let mut file = File::from(fd);
    let metadata = file.metadata().map_err(|_| "input-metadata-refused")?;
    require(
        metadata.is_file() && metadata.len() <= MAXIMUM_INPUT && metadata.nlink() == 1,
        "bounded-regular-input-required",
    )?;
    if private {
        require(
            path.is_absolute()
                && metadata.uid() == rustix::process::geteuid().as_raw()
                && metadata.mode() & 0o077 == 0,
            "protected-owner-input-required",
        )?;
    }
    let mut bytes = Vec::new();
    (&mut file)
        .take(MAXIMUM_INPUT + 1)
        .read_to_end(&mut bytes)
        .map_err(|_| "input-read-refused")?;
    require(bytes.len() as u64 <= MAXIMUM_INPUT, "input-bound-exceeded")?;
    Ok(bytes)
}

fn hash(bytes: &[u8]) -> String {
    let digest = Sha256::digest(bytes);
    digest.iter().map(|byte| format!("{byte:02x}")).collect()
}

fn write(path: &Path, bytes: &[u8]) -> Result<()> {
    admitted(path)?;
    let mut file = OpenOptions::new()
        .write(true)
        .create_new(true)
        .mode(0o600)
        .open(path)
        .map_err(|_| "output-create-refused")?;
    file.write_all(bytes)
        .and_then(|()| file.sync_all())
        .map_err(|_| "output-write-refused")
}

fn directory(path: &Path) -> Result<()> {
    admitted(path)?;
    DirBuilder::new()
        .mode(0o700)
        .create(path)
        .map_err(|_| "fresh-output-directory-required")
}

fn registry(value: &str) -> Result<String> {
    let without = value
        .strip_prefix("https://")
        .ok_or("https-registry-required")?;
    require(
        !without.is_empty()
            && !without.contains(['@', '?', '#', '\\'])
            && without.is_ascii()
            && without
                .bytes()
                .all(|byte| byte.is_ascii_alphanumeric() || b".-_~:/".contains(&byte)),
        "canonical-registry-required",
    )?;
    let authority = without
        .split('/')
        .next()
        .ok_or("registry-authority-required")?;
    require(
        !authority.is_empty() && !authority.starts_with(':') && !authority.starts_with('.'),
        "registry-authority-required",
    )?;
    require(
        !without.split('/').any(|part| part == "." || part == ".."),
        "canonical-registry-required",
    )?;
    Ok(value.trim_end_matches('/').to_owned())
}

fn integration(source: &str) -> Result<Integration> {
    let mut active = false;
    let mut closed = false;
    let mut code = String::new();
    let mut lines = 0;
    let mut first = 0;
    let mut last = 0;
    let mut imports = BTreeMap::new();
    for (offset, line) in source.lines().enumerate() {
        let value = line.trim();
        if value == BEGIN {
            require(!active && !closed, "exactly-one-integration-block-required")?;
            active = true;
            first = offset + 2;
            continue;
        }
        if value == END {
            require(active && !closed, "integration-marker-order-refused")?;
            active = false;
            closed = true;
            last = offset;
            continue;
        }
        if !active {
            continue;
        }
        code.push_str(line);
        code.push('\n');
        if !value.is_empty() && !value.starts_with("//") {
            lines += 1;
        }
        if value.starts_with("import ") {
            let (names, package) = value
                .split_once(" from ")
                .ok_or("named-package-import-required")?;
            let names = names
                .strip_prefix("import {")
                .and_then(|part| part.strip_suffix('}'))
                .ok_or("named-package-import-required")?;
            let package = package.trim_end_matches(';');
            require(
                package.len() > 2
                    && (package.starts_with('"') && package.ends_with('"')
                        || package.starts_with('\'') && package.ends_with('\'')),
                "quoted-package-import-required",
            )?;
            let package = &package[1..package.len() - 1];
            require(
                package.starts_with("@sidiora/layerx-") && !package.contains(['\\', '%', '?', '#']),
                "published-layerx-import-required",
            )?;
            let members: Vec<String> = names.split(',').map(str::trim).map(str::to_owned).collect();
            require(
                !members.is_empty()
                    && members.iter().all(|name| {
                        !name.is_empty()
                            && name
                                .bytes()
                                .all(|byte| byte.is_ascii_alphanumeric() || byte == b'_')
                    }),
                "named-package-import-required",
            )?;
            require(
                imports.insert(package.to_owned(), members).is_none(),
                "duplicate-package-import-refused",
            )?;
        }
    }
    require(
        closed && !active && lines > 0 && !imports.is_empty(),
        "complete-integration-block-required",
    )?;
    Ok(Integration {
        code,
        lines,
        first,
        last,
        imports,
    })
}

fn object(value: &Value) -> Result<&serde_json::Map<String, Value>> {
    value.as_object().ok_or("snapshot-object-required")
}

fn lock_contract(
    lock: &Value,
    package: &Value,
    registry: &str,
) -> Result<(BTreeMap<String, String>, usize)> {
    require(
        lock.get("lockfileVersion").and_then(Value::as_u64) == Some(3),
        "npm-lock-v3-required",
    )?;
    let packages = object(
        lock.get("packages")
            .ok_or("closed-lock-packages-required")?,
    )?;
    require(
        packages.len() > 1 && packages.len() <= 4096,
        "closed-lock-package-bound",
    )?;
    let root = packages.get("").ok_or("lock-root-required")?;
    let dependencies = object(
        package
            .get("dependencies")
            .ok_or("sample-dependencies-required")?,
    )?;
    require(
        root.get("dependencies") == package.get("dependencies"),
        "sample-lock-dependencies-mismatch",
    )?;
    require(
        root.get("name") == package.get("name") && root.get("version") == package.get("version"),
        "sample-lock-identity-mismatch",
    )?;
    let mut versions = BTreeMap::new();
    for (name, expected) in dependencies {
        let expected = expected
            .as_str()
            .ok_or("exact-published-version-required")?;
        require(
            !expected.is_empty()
                && expected.as_bytes()[0].is_ascii_digit()
                && expected
                    .bytes()
                    .all(|byte| byte.is_ascii_alphanumeric() || b".-+".contains(&byte)),
            "exact-published-version-required",
        )?;
        let installed = packages
            .get(&format!("node_modules/{name}"))
            .ok_or("direct-package-missing")?;
        require(
            installed.get("version").and_then(Value::as_str) == Some(expected),
            "direct-package-version-mismatch",
        )?;
        versions.insert(name.clone(), expected.to_owned());
    }
    for (name, value) in packages {
        if name.is_empty() {
            continue;
        }
        require(
            name.starts_with("node_modules/")
                && !name.split('/').any(|part| part == "." || part == "..")
                && value.get("link") != Some(&Value::Bool(true)),
            "workspace-or-file-shortcut-refused",
        )?;
        let resolved = value
            .get("resolved")
            .and_then(Value::as_str)
            .ok_or("registry-resolution-required")?;
        require(
            resolved.starts_with(&format!("{registry}/"))
                && !resolved.contains(['?', '#', '\\'])
                && !resolved.split('/').any(|part| part == "." || part == ".."),
            "snapshot-registry-resolution-required",
        )?;
        let integrity = value
            .get("integrity")
            .and_then(Value::as_str)
            .ok_or("published-integrity-required")?;
        let encoded = integrity
            .strip_prefix("sha512-")
            .ok_or("sha512-integrity-required")?;
        require(
            encoded.len() == 88
                && encoded.ends_with("==")
                && encoded[..86]
                    .bytes()
                    .all(|byte| byte.is_ascii_alphanumeric() || byte == b'+' || byte == b'/'),
            "canonical-sha512-integrity-required",
        )?;
        require(
            value
                .get("version")
                .and_then(Value::as_str)
                .is_some_and(|version| !version.is_empty()),
            "published-package-version-required",
        )?;
    }
    Ok((versions, packages.len() - 1))
}

fn command(
    program: &Path,
    arguments: &[&str],
    current: &Path,
    output: &Path,
    registry: &str,
) -> Result<()> {
    let stdout = OpenOptions::new()
        .write(true)
        .create_new(true)
        .mode(0o600)
        .open(output)
        .map_err(|_| "command-log-create-refused")?;
    let stderr = stdout.try_clone().map_err(|_| "command-log-refused")?;
    let mut child = Command::new(program)
        .args(arguments)
        .current_dir(current)
        .env_remove("NODE_PATH")
        .env_remove("NODE_OPTIONS")
        .env("NPM_CONFIG_REGISTRY", registry)
        .env("NPM_CONFIG_CACHE", current.join("cache"))
        .env("NPM_CONFIG_AUDIT", "false")
        .env("NPM_CONFIG_FUND", "false")
        .env("NPM_CONFIG_IGNORE_SCRIPTS", "true")
        .env("NPM_CONFIG_FETCH_RETRIES", "0")
        .env("NPM_CONFIG_FETCH_TIMEOUT", "60000")
        .env("NPM_CONFIG_OFFLINE", "false")
        .stdin(Stdio::null())
        .stdout(Stdio::from(stdout))
        .stderr(Stdio::from(stderr))
        .spawn()
        .map_err(|_| "declared-toolchain-unavailable")?;
    let start = Instant::now();
    loop {
        if let Some(status) = child.try_wait().map_err(|_| "command-wait-refused")? {
            return require(
                status.success(),
                "published-package-install-or-import-refused",
            );
        }
        if start.elapsed() >= Duration::from_secs(180)
            || fs::metadata(output)
                .map_err(|_| "command-log-refused")?
                .len()
                > 8 * 1024 * 1024
        {
            let _ = child.kill();
            let _ = child.wait();
            return Err("bounded-registry-command-refused");
        }
        thread::sleep(Duration::from_millis(25));
    }
}

fn prepare_output(args: &Arguments) -> Result<()> {
    require(
        args.repository.is_absolute() && args.output.is_absolute(),
        "absolute-source-and-output-required",
    )?;
    require(
        args.repository
            .canonicalize()
            .map_err(|_| "canonical-source-required")?
            == args.repository,
        "canonical-source-required",
    )?;
    let parent = args
        .output
        .parent()
        .ok_or("private-output-parent-required")?;
    let parent_metadata =
        fs::symlink_metadata(parent).map_err(|_| "private-output-parent-required")?;
    require(
        parent_metadata.is_dir()
            && parent_metadata.uid() == rustix::process::geteuid().as_raw()
            && parent_metadata.mode() & 0o077 == 0
            && parent
                .canonicalize()
                .map_err(|_| "private-output-parent-required")?
                == parent,
        "private-output-parent-required",
    )?;
    directory(&args.output)
}

fn capture_snapshot(args: &Arguments) -> Result<bool> {
    let registry = registry(
        args.registry_url
            .as_deref()
            .ok_or("declared-registry-required")?,
    )?;
    prepare_output(args)?;
    let mut samples = BTreeMap::new();
    for (name, relative) in SAMPLES {
        let sample_directory = args.output.join(name);
        directory(&sample_directory)?;
        let source_bytes = read(&args.repository.join(relative).join("index.mjs"), false)?;
        integration(std::str::from_utf8(&source_bytes).map_err(|_| "utf8-quickstart-required")?)?;
        let package_bytes = read(&args.repository.join(relative).join("package.json"), false)?;
        let package: Value =
            serde_json::from_slice(&package_bytes).map_err(|_| "sample-package-json-required")?;
        write(&sample_directory.join("package.json"), &package_bytes)?;
        command(
            &args.npm,
            &[
                "install",
                "--package-lock-only",
                "--ignore-scripts",
                "--no-audit",
                "--no-fund",
                "--workspaces=false",
                "--include=optional",
            ],
            &sample_directory,
            &sample_directory.join("registry-snapshot.log"),
            &registry,
        )?;
        require(
            read(&sample_directory.join("package.json"), false)? == package_bytes,
            "registry-snapshot-mutated-package",
        )?;
        let lock_path = sample_directory.join("package-lock.json");
        let metadata = fs::symlink_metadata(&lock_path).map_err(|_| "snapshot-lock-missing")?;
        require(
            metadata.is_file()
                && metadata.nlink() == 1
                && metadata.uid() == rustix::process::geteuid().as_raw(),
            "snapshot-lock-identity-refused",
        )?;
        fs::set_permissions(
            &lock_path,
            std::os::unix::fs::PermissionsExt::from_mode(0o600),
        )
        .map_err(|_| "snapshot-lock-protection-refused")?;
        let lock_bytes = read(&lock_path, true)?;
        let lock: Value =
            serde_json::from_slice(&lock_bytes).map_err(|_| "snapshot-lock-json-required")?;
        lock_contract(&lock, &package, &registry)?;
        samples.insert(
            name.to_owned(),
            SnapshotSample {
                lock_file: lock_path,
                lock_sha256: hash(&lock_bytes),
                quickstart_sha256: hash(&source_bytes),
            },
        );
    }
    let mut snapshot = Snapshot {
        version: "layerx-ten-line-registry-snapshot-v1".to_owned(),
        registry_url: registry,
        snapshot_id: String::new(),
        samples,
    };
    let context = serde_json::to_vec(&snapshot).map_err(|_| "snapshot-context-encode-refused")?;
    snapshot.snapshot_id = format!("npm-sha256-{}", hash(&context));
    let bytes = serde_json::to_vec_pretty(&snapshot).map_err(|_| "snapshot-encode-refused")?;
    write(&args.output.join("registry-snapshot.json"), &bytes)?;
    File::open(&args.output)
        .and_then(|file| file.sync_all())
        .map_err(|_| "snapshot-directory-sync-refused")?;
    println!("registry-snapshot-captured");
    Ok(true)
}

fn measure(args: &Arguments) -> Result<bool> {
    let snapshot_bytes = read(
        args.snapshot
            .as_deref()
            .ok_or("protected-registry-snapshot-required")?,
        true,
    )?;
    let snapshot: Snapshot =
        serde_json::from_slice(&snapshot_bytes).map_err(|_| "closed-registry-snapshot-required")?;
    require(
        snapshot.version == "layerx-ten-line-registry-snapshot-v1"
            && !snapshot.snapshot_id.is_empty()
            && snapshot.snapshot_id.len() <= 128
            && snapshot
                .snapshot_id
                .bytes()
                .all(|byte| byte.is_ascii_alphanumeric() || b"._-".contains(&byte)),
        "immutable-registry-snapshot-required",
    )?;
    require(
        snapshot
            .samples
            .keys()
            .map(String::as_str)
            .collect::<BTreeSet<_>>()
            == BTreeSet::from(["seller", "buyer"]),
        "exact-seller-and-buyer-snapshot-required",
    )?;
    let registry = registry(&snapshot.registry_url)?;
    prepare_output(args)?;
    let mut samples = Vec::new();
    for (name, relative) in SAMPLES {
        let entry = snapshot
            .samples
            .get(name)
            .ok_or("sample-snapshot-required")?;
        let source_path = args.repository.join(relative).join("index.mjs");
        let source = read(&source_path, false)?;
        require(
            hash(&source) == entry.quickstart_sha256,
            "published-quickstart-source-mismatch",
        )?;
        let integration =
            integration(std::str::from_utf8(&source).map_err(|_| "utf8-quickstart-required")?)?;
        let package_bytes = read(&args.repository.join(relative).join("package.json"), false)?;
        let package: Value =
            serde_json::from_slice(&package_bytes).map_err(|_| "sample-package-json-required")?;
        let lock_bytes = read(&entry.lock_file, true)?;
        require(
            hash(&lock_bytes) == entry.lock_sha256,
            "registry-snapshot-lock-mismatch",
        )?;
        let lock: Value =
            serde_json::from_slice(&lock_bytes).map_err(|_| "snapshot-lock-json-required")?;
        let (versions, packages) = lock_contract(&lock, &package, &registry)?;
        require(
            integration
                .imports
                .keys()
                .all(|name| versions.contains_key(name)),
            "integration-import-not-declared",
        )?;
        let directory = args.output.join(name);
        directory_create(&directory)?;
        write(&directory.join("package.json"), &package_bytes)?;
        write(&directory.join("package-lock.json"), &lock_bytes)?;
        write(
            &directory.join("integration.mjs"),
            integration.code.as_bytes(),
        )?;
        command(
            &args.npm,
            &[
                "ci",
                "--ignore-scripts",
                "--no-audit",
                "--no-fund",
                "--workspaces=false",
                "--include=optional",
            ],
            &directory,
            &directory.join("registry-install.log"),
            &registry,
        )?;
        require(
            read(&directory.join("package-lock.json"), false)? == lock_bytes,
            "registry-install-mutated-lock",
        )?;
        for (package_name, version) in &versions {
            let path = directory.join("node_modules").join(package_name);
            require(
                fs::symlink_metadata(&path)
                    .map_err(|_| "installed-package-missing")?
                    .is_dir(),
                "linked-package-refused",
            )?;
            let installed: Value =
                serde_json::from_slice(&read(&path.join("package.json"), false)?)
                    .map_err(|_| "installed-package-json-required")?;
            require(
                installed.get("name").and_then(Value::as_str) == Some(package_name.as_str())
                    && installed.get("version").and_then(Value::as_str) == Some(version.as_str()),
                "installed-package-identity-mismatch",
            )?;
        }
        let imports = serde_json::to_string(&integration.imports)
            .map_err(|_| "import-manifest-encode-refused")?;
        let probe = format!("import assert from 'node:assert/strict';\nimport {{ realpath }} from 'node:fs/promises';\nimport {{ fileURLToPath }} from 'node:url';\nimport {{ resolve, sep }} from 'node:path';\nconst imports = {imports};\nconst root = await realpath(resolve('node_modules'));\nfor (const [name, members] of Object.entries(imports)) {{\n const path = await realpath(fileURLToPath(import.meta.resolve(name)));\n assert.ok(path.startsWith(root + sep));\n const exported = await import(name);\n for (const member of members) assert.equal(typeof exported[member], 'function');\n}}\nconsole.log('published-package-imports-verified');\n");
        write(&directory.join("published-imports.mjs"), probe.as_bytes())?;
        command(
            &args.node,
            &["--check", "integration.mjs"],
            &directory,
            &directory.join("syntax.log"),
            &registry,
        )?;
        command(
            &args.node,
            &["published-imports.mjs"],
            &directory,
            &directory.join("published-imports.log"),
            &registry,
        )?;
        samples.push(SampleMeasurement {
            name: name.to_owned(),
            source: format!("{relative}/index.mjs"),
            source_sha256: hash(&source),
            first_integration_line: integration.first,
            last_integration_line: integration.last,
            integration_lines: integration.lines,
            within_limit: integration.lines <= LIMIT,
            lock_sha256: hash(&lock_bytes),
            package_versions: versions,
            registry_packages: packages,
            published_imports_checked: true,
        });
    }
    let passed = samples.iter().all(|sample| sample.within_limit);
    let measurement = Measurement {
        version: "layerx-ten-line-measurement-v1", registry_url: registry,
        snapshot_id: snapshot.snapshot_id, snapshot_sha256: hash(&snapshot_bytes),
        counting_rule: "Every nonblank, non-line-comment physical line between the retained exact integration markers; braces, imports and configuration lines count.",
        limit: LIMIT, passed, samples,
    };
    let artifact =
        serde_json::to_vec_pretty(&measurement).map_err(|_| "measurement-encode-refused")?;
    write(&args.output.join("ten-line-counts.json"), &artifact)?;
    File::open(&args.output)
        .and_then(|file| file.sync_all())
        .map_err(|_| "measurement-directory-sync-refused")?;
    println!(
        "{}",
        String::from_utf8(artifact).map_err(|_| "measurement-utf8-refused")?
    );
    Ok(passed)
}

fn directory_create(path: &Path) -> Result<()> {
    directory(path)
}

fn main() {
    let arguments = Arguments::parse();
    let result = if arguments.capture_snapshot {
        capture_snapshot(&arguments)
    } else {
        measure(&arguments)
    };
    match result {
        Ok(true) => {}
        Ok(false) => {
            eprintln!("ten-line-benchmark: integration-line-limit-exceeded");
            std::process::exit(1);
        }
        Err(reason) => {
            eprintln!("ten-line-benchmark: {reason}");
            std::process::exit(78);
        }
    }
}
