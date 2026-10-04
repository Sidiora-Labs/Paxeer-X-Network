//! Deployable independent-worker mirror publisher runtime.

use std::collections::BTreeMap;
use std::fs;
use std::io::{Read, Write};
use std::net::{IpAddr, SocketAddr, TcpListener, TcpStream};
use std::path::{Path, PathBuf};
use std::sync::{Arc, Mutex};
use std::thread;
use std::time::Duration;

use serde::{Deserialize, Serialize};

use crate::ethereum::{
    EthereumArchiveClient, EthereumError, EthereumProductionConfig, EthereumProgress,
};
use crate::node::{CheckpointAcquisition, LniArchiveSource, NodeSourceConfig};
use crate::rpc::{RpcCluster, RpcQuorumConfig};
use crate::signer::{RemoteChainSigner, RemoteSignerConfig, SignerEndpoint, SigningAlgorithm};
use crate::solana::{SolanaArchiveClient, SolanaError, SolanaProductionConfig, SolanaProgress};
use crate::store::{ArchiveSpool, PublicationPhase};
use crate::{Archive, ArchiveCommitment, ChainProgress, ProgressGate};

const MAX_CONFIG_BYTES: usize = 1024 * 1024;
const MAX_STATUS_REQUEST_BYTES: usize = 4096;

#[derive(Clone, Debug, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct RuntimeConfig {
    pub state_directory: PathBuf,
    pub first_batch_number: u64,
    pub poll_interval_ms: u64,
    /// Largest number of batches a chain's mirrored checkpoint may trail the
    /// latest certificate-verified checkpoint while that chain reports ready.
    pub checkpoint_freshness_budget_batches: u64,
    pub status_listen: SocketAddr,
    pub node: NodeFileConfig,
    pub ethereum: EthereumFileConfig,
    /// Absent when the deployment mirrors to the EVM chain only. A present
    /// section is validated exactly as a required one.
    #[serde(default)]
    pub solana: Option<SolanaFileConfig>,
}

#[derive(Clone, Debug, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct NodeFileConfig {
    pub socket: PathBuf,
    pub expected_protocol_version: u16,
    pub expected_network_id: u32,
    pub maximum_frame_bytes: usize,
    pub maximum_connections: usize,
    pub maximum_streams: usize,
    pub maximum_queued_bytes: usize,
    pub deadline_ms: u64,
    pub maximum_archive_bytes: usize,
    pub maximum_archive_chunks: usize,
    pub checkpoint_policy: crate::node::NativeCheckpointPolicy,
}

#[derive(Clone, Debug, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct EthereumFileConfig {
    pub rpc: RpcQuorumConfig,
    pub chain_id: u64,
    pub genesis_hash_hex: String,
    pub archive_contract_hex: String,
    pub archive_code_hash_hex: String,
    pub required_confirmations: u64,
    pub maximum_reorg_depth: u64,
    pub chunk_bytes: usize,
    pub transaction_gas_limit: u64,
    pub maximum_fee_per_gas: u128,
    pub maximum_priority_fee_per_gas: u128,
    pub signer: SignerFileConfig,
}

#[derive(Clone, Debug, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct SolanaFileConfig {
    pub rpc: RpcQuorumConfig,
    pub genesis_hash_base58: String,
    pub archive_program_base58: String,
    pub upgradeable_loader_base58: String,
    pub program_data_account_base58: String,
    pub program_code_hash_hex: String,
    pub required_rooted_slots: u64,
    pub maximum_ancestry: u64,
    pub chunk_bytes: usize,
    pub signer: SignerFileConfig,
}

#[derive(Clone, Debug, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct SignerFileConfig {
    pub key_handle: String,
    pub public_key: String,
    pub timeout_ms: u64,
    pub transport: SignerTransportFileConfig,
}

#[derive(Clone, Debug, Deserialize)]
#[serde(tag = "kind", rename_all = "snake_case", deny_unknown_fields)]
pub enum SignerTransportFileConfig {
    Uds {
        socket: PathBuf,
    },
    MutualTls {
        endpoint: SocketAddr,
        server_name: String,
        trust_anchor: PathBuf,
        client_certificate: PathBuf,
        client_private_key: PathBuf,
    },
}

#[derive(Clone, Debug, Default, Serialize)]
struct ComponentStatus {
    ready: bool,
    latest_batch_mirrored: Option<u64>,
    latest_batch_observed: Option<u64>,
    latest_checkpoint_batch_mirrored: Option<u64>,
    latest_checkpoint_id_mirrored: Option<String>,
    latest_batch_verified: Option<u64>,
    latest_checkpoint_batch_verified: Option<u64>,
    checkpoint_batch_lag: Option<u64>,
    freshness_budget_batches: u64,
    freshness: Option<&'static str>,
    checkpoint_within_budget: bool,
    phase: Option<&'static str>,
    error_class: Option<&'static str>,
    reorgs_observed: u64,
}

#[derive(Clone, Debug, Default, Serialize)]
struct RuntimeStatus {
    source_chain_id: u64,
    native_network_id: u32,
    native_protocol_version: u16,
    service_version: String,
    node: ComponentStatus,
    ethereum: ComponentStatus,
    solana: Option<ComponentStatus>,
    checkpoint_proof_boundary_ready: bool,
    checkpoint_identifier_observed: bool,
    latest_verified_checkpoint_batch: Option<u64>,
    latest_verified_checkpoint_id: Option<String>,
    checkpoint_refusal: Option<&'static str>,
    checkpoint_freshness_budget_batches: u64,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum RuntimeError {
    Arguments,
    Configuration,
    State,
    Status,
}

#[derive(Debug)]
enum SpoolRecoveryError {
    Store,
    Archive,
    BatchBeforeFirst,
    DuplicateBatch,
    BatchGap,
    SequenceExhausted,
}

/// Loads one bounded JSON configuration and starts node acquisition, two
/// independent durable chain workers and a redacted loopback status endpoint.
///
/// # Errors
/// Returns an error for invalid configuration, inaccessible durable state or status listener failure.
pub fn run(config_path: &Path) -> Result<(), RuntimeError> {
    let metadata = fs::metadata(config_path).map_err(|_| RuntimeError::Configuration)?;
    if metadata.len() == 0 || metadata.len() > MAX_CONFIG_BYTES as u64 {
        return Err(RuntimeError::Configuration);
    }
    let bytes = fs::read(config_path).map_err(|_| RuntimeError::Configuration)?;
    let config: RuntimeConfig =
        serde_json::from_slice(&bytes).map_err(|_| RuntimeError::Configuration)?;
    validate_runtime(&config)?;
    let spool = ArchiveSpool::open(config.state_directory.join("archives"))
        .map_err(|_| RuntimeError::State)?;
    let next_batch =
        recover_next_batch(&spool, config.first_batch_number).map_err(|_| RuntimeError::State)?;
    let status = Arc::new(Mutex::new(RuntimeStatus {
        source_chain_id: config.node.checkpoint_policy.chain_id,
        native_network_id: config.node.expected_network_id,
        native_protocol_version: config.node.expected_protocol_version,
        service_version: env!("CARGO_PKG_VERSION").to_owned(),
        solana: config.solana.as_ref().map(|_| ComponentStatus::default()),
        checkpoint_freshness_budget_batches: config.checkpoint_freshness_budget_batches,
        ..RuntimeStatus::default()
    }));
    let poll = Duration::from_millis(config.poll_interval_ms);

    spawn_status(config.status_listen, Arc::clone(&status))?;
    spawn_node(
        config.clone(),
        spool.clone(),
        Arc::clone(&status),
        poll,
        next_batch,
    );
    spawn_ethereum(config.clone(), spool.clone(), Arc::clone(&status), poll);
    if let Some(solana) = config.solana.clone() {
        spawn_solana(config, solana, spool, status, poll);
    }
    loop {
        thread::park();
    }
}

fn spawn_node(
    config: RuntimeConfig,
    spool: ArchiveSpool,
    status: Arc<Mutex<RuntimeStatus>>,
    poll: Duration,
    mut next_batch: u64,
) {
    thread::spawn(move || {
        let source_config = node_config(&config.node);
        let mut source = LniArchiveSource::new(source_config);
        loop {
            match source.acquire(next_batch) {
                Ok(acquired) => {
                    update_status(&status, |value| {
                        value.node.latest_batch_observed = Some(acquired.head.latest_sealed_batch);
                        record_checkpoint(value, &acquired.checkpoint);
                    });
                    if !acquired
                        .checkpoint
                        .coordinate()
                        .is_some_and(|checkpoint| checkpoint.batch_number >= next_batch)
                    {
                        update_status(&status, |value| {
                            value.node.ready = false;
                            value.node.phase = Some("awaiting_verified_checkpoint");
                        });
                        thread::sleep(poll);
                        continue;
                    }
                    let material = match &acquired.checkpoint {
                        CheckpointAcquisition::Verified(verified) => Some(verified.material()),
                        CheckpointAcquisition::NodeHasNoCheckpoint
                        | CheckpointAcquisition::Refused { .. } => None,
                    };
                    match Archive::from_node(
                        &acquired.batch,
                        &acquired.availability,
                        material,
                        acquired.head,
                    )
                    .and_then(|archive| {
                        if archive.data().batch_number != next_batch {
                            return Err(crate::SourceError::BatchMismatch);
                        }
                        spool
                            .put(archive.commitment(), archive.bytes(), archive.node_head())
                            .map_err(|_| {
                                crate::SourceError::Archive(crate::ArchiveError::Format)
                            })?;
                        Ok(archive)
                    }) {
                        Ok(_) => {
                            update_status(&status, |value| {
                                value.node.ready = true;
                                value.node.latest_batch_mirrored = Some(next_batch);
                                value.node.error_class = None;
                                value.node.phase = Some("spooled_verified_batch");
                                record_checkpoint(value, &acquired.checkpoint);
                            });
                            let Some(incremented) = next_batch.checked_add(1) else {
                                update_status(&status, |value| {
                                    value.node.ready = false;
                                    value.node.error_class = Some("batch_sequence_exhausted");
                                });
                                return;
                            };
                            next_batch = incremented;
                        }
                        Err(_) => update_status(&status, |value| {
                            value.node.ready = false;
                            value.node.error_class = Some("archive_evidence");
                        }),
                    }
                }
                Err(error) => match source.observe_checkpoint() {
                    Ok((head, checkpoint))
                        if head.latest_sealed_batch.checked_add(1) == Some(next_batch) =>
                    {
                        update_status(&status, |value| {
                            value.node.latest_batch_observed = Some(head.latest_sealed_batch);
                            record_checkpoint(value, &checkpoint);
                            value.node.ready =
                                matches!(checkpoint, CheckpointAcquisition::Verified(_));
                            value.node.error_class = None;
                            value.node.phase = Some("head_reconciled");
                        });
                    }
                    _ => update_status(&status, |value| {
                        value.node.ready = false;
                        value.node.error_class = Some(node_error_class(&error));
                    }),
                },
            }
            thread::sleep(poll);
        }
    });
}

fn spawn_ethereum(
    config: RuntimeConfig,
    spool: ArchiveSpool,
    status: Arc<Mutex<RuntimeStatus>>,
    poll: Duration,
) {
    thread::spawn(move || {
        let mut client = loop {
            match ethereum_client(&config) {
                Ok(value) => break value,
                Err(error) => {
                    update_status(&status, |value| {
                        value.ethereum.error_class = Some(ethereum_error_class(&error));
                    });
                    thread::sleep(poll);
                }
            }
        };
        let mut gate = ProgressGate::new();
        loop {
            let ordered = ordered_archives(&spool);
            let mut publication_error = None;
            for archive in &ordered {
                match archive {
                    Ok(archive) => match client.advance(archive) {
                        Ok(progress) => update_ethereum(&status, progress),
                        Err(error) => {
                            let error_class = ethereum_error_class(&error);
                            publication_error.get_or_insert(error_class);
                            update_status(&status, |value| {
                                value.ethereum.ready = false;
                                value.ethereum.error_class = Some(error_class);
                            });
                        }
                    },
                    Err(()) => {
                        publication_error.get_or_insert("archive_spool");
                        update_status(&status, |value| {
                            value.ethereum.ready = false;
                            value.ethereum.error_class = Some("archive_spool");
                        });
                    }
                }
            }
            let archives = ordered.into_iter().flatten().collect::<Vec<_>>();
            let progress = gate.acknowledge(client.cursor(), &archives, |commitment| {
                client.retrieve_finalized(commitment)
            });
            apply_ethereum_progress(&status, progress, publication_error);
            thread::sleep(poll);
        }
    });
}

fn spawn_solana(
    config: RuntimeConfig,
    solana: SolanaFileConfig,
    spool: ArchiveSpool,
    status: Arc<Mutex<RuntimeStatus>>,
    poll: Duration,
) {
    thread::spawn(move || {
        let mut client = loop {
            match solana_client(&config, &solana) {
                Ok(value) => break value,
                Err(error) => {
                    update_solana_status(&status, |value| {
                        value.error_class = Some(solana_error_class(&error));
                    });
                    thread::sleep(poll);
                }
            }
        };
        let mut gate = ProgressGate::new();
        loop {
            let ordered = ordered_archives(&spool);
            let mut publication_error = None;
            for archive in &ordered {
                match archive {
                    Ok(archive) => match client.advance(archive) {
                        Ok(progress) => update_solana(&status, progress),
                        Err(error) => {
                            let error_class = solana_error_class(&error);
                            publication_error.get_or_insert(error_class);
                            update_solana_status(&status, |value| {
                                value.ready = false;
                                value.error_class = Some(error_class);
                            });
                        }
                    },
                    Err(()) => {
                        publication_error.get_or_insert("archive_spool");
                        update_solana_status(&status, |value| {
                            value.ready = false;
                            value.error_class = Some("archive_spool");
                        });
                    }
                }
            }
            let archives = ordered.into_iter().flatten().collect::<Vec<_>>();
            let progress = gate.acknowledge(client.cursor(), &archives, |commitment| {
                client.retrieve_finalized(commitment)
            });
            apply_solana_progress(&status, progress, publication_error);
            thread::sleep(poll);
        }
    });
}

fn ordered_archives(spool: &ArchiveSpool) -> Vec<Result<Archive, ()>> {
    let Ok(commitments) = spool.commitments() else {
        return vec![Err(())];
    };
    let mut archives = commitments
        .into_iter()
        .map(|commitment| {
            let stored = spool.get(commitment).map_err(|_| ())?;
            Archive::from_spool(stored.bytes, stored.node_head).map_err(|_| ())
        })
        .collect::<Vec<_>>();
    archives.sort_by_key(|value| {
        value
            .as_ref()
            .map_or(u64::MAX, |archive| archive.data().batch_number)
    });
    archives
}

fn recover_next_batch(spool: &ArchiveSpool, first: u64) -> Result<u64, SpoolRecoveryError> {
    recover_spool(
        first,
        || spool.commitments().map_err(|_| SpoolRecoveryError::Store),
        |commitment| {
            let stored = spool
                .get(commitment)
                .map_err(|_| SpoolRecoveryError::Store)?;
            let archive = Archive::from_spool(stored.bytes, stored.node_head)
                .map_err(|_| SpoolRecoveryError::Archive)?;
            Ok(archive.data().batch_number)
        },
    )
}

fn recover_spool(
    first: u64,
    inventory: impl FnOnce() -> Result<Vec<ArchiveCommitment>, SpoolRecoveryError>,
    load_batch: impl FnMut(ArchiveCommitment) -> Result<u64, SpoolRecoveryError>,
) -> Result<u64, SpoolRecoveryError> {
    recover_batch_sequence(first, inventory()?, load_batch)
}

fn recover_batch_sequence(
    first: u64,
    commitments: impl IntoIterator<Item = ArchiveCommitment>,
    mut load_batch: impl FnMut(ArchiveCommitment) -> Result<u64, SpoolRecoveryError>,
) -> Result<u64, SpoolRecoveryError> {
    let mut batches = BTreeMap::new();
    for commitment in commitments {
        let batch = load_batch(commitment)?;
        if batch < first {
            return Err(SpoolRecoveryError::BatchBeforeFirst);
        }
        if batches.insert(batch, commitment).is_some() {
            return Err(SpoolRecoveryError::DuplicateBatch);
        }
    }
    let mut expected = first;
    for batch in batches.keys().copied() {
        if batch != expected {
            return Err(SpoolRecoveryError::BatchGap);
        }
        expected = expected
            .checked_add(1)
            .ok_or(SpoolRecoveryError::SequenceExhausted)?;
    }
    Ok(expected)
}

fn ethereum_client(config: &RuntimeConfig) -> Result<EthereumArchiveClient, EthereumError> {
    let signer = RemoteChainSigner::new(signer_config(
        &config.ethereum.signer,
        SigningAlgorithm::Secp256k1Recoverable,
    )?)?;
    EthereumArchiveClient::open(ethereum_production_config(config)?, signer)
}

fn solana_client(
    config: &RuntimeConfig,
    solana: &SolanaFileConfig,
) -> Result<SolanaArchiveClient, SolanaError> {
    let signer = RemoteChainSigner::new(signer_config(&solana.signer, SigningAlgorithm::Ed25519)?)?;
    SolanaArchiveClient::open(solana_production_config(config, solana)?, signer)
}

fn ethereum_production_config(
    config: &RuntimeConfig,
) -> Result<EthereumProductionConfig, EthereumError> {
    Ok(EthereumProductionConfig {
        rpc: config.ethereum.rpc.clone(),
        chain_id: config.ethereum.chain_id,
        genesis_hash: fixed_hex(&config.ethereum.genesis_hash_hex)
            .map_err(|_| EthereumError::Configuration)?,
        archive_contract: fixed_hex(&config.ethereum.archive_contract_hex)
            .map_err(|_| EthereumError::Configuration)?,
        archive_code_hash: fixed_hex(&config.ethereum.archive_code_hash_hex)
            .map_err(|_| EthereumError::Configuration)?,
        first_batch_number: config.first_batch_number,
        required_confirmations: config.ethereum.required_confirmations,
        maximum_reorg_depth: config.ethereum.maximum_reorg_depth,
        chunk_bytes: config.ethereum.chunk_bytes,
        transaction_gas_limit: config.ethereum.transaction_gas_limit,
        maximum_fee_per_gas: config.ethereum.maximum_fee_per_gas,
        maximum_priority_fee_per_gas: config.ethereum.maximum_priority_fee_per_gas,
        journal_directory: config.state_directory.join("ethereum"),
    })
}

fn solana_production_config(
    config: &RuntimeConfig,
    solana: &SolanaFileConfig,
) -> Result<SolanaProductionConfig, SolanaError> {
    Ok(SolanaProductionConfig {
        rpc: solana.rpc.clone(),
        genesis_hash: fixed_base58(&solana.genesis_hash_base58)?,
        archive_program: fixed_base58(&solana.archive_program_base58)?,
        upgradeable_loader: fixed_base58(&solana.upgradeable_loader_base58)?,
        program_data_account: fixed_base58(&solana.program_data_account_base58)?,
        program_code_hash: fixed_hex(&solana.program_code_hash_hex)
            .map_err(|_| SolanaError::Configuration)?,
        first_batch_number: config.first_batch_number,
        required_rooted_slots: solana.required_rooted_slots,
        maximum_ancestry: solana.maximum_ancestry,
        chunk_bytes: solana.chunk_bytes,
        journal_directory: config.state_directory.join("solana"),
    })
}

fn signer_config(
    config: &SignerFileConfig,
    algorithm: SigningAlgorithm,
) -> Result<RemoteSignerConfig, SignerErrorShim> {
    let public_key = match algorithm {
        SigningAlgorithm::Secp256k1Recoverable => decode_hex(&config.public_key)?,
        SigningAlgorithm::Ed25519 => base58_decode(&config.public_key, 32)?,
    };
    let endpoint = match &config.transport {
        SignerTransportFileConfig::Uds { socket } => {
            if !socket.is_absolute() {
                return Err(SignerErrorShim);
            }
            SignerEndpoint::Uds {
                socket: socket.clone(),
            }
        }
        SignerTransportFileConfig::MutualTls {
            endpoint,
            server_name,
            trust_anchor,
            client_certificate,
            client_private_key,
        } => {
            if !trust_anchor.is_absolute()
                || !client_certificate.is_absolute()
                || !client_private_key.is_absolute()
            {
                return Err(SignerErrorShim);
            }
            SignerEndpoint::MutualTls {
                endpoint: *endpoint,
                server_name: server_name.clone(),
                trust_anchor: trust_anchor.clone(),
                client_certificate: client_certificate.clone(),
                client_private_key: client_private_key.clone(),
            }
        }
    };
    Ok(RemoteSignerConfig {
        endpoint,
        algorithm,
        key_handle: config.key_handle.clone(),
        public_key,
        timeout: Duration::from_millis(config.timeout_ms),
    })
}

#[derive(Clone, Copy, Debug)]
struct SignerErrorShim;

impl From<SignerErrorShim> for EthereumError {
    fn from(_: SignerErrorShim) -> Self {
        Self::Configuration
    }
}

impl From<SignerErrorShim> for SolanaError {
    fn from(_: SignerErrorShim) -> Self {
        Self::Configuration
    }
}

fn node_config(config: &NodeFileConfig) -> NodeSourceConfig {
    NodeSourceConfig {
        socket: config.socket.clone(),
        checkpoint_policy: config.checkpoint_policy.clone(),
        handshake: layerx_client::lni::handshake::HandshakeConfig {
            built_interface_version: layerx_client::lni::schema::Version::V1_0,
            expected_protocol_version: config.expected_protocol_version,
            expected_network_id: config.expected_network_id,
        },
        transport_limits: layerx_client::lni::transport::Limits {
            maximum_frame_bytes: config.maximum_frame_bytes,
            maximum_connections: config.maximum_connections,
            maximum_streams: config.maximum_streams,
            maximum_queued_bytes: config.maximum_queued_bytes,
            deadline: Duration::from_millis(config.deadline_ms),
        },
        retrieval_limits: layerx_client::availability::RetrievalLimits {
            maximum_bytes: config.maximum_archive_bytes,
            maximum_chunks: config.maximum_archive_chunks,
            deadline: Duration::from_millis(config.deadline_ms),
        },
    }
}

fn spawn_status(
    address: SocketAddr,
    status: Arc<Mutex<RuntimeStatus>>,
) -> Result<(), RuntimeError> {
    if !address.ip().is_loopback() {
        return Err(RuntimeError::Configuration);
    }
    let listener = TcpListener::bind(address).map_err(|_| RuntimeError::Status)?;
    listener
        .set_nonblocking(false)
        .map_err(|_| RuntimeError::Status)?;
    thread::spawn(move || {
        for stream in listener.incoming().flatten() {
            let _ = serve_status(stream, &status);
        }
    });
    Ok(())
}

fn serve_status(
    mut stream: TcpStream,
    status: &Arc<Mutex<RuntimeStatus>>,
) -> Result<(), RuntimeError> {
    stream
        .set_read_timeout(Some(Duration::from_secs(2)))
        .and_then(|()| stream.set_write_timeout(Some(Duration::from_secs(2))))
        .map_err(|_| RuntimeError::Status)?;
    let mut request = [0_u8; MAX_STATUS_REQUEST_BYTES];
    let read = stream
        .read(&mut request)
        .map_err(|_| RuntimeError::Status)?;
    let request = std::str::from_utf8(&request[..read]).map_err(|_| RuntimeError::Status)?;
    let first = request.lines().next().ok_or(RuntimeError::Status)?;
    let snapshot = status.lock().map_err(|_| RuntimeError::Status)?.clone();
    let ready = snapshot.node.ready
        && snapshot.ethereum.ready
        && snapshot.ethereum.checkpoint_within_budget
        && snapshot
            .solana
            .as_ref()
            .is_none_or(|solana| solana.ready && solana.checkpoint_within_budget)
        && snapshot.checkpoint_proof_boundary_ready;
    let (code, reason, body) = match first {
        "GET /status HTTP/1.1" | "GET /status HTTP/1.0" => (
            200,
            "OK",
            serde_json::to_vec(&snapshot).map_err(|_| RuntimeError::Status)?,
        ),
        "GET /readyz HTTP/1.1" | "GET /readyz HTTP/1.0" if ready => {
            (200, "OK", b"{\"ready\":true}".to_vec())
        }
        "GET /readyz HTTP/1.1" | "GET /readyz HTTP/1.0" => {
            (503, "Service Unavailable", b"{\"ready\":false}".to_vec())
        }
        _ => (404, "Not Found", b"{\"error\":\"not_found\"}".to_vec()),
    };
    write!(
        stream,
        "HTTP/1.1 {code} {reason}\r\nContent-Type: application/json\r\nCache-Control: no-store\r\nContent-Length: {}\r\nConnection: close\r\n\r\n",
        body.len()
    )
    .and_then(|()| stream.write_all(&body))
    .and_then(|()| stream.flush())
    .map_err(|_| RuntimeError::Status)
}

/// A verified certificate restores checkpoint readiness; a refusal latches it
/// off, and batches observed without checkpoint evidence never clear a refusal.
fn record_checkpoint(status: &mut RuntimeStatus, checkpoint: &CheckpointAcquisition) {
    match checkpoint {
        CheckpointAcquisition::NodeHasNoCheckpoint => {
            if !status.checkpoint_identifier_observed {
                status.checkpoint_proof_boundary_ready = true;
            }
        }
        CheckpointAcquisition::Verified(verified) => {
            let coordinate = verified.coordinate();
            status.checkpoint_identifier_observed = true;
            status.checkpoint_proof_boundary_ready = true;
            status.checkpoint_refusal = None;
            status.latest_verified_checkpoint_batch = Some(coordinate.batch_number);
            status.latest_verified_checkpoint_id = Some(hex_bytes(&coordinate.checkpoint_id));
        }
        CheckpointAcquisition::Refused { refusal, .. } => {
            status.checkpoint_identifier_observed = true;
            status.checkpoint_proof_boundary_ready = false;
            status.checkpoint_refusal = Some(refusal.code());
        }
    }
    let progress = VerifiedProgress::of(status);
    progress.apply(&mut status.ethereum);
    if let Some(solana) = status.solana.as_mut() {
        progress.apply(solana);
    }
}

/// Latest certificate-verified coordinates every chain is measured against.
#[derive(Clone, Copy)]
struct VerifiedProgress {
    batch: Option<u64>,
    checkpoint_batch: Option<u64>,
    budget: u64,
}

impl VerifiedProgress {
    fn of(status: &RuntimeStatus) -> Self {
        Self {
            batch: status.node.latest_batch_mirrored,
            checkpoint_batch: status.latest_verified_checkpoint_batch,
            budget: status.checkpoint_freshness_budget_batches,
        }
    }

    fn apply(self, status: &mut ComponentStatus) {
        status.latest_batch_verified = self.batch;
        status.latest_checkpoint_batch_verified = self.checkpoint_batch;
        status.freshness_budget_batches = self.budget;
        status.checkpoint_batch_lag = self.checkpoint_batch.map(|target| {
            target.saturating_sub(status.latest_checkpoint_batch_mirrored.unwrap_or(0))
        });
        status.checkpoint_within_budget = status
            .checkpoint_batch_lag
            .is_none_or(|lag| lag <= self.budget);
        status.freshness = Some(match status.checkpoint_batch_lag {
            None => "node_has_no_verified_checkpoint",
            Some(0) => "current",
            Some(lag) if lag <= self.budget => "within_budget",
            Some(_) => "beyond_budget",
        });
    }
}

fn update_ethereum(status: &Arc<Mutex<RuntimeStatus>>, progress: EthereumProgress) {
    update_status(status, |value| {
        if progress.phase == PublicationPhase::Reorged {
            value.ethereum.reorgs_observed = value.ethereum.reorgs_observed.saturating_add(1);
        }
        value.ethereum.ready = value.ethereum.ready
            && !matches!(
                progress.phase,
                PublicationPhase::PermanentRefusal | PublicationPhase::Reorged
            );
        VerifiedProgress::of(value).apply(&mut value.ethereum);
        value.ethereum.phase = Some(phase_name(progress.phase));
        value.ethereum.error_class = None;
    });
}

fn update_solana(status: &Arc<Mutex<RuntimeStatus>>, progress: SolanaProgress) {
    let Some(verified) = status.lock().ok().map(|value| VerifiedProgress::of(&value)) else {
        return;
    };
    update_solana_status(status, |value| {
        if progress.phase == PublicationPhase::Reorged {
            value.reorgs_observed = value.reorgs_observed.saturating_add(1);
        }
        value.ready = value.ready
            && !matches!(
                progress.phase,
                PublicationPhase::PermanentRefusal | PublicationPhase::Reorged
            );
        verified.apply(value);
        value.phase = Some(phase_name(progress.phase));
        value.error_class = None;
    });
}

/// Reports only gate-acknowledged mirrored coordinates. A reconcile refusal
/// holds the lane unready under its typed code.
fn apply_ethereum_progress(
    status: &Arc<Mutex<RuntimeStatus>>,
    progress: ChainProgress,
    publication_error: Option<&'static str>,
) {
    update_status(status, |value| {
        let verified = VerifiedProgress::of(value);
        apply_progress(&mut value.ethereum, verified, progress, publication_error);
    });
}

fn apply_solana_progress(
    status: &Arc<Mutex<RuntimeStatus>>,
    progress: ChainProgress,
    publication_error: Option<&'static str>,
) {
    update_status(status, |value| {
        let verified = VerifiedProgress::of(value);
        if let Some(solana) = value.solana.as_mut() {
            apply_progress(solana, verified, progress, publication_error);
        }
    });
}

fn apply_progress(
    status: &mut ComponentStatus,
    verified: VerifiedProgress,
    progress: ChainProgress,
    publication_error: Option<&'static str>,
) {
    status.latest_batch_mirrored = progress.mirrored.latest_batch;
    set_checkpoint_status(status, progress.mirrored.latest_checkpoint);
    verified.apply(status);
    if let Some(error_class) = progress
        .refusal
        .map(|refusal| refusal.code())
        .or(publication_error)
    {
        status.ready = false;
        status.error_class = Some(error_class);
    } else {
        status.ready = progress.mirrored.latest_checkpoint.is_some()
            && !matches!(status.phase, Some("permanent_refusal" | "reorged"));
        status.error_class = None;
    }
}

/// Updates the Solana component of a runtime that has one. The Solana worker is
/// spawned only for a configured section, so the slot it reports into is always
/// present while that worker runs.
fn update_solana_status(
    status: &Arc<Mutex<RuntimeStatus>>,
    update: impl FnOnce(&mut ComponentStatus),
) {
    update_status(status, |value| {
        if let Some(solana) = value.solana.as_mut() {
            update(solana);
        }
    });
}

fn update_status(status: &Arc<Mutex<RuntimeStatus>>, update: impl FnOnce(&mut RuntimeStatus)) {
    if let Ok(mut status) = status.lock() {
        update(&mut status);
    }
}

fn set_checkpoint_status(
    status: &mut ComponentStatus,
    checkpoint: Option<crate::CheckpointCoordinate>,
) {
    status.latest_checkpoint_batch_mirrored = checkpoint.map(|value| value.batch_number);
    status.latest_checkpoint_id_mirrored = checkpoint.map(|value| hex_bytes(&value.checkpoint_id));
}

fn hex_bytes(bytes: &[u8]) -> String {
    const DIGITS: &[u8; 16] = b"0123456789abcdef";
    let mut output = String::with_capacity(bytes.len().saturating_mul(2));
    for byte in bytes {
        output.push(char::from(DIGITS[usize::from(byte >> 4)]));
        output.push(char::from(DIGITS[usize::from(byte & 0x0f)]));
    }
    output
}

const fn phase_name(value: PublicationPhase) -> &'static str {
    match value {
        PublicationPhase::Prepared => "prepared",
        PublicationPhase::PreBroadcastFailure => "pre_broadcast_failure",
        PublicationPhase::Signed => "signed",
        PublicationPhase::BroadcastUnknown => "broadcast_unknown",
        PublicationPhase::Pending => "pending",
        PublicationPhase::Finalized => "finalized",
        PublicationPhase::RetrievedVerified => "retrieved_verified",
        PublicationPhase::PermanentRefusal => "permanent_refusal",
        PublicationPhase::Reorged => "reorged",
        PublicationPhase::BroadcastExpired => "broadcast_expired",
    }
}

fn node_error_class(error: &crate::node::NodeSourceError) -> &'static str {
    match error {
        crate::node::NodeSourceError::Transport(_) => "transport",
        crate::node::NodeSourceError::Handshake(_) => "handshake",
        crate::node::NodeSourceError::Capability(_) => "capability",
        crate::node::NodeSourceError::Availability(_)
        | crate::node::NodeSourceError::AvailabilityPartial => "availability",
        _ => "evidence",
    }
}

fn ethereum_error_class(error: &EthereumError) -> &'static str {
    match error {
        EthereumError::Rpc(_) => "rpc",
        EthereumError::Signer(_) => "signer",
        EthereumError::Store(_) => "store",
        EthereumError::ChainIdentity | EthereumError::ContractIdentity => "target_identity",
        EthereumError::Retrieval => "retrieval",
        EthereumError::Reorg => "reorg",
        _ => "publication",
    }
}

fn solana_error_class(error: &SolanaError) -> &'static str {
    match error {
        SolanaError::Rpc(_) => "rpc",
        SolanaError::Signer(_) => "signer",
        SolanaError::Store(_) => "store",
        SolanaError::ClusterIdentity
        | SolanaError::ProgramIdentity
        | SolanaError::ProgramMutable => "target_identity",
        SolanaError::Retrieval => "retrieval",
        SolanaError::Reorg => "reorg",
        _ => "publication",
    }
}

fn validate_runtime(config: &RuntimeConfig) -> Result<(), RuntimeError> {
    let checkpoint_policy = &config.node.checkpoint_policy;
    if checkpoint_policy.chain_id == 0
        || checkpoint_policy.confirmations == 0
        || fixed_hex::<32>(&checkpoint_policy.genesis_hash_hex)
            .map_err(|_| RuntimeError::Configuration)?
            == [0; 32]
        || fixed_hex::<32>(&checkpoint_policy.sequencer_public_key_hex)
            .map_err(|_| RuntimeError::Configuration)?
            == [0; 32]
    {
        return Err(RuntimeError::Configuration);
    }
    RpcCluster::new(&checkpoint_policy.rpc).map_err(|_| RuntimeError::Configuration)?;
    if !config.state_directory.is_absolute()
        || !config.node.socket.is_absolute()
        || config.first_batch_number == 0
        || !(100..=60_000).contains(&config.poll_interval_ms)
        || !(1..=1_000_000).contains(&config.checkpoint_freshness_budget_batches)
        || !matches!(config.status_listen.ip(), IpAddr::V4(_) | IpAddr::V6(_))
        || !config.status_listen.ip().is_loopback()
        || config.status_listen.port() == 0
        || !layerx_wire::limits::protocol_version_uses_occupancy(
            config.node.expected_protocol_version,
        )
        || !(100..=120_000).contains(&config.node.deadline_ms)
        || !(1024..=64 * 1024 * 1024).contains(&config.node.maximum_frame_bytes)
        || !(1..=64).contains(&config.node.maximum_connections)
        || !(1..=64).contains(&config.node.maximum_streams)
        || !(1024..=128 * 1024 * 1024).contains(&config.node.maximum_queued_bytes)
        || !(1024..=64 * 1024 * 1024).contains(&config.node.maximum_archive_bytes)
        || !(1..=65_536).contains(&config.node.maximum_archive_chunks)
    {
        return Err(RuntimeError::Configuration);
    }
    let ethereum = ethereum_production_config(config).map_err(|_| RuntimeError::Configuration)?;
    crate::ethereum::validate_config(&ethereum).map_err(|_| RuntimeError::Configuration)?;
    RpcCluster::new(&ethereum.rpc).map_err(|_| RuntimeError::Configuration)?;
    let ethereum_signer = signer_config(
        &config.ethereum.signer,
        SigningAlgorithm::Secp256k1Recoverable,
    )
    .map_err(|_| RuntimeError::Configuration)?;
    RemoteChainSigner::new(ethereum_signer).map_err(|_| RuntimeError::Configuration)?;

    if let Some(section) = &config.solana {
        let solana =
            solana_production_config(config, section).map_err(|_| RuntimeError::Configuration)?;
        crate::solana::validate_config(&solana).map_err(|_| RuntimeError::Configuration)?;
        RpcCluster::new(&solana.rpc).map_err(|_| RuntimeError::Configuration)?;
        let solana_signer = signer_config(&section.signer, SigningAlgorithm::Ed25519)
            .map_err(|_| RuntimeError::Configuration)?;
        RemoteChainSigner::new(solana_signer).map_err(|_| RuntimeError::Configuration)?;
    }
    Ok(())
}

fn fixed_hex<const N: usize>(value: &str) -> Result<[u8; N], SignerErrorShim> {
    decode_hex(value)?.try_into().map_err(|_| SignerErrorShim)
}

fn decode_hex(value: &str) -> Result<Vec<u8>, SignerErrorShim> {
    let digits = value.strip_prefix("0x").unwrap_or(value);
    if digits.is_empty() || !digits.len().is_multiple_of(2) {
        return Err(SignerErrorShim);
    }
    digits
        .as_bytes()
        .chunks_exact(2)
        .map(|pair| {
            let high = hex_digit(pair[0]).ok_or(SignerErrorShim)?;
            let low = hex_digit(pair[1]).ok_or(SignerErrorShim)?;
            Ok(high << 4 | low)
        })
        .collect()
}

fn hex_digit(value: u8) -> Option<u8> {
    match value {
        b'0'..=b'9' => Some(value - b'0'),
        b'a'..=b'f' => Some(value - b'a' + 10),
        b'A'..=b'F' => Some(value - b'A' + 10),
        _ => None,
    }
}

fn fixed_base58(value: &str) -> Result<[u8; 32], SolanaError> {
    base58_decode(value, 32)?
        .try_into()
        .map_err(|_| SolanaError::Configuration)
}

fn base58_decode(value: &str, maximum: usize) -> Result<Vec<u8>, SignerErrorShim> {
    if value.is_empty() || value.len() > maximum.saturating_mul(2) {
        return Err(SignerErrorShim);
    }
    let alphabet = b"123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz";
    let mut bytes = vec![0_u8];
    for character in value.bytes() {
        let digit = alphabet
            .iter()
            .position(|candidate| *candidate == character)
            .ok_or(SignerErrorShim)?;
        let mut carry = u32::try_from(digit).map_err(|_| SignerErrorShim)?;
        for byte in bytes.iter_mut().rev() {
            let next = u32::from(*byte).saturating_mul(58).saturating_add(carry);
            *byte = u8::try_from(next & 0xff).map_err(|_| SignerErrorShim)?;
            carry = next >> 8;
        }
        while carry > 0 {
            bytes.insert(0, u8::try_from(carry & 0xff).map_err(|_| SignerErrorShim)?);
            carry >>= 8;
        }
        if bytes.len() > maximum {
            return Err(SignerErrorShim);
        }
    }
    let leading = value.bytes().take_while(|byte| *byte == b'1').count();
    let first_nonzero = bytes
        .iter()
        .position(|byte| *byte != 0)
        .unwrap_or(bytes.len());
    let mut decoded = vec![0; leading];
    decoded.extend_from_slice(&bytes[first_nonzero..]);
    if decoded.len() > maximum {
        return Err(SignerErrorShim);
    }
    Ok(decoded)
}

#[cfg(test)]
mod tests {
    use std::path::PathBuf;
    use std::sync::atomic::{AtomicU64, Ordering};
    use std::sync::{Arc, Mutex};

    use super::{
        apply_ethereum_progress, apply_solana_progress, hex_bytes, record_checkpoint,
        recover_batch_sequence, recover_next_batch, recover_spool, ComponentStatus, RuntimeConfig,
        RuntimeStatus, SpoolRecoveryError,
    };
    use crate::node::{CheckpointAcquisition, CheckpointRefusal};
    use crate::store::{ArchiveSpool, StoreError};
    use crate::{archive_commitment, ArchiveCommitment, ChainProgress, NodeHead};
    use layerx_client::evidence::EvidenceError;
    use layerx_proof::checkpoint::CheckpointError;

    static NEXT_DIRECTORY: AtomicU64 = AtomicU64::new(0);

    struct ScratchDirectory(PathBuf);

    impl ScratchDirectory {
        fn new(label: &str) -> Self {
            let sequence = NEXT_DIRECTORY.fetch_add(1, Ordering::Relaxed);
            let path = std::env::temp_dir().join(format!(
                "layerx-mirror-runtime-{label}-{}-{sequence}",
                std::process::id()
            ));
            std::fs::create_dir(&path)
                .unwrap_or_else(|error| panic!("create scratch directory: {error}"));
            Self(path)
        }

        fn path(&self) -> &std::path::Path {
            &self.0
        }
    }

    impl Drop for ScratchDirectory {
        fn drop(&mut self) {
            if self.0.is_dir() {
                let _ = std::fs::remove_dir_all(&self.0);
            } else {
                let _ = std::fs::remove_file(&self.0);
            }
        }
    }

    fn commitment(byte: u8) -> ArchiveCommitment {
        ArchiveCommitment::from_bytes([byte; 32])
    }

    const ETHEREUM_SECTION: &str = r#"
        "ethereum": {
          "rpc": {
            "endpoints": [
              {"url":"https://ethereum-a.invalid/rpc","ca_certificate_der":"/run/secrets/a.der","bearer_token_file":"/run/secrets/a.token","independent_backend":"ethereum-a"},
              {"url":"https://ethereum-b.invalid/rpc","ca_certificate_der":"/run/secrets/b.der","bearer_token_file":"/run/secrets/b.token","independent_backend":"ethereum-b"}
            ],
            "quorum": 2,
            "connect_timeout_ms": 3000,
            "request_timeout_ms": 15000,
            "maximum_response_bytes": 16777216
          },
          "chain_id": 125,
          "genesis_hash_hex": "0000000000000000000000000000000000000000000000000000000000000001",
          "archive_contract_hex": "0000000000000000000000000000000000000001",
          "archive_code_hash_hex": "0000000000000000000000000000000000000000000000000000000000000001",
          "required_confirmations": 12,
          "maximum_reorg_depth": 256,
          "chunk_bytes": 24576,
          "transaction_gas_limit": 30000000,
          "maximum_fee_per_gas": 100000000000,
          "maximum_priority_fee_per_gas": 5000000000,
          "signer": {
            "key_handle": "mirror/ethereum/beta",
            "public_key": "02aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
            "timeout_ms": 5000,
            "transport": {"kind":"uds","socket":"/run/mirror-signer/signer.sock"}
          }
        }"#;

    const SOLANA_SECTION: &str = r#"
        "solana": {
          "rpc": {
            "endpoints": [
              {"url":"https://solana-a.invalid/rpc","ca_certificate_der":"/run/secrets/a.der","bearer_token_file":"/run/secrets/a.token","independent_backend":"solana-a"},
              {"url":"https://solana-b.invalid/rpc","ca_certificate_der":"/run/secrets/b.der","bearer_token_file":"/run/secrets/b.token","independent_backend":"solana-b"}
            ],
            "quorum": 2,
            "connect_timeout_ms": 3000,
            "request_timeout_ms": 15000,
            "maximum_response_bytes": 16777216
          },
          "genesis_hash_base58": "11111111111111111111111111111112",
          "archive_program_base58": "11111111111111111111111111111113",
          "upgradeable_loader_base58": "BPFLoaderUpgradeab1e11111111111111111111111",
          "program_data_account_base58": "11111111111111111111111111111114",
          "program_code_hash_hex": "0000000000000000000000000000000000000000000000000000000000000001",
          "required_rooted_slots": 32,
          "maximum_ancestry": 4096,
          "chunk_bytes": 640,
          "signer": {
            "key_handle": "mirror/solana/beta",
            "public_key": "11111111111111111111111111111115",
            "timeout_ms": 5000,
            "transport": {"kind":"uds","socket":"/run/mirror-signer/signer.sock"}
          }
        }"#;

    /// The rendered publisher configuration, with the chain sections the caller
    /// names. This is the shape `interop/deploy/mirror/render-config.py` writes.
    fn rendered_configuration(sections: &[&str]) -> String {
        format!(
            r#"{{
              "state_directory": "/var/lib/layerx-mirror",
              "first_batch_number": 1,
              "poll_interval_ms": 5000,
              "checkpoint_freshness_budget_batches": 64,
              "status_listen": "127.0.0.1:9091",
              "node": {{
                "socket": "/run/layerx/node/layerxd.lni.sock",
                "expected_protocol_version": 3,
                "expected_network_id": 1,
                "maximum_frame_bytes": 67108864,
                "maximum_connections": 2,
                "maximum_streams": 2,
                "maximum_queued_bytes": 67108864,
                "deadline_ms": 30000,
                "maximum_archive_bytes": 67108864,
                "maximum_archive_chunks": 65536,
                "checkpoint_policy": {}
              }},
              {}
            }}"#,
            serde_json::to_string(
                &serde_json::from_str::<serde_json::Value>(include_str!(
                    "../../../deploy/mirror/config.example.json"
                ))
                .unwrap_or_else(|error| panic!("portable config: {error:?}"))["node"]
                    ["checkpoint_policy"]
            )
            .unwrap_or_else(|error| panic!("portable policy: {error:?}")),
            sections.join(",\n")
        )
    }

    #[test]
    fn a_configuration_without_a_solana_section_mirrors_to_the_evm_chain_only() {
        let both: RuntimeConfig =
            serde_json::from_str(&rendered_configuration(&[ETHEREUM_SECTION, SOLANA_SECTION]))
                .unwrap_or_else(|error| panic!("parse the dual-target configuration: {error}"));
        assert!(both.solana.is_some());

        let ethereum_only: RuntimeConfig =
            serde_json::from_str(&rendered_configuration(&[ETHEREUM_SECTION]))
                .unwrap_or_else(|error| panic!("parse the Ethereum-only configuration: {error}"));

        assert!(
            ethereum_only.solana.is_none(),
            "a configuration with no solana section mirrors to the EVM chain only"
        );
        assert_eq!(ethereum_only.ethereum.chain_id, both.ethereum.chain_id);
    }

    #[test]
    fn a_present_solana_section_is_still_complete_or_refused() {
        let incomplete = SOLANA_SECTION.replace(
            r#""program_data_account_base58": "11111111111111111111111111111114","#,
            "",
        );
        assert_ne!(incomplete, SOLANA_SECTION);

        assert!(
            serde_json::from_str::<RuntimeConfig>(&rendered_configuration(&[
                ETHEREUM_SECTION,
                &incomplete
            ]))
            .is_err(),
            "a solana section that is present must still carry every field"
        );
    }

    #[test]
    fn startup_refuses_unreadable_commitment_inventory() {
        let result = recover_spool(
            1,
            || Err(SpoolRecoveryError::Store),
            |_| unreachable!("failed inventory cannot load an object"),
        );

        assert!(matches!(result, Err(SpoolRecoveryError::Store)));
    }

    #[test]
    fn startup_refuses_a_second_spool_owner() {
        let scratch = ScratchDirectory::new("exclusive-owner");
        let directory = scratch.path().join("archives");
        let owner = ArchiveSpool::open(&directory)
            .unwrap_or_else(|error| panic!("open archive spool owner: {error:?}"));

        assert!(matches!(
            ArchiveSpool::open(&directory),
            Err(StoreError::Conflict)
        ));

        drop(owner);
        ArchiveSpool::open(&directory)
            .unwrap_or_else(|error| panic!("reopen released archive spool: {error:?}"));
    }

    #[test]
    fn startup_refuses_object_missing_after_inventory() {
        let scratch = ScratchDirectory::new("missing-object");
        let directory = scratch.path().join("archives");
        let spool = ArchiveSpool::open(&directory)
            .unwrap_or_else(|error| panic!("open archive spool: {error:?}"));
        let bytes = b"inventory object removed before recovery";
        let missing = archive_commitment(bytes);
        spool
            .put(
                missing,
                bytes,
                NodeHead {
                    latest_sealed_batch: 1,
                    latest_finalised_checkpoint: None,
                },
            )
            .unwrap_or_else(|error| panic!("spool missing archive fixture: {error:?}"));
        let commitments = spool
            .commitments()
            .unwrap_or_else(|error| panic!("list archive fixture: {error:?}"));
        let name = hex_bytes(missing.as_bytes());
        std::fs::remove_file(directory.join(format!("{name}.archive")))
            .unwrap_or_else(|error| panic!("remove inventoried archive: {error}"));
        let result = recover_batch_sequence(1, commitments, |value| {
            spool
                .get(value)
                .map(|_| 1)
                .map_err(|_| SpoolRecoveryError::Store)
        });

        assert!(matches!(result, Err(SpoolRecoveryError::Store)));
    }

    #[test]
    fn startup_refuses_corrupt_archive_bytes() {
        let scratch = ScratchDirectory::new("corrupt-archive");
        let spool = ArchiveSpool::open(scratch.path().join("archives"))
            .unwrap_or_else(|error| panic!("open archive spool: {error:?}"));
        let bytes = b"not a canonical LayerX mirror archive";
        spool
            .put(
                archive_commitment(bytes),
                bytes,
                NodeHead {
                    latest_sealed_batch: 1,
                    latest_finalised_checkpoint: None,
                },
            )
            .unwrap_or_else(|error| panic!("spool corrupt archive fixture: {error:?}"));

        assert!(matches!(
            recover_next_batch(&spool, 1),
            Err(SpoolRecoveryError::Archive)
        ));
    }

    #[test]
    fn startup_refuses_duplicate_batch_metadata() {
        let result = recover_batch_sequence(7, [commitment(1), commitment(2)], |_| Ok(7));

        assert!(matches!(result, Err(SpoolRecoveryError::DuplicateBatch)));
    }

    #[test]
    fn startup_refuses_gapped_batch_metadata() {
        let first = commitment(1);
        let third = commitment(3);
        let result = recover_batch_sequence(10, [first, third], |value| {
            Ok(if value == first { 10 } else { 12 })
        });

        assert!(matches!(result, Err(SpoolRecoveryError::BatchGap)));
    }

    #[test]
    fn clean_restart_resumes_after_the_contiguous_spool_prefix() {
        let first = commitment(1);
        let second = commitment(2);
        let result = recover_batch_sequence(41, [second, first], |value| {
            Ok(if value == first { 41 } else { 42 })
        });

        assert_eq!(
            result.unwrap_or_else(|error| panic!("recover clean spool: {error:?}")),
            43
        );
        assert_eq!(
            recover_batch_sequence(41, [], |_| unreachable!(
                "empty inventory has no loader calls"
            ))
            .unwrap_or_else(|error| panic!("recover empty spool: {error:?}")),
            41
        );
    }

    #[test]
    fn worker_degradation_survives_progress_application_per_lane() {
        let status = Arc::new(Mutex::new(RuntimeStatus {
            solana: Some(ComponentStatus::default()),
            ..RuntimeStatus::default()
        }));
        apply_ethereum_progress(&status, ChainProgress::default(), Some("rpc"));
        apply_solana_progress(&status, ChainProgress::default(), Some("archive_spool"));
        {
            let value = status
                .lock()
                .unwrap_or_else(|error| panic!("runtime status: {error}"));
            assert!(!value.ethereum.ready);
            assert_eq!(value.ethereum.error_class, Some("rpc"));
            assert_eq!(value.ethereum.latest_batch_mirrored, None);
            let solana = value
                .solana
                .as_ref()
                .unwrap_or_else(|| panic!("configured Solana lane"));
            assert!(!solana.ready);
            assert_eq!(solana.error_class, Some("archive_spool"));
            assert_eq!(solana.latest_checkpoint_batch_mirrored, None);
        }

        apply_ethereum_progress(&status, ChainProgress::default(), None);
        let value = status
            .lock()
            .unwrap_or_else(|error| panic!("runtime status: {error}"));
        assert!(!value.ethereum.ready);
        assert_eq!(value.ethereum.error_class, None);
        let solana = value
            .solana
            .as_ref()
            .unwrap_or_else(|| panic!("configured Solana lane"));
        assert!(!solana.ready);
        assert_eq!(solana.error_class, Some("archive_spool"));
    }

    #[test]
    fn checkpoint_refusal_latches_until_verified_evidence() {
        let mut status = RuntimeStatus {
            solana: Some(ComponentStatus::default()),
            checkpoint_freshness_budget_batches: 4,
            ..RuntimeStatus::default()
        };
        record_checkpoint(&mut status, &CheckpointAcquisition::NodeHasNoCheckpoint);
        assert!(status.checkpoint_proof_boundary_ready);
        assert!(status.ethereum.checkpoint_within_budget);
        assert_eq!(
            status.ethereum.freshness,
            Some("node_has_no_verified_checkpoint")
        );
        assert_eq!(status.ethereum.freshness_budget_batches, 4);

        for error in [
            EvidenceError::Checkpoint(CheckpointError::SignerMembership([7; 32])),
            EvidenceError::Checkpoint(CheckpointError::Threshold {
                achieved: 1,
                required: 2,
            }),
            EvidenceError::SelectorMismatch,
            EvidenceError::NetworkMismatch,
            EvidenceError::Unavailable,
        ] {
            let refusal = CheckpointRefusal::from_evidence(&error);
            record_checkpoint(
                &mut status,
                &CheckpointAcquisition::Refused {
                    checkpoint_id: [9; 32],
                    refusal,
                },
            );
            assert!(!status.checkpoint_proof_boundary_ready);
            assert_eq!(status.checkpoint_refusal, Some(refusal.code()));
            assert_eq!(status.latest_verified_checkpoint_batch, None);
            record_checkpoint(&mut status, &CheckpointAcquisition::NodeHasNoCheckpoint);
            assert!(!status.checkpoint_proof_boundary_ready);
            assert_eq!(status.checkpoint_refusal, Some(refusal.code()));
        }
        assert_eq!(
            CheckpointRefusal::from_evidence(&EvidenceError::Checkpoint(
                CheckpointError::SignerMembership([7; 32])
            )),
            CheckpointRefusal::SignerMembership
        );
        assert_eq!(
            CheckpointRefusal::from_evidence(&EvidenceError::Checkpoint(
                CheckpointError::Threshold {
                    achieved: 1,
                    required: 2
                }
            )),
            CheckpointRefusal::Threshold
        );
        assert_eq!(
            CheckpointRefusal::from_evidence(&EvidenceError::NetworkMismatch),
            CheckpointRefusal::DomainMismatch
        );
    }
}
