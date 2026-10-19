use std::env;
use std::path::PathBuf;
use std::process::ExitCode;

use layerx_agentd::audit::verify_chain;

#[must_use]
fn hex(bytes: &[u8]) -> String {
    const DIGITS: &[u8; 16] = b"0123456789abcdef";
    let mut output = String::with_capacity(bytes.len() * 2);
    for byte in bytes {
        output.push(char::from(DIGITS[usize::from(byte >> 4)]));
        output.push(char::from(DIGITS[usize::from(byte & 0x0f)]));
    }
    output
}

fn main() -> ExitCode {
    let mut arguments = env::args_os();
    let program = arguments
        .next()
        .and_then(|value| PathBuf::from(value).file_name().map(ToOwned::to_owned))
        .unwrap_or_else(|| "layerx-audit-verify".into());
    let Some(path) = arguments.next() else {
        eprintln!("usage: {} PATH", program.to_string_lossy());
        return ExitCode::from(2);
    };
    if arguments.next().is_some() {
        eprintln!("usage: {} PATH", program.to_string_lossy());
        return ExitCode::from(2);
    }
    match verify_chain(PathBuf::from(path)) {
        Ok(verification) => {
            println!(
                "entries={} tail_hash={}",
                verification.entries,
                hex(&verification.tail_hash)
            );
            ExitCode::SUCCESS
        }
        Err(error) => {
            eprintln!("{error}");
            ExitCode::FAILURE
        }
    }
}
