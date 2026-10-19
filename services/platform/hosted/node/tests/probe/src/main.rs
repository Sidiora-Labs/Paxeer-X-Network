//! Real-process probe for the beta node: performs the LNI handshake with
//! `layerx-client`, reads an account balance, and talks to the supervisor
//! socket. Every result is printed as one JSON line on stdout.

use std::collections::BTreeMap;
use std::fmt::Write as _;
use std::io::{BufRead, BufReader, Read, Write};
use std::os::unix::fs::OpenOptionsExt;
use std::os::unix::net::UnixStream;
use std::path::PathBuf;
use std::process::ExitCode;
use std::time::Duration;

use layerx_client::batch::{self, BatchHeaderError, SignedBatchHeader};
use layerx_client::client::{Client, ClientConfig, ReconnectPolicy};
use layerx_client::evidence::RootSelector;
use layerx_client::head::HeadTracker;
use layerx_client::lni::handshake::{perform, HandshakeConfig};
use layerx_client::lni::schema::{Capability, Version};
use layerx_client::lni::transport::{ConnectionGate, Limits, Uds};
use layerx_client::read::{ReadContext, ReadError, Requested};
use layerx_proof::inclusion::SequencerAuthorization;
use layerx_types::account::AccountId;
use layerx_types::ids::Did;
use layerx_types::verify::VerificationLevel;

const FRAME_BYTES: usize = 1_212_416;

fn usage() -> ExitCode {
    eprintln!(
        "usage: layerx-node-probe handshake --socket PATH --network-id N\n       layerx-node-probe balance --socket PATH --network-id N --account NAME --asset HEX64\n       layerx-node-probe did-accounts --socket PATH --network-id N --did DID\n       layerx-node-probe supervisor --socket PATH --request reset|status\n       layerx-node-probe custody-asset-profile --socket PATH --network-id N --asset HEX64 [--source-did DID --destination-did DID --profile-output ABS]\n       layerx-node-probe write-asset-send --socket PATH --network-id N --seed-file PATH --destination-did DID --asset HEX64 --output PATH [--source-did DID --amount N]"
    );
    ExitCode::from(2)
}

fn options(arguments: &[String]) -> Result<BTreeMap<String, String>, String> {
    let mut parsed = BTreeMap::new();
    let mut index = 0;
    while index < arguments.len() {
        let key = &arguments[index];
        let Some(name) = key.strip_prefix("--") else {
            return Err(format!("unexpected argument {key}"));
        };
        let Some(value) = arguments.get(index + 1) else {
            return Err(format!("missing value for --{name}"));
        };
        parsed.insert(name.to_owned(), value.clone());
        index += 2;
    }
    Ok(parsed)
}

fn required<'a>(parsed: &'a BTreeMap<String, String>, name: &str) -> Result<&'a str, String> {
    parsed
        .get(name)
        .map(String::as_str)
        .ok_or_else(|| format!("--{name} is required"))
}

fn hex(bytes: &[u8]) -> String {
    let mut text = String::with_capacity(bytes.len() * 2);
    for byte in bytes {
        let _ = write!(text, "{byte:02x}");
    }
    text
}

fn hex32(text: &str) -> Result<[u8; 32], String> {
    if text.len() != 64 {
        return Err("expected 64 hex characters".to_owned());
    }
    let mut out = [0u8; 32];
    for (index, chunk) in text.as_bytes().chunks(2).enumerate() {
        let pair = std::str::from_utf8(chunk).map_err(|error| error.to_string())?;
        out[index] = u8::from_str_radix(pair, 16).map_err(|error| error.to_string())?;
    }
    Ok(out)
}

fn limits() -> Limits {
    Limits {
        maximum_frame_bytes: FRAME_BYTES,
        maximum_connections: 4,
        maximum_streams: 32,
        maximum_queued_bytes: 4 * FRAME_BYTES,
        deadline: Duration::from_secs(5),
    }
}

fn handshake_config(built: Version, network_id: u32) -> HandshakeConfig {
    HandshakeConfig {
        built_interface_version: built,
        expected_protocol_version: layerx_wire::limits::STATE_COMMITMENT_PROTOCOL_VERSION,
        expected_network_id: network_id,
    }
}

fn connect(socket: &str, network_id: u32) -> Result<Client, String> {
    Client::connect(ClientConfig {
        endpoint: PathBuf::from(socket),
        handshake: handshake_config(Version::V1_3, network_id),
        limits: limits(),
        reconnect: ReconnectPolicy {
            maximum_attempts: 3,
            base_delay: Duration::from_millis(50),
            maximum_delay: Duration::from_millis(500),
            jitter_percent: 10,
        },
    })
    .map_err(|error| format!("lni connect failed: {error:?}"))
}

fn handshake(parsed: &BTreeMap<String, String>) -> Result<String, String> {
    let socket = required(parsed, "socket")?;
    let network_id: u32 = required(parsed, "network-id")?
        .parse()
        .map_err(|error| format!("--network-id: {error}"))?;
    let client = connect(socket, network_id)?;
    let node = client.handshake().node();
    let capabilities: Vec<String> = client
        .handshake()
        .capabilities()
        .available()
        .iter()
        .map(|capability| format!("{capability:?}"))
        .collect();
    Ok(format!(
        "{{\"network_id\":{},\"protocol_version\":{},\"interface_version\":\"{}.{}\",\"role\":\"{:?}\",\"chain_head_sequence\":{},\"latest_sealed_batch\":{},\"sequencer_public_key\":\"{}\",\"capabilities\":[{}]}}",
        node.network_id,
        node.protocol_version,
        node.interface_version.major,
        node.interface_version.minor,
        node.role,
        node.chain_head_sequence,
        node.latest_sealed_batch,
        hex(&node.authorised_sequencer_key),
        capabilities
            .iter()
            .map(|name| format!("\"{name}\""))
            .collect::<Vec<_>>()
            .join(",")
    ))
}

// Read evidence is bound to the signing terms of the latest sealed batch header, so they are
// taken from that header; before the first seal no state can be answered with evidence.
fn sealed_authorization(
    sequencer_key: [u8; 32],
    sealed: u64,
    header: impl FnOnce(u64) -> Result<SignedBatchHeader, BatchHeaderError>,
) -> Result<SequencerAuthorization, String> {
    if sealed == 0 {
        return Ok(SequencerAuthorization::new(
            sequencer_key,
            sequencer_key,
            1,
            1,
        ));
    }
    let header = header(sealed).map_err(|error| format!("batch header {sealed}: {error:?}"))?;
    Ok(SequencerAuthorization::new(
        header.sequencer_id,
        sequencer_key,
        header.first_batch_number,
        header.last_batch_number,
    ))
}

fn balance(parsed: &BTreeMap<String, String>) -> Result<String, String> {
    let socket = required(parsed, "socket")?;
    let network_id: u32 = required(parsed, "network-id")?
        .parse()
        .map_err(|error| format!("--network-id: {error}"))?;
    let account = AccountId::parse(required(parsed, "account")?)
        .map_err(|error| format!("--account: {error:?}"))?;
    let asset = hex32(required(parsed, "asset")?)?;
    let mut client = connect(socket, network_id)?;
    let node = client.handshake().node();
    let account_id = layerx_wire::hash::account_id_for_protocol(&account, node.protocol_version)
        .map_err(|error| format!("account id: {error:?}"))?;
    let sequencer_key = node.authorised_sequencer_key;
    let sealed = node.latest_sealed_batch;
    let authorization =
        sealed_authorization(sequencer_key, sealed, |batch| client.batch_header(batch, 2))?;
    let read = match client.balance(
        account_id,
        asset,
        VerificationLevel::UNVERIFIED,
        1,
        authorization,
    ) {
        Ok(read) => read,
        Err(ReadError::CoreRefusal { class, result }) => {
            return Ok(format!(
                "{{\"account\":\"{}\",\"account_id\":\"{}\",\"asset\":\"{}\",\"refused\":{{\"class\":{class},\"result\":{}}}}}",
                account.canonical(),
                hex(&account_id),
                hex(&asset),
                result.raw()
            ));
        }
        Err(error) => return Err(format!("balance read failed: {error:?}")),
    };
    Ok(format!(
        "{{\"account\":\"{}\",\"account_id\":\"{}\",\"asset\":\"{}\",\"balance\":\"{}\",\"achieved\":\"{:?}\",\"global_sequence\":{}}}",
        account.canonical(),
        hex(&read.account),
        hex(&read.asset),
        read.amount.value(),
        read.achieved(),
        read.freshness().global_sequence
    ))
}

fn did_accounts(parsed: &BTreeMap<String, String>) -> Result<String, String> {
    let socket = required(parsed, "socket")?;
    let network_id: u32 = required(parsed, "network-id")?
        .parse()
        .map_err(|error| format!("--network-id: {error}"))?;
    let did = required(parsed, "did")?;
    let did_value = Did::new(did.as_bytes()).map_err(|error| format!("--did: {error:?}"))?;
    let gate = ConnectionGate::new(1);
    let mut transport = Uds::connect(&PathBuf::from(socket), &gate, limits())
        .map_err(|error| format!("lni connect failed: {error:?}"))?;
    let handshake = perform(
        &mut transport,
        &handshake_config(Version::V1_6, network_id),
        None,
    )
    .map_err(|error| format!("lni handshake failed: {error:?}"))?;
    let node = handshake.node();
    let sequencer_key = node.authorised_sequencer_key;
    let sequencer_authorization =
        sealed_authorization(sequencer_key, node.latest_sealed_batch, |sealed| {
            batch::lookup(
                &mut transport,
                node.interface_version,
                sealed,
                2,
                sequencer_key,
            )
        })?;
    let context = ReadContext {
        interface_version: node.interface_version,
        correlation_id: 1,
        expected_protocol_version: node.protocol_version,
        expected_network_id: network_id,
        requested: Requested::new(VerificationLevel::UNVERIFIED),
        head: HeadTracker::new(node).current(),
        sequencer_authorization,
        handshake_sequencer_key: sequencer_key,
        root_selector: RootSelector::Latest,
    };
    let values = layerx_client::read::did_accounts(&mut transport, &did_value, context)
        .map_err(|error| format!("did account listing failed: {error:?}"))?;
    let mut names = Vec::with_capacity(values.len());
    for value in &values {
        let bytes = value.canonical_bytes();
        let name = bytes
            .get(..2)
            .map(|v| usize::from(u16::from_be_bytes([v[0], v[1]])))
            .and_then(|length| bytes.get(2..2 + length))
            .and_then(|name| std::str::from_utf8(name).ok())
            .ok_or_else(|| "malformed account name in listing".to_owned())?;
        names.push(format!("\"{name}\""));
    }
    Ok(format!(
        "{{\"did\":\"{did}\",\"count\":{},\"accounts\":[{}]}}",
        values.len(),
        names.join(",")
    ))
}

fn supervisor(parsed: &BTreeMap<String, String>) -> Result<String, String> {
    let socket = required(parsed, "socket")?;
    let request = required(parsed, "request")?;
    if request != "reset" && request != "status" {
        return Err("--request must be reset or status".to_owned());
    }
    let mut stream = UnixStream::connect(socket)
        .map_err(|error| format!("supervisor connect failed: {error}"))?;
    stream
        .set_read_timeout(Some(Duration::from_secs(330)))
        .map_err(|error| error.to_string())?;
    stream
        .write_all(format!("{request}\n").as_bytes())
        .map_err(|error| format!("supervisor write failed: {error}"))?;
    let mut reply = String::new();
    BufReader::new(stream)
        .read_line(&mut reply)
        .map_err(|error| format!("supervisor read failed: {error}"))?;
    let reply = reply.trim_end().to_owned();
    if reply.is_empty() {
        return Err("supervisor closed without a reply".to_owned());
    }
    Ok(reply)
}

fn write_send(parsed: &BTreeMap<String, String>) -> Result<String, String> {
    let network_id = required(parsed, "network-id")?
        .parse::<u32>()
        .map_err(|error| error.to_string())?;
    let seed_file = std::fs::OpenOptions::new()
        .read(true)
        .custom_flags(libc::O_NONBLOCK | libc::O_NOFOLLOW)
        .open(required(parsed, "seed-file")?)
        .map_err(|error| error.to_string())?;
    let metadata = seed_file.metadata().map_err(|error| error.to_string())?;
    if !metadata.is_file() || metadata.len() != 32 {
        return Err("test seed must be a 32-byte regular file".to_owned());
    }
    let mut seed_bytes = Vec::new();
    seed_file
        .take(33)
        .read_to_end(&mut seed_bytes)
        .map_err(|error| error.to_string())?;
    let seed: [u8; 32] = seed_bytes
        .try_into()
        .map_err(|_| "test seed must be 32 bytes".to_owned())?;
    let source_did = layerx_platform_core::treasury_did(&seed);
    let actor = layerx_types::ids::Did::new(source_did.as_bytes())
        .map_err(|error| format!("actor: {error:?}"))?;
    let mut client = connect(required(parsed, "socket")?, network_id)?;
    let state = client
        .preparation_state(&actor, 1)
        .map_err(|error| format!("preparation: {error:?}"))?;
    let signed = layerx_platform_core::build_send(
        &seed,
        &layerx_platform_core::SendRequest {
            network_id,
            source_did,
            destination_did: required(parsed, "destination-did")?.to_owned(),
            asset: hex32(required(parsed, "asset")?)?,
            amount: 1,
            account_sequence: state
                .account_sequence
                .checked_add(1)
                .ok_or("sequence exhausted")?,
            idempotency_key: [0x61; 32],
            not_before_ms: state.protocol_timestamp,
            expires_at_ms: state
                .protocol_timestamp
                .checked_add(300_000)
                .ok_or("timestamp exhausted")?,
            fee_limit: 1000,
        },
    )?;
    let mut output = std::fs::OpenOptions::new()
        .write(true)
        .create_new(true)
        .mode(0o600)
        .open(required(parsed, "output")?)
        .map_err(|error| error.to_string())?;
    output
        .write_all(&signed.canonical)
        .map_err(|error| error.to_string())?;
    output.sync_all().map_err(|error| error.to_string())?;
    Ok(hex(&signed.activity_id))
}

struct CustodySession {
    transport: Uds,
    context: ReadContext,
    state_root: [u8; 32],
    capabilities: layerx_client::lni::Capabilities,
}

fn custody_session(parsed: &BTreeMap<String, String>) -> Result<CustodySession, String> {
    let network_id = required(parsed, "network-id")?
        .parse::<u32>()
        .map_err(|error| error.to_string())?;
    let gate = ConnectionGate::new(1);
    let mut transport = Uds::connect(&PathBuf::from(required(parsed, "socket")?), &gate, limits())
        .map_err(|error| format!("custody lni connect: {error:?}"))?;
    let handshake = perform(&mut transport, &handshake_config(Version::V1_8, network_id), None)
        .map_err(|error| format!("custody lni handshake: {error:?}"))?;
    for capability in [Capability::AccountRead, Capability::BatchHeader, Capability::PreparationState, Capability::CapsDiscovery] {
        if !handshake.capabilities().contains(capability) {
            return Err(format!("custody unavailable capability: {capability:?}"));
        }
    }
    let node = handshake.node();
    if node.interface_version.minor < 8 || node.latest_sealed_batch == 0 {
        return Err("custody requires interface 1.8 and a sealed state-proven head".to_owned());
    }
    let signed = batch::lookup(
        &mut transport, node.interface_version, node.latest_sealed_batch, 2,
        node.authorised_sequencer_key,
    ).map_err(|error| format!("custody batch header: {error:?}"))?;
    if signed.header.network_id() != network_id
        || signed.header.protocol_version() != node.protocol_version
        || signed.header.last_sequence() != node.chain_head_sequence
    {
        return Err("custody sealed head mismatch".to_owned());
    }
    let context = ReadContext {
        interface_version: node.interface_version,
        correlation_id: 10,
        expected_protocol_version: node.protocol_version,
        expected_network_id: network_id,
        requested: Requested::new(VerificationLevel::STATE_PROVEN),
        head: HeadTracker::new(node).current(),
        sequencer_authorization: SequencerAuthorization::new(
            signed.sequencer_id, node.authorised_sequencer_key,
            signed.first_batch_number, signed.last_batch_number,
        ),
        handshake_sequencer_key: node.authorised_sequencer_key,
        root_selector: RootSelector::Latest,
    };
    Ok(CustodySession { transport, context, state_root: signed.header.resulting_state_root(), capabilities: handshake.capabilities().clone() })
}

fn custody_effective_asset(
    session: &mut CustodySession,
    source_did: &str,
    asset: [u8; 32],
) -> Result<layerx_client::evidence::VerifiedEffectiveAsset, String> {
    let did = Did::new(source_did.as_bytes()).map_err(|error| format!("custody DID: {error:?}"))?;
    let did_id = layerx_wire::hash::did_id_for_protocol(&did, session.context.expected_protocol_version)
        .map_err(|error| format!("custody DID id: {error:?}"))?;
    let page_bytes = FRAME_BYTES.checked_sub(layerx_client::caps::CAPS_RESPONSE_HEADER_BYTES + 22)
        .ok_or("custody caps frame bound")?.min(layerx_client::caps::MAX_CAPS_PAGE_BYTES);
    let mut context = session.context;
    context.correlation_id = 4;
    let mut discovery = layerx_client::caps::CapsDiscovery::begin(
        &mut session.transport, &session.capabilities, context, did_id,
        u32::try_from(page_bytes).map_err(|error| error.to_string())?, Duration::from_secs(5), None,
    ).map_err(|error| format!("custody caps discovery: {error:?}"))?;
    let caps = loop {
        match discovery.advance() {
            layerx_client::caps::CapsProgress::Incomplete { .. } => {}
            layerx_client::caps::CapsProgress::Complete(caps)
            | layerx_client::caps::CapsProgress::Empty(caps) => break caps,
            layerx_client::caps::CapsProgress::Refused(error) => return Err(format!("custody caps refused: {error:?}")),
            layerx_client::caps::CapsProgress::Unavailable => return Err("custody caps unavailable".to_owned()),
        }
    };
    if caps.state_root() != session.state_root
        || caps.level() < VerificationLevel::STATE_PROVEN
        || caps.freshness().global_sequence != session.context.head.chain_sequence
        || caps.freshness().batch_number != session.context.head.sealed_batch
    {
        return Err("custody caps do not match the authenticated sealed head".to_owned());
    }
    let effective = caps.effective_asset(asset)
        .map_err(|error| format!("custody effective asset: {error:?}"))?;
    if effective.state_root() != session.state_root || effective.level() < VerificationLevel::STATE_PROVEN {
        return Err("custody effective asset is not state proven at the sealed head".to_owned());
    }
    Ok(effective)
}

fn custody_preparation(
    session: &mut CustodySession,
    actor: &Did,
    correlation_id: u64,
) -> Result<layerx_client::lni::PreparationState, String> {
    let state = layerx_client::lni::preparation::preparation_state(
        &mut session.transport, actor,
        layerx_client::lni::preparation::PreparationStateContext {
            interface_version: session.context.interface_version,
            expected_network_id: session.context.expected_network_id,
            minimum_observed_head: session.context.head.chain_sequence,
            correlation_id,
        },
    ).map_err(|error| format!("custody preparation: {error:?}"))?;
    if state.observed_head_sequence != session.context.head.chain_sequence
        || state.observed_state_root != session.state_root
    {
        return Err("custody preparation is not the authenticated sealed head; refresh required".to_owned());
    }
    Ok(state)
}

fn public_json_string(value: &str) -> String {
    let mut output = String::from("\"");
    for character in value.chars() {
        match character {
            '\\' => output.push_str("\\\\"),
            '"' => output.push_str("\\\""),
            control if control <= '\u{001f}' => { let _ = write!(output, "\\u{:04x}", u32::from(control)); }
            other => output.push(other),
        }
    }
    output.push('"');
    output
}

fn custody_asset_profile(parsed: &BTreeMap<String, String>) -> Result<String, String> {
    let mut session = custody_session(parsed)?;
    let asset = hex32(required(parsed, "asset")?)?;
    let effective = custody_effective_asset(&mut session, required(parsed, "source-did")?, asset)?;
    let selected = layerx_platform_core::read_custody_profile(
        &mut session.transport, session.context, asset, &effective,
    )?;
    let mut accounts = String::new();
    for role in ["source", "destination"] {
        if let Some(did) = parsed.get(&format!("{role}-did")) {
            let name = selected.account_name(did)?;
            let account = AccountId::parse(&name)
                .map_err(|error| format!("custody {role} account: {error:?}"))?;
            let id = layerx_wire::hash::account_id_for_protocol(
                &account, session.context.expected_protocol_version,
            ).map_err(|error| format!("custody {role} account id: {error:?}"))?;
            let _ = write!(accounts, ",\"{role}_account\":{},\"{role}_account_id\":\"{}\"",
                public_json_string(&name), hex(&id));
        }
    }
    if let Some(profile_output) = parsed.get("profile-output") {
        let path = PathBuf::from(profile_output);
        if !path.is_absolute() {
            return Err("--profile-output must be absolute".to_owned());
        }
        let mut output = std::fs::OpenOptions::new()
            .write(true).create_new(true).mode(0o600)
            .custom_flags(libc::O_NOFOLLOW | libc::O_NONBLOCK)
            .open(&path).map_err(|error| format!("custody profile output: {error}"))?;
        output.write_all(selected.profile()).map_err(|error| error.to_string())?;
        output.sync_all().map_err(|error| error.to_string())?;
        let _ = write!(accounts, ",\"profile_path\":{}", public_json_string(profile_output));
    }
    Ok(format!(
        "{{\"asset\":\"{}\",\"profile_sha256\":\"{}\",\"profile_hex\":\"{}\",\"trusted_height\":{},\"trust_source\":\"authenticated_initial_profile\",\"head_sequence\":{},\"global_sequence\":{},\"batch_number\":{},\"achieved\":\"STATE_PROVEN\"{accounts}}}",
        hex(&selected.asset()), hex(&selected.profile_hash()),
        hex(selected.profile()), selected.trusted_height(),
        session.context.head.chain_sequence, session.context.head.chain_sequence, session.context.head.sealed_batch,
    ))
}

fn write_asset_send(parsed: &BTreeMap<String, String>) -> Result<String, String> {
    let seed_file = std::fs::OpenOptions::new()
        .read(true).custom_flags(libc::O_NONBLOCK | libc::O_NOFOLLOW)
        .open(required(parsed, "seed-file")?).map_err(|error| error.to_string())?;
    let metadata = seed_file.metadata().map_err(|error| error.to_string())?;
    if !metadata.is_file() || metadata.len() != 32 {
        return Err("test seed must be a 32-byte regular file".to_owned());
    }
    let mut seed_bytes = Vec::new();
    seed_file.take(33).read_to_end(&mut seed_bytes).map_err(|error| error.to_string())?;
    let seed: [u8; 32] = seed_bytes.try_into().map_err(|_| "test seed must be 32 bytes".to_owned())?;
    let source_did = layerx_platform_core::treasury_did(&seed);
    if parsed.get("source-did").is_some_and(|supplied| supplied != &source_did) {
        return Err("source DID does not match the seed owner".to_owned());
    }
    let actor = Did::new(source_did.as_bytes()).map_err(|error| format!("actor: {error:?}"))?;
    let mut session = custody_session(parsed)?;
    let before = custody_preparation(&mut session, &actor, 3)?;
    let asset = hex32(required(parsed, "asset")?)?;
    let effective = custody_effective_asset(&mut session, &source_did, asset)?;
    let selected = layerx_platform_core::read_custody_asset(
        &mut session.transport, session.context, asset, &source_did, &effective,
    )?;
    if selected.state_root() != session.state_root {
        return Err("custody account and preparation roots differ".to_owned());
    }
    let after = custody_preparation(&mut session, &actor, 100)?;
    if before != after {
        return Err("custody preparation changed during asset proof reads; refresh required".to_owned());
    }
    let amount = parsed.get("amount").map_or(Ok(1), |value| value.parse::<u128>())
        .map_err(|error| format!("--amount: {error}"))?;
    let signed = layerx_platform_core::build_asset_send_with_identity_sequence(
        &seed,
        before.account_sequence,
        &layerx_platform_core::SendRequest {
            network_id: session.context.expected_network_id,
            source_did,
            destination_did: required(parsed, "destination-did")?.to_owned(),
            asset,
            amount,
            account_sequence: selected.account_sequence(),
            idempotency_key: [0x62; 32],
            not_before_ms: before.protocol_timestamp,
            expires_at_ms: before.protocol_timestamp.checked_add(300_000).ok_or("timestamp exhausted")?,
            fee_limit: 1000,
        },
        &selected,
    )?;
    let mut output = std::fs::OpenOptions::new()
        .write(true).create_new(true).mode(0o600)
        .custom_flags(libc::O_NOFOLLOW | libc::O_NONBLOCK)
        .open(required(parsed, "output")?).map_err(|error| error.to_string())?;
    output.write_all(&signed.canonical).map_err(|error| error.to_string())?;
    output.sync_all().map_err(|error| error.to_string())?;
    Ok(hex(&signed.activity_id))
}

fn main() -> ExitCode {
    let arguments: Vec<String> = std::env::args().skip(1).collect();
    let Some(command) = arguments.first() else {
        return usage();
    };
    let parsed = match options(&arguments[1..]) {
        Ok(parsed) => parsed,
        Err(error) => {
            eprintln!("layerx-node-probe: {error}");
            return usage();
        }
    };
    let outcome = match command.as_str() {
        "handshake" => handshake(&parsed),
        "balance" => balance(&parsed),
        "did-accounts" => did_accounts(&parsed),
        "supervisor" => supervisor(&parsed),
        "write-send" => write_send(&parsed),
        "custody-asset-profile" => custody_asset_profile(&parsed),
        "write-asset-send" => write_asset_send(&parsed),
        _ => return usage(),
    };
    match outcome {
        Ok(line) => {
            println!("{line}");
            ExitCode::SUCCESS
        }
        Err(error) => {
            eprintln!("layerx-node-probe: {error}");
            ExitCode::FAILURE
        }
    }
}
