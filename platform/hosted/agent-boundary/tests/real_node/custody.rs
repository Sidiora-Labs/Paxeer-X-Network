use super::*;
use layerx_client::lni::transport::{FrameTransport, TransportError};
use layerx_proof::inclusion::{verify_receipt, SequencerAuthorization};
use layerx_proof::merkle::Proof;
use layerx_wire::hash::{receipt_digest, receipt_execution_batch_id};
use layerx_wire::receipt::{decode_batch_header, decode_merkle_proof, encode_unsigned};

struct AccountProofConnection {
    child: Child,
    input: std::process::ChildStdin,
    output: std::process::ChildStdout,
    corrupt_account_root: Option<[u8; 32]>,
    corrupt_receipt_identity: bool,
}

impl AccountProofConnection {
    fn connect(cluster: &Cluster) -> Self {
        let mut child = must(
            Command::new("/usr/bin/setpriv")
                .args([
                    "--reuid",
                    &BOUNDARY_UID.to_string(),
                    "--regid",
                    &BOUNDARY_GID.to_string(),
                    "--groups",
                    &BOUNDARY_GID.to_string(),
                    "--",
                    "/usr/bin/python3",
                    "-c",
                ])
                .arg(include_str!("lni_relay.py"))
                .arg(cluster.root.join("run/layerxd.sock"))
                .stdin(Stdio::piped())
                .stdout(Stdio::piped())
                .stderr(Stdio::inherit())
                .spawn(),
            "real LNI account-proof connection",
        );
        let input = child
            .stdin
            .take()
            .unwrap_or_else(|| panic!("LNI relay input missing"));
        let output = child
            .stdout
            .take()
            .unwrap_or_else(|| panic!("LNI relay output missing"));
        Self {
            child,
            input,
            output,
            corrupt_account_root: None,
            corrupt_receipt_identity: false,
        }
    }
}

impl Drop for AccountProofConnection {
    fn drop(&mut self) {
        let _ = self.child.kill();
        let _ = self.child.wait();
    }
}

impl FrameTransport for AccountProofConnection {
    fn send(&mut self, bytes: &[u8]) -> Result<(), TransportError> {
        layerx_client::lni::framing::write_frame(&mut self.input, bytes, LNI_FRAME_BYTES)
    }

    fn receive(&mut self) -> Result<Vec<u8>, TransportError> {
        let bytes = layerx_client::lni::framing::read_frame(&mut self.output, LNI_FRAME_BYTES)?;
        let envelope = must(
            layerx_client::lni::schema::decode_envelope(&bytes),
            "real proof envelope",
        );
        let mut proof = envelope.proof_material.to_vec();
        if let Some(account) = self.corrupt_account_root.take() {
            assert_eq!(envelope.message_tag, 17);
            assert!(proof.len() >= 68);
            assert_eq!(&proof[..4], &[0, 3, 2, 1]);
            assert_eq!(&proof[4..36], &account);
            proof[36] ^= 1;
        } else if self.corrupt_receipt_identity
            && envelope.message_tag == 17
            && proof.starts_with(&[0, 1, 3])
        {
            assert!(proof.len() >= 35);
            proof[3] ^= 1;
            self.corrupt_receipt_identity = false;
        } else {
            return Ok(bytes);
        }
        Ok(must(
            layerx_client::lni::schema::encode_envelope(layerx_client::lni::schema::Envelope {
                proof_material: &proof,
                ..envelope
            }),
            "fault-injected real account proof",
        ))
    }
}

fn verify_funded_accounts(
    cluster: &Cluster,
    protocol: &layerx_wire::receipt::ProtocolReceipt,
    authorization: SequencerAuthorization,
) {
    use layerx_client::evidence::RootSelector;
    use layerx_client::lni::handshake::{perform, HandshakeConfig};
    use layerx_client::lni::schema::Version;
    use layerx_client::read::{account, ReadContext, Requested};
    use layerx_types::verify::VerificationLevel;

    let mut connection = AccountProofConnection::connect(cluster);
    let handshake = must(
        perform(
            &mut connection,
            &HandshakeConfig {
                built_interface_version: Version::V1_4,
                expected_protocol_version: PROTOCOL_VERSION,
                expected_network_id: NETWORK_ID,
            },
            None,
        ),
        "account proof handshake",
    );
    assert_eq!(
        handshake.node().authorised_sequencer_key,
        cluster.sequencer_key
    );
    let head = layerx_client::head::HeadTracker::new(handshake.node()).current();
    for (index, (identifier, expected_balance)) in [
        (reserve_account(), 0),
        (cluster.actor.source, FUNDING_AMOUNT),
    ]
    .into_iter()
    .enumerate()
    {
        let value = must(
            account(
                &mut connection,
                identifier,
                ReadContext {
                    interface_version: handshake.node().interface_version,
                    correlation_id: must(u64::try_from(index + 1), "account correlation"),
                    expected_protocol_version: PROTOCOL_VERSION,
                    expected_network_id: NETWORK_ID,
                    requested: Requested::new(VerificationLevel::STATE_PROVEN),
                    head,
                    sequencer_authorization: authorization,
                    handshake_sequencer_key: cluster.sequencer_key,
                    root_selector: RootSelector::Latest,
                },
            ),
            "funding account-state proof",
        );
        verify_funded_account(cluster, protocol, identifier, expected_balance, &value);
        verify_account_bundle(
            cluster,
            protocol,
            &authorization,
            identifier,
            &value,
            &mut connection,
            layerx_client::evidence::EvidenceContext {
                interface_version: handshake.node().interface_version,
                correlation_id: must(u64::try_from(index + 10), "bundle correlation"),
                expected_protocol_version: PROTOCOL_VERSION,
                expected_network_id: NETWORK_ID,
                handshake_sequencer_key: cluster.sequencer_key,
            },
        );
    }
}

fn verify_funded_account(
    cluster: &Cluster,
    protocol: &layerx_wire::receipt::ProtocolReceipt,
    identifier: [u8; 32],
    expected_balance: u128,
    value: &layerx_client::read::ReadValue,
) {
    use layerx_proof::state::decode_account_value;
    use layerx_types::verify::VerificationLevel;
    assert_eq!(value.achieved(), VerificationLevel::STATE_PROVEN);
    let decoded = must(
        decode_account_value(identifier, value.canonical_bytes()),
        "funding canonical account",
    );
    assert_eq!(decoded.asset_id(), cluster.asset);
    assert_eq!(decoded.balance(), expected_balance);
    if identifier == reserve_account() {
        let balances = protocol.effects()[2].body();
        let after_sequence = u64::from_be_bytes(must(
            balances[104..112].try_into(),
            "reserve after sequence",
        ));
        assert_eq!(decoded.next_sequence, after_sequence);
    }
}

fn verify_account_bundle(
    cluster: &Cluster,
    protocol: &layerx_wire::receipt::ProtocolReceipt,
    authorization: &SequencerAuthorization,
    identifier: [u8; 32],
    value: &layerx_client::read::ReadValue,
    connection: &mut AccountProofConnection,
    context: layerx_client::evidence::EvidenceContext,
) {
    use layerx_client::evidence::{
        proof_bundle, EvidenceError, ProofBundleSelector, VerifiedProofBundle,
    };
    let selector = ProofBundleSelector::AccountState {
        activity_id: protocol.activity_id(),
        account_id: identifier,
    };
    let bundle = must(
        proof_bundle(connection, selector, context, &bridge_registry()),
        "public nested account verifier",
    );
    match bundle {
        VerifiedProofBundle::Account {
            canonical_bytes,
            proof_material,
            activity_id,
            verified,
            signed_header,
        } => {
            assert_eq!(canonical_bytes, value.canonical_bytes());
            assert_eq!(proof_material, value.proof_material());
            assert_eq!(activity_id, protocol.activity_id());
            assert_eq!(
                verified.header().header().resulting_state_root(),
                protocol.resulting_state_root()
            );
            assert_eq!(signed_header.public_key, cluster.sequencer_key);
            assert_eq!(verified.receipt_activity_id(), protocol.activity_id());
        }
        VerifiedProofBundle::MaintainedAccount {
            canonical_bytes,
            proof_material,
            activity_id,
            activity_receipt,
            activity_receipt_proof,
            verified,
            signed_header,
        } => {
            let activity_inclusion = must(
                verify_receipt(
                    &activity_receipt,
                    &activity_receipt_proof,
                    &signed_header.canonical_bytes,
                    &signed_header.signature,
                    authorization,
                ),
                "maintained activity receipt inclusion",
            );
            let layerx_wire::receipt::Receipt::Protocol(covered) = must(
                layerx_proof::receipt::verify_sequencer_signature(
                    &activity_receipt,
                    cluster.sequencer_key,
                ),
                "maintained activity receipt signature",
            ) else {
                panic!("protocol receipt required");
            };
            assert_eq!(canonical_bytes, value.canonical_bytes());
            assert_eq!(proof_material, value.proof_material());
            assert_eq!(activity_id, protocol.activity_id());
            assert_eq!(covered.activity_id(), protocol.activity_id());
            assert_eq!(covered.batch_id(), protocol.batch_id());
            assert_eq!(covered.global_sequence(), protocol.global_sequence());
            assert_eq!(
                covered.previous_state_root(),
                protocol.previous_state_root()
            );
            assert_eq!(activity_inclusion.header(), verified.header());
            assert_eq!(
                covered.resulting_state_root(),
                protocol.resulting_state_root()
            );
            assert_eq!(
                verified.header().header().last_sequence(),
                protocol.global_sequence() + 1
            );
            assert_eq!(
                verified.header().header().resulting_state_root(),
                must(
                    decode_batch_header(&signed_header.canonical_bytes),
                    "maintained header"
                )
                .resulting_state_root()
            );
            assert_eq!(signed_header.public_key, cluster.sequencer_key);
        }
        _ => panic!("account proof bundle required"),
    }
    connection.corrupt_account_root = Some(identifier);
    let mut retry = context;
    retry.correlation_id += 100;
    assert!(matches!(
        proof_bundle(connection, selector, retry, &bridge_registry()),
        Err(EvidenceError::Account(_))
    ));
    connection.corrupt_receipt_identity = true;
    retry.correlation_id += 100;
    assert!(matches!(
        proof_bundle(connection, selector, retry, &bridge_registry()),
        Err(EvidenceError::SelectorMismatch)
    ));
    assert!(!connection.corrupt_receipt_identity);
}

const CUSTODY_CHAIN_ID: u64 = 31337;
const FUNDING_AMOUNT: u128 = 100_000_000_000_000;

pub(super) struct CustodyChain {
    root: PathBuf,
    nodes: Vec<Daemon>,
    evidence_arguments: Vec<String>,
}

impl CustodyChain {
    pub(super) fn verify_evidence(&self) {
        producer("test_evidence.py", &self.evidence_arguments);
        println!("real custody Python evidence suite passed");
    }
}

impl Drop for CustodyChain {
    fn drop(&mut self) {
        for node in &mut self.nodes {
            node.stop();
        }
        if thread::panicking() {
            for node in &self.nodes {
                eprintln!("custody Anvil stderr:\n{}", node.diagnostics());
            }
            eprintln!("custody artifacts retained at {}", self.root.display());
        } else {
            let _ = fs::remove_dir_all(&self.root);
        }
    }
}

fn producer(script: &str, arguments: &[String]) {
    let script = repository_root().join("tests/bridge").join(script);
    let output = must(
        Command::new("python3").arg(script).args(arguments).output(),
        "custody producer",
    );
    assert!(
        output.status.success(),
        "custody producer failed: {}",
        String::from_utf8_lossy(&output.stderr)
    );
    eprintln!("{}", String::from_utf8_lossy(&output.stdout));
    eprintln!("{}", String::from_utf8_lossy(&output.stderr));
}

fn start_anvil(chain: &mut CustodyChain, fork: Option<(&str, u64)>) -> String {
    let port = loop {
        let candidate = free_port();
        if candidate != 18545 {
            break candidate;
        }
    };
    let stderr = chain.root.join(format!("anvil-{port}.stderr"));
    let mut invocation = Command::new("anvil");
    invocation.args([
        "--host",
        "127.0.0.1",
        "--port",
        &port.to_string(),
        "--chain-id",
        &CUSTODY_CHAIN_ID.to_string(),
        "--silent",
    ]);
    if let Some((url, block)) = fork {
        invocation.args(["--fork-url", url, "--fork-block-number", &block.to_string()]);
    }
    let child = must(
        invocation
            .stdin(Stdio::null())
            .stdout(Stdio::null())
            .stderr(Stdio::from(must(fs::File::create(&stderr), "Anvil stderr")))
            .spawn(),
        "start isolated Anvil",
    );
    let mut daemon = Daemon { child, stderr };
    wait_for_port(port, &mut daemon, "custody Anvil");
    chain.nodes.push(daemon);
    format!("http://127.0.0.1:{port}")
}

fn bridge_registry() -> ModuleRegistry {
    let activity = must(
        ActivityType::new(ModuleId::Bridge, 1),
        "custody credit ordinal",
    );
    let registration = must(
        ModuleRegistration::new(ModuleId::Bridge, &[activity]),
        "custody module",
    );
    must(ModuleRegistry::new(&[registration]), "custody registry")
}

fn reserve_account() -> [u8; 32] {
    let reserve_name = b"system:paxeer-reserve";
    let mut reserve = Sha256::new();
    reserve.update(b"LX:ACCOUNT:v1");
    reserve.update(must(u32::try_from(reserve_name.len()), "reserve name length").to_be_bytes());
    reserve.update(reserve_name);
    reserve.finalize().into()
}

fn verify_funding_projection(cluster: &Cluster, protocol: &layerx_wire::receipt::ProtocolReceipt) {
    assert_eq!(
        protocol.previous_state_root(),
        cluster.genesis_receipt_state_root
    );
    assert_eq!(protocol.operation(), 0);
    assert_eq!(protocol.asset(), [0; 32]);
    assert_eq!(protocol.amount(), 0);
    assert_eq!(protocol.from(), [0; 32]);
    assert_eq!(protocol.to(), [0; 32]);
    assert_eq!(protocol.debit_sequence(), 0);
    assert_eq!(protocol.debit_balance_before(), 0);
    assert_eq!(protocol.debit_balance_after(), 0);
    assert_eq!(protocol.credit_balance_before(), 0);
    assert_eq!(protocol.credit_balance_after(), 0);
    assert_eq!(protocol.authorization_hash(), [0; 32]);
    assert_eq!(protocol.context_hash(), [0; 32]);
    assert_eq!(protocol.transfer_set_root(), [0; 32]);
}

fn verify_funding_effects(protocol: &layerx_wire::receipt::ProtocolReceipt, credit: &[u8]) {
    let effects = protocol.effects();
    assert_eq!(effects.len(), 3);
    let transfer = &effects[0];
    assert_eq!(transfer.module_id(), 8);
    assert_eq!(transfer.kind(), 2);
    assert!(transfer.monetary());
    assert_ne!(transfer.transfer_set_root(), [0; 32]);
    assert_eq!(transfer.event_type(), 0);
    assert!(transfer.body().is_empty());
    for (effect, event_type, length) in [(&effects[1], 1, 208), (&effects[2], 2, 112)] {
        assert_eq!(effect.module_id(), 8);
        assert_eq!(effect.kind(), 3);
        assert_eq!(effect.event_type(), event_type);
        assert!(!effect.monetary());
        assert_eq!(effect.transfer_set_root(), [0; 32]);
        assert_eq!(effect.body().len(), length);
    }
    let mut expected = credit[43..139].to_vec();
    expected.extend_from_slice(&FUNDING_AMOUNT.to_be_bytes());
    expected.extend_from_slice(&credit[5..37]);
    expected.extend_from_slice(&Sha256::digest(credit));
    expected.extend_from_slice(&0_u128.to_be_bytes());
    expected.extend_from_slice(&FUNDING_AMOUNT.to_be_bytes());
    assert_eq!(effects[1].body(), expected);
    let balances = effects[2].body();
    assert_eq!(&balances[..32], &reserve_account());
    assert_eq!(
        &balances[32..48],
        &0_u128.to_be_bytes(),
        "reserve before is committed genesis, not interim issuance"
    );
    assert_eq!(&balances[48..64], &0_u128.to_be_bytes());
    assert_eq!(&balances[64..80], &0_u128.to_be_bytes());
    assert_eq!(&balances[80..96], &FUNDING_AMOUNT.to_be_bytes());
    let before = u64::from_be_bytes(must(
        balances[96..104].try_into(),
        "reserve before sequence",
    ));
    let after = u64::from_be_bytes(must(
        balances[104..112].try_into(),
        "reserve after sequence",
    ));
    assert_eq!(Some(after), before.checked_add(1));
}

fn verify_funding_receipt(cluster: &Cluster, signed: &[u8], answer: &HttpAnswer) {
    assert_eq!(
        answer.status,
        200,
        "funding must succeed: {}",
        answer.text()
    );
    let activity = must(
        decode_signed(signed, &bridge_registry()),
        "C-signed credit activity",
    );
    assert_eq!(activity.activity_type().module(), ModuleId::Bridge);
    assert_eq!(activity.activity_type().ordinal(), 1);
    assert_eq!(activity.protocol_version(), PROTOCOL_VERSION);
    assert_eq!(activity.network_id(), NETWORK_ID);
    assert_eq!(activity.account_sequence(), 1);
    let payload = activity.payload();
    assert!(payload.len() > 363);
    assert_eq!(&payload[..5], b"LXDC3");
    assert_eq!(&payload[37..41], &NETWORK_ID.to_be_bytes());
    assert_eq!(&payload[41..43], &PROTOCOL_VERSION.to_be_bytes());
    let state_height = u64::from_be_bytes(must(payload[215..223].try_into(), "state height"));
    assert_ne!(state_height, 0);
    assert_eq!(
        u64::from_be_bytes(must(payload[287..295].try_into(), "header height")),
        state_height + 1
    );
    assert_eq!(&payload[327..359], &Sha256::digest(&payload[363..])[..]);
    assert_eq!(&payload[359..363], &2_u32.to_be_bytes());
    assert_eq!(&payload[363..368], b"LXLB1");
    assert_eq!(
        cluster.actor.did,
        format!(
            "did:layerx:{}",
            hex(&cluster.actor.signing_key.verifying_key().to_bytes())
        )
    );
    assert_eq!(&activity.payload()[75..107], &cluster.asset);
    assert_eq!(&activity.payload()[107..139], &cluster.actor.source);
    assert_eq!(
        &activity.payload()[139..171],
        &cluster.actor.signing_key.verifying_key().to_bytes()
    );
    assert_eq!(&activity.payload()[191..207], &FUNDING_AMOUNT.to_be_bytes());
    let mut nullifier = Sha256::new();
    nullifier.update(b"LX:DEPOSIT:NULLIFIER:v1");
    nullifier.update(&activity.payload()[43..75]);
    let nullifier: [u8; 32] = nullifier.finalize().into();
    assert_eq!(activity.idempotency_key(), nullifier);
    let expected_id = must(activity_id(&activity), "funding activity id");
    let document = answer.json();
    let result = &document["result"];
    assert_eq!(field(result, "state"), "completed");
    assert_eq!(field(result, "activity_id"), hex(&expected_id));
    let bytes = unhex(field(result, "receipt"));
    let receipt = must(
        verify_sequencer_signature(&bytes, cluster.sequencer_key),
        "funding receipt signature",
    );
    let protocol = receipt
        .protocol()
        .unwrap_or_else(|| panic!("funding protocol receipt required"));
    assert_eq!(protocol.activity_id(), expected_id);
    assert_eq!(protocol.protocol_version(), PROTOCOL_VERSION);
    assert_eq!(protocol.module_id(), 8);
    assert_eq!(protocol.module_version(), 1);
    assert_eq!(
        protocol.result_code(),
        0,
        "refused funding cannot qualify escrow"
    );
    verify_funding_projection(cluster, protocol);
    verify_funding_effects(protocol, activity.payload());
    let authorization = verify_funding_batch(cluster, &receipt, &bytes);
    verify_funded_accounts(cluster, protocol, authorization);
}

fn verify_funding_batch(
    cluster: &Cluster,
    receipt: &layerx_wire::receipt::Receipt,
    bytes: &[u8],
) -> SequencerAuthorization {
    let protocol = receipt
        .protocol()
        .unwrap_or_else(|| panic!("funding protocol receipt required"));
    let digest = must(
        receipt_digest(&must(encode_unsigned(receipt), "unsigned funding receipt")),
        "funding receipt digest",
    );
    let evidence = http_get(
        cluster.program_port,
        &format!(
            "/v1/batches/{}/receipt-authority?receipt_digest={}",
            hex(&protocol.batch_id()),
            hex(&digest),
        ),
        &cluster.program_token,
    );
    assert_eq!(evidence.status, 200, "{}", evidence.text());
    let document = evidence.json();
    assert_eq!(
        field(&document, "sequencer_public_key"),
        hex(&cluster.sequencer_key)
    );
    let evidence = &document["batch_evidence"];
    let header_bytes = unhex(field(evidence, "header_hex"));
    let header = must(decode_batch_header(&header_bytes), "funding header");
    assert_eq!(header.protocol_version(), PROTOCOL_VERSION);
    assert_eq!(header.network_id(), NETWORK_ID);
    assert!(
        (header.first_sequence()..=header.last_sequence()).contains(&protocol.global_sequence())
    );
    let signature = must(
        <[u8; 64]>::try_from(unhex(field(evidence, "header_signature"))),
        "funding header signature",
    );
    let wire_proof = must(
        decode_merkle_proof(&unhex(field(evidence, "receipt_proof_hex"))),
        "funding proof",
    );
    let proof = must(
        Proof::new(
            wire_proof.leaf_index(),
            wire_proof.leaf_count(),
            wire_proof.siblings().to_vec(),
        ),
        "funding inclusion proof",
    );
    let authorization = SequencerAuthorization::new(
        header.sequencer_id(),
        cluster.sequencer_key,
        FIRST_BATCH,
        LAST_BATCH,
    );
    must(
        verify_receipt(bytes, &proof, &header_bytes, &signature, &authorization),
        "offline funding inclusion",
    );
    let activity = funding_activity_batch(bytes, evidence, &authorization);
    assert_eq!(protocol.batch_id(), activity.batch_id());
    assert_eq!(protocol.previous_state_root(), header.previous_state_root());
    assert_eq!(
        protocol.resulting_state_root(),
        activity.resulting_state_root()
    );
    let mut tampered = bytes.to_vec();
    let last = tampered.len() - 1;
    tampered[last] ^= 1;
    assert!(verify_receipt(&tampered, &proof, &header_bytes, &signature, &authorization).is_err());
    authorization
}

fn funding_activity_batch(
    bytes: &[u8],
    evidence: &serde_json::Value,
    authorization: &SequencerAuthorization,
) -> layerx_proof::receipt::AuthorizedBatch {
    use layerx_proof::receipt::{
        authorized_maintained_activity_batch, AuthorizedBatch, MaintainedOutcomeEvidence,
    };
    let receipt = must(layerx_wire::receipt::decode(bytes), "funding receipt");
    let protocol = receipt
        .protocol()
        .unwrap_or_else(|| panic!("funding protocol"));
    let header_bytes = unhex(field(evidence, "header_hex"));
    let header = must(decode_batch_header(&header_bytes), "funding header");
    let authority = AuthorizedBatch::new(
        protocol.batch_id(),
        protocol.asset(),
        header.previous_state_root(),
        header.resulting_state_root(),
        authorization.public_key(),
    );
    let Some(identity) = evidence.get("batch_identity") else {
        assert_eq!(
            protocol.batch_id(),
            must(
                receipt_execution_batch_id(protocol, &header),
                "funding batch id"
            )
        );
        return authority;
    };
    assert_eq!(field(identity, "kind"), "batch_maintenance_v1");
    let decode_proof = |value: &serde_json::Value| {
        let wire = must(
            decode_merkle_proof(&unhex(field(value, "receipt_proof_hex"))),
            "funding wire proof",
        );
        must(
            Proof::new(
                wire.leaf_index(),
                wire.leaf_count(),
                wire.siblings().to_vec(),
            ),
            "funding Merkle proof",
        )
    };
    let proof = decode_proof(evidence);
    let maintenance_proof = decode_proof(identity);
    let maintenance = unhex(field(identity, "receipt_hex"));
    let signature = must(
        <[u8; 64]>::try_from(unhex(field(evidence, "header_signature"))),
        "funding header signature",
    );
    let activity = must(
        authorized_maintained_activity_batch(
            bytes,
            &authority,
            &MaintainedOutcomeEvidence {
                header: &header_bytes,
                header_signature: &signature,
                activity_proof: &proof,
                maintenance: &maintenance,
                maintenance_proof: &maintenance_proof,
                authorization,
            },
        ),
        "authenticated funding maintenance",
    );
    let envelope = must(
        layerx_wire::batch_maintenance::decode_batch_maintenance(&maintenance),
        "funding maintenance",
    );
    assert_eq!(envelope.protocol_version, header.protocol_version());
    assert_eq!(envelope.epoch, header.epoch());
    assert_eq!(envelope.timestamp_ms, header.timestamp_ms());
    let record = &envelope.occupancy;
    assert_eq!(header.resulting_state_root(), record.resulting_state_root);
    assert_eq!(protocol.resulting_state_root(), record.previous_state_root);
    assert_eq!(activity.resulting_state_root(), record.previous_state_root);
    activity
}

fn credit_actor(cluster: &Cluster, root: &Path, profile: &Path, credit: &Path, actor_key: &Path) {
    let sign_credit_binary = repository_root().join("build/tests/bridge/sign-credit");
    assert!(
        sign_credit_binary.is_file(),
        "parent must build {}",
        sign_credit_binary.display()
    );
    let signed_path = root.join("signed-credit.bin");
    command(
        &text(&sign_credit_binary),
        &[
            &text(profile),
            &text(credit),
            &cluster.actor.did,
            &text(actor_key),
            "1",
            &now_ms().saturating_sub(1000).to_string(),
            &text(&signed_path),
        ],
    );
    let signed = must(fs::read(&signed_path), "native signed custody credit");
    let credit_test_binary = repository_root().join("build/tests/bridge/test-credit");
    assert!(
        credit_test_binary.is_file(),
        "parent must build {}",
        credit_test_binary.display()
    );
    command(
        &text(&credit_test_binary),
        &[
            &text(&cluster.root.join("genesis/artifacts/genesis.manifest")),
            &text(&signed_path),
            &text(actor_key),
        ],
    );
    eprintln!(
        "native custody credit gates passed: {}",
        credit_test_binary.display()
    );
    let activity = must(
        decode_signed(&signed, &bridge_registry()),
        "credit idempotency key",
    );
    let key = hex(&activity.idempotency_key());
    let deadline = Instant::now() + Duration::from_secs(60);
    let answer = loop {
        let answer = cluster.client.call(&Call::submit(
            "/v1/activities",
            &cluster.gateway_token,
            &key,
            &signed,
        ));
        if answer.status != 202 || Instant::now() >= deadline {
            break answer;
        }
        thread::sleep(Duration::from_millis(100));
    };
    verify_funding_receipt(cluster, &signed, &answer);
    let replay = cluster.client.call(&Call::submit(
        "/v1/activities",
        &cluster.gateway_token,
        &key,
        &signed,
    ));
    assert_eq!(replay.status, 200, "{}", replay.text());
    assert_eq!(replay.text(), answer.text());
    assert_eq!(journal_record(cluster, &key)["attempts"], 1);
}

pub(super) fn start_funded_cluster() -> (Cluster, CustodyChain) {
    let root =
        std::env::temp_dir().join(format!("layerx-custody-{}-{}", std::process::id(), token()));
    make_dir(&root, 0o700);
    let mut chain = CustodyChain {
        root,
        nodes: Vec::new(),
        evidence_arguments: Vec::new(),
    };
    let actor = actor();
    let asset = format!("0x{}", hex(&random32()));
    let beneficiary = format!("0x{}", hex(&actor.source));
    let actor_key = chain.root.join("actor.key");
    write(&actor_key, &actor.signing_key.to_bytes(), 0o600);
    let primary = start_anvil(&mut chain, None);
    let deployment_path = chain.root.join("deployment.json");
    producer(
        "deploy_local_custody.py",
        &[
            "--rpc".into(),
            primary.clone(),
            "--asset".into(),
            asset.clone(),
            "--beneficiary".into(),
            beneficiary.clone(),
            "--amount".into(),
            FUNDING_AMOUNT.to_string(),
            "--output".into(),
            text(&deployment_path),
            "--allow-local-chain".into(),
        ],
    );
    let deployment: serde_json::Value = must(
        serde_json::from_slice(&must(fs::read(&deployment_path), "real custody deployment")),
        "custody deployment JSON",
    );
    assert_eq!(deployment["chain_id"], CUSTODY_CHAIN_ID);
    assert_eq!(field(&deployment, "beneficiary"), beneficiary);
    assert_eq!(field(&deployment, "asset"), asset);
    assert_eq!(field(&deployment, "amount"), FUNDING_AMOUNT.to_string());
    let (profile, credit) =
        produce_custody_credit(&mut chain, &primary, &deployment, &actor, &asset);
    let cluster = start_cluster_with_custody(Some(CustodySetup {
        profile_path: profile.clone(),
        actor,
    }));
    check_readiness(&cluster);
    credit_actor(&cluster, &chain.root, &profile, &credit, &actor_key);
    (cluster, chain)
}

pub(super) struct MultiassetFunding {
    pub(super) symbol: &'static str,
    pub(super) asset: [u8; 32],
    pub(super) actor: Actor,
    pub(super) account: [u8; 32],
    pub(super) amount: u128,
    pub(super) credit_activity: [u8; 32],
    pub(super) credit_receipt: Vec<u8>,
    pub(super) next_sequence: u64,
    pub(super) asset_next_sequence: u64,
    pub(super) fee_account: [u8; 32],
    pub(super) fee_balance: u128,
}

fn multiasset_account(name: &str) -> [u8; 32] {
    Sha256::new()
        .chain_update(b"LX:ACCOUNT:v1")
        .chain_update(must(u32::try_from(name.len()), "multiasset account name").to_be_bytes())
        .chain_update(name.as_bytes())
        .finalize()
        .into()
}

pub(super) fn multiasset_identity_sequence(cluster: &Cluster, actor: &Actor) -> u64 {
    use layerx_client::lni::handshake::{perform, HandshakeConfig};
    use layerx_client::lni::preparation::{preparation_state, PreparationStateContext};
    use layerx_client::lni::schema::Version;
    let mut connection = AccountProofConnection::connect(cluster);
    let handshake = must(
        perform(
            &mut connection,
            &HandshakeConfig {
                built_interface_version: Version::V1_7,
                expected_protocol_version: PROTOCOL_VERSION,
                expected_network_id: NETWORK_ID,
            },
            None,
        ),
        "multiasset identity handshake",
    );
    assert_eq!(
        handshake.node().authorised_sequencer_key,
        cluster.sequencer_key
    );
    let state = must(
        preparation_state(
            &mut connection,
            &must(Did::new(actor.did.as_bytes()), "multiasset actor DID"),
            PreparationStateContext {
                interface_version: handshake.node().interface_version,
                expected_network_id: NETWORK_ID,
                minimum_observed_head: handshake.node().chain_head_sequence,
                correlation_id: 2,
            },
        ),
        "authenticated native identity sequence",
    );
    state.account_sequence
}

pub(super) fn multiasset_account_read(
    cluster: &Cluster,
    id: [u8; 32],
) -> layerx_proof::state::CanonicalAccount {
    use layerx_client::evidence::RootSelector;
    use layerx_client::lni::handshake::{perform, HandshakeConfig};
    use layerx_client::lni::schema::Version;
    use layerx_client::read::{account, ReadContext, Requested};
    use layerx_types::verify::VerificationLevel;
    let mut connection = AccountProofConnection::connect(cluster);
    let handshake = must(
        perform(
            &mut connection,
            &HandshakeConfig {
                built_interface_version: Version::V1_7,
                expected_protocol_version: PROTOCOL_VERSION,
                expected_network_id: NETWORK_ID,
            },
            None,
        ),
        "multiasset account handshake",
    );
    assert_eq!(
        handshake.node().authorised_sequencer_key,
        cluster.sequencer_key
    );
    let sequencer = must(
        unhex(&cluster.sequencer_environment["LAYERX_NODE_SEQUENCER_ID"]).try_into(),
        "multiasset actual sequencer identity",
    );
    let value = must(
        account(
            &mut connection,
            id,
            ReadContext {
                interface_version: handshake.node().interface_version,
                correlation_id: 2,
                expected_protocol_version: PROTOCOL_VERSION,
                expected_network_id: NETWORK_ID,
                requested: Requested::new(VerificationLevel::STATE_PROVEN),
                head: layerx_client::head::HeadTracker::new(handshake.node()).current(),
                sequencer_authorization: SequencerAuthorization::new(
                    sequencer,
                    cluster.sequencer_key,
                    FIRST_BATCH,
                    LAST_BATCH,
                ),
                handshake_sequencer_key: cluster.sequencer_key,
                root_selector: RootSelector::Latest,
            },
        ),
        "multiasset STATE_PROVEN account",
    );
    assert_eq!(value.achieved(), VerificationLevel::STATE_PROVEN);
    must(
        layerx_proof::state::decode_account_value(id, value.canonical_bytes()),
        "multiasset canonical account",
    )
}

fn multiasset_submit(cluster: &Cluster, signed: &[u8], registry: &ModuleRegistry) -> Vec<u8> {
    let activity = must(
        decode_signed(signed, registry),
        "multiasset signed activity",
    );
    let key = hex(&activity.idempotency_key());
    let deadline = Instant::now() + Duration::from_secs(60);
    let answer = loop {
        let answer = cluster.client.call(&Call::submit(
            "/v1/activities",
            &cluster.gateway_token,
            &key,
            signed,
        ));
        if answer.status != 202 || Instant::now() >= deadline {
            break answer;
        }
        thread::sleep(Duration::from_millis(100));
    };
    assert_eq!(answer.status, 200, "{}", answer.text());
    let result = answer.json();
    assert_eq!(field(&result["result"], "state"), "completed");
    let bytes = unhex(field(&result["result"], "receipt"));
    let receipt = must(
        verify_sequencer_signature(&bytes, cluster.sequencer_key),
        "multiasset receipt signature",
    );
    let protocol = receipt
        .protocol()
        .unwrap_or_else(|| panic!("multiasset native receipt required"));
    assert_eq!(
        protocol.activity_id(),
        must(activity_id(&activity), "multiasset activity identity")
    );
    assert_eq!(protocol.result_code(), 0, "multiasset funding refused");
    verify_funding_batch(cluster, &receipt, &bytes);
    let replay = cluster.client.call(&Call::submit(
        "/v1/activities",
        &cluster.gateway_token,
        &key,
        signed,
    ));
    assert_eq!(replay.status, 200, "{}", replay.text());
    assert_eq!(replay.text(), answer.text());
    assert_eq!(journal_record(cluster, &key)["attempts"], 1);
    bytes
}

pub(super) fn start_funded_multiasset_cluster() -> (Cluster, [MultiassetFunding; 4]) {
    use std::os::unix::fs::MetadataExt as _;
    #[derive(serde::Deserialize)]
    #[serde(deny_unknown_fields)]
    struct Metadata {
        assets: Vec<AssetMetadata>,
    }
    #[derive(serde::Deserialize)]
    #[serde(deny_unknown_fields)]
    struct AssetMetadata {
        symbol: String,
        asset_id: String,
        token_pointer: String,
        decimals: u8,
    }
    const SYMBOLS: [&str; 4] = ["PAX", "SID", "USDC", "USDL"];
    let fixture = PathBuf::from(
        std::env::var_os("LAYERX_MULTI_ASSET_CUSTODY_FIXTURE")
            .unwrap_or_else(|| panic!("genuine LAYERX_MULTI_ASSET_CUSTODY_FIXTURE required")),
    );
    assert!(fixture.is_absolute());
    assert_eq!(
        must(fs::canonicalize(&fixture), "multiasset fixture path"),
        fixture
    );
    let read = |name: &str, secret: bool| {
        let path = fixture.join(name);
        let metadata = must(
            fs::symlink_metadata(&path),
            "genuine multiasset fixture input",
        );
        assert!(metadata.is_file() && !metadata.file_type().is_symlink());
        assert_eq!(metadata.uid(), effective_uid());
        assert_eq!(metadata.nlink(), 1);
        assert_eq!(metadata.mode() & if secret { 0o077 } else { 0o022 }, 0);
        assert!(metadata.len() > 0 && metadata.len() <= 1_048_576);
        must(fs::read(path), "multiasset fixture bytes")
    };
    let metadata: Metadata = must(
        serde_json::from_slice(&read("custody-assets.json", false)),
        "closed custody assets",
    );
    assert_eq!(metadata.assets.len(), 4);
    let custody_registry = read("custody.registry", false);
    assert_eq!(custody_registry.len(), 901);
    assert_eq!(&custody_registry[..5], b"LXBR1");
    let request = read("request.lxgb", false);
    assert!(request.len() >= 11 && &request[..5] == b"LXGB\x02");
    assert_eq!(&request[5..7], &PROTOCOL_VERSION.to_be_bytes());
    assert_eq!(
        &request[7..11],
        &NETWORK_ID.to_be_bytes(),
        "signed fixture cannot be retargeted"
    );
    assert_eq!(read("genesis.key", true).len(), 32);
    let mut rows = Vec::new();
    let mut pointers = BTreeSet::new();
    for (index, symbol) in SYMBOLS.into_iter().enumerate() {
        let declared = &metadata.assets[index];
        let asset: [u8; 32] =
            Sha256::digest(format!("layerx-asset:125:{symbol}").as_bytes()).into();
        assert_eq!(declared.symbol, symbol);
        assert_eq!(declared.asset_id, hex(&asset));
        assert!(declared.decimals <= 38);
        assert_eq!(declared.token_pointer.len(), 42);
        assert!(declared.token_pointer.starts_with("0x"));
        let pointer = unhex(&declared.token_pointer[2..]);
        assert_eq!(declared.token_pointer, format!("0x{}", hex(&pointer)));
        if symbol == "PAX" {
            assert_eq!(pointer, [0; 20]);
            assert_eq!(declared.decimals, 6);
        } else {
            assert_ne!(pointer, [0; 20]);
            assert!(pointers.insert(pointer));
        }
        let offset = 5 + index * 224;
        assert_eq!(
            custody_registry[offset],
            must(u8::try_from(index + 1), "asset registry ordinal")
        );
        let profile = &custody_registry[offset + 1..offset + 224];
        assert_eq!(&profile[..5], b"LXBC4");
        assert_eq!(&profile[5..13], &125_u64.to_be_bytes());
        assert_eq!(&profile[97..129], &asset);
        assert_eq!(
            &profile[129..161],
            &multiasset_account(&format!("system:paxeer-reserve:{}", symbol.to_lowercase()))
        );
        assert_eq!(&profile[201..205], &NETWORK_ID.to_be_bytes());
        assert_eq!(&profile[205..207], &PROTOCOL_VERSION.to_be_bytes());
        if index > 0 {
            assert_eq!(&profile[5..97], &custody_registry[6 + 5..6 + 97]);
            assert_eq!(&profile[161..223], &custody_registry[6 + 161..6 + 223]);
        }
        let seed: [u8; 32] = must(
            read(&format!("{symbol}.actor.key"), true).try_into(),
            "actual beneficiary key",
        );
        let signing_key = SigningKey::from_bytes(&seed);
        let did = format!(
            "did:layerx:{}",
            hex(&signing_key.verifying_key().to_bytes())
        );
        let source = multiasset_account(&format!("agent:{did}:main"));
        let account = multiasset_account(&format!("agent:{did}:asset:{}", hex(&asset)));
        let credit = read(&format!("{symbol}.credit"), false);
        assert!(credit.len() >= 400 && &credit[..5] == b"LXDC3");
        assert_eq!(&credit[5..37], &Sha256::digest(profile)[..]);
        assert_eq!(&credit[37..41], &NETWORK_ID.to_be_bytes());
        assert_eq!(&credit[75..107], &asset);
        assert_eq!(&credit[107..139], &account);
        assert_eq!(&credit[139..171], &signing_key.verifying_key().to_bytes());
        assert_eq!(&credit[327..359], &Sha256::digest(&credit[363..])[..]);
        let amount = u128::from_be_bytes(must(credit[191..207].try_into(), "actual credit amount"));
        assert!(amount > 0);
        rows.push(MultiassetFunding {
            symbol,
            asset,
            actor: Actor {
                signing_key,
                did,
                source,
            },
            account,
            amount,
            credit_activity: [0; 32],
            credit_receipt: Vec::new(),
            next_sequence: 0,
            asset_next_sequence: 0,
            fee_account: [0; 32],
            fee_balance: 0,
        });
    }
    let copy_actor = |actor: &Actor| Actor {
        signing_key: SigningKey::from_bytes(&actor.signing_key.to_bytes()),
        did: actor.did.clone(),
        source: actor.source,
    };
    let cluster = start_cluster_with_setup(
        None,
        Some(RegistryGenesisSetup {
            fixture: fixture.clone(),
            asset: rows[0].asset,
            actor: copy_actor(&rows[0].actor),
            additional: rows
                .iter()
                .skip(1)
                .map(|row| copy_actor(&row.actor))
                .collect(),
        }),
    );
    check_readiness(&cluster);
    for row in &mut rows {
        let profile = cluster.root.join(format!("{}.profile", row.symbol));
        let index = SYMBOLS
            .iter()
            .position(|symbol| *symbol == row.symbol)
            .unwrap_or_else(|| panic!("asset symbol"));
        write(
            &profile,
            &custody_registry[6 + index * 224..229 + index * 224],
            0o600,
        );
        let output = cluster.root.join(format!("{}.credit.activity", row.symbol));
        let sequence = multiasset_identity_sequence(&cluster, &row.actor);
        command(
            &text(&repository_root().join("build/tests/bridge/sign-credit")),
            &[
                "--asset-profile",
                &text(&profile),
                &text(&fixture.join(format!("{}.credit", row.symbol))),
                &row.actor.did,
                &text(&fixture.join(format!("{}.actor.key", row.symbol))),
                &sequence.to_string(),
                &now_ms().saturating_sub(1000).to_string(),
                &text(&output),
            ],
        );
        let signed = must(fs::read(output), "actual signed multiasset credit");
        row.credit_activity = must(
            activity_id(&must(
                decode_signed(&signed, &bridge_registry()),
                "asset credit",
            )),
            "credit activity identity",
        );
        row.credit_receipt = multiasset_submit(&cluster, &signed, &bridge_registry());
        let account = multiasset_account_read(&cluster, row.account);
        assert_eq!(account.asset_id(), row.asset);
        assert_eq!(account.balance(), row.amount);
        assert_eq!(
            account.name,
            format!("agent:{}:asset:{}", row.actor.did, hex(&row.asset)).as_bytes()
        );
    }
    let owners: BTreeMap<_, _> = rows
        .iter()
        .map(|row| {
            (
                row.actor.did.clone(),
                multiasset_account(&format!(
                    "agent:{}:asset:{}",
                    row.actor.did,
                    hex(&rows[0].asset)
                )),
            )
        })
        .collect();
    let amount = rows[0].amount / must(u128::try_from(owners.len() + 1), "fee funding recipients");
    assert!(amount > 0, "genuine PAX credit too small for fee accounts");
    for destination in owners.values() {
        if *destination == rows[0].account {
            continue;
        }
        let source = multiasset_account_read(&cluster, rows[0].account);
        let identity = multiasset_identity_sequence(&cluster, &rows[0].actor);
        let signed = signed_multiasset_fee_send(
            &rows[0].actor,
            rows[0].account,
            *destination,
            rows[0].asset,
            amount,
            source.next_sequence,
            identity,
        );
        let bytes = multiasset_submit(&cluster, &signed, &registry());
        let receipt = must(
            verify_sequencer_signature(&bytes, cluster.sequencer_key),
            "fee funding receipt",
        );
        let protocol = receipt
            .protocol()
            .unwrap_or_else(|| panic!("fee funding protocol receipt"));
        assert_eq!(protocol.from(), rows[0].account);
        assert_eq!(protocol.to(), *destination);
        assert_eq!(protocol.asset(), rows[0].asset);
        assert_eq!(protocol.amount(), amount);
        let after = multiasset_account_read(&cluster, rows[0].account);
        assert_eq!(after.balance().checked_add(amount), Some(source.balance()));
        assert_eq!(
            after.next_sequence,
            must(
                source
                    .next_sequence
                    .checked_add(1)
                    .ok_or("sequence overflow"),
                "source sequence"
            )
        );
        let funded = multiasset_account_read(&cluster, *destination);
        assert_eq!(funded.asset_id(), rows[0].asset);
        assert_eq!(funded.balance(), amount);
    }
    let fee_asset = rows[0].asset;
    for row in &mut rows {
        row.next_sequence = multiasset_identity_sequence(&cluster, &row.actor);
        row.asset_next_sequence = multiasset_account_read(&cluster, row.account).next_sequence;
        row.fee_account = multiasset_account(&format!(
            "agent:{}:asset:{}",
            row.actor.did,
            hex(&fee_asset)
        ));
        row.fee_balance = multiasset_account_read(&cluster, row.fee_account).balance();
    }
    let rows = match rows.try_into() {
        Ok(rows) => rows,
        Err(_) => panic!("exact four funded assets required"),
    };
    (cluster, rows)
}

fn signed_multiasset_fee_send(
    actor: &Actor,
    from: [u8; 32],
    to: [u8; 32],
    asset: [u8; 32],
    amount: u128,
    source_sequence: u64,
    identity_sequence: u64,
) -> Vec<u8> {
    let idempotency = random32();
    let expires_at = now_ms() + 120_000;
    let mut context = Vec::new();
    context.extend(from);
    context.extend(to);
    context.extend(asset);
    context.extend(amount.to_be_bytes());
    context.extend(idempotency);
    let context = domain_hash(Domain::ContextHash, &context);
    let payload_bytes = send_payload(
        &actor.signing_key,
        from,
        to,
        asset,
        amount,
        source_sequence,
        idempotency,
        expires_at,
        context,
    );
    let kind = must(
        ActivityType::new(ModuleId::Asset, SEND_ACTIVITY),
        "fee SEND kind",
    );
    let payload = must(
        Payload::new(&registry(), kind, &payload_bytes),
        "fee SEND payload",
    );
    let mut builder = EnvelopeBuilder::new();
    must(
        builder
            .protocol_version(PROTOCOL_VERSION)
            .and_then(|b| b.network_id(NETWORK_ID))
            .and_then(|b| b.activity_type(kind))
            .and_then(|b| b.actor_did(must(Did::new(actor.did.as_bytes()), "fee actor")))
            .and_then(|b| {
                b.authority(must(
                    Authority::owner(&actor.signing_key.verifying_key().to_bytes()),
                    "fee owner",
                ))
            })
            .and_then(|b| b.account_sequence(identity_sequence))
            .and_then(|b| {
                b.timestamp_bound(must(
                    TimestampBound::new(now_ms().saturating_sub(30_000), expires_at),
                    "fee time",
                ))
            })
            .and_then(|b| b.idempotency_key(IdempotencyKey::new(idempotency)))
            .and_then(|b| b.fee_limit(Amount::from_u128(0)))
            .and_then(|b| b.payload_hash(domain_hash(Domain::PayloadHash, payload.as_bytes())))
            .and_then(|b| b.payload(payload))
            .map(|_| ()),
        "fee SEND envelope",
    );
    let unsigned = must(builder.build(), "fee unsigned SEND");
    let signature = actor
        .signing_key
        .sign(&domain_hash(
            Domain::SignaturePreimage,
            &must(encode_unsigned_envelope(&unsigned), "fee signing bytes"),
        ))
        .to_bytes();
    must(
        encode_signed_envelope(
            &unsigned.attach_signature(must(Signature::new(&signature), "fee signature")),
        ),
        "fee signed SEND",
    )
}

fn produce_custody_credit(
    chain: &mut CustodyChain,
    primary: &str,
    deployment: &serde_json::Value,
    actor: &Actor,
    asset: &str,
) -> (PathBuf, PathBuf) {
    let beneficiary = format!("0x{}", hex(&actor.source));
    let beneficiary_key = format!("0x{}", hex(&actor.signing_key.verifying_key().to_bytes()));
    let fork_block = deployment["fork_block"]
        .as_u64()
        .unwrap_or_else(|| panic!("custody fork block missing"));
    let secondary = start_anvil(chain, Some((primary, fork_block)));
    assert_ne!(primary, secondary);
    let profile = chain.root.join("profile.bin");
    producer(
        "custody_credit.py",
        &[
            "profile".into(),
            "--rpc".into(),
            primary.to_owned(),
            "--rpc".into(),
            secondary.clone(),
            "--network-id".into(),
            NETWORK_ID.to_string(),
            "--chain-id".into(),
            CUSTODY_CHAIN_ID.to_string(),
            "--vault".into(),
            field(deployment, "vault").into(),
            "--runtime-sha256".into(),
            field(deployment, "runtime_sha256").into(),
            "--asset".into(),
            asset.to_owned(),
            "--trusted-height".into(),
            "1".into(),
            "--trusting-period-seconds".into(),
            "1209600".into(),
            "--output".into(),
            text(&profile),
        ],
    );
    assert_eq!(
        must(fs::metadata(&profile), "custody profile metadata").len(),
        must(u64::try_from(CUSTODY_PROFILE_BYTES), "profile length")
    );
    let credit = chain.root.join("credit.bin");
    let evidence_arguments = vec![
        "--rpc".into(),
        primary.to_owned(),
        "--rpc".into(),
        secondary,
        "--profile".into(),
        text(&profile),
        "--network-id".into(),
        NETWORK_ID.to_string(),
        "--transaction".into(),
        field(deployment, "transaction").into(),
        "--beneficiary".into(),
        beneficiary,
        "--beneficiary-key".into(),
        beneficiary_key,
        "--expected-amount".into(),
        FUNDING_AMOUNT.to_string(),
    ];
    let mut attest_arguments = vec!["attest".into()];
    attest_arguments.extend(evidence_arguments.clone());
    attest_arguments.extend(["--output".into(), text(&credit)]);
    producer("custody_credit.py", &attest_arguments);
    chain.evidence_arguments = evidence_arguments;
    (profile, credit)
}

#[test]
fn maintained_program_journal_rejects_missing_corrupt_and_substituted_attachments() {
    let cluster = start_cluster();
    check_readiness(&cluster);
    let signed = signed_program_call(&cluster.actor, random32());
    let key = format!("maintenance-{}", token());
    let deadline = Instant::now() + Duration::from_secs(30);
    let submitted = loop {
        let answer = cluster.client.call(&Call::submit(
            "/v1/programs/call",
            &cluster.gateway_token,
            &key,
            &signed,
        ));
        if answer.status == 200 || Instant::now() >= deadline {
            break answer;
        }
        assert!(
            answer.status == 202 || answer.status == 503,
            "{}",
            answer.text()
        );
        thread::sleep(Duration::from_millis(100));
    };
    assert_eq!(submitted.status, 200, "{}", submitted.text());
    let key_digest = format!("{:x}", Sha256::digest(key.as_bytes()));
    let journal = cluster
        .state_dir
        .join("journal")
        .join(format!("{key_digest}.json"));
    let original = must(fs::read(&journal), "maintained journal");
    let document: serde_json::Value =
        must(serde_json::from_slice(&original), "maintained journal JSON");
    assert_eq!(
        document["program_execution"]["evidence"]["batch_identity"]["kind"],
        "batch_maintenance_v1"
    );
    for case in 0..5 {
        let mut corrupted = document.clone();
        let evidence = &mut corrupted["program_execution"]["evidence"];
        match case {
            0 => {
                evidence
                    .as_object_mut()
                    .unwrap_or_else(|| panic!("batch evidence"))
                    .remove("batch_identity");
            }
            1 => {
                evidence["batch_identity"] = serde_json::json!({"kind": "historical"});
            }
            2 => {
                let mut bytes = unhex(field(&evidence["batch_identity"], "receipt_hex"));
                let last = bytes.len() - 1;
                bytes[last] ^= 1;
                evidence["batch_identity"]["receipt_hex"] = serde_json::json!(hex(&bytes));
            }
            3 => {
                evidence["batch_identity"]["receipt_proof_hex"] =
                    evidence["receipt_proof_hex"].clone();
            }
            4 => {
                evidence["batch_identity"]["kind"] = serde_json::json!("unknown");
            }
            _ => unreachable!(),
        }
        must(
            fs::write(
                &journal,
                must(serde_json::to_vec(&corrupted), "corrupt maintained journal"),
            ),
            "write corrupt maintained journal",
        );
        let refused = cluster.client.call(&Call::submit(
            "/v1/programs/call",
            &cluster.gateway_token,
            &key,
            &signed,
        ));
        let expected_code = if case == 4 {
            "persistence_unavailable"
        } else {
            "program_artifacts_invalid"
        };
        assert_refusal(&refused, 503, expected_code);
    }
    must(fs::write(&journal, original), "restore maintained journal");
    let replay = cluster.client.call(&Call::submit(
        "/v1/programs/call",
        &cluster.gateway_token,
        &key,
        &signed,
    ));
    assert_eq!(replay.status, 200, "{}", replay.text());
    assert_eq!(replay.text(), submitted.text());
    assert_eq!(journal_record(&cluster, &key)["attempts"], 1);
}
