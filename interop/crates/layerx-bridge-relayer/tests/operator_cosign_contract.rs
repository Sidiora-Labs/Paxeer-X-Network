//! Operator cosign contract: the approved bridge attestor threshold is reached
//! through distinct operators, each signing through its own real
//! `layerx-mirror-signer bridge` process and publishing into the shared
//! cosign directory. Shares are untrusted: wrong-digest, unknown, duplicate
//! and malformed entries never count, and below the threshold assembly keeps
//! waiting. Killing and restarting one operator's signer keeps every share
//! already published, and republishing never adds a second share.

mod support;

use std::fs;
use std::os::unix::fs::PermissionsExt as _;
use std::path::{Path, PathBuf};
use std::process::{Child, Command, Stdio};
use std::time::{Duration, Instant};

use k256::ecdsa::SigningKey;
use layerx_bridge_relayer::attestation::{
    assemble_signatures, ethereum_address, recover_signer, uint256_from_u64, InboundAttestation,
    SignatureError,
};
use layerx_bridge_relayer::cosign::CosignDirectory;
use layerx_bridge_relayer::hex;
use layerx_bridge_relayer::signer::{Attestor, ATTEST_INBOUND_DOMAIN};
use layerx_mirror::signer::{
    RemoteChainSigner, RemoteSignerConfig, SignerEndpoint, SigningAlgorithm,
};

const HANDLE: &str = "bridge-attestor";

/// The real bridge signer binary. A missing binary is a refused fixture,
/// never a skipped or simulated signer.
fn signer_binary() -> PathBuf {
    if let Some(path) = std::env::var_os("LAYERX_MIRROR_SIGNER_BIN") {
        return PathBuf::from(path);
    }
    let target = std::env::var_os("CARGO_TARGET_DIR").map_or_else(
        || Path::new(env!("CARGO_MANIFEST_DIR")).join("../../target"),
        PathBuf::from,
    );
    let binary = target.join("debug/layerx-mirror-signer");
    assert!(
        binary.is_file(),
        "refused: the real bridge signer binary {} is missing; build it with \
         `cargo build --locked --manifest-path interop/Cargo.toml -p layerx-mirror-signer` \
         or name it in LAYERX_MIRROR_SIGNER_BIN",
        binary.display()
    );
    binary
}

fn approved_threshold() -> usize {
    let path = Path::new(env!("CARGO_MANIFEST_DIR")).join("../../../bridge/deploy/attestors.json");
    let text = fs::read_to_string(&path)
        .unwrap_or_else(|error| panic!("approved membership {}: {error}", path.display()));
    let document: serde_json::Value =
        serde_json::from_str(&text).unwrap_or_else(|error| panic!("attestors.json: {error}"));
    let threshold = document["threshold"]
        .as_u64()
        .and_then(|value| usize::try_from(value).ok())
        .unwrap_or_else(|| panic!("attestors.json has no threshold"));
    let members = document["attestors"].as_array().map_or(0, Vec::len);
    assert!(
        threshold >= 2 && threshold <= members,
        "approved threshold {threshold} of {members}"
    );
    threshold
}

/// One operator: its own key file, key manifest, signer process and journal
/// directory. Only the cosign directory is shared.
struct Operator {
    key: SigningKey,
    home: PathBuf,
    socket: PathBuf,
    child: Option<Child>,
}

impl Operator {
    fn new(root: &Path, index: u8) -> Self {
        let home = root.join(format!("operator-{index}"));
        fs::create_dir_all(home.join("journal")).unwrap_or_else(|error| panic!("home: {error}"));
        let key = support::key(0x40 + index);
        let key_file = home.join("attestor.key");
        fs::write(&key_file, hex::encode(&support::secret(0x40 + index)))
            .unwrap_or_else(|error| panic!("key file: {error}"));
        fs::set_permissions(&key_file, fs::Permissions::from_mode(0o400))
            .unwrap_or_else(|error| panic!("key mode: {error}"));
        let manifest = serde_json::json!({"keys": [{
            "handle": HANDLE,
            "algorithm": "secp256k1",
            "key_file": key_file.to_str().unwrap_or_else(|| panic!("key path")),
            "domains": [std::str::from_utf8(ATTEST_INBOUND_DOMAIN).unwrap_or_else(|_| panic!("domain"))],
        }]});
        fs::write(home.join("keys.json"), manifest.to_string())
            .unwrap_or_else(|error| panic!("manifest: {error}"));
        let socket = home.join("signer.sock");
        let mut operator = Self {
            key,
            home,
            socket,
            child: None,
        };
        operator.start();
        operator
    }

    fn start(&mut self) {
        let _ = fs::remove_file(&self.socket);
        let child = Command::new(signer_binary())
            .arg("bridge")
            .arg("--socket")
            .arg(&self.socket)
            .arg("--keys")
            .arg(self.home.join("keys.json"))
            .stdin(Stdio::null())
            .spawn()
            .unwrap_or_else(|error| panic!("spawn bridge signer: {error}"));
        self.child = Some(child);
        let started = Instant::now();
        while !self.socket.exists() {
            assert!(
                started.elapsed() < Duration::from_secs(10),
                "bridge signer did not bind"
            );
            std::thread::sleep(Duration::from_millis(20));
        }
    }

    fn kill(&mut self) {
        if let Some(mut child) = self.child.take() {
            let _ = child.kill();
            let _ = child.wait();
        }
    }

    fn attestor(&self) -> Attestor {
        let remote = RemoteChainSigner::new(RemoteSignerConfig {
            endpoint: SignerEndpoint::Uds {
                socket: self.socket.clone(),
            },
            algorithm: SigningAlgorithm::Secp256k1Recoverable,
            key_handle: HANDLE.to_owned(),
            public_key: support::public_key(&self.key),
            timeout: Duration::from_secs(5),
        })
        .unwrap_or_else(|error| panic!("remote signer: {error:?}"));
        Attestor::new(remote).unwrap_or_else(|error| panic!("attestor: {error:?}"))
    }

    fn address(&self) -> [u8; 20] {
        ethereum_address(self.key.verifying_key())
    }

    /// Signs through the real signer process and publishes the share.
    fn publish(&self, cosign: &CosignDirectory, attestation: &InboundAttestation) -> [u8; 65] {
        let attestor = self.attestor();
        assert_eq!(attestor.address(), self.address());
        let signature = attestor
            .sign_inbound(attestation)
            .unwrap_or_else(|error| panic!("sign: {error:?}"));
        cosign
            .publish(&attestation.digest(), &attestor.address(), &signature)
            .unwrap_or_else(|error| panic!("publish: {error:?}"));
        signature
    }
}

impl Drop for Operator {
    fn drop(&mut self) {
        self.kill();
    }
}

fn attestation(log_index: u64) -> InboundAttestation {
    let mut recipient = [0_u8; 32];
    recipient[31] = 0x77;
    InboundAttestation {
        chain_id: 1,
        vault: [0x11; 20],
        tx_hash: [0xab; 32],
        log_index,
        recipient,
        asset: [0x22; 20],
        amount: uint256_from_u64(1_000_000),
    }
}

fn digest_directory(shared: &Path, digest: &[u8; 32]) -> PathBuf {
    shared.join(hex::encode(digest))
}

#[test]
fn the_approved_threshold_assembles_only_from_distinct_real_operator_shares() {
    let threshold = approved_threshold();
    let root = support::work_directory("operator-cosign");
    let shared = root.join("cosign");
    let cosign = CosignDirectory::new(shared.clone());
    let mut operators: Vec<Operator> = (0..u8::try_from(threshold + 1).unwrap_or(u8::MAX))
        .map(|index| Operator::new(&root, index))
        .collect();
    let outsider = operators.pop().unwrap_or_else(|| panic!("outsider"));
    let members: Vec<[u8; 20]> = operators.iter().map(Operator::address).collect();
    let mut distinct = members.clone();
    distinct.sort_unstable();
    distinct.dedup();
    assert_eq!(
        distinct.len(),
        threshold,
        "operators hold distinct attestor keys"
    );
    for (index, operator) in operators.iter().enumerate() {
        for other in &operators[index + 1..] {
            assert_ne!(operator.home.join("journal"), other.home.join("journal"));
            assert_ne!(operator.socket, other.socket);
        }
    }

    let wanted = attestation(7);
    let digest = wanted.digest();
    let other = attestation(8);

    // Below the threshold assembly keeps waiting and never yields a partial set.
    for operator in &operators[..threshold - 1] {
        operator.publish(&cosign, &wanted);
    }
    assert_eq!(
        assemble_signatures(&digest, cosign.collect(&digest), &members, threshold),
        Err(SignatureError::BelowThreshold {
            signatures: threshold - 1,
            threshold
        })
    );

    // An unknown signer's valid share never counts toward the approved set.
    outsider.publish(&cosign, &wanted);
    assert_eq!(cosign.collect(&digest).len(), threshold);
    assert_eq!(
        assemble_signatures(&digest, cosign.collect(&digest), &members, threshold),
        Err(SignatureError::BelowThreshold {
            signatures: threshold - 1,
            threshold
        })
    );

    // A last member's share over another digest, placed under this digest, is refused.
    let last = &operators[threshold - 1];
    let foreign = last.publish(&cosign, &other);
    let directory = digest_directory(&shared, &digest);
    fs::write(
        directory.join(format!("{}.sig", hex::encode(&last.address()))),
        hex::prefixed(&foreign),
    )
    .unwrap_or_else(|error| panic!("wrong-digest share: {error}"));
    // A duplicate of a member's share under another spelling, and malformed entries.
    let first = operators[0].address();
    let first_share = fs::read_to_string(directory.join(format!("{}.sig", hex::encode(&first))))
        .unwrap_or_else(|error| panic!("first share: {error}"));
    fs::write(
        directory.join(format!("{}.sig", hex::encode(&first).to_uppercase())),
        &first_share,
    )
    .unwrap_or_else(|error| panic!("duplicate share: {error}"));
    fs::write(
        directory.join(format!("{}.sig", hex::encode(&[0x99; 20]))),
        "0xnot-a-signature",
    )
    .unwrap_or_else(|error| panic!("malformed share: {error}"));
    fs::write(directory.join("garbage.sig"), first_share.as_bytes())
        .unwrap_or_else(|error| panic!("misnamed share: {error}"));
    fs::write(
        directory.join(format!("{}.sig", hex::encode(&members[1]))),
        "x".repeat(4096),
    )
    .unwrap_or_else(|error| panic!("oversized share: {error}"));
    assert_eq!(
        assemble_signatures(&digest, cosign.collect(&digest), &members, threshold),
        Err(SignatureError::BelowThreshold {
            signatures: threshold - 2,
            threshold
        }),
        "wrong-digest, duplicate and malformed shares never count"
    );

    // Republishing the overwritten members restores the waiting set exactly.
    operators[1].publish(&cosign, &wanted);
    let below = cosign.collect(&digest);
    assert_eq!(
        assemble_signatures(&digest, below, &members, threshold),
        Err(SignatureError::BelowThreshold {
            signatures: threshold - 1,
            threshold
        })
    );

    // The last operator's signer dies; its restarted process completes the quorum
    // and every share published before the restart is preserved.
    let before: Vec<[u8; 65]> = cosign.collect(&digest);
    operators[threshold - 1].kill();
    assert!(
        operators[threshold - 1]
            .attestor()
            .sign_inbound(&wanted)
            .is_err(),
        "a dead signer process produces no share"
    );
    operators[threshold - 1].start();
    for share in &before {
        assert!(
            cosign.collect(&digest).contains(share),
            "restart preserves published shares"
        );
    }
    operators[threshold - 1].publish(&cosign, &wanted);
    let assembled = assemble_signatures(&digest, cosign.collect(&digest), &members, threshold)
        .unwrap_or_else(|error| panic!("quorum: {error:?}"));
    assert_eq!(assembled.len(), threshold);
    let signers: Vec<[u8; 20]> = assembled
        .iter()
        .map(|signature| {
            recover_signer(&digest, signature).unwrap_or_else(|error| panic!("{error:?}"))
        })
        .collect();
    assert!(
        signers.windows(2).all(|pair| pair[0] < pair[1]),
        "ascending distinct signers"
    );
    assert!(signers.iter().all(|signer| members.contains(signer)));

    // Once-only completion: every operator republishing after a restart yields
    // the identical authorization, never a second or larger set.
    for operator in &mut operators {
        operator.kill();
        operator.start();
        operator.publish(&cosign, &wanted);
    }
    assert_eq!(
        assemble_signatures(&digest, cosign.collect(&digest), &members, threshold),
        Ok(assembled)
    );

    // Private keys never enter the shared cosign storage.
    for operator in &operators {
        let secret = hex::encode(&operator.key.to_bytes()[..]);
        for entry in walk(&shared) {
            let text = fs::read(&entry).unwrap_or_default();
            assert!(
                !String::from_utf8_lossy(&text).contains(&secret),
                "{} carries a private key",
                entry.display()
            );
        }
    }
    drop(outsider);
}

fn walk(directory: &Path) -> Vec<PathBuf> {
    let mut files = Vec::new();
    for entry in fs::read_dir(directory).into_iter().flatten().flatten() {
        let path = entry.path();
        if path.is_dir() {
            files.extend(walk(&path));
        } else {
            files.push(path);
        }
    }
    files
}
