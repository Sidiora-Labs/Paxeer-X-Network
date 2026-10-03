use std::path::PathBuf;
use std::time::Duration;

use layerx_client::availability::RetrievalLimits;
use layerx_client::client::{ClientConfig, ReconnectPolicy};
use layerx_client::evidence::{
    CheckpointSelector, FinalityEvidenceCandidate, ProofBundleSelector, VerifiedCheckpoint,
    VerifiedProofBundle,
};
use layerx_client::handover::{decode_finality_policy, SequencerHistory};
use layerx_client::lni::handshake::HandshakeConfig;
use layerx_client::lni::schema::Version;
use layerx_client::lni::transport::Limits;
use layerx_client::Client;
use layerx_paxeer_verifier::{EndpointFault, PaxeerCheckpointVerifier};
use layerx_types::payload::ModuleRegistry;
use serde_json::{json, Value};
use sha2::{Digest, Sha256};

use crate::codec::{hex, unhex_fixed};
use crate::store::Store;
use crate::IndexError;

const MAX_TRUST_BYTES: usize = 1_048_576;
const MAX_HISTORY_BATCHES: usize = 64;
const DEADLINE: Duration = Duration::from_secs(10);

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum SettlementFailure {
    Unavailable,
    Invalid,
}

impl SettlementFailure {
    pub fn document(self) -> Value {
        json!({"level": "unverified", "source": "native_lni_and_independent_paxeer", "reason": match self {
            Self::Unavailable => "settlement_evidence_unavailable",
            Self::Invalid => "invalid_settlement_evidence",
        }})
    }
}

#[derive(Clone, Debug)]
pub struct VerifiedSettlement {
    pub(crate) receipt: Vec<u8>,
    pub(crate) activity_id: [u8; 32],
    pub(crate) sequence: u64,
    pub(crate) batch: u64,
    pub(crate) batch_id: [u8; 32],
    pub(crate) document: Value,
}

pub struct SettlementSource {
    client: Client,
    history: SequencerHistory,
    registry: ModuleRegistry,
    verifier: PaxeerCheckpointVerifier,
    correlation: u64,
    trust_digest: String,
    reconnect_required: bool,
}

fn bounded_file(path: &str) -> Result<Vec<u8>, IndexError> {
    use std::io::Read;
    let file = std::fs::File::open(path)
        .map_err(|_| IndexError::Config("settlement trust artifact unavailable".into()))?;
    let mut bytes = Vec::new();
    file.take((MAX_TRUST_BYTES + 1) as u64)
        .read_to_end(&mut bytes)
        .map_err(|_| IndexError::Config("settlement trust artifact unreadable".into()))?;
    if bytes.is_empty() || bytes.len() > MAX_TRUST_BYTES {
        return Err(IndexError::Config(
            "settlement trust artifact exceeds bounds".into(),
        ));
    }
    Ok(bytes)
}

pub fn validate_configuration<F: Fn(&str) -> Option<String>>(lookup: &F) -> Result<(), IndexError> {
    configuration(lookup).map(|_| ())
}

fn configuration<F: Fn(&str) -> Option<String>>(
    lookup: &F,
) -> Result<
    Option<(
        ClientConfig,
        SequencerHistory,
        ModuleRegistry,
        PaxeerCheckpointVerifier,
        String,
    )>,
    IndexError,
> {
    let names = [
        "LAYERX_INDEXER_SETTLEMENT_POLICY",
        "LAYERX_INDEXER_GENESIS_TRUST",
        "LAYERX_INDEXER_INITIAL_SEQUENCER_KEY",
        "LAYERX_INDEXER_LNI_SOCKET",
    ];
    let values: Vec<_> = names.iter().map(|name| lookup(name)).collect();
    if values.iter().all(Option::is_none) {
        return Ok(None);
    }
    if values.iter().any(Option::is_none) {
        return Err(IndexError::Config("settlement policy, genesis trust, initial sequencer key and LNI socket are required together".into()));
    }
    let get = |i: usize| {
        values[i]
            .as_deref()
            .ok_or_else(|| IndexError::Config("missing settlement configuration".into()))
    };
    let policy_bytes = bounded_file(get(0)?)?;
    let policy = decode_finality_policy(&policy_bytes)
        .map_err(|_| IndexError::Config("invalid canonical settlement policy".into()))?;
    if policy.protocol_version != 3 {
        return Err(IndexError::Config(
            "historical settlement requires protocol 3".into(),
        ));
    }
    let artifact = bounded_file(get(1)?)?;
    let initial_key = unhex_fixed::<32>(get(2)?)?;
    let history = SequencerHistory::from_genesis_artifact(
        &artifact,
        policy.network_id,
        policy.canonical_genesis_root,
        initial_key,
    )
    .map_err(|_| {
        IndexError::Config(
            "genesis trust does not match independently pinned settlement policy".into(),
        )
    })?;
    let registry = layerx_wire::handover::decode_genesis_trust(&artifact)
        .map_err(|_| IndexError::Config("invalid canonical genesis trust".into()))?
        .registry;
    let verifier = PaxeerCheckpointVerifier::new(policy.clone())
        .map_err(|_| IndexError::Config("invalid independent Paxeer policy".into()))?;
    let config = ClientConfig {
        endpoint: PathBuf::from(get(3)?),
        handshake: HandshakeConfig {
            built_interface_version: Version { major: 1, minor: 9 },
            expected_protocol_version: policy.protocol_version,
            expected_network_id: policy.network_id,
        },
        limits: Limits {
            maximum_frame_bytes: 8 * 1024 * 1024,
            maximum_connections: 1,
            maximum_streams: 1,
            maximum_queued_bytes: 8 * 1024 * 1024,
            deadline: DEADLINE,
        },
        reconnect: ReconnectPolicy {
            maximum_attempts: 1,
            base_delay: Duration::from_millis(100),
            maximum_delay: Duration::from_millis(100),
            jitter_percent: 0,
        },
    };
    if config.endpoint.as_os_str().is_empty() {
        return Err(IndexError::Config("LNI socket is empty".into()));
    }
    let mut digest = Sha256::new();
    digest.update(b"LayerX/indexer/settlement-trust/v1\0");
    digest.update(&policy_bytes);
    digest.update(&artifact);
    digest.update(initial_key);
    Ok(Some((
        config,
        history,
        registry,
        verifier,
        hex(&digest.finalize()),
    )))
}

impl SettlementSource {
    pub fn from_environment() -> Result<Option<Self>, IndexError> {
        let Some((config, history, registry, verifier, trust_digest)) =
            configuration(&|name| std::env::var(name).ok())?
        else {
            return Ok(None);
        };
        let client = Client::connect(config)
            .map_err(|_| IndexError::Source("settlement LNI unavailable".into()))?;
        Ok(Some(Self {
            client,
            history,
            registry,
            verifier,
            correlation: 1,
            trust_digest,
            reconnect_required: false,
        }))
    }

    fn next_correlation(&mut self) -> Result<u64, SettlementFailure> {
        let value = self.correlation;
        self.correlation = value.checked_add(4).ok_or(SettlementFailure::Invalid)?;
        Ok(value)
    }

    pub fn prepare_through(&mut self, batch: u64) -> Result<(), SettlementFailure> {
        for _ in 0..MAX_HISTORY_BATCHES {
            if self
                .history
                .verified_head()
                .is_some_and(|head| head.header().batch_number() >= batch)
            {
                return Ok(());
            }
            let correlation = self.next_correlation()?;
            self.client
                .advance_sequencer_history_with_finality(
                    &mut self.history,
                    correlation,
                    RetrievalLimits {
                        maximum_bytes: 64 * 1024 * 1024,
                        maximum_chunks: 4096,
                        deadline: DEADLINE,
                    },
                    Some(&self.verifier),
                )
                .map_err(|error| match error {
                    layerx_client::handover::HistoryError::Transport
                    | layerx_client::handover::HistoryError::Availability => {
                        SettlementFailure::Unavailable
                    }
                    _ => SettlementFailure::Invalid,
                })?;
        }
        if self
            .history
            .verified_head()
            .is_some_and(|head| head.header().batch_number() >= batch)
        {
            Ok(())
        } else {
            Err(SettlementFailure::Unavailable)
        }
    }

    pub fn verify(
        &mut self,
        receipt_bytes: &[u8],
        batch: u64,
    ) -> Result<VerifiedSettlement, SettlementFailure> {
        let decoded =
            layerx_wire::receipt::decode(receipt_bytes).map_err(|_| SettlementFailure::Invalid)?;
        let receipt = decoded.protocol().ok_or(SettlementFailure::Invalid)?;
        let correlation = self.next_correlation()?;
        let bundle = self
            .client
            .proof_bundle_with_history(
                ProofBundleSelector::Receipt(receipt.activity_id()),
                correlation,
                &self.registry,
                &self.history,
            )
            .map_err(|error| {
                if matches!(
                    error,
                    layerx_client::evidence::EvidenceError::Unavailable
                        | layerx_client::evidence::EvidenceError::Transport(_)
                ) {
                    SettlementFailure::Unavailable
                } else {
                    SettlementFailure::Invalid
                }
            })?;
        let VerifiedProofBundle::Receipt {
            canonical_bytes,
            signed_header,
            ..
        } = bundle
        else {
            return Err(SettlementFailure::Invalid);
        };
        let verified_header = self
            .history
            .verify_header(&signed_header.canonical_bytes, &signed_header.signature)
            .map_err(|_| SettlementFailure::Invalid)?;
        let header = verified_header.header();
        if canonical_bytes != receipt_bytes
            || header.batch_number() != batch
            || receipt.protocol_version() != header.protocol_version()
            || !(header.first_sequence()..=header.last_sequence())
                .contains(&receipt.global_sequence())
        {
            return Err(SettlementFailure::Invalid);
        }
        let correlation = self.next_correlation()?;
        let checkpoint = self
            .client
            .checkpoint_evidence(CheckpointSelector::Batch(batch), correlation)
            .map_err(|error| {
                if matches!(
                    error,
                    layerx_client::evidence::EvidenceError::Unavailable
                        | layerx_client::evidence::EvidenceError::Transport(_)
                ) {
                    SettlementFailure::Unavailable
                } else {
                    SettlementFailure::Invalid
                }
            })?;
        if checkpoint.canonical_header() != signed_header.canonical_bytes {
            return Err(SettlementFailure::Invalid);
        }
        let candidate = FinalityEvidenceCandidate::from_exact_bytes(
            checkpoint.checkpoint_bytes().to_vec(),
            checkpoint.context_bytes().to_vec(),
            self.verifier.policy().protocol_version,
            self.verifier.policy().network_id,
        )
        .map_err(|_| SettlementFailure::Invalid)?;
        let publication = self
            .verifier
            .verify(
                &candidate
                    .certificate()
                    .map_err(|_| SettlementFailure::Invalid)?,
                candidate
                    .set_version()
                    .map_err(|_| SettlementFailure::Invalid)?,
            )
            .map_err(|error| match error.fault {
                EndpointFault::Connect { .. }
                | EndpointFault::Transport { .. }
                | EndpointFault::Http { .. } => SettlementFailure::Unavailable,
                _ => SettlementFailure::Invalid,
            })?;
        let checkpoint = VerifiedCheckpoint::from_independent_publication(candidate, &publication)
            .map_err(|_| SettlementFailure::Invalid)?;
        let document = json!({
            "level": "settlement_verified", "source": "native_lni_and_independent_paxeer", "reason": Value::Null,
            "receipt_digest": hex(&layerx_proof::merkle::leaf_hash(receipt_bytes).map_err(|_| SettlementFailure::Invalid)?),
            "signed_header_digest": hex(&verified_header.digest()), "checkpoint_id": hex(&publication.checkpoint_id()),
            "network_id": publication.network_id(), "batch_number": batch.to_string(), "guarantor_set_version": checkpoint.set_version().to_string(),
            "registration_block": publication.registration().number.to_string(), "registration_block_hash": hex(&publication.registration().hash),
            "confirmed_head": publication.confirmed_head().number.to_string(), "confirmed_head_hash": hex(&publication.confirmed_head().hash),
            "trust_digest": self.trust_digest,
            "paxeer_endpoint": layerx_paxeer_verifier::canonical_endpoint_identity(&self.verifier.policy().endpoint).map_err(|_| SettlementFailure::Invalid)?,
            "paxeer_chain_id": self.verifier.policy().endpoint.expected_chain_id.to_string(),
            "canonical_genesis_root": hex(&self.verifier.policy().canonical_genesis_root),
        });
        Ok(VerifiedSettlement {
            receipt: receipt_bytes.to_vec(),
            activity_id: receipt.activity_id(),
            sequence: receipt.global_sequence(),
            batch,
            batch_id: receipt.batch_id(),
            document,
        })
    }

    pub fn reconcile(&mut self, store: &Store) -> Result<(), IndexError> {
        if self.reconnect_required {
            self.client
                .reconnect()
                .map_err(|_| IndexError::Source("settlement LNI reconnect unavailable".into()))?;
            self.reconnect_required = false;
        }
        let pending = store.pending_settlement(MAX_HISTORY_BATCHES)?;
        let Some(batch) = pending.iter().map(|(batch, _)| *batch).max() else {
            return Ok(());
        };
        let preparation = self.prepare_through(batch);
        for (batch, receipt) in pending {
            match preparation.and_then(|()| self.verify(&receipt, batch)) {
                Ok(verified) => store.record_settlement(&verified)?,
                Err(failure) => {
                    self.reconnect_required |= failure == SettlementFailure::Unavailable;
                    store.record_settlement_failure(&receipt, failure)?;
                }
            }
        }
        Ok(())
    }
}
