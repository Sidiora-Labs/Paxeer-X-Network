use std::path::Path;

use k256::ecdsa::signature::hazmat::PrehashVerifier;
use k256::ecdsa::{Signature, VerifyingKey};
use layerx_oracle_feeder::cosmos_tx::{
    compressed_public_key, encode_tx, feed_account, sign_doc, Any, AuthInfo, MsgSubmitPrices,
    Observation, SignDoc, SignerInfo, Submission, TxBody, MSG_SUBMIT_PRICES_TYPE_URL,
};
use layerx_oracle_feeder::hex_decode;
use serde_json::Value;
use sha2::{Digest, Sha256};

type Result = std::result::Result<(), String>;

const HALF_ORDER: [u8; 32] = [
    0x7f, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
    0x5d, 0x57, 0x6e, 0x73, 0x57, 0xa4, 0x50, 0x1d, 0xdf, 0xe9, 0x2f, 0x46, 0x68, 0x1b, 0x20, 0xa0,
];

fn fixture(name: &str) -> std::result::Result<Value, String> {
    let path = Path::new(env!("CARGO_MANIFEST_DIR"))
        .join("tests/fixtures")
        .join(name);
    let text = std::fs::read_to_string(&path).map_err(|e| e.to_string())?;
    serde_json::from_str(&text).map_err(|e| e.to_string())
}

fn text<'a>(value: &'a Value, field: &str) -> std::result::Result<&'a str, String> {
    value[field]
        .as_str()
        .ok_or_else(|| format!("vector field {field} is not a string"))
}

fn number(value: &Value, field: &str) -> std::result::Result<u64, String> {
    value[field]
        .as_u64()
        .ok_or_else(|| format!("vector field {field} is not an unsigned integer"))
}

fn bytes<const N: usize>(value: &Value, field: &str) -> std::result::Result<[u8; N], String> {
    hex_decode(text(value, field)?)?
        .try_into()
        .map_err(|_| format!("vector field {field} is not {N} bytes"))
}

struct Vector {
    secret: [u8; 32],
    public_key: [u8; 33],
    feeder: String,
    submission: Submission,
    sign_doc: Vec<u8>,
    signature: [u8; 64],
    tx: Vec<u8>,
}

fn vector() -> std::result::Result<Vector, String> {
    let value = fixture("cosmos_tx_vector.json")?;
    let observations = value["observations"]
        .as_array()
        .ok_or_else(|| "vector observations is not an array".to_owned())?
        .iter()
        .map(|observation| {
            Ok(Observation {
                market_id: number(observation, "market_id")?,
                sequence: number(observation, "sequence")?,
                index_price: text(observation, "index_price")?.to_owned(),
                mark_price: text(observation, "mark_price")?.to_owned(),
                source_timestamp_ms: number(observation, "source_timestamp_ms")?,
            })
        })
        .collect::<std::result::Result<Vec<_>, String>>()?;
    Ok(Vector {
        secret: bytes(&value, "secret_key_hex")?,
        public_key: bytes(&value, "public_key_hex")?,
        feeder: text(&value, "feeder")?.to_owned(),
        submission: Submission {
            chain_id: text(&value, "chain_id")?.to_owned(),
            account_number: number(&value, "account_number")?,
            sequence: number(&value, "sequence")?,
            gas_limit: number(&value, "gas_limit")?,
            feed_account: bytes(&value, "feed_account_hex")?,
            observations,
        },
        sign_doc: hex_decode(text(&value, "sign_doc_hex")?)?,
        signature: bytes(&value, "signature_hex")?,
        tx: hex_decode(text(&value, "tx_hex")?)?,
    })
}

fn vector_sign_doc(vector: &Vector) -> std::result::Result<SignDoc, String> {
    let submission = &vector.submission;
    let message = MsgSubmitPrices {
        feeder: feed_account(&submission.feed_account),
        observations: submission.observations.clone(),
    };
    let body = TxBody {
        messages: vec![Any {
            type_url: MSG_SUBMIT_PRICES_TYPE_URL.to_owned(),
            value: message.encode(),
        }],
    };
    let auth_info = AuthInfo {
        signer_info: SignerInfo {
            public_key: compressed_public_key(&vector.secret)?,
            sequence: submission.sequence,
        },
        gas_limit: submission.gas_limit,
    };
    Ok(SignDoc {
        body_bytes: body.encode(),
        auth_info_bytes: auth_info.encode(),
        chain_id: submission.chain_id.clone(),
        account_number: submission.account_number,
    })
}

#[test]
fn tx_matches_vector() -> Result {
    let vector = vector()?;
    let submission = &vector.submission;
    assert_eq!(vector.secret, [1; 32]);
    assert_eq!(submission.chain_id, "test-1");
    assert_eq!(submission.account_number, 9);
    assert_eq!(submission.sequence, 3);
    assert_eq!(submission.observations.len(), 2);
    assert_eq!(feed_account(&submission.feed_account), vector.feeder);
    assert_eq!(compressed_public_key(&vector.secret)?, vector.public_key);
    assert_eq!(vector_sign_doc(&vector)?.encode(), vector.sign_doc);
    assert_eq!(encode_tx(&vector.secret, submission)?, vector.tx);
    Ok(())
}

#[test]
fn signature_is_low_s_and_verifies() -> Result {
    let vector = vector()?;
    let doc = vector_sign_doc(&vector)?;
    assert_eq!(doc.encode(), vector.sign_doc);
    let signature = sign_doc(&vector.secret, &doc)?;
    assert_eq!(signature, vector.signature);
    assert!(vector.tx.ends_with(&signature));
    assert!(signature[32..] <= HALF_ORDER[..]);

    let verifying =
        VerifyingKey::from_sec1_bytes(&vector.public_key).map_err(|error| error.to_string())?;
    let parsed = Signature::from_slice(&signature).map_err(|error| error.to_string())?;
    assert!(parsed.normalize_s().is_none());
    verifying
        .verify_prehash(&Sha256::digest(&vector.sign_doc), &parsed)
        .map_err(|error| error.to_string())?;

    let other_chain = SignDoc {
        chain_id: "test-2".to_owned(),
        ..doc
    };
    assert!(verifying
        .verify_prehash(&Sha256::digest(other_chain.encode()), &parsed)
        .is_err());
    Ok(())
}
