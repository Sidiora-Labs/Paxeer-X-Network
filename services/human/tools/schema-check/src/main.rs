use std::env;
use std::fs;
use std::path::PathBuf;
use std::process::ExitCode;

use layerx_human_schema_check::{
    baseline_manifest, human_api_schema, Violation, BASELINE_FILE, REFRESH_COMMAND,
};

fn print_violations(violations: &[Violation]) {
    for entry in violations {
        eprintln!("{}: {} ({})", entry.rule, entry.path.display(), entry.detail);
    }
}

fn main() -> ExitCode {
    let mut arguments = env::args_os().skip(1);
    let first = arguments.next();
    if first.as_deref().is_some_and(|value| value == "--write-baseline") {
        let root = arguments
            .next()
            .map_or_else(|| PathBuf::from("services/human/schema/human-api"), PathBuf::from);
        return match baseline_manifest(&root) {
            Ok(manifest) => {
                let path = root.join(BASELINE_FILE);
                match fs::write(&path, manifest) {
                    Ok(()) => {
                        println!("wrote frozen baseline {}", path.display());
                        ExitCode::SUCCESS
                    }
                    Err(error) => {
                        eprintln!("cannot write {}: {error}", path.display());
                        ExitCode::FAILURE
                    }
                }
            }
            Err(violations) => {
                print_violations(&violations);
                ExitCode::FAILURE
            }
        };
    }
    let root = first.map_or_else(|| PathBuf::from("services/human/schema/human-api"), PathBuf::from);
    match human_api_schema(&root) {
        Ok(outcome) => {
            if outcome.additions_beyond_baseline > 0 {
                println!(
                    "{} declaration(s) added beyond the frozen baseline; refresh it deliberately with: {REFRESH_COMMAND}",
                    outcome.additions_beyond_baseline
                );
            }
            println!(
                "human-api schema conformance passed: {} declarations, {} operations, {} golden vectors",
                outcome.declarations, outcome.operations, outcome.golden_vectors
            );
            ExitCode::SUCCESS
        }
        Err(violations) => {
            print_violations(&violations);
            ExitCode::FAILURE
        }
    }
}
