use layerx_platform_webhooks::encoding::{base64_decode, fixed_base64};
use layerx_platform_webhooks::scheme::{self, Presentation, ReplayGuard};
use layerx_platform_webhooks::WebhookError;
use serde::Deserialize;
use std::collections::BTreeMap;
use std::io::{self, Read};
use std::time::{SystemTime, UNIX_EPOCH};

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct Input {
    body_base64: String,
    headers: BTreeMap<String, String>,
    keys: BTreeMap<String, String>,
}

fn verify() -> Result<(), Box<dyn std::error::Error>> {
    let mut raw = Vec::new();
    io::stdin().take(1_048_577).read_to_end(&mut raw)?;
    if raw.len() > 1_048_576 {
        return Err("delivery input exceeds bound".into());
    }
    let input: Input = serde_json::from_slice(&raw)?;
    let payload = base64_decode(&input.body_base64)?;
    let keys = input
        .keys
        .iter()
        .map(|(id, value)| Ok((id.clone(), fixed_base64::<32>(value)?)))
        .collect::<Result<BTreeMap<_, _>, WebhookError>>()?;
    let header = |name: &str| {
        input
            .headers
            .get(name)
            .map(String::as_str)
            .ok_or("authenticated signature header missing")
    };
    let now = SystemTime::now().duration_since(UNIX_EPOCH)?.as_secs();
    let presentation = Presentation {
        id: header(scheme::ID_HEADER)?,
        timestamp: header(scheme::TIMESTAMP_HEADER)?,
        key_id: header(scheme::KEY_HEADER)?,
        signature: header(scheme::SIGNATURE_HEADER)?,
        payload: &payload,
        now,
        tolerance_seconds: scheme::DEFAULT_TOLERANCE_SECONDS,
    };
    let verified = scheme::verify(&presentation, &keys)?;
    let mut replay = ReplayGuard::new(scheme::DEFAULT_TOLERANCE_SECONDS, 2)?;
    replay.admit(&verified.id, verified.timestamp, now)?;
    if !matches!(
        replay.admit(&verified.id, verified.timestamp, now),
        Err(WebhookError::ReplayRejected)
    ) {
        return Err("duplicate delivery was admitted".into());
    }
    let mut changed = payload.clone();
    changed.push(b' ');
    let tampered = Presentation {
        payload: &changed,
        ..presentation
    };
    if scheme::verify(&tampered, &keys).is_ok() {
        return Err("altered delivery verified".into());
    }
    let event: serde_json::Value = serde_json::from_slice(&payload)?;
    if event["event"]["id"].as_str() != Some(verified.id.as_str()) {
        return Err("signed event identity mismatch".into());
    }
    println!("signed delivery verified; duplicate and altered bytes refused");
    Ok(())
}
fn main() {
    if let Err(error) = verify() {
        eprintln!("verify-delivery: {error}");
        std::process::exit(1);
    }
}
