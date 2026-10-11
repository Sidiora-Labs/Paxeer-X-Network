use ed25519_dalek::{Signer as _, SigningKey};
use layerx_platform_gateway::{
    verify_live_receipt, verify_settlement_receipt, AuthorityFacts, GatewayError, ReceiptKeyList,
};
use layerx_platform_internal::base64;
use layerx_proof::receipt::ReceiptCheck;
use layerx_wire::encode::Encoder;
use layerx_wire::hash::receipt_digest;
use serde_json::{json, Value};

type TestResult = Result<(), Box<dyn std::error::Error>>;

fn entry(id: [u8; 32], signer: &SigningKey, first: u64, last: u64) -> Value {
    json!({
        "key_id": base64::encode(&id),
        "public_key": base64::encode(&signer.verifying_key().to_bytes()),
        "first_height": first,
        "last_height": last,
    })
}

fn batch(height: u64) -> [u8; 32] {
    let mut id = [0; 32];
    id[24..].copy_from_slice(&height.to_be_bytes());
    id
}

fn unsigned_receipt(key_id: [u8; 32], batch_id: [u8; 32], activity: [u8; 32]) -> Vec<u8> {
    let mut encoder = Encoder::new(4096);
    assert_eq!(encoder.structure_header_version(0x5201, 2), Ok(()));
    assert_eq!(encoder.u16(2), Ok(()));
    assert_eq!(encoder.bytes(&activity, 32), Ok(()));
    assert_eq!(encoder.u64(9), Ok(()));
    assert_eq!(encoder.bytes(&[2; 32], 32), Ok(()));
    assert_eq!(encoder.bytes(&[3; 32], 32), Ok(()));
    assert_eq!(encoder.bytes(&[8; 32], 32), Ok(()));
    assert_eq!(encoder.i32(0), Ok(()));
    assert_eq!(encoder.sequence_length(0, 512), Ok(()));
    assert_eq!(encoder.u128(1), Ok(()));
    assert_eq!(encoder.bytes(&batch_id, 32), Ok(()));
    assert_eq!(encoder.u16(1), Ok(()));
    assert_eq!(encoder.u32(1), Ok(()));
    assert_eq!(encoder.u32(0), Ok(()));
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
    assert_eq!(encoder.bytes(&[0; 32], 32), Ok(()));
    assert_eq!(encoder.bytes(&[0; 32], 32), Ok(()));
    assert_eq!(encoder.bytes(&key_id, 32), Ok(()));
    assert_eq!(encoder.u64(1_000), Ok(()));
    assert_eq!(encoder.u8(0), Ok(()));
    encoder.finish()
}

fn sign(mut unsigned: Vec<u8>, signer: &SigningKey) -> Result<Vec<u8>, String> {
    let digest = receipt_digest(&unsigned).map_err(|error| format!("{error:?}"))?;
    assert_eq!(unsigned.pop(), Some(0));
    let mut signature = Encoder::new(128);
    assert_eq!(signature.u8(1), Ok(()));
    assert_eq!(
        signature.bytes(&signer.sign(&digest).to_bytes(), 64),
        Ok(())
    );
    unsigned.extend(signature.finish());
    Ok(unsigned)
}

fn facts(height: u64, signer: &SigningKey) -> AuthorityFacts {
    AuthorityFacts::new(
        batch(height),
        [5; 32],
        [2; 32],
        [3; 32],
        signer.verifying_key().to_bytes(),
    )
}

#[test]
fn list_covers_rotation() -> TestResult {
    let first = SigningKey::from_bytes(&[3; 32]);
    let second = SigningKey::from_bytes(&[4; 32]);
    let document = json!([
        entry([11; 32], &first, 5000, 6999),
        entry([12; 32], &second, 7000, u64::MAX)
    ]);
    let keys = ReceiptKeyList::from_config(&document.to_string())?;
    assert_eq!(keys.single(), None);
    for (height, id, signer) in [
        (5000, [11; 32], &first),
        (6500, [11; 32], &first),
        (6999, [11; 32], &first),
        (7000, [12; 32], &second),
        (7100, [12; 32], &second),
        (u64::MAX, [12; 32], &second),
    ] {
        let bytes = sign(unsigned_receipt(id, batch(height), [1; 32]), signer)?;
        let verified = verify_settlement_receipt(&bytes, facts(height, signer), &keys, [1; 32])?;
        assert_eq!(verified.receipt(), bytes);
        assert_eq!(verified.result_code(), 0);
        assert_eq!(verified.activity_id(), [1; 32]);
        let decoded = keys.verify_receipt(&bytes)?;
        let protocol = decoded.protocol().ok_or("protocol receipt required")?;
        assert_eq!(protocol.context_hash(), id);
        assert_eq!(protocol.batch_id(), batch(height));
        assert_eq!(protocol.global_sequence(), 9);
    }
    for height in [4999, 7000, 7100] {
        let bytes = sign(unsigned_receipt([11; 32], batch(height), [1; 32]), &first)?;
        assert_eq!(
            keys.verify_receipt(&bytes),
            Err(GatewayError::UntrustedSequencer)
        );
    }
    let bytes = sign(unsigned_receipt([12; 32], batch(6999), [1; 32]), &second)?;
    assert_eq!(
        keys.verify_receipt(&bytes),
        Err(GatewayError::UntrustedSequencer)
    );
    Ok(())
}

#[test]
fn overlapping_primary_and_standby_ranges_are_pinned_by_id() -> TestResult {
    let primary = SigningKey::from_bytes(&[3; 32]);
    let standby = SigningKey::from_bytes(&[4; 32]);
    let document = json!([
        entry([11; 32], &primary, 5000, 7999),
        entry([12; 32], &standby, 7000, u64::MAX)
    ]);
    let keys = ReceiptKeyList::from_config(&document.to_string())?;
    for (id, signer) in [([11; 32], &primary), ([12; 32], &standby)] {
        let bytes = sign(unsigned_receipt(id, batch(7100), [1; 32]), signer)?;
        let decoded = keys.verify_receipt(&bytes)?;
        assert_eq!(
            decoded
                .protocol()
                .ok_or("protocol receipt required")?
                .context_hash(),
            id
        );
    }
    let bytes = sign(unsigned_receipt([12; 32], batch(7100), [1; 32]), &primary)?;
    assert_eq!(
        keys.verify_receipt(&bytes),
        Err(GatewayError::Receipt(ReceiptCheck::SequencerSignature))
    );
    let bytes = sign(unsigned_receipt([13; 32], batch(7100), [1; 32]), &primary)?;
    assert_eq!(
        keys.verify_receipt(&bytes),
        Err(GatewayError::UntrustedSequencer)
    );
    Ok(())
}

#[test]
fn invalid_lists_are_refused_without_partial_pins() {
    let signer = SigningKey::from_bytes(&[3; 32]);
    let valid = entry([11; 32], &signer, 5000, 6999);
    for document in [
        json!([]),
        json!({"receipt_keys": [valid.clone()]}),
        Value::Null,
    ] {
        assert_eq!(
            ReceiptKeyList::from_config(&document.to_string()),
            Err("receipt key list")
        );
    }
    assert_eq!(
        ReceiptKeyList::from_config(&" ".repeat(256 * 1024 + 1)),
        Err("receipt key list")
    );
    assert_eq!(
        ReceiptKeyList::from_config(&json!([valid.clone(), valid.clone()]).to_string()),
        Err("duplicate receipt key ID")
    );
    for (field, value, refused) in [
        ("first_height", json!(7000), "authorized batch range"),
        ("first_height", json!("5000"), "receipt key list"),
        ("last_height", json!(-1), "receipt key list"),
        ("key_id", json!(base64::encode(&[0; 32])), "sequencer ID"),
        ("key_id", json!(base64::encode(&[11; 31])), "sequencer ID"),
        (
            "public_key",
            json!(base64::encode(&[0; 32])),
            "sequencer public key",
        ),
        (
            "public_key",
            json!(base64::encode(&[255; 32])),
            "sequencer public key",
        ),
        (
            "public_key",
            json!(base64::encode(&[3; 31])),
            "sequencer public key",
        ),
        ("extra", json!(1), "receipt key list"),
    ] {
        let mut invalid = valid.clone();
        invalid[field] = value;
        assert_eq!(
            ReceiptKeyList::from_config(&json!([invalid]).to_string()),
            Err(refused)
        );
    }
}

#[test]
fn receipts_require_canonical_height_identity_and_signature() -> TestResult {
    let signer = SigningKey::from_bytes(&[3; 32]);
    let keys =
        ReceiptKeyList::from_config(&json!([entry([11; 32], &signer, 5000, 6999)]).to_string())?;
    let unsigned = unsigned_receipt([11; 32], batch(6500), [1; 32]);
    assert_eq!(
        keys.verify_receipt(&unsigned),
        Err(GatewayError::Receipt(ReceiptCheck::MissingSignature))
    );
    let mut invalid_height = batch(6500);
    invalid_height[0] = 1;
    let bytes = sign(unsigned_receipt([11; 32], invalid_height, [1; 32]), &signer)?;
    assert_eq!(
        keys.verify_receipt(&bytes),
        Err(GatewayError::Receipt(ReceiptCheck::BatchId))
    );
    let bytes = sign(unsigned_receipt([11; 32], batch(6500), [0; 32]), &signer)?;
    assert_eq!(
        keys.verify_receipt(&bytes),
        Err(GatewayError::Receipt(ReceiptCheck::ActivityId))
    );
    let mut bytes = sign(unsigned, &signer)?;
    let last = bytes.last_mut().ok_or("signature required")?;
    *last ^= 1;
    assert_eq!(
        keys.verify_receipt(&bytes),
        Err(GatewayError::Receipt(ReceiptCheck::SequencerSignature))
    );
    assert_eq!(
        keys.verify_receipt(&bytes[..bytes.len() - 1]),
        Err(GatewayError::Receipt(ReceiptCheck::Decode))
    );
    bytes.push(0);
    assert_eq!(
        keys.verify_receipt(&bytes),
        Err(GatewayError::Receipt(ReceiptCheck::Decode))
    );
    Ok(())
}

#[test]
fn settlement_preserves_authority_and_activity_checks() -> TestResult {
    let signer = SigningKey::from_bytes(&[3; 32]);
    let other = SigningKey::from_bytes(&[4; 32]);
    let keys =
        ReceiptKeyList::from_config(&json!([entry([11; 32], &signer, 5000, 6999)]).to_string())?;
    let bytes = sign(unsigned_receipt([11; 32], batch(6500), [1; 32]), &signer)?;
    assert_eq!(
        keys.single().ok_or("single pin required")?.sequencer_id(),
        [11; 32]
    );
    assert_eq!(
        verify_settlement_receipt(&bytes, facts(6500, &other), &keys, [1; 32]).err(),
        Some(GatewayError::UntrustedSequencer)
    );
    assert_eq!(
        verify_settlement_receipt(&bytes, facts(6500, &signer), &keys, [2; 32]).err(),
        Some(GatewayError::ActivityMismatch)
    );
    assert_eq!(
        verify_settlement_receipt(&bytes, facts(6501, &signer), &keys, [1; 32]).err(),
        Some(GatewayError::Receipt(ReceiptCheck::BatchId))
    );
    let wrong_asset = AuthorityFacts::new(
        batch(6500),
        [9; 32],
        [2; 32],
        [3; 32],
        signer.verifying_key().to_bytes(),
    );
    assert_eq!(
        verify_settlement_receipt(&bytes, wrong_asset, &keys, [1; 32]).err(),
        Some(GatewayError::Receipt(ReceiptCheck::Asset))
    );
    Ok(())
}

#[test]
fn two_key_list_verifies_historical_and_current_receipts() -> TestResult {
    let first = SigningKey::from_bytes(&[3; 32]);
    let second = SigningKey::from_bytes(&[4; 32]);
    let stranger = SigningKey::from_bytes(&[5; 32]);
    let document = json!([
        entry([11; 32], &first, 5000, 6999),
        entry([12; 32], &second, 7000, u64::MAX)
    ]);
    let keys = ReceiptKeyList::from_config(&document.to_string())?;
    assert_eq!(keys.single(), None);
    for (height, id, signer) in [(6000, [11; 32], &first), (7500, [12; 32], &second)] {
        let bytes = sign(unsigned_receipt(id, batch(height), [1; 32]), signer)?;
        let authority = facts(height, signer);
        let selected = keys.authorization_for_facts(authority)?;
        assert_eq!(selected.sequencer_id(), id);
        assert_eq!(selected.public_key(), signer.verifying_key().to_bytes());
        assert_eq!(keys.authorization_for_receipt(&bytes)?.sequencer_id(), id);
        assert_eq!(
            keys.authorization_for_header(id, height)?.sequencer_id(),
            id
        );
        let verified = verify_live_receipt(&bytes, authority, &keys, Some([1; 32]))?;
        assert_eq!(verified.receipt(), bytes);
        assert_eq!(verified.result_code(), 0);
        assert_eq!(verified.activity_id(), [1; 32]);
    }
    for (height, id, signer) in [
        (7500, [13; 32], &stranger),
        (7500, [11; 32], &first),
        (4999, [11; 32], &first),
    ] {
        let bytes = sign(unsigned_receipt(id, batch(height), [1; 32]), signer)?;
        let authority = facts(height, signer);
        assert_eq!(
            keys.authorization_for_facts(authority),
            Err(GatewayError::UntrustedSequencer)
        );
        assert_eq!(
            keys.authorization_for_receipt(&bytes),
            Err(GatewayError::UntrustedSequencer)
        );
        assert_eq!(
            keys.authorization_for_header(id, height),
            Err(GatewayError::UntrustedSequencer)
        );
        assert_eq!(
            verify_live_receipt(&bytes, authority, &keys, Some([1; 32])).err(),
            Some(GatewayError::UntrustedSequencer)
        );
    }
    Ok(())
}
