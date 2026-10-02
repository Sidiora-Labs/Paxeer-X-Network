use std::env;
use std::error::Error;
use std::fs;
use std::os::unix::fs::{DirBuilderExt as _, PermissionsExt as _};
use std::path::PathBuf;
use std::sync::atomic::{AtomicU64, Ordering};

use layerx_interop_gateway::principal::PrincipalId;
use layerx_interop_gateway::trace::TraceId;
use layerx_migrate::ethereum::{EthereumConfig, EthereumVerifier};
use layerx_migrate::solana::{SolanaConfig, SolanaVerifier};
use layerx_migrate::history::DurableExternalHistory;
use layerx_migrate::{
    ExternalHistoryRecord, ExternalHistorySink, ExternalProvenance, JournalConfig,
    MigrationError, SourceChain, SourceEvidence, SourceVerifier, VerifiedHistoryPage,
};
use serde::de::DeserializeOwned;

const SECRET_DIRECTORY_MARKER: &str = "${LAYERX_MIGRATION_SECRET_DIR}";
static NEXT_COPY: AtomicU64 = AtomicU64::new(0);

struct JournalCopy(PathBuf);

impl Drop for JournalCopy {
    fn drop(&mut self) {
        let _ = fs::remove_dir_all(&self.0);
    }
}

fn read_history(
    sink: &DurableExternalHistory,
    principal: &PrincipalId,
) -> Result<Vec<ExternalHistoryRecord>, Box<dyn Error>> {
    let mut records = Vec::new();
    let mut after = None;
    loop {
        let page = sink.read(principal, after, 1)?;
        assert!(page.records().len() <= 1);
        for record in page.records() {
            assert!(!records.contains(record), "stored history must not duplicate a source identity");
            records.push(*record);
        }
        assert!(records.len() <= 256, "fixture requires one bounded source page");
        match page.next_cursor() {
            Some(next) => {
                assert_ne!(Some(next), after, "history cursor must advance");
                assert_eq!(page.records().len(), 1);
                after = Some(next);
            }
            None => return Ok(records),
        }
    }
}

fn copied_journal_refuses_damage(config: &JournalConfig, damage: &str) -> Result<(), Box<dyn Error>> {
    let path = env::temp_dir().join(format!(
        "layerx-migration-history-{}-{}",
        std::process::id(), NEXT_COPY.fetch_add(1, Ordering::Relaxed)
    ));
    fs::DirBuilder::new().mode(0o700).create(&path)?;
    let copy = JournalCopy(fs::canonicalize(path)?);
    let directory = copy.0.join(&config.namespace);
    fs::DirBuilder::new().mode(0o700).create(&directory)?;
    let mut records = Vec::new();
    for entry in fs::read_dir(config.directory.join(&config.namespace))? {
        let entry = entry?;
        assert!(entry.file_type()?.is_file());
        let destination = directory.join(entry.file_name());
        fs::copy(entry.path(), &destination)?;
        fs::set_permissions(&destination, fs::Permissions::from_mode(0o600))?;
        if destination.extension().is_some_and(|extension| extension == "record") {
            records.push(destination);
        }
    }
    records.sort();
    let first = records.first().expect("real stored source page creates a journal record");
    match damage {
        "tampered" => {
            let mut bytes = fs::read(first)?;
            let position = bytes.len() / 2;
            bytes[position] ^= 1;
            fs::write(first, bytes)?;
        }
        "permissions" => fs::set_permissions(first, fs::Permissions::from_mode(0o644))?,
        "rollback" => {
            for entry in fs::read_dir(&directory)? {
                fs::remove_file(entry?.path())?;
            }
        }
        _ => panic!("unknown history damage case"),
    }
    let mut copied = config.clone();
    copied.directory = copy.0.clone();
    assert!(matches!(DurableExternalHistory::new(&copied), Err(MigrationError::CheckpointIntegrity)));
    Ok(())
}

fn exercise_history_store(
    variable: &str,
    source_journal: &JournalConfig,
    verified: &VerifiedHistoryPage,
    trace: &TraceId,
) -> Result<(), Box<dyn Error>> {
    assert!(!verified.records().is_empty(), "a genuine nonempty history capture is required");
    let config: JournalConfig = load_config(variable)?;
    assert_ne!(config.directory.join(&config.namespace), source_journal.directory.join(&source_journal.namespace));
    assert_ne!(config.rollback_anchor_id, source_journal.rollback_anchor_id);
    let principal = PrincipalId::new("migration-history-fixture")?;
    let other = PrincipalId::new("migration-history-isolation")?;
    let mut sink = DurableExternalHistory::new(&config)?;
    assert!(sink.read(&principal, None, 256)?.records().is_empty(), "supply a fresh dedicated qualification history namespace");
    assert!(matches!(sink.read(&principal, None, 0), Err(MigrationError::InvalidHistory)));
    assert!(matches!(sink.read(&principal, None, 257), Err(MigrationError::InvalidHistory)));
    sink.store_external(&principal, verified, trace)?;
    sink.store_external(&principal, verified, trace)?;
    assert!(sink.read(&other, None, 256)?.records().is_empty());
    assert!(matches!(sink.read(&other, Some([1; 32]), 1), Err(MigrationError::CheckpointConflict)));
    let stored = read_history(&sink, &principal)?;
    let mut distinct = Vec::new();
    for record in verified.records() {
        if !distinct.contains(record) {
            distinct.push(*record);
        }
        assert!(stored.contains(record));
    }
    assert_eq!(stored.len(), distinct.len());
    drop(sink);
    let mut reopened = DurableExternalHistory::new(&config)?;
    assert_eq!(read_history(&reopened, &principal)?, stored);
    reopened.store_external(&principal, verified, trace)?;
    assert_eq!(read_history(&reopened, &principal)?, stored);
    for damage in ["tampered", "permissions", "rollback"] {
        copied_journal_refuses_damage(&config, damage)?;
    }
    Ok(())
}

fn load_config<T: DeserializeOwned>(variable: &str) -> Result<T, Box<dyn Error>> {
    let path = env::var(variable)?;
    let secret_directory = env::var("LAYERX_MIGRATION_SECRET_DIR")?;
    let encoded = fs::read_to_string(path)?;
    Ok(serde_json::from_str(
        &encoded.replace(SECRET_DIRECTORY_MARKER, &secret_directory),
    )?)
}

fn load_evidence(variable: &str) -> Result<SourceEvidence, Box<dyn Error>> {
    Ok(SourceEvidence::new(fs::read(env::var(variable)?)?)?)
}

fn trace(entropy: u8) -> TraceId {
    TraceId::mint([entropy; 16])
}

#[test]
#[ignore = "requires the protected Ethereum migration testnet environment"]
fn ethereum_account_mapping_uses_live_quorum_and_wallet_signature() -> Result<(), Box<dyn Error>> {
    let config: EthereumConfig = load_config("LAYERX_ETHEREUM_CONFIG")?;
    let expected_chain = config.chain_id;
    let verifier = EthereumVerifier::new(config)?;
    let evidence = load_evidence("LAYERX_ETHEREUM_OWNERSHIP_EVIDENCE")?;
    let ownership = verifier.verify_ownership(&evidence, &trace(0x31))?;
    assert_eq!(
        ownership.chain(),
        SourceChain::Ethereum {
            chain_id: expected_chain
        }
    );
    assert_eq!(ownership.evidence_digest(), evidence.digest());
    Ok(())
}

#[test]
#[ignore = "requires the protected Ethereum migration testnet environment"]
fn ethereum_asset_claim_uses_live_custody_event_and_finality() -> Result<(), Box<dyn Error>> {
    let config: EthereumConfig = load_config("LAYERX_ETHEREUM_CONFIG")?;
    let expected_chain = config.chain_id;
    let verifier = EthereumVerifier::new(config)?;
    let evidence = load_evidence("LAYERX_ETHEREUM_ASSET_EVIDENCE")?;
    let finality = verifier.verify_asset_finality(&evidence, &trace(0x32))?;
    assert_eq!(
        finality.chain(),
        SourceChain::Ethereum {
            chain_id: expected_chain
        }
    );
    assert_eq!(finality.evidence_digest(), evidence.digest());
    Ok(())
}

#[test]
#[ignore = "requires the protected Ethereum migration testnet environment"]
fn ethereum_history_is_live_external_provenance() -> Result<(), Box<dyn Error>> {
    let config: EthereumConfig = load_config("LAYERX_ETHEREUM_CONFIG")?;
    let source_journal = config.journal.clone();
    let verifier = EthereumVerifier::new(config)?;
    let evidence = load_evidence("LAYERX_ETHEREUM_HISTORY_EVIDENCE")?;
    let page = verifier.verify_history(&evidence, &trace(0x33))?;
    assert_eq!(page.evidence_digest(), evidence.digest());
    assert!(page
        .records()
        .iter()
        .all(|record| record.provenance() == ExternalProvenance::Ethereum));
    exercise_history_store("LAYERX_ETHEREUM_HISTORY_STORE_CONFIG", &source_journal, &page, &trace(0x33))?;
    verifier.commit_history(&evidence, &page, &trace(0x33))?;
    Ok(())
}

#[test]
#[ignore = "requires the protected Solana migration testnet environment"]
fn solana_account_mapping_uses_live_quorum_and_wallet_signature() -> Result<(), Box<dyn Error>> {
    let config: SolanaConfig = load_config("LAYERX_SOLANA_CONFIG")?;
    let expected_genesis = config.genesis_hash;
    let verifier = SolanaVerifier::new(config)?;
    let evidence = load_evidence("LAYERX_SOLANA_OWNERSHIP_EVIDENCE")?;
    let ownership = verifier.verify_ownership(&evidence, &trace(0x41))?;
    assert_eq!(
        ownership.chain(),
        SourceChain::Solana {
            genesis_hash: expected_genesis,
        }
    );
    assert_eq!(ownership.evidence_digest(), evidence.digest());
    Ok(())
}

#[test]
#[ignore = "requires the protected Solana migration testnet environment"]
fn solana_asset_claim_uses_live_program_and_finality() -> Result<(), Box<dyn Error>> {
    let config: SolanaConfig = load_config("LAYERX_SOLANA_CONFIG")?;
    let expected_genesis = config.genesis_hash;
    let verifier = SolanaVerifier::new(config)?;
    let evidence = load_evidence("LAYERX_SOLANA_ASSET_EVIDENCE")?;
    let finality = verifier.verify_asset_finality(&evidence, &trace(0x42))?;
    assert_eq!(
        finality.chain(),
        SourceChain::Solana {
            genesis_hash: expected_genesis,
        }
    );
    assert_eq!(finality.evidence_digest(), evidence.digest());
    Ok(())
}

#[test]
#[ignore = "requires the protected Solana migration testnet environment"]
fn solana_history_is_live_external_provenance() -> Result<(), Box<dyn Error>> {
    let config: SolanaConfig = load_config("LAYERX_SOLANA_CONFIG")?;
    let source_journal = config.journal.clone();
    let verifier = SolanaVerifier::new(config)?;
    let evidence = load_evidence("LAYERX_SOLANA_HISTORY_EVIDENCE")?;
    let page = verifier.verify_history(&evidence, &trace(0x43))?;
    assert_eq!(page.evidence_digest(), evidence.digest());
    assert!(page
        .records()
        .iter()
        .all(|record| record.provenance() == ExternalProvenance::Solana));
    exercise_history_store("LAYERX_SOLANA_HISTORY_STORE_CONFIG", &source_journal, &page, &trace(0x43))?;
    verifier.commit_history(&evidence, &page, &trace(0x43))?;
    Ok(())
}
