use super::*;
use layerx_client::lni::handshake::{perform, HandshakeConfig};
use layerx_client::lni::schema::Version;
use layerx_client::lni::transport::{FrameTransport, TransportError};
use layerx_client::receipt::{
    lookup_authenticated, AuthenticatedLookup, AuthenticatedLookupContext, ReceiptWaitMode,
};
use layerx_client::submit::{submit_signed, Submission, SubmissionContext};
use layerx_programs::{
    DeploymentProof, InterfaceCapability, InterfaceEntryPoint, ProgramInterface,
    ProgramStateBundle, ProtocolDeploymentVerifier, ValueSchema, ValueType,
};
use layerx_programs_runtime::test_support::{
    code_section, func_body, function_section, import_section, module, raw_section, type_section,
    unsigned_leb, OP_CALL, OP_DROP, OP_END, OP_I32_CONST, TYPE_I32,
};
use layerx_types::intent::ProgramId;
use layerx_types::program_call::{NativeProgramCall, Resources};
use layerx_types::program_lifecycle::{
    NativeProgramDeploy, NativeProgramUpgrade, NativeProgramWindDown, ProgramUpgradePolicy,
    ProgramWindDownOperation,
};

const FEE_LIMIT: u128 = 1_000_000_000_000;
const DOMAIN_BYTES: usize = b"LayerX/program-interface/v1\0".len();

struct NativeConnection {
    child: Child,
    input: std::process::ChildStdin,
    output: std::process::ChildStdout,
}

impl NativeConnection {
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
            "production Unix LNI connection",
        );
        Self {
            input: child
                .stdin
                .take()
                .unwrap_or_else(|| panic!("missing LNI input")),
            output: child
                .stdout
                .take()
                .unwrap_or_else(|| panic!("missing LNI output")),
            child,
        }
    }
}
impl FrameTransport for NativeConnection {
    fn send(&mut self, bytes: &[u8]) -> Result<(), TransportError> {
        layerx_client::lni::framing::write_frame(&mut self.input, bytes, LNI_FRAME_BYTES)
    }
    fn receive(&mut self) -> Result<Vec<u8>, TransportError> {
        layerx_client::lni::framing::read_frame(&mut self.output, LNI_FRAME_BYTES)
    }
}
impl Drop for NativeConnection {
    fn drop(&mut self) {
        let _ = self.child.kill();
        let _ = self.child.wait();
    }
}

fn registry() -> ModuleRegistry {
    let types: Vec<_> = [1, 2, 3, 7]
        .into_iter()
        .map(|ordinal| {
            must(
                ActivityType::new(ModuleId::Programs, ordinal),
                "activity type",
            )
        })
        .collect();
    must(
        ModuleRegistry::new(&[must(
            ModuleRegistration::new(ModuleId::Programs, &types),
            "program registration",
        )]),
        "program registry",
    )
}

fn submit(
    cluster: &Cluster,
    sequence: &mut u64,
    ordinal: u16,
    payload: &[u8],
    expected: i32,
    label: &str,
    evidence: &Path,
) -> Vec<u8> {
    let signed = signed_program_operation(&cluster.actor, ordinal, *sequence, FEE_LIMIT, payload);
    let activity = must(decode_signed(&signed, &registry()), "signed activity");
    let identifier = must(activity_id(&activity), "activity identity");
    let mut connection = NativeConnection::connect(cluster);
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
        "native handshake",
    );
    assert_eq!(
        handshake.node().authorised_sequencer_key,
        cluster.sequencer_key
    );
    let context = SubmissionContext {
        interface_version: handshake.node().interface_version,
        protocol_version: PROTOCOL_VERSION,
        network_id: NETWORK_ID,
        correlation_id: 2,
        signer_public_key: cluster.actor.signing_key.verifying_key().to_bytes(),
        attempt: 1,
    };
    match must(
        submit_signed(&mut connection, &registry(), context, &signed),
        "signed native submit",
    ) {
        Submission::Acknowledged(ack) => assert_eq!(ack.activity_id(), identifier),
        Submission::Unknown(_) => panic!("{label}: submission unresolved"),
    }
    let received = must(
        lookup_authenticated(
            &mut connection,
            identifier,
            AuthenticatedLookupContext {
                interface_version: handshake.node().interface_version,
                correlation_id: 3,
                sequencer_public_key: cluster.sequencer_key,
                wait_mode: ReceiptWaitMode::Published,
            },
        ),
        "native authenticated receipt",
    );
    let AuthenticatedLookup::Verified(receipt) = received else {
        panic!("{label}: missing receipt")
    };
    let protocol = receipt
        .receipt()
        .protocol()
        .unwrap_or_else(|| panic!("protocol receipt"));
    assert_eq!(protocol.activity_id(), identifier);
    assert_eq!(protocol.module_id(), 9);
    assert_eq!(protocol.protocol_version(), PROTOCOL_VERSION);
    assert_eq!(protocol.result_code(), expected, "{label}");
    assert_eq!(protocol.operation(), if ordinal == 3 { 3 } else { 0 });
    if ordinal == 3 && expected == 0 {
        let outcome = protocol
            .program_outcome()
            .unwrap_or_else(|| panic!("call outcome absent"));
        assert_eq!(
            outcome.abi_version(),
            u16::from_be_bytes([payload[32], payload[33]])
        );
        assert_eq!(outcome.result_code(), 0);
    }
    let authority = super::lifecycle::verify_lifecycle_batch(
        cluster,
        receipt.receipt(),
        receipt.canonical_bytes(),
    );
    if ordinal == 3 && expected == 0 {
        let digest = must(
            layerx_wire::hash::receipt_digest(&must(
                layerx_wire::receipt::encode_unsigned(receipt.receipt()),
                "unsigned receipt",
            )),
            "receipt digest",
        );
        let response = http_get(
            cluster.program_port,
            &format!(
                "/v1/programs/activities/{}/artifacts?receipt_digest={}",
                hex(&identifier),
                hex(&digest)
            ),
            &cluster.program_token,
        );
        assert_eq!(
            response.status, 200,
            "{label}: native execution artifacts unavailable"
        );
        let document = response.json();
        let call = must(NativeProgramCall::decode(payload), "call expectation");
        must(
            layerx_proof::program::verify_authorized_program_execution(
                receipt.canonical_bytes(),
                &unhex(field(&document, "terminal_payload")),
                &unhex(field(&document, "call_graph")),
                &layerx_proof::program::AuthorizedProgramExecutionExpectation {
                    authority,
                    activity_id: identifier,
                    payload_hash: must(
                        layerx_wire::hash::payload_hash(&activity),
                        "payload digest",
                    ),
                    program_id: call.program_id.bytes(),
                    guest_abi_version: call.guest_abi,
                },
            ),
            "native committed execution proof",
        );
        write(
            &evidence.join(format!("{label}.artifacts.json")),
            response.text().as_bytes(),
            0o600,
        );
    } else if ordinal != 3 && expected == 0 {
        must(
            layerx_proof::receipt::verify_program_state(receipt.canonical_bytes(), &authority),
            "native lifecycle state receipt",
        );
    }
    *sequence += 1;
    write(&evidence.join(format!("{label}.activity")), &signed, 0o600);
    write(
        &evidence.join(format!("{label}.receipt")),
        receipt.canonical_bytes(),
        0o600,
    );
    println!("NATIVE_INTERFACE_CASE {label}");
    signed
}

fn guest(capability: u8, distinct: u8) -> Vec<u8> {
    let imports = match capability {
        0 => Vec::new(),
        1 => vec![("layerx_v1", "storage_read", 0)],
        2 => vec![("layerx_v2", "transfer_program_402", 3)],
        3 => vec![("layerx_v3", "oracle_read", 0)],
        4 => vec![("layerx_v4", "web_read", 0)],
        _ => panic!("unknown guest capability"),
    };
    let n = u64::try_from(imports.len()).unwrap_or_else(|_| panic!("import bound"));
    let mut exports = unsigned_leb(3);
    for (name, kind, index) in [
        ("layerx_reserve", 0_u8, n),
        ("call", 0, n + 1),
        ("memory", 2, 0),
    ] {
        exports.extend(unsigned_leb(name.len() as u64));
        exports.extend_from_slice(name.as_bytes());
        exports.push(kind);
        exports.extend(unsigned_leb(index));
    }
    let mut body = Vec::new();
    for _ in 0..distinct {
        body.extend([OP_I32_CONST, 0, OP_DROP]);
    }
    for (index, (_, _, ty)) in imports.iter().enumerate() {
        body.extend([OP_I32_CONST, 0, 0x04, 0x40]);
        if *ty == 3 {
            body.extend([0x42, 0, 0x42, 0]);
            for _ in 0..8 {
                body.extend([OP_I32_CONST, 0]);
            }
        } else {
            for _ in 0..4 {
                body.extend([OP_I32_CONST, 0]);
            }
        }
        body.push(OP_CALL);
        body.extend(unsigned_leb(index as u64));
        body.extend([OP_DROP, OP_END]);
    }
    body.extend([OP_I32_CONST, 0, OP_END]);
    module(&[
        type_section(&[
            (&[TYPE_I32; 4], &[TYPE_I32]),
            (&[TYPE_I32], &[TYPE_I32]),
            (&[TYPE_I32; 2], &[TYPE_I32]),
            (
                &[
                    0x7e, 0x7e, TYPE_I32, TYPE_I32, TYPE_I32, TYPE_I32, TYPE_I32, TYPE_I32,
                    TYPE_I32, TYPE_I32,
                ],
                &[TYPE_I32],
            ),
        ]),
        import_section(&imports),
        function_section(&[1, 2]),
        raw_section(5, &[1, 1, 1, 1]),
        raw_section(7, &exports),
        code_section(&[
            func_body(&[], &[OP_I32_CONST, 0, OP_END]),
            func_body(&[], &body),
        ]),
    ])
}

fn interface(wasm: &[u8], abi: u16, capability: u8, input: u32) -> ProgramInterface {
    let capabilities = match capability {
        0 => Vec::new(),
        1 => vec![InterfaceCapability::StorageRead],
        2 => vec![InterfaceCapability::CallerAuthorizedSpend {
            asset: [2; 32],
            maximum_amount: 100,
            recipient_offset: 8,
            amount_offset: 40,
        }],
        3 => vec![InterfaceCapability::OracleRead],
        4 => vec![InterfaceCapability::WebRead],
        _ => panic!("unknown interface capability"),
    };
    must(
        ProgramInterface::bind(
            wasm,
            abi,
            vec![InterfaceEntryPoint {
                name: "call".into(),
                discriminator: [1, 2, 3, 4],
                calldata: ValueSchema::layerx(ValueType::Bytes { max_len: input }),
                response: ValueSchema::layerx(ValueType::Bytes { max_len: 64 }),
                capabilities,
                event_topics: Vec::new(),
                failures: Vec::new(),
            }],
        ),
        "canonical module-bound interface",
    )
}
fn deploy(cluster: &Cluster, program: [u8; 32], abi: u16, wasm: &[u8], encoded: &[u8]) -> Vec<u8> {
    must(
        NativeProgramDeploy {
            program_id: ProgramId::new(program),
            guest_abi: abi,
            policy: ProgramUpgradePolicy::Authority(cluster.actor.source),
            new_hash: Sha256::digest(wasm).into(),
            interface: Some(encoded),
            wasm,
        }
        .encode(),
        "deploy",
    )
}
fn upgrade(
    program: [u8; 32],
    abi: u16,
    old: &[u8],
    wasm: &[u8],
    encoded: &[u8],
    breaking: bool,
) -> Vec<u8> {
    must(
        NativeProgramUpgrade {
            program_id: ProgramId::new(program),
            guest_abi: abi,
            old_hash: Sha256::digest(old).into(),
            new_hash: Sha256::digest(wasm).into(),
            migration_hook: &[],
            clear_interface: breaking,
            interface: Some(encoded),
            wasm,
        }
        .encode(),
        "upgrade",
    )
}
fn call(program: [u8; 32], abi: u16, encoded: &ProgramInterface) -> Vec<u8> {
    let calldata = must(encoded.encode_call("call", &[0; 8]), "typed calldata");
    let grants = if encoded.entries()[0]
        .capabilities
        .contains(&InterfaceCapability::StorageRead)
    {
        vec![layerx_programs_runtime::Capability::StorageRead]
    } else {
        Vec::new()
    };
    let capabilities = must(
        layerx_programs_runtime::CapabilitySet::new(grants),
        "call grants",
    )
    .canonical_encoding();
    let budget = layerx_programs_runtime::ResourceBudget::declared();
    must(
        NativeProgramCall {
            program_id: ProgramId::new(program),
            guest_abi: abi,
            entrypoint: b"call",
            calldata: &calldata,
            capabilities: &capabilities,
            access_declaration: b"LayerX/programs/access-declaration/v1\0\0",
            response_capacity: 64,
            resources: Resources([
                budget.cpu_fuel(),
                budget.memory_bytes(),
                budget.storage_read_bytes(),
                budget.storage_write_bytes(),
                u64::from(budget.output_values()),
                budget.output_bytes(),
                u64::from(budget.table_elements()),
            ]),
        }
        .encode(),
        "native call",
    )
}

fn provisioned_history(cluster: &Cluster, path: &Path) {
    let configured = &cluster.sequencer_environment;
    let mut bytes = b"LayerX/sequencer-trust-history/v1\0".to_vec();
    bytes.extend([0, 1, 0, 0]);
    bytes.extend(PROTOCOL_VERSION.to_be_bytes());
    bytes.extend(NETWORK_ID.to_be_bytes());
    bytes.extend(0_u64.to_be_bytes());
    bytes.extend(unhex(configured["LAYERX_NODE_SEQUENCER_ID"].as_str()));
    assert_eq!(
        unhex(configured["LAYERX_NODE_SEQUENCER_PUBLIC_KEY"].as_str()),
        cluster.sequencer_key
    );
    bytes.extend(cluster.sequencer_key);
    bytes.extend(FIRST_BATCH.to_be_bytes());
    bytes.extend(LAST_BATCH.to_be_bytes());
    bytes.extend([0; 9]);
    write(path, &bytes, 0o600);
}

fn current_interface(
    cluster: &Cluster,
    verifier: &ProtocolDeploymentVerifier,
    program: [u8; 32],
    expected: &ProgramInterface,
    evidence: &Path,
    label: &str,
) -> Vec<u8> {
    let head = http_get(
        cluster.program_port,
        "/v1/protocol/account-state/head",
        &cluster.program_token,
    );
    assert_eq!(head.status, 200, "native current head unavailable");
    let head = head.json();
    let sequence = head["observed_sequence"]
        .as_u64()
        .unwrap_or_else(|| panic!("head sequence"));
    let response = http_get(
        cluster.program_port,
        &format!("/v1/programs/{}/account-state?at={sequence}", hex(&program)),
        &cluster.program_token,
    );
    assert_eq!(response.status, 200, "native program state unavailable");
    let document = response.json();
    let bytes = unhex(field(&document, "record_hex"));
    let bundle = must(
        ProgramStateBundle::decode(&bytes),
        "native current state bundle",
    );
    assert_eq!(bundle.canonical_encoding(), bytes);
    let state = &bundle.state;
    let chain = must(
        verifier.verify_current_chain_head(
            bundle.head_kind,
            &state.receipt,
            &state.receipt_proof,
            &state.header,
            &state.header_signature,
            now_ms(),
        ),
        "current chain proof",
    );
    assert_eq!(
        chain.state_root(),
        <[u8; 32]>::try_from(unhex(field(&head, "state_root")))
            .unwrap_or_else(|_| panic!("head root"))
    );
    assert_eq!(chain.global_sequence(), sequence);
    let program = must(
        layerx_programs_runtime::ProgramId::new(program),
        "runtime program",
    );
    let verified = must(
        verifier.verify_current_program_bundle(
            &bundle,
            &chain,
            program,
            &cluster.sequencer_key,
            now_ms(),
        ),
        "registry authenticated stored interface",
    );
    assert_eq!(&verified.interface().interface, expected);
    assert_eq!(
        verified.program_head().abi_version(),
        expected.abi_version()
    );
    assert_eq!(verified.program_head().code_hash(), expected.code_hash());
    let mut damaged = bundle.clone();
    damaged.interface.value[0] ^= 1;
    assert!(verifier
        .verify_current_program_bundle(&damaged, &chain, program, &cluster.sequencer_key, now_ms())
        .is_err());
    let mut damaged = bundle.clone();
    damaged.state.header_signature[0] ^= 1;
    assert!(verifier
        .verify_current_program_bundle(&damaged, &chain, program, &cluster.sequencer_key, now_ms())
        .is_err());
    write(&evidence.join(format!("{label}.state")), &bytes, 0o600);
    println!("NATIVE_INTERFACE_CASE {label}");
    bundle.interface.value
}

fn typed_bindings(
    cluster: &Cluster,
    hosted_registry: &HostedRegistry,
    signed: &[u8],
    interface: &ProgramInterface,
    stored: &[u8],
    history: &Path,
    evidence: &Path,
    label: &str,
) {
    hosted_registry.consume(
        signed,
        interface,
        &format!("{label}-registry-process"),
        evidence,
    );
    let identifier = must(
        activity_id(&must(decode_signed(signed, &registry()), "deployment")),
        "deployment id",
    );
    let deadline = Instant::now() + Duration::from_secs(15);
    let response = loop {
        let path = format!("/internal/v1/deployment-proof/{}", hex(&identifier));
        let mut request = Call::get(&path, Some(&cluster.registry_token));
        request.identity = Some(&cluster.tls.client_identity);
        let response = cluster.client.call(&request);
        if response.status == 200 {
            break response;
        }
        assert_eq!(response.status, 503);
        assert_eq!(response.json()["native_result"].as_i64(), Some(-106));
        assert!(Instant::now() < deadline, "deployment proof unavailable");
        thread::sleep(Duration::from_millis(20));
    };
    let bytes = unhex(field(&response.json(), "proof_hex"));
    let proof = must(DeploymentProof::decode(&bytes), "actual deployment proof");
    assert_eq!(proof.activity, signed);
    let proof_path = evidence.join(format!("{label}.deployment"));
    let interface_path = evidence.join(format!("{label}.interface"));
    write(&proof_path, &bytes, 0o600);
    assert_eq!(&stored[72..], interface.canonical_encoding());
    write(&interface_path, &stored[72..], 0o600);
    let cli = PathBuf::from(
        std::env::var_os("PAXEER_X_PROGRAM_CLI")
            .unwrap_or_else(|| panic!("candidate CLI required")),
    );
    for (suffix, digest, hash, success) in [
        (
            "valid",
            interface.digest().into_bytes(),
            interface.code_hash(),
            true,
        ),
        ("bad-digest", [0; 32], interface.code_hash(), false),
        ("bad-code", interface.digest().into_bytes(), [0; 32], false),
    ] {
        let output = evidence.join(format!("{label}-{suffix}"));
        let result = must(
            Command::new(&cli)
                .args([
                    "--json",
                    "program",
                    "bindings",
                    "--historical",
                    "--interface",
                    &text(&interface_path),
                    "--deployment-proof",
                    &text(&proof_path),
                    "--trust-history",
                    &text(history),
                    "--digest",
                    &hex(&digest),
                    "--code-hash",
                    &hex(&hash),
                    "--output",
                    &text(&output),
                ])
                .output(),
            "shipped typed binding process",
        );
        write(
            &evidence.join(format!("{label}-{suffix}.stdout")),
            &result.stdout,
            0o600,
        );
        write(
            &evidence.join(format!("{label}-{suffix}.stderr")),
            &result.stderr,
            0o600,
        );
        assert_eq!(result.status.success(), success, "{label}:{suffix}");
        if success {
            for file in [
                "client.rs",
                "client.ts",
                "guest.rs",
                "client.go",
                "ProgramBindings.java",
                "client.kt",
                "client.py",
                "client.swift",
                "Client.cs",
                "bindings.json",
            ] {
                assert!(output.join(file).is_file(), "typed artifact {file}");
            }
        } else {
            assert!(!output.exists());
        }
        println!("NATIVE_INTERFACE_CASE {label}-{suffix}");
    }
}

struct HostedRegistry {
    process: Daemon,
    client: Client,
    identity: Identity,
    bearer: String,
    cgroup: PathBuf,
    binary: PathBuf,
    environment: BTreeMap<String, String>,
    host_mount: PathBuf,
}
impl HostedRegistry {
    fn start(cluster: &Cluster, history: &Path) -> Self {
        let configuration = PathBuf::from(
            std::env::var_os("PAXEER_X_REGISTRY_CONFIGURATION")
                .unwrap_or_else(|| panic!("provisioned registry builder configuration required")),
        );
        let mut environment: BTreeMap<String, String> = must(
            serde_json::from_slice(&must(fs::read(configuration), "registry configuration")),
            "registry environment",
        );
        let required: BTreeSet<_> = [
            "LAYERX_REGISTRY_BUILDER_IMAGE_DIGEST",
            "LAYERX_REGISTRY_BUILDER_ENVIRONMENT_ROOT",
            "LAYERX_REGISTRY_BUILDER_ENTRYPOINT",
            "LAYERX_REGISTRY_BUILDER_ISOLATION_RUNTIME",
            "LAYERX_REGISTRY_BUILDER_ISOLATION_RUNTIME_DIGEST",
            "LAYERX_REGISTRY_BUILDER_JOB_SUPERVISOR",
            "LAYERX_REGISTRY_BUILDER_JOB_SUPERVISOR_DIGEST",
        ]
        .into_iter()
        .map(str::to_owned)
        .collect();
        let mut required = required;
        let mut allowed = required.clone();
        for kind in ["PROGRAM", "WEBHOOKS"] {
            for suffix in ["URL", "CA_DER"] {
                required.insert(format!("LAYERX_EVENTS_{kind}_UPSTREAM_{suffix}"));
            }
            for suffix in [
                "URL",
                "CA_DER",
                "TOKEN_FILE",
                "CLIENT_IDENTITY_PKCS12",
                "CLIENT_IDENTITY_PASSWORD_FILE",
                "COOKIE_FILE",
            ] {
                allowed.insert(format!("LAYERX_EVENTS_{kind}_UPSTREAM_{suffix}"));
            }
        }
        let configured: BTreeSet<_> = environment.keys().cloned().collect();
        assert!(required.is_subset(&configured) && configured.is_subset(&allowed));
        let root = cluster.root.join("hosted-registry");
        make_dir(&root, 0o755);
        let state = root.join("state");
        make_dir(&state, 0o700);
        chown(&state, 4030, 4030);
        let tls = tls_material(&root);
        let authority_file = |name: &str, bytes: &[u8]| {
            let path = root.join(name);
            write(&path, bytes, 0o600);
            chown(&path, 4030, 4030);
            path
        };
        let bearer = token();
        let request = authority_file("request.token", bearer.as_bytes());
        let publication = authority_file("publication.token", token().as_bytes());
        let trust = authority_file(
            "trust-history",
            &must(fs::read(history), "provisioned registry trust"),
        );
        let outbound_ca = authority_file(
            "outbound-ca.der",
            &must(fs::read(&cluster.tls.server_der), "boundary CA"),
        );
        for path in [&tls.server_der, &tls.server_key_der, &tls.client_ca_der] {
            chown(path, 4030, 4030);
        }
        let password = authority_file("client-password", token().as_bytes());
        let identity = root.join("outbound-client.p12");
        command(
            "openssl",
            &[
                "pkcs12",
                "-export",
                "-in",
                &text(&cluster.root.join("tls/gateway-client.pem")),
                "-inkey",
                &text(&cluster.root.join("tls/gateway-client-key.pem")),
                "-out",
                &text(&identity),
                "-passout",
                &format!("file:{}", text(&password)),
            ],
        );
        chown(&identity, 4030, 4030);
        must(
            fs::set_permissions(&identity, fs::Permissions::from_mode(0o600)),
            "registry client identity mode",
        );
        let port = free_port();
        let host_mount = root.join("host-cgroup");
        make_dir(&host_mount, 0o755);
        let parent = PathBuf::from(
            std::env::var_os("PAXEER_X_REGISTRY_CGROUP_PARENT")
                .unwrap_or_else(|| panic!("delegated disposable registry cgroup parent required")),
        );
        assert_eq!(must(fs::canonicalize(&parent), "cgroup parent"), parent);
        let enabled = must(
            fs::read_to_string(parent.join("cgroup.subtree_control")),
            "delegated controllers",
        );
        for controller in ["cpu", "memory", "pids", "io"] {
            assert!(enabled.split_whitespace().any(|item| item == controller));
        }
        let cgroup = parent.join(format!(
            "native-interface-{}-{}",
            std::process::id(),
            token()
        ));
        must(fs::create_dir(&cgroup), "dedicated registry cgroup");
        let configured = &cluster.sequencer_environment;
        for (key, value) in [
            ("LAYERX_REGISTRY_STATE", text(&state)),
            ("LAYERX_REGISTRY_LISTEN", format!("127.0.0.1:{port}")),
            ("LAYERX_REGISTRY_HOST_CGROUP_MOUNT", text(&host_mount)),
            ("LAYERX_REGISTRY_REQUEST_TOKEN_FILE", text(&request)),
            ("LAYERX_REGISTRY_PUBLICATION_TOKEN_FILE", text(&publication)),
            ("LAYERX_REGISTRY_TLS_CERT_DER", text(&tls.server_der)),
            ("LAYERX_REGISTRY_TLS_KEY_DER", text(&tls.server_key_der)),
            ("LAYERX_REGISTRY_CLIENT_CA_DER", text(&tls.client_ca_der)),
            (
                "LAYERX_REGISTRY_NODE_ENDPOINT",
                format!("https://localhost:{}", cluster.client.port),
            ),
            (
                "LAYERX_REGISTRY_NODE_AUTHORIZATION",
                cluster.registry_token.clone(),
            ),
            ("LAYERX_REGISTRY_OUTBOUND_CA_DER", text(&outbound_ca)),
            ("LAYERX_REGISTRY_CLIENT_IDENTITY_PKCS12", text(&identity)),
            (
                "LAYERX_REGISTRY_CLIENT_IDENTITY_PASSWORD_FILE",
                text(&password),
            ),
            (
                "LAYERX_REGISTRY_RECEIPT_AUTHORITY_ENDPOINT",
                format!(
                    "http://127.0.0.1:{}",
                    configured["LAYERX_NODE_AUTHORITY_REPLICA_PORT"]
                ),
            ),
            (
                "LAYERX_REGISTRY_RECEIPT_AUTHORITY_AUTHORIZATION",
                configured["LAYERX_NODE_AUTHORITY_REPLICA_BEARER_TOKEN"].clone(),
            ),
            (
                "LAYERX_REGISTRY_RECEIPT_AUTHORITY_REPLICA_ID",
                configured["LAYERX_NODE_AUTHORITY_REPLICA_ID"].clone(),
            ),
            ("LAYERX_REGISTRY_SEQUENCER_TRUST_HISTORY", text(&trust)),
        ] {
            environment.insert(key.to_owned(), value);
        }
        let binary = PathBuf::from(
            std::env::var_os("PAXEER_X_REGISTRY_BINARY")
                .unwrap_or_else(|| panic!("candidate registry binary required")),
        );
        let stderr = root.join("registry.stderr");
        let script = r#"set -eu
printf '%s' "$$" > "$1/cgroup.procs"
shift
exec /usr/bin/unshare --mount --cgroup /bin/sh -c '
set -eu
mount --make-rprivate /
mount --bind /sys/fs/cgroup "$1"
mount -t cgroup2 none /sys/fs/cgroup
shift
exec "$@"
' registry "$@"
"#;
        let child = must(
            Command::new("/bin/sh")
                .args(["-c", script, "registry"])
                .arg(&cgroup)
                .arg(&host_mount)
                .arg(&binary)
                .env_clear()
                .env("PATH", "/usr/bin:/bin")
                .envs(&environment)
                .stdin(Stdio::null())
                .stdout(Stdio::null())
                .stderr(Stdio::from(must(fs::File::create(&stderr), "registry log")))
                .spawn(),
            "real hosted registry",
        );
        let mut process = Daemon { child, stderr };
        wait_for_port(port, &mut process, "real hosted registry");
        Self {
            process,
            client: Client {
                port,
                certificate: tls.server_certificate,
            },
            identity: tls.client_identity,
            bearer,
            cgroup,
            binary,
            environment,
            host_mount,
        }
    }
    fn restart(&mut self) {
        self.process.stop();
        let script = r#"set -eu
printf '%s' "$$" > "$1/cgroup.procs"
shift
exec /usr/bin/unshare --mount --cgroup /bin/sh -c '
set -eu
mount --make-rprivate /
mount --bind /sys/fs/cgroup "$1"
mount -t cgroup2 none /sys/fs/cgroup
shift
exec "$@"
' registry "$@"
"#;
        let child = must(
            Command::new("/bin/sh")
                .args(["-c", script, "registry"])
                .arg(&self.cgroup)
                .arg(&self.host_mount)
                .arg(&self.binary)
                .env_clear()
                .env("PATH", "/usr/bin:/bin")
                .envs(&self.environment)
                .stdin(Stdio::null())
                .stdout(Stdio::null())
                .stderr(Stdio::from(must(
                    fs::OpenOptions::new()
                        .append(true)
                        .open(&self.process.stderr),
                    "registry restart log",
                )))
                .spawn(),
            "real registry restart",
        );
        self.process = Daemon {
            child,
            stderr: self.process.stderr.clone(),
        };
        wait_for_port(
            self.client.port,
            &mut self.process,
            "restarted hosted registry",
        );
    }
    fn consume(&self, signed: &[u8], interface: &ProgramInterface, label: &str, evidence: &Path) {
        let idempotency = token();
        let mut request = Call::submit(
            "/__registry/deployments",
            &self.bearer,
            &idempotency,
            signed,
        );
        request.identity = Some(&self.identity);
        let response = must(
            self.client.try_call(&request),
            "registry production deployment ingestion",
        );
        assert_eq!(
            response.status,
            200,
            "registry ingestion: {}",
            response.text()
        );
        let activity = must(
            decode_signed(signed, &registry()),
            "registry program activity",
        );
        let program: [u8; 32] = activity.payload()[..32]
            .try_into()
            .unwrap_or_else(|_| panic!("program id"));
        self.read(program, interface, label, evidence);
    }
    fn read(&self, program: [u8; 32], interface: &ProgramInterface, label: &str, evidence: &Path) {
        let path = format!("/v1/programs/registry/{}/interface", hex(&program));
        let mut request = Call::get(&path, Some(&self.bearer));
        request.identity = Some(&self.identity);
        let response = must(
            self.client.try_call(&request),
            "registry production interface read",
        );
        assert_eq!(
            response.status,
            200,
            "registry interface: {}",
            response.text()
        );
        let document = response.json();
        assert_eq!(
            unhex(field(&document, "interface")),
            interface.canonical_encoding()
        );
        assert_eq!(
            field(&document, "interface_digest"),
            hex(interface.digest().as_bytes())
        );
        assert_eq!(field(&document, "code_hash"), hex(&interface.code_hash()));
        assert_eq!(
            document["abi_version"].as_u64(),
            Some(u64::from(interface.abi_version()))
        );
        assert_eq!(
            field(&document, "verification"),
            "deployment-interface-and-current-head-verified"
        );
        write(
            &evidence.join(format!("{label}.json")),
            response.text().as_bytes(),
            0o600,
        );
        println!("NATIVE_INTERFACE_CASE {label}");
    }
}
impl Drop for HostedRegistry {
    fn drop(&mut self) {
        self.process.stop();
        let _ = fs::write(self.cgroup.join("cgroup.kill"), "1");
        let _ = fs::remove_dir(self.cgroup.join("workers"));
        let _ = fs::remove_dir(self.cgroup.join("main"));
        let _ = fs::remove_dir(&self.cgroup);
    }
}

#[test]
fn canonical_native_interfaces_across_supported_abis() {
    let evidence = PathBuf::from(
        std::env::var_os("PAXEER_X_NATIVE_INTERFACE_EVIDENCE")
            .unwrap_or_else(|| panic!("private evidence directory required")),
    );
    assert!(evidence.is_dir());
    assert_eq!(
        must(
            fs::canonicalize(env!("CARGO_BIN_EXE_layerx-agent-boundary")),
            "compiled boundary"
        ),
        must(
            fs::canonicalize(PathBuf::from(
                std::env::var_os("PAXEER_X_NATIVE_INTERFACE_BOUNDARY")
                    .unwrap_or_else(|| panic!("candidate boundary identity required"))
            )),
            "candidate boundary"
        )
    );
    let (mut cluster, _custody) = custody::start_funded_cluster();
    let history = evidence.join("provisioned-trust-history.bin");
    provisioned_history(&cluster, &history);
    let verifier = must(
        ProtocolDeploymentVerifier::from_protected_history(&history, 60_000),
        "provisioned trust",
    );
    let hosted_registry = HostedRegistry::start(&cluster, &history);
    let mut sequence = 2;
    let mut persisted = Vec::new();
    for abi in 1..=4 {
        let prefix = format!("abi{abi}");
        let program = random32();
        let initial = guest(1, 0);
        let original = interface(&initial, abi, 1, 64);
        let signed = submit(
            &cluster,
            &mut sequence,
            1,
            &deploy(
                &cluster,
                program,
                abi,
                &initial,
                &original.canonical_encoding(),
            ),
            0,
            &format!("{prefix}-deploy"),
            &evidence,
        );
        let stored = current_interface(
            &cluster,
            &verifier,
            program,
            &original,
            &evidence,
            &format!("{prefix}-registry"),
        );
        typed_bindings(
            &cluster,
            &hosted_registry,
            &signed,
            &original,
            &stored,
            &history,
            &evidence,
            &format!("{prefix}-bindings"),
        );
        submit(
            &cluster,
            &mut sequence,
            3,
            &call(program, abi, &original),
            0,
            &format!("{prefix}-call"),
            &evidence,
        );
        let narrow = interface(&initial, abi, 1, 32);
        submit(
            &cluster,
            &mut sequence,
            2,
            &upgrade(
                program,
                abi,
                &initial,
                &initial,
                &narrow.canonical_encoding(),
                false,
            ),
            -3,
            &format!("{prefix}-breaking-refused"),
            &evidence,
        );
        assert_eq!(
            current_interface(
                &cluster,
                &verifier,
                program,
                &original,
                &evidence,
                &format!("{prefix}-refusal-preserves-state")
            ),
            stored
        );
        let next = guest(0, 1);
        let compatible = interface(&next, abi, 0, 96);
        let signed = submit(
            &cluster,
            &mut sequence,
            2,
            &upgrade(
                program,
                abi,
                &initial,
                &next,
                &compatible.canonical_encoding(),
                false,
            ),
            0,
            &format!("{prefix}-compatible-upgrade"),
            &evidence,
        );
        let stored = current_interface(
            &cluster,
            &verifier,
            program,
            &compatible,
            &evidence,
            &format!("{prefix}-upgrade-registry"),
        );
        typed_bindings(
            &cluster,
            &hosted_registry,
            &signed,
            &compatible,
            &stored,
            &history,
            &evidence,
            &format!("{prefix}-upgrade-bindings"),
        );
        submit(
            &cluster,
            &mut sequence,
            3,
            &call(program, abi, &compatible),
            0,
            &format!("{prefix}-upgraded-call"),
            &evidence,
        );
        let breaking = guest(1, 2);
        let widened = interface(&breaking, abi, 1, 32);
        submit(
            &cluster,
            &mut sequence,
            2,
            &upgrade(
                program,
                abi,
                &next,
                &breaking,
                &widened.canonical_encoding(),
                false,
            ),
            -3,
            &format!("{prefix}-capability-widening-refused"),
            &evidence,
        );
        let signed = submit(
            &cluster,
            &mut sequence,
            2,
            &upgrade(
                program,
                abi,
                &next,
                &breaking,
                &widened.canonical_encoding(),
                true,
            ),
            0,
            &format!("{prefix}-explicit-breaking-upgrade"),
            &evidence,
        );
        hosted_registry.consume(
            &signed,
            &widened,
            &format!("{prefix}-breaking-registry-process"),
            &evidence,
        );
        submit(
            &cluster,
            &mut sequence,
            3,
            &call(program, abi, &widened),
            0,
            &format!("{prefix}-breaking-call"),
            &evidence,
        );
        let stored = current_interface(
            &cluster,
            &verifier,
            program,
            &widened,
            &evidence,
            &format!("{prefix}-before-restart"),
        );
        persisted.push((program, abi, widened, stored));

        let plain = guest(0, 0);
        let valid = interface(&plain, abi, 0, 64);
        let encoded = valid.canonical_encoding().to_vec();
        let mut wrong_hash = encoded.clone();
        wrong_hash[DOMAIN_BYTES] ^= 1;
        let other_abi = if abi == 4 { 3 } else { abi + 1 };
        let wrong_abi = interface(&plain, other_abi, 0, 64)
            .canonical_encoding()
            .to_vec();
        let mut unknown = encoded.clone();
        unknown[DOMAIN_BYTES + 32..DOMAIN_BYTES + 34].copy_from_slice(&5_u16.to_be_bytes());
        let mut domain = encoded.clone();
        domain[DOMAIN_BYTES - 2] = b'9';
        let mut trailing = encoded.clone();
        trailing.push(0);
        let mut schema = encoded.clone();
        schema[DOMAIN_BYTES + 32 + 2 + 2 + 2 + 4 + 4] = 0xff;
        let mut missing_export = encoded.clone();
        missing_export[DOMAIN_BYTES + 38..DOMAIN_BYTES + 42].copy_from_slice(b"nope");
        let mut result_schema = encoded.clone();
        result_schema[DOMAIN_BYTES + 52] = 0xff;
        let mut overdeclared = interface(&initial, abi, 1, 64)
            .canonical_encoding()
            .to_vec();
        overdeclared[DOMAIN_BYTES..DOMAIN_BYTES + 32].copy_from_slice(&valid.code_hash());
        for (suffix, bytes, result) in [
            ("hash-mismatch", wrong_hash, -213),
            ("abi-mismatch", wrong_abi, -213),
            ("unknown-interface-abi", unknown, -101),
            ("wrong-domain", domain, -3),
            ("trailing-interface", trailing, -3),
            ("invalid-schema", schema, -3),
            ("invalid-result-schema", result_schema, -3),
            ("missing-entrypoint", missing_export, -3),
            ("overdeclared-capability", overdeclared, -3),
        ] {
            submit(
                &cluster,
                &mut sequence,
                1,
                &deploy(&cluster, random32(), abi, &plain, &bytes),
                result,
                &format!("{prefix}-{suffix}"),
                &evidence,
            );
        }
        let mut undeclared = encoded.clone();
        undeclared[DOMAIN_BYTES..DOMAIN_BYTES + 32].copy_from_slice(&original.code_hash());
        submit(
            &cluster,
            &mut sequence,
            1,
            &deploy(&cluster, random32(), abi, &initial, &undeclared),
            -3,
            &format!("{prefix}-undeclared-capability"),
            &evidence,
        );
        for unknown in [0_u16, 5, u16::MAX] {
            let mut payload = deploy(&cluster, random32(), abi, &plain, &encoded);
            payload[32..34].copy_from_slice(&unknown.to_be_bytes());
            submit(
                &cluster,
                &mut sequence,
                1,
                &payload,
                -101,
                &format!("{prefix}-unknown-native-abi-{unknown}"),
                &evidence,
            );
        }
        let mut mismatch = call(
            program,
            other_abi,
            &persisted.last().unwrap_or_else(|| panic!("program")).2,
        );
        submit(
            &cluster,
            &mut sequence,
            3,
            &mismatch,
            -101,
            &format!("{prefix}-call-abi-mismatch"),
            &evidence,
        );
        mismatch[32..34].copy_from_slice(&5_u16.to_be_bytes());
        submit(
            &cluster,
            &mut sequence,
            3,
            &mismatch,
            -101,
            &format!("{prefix}-call-unknown-abi"),
            &evidence,
        );
        {
            let mut forbidden = guest(4, 0);
            let import = forbidden
                .windows(9)
                .position(|bytes| bytes == b"layerx_v4")
                .unwrap_or_else(|| panic!("import module"));
            forbidden[import + 8] = b'9';
            let mut forged = encoded.clone();
            forged[DOMAIN_BYTES..DOMAIN_BYTES + 32].copy_from_slice(&Sha256::digest(&forbidden));
            submit(
                &cluster,
                &mut sequence,
                1,
                &deploy(&cluster, random32(), abi, &forbidden, &forged),
                -3,
                &format!("{prefix}-unsupported-import"),
                &evidence,
            );
        }
        if abi >= 2 {
            let dynamic_wasm = guest(2, 0);
            let dynamic = interface(&dynamic_wasm, abi, 2, 64);
            let program = random32();
            let signed = submit(
                &cluster,
                &mut sequence,
                1,
                &deploy(
                    &cluster,
                    program,
                    abi,
                    &dynamic_wasm,
                    &dynamic.canonical_encoding(),
                ),
                0,
                &format!("{prefix}-dynamic-deploy"),
                &evidence,
            );
            let stored = current_interface(
                &cluster,
                &verifier,
                program,
                &dynamic,
                &evidence,
                &format!("{prefix}-dynamic-registry"),
            );
            typed_bindings(
                &cluster,
                &hosted_registry,
                &signed,
                &dynamic,
                &stored,
                &history,
                &evidence,
                &format!("{prefix}-dynamic-bindings"),
            );
            let capability = DOMAIN_BYTES + 32 + 2 + 2 + 2 + 4 + 4 + 6 + 6 + 2;
            assert_eq!(dynamic.canonical_encoding()[capability], 10);
            for (suffix, start, bytes) in [
                ("zero-ceiling", capability + 33, vec![0; 16]),
                (
                    "offset-overflow",
                    capability + 49,
                    u32::MAX.to_be_bytes().to_vec(),
                ),
                (
                    "amount-offset-overflow",
                    capability + 53,
                    u32::MAX.to_be_bytes().to_vec(),
                ),
            ] {
                let mut malformed = dynamic.canonical_encoding().to_vec();
                malformed[start..start + bytes.len()].copy_from_slice(&bytes);
                submit(
                    &cluster,
                    &mut sequence,
                    1,
                    &deploy(&cluster, random32(), abi, &dynamic_wasm, &malformed),
                    -3,
                    &format!("{prefix}-dynamic-{suffix}"),
                    &evidence,
                );
            }
        }
    }
    for (abi, capability, label) in [(3, 3, "oracle-v3"), (4, 3, "oracle-v4"), (4, 4, "web-v4")] {
        let wasm = guest(capability, 0);
        let interface = interface(&wasm, abi, capability, 64);
        let program = random32();
        let signed = submit(
            &cluster,
            &mut sequence,
            1,
            &deploy(
                &cluster,
                program,
                abi,
                &wasm,
                interface.canonical_encoding(),
            ),
            0,
            &format!("{label}-deploy"),
            &evidence,
        );
        let stored = current_interface(
            &cluster,
            &verifier,
            program,
            &interface,
            &evidence,
            &format!("{label}-registry"),
        );
        typed_bindings(
            &cluster,
            &hosted_registry,
            &signed,
            &interface,
            &stored,
            &history,
            &evidence,
            &format!("{label}-bindings"),
        );
    }
    let immutable = random32();
    let immutable_wasm = guest(0, 0);
    let immutable_interface = interface(&immutable_wasm, 4, 0, 64);
    let payload = must(
        NativeProgramDeploy {
            program_id: ProgramId::new(immutable),
            guest_abi: 4,
            policy: ProgramUpgradePolicy::Immutable,
            new_hash: Sha256::digest(&immutable_wasm).into(),
            interface: Some(immutable_interface.canonical_encoding()),
            wasm: &immutable_wasm,
        }
        .encode(),
        "immutable deploy",
    );
    submit(
        &cluster,
        &mut sequence,
        1,
        &payload,
        0,
        "immutable-deploy",
        &evidence,
    );
    let changed_wasm = guest(0, 1);
    let changed_interface = interface(&changed_wasm, 4, 0, 32);
    submit(
        &cluster,
        &mut sequence,
        2,
        &upgrade(
            immutable,
            4,
            &immutable_wasm,
            &changed_wasm,
            changed_interface.canonical_encoding(),
            true,
        ),
        -204,
        "breaking-cannot-bypass-authority",
        &evidence,
    );
    current_interface(
        &cluster,
        &verifier,
        immutable,
        &immutable_interface,
        &evidence,
        "immutable-refusal-preserves-state",
    );
    let program = random32();
    let wasm = guest(0, 0);
    let first = interface(&wasm, 1, 0, 64);
    submit(
        &cluster,
        &mut sequence,
        1,
        &deploy(&cluster, program, 1, &wasm, &first.canonical_encoding()),
        0,
        "monotonic-deploy",
        &evidence,
    );
    for abi in 2..=4 {
        let next = interface(&wasm, abi, 0, 64);
        submit(
            &cluster,
            &mut sequence,
            2,
            &upgrade(
                program,
                abi,
                &wasm,
                &wasm,
                &next.canonical_encoding(),
                false,
            ),
            0,
            &format!("monotonic-upgrade-{abi}"),
            &evidence,
        );
        submit(
            &cluster,
            &mut sequence,
            3,
            &call(program, abi, &next),
            0,
            &format!("monotonic-call-{abi}"),
            &evidence,
        );
    }
    submit(
        &cluster,
        &mut sequence,
        2,
        &upgrade(program, 1, &wasm, &wasm, &first.canonical_encoding(), true),
        -101,
        "breaking-cannot-downgrade-abi",
        &evidence,
    );
    cluster.restart_native_sequencer();
    check_readiness(&cluster);
    for (program, abi, interface, stored) in persisted {
        hosted_registry.read(
            program,
            &interface,
            &format!("abi{abi}-restart-registry-process"),
            &evidence,
        );
        assert_eq!(
            current_interface(
                &cluster,
                &verifier,
                program,
                &interface,
                &evidence,
                &format!("abi{abi}-after-restart")
            ),
            stored
        );
        submit(
            &cluster,
            &mut sequence,
            3,
            &call(program, abi, &interface),
            0,
            &format!("abi{abi}-restart-call"),
            &evidence,
        );
    }
}

fn typed_echo_guest(abi: u16, distinct: u8) -> Vec<u8> {
    let mut imports = Vec::new();
    if abi >= 2 {
        imports.push(("layerx_v2", "response_write", 0));
    }
    if abi == 3 {
        imports.push(("layerx_v3", "oracle_read", 1));
    }
    if abi == 4 {
        imports.push(("layerx_v4", "web_read", 1));
    }
    let n = imports.len() as u64;
    let mut exports = unsigned_leb(3);
    for (name, kind, index) in [
        ("layerx_reserve", 0_u8, n),
        ("call", 0, n + 1),
        ("memory", 2, 0),
    ] {
        exports.extend(unsigned_leb(name.len() as u64));
        exports.extend(name.as_bytes());
        exports.push(kind);
        exports.extend(unsigned_leb(index));
    }
    let mut body = Vec::new();
    for _ in 0..distinct {
        body.extend([OP_I32_CONST, 0, OP_DROP]);
    }
    if abi >= 3 {
        body.extend([OP_I32_CONST, 0, 0x04, 0x40]);
        for _ in 0..4 {
            body.extend([OP_I32_CONST, 0]);
        }
        body.extend([OP_CALL, 1, OP_DROP, OP_END]);
    }
    if abi >= 2 {
        body.extend([
            OP_I32_CONST,
            0,
            0x20,
            0,
            OP_I32_CONST,
            4,
            0x6a,
            0x20,
            1,
            OP_I32_CONST,
            4,
            0x6b,
            OP_CALL,
            0,
            OP_DROP,
        ]);
    }
    body.extend([OP_I32_CONST, 0, OP_END]);
    module(&[
        type_section(&[
            (&[TYPE_I32; 3], &[TYPE_I32]),
            (&[TYPE_I32; 4], &[TYPE_I32]),
            (&[TYPE_I32], &[TYPE_I32]),
            (&[TYPE_I32; 2], &[TYPE_I32]),
        ]),
        import_section(&imports),
        function_section(&[2, 3]),
        raw_section(5, &[1, 1, 1, 1]),
        raw_section(7, &exports),
        code_section(&[
            func_body(&[], &[OP_I32_CONST, 0, OP_END]),
            func_body(&[], &body),
        ]),
    ])
}

fn typed_echo_interface(wasm: &[u8], abi: u16, capability: u8, bound: u32) -> ProgramInterface {
    let original = interface(wasm, abi, capability, bound);
    let mut entries = original.entries().to_vec();
    entries[0].failures.push(layerx_programs::TypedFailure {
        code: 7,
        name: "denied".into(),
        detail: ValueSchema::layerx(ValueType::U8),
    });
    must(
        ProgramInterface::bind(wasm, abi, entries),
        "typed echo interface",
    )
}

#[test]
fn emit_typed_interface_inputs() {
    let evidence = PathBuf::from(
        std::env::var_os("PAXEER_X_TYPED_INTERFACE_INPUTS")
            .unwrap_or_else(|| panic!("private typed-interface input directory required")),
    );
    assert!(evidence.is_dir());
    for abi in 1..=4 {
        let directory = evidence.join(format!("abi{abi}"));
        make_dir(&directory, 0o700);
        write(
            &directory.join("LayerX.toml"),
            format!("abi_version = {abi}\n").as_bytes(),
            0o600,
        );
        let capability = if abi == 3 {
            3
        } else if abi == 4 {
            4
        } else {
            0
        };
        let wasm = typed_echo_guest(abi, 0);
        let published = typed_echo_interface(&wasm, abi, capability, 64);
        write(&directory.join("module.wasm"), &wasm, 0o600);
        write(
            &directory.join("interface.bin"),
            published.canonical_encoding(),
            0o600,
        );
        let upgraded = typed_echo_guest(abi, 1);
        let wider = typed_echo_interface(&upgraded, abi, capability, 128);
        let narrower = typed_echo_interface(&upgraded, abi, capability, 32);
        write(&directory.join("upgrade.wasm"), &upgraded, 0o600);
        write(
            &directory.join("upgrade.bin"),
            wider.canonical_encoding(),
            0o600,
        );
        write(
            &directory.join("narrow.bin"),
            narrower.canonical_encoding(),
            0o600,
        );
        for (name, offset, replacement) in [
            ("wrong-hash", DOMAIN_BYTES, 0_u8),
            ("unknown-abi", DOMAIN_BYTES + 33, 5_u8),
            (
                "wrong-schema",
                DOMAIN_BYTES + 2 + 32 + 2 + 2 + 4 + 4,
                0xff_u8,
            ),
        ] {
            let mut bytes = published.canonical_encoding().to_vec();
            if name == "wrong-hash" {
                bytes[offset] ^= 1;
            } else {
                bytes[offset] = replacement;
            }
            write(&directory.join(format!("{name}.bin")), &bytes, 0o600);
        }
        let other = guest(if capability == 0 { 1 } else { 0 }, 0);
        let mut undeclared = interface(&other, abi, if capability == 0 { 1 } else { 0 }, 64)
            .canonical_encoding()
            .to_vec();
        undeclared[DOMAIN_BYTES..DOMAIN_BYTES + 32].copy_from_slice(&published.code_hash());
        write(&directory.join("wrong-capability.bin"), &undeclared, 0o600);
        println!("TYPED_INTERFACE_INPUT abi{abi}");
    }
}

fn discovery_interface(
    cluster: &Cluster,
    verifier: &ProtocolDeploymentVerifier,
    program: [u8; 32],
    expected: &ProgramInterface,
    evidence: &Path,
    label: &str,
) -> Vec<u8> {
    let head = http_get(
        cluster.program_port,
        "/v1/protocol/account-state/head",
        &cluster.program_token,
    );
    assert_eq!(head.status, 200, "native current head unavailable");
    let head = head.json();
    let sequence = head["observed_sequence"]
        .as_u64()
        .unwrap_or_else(|| panic!("head sequence"));
    let response = http_get(
        cluster.program_port,
        &format!("/v1/programs/{}/state-proof?at={sequence}", hex(&program)),
        &cluster.program_token,
    );
    assert_eq!(response.status, 200, "native program state unavailable");
    let document = response.json();
    let bytes = unhex(field(&document, "record_hex"));
    let bundle = must(
        ProgramStateBundle::decode(&bytes),
        "native current state bundle",
    );
    assert_eq!(bundle.canonical_encoding(), bytes);
    let state = &bundle.state;
    let chain = must(
        verifier.verify_current_chain_head(
            bundle.head_kind,
            &state.receipt,
            &state.receipt_proof,
            &state.header,
            &state.header_signature,
            now_ms(),
        ),
        "current chain proof",
    );
    assert_eq!(
        chain.state_root(),
        <[u8; 32]>::try_from(unhex(field(&head, "state_root")))
            .unwrap_or_else(|_| panic!("head root"))
    );
    assert_eq!(chain.global_sequence(), sequence);
    let program = must(
        layerx_programs_runtime::ProgramId::new(program),
        "runtime program",
    );
    let verified = must(
        verifier.verify_current_program_bundle(
            &bundle,
            &chain,
            program,
            &cluster.sequencer_key,
            now_ms(),
        ),
        "registry authenticated stored interface",
    );
    assert_eq!(&verified.interface().interface, expected);
    assert_eq!(
        verified.program_head().abi_version(),
        expected.abi_version()
    );
    assert_eq!(verified.program_head().code_hash(), expected.code_hash());
    let mut damaged = bundle.clone();
    damaged.interface.value[0] ^= 1;
    assert!(verifier
        .verify_current_program_bundle(&damaged, &chain, program, &cluster.sequencer_key, now_ms())
        .is_err());
    let mut damaged = bundle.clone();
    damaged.state.header_signature[0] ^= 1;
    assert!(verifier
        .verify_current_program_bundle(&damaged, &chain, program, &cluster.sequencer_key, now_ms())
        .is_err());
    write(&evidence.join(format!("{label}.state")), &bytes, 0o600);
    println!("NATIVE_INTERFACE_CASE {label}");
    bundle.interface.value
}

fn discovery_mark(abi: u16, case: &str) {
    println!("REGISTRY_DISCOVERY_CASE abi{abi}-{case}");
}

fn discovery_proof_refusals(
    cluster: &Cluster,
    verifier: &ProtocolDeploymentVerifier,
    program: [u8; 32],
    abi: u16,
    path: &Path,
) {
    let bundle = must(
        ProgramStateBundle::decode(&must(fs::read(path), "native capture")),
        "native proof",
    );
    let at = now_ms();
    let chain = must(
        verifier.verify_current_chain_head(
            bundle.head_kind,
            &bundle.state.receipt,
            &bundle.state.receipt_proof,
            &bundle.state.header,
            &bundle.state.header_signature,
            at,
        ),
        "captured live head",
    );
    let program = must(
        layerx_programs_runtime::ProgramId::new(program),
        "program identity",
    );
    for case in [
        "bad-record",
        "bad-interface",
        "bad-membership",
        "unknown-abi",
    ] {
        let mut damaged = bundle.clone();
        match case {
            "bad-record" => damaged.state.program_record.value[33] ^= 1,
            "bad-interface" => damaged.interface.value[36] ^= 1,
            "bad-membership" => damaged.state.program_record.proof.leaf_index ^= 1,
            "unknown-abi" => {
                damaged.state.program_record.value[65..67].copy_from_slice(&5_u16.to_be_bytes())
            }
            _ => unreachable!(),
        }
        assert!(
            verifier
                .verify_current_program_bundle(
                    &damaged,
                    &chain,
                    program,
                    &cluster.sequencer_key,
                    at,
                )
                .is_err(),
            "{case} became discoverable"
        );
        discovery_mark(abi, case);
    }
    let mut signer = cluster.sequencer_key;
    signer[0] ^= 1;
    assert!(verifier
        .verify_current_program_bundle(&bundle, &chain, program, &signer, at)
        .is_err());
    discovery_mark(abi, "foreign-signer");
    assert!(verifier
        .verify_current_program_bundle(
            &bundle,
            &chain,
            program,
            &cluster.sequencer_key,
            at.checked_add(60_001)
                .unwrap_or_else(|| panic!("time overflow")),
        )
        .is_err());
    discovery_mark(abi, "stale-head");
}

fn discovery_registry_document(registry: &HostedRegistry, program: [u8; 32]) -> serde_json::Value {
    let path = format!("/v1/programs/registry/{}", hex(&program));
    let mut request = Call::get(&path, Some(&registry.bearer));
    request.identity = Some(&registry.identity);
    let response = must(registry.client.try_call(&request), "live hosted discovery");
    assert_eq!(
        response.status,
        200,
        "hosted discovery: {}",
        response.text()
    );
    response.json()
}

struct DiscoveryEmulator {
    process: Daemon,
    port: u16,
    sequence: u64,
    binary: PathBuf,
    arguments: Vec<String>,
}
impl DiscoveryEmulator {
    fn start(cluster: &Cluster, evidence: &Path) -> Self {
        let binary = PathBuf::from(
            std::env::var_os("PAXEER_X_REGISTRY_EMULATOR_BINARY")
                .unwrap_or_else(|| panic!("candidate CLI emulator required")),
        );
        let seed = evidence.join("emulator-sequencer.seed");
        write(&seed, &random32(), 0o600);
        let port = free_port();
        let stderr = evidence.join("emulator.stderr");
        let arguments = vec![
            "emulator".to_owned(),
            "up".to_owned(),
            "--listen".to_owned(),
            format!("127.0.0.1:{port}"),
            "--network-id".to_owned(),
            NETWORK_ID.to_string(),
            "--protocol-version".to_owned(),
            PROTOCOL_VERSION.to_string(),
            "--time-ms".to_owned(),
            now_ms().to_string(),
            "--sequencer-seed-file".to_owned(),
            text(&seed),
            "--prefund".to_owned(),
            format!(
                "{},{},1000000000000000",
                cluster.actor.did,
                hex(&cluster.actor.signing_key.verifying_key().to_bytes())
            ),
        ];
        let child = must(
            Command::new(&binary)
                .args(&arguments)
                .stdin(Stdio::null())
                .stdout(Stdio::null())
                .stderr(Stdio::from(must(fs::File::create(&stderr), "emulator log")))
                .spawn(),
            "real CLI emulator process",
        );
        let mut process = Daemon { child, stderr };
        wait_for_port(port, &mut process, "CLI emulator");
        let mut emulator = Self {
            process,
            port,
            sequence: 0,
            binary,
            arguments,
        };
        let response = emulator.request(
            "GET",
            &format!("/v1/dids/{}/sequence", cluster.actor.did),
            &[],
        );
        assert_eq!(response.status, 200);
        emulator.sequence = response.json()["result"]["next_sequence"]
            .as_str()
            .and_then(|v| v.parse().ok())
            .unwrap_or_else(|| panic!("real emulator sequence"));
        emulator
    }
    fn request(&self, method: &str, path: &str, body: &[u8]) -> HttpAnswer {
        let mut stream = must(
            TcpStream::connect(("127.0.0.1", self.port)),
            "emulator connect",
        );
        must(
            stream.set_read_timeout(Some(Duration::from_secs(60))),
            "emulator timeout",
        );
        let media_type = if method == "GET" && !body.is_empty() {
            "application/json"
        } else if path == "/__emulator/snapshot" {
            "application/vnd.layerx.emulator-snapshot"
        } else {
            "application/octet-stream"
        };
        must(stream.write_all(format!("{method} {path} HTTP/1.1\r\nHost: localhost\r\nContent-Type: {media_type}\r\nContent-Length: {}\r\nConnection: close\r\n\r\n", body.len()).as_bytes()), "emulator request");
        must(stream.write_all(body), "emulator activity");
        let mut bytes = Vec::new();
        must(stream.read_to_end(&mut bytes), "emulator response");
        parse_http(&bytes)
    }
    fn submit(&mut self, cluster: &Cluster, ordinal: u16, payload: &[u8], route: &str) {
        let signed =
            signed_program_operation(&cluster.actor, ordinal, self.sequence, FEE_LIMIT, payload);
        let response = self.request("POST", route, &signed);
        assert_eq!(
            response.status,
            200,
            "emulator native activity: {}",
            response.text()
        );
        assert_eq!(
            response.json()["value"]["state"],
            "completed",
            "emulator refused a positive native lifecycle: {}",
            response.text()
        );
        self.sequence = self
            .sequence
            .checked_add(1)
            .unwrap_or_else(|| panic!("sequence overflow"));
    }
    fn read(&self, program: [u8; 32]) -> serde_json::Value {
        let selector = serde_json::json!({"program_id":hex(&program),
            "requested_verification_level":"sequencer-signed"})
        .to_string();
        let response = self.request(
            "GET",
            &format!("/v1/programs/registry/{}", hex(&program)),
            selector.as_bytes(),
        );
        assert_eq!(
            response.status,
            200,
            "emulator discovery: {}",
            response.text()
        );
        response.json()["value"].clone()
    }
    fn interface(&self, program: [u8; 32], expected: &ProgramInterface) {
        let selector = serde_json::json!({"program_id":hex(&program),
            "requested_verification_level":"sequencer-signed"})
        .to_string();
        let response = self.request(
            "GET",
            &format!("/v1/programs/registry/{}/interface", hex(&program)),
            selector.as_bytes(),
        );
        assert_eq!(
            response.status,
            200,
            "emulator interface: {}",
            response.text()
        );
        let document = response.json();
        assert_eq!(
            unhex(field(&document["value"], "interface")),
            expected.canonical_encoding()
        );
        assert_eq!(document["value"]["code_hash"], hex(&expected.code_hash()));
        assert_eq!(
            document["value"]["abi_version"].as_u64(),
            Some(u64::from(expected.abi_version()))
        );
    }
    fn restore(&mut self) {
        let snapshot = self.request("GET", "/__emulator/snapshot", &[]);
        assert_eq!(snapshot.status, 200);
        self.process.stop();
        let child = must(
            Command::new(&self.binary)
                .args(&self.arguments)
                .stdin(Stdio::null())
                .stdout(Stdio::null())
                .stderr(Stdio::from(must(
                    fs::OpenOptions::new()
                        .append(true)
                        .open(&self.process.stderr),
                    "emulator restart log",
                )))
                .spawn(),
            "real emulator restart",
        );
        self.process = Daemon {
            child,
            stderr: self.process.stderr.clone(),
        };
        wait_for_port(self.port, &mut self.process, "restarted CLI emulator");
        let imported = self.request("PUT", "/__emulator/snapshot", &snapshot.body);
        assert_eq!(
            imported.status,
            200,
            "emulator snapshot import: {}",
            imported.text()
        );
    }
}
impl Drop for DiscoveryEmulator {
    fn drop(&mut self) {
        self.process.stop();
    }
}

fn discovery_attestation(
    document: &serde_json::Value,
    program: [u8; 32],
    interface: &ProgramInterface,
    version: u64,
    signer: [u8; 32],
) {
    use layerx_client::lni::head_attestation::{
        program_discovery_proof_digest, ProgramDiscoveryHead,
    };
    assert_eq!(unhex(field(document, "discovery_public_key")), signer);
    let head = ProgramDiscoveryHead {
        program_id: program,
        version: must(u32::try_from(version), "version"),
        code_hash: interface.code_hash(),
        abi_version: interface.abi_version(),
        observed_sequence: document["observed_sequence"]
            .as_u64()
            .unwrap_or_else(|| panic!("head sequence")),
        observed_at: document["observed_at"]
            .as_u64()
            .unwrap_or_else(|| panic!("head timestamp")),
        valid_through: document["valid_through"]
            .as_u64()
            .unwrap_or_else(|| panic!("head bound")),
        state_root: must(unhex(field(document, "state_root")).try_into(), "head root"),
    };
    let digest = program_discovery_proof_digest(&head);
    assert_eq!(unhex(field(document, "receipt_digest")), digest);
    let signature: [u8; 64] = must(
        unhex(field(document, "discovery_signature")).try_into(),
        "signature",
    );
    must(
        must(
            ed25519_dalek::VerifyingKey::from_bytes(&signer),
            "sequencer key",
        )
        .verify_strict(&digest, &ed25519_dalek::Signature::from_bytes(&signature)),
        "exact remote head signature",
    );
}

fn discovery_compare(
    hosted: &serde_json::Value,
    emulator: &serde_json::Value,
    program: [u8; 32],
    interface: &ProgramInterface,
    version: u64,
    signer: [u8; 32],
) {
    discovery_attestation(hosted, program, interface, version, signer);
    assert_eq!(hosted["program_id"], hex(&program));
    assert_eq!(hosted["program_id"], emulator["program_id"]);
    assert_eq!(hosted["lifecycle"], emulator["lifecycle"]);
    assert_eq!(hosted["latest_version"].as_u64(), Some(version));
    assert_eq!(emulator["version"].as_u64(), Some(version));
    let latest = hosted["versions"]
        .as_array()
        .unwrap_or_else(|| panic!("version history"))
        .last()
        .unwrap_or_else(|| panic!("latest version"));
    assert_eq!(latest["code_hash"], hex(&interface.code_hash()));
    assert_eq!(latest["code_hash"], emulator["code_hash"]);
    assert_eq!(
        latest["abi_version"].as_u64(),
        Some(u64::from(interface.abi_version()))
    );
    assert_eq!(latest["abi_version"], emulator["abi_version"]);
    assert_eq!(
        hosted["value_accounts"]["accounts"],
        emulator["value_accounts"]["accounts"]
    );
    if interface.abi_version() == 1 {
        assert_eq!(hosted["value_accounts"]["status"], "account-incapable-abi1");
        assert_eq!(
            emulator["value_accounts"]["status"],
            "account-incapable-abi1"
        );
    } else {
        assert_eq!(hosted["value_accounts"]["status"], "current");
        assert_eq!(emulator["value_accounts"]["status"], "current");
        if interface.abi_version() > 2 {
            assert_eq!(
                hosted["value_accounts"]["account_profile"],
                emulator["value_accounts"]["account_profile"]
            );
        }
    }
}

#[test]
fn guest_abi_discovery() {
    let evidence = PathBuf::from(
        std::env::var_os("PAXEER_X_NATIVE_INTERFACE_EVIDENCE")
            .unwrap_or_else(|| panic!("private discovery evidence required")),
    );
    assert!(evidence.is_dir());
    assert_eq!(
        must(
            fs::canonicalize(env!("CARGO_BIN_EXE_layerx-agent-boundary")),
            "compiled boundary"
        ),
        must(
            fs::canonicalize(PathBuf::from(
                std::env::var_os("PAXEER_X_NATIVE_INTERFACE_BOUNDARY")
                    .unwrap_or_else(|| panic!("candidate boundary identity"))
            )),
            "pinned boundary"
        )
    );
    let (mut cluster, _custody) = custody::start_funded_cluster();
    let history = evidence.join("provisioned-trust-history.bin");
    provisioned_history(&cluster, &history);
    let verifier = must(
        ProtocolDeploymentVerifier::from_protected_history(&history, 60_000),
        "trust owner",
    );
    let mut hosted = HostedRegistry::start(&cluster, &history);
    let mut emulator = DiscoveryEmulator::start(&cluster, &evidence);
    let mut sequence = 2;
    let mut retained = Vec::new();
    for abi in 1..=4 {
        let program = random32();
        let initial = guest(0, 0);
        let original = interface(&initial, abi, 0, 64);
        let payload = deploy(
            &cluster,
            program,
            abi,
            &initial,
            &original.canonical_encoding(),
        );
        let signed = submit(
            &cluster,
            &mut sequence,
            1,
            &payload,
            0,
            &format!("abi{abi}-deploy"),
            &evidence,
        );
        emulator.submit(&cluster, 1, &payload, "/v1/programs/deploy");
        hosted.consume(
            &signed,
            &original,
            &format!("abi{abi}-deployed-interface"),
            &evidence,
        );
        discovery_compare(
            &discovery_registry_document(&hosted, program),
            &emulator.read(program),
            program,
            &original,
            1,
            cluster.sequencer_key,
        );
        discovery_mark(abi, "deploy");
        let mut unknown = deploy(
            &cluster,
            random32(),
            abi,
            &initial,
            &original.canonical_encoding(),
        );
        unknown[32..34].copy_from_slice(&5_u16.to_be_bytes());
        submit(
            &cluster,
            &mut sequence,
            1,
            &unknown,
            -101,
            &format!("abi{abi}-unknown-refused"),
            &evidence,
        );
        let changed = guest(0, 1);
        let next = interface(&changed, abi, 0, 96);
        let payload = upgrade(
            program,
            abi,
            &initial,
            &changed,
            &next.canonical_encoding(),
            false,
        );
        let signed = submit(
            &cluster,
            &mut sequence,
            2,
            &payload,
            0,
            &format!("abi{abi}-upgrade"),
            &evidence,
        );
        emulator.submit(&cluster, 2, &payload, "/v1/programs/upgrade");
        hosted.consume(
            &signed,
            &next,
            &format!("abi{abi}-upgraded-interface"),
            &evidence,
        );
        discovery_mark(abi, "upgrade");
        let stored = discovery_interface(
            &cluster,
            &verifier,
            program,
            &next,
            &evidence,
            &format!("abi{abi}-before-restart"),
        );
        emulator.interface(program, &next);
        discovery_mark(abi, "interface");
        let document = discovery_registry_document(&hosted, program);
        discovery_compare(
            &document,
            &emulator.read(program),
            program,
            &next,
            2,
            cluster.sequencer_key,
        );
        discovery_mark(abi, "discovery");
        discovery_mark(abi, "account-state");
        discovery_proof_refusals(
            &cluster,
            &verifier,
            program,
            abi,
            &evidence.join(format!("abi{abi}-before-restart.state")),
        );
        let mut invalid = upgrade(
            program,
            abi,
            &changed,
            &changed,
            &next.canonical_encoding(),
            true,
        );
        invalid[32..34].copy_from_slice(&(abi - 1).to_be_bytes());
        submit(
            &cluster,
            &mut sequence,
            2,
            &invalid,
            -101,
            &format!("abi{abi}-downgrade-refused"),
            &evidence,
        );
        assert_eq!(
            discovery_interface(
                &cluster,
                &verifier,
                program,
                &next,
                &evidence,
                &format!("abi{abi}-after-refusal")
            ),
            stored
        );
        discovery_mark(abi, "downgrade");
        if abi != 1 {
            let payload = must(
                NativeProgramWindDown {
                    program_id: ProgramId::new(program),
                    operation: ProgramWindDownOperation::Deprecate {
                        exit_program: program,
                        deadline_batch: LAST_BATCH,
                    },
                }
                .encode(),
                "native wind-down",
            );
            submit(
                &cluster,
                &mut sequence,
                7,
                &payload,
                0,
                &format!("abi{abi}-deprecate"),
                &evidence,
            );
            emulator.submit(&cluster, 7, &payload, "/v1/programs/wind-down");
            let deprecated = discovery_registry_document(&hosted, program);
            discovery_compare(
                &deprecated,
                &emulator.read(program),
                program,
                &next,
                2,
                cluster.sequencer_key,
            );
            assert_eq!(deprecated["lifecycle"], "deprecated");
            assert!(!deprecated["lifecycle_history"]
                .as_array()
                .unwrap_or_else(|| panic!("lifecycle history"))
                .is_empty());
            discovery_interface(
                &cluster,
                &verifier,
                program,
                &next,
                &evidence,
                &format!("abi{abi}-before-restart"),
            );
            retained.push((program, abi, next, deprecated, stored));
        } else {
            retained.push((program, abi, next, document, stored));
        }
    }
    cluster.restart_native_sequencer();
    check_readiness(&cluster);
    emulator.restore();
    hosted.restart();
    for (program, abi, interface, before, stored) in retained {
        let current = discovery_registry_document(&hosted, program);
        assert_eq!(current["versions"], before["versions"]);
        assert_eq!(current["lifecycle_history"], before["lifecycle_history"]);
        assert_eq!(
            current["value_accounts"]["accounts"],
            before["value_accounts"]["accounts"]
        );
        assert_eq!(
            discovery_interface(
                &cluster,
                &verifier,
                program,
                &interface,
                &evidence,
                &format!("abi{abi}-after-restart")
            ),
            stored
        );
        discovery_compare(
            &current,
            &emulator.read(program),
            program,
            &interface,
            2,
            cluster.sequencer_key,
        );
        emulator.interface(program, &interface);
        discovery_mark(abi, "restart");
    }
    println!("REGISTRY_DISCOVERY_CASE remote-attestation");
}
