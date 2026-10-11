use std::io::{ErrorKind, Read as _};
use std::os::unix::fs::{MetadataExt as _, OpenOptionsExt as _};
use std::path::Path;
use std::time::{Duration, Instant};

use serde_json::{json, Map, Value};

use crate::config::{field, object, read_text, ConfigError, StationConfig};
use crate::journal::{
    Entry, Journal, JournalError, Publication, PublicationTransaction, Settlement,
};
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
    LegacyUnresolved,
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
        let mut options = std::fs::OpenOptions::new();
        options.read(true);
        #[cfg(target_os = "linux")]
        options.custom_flags(0x20000);
        let file = options.open(path).map_err(|e| {
            if e.kind() == ErrorKind::NotFound {
                RateRefusal::RateFileMissing
            } else {
                RateRefusal::RateFileMalformed
            }
        })?;
        let metadata = file
            .metadata()
            .map_err(|_| RateRefusal::RateFileMalformed)?;
        let owner = std::fs::metadata("/proc/self")
            .map_err(|_| RateRefusal::RateFileMalformed)?
            .uid();
        if !metadata.is_file()
            || metadata.mode() & 0o077 != 0
            || metadata.nlink() != 1
            || metadata.uid() != owner
        {
            return Err(RateRefusal::RateFileMalformed);
        }
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
    pub confirmation_retry_seconds: u64,
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
        let confirmation_retry_seconds: u64 = field(&mut map, "rate_confirmation_retry_seconds")?;
        let config = Self {
            station: StationConfig::from_map(map)?,
            owner_key_env,
            cadence_seconds,
            gas_budget_per_day,
            balance_floor,
            max_priority_fee_per_gas,
            max_fee_per_gas,
            confirmation_retry_seconds,
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
    /// `max_rate_age`, a budget below one publication, a zero confirmation
    /// retry or one whose sum with the cadence is not strictly below
    /// `max_rate_age` (itself at most 300 s), a zero floor, a
    /// paymaster in the precompile range (the fee token at 0x1018 and every
    /// other system address, whose rates move only by governance), a fee
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
            (
                "rate_confirmation_retry_seconds",
                self.confirmation_retry_seconds > 0
                    && self
                        .cadence_seconds
                        .checked_add(self.confirmation_retry_seconds)
                        .is_some_and(|bound| bound < self.station.max_rate_age),
            ),
            ("rate_balance_floor", self.balance_floor > 0),
            ("paymaster", self.station.paymaster[..18] != [0; 18]),
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
        rpc.set_deadline(Some(
            Instant::now() + Duration::from_secs(config.confirmation_retry_seconds),
        ))?;
        let chain: String = read(&rpc, "eth_chainId", json!([]))?;
        if signer.address() == [0; 20] || quantity(&chain)? != u128::from(config.station.chain_id) {
            return Err(RateRefusal::Rpc(RpcFault::Configuration));
        }
        if journal
            .state()
            .unsettled()
            .iter()
            .any(|p| !journal.state().rate_transactions.contains_key(&p.hash))
        {
            return Err(RateRefusal::LegacyUnresolved);
        }
        for transaction in journal.state().rate_transactions.values() {
            validate_publication_transaction(transaction)?;
            if transaction.chain_id != config.station.chain_id
                || transaction.paymaster != config.station.paymaster
                || transaction.publication.owner != signer.address()
                || transaction.publication.max_fee_per_gas > config.max_fee_per_gas
            {
                return Err(RateRefusal::NotOwner);
            }
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

    /// The fee ceiling of the next publication: twice the base fee plus the
    /// priority fee.
    /// # Errors
    /// Refuses an exhausted daily gas budget, a fee per gas above the
    /// ceiling, and an owner balance that would fall below its floor once this
    /// publication and every unsettled one pay their most.
    fn spend_ceiling(
        &self,
        now: u64,
        base_fee: u128,
        signer: &Address,
    ) -> Result<u128, RateRefusal> {
        let spent = self.journal.state().publication_gas(now / DAY_SECONDS);
        if spent
            .checked_add(u128::from(RATE_GAS_LIMIT))
            .is_none_or(|gas| gas > self.config.gas_budget_per_day)
        {
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
        let balance: String = read(&self.rpc, "eth_getBalance", json!([hex(signer), "pending"]))?;
        let balance = quantity(&balance)?;
        let required = max_fee_per_gas
            .checked_mul(u128::from(RATE_GAS_LIMIT))
            .and_then(|cost| cost.checked_add(self.journal.state().reserved_wei()))
            .and_then(|cost| cost.checked_add(self.config.balance_floor));
        if required.is_none_or(|required| balance < required) {
            return Err(RateRefusal::BelowFloor);
        }
        Ok(max_fee_per_gas)
    }

    /// Publishes the rate file's rate once to the sequencer, confirms it by
    /// its `pax_sequencerReceipt` and returns the journalled publication.
    /// # Errors
    /// Refuses a missing, malformed, zero, not yet set or expired rate file,
    /// a rate unchanged on chain within the cadence, a signer that is not the
    /// paymaster owner, an exhausted daily gas budget, a fee per gas above the
    /// ceiling, an owner balance that would fall below its floor once this
    /// publication and every unsettled one pay their most, a reverted or
    /// unconfirmed publication, and node or journal failures. Before the
    /// budget and balance checks it confirms every unconfirmed publication and
    /// settles every one whose committed receipt the node now has. A refusal
    /// never publishes, so the paymaster's rate ages until `currentRate`
    /// reverts with `StaleRate`.
    pub fn publish(&mut self, rate_file: &Path) -> Result<Publication, RateRefusal> {
        self.rpc.set_deadline(Some(
            Instant::now() + Duration::from_secs(self.config.confirmation_retry_seconds),
        ))?;
        if let Some(recovered) = self.recover_pending()? {
            return Ok(recovered);
        }
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
        let max_fee_per_gas = self.spend_ceiling(now, base_fee, &signer)?;
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
        let envelope = tx::sign_call(
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
            hash: envelope.hash,
            rate: word(file.rate),
            gas_limit: RATE_GAS_LIMIT,
            max_fee_per_gas,
            signed_at: now,
        };
        let transaction = PublicationTransaction {
            version: 2,
            publication,
            chain_id: self.config.station.chain_id,
            paymaster: self.config.station.paymaster,
            max_priority_fee_per_gas: fees.max_priority_fee_per_gas,
            raw: envelope.raw,
            cancellation: false,
        };
        self.journal.append(&Entry::RatePrepared {
            transaction: transaction.clone(),
        })?;
        self.broadcast(&transaction)?;
        self.confirm_by_receipt(&transaction)
    }

    /// Settles every journalled publication whose committed receipt the node
    /// has, and waits for the sequencer receipt of every other one. A
    /// publication the sequencer has no record of is resent as its exact
    /// journalled bytes; no other transaction is ever signed at its nonce.
    /// # Errors
    /// Refuses a signer that is not the paymaster owner, a reverted or
    /// unconfirmed publication, a nonce consumed by another transaction, and
    /// node or journal failures.
    pub fn recover_pending(&mut self) -> Result<Option<Publication>, RateRefusal> {
        let owner = self.paymaster_word(b"owner()")?;
        if owner[..12] != [0; 12] || owner[12..] != self.signer.address() {
            return Err(RateRefusal::NotOwner);
        }
        let groups: Vec<_> = self.journal.state().rate_active.keys().copied().collect();
        let mut recovered = None;
        for (signer, nonce) in groups {
            let candidates: Vec<_> = self
                .journal
                .state()
                .rate_transactions
                .values()
                .filter(|tx| tx.publication.owner == signer && tx.publication.nonce == nonce)
                .cloned()
                .collect();
            let mut complete = None;
            for transaction in candidates {
                if let Some(settlement) = self.try_finalize(&transaction)? {
                    complete = Some((transaction.publication, settlement));
                    break;
                }
            }
            if let Some((publication, settlement)) = complete {
                if !settlement.succeeded {
                    return Err(RateRefusal::Reverted);
                }
                recovered = Some(publication);
                continue;
            }
            let hash = *self
                .journal
                .state()
                .rate_active
                .get(&(signer, nonce))
                .ok_or(JournalError::Corrupt)?;
            let transaction = self
                .journal
                .state()
                .rate_transactions
                .get(&hash)
                .cloned()
                .ok_or(JournalError::Corrupt)?;
            let finalized_nonce: String = read(
                &self.rpc,
                "eth_getTransactionCount",
                json!([hex(&signer), "finalized"]),
            )?;
            if quantity(&finalized_nonce)? > u128::from(nonce) {
                return Err(RpcFault::Divergence.into());
            }
            if self
                .rpc
                .call("pax_sequencerReceipt", json!([hex(&hash)]))?
                .is_null()
            {
                self.broadcast(&transaction)?;
            }
            recovered = Some(self.confirm_by_receipt(&transaction)?);
        }
        Ok(recovered)
    }

    fn broadcast(&mut self, transaction: &PublicationTransaction) -> Result<(), RateRefusal> {
        let publication = transaction.publication;
        match self
            .rpc
            .call("eth_sendRawTransaction", json!([hex(&transaction.raw)]))
        {
            Ok(value) if value == hex(&publication.hash) => {
                if !self
                    .journal
                    .state()
                    .rate_broadcast
                    .contains(&publication.hash)
                {
                    self.journal.append(&Entry::RateBroadcast {
                        hash: publication.hash,
                    })?;
                }
                Ok(())
            }
            Ok(_) => Err(RpcFault::Malformed.into()),
            Err(error) => {
                let seen = self
                    .rpc
                    .call("pax_sequencerReceipt", json!([hex(&publication.hash)]))?;
                if seen["txHash"] == hex(&publication.hash) {
                    Ok(())
                } else {
                    Err(error.into())
                }
            }
        }
    }

    /// Waits up to `rate_confirmation_retry_seconds` for the sequencer to
    /// receipt the publication: `pax_sequencerReceipt` at rung receipted or
    /// committed, from the owner to the paymaster at the publication's nonce,
    /// within its gas limit and fee ceiling, and on success carrying the
    /// paymaster's `RateUpdated` event with the published rate. An unknown or
    /// submitted publication is waited on, never resent or replaced. A
    /// confirmed publication whose committed receipt the node has finalized is
    /// settled in the journal.
    /// # Errors
    /// Refuses a reverted publication, a receipt that does not match it, a
    /// publication still unreceipted at the deadline, a committed receipt
    /// that disagrees with the sequencer's, and node or journal failures.
    pub fn confirm_by_receipt(
        &mut self,
        transaction: &PublicationTransaction,
    ) -> Result<Publication, RateRefusal> {
        let publication = transaction.publication;
        let deadline = Instant::now() + Duration::from_secs(self.config.confirmation_retry_seconds);
        self.rpc.set_deadline(Some(deadline))?;
        loop {
            if Instant::now() >= deadline {
                return Err(RateRefusal::ReceiptTimeout);
            }
            let receipt = match self
                .rpc
                .call("pax_sequencerReceipt", json!([hex(&publication.hash)]))
            {
                Err(RpcFault::Unavailable) if Instant::now() >= deadline => {
                    return Err(RateRefusal::ReceiptTimeout);
                }
                result => result?,
            };
            if !receipt.is_null() {
                if receipt["txHash"] != hex(&publication.hash)
                    || field_quantity(&receipt, "nonce")? != u128::from(publication.nonce)
                {
                    return Err(RpcFault::Malformed.into());
                }
                match receipt["rung"].as_str() {
                    Some("submitted") => {}
                    Some("receipted" | "committed") => {
                        if !sequencer_outcome(transaction, &receipt)? {
                            return Err(RateRefusal::Reverted);
                        }
                        if self
                            .try_finalize(transaction)?
                            .is_some_and(|settlement| !settlement.succeeded)
                        {
                            return Err(RpcFault::Divergence.into());
                        }
                        return Ok(publication);
                    }
                    _ => return Err(RpcFault::Malformed.into()),
                }
            }
            std::thread::sleep(self.poll);
        }
    }

    /// Journals the settlement of a publication whose committed receipt the
    /// node has finalized; `None` while it has none.
    fn try_finalize(
        &mut self,
        transaction: &PublicationTransaction,
    ) -> Result<Option<Settlement>, RateRefusal> {
        let receipt = self.rpc.call(
            "eth_getTransactionReceipt",
            json!([hex(&transaction.publication.hash)]),
        )?;
        if receipt.is_null() {
            return Ok(None);
        }
        match self.record(&transaction.publication, &receipt) {
            Ok(settlement) => Ok(Some(settlement)),
            Err(RateRefusal::ReceiptTimeout) => Ok(None),
            Err(error) => Err(error),
        }
    }

    /// Journals the settlement the receipt reports, its cost the effective
    /// gas price times the gas used.
    fn record(
        &mut self,
        publication: &Publication,
        receipt: &Value,
    ) -> Result<Settlement, RateRefusal> {
        let transaction = self
            .journal
            .state()
            .rate_transactions
            .get(&publication.hash)
            .ok_or(RateRefusal::LegacyUnresolved)?;
        let settlement = publication_settlement(transaction, receipt)?;
        let canonical = self.rpc.call(
            "eth_getBlockByNumber",
            json!([receipt["blockNumber"], false]),
        )?;
        let finalized = self
            .rpc
            .call("eth_getBlockByNumber", json!(["finalized", false]))?;
        if field_quantity(&finalized, "number")? < u128::from(settlement.block_number) {
            return Err(RateRefusal::ReceiptTimeout);
        }
        crate::station::validate_finalized_receipt(
            &publication.hash,
            receipt,
            &canonical,
            &finalized,
        )?;
        let final_nonce: String = read(
            &self.rpc,
            "eth_getTransactionCount",
            json!([hex(&publication.owner), "finalized"]),
        )?;
        if quantity(&final_nonce)? <= u128::from(publication.nonce) {
            return Err(RateRefusal::ReceiptTimeout);
        }
        let checked = self.rpc.call(
            "eth_getBlockByNumber",
            json!([receipt["blockNumber"], false]),
        )?;
        if checked != canonical {
            return Err(RpcFault::Divergence.into());
        }
        self.journal.append(&Entry::RateFinalized {
            hash: publication.hash,
            settlement,
            receipt: receipt.clone(),
            canonical,
            finalized,
        })?;
        Ok(settlement)
    }
}

fn field_quantity(value: &Value, name: &str) -> Result<u128, RpcFault> {
    quantity(value[name].as_str().ok_or(RpcFault::Malformed)?)
}

/// Whether a receipted or committed `pax_sequencerReceipt` reports the
/// publication succeeded; a setRate that succeeded must carry the paymaster's
/// `RateUpdated(rate, updatedAt)` event with the published rate.
fn sequencer_outcome(
    transaction: &PublicationTransaction,
    receipt: &Value,
) -> Result<bool, RpcFault> {
    let p = transaction.publication;
    let to = if transaction.cancellation {
        p.owner
    } else {
        transaction.paymaster
    };
    let gas = field_quantity(receipt, "gasUsed")?;
    let price = field_quantity(receipt, "effectiveGasPrice")?;
    let status = field_quantity(receipt, "status")?;
    if receipt["from"] != hex(&p.owner)
        || receipt["to"] != hex(&to)
        || gas == 0
        || gas > u128::from(p.gas_limit)
        || price > p.max_fee_per_gas
        || status > 1
    {
        return Err(RpcFault::Malformed);
    }
    if status == 0 || transaction.cancellation {
        return Ok(status == 1);
    }
    let topic = hex(&keccak(b"RateUpdated(uint256,uint256)"));
    let logs = receipt["logs"].as_array().ok_or(RpcFault::Malformed)?;
    let mut updated = false;
    for log in logs {
        if log["address"] != hex(&transaction.paymaster) || log["topics"] != json!([topic]) {
            continue;
        }
        let data = bytes(log["data"].as_str().ok_or(RpcFault::Malformed)?)?;
        if data.len() != 64 || data[..32] != p.rate {
            return Err(RpcFault::Malformed);
        }
        updated = true;
    }
    if updated {
        Ok(true)
    } else {
        Err(RpcFault::Malformed)
    }
}

pub(crate) fn publication_settlement(
    transaction: &PublicationTransaction,
    receipt: &Value,
) -> Result<Settlement, RpcFault> {
    let p = transaction.publication;
    let to = if transaction.cancellation {
        p.owner
    } else {
        transaction.paymaster
    };
    let gas = field_quantity(receipt, "gasUsed")?;
    let price = field_quantity(receipt, "effectiveGasPrice")?;
    let status = field_quantity(receipt, "status")?;
    let block_hash = bytes(receipt["blockHash"].as_str().ok_or(RpcFault::Malformed)?)?;
    if receipt["transactionHash"] != hex(&p.hash)
        || receipt["from"] != hex(&p.owner)
        || receipt["to"] != hex(&to)
        || gas == 0
        || gas > u128::from(p.gas_limit)
        || price > p.max_fee_per_gas
        || status > 1
        || block_hash.len() != 32
        || block_hash.iter().all(|byte| *byte == 0)
    {
        return Err(RpcFault::Malformed);
    }
    Ok(Settlement {
        block_number: u64::try_from(field_quantity(receipt, "blockNumber")?)
            .map_err(|_| RpcFault::Malformed)?,
        gas_used: u64::try_from(gas).map_err(|_| RpcFault::Malformed)?,
        cost_wei: gas.checked_mul(price).ok_or(RpcFault::Malformed)?,
        succeeded: status == 1,
    })
}

fn rlp_item(input: &[u8]) -> Result<(&[u8], usize, bool), JournalError> {
    let first = *input.first().ok_or(JournalError::Corrupt)?;
    if first < 128 {
        return Ok((&input[..1], 1, false));
    }
    let list = first >= 192;
    let base = if list { 192 } else { 128 };
    let tag = usize::from(first - base);
    let (length, offset) = if tag < 56 {
        (tag, 1)
    } else {
        let count = tag - 55;
        if count > std::mem::size_of::<usize>() {
            return Err(JournalError::Corrupt);
        }
        let encoded = input.get(1..1 + count).ok_or(JournalError::Corrupt)?;
        if encoded.first() == Some(&0) {
            return Err(JournalError::Corrupt);
        }
        let length = encoded.iter().try_fold(0usize, |n, b| {
            n.checked_mul(256)
                .and_then(|n| n.checked_add(usize::from(*b)))
                .ok_or(JournalError::Corrupt)
        })?;
        if length < 56 {
            return Err(JournalError::Corrupt);
        }
        (length, 1 + count)
    };
    let end = offset.checked_add(length).ok_or(JournalError::Corrupt)?;
    let value = input.get(offset..end).ok_or(JournalError::Corrupt)?;
    if !list && length == 1 && value[0] < 128 {
        return Err(JournalError::Corrupt);
    }
    Ok((value, end, list))
}

pub(crate) fn validate_publication_transaction(
    transaction: &PublicationTransaction,
) -> Result<(), JournalError> {
    let p = transaction.publication;
    if transaction.version != 2
        || transaction.chain_id == 0
        || transaction.paymaster == [0; 20]
        || p.owner == [0; 20]
        || p.hash == [0; 32]
        || p.rate == [0; 32]
        || p.signed_at == 0
        || transaction.raw.len() > 1024
        || transaction.raw.first() != Some(&2)
        || keccak(&transaction.raw) != p.hash
    {
        return Err(JournalError::Corrupt);
    }
    let (payload, used, list) = rlp_item(&transaction.raw[1..])?;
    if !list || used != transaction.raw.len() - 1 {
        return Err(JournalError::Corrupt);
    }
    let mut fields = Vec::new();
    let mut offset = 0;
    let mut unsigned_end = 0;
    while offset < payload.len() {
        let (value, count, list) = rlp_item(&payload[offset..])?;
        fields.push((value, list));
        offset += count;
        if fields.len() == 9 {
            unsigned_end = offset;
        }
        if fields.len() > 12 {
            return Err(JournalError::Corrupt);
        }
    }
    if fields.len() != 12
        || !fields[8].1
        || !fields[8].0.is_empty()
        || fields
            .iter()
            .enumerate()
            .any(|(i, (_, list))| *list && i != 8)
    {
        return Err(JournalError::Corrupt);
    }
    let number = |index: usize| -> Result<u128, JournalError> {
        let bytes = fields[index].0;
        if bytes.len() > 16 || bytes.first() == Some(&0) {
            return Err(JournalError::Corrupt);
        }
        Ok(bytes.iter().fold(0u128, |n, b| (n << 8) | u128::from(*b)))
    };
    let recipient = if transaction.cancellation {
        p.owner
    } else {
        transaction.paymaster
    };
    let data = if transaction.cancellation {
        Vec::new()
    } else {
        [&keccak(b"setRate(uint256)")[..4], p.rate.as_slice()].concat()
    };
    if number(0)? != u128::from(transaction.chain_id)
        || number(1)? != u128::from(p.nonce)
        || number(2)? != transaction.max_priority_fee_per_gas
        || number(3)? != p.max_fee_per_gas
        || number(4)? != u128::from(p.gas_limit)
        || fields[5].0 != recipient
        || number(6)? != 0
        || fields[7].0 != data
        || number(9)? > 1
        || p.gas_limit
            != if transaction.cancellation {
                tx::CANCELLATION_GAS
            } else {
                RATE_GAS_LIMIT
            }
        || transaction.max_priority_fee_per_gas > p.max_fee_per_gas
        || p.max_fee_per_gas == 0
    {
        return Err(JournalError::Corrupt);
    }
    let mut signature = [0u8; 65];
    signature[64] = 27 + u8::try_from(number(9)?).map_err(|_| JournalError::Corrupt)?;
    for (index, end) in [(10, 32usize), (11, 64usize)] {
        let bytes = fields[index].0;
        if bytes.is_empty() || bytes.len() > 32 || bytes[0] == 0 {
            return Err(JournalError::Corrupt);
        }
        signature[end - bytes.len()..end].copy_from_slice(bytes);
    }
    let unsigned = unsigned_envelope(&payload[..unsigned_end])?;
    if tx::recover(keccak(&unsigned), &signature).map_err(|_| JournalError::Corrupt)? != p.owner {
        return Err(JournalError::Corrupt);
    }
    Ok(())
}

/// The type-2 signing preimage: 0x02 and the RLP list of the unsigned fields.
fn unsigned_envelope(fields: &[u8]) -> Result<Vec<u8>, JournalError> {
    let encoded_length = fields.len().to_be_bytes();
    let mut unsigned = vec![2];
    if fields.len() < 56 {
        unsigned.push(192 + u8::try_from(fields.len()).map_err(|_| JournalError::Corrupt)?);
    } else {
        let skip = encoded_length.iter().take_while(|b| **b == 0).count();
        unsigned.push(
            247 + u8::try_from(encoded_length.len() - skip).map_err(|_| JournalError::Corrupt)?,
        );
        unsigned.extend_from_slice(&encoded_length[skip..]);
    }
    unsigned.extend_from_slice(fields);
    Ok(unsigned)
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
