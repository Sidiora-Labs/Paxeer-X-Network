//! Hexadecimal and base64 encoding, and constant-time comparison, shared by the
//! signature scheme, the cursor codec and the durable delivery records.
//!
//! The base64 alphabet is the standard one with mandatory padding, which is the
//! exact form every shipped `LayerX` webhook consumer accepts.

use sha2::{Digest, Sha256};

use crate::error::WebhookError;

/// Encodes bytes as lowercase hexadecimal.
#[must_use]
pub fn hex_encode(bytes: &[u8]) -> String {
    const DIGITS: &[u8; 16] = b"0123456789abcdef";
    let mut encoded = String::with_capacity(bytes.len() * 2);
    for byte in bytes {
        encoded.push(char::from(DIGITS[usize::from(byte >> 4)]));
        encoded.push(char::from(DIGITS[usize::from(byte & 0x0f)]));
    }
    encoded
}

/// Decodes lowercase or uppercase hexadecimal into bytes.
///
/// # Errors
/// Returns [`WebhookError::InvalidRequest`] when the input is not hexadecimal.
pub fn hex_decode(encoded: &str) -> Result<Vec<u8>, WebhookError> {
    if !encoded.len().is_multiple_of(2) {
        return Err(WebhookError::InvalidRequest);
    }
    encoded
        .as_bytes()
        .chunks_exact(2)
        .map(|pair| {
            let pair = std::str::from_utf8(pair).map_err(|_| WebhookError::InvalidRequest)?;
            u8::from_str_radix(pair, 16).map_err(|_| WebhookError::InvalidRequest)
        })
        .collect()
}

/// Decodes hexadecimal into an exact byte width.
///
/// # Errors
/// Returns [`WebhookError::InvalidRequest`] when the input is not hexadecimal
/// or does not decode to exactly `N` bytes.
pub fn fixed_hex<const N: usize>(encoded: &str) -> Result<[u8; N], WebhookError> {
    hex_decode(encoded)?
        .try_into()
        .map_err(|_: Vec<u8>| WebhookError::InvalidRequest)
}

/// Returns true when the value is exactly 64 lowercase hexadecimal characters.
#[must_use]
pub fn is_digest(value: &str) -> bool {
    value.len() == 64
        && value
            .bytes()
            .all(|byte| matches!(byte, b'0'..=b'9' | b'a'..=b'f'))
}

pub(crate) fn digest(bytes: &[u8]) -> [u8; 32] {
    Sha256::digest(bytes).into()
}

const BASE64_ALPHABET: &[u8; 64] =
    b"ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";

fn base64_digit(value: u32) -> char {
    let index = usize::try_from(value & 63).unwrap_or(0);
    char::from(BASE64_ALPHABET[index.min(63)])
}

/// Encodes bytes as standard padded base64.
#[must_use]
pub fn base64_encode(bytes: &[u8]) -> String {
    let mut encoded = String::with_capacity(bytes.len().div_ceil(3).saturating_mul(4));
    for chunk in bytes.chunks(3) {
        let first = u32::from(chunk.first().copied().unwrap_or(0));
        let second = u32::from(chunk.get(1).copied().unwrap_or(0));
        let third = u32::from(chunk.get(2).copied().unwrap_or(0));
        let triple = (first << 16) | (second << 8) | third;
        encoded.push(base64_digit(triple >> 18));
        encoded.push(base64_digit(triple >> 12));
        if chunk.len() > 1 {
            encoded.push(base64_digit(triple >> 6));
        } else {
            encoded.push('=');
        }
        if chunk.len() > 2 {
            encoded.push(base64_digit(triple));
        } else {
            encoded.push('=');
        }
    }
    encoded
}

/// Decodes standard padded base64.
///
/// # Errors
/// Returns [`WebhookError::InvalidRequest`] when the input is empty, is not a
/// whole number of quanta, carries more than two padding characters, or holds a
/// character outside the standard alphabet.
pub fn base64_decode(encoded: &str) -> Result<Vec<u8>, WebhookError> {
    if encoded.is_empty() || !encoded.len().is_multiple_of(4) {
        return Err(WebhookError::InvalidRequest);
    }
    let body = encoded.trim_end_matches('=');
    if encoded.len().saturating_sub(body.len()) > 2 {
        return Err(WebhookError::InvalidRequest);
    }
    let mut decoded = Vec::with_capacity(body.len().saturating_mul(3) / 4);
    let mut accumulator = 0_u32;
    let mut bits = 0_u32;
    for byte in body.bytes() {
        let sextet = match byte {
            b'A'..=b'Z' => byte.wrapping_sub(b'A'),
            b'a'..=b'z' => byte.wrapping_sub(b'a').wrapping_add(26),
            b'0'..=b'9' => byte.wrapping_sub(b'0').wrapping_add(52),
            b'+' => 62,
            b'/' => 63,
            _ => return Err(WebhookError::InvalidRequest),
        };
        accumulator = ((accumulator << 6) | u32::from(sextet)) & 0xffff;
        bits = bits.saturating_add(6);
        if bits >= 8 {
            bits = bits.saturating_sub(8);
            decoded.push(u8::try_from((accumulator >> bits) & 0xff).unwrap_or(0));
        }
    }
    Ok(decoded)
}

/// Decodes standard padded base64 into an exact byte width.
///
/// # Errors
/// Returns [`WebhookError::InvalidRequest`] when the input is not base64 or does
/// not decode to exactly `N` bytes.
pub fn fixed_base64<const N: usize>(encoded: &str) -> Result<[u8; N], WebhookError> {
    base64_decode(encoded)?
        .try_into()
        .map_err(|_: Vec<u8>| WebhookError::InvalidRequest)
}
