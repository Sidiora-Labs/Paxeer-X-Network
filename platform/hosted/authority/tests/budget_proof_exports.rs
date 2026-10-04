use layerx_client::evidence::{
    verify_account_evidence, verify_module_evidence, AccountEvidencePolicy,
    FinalityEvidenceCandidate, RootSelector,
};
use layerx_platform_authority::hex;
use layerx_types::verify::VerificationLevel;
use serde_json::Value;
use sha2::{Digest, Sha256};
use std::collections::BTreeSet;
use std::fs;
use std::os::unix::fs::PermissionsExt;
use std::path::Path;

fn protected(path: &Path, maximum: u64) -> Vec<u8> {
    let metadata = fs::symlink_metadata(path).expect("real corpus metadata");
    assert!(metadata.is_file() && metadata.len() > 0 && metadata.len() <= maximum);
    assert_eq!(metadata.permissions().mode() & 0o077, 0);
    fs::read(path).expect("real protected corpus")
}

fn bytes(value: &Value) -> Vec<u8> {
    let text = value.as_str().expect("canonical hexadecimal field");
    let decoded = hex::decode(text).expect("hexadecimal field");
    assert_eq!(hex::encode(&decoded), text);
    decoded
}

fn id(value: &Value) -> [u8; 32] {
    bytes(value).try_into().expect("32-byte identity")
}

fn verify_row(
    row: &Value,
    canonical: &[u8],
    proof: &[u8],
    policy: AccountEvidencePolicy,
    header: &[u8],
) -> bool {
    let selector = &row["selector"];
    match selector["kind"].as_str() {
        Some("account") => {
            verify_account_evidence(canonical, proof, id(&selector["account_id"]), None, policy)
                .is_ok_and(|verified| {
                    verified.level() >= VerificationLevel::CHECKPOINT_FINALISED
                        && verified.signed_header().canonical_bytes == header
                })
        }
        Some("module") => verify_module_evidence(
            canonical,
            proof,
            u16::try_from(selector["module_id"].as_u64().expect("module identity"))
                .expect("bounded module"),
            &bytes(&selector["key"]),
            policy,
        )
        .is_ok_and(|verified| {
            verified.level() >= VerificationLevel::CHECKPOINT_FINALISED
                && verified.signed_header().canonical_bytes == header
        }),
        _ => false,
    }
}

#[test]
fn real_budget_exports_bind_each_selector_finality_and_tamper_refusals() {
    let fixture = std::env::var_os("PAXEER_X_BUDGET_PROOF_CORPUS")
        .expect("genuine source-bound budget proof corpus required");
    let fixture: Value =
        serde_json::from_slice(&protected(Path::new(&fixture), 65_536)).expect("corpus JSON");
    let cases = fixture["cases"].as_array().expect("real export cases");
    assert!(!cases.is_empty());
    for case in cases {
        let document: Value = serde_json::from_slice(&protected(
            Path::new(case["response"].as_str().expect("response path")),
            1_048_576,
        ))
        .expect("real export JSON");
        assert_eq!(document["schema"], "layerx.human.budget-proof.v1");
        assert_eq!(document["protocol_version"], 3);
        assert_eq!(
            document["lni_interface_version"],
            serde_json::json!({"major":1,"minor":8})
        );
        assert_eq!(document["budget_state"]["budget_id"], case["budget_id"]);
        assert_eq!(document["budget_state"]["asset"], case["asset_id"]);
        assert_eq!(document["accounts"]["owner"], case["owner_account"]);
        let checkpoint = id(&case["checkpoint_id"]);
        assert_eq!(id(&document["finality"]["checkpoint_id"]), checkpoint);
        let network = u32::try_from(case["network_id"].as_u64().expect("pinned network"))
            .expect("bounded network");
        let policy = AccountEvidencePolicy {
            expected_protocol_version: 3,
            expected_network_id: network,
            handshake_sequencer_key: id(&case["sequencer_public_key"]),
            root_selector: RootSelector::Checkpoint(checkpoint),
        };
        let header = bytes(&document["finality"]["canonical_header"]);
        let certificate = bytes(&document["finality"]["checkpoint_bytes"]);
        let context = bytes(&document["finality"]["context_bytes"]);
        let candidate = FinalityEvidenceCandidate::from_exact_bytes(
            certificate.clone(),
            context.clone(),
            3,
            network,
        )
        .expect("genuine checkpoint certificate");
        assert_eq!(candidate.checkpoint_id(), checkpoint);
        assert_eq!(candidate.canonical_header(), header);
        assert_eq!(
            candidate.set_version().expect("set version"),
            document["finality"]["set_version"]
                .as_u64()
                .expect("export set version")
        );
        let mut corrupt_certificate = certificate;
        *corrupt_certificate
            .last_mut()
            .expect("nonempty certificate") ^= 1;
        assert!(FinalityEvidenceCandidate::from_exact_bytes(
            corrupt_certificate,
            context,
            3,
            network
        )
        .is_err());
        let rows = document["proofs"]
            .as_array()
            .expect("exact retained proofs");
        assert!((4..=5).contains(&rows.len()));
        let mut selectors = BTreeSet::new();
        let mut evidence = Sha256::new();
        evidence.update(b"layerx-human/native-budget-evidence/v1\0");
        evidence.update(checkpoint);
        for row in rows {
            assert!(selectors.insert(row["selector"].to_string()));
            let canonical = bytes(&row["canonical_bytes"]);
            let proof = bytes(&row["proof_material"]);
            assert!(verify_row(row, &canonical, &proof, policy, &header));
            for material in [&canonical, &proof] {
                evidence.update(
                    u64::try_from(material.len())
                        .expect("bounded material")
                        .to_be_bytes(),
                );
                evidence.update(material);
            }
            let mut corrupt = canonical.clone();
            *corrupt.first_mut().expect("nonempty native value") ^= 1;
            assert!(!verify_row(row, &corrupt, &proof, policy, &header));
            let mut corrupt = proof.clone();
            *corrupt.last_mut().expect("nonempty proof") ^= 1;
            assert!(!verify_row(row, &canonical, &corrupt, policy, &header));
            let mut wrong = policy;
            wrong.expected_network_id ^= 1;
            assert!(!verify_row(row, &canonical, &proof, wrong, &header));
            if row["selector"]["module_id"] == 3 {
                assert_eq!(
                    canonical,
                    bytes(&document["budget_state"]["canonical_core_bytes"])
                );
                assert_eq!(
                    bytes(&row["selector"]["key"]),
                    [b"budget:".as_slice(), id(&case["budget_id"]).as_slice()].concat()
                );
            }
        }
        let digest: [u8; 32] = evidence.finalize().into();
        assert_eq!(
            hex::encode(&digest),
            document["budget_state"]["evidence_digest"]
                .as_str()
                .expect("digest")
        );
        for account in ["owner", "budget", "source"] {
            assert!(rows
                .iter()
                .any(|row| row["selector"]["account_id"] == document["accounts"][account]));
        }
        assert!(rows.iter().any(|row| row["selector"]["module_id"] == 7
            && row["selector"]["key"] == document["identity_key"]));
    }
}
