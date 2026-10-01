use layerx_client::availability::RetrievalLimits;
use layerx_client::handover::SequencerHistory;
use layerx_client::lni::handshake::{perform, HandshakeConfig};
use layerx_client::lni::schema::Version;
use layerx_client::lni::transport::{ConnectionGate, Limits, Uds};
use layerx_explorer_index::receipt_authority::{
    ConfigurationError, ReadOutcome, ReceiptAuthorityReader, MAX_RESPONSE_BYTES,
};
use layerx_platform_authority::{hex, receipt_locator};
use std::{
    collections::BTreeSet,
    env, fs,
    io::{Read, Write},
    os::unix::net::UnixStream,
    path::Path,
    time::Duration,
};

fn input(name: &str) -> String {
    env::var(name).unwrap_or_else(|_| panic!("required real fixture input missing: {name}"))
}
fn refused(value: ReadOutcome) {
    assert!(
        !matches!(value, ReadOutcome::Verified(_)),
        "refusal returned facts"
    );
}

#[test]
fn real_receipts_preserve_per_receipt_authority_and_refuse_untrusted_evidence() {
    let manifest: serde_json::Value =
        serde_json::from_slice(&fs::read(input("PAXEER_X_READER_FIXTURE")).unwrap()).unwrap();
    let field = |name: &str| manifest[name].as_str().unwrap().to_owned();
    let ca = fs::read(field("ca")).unwrap();
    let bearer = fs::read_to_string(input("PAXEER_X_READER_TOKEN_FD")).unwrap();
    let replica = hex::decode32(&field("replica_id")).unwrap();
    let endpoint = field("authority_endpoint");
    let node = field("node_endpoint");
    let reader =
        ReceiptAuthorityReader::new(&endpoint, &node, bearer.clone(), replica, &ca).unwrap();
    assert!(matches!(
        ReceiptAuthorityReader::new(&endpoint, &endpoint, bearer.clone(), replica, &ca),
        Err(ConfigurationError::SameEndpoint)
    ));
    assert!(matches!(
        ReceiptAuthorityReader::new(&endpoint, &node, String::new(), replica, &ca),
        Err(ConfigurationError::Bearer)
    ));
    let historical = layerx_types::json::parse(include_str!(
        "../../../../contracts/config/native-state-proofs.json"
    ))
    .unwrap();
    for vector in historical.array_at("vectors").unwrap() {
        let witness =
            layerx_proof::state_witness::StateWitness::decode(&vector.hex_at("proof").unwrap())
                .unwrap();
        witness
            .verify(vector.hex_array_at("root").unwrap())
            .unwrap();
        let mut bad = witness.clone();
        bad.module_id = 10;
        assert_eq!(
            bad.root(),
            Err(layerx_proof::state_witness::StateProofError::Module)
        );
    }
    let genesis = fs::read(field("genesis_trust")).unwrap();
    let root = hex::decode32(&field("genesis_root")).unwrap();
    let key = hex::decode32(&field("sequencer_key")).unwrap();
    let material = layerx_wire::handover::decode_genesis_trust(&genesis)
        .expect("decode the actual native genesis trust artifact");
    assert_eq!(material.network_id, 77);
    assert_eq!(material.canonical_state_root, root);
    assert_eq!(material.initial_sequencer_key, key);
    assert!(material
        .registry
        .registrations()
        .iter()
        .any(|entry| entry.module() == layerx_types::payload::ModuleId::Spot));
    let witness = layerx_proof::state_witness::StateWitness::decode(material.governance_witness)
        .expect("decode the actual native governance witness");
    assert!(
        witness.leaf_count_b > 10,
        "native Spot registration must be committed"
    );
    witness
        .verify(root)
        .expect("native governance witness must commit to pinned genesis");
    for count in [0, 8, layerx_types::payload::ModuleId::ALL.len() as u32 + 2] {
        let mut invalid = witness.clone();
        invalid.leaf_count_b = count;
        assert!(invalid.root().is_err());
    }
    let mut unknown = witness.clone();
    unknown.module_id = u16::MAX;
    assert!(unknown.root().is_err());
    let mut outside_commitment = witness.clone();
    outside_commitment.module_id = u16::try_from(witness.leaf_count_b).unwrap();
    assert!(outside_commitment.root().is_err());
    let mut changed_value = witness.clone();
    changed_value.value[0] ^= 1;
    assert!(changed_value.verify(root).is_err());
    let mut offset = b"LXP/public-handover-genesis/v1\0".len() + 4;
    for _ in 0..3 {
        let length = u32::from_be_bytes(genesis[offset..offset + 4].try_into().unwrap()) as usize;
        offset += 4 + length;
    }
    for count in [0_u32, layerx_types::payload::ModuleId::ALL.len() as u32 + 1] {
        let mut invalid = genesis.clone();
        invalid[offset..offset + 4].copy_from_slice(&count.to_be_bytes());
        assert!(layerx_wire::handover::decode_genesis_trust(&invalid).is_err());
    }
    let mut unknown_registration = genesis.clone();
    unknown_registration[offset + 4..offset + 6].copy_from_slice(&u16::MAX.to_be_bytes());
    assert!(layerx_wire::handover::decode_genesis_trust(&unknown_registration).is_err());
    let mut wrong_root = root;
    wrong_root[0] ^= 1;
    assert!(SequencerHistory::from_genesis_artifact(&genesis, 77, wrong_root, key).is_err());
    assert!(SequencerHistory::from_genesis_artifact(&genesis, 77, root, [0; 32]).is_err());
    let mut history = SequencerHistory::from_genesis_artifact(&genesis, 77, root, key)
        .expect("authenticate the actual native genesis trust artifact");
    let excluded = history.clone();
    let mut transport = Uds::connect(
        Path::new(&field("node_socket")),
        &ConnectionGate::new(1),
        Limits {
            maximum_frame_bytes: 1_212_416,
            maximum_connections: 1,
            maximum_streams: 1,
            maximum_queued_bytes: 1_212_416,
            deadline: Duration::from_secs(8),
        },
    )
    .unwrap();
    let handshake = perform(
        &mut transport,
        &HandshakeConfig {
            built_interface_version: Version::V1_3,
            expected_protocol_version: 3,
            expected_network_id: 77,
        },
        None,
    )
    .unwrap();
    let head = handshake.node().latest_sealed_batch;
    assert!(head > 0 && head < 1024);
    for batch in 1..=head {
        let candidate = layerx_client::batch::lookup_untrusted(
            &mut transport,
            Version::V1_3,
            batch,
            100 + 3 * batch,
        )
        .unwrap();
        let header = candidate.header();
        let availability = layerx_client::availability::fetch_sealed_candidate(
            &mut layerx_client::availability::ProviderSet::new(vec![
                layerx_client::availability::Provider {
                    name: "authenticated-fixture-node".to_owned(),
                    transport: &mut transport,
                },
            ]),
            layerx_client::availability::FetchContext {
                interface_version: Version::V1_3,
                correlation_id: 101 + 3 * batch,
                expected_batch_number: batch,
                data_availability_root: header.data_availability_root(),
                record_roots: layerx_proof::availability::RootCommitments {
                    activity: header.activity_merkle_root(),
                    receipt: header.receipt_merkle_root(),
                    event: header.event_merkle_root(),
                    oracle: header.oracle_root(),
                },
                limits: RetrievalLimits {
                    maximum_bytes: 16 * 1024 * 1024,
                    maximum_chunks: 4096,
                    deadline: Duration::from_secs(8),
                },
            },
            |_| {},
        )
        .unwrap();
        let layerx_client::availability::FetchOutcome::Complete(availability) = availability else {
            panic!("actual sealed batch availability is incomplete");
        };
        history
            .advance(
                candidate.canonical_bytes(),
                candidate.signature(),
                &availability,
                None,
            )
            .expect("authenticate complete sealed batch without claiming finality");
    }
    drop(transport);
    let mut assets = BTreeSet::new();
    let mut maintained = false;
    let rows = manifest["receipts"].as_array().unwrap();
    assert!(rows.len() >= 2);
    for row in rows {
        let receipt = hex::decode(row["raw"].as_str().unwrap()).unwrap();
        let document = serde_json::to_vec(&row["evidence"]).unwrap();
        let decoded = layerx_wire::receipt::decode(&receipt).unwrap();
        let protocol = decoded.protocol().unwrap();
        let ReadOutcome::Verified(facts) = reader.read(&receipt, &history) else {
            panic!("real authority receipt refused");
        };
        assert_eq!(facts.asset, protocol.asset());
        assert_eq!(facts.global_sequence, protocol.global_sequence());
        assert_eq!(facts.batch_id, protocol.batch_id());
        assert_eq!(
            reader.verify_document(&receipt, &history, &document),
            ReadOutcome::Verified(facts)
        );
        assets.insert(facts.asset);
        maintained |= row["evidence"]["batch_evidence"]["batch_identity"]["kind"]
            .as_str()
            .is_some_and(|v| v != "historical");
        refused(reader.read(&receipt, &excluded));
        let wrong_replica =
            ReceiptAuthorityReader::new(&endpoint, &node, bearer.clone(), [0; 32], &ca).unwrap();
        refused(wrong_replica.read(&receipt, &history));
        let wrong_bearer = ReceiptAuthorityReader::new(
            &endpoint,
            &node,
            "wrong-reader-bearer".into(),
            replica,
            &ca,
        )
        .unwrap();
        assert_eq!(
            wrong_bearer.read(&receipt, &history),
            ReadOutcome::Malformed
        );
        let mut altered = row["evidence"].clone();
        let proof = altered["batch_evidence"]["receipt_proof_hex"]
            .as_str()
            .unwrap();
        let mut proof = hex::decode(proof).unwrap();
        let last = proof.len() - 1;
        proof[last] ^= 1;
        altered["batch_evidence"]["receipt_proof_hex"] = hex::encode(&proof).into();
        refused(reader.verify_document(&receipt, &history, &serde_json::to_vec(&altered).unwrap()));
        let mut changed = receipt.clone();
        let offset = changed
            .windows(32)
            .position(|bytes| bytes == facts.batch_id)
            .unwrap();
        changed[offset] ^= 1;
        refused(reader.verify_document(&changed, &history, &document));
        assert_eq!(
            reader.read(&changed, &history),
            ReadOutcome::NotYetAuthorised
        );
        let mut changed = receipt.clone();
        let offset = changed
            .windows(32)
            .position(|bytes| bytes == facts.asset)
            .unwrap();
        changed[offset] ^= 1;
        assert_ne!(
            receipt_locator(&changed).unwrap().receipt_digest,
            receipt_locator(&receipt).unwrap().receipt_digest
        );
        refused(reader.verify_document(&changed, &history, &document));
        assert_eq!(
            reader.verify_document(&receipt, &history, &document[..1]),
            ReadOutcome::Malformed
        );
        let mut oversized = document.clone();
        oversized.resize(MAX_RESPONSE_BYTES + 1, b' ');
        assert_eq!(
            reader.verify_document(&receipt, &history, &oversized),
            ReadOutcome::Malformed
        );
    }
    assert!(assets.len() >= 2, "actual receipts must cover two assets");
    assert!(
        maintained,
        "actual producer did not produce a maintained batch"
    );
    let receipt = hex::decode(rows[0]["raw"].as_str().unwrap()).unwrap();
    let locator = receipt_locator(&receipt).unwrap();
    let tls = ureq::tls::TlsConfig::builder()
        .provider(ureq::tls::TlsProvider::Rustls)
        .root_certs(ureq::tls::RootCerts::new_with_certs(&[
            ureq::tls::Certificate::from_der(&ca).to_owned(),
        ]))
        .build();
    let agent: ureq::Agent = ureq::Agent::config_builder()
        .tls_config(tls)
        .http_status_as_error(false)
        .timeout_global(Some(Duration::from_secs(8)))
        .build()
        .into();
    let missing = agent
        .get(format!(
            "{endpoint}/v1/batches/{}/receipt-authority?receipt_digest={}",
            hex::encode(&locator.batch_id),
            hex::encode(&locator.receipt_digest)
        ))
        .call()
        .unwrap();
    assert_eq!(missing.status().as_u16(), 401);
    let mut control = UnixStream::connect(field("control_socket")).unwrap();
    control
        .set_read_timeout(Some(Duration::from_secs(30)))
        .unwrap();
    control.write_all(b"stop-replica\n").unwrap();
    let mut reply = String::new();
    control.read_to_string(&mut reply).unwrap();
    assert_eq!(reply, "stopped\n");
    assert_eq!(reader.read(&receipt, &history), ReadOutcome::Unavailable);
}
