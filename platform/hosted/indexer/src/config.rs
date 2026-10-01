//! Environment configuration of the indexer service.

use std::fs::File;
use std::io::Read as _;
use std::net::SocketAddr;
use std::os::unix::fs::MetadataExt as _;
use std::path::{Path, PathBuf};
use std::time::Duration;

use layerx_client::handover::{decode_finality_policy, SequencerHistory};
use layerx_paxeer_verifier::PaxeerCheckpointVerifier;
use layerx_proof::inclusion::SequencerAuthorization;
use layerx_wire::handover::{sequencer_id, GENESIS_TRUST_MAX_BYTES};
use serde_json::json;

use crate::abi::address_text;
use crate::codec::unhex_fixed;
use crate::follow::FollowPolicy;
use crate::paxeer::AttributeEncoding;
use crate::paxscan::PaxscanTls;
use crate::store::AssetRow;
use crate::transport::{Endpoint, Security};
use crate::IndexError;

/// The LayerX anchor's `ChallengeWindowSeconds` genesis default.
pub const ANCHOR_DEFAULT_CHALLENGE_WINDOW_SECONDS: u64 = 0;

const DEFAULT_LISTEN: &str = "127.0.0.1:8095";
const DEFAULT_BLOCK_TIME_MS: u64 = 1_000;
const DEFAULT_BATCH_INTERVAL_MS: u64 = 1_000;
const DEFAULT_POLL_MS: u64 = 1_000;
const DEFAULT_TIMEOUT_MS: u64 = 10_000;
const DEFAULT_MAX_UNITS_PER_STEP: u64 = 64;
const DEFAULT_BACKFILL_RANGE_BLOCKS: u64 = 1_000;

/// The number of units a reorganisation may still reach: every unit whose
/// age is inside the challenge window stays reversible, and at least the
/// newest unit always is.
#[must_use]
pub fn finality_depth_from_challenge_window(window_seconds: u64, unit_interval_ms: u64) -> u64 {
    let interval = unit_interval_ms.max(1);
    window_seconds
        .saturating_mul(1_000)
        .div_ceil(interval)
        .max(1)
}

/// Where the Paxeer half reads from.
#[derive(Clone, Debug)]
pub struct PaxeerSource {
    pub evm: Endpoint,
    pub comet: Option<Endpoint>,
    pub start_block: u64,
    pub chain_id: Option<u64>,
    pub encoding: AttributeEncoding,
    pub policy: FollowPolicy,
}

/// Where the LayerX half reads from.
#[derive(Clone, Debug)]
pub struct LayerXSource {
    pub relay: Endpoint,
    pub start_batch: Option<u64>,
    pub policy: FollowPolicy,
    /// Receipt settlement verification trust; `None` publishes every receipt
    /// as settlement-unverified.
    pub settlement: Option<SettlementTrust>,
}

/// The settlement trust variables. All six are set together or none is:
/// - `LAYERX_INDEXER_GENESIS_TRUST`: absolute path of the protected genesis
///   trust artifact (owner-only file) that pins the initial sequencer key;
/// - `LAYERX_INDEXER_HANDOVER_FINALITY`: absolute path of the protected
///   handover finality policy (Paxeer endpoint, registry, guarantor bond,
///   protocol version, network, genesis root, confirmations);
/// - `LAYERX_INDEXER_SEQUENCER_ID`: 32-byte hex sequencer id, derived from
///   the public key;
/// - `LAYERX_INDEXER_SEQUENCER_PUBLIC_KEY`: 32-byte hex genesis sequencer key;
/// - `LAYERX_INDEXER_SEQUENCER_FIRST_BATCH` / `LAYERX_INDEXER_SEQUENCER_LAST_BATCH`:
///   inclusive decimal batch range that key is authorised for.
pub const SETTLEMENT_VARIABLES: [&str; 6] = [
    "LAYERX_INDEXER_GENESIS_TRUST",
    "LAYERX_INDEXER_HANDOVER_FINALITY",
    "LAYERX_INDEXER_SEQUENCER_ID",
    "LAYERX_INDEXER_SEQUENCER_PUBLIC_KEY",
    "LAYERX_INDEXER_SEQUENCER_FIRST_BATCH",
    "LAYERX_INDEXER_SEQUENCER_LAST_BATCH",
];

const FINALITY_POLICY_MAX_BYTES: u64 = 1_048_576;

/// The pinned trust receipt settlement is verified against: the sequencer
/// authorisation bound to the genesis trust artifact and the independent
/// Paxeer publication verifier under the handover finality policy.
#[derive(Clone, Debug)]
pub struct SettlementTrust {
    pub authorization: SequencerAuthorization,
    pub verifier: PaxeerCheckpointVerifier,
}

impl SettlementTrust {
    /// Reads the settlement trust through `lookup`; `None` when none of
    /// [`SETTLEMENT_VARIABLES`] is set.
    ///
    /// # Errors
    /// Refuses a partial set, malformed pins, an unprotected or unreadable
    /// file, a policy the canonical decoder or verifier refuses, a sequencer
    /// id that does not derive from the key, and a genesis artifact that does
    /// not pin that key under the policy's network and genesis root.
    pub fn from_lookup<F>(lookup: &F) -> Result<Option<Self>, IndexError>
    where
        F: Fn(&str) -> Option<String>,
    {
        let missing: Vec<&str> = SETTLEMENT_VARIABLES
            .into_iter()
            .filter(|name| lookup(name).is_none())
            .collect();
        if missing.len() == SETTLEMENT_VARIABLES.len() {
            return Ok(None);
        }
        if !missing.is_empty() {
            return Err(IndexError::Config(format!(
                "settlement trust is incomplete: set {}",
                missing.join(", ")
            )));
        }
        let value = |name: &str| lookup(name).unwrap_or_default().trim().to_owned();
        let authorization = SequencerAuthorization::from_config(
            &value("LAYERX_INDEXER_SEQUENCER_ID"),
            &value("LAYERX_INDEXER_SEQUENCER_PUBLIC_KEY"),
            &value("LAYERX_INDEXER_SEQUENCER_FIRST_BATCH"),
            &value("LAYERX_INDEXER_SEQUENCER_LAST_BATCH"),
        )
        .map_err(|field| IndexError::Config(format!("settlement {field} is malformed")))?;
        let key = authorization.public_key();
        if sequencer_id(&key).ok() != Some(authorization.sequencer_id()) {
            return Err(IndexError::Config(
                "LAYERX_INDEXER_SEQUENCER_ID does not derive from LAYERX_INDEXER_SEQUENCER_PUBLIC_KEY"
                    .to_owned(),
            ));
        }
        let policy_name = "LAYERX_INDEXER_HANDOVER_FINALITY";
        let policy = decode_finality_policy(&protected_file(
            policy_name,
            &value(policy_name),
            FINALITY_POLICY_MAX_BYTES,
        )?)
        .map_err(|_| IndexError::Config(format!("{policy_name} is not a finality policy")))?;
        let genesis_name = "LAYERX_INDEXER_GENESIS_TRUST";
        let genesis_limit = u64::try_from(GENESIS_TRUST_MAX_BYTES)
            .map_err(|_| IndexError::Config(format!("{genesis_name} limit exceeds u64")))?;
        let artifact = protected_file(genesis_name, &value(genesis_name), genesis_limit)?;
        SequencerHistory::from_genesis_artifact(
            &artifact,
            policy.network_id,
            policy.canonical_genesis_root,
            key,
        )
        .map_err(|_| {
            IndexError::Config(format!(
                "{genesis_name} does not pin the sequencer key under {policy_name}"
            ))
        })?;
        let verifier = PaxeerCheckpointVerifier::new(policy)
            .map_err(|_| IndexError::Config(format!("{policy_name} is refused by the verifier")))?;
        Ok(Some(Self {
            authorization,
            verifier,
        }))
    }
}

/// Reads trust material only from an absolute canonical regular file owned
/// by this process's user with no group or other permission bits.
fn protected_file(name: &str, path: &str, maximum: u64) -> Result<Vec<u8>, IndexError> {
    let refuse = |why: &str| IndexError::Config(format!("{name} {path}: {why}"));
    let path_ref = Path::new(path);
    if !path_ref.is_absolute() || std::fs::canonicalize(path_ref).ok().as_deref() != Some(path_ref)
    {
        return Err(refuse("must be an absolute canonical path"));
    }
    let owner = std::fs::metadata("/proc/self")
        .map_err(|error| refuse(&error.to_string()))?
        .uid();
    let file = File::open(path_ref).map_err(|error| refuse(&error.to_string()))?;
    let metadata = file
        .metadata()
        .map_err(|error| refuse(&error.to_string()))?;
    if !metadata.is_file() || metadata.uid() != owner || metadata.mode() & 0o077 != 0 {
        return Err(refuse(
            "must be a regular file owned by this user without group or other access",
        ));
    }
    let mut bytes = Vec::new();
    file.take(maximum.saturating_add(1))
        .read_to_end(&mut bytes)
        .map_err(|error| refuse(&error.to_string()))?;
    if !u64::try_from(bytes.len()).is_ok_and(|length| length <= maximum) {
        return Err(refuse("exceeds its size limit"));
    }
    Ok(bytes)
}

/// The whole service configuration.
#[derive(Clone, Debug)]
pub struct Config {
    pub database: PathBuf,
    pub listen: SocketAddr,
    pub tls: bool,
    pub abi_dir: Option<PathBuf>,
    pub layerx: Option<LayerXSource>,
    pub paxeer: Option<PaxeerSource>,
    pub pointers: Vec<AssetRow>,
    pub poll: Duration,
}

/// The backfill mode's settings: `backfill --cutover-height N
/// [--range-blocks N]` plus the paxscan database environment.
#[derive(Clone, Eq, PartialEq)]
pub struct BackfillConfig {
    /// The last height the backfill indexes; live ingestion resumes after it.
    pub cutover: u64,
    /// Heights read from paxscan per step.
    pub range_blocks: u64,
    /// The paxscan Postgres URL (`PAXSCAN_DATABASE_PUBLIC_URL`).
    pub paxscan_url: String,
    pub paxscan_tls: PaxscanTls,
}

impl std::fmt::Debug for BackfillConfig {
    fn fmt(&self, formatter: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        formatter
            .debug_struct("BackfillConfig")
            .field("cutover", &self.cutover)
            .field("range_blocks", &self.range_blocks)
            .field("paxscan_url", &"<redacted>")
            .field("paxscan_tls", &self.paxscan_tls)
            .finish()
    }
}

impl BackfillConfig {
    /// Parses the backfill arguments (after the `backfill` word) and reads
    /// the paxscan settings through `lookup`.
    ///
    /// # Errors
    /// Refuses a missing or malformed `--cutover-height`, an unknown
    /// argument, a missing `PAXSCAN_DATABASE_PUBLIC_URL`, and a missing
    /// TLS authentication choice.
    pub fn from_args<F>(args: &[String], lookup: &F) -> Result<Self, IndexError>
    where
        F: Fn(&str) -> Option<String>,
    {
        let mut cutover = None;
        let mut range_blocks = number(
            lookup,
            "LAYERX_INDEXER_BACKFILL_RANGE_BLOCKS",
            DEFAULT_BACKFILL_RANGE_BLOCKS,
        )?;
        let mut rest = args.iter();
        while let Some(argument) = rest.next() {
            let (name, inline) = match argument.split_once('=') {
                Some((name, value)) => (name, Some(value.to_owned())),
                None => (argument.as_str(), None),
            };
            let mut value = || {
                inline
                    .clone()
                    .or_else(|| rest.next().cloned())
                    .ok_or_else(|| IndexError::Config(format!("{name} needs a value")))
            };
            let parse = |text: String| {
                text.trim()
                    .parse::<u64>()
                    .map_err(|_| IndexError::Config(format!("{name} must be a decimal number")))
            };
            match name {
                "--cutover-height" => cutover = Some(parse(value()?)?),
                "--range-blocks" => range_blocks = parse(value()?)?,
                other => {
                    return Err(IndexError::Config(format!(
                        "unknown backfill argument {other}"
                    )))
                }
            }
        }
        let cutover = cutover
            .ok_or_else(|| IndexError::Config("backfill needs --cutover-height".to_owned()))?;
        let paxscan_url = lookup("PAXSCAN_DATABASE_PUBLIC_URL").ok_or_else(|| {
            IndexError::Config("PAXSCAN_DATABASE_PUBLIC_URL is required for backfill".to_owned())
        })?;
        let paxscan_tls = match (
            lookup("LAYERX_INDEXER_PAXSCAN_CERT_SHA256"),
            lookup("LAYERX_INDEXER_PAXSCAN_TLS_UNAUTHENTICATED").as_deref(),
        ) {
            (Some(pin), _) => PaxscanTls::PinnedLeaf(unhex_fixed::<32>(pin.trim()).map_err(
                |_| {
                    IndexError::Config(
                        "LAYERX_INDEXER_PAXSCAN_CERT_SHA256 must be 32 bytes of hex".to_owned(),
                    )
                },
            )?),
            (None, Some("1")) => PaxscanTls::Unauthenticated,
            (None, _) => {
                return Err(IndexError::Config(
                    "set LAYERX_INDEXER_PAXSCAN_CERT_SHA256 to pin the paxscan certificate, or LAYERX_INDEXER_PAXSCAN_TLS_UNAUTHENTICATED=1 to accept it unauthenticated"
                        .to_owned(),
                ))
            }
        };
        Ok(Self {
            cutover,
            range_blocks: range_blocks.max(1),
            paxscan_url,
            paxscan_tls,
        })
    }
}

fn number<F>(lookup: &F, name: &str, default: u64) -> Result<u64, IndexError>
where
    F: Fn(&str) -> Option<String>,
{
    lookup(name).map_or(Ok(default), |text| {
        text.trim()
            .parse()
            .map_err(|_| IndexError::Config(format!("{name} must be a decimal number")))
    })
}

fn optional_number<F>(lookup: &F, name: &str) -> Result<Option<u64>, IndexError>
where
    F: Fn(&str) -> Option<String>,
{
    lookup(name)
        .map(|text| {
            text.trim()
                .parse()
                .map_err(|_| IndexError::Config(format!("{name} must be a decimal number")))
        })
        .transpose()
}

fn attribute_encoding<F>(lookup: &F) -> Result<AttributeEncoding, IndexError>
where
    F: Fn(&str) -> Option<String>,
{
    match lookup("LAYERX_INDEXER_COMET_ATTRIBUTES").as_deref() {
        None | Some("base64") => Ok(AttributeEncoding::Base64),
        Some("plain") => Ok(AttributeEncoding::Plain),
        Some(other) => Err(IndexError::Config(format!(
            "LAYERX_INDEXER_COMET_ATTRIBUTES {other} is neither base64 nor plain"
        ))),
    }
}

/// Parses `0xaddress=denom,...` into pointer asset registrations.
///
/// # Errors
/// Refuses a malformed entry.
pub fn parse_pointers(text: &str) -> Result<Vec<AssetRow>, IndexError> {
    text.split(',')
        .map(str::trim)
        .filter(|entry| !entry.is_empty())
        .map(|entry| {
            let (address, denom) = entry.split_once('=').ok_or_else(|| {
                IndexError::Config(format!("pointer {entry} is not address=denom"))
            })?;
            let address = address_text(unhex_fixed::<20>(address.trim())?);
            let denom = denom.trim();
            if denom.is_empty() {
                return Err(IndexError::Config(format!("pointer {entry} has no denom")));
            }
            Ok(AssetRow {
                asset: format!("evm:{address}"),
                chain: crate::paxeer::CHAIN.to_owned(),
                kind: "pointer".to_owned(),
                address: Some(address),
                denom: Some(denom.to_owned()),
                metadata: json!({}),
            })
        })
        .collect()
}

impl Config {
    /// Reads the process environment.
    ///
    /// # Errors
    /// Refuses a missing database path, a malformed value, or an endpoint
    /// whose security policy cannot be satisfied.
    pub fn from_environment() -> Result<Self, IndexError> {
        Self::from_lookup(&|name: &str| std::env::var(name).ok())
    }

    /// Reads configuration through `lookup`.
    ///
    /// # Errors
    /// As [`Config::from_environment`].
    pub fn from_lookup<F>(lookup: &F) -> Result<Self, IndexError>
    where
        F: Fn(&str) -> Option<String>,
    {
        let database = lookup("LAYERX_INDEXER_DB")
            .map(PathBuf::from)
            .ok_or_else(|| IndexError::Config("LAYERX_INDEXER_DB is required".to_owned()))?;
        let listen = lookup("LAYERX_INDEXER_LISTEN")
            .unwrap_or_else(|| DEFAULT_LISTEN.to_owned())
            .parse()
            .map_err(|_| IndexError::Config("LAYERX_INDEXER_LISTEN is not host:port".to_owned()))?;
        let timeout = Duration::from_millis(number(
            lookup,
            "LAYERX_INDEXER_TIMEOUT_MS",
            DEFAULT_TIMEOUT_MS,
        )?);
        let allow_remote = lookup("LAYERX_INDEXER_ALLOW_REMOTE_PLAINTEXT").as_deref() == Some("1");
        let endpoint = |url_name: &str, ca_name: &str| -> Result<Option<Endpoint>, IndexError> {
            let Some(url) = lookup(url_name) else {
                return Ok(None);
            };
            let security =
                match lookup(ca_name) {
                    Some(path) => Security::PinnedTls(std::fs::read(&path).map_err(|error| {
                        IndexError::Config(format!("{ca_name} {path}: {error}"))
                    })?),
                    None => Security::Plaintext { allow_remote },
                };
            Endpoint::parse(&url, security, timeout).map(Some)
        };
        let window = number(
            lookup,
            "LAYERX_INDEXER_CHALLENGE_WINDOW_SECONDS",
            ANCHOR_DEFAULT_CHALLENGE_WINDOW_SECONDS,
        )?;
        let fixed_depth = optional_number(lookup, "LAYERX_INDEXER_FINALITY_DEPTH")?;
        let max_units_per_step = number(
            lookup,
            "LAYERX_INDEXER_MAX_UNITS_PER_STEP",
            DEFAULT_MAX_UNITS_PER_STEP,
        )?
        .max(1);
        let policy =
            |interval_name: &str, default_interval: u64| -> Result<FollowPolicy, IndexError> {
                let interval = number(lookup, interval_name, default_interval)?;
                Ok(FollowPolicy {
                    finality_depth: fixed_depth
                        .unwrap_or_else(|| finality_depth_from_challenge_window(window, interval)),
                    max_units_per_step,
                })
            };
        let settlement = SettlementTrust::from_lookup(lookup)?;
        let layerx = endpoint("LAYERX_INDEXER_RELAY_URL", "LAYERX_INDEXER_RELAY_CA_DER")?
            .map(|relay| -> Result<LayerXSource, IndexError> {
                Ok(LayerXSource {
                    relay,
                    start_batch: optional_number(lookup, "LAYERX_INDEXER_START_BATCH")?,
                    policy: policy(
                        "LAYERX_INDEXER_BATCH_INTERVAL_MS",
                        DEFAULT_BATCH_INTERVAL_MS,
                    )?,
                    settlement: settlement.clone(),
                })
            })
            .transpose()?;
        let encoding = attribute_encoding(lookup)?;
        let comet = endpoint("LAYERX_INDEXER_COMET_URL", "LAYERX_INDEXER_COMET_CA_DER")?;
        let paxeer = endpoint("LAYERX_INDEXER_EVM_URL", "LAYERX_INDEXER_EVM_CA_DER")?
            .map(|evm| -> Result<PaxeerSource, IndexError> {
                Ok(PaxeerSource {
                    evm,
                    comet: comet.clone(),
                    start_block: number(lookup, "LAYERX_INDEXER_START_BLOCK", 1)?,
                    chain_id: optional_number(lookup, "LAYERX_INDEXER_EVM_CHAIN_ID")?,
                    encoding,
                    policy: policy("LAYERX_INDEXER_BLOCK_TIME_MS", DEFAULT_BLOCK_TIME_MS)?,
                })
            })
            .transpose()?;
        if layerx.is_none() && paxeer.is_none() {
            return Err(IndexError::Config(
                "set LAYERX_INDEXER_RELAY_URL, LAYERX_INDEXER_EVM_URL or both".to_owned(),
            ));
        }
        Ok(Self {
            database,
            listen,
            tls: lookup("LAYERX_INDEXER_TLS_CERT_DER").is_some(),
            abi_dir: lookup("LAYERX_INDEXER_ABI_DIR").map(PathBuf::from),
            layerx,
            paxeer,
            pointers: parse_pointers(&lookup("LAYERX_INDEXER_POINTERS").unwrap_or_default())?,
            poll: Duration::from_millis(
                number(lookup, "LAYERX_INDEXER_POLL_MS", DEFAULT_POLL_MS)?.max(1),
            ),
        })
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::collections::BTreeMap;

    #[test]
    fn finality_depth_follows_the_challenge_window() {
        assert_eq!(finality_depth_from_challenge_window(0, 1_000), 1);
        assert_eq!(finality_depth_from_challenge_window(60, 400), 150);
        assert_eq!(finality_depth_from_challenge_window(61, 1_000), 61);
        assert_eq!(finality_depth_from_challenge_window(1, 3_000), 1);
    }

    #[test]
    fn environment_builds_both_sources() {
        let values: BTreeMap<&str, &str> = [
            ("LAYERX_INDEXER_DB", "/tmp/indexer.sqlite"),
            ("LAYERX_INDEXER_RELAY_URL", "http://127.0.0.1:7000"),
            ("LAYERX_INDEXER_EVM_URL", "http://127.0.0.1:8545"),
            ("LAYERX_INDEXER_COMET_URL", "http://127.0.0.1:26657"),
            ("LAYERX_INDEXER_CHALLENGE_WINDOW_SECONDS", "30"),
            ("LAYERX_INDEXER_BLOCK_TIME_MS", "500"),
            (
                "LAYERX_INDEXER_POINTERS",
                "0x00000000000000000000000000000000000000Aa=upax",
            ),
        ]
        .into_iter()
        .collect();
        let config =
            Config::from_lookup(&|name: &str| values.get(name).map(|value| (*value).to_owned()))
                .unwrap_or_else(|error| panic!("{error}"));
        let paxeer = config.paxeer.unwrap_or_else(|| panic!("paxeer source"));
        assert_eq!(paxeer.policy.finality_depth, 60);
        assert!(paxeer.comet.is_some());
        let layerx = config.layerx.unwrap_or_else(|| panic!("layerx source"));
        assert_eq!(layerx.policy.finality_depth, 30);
        assert_eq!(config.pointers.len(), 1);
        assert_eq!(
            config.pointers[0].address.as_deref(),
            Some("0x00000000000000000000000000000000000000aa")
        );
        let remote: BTreeMap<&str, &str> = [
            ("LAYERX_INDEXER_DB", "/tmp/indexer.sqlite"),
            ("LAYERX_INDEXER_EVM_URL", "http://paxeer.example:8545"),
        ]
        .into_iter()
        .collect();
        assert!(Config::from_lookup(&|name: &str| remote
            .get(name)
            .map(|value| (*value).to_owned()))
        .is_err());
    }
}
