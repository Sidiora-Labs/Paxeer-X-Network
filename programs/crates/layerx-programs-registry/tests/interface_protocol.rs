use std::collections::BTreeMap;
use std::fs;
use std::path::{Path, PathBuf};

use layerx_programs::{
    verify_interface_read, DeploymentProof, InterfaceCapability, InterfaceEntryPoint,
    InterfaceRefusal, InterfaceStateWitness, ProgramInterface, ProgramLifecycleProof,
    ProgramStateProof, ProtocolDeploymentVerifier, StateLeafWitness, StateProof, ValueSchema,
    ValueType,
};
use layerx_programs_runtime::test_support::{
    code_section, func_body, function_section, import_section, module, raw_section, type_section,
    unsigned_leb, OP_CALL, OP_DROP, OP_END, OP_I32_CONST, TYPE_I32,
};
use layerx_programs_runtime::{ProgramId, WasmEngine};
use layerx_proof::merkle::Proof;
use sha2::{Digest, Sha256};

const CASES: [(&str, u16, bool); 7] = [
    ("abi1", 1, false),
    ("abi2", 2, false),
    ("abi2-dynamic", 2, true),
    ("abi3", 3, false),
    ("abi3-dynamic", 3, true),
    ("abi4", 4, false),
    ("abi4-dynamic", 4, true),
];
const DOMAIN_LEN: usize = b"LayerX/program-interface/v1\0".len();

fn capabilities(abi: u16, dynamic: bool) -> Vec<InterfaceCapability> {
    let mut result = Vec::new();
    if dynamic {
        result.push(InterfaceCapability::CallerAuthorizedSpend {
            asset: [2; 32],
            maximum_amount: 100,
            recipient_offset: 4,
            amount_offset: 36,
        });
    }
    if abi >= 3 {
        result.push(InterfaceCapability::OracleRead);
    }
    if abi >= 4 {
        result.push(InterfaceCapability::WebRead);
    }
    result
}

fn entry(abi: u16, dynamic: bool, input: u32, output: u32) -> InterfaceEntryPoint {
    InterfaceEntryPoint {
        name: "call".to_owned(),
        discriminator: [0x10, 0x20, 0x30, 0x40],
        calldata: ValueSchema::layerx(ValueType::Bytes { max_len: input }),
        response: ValueSchema::layerx(ValueType::Bytes { max_len: output }),
        capabilities: capabilities(abi, dynamic),
        event_topics: Vec::new(),
        failures: Vec::new(),
    }
}

fn wasm(abi: u16, dynamic: bool) -> Vec<u8> {
    let mut imports = Vec::new();
    if dynamic {
        imports.push(("layerx_v2", "transfer_program_402", 3));
    }
    if abi >= 3 {
        imports.push(("layerx_v3", "oracle_read", 0));
    }
    if abi >= 4 {
        imports.push(("layerx_v4", "web_read", 0));
    }
    let n = imports.len() as u32;
    let mut exports = unsigned_leb(3);
    for (name, kind, index) in [
        ("layerx_reserve", 0_u8, n),
        ("call", 0, n + 1),
        ("memory", 2, 0),
    ] {
        exports.extend(unsigned_leb(name.len() as u64));
        exports.extend_from_slice(name.as_bytes());
        exports.push(kind);
        exports.extend(unsigned_leb(u64::from(index)));
    }
    let mut body = Vec::new();
    for (index, (_, _, ty)) in imports.iter().enumerate() {
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
        body.push(OP_DROP);
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

fn bound(abi: u16, dynamic: bool, input: u32, output: u32) -> ProgramInterface {
    ProgramInterface::bind(
        &wasm(abi, dynamic),
        abi,
        vec![entry(abi, dynamic, input, output)],
    )
    .unwrap_or_else(|error| panic!("bind ABI {abi} dynamic {dynamic}: {error}"))
}

fn directory(variable: &str) -> PathBuf {
    PathBuf::from(std::env::var_os(variable).unwrap_or_else(|| panic!("missing {variable}")))
}

#[test]
fn emit_native_inputs() {
    let root = directory("PAXEER_X_INTERFACE_INPUTS");
    for (name, abi, dynamic) in CASES {
        let path = root.join(name);
        fs::create_dir_all(&path).expect("create case inputs");
        fs::write(path.join("module.wasm"), wasm(abi, dynamic)).expect("write actual module");
        fs::write(
            path.join("interface.bin"),
            bound(abi, dynamic, 64, 64).canonical_encoding(),
        )
        .expect("write canonical interface");
    }
    for (name, input, output) in [("abi2-widening", 128, 32), ("abi2-narrowing", 32, 128)] {
        let path = root.join(name);
        fs::create_dir_all(&path).expect("create upgrade input");
        fs::write(path.join("module.wasm"), wasm(2, false)).expect("write upgrade module");
        fs::write(
            path.join("interface.bin"),
            bound(2, false, input, output).canonical_encoding(),
        )
        .expect("write upgrade interface");
    }
}

fn unhex(text: &str) -> Vec<u8> {
    assert_eq!(text.len() % 2, 0, "odd hex length");
    text.as_bytes()
        .chunks_exact(2)
        .map(|pair| {
            let digit = |c: u8| match c {
                b'0'..=b'9' => c - b'0',
                b'a'..=b'f' => c - b'a' + 10,
                _ => panic!("noncanonical hex"),
            };
            (digit(pair[0]) << 4) | digit(pair[1])
        })
        .collect()
}

#[test]
fn canonical_abi_domains_and_legacy_vectors() {
    for (_, abi, dynamic) in CASES {
        let interface = bound(abi, dynamic, 64, 64);
        let version = if abi >= 3 {
            abi
        } else if dynamic {
            2
        } else {
            1
        };
        let domain = format!("LayerX/program-interface/v{version}\0");
        let mut expected = domain.into_bytes();
        expected.extend_from_slice(&Sha256::digest(wasm(abi, dynamic)));
        expected.extend_from_slice(&abi.to_be_bytes());
        expected.extend(unhex("0001000463616c6c10203040012000000040012000000040"));
        expected.extend_from_slice(&(capabilities(abi, dynamic).len() as u16).to_be_bytes());
        if dynamic {
            expected.push(10);
            expected.extend([2; 32]);
            expected.extend_from_slice(&100_u128.to_be_bytes());
            expected.extend_from_slice(&4_u32.to_be_bytes());
            expected.extend_from_slice(&36_u32.to_be_bytes());
        }
        if abi >= 3 {
            expected.push(11);
        }
        if abi >= 4 {
            expected.push(12);
        }
        expected.extend([0, 0, 0, 0]);
        assert_eq!(
            interface.canonical_encoding(),
            expected,
            "ABI {abi} frozen wire vector"
        );
        assert_eq!(
            interface.digest().as_bytes().as_slice(),
            Sha256::digest(&expected).as_slice()
        );
        assert_eq!(ProgramInterface::decode(&expected), Ok(interface));
        for wrong in [0_u16, 5, u16::MAX] {
            let mut bad = expected.clone();
            bad[DOMAIN_LEN + 32..DOMAIN_LEN + 34].copy_from_slice(&wrong.to_be_bytes());
            assert!(ProgramInterface::decode(&bad).is_err());
        }
        for wrong_domain in [b'0', b'1', b'2', b'3', b'4', b'5'] {
            if wrong_domain == b'0' + version as u8 {
                continue;
            }
            let mut bad = expected.clone();
            bad[DOMAIN_LEN - 2] = wrong_domain;
            assert!(ProgramInterface::decode(&bad).is_err());
        }
    }
}

#[test]
fn exact_capabilities_exports_and_upgrade_policy() {
    for (_, abi, dynamic) in CASES {
        let module = wasm(abi, dynamic);
        let original = entry(abi, dynamic, 64, 64);
        let engine = WasmEngine::declared().expect("declared real engine");
        let validated = match abi {
            1 => engine.validate(&module),
            2 => engine.validate_v2(&module),
            3 => engine.validate_v3(&module),
            4 => engine.validate_v4(&module),
            _ => unreachable!(),
        }
        .expect("real imported module validation");
        let mask = if dynamic { 1 << 7 } else { 0 }
            | if abi >= 3 { 1 << 10 } else { 0 }
            | if abi >= 4 { 1 << 11 } else { 0 };
        assert!(validated.interface_capability_mask_matches("call", mask));
        for bit in 0..12 {
            assert!(!validated.interface_capability_mask_matches("call", mask ^ (1 << bit)));
        }
        let mut missing = original.clone();
        missing.name = "missing".to_owned();
        assert_eq!(
            ProgramInterface::bind(&module, abi, vec![missing]),
            Err(InterfaceRefusal::MissingExport)
        );
        let mut excess = original.clone();
        excess
            .capabilities
            .insert(0, InterfaceCapability::StorageRead);
        assert!(ProgramInterface::bind(&module, abi, vec![excess]).is_err());
        if !original.capabilities.is_empty() {
            let mut omitted = original.clone();
            omitted.capabilities.pop();
            assert!(ProgramInterface::bind(&module, abi, vec![omitted]).is_err());
        }
        let prior = bound(abi, dynamic, 64, 64);
        assert_eq!(
            bound(abi, dynamic, 128, 32).authorize_upgrade(&prior, false),
            Ok(())
        );
        let narrower = bound(abi, dynamic, 32, 128);
        assert_eq!(
            narrower.authorize_upgrade(&prior, false),
            Err(InterfaceRefusal::NarrowingUpgrade)
        );
        assert_eq!(narrower.authorize_upgrade(&prior, true), Ok(()));
        for unsupported in [0, 5, u16::MAX] {
            assert!(ProgramInterface::bind(&module, unsupported, vec![original.clone()]).is_err());
        }
    }
    for abi in [1, 2] {
        assert!(
            ProgramInterface::bind(&wasm(3, false), abi, vec![entry(3, false, 64, 64)]).is_err()
        );
        assert!(
            ProgramInterface::bind(&wasm(4, false), abi, vec![entry(4, false, 64, 64)]).is_err()
        );
    }
    assert!(ProgramInterface::bind(&wasm(4, false), 3, vec![entry(4, false, 64, 64)]).is_err());
    assert!(ProgramInterface::bind(&wasm(2, true), 1, vec![entry(2, true, 64, 64)]).is_err());
    for newer in [2, 3, 4] {
        for older in 1..newer {
            assert!(bound(older, false, 64, 64)
                .authorize_upgrade(&bound(newer, false, 64, 64), true)
                .is_err());
        }
    }
    let mut corrupt = wasm(4, false);
    corrupt[0] ^= 1;
    assert!(ProgramInterface::bind(&corrupt, 4, vec![entry(4, false, 64, 64)]).is_err());
    assert!(ProgramInterface::bind(&vec![0; 1_048_577], 4, vec![entry(4, false, 64, 64)]).is_err());
}

#[test]
fn canonical_encoding_refuses_malformed_bounds() {
    let canonical = bound(4, false, 64, 64).canonical_encoding().to_vec();
    for length in 0..canonical.len() {
        assert!(ProgramInterface::decode(&canonical[..length]).is_err());
    }
    let mut appended = canonical.clone();
    appended.push(0);
    assert!(ProgramInterface::decode(&appended).is_err());
    assert!(ProgramInterface::decode(&vec![0; 953]).is_err());
    let mut largest = canonical[..canonical.len() - 4].to_vec();
    largest.extend_from_slice(&26_u16.to_be_bytes());
    for index in 0..26_u8 {
        let mut topic = [0; 32];
        topic[31] = index;
        largest.extend(topic);
    }
    largest.extend([0, 0]);
    let padding = 952 - largest.len();
    let name_offset = DOMAIN_LEN + 36;
    largest[name_offset..name_offset + 2].copy_from_slice(
        &u16::try_from(4 + padding)
            .expect("bounded name")
            .to_be_bytes(),
    );
    largest.splice(name_offset + 6..name_offset + 6, vec![b'x'; padding]);
    assert_eq!(largest.len(), 952);
    assert!(ProgramInterface::decode(&largest).is_ok());
    largest.push(0);
    assert!(ProgramInterface::decode(&largest).is_err());
    for count in [0_u16, 257, u16::MAX] {
        let mut bad = canonical.clone();
        bad[DOMAIN_LEN + 34..DOMAIN_LEN + 36].copy_from_slice(&count.to_be_bytes());
        assert!(ProgramInterface::decode(&bad).is_err());
    }
    let cap_offset = DOMAIN_LEN + 36 + 2 + 4 + 4 + 6 + 6 + 2;
    for tags in [[12, 11], [11, 11], [11, 13], [0xff, 12]] {
        let mut bad = canonical.clone();
        bad[cap_offset..cap_offset + 2].copy_from_slice(&tags);
        assert!(ProgramInterface::decode(&bad).is_err());
    }
    for abi in [1_u16, 2, 3] {
        let mut bad = canonical.clone();
        bad[DOMAIN_LEN - 2] = b'0' + abi as u8;
        bad[DOMAIN_LEN + 32..DOMAIN_LEN + 34].copy_from_slice(&abi.to_be_bytes());
        assert!(ProgramInterface::decode(&bad).is_err());
    }
    let module = wasm(4, false);
    let mut malformed = entry(4, false, 64, 64);
    malformed.name = "x".repeat(129);
    assert!(ProgramInterface::bind(&module, 4, vec![malformed]).is_err());
    let mut malformed = entry(4, false, 64, 64);
    malformed.event_topics = vec![[1; 32]; 257];
    assert!(ProgramInterface::bind(&module, 4, vec![malformed]).is_err());
    let mut value = ValueType::U8;
    for _ in 0..18 {
        value = ValueType::Option(Box::new(value));
    }
    let mut malformed = entry(4, false, 64, 64);
    malformed.calldata = ValueSchema::layerx(value);
    assert!(ProgramInterface::bind(&module, 4, vec![malformed]).is_err());
    let mut malformed = entry(4, false, 64, 64);
    malformed.capabilities.reverse();
    assert!(ProgramInterface::bind(&module, 4, vec![malformed]).is_err());
    assert!(ProgramInterface::bind(&module, 4, vec![entry(4, false, 64, 64); 257]).is_err());
    for offset in [u32::MAX, u32::MAX - 15] {
        let mut malformed = entry(2, true, 64, 64);
        malformed.capabilities = vec![InterfaceCapability::CallerAuthorizedSpend {
            asset: [2; 32],
            maximum_amount: 100,
            recipient_offset: offset,
            amount_offset: offset,
        }];
        assert!(ProgramInterface::bind(&wasm(2, true), 2, vec![malformed]).is_err());
    }
}

struct NativeEvidence {
    fields: BTreeMap<String, String>,
}
impl NativeEvidence {
    fn read(path: &Path) -> Self {
        let text = fs::read_to_string(path).expect("native evidence.kvx must exist");
        let mut fields = BTreeMap::new();
        for line in text.lines() {
            let (key, value) = line.split_once('=').expect("canonical evidence key=value");
            assert!(
                fields.insert(key.to_owned(), value.to_owned()).is_none(),
                "duplicate evidence key"
            );
        }
        Self { fields }
    }
    fn text(&self, key: &str) -> &str {
        self.fields
            .get(key)
            .unwrap_or_else(|| panic!("missing native field {key}"))
    }
    fn bytes(&self, key: &str) -> Vec<u8> {
        unhex(self.text(key))
    }
    fn fixed<const N: usize>(&self, key: &str) -> [u8; N] {
        self.bytes(key)
            .try_into()
            .unwrap_or_else(|_| panic!("wrong length {key}"))
    }
    fn number(&self, key: &str) -> u64 {
        self.text(key).parse().expect("decimal evidence field")
    }
    fn state_proof(&self, prefix: &str) -> StateProof {
        let siblings = self.text(&format!("{prefix}.siblings"));
        StateProof {
            leaf_index: u32::try_from(self.number(&format!("{prefix}.leaf_index")))
                .expect("proof index"),
            leaf_count: u32::try_from(self.number(&format!("{prefix}.leaf_count")))
                .expect("proof count"),
            siblings: if siblings.is_empty() {
                Vec::new()
            } else {
                siblings
                    .split(',')
                    .map(|hex| unhex(hex).try_into().expect("sibling length"))
                    .collect()
            },
        }
    }
    fn inclusion(&self, prefix: &str) -> Proof {
        let proof = self.state_proof(prefix);
        Proof::new(proof.leaf_index, proof.leaf_count, proof.siblings)
            .expect("native inclusion proof")
    }
    fn witness(&self, prefix: &str) -> StateLeafWitness {
        StateLeafWitness {
            key: self.bytes(&format!("{prefix}.key")),
            value: self.bytes(&format!("{prefix}.value")),
            proof: self.state_proof(&format!("{prefix}.proof")),
        }
    }
    fn optional(&self, prefix: &str) -> Option<StateLeafWitness> {
        if self
            .fields
            .get(prefix)
            .is_some_and(|value| value == "absent")
        {
            None
        } else {
            Some(self.witness(prefix))
        }
    }
    fn deployment(&self) -> DeploymentProof {
        DeploymentProof {
            activity: self.bytes("activity"),
            activity_proof: self.inclusion("activity_proof"),
            maintenance: None,
            state: ProgramStateProof {
                receipt: self.bytes("receipt"),
                receipt_proof: self.inclusion("receipt_proof"),
                header: self.bytes("header"),
                header_signature: self.fixed("header_signature"),
                programs_root: self.fixed("programs_root"),
                programs_root_proof: self.state_proof("programs_root_proof"),
                program_record: self.witness("program_record"),
                lifecycle: ProgramLifecycleProof::Active {
                    lower: self.optional("lifecycle_lower"),
                    upper: self.optional("lifecycle_upper"),
                },
            },
        }
    }
    fn verifier(&self, path: &Path) -> ProtocolDeploymentVerifier {
        let mut bytes = b"LayerX/sequencer-trust-history/v1\0".to_vec();
        bytes.extend([0, 1, 0, 0]);
        bytes.extend_from_slice(
            &u16::try_from(self.number("protocol_version"))
                .expect("protocol")
                .to_be_bytes(),
        );
        bytes.extend_from_slice(
            &u32::try_from(self.number("network_id"))
                .expect("network")
                .to_be_bytes(),
        );
        bytes.extend_from_slice(&self.number("epoch").to_be_bytes());
        bytes.extend_from_slice(&self.fixed::<32>("sequencer_id"));
        bytes.extend_from_slice(&self.fixed::<32>("sequencer_key"));
        bytes.extend_from_slice(&self.number("first_batch").to_be_bytes());
        bytes.extend_from_slice(&self.number("last_batch").to_be_bytes());
        bytes.push(0);
        bytes.extend_from_slice(&0_u64.to_be_bytes());
        fs::write(path, bytes).expect("write real native signer trust policy");
        #[cfg(unix)]
        {
            use std::os::unix::fs::PermissionsExt;
            fs::set_permissions(path, fs::Permissions::from_mode(0o600))
                .expect("protect trust policy");
        }
        ProtocolDeploymentVerifier::from_protected_history(path, 1000)
            .expect("protected native trust")
    }
}

#[test]
fn native_receipt_authorized_interface_reads() {
    let root = directory("PAXEER_X_INTERFACE_FIXTURES");
    for (name, abi, dynamic) in CASES {
        verify_native_case(&root, name, bound(abi, dynamic, 64, 64));
    }
    verify_native_case(&root, "upgrade-widening", bound(2, false, 128, 32));
    verify_native_case(&root, "upgrade-breaking", bound(2, false, 32, 128));
    for name in ["upgrade-narrowing", "upgrade-downgrade"] {
        let path = root.join(name);
        let evidence = NativeEvidence::read(&path.join("evidence.kvx"));
        let verifier = evidence.verifier(&path.join("trust.bin"));
        assert_eq!(
            evidence.text("result_code"),
            if name == "upgrade-narrowing" {
                "-3"
            } else {
                "-101"
            }
        );
        assert!(
            verifier
                .verify_deployment(&evidence.deployment(), evidence.number("now_ms"))
                .is_err(),
            "{name} must refuse"
        );
    }
}

fn verify_native_case(root: &Path, name: &str, expected: ProgramInterface) {
    let path = root.join(name);
    let evidence = NativeEvidence::read(&path.join("evidence.kvx"));
    let verifier = evidence.verifier(&path.join("trust.bin"));
    let now = evidence.number("now_ms");
    let proof = evidence.deployment();
    assert_eq!(
        evidence.text("result_code"),
        "0",
        "{name} successful native receipt required"
    );
    assert_eq!(
        DeploymentProof::decode(&proof.canonical_encoding()).expect("native proof roundtrip"),
        proof
    );
    let deployment = verifier
        .verify_deployment(&proof, now)
        .unwrap_or_else(|error| panic!("{name}: real native deployment: {error}"));
    assert_eq!(
        ProgramInterface::bind_deployment(&deployment, expected.entries().to_vec()),
        Ok(expected.clone())
    );
    let program = ProgramId::new(evidence.fixed("program_id")).expect("native program ID");
    let head = verifier
        .verify_current_program(&proof.state, program, now)
        .expect("real current program");
    let record = evidence.witness("interface_record");
    let witness = InterfaceStateWitness {
        key: record.key,
        value: record.value,
        proof: record.proof,
    };
    let read = verify_interface_read(&head, &witness).expect("receipt-authorized native interface");
    assert_eq!(read.interface, expected);
    assert_eq!(read.receipt_digest, head.receipt_digest());
    assert_eq!(read.state_root, head.state_root());
    let mut bad = proof.clone();
    bad.state.header_signature[0] ^= 1;
    assert!(verifier.verify_deployment(&bad, now).is_err());
    let mut bad = proof.clone();
    bad.state.receipt[0] ^= 1;
    assert!(verifier.verify_deployment(&bad, now).is_err());
    let mut bad = proof.clone();
    bad.activity[0] ^= 1;
    assert!(verifier.verify_deployment(&bad, now).is_err());
    let mut bad = proof.clone();
    bad.state.programs_root[0] ^= 1;
    assert!(verifier.verify_deployment(&bad, now).is_err());
    let mut bad = proof.clone();
    bad.state.program_record.value[0] ^= 1;
    assert!(verifier.verify_deployment(&bad, now).is_err());
    assert!(verifier.verify_deployment(&proof, now + 1001).is_err());
    for index in [0, 32, 36, 68, witness.value.len() - 1] {
        let mut bad = witness.clone();
        bad.value[index] ^= 1;
        assert!(verify_interface_read(&head, &bad).is_err());
    }
    let mut bad = witness.clone();
    bad.key[0] ^= 1;
    assert!(verify_interface_read(&head, &bad).is_err());
    let mut bad = witness.clone();
    bad.proof.leaf_count = 0;
    assert!(verify_interface_read(&head, &bad).is_err());
}
