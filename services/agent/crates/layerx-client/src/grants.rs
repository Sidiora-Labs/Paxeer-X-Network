//! Canonical committed grant records; signature validation is not state inclusion.

use layerx_crypto::payments::{Grant, Payment};
use layerx_types::payload::ModuleId;

pub const GRANT_MODULE_ID: u16 = 1;
pub const COMMITTED_GRANT_BYTES: usize = 396;

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum GrantDecodeError {
    Encoding,
    Key,
    Grant,
}

#[must_use]
pub fn grant_state_key(id: [u8; 32]) -> Vec<u8> {
    let mut key = b"grant:".to_vec();
    key.extend_from_slice(&id);
    key
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct CommittedGrant {
    pub grant: Grant,
    pub drawn_total: u128,
    pub drawn_this_period: u128,
    pub window_start: u64,
    pub revoked_at_sequence: u64,
    pub revoked: bool,
    pub invoice_settled: bool,
}

impl CommittedGrant {
    /// # Errors
    /// Refuses noncanonical lengths, booleans, keys and invalid grant signatures.
    pub fn decode(key: &[u8], bytes: &[u8]) -> Result<Self, GrantDecodeError> {
        if bytes.len() != COMMITTED_GRANT_BYTES || bytes[394] > 1 || bytes[395] > 1 {
            return Err(GrantDecodeError::Encoding);
        }
        let Payment::IssueGrant(grant) = Payment::decode(ModuleId::Asset, 7, &bytes[..346], b"")
            .map_err(|_| GrantDecodeError::Grant)?
        else {
            return Err(GrantDecodeError::Grant);
        };
        if key != grant_state_key(grant.id) {
            return Err(GrantDecodeError::Key);
        }
        let u128_at = |offset: usize| {
            let mut value = [0; 16];
            value.copy_from_slice(&bytes[offset..offset + 16]);
            u128::from_be_bytes(value)
        };
        let u64_at = |offset: usize| {
            let mut value = [0; 8];
            value.copy_from_slice(&bytes[offset..offset + 8]);
            u64::from_be_bytes(value)
        };
        Ok(Self {
            grant,
            drawn_total: u128_at(346),
            drawn_this_period: u128_at(362),
            window_start: u64_at(378),
            revoked_at_sequence: u64_at(386),
            revoked: bytes[394] != 0,
            invoice_settled: bytes[395] != 0,
        })
    }
}
