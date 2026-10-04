use std::{error::Error, path::PathBuf, time::Duration};
use layerx_client::{batch, evidence::RootSelector, head::HeadTracker};
use layerx_client::lni::{handshake::{perform, HandshakeConfig}, schema::Version, transport::{ConnectionGate, Limits, Uds}};
use layerx_client::read::{ReadContext, Requested};
use layerx_platform_core::{build_asset_send_with_identity_sequence, fixed_hex, read_custody_asset, read_custody_profile, treasury_did, SendRequest};
use layerx_proof::inclusion::SequencerAuthorization;
use layerx_types::verify::VerificationLevel;
use sha2::{Digest, Sha256};

fn fixture() -> Result<serde_json::Value, Box<dyn Error>> {
    let path = std::env::var("LAYERX_MULTI_ASSET_RUNTIME_FIXTURE")?;
    Ok(serde_json::from_slice(&std::fs::read(path)?)?)
}

fn field<'a>(fixture: &'a serde_json::Value, name: &str) -> Result<&'a str, Box<dyn Error>> {
    fixture[name].as_str().ok_or_else(|| format!("missing genuine runtime fixture field {name}").into())
}

fn connected(fixture: &serde_json::Value) -> Result<(Uds, ReadContext, layerx_client::lni::capabilities::Capabilities), Box<dyn Error>> {
    let network = u32::try_from(fixture["network_id"].as_u64().ok_or("network_id missing")?)?;
    let gate = ConnectionGate::new(1);
    let socket = PathBuf::from(field(fixture,"run_root")?).join("node/layerxd.lni.sock");
    let mut transport = Uds::connect(&socket, &gate, Limits { maximum_frame_bytes: 1_212_416,
        maximum_connections: 1, maximum_streams: 32, maximum_queued_bytes: 4 * 1_212_416,
        deadline: Duration::from_secs(5) }).map_err(|error| format!("actual LNI connection: {error:?}"))?;
    let handshake = perform(&mut transport, &HandshakeConfig { built_interface_version: Version::V1_8,
        expected_protocol_version: 3, expected_network_id: network }, None)
        .map_err(|error| format!("actual handshake: {error:?}"))?;
    let node = handshake.node();
    let header = batch::lookup(&mut transport, node.interface_version, node.latest_sealed_batch, 2,
        node.authorised_sequencer_key).map_err(|error| format!("actual sealed header: {error:?}"))?;
    let context = ReadContext { interface_version: node.interface_version, correlation_id: 3,
        expected_protocol_version: node.protocol_version, expected_network_id: network,
        requested: Requested::new(VerificationLevel::STATE_PROVEN), head: HeadTracker::new(node).current(),
        sequencer_authorization: SequencerAuthorization::new(header.sequencer_id,
            node.authorised_sequencer_key, header.first_batch_number, header.last_batch_number),
        handshake_sequencer_key: node.authorised_sequencer_key, root_selector: RootSelector::Latest };
    let capabilities = handshake.capabilities().clone();
    Ok((transport, context, capabilities))
}

#[test]
fn genuine_four_asset_registry_and_signed_send() -> Result<(), Box<dyn Error>> {
    let runtime = fixture()?;
    let root = PathBuf::from(field(&runtime,"runtime_root")?);
    let registry = std::fs::read(root.join("genesis/custody.registry"))?;
    assert_eq!(registry.len(),901);
    let seed: [u8;32] = std::fs::read(root.join("keys/value-loop/sender.key"))?.try_into().map_err(|_| "sender seed length")?;
    let recipient_seed: [u8;32] = std::fs::read(root.join("keys/value-loop/recipient.key"))?.try_into().map_err(|_| "recipient seed length")?;
    let sender = treasury_did(&seed);
    for (index,symbol) in ["PAX","SID","USDC","USDL"].iter().enumerate() {
        let asset: [u8;32] = Sha256::digest(format!("layerx-asset:125:{symbol}").as_bytes()).into();
        let (mut transport,context,capabilities) = connected(&runtime)?;
        let actor = layerx_types::ids::Did::new(sender.as_bytes()).map_err(|error| format!("actor: {error:?}"))?;
        let preparation_context = layerx_client::lni::preparation::PreparationStateContext { interface_version: context.interface_version, expected_network_id: context.expected_network_id, minimum_observed_head: context.head.chain_sequence, correlation_id: 20 };
        let before = layerx_client::lni::preparation::preparation_state(&mut transport, &actor, preparation_context).map_err(|error| format!("actual actor preparation: {error:?}"))?;
        let sealed = caps(&mut transport, &capabilities, context, &actor)?;
        let effective = sealed.effective_asset(asset).map_err(|error| format!("effective asset evidence: {error:?}"))?;
        let authenticated = read_custody_asset(&mut transport,context,asset,&sender,&effective)?;
        let after = layerx_client::lni::preparation::preparation_state(&mut transport, &actor, layerx_client::lni::preparation::PreparationStateContext { correlation_id: 21, ..preparation_context }).map_err(|error| format!("actual actor preparation after: {error:?}"))?;
        assert_eq!(before,after);
        assert_eq!(before.observed_state_root,authenticated.state_root());
        assert_eq!(before.observed_head_sequence,context.head.chain_sequence);
        assert_eq!(authenticated.profile().as_slice(), &registry[6+index*224..229+index*224]);
        let request = SendRequest { network_id: context.expected_network_id, source_did: sender.clone(),
            destination_did: treasury_did(&recipient_seed), asset, amount: 1,
            account_sequence: authenticated.account_sequence(), idempotency_key: [17;32],
            not_before_ms: 1, expires_at_ms: 2, fee_limit: 10_000 };
        let signed = build_asset_send_with_identity_sequence(&seed,before.account_sequence,&request,&authenticated)?;
        let (module_registry,_) = layerx_platform_core::asset_registry()?;
        let decoded = layerx_wire::activity::decode_signed(&signed.canonical,&module_registry)
            .map_err(|error| format!("production signed decoder: {error:?}"))?;
        assert_eq!(layerx_wire::hash::activity_id(&decoded).map_err(|error| format!("activity id: {error:?}"))?,signed.activity_id);
        assert_ne!(signed.source_account,layerx_platform_core::main_account(&sender)?);
        for wrong in ["asset","network","source","sequence"] {
            let mut invalid = request.clone();
            match wrong { "asset"=>invalid.asset=[0;32], "network"=>invalid.network_id ^= 1,
                "source"=>invalid.source_did=invalid.destination_did.clone(), _=>invalid.account_sequence=invalid.account_sequence.checked_add(1).ok_or("sequence overflow")? }
            assert!(build_asset_send_with_identity_sequence(&seed,before.account_sequence,&invalid,&authenticated).is_err());
        }
        assert!(build_asset_send_with_identity_sequence(&recipient_seed,before.account_sequence,&request,&authenticated).is_err());
    }
    Ok(())
}

#[test]
fn actual_registry_refuses_unknown_asset_and_unverified_context() -> Result<(), Box<dyn Error>> {
    let runtime=fixture()?;
    let (mut transport,context,capabilities)=connected(&runtime)?;
    let root=PathBuf::from(field(&runtime,"runtime_root")?);
    let seed:[u8;32]=std::fs::read(root.join("keys/value-loop/sender.key"))?.try_into().map_err(|_| "seed length")?;
    let did=treasury_did(&seed);
    let actor=layerx_types::ids::Did::new(did.as_bytes()).map_err(|error| format!("actor: {error:?}"))?;
    let sealed=caps(&mut transport,&capabilities,context,&actor)?;
    let asset:[u8;32]=Sha256::digest(b"layerx-asset:125:PAX").into();
    let effective=sealed.effective_asset(asset).map_err(|error| format!("effective asset: {error:?}"))?;
    assert!(read_custody_profile(&mut transport,context,fixed_hex::<32>("unknown",&"00".repeat(32))?,&effective).is_err());
    let context=ReadContext { requested:Requested::new(VerificationLevel::UNVERIFIED),..context };
    assert!(read_custody_profile(&mut transport,context,asset,&effective).is_err());
    Ok(())
}

fn caps(transport:&mut Uds, capabilities:&layerx_client::lni::capabilities::Capabilities,
    context:ReadContext, actor:&layerx_types::ids::Did) -> Result<layerx_client::evidence::VerifiedCaps,Box<dyn Error>> {
    let did=layerx_wire::hash::did_id_for_protocol(actor,3).map_err(|error| format!("caps DID: {error:?}"))?;
    let mut discovery=layerx_client::caps::CapsDiscovery::begin(transport,capabilities,context,did,1_048_576,Duration::from_secs(5),None)
        .map_err(|error| format!("genuine caps discovery: {error:?}"))?;
    loop { match discovery.advance() {
        layerx_client::caps::CapsProgress::Incomplete { .. }=>{},
        layerx_client::caps::CapsProgress::Complete(sealed)|layerx_client::caps::CapsProgress::Empty(sealed)=>return Ok(sealed),
        other=>return Err(format!("genuine caps incomplete: {other:?}").into()),
    } }
}
