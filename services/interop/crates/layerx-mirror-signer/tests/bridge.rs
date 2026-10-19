// Bridge mode is exercised through the same remote signer client the bridge
// relayer uses over a real Unix domain socket: every returned signature is
// verified against the independently configured public key, so a successful
// round trip proves the manifest bound the handle to its own key, and a
// refusal proves the handle's own domain list was enforced.

use std::fs;
use std::os::unix::fs::PermissionsExt;
use std::path::{Path, PathBuf};
use std::thread;
use std::time::Duration;

use ed25519_dalek::SigningKey as Ed25519SigningKey;
use k256::ecdsa::SigningKey as Secp256k1SigningKey;
use layerx_mirror::signer::{
    ChainSignature, RemoteChainSigner, RemoteSignerConfig, SignerEndpoint, SignerError,
    SigningAlgorithm,
};
use layerx_mirror_signer::{
    BridgeOptions, Options, SignerListener, SignerService, StartupError,
    BRIDGE_ATTEST_INBOUND_DOMAIN, BRIDGE_ATTEST_OUTBOUND_DOMAIN,
    BRIDGE_ETHEREUM_TRANSACTION_DOMAIN, BRIDGE_PAXEER_TRANSACTION_DOMAIN,
    BRIDGE_SOLANA_TRANSACTION_DOMAIN, ETHEREUM_POLICY_DOMAIN, SOLANA_POLICY_DOMAIN,
};

const ATTESTOR_KEY_HEX: &str = "4c0883a69102937d6231471b5dbb6204fe5129617082792ae468d01a3f362318";
const PAXEER_SUBMITTER_KEY_HEX: &str =
    "0x1111111111111111111111111111111111111111111111111111111111111111";
const BASE_SUBMITTER_KEY_HEX: &str =
    "2222222222222222222222222222222222222222222222222222222222222222";
const FEE_PAYER_SEED: [u8; 32] = [
    0x9d, 0x61, 0xb1, 0x9d, 0xef, 0xfd, 0x5a, 0x60, 0xba, 0x84, 0x4a, 0xf4, 0x92, 0xec, 0x2c, 0xc4,
    0x44, 0x49, 0xc5, 0x69, 0x7b, 0x32, 0x69, 0x19, 0x70, 0x3b, 0xac, 0x03, 0x1c, 0xae, 0x7f, 0x60,
];

const ATTESTOR: &str = "bridge/attestor";
const PAXEER_SUBMITTER: &str = "bridge/submitter/paxeer";
const BASE_SUBMITTER: &str = "bridge/submitter/base";
const FEE_PAYER: &str = "bridge/fee-payer/solana";

fn work_directory(name: &str) -> PathBuf {
    let directory = std::env::temp_dir().join(format!(
        "layerx-bridge-signer-{}-{name}",
        std::process::id()
    ));
    let _ = fs::remove_dir_all(&directory);
    fs::create_dir_all(&directory)
        .unwrap_or_else(|error| panic!("work directory {}: {error}", directory.display()));
    fs::set_permissions(&directory, fs::Permissions::from_mode(0o700))
        .unwrap_or_else(|error| panic!("work directory permissions: {error}"));
    directory
}

fn owner_only(path: &Path, contents: &str) {
    fs::write(path, contents).unwrap_or_else(|error| panic!("{}: {error}", path.display()));
    fs::set_permissions(path, fs::Permissions::from_mode(0o600))
        .unwrap_or_else(|error| panic!("{} permissions: {error}", path.display()));
}

fn secp256k1_key(hex: &str) -> Secp256k1SigningKey {
    let hex = hex.trim_start_matches("0x");
    let mut raw = [0_u8; 32];
    for (index, slot) in raw.iter_mut().enumerate() {
        let pair = hex
            .get(index * 2..index * 2 + 2)
            .unwrap_or_else(|| panic!("the fixture key is 32 hexadecimal bytes"));
        *slot = u8::from_str_radix(pair, 16)
            .unwrap_or_else(|error| panic!("the fixture key is hexadecimal: {error}"));
    }
    Secp256k1SigningKey::from_slice(&raw)
        .unwrap_or_else(|error| panic!("the fixture key is a secp256k1 scalar: {error}"))
}

fn fee_payer_keypair() -> String {
    let key = Ed25519SigningKey::from_bytes(&FEE_PAYER_SEED);
    let mut keypair = Vec::with_capacity(64);
    keypair.extend_from_slice(&FEE_PAYER_SEED);
    keypair.extend_from_slice(&key.verifying_key().to_bytes());
    let values: Vec<String> = keypair.iter().map(ToString::to_string).collect();
    format!("[{}]", values.join(","))
}

fn quoted(bytes: &[u8]) -> String {
    format!("\"{}\"", String::from_utf8_lossy(bytes))
}

fn entry(handle: &str, algorithm: &str, key_file: &Path, domains: &[&[u8]]) -> String {
    let domains: Vec<String> = domains.iter().map(|domain| quoted(domain)).collect();
    format!(
        "{{\"handle\":\"{handle}\",\"algorithm\":\"{algorithm}\",\"key_file\":\"{}\",\"domains\":[{}]}}",
        key_file.display(),
        domains.join(",")
    )
}

/// Writes the four key files and returns their paths: attestor, Paxeer
/// submitter, one chain submitter and the Solana fee payer.
fn write_keys(directory: &Path) -> [PathBuf; 4] {
    let files = [
        directory.join("attestor.key"),
        directory.join("paxeer-submitter.key"),
        directory.join("base-submitter.key"),
        directory.join("solana-fee-payer.json"),
    ];
    owner_only(&files[0], ATTESTOR_KEY_HEX);
    owner_only(&files[1], PAXEER_SUBMITTER_KEY_HEX);
    owner_only(&files[2], BASE_SUBMITTER_KEY_HEX);
    owner_only(&files[3], &fee_payer_keypair());
    files
}

fn bridge_manifest(files: &[PathBuf; 4]) -> String {
    format!(
        "{{\"keys\":[{},{},{},{}]}}",
        entry(
            ATTESTOR,
            "secp256k1",
            &files[0],
            &[BRIDGE_ATTEST_INBOUND_DOMAIN, BRIDGE_ATTEST_OUTBOUND_DOMAIN]
        ),
        entry(
            PAXEER_SUBMITTER,
            "secp256k1",
            &files[1],
            &[BRIDGE_PAXEER_TRANSACTION_DOMAIN]
        ),
        entry(
            BASE_SUBMITTER,
            "secp256k1",
            &files[2],
            &[BRIDGE_ETHEREUM_TRANSACTION_DOMAIN]
        ),
        entry(
            FEE_PAYER,
            "ed25519",
            &files[3],
            &[BRIDGE_SOLANA_TRANSACTION_DOMAIN]
        ),
    )
}

fn options_with(directory: &Path, manifest: &str) -> BridgeOptions {
    let keys_file = directory.join("keys.json");
    owner_only(&keys_file, manifest);
    BridgeOptions {
        socket: directory.join("signer.sock"),
        keys_file,
    }
}

fn serve(listener: SignerListener) {
    let mode = fs::metadata(listener.socket())
        .unwrap_or_else(|error| panic!("the signer socket is missing: {error}"))
        .permissions()
        .mode()
        & 0o777;
    assert_eq!(
        mode, 0o660,
        "the signer socket must be reachable by its owner and group only"
    );
    thread::spawn(move || listener.serve());
}

fn secp256k1_client(socket: &Path, handle: &str, hex: &str) -> RemoteChainSigner {
    client(
        socket,
        handle,
        SigningAlgorithm::Secp256k1Recoverable,
        secp256k1_key(hex)
            .verifying_key()
            .to_encoded_point(true)
            .as_bytes()
            .to_vec(),
    )
}

fn fee_payer_client(socket: &Path, handle: &str) -> RemoteChainSigner {
    client(
        socket,
        handle,
        SigningAlgorithm::Ed25519,
        Ed25519SigningKey::from_bytes(&FEE_PAYER_SEED)
            .verifying_key()
            .to_bytes()
            .to_vec(),
    )
}

fn client(
    socket: &Path,
    handle: &str,
    algorithm: SigningAlgorithm,
    public_key: Vec<u8>,
) -> RemoteChainSigner {
    RemoteChainSigner::new(RemoteSignerConfig {
        endpoint: SignerEndpoint::Uds {
            socket: socket.to_path_buf(),
        },
        algorithm,
        key_handle: handle.to_owned(),
        public_key,
        timeout: Duration::from_secs(5),
    })
    .unwrap_or_else(|error| panic!("the relayer signer client refused the fixture: {error:?}"))
}

fn assert_digest_signed(signer: &RemoteChainSigner, domain: &[u8]) {
    match signer.sign_digest(domain, [0x5c_u8; 32]) {
        Ok(ChainSignature::Secp256k1(signature)) => assert!(
            signature[64] <= 1,
            "the recovery identifier must be 0 or 1, got {}",
            signature[64]
        ),
        other => panic!(
            "{} did not sign {}: {other:?}",
            signer.key_handle(),
            String::from_utf8_lossy(domain)
        ),
    }
}

fn assert_digest_refused(signer: &RemoteChainSigner, domain: &[u8]) {
    assert_eq!(
        signer.sign_digest(domain, [0x5c_u8; 32]),
        Err(SignerError::Refused),
        "{} must refuse {}",
        signer.key_handle(),
        String::from_utf8_lossy(domain)
    );
}

#[test]
fn every_bridge_handle_signs_its_own_domains_with_its_own_key() {
    let directory = work_directory("round-trip");
    let files = write_keys(&directory);
    let options = options_with(&directory, &bridge_manifest(&files));
    let listener = SignerListener::bind_bridge(&options)
        .unwrap_or_else(|error| panic!("the bridge signer refused to start: {error}"));
    assert_eq!(
        listener.service().handles(),
        vec![ATTESTOR, PAXEER_SUBMITTER, BASE_SUBMITTER, FEE_PAYER]
    );
    serve(listener);

    let attestor = secp256k1_client(&options.socket, ATTESTOR, ATTESTOR_KEY_HEX);
    assert_digest_signed(&attestor, BRIDGE_ATTEST_INBOUND_DOMAIN);
    assert_digest_signed(&attestor, BRIDGE_ATTEST_OUTBOUND_DOMAIN);
    let paxeer = secp256k1_client(&options.socket, PAXEER_SUBMITTER, PAXEER_SUBMITTER_KEY_HEX);
    assert_digest_signed(&paxeer, BRIDGE_PAXEER_TRANSACTION_DOMAIN);
    let base = secp256k1_client(&options.socket, BASE_SUBMITTER, BASE_SUBMITTER_KEY_HEX);
    assert_digest_signed(&base, BRIDGE_ETHEREUM_TRANSACTION_DOMAIN);
    let fee_payer = fee_payer_client(&options.socket, FEE_PAYER);
    match fee_payer.sign_message(BRIDGE_SOLANA_TRANSACTION_DOMAIN, b"LayerX bridge release") {
        Ok(ChainSignature::Ed25519(signature)) => {
            assert_ne!(
                signature, [0_u8; 64],
                "the fee payer signature must be real"
            );
        }
        other => panic!("the fee payer did not sign: {other:?}"),
    }

    let _ = fs::remove_dir_all(&directory);
}

#[test]
fn a_bridge_handle_refuses_every_domain_it_does_not_list() {
    let directory = work_directory("domain-refusal");
    let files = write_keys(&directory);
    let options = options_with(&directory, &bridge_manifest(&files));
    serve(
        SignerListener::bind_bridge(&options)
            .unwrap_or_else(|error| panic!("the bridge signer refused to start: {error}")),
    );

    let attestor = secp256k1_client(&options.socket, ATTESTOR, ATTESTOR_KEY_HEX);
    for domain in [
        BRIDGE_PAXEER_TRANSACTION_DOMAIN,
        BRIDGE_ETHEREUM_TRANSACTION_DOMAIN,
        ETHEREUM_POLICY_DOMAIN,
    ] {
        assert_digest_refused(&attestor, domain);
    }
    let paxeer = secp256k1_client(&options.socket, PAXEER_SUBMITTER, PAXEER_SUBMITTER_KEY_HEX);
    for domain in [
        BRIDGE_ATTEST_INBOUND_DOMAIN,
        BRIDGE_ATTEST_OUTBOUND_DOMAIN,
        BRIDGE_ETHEREUM_TRANSACTION_DOMAIN,
    ] {
        assert_digest_refused(&paxeer, domain);
    }
    let base = secp256k1_client(&options.socket, BASE_SUBMITTER, BASE_SUBMITTER_KEY_HEX);
    for domain in [
        BRIDGE_ATTEST_INBOUND_DOMAIN,
        BRIDGE_PAXEER_TRANSACTION_DOMAIN,
    ] {
        assert_digest_refused(&base, domain);
    }
    let fee_payer = fee_payer_client(&options.socket, FEE_PAYER);
    assert_eq!(
        fee_payer.sign_message(SOLANA_POLICY_DOMAIN, b"archive"),
        Err(SignerError::Refused),
        "the fee payer must refuse the mirror Solana domain"
    );
    let unknown = secp256k1_client(&options.socket, "bridge/submitter/absent", ATTESTOR_KEY_HEX);
    assert_digest_refused(&unknown, BRIDGE_ATTEST_INBOUND_DOMAIN);

    let _ = fs::remove_dir_all(&directory);
}

#[test]
fn a_manifest_that_does_not_bind_each_handle_cleanly_is_refused() {
    let directory = work_directory("manifest-refusal");
    let files = write_keys(&directory);
    let attestor = |domains: &[&[u8]]| entry(ATTESTOR, "secp256k1", &files[0], domains);
    let cases = [
        ("no keys", "{\"keys\":[]}".to_owned()),
        (
            "an unknown top-level field",
            format!(
                "{{\"keys\":[{}],\"extra\":1}}",
                attestor(&[BRIDGE_ATTEST_INBOUND_DOMAIN])
            ),
        ),
        (
            "a repeated handle",
            format!(
                "{{\"keys\":[{},{}]}}",
                attestor(&[BRIDGE_ATTEST_INBOUND_DOMAIN]),
                attestor(&[BRIDGE_ATTEST_OUTBOUND_DOMAIN])
            ),
        ),
        (
            "a mirror domain",
            format!("{{\"keys\":[{}]}}", attestor(&[ETHEREUM_POLICY_DOMAIN])),
        ),
        (
            "a domain twice",
            format!(
                "{{\"keys\":[{}]}}",
                attestor(&[BRIDGE_ATTEST_INBOUND_DOMAIN, BRIDGE_ATTEST_INBOUND_DOMAIN])
            ),
        ),
        (
            "attestation and transaction domains on one handle",
            format!(
                "{{\"keys\":[{}]}}",
                attestor(&[
                    BRIDGE_ATTEST_INBOUND_DOMAIN,
                    BRIDGE_PAXEER_TRANSACTION_DOMAIN
                ])
            ),
        ),
        (
            "a secp256k1 key on the Solana domain",
            format!(
                "{{\"keys\":[{}]}}",
                attestor(&[BRIDGE_SOLANA_TRANSACTION_DOMAIN])
            ),
        ),
        (
            "an unknown algorithm",
            format!(
                "{{\"keys\":[{}]}}",
                entry(ATTESTOR, "p256", &files[0], &[BRIDGE_ATTEST_INBOUND_DOMAIN])
            ),
        ),
        (
            "a relative key file",
            format!(
                "{{\"keys\":[{}]}}",
                entry(
                    ATTESTOR,
                    "secp256k1",
                    Path::new("attestor.key"),
                    &[BRIDGE_ATTEST_INBOUND_DOMAIN]
                )
            ),
        ),
        ("no domain", format!("{{\"keys\":[{}]}}", attestor(&[]))),
    ];
    for (case, manifest) in cases {
        let options = options_with(&directory, &manifest);
        match SignerService::load_bridge(&options) {
            Err(StartupError::Manifest { .. }) => {}
            Err(other) => panic!("{case}: refused for the wrong reason: {other}"),
            Ok(_) => panic!("{case}: the manifest must be refused"),
        }
    }

    fs::set_permissions(&files[0], fs::Permissions::from_mode(0o620))
        .unwrap_or_else(|error| panic!("attestor key permissions: {error}"));
    let options = options_with(
        &directory,
        &format!(
            "{{\"keys\":[{}]}}",
            attestor(&[BRIDGE_ATTEST_INBOUND_DOMAIN])
        ),
    );
    assert!(
        matches!(
            SignerService::load_bridge(&options),
            Err(StartupError::KeyMaterial { .. })
        ),
        "a group-writable key file must be refused in bridge mode as in mirror mode"
    );

    let _ = fs::remove_dir_all(&directory);
}

#[test]
fn mirror_mode_keeps_its_handles_and_refuses_every_bridge_domain() {
    let directory = work_directory("mirror-compatibility");
    let ethereum = directory.join("ethereum.key");
    owner_only(&ethereum, ATTESTOR_KEY_HEX);
    let solana = directory.join("solana.json");
    owner_only(&solana, &fee_payer_keypair());
    let defaults = Options::default();
    let options = Options {
        socket: directory.join("signer.sock"),
        ethereum_key_file: ethereum,
        solana_keypair_file: solana,
        ..defaults
    };
    assert_eq!(options.ethereum_key_handle, "mirror/ethereum/beta");
    assert_eq!(options.solana_key_handle, "mirror/solana/beta");
    let listener = SignerListener::bind(&options)
        .unwrap_or_else(|error| panic!("the mirror signer refused to start: {error}"));
    assert_eq!(
        listener.service().handles(),
        vec!["mirror/ethereum/beta", "mirror/solana/beta"]
    );
    serve(listener);

    let ethereum = secp256k1_client(&options.socket, "mirror/ethereum/beta", ATTESTOR_KEY_HEX);
    assert_digest_signed(&ethereum, ETHEREUM_POLICY_DOMAIN);
    for domain in [
        BRIDGE_ATTEST_INBOUND_DOMAIN,
        BRIDGE_ATTEST_OUTBOUND_DOMAIN,
        BRIDGE_PAXEER_TRANSACTION_DOMAIN,
        BRIDGE_ETHEREUM_TRANSACTION_DOMAIN,
    ] {
        assert_digest_refused(&ethereum, domain);
    }
    let solana = fee_payer_client(&options.socket, "mirror/solana/beta");
    assert!(
        matches!(
            solana.sign_message(SOLANA_POLICY_DOMAIN, b"archive"),
            Ok(ChainSignature::Ed25519(_))
        ),
        "the mirror Solana handle must still sign its own domain"
    );
    assert_eq!(
        solana.sign_message(BRIDGE_SOLANA_TRANSACTION_DOMAIN, b"release"),
        Err(SignerError::Refused),
        "the mirror Solana handle must refuse the bridge fee payer domain"
    );

    let _ = fs::remove_dir_all(&directory);
}
