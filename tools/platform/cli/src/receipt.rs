use std::fs;
use std::path::Path;

use layerx_proof::receipt::{verify_outcome, AuthorizedBatch};
use serde_json::{json, Value};

use crate::encoding::{fixed_hex, hex_decode, hex_encode};

#[derive(Clone, Copy)]
pub struct VerificationFacts<'a> {
    pub batch_id: &'a str,
    pub asset: &'a str,
    pub previous_state_root: &'a str,
    pub resulting_state_root: &'a str,
    pub sequencer_public_key: &'a str,
}

pub fn verify_file(path: &Path, facts: VerificationFacts<'_>) -> Result<Value, String> {
    let source = fs::read(path)
        .map_err(|error| format!("could not read receipt {}: {error}", path.display()))?;
    let receipt = decode_receipt(&source)?;
    verify_bytes(&receipt, facts)
}

pub fn verify_bytes(receipt: &[u8], facts: VerificationFacts<'_>) -> Result<Value, String> {
    let authorised = AuthorizedBatch::new(
        fixed_hex("batch id", facts.batch_id)?,
        fixed_hex("asset", facts.asset)?,
        fixed_hex("previous state root", facts.previous_state_root)?,
        fixed_hex("resulting state root", facts.resulting_state_root)?,
        fixed_hex("sequencer public key", facts.sequencer_public_key)?,
    );
    let verified = verify_outcome(receipt, &authorised)
        .map_err(|failure| format!("receipt verification failed at {:?}", failure.check))?;
    let protocol = verified
        .receipt()
        .protocol()
        .ok_or_else(|| "verified receipt did not contain protocol facts".to_string())?;
    let digest = verified
        .evidence()
        .receipt_digest()
        .ok_or_else(|| "receipt verifier did not produce a digest".to_string())?;
    Ok(json!({
        "verified": true,
        "verification_level": verified.level().wire_rank(),
        "receipt_digest": hex_encode(&digest),
        "activity_id": hex_encode(&protocol.activity_id()),
        "batch_id": hex_encode(&protocol.batch_id()),
        "result_code": protocol.result_code(),
        "canonical_bytes": verified.canonical_bytes().len(),
    }))
}

fn decode_receipt(source: &[u8]) -> Result<Vec<u8>, String> {
    if let Ok(text) = std::str::from_utf8(source) {
        let trimmed = text.trim();
        if !trimmed.is_empty() && trimmed.bytes().all(|byte| byte.is_ascii_hexdigit()) {
            return hex_decode("receipt", trimmed);
        }
    }
    if source.is_empty() {
        return Err("receipt file is empty".into());
    }
    Ok(source.to_vec())
}
