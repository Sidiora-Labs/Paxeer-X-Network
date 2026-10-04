use std::{collections::BTreeMap, fs, path::Path};

use layerx_client::evidence::{verify_arbiter_admission_v3, VerifiedAdmissionPrestate};
use layerx_programs_arbiter::{
    AuthenticatedCatalogue, BoundaryProof, ReplayError, VerifiedReplay, VerifiedReplayAuthority,
};
use layerx_programs_runtime::portable_replay::PortableReplayRecord;
use layerx_proof::{
    inclusion::{verify_header, verify_receipt, SequencerAuthorization, VerifiedBatchHeader},
    merkle::{decode_proof, Proof},
    receipt::{
        verify_outcome_maintained_chain, verify_program_preexecution_rejection_maintained_chain,
        AuthorizedBatch, MaintainedOutcomeEvidence, VerifiedReceipt,
    },
    state_witness::StateWitness,
};
use serde_json::Value;
use sha2::{Digest, Sha256};

fn field<'a>(v: &'a Value, key: &str) -> &'a str {
    v[key]
        .as_str()
        .unwrap_or_else(|| panic!("required native {key}"))
}
fn read(v: &Value, key: &str) -> Vec<u8> {
    fs::read(field(v, key)).expect("genuine native file")
}
fn hash(v: &Value, key: &str) -> [u8; 32] {
    let s = field(v, key);
    assert_eq!(s.len(), 64);
    std::array::from_fn(|i| {
        u8::from_str_radix(&s[2 * i..2 * i + 2], 16).expect("native pinned hash")
    })
}
fn verified(
    v: &Value,
    authorization: &SequencerAuthorization,
) -> (VerifiedReceipt, VerifiedBatchHeader, Proof) {
    let bytes = read(v, "receipt_path");
    let header = read(v, "header_path");
    let signature: [u8; 64] = read(v, "header_signature_path")
        .try_into()
        .expect("actual header signature");
    let signed =
        verify_header(&header, &signature, authorization).expect("genuine sequencer header");
    let proof = decode_proof(&read(v, "proof_path")).expect("actual receipt Merkle proof");
    verify_receipt(&bytes, &proof, &header, &signature, authorization)
        .expect("actual signed receipt inclusion");
    let maintenance = read(v, "maintenance_path");
    let maintenance_proof =
        decode_proof(&read(v, "maintenance_proof_path")).expect("actual maintenance proof");
    verify_receipt(
        &maintenance,
        &maintenance_proof,
        &header,
        &signature,
        authorization,
    )
    .expect("maintenance inclusion");
    let record = layerx_wire::batch_maintenance::decode_maintenance(&maintenance)
        .expect("maintenance decode");
    record
        .verify_header(signed.header())
        .expect("maintenance exact signed header");
    let decoded = layerx_wire::receipt::decode(&bytes).expect("canonical receipt");
    let protocol = decoded.protocol().expect("protocol receipt");
    let count = u32::try_from(signed.header().last_sequence() - signed.header().first_sequence())
        .expect("receipt count");
    let batch_id = layerx_wire::hash::receipt_execution_batch_id_maintenance(
        protocol,
        signed.header(),
        record.occupancy(),
        count,
    )
    .expect("actual execution batch id");
    assert_eq!(batch_id, protocol.batch_id());
    let batch = AuthorizedBatch::new(
        batch_id,
        protocol.asset(),
        signed.header().previous_state_root(),
        signed.header().resulting_state_root(),
        authorization.public_key(),
    );
    let receipts: Vec<Vec<u8>> = v["receipts"]
        .as_array()
        .expect("complete receipt chain")
        .iter()
        .map(|p| fs::read(p.as_str().expect("receipt path")).expect("real receipt chain entry"))
        .collect();
    assert_eq!(receipts.len(), count as usize);
    let evidence = MaintainedOutcomeEvidence {
        header: &header,
        header_signature: &signature,
        activity_proof: &proof,
        maintenance: &maintenance,
        maintenance_proof: &maintenance_proof,
        authorization,
    };
    let verified = if protocol.module_id() == 9
        && protocol.operation() == 3
        && protocol.result_code() != 0
        && protocol.program_outcome().is_none()
    {
        verify_program_preexecution_rejection_maintained_chain(&bytes, &batch, &evidence, &receipts)
    } else {
        verify_outcome_maintained_chain(&bytes, &batch, &evidence, &receipts)
    }
    .expect("real maintained receipt chain");
    let mut bad_signature = signature;
    bad_signature[0] ^= 1;
    assert!(verify_header(&header, &bad_signature, authorization).is_err());
    (verified, signed, proof)
}

struct Capture {
    path: String,
    receipt: VerifiedReceipt,
    header: VerifiedBatchHeader,
    receipt_proof: Proof,
    admission: VerifiedAdmissionPrestate,
}
struct Fixture {
    directory: std::path::PathBuf,
    replay: Value,
    captures: Vec<Capture>,
    code: BTreeMap<[u8; 32], Vec<u8>>,
}
impl Fixture {
    fn load() -> Self {
        let path = std::env::var("LAYERX_AUTHENTICATED_REPLAY_INPUTS")
            .expect("required genuine native replay corpus; no fallback");
        let directory = Path::new(&path)
            .parent()
            .expect("native corpus parent")
            .to_path_buf();
        let replay: Value =
            serde_json::from_slice(&fs::read(path).expect("native replay manifest"))
                .expect("native replay JSON");
        let inputs = std::env::var("LAYERX_ARBITER_ADMISSION_INPUTS")
            .expect("required real signed admission corpus");
        let manifest: Value =
            serde_json::from_slice(&fs::read(inputs).expect("native admission manifest"))
                .expect("native admission JSON");
        let network = u32::try_from(manifest["network_id"].as_u64().expect("network"))
            .expect("network width");
        let authorization = SequencerAuthorization::new(
            hash(&manifest, "sequencer_id"),
            hash(&manifest, "sequencer_public_key"),
            manifest["first_batch_number"]
                .as_u64()
                .expect("first batch"),
            manifest["last_batch_number"].as_u64().expect("last batch"),
        );
        let mut code = BTreeMap::new();
        let captures = manifest["captures"]
            .as_array()
            .expect("actual captures")
            .iter()
            .map(|v| {
                let (receipt, header, receipt_proof) = verified(v, &authorization);
                let admission = verify_arbiter_admission_v3(&read(v, "v3_path"), &receipt, network)
                    .expect("genuine sealed V3 prestate");
                if admission.activity().activity_type().module()
                    == layerx_types::payload::ModuleId::Programs
                    && admission.activity().activity_type().ordinal() == 1
                {
                    let payload = admission.activity().payload();
                    assert!(payload.len() >= 108);
                    let length = u32::from_be_bytes(
                        payload[100..104].try_into().expect("deploy code length"),
                    ) as usize;
                    let interface_length = u32::from_be_bytes(
                        payload[104..108]
                            .try_into()
                            .expect("deploy interface length"),
                    ) as usize;
                    let start = 108usize
                        .checked_add(interface_length)
                        .expect("bounded interface");
                    assert_eq!(start.checked_add(length), Some(payload.len()));
                    let wasm = payload[start..].to_vec();
                    let digest: [u8; 32] = Sha256::digest(&wasm).into();
                    assert_eq!(&digest, &payload[68..100]);
                    code.insert(digest, wasm);
                }
                Capture {
                    path: field(v, "receipt_path").to_owned(),
                    receipt,
                    header,
                    receipt_proof,
                    admission,
                }
            })
            .collect();
        Self {
            directory,
            replay,
            captures,
            code,
        }
    }
    fn receipt(&self, replay: &Value) -> &Capture {
        let name = field(replay, "receipt");
        self.captures
            .iter()
            .find(|c| Path::new(&c.path).file_name().and_then(|p| p.to_str()) == Some(name))
            .expect("replay exact genuine receipt binding")
    }
    fn file(&self, v: &Value, key: &str) -> Vec<u8> {
        fs::read(self.directory.join(field(v, key))).expect("real replay evidence file")
    }
}

#[test]
fn genuine_signed_serial_scheduled_trap_steps_and_bound_refusals() {
    let fixture = Fixture::load();
    let captures = fixture.replay["captures"]
        .as_array()
        .expect("replay captures");
    assert_eq!(captures.len(), 4);
    let mut verified_instructions = 0;
    let mut verified_traps = 0;
    for capture in captures {
        let trusted = fixture.receipt(capture);
        let proof = StateWitness::decode(&fixture.file(capture, "metadata_proof"))
            .expect("native module9 proof");
        let portable =
            PortableReplayRecord::decode_untrusted(&fixture.file(capture, "witness"), 1_048_576)
                .expect("real native replay witness");
        let catalogue = AuthenticatedCatalogue::verify(&trusted.admission, &fixture.code)
            .expect("genuine authenticated complete catalogue");
        let replay = VerifiedReplay::verify(
            &trusted.receipt,
            &trusted.admission,
            &proof,
            &portable.authority_bytes,
            &portable.hosts_bytes,
            catalogue,
        )
        .expect("sealed replay authority");
        assert_eq!(
            replay.metadata().boundary_root,
            portable.record.boundary_root()
        );
        assert_eq!(
            replay.metadata().boundary_count as usize,
            portable.leaves.len()
        );
        assert_eq!(
            replay.metadata().witness_digest,
            portable.record.witness_digest()
        );
        let paths: Vec<BoundaryProof> = portable
            .leaves
            .iter()
            .enumerate()
            .map(|(i, leaf)| BoundaryProof {
                index: i as u32,
                leaf: leaf.clone(),
                siblings: portable
                    .merkle_path_untrusted(i as u32)
                    .expect("actual bounded Merkle path"),
            })
            .collect();
        let mut accepted = 0;
        for pair in paths.windows(2) {
            if let Ok(step) = replay.verify_step(&pair[0], &pair[1]) {
                assert_eq!(step.activity_id(), trusted.admission.activity_id());
                accepted += 1;
                let mut mutated = pair[0].clone();
                mutated.leaf[0] ^= 1;
                assert!(replay.verify_step(&mutated, &pair[1]).is_err());
                let mut index = pair[0].clone();
                index.index = replay.metadata().boundary_count;
                assert_eq!(
                    replay.verify_step(&index, &pair[1]),
                    Err(ReplayError::Bounds)
                );
                let mut path = pair[0].clone();
                path.siblings.push([0; 32]);
                assert!(replay.verify_step(&path, &pair[1]).is_err());
                assert!(replay.verify_step(&pair[1], &pair[0]).is_err());
            }
        }
        assert!(
            accepted > 0,
            "each genuine capture must replay actual engine instructions"
        );
        verified_instructions += accepted;
        if replay.metadata().terminal_status == 1 {
            let final_leaf = paths.last().expect("real terminal leaf");
            replay
                .verify_step(final_leaf, final_leaf)
                .expect("genuine final trap execute_step");
            verified_traps += 1;
        }
        let catalogue = || {
            AuthenticatedCatalogue::verify(&trusted.admission, &fixture.code)
                .expect("real catalogue")
        };
        let mut authority = portable.authority_bytes.clone();
        authority[0] ^= 1;
        assert!(VerifiedReplay::verify(
            &trusted.receipt,
            &trusted.admission,
            &proof,
            &authority,
            &portable.hosts_bytes,
            catalogue()
        )
        .is_err());
        let mut hosts = portable.hosts_bytes.clone();
        hosts[0] ^= 1;
        assert!(VerifiedReplay::verify(
            &trusted.receipt,
            &trusted.admission,
            &proof,
            &portable.authority_bytes,
            &hosts,
            catalogue()
        )
        .is_err());
        let mut metadata = proof.clone();
        metadata.value[32] ^= 1;
        assert!(VerifiedReplay::verify(
            &trusted.receipt,
            &trusted.admission,
            &metadata,
            &portable.authority_bytes,
            &portable.hosts_bytes,
            catalogue()
        )
        .is_err());
        let mut wrong_key = proof.clone();
        wrong_key.key[14] ^= 1;
        assert!(VerifiedReplay::verify(
            &trusted.receipt,
            &trusted.admission,
            &wrong_key,
            &portable.authority_bytes,
            &portable.hosts_bytes,
            catalogue()
        )
        .is_err());
        let mut changed_code = fixture.code.clone();
        let code = changed_code
            .get_mut(&replay.metadata().code_hash)
            .expect("real deployed code");
        code[0] ^= 1;
        assert!(AuthenticatedCatalogue::verify(&trusted.admission, &changed_code).is_err());
        assert!(AuthenticatedCatalogue::verify(&trusted.admission, &BTreeMap::new()).is_err());
    }
    assert!(verified_instructions >= 4);
    assert_eq!(verified_traps, 1);
}

#[test]
fn authenticated_admission_authority_reaches_real_bounded_step_verifier() {
    let fixture = Fixture::load();
    let captures = fixture.replay["captures"]
        .as_array()
        .expect("real canonical replay captures");
    assert_eq!(captures.len(), 4);
    let mut verified_instructions = 0;
    let mut verified_traps = 0;
    let mut verified_scheduled = 0;
    for capture in captures {
        let trusted = fixture.receipt(capture);
        let proof = StateWitness::decode(&fixture.file(capture, "metadata_proof"))
            .expect("real metadata state proof");
        let portable =
            PortableReplayRecord::decode_untrusted(&fixture.file(capture, "witness"), 1_048_576)
                .expect("real captured replay record");
        let authority = VerifiedReplayAuthority::derive(
            &trusted.receipt,
            &trusted.admission,
            &trusted.header,
            &trusted.receipt_proof,
        )
        .expect("authority independently derived from sealed historical admission");
        authority
            .verify_native(&portable.authority_bytes)
            .expect("canonical native authority equality");
        assert_eq!(authority.canonical_bytes(), portable.authority_bytes);
        assert_native_field_refusals(&authority, &portable.authority_bytes);
        let other_header = fixture
            .captures
            .iter()
            .find(|other| other.header.digest() != trusted.header.digest())
            .expect("genuine distinct signed batch header");
        assert!(VerifiedReplayAuthority::derive(
            &trusted.receipt,
            &trusted.admission,
            &other_header.header,
            &trusted.receipt_proof,
        )
        .is_err());
        let other_admission = fixture
            .captures
            .iter()
            .find(|other| other.admission.activity_id() != trusted.admission.activity_id())
            .expect("genuine distinct signed admission");
        assert!(VerifiedReplayAuthority::derive(
            &trusted.receipt,
            &other_admission.admission,
            &trusted.header,
            &trusted.receipt_proof,
        )
        .is_err());
        let mut siblings = trusted.receipt_proof.siblings().to_vec();
        siblings.first_mut().expect("real nonempty receipt proof")[0] ^= 1;
        let bad_proof = Proof::new(
            trusted.receipt_proof.leaf_index(),
            trusted.receipt_proof.leaf_count(),
            siblings,
        )
        .expect("same canonical Merkle proof shape");
        assert!(VerifiedReplayAuthority::derive(
            &trusted.receipt,
            &trusted.admission,
            &trusted.header,
            &bad_proof,
        )
        .is_err());
        let catalogue = || {
            AuthenticatedCatalogue::verify(&trusted.admission, &fixture.code)
                .expect("genuine previous-root catalogue and signed deployed code")
        };
        let replay = VerifiedReplay::verify_with_batch_context(
            &trusted.receipt,
            &trusted.admission,
            &trusted.header,
            &trusted.receipt_proof,
            &proof,
            &portable.authority_bytes,
            &portable.hosts_bytes,
            catalogue(),
        )
        .expect("sealed replay after independent authority comparison");
        assert_eq!(
            replay.metadata().boundary_root,
            portable.record.boundary_root()
        );
        assert_eq!(
            replay.metadata().boundary_count as usize,
            portable.leaves.len()
        );
        assert_eq!(
            replay.metadata().witness_digest,
            portable.record.witness_digest()
        );
        let paths: Vec<BoundaryProof> = portable
            .leaves
            .iter()
            .enumerate()
            .map(|(index, leaf)| BoundaryProof {
                index: index as u32,
                leaf: leaf.clone(),
                siblings: portable
                    .merkle_path_untrusted(index as u32)
                    .expect("actual boundary Merkle path"),
            })
            .collect();
        let mut accepted = 0;
        for pair in paths.windows(2) {
            if let Ok(step) = replay.verify_step(&pair[0], &pair[1]) {
                assert_eq!(step.activity_id(), trusted.admission.activity_id());
                accepted += 1;
                let mut bad_leaf = pair[0].clone();
                bad_leaf.leaf[0] ^= 1;
                assert!(replay.verify_step(&bad_leaf, &pair[1]).is_err());
                let mut bad_index = pair[0].clone();
                bad_index.index = replay.metadata().boundary_count;
                assert_eq!(
                    replay.verify_step(&bad_index, &pair[1]),
                    Err(ReplayError::Bounds)
                );
                let mut bad_path = pair[0].clone();
                bad_path.siblings.push([0; 32]);
                assert!(replay.verify_step(&bad_path, &pair[1]).is_err());
                assert!(replay.verify_step(&pair[1], &pair[0]).is_err());
            }
        }
        assert!(
            accepted > 0,
            "each canonical native capture must replay actual instructions"
        );
        verified_instructions += accepted;
        if capture["receipt_index"].as_u64() == Some(1) {
            verified_scheduled += 1;
        }
        if replay.metadata().terminal_status == 1 {
            let terminal = paths.last().expect("real terminal boundary");
            replay
                .verify_step(terminal, terminal)
                .expect("actual canonical trap instruction");
            verified_traps += 1;
        }
        let mut bad_authority = portable.authority_bytes.clone();
        bad_authority[0] ^= 1;
        assert!(VerifiedReplay::verify_with_batch_context(
            &trusted.receipt,
            &trusted.admission,
            &trusted.header,
            &trusted.receipt_proof,
            &proof,
            &bad_authority,
            &portable.hosts_bytes,
            catalogue(),
        )
        .is_err());
        let mut bad_hosts = portable.hosts_bytes.clone();
        bad_hosts[0] ^= 1;
        assert!(VerifiedReplay::verify_with_batch_context(
            &trusted.receipt,
            &trusted.admission,
            &trusted.header,
            &trusted.receipt_proof,
            &proof,
            &portable.authority_bytes,
            &bad_hosts,
            catalogue(),
        )
        .is_err());
        let mut bad_metadata = proof.clone();
        bad_metadata.value[32] ^= 1;
        assert!(VerifiedReplay::verify_with_batch_context(
            &trusted.receipt,
            &trusted.admission,
            &trusted.header,
            &trusted.receipt_proof,
            &bad_metadata,
            &portable.authority_bytes,
            &portable.hosts_bytes,
            catalogue(),
        )
        .is_err());
        let mut bad_key = proof.clone();
        bad_key.key[14] ^= 1;
        assert!(VerifiedReplay::verify_with_batch_context(
            &trusted.receipt,
            &trusted.admission,
            &trusted.header,
            &trusted.receipt_proof,
            &bad_key,
            &portable.authority_bytes,
            &portable.hosts_bytes,
            catalogue(),
        )
        .is_err());
        let mut bad_code = fixture.code.clone();
        bad_code
            .get_mut(&replay.metadata().code_hash)
            .expect("actual deployed code")[0] ^= 1;
        assert!(AuthenticatedCatalogue::verify(&trusted.admission, &bad_code).is_err());
        assert!(AuthenticatedCatalogue::verify(&trusted.admission, &BTreeMap::new()).is_err());
    }
    assert!(verified_instructions >= 4);
    assert_eq!(verified_traps, 1);
    assert_eq!(verified_scheduled, 1);
}

fn assert_native_field_refusals(authority: &VerifiedReplayAuthority, bytes: &[u8]) {
    let domain = b"LXP/program-replay-authority/v1\0";
    assert!(bytes.starts_with(domain));
    let length = u32::from_be_bytes(
        bytes[domain.len()..domain.len() + 4]
            .try_into()
            .expect("native signed activity length"),
    ) as usize;
    let base = domain
        .len()
        .checked_add(4)
        .and_then(|value| value.checked_add(length))
        .expect("bounded signed activity");
    let footer = bytes
        .len()
        .checked_sub(272)
        .expect("exact canonical admission footer");
    assert_eq!(
        u64::from_be_bytes(
            bytes[base + 168..base + 176]
                .try_into()
                .expect("real scope marker")
        ),
        1
    );
    let scope = base + 176;
    let signer_count = usize::try_from(u64::from_be_bytes(
        bytes[scope + 208..scope + 216]
            .try_into()
            .expect("real signer count"),
    ))
    .expect("signer count width");
    let approval_count_offset = scope
        .checked_add(216)
        .and_then(|value| value.checked_add(signer_count.checked_mul(32)?))
        .expect("bounded signer list");
    let approval_count = usize::try_from(u64::from_be_bytes(
        bytes[approval_count_offset..approval_count_offset + 8]
            .try_into()
            .expect("real approval count"),
    ))
    .expect("approval count width");
    assert_eq!(
        approval_count_offset
            .checked_add(8)
            .and_then(|value| value.checked_add(approval_count.checked_mul(32)?)),
        Some(footer)
    );
    let mut fields = vec![
        ("signed activity", domain.len() + 4),
        ("actor", base),
        ("principal", base + 32),
        ("authority kind", base + 64),
        ("verified key", base + 72),
        ("authority hash", base + 104),
        ("grant", base + 136),
        ("scope selection", base + 168),
        ("activity binding", footer),
        ("payer", footer + 32),
        ("available fee units", footer + 64),
        ("signed fee limit", footer + 80),
        ("fee version", footer + 96),
        ("meter version", footer + 104),
        ("parameter version", footer + 112),
        ("protocol", footer + 248),
        ("batch", footer + 256),
        ("epoch", footer + 264),
    ];
    fields.extend([
        ("scope module mask", scope),
        ("scope ordinal minimum", scope + 8),
        ("scope ordinal maximum", scope + 16),
        ("scope asset", scope + 24),
        ("scope per-activity limit", scope + 56),
        ("scope total limit", scope + 72),
        ("scope spent total", scope + 88),
        ("scope period length", scope + 104),
        ("scope period limit", scope + 112),
        ("scope period spent", scope + 128),
        ("scope period start", scope + 144),
        ("scope purpose", scope + 152),
        ("scope earliest sequence", scope + 184),
        ("scope earliest timestamp", scope + 192),
        ("scope signer threshold", scope + 200),
        ("scope signer count", scope + 208),
        ("scope approval count", approval_count_offset),
    ]);
    for index in 0..signer_count {
        fields.push(("scope signer", scope + 216 + index * 32));
    }
    for index in 0..approval_count {
        fields.push(("scope approval", approval_count_offset + 8 + index * 32));
    }
    for index in 0..9 {
        fields.push(("meter coefficient", footer + 120 + index * 8));
    }
    for index in 0..7 {
        fields.push(("fee price", footer + 192 + index * 8));
    }
    for (name, index) in fields {
        let mut changed = bytes.to_vec();
        changed[index] ^= 1;
        assert!(
            authority.verify_native(&changed).is_err(),
            "uncommitted {name} refused independently of metadata hash"
        );
    }
    for length in [0, domain.len(), bytes.len() - 1] {
        assert!(authority.verify_native(&bytes[..length]).is_err());
    }
    let mut trailing = bytes.to_vec();
    trailing.push(0);
    assert!(authority.verify_native(&trailing).is_err());
}
