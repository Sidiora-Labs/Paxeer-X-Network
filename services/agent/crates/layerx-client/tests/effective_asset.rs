use layerx_client::{
    client::{ClientConfig, ReconnectPolicy},
    evidence::{verify_caps_object, AssetSourceKind, RootSelector},
    head::Head,
    lni::{
        handshake::{perform, HandshakeConfig},
        schema::{decode_envelope, encode_envelope, Envelope, Version},
        transport::{ConnectionGate, FrameTransport, Limits, Uds},
    },
    read::{ReadContext, Requested},
    Client,
};
use layerx_proof::{inclusion::SequencerAuthorization, state_witness::StateWitness};
use layerx_types::verify::VerificationLevel;
use serde_json::Value;
use std::{fs, path::PathBuf, time::Duration};
const VERSION: Version = Version::V1_9;
fn text<'a>(value: &'a Value, key: &str) -> &'a str {
    value[key]
        .as_str()
        .unwrap_or_else(|| panic!("missing native field {key}"))
}
fn id(value: &Value, key: &str) -> [u8; 32] {
    let hex = text(value, key);
    assert_eq!(hex.len(), 64);
    let mut bytes = [0; 32];
    for (index, byte) in bytes.iter_mut().enumerate() {
        *byte = u8::from_str_radix(&hex[index * 2..index * 2 + 2], 16).expect("native identity");
    }
    bytes
}
fn number(bytes: &[u8], offset: usize) -> u32 {
    u32::from_be_bytes(
        bytes[offset..offset + 4]
            .try_into()
            .expect("bounded number"),
    )
}
fn config(manifest: &Value) -> ClientConfig {
    ClientConfig {
        endpoint: PathBuf::from(text(manifest, "endpoint")),
        handshake: HandshakeConfig {
            built_interface_version: VERSION,
            expected_protocol_version: 3,
            expected_network_id: u32::try_from(manifest["network_id"].as_u64().expect("network"))
                .expect("network width"),
        },
        limits: Limits {
            maximum_frame_bytes: 512,
            maximum_connections: 1,
            maximum_streams: 1,
            maximum_queued_bytes: 4096,
            deadline: Duration::from_secs(10),
        },
        reconnect: ReconnectPolicy {
            maximum_attempts: 1,
            base_delay: Duration::from_millis(1),
            maximum_delay: Duration::from_millis(1),
            jitter_percent: 0,
        },
    }
}
fn live_object(
    manifest: &Value,
    authorization: SequencerAuthorization,
) -> (Vec<u8>, [u8; 32], ReadContext) {
    let configured = config(manifest);
    let gate = ConnectionGate::new(1);
    let mut transport = Uds::connect(&configured.endpoint, &gate, configured.limits)
        .expect("actual native transport");
    let accepted =
        perform(&mut transport, &configured.handshake, None).expect("actual native handshake");
    assert_eq!(
        accepted.node().authorised_sequencer_key,
        id(manifest, "sequencer_public_key")
    );
    let context = ReadContext {
        interface_version: VERSION,
        correlation_id: 71,
        expected_protocol_version: 3,
        expected_network_id: configured.handshake.expected_network_id,
        requested: Requested::new(VerificationLevel::STATE_PROVEN),
        head: Head {
            chain_sequence: accepted.node().chain_head_sequence,
            sealed_batch: accepted.node().latest_sealed_batch,
            finalised_checkpoint: accepted.node().latest_finalised_checkpoint,
        },
        sequencer_authorization: authorization,
        handshake_sequencer_key: accepted.node().authorised_sequencer_key,
        root_selector: RootSelector::Latest,
    };
    let mut request = [0; 177];
    request[..2].copy_from_slice(&1_u16.to_be_bytes());
    request[3..7].copy_from_slice(&context.expected_network_id.to_be_bytes());
    request[7..39].copy_from_slice(&id(manifest, "did"));
    request[39] = 1;
    request[72] = 3;
    request[73..77].copy_from_slice(&299_u32.to_be_bytes());
    let mut raw = Vec::new();
    let mut root = [0; 32];
    let mut total = None;
    for _ in 0..131072 {
        transport
            .send(
                &encode_envelope(Envelope {
                    version: VERSION,
                    message_tag: 42,
                    correlation_id: 71,
                    canonical_payload: &request,
                    proof_material: &[],
                })
                .expect("request encode"),
            )
            .expect("actual request");
        let frame = transport.receive().expect("actual page");
        let reply = decode_envelope(&frame).expect("page envelope");
        assert_eq!(reply.message_tag, 43);
        assert_eq!(reply.correlation_id, 71);
        assert_eq!(reply.version, VERSION);
        assert!(reply.proof_material.is_empty());
        let page = reply.canonical_payload;
        assert!(page.len() >= 119);
        assert_eq!(&page[..2], &[0, 1]);
        assert_eq!(number(page, 34), context.expected_network_id);
        assert_eq!(number(page, 70) as usize, raw.len());
        let length = number(page, 115) as usize;
        assert_eq!(length, page.len() - 119);
        assert!(length > 0 && length <= 299);
        if raw.is_empty() {
            root.copy_from_slice(&page[38..70]);
            total = Some(number(page, 74));
        } else {
            assert_eq!(&page[38..70], &root);
            assert_eq!(Some(number(page, 74)), total);
        }
        assert_eq!(number(page, 78), number(page, 70) + number(page, 115));
        assert!(number(page, 78) <= number(page, 74));
        raw.extend_from_slice(&page[119..]);
        if page[82] == 1 {
            assert_eq!(raw.len(), total.expect("total") as usize);
            assert_eq!(&page[83..115], &[0; 32]);
            return (raw, root, context);
        }
        assert_eq!(page[82], 0);
        assert!(raw.len() < total.expect("total") as usize);
        request[2] = 1;
        request[77..109].copy_from_slice(&page[2..34]);
        request[109..141].copy_from_slice(&root);
        request[141..145].copy_from_slice(&number(page, 78).to_be_bytes());
        request[145..177].copy_from_slice(&page[83..115]);
    }
    panic!("genuine bounded paging failed to terminate")
}
struct Reader<'a>(&'a [u8]);
impl Reader<'_> {
    fn take(&mut self, count: usize) -> Vec<u8> {
        assert!(count <= self.0.len(), "truncated genuine object");
        let result = self.0[..count].to_vec();
        self.0 = &self.0[count..];
        result
    }
    fn u16(&mut self) -> u16 {
        u16::from_be_bytes(self.take(2).try_into().expect("u16"))
    }
    fn u32(&mut self) -> u32 {
        u32::from_be_bytes(self.take(4).try_into().expect("u32"))
    }
    fn vector(&mut self) -> Vec<u8> {
        let count = self.u32() as usize;
        self.take(count)
    }
    fn witness(&mut self) -> StateWitness {
        StateWitness::decode(&self.vector()).expect("genuine state witness")
    }
    fn range(&mut self) -> Range {
        let id = self.u16();
        let root = self.take(32);
        let index = self.u32();
        let count = self.u32();
        let depth = self.take(1)[0];
        let siblings = self.take(usize::from(depth) * 32);
        let leaves = (0..self.u32()).map(|_| self.witness()).collect();
        Range {
            id,
            root,
            index,
            count,
            siblings,
            leaves,
        }
    }
}
#[derive(Clone)]
struct Range {
    id: u16,
    root: Vec<u8>,
    index: u32,
    count: u32,
    siblings: Vec<u8>,
    leaves: Vec<StateWitness>,
}
#[derive(Clone)]
struct Object {
    value: Vec<u8>,
    proof: Vec<u8>,
    roots: Vec<u8>,
    universal: Range,
    budget: Range,
    grant: Range,
    accounts: Vec<StateWitness>,
}
fn vector(out: &mut Vec<u8>, value: &[u8]) {
    out.extend_from_slice(
        &u32::try_from(value.len())
            .expect("bounded vector")
            .to_be_bytes(),
    );
    out.extend_from_slice(value);
}
fn path(out: &mut Vec<u8>, siblings: &[[u8; 32]]) {
    out.push(u8::try_from(siblings.len()).expect("bounded path"));
    for sibling in siblings {
        out.extend_from_slice(sibling);
    }
}
fn witness_bytes(witness: &StateWitness) -> Vec<u8> {
    let mut out = 2_u16.to_be_bytes().to_vec();
    out.extend_from_slice(&witness.module_id.to_be_bytes());
    vector(&mut out, &witness.key);
    vector(&mut out, &witness.value);
    if let Some(account) = &witness.account_path {
        out.extend_from_slice(&account.index.to_be_bytes());
        out.extend_from_slice(&account.count.to_be_bytes());
        path(&mut out, &account.siblings);
    }
    out.extend_from_slice(&witness.leaf_index_a.to_be_bytes());
    out.extend_from_slice(&witness.leaf_count_a.to_be_bytes());
    path(&mut out, &witness.siblings_a);
    out.extend_from_slice(&witness.leaf_count_b.to_be_bytes());
    path(&mut out, &witness.siblings_b);
    out
}
impl Range {
    fn encode(&self, out: &mut Vec<u8>) {
        out.extend_from_slice(&self.id.to_be_bytes());
        out.extend_from_slice(&self.root);
        out.extend_from_slice(&self.index.to_be_bytes());
        out.extend_from_slice(&self.count.to_be_bytes());
        out.push(u8::try_from(self.siblings.len() / 32).expect("depth"));
        out.extend_from_slice(&self.siblings);
        out.extend_from_slice(
            &u32::try_from(self.leaves.len())
                .expect("count")
                .to_be_bytes(),
        );
        for leaf in &self.leaves {
            vector(out, &witness_bytes(leaf));
        }
    }
}
impl Object {
    fn decode(bytes: &[u8]) -> Self {
        let mut reader = Reader(bytes);
        assert_eq!(reader.u16(), 1);
        let value = reader.vector();
        let proof = reader.vector();
        let count = usize::from(reader.u16());
        let roots = reader.take(count * 32);
        let universal = reader.range();
        assert_eq!(reader.take(1), [2]);
        let budget = reader.range();
        let grant = reader.range();
        let accounts = (0..reader.u32()).map(|_| reader.witness()).collect();
        assert!(reader.0.is_empty());
        let result = Self {
            value,
            proof,
            roots,
            universal,
            budget,
            grant,
            accounts,
        };
        assert_eq!(result.encode(), bytes, "lossless parsing of native capture");
        result
    }
    fn encode(&self) -> Vec<u8> {
        let mut out = 1_u16.to_be_bytes().to_vec();
        vector(&mut out, &self.value);
        vector(&mut out, &self.proof);
        out.extend_from_slice(
            &u16::try_from(self.roots.len() / 32)
                .expect("roots")
                .to_be_bytes(),
        );
        out.extend_from_slice(&self.roots);
        self.universal.encode(&mut out);
        out.push(2);
        self.budget.encode(&mut out);
        self.grant.encode(&mut out);
        out.extend_from_slice(
            &u32::try_from(self.accounts.len())
                .expect("accounts")
                .to_be_bytes(),
        );
        for account in &self.accounts {
            vector(&mut out, &witness_bytes(account));
        }
        out
    }
}

#[test]
fn real_native_effective_asset_transport() {
    let phase = std::env::var("LAYERX_ASSET_INVENTORY_PHASE").expect("actual mutation phase");
    assert!(phase == "initial" || phase == "updated");
    let path = std::env::var("LAYERX_ASSET_INVENTORY_INPUTS").expect("actual native manifest");
    let manifest: Value =
        serde_json::from_slice(&fs::read(path).expect("native manifest")).expect("native JSON");
    let authorization = SequencerAuthorization::new(
        id(&manifest, "sequencer_id"),
        id(&manifest, "sequencer_public_key"),
        manifest["first_batch_number"]
            .as_u64()
            .expect("first batch"),
        manifest["last_batch_number"].as_u64().expect("last batch"),
    );
    let mut client = Client::connect(config(&manifest)).expect("actual credentialed connection");
    let caps = client
        .caps_discovery(
            id(&manifest, "did"),
            VerificationLevel::STATE_PROVEN,
            21,
            authorization,
            None,
        )
        .expect("closed complete live CAPS");
    let asset_id = id(&manifest, "asset_id");
    let asset = caps
        .effective_asset(asset_id)
        .expect("authenticated effective record");
    assert_eq!(asset.asset_id(), asset_id);
    assert!(asset.registered());
    assert_eq!(asset.paused(), phase == "updated");
    assert_eq!(
        asset.source_kind(),
        if phase == "initial" {
            AssetSourceKind::Initial
        } else {
            AssetSourceKind::Mutable
        }
    );
    assert_eq!(
        asset.canonical_bytes(),
        fs::read(text(&manifest, "expected_record_path")).expect("actual native selected record")
    );
    assert_eq!(asset.state_root(), caps.state_root());
    assert_eq!(asset.level(), VerificationLevel::STATE_PROVEN);
    assert_eq!(asset.freshness(), caps.freshness());
    assert_eq!(asset.metadata().asset_id, asset_id);
    assert_eq!(asset.metadata().paused, phase == "updated");
    assert!(!asset.metadata().symbol.is_empty());
    assert!(
        caps.effective_asset([0xfe; 32]).is_err(),
        "unknown assets never use initial fallback"
    );
    drop(client);
    let (raw, root, context) = live_object(&manifest, authorization);
    assert_eq!(root, caps.state_root());
    let parsed = Object::decode(&raw);
    let verify =
        |bytes: &[u8]| verify_caps_object(bytes, id(&manifest, "did"), root, context, None);
    let independent = verify(&raw).expect("independent native fullinventory");
    assert_eq!(
        independent
            .effective_asset(asset_id)
            .expect("same sealed selector")
            .canonical_bytes(),
        asset.canonical_bytes()
    );
    let initial = parsed
        .grant
        .leaves
        .iter()
        .position(|leaf| leaf.key == asset_id)
        .expect("actual initial committed record");
    let mutable_key = [b"asset:".as_slice(), asset_id.as_slice()].concat();
    let mutable = parsed
        .grant
        .leaves
        .iter()
        .position(|leaf| leaf.key == mutable_key);
    assert_eq!(
        mutable.is_some(),
        phase == "updated",
        "initial absence and actual override bound to same root"
    );
    for index in [initial, mutable.unwrap_or(initial)] {
        let mut bad = parsed.clone();
        bad.grant.leaves.remove(index);
        assert!(
            verify(&bad.encode()).is_err(),
            "omission cannot prove mutable absence or initial record"
        );
        let mut bad = parsed.clone();
        bad.grant.leaves[index].value[0] ^= 1;
        assert!(
            verify(&bad.encode()).is_err(),
            "record bytes are authenticated"
        );
        let mut bad = parsed.clone();
        bad.grant.leaves[index].leaf_count_a += 1;
        assert!(
            verify(&bad.encode()).is_err(),
            "inventory completeness count"
        );
        let mut bad = parsed.clone();
        bad.grant
            .leaves
            .insert(index, bad.grant.leaves[index].clone());
        assert!(verify(&bad.encode()).is_err(), "duplicate leaf refused");
    }
    if parsed.grant.leaves.len() > 1 {
        let mut bad = parsed.clone();
        bad.grant.leaves.swap(0, 1);
        assert!(verify(&bad.encode()).is_err(), "strict canonical ordering");
    }
    let mut bad = parsed.clone();
    bad.grant.root[0] ^= 1;
    assert!(verify(&bad.encode()).is_err(), "subtree root substitution");
    let mut wrong = context;
    wrong.expected_network_id += 1;
    assert!(
        verify_caps_object(&raw, id(&manifest, "did"), root, wrong, None).is_err(),
        "network refusal"
    );
    let mut wrong = context;
    wrong.head.chain_sequence += 1;
    assert!(
        verify_caps_object(&raw, id(&manifest, "did"), root, wrong, None).is_err(),
        "stale head refusal"
    );
    let mut wrong = context;
    wrong.handshake_sequencer_key[0] ^= 1;
    assert!(
        verify_caps_object(&raw, id(&manifest, "did"), root, wrong, None).is_err(),
        "signing authority refusal"
    );
    assert!(
        verify_caps_object(&raw, id(&manifest, "did"), [0xaa; 32], context, None).is_err(),
        "root refusal"
    );
    assert!(verify(&raw[..raw.len() - 1]).is_err(), "truncation refusal");
    println!("ASSET_INVENTORY_CASE {phase}-actual-caps-effective-record-fullinventory-refusals");
}
