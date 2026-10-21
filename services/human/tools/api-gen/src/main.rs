use std::env;
use std::path::PathBuf;
use std::process::ExitCode;

use layerx_human_api_gen::{human_api_drift_gate, write_client, Violation};

fn print_violations(violations: &[Violation]) {
    for entry in violations {
        eprintln!(
            "{}: {} ({})",
            entry.rule,
            entry.path.display(),
            entry.detail
        );
    }
}

fn main() -> ExitCode {
    let mut arguments = env::args_os().skip(1).peekable();
    let checking = arguments.peek().is_some_and(|value| value == "--check");
    if checking {
        arguments.next();
    }
    let root = arguments
        .next()
        .map_or_else(|| PathBuf::from("services/human/schema/human-api"), PathBuf::from);
    let out_dir = arguments.next().map_or_else(
        || PathBuf::from("human/apps/web/src/api/generated"),
        PathBuf::from,
    );
    if checking {
        match human_api_drift_gate(&root, &out_dir) {
            Ok(generated) => {
                println!(
                    "human-api client is fresh: {} file(s), {} operations, {} types",
                    generated.files.len(),
                    generated.operations,
                    generated.types
                );
                ExitCode::SUCCESS
            }
            Err(violations) => {
                print_violations(&violations);
                ExitCode::FAILURE
            }
        }
    } else {
        match write_client(&root, &out_dir) {
            Ok(generated) => {
                println!(
                    "generated {} file(s) covering {} operations and {} types into {}",
                    generated.files.len(),
                    generated.operations,
                    generated.types,
                    out_dir.display()
                );
                ExitCode::SUCCESS
            }
            Err(violations) => {
                print_violations(&violations);
                ExitCode::FAILURE
            }
        }
    }
}
