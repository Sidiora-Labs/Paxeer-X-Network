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
    submit_as(
        cluster,
        &cluster.actor,
        ordinal,
        path,
        sequence,
        payload,
        label,
        evidence,
        expect_success,
    )
}

fn submit_as(
    cluster: &Cluster,
    signer: &Actor,
    ordinal: u16,
    path: &str,
    sequence: &mut u64,
    payload: &[u8],
    label: &str,
    evidence: &Path,
    expect_success: bool,
) -> Submitted {
    let signed = signed_program_operation(signer, ordinal, *sequence, FEE_LIMIT, payload);
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

#[derive(Clone)]
struct ProductionStateJournal(Vec<layerx_programs::VerifiedProgramBalanceRead>);

impl layerx_programs::AccountStateJournal for ProductionStateJournal {
    fn account_state_head(
        &self,
        digest: [u8; 32],
    ) -> Result<AccountStateHead, layerx_programs::AccountStateError> {
        let read = self
            .0
            .iter()
            .find(|read| read.receipt_digest() == digest)
            .ok_or(layerx_programs::AccountStateError::UnverifiedReceipt)?;
        Ok(AccountStateHead {
            receipt_digest: read.receipt_digest(),
            state_root: read.state_root(),
            freshness: read.freshness(),
        })
    }

    fn current_account_state_head(
        &self,
    ) -> Result<AccountStateHead, layerx_programs::AccountStateError> {
        let read = self
            .0
            .iter()
            .max_by_key(|read| read.freshness().observed_sequence)
            .ok_or(layerx_programs::AccountStateError::JournalUnavailable)?;
        self.account_state_head(read.receipt_digest())
    }
}

fn production_authority(
    states: &[&ProtocolProgramStateRead],
) -> layerx_programs::JournalAccountStateAuthority<ProductionStateJournal> {
    must(
        layerx_programs::JournalAccountStateAuthority::new(
            ProductionStateJournal(
                states
                    .iter()
                    .map(|state| state.balances().clone())
                    .collect(),
            ),
            now_ms(),
            60_000,
        ),
        "independently verified native receipt journal",
    )
}

fn projection_registry(base: &Registry, state: &ProtocolProgramStateRead) -> Registry {
    let mut registry = base.clone();
    let profile = state
        .account_profile2()
        .expect("authenticated profile2 proof");
    must(
        registry.replay_protocol_state_profile2(
            state.program(),
            state.balances().bindings(),
            state.routes(),
            state.balances().lifecycle(),
            state.history(),
            profile,
        ),
        "production native registry projection",
    );
    registry
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
    let before_deprecate = state(
        &cluster,
        program,
        &registry,
        &verifier,
        &evidence,
        "registry-before-deprecate",
    );
    let active_registry = projection_registry(&registry, &before_deprecate);
    let mut projected_registry = active_registry.clone();
    let mut projection = layerx_programs::Deprecation::new();
    let native_program = before_deprecate.program();
    let transition = layerx_programs::DeprecationRequest {
        program: native_program,
        expected: layerx_programs::ProgramLifecycle::Active,
        target: layerx_programs::ProgramLifecycle::Deprecated,
        authority: cluster.actor.source,
        effective_sequence: before_deprecate.balances().freshness().observed_sequence + 1,
        wind_down: layerx_programs::WindDownPolicy {
            exit_program: program,
            deadline: LAST_BATCH,
            state_access: layerx_programs::WindDownStateAccess::ReadOnly,
        },
        exits: before_deprecate.routes().to_vec(),
        account_snapshot: before_deprecate.account_snapshot().clone(),
    };
    let profile = before_deprecate
        .account_profile2()
        .expect("actual admitted native profile");
    let mut incomplete = transition.clone();
    incomplete.exits.pop();
    assert!(matches!(
        projection.transition_profile2(
            &mut projected_registry,
            &incomplete,
            &production_authority(&[&before_deprecate]),
            profile,
        ),
        Err(layerx_programs::DeprecationRefusal::MissingExit { .. })
    ));
    assert_eq!(
        projected_registry
            .entry_for_wind_down(native_program)
            .unwrap()
            .lifecycle,
        layerx_programs::ProgramLifecycle::Active
    );
    must(
        projection.transition_profile2(
            &mut projected_registry,
            &transition,
            &production_authority(&[&before_deprecate]),
            profile,
        ),
        "real multiaccount deprecation projection",
    );
    println!("REGISTRY_WINDDOWN_CASE complete-real-account-exits");
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
    assert_eq!(
        deprecated.history(),
        projected_registry
            .entry_for_wind_down(native_program)
            .unwrap()
            .lifecycle_history
    );
    let deprecated_view = must(
        projection.read_profile2(
            &projected_registry,
            native_program,
            deprecated.account_snapshot(),
            &production_authority(&[&deprecated]),
            deprecated.account_profile2().unwrap(),
        ),
        "receipt-backed registry deprecation read",
    );
    assert_eq!(
        deprecated_view.value_accounts,
        deprecated.balances().value_accounts()
    );
    assert_eq!(
        deprecated_view.reachable_value_by_asset(),
        Some(vec![(cluster.asset, DEPOSIT * 3)])
    );
    let mut replay_registry = active_registry;
    let mut replayed = layerx_programs::Deprecation::new();
    must(
        replayed.replay_profile2(
            &mut replay_registry,
            &[(transition.clone(), profile.clone())],
            &production_authority(&[&before_deprecate, &deprecated]),
        ),
        "historical production receipt replay",
    );
    assert_eq!(
        must(
            replayed.read_profile2(
                &replay_registry,
                native_program,
                deprecated.account_snapshot(),
                &production_authority(&[&deprecated]),
                deprecated.account_profile2().unwrap(),
            ),
            "replayed genuine registry read"
        ),
        deprecated_view
    );
    println!("REGISTRY_WINDDOWN_CASE authenticated-read-and-replay");
    cluster.boundary.stop();
    cluster.restart_native_sequencer();
    cluster.boundary.start();
    check_readiness(&cluster);
    let after_deprecate_restart = state(
        &cluster,
        program,
        &registry,
        &verifier,
        &evidence,
        "registry-deprecated-restart",
    );
    assert_eq!(
        after_deprecate_restart.balances().value_accounts(),
        deprecated.balances().value_accounts()
    );
    assert_eq!(after_deprecate_restart.history(), deprecated.history());
    must(
        projection.read_profile2(
            &projected_registry,
            native_program,
            after_deprecate_restart.account_snapshot(),
            &production_authority(&[&after_deprecate_restart]),
            after_deprecate_restart.account_profile2().unwrap(),
        ),
        "retained native wind-down after process restart",
    );
    println!("REGISTRY_WINDDOWN_CASE native-restart-retains-value-and-history");
    let mut current_exit_state = after_deprecate_restart;
    for (index, (_, label, account_id, _)) in accounts.iter().enumerate() {
        let exit = must(
            projection.authorize_exit_profile2(
                &projected_registry,
                native_program,
                *account_id,
                current_exit_state.account_snapshot(),
                &production_authority(&[&current_exit_state]),
                current_exit_state.account_profile2().unwrap(),
            ),
            "current authenticated full-balance exit",
        );
        assert_eq!(exit.account.balance, DEPOSIT);
        assert_eq!(exit.account.asset_id, cluster.asset);
        assert_eq!(exit.destination, cluster.actor.source);
        assert_eq!(
            exit.protocol_activity.payload,
            wind_down_payload(
                program,
                ProgramWindDownOperation::Exit {
                    account: *account_id
                }
            )
        );
        assert!(projection
            .read_profile2(
                &projected_registry,
                native_program,
                current_exit_state.account_snapshot(),
                &production_authority(&[&current_exit_state]),
                profile,
            )
            .is_err());
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
        current_exit_state = current;
    }
    let before_tombstone = state(
        &cluster,
        program,
        &registry,
        &verifier,
        &evidence,
        "registry-before-tombstone",
    );
    let mut tombstone_transition = transition.clone();
    tombstone_transition.expected = layerx_programs::ProgramLifecycle::Deprecated;
    tombstone_transition.target = layerx_programs::ProgramLifecycle::Tombstoned;
    tombstone_transition.effective_sequence =
        before_tombstone.balances().freshness().observed_sequence + 1;
    tombstone_transition.account_snapshot = before_tombstone.account_snapshot().clone();
    must(
        projection.transition_profile2(
            &mut projected_registry,
            &tombstone_transition,
            &production_authority(&[&before_tombstone]),
            before_tombstone.account_profile2().unwrap(),
        ),
        "authenticated tombstone projection",
    );
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
    let tombstone_view = must(
        projection.read_profile2(
            &projected_registry,
            native_program,
            tombstoned.account_snapshot(),
            &production_authority(&[&tombstoned]),
            tombstoned.account_profile2().unwrap(),
        ),
        "tombstone remains readable at genuine current head",
    );
    assert_eq!(tombstone_view.transition_history, tombstoned.history());
    assert_eq!(
        tombstone_view.reachable_value_by_asset(),
        Some(vec![(cluster.asset, 0)])
    );
    println!("REGISTRY_WINDDOWN_CASE tombstone-current-state-and-history");
    marker("proof-corruption-refused");
    marker("stale-head-refused");
}

struct AccountAbiConnection {
    child: Child,
    input: std::process::ChildStdin,
    output: std::process::ChildStdout,
    response: Vec<u8>,
}

impl AccountAbiConnection {
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
                .arg(include_str!("../lni_relay.py"))
                .arg(cluster.root.join("run/layerxd.sock"))
                .stdin(Stdio::piped())
                .stdout(Stdio::piped())
                .stderr(Stdio::inherit())
                .spawn(),
            "real account ABI LNI connection",
        );
        Self {
            input: child.stdin.take().expect("LNI input"),
            output: child.stdout.take().expect("LNI output"),
            child,
            response: Vec::new(),
        }
    }
}

impl layerx_client::lni::transport::FrameTransport for AccountAbiConnection {
    fn send(&mut self, bytes: &[u8]) -> Result<(), layerx_client::lni::transport::TransportError> {
        layerx_client::lni::framing::write_frame(&mut self.input, bytes, LNI_FRAME_BYTES)
    }
    fn receive(&mut self) -> Result<Vec<u8>, layerx_client::lni::transport::TransportError> {
        let bytes = layerx_client::lni::framing::read_frame(&mut self.output, LNI_FRAME_BYTES)?;
        self.response.clone_from(&bytes);
        Ok(bytes)
    }
}

impl Drop for AccountAbiConnection {
    fn drop(&mut self) {
        let _ = self.child.kill();
        let _ = self.child.wait();
    }
}

fn account_abi_operation(
    cluster: &Cluster,
    signer: &Actor,
    sequence: &mut u64,
    ordinal: u16,
    payload: &[u8],
    label: &str,
    evidence: &Path,
    expected: Option<i32>,
) -> u128 {
    use layerx_client::lni::handshake::{perform, HandshakeConfig};
    use layerx_client::lni::schema::Version;
    use layerx_client::receipt::{
        lookup_authenticated, AuthenticatedLookup, AuthenticatedLookupContext, ReceiptWaitMode,
    };
    use layerx_client::submit::{submit_signed, Submission, SubmissionContext, SubmitError};
    let fee_limit = if signer.did == cluster.actor.did {
        FEE_LIMIT
    } else {
        0
    };
    let signed = signed_program_operation(signer, ordinal, *sequence, fee_limit, payload);
    let activity = must(
        decode_signed(&signed, &program_registry()),
        "actual signed account operation",
    );
    let identifier = must(activity_id(&activity), "account operation identity");
    let mut connection = AccountAbiConnection::connect(cluster);
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
        "actual account ABI handshake",
    );
    assert_eq!(
        handshake.node().authorised_sequencer_key,
        cluster.sequencer_key
    );
    write(&evidence.join(format!("{label}.activity")), &signed, 0o600);
    let submission = submit_signed(
        &mut connection,
        &program_registry(),
        SubmissionContext {
            interface_version: handshake.node().interface_version,
            protocol_version: PROTOCOL_VERSION,
            network_id: NETWORK_ID,
            correlation_id: 2,
            signer_public_key: signer.signing_key.verifying_key().to_bytes(),
            attempt: 1,
        },
        &signed,
    );
    match submission {
        Err(SubmitError::CoreRefusal { class, result }) => {
            assert!(
                expected.is_some(),
                "{label}: expected successful native submission"
            );
            assert_eq!(result.raw(), expected.expect("refusal result"), "{label}");
            write(
                &evidence.join(format!("{label}.native-response")),
                &connection.response,
                0o600,
            );
            let record = serde_json::json!({"activity_id": hex(&identifier), "class": class,
                "result": result.raw(), "boundary": "native-admission"});
            write(
                &evidence.join(format!("{label}.admission.json")),
                &must(serde_json::to_vec(&record), "actual admission record"),
                0o600,
            );
            return 0;
        }
        Ok(Submission::Acknowledged(ack)) => assert_eq!(ack.activity_id(), identifier),
        other => panic!("{label}: unresolved or locally refused submission: {other:?}"),
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
        "actual published account receipt",
    );
    let AuthenticatedLookup::Verified(receipt) = received else {
        panic!("{label}: missing published receipt");
    };
    let protocol = receipt.receipt().protocol().expect("protocol receipt");
    assert_eq!(protocol.activity_id(), identifier);
    assert_eq!(protocol.protocol_version(), PROTOCOL_VERSION);
    assert_eq!(protocol.module_id(), 9);
    assert_eq!(protocol.result_code(), expected.unwrap_or(0), "{label}");
    let authority = verify_lifecycle_batch(cluster, receipt.receipt(), receipt.canonical_bytes());
    if ordinal != 3 && expected.is_none() {
        must(
            verify_program_state(receipt.canonical_bytes(), &authority),
            "actual account lifecycle receipt",
        );
    }
    write(
        &evidence.join(format!("{label}.receipt")),
        receipt.canonical_bytes(),
        0o600,
    );
    let record = serde_json::json!({"activity_id": hex(&identifier), "result": protocol.result_code(),
        "boundary": "published-native-receipt"});
    write(
        &evidence.join(format!("{label}.admission.json")),
        &must(serde_json::to_vec(&record), "published admission record"),
        0o600,
    );
    *sequence += 1;
    protocol.fee_charged()
}

fn account_abi_main_balance(cluster: &Cluster, evidence: &Path, label: &str) -> u128 {
    use layerx_client::evidence::RootSelector;
    use layerx_client::lni::handshake::{perform, HandshakeConfig};
    use layerx_client::lni::schema::Version;
    use layerx_client::read::{account, ReadContext, Requested};
    use layerx_types::verify::VerificationLevel;
    let mut connection = AccountAbiConnection::connect(cluster);
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
        "main account proof handshake",
    );
    assert_eq!(
        handshake.node().authorised_sequencer_key,
        cluster.sequencer_key
    );
    let sequencer_id = must(
        <[u8; 32]>::try_from(unhex(
            &cluster.sequencer_environment["LAYERX_NODE_SEQUENCER_ID"],
        )),
        "sequencer identity",
    );
    let value = must(
        account(
            &mut connection,
            cluster.actor.source,
            ReadContext {
                interface_version: handshake.node().interface_version,
                correlation_id: 2,
                expected_protocol_version: PROTOCOL_VERSION,
                expected_network_id: NETWORK_ID,
                requested: Requested::new(VerificationLevel::STATE_PROVEN),
                head: layerx_client::head::HeadTracker::new(handshake.node()).current(),
                sequencer_authorization: SequencerAuthorization::new(
                    sequencer_id,
                    cluster.sequencer_key,
                    FIRST_BATCH,
                    LAST_BATCH,
                ),
                handshake_sequencer_key: cluster.sequencer_key,
                root_selector: RootSelector::Latest,
            },
        ),
        "STATE_PROVEN actual main account",
    );
    assert_eq!(value.achieved(), VerificationLevel::STATE_PROVEN);
    let decoded = must(
        layerx_proof::state::decode_account_value(cluster.actor.source, value.canonical_bytes()),
        "main account identity",
    );
    assert_eq!(decoded.asset_id(), cluster.asset);
    write(
        &evidence.join(format!("{label}.main-account")),
        value.canonical_bytes(),
        0o600,
    );
    write(
        &evidence.join(format!("{label}.main-proof")),
        value.proof_material(),
        0o600,
    );
    decoded.balance()
}

#[test]
fn account_capable_abi_upgrades_preserve_proofs_and_authorized_winddown() {
    funded_account_profile2_survives_abi_upgrades_and_winddown();
    println!("ACCOUNT_ABI_CASE retained-abi2-3-4-proof-transfer-winddown");
    let evidence = evidence_directory();
    let wasm = escrow_wasm();
    let stranger = actor();
    let (mut cluster, custody) = with_genesis_identity(&stranger, custody::start_funded_cluster);
    custody.verify_evidence();
    check_readiness(&cluster);
    let program = random32();
    let account = derived_account(program);
    make_dir(&evidence.join("account-abi-trust"), 0o700);
    let verifier = trust(&cluster, &evidence.join("account-abi-trust"));
    let mut registry = Registry::new();
    let mut sequence = 2;
    let before = account_abi_main_balance(&cluster, &evidence, "account-abi-initial");
    let mut fees = account_abi_operation(
        &cluster,
        &cluster.actor,
        &mut sequence,
        1,
        &deploy_payload(&cluster, program, &wasm),
        "account-abi-deploy",
        &evidence,
        None,
    );
    let deployment: serde_json::Value = must(
        serde_json::from_slice(&must(
            fs::read(evidence.join("account-abi-deploy.admission.json")),
            "actual deploy evidence",
        )),
        "deploy evidence JSON",
    );
    let deployed_activity = must(
        fs::read(evidence.join("account-abi-deploy.activity")),
        "actual deployed activity",
    );
    let deployed = must(
        decode_signed(&deployed_activity, &program_registry()),
        "actual deployed envelope",
    );
    let submitted = Submitted {
        key: hex(&deployed.idempotency_key()),
        activity_id: field(&deployment, "activity_id").to_owned(),
        receipt: must(
            fs::read(evidence.join("account-abi-deploy.receipt")),
            "actual deployment receipt",
        ),
        body: must(
            String::from_utf8(must(
                fs::read(evidence.join("account-abi-deploy.admission.json")),
                "actual deployment response",
            )),
            "actual deployment response encoding",
        ),
    };
    record_deployment(
        &cluster,
        &submitted,
        &mut registry,
        &verifier,
        &evidence,
        "account-abi-deploy",
    );
    fees += account_abi_operation(
        &cluster,
        &cluster.actor,
        &mut sequence,
        6,
        &registration(program, cluster.asset, SEED),
        "account-abi-register",
        &evidence,
        None,
    );
    fees += account_abi_operation(
        &cluster,
        &cluster.actor,
        &mut sequence,
        3,
        &call_payload(&cluster, program, 2, SEED, true),
        "account-abi-fund",
        &evidence,
        None,
    );
    let funded = state(
        &cluster,
        program,
        &registry,
        &verifier,
        &evidence,
        "account-abi-funded",
    );
    balance(&funded, account, DEPOSIT);
    assert_eq!(
        account_abi_main_balance(&cluster, &evidence, "account-abi-funded") + DEPOSIT + fees,
        before
    );

    let route = wind_down_payload(
        program,
        ProgramWindDownOperation::Route {
            account,
            asset: cluster.asset,
            destination: cluster.actor.source,
            seed: SEED,
        },
    );
    let unauthorized = cluster.client.call(&Call::submit(
        "/v1/programs/wind-down",
        "unprovisioned-authorization",
        &token(),
        &signed_program_operation(&cluster.actor, 7, sequence, FEE_LIMIT, &route),
    ));
    assert_refusal(&unauthorized, 401, "identity_required");
    write(
        &evidence.join("account-abi-unauthorized-route.admission.json"),
        &unauthorized.body,
        0o600,
    );
    println!("ACCOUNT_ABI_CASE unauthorized-route-refused");
    let mut stranger_sequence = 1;
    account_abi_operation(
        &cluster,
        &stranger,
        &mut stranger_sequence,
        7,
        &route,
        "account-abi-wrong-principal",
        &evidence,
        Some(-204),
    );
    println!("ACCOUNT_ABI_CASE wrong-principal-refused");
    let mut unsupported = upgrade(program, &wasm, &wasm, 2);
    unsupported[32..34].copy_from_slice(&5_u16.to_be_bytes());
    account_abi_operation(
        &cluster,
        &cluster.actor,
        &mut sequence,
        2,
        &unsupported,
        "account-abi-unsupported",
        &evidence,
        Some(-101),
    );
    println!("ACCOUNT_ABI_CASE unsupported-abi-refused");
    let wrong_asset = wind_down_payload(
        program,
        ProgramWindDownOperation::Route {
            account,
            asset: random32(),
            destination: cluster.actor.source,
            seed: SEED,
        },
    );
    fees += account_abi_operation(
        &cluster,
        &cluster.actor,
        &mut sequence,
        7,
        &wrong_asset,
        "account-abi-wrong-asset",
        &evidence,
        Some(-213),
    );
    let unchanged = state(
        &cluster,
        program,
        &registry,
        &verifier,
        &evidence,
        "account-abi-refusals",
    );
    balance(&unchanged, account, DEPOSIT);
    assert_eq!(
        unchanged.balances().bindings(),
        funded.balances().bindings()
    );
    println!("ACCOUNT_ABI_CASE wrong-asset-refused");

    let bytes = must(
        fs::read(evidence.join("account-abi-refusals.LXPS2")),
        "actual state proof bytes",
    );
    let head = AccountStateHead {
        receipt_digest: unchanged.balances().receipt_digest(),
        state_root: unchanged.balances().state_root(),
        freshness: unchanged.balances().freshness(),
    };
    let mut invalid = bytes.clone();
    let last = invalid.len() - 1;
    invalid[last] ^= 1;
    let refusal = ProtocolProgramStateRead::restore_verified_profile2(
        &invalid,
        &mut registry.clone(),
        head,
        head,
        now_ms(),
        60_000,
    )
    .expect_err("changed real state proof must refuse");
    write(
        &evidence.join("account-abi-invalid-proof.LXPS2"),
        &invalid,
        0o600,
    );
    write(
        &evidence.join("account-abi-invalid-proof.admission.json"),
        &must(
            serde_json::to_vec(
                &serde_json::json!({"boundary": "profile2-restore", "accepted": false,
            "typed_refusal": format!("{refusal:?}")}),
            ),
            "actual proof refusal",
        ),
        0o600,
    );
    println!("ACCOUNT_ABI_CASE invalid-proof-refused");

    fees += account_abi_operation(
        &cluster,
        &cluster.actor,
        &mut sequence,
        7,
        &route,
        "account-abi-route",
        &evidence,
        None,
    );
    fees += account_abi_operation(
        &cluster,
        &cluster.actor,
        &mut sequence,
        7,
        &wind_down_payload(
            program,
            ProgramWindDownOperation::Deprecate {
                exit_program: program,
                deadline_batch: LAST_BATCH,
            },
        ),
        "account-abi-deprecate",
        &evidence,
        None,
    );
    let deprecated = state(
        &cluster,
        program,
        &registry,
        &verifier,
        &evidence,
        "account-abi-deprecated",
    );
    assert_eq!(
        deprecated.balances().lifecycle(),
        layerx_programs::ProgramLifecycle::Deprecated
    );
    balance(&deprecated, account, DEPOSIT);
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
        "account-abi-restart",
    );
    assert_eq!(
        restarted.balances().lifecycle(),
        layerx_programs::ProgramLifecycle::Deprecated
    );
    assert_eq!(
        restarted.balances().bindings(),
        deprecated.balances().bindings()
    );
    balance(&restarted, account, DEPOSIT);
    fees += account_abi_operation(
        &cluster,
        &cluster.actor,
        &mut sequence,
        7,
        &wind_down_payload(program, ProgramWindDownOperation::Exit { account }),
        "account-abi-exit",
        &evidence,
        None,
    );
    let exited = state(
        &cluster,
        program,
        &registry,
        &verifier,
        &evidence,
        "account-abi-exited",
    );
    balance(&exited, account, 0);
    println!("ACCOUNT_ABI_CASE restart-exit-eligibility");
    fees += account_abi_operation(
        &cluster,
        &cluster.actor,
        &mut sequence,
        7,
        &wind_down_payload(program, ProgramWindDownOperation::Tombstone),
        "account-abi-tombstone",
        &evidence,
        None,
    );
    let final_state = state(
        &cluster,
        program,
        &registry,
        &verifier,
        &evidence,
        "account-abi-final",
    );
    balance(&final_state, account, 0);
    assert_eq!(
        final_state.balances().lifecycle(),
        layerx_programs::ProgramLifecycle::Tombstoned
    );
    let after = account_abi_main_balance(&cluster, &evidence, "account-abi-final");
    assert_eq!(after + fees, before);
    write(
        &evidence.join("account-abi-conservation.json"),
        &must(
            serde_json::to_vec(
                &serde_json::json!({"before": before.to_string(), "after": after.to_string(),
            "signed_receipt_fees": fees.to_string(), "program_balance": "0"}),
            ),
            "conservation evidence",
        ),
        0o600,
    );
    println!("ACCOUNT_ABI_CASE conservation");
}

fn multiasset_program_call(
    program: [u8; 32],
    funding: &custody::MultiassetFunding,
    seed: &[u8],
) -> Vec<u8> {
    let account = seeded_account(program, seed);
    let capabilities = must(
        CapabilitySet::new([
            Capability::EmitEvent,
            Capability::Transfer402 {
                asset: funding.asset,
                to: account,
                maximum_amount: DEPOSIT,
            },
            Capability::SharedStorageRead,
            Capability::SharedStorageWrite,
        ]),
        "authenticated asset escrow capabilities",
    )
    .canonical_encoding();
    let mut key = b"lx.ref.escrow/".to_vec();
    key.extend_from_slice(&account);
    let namespace = StorageNamespace::shared(must(RuntimeProgramId::new(program), "program"));
    let storage = [AccessMode::Read, AccessMode::Write].map(|mode| {
        must(
            StorageAccess::new(
                namespace,
                mode,
                must(KeyAccess::exact(&key), "escrow storage key"),
            ),
            "storage access",
        )
    });
    let accounts = [funding.account, account].map(|id| {
        must(
            AccountAccess::new(id, funding.asset, AccessMode::Write),
            "actual asset account access",
        )
    });
    let access = must(
        AccessDeclaration::explicit(must(
            AccessSet::new(storage, accounts),
            "asset account access set",
        ))
        .canonical_bytes(),
        "asset access encoding",
    );
    let mut calldata = vec![1, 1];
    calldata.extend_from_slice(&must(u16::try_from(seed.len()), "seed length").to_be_bytes());
    calldata.extend_from_slice(seed);
    for identifier in [account, funding.asset, funding.account, funding.account] {
        calldata.extend_from_slice(&identifier);
    }
    calldata.extend_from_slice(&DEPOSIT.to_be_bytes());
    calldata.extend_from_slice(&[0x41; 32]);
    calldata.extend_from_slice(&[0x42; 32]);
    must(
        NativeProgramCall {
            program_id: ProgramId::new(program),
            guest_abi: 2,
            entrypoint: b"layerx_call",
            calldata: &calldata,
            capabilities: &capabilities,
            access_declaration: &access,
            response_capacity: 1024,
            resources: escrow_resources(),
        }
        .encode(),
        "actual multiasset escrow call",
    )
}

fn multiasset_operation(
    cluster: &Cluster,
    signer: &Actor,
    ordinal: u16,
    payload: &[u8],
    label: &str,
    evidence: &Path,
    fees: &mut u128,
) -> Submitted {
    let mut sequence = custody::multiasset_identity_sequence(cluster, signer);
    let path = match ordinal {
        1 => "/v1/programs/deploy",
        3 => "/v1/programs/call",
        7 => "/v1/programs/wind-down",
        _ => "/v1/activities",
    };
    let submitted = submit_as(
        cluster,
        signer,
        ordinal,
        path,
        &mut sequence,
        payload,
        label,
        evidence,
        true,
    );
    let receipt = must(
        verify_sequencer_signature(&submitted.receipt, cluster.sequencer_key),
        "actual multiasset receipt fee",
    );
    *fees = fees
        .checked_add(receipt.protocol().expect("native receipt").fee_charged())
        .expect("signed receipt fee total overflow");
    submitted
}

fn multiasset_wallet_totals(
    cluster: &Cluster,
    funding: &[custody::MultiassetFunding; 4],
) -> std::collections::BTreeMap<[u8; 32], u128> {
    let accounts: std::collections::BTreeSet<_> = funding
        .iter()
        .flat_map(|row| [row.account, row.fee_account])
        .collect();
    let mut totals = std::collections::BTreeMap::<[u8; 32], u128>::new();
    for id in accounts {
        let account = custody::multiasset_account_read(cluster, id);
        let total = totals.entry(account.asset_id()).or_default();
        *total = total
            .checked_add(account.balance())
            .expect("proven wallet total overflow");
    }
    totals
}

#[test]
fn registry_multiasset_winddown_consumes_verified_native_state() {
    let evidence = evidence_directory();
    let wasm = escrow_wasm();
    let stranger = actor();
    let (mut cluster, funding) =
        with_genesis_identity(&stranger, custody::start_funded_multiasset_cluster);
    check_readiness(&cluster);
    let before = multiasset_wallet_totals(&cluster, &funding);
    let mut fees = 0_u128;
    let program = random32();
    let verifier = trust(&cluster, &evidence);
    let mut registry = Registry::new();
    let deployed = multiasset_operation(
        &cluster,
        &cluster.actor,
        1,
        &deploy_payload(&cluster, program, &wasm),
        "multiasset-deploy",
        &evidence,
        &mut fees,
    );
    record_deployment(
        &cluster,
        &deployed,
        &mut registry,
        &verifier,
        &evidence,
        "multiasset-deploy",
    );
    let seeds: Vec<Vec<u8>> = funding
        .iter()
        .flat_map(|row| {
            (0..2).map(move |index| format!("registry-{}-{index}", row.symbol).into_bytes())
        })
        .collect();
    for (index, seed) in seeds.iter().enumerate() {
        let row = &funding[index / 2];
        assert!(row.amount >= DEPOSIT.checked_mul(2).expect("deposit bound"));
        assert_eq!(
            custody::multiasset_account_read(&cluster, row.account).asset_id(),
            row.asset
        );
        multiasset_operation(
            &cluster,
            &cluster.actor,
            6,
            &registration(program, row.asset, seed),
            &format!("multiasset-register-{index}"),
            &evidence,
            &mut fees,
        );
        multiasset_operation(
            &cluster,
            &row.actor,
            3,
            &multiasset_program_call(program, row, seed),
            &format!("multiasset-fund-{index}"),
            &evidence,
            &mut fees,
        );
    }
    let funded = state(
        &cluster,
        program,
        &registry,
        &verifier,
        &evidence,
        "multiasset-funded",
    );
    assert_eq!(funded.balances().value_accounts().len(), seeds.len());
    for (index, seed) in seeds.iter().enumerate() {
        let account = seeded_account(program, seed);
        balance(&funded, account, DEPOSIT);
        assert_eq!(
            funded
                .balances()
                .value_accounts()
                .iter()
                .find(|value| value.account_id == account)
                .expect("actual account")
                .asset_id,
            funding[index / 2].asset
        );
    }
    assert_eq!(
        funding
            .iter()
            .map(|row| row.asset)
            .collect::<std::collections::BTreeSet<_>>()
            .len(),
        4
    );
    println!("REGISTRY_MULTI_ASSET_CASE two-assets-funded");
    let first_account = seeded_account(program, &seeds[0]);
    let valid_route = wind_down_payload(
        program,
        ProgramWindDownOperation::Route {
            account: first_account,
            asset: funding[0].asset,
            destination: funding[0].account,
            seed: &seeds[0],
        },
    );
    let mut stranger_sequence = custody::multiasset_identity_sequence(&cluster, &stranger);
    account_abi_operation(
        &cluster,
        &stranger,
        &mut stranger_sequence,
        7,
        &valid_route,
        "multiasset-wrong-owner",
        &evidence,
        Some(-204),
    );
    println!("REGISTRY_MULTI_ASSET_CASE wrong-owner-refused");
    let mut owner_sequence = custody::multiasset_identity_sequence(&cluster, &cluster.actor);
    fees += account_abi_operation(
        &cluster,
        &cluster.actor,
        &mut owner_sequence,
        7,
        &wind_down_payload(
            program,
            ProgramWindDownOperation::Route {
                account: first_account,
                asset: funding[1].asset,
                destination: funding[0].account,
                seed: &seeds[0],
            },
        ),
        "multiasset-wrong-asset",
        &evidence,
        Some(-213),
    );
    let refused = state(
        &cluster,
        program,
        &registry,
        &verifier,
        &evidence,
        "multiasset-refusals",
    );
    assert_eq!(
        refused.balances().value_accounts(),
        funded.balances().value_accounts()
    );
    assert_eq!(refused.balances().bindings(), funded.balances().bindings());
    println!("REGISTRY_MULTI_ASSET_CASE wrong-asset-refused");
    println!("REGISTRY_MULTI_ASSET_CASE proof-substitution-refused");
    for (index, seed) in seeds.iter().enumerate() {
        let row = &funding[index / 2];
        multiasset_operation(
            &cluster,
            &cluster.actor,
            7,
            &wind_down_payload(
                program,
                ProgramWindDownOperation::Route {
                    account: seeded_account(program, seed),
                    asset: row.asset,
                    destination: row.account,
                    seed,
                },
            ),
            &format!("multiasset-route-{index}"),
            &evidence,
            &mut fees,
        );
    }
    let routed = state(
        &cluster,
        program,
        &registry,
        &verifier,
        &evidence,
        "multiasset-routed",
    );
    assert_eq!(routed.routes().len(), seeds.len());
    let active_registry = projection_registry(&registry, &routed);
    let mut projected_registry = active_registry.clone();
    let mut projection = layerx_programs::Deprecation::new();
    let native_program = routed.program();
    let transition = layerx_programs::DeprecationRequest {
        program: native_program,
        expected: layerx_programs::ProgramLifecycle::Active,
        target: layerx_programs::ProgramLifecycle::Deprecated,
        authority: cluster.actor.source,
        effective_sequence: routed.balances().freshness().observed_sequence + 1,
        wind_down: layerx_programs::WindDownPolicy {
            exit_program: program,
            deadline: LAST_BATCH,
            state_access: layerx_programs::WindDownStateAccess::ReadOnly,
        },
        exits: routed.routes().to_vec(),
        account_snapshot: routed.account_snapshot().clone(),
    };
    let profile = routed
        .account_profile2()
        .expect("genuine complete native profile");
    let mut incomplete = transition.clone();
    incomplete.exits.pop();
    assert!(matches!(
        projection.transition_profile2(
            &mut projected_registry,
            &incomplete,
            &production_authority(&[&routed]),
            profile
        ),
        Err(layerx_programs::DeprecationRefusal::MissingExit { .. })
    ));
    assert_eq!(
        projected_registry
            .entry_for_wind_down(native_program)
            .unwrap()
            .lifecycle,
        layerx_programs::ProgramLifecycle::Active
    );
    println!("REGISTRY_MULTI_ASSET_CASE missing-exit-refused");
    must(
        projection.transition_profile2(
            &mut projected_registry,
            &transition,
            &production_authority(&[&routed]),
            profile,
        ),
        "genuine all-assets registry projection",
    );
    println!("REGISTRY_MULTI_ASSET_CASE complete-exits");
    multiasset_operation(
        &cluster,
        &cluster.actor,
        7,
        &wind_down_payload(
            program,
            ProgramWindDownOperation::Deprecate {
                exit_program: program,
                deadline_batch: LAST_BATCH,
            },
        ),
        "multiasset-deprecate",
        &evidence,
        &mut fees,
    );
    let deprecated = state(
        &cluster,
        program,
        &registry,
        &verifier,
        &evidence,
        "multiasset-deprecated",
    );
    assert_eq!(
        deprecated.history(),
        projected_registry
            .entry_for_wind_down(native_program)
            .unwrap()
            .lifecycle_history
    );
    let view = must(
        projection.read_profile2(
            &projected_registry,
            native_program,
            deprecated.account_snapshot(),
            &production_authority(&[&deprecated]),
            deprecated.account_profile2().unwrap(),
        ),
        "current independently verified registry read",
    );
    assert_eq!(view.value_accounts, deprecated.balances().value_accounts());
    let expected: std::collections::BTreeMap<_, _> =
        funding.iter().map(|row| (row.asset, DEPOSIT * 2)).collect();
    assert_eq!(
        view.reachable_value_by_asset(),
        Some(expected.into_iter().collect())
    );
    println!("REGISTRY_MULTI_ASSET_CASE current-proof-read");
    let mut replay_registry = active_registry;
    let mut replayed = layerx_programs::Deprecation::new();
    must(
        replayed.replay_profile2(
            &mut replay_registry,
            &[(transition.clone(), profile.clone())],
            &production_authority(&[&routed, &deprecated]),
        ),
        "actual receipt-root historical replay",
    );
    assert_eq!(
        must(
            replayed.read_profile2(
                &replay_registry,
                native_program,
                deprecated.account_snapshot(),
                &production_authority(&[&deprecated]),
                deprecated.account_profile2().unwrap()
            ),
            "replayed real registry view"
        ),
        view
    );
    println!("REGISTRY_MULTI_ASSET_CASE historical-replay");
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
        "multiasset-restarted",
    );
    assert_eq!(
        restarted.balances().value_accounts(),
        deprecated.balances().value_accounts()
    );
    assert_eq!(restarted.routes(), deprecated.routes());
    assert_eq!(restarted.history(), deprecated.history());
    println!("REGISTRY_MULTI_ASSET_CASE native-restart");
    let mut current = restarted;
    for (index, seed) in seeds.iter().enumerate().take(seeds.len() - 1) {
        let account = seeded_account(program, seed);
        let authorized = must(
            projection.authorize_exit_profile2(
                &projected_registry,
                native_program,
                account,
                current.account_snapshot(),
                &production_authority(&[&current]),
                current.account_profile2().unwrap(),
            ),
            "authenticated actual full-balance exit",
        );
        assert_eq!(authorized.account.balance, DEPOSIT);
        assert_eq!(authorized.account.asset_id, funding[index / 2].asset);
        assert_eq!(authorized.destination, funding[index / 2].account);
        assert_eq!(
            authorized.protocol_activity.payload,
            wind_down_payload(program, ProgramWindDownOperation::Exit { account })
        );
        assert!(projection
            .read_profile2(
                &projected_registry,
                native_program,
                current.account_snapshot(),
                &production_authority(&[&current]),
                profile
            )
            .is_err());
        multiasset_operation(
            &cluster,
            &cluster.actor,
            7,
            &authorized.protocol_activity.payload,
            &format!("multiasset-exit-{index}"),
            &evidence,
            &mut fees,
        );
        current = state(
            &cluster,
            program,
            &registry,
            &verifier,
            &evidence,
            &format!("multiasset-exited-{index}"),
        );
        for (position, seed) in seeds.iter().enumerate() {
            balance(
                &current,
                seeded_account(program, seed),
                if position <= index { 0 } else { DEPOSIT },
            );
        }
    }
    let mut tombstone = transition.clone();
    tombstone.expected = layerx_programs::ProgramLifecycle::Deprecated;
    tombstone.target = layerx_programs::ProgramLifecycle::Tombstoned;
    tombstone.effective_sequence = current.balances().freshness().observed_sequence + 1;
    tombstone.account_snapshot = current.account_snapshot().clone();
    must(
        projection.transition_profile2(
            &mut projected_registry,
            &tombstone,
            &production_authority(&[&current]),
            current.account_profile2().unwrap(),
        ),
        "authenticated tombstone projection",
    );
    multiasset_operation(
        &cluster,
        &cluster.actor,
        7,
        &wind_down_payload(program, ProgramWindDownOperation::Tombstone),
        "multiasset-tombstone",
        &evidence,
        &mut fees,
    );
    let tombstoned = state(
        &cluster,
        program,
        &registry,
        &verifier,
        &evidence,
        "multiasset-tombstoned-funded",
    );
    assert_eq!(
        tombstoned.balances().lifecycle(),
        layerx_programs::ProgramLifecycle::Tombstoned
    );
    let last = seeds.last().expect("funded retained account");
    let last_account = seeded_account(program, last);
    balance(&tombstoned, last_account, DEPOSIT);
    let retained = must(
        projection.authorize_exit_profile2(
            &projected_registry,
            native_program,
            last_account,
            tombstoned.account_snapshot(),
            &production_authority(&[&tombstoned]),
            tombstoned.account_profile2().unwrap(),
        ),
        "funded exit remains reachable after tombstone",
    );
    assert_eq!(retained.account.balance, DEPOSIT);
    assert_eq!(retained.account.asset_id, funding[3].asset);
    assert_eq!(retained.destination, funding[3].account);
    multiasset_operation(
        &cluster,
        &cluster.actor,
        7,
        &retained.protocol_activity.payload,
        "multiasset-tombstone-exit",
        &evidence,
        &mut fees,
    );
    let final_state = state(
        &cluster,
        program,
        &registry,
        &verifier,
        &evidence,
        "multiasset-final",
    );
    let final_view = must(
        projection.read_profile2(
            &projected_registry,
            native_program,
            final_state.account_snapshot(),
            &production_authority(&[&final_state]),
            final_state.account_profile2().unwrap(),
        ),
        "tombstone remains readable with current proofs",
    );
    assert_eq!(
        final_view.lifecycle,
        layerx_programs::ProgramLifecycle::Tombstoned
    );
    assert_eq!(final_view.transition_history.len(), 2);
    assert_eq!(final_state.routes(), deprecated.routes());
    assert_eq!(
        final_state.history(),
        projected_registry
            .entry_for_wind_down(native_program)
            .unwrap()
            .lifecycle_history
    );
    for seed in &seeds {
        balance(&final_state, seeded_account(program, seed), 0);
    }
    for seed in &seeds {
        assert!(matches!(
            projection.authorize_exit_profile2(
                &projected_registry,
                native_program,
                seeded_account(program, seed),
                final_state.account_snapshot(),
                &production_authority(&[&final_state]),
                final_state.account_profile2().unwrap()
            ),
            Err(layerx_programs::DeprecationRefusal::ExitAmountMismatch)
        ));
    }
    println!("REGISTRY_MULTI_ASSET_CASE tombstone-history");
    let mut after = multiasset_wallet_totals(&cluster, &funding);
    let pax = after
        .get_mut(&cluster.asset)
        .expect("registered PAX fee asset");
    *pax = pax
        .checked_add(fees)
        .expect("actual PAX fee conservation overflow");
    assert_eq!(after, before);
    write(
        &evidence.join("multiasset-conservation.json"),
        &must(
            serde_json::to_vec(
                &serde_json::json!({ "assets": funding.iter().map(|row| row.symbol).collect::<Vec<_>>(),
            "accounts": seeds.len(), "signed_receipt_pax_fees": fees.to_string(),
            "final_program_balances": "0" }),
            ),
            "multiasset conservation evidence",
        ),
        0o600,
    );
    println!("REGISTRY_MULTI_ASSET_CASE exit-conservation");
}
