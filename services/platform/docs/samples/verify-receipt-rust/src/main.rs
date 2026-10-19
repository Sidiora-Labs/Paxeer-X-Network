use std::env;
use std::fs;
use std::process::ExitCode;

pub struct BatchFacts {
    pub batch_id: [u8; 32],
    pub asset: [u8; 32],
    pub previous_state_root: [u8; 32],
    pub resulting_state_root: [u8; 32],
    pub sequencer_public_key: [u8; 32],
}

pub struct Settlement {
    pub level: u8,
    pub amount: u128,
    pub result_code: i32,
    pub digest: [u8; 32],
}

// layerx:begin integration
use layerx_proof::receipt::AuthorizedBatch;
use layerx_sdk::production::verify_receipt;

fn settlement(receipt: &[u8], batch: &BatchFacts) -> Result<Settlement, String> {
    let authorised = AuthorizedBatch::new(batch.batch_id, batch.asset, batch.previous_state_root, batch.resulting_state_root, batch.sequencer_public_key);
    let verified = verify_receipt(receipt, &authorised).map_err(|failure| format!("receipt refused at {:?}", failure.check))?;
    let facts = verified.receipt().protocol().ok_or("receipt carries no protocol facts")?;
    Ok(Settlement { level: verified.level().wire_rank(), amount: facts.amount(), result_code: facts.result_code(), digest: verified.evidence().receipt_digest().ok_or("verifier produced no digest")? })
}
// layerx:end integration

fn main() -> ExitCode {
    match run() {
        Ok(report) => {
            println!("{report}");
            ExitCode::SUCCESS
        }
        Err(reason) => {
            eprintln!("verify-receipt-rust: {reason}");
            ExitCode::from(2)
        }
    }
}

fn run() -> Result<String, String> {
    let path = required("LAYERX_RECEIPT_FILE")?;
    let receipt = read_receipt(&path)?;
    let batch = BatchFacts {
        batch_id: fixed_hex("LAYERX_BATCH_ID")?,
        asset: fixed_hex("LAYERX_ASSET")?,
        previous_state_root: fixed_hex("LAYERX_PREVIOUS_STATE_ROOT")?,
        resulting_state_root: fixed_hex("LAYERX_RESULTING_STATE_ROOT")?,
        sequencer_public_key: fixed_hex("LAYERX_SEQUENCER_PUBLIC_KEY")?,
    };
    let settled = settlement(&receipt, &batch)?;
    if settled.result_code != 0 {
        return Err(format!(
            "protocol refused the activity with result code {}",
            settled.result_code
        ));
    }
    Ok(format!(
        "{{\"verified\":true,\"verification_level\":{},\"amount\":\"{}\",\"result_code\":{},\"receipt_digest\":\"{}\"}}",
        settled.level,
        settled.amount,
        settled.result_code,
        hex_encode(&settled.digest)
    ))
}

fn read_receipt(path: &str) -> Result<Vec<u8>, String> {
    let source = fs::read(path).map_err(|error| format!("could not read {path}: {error}"))?;
    if let Ok(text) = std::str::from_utf8(&source) {
        let trimmed = text.trim();
        if !trimmed.is_empty() && trimmed.len() % 2 == 0 && trimmed.bytes().all(|byte| byte.is_ascii_hexdigit()) {
            return hex_decode(trimmed);
        }
    }
    if source.is_empty() {
        return Err(format!("{path} is empty"));
    }
    Ok(source)
}

fn required(name: &str) -> Result<String, String> {
    match env::var(name) {
        Ok(value) if !value.is_empty() => Ok(value),
        _ => Err(format!("missing {name}")),
    }
}

fn fixed_hex(name: &str) -> Result<[u8; 32], String> {
    let decoded = hex_decode(&required(name)?)?;
    <[u8; 32]>::try_from(decoded.as_slice())
        .map_err(|_| format!("{name} must be 32 hexadecimal bytes"))
}

fn hex_decode(value: &str) -> Result<Vec<u8>, String> {
    let digits = value.strip_prefix("0x").unwrap_or(value);
    if digits.is_empty() || digits.len() % 2 != 0 {
        return Err("hexadecimal input must contain an even number of digits".to_owned());
    }
    let bytes = digits.as_bytes();
    let mut decoded = Vec::with_capacity(digits.len() / 2);
    for pair in bytes.chunks_exact(2) {
        let high = (pair[0] as char)
            .to_digit(16)
            .ok_or_else(|| "hexadecimal input contains a non-hexadecimal digit".to_owned())?;
        let low = (pair[1] as char)
            .to_digit(16)
            .ok_or_else(|| "hexadecimal input contains a non-hexadecimal digit".to_owned())?;
        decoded.push(((high << 4) | low) as u8);
    }
    Ok(decoded)
}

fn hex_encode(value: &[u8]) -> String {
    let mut encoded = String::with_capacity(value.len() * 2);
    for byte in value {
        encoded.push(char::from_digit((byte >> 4) as u32, 16).unwrap_or('0'));
        encoded.push(char::from_digit((byte & 0x0f) as u32, 16).unwrap_or('0'));
    }
    encoded
}
