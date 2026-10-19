use base64::engine::general_purpose::STANDARD;
use base64::Engine as _;
use serde::de::DeserializeOwned;
use serde::Serialize;

use crate::model::X402Error;

const MAXIMUM_HEADER_BYTES: usize = 64 * 1_024;

pub(crate) fn encode_header<T: Serialize>(value: &T) -> Result<String, X402Error> {
    let json = serde_json::to_vec(value).map_err(|_| X402Error::Encode)?;
    if json.len() > MAXIMUM_HEADER_BYTES {
        return Err(X402Error::Bounds);
    }
    Ok(STANDARD.encode(json))
}

pub(crate) fn decode_header<T: DeserializeOwned>(value: &str) -> Result<T, X402Error> {
    if value.is_empty() || value.len() > MAXIMUM_HEADER_BYTES.saturating_mul(2) {
        return Err(X402Error::Bounds);
    }
    let decoded = STANDARD
        .decode(value.as_bytes())
        .map_err(|_| X402Error::Decode)?;
    if decoded.len() > MAXIMUM_HEADER_BYTES {
        return Err(X402Error::Bounds);
    }
    serde_json::from_slice(&decoded).map_err(|_| X402Error::Decode)
}
