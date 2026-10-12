//! EIP-1559 call signing for the one transaction an account sends itself:
//! binding its `LayerX` identity through the `addr` precompile.
#![cfg(feature = "custody")]

use k256::ecdsa::{RecoveryId, Signature, SigningKey, VerifyingKey};
use sha3::{Digest as _, Keccak256};

use crate::secp256k1::{evm_address, verify_recoverable_digest};
use crate::signer::AgentSignature;

/// An unsigned EIP-1559 call carrying no value and no access list.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Eip1559Call {
    pub chain_id: u64,
    pub nonce: u64,
    pub max_priority_fee_per_gas: u128,
    pub max_fee_per_gas: u128,
    pub gas_limit: u64,
    pub to: [u8; 20],
    pub data: Vec<u8>,
}

/// The private key is not a usable secp256k1 scalar.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct InvalidEvmKey;

impl std::fmt::Display for InvalidEvmKey {
    fn fmt(&self, formatter: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        formatter.write_str("invalid_evm_key")
    }
}

impl std::error::Error for InvalidEvmKey {}

/// Refusal of a raw signed EIP-1559 call or of its signature.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum EvmTransactionError {
    /// The input is empty or its envelope type byte is not 2.
    NotEip1559,
    /// The RLP is truncated, non-minimal, has the wrong field count, or is
    /// followed by trailing bytes.
    MalformedRlp,
    /// A field has the wrong kind or width, or the call carries value or an
    /// access list.
    UnsupportedField,
    /// The decoded call does not re-encode to the input byte for byte.
    NonCanonical,
    /// The signature is not canonical or recovers no sender.
    BadSignature,
}

impl std::fmt::Display for EvmTransactionError {
    fn fmt(&self, formatter: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        formatter.write_str(match self {
            Self::NotEip1559 => "not_eip1559",
            Self::MalformedRlp => "malformed_rlp",
            Self::UnsupportedField => "unsupported_field",
            Self::NonCanonical => "non_canonical",
            Self::BadSignature => "bad_signature",
        })
    }
}

impl std::error::Error for EvmTransactionError {}

fn rlp_length(length: usize, short: u8, out: &mut Vec<u8>) {
    if length < 56 {
        #[allow(clippy::cast_possible_truncation)]
        out.push(short + length as u8);
    } else {
        let bytes = length.to_be_bytes();
        let skip = bytes.iter().take_while(|byte| **byte == 0).count();
        #[allow(clippy::cast_possible_truncation)]
        out.push(short + 55 + (bytes.len() - skip) as u8);
        out.extend_from_slice(&bytes[skip..]);
    }
}

fn rlp_bytes(bytes: &[u8], out: &mut Vec<u8>) {
    if bytes.len() == 1 && bytes[0] < 0x80 {
        out.push(bytes[0]);
    } else {
        rlp_length(bytes.len(), 0x80, out);
        out.extend_from_slice(bytes);
    }
}

fn rlp_integer(bytes: &[u8], out: &mut Vec<u8>) {
    let skip = bytes.iter().take_while(|byte| **byte == 0).count();
    rlp_bytes(&bytes[skip..], out);
}

fn rlp_list(payload: &[u8]) -> Vec<u8> {
    let mut out = Vec::with_capacity(payload.len() + 9);
    rlp_length(payload.len(), 0xc0, &mut out);
    out.extend_from_slice(payload);
    out
}

enum Item<'a> {
    String(&'a [u8]),
    List(&'a [u8]),
}

/// Takes one canonically encoded RLP item from the front of `input`.
fn take_item<'a>(input: &mut &'a [u8]) -> Result<Item<'a>, EvmTransactionError> {
    let Some((&prefix, rest)) = input.split_first() else {
        return Err(EvmTransactionError::MalformedRlp);
    };
    if prefix < 0x80 {
        let (item, rest) = input.split_at(1);
        *input = rest;
        return Ok(Item::String(item));
    }
    let (list, short) = if prefix < 0xc0 {
        (false, prefix - 0x80)
    } else {
        (true, prefix - 0xc0)
    };
    let (length, rest) = if short < 56 {
        (usize::from(short), rest)
    } else {
        let width = usize::from(short - 55);
        let Some((length_bytes, rest)) = rest.split_at_checked(width) else {
            return Err(EvmTransactionError::MalformedRlp);
        };
        if width > size_of::<usize>() || length_bytes.first() == Some(&0) {
            return Err(EvmTransactionError::MalformedRlp);
        }
        let length = length_bytes
            .iter()
            .fold(0_usize, |length, byte| (length << 8) | usize::from(*byte));
        if length < 56 {
            return Err(EvmTransactionError::MalformedRlp);
        }
        (length, rest)
    };
    let Some((payload, rest)) = rest.split_at_checked(length) else {
        return Err(EvmTransactionError::MalformedRlp);
    };
    *input = rest;
    if list {
        return Ok(Item::List(payload));
    }
    if let [byte] = payload {
        if *byte < 0x80 {
            return Err(EvmTransactionError::MalformedRlp);
        }
    }
    Ok(Item::String(payload))
}

fn take_string<'a>(input: &mut &'a [u8]) -> Result<&'a [u8], EvmTransactionError> {
    match take_item(input)? {
        Item::String(bytes) => Ok(bytes),
        Item::List(_) => Err(EvmTransactionError::UnsupportedField),
    }
}

/// Takes one minimal big-endian unsigned integer of at most `N` bytes.
fn take_uint<const N: usize>(input: &mut &[u8]) -> Result<[u8; N], EvmTransactionError> {
    let bytes = take_string(input)?;
    if bytes.first() == Some(&0) {
        return Err(EvmTransactionError::MalformedRlp);
    }
    let Some(offset) = N.checked_sub(bytes.len()) else {
        return Err(EvmTransactionError::UnsupportedField);
    };
    let mut value = [0_u8; N];
    value[offset..].copy_from_slice(bytes);
    Ok(value)
}

impl Eip1559Call {
    fn fields(&self) -> Vec<u8> {
        let mut out = Vec::with_capacity(self.data.len() + 96);
        rlp_integer(&self.chain_id.to_be_bytes(), &mut out);
        rlp_integer(&self.nonce.to_be_bytes(), &mut out);
        rlp_integer(&self.max_priority_fee_per_gas.to_be_bytes(), &mut out);
        rlp_integer(&self.max_fee_per_gas.to_be_bytes(), &mut out);
        rlp_integer(&self.gas_limit.to_be_bytes(), &mut out);
        rlp_bytes(&self.to, &mut out);
        rlp_integer(&[], &mut out);
        rlp_bytes(&self.data, &mut out);
        out.push(0xc0);
        out
    }

    /// The digest the sender signs.
    #[must_use]
    pub fn signing_hash(&self) -> [u8; 32] {
        let mut hasher = Keccak256::new();
        hasher.update([0x02]);
        hasher.update(rlp_list(&self.fields()));
        hasher.finalize().into()
    }

    /// Signs the call, yielding the bytes `eth_sendRawTransaction` takes.
    ///
    /// # Errors
    ///
    /// Refuses a key that is zero or not below the group order.
    pub fn sign(&self, evm_secret: &[u8; 32]) -> Result<Vec<u8>, InvalidEvmKey> {
        let key = SigningKey::from_slice(evm_secret).map_err(|_| InvalidEvmKey)?;
        let (signature, recovery) = key
            .sign_prehash_recoverable(&self.signing_hash())
            .map_err(|_| InvalidEvmKey)?;
        let mut bytes = [0_u8; 65];
        bytes[..64].copy_from_slice(&signature.to_bytes());
        bytes[64] = 27 + recovery.to_byte();
        let signature = AgentSignature::new(bytes).map_err(|_| InvalidEvmKey)?;
        Ok(assemble(self, &signature))
    }
}

/// Assembles the signed type-2 call, yielding the bytes
/// `eth_sendRawTransaction` takes; `yParity` is `v - 27`.
#[must_use]
pub fn assemble(call: &Eip1559Call, signature: &AgentSignature) -> Vec<u8> {
    let bytes = signature.as_bytes();
    let mut payload = call.fields();
    rlp_integer(&[bytes[64] - 27], &mut payload);
    rlp_integer(&bytes[..32], &mut payload);
    rlp_integer(&bytes[32..64], &mut payload);
    let mut raw = vec![0x02];
    raw.extend_from_slice(&rlp_list(&payload));
    raw
}

/// Decodes a signed type-2 call that [`assemble`] produced.
///
/// # Errors
///
/// Refuses another envelope type, non-canonical or trailing RLP, non-minimal
/// integers, a non-zero value, a non-empty access list, a non-canonical
/// signature, and any input that does not re-encode byte for byte.
pub fn decode_signed(raw: &[u8]) -> Result<(Eip1559Call, AgentSignature), EvmTransactionError> {
    let Some((&0x02, mut envelope)) = raw.split_first() else {
        return Err(EvmTransactionError::NotEip1559);
    };
    let Item::List(mut fields) = take_item(&mut envelope)? else {
        return Err(EvmTransactionError::UnsupportedField);
    };
    if !envelope.is_empty() {
        return Err(EvmTransactionError::MalformedRlp);
    }
    let chain_id = u64::from_be_bytes(take_uint(&mut fields)?);
    let nonce = u64::from_be_bytes(take_uint(&mut fields)?);
    let max_priority_fee_per_gas = u128::from_be_bytes(take_uint(&mut fields)?);
    let max_fee_per_gas = u128::from_be_bytes(take_uint(&mut fields)?);
    let gas_limit = u64::from_be_bytes(take_uint(&mut fields)?);
    let to = take_string(&mut fields)?
        .try_into()
        .map_err(|_| EvmTransactionError::UnsupportedField)?;
    if take_uint::<32>(&mut fields)? != [0; 32] {
        return Err(EvmTransactionError::UnsupportedField);
    }
    let data = take_string(&mut fields)?.to_vec();
    let Item::List([]) = take_item(&mut fields)? else {
        return Err(EvmTransactionError::UnsupportedField);
    };
    let [y_parity] = take_uint::<1>(&mut fields)?;
    let r = take_uint::<32>(&mut fields)?;
    let s = take_uint::<32>(&mut fields)?;
    if !fields.is_empty() {
        return Err(EvmTransactionError::MalformedRlp);
    }
    if y_parity > 1 {
        return Err(EvmTransactionError::BadSignature);
    }
    let mut bytes = [0_u8; 65];
    bytes[..32].copy_from_slice(&r);
    bytes[32..64].copy_from_slice(&s);
    bytes[64] = 27 + y_parity;
    let signature = AgentSignature::new(bytes).map_err(|_| EvmTransactionError::BadSignature)?;
    let call = Eip1559Call {
        chain_id,
        nonce,
        max_priority_fee_per_gas,
        max_fee_per_gas,
        gas_limit,
        to,
        data,
    };
    if assemble(&call, &signature) != raw {
        return Err(EvmTransactionError::NonCanonical);
    }
    Ok((call, signature))
}

/// The transaction hash: keccak256 of the raw signed bytes.
#[must_use]
pub fn transaction_hash(raw: &[u8]) -> [u8; 32] {
    Keccak256::digest(raw).into()
}

/// Recovers the EVM account that signed `call`.
///
/// # Errors
///
/// Returns `BadSignature` when the signature recovers no key over the call's
/// signing hash.
pub fn recover_sender(
    call: &Eip1559Call,
    signature: &AgentSignature,
) -> Result<[u8; 20], EvmTransactionError> {
    let (compact, &[v]) = signature.as_bytes().split_at(64) else {
        return Err(EvmTransactionError::BadSignature);
    };
    let compact: &[u8; 64] = compact
        .try_into()
        .map_err(|_| EvmTransactionError::BadSignature)?;
    let recovery = v
        .checked_sub(27)
        .and_then(RecoveryId::from_byte)
        .ok_or(EvmTransactionError::BadSignature)?;
    let parsed = Signature::from_slice(compact).map_err(|_| EvmTransactionError::BadSignature)?;
    let digest = call.signing_hash();
    let recovered = VerifyingKey::recover_from_prehash(&digest, &parsed, recovery)
        .map_err(|_| EvmTransactionError::BadSignature)?;
    let public_key = recovered.to_encoded_point(false);
    verify_recoverable_digest(public_key.as_bytes(), compact, v, &digest)
        .map_err(|_| EvmTransactionError::BadSignature)?;
    evm_address(public_key.as_bytes()).map_err(|_| EvmTransactionError::BadSignature)
}
