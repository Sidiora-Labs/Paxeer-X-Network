pub mod support;

use layerx_programs::{
    programs_root_commitment, InterfaceStateWitness, ProgramBundleError, ProgramHeadKind,
    ProgramLifecycleProof, ProgramStateBundle, ProtocolDeploymentVerifier, ProtocolEvidenceError,
    ReceiptEvidenceDigest, StateLeafWitness, StateProof, VerifiedChainHead,
};
use layerx_programs_runtime::{ProgramId, UpgradePolicy};
use layerx_proof::merkle::{build_proof, encode_proof};
use layerx_wire::receipt::decode_batch_header;
use sha2::{Digest, Sha256};
use support::{
    deploy_fixture, program, verifier_for_fixture, verifier_from_history, ProtocolFixture,
    TrustAnchorFixture, AUTHORITY, NOW, WASM_V1, WASM_V2,
};

const DOMAIN: &[u8; 31] = b"LayerX/programs/state-proof/v1\0";
const STALENESS_MS: u64 = 1_000;

const MAINTENANCE_RECEIPT: &[u8] =
    include_bytes!("../../../../tests/fixtures/custody/daemon-module-head/receipt");
const MAINTENANCE: &[u8] =
    include_bytes!("../../../../tests/fixtures/custody/daemon-module-head/maintenance.receipt");
const MAINTENANCE_HEADER: &[u8] =
    include_bytes!("../../../../tests/fixtures/custody/daemon-module-head/header");
const MAINTENANCE_SIGNATURE: &[u8; 64] =
    include_bytes!("../../../../tests/fixtures/custody/daemon-module-head/header.signature");
const MAINTENANCE_PUBLIC: &[u8; 32] =
    include_bytes!("../../../../tests/fixtures/custody/daemon-module-head/sequencer.public");

fn deployed(batch_number: u64, wasm: &[u8]) -> ProtocolFixture {
    deploy_fixture(
        wasm,
        UpgradePolicy::Authority(AUTHORITY),
        batch_number,
        1_700_000_000 + batch_number,
    )
}

fn verifier(fixture: &ProtocolFixture) -> ProtocolDeploymentVerifier {
    verifier_for_fixture(fixture, 70, 100, None, STALENESS_MS)
}

fn ordinary_bundle(fixture: &ProtocolFixture) -> ProgramStateBundle {
    ProgramStateBundle {
        head_kind: ProgramHeadKind::Ordinary,
        state: fixture.proof.state.clone(),
        interface: fixture.interface_witness.clone(),
    }
}

fn ordinary_chain(verifier: &ProtocolDeploymentVerifier, fixture: &ProtocolFixture) -> VerifiedChainHead {
    let state = &fixture.proof.state;
    verifier
        .verify_current_chain_head(
            ProgramHeadKind::Ordinary,
            &state.receipt,
            &state.receipt_proof,
            &state.header,
            &state.header_signature,
            NOW,
        )
        .unwrap_or_else(|error| panic!("independent ordinary chain head: {error}"))
}

fn other_program() -> ProgramId {
    ProgramId::new([0x32; 32]).unwrap_or_else(|error| panic!("program: {error}"))
}

fn frame(bytes: &mut Vec<u8>, value: &[u8]) {
    let length = u32::try_from(value.len()).unwrap_or_else(|_| panic!("frame length"));
    bytes.extend_from_slice(&length.to_be_bytes());
    bytes.extend_from_slice(value);
}

fn state_proof(bytes: &mut Vec<u8>, proof: &StateProof) {
    bytes.extend_from_slice(&proof.leaf_index.to_be_bytes());
    bytes.extend_from_slice(&proof.leaf_count.to_be_bytes());
    bytes.push(u8::try_from(proof.siblings.len()).unwrap_or_else(|_| panic!("depth")));
    for sibling in &proof.siblings {
        bytes.extend_from_slice(sibling);
    }
}

fn witness(bytes: &mut Vec<u8>, key: &[u8], value: &[u8], proof: &StateProof) {
    frame(bytes, key);
    frame(bytes, value);
    state_proof(bytes, proof);
}

/// Builds the kind-5 response payload field by field from the wire contract,
/// independently of the registry codec.
fn contract_payload(head_kind: u8, fixture: &ProtocolFixture) -> Vec<u8> {
    let state = &fixture.proof.state;
    let mut bytes = Vec::new();
    bytes.extend_from_slice(DOMAIN);
    bytes.push(head_kind);
    frame(&mut bytes, &state.receipt);
    let merkle = encode_proof(&state.receipt_proof);
    assert_eq!(merkle[0], 1);
    assert_eq!(&merkle[1..5], &state.receipt_proof.leaf_index().to_be_bytes());
    assert_eq!(&merkle[5..9], &state.receipt_proof.leaf_count().to_be_bytes());
    assert_eq!(merkle.len(), 10 + 32 * usize::from(merkle[9]));
    frame(&mut bytes, &merkle);
    frame(&mut bytes, &state.header);
    bytes.extend_from_slice(&state.header_signature);
    bytes.extend_from_slice(&state.programs_root);
    state_proof(&mut bytes, &state.programs_root_proof);
    let record = &state.program_record;
    witness(&mut bytes, &record.key, &record.value, &record.proof);
    match &state.lifecycle {
        ProgramLifecycleProof::Status(status) => {
            bytes.push(1);
            witness(&mut bytes, &status.key, &status.value, &status.proof);
        }
        ProgramLifecycleProof::Active { lower, upper } => {
            bytes.push(0);
            for neighbour in [lower, upper] {
                match neighbour {
                    Some(leaf) => {
                        bytes.push(1);
                        witness(&mut bytes, &leaf.key, &leaf.value, &leaf.proof);
                    }
                    None => bytes.push(0),
                }
            }
        }
    }
    let interface = &fixture.interface_witness;
    witness(&mut bytes, &interface.key, &interface.value, &interface.proof);
    bytes
}

/// Offset of the lifecycle tag in a payload built by `contract_payload`.
fn lifecycle_offset(fixture: &ProtocolFixture) -> usize {
    let state = &fixture.proof.state;
    let record = &state.program_record;
    DOMAIN.len()
        + 1
        + 4
        + state.receipt.len()
        + 4
        + encode_proof(&state.receipt_proof).len()
        + 4
        + state.header.len()
        + 64
        + 32
        + 9
        + 32 * state.programs_root_proof.siblings.len()
        + 4
        + record.key.len()
        + 4
        + record.value.len()
        + 9
        + 32 * record.proof.siblings.len()
}

fn decode_refused(bytes: &[u8]) {
    assert_eq!(
        ProgramStateBundle::decode(bytes),
        Err(ProtocolEvidenceError::Encoding)
    );
}

#[test]
fn kind5_payload_built_from_the_contract_decodes_and_reencodes_identically() {
    let fixture = deployed(70, WASM_V1);
    let payload = contract_payload(0, &fixture);
    let decoded = ProgramStateBundle::decode(&payload)
        .unwrap_or_else(|error| panic!("contract payload: {error}"));
    assert_eq!(decoded, ordinary_bundle(&fixture));
    assert_eq!(decoded.canonical_encoding(), payload);
    assert_eq!(&payload[..31], DOMAIN);
    assert_eq!(payload[30], 0);
    assert_eq!(payload[31], ProgramHeadKind::Ordinary.wire_byte());
    let maintenance = contract_payload(1, &fixture);
    let decoded = ProgramStateBundle::decode(&maintenance)
        .unwrap_or_else(|error| panic!("maintenance-kind payload: {error}"));
    assert_eq!(decoded.head_kind, ProgramHeadKind::Maintenance);
    assert_eq!(decoded.canonical_encoding(), maintenance);
}

#[test]
fn head_kind_byte_and_digest_domains_are_strict_and_distinct() {
    assert_eq!(
        ProgramHeadKind::from_wire_byte(0),
        Some(ProgramHeadKind::Ordinary)
    );
    assert_eq!(
        ProgramHeadKind::from_wire_byte(1),
        Some(ProgramHeadKind::Maintenance)
    );
    for byte in 2..=u8::MAX {
        assert_eq!(ProgramHeadKind::from_wire_byte(byte), None);
    }
    let digest = [0x5a; 32];
    let ordinary = ReceiptEvidenceDigest::OrdinaryUnsignedReceipt(digest);
    let maintenance = ReceiptEvidenceDigest::MaintenanceReceiptSha256(digest);
    assert_ne!(ordinary, maintenance);
    assert_eq!(ordinary.bytes(), maintenance.bytes());
    assert_eq!(ordinary.head_kind(), ProgramHeadKind::Ordinary);
    assert_eq!(maintenance.head_kind(), ProgramHeadKind::Maintenance);
}

#[test]
fn decode_refuses_every_noncanonical_kind5_payload() {
    let fixture = deployed(70, WASM_V1);
    let payload = contract_payload(0, &fixture);

    decode_refused(&[]);
    decode_refused(&payload[..30]);
    let mut wrong_domain = payload.clone();
    wrong_domain[0] ^= 1;
    decode_refused(&wrong_domain);
    let mut no_terminator = payload.clone();
    no_terminator[30] = b'/';
    decode_refused(&no_terminator);
    let mut deployment_domain = b"LayerX/programs/deployment-proof/v2\0".to_vec();
    deployment_domain.extend_from_slice(&payload[31..]);
    decode_refused(&deployment_domain);
    for head_kind in [2, 0x80, u8::MAX] {
        decode_refused(&contract_payload(head_kind, &fixture));
    }
    let mut trailing = payload.clone();
    trailing.push(0);
    decode_refused(&trailing);
    for cut in [32, 36, payload.len() / 2, payload.len() - 1] {
        decode_refused(&payload[..cut]);
    }

    let lifecycle = lifecycle_offset(&fixture);
    assert_eq!(payload[lifecycle], 0);
    let mut lifecycle_tag = payload.clone();
    lifecycle_tag[lifecycle] = 2;
    decode_refused(&lifecycle_tag);
    assert_eq!(payload[lifecycle + 1], 1);
    let mut lower_presence = payload.clone();
    lower_presence[lifecycle + 1] = 2;
    decode_refused(&lower_presence);

    let missing_interface = &payload[..payload.len()
        - (8
            + fixture.interface_witness.key.len()
            + fixture.interface_witness.value.len()
            + 9
            + 32 * fixture.interface_witness.proof.siblings.len())];
    decode_refused(missing_interface);

    let mut oversized_receipt = payload[..32].to_vec();
    oversized_receipt.extend_from_slice(&(40_u32 * 1024 * 1024 + 1).to_be_bytes());
    oversized_receipt.extend_from_slice(&payload[36..]);
    decode_refused(&oversized_receipt);

    let receipt_frame_end = 36 + fixture.proof.state.receipt.len();
    let mut oversized_merkle = payload.clone();
    oversized_merkle[receipt_frame_end..receipt_frame_end + 4]
        .copy_from_slice(&1_035_u32.to_be_bytes());
    decode_refused(&oversized_merkle);

    let mut deep = ordinary_bundle(&fixture);
    deep.state.programs_root_proof.siblings = vec![[0; 32]; 33];
    decode_refused(&deep.canonical_encoding());
}

#[test]
fn ordinary_bundle_verifies_only_against_its_independent_chain_head() {
    let fixture = deployed(70, WASM_V1);
    let verifier = verifier(&fixture);
    let chain = ordinary_chain(&verifier, &fixture);
    let header = decode_batch_header(&fixture.proof.state.header)
        .unwrap_or_else(|error| panic!("fixture header: {error:?}"));
    assert_eq!(chain.head_kind(), ProgramHeadKind::Ordinary);
    assert!(matches!(
        chain.receipt_evidence_digest(),
        ReceiptEvidenceDigest::OrdinaryUnsignedReceipt(_)
    ));
    assert_eq!(chain.receipt_digest(), chain.receipt_evidence_digest().bytes());
    assert_eq!(chain.global_sequence(), header.last_sequence());
    assert_eq!(chain.freshness().observed_sequence, chain.global_sequence());
    assert_eq!(chain.state_root(), header.resulting_state_root());
    assert_eq!(
        chain.state_root(),
        programs_root_commitment(fixture.proof.state.programs_root)
    );
    assert_eq!(chain.sequencer_public_key(), fixture.sequencer_public_key);

    let decoded = ProgramStateBundle::decode(&contract_payload(0, &fixture))
        .unwrap_or_else(|error| panic!("decoded bundle: {error}"));
    let verified = verifier
        .verify_current_program_bundle(
            &decoded,
            &chain,
            program(),
            &fixture.sequencer_public_key,
            NOW,
        )
        .unwrap_or_else(|error| panic!("bound program bundle: {error}"));
    let head = verified.program_head();
    assert_eq!(verified.chain_head(), &chain);
    assert_eq!(head.program(), program());
    assert_eq!(head.receipt_digest(), chain.receipt_digest());
    assert_eq!(head.state_root(), chain.state_root());
    assert_eq!(head.programs_root(), fixture.proof.state.programs_root);
    assert_eq!(head.freshness(), chain.freshness());
    assert_eq!(
        Some(head.valid_until_ms()),
        chain.freshness().observed_at.checked_add(STALENESS_MS)
    );
    assert_eq!(verified.interface().receipt_digest, head.receipt_digest());
    assert_eq!(verified.interface().state_root, head.state_root());
    assert_eq!(verified.interface().interface, support::fixture_interface(WASM_V1));
}

#[test]
fn bundle_head_kind_must_equal_the_chain_head_kind() {
    let fixture = deployed(70, WASM_V1);
    let verifier = verifier(&fixture);
    let chain = ordinary_chain(&verifier, &fixture);
    let mut changed = ordinary_bundle(&fixture);
    changed.head_kind = ProgramHeadKind::Maintenance;
    assert_eq!(
        verifier.verify_current_program_bundle(
            &changed,
            &chain,
            program(),
            &fixture.sequencer_public_key,
            NOW
        ),
        Err(ProgramBundleError::HeadKind)
    );
}

#[test]
fn unauthorised_lni_signer_and_wrong_program_are_refused() {
    let fixture = deployed(70, WASM_V1);
    let verifier = verifier(&fixture);
    let chain = ordinary_chain(&verifier, &fixture);
    let bundle = ordinary_bundle(&fixture);
    let mut signer = fixture.sequencer_public_key;
    signer[0] ^= 1;
    assert_eq!(
        verifier.verify_current_program_bundle(&bundle, &chain, program(), &signer, NOW),
        Err(ProgramBundleError::SignerMismatch)
    );
    assert_eq!(
        verifier.verify_current_program_bundle(
            &bundle,
            &chain,
            other_program(),
            &fixture.sequencer_public_key,
            NOW
        ),
        Err(ProgramBundleError::ProgramMismatch)
    );
}

#[test]
fn cross_associated_or_advanced_heads_are_refused() {
    let pinned = deployed(70, WASM_V1);
    let advanced = deployed(71, WASM_V1);
    let verifier = verifier(&pinned);
    let pinned_chain = ordinary_chain(&verifier, &pinned);
    let advanced_chain = ordinary_chain(&verifier, &advanced);
    assert_ne!(pinned_chain.global_sequence(), advanced_chain.global_sequence());
    assert_eq!(
        verifier.verify_current_program_bundle(
            &ordinary_bundle(&advanced),
            &pinned_chain,
            program(),
            &pinned.sequencer_public_key,
            NOW
        ),
        Err(ProgramBundleError::ChainHeadMismatch)
    );
    assert_eq!(
        verifier.verify_current_program_bundle(
            &ordinary_bundle(&pinned),
            &advanced_chain,
            program(),
            &pinned.sequencer_public_key,
            NOW
        ),
        Err(ProgramBundleError::ChainHeadMismatch)
    );
}

#[test]
fn cross_associated_or_missing_interface_is_refused() {
    let fixture = deployed(70, WASM_V1);
    let unrelated = fixture_with_other_code();
    let verifier = verifier(&fixture);
    let chain = ordinary_chain(&verifier, &fixture);

    let mut foreign = ordinary_bundle(&fixture);
    foreign.interface = unrelated.interface_witness.clone();
    assert!(matches!(
        verifier.verify_current_program_bundle(
            &foreign,
            &chain,
            program(),
            &fixture.sequencer_public_key,
            NOW
        ),
        Err(ProgramBundleError::Interface(_))
    ));

    let record = &fixture.proof.state.program_record;
    let mut missing = ordinary_bundle(&fixture);
    missing.interface = InterfaceStateWitness {
        key: record.key.clone(),
        value: record.value.clone(),
        proof: record.proof.clone(),
    };
    assert!(matches!(
        verifier.verify_current_program_bundle(
            &missing,
            &chain,
            program(),
            &fixture.sequencer_public_key,
            NOW
        ),
        Err(ProgramBundleError::Interface(_))
    ));
}

fn fixture_with_other_code() -> ProtocolFixture {
    deployed(72, WASM_V2)
}

#[test]
fn altered_head_or_state_material_is_refused() {
    let fixture = deployed(70, WASM_V1);
    let other = deployed(71, WASM_V1);
    let verifier = verifier(&fixture);
    let chain = ordinary_chain(&verifier, &fixture);
    let verify = |bundle: &ProgramStateBundle| {
        verifier.verify_current_program_bundle(
            bundle,
            &chain,
            program(),
            &fixture.sequencer_public_key,
            NOW,
        )
    };

    let mut header = ordinary_bundle(&fixture);
    let last = header.state.header.len() - 1;
    header.state.header[last] ^= 1;
    assert!(matches!(verify(&header), Err(ProgramBundleError::Evidence(_))));

    let mut signature = ordinary_bundle(&fixture);
    signature.state.header_signature[0] ^= 1;
    assert!(matches!(verify(&signature), Err(ProgramBundleError::Evidence(_))));

    let mut receipt = ordinary_bundle(&fixture);
    receipt.state.receipt[20] ^= 1;
    assert!(matches!(verify(&receipt), Err(ProgramBundleError::Evidence(_))));

    let mut receipt_proof = ordinary_bundle(&fixture);
    receipt_proof.state.receipt_proof = other.proof.state.receipt_proof.clone();
    receipt_proof.state.receipt = other.proof.state.receipt.clone();
    assert!(matches!(
        verify(&receipt_proof),
        Err(ProgramBundleError::Evidence(_) | ProgramBundleError::ChainHeadMismatch)
    ));

    let mut programs_root = ordinary_bundle(&fixture);
    programs_root.state.programs_root[0] ^= 1;
    assert!(matches!(
        verify(&programs_root),
        Err(ProgramBundleError::Evidence(
            ProtocolEvidenceError::StateRoot | ProtocolEvidenceError::StateProof
        ))
    ));

    let mut zero_root = ordinary_bundle(&fixture);
    zero_root.state.programs_root = [0; 32];
    assert!(matches!(verify(&zero_root), Err(ProgramBundleError::Evidence(_))));

    let mut record_leaf = ordinary_bundle(&fixture);
    let last = record_leaf.state.program_record.value.len() - 1;
    record_leaf.state.program_record.value[last] ^= 1;
    assert!(matches!(verify(&record_leaf), Err(ProgramBundleError::Evidence(_))));

    let mut record_count = ordinary_bundle(&fixture);
    record_count.state.program_record.proof.leaf_count += 1;
    assert!(matches!(verify(&record_count), Err(ProgramBundleError::Evidence(_))));

    let mut record_index = ordinary_bundle(&fixture);
    record_index.state.program_record.proof.leaf_index ^= 1;
    assert!(matches!(verify(&record_index), Err(ProgramBundleError::Evidence(_))));

    let mut no_neighbours = ordinary_bundle(&fixture);
    no_neighbours.state.lifecycle = ProgramLifecycleProof::Active {
        lower: None,
        upper: None,
    };
    assert!(matches!(
        verify(&no_neighbours),
        Err(ProgramBundleError::Evidence(_))
    ));

    let mut forged_neighbour = ordinary_bundle(&fixture);
    forged_neighbour.state.lifecycle = ProgramLifecycleProof::Active {
        lower: Some(StateLeafWitness {
            key: fixture.interface_witness.key.clone(),
            value: fixture.interface_witness.value.clone(),
            proof: StateProof {
                leaf_index: 0,
                leaf_count: 1,
                siblings: Vec::new(),
            },
        }),
        upper: None,
    };
    assert!(matches!(
        verify(&forged_neighbour),
        Err(ProgramBundleError::Evidence(_))
    ));
}

#[test]
fn stale_chain_head_and_stale_bundle_are_refused() {
    let fixture = deployed(70, WASM_V1);
    let verifier = verifier(&fixture);
    let state = &fixture.proof.state;
    let observed_at = 1_700_000_070;
    for now in [observed_at - 1, observed_at + STALENESS_MS + 1] {
        assert_eq!(
            verifier.verify_current_chain_head(
                ProgramHeadKind::Ordinary,
                &state.receipt,
                &state.receipt_proof,
                &state.header,
                &state.header_signature,
                now,
            ),
            Err(ProtocolEvidenceError::Stale)
        );
    }
    let chain = ordinary_chain(&verifier, &fixture);
    assert_eq!(
        verifier.verify_current_program_bundle(
            &ordinary_bundle(&fixture),
            &chain,
            program(),
            &fixture.sequencer_public_key,
            observed_at + STALENESS_MS + 1,
        ),
        Err(ProgramBundleError::Evidence(ProtocolEvidenceError::Stale))
    );
}

fn maintenance_verifier() -> (ProtocolDeploymentVerifier, u64) {
    let header = decode_batch_header(MAINTENANCE_HEADER)
        .unwrap_or_else(|error| panic!("native maintenance header: {error:?}"));
    let verifier = verifier_from_history(
        &[TrustAnchorFixture {
            protocol_version: header.protocol_version(),
            network_id: header.network_id(),
            epoch: header.epoch(),
            sequencer_id: header.sequencer_id(),
            sequencer_public_key: *MAINTENANCE_PUBLIC,
            first_batch: header.batch_number(),
            last_batch: header.batch_number(),
            revoked_from_batch: None,
        }],
        0,
        STALENESS_MS,
    );
    (verifier, header.timestamp_ms())
}

#[test]
fn native_maintenance_head_is_typed_tagged_and_equal_to_the_tuple_api() {
    let header = decode_batch_header(MAINTENANCE_HEADER)
        .unwrap_or_else(|error| panic!("native maintenance header: {error:?}"));
    let (verifier, now) = maintenance_verifier();
    let (proof, root) = build_proof(&[MAINTENANCE_RECEIPT, MAINTENANCE], 1)
        .unwrap_or_else(|error| panic!("maintenance inclusion: {error:?}"));
    assert_eq!(root, header.receipt_merkle_root());

    let typed = verifier
        .verify_current_maintenance_head_typed(
            MAINTENANCE,
            &proof,
            MAINTENANCE_HEADER,
            MAINTENANCE_SIGNATURE,
            now,
        )
        .unwrap_or_else(|error| panic!("typed maintenance head: {error}"));
    let tuple = verifier
        .verify_current_maintenance_head(
            MAINTENANCE,
            &proof,
            MAINTENANCE_HEADER,
            MAINTENANCE_SIGNATURE,
            now,
        )
        .unwrap_or_else(|error| panic!("tuple maintenance head: {error}"));
    assert_eq!(typed.account_state_head(), tuple.0);
    assert_eq!(typed.sequencer_public_key(), tuple.1);
    assert_eq!(typed.sequencer_public_key(), *MAINTENANCE_PUBLIC);
    let sha: [u8; 32] = Sha256::digest(MAINTENANCE).into();
    assert_eq!(typed.maintenance_receipt_sha256(), sha);
    assert_eq!(typed.state_root(), header.resulting_state_root());
    assert_eq!(typed.freshness().observed_sequence, header.last_sequence());
    assert_eq!(typed.freshness().observed_at, header.timestamp_ms());
    assert_eq!(
        verifier.verify_historical_maintenance_head_typed(
            MAINTENANCE,
            &proof,
            MAINTENANCE_HEADER,
            MAINTENANCE_SIGNATURE
        ),
        Ok(typed)
    );

    let chain = verifier
        .verify_current_chain_head(
            ProgramHeadKind::Maintenance,
            MAINTENANCE,
            &proof,
            MAINTENANCE_HEADER,
            MAINTENANCE_SIGNATURE,
            now,
        )
        .unwrap_or_else(|error| panic!("maintenance chain head: {error}"));
    assert_eq!(chain, VerifiedChainHead::from(typed));
    assert_eq!(chain.head_kind(), ProgramHeadKind::Maintenance);
    assert_eq!(
        chain.receipt_evidence_digest(),
        ReceiptEvidenceDigest::MaintenanceReceiptSha256(sha)
    );
    assert_eq!(chain.global_sequence(), header.last_sequence());
    assert_eq!(chain.state_root(), header.resulting_state_root());
    assert_eq!(chain.batch_header_digest(), typed.batch_header_digest());
}

#[test]
fn maintenance_head_refuses_wrong_leaf_kind_signature_and_staleness() {
    let header = decode_batch_header(MAINTENANCE_HEADER)
        .unwrap_or_else(|error| panic!("native maintenance header: {error:?}"));
    let (verifier, now) = maintenance_verifier();
    let (proof, _) = build_proof(&[MAINTENANCE_RECEIPT, MAINTENANCE], 1)
        .unwrap_or_else(|error| panic!("maintenance inclusion: {error:?}"));
    let (first_leaf, _) = build_proof(&[MAINTENANCE_RECEIPT, MAINTENANCE], 0)
        .unwrap_or_else(|error| panic!("activity inclusion: {error:?}"));
    let head = |receipt: &[u8],
                proof: &layerx_proof::merkle::Proof,
                signature: &[u8; 64],
                kind: ProgramHeadKind,
                now: u64| {
        verifier.verify_current_chain_head(kind, receipt, proof, MAINTENANCE_HEADER, signature, now)
    };

    assert!(head(MAINTENANCE, &first_leaf, MAINTENANCE_SIGNATURE, ProgramHeadKind::Maintenance, now).is_err());
    assert!(head(
        MAINTENANCE_RECEIPT,
        &first_leaf,
        MAINTENANCE_SIGNATURE,
        ProgramHeadKind::Maintenance,
        now
    )
    .is_err());
    assert!(head(MAINTENANCE, &proof, MAINTENANCE_SIGNATURE, ProgramHeadKind::Ordinary, now).is_err());
    let mut signature = *MAINTENANCE_SIGNATURE;
    signature[0] ^= 1;
    assert!(head(MAINTENANCE, &proof, &signature, ProgramHeadKind::Maintenance, now).is_err());
    let mut receipt = MAINTENANCE.to_vec();
    let last = receipt.len() - 1;
    receipt[last] ^= 1;
    assert!(head(&receipt, &proof, MAINTENANCE_SIGNATURE, ProgramHeadKind::Maintenance, now).is_err());
    assert_eq!(
        head(
            MAINTENANCE,
            &proof,
            MAINTENANCE_SIGNATURE,
            ProgramHeadKind::Maintenance,
            header.timestamp_ms() + STALENESS_MS + 1
        ),
        Err(ProtocolEvidenceError::Stale)
    );
}

#[test]
fn ordinary_bundle_is_refused_against_a_maintenance_chain_head() {
    let (maintenance_verifier, now) = maintenance_verifier();
    let (proof, _) = build_proof(&[MAINTENANCE_RECEIPT, MAINTENANCE], 1)
        .unwrap_or_else(|error| panic!("maintenance inclusion: {error:?}"));
    let chain = maintenance_verifier
        .verify_current_chain_head(
            ProgramHeadKind::Maintenance,
            MAINTENANCE,
            &proof,
            MAINTENANCE_HEADER,
            MAINTENANCE_SIGNATURE,
            now,
        )
        .unwrap_or_else(|error| panic!("maintenance chain head: {error}"));
    let fixture = deployed(70, WASM_V1);
    assert_eq!(
        maintenance_verifier.verify_current_program_bundle(
            &ordinary_bundle(&fixture),
            &chain,
            program(),
            MAINTENANCE_PUBLIC,
            now
        ),
        Err(ProgramBundleError::HeadKind)
    );
    let mut relabelled = ordinary_bundle(&fixture);
    relabelled.head_kind = ProgramHeadKind::Maintenance;
    assert!(matches!(
        maintenance_verifier.verify_current_program_bundle(
            &relabelled,
            &chain,
            program(),
            MAINTENANCE_PUBLIC,
            now
        ),
        Err(ProgramBundleError::Evidence(_) | ProgramBundleError::ChainHeadMismatch)
    ));
}
