use std::fs;

use layerx_client::evidence::{
    verify_arbiter_admission_v3, verify_arbiter_admission_v3_bounded, MAX_ADMISSION_PRESTATE_BYTES,
};
use layerx_proof::inclusion::{verify_header, verify_receipt, SequencerAuthorization};
use layerx_proof::merkle::decode_proof;
use layerx_proof::receipt::{
    verify_outcome_maintained_chain, verify_program_preexecution_rejection_maintained_chain,
    AuthorizedBatch, MaintainedOutcomeEvidence, VerifiedReceipt,
};
use layerx_proof::state_range::{ModuleRangeWitness, RangeError, MAX_MODULE_LEAVES};
use layerx_proof::state_witness::StateWitness;
use serde_json::Value;

struct Capture {
    name: String,
    v3: Vec<u8>,
    v2: Vec<u8>,
    receipt: VerifiedReceipt,
}

struct Fixture {
    network: u32,
    captures: Vec<Capture>,
}

fn string<'a>(value: &'a Value, key: &str) -> &'a str {
    value[key]
        .as_str()
        .unwrap_or_else(|| panic!("missing genuine fixture field {key}"))
}

fn bytes(value: &Value, key: &str) -> Vec<u8> {
    fs::read(string(value, key)).unwrap_or_else(|error| panic!("native fixture {key}: {error}"))
}

fn hex32(value: &Value, key: &str) -> [u8; 32] {
    let text = string(value, key);
    assert_eq!(text.len(), 64);
    let mut out = [0; 32];
    for (index, byte) in out.iter_mut().enumerate() {
        *byte = u8::from_str_radix(&text[index * 2..index * 2 + 2], 16)
            .expect("native pinned identity");
    }
    out
}

impl Fixture {
    fn load() -> Self {
        let path = std::env::var("LAYERX_ARBITER_ADMISSION_INPUTS")
            .expect("required genuine native arbiter prestate fixture; no fallback");
        let manifest: Value =
            serde_json::from_slice(&fs::read(path).expect("native fixture manifest"))
                .expect("canonical fixture JSON");
        let network = u32::try_from(manifest["network_id"].as_u64().expect("native network"))
            .expect("native network width");
        assert_ne!(network, 0);
        let authorization = SequencerAuthorization::new(
            hex32(&manifest, "sequencer_id"),
            hex32(&manifest, "sequencer_public_key"),
            manifest["first_batch_number"]
                .as_u64()
                .expect("first configured batch"),
            manifest["last_batch_number"]
                .as_u64()
                .expect("last configured batch"),
        );
        let captures = manifest["captures"]
            .as_array()
            .expect("native captures")
            .iter()
            .map(|capture| {
                let receipt = bytes(capture, "receipt_path");
                let proof =
                    decode_proof(&bytes(capture, "proof_path")).expect("native receipt path");
                let header = bytes(capture, "header_path");
                let signature: [u8; 64] = bytes(capture, "header_signature_path")
                    .try_into()
                    .expect("real header signature");
                let verified_header = verify_header(&header, &signature, &authorization)
                    .expect("native signed header authority");
                let signed = verified_header.header();
                verify_receipt(&receipt, &proof, &header, &signature, &authorization)
                    .expect("real receipt inclusion");
                let maintenance = bytes(capture, "maintenance_path");
                let maintenance_proof = decode_proof(&bytes(capture, "maintenance_proof_path"))
                    .expect("native maintenance path");
                verify_receipt(
                    &maintenance,
                    &maintenance_proof,
                    &header,
                    &signature,
                    &authorization,
                )
                .expect("real maintenance inclusion");
                let record = layerx_wire::batch_maintenance::decode_maintenance(&maintenance)
                    .expect("native maintenance record");
                record
                    .verify_header(signed)
                    .expect("signed maintenance binding");
                let decoded =
                    layerx_wire::receipt::decode(&receipt).expect("native receipt canonical bytes");
                let protocol = decoded.protocol().expect("native protocol receipt");
                let count = signed
                    .last_sequence()
                    .checked_sub(signed.first_sequence())
                    .and_then(|value| u32::try_from(value).ok())
                    .expect("bounded ordinary receipt count");
                let batch_id = layerx_wire::hash::receipt_execution_batch_id_maintenance(
                    protocol,
                    signed,
                    record.occupancy(),
                    count,
                )
                .expect("native execution batch identity");
                assert_eq!(batch_id, protocol.batch_id());
                let batch = AuthorizedBatch::new(
                    batch_id,
                    protocol.asset(),
                    signed.previous_state_root(),
                    signed.resulting_state_root(),
                    authorization.public_key(),
                );
                let paths = capture["receipts"]
                    .as_array()
                    .expect("complete genuine receipt chain");
                assert_eq!(paths.len(), usize::try_from(count).expect("receipt count"));
                assert!(!paths.is_empty() && paths.len() <= 64);
                let receipts = paths
                    .iter()
                    .map(|path| {
                        fs::read(path.as_str().expect("native receipt path"))
                            .expect("ordinary native receipt bytes")
                    })
                    .collect::<Vec<_>>();
                let evidence = MaintainedOutcomeEvidence {
                    header: &header,
                    header_signature: &signature,
                    activity_proof: &proof,
                    maintenance: &maintenance,
                    maintenance_proof: &maintenance_proof,
                    authorization: &authorization,
                };
                let preexecution_rejection = protocol.module_id() == 9
                    && protocol.operation() == 3
                    && protocol.result_code() != 0
                    && protocol.program_outcome().is_none();
                let verified = if preexecution_rejection {
                    assert!(verify_outcome_maintained_chain(
                        &receipt, &batch, &evidence, &receipts
                    )
                    .is_err());
                    verify_program_preexecution_rejection_maintained_chain(
                        &receipt, &batch, &evidence, &receipts,
                    )
                    .expect("genuine closed pre-execution rejection maintained verification")
                } else {
                    verify_outcome_maintained_chain(&receipt, &batch, &evidence, &receipts)
                        .expect("genuine maintained native receipt verification")
                };
                let mut omitted = receipts.clone();
                omitted.pop();
                if preexecution_rejection {
                    assert!(verify_program_preexecution_rejection_maintained_chain(
                        &receipt, &batch, &evidence, &omitted
                    )
                    .is_err());
                } else {
                    assert!(
                        verify_outcome_maintained_chain(&receipt, &batch, &evidence, &omitted)
                            .is_err()
                    );
                }
                let mut bad_signature = signature;
                bad_signature[0] ^= 1;
                assert!(verify_header(&header, &bad_signature, &authorization).is_err());
                Capture {
                    name: string(capture, "name").to_owned(),
                    v3: bytes(capture, "v3_path"),
                    v2: bytes(capture, "v2_path"),
                    receipt: verified,
                }
            })
            .collect::<Vec<_>>();
        assert!(
            captures.len() >= 4,
            "serial, scheduled and terminal native captures required"
        );
        assert!(captures
            .iter()
            .any(|capture| capture.name.starts_with("serial-empty")));
        assert!(captures
            .iter()
            .any(|capture| capture.name.starts_with("scheduled")));
        assert!(captures
            .iter()
            .any(|capture| capture.name.starts_with("terminal")));
        Self { network, captures }
    }
}

use std::path::PathBuf;
use std::time::Duration;

use layerx_client::arbiter_admission::{
    AdmissionError, AdmissionProgress, ADMISSION_PRESTATE_REQUEST_BYTES,
};
use layerx_client::client::{ClientConfig, ConnectionError, ReconnectPolicy};
use layerx_client::lni::handshake::{perform_with_schema, HandshakeConfig, HandshakeError};
use layerx_client::lni::refusal::decode_core_refusal;
use layerx_client::lni::schema::{
    decode_envelope_with_schema, encode_envelope_with_schema, lni_schema_arbiter_admission_v3,
    lni_schema_v1, Capability, Envelope, Version,
};
use layerx_client::lni::transport::{ConnectionGate, FrameTransport, Limits, Uds};
use layerx_client::Client;

fn config(endpoint: &str, network: u32, version: Version) -> ClientConfig {
    ClientConfig {
        endpoint: PathBuf::from(endpoint),
        handshake: HandshakeConfig {
            built_interface_version: version,
            expected_protocol_version: 3,
            expected_network_id: network,
        },
        limits: Limits {
            maximum_frame_bytes: 512,
            maximum_connections: 1,
            maximum_streams: 1,
            maximum_queued_bytes: 4096,
            deadline: Duration::from_secs(5),
        },
        reconnect: ReconnectPolicy {
            maximum_attempts: 1,
            base_delay: Duration::from_millis(1),
            maximum_delay: Duration::from_millis(1),
            jitter_percent: 0,
        },
    }
}

fn typed_refusal(endpoint: &str, fixture: &Fixture, request: &[u8]) {
    let configured = config(endpoint, fixture.network, Version::V1_11);
    let gate = ConnectionGate::new(1);
    let mut transport = Uds::connect(&configured.endpoint, &gate, configured.limits)
        .expect("real peer-authorized native UDS");
    let schema = lni_schema_arbiter_admission_v3();
    let handshake = perform_with_schema(&mut transport, &configured.handshake, None, schema)
        .expect("actual native handshake");
    assert!(handshake
        .capabilities()
        .contains(Capability::ArbiterAdmissionV3));
    let frame = encode_envelope_with_schema(
        Envelope {
            version: Version::V1_11,
            message_tag: 48,
            correlation_id: 99,
            canonical_payload: request,
            proof_material: &[],
        },
        schema,
    )
    .expect("schema-qualified real request");
    transport
        .send(&frame)
        .expect("send actual malformed selector");
    let response = transport.receive().expect("native typed refusal frame");
    let envelope = decode_envelope_with_schema(&response, schema).expect("native refusal envelope");
    assert_eq!(envelope.version, Version::V1_11);
    assert_eq!(envelope.correlation_id, 99);
    assert_eq!(envelope.message_tag, 25);
    assert!(envelope.proof_material.is_empty());
    assert!(decode_core_refusal(envelope.canonical_payload).is_some());
}

#[test]
fn real_native_arbiter_admission_transport() {
    let phase = std::env::var("LAYERX_ARBITER_ADMISSION_PHASE")
        .expect("before or after genuine evidence-store reopen");
    assert!(phase == "before" || phase == "after");
    let path = std::env::var("LAYERX_ARBITER_ADMISSION_INPUTS").expect("native transport manifest");
    let manifest: Value = serde_json::from_slice(&fs::read(path).expect("native manifest"))
        .expect("native manifest JSON");
    let endpoint = string(&manifest, "endpoint");
    let fixture = Fixture::load();
    assert_eq!(lni_schema_v1().version, Version::V1_9);
    assert_eq!(lni_schema_v1().messages.len(), 45);
    assert!(!lni_schema_v1()
        .capabilities
        .contains(&Capability::ArbiterAdmissionV3));
    assert_eq!(
        layerx_client::lni::schema::lni_schema_arbiter_prestate_v2().version,
        Version::V1_10
    );
    assert_eq!(
        layerx_client::lni::schema::lni_schema_arbiter_prestate_v2()
            .messages
            .len(),
        47
    );
    assert!(
        !layerx_client::lni::schema::lni_schema_arbiter_prestate_v2()
            .capabilities
            .contains(&Capability::ArbiterAdmissionV3)
    );
    let mut legacy = Client::connect(config(endpoint, fixture.network, Version::V1_9))
        .expect("legacy native handshake remains available");
    assert!(!legacy
        .handshake()
        .capabilities()
        .contains(Capability::ArbiterAdmissionV3));
    assert!(matches!(
        legacy.arbiter_admission_v3(&fixture.captures[0].receipt, 1),
        Err(AdmissionError::Unavailable)
    ));
    drop(legacy);
    let wrong_network = fixture
        .network
        .checked_add(1)
        .expect("different real network");
    assert!(matches!(
        Client::connect_arbiter_admission_v3(config(endpoint, wrong_network, Version::V1_11)),
        Err(ConnectionError::Handshake(HandshakeError::Network { .. }))
    ));
    let mut client =
        Client::connect_arbiter_admission_v3(config(endpoint, fixture.network, Version::V1_11))
            .expect("actual native opt-in transport");
    assert!(client
        .handshake()
        .capabilities()
        .contains(Capability::ArbiterAdmissionV3));
    for (index, capture) in fixture.captures.iter().enumerate() {
        let correlation = u64::try_from(index).expect("capture count") + 1;
        let mut discovery = client
            .start_arbiter_admission_v3(&capture.receipt, correlation)
            .expect("receipt-bound genuine historical selection");
        let mut pages = 0;
        let checked = loop {
            match discovery.advance() {
                AdmissionProgress::Incomplete {
                    received_bytes,
                    total_bytes,
                } => {
                    assert!(received_bytes > 0 && received_bytes < total_bytes);
                    assert_eq!(
                        usize::try_from(total_bytes).expect("bounded total"),
                        capture.v3.len()
                    );
                    pages += 1;
                }
                AdmissionProgress::Complete(prestate) => break prestate,
                other => panic!("genuine native page refused: {other:?}"),
            }
        };
        assert!(
            pages > 0,
            "small-frame real client must exercise continuation"
        );
        assert_eq!(checked.canonical_bytes(), capture.v3);
        assert_eq!(checked.legacy().canonical_bytes(), capture.v2);
        let closed = verify_arbiter_admission_v3(&capture.v3, &capture.receipt, fixture.network)
            .expect("closed verifier on native kernel bytes");
        assert_eq!(checked.commitment(), closed.commitment());
        assert_eq!(checked.activity_id(), closed.activity_id());
        assert_eq!(checked.receipt_digest(), closed.receipt_digest());
        assert_eq!(checked.execution_sequence(), closed.execution_sequence());
        assert_eq!(checked.state_root(), closed.state_root());
        assert!(matches!(
            discovery.advance(),
            AdmissionProgress::Refused(AdmissionError::Terminal)
        ));
        drop(discovery);
        let signed_activity = checked.activity();
        assert_eq!(
            layerx_wire::hash::activity_id(signed_activity).expect("real activity binding"),
            checked.activity_id()
        );
        let v2_end = 6 + usize::try_from(u32::from_be_bytes(
            capture.v3[2..6].try_into().expect("v2 length"),
        ))
        .expect("bounded v2");
        let activity_end = v2_end
            + 4
            + usize::try_from(u32::from_be_bytes(
                capture.v3[v2_end..v2_end + 4]
                    .try_into()
                    .expect("activity length"),
            ))
            .expect("bounded activity");
        assert_eq!(
            &capture.v3[activity_end..activity_end + 2],
            &3_u16.to_be_bytes()
        );
        let mut inventory_offset = activity_end + 2;
        for expected_module in [1_u16, 3, 7] {
            let start = inventory_offset;
            assert_eq!(
                &capture.v3[start..start + 2],
                &expected_module.to_be_bytes()
            );
            let depth = usize::from(capture.v3[start + 42]);
            let count_offset = start + 43 + depth * 32;
            let count = usize::try_from(u32::from_be_bytes(
                capture.v3[count_offset..count_offset + 4]
                    .try_into()
                    .expect("count"),
            ))
            .expect("bounded count");
            inventory_offset = count_offset + 4;
            let first_leaf = inventory_offset;
            let mut first_leaf_end = first_leaf;
            let mut leaves = Vec::with_capacity(count);
            for index in 0..count {
                let length = usize::try_from(u32::from_be_bytes(
                    capture.v3[inventory_offset..inventory_offset + 4]
                        .try_into()
                        .expect("witness"),
                ))
                .expect("bounded witness");
                leaves.push(
                    StateWitness::decode(
                        &capture.v3[inventory_offset + 4..inventory_offset + 4 + length],
                    )
                    .expect("genuine canonical module leaf"),
                );
                inventory_offset += 4 + length;
                if index == 0 {
                    first_leaf_end = inventory_offset;
                }
            }
            let module = ModuleRangeWitness {
                module_id: expected_module,
                subtree_root: capture.v3[start + 2..start + 34]
                    .try_into()
                    .expect("subtree root"),
                composite_index: u32::from_be_bytes(
                    capture.v3[start + 34..start + 38]
                        .try_into()
                        .expect("module index"),
                ),
                composite_count: u32::from_be_bytes(
                    capture.v3[start + 38..start + 42]
                        .try_into()
                        .expect("module count"),
                ),
                composite_siblings: capture.v3[start + 43..count_offset]
                    .chunks_exact(32)
                    .map(|bytes| bytes.try_into().expect("composite sibling"))
                    .collect(),
                leaves,
            };
            assert_eq!(
                module.verify_prefix(checked.state_root(), b""),
                Err(RangeError::Bounds)
            );
            let inventory = module
                .verify_full_module(checked.state_root())
                .expect("complete native module inventory");
            assert_eq!(inventory.module_id(), expected_module);
            assert_eq!(inventory.state_root(), checked.state_root());
            assert_eq!(inventory.records().len(), count);
            assert_eq!(inventory.is_empty(), count == 0);
            let records: std::collections::BTreeMap<_, _> =
                inventory.records().iter().cloned().collect();
            assert_eq!(
                &records,
                match expected_module {
                    1 => checked.asset_records(),
                    3 => checked.budget_records(),
                    7 => checked.governance_records(),
                    _ => unreachable!("fixed native module inventory"),
                }
            );
            let mut changed_module = module.clone();
            changed_module.subtree_root[0] ^= 1;
            assert!(changed_module
                .verify_full_module(checked.state_root())
                .is_err());
            let mut changed_module = module.clone();
            changed_module.composite_index ^= 1;
            assert!(changed_module
                .verify_full_module(checked.state_root())
                .is_err());
            let mut changed_module = module.clone();
            changed_module.composite_count = 0;
            assert!(changed_module
                .verify_full_module(checked.state_root())
                .is_err());
            let mut changed_module = module.clone();
            changed_module.composite_siblings.clear();
            assert!(changed_module
                .verify_full_module(checked.state_root())
                .is_err());
            if let Some(first) = module.leaves.first() {
                assert!(module
                    .verify_prefix(checked.state_root(), &first.key)
                    .is_ok());
                let mut changed_module = module.clone();
                changed_module.leaves[0].leaf_index_a = u32::MAX;
                assert_eq!(
                    changed_module.verify_full_module(checked.state_root()),
                    Err(RangeError::Position)
                );
                let mut changed_module = module.clone();
                changed_module.leaves[0].leaf_count_a = 0;
                assert_eq!(
                    changed_module.verify_full_module(checked.state_root()),
                    Err(RangeError::Position)
                );
                let mut changed_module = module.clone();
                changed_module.leaves[0].leaf_count_b = 0;
                assert_eq!(
                    changed_module.verify_full_module(checked.state_root()),
                    Err(RangeError::Position)
                );
                let mut changed_module = module.clone();
                changed_module.leaves[0].key.clear();
                assert!(changed_module
                    .verify_full_module(checked.state_root())
                    .is_err());
                let mut changed_module = module.clone();
                changed_module.leaves = vec![first.clone(); MAX_MODULE_LEAVES + 1];
                assert_eq!(
                    changed_module.verify_full_module(checked.state_root()),
                    Err(RangeError::Bounds)
                );
            } else {
                assert!(module
                    .verify_prefix(checked.state_root(), b"existing-prefix")
                    .expect("genuine empty module absence")
                    .is_empty());
            }
            if count > 0 {
                let mut missing_leaf = capture.v3.clone();
                missing_leaf.drain(first_leaf..first_leaf_end);
                missing_leaf[count_offset..count_offset + 4].copy_from_slice(
                    &u32::try_from(count - 1)
                        .expect("bounded count")
                        .to_be_bytes(),
                );
                assert!(verify_arbiter_admission_v3(
                    &missing_leaf,
                    &capture.receipt,
                    fixture.network
                )
                .is_err());
                let mut duplicated = capture.v3.clone();
                duplicated.splice(
                    first_leaf_end..first_leaf_end,
                    capture.v3[first_leaf..first_leaf_end].iter().copied(),
                );
                duplicated[count_offset..count_offset + 4].copy_from_slice(
                    &u32::try_from(count + 1)
                        .expect("bounded count")
                        .to_be_bytes(),
                );
                assert!(verify_arbiter_admission_v3(
                    &duplicated,
                    &capture.receipt,
                    fixture.network
                )
                .is_err());
            }
            let mut omitted = capture.v3.clone();
            omitted.drain(start..inventory_offset);
            assert!(
                verify_arbiter_admission_v3(&omitted, &capture.receipt, fixture.network).is_err()
            );
            let mut wrong_module = capture.v3.clone();
            wrong_module[start + 1] ^= 1;
            assert!(
                verify_arbiter_admission_v3(&wrong_module, &capture.receipt, fixture.network)
                    .is_err()
            );
            let mut wrong_root = capture.v3.clone();
            wrong_root[start + 2] ^= 1;
            assert!(
                verify_arbiter_admission_v3(&wrong_root, &capture.receipt, fixture.network)
                    .is_err()
            );
        }
        assert_eq!(inventory_offset, capture.v3.len());
        for offset in [v2_end + 4, activity_end - 1] {
            let mut changed = capture.v3.clone();
            changed[offset] ^= 1;
            assert!(
                verify_arbiter_admission_v3(&changed, &capture.receipt, fixture.network).is_err()
            );
        }
        for other in &fixture.captures {
            if other.receipt.canonical_bytes() != capture.receipt.canonical_bytes() {
                assert!(
                    verify_arbiter_admission_v3(&capture.v3, &other.receipt, fixture.network)
                        .is_err()
                );
            }
        }
        for cut in [
            0,
            1,
            5,
            v2_end,
            v2_end + 3,
            activity_end,
            activity_end + 1,
            capture.v3.len() - 1,
        ] {
            assert!(verify_arbiter_admission_v3(
                &capture.v3[..cut],
                &capture.receipt,
                fixture.network
            )
            .is_err());
        }
        let mut trailing = capture.v3.clone();
        trailing.push(0);
        assert!(verify_arbiter_admission_v3(&trailing, &capture.receipt, fixture.network).is_err());
        let mut changed = capture.v3.clone();
        changed[52] ^= 1;
        assert!(verify_arbiter_admission_v3(&changed, &capture.receipt, fixture.network).is_err());
        assert!(verify_arbiter_admission_v3_bounded(
            &capture.v3,
            &capture.receipt,
            fixture.network,
            capture.v3.len() - 1
        )
        .is_err());
        assert!(verify_arbiter_admission_v3_bounded(
            &capture.v3,
            &capture.receipt,
            fixture.network,
            MAX_ADMISSION_PRESTATE_BYTES + 1
        )
        .is_err());
    }
    client
        .reconnect()
        .expect("real reconnect retains opted-in schema and authority");
    let reopened = client
        .arbiter_admission_v3(&fixture.captures[0].receipt, 88)
        .expect("real receipt-bound query after reconnect");
    assert_eq!(reopened.canonical_bytes(), fixture.captures[0].v3);
    drop(client);
    let capture = &fixture.captures[0];
    let protocol = capture
        .receipt
        .receipt()
        .protocol()
        .expect("native protocol");
    let unsigned = layerx_wire::receipt::encode_unsigned(capture.receipt.receipt())
        .expect("canonical receipt");
    let digest = layerx_wire::hash::receipt_digest(&unsigned).expect("native receipt digest");
    let mut request = [0; ADMISSION_PRESTATE_REQUEST_BYTES];
    request[..2].copy_from_slice(&3_u16.to_be_bytes());
    request[3..7].copy_from_slice(&fixture.network.to_be_bytes());
    request[7..39].copy_from_slice(&protocol.activity_id());
    request[39..71].copy_from_slice(&digest);
    request[71..79].copy_from_slice(&protocol.global_sequence().to_be_bytes());
    request[79..111].copy_from_slice(&protocol.previous_state_root());
    request[111..115].copy_from_slice(&299_u32.to_be_bytes());
    for offset in [3, 7, 39, 71, 79, 147, 183] {
        let mut bad = request;
        bad[offset] ^= 1;
        typed_refusal(endpoint, &fixture, &bad);
    }
    let mut oversized = request;
    oversized[111..115].copy_from_slice(&1_048_577_u32.to_be_bytes());
    typed_refusal(endpoint, &fixture, &oversized);
    typed_refusal(endpoint, &fixture, &request[..request.len() - 1]);
}
