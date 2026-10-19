#[path = "../boundary.rs"]
mod boundary;
#[path = "../exactly_once.rs"]
mod exactly_once;
#[path = "../fabrication.rs"]
mod fabrication;
#[path = "../faults.rs"]
mod faults;
#[path = "../fuzz.rs"]
mod fuzz;
#[path = "../hostile_node.rs"]
mod hostile_node;
#[path = "../wire.rs"]
mod wire;

use std::path::PathBuf;
use std::process::ExitCode;

fn main() -> ExitCode {
    let mut arguments = std::env::args_os();
    let _program = arguments.next();
    let Some(command) = arguments.next() else {
        eprintln!("usage: agent-qualification-gates GATE [ARGS]");
        return ExitCode::FAILURE;
    };
    let result = if command == "wire" {
        let Some(repository) = arguments.next().map(PathBuf::from) else {
            eprintln!("wire gate is missing REPOSITORY");
            return ExitCode::FAILURE;
        };
        let Some(reference) = arguments.next().map(PathBuf::from) else {
            eprintln!("wire gate is missing C_REFERENCE");
            return ExitCode::FAILURE;
        };
        let Some(harness) = arguments.next().map(PathBuf::from) else {
            eprintln!("wire gate is missing HARNESS");
            return ExitCode::FAILURE;
        };
        if arguments.next().is_some() {
            Err("wire gate received an unexpected argument".to_owned())
        } else {
            wire::agent_qualify_wire_gate(&repository, &reference, &harness)
        }
    } else if command == "boundary" {
        let Some(repository) = arguments.next().map(PathBuf::from) else {
            eprintln!("boundary gate is missing REPOSITORY");
            return ExitCode::FAILURE;
        };
        let Some(node) = arguments.next().map(PathBuf::from) else {
            eprintln!("boundary gate is missing NODE");
            return ExitCode::FAILURE;
        };
        let Some(harness) = arguments.next().map(PathBuf::from) else {
            eprintln!("boundary gate is missing HARNESS");
            return ExitCode::FAILURE;
        };
        if arguments.next().is_some() {
            Err("boundary gate received an unexpected argument".to_owned())
        } else {
            boundary::agent_qualify_boundary_gate(&repository, &node, &harness)
        }
    } else if command == "fabrication" {
        let Some(repository) = arguments.next().map(PathBuf::from) else {
            eprintln!("fabrication gate is missing REPOSITORY");
            return ExitCode::FAILURE;
        };
        if arguments.next().is_some() {
            Err("fabrication gate received an unexpected argument".to_owned())
        } else {
            fabrication::agent_qualify_fabrication_gate(&repository)
        }
    } else if command == "faults" {
        let Some(repository) = arguments.next().map(PathBuf::from) else {
            eprintln!("fault gate is missing REPOSITORY");
            return ExitCode::FAILURE;
        };
        if arguments.next().is_some() {
            Err("fault gate received an unexpected argument".to_owned())
        } else {
            faults::agent_fault_injection_suite(&repository).and_then(|faults| {
                exactly_once::agent_exactly_once_suite(&repository)
                    .map(|exactly_once| format!("{faults}\n{exactly_once}"))
            })
        }
    } else if command == "fuzz" {
        let Some(repository) = arguments.next().map(PathBuf::from) else {
            eprintln!("fuzz gate is missing REPOSITORY");
            return ExitCode::FAILURE;
        };
        let Some(minimized_root) = arguments.next().map(PathBuf::from) else {
            eprintln!("fuzz gate is missing MINIMIZED_ROOT");
            return ExitCode::FAILURE;
        };
        if arguments.next().is_some() {
            Err("fuzz gate received an unexpected argument".to_owned())
        } else {
            fuzz::agent_qualify_fuzz_gate(&repository, &minimized_root).and_then(|fuzz| {
                fuzz::agent_qualify_sanitizer_gate(&repository)
                    .map(|sanitizers| format!("{fuzz}\n{sanitizers}"))
            })
        }
    } else {
        Err(format!(
            "unknown qualification gate {}",
            command.to_string_lossy()
        ))
    };
    match result {
        Ok(report) => {
            println!("{report}");
            ExitCode::SUCCESS
        }
        Err(error) => {
            eprintln!("qualification gate failed: {error}");
            ExitCode::FAILURE
        }
    }
}
