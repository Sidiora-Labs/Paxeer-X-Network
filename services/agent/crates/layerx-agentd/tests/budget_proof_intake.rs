use layerx_agentd::budget::budget_proof::{verify_budget_proof, BudgetProofTrust};
use layerx_agentd::human::{HumanPeer, HumanSubject};
use layerx_agentd::human_runtime::{HumanAuthorityBoundary, RemoteHumanAuthority};
use layerx_client::client::{ClientConfig, ReconnectPolicy};
use layerx_client::evidence::CheckpointSelector;
use layerx_client::lni::handshake::HandshakeConfig;
use layerx_client::lni::schema::Version;
use layerx_client::lni::transport::Limits;
use layerx_client::Client;
use layerx_programs::hex;
use layerx_types::ids::Did;
use serde_json::Value;
use std::fs;
use std::os::unix::fs::PermissionsExt;
use std::path::Path;
use std::time::{Duration, SystemTime, UNIX_EPOCH};

fn protected(path: &Path, max: u64) -> Vec<u8> {
    let meta = fs::symlink_metadata(path).expect("genuine fixture metadata");
    assert!(
        meta.is_file()
            && meta.len() > 0
            && meta.len() <= max
            && meta.permissions().mode() & 0o077 == 0
    );
    fs::read(path).expect("protected genuine fixture")
}
fn text<'a>(value: &'a Value, key: &str) -> &'a str {
    value[key].as_str().expect("real fixture field")
}
fn id(value: &str) -> [u8; 32] {
    hex::decode(value)
        .expect("native identity")
        .try_into()
        .expect("32-byte identity")
}

#[test]
fn real_remote_native_budget_proofs_refuse_report_selector_principal_and_finality_substitution() {
    let path = std::env::var_os("PAXEER_X_BUDGET_PROOF_INTAKE_FIXTURE")
        .expect("genuine protected TLS/LNI fixture required");
    let fixture: Value =
        serde_json::from_slice(&protected(Path::new(&path), 65_536)).expect("genuine fixture JSON");
    assert_eq!(fixture["schema"], "paxeer-x.budget-proof-intake.v1");
    let network = u32::try_from(fixture["network_id"].as_u64().expect("native network"))
        .expect("network bound");
    let mut node = Client::connect(ClientConfig {
        endpoint: Path::new(text(&fixture, "lni_socket")).to_path_buf(),
        handshake: HandshakeConfig {
            built_interface_version: Version::V1_8,
            expected_protocol_version: 3,
            expected_network_id: network,
        },
        limits: Limits {
            maximum_frame_bytes: 1_212_416,
            maximum_connections: 1,
            maximum_streams: 1,
            maximum_queued_bytes: 1_212_416,
            deadline: Duration::from_secs(8),
        },
        reconnect: ReconnectPolicy {
            maximum_attempts: 1,
            base_delay: Duration::from_millis(1),
            maximum_delay: Duration::from_millis(1),
            jitter_percent: 0,
        },
    })
    .expect("real authenticated native node");
    let head = node.head();
    let checkpoint = node
        .checkpoint_evidence(CheckpointSelector::Identifier(head.finalised_checkpoint), 1)
        .expect("real trusted native checkpoint");
    let bearer = String::from_utf8(protected(Path::new(text(&fixture, "bearer_file")), 4096))
        .expect("protected bearer");
    let ca = protected(Path::new(text(&fixture, "ca_der_file")), 65_536);
    let mut authority = RemoteHumanAuthority::connect(
        text(&fixture, "authority_endpoint"),
        bearer.trim().to_owned(),
        Duration::from_secs(8),
        1_048_576,
        &ca,
    )
    .expect("real private TLS authority");
    let registration = fixture
        .get("registration_file")
        .and_then(Value::as_str)
        .map(|path| protected(Path::new(path), 1_048_576));
    let peer = HumanPeer {
        uid: u32::try_from(fixture["uid"].as_u64().expect("native peer uid")).expect("uid bound"),
        tenant: text(&fixture, "agent_tenant").to_owned(),
        principal: text(&fixture, "subject_principal").to_owned(),
        subject: Some(HumanSubject {
            transport_tenant: text(&fixture, "tenant").to_owned(),
            transport_principal: text(&fixture, "principal").to_owned(),
            owner: text(&fixture, "subject_owner").to_owned(),
            account: text(&fixture, "subject_account").to_owned(),
            asset: id(text(&fixture, "asset_id")),
            registration,
        }),
    };
    let owner = Did::new(text(&fixture, "budget_owner_did").as_bytes())
        .expect("actual managed budget owner");
    let budget = id(text(&fixture, "budget_id"));
    let raw = authority
        .budget_proof_export(&peer, budget, &owner)
        .expect("actual remote proof bytes");
    let now_ms = u64::try_from(
        SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .expect("clock")
            .as_millis(),
    )
    .expect("clock bound");
    let trust = BudgetProofTrust {
        tenant: text(&fixture, "tenant"),
        principal: text(&fixture, "principal"),
        owner: &owner,
        protocol_version: 3,
        network_id: network,
        sequencer_key: node.handshake().node().authorised_sequencer_key,
        head,
        checkpoint: &checkpoint,
        now_ms,
        maximum_age_seconds: fixture["maximum_age_seconds"]
            .as_u64()
            .expect("configured freshness"),
    };
    let verified =
        verify_budget_proof(&raw, budget, &trust).expect("actual proof independently verified");
    assert_eq!(verified.budget_id(), budget);
    assert_eq!(verified.asset(), id(text(&fixture, "asset_id")));
    assert_eq!(verified.canonical_export_bytes(), raw);
    let actual: Value = serde_json::from_slice(&raw).expect("real export JSON");
    for field in [
        "remaining",
        "evidence_digest",
        "receipt_digest",
        "canonical_core_bytes",
    ] {
        let mut changed = actual.clone();
        changed["budget_state"][field] = Value::String(if field == "remaining" {
            "999999999999999999999999999999".to_owned()
        } else {
            "00".repeat(32)
        });
        assert!(verify_budget_proof(
            &serde_json::to_vec(&changed).expect("modified real candidate"),
            budget,
            &trust
        )
        .is_err());
    }
    for field in ["tenant", "principal", "schema"] {
        let mut changed = actual.clone();
        changed[field] = Value::String("substituted".to_owned());
        assert!(verify_budget_proof(
            &serde_json::to_vec(&changed).expect("modified real candidate"),
            budget,
            &trust
        )
        .is_err());
    }
    for field in ["canonical_header", "checkpoint_bytes", "context_bytes"] {
        let mut changed = actual.clone();
        let value = changed["finality"][field]
            .as_str()
            .expect("actual finality bytes");
        let mut bytes = hex::decode(value).expect("canonical finality");
        *bytes.last_mut().expect("nonempty finality") ^= 1;
        changed["finality"][field] = Value::String(hex::encode(&bytes));
        assert!(verify_budget_proof(
            &serde_json::to_vec(&changed).expect("modified real candidate"),
            budget,
            &trust
        )
        .is_err());
    }
    let rows = actual["proofs"].as_array().expect("genuine witness rows");
    for index in 0..rows.len() {
        for field in ["canonical_bytes", "proof_material"] {
            let mut changed = actual.clone();
            let value = changed["proofs"][index][field]
                .as_str()
                .expect("actual proof bytes");
            let mut bytes = hex::decode(value).expect("canonical witness");
            *bytes.last_mut().expect("nonempty witness") ^= 1;
            changed["proofs"][index][field] = Value::String(hex::encode(&bytes));
            assert!(verify_budget_proof(
                &serde_json::to_vec(&changed).expect("modified real candidate"),
                budget,
                &trust
            )
            .is_err());
        }
    }
    let mut changed = actual.clone();
    changed["proofs"][0]["selector"]["key"] = Value::String("00".repeat(32));
    assert!(verify_budget_proof(
        &serde_json::to_vec(&changed).expect("modified real selector"),
        budget,
        &trust
    )
    .is_err());
    let mut wrong_budget = budget;
    wrong_budget[0] ^= 1;
    assert!(verify_budget_proof(&raw, wrong_budget, &trust).is_err());
}
