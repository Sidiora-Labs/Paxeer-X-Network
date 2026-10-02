use std::io::Cursor;
use std::path::PathBuf;

use layerx_client::caps::{
    progress, require_caps_discovery, selection_digest, CapsCodecError, CapsCursor, CapsOutcome,
    CapsPage, CapsRequest, CapsResponse, CapsSnapshotOpen, CapsUnavailable,
    CAPS_DISCOVERY_CAPABILITY, CAPS_DISCOVERY_RESPONSE_TAG, CAPS_PROOF_MATERIAL_BYTES,
};
use layerx_client::lni::framing::read_frame;
use layerx_client::lni::schema::decode_envelope;
use serde_json::Value;

const SCENARIOS: [&str; 5] = [
    "populated",
    "mixed-owner",
    "prefix-empty",
    "empty-module",
    "mutation",
];

fn fixture_dir() -> PathBuf {
    PathBuf::from(
        std::env::var("PAXEER_X_CAPS_FIXTURE_DIR")
            .expect("missing prerequisite: PAXEER_X_CAPS_FIXTURE_DIR from the native caps fixture"),
    )
}

fn hex32(value: &Value, name: &str) -> [u8; 32] {
    let text = value[name].as_str().expect("fixture hex field");
    assert_eq!(text.len(), 64, "{name} is 32 bytes");
    let mut out = [0; 32];
    for (index, byte) in out.iter_mut().enumerate() {
        *byte = u8::from_str_radix(&text[index * 2..index * 2 + 2], 16).expect("hex");
    }
    out
}

struct Scenario {
    network_id: u32,
    state_root: [u8; 32],
    selector: [u8; 32],
    payloads: Vec<Vec<u8>>,
    responses: Vec<CapsResponse>,
}

fn load(name: &str) -> Scenario {
    let dir = fixture_dir();
    let meta: Value = serde_json::from_slice(
        &std::fs::read(dir.join(format!("{name}.json"))).expect("fixture metadata readable"),
    )
    .expect("fixture metadata JSON");
    let frames = std::fs::read(dir.join(format!("{name}.bin"))).expect("fixture frames readable");
    let mut reader = Cursor::new(frames.as_slice());
    let mut payloads = Vec::new();
    let mut responses = Vec::new();
    while usize::try_from(reader.position()).expect("position") < frames.len() {
        let frame = read_frame(&mut reader, frames.len()).expect("canonical frame");
        let envelope = decode_envelope(&frame).expect("canonical LNI envelope");
        assert_eq!(envelope.message_tag, CAPS_DISCOVERY_RESPONSE_TAG);
        assert_eq!(envelope.proof_material.len(), CAPS_PROOF_MATERIAL_BYTES);
        let response = CapsResponse::decode(envelope.canonical_payload).expect("caps response");
        assert_eq!(
            response.encode().expect("re-encode"),
            envelope.canonical_payload,
            "{name}: decode/encode is byte exact"
        );
        payloads.push(envelope.canonical_payload.to_vec());
        responses.push(response);
    }
    Scenario {
        network_id: u32::try_from(meta["network_id"].as_u64().expect("network_id"))
            .expect("u32 network"),
        state_root: hex32(&meta, "state_root"),
        selector: hex32(&meta, "selector_account"),
        payloads,
        responses,
    }
}

fn split(scenario: &Scenario) -> (CapsSnapshotOpen, Vec<CapsPage>) {
    let mut responses = scenario.responses.iter();
    let Some(CapsResponse::Open(open)) = responses.next() else {
        panic!("first frame must open a snapshot");
    };
    let pages = responses
        .map(|response| match response {
            CapsResponse::Page(page) => page.clone(),
            other => panic!("unexpected response after open: {other:?}"),
        })
        .collect();
    (open.clone(), pages)
}

fn assert_cursor_binds(cursor: &CapsCursor, open: &CapsSnapshotOpen) {
    assert_eq!(cursor.snapshot_id(), open.snapshot_id);
    assert_eq!(cursor.network_id(), open.network_id);
    assert_eq!(cursor.state_root(), open.state_root);
    assert_eq!(cursor.selection_digest(), open.selection_digest);
}

#[test]
fn native_snapshots_bind_root_selection_and_cursor_chain() {
    for name in SCENARIOS {
        let scenario = load(name);
        let (open, pages) = split(&scenario);
        assert_eq!(open.network_id, scenario.network_id, "{name}");
        assert_eq!(open.state_root, scenario.state_root, "{name}");
        assert_eq!(
            open.selection_digest,
            selection_digest(scenario.network_id, scenario.selector),
            "{name}"
        );
        assert_eq!(open.first_cursor.prefix(), 1, "{name}: budget prefix first");
        assert_eq!(
            open.first_cursor.next_position(),
            open.budget_module.prefix_first,
            "{name}"
        );
        assert!(
            !pages.is_empty(),
            "{name}: every prefix exhausted explicitly"
        );
        let mut expected = open.first_cursor;
        for (index, page) in pages.iter().enumerate() {
            assert_eq!(page.cursor_echo, expected, "{name}: page {index} chains");
            assert_cursor_binds(&page.cursor_echo, &open);
            let done = index + 1 == pages.len();
            assert_eq!(page.exhausted, done, "{name}: only the last page exhausts");
            if let Some(next) = page.next_cursor {
                assert_cursor_binds(&next, &open);
                assert!(
                    next.prefix() > expected.prefix()
                        || next.next_position() > expected.next_position(),
                    "{name}: every page makes progress"
                );
                expected = next;
            }
            match progress(&open, &pages[..=index]) {
                CapsOutcome::Incomplete(state) => {
                    assert_eq!(state.snapshot_id, open.snapshot_id);
                    assert_eq!(state.pages_received, index + 1);
                    assert_eq!(state.next_cursor, page.next_cursor);
                }
                other => panic!("{name}: partial pages stay incomplete, got {other:?}"),
            }
        }
        assert_eq!(expected.prefix(), 2, "{name}: grant prefix reached");
    }
}

#[test]
fn mutation_fixture_produces_a_fresh_snapshot() {
    let before = split(&load("populated")).0;
    let after = split(&load("mutation")).0;
    assert_ne!(before.snapshot_id, after.snapshot_id);
    assert_ne!(before.first_cursor, after.first_cursor);
}

#[test]
fn malformed_native_payloads_refuse() {
    let scenario = load("populated");
    for payload in &scenario.payloads {
        let mut trailing = payload.clone();
        trailing.push(0);
        assert_eq!(
            CapsResponse::decode(&trailing),
            Err(CapsCodecError::Trailing)
        );
        for cut in [0, 2, 3, payload.len() / 2, payload.len() - 1] {
            assert!(CapsResponse::decode(&payload[..cut]).is_err());
        }
        let mut version = payload.clone();
        version[1] = 2;
        assert_eq!(CapsResponse::decode(&version), Err(CapsCodecError::Version));
        let mut op = payload.clone();
        op[2] = 9;
        assert_eq!(CapsResponse::decode(&op), Err(CapsCodecError::Op));
    }
    let (open, _) = split(&scenario);
    let cursor = open.first_cursor.as_bytes();
    assert_eq!(
        CapsCursor::from_bytes(&cursor[..106]),
        Err(CapsCodecError::Truncated)
    );
    let mut prefix = *cursor;
    prefix[102] = 3;
    assert_eq!(CapsCursor::from_bytes(&prefix), Err(CapsCodecError::Prefix));
    let mut version = *cursor;
    version[0] = 1;
    assert_eq!(
        CapsCursor::from_bytes(&version),
        Err(CapsCodecError::Version)
    );
}

#[test]
fn request_limits_and_selector_are_exact() {
    let scenario = load("populated");
    let open = split(&scenario).0;
    let request = CapsRequest::Open {
        network_id: scenario.network_id,
        selector_account: scenario.selector,
        max_items: 64,
        max_bytes: 4096,
    };
    let bytes = request.encode().expect("open request");
    assert_eq!(bytes.len(), 43);
    assert_eq!(CapsRequest::decode(&bytes), Ok(request));
    let page = CapsRequest::Page {
        cursor: open.first_cursor,
        max_items: 1,
        max_bytes: 4096,
    };
    let bytes = page.encode().expect("page request");
    assert_eq!(bytes.len(), 116);
    assert_eq!(CapsRequest::decode(&bytes), Ok(page));
    let release = CapsRequest::Release {
        snapshot_id: open.snapshot_id,
    };
    assert_eq!(release.encode().expect("release").len(), 35);
    for (max_items, max_bytes) in [(0, 4096), (65, 4096), (1, 4095)] {
        assert_eq!(
            CapsRequest::Page {
                cursor: open.first_cursor,
                max_items,
                max_bytes
            }
            .encode(),
            Err(CapsCodecError::Limit)
        );
    }
    assert_eq!(
        CapsRequest::Open {
            network_id: scenario.network_id,
            selector_account: [0; 32],
            max_items: 1,
            max_bytes: 4096
        }
        .encode(),
        Err(CapsCodecError::Selector)
    );
}

#[test]
fn unsupported_peers_refuse_explicitly() {
    assert_eq!(
        require_caps_discovery(&["program_head_attest"], 8),
        Err(CapsUnavailable::CapabilityNotAdvertised)
    );
    assert_eq!(
        require_caps_discovery(&[CAPS_DISCOVERY_CAPABILITY], 7),
        Err(CapsUnavailable::PeerVersionUnsupported)
    );
    assert_eq!(
        require_caps_discovery(&[CAPS_DISCOVERY_CAPABILITY], 8),
        Ok(())
    );
}
