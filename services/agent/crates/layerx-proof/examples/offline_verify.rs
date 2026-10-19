use std::env;
use std::fs;
use std::process::ExitCode;

use layerx_proof::receipt::{verify, AuthorizedBatch};

fn fixed_hex<const N: usize>(name: &str, value: &str) -> Result<[u8; N], String> {
    if value.len() != N * 2 {
        return Err(format!(
            "{name} must contain exactly {} hexadecimal characters",
            N * 2
        ));
    }
    let mut bytes = [0_u8; N];
    for (index, byte) in bytes.iter_mut().enumerate() {
        let start = index * 2;
        *byte = u8::from_str_radix(&value[start..start + 2], 16)
            .map_err(|_| format!("{name} contains non-hexadecimal input"))?;
    }
    Ok(bytes)
}

fn run(arguments: &[String]) -> Result<(), String> {
    if arguments.len() != 7 {
        let program = arguments.first().map_or("offline_verify", String::as_str);
        return Err(format!(
            "usage: {program} RECEIPT_HEX_FILE BATCH_ID ASSET PREVIOUS_ROOT RESULTING_ROOT SEQUENCER_PUBLIC_KEY"
        ));
    }
    let receipt_hex = fs::read_to_string(&arguments[1])
        .map_err(|error| format!("could not read receipt file: {error}"))?;
    let receipt = hex_bytes("receipt", receipt_hex.trim())?;
    let authorised = AuthorizedBatch::new(
        fixed_hex("batch id", &arguments[2])?,
        fixed_hex("asset", &arguments[3])?,
        fixed_hex("previous state root", &arguments[4])?,
        fixed_hex("resulting state root", &arguments[5])?,
        fixed_hex("sequencer public key", &arguments[6])?,
    );
    let verified = verify(&receipt, &authorised)
        .map_err(|failure| format!("receipt verification failed at {:?}", failure.check))?;
    println!(
        "verified level={} canonical_bytes={}",
        verified.level().wire_rank(),
        verified.canonical_bytes().len()
    );
    Ok(())
}

fn hex_bytes(name: &str, value: &str) -> Result<Vec<u8>, String> {
    if !value.len().is_multiple_of(2) {
        return Err(format!(
            "{name} must contain an even number of hexadecimal characters"
        ));
    }
    value
        .as_bytes()
        .chunks_exact(2)
        .map(|pair| {
            let digits =
                std::str::from_utf8(pair).map_err(|_| format!("{name} contains invalid UTF-8"))?;
            u8::from_str_radix(digits, 16)
                .map_err(|_| format!("{name} contains non-hexadecimal input"))
        })
        .collect()
}

fn main() -> ExitCode {
    let arguments = env::args().collect::<Vec<_>>();
    match run(&arguments) {
        Ok(()) => ExitCode::SUCCESS,
        Err(error) => {
            eprintln!("{error}");
            ExitCode::FAILURE
        }
    }
}
