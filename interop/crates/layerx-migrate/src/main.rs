use std::collections::BTreeMap;
use std::env;
use std::fs::{self, File};
use std::io::{Read, Write};
use std::os::unix::fs::{MetadataExt, PermissionsExt};
use std::path::Path;

use layerx_interop_gateway::principal::PrincipalId;
use layerx_interop_gateway::trace::TraceId;
use layerx_migrate::ethereum::{EthereumConfig, EthereumVerifier};
use layerx_migrate::gateway_client::{GatewayClient, GatewayClientConfig};
use layerx_migrate::history::DurableExternalHistory;
use layerx_migrate::mapping_v2::{PaxeerBindingConfigV2, PaxeerBindingVerifierV2};
use layerx_migrate::solana::{SolanaConfig, SolanaVerifier};
use layerx_migrate::{
    AccountMappingStoreV2, ExternalAddress, ExternalHistorySink, ExternalProvenance, JournalConfig,
    MigrationError, SourceChain, SourceEvidence, SourceTransaction, SourceVerifier,
};
use serde::de::DeserializeOwned;
use serde_json::{json, Value};

fn hex(bytes: &[u8]) -> String {
    use std::fmt::Write as _;
    let mut output = String::with_capacity(bytes.len() * 2);
    for byte in bytes {
        let _ = write!(output, "{byte:02x}");
    }
    output
}

fn private_input(path: &str, maximum: usize) -> Result<Vec<u8>, MigrationError> {
    let path = Path::new(path);
    let before = fs::symlink_metadata(path).map_err(|_| MigrationError::Configuration)?;
    if !path.is_absolute()
        || fs::canonicalize(path).ok().as_deref() != Some(path)
        || !before.is_file()
        || before.nlink() != 1
        || before.permissions().mode() & 0o077 != 0
        || before.len() == 0
        || before.len() > maximum as u64
    {
        return Err(MigrationError::Configuration);
    }
    let mut file = File::open(path).map_err(|_| MigrationError::Configuration)?;
    let after = file.metadata().map_err(|_| MigrationError::Configuration)?;
    if before.dev() != after.dev()
        || before.ino() != after.ino()
        || before.uid() != after.uid()
        || before.mode() != after.mode()
    {
        return Err(MigrationError::Configuration);
    }
    let mut output = Vec::new();
    Read::by_ref(&mut file)
        .take(maximum as u64 + 1)
        .read_to_end(&mut output)
        .map_err(|_| MigrationError::Configuration)?;
    if output.is_empty() || output.len() > maximum {
        return Err(MigrationError::Configuration);
    }
    Ok(output)
}

fn config<T: DeserializeOwned>(path: &str) -> Result<T, MigrationError> {
    let bytes = private_input(path, 256 * 1024)?;
    let text = std::str::from_utf8(&bytes).map_err(|_| MigrationError::Configuration)?;
    let marker = "${LAYERX_MIGRATION_SECRET_DIR}";
    let text = if text.contains(marker) {
        let directory =
            env::var("LAYERX_MIGRATION_SECRET_DIR").map_err(|_| MigrationError::Configuration)?;
        let path = Path::new(&directory);
        if !path.is_absolute() || fs::canonicalize(path).ok().as_deref() != Some(path) {
            return Err(MigrationError::Configuration);
        }
        text.replace(marker, &directory)
    } else {
        text.to_owned()
    };
    serde_json::from_str(&text).map_err(|_| MigrationError::Configuration)
}

fn chain(value: SourceChain) -> Value {
    match value {
        SourceChain::Ethereum { chain_id } => {
            json!({"chain":"ethereum", "chain_id":chain_id.to_string()})
        }
        SourceChain::Solana { genesis_hash } => {
            json!({"chain":"solana", "genesis_hash":hex(&genesis_hash)})
        }
    }
}

fn address(value: ExternalAddress) -> String {
    match value {
        ExternalAddress::Ethereum(value) => hex(&value),
        ExternalAddress::Solana(value) => hex(&value),
    }
}

fn transaction(value: SourceTransaction) -> String {
    match value {
        SourceTransaction::Ethereum(value) => hex(&value),
        SourceTransaction::Solana(value) => hex(&value),
    }
}

fn required<'a>(args: &'a BTreeMap<String, String>, key: &str) -> Result<&'a str, MigrationError> {
    args.get(key)
        .map(String::as_str)
        .ok_or(MigrationError::Configuration)
}

fn run() -> Result<Value, MigrationError> {
    let mut values = env::args().skip(1);
    let command = values.next().ok_or(MigrationError::Configuration)?;
    let mut args = BTreeMap::new();
    while let Some(key) = values.next() {
        if ![
            "--chain",
            "--config",
            "--evidence",
            "--store-config",
            "--principal",
            "--trace",
            "--cursor",
            "--limit",
            "--binding-config",
            "--mapping-config",
            "--identity",
            "--gateway-config",
            "--order-digest",
            "--idempotency",
        ]
        .contains(&key.as_str())
        {
            return Err(MigrationError::Configuration);
        }
        let value = values.next().ok_or(MigrationError::Configuration)?;
        if args.insert(key, value).is_some() {
            return Err(MigrationError::Configuration);
        }
    }
    let allowed: &[&str] = match command.as_str() {
        "migrate-asset" => &[
            "--chain",
            "--evidence",
            "--gateway-config",
            "--order-digest",
            "--idempotency",
            "--trace",
        ],
        "confirm-mapping" => &[
            "--chain",
            "--config",
            "--evidence",
            "--principal",
            "--trace",
            "--binding-config",
            "--mapping-config",
            "--identity",
        ],
        "verify-ownership" | "verify-asset" => &["--chain", "--config", "--evidence", "--trace"],
        "import-history" => &[
            "--chain",
            "--config",
            "--evidence",
            "--store-config",
            "--principal",
            "--trace",
        ],
        "read-history" => &[
            "--store-config",
            "--principal",
            "--trace",
            "--cursor",
            "--limit",
        ],
        _ => return Err(MigrationError::Configuration),
    };
    if args.keys().any(|key| !allowed.contains(&key.as_str())) {
        return Err(MigrationError::Configuration);
    }
    let mut entropy = [0_u8; 16];
    File::open("/dev/urandom")
        .and_then(|mut file| file.read_exact(&mut entropy))
        .map_err(|_| MigrationError::Configuration)?;
    let trace = match args.get("--trace") {
        Some(value) => TraceId::parse(value).map_err(|_| MigrationError::Configuration)?,
        None => TraceId::mint(entropy),
    };
    if command == "migrate-asset" {
        let text = required(&args, "--order-digest")?;
        if text.len() != 64 || !text.bytes().all(|value| value.is_ascii_hexdigit()) {
            return Err(MigrationError::InvalidEvidence);
        }
        let mut order_digest = [0_u8; 32];
        for (index, byte) in order_digest.iter_mut().enumerate() {
            *byte = u8::from_str_radix(&text[index * 2..index * 2 + 2], 16)
                .map_err(|_| MigrationError::InvalidEvidence)?;
        }
        let evidence =
            SourceEvidence::new(private_input(required(&args, "--evidence")?, 1024 * 1024)?)?;
        let client = GatewayClient::new(&config::<GatewayClientConfig>(required(
            &args,
            "--gateway-config",
        )?)?)?;
        let observation = client.migrate_asset(
            order_digest,
            required(&args, "--chain")?,
            &evidence,
            required(&args, "--idempotency")?,
            &trace,
        )?;
        return Ok(json!({"ok":true, "result":observation, "trace":trace.as_str()}));
    }
    if command == "confirm-mapping" {
        let principal = PrincipalId::new(required(&args, "--principal")?)
            .map_err(|_| MigrationError::Configuration)?;
        let text = required(&args, "--identity")?;
        if text.len() != 64 || !text.bytes().all(|value| value.is_ascii_hexdigit()) {
            return Err(MigrationError::InvalidEvidence);
        }
        let mut identity = [0_u8; 32];
        for (index, byte) in identity.iter_mut().enumerate() {
            *byte = u8::from_str_radix(&text[index * 2..index * 2 + 2], 16)
                .map_err(|_| MigrationError::InvalidEvidence)?;
        }
        let evidence =
            SourceEvidence::new(private_input(required(&args, "--evidence")?, 1024 * 1024)?)?;
        let binding = PaxeerBindingVerifierV2::new(config::<PaxeerBindingConfigV2>(required(
            &args,
            "--binding-config",
        )?)?)?;
        let store = AccountMappingStoreV2::new(&config::<JournalConfig>(required(
            &args,
            "--mapping-config",
        )?)?)?;
        let mapping = match required(&args, "--chain")? {
            "ethereum" => store.confirm(
                &principal,
                identity,
                &evidence,
                &EthereumVerifier::new(config::<EthereumConfig>(required(&args, "--config")?)?)?,
                &binding,
                &trace,
            )?,
            "solana" => store.confirm(
                &principal,
                identity,
                &evidence,
                &SolanaVerifier::new(config::<SolanaConfig>(required(&args, "--config")?)?)?,
                &binding,
                &trace,
            )?,
            _ => return Err(MigrationError::InvalidNetwork),
        };
        return Ok(
            json!({"ok":true, "result":{"state":"external_mapping_confirmed", "mapping":mapping}, "trace":trace.as_str()}),
        );
    }
    if command == "read-history" {
        let store = DurableExternalHistory::new(&config::<JournalConfig>(required(
            &args,
            "--store-config",
        )?)?)?;
        let principal = PrincipalId::new(required(&args, "--principal")?)
            .map_err(|_| MigrationError::Configuration)?;
        let cursor = args
            .get("--cursor")
            .map(|text| {
                if text.len() != 64 || !text.bytes().all(|value| value.is_ascii_hexdigit()) {
                    return Err(MigrationError::Configuration);
                }
                let mut cursor = [0_u8; 32];
                for (index, byte) in cursor.iter_mut().enumerate() {
                    *byte = u8::from_str_radix(&text[index * 2..index * 2 + 2], 16)
                        .map_err(|_| MigrationError::Configuration)?;
                }
                Ok(cursor)
            })
            .transpose()?;
        let limit = args.get("--limit").map_or(Ok(256), |text| {
            text.parse::<usize>()
                .map_err(|_| MigrationError::Configuration)
        })?;
        let page = store.read(&principal, cursor, limit)?;
        let records: Vec<_> = page.records().iter().map(|record| json!({
            "network":chain(record.chain()), "transaction":transaction(record.transaction()),
            "address":address(record.address()), "kind":format!("{:?}", record.kind()),
            "timestamp":record.timestamp().to_string(), "asset":hex(&record.source_asset()),
            "amount":record.source_amount().to_string(),
            "provenance": match record.provenance() { ExternalProvenance::Ethereum => "ethereum-external", ExternalProvenance::Solana => "solana-external" },
            "layerx_receipt":false,
        })).collect();
        return Ok(
            json!({"ok":true, "result":{"state":"external-history", "records":records,
            "next_cursor":page.next_cursor().map(|value| hex(&value))}, "trace":trace.as_str()}),
        );
    }
    let verifier: Box<dyn SourceVerifier> = match required(&args, "--chain")? {
        "ethereum" => Box::new(EthereumVerifier::new(config::<EthereumConfig>(required(
            &args, "--config",
        )?)?)?),
        "solana" => Box::new(SolanaVerifier::new(config::<SolanaConfig>(required(
            &args, "--config",
        )?)?)?),
        _ => return Err(MigrationError::InvalidNetwork),
    };
    let evidence =
        SourceEvidence::new(private_input(required(&args, "--evidence")?, 1024 * 1024)?)?;
    let result = match command.as_str() {
        "verify-ownership" => {
            let ownership = verifier.verify_ownership(&evidence, &trace)?;
            json!({"state":"source-ownership-verified", "network":chain(ownership.chain()),
                "address":address(ownership.address()), "layerx_identity":hex(&ownership.layerx_identity()),
                "evidence_digest":hex(&ownership.evidence_digest()), "layerx_bound":false})
        }
        "verify-asset" => {
            let finality = verifier.verify_asset_finality(&evidence, &trace)?;
            json!({"state":"source-finality-verified", "network":chain(finality.chain()),
                "transaction":transaction(finality.transaction()), "source":address(finality.source()),
                "source_asset":hex(&finality.source_asset()), "source_amount":finality.source_amount().to_string(),
                "custody_reference":hex(&finality.custody_reference()), "layerx_asset":hex(&finality.layerx_asset()),
                "layerx_amount":finality.layerx_amount().to_string(), "destination":hex(&finality.destination()),
                "height":finality.finality_height().to_string(), "evidence_digest":hex(&finality.evidence_digest()),
                "layerx_credited":false})
        }
        "import-history" => {
            let principal = PrincipalId::new(required(&args, "--principal")?)
                .map_err(|_| MigrationError::Configuration)?;
            let mut store = DurableExternalHistory::new(&config::<JournalConfig>(required(
                &args,
                "--store-config",
            )?)?)?;
            let page = verifier.verify_history(&evidence, &trace)?;
            store.store_external(&principal, &page, &trace)?;
            verifier.commit_history(&evidence, &page, &trace)?;
            json!({"state":"external-history-imported", "record_count":page.records().len(),
                "next_cursor":page.next_cursor().map(|value| hex(&value)),
                "evidence_digest":hex(&page.evidence_digest()), "layerx_receipt":false})
        }
        _ => return Err(MigrationError::Configuration),
    };
    Ok(json!({"ok":true, "result":result, "trace":trace.as_str()}))
}

fn main() {
    let (result, status) = match run() {
        Ok(result) => (result, 0),
        Err(error) => (
            json!({"ok":false, "error":{"code":error.code(), "message":error.to_string(),
            "retry_after_seconds":match error { MigrationError::RpcRateLimited { retry_after_seconds } => Some(retry_after_seconds), _ => None }}}),
            1,
        ),
    };
    let written = writeln!(std::io::stdout(), "{result}");
    std::process::exit(if written.is_ok() { status } else { 1 });
}
