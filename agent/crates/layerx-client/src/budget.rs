//! Canonical budget record decoding; decoding alone does not verify state.

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum ReconcileError {
    UnverifiedProtocolState,
    ProtocolStateSchemaUnavailable,
    UnverifiedReceipt,
    DuplicateReceipt,
    DuplicateActivity,
    ReceiptActivityMismatch,
}

/// Core module identifier whose state holds canonical budget records.
pub const BUDGET_MODULE_ID: u16 = 3;

const BUDGET_STATE_KEY_PREFIX: &[u8] = b"budget:";
const BUDGET_RECORD_FIXED_BYTES: usize = 278;
const BUDGET_MAX_DELEGATES: usize = 16;

/// Returns the exact core state key under which a budget record is stored.
#[must_use]
pub fn budget_state_key(budget_id: [u8; 32]) -> Vec<u8> {
    let mut key = Vec::with_capacity(BUDGET_STATE_KEY_PREFIX.len() + budget_id.len());
    key.extend_from_slice(BUDGET_STATE_KEY_PREFIX);
    key.extend_from_slice(&budget_id);
    key
}

/// Canonical core budget record. Inclusion and authority must be verified separately.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ProtocolBudgetRecord {
    pub budget_id: [u8; 32],
    pub owner: [u8; 32],
    pub budget_account: [u8; 32],
    pub asset_id: [u8; 32],
    pub purpose_hash: [u8; 32],
    pub per_period_limit: u128,
    pub configured_period_limit: u128,
    pub carry_cap: u128,
    pub spent_this_period: u128,
    pub carried: u128,
    pub period_length: u64,
    pub period_start: u64,
    pub expiry: u64,
    pub revocation_sequence: u64,
    pub rollover_policy: u8,
    pub closed: bool,
    pub revoked: bool,
    pub delegates: Vec<[u8; 32]>,
    pub source_account: Option<[u8; 32]>,
}

impl ProtocolBudgetRecord {
    /// # Errors
    /// Refuses a malformed record or a state key that does not name its budget.
    pub fn decode_state(key: &[u8], bytes: &[u8]) -> Result<Self, ReconcileError> {
        let record = Self::decode(bytes)?;
        if key != budget_state_key(record.budget_id) {
            return Err(ReconcileError::ProtocolStateSchemaUnavailable);
        }
        Ok(record)
    }

    /// Decodes the exact core budget record encoding.
    ///
    /// # Errors
    ///
    /// Returns `ProtocolStateSchemaUnavailable` when the bytes are not one canonical
    /// core budget record.
    pub fn decode(bytes: &[u8]) -> Result<Self, ReconcileError> {
        let unavailable = ReconcileError::ProtocolStateSchemaUnavailable;
        if bytes.len() < BUDGET_RECORD_FIXED_BYTES
            || bytes[0] != 0
            || (bytes[1] != 1 && bytes[1] != 2)
            || bytes[275] > 1
            || bytes[276] > 1
        {
            return Err(unavailable);
        }
        let native_source = bytes[1] == 2;
        let delegate_count = usize::from(bytes[277]);
        let expected =
            BUDGET_RECORD_FIXED_BYTES + delegate_count * 32 + if native_source { 32 } else { 0 };
        if delegate_count > BUDGET_MAX_DELEGATES || bytes.len() != expected {
            return Err(unavailable);
        }
        let fixed = |offset: usize| -> [u8; 32] {
            let mut value = [0_u8; 32];
            value.copy_from_slice(&bytes[offset..offset + 32]);
            value
        };
        let u128_at = |offset: usize| -> u128 {
            let mut value = [0_u8; 16];
            value.copy_from_slice(&bytes[offset..offset + 16]);
            u128::from_be_bytes(value)
        };
        let u64_at = |offset: usize| -> u64 {
            let mut value = [0_u8; 8];
            value.copy_from_slice(&bytes[offset..offset + 8]);
            u64::from_be_bytes(value)
        };
        let mut delegates = Vec::with_capacity(delegate_count);
        for index in 0..delegate_count {
            let entry = fixed(BUDGET_RECORD_FIXED_BYTES + index * 32);
            if delegates.last().is_some_and(|previous| *previous >= entry) {
                return Err(unavailable);
            }
            delegates.push(entry);
        }
        let record = Self {
            budget_id: fixed(2),
            owner: fixed(34),
            budget_account: fixed(66),
            asset_id: fixed(98),
            purpose_hash: fixed(130),
            per_period_limit: u128_at(162),
            configured_period_limit: u128_at(178),
            carry_cap: u128_at(194),
            spent_this_period: u128_at(210),
            carried: u128_at(226),
            period_length: u64_at(242),
            period_start: u64_at(250),
            expiry: u64_at(258),
            revocation_sequence: u64_at(266),
            rollover_policy: bytes[274],
            closed: bytes[275] != 0,
            revoked: bytes[276] != 0,
            delegates,
            source_account: native_source.then(|| fixed(bytes.len() - 32)),
        };
        if record.budget_id == [0; 32]
            || record.period_length == 0
            || record.spent_this_period > record.per_period_limit
            || record
                .period_start
                .checked_add(record.period_length)
                .is_none()
        {
            return Err(unavailable);
        }
        Ok(record)
    }

    /// Returns the allowance still spendable in the current period.
    #[must_use]
    pub const fn remaining(&self) -> u128 {
        self.per_period_limit.saturating_sub(self.spent_this_period)
    }

    /// Returns the exclusive end sequence of the current period window.
    #[must_use]
    pub const fn window_end_sequence(&self) -> u64 {
        self.period_start.saturating_add(self.period_length)
    }
}
