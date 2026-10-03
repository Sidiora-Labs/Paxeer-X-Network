use std::fs;

use layerx_client::evidence::{
    verify_arbiter_prestate_v2, verify_arbiter_prestate_v2_bounded,
    verify_native_execution_prestate_object, MAX_ARBITER_PRESTATE_BYTES,
};
use layerx_proof::inclusion::{verify_header, verify_receipt, SequencerAuthorization};
use layerx_proof::merkle::decode_proof;
use layerx_proof::receipt::{
    verify_outcome_maintained_chain, AuthorizedBatch, MaintainedOutcomeEvidence, VerifiedReceipt,
};
use serde_json::Value;
use sha2::{Digest, Sha256};

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
        let path = std::env::var("LAYERX_ARBITER_PRESTATE_INPUTS")
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
                let verified =
                    verify_outcome_maintained_chain(&receipt, &batch, &evidence, &receipts)
                        .expect("genuine maintained native receipt verification");
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
            .any(|capture| capture.name == "serial-empty"));
        assert!(captures
            .iter()
            .any(|capture| capture.name.starts_with("scheduled")));
        assert!(captures
            .iter()
            .any(|capture| capture.name.starts_with("terminal")));
        Self { network, captures }
    }
}

#[derive(Clone)]
struct Module {
    start: usize,
    end: usize,
    depth: usize,
    count: usize,
    leaves: Vec<(usize, usize)>,
}

fn u32_at(bytes: &[u8], offset: usize) -> usize {
    usize::try_from(u32::from_be_bytes(
        bytes[offset..offset + 4].try_into().expect("native u32"),
    ))
    .expect("native length")
}

fn layout(bytes: &[u8]) -> (usize, [Module; 2]) {
    let legacy_end = 6 + u32_at(bytes, 2);
    let mut offset = legacy_end + 2;
    let modules = std::array::from_fn(|_| {
        let start = offset;
        let depth = usize::from(bytes[start + 42]);
        let count = start + 43 + depth * 32;
        offset = count + 4;
        let leaves = (0..u32_at(bytes, count))
            .map(|_| {
                let start = offset;
                offset += 4 + u32_at(bytes, offset);
                (start, offset)
            })
            .collect();
        Module {
            start,
            end: offset,
            depth,
            count,
            leaves,
        }
    });
    assert_eq!(offset, bytes.len());
    (legacy_end, modules)
}

fn refuses(bytes: &[u8], capture: &Capture, network: u32) {
    assert!(verify_arbiter_prestate_v2(bytes, &capture.receipt, network).is_err());
}

#[test]
fn native_serial_scheduled_terminal_receipt_binding_and_legacy_bytes() {
    let fixture = Fixture::load();
    for capture in &fixture.captures {
        let checked = verify_arbiter_prestate_v2(&capture.v2, &capture.receipt, fixture.network)
            .expect("genuine native v2 at each actual apply boundary");
        assert_eq!(checked.legacy().canonical_bytes(), capture.v1);
        let legacy =
            verify_native_execution_prestate_object(&capture.v1, &capture.receipt, fixture.network)
                .expect("unchanged closed v1 verifier");
        assert_eq!(checked.state_root(), legacy.state_root());
        assert_eq!(checked.receipt_digest(), legacy.receipt_digest());
        assert_eq!(checked.activity_id(), legacy.activity_id());
        assert_eq!(checked.execution_sequence(), legacy.execution_sequence());
        assert_eq!(checked.network_id(), fixture.network);
        assert_eq!(checked.canonical_bytes(), capture.v2);
        let mut hasher = Sha256::new();
        hasher.update(b"LayerX/programs/arbiter-prestate/v2\0");
        hasher.update(&capture.v2);
        let digest: [u8; 32] = hasher.finalize().into();
        assert_eq!(checked.commitment(), digest);
        refuses(&capture.v2, capture, 0);
        refuses(
            &capture.v2,
            capture,
            fixture.network.checked_add(1).expect("network increment"),
        );
        assert!(verify_native_execution_prestate_object(
            &capture.v2,
            &capture.receipt,
            fixture.network
        )
        .is_err());
        for other in &fixture.captures {
            if other.receipt.canonical_bytes() != capture.receipt.canonical_bytes() {
                assert!(
                    verify_arbiter_prestate_v2(&capture.v2, &other.receipt, fixture.network)
                        .is_err()
                );
            }
        }
        for offset in [6 + 2, 6 + 6, 6 + 38, 6 + 46] {
            let mut changed = capture.v2.clone();
            changed[offset] ^= 1;
            refuses(&changed, capture, fixture.network);
        }
        let protocol = capture
            .receipt
            .receipt()
            .protocol()
            .expect("native protocol");
        if protocol.resulting_state_root() != protocol.previous_state_root() {
            let mut poststate = capture.v2.clone();
            poststate[52..84].copy_from_slice(&protocol.resulting_state_root());
            refuses(&poststate, capture, fixture.network);
        }
    }
}

#[test]
fn complete_inventories_authenticate_empty_modules_and_absence() {
    let fixture = Fixture::load();
    let empty = fixture
        .captures
        .iter()
        .find(|capture| capture.name == "serial-empty")
        .expect("real empty module snapshot");
    let checked = verify_arbiter_prestate_v2(&empty.v2, &empty.receipt, fixture.network)
        .expect("authenticated empty ranges");
    assert!(checked.perps_records().is_empty());
    assert!(checked.web_records().is_empty());
    assert!(checked
        .perps_records()
        .get(b"oracle:absent".as_slice())
        .is_none());
    assert!(checked
        .web_records()
        .get(b"web/answer/absent".as_slice())
        .is_none());
    let (_, empty_modules) = layout(&empty.v2);
    for capture in fixture
        .captures
        .iter()
        .filter(|capture| capture.name.starts_with("scheduled"))
    {
        let verified = verify_arbiter_prestate_v2(&capture.v2, &capture.receipt, fixture.network)
            .expect("real populated modules");
        assert!(!verified.perps_records().is_empty());
        assert!(!verified.web_records().is_empty());
        let (_, modules) = layout(&capture.v2);
        for index in 0..2 {
            let mut substituted = capture.v2.clone();
            substituted.splice(
                modules[index].start..modules[index].end,
                empty.v2[empty_modules[index].start..empty_modules[index].end]
                    .iter()
                    .copied(),
            );
            refuses(&substituted, capture, fixture.network);
        }
    }
}

#[test]
fn module_and_leaf_omission_reordering_duplication_and_proof_tampering_refuse() {
    let fixture = Fixture::load();
    for capture in &fixture.captures {
        let (legacy_end, modules) = layout(&capture.v2);
        let mut reordered = capture.v2[..legacy_end + 2].to_vec();
        reordered.extend_from_slice(&capture.v2[modules[1].start..modules[1].end]);
        reordered.extend_from_slice(&capture.v2[modules[0].start..modules[0].end]);
        refuses(&reordered, capture, fixture.network);
        for (index, module) in modules.iter().enumerate() {
            let mut duplicate = capture.v2.clone();
            duplicate.splice(
                module.start..module.end,
                capture.v2[modules[1 - index].start..modules[1 - index].end]
                    .iter()
                    .copied(),
            );
            refuses(&duplicate, capture, fixture.network);
            let mut omitted = capture.v2.clone();
            omitted.drain(module.start..module.end);
            refuses(&omitted, capture, fixture.network);
            for offset in [
                module.start,
                module.start + 2,
                module.start + 34,
                module.start + 38,
            ] {
                let mut changed = capture.v2.clone();
                changed[offset] ^= 1;
                refuses(&changed, capture, fixture.network);
            }
            if module.depth != 0 {
                let mut path = capture.v2.clone();
                path[module.start + 43] ^= 1;
                refuses(&path, capture, fixture.network);
            }
            if let Some(&(start, end)) = module.leaves.first() {
                let mut omitted = capture.v2.clone();
                omitted.drain(start..end);
                omitted[module.count..module.count + 4].copy_from_slice(
                    &u32::try_from(module.leaves.len() - 1)
                        .expect("leaf count")
                        .to_be_bytes(),
                );
                refuses(&omitted, capture, fixture.network);
                let mut duplicated = capture.v2.clone();
                duplicated.splice(end..end, capture.v2[start..end].iter().copied());
                duplicated[module.count..module.count + 4].copy_from_slice(
                    &u32::try_from(module.leaves.len() + 1)
                        .expect("leaf count")
                        .to_be_bytes(),
                );
                refuses(&duplicated, capture, fixture.network);
                let key_length = u32_at(&capture.v2, start + 8);
                let value_length_offset = start + 12 + key_length;
                let value_length = u32_at(&capture.v2, value_length_offset);
                assert!(
                    value_length > 0,
                    "native fixture first module leaf has real value"
                );
                let mut changed_value = capture.v2.clone();
                changed_value[value_length_offset + 4] ^= 1;
                refuses(&changed_value, capture, fixture.network);
                let mut oversized_key = capture.v2.clone();
                oversized_key[start + 8..start + 12].copy_from_slice(&130_u32.to_be_bytes());
                refuses(&oversized_key, capture, fixture.network);
                let mut oversized_value = capture.v2.clone();
                oversized_value[value_length_offset..value_length_offset + 4]
                    .copy_from_slice(&1_048_577_u32.to_be_bytes());
                refuses(&oversized_value, capture, fixture.network);
                let mut bad_witness_version = capture.v2.clone();
                bad_witness_version[start + 4..start + 6].copy_from_slice(&1_u16.to_be_bytes());
                refuses(&bad_witness_version, capture, fixture.network);
                let mut oversized_witness = capture.v2.clone();
                oversized_witness[start..start + 4].copy_from_slice(&u32::MAX.to_be_bytes());
                refuses(&oversized_witness, capture, fixture.network);
            }
            if module.leaves.len() >= 2 {
                let (first_start, first_end) = module.leaves[0];
                let (second_start, second_end) = module.leaves[1];
                let mut changed = capture.v2.clone();
                let mut swapped = capture.v2[second_start..second_end].to_vec();
                swapped.extend_from_slice(&capture.v2[first_start..first_end]);
                changed.splice(first_start..second_end, swapped);
                refuses(&changed, capture, fixture.network);
            }
        }
    }
}

#[test]
fn canonical_profile_truncation_trailing_overflow_and_all_outer_bounds_refuse() {
    let fixture = Fixture::load();
    for capture in &fixture.captures {
        let (legacy_end, modules) = layout(&capture.v2);
        let mut cuts = vec![
            0,
            1,
            2,
            5,
            6,
            legacy_end - 1,
            legacy_end,
            legacy_end + 1,
            capture.v2.len() - 1,
        ];
        for module in &modules {
            cuts.extend([
                module.start,
                module.start + 41,
                module.count,
                module.count + 3,
            ]);
            for &(start, end) in &module.leaves {
                cuts.extend([start + 3, start + 4, end - 1]);
            }
        }
        for cut in cuts {
            refuses(&capture.v2[..cut], capture, fixture.network);
        }
        let mut trailing = capture.v2.clone();
        trailing.push(0);
        refuses(&trailing, capture, fixture.network);
        let mut version = capture.v2.clone();
        version[..2].copy_from_slice(&1_u16.to_be_bytes());
        refuses(&version, capture, fixture.network);
        let mut oversized = capture.v2.clone();
        oversized[2..6].copy_from_slice(&u32::MAX.to_be_bytes());
        refuses(&oversized, capture, fixture.network);
        for count in [0_u16, 1, 3, u16::MAX] {
            let mut changed = capture.v2.clone();
            changed[legacy_end..legacy_end + 2].copy_from_slice(&count.to_be_bytes());
            refuses(&changed, capture, fixture.network);
        }
        for module in &modules {
            let mut depth = capture.v2.clone();
            depth[module.start + 42] = 33;
            refuses(&depth, capture, fixture.network);
            let mut count = capture.v2.clone();
            count[module.count..module.count + 4].copy_from_slice(&1025_u32.to_be_bytes());
            refuses(&count, capture, fixture.network);
        }
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
        assert!(verify_arbiter_prestate_v2_bounded(
            &capture.v2,
            &capture.receipt,
            fixture.network,
            capture.v2.len()
        )
        .is_ok());
    }
    let oversized = vec![0; MAX_ARBITER_PRESTATE_BYTES + 1];
    refuses(&oversized, &fixture.captures[0], fixture.network);
}
