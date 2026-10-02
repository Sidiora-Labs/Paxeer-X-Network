use ed25519_dalek::{Signer as _, SigningKey};
use layerx_agent_api::export::{FactRef, OfflineExport};
use layerx_agent_api::prepare::CanonicalBytes;
use layerx_proof::checkpoint::{GuarantorKey, SettlementDomain};
use layerx_proof::export::{
    verify_complete, CompleteExportError, IndependentOfflineTrust, TrustedCheckpointMembership,
};
use layerx_proof::export_codec::{CompleteOfflineArtifact, FactSelector};
use layerx_proof::merkle::{encode_proof, Proof};
use layerx_proof::signed_authority::SignedAuthorityHistory;
use layerx_sdk::rpc_export::decode_offline_export;
use layerx_types::verify::VerificationLevel;
use layerx_wire::activity::decode_signed;
use layerx_wire::encode::Encoder;
use layerx_wire::handover::{decode_genesis_trust, sequencer_id};
use layerx_wire::hash::{batch_header_digest, receipt_digest};
use layerx_wire::limits::PROTOCOL_VERSION;

const GENESIS: &[u8] = include_bytes!("../../layerx-proof/tests/fixtures/signed-authority/genesis.bin");
const HEADERS: &[u8] = include_bytes!("../../layerx-proof/tests/fixtures/signed-authority/headers.bin");
const HANDOVER: &[u8] =
    include_bytes!("../../layerx-proof/tests/fixtures/signed-authority/handover.activity");
const HEADER_RECORD: usize = 418;
const HEADER_BYTES: usize = 354;
// Compressed secp256k1 public keys for the scalars 1, 2 and 3.
const GUARANTOR_KEYS: [&str; 3] = [
    "0279be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f81798",
    "02c6047f9441ed7d6d3045406e95c07cd85c778e4b8cef3ca7abac09b95c709ee5",
    "02f9308a019258c31049344f85f89d5229b531c845836f99b08601f113bce036f9",
];

fn must<T, E: std::fmt::Debug>(value: Result<T, E>) -> T {
    value.unwrap_or_else(|error| panic!("sdk offline export: {error:?}"))
}

fn hex(bytes: &[u8]) -> String {
    bytes.iter().map(|byte| format!("{byte:02x}")).collect()
}

fn unhex33(text: &str) -> [u8; 33] {
    let mut out = [0_u8; 33];
    for (index, slot) in out.iter_mut().enumerate() {
        *slot = must(u8::from_str_radix(&text[index * 2..index * 2 + 2], 16));
    }
    out
}

fn id(value: u8) -> [u8; 32] {
    let mut out = [value; 32];
    out[0] = 0xa0 | (value & 0x0f);
    out
}

fn lp(out: &mut Vec<u8>, bytes: &[u8]) {
    out.extend_from_slice(&must(u32::try_from(bytes.len())).to_be_bytes());
    out.extend_from_slice(bytes);
}

fn receipt(activity: [u8; 32], signer: &SigningKey) -> (Vec<u8>, [u8; 32]) {
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
    (encode(Some(signer.sign(&digest).to_bytes())), digest)
}

fn trust() -> (IndependentOfflineTrust, [u8; 32]) {
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
    let keys = GUARANTOR_KEYS
        .iter()
        .enumerate()
        .map(|(index, key)| {
            let mut guarantor_id = [0_u8; 32];
            guarantor_id[0] = must(u8::try_from(index + 1));
            GuarantorKey::new(guarantor_id, unhex33(key), true)
        })
        .collect();
    let trust = must(IndependentOfflineTrust::new(
        genesis.registry,
        history,
        SettlementDomain::new(31_337, [0x55; 20]),
        vec![must(TrustedCheckpointMembership::new(1, keys, 2))],
    ));
    (trust, genesis.initial_sequencer_key)
}

struct Buckets {
    facts: Vec<String>,
    receipts: Vec<Vec<u8>>,
    proofs: Vec<Vec<u8>>,
    headers: Vec<Vec<u8>>,
}

// One receipt fact bound to the first real signed header in the frozen v1 layouts.
fn buckets(sequencer_key: [u8; 32], activity: [u8; 32]) -> Buckets {
    let signer = SigningKey::from_bytes(&[9; 32]);
    let (receipt_bytes, receipt_hash) = receipt(activity, &signer);
    let reference = format!("receipt:{}", hex(&activity));
    let header = &HEADERS[..HEADER_BYTES];
    let mut header_record = b"LXHD".to_vec();
    header_record.push(1);
    lp(&mut header_record, header);
    header_record.extend_from_slice(&HEADERS[HEADER_BYTES..HEADER_RECORD]);
    header_record.extend_from_slice(&must(sequencer_id(&sequencer_key)));
    header_record.extend_from_slice(&sequencer_key);
    header_record.extend_from_slice(&1_u64.to_be_bytes());
    header_record.extend_from_slice(&13_u64.to_be_bytes());
    let mut receipt_record = b"LXRF".to_vec();
    receipt_record.push(1);
    lp(&mut receipt_record, reference.as_bytes());
    lp(&mut receipt_record, &receipt_bytes);
    for value in [[4; 32], [5; 32], [2; 32], [3; 32]] {
        receipt_record.extend_from_slice(&value);
    }
    receipt_record.extend_from_slice(&signer.verifying_key().to_bytes());
    receipt_record.extend_from_slice(&receipt_hash);
    let mut inclusion = b"LXIP".to_vec();
    inclusion.push(1);
    inclusion.push(2);
    lp(&mut inclusion, reference.as_bytes());
    lp(&mut inclusion, &receipt_bytes);
    lp(&mut inclusion, &encode_proof(&must(Proof::new(0, 1, Vec::new()))));
    inclusion.extend_from_slice(&must(batch_header_digest(header)));
    Buckets {
        facts: vec![reference],
        receipts: vec![receipt_record],
        proofs: vec![inclusion],
        headers: vec![header_record],
    }
}

fn api(buckets: &Buckets) -> OfflineExport {
    let bytes = |items: &[Vec<u8>]| -> Vec<CanonicalBytes> {
        items
            .iter()
            .map(|item| must(CanonicalBytes::new(item.clone())))
            .collect()
    };
    OfflineExport {
        facts: buckets
            .facts
            .iter()
            .map(|fact| must(FactRef::new(fact.clone())))
            .collect(),
        receipts: bytes(&buckets.receipts),
        proofs: bytes(&buckets.proofs),
        certificates: Vec::new(),
        headers: bytes(&buckets.headers),
    }
}

#[test]
fn api_export_decodes_through_the_one_shared_codec() {
    let (trust, key) = trust();
    let activity = id(1);
    let parts = buckets(key, activity);
    let no_certificates: [Vec<u8>; 0] = [];
    let Ok(decoded) = decode_offline_export(&api(&parts), &trust) else {
        panic!("canonical api export refused");
    };
    let direct = must(CompleteOfflineArtifact::decode(
        &parts.facts,
        &parts.receipts,
        &parts.proofs,
        &no_certificates,
        &parts.headers,
    ));
    assert_eq!(decoded.facts, vec![FactSelector::Receipt { activity_id: activity }]);
    assert_eq!(decoded.facts, direct.facts);
    let encoded = must(decoded.encode());
    assert_eq!(encoded.facts, parts.facts);
    assert_eq!(encoded.receipts, parts.receipts);
    assert_eq!(encoded.proofs, parts.proofs);
    assert_eq!(encoded.headers, parts.headers);
    assert!(encoded.certificates.is_empty());
}

#[test]
fn api_fact_refs_are_validated_at_the_export_boundary() {
    let (trust, key) = trust();
    let activity = id(1);
    let canonical = buckets(key, activity);
    let good = hex(&activity);
    for fact in [
        format!("receipt:{}", good.to_uppercase()),
        format!("receipt:0x{}", &good[2..]),
        format!("receipt:{good} "),
        format!("receipt:{}", hex(&[0; 32])),
        format!("ledger:{good}"),
        format!("checkpoint:{good}:01"),
        format!("checkpoint:{good}:18446744073709551616"),
    ] {
        let mut parts = buckets(key, activity);
        // The compatibility newtype still accepts any non-empty text; the SDK must not.
        assert!(FactRef::new(fact.clone()).is_ok());
        parts.facts = vec![fact.clone()];
        assert!(
            decode_offline_export(&api(&parts), &trust).is_err(),
            "accepted {fact:?}"
        );
    }
    let mut repeated = buckets(key, activity);
    repeated.facts = vec![canonical.facts[0].clone(), canonical.facts[0].clone()];
    assert!(decode_offline_export(&api(&repeated), &trust).is_err());
    let mut seventeen = buckets(key, activity);
    seventeen.facts = (1..=17)
        .map(|value| format!("receipt:{}", hex(&id(value))))
        .collect();
    assert!(decode_offline_export(&api(&seventeen), &trust).is_err());
}

#[test]
fn api_buckets_refuse_trailing_truncated_or_misplaced_records() {
    let (trust, key) = trust();
    let activity = id(1);
    let mut trailing = buckets(key, activity);
    trailing.headers[0].push(0);
    assert!(decode_offline_export(&api(&trailing), &trust).is_err());
    let mut truncated = buckets(key, activity);
    truncated.receipts[0].pop();
    assert!(decode_offline_export(&api(&truncated), &trust).is_err());
    let mut version = buckets(key, activity);
    version.proofs[0][4] = 2;
    assert!(decode_offline_export(&api(&version), &trust).is_err());
    let mut misplaced = buckets(key, activity);
    std::mem::swap(&mut misplaced.receipts, &mut misplaced.headers);
    assert!(decode_offline_export(&api(&misplaced), &trust).is_err());
    let mut certificate = buckets(key, activity);
    let header = certificate.headers[0].clone();
    let mut export = api(&certificate);
    export.certificates = vec![must(CanonicalBytes::new(header))];
    certificate.headers.clear();
    assert!(decode_offline_export(&export, &trust).is_err());
}

#[test]
fn decoded_export_never_claims_settlement_anchoring_or_a_rogue_sequencer() {
    let (trust, key) = trust();
    let activity = id(1);
    let Ok(decoded) = decode_offline_export(&api(&buckets(key, activity)), &trust) else {
        panic!("canonical api export refused");
    };
    assert_eq!(
        verify_complete(&decoded, &trust, VerificationLevel::SETTLEMENT_ANCHORED).err(),
        Some(CompleteExportError::SettlementAnchoringUnavailable)
    );
    let rogue = SigningKey::from_bytes(&[0x77; 32]).verifying_key().to_bytes();
    let Ok(forged) = decode_offline_export(&api(&buckets(rogue, activity)), &trust) else {
        panic!("rogue key must be refused by verification, not by the codec");
    };
    assert!(matches!(
        verify_complete(&forged, &trust, VerificationLevel::BATCH_INCLUDED),
        Err(CompleteExportError::Header { fact: 0 })
    ));
}
