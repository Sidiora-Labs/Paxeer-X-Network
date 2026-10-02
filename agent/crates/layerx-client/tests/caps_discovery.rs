use std::{collections::BTreeSet, fs, path::Path, sync::OnceLock, time::Duration};

use layerx_client::{
    availability::RetrievalLimits,
    budget::ProtocolBudgetRecord,
    caps::{CapsDiscovery, CapsError, CapsProgress},
    evidence::{verify_caps_object, RootSelector, VerifiedCaps},
    grants::CommittedGrant,
    handover::SequencerHistory,
    head::Head,
    lni::{capabilities::Capabilities, handshake::{self, Handshake, HandshakeConfig}, schema::{decode_envelope, encode_envelope, Envelope, Version},
        transport::{ConnectionGate, FrameTransport, Limits, TransportError, Uds}},
    read::{ReadContext, Requested},
};
use layerx_proof::{inclusion::SequencerAuthorization, state::decode_account_value, state_witness::StateWitness};
use layerx_types::verify::VerificationLevel;
use serde_json::Value;
use sha2::{Digest, Sha256};

const VERSION: Version = Version { major: 1, minor: 8 };
const CAPTURES: &[&str] = &[
    "empty-prefix-and-module", "populated-owner", "populated-foreign", "retained-before-mutation",
    "fresh-after-mutation", "retained-before-revocation", "retained-before-rollover",
    "fresh-after-rollover", "fresh-after-restart", "real-finality",
];

fn string<'a>(value: &'a Value, key: &str) -> &'a str {
    value[key].as_str().unwrap_or_else(|| panic!("missing string {key}"))
}
fn number(value: &Value, key: &str) -> u64 {
    value[key].as_u64().unwrap_or_else(|| panic!("missing number {key}"))
}
fn hex32(text: &str) -> [u8; 32] {
    assert_eq!(text.len(), 64);
    let mut result = [0; 32];
    for (index, slot) in result.iter_mut().enumerate() {
        *slot = u8::from_str_radix(&text[index * 2..index * 2 + 2], 16).expect("hex pin");
    }
    result
}
fn bytes(value: &Value, key: &str) -> [u8; 32] { hex32(string(value, key)) }
fn read_file(path: &str) -> Vec<u8> {
    assert!(Path::new(path).is_absolute(), "fixture files must have absolute paths");
    fs::read(path).unwrap_or_else(|error| panic!("required genuine fixture {path}: {error}"))
}

struct Fixture { inputs: Value, history: SequencerHistory }
impl Fixture {
    fn connect(inputs: &Value) -> (Uds, Handshake) {
        let mut transport = Uds::connect(Path::new(string(inputs, "socket")), &ConnectionGate::new(4), Limits {
            maximum_frame_bytes: 2 * 1024 * 1024, maximum_connections: 4, maximum_streams: 1,
            maximum_queued_bytes: 4 * 1024 * 1024, deadline: Duration::from_secs(10),
        }).expect("live credentialed native UDS");
        let accepted = handshake::perform(&mut transport, &HandshakeConfig {
            built_interface_version: VERSION, expected_protocol_version: 3, expected_network_id: 77,
        }, None).expect("production handshake");
        assert_eq!(accepted.node().interface_version, VERSION);
        assert_eq!(accepted.node().authorised_sequencer_key, bytes(inputs, "sequencer_public_key"));
        (transport, accepted)
    }
    fn load() -> Self {
        let path = std::env::var("LAYERX_CAPS_DISCOVERY_INPUTS")
            .expect("run only through the real native caps qualification fixture; no capture fallback");
        let inputs: Value = serde_json::from_slice(&read_file(&path)).expect("fixture manifest JSON");
        let captures = inputs["captures"].as_array().expect("native captures");
        assert_eq!(captures.len(), CAPTURES.len());
        let names: BTreeSet<_> = captures.iter().map(|capture| string(capture, "name")).collect();
        assert_eq!(names, CAPTURES.iter().copied().collect());
        assert!(!inputs["finality_provenance"].is_null(), "real registered finality provenance");
        assert!(!read_file(string(&inputs, "genesis_manifest")).is_empty());
        let descriptor = read_file(string(&inputs, "genesis_descriptor"));
        assert_eq!(descriptor.len(), 105);
        assert_eq!(&descriptor[..5], b"LXGD\x01");
        assert_eq!(&descriptor[5..9], &77_u32.to_be_bytes());
        let genesis_root = descriptor[41..73].try_into().expect("deployment genesis root");
        let public = bytes(&inputs, "sequencer_public_key");
        let id: [u8; 32] = Sha256::digest(format!("layerx-sequencer:{}", string(&inputs, "sequencer_public_key")).as_bytes()).into();
        assert_eq!(id, bytes(&inputs, "sequencer_id"));
        let trust = read_file(string(&inputs, "genesis_trust"));
        let material = layerx_wire::handover::decode_genesis_trust(&trust).expect("native genesis trust");
        let governance = StateWitness::decode(material.governance_witness).expect("committed governance witness");
        assert_eq!(governance.value, bytes(&inputs, "handover_authority_public_key"));
        let mut history = SequencerHistory::from_genesis_artifact(
            &trust, 77, genesis_root, public,
        ).expect("independently pinned genesis authority");
        let (mut transport, accepted) = Self::connect(&inputs);
        let head = accepted.node().latest_sealed_batch;
        assert!(head > 0 && head < 4096, "bounded genuine fixture history");
        for batch in 1..=head {
            history.fetch_next(&mut transport, VERSION, 100 + batch * 3, RetrievalLimits {
                maximum_bytes: 64 * 1024 * 1024, maximum_chunks: 4096, deadline: Duration::from_secs(10),
            }).unwrap_or_else(|error| panic!("native signed history batch {batch}: {error:?}"));
        }
        Self { inputs, history }
    }
    fn captures(&self) -> &[Value] { self.inputs["captures"].as_array().expect("captures") }
    fn capture(&self, name: &str) -> &Value {
        self.captures().iter().find(|capture| string(capture, "name") == name).expect("required capture")
    }
    fn context(&self, capture: &Value) -> ReadContext {
        let rank = match number(capture, "rank") {
            3 => VerificationLevel::STATE_PROVEN, 4 => VerificationLevel::CHECKPOINT_FINALISED,
            other => panic!("unexpected native capture rank {other}"),
        };
        let root_selector = match number(capture, "selector") {
            1 => RootSelector::Latest, 3 => RootSelector::Checkpoint(bytes(capture, "selected")),
            other => panic!("unexpected capture selector {other}"),
        };
        ReadContext { interface_version: VERSION, correlation_id: 9001,
            expected_protocol_version: 3, expected_network_id: 77, requested: Requested::new(rank),
            head: Head { chain_sequence: number(capture, "head_sequence"), sealed_batch: number(capture, "head_batch"),
                finalised_checkpoint: if number(capture, "selector") == 3 { bytes(capture, "selected") } else { [0; 32] } },
            sequencer_authorization: self.history.authorization_for_batch(number(capture, "head_batch")).expect("verified term"),
            handshake_sequencer_key: bytes(&self.inputs, "sequencer_public_key"), root_selector }
    }
    fn verify(&self, capture: &Value, object: &[u8]) -> VerifiedCaps {
        verify_caps_object(object, bytes(capture, "did"), bytes(capture, "root"), self.context(capture), Some(&self.history))
            .unwrap_or_else(|error| panic!("native {} rejected: {error:?}", string(capture, "name")))
    }
    fn refuse(&self, capture: &Value, object: &Object, label: &str) {
        assert!(verify_caps_object(&object.encode(), bytes(capture, "did"), bytes(capture, "root"),
            self.context(capture), Some(&self.history)).is_err(), "accepted {label}");
        println!("CAPS_CASE {label}");
    }
}
fn fixture() -> &'static Fixture {
    static FIXTURE: OnceLock<Fixture> = OnceLock::new();
    FIXTURE.get_or_init(Fixture::load)
}

struct Reader<'a>(&'a [u8]);
impl Reader<'_> {
    fn take(&mut self, count: usize) -> Vec<u8> {
        assert!(count <= self.0.len(), "truncated genuine object");
        let result = self.0[..count].to_vec(); self.0 = &self.0[count..]; result
    }
    fn u16(&mut self) -> u16 { u16::from_be_bytes(self.take(2).try_into().expect("u16")) }
    fn u32(&mut self) -> u32 { u32::from_be_bytes(self.take(4).try_into().expect("u32")) }
    fn vector(&mut self) -> Vec<u8> { let count = self.u32() as usize; self.take(count) }
    fn witness(&mut self) -> StateWitness { StateWitness::decode(&self.vector()).expect("genuine state witness") }
    fn range(&mut self) -> Range {
        let id = self.u16(); let root = self.take(32); let index = self.u32(); let count = self.u32();
        let depth = self.take(1)[0]; let siblings = self.take(usize::from(depth) * 32);
        let leaves = (0..self.u32()).map(|_| self.witness()).collect();
        Range { id, root, index, count, siblings, leaves }
    }
}
#[derive(Clone)]
struct Range { id: u16, root: Vec<u8>, index: u32, count: u32, siblings: Vec<u8>, leaves: Vec<StateWitness> }
#[derive(Clone)]
struct Object { value: Vec<u8>, proof: Vec<u8>, roots: Vec<u8>, universal: Range, budget: Range, grant: Range, accounts: Vec<StateWitness> }
fn vector(out: &mut Vec<u8>, value: &[u8]) {
    out.extend_from_slice(&u32::try_from(value.len()).expect("bounded vector").to_be_bytes()); out.extend_from_slice(value);
}
fn path(out: &mut Vec<u8>, siblings: &[[u8; 32]]) {
    out.push(u8::try_from(siblings.len()).expect("bounded path"));
    for sibling in siblings { out.extend_from_slice(sibling); }
}
fn witness_bytes(witness: &StateWitness) -> Vec<u8> {
    let mut out = 2_u16.to_be_bytes().to_vec(); out.extend_from_slice(&witness.module_id.to_be_bytes());
    vector(&mut out, &witness.key); vector(&mut out, &witness.value);
    if let Some(account) = &witness.account_path {
        out.extend_from_slice(&account.index.to_be_bytes()); out.extend_from_slice(&account.count.to_be_bytes()); path(&mut out, &account.siblings);
    }
    out.extend_from_slice(&witness.leaf_index_a.to_be_bytes()); out.extend_from_slice(&witness.leaf_count_a.to_be_bytes());
    path(&mut out, &witness.siblings_a); out.extend_from_slice(&witness.leaf_count_b.to_be_bytes()); path(&mut out, &witness.siblings_b); out
}
impl Range {
    fn encode(&self, out: &mut Vec<u8>) {
        out.extend_from_slice(&self.id.to_be_bytes()); out.extend_from_slice(&self.root);
        out.extend_from_slice(&self.index.to_be_bytes()); out.extend_from_slice(&self.count.to_be_bytes());
        out.push(u8::try_from(self.siblings.len() / 32).expect("depth")); out.extend_from_slice(&self.siblings);
        out.extend_from_slice(&u32::try_from(self.leaves.len()).expect("count").to_be_bytes());
        for leaf in &self.leaves { vector(out, &witness_bytes(leaf)); }
    }
}
impl Object {
    fn decode(bytes: &[u8]) -> Self {
        let mut reader = Reader(bytes); assert_eq!(reader.u16(), 1);
        let value = reader.vector(); let proof = reader.vector(); let count = usize::from(reader.u16());
        let roots = reader.take(count * 32); let universal = reader.range(); assert_eq!(reader.take(1), [2]);
        let budget = reader.range(); let grant = reader.range();
        let accounts = (0..reader.u32()).map(|_| reader.witness()).collect(); assert!(reader.0.is_empty());
        let result = Self { value, proof, roots, universal, budget, grant, accounts };
        assert_eq!(result.encode(), bytes, "lossless parsing of native capture"); result
    }
    fn encode(&self) -> Vec<u8> {
        let mut out = 1_u16.to_be_bytes().to_vec(); vector(&mut out, &self.value); vector(&mut out, &self.proof);
        out.extend_from_slice(&u16::try_from(self.roots.len() / 32).expect("roots").to_be_bytes()); out.extend_from_slice(&self.roots);
        self.universal.encode(&mut out); out.push(2); self.budget.encode(&mut out); self.grant.encode(&mut out);
        out.extend_from_slice(&u32::try_from(self.accounts.len()).expect("accounts").to_be_bytes());
        for account in &self.accounts { vector(&mut out, &witness_bytes(account)); } out
    }
    fn range_mut(&mut self, budget: bool) -> &mut Range { if budget { &mut self.budget } else { &mut self.grant } }
}

#[test]
fn native_complete_evidence_and_mutation_refusals() {
    let f = fixture();
    for capture in f.captures() {
        let raw = read_file(string(capture, "path")); let object = Object::decode(&raw); let verified = f.verify(capture, &raw);
        assert_eq!(verified.did(), bytes(capture, "did")); assert_eq!(verified.state_root(), bytes(capture, "root"));
        assert_eq!(verified.budgets().len() as u64, number(capture, "budgets"));
        assert_eq!(verified.grants().len() as u64, number(capture, "grants"));
        assert_eq!(u64::from(verified.level().wire_rank()), number(capture, "rank"));
        assert_eq!(verified.is_empty(), number(capture, "budgets") + number(capture, "grants") == 0);
        for budget in verified.budgets() {
            assert!(object.budget.leaves.iter().filter(|leaf| leaf.key.starts_with(b"budget:")).any(|leaf|
                matches!(ProtocolBudgetRecord::decode_state(&leaf.key, &leaf.value), Ok(ref record) if record == budget)), "all budget fields preserved");
        }
        for grant in verified.grants() {
            assert!(object.grant.leaves.iter().filter(|leaf| leaf.key.starts_with(b"grant:")).any(|leaf|
                matches!(CommittedGrant::decode(&leaf.key, &leaf.value), Ok(ref record) if record == grant)), "all grant fields preserved");
        }
        println!("CAPS_CASE {}", string(capture, "name"));
        let mut bad = object.clone(); bad.roots[0] ^= 1; f.refuse(capture, &bad, "composite-root-substitution");
        let mut bad = object.clone(); bad.universal.leaves.remove(0); f.refuse(capture, &bad, "universal-leaf-omission");
        let mut bad = object.clone(); bad.accounts.remove(0); f.refuse(capture, &bad, "account-omission");
        let mut bad = object.clone(); bad.accounts[0].value[0] ^= 1; f.refuse(capture, &bad, "account-value-substitution");
        let mut bad = object.clone(); bad.accounts[0].account_path.as_mut().expect("account path").count += 1;
        f.refuse(capture, &bad, "account-count-padding");
        for budget in [true, false] {
            let original = if budget { &object.budget } else { &object.grant };
            let prefix: &[u8] = if budget { b"budget:" } else { b"grant:" };
            let indices: Vec<_> = original.leaves.iter().enumerate().filter(|(_, leaf)| leaf.key.starts_with(prefix)).map(|(index, _)| index).collect();
            if indices.is_empty() {
                let mut bad = object.clone(); bad.range_mut(budget).root[0] ^= 1; f.refuse(capture, &bad, "empty-prefix-or-module-root");
            } else {
                for index in [indices[0], indices[indices.len() / 2], *indices.last().expect("last")] {
                    let mut bad = object.clone(); bad.range_mut(budget).leaves.remove(index); f.refuse(capture, &bad, "first-middle-last-prefix-omission");
                }
                let index = indices[0];
                let mut bad = object.clone(); bad.range_mut(budget).leaves.insert(index, original.leaves[index].clone()); f.refuse(capture, &bad, "duplicate-prefix-leaf");
                let mut bad = object.clone(); bad.range_mut(budget).leaves[index].value.pop(); f.refuse(capture, &bad, "noncanonical-budget-or-grant");
                let leaf = &original.leaves[index]; let mut value = leaf.value.clone(); value.pop();
                if budget { assert!(ProtocolBudgetRecord::decode_state(&leaf.key, &value).is_err()); }
                else { assert!(CommittedGrant::decode(&leaf.key, &value).is_err()); }
                let mut bad = object.clone(); bad.range_mut(budget).leaves[index].leaf_index_a += 1; f.refuse(capture, &bad, "wrong-leaf-index");
                let mut bad = object.clone(); bad.range_mut(budget).leaves[index].leaf_count_a += 1; f.refuse(capture, &bad, "wrong-leaf-count-padding");
                let mut bad = object.clone(); bad.range_mut(budget).leaves[index].siblings_a.push([0; 32]); f.refuse(capture, &bad, "noncanonical-path-padding");
            }
            if original.leaves.len() > 1 {
                let mut bad = object.clone(); bad.range_mut(budget).leaves.swap(0, 1); f.refuse(capture, &bad, "range-order");
            }
            for (index, leaf) in original.leaves.iter().enumerate().filter(|(_, leaf)| !leaf.key.starts_with(prefix)) {
                let mut bad = object.clone(); bad.range_mut(budget).leaves.remove(index); f.refuse(capture, &bad,
                    if leaf.key.as_slice() < prefix { "lower-boundary-omission" } else { "upper-boundary-omission" });
            }
        }
        let mut context = f.context(capture); context.expected_network_id += 1;
        assert!(verify_caps_object(&raw, bytes(capture, "did"), bytes(capture, "root"), context, Some(&f.history)).is_err());
        let mut context = f.context(capture); context.expected_protocol_version += 1;
        assert!(verify_caps_object(&raw, bytes(capture, "did"), bytes(capture, "root"), context, Some(&f.history)).is_err());
        let mut context = f.context(capture); context.requested = Requested::new(VerificationLevel::SETTLEMENT_ANCHORED);
        assert!(verify_caps_object(&raw, bytes(capture, "did"), bytes(capture, "root"), context, Some(&f.history)).is_err());
        let mut context = f.context(capture); context.handshake_sequencer_key[0] ^= 1;
        assert!(verify_caps_object(&raw, bytes(capture, "did"), bytes(capture, "root"), context, None).is_err());
        let mut context = f.context(capture); context.sequencer_authorization = SequencerAuthorization::new(
            bytes(&f.inputs, "sequencer_id"), bytes(&f.inputs, "sequencer_public_key"), number(capture, "head_batch") + 1, u64::MAX);
        assert!(verify_caps_object(&raw, bytes(capture, "did"), bytes(capture, "root"), context, None).is_err());
    }
    source_record_attacks(f);
    println!("CAPS_CASE domain-authority-term-rank-refusals");
    assert_eq!(bytes(f.capture("populated-owner"), "root"), bytes(f.capture("retained-before-mutation"), "root"));
    assert_ne!(bytes(f.capture("retained-before-mutation"), "root"), bytes(f.capture("fresh-after-mutation"), "root"));
    assert_ne!(bytes(f.capture("retained-before-rollover"), "root"), bytes(f.capture("fresh-after-rollover"), "root"));
    println!("CAPS_CASE retained-and-fresh-root-separation");
}

#[test]
fn native_live_paginated_discovery() {
    let f = fixture(); let capture = f.capture("fresh-after-restart");
    let (mut transport, handshake) = Fixture::connect(&f.inputs); let mut context = f.context(capture);
    context.head = Head { chain_sequence: handshake.node().chain_head_sequence, sealed_batch: handshake.node().latest_sealed_batch,
        finalised_checkpoint: handshake.node().latest_finalised_checkpoint };
    context.sequencer_authorization = f.history.authorization_for_batch(context.head.sealed_batch).expect("live verified authority");
    let mut session = CapsDiscovery::begin(&mut transport, handshake.capabilities(), context, bytes(capture, "did"), 512,
        Duration::from_secs(30), Some(&f.history)).expect("production discovery");
    let mut received = 0; let mut expected_total = None; let mut complete = false;
    for _ in 0..131_072 {
        match session.advance() {
            CapsProgress::Incomplete { received_bytes, total_bytes } => {
                assert!(received_bytes > received && received_bytes < total_bytes);
                if let Some(total) = expected_total { assert_eq!(total, total_bytes); }
                expected_total = Some(total_bytes); received = received_bytes;
            }
            CapsProgress::Complete(value) => {
                assert!(received > 0, "real multi-page path required");
                assert_eq!(value.budgets().len(), 2); assert_eq!(value.grants().len(), 3);
                assert_eq!(value.freshness().global_sequence, context.head.chain_sequence);
                assert_eq!(value.level(), VerificationLevel::STATE_PROVEN); complete = true; break;
            }
            other => panic!("native discovery did not complete: {other:?}"),
        }
    }
    assert!(complete, "bounded traversal exhausted");
    assert!(matches!(session.advance(), CapsProgress::Refused(CapsError::Terminal)));
    println!("CAPS_CASE live-paginated-complete-and-terminal");
    drop(session);
    let absent_text = b"did:layerx:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff";
    let mut hasher = Sha256::new(); hasher.update(b"LXP/v1/did-id\0");
    hasher.update(u16::try_from(absent_text.len()).expect("DID length").to_be_bytes()); hasher.update(absent_text);
    let absent: [u8; 32] = hasher.finalize().into();
    context.correlation_id += 1;
    let mut empty = CapsDiscovery::begin(&mut transport, handshake.capabilities(), context, absent, 512,
        Duration::from_secs(30), Some(&f.history)).expect("real native absent-owner selection");
    let mut partial = false; let mut confirmed_empty = false;
    for _ in 0..131_072 {
        match empty.advance() {
            CapsProgress::Incomplete { received_bytes, total_bytes } => {
                assert!(received_bytes > 0 && received_bytes < total_bytes); partial = true;
            }
            CapsProgress::Empty(value) => {
                assert!(partial && value.is_empty()); assert_eq!(value.did(), absent);
                assert_eq!(value.level(), VerificationLevel::STATE_PROVEN); confirmed_empty = true; break;
            }
            other => panic!("absent-owner evidence refused or exposed incorrect caps: {other:?}"),
        }
    }
    assert!(confirmed_empty, "empty requires complete authenticated object");
    assert!(matches!(empty.advance(), CapsProgress::Refused(CapsError::Terminal)));
    println!("CAPS_CASE live-paginated-confirmed-empty");
}

#[test]
fn native_client_selection_and_terminal_refusals() {
    let f = fixture(); let capture = f.capture("fresh-after-restart");
    let (mut transport, handshake) = Fixture::connect(&f.inputs); let context = f.context(capture); let did = bytes(capture, "did");
    let unavailable = Capabilities::negotiate(&[]);
    assert!(matches!(CapsDiscovery::begin(&mut transport, &unavailable, context, did, 512, Duration::from_secs(10), Some(&f.history)), Err(CapsError::Unavailable)));
    for page in [0, 1_048_577] {
        assert!(matches!(CapsDiscovery::begin(&mut transport, handshake.capabilities(), context, did, page, Duration::from_secs(10), Some(&f.history)), Err(CapsError::Bounds)));
    }
    assert!(matches!(CapsDiscovery::begin(&mut transport, handshake.capabilities(), context, [0; 32], 512, Duration::from_secs(10), Some(&f.history)), Err(CapsError::Selection)));
    let mut invalid = context; invalid.root_selector = RootSelector::Checkpoint([0; 32]);
    assert!(matches!(CapsDiscovery::begin(&mut transport, handshake.capabilities(), invalid, did, 512, Duration::from_secs(10), Some(&f.history)), Err(CapsError::Selection)));
    let mut invalid = context; invalid.requested = Requested::new(VerificationLevel::SETTLEMENT_ANCHORED);
    assert!(matches!(CapsDiscovery::begin(&mut transport, handshake.capabilities(), invalid, did, 512, Duration::from_secs(10), Some(&f.history)), Err(CapsError::Unavailable)));
    assert!(matches!(CapsDiscovery::begin(&mut transport, handshake.capabilities(), context, did, 512, Duration::ZERO, Some(&f.history)), Err(CapsError::Bounds)));
    let mut expired = CapsDiscovery::begin(&mut transport, handshake.capabilities(), context, did, 512, Duration::from_nanos(1), Some(&f.history)).expect("bounded session");
    std::thread::sleep(Duration::from_millis(1));
    assert!(matches!(expired.advance(), CapsProgress::Refused(CapsError::Expired)));
    assert!(matches!(expired.advance(), CapsProgress::Refused(CapsError::Terminal)));
    drop(expired);
    println!("CAPS_CASE client-preflight-expiry-and-terminal");
    hostile_native_pages(f);
}


struct HostileRelay { native: Uds, attack: &'static str, pages: usize, previous_cursor: [u8; 32] }
impl FrameTransport for HostileRelay {
    fn send(&mut self, frame: &[u8]) -> Result<(), TransportError> { self.native.send(frame) }
    fn receive(&mut self) -> Result<Vec<u8>, TransportError> {
        let frame = self.native.receive()?;
        let response = decode_envelope(&frame).expect("real native envelope before relay mutation");
        assert_eq!(response.message_tag, 43, "relay only mutates genuine native caps responses");
        assert!(response.proof_material.is_empty());
        let mut payload = response.canonical_payload.to_vec();
        assert!(payload.len() >= 119);
        self.pages += 1;
        if self.pages == 1 {
            self.previous_cursor.copy_from_slice(&payload[83..115]);
            return Ok(frame);
        }
        assert_eq!(self.pages, 2, "refusal must be terminal");
        let mut correlation_id = response.correlation_id;
        let mut proof = Vec::new();
        match self.attack {
            "relay-no-progress" => payload[70..74].copy_from_slice(&0_u32.to_be_bytes()),
            "relay-oversize-page" => payload[115..119].copy_from_slice(&513_u32.to_be_bytes()),
            "relay-oversize-object" => payload[74..78].copy_from_slice(&67_108_865_u32.to_be_bytes()),
            "relay-wrong-root" => payload[38] ^= 1,
            "relay-wrong-network" => payload[34] ^= 1,
            "relay-wrong-snapshot" => payload[2] ^= 1,
            "relay-repeated-cursor" => payload[83..115].copy_from_slice(&self.previous_cursor),
            "relay-empty-cursor" => payload[83..115].fill(0),
            "relay-premature-done" => payload[82] = 1,
            "relay-changed-total" => payload[77] ^= 1,
            "relay-zero-chunk" => payload[115..119].fill(0),
            "relay-truncated-chunk" => { payload.pop(); }
            "relay-wrong-correlation" => correlation_id += 1,
            "relay-unexpected-proof" => proof.push(1),
            other => panic!("unknown hostile native relay {other}"),
        }
        Ok(encode_envelope(Envelope { version: response.version, message_tag: response.message_tag,
            correlation_id, canonical_payload: &payload, proof_material: &proof }).expect("mutated real response envelope"))
    }
}
fn hostile_native_pages(f: &Fixture) {
    for attack in ["relay-no-progress", "relay-oversize-page", "relay-oversize-object", "relay-wrong-root",
        "relay-wrong-network", "relay-wrong-snapshot", "relay-repeated-cursor", "relay-empty-cursor",
        "relay-premature-done", "relay-changed-total", "relay-zero-chunk", "relay-truncated-chunk",
        "relay-wrong-correlation", "relay-unexpected-proof"] {
        let (native, handshake) = Fixture::connect(&f.inputs);
        let mut relay = HostileRelay { native, attack, pages: 0, previous_cursor: [0; 32] };
        let capture = f.capture("fresh-after-restart"); let mut context = f.context(capture);
        context.head = Head { chain_sequence: handshake.node().chain_head_sequence,
            sealed_batch: handshake.node().latest_sealed_batch, finalised_checkpoint: handshake.node().latest_finalised_checkpoint };
        let mut session = CapsDiscovery::begin(&mut relay, handshake.capabilities(), context, bytes(capture, "did"),
            512, Duration::from_secs(30), Some(&f.history)).expect("native hostile relay session");
        assert!(matches!(session.advance(), CapsProgress::Incomplete { received_bytes: 512, .. }), "real first page required: {attack}");
        assert!(matches!(session.advance(), CapsProgress::Refused(_)), "accepted hostile native response: {attack}");
        assert!(matches!(session.advance(), CapsProgress::Refused(CapsError::Terminal)), "nonterminal refusal: {attack}");
        println!("CAPS_CASE {attack}");
    }
}


fn source_record_attacks(f: &Fixture) {
    let capture = f.capture("populated-owner");
    let raw = read_file(string(capture, "path")); let object = Object::decode(&raw); let verified = f.verify(capture, &raw);
    let budget_indices: Vec<_> = object.budget.leaves.iter().enumerate().filter(|(_, leaf)| leaf.key.starts_with(b"budget:")).collect();
    assert_eq!(budget_indices.len(), 3, "two owned budgets among a foreign budget");
    let owned = budget_indices.iter().find(|(_, leaf)| {
        let record = ProtocolBudgetRecord::decode_state(&leaf.key, &leaf.value).expect("canonical native budget");
        verified.budgets().contains(&record)
    }).expect("owned budget among foreign records").0;
    let foreign = budget_indices.iter().find(|(_, leaf)| {
        let record = ProtocolBudgetRecord::decode_state(&leaf.key, &leaf.value).expect("canonical native budget");
        !verified.budgets().contains(&record)
    }).expect("foreign budget present").0;
    let mut bad = object.clone(); bad.budget.leaves.remove(owned); f.refuse(capture, &bad, "owned-omission-among-foreign");
    let mut bad = object.clone(); bad.budget.leaves[foreign].value.pop(); f.refuse(capture, &bad, "foreign-malformation-before-filter");
    for budget in [true, false] {
        let range = if budget { &object.budget } else { &object.grant };
        assert!(!range.leaves.is_empty());
        let mut bad = object.clone(); bad.range_mut(budget).leaves.clear(); f.refuse(capture, &bad, "forged-empty-prefix");
        let mut bad = object.clone(); bad.range_mut(budget).leaves.clear(); bad.range_mut(budget).root.fill(0); f.refuse(capture, &bad, "forged-empty-module");
        let mut bad = object.clone(); bad.range_mut(budget).leaves[0].key[0] = 0;
        f.refuse(capture, &bad, "lower-boundary-substitution");
        let mut bad = object.clone(); bad.range_mut(budget).leaves.last_mut().expect("last").key[0] = 255;
        f.refuse(capture, &bad, "upper-boundary-substitution");
    }
    let odd = object.budget.leaves.iter().position(|leaf| leaf.leaf_count_a % 2 == 1 && leaf.leaf_index_a + 1 == leaf.leaf_count_a)
        .expect("genuine odd-sized budget tree");
    let mut bad = object.clone(); bad.budget.leaves[odd].siblings_a[0][0] ^= 1;
    f.refuse(capture, &bad, "odd-node-sibling-substitution");
    let mut bad = object.clone(); bad.accounts.swap(0, 1); f.refuse(capture, &bad, "account-order");
    let mut bad = object.clone(); bad.accounts.insert(0, object.accounts[0].clone()); f.refuse(capture, &bad, "duplicate-account-proof");
    let mut bad = object.clone(); bad.accounts[0].key = object.accounts[1].key.clone(); f.refuse(capture, &bad, "account-id-substitution");
    let owner = verified.budgets()[0].owner;
    let account_index = object.accounts.iter().position(|witness| witness.key[1..] == owner).expect("owner account witness");
    let foreign_capture = f.capture("populated-foreign");
    let foreign_caps = f.verify(foreign_capture, &read_file(string(foreign_capture, "path")));
    assert!(verified.budgets().iter().all(|record| !foreign_caps.budgets().contains(record)));
    assert!(verified.grants().iter().all(|record| !foreign_caps.grants().contains(record)));
    println!("CAPS_CASE owner-and-foreign-classification");
    let foreign_owner = foreign_caps.budgets()[0].owner;
    let foreign_index = object.accounts.iter().position(|witness| witness.key[1..] == foreign_owner).expect("foreign owner witness");
    let mut bad = object.clone(); bad.accounts[account_index].value = object.accounts[foreign_index].value.clone();
    f.refuse(capture, &bad, "foreign-owner-account-substitution");
    let account = decode_account_value(owner, &object.accounts[account_index].value).expect("canonical owner account");
    assert_eq!(account.kind, 1); assert!(account.name.starts_with(b"agent:") && account.name.ends_with(b":main"));
    let name_end = 2 + account.name.len();
    let mut bad = object.clone(); bad.accounts[account_index].value[2] = b'x'; f.refuse(capture, &bad, "invalid-owner-prefix");
    let mut bad = object.clone(); bad.accounts[account_index].value[name_end - 1] ^= 1; f.refuse(capture, &bad, "owner-name-alias");
    let mut bad = object.clone(); bad.accounts[account_index].value[name_end] = 6; f.refuse(capture, &bad, "crosschain-owner-kind");
    let source = verified.budgets().iter().find_map(|record| record.source_account).expect("real custody-v2 source account");
    let source_index = object.accounts.iter().position(|witness| witness.key[1..] == source).expect("custody source proof");
    let mut bad = object.clone(); bad.accounts.remove(source_index); f.refuse(capture, &bad, "missing-custody-v2-source");
    let mut bad = object.clone(); let last = bad.accounts[source_index].value.len() - 2;
    bad.accounts[source_index].value[last] ^= 1; f.refuse(capture, &bad, "custody-source-authority-substitution");
    let grant_index = object.grant.leaves.iter().position(|leaf| leaf.key.starts_with(b"grant:")).expect("real grant leaf");
    let mut bad = object.clone(); bad.grant.leaves[grant_index].value[394] = 2;
    assert!(CommittedGrant::decode(&bad.grant.leaves[grant_index].key, &bad.grant.leaves[grant_index].value).is_err());
    f.refuse(capture, &bad, "noncanonical-grant-boolean");
    let mut proof = Reader(&object.proof); assert_eq!(proof.u16(), 1); assert_eq!(proof.take(1), [4]);
    assert_eq!(proof.take(1), [1]); proof.vector();
    let signed = object.proof.len() - proof.0.len(); assert_eq!(proof.u16(), 1);
    proof.take(32 + 32 + 8 + 8); proof.vector(); let signature = object.proof.len() - proof.0.len();
    let mut bad = object.clone(); bad.proof[signed + 34] ^= 1; f.refuse(capture, &bad, "signed-header-authority-substitution");
    let mut bad = object.clone(); bad.proof[signed + 66..signed + 74].copy_from_slice(&(number(capture, "head_batch") + 1).to_be_bytes());
    f.refuse(capture, &bad, "signed-header-term-substitution");
    let mut bad = object.clone(); bad.proof[signature] ^= 1; f.refuse(capture, &bad, "signed-header-signature-substitution");
    let mut context = f.context(capture); context.head.chain_sequence += 1;
    assert!(verify_caps_object(&raw, bytes(capture, "did"), bytes(capture, "root"), context, Some(&f.history)).is_err());
    let mut context = f.context(capture); context.root_selector = RootSelector::Batch(context.head.sealed_batch);
    assert!(verify_caps_object(&raw, bytes(capture, "did"), bytes(capture, "root"), context, Some(&f.history)).is_err());
    let mut root = bytes(capture, "root"); root[0] ^= 1;
    assert!(verify_caps_object(&raw, bytes(capture, "did"), root, f.context(capture), Some(&f.history)).is_err());
    println!("CAPS_CASE freshness-selector-and-expected-root-refusals");
    let finality = f.capture("real-finality"); let object = Object::decode(&read_file(string(finality, "path")));
    assert_eq!(bytes(finality, "selected"), bytes(&f.inputs["finality_provenance"], "checkpoint_id"));
    let mut bad = object.clone(); let last = bad.proof.len() - 1; bad.proof[last] ^= 1;
    f.refuse(finality, &bad, "finality-context-substitution");
    let mut context = f.context(finality); let mut selected = bytes(finality, "selected"); selected[0] ^= 1;
    context.root_selector = RootSelector::Checkpoint(selected);
    assert!(verify_caps_object(&object.encode(), bytes(finality, "did"), bytes(finality, "root"), context, Some(&f.history)).is_err());
    println!("CAPS_CASE finality-selector-substitution");
}

#[test]
fn execution_prestate_schema_keeps_caps_tags_and_vectors() {
    use layerx_client::lni::schema::{lni_golden_vectors, lni_schema_v1, Capability};
    let schema = lni_schema_v1();
    assert_eq!(schema.version, Version::V1_9);
    for (tag, name, capability, literal) in [
        (42, "CapsDiscoveryRequest", Capability::CapsDiscovery, "00010008002a0000000000000000000000012a00000000"),
        (43, "CapsDiscoveryResponse", Capability::CapsDiscovery, "00010008002b0000000000000000000000012b00000000"),
        (44, "ExecutionPrestateRequest", Capability::ExecutionPrestate, "00010009002c0000000000000000000000012c00000000"),
        (45, "ExecutionPrestateResponse", Capability::ExecutionPrestate, "00010009002d0000000000000000000000012d00000000"),
    ] {
        let descriptor = schema.messages.iter().find(|message| message.tag == tag).expect("distinct additive message");
        assert_eq!(descriptor.name, name);
        assert_eq!(descriptor.capability, capability);
        let golden = lni_golden_vectors().iter().find(|vector| vector.message == name).expect("canonical vector");
        assert_eq!(golden.encoded_hex, literal);
        assert_eq!(golden.version(), if tag < 44 { Version::V1_8 } else { Version::V1_9 });
        let encoded = encode_envelope(Envelope { version: golden.version(), message_tag: tag, correlation_id: 0,
            canonical_payload: golden.payload, proof_material: golden.proof_material }).expect("real canonical encoder");
        let actual_hex: String = encoded.iter().map(|byte| format!("{byte:02x}")).collect();
        assert_eq!(actual_hex, literal);
        let decoded = decode_envelope(&encoded).expect("canonical decoder");
        assert_eq!(decoded.message_tag, tag);
        for length in 0..encoded.len() { assert!(decode_envelope(&encoded[..length]).is_err()); }
        let mut trailing = encoded.clone(); trailing.push(0);
        assert!(decode_envelope(&trailing).is_err());
    }
    let old = Capabilities::negotiate(&["caps_discovery".to_owned()]);
    assert!(old.contains(Capability::CapsDiscovery));
    assert!(!old.contains(Capability::ExecutionPrestate));
    let current = Capabilities::negotiate(&["caps_discovery".to_owned(), "execution_prestate".to_owned()]);
    assert!(current.contains(Capability::CapsDiscovery) && current.contains(Capability::ExecutionPrestate));
    println!("CAPS_CASE execution-prestate-additive-tags-and-legacy-vectors");
}

#[test]
fn native_legacy_caps_connection_refuses_execution_prestate_routing() {
    use layerx_client::lni::{refusal::decode_core_refusal, schema::Capability};
    let f = fixture();
    for (tag, request_version) in [(42, Version::V1_8), (44, Version::V1_8), (44, Version::V1_9)] {
        let (mut transport, accepted) = Fixture::connect(&f.inputs);
        assert_eq!(accepted.node().interface_version, Version::V1_8);
        assert!(!accepted.capabilities().contains(Capability::ExecutionPrestate));
        let mut selection = [0_u8; 177];
        selection[..2].copy_from_slice(&2_u16.to_be_bytes());
        selection[3..7].copy_from_slice(&77_u32.to_be_bytes());
        selection[71..75].copy_from_slice(&512_u32.to_be_bytes());
        transport.send(&encode_envelope(Envelope { version: request_version, message_tag: tag,
            correlation_id: 18_001, canonical_payload: &selection, proof_material: &[] })
            .expect("bounded malformed selection")).expect("real native request");
        let frame = transport.receive().expect("explicit native refusal");
        let response = decode_envelope(&frame).expect("canonical refusal envelope");
        assert_eq!(response.version, Version::V1_8);
        assert_eq!(response.message_tag, 25);
        assert_eq!(response.correlation_id, 18_001);
        assert!(response.proof_material.is_empty());
        assert!(decode_core_refusal(response.canonical_payload).is_some());
    }
    println!("CAPS_CASE legacy-connection-refuses-new-prestate-routing");
}

fn prestate_module_primitive(range: &Range) -> layerx_proof::state_range::ModuleRangeWitness {
    layerx_proof::state_range::ModuleRangeWitness {
        module_id: range.id,
        subtree_root: range.root.as_slice().try_into().expect("native root"),
        composite_index: range.index,
        composite_count: range.count,
        composite_siblings: range.siblings.chunks_exact(32).map(|bytes| bytes.try_into().expect("sibling")).collect(),
        leaves: range.leaves.clone(),
    }
}

#[test]
fn native_prestate_primitives_reject_omitted_mixed_and_malformed_proofs() {
    use layerx_proof::state_range::{verify_account_tree, verify_composite_roots};
    let f = fixture();
    let capture = f.capture("retained-before-mutation");
    let bytes = read_file(string(capture, "path"));
    let verified = f.verify(capture, &bytes);
    let object = Object::decode(&bytes);
    let root = verified.state_root();
    let universal = prestate_module_primitive(&object.universal);
    let roots: Vec<[u8; 32]> = object.roots.chunks_exact(32).map(|bytes| bytes.try_into().expect("native composite root")).collect();
    verify_composite_roots(&roots, root).expect("actual complete composite proof");
    universal.verify_prefix(root, b"sequence").expect("actual complete universal proof");
    verify_account_tree(&object.accounts, &universal, root).expect("actual complete account proof");
    let mut omitted = roots.clone(); omitted.pop();
    assert!(verify_composite_roots(&omitted, root).is_err());
    let mut bad_universal = universal.clone(); bad_universal.leaves.remove(0);
    assert!(bad_universal.verify_prefix(root, b"sequence").is_err());
    let mut accounts = object.accounts.clone(); accounts.remove(0);
    assert!(verify_account_tree(&accounts, &universal, root).is_err());
    let mut accounts = object.accounts.clone(); accounts.insert(0, object.accounts[0].clone());
    assert!(verify_account_tree(&accounts, &universal, root).is_err());
    let mut accounts = object.accounts.clone();
    accounts[0].account_path.as_mut().expect("real account path").count += 1;
    assert!(verify_account_tree(&accounts, &universal, root).is_err());
    let fresh_capture = f.capture("fresh-after-mutation");
    let fresh_bytes = read_file(string(fresh_capture, "path"));
    let fresh_verified = f.verify(fresh_capture, &fresh_bytes);
    assert_ne!(fresh_verified.state_root(), root);
    let fresh = Object::decode(&fresh_bytes);
    assert!(verify_account_tree(&fresh.accounts, &universal, root).is_err());
    assert!(verify_account_tree(&object.accounts, &prestate_module_primitive(&fresh.universal), root).is_err());
    let encoded = witness_bytes(&object.accounts[0]);
    StateWitness::decode(&encoded).expect("canonical genuine account witness");
    assert!(StateWitness::decode(&encoded[..encoded.len() - 1]).is_err());
    let mut trailing = encoded; trailing.push(0);
    assert!(StateWitness::decode(&trailing).is_err());
    println!("CAPS_CASE prestate-primitives-missing-mixed-and-malformed-refusals");
}

struct ExecutionPrestateFixture {
    inputs: Value,
    history: SequencerHistory,
}

impl ExecutionPrestateFixture {
    fn connect(inputs: &Value) -> (Uds, Handshake) {
        use layerx_client::lni::schema::Capability;
        let mut transport = Uds::connect(Path::new(string(inputs, "socket")), &ConnectionGate::new(4), Limits {
            maximum_frame_bytes: 2 * 1024 * 1024, maximum_connections: 4, maximum_streams: 1,
            maximum_queued_bytes: 4 * 1024 * 1024, deadline: Duration::from_secs(30),
        }).expect("real execution-prestate UDS");
        let accepted = handshake::perform(&mut transport, &HandshakeConfig {
            built_interface_version: Version::V1_9, expected_protocol_version: 3, expected_network_id: 77,
        }, None).expect("native execution-prestate handshake");
        assert_eq!(accepted.node().interface_version, Version::V1_9);
        assert_eq!(accepted.node().authorised_sequencer_key, bytes(inputs, "sequencer_public_key"));
        assert!(accepted.capabilities().contains(Capability::CapsDiscovery));
        assert!(accepted.capabilities().contains(Capability::ExecutionPrestate));
        (transport, accepted)
    }

    fn load() -> Self {
        let path = std::env::var("LAYERX_EXECUTION_PRESTATE_INPUTS")
            .expect("required real execution-prestate fixture; no fixture fallback");
        let inputs: Value = serde_json::from_slice(&read_file(&path)).expect("execution-prestate input manifest");
        assert!(!read_file(string(&inputs, "genesis_manifest")).is_empty());
        let descriptor = read_file(string(&inputs, "genesis_descriptor"));
        assert_eq!(descriptor.len(), 105);
        assert_eq!(&descriptor[..5], b"LXGD\x01");
        assert_eq!(&descriptor[5..9], &77_u32.to_be_bytes());
        let genesis_root = descriptor[41..73].try_into().expect("independent deployment genesis root");
        let public = bytes(&inputs, "sequencer_public_key");
        let id: [u8; 32] = Sha256::digest(format!("layerx-sequencer:{}", string(&inputs, "sequencer_public_key")).as_bytes()).into();
        assert_eq!(id, bytes(&inputs, "sequencer_id"));
        let trust = read_file(string(&inputs, "genesis_trust"));
        let material = layerx_wire::handover::decode_genesis_trust(&trust).expect("real genesis trust artifact");
        let governance = StateWitness::decode(material.governance_witness).expect("committed governance proof");
        assert_eq!(governance.value, bytes(&inputs, "handover_authority_public_key"));
        let mut history = SequencerHistory::from_genesis_artifact(&trust, 77, genesis_root, public)
            .expect("independently pinned genuine genesis authority");
        let (mut transport, accepted) = Self::connect(&inputs);
        let head = accepted.node().latest_sealed_batch;
        assert!(head > 0 && head < 4096, "bounded genuine execution history");
        for batch in 1..=head {
            history.fetch_next(&mut transport, Version::V1_9, 20_000 + batch * 3, RetrievalLimits {
                maximum_bytes: 64 * 1024 * 1024, maximum_chunks: 4096, deadline: Duration::from_secs(30),
            }).unwrap_or_else(|error| panic!("native execution history batch {batch}: {error:?}"));
        }
        let captures = inputs["execution_prestate_captures"].as_array().expect("real execution-prestate captures");
        assert_eq!(captures.len(), 3);
        let names: BTreeSet<_> = captures.iter().map(|capture| string(capture, "name")).collect();
        assert_eq!(names, ["native-lifecycle", "intervening-native", "intervening-call"].into_iter().collect());
        Self { inputs, history }
    }

    fn capture(&self, name: &str) -> &Value {
        self.inputs["execution_prestate_captures"].as_array().expect("captures").iter()
            .find(|capture| string(capture, "name") == name).expect("required native execution capture")
    }

    fn receipt(&self, capture: &Value) -> layerx_proof::receipt::VerifiedReceipt {
        use layerx_proof::{inclusion::verify_receipt, merkle::decode_proof,
            receipt::{verify_outcome_maintained_chain, AuthorizedBatch, MaintainedOutcomeEvidence}};
        let receipt = read_file(string(capture, "receipt_path"));
        let proof = decode_proof(&read_file(string(capture, "proof_path"))).expect("native receipt proof");
        let header = read_file(string(capture, "header_path"));
        let signature: [u8; 64] = read_file(string(capture, "header_signature_path")).try_into().expect("native header signature");
        let checked_header = self.history.verify_header(&header, &signature).expect("maintained header in genuine history");
        let signed = checked_header.header();
        let authorization = self.history.authorization_for_batch(signed.batch_number()).expect("verified historical sequencer authority");
        verify_receipt(&receipt, &proof, &header, &signature, &authorization).expect("selected receipt inclusion");
        let maintenance = read_file(string(capture, "maintenance_path"));
        let maintenance_proof = decode_proof(&read_file(string(capture, "maintenance_proof_path"))).expect("native maintenance proof");
        verify_receipt(&maintenance, &maintenance_proof, &header, &signature, &authorization).expect("maintenance inclusion");
        let record = layerx_wire::batch_maintenance::decode_maintenance(&maintenance).expect("real canonical maintenance");
        record.verify_header(signed).expect("maintenance bound to signed header");
        let decoded = layerx_wire::receipt::decode(&receipt).expect("canonical native receipt");
        let protocol = decoded.protocol().expect("native receipt protocol");
        assert_eq!(protocol.protocol_version(), 3);
        assert_eq!(protocol.module_id(), 9);
        assert_eq!(protocol.result_code(), 0);
        let count = signed.last_sequence().checked_sub(signed.first_sequence()).and_then(|count| u32::try_from(count).ok())
            .expect("bounded maintained activity count");
        let batch_id = layerx_wire::hash::receipt_execution_batch_id_maintenance(protocol, signed, record.occupancy(), count)
            .expect("authenticated native execution identity");
        assert_eq!(protocol.batch_id(), batch_id);
        let sealed = AuthorizedBatch::new(batch_id, protocol.asset(), signed.previous_state_root(),
            signed.resulting_state_root(), authorization.public_key());
        let paths = capture["receipts"].as_array().expect("complete ordered native receipt paths");
        assert_eq!(paths.len(), usize::try_from(count).expect("activity count"));
        assert!(!paths.is_empty() && paths.len() <= 64);
        let receipts = paths.iter().map(|path| read_file(path.as_str().expect("absolute native receipt path"))).collect::<Vec<_>>();
        let evidence = MaintainedOutcomeEvidence { header: &header, header_signature: &signature, activity_proof: &proof,
            maintenance: &maintenance, maintenance_proof: &maintenance_proof, authorization: &authorization };
        let verified = verify_outcome_maintained_chain(&receipt, &sealed, &evidence, &receipts)
            .expect("genuine complete signed receipt root chain");
        let mut missing = receipts.clone(); missing.pop();
        assert!(verify_outcome_maintained_chain(&receipt, &sealed, &evidence, &missing).is_err());
        let mut altered_maintenance = maintenance.clone();
        let last = altered_maintenance.len() - 1; altered_maintenance[last] ^= 1;
        let altered = MaintainedOutcomeEvidence { maintenance: &altered_maintenance, ..evidence };
        assert!(verify_outcome_maintained_chain(&receipt, &sealed, &altered, &receipts).is_err());
        verified
    }
}

#[derive(Clone)]
struct ExecutionPrestateObject {
    identity: Vec<u8>,
    roots: Vec<u8>,
    universal: Range,
    programs: Range,
    accounts: Vec<StateWitness>,
}

impl ExecutionPrestateObject {
    fn decode(bytes: &[u8]) -> Self {
        let mut reader = Reader(bytes);
        let identity = reader.take(78);
        assert_eq!(&identity[..2], &1_u16.to_be_bytes());
        let count = usize::from(reader.u16());
        let roots = reader.take(count * 32);
        let universal = reader.range(); assert_eq!(universal.id, 0);
        let programs = reader.range(); assert_eq!(programs.id, 9);
        let accounts = (0..reader.u32()).map(|_| reader.witness()).collect();
        assert!(reader.0.is_empty());
        let value = Self { identity, roots, universal, programs, accounts };
        assert_eq!(value.encode(), bytes, "lossless native prestate decode");
        value
    }

    fn encode(&self) -> Vec<u8> {
        let mut bytes = self.identity.clone();
        bytes.extend_from_slice(&u16::try_from(self.roots.len() / 32).expect("composite count").to_be_bytes());
        bytes.extend_from_slice(&self.roots);
        self.universal.encode(&mut bytes);
        self.programs.encode(&mut bytes);
        bytes.extend_from_slice(&u32::try_from(self.accounts.len()).expect("account count").to_be_bytes());
        for witness in &self.accounts { vector(&mut bytes, &witness_bytes(witness)); }
        bytes
    }
}

#[test]
fn execution_prestate_authenticated_native_objects() {
    use layerx_client::evidence::{verify_execution_prestate_object, verify_native_execution_prestate_object};
    use layerx_client::execution_prestate::{ExecutionPrestateError, NativeExecutionPrestateDiscovery, NativeExecutionPrestateProgress};
    let fixture = ExecutionPrestateFixture::load();
    let names = ["native-lifecycle", "intervening-native", "intervening-call"];
    let anchors = names.iter().map(|name| fixture.receipt(fixture.capture(name))).collect::<Vec<_>>();
    let sequences = anchors.iter().map(|anchor| anchor.receipt().protocol().expect("verified protocol").global_sequence()).collect::<Vec<_>>();
    assert!(sequences[0] < sequences[1] && sequences[1] < sequences[2]);
    assert!(sequences[0].checked_add(1).is_some_and(|next| next < sequences[2]));
    assert_ne!(anchors[0].receipt().protocol().expect("native lifecycle").operation(), 3);
    assert_eq!(anchors[1].receipt().protocol().expect("first call").operation(), 3);
    assert_eq!(anchors[2].receipt().protocol().expect("second call").operation(), 3);
    {
        use layerx_client::lni::schema::Capability;
        let (mut legacy, accepted) = Fixture::connect(&fixture.inputs);
        assert_eq!(accepted.node().interface_version, Version::V1_8);
        assert!(accepted.capabilities().contains(Capability::CapsDiscovery));
        assert!(!accepted.capabilities().contains(Capability::ExecutionPrestate));
        assert!(matches!(NativeExecutionPrestateDiscovery::begin(&mut legacy, accepted.capabilities(),
            Version::V1_8, 77, 49_000, &anchors[0], 512, Duration::from_secs(30)),
            Err(ExecutionPrestateError::Unavailable)));
        let absent_text = b"did:layerx:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff";
        let mut hash = Sha256::new(); hash.update(b"LXP/v1/did-id\0");
        hash.update(u16::try_from(absent_text.len()).expect("bounded DID").to_be_bytes()); hash.update(absent_text);
        let did: [u8; 32] = hash.finalize().into();
        let node = accepted.node();
        let context = ReadContext { interface_version: Version::V1_8, correlation_id: 49_001,
            expected_protocol_version: 3, expected_network_id: 77, requested: Requested::new(VerificationLevel::STATE_PROVEN),
            head: Head { chain_sequence: node.chain_head_sequence, sealed_batch: node.latest_sealed_batch,
                finalised_checkpoint: node.latest_finalised_checkpoint },
            sequencer_authorization: fixture.history.authorization_for_batch(node.latest_sealed_batch).expect("actual legacy head authority"),
            handshake_sequencer_key: node.authorised_sequencer_key, root_selector: RootSelector::Latest };
        let mut caps = CapsDiscovery::begin(&mut legacy, accepted.capabilities(), context, did, 512,
            Duration::from_secs(30), Some(&fixture.history)).expect("unchanged native tags42/43 caps path");
        let mut complete = false;
        for _ in 0..131_072 {
            match caps.advance() {
                CapsProgress::Incomplete { received_bytes, total_bytes } => assert!(received_bytes > 0 && received_bytes < total_bytes),
                CapsProgress::Complete(value) | CapsProgress::Empty(value) => {
                    assert_eq!(value.did(), did);
                    assert_eq!(value.freshness().global_sequence, node.chain_head_sequence);
                    assert_eq!(value.level(), VerificationLevel::STATE_PROVEN);
                    complete = true; break;
                }
                other => panic!("legacy42/43 discovery failed after newcap refusal: {other:?}"),
            }
        }
        assert!(complete);
        assert!(matches!(caps.advance(), CapsProgress::Refused(CapsError::Terminal)));
        println!("EXECUTION_PRESTATE_CASE real-legacy18-handshake-and-caps42-43");
    }
    let raw = names.iter().map(|name| read_file(string(fixture.capture(name), "object_path"))).collect::<Vec<_>>();
    let objects = raw.iter().map(|bytes| ExecutionPrestateObject::decode(bytes)).collect::<Vec<_>>();
    for (index, ((name, anchor), raw)) in names.iter().zip(&anchors).zip(&raw).enumerate() {
        let native = verify_native_execution_prestate_object(raw, anchor, 77).expect("real signed-receipt prestate proof");
        assert_eq!(native.canonical_bytes(), raw);
        assert_eq!(native.execution_sequence(), sequences[index]);
        assert_eq!(native.state_root(), anchor.receipt().protocol().expect("receipt").previous_state_root());
        assert_eq!(native.activity_id(), anchor.receipt().protocol().expect("receipt").activity_id());
        assert_eq!(native.all_accounts().len(), objects[index].accounts.len());
        assert_eq!(native.program_records().len(), objects[index].programs.leaves.len());
        let unsigned = layerx_wire::receipt::encode_unsigned(anchor.receipt()).expect("native unsigned receipt");
        assert_eq!(native.receipt_digest(), layerx_wire::hash::receipt_digest(&unsigned).expect("native receipt digest"));
        assert_eq!(number(fixture.capture(name), "admission_sequence"), sequences[0]);
        if index == 0 {
            assert!(native.clone().for_program_call(anchor).is_err());
            assert!(verify_execution_prestate_object(raw, anchor, 77).is_err());
        } else {
            let call = verify_execution_prestate_object(raw, anchor, 77).expect("actual Call schedule provenance");
            let converted = native.clone().for_program_call(anchor).expect("same-receipt strict Call conversion");
            assert_eq!(call.selected_fee_asset(), converted.selected_fee_asset());
            assert_ne!(call.selected_fee_asset(), [0; 32]);
            assert_eq!(call.selected_fee_schedule_version(), anchor.receipt().protocol().expect("receipt").program_outcome().expect("real Call outcome").fee_schedule_version());
            assert!(native.clone().for_program_call(&anchors[0]).is_err());
        }
        let other = (index + 1) % names.len();
        assert_ne!(native.state_root(), anchors[other].receipt().protocol().expect("other receipt").previous_state_root());
        assert!(verify_native_execution_prestate_object(raw, &anchors[other], 77).is_err());
        assert!(verify_native_execution_prestate_object(raw, anchor, 78).is_err());
        assert!(verify_native_execution_prestate_object(&raw[..raw.len() - 1], anchor, 77).is_err());
        let mut trailing = raw.clone(); trailing.push(0);
        assert!(verify_native_execution_prestate_object(&trailing, anchor, 77).is_err());
        let refuse = |bad: &ExecutionPrestateObject| {
            assert!(verify_native_execution_prestate_object(&bad.encode(), anchor, 77).is_err(), "accepted altered complete prestate for {name}");
        };
        let original = &objects[index];
        assert!(!original.programs.leaves.is_empty() && !original.universal.leaves.is_empty() && original.accounts.len() > 1);
        for offset in [0, 2, 6, 38, 46] {
            let mut bad = original.clone(); bad.identity[offset] ^= 1; refuse(&bad);
        }
        let mut bad = original.clone(); bad.roots.truncate(bad.roots.len() - 32); refuse(&bad);
        let mut bad = original.clone(); bad.universal.leaves.remove(0); refuse(&bad);
        let mut bad = original.clone(); bad.programs.leaves.remove(0); refuse(&bad);
        let mut bad = original.clone(); bad.programs.leaves.insert(0, original.programs.leaves[0].clone()); refuse(&bad);
        let mut bad = original.clone(); bad.programs.leaves[0].value.push(0); refuse(&bad);
        let mut bad = original.clone(); bad.accounts.remove(0); refuse(&bad);
        let mut bad = original.clone(); bad.accounts.insert(0, original.accounts[0].clone()); refuse(&bad);
        let mut bad = original.clone(); bad.accounts.swap(0, 1); refuse(&bad);
        let mut bad = original.clone(); bad.accounts[0].account_path.as_mut().expect("native account path").count += 1; refuse(&bad);
        let mut bad = original.clone(); bad.accounts = objects[other].accounts.clone(); refuse(&bad);
        let mut bad = original.clone(); bad.programs = objects[other].programs.clone(); refuse(&bad);
        let mut bad = original.clone(); bad.universal = objects[other].universal.clone(); refuse(&bad);
        let (mut transport, accepted) = ExecutionPrestateFixture::connect(&fixture.inputs);
        let mut discovery = NativeExecutionPrestateDiscovery::begin(&mut transport, accepted.capabilities(), Version::V1_9,
            77, 50_000 + index as u64, anchor, 512, Duration::from_secs(30)).expect("actual receipt-selected discovery");
        let mut complete = false;
        for _ in 0..131_072 {
            match discovery.advance() {
                NativeExecutionPrestateProgress::Incomplete { received_bytes, total_bytes } => assert!(received_bytes > 0 && received_bytes < total_bytes),
                NativeExecutionPrestateProgress::Complete(value) => {
                    assert_eq!(value.canonical_bytes(), raw);
                    assert_eq!(value.receipt_digest(), native.receipt_digest()); complete = true; break;
                }
                other => panic!("real native prestate acquisition failed: {other:?}"),
            }
        }
        assert!(complete);
        assert!(matches!(discovery.advance(), NativeExecutionPrestateProgress::Refused(ExecutionPrestateError::Terminal)));
        println!("EXECUTION_PRESTATE_CASE authenticated-{name}-complete-and-negative");
    }
    println!("EXECUTION_PRESTATE_CASE genuine-intervening-receipts");
}
