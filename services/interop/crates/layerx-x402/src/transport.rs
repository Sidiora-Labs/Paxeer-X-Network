//! Transport representation for the transport-independent x402 v2 models.
//! HTTP uses the standard base64 headers for the three payment messages;
//! MCP and A2A carry the same validated JSON objects without changing their
//! payment semantics.

use serde::de::DeserializeOwned;
use serde::Serialize;
use serde_json::Value;

use crate::codec::{decode_header, encode_header};
use crate::facilitator::{
    validate_supported, validate_verify, FacilitatorRequest, SupportedResponse, VerifyResponse,
};
use crate::model::{
    PaymentPayload, PaymentRequired, SettlementResponse, X402Error, PAYMENT_REQUIRED_HEADER,
    PAYMENT_RESPONSE_HEADER, PAYMENT_SIGNATURE_HEADER,
};

const MAXIMUM_JSON_BYTES: usize = 64 * 1_024;

/// The three supported x402 v2 transport bindings.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum TransportKind {
    Http,
    Mcp,
    A2a,
}

/// A transport representation of an unchanged x402 core value.
#[derive(Clone, Debug, PartialEq)]
pub enum TransportValue {
    HttpHeader { name: &'static str, value: String },
    Json(Value),
}

/// One published compatibility-matrix row.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct TransportCapability {
    pub transport: TransportKind,
    pub buyer: bool,
    pub seller: bool,
    pub facilitator: bool,
}

/// x402 v2 buyer, seller and facilitator representations are available over
/// every required transport. HTTP facilitator calls use JSON REST bodies;
/// the payment signals use their normative headers.
pub const TRANSPORT_MATRIX: [TransportCapability; 3] = [
    TransportCapability {
        transport: TransportKind::Http,
        buyer: true,
        seller: true,
        facilitator: true,
    },
    TransportCapability {
        transport: TransportKind::Mcp,
        buyer: true,
        seller: true,
        facilitator: true,
    },
    TransportCapability {
        transport: TransportKind::A2a,
        buyer: true,
        seller: true,
        facilitator: true,
    },
];

/// Encodes a validated payment-required signal.
///
/// # Errors
///
/// Refuses invalid values, oversized encodings, or serialization failures.
pub fn encode_payment_required(
    transport: TransportKind,
    value: &PaymentRequired,
) -> Result<TransportValue, X402Error> {
    value.validate()?;
    encode_core(transport, PAYMENT_REQUIRED_HEADER, value)
}

/// Decodes and validates a payment-required signal.
///
/// # Errors
///
/// Refuses representation mismatches, malformed encodings, and invalid values.
pub fn decode_payment_required(
    transport: TransportKind,
    value: &TransportValue,
) -> Result<PaymentRequired, X402Error> {
    let decoded: PaymentRequired = decode_core(transport, PAYMENT_REQUIRED_HEADER, value)?;
    decoded.validate()?;
    Ok(decoded)
}

/// Encodes a validated payment payload.
///
/// # Errors
///
/// Refuses invalid values, oversized encodings, or serialization failures.
pub fn encode_payment_payload(
    transport: TransportKind,
    value: &PaymentPayload,
) -> Result<TransportValue, X402Error> {
    value.validate()?;
    encode_core(transport, PAYMENT_SIGNATURE_HEADER, value)
}

/// Decodes and validates a payment payload.
///
/// # Errors
///
/// Refuses representation mismatches, malformed encodings, and invalid values.
pub fn decode_payment_payload(
    transport: TransportKind,
    value: &TransportValue,
) -> Result<PaymentPayload, X402Error> {
    let decoded: PaymentPayload = decode_core(transport, PAYMENT_SIGNATURE_HEADER, value)?;
    decoded.validate()?;
    Ok(decoded)
}

/// Encodes a validated settlement response.
///
/// # Errors
///
/// Refuses invalid values, oversized encodings, or serialization failures.
pub fn encode_settlement(
    transport: TransportKind,
    value: &SettlementResponse,
) -> Result<TransportValue, X402Error> {
    value.validate_wire()?;
    encode_core(transport, PAYMENT_RESPONSE_HEADER, value)
}

/// Decodes and validates a settlement response.
///
/// # Errors
///
/// Refuses representation mismatches, malformed encodings, and invalid values.
pub fn decode_settlement(
    transport: TransportKind,
    value: &TransportValue,
) -> Result<SettlementResponse, X402Error> {
    let decoded: SettlementResponse = decode_core(transport, PAYMENT_RESPONSE_HEADER, value)?;
    decoded.validate_wire()?;
    Ok(decoded)
}

/// Encodes the standard facilitator request. Its HTTP representation is a
/// JSON REST body, not one of the three payment headers.
///
/// # Errors
///
/// Refuses invalid requests, oversized encodings, or serialization failures.
pub fn encode_facilitator_request(
    _transport: TransportKind,
    value: &FacilitatorRequest,
) -> Result<TransportValue, X402Error> {
    value.validate()?;
    encode_json(value)
}

/// Decodes and validates a standard facilitator request body.
///
/// # Errors
///
/// Refuses non-JSON representations, malformed encodings, and invalid values.
pub fn decode_facilitator_request(
    _transport: TransportKind,
    value: &TransportValue,
) -> Result<FacilitatorRequest, X402Error> {
    let decoded: FacilitatorRequest = decode_json(value)?;
    decoded.validate()?;
    Ok(decoded)
}

/// Encodes a validated facilitator verification response.
///
/// # Errors
///
/// Refuses invalid responses, oversized encodings, or serialization failures.
pub fn encode_verify_response(
    _transport: TransportKind,
    value: &VerifyResponse,
) -> Result<TransportValue, X402Error> {
    validate_verify(value)?;
    encode_json(value)
}

/// Decodes and validates a facilitator verification response.
///
/// # Errors
///
/// Refuses non-JSON representations, malformed encodings, and invalid values.
pub fn decode_verify_response(
    _transport: TransportKind,
    value: &TransportValue,
) -> Result<VerifyResponse, X402Error> {
    let decoded: VerifyResponse = decode_json(value)?;
    validate_verify(&decoded)?;
    Ok(decoded)
}

/// Encodes a validated facilitator settlement as an endpoint response body.
///
/// # Errors
///
/// Refuses invalid responses, oversized encodings, or serialization failures.
pub fn encode_facilitator_settlement(
    _transport: TransportKind,
    value: &SettlementResponse,
) -> Result<TransportValue, X402Error> {
    value.validate_wire()?;
    encode_json(value)
}

/// Decodes and validates a facilitator settlement response body.
///
/// # Errors
///
/// Refuses non-JSON representations, malformed encodings, and invalid values.
pub fn decode_facilitator_settlement(
    _transport: TransportKind,
    value: &TransportValue,
) -> Result<SettlementResponse, X402Error> {
    let decoded: SettlementResponse = decode_json(value)?;
    decoded.validate_wire()?;
    Ok(decoded)
}

/// Encodes a validated facilitator support declaration.
///
/// # Errors
///
/// Refuses invalid declarations, oversized encodings, or serialization failures.
pub fn encode_supported_response(
    _transport: TransportKind,
    value: &SupportedResponse,
) -> Result<TransportValue, X402Error> {
    validate_supported(value)?;
    encode_json(value)
}

/// Decodes and validates a facilitator support declaration.
///
/// # Errors
///
/// Refuses non-JSON representations, malformed encodings, and invalid values.
pub fn decode_supported_response(
    _transport: TransportKind,
    value: &TransportValue,
) -> Result<SupportedResponse, X402Error> {
    let decoded: SupportedResponse = decode_json(value)?;
    validate_supported(&decoded)?;
    Ok(decoded)
}

fn encode_core<T: Serialize>(
    transport: TransportKind,
    header: &'static str,
    value: &T,
) -> Result<TransportValue, X402Error> {
    match transport {
        TransportKind::Http => Ok(TransportValue::HttpHeader {
            name: header,
            value: encode_header(value)?,
        }),
        TransportKind::Mcp | TransportKind::A2a => encode_json(value),
    }
}

fn decode_core<T: DeserializeOwned>(
    transport: TransportKind,
    header: &'static str,
    value: &TransportValue,
) -> Result<T, X402Error> {
    match (transport, value) {
        (
            TransportKind::Http,
            TransportValue::HttpHeader {
                name,
                value: encoded,
            },
        ) if *name == header => decode_header(encoded),
        (TransportKind::Mcp | TransportKind::A2a, TransportValue::Json(value)) => {
            serde_json::from_value(value.clone()).map_err(|_| X402Error::Decode)
        }
        _ => Err(X402Error::Decode),
    }
}

fn encode_json<T: Serialize>(value: &T) -> Result<TransportValue, X402Error> {
    let bytes = serde_json::to_vec(value).map_err(|_| X402Error::Encode)?;
    if bytes.len() > MAXIMUM_JSON_BYTES {
        return Err(X402Error::Bounds);
    }
    let value = serde_json::from_slice(&bytes).map_err(|_| X402Error::Encode)?;
    Ok(TransportValue::Json(value))
}

fn decode_json<T: DeserializeOwned>(value: &TransportValue) -> Result<T, X402Error> {
    let TransportValue::Json(value) = value else {
        return Err(X402Error::Decode);
    };
    let bytes = serde_json::to_vec(value).map_err(|_| X402Error::Decode)?;
    if bytes.len() > MAXIMUM_JSON_BYTES {
        return Err(X402Error::Bounds);
    }
    serde_json::from_slice(&bytes).map_err(|_| X402Error::Decode)
}
