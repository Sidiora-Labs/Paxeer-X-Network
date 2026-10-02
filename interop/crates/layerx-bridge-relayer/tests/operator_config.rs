use std::collections::BTreeSet;
use std::path::{Path, PathBuf};
use std::sync::atomic::{AtomicU64, Ordering};

use layerx_bridge_relayer::config::RelayerConfig;
use layerx_bridge_relayer::relayer::RelayerError;
use layerx_bridge_relayer::solana::release::{find_program_address, VAULT_SEED};
use layerx_bridge_relayer::solana::rpc::Commitment;
use layerx_bridge_relayer::solana::{base58_encode, handle, SOLANA_CHAIN_ID};
use serde_json::{json, Value};

const EXAMPLE_PATH: &str = concat!(
    env!("CARGO_MANIFEST_DIR"),
    "/../../../bridge/deploy/relayer-solana.example.json"
);
const EXAMPLE: &str = include_str!(concat!(
    env!("CARGO_MANIFEST_DIR"),
    "/../../../bridge/deploy/relayer-solana.example.json"
));
static NEXT_FILE: AtomicU64 = AtomicU64::new(0);

struct ConfigFile {
    directory: PathBuf,
}

impl ConfigFile {
    fn new(value: &Value) -> Self {
        let directory = std::env::temp_dir().join(format!(
            "layerx-operator-config-{}-{}",
            std::process::id(),
            NEXT_FILE.fetch_add(1, Ordering::Relaxed)
        ));
        std::fs::create_dir(&directory).expect("create exclusive config case directory");
        let file = Self { directory };
        std::fs::write(
            file.directory.join("relayer.json"),
            serde_json::to_vec(value).expect("encode public config case"),
        )
        .expect("write public config case");
        file
    }

    fn load(&self) -> Result<RelayerConfig, RelayerError> {
        RelayerConfig::load(&self.directory.join("relayer.json"))
    }
}

impl Drop for ConfigFile {
    fn drop(&mut self) {
        let _ = std::fs::remove_dir_all(&self.directory);
    }
}

fn example() -> Value {
    serde_json::from_str(EXAMPLE).expect("documented example JSON")
}

fn load(value: &Value) -> RelayerConfig {
    ConfigFile::new(value)
        .load()
        .expect("production loader accepts public config case")
}

fn refusal(value: &Value) -> String {
    match ConfigFile::new(value).load() {
        Err(RelayerError::Configuration(detail)) => detail,
        result => panic!("expected configuration refusal, got {result:?}"),
    }
}

fn replace(value: &mut Value, pointer: &str, replacement: Value) {
    *value.pointer_mut(pointer).expect("existing config field") = replacement;
}

#[test]
fn documented_example_loads_through_production_config() {
    let config = RelayerConfig::load(Path::new(EXAMPLE_PATH))
        .expect("unmodified documented example passes production validation");
    assert_eq!(config, load(&example()));
    let solana = config.solana.as_ref().expect("Solana entry");
    let settings = solana.settings().expect("typed observer settings");
    assert_eq!(settings.chain_id, SOLANA_CHAIN_ID);
    assert_eq!(settings.commitment, Commitment::Finalized);
    assert_eq!(settings.finality_depth, 32);
    assert_eq!(settings.start_slot, 1);
    assert_eq!(settings.max_slot_range, 100);
    assert_eq!(solana.mints().expect("typed release mints"), vec![[1; 32]]);
    let (authority, bump) = find_program_address(&[VAULT_SEED], &settings.program_id)
        .expect("public custody program PDA vector");
    assert_eq!(bump, 255);
    assert_eq!(
        base58_encode(&authority),
        "GxxA9Cs9v5pAGVsaCe2jjDrtmieeBijcY4S5HHTY8Vq6"
    );
    assert_eq!(settings.vault, handle(&authority));
    assert_ne!(settings.vault, handle(&settings.program_id));
    let mut handles = BTreeSet::new();
    for key in std::iter::once(&config.attestor)
        .chain(std::iter::once(&config.paxeer.submitter))
        .chain(config.chains.iter().map(|chain| &chain.submitter))
    {
        assert!(key.handle.starts_with("EXAMPLE_ONLY/"));
        assert!(handles.insert(key.handle.as_str()));
        k256::PublicKey::from_sec1_bytes(&key.public_key).expect("full public secp256k1 vector");
    }
    assert!(handles.insert(solana.fee_payer.handle.as_str()));
    assert!(solana.fee_payer.handle.starts_with("EXAMPLE_ONLY/"));
    let fee_public: [u8; 32] = solana.fee_payer.public_key.as_slice().try_into().expect("ed25519 width");
    ed25519_dalek::VerifyingKey::from_bytes(&fee_public).expect("public ed25519 vector");
    for rpc in std::iter::once(&solana.rpc).chain(config.chains.iter().map(|chain| &chain.rpc)) {
        assert_eq!(rpc.endpoints.len(), 3);
        assert_eq!(rpc.quorum, 2);
        let mut backends = BTreeSet::new();
        let mut origins = BTreeSet::new();
        for endpoint in &rpc.endpoints {
            assert!(endpoint.url.starts_with("https://"));
            assert!(endpoint.url.ends_with(".example.invalid/rpc"));
            assert!(origins.insert(endpoint.url.as_str()));
            assert!(endpoint.independent_backend.starts_with("EXAMPLE_ONLY-"));
            assert!(backends.insert(endpoint.independent_backend.as_str()));
            assert!(endpoint.ca_certificate_der.starts_with("/EXAMPLE_ONLY"));
            assert!(endpoint.bearer_token_file.starts_with("/EXAMPLE_ONLY"));
        }
    }
}

#[test]
fn solana_omission_and_empty_mints_preserve_observation_only_settings() {
    let configured = load(&example());
    let expected = configured.solana.as_ref().expect("Solana entry");
    for omitted in [false, true] {
        let mut value = example();
        if omitted {
            value["solana"].as_object_mut().expect("Solana object").remove("release_mints");
        } else {
            value["solana"]["release_mints"] = json!([]);
        }
        let config = load(&value);
        let observation = config.solana.expect("observer remains configured");
        assert!(observation.release_mints.is_empty());
        assert!(observation.mints().expect("empty mint set").is_empty());
        assert_eq!(observation.settings().expect("observer settings"), expected.settings().expect("original settings"));
        assert_eq!(observation.fee_payer, expected.fee_payer);
    }
    let mut value = example();
    value.as_object_mut().expect("config object").remove("solana");
    let evm_only = load(&value);
    assert!(evm_only.solana.is_none());
    assert_eq!(evm_only.chains, configured.chains);
    assert_eq!(evm_only.paxeer, configured.paxeer);
}

#[test]
fn accepted_commitments_preserve_slot_policy() {
    for (text, commitment) in [("confirmed", Commitment::Confirmed), ("finalized", Commitment::Finalized)] {
        let mut value = example();
        value["solana"]["commitment"] = json!(text);
        value["solana"]["finality_depth"] = json!(1);
        value["solana"]["start_slot"] = json!(0);
        value["solana"]["max_slot_range"] = json!(1);
        let settings = load(&value).solana.expect("Solana entry").settings().expect("settings");
        assert_eq!(settings.commitment, commitment);
        assert_eq!(settings.finality_depth, 1);
        assert_eq!(settings.start_slot, 0);
        assert_eq!(settings.max_slot_range, 1);
    }
}

#[test]
fn unknown_fields_are_refused_at_every_config_boundary() {
    for pointer in [
        "", "/signer", "/signer/endpoint", "/attestor", "/paxeer", "/paxeer/endpoints/0",
        "/paxeer/submitter", "/paxeer/gas", "/chains/0", "/chains/0/rpc",
        "/chains/0/rpc/endpoints/0", "/chains/0/submitter", "/chains/0/gas",
        "/solana", "/solana/rpc", "/solana/rpc/endpoints/0", "/solana/fee_payer",
    ] {
        let mut value = example();
        value.pointer_mut(pointer).expect("schema object").as_object_mut().expect("object")
            .insert("unexpected_operator_field".to_owned(), json!(true));
        let detail = refusal(&value);
        assert!(detail.starts_with("unknown field `unexpected_operator_field`"), "{pointer}: {detail}");
    }
}

#[test]
fn reserved_chain_identity_is_not_an_evm_destination() {
    let expected = format!(
        "solana must use the reserved chain id {SOLANA_CHAIN_ID} and no ethereum chain may"
    );
    for chain_id in [0, 1, 125, SOLANA_CHAIN_ID - 1, SOLANA_CHAIN_ID + 1] {
        let mut value = example();
        value["solana"]["chain_id"] = json!(chain_id);
        assert_eq!(refusal(&value), expected);
    }
    let mut value = example();
    value["chains"][0]["chain_id"] = json!(SOLANA_CHAIN_ID);
    assert_eq!(refusal(&value), expected);
}

#[test]
fn invalid_commitments_are_refused_during_load() {
    for commitment in ["processed", "Confirmed", "FINALIZED", "finalised", ""] {
        let mut value = example();
        value["solana"]["commitment"] = json!(commitment);
        let detail = refusal(&value);
        assert!(detail.starts_with(&format!("unknown variant `{commitment}`")), "{detail}");
        assert!(detail.contains("expected `confirmed` or `finalized`"), "{detail}");
    }
    for commitment in [Value::Null, json!(1), json!([])] {
        let mut value = example();
        value["solana"]["commitment"] = commitment;
        assert!(refusal(&value).starts_with("invalid type:"));
    }
}

#[test]
fn attestor_handle_cannot_pay_any_chain_fees() {
    for pointer in ["/solana/fee_payer/handle", "/paxeer/submitter/handle", "/chains/0/submitter/handle"] {
        let mut value = example();
        let attestor = value["attestor"]["handle"].clone();
        replace(&mut value, pointer, attestor);
        assert_eq!(refusal(&value), "the attestor key must not pay transaction fees");
    }
    let mut value = example();
    value["solana"].as_object_mut().expect("Solana object").remove("release_mints");
    value["solana"]["fee_payer"]["handle"] = value["attestor"]["handle"].clone();
    assert_eq!(refusal(&value), "the attestor key must not pay transaction fees");
}

#[test]
fn solana_identity_and_bounds_are_required() {
    for (pointer, replacement, expected) in [
        ("/solana/vault", json!("0x0000000000000000000000000000000000000000"), "solana needs a vault, a finality depth and a slot range"),
        ("/solana/finality_depth", json!(0), "solana needs a vault, a finality depth and a slot range"),
        ("/solana/max_slot_range", json!(0), "solana needs a vault, a finality depth and a slot range"),
        ("/solana/program_id", json!("11111111111111111111111111111111"), "solana program id must not be the zero key"),
        ("/solana/program_id", json!("not-a-base58-key"), "solana program id is not a base58 32-byte key"),
        ("/solana/program_id", json!("1"), "solana program id is not a base58 32-byte key"),
        ("/solana/fee_payer/handle", json!(""), "the solana fee payer needs a handle and a 32-byte public key"),
        ("/solana/fee_payer/public_key", json!("0x01"), "the solana fee payer needs a handle and a 32-byte public key"),
    ] {
        let mut value = example();
        replace(&mut value, pointer, replacement);
        assert_eq!(refusal(&value), expected, "{pointer}");
    }
    let mut value = example();
    value["solana"]["fee_payer"]["public_key"] = value["attestor"]["public_key"].clone();
    assert_eq!(refusal(&value), "the solana fee payer needs a handle and a 32-byte public key");
    let mut value = example();
    value["solana"]["vault"] = json!("0x01");
    assert!(refusal(&value).contains("hex"));
}

#[test]
fn release_mints_are_distinct_nonzero_full_keys() {
    let mint = example()["solana"]["release_mints"][0].clone();
    for mints in [json!([mint.clone(), mint]), json!(["11111111111111111111111111111111"])] {
        let mut value = example();
        value["solana"]["release_mints"] = mints;
        assert_eq!(refusal(&value), "solana release mints must be distinct non-zero keys");
    }
    for mint in ["", "1", "not-a-base58-key"] {
        let mut value = example();
        value["solana"]["release_mints"] = json!([mint]);
        assert_eq!(refusal(&value), "a solana release mint is not a base58 32-byte key");
    }
}

#[test]
fn required_solana_fields_cannot_be_omitted() {
    for field in ["chain_id", "vault", "program_id", "commitment", "finality_depth", "start_slot", "max_slot_range", "rpc", "fee_payer"] {
        let mut value = example();
        value["solana"].as_object_mut().expect("Solana object").remove(field);
        let detail = refusal(&value);
        assert!(detail.starts_with(&format!("missing field `{field}`")), "{detail}");
    }
}
