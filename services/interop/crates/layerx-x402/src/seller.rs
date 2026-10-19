//! x402 v2 resource-server role. Issuance is pure edge translation; payment
//! execution crosses a typed plane boundary and can only become successful
//! after the gateway verifies a canonical `LayerX` receipt.

use std::collections::BTreeMap;

use base64::engine::general_purpose::STANDARD;
use base64::Engine as _;
use layerx_interop_gateway::gateway::{TranslationKind, TranslationRequest, TranslationStatus};
use layerx_interop_gateway::principal::PrincipalId;
use layerx_interop_gateway::trace::{TraceId, Traced};
use layerx_interop_gateway::GatewayCore;
use layerx_proof::receipt::{verify, AuthorizedBatch};
use serde_json::{json, Value};
use sha2::{Digest as _, Sha256};

use crate::codec::{decode_header, encode_header};
use crate::model::{
    AtomicAmount, PaymentPayload, PaymentRequired, PaymentRequirements, SettlementResponse,
    X402Error,
};

const ADAPTER_ID: &str = "x402";
const PAYMENT_DIGEST_DOMAIN: &[u8] = b"LayerX/x402/v2/payment\0";
const PAYMENT_KEY_DOMAIN: &[u8] = b"LayerX/x402/v2/idempotency\0";

/// Typed request handed to the plane's existing payment authority. The x402
/// adapter never constructs `LayerX` payload bytes; it passes meaning and the
/// exact external scheme payload to that authority.
#[derive(Clone, Debug, PartialEq)]
pub struct LayerXPaymentRequest {
    pub principal: PrincipalId,
    pub scheme: String,
    pub network: String,
    pub amount: AtomicAmount,
    pub asset: String,
    pub pay_to: String,
    pub scheme_payload: Value,
    pub idempotency_key: [u8; 32],
    pub request_digest: [u8; 32],
}

/// Evidence returned by the real plane boundary after an execution. The
/// adapter cannot construct this type without canonical receipt bytes and the
/// matching authorised batch used by the protocol verifier.
#[derive(Debug)]
pub struct ExecutedPayment {
    pub canonical_receipt: Vec<u8>,
    pub authorised_batch: AuthorizedBatch,
}

/// Honest outcome of the plane call. Pending and refused are first-class;
/// neither can be rendered as x402 settlement success.
#[derive(Debug)]
pub enum PlanePaymentOutcome {
    Pending,
    Refused { reason: &'static str },
    Executed(ExecutedPayment),
}

/// The only boundary through which the edge adapter may ask `LayerX` to pay.
pub trait PaymentPlane {
    /// Executes one typed request through the plane's payment authority.
    ///
    /// # Errors
    ///
    /// Returns a typed construction, policy or protocol refusal.
    fn execute(
        &mut self,
        request: LayerXPaymentRequest,
        trace: &TraceId,
    ) -> Result<PlanePaymentOutcome, X402Error>;
}

#[derive(Clone, Debug, PartialEq)]
pub struct PaymentRequiredSignal {
    pub status: u16,
    pub header: String,
    pub body: PaymentRequired,
}

#[derive(Clone, Debug, PartialEq)]
pub enum SellerOutcome {
    Pending,
    Refused {
        header: String,
        response: SettlementResponse,
    },
    Settled {
        header: String,
        response: SettlementResponse,
        receipt_digest: [u8; 32],
    },
}

/// x402 v2 seller bound to one validated payment-required declaration.
#[derive(Clone, Debug)]
pub struct Seller {
    required: PaymentRequired,
}

impl Seller {
    /// Creates a seller after validating its complete v2 offer.
    ///
    /// # Errors
    ///
    /// Refuses invalid versions, resource metadata, requirements or bounds.
    pub fn new(required: PaymentRequired) -> Result<Self, X402Error> {
        required.validate()?;
        for accepted in &required.accepts {
            validate_network(&accepted.network)?;
            accepted.layerx_terms()?;
        }
        Ok(Self { required })
    }

    /// Emits the HTTP x402 v2 payment-required signal.
    ///
    /// # Errors
    ///
    /// Returns an encoding or declared-size refusal.
    pub fn payment_required(&self) -> Result<PaymentRequiredSignal, X402Error> {
        Ok(PaymentRequiredSignal {
            status: 402,
            header: encode_header(&self.required)?,
            body: self.required.clone(),
        })
    }

    /// Decodes a `PAYMENT-SIGNATURE` header, binds it to the issued offer,
    /// executes it through the real plane authority and renders success only
    /// after the gateway verifies the returned canonical receipt.
    ///
    /// # Errors
    ///
    /// Returns a trace-bound typed refusal for malformed, mismatched or
    /// unverifiable payments and for gateway/plane failures.
    pub fn settle(
        &self,
        gateway: &mut GatewayCore,
        principal: &PrincipalId,
        payment_header: &str,
        plane: &mut impl PaymentPlane,
        trace: &TraceId,
        now: u64,
    ) -> Result<SellerOutcome, Traced<X402Error>> {
        let fail = |error| trace.wrap(error);
        let payload: PaymentPayload = decode_header(payment_header).map_err(fail)?;
        payload.validate().map_err(fail)?;
        let accepted = self.match_payload(&payload).map_err(fail)?;
        let canonical = serde_json::to_vec(&payload).map_err(|_| fail(X402Error::Encode))?;
        let request_digest = digest(PAYMENT_DIGEST_DOMAIN, &[&canonical]);
        let idempotency_key = digest(
            PAYMENT_KEY_DOMAIN,
            &[principal.as_str().as_bytes(), &request_digest],
        );
        let adapter = layerx_interop_gateway::adapter::AdapterId::new(ADAPTER_ID)
            .map_err(|error| fail(X402Error::Gateway(error.into())))?;
        let translation = TranslationRequest::new(
            adapter,
            TranslationKind::StateChanging,
            idempotency_key,
            request_digest,
        )
        .map_err(|error| fail(X402Error::Gateway(error)))?;
        let opened = gateway
            .begin_translation(principal, &translation, trace, now)
            .map_err(|error| trace.wrap(X402Error::Gateway(error.into_error())))?;
        if opened == TranslationStatus::Refused {
            return Self::refused(accepted, "payment_refused").map_err(fail);
        }
        let request = LayerXPaymentRequest {
            principal: principal.clone(),
            scheme: accepted.scheme.clone(),
            network: accepted.network.clone(),
            amount: accepted.amount,
            asset: accepted.asset.clone(),
            pay_to: accepted.pay_to.clone(),
            scheme_payload: payload.payload,
            idempotency_key,
            request_digest,
        };
        match plane.execute(request, trace).map_err(fail)? {
            PlanePaymentOutcome::Pending => Ok(SellerOutcome::Pending),
            PlanePaymentOutcome::Refused { reason } => {
                gateway
                    .refuse_translation(principal, idempotency_key, trace, now)
                    .map_err(|error| trace.wrap(X402Error::Gateway(error.into_error())))?;
                Self::refused(accepted, reason).map_err(fail)
            }
            PlanePaymentOutcome::Executed(executed) => Self::settled(
                gateway,
                principal,
                idempotency_key,
                accepted,
                &executed,
                trace,
                now,
            )
            .map_err(fail),
        }
    }

    fn match_payload<'a>(
        &'a self,
        payload: &PaymentPayload,
    ) -> Result<&'a PaymentRequirements, X402Error> {
        let accepted = self
            .required
            .accepts
            .iter()
            .find(|candidate| *candidate == &payload.accepted)
            .ok_or(X402Error::RequirementsMismatch)?;
        for (name, required) in &self.required.extensions {
            if payload.extensions.get(name) != Some(required) {
                return Err(X402Error::ExtensionsMismatch);
            }
        }
        Ok(accepted)
    }

    fn refused(accepted: &PaymentRequirements, reason: &str) -> Result<SellerOutcome, X402Error> {
        let response = SettlementResponse {
            success: false,
            error_reason: Some(safe_reason(reason)),
            payer: None,
            transaction: String::new(),
            network: accepted.network.clone(),
            amount: None,
            extensions: BTreeMap::new(),
        };
        response.validate_wire()?;
        let header = encode_header(&response)?;
        Ok(SellerOutcome::Refused { header, response })
    }

    #[allow(clippy::too_many_arguments)]
    fn settled(
        gateway: &mut GatewayCore,
        principal: &PrincipalId,
        idempotency_key: [u8; 32],
        accepted: &PaymentRequirements,
        executed: &ExecutedPayment,
        trace: &TraceId,
        now: u64,
    ) -> Result<SellerOutcome, X402Error> {
        let verified = verify(&executed.canonical_receipt, &executed.authorised_batch)
            .map_err(|_| X402Error::EvidenceMismatch)?;
        let protocol = verified
            .receipt()
            .protocol()
            .ok_or(X402Error::EvidenceMismatch)?;
        payment_matches(accepted, protocol.asset(), protocol.to(), protocol.amount())?;
        let purpose = grant_purpose(accepted)?;
        let status = gateway
            .settle_with_receipt(
                principal,
                idempotency_key,
                &executed.canonical_receipt,
                &executed.authorised_batch,
                trace,
                now,
            )
            .map_err(|error| X402Error::Gateway(error.into_error()))?;
        let TranslationStatus::ReceiptVerified { receipt_digest } = status else {
            return Err(X402Error::EvidenceMissing);
        };
        let digest_text = hex(&receipt_digest);
        let mut layerx = json!({
            "receipt": STANDARD.encode(&executed.canonical_receipt),
            "receiptDigest": digest_text,
            "verificationLevel": "receipted"
        });
        if let (Some(purpose), Some(terms)) = (purpose, layerx.as_object_mut()) {
            terms.insert("purposeHash".to_owned(), Value::String(purpose));
        }
        let mut extensions = BTreeMap::new();
        extensions.insert("layerx".to_owned(), layerx);
        let response = SettlementResponse {
            success: true,
            error_reason: None,
            payer: Some(hex(&protocol.from())),
            transaction: format!("0x{}", hex(&protocol.activity_id())),
            network: accepted.network.clone(),
            amount: Some(accepted.amount),
            extensions,
        };
        response.validate_wire()?;
        let header = encode_header(&response)?;
        Ok(SellerOutcome::Settled {
            header,
            response,
            receipt_digest,
        })
    }
}

pub(crate) fn validate_network(network: &str) -> Result<(), X402Error> {
    let chain_id = network
        .strip_prefix("eip155:")
        .ok_or(X402Error::UnsupportedOffer)?;
    if chain_id.is_empty()
        || chain_id.len() > 64
        || chain_id.starts_with('0')
        || !chain_id.bytes().all(|byte| byte.is_ascii_digit())
    {
        return Err(X402Error::UnsupportedOffer);
    }
    Ok(())
}

pub(crate) fn validate_transaction(transaction: &str) -> Result<(), X402Error> {
    let hash = transaction
        .strip_prefix("0x")
        .ok_or(X402Error::InvalidPayload)?;
    if hash.len() != 64
        || !hash.bytes().all(|byte| byte.is_ascii_hexdigit())
        || hash.bytes().all(|byte| byte == b'0')
    {
        return Err(X402Error::InvalidPayload);
    }
    Ok(())
}

pub(crate) fn payment_matches(
    accepted: &PaymentRequirements,
    asset: [u8; 32],
    recipient: [u8; 32],
    amount: u128,
) -> Result<(), X402Error> {
    validate_network(&accepted.network)?;
    accepted.validate()?;
    accepted.layerx_terms()?;
    if !matches_identifier(&accepted.asset, &asset)
        || !matches_identifier(&accepted.pay_to, &recipient)
        || accepted.amount.value() != amount
    {
        return Err(X402Error::EvidenceMismatch);
    }
    Ok(())
}

fn matches_identifier(value: &str, identifier: &[u8; 32]) -> bool {
    let digits = match value.strip_prefix("0x") {
        Some(digits) => digits,
        None => value,
    };
    digits.eq_ignore_ascii_case(&hex(identifier))
}

/// The challenge's `purposeHash` a grant settlement repeats: `None` for an
/// exact offer, the offer's 64 lowercase hexadecimal characters for a metered
/// or subscription offer, and a refusal when a grant offer carries none.
fn grant_purpose(accepted: &PaymentRequirements) -> Result<Option<String>, X402Error> {
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
        .ok_or(X402Error::ProfileMissing)
}

fn digest(domain: &[u8], parts: &[&[u8]]) -> [u8; 32] {
    let mut hash = Sha256::new();
    hash.update(domain);
    for part in parts {
        hash.update(part);
    }
    hash.finalize().into()
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

fn safe_reason(reason: &str) -> String {
    let valid = !reason.is_empty()
        && reason.len() <= 64
        && reason
            .bytes()
            .all(|byte| byte.is_ascii_lowercase() || byte == b'_');
    if valid {
        reason.to_owned()
    } else {
        "payment_refused".to_owned()
    }
}

#[cfg(test)]
pub(crate) mod tests {
    use super::*;
    use ed25519_dalek::{Signer as _, SigningKey};
    use layerx_interop_gateway::adapter::{AdapterId, ConformanceSuite};
    use layerx_wire::encode::Encoder;
    use layerx_wire::limits::PROTOCOL_VERSION;

    pub(crate) fn required() -> PaymentRequired {
        let vectors: Value = serde_json::from_str(include_str!(
            "../../../specs/conformance/x402/payment-required.json"
        ))
        .unwrap_or_else(|error| panic!("payment vectors: {error}"));
        let mut required: PaymentRequired = serde_json::from_value(vectors[0]["document"].clone())
            .unwrap_or_else(|error| panic!("payment required: {error}"));
        required.accepts[0].network = "eip155:125".to_owned();
        required.accepts[0].asset = format!("0x{}", hex(&[0xab; 32]));
        required
    }

    fn encoded_receipt(recipient: [u8; 32], signature: Option<[u8; 64]>) -> Vec<u8> {
        let mut encoder = Encoder::new(4096);
        assert_eq!(
            encoder.structure_header_version(0x5201, PROTOCOL_VERSION),
            Ok(())
        );
        assert_eq!(encoder.u16(PROTOCOL_VERSION), Ok(()));
        assert_eq!(encoder.bytes(&[0x61; 32], 32), Ok(()));
        assert_eq!(encoder.u64(9), Ok(()));
        assert_eq!(encoder.bytes(&[0x62; 32], 32), Ok(()));
        assert_eq!(encoder.bytes(&[0x63; 32], 32), Ok(()));
        assert_eq!(encoder.bytes(&[0x64; 32], 32), Ok(()));
        assert_eq!(encoder.i32(0), Ok(()));
        assert_eq!(encoder.sequence_length(0, 512), Ok(()));
        assert_eq!(encoder.u128(1), Ok(()));
        assert_eq!(encoder.bytes(&[0x65; 32], 32), Ok(()));
        assert_eq!(encoder.u16(1), Ok(()));
        assert_eq!(encoder.u32(1), Ok(()));
        assert_eq!(encoder.u32(1), Ok(()));
        assert_eq!(encoder.u8(1), Ok(()));
        assert_eq!(encoder.bytes(&[0xab; 32], 32), Ok(()));
        assert_eq!(encoder.u128(1000), Ok(()));
        assert_eq!(encoder.bytes(&[0x71; 32], 32), Ok(()));
        assert_eq!(encoder.u128(5000), Ok(()));
        assert_eq!(encoder.u128(4000), Ok(()));
        assert_eq!(encoder.u64(1), Ok(()));
        assert_eq!(encoder.bytes(&recipient, 32), Ok(()));
        assert_eq!(encoder.u128(0), Ok(()));
        assert_eq!(encoder.u128(1000), Ok(()));
        assert_eq!(encoder.bytes(&[0x66; 32], 32), Ok(()));
        assert_eq!(encoder.bytes(&[0x67; 32], 32), Ok(()));
        assert_eq!(encoder.bytes(&[0x68; 32], 32), Ok(()));
        assert_eq!(encoder.u64(1000), Ok(()));
        assert_eq!(encoder.u8(u8::from(signature.is_some())), Ok(()));
        if let Some(signature) = signature {
            assert_eq!(encoder.bytes(&signature, 64), Ok(()));
        }
        encoder.finish()
    }

    pub(crate) fn executed(accepted: &PaymentRequirements) -> ExecutedPayment {
        let terms = accepted
            .layerx_terms()
            .unwrap_or_else(|error| panic!("payment terms: {error}"));
        let recipient = crate::model::account_identifiers(&terms.account)
            .unwrap_or_else(|error| panic!("account identifiers: {error}"))[0];
        let key = SigningKey::from_bytes(&[0x72; 32]);
        let unsigned = encoded_receipt(recipient, None);
        let digest = layerx_wire::hash::receipt_digest(&unsigned)
            .unwrap_or_else(|error| panic!("receipt digest: {error:?}"));
        ExecutedPayment {
            canonical_receipt: encoded_receipt(recipient, Some(key.sign(&digest).to_bytes())),
            authorised_batch: AuthorizedBatch::new(
                [0x65; 32],
                [0xab; 32],
                [0x62; 32],
                [0x63; 32],
                key.verifying_key().to_bytes(),
            ),
        }
    }

    pub(crate) fn opened_gateway() -> (GatewayCore, PrincipalId, TraceId) {
        let mut gateway = GatewayCore::new();
        let principal =
            PrincipalId::new("x402-payee").unwrap_or_else(|error| panic!("principal: {error}"));
        let trace = TraceId::mint([0x73; 16]);
        let suite = ConformanceSuite::new(
            AdapterId::new("x402-v2").unwrap_or_else(|error| panic!("suite id: {error}")),
            1,
            Sha256::digest(include_bytes!(
                "../../../specs/conformance/x402/payment-required.json"
            ))
            .into(),
        )
        .unwrap_or_else(|error| panic!("suite: {error}"));
        let descriptor = crate::x402_adapter_descriptor(suite)
            .unwrap_or_else(|error| panic!("descriptor: {error}"));
        assert_eq!(
            gateway
                .register_adapter(descriptor, &trace, 0)
                .map_err(Traced::into_error),
            Ok(())
        );
        let translation = TranslationRequest::new(
            AdapterId::new("x402").unwrap_or_else(|error| panic!("adapter: {error}")),
            TranslationKind::StateChanging,
            [0x74; 32],
            [0x75; 32],
        )
        .unwrap_or_else(|error| panic!("translation: {error}"));
        assert_eq!(
            gateway
                .begin_translation(&principal, &translation, &trace, 0)
                .map_err(Traced::into_error),
            Ok(TranslationStatus::Pending)
        );
        (gateway, principal, trace)
    }

    #[test]
    fn settlement_transaction_is_tx_hash() {
        let required = required();
        let accepted = &required.accepts[0];
        let executed = executed(accepted);
        let (mut gateway, principal, trace) = opened_gateway();
        let outcome = Seller::settled(
            &mut gateway,
            &principal,
            [0x74; 32],
            accepted,
            &executed,
            &trace,
            1,
        )
        .unwrap_or_else(|error| panic!("settlement: {error}"));
        let SellerOutcome::Settled {
            header,
            response,
            receipt_digest,
        } = outcome
        else {
            panic!("settled response required");
        };
        assert_eq!(response.transaction, format!("0x{}", "61".repeat(32)));
        assert_eq!(response.network, "eip155:125");
        assert_eq!(
            response.extensions["layerx"]["verificationLevel"],
            "receipted"
        );
        assert_eq!(
            response.extensions["layerx"]["receiptDigest"],
            hex(&receipt_digest)
        );
        assert_ne!(response.transaction, format!("0x{}", hex(&receipt_digest)));
        assert_eq!(decode_header::<SettlementResponse>(&header), Ok(response));
    }

    #[test]
    fn network_is_eip155() {
        let seller = Seller::new(required()).unwrap_or_else(|error| panic!("seller: {error}"));
        let signal = seller
            .payment_required()
            .unwrap_or_else(|error| panic!("payment required: {error}"));
        assert_eq!(signal.body.accepts[0].network, "eip155:125");
        assert_eq!(
            decode_header::<PaymentRequired>(&signal.header),
            Ok(signal.body)
        );
        for (network, error) in [
            ("layerx:beta", X402Error::UnsupportedOffer),
            ("eip155:", X402Error::InvalidRequirements),
            ("eip155:0125", X402Error::UnsupportedOffer),
            ("eip155:0", X402Error::UnsupportedOffer),
            ("eip155:abc", X402Error::UnsupportedOffer),
        ] {
            let mut required = required();
            required.accepts[0].network = network.to_owned();
            assert_eq!(Seller::new(required).err(), Some(error));
        }
    }

    #[test]
    fn payment_facts_remain_bound_to_the_offer() {
        let required = required();
        let accepted = &required.accepts[0];
        let terms = accepted
            .layerx_terms()
            .unwrap_or_else(|error| panic!("payment terms: {error}"));
        let recipient = crate::model::account_identifiers(&terms.account)
            .unwrap_or_else(|error| panic!("account identifiers: {error}"))[0];
        assert_eq!(
            payment_matches(accepted, [0xab; 32], recipient, 1000),
            Ok(())
        );
        assert_eq!(
            payment_matches(accepted, [0xcd; 32], recipient, 1000),
            Err(X402Error::EvidenceMismatch)
        );
        assert_eq!(
            payment_matches(accepted, [0xab; 32], [0x76; 32], 1000),
            Err(X402Error::EvidenceMismatch)
        );
        assert_eq!(
            payment_matches(accepted, [0xab; 32], recipient, 1001),
            Err(X402Error::EvidenceMismatch)
        );
        let mut missing = accepted.clone();
        missing.extra = None;
        assert_eq!(
            payment_matches(&missing, [0xab; 32], recipient, 1000),
            Err(X402Error::ProfileMissing)
        );
        let mut bare = accepted.clone();
        bare.asset = hex(&[0xab; 32]).to_uppercase();
        bare.pay_to = hex(&recipient).to_uppercase();
        assert_eq!(payment_matches(&bare, [0xab; 32], recipient, 1000), Ok(()));
    }
}
