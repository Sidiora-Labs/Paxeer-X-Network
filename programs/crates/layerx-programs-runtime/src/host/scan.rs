//! Candidate-V2 bounded, resumable storage-scan host function.

use wasmi::{Caller, Linker};

use crate::abi::response::CANDIDATE_ABI_MODULE;
use crate::abi::StorageSelector;
use crate::execute::ExecutionFault;
use crate::storage::{ScanLimits, MAX_STORAGE_KEY_BYTES, MAX_STORAGE_SCAN_CURSOR_BYTES};

use super::memory::{nonnegative, read_guest, validate_output};
use super::{error_status, linker_fault, RuntimeState, STATUS_BOUNDS};

fn selector(raw: i32) -> Result<StorageSelector, i32> {
    StorageSelector::try_from(raw).map_err(|error| error_status(&error))
}

/// Registers `storage_scan_scoped` without changing the frozen V1 ABI.
pub(super) fn register_v2(linker: &mut Linker<RuntimeState>) -> Result<(), ExecutionFault> {
    linker
        .func_wrap(
            CANDIDATE_ABI_MODULE,
            "storage_scan_scoped",
            |mut caller: Caller<'_, RuntimeState>,
             raw_selector: i32,
             prefix_pointer: i32,
             prefix_length: i32,
             cursor_pointer: i32,
             cursor_length: i32,
             max_entries: i32,
             max_bytes: i32,
             output_pointer: i32,
             output_capacity: i32|
             -> i32 {
                let selected = match selector(raw_selector) {
                    Ok(selected) => selected,
                    Err(status) => return status,
                };
                let output = match validate_output(&caller, output_pointer, output_capacity) {
                    Ok(output) => output,
                    Err(status) => return status,
                };
                let prefix = match read_guest(
                    &caller,
                    prefix_pointer,
                    prefix_length,
                    MAX_STORAGE_KEY_BYTES,
                ) {
                    Ok(prefix) => prefix,
                    Err(status) => return status,
                };
                let cursor = match read_guest(
                    &caller,
                    cursor_pointer,
                    cursor_length,
                    MAX_STORAGE_SCAN_CURSOR_BYTES,
                ) {
                    Ok(cursor) => cursor,
                    Err(status) => return status,
                };
                let max_entries = match nonnegative(max_entries)
                    .and_then(|value| u32::try_from(value).map_err(|_| STATUS_BOUNDS))
                {
                    Ok(value) => value,
                    Err(status) => return status,
                };
                let max_bytes = match nonnegative(max_bytes)
                    .and_then(|value| u32::try_from(value).map_err(|_| STATUS_BOUNDS))
                {
                    Ok(value) => value,
                    Err(status) => return status,
                };
                let limits = match ScanLimits::new(max_entries, max_bytes) {
                    Ok(limits) => limits,
                    Err(error) => return error_status(&error.into()),
                };
                let page = match caller
                    .data_mut()
                    .with_abi(|abi, _| abi.storage_scan_preview(selected, &prefix, &cursor, limits))
                {
                    Ok(page) => page,
                    Err(error) => return error_status(&error),
                };
                let encoded = match page.encode_for_guest() {
                    Ok(encoded) => encoded,
                    Err(error) => return error_status(&error.into()),
                };
                if encoded.len() > output.capacity() {
                    return STATUS_BOUNDS;
                }
                if let Err(error) = caller
                    .data_mut()
                    .with_abi(|abi, meter| abi.charge_storage_scan(meter, selected, &page))
                {
                    return error_status(&error);
                }
                if let Err(status) = output.write(&mut caller, &encoded) {
                    return status;
                }
                i32::try_from(encoded.len()).unwrap_or(STATUS_BOUNDS)
            },
        )
        .map_err(|error| linker_fault(&error))?;
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::super::{linker, RuntimeState};
    use crate::abi::response::CANDIDATE_ABI_MODULE;
    use crate::abi::{Abi, AuthorizationContext, Capability, CapabilitySet};
    use crate::storage::{PrincipalId, ProgramId, ScanLimits, Storage, StorageNamespace};
    use crate::test_support::{
        code_section, func_body, function_section, import_section, module, raw_section,
        type_section, unsigned_leb, TYPE_I32,
    };
    use crate::{
        FuelSchedule, Meter, MeteredUsage, ProgramInstance, UnavailableReceiptOracle,
        ValidationLimits, WasmEngine, WasmValue, CALL_ENTRY_EXPORT,
    };

    const OUTPUT_POINTER: i32 = 1024;
    const OUTPUT_CAPACITY: i32 = 4096;
    const CURSOR_POINTER: i32 = 64;
    const ENTRIES: [(&[u8], &[u8]); 7] = [
        (b"k/03", b"ccc"),
        (b"k/01", b"a"),
        (b"k/10", b"dddddddd"),
        (b"k/02", b"bb"),
        (b"j", b"x"),
        (b"k/", b"root"),
        (b"l", b"y"),
    ];

    #[derive(Debug, PartialEq)]
    struct Observation {
        outputs: Vec<WasmValue>,
        usage: MeteredUsage,
        state: Vec<(Vec<u8>, Vec<u8>)>,
    }

    fn signed_leb(mut value: i32) -> Vec<u8> {
        let mut bytes = Vec::new();
        loop {
            let byte = value.to_le_bytes()[0] & 0x7f;
            value >>= 7;
            let done = (value == 0 && byte & 0x40 == 0) || (value == -1 && byte & 0x40 != 0);
            bytes.push(if done { byte } else { byte | 0x80 });
            if done {
                return bytes;
            }
        }
    }

    fn length(bytes: &[u8]) -> i32 {
        i32::try_from(bytes.len()).unwrap_or_else(|error| panic!("length: {error}"))
    }

    fn data_segment(payload: &mut Vec<u8>, offset: i32, bytes: &[u8]) {
        payload.extend_from_slice(&[0, 0x41]);
        payload.extend(signed_leb(offset));
        payload.push(0x0b);
        payload.extend(unsigned_leb(bytes.len() as u64));
        payload.extend_from_slice(bytes);
    }

    // `layerx_call` scans one page into guest memory and folds the returned
    // status and every delivered byte into a wrapping base-31 checksum, so the
    // engine outputs expose the exact bytes each tier delivered to the guest.
    fn checksum_guest(prefix: &[u8], cursor: &[u8], limits: ScanLimits) -> Vec<u8> {
        let types = type_section(&[
            (&[TYPE_I32; 9], &[TYPE_I32]),
            (&[TYPE_I32], &[TYPE_I32]),
            (&[TYPE_I32, TYPE_I32], &[TYPE_I32]),
        ]);
        let imports = import_section(&[(CANDIDATE_ABI_MODULE, "storage_scan_scoped", 0)]);
        let mut exports = unsigned_leb(3);
        for (name, kind, index) in [
            ("layerx_reserve", 0u8, 1u8),
            ("layerx_call", 0, 2),
            ("memory", 2, 0),
        ] {
            exports.extend(unsigned_leb(name.len() as u64));
            exports.extend_from_slice(name.as_bytes());
            exports.extend_from_slice(&[kind, index]);
        }
        let mut entry = Vec::new();
        for value in [
            1,
            0,
            length(prefix),
            CURSOR_POINTER,
            length(cursor),
            i32::try_from(limits.max_entries()).unwrap_or_else(|error| panic!("{error}")),
            i32::try_from(limits.max_bytes()).unwrap_or_else(|error| panic!("{error}")),
            OUTPUT_POINTER,
            OUTPUT_CAPACITY,
        ] {
            entry.push(0x41);
            entry.extend(signed_leb(value));
        }
        entry.extend_from_slice(&[0x10, 0, 0x22, 3, 0x21, 4]);
        entry.extend_from_slice(&[0x02, 0x40, 0x03, 0x40]);
        entry.extend_from_slice(&[0x20, 2, 0x20, 3, 0x4e, 0x0d, 1]);
        entry.extend_from_slice(&[0x20, 4, 0x41, 31, 0x6c, 0x20, 2, 0x2d, 0]);
        entry.extend(unsigned_leb(
            u64::try_from(OUTPUT_POINTER).unwrap_or_else(|error| panic!("{error}")),
        ));
        entry.extend_from_slice(&[0x6a, 0x21, 4]);
        entry.extend_from_slice(&[0x20, 2, 0x41, 1, 0x6a, 0x21, 2, 0x0c, 0]);
        entry.extend_from_slice(&[0x0b, 0x0b, 0x20, 4, 0x0b]);
        let mut data = unsigned_leb(2);
        data_segment(&mut data, 0, prefix);
        data_segment(&mut data, CURSOR_POINTER, cursor);
        module(&[
            types,
            imports,
            function_section(&[1, 2]),
            raw_section(5, &[1, 1, 1, 1]),
            raw_section(7, &exports),
            code_section(&[
                func_body(&[], &[0x41, 0, 0x0b]),
                func_body(&[(3, TYPE_I32)], &entry),
            ]),
            raw_section(11, &data),
        ])
    }

    fn checksum(status: i32, delivered: &[u8]) -> i32 {
        delivered.iter().fold(status, |hash, byte| {
            hash.wrapping_mul(31).wrapping_add(i32::from(*byte))
        })
    }

    fn owner() -> ProgramId {
        ProgramId::new([1; 32]).unwrap_or_else(|error| panic!("program: {error}"))
    }

    fn actor() -> PrincipalId {
        PrincipalId::new([2; 32]).unwrap_or_else(|error| panic!("principal: {error}"))
    }

    fn abi(storage: Storage) -> Abi {
        Abi::new(
            crate::ABI_V2_VERSION,
            owner(),
            AuthorizationContext::new(
                actor(),
                CapabilitySet::new([Capability::StorageRead])
                    .unwrap_or_else(|error| panic!("grant: {error}")),
            ),
            storage,
            &UnavailableReceiptOracle,
        )
        .unwrap_or_else(|error| panic!("ABI: {error}"))
    }

    fn observe(instance: &mut ProgramInstance, outputs: Vec<WasmValue>) -> Observation {
        Observation {
            outputs,
            usage: instance
                .meter()
                .finish()
                .unwrap_or_else(|refusal| panic!("usage: {refusal}")),
            state: instance
                .storage_snapshot()
                .unwrap_or_else(|| panic!("storage snapshot absent"))
                .namespace_entries(StorageNamespace::principal(owner(), actor())),
        }
    }

    fn production(wasm: &[u8], storage: Storage) -> Observation {
        let module = WasmEngine::declared()
            .unwrap_or_else(|error| panic!("engine: {error}"))
            .validate_candidate_v2(wasm)
            .unwrap_or_else(|error| panic!("validation: {error}"));
        let mut instance = module
            .instantiate_sandbox(Meter::declared(), abi(storage))
            .unwrap_or_else(|fault| panic!("instantiate: {fault}"));
        let outputs = instance
            .call(CALL_ENTRY_EXPORT, &[WasmValue::I32(0), WasmValue::I32(0)])
            .unwrap_or_else(|fault| panic!("production call: {fault}"));
        observe(&mut instance, outputs)
    }

    // Mirrors the historical Wasmi internal-fuel reference tier of the
    // determinism differential, but over an ABI-backed state so the scan host
    // function executes against real storage instead of refusing.
    fn reference(wasm: &[u8], storage: Storage) -> Observation {
        let limits = ValidationLimits::declared();
        let maximum_height = usize::try_from(limits.max_value_stack_height())
            .unwrap_or_else(|error| panic!("height: {error}"));
        let stack_limits = wasmi::StackLimits::new(
            1_024usize.min(maximum_height),
            maximum_height,
            usize::try_from(limits.max_call_depth())
                .unwrap_or_else(|error| panic!("depth: {error}")),
        )
        .unwrap_or_else(|error| panic!("stack limits: {error}"));
        let mut config = wasmi::Config::default();
        config
            .set_stack_limits(stack_limits)
            .wasm_mutable_global(true)
            .wasm_sign_extension(true)
            .wasm_multi_value(true)
            .wasm_bulk_memory(true)
            .wasm_saturating_float_to_int(false)
            .wasm_reference_types(false)
            .wasm_tail_call(false)
            .wasm_extended_const(false)
            .consume_fuel(true)
            .floats(false);
        let engine = wasmi::Engine::new(&config);
        let module = crate::validate::validate_original_for_qualification(
            &engine,
            limits,
            wasm,
            crate::validate::AbiRevision::V2,
        )
        .unwrap_or_else(|error| panic!("reference validation: {error}"));
        let linker = linker(&engine).unwrap_or_else(|fault| panic!("linker: {fault}"));
        let mut state = RuntimeState::sandbox(Meter::declared(), abi(storage));
        state.legacy_reference_fuel = true;
        state.bind_metering_schedule(FuelSchedule::WASMI_0_31_2);
        let initial_fuel = state.meter().cpu_remaining();
        let mut store = wasmi::Store::new(&engine, state);
        store.limiter(|state| state.meter_mut() as &mut dyn wasmi::ResourceLimiter);
        store
            .add_fuel(initial_fuel)
            .unwrap_or_else(|error| panic!("fuel: {error}"));
        let instance = linker
            .instantiate(&mut store, &module)
            .and_then(|pre| pre.start(&mut store))
            .unwrap_or_else(|error| panic!("reference instantiate: {error}"));
        let mut instance = ProgramInstance::new(store, instance);
        let outputs = instance
            .call(CALL_ENTRY_EXPORT, &[WasmValue::I32(0), WasmValue::I32(0)])
            .unwrap_or_else(|fault| panic!("reference call: {fault}"));
        let _ = instance
            .commit_reference_fuel()
            .unwrap_or_else(|fault| panic!("reference fuel: {fault}"));
        observe(&mut instance, outputs)
    }

    fn seeded(entries: &[(&[u8], &[u8])]) -> Storage {
        let mut storage = Storage::new();
        let mut transaction = storage.transaction(StorageNamespace::principal(owner(), actor()));
        for (key, value) in entries {
            transaction
                .write(key, value)
                .unwrap_or_else(|error| panic!("seed: {error}"));
        }
        let _ = transaction.commit();
        storage
    }

    #[test]
    fn scan_pages_are_identical_across_both_engine_tiers_and_insertion_orders() {
        let forward = seeded(&ENTRIES);
        let reversed: Vec<_> = ENTRIES.iter().rev().copied().collect();
        let backward = seeded(&reversed);
        let namespace = StorageNamespace::principal(owner(), actor());
        let limits = ScanLimits::new(2, 4096).unwrap_or_else(|error| panic!("limits: {error}"));
        let mut cursor = Vec::new();
        let mut delivered_keys = Vec::new();
        let mut activities = 0;
        loop {
            let expected = forward
                .scan(namespace, b"k/", &cursor, limits)
                .unwrap_or_else(|error| panic!("expected page: {error}"));
            let page = expected
                .encode_for_guest()
                .unwrap_or_else(|error| panic!("encode: {error}"));
            let wasm = checksum_guest(b"k/", &cursor, limits);
            let reference = reference(&wasm, forward.clone());
            let production = production(&wasm, backward.clone());
            assert_eq!(reference, production);
            assert_eq!(
                reference.outputs,
                vec![WasmValue::I32(checksum(length(&page), &page))]
            );
            assert_eq!(reference.usage.storage_read_bytes, expected.metered_bytes());
            assert_eq!(reference.state, forward.namespace_entries(namespace));
            delivered_keys.extend(expected.entries().iter().map(|entry| entry.key.clone()));
            activities += 1;
            match expected.cursor() {
                Some(next) => cursor = next.to_vec(),
                None => break,
            }
        }
        assert_eq!(activities, 3);
        assert_eq!(
            delivered_keys,
            vec![
                b"k/".to_vec(),
                b"k/01".to_vec(),
                b"k/02".to_vec(),
                b"k/03".to_vec(),
                b"k/10".to_vec()
            ]
        );
    }
}
