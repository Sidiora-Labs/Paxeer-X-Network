//! Exchange of attestor signatures between relayer instances.
//!
//! Neither verifier aggregates partial signatures across transactions:
//! `bridgeIn` and `PaxeerXVault.release` each need `threshold` signatures,
//! ascending by signer, in one call. Each relayer instance holds one attestor
//! key, so with `threshold > 1` the instances publish their signature for a
//! digest into a shared directory (a shared volume or a synchronised bucket
//! mount) at `{digest}/{signer}.sig`, and every instance reads the others'.
//! Entries are untrusted: each is recovered against the digest and kept only
//! when it recovers to the signer its name claims and that signer is a current
//! attestor on the destination.

use std::fs;
use std::io::{Read as _, Write as _};
use std::os::unix::fs::{DirBuilderExt as _, OpenOptionsExt as _, PermissionsExt as _};
use std::path::{Path, PathBuf};

use crate::attestation::recover_signer;
use crate::hex;
use crate::journal::JournalError;

const MAX_ENTRIES: usize = 256;
const MAX_ENTRY_BYTES: u64 = 256;

pub struct CosignDirectory {
    root: PathBuf,
}

impl CosignDirectory {
    #[must_use]
    pub const fn new(root: PathBuf) -> Self {
        Self { root }
    }

    /// Publishes this instance's signature for `digest` atomically.
    ///
    /// # Errors
    ///
    /// Returns any i/o failure.
    pub fn publish(
        &self,
        digest: &[u8; 32],
        signer: &[u8; 20],
        signature: &[u8; 65],
    ) -> Result<(), JournalError> {
        match self.admit(digest, signer, signature)? {
            Admission::Stored | Admission::AlreadyHeld => Ok(()),
            Admission::Conflict => Err(JournalError::Conflict("cosign share already has different bytes".to_owned())),
        }
    }

    /// Validates and durably admits one share without replacing existing bytes.
    ///
    /// # Errors
    ///
    /// Refuses invalid signatures and filesystem errors before acknowledging delivery.
    pub fn admit(
        &self,
        digest: &[u8; 32],
        signer: &[u8; 20],
        signature: &[u8; 65],
    ) -> Result<Admission, JournalError> {
        if recover_signer(digest, signature) != Ok(*signer) {
            return Err(JournalError::Conflict("cosign signature does not bind digest and signer".to_owned()));
        }
        let io = |error: std::io::Error| JournalError::Io(error.to_string());
        private_directory(&self.root).map_err(io)?;
        let directory = self.root.join(hex::encode(digest));
        private_directory(&directory).map_err(io)?;
        let target = directory.join(format!("{}.sig", hex::encode(signer)));
        let bytes = hex::prefixed(signature);
        if fs::symlink_metadata(&target).is_ok() {
            let admission = if read_bounded(&target).as_deref() == Some(bytes.as_bytes()) {
                Admission::AlreadyHeld
            } else { Admission::Conflict };
            if admission == Admission::AlreadyHeld { fs::File::open(&target).and_then(|file| file.sync_all()).map_err(io)?; }
            sync_dir(&directory).and_then(|()| sync_dir(&self.root)).map_err(io)?;
            if let Some(parent) = self.root.parent().filter(|parent| !parent.as_os_str().is_empty()) { sync_dir(parent).map_err(io)?; }
            return Ok(admission);
        }
        let staging = staging_path(&directory, &hex::encode(signer)).map_err(io)?;
        let mut file = fs::OpenOptions::new().write(true).create_new(true).mode(0o600).open(&staging).map_err(io)?;
        let written = file.write_all(bytes.as_bytes()).and_then(|()| file.sync_all());
        if let Err(error) = written {
            let _ = fs::remove_file(&staging);
            return Err(io(error));
        }
        let linked = fs::hard_link(&staging, &target);
        fs::remove_file(&staging).map_err(io)?;
        let admission = match linked {
            Ok(()) => Admission::Stored,
            Err(error) if error.kind() == std::io::ErrorKind::AlreadyExists => {
                if read_bounded(&target).as_deref() == Some(bytes.as_bytes()) { Admission::AlreadyHeld }
                else { Admission::Conflict }
            }
            Err(error) => return Err(io(error)),
        };
        sync_dir(&directory).and_then(|()| sync_dir(&self.root)).map_err(io)?;
        if let Some(parent) = self.root.parent().filter(|parent| !parent.as_os_str().is_empty()) {
            sync_dir(parent).map_err(io)?;
        }
        Ok(admission)
    }

    /// This instance's own published share for every digest, ordered by
    /// digest.
    #[must_use]
    pub fn own_shares(&self, signer: &[u8; 20]) -> Vec<([u8; 32], [u8; 65])> {
        let Ok(entries) = fs::read_dir(&self.root) else {
            return Vec::new();
        };
        let name = format!("{}.sig", hex::encode(signer));
        let mut shares = Vec::new();
        for entry in entries.flatten() {
            let Some(digest) = entry
                .file_name()
                .to_str()
                .and_then(|stem| hex::fixed::<32>(&format!("0x{stem}")).ok())
            else {
                continue;
            };
            let Some(text) = read_bounded(&entry.path().join(&name)) else {
                continue;
            };
            let Ok(signature) = std::str::from_utf8(&text)
                .map_err(|_| ())
                .and_then(|text| hex::fixed::<65>(text.trim()).map_err(|_| ()))
            else {
                continue;
            };
            if recover_signer(&digest, &signature) == Ok(*signer) {
                shares.push((digest, signature));
            }
        }
        shares.sort_unstable_by_key(|share| share.0);
        shares
    }

    /// Every well-formed signature published for `digest` whose recovered
    /// signer matches its file name. Unreadable or malformed entries are
    /// skipped, never fatal.
    #[must_use]
    pub fn collect(&self, digest: &[u8; 32]) -> Vec<[u8; 65]> {
        let Ok(entries) = fs::read_dir(self.root.join(hex::encode(digest))) else {
            return Vec::new();
        };
        let mut signatures = Vec::new();
        for entry in entries.flatten().take(MAX_ENTRIES) {
            let name = entry.file_name();
            let Some(stem) = name.to_str().and_then(|name| name.strip_suffix(".sig")) else {
                continue;
            };
            let Ok(claimed) = hex::fixed::<20>(&format!("0x{stem}")) else {
                continue;
            };
            if stem != hex::encode(&claimed) { continue; }
            let Some(bytes) = read_bounded(&entry.path()) else { continue; };
            let Ok(text) = std::str::from_utf8(&bytes) else { continue; };
            let Ok(signature) = hex::fixed::<65>(text.trim()) else {
                continue;
            };
            if recover_signer(digest, &signature) == Ok(claimed) {
                signatures.push(signature);
            }
        }
        signatures
    }
}

/// Outcome of admitting a peer's share.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Admission {
    Stored,
    AlreadyHeld,
    Conflict,
}

fn read_bounded(path: &Path) -> Option<Vec<u8>> {
    let metadata = fs::symlink_metadata(path).ok()?;
    if !metadata.is_file() || metadata.len() > MAX_ENTRY_BYTES {
        return None;
    }
    let file = fs::File::open(path).ok()?;
    let mut bytes = Vec::new();
    file.take(MAX_ENTRY_BYTES + 1).read_to_end(&mut bytes).ok()?;
    (bytes.len() as u64 <= MAX_ENTRY_BYTES).then_some(bytes)
}

fn staging_path(directory: &Path, name: &str) -> std::io::Result<PathBuf> {
    let mut unique = [0; 16];
    openssl::rand::rand_bytes(&mut unique).map_err(|error| std::io::Error::other(error.to_string()))?;
    Ok(directory.join(format!(".{name}.{}.{}", std::process::id(), hex::encode(&unique))))
}

fn private_directory(path: &Path) -> std::io::Result<()> {
    fs::DirBuilder::new().recursive(true).mode(0o700).create(path)?;
    let metadata = fs::symlink_metadata(path)?;
    if !metadata.is_dir() || metadata.file_type().is_symlink() {
        return Err(std::io::Error::new(std::io::ErrorKind::InvalidInput, "cosign directory is not an owned directory"));
    }
    fs::set_permissions(path, fs::Permissions::from_mode(0o700))
}

fn sync_dir(path: &Path) -> std::io::Result<()> {
    fs::File::open(path)?.sync_all()
}

/// Authenticated delivery of cosign shares between relayer instances.
pub mod transport {
    use std::collections::BTreeSet;
    use std::fs;
    use std::io::{ErrorKind, Read, Write};
    use std::net::{SocketAddr, TcpListener, TcpStream};
    use std::os::unix::fs::{OpenOptionsExt as _, PermissionsExt as _};
    use std::path::{Path, PathBuf};
    use std::time::Duration;

    use openssl::ssl::{SslAcceptor, SslConnector, SslMethod, SslRef, SslVerifyMode, SslVersion};
    use openssl::x509::X509;
    use serde::Deserialize;
    use sha2::{Digest as _, Sha256};

    use super::{sync_dir, Admission, CosignDirectory};
    use crate::attestation::recover_signer;
    use crate::hex;

    pub const ENABLE_VARIABLE: &str = "LAYERX_BRIDGE_COSIGN_TRANSPORT_CONFIG";
    pub const FRAME_LENGTH: usize = 143;
    pub const MAGIC: [u8; 4] = *b"LXSH";
    pub const VERSION: u16 = 1;
    pub const MAX_SWEEP_ENTRIES: usize = 256;
    pub const MAX_PEERS: usize = 32;
    pub const MAX_CONNECTIONS: usize = 64;
    pub const MAX_TIMEOUT_MS: u64 = 60_000;
    pub const MAX_INTERVAL_MS: u64 = 600_000;
    const SURPLUS_PROBE: Duration = Duration::from_millis(50);

    pub const REPLY_STORED: u8 = 1;
    pub const REPLY_ALREADY_HELD: u8 = 2;
    pub const REPLY_CONFLICT: u8 = 3;

    #[derive(Debug, Deserialize)]
    #[serde(deny_unknown_fields)]
    pub struct PeerConfig {
        pub attestor: String,
        pub address: SocketAddr,
        pub server_name: String,
        pub spki_sha256: String,
    }

    #[derive(Debug, Deserialize)]
    #[serde(deny_unknown_fields)]
    pub struct TransportConfig {
        pub attestor: String,
        pub listen: SocketAddr,
        pub server_name: String,
        pub trust_anchor: PathBuf,
        pub certificate: PathBuf,
        pub private_key: PathBuf,
        pub cosign_directory: PathBuf,
        pub delivery_directory: PathBuf,
        pub peers: Vec<PeerConfig>,
        pub timeout_ms: u64,
        pub sweep_interval_ms: u64,
        pub max_connections: usize,
    }

    pub struct Peer {
        pub attestor: [u8; 20],
        pub address: SocketAddr,
        pub server_name: String,
        pub pin: [u8; 32],
    }

    pub struct Transport {
        pub attestor: [u8; 20],
        pub listen: SocketAddr,
        pub server_name: String,
        pub trust_anchor: PathBuf,
        pub certificate: PathBuf,
        pub private_key: PathBuf,
        pub cosign: CosignDirectory,
        pub cosign_root: PathBuf,
        pub delivery: PathBuf,
        pub peers: Vec<Peer>,
        pub timeout: Duration,
        pub interval: Duration,
        pub max_connections: usize,
    }

    #[derive(Debug)]
    pub struct ConfigError(pub String);

    impl std::fmt::Display for ConfigError {
        fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
            f.write_str(&self.0)
        }
    }

    fn bad(message: impl Into<String>) -> ConfigError {
        ConfigError(message.into())
    }

    /// SHA-256 over the DER `SubjectPublicKeyInfo` of a certificate.
    ///
    /// # Errors
    ///
    /// Refuses a certificate whose key cannot be encoded.
    pub fn spki_pin(certificate: &X509) -> Result<[u8; 32], openssl::error::ErrorStack> {
        let der = certificate.public_key()?.public_key_to_der()?;
        Ok(Sha256::digest(der).into())
    }

    pub const INVENTORY_VARIABLE: &str = "LAYERX_BRIDGE_OPERATOR_INVENTORY";
    pub const RELAYER_CONFIG_VARIABLE: &str = "LAYERX_BRIDGE_RELAYER_CONFIG";

    #[derive(Debug, Deserialize)]
    #[serde(deny_unknown_fields)]
    pub struct OperatorInventory {
        pub version: u16,
        pub authority: String,
        pub approval: String,
        pub membership: PathBuf,
        pub operators: Vec<OperatorRecord>,
        pub destinations: Vec<DestinationPolicy>,
    }

    #[derive(Debug, Deserialize)]
    #[serde(deny_unknown_fields)]
    pub struct OperatorRecord {
        pub instance: String,
        pub storage: String,
        pub attestor: String,
        pub signer_handle: String,
        pub signer_public_key: String,
        pub journal_path: PathBuf,
        pub cosign_directory: PathBuf,
        pub delivery_directory: PathBuf,
        pub spki_sha256: String,
        pub fee_payers: Vec<OperatorFeePayer>,
    }

    #[derive(Debug, Deserialize)]
    #[serde(deny_unknown_fields)]
    pub struct OperatorFeePayer {
        pub chain_id: u64,
        pub handle: String,
        pub public_key: String,
    }

    #[derive(Debug, Deserialize)]
    #[serde(deny_unknown_fields)]
    pub struct DestinationPolicy {
        pub chain_id: u64,
        pub vault: String,
        pub attestors: Vec<String>,
        pub threshold: usize,
        pub observed_block_hash: String,
    }

    #[derive(Debug, Deserialize)]
    #[serde(deny_unknown_fields)]
    struct Membership { attestors: Vec<String>, threshold: usize }

    fn public_key(text: &str) -> Result<Vec<u8>, ConfigError> {
        let bytes = hex::decode(text).map_err(|_| bad("operator public key"))?;
        if bytes.len() == 32 { if bytes.iter().all(|byte| *byte == 0) { return Err(bad("zero operator public key")); } return Ok(bytes); }
        let key = k256::ecdsa::VerifyingKey::from_sec1_bytes(&bytes).map_err(|_| bad("operator public key"))?;
        Ok(key.to_encoded_point(true).as_bytes().to_vec())
    }

    fn membership(values: &[String]) -> Result<BTreeSet<[u8; 20]>, ConfigError> {
        let mut members = BTreeSet::new();
        let mut previous = None;
        for value in values {
            let address = hex::fixed::<20>(value).map_err(|_| bad("approved bridge attestor"))?;
            if address == [0; 20] || address.iter().all(|byte| *byte == address[0]) || previous.is_some_and(|prior| prior >= address) || !members.insert(address) {
                return Err(bad("approved bridge attestors must be ascending distinct non-placeholder addresses"));
            }
            previous = Some(address);
        }
        if !(2..=MAX_PEERS + 1).contains(&members.len()) { return Err(bad("approved bridge membership cardinality")); }
        Ok(members)
    }

    fn read_metadata<T: serde::de::DeserializeOwned>(path: &Path) -> Result<T, ConfigError> {
        if !path.is_absolute() { return Err(bad("operator metadata path must be absolute")); }
        let metadata = fs::symlink_metadata(path).map_err(|error| bad(format!("operator metadata: {error}")))?;
        if !metadata.is_file() || metadata.len() > 1_048_576 { return Err(bad("operator metadata must be a bounded regular file")); }
        let bytes = fs::read(path).map_err(|error| bad(format!("operator metadata: {error}")))?;
        if bytes.len() > 1_048_576 { return Err(bad("operator metadata exceeds bound")); }
        serde_json::from_slice(&bytes).map_err(|error| bad(format!("operator metadata: {error}")))
    }

    impl OperatorInventory {
        pub fn load(path: &Path) -> Result<Self, ConfigError> {
            let inventory: Self = read_metadata(path)?;
            inventory.validate()?;
            Ok(inventory)
        }

        pub fn validate(&self) -> Result<(), ConfigError> {
            if self.version != 1 || self.authority != "bridge" || self.approval.trim().is_empty() {
                return Err(bad("explicit bridge operator approval metadata is required"));
            }
            let approved: Membership = read_metadata(&self.membership)?;
            let members = membership(&approved.attestors)?;
            if approved.threshold < 2 || approved.threshold > members.len() || self.operators.len() != members.len() {
                return Err(bad("approved membership and operator cardinality or threshold disagree"));
            }
            let mut operators = BTreeSet::new(); let mut instances = BTreeSet::new(); let mut storage = BTreeSet::new();
            let mut pins = BTreeSet::new(); let mut attestor_keys = BTreeSet::new();
            let mut payer_keys = BTreeSet::new();
            for operator in &self.operators {
                let address = hex::fixed::<20>(&operator.attestor).map_err(|_| bad("operator attestor"))?;
                let public = public_key(&operator.signer_public_key)?;
                let key = k256::ecdsa::VerifyingKey::from_sec1_bytes(&public).map_err(|_| bad("bridge attestor must be secp256k1"))?;
                let pin = hex::fixed::<32>(&operator.spki_sha256).map_err(|_| bad("operator transport pin"))?;
                if !members.contains(&address) || crate::attestation::ethereum_address(&key) != address
                    || !operators.insert(address) || !attestor_keys.insert(public) || operator.signer_handle.is_empty()
                    || operator.instance.trim().is_empty() || !instances.insert(&operator.instance)
                    || operator.storage.trim().is_empty() || !storage.insert(&operator.storage)
                    || pin == [0; 32] || !pins.insert(pin)
                { return Err(bad("operator identities, signer bindings, storage and pins must be distinct and approved")); }
                let paths = [&operator.journal_path, &operator.cosign_directory, &operator.delivery_directory];
                if paths.iter().any(|path| !path.is_absolute() || path.components().any(|part| matches!(part, std::path::Component::ParentDir)))
                    || operator.journal_path.starts_with(&operator.cosign_directory)
                    || operator.journal_path.starts_with(&operator.delivery_directory)
                    || operator.cosign_directory.starts_with(&operator.delivery_directory)
                    || operator.delivery_directory.starts_with(&operator.cosign_directory)
                { return Err(bad("operator journal and share storage must be separate absolute paths")); }
                if operator.fee_payers.is_empty() { return Err(bad("operator independent fee payer handles are required")); }
                let mut chains = BTreeSet::new(); let mut handles = BTreeSet::new();
                for payer in &operator.fee_payers {
                    let public = public_key(&payer.public_key)?;
                    if payer.chain_id == 0 || payer.handle.is_empty() || payer.handle == operator.signer_handle
                        || !chains.insert(payer.chain_id) || !handles.insert(&payer.handle) || !payer_keys.insert(public)
                    { return Err(bad("fee payer chain handles and public keys must be independent")); }
                }
            }
            if !attestor_keys.is_disjoint(&payer_keys) { return Err(bad("bridge attestor keys must not pay fees")); }
            if self.destinations.is_empty() || self.destinations.len() > 64 { return Err(bad("registered destination inventory required")); }
            let mut chains = BTreeSet::new();
            for policy in &self.destinations {
                if policy.chain_id == 0 || !chains.insert(policy.chain_id) || membership(&policy.attestors)? != members
                    || policy.threshold != approved.threshold || hex::fixed::<20>(&policy.vault).map_err(|_| bad("destination vault"))? == [0; 20]
                    || hex::fixed::<32>(&policy.observed_block_hash).map_err(|_| bad("destination policy observation"))? == [0; 32]
                { return Err(bad("registered destination policy differs from approved bridge membership")); }
            }
            for operator in &self.operators {
                if operator.fee_payers.iter().map(|payer| payer.chain_id).collect::<BTreeSet<_>>() != chains {
                    return Err(bad("each operator needs its own fee payer for every registered destination"));
                }
            }
            Ok(())
        }
    }

    impl Transport {
        /// Loads and validates the transport configuration.
        ///
        /// # Errors
        ///
        /// Refuses a missing, unreadable or invalid file, unbounded limits,
        /// duplicate or self peers, duplicate pins and private keys readable
        /// by others or placed inside a shared directory.
        pub fn load(path: &Path) -> Result<Self, ConfigError> {
            let text = fs::read_to_string(path)
                .map_err(|error| bad(format!("transport config {}: {error}", path.display())))?;
            let config: TransportConfig = serde_json::from_str(&text)
                .map_err(|error| bad(format!("transport config {}: {error}", path.display())))?;
            let attestor = hex::fixed::<20>(&config.attestor).map_err(|_| bad("attestor"))?;
            if config.peers.is_empty() || config.peers.len() > MAX_PEERS {
                return Err(bad("peers must number 1..=32"));
            }
            if !(1..=MAX_TIMEOUT_MS).contains(&config.timeout_ms) {
                return Err(bad("timeout_ms must be 1..=60000"));
            }
            if !(1..=MAX_INTERVAL_MS).contains(&config.sweep_interval_ms) {
                return Err(bad("sweep_interval_ms must be 1..=600000"));
            }
            if !(1..=MAX_CONNECTIONS).contains(&config.max_connections) {
                return Err(bad("max_connections must be 1..=64"));
            }
            if config.server_name.is_empty() {
                return Err(bad("server_name"));
            }
            let mut attestors = BTreeSet::from([attestor]);
            let mut pins = BTreeSet::new();
            let mut peers = Vec::new();
            for peer in config.peers {
                let peer_attestor =
                    hex::fixed::<20>(&peer.attestor).map_err(|_| bad("peer attestor"))?;
                let pin = hex::fixed::<32>(&peer.spki_sha256).map_err(|_| bad("peer pin"))?;
                if !attestors.insert(peer_attestor) || !pins.insert(pin) {
                    return Err(bad("peer attestors and pins must be unique"));
                }
                if peer.server_name.is_empty() || peer.server_name.as_bytes().contains(&0) {
                    return Err(bad("peer server_name"));
                }
                peers.push(Peer {
                    attestor: peer_attestor,
                    address: peer.address,
                    server_name: peer.server_name,
                    pin,
                });
            }
            let cosign_root = fs::canonicalize(&config.cosign_directory)
                .map_err(|error| bad(format!("cosign_directory: {error}")))?;
            let delivery = fs::canonicalize(&config.delivery_directory)
                .map_err(|error| bad(format!("delivery_directory: {error}")))?;
            for secret in [&config.private_key] {
                let resolved = fs::canonicalize(secret)
                    .map_err(|error| bad(format!("private_key: {error}")))?;
                let metadata = fs::metadata(&resolved)
                    .map_err(|error| bad(format!("private_key: {error}")))?;
                if !metadata.is_file() || metadata.permissions().mode() & 0o077 != 0 {
                    return Err(bad(
                        "private_key must be a file with no group or other access",
                    ));
                }
                if resolved.starts_with(&cosign_root) || resolved.starts_with(&delivery) {
                    return Err(bad("private_key must stay outside the shared directories"));
                }
            }
            let transport = Self {
                attestor,
                listen: config.listen,
                server_name: config.server_name,
                trust_anchor: config.trust_anchor,
                certificate: config.certificate,
                private_key: config.private_key,
                cosign: CosignDirectory::new(cosign_root.clone()),
                cosign_root,
                delivery,
                peers,
                timeout: Duration::from_millis(config.timeout_ms),
                interval: Duration::from_millis(config.sweep_interval_ms),
                max_connections: config.max_connections,
            };
            transport
                .acceptor()
                .map_err(|error| bad(format!("tls: {error}")))?;
            layerx_mirror::signer::tls_connector(
                &transport.server_name,
                &transport.trust_anchor,
                &transport.certificate,
                &transport.private_key,
            )
            .map_err(|error| bad(format!("tls connector: {error:?}")))?;
            if let Some(inventory_path) = std::env::var_os(INVENTORY_VARIABLE) {
                let relayer_path = std::env::var_os(RELAYER_CONFIG_VARIABLE).ok_or_else(|| bad("operator inventory requires explicit relayer configuration"))?;
                let inventory = OperatorInventory::load(Path::new(&inventory_path))?;
                let relayer = crate::config::RelayerConfig::load(Path::new(&relayer_path)).map_err(|error| bad(error.to_string()))?;
                transport.validate_operator(&inventory, &relayer)?;
            }
            Ok(transport)
        }

        pub fn validate_operator(&self, inventory: &OperatorInventory, relayer: &crate::config::RelayerConfig) -> Result<(), ConfigError> {
            inventory.validate()?;
            let local = inventory.operators.iter().find(|operator| hex::fixed::<20>(&operator.attestor).ok() == Some(self.attestor))
                .ok_or_else(|| bad("local attestor is not an approved operator"))?;
            let certificate = X509::from_pem(&fs::read(&self.certificate).map_err(|error| bad(error.to_string()))?).map_err(|error| bad(error.to_string()))?;
            if spki_pin(&certificate).map_err(|error| bad(error.to_string()))? != hex::fixed::<32>(&local.spki_sha256).map_err(|_| bad("local pin"))?
                || local.signer_handle != relayer.attestor.handle || public_key(&local.signer_public_key)? != public_key(&hex::prefixed(&relayer.attestor.public_key))?
                || local.journal_path != relayer.journal_path || local.cosign_directory != self.cosign_root || local.delivery_directory != self.delivery
                || relayer.cosign_directory.as_ref() != Some(&self.cosign_root)
            { return Err(bad("local relayer signer, journal or transport differs from approved inventory")); }
            let peers = self.peers.iter().map(|peer| (peer.attestor, peer.pin)).collect::<BTreeSet<_>>();
            let expected = inventory.operators.iter().filter(|operator| operator.attestor != local.attestor).map(|operator| {
                Ok((hex::fixed::<20>(&operator.attestor).map_err(|_| bad("operator attestor"))?, hex::fixed::<32>(&operator.spki_sha256).map_err(|_| bad("operator pin"))?))
            }).collect::<Result<BTreeSet<_>, ConfigError>>()?;
            if peers != expected { return Err(bad("transport peers do not equal the approved bridge roster")); }
            let mut configured = vec![(relayer.paxeer.chain_id, &relayer.paxeer.submitter)];
            configured.extend(relayer.chains.iter().map(|chain| (chain.chain_id, &chain.submitter)));
            if let Some(solana) = &relayer.solana { configured.push((solana.chain_id, &solana.fee_payer)); }
            if configured.len() != local.fee_payers.len() { return Err(bad("relayer destination and fee payer inventory disagree")); }
            for (chain_id, key) in configured {
                let approved = local.fee_payers.iter().find(|payer| payer.chain_id == chain_id).ok_or_else(|| bad("missing approved destination fee payer"))?;
                if approved.handle != key.handle || public_key(&approved.public_key)? != public_key(&hex::prefixed(&key.public_key))? {
                    return Err(bad("relayer fee payer differs from approved inventory"));
                }
            }
            if !inventory.destinations.iter().any(|policy| policy.chain_id == relayer.paxeer.chain_id && hex::fixed::<20>(&policy.vault).ok() == Some(crate::abi::LAYERX_BRIDGE_PRECOMPILE)) {
                return Err(bad("registered Paxeer bridge inventory differs from relayer configuration"));
            }
            for chain in &relayer.chains {
                if !inventory.destinations.iter().any(|policy| policy.chain_id == chain.chain_id && hex::fixed::<20>(&policy.vault).ok() == Some(chain.vault)) {
                    return Err(bad("registered EVM vault inventory differs from relayer configuration"));
                }
            }
            if let Some(solana) = &relayer.solana {
                if !inventory.destinations.iter().any(|policy| policy.chain_id == solana.chain_id && hex::fixed::<20>(&policy.vault).ok() == Some(solana.vault)) {
                    return Err(bad("registered Solana vault inventory differs from relayer configuration"));
                }
            }
            Ok(())
        }

        fn acceptor(&self) -> Result<SslAcceptor, openssl::error::ErrorStack> {
            let mut builder = SslAcceptor::mozilla_modern_v5(SslMethod::tls_server())?;
            builder.set_min_proto_version(Some(SslVersion::TLS1_3))?;
            builder.set_ca_file(&self.trust_anchor)?;
            builder.set_certificate_chain_file(&self.certificate)?;
            builder.set_private_key_file(&self.private_key, openssl::ssl::SslFiletype::PEM)?;
            builder.check_private_key()?;
            builder.set_verify(SslVerifyMode::PEER | SslVerifyMode::FAIL_IF_NO_PEER_CERT);
            Ok(builder.build())
        }

        /// Serves peers until the listener fails.
        ///
        /// # Errors
        ///
        /// Returns a bind or TLS setup failure.
        pub fn serve(self: &std::sync::Arc<Self>) -> Result<(), String> {
            let acceptor = std::sync::Arc::new(self.acceptor().map_err(|error| error.to_string())?);
            let listener = TcpListener::bind(self.listen).map_err(|error| error.to_string())?;
            let active = std::sync::Arc::new(std::sync::atomic::AtomicUsize::new(0));
            for stream in listener.incoming() {
                let Ok(stream) = stream else { continue };
                if active.fetch_add(1, std::sync::atomic::Ordering::SeqCst) >= self.max_connections
                {
                    active.fetch_sub(1, std::sync::atomic::Ordering::SeqCst);
                    continue;
                }
                let this = std::sync::Arc::clone(self);
                let acceptor = std::sync::Arc::clone(&acceptor);
                let active = std::sync::Arc::clone(&active);
                std::thread::spawn(move || {
                    if let Err(reason) = this.handle(&acceptor, stream) {
                        eprintln!("layerx-bridge-cosign: refused inbound share: {reason}");
                    }
                    active.fetch_sub(1, std::sync::atomic::Ordering::SeqCst);
                });
            }
            Ok(())
        }

        fn handle(&self, acceptor: &SslAcceptor, stream: TcpStream) -> Result<(), String> {
            stream
                .set_read_timeout(Some(self.timeout))
                .and_then(|()| stream.set_write_timeout(Some(self.timeout)))
                .map_err(|error| error.to_string())?;
            let mut tls = acceptor
                .accept(stream)
                .map_err(|error| format!("handshake: {error}"))?;
            let peer = self
                .peer_by_pin(tls.ssl())
                .ok_or_else(|| "unpinned client".to_owned())?;
            let mut prefix = [0_u8; 4];
            tls.read_exact(&mut prefix)
                .map_err(|error| format!("prefix: {error}"))?;
            if u32::from_be_bytes(prefix) as usize != FRAME_LENGTH {
                return Err("frame length".to_owned());
            }
            let mut frame = [0_u8; FRAME_LENGTH];
            tls.read_exact(&mut frame)
                .map_err(|error| format!("frame: {error}"))?;
            if tls.ssl().pending() > 0 {
                return Err("surplus frame data".to_owned());
            }
            tls.get_ref()
                .set_read_timeout(Some(SURPLUS_PROBE))
                .map_err(|error| error.to_string())?;
            let mut probe = [0_u8; 1];
            match tls.read(&mut probe) {
                Ok(0) => {}
                Ok(_) => return Err("surplus frame data".to_owned()),
                Err(error)
                    if matches!(error.kind(), ErrorKind::WouldBlock | ErrorKind::TimedOut) => {}
                Err(_) => {}
            }
            let share = decode_frame(&frame)?;
            if share.recipient != self.attestor {
                return Err("wrong recipient".to_owned());
            }
            if share.signer != peer.attestor {
                return Err("signer is not the authenticated peer".to_owned());
            }
            if recover_signer(&share.digest, &share.signature) != Ok(share.signer) {
                return Err("signature does not recover to signer".to_owned());
            }
            let admission = self
                .cosign
                .admit(&share.digest, &share.signer, &share.signature)
                .map_err(|error| error.to_string())?;
            let reply = match admission {
                Admission::Stored => REPLY_STORED,
                Admission::AlreadyHeld => REPLY_ALREADY_HELD,
                Admission::Conflict => REPLY_CONFLICT,
            };
            tls.get_ref()
                .set_read_timeout(Some(self.timeout))
                .map_err(|error| error.to_string())?;
            tls.write_all(&[reply])
                .and_then(|()| tls.flush())
                .map_err(|error| error.to_string())?;
            let _ = tls.shutdown();
            Ok(())
        }

        fn peer_by_pin(&self, ssl: &SslRef) -> Option<&Peer> {
            let pin = spki_pin(&ssl.peer_certificate()?).ok()?;
            self.peers.iter().find(|peer| peer.pin == pin)
        }

        /// One bounded sweep: up to 256 own shares after the persisted
        /// cursor, wrapping, each offered to every peer lacking an
        /// acknowledgement or conflict record for its current pin.
        pub fn sweep(&self) {
            let shares = self.cosign.own_shares(&self.attestor);
            if shares.is_empty() {
                return;
            }
            let cursor_path = self.delivery.join("cursor");
            let cursor = fs::read_to_string(&cursor_path)
                .ok()
                .and_then(|text| hex::fixed::<32>(text.trim()).ok());
            let start = cursor.map_or(0, |cursor| {
                shares.partition_point(|share| share.0 <= cursor) % shares.len()
            });
            let count = shares.len().min(MAX_SWEEP_ENTRIES);
            let page: Vec<_> = (0..count)
                .map(|index| shares[(start + index) % shares.len()])
                .collect();
            let last = page.last().map(|share| share.0);
            std::thread::scope(|scope| {
                for peer in &self.peers {
                    let page = &page;
                    scope.spawn(move || {
                        let connector = match layerx_mirror::signer::tls_connector(
                            &peer.server_name,
                            &self.trust_anchor,
                            &self.certificate,
                            &self.private_key,
                        ) {
                            Ok(connector) => connector,
                            Err(error) => {
                                eprintln!("layerx-bridge-cosign: tls connector: {error:?}");
                                return;
                            }
                        };
                        for (digest, signature) in page {
                            self.deliver(&connector, peer, digest, signature);
                        }
                    });
                }
            });
            if let Some(last) = last.filter(|last| Some(*last) != cursor) {
                let _ = write_atomic(&self.delivery, "cursor", hex::prefixed(&last).as_bytes());
            }
        }

        /// The record name binding peer attestor, peer pin, digest and own
        /// share identity.
        #[must_use]
        pub fn record_stem(&self, peer: &Peer, digest: &[u8; 32], signature: &[u8; 65]) -> String {
            let mut identity = Sha256::new();
            identity.update(self.attestor);
            identity.update(signature);
            format!(
                "{}-{}-{}-{}",
                hex::encode(&peer.attestor),
                hex::encode(&peer.pin),
                hex::encode(digest),
                hex::encode(&identity.finalize())
            )
        }

        fn deliver(
            &self,
            connector: &SslConnector,
            peer: &Peer,
            digest: &[u8; 32],
            signature: &[u8; 65],
        ) {
            let stem = self.record_stem(peer, digest, signature);
            let ack = format!("{stem}.ack");
            let conflict = format!("{stem}.conflict");
            if self.delivery.join(&ack).exists() || self.delivery.join(&conflict).exists() {
                return;
            }
            match self.send(connector, peer, digest, signature) {
                Ok(REPLY_STORED | REPLY_ALREADY_HELD) => {
                    if let Err(error) = write_atomic(&self.delivery, &ack, b"acknowledged\n") {
                        eprintln!("layerx-bridge-cosign: ack record {ack}: {error}");
                    }
                }
                Ok(REPLY_CONFLICT) => {
                    eprintln!(
                        "layerx-bridge-cosign: conflict at peer {}",
                        hex::prefixed(&peer.attestor)
                    );
                    if let Err(error) = write_atomic(&self.delivery, &conflict, b"conflict\n") {
                        eprintln!("layerx-bridge-cosign: conflict record {conflict}: {error}");
                    }
                }
                Ok(other) => eprintln!("layerx-bridge-cosign: unknown reply {other}"),
                Err(error) => eprintln!(
                    "layerx-bridge-cosign: delivery to {} pending: {error}",
                    hex::prefixed(&peer.attestor)
                ),
            }
        }

        fn send(
            &self,
            connector: &SslConnector,
            peer: &Peer,
            digest: &[u8; 32],
            signature: &[u8; 65],
        ) -> Result<u8, String> {
            let stream = TcpStream::connect_timeout(&peer.address, self.timeout)
                .map_err(|error| error.to_string())?;
            stream
                .set_read_timeout(Some(self.timeout))
                .and_then(|()| stream.set_write_timeout(Some(self.timeout)))
                .map_err(|error| error.to_string())?;
            let mut tls = connector
                .connect(&peer.server_name, stream)
                .map_err(|error| format!("handshake: {error}"))?;
            let pin = tls
                .ssl()
                .peer_certificate()
                .and_then(|certificate| spki_pin(&certificate).ok())
                .ok_or_else(|| "no server certificate".to_owned())?;
            if pin != peer.pin {
                return Err("server pin mismatch".to_owned());
            }
            let wire = encode_wire(&peer.attestor, digest, &self.attestor, signature);
            tls.write_all(&wire)
                .and_then(|()| tls.flush())
                .map_err(|error| error.to_string())?;
            let mut reply = [0_u8; 1];
            tls.read_exact(&mut reply)
                .map_err(|error| error.to_string())?;
            Ok(reply[0])
        }
    }

    pub struct Share {
        pub recipient: [u8; 20],
        pub digest: [u8; 32],
        pub signer: [u8; 20],
        pub signature: [u8; 65],
    }

    /// The 147 wire bytes: big-endian length 143 then the frame.
    #[must_use]
    pub fn encode_wire(
        recipient: &[u8; 20],
        digest: &[u8; 32],
        signer: &[u8; 20],
        signature: &[u8; 65],
    ) -> Vec<u8> {
        let mut wire = Vec::with_capacity(4 + FRAME_LENGTH);
        wire.extend_from_slice(&u32::try_from(FRAME_LENGTH).unwrap_or(0).to_be_bytes());
        wire.extend_from_slice(&MAGIC);
        wire.extend_from_slice(&VERSION.to_be_bytes());
        wire.extend_from_slice(recipient);
        wire.extend_from_slice(digest);
        wire.extend_from_slice(signer);
        wire.extend_from_slice(signature);
        wire
    }

    /// Parses a 143-byte frame.
    ///
    /// # Errors
    ///
    /// Refuses a wrong magic or version.
    pub fn decode_frame(frame: &[u8; FRAME_LENGTH]) -> Result<Share, String> {
        if frame[..4] != MAGIC {
            return Err("bad magic".to_owned());
        }
        if u16::from_be_bytes([frame[4], frame[5]]) != VERSION {
            return Err("bad version".to_owned());
        }
        let mut share = Share {
            recipient: [0; 20],
            digest: [0; 32],
            signer: [0; 20],
            signature: [0; 65],
        };
        share.recipient.copy_from_slice(&frame[6..26]);
        share.digest.copy_from_slice(&frame[26..58]);
        share.signer.copy_from_slice(&frame[58..78]);
        share.signature.copy_from_slice(&frame[78..143]);
        Ok(share)
    }

    fn write_atomic(directory: &Path, name: &str, bytes: &[u8]) -> std::io::Result<()> {
        let staging = super::staging_path(directory, name)?;
        let mut file = fs::OpenOptions::new()
            .write(true)
            .create_new(true)
            .mode(0o600)
            .open(&staging)?;
        file.write_all(bytes)?;
        file.sync_all()?;
        fs::rename(&staging, directory.join(name))?;
        sync_dir(directory)
    }
}
