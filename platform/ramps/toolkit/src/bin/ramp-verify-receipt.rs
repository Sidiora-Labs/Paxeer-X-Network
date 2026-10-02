#![forbid(unsafe_code)]

use std::fs::{self, OpenOptions};
use std::io::{Read, Write};
use std::os::unix::fs::{MetadataExt, OpenOptionsExt};
use std::path::{Path, PathBuf};
use std::time::Duration;

use layerx_ramp_toolkit::clients::{
    parse_hex32, LayerxConfig, LayerxSubmission, MutualTlsClient, MutualTlsFiles, SecretFile,
};
use layerx_ramp_toolkit::{RampDirection, RampOrder, EXTERNAL_CUSTODY_LABEL};
use serde::Deserialize;
use serde_json::json;

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct TlsConfig {
    ca_pem: PathBuf,
    identity_pkcs12: PathBuf,
    identity_password_file: PathBuf,
    timeout_seconds: u64,
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct Config {
    client_tls: TlsConfig,
    layerx: LayerxConfig,
}

fn protected(path: &Path) -> Result<Vec<u8>, u8> {
    let file = OpenOptions::new().read(true)
        .custom_flags(rustix::fs::OFlags::NOFOLLOW.bits() as i32)
        .open(path).map_err(|_| 2_u8)?;
    let meta = file.metadata().map_err(|_| 2_u8)?;
    let owner = fs::metadata("/proc/self").map_err(|_| 2_u8)?.uid();
    if !meta.is_file() || meta.mode() & 0o077 != 0 || meta.nlink() != 1
        || meta.uid() != owner || meta.len() == 0 || meta.len() > 65536 {
        return Err(2);
    }
    let mut bytes = Vec::new();
    file.take(65537).read_to_end(&mut bytes).map_err(|_| 2_u8)?;
    if bytes.len() > 65536 { return Err(2); }
    Ok(bytes)
}

fn hex(bytes: &[u8]) -> String {
    bytes.iter().map(|byte| format!("{byte:02x}")).collect()
}

fn run() -> Result<(), u8> {
    let args: Vec<_> = std::env::args_os().skip(1).collect();
    if args.len() != 8 || args[0] != "--config-file" || args[2] != "--order-file"
        || args[4] != "--activity-id" || args[6] != "--output-file" { return Err(2); }
    let config: Config = serde_json::from_slice(&protected(Path::new(&args[1]))?).map_err(|_| 2_u8)?;
    let order: RampOrder = serde_json::from_slice(&protected(Path::new(&args[3]))?).map_err(|_| 2_u8)?;
    order.validate_bound().map_err(|_| 2_u8)?;
    let activity = parse_hex32(args[5].to_str().ok_or(2_u8)?).map_err(|_| 2_u8)?;
    if activity == [0; 32] || config.layerx.network_id == 0
        || !(1..=120).contains(&config.client_tls.timeout_seconds) { return Err(2); }
    let tls = MutualTlsFiles {
        ca_pem: config.client_tls.ca_pem,
        identity_pkcs12: SecretFile::new(config.client_tls.identity_pkcs12).map_err(|_| 2_u8)?,
        identity_password: SecretFile::new(config.client_tls.identity_password_file).map_err(|_| 2_u8)?,
    };
    let http = MutualTlsClient::new(&tls, Duration::from_secs(config.client_tls.timeout_seconds))
        .map_err(|_| 2_u8)?;
    let client = config.layerx.build(http).map_err(|_| 2_u8)?;
    let (submission, evidence, maintained_batch) = client.resolve_with_evidence(&order, activity).map_err(|_| 5_u8)?;
    let leg = match submission {
        LayerxSubmission::Verified { leg, .. } => leg,
        LayerxSubmission::Pending { .. } => return Err(3),
        LayerxSubmission::Unknown { .. } => return Err(4),
        LayerxSubmission::Refused { .. } => return Err(5),
    };
    if !maintained_batch { return Err(5); }
    let evidence = evidence.ok_or(5_u8)?;
    let verified = layerx_proof::receipt::verify(&evidence.canonical_receipt, &evidence.authorized_batch)
        .map_err(|_| 5_u8)?;
    let receipt = verified.receipt().protocol().ok_or(5_u8)?;
    let operation = match order.direction() { RampDirection::OnRamp => 5, RampDirection::OffRamp => 6 };
    if receipt.module_id() != 1 || receipt.operation() != operation || receipt.result_code() != 0
        || receipt.protocol_version() != config.layerx.protocol_version { return Err(5); }
    let result = json!({
        "verified": true, "maintained_batch": maintained_batch, "order_digest": hex(&order.order_digest),
        "activity_id": hex(&leg.activity_id), "receipt_digest": hex(&leg.receipt_digest),
        "batch_id": hex(&leg.batch_id), "resulting_state_root": hex(&leg.resulting_state_root),
        "module_id": receipt.module_id(), "operation": receipt.operation(),
        "result_code": receipt.result_code(), "asset": hex(&receipt.asset()),
        "amount": receipt.amount().to_string(), "from_account": hex(&receipt.from()),
        "to_account": hex(&receipt.to()), "context": hex(&receipt.context_hash()),
        "protocol_version": receipt.protocol_version(), "network_id": config.layerx.network_id,
        "sequencer_public_key": hex(&client.sequencer_authorization.public_key()),
        "verification_level": verified.level().wire_rank(),
        "external_custody_label": EXTERNAL_CUSTODY_LABEL,
        "build_revision": option_env!("LAYERX_RAMP_BUILD_REVISION"),
        "build_source_digest": option_env!("LAYERX_RAMP_BUILD_SOURCE_DIGEST")
    });
    let output = Path::new(&args[7]);
    let parent = output.parent().ok_or(2_u8)?;
    let meta = fs::symlink_metadata(parent).map_err(|_| 2_u8)?;
    if !meta.is_dir() || meta.mode() & 0o077 != 0
        || meta.uid() != fs::metadata("/proc/self").map_err(|_| 2_u8)?.uid() { return Err(2); }
    let mut file = OpenOptions::new().write(true).create_new(true).mode(0o600)
        .custom_flags(rustix::fs::OFlags::NOFOLLOW.bits() as i32)
        .open(output).map_err(|_| 2_u8)?;
    let write = (|| {
        file.write_all(result.to_string().as_bytes())?;
        file.write_all(b"\n")?;
        file.sync_all()
    })();
    if write.is_err() { let _ = fs::remove_file(output); return Err(6); }
    Ok(())
}

fn main() {
    if let Err(code) = run() {
        eprintln!("ramp receipt verification refused ({code})");
        std::process::exit(i32::from(code));
    }
}
