use layerx_agentd::budget::budget_proof::{verify_budget_proof, BudgetProofTrust};
use layerx_agentd::human::{HumanPeer, HumanSubject};
use layerx_agentd::human_runtime::{HumanAuthorityBoundary, RemoteHumanAuthority};
use layerx_client::client::{ClientConfig, ReconnectPolicy};
use layerx_client::evidence::CheckpointSelector;
use layerx_client::lni::handshake::HandshakeConfig;
use layerx_client::lni::schema::Version;
use layerx_client::lni::transport::Limits;
use layerx_client::Client;
use layerx_programs::hex;
use layerx_types::ids::Did;
use serde_json::Value;
use std::fs;
use std::os::unix::fs::PermissionsExt;
use std::path::Path;
use std::time::{Duration, SystemTime, UNIX_EPOCH};

fn protected(path: &Path, max: u64) -> Vec<u8> {
    let meta = fs::symlink_metadata(path).expect("genuine fixture metadata");
    assert!(
        meta.is_file()
            && meta.len() > 0
            && meta.len() <= max
            && meta.permissions().mode() & 0o077 == 0
    );
    fs::read(path).expect("protected genuine fixture")
}
fn text<'a>(value: &'a Value, key: &str) -> &'a str {
    value[key].as_str().expect("real fixture field")
}
fn id(value: &str) -> [u8; 32] {
    hex::decode(value)
        .expect("native identity")
        .try_into()
        .expect("32-byte identity")
}

#[test]
fn real_program_call_presentation_preserves_limits_and_owned_verified_budget() {
    let path = std::env::var_os("PAXEER_X_NATIVE_PROGRAM_PRESENTATION_FIXTURE")
        .expect("genuine protected TLS/LNI fixture required");
    let fixture: Value =
        serde_json::from_slice(&protected(Path::new(&path), 65_536)).expect("genuine fixture JSON");
    assert_eq!(fixture["schema"], "paxeer-x.native-program-presentation.v1");
    let network = u32::try_from(fixture["network_id"].as_u64().expect("native network"))
        .expect("network bound");
    let mut node = Client::connect(ClientConfig {
        endpoint: Path::new(text(&fixture, "lni_socket")).to_path_buf(),
        handshake: HandshakeConfig {
            built_interface_version: Version::V1_8,
            expected_protocol_version: 3,
            expected_network_id: network,
        },
        limits: Limits {
            maximum_frame_bytes: 1_212_416,
            maximum_connections: 1,
            maximum_streams: 1,
            maximum_queued_bytes: 1_212_416,
            deadline: Duration::from_secs(8),
        },
        reconnect: ReconnectPolicy {
            maximum_attempts: 1,
            base_delay: Duration::from_millis(1),
            maximum_delay: Duration::from_millis(1),
            jitter_percent: 0,
        },
    })
    .expect("real authenticated native node");
    let head = node.head();
    let checkpoint = node
        .checkpoint_evidence(CheckpointSelector::Identifier(head.finalised_checkpoint), 1)
        .expect("real trusted native checkpoint");
    let bearer = String::from_utf8(protected(Path::new(text(&fixture, "bearer_file")), 4096))
        .expect("protected bearer");
    let ca = protected(Path::new(text(&fixture, "ca_der_file")), 65_536);
    let mut authority = RemoteHumanAuthority::connect(
        text(&fixture, "authority_endpoint"),
        bearer.trim().to_owned(),
        Duration::from_secs(8),
        1_048_576,
        &ca,
    )
    .expect("real private TLS authority");
    let registration = fixture
        .get("registration_file")
        .and_then(Value::as_str)
        .map(|path| protected(Path::new(path), 1_048_576));
    let peer = HumanPeer {
        uid: u32::try_from(fixture["uid"].as_u64().expect("native peer uid")).expect("uid bound"),
        tenant: text(&fixture, "agent_tenant").to_owned(),
        principal: text(&fixture, "subject_principal").to_owned(),
        subject: Some(HumanSubject {
            transport_tenant: text(&fixture, "tenant").to_owned(),
            transport_principal: text(&fixture, "principal").to_owned(),
            owner: text(&fixture, "subject_owner").to_owned(),
            account: text(&fixture, "subject_account").to_owned(),
            asset: id(text(&fixture, "asset_id")),
            registration,
        }),
    };
    let owner = Did::new(text(&fixture, "budget_owner_did").as_bytes())
        .expect("actual managed budget owner");
    let budget = id(text(&fixture, "budget_id"));
    let raw = authority
        .budget_proof_export(&peer, budget, &owner)
        .expect("actual remote proof bytes");
    let now_ms = u64::try_from(
        SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .expect("clock")
            .as_millis(),
    )
    .expect("clock bound");
    let trust = BudgetProofTrust {
        tenant: text(&fixture, "tenant"),
        principal: text(&fixture, "principal"),
        owner: &owner,
        protocol_version: 3,
        network_id: network,
        sequencer_key: node.handshake().node().authorised_sequencer_key,
        head,
        checkpoint: &checkpoint,
        now_ms,
        maximum_age_seconds: fixture["maximum_age_seconds"]
            .as_u64()
            .expect("configured freshness"),
    };
    let verified =
        verify_budget_proof(&raw, budget, &trust).expect("actual proof independently verified");
    use layerx_agentd::approval::native_program_presentation::{read_owned, ProgramPresentationOperation, ProgramPresentationSemantics};
    use layerx_agentd::budget::{BudgetLimiter, ProgramChargeKind, load_daemon_limits};
    use layerx_agentd::budget::program_settlement::read_owned_program_budget;
    use layerx_agentd::prepare::DurablePreparation;
    use layerx_agentd::store::{Store, TenantId};
    use layerx_types::payload::{ActivityType, ModuleId, ModuleRegistration, ModuleRegistry};
    use sha2::{Digest, Sha256};
    let ordinals: Vec<_> = fixture["program_ordinals"].as_array().expect("real registered Programs ordinals")
        .iter().map(|ordinal| ActivityType::new(ModuleId::Programs, u16::try_from(ordinal.as_u64().expect("ordinal")).expect("ordinal bound")).expect("registered native operation")).collect();
    let registry = ModuleRegistry::new(&[ModuleRegistration::new(ModuleId::Programs, &ordinals).expect("actual Programs registration")]).expect("actual module registry");
    let store = Store::open(Path::new(text(&fixture, "retained_store"))).expect("genuine retained tenant store");
    let approval_id = id(text(&fixture, "approval_id"));
    let held = read_owned(&store, &peer, approval_id, &registry).expect("actual owner-bound Programs carrier and creation sidecar");
    assert!(matches!(held.operation(), ProgramPresentationOperation::Call(_)));
    assert_eq!(held.id(), approval_id);
    assert_eq!(held.held_digest(), id(text(&fixture, "held_digest")));
    assert_eq!(held.held_digest(), <[u8;32]>::from(Sha256::digest(held.immutable_carrier_bytes())));
    assert_eq!(approval_id, <[u8;32]>::from(Sha256::digest(held.canonical_unsigned_bytes())));
    let reservation = held.reservation();
    let allocations = reservation.allocations().expect("genuine reserved allocations");
    let limits: Vec<_> = allocations.iter().filter(|row| row.kind != ProgramChargeKind::Fee).cloned().collect();
    match held.semantics() {
        ProgramPresentationSemantics::OperationOnly => assert!(limits.is_empty()),
        ProgramPresentationSemantics::AuthorizedLimits(actual) => assert_eq!(actual, &limits),
    }
    let fees: Vec<_> = allocations.iter().filter(|row| row.kind == ProgramChargeKind::Fee).map(|row|row.asset).collect();
    assert_eq!(held.fee_asset(), fees.first().copied());
    assert!(fees.iter().all(|asset| Some(*asset) == held.fee_asset()));
    let tenant = TenantId::new(peer.tenant.clone()).expect("actual tenant");
    let durable_key = DurablePreparation::store_key(&tenant, approval_id).expect("actual durable key");
    let durable = DurablePreparation::decode(tenant, store.get(&durable_key).expect("actual durable lifecycle").bytes()).expect("actual lifecycle encoding");
    let limiter = BudgetLimiter::new(Vec::new()).expect("empty limiter for genuine persisted configuration");
    load_daemon_limits(&store, &limiter).expect("actual persisted daemon limit owners");
    if !durable.terminal() { limiter.restore_program_reservation(reservation).expect("actual exact retained reservation"); }
    let row = read_owned_program_budget(&store, &peer, approval_id, held.held_digest(), &registry, &limiter, &verified, head.chain_sequence).expect("verified budget and exact local reservation lineage");
    assert_eq!(row.asset(), verified.asset());
    assert_eq!(row.source(), verified.source_account());
    assert_eq!(row.budget_id(), verified.budget_id());
    assert_eq!(row.verified_proof_bytes(), verified.canonical_export_bytes());
    assert_eq!(row.remaining_after_reservations(), text(&fixture, "expected_remaining_after_reservations").parse::<u128>().expect("actual owner computed remaining"));
    assert!(row.remaining_after_reservations() <= verified.remaining());
    assert_eq!(row.terminal(), durable.terminal());
    assert!(read_owned_program_budget(&store, &peer, approval_id, held.held_digest(), &registry, &limiter, &verified, head.chain_sequence.checked_add(1).expect("next head")).is_err());
    let mut foreign = peer.clone();
    foreign.subject.as_mut().expect("real subject").owner.push_str("-substituted");
    assert!(read_owned(&store, &foreign, approval_id, &registry).is_err());
    let reopened = Store::open(Path::new(text(&fixture, "retained_store"))).expect("real reopen");
    let again = read_owned(&reopened, &peer, approval_id, &registry).expect("same real durable carrier");
    assert_eq!(again.immutable_carrier_bytes(), held.immutable_carrier_bytes());
    assert_eq!(again.created_at_unix_seconds(), held.created_at_unix_seconds());
    assert_eq!(again.activity_expires_at_unix_milliseconds(), held.activity_expires_at_unix_milliseconds());
    assert_eq!(node.head(), head);
}
