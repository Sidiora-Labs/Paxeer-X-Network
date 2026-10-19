//! x402 v2 buyer role: strict offer selection, payment construction through a
//! typed `LayerX` boundary, extension echoing, and local receipt capture.

use base64::engine::general_purpose::STANDARD;
use base64::Engine as _;
use layerx_interop_gateway::trace::{TraceId, Traced};
use layerx_proof::merkle::leaf_hash;
use layerx_proof::receipt::{verify, AuthorizedBatch};
use serde::Deserialize;
use serde_json::Value;

use crate::codec::{decode_header, encode_header};
use crate::model::{
    PaymentPayload, PaymentRequired, PaymentRequirements, SettlementResponse, X402Error,
    X402_VERSION,
};
use crate::seller::{payment_matches, validate_network};

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct SupportedKind {
    pub scheme: String,
    pub network: String,
}

#[derive(Clone, Debug, PartialEq)]
pub struct PaymentBuildRequest {
    pub requirements: PaymentRequirements,
    pub idempotency_key: [u8; 32],
    pub trace: TraceId,
}

/// Boundary that constructs the scheme-specific payment through the plane's
/// typed authority. The buyer adapter never signs or invents payment bytes.
pub trait BuyerPaymentPlane {
    /// Constructs the scheme payload through the plane's authority.
    ///
    /// # Errors
    ///
    /// Returns a typed construction or policy refusal.
    fn construct(&mut self, request: PaymentBuildRequest) -> Result<Value, X402Error>;
}

#[derive(Clone, Debug, PartialEq)]
pub struct BuiltPayment {
    pub header: String,
    pub payload: PaymentPayload,
    pub idempotency_key: [u8; 32],
}

#[derive(Clone, Debug, PartialEq)]
pub struct CapturedSettlement {
    pub response: SettlementResponse,
    pub canonical_receipt: Vec<u8>,
    pub receipt_digest: [u8; 32],
}

#[derive(Debug, Deserialize)]
#[serde(deny_unknown_fields, rename_all = "camelCase")]
struct LayerXEvidence {
    receipt: String,
    receipt_digest: String,
    verification_level: String,
    #[serde(default)]
    purpose_hash: Option<String>,
}

/// x402 buyer with a closed list of supported scheme/network pairs.
#[derive(Clone, Debug)]
pub struct Buyer {
    supported: Vec<SupportedKind>,
}

impl Buyer {
    /// Constructs a buyer with at least one bounded supported kind.
    ///
    /// # Errors
    ///
    /// Refuses an empty or duplicate support declaration.
    pub fn new(supported: Vec<SupportedKind>) -> Result<Self, X402Error> {
        if supported.is_empty() || supported.len() > 64 {
            return Err(X402Error::UnsupportedOffer);
        }
        for (index, kind) in supported.iter().enumerate() {
            if kind.scheme.is_empty()
                || validate_network(&kind.network).is_err()
                || supported[..index].contains(kind)
            {
                return Err(X402Error::UnsupportedOffer);
            }
        }
        Ok(Self { supported })
    }

    /// Parses a `PAYMENT-REQUIRED` header, selects the first supported offer
    /// in seller order, and asks the real plane boundary to construct its
    /// scheme payload. Required extensions are echoed byte-for-value.
    ///
    /// # Errors
    ///
    /// Returns a trace-bound typed refusal for malformed or unsupported
    /// offers and plane construction failures.
    pub fn build_payment(
        &self,
        required_header: &str,
        idempotency_key: [u8; 32],
        plane: &mut impl BuyerPaymentPlane,
        trace: &TraceId,
    ) -> Result<BuiltPayment, Traced<X402Error>> {
        let fail = |error| trace.wrap(error);
        if idempotency_key == [0; 32] {
            return Err(fail(X402Error::InvalidPayload));
        }
        let required: PaymentRequired = decode_header(required_header).map_err(fail)?;
        required.validate().map_err(fail)?;
        let accepted = required
            .accepts
            .iter()
            .find(|requirements| {
                self.supported.iter().any(|kind| {
                    kind.scheme == requirements.scheme && kind.network == requirements.network
                })
            })
            .cloned()
            .ok_or_else(|| fail(X402Error::UnsupportedOffer))?;
        validate_network(&accepted.network).map_err(fail)?;
        accepted.layerx_terms().map_err(fail)?;
        let scheme_payload = plane
            .construct(PaymentBuildRequest {
                requirements: accepted.clone(),
                idempotency_key,
                trace: trace.clone(),
            })
            .map_err(fail)?;
        if !scheme_payload.is_object() {
            return Err(fail(X402Error::InvalidPayload));
        }
        let payload = PaymentPayload {
            x402_version: X402_VERSION,
            resource: Some(required.resource),
            payload: scheme_payload,
            accepted,
            extensions: required.extensions,
        };
        payload.validate().map_err(fail)?;
        let header = encode_header(&payload).map_err(fail)?;
        Ok(BuiltPayment {
            header,
            payload,
            idempotency_key,
        })
    }

    /// Captures a `PAYMENT-RESPONSE` only after locally verifying the attached
    /// canonical `LayerX` receipt under the caller's authorised batch.
    ///
    /// # Errors
    ///
    /// Refuses pending/failed responses as success, missing or mismatched
    /// evidence, malformed receipts and unauthorised sequencer signatures,
    /// a grant settlement whose `purposeHash` is absent or is not the one the
    /// accepted offer carries, and an exact settlement carrying one.
    pub fn capture_settlement(
        response_header: &str,
        expected: &BuiltPayment,
        authorised_batch: &AuthorizedBatch,
        trace: &TraceId,
    ) -> Result<CapturedSettlement, Traced<X402Error>> {
        let fail = |error| trace.wrap(error);
        let response: SettlementResponse = decode_header(response_header).map_err(fail)?;
        response.validate_wire().map_err(fail)?;
        if !response.success {
            return Err(fail(X402Error::PaymentRefused));
        }
        if response.transaction.len() != 66
            || !response.transaction.starts_with("0x")
            || !response.transaction.as_bytes()[2..]
                .iter()
                .all(|byte| byte.is_ascii_digit() || (b'a'..=b'f').contains(byte))
        {
            return Err(fail(X402Error::EvidenceMismatch));
        }
        validate_network(&response.network).map_err(fail)?;
        let evidence_value = response
            .extensions
            .get("layerx")
            .cloned()
            .ok_or_else(|| fail(X402Error::EvidenceMissing))?;
        let evidence: LayerXEvidence =
            serde_json::from_value(evidence_value).map_err(|_| fail(X402Error::EvidenceMissing))?;
        if evidence.verification_level != "receipted"
            || evidence.purpose_hash
                != expected_purpose(&expected.payload.accepted).map_err(fail)?
        {
            return Err(fail(X402Error::EvidenceMismatch));
        }
        let canonical_receipt = STANDARD
            .decode(evidence.receipt.as_bytes())
            .map_err(|_| fail(X402Error::EvidenceMismatch))?;
        let verified = verify(&canonical_receipt, authorised_batch)
            .map_err(|_| fail(X402Error::EvidenceMismatch))?;
        let protocol = verified
            .receipt()
            .protocol()
            .ok_or_else(|| fail(X402Error::EvidenceMismatch))?;
        payment_matches(
            &expected.payload.accepted,
            protocol.asset(),
            protocol.to(),
            protocol.amount(),
        )
        .map_err(fail)?;
        let payer = hex(&protocol.from());
        if response.network != expected.payload.accepted.network
            || response.amount != Some(expected.payload.accepted.amount)
            || response.payer.as_deref() != Some(payer.as_str())
        {
            return Err(fail(X402Error::EvidenceMismatch));
        }
        let receipt_digest =
            leaf_hash(verified.canonical_bytes()).map_err(|_| fail(X402Error::EvidenceMismatch))?;
        let digest_text = hex(&receipt_digest);
        if evidence.receipt_digest != digest_text
            || response.transaction != format!("0x{}", hex(&protocol.activity_id()))
        {
            return Err(fail(X402Error::EvidenceMismatch));
        }
        Ok(CapturedSettlement {
            response,
            canonical_receipt,
            receipt_digest,
        })
    }
}

/// The `purposeHash` a settlement of `accepted` must repeat: none for an
/// exact offer, and for a metered or subscription grant offer the 64 lowercase
/// hexadecimal characters of its challenge, which such an offer must carry.
fn expected_purpose(accepted: &PaymentRequirements) -> Result<Option<String>, X402Error> {
    if !matches!(accepted.scheme.as_str(), "metered" | "subscription") {
        return Ok(None);
    }
    accepted
        .extra
        .as_ref()
        .and_then(|extra| extra.pointer("/layerx/purposeHash"))
        .and_then(Value::as_str)
        .filter(|purpose| {
            purpose.len() == 64
                && purpose
                    .bytes()
                    .all(|byte| byte.is_ascii_digit() || (b'a'..=b'f').contains(&byte))
        })
        .map(|purpose| Some(purpose.to_owned()))
        .ok_or(X402Error::EvidenceMismatch)
}

fn hex(bytes: &[u8; 32]) -> String {
    const DIGITS: &[u8; 16] = b"0123456789abcdef";
    let mut output = String::with_capacity(64);
    for byte in bytes {
        output.push(char::from(DIGITS[usize::from(byte >> 4)]));
        output.push(char::from(DIGITS[usize::from(byte & 0x0f)]));
    }
    output
}

#[cfg(test)]
mod tests {
    use std::collections::BTreeMap;

    use ed25519_dalek::{Signer as _, SigningKey};
    use layerx_wire::encode::Encoder;
    use layerx_wire::limits::PROTOCOL_VERSION;
    use serde_json::json;

    use super::{hex, BuiltPayment, Buyer, CapturedSettlement, SupportedKind};
    use crate::codec::encode_header;
    use crate::model::{account_identifiers, AtomicAmount};
    use crate::model::{
        PaymentPayload, PaymentRequirements, SettlementResponse, X402Error, X402_VERSION,
    };
    use base64::engine::general_purpose::STANDARD;
    use base64::Engine as _;
    use layerx_interop_gateway::trace::{TraceId, Traced};
    use layerx_proof::merkle::leaf_hash;
    use layerx_proof::receipt::AuthorizedBatch;

    const ACCOUNT: &str = "agent:did:layerx:payee:main";
    const ASSET: [u8; 32] = [0x11; 32];
    const PAYER: [u8; 32] = [0x22; 32];
    const TRANSACTION: [u8; 32] = [0x61; 32];

    fn receipt(recipient: &[u8; 32], signature: Option<[u8; 64]>) -> Result<Vec<u8>, X402Error> {
        let invalid = |_| X402Error::InvalidPayload;
        let mut encoder = Encoder::new(4096);
        encoder
            .structure_header_version(0x5201, PROTOCOL_VERSION)
            .map_err(invalid)?;
        encoder.u16(PROTOCOL_VERSION).map_err(invalid)?;
        encoder.bytes(&TRANSACTION, 32).map_err(invalid)?;
        encoder.u64(9).map_err(invalid)?;
        encoder.bytes(&[0x62; 32], 32).map_err(invalid)?;
        encoder.bytes(&[0x63; 32], 32).map_err(invalid)?;
        encoder.bytes(&[0x64; 32], 32).map_err(invalid)?;
        encoder.i32(0).map_err(invalid)?;
        encoder.sequence_length(0, 512).map_err(invalid)?;
        encoder.u128(1).map_err(invalid)?;
        encoder.bytes(&[0x65; 32], 32).map_err(invalid)?;
        encoder.u16(1).map_err(invalid)?;
        encoder.u32(1).map_err(invalid)?;
        encoder.u32(1).map_err(invalid)?;
        encoder.u8(6).map_err(invalid)?;
        encoder.bytes(&ASSET, 32).map_err(invalid)?;
        encoder.u128(1000).map_err(invalid)?;
        encoder.bytes(&PAYER, 32).map_err(invalid)?;
        encoder.u128(5000).map_err(invalid)?;
        encoder.u128(4000).map_err(invalid)?;
        encoder.u64(1).map_err(invalid)?;
        encoder.bytes(recipient, 32).map_err(invalid)?;
        encoder.u128(0).map_err(invalid)?;
        encoder.u128(1000).map_err(invalid)?;
        encoder.bytes(&[0x66; 32], 32).map_err(invalid)?;
        encoder.bytes(&[0x67; 32], 32).map_err(invalid)?;
        encoder.bytes(&[0x68; 32], 32).map_err(invalid)?;
        encoder.u64(1000).map_err(invalid)?;
        encoder.u8(u8::from(signature.is_some())).map_err(invalid)?;
        if let Some(signature) = signature {
            encoder.bytes(&signature, 64).map_err(invalid)?;
        }
        Ok(encoder.finish())
    }

    fn settlement() -> Result<(BuiltPayment, SettlementResponse, AuthorizedBatch), X402Error> {
        let recipient = account_identifiers(ACCOUNT)?[0];
        let key = SigningKey::from_bytes(&[0x51; 32]);
        let unsigned = receipt(&recipient, None)?;
        let signature_digest = layerx_wire::hash::receipt_digest(&unsigned)
            .map_err(|_| X402Error::EvidenceMismatch)?;
        let canonical = receipt(&recipient, Some(key.sign(&signature_digest).to_bytes()))?;
        let digest = leaf_hash(&canonical).map_err(|_| X402Error::EvidenceMismatch)?;
        let mut payment = BuiltPayment {
            header: String::new(),
            payload: PaymentPayload {
                x402_version: X402_VERSION,
                resource: None,
                accepted: PaymentRequirements {
                    scheme: "exact".to_owned(),
                    network: "eip155:125".to_owned(),
                    amount: AtomicAmount::from_u128(1000),
                    asset: format!("0x{}", hex(&ASSET)),
                    pay_to: format!("0x{}", hex(&recipient)),
                    max_timeout_seconds: 90,
                    extra: Some(json!({"layerx": {"account": ACCOUNT, "currency": "LXP"}})),
                },
                payload: json!({}),
                extensions: BTreeMap::new(),
            },
            idempotency_key: [0x5c; 32],
        };
        payment.header = encode_header(&payment.payload)?;
        let response = SettlementResponse {
            success: true,
            error_reason: None,
            payer: Some(hex(&PAYER)),
            transaction: format!("0x{}", hex(&TRANSACTION)),
            network: payment.payload.accepted.network.clone(),
            amount: Some(payment.payload.accepted.amount),
            extensions: BTreeMap::from([(
                "layerx".to_owned(),
                json!({
                    "receipt": STANDARD.encode(&canonical),
                    "receiptDigest": hex(&digest),
                    "verificationLevel": "receipted",
                }),
            )]),
        };
        let batch = AuthorizedBatch::new(
            [0x65; 32],
            ASSET,
            [0x62; 32],
            [0x63; 32],
            key.verifying_key().to_bytes(),
        );
        Ok((payment, response, batch))
    }

    fn capture(
        payment: &BuiltPayment,
        response: &SettlementResponse,
        batch: &AuthorizedBatch,
    ) -> Result<CapturedSettlement, X402Error> {
        Buyer::capture_settlement(
            &encode_header(response)?,
            payment,
            batch,
            &TraceId::mint([0xab; 16]),
        )
        .map_err(Traced::into_error)
    }

    #[test]
    fn buyer_accepts_receipted_transaction_hash() -> Result<(), X402Error> {
        let (payment, response, batch) = settlement()?;
        let captured = capture(&payment, &response, &batch)?;
        assert_eq!(captured.response, response);
        assert_eq!(
            captured.response.transaction,
            format!("0x{}", hex(&TRANSACTION))
        );
        assert_eq!(
            captured.response.extensions["layerx"]["verificationLevel"],
            "receipted"
        );
        assert_eq!(
            response.extensions["layerx"]["receiptDigest"],
            json!(hex(&captured.receipt_digest))
        );
        Ok(())
    }

    #[test]
    fn buyer_refuses_lxp_form_and_malformed_hashes() -> Result<(), X402Error> {
        let (payment, mut response, batch) = settlement()?;
        for transaction in [
            "lxp:abc".to_owned(),
            format!("lxp:{}", hex(&TRANSACTION)),
            hex(&TRANSACTION),
            format!("0x{}", "61".repeat(31)),
            format!("0x{}", "61".repeat(33)),
            format!("0x{}", "ZZ".repeat(32)),
            format!("0x{}", "AB".repeat(32)),
        ] {
            response.transaction = transaction;
            assert_eq!(
                capture(&payment, &response, &batch),
                Err(X402Error::EvidenceMismatch)
            );
        }
        Ok(())
    }

    #[test]
    fn buyer_binds_hash_network_digest_and_signature() -> Result<(), X402Error> {
        let (payment, response, batch) = settlement()?;
        let mut changed = response.clone();
        changed.transaction = format!("0x{}", hex(&[0x71; 32]));
        assert_eq!(
            capture(&payment, &changed, &batch),
            Err(X402Error::EvidenceMismatch)
        );
        changed = response.clone();
        changed.network = "eip155:126".to_owned();
        assert_eq!(
            capture(&payment, &changed, &batch),
            Err(X402Error::EvidenceMismatch)
        );
        changed = response.clone();
        changed
            .extensions
            .get_mut("layerx")
            .ok_or(X402Error::EvidenceMissing)?["receiptDigest"] = json!(hex(&[0x72; 32]));
        assert_eq!(
            capture(&payment, &changed, &batch),
            Err(X402Error::EvidenceMismatch)
        );
        let wrong_key = SigningKey::from_bytes(&[0x52; 32]);
        let wrong_batch = AuthorizedBatch::new(
            batch.batch_id(),
            batch.asset(),
            batch.previous_state_root(),
            batch.resulting_state_root(),
            wrong_key.verifying_key().to_bytes(),
        );
        assert_eq!(
            capture(&payment, &response, &wrong_batch),
            Err(X402Error::EvidenceMismatch)
        );
        Ok(())
    }

    #[test]
    fn buyer_refuses_legacy_pending_and_unproved_committed_rungs() -> Result<(), X402Error> {
        let (payment, mut response, batch) = settlement()?;
        for rung in ["sequencer-signed", "submitted", "committed"] {
            response
                .extensions
                .get_mut("layerx")
                .ok_or(X402Error::EvidenceMissing)?["verificationLevel"] = json!(rung);
            assert_eq!(
                capture(&payment, &response, &batch),
                Err(X402Error::EvidenceMismatch)
            );
        }
        Ok(())
    }

    #[test]
    fn buyer_support_requires_canonical_eip155_network() -> Result<(), X402Error> {
        let kind = |network: &str| SupportedKind {
            scheme: "exact".to_owned(),
            network: network.to_owned(),
        };
        Buyer::new(vec![kind("eip155:125")])?;
        for network in [
            "layerx:beta",
            "eip155:0",
            "eip155:0125",
            "eip155:+125",
            "eip155:",
        ] {
            assert_eq!(
                Buyer::new(vec![kind(network)]).err(),
                Some(X402Error::UnsupportedOffer)
            );
        }
        Ok(())
    }
}
