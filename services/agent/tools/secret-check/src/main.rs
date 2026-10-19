use std::env;
use std::fs;
use std::path::{Path, PathBuf};

const SECRET_FIELDS: &[&str] = &[
    "seed:",
    "secret:",
    "private_key:",
    "password:",
    "session_token:",
    "api_token:",
    "credential:",
];

const SECRET_IDENTIFIERS: &[&str] = &[
    "seed",
    "secret",
    "private_key",
    "password",
    "session_token",
    "api_token",
    "credential",
];

const OUTPUT_SURFACES: &[&str] = &[
    "format!(",
    "print!(",
    "println!(",
    "eprint!(",
    "eprintln!(",
    "panic!(",
    "tracing::trace!(",
    "tracing::debug!(",
    "tracing::info!(",
    "tracing::warn!(",
    "tracing::error!(",
    "tracing::event!(",
    "tracing::span!(",
    "log::trace!(",
    "log::debug!(",
    "log::info!(",
    "log::warn!(",
    "log::error!(",
    "metrics::counter!(",
    "metrics::gauge!(",
    "metrics::histogram!(",
    "metrics::describe_",
    ".serialize(",
    "serde_json::to_",
];

const DIRECT_OUTPUT_SURFACES: &[&str] = &[
    "print!(",
    "println!(",
    "eprint!(",
    "eprintln!(",
    "panic!(",
    "tracing::",
    "log::",
    "metrics::",
    "serde_json::to_",
    ".serialize(",
];

#[derive(Debug, Eq, PartialEq)]
struct Violation {
    path: PathBuf,
    line: usize,
    rule: &'static str,
}

fn source_files(root: &Path, files: &mut Vec<PathBuf>) -> Result<(), String> {
    let entries =
        fs::read_dir(root).map_err(|error| format!("cannot read {}: {error}", root.display()))?;
    for entry in entries {
        let entry = entry.map_err(|error| error.to_string())?;
        let path = entry.path();
        if path.is_dir() {
            if path.file_name().is_some_and(|name| name == "target") {
                continue;
            }
            source_files(&path, files)?;
        } else if path.extension().is_some_and(|extension| extension == "rs")
            && path
                .components()
                .any(|component| component.as_os_str() == "src")
        {
            files.push(path);
        }
    }
    Ok(())
}

fn strip_strings(source: &str) -> String {
    let mut output = String::with_capacity(source.len());
    let mut characters = source.chars().peekable();
    let mut quoted = false;
    while let Some(character) = characters.next() {
        if quoted {
            if character == '\\' {
                let _ = characters.next();
            } else if character == '"' {
                quoted = false;
            }
            output.push(' ');
        } else if character == '"' {
            quoted = true;
            output.push(' ');
        } else {
            output.push(character);
        }
    }
    output
}

fn block_from(lines: &[&str], start: usize) -> (String, usize) {
    let mut block = String::new();
    let mut depth = 0_i64;
    let mut opened = false;
    let mut end = start;
    for (index, line) in lines.iter().enumerate().skip(start) {
        block.push_str(line);
        block.push('\n');
        for character in strip_strings(line).chars() {
            if character == '{' {
                depth += 1;
                opened = true;
            } else if character == '}' {
                depth -= 1;
            }
        }
        end = index;
        if opened && depth <= 0 {
            break;
        }
    }
    (block, end)
}

fn has_secret_field(block: &str) -> bool {
    let normalized = strip_strings(block).replace(' ', "").to_lowercase();
    SECRET_FIELDS.iter().any(|field| normalized.contains(field))
}

fn secret_fields_are_wrapped(block: &str) -> bool {
    block.lines().all(|line| {
        let normalized = strip_strings(line).replace(' ', "").to_lowercase();
        !SECRET_FIELDS.iter().any(|field| normalized.contains(field))
            || normalized.contains("secret<")
    })
}

fn has_secret_identifier(source: &str) -> bool {
    let source = strip_strings(source).to_lowercase();
    SECRET_IDENTIFIERS
        .iter()
        .any(|identifier| source.contains(identifier))
}

fn scan_source(path: &Path, body: &str) -> Vec<Violation> {
    let mut violations = Vec::new();
    let lines: Vec<_> = body.lines().collect();
    if path.file_name().is_none_or(|name| name != "redact.rs") {
        for (index, line) in lines.iter().enumerate() {
            if strip_strings(line).contains("Zeroizing<") {
                violations.push(Violation {
                    path: path.to_path_buf(),
                    line: index + 1,
                    rule: "raw-zeroizing-secret",
                });
            }
        }
    }

    let mut pending_derive = String::new();
    let mut index = 0;
    while index < lines.len() {
        let trimmed = lines[index].trim();
        if trimmed.starts_with("#[derive(") {
            trimmed.clone_into(&mut pending_derive);
            index += 1;
            continue;
        }
        if trimmed.contains("struct ") {
            let (block, end) = block_from(&lines, index);
            if has_secret_field(&block) {
                if pending_derive.contains("Debug")
                    || pending_derive.contains("Serialize")
                    || pending_derive.contains("Deserialize")
                {
                    violations.push(Violation {
                        path: path.to_path_buf(),
                        line: index + 1,
                        rule: "secret-derived-output",
                    });
                }
                if !secret_fields_are_wrapped(&block) {
                    violations.push(Violation {
                        path: path.to_path_buf(),
                        line: index + 1,
                        rule: "unwrapped-secret-field",
                    });
                }
            }
            pending_derive.clear();
            index = end + 1;
            continue;
        }
        if trimmed.contains("enum ") && trimmed.contains("Error") {
            let (block, end) = block_from(&lines, index);
            let normalized = strip_strings(&block).replace(' ', "").to_lowercase();
            if ["secret(", "seed(", "privatekey(", "token(", "credential("]
                .iter()
                .any(|field| normalized.contains(field))
            {
                violations.push(Violation {
                    path: path.to_path_buf(),
                    line: index + 1,
                    rule: "secret-bearing-error",
                });
            }
            pending_derive.clear();
            index = end + 1;
            continue;
        }
        if !trimmed.is_empty() && !trimmed.starts_with('#') && !trimmed.starts_with("///") {
            pending_derive.clear();
        }
        index += 1;
    }

    for (index, line) in lines.iter().enumerate() {
        if OUTPUT_SURFACES.iter().any(|surface| line.contains(surface)) {
            let end = (index + 8).min(lines.len());
            let statement = lines[index..end].join("\n");
            let statement = statement.split(';').next().unwrap_or(&statement);
            if has_secret_identifier(statement) {
                violations.push(Violation {
                    path: path.to_path_buf(),
                    line: index + 1,
                    rule: "secret-output-surface",
                });
            }
            if path.file_name().is_none_or(|name| name != "redact.rs")
                && DIRECT_OUTPUT_SURFACES
                    .iter()
                    .any(|surface| statement.contains(surface))
                && !statement.contains("Redacted")
                && !statement.contains("redact(")
            {
                violations.push(Violation {
                    path: path.to_path_buf(),
                    line: index + 1,
                    rule: "unwrapped-output-surface",
                });
            }
        }
    }
    violations
}

/// Audits the agent crate source tree for secret output and storage escapes.
fn agent_secret_hygiene_check(agent_root: &Path) -> Result<(), Vec<Violation>> {
    let mut files = Vec::new();
    if source_files(&agent_root.join("crates"), &mut files).is_err() {
        return Err(vec![Violation {
            path: agent_root.join("crates"),
            line: 0,
            rule: "unreadable-agent-source",
        }]);
    }
    let mut violations = Vec::new();
    for path in files {
        match fs::read_to_string(&path) {
            Ok(body) => violations.extend(scan_source(&path, &body)),
            Err(_) => violations.push(Violation {
                path,
                line: 0,
                rule: "unreadable-agent-source",
            }),
        }
    }
    if violations.is_empty() {
        Ok(())
    } else {
        Err(violations)
    }
}

fn main() {
    let root = env::args_os()
        .nth(1)
        .map_or_else(|| PathBuf::from("agent"), PathBuf::from);
    if let Err(violations) = agent_secret_hygiene_check(&root) {
        for violation in violations {
            eprintln!(
                "{}:{}: {}",
                violation.path.display(),
                violation.line,
                violation.rule
            );
        }
        std::process::exit(1);
    }
    println!("agent secret hygiene passed");
}
