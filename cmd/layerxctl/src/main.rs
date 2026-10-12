use std::collections::BTreeMap;
use std::fmt::Write as _;
use std::fs::OpenOptions;
use std::io::Read;
use std::os::unix::fs::OpenOptionsExt;
use std::process::ExitCode;

use layerx_client::sequencer::{SequencerEndpoint, SequencerError, SequencerReceipt, REVERT_CODE};
use layerx_types::result::{KnownResult, Retriability};
use layerx_wire::limits::MAX_MESSAGE_BYTES;

const MAX_CERTIFICATE_BYTES: usize = 65_536;
const USAGE: &str = "usage: layerxctl read-state --rpc-url URL --to HEX40 --data HEX [--block-height N] [--ca-cert DER_FILE]\n       layerxctl submit --rpc-url URL --transaction FILE [--ca-cert DER_FILE]\n       read-state defaults to pending; submit reads binary signed EIP-1559 bytes and reports submitted until receipt verification is available; HTTPS requires a pinned DER root";

fn options(arguments: &[String], submit: bool) -> Result<BTreeMap<String, String>, String> {
    let mut parsed = BTreeMap::new();
    for pair in arguments.chunks(2) {
        let [key, value] = pair else {
            return Err("option requires a value".to_owned());
        };
        let common = matches!(key.as_str(), "--rpc-url" | "--ca-cert");
        let command_option = if submit {
            key == "--transaction"
        } else {
            matches!(key.as_str(), "--to" | "--data" | "--block-height")
        };
        if !(common || command_option) {
            return Err(format!("unknown option {key}"));
        }
        if value.is_empty() || parsed.insert(key.clone(), value.clone()).is_some() {
            return Err(format!("empty or duplicate option {key}"));
        }
    }
    Ok(parsed)
}

fn required<'a>(options: &'a BTreeMap<String, String>, key: &str) -> Result<&'a str, String> {
    options
        .get(key)
        .map(String::as_str)
        .ok_or_else(|| format!("{key} is required"))
}

fn hex(bytes: &[u8]) -> String {
    let mut output = String::with_capacity(bytes.len() * 2 + 2);
    output.push_str("0x");
    for byte in bytes {
        let _ = write!(output, "{byte:02x}");
    }
    output
}

fn unhex(text: &str, maximum: usize) -> Result<Vec<u8>, String> {
    let digits = text
        .strip_prefix("0x")
        .ok_or_else(|| "hex bytes require the 0x prefix".to_owned())?;
    if !digits.len().is_multiple_of(2)
        || digits.len() / 2 > maximum
        || !digits.bytes().all(|byte| byte.is_ascii_hexdigit())
    {
        return Err("hex bytes must be even-length and within the message bound".to_owned());
    }
    digits
        .as_bytes()
        .chunks_exact(2)
        .map(|pair| {
            let pair = std::str::from_utf8(pair).map_err(|error| error.to_string())?;
            u8::from_str_radix(pair, 16).map_err(|error| error.to_string())
        })
        .collect()
}

fn input_file(path: &str, kind: &str, maximum: usize) -> Result<Vec<u8>, String> {
    let file = OpenOptions::new()
        .read(true)
        .custom_flags(libc::O_NONBLOCK | libc::O_NOFOLLOW)
        .open(path)
        .map_err(|error| format!("{kind} file: {error}"))?;
    let metadata = file.metadata().map_err(|error| error.to_string())?;
    if !metadata.is_file() || metadata.len() == 0 || metadata.len() > maximum as u64 {
        return Err(format!(
            "{kind} must be a nonempty regular file within the {maximum}-byte bound"
        ));
    }
    let mut bytes = Vec::new();
    file.take(maximum as u64 + 1)
        .read_to_end(&mut bytes)
        .map_err(|error| error.to_string())?;
    if bytes.is_empty() || bytes.len() > maximum {
        return Err(format!("{kind} changed outside the {maximum}-byte bound"));
    }
    Ok(bytes)
}

fn quoted(text: &str) -> String {
    let mut output = String::with_capacity(text.len() + 2);
    output.push('"');
    for character in text.chars() {
        match character {
            '"' => output.push_str("\\\""),
            '\\' => output.push_str("\\\\"),
            '\n' => output.push_str("\\n"),
            '\r' => output.push_str("\\r"),
            '\t' => output.push_str("\\t"),
            control if control <= '\u{1f}' => {
                let _ = write!(output, "\\u{:04x}", u32::from(control));
            }
            other => output.push(other),
        }
    }
    output.push('"');
    output
}

fn abi_length(word: &[u8]) -> Option<usize> {
    let (high, low) = word.split_at_checked(24)?;
    if high.iter().any(|byte| *byte != 0) {
        return None;
    }
    usize::try_from(u64::from_be_bytes(low.try_into().ok()?)).ok()
}

fn revert_reason(data: &[u8]) -> Option<&str> {
    let body = data.strip_prefix(&[0x08, 0xc3, 0x79, 0xa0])?;
    let (offset, rest) = body.split_at_checked(32)?;
    let (length, text) = rest.split_at_checked(32)?;
    if abi_length(offset)? != 32 {
        return None;
    }
    let length = abi_length(length)?;
    let padded = length.checked_next_multiple_of(32)?;
    if text.len() != padded || text.get(length..)?.iter().any(|byte| *byte != 0) {
        return None;
    }
    std::str::from_utf8(text.get(..length)?).ok()
}

fn revert_error(data: &[u8]) -> String {
    let reason = revert_reason(data);
    let known = reason.and_then(KnownResult::from_revert_reason);
    let class = match reason.map_or(Retriability::Terminal, Retriability::of_revert_reason) {
        Retriability::Terminal => "terminal",
        Retriability::Retriable => "retriable",
    };
    let code = reason
        .and_then(|text| text.split_once(": "))
        .map(|(code, _)| code)
        .filter(|code| {
            code.starts_with("LXP_")
                && code.len() > 4
                && code
                    .bytes()
                    .all(|byte| byte.is_ascii_uppercase() || byte.is_ascii_digit() || byte == b'_')
        })
        .unwrap_or("LXP_UNKNOWN");
    let number = known.map_or_else(|| "null".to_owned(), |value| value.raw().to_string());
    let reason = reason.map_or_else(|| "null".to_owned(), quoted);
    format!(
        "{{\"code\":{},\"result_code\":{number},\"class\":\"{class}\",\"reason\":{reason}}}",
        quoted(code)
    )
}

fn rpc_error(error: &SequencerError) -> String {
    if let SequencerError::Rpc(refusal) = error {
        let data = refusal.data.as_ref().and_then(|data| {
            data.as_str()
                .or_else(|| data.get("data").and_then(|nested| nested.as_str()))
        });
        let bytes = data.and_then(|data| unhex(data, MAX_MESSAGE_BYTES).ok());
        if refusal.code == REVERT_CODE
            || bytes.as_deref().and_then(revert_reason).is_some()
            || refusal.message.starts_with("execution reverted")
        {
            let revert = revert_error(bytes.as_deref().unwrap_or(&[]));
            return format!(
                "{{\"rpc_code\":{},\"message\":{},\"revert\":{revert}}}",
                refusal.code,
                quoted(&refusal.message)
            );
        }
        let data = refusal
            .data
            .as_ref()
            .map_or_else(|| "null".to_owned(), ToString::to_string);
        return format!(
            "{{\"rpc_code\":{},\"message\":{},\"data\":{data}}}",
            refusal.code,
            quoted(&refusal.message)
        );
    }
    format!("{{\"transport_error\":{}}}", quoted(&format!("{error:?}")))
}

fn submit(client: &SequencerEndpoint, raw: &[u8]) -> Result<(String, ExitCode), String> {
    let hash = match client.send_raw_transaction(raw) {
        Ok(hash) if hash != [0; 32] => hash,
        Ok(_) => return Err("submission returned a zero transaction hash".to_owned()),
        Err(SequencerError::Unavailable) => {
            return Ok((
                "{\"state\":\"submitted\",\"outcome\":\"unknown\"}".to_owned(),
                ExitCode::from(3),
            ));
        }
        Err(error) => return Err(format!("submission refused: {}", rpc_error(&error))),
    };
    let hash_text = hex(&hash);
    let receipt = match client.sequencer_receipt(&hash) {
        Ok(receipt) => receipt,
        Err(error) => {
            return Ok((format!("{{\"state\":\"submitted\",\"transaction_hash\":\"{hash_text}\",\"receipt_error\":{}}}", rpc_error(&error)), ExitCode::from(3)));
        }
    };
    match receipt {
        Some(SequencerReceipt::Receipted(receipt)
            | SequencerReceipt::Committed { receipt, .. }) if !receipt.success => {
            Ok((format!("{{\"state\":\"submitted\",\"transaction_hash\":\"{hash_text}\",\"execution\":\"reported_revert\",\"evidence\":\"unverified_json_rpc\",\"revert\":{}}}", revert_error(&receipt.return_data)), ExitCode::FAILURE))
        }
        _ => Ok((format!("{{\"state\":\"submitted\",\"transaction_hash\":\"{hash_text}\",\"outcome\":\"unverified\"}}"), ExitCode::from(3))),
    }
}

fn run(arguments: &[String]) -> Result<(String, ExitCode), String> {
    let Some(command) = arguments.first() else {
        return Err(USAGE.to_owned());
    };
    if command == "--help" && arguments.len() == 1 {
        return Ok((USAGE.to_owned(), ExitCode::SUCCESS));
    }
    let submitting = match command.as_str() {
        "read-state" => false,
        "submit" => true,
        _ => return Err(USAGE.to_owned()),
    };
    let parsed = options(&arguments[1..], submitting)?;
    let (raw, call) = if submitting {
        let raw = input_file(
            required(&parsed, "--transaction")?,
            "transaction",
            MAX_MESSAGE_BYTES,
        )?;
        if raw.first() != Some(&2) {
            return Err(
                "transaction must contain binary signed EIP-1559 (type 2) bytes".to_owned(),
            );
        }
        (Some(raw), None)
    } else {
        let to: [u8; 20] = unhex(required(&parsed, "--to")?, 20)?
            .try_into()
            .map_err(|_| "--to requires a 20-byte address".to_owned())?;
        let data = unhex(required(&parsed, "--data")?, MAX_MESSAGE_BYTES)?;
        let height = parsed
            .get("--block-height")
            .map(|text| {
                text.parse::<u64>()
                    .map_err(|error| format!("block height: {error}"))
            })
            .transpose()?;
        (None, Some((to, data, height)))
    };
    let certificate = parsed
        .get("--ca-cert")
        .map(|path| input_file(path, "certificate", MAX_CERTIFICATE_BYTES))
        .transpose()?;
    let roots: Vec<&[u8]> = certificate.as_deref().into_iter().collect();
    let client = SequencerEndpoint::new(required(&parsed, "--rpc-url")?, &roots)
        .map_err(|error| format!("RPC endpoint: {}", rpc_error(&error)))?;
    if let Some(raw) = raw {
        return submit(&client, &raw);
    }
    let Some((to, data, height)) = call else {
        return Err(USAGE.to_owned());
    };
    let (answer, block) = if let Some(height) = height {
        (client.call_at(height, &to, &data), format!("{height:#x}"))
    } else {
        (client.call_pending(&to, &data), "pending".to_owned())
    };
    let answer = answer.map_err(|error| format!("state read refused: {}", rpc_error(&error)))?;
    Ok((
        format!(
            "{{\"block\":\"{block}\",\"to\":\"{}\",\"data\":\"{}\"}}",
            hex(&to),
            hex(&answer)
        ),
        ExitCode::SUCCESS,
    ))
}

fn main() -> ExitCode {
    match run(&std::env::args().skip(1).collect::<Vec<_>>()) {
        Ok((output, status)) => {
            println!("{output}");
            status
        }
        Err(error) => {
            eprintln!("layerxctl: {error}");
            ExitCode::FAILURE
        }
    }
}
