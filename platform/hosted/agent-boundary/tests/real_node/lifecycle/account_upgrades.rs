use super::*;
use layerx_agentd::read::{LayerxdProgramBalanceReader, ProgramAuthority};
use layerx_programs::{AccountStateHead, DeploymentProof, ProtocolDeploymentVerifier, Registry};
use layerx_programs_protocol_adapter::ProtocolProgramStateRead;
use layerx_types::program_lifecycle::NativeProgramAccountProfile2;

fn evidence_directory() -> PathBuf {
    let path = PathBuf::from(
        std::env::var_os("PAXEER_X_PROFILE2_ACCOUNTS_EVIDENCE")
            .unwrap_or_else(|| panic!("PAXEER_X_PROFILE2_ACCOUNTS_EVIDENCE is required")),
    );
    let metadata = must(
        fs::symlink_metadata(&path),
        "private profile2 evidence directory",
    );
    assert!(path.is_absolute() && metadata.is_dir() && !metadata.file_type().is_symlink());
    assert_eq!(metadata.permissions().mode() & 0o777, 0o700);
    path
}

fn marker(name: &str) {
    println!("PROFILE2_ACCOUNT_CASE {name}");
}

fn registration(program: [u8; 32], asset: [u8; 32], seed: &[u8]) -> Vec<u8> {
    must(
        NativeProgramAccountProfile2 {
            program_id: ProgramId::new(program),
            asset,
            seed,
        }
        .encode(),
        "profile2 registration",
    )
}

fn legacy_registration(program: [u8; 32], asset: [u8; 32], seed: &[u8]) -> Vec<u8> {
    let mut payload = program.to_vec();
    payload.extend_from_slice(b"LXPA1");
    payload.extend_from_slice(&asset);
    payload
        .extend_from_slice(&must(u32::try_from(seed.len()), "account seed length").to_be_bytes());
    payload.extend_from_slice(seed);
    payload
}

fn seeded_account(program: [u8; 32], seed: &[u8]) -> [u8; 32] {
    must(
        derive_program_account(must(RuntimeProgramId::new(program), "program"), seed),
        "profile2 derived account",
    )
    .bytes()
}

fn call_payload(
    cluster: &Cluster,
    program: [u8; 32],
    abi: u16,
    seed: &[u8],
    capabilities: bool,
) -> Vec<u8> {
    let account = seeded_account(program, seed);
    let capability_bytes = if capabilities {
        must(
            CapabilitySet::new([
                Capability::EmitEvent,
                Capability::Transfer402 {
                    asset: cluster.asset,
                    to: account,
                    maximum_amount: DEPOSIT,
                },
                Capability::SharedStorageRead,
                Capability::SharedStorageWrite,
            ]),
            "seed-bound transfer capabilities",
        )
        .canonical_encoding()
    } else {
        must(
            CapabilitySet::new([
                Capability::EmitEvent,
                Capability::SharedStorageRead,
                Capability::SharedStorageWrite,
            ]),
            "transfer capability omission",
        )
        .canonical_encoding()
    };
    let access = escrow_access(cluster, program, account);
    let mut calldata = vec![1, 1];
    calldata.extend_from_slice(&must(u16::try_from(seed.len()), "seed length").to_be_bytes());
    calldata.extend_from_slice(seed);
    for identifier in [
        account,
        cluster.asset,
        cluster.actor.source,
        cluster.actor.source,
    ] {
        calldata.extend_from_slice(&identifier);
    }
    calldata.extend_from_slice(&DEPOSIT.to_be_bytes());
    calldata.extend_from_slice(&[0x41; 32]);
    calldata.extend_from_slice(&[0x42; 32]);
    must(
        NativeProgramCall {
            program_id: ProgramId::new(program),
            guest_abi: abi,
            entrypoint: b"layerx_call",
            calldata: &calldata,
            capabilities: &capability_bytes,
            access_declaration: &access,
            response_capacity: 1024,
            resources: escrow_resources(),
        }
        .encode(),
        "upgraded escrow call",
    )
}

fn upgrade(program: [u8; 32], old: &[u8], new: &[u8], abi: u16) -> Vec<u8> {
    must(
        NativeProgramUpgrade {
            program_id: ProgramId::new(program),
            guest_abi: abi,
            old_hash: Sha256::digest(old).into(),
            new_hash: Sha256::digest(new).into(),
            migration_hook: &[],
            clear_interface: false,
            interface: None,
            wasm: new,
        }
        .encode(),
        "profile2 upgrade",
    )
}

fn submit(
    cluster: &Cluster,
    ordinal: u16,
    path: &str,
    sequence: &mut u64,
    payload: &[u8],
    label: &str,
    evidence: &Path,
    expect_success: bool,
) -> Submitted {
    let signed = signed_program_operation(&cluster.actor, ordinal, *sequence, FEE_LIMIT, payload);
    *sequence += 1;
    let key = format!("profile2-{label}-{}", token());
    let deadline = Instant::now() + Duration::from_secs(60);
    let response = loop {
        let response =
            cluster
                .client
                .call(&Call::submit(path, &cluster.gateway_token, &key, &signed));
        if response.status != 202 || Instant::now() >= deadline {
            break response;
        }
        thread::sleep(Duration::from_millis(100));
    };
    assert_eq!(response.status, 200, "{label}: {}", response.text());
    let document = response.json();
    let result = &document["result"];
    let bytes = unhex(field(result, "receipt"));
    let receipt = must(
        verify_sequencer_signature(&bytes, cluster.sequencer_key),
        "profile2 receipt signature",
    );
    let protocol = receipt
        .protocol()
        .unwrap_or_else(|| panic!("protocol receipt required"));
    let activity = must(
        decode_signed(&signed, &program_registry()),
        "profile2 signed activity",
    );
    assert_eq!(
        protocol.activity_id(),
        must(activity_id(&activity), "activity id")
    );
    assert_eq!(protocol.protocol_version(), PROTOCOL_VERSION);
    assert_eq!(protocol.module_id(), 9);
    assert_committed_metadata(result, protocol);
    let authority = verify_lifecycle_batch(cluster, &receipt, &bytes);
    if ordinal == 3 {
        let call = must(NativeProgramCall::decode(activity.payload()), "native call");
        must(
            verify_authorized_program_execution(
                &bytes,
                &unhex(field(result, "terminal_payload")),
                &unhex(field(result, "call_graph")),
                &AuthorizedProgramExecutionExpectation {
                    authority,
                    activity_id: must(activity_id(&activity), "activity id"),
                    payload_hash: must(layerx_wire::hash::payload_hash(&activity), "payload hash"),
                    program_id: call.program_id.bytes(),
                    guest_abi_version: call.guest_abi,
                },
            ),
            "profile2 authorized execution",
        );
    } else {
        assert!(expect_success);
        must(
            verify_program_state(&bytes, &authority),
            "profile2 native lifecycle state",
        );
    }
    if expect_success {
        assert_eq!(protocol.result_code(), 0, "{label}: {}", response.text());
    } else {
        assert_ne!(protocol.result_code(), 0, "missing capability must refuse");
    }
    assert_eq!(journal_record(cluster, &key)["attempts"], 1);
    let replay = cluster
        .client
        .call(&Call::submit(path, &cluster.gateway_token, &key, &signed));
    assert_eq!(replay.status, 200, "{}", replay.text());
    assert_eq!(replay.text(), response.text());
    write(&evidence.join(format!("{label}.activity")), &signed, 0o600);
    write(&evidence.join(format!("{label}.receipt")), &bytes, 0o600);
    write(
        &evidence.join(format!("{label}.result.json")),
        &response.body,
        0o600,
    );
    let submitted = Submitted {
        key,
        activity_id: field(result, "activity_id").to_owned(),
        receipt: bytes,
        body: response.text(),
    };
    check_idempotency_lookup(cluster, &signed, &submitted);
    marker(label);
    submitted
}

fn trust(cluster: &Cluster, evidence: &Path) -> ProtocolDeploymentVerifier {
    let mut bytes = b"LayerX/sequencer-trust-history/v1\0".to_vec();
    bytes.extend_from_slice(&[0, 1, 0, 0]);
    bytes.extend_from_slice(&PROTOCOL_VERSION.to_be_bytes());
    bytes.extend_from_slice(&NETWORK_ID.to_be_bytes());
    bytes.extend_from_slice(&0_u64.to_be_bytes());
    bytes.extend_from_slice(&unhex(
        &cluster.sequencer_environment["LAYERX_NODE_SEQUENCER_ID"],
    ));
    bytes.extend_from_slice(&cluster.sequencer_key);
    bytes.extend_from_slice(&FIRST_BATCH.to_be_bytes());
    bytes.extend_from_slice(&LAST_BATCH.to_be_bytes());
    bytes.extend_from_slice(&[0; 9]);
    let path = evidence.join("provisioned-sequencer-history");
    write(&path, &bytes, 0o600);
    must(
        ProtocolDeploymentVerifier::from_protected_history(&path, 60_000),
        "protected trust",
    )
}

fn record_deployment(
    cluster: &Cluster,
    submitted: &Submitted,
    registry: &mut Registry,
    verifier: &ProtocolDeploymentVerifier,
    evidence: &Path,
    label: &str,
) {
    let path = format!("/internal/v1/deployment-proof/{}", submitted.activity_id);
    let deadline = Instant::now() + Duration::from_secs(15);
    let document = loop {
        let response = cluster.client.get(&path, Some(&cluster.registry_token));
        if response.status == 200 {
            break response.json();
        }
        assert_eq!(response.status, 503, "{}", response.text());
        assert!(
            Instant::now() < deadline,
            "genuine deployment proof unavailable"
        );
        thread::sleep(Duration::from_millis(20));
    };
    let bytes = unhex(field(&document, "proof_hex"));
    let proof = must(DeploymentProof::decode(&bytes), "native deployment proof");
    let verified = must(
        verifier.verify_deployment(&proof, now_ms()),
        "native deployment authority",
    );
    must(
        registry.record_verified_deployment(&verified),
        "authenticated registry update",
    );
    write(
        &evidence.join(format!("{label}.deployment-proof")),
        &bytes,
        0o600,
    );
}

fn reader(
    cluster: &Cluster,
    registry: &Registry,
    verifier: &ProtocolDeploymentVerifier,
) -> LayerxdProgramBalanceReader {
    let environment = &cluster.sequencer_environment;
    let endpoint = format!("http://127.0.0.1:{}", cluster.program_port);
    let authority = format!(
        "http://127.0.0.1:{}",
        environment["LAYERX_NODE_AUTHORITY_REPLICA_PORT"]
    );
    let replica_id = must(
        <[u8; 32]>::try_from(unhex(&environment["LAYERX_NODE_AUTHORITY_REPLICA_ID"])),
        "replica identity",
    );
    let ca = must(fs::read(&cluster.tls.server_der), "actual cluster CA");
    must(
        LayerxdProgramBalanceReader::connect(
            &endpoint,
            cluster.program_token.clone(),
            ProgramAuthority {
                endpoint: &authority,
                authorization: environment["LAYERX_NODE_AUTHORITY_REPLICA_BEARER_TOKEN"].clone(),
                replica_id,
                ca_der: &ca,
            },
            verifier.clone(),
            registry.clone(),
        ),
        "production independent protocol-state reader",
    )
}

fn state(
    cluster: &Cluster,
    program: [u8; 32],
    registry: &Registry,
    verifier: &ProtocolDeploymentVerifier,
    evidence: &Path,
    label: &str,
) -> ProtocolProgramStateRead {
    let mut reader = reader(cluster, registry, verifier);
    let program_id = must(RuntimeProgramId::new(program), "runtime program");
    let state = must(
        reader.read_protocol_state_profile2(program_id, now_ms()),
        "profile2 independent state read",
    );
    let balances = state.balances();
    let sequence = balances.freshness().observed_sequence;
    let response = http_get(
        cluster.program_port,
        &format!(
            "/v1/programs/{}/account-state?at={sequence}&profile=2",
            hex(&program)
        ),
        &cluster.program_token,
    );
    assert_eq!(response.status, 200, "{}", response.text());
    let document = response.json();
    let bytes = unhex(field(&document, "record_hex"));
    assert_eq!(bytes.get(..5), Some(&b"LXPS2"[..]));
    assert_eq!(
        field(&document, "record_digest"),
        hex(&Sha256::digest(&bytes))
    );
    let head = AccountStateHead {
        receipt_digest: balances.receipt_digest(),
        state_root: balances.state_root(),
        freshness: balances.freshness(),
    };
    let mut restored_registry = registry.clone();
    let restored = must(
        ProtocolProgramStateRead::restore_verified_profile2(
            &bytes,
            &mut restored_registry,
            head,
            head,
            now_ms(),
            60_000,
        ),
        "strict profile2 restore",
    );
    assert_eq!(restored, state);
    let mut changed = bytes.clone();
    let last = changed.len() - 1;
    changed[last] ^= 1;
    assert!(ProtocolProgramStateRead::restore_verified_profile2(
        &changed,
        &mut registry.clone(),
        head,
        head,
        now_ms(),
        60_000
    )
    .is_err());
    let mut stale = head;
    stale.freshness.observed_sequence += 1;
    assert!(ProtocolProgramStateRead::restore_verified_profile2(
        &bytes,
        &mut registry.clone(),
        head,
        stale,
        now_ms(),
        60_000
    )
    .is_err());
    write(&evidence.join(format!("{label}.LXPS2")), &bytes, 0o600);
    write(&evidence.join(format!("{label}.state")), &bytes, 0o600);
    write(
        &evidence.join(format!("{label}.state.json")),
        &response.body,
        0o600,
    );
    state
}

fn balance(state: &ProtocolProgramStateRead, account: [u8; 32], expected: u128) {
    let balances = state.balances();
    let value = balances
        .value_accounts()
        .iter()
        .find(|value| value.account_id == account)
        .unwrap_or_else(|| panic!("registered native account omitted"));
    assert_eq!(value.balance, expected);
    assert!(!value.frozen);
    assert!(balances
        .bindings()
        .iter()
        .any(|binding| binding.account_id == account));
}

#[test]
fn funded_account_profile2_survives_abi_upgrades_and_winddown() {
    let evidence = evidence_directory();
    let wasm = escrow_wasm();
    let mut wasm3 = wasm.clone();
    wasm3.extend_from_slice(&[0, 2, 1, b'3']);
    let mut wasm4 = wasm3.clone();
    wasm4.extend_from_slice(&[0, 2, 1, b'4']);
    let (mut cluster, custody) = custody::start_funded_cluster();
    custody.verify_evidence();
    check_readiness(&cluster);
    let program = random32();
    let account = derived_account(program);
    let verifier = trust(&cluster, &evidence);
    let mut registry = Registry::new();
    let mut sequence = 2;
    let deployed = submit(
        &cluster,
        1,
        "/v1/programs/deploy",
        &mut sequence,
        &deploy_payload(&cluster, program, &wasm),
        "deploy-abi2",
        &evidence,
        true,
    );
    record_deployment(
        &cluster,
        &deployed,
        &mut registry,
        &verifier,
        &evidence,
        "deploy-abi2",
    );
    submit(
        &cluster,
        6,
        "/v1/activities",
        &mut sequence,
        &registration(program, cluster.asset, SEED),
        "owner-opt-in-profile2",
        &evidence,
        true,
    );
    let registered = state(&cluster, program, &registry, &verifier, &evidence, "opt-in");
    balance(&registered, account, 0);
    submit(
        &cluster,
        3,
        "/v1/programs/call",
        &mut sequence,
        &call_payload(&cluster, program, 2, SEED, true),
        "fund-abi2",
        &evidence,
        true,
    );
    let funded = state(
        &cluster,
        program,
        &registry,
        &verifier,
        &evidence,
        "funded-abi2",
    );
    balance(&funded, account, DEPOSIT);
    let original_binding = funded.balances().bindings().to_vec();
    for (abi, old, new, seed) in [
        (3, &wasm[..], &wasm3[..], &b"profile2-abi3"[..]),
        (4, &wasm3[..], &wasm4[..], &b"profile2-abi4"[..]),
    ] {
        let label = format!("upgrade-abi{abi}");
        let upgraded = submit(
            &cluster,
            2,
            "/v1/programs/upgrade",
            &mut sequence,
            &upgrade(program, old, new, abi),
            &label,
            &evidence,
            true,
        );
        record_deployment(
            &cluster,
            &upgraded,
            &mut registry,
            &verifier,
            &evidence,
            &label,
        );
        let upgraded_state = state(&cluster, program, &registry, &verifier, &evidence, &label);
        balance(&upgraded_state, account, DEPOSIT);
        for binding in &original_binding {
            assert!(upgraded_state.balances().bindings().contains(binding));
        }
        let label = format!("additional-account-abi{abi}");
        submit(
            &cluster,
            6,
            "/v1/activities",
            &mut sequence,
            &legacy_registration(program, cluster.asset, seed),
            &label,
            &evidence,
            true,
        );
        let registered = state(&cluster, program, &registry, &verifier, &evidence, &label);
        balance(&registered, seeded_account(program, seed), 0);
        if abi == 4 {
            submit(
                &cluster,
                3,
                "/v1/programs/call",
                &mut sequence,
                &call_payload(&cluster, program, abi, seed, false),
                "missing-transfer-capability",
                &evidence,
                false,
            );
            let refused = state(
                &cluster,
                program,
                &registry,
                &verifier,
                &evidence,
                "missing-transfer-capability",
            );
            balance(&refused, seeded_account(program, seed), 0);
            balance(&refused, account, DEPOSIT);
        }
        let label = format!("fund-abi{abi}");
        submit(
            &cluster,
            3,
            "/v1/programs/call",
            &mut sequence,
            &call_payload(&cluster, program, abi, seed, true),
            &label,
            &evidence,
            true,
        );
        let funded = state(&cluster, program, &registry, &verifier, &evidence, &label);
        balance(&funded, seeded_account(program, seed), DEPOSIT);
        balance(&funded, account, DEPOSIT);
    }
    let after = state(
        &cluster,
        program,
        &registry,
        &verifier,
        &evidence,
        "before-native-restart",
    );
    cluster.boundary.stop();
    cluster.restart_native_sequencer();
    cluster.boundary.start();
    check_readiness(&cluster);
    let restarted = state(
        &cluster,
        program,
        &registry,
        &verifier,
        &evidence,
        "native-restart",
    );
    balance(&restarted, account, DEPOSIT);
    assert_eq!(restarted.balances().bindings(), after.balances().bindings());
    marker("native-restart");
    let accounts = [
        ("route", "exit", account, SEED),
        (
            "route-abi3",
            "exit-abi3",
            seeded_account(program, b"profile2-abi3"),
            &b"profile2-abi3"[..],
        ),
        (
            "route-abi4",
            "exit-abi4",
            seeded_account(program, b"profile2-abi4"),
            &b"profile2-abi4"[..],
        ),
    ];
    for (label, _, account_id, seed) in accounts {
        let operation = ProgramWindDownOperation::Route {
            account: account_id,
            asset: cluster.asset,
            destination: cluster.actor.source,
            seed,
        };
        submit(
            &cluster,
            7,
            "/v1/programs/wind-down",
            &mut sequence,
            &wind_down_payload(program, operation),
            label,
            &evidence,
            true,
        );
        let current = state(&cluster, program, &registry, &verifier, &evidence, label);
        for (_, _, identifier, _) in accounts {
            balance(&current, identifier, DEPOSIT);
        }
        assert!(current
            .balances()
            .bindings()
            .iter()
            .any(|binding| binding == &original_binding[0]));
    }
    submit(
        &cluster,
        7,
        "/v1/programs/wind-down",
        &mut sequence,
        &wind_down_payload(
            program,
            ProgramWindDownOperation::Deprecate {
                exit_program: program,
                deadline_batch: LAST_BATCH,
            },
        ),
        "deprecate",
        &evidence,
        true,
    );
    let deprecated = state(
        &cluster,
        program,
        &registry,
        &verifier,
        &evidence,
        "deprecate",
    );
    assert_eq!(
        deprecated.balances().lifecycle(),
        layerx_programs::ProgramLifecycle::Deprecated
    );
    for (index, (_, label, account_id, _)) in accounts.iter().enumerate() {
        submit(
            &cluster,
            7,
            "/v1/programs/wind-down",
            &mut sequence,
            &wind_down_payload(
                program,
                ProgramWindDownOperation::Exit {
                    account: *account_id,
                },
            ),
            label,
            &evidence,
            true,
        );
        let current = state(&cluster, program, &registry, &verifier, &evidence, label);
        for (position, (_, _, identifier, _)) in accounts.iter().enumerate() {
            balance(
                &current,
                *identifier,
                if position <= index { 0 } else { DEPOSIT },
            );
        }
    }
    submit(
        &cluster,
        7,
        "/v1/programs/wind-down",
        &mut sequence,
        &wind_down_payload(program, ProgramWindDownOperation::Tombstone),
        "tombstone",
        &evidence,
        true,
    );
    let tombstoned = state(
        &cluster,
        program,
        &registry,
        &verifier,
        &evidence,
        "tombstone",
    );
    assert_eq!(
        tombstoned.balances().lifecycle(),
        layerx_programs::ProgramLifecycle::Tombstoned
    );
    for (_, _, identifier, _) in accounts {
        balance(&tombstoned, identifier, 0);
    }
    marker("proof-corruption-refused");
    marker("stale-head-refused");
}
