//! The merchant settlement contract admitted by `POST /v1/settle`.
//!
//! The seller middleware posts its settlement request verbatim, so this module
//! parses exactly those fields, binds the presented evidence to the canonical
//! receipt it names, and reports the activity the settlement authority has to
//! resolve. Nothing here decides a settlement: the caller resolves the named
//! activity through the real authority path and renders the outcome with
//! [`settled`], [`pending`] or [`refused`].

use layerx_platform_internal::base64;
use layerx_proof::receipt::AuthorizedBatch;
use serde::Deserialize;
use sha2::{Digest, Sha256};
use subtle::ConstantTimeEq;

const X402_VERSION: u16 = 2;
const IDEMPOTENCY_DOMAIN: &[u8] = b"LayerX/middleware/x402/idempotency\0";
const VERIFICATION_LEVEL: &str = "sequencer-signed";
const MAX_PRINCIPAL_BYTES: usize = 512;
const MAX_KEY_BYTES: usize = 256;
const MAX_RECEIPT_BYTES: usize = 256 * 1024;

/// The largest settlement request body the route accepts.
pub const MAX_REQUEST_BYTES: usize = 512 * 1024;

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct RequestDocument {
    principal: String,
    payload: PayloadDocument,
    requirements: serde_json::Value,
    #[serde(rename = "idempotencyKey")]
    idempotency_key: String,
    #[serde(rename = "requestDigest")]
    request_digest: String,
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct PayloadDocument {
    #[serde(rename = "x402Version")]
    x402_version: u16,
    #[serde(default)]
    resource: Option<serde_json::Value>,
    payload: EvidenceDocument,
    accepted: serde_json::Value,
    #[serde(default)]
    extensions: Option<serde_json::Value>,
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct EvidenceDocument {
    receipt: String,
    #[serde(rename = "receiptDigest")]
    receipt_digest: String,
    #[serde(rename = "verificationLevel")]
    verification_level: String,
    #[serde(default, rename = "idempotencyKey")]
    idempotency_key: Option<String>,
    #[serde(default, rename = "purposeHash")]
    purpose_hash: Option<String>,
}

/// A settlement request bound to the exact canonical receipt it presents.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Claim {
    receipt: Vec<u8>,
    activity_id: [u8; 32],
    idempotency_key: String,
}

impl Claim {
    /// Borrows the canonical receipt bytes the merchant presented.
    #[must_use]
    pub fn receipt(&self) -> &[u8] {
        &self.receipt
    }

    /// Returns the activity the presented receipt names.
    #[must_use]
    pub const fn activity_id(&self) -> [u8; 32] {
        self.activity_id
    }
}

/// The exact contract check that refused a settlement request.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum Refusal {
    /// The body is not the bounded settlement request the seller sends.
    Request,
    /// The evidence asks for a verification level this authority never grants.
    VerificationLevel,
    /// A digest, key or receipt field is not the bounded encoding it declares.
    Evidence,
    /// The declared receipt digest is not the digest of the presented receipt.
    ReceiptDigest,
    /// The idempotency key is not bound to this principal and request digest.
    IdempotencyBinding,
    /// The presented bytes are not one canonical protocol receipt.
    Receipt,
}

impl Refusal {
    /// Returns the machine-readable reason carried to the merchant.
    #[must_use]
    pub const fn reason(self) -> &'static str {
        match self {
            Self::Request => "invalid_settlement_request",
            Self::VerificationLevel => "unsupported_verification_level",
            Self::Evidence => "invalid_receipt_evidence",
            Self::ReceiptDigest => "receipt_digest_mismatch",
            Self::IdempotencyBinding => "idempotency_binding_mismatch",
            Self::Receipt => "invalid_canonical_receipt",
        }
    }
}

/// Parses one seller settlement request and binds it to its canonical receipt.
///
/// # Errors
/// Returns the exact contract, evidence, digest, binding or receipt check that
/// refused the request. No partially bound claim is returned.
pub fn claim(body: &[u8]) -> Result<Claim, Refusal> {
    if body.is_empty() || body.len() > MAX_REQUEST_BYTES {
        return Err(Refusal::Request);
    }
    let document: RequestDocument = serde_json::from_slice(body).map_err(|_| Refusal::Request)?;
    if document.principal.is_empty()
        || document.principal.len() > MAX_PRINCIPAL_BYTES
        || document.payload.x402_version != X402_VERSION
        || !document.requirements.is_object()
        || !document.payload.accepted.is_object()
        || !document.payload.resource.as_ref().is_none_or(is_object)
        || !document.payload.extensions.as_ref().is_none_or(is_object)
    {
        return Err(Refusal::Request);
    }
    let evidence = &document.payload.payload;
    if evidence.verification_level != VERIFICATION_LEVEL {
        return Err(Refusal::VerificationLevel);
    }
    let receipt_digest = hex32(&evidence.receipt_digest).ok_or(Refusal::Evidence)?;
    let request_digest = hex32(&document.request_digest).ok_or(Refusal::Evidence)?;
    let idempotency_key = hex32(&document.idempotency_key).ok_or(Refusal::Evidence)?;
    if evidence
        .purpose_hash
        .as_deref()
        .is_some_and(|value| hex32(value).is_none())
        || evidence
            .idempotency_key
            .as_deref()
            .is_some_and(|value| value.is_empty() || value.len() > MAX_KEY_BYTES)
    {
        return Err(Refusal::Evidence);
    }
    let receipt = base64_decode(&evidence.receipt)
        .filter(|bytes| !bytes.is_empty() && bytes.len() <= MAX_RECEIPT_BYTES)
        .ok_or(Refusal::Evidence)?;
    let presented = layerx_proof::merkle::leaf_hash(&receipt).map_err(|_| Refusal::Evidence)?;
    if presented.ct_eq(&receipt_digest).unwrap_u8() != 1 {
        return Err(Refusal::ReceiptDigest);
    }
    let mut binding = Sha256::new();
    binding.update(IDEMPOTENCY_DOMAIN);
    binding.update(document.principal.as_bytes());
    binding.update(request_digest);
    let bound: [u8; 32] = binding.finalize().into();
    if bound.ct_eq(&idempotency_key).unwrap_u8() != 1 {
        return Err(Refusal::IdempotencyBinding);
    }
    let decoded = layerx_wire::receipt::decode(&receipt).map_err(|_| Refusal::Receipt)?;
    if layerx_wire::receipt::encode(&decoded).map_err(|_| Refusal::Receipt)? != receipt {
        return Err(Refusal::Receipt);
    }
    let activity_id = decoded
        .protocol()
        .map(layerx_wire::receipt::ProtocolReceipt::activity_id)
        .ok_or(Refusal::Receipt)?;
    if activity_id == [0; 32] {
        return Err(Refusal::Receipt);
    }
    Ok(Claim {
        receipt,
        activity_id,
        idempotency_key: document.idempotency_key,
    })
}

pub(super) fn authorize(
    claim: &Claim,
    keys: &layerx_platform_gateway::ReceiptKeyList,
) -> Result<(layerx_platform_gateway::VerifiedOperation, AuthorizedBatch), &'static str> {
    let decoded =
        layerx_wire::receipt::decode(claim.receipt()).map_err(|_| "receipt_verification_failed")?;
    let receipt = decoded.protocol().ok_or("receipt_verification_failed")?;
    let height = receipt_height(receipt).ok_or("receipt_verification_failed")?;
    let key = keys
        .authorization_for_header(receipt.context_hash(), height)
        .map_err(|_| "receipt_verification_failed")?;
    let facts = layerx_platform_gateway::AuthorityFacts::new(
        receipt.batch_id(),
        receipt.asset(),
        receipt.previous_state_root(),
        receipt.resulting_state_root(),
        key.public_key(),
    );
    let verified = layerx_platform_gateway::verify_settlement_receipt(
        claim.receipt(),
        facts,
        keys,
        claim.activity_id(),
    )
    .map_err(|_| "receipt_verification_failed")?;
    Ok((verified, facts.authorized()))
}

pub(super) fn sequencer_committed(
    canonical: &[u8],
    inclusion: &serde_json::Value,
) -> Result<bool, &'static str> {
    if inclusion.is_null() {
        return Ok(false);
    }
    let decoded =
        layerx_wire::receipt::decode(canonical).map_err(|_| "receipt_verification_failed")?;
    let receipt = decoded.protocol().ok_or("receipt_verification_failed")?;
    if rpc_bytes(&inclusion["txHash"]).as_deref() != Some(receipt.activity_id().as_slice())
        || quantity(&inclusion["nonce"]) != Some(u128::from(receipt.debit_sequence()))
    {
        return Err("settlement_inclusion_mismatch");
    }
    let rung = inclusion["rung"]
        .as_str()
        .ok_or("settlement_inclusion_mismatch")?;
    if rung == "submitted" {
        if inclusion.get("receipt").is_some()
            || inclusion.get("blockHash").is_some()
            || inclusion.get("blockNumber").is_some()
        {
            return Err("settlement_inclusion_mismatch");
        }
        return Ok(false);
    }
    if !matches!(rung, "receipted" | "committed") {
        return Err("settlement_inclusion_mismatch");
    }
    if rpc_bytes(&inclusion["receipt"]).as_deref() != Some(canonical) {
        return Err("settlement_receipt_mismatch");
    }
    if !sequencer_fields_match(receipt, inclusion) {
        return Err("settlement_inclusion_mismatch");
    }
    if rung == "receipted" {
        if inclusion.get("blockHash").is_some() || inclusion.get("blockNumber").is_some() {
            return Err("settlement_inclusion_mismatch");
        }
        return Ok(false);
    }
    let height = receipt_height(receipt).ok_or("settlement_inclusion_mismatch")?;
    if quantity(&inclusion["blockNumber"])
        .is_none_or(|committed| committed < u128::from(height) || u64::try_from(committed).is_err())
        || !rpc_bytes(&inclusion["blockHash"])
            .is_some_and(|hash| hash.len() == 32 && hash != [0; 32])
    {
        return Err("settlement_inclusion_mismatch");
    }
    Ok(true)
}

pub(super) fn transaction_committed(
    inclusion: &serde_json::Value,
    transaction: &serde_json::Value,
) -> Result<bool, &'static str> {
    if transaction.is_null() {
        return Ok(false);
    }
    for (chain, sequencer) in [("transactionHash", "txHash"), ("blockHash", "blockHash")] {
        if rpc_bytes(&transaction[chain]) != rpc_bytes(&inclusion[sequencer])
            || rpc_bytes(&transaction[chain]).is_none()
        {
            return Err("settlement_inclusion_mismatch");
        }
    }
    for (chain, sequencer) in [
        ("blockNumber", "blockNumber"),
        ("status", "status"),
        ("gasUsed", "gasUsed"),
        ("cumulativeGasUsed", "cumulativeGasUsed"),
        ("effectiveGasPrice", "effectiveGasPrice"),
    ] {
        if quantity(&transaction[chain]) != quantity(&inclusion[sequencer])
            || quantity(&transaction[chain]).is_none()
        {
            return Err("settlement_inclusion_mismatch");
        }
    }
    let logs = transaction["logs"]
        .as_array()
        .ok_or("settlement_inclusion_mismatch")?;
    let expected = inclusion["logs"]
        .as_array()
        .ok_or("settlement_inclusion_mismatch")?;
    if logs.len() != expected.len()
        || logs.iter().zip(expected).any(|(actual, expected)| {
            actual
                .get("removed")
                .is_some_and(|removed| removed != false)
                || ["address", "data"]
                    .iter()
                    .any(|field| rpc_bytes(&actual[field]) != rpc_bytes(&expected[field]))
                || !topics_match(&actual["topics"], &expected["topics"])
        })
    {
        return Err("settlement_inclusion_mismatch");
    }
    Ok(true)
}

fn sequencer_fields_match(
    receipt: &layerx_wire::receipt::ProtocolReceipt,
    view: &serde_json::Value,
) -> bool {
    let Some(summary) = receipt.effects().first() else {
        return false;
    };
    let body = summary.body();
    if summary.ordinal() != 0
        || summary.kind() != 1
        || summary.monetary()
        || summary.module_id() != receipt.module_id()
        || body.len() < 109
    {
        return false;
    }
    let Some(denom) = view["feeDenom"].as_str() else {
        return false;
    };
    let Some(return_data) = rpc_bytes(&view["returnData"]) else {
        return false;
    };
    let Some(vm_error) = view["vmError"].as_str() else {
        return false;
    };
    let Some(logs) = view["logs"].as_array() else {
        return false;
    };
    let Some(height) = receipt_height(receipt) else {
        return false;
    };
    let mut sequence = Sha256::new();
    sequence.update(b"PAXEER/v1/sequence\0");
    sequence.update(receipt.previous_state_root());
    sequence.update(receipt.global_sequence().to_be_bytes());
    sequence.update(receipt.activity_id());
    let sequence_hash: [u8; 32] = sequence.finalize().into();
    if sequence_hash != receipt.resulting_state_root()
        || receipt.global_sequence() == 0
        || height == 0
        || receipt.timestamp() == 0
        || receipt.protocol_version() != 2
        || receipt.module_version() != 1
        || receipt.parameter_version() != 0
        || !matches!(receipt.operation(), 0 | 1 | 2 | 4)
        || !matches!(receipt.result_code(), 0 | 1)
        || receipt.from()[..12] != [0; 12]
        || receipt.to()[..12] != [0; 12]
        || receipt.transfer_set_root() != [0; 32]
        || denom.is_empty()
        || denom.len() > 64
        || usize::from(body[108]) != denom.len()
        || body.get(109..) != Some(denom.as_bytes())
        || Sha256::digest(denom.as_bytes()).as_slice() != receipt.asset()
        || Sha256::digest(&return_data).as_slice() != receipt.activity_root()
        || Sha256::digest(vm_error.as_bytes()).as_slice() != &body[68..100]
        || word(&body[100..104]) != Some(return_data.len() as u128)
        || word(&body[104..108]) != Some(logs.len() as u128)
        || receipt.effects().len() != logs.len() + 1
    {
        return false;
    }
    view_fields_match(receipt, view, body, height) && logs_match(receipt, logs)
}

fn view_fields_match(
    receipt: &layerx_wire::receipt::ProtocolReceipt,
    view: &serde_json::Value,
    body: &[u8],
    height: u64,
) -> bool {
    for (name, expected) in [
        ("seq", u128::from(receipt.global_sequence())),
        ("height", u128::from(height)),
        ("timestamp", u128::from(receipt.timestamp())),
        ("status", u128::from(receipt.result_code() == 0)),
        ("feeAmount", receipt.fee_charged()),
    ] {
        if quantity(&view[name]) != Some(expected) {
            return false;
        }
    }
    for (name, bytes) in [
        ("gasUsed", &body[..8]),
        ("cumulativeGasUsed", &body[8..16]),
        ("effectiveGasPrice", &body[16..24]),
        ("index", &body[24..28]),
    ] {
        if quantity(&view[name]) != word(bytes) {
            return false;
        }
    }
    for (name, expected) in [
        ("keyId", receipt.context_hash().to_vec()),
        ("from", receipt.from()[12..].to_vec()),
        ("to", receipt.to()[12..].to_vec()),
        ("contractAddress", body[28..48].to_vec()),
        ("sponsor", body[48..68].to_vec()),
    ] {
        if rpc_bytes(&view[name]) != Some(expected) {
            return false;
        }
    }
    true
}

fn logs_match(receipt: &layerx_wire::receipt::ProtocolReceipt, logs: &[serde_json::Value]) -> bool {
    logs.iter()
        .zip(&receipt.effects()[1..])
        .enumerate()
        .all(|(index, (log, effect))| {
            let body = effect.body();
            if body.len() < 53
                || effect.kind() != 3
                || effect.monetary()
                || effect.module_id() != receipt.module_id()
                || u16::try_from(index)
                    .ok()
                    .and_then(|ordinal| ordinal.checked_add(1))
                    != Some(effect.ordinal())
                || quantity(&log["logIndex"]) != Some(index as u128)
            {
                return false;
            }
            let count = usize::from(body[20]);
            let Some(topics) = log["topics"].as_array() else {
                return false;
            };
            let Some(data) = rpc_bytes(&log["data"]) else {
                return false;
            };
            count <= 4
                && topics.len() == count
                && body.len() == 53 + count * 32
                && rpc_bytes(&log["address"]).as_deref() == Some(&body[..20])
                && topics.iter().enumerate().all(|(i, topic)| {
                    rpc_bytes(topic).as_deref() == body.get(21 + i * 32..53 + i * 32)
                })
                && Sha256::digest(data).as_slice() == &body[21 + count * 32..]
        })
}

fn receipt_height(receipt: &layerx_wire::receipt::ProtocolReceipt) -> Option<u64> {
    let batch = receipt.batch_id();
    if batch[..24] != [0; 24] {
        return None;
    }
    Some(u64::from_be_bytes(batch[24..].try_into().ok()?))
}

fn word(bytes: &[u8]) -> Option<u128> {
    if bytes.len() > 16 {
        return None;
    }
    Some(
        bytes
            .iter()
            .fold(0, |value, byte| (value << 8) | u128::from(*byte)),
    )
}

fn quantity(value: &serde_json::Value) -> Option<u128> {
    let digits = value.as_str()?.strip_prefix("0x")?;
    if digits.is_empty()
        || digits.len() > 32
        || (digits.len() > 1 && digits.starts_with('0'))
        || !digits.bytes().all(|byte| byte.is_ascii_hexdigit())
    {
        return None;
    }
    u128::from_str_radix(digits, 16).ok()
}

fn rpc_bytes(value: &serde_json::Value) -> Option<Vec<u8>> {
    let digits = value.as_str()?.strip_prefix("0x")?;
    if digits.len() > super::MAX_REQUEST * 2 || !digits.len().is_multiple_of(2) {
        return None;
    }
    digits
        .as_bytes()
        .chunks_exact(2)
        .map(|pair| u8::from_str_radix(std::str::from_utf8(pair).ok()?, 16).ok())
        .collect()
}

fn topics_match(actual: &serde_json::Value, expected: &serde_json::Value) -> bool {
    let (Some(actual), Some(expected)) = (actual.as_array(), expected.as_array()) else {
        return false;
    };
    actual.len() == expected.len()
        && actual
            .iter()
            .zip(expected)
            .all(|(actual, expected)| rpc_bytes(actual) == rpc_bytes(expected))
}

/// Renders the settled result exactly as the seller middleware parses it.
#[must_use]
pub fn settled(claim: &Claim, canonical: &[u8], batch: &AuthorizedBatch) -> serde_json::Value {
    serde_json::json!({
        "state": "settled",
        "activity_id": hex(&claim.activity_id),
        "idempotency_key": claim.idempotency_key,
        "receipt_base64": base64_encode(canonical),
        "authorized_batch": {
            "batch_id": hex(&batch.batch_id()),
            "asset": hex(&batch.asset()),
            "previous_state_root": hex(&batch.previous_state_root()),
            "resulting_state_root": hex(&batch.resulting_state_root()),
            "sequencer_public_key": hex(&batch.sequencer_public_key()),
        },
    })
}

/// Renders the result carried while the named activity has no published batch.
#[must_use]
pub fn pending() -> serde_json::Value {
    serde_json::json!({ "state": "pending" })
}

/// Renders a settlement this authority refuses, naming the exact reason.
#[must_use]
pub fn refused(reason: &str) -> serde_json::Value {
    serde_json::json!({ "state": "refused", "reason": reason })
}

fn is_object(value: &serde_json::Value) -> bool {
    value.is_object()
}

fn base64_decode(value: &str) -> Option<Vec<u8>> {
    base64::decode(value)
}

fn base64_encode(bytes: &[u8]) -> String {
    base64::encode(bytes)
}

fn hex32(value: &str) -> Option<[u8; 32]> {
    if value.len() != 64 || !value.bytes().all(|byte| byte.is_ascii_hexdigit()) {
        return None;
    }
    let mut bytes = [0_u8; 32];
    for (index, pair) in value.as_bytes().chunks_exact(2).enumerate() {
        let text = std::str::from_utf8(pair).ok()?;
        bytes[index] = u8::from_str_radix(text, 16).ok()?;
    }
    Some(bytes)
}

fn hex(bytes: &[u8]) -> String {
    const DIGITS: &[u8; 16] = b"0123456789abcdef";
    let mut value = String::with_capacity(bytes.len() * 2);
    for byte in bytes {
        value.push(char::from(DIGITS[usize::from(byte >> 4)]));
        value.push(char::from(DIGITS[usize::from(byte & 0x0f)]));
    }
    value
}
