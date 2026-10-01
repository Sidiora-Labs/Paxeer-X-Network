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
use std::io::Write as _;
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
        let directory = self.root.join(hex::encode(digest));
        fs::create_dir_all(&directory).map_err(|error| JournalError::Io(error.to_string()))?;
        let name = format!("{}.sig", hex::encode(signer));
        let target = directory.join(&name);
        let staging = directory.join(format!(".{name}.{}", std::process::id()));
        let mut file =
            fs::File::create(&staging).map_err(|error| JournalError::Io(error.to_string()))?;
        file.write_all(hex::prefixed(signature).as_bytes())
            .and_then(|()| file.sync_all())
            .map_err(|error| JournalError::Io(error.to_string()))?;
        fs::rename(&staging, &target).map_err(|error| JournalError::Io(error.to_string()))
    }

    /// Admits a peer's signature for `digest` without ever replacing
    /// existing bytes: staged, synced, hard-linked into place (which fails
    /// when the target exists) and the directory synced.
    ///
    /// # Errors
    ///
    /// Returns any i/o failure.
    pub fn admit(
        &self,
        digest: &[u8; 32],
        signer: &[u8; 20],
        signature: &[u8; 65],
    ) -> Result<Admission, JournalError> {
        let io = |error: std::io::Error| JournalError::Io(error.to_string());
        let directory = self.root.join(hex::encode(digest));
        fs::create_dir_all(&directory).map_err(io)?;
        let name = format!("{}.sig", hex::encode(signer));
        let target = directory.join(&name);
        let bytes = hex::prefixed(signature);
        if let Some(held) = read_bounded(&target) {
            return Ok(if held == bytes.as_bytes() {
                Admission::AlreadyHeld
            } else {
                Admission::Conflict
            });
        }
        let staging = directory.join(format!(".{name}.{}.admit", std::process::id()));
        let _ = fs::remove_file(&staging);
        let mut file = fs::OpenOptions::new()
            .write(true)
            .create_new(true)
            .open(&staging)
            .map_err(io)?;
        file.write_all(bytes.as_bytes())
            .and_then(|()| file.sync_all())
            .map_err(io)?;
        let linked = fs::hard_link(&staging, &target);
        let _ = fs::remove_file(&staging);
        match linked {
            Ok(()) => {
                sync_dir(&directory).map_err(io)?;
                if let Some(parent) = directory.parent() {
                    sync_dir(parent).map_err(io)?;
                }
                Ok(Admission::Stored)
            }
            Err(error) if error.kind() == std::io::ErrorKind::AlreadyExists => {
                Ok(match read_bounded(&target) {
                    Some(held) if held == bytes.as_bytes() => Admission::AlreadyHeld,
                    _ => Admission::Conflict,
                })
            }
            Err(error) => Err(io(error)),
        }
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
            if entry.metadata().map_or(true, |metadata| {
                !metadata.is_file() || metadata.len() > MAX_ENTRY_BYTES
            }) {
                continue;
            }
            let Ok(text) = fs::read_to_string(entry.path()) else {
                continue;
            };
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
    fs::read(path).ok()
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
            Ok(transport)
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
        let staging = directory.join(format!(".{name}.{}", std::process::id()));
        let mut file = fs::OpenOptions::new()
            .write(true)
            .create(true)
            .truncate(true)
            .mode(0o644)
            .open(&staging)?;
        file.write_all(bytes)?;
        file.sync_all()?;
        fs::rename(&staging, directory.join(name))?;
        sync_dir(directory)
    }
}
