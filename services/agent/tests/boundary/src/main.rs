mod cases;
mod gaps;
mod production;

use std::path::PathBuf;
use std::process::ExitCode;

fn main() -> ExitCode {
    let mut arguments = std::env::args_os();
    let _program = arguments.next();
    let first = arguments.next();
    if let Some(mode) = first.as_deref().and_then(|value| value.to_str()) {
        let production_mode = match mode {
            "--production-required" => Some(production::run_required as fn() -> Result<(), String>),
            "--production-recovery" => Some(production::run_recovery as fn() -> Result<(), String>),
            "--production-duplicate" => {
                Some(production::run_duplicate as fn() -> Result<(), String>)
            }
            "--production-peer-refusal" => {
                Some(production::run_peer_refusal as fn() -> Result<(), String>)
            }
            _ => None,
        };
        if let Some(run) = production_mode {
            if arguments.next().is_some() {
                eprintln!("unexpected production-boundary argument");
                return ExitCode::FAILURE;
            }
            return match run() {
                Ok(()) => ExitCode::SUCCESS,
                Err(error) => {
                    eprintln!("mandatory production boundary refused: {error}");
                    ExitCode::FAILURE
                }
            };
        }
    }
    let Some(node) = first.map(PathBuf::from) else {
        eprintln!("missing real layerxd executable path");
        return ExitCode::FAILURE;
    };
    let Some(repository) = arguments.next().map(PathBuf::from) else {
        eprintln!("missing repository root");
        return ExitCode::FAILURE;
    };
    if arguments.next().is_some() {
        eprintln!("unexpected boundary-suite argument");
        return ExitCode::FAILURE;
    }
    match cases::agent_boundary_conformance_suite(&node, &repository) {
        Ok(report) => {
            println!("{report}");
            match production::run_if_configured() {
                Ok(Some(production)) => println!("{production}"),
                Ok(None) => {}
                Err(error) => {
                    eprintln!("production boundary qualification failed: {error}");
                    return ExitCode::FAILURE;
                }
            }
            ExitCode::SUCCESS
        }
        Err(error) => {
            eprintln!("boundary conformance failed: {error}");
            ExitCode::FAILURE
        }
    }
}
