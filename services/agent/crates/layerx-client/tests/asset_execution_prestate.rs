use std::fs;

use layerx_proof::inclusion::{verify_header, verify_receipt, SequencerAuthorization};
use layerx_proof::merkle::decode_proof;
use layerx_proof::receipt::{
    verify_outcome_maintained_chain, AuthorizedBatch, MaintainedOutcomeEvidence, VerifiedReceipt,
};
use serde_json::Value;

struct Capture {
    name: String,
    object: Vec<u8>,
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
        let path = std::env::var("LAYERX_ASSET_EXECUTION_PRESTATE_INPUTS")
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
                assert_eq!(protocol.module_id(), 1);
                assert_eq!(protocol.operation(), 5);
                let verified =
                    verify_outcome_maintained_chain(&receipt, &batch, &evidence, &receipts)
                        .expect("genuine maintained Asset Send receipt");
                let mut omitted = receipts.clone();
                omitted.pop();
                assert!(
                    verify_outcome_maintained_chain(&receipt, &batch, &evidence, &omitted).is_err()
                );
                let mut bad_signature = signature;
                bad_signature[0] ^= 1;
                assert!(verify_header(&header, &bad_signature, &authorization).is_err());
                Capture {
                    name: string(capture, "name").to_owned(),
                    object: bytes(capture, "prestate_path"),
                    receipt: verified,
                }
            })
            .collect::<Vec<_>>();
        assert!(
            captures.len() >= 2,
            "distinct genuine native Asset executions"
        );
        Self { network, captures }
    }
}

use layerx_client::client::{ClientConfig, ConnectionError, ReconnectPolicy};
use layerx_client::evidence::{
    verify_asset_execution_prestate_object, verify_native_execution_prestate_object,
};
use layerx_client::execution_prestate::{
    AssetExecutionPrestateProgress, ExecutionPrestateError, CAPS_REQUEST_BYTES,
};
use layerx_client::lni::handshake::{perform, HandshakeConfig, HandshakeError};
use layerx_client::lni::refusal::decode_core_refusal;
use layerx_client::lni::schema::{
    decode_envelope, encode_envelope, lni_schema_v1, Capability, Envelope, Version,
};
use layerx_client::lni::transport::{ConnectionGate, FrameTransport, Limits, Uds};
use layerx_client::Client;
use std::path::PathBuf;
use std::time::Duration;

fn config(endpoint: &str, network: u32) -> ClientConfig {
    ClientConfig {
        endpoint: PathBuf::from(endpoint),
        handshake: HandshakeConfig {
            built_interface_version: Version::V1_9,
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

fn typed_refusal(endpoint: &str, network: u32, request: &[u8]) {
    let configured = config(endpoint, network);
    let gate = ConnectionGate::new(1);
    let mut transport =
        Uds::connect(&configured.endpoint, &gate, configured.limits).expect("real native UDS peer");
    let handshake =
        perform(&mut transport, &configured.handshake, None).expect("real native handshake");
    assert!(handshake
        .capabilities()
        .unknown_advertised()
        .iter()
        .any(|name| name == "asset_execution_prestate"));
    transport
        .send(
            &encode_envelope(Envelope {
                version: Version::V1_9,
                message_tag: 44,
                correlation_id: 99,
                canonical_payload: request,
                proof_material: &[],
            })
            .expect("actual selector encoding"),
        )
        .expect("send actual malformed selector");
    let response = transport.receive().expect("real native refusal");
    let envelope = decode_envelope(&response).expect("canonical refusal envelope");
    assert_eq!(envelope.version, Version::V1_9);
    assert_eq!(envelope.correlation_id, 99);
    assert_eq!(envelope.message_tag, 25);
    assert!(envelope.proof_material.is_empty());
    assert!(decode_core_refusal(envelope.canonical_payload).is_some());
}

fn number(bytes: &[u8], offset: usize) -> usize {
    usize::try_from(u32::from_be_bytes(
        bytes[offset..offset + 4].try_into().expect("native count"),
    ))
    .expect("bounded native count")
}

#[test]
fn real_native_asset_execution_prestate_transport() {
    let phase = std::env::var("LAYERX_ASSET_EXECUTION_PRESTATE_PHASE")
        .expect("real before/after reopen phase");
    assert!(phase == "before" || phase == "after");
    let path =
        std::env::var("LAYERX_ASSET_EXECUTION_PRESTATE_INPUTS").expect("real native manifest");
    let manifest: Value =
        serde_json::from_slice(&fs::read(path).expect("native manifest")).expect("manifest JSON");
    let endpoint = string(&manifest, "endpoint");
    let fixture = Fixture::load();
    assert_eq!(lni_schema_v1().version, Version::V1_9);
    assert_eq!(lni_schema_v1().messages.len(), 45);
    assert!(matches!(
        Client::connect(config(
            endpoint,
            fixture.network.checked_add(1).expect("other network")
        )),
        Err(ConnectionError::Handshake(HandshakeError::Network { .. }))
    ));
    let mut client =
        Client::connect(config(endpoint, fixture.network)).expect("real native Asset connection");
    assert!(client
        .handshake()
        .capabilities()
        .contains(Capability::CapsDiscovery));
    assert!(client
        .handshake()
        .capabilities()
        .unknown_advertised()
        .iter()
        .any(|name| name == "asset_execution_prestate"));
    let mut roots = std::collections::BTreeSet::new();
    for (index, capture) in fixture.captures.iter().enumerate() {
        let mut discovery = client
            .start_asset_execution_prestate(
                &capture.receipt,
                u64::try_from(index).expect("bounded captures") + 1,
            )
            .expect("real Asset historical selection");
        let mut pages = 0;
        let checked = loop {
            match discovery.advance() {
                AssetExecutionPrestateProgress::Incomplete {
                    received_bytes,
                    total_bytes,
                } => {
                    assert!(received_bytes > 0 && received_bytes < total_bytes);
                    assert_eq!(
                        usize::try_from(total_bytes).expect("bounded total"),
                        capture.object.len()
                    );
                    pages += 1;
                }
                AssetExecutionPrestateProgress::Complete(checked) => break checked,
                other => panic!("genuine Asset capture {} refused: {other:?}", capture.name),
            }
        };
        assert!(pages > 0, "small real client frames require continuation");
        assert_eq!(checked.canonical_bytes(), capture.object);
        assert!(matches!(
            discovery.advance(),
            AssetExecutionPrestateProgress::Refused(ExecutionPrestateError::Terminal)
        ));
        drop(discovery);
        let closed = verify_asset_execution_prestate_object(
            &capture.object,
            &capture.receipt,
            fixture.network,
        )
        .expect("closed proof verification of native bytes");
        assert_eq!(checked.all_accounts(), closed.all_accounts());
        assert_eq!(
            checked.fee_policy().canonical_bytes(),
            closed.fee_policy().canonical_bytes()
        );
        let protocol = capture
            .receipt
            .receipt()
            .protocol()
            .expect("native protocol");
        assert_eq!(checked.state_root(), protocol.previous_state_root());
        assert_eq!(checked.execution_sequence(), protocol.global_sequence());
        assert_eq!(checked.activity_id(), protocol.activity_id());
        assert_eq!(
            checked.activity().activity_type().module(),
            layerx_types::payload::ModuleId::Asset
        );
        assert_eq!(checked.activity().activity_type().ordinal(), 5);
        assert_eq!(
            checked.fee_policy().parameter_version(),
            protocol.parameter_version()
        );
        assert!(!checked.all_accounts().is_empty());
        for (id, account) in checked.all_accounts() {
            assert_eq!(*id, account.account_id);
        }
        roots.insert(checked.state_root());
        let inner_end = 6 + number(&capture.object, 2);
        assert!(
            verify_native_execution_prestate_object(
                &capture.object[6..inner_end],
                &capture.receipt,
                fixture.network
            )
            .is_err(),
            "existing Programs-only verifier must still refuse Asset"
        );
        assert!(
            client
                .native_execution_prestate(&capture.receipt, 70)
                .is_err(),
            "old Programs discovery preserves refusal"
        );
        let activity_end = inner_end + 4 + number(&capture.object, inner_end);
        assert_eq!(&capture.object[activity_end..activity_end + 2], &[0, 2]);
        let mut offset = activity_end + 2;
        for expected_module in [1_u16, 7] {
            let start = offset;
            assert_eq!(
                &capture.object[start..start + 2],
                &expected_module.to_be_bytes()
            );
            let count_offset = start + 43 + usize::from(capture.object[start + 42]) * 32;
            let count = number(&capture.object, count_offset);
            offset = count_offset + 4;
            let first_start = offset;
            let mut first_end = offset;
            for leaf in 0..count {
                offset += 4 + number(&capture.object, offset);
                if leaf == 0 {
                    first_end = offset;
                }
            }
            let mut omitted = capture.object.clone();
            omitted.drain(start..offset);
            assert!(verify_asset_execution_prestate_object(
                &omitted,
                &capture.receipt,
                fixture.network
            )
            .is_err());
            for byte in [start + 1, start + 2, start + 34, start + 38] {
                let mut changed = capture.object.clone();
                changed[byte] ^= 1;
                assert!(verify_asset_execution_prestate_object(
                    &changed,
                    &capture.receipt,
                    fixture.network
                )
                .is_err());
            }
            if count > 0 {
                let mut missing = capture.object.clone();
                missing.drain(first_start..first_end);
                missing[count_offset..count_offset + 4]
                    .copy_from_slice(&u32::try_from(count - 1).expect("count").to_be_bytes());
                assert!(verify_asset_execution_prestate_object(
                    &missing,
                    &capture.receipt,
                    fixture.network
                )
                .is_err());
                let mut duplicate = capture.object.clone();
                duplicate.splice(
                    first_end..first_end,
                    capture.object[first_start..first_end].iter().copied(),
                );
                duplicate[count_offset..count_offset + 4]
                    .copy_from_slice(&u32::try_from(count + 1).expect("count").to_be_bytes());
                assert!(verify_asset_execution_prestate_object(
                    &duplicate,
                    &capture.receipt,
                    fixture.network
                )
                .is_err());
                let mut changed = capture.object.clone();
                changed[first_end - 1] ^= 1;
                assert!(verify_asset_execution_prestate_object(
                    &changed,
                    &capture.receipt,
                    fixture.network
                )
                .is_err());
            }
        }
        assert_eq!(offset, capture.object.len());
        for byte in [6 + 6, 6 + 38, 6 + 46, inner_end + 4, activity_end - 1] {
            let mut changed = capture.object.clone();
            changed[byte] ^= 1;
            assert!(verify_asset_execution_prestate_object(
                &changed,
                &capture.receipt,
                fixture.network
            )
            .is_err());
        }
        for cut in [
            0,
            1,
            5,
            inner_end,
            inner_end + 3,
            activity_end,
            activity_end + 1,
            capture.object.len() - 1,
        ] {
            assert!(verify_asset_execution_prestate_object(
                &capture.object[..cut],
                &capture.receipt,
                fixture.network
            )
            .is_err());
        }
        let mut trailing = capture.object.clone();
        trailing.push(0);
        assert!(verify_asset_execution_prestate_object(
            &trailing,
            &capture.receipt,
            fixture.network
        )
        .is_err());
        assert!(verify_asset_execution_prestate_object(
            &capture.object,
            &capture.receipt,
            fixture.network + 1
        )
        .is_err());
        for other in &fixture.captures {
            if other.receipt.canonical_bytes() != capture.receipt.canonical_bytes() {
                assert!(
                    verify_asset_execution_prestate_object(
                        &capture.object,
                        &other.receipt,
                        fixture.network
                    )
                    .is_err(),
                    "later or unrelated receipt cannot authorize historical execution prestate"
                );
            }
        }
    }
    assert!(
        roots.len() >= 2,
        "distinct genuine executions commit distinct previous roots"
    );
    client
        .reconnect()
        .expect("real reconnect preserves native negotiation");
    assert_eq!(
        client
            .asset_execution_prestate(&fixture.captures[0].receipt, 88)
            .expect("historical capture after reconnect")
            .canonical_bytes(),
        fixture.captures[0].object
    );
    drop(client);
    let receipt = fixture.captures[0]
        .receipt
        .receipt()
        .protocol()
        .expect("native protocol");
    let unsigned = layerx_wire::receipt::encode_unsigned(fixture.captures[0].receipt.receipt())
        .expect("unsigned native receipt");
    let digest = layerx_wire::hash::receipt_digest(&unsigned).expect("canonical digest");
    let mut request = [0; CAPS_REQUEST_BYTES];
    request[..2].copy_from_slice(&3_u16.to_be_bytes());
    request[3..7].copy_from_slice(&fixture.network.to_be_bytes());
    request[7..39].copy_from_slice(&receipt.activity_id());
    request[39..71].copy_from_slice(&digest);
    request[71..75].copy_from_slice(&299_u32.to_be_bytes());
    for byte in [3, 7, 39, 139, 175] {
        let mut changed = request;
        changed[byte] ^= 1;
        typed_refusal(endpoint, fixture.network, &changed);
    }
    let mut changed = request;
    changed[71..75].copy_from_slice(&1_048_577_u32.to_be_bytes());
    typed_refusal(endpoint, fixture.network, &changed);
    typed_refusal(endpoint, fixture.network, &request[..request.len() - 1]);
}
