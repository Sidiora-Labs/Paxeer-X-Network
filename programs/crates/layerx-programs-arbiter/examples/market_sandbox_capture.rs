use layerx_program_sdk::arbiter::{
    MarketBillingCommitment, MarketSandboxProfile, ProfileLimits, MARKET_SANDBOX_BILLING_CAPACITY,
    MARKET_SANDBOX_PROFILE_CAPACITY,
};
use layerx_programs_runtime::execute::{
    instantiate_market_sandbox_untrusted, market_sandbox_input_digest, observe_market_sandbox_step,
    MarketSandboxExecution, MarketSandboxRequest,
};
use layerx_programs_runtime::portable_replay::{
    replay_leaf_hash, replay_node_hash, PortableBoundary,
};
use layerx_programs_runtime::replay::{
    market_sandbox_baseline_root, market_sandbox_namespace, MarketSandboxReplayAuthority,
};
use layerx_programs_runtime::test_support::{
    code_section, export_section, func_body, function_section, module, type_section, OP_END,
    OP_I32_CONST, TYPE_I32,
};
use layerx_programs_runtime::{
    FeeSchedule, PrincipalId, ProgramId, ProgramReplayProfile, ResourceBudget, Storage, WasmEngine,
    WasmValue, RUNTIME_VERSION,
};
use serde_json::{json, Value};
use sha2::{Digest, Sha256};
use std::{error::Error, fs, path::Path};

type Result<T> = std::result::Result<T, Box<dyn Error>>;
const MAXIMUM: usize = 1_048_576;

trait Captured<T> {
    fn captured(self) -> Result<T>;
}

impl<T, E: std::fmt::Debug> Captured<T> for std::result::Result<T, E> {
    fn captured(self) -> Result<T> {
        self.map_err(|error| format!("{error:?}").into())
    }
}

fn hex(bytes: &[u8]) -> String {
    bytes.iter().map(|byte| format!("{byte:02x}")).collect()
}

fn hash(object: &Value, key: &str) -> Result<[u8; 32]> {
    let value = object[key]
        .as_str()
        .ok_or_else(|| format!("missing native {key}"))
        .captured()?;
    if value.len() != 64 {
        return Err(format!("invalid native {key} width").into());
    }
    let mut result = [0; 32];
    for (index, byte) in result.iter_mut().enumerate() {
        *byte = u8::from_str_radix(&value[2 * index..2 * index + 2], 16).captured()?;
    }
    if result == [0; 32] {
        return Err(format!("zero native {key}").into());
    }
    Ok(result)
}

fn number(object: &Value, key: &str) -> Result<u64> {
    object[key]
        .as_u64()
        .ok_or_else(|| format!("missing native {key}").into())
}

fn write(directory: &Path, name: &str, bytes: &[u8]) -> Result<String> {
    fs::write(directory.join(name), bytes).captured()?;
    Ok(name.to_owned())
}

fn load(path: &Path) -> Result<Value> {
    Ok(serde_json::from_slice(&fs::read(path).captured()?).captured()?)
}

fn modules(directory: &Path) -> Result<()> {
    fs::create_dir_all(directory).captured()?;
    for name in ["integer", "trap", "incorrect"] {
        let body = if name == "trap" {
            vec![0, OP_END]
        } else {
            vec![OP_I32_CONST, 7, OP_I32_CONST, 5, 0x6a, OP_END]
        };
        let wasm = module(&[
            type_section(&[(&[], &[TYPE_I32])]),
            function_section(&[0]),
            export_section(&[("compute", 0)]),
            code_section(&[func_body(&[], &body)]),
        ]);
        write(directory, &format!("{name}.wasm"), &wasm).captured()?;
    }
    Ok(())
}

fn execute(
    module: &layerx_programs_runtime::ValidatedModule,
    authority: &MarketSandboxReplayAuthority,
) -> Result<MarketSandboxExecution> {
    let mut instance = instantiate_market_sandbox_untrusted(module, authority).captured()?;
    Ok(instance
        .call_market_sandbox_untrusted(MarketSandboxRequest {
            module,
            entrypoint: "compute",
            args: &[],
            authority,
            replay_profile: ProgramReplayProfile::new(128, MAXIMUM as u32).captured()?,
        })
        .captured()?)
}

fn merkle(leaves: &[Vec<u8>]) -> Result<([u8; 32], Vec<Vec<[u8; 32]>>)> {
    if leaves.len() < 2 || leaves.len() > 128 {
        return Err("runtime boundary count outside profile".into());
    }
    let mut level = leaves
        .iter()
        .enumerate()
        .map(|(index, leaf)| replay_leaf_hash(u32::try_from(index).captured()?, leaf).captured())
        .collect::<Result<Vec<_>>>()
        .captured()?;
    let mut paths = vec![Vec::new(); leaves.len()];
    let mut positions: Vec<usize> = (0..leaves.len()).collect();
    while level.len() > 1 {
        for (path, position) in paths.iter_mut().zip(&mut positions) {
            let sibling = if *position % 2 == 0 {
                (*position + 1).min(level.len() - 1)
            } else {
                *position - 1
            };
            path.push(level[sibling]);
            *position /= 2;
        }
        level = level
            .chunks(2)
            .map(|pair| replay_node_hash(pair[0], *pair.get(1).unwrap_or(&pair[0])))
            .collect();
    }
    Ok((level[0], paths))
}

fn proof(
    directory: &Path,
    name: &str,
    index: usize,
    leaves: &[Vec<u8>],
    paths: &[Vec<[u8; 32]>],
) -> Result<Value> {
    let leaf = write(
        directory,
        &format!("{name}-boundary-{index}.bin"),
        &leaves[index],
    )
    .captured()?;
    Ok(
        json!({"index": index, "leaf": leaf, "siblings": paths[index].iter().map(|digest| json!({"sha256": hex(digest)})).collect::<Vec<_>>() }),
    )
}

fn prepare(directory: &Path, setup_path: &Path) -> Result<()> {
    let setup = load(setup_path).captured()?;
    let network_id = u32::try_from(number(&setup, "network_id").captured()?).captured()?;
    let market_program = hash(&setup, "market_program").captured()?;
    let provider = hash(&setup, "provider").captured()?;
    let tenant = hash(&setup, "tenant").captured()?;
    let payment_account = if setup.get("payment_account").is_some() {
        hash(&setup, "payment_account").captured()?
    } else {
        tenant
    };
    let engine = WasmEngine::declared().captured()?;
    let mut cases = Vec::new();
    let native_cases = setup["cases"]
        .as_array()
        .ok_or("missing native setup cases")
        .captured()?;
    if native_cases.len() != 3 {
        return Err("three genuine Market setup cases required".into());
    }
    for case in native_cases {
        let name = case["name"]
            .as_str()
            .ok_or("missing native case name")
            .captured()?;
        if !["integer", "trap", "incorrect"].contains(&name)
            || cases
                .iter()
                .any(|previous: &Value| previous["name"] == name)
        {
            return Err("closed distinct runtime cases required".into());
        }
        let wasm = fs::read(directory.join(format!("{name}.wasm"))).captured()?;
        let validated = engine.validate_versioned(2, &wasm).captured()?;
        let sandbox_program = hash(case, "sandbox_program").captured()?;
        let program = ProgramId::new(sandbox_program).captured()?;
        let lease = hash(case, "lease_id").captured()?;
        let baseline = Storage::new();
        let budget = ResourceBudget::new_complete(100_000, 65_536, 1024, 1024, 1, 64, 0);
        let fees = FeeSchedule::declared();
        let mut authority = MarketSandboxReplayAuthority {
            profile_binding: Sha256::digest(b"LayerX/genuine-market-profile-sizing/v1\0").into(),
            namespace: market_sandbox_namespace(program, lease).captured()?,
            lease_id: lease,
            namespace_limit: 1024,
            program,
            tenant: PrincipalId::new(tenant).captured()?,
            payment_account,
            code_hash: validated.code_hash(),
            input_digest: market_sandbox_input_digest("compute", &[]).captured()?,
            runtime_version: RUNTIME_VERSION,
            abi_version: 2,
            fee_schedule_version: fees.version(),
            metering_schedule_version: validated.metering_schedule_version(),
            budget,
            fees,
            fee_budget: 1_000_000_000,
            baseline_state_root: market_sandbox_baseline_root(&baseline).captured()?,
            baseline_storage: baseline.clone(),
        };
        let sizing = execute(&validated, &authority).captured()?;
        let mut entrypoint_bytes = [0; 64];
        entrypoint_bytes[..7].copy_from_slice(b"compute");
        let profile = MarketSandboxProfile {
            network_id,
            market_program,
            sandbox_program,
            offer_id: hash(case, "offer_id").captured()?,
            lease_id: lease,
            claim_id: hash(case, "claim_id").captured()?,
            provider,
            tenant,
            code_hash: authority.code_hash,
            input_digest: authority.input_digest,
            attested_input_commitment: hash(case, "attested_input_commitment").captured()?,
            namespace: authority.namespace,
            baseline_state_root: authority.baseline_state_root,
            initial_execution_state_root: sizing.initial_commitment.digest,
            entrypoint_bytes,
            entrypoint_length: 7,
            runtime_version: RUNTIME_VERSION,
            abi_version: 2,
            fee_schedule_version: authority.fee_schedule_version,
            metering_schedule_version: authority.metering_schedule_version,
            limits: ProfileLimits {
                cpu_fuel: budget.cpu_fuel(),
                memory_bytes: budget.memory_bytes(),
                storage_read_bytes: budget.storage_read_bytes(),
                storage_write_bytes: budget.storage_write_bytes(),
                output_values: u64::from(budget.output_values()),
                output_bytes: budget.output_bytes(),
                table_elements: u64::from(budget.table_elements()),
                namespace_bytes: authority.namespace_limit,
            },
            fee_budget: authority.fee_budget,
            interval_start: number(case, "interval_start").captured()?,
            interval_end: number(case, "interval_end").captured()?,
            response_deadline: number(case, "response_deadline").captured()?,
            maximum_boundaries: 128,
            maximum_bytes: MAXIMUM as u32,
        };
        let mut profile_bytes = [0; MARKET_SANDBOX_PROFILE_CAPACITY];
        let profile_length = profile.encode(&mut profile_bytes).captured()?;
        authority.profile_binding = Sha256::digest(&profile_bytes[..profile_length]).into();
        let execution = execute(&validated, &authority).captured()?;
        if execution.initial_commitment.digest != profile.initial_execution_state_root
            || execution.record.boundary_root() != sizing.record.boundary_root()
        {
            return Err("profile binding changed genuine captured execution".into());
        }
        if (name == "trap") != execution.terminal_fault.is_some()
            || (name != "trap" && execution.values != vec![WasmValue::I32(12)])
        {
            return Err("unexpected real sandbox integer/trap outcome".into());
        }
        let captured = &execution.boundary_leaves;
        let (captured_root, _) = merkle(captured).captured()?;
        if captured_root != execution.record.boundary_root()
            || captured.len() != execution.record.boundary_count() as usize
        {
            return Err("runtime record and genuine captured leaves disagree".into());
        }
        let last = captured.len() - 1;
        let mut claimed = captured.clone();
        let (pre_index, post_index) = if name == "trap" {
            let terminal =
                PortableBoundary::decode_untrusted(&captured[last], MAXIMUM).captured()?;
            if observe_market_sandbox_step(&validated, &terminal, &authority, MAXIMUM)
                .captured()?
                .trap
                .is_none()
            {
                return Err("terminal boundary did not reproduce a real trap".into());
            }
            (last, last)
        } else {
            let mut selected = None;
            for index in 0..last {
                if name == "incorrect" && index + 1 == last {
                    continue;
                }
                let pre =
                    PortableBoundary::decode_untrusted(&captured[index], MAXIMUM).captured()?;
                let mut post =
                    PortableBoundary::decode_untrusted(&captured[index + 1], MAXIMUM).captured()?;
                if let Ok(observed) =
                    observe_market_sandbox_step(&validated, &pre, &authority, MAXIMUM)
                {
                    if observed.trap.is_none() {
                        post.trap = None;
                    }
                    if observed.reencode_untrusted(MAXIMUM).captured()?
                        == post.reencode_untrusted(MAXIMUM).captured()?
                    {
                        selected = Some((index, index + 1));
                        break;
                    }
                }
            }
            let pair = selected
                .ok_or("no actual replayable adjacent integer instruction")
                .captured()?;
            if name == "incorrect" {
                claimed[pair.1] = captured[pair.0].clone();
                let pre =
                    PortableBoundary::decode_untrusted(&claimed[pair.0], MAXIMUM).captured()?;
                let observed = observe_market_sandbox_step(&validated, &pre, &authority, MAXIMUM)
                    .captured()?;
                let mut post =
                    PortableBoundary::decode_untrusted(&claimed[pair.1], MAXIMUM).captured()?;
                if observed.trap.is_none() {
                    post.trap = None;
                }
                if observed.reencode_untrusted(MAXIMUM).captured()?
                    == post.reencode_untrusted(MAXIMUM).captured()?
                {
                    return Err(
                        "provider negative did not commit an incorrect actual transition".into(),
                    );
                }
            }
            pair
        };
        if claimed.first() != captured.first() || claimed.last() != captured.last() {
            return Err("provider claim changed authenticated initial/final boundaries".into());
        }
        let (provider_root, paths) = merkle(&claimed).captured()?;
        if name != "incorrect" && provider_root != execution.record.boundary_root() {
            return Err("positive provider root differs from actual runtime record".into());
        }
        let usage = execution.usage;
        let billing_height = number(case, "billing_height").captured()?;
        let successful_output = 12i32.to_be_bytes();
        let billing = MarketBillingCommitment {
            profile_digest: authority.profile_binding,
            provider_trace_root: provider_root,
            final_execution_state_root: execution.final_commitment.digest,
            output_digest: Sha256::digest(if name == "trap" {
                b"trap".as_slice()
            } else {
                &successful_output
            })
            .into(),
            usage: [
                usage.cpu_fuel,
                usage.memory_bytes,
                usage.storage_read_bytes,
                usage.storage_write_bytes,
                u64::from(usage.output_values),
                usage.output_bytes,
            ],
            payable: usage.fee_units,
            challenger_stake: 1,
            challenge_window_batches: profile
                .response_deadline
                .checked_sub(billing_height)
                .ok_or("invalid actual billing height")
                .captured()?,
            boundary_count: u32::try_from(claimed.len()).captured()?,
        };
        let mut billing_bytes = [0; MARKET_SANDBOX_BILLING_CAPACITY];
        let billing_length = billing.encode(&mut billing_bytes).captured()?;
        let profile_path = write(
            directory,
            &format!("{name}-profile.bin"),
            &profile_bytes[..profile_length],
        )
        .captured()?;
        let billing_path = write(
            directory,
            &format!("{name}-billing.bin"),
            &billing_bytes[..billing_length],
        )
        .captured()?;
        let baseline_storage = write(
            directory,
            &format!("{name}-baseline.bin"),
            &baseline.replay_state_bytes(MAXIMUM).captured()?,
        )
        .captured()?;
        let input = write(directory, &format!("{name}-input.bin"), &[]).captured()?;
        let captured_paths = captured
            .iter()
            .enumerate()
            .map(|(index, leaf)| write(directory, &format!("{name}-captured-{index}.bin"), leaf))
            .collect::<Result<Vec<_>>>()
            .captured()?;
        cases.push(json!({"name": name, "kind": if name == "trap" {"trap"} else {"integer"},
            "expected": if name == "incorrect" {"incorrect"} else {"correct"},
            "wasm_path": format!("{name}.wasm"), "profile_path": profile_path, "billing_path": billing_path,
            "baseline_storage": baseline_storage, "input": input, "baseline_namespace": null,
            "captured_boundary_root": hex(&captured_root), "provider_trace_root": hex(&provider_root),
            "captured_boundaries": captured_paths, "provider_commits_incorrect_transition": name == "incorrect",
            "initial": proof(directory, name, 0, &claimed, &paths).captured()?, "final": proof(directory, name, last, &claimed, &paths).captured()?,
            "pre": proof(directory, name, pre_index, &claimed, &paths).captured()?, "post": proof(directory, name, post_index, &claimed, &paths).captured()?}));
    }
    fs::write(
        directory.join("runtime-inputs.json"),
        serde_json::to_vec_pretty(
            &json!({"version":1,"market_program":hex(&market_program),"cases":cases}),
        )
        .captured()?,
    )
    .captured()?;
    Ok(())
}

fn finish(directory: &Path) -> Result<()> {
    let mut native = load(&directory.join("market-native.json")).captured()?;
    let runtime = load(&directory.join("runtime-inputs.json")).captured()?;
    if native["market_program"] != runtime["market_program"] {
        return Err("native and runtime Market identity mismatch".into());
    }
    let native_cases = native["cases"]
        .as_array_mut()
        .ok_or("missing actual signed Market cases")
        .captured()?;
    if native_cases.len() != 3 {
        return Err("three actual signed Market cases required".into());
    }
    for case in native_cases {
        let name = case["name"]
            .as_str()
            .ok_or("missing signed case name")
            .captured()?
            .to_owned();
        let captured = runtime["cases"]
            .as_array()
            .ok_or("missing real runtime cases")
            .captured()?
            .iter()
            .find(|candidate| candidate["name"] == name)
            .ok_or("signed case has no runtime capture")
            .captured()?;
        for field in [
            "tenant_receipt",
            "provider_receipt",
            "tenant_namespace",
            "provider_namespace",
        ] {
            if case.get(field).is_none() {
                return Err(format!("missing actual signed {field}").into());
            }
        }
        for (key, value) in captured
            .as_object()
            .ok_or("invalid runtime case")
            .captured()?
        {
            if key == "baseline_namespace" && case.get(key).is_some() {
                continue;
            }
            case.as_object_mut()
                .ok_or("invalid signed case")
                .captured()?
                .insert(key.clone(), value.clone());
        }
    }
    fs::write(
        directory.join("market-inputs.json"),
        serde_json::to_vec_pretty(&native).captured()?,
    )
    .captured()?;
    Ok(())
}

fn main() -> Result<()> {
    let args: Vec<String> = std::env::args().collect();
    let command = args
        .get(1)
        .ok_or("usage: market_sandbox_capture modules|prepare|finish DIRECTORY [SETUP_JSON]")
        .captured()?;
    let directory = Path::new(args.get(2).ok_or("missing capture directory").captured()?);
    match command.as_str() {
        "modules" if args.len() == 3 => modules(directory),
        "prepare" if args.len() == 4 => prepare(directory, Path::new(&args[3])),
        "finish" if args.len() == 3 => finish(directory),
        _ => Err("invalid capture command or arguments".into()),
    }
}
