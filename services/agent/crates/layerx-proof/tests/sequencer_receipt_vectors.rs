use std::path::PathBuf;

use layerx_proof::inclusion::SequencerAuthorization;
use layerx_proof::receipt::{
    verify_sequencer_signature, ReceiptCheck, ReceiptKeys, VerificationFailure,
};
use layerx_types::json::{parse, JsonValue};
use layerx_wire::hash::receipt_digest;
use layerx_wire::receipt::{encode, encode_unsigned, ProtocolReceipt, Receipt};
use sha2::{Digest as _, Sha256};

const SEQUENCE_DOMAIN: &[u8] = b"PAXEER/v1/sequence\0";
const REVERT_SELECTOR: [u8; 4] = [0x08, 0xc3, 0x79, 0xa0];
const STATE_KIND: u8 = 1;
const EVENT_KIND: u8 = 3;

fn fixture() -> JsonValue {
    let path = PathBuf::from(env!("CARGO_MANIFEST_DIR"))
        .join("../../../../platform/sdk/conformance/fixtures/sequencer-receipt-v1.json");
    let text = std::fs::read_to_string(&path)
        .unwrap_or_else(|error| panic!("{}: {error}", path.display()));
    parse(&text).unwrap_or_else(|error| panic!("{}: {error}", path.display()))
}

fn cases(document: &JsonValue) -> &[JsonValue] {
    document
        .array_at("cases")
        .unwrap_or_else(|error| panic!("{error}"))
}

fn hex(value: &JsonValue, path: &str) -> Vec<u8> {
    value
        .hex_at(path)
        .unwrap_or_else(|error| panic!("{path}: {error}"))
}

fn array<const N: usize>(value: &JsonValue, path: &str) -> [u8; N] {
    value
        .hex_array_at(path)
        .unwrap_or_else(|error| panic!("{path}: {error}"))
}

fn text<'a>(value: &'a JsonValue, path: &str) -> &'a str {
    value
        .str_at(path)
        .unwrap_or_else(|error| panic!("{path}: {error}"))
}

fn integer(value: &JsonValue, path: &str) -> u64 {
    value
        .u64_at(path)
        .unwrap_or_else(|error| panic!("{path}: {error}"))
}

fn quantity(value: &JsonValue, path: &str) -> u128 {
    let digits = text(value, path)
        .strip_prefix("0x")
        .unwrap_or_else(|| panic!("{path} is not a 0x quantity"));
    u128::from_str_radix(digits, 16).unwrap_or_else(|error| panic!("{path}: {error}"))
}

fn quantity_u64(value: &JsonValue, path: &str) -> u64 {
    u64::try_from(quantity(value, path)).unwrap_or_else(|error| panic!("{path}: {error}"))
}

fn sha256(bytes: &[u8]) -> [u8; 32] {
    Sha256::digest(bytes).into()
}

fn addr32(address: &[u8; 20]) -> [u8; 32] {
    let mut word = [0; 32];
    word[12..].copy_from_slice(address);
    word
}

fn u32_len(length: usize) -> [u8; 4] {
    u32::try_from(length)
        .unwrap_or_else(|error| panic!("{error}"))
        .to_be_bytes()
}

fn id(case: &JsonValue) -> &str {
    text(case, "id")
}

fn pin(case: &JsonValue) -> SequencerAuthorization {
    SequencerAuthorization::new(
        array(case, "pinnedKey.keyId"),
        array(case, "pinnedKey.publicKey"),
        integer(case, "pinnedKey.firstHeight"),
        integer(case, "pinnedKey.lastHeight"),
    )
}

fn verified(case: &JsonValue) -> Result<Receipt, VerificationFailure> {
    let pins = [pin(case)];
    verify_sequencer_signature(
        &hex(case, "signedReceipt"),
        ReceiptKeys::HeightRanges(&pins),
    )
}

fn protocol<'a>(receipt: &'a Receipt, case: &JsonValue) -> &'a ProtocolReceipt {
    receipt
        .protocol()
        .unwrap_or_else(|| panic!("{} is not a protocol receipt", id(case)))
}

fn seq_hash(previous: &[u8; 32], sequence: u64, activity: &[u8; 32]) -> [u8; 32] {
    sha256(&[SEQUENCE_DOMAIN, previous, &sequence.to_be_bytes(), activity].concat())
}

fn revert_reason(data: &[u8]) -> String {
    assert_eq!(data.get(..4), Some(REVERT_SELECTOR.as_slice()));
    let word = |index: usize| -> usize {
        let start = 4 + index * 32;
        let bytes = data
            .get(start..start + 32)
            .unwrap_or_else(|| panic!("revert word {index}"));
        assert!(bytes[..24].iter().all(|byte| *byte == 0));
        let tail: [u8; 8] = bytes[24..]
            .try_into()
            .unwrap_or_else(|_| panic!("revert word {index}"));
        usize::try_from(u64::from_be_bytes(tail)).unwrap_or_else(|error| panic!("{error}"))
    };
    assert_eq!(word(0), 32);
    let length = word(1);
    let reason = data
        .get(68..68 + length)
        .unwrap_or_else(|| panic!("revert reason of {length} bytes"));
    String::from_utf8(reason.to_vec()).unwrap_or_else(|error| panic!("{error}"))
}

fn summary_body(json: &JsonValue) -> Vec<u8> {
    let denom = text(json, "feeDenom");
    let logs = json
        .array_at("logs")
        .unwrap_or_else(|error| panic!("{error}"));
    [
        quantity_u64(json, "gasUsed").to_be_bytes().as_slice(),
        &quantity_u64(json, "cumulativeGasUsed").to_be_bytes(),
        &quantity_u64(json, "effectiveGasPrice").to_be_bytes(),
        &u32::try_from(quantity(json, "index"))
            .unwrap_or_else(|error| panic!("{error}"))
            .to_be_bytes(),
        &array::<20>(json, "contractAddress"),
        &array::<20>(json, "sponsor"),
        &sha256(text(json, "vmError").as_bytes()),
        &u32_len(hex(json, "returnData").len()),
        &u32_len(logs.len()),
        &[u8::try_from(denom.len()).unwrap_or_else(|error| panic!("{error}"))],
        denom.as_bytes(),
    ]
    .concat()
}

fn log_body(log: &JsonValue) -> Vec<u8> {
    let topics = log
        .array_at("topics")
        .unwrap_or_else(|error| panic!("{error}"));
    let mut body = array::<20>(log, "address").to_vec();
    body.push(u8::try_from(topics.len()).unwrap_or_else(|error| panic!("{error}")));
    for topic in topics {
        let JsonValue::String(topic) = topic else {
            panic!("topic is not a string");
        };
        let topic = layerx_types::json::decode_hex(topic).unwrap_or_else(|error| panic!("{error}"));
        assert_eq!(topic.len(), 32);
        body.extend_from_slice(&topic);
    }
    body.extend_from_slice(&sha256(&hex(log, "data")));
    body
}

fn assert_envelope(document: &JsonValue, case: &JsonValue, receipt: &ProtocolReceipt) {
    let json = case
        .path("receiptJson")
        .unwrap_or_else(|error| panic!("{error}"));
    let activity: [u8; 32] = array(json, "txHash");
    let previous: [u8; 32] = array(case, "prevSeqHash");
    let sequence = quantity_u64(json, "seq");
    let mut batch_id = [0; 32];
    batch_id[24..].copy_from_slice(&quantity_u64(json, "height").to_be_bytes());
    assert_eq!(receipt.protocol_version(), 2);
    assert_eq!(
        u64::from(receipt.module_id()),
        integer(document, "moduleId")
    );
    assert_eq!(receipt.module_version(), 1);
    assert_eq!(receipt.parameter_version(), 0);
    assert_eq!(receipt.operation(), 2);
    assert_eq!(receipt.activity_id(), activity);
    assert_eq!(receipt.global_sequence(), sequence);
    assert_eq!(receipt.previous_state_root(), previous);
    assert_eq!(
        receipt.resulting_state_root(),
        seq_hash(&previous, sequence, &activity)
    );
    assert_eq!(receipt.batch_id(), batch_id);
    assert_eq!(receipt.timestamp(), quantity_u64(json, "timestamp"));
    assert_eq!(
        receipt.result_code(),
        match quantity(json, "status") {
            1 => 0,
            0 => 1,
            status => panic!("status {status}"),
        }
    );
    assert_eq!(receipt.context_hash(), array::<32>(json, "keyId"));
    assert_eq!(receipt.fee_charged(), quantity(json, "feeAmount"));
    assert_eq!(text(json, "feeDenom"), text(document, "feeDenom"));
    assert_eq!(receipt.asset(), sha256(text(json, "feeDenom").as_bytes()));
    assert_eq!(receipt.from(), addr32(&array(json, "from")));
    assert_eq!(receipt.to(), addr32(&array(json, "to")));
    assert_eq!(receipt.debit_sequence(), quantity_u64(json, "nonce"));
    assert_eq!(receipt.activity_root(), sha256(&hex(json, "returnData")));
    assert_eq!(receipt.transfer_set_root(), [0; 32]);
    assert_eq!(receipt.authorization_hash(), [0; 32]);
    assert!(receipt.sequencer_signature().is_some());
}

fn assert_effects(case: &JsonValue, receipt: &ProtocolReceipt) {
    let json = case
        .path("receiptJson")
        .unwrap_or_else(|error| panic!("{error}"));
    let logs = json
        .array_at("logs")
        .unwrap_or_else(|error| panic!("{error}"));
    let effects = receipt.effects();
    assert_eq!(effects.len(), logs.len() + 1, "{}", id(case));
    for (ordinal, effect) in effects.iter().enumerate() {
        assert_eq!(usize::from(effect.ordinal()), ordinal);
        assert_eq!(effect.module_id(), receipt.module_id());
        assert_eq!(effect.event_type(), 0);
        assert!(!effect.monetary());
        assert_eq!(effect.transfer_set_root(), [0; 32]);
        if ordinal == 0 {
            assert_eq!(effect.kind(), STATE_KIND);
            assert!(effect.body().len() <= 191);
            assert_eq!(effect.body(), summary_body(json).as_slice(), "{}", id(case));
        } else {
            let log = &logs[ordinal - 1];
            assert_eq!(effect.kind(), EVENT_KIND);
            assert!(effect.body().len() <= 181);
            assert_eq!(
                quantity(log, "logIndex"),
                u128::try_from(ordinal - 1).unwrap_or_else(|error| panic!("{error}"))
            );
            assert_eq!(effect.body(), log_body(log).as_slice(), "{}", id(case));
        }
    }
}

#[test]
fn go_signed_positive_and_revert_receipts_verify_with_identical_fields() {
    let document = fixture();
    assert_eq!(text(&document, "name"), "sequencer-receipt-v1");
    assert_eq!(integer(&document, "chainId"), 125);
    let mut seen = (0, 0);
    for case in cases(&document) {
        let vector = text(case, "vector");
        if vector == "V3" {
            continue;
        }
        let signed = hex(case, "signedReceipt");
        let receipt = verified(case).unwrap_or_else(|failure| panic!("{}: {failure:?}", id(case)));
        assert_eq!(text(case, "expected.rung"), "receipted");
        assert_eq!(text(case, "expected.verifier"), "accepts");
        assert_eq!(hex(case, "receiptJson.receipt"), signed, "{}", id(case));
        assert_eq!(
            encode(&receipt).unwrap_or_else(|error| panic!("{error:?}")),
            signed,
            "the Rust encoder reproduces the Go encoding of {}",
            id(case)
        );
        let unsigned = encode_unsigned(&receipt).unwrap_or_else(|error| panic!("{error:?}"));
        assert_eq!(
            receipt_digest(&unsigned).unwrap_or_else(|error| panic!("{error:?}")),
            array::<32>(case, "receiptDigest"),
            "{}",
            id(case)
        );
        let protocol = protocol(&receipt, case);
        assert_envelope(&document, case, protocol);
        assert_effects(case, protocol);
        match vector {
            "V1" => {
                assert_eq!(protocol.result_code(), 0);
                assert!(case.path("lxpCode").is_err());
                seen.0 += 1;
            }
            "V2" => {
                assert_eq!(protocol.result_code(), 1);
                let code = text(case, "lxpCode");
                let reason = revert_reason(&hex(case, "receiptJson.returnData"));
                assert_eq!(reason, format!("{code}: insufficient balance"));
                assert_eq!(code, "LXP_INSUFFICIENT_BALANCE");
                seen.1 += 1;
            }
            other => panic!("unexpected vector {other}"),
        }
    }
    assert_eq!(seen, (4, 1));
}

#[test]
fn go_signed_refusal_receipts_fail_at_the_named_check() {
    let document = fixture();
    let mut refused = Vec::new();
    let mut accepted = Vec::new();
    for case in cases(&document)
        .iter()
        .filter(|case| text(case, "vector") == "V3")
    {
        let outcome = verified(case);
        match text(case, "expected.verifier") {
            "accepts" => {
                let receipt = outcome.unwrap_or_else(|failure| panic!("{}: {failure:?}", id(case)));
                let protocol = protocol(&receipt, case);
                let previous: [u8; 32] = array(case, "prevSeqHash");
                let chained = protocol.previous_state_root() == previous
                    && protocol.resulting_state_root()
                        == seq_hash(
                            &previous,
                            protocol.global_sequence(),
                            &protocol.activity_id(),
                        );
                assert_eq!(
                    chained,
                    text(case, "expected.refusedAt") != "R6",
                    "{}",
                    id(case)
                );
                accepted.push(id(case).to_owned());
            }
            "SequencerSignature" => {
                assert_eq!(
                    outcome,
                    Err(VerificationFailure {
                        check: ReceiptCheck::SequencerSignature
                    }),
                    "{}",
                    id(case)
                );
                refused.push(id(case).to_owned());
            }
            "Decode" => {
                assert_eq!(
                    outcome,
                    Err(VerificationFailure {
                        check: ReceiptCheck::Decode
                    }),
                    "{}",
                    id(case)
                );
                refused.push(id(case).to_owned());
            }
            other => panic!("{}: unknown verifier check {other}", id(case)),
        }
    }
    assert_eq!(
        refused,
        [
            "V3-key-not-pinned",
            "V3-height-outside-key-range",
            "V3-non-canonical-bytes"
        ]
    );
    assert_eq!(
        accepted,
        [
            "V3-activity-id-not-raw-hash",
            "V3-seq-hash-mismatch",
            "V3-tampered-log"
        ]
    );
}

#[test]
fn tampered_log_data_differs_from_its_signed_effect() {
    let document = fixture();
    let case = cases(&document)
        .iter()
        .find(|case| id(case) == "V3-tampered-log")
        .unwrap_or_else(|| panic!("V3-tampered-log"));
    assert_eq!(text(case, "expected.refusedAt"), "R5");
    let receipt = verified(case).unwrap_or_else(|failure| panic!("{failure:?}"));
    let protocol = protocol(&receipt, case);
    let log = case
        .path("receiptJson.logs.0")
        .unwrap_or_else(|error| panic!("{error}"));
    let effect = &protocol.effects()[1];
    assert_ne!(effect.body(), log_body(log).as_slice());
    assert_eq!(
        effect.body()[..effect.body().len() - 32],
        log_body(log)[..effect.body().len() - 32]
    );
}
