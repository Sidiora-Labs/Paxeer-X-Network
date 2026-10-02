use layerx_programs_runtime::abi::response::CANDIDATE_ABI_MODULE;
use layerx_programs_runtime::UnavailableReceiptOracle as NoReceipts;
use layerx_programs_runtime::test_support::{
    code_section, func_body, function_section, import_section, module, type_section, unsigned_leb,
    OP_CALL, OP_END, OP_I32_CONST, TYPE_I32, TYPE_I64,
};
use layerx_programs_runtime::{
    derive_program_account, Abi, AbiError, AuthorizationContext, AuthorizedExecutionRequest,
    Capability, CapabilitySet, CompositionContext, CompositionRules, ExecutionError, Executor,
    PrincipalId, ProgramCatalog, ProgramId, UnavailableReceiptOracle, ResponseRefusal, Storage,
    StorageNamespace, ValidationRefusal, WasmEngine, WasmValue, ABI_MODULE, ABI_V1_VERSION,
    ABI_V2_HOST_FUNCTIONS, ABI_V2_MANIFEST, ABI_V2_VERSION, ABI_VERSION, CALL_ENTRY_EXPORT,
    HOST_FUNCTIONS,
};

const ABI_V1_GOLDEN: &str = include_str!("../../../tests/vectors/abi-v1.hex");
const ABI_V2_GOLDEN: &str = include_str!("../../../tests/vectors/abi-v2.hex");
const ATTACK_INVENTORY: &str = include_str!("../../../tests/gauntlet/attack-inventory.tsv");
#[path = "../../../tests/gauntlet/state.rs"]
mod state_gauntlet;

const fn frozen_manifest_part(manifest: &[u8], offset: usize, part: &[u8]) -> usize {
    let mut index = 0;
    while index < part.len() {
        assert!(offset + index < manifest.len());
        assert!(manifest[offset + index] == part[index]);
        index += 1;
    }
    offset + part.len()
}

const fn frozen_manifest_table(
    manifest: &[u8], offset: usize, module: &str,
    functions: &[layerx_programs_runtime::HostFunction],
) -> usize {
    let mut offset = frozen_manifest_part(manifest, offset, module.as_bytes());
    offset = frozen_manifest_part(manifest, offset, &[0]);
    let mut index = 0;
    while index < functions.len() {
        offset = frozen_manifest_part(manifest, offset, functions[index].name.as_bytes());
        offset = frozen_manifest_part(manifest, offset, functions[index].signature.as_bytes());
        offset = frozen_manifest_part(manifest, offset, &[0]);
        index += 1;
    }
    offset
}

const fn frozen_golden(version: u16, manifest: &[u8], golden: &str) {
    let bytes = golden.as_bytes();
    let mut start = 0;
    let mut end = bytes.len();
    while start < end && bytes[start].is_ascii_whitespace() { start += 1; }
    while start < end && bytes[end - 1].is_ascii_whitespace() { end -= 1; }
    assert!(end - start == (manifest.len() + 2) * 2);
    let version = version.to_be_bytes();
    let mut index = 0;
    while index < manifest.len() + 2 {
        let actual = (nibble(bytes[start + index * 2]) << 4)
            | nibble(bytes[start + index * 2 + 1]);
        let expected = if index < 2 { version[index] } else { manifest[index - 2] };
        assert!(actual == expected);
        index += 1;
    }
}

const _: () = {
    let v1 = Abi::canonical_manifest();
    assert!(frozen_manifest_table(v1, 0, ABI_MODULE, &HOST_FUNCTIONS) == v1.len());
    frozen_golden(ABI_V1_VERSION, v1, ABI_V1_GOLDEN);
    let v2 = ABI_V2_MANIFEST.as_bytes();
    let inherited = frozen_manifest_part(v2, 0, v1);
    assert!(frozen_manifest_table(v2, inherited, CANDIDATE_ABI_MODULE,
        &ABI_V2_HOST_FUNCTIONS) == v2.len());
    frozen_golden(ABI_V2_VERSION, v2, ABI_V2_GOLDEN);
};

fn ids(program: u8, principal: u8) -> (ProgramId, PrincipalId) {
    let program = ProgramId::new([program; 32]).unwrap_or_else(|error| panic!("program: {error}"));
    let principal =
        PrincipalId::new([principal; 32]).unwrap_or_else(|error| panic!("principal: {error}"));
    (program, principal)
}

fn section(id: u8, payload: &[u8]) -> Vec<u8> {
    let mut encoded = vec![id];
    encoded.extend(unsigned_leb(payload.len() as u64));
    encoded.extend_from_slice(payload);
    encoded
}

fn exports(entries: &[(&str, u8, u8)]) -> Vec<u8> {
    let mut payload = unsigned_leb(entries.len() as u64);
    for (name, kind, index) in entries {
        payload.extend(unsigned_leb(name.len() as u64));
        payload.extend_from_slice(name.as_bytes());
        payload.extend_from_slice(&[*kind, *index]);
    }
    section(7, &payload)
}

fn data_section(entries: &[(u32, &[u8])]) -> Vec<u8> {
    let mut payload = unsigned_leb(entries.len() as u64);
    for (offset, bytes) in entries {
        payload.extend([0, OP_I32_CONST]);
        payload.extend(signed_leb_i32(
            i32::try_from(*offset).unwrap_or_else(|error| panic!("data offset: {error}")),
        ));
        payload.push(OP_END);
        payload.extend(unsigned_leb(bytes.len() as u64));
        payload.extend_from_slice(bytes);
    }
    section(11, &payload)
}

fn push_i32(instructions: &mut Vec<u8>, value: i32) {
    instructions.push(OP_I32_CONST);
    instructions.extend(signed_leb_i32(value));
}

fn memory_and_data(value: &[u8]) -> (Vec<u8>, Vec<u8>) {
    let memory = vec![5, 3, 1, 0, 1];
    let mut contents = b"key".to_vec();
    contents.extend_from_slice(value);
    let mut data_payload = vec![1, 0, OP_I32_CONST, 0, OP_END];
    data_payload.extend(unsigned_leb(contents.len() as u64));
    data_payload.extend(contents);
    (memory, section(11, &data_payload))
}

fn storage_module(function: &str, pointer: i32, value: &[u8]) -> Vec<u8> {
    let (memory, data) = memory_and_data(value);
    let export_payload = vec![
        2, 3, b'r', b'u', b'n', 0, 1, 6, b'm', b'e', b'm', b'o', b'r', b'y', 2, 0,
    ];
    let mut instructions = vec![OP_I32_CONST];
    instructions.extend(signed_leb_i32(pointer));
    instructions.extend([OP_I32_CONST, 3]);
    if function == "storage_write" {
        instructions.extend([OP_I32_CONST, 3, OP_I32_CONST]);
        instructions.extend(signed_leb_i32(
            i32::try_from(value.len()).unwrap_or_else(|error| panic!("value length: {error}")),
        ));
    } else {
        instructions.extend([OP_I32_CONST, 16, OP_I32_CONST, 16]);
    }
    instructions.extend([OP_CALL, 0, OP_END]);
    module(&[
        type_section(&[
            (&[TYPE_I32, TYPE_I32, TYPE_I32, TYPE_I32], &[TYPE_I32]),
            (&[TYPE_I32, TYPE_I32], &[TYPE_I32]),
        ]),
        import_section(&[(ABI_MODULE, function, 0)]),
        function_section(&[1]),
        memory,
        section(7, &export_payload),
        code_section(&[func_body(&[], &instructions)]),
        data,
    ])
}

fn storage_read_destination_module(pointer: i32, capacity: i32) -> Vec<u8> {
    let mut instructions = Vec::new();
    for value in [0, 3, pointer, capacity] {
        push_i32(&mut instructions, value);
    }
    instructions.extend([OP_CALL, 0, OP_END]);
    module(&[
        type_section(&[
            (&[TYPE_I32; 4], &[TYPE_I32]),
            (&[TYPE_I32, TYPE_I32], &[TYPE_I32]),
        ]),
        import_section(&[(ABI_MODULE, "storage_read", 0)]),
        function_section(&[1]),
        section(5, &[1, 1, 1, 1]),
        exports(&[("run", 0, 1), ("memory", 2, 0)]),
        code_section(&[func_body(&[], &instructions)]),
        data_section(&[(0, b"key")]),
    ])
}

fn missing_memory_storage_module() -> Vec<u8> {
    module(&[
        type_section(&[
            (&[TYPE_I32; 4], &[TYPE_I32]),
            (&[TYPE_I32, TYPE_I32], &[TYPE_I32]),
        ]),
        import_section(&[(ABI_MODULE, "storage_write", 0)]),
        function_section(&[1]),
        exports(&[("run", 0, 1)]),
        code_section(&[func_body(
            &[],
            &[
                OP_I32_CONST,
                0,
                OP_I32_CONST,
                0,
                OP_I32_CONST,
                0,
                OP_I32_CONST,
                0,
                OP_CALL,
                0,
                OP_END,
            ],
        )]),
    ])
}

fn storage_write_entry(value: u8) -> Vec<u8> {
    let mut entry = Vec::new();
    for argument in [0, 1, 1, 1] {
        push_i32(&mut entry, argument);
    }
    entry.extend([OP_CALL, 0, OP_END]);
    module(&[
        type_section(&[
            (&[TYPE_I32; 4], &[TYPE_I32]),
            (&[TYPE_I32], &[TYPE_I32]),
            (&[TYPE_I32, TYPE_I32], &[TYPE_I32]),
        ]),
        import_section(&[(ABI_MODULE, "storage_write", 0)]),
        function_section(&[1, 2]),
        section(5, &[1, 1, 1, 1]),
        exports(&[
            ("layerx_reserve", 0, 1),
            (CALL_ENTRY_EXPORT, 0, 2),
            ("memory", 2, 0),
        ]),
        code_section(&[
            func_body(&[], &[OP_I32_CONST, 0, OP_END]),
            func_body(&[], &entry),
        ]),
        data_section(&[(0, b"k"), (1, &[value])]),
    ])
}

fn nested_storage_root(child: ProgramId, value: u8) -> Vec<u8> {
    let requested = CapabilitySet::new([Capability::StorageWrite])
        .unwrap_or_else(|error| panic!("requested storage capability: {error}"))
        .canonical_encoding();
    let mut entry = Vec::new();
    for argument in [0, 1, 1, 1] {
        push_i32(&mut entry, argument);
    }
    entry.extend([OP_CALL, 0, 0x1a]);
    for argument in [
        32,
        32,
        0,
        0,
        64,
        i32::try_from(requested.len()).unwrap_or(i32::MAX),
    ] {
        push_i32(&mut entry, argument);
    }
    entry.extend([OP_CALL, 1, OP_END]);
    module(&[
        type_section(&[
            (&[TYPE_I32; 4], &[TYPE_I32]),
            (&[TYPE_I32; 6], &[TYPE_I32]),
            (&[TYPE_I32], &[TYPE_I32]),
            (&[TYPE_I32, TYPE_I32], &[TYPE_I32]),
        ]),
        import_section(&[
            (ABI_MODULE, "storage_write", 0),
            (ABI_MODULE, "program_call", 1),
        ]),
        function_section(&[2, 3]),
        section(5, &[1, 1, 1, 1]),
        exports(&[
            ("layerx_reserve", 0, 2),
            (CALL_ENTRY_EXPORT, 0, 3),
            ("memory", 2, 0),
        ]),
        code_section(&[
            func_body(&[], &[OP_I32_CONST, 0, OP_END]),
            func_body(&[], &entry),
        ]),
        data_section(&[
            (0, b"k"),
            (1, &[value]),
            (32, &child.bytes()),
            (64, &requested),
        ]),
    ])
}

fn execute_nested_storage(
    root_wasm: &[u8],
    child: ProgramId,
    child_wasm: &[u8],
    storage: &mut Storage,
    root: ProgramId,
    principal: PrincipalId,
) -> layerx_programs_runtime::AuthorizedExecutionRecord {
    let engine = WasmEngine::declared().unwrap_or_else(|error| panic!("engine: {error}"));
    let root_module = engine
        .validate(root_wasm)
        .unwrap_or_else(|error| panic!("root validation: {error}"));
    let mut catalog = ProgramCatalog::new();
    catalog.insert(
        child,
        engine
            .validate(child_wasm)
            .unwrap_or_else(|error| panic!("child validation: {error}")),
    );
    let grants = CapabilitySet::new([
        Capability::StorageWrite,
        Capability::Call { program: child },
    ])
    .unwrap_or_else(|error| panic!("root grants: {error}"));
    Executor::declared()
        .execute_authorized(
            storage,
            AuthorizedExecutionRequest {
                module: &root_module,
                program: root,
                authorization: AuthorizationContext::new(principal, grants),
                receipts: &UnavailableReceiptOracle,
                entrypoint: CALL_ENTRY_EXPORT,
                calldata: &[],
                composition: CompositionContext::catalog(catalog, CompositionRules::declared()),
                response_capacity: 0,
            },
        )
        .unwrap_or_else(|error| panic!("nested execution: {error}"))
}

fn event_module() -> Vec<u8> {
    module(&[
        type_section(&[
            (&[TYPE_I32; 4], &[TYPE_I32]),
            (&[TYPE_I32, TYPE_I32], &[TYPE_I32]),
        ]),
        import_section(&[(ABI_MODULE, "event_emit", 0)]),
        function_section(&[1]),
        section(5, &[1, 1, 1, 1]),
        exports(&[("run", 0, 1), ("memory", 2, 0)]),
        code_section(&[func_body(
            &[],
            &[
                OP_I32_CONST,
                0,
                OP_I32_CONST,
                1,
                OP_I32_CONST,
                1,
                OP_I32_CONST,
                1,
                OP_CALL,
                0,
                OP_END,
            ],
        )]),
        data_section(&[(0, b"te")]),
    ])
}

fn transfer_module() -> Vec<u8> {
    let asset = [0xa5; 32];
    let recipient = [0x5a; 32];
    module(&[
        type_section(&[
            (
                &[TYPE_I64, TYPE_I64, TYPE_I32, TYPE_I32, TYPE_I32, TYPE_I32],
                &[TYPE_I32],
            ),
            (&[TYPE_I32, TYPE_I32], &[TYPE_I32]),
        ]),
        import_section(&[(ABI_MODULE, "transfer_402", 0)]),
        function_section(&[1]),
        section(5, &[1, 1, 1, 1]),
        exports(&[("run", 0, 1), ("memory", 2, 0)]),
        code_section(&[func_body(
            &[],
            &[
                0x42,
                0,
                0x42,
                1,
                OP_I32_CONST,
                0,
                OP_I32_CONST,
                32,
                OP_I32_CONST,
                32,
                OP_I32_CONST,
                32,
                OP_CALL,
                0,
                OP_END,
            ],
        )]),
        data_section(&[(0, &asset), (32, &recipient)]),
    ])
}

fn candidate_oob_response_module(pointer: i32) -> Vec<u8> {
    let mut entry = Vec::new();
    for value in [0, pointer, 1] {
        push_i32(&mut entry, value);
    }
    entry.extend([OP_CALL, 0, 0x1a, OP_I32_CONST, 0, OP_END]);
    module(&[
        type_section(&[
            (&[TYPE_I32; 3], &[TYPE_I32]),
            (&[TYPE_I32], &[TYPE_I32]),
            (&[TYPE_I32, TYPE_I32], &[TYPE_I32]),
        ]),
        import_section(&[(CANDIDATE_ABI_MODULE, "response_write", 0)]),
        function_section(&[1, 2]),
        section(5, &[1, 1, 1, 1]),
        exports(&[
            ("layerx_reserve", 0, 1),
            (CALL_ENTRY_EXPORT, 0, 2),
            ("memory", 2, 0),
        ]),
        code_section(&[
            func_body(&[], &[OP_I32_CONST, 0, OP_END]),
            func_body(&[], &entry),
        ]),
    ])
}

fn signed_leb_i32(value: i32) -> Vec<u8> {
    let mut value = i64::from(value);
    let mut bytes = Vec::new();
    loop {
        let byte = match u8::try_from(value & 0x7f) {
            Ok(byte) => byte,
            Err(error) => panic!("signed LEB byte conversion failed: {error}"),
        };
        value >>= 7;
        let done = (value == 0 && byte & 0x40 == 0) || (value == -1 && byte & 0x40 != 0);
        bytes.push(if done { byte } else { byte | 0x80 });
        if done {
            return bytes;
        }
    }
}

fn execute(
    wasm: &[u8],
    storage: &mut Storage,
    program: ProgramId,
    principal: PrincipalId,
    grants: impl IntoIterator<Item = Capability>,
) -> layerx_programs_runtime::AuthorizedExecutionRecord {
    execute_result(wasm, storage, program, principal, grants)
        .unwrap_or_else(|error| panic!("execution: {error}"))
}

fn execute_result(
    wasm: &[u8],
    storage: &mut Storage,
    program: ProgramId,
    principal: PrincipalId,
    grants: impl IntoIterator<Item = Capability>,
) -> Result<
    layerx_programs_runtime::AuthorizedExecutionRecord,
    layerx_programs_runtime::ExecutionError,
> {
    let engine = WasmEngine::declared().unwrap_or_else(|error| panic!("engine: {error}"));
    let module = engine
        .validate(wasm)
        .unwrap_or_else(|error| panic!("validation: {error}"));
    let capabilities =
        CapabilitySet::new(grants).unwrap_or_else(|error| panic!("capabilities: {error}"));
    Executor::declared().execute_authorized(
        storage,
        AuthorizedExecutionRequest {
            module: &module,
            program,
            authorization: AuthorizationContext::new(principal, capabilities),
            receipts: &UnavailableReceiptOracle,
            entrypoint: "run",
            calldata: &[],
            composition: CompositionContext::isolated(),
            response_capacity: 0,
        },
    )
}

fn decode_hex(encoded: &str) -> Vec<u8> {
    let bytes = encoded.trim().as_bytes();
    assert_eq!(bytes.len() % 2, 0);
    bytes
        .chunks_exact(2)
        .map(|pair| (nibble(pair[0]) << 4) | nibble(pair[1]))
        .collect()
}

fn validation_refusal(engine: &WasmEngine, wasm: &[u8]) -> ValidationRefusal {
    match engine.validate(wasm) {
        Ok(_) => panic!("module unexpectedly validated"),
        Err(refusal) => refusal,
    }
}

fn assert_namespace(storage: &mut Storage, namespace: StorageNamespace, value: &[u8]) {
    assert_eq!(storage.namespace_cell_count(namespace), 1);
    assert_eq!(
        storage.namespace_persistent_bytes(namespace),
        Ok(u64::try_from(3 + value.len())
            .unwrap_or_else(|error| panic!("namespace bytes: {error}")))
    );
    assert_eq!(
        storage.transaction(namespace).read(b"key"),
        Ok(Some(value.to_vec()))
    );
}

const fn nibble(byte: u8) -> u8 {
    match byte {
        b'0'..=b'9' => byte - b'0',
        b'a'..=b'f' => byte - b'a' + 10,
        b'A'..=b'F' => byte - b'A' + 10,
        _ => panic!("non-hex golden byte"),
    }
}

fn inventory_rows(suite: &str) -> Vec<(&'static str, &'static str)> {
    let mut lines = ATTACK_INVENTORY.lines();
    assert_eq!(
        lines.next(),
        Some("id\tsuite\thostile_action\tboundary\texpected\ttest\tatomicity\tfuture_owner")
    );
    let mut identifiers = std::collections::BTreeSet::new();
    let mut selected = Vec::new();
    for (offset, line) in lines.enumerate() {
        assert!(
            !line.trim().is_empty(),
            "blank inventory row {}",
            offset + 2
        );
        let fields = line.split('\t').collect::<Vec<_>>();
        assert_eq!(fields.len(), 8, "malformed inventory row {}", offset + 2);
        assert!(
            fields.iter().all(|field| !field.trim().is_empty()),
            "empty inventory field on row {}",
            offset + 2
        );
        assert!(identifiers.insert(fields[0]), "duplicate id {}", fields[0]);
        assert!(
            matches!(fields[1], "isolation" | "composition"),
            "unknown inventory suite {} on row {}",
            fields[1],
            offset + 2
        );
        if fields[1] == suite {
            selected.push((fields[0], fields[5]));
        }
    }
    selected
}

#[test]
fn abi_v1_manifest_matches_typed_declarations_and_golden() {
    let mut regenerated = Vec::new();
    regenerated.extend_from_slice(ABI_MODULE.as_bytes());
    regenerated.push(0);
    for function in HOST_FUNCTIONS {
        regenerated.extend_from_slice(function.name.as_bytes());
        regenerated.extend_from_slice(function.signature.as_bytes());
        regenerated.push(0);
    }
    assert_eq!(regenerated, Abi::canonical_manifest());
    let mut versioned = ABI_V1_VERSION.to_be_bytes().to_vec();
    versioned.extend_from_slice(&regenerated);
    assert_eq!(versioned, decode_hex(ABI_V1_GOLDEN));
}

#[test]
fn abi_v2_manifest_matches_typed_declarations_and_golden() {
    let mut manifest = Abi::canonical_manifest().to_vec();
    manifest.extend_from_slice(CANDIDATE_ABI_MODULE.as_bytes());
    manifest.push(0);
    for function in ABI_V2_HOST_FUNCTIONS {
        manifest.extend_from_slice(function.name.as_bytes());
        manifest.extend_from_slice(function.signature.as_bytes());
        manifest.push(0);
    }
    assert_eq!(manifest, ABI_V2_MANIFEST.as_bytes());
    let mut regenerated = ABI_V2_VERSION.to_be_bytes().to_vec();
    regenerated.extend_from_slice(&manifest);
    assert_eq!(regenerated, decode_hex(ABI_V2_GOLDEN));
    let names = ABI_V2_HOST_FUNCTIONS
        .iter()
        .map(|function| function.name)
        .collect::<std::collections::BTreeSet<_>>();
    assert_eq!(names.len(), ABI_V2_HOST_FUNCTIONS.len());
    assert!(names.contains("context_read"));
    assert!(names.contains("balance_read"));
}

#[test]
fn exact_seven_function_surface_validates_and_instantiates() {
    let i32x4 = &[TYPE_I32; 4];
    let i32x2 = &[TYPE_I32; 2];
    let i32x6 = &[TYPE_I32; 6];
    let transfer = &[TYPE_I64, TYPE_I64, TYPE_I32, TYPE_I32, TYPE_I32, TYPE_I32];
    let wasm = module(&[
        type_section(&[
            (i32x4, &[TYPE_I32]),
            (i32x2, &[TYPE_I32]),
            (i32x6, &[TYPE_I32]),
            (transfer, &[TYPE_I32]),
        ]),
        import_section(&[
            (ABI_MODULE, "storage_read", 0),
            (ABI_MODULE, "storage_write", 0),
            (ABI_MODULE, "storage_delete", 1),
            (ABI_MODULE, "event_emit", 0),
            (ABI_MODULE, "program_call", 2),
            (ABI_MODULE, "transfer_402", 3),
            (ABI_MODULE, "receipt_read", 0),
        ]),
    ]);
    let engine = WasmEngine::declared().unwrap_or_else(|error| panic!("engine: {error}"));
    let validated = engine
        .validate(&wasm)
        .unwrap_or_else(|error| panic!("validation: {error}"));
    validated
        .instantiate()
        .unwrap_or_else(|error| panic!("instantiation: {error}"));
}

#[test]
fn wrong_signature_is_rejected_during_validation() {
    let wasm = module(&[
        type_section(&[(&[TYPE_I32], &[TYPE_I32])]),
        import_section(&[(ABI_MODULE, "storage_read", 0)]),
    ]);
    let engine = WasmEngine::declared().unwrap_or_else(|error| panic!("engine: {error}"));
    assert_eq!(
        validation_refusal(&engine, &wasm),
        ValidationRefusal::WrongImportSignature {
            import_name: "storage_read".into()
        }
    );
}

#[test]
fn unknown_and_ambient_kernel_imports_are_rejected() {
    let engine = WasmEngine::declared().unwrap_or_else(|error| panic!("engine: {error}"));
    for (module_name, import_name) in [
        (ABI_MODULE, "kernel_state"),
        (ABI_MODULE, "balance_write"),
        ("env", "kernel_state"),
        ("kernel", "storage_read"),
        ("wasi_snapshot_preview1", "fd_write"),
        (CANDIDATE_ABI_MODULE, "response_write"),
    ] {
        let wasm = module(&[
            type_section(&[(&[], &[TYPE_I32])]),
            import_section(&[(module_name, import_name, 0)]),
        ]);
        assert_eq!(
            validation_refusal(&engine, &wasm),
            ValidationRefusal::ForbiddenImport {
                import_module: module_name.into(),
                import_name: import_name.into(),
            }
        );
    }
}

#[test]
fn wrong_kind_is_rejected_during_validation() {
    let engine = WasmEngine::declared().unwrap_or_else(|error| panic!("engine: {error}"));
    for descriptor in [vec![1, 0x70, 0, 1], vec![2, 0, 1], vec![3, TYPE_I32, 0]] {
        let mut payload = vec![1, 9];
        payload.extend_from_slice(ABI_MODULE.as_bytes());
        payload.push(12);
        payload.extend_from_slice(b"storage_read");
        payload.extend(descriptor);
        let wasm = module(&[section(2, &payload)]);
        assert_eq!(
            validation_refusal(&engine, &wasm),
            ValidationRefusal::WrongImportKind {
                import_name: "storage_read".into()
            }
        );
    }
}

#[test]
fn denied_guest_storage_write_is_stable_and_has_no_effect() {
    let (program, principal) = ids(1, 2);
    let mut storage = Storage::new();
    let before = storage.clone();
    let refusal = execute_result(
        &storage_module("storage_write", 0, b"new"),
        &mut storage,
        program,
        principal,
        [],
    );
    assert_eq!(
        refusal,
        Err(layerx_programs_runtime::ExecutionError::Entrypoint(
            layerx_programs_runtime::EntrypointRefusal::GuestRefused { code: -1 }
        ))
    );
    assert_eq!(storage, before);
}

#[test]
fn real_guest_storage_is_scoped_by_program_and_principal() {
    let (program_a, principal_p) = ids(1, 7);
    let (program_b, _) = ids(2, 7);
    let (_, principal_q) = ids(1, 8);
    let mut storage = Storage::new();
    let written = execute(
        &storage_module("storage_write", 0, b"new"),
        &mut storage,
        program_a,
        principal_p,
        [Capability::StorageWrite],
    );
    assert_eq!(written.execution.outputs, vec![WasmValue::I32(0)]);
    assert_eq!(written.execution.usage.storage_write_bytes, 6);
    let foreign_program_write = execute(
        &storage_module("storage_write", 0, b"foreign"),
        &mut storage,
        program_b,
        principal_p,
        [Capability::StorageWrite],
    );
    let foreign_principal_write = execute(
        &storage_module("storage_write", 0, b"queue"),
        &mut storage,
        program_a,
        principal_q,
        [Capability::StorageWrite],
    );
    assert_eq!(
        foreign_program_write.execution.outputs,
        vec![WasmValue::I32(0)]
    );
    assert_eq!(
        foreign_program_write.execution.usage.storage_write_bytes,
        10
    );
    assert_eq!(
        foreign_principal_write.execution.outputs,
        vec![WasmValue::I32(0)]
    );
    assert_eq!(
        foreign_principal_write.execution.usage.storage_write_bytes,
        8
    );
    let owner = execute(
        &storage_module("storage_read", 0, b""),
        &mut storage,
        program_a,
        principal_p,
        [Capability::StorageRead],
    );
    let other_program = execute(
        &storage_module("storage_read", 0, b""),
        &mut storage,
        program_b,
        principal_p,
        [Capability::StorageRead],
    );
    let other_principal = execute(
        &storage_module("storage_read", 0, b""),
        &mut storage,
        program_a,
        principal_q,
        [Capability::StorageRead],
    );
    assert_eq!(owner.execution.outputs, vec![WasmValue::I32(4)]);
    assert_eq!(owner.execution.usage.storage_read_bytes, 6);
    assert_eq!(other_program.execution.outputs, vec![WasmValue::I32(8)]);
    assert_eq!(other_program.execution.usage.storage_read_bytes, 10);
    assert_eq!(other_principal.execution.outputs, vec![WasmValue::I32(6)]);
    assert_eq!(other_principal.execution.usage.storage_read_bytes, 8);
    let owner_namespace = StorageNamespace::principal(program_a, principal_p);
    let program_namespace = StorageNamespace::principal(program_b, principal_p);
    let principal_namespace = StorageNamespace::principal(program_a, principal_q);
    assert_namespace(&mut storage, owner_namespace, b"new");
    assert_namespace(&mut storage, program_namespace, b"foreign");
    assert_namespace(&mut storage, principal_namespace, b"queue");
    let mut replacement = storage.transaction(owner_namespace);
    replacement
        .write(b"key", b"value")
        .unwrap_or_else(|error| panic!("replacement: {error}"));
    assert_eq!(replacement.commit(), 1);
    assert_eq!(storage.namespace_persistent_bytes(owner_namespace), Ok(8));
}

#[test]
fn principal_and_shared_namespaces_are_closed_ordered_and_disjoint() {
    let (program_a, principal_a) = ids(21, 31);
    let (program_b, principal_b) = ids(22, 32);
    let principal_a_namespace = StorageNamespace::principal(program_a, principal_a);
    let second_principal_namespace = StorageNamespace::principal(program_a, principal_b);
    let other_program_principal = StorageNamespace::principal(program_b, principal_a);
    let shared_a_namespace = StorageNamespace::shared(program_a);
    let other_shared_namespace = StorageNamespace::shared(program_b);
    assert_eq!(principal_a_namespace.program(), program_a);
    assert_eq!(principal_a_namespace.principal_scope(), Some(principal_a));
    assert_eq!(shared_a_namespace.program(), program_a);
    assert_eq!(shared_a_namespace.principal_scope(), None);
    assert!(principal_a_namespace < second_principal_namespace);
    assert!(second_principal_namespace < shared_a_namespace);
    assert!(shared_a_namespace < other_program_principal);
    assert!(shared_a_namespace < other_shared_namespace);
    let mut principal_bytes = vec![21; 32];
    principal_bytes.push(0);
    principal_bytes.extend_from_slice(&[31; 32]);
    assert_eq!(principal_a_namespace.canonical_bytes(), principal_bytes);
    let mut shared_bytes = vec![21; 32];
    shared_bytes.push(1);
    assert_eq!(shared_a_namespace.canonical_bytes(), shared_bytes);
    let abi = Abi::new(
        ABI_VERSION,
        program_a,
        AuthorizationContext::new(principal_a, CapabilitySet::empty()),
        Storage::new(),
        &UnavailableReceiptOracle,
    )
    .unwrap_or_else(|error| panic!("abi: {error}"));
    assert_eq!(abi.principal_namespace(), principal_a_namespace);
    assert_eq!(abi.shared_namespace(), shared_a_namespace);

    let mut storage = Storage::new();
    for (namespace, value) in [
        (principal_a_namespace, b"principal-a".as_slice()),
        (second_principal_namespace, b"principal-b".as_slice()),
        (shared_a_namespace, b"shared-a".as_slice()),
        (other_shared_namespace, b"shared-b".as_slice()),
    ] {
        let mut transaction = storage.transaction(namespace);
        transaction
            .write(b"same-key", value)
            .unwrap_or_else(|error| panic!("write: {error}"));
        assert_eq!(transaction.commit(), 1);
    }
    for (namespace, expected) in [
        (principal_a_namespace, b"principal-a".as_slice()),
        (second_principal_namespace, b"principal-b".as_slice()),
        (shared_a_namespace, b"shared-a".as_slice()),
        (other_shared_namespace, b"shared-b".as_slice()),
    ] {
        assert_eq!(storage.namespace_cell_count(namespace), 1);
        assert_eq!(
            storage.transaction(namespace).read(b"same-key"),
            Ok(Some(expected.to_vec()))
        );
    }
}

#[test]
fn guest_memory_bounds_refusal_cannot_write_or_emit_effects() {
    let (program, principal) = ids(3, 4);
    for pointer in [-1, 65_535, i32::MAX] {
        let mut storage = Storage::new();
        let before = storage.clone();
        let refusal = execute_result(
            &storage_module("storage_write", pointer, b"new"),
            &mut storage,
            program,
            principal,
            [Capability::StorageWrite],
        );
        let status = if pointer < 0 { -2 } else { -3 };
        assert_eq!(
            refusal,
            Err(layerx_programs_runtime::ExecutionError::Entrypoint(
                layerx_programs_runtime::EntrypointRefusal::GuestRefused { code: status }
            )),
            "pointer {pointer}"
        );
        assert_eq!(storage, before, "pointer {pointer}");
    }

    let namespace = StorageNamespace::principal(program, principal);
    for pointer in [-1, 65_535, i32::MAX] {
        let mut storage = Storage::new();
        let mut seed = storage.transaction(namespace);
        seed.write(b"key", b"canary")
            .unwrap_or_else(|error| panic!("seed: {error}"));
        assert_eq!(seed.commit(), 1);
        let before = storage.clone();
        let refusal = execute_result(
            &storage_read_destination_module(pointer, 16),
            &mut storage,
            program,
            principal,
            [Capability::StorageRead],
        );
        let status = if pointer < 0 { -2 } else { -3 };
        assert_eq!(
            refusal,
            Err(ExecutionError::Entrypoint(
                layerx_programs_runtime::EntrypointRefusal::GuestRefused { code: status }
            )),
            "destination {pointer}"
        );
        assert_eq!(storage, before, "destination {pointer}");
    }

    let mut storage = Storage::new();
    let before = storage.clone();
    assert_eq!(
        execute_result(
            &missing_memory_storage_module(),
            &mut storage,
            program,
            principal,
            [Capability::StorageWrite],
        ),
        Err(ExecutionError::Entrypoint(
            layerx_programs_runtime::EntrypointRefusal::GuestRefused { code: -2 }
        ))
    );
    assert_eq!(storage, before);
}

#[test]
fn denied_event_and_transfer_guests_have_no_effects() {
    let (program, principal) = ids(11, 12);
    for (name, wasm) in [("event", event_module()), ("transfer", transfer_module())] {
        let mut storage = Storage::new();
        let before = storage.clone();
        assert_eq!(
            execute_result(&wasm, &mut storage, program, principal, []),
            Err(ExecutionError::Entrypoint(
                layerx_programs_runtime::EntrypointRefusal::GuestRefused { code: -1 }
            )),
            "{name}"
        );
        assert_eq!(storage, before, "{name}");
    }
}

#[test]
fn nested_frames_use_their_own_program_memory_and_storage_namespace() {
    let (root, principal_p) = ids(13, 15);
    let (child, _) = ids(14, 15);
    let (_, principal_q) = ids(13, 16);
    let root_wasm = nested_storage_root(child, b'A');
    let child_wasm = storage_write_entry(b'B');
    let mut storage = Storage::new();
    let record = execute_nested_storage(
        &root_wasm,
        child,
        &child_wasm,
        &mut storage,
        root,
        principal_p,
    );
    assert_eq!(record.call_graph.edges().len(), 1);
    assert_eq!(record.call_graph.edges()[0].principal(), principal_p);
    assert_eq!(
        storage
            .transaction(StorageNamespace::principal(root, principal_p))
            .read(b"k"),
        Ok(Some(vec![b'A']))
    );
    assert_eq!(
        storage
            .transaction(StorageNamespace::principal(child, principal_p))
            .read(b"k"),
        Ok(Some(vec![b'B']))
    );
    assert_eq!(
        storage
            .transaction(StorageNamespace::principal(root, principal_q))
            .read(b"k"),
        Ok(None)
    );

    let other = execute(
        &storage_module("storage_write", 0, b"queue"),
        &mut storage,
        root,
        principal_q,
        [Capability::StorageWrite],
    );
    assert_eq!(other.execution.outputs, vec![WasmValue::I32(0)]);
    assert_eq!(
        storage
            .transaction(StorageNamespace::principal(root, principal_p))
            .read(b"k"),
        Ok(Some(vec![b'A']))
    );
    assert_eq!(
        storage
            .transaction(StorageNamespace::principal(root, principal_q))
            .read(b"key"),
        Ok(Some(b"queue".to_vec()))
    );
}

#[test]
fn candidate_linker_uses_the_same_bounded_guest_memory_boundary() {
    let (program, principal) = ids(17, 18);
    let engine = WasmEngine::declared().unwrap_or_else(|error| panic!("engine: {error}"));
    let wasm = candidate_oob_response_module(65_536);
    assert!(matches!(
        engine.validate(&wasm),
        Err(ValidationRefusal::ForbiddenImport { .. })
    ));
    let module = engine
        .validate_candidate_v2(&wasm)
        .unwrap_or_else(|error| panic!("candidate validation: {error}"));
    let mut storage = Storage::new();
    let before = storage.clone();
    assert_eq!(
        Executor::declared().execute_authorized_candidate(
            &mut storage,
            AuthorizedExecutionRequest {
                module: &module,
                program,
                authorization: AuthorizationContext::new(principal, CapabilitySet::empty()),
                receipts: &UnavailableReceiptOracle,
                entrypoint: CALL_ENTRY_EXPORT,
                calldata: &[],
                composition: CompositionContext::isolated(),
                response_capacity: 1,
            },
        ),
        Err(ExecutionError::Response(
            ResponseRefusal::InvalidPublication
        ))
    );
    assert_eq!(storage, before);
}

#[test]
fn capability_narrowing_rejects_missing_grants_and_limit_widening_without_effects() {
    let (program, principal) = ids(5, 6);
    let (callee, _) = ids(9, 6);
    let asset = [11; 32];
    let to = [12; 32];
    let parent = CapabilitySet::new([
        Capability::Call { program: callee },
        Capability::Transfer402 {
            asset,
            to,
            maximum_amount: 10,
        },
    ])
    .unwrap_or_else(|error| panic!("capabilities: {error}"));
    assert_eq!(
        parent.narrow([Capability::StorageRead]),
        Err(AbiError::CapabilityDenied)
    );
    assert_eq!(
        parent.narrow([Capability::Transfer402 {
            asset,
            to,
            maximum_amount: 11
        }]),
        Err(AbiError::CapabilityEscalation)
    );
    let mut abi = Abi::new(
        ABI_VERSION,
        program,
        AuthorizationContext::new(principal, parent),
        Storage::new(),
        &UnavailableReceiptOracle,
    )
    .unwrap_or_else(|error| panic!("abi: {error}"));
    assert_eq!(
        abi.call_program(callee, b"", [Capability::StorageRead]),
        Err(AbiError::CapabilityDenied)
    );
    assert!(abi.commit().effects.calls.is_empty());
}

#[test]
fn program_spend_capability_binds_owner_seed_account_asset_and_destination() {
    let (owner, _) = ids(16, 17);
    let (other, _) = ids(18, 17);
    let seed = b"vault/isolation";
    let source_account = derive_program_account(owner, seed)
        .unwrap_or_else(|error| panic!("source account: {error}"))
        .bytes();
    let grant = Capability::ProgramSpend {
        owner_program: owner,
        seed: seed.to_vec(),
        source_account,
        asset: [19; 32],
        to: [20; 32],
        maximum_amount: 50,
    };
    let parent = CapabilitySet::new([grant.clone()])
        .unwrap_or_else(|error| panic!("program spend: {error}"));
    assert!(matches!(
        Abi::new(
            ABI_VERSION,
            other,
            AuthorizationContext::new(
                PrincipalId::new([17; 32]).unwrap_or_else(|error| panic!("principal: {error}")),
                parent.clone(),
            ),
            Storage::new(),
            &UnavailableReceiptOracle,
        ),
        Err(AbiError::InvalidCapability)
    ));
    let encoded = parent.canonical_encoding();
    assert_eq!(encoded[2], 9);
    assert_eq!(&encoded[3..35], &owner.bytes());
    assert_eq!(
        &encoded[35..37],
        &u16::try_from(seed.len())
            .unwrap_or_else(|error| panic!("seed length: {error}"))
            .to_be_bytes()
    );
    assert_eq!(&encoded[37..37 + seed.len()], seed);
    assert_eq!(&encoded[37 + seed.len()..69 + seed.len()], &source_account);

    let mut wrong_account = source_account;
    wrong_account[31] ^= 1;
    assert_eq!(
        CapabilitySet::new([Capability::ProgramSpend {
            owner_program: owner,
            seed: seed.to_vec(),
            source_account: wrong_account,
            asset: [19; 32],
            to: [20; 32],
            maximum_amount: 50,
        }]),
        Err(AbiError::InvalidCapability)
    );
    for widened in [
        Capability::ProgramSpend {
            owner_program: other,
            seed: seed.to_vec(),
            source_account: derive_program_account(other, seed)
                .unwrap_or_else(|error| panic!("other account: {error}"))
                .bytes(),
            asset: [19; 32],
            to: [20; 32],
            maximum_amount: 50,
        },
        Capability::ProgramSpend {
            owner_program: owner,
            seed: b"vault/other".to_vec(),
            source_account: derive_program_account(owner, b"vault/other")
                .unwrap_or_else(|error| panic!("other seed account: {error}"))
                .bytes(),
            asset: [19; 32],
            to: [20; 32],
            maximum_amount: 50,
        },
        Capability::ProgramSpend {
            owner_program: owner,
            seed: seed.to_vec(),
            source_account,
            asset: [21; 32],
            to: [20; 32],
            maximum_amount: 50,
        },
        Capability::ProgramSpend {
            owner_program: owner,
            seed: seed.to_vec(),
            source_account,
            asset: [19; 32],
            to: [22; 32],
            maximum_amount: 50,
        },
    ] {
        assert_eq!(
            parent.narrow([widened]),
            Err(AbiError::CapabilityEscalation)
        );
    }
}

#[test]
fn host_table_contains_seven_unique_names() {
    let mut names = std::collections::BTreeSet::new();
    assert_eq!(HOST_FUNCTIONS.len(), 7);
    for function in HOST_FUNCTIONS {
        assert!(names.insert(function.name));
    }
}

#[test]
#[allow(clippy::missing_panics_doc)]
pub fn programs_isolation_suite() {
    unknown_and_ambient_kernel_imports_are_rejected();
    wrong_kind_is_rejected_during_validation();
    abi_v1_manifest_matches_typed_declarations_and_golden();
    denied_guest_storage_write_is_stable_and_has_no_effect();
    denied_event_and_transfer_guests_have_no_effects();
    real_guest_storage_is_scoped_by_program_and_principal();
    nested_frames_use_their_own_program_memory_and_storage_namespace();
    guest_memory_bounds_refusal_cannot_write_or_emit_effects();
    candidate_linker_uses_the_same_bounded_guest_memory_boundary();
    capability_narrowing_rejects_missing_grants_and_limit_widening_without_effects();
    state_gauntlet::shared_state_gauntlet_suite();

    let executed = vec![
        ("ISO-001", "unknown_and_ambient_kernel_imports_are_rejected"),
        ("ISO-002", "wrong_kind_is_rejected_during_validation"),
        (
            "ISO-003",
            "abi_v1_manifest_matches_typed_declarations_and_golden",
        ),
        (
            "ISO-004",
            "denied_guest_storage_write_is_stable_and_has_no_effect",
        ),
        (
            "ISO-005",
            "denied_event_and_transfer_guests_have_no_effects",
        ),
        (
            "ISO-006",
            "real_guest_storage_is_scoped_by_program_and_principal",
        ),
        (
            "ISO-007",
            "nested_frames_use_their_own_program_memory_and_storage_namespace",
        ),
        (
            "ISO-008",
            "guest_memory_bounds_refusal_cannot_write_or_emit_effects",
        ),
        (
            "ISO-009",
            "candidate_linker_uses_the_same_bounded_guest_memory_boundary",
        ),
        (
            "ISO-010",
            "capability_narrowing_rejects_missing_grants_and_limit_widening_without_effects",
        ),
        (
            "ISO-011",
            "foreign_shared_selectors_are_structurally_closed",
        ),
        (
            "ISO-012",
            "forged_shared_capability_bytes_never_enter_the_child",
        ),
        ("ISO-013", "crafted_keys_cannot_name_a_foreign_namespace"),
        (
            "ISO-014",
            "narrowing_never_widens_shared_authority_across_a_call",
        ),
        (
            "ISO-015",
            "shared_surface_cannot_reach_another_principal_cells",
        ),
        (
            "ISO-016",
            "repeated_iteration_exhaustion_has_no_partial_output",
        ),
        (
            "ISO-017",
            "repeated_drop_rewrite_exhaustion_rolls_back_atomically",
        ),
    ];
    assert_eq!(inventory_rows("isolation"), executed);
}

fn scoped_write_guest(selector: i32, key: &[u8]) -> Vec<u8> {
    let mut entry = Vec::new();
    for argument in [
        selector, 0,
        i32::try_from(key.len()).unwrap_or_else(|error| panic!("key length: {error}")),
        512, 3,
    ] {
        push_i32(&mut entry, argument);
    }
    entry.extend([OP_CALL, 0, OP_END]);
    module(&[
        type_section(&[
            (&[TYPE_I32; 5], &[TYPE_I32]),
            (&[TYPE_I32], &[TYPE_I32]),
            (&[TYPE_I32; 2], &[TYPE_I32]),
        ]),
        import_section(&[(CANDIDATE_ABI_MODULE, "storage_write_scoped", 0)]),
        function_section(&[1, 2]),
        section(5, &[1, 1, 1, 1]),
        exports(&[
            ("layerx_reserve", 0, 1),
            (CALL_ENTRY_EXPORT, 0, 2),
            ("memory", 2, 0),
        ]),
        code_section(&[
            func_body(&[], &[OP_I32_CONST, 0, OP_END]),
            func_body(&[], &entry),
        ]),
        data_section(&[(0, key), (512, b"new")]),
    ])
}

#[test]
fn protocol_private_namespace_is_unreachable_from_guest_selectors_and_keys() {
    let (owner, actor) = ids(41, 42);
    let private = StorageNamespace::protocol_private(owner, [43; 32]);
    let mut initial = Storage::new();
    initial.replace_protocol_namespace(private, &[(b"key".to_vec(), b"secret".to_vec())])
        .unwrap_or_else(|error| panic!("seed: {error}"));
    let mut forged_key = private.canonical_bytes();
    forged_key.extend_from_slice(b"key");
    let engine = WasmEngine::declared().unwrap_or_else(|error| panic!("engine: {error}"));
    for selector in [-1, 0, 1, 2, 3, i32::MAX] {
        let wasm = scoped_write_guest(selector, &forged_key);
        let module = engine.validate_candidate_v2(&wasm)
            .unwrap_or_else(|error| panic!("module: {error}"));
        let mut storage = initial.clone();
        let grants = CapabilitySet::new([Capability::StorageWrite, Capability::SharedStorageWrite])
            .unwrap_or_else(|error| panic!("grants: {error}"));
        let record = Executor::declared().execute_authorized_candidate(
            &mut storage,
            AuthorizedExecutionRequest {
                module: &module, program: owner,
                authorization: AuthorizationContext::new(actor, grants),
                receipts: &UnavailableReceiptOracle,
                entrypoint: CALL_ENTRY_EXPORT, calldata: &[],
                composition: CompositionContext::isolated(), response_capacity: 0,
            },
        ).unwrap_or_else(|error| panic!("execution: {error}"));
        assert_eq!(storage.protocol_state_value(private, b"key"), Ok(Some(b"secret".to_vec())));
        assert_eq!(storage.namespace_cell_count(private), 1);
        if selector == 1 || selector == 2 {
            let namespace = if selector == 1 {
                StorageNamespace::principal(owner, actor)
            } else {
                StorageNamespace::shared(owner)
            };
            assert_eq!(record.execution().outputs(), [WasmValue::I32(0)]);
            assert_eq!(record.execution().usage().storage_write_bytes, 71);
            assert_eq!(storage.protocol_state_value(namespace, &forged_key), Ok(Some(b"new".to_vec())));
            let other = if selector == 1 { StorageNamespace::shared(owner) }
                else { StorageNamespace::principal(owner, actor) };
            assert_eq!(storage.namespace_cell_count(other), 0);
        } else {
            assert_eq!(record.execution().outputs(), [WasmValue::I32(-2)]);
            assert_eq!(record.execution().usage().storage_write_bytes, 0);
            assert_eq!(storage, initial);
        }
    }
}

#[test]
fn namespace_persistence_bridge_preserves_all_scopes_and_exact_reclamation() {
    use layerx_programs_runtime::{Meter, StorageSelector};
    let (owner, actor) = ids(51, 52);
    let (other, other_actor) = ids(53, 54);
    let namespaces = [
        StorageNamespace::principal(owner, actor), StorageNamespace::shared(owner),
        StorageNamespace::protocol_private(owner, [55; 32]),
        StorageNamespace::principal(owner, other_actor), StorageNamespace::shared(other),
    ];
    let mut storage = Storage::new();
    for namespace in namespaces {
        storage.replace_protocol_namespace(namespace, &[(b"key".to_vec(), b"old".to_vec())])
            .unwrap_or_else(|error| panic!("seed: {error}"));
    }
    let mut restored = Storage::new();
    for namespace in namespaces {
        let entries = storage.protocol_namespace_entries(namespace)
            .unwrap_or_else(|error| panic!("persist: {error}"));
        restored.replace_protocol_namespace(namespace, &entries)
            .unwrap_or_else(|error| panic!("restore: {error}"));
        assert_eq!(restored.namespace_cell_count(namespace), 1);
        assert_eq!(restored.namespace_persistent_bytes(namespace), Ok(6));
    }
    assert_eq!(restored, storage);
    let mut expected_private = vec![51; 32];
    expected_private.push(2);
    expected_private.extend_from_slice(&[55; 32]);
    assert_eq!(namespaces[2].canonical_bytes(), expected_private);
    assert!(namespaces[1] < namespaces[2]);
    assert!(namespaces[2] < namespaces[4]);
    let malformed = [(b"key".to_vec(), b"one".to_vec()), (b"key".to_vec(), b"two".to_vec())];
    assert_eq!(restored.replace_protocol_namespace(namespaces[0], &malformed),
        Err(layerx_programs_runtime::StorageError::PrefixTooLarge));
    assert_eq!(restored, storage);
    let grants = CapabilitySet::new([Capability::StorageWrite, Capability::SharedStorageWrite])
        .unwrap_or_else(|error| panic!("grants: {error}"));
    let mut abi = Abi::new(ABI_VERSION, owner, AuthorizationContext::new(actor, grants),
        restored, &UnavailableReceiptOracle).unwrap_or_else(|error| panic!("abi: {error}"));
    let mut meter = Meter::declared();
    for (selector, namespace) in [
        (StorageSelector::Principal, namespaces[0]), (StorageSelector::Shared, namespaces[1]),
    ] {
        let drop = abi.storage_drop_selected(&mut meter, selector)
            .unwrap_or_else(|error| panic!("drop: {error}"));
        assert_eq!(drop.namespace(), namespace);
        assert_eq!(drop.reclaimed_cells(), 1);
        assert_eq!(drop.reclaimed_key_value_bytes(), 6);
        assert_eq!(drop.metered_work(), 7);
    }
    assert_eq!(meter.finish().map(|usage| usage.storage_write_bytes), Ok(14));
    let committed = abi.commit().storage;
    for namespace in &namespaces[..2] {
        assert_eq!(committed.namespace_cell_count(*namespace), 0);
    }
    for namespace in &namespaces[2..] {
        assert_eq!(committed.protocol_state_value(*namespace, b"key"), Ok(Some(b"old".to_vec())));
        assert_eq!(committed.namespace_persistent_bytes(*namespace), Ok(6));
    }
}

#[test]
fn receipt_and_balance_sight_fail_closed_without_verified_authority() {
    let (owner, actor) = ids(61, 62);
    let digest = [63; 32];
    let account = [64; 32];
    let asset = [65; 32];
    let abi = Abi::new(ABI_VERSION, owner,
        AuthorizationContext::new(actor, CapabilitySet::empty()), Storage::new(),
        &UnavailableReceiptOracle).unwrap_or_else(|error| panic!("abi: {error}"));
    assert_eq!(abi.receipt_read(digest), Err(AbiError::CapabilityDenied));
    assert_eq!(abi.balance_read(account, asset), Err(AbiError::CapabilityDenied));
    let grant = CapabilitySet::new([Capability::ReceiptRead { receipt_digest: digest }])
        .unwrap_or_else(|error| panic!("receipt grant: {error}"));
    assert!(matches!(Abi::new(ABI_VERSION, owner, AuthorizationContext::new(actor, grant),
        Storage::new(), &UnavailableReceiptOracle), Err(AbiError::ReceiptMismatch)));
    let grant = CapabilitySet::new([Capability::BalanceView { account, asset, receipt_digest: digest }])
        .unwrap_or_else(|error| panic!("balance grant: {error}"));
    let abi = Abi::new(ABI_VERSION, owner, AuthorizationContext::new(actor, grant),
        Storage::new(), &UnavailableReceiptOracle)
        .unwrap_or_else(|error| panic!("balance abi: {error}"));
    assert_eq!(abi.balance_read(account, asset), Err(AbiError::BalanceEvidenceUnavailable));
    assert_eq!(abi.balance_read([66; 32], asset), Err(AbiError::CapabilityDenied));
    assert!(matches!(Abi::new(u16::MAX, owner,
        AuthorizationContext::new(actor, CapabilitySet::empty()), Storage::new(),
        &UnavailableReceiptOracle), Err(AbiError::WrongVersion)));
}
