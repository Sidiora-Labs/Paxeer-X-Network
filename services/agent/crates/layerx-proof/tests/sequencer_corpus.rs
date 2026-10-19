use std::fmt::{Debug, Write as _};
use std::path::PathBuf;

use ed25519_dalek::{Signer as _, SigningKey};
use layerx_proof::inclusion::SequencerAuthorization;
use layerx_proof::receipt::{verify_sequencer_signature, ReceiptCheck, ReceiptKeys};
use layerx_types::json::decode_hex;
use layerx_wire::encode::Encoder;
use layerx_wire::hash::receipt_digest;
use layerx_wire::limits::{MAX_MESSAGE_BYTES, MAX_SEQUENCER_EFFECTS, PROTOCOL_VERSION};
use layerx_wire::receipt::{decode, encode, encode_unsigned};
use layerx_wire::WireError;
use serde_json::{json, Value};
use sha2::{Digest as _, Sha256};

type TestResult = Result<(), String>;

const FIXTURE: &str = "../../../../sdk/platform/conformance/fixtures/receipt-sequencer-v1.json";
const UPDATE_ENV: &str = "LAYERX_UPDATE_FIXTURES";
const NAME: &str = "receipt-sequencer-v1";
const STRUCTURE_TAG: u16 = 0x5201;
const MODULE_ID: u16 = 12;
const OPERATION: u8 = 2;
const KIND_STATE: u8 = 1;
const SEQUENCE: u64 = 7;
const PREVIOUS_SEQ_HASH: [u8; 32] = [0x04; 32];
const TIMESTAMP_MS: u64 = 1_750_000_000_250;
const FEE_DENOM: &str = "uhpx";
const RETURN_DATA: [u8; 32] = [0; 32];

const PRIMARY_SEED: [u8; 32] = [0x11; 32];
const STANDBY_SEED: [u8; 32] = [0x22; 32];
const OUTSIDE_SEED: [u8; 32] = [0x33; 32];
const PRIMARY_KEY_ID: [u8; 32] = [0xa1; 32];
const STANDBY_KEY_ID: [u8; 32] = [0xb2; 32];
const UNKNOWN_KEY_ID: [u8; 32] = [0xc3; 32];
const PRIMARY_RANGE: (u64, u64) = (1_000, 1_999);
const STANDBY_RANGE: (u64, u64) = (1_500, 2_999);
const IN_RANGE_HEIGHT: u64 = 1_200;
const STANDBY_ONLY_HEIGHT: u64 = 2_500;

/// Signature flag (1 byte), signature length (4 bytes) and signature (64 bytes).
const SIGNATURE_TAIL: usize = 1 + 4 + 64;

fn fail(error: impl Debug) -> String {
    format!("{error:?}")
}

fn sha256(bytes: &[u8]) -> [u8; 32] {
    Sha256::digest(bytes).into()
}

fn hex(bytes: &[u8]) -> String {
    bytes.iter().fold(String::from("0x"), |mut text, byte| {
        let _ = write!(text, "{byte:02x}");
        text
    })
}

fn key(seed: [u8; 32]) -> SigningKey {
    SigningKey::from_bytes(&seed)
}

fn table() -> [SequencerAuthorization; 2] {
    [
        SequencerAuthorization::new(
            PRIMARY_KEY_ID,
            key(PRIMARY_SEED).verifying_key().to_bytes(),
            PRIMARY_RANGE.0,
            PRIMARY_RANGE.1,
        ),
        SequencerAuthorization::new(
            STANDBY_KEY_ID,
            key(STANDBY_SEED).verifying_key().to_bytes(),
            STANDBY_RANGE.0,
            STANDBY_RANGE.1,
        ),
    ]
}

fn summary_body() -> Vec<u8> {
    let mut body = Vec::new();
    body.extend_from_slice(&21_000_u64.to_be_bytes());
    body.extend_from_slice(&21_000_u64.to_be_bytes());
    body.extend_from_slice(&1_000_000_000_u64.to_be_bytes());
    body.extend_from_slice(&1_u32.to_be_bytes());
    body.extend_from_slice(&[0; 20]);
    body.extend_from_slice(&[0; 20]);
    body.extend_from_slice(&sha256(b""));
    body.extend_from_slice(&32_u32.to_be_bytes());
    body.extend_from_slice(&0_u32.to_be_bytes());
    body.push(4);
    body.extend_from_slice(FEE_DENOM.as_bytes());
    body
}

/// The sequencer receipt fields one vector varies.
#[derive(Clone, Copy)]
struct Envelope {
    tx_hash: [u8; 32],
    height: u64,
    key_id: [u8; 32],
}

impl Envelope {
    fn new(id: &str, height: u64, key_id: [u8; 32]) -> Self {
        Self {
            tx_hash: sha256(format!("{NAME}/{id}").as_bytes()),
            height,
            key_id,
        }
    }

    /// Lays out the ProtocolReceipt v2 fields in the canonical order of
    /// `layerx_wire::receipt::encode`.
    fn layout(&self, signature: Option<[u8; 64]>) -> Result<Vec<u8>, WireError> {
        let mut seq_hash_preimage = b"PAXEER/v1/sequence\0".to_vec();
        seq_hash_preimage.extend_from_slice(&PREVIOUS_SEQ_HASH);
        seq_hash_preimage.extend_from_slice(&SEQUENCE.to_be_bytes());
        seq_hash_preimage.extend_from_slice(&self.tx_hash);
        let mut batch_id = [0; 32];
        batch_id[24..].copy_from_slice(&self.height.to_be_bytes());
        let mut from = [0; 32];
        from[12..].copy_from_slice(&[0xaa; 20]);
        let mut to = [0; 32];
        to[12..].copy_from_slice(&[0xbb; 20]);

        let mut encoder = Encoder::new(MAX_MESSAGE_BYTES);
        encoder.structure_header_version(STRUCTURE_TAG, PROTOCOL_VERSION)?;
        encoder.u16(PROTOCOL_VERSION)?;
        encoder.bytes(&self.tx_hash, 32)?;
        encoder.u64(SEQUENCE)?;
        encoder.bytes(&PREVIOUS_SEQ_HASH, 32)?;
        encoder.bytes(&sha256(&seq_hash_preimage), 32)?;
        encoder.bytes(&sha256(&RETURN_DATA), 32)?;
        encoder.i32(0)?;
        encoder.sequence_length(1, MAX_SEQUENCER_EFFECTS)?;
        encoder.u16(MODULE_ID)?;
        encoder.u16(0)?;
        encoder.u16(0)?;
        encoder.tag(KIND_STATE, 3)?;
        encoder.u8(0)?;
        encoder.bytes(&[0; 32], 32)?;
        encoder.bytes(&summary_body(), 256)?;
        encoder.u128(21_000_000_000_000)?;
        encoder.bytes(&batch_id, 32)?;
        encoder.u16(MODULE_ID)?;
        encoder.u32(1)?;
        encoder.u32(0)?;
        encoder.u8(OPERATION)?;
        encoder.bytes(&sha256(FEE_DENOM.as_bytes()), 32)?;
        encoder.u128(1_000_000_000_000_000_000)?;
        encoder.bytes(&from, 32)?;
        encoder.u128(5_000_000_000_000_000_000)?;
        encoder.u128(3_999_979_000_000_000_000)?;
        encoder.u64(41)?;
        encoder.bytes(&to, 32)?;
        encoder.u128(2_000_000_000_000_000_000)?;
        encoder.u128(3_000_000_000_000_000_000)?;
        encoder.bytes(&[0; 32], 32)?;
        encoder.bytes(&[0; 32], 32)?;
        encoder.bytes(&self.key_id, 32)?;
        encoder.u64(TIMESTAMP_MS)?;
        encoder.u8(u8::from(signature.is_some()))?;
        if let Some(signature) = signature {
            encoder.bytes(&signature, 64)?;
        }
        Ok(encoder.finish())
    }

    /// Returns the receipt bytes the real wire encoder produces, failing if the
    /// layout is not byte-identical to them.
    fn encoded(&self, signature: Option<[u8; 64]>) -> Result<Vec<u8>, String> {
        let bytes = self.layout(signature).map_err(fail)?;
        let reencoded = encode(&decode(&bytes).map_err(fail)?).map_err(fail)?;
        if reencoded != bytes {
            return Err("the layout differs from the wire encoder".to_owned());
        }
        Ok(reencoded)
    }

    fn signature(&self, signer: &SigningKey) -> Result<[u8; 64], String> {
        let unsigned = self.encoded(None)?;
        let preimage = encode_unsigned(&decode(&unsigned).map_err(fail)?).map_err(fail)?;
        if preimage != unsigned {
            return Err("the unsigned preimage differs from the wire encoder".to_owned());
        }
        let digest = receipt_digest(&preimage).map_err(fail)?;
        Ok(signer.sign(&digest).to_bytes())
    }
}

struct Vector {
    id: &'static str,
    envelope: Envelope,
    receipt: Vec<u8>,
    signature: Option<[u8; 64]>,
    expected: Result<(), ReceiptCheck>,
}

impl Vector {
    fn signed(
        id: &'static str,
        envelope: Envelope,
        signer: &SigningKey,
        expected: Result<(), ReceiptCheck>,
    ) -> Result<Self, String> {
        let signature = envelope.signature(signer)?;
        Ok(Self {
            id,
            envelope,
            receipt: envelope.encoded(Some(signature))?,
            signature: Some(signature),
            expected,
        })
    }

    fn expected_json(&self) -> Value {
        match self.expected {
            Ok(()) => json!({ "rung": "receipted", "verifier": "accepts" }),
            Err(check) => json!({ "verifier": format!("{check:?}") }),
        }
    }

    fn to_json(&self, keys: &[SequencerAuthorization]) -> Value {
        json!({
            "expected": self.expected_json(),
            "height": self.envelope.height,
            "id": self.id,
            "keyId": hex(&self.envelope.key_id),
            "receipt": hex(&self.receipt),
            "receiptKeys": keys.iter().map(key_json).collect::<Vec<_>>(),
            "signature": self.signature.map(|signature| hex(&signature)),
            "txHash": hex(&self.envelope.tx_hash),
        })
    }
}

fn key_json(key: &SequencerAuthorization) -> Value {
    json!({
        "firstHeight": key.first_batch_number(),
        "keyId": hex(&key.sequencer_id()),
        "lastHeight": key.last_batch_number(),
        "publicKey": hex(&key.public_key()),
    })
}

fn corpus() -> Result<Vec<Vector>, String> {
    let primary = key(PRIMARY_SEED);
    let outside = key(OUTSIDE_SEED);
    let valid = Vector::signed(
        "receipted-primary-key",
        Envelope::new("receipted-primary-key", IN_RANGE_HEIGHT, PRIMARY_KEY_ID),
        &primary,
        Ok(()),
    )?;

    let outside_signer = Vector::signed(
        "signature-key-outside-table",
        Envelope::new(
            "signature-key-outside-table",
            IN_RANGE_HEIGHT,
            PRIMARY_KEY_ID,
        ),
        &outside,
        Err(ReceiptCheck::SequencerSignature),
    )?;
    let outside_range = Vector::signed(
        "height-outside-key-range",
        Envelope::new(
            "height-outside-key-range",
            STANDBY_ONLY_HEIGHT,
            PRIMARY_KEY_ID,
        ),
        &primary,
        Err(ReceiptCheck::SequencerSignature),
    )?;
    let unknown_key = Vector::signed(
        "unknown-key-id",
        Envelope::new("unknown-key-id", IN_RANGE_HEIGHT, UNKNOWN_KEY_ID),
        &outside,
        Err(ReceiptCheck::SequencerSignature),
    )?;

    let timestamp_tail = valid
        .receipt
        .len()
        .checked_sub(SIGNATURE_TAIL + 1)
        .ok_or("receipt shorter than its signature tail")?;
    let mut mutated_bytes = valid.receipt.clone();
    mutated_bytes[timestamp_tail] ^= 0x01;
    let mutated = Vector {
        id: "mutated-receipt-byte",
        envelope: valid.envelope,
        receipt: mutated_bytes,
        signature: valid.signature,
        expected: Err(ReceiptCheck::SequencerSignature),
    };

    let flag = valid
        .receipt
        .len()
        .checked_sub(SIGNATURE_TAIL)
        .ok_or("receipt shorter than its signature tail")?;
    if valid.receipt[flag] != 1 {
        return Err(format!("signature flag is {}", valid.receipt[flag]));
    }
    let mut non_canonical_bytes = valid.receipt.clone();
    non_canonical_bytes[flag] = 2;
    let non_canonical = Vector {
        id: "non-canonical-encoding",
        envelope: valid.envelope,
        receipt: non_canonical_bytes,
        signature: valid.signature,
        expected: Err(ReceiptCheck::Decode),
    };

    let missing = Vector {
        id: "missing-signature",
        envelope: valid.envelope,
        receipt: valid.envelope.encoded(None)?,
        signature: None,
        expected: Err(ReceiptCheck::MissingSignature),
    };

    Ok(vec![
        valid,
        outside_signer,
        outside_range,
        unknown_key,
        mutated,
        non_canonical,
        missing,
    ])
}

fn document(vectors: &[Vector]) -> Result<String, String> {
    let keys = table();
    let value = json!({
        "moduleId": MODULE_ID,
        "name": NAME,
        "provenance": {
            "command": format!("{UPDATE_ENV}=1 cargo test --locked --manifest-path services/agent/Cargo.toml -p layerx-proof --test sequencer_corpus"),
            "generator": "services/agent/crates/layerx-proof/tests/sequencer_corpus.rs",
        },
        "vectors": vectors.iter().map(|vector| vector.to_json(&keys)).collect::<Vec<_>>(),
    });
    let mut text = serde_json::to_string_pretty(&value).map_err(fail)?;
    text.push('\n');
    Ok(text)
}

fn fixture_path() -> PathBuf {
    PathBuf::from(env!("CARGO_MANIFEST_DIR")).join(FIXTURE)
}

fn outcome(receipt: &[u8], keys: &[SequencerAuthorization]) -> Result<(), ReceiptCheck> {
    verify_sequencer_signature(receipt, ReceiptKeys::HeightRanges(keys))
        .map(|_| ())
        .map_err(|failure| failure.check)
}

fn field<'a>(value: &'a Value, name: &str) -> Result<&'a Value, String> {
    value.get(name).ok_or_else(|| format!("missing {name}"))
}

fn text<'a>(value: &'a Value, name: &str) -> Result<&'a str, String> {
    field(value, name)?
        .as_str()
        .ok_or_else(|| format!("{name} is not a string"))
}

fn integer(value: &Value, name: &str) -> Result<u64, String> {
    field(value, name)?
        .as_u64()
        .ok_or_else(|| format!("{name} is not an unsigned integer"))
}

fn bytes(value: &Value, name: &str) -> Result<Vec<u8>, String> {
    decode_hex(text(value, name)?).map_err(|error| format!("{name}: {error}"))
}

fn array32(value: &Value, name: &str) -> Result<[u8; 32], String> {
    bytes(value, name)?
        .try_into()
        .map_err(|_| format!("{name} is not 32 bytes"))
}

fn keys_from(vector: &Value) -> Result<Vec<SequencerAuthorization>, String> {
    field(vector, "receiptKeys")?
        .as_array()
        .ok_or("receiptKeys is not an array")?
        .iter()
        .map(|key| {
            Ok(SequencerAuthorization::new(
                array32(key, "keyId")?,
                array32(key, "publicKey")?,
                integer(key, "firstHeight")?,
                integer(key, "lastHeight")?,
            ))
        })
        .collect()
}

#[test]
fn every_vector_meets_its_labelled_outcome() -> TestResult {
    let keys = table();
    let vectors = corpus()?;
    let ids: Vec<_> = vectors.iter().map(|vector| vector.id).collect();
    assert_eq!(
        ids,
        [
            "receipted-primary-key",
            "signature-key-outside-table",
            "height-outside-key-range",
            "unknown-key-id",
            "mutated-receipt-byte",
            "non-canonical-encoding",
            "missing-signature",
        ]
    );
    for vector in &vectors {
        assert_eq!(
            outcome(&vector.receipt, &keys),
            vector.expected,
            "{}",
            vector.id
        );
    }

    let valid = &vectors[0];
    let receipt = verify_sequencer_signature(&valid.receipt, ReceiptKeys::HeightRanges(&keys))
        .map_err(fail)?;
    let protocol = receipt.protocol().ok_or("not a protocol receipt")?;
    let mut batch_id = [0; 32];
    batch_id[24..].copy_from_slice(&IN_RANGE_HEIGHT.to_be_bytes());
    assert_eq!(protocol.protocol_version(), PROTOCOL_VERSION);
    assert_eq!(protocol.activity_id(), valid.envelope.tx_hash);
    assert_eq!(protocol.context_hash(), PRIMARY_KEY_ID);
    assert_eq!(protocol.batch_id(), batch_id);
    assert_eq!(protocol.module_id(), MODULE_ID);
    assert_eq!(protocol.sequencer_signature(), valid.signature);

    let standby_signed = Vector::signed(
        "standby-overlap",
        Envelope::new("standby-overlap", STANDBY_ONLY_HEIGHT, STANDBY_KEY_ID),
        &key(STANDBY_SEED),
        Ok(()),
    )?;
    assert_eq!(outcome(&standby_signed.receipt, &keys), Ok(()));

    let mutated = decode(&vectors[4].receipt).map_err(fail)?;
    let mutated = mutated.protocol().ok_or("not a protocol receipt")?;
    assert_eq!(mutated.timestamp(), TIMESTAMP_MS ^ 1);
    assert_eq!(encode(&receipt).map_err(fail)?, valid.receipt);
    Ok(())
}

#[test]
fn checked_in_corpus_equals_the_generated_bytes() -> TestResult {
    let generated = document(&corpus()?)?;
    let path = fixture_path();
    if std::env::var_os(UPDATE_ENV).is_some() {
        std::fs::write(&path, &generated)
            .map_err(|error| format!("{}: {error}", path.display()))?;
    }
    let checked_in =
        std::fs::read_to_string(&path).map_err(|error| format!("{}: {error}", path.display()))?;
    assert!(
        checked_in == generated,
        "{} differs from the generated corpus; regenerate with {UPDATE_ENV}=1",
        path.display()
    );
    Ok(())
}

#[test]
fn checked_in_vectors_verify_as_labelled() -> TestResult {
    let path = fixture_path();
    let text_value =
        std::fs::read_to_string(&path).map_err(|error| format!("{}: {error}", path.display()))?;
    let document: Value = serde_json::from_str(&text_value).map_err(fail)?;
    assert_eq!(text(&document, "name")?, NAME);
    assert_eq!(integer(&document, "moduleId")?, u64::from(MODULE_ID));
    let vectors = field(&document, "vectors")?
        .as_array()
        .ok_or("vectors is not an array")?;
    assert_eq!(vectors.len(), 7);
    let mut accepted = 0;
    for vector in vectors {
        let id = text(vector, "id")?;
        let keys = keys_from(vector)?;
        assert_eq!(keys, table(), "{id}");
        let receipt = bytes(vector, "receipt")?;
        let expected = field(vector, "expected")?;
        let verifier = text(expected, "verifier")?;
        match outcome(&receipt, &keys) {
            Ok(()) => {
                assert_eq!(verifier, "accepts", "{id}");
                assert_eq!(text(expected, "rung")?, "receipted", "{id}");
                let decoded = decode(&receipt).map_err(fail)?;
                let protocol = decoded.protocol().ok_or("not a protocol receipt")?;
                assert_eq!(protocol.activity_id(), array32(vector, "txHash")?, "{id}");
                assert_eq!(protocol.context_hash(), array32(vector, "keyId")?, "{id}");
                assert_eq!(
                    protocol.batch_id()[24..],
                    integer(vector, "height")?.to_be_bytes(),
                    "{id}"
                );
                assert_eq!(
                    protocol
                        .sequencer_signature()
                        .map(|signature| hex(&signature)),
                    field(vector, "signature")?.as_str().map(str::to_owned),
                    "{id}"
                );
                accepted += 1;
            }
            Err(check) => {
                assert_eq!(verifier, format!("{check:?}"), "{id}");
                assert!(expected.get("rung").is_none(), "{id}");
            }
        }
    }
    assert_eq!(accepted, 1);
    Ok(())
}
