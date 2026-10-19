use std::collections::{BTreeMap, BTreeSet};
use std::fs::{File, OpenOptions};
use std::io::{BufRead as _, BufReader, Write as _};
use std::os::unix::fs::MetadataExt as _;
use std::os::unix::fs::OpenOptionsExt as _;
use std::path::Path;

use serde::{Deserialize, Serialize};

use crate::quote::{keccak, quote_digest, word, Address, Quote, Word};
use crate::tx::{recover, Fees};
use crate::SignedQuote;

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum JournalError {
    Io,
    Corrupt,
    Conflict,
    Locked,
    Version,
}
impl std::fmt::Display for JournalError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::Version => f.write_str(
                "journal refused: written by an earlier version that recorded same-nonce replacements",
            ),
            _ => write!(f, "journal refused: {self:?}"),
        }
    }
}
impl std::error::Error for JournalError {}

#[derive(Clone, Copy, Debug, Deserialize, Serialize, Eq, PartialEq, Ord, PartialOrd)]
#[serde(deny_unknown_fields)]
pub struct Key {
    pub sponsor: Address,
    pub quote_nonce: Word,
}

#[derive(Clone, Debug, Deserialize, Serialize, Eq, PartialEq)]
#[serde(deny_unknown_fields)]
pub struct QuoteRecord {
    pub key: Key,
    pub did: String,
    pub batch_nonce: Word,
    pub chain_id: u64,
    pub paymaster: Address,
    pub account: Address,
    pub token: Address,
    pub maximum: Word,
    pub amount: Word,
    pub deadline: u64,
    pub gas_cost: Word,
    pub issued_at: u64,
    pub signature: Vec<u8>,
    pub fees: Fees,
}
impl QuoteRecord {
    /// The budget key the quote was charged to, from which a restart rebuilds
    /// per-DID usage.
    ///
    /// # Errors
    /// Refuses a record whose key is neither a DID nor its own account.
    pub fn budget_key(&self) -> Result<crate::policy::BudgetKey, JournalError> {
        crate::policy::BudgetKey::parse(&self.did)
            .ok()
            .filter(|key| {
                key.as_str().starts_with("did:layerx:")
                    || key.as_str() == crate::rpc::hex(&self.account)
            })
            .ok_or(JournalError::Corrupt)
    }

    /// # Errors
    /// Refuses malformed or incorrectly signed records.
    pub fn signed_quote(&self) -> Result<SignedQuote, JournalError> {
        let quote = Quote {
            sponsor: self.key.sponsor,
            token: self.token,
            max_token_amount: self.maximum,
            token_amount: self.amount,
            deadline: word(u128::from(self.deadline)),
            nonce: self.key.quote_nonce,
            gas_cost: self.gas_cost,
        };
        let digest = quote_digest(word(u128::from(self.chain_id)), self.account, &quote);
        let signature = self
            .signature
            .as_slice()
            .try_into()
            .map_err(|_| JournalError::Corrupt)?;
        if self.budget_key().is_err()
            || recover(digest, &signature).map_err(|_| JournalError::Corrupt)? != self.key.sponsor
            || self.amount == word(0)
            || self.amount > self.maximum
            || self.deadline < self.issued_at
            || self.gas_cost != word(self.fees.gas_cost().map_err(|_| JournalError::Corrupt)?)
        {
            return Err(JournalError::Corrupt);
        }
        Ok(SignedQuote {
            quote,
            digest,
            signature,
        })
    }
}
#[derive(Clone, Debug, Deserialize, Serialize, Eq, PartialEq)]
#[serde(deny_unknown_fields)]
pub struct Submission {
    pub did: String,
    pub batch_nonce: Word,
    pub nonce: u64,
    pub hash: Word,
    pub raw: Vec<u8>,
}

#[derive(Clone, Copy, Debug, Deserialize, Serialize, Eq, PartialEq)]
#[serde(tag = "outcome", rename_all = "snake_case", deny_unknown_fields)]
pub enum Completion {
    Consumed,
    Included {
        hash: Word,
        block_number: u64,
        sid_collected: Word,
        pax_spent: Word,
    },
    Reverted {
        hash: Word,
    },
    Cancelled {
        hash: Word,
        block_number: u64,
    },
}
#[derive(Clone, Debug, Deserialize, Serialize, Eq, PartialEq)]
#[serde(tag = "reason", rename_all = "snake_case", deny_unknown_fields)]
pub enum LiabilityRelease {
    Expired {
        canonical: serde_json::Value,
        finalized: serde_json::Value,
    },
    Settled {
        hash: Word,
    },
    Refused {
        hash: Word,
    },
    Consumed {
        chain_id: u64,
        account: Address,
        paymaster: Address,
        quote_nonce: Word,
        canonical: serde_json::Value,
        finalized: serde_json::Value,
        code: String,
        params: serde_json::Value,
        result: serde_json::Value,
    },
}
#[derive(Clone, Copy, Debug, Deserialize, Serialize, Eq, PartialEq)]
#[serde(deny_unknown_fields)]
pub struct Admission {
    pub identity: Word,
    pub request_digest: Word,
    pub batch_nonce: Word,
    pub interval: u64,
}
impl Admission {
    #[must_use]
    pub fn identity(account: Address, nonce: Word, interval: u64) -> Word {
        keccak(
            &[
                b"paxeer-gas-admission-v1".as_slice(),
                &account,
                &nonce,
                &interval.to_be_bytes(),
            ]
            .concat(),
        )
    }
}
#[derive(Clone, Debug, Deserialize, Serialize, Eq, PartialEq)]
#[serde(tag = "kind", rename_all = "snake_case", deny_unknown_fields)]
pub enum Entry {
    QuoteReserved {
        quote: Box<QuoteRecord>,
    },
    SigningIntent {
        key: Key,
        nonce: u64,
        digest: Word,
    },
    QuoteAdmitted {
        quote: Box<QuoteRecord>,
        admission: Admission,
    },
    LiabilityReleased {
        key: Key,
        proof: LiabilityRelease,
    },
    Quoted {
        quote: Box<QuoteRecord>,
    },
    Prepared {
        key: Key,
        submission: Submission,
    },
    Completed {
        key: Key,
        account: Address,
        completion: Completion,
    },
    Refused {
        key: Key,
        hash: Word,
    },
    ReceiptObserved {
        key: Key,
        hash: Word,
        receipt: serde_json::Value,
        canonical: serde_json::Value,
        finalized: serde_json::Value,
    },
    RatePublished {
        publication: Publication,
    },
    RateSettled {
        hash: Word,
        settlement: Settlement,
    },
    RatePrepared {
        transaction: PublicationTransaction,
    },
    RateBroadcast {
        hash: Word,
    },
    RateReplaced {
        previous: Word,
        transaction: PublicationTransaction,
    },
    RateFinalized {
        hash: Word,
        settlement: Settlement,
        receipt: serde_json::Value,
        canonical: serde_json::Value,
        finalized: serde_json::Value,
    },
}
#[derive(Clone, Debug, Deserialize, Serialize, Eq, PartialEq)]
#[serde(deny_unknown_fields)]
pub struct PublicationTransaction {
    pub version: u16,
    pub publication: Publication,
    pub chain_id: u64,
    pub paymaster: Address,
    #[serde(with = "crate::tx::fee_amount")]
    pub max_priority_fee_per_gas: u128,
    pub raw: Vec<u8>,
    pub cancellation: bool,
}
/// One setRate transaction of the rate publisher, journalled before it is
/// broadcast: the owner's nonce, the transaction hash, the published rate in
/// SID base units per whole PAX, the gas limit it may spend, its fee per gas
/// ceiling in wei and the chain time it was signed at.
#[derive(Clone, Copy, Debug, Deserialize, Serialize, Eq, PartialEq)]
#[serde(deny_unknown_fields)]
pub struct Publication {
    pub owner: Address,
    pub nonce: u64,
    pub hash: Word,
    pub rate: Word,
    pub gas_limit: u64,
    #[serde(with = "crate::tx::fee_amount")]
    pub max_fee_per_gas: u128,
    pub signed_at: u64,
}
/// The receipt of a publication: its block, the gas it used, what it cost in
/// wei (effective gas price times gas used) and whether it succeeded.
#[derive(Clone, Copy, Debug, Deserialize, Serialize, Eq, PartialEq)]
#[serde(deny_unknown_fields)]
pub struct Settlement {
    pub block_number: u64,
    pub gas_used: u64,
    #[serde(with = "crate::tx::fee_amount")]
    pub cost_wei: u128,
    pub succeeded: bool,
}
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Item {
    pub account: Address,
    pub quote: Option<QuoteRecord>,
    pub submission: Option<Submission>,
    pub completion: Option<Completion>,
}
#[derive(Clone, Debug, Default, Eq, PartialEq)]
pub struct State {
    pub items: BTreeMap<Key, Item>,
    pub receipts: BTreeMap<Word, serde_json::Value>,
    pub publications: BTreeMap<Word, (Publication, Option<Settlement>)>,
    pub admissions: BTreeMap<Word, (Admission, Key)>,
    pub tracked_quotes: BTreeSet<Key>,
    pub signing_intents: BTreeMap<Key, u64>,
    pub liability_releases: BTreeMap<Key, LiabilityRelease>,
    pub rate_transactions: BTreeMap<Word, PublicationTransaction>,
    pub rate_active: BTreeMap<(Address, u64), Word>,
    pub rate_broadcast: BTreeSet<Word>,
}
impl State {
    /// The gas the rate publisher spent or reserved on the chain day `day`
    /// (chain time divided by 86400): the gas used of each settled
    /// publication and the gas limit of each unsettled one.
    #[must_use]
    pub fn publication_gas(&self, day: u64) -> u128 {
        let mut groups = BTreeMap::<(Address, u64), u128>::new();
        for (p, _) in self
            .publications
            .values()
            .filter(|(p, _)| p.signed_at / 86_400 == day)
        {
            let settled = self.publications.values().find_map(|(q, s)| {
                (q.owner == p.owner && q.nonce == p.nonce)
                    .then_some(*s)
                    .flatten()
            });
            let gas = u128::from(settled.map_or(p.gas_limit, |s| s.gas_used));
            groups
                .entry((p.owner, p.nonce))
                .and_modify(|value| *value = (*value).max(gas))
                .or_insert(gas);
        }
        groups.values().copied().fold(0, u128::saturating_add)
    }
    /// The wei the rate publisher spent on the chain day `day`: the cost of
    /// each settled publication signed that day.
    #[must_use]
    pub fn publication_wei(&self, day: u64) -> u128 {
        self.publications
            .values()
            .filter(|(p, _)| p.signed_at / 86_400 == day)
            .filter_map(|(_, s)| s.map(|s| s.cost_wei))
            .fold(0, u128::saturating_add)
    }
    /// The publications still able to spend: unsettled, with no settled
    /// publication of the same owner at their nonce or above.
    #[must_use]
    pub fn unsettled(&self) -> Vec<Publication> {
        self.publications
            .values()
            .filter(|(p, s)| {
                s.is_none()
                    && !self
                        .publications
                        .values()
                        .any(|(q, t)| t.is_some() && q.owner == p.owner && q.nonce == p.nonce)
            })
            .map(|(p, _)| *p)
            .collect()
    }
    /// The wei reserved by unsettled publications: each one's fee per gas
    /// ceiling times its gas limit.
    #[must_use]
    pub fn reserved_wei(&self) -> u128 {
        let mut groups = BTreeMap::<(Address, u64), u128>::new();
        for p in self.unsettled() {
            let liability = p.max_fee_per_gas.saturating_mul(u128::from(p.gas_limit));
            groups
                .entry((p.owner, p.nonce))
                .and_modify(|value| *value = (*value).max(liability))
                .or_insert(liability);
        }
        groups.values().copied().fold(0, u128::saturating_add)
    }
    /// Whether the sequencer refused the submission of `key`, or held no
    /// record of it past its quote deadline: its sponsor nonce is free.
    #[must_use]
    pub fn refused(&self, key: &Key) -> bool {
        matches!(
            self.liability_releases.get(key),
            Some(LiabilityRelease::Refused { .. })
        )
    }
    /// Whether a signing intent or a submission of `sponsor` that the
    /// sequencer did not refuse holds `nonce`.
    #[must_use]
    pub fn holds(&self, sponsor: Address, nonce: u64) -> bool {
        self.items.iter().any(|(key, item)| {
            key.sponsor == sponsor
                && !self.refused(key)
                && (self
                    .signing_intents
                    .get(key)
                    .is_some_and(|saved| *saved == nonce)
                    || item.submission.as_ref().is_some_and(|s| s.nonce == nonce))
        })
    }
    #[must_use]
    pub fn next_nonce(&self, sponsor: Address, pending: u64) -> Option<u64> {
        if self.signing_intents.iter().any(|(key, nonce)| {
            key.sponsor == sponsor
                && *nonce >= pending
                && self
                    .items
                    .get(key)
                    .is_none_or(|item| item.submission.is_none())
        }) {
            return None;
        }
        let mut nonce = pending;
        while self.holds(sponsor, nonce) {
            nonce = nonce.checked_add(1)?;
        }
        nonce.checked_add(1)?;
        Some(nonce)
    }

    /// The token amount and gas limit quoted under the budget key `did` in
    /// the interval `interval` of `interval_seconds` seconds, summed over
    /// every journalled quote issued in it.
    /// # Errors
    /// Refuses a zero interval length, an amount wider than 128 bits or an
    /// overflowing total.
    pub fn did_spend(
        &self,
        did: &str,
        interval_seconds: u64,
        interval: u64,
    ) -> Result<(u128, u64), JournalError> {
        if interval_seconds == 0 {
            return Err(JournalError::Corrupt);
        }
        self.items
            .values()
            .filter_map(|item| item.quote.as_ref())
            .filter(|quote| quote.did == did && quote.issued_at / interval_seconds == interval)
            .try_fold((0u128, 0u64), |(amount, gas), quote| {
                if quote.amount[..16] != [0; 16] {
                    return Err(JournalError::Corrupt);
                }
                let mut low = [0; 16];
                low.copy_from_slice(&quote.amount[16..]);
                Ok((
                    amount
                        .checked_add(u128::from_be_bytes(low))
                        .ok_or(JournalError::Corrupt)?,
                    gas.checked_add(quote.fees.gas_limit)
                        .ok_or(JournalError::Corrupt)?,
                ))
            })
    }

    /// # Errors
    /// Refuses a corrupt fee record.
    pub fn liability(&self, key: Key) -> Result<u128, JournalError> {
        if self.liability_releases.contains_key(&key) {
            return Ok(0);
        }
        let item = self.items.get(&key).ok_or(JournalError::Conflict)?;
        let Some(quote) = &item.quote else {
            return Ok(0);
        };
        quote.fees.gas_cost().map_err(|_| JournalError::Corrupt)
    }

    /// # Errors
    /// Refuses a corrupt fee record or an overflowing total.
    pub fn active_pax(&self) -> Result<u128, JournalError> {
        self.items.keys().try_fold(0u128, |sum, key| {
            sum.checked_add(self.liability(*key)?)
                .ok_or(JournalError::Corrupt)
        })
    }

    /// # Errors
    /// Refuses a corrupt settlement record or an overflowing total.
    pub fn finalized_spend(&self) -> Result<u128, JournalError> {
        self.liability_releases
            .iter()
            .try_fold(0u128, |sum, (key, proof)| {
                let spent = match proof {
                    LiabilityRelease::Expired { .. }
                    | LiabilityRelease::Refused { .. }
                    | LiabilityRelease::Consumed { .. } => 0,
                    LiabilityRelease::Settled { hash } => self.settlement_cost(*key, *hash)?,
                };
                sum.checked_add(spent).ok_or(JournalError::Corrupt)
            })
    }

    fn settlement_cost(&self, key: Key, hash: Word) -> Result<u128, JournalError> {
        let item = self.items.get(&key).ok_or(JournalError::Conflict)?;
        let quote = item.quote.as_ref().ok_or(JournalError::Conflict)?;
        let receipt = self.receipts.get(&hash).ok_or(JournalError::Conflict)?;
        let fees = quote.fees;
        let status = match item.completion {
            Some(Completion::Included { hash: saved, .. }) if saved == hash => 1,
            Some(Completion::Reverted { hash: saved }) if saved == hash => 0,
            _ => return Err(JournalError::Conflict),
        };
        let number = |name: &str| {
            crate::rpc::quantity(receipt[name].as_str().ok_or(JournalError::Corrupt)?)
                .map_err(|_| JournalError::Corrupt)
        };
        let gas = number("gasUsed")?;
        let price = number("effectiveGasPrice")?;
        if receipt["transactionHash"] != crate::rpc::hex(&hash)
            || receipt["from"] != crate::rpc::hex(&key.sponsor)
            || receipt["to"] != crate::rpc::hex(&quote.account)
            || number("status")? != status
            || gas == 0
            || gas > u128::from(fees.gas_limit)
            || price > fees.max_fee_per_gas
        {
            return Err(JournalError::Corrupt);
        }
        let spent = gas.checked_mul(price).ok_or(JournalError::Corrupt)?;
        if let Some(Completion::Included { pax_spent, .. }) = item.completion {
            if pax_spent != word(spent) {
                return Err(JournalError::Corrupt);
            }
        }
        Ok(spent)
    }

    fn apply(&mut self, entry: &Entry) -> Result<(), JournalError> {
        match entry {
            Entry::QuoteReserved { quote } => self.apply_quote_reserved(quote),
            Entry::SigningIntent { key, nonce, digest } => {
                self.apply_signing_intent(key, *nonce, digest)
            }
            Entry::Refused { key, hash } => self.apply_refused(key, hash),
            Entry::QuoteAdmitted { quote, admission } => {
                self.apply_quote_admitted(quote, admission)
            }
            Entry::LiabilityReleased { key, proof } => self.apply_liability_released(key, proof),
            Entry::Quoted { quote } => self.apply_quoted(quote),
            Entry::Prepared { key, submission } => self.apply_prepared(key, submission),
            Entry::Completed {
                key,
                account,
                completion,
            } => self.apply_completed(key, account, completion),
            Entry::ReceiptObserved {
                key,
                hash,
                receipt,
                canonical,
                finalized,
            } => self.apply_receipt_observed(key, hash, receipt, canonical, finalized),
            Entry::RatePublished { publication } => self.apply_rate_published(publication),
            Entry::RateSettled { hash, settlement } => self.apply_rate_settled(hash, settlement),
            Entry::RatePrepared { transaction } => self.apply_rate_prepared(transaction),
            Entry::RateBroadcast { hash } => self.apply_rate_broadcast(hash),
            Entry::RateReplaced {
                previous,
                transaction,
            } => self.apply_rate_replaced(previous, transaction),
            Entry::RateFinalized {
                hash,
                settlement,
                receipt,
                canonical,
                finalized,
            } => self.apply_rate_finalized(hash, settlement, receipt, canonical, finalized),
        }
    }

    fn apply_quote_reserved(&mut self, quote: &QuoteRecord) -> Result<(), JournalError> {
        self.apply_quoted(quote)?;
        self.tracked_quotes.insert(quote.key);
        Ok(())
    }

    fn apply_signing_intent(
        &mut self,
        key: &Key,
        nonce: u64,
        digest: &Word,
    ) -> Result<(), JournalError> {
        let item = self.items.get(key).ok_or(JournalError::Conflict)?;
        if *digest == [0; 32]
            || !self.tracked_quotes.contains(key)
            || self.signing_intents.contains_key(key)
            || self.liability_releases.contains_key(key)
            || item.completion.is_some()
            || item.submission.is_some()
            || self.holds(key.sponsor, nonce)
        {
            return Err(JournalError::Conflict);
        }
        self.signing_intents.insert(*key, nonce);
        Ok(())
    }

    fn apply_refused(&mut self, key: &Key, hash: &Word) -> Result<(), JournalError> {
        let item = self.items.get(key).ok_or(JournalError::Conflict)?;
        if item.quote.is_none()
            || item.completion.is_some()
            || item.submission.as_ref().is_none_or(|s| s.hash != *hash)
            || self.liability_releases.contains_key(key)
            || self.receipts.contains_key(hash)
        {
            return Err(JournalError::Conflict);
        }
        self.liability_releases
            .insert(*key, LiabilityRelease::Refused { hash: *hash });
        Ok(())
    }

    fn apply_quote_admitted(
        &mut self,
        quote: &QuoteRecord,
        admission: &Admission,
    ) -> Result<(), JournalError> {
        if admission.identity
            != Admission::identity(quote.account, admission.batch_nonce, admission.interval)
            || admission.request_digest == [0; 32]
            || self.admissions.contains_key(&admission.identity)
        {
            return Err(JournalError::Conflict);
        }
        self.apply_quoted(quote)?;
        self.admissions
            .insert(admission.identity, (*admission, quote.key));
        self.tracked_quotes.insert(quote.key);
        Ok(())
    }

    fn apply_liability_released(
        &mut self,
        key: &Key,
        proof: &LiabilityRelease,
    ) -> Result<(), JournalError> {
        if self.liability_releases.contains_key(key) {
            return Err(JournalError::Conflict);
        }
        let item = self.items.get(key).ok_or(JournalError::Conflict)?;
        let quote = item.quote.as_ref().ok_or(JournalError::Conflict)?;
        match proof {
            LiabilityRelease::Settled { hash } => {
                self.settlement_cost(*key, *hash)?;
            }
            LiabilityRelease::Refused { .. } => return Err(JournalError::Conflict),
            LiabilityRelease::Consumed {
                chain_id,
                account,
                paymaster,
                quote_nonce,
                canonical,
                finalized,
                code,
                params,
                result,
            } => {
                if !self.tracked_quotes.contains(key)
                    || self.signing_intents.contains_key(key)
                    || item.submission.is_some()
                    || item.completion != Some(Completion::Consumed)
                    || *chain_id != quote.chain_id
                    || *account != quote.account
                    || *paymaster != quote.paymaster
                    || *quote_nonce != key.quote_nonce
                {
                    return Err(JournalError::Conflict);
                }
                validate_consumed_proof(key, quote, canonical, finalized, code, params, result)?;
            }
            LiabilityRelease::Expired {
                canonical,
                finalized,
            } => {
                let number = |value: &serde_json::Value, field: &str| {
                    crate::rpc::quantity(value[field].as_str().ok_or(JournalError::Corrupt)?)
                        .map_err(|_| JournalError::Corrupt)
                };
                let hash =
                    crate::rpc::bytes(finalized["hash"].as_str().ok_or(JournalError::Corrupt)?)
                        .map_err(|_| JournalError::Corrupt)?;
                if !self.tracked_quotes.contains(key)
                    || self.signing_intents.contains_key(key)
                    || item.submission.is_some()
                    || hash.len() != 32
                    || hash.iter().all(|byte| *byte == 0)
                    || canonical["hash"] != finalized["hash"]
                    || number(canonical, "number")? != number(finalized, "number")?
                    || number(canonical, "timestamp")? != number(finalized, "timestamp")?
                    || number(finalized, "timestamp")? <= u128::from(quote.deadline)
                {
                    return Err(JournalError::Conflict);
                }
            }
        }
        self.liability_releases.insert(*key, proof.clone());
        Ok(())
    }

    fn apply_quoted(&mut self, quote: &QuoteRecord) -> Result<(), JournalError> {
        quote.signed_quote()?;
        if self.items.contains_key(&quote.key) {
            return Err(JournalError::Conflict);
        }
        self.items.insert(
            quote.key,
            Item {
                account: quote.account,
                quote: Some(quote.clone()),
                submission: None,
                completion: None,
            },
        );
        Ok(())
    }

    fn apply_prepared(&mut self, key: &Key, submission: &Submission) -> Result<(), JournalError> {
        if self.liability_releases.contains_key(key) {
            return Err(JournalError::Conflict);
        }
        let quote = self
            .items
            .get(key)
            .and_then(|item| item.quote.as_ref())
            .ok_or(JournalError::Conflict)?;
        if submission.did != quote.did || submission.batch_nonce != quote.batch_nonce {
            return Err(JournalError::Conflict);
        }
        if submission.raw.first() != Some(&4) || keccak(&submission.raw) != submission.hash {
            return Err(JournalError::Corrupt);
        }
        if self
            .signing_intents
            .get(key)
            .is_some_and(|nonce| *nonce != submission.nonce)
            || (self.tracked_quotes.contains(key) && !self.signing_intents.contains_key(key))
            || self.items.keys().any(|other| {
                other != key
                    && other.sponsor == key.sponsor
                    && !self.refused(other)
                    && (self
                        .signing_intents
                        .get(other)
                        .is_some_and(|nonce| *nonce == submission.nonce)
                        || self.items[other]
                            .submission
                            .as_ref()
                            .is_some_and(|saved| saved.nonce == submission.nonce))
            })
        {
            return Err(JournalError::Conflict);
        }
        let item = self.items.get_mut(key).ok_or(JournalError::Conflict)?;
        if item.quote.is_none() || item.submission.is_some() || item.completion.is_some() {
            return Err(JournalError::Conflict);
        }
        item.submission = Some(submission.clone());
        Ok(())
    }

    fn apply_completed(
        &mut self,
        key: &Key,
        account: &Address,
        completion: &Completion,
    ) -> Result<(), JournalError> {
        let item = self.items.entry(*key).or_insert(Item {
            account: *account,
            quote: None,
            submission: None,
            completion: None,
        });
        if item.account != *account
            || item.completion.is_some()
            || matches!(
                self.liability_releases.get(key),
                Some(LiabilityRelease::Refused { .. })
            )
        {
            return Err(JournalError::Conflict);
        }
        match completion {
            Completion::Consumed => (),
            Completion::Included {
                hash,
                sid_collected,
                ..
            } => {
                if item.submission.as_ref().is_none_or(|s| s.hash != *hash)
                    || item
                        .quote
                        .as_ref()
                        .is_none_or(|q| q.amount != *sid_collected)
                {
                    return Err(JournalError::Conflict);
                }
            }
            Completion::Reverted { hash } => {
                if item.submission.as_ref().is_none_or(|s| s.hash != *hash) {
                    return Err(JournalError::Conflict);
                }
            }
            Completion::Cancelled { .. } => return Err(JournalError::Conflict),
        }
        item.completion = Some(*completion);
        Ok(())
    }

    fn apply_receipt_observed(
        &mut self,
        key: &Key,
        hash: &Word,
        receipt: &serde_json::Value,
        canonical: &serde_json::Value,
        finalized: &serde_json::Value,
    ) -> Result<(), JournalError> {
        let item = self.items.get(key).ok_or(JournalError::Conflict)?;
        if item.submission.as_ref().is_none_or(|s| s.hash != *hash) || self.refused(key) {
            return Err(JournalError::Conflict);
        }
        crate::station::validate_finalized_receipt(hash, receipt, canonical, finalized)
            .map_err(|_| JournalError::Corrupt)?;
        if self.receipts.contains_key(hash) {
            return Err(JournalError::Conflict);
        }
        self.receipts.insert(*hash, receipt.clone());
        Ok(())
    }

    fn apply_rate_published(&mut self, publication: &Publication) -> Result<(), JournalError> {
        if publication.hash == [0; 32]
            || publication.rate == [0; 32]
            || publication.gas_limit == 0
            || publication.max_fee_per_gas == 0
        {
            return Err(JournalError::Corrupt);
        }
        if self.publications.contains_key(&publication.hash) {
            return Err(JournalError::Conflict);
        }
        self.publications
            .insert(publication.hash, (*publication, None));
        Ok(())
    }

    fn apply_rate_settled(
        &mut self,
        hash: &Word,
        settlement: &Settlement,
    ) -> Result<(), JournalError> {
        if self.rate_transactions.contains_key(hash) {
            return Err(JournalError::Conflict);
        }
        let (publication, settled) = self
            .publications
            .get_mut(hash)
            .ok_or(JournalError::Conflict)?;
        if settled.is_some()
            || settlement.gas_used > publication.gas_limit
            || publication
                .max_fee_per_gas
                .checked_mul(u128::from(settlement.gas_used))
                .is_none_or(|most| settlement.cost_wei > most)
        {
            return Err(JournalError::Conflict);
        }
        *settled = Some(*settlement);
        Ok(())
    }

    fn apply_rate_prepared(
        &mut self,
        transaction: &PublicationTransaction,
    ) -> Result<(), JournalError> {
        crate::rate::validate_publication_transaction(transaction)?;
        let p = transaction.publication;
        if transaction.cancellation
            || self.rate_active.contains_key(&(p.owner, p.nonce))
            || self
                .publications
                .values()
                .any(|(saved, _)| saved.owner == p.owner && saved.nonce == p.nonce)
            || self.publications.contains_key(&p.hash)
        {
            return Err(JournalError::Conflict);
        }
        self.publications.insert(p.hash, (p, None));
        self.rate_transactions.insert(p.hash, transaction.clone());
        self.rate_active.insert((p.owner, p.nonce), p.hash);
        Ok(())
    }

    fn apply_rate_broadcast(&mut self, hash: &Word) -> Result<(), JournalError> {
        let tx = self
            .rate_transactions
            .get(hash)
            .ok_or(JournalError::Conflict)?;
        if self
            .rate_active
            .get(&(tx.publication.owner, tx.publication.nonce))
            != Some(hash)
            || self.publications[hash].1.is_some()
            || !self.rate_broadcast.insert(*hash)
        {
            return Err(JournalError::Conflict);
        }
        Ok(())
    }

    fn apply_rate_replaced(
        &mut self,
        previous: &Word,
        transaction: &PublicationTransaction,
    ) -> Result<(), JournalError> {
        crate::rate::validate_publication_transaction(transaction)?;
        let old = self
            .rate_transactions
            .get(previous)
            .ok_or(JournalError::Conflict)?;
        let p = transaction.publication;
        let q = old.publication;
        let bump = |fee: u128| fee.checked_mul(110).map(|n| n.div_ceil(100));
        if p.owner != q.owner
            || p.nonce != q.nonce
            || p.rate != q.rate
            || p.signed_at != q.signed_at
            || transaction.chain_id != old.chain_id
            || transaction.paymaster != old.paymaster
            || self.publications[previous].1.is_some()
            || self.rate_active.get(&(q.owner, q.nonce)) != Some(previous)
            || self.publications.contains_key(&p.hash)
            || bump(q.max_fee_per_gas).is_none_or(|fee| p.max_fee_per_gas < fee)
            || bump(old.max_priority_fee_per_gas)
                .is_none_or(|fee| transaction.max_priority_fee_per_gas < fee)
        {
            return Err(JournalError::Conflict);
        }
        self.publications.insert(p.hash, (p, None));
        self.rate_transactions.insert(p.hash, transaction.clone());
        self.rate_active.insert((p.owner, p.nonce), p.hash);
        Ok(())
    }

    fn apply_rate_finalized(
        &mut self,
        hash: &Word,
        settlement: &Settlement,
        receipt: &serde_json::Value,
        canonical: &serde_json::Value,
        finalized: &serde_json::Value,
    ) -> Result<(), JournalError> {
        let tx = self
            .rate_transactions
            .get(hash)
            .ok_or(JournalError::Conflict)?;
        crate::station::validate_finalized_receipt(hash, receipt, canonical, finalized)
            .map_err(|_| JournalError::Corrupt)?;
        let observed =
            crate::rate::publication_settlement(tx, receipt).map_err(|_| JournalError::Corrupt)?;
        if *settlement != observed
            || self.publications.values().any(|(p, s)| {
                p.owner == tx.publication.owner && p.nonce == tx.publication.nonce && s.is_some()
            })
        {
            return Err(JournalError::Conflict);
        }
        self.publications
            .get_mut(hash)
            .ok_or(JournalError::Conflict)?
            .1 = Some(*settlement);
        self.rate_active
            .remove(&(tx.publication.owner, tx.publication.nonce));
        Ok(())
    }
}
fn validate_consumed_proof(
    key: &Key,
    quote: &QuoteRecord,
    canonical: &serde_json::Value,
    finalized: &serde_json::Value,
    code: &str,
    params: &serde_json::Value,
    result: &serde_json::Value,
) -> Result<(), JournalError> {
    use crate::rpc::{bytes, hex, quantity};
    let number = |value: &serde_json::Value, field: &str| {
        quantity(value[field].as_str().ok_or(JournalError::Corrupt)?)
            .map_err(|_| JournalError::Corrupt)
    };
    let hash = bytes(finalized["hash"].as_str().ok_or(JournalError::Corrupt)?)
        .map_err(|_| JournalError::Corrupt)?;
    if hash.len() != 32
        || hash.iter().all(|byte| *byte == 0)
        || canonical["hash"] != finalized["hash"]
        || number(canonical, "number")? != number(finalized, "number")?
        || number(canonical, "timestamp")? != number(finalized, "timestamp")?
    {
        return Err(JournalError::Corrupt);
    }
    let data = [
        keccak(b"usedQuoteNonces(address,uint256)")[..4].to_vec(),
        crate::quote::address_word(key.sponsor).to_vec(),
        key.quote_nonce.to_vec(),
    ]
    .concat();
    let params = params.as_array().ok_or(JournalError::Corrupt)?;
    if !(2..=3).contains(&params.len())
        || params[0] != serde_json::json!({"to":hex(&quote.account),"data":hex(&data)})
        || params[1] != serde_json::json!({"blockHash":finalized["hash"],"requireCanonical":true})
        || bytes(result.as_str().ok_or(JournalError::Corrupt)?)
            .map_err(|_| JournalError::Corrupt)?
            != word(1)
    {
        return Err(JournalError::Corrupt);
    }
    if code == "0x" {
        if params.len() != 3 {
            return Err(JournalError::Corrupt);
        }
        let override_ = params[2].as_object().ok_or(JournalError::Corrupt)?;
        let account = override_
            .get(&hex(&quote.account))
            .and_then(serde_json::Value::as_object)
            .ok_or(JournalError::Corrupt)?;
        let runtime = account
            .get("code")
            .and_then(serde_json::Value::as_str)
            .ok_or(JournalError::Corrupt)?;
        let raw = bytes(runtime).map_err(|_| JournalError::Corrupt)?;
        if override_.len() != 1
            || account.len() != 1
            || raw.is_empty()
            || raw.len() > 24_576
            || raw.starts_with(&[0xef, 0x01, 0x00])
        {
            return Err(JournalError::Corrupt);
        }
    } else if params.len() != 2
        || code.to_ascii_lowercase() != format!("0xef0100{}", &hex(&quote.paymaster)[2..])
    {
        return Err(JournalError::Corrupt);
    }
    Ok(())
}

/// The record kinds of the retired same-nonce replacement path. A journal
/// holding one was written by an earlier version and is refused on replay.
const RETIRED_KINDS: [&str; 4] = [
    "replacement_signing_intent",
    "replaced",
    "cancelled",
    "released",
];

fn unreadable(line: &[u8]) -> JournalError {
    match serde_json::from_slice::<serde_json::Value>(line) {
        Ok(value)
            if value["kind"]
                .as_str()
                .is_some_and(|kind| RETIRED_KINDS.contains(&kind)) =>
        {
            JournalError::Version
        }
        _ => JournalError::Corrupt,
    }
}

pub struct Journal {
    file: File,
    state: State,
    poisoned: bool,
}
impl Journal {
    /// # Errors
    /// Refuses concurrent writers, malformed lines, torn tails and contradictory records.
    pub fn open(path: &Path) -> Result<Self, JournalError> {
        let mut options = OpenOptions::new();
        options.read(true).append(true).create(true).mode(0o600);
        #[cfg(target_os = "linux")]
        options.custom_flags(0x20000);
        let file = options.open(path).map_err(|_| JournalError::Io)?;
        let metadata = file.metadata().map_err(|_| JournalError::Io)?;
        let owner = std::fs::metadata("/proc/self")
            .map_err(|_| JournalError::Io)?
            .uid();
        if !metadata.is_file()
            || metadata.mode() & 0o077 != 0
            || metadata.nlink() != 1
            || metadata.uid() != owner
        {
            return Err(JournalError::Corrupt);
        }
        file.try_lock().map_err(|_| JournalError::Locked)?;
        let mut reader = BufReader::new(&file);
        let mut state = State::default();
        loop {
            let mut line = Vec::new();
            let count = reader
                .read_until(b'\n', &mut line)
                .map_err(|_| JournalError::Io)?;
            if count == 0 {
                break;
            }
            if line.last() != Some(&b'\n') || count > 4_194_304 {
                return Err(JournalError::Corrupt);
            }
            let entry = serde_json::from_slice(&line).map_err(|_| unreadable(&line))?;
            state.apply(&entry)?;
        }
        file.sync_all().map_err(|_| JournalError::Io)?;
        let parent = path
            .parent()
            .filter(|p| !p.as_os_str().is_empty())
            .unwrap_or_else(|| Path::new("."));
        File::open(parent)
            .and_then(|f| f.sync_all())
            .map_err(|_| JournalError::Io)?;
        Ok(Self {
            file,
            state,
            poisoned: false,
        })
    }
    #[must_use]
    pub const fn state(&self) -> &State {
        &self.state
    }
    /// # Errors
    /// Refuses conflicting entries and permanently stops this writer after uncertain disk writes.
    pub fn append(&mut self, entry: &Entry) -> Result<(), JournalError> {
        if self.poisoned {
            return Err(JournalError::Io);
        }
        if let Entry::RatePrepared { transaction } = entry {
            if self
                .state
                .rate_transactions
                .get(&transaction.publication.hash)
                == Some(transaction)
            {
                return Ok(());
            }
        }
        let mut next = self.state.clone();
        next.apply(entry)?;
        let mut line = serde_json::to_vec(entry).map_err(|_| JournalError::Corrupt)?;
        line.push(b'\n');
        if line.len() > 4_194_304 {
            return Err(JournalError::Corrupt);
        }
        self.poisoned = true;
        self.file
            .write_all(&line)
            .and_then(|()| self.file.flush())
            .and_then(|()| self.file.sync_all())
            .map_err(|_| JournalError::Io)?;
        self.state = next;
        self.poisoned = false;
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::signer::QuoteSigner;
    #[test]
    fn append_replay_lock_conflict_and_corruption() -> Result<(), Box<dyn std::error::Error>> {
        let path =
            std::env::temp_dir().join(format!("paxeer-journal-{}.jsonl", std::process::id()));
        if path.exists() {
            std::fs::remove_file(&path)?;
        }
        let mut journal = Journal::open(&path)?;
        assert!(matches!(Journal::open(&path), Err(JournalError::Locked)));
        let entry = Entry::Completed {
            key: Key {
                sponsor: [1; 20],
                quote_nonce: word(2),
            },
            account: [3; 20],
            completion: Completion::Consumed,
        };
        journal.append(&entry)?;
        assert_eq!(journal.append(&entry), Err(JournalError::Conflict));
        let state = journal.state().clone();
        drop(journal);
        let journal = Journal::open(&path)?;
        assert_eq!(journal.state(), &state);
        drop(journal);
        let mut file = OpenOptions::new().append(true).open(&path)?;
        file.write_all(b"{")?;
        drop(file);
        assert!(matches!(Journal::open(&path), Err(JournalError::Corrupt)));
        std::fs::write(&path, b"bad\n")?;
        assert!(matches!(Journal::open(&path), Err(JournalError::Corrupt)));
        std::fs::remove_file(path)?;
        Ok(())
    }
    fn quoted(
        signer: &impl QuoteSigner,
        quote_nonce: u128,
    ) -> Result<Entry, Box<dyn std::error::Error>> {
        let fees = Fees {
            gas_limit: 200_000,
            max_fee_per_gas: 5_000_000_000_000,
            max_priority_fee_per_gas: 1_000_000_000,
        };
        let mut record = QuoteRecord {
            did: crate::rpc::hex(&[0x11; 20]),
            batch_nonce: word(0),
            key: Key {
                sponsor: signer.address(),
                quote_nonce: word(quote_nonce),
            },
            chain_id: 1325,
            paymaster: [0x44; 20],
            account: [0x11; 20],
            token: [0x22; 20],
            maximum: word(3_200_000),
            amount: word(3_145_140),
            deadline: 1019,
            gas_cost: word(fees.gas_cost()?),
            issued_at: 1000,
            signature: vec![],
            fees,
        };
        let quote = Quote {
            sponsor: record.key.sponsor,
            token: record.token,
            max_token_amount: record.maximum,
            token_amount: record.amount,
            deadline: word(1019),
            nonce: record.key.quote_nonce,
            gas_cost: record.gas_cost,
        };
        record.signature = signer
            .sign_digest(quote_digest(word(1325), record.account, &quote))?
            .to_vec();
        Ok(Entry::Quoted {
            quote: Box::new(record),
        })
    }
    fn prepared(key: Key, nonce: u64, marker: u8) -> Entry {
        let raw = vec![4, marker];
        Entry::Prepared {
            key,
            submission: Submission {
                did: crate::rpc::hex(&[0x11; 20]),
                batch_nonce: word(0),
                nonce,
                hash: keccak(&raw),
                raw,
            },
        }
    }
    #[test]
    fn refusal_frees_the_nonce_and_retired_replacement_records_refuse_replay(
    ) -> Result<(), Box<dyn std::error::Error>> {
        let signer = crate::signer::tests::signer()?;
        let path = std::env::temp_dir().join(format!(
            "paxeer-journal-lifecycle-{}.jsonl",
            std::process::id()
        ));
        if path.exists() {
            std::fs::remove_file(&path)?;
        }
        let key = |n: u128| Key {
            sponsor: signer.address(),
            quote_nonce: word(n),
        };
        let mut journal = Journal::open(&path)?;
        for n in [1, 2] {
            journal.append(&quoted(&signer, n)?)?;
        }
        journal.append(&prepared(key(1), 5, 1))?;
        let refused_hash = keccak(&[4, 1]);
        assert_eq!(
            journal.append(&prepared(key(2), 5, 2)),
            Err(JournalError::Conflict)
        );
        assert!(journal.state().holds(signer.address(), 5));
        assert_eq!(journal.state().next_nonce(signer.address(), 5), Some(6));
        for conflicting in [
            Entry::Refused {
                key: key(1),
                hash: word(9),
            },
            Entry::Refused {
                key: key(2),
                hash: refused_hash,
            },
        ] {
            assert_eq!(journal.append(&conflicting), Err(JournalError::Conflict));
        }
        let refusal = Entry::Refused {
            key: key(1),
            hash: refused_hash,
        };
        journal.append(&refusal)?;
        assert!(journal.state().refused(&key(1)));
        assert!(!journal.state().holds(signer.address(), 5));
        assert_eq!(journal.state().next_nonce(signer.address(), 5), Some(5));
        assert_eq!(journal.state().liability(key(1))?, 0);
        assert_eq!(journal.append(&refusal), Err(JournalError::Conflict));
        assert_eq!(
            journal.append(&Entry::Completed {
                key: key(1),
                account: [0x11; 20],
                completion: Completion::Reverted { hash: refused_hash },
            }),
            Err(JournalError::Conflict)
        );
        journal.append(&prepared(key(2), 5, 2))?;
        assert!(journal.state().holds(signer.address(), 5));
        let state = journal.state().clone();
        drop(journal);
        let journal = Journal::open(&path)?;
        assert_eq!(journal.state(), &state);
        assert!(journal.state().refused(&key(1)));
        assert_eq!(
            journal.state().items[&key(2)]
                .submission
                .as_ref()
                .map(|s| s.nonce),
            Some(5)
        );
        drop(journal);
        let durable = std::fs::read(&path)?;
        for retired in [
            serde_json::json!({"kind": "released", "key": key(2), "nonce": 5}),
            serde_json::json!({"kind": "replacement_signing_intent", "key": key(2), "nonce": 5}),
            serde_json::json!({"kind": "replaced", "key": key(2)}),
            serde_json::json!({"kind": "cancelled", "key": key(2), "block_number": 17}),
        ] {
            let mut legacy = durable.clone();
            serde_json::to_writer(&mut legacy, &retired)?;
            legacy.push(b'\n');
            std::fs::write(&path, &legacy)?;
            assert!(matches!(Journal::open(&path), Err(JournalError::Version)));
        }
        let mut unknown = durable;
        unknown.extend_from_slice(b"{\"kind\":\"withdrawn\"}\n");
        std::fs::write(&path, &unknown)?;
        assert!(matches!(Journal::open(&path), Err(JournalError::Corrupt)));
        std::fs::remove_file(path)?;
        Ok(())
    }
}
