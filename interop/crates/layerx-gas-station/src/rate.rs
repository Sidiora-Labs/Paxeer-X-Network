use std::io::{ErrorKind, Read as _};
use std::path::Path;
use std::time::{Duration, Instant};

use serde_json::{json, Map, Value};

use crate::config::{field, object, read_text, ConfigError, StationConfig};
use crate::journal::{Entry, Journal, JournalError, Publication, Settlement};
use crate::quote::{keccak, word, Address, Word};
use crate::rpc::{bytes, hex, quantity, read, JsonRpc, RpcFault};
use crate::signer::QuoteSigner;
use crate::tx::{self, Fees, TxError};

/// The gas limit of every setRate transaction the publisher signs; the daily
/// budget counts it for each publication until its receipt settles the gas used.
pub const RATE_GAS_LIMIT: u64 = 60_000;
pub const DAY_SECONDS: u64 = 86_400;

#[derive(Clone, Debug, Eq, PartialEq)]
pub enum RateRefusal {
    RateFileMissing,
    RateFileMalformed,
    ZeroRate,
    NotYetSet,
    Expired,
    Unchanged { age: u64 },
    NotOwner,
    BudgetExhausted,
    BelowFloor,
    FeeAboveCeiling { required: u128, ceiling: u128 },
    Reverted,
    ReceiptTimeout,
    Rpc(RpcFault),
    Journal(JournalError),
    Transaction(TxError),
}
impl std::fmt::Display for RateRefusal {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "rate publication refused: {self:?}")
    }
}
impl std::error::Error for RateRefusal {}
impl From<RpcFault> for RateRefusal {
    fn from(e: RpcFault) -> Self {
        Self::Rpc(e)
    }
}
impl From<JournalError> for RateRefusal {
    fn from(e: JournalError) -> Self {
        Self::Journal(e)
    }
}
impl From<TxError> for RateRefusal {
    fn from(e: TxError) -> Self {
        Self::Transaction(e)
    }
}

/// The owner's published rate, read from the rate file on the station volume:
/// `rate` in SID base units per whole PAX, `set_at` the chain time the owner
/// set it, and the optional `not_after` chain time after which it is withdrawn.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct RateFile {
    pub rate: u128,
    pub set_at: u64,
    pub not_after: Option<u64>,
}
impl RateFile {
    /// Parses `name = integer` lines, blank lines and `#` comment lines; TOML
    /// digit separators are accepted.
    /// # Errors
    /// Refuses unknown, duplicate or missing keys, noncanonical integers,
    /// a zero `set_at`, a `not_after` not after `set_at`, and a zero rate.
    pub fn parse(text: &str) -> Result<Self, RateRefusal> {
        let (mut rate, mut set_at, mut not_after) = (None, None, None);
        for line in text.lines().map(str::trim) {
            if line.is_empty() || line.starts_with('#') {
                continue;
            }
            let (name, value) = line.split_once('=').ok_or(RateRefusal::RateFileMalformed)?;
            let value = integer(value.trim()).ok_or(RateRefusal::RateFileMalformed)?;
            let slot = match name.trim() {
                "rate" => &mut rate,
                "set_at" => &mut set_at,
                "not_after" => &mut not_after,
                _ => return Err(RateRefusal::RateFileMalformed),
            };
            if slot.replace(value).is_some() {
                return Err(RateRefusal::RateFileMalformed);
            }
        }
        let rate = rate.ok_or(RateRefusal::RateFileMalformed)?;
        let set_at = set_at
            .and_then(|v| u64::try_from(v).ok())
            .filter(|v| *v > 0)
            .ok_or(RateRefusal::RateFileMalformed)?;
        let not_after = not_after
            .map(|v| u64::try_from(v).ok().filter(|v| *v > set_at))
            .map(|v| v.ok_or(RateRefusal::RateFileMalformed))
            .transpose()?;
        if rate == 0 {
            return Err(RateRefusal::ZeroRate);
        }
        Ok(Self {
            rate,
            set_at,
            not_after,
        })
    }

    /// # Errors
    /// Refuses a missing file as missing and an unreadable, oversized or
    /// invalid one as malformed.
    pub fn load(path: &Path) -> Result<Self, RateRefusal> {
        let file = std::fs::File::open(path).map_err(|e| {
            if e.kind() == ErrorKind::NotFound {
                RateRefusal::RateFileMissing
            } else {
                RateRefusal::RateFileMalformed
            }
        })?;
        let mut text = String::new();
        file.take(4097)
            .read_to_string(&mut text)
            .map_err(|_| RateRefusal::RateFileMalformed)?;
        if text.len() > 4096 {
            return Err(RateRefusal::RateFileMalformed);
        }
        Self::parse(&text)
    }
}

fn integer(text: &str) -> Option<u128> {
    let digits = text.replace('_', "");
    if digits.is_empty()
        || text.starts_with('_')
        || text.ends_with('_')
        || text.contains("__")
        || (digits.len() > 1 && digits.starts_with('0'))
        || !digits.bytes().all(|b| b.is_ascii_digit())
    {
        return None;
    }
    digits.parse().ok()
}

/// The station configuration plus the publisher's own fields: the env
/// variable holding the paymaster owner's key, the publication cadence
/// (strictly below `max_rate_age`), the daily gas budget of publications, the
/// owner balance floor in PAX base units, the priority fee per gas and the
/// ceiling on the fee per gas of a publication, in wei.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct PublisherConfig {
    pub station: StationConfig,
    pub owner_key_env: String,
    pub cadence_seconds: u64,
    pub gas_budget_per_day: u128,
    pub balance_floor: u128,
    pub max_priority_fee_per_gas: u128,
    pub max_fee_per_gas: u128,
}
impl PublisherConfig {
    /// # Errors
    /// Returns the field that is missing, malformed or inconsistent.
    pub fn parse(text: &str) -> Result<Self, ConfigError> {
        let mut map: Map<String, Value> = object(text)?;
        let owner_key_env: String = field(&mut map, "rate_owner_key_env")?;
        let cadence_seconds: u64 = field(&mut map, "rate_cadence_seconds")?;
        let gas_budget_per_day: u128 = field(&mut map, "rate_gas_budget_per_day")?;
        let balance_floor: u128 = field(&mut map, "rate_balance_floor")?;
        let max_priority_fee_per_gas: u128 = field(&mut map, "max_priority_fee_per_gas")?;
        let max_fee_per_gas: u128 = field(&mut map, "rate_max_fee_per_gas")?;
        let config = Self {
            station: StationConfig::from_map(map)?,
            owner_key_env,
            cadence_seconds,
            gas_budget_per_day,
            balance_floor,
            max_priority_fee_per_gas,
            max_fee_per_gas,
        };
        config.validate()?;
        Ok(config)
    }

    /// # Errors
    /// Refuses unreadable, oversized or invalid configuration files.
    pub fn load(path: &Path) -> Result<Self, ConfigError> {
        Self::parse(&read_text(path)?)
    }

    /// # Errors
    /// Names the first field that is inconsistent: a cadence not below
    /// `max_rate_age`, a budget below one publication, a zero floor, a fee
    /// ceiling not above the priority fee or whose daily worst case overflows,
    /// or an owner key env that is invalid or the sponsor's.
    pub fn validate(&self) -> Result<(), ConfigError> {
        self.station.validate()?;
        let checks = [
            (
                "rate_owner_key_env",
                !self.owner_key_env.is_empty()
                    && self.owner_key_env != self.station.relayer_key_env
                    && self.owner_key_env.bytes().enumerate().all(|(i, b)| {
                        b == b'_' || b.is_ascii_uppercase() || (i > 0 && b.is_ascii_digit())
                    }),
            ),
            (
                "rate_cadence_seconds",
                self.cadence_seconds > 0 && self.cadence_seconds < self.station.max_rate_age,
            ),
            (
                "rate_gas_budget_per_day",
                self.gas_budget_per_day >= u128::from(RATE_GAS_LIMIT),
            ),
            ("rate_balance_floor", self.balance_floor > 0),
            (
                "rate_max_fee_per_gas",
                self.max_fee_per_gas > self.max_priority_fee_per_gas
                    && self.daily_wei_ceiling().is_some(),
            ),
        ];
        for (field, valid) in checks {
            if !valid {
                return Err(ConfigError { field });
            }
        }
        Ok(())
    }

    /// The most wei the publisher may spend in one chain day: the fee ceiling
    /// times the daily gas budget.
    #[must_use]
    pub fn daily_wei_ceiling(&self) -> Option<u128> {
        self.max_fee_per_gas.checked_mul(self.gas_budget_per_day)
    }
}

/// Publishes the owner's rate to the paymaster with setRate, signed by the
/// paymaster owner's key, and journals every publication before broadcast.
pub struct RatePublisher<S, R> {
    config: PublisherConfig,
    signer: S,
    rpc: R,
    journal: Journal,
    poll: Duration,
}
impl<S: QuoteSigner, R: JsonRpc> RatePublisher<S, R> {
    /// # Errors
    /// Refuses an invalid configuration, a zero signer or a node on another chain.
    pub fn new(
        config: PublisherConfig,
        signer: S,
        rpc: R,
        journal: Journal,
        poll: Duration,
    ) -> Result<Self, RateRefusal> {
        config
            .validate()
            .map_err(|_| RateRefusal::Rpc(RpcFault::Configuration))?;
        let chain: String = read(&rpc, "eth_chainId", json!([]))?;
        if signer.address() == [0; 20] || quantity(&chain)? != u128::from(config.station.chain_id) {
            return Err(RateRefusal::Rpc(RpcFault::Configuration));
        }
        Ok(Self {
            config,
            signer,
            rpc,
            journal,
            poll,
        })
    }
    #[must_use]
    pub const fn journal(&self) -> &Journal {
        &self.journal
    }
    #[must_use]
    pub const fn cadence(&self) -> u64 {
        self.config.cadence_seconds
    }

    fn paymaster_word(&self, signature: &[u8]) -> Result<Word, RateRefusal> {
        let result: String = read(
            &self.rpc,
            "eth_call",
            json!([{"to":hex(&self.config.station.paymaster),"data":hex(&keccak(signature)[..4])},"latest"]),
        )?;
        Ok(bytes(&result)?
            .try_into()
            .map_err(|_| RpcFault::Malformed)?)
    }
    fn paymaster_uint(&self, signature: &[u8]) -> Result<u128, RateRefusal> {
        let value = self.paymaster_word(signature)?;
        if value[..16] != [0; 16] {
            return Err(RpcFault::Malformed.into());
        }
        Ok(u128::from_be_bytes(
            value[16..].try_into().map_err(|_| RpcFault::Malformed)?,
        ))
    }

    /// Publishes the rate file's rate once, waits for its receipt and returns
    /// the journalled publication.
    /// # Errors
    /// Refuses a missing, malformed, zero, not yet set or expired rate file,
    /// a rate unchanged on chain within the cadence, a signer that is not the
    /// paymaster owner, an exhausted daily gas budget, a fee per gas above the
    /// ceiling, an owner balance that would fall below its floor once this
    /// publication and every unsettled one pay their most, a reverted or
    /// unconfirmed publication, and node or journal failures. Before the
    /// budget and balance checks it settles every unsettled publication whose
    /// receipt the node now has. A refusal never publishes, so the paymaster's rate ages
    /// until `currentRate` reverts with `StaleRate`.
    pub fn publish(&mut self, rate_file: &Path) -> Result<Publication, RateRefusal> {
        let file = RateFile::load(rate_file)?;
        let head: Value = self
            .rpc
            .call("eth_getBlockByNumber", json!(["latest", false]))?;
        let now =
            u64::try_from(field_quantity(&head, "timestamp")?).map_err(|_| RpcFault::Malformed)?;
        let base_fee = field_quantity(&head, "baseFeePerGas")?;
        if file.set_at > now {
            return Err(RateRefusal::NotYetSet);
        }
        if file.not_after.is_some_and(|t| now >= t) {
            return Err(RateRefusal::Expired);
        }
        let on_chain = self.paymaster_uint(b"rate()")?;
        let updated_at = self.paymaster_uint(b"rateUpdatedAt()")?;
        let age = u64::try_from(u128::from(now).saturating_sub(updated_at)).unwrap_or(u64::MAX);
        if on_chain == file.rate && age < self.config.cadence_seconds {
            return Err(RateRefusal::Unchanged { age });
        }
        let owner = self.paymaster_word(b"owner()")?;
        let signer: Address = self.signer.address();
        if owner[..12] != [0; 12] || owner[12..] != signer {
            return Err(RateRefusal::NotOwner);
        }
        for pending in self.journal.state().unsettled() {
            let receipt = self
                .rpc
                .call("eth_getTransactionReceipt", json!([hex(&pending.hash)]))?;
            if !receipt.is_null() {
                self.record(&pending, &receipt)?;
            }
        }
        let spent = self.journal.state().publication_gas(now / DAY_SECONDS);
        if spent + u128::from(RATE_GAS_LIMIT) > self.config.gas_budget_per_day {
            return Err(RateRefusal::BudgetExhausted);
        }
        let max_fee_per_gas = base_fee
            .checked_mul(2)
            .and_then(|fee| fee.checked_add(self.config.max_priority_fee_per_gas))
            .ok_or(TxError::Invalid)?;
        if max_fee_per_gas > self.config.max_fee_per_gas {
            return Err(RateRefusal::FeeAboveCeiling {
                required: max_fee_per_gas,
                ceiling: self.config.max_fee_per_gas,
            });
        }
        let balance: String = read(
            &self.rpc,
            "eth_getBalance",
            json!([hex(&signer), "pending"]),
        )?;
        let balance = quantity(&balance)?;
        let required = max_fee_per_gas
            .checked_mul(u128::from(RATE_GAS_LIMIT))
            .and_then(|cost| cost.checked_add(self.journal.state().reserved_wei()))
            .and_then(|cost| cost.checked_add(self.config.balance_floor));
        if required.is_none_or(|required| balance < required) {
            return Err(RateRefusal::BelowFloor);
        }
        let count: String = read(
            &self.rpc,
            "eth_getTransactionCount",
            json!([hex(&signer), "pending"]),
        )?;
        let nonce = u64::try_from(quantity(&count)?).map_err(|_| RpcFault::Malformed)?;
        let fees = Fees {
            gas_limit: RATE_GAS_LIMIT,
            max_fee_per_gas,
            max_priority_fee_per_gas: self.config.max_priority_fee_per_gas,
        };
        let data = [
            &keccak(b"setRate(uint256)")[..4],
            word(file.rate).as_slice(),
        ]
        .concat();
        let signed = tx::sign_call(
            self.config.station.chain_id,
            nonce,
            fees,
            self.config.station.paymaster,
            &data,
            &self.signer,
        )?;
        let publication = Publication {
            owner: signer,
            nonce,
            hash: signed.hash,
            rate: word(file.rate),
            gas_limit: RATE_GAS_LIMIT,
            max_fee_per_gas,
            signed_at: now,
        };
        self.journal.append(&Entry::RatePublished { publication })?;
        match self
            .rpc
            .call("eth_sendRawTransaction", json!([hex(&signed.raw)]))
        {
            Err(fault @ (RpcFault::Rejected { .. } | RpcFault::Configuration)) => {
                return Err(fault.into())
            }
            Ok(_) | Err(_) => (),
        }
        self.settle(&publication)
    }

    fn settle(&mut self, publication: &Publication) -> Result<Publication, RateRefusal> {
        let deadline = Instant::now() + Duration::from_secs(self.config.cadence_seconds);
        loop {
            let receipt = self
                .rpc
                .call("eth_getTransactionReceipt", json!([hex(&publication.hash)]))?;
            if !receipt.is_null() {
                return if self.record(publication, &receipt)?.succeeded {
                    Ok(*publication)
                } else {
                    Err(RateRefusal::Reverted)
                };
            }
            if Instant::now() >= deadline {
                return Err(RateRefusal::ReceiptTimeout);
            }
            std::thread::sleep(self.poll);
        }
    }

    /// Journals the settlement the receipt reports, its cost the effective
    /// gas price times the gas used.
    fn record(
        &mut self,
        publication: &Publication,
        receipt: &Value,
    ) -> Result<Settlement, RateRefusal> {
        if receipt["transactionHash"] != hex(&publication.hash)
            || receipt["from"] != hex(&publication.owner)
            || receipt["to"] != hex(&self.config.station.paymaster)
        {
            return Err(RpcFault::Malformed.into());
        }
        let gas_used =
            u64::try_from(field_quantity(receipt, "gasUsed")?).map_err(|_| RpcFault::Malformed)?;
        let price = field_quantity(receipt, "effectiveGasPrice")?;
        if price > publication.max_fee_per_gas {
            return Err(RpcFault::Malformed.into());
        }
        let settlement = Settlement {
            block_number: u64::try_from(field_quantity(receipt, "blockNumber")?)
                .map_err(|_| RpcFault::Malformed)?,
            gas_used,
            cost_wei: price
                .checked_mul(u128::from(gas_used))
                .ok_or(RpcFault::Malformed)?,
            succeeded: field_quantity(receipt, "status")? == 1,
        };
        self.journal.append(&Entry::RateSettled {
            hash: publication.hash,
            settlement,
        })?;
        Ok(settlement)
    }
}

fn field_quantity(value: &Value, name: &str) -> Result<u128, RpcFault> {
    quantity(value[name].as_str().ok_or(RpcFault::Malformed)?)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn integers_follow_toml_digit_rules() {
        assert_eq!(integer("3_114_000"), Some(3_114_000));
        assert_eq!(integer("0"), Some(0));
        for invalid in [
            "", "_1", "1_", "1__0", "01", "-1", "+1", "0x10", "1.5", "\"1\"",
        ] {
            assert_eq!(integer(invalid), None, "{invalid}");
        }
    }
}
