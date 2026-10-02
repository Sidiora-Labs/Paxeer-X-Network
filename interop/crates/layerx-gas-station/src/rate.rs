use std::io::{ErrorKind, Read as _};
use std::path::Path;
use std::time::{Duration, Instant};

use serde_json::{json, Map, Value};

use crate::config::{
    cadence_within_rate_age, distinct_key_sources, field, object, read_text, ConfigError,
    StationConfig,
};
use crate::journal::{
    Entry, Family, Journal, JournalError, PreparedPublication, Publication, Replacement,
    Settlement, PUBLICATION_VERSION,
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
    Cancelled { nonce: u64 },
    Unresolved { hash: Word, nonce: u64 },
    IdentityChanged { hash: Word },
    NonceConsumed { nonce: u64 },
    UncertainFinality { hash: Word },
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
/// (two cadences strictly below `max_rate_age`), the daily gas budget of publications, the
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
    /// Names the first field that is inconsistent: a cadence whose
    /// publish-and-confirm worst case is not below `max_rate_age`, a budget below one publication, a zero floor, a fee
    /// ceiling not above the priority fee or whose daily worst case overflows,
    /// or an owner key env that is invalid, the sponsor's or holds the
    /// sponsor's key.
    pub fn validate(&self) -> Result<(), ConfigError> {
        self.station.validate()?;
        let checks = [
            (
                "rate_owner_key_env",
                !self.owner_key_env.is_empty()
                    && distinct_key_sources(&self.station.relayer_key_env, &self.owner_key_env)
                    && self.owner_key_env.bytes().enumerate().all(|(i, b)| {
                        b == b'_' || b.is_ascii_uppercase() || (i > 0 && b.is_ascii_digit())
                    }),
            ),
            (
                "rate_cadence_seconds",
                cadence_within_rate_age(self.cadence_seconds, self.station.max_rate_age),
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
/// paymaster owner's key, and journals every publication's exact signed
/// bytes before broadcast.
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

    /// Recovers every unresolved publication, then publishes the rate file's
    /// rate once, waits for its finalized receipt and returns the journalled
    /// publication.
    /// # Errors
    /// Refuses an unresolved legacy publication without a receipt, a prepared
    /// publication journalled for another owner, chain or paymaster, malformed
    /// journalled bytes, a nonce consumed by a transaction the journal does not
    /// hold, uncertain finality, a missing, malformed, zero, not yet set or
    /// expired rate file, a rate unchanged on chain within the cadence, a signer
    /// that is not the paymaster owner, an exhausted daily gas budget, a fee per
    /// gas above the ceiling, an owner balance that would fall below its floor
    /// once this publication and every unsettled one pay their most, a reverted,
    /// cancelled or unconfirmed publication, and node or journal failures. No
    /// new publication is constructed while an earlier one is unresolved, and a
    /// refusal never deletes journal history. A refusal never publishes, so the
    /// paymaster's rate ages until `currentRate` reverts with `StaleRate`.
    pub fn publish(&mut self, rate_file: &Path) -> Result<Publication, RateRefusal> {
        let file = RateFile::load(rate_file);
        let head: Value = self
            .rpc
            .call("eth_getBlockByNumber", json!(["latest", false]))?;
        let now =
            u64::try_from(field_quantity(&head, "timestamp")?).map_err(|_| RpcFault::Malformed)?;
        let base_fee = field_quantity(&head, "baseFeePerGas")?;
        let withdrawn = match &file {
            Ok(file) => file.set_at > now || file.not_after.is_some_and(|t| now >= t),
            Err(_) => true,
        };
        if let Some(recovered) = self.recover(base_fee, withdrawn)? {
            return Ok(recovered);
        }
        let file = file?;
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
        let signer = self.owner()?;
        let spent = self.journal.state().publication_gas(now / DAY_SECONDS);
        if spent + u128::from(RATE_GAS_LIMIT) > self.config.gas_budget_per_day {
            return Err(RateRefusal::BudgetExhausted);
        }
        let max_fee_per_gas = self.fee_for(base_fee)?;
        self.funded(signer, max_fee_per_gas)?;
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
        let prepared = self.prepare(nonce, fees, file.rate, now)?;
        self.journal.append(&Entry::RatePrepared {
            prepared: prepared.clone(),
        })?;
        self.send(&prepared.publication.hash, &prepared.raw)?;
        self.settle(nonce)
    }

    /// The paymaster owner, refused unless it is this publisher's signer.
    fn owner(&self) -> Result<Address, RateRefusal> {
        let owner = self.paymaster_word(b"owner()")?;
        let signer: Address = self.signer.address();
        if owner[..12] != [0; 12] || owner[12..] != signer {
            return Err(RateRefusal::NotOwner);
        }
        Ok(signer)
    }

    /// The fee per gas cap for `base_fee`: twice the base fee plus the
    /// priority fee, refused above the configured ceiling.
    fn fee_for(&self, base_fee: u128) -> Result<u128, RateRefusal> {
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
        Ok(max_fee_per_gas)
    }

    /// Refuses unless the owner's pending balance covers `max_fee_per_gas`
    /// times the publication gas limit on top of every reservation and the floor.
    fn funded(&self, owner: Address, max_fee_per_gas: u128) -> Result<(), RateRefusal> {
        let balance: String = read(&self.rpc, "eth_getBalance", json!([hex(&owner), "pending"]))?;
        let balance = quantity(&balance)?;
        let required = max_fee_per_gas
            .checked_mul(u128::from(RATE_GAS_LIMIT))
            .and_then(|cost| cost.checked_add(self.journal.state().reserved_wei()))
            .and_then(|cost| cost.checked_add(self.config.balance_floor));
        if required.is_none_or(|required| balance < required) {
            return Err(RateRefusal::BelowFloor);
        }
        Ok(())
    }

    /// Signs setRate(`rate`) at `nonce` and returns the versioned record that
    /// holds its exact signed bytes and identity; it carries no key material.
    fn prepare(
        &self,
        nonce: u64,
        fees: Fees,
        rate: u128,
        signed_at: u64,
    ) -> Result<PreparedPublication, RateRefusal> {
        let data = [&keccak(b"setRate(uint256)")[..4], word(rate).as_slice()].concat();
        let signed = tx::sign_call(
            self.config.station.chain_id,
            nonce,
            fees,
            self.config.station.paymaster,
            &data,
            &self.signer,
        )?;
        Ok(PreparedPublication {
            version: PUBLICATION_VERSION,
            chain_id: self.config.station.chain_id,
            paymaster: self.config.station.paymaster,
            publication: Publication {
                owner: self.signer.address(),
                nonce,
                hash: signed.hash,
                rate: word(rate),
                gas_limit: fees.gas_limit,
                max_fee_per_gas: fees.max_fee_per_gas,
                signed_at,
            },
            max_priority_fee_per_gas: fees.max_priority_fee_per_gas,
            raw: signed.raw,
        })
    }

    /// Resolves journalled publications before any new one is constructed:
    /// settles legacy ones by receipt or refuses, then checks the identity
    /// and bytes of the oldest unresolved family, settles it by any member's
    /// finalized receipt, and otherwise rebroadcasts its exact journalled
    /// bytes (replaced at the bounded fee or, when the owner's rate is
    /// withdrawn, cancelled) and waits for its outcome. Returns the
    /// publication a recovered family finalized with a successful setRate.
    fn recover(
        &mut self,
        base_fee: u128,
        withdrawn: bool,
    ) -> Result<Option<Publication>, RateRefusal> {
        for pending in self.journal.state().legacy_unresolved() {
            let receipt = self
                .rpc
                .call("eth_getTransactionReceipt", json!([hex(&pending.hash)]))?;
            if receipt.is_null() || !self.finalized(&receipt)? {
                return Err(RateRefusal::Unresolved {
                    hash: pending.hash,
                    nonce: pending.nonce,
                });
            }
            self.record(
                pending.hash,
                pending.owner,
                self.config.station.paymaster,
                pending.max_fee_per_gas,
                &receipt,
            )?;
        }
        let mut recovered = None;
        while let Some(family) = self.journal.state().unresolved().cloned() {
            let latest = family.latest().clone();
            let owner = self.signer.address();
            for member in &family.members {
                if member.version != PUBLICATION_VERSION
                    || member.chain_id != self.config.station.chain_id
                    || member.paymaster != self.config.station.paymaster
                    || member.publication.owner != owner
                    || member.publication.nonce != latest.publication.nonce
                {
                    return Err(RateRefusal::IdentityChanged {
                        hash: member.publication.hash,
                    });
                }
                if member.raw.first() != Some(&2) || keccak(&member.raw) != member.publication.hash
                {
                    return Err(RpcFault::Malformed.into());
                }
            }
            if let Some(cancellation) = &family.cancellation {
                if cancellation.nonce != latest.publication.nonce
                    || cancellation.raw.first() != Some(&2)
                    || keccak(&cancellation.raw) != cancellation.hash
                {
                    return Err(RpcFault::Malformed.into());
                }
            }
            self.owner()?;
            let nonce = latest.publication.nonce;
            if self.finalize(&family)?.is_none() {
                if family.cancellation.is_none() && withdrawn {
                    self.cancel(&latest)?;
                } else if family.cancellation.is_none()
                    && matches!(self.fee_for(base_fee), Ok(fee) if fee > latest.publication.max_fee_per_gas)
                {
                    self.replace(&latest, base_fee)?;
                } else {
                    let (hash, raw) = family.cancellation.as_ref().map_or(
                        (latest.publication.hash, latest.raw.clone()),
                        |c| (c.hash, c.raw.clone()),
                    );
                    self.send(&hash, &raw)?;
                }
            }
            recovered = Some(self.settle(nonce)?);
        }
        Ok(recovered)
    }

    /// Signs and journals a replacement of `latest` at its nonce with the fee
    /// cap the head now needs, raised at least by the replacement bump and
    /// bounded by the fee ceiling, then broadcasts it.
    fn replace(&mut self, latest: &PreparedPublication, base_fee: u128) -> Result<(), RateRefusal> {
        let original = Fees {
            gas_limit: latest.publication.gas_limit,
            max_fee_per_gas: latest.publication.max_fee_per_gas,
            max_priority_fee_per_gas: latest.max_priority_fee_per_gas,
        };
        let floor = original.replacement()?;
        let fees = Fees {
            gas_limit: latest.publication.gas_limit,
            max_fee_per_gas: self.fee_for(base_fee)?.max(floor.max_fee_per_gas),
            max_priority_fee_per_gas: self
                .config
                .max_priority_fee_per_gas
                .max(floor.max_priority_fee_per_gas),
        };
        if fees.max_fee_per_gas > self.config.max_fee_per_gas {
            return Err(RateRefusal::FeeAboveCeiling {
                required: fees.max_fee_per_gas,
                ceiling: self.config.max_fee_per_gas,
            });
        }
        let rate = u128::from_be_bytes(
            latest.publication.rate[16..]
                .try_into()
                .map_err(|_| RpcFault::Malformed)?,
        );
        let prepared = self.prepare(latest.publication.nonce, fees, rate, latest.publication.signed_at)?;
        self.journal.append(&Entry::RateReplaced {
            previous: latest.publication.hash,
            prepared: prepared.clone(),
        })?;
        self.send(&prepared.publication.hash, &prepared.raw)
    }

    /// Signs and journals the zero-value self-transfer that fills the nonce of
    /// `latest` at the cancellation fees, bounded by the fee ceiling, then
    /// broadcasts it.
    fn cancel(&mut self, latest: &PreparedPublication) -> Result<(), RateRefusal> {
        let fees = Fees {
            gas_limit: latest.publication.gas_limit,
            max_fee_per_gas: latest.publication.max_fee_per_gas,
            max_priority_fee_per_gas: latest.max_priority_fee_per_gas,
        }
        .replacement()?;
        if fees.max_fee_per_gas > self.config.max_fee_per_gas {
            return Err(RateRefusal::FeeAboveCeiling {
                required: fees.max_fee_per_gas,
                ceiling: self.config.max_fee_per_gas,
            });
        }
        let signed = tx::sign_cancellation(
            self.config.station.chain_id,
            latest.publication.nonce,
            fees,
            &self.signer,
        )?;
        let cancellation = Replacement {
            nonce: latest.publication.nonce,
            fees,
            hash: signed.hash,
            raw: signed.raw,
        };
        self.journal.append(&Entry::RateCancelled {
            previous: latest.publication.hash,
            cancellation: cancellation.clone(),
        })?;
        self.send(&cancellation.hash, &cancellation.raw)
    }

    /// Broadcasts the exact journalled bytes of `hash` and journals the
    /// broadcast once the node accepted them or already holds them; a
    /// duplicate broadcast of the same hash is an idempotent journal state.
    /// A node that refuses bytes it does not hold refuses the publication and
    /// keeps the prepared record for the next recovery.
    fn send(&mut self, hash: &Word, raw: &[u8]) -> Result<(), RateRefusal> {
        if raw.first() != Some(&2) || keccak(raw) != *hash {
            return Err(RpcFault::Malformed.into());
        }
        match self.rpc.call("eth_sendRawTransaction", json!([hex(raw)])) {
            Ok(accepted) if accepted == json!(hex(hash)) => (),
            Ok(_) => return Err(RpcFault::Malformed.into()),
            Err(RpcFault::Configuration) => return Err(RpcFault::Configuration.into()),
            Err(fault) => {
                let known = self
                    .rpc
                    .call("eth_getTransactionByHash", json!([hex(hash)]))?;
                if known.is_null() {
                    return Err(fault.into());
                }
                if known["hash"] != hex(hash) {
                    return Err(RpcFault::Malformed.into());
                }
            }
        }
        self.journal.append(&Entry::RateBroadcast { hash: *hash })?;
        Ok(())
    }

    /// Settles the family at `nonce` by any member's finalized receipt and
    /// returns the outcome; `None` while no member has one.
    /// # Errors
    /// Refuses a nonce the owner consumed with a transaction the family does
    /// not hold, and malformed or uncertain receipts.
    fn finalize(&mut self, family: &Family) -> Result<Option<Outcome>, RateRefusal> {
        let latest = family.latest();
        let owner = latest.publication.owner;
        let mut candidates: Vec<(Word, Address, u128)> = family
            .members
            .iter()
            .map(|m| {
                (
                    m.publication.hash,
                    self.config.station.paymaster,
                    m.publication.max_fee_per_gas,
                )
            })
            .collect();
        if let Some(c) = &family.cancellation {
            candidates.push((c.hash, owner, c.fees.max_fee_per_gas));
        }
        for (hash, to, max_fee) in candidates {
            let receipt = self.rpc.call("eth_getTransactionReceipt", json!([hex(&hash)]))?;
            if receipt.is_null() {
                continue;
            }
            if !self.finalized(&receipt)? {
                return Err(RateRefusal::UncertainFinality { hash });
            }
            let settlement = self.record(hash, owner, to, max_fee, &receipt)?;
            let cancelled = family.cancellation.as_ref().is_some_and(|c| c.hash == hash);
            return Ok(Some(if cancelled {
                Outcome::Cancelled
            } else if settlement.succeeded {
                let member = family
                    .members
                    .iter()
                    .find(|m| m.publication.hash == hash)
                    .ok_or(RpcFault::Malformed)?;
                Outcome::Published(member.publication)
            } else {
                Outcome::Reverted
            }));
        }
        let mined: String = read(
            &self.rpc,
            "eth_getTransactionCount",
            json!([hex(&owner), "latest"]),
        )?;
        if quantity(&mined)? > u128::from(latest.publication.nonce) {
            return Err(RateRefusal::NonceConsumed {
                nonce: latest.publication.nonce,
            });
        }
        Ok(None)
    }

    /// Whether the receipt's block is canonical (the node's block at that
    /// number has the receipt's block hash) and at or below the finalized head.
    fn finalized(&self, receipt: &Value) -> Result<bool, RateRefusal> {
        let number = field_quantity(receipt, "blockNumber")?;
        let block = self.rpc.call(
            "eth_getBlockByNumber",
            json!([format!("{number:#x}"), false]),
        )?;
        if block.is_null() || block["hash"].is_null() || receipt["blockHash"].is_null() {
            return Ok(false);
        }
        if block["hash"] != receipt["blockHash"] {
            return Ok(false);
        }
        let finalized = self
            .rpc
            .call("eth_getBlockByNumber", json!(["finalized", false]))?;
        if finalized.is_null() {
            return Ok(false);
        }
        Ok(field_quantity(&finalized, "number")? >= number)
    }

    /// Waits up to the cadence for the family at `nonce` to finalize.
    fn settle(&mut self, nonce: u64) -> Result<Publication, RateRefusal> {
        let deadline = Instant::now() + Duration::from_secs(self.config.cadence_seconds);
        let owner = self.signer.address();
        loop {
            let family = self
                .journal
                .state()
                .families
                .get(&(owner, nonce))
                .cloned()
                .ok_or(JournalError::Conflict)?;
            match self.finalize(&family) {
                Ok(Some(Outcome::Published(publication))) => return Ok(publication),
                Ok(Some(Outcome::Reverted)) => return Err(RateRefusal::Reverted),
                Ok(Some(Outcome::Cancelled)) => return Err(RateRefusal::Cancelled { nonce }),
                Ok(None) | Err(RateRefusal::UncertainFinality { .. }) if Instant::now() < deadline => (),
                Ok(None) => return Err(RateRefusal::ReceiptTimeout),
                Err(error) => return Err(error),
            }
            std::thread::sleep(self.poll);
        }
    }

    /// Journals the settlement the receipt reports, its cost the effective
    /// gas price times the gas used.
    fn record(
        &mut self,
        hash: Word,
        from: Address,
        to: Address,
        max_fee_per_gas: u128,
        receipt: &Value,
    ) -> Result<Settlement, RateRefusal> {
        if receipt["transactionHash"] != hex(&hash)
            || receipt["from"] != hex(&from)
            || receipt["to"] != hex(&to)
        {
            return Err(RpcFault::Malformed.into());
        }
        let gas_used =
            u64::try_from(field_quantity(receipt, "gasUsed")?).map_err(|_| RpcFault::Malformed)?;
        let price = field_quantity(receipt, "effectiveGasPrice")?;
        if price > max_fee_per_gas {
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
        self.journal.append(&Entry::RateSettled { hash, settlement })?;
        Ok(settlement)
    }
}

/// How a finalized publication family ended.
enum Outcome {
    Published(Publication),
    Reverted,
    Cancelled,
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
