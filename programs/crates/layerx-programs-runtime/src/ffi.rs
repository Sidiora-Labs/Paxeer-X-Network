//! Scalar-only, activity-owned C ingress for migration validation.
//!
//! C owns the bounded source bytes in the current module-context arena. Rust
//! reads them through one scalar callback and reconstructs transient local
//! input; only the hash-bound compiled artifact may outlive the call.

use crate::{Executor, ModuleCacheKey, RuntimeArtifactOwnerRefusal};

const RESULT_OK: i32 = 0;
const RESULT_NON_CANONICAL: i32 = -3;
const RESULT_LENGTH_LIMIT: i32 = -5;
const RESULT_UNKNOWN_ACTIVITY: i32 = -106;
const RESULT_GAS_EXHAUSTED: i32 = -601;
const RESULT_FATAL_INVARIANT: i32 = -1001;
const MAX_MODULE_BYTES: usize = 1_048_576;
const MAX_HOOK_BYTES: usize = 1_024;
const WASM_SECTION: u16 = 0;
const HOOK_SECTION: u16 = 1;
const MIGRATION_ADMISSION_BYTES: usize = 140;

#[no_mangle]
pub extern "C" fn layerx_programs_migration_runtime_version() -> u16 {
    crate::RUNTIME_VERSION
}

unsafe extern "C" {
    /// Returns one C-owned activity byte as `0..=255`, or a negative `LayerX`
    /// refusal. The token and bytes are valid only for this synchronous call.
    fn layerx_programs_migration_activity_byte(token: u64, section: u16, offset: u32) -> i32;
    fn layerx_programs_migration_admission_byte(token: u64, offset: u32) -> i32;
    fn layerx_programs_migration_usage_commit(
        token: u64,
        cpu: u64,
        memory: u64,
        read: u64,
        write: u64,
        output_values: u32,
        output_bytes: u64,
        fee_hi: u64,
        fee_lo: u64,
    ) -> i32;
}

fn migration_executor(token: u64, abi_version: u16) -> Result<(Executor, bool), i32> {
    let mut bytes = [0_u8; MIGRATION_ADMISSION_BYTES];
    for (offset, byte) in bytes.iter_mut().enumerate() {
        let value = unsafe { layerx_programs_migration_admission_byte(token, offset as u32) };
        *byte = u8::try_from(value).map_err(|_| {
            if value < 0 {
                value
            } else {
                RESULT_NON_CANONICAL
            }
        })?;
    }
    let u32_at = |offset| {
        u32::from_be_bytes(
            bytes[offset..offset + 4]
                .try_into()
                .expect("bounded admission field"),
        )
    };
    let u16_at = |offset| {
        u16::from_be_bytes(
            bytes[offset..offset + 2]
                .try_into()
                .expect("bounded admission field"),
        )
    };
    let u64_at = |offset| {
        u64::from_be_bytes(
            bytes[offset..offset + 8]
                .try_into()
                .expect("bounded admission field"),
        )
    };
    let profile = u32_at(0);
    let price_version = u32_at(4);
    let runtime = u16_at(8);
    let admitted_abi = u16_at(10);
    let limits = std::array::from_fn::<_, 7, _>(|index| u64_at(12 + index * 8));
    if runtime != crate::RUNTIME_VERSION
        || admitted_abi != abi_version
        || limits
            != [
                1_000_000, 16_777_216, 1_048_576, 1_048_576, 64, 1_048_576, 4096,
            ]
    {
        return Err(RESULT_NON_CANONICAL);
    }
    if profile == 0 {
        if runtime != 1 {
            return Err(RESULT_NON_CANONICAL);
        }
        return Ok((Executor::legacy_migration_executor(), true));
    }
    if profile != 1 {
        return Err(RESULT_NON_CANONICAL);
    }
    let budget = crate::ResourceBudget::new_complete(
        limits[0],
        limits[1],
        limits[2],
        limits[3],
        u32::try_from(limits[4]).map_err(|_| RESULT_LENGTH_LIMIT)?,
        limits[5],
        u32::try_from(limits[6]).map_err(|_| RESULT_LENGTH_LIMIT)?,
    );
    let prices = crate::FeeSchedule::new_complete(crate::FeeScheduleParameters {
        version: price_version,
        fee_units_per_cpu_fuel: u64_at(68),
        fee_units_per_memory_byte: u64_at(76),
        fee_units_per_storage_read_byte: u64_at(84),
        fee_units_per_storage_write_byte: u64_at(92),
        fee_units_per_output_value: u64_at(100),
        fee_units_per_output_byte: u64_at(108),
        fee_units_per_occupancy_byte_batch: u64_at(116),
    });
    if !prices.is_valid() {
        return Err(RESULT_NON_CANONICAL);
    }
    let coverage = u128::from_be_bytes(bytes[124..140].try_into().expect("bounded coverage"));
    let maximum = crate::budget::maximum_fee_units(budget, prices).map_err(|_| -500)?;
    if maximum > coverage {
        return Err(-600);
    }
    Ok((
        Executor::new_versioned(budget, prices, runtime, admitted_abi),
        false,
    ))
}

fn activity_bytes(token: u64, section: u16, length: usize) -> Result<Vec<u8>, i32> {
    let length_u32 = u32::try_from(length).map_err(|_| RESULT_LENGTH_LIMIT)?;
    let mut bytes = Vec::with_capacity(length);
    for offset in 0..length_u32 {
        // The C boundary returns only a scalar byte or typed refusal; it never
        // shares a pointer, and the token is owned by the active C context.
        let value = unsafe { layerx_programs_migration_activity_byte(token, section, offset) };
        let byte = u8::try_from(value).map_err(|_| {
            if value < 0 {
                value
            } else {
                RESULT_NON_CANONICAL
            }
        })?;
        bytes.push(byte);
    }
    Ok(bytes)
}

/// Validates and executes one C-owned migration request under declared
/// metering. The activity token is transient and cannot outlive its module
/// context; this bridge owns no pending state.
#[no_mangle]
pub extern "C" fn layerx_programs_migration_execute_activity(
    token: u64,
    wasm_length: u32,
    hook_length: u16,
    abi_version: u16,
    metering_schedule_version: u32,
    meter_base: u64,
    meter_entity: u64,
    meter_load: u64,
    meter_store: u64,
    meter_call: u64,
    meter_branch_kept_per_fuel: u64,
    meter_func_locals_per_fuel: u64,
    meter_memory_bytes_per_fuel: u64,
    meter_table_elements_per_fuel: u64,
    h0: u64,
    h1: u64,
    h2: u64,
    h3: u64,
) -> i32 {
    let wasm_length = wasm_length as usize;
    let hook_length = hook_length as usize;
    if token == 0
        || wasm_length == 0
        || wasm_length > MAX_MODULE_BYTES
        || hook_length == 0
        || hook_length > MAX_HOOK_BYTES
        || metering_schedule_version == 0
    {
        return RESULT_NON_CANONICAL;
    }
    let wasm = match activity_bytes(token, WASM_SECTION, wasm_length) {
        Ok(wasm) => wasm,
        Err(refusal) => return refusal,
    };
    let hook = match activity_bytes(token, HOOK_SECTION, hook_length) {
        Ok(hook) => hook,
        Err(refusal) => return refusal,
    };
    let Ok(hook) = String::from_utf8(hook) else {
        return RESULT_NON_CANONICAL;
    };
    let mut code_hash = [0_u8; 32];
    for (chunk, word) in code_hash.chunks_exact_mut(8).zip([h0, h1, h2, h3]) {
        chunk.copy_from_slice(&word.to_be_bytes());
    }
    let Ok(owner) = crate::cache::runtime_artifacts() else {
        return RESULT_FATAL_INVARIANT;
    };
    let mut schedule_bytes = [0_u8; 76];
    schedule_bytes[..4].copy_from_slice(&metering_schedule_version.to_be_bytes());
    for (index, coefficient) in [
        meter_base,
        meter_entity,
        meter_load,
        meter_store,
        meter_call,
        meter_branch_kept_per_fuel,
        meter_func_locals_per_fuel,
        meter_memory_bytes_per_fuel,
        meter_table_elements_per_fuel,
    ]
    .into_iter()
    .enumerate()
    {
        let start = 4 + index * 8;
        schedule_bytes[start..start + 8].copy_from_slice(&coefficient.to_be_bytes());
    }
    let Ok(schedule) = crate::FuelSchedule::from_protocol_bytes(&schedule_bytes) else {
        return RESULT_NON_CANONICAL;
    };
    let Ok(cache_key) = ModuleCacheKey::for_wasm_with_schedule(
        code_hash,
        crate::RUNTIME_VERSION,
        abi_version,
        &wasm,
        schedule,
    ) else {
        return RESULT_NON_CANONICAL;
    };
    let module = match owner.get_or_compile(cache_key, &wasm) {
        Ok(module) => module,
        Err(RuntimeArtifactOwnerRefusal::Compilation(_)) => return RESULT_NON_CANONICAL,
        Err(
            RuntimeArtifactOwnerRefusal::Initialization(_)
            | RuntimeArtifactOwnerRefusal::SynchronizationPoisoned,
        ) => return RESULT_FATAL_INVARIANT,
    };
    let (executor, legacy) = match migration_executor(token, abi_version) {
        Ok(executor) => executor,
        Err(refusal) => return refusal,
    };
    let execution = if legacy {
        Executor::execute_legacy_migration(module.validated(), &hook, schedule)
    } else {
        executor.execute_migration(module.validated(), &hook, abi_version, schedule)
    };
    match execution {
        Ok(record) => {
            if legacy {
                return RESULT_OK;
            }
            let usage = record.usage;
            if usage.occupancy_byte_batches != 0 || usage.occupancy_fee_units != 0 {
                return RESULT_FATAL_INVARIANT;
            }
            unsafe {
                layerx_programs_migration_usage_commit(
                    token,
                    usage.cpu_fuel,
                    usage.memory_bytes,
                    usage.storage_read_bytes,
                    usage.storage_write_bytes,
                    usage.output_values,
                    usage.output_bytes,
                    (usage.fee_units >> 64) as u64,
                    usage.fee_units as u64,
                )
            }
        }
        Err(crate::ExecutionError::Resource(_)) => RESULT_GAS_EXHAUSTED,
        Err(crate::ExecutionError::Fault(crate::ExecutionFault::UnknownExport { .. })) => {
            RESULT_UNKNOWN_ACTIVITY
        }
        Err(_) => RESULT_NON_CANONICAL,
    }
}

#[no_mangle]
pub extern "C" fn layerx_programs_deployment_validate(
    token: u64,
    wasm_length: u32,
    abi_version: u16,
    metering_schedule_version: u32,
    meter_base: u64,
    meter_entity: u64,
    meter_load: u64,
    meter_store: u64,
    meter_call: u64,
    meter_branch_kept_per_fuel: u64,
    meter_func_locals_per_fuel: u64,
    meter_memory_bytes_per_fuel: u64,
    meter_table_elements_per_fuel: u64,
) -> i32 {
    if token == 0 || wasm_length == 0 || wasm_length as usize > MAX_MODULE_BYTES {
        return RESULT_NON_CANONICAL;
    }
    let wasm = match activity_bytes(token, WASM_SECTION, wasm_length as usize) {
        Ok(wasm) => wasm,
        Err(refusal) => return refusal,
    };
    let mut schedule_bytes = [0_u8; 76];
    schedule_bytes[..4].copy_from_slice(&metering_schedule_version.to_be_bytes());
    for (index, coefficient) in [
        meter_base,
        meter_entity,
        meter_load,
        meter_store,
        meter_call,
        meter_branch_kept_per_fuel,
        meter_func_locals_per_fuel,
        meter_memory_bytes_per_fuel,
        meter_table_elements_per_fuel,
    ]
    .into_iter()
    .enumerate()
    {
        let start = 4 + index * 8;
        schedule_bytes[start..start + 8].copy_from_slice(&coefficient.to_be_bytes());
    }
    let Ok(schedule) = crate::FuelSchedule::from_protocol_bytes(&schedule_bytes) else {
        return RESULT_NON_CANONICAL;
    };
    let Ok(engine) = crate::WasmEngine::declared() else {
        return RESULT_FATAL_INVARIANT;
    };
    match engine.validate_deployment_versioned_metered(abi_version, &wasm, schedule) {
        Ok(_) => RESULT_OK,
        Err(crate::ValidationRefusal::UnsupportedAbiVersion { .. }) => -101,
        Err(_) => RESULT_NON_CANONICAL,
    }
}
