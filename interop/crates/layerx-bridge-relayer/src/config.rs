//! Relayer configuration: one JSON file naming the journal, the remote signer
//! and its key handles, Paxeer, and every Ethereum chain with its vault,
//! finality depth and RPC quorum, and optionally the Solana custody program.
//! The file holds handles and public keys only; no private key is ever
//! configured into this process.

use std::net::SocketAddr;
use std::path::{Path, PathBuf};
use std::time::Duration;

use layerx_mirror::rpc::{RpcCluster, RpcQuorumConfig};
use layerx_mirror::signer::{
    RemoteChainSigner, RemoteSignerConfig, SignerEndpoint, SigningAlgorithm,
};
use layerx_paxeer_verifier::{EndpointConfig, EndpointTransport};
use serde::Deserialize;

use crate::cosign::CosignDirectory;
use crate::hex;
use crate::journal::Journal;
use crate::relayer::{
    ChainLink, ChainSettings, GasPolicy, PaxeerLink, PaxeerSettings, RelayerAssembly, RelayerError,
    RelayerParts, RelayerSetup, SolanaLink, SolanaRelease,
};
use crate::rpc::PaxeerRpc;
use crate::signer::{Attestor, FeePayer, Submitter, ALGORITHM, FEE_PAYER_ALGORITHM};
use crate::solana::observe::SolanaSettings;
use crate::solana::rpc::{Commitment, SolanaRpc};
use crate::solana::{base58_fixed, SOLANA_CHAIN_ID};

const MAX_CONFIG_BYTES: u64 = 1024 * 1024;

/// The approved attestor set every relayer instance runs against.
pub const ATTESTOR_SET_SIZE: usize = 5;
/// Distinct attestor signatures a release needs from that set.
pub const ATTESTOR_THRESHOLD: u8 = 3;

#[derive(Clone, Debug, Deserialize, Eq, PartialEq)]
#[serde(tag = "transport", rename_all = "snake_case", deny_unknown_fields)]
pub enum SignerTransportConfig {
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

#[derive(Clone, Debug, Deserialize, Eq, PartialEq)]
#[serde(deny_unknown_fields)]
pub struct SignerConfig {
    pub endpoint: SignerTransportConfig,
    pub timeout_ms: u64,
}

/// An opaque signer policy handle and the public key it must answer for.
#[derive(Clone, Debug, Deserialize, Eq, PartialEq)]
#[serde(deny_unknown_fields)]
pub struct KeyHandleConfig {
    pub handle: String,
    /// SEC1 secp256k1 public key, `0x`-prefixed hex.
    #[serde(with = "hex::bytes_serde")]
    pub public_key: Vec<u8>,
}

#[derive(Clone, Copy, Debug, Deserialize, Eq, PartialEq)]
#[serde(deny_unknown_fields)]
pub struct GasConfig {
    pub gas_limit: u64,
    pub max_fee_per_gas: u128,
    pub max_priority_fee_per_gas: u128,
}

impl From<GasConfig> for GasPolicy {
    fn from(value: GasConfig) -> Self {
        Self {
            gas_limit: value.gas_limit,
            max_fee_per_gas: value.max_fee_per_gas,
            max_priority_fee_per_gas: value.max_priority_fee_per_gas,
        }
    }
}

#[derive(Clone, Debug, Deserialize, Eq, PartialEq)]
#[serde(deny_unknown_fields)]
pub struct PaxeerEndpointConfig {
    pub url: String,
    /// DER trust anchor of a TLS endpoint. Absent only with `local_emulator`.
    #[serde(default)]
    pub trust_anchor_der: Option<PathBuf>,
    #[serde(default)]
    pub local_emulator: bool,
    pub request_timeout_ms: u64,
}

#[derive(Clone, Debug, Deserialize, Eq, PartialEq)]
#[serde(deny_unknown_fields)]
pub struct PaxeerConfig {
    pub chain_id: u64,
    pub endpoints: Vec<PaxeerEndpointConfig>,
    pub finality_depth: u64,
    pub start_block: u64,
    pub max_block_range: u64,
    pub submitter: KeyHandleConfig,
    pub gas: GasConfig,
}

#[derive(Clone, Debug, Deserialize, Eq, PartialEq)]
#[serde(deny_unknown_fields)]
pub struct ChainConfig {
    pub chain_id: u64,
    #[serde(with = "hex::fixed_serde")]
    pub vault: [u8; 20],
    pub finality_depth: u64,
    pub start_block: u64,
    pub max_block_range: u64,
    pub rpc: RpcQuorumConfig,
    pub submitter: KeyHandleConfig,
    pub gas: GasConfig,
}

/// The Solana custody program: what a `chains[]` entry carries, in slots,
/// plus the program id and the commitment it is read at.
#[derive(Clone, Debug, Deserialize, Eq, PartialEq)]
#[serde(deny_unknown_fields)]
pub struct SolanaConfig {
    /// The reserved Solana chain id, [`SOLANA_CHAIN_ID`].
    pub chain_id: u64,
    /// The handle of the program's vault-authority PDA, as Paxeer registers it.
    #[serde(with = "hex::fixed_serde")]
    pub vault: [u8; 20],
    pub finality_depth: u64,
    pub start_slot: u64,
    pub max_slot_range: u64,
    pub rpc: RpcQuorumConfig,
    /// The ed25519 fee-payer key the remote signer holds; its public key is
    /// the 32-byte Solana account key.
    pub fee_payer: KeyHandleConfig,
    /// The custody program id, base58.
    pub program_id: String,
    /// `confirmed` or `finalized`; `processed` is refused.
    pub commitment: Commitment,
    /// Mints, base58, whose Paxeer burns this relayer releases on Solana;
    /// each is matched to a burn's asset id through its asset PDA. Empty, the
    /// relayer does not release on Solana.
    #[serde(default)]
    pub release_mints: Vec<String>,
}

impl SolanaConfig {
    /// The observer's settings.
    ///
    /// # Errors
    ///
    /// Refuses a program id that is not a base58 32-byte key.
    pub fn settings(&self) -> Result<SolanaSettings, RelayerError> {
        Ok(SolanaSettings {
            chain_id: self.chain_id,
            vault: self.vault,
            program_id: base58_fixed::<32>(&self.program_id)
                .map_err(|_| invalid("solana program id is not a base58 32-byte key"))?,
            finality_depth: self.finality_depth,
            start_slot: self.start_slot,
            max_slot_range: self.max_slot_range,
            commitment: self.commitment,
        })
    }

    fn validate(
        &self,
        attestor: &KeyHandleConfig,
        chains: &[ChainConfig],
    ) -> Result<(), RelayerError> {
        if self.chain_id != SOLANA_CHAIN_ID
            || chains.iter().any(|chain| chain.chain_id == SOLANA_CHAIN_ID)
        {
            return Err(invalid(format!(
                "solana must use the reserved chain id {SOLANA_CHAIN_ID} and no ethereum chain may"
            )));
        }
        if self.fee_payer.handle.is_empty() || self.fee_payer.public_key.len() != 32 {
            return Err(invalid(
                "the solana fee payer needs a handle and a 32-byte public key",
            ));
        }
        if self.fee_payer.handle == attestor.handle {
            return Err(invalid("the attestor key must not pay transaction fees"));
        }
        if self.vault == [0; 20] || self.finality_depth == 0 || self.max_slot_range == 0 {
            return Err(invalid(
                "solana needs a vault, a finality depth and a slot range",
            ));
        }
        let settings = self.settings()?;
        if settings.program_id == [0; 32] {
            return Err(invalid("solana program id must not be the zero key"));
        }
        let mints = self.mints()?;
        let distinct: std::collections::BTreeSet<[u8; 32]> = mints.iter().copied().collect();
        if distinct.len() != mints.len() || distinct.contains(&[0; 32]) {
            return Err(invalid(
                "solana release mints must be distinct non-zero keys",
            ));
        }
        Ok(())
    }

    /// The release mints as keys.
    ///
    /// # Errors
    ///
    /// Refuses a mint that is not a base58 32-byte key.
    pub fn mints(&self) -> Result<Vec<[u8; 32]>, RelayerError> {
        self.release_mints
            .iter()
            .map(|mint| {
                base58_fixed::<32>(mint)
                    .map_err(|_| invalid("a solana release mint is not a base58 32-byte key"))
            })
            .collect()
    }
}

/// The approved attestor membership: five distinct addresses and the
/// three-of-five threshold. This instance's attestor must be a member.
#[derive(Clone, Debug, Deserialize, Eq, PartialEq)]
#[serde(deny_unknown_fields)]
pub struct AttestorSetConfig {
    /// `0x`-prefixed 20-byte attestor addresses.
    pub members: Vec<String>,
    pub threshold: u8,
}

impl AttestorSetConfig {
    /// The member addresses.
    ///
    /// # Errors
    ///
    /// Refuses a member that is not a 20-byte hex address.
    pub fn addresses(&self) -> Result<Vec<[u8; 20]>, RelayerError> {
        self.members
            .iter()
            .map(|member| {
                hex::fixed::<20>(member).map_err(|_| {
                    invalid(format!("attestor member {member} is not a 20-byte address"))
                })
            })
            .collect()
    }

    fn validate(
        &self,
        attestor: &KeyHandleConfig,
        cosign_directory: Option<&PathBuf>,
    ) -> Result<(), RelayerError> {
        let addresses = self.addresses()?;
        let distinct: std::collections::BTreeSet<[u8; 20]> = addresses.iter().copied().collect();
        if addresses.len() != ATTESTOR_SET_SIZE
            || distinct.len() != ATTESTOR_SET_SIZE
            || distinct.contains(&[0; 20])
        {
            return Err(invalid(format!(
                "the attestor set needs exactly {ATTESTOR_SET_SIZE} distinct non-zero members"
            )));
        }
        if self.threshold != ATTESTOR_THRESHOLD {
            return Err(invalid(format!(
                "the attestor threshold must be {ATTESTOR_THRESHOLD} of {ATTESTOR_SET_SIZE}"
            )));
        }
        if cosign_directory.is_none() {
            return Err(invalid("a threshold above one needs a cosign directory"));
        }
        let key = k256::ecdsa::VerifyingKey::from_sec1_bytes(&attestor.public_key)
            .map_err(|_| invalid("the attestor public key is not a secp256k1 key"))?;
        if !distinct.contains(&crate::attestation::ethereum_address(&key)) {
            return Err(invalid("this attestor is not a member of the attestor set"));
        }
        Ok(())
    }
}

#[derive(Clone, Debug, Deserialize, Eq, PartialEq)]
#[serde(deny_unknown_fields)]
pub struct RelayerConfig {
    pub journal_path: PathBuf,
    /// Shared directory for exchanging attestor signatures when a threshold
    /// above one needs several relayer instances.
    #[serde(default)]
    pub cosign_directory: Option<PathBuf>,
    pub poll_interval_ms: u64,
    pub max_submissions: u32,
    pub signer: SignerConfig,
    pub attestor: KeyHandleConfig,
    pub paxeer: PaxeerConfig,
    pub chains: Vec<ChainConfig>,
    /// The Solana custody program; absent, the relayer relays the Ethereum
    /// chains only.
    #[serde(default)]
    pub solana: Option<SolanaConfig>,
    /// The approved attestor set; absent, the relayer runs a single attestor.
    #[serde(default)]
    pub attestor_set: Option<AttestorSetConfig>,
}

fn invalid(detail: impl Into<String>) -> RelayerError {
    RelayerError::Configuration(detail.into())
}

impl RelayerConfig {
    /// Reads and validates a configuration file.
    ///
    /// # Errors
    ///
    /// Refuses unreadable, oversized, unknown-field or inconsistent files.
    pub fn load(path: &Path) -> Result<Self, RelayerError> {
        let metadata = std::fs::metadata(path).map_err(|error| invalid(error.to_string()))?;
        if metadata.len() > MAX_CONFIG_BYTES {
            return Err(invalid("configuration file is too large"));
        }
        let text = std::fs::read_to_string(path).map_err(|error| invalid(error.to_string()))?;
        let config: Self =
            serde_json::from_str(&text).map_err(|error| invalid(error.to_string()))?;
        config.validate()?;
        Ok(config)
    }

    /// Checks everything that can be checked without the network.
    ///
    /// # Errors
    ///
    /// Names the first inconsistency.
    pub fn validate(&self) -> Result<(), RelayerError> {
        if self.poll_interval_ms == 0 || self.max_submissions == 0 {
            return Err(invalid(
                "poll interval and submission budget must be positive",
            ));
        }
        if self.signer.timeout_ms == 0 {
            return Err(invalid("signer timeout must be positive"));
        }
        if self.paxeer.endpoints.is_empty() {
            return Err(invalid("paxeer needs at least one endpoint"));
        }
        for endpoint in &self.paxeer.endpoints {
            if endpoint.request_timeout_ms == 0
                || endpoint.local_emulator == endpoint.trust_anchor_der.is_some()
            {
                return Err(invalid(format!(
                    "paxeer endpoint {} needs a timeout and exactly one of a trust anchor or local_emulator",
                    endpoint.url
                )));
            }
        }
        if self.chains.is_empty() {
            return Err(invalid("at least one chain is required"));
        }
        let handles = std::iter::once(&self.attestor)
            .chain(std::iter::once(&self.paxeer.submitter))
            .chain(self.chains.iter().map(|chain| &chain.submitter));
        for key in handles {
            if key.handle.is_empty() || key.public_key.is_empty() {
                return Err(invalid("every key needs a handle and a public key"));
            }
        }
        if self
            .chains
            .iter()
            .any(|chain| chain.submitter.handle == self.attestor.handle)
            || self.paxeer.submitter.handle == self.attestor.handle
        {
            return Err(invalid("the attestor key must not pay transaction fees"));
        }
        if let Some(solana) = &self.solana {
            solana.validate(&self.attestor, &self.chains)?;
        }
        if let Some(set) = &self.attestor_set {
            set.validate(&self.attestor, self.cosign_directory.as_ref())?;
        }
        Ok(())
    }

    fn signer_endpoint(&self) -> SignerEndpoint {
        match &self.signer.endpoint {
            SignerTransportConfig::Uds { socket } => SignerEndpoint::Uds {
                socket: socket.clone(),
            },
            SignerTransportConfig::MutualTls {
                endpoint,
                server_name,
                trust_anchor,
                client_certificate,
                client_private_key,
            } => SignerEndpoint::MutualTls {
                endpoint: *endpoint,
                server_name: server_name.clone(),
                trust_anchor: trust_anchor.clone(),
                client_certificate: client_certificate.clone(),
                client_private_key: client_private_key.clone(),
            },
        }
    }

    fn remote_signer(&self, key: &KeyHandleConfig) -> Result<RemoteChainSigner, RelayerError> {
        self.remote_signer_for(key, ALGORITHM)
    }

    fn remote_signer_for(
        &self,
        key: &KeyHandleConfig,
        algorithm: SigningAlgorithm,
    ) -> Result<RemoteChainSigner, RelayerError> {
        RemoteChainSigner::new(RemoteSignerConfig {
            endpoint: self.signer_endpoint(),
            algorithm,
            key_handle: key.handle.clone(),
            public_key: key.public_key.clone(),
            timeout: Duration::from_millis(self.signer.timeout_ms),
        })
        .map_err(|error| invalid(format!("signer handle {}: {error:?}", key.handle)))
    }

    fn paxeer_endpoints(&self) -> Result<Vec<EndpointConfig>, RelayerError> {
        self.paxeer
            .endpoints
            .iter()
            .map(|endpoint| {
                let transport = match &endpoint.trust_anchor_der {
                    Some(path) => EndpointTransport::PinnedTls {
                        trust_anchor_der: std::fs::read(path)
                            .map_err(|error| invalid(format!("{}: {error}", path.display())))?,
                    },
                    None => EndpointTransport::LocalEmulator,
                };
                Ok(EndpointConfig {
                    url: endpoint.url.clone(),
                    request_timeout: Duration::from_millis(endpoint.request_timeout_ms),
                    transport,
                    expected_chain_id: self.paxeer.chain_id,
                })
            })
            .collect()
    }

    /// Opens the journal and connects every transport and key handle, the
    /// Solana fee payer as an ed25519 handle when release mints are
    /// configured.
    ///
    /// # Errors
    ///
    /// Returns the first transport, signer or journal failure.
    pub fn build(&self) -> Result<RelayerSetup, RelayerError> {
        self.validate()?;
        let attestor = Attestor::new(self.remote_signer(&self.attestor)?)
            .map_err(|error| invalid(format!("attestor: {error:?}")))?;
        let paxeer = PaxeerLink {
            settings: PaxeerSettings {
                chain_id: self.paxeer.chain_id,
                finality_depth: self.paxeer.finality_depth,
                start_block: self.paxeer.start_block,
                max_block_range: self.paxeer.max_block_range,
                gas: self.paxeer.gas.into(),
            },
            rpc: Box::new(PaxeerRpc::new(self.paxeer_endpoints()?)?),
            submitter: Submitter::paxeer(self.remote_signer(&self.paxeer.submitter)?)
                .map_err(|error| invalid(format!("paxeer submitter: {error:?}")))?,
        };
        let mut chains = Vec::with_capacity(self.chains.len());
        for chain in &self.chains {
            chains.push(ChainLink {
                settings: ChainSettings {
                    chain_id: chain.chain_id,
                    vault: chain.vault,
                    finality_depth: chain.finality_depth,
                    start_block: chain.start_block,
                    max_block_range: chain.max_block_range,
                    gas: chain.gas.into(),
                },
                rpc: Box::new(RpcCluster::new(&chain.rpc).map_err(|error| {
                    invalid(format!("chain {} rpc: {error:?}", chain.chain_id))
                })?),
                submitter: Submitter::ethereum(self.remote_signer(&chain.submitter)?).map_err(
                    |error| invalid(format!("chain {} submitter: {error:?}", chain.chain_id)),
                )?,
            });
        }
        let solana = match &self.solana {
            Some(solana) => Some(SolanaLink {
                settings: solana.settings()?,
                rpc: SolanaRpc::new(Box::new(
                    RpcCluster::new(&solana.rpc)
                        .map_err(|error| invalid(format!("solana rpc: {error:?}")))?,
                )),
            }),
            None => None,
        };
        let release = match &self.solana {
            Some(solana) if !solana.release_mints.is_empty() => Some(SolanaRelease {
                fee_payer: self.fee_payer(solana)?,
                mints: solana.mints()?,
            }),
            _ => None,
        };
        let assembly = RelayerAssembly {
            parts: RelayerParts {
                attestor,
                paxeer,
                chains,
                journal: Journal::open(&self.journal_path)?,
                cosign: self.cosign_directory.clone().map(CosignDirectory::new),
                max_submissions: self.max_submissions,
            },
            solana,
        };
        Ok(RelayerSetup { assembly, release })
    }

    fn fee_payer(&self, solana: &SolanaConfig) -> Result<FeePayer, RelayerError> {
        FeePayer::new(self.remote_signer_for(&solana.fee_payer, FEE_PAYER_ALGORITHM)?)
            .map_err(|error| invalid(format!("solana fee payer: {error:?}")))
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    const EXAMPLE: &str = r#"{
        "journal_path": "/var/lib/layerx-bridge-relayer/journal.jsonl",
        "cosign_directory": "/var/lib/layerx-bridge-relayer/cosign",
        "poll_interval_ms": 4000,
        "max_submissions": 3,
        "signer": {"endpoint": {"transport": "uds", "socket": "/run/layerx-signer.sock"}, "timeout_ms": 2000},
        "attestor": {"handle": "bridge-attestor-1", "public_key": "0x02aa"},
        "paxeer": {
            "chain_id": 229,
            "endpoints": [{"url": "http://127.0.0.1:8545", "local_emulator": true, "request_timeout_ms": 3000}],
            "finality_depth": 2, "start_block": 0, "max_block_range": 500,
            "submitter": {"handle": "bridge-paxeer-fees", "public_key": "0x02bb"},
            "gas": {"gas_limit": 1000000, "max_fee_per_gas": 100000000000, "max_priority_fee_per_gas": 2000000000}
        },
        "chains": [{
            "chain_id": 1,
            "vault": "0x1111111111111111111111111111111111111111",
            "finality_depth": 64, "start_block": 19000000, "max_block_range": 1000,
            "rpc": {"endpoints": [], "quorum": 2, "connect_timeout_ms": 1000, "request_timeout_ms": 3000, "maximum_response_bytes": 1048576},
            "submitter": {"handle": "bridge-ethereum-fees", "public_key": "0x02cc"},
            "gas": {"gas_limit": 400000, "max_fee_per_gas": 200000000000, "max_priority_fee_per_gas": 3000000000}
        }]
    }"#;

    fn example() -> RelayerConfig {
        serde_json::from_str(EXAMPLE).unwrap_or_else(|error| panic!("example: {error}"))
    }

    #[test]
    fn the_example_configuration_parses_and_validates() {
        let config = example();
        assert_eq!(config.chains[0].vault, [0x11; 20]);
        assert_eq!(config.attestor.public_key, vec![0x02, 0xaa]);
        assert_eq!(config.validate(), Ok(()));
    }

    #[test]
    fn unknown_fields_and_inline_secrets_are_refused() {
        let with_secret = EXAMPLE.replacen(
            r#""handle": "bridge-attestor-1","#,
            r#""handle": "bridge-attestor-1", "private_key": "0x01","#,
            1,
        );
        assert!(serde_json::from_str::<RelayerConfig>(&with_secret).is_err());
    }

    #[test]
    fn an_attestor_that_pays_fees_is_refused() {
        let mut config = example();
        config.paxeer.submitter.handle = config.attestor.handle.clone();
        assert!(config.validate().is_err());
    }

    const SOLANA: &str = r#""solana": {
            "chain_id": 91600046870081,
            "vault": "0x334121a65b47bd45c3f6381537d9180e98e445bc",
            "finality_depth": 32, "start_slot": 1000, "max_slot_range": 500,
            "rpc": {"endpoints": [], "quorum": 2, "connect_timeout_ms": 1000, "request_timeout_ms": 3000, "maximum_response_bytes": 1048576},
            "fee_payer": {"handle": "bridge-solana-fees", "public_key": "0x3333333333333333333333333333333333333333333333333333333333333333"},
            "program_id": "A7SZbByPYuHpunZ9pyMDMrhMYvK44ANT1AVqb8U1FpM9",
            "commitment": "finalized"
        },
        "chains": ["#;

    fn with_solana() -> RelayerConfig {
        let text = EXAMPLE.replacen(r#""chains": ["#, SOLANA, 1);
        serde_json::from_str(&text).unwrap_or_else(|error| panic!("solana example: {error}"))
    }

    #[test]
    fn the_solana_entry_is_optional_and_validates_against_the_reserved_id() {
        assert_eq!(example().solana, None);
        let config = with_solana();
        assert_eq!(config.validate(), Ok(()));
        let solana = config
            .solana
            .as_ref()
            .unwrap_or_else(|| panic!("solana entry"));
        let settings = solana
            .settings()
            .unwrap_or_else(|error| panic!("settings: {error}"));
        assert_eq!(settings.chain_id, SOLANA_CHAIN_ID);
        assert_eq!(settings.commitment, Commitment::Finalized);
        assert_eq!(settings.program_id[0], 0x87);

        let mut wrong_id = with_solana();
        if let Some(solana) = wrong_id.solana.as_mut() {
            solana.chain_id = 101;
        }
        assert!(wrong_id.validate().is_err());

        let mut paying_attestor = with_solana();
        if let Some(solana) = paying_attestor.solana.as_mut() {
            solana.fee_payer.handle = paying_attestor.attestor.handle.clone();
        }
        assert!(paying_attestor.validate().is_err());

        let processed = EXAMPLE.replacen(
            r#""chains": ["#,
            &SOLANA.replace(r#""finalized""#, r#""processed""#),
            1,
        );
        assert!(serde_json::from_str::<RelayerConfig>(&processed).is_err());
    }

    #[test]
    fn solana_release_mints_are_optional_distinct_base58_keys() {
        let config = with_solana();
        let solana = config
            .solana
            .as_ref()
            .unwrap_or_else(|| panic!("solana entry"));
        assert!(solana.release_mints.is_empty());
        assert_eq!(solana.mints(), Ok(Vec::new()));

        let releasing = EXAMPLE.replacen(
            r#""chains": ["#,
            &SOLANA.replace(
                r#""commitment": "finalized""#,
                r#""commitment": "finalized", "release_mints": ["5w3wVdJaESaJKyLmStM6Hv9UyUkmZ1b9DLQquAqqpump"]"#,
            ),
            1,
        );
        let mut config: RelayerConfig =
            serde_json::from_str(&releasing).unwrap_or_else(|error| panic!("releasing: {error}"));
        assert_eq!(config.validate(), Ok(()));
        let mints = config
            .solana
            .as_ref()
            .map_or_else(|| panic!("solana entry"), SolanaConfig::mints)
            .unwrap_or_else(|error| panic!("mints: {error}"));
        assert_eq!(mints.len(), 1);
        assert_eq!(mints[0][0], 0x49);

        if let Some(solana) = config.solana.as_mut() {
            solana.release_mints.push(solana.release_mints[0].clone());
        }
        assert!(config.validate().is_err());
        if let Some(solana) = config.solana.as_mut() {
            solana.release_mints = vec!["not-a-key".to_owned()];
        }
        assert!(config.validate().is_err());
        if let Some(solana) = config.solana.as_mut() {
            solana.release_mints = vec!["11111111111111111111111111111111".to_owned()];
        }
        assert!(config.validate().is_err());
    }

    #[test]
    fn a_paxeer_endpoint_needs_exactly_one_trust_mode() {
        let mut config = example();
        config.paxeer.endpoints[0].local_emulator = false;
        assert!(config.validate().is_err());
        config.paxeer.endpoints[0].trust_anchor_der = Some(PathBuf::from("/etc/anchor.der"));
        assert_eq!(config.validate(), Ok(()));
    }

    const GENERATOR_KEY: &str =
        "0x0279be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f81798";
    const GENERATOR_ADDRESS: &str = "0x7e5f4552091a69125d5dfcb7b8c2659029395bdf";

    fn with_set() -> RelayerConfig {
        let mut config = example();
        config.attestor.public_key =
            hex::decode(GENERATOR_KEY).unwrap_or_else(|error| panic!("key: {error:?}"));
        config.attestor_set = Some(AttestorSetConfig {
            members: vec![
                GENERATOR_ADDRESS.to_owned(),
                "0x2222222222222222222222222222222222222222".to_owned(),
                "0x3333333333333333333333333333333333333333".to_owned(),
                "0x4444444444444444444444444444444444444444".to_owned(),
                "0x5555555555555555555555555555555555555555".to_owned(),
            ],
            threshold: ATTESTOR_THRESHOLD,
        });
        config
    }

    fn set(config: &mut RelayerConfig) -> &mut AttestorSetConfig {
        config
            .attestor_set
            .as_mut()
            .unwrap_or_else(|| panic!("attestor set"))
    }

    #[test]
    fn a_five_member_three_of_five_set_validates() {
        assert_eq!(example().attestor_set, None);
        assert_eq!(with_set().validate(), Ok(()));
        let parsed: RelayerConfig = serde_json::from_str(&EXAMPLE.replacen(
            r#""chains": ["#,
            r#""attestor_set": {"members": ["0x01"], "threshold": 3}, "chains": ["#,
            1,
        ))
        .unwrap_or_else(|error| panic!("set: {error}"));
        assert!(parsed.validate().is_err());
    }

    #[test]
    fn an_attestor_set_of_the_wrong_size_or_with_repeats_is_refused() {
        let mut short = with_set();
        set(&mut short).members.pop();
        assert!(short.validate().is_err());

        let mut long = with_set();
        set(&mut long)
            .members
            .push("0x6666666666666666666666666666666666666666".to_owned());
        assert!(long.validate().is_err());

        let mut repeated = with_set();
        set(&mut repeated).members[4] = "0x2222222222222222222222222222222222222222".to_owned();
        assert!(repeated.validate().is_err());

        let mut zero = with_set();
        set(&mut zero).members[4] = "0x0000000000000000000000000000000000000000".to_owned();
        assert!(zero.validate().is_err());

        let mut malformed = with_set();
        set(&mut malformed).members[4] = "0x5555".to_owned();
        assert!(malformed.validate().is_err());
    }

    #[test]
    fn a_threshold_other_than_three_is_refused() {
        for threshold in [0, 1, 2, 4, 5, 6] {
            let mut config = with_set();
            set(&mut config).threshold = threshold;
            assert!(config.validate().is_err(), "threshold {threshold}");
        }
    }

    #[test]
    fn the_attestor_must_be_a_member_and_needs_a_cosign_directory() {
        let mut outsider = with_set();
        set(&mut outsider).members[0] = "0x1111111111111111111111111111111111111111".to_owned();
        assert!(outsider.validate().is_err());

        let mut invalid_key = with_set();
        invalid_key.attestor.public_key = vec![0x02, 0xaa];
        assert!(invalid_key.validate().is_err());

        let mut alone = with_set();
        alone.cosign_directory = None;
        assert!(alone.validate().is_err());
    }
}
