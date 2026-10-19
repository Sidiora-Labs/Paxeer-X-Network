use std::fs;

use layerx_client::evidence::{
    verify_arbiter_prestate_v2, verify_arbiter_prestate_v2_bounded, MAX_ARBITER_PRESTATE_BYTES,
};
use layerx_proof::inclusion::{verify_header, verify_receipt, SequencerAuthorization};
use layerx_proof::merkle::decode_proof;
use layerx_proof::receipt::{
    verify_outcome_maintained_chain, verify_program_preexecution_rejection_maintained_chain,
    AuthorizedBatch, MaintainedOutcomeEvidence, VerifiedReceipt,
};
use serde_json::Value;

struct Capture {
    name: String,
    v2: Vec<u8>,
    v1: Vec<u8>,
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
        let path = std::env::var("LAYERX_ARBITER_TRANSPORT_INPUTS")
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
                    v2: bytes(capture, "v2_path"),
                    v1: bytes(capture, "v1_path"),
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

use layerx_client::arbiter_prestate::{
    ArbiterPrestateError, ArbiterPrestateProgress, ARBITER_PRESTATE_REQUEST_BYTES,
};
use layerx_client::client::{ClientConfig, ConnectionError, ReconnectPolicy};
use layerx_client::lni::handshake::{perform_with_schema, HandshakeConfig, HandshakeError};
use layerx_client::lni::refusal::decode_core_refusal;
use layerx_client::lni::schema::{
    decode_envelope_with_schema, encode_envelope_with_schema, lni_schema_arbiter_prestate_v2,
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
    let configured = config(endpoint, fixture.network, Version::V1_10);
    let gate = ConnectionGate::new(1);
    let mut transport = Uds::connect(&configured.endpoint, &gate, configured.limits)
        .expect("real peer-authorized native UDS");
    let schema = lni_schema_arbiter_prestate_v2();
    let handshake = perform_with_schema(&mut transport, &configured.handshake, None, schema)
        .expect("actual native handshake");
    assert!(handshake
        .capabilities()
        .contains(Capability::ArbiterPrestateV2));
    let frame = encode_envelope_with_schema(
        Envelope {
            version: Version::V1_10,
            message_tag: 46,
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
    assert_eq!(envelope.version, Version::V1_10);
    assert_eq!(envelope.correlation_id, 99);
    assert_eq!(envelope.message_tag, 25);
    assert!(envelope.proof_material.is_empty());
    assert!(decode_core_refusal(envelope.canonical_payload).is_some());
}

#[test]
fn real_native_arbiter_prestate_transport() {
    let phase = std::env::var("LAYERX_ARBITER_TRANSPORT_PHASE")
        .expect("before or after genuine evidence-store reopen");
    assert!(phase == "before" || phase == "after");
    let path = std::env::var("LAYERX_ARBITER_TRANSPORT_INPUTS").expect("native transport manifest");
    let manifest: Value = serde_json::from_slice(&fs::read(path).expect("native manifest"))
        .expect("native manifest JSON");
    let endpoint = string(&manifest, "endpoint");
    let fixture = Fixture::load();
    assert_eq!(lni_schema_v1().version, Version::V1_9);
    assert_eq!(lni_schema_v1().messages.len(), 45);
    assert!(!lni_schema_v1()
        .capabilities
        .contains(&Capability::ArbiterPrestateV2));
    let mut legacy = Client::connect(config(endpoint, fixture.network, Version::V1_9))
        .expect("legacy native handshake remains available");
    assert!(!legacy
        .handshake()
        .capabilities()
        .contains(Capability::ArbiterPrestateV2));
    assert!(matches!(
        legacy.arbiter_prestate_v2(&fixture.captures[0].receipt, 1),
        Err(ArbiterPrestateError::Unavailable)
    ));
    drop(legacy);
    let wrong_network = fixture
        .network
        .checked_add(1)
        .expect("different real network");
    assert!(matches!(
        Client::connect_arbiter_prestate_v2(config(endpoint, wrong_network, Version::V1_10)),
        Err(ConnectionError::Handshake(HandshakeError::Network { .. }))
    ));
    let mut client =
        Client::connect_arbiter_prestate_v2(config(endpoint, fixture.network, Version::V1_10))
            .expect("actual native opt-in transport");
    assert!(client
        .handshake()
        .capabilities()
        .contains(Capability::ArbiterPrestateV2));
    for (index, capture) in fixture.captures.iter().enumerate() {
        let correlation = u64::try_from(index).expect("capture count") + 1;
        let mut discovery = client
            .start_arbiter_prestate_v2(&capture.receipt, correlation)
            .expect("receipt-bound genuine historical selection");
        let mut pages = 0;
        let checked = loop {
            match discovery.advance() {
                ArbiterPrestateProgress::Incomplete {
                    received_bytes,
                    total_bytes,
                } => {
                    assert!(received_bytes > 0 && received_bytes < total_bytes);
                    assert_eq!(
                        usize::try_from(total_bytes).expect("bounded total"),
                        capture.v2.len()
                    );
                    pages += 1;
                }
                ArbiterPrestateProgress::Complete(prestate) => break prestate,
                other => panic!("genuine native page refused: {other:?}"),
            }
        };
        assert!(
            pages > 0,
            "small-frame real client must exercise continuation"
        );
        assert_eq!(checked.canonical_bytes(), capture.v2);
        assert_eq!(checked.legacy().canonical_bytes(), capture.v1);
        let closed = verify_arbiter_prestate_v2(&capture.v2, &capture.receipt, fixture.network)
            .expect("closed verifier on native kernel bytes");
        assert_eq!(checked.commitment(), closed.commitment());
        assert_eq!(checked.activity_id(), closed.activity_id());
        assert_eq!(checked.receipt_digest(), closed.receipt_digest());
        assert_eq!(checked.execution_sequence(), closed.execution_sequence());
        assert_eq!(checked.state_root(), closed.state_root());
        assert!(matches!(
            discovery.advance(),
            ArbiterPrestateProgress::Refused(ArbiterPrestateError::Terminal)
        ));
        drop(discovery);
        let mut changed = capture.v2.clone();
        changed[52] ^= 1;
        assert!(verify_arbiter_prestate_v2(&changed, &capture.receipt, fixture.network).is_err());
        assert!(verify_arbiter_prestate_v2_bounded(
            &capture.v2,
            &capture.receipt,
            fixture.network,
            capture.v2.len() - 1
        )
        .is_err());
        assert!(verify_arbiter_prestate_v2_bounded(
            &capture.v2,
            &capture.receipt,
            fixture.network,
            MAX_ARBITER_PRESTATE_BYTES + 1
        )
        .is_err());
    }
    client
        .reconnect()
        .expect("real reconnect retains opted-in schema and authority");
    let reopened = client
        .arbiter_prestate_v2(&fixture.captures[0].receipt, 88)
        .expect("real receipt-bound query after reconnect");
    assert_eq!(reopened.canonical_bytes(), fixture.captures[0].v2);
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
    let mut request = [0; ARBITER_PRESTATE_REQUEST_BYTES];
    request[..2].copy_from_slice(&2_u16.to_be_bytes());
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
