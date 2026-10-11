use std::fs;
use std::path::{Path, PathBuf};

const BANNED_DEPENDENCIES: &[&str] = &[
    "bindgen",
    "libsqlite3-sys",
    "rusqlite",
    "sqlx-sqlite",
    "layerx-core",
    "layerx-protocol-core",
];

const PRIVATE_NODE_PATHS: &[&str] = &[
    "node.db",
    "projection.db",
    "append-only.log",
    "log/segments/",
    "state/snapshots/",
    "snapshot.bin",
];

const AMBIENT_NONDETERMINISM: &[&str] = &[
    "SystemTime::now",
    "Instant::now",
    "thread_rng(",
    "getrandom(",
    "OsRng",
];

const PAYLOAD_AUTHORITY_CRATE: &str = "layerx-intents";

const PAYLOAD_ENCODING_CRATE: &str = "layerx-wire";

const PAYLOAD_ENCODING_INVOCATIONS: &[&str] = &[
    "layerx_wire::encode",
    "layerx_wire :: encode",
    "use layerx_wire::encode",
];

const BUNDLE_PAYLOAD_MARKERS: &[&[u8]] = &[b"layerx_wire", b"layerx-wire"];

#[derive(Debug, Eq, PartialEq)]
pub struct Violation {
    pub path: PathBuf,
    pub rule: &'static str,
}

fn visit_files(root: &Path, output: &mut Vec<PathBuf>) -> Result<(), String> {
    let entries =
        fs::read_dir(root).map_err(|error| format!("cannot read {}: {error}", root.display()))?;
    for entry in entries {
        let entry = entry.map_err(|error| error.to_string())?;
        let path = entry.path();
        if path.is_dir() {
            if path.file_name().is_some_and(|name| name == "target") {
                continue;
            }
            visit_files(&path, output)?;
        } else if path.file_name().is_some_and(|name| name == "Cargo.toml")
            || path.extension().is_some_and(|extension| extension == "rs")
        {
            output.push(path);
        }
    }
    Ok(())
}

fn visit_all_files(root: &Path, output: &mut Vec<PathBuf>) -> Result<(), String> {
    let entries =
        fs::read_dir(root).map_err(|error| format!("cannot read {}: {error}", root.display()))?;
    for entry in entries {
        let entry = entry.map_err(|error| error.to_string())?;
        let path = entry.path();
        if path.is_dir() {
            if path.file_name().is_some_and(|name| name == "cache") {
                continue;
            }
            visit_all_files(&path, output)?;
        } else {
            output.push(path);
        }
    }
    Ok(())
}

fn manifest_uses_banned_dependency(body: &str) -> bool {
    body.lines().any(|line| {
        let declaration = line.split('#').next().unwrap_or("").trim();
        BANNED_DEPENDENCIES.iter().any(|dependency| {
            declaration
                .strip_prefix(dependency)
                .is_some_and(|tail| tail.trim_start().starts_with('='))
        }) || declaration.starts_with("links = \"layerx")
    })
}

fn manifest_declares_wire_dependency(body: &str) -> bool {
    body.lines().any(|line| {
        let declaration = line.split('#').next().unwrap_or("").trim();
        declaration
            .strip_prefix(PAYLOAD_ENCODING_CRATE)
            .is_some_and(|tail| {
                let tail = tail.trim_start();
                tail.starts_with('=') || tail.starts_with(".workspace")
            })
    })
}

fn source_violation(body: &str) -> Option<&'static str> {
    if body.contains("include!(") && body.contains("include/layerx")
        || body.contains("include_bytes!(") && body.contains("include/layerx")
        || body.contains("bindgen::")
        || body.contains("#[link(name = \"layerx")
        || body.contains("extern \"C\"")
    {
        return Some("private-c-boundary");
    }
    if body.contains("#[repr(C)]") || body.contains("#[repr(C,") {
        return Some("mirrored-c-layout");
    }
    if PRIVATE_NODE_PATHS
        .iter()
        .any(|private| body.contains(private))
    {
        return Some("node-private-path");
    }
    if AMBIENT_NONDETERMINISM
        .iter()
        .any(|ambient| body.contains(ambient))
    {
        return Some("ambient-nondeterminism");
    }
    if body.contains("::transport::Uds")
        || body.contains("::transport::Tls")
        || body.contains("::abi::Handle")
    {
        return Some("direct-core-connection");
    }
    None
}

fn inside_payload_authority(path: &Path) -> bool {
    path.components().any(|component| {
        component
            .as_os_str()
            .to_str()
            .is_some_and(|name| name == PAYLOAD_AUTHORITY_CRATE)
    })
}

/// # Errors
///
/// Returns every manifest declaring a banned dependency under `root`.
pub fn human_dependency_policy(root: &Path) -> Result<(), Vec<Violation>> {
    let mut files = Vec::new();
    if visit_files(root, &mut files).is_err() {
        return Err(vec![Violation {
            path: root.to_path_buf(),
            rule: "unreadable-human-tree",
        }]);
    }
    let violations = files
        .into_iter()
        .filter(|path| path.file_name().is_some_and(|name| name == "Cargo.toml"))
        .filter_map(|path| {
            let body = fs::read_to_string(&path).ok()?;
            manifest_uses_banned_dependency(&body).then_some(Violation {
                path,
                rule: "forbidden-dependency",
            })
        })
        .collect::<Vec<_>>();
    if violations.is_empty() {
        Ok(())
    } else {
        Err(violations)
    }
}

/// # Errors
///
/// Returns every source file crossing the node or determinism boundary under `root`.
pub fn human_boundary_policy(root: &Path) -> Result<(), Vec<Violation>> {
    let mut files = Vec::new();
    if visit_files(root, &mut files).is_err() {
        return Err(vec![Violation {
            path: root.to_path_buf(),
            rule: "unreadable-human-tree",
        }]);
    }
    let violations = files
        .into_iter()
        .filter(|path| path.extension().is_some_and(|extension| extension == "rs"))
        .filter_map(|path| {
            let body = fs::read_to_string(&path).ok()?;
            source_violation(&body).map(|rule| Violation { path, rule })
        })
        .collect::<Vec<_>>();
    if violations.is_empty() {
        Ok(())
    } else {
        Err(violations)
    }
}

/// # Errors
///
/// Returns every component under `root` other than layerx-intents that depends on
/// layerx-wire or invokes its payload-encoding entry points.
pub fn human_payload_authority_gate(root: &Path) -> Result<(), Vec<Violation>> {
    let mut files = Vec::new();
    if visit_files(root, &mut files).is_err() {
        return Err(vec![Violation {
            path: root.to_path_buf(),
            rule: "unreadable-human-tree",
        }]);
    }
    let violations = files
        .into_iter()
        .filter(|path| !inside_payload_authority(path))
        .filter_map(|path| {
            let body = fs::read_to_string(&path).ok()?;
            if path.file_name().is_some_and(|name| name == "Cargo.toml") {
                return manifest_declares_wire_dependency(&body).then_some(Violation {
                    path,
                    rule: "payload-authority-dependency",
                });
            }
            PAYLOAD_ENCODING_INVOCATIONS
                .iter()
                .any(|invocation| body.contains(invocation))
                .then_some(Violation {
                    path,
                    rule: "payload-encoding-entry-point",
                })
        })
        .collect::<Vec<_>>();
    if violations.is_empty() {
        Ok(())
    } else {
        Err(violations)
    }
}

/// # Errors
///
/// Returns every file in the built web bundle under `bundle` carrying a
/// payload-encoding code path marker, or an unreadable-bundle violation when the
/// build output is absent.
pub fn human_payload_bundle_gate(bundle: &Path) -> Result<(), Vec<Violation>> {
    let mut files = Vec::new();
    if visit_all_files(bundle, &mut files).is_err() {
        return Err(vec![Violation {
            path: bundle.to_path_buf(),
            rule: "unreadable-web-bundle",
        }]);
    }
    if files.is_empty() {
        return Err(vec![Violation {
            path: bundle.to_path_buf(),
            rule: "empty-web-bundle",
        }]);
    }
    let violations = files
        .into_iter()
        .filter_map(|path| {
            let body = fs::read(&path).ok()?;
            BUNDLE_PAYLOAD_MARKERS
                .iter()
                .any(|marker| body.windows(marker.len()).any(|window| &window == marker))
                .then_some(Violation {
                    path,
                    rule: "payload-encoding-in-bundle",
                })
        })
        .collect::<Vec<_>>();
    if violations.is_empty() {
        Ok(())
    } else {
        Err(violations)
    }
}

#[must_use]
pub fn report(result: Result<(), Vec<Violation>>) -> bool {
    match result {
        Ok(()) => true,
        Err(violations) => {
            for violation in violations {
                eprintln!("{}: {}", violation.rule, violation.path.display());
            }
            false
        }
    }
}
