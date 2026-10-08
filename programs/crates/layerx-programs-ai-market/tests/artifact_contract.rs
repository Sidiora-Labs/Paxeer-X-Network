//! F09 codec-only cases AI.F09-A26..A33 with real SHA256 and real Ed25519.
//! No publication, grant admission, availability, quota or protocol state.
use ed25519_dalek::{Signer, SigningKey};
use layerx_programs_ai_market::evidence::*;
use layerx_programs_ai_market::types::*;
use sha2::{Digest, Sha256};
use std::fmt::Debug;

fn ok<T, E: Debug>(result: Result<T, E>) -> T {
    result.unwrap_or_else(|error| panic!("unexpected refusal: {error:?}"))
}
fn err<T: Debug, E: Debug + PartialEq>(result: Result<T, E>, expected: E) {
    match result {
        Ok(value) => panic!("expected {expected:?}, accepted {value:?}"),
        Err(error) => assert_eq!(error, expected),
    }
}
fn sha(parts: &[&[u8]]) -> [u8; 32] {
    let mut h = Sha256::new();
    for part in parts {
        h.update(part);
    }
    h.finalize().into()
}
fn v(value: u64) -> Version {
    ok(Version::new(value))
}
fn ctx_with(chain: u8, program: u8, market: u8, policy: u8) -> ArtifactContext {
    ArtifactContext {
        chain: ok(ChainDomain::new([chain; 32])),
        program: ok(ProgramId::new([program; 32])),
        market: ok(MarketId::new([market; 32])),
        policy: ok(PolicyDigest::new([policy; 32])),
    }
}
fn ctx() -> ArtifactContext {
    ctx_with(0x11, 0x22, 0x33, 0x44)
}
fn ctx_bytes() -> Vec<u8> {
    [[0x11u8; 32], [0x22; 32], [0x33; 32], [0x44; 32]].concat()
}
fn task(byte: u8) -> TaskId {
    ok(TaskId::new([byte; 32]))
}
fn manifest(byte_length: u64, chunk_count: u32, content: [u8; 32]) -> ArtifactManifest<'static> {
    ArtifactManifest {
        kind: ArtifactKind::Result,
        privacy: Privacy::Public,
        context: ctx(),
        epoch: 9,
        publisher: ok(PrincipalId::new([0x55; 32])),
        subject: [0x31; 32],
        byte_length,
        chunk_count,
        content_root: content,
        parents: Items::Typed(&[]),
        declaration_root: [0; 32],
        reproduction_root: [0; 32],
        access_policy_root: [0; 32],
        not_after_height: 0,
    }
}
fn encode(value: &ArtifactManifest<'_>) -> Vec<u8> {
    let mut out = vec![0; MAX_MANIFEST_BYTES];
    let n = ok(encode_manifest(value, &mut out));
    out.truncate(n);
    out
}
fn content_of(object: &[u8]) -> [u8; 32] {
    let mut scratch = vec![[0u8; 32]; object.len().div_ceil(CHUNK_BYTES as usize)];
    ok(object_content_root(object, &mut scratch))
}
fn manifest_for(object: &[u8]) -> Vec<u8> {
    let n = object.len().div_ceil(CHUNK_BYTES as usize) as u32;
    encode(&manifest(object.len() as u64, n, content_of(object)))
}
fn signer(seed: u8) -> SigningKey {
    SigningKey::from_bytes(&[seed; 32])
}
fn envelope_bytes(
    manifest: &[u8],
    generation: u64,
    key: &SigningKey,
    digest: &[u8; 32],
) -> Vec<u8> {
    let envelope = PublisherEnvelope {
        manifest,
        generation: v(generation),
        key: PublicKey32(key.verifying_key().to_bytes()),
        signature: Signature64(key.sign(digest).to_bytes()),
    };
    let mut out = vec![0; MAX_ENVELOPE_BYTES];
    let n = ok(encode_envelope(&envelope, &mut out));
    out.truncate(n);
    out
}
fn mutate(bytes: &[u8], at: usize, value: u8) -> Vec<u8> {
    let mut out = bytes.to_vec();
    out[at] = value;
    out
}
fn appended(bytes: &[u8]) -> Vec<u8> {
    let mut out = bytes.to_vec();
    out.push(0);
    out
}

#[test]
fn a26_codec_known_answer_result_commitment() {
    let object = [1u8, 2, 3];
    let leaf = sha(&[
        b"PAXAI/artifact-chunk/v1\0",
        &0u32.to_be_bytes(),
        &3u32.to_be_bytes(),
        &object,
    ]);
    assert_eq!(ok(chunk_leaf(0, &object)), leaf);
    let mut leaves = [leaf];
    assert_eq!(tree_root(&mut leaves), leaf);
    let content = sha(&[
        b"PAXAI/artifact-content/v1\0",
        &3u64.to_be_bytes(),
        &1u32.to_be_bytes(),
        &leaf,
    ]);
    assert_eq!(content_of(&object), content);

    let mut expected = Vec::new();
    expected.extend_from_slice(b"PAXAIF09");
    expected.extend_from_slice(&1u16.to_be_bytes());
    expected.extend_from_slice(&[5, 0, 0, 0, 0, 0]);
    expected.extend_from_slice(&ctx_bytes());
    expected.extend_from_slice(&9u64.to_be_bytes());
    expected.extend_from_slice(&[0x55; 32]);
    expected.extend_from_slice(&[0x31; 32]);
    expected.extend_from_slice(&3u64.to_be_bytes());
    expected.extend_from_slice(&262_144u32.to_be_bytes());
    expected.extend_from_slice(&1u32.to_be_bytes());
    expected.extend_from_slice(&content);
    expected.extend_from_slice(&0u16.to_be_bytes());
    expected.extend_from_slice(&[0; 96]);
    expected.extend_from_slice(&0u64.to_be_bytes());
    expected.extend_from_slice(&[0; 16]);
    assert_eq!(expected.len(), MANIFEST_FIXED_BYTES);
    let bytes = encode(&manifest(3, 1, content));
    assert_eq!(bytes, expected);
    assert_eq!(encode(&ok(decode_manifest(&bytes))), bytes);

    let root = sha(&[
        b"PAXAI/artifact-manifest/v1\0",
        &ctx_bytes(),
        &(bytes.len() as u32).to_be_bytes(),
        &bytes,
    ]);
    let unsigned_root = ok(manifest_root(&bytes));
    assert_eq!(unsigned_root.bytes(), root);
    let digest = sha(&[
        b"PAXAI/artifact-publisher/v1\0",
        &ctx_bytes(),
        &root,
        &7u64.to_be_bytes(),
    ]);
    assert_eq!(publisher_digest(&ctx(), unsigned_root, v(7)), digest);

    let first = envelope_bytes(&bytes, 7, &signer(0x73), &digest);
    let second = envelope_bytes(&bytes, 7, &signer(0x74), &digest);
    assert_ne!(first, second);
    let verified_first = ok(verify_publisher(&ok(decode_envelope(&first))));
    let verified_second = ok(verify_publisher(&ok(decode_envelope(&second))));
    assert_eq!(verified_first.root, unsigned_root);
    assert_eq!(verified_second.root, unsigned_root);
    assert_eq!(verified_first.generation, v(7));
    assert_eq!(verified_first.publisher, ok(PrincipalId::new([0x55; 32])));
    assert_eq!(
        ok(manifest_root(ok(decode_envelope(&second)).manifest)),
        unsigned_root
    );

    let mut wrong_signer = ok(decode_envelope(&first));
    wrong_signer.key = PublicKey32(signer(0x74).verifying_key().to_bytes());
    err(
        verify_publisher(&wrong_signer),
        VerificationFailure::Artifact(ArtifactError::SignatureInvalid),
    );
    let mut wrong_generation = ok(decode_envelope(&first));
    wrong_generation.generation = v(8);
    err(
        verify_publisher(&wrong_generation),
        VerificationFailure::Artifact(ArtifactError::SignatureInvalid),
    );
    let tampered = mutate(&first, first.len() - 1, first[first.len() - 1] ^ 1);
    err(
        verify_publisher(&ok(decode_envelope(&tampered))),
        VerificationFailure::Artifact(ArtifactError::SignatureInvalid),
    );
    for error in [
        ArtifactError::Malformed,
        ArtifactError::UnsupportedVersion,
        ArtifactError::UnsupportedKind,
        ArtifactError::InvalidContext,
        ArtifactError::MissingChunk,
        ArtifactError::LengthMismatch,
        ArtifactError::RootMismatch,
        ArtifactError::SignatureInvalid,
        ArtifactError::IntegrityConflict,
        ArtifactError::StorageFailure,
    ] {
        assert_eq!(error.name(), format!("{error:?}"));
    }
}

#[test]
fn a27_codec_corruption_refusal() {
    let object: Vec<u8> = (0..300_000u32).map(|i| (i % 251) as u8).collect();
    let bytes = manifest_for(&object);
    let root = ok(manifest_root(&bytes));
    let decoded = ok(decode_manifest(&bytes));
    let expected_content = decoded.content_root;
    let original = bytes.clone();

    let (chunk0, chunk1) = object.split_at(CHUNK_BYTES as usize);
    let mut corrupted = chunk1.to_vec();
    corrupted[17] ^= 0x01;

    let mut scratch = vec![[0u8; 32]; 2];
    let mut assembler = ok(ContentAssembler::new(&decoded, &mut scratch));
    assert!(ok(assembler.deliver(0, chunk0)));
    assert!(ok(assembler.deliver(1, &corrupted)));
    err(assembler.finish(), ArtifactError::RootMismatch);

    let leaf0 = [ok(chunk_leaf(0, chunk0))];
    let proof = |chunk| ChunkProof {
        manifest_root: root,
        index: 1,
        chunk,
        siblings: Items::Typed(&leaf0),
    };
    ok(verify_chunk_proof(&proof(chunk1), &bytes));
    err(
        verify_chunk_proof(&proof(&corrupted), &bytes),
        ArtifactError::RootMismatch,
    );

    assert_eq!(bytes, original);
    assert_eq!(ok(manifest_root(&bytes)), root);
    assert_eq!(ok(decode_manifest(&bytes)).content_root, expected_content);

    let mut scratch = vec![[0u8; 32]; 2];
    let mut assembler = ok(ContentAssembler::new(&decoded, &mut scratch));
    ok(assembler.deliver(0, chunk0));
    ok(assembler.deliver(1, chunk1));
    ok(assembler.finish());
}

#[test]
fn a28_codec_empty_and_mandatory_root_rules() {
    let empty_tree = sha(&[b"PAXAI/artifact-empty/v1\0"]);
    assert_eq!(empty_tree_root(), empty_tree);
    let empty_content = sha(&[
        b"PAXAI/artifact-content/v1\0",
        &0u64.to_be_bytes(),
        &0u32.to_be_bytes(),
        &empty_tree,
    ]);
    assert_eq!(content_of(&[]), empty_content);
    let bytes = encode(&manifest(0, 0, empty_content));
    let decoded = ok(decode_manifest(&bytes));
    assert_eq!(decoded.chunk_count, 0);
    assert_eq!(decoded.content_root, empty_content);
    let mut scratch: Vec<[u8; 32]> = Vec::new();
    ok(ok(ContentAssembler::new(&decoded, &mut scratch)).finish());

    let mut out = vec![0; MAX_MANIFEST_BYTES];
    let mut encrypted_empty = manifest(0, 0, empty_content);
    encrypted_empty.privacy = Privacy::Encrypted;
    encrypted_empty.access_policy_root = [0x99; 32];
    err(
        encode_manifest(&encrypted_empty, &mut out),
        ArtifactError::Malformed,
    );

    err(
        encode_manifest(&manifest(0, 0, [0; 32]), &mut out),
        ArtifactError::Malformed,
    );
    err(
        encode_manifest(&manifest(3, 1, [0; 32]), &mut out),
        ArtifactError::Malformed,
    );
    err(
        encode_manifest(&manifest(0, 0, [0x12; 32]), &mut out),
        ArtifactError::RootMismatch,
    );

    let mut encrypted = manifest(3, 1, [0x12; 32]);
    encrypted.privacy = Privacy::Encrypted;
    err(
        encode_manifest(&encrypted, &mut out),
        ArtifactError::Malformed,
    );
    encrypted.access_policy_root = [0x99; 32];
    ok(encode_manifest(&encrypted, &mut out));

    let zero_model = [ParentRef {
        purpose: ParentPurpose::Model,
        root: [0; 32],
    }];
    let mut with_parent = manifest(3, 1, [0x12; 32]);
    with_parent.parents = Items::Typed(&zero_model);
    err(
        encode_manifest(&with_parent, &mut out),
        ArtifactError::Malformed,
    );
    let mut zero_subject = manifest(3, 1, [0x12; 32]);
    zero_subject.subject = [0; 32];
    err(
        encode_manifest(&zero_subject, &mut out),
        ArtifactError::Malformed,
    );
    let mut optional = manifest(3, 1, [0x12; 32]);
    optional.declaration_root = [0; 32];
    optional.reproduction_root = [0; 32];
    ok(encode_manifest(&optional, &mut out));
}

#[test]
fn a29_codec_chunk_boundaries_and_completeness() {
    assert_eq!(ok(chunk_count(262_144)), 1);
    assert_eq!(ok(chunk_count(262_145)), 2);
    assert_eq!(ok(expected_chunk_length(262_145, 2, 1)), 1);
    let one: Vec<u8> = vec![0xa5; 262_144];
    let leaf = ok(chunk_leaf(0, &one));
    assert_eq!(content_of(&one), content_root(262_144, 1, &leaf));

    let two: Vec<u8> = (0..262_145u32).map(|i| (i % 253) as u8).collect();
    let bytes = manifest_for(&two);
    let decoded = ok(decode_manifest(&bytes));
    assert_eq!(decoded.chunk_count, 2);
    let (head, tail) = two.split_at(262_144);
    assert_eq!(tail.len(), 1);
    let mut scratch = vec![[0u8; 32]; 2];
    let mut assembler = ok(ContentAssembler::new(&decoded, &mut scratch));
    err(assembler.deliver(1, head), ArtifactError::LengthMismatch);
    err(assembler.deliver(0, tail), ArtifactError::LengthMismatch);
    err(assembler.deliver(2, tail), ArtifactError::Malformed);
    assert!(ok(assembler.deliver(1, tail)));
    assert!(!ok(assembler.deliver(1, tail)));
    err(
        assembler.deliver(1, &[tail[0] ^ 1]),
        ArtifactError::IntegrityConflict,
    );
    err(assembler.finish(), ArtifactError::MissingChunk);

    let mut scratch = vec![[0u8; 32]; 2];
    let mut assembler = ok(ContentAssembler::new(&decoded, &mut scratch));
    ok(assembler.deliver(1, tail));
    ok(assembler.deliver(0, head));
    assert!(!ok(assembler.deliver(0, head)));
    ok(assembler.finish());

    let full: Vec<u8> = (0..524_288u32).map(|i| (i % 249) as u8).collect();
    let full_bytes = manifest_for(&full);
    let full_manifest = ok(decode_manifest(&full_bytes));
    let (a, b) = full.split_at(262_144);
    let mut scratch = vec![[0u8; 32]; 2];
    let mut assembler = ok(ContentAssembler::new(&full_manifest, &mut scratch));
    ok(assembler.deliver(0, b));
    ok(assembler.deliver(1, a));
    err(assembler.finish(), ArtifactError::RootMismatch);
    let mut short: Vec<[u8; 32]> = vec![[0u8; 32]; 1];
    err(
        ContentAssembler::new(&full_manifest, &mut short).map(|_| ()),
        ArtifactError::CapacityUnavailable,
    );
}

#[test]
fn a30_codec_object_and_proof_limits() {
    let mut out = vec![0; MAX_MANIFEST_BYTES];
    let mut boundary = manifest(MAX_OBJECT_BYTES, MAX_CHUNKS, [0x12; 32]);
    boundary.privacy = Privacy::Encrypted;
    boundary.access_policy_root = [0x99; 32];
    ok(encode_manifest(&boundary, &mut out));
    let mut over = boundary;
    over.byte_length = MAX_OBJECT_BYTES + 1;
    over.chunk_count = MAX_CHUNKS + 1;
    err(encode_manifest(&over, &mut out), ArtifactError::Malformed);
    let mut over_count = boundary;
    over_count.chunk_count = MAX_CHUNKS + 1;
    err(
        encode_manifest(&over_count, &mut out),
        ArtifactError::Malformed,
    );
    err(chunk_count(MAX_OBJECT_BYTES + 1), ArtifactError::Malformed);
    err(chunk_count(u64::MAX), ArtifactError::Malformed);
    let mut boundary_bytes = encode(&boundary);
    boundary_bytes[216..224].copy_from_slice(&(MAX_OBJECT_BYTES + 1).to_be_bytes());
    boundary_bytes[228..232].copy_from_slice(&(MAX_CHUNKS + 1).to_be_bytes());
    err(decode_manifest(&boundary_bytes), ArtifactError::Malformed);

    let last = MAX_CHUNKS - 1;
    let chunk = vec![0x5a; 262_144];
    let siblings: Vec<[u8; 32]> = (0..17u8)
        .map(|k| sha(&[b"independent sibling", &[k]]))
        .collect();
    let mut node = sha(&[
        b"PAXAI/artifact-chunk/v1\0",
        &last.to_be_bytes(),
        &262_144u32.to_be_bytes(),
        &chunk,
    ]);
    for sibling in &siblings {
        node = sha(&[b"PAXAI/artifact-node/v1\0", sibling, &node]);
    }
    let content = sha(&[
        b"PAXAI/artifact-content/v1\0",
        &MAX_OBJECT_BYTES.to_be_bytes(),
        &MAX_CHUNKS.to_be_bytes(),
        &node,
    ]);
    let mut big = boundary;
    big.content_root = content;
    let big_bytes = encode(&big);
    let root = ok(manifest_root(&big_bytes));
    assert_eq!(proof_sibling_count(MAX_CHUNKS), 17);
    let proof = ChunkProof {
        manifest_root: root,
        index: last,
        chunk: &chunk,
        siblings: Items::Typed(&siblings),
    };
    let mut proof_bytes = vec![0; PROOF_FIXED_BYTES + chunk.len() + 18 * 32];
    let n = ok(encode_chunk_proof(&proof, &mut proof_bytes));
    proof_bytes.truncate(n);
    let decoded = ok(decode_chunk_proof(&proof_bytes));
    ok(verify_chunk_proof(&decoded, &big_bytes));

    let mut eighteen = proof_bytes.clone();
    let count_at = PROOF_FIXED_BYTES - 1 + chunk.len();
    eighteen[count_at] = 18;
    eighteen.extend_from_slice(&[0x01; 32]);
    err(decode_chunk_proof(&eighteen), ArtifactError::Malformed);
    let mut too_many = siblings.clone();
    too_many.push([0x01; 32]);
    let mut scratch = vec![0; PROOF_FIXED_BYTES + chunk.len() + 18 * 32];
    err(
        encode_chunk_proof(
            &ChunkProof {
                siblings: Items::Typed(&too_many),
                ..proof
            },
            &mut scratch,
        ),
        ArtifactError::Malformed,
    );
    err(
        verify_chunk_proof(
            &ChunkProof {
                siblings: Items::Typed(&siblings[..16]),
                ..proof
            },
            &big_bytes,
        ),
        ArtifactError::Malformed,
    );
    err(
        verify_chunk_proof(
            &ChunkProof {
                index: MAX_CHUNKS,
                ..proof
            },
            &big_bytes,
        ),
        ArtifactError::Malformed,
    );
    err(
        verify_chunk_proof(
            &ChunkProof {
                chunk: &chunk[..262_143],
                ..proof
            },
            &big_bytes,
        ),
        ArtifactError::LengthMismatch,
    );
    let mut wrong_sibling = siblings.clone();
    wrong_sibling[3][0] ^= 1;
    err(
        verify_chunk_proof(
            &ChunkProof {
                siblings: Items::Typed(&wrong_sibling),
                ..proof
            },
            &big_bytes,
        ),
        ArtifactError::RootMismatch,
    );

    let three: Vec<u8> = (0..(2 * 262_144 + 5) as u32)
        .map(|i| (i % 241) as u8)
        .collect();
    let three_bytes = manifest_for(&three);
    let three_root = ok(manifest_root(&three_bytes));
    let leaves: Vec<[u8; 32]> = three
        .chunks(262_144)
        .enumerate()
        .map(|(i, c)| ok(chunk_leaf(i as u32, c)))
        .collect();
    let left = sha(&[b"PAXAI/artifact-node/v1\0", &leaves[0], &leaves[1]]);
    let canonical = [leaves[2], left];
    let odd = ChunkProof {
        manifest_root: three_root,
        index: 2,
        chunk: &three[2 * 262_144..],
        siblings: Items::Typed(&canonical),
    };
    ok(verify_chunk_proof(&odd, &three_bytes));
    let noncanonical = [leaves[1], left];
    err(
        verify_chunk_proof(
            &ChunkProof {
                siblings: Items::Typed(&noncanonical),
                ..odd
            },
            &three_bytes,
        ),
        ArtifactError::Malformed,
    );
    err(
        verify_chunk_proof(&odd, &big_bytes),
        ArtifactError::RootMismatch,
    );
}

#[test]
fn a31_codec_exact_context_binding() {
    let bytes = manifest_for(&[1, 2, 3]);
    let root = ok(manifest_root(&bytes));
    let digest = publisher_digest(&ctx(), root, v(7));
    let envelope = envelope_bytes(&bytes, 7, &signer(0x73), &digest);
    ok(verify_publisher(&ok(decode_envelope(&envelope))));
    let signed = ok(decode_manifest(ok(decode_envelope(&envelope)).manifest));
    let bound = SubjectContext::Task {
        epoch: 9,
        task: task(0x31),
    };
    ok(check_manifest_context(&signed, &ctx(), bound));
    for other in [
        ctx_with(0x11, 0x22, 0x34, 0x44),
        ctx_with(0x12, 0x22, 0x33, 0x44),
        ctx_with(0x11, 0x23, 0x33, 0x44),
        ctx_with(0x11, 0x22, 0x33, 0x45),
    ] {
        err(
            check_manifest_context(&signed, &other, bound),
            ArtifactError::InvalidContext,
        );
    }
    for subject in [
        SubjectContext::Task {
            epoch: 9,
            task: task(0x32),
        },
        SubjectContext::Task {
            epoch: 10,
            task: task(0x31),
        },
        SubjectContext::Policy,
    ] {
        err(
            check_manifest_context(&signed, &ctx(), subject),
            ArtifactError::InvalidContext,
        );
    }

    let mut model = manifest(3, 1, content_of(&[1, 2, 3]));
    model.kind = ArtifactKind::Model;
    model.epoch = 0;
    model.subject = [0x44; 32];
    let model_bytes = encode(&model);
    let model = ok(decode_manifest(&model_bytes));
    ok(check_manifest_context(
        &model,
        &ctx(),
        SubjectContext::Policy,
    ));
    err(
        check_manifest_context(&model, &ctx(), bound),
        ArtifactError::InvalidContext,
    );
    let mut out = vec![0; MAX_MANIFEST_BYTES];
    for kind in [
        ArtifactKind::Model,
        ArtifactKind::Dataset,
        ArtifactKind::Benchmark,
    ] {
        let mut wrong_epoch = model;
        wrong_epoch.kind = kind;
        wrong_epoch.epoch = 1;
        err(
            encode_manifest(&wrong_epoch, &mut out),
            ArtifactError::InvalidContext,
        );
        let mut wrong_subject = model;
        wrong_subject.kind = kind;
        wrong_subject.subject = [0x31; 32];
        err(
            encode_manifest(&wrong_subject, &mut out),
            ArtifactError::InvalidContext,
        );
    }
    for kind in [ArtifactKind::Input, ArtifactKind::ExecutionEvidence] {
        let mut task_bound = signed;
        task_bound.kind = kind;
        ok(check_manifest_context(&task_bound, &ctx(), bound));
        err(
            check_manifest_context(&task_bound, &ctx(), SubjectContext::Policy),
            ArtifactError::InvalidContext,
        );
    }
}

fn binding(config: u64) -> EvaluatorBinding {
    EvaluatorBinding {
        frozen: FrozenBinding {
            chain: ok(ChainDomain::new([0x11; 32])),
            program: ok(ProgramId::new([0x22; 32])),
            market: ok(MarketId::new([0x33; 32])),
            epoch: 9,
            config: v(config),
            roster: ok(RosterDigest::new([0x66; 32])),
        },
        evaluator: ok(EvaluatorId::new([0x77; 32])),
        grant: v(3),
        key_version: v(4),
    }
}
fn policy() -> EvidencePolicy {
    EvidencePolicy {
        mode: AssessmentMode::Objective,
        rubric: ok(RubricDigest::new([0x88; 32])),
        dataset_absence_admitted: false,
        benchmark_absence_admitted: false,
        missing_result_admitted: false,
    }
}
fn entry(id: u8, status: TerminalStatus) -> EvidenceTask {
    EvidenceTask {
        task: task(id),
        request_root: [0xa1; 32],
        result_root: [0xa2; 32],
        execution_root: [0xa3; 32],
        status,
        reproduction_root: [0; 32],
    }
}
fn group(worker: u8, tasks: &[EvidenceTask]) -> WorkerGroup<'_> {
    WorkerGroup {
        worker: ok(WorkerId::new([worker; 32])),
        generation: v(5),
        model_root: [0xb1; 32],
        deployment_root: [0xb2; 32],
        score: ok(Score::new(700_000)),
        reason_code: 0,
        reason_artifact_root: [0; 32],
        tasks: Items::Typed(tasks),
    }
}
fn evidence<'a>(groups: &'a [WorkerGroup<'a>]) -> EvidenceManifest<'a> {
    EvidenceManifest {
        binding: binding(2),
        task_policy: ok(PolicyDigest::new([0x44; 32])),
        task_set: ok(Digest32::new([0x45; 32])),
        rubric: ok(RubricDigest::new([0x88; 32])),
        dataset_root: [0xc1; 32],
        benchmark_root: [0xc2; 32],
        mode: AssessmentMode::Objective,
        groups: Items::Typed(groups),
    }
}
fn encode_evidence(
    value: &EvidenceManifest<'_>,
    policy: &EvidencePolicy,
) -> Result<Vec<u8>, ArtifactError> {
    let mut out = vec![0; MAX_EVIDENCE_BYTES];
    let n = encode_evidence_manifest(value, policy, &mut out)?;
    out.truncate(n);
    Ok(out)
}
fn root_of(value: &EvidenceManifest<'_>, policy: &EvidencePolicy) -> [u8; 32] {
    ok(evidence_root(&ok(encode_evidence(value, policy)), policy)).bytes()
}

#[test]
fn a32_codec_typed_evidence_commitment() {
    let tasks = [entry(0x31, TerminalStatus::Success)];
    let groups = [group(0x01, &tasks)];
    let base = evidence(&groups);
    let bytes = ok(encode_evidence(&base, &policy()));

    let mut expected = Vec::new();
    expected.extend_from_slice(&1u16.to_be_bytes());
    for b in [0x11u8, 0x22, 0x33] {
        expected.extend_from_slice(&[b; 32]);
    }
    expected.extend_from_slice(&9u64.to_be_bytes());
    expected.extend_from_slice(&2u64.to_be_bytes());
    expected.extend_from_slice(&[0x66; 32]);
    expected.extend_from_slice(&[0x77; 32]);
    expected.extend_from_slice(&3u64.to_be_bytes());
    expected.extend_from_slice(&4u64.to_be_bytes());
    for b in [0x44u8, 0x45, 0x88, 0xc1, 0xc2] {
        expected.extend_from_slice(&[b; 32]);
    }
    expected.push(1);
    expected.extend_from_slice(&1u16.to_be_bytes());
    expected.extend_from_slice(&[0x01; 32]);
    expected.extend_from_slice(&5u64.to_be_bytes());
    expected.extend_from_slice(&[0xb1; 32]);
    expected.extend_from_slice(&[0xb2; 32]);
    expected.extend_from_slice(&700_000u32.to_be_bytes());
    expected.extend_from_slice(&0u16.to_be_bytes());
    expected.extend_from_slice(&[0; 32]);
    expected.extend_from_slice(&1u16.to_be_bytes());
    for b in [0x31u8, 0xa1, 0xa2, 0xa3] {
        expected.extend_from_slice(&[b; 32]);
    }
    expected.push(1);
    expected.extend_from_slice(&[0; 32]);
    expected.extend_from_slice(&[0; 16]);
    assert_eq!(bytes, expected);
    assert_eq!(
        bytes.len(),
        EVIDENCE_FIXED_BYTES + WORKER_GROUP_FIXED_BYTES + TASK_ENTRY_BYTES
    );
    let decoded = ok(decode_evidence_manifest(&bytes, &policy()));
    assert_eq!(ok(encode_evidence(&decoded, &policy())), bytes);

    let root = ok(evidence_root(&bytes, &policy()));
    assert_eq!(root.bytes(), sha(&[b"PAXAI/evidence/v1\0", &bytes]));
    assert_ne!(
        root.bytes(),
        sha(&[
            b"PAXAI/artifact-manifest/v1\0",
            &ctx_bytes(),
            &(bytes.len() as u32).to_be_bytes(),
            &bytes
        ])
    );
    err(manifest_root(&bytes), ArtifactError::Malformed);

    let base_root = root.bytes();
    let mut roots = vec![base_root];
    let changed_task = [entry(0x32, TerminalStatus::Success)];
    let g = [group(0x01, &changed_task)];
    roots.push(root_of(&evidence(&g), &policy()));
    let mut model = group(0x01, &tasks);
    model.model_root = [0xb9; 32];
    roots.push(root_of(&evidence(&[model]), &policy()));
    let mut task_policy = evidence(&groups);
    task_policy.task_policy = ok(PolicyDigest::new([0x49; 32]));
    roots.push(root_of(&task_policy, &policy()));
    let mut generation = group(0x01, &tasks);
    generation.generation = v(6);
    roots.push(root_of(&evidence(&[generation]), &policy()));
    let mut score = group(0x01, &tasks);
    score.score = ok(Score::new(700_001));
    roots.push(root_of(&evidence(&[score]), &policy()));
    let refused = [entry(0x31, TerminalStatus::Refused)];
    roots.push(root_of(&evidence(&[group(0x01, &refused)]), &policy()));
    let mut rubric = evidence(&groups);
    rubric.rubric = ok(RubricDigest::new([0x89; 32]));
    let rubric_policy = EvidencePolicy {
        rubric: rubric.rubric,
        ..policy()
    };
    roots.push(root_of(&rubric, &rubric_policy));
    let mut config = evidence(&groups);
    config.binding = binding(3);
    roots.push(root_of(&config, &policy()));
    for (i, a) in roots.iter().enumerate() {
        for b in &roots[i + 1..] {
            assert_ne!(a, b);
        }
    }

    err(
        encode_evidence(&evidence(&[]), &policy()),
        ArtifactError::Malformed,
    );
    let many_tasks: Vec<EvidenceTask> = (1..=33u8)
        .map(|i| entry(i, TerminalStatus::Success))
        .collect();
    let thirty_three: Vec<WorkerGroup<'_>> = (1..=33u8)
        .map(|i| group(i, &many_tasks[usize::from(i) - 1..usize::from(i)]))
        .collect();
    err(
        encode_evidence(&evidence(&thirty_three), &policy()),
        ArtifactError::Malformed,
    );
    ok(encode_evidence(&evidence(&thirty_three[..32]), &policy()));
    let other = [entry(0x30, TerminalStatus::Success)];
    err(
        encode_evidence(
            &evidence(&[group(0x02, &other), group(0x01, &tasks)]),
            &policy(),
        ),
        ArtifactError::Malformed,
    );
    err(
        encode_evidence(
            &evidence(&[group(0x01, &other), group(0x01, &tasks)]),
            &policy(),
        ),
        ArtifactError::Malformed,
    );
    err(
        encode_evidence(&evidence(&[group(0x01, &[])]), &policy()),
        ArtifactError::Malformed,
    );
    err(
        encode_evidence(
            &evidence(&[group(0x01, &tasks), group(0x02, &tasks)]),
            &policy(),
        ),
        ArtifactError::Malformed,
    );
    let unsorted = [
        entry(0x32, TerminalStatus::Success),
        entry(0x31, TerminalStatus::Success),
    ];
    err(
        encode_evidence(&evidence(&[group(0x01, &unsorted)]), &policy()),
        ArtifactError::Malformed,
    );
    let sixty_five: Vec<EvidenceTask> = (1..=65u8)
        .map(|i| entry(i, TerminalStatus::Success))
        .collect();
    err(
        encode_evidence(
            &evidence(&[
                group(0x01, &sixty_five[..33]),
                group(0x02, &sixty_five[33..]),
            ]),
            &policy(),
        ),
        ArtifactError::Malformed,
    );
    ok(encode_evidence(
        &evidence(&[
            group(0x01, &sixty_five[..32]),
            group(0x02, &sixty_five[32..64]),
        ]),
        &policy(),
    ));

    let mut subjective = evidence(&groups);
    subjective.mode = AssessmentMode::Subjective;
    err(
        encode_evidence(&subjective, &policy()),
        ArtifactError::InvalidContext,
    );
    err(
        encode_evidence(&base, &rubric_policy),
        ArtifactError::InvalidContext,
    );
    let mut no_dataset = evidence(&groups);
    no_dataset.dataset_root = [0; 32];
    err(
        encode_evidence(&no_dataset, &policy()),
        ArtifactError::Malformed,
    );
    ok(encode_evidence(
        &no_dataset,
        &EvidencePolicy {
            dataset_absence_admitted: true,
            ..policy()
        },
    ));
    let mut no_benchmark = evidence(&groups);
    no_benchmark.benchmark_root = [0; 32];
    err(
        encode_evidence(&no_benchmark, &policy()),
        ArtifactError::Malformed,
    );

    let mut missing = entry(0x31, TerminalStatus::Success);
    missing.result_root = [0; 32];
    let permissive = EvidencePolicy {
        missing_result_admitted: true,
        ..policy()
    };
    err(
        encode_evidence(&evidence(&[group(0x01, &[missing])]), &permissive),
        ArtifactError::Malformed,
    );
    let mut timeout = entry(0x31, TerminalStatus::Timeout);
    timeout.result_root = [0; 32];
    timeout.execution_root = [0; 32];
    err(
        encode_evidence(&evidence(&[group(0x01, &[timeout])]), &policy()),
        ArtifactError::Malformed,
    );
    ok(encode_evidence(
        &evidence(&[group(0x01, &[timeout])]),
        &permissive,
    ));
    let mut no_request = entry(0x31, TerminalStatus::Unknown);
    no_request.request_root = [0; 32];
    err(
        encode_evidence(&evidence(&[group(0x01, &[no_request])]), &permissive),
        ArtifactError::Malformed,
    );

    err(
        decode_evidence_manifest(&mutate(&bytes, 1, 2), &policy()),
        ArtifactError::UnsupportedVersion,
    );
    err(
        decode_evidence_manifest(&mutate(&bytes, 354, 3), &policy()),
        ArtifactError::Malformed,
    );
    err(
        decode_evidence_manifest(&mutate(&bytes, 629, 6), &policy()),
        ArtifactError::Malformed,
    );
    err(
        decode_evidence_manifest(&mutate(&bytes, 629, 0), &policy()),
        ArtifactError::Malformed,
    );
    let mut big_score = bytes.clone();
    big_score[461..465].copy_from_slice(&1_000_001u32.to_be_bytes());
    err(
        decode_evidence_manifest(&big_score, &policy()),
        ArtifactError::Malformed,
    );
    let mut zero_generation = bytes.clone();
    zero_generation[389..397].copy_from_slice(&0u64.to_be_bytes());
    err(
        decode_evidence_manifest(&zero_generation, &policy()),
        ArtifactError::Malformed,
    );
    let mut too_many_workers = bytes.clone();
    too_many_workers[355..357].copy_from_slice(&33u16.to_be_bytes());
    err(
        decode_evidence_manifest(&too_many_workers, &policy()),
        ArtifactError::Malformed,
    );
    let mut too_many_tasks = bytes.clone();
    too_many_tasks[499..501].copy_from_slice(&65u16.to_be_bytes());
    err(
        decode_evidence_manifest(&too_many_tasks, &policy()),
        ArtifactError::Malformed,
    );
    err(
        decode_evidence_manifest(&appended(&bytes), &policy()),
        ArtifactError::Malformed,
    );
    err(
        decode_evidence_manifest(&mutate(&bytes, bytes.len() - 1, 1), &policy()),
        ArtifactError::Malformed,
    );
    err(
        decode_evidence_manifest(&vec![0; MAX_EVIDENCE_BYTES + 1], &policy()),
        ArtifactError::Malformed,
    );
}

fn document(role: DocumentRole, root: u8, label: &str) -> RightsDocument<'_> {
    RightsDocument {
        role,
        root: [root; 32],
        label,
    }
}
fn declaration<'a>(documents: &'a [RightsDocument<'a>]) -> Declaration<'a> {
    Declaration {
        publisher: ok(PrincipalId::new([0x55; 32])),
        rights: RightsStatus::Documented,
        purpose_mask: 0b0011,
        restriction_mask: 0b0_0001,
        valid_until_height: 1_000,
        documents: Items::Typed(documents),
        review_reference_root: [0x5e; 32],
    }
}
fn encode_decl(value: &Declaration<'_>) -> Result<Vec<u8>, ArtifactError> {
    let mut out = vec![0; MAX_RECORD_BYTES];
    let n = encode_declaration(value, &mut out)?;
    out.truncate(n);
    Ok(out)
}

#[test]
fn a33_codec_strict_versions_declarations_and_wrappers() {
    let object = [1u8, 2, 3];
    let parents = [
        ParentRef {
            purpose: ParentPurpose::Input,
            root: [0x02; 32],
        },
        ParentRef {
            purpose: ParentPurpose::Model,
            root: [0x01; 32],
        },
        ParentRef {
            purpose: ParentPurpose::ScoreEvidence,
            root: [0x09; 32],
        },
    ];
    let mut with_parents = manifest(3, 1, content_of(&object));
    with_parents.parents = Items::Typed(&parents);
    let bytes = encode(&with_parents);
    assert_eq!(bytes.len(), MANIFEST_FIXED_BYTES + 3 * PARENT_BYTES);
    let decoded = ok(decode_manifest(&bytes));
    let got: Vec<ParentRef> = decoded.parents.iter().map(ok).collect();
    assert_eq!(got, parents);
    assert_eq!(encode(&decoded), bytes);

    err(
        decode_manifest(&mutate(&bytes, 0, b'X')),
        ArtifactError::Malformed,
    );
    err(
        decode_manifest(&mutate(&bytes, 9, 2)),
        ArtifactError::UnsupportedVersion,
    );
    err(
        decode_manifest(&mutate(&bytes, 10, 10)),
        ArtifactError::UnsupportedKind,
    );
    err(
        decode_manifest(&mutate(&bytes, 10, 0)),
        ArtifactError::UnsupportedKind,
    );
    err(
        decode_manifest(&mutate(&bytes, 11, 2)),
        ArtifactError::Malformed,
    );
    err(
        decode_manifest(&mutate(&bytes, 12, 1)),
        ArtifactError::Malformed,
    );
    err(
        decode_manifest(&mutate(&bytes, 226, 0x05)),
        ArtifactError::Malformed,
    );
    err(
        decode_manifest(&mutate(&bytes, 266, 11)),
        ArtifactError::Malformed,
    );
    err(
        decode_manifest(&mutate(&bytes, 266, 0)),
        ArtifactError::Malformed,
    );
    err(
        decode_manifest(&mutate(&bytes, 265, 17)),
        ArtifactError::Malformed,
    );
    err(decode_manifest(&appended(&bytes)), ArtifactError::Malformed);
    err(
        decode_manifest(&mutate(&bytes, bytes.len() - 1, 1)),
        ArtifactError::Malformed,
    );
    err(
        decode_manifest(&bytes[..bytes.len() - 1]),
        ArtifactError::Malformed,
    );
    err(
        decode_manifest(&vec![0; MAX_MANIFEST_BYTES + 1]),
        ArtifactError::Malformed,
    );
    err(
        decode_manifest(&mutate(&bytes, 231, 2)),
        ArtifactError::LengthMismatch,
    );

    let mut out = vec![0; MAX_MANIFEST_BYTES];
    let unsorted = [parents[1], parents[0]];
    with_parents.parents = Items::Typed(&unsorted);
    err(
        encode_manifest(&with_parents, &mut out),
        ArtifactError::Malformed,
    );
    let duplicate = [parents[0], parents[0]];
    with_parents.parents = Items::Typed(&duplicate);
    err(
        encode_manifest(&with_parents, &mut out),
        ArtifactError::Malformed,
    );
    let seventeen: Vec<ParentRef> = (1..=17u8)
        .map(|i| ParentRef {
            purpose: ParentPurpose::ModelShard,
            root: [i; 32],
        })
        .collect();
    with_parents.parents = Items::Typed(&seventeen);
    err(
        encode_manifest(&with_parents, &mut out),
        ArtifactError::Malformed,
    );
    with_parents.parents = Items::Typed(&seventeen[..16]);
    ok(encode_manifest(&with_parents, &mut out));

    let plain = manifest_for(&object);
    let root = ok(manifest_root(&plain));
    let digest = publisher_digest(&ctx(), root, v(7));
    let envelope = envelope_bytes(&plain, 7, &signer(0x73), &digest);
    assert_eq!(envelope.len(), ENVELOPE_FIXED_BYTES + plain.len());
    assert_eq!(envelope[..4], (plain.len() as u32).to_be_bytes());
    let scheme_at = 4 + plain.len();
    err(
        decode_envelope(&mutate(&envelope, scheme_at + 1, 2)),
        ArtifactError::Malformed,
    );
    let mut zero_generation = envelope.clone();
    zero_generation[scheme_at + 2..scheme_at + 10].copy_from_slice(&0u64.to_be_bytes());
    err(decode_envelope(&zero_generation), ArtifactError::Malformed);
    let mut zero_key = envelope.clone();
    zero_key[scheme_at + 10..scheme_at + 42].copy_from_slice(&[0; 32]);
    err(decode_envelope(&zero_key), ArtifactError::Malformed);
    err(
        decode_envelope(&appended(&envelope)),
        ArtifactError::Malformed,
    );
    err(
        decode_envelope(&envelope[..envelope.len() - 1]),
        ArtifactError::Malformed,
    );
    let mut long = envelope.clone();
    long[..4].copy_from_slice(&4_097u32.to_be_bytes());
    err(decode_envelope(&long), ArtifactError::Malformed);
    let mut shifted = envelope.clone();
    shifted[..4].copy_from_slice(&(plain.len() as u32 + 1).to_be_bytes());
    err(decode_envelope(&shifted), ArtifactError::Malformed);
    err(
        decode_envelope(&vec![0; MAX_ENVELOPE_BYTES + 1]),
        ArtifactError::Malformed,
    );

    let nfd = "cafe\u{301}";
    let nfc = "caf\u{e9}";
    let documents = [
        document(DocumentRole::License, 0x10, nfd),
        document(DocumentRole::License, 0x10, nfc),
        document(DocumentRole::Consent, 0x01, "consent"),
    ];
    let decl = ok(encode_decl(&declaration(&documents)));
    let parsed = ok(decode_declaration(&decl));
    let labels: Vec<&str> = parsed.documents.iter().map(|d| ok(d).label).collect();
    assert_eq!(labels, [nfd, nfc, "consent"]);
    assert_ne!(labels[0].as_bytes(), labels[1].as_bytes());
    assert_eq!(ok(encode_decl(&parsed)), decl);
    let decl_root = ok(declaration_root(&ctx(), &decl));
    assert_eq!(
        decl_root.bytes(),
        sha(&[
            b"PAXAI/artifact-declaration/v1\0",
            &ctx_bytes(),
            &(decl.len() as u32).to_be_bytes(),
            &decl
        ])
    );
    let nfc_only = [document(DocumentRole::License, 0x10, nfc)];
    let nfd_only = [document(DocumentRole::License, 0x10, nfd)];
    assert_ne!(
        ok(declaration_root(
            &ctx(),
            &ok(encode_decl(&declaration(&nfc_only)))
        )),
        ok(declaration_root(
            &ctx(),
            &ok(encode_decl(&declaration(&nfd_only)))
        ))
    );
    assert_eq!(rights_status(None), RightsStatus::Undeclared);
    assert_eq!(rights_status(Some(&parsed)), RightsStatus::Documented);

    let mut bad = declaration(&documents);
    bad.purpose_mask = 0b1_0000;
    err(encode_decl(&bad), ArtifactError::Malformed);
    let mut bad = declaration(&documents);
    bad.restriction_mask = 0b10_0000;
    err(encode_decl(&bad), ArtifactError::Malformed);
    let mut bad = declaration(&[]);
    err(encode_decl(&bad), ArtifactError::Malformed);
    bad.rights = RightsStatus::Undeclared;
    ok(encode_decl(&bad));
    let swapped = [documents[1], documents[0]];
    err(
        encode_decl(&declaration(&swapped)),
        ArtifactError::Malformed,
    );
    let duplicated = [documents[0], documents[0]];
    err(
        encode_decl(&declaration(&duplicated)),
        ArtifactError::Malformed,
    );
    let zero_root = [document(DocumentRole::License, 0, "x")];
    err(
        encode_decl(&declaration(&zero_root)),
        ArtifactError::Malformed,
    );
    let long_label = "a".repeat(257);
    let too_long = [document(DocumentRole::License, 0x10, &long_label)];
    err(
        encode_decl(&declaration(&too_long)),
        ArtifactError::Malformed,
    );
    let max_label = "a".repeat(256);
    let at_max = [document(DocumentRole::License, 0x10, &max_label)];
    ok(encode_decl(&declaration(&at_max)));
    let many: Vec<RightsDocument<'_>> = (1..=17u8)
        .map(|i| document(DocumentRole::License, i, "l"))
        .collect();
    err(encode_decl(&declaration(&many)), ArtifactError::Malformed);
    ok(encode_decl(&declaration(&many[..16])));

    err(
        decode_declaration(&mutate(&decl, 1, 2)),
        ArtifactError::UnsupportedVersion,
    );
    err(
        decode_declaration(&mutate(&decl, 34, 3)),
        ArtifactError::Malformed,
    );
    err(
        decode_declaration(&mutate(&decl, 49, 5)),
        ArtifactError::Malformed,
    );
    let first_label = 49 + DOCUMENT_FIXED_BYTES;
    err(
        decode_declaration(&mutate(&decl, first_label, 0xff)),
        ArtifactError::Malformed,
    );
    let mut long_len = decl.clone();
    long_len[first_label - 4..first_label].copy_from_slice(&257u32.to_be_bytes());
    err(decode_declaration(&long_len), ArtifactError::Malformed);
    let mut count = decl.clone();
    count[47..49].copy_from_slice(&17u16.to_be_bytes());
    err(decode_declaration(&count), ArtifactError::Malformed);
    err(
        decode_declaration(&appended(&decl)),
        ArtifactError::Malformed,
    );
    err(
        decode_declaration(&mutate(&decl, decl.len() - 1, 1)),
        ArtifactError::Malformed,
    );

    let reproduction = Reproduction {
        task: task(0x31),
        request_root: [0xd1; 32],
        model_root: [0xd2; 32],
        input_root: [0xd3; 32],
        result_root: [0xd4; 32],
        evaluator_program_record_root: [0xd5; 32],
        environment_root: [0xd6; 32],
        method_root: [0xd7; 32],
        seed_root: [0xd8; 32],
        metric_schema_root: [0xd9; 32],
        observed_units: 42,
        status: ReproductionStatus::Diverged,
        repeated_result_root: [0xda; 32],
    };
    let repro = ok(encode_reproduction(&reproduction));
    assert_eq!(ok(decode_reproduction(&repro)), reproduction);
    assert_eq!(
        ok(reproduction_root(&ctx(), &repro)).bytes(),
        sha(&[
            b"PAXAI/artifact-reproduction/v1\0",
            &ctx_bytes(),
            &(REPRODUCTION_BYTES as u32).to_be_bytes(),
            &repro
        ])
    );
    err(
        decode_reproduction(&mutate(&repro, 1, 2)),
        ArtifactError::UnsupportedVersion,
    );
    err(
        decode_reproduction(&mutate(&repro, 330, 4)),
        ArtifactError::Malformed,
    );
    err(
        decode_reproduction(&mutate(&repro, 378, 1)),
        ArtifactError::Malformed,
    );
    err(
        decode_reproduction(&appended(&repro)),
        ArtifactError::Malformed,
    );
    let mut zero_model = repro;
    zero_model[66..98].copy_from_slice(&[0; 32]);
    err(decode_reproduction(&zero_model), ArtifactError::Malformed);
    err(
        encode_reproduction(&Reproduction {
            result_root: [0; 32],
            ..reproduction
        }),
        ArtifactError::Malformed,
    );

    let leaf = [ok(chunk_leaf(0, &object))];
    let proof = ChunkProof {
        manifest_root: root,
        index: 0,
        chunk: &object,
        siblings: Items::Typed(&[]),
    };
    let mut proof_bytes = vec![0; 64];
    let n = ok(encode_chunk_proof(&proof, &mut proof_bytes));
    proof_bytes.truncate(n);
    ok(verify_chunk_proof(
        &ok(decode_chunk_proof(&proof_bytes)),
        &plain,
    ));
    err(
        decode_chunk_proof(&mutate(&proof_bytes, 1, 2)),
        ArtifactError::UnsupportedVersion,
    );
    err(
        decode_chunk_proof(&appended(&proof_bytes)),
        ArtifactError::Malformed,
    );
    let mut oversize = proof_bytes.clone();
    oversize[38..42].copy_from_slice(&262_145u32.to_be_bytes());
    err(decode_chunk_proof(&oversize), ArtifactError::Malformed);
    err(
        verify_chunk_proof(
            &ChunkProof {
                siblings: Items::Typed(&leaf),
                ..proof
            },
            &plain,
        ),
        ArtifactError::Malformed,
    );
    err(expected_chunk_length(3, 1, 1), ArtifactError::Malformed);
    err(chunk_leaf(0, &vec![0; 262_145]), ArtifactError::Malformed);
}
