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
fn real_programs_authenticated_peer_preserves_material_budget_and_owner_decisions() {
    let path = std::env::var_os("PAXEER_X_NATIVE_PROGRAM_HUMAN_PEER_FIXTURE")
        .expect("genuine protected TLS/LNI fixture required");
    let fixture: Value =
        serde_json::from_slice(&protected(Path::new(&path), 65_536)).expect("genuine fixture JSON");
    assert_eq!(fixture["schema"], "paxeer-x.native-program-human-peer.v1");
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
    use layerx_agentd::approval::native_program_presentation::{
        read_owned, ProgramPresentationOperation, ProgramPresentationSemantics,
    };
    use layerx_agentd::budget::program_settlement::read_owned_program_budget;
    use layerx_agentd::budget::{load_daemon_limits, BudgetLimiter, ProgramChargeKind};
    use layerx_agentd::prepare::DurablePreparation;
    use layerx_agentd::store::{Store, TenantId};
    use layerx_types::payload::{ActivityType, ModuleId, ModuleRegistration, ModuleRegistry};
    use sha2::{Digest, Sha256};
    let ordinals: Vec<_> = fixture["program_ordinals"]
        .as_array()
        .expect("real registered Programs ordinals")
        .iter()
        .map(|ordinal| {
            ActivityType::new(
                ModuleId::Programs,
                u16::try_from(ordinal.as_u64().expect("ordinal")).expect("ordinal bound"),
            )
            .expect("registered native operation")
        })
        .collect();
    let registry = ModuleRegistry::new(&[ModuleRegistration::new(ModuleId::Programs, &ordinals)
        .expect("actual Programs registration")])
    .expect("actual module registry");
    let store = Store::open(Path::new(text(&fixture, "retained_store")))
        .expect("genuine retained tenant store");
    let approval_id = id(text(&fixture, "approval_id"));
    let held = read_owned(&store, &peer, approval_id, &registry)
        .expect("actual owner-bound Programs carrier and creation sidecar");
    assert!(matches!(
        held.operation(),
        ProgramPresentationOperation::Call(_)
    ));
    assert_eq!(held.id(), approval_id);
    assert_eq!(held.held_digest(), id(text(&fixture, "held_digest")));
    assert_eq!(
        held.held_digest(),
        <[u8; 32]>::from(Sha256::digest(held.immutable_carrier_bytes()))
    );
    assert_eq!(
        approval_id,
        <[u8; 32]>::from(Sha256::digest(held.canonical_unsigned_bytes()))
    );
    assert!(
        matches!(held.semantics(), ProgramPresentationSemantics::AuthorizedLimits(rows) if !rows.is_empty())
    );
    let reservation = held.reservation();
    let allocations = reservation
        .allocations()
        .expect("genuine reserved allocations");
    let limits: Vec<_> = allocations
        .iter()
        .filter(|row| row.kind != ProgramChargeKind::Fee)
        .cloned()
        .collect();
    match held.semantics() {
        ProgramPresentationSemantics::OperationOnly => assert!(limits.is_empty()),
        ProgramPresentationSemantics::AuthorizedLimits(actual) => assert_eq!(actual, &limits),
    }
    let fees: Vec<_> = allocations
        .iter()
        .filter(|row| row.kind == ProgramChargeKind::Fee)
        .map(|row| row.asset)
        .collect();
    assert_eq!(held.fee_asset(), fees.first().copied());
    assert!(fees.iter().all(|asset| Some(*asset) == held.fee_asset()));
    let tenant = TenantId::new(peer.tenant.clone()).expect("actual tenant");
    let durable_key =
        DurablePreparation::store_key(&tenant, approval_id).expect("actual durable key");
    let durable = DurablePreparation::decode(
        tenant,
        store
            .get(&durable_key)
            .expect("actual durable lifecycle")
            .bytes(),
    )
    .expect("actual lifecycle encoding");
    let limiter =
        BudgetLimiter::new(Vec::new()).expect("empty limiter for genuine persisted configuration");
    load_daemon_limits(&store, &limiter).expect("actual persisted daemon limit owners");
    if !durable.terminal() {
        limiter
            .restore_program_reservation(reservation)
            .expect("actual exact retained reservation");
    }
    let row = read_owned_program_budget(
        &store,
        &peer,
        approval_id,
        held.held_digest(),
        &registry,
        &limiter,
        &verified,
        head.chain_sequence,
    )
    .expect("verified budget and exact local reservation lineage");
    assert_eq!(row.asset(), verified.asset());
    assert_eq!(row.source(), verified.source_account());
    assert_eq!(row.budget_id(), verified.budget_id());
    assert_eq!(
        row.verified_proof_bytes(),
        verified.canonical_export_bytes()
    );
    assert_eq!(
        row.remaining_after_reservations(),
        text(&fixture, "expected_remaining_after_reservations")
            .parse::<u128>()
            .expect("actual owner computed remaining")
    );
    assert!(row.remaining_after_reservations() <= verified.remaining());
    assert_eq!(row.terminal(), durable.terminal());
    assert!(read_owned_program_budget(
        &store,
        &peer,
        approval_id,
        held.held_digest(),
        &registry,
        &limiter,
        &verified,
        head.chain_sequence.checked_add(1).expect("next head")
    )
    .is_err());
    let mut foreign = peer.clone();
    foreign
        .subject
        .as_mut()
        .expect("real subject")
        .owner
        .push_str("-substituted");
    assert!(read_owned(&store, &foreign, approval_id, &registry).is_err());
    let reopened = Store::open(Path::new(text(&fixture, "retained_store"))).expect("real reopen");
    let again =
        read_owned(&reopened, &peer, approval_id, &registry).expect("same real durable carrier");
    assert_eq!(
        again.immutable_carrier_bytes(),
        held.immutable_carrier_bytes()
    );
    assert_eq!(
        again.created_at_unix_seconds(),
        held.created_at_unix_seconds()
    );
    assert_eq!(
        again.activity_expires_at_unix_milliseconds(),
        held.activity_expires_at_unix_milliseconds()
    );
    use layerx_client::lni::transport::{ConnectionGate, FrameTransport, Uds};
    fn bytes(out: &mut Vec<u8>, value: &[u8]) {
        out.extend_from_slice(
            &u32::try_from(value.len())
                .expect("bounded native field")
                .to_be_bytes(),
        );
        out.extend_from_slice(value)
    }
    fn request(fixture: &Value, inner: &[u8], principal: &str) -> Vec<u8> {
        let mut out = b"LXHAGT01".to_vec();
        out.push(44);
        bytes(&mut out, principal.as_bytes());
        bytes(&mut out, text(fixture, "subject_owner").as_bytes());
        bytes(&mut out, text(fixture, "subject_account").as_bytes());
        out.extend_from_slice(&id(text(fixture, "asset_id")));
        bytes(&mut out, inner);
        out
    }
    fn exchange(fixture: &Value, inner: &[u8], principal: &str) -> Vec<u8> {
        let mut transport = Uds::connect(
            Path::new(text(fixture, "human_socket")),
            &ConnectionGate::new(1),
            Limits {
                maximum_frame_bytes: 1_048_576,
                maximum_connections: 1,
                maximum_streams: 1,
                maximum_queued_bytes: 1_048_576,
                deadline: Duration::from_secs(8),
            },
        )
        .expect("actual authenticated Human Unix peer");
        transport
            .send(&request(fixture, inner, principal))
            .expect("real private request");
        transport.receive().expect("real private response")
    }
    fn operation(selector: u8, id: Option<[u8; 32]>, digest: Option<[u8; 32]>) -> Vec<u8> {
        let mut out = b"LXHAGT01".to_vec();
        out.push(selector);
        out.extend_from_slice(&5u16.to_be_bytes());
        if let Some(id) = id {
            out.extend_from_slice(&id)
        };
        if let Some(digest) = digest {
            out.extend_from_slice(&digest)
        };
        out
    }
    struct Decode<'a>(&'a [u8]);
    impl<'a> Decode<'a> {
        fn take(&mut self, n: usize) -> &'a [u8] {
            let (value, rest) = self
                .0
                .split_at_checked(n)
                .expect("bounded actual peer response");
            self.0 = rest;
            value
        }
        fn u8(&mut self) -> u8 {
            self.take(1)[0]
        }
        fn u16(&mut self) -> u16 {
            u16::from_be_bytes(self.take(2).try_into().unwrap())
        }
        fn u64(&mut self) -> u64 {
            u64::from_be_bytes(self.take(8).try_into().unwrap())
        }
        fn u128(&mut self) -> u128 {
            u128::from_be_bytes(self.take(16).try_into().unwrap())
        }
        fn field(&mut self) -> &'a [u8] {
            let n = u32::from_be_bytes(self.take(4).try_into().unwrap());
            self.take(usize::try_from(n).unwrap())
        }
        fn optional(&mut self) -> Option<[u8; 32]> {
            match self.u8() {
                0 => None,
                1 => Some(self.take(32).try_into().unwrap()),
                _ => panic!("closed native optional"),
            }
        }
    }
    let get = operation(59, Some(approval_id), None);
    let response = exchange(&fixture, &get, &peer.principal);
    let mut decoded = Decode(&response);
    assert_eq!(decoded.take(8), b"LXHAGT01");
    assert_eq!(decoded.u8(), 0);
    assert_eq!(decoded.u16(), 5);
    assert_eq!(decoded.take(32), approval_id);
    assert_eq!(decoded.take(32), held.held_digest());
    assert_eq!(decoded.field(), held.owner().as_bytes());
    assert_eq!(decoded.field(), held.actor());
    assert_eq!(decoded.u16(), 9);
    assert_eq!(decoded.u16(), held.activity_ordinal());
    let actual_state = decoded.u8();
    assert!(actual_state <= 5);
    assert_eq!(decoded.u64(), held.created_at_sequence());
    assert_eq!(decoded.u64(), held.budget_expiry_sequence());
    assert_eq!(decoded.u64(), held.created_at_unix_seconds());
    assert_eq!(decoded.u64(), held.activity_expires_at_unix_milliseconds());
    assert_eq!(decoded.optional(), held.release_ref());
    assert_eq!(decoded.optional(), held.fee_asset());
    let ProgramPresentationOperation::Call(call) = held.operation() else {
        panic!("genuine native Call fixture")
    };
    assert_eq!(
        decoded.field(),
        call.native().encode().expect("actual native payload")
    );
    match held.semantics() {
        ProgramPresentationSemantics::OperationOnly => assert_eq!(decoded.u8(), 0),
        ProgramPresentationSemantics::AuthorizedLimits(rows) => {
            assert_eq!(decoded.u8(), 1);
            assert_eq!(usize::from(decoded.u16()), rows.len());
            for row in rows {
                assert_eq!(decoded.u8(), row.kind as u8);
                assert_eq!(decoded.take(32), row.source);
                assert_eq!(decoded.take(32), row.asset);
                assert_eq!(decoded.optional(), row.destination);
                assert_eq!(decoded.u128(), row.maximum_amount)
            }
        }
    }
    assert!(decoded.0.is_empty());
    let only_id = id(text(&fixture, "operation_only_approval_id"));
    let only = read_owned(&store, &peer, only_id, &registry)
        .expect("genuine nonmonetary Program operation");
    assert!(matches!(
        only.semantics(),
        ProgramPresentationSemantics::OperationOnly
    ));
    let response = exchange(
        &fixture,
        &operation(59, Some(only_id), None),
        &peer.principal,
    );
    let mut decoded = Decode(&response);
    assert_eq!(decoded.take(8), b"LXHAGT01");
    assert_eq!(decoded.u8(), 0);
    assert_eq!(decoded.u16(), 5);
    assert_eq!(decoded.take(32), only_id);
    assert_eq!(decoded.take(32), only.held_digest());
    decoded.field();
    decoded.field();
    decoded.u16();
    decoded.u16();
    decoded.u8();
    for _ in 0..4 {
        decoded.u64();
    }
    decoded.optional();
    decoded.optional();
    decoded.field();
    assert_eq!(decoded.u8(), 0);
    assert!(decoded.0.is_empty());
    let mut list = operation(58, None, None);
    list.extend_from_slice(&[0, 100]);
    let response = exchange(&fixture, &list, &peer.principal);
    let mut decoded = Decode(&response);
    assert_eq!(decoded.take(8), b"LXHAGT01");
    assert_eq!(decoded.u8(), 0);
    assert_eq!(decoded.u16(), 5);
    assert!(decoded.u8() > 0);
    let material = operation(60, Some(approval_id), Some(held.held_digest()));
    let response = exchange(&fixture, &material, &peer.principal);
    let mut decoded = Decode(&response);
    assert_eq!(decoded.take(8), b"LXHAGT01");
    assert_eq!(decoded.u8(), 0);
    assert_eq!(decoded.u16(), 5);
    assert_eq!(decoded.take(32), approval_id);
    assert_eq!(decoded.take(32), held.held_digest());
    assert_eq!(decoded.field(), held.owner().as_bytes());
    assert_eq!(decoded.u8(), 0);
    assert_eq!(decoded.field(), held.canonical_unsigned_bytes());
    assert_eq!(decoded.field(), held.immutable_carrier_bytes());
    assert_eq!(decoded.field(), held.reservation().encode().unwrap());
    assert!(decoded.0.is_empty());
    let mut budget_request = operation(61, Some(approval_id), Some(held.held_digest()));
    budget_request.extend_from_slice(&head.chain_sequence.to_be_bytes());
    let response = exchange(&fixture, &budget_request, &peer.principal);
    let mut decoded = Decode(&response);
    assert_eq!(decoded.take(8), b"LXHAGT01");
    assert_eq!(decoded.u8(), 0);
    assert_eq!(decoded.u16(), 5);
    assert_eq!(decoded.take(32), approval_id);
    assert_eq!(decoded.take(32), held.held_digest());
    assert_eq!(decoded.field(), held.owner().as_bytes());
    assert_eq!(decoded.take(32), row.budget_id());
    assert_eq!(decoded.take(32), row.asset());
    assert_eq!(decoded.take(32), row.source());
    assert_eq!(decoded.u64(), head.chain_sequence);
    assert_eq!(decoded.u128(), row.remaining_after_reservations());
    assert_eq!(decoded.u8(), u8::from(row.terminal()));
    assert_eq!(decoded.u8(), verified.verification().wire_rank());
    assert_eq!(decoded.take(32), verified.evidence_digest());
    assert_eq!(decoded.take(32), verified.receipt_digest());
    assert_eq!(decoded.take(32), verified.checkpoint_digest());
    assert_eq!(decoded.u64(), verified.age_sequences());
    assert_eq!(decoded.u64(), verified.maximum_age_sequences());
    let package_digest: [u8; 32] = decoded.take(32).try_into().unwrap();
    let package = decoded.field();
    assert!(decoded.0.is_empty());
    assert_eq!(package_digest, <[u8; 32]>::from(Sha256::digest(package)));
    let exported = verify_budget_proof(package, budget, &trust)
        .expect("actual exported native proof independently reverified");
    assert_eq!(exported.asset(), row.asset());
    assert_eq!(exported.source_account(), row.source());
    assert!(exported.remaining() >= row.remaining_after_reservations());
    let wrong_digest: [u8; 32] =
        Sha256::digest(b"negative substitution of genuine held binding").into();
    let response = exchange(
        &fixture,
        &operation(60, Some(approval_id), Some(wrong_digest)),
        &peer.principal,
    );
    assert_eq!(response, b"LXHAGT01\x01");
    let response = exchange(&fixture, &get, text(&fixture, "foreign_subject_principal"));
    assert_eq!(response, b"LXHAGT01\x01");
    let cases = fixture["decision_cases"]
        .as_array()
        .expect("genuine isolated owner decision corpus");
    assert!(cases.len() >= 4);
    let mut outcomes = std::collections::BTreeSet::new();
    for case in cases {
        let case_id = id(text(case, "approval_id"));
        let case_digest = id(text(case, "held_digest"));
        let mut decision = operation(62, Some(case_id), Some(case_digest));
        bytes(&mut decision, text(case, "idempotency_key").as_bytes());
        decision.push(u8::from(
            case["grant"].as_bool().expect("actual owner choice"),
        ));
        decision.extend_from_slice(&head.chain_sequence.to_be_bytes());
        let first = exchange(&fixture, &decision, &peer.principal);
        let status = case["expected_status"]
            .as_u64()
            .expect("genuine expected terminal status");
        if status == 1 {
            assert_eq!(first, b"LXHAGT01\x01");
            outcomes.insert("session-refusal");
            continue;
        }
        let mut decoded = Decode(&first);
        assert_eq!(decoded.take(8), b"LXHAGT01");
        assert_eq!(decoded.u8(), 0);
        assert_eq!(decoded.u16(), 5);
        assert_eq!(decoded.take(32), case_id);
        assert_eq!(decoded.take(32), case_digest);
        decoded.field();
        decoded.field();
        assert_eq!(decoded.u16(), 9);
        decoded.u16();
        let state = decoded.u8();
        assert_eq!(
            u64::from(state),
            case["expected_state"]
                .as_u64()
                .expect("actual local terminal")
        );
        outcomes.insert(match state {
            1 => "grant",
            2 => "reject",
            3 => "expired",
            _ => panic!("decision must preserve true terminal"),
        });
        if state == 1 || state == 2 {
            assert_eq!(exchange(&fixture, &decision, &peer.principal), first);
        }
        let mut conflict = decision;
        let offset = 8 + 1 + 2 + 32 + 32 + 4;
        conflict[offset] = if conflict[offset] == b'0' { b'1' } else { b'0' };
        assert_eq!(
            exchange(&fixture, &conflict, &peer.principal),
            b"LXHAGT01\x01"
        );
    }
    assert!(
        outcomes.contains("grant")
            && outcomes.contains("reject")
            && outcomes.contains("session-refusal")
            && outcomes.contains("expired")
    );
    assert_eq!(node.head(), head);
}
