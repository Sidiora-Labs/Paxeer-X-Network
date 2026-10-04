use ed25519_dalek::{Signer as _, SigningKey as EdSigningKey};
use k256::ecdsa::{Signature, SigningKey};
use layerx_crypto::secp256k1;
use layerx_proof::availability::{AvailabilityClass, Chunk};
use layerx_proof::checkpoint::{Attestation, GuarantorKey, SettlementDomain};
use layerx_proof::export::{
    verify_complete, CompleteExportError, IndependentOfflineTrust, OfflineTrustError,
    TrustedCheckpointMembership,
};
use layerx_proof::export_codec::{
    parse_fact_set, AccountStateRecord, ArtifactDecodeError, CheckpointRecord,
    CompleteOfflineArtifact, ExportCodecError, FactRefError, FactSelector, HeaderRecord,
    InclusionRecord, ProofRecord, ReceiptRecord, MAX_FACT_REFS, MAX_FACT_REF_BYTES,
    MAX_RECORD_BYTES, RECORD_VERSION,
};
use layerx_proof::merkle::{build_leaf_hash_proof, encode_proof, Proof};
use layerx_proof::signed_authority::SignedAuthorityHistory;
use layerx_types::verify::VerificationLevel;
use layerx_wire::activity::decode_signed;
use layerx_wire::encode::Encoder;
use layerx_wire::handover::{decode_genesis_trust, sequencer_id};
use layerx_wire::hash::{availability_chunk_digest, batch_header_digest, receipt_digest};
use layerx_wire::limits::PROTOCOL_VERSION;

const GENESIS: &[u8] = include_bytes!("fixtures/signed-authority/genesis.bin");
const HEADERS: &[u8] = include_bytes!("fixtures/signed-authority/headers.bin");
const HANDOVER: &[u8] = include_bytes!("fixtures/signed-authority/handover.activity");
const HEADER_RECORD: usize = 418;
const HEADER_BYTES: usize = 354;
const NO_RECORDS: [Vec<u8>; 0] = [];

fn must<T, E: std::fmt::Debug>(value: Result<T, E>) -> T {
    value.unwrap_or_else(|error| panic!("export fixture: {error:?}"))
}

fn hex(bytes: &[u8]) -> String {
    bytes.iter().map(|byte| format!("{byte:02x}")).collect()
}

fn id(value: u8) -> [u8; 32] {
    let mut out = [value; 32];
    out[0] = 0xa0 | (value & 0x0f);
    out
}

fn receipt_ref(activity: [u8; 32]) -> String {
    format!("receipt:{}", hex(&activity))
}

fn lp(out: &mut Vec<u8>, bytes: &[u8]) {
    out.extend_from_slice(&must(u32::try_from(bytes.len())).to_be_bytes());
    out.extend_from_slice(bytes);
}

// Signed native receipt in the layout the receipt verifier decodes.
fn receipt(activity: [u8; 32], signer: &EdSigningKey) -> Vec<u8> {
    let encode = |signature: Option<[u8; 64]>| {
        let mut encoder = Encoder::new(4096);
        assert_eq!(
            encoder.structure_header_version(0x5201, PROTOCOL_VERSION),
            Ok(())
        );
        assert_eq!(encoder.u16(PROTOCOL_VERSION), Ok(()));
        assert_eq!(encoder.bytes(&activity, 32), Ok(()));
        assert_eq!(encoder.u64(9), Ok(()));
        assert_eq!(encoder.bytes(&[2; 32], 32), Ok(()));
        assert_eq!(encoder.bytes(&[3; 32], 32), Ok(()));
        assert_eq!(encoder.bytes(&[8; 32], 32), Ok(()));
        assert_eq!(encoder.i32(0), Ok(()));
        assert_eq!(encoder.sequence_length(0, 512), Ok(()));
        assert_eq!(encoder.u128(1), Ok(()));
        assert_eq!(encoder.bytes(&[4; 32], 32), Ok(()));
        assert_eq!(encoder.u16(1), Ok(()));
        assert_eq!(encoder.u32(1), Ok(()));
        assert_eq!(encoder.u32(1), Ok(()));
        assert_eq!(encoder.u8(1), Ok(()));
        assert_eq!(encoder.bytes(&[5; 32], 32), Ok(()));
        assert_eq!(encoder.u128(25), Ok(()));
        assert_eq!(encoder.bytes(&[6; 32], 32), Ok(()));
        assert_eq!(encoder.u128(100), Ok(()));
        assert_eq!(encoder.u128(75), Ok(()));
        assert_eq!(encoder.u64(1), Ok(()));
        assert_eq!(encoder.bytes(&[7; 32], 32), Ok(()));
        assert_eq!(encoder.u128(10), Ok(()));
        assert_eq!(encoder.u128(35), Ok(()));
        assert_eq!(encoder.bytes(&[9; 32], 32), Ok(()));
        assert_eq!(encoder.bytes(&[10; 32], 32), Ok(()));
        assert_eq!(encoder.bytes(&[11; 32], 32), Ok(()));
        assert_eq!(encoder.u64(1_000), Ok(()));
        assert_eq!(encoder.u8(u8::from(signature.is_some())), Ok(()));
        if let Some(value) = signature {
            assert_eq!(encoder.bytes(&value, 64), Ok(()));
        }
        encoder.finish()
    };
    let digest = must(receipt_digest(&encode(None)));
    encode(Some(signer.sign(&digest).to_bytes()))
}

fn ed_key(value: u8) -> EdSigningKey {
    EdSigningKey::from_bytes(&[value; 32])
}

struct Native {
    history: SignedAuthorityHistory,
    registry: layerx_types::payload::ModuleRegistry,
    first_key: [u8; 32],
}

// Trust is built only from the checked-in native genesis and its signed headers.
fn native() -> Native {
    let genesis = must(decode_genesis_trust(GENESIS));
    let activity = must(decode_signed(HANDOVER, &genesis.registry));
    let mut history = must(SignedAuthorityHistory::from_genesis(
        genesis.network_id,
        genesis.canonical_state_root,
        genesis.initial_sequencer_key,
        genesis.governance_witness,
    ));
    for (index, record) in HEADERS.chunks_exact(HEADER_RECORD).enumerate() {
        let packet = (index >= 13).then_some(activity.payload());
        must(history.advance(
            &record[..HEADER_BYTES],
            &must(record[HEADER_BYTES..].try_into()),
            packet,
        ));
    }
    Native {
        history,
        registry: genesis.registry,
        first_key: genesis.initial_sequencer_key,
    }
}

fn guarantor(value: u8) -> (SigningKey, [u8; 33], [u8; 32]) {
    let mut scalar = [0_u8; 32];
    scalar[31] = value;
    let signing = must(SigningKey::from_bytes((&scalar).into()));
    let public_key: [u8; 33] = must(
        signing
            .verifying_key()
            .to_encoded_point(true)
            .as_bytes()
            .try_into(),
    );
    let mut guarantor_id = [0_u8; 32];
    guarantor_id[0] = value;
    (signing, public_key, guarantor_id)
}

fn domain() -> SettlementDomain {
    SettlementDomain::new(31_337, [0x55; 20])
}

fn membership(values: &[u8], threshold: usize) -> TrustedCheckpointMembership {
    let keys = values
        .iter()
        .map(|value| {
            let (_, public_key, guarantor_id) = guarantor(*value);
            GuarantorKey::new(guarantor_id, public_key, true)
        })
        .collect();
    must(TrustedCheckpointMembership::new(1, keys, threshold))
}

fn trust() -> IndependentOfflineTrust {
    let native = native();
    must(IndependentOfflineTrust::new(
        native.registry,
        native.history,
        domain(),
        vec![membership(&[1, 2, 3], 2)],
    ))
}

// Header record over the first real signed header with the trusted interval key.
fn header_record(index: usize, public_key: [u8; 32], first: u64, last: u64) -> HeaderRecord {
    let record = &HEADERS[index * HEADER_RECORD..(index + 1) * HEADER_RECORD];
    HeaderRecord {
        canonical_header: record[..HEADER_BYTES].to_vec(),
        signature: must(record[HEADER_BYTES..].try_into()),
        sequencer_id: must(sequencer_id(&public_key)),
        public_key,
        first_batch: first,
        last_batch: last,
    }
}

fn header_bytes(record: &HeaderRecord) -> Vec<u8> {
    let mut out = b"LXHD".to_vec();
    out.push(1);
    lp(&mut out, &record.canonical_header);
    out.extend_from_slice(&record.signature);
    out.extend_from_slice(&record.sequencer_id);
    out.extend_from_slice(&record.public_key);
    out.extend_from_slice(&record.first_batch.to_be_bytes());
    out.extend_from_slice(&record.last_batch.to_be_bytes());
    out
}

fn receipt_record_bytes(activity: [u8; 32], receipt: &[u8], sequencer: [u8; 32]) -> Vec<u8> {
    let mut out = b"LXRF".to_vec();
    out.push(1);
    lp(&mut out, receipt_ref(activity).as_bytes());
    lp(&mut out, receipt);
    out.extend_from_slice(&[4; 32]);
    out.extend_from_slice(&[5; 32]);
    out.extend_from_slice(&[2; 32]);
    out.extend_from_slice(&[3; 32]);
    out.extend_from_slice(&sequencer);
    let unsigned_digest = {
        let signature_offset = receipt.len() - 65;
        let mut unsigned = receipt[..signature_offset].to_vec();
        unsigned.push(0);
        must(receipt_digest(&unsigned))
    };
    out.extend_from_slice(&unsigned_digest);
    out
}

fn single_proof() -> Proof {
    must(Proof::new(0, 1, Vec::new()))
}

fn pair_proof(index: u32) -> Proof {
    must(Proof::new(index, 2, vec![[0x31; 32]]))
}

fn inclusion_bytes(
    kind: u8,
    reference: &str,
    leaf: &[u8],
    proof: &Proof,
    digest: [u8; 32],
) -> Vec<u8> {
    let mut out = b"LXIP".to_vec();
    out.push(1);
    out.push(kind);
    lp(&mut out, reference.as_bytes());
    lp(&mut out, leaf);
    lp(&mut out, &encode_proof(proof));
    out.extend_from_slice(&digest);
    out
}

fn account_state_bytes(
    reference: &str,
    variant: u8,
    receipt: &[u8],
    digest: [u8; 32],
    tail: Option<(u32, u32, [u8; 32])>,
) -> Vec<u8> {
    let mut out = b"LXAS".to_vec();
    out.push(1);
    lp(&mut out, reference.as_bytes());
    out.push(variant);
    out.extend_from_slice(&id(0x22));
    lp(&mut out, &[0x41; 48]);
    out.extend_from_slice(&[0x42; 32]);
    out.extend_from_slice(&[0x43; 32]);
    out.extend_from_slice(&[0x44; 32]);
    lp(&mut out, &encode_proof(&pair_proof(0)));
    lp(&mut out, &encode_proof(&pair_proof(1)));
    lp(&mut out, &encode_proof(&single_proof()));
    lp(&mut out, receipt);
    lp(&mut out, &encode_proof(&pair_proof(1)));
    out.extend_from_slice(&digest);
    if let Some((count, version, dependency)) = tail {
        out.extend_from_slice(&count.to_be_bytes());
        out.extend_from_slice(&version.to_be_bytes());
        out.extend_from_slice(&dependency);
    }
    out
}

fn attestation(guarantor_value: u8) -> Attestation {
    let (signing, _, guarantor_id) = guarantor(guarantor_value);
    let checkpoint = [0x61; 32];
    let attested_at_ms = 1_000 + u64::from(guarantor_value);
    let mut message = [0_u8; 189];
    message[..2].copy_from_slice(&PROTOCOL_VERSION.to_be_bytes());
    message[2..6].copy_from_slice(&42_u32.to_be_bytes());
    message[6..14].copy_from_slice(&31_337_u64.to_be_bytes());
    message[14..34].copy_from_slice(&[0x55; 20]);
    message[34..42].copy_from_slice(&7_u64.to_be_bytes());
    message[42..74].copy_from_slice(&checkpoint);
    message[74..106].copy_from_slice(&checkpoint);
    message[106..138].copy_from_slice(&guarantor_id);
    message[138..146].copy_from_slice(&8_u64.to_be_bytes());
    message[146..178].copy_from_slice(&[12; 32]);
    message[178] = 1;
    message[179] = 1;
    message[180] = 0x1f;
    message[181..].copy_from_slice(&attested_at_ms.to_be_bytes());
    let digest = must(layerx_wire::hash::checkpoint_attestation_digest(&message));
    let (signature, recovery): (Signature, _) = must(signing.sign_prehash_recoverable(&digest));
    let signer = must(secp256k1::evm_address(
        signing.verifying_key().to_encoded_point(true).as_bytes(),
    ));
    Attestation::new(
        PROTOCOL_VERSION,
        42,
        31_337,
        [0x55; 20],
        7,
        checkpoint,
        checkpoint,
        guarantor_id,
        8,
        [12; 32],
        true,
        true,
        0x1f,
        attested_at_ms,
        signer,
        signature.to_bytes().into(),
        27 + u8::from(recovery),
    )
}

struct Chunks {
    chunks: Vec<Chunk>,
    proofs: Vec<Proof>,
}

fn availability() -> Chunks {
    let mut chunks: Vec<Chunk> = [
        (AvailabilityClass::Activities, b"activity".to_vec()),
        (AvailabilityClass::Receipts, b"receipt".to_vec()),
        (AvailabilityClass::Oracle, b"oracle".to_vec()),
    ]
    .into_iter()
    .enumerate()
    .map(|(index, (class, bytes))| Chunk {
        batch_number: 1,
        index: must(u32::try_from(index)),
        class,
        class_offset: 0,
        bytes,
        claimed_hash: [0; 32],
    })
    .collect();
    for chunk in &mut chunks {
        chunk.claimed_hash = must(availability_chunk_digest(
            chunk.batch_number,
            chunk.index,
            chunk.class as u8,
            chunk.class_offset,
            &chunk.bytes,
        ));
    }
    let hashes: Vec<_> = chunks.iter().map(|chunk| chunk.claimed_hash).collect();
    let proofs = (0..hashes.len())
        .map(|index| must(build_leaf_hash_proof(&hashes, index)).0)
        .collect();
    Chunks { chunks, proofs }
}

fn chunk_bytes(chunk: &Chunk, proof: &Proof) -> Vec<u8> {
    let mut out = chunk.batch_number.to_be_bytes().to_vec();
    out.extend_from_slice(&chunk.index.to_be_bytes());
    out.push(chunk.class as u8);
    out.extend_from_slice(&chunk.class_offset.to_be_bytes());
    lp(&mut out, &chunk.bytes);
    out.extend_from_slice(&chunk.claimed_hash);
    lp(&mut out, &encode_proof(proof));
    out
}

fn checkpoint_bytes(
    reference: &str,
    header: &[u8],
    order: &[usize],
    chunk_count: Option<u16>,
) -> Vec<u8> {
    let attestations = [attestation(1), attestation(2)];
    let mut certificate = Vec::new();
    lp(&mut certificate, header);
    lp(&mut certificate, b"validity");
    certificate.push(2);
    certificate.push(2);
    for item in &attestations {
        certificate.extend_from_slice(&item.canonical_statement());
        certificate.extend_from_slice(&item.signer());
        certificate.extend_from_slice(&item.signature());
        certificate.push(item.signature_v());
    }
    certificate.push(0);
    let mut out = b"LXCP".to_vec();
    out.push(1);
    lp(&mut out, reference.as_bytes());
    lp(&mut out, &certificate);
    out.extend_from_slice(&1_u64.to_be_bytes());
    out.extend_from_slice(&3_u16.to_be_bytes());
    for value in [1, 2, 3] {
        let (_, public_key, guarantor_id) = guarantor(value);
        out.extend_from_slice(&guarantor_id);
        out.extend_from_slice(&public_key);
        out.push(1);
    }
    out.extend_from_slice(&[0x61; 32]);
    out.push(0);
    let data = availability();
    let count = chunk_count.unwrap_or(must(u16::try_from(order.len())));
    out.extend_from_slice(&count.to_be_bytes());
    for index in order {
        out.extend_from_slice(&chunk_bytes(&data.chunks[*index], &data.proofs[*index]));
    }
    out
}

fn checkpoint_ref(activity: [u8; 32], batch: u64) -> String {
    format!("checkpoint:{}:{batch}", hex(&activity))
}

fn state_ref(activity: [u8; 32], account: [u8; 32]) -> String {
    format!("state:{}:{}", hex(&activity), hex(&account))
}

// Every decode must also refuse each strict prefix, one appended byte and a later version.
fn assert_strict<T>(bytes: &[u8], decode: impl Fn(&[u8]) -> Result<T, ExportCodecError>) {
    for end in 0..bytes.len() {
        assert!(decode(&bytes[..end]).is_err(), "prefix {end} accepted");
    }
    let mut trailing = bytes.to_vec();
    trailing.push(0);
    assert_eq!(
        decode(&trailing).err(),
        Some(ExportCodecError::TrailingBytes)
    );
    let mut version = bytes.to_vec();
    version[4] = RECORD_VERSION + 1;
    assert_eq!(decode(&version).err(), Some(ExportCodecError::Version));
    let mut magic = bytes.to_vec();
    magic[0] ^= 0x20;
    assert_eq!(decode(&magic).err(), Some(ExportCodecError::Magic));
    let mut overflow = bytes.to_vec();
    overflow[5..9].copy_from_slice(&u32::MAX.to_be_bytes());
    assert!(decode(&overflow).is_err());
}

#[test]
fn fact_grammar_accepts_each_canonical_form_in_request_order() {
    let activity = id(1);
    let account = id(2);
    let texts = [
        checkpoint_ref(activity, u64::MAX),
        state_ref(activity, account),
        receipt_ref(activity),
        format!("activity:{}", hex(&activity)),
        checkpoint_ref(activity, 1),
    ];
    let parsed = must(parse_fact_set(&texts));
    assert_eq!(
        parsed,
        vec![
            FactSelector::Checkpoint {
                activity_id: activity,
                batch_number: u64::MAX
            },
            FactSelector::State {
                activity_id: activity,
                account_id: account
            },
            FactSelector::Receipt {
                activity_id: activity
            },
            FactSelector::Activity {
                activity_id: activity
            },
            FactSelector::Checkpoint {
                activity_id: activity,
                batch_number: 1
            },
        ]
    );
    for (text, selector) in texts.iter().zip(&parsed) {
        assert_eq!(&selector.canonical_text(), text);
        assert_eq!(selector.activity_id(), activity);
    }
    assert_eq!(state_ref(activity, account).len(), MAX_FACT_REF_BYTES);
    let sixteen: Vec<String> = (1..=16).map(|value| receipt_ref(id(value))).collect();
    assert_eq!(MAX_FACT_REFS, 16);
    assert_eq!(must(parse_fact_set(&sixteen)).len(), 16);
}

#[test]
fn fact_grammar_refuses_every_noncanonical_form() {
    let good = hex(&id(1));
    let zero = hex(&[0; 32]);
    let upper = good.to_uppercase();
    let refused = [
        String::new(),
        format!("receipt:{zero}"),
        format!("activity:{zero}"),
        format!("state:{zero}:{good}"),
        format!("state:{good}:{zero}"),
        format!("checkpoint:{zero}:1"),
        format!("receipt:{upper}"),
        format!("Receipt:{good}"),
        format!("receipt:0x{}", &good[2..]),
        format!("receipt:{}", &good[..63]),
        format!("receipt:{good}0"),
        format!(" receipt:{good}"),
        format!("receipt:{good} "),
        format!("receipt: {good}"),
        format!("receipt:{good}\n"),
        format!("receipt:{good}:1"),
        format!("account:{good}"),
        format!("balance:{good}"),
        format!("state:{good}"),
        format!("state:{good}:{good}:{good}"),
        format!("checkpoint:{good}"),
        format!("checkpoint:{good}:"),
        format!("checkpoint:{good}:01"),
        format!("checkpoint:{good}:+1"),
        format!("checkpoint:{good}:-1"),
        format!("checkpoint:{good}:1 "),
        format!("checkpoint:{good}:18446744073709551616"),
        format!("checkpoint:{good}:99999999999999999999"),
        format!("{}0", state_ref(id(1), id(2))),
    ];
    for text in &refused {
        assert!(FactSelector::parse(text).is_err(), "accepted {text:?}");
        assert!(
            parse_fact_set(&[text.as_str()]).is_err(),
            "set accepted {text:?}"
        );
    }
    assert_eq!(
        FactSelector::parse(&format!("receipt:{zero}")),
        Err(FactRefError::ZeroIdentifier)
    );
    assert_eq!(
        FactSelector::parse(&format!("checkpoint:{good}:0")),
        Err(FactRefError::ZeroBatch)
    );
    assert_eq!(
        FactSelector::parse(&format!("checkpoint:{good}:18446744073709551616")),
        Err(FactRefError::Decimal)
    );
    assert_eq!(
        FactSelector::parse(&format!("checkpoint:{good}:01")),
        Err(FactRefError::Decimal)
    );
    assert_eq!(
        FactSelector::parse(&format!("ledger:{good}")),
        Err(FactRefError::UnknownKind)
    );
    let long = format!("{}0", state_ref(id(1), id(2)));
    assert_eq!(long.len(), MAX_FACT_REF_BYTES + 1);
    assert_eq!(
        FactSelector::parse(&long),
        Err(FactRefError::TooLong {
            length: MAX_FACT_REF_BYTES + 1
        })
    );
    let empty: [&str; 0] = [];
    assert_eq!(parse_fact_set(&empty), Err(FactRefError::NoFacts));
    let seventeen: Vec<String> = (1..=17).map(|value| receipt_ref(id(value))).collect();
    assert_eq!(
        parse_fact_set(&seventeen),
        Err(FactRefError::TooMany { count: 17 })
    );
    let repeated = [receipt_ref(id(1)), receipt_ref(id(2)), receipt_ref(id(1))];
    assert_eq!(
        parse_fact_set(&repeated),
        Err(FactRefError::Duplicate { index: 2 })
    );
}

#[test]
fn receipt_header_and_inclusion_records_round_trip_their_frozen_byte_layouts() {
    let native = native();
    let signer = ed_key(9);
    let activity = id(1);
    let receipt_bytes = receipt(activity, &signer);

    let header = header_record(0, native.first_key, 1, 13);
    let header_wire = header_bytes(&header);
    let decoded = must(HeaderRecord::decode(&header_wire));
    assert_eq!(decoded.canonical_header, header.canonical_header);
    assert_eq!(decoded.signature, header.signature);
    assert_eq!(decoded.sequencer_id, header.sequencer_id);
    assert_eq!(decoded.public_key, native.first_key);
    assert_eq!((decoded.first_batch, decoded.last_batch), (1, 13));
    assert_eq!(must(decoded.encode()), header_wire);
    assert_eq!(
        must(decoded.digest()),
        must(batch_header_digest(&HEADERS[..HEADER_BYTES]))
    );
    assert_strict(&header_wire, HeaderRecord::decode);

    let receipt_wire =
        receipt_record_bytes(activity, &receipt_bytes, signer.verifying_key().to_bytes());
    let record = must(ReceiptRecord::decode(&receipt_wire));
    assert_eq!(
        record.reference,
        FactSelector::Receipt {
            activity_id: activity
        }
    );
    assert_eq!(record.canonical_receipt, receipt_bytes);
    assert_eq!(must(record.encode()), receipt_wire);
    assert_strict(&receipt_wire, ReceiptRecord::decode);
    let mut wrong_kind = b"LXRF".to_vec();
    wrong_kind.push(1);
    lp(
        &mut wrong_kind,
        format!("activity:{}", hex(&activity)).as_bytes(),
    );
    wrong_kind.extend_from_slice(&receipt_wire[9 + receipt_ref(activity).len()..]);
    assert_eq!(
        ReceiptRecord::decode(&wrong_kind).err(),
        Some(ExportCodecError::ReferenceKind)
    );

    let digest = must(header.digest());
    for (kind, reference, leaf) in [
        (2_u8, receipt_ref(activity), receipt_bytes.clone()),
        (
            1_u8,
            format!("activity:{}", hex(&activity)),
            b"activity-leaf".to_vec(),
        ),
    ] {
        let wire = inclusion_bytes(kind, &reference, &leaf, &pair_proof(1), digest);
        let decoded = must(InclusionRecord::decode(&wire));
        assert!(matches!(
            (&decoded.kind, kind),
            (layerx_proof::export_codec::InclusionRecordKind::Activity, 1)
                | (layerx_proof::export_codec::InclusionRecordKind::Receipt, 2)
        ));
        assert_eq!(decoded.reference.canonical_text(), reference);
        assert_eq!(decoded.canonical_leaf, leaf);
        assert_eq!(decoded.proof, pair_proof(1));
        assert_eq!(decoded.header_digest, digest);
        assert_eq!(must(decoded.encode()), wire);
        let routed = must(ProofRecord::decode(&wire));
        assert_eq!(must(routed.encode()), wire);
        assert_strict(&wire, InclusionRecord::decode);
        for tag in [0_u8, 3, 0xff] {
            let mut unknown = wire.clone();
            unknown[5] = tag;
            assert_eq!(
                InclusionRecord::decode(&unknown).err(),
                Some(ExportCodecError::Tag)
            );
        }
    }
    // Receipt inclusion must not travel under the activity kind.
    let swapped = inclusion_bytes(
        1,
        &receipt_ref(activity),
        &receipt_bytes,
        &pair_proof(1),
        digest,
    );
    assert_eq!(
        InclusionRecord::decode(&swapped).err(),
        Some(ExportCodecError::ReferenceKind)
    );
    // A Merkle path that does not match its tree size is not a canonical proof.
    let mut bad_proof = inclusion_bytes(
        2,
        &receipt_ref(activity),
        &receipt_bytes,
        &pair_proof(1),
        digest,
    );
    let proof_at = bad_proof.len() - 32 - encode_proof(&pair_proof(1)).len();
    bad_proof[proof_at + 5..proof_at + 9].copy_from_slice(&3_u32.to_be_bytes());
    assert!(InclusionRecord::decode(&bad_proof).is_err());
}

#[test]
fn account_state_records_round_trip_both_variants_and_refuse_unknown_variants() {
    let signer = ed_key(9);
    let activity = id(1);
    let account = id(0x22);
    let reference = state_ref(activity, account);
    let receipt_bytes = receipt(activity, &signer);
    let digest = [0x51; 32];
    let plain = account_state_bytes(&reference, 1, &receipt_bytes, digest, None);
    let decoded = must(AccountStateRecord::decode(&plain));
    assert_eq!(
        decoded.reference,
        FactSelector::State {
            activity_id: activity,
            account_id: account
        }
    );
    assert_eq!(decoded.account_id, account);
    assert_eq!(decoded.account_value, vec![0x41; 48]);
    assert_eq!(decoded.account_proof, pair_proof(0));
    assert_eq!(decoded.account_tree_proof, pair_proof(1));
    assert_eq!(decoded.universal_root_proof, single_proof());
    assert_eq!(decoded.receipt_bytes, receipt_bytes);
    assert_eq!(decoded.header_digest, digest);
    assert_eq!(must(decoded.encode()), plain);
    assert_eq!(must(must(ProofRecord::decode(&plain)).encode()), plain);
    assert_strict(&plain, AccountStateRecord::decode);

    let maintained = account_state_bytes(
        &reference,
        2,
        b"maintenance",
        digest,
        Some((3, 7, activity)),
    );
    let decoded = must(AccountStateRecord::decode(&maintained));
    assert!(matches!(
        decoded.variant,
        layerx_proof::export_codec::AccountStateVariant::Maintenance {
            activity_count: 3,
            parameter_version: 7,
            dependency_receipt_activity_id,
        } if dependency_receipt_activity_id == activity
    ));
    assert_eq!(must(decoded.encode()), maintained);
    assert_strict(&maintained, AccountStateRecord::decode);
    // Variant 2 without its dependency tail, and variant 1 with one, are not canonical.
    assert!(AccountStateRecord::decode(&maintained[..maintained.len() - 40]).is_err());
    let mut extended = plain.clone();
    extended.extend_from_slice(&maintained[maintained.len() - 40..]);
    assert!(AccountStateRecord::decode(&extended).is_err());
    let variant_at = 9 + reference.len();
    for tag in [0_u8, 3, 0xff] {
        let mut unknown = plain.clone();
        unknown[variant_at] = tag;
        assert_eq!(
            AccountStateRecord::decode(&unknown).err(),
            Some(ExportCodecError::Tag)
        );
    }
}

#[test]
fn checkpoint_record_round_trips_and_refuses_partial_reordered_or_unknown_material() {
    let activity = id(1);
    let reference = checkpoint_ref(activity, 1);
    let header = &HEADERS[..HEADER_BYTES];
    let wire = checkpoint_bytes(&reference, header, &[0, 1, 2], None);
    let decoded = must(CheckpointRecord::decode(&wire));
    assert_eq!(
        decoded.reference,
        FactSelector::Checkpoint {
            activity_id: activity,
            batch_number: 1
        }
    );
    assert_eq!(decoded.certificate.checkpoint().header_bytes(), header);
    assert_eq!(decoded.certificate.threshold(), 2);
    assert_eq!(decoded.certificate.attestations().len(), 2);
    assert_eq!(decoded.certificate.attestations()[0], attestation(1));
    assert_eq!(decoded.set_version, 1);
    assert_eq!(decoded.bonded_set.len(), 3);
    assert_eq!(decoded.checkpoint_id, [0x61; 32]);
    assert_eq!(decoded.registered_settlement_reference, None);
    let data = availability();
    assert_eq!(decoded.availability.len(), 3);
    for (position, (chunk, proof)) in decoded.availability.iter().enumerate() {
        assert_eq!(chunk, &data.chunks[position]);
        assert_eq!(proof, &data.proofs[position]);
    }
    assert_eq!(must(decoded.encode()), wire);
    assert_strict(&wire, CheckpointRecord::decode);

    for order in [
        &[1_usize, 0, 2][..],
        &[0, 2][..],
        &[0, 1, 1][..],
        &[2, 1, 0][..],
    ] {
        let reordered = checkpoint_bytes(&reference, header, order, None);
        assert!(
            CheckpointRecord::decode(&reordered).is_err(),
            "order {order:?} accepted"
        );
    }
    assert!(CheckpointRecord::decode(&checkpoint_bytes(&reference, header, &[], None)).is_err());
    assert!(
        CheckpointRecord::decode(&checkpoint_bytes(&reference, header, &[0, 1, 2], Some(4)))
            .is_err()
    );
    assert!(
        CheckpointRecord::decode(&checkpoint_bytes(&reference, header, &[0, 1, 2], Some(2)))
            .is_err()
    );
    let mut unknown_class = wire.clone();
    let class_at = wire.len() - chunk_bytes(&data.chunks[2], &data.proofs[2]).len() + 12;
    unknown_class[class_at] = 6;
    assert!(CheckpointRecord::decode(&unknown_class).is_err());
    let mut option_tag = wire.clone();
    let option_at = wire.len()
        - 2
        - (0..3)
            .map(|index| chunk_bytes(&data.chunks[index], &data.proofs[index]).len())
            .sum::<usize>()
        - 1;
    option_tag[option_at] = 2;
    assert!(CheckpointRecord::decode(&option_tag).is_err());
    assert!(CheckpointRecord::decode(&checkpoint_bytes(
        &receipt_ref(activity),
        header,
        &[0, 1, 2],
        None
    ))
    .is_err());
}

#[test]
fn record_size_is_capped_at_exactly_one_mebibyte() {
    let digest = [0x51; 32];
    let reference = format!("activity:{}", hex(&id(1)));
    let empty = inclusion_bytes(1, &reference, &[], &single_proof(), digest).len();
    let fits = vec![0x5a; MAX_RECORD_BYTES - empty];
    let exact = inclusion_bytes(1, &reference, &fits, &single_proof(), digest);
    assert_eq!(exact.len(), MAX_RECORD_BYTES);
    let decoded = must(InclusionRecord::decode(&exact));
    assert_eq!(must(decoded.encode()).len(), MAX_RECORD_BYTES);
    let over = vec![0x5a; MAX_RECORD_BYTES - empty + 1];
    let oversized = inclusion_bytes(1, &reference, &over, &single_proof(), digest);
    assert_eq!(oversized.len(), MAX_RECORD_BYTES + 1);
    assert_eq!(
        InclusionRecord::decode(&oversized).err(),
        Some(ExportCodecError::Bound)
    );
    let mut record = decoded;
    record.canonical_leaf = over;
    assert_eq!(record.encode().err(), Some(ExportCodecError::Bound));
}

fn receipt_artifact(header: &HeaderRecord, activity: [u8; 32]) -> Vec<Vec<u8>> {
    let signer = ed_key(9);
    let receipt_bytes = receipt(activity, &signer);
    vec![
        receipt_record_bytes(activity, &receipt_bytes, signer.verifying_key().to_bytes()),
        inclusion_bytes(
            2,
            &receipt_ref(activity),
            &receipt_bytes,
            &single_proof(),
            must(header.digest()),
        ),
        header_bytes(header),
    ]
}

fn decode_artifact(
    facts: &[String],
    parts: &[Vec<u8>],
    certificates: &[Vec<u8>],
) -> CompleteOfflineArtifact {
    must(CompleteOfflineArtifact::decode(
        facts,
        &parts[..1],
        &parts[1..parts.len() - 1],
        certificates,
        &parts[parts.len() - 1..],
    ))
}

#[test]
fn artifact_container_round_trips_and_refuses_duplicate_or_malformed_buckets() {
    let native = native();
    let activity = id(1);
    let header = header_record(0, native.first_key, 1, 13);
    let parts = receipt_artifact(&header, activity);
    let facts = vec![receipt_ref(activity)];
    let artifact = decode_artifact(&facts, &parts, &[]);
    assert_eq!(
        artifact.facts,
        vec![FactSelector::Receipt {
            activity_id: activity
        }]
    );
    let encoded = must(artifact.encode());
    assert_eq!(encoded.facts, facts);
    assert_eq!(encoded.receipts, parts[..1].to_vec());
    assert_eq!(encoded.proofs, parts[1..2].to_vec());
    assert_eq!(encoded.headers, parts[2..].to_vec());
    assert!(encoded.certificates.is_empty());
    let no_certificates: [Vec<u8>; 0] = [];
    let doubled = [receipt_ref(activity), receipt_ref(activity)];
    assert_eq!(
        CompleteOfflineArtifact::decode(
            &doubled,
            &parts[..1],
            &parts[1..2],
            &no_certificates,
            &parts[2..]
        )
        .err(),
        Some(ArtifactDecodeError::Facts(FactRefError::Duplicate {
            index: 1
        }))
    );
    let mut trailing = parts[2].clone();
    trailing.push(0);
    assert_eq!(
        CompleteOfflineArtifact::decode(
            &facts,
            &parts[..1],
            &parts[1..2],
            &no_certificates,
            &[trailing]
        )
        .err(),
        Some(ArtifactDecodeError::Record {
            bucket: "headers",
            index: 0,
            error: ExportCodecError::TrailingBytes,
        })
    );
    // A header record in the receipt bucket is not reinterpreted.
    assert!(CompleteOfflineArtifact::decode(
        &facts,
        &parts[2..],
        &parts[1..2],
        &no_certificates,
        &parts[2..]
    )
    .is_err());
}

#[test]
fn independent_trust_refuses_false_membership_threshold_and_network() {
    let keys = |values: &[u8]| -> Vec<GuarantorKey> {
        values
            .iter()
            .map(|value| {
                let (_, public_key, guarantor_id) = guarantor(*value);
                GuarantorKey::new(guarantor_id, public_key, true)
            })
            .collect()
    };
    assert!(TrustedCheckpointMembership::new(1, keys(&[1, 2, 3]), 0).is_err());
    assert!(TrustedCheckpointMembership::new(1, keys(&[1, 2, 3]), 4).is_err());
    assert!(TrustedCheckpointMembership::new(1, Vec::new(), 1).is_err());
    assert!(TrustedCheckpointMembership::new(1, keys(&[1, 1, 2]), 2).is_err());
    let native = native();
    assert!(matches!(
        IndependentOfflineTrust::new(
            native.registry,
            native.history,
            domain(),
            vec![membership(&[1, 2, 3], 2), membership(&[1, 2, 4], 2)],
        ),
        Err(OfflineTrustError::DuplicateSetVersion)
    ));
    let trust = trust();
    assert_eq!(trust.settlement(), domain());
    assert_eq!(trust.network_id(), self::native().history.network_id());
}

#[test]
fn settlement_anchored_is_refused_without_an_offline_finality_proof() {
    let native = native();
    let activity = id(1);
    let header = header_record(0, native.first_key, 1, 13);
    let artifact = decode_artifact(
        &[receipt_ref(activity)],
        &receipt_artifact(&header, activity),
        &[],
    );
    assert_eq!(
        verify_complete(&artifact, &trust(), VerificationLevel::SETTLEMENT_ANCHORED).err(),
        Some(CompleteExportError::SettlementAnchoringUnavailable)
    );
}

#[test]
fn rogue_sequencer_key_and_false_key_range_are_refused_against_independent_trust() {
    let native = native();
    let activity = id(1);
    let trust = trust();
    let level = VerificationLevel::BATCH_INCLUDED;
    let genuine = header_record(0, native.first_key, 1, 13);
    let control = verify_complete(
        &decode_artifact(
            &[receipt_ref(activity)],
            &receipt_artifact(&genuine, activity),
            &[],
        ),
        &trust,
        level,
    );
    // The receipt is not a leaf of the real batch, so the control cannot pass either.
    assert!(control.is_err());
    assert!(!matches!(control, Err(CompleteExportError::Header { .. })));
    let rogue = ed_key(0x77).verifying_key().to_bytes();
    let handover = native.history.intervals()[1].public_key();
    for header in [
        header_record(0, rogue, 1, 13),
        header_record(0, handover, 1, 13),
        header_record(0, native.first_key, 2, 13),
        header_record(0, native.first_key, 1, 0),
        header_record(14, native.first_key, 1, 15),
        {
            let mut wrong_id = header_record(0, native.first_key, 1, 13);
            wrong_id.sequencer_id = [0x01; 32];
            wrong_id
        },
    ] {
        let artifact = decode_artifact(
            &[receipt_ref(activity)],
            &receipt_artifact(&header, activity),
            &[],
        );
        assert!(
            matches!(
                verify_complete(&artifact, &trust, level),
                Err(CompleteExportError::Header { fact: 0 })
            ),
            "header {:?} accepted",
            (header.public_key, header.first_batch, header.last_batch)
        );
    }
    let mut forged = header_record(0, native.first_key, 1, 13);
    forged.canonical_header[100] ^= 1;
    let artifact = decode_artifact(
        &[receipt_ref(activity)],
        &receipt_artifact(&forged, activity),
        &[],
    );
    assert!(verify_complete(&artifact, &trust, level).is_err());
}

#[test]
fn swapped_and_orphan_records_are_refused() {
    let native = native();
    let header = header_record(0, native.first_key, 1, 13);
    let first = receipt_artifact(&header, id(1));
    let second = receipt_artifact(&header, id(2));
    let facts = vec![receipt_ref(id(1)), receipt_ref(id(2))];
    let trust = trust();
    let level = VerificationLevel::SEQUENCER_SIGNED;
    // Each reference names the other fact's receipt bytes.
    let swap = |record: &[u8], from: [u8; 32], to: [u8; 32]| -> Vec<u8> {
        let (old, new) = (receipt_ref(from), receipt_ref(to));
        let at = 9;
        assert_eq!(&record[at..at + old.len()], old.as_bytes());
        let mut out = record.to_vec();
        out[at..at + new.len()].copy_from_slice(new.as_bytes());
        out
    };
    let receipts = [
        swap(&first[0], id(1), id(2)),
        swap(&second[0], id(2), id(1)),
    ];
    let artifact = must(CompleteOfflineArtifact::decode(
        &facts,
        &receipts,
        &[first[1].clone(), second[1].clone()],
        &NO_RECORDS,
        &first[2..],
    ));
    assert!(verify_complete(&artifact, &trust, level).is_err());
    let inclusions = [
        swap_inclusion(&first[1], id(1), id(2)),
        swap_inclusion(&second[1], id(2), id(1)),
    ];
    let artifact = must(CompleteOfflineArtifact::decode(
        &facts,
        &[first[0].clone(), second[0].clone()],
        &inclusions,
        &NO_RECORDS,
        &first[2..],
    ));
    assert!(verify_complete(&artifact, &trust, level).is_err());
    // A complete second receipt that no requested fact uses is an orphan.
    let artifact = must(CompleteOfflineArtifact::decode(
        &facts[..1],
        &[first[0].clone(), second[0].clone()],
        &[first[1].clone(), second[1].clone()],
        &NO_RECORDS,
        &first[2..],
    ));
    assert!(verify_complete(&artifact, &trust, level).is_err());
    // A requested fact with no primary record is refused, never served partially.
    let artifact = must(CompleteOfflineArtifact::decode(
        &facts,
        &first[..1],
        &first[1..2],
        &NO_RECORDS,
        &first[2..],
    ));
    assert!(verify_complete(&artifact, &trust, level).is_err());
}

fn swap_inclusion(record: &[u8], from: [u8; 32], to: [u8; 32]) -> Vec<u8> {
    let (old, new) = (receipt_ref(from), receipt_ref(to));
    let at = 10;
    assert_eq!(&record[at..at + old.len()], old.as_bytes());
    let mut out = record.to_vec();
    out[at..at + new.len()].copy_from_slice(new.as_bytes());
    out
}

#[test]
fn state_path_tamper_and_missing_maintenance_link_are_refused() {
    let native = native();
    let trust = trust();
    let activity = id(1);
    let account = id(0x22);
    let header = header_record(0, native.first_key, 1, 13);
    let digest = must(header.digest());
    let signer = ed_key(9);
    let receipt_bytes = receipt(activity, &signer);
    let facts = vec![state_ref(activity, account)];
    let plain = account_state_bytes(&facts[0], 1, &receipt_bytes, digest, None);
    let verify = |proofs: Vec<Vec<u8>>, receipts: Vec<Vec<u8>>| {
        let artifact = must(CompleteOfflineArtifact::decode(
            &facts,
            &receipts,
            &proofs,
            &NO_RECORDS,
            &[header_bytes(&header)],
        ));
        verify_complete(&artifact, &trust, VerificationLevel::STATE_PROVEN)
    };
    let control = verify(vec![plain.clone()], Vec::new());
    assert!(control.is_err());
    let path_at = 9 + facts[0].len() + 1 + 32 + 4 + 48 + 96 + 4 + 10;
    let mut tampered = plain.clone();
    tampered[path_at] ^= 1;
    assert!(verify(vec![tampered], Vec::new()).is_err());
    // Maintenance state without its dependency receipt and inclusion is refused.
    let maintained =
        account_state_bytes(&facts[0], 2, b"maintenance", digest, Some((3, 7, activity)));
    let missing = verify(vec![maintained.clone()], Vec::new());
    assert!(matches!(
        missing,
        Err(CompleteExportError::MissingRecord { fact: 0 }
            | CompleteExportError::MaintenanceLink { fact: 0 })
    ));
    // A dependency receipt for another activity does not complete the link.
    let other = receipt(id(3), &signer);
    let wrong = verify(
        vec![
            maintained,
            inclusion_bytes(2, &receipt_ref(id(3)), &other, &single_proof(), digest),
        ],
        vec![receipt_record_bytes(
            id(3),
            &other,
            signer.verifying_key().to_bytes(),
        )],
    );
    assert!(wrong.is_err());
}

#[test]
fn checkpoint_facts_refuse_wrong_domain_membership_and_incomplete_availability() {
    let native = native();
    let activity = id(1);
    let header = header_record(0, native.first_key, 1, 13);
    let parts = receipt_artifact(&header, activity);
    let reference = checkpoint_ref(activity, 1);
    let facts = vec![reference.clone()];
    let run = |trust: &IndependentOfflineTrust, certificate: Vec<u8>| {
        let artifact = decode_artifact(&facts, &parts, &[certificate]);
        verify_complete(&artifact, trust, VerificationLevel::CHECKPOINT_FINALISED)
    };
    let full = checkpoint_bytes(&reference, &HEADERS[..HEADER_BYTES], &[0, 1, 2], None);
    assert!(run(&trust(), full.clone()).is_err());
    let other = |domain: SettlementDomain, member: TrustedCheckpointMembership| {
        let native = self::native();
        must(IndependentOfflineTrust::new(
            native.registry,
            native.history,
            domain,
            vec![member],
        ))
    };
    for trust in [
        other(
            SettlementDomain::new(31_337, [0x56; 20]),
            membership(&[1, 2, 3], 2),
        ),
        other(
            SettlementDomain::new(31_338, [0x55; 20]),
            membership(&[1, 2, 3], 2),
        ),
        other(domain(), membership(&[1, 2, 4], 2)),
        other(domain(), membership(&[1, 2], 2)),
        other(domain(), membership(&[1, 2, 3], 3)),
    ] {
        assert!(run(&trust, full.clone()).is_err());
    }
    // Missing tail chunk and substituted chunk bytes are refused, never partially served.
    assert!(run(
        &trust(),
        checkpoint_bytes(&reference, &HEADERS[..HEADER_BYTES], &[0, 1], None)
    )
    .is_err());
    let mut substituted = full.clone();
    let data = availability();
    let tail = chunk_bytes(&data.chunks[2], &data.proofs[2]);
    let bytes_at = full.len() - tail.len() + 21 + 4;
    substituted[bytes_at] ^= 1;
    assert!(run(&trust(), substituted).is_err());
    // A certificate for a different batch than the requested reference is refused.
    let next = checkpoint_ref(activity, 2);
    let artifact = decode_artifact(
        &[next.clone()],
        &parts,
        &[checkpoint_bytes(
            &next,
            &HEADERS[..HEADER_BYTES],
            &[0, 1, 2],
            None,
        )],
    );
    assert!(verify_complete(&artifact, &trust(), VerificationLevel::CHECKPOINT_FINALISED).is_err());
}
