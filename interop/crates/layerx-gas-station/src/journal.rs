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
}
impl std::fmt::Display for JournalError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "journal refused: {self:?}")
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
        if recover(digest, &signature).map_err(|_| JournalError::Corrupt)? != self.key.sponsor
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
    pub nonce: u64,
    pub hash: Word,
    pub raw: Vec<u8>,
}
#[derive(Clone, Debug, Deserialize, Serialize, Eq, PartialEq)]
#[serde(deny_unknown_fields)]
pub struct Replacement {
    pub nonce: u64,
    pub fees: Fees,
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
    ReplacementSigningIntent {
        key: Key,
        nonce: u64,
        fees: Fees,
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
    Released {
        key: Key,
        nonce: u64,
    },
    Replaced {
        key: Key,
        replacement: Replacement,
    },
    Cancelled {
        key: Key,
        hash: Word,
        block_number: u64,
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
    pub replacement: Option<Replacement>,
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
    pub replacement_intents: BTreeMap<Key, Fees>,
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
    /// Whether a live submission or replacement of `sponsor` holds `nonce`.
    #[must_use]
    pub fn holds(&self, sponsor: Address, nonce: u64) -> bool {
        self.items.iter().any(|(key, item)| {
            key.sponsor == sponsor
                && (self
                    .signing_intents
                    .get(key)
                    .is_some_and(|saved| *saved == nonce)
                    || item.submission.as_ref().is_some_and(|s| s.nonce == nonce)
                    || item.replacement.as_ref().is_some_and(|r| r.nonce == nonce))
        })
    }
    pub fn liability(&self, key: Key) -> Result<u128, JournalError> {
        if self.liability_releases.contains_key(&key) {
            return Ok(0);
        }
        let item = self.items.get(&key).ok_or(JournalError::Conflict)?;
        let Some(quote) = &item.quote else {
            return Ok(0);
        };
        let original = quote.fees.gas_cost().map_err(|_| JournalError::Corrupt)?;
        let replacement = item
            .replacement
            .as_ref()
            .map(|value| value.fees.gas_cost())
            .transpose()
            .map_err(|_| JournalError::Corrupt)?
            .unwrap_or(0);
        let intended = self
            .replacement_intents
            .get(&key)
            .map(|fees| fees.gas_cost())
            .transpose()
            .map_err(|_| JournalError::Corrupt)?
            .unwrap_or(0);
        Ok(original.max(replacement).max(intended))
    }

    pub fn active_pax(&self) -> Result<u128, JournalError> {
        self.items.keys().try_fold(0u128, |sum, key| {
            sum.checked_add(self.liability(*key)?)
                .ok_or(JournalError::Corrupt)
        })
    }

    pub fn finalized_spend(&self) -> Result<u128, JournalError> {
        self.liability_releases
            .iter()
            .try_fold(0u128, |sum, (key, proof)| {
                let spent = match proof {
                    LiabilityRelease::Expired { .. } | LiabilityRelease::Consumed { .. } => 0,
                    LiabilityRelease::Settled { hash } => self.settlement_cost(*key, *hash)?,
                };
                sum.checked_add(spent).ok_or(JournalError::Corrupt)
            })
    }

    fn settlement_cost(&self, key: Key, hash: Word) -> Result<u128, JournalError> {
        let item = self.items.get(&key).ok_or(JournalError::Conflict)?;
        let quote = item.quote.as_ref().ok_or(JournalError::Conflict)?;
        let receipt = self.receipts.get(&hash).ok_or(JournalError::Conflict)?;
        let (fees, status, recipient) = match item.completion {
            Some(Completion::Included { hash: saved, .. }) if saved == hash => {
                (quote.fees, 1, quote.account)
            }
            Some(Completion::Reverted { hash: saved }) if saved == hash => {
                (quote.fees, 0, quote.account)
            }
            Some(Completion::Cancelled { hash: saved, .. }) if saved == hash => (
                item.replacement
                    .as_ref()
                    .ok_or(JournalError::Conflict)?
                    .fees,
                1,
                key.sponsor,
            ),
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
            || receipt["to"] != crate::rpc::hex(&recipient)
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
            Entry::QuoteReserved { quote } => {
                self.apply(&Entry::Quoted {
                    quote: quote.clone(),
                })?;
                self.tracked_quotes.insert(quote.key);
            }
            Entry::SigningIntent { key, nonce, digest } => {
                let item = self.items.get(key).ok_or(JournalError::Conflict)?;
                if *digest == [0; 32]
                    || !self.tracked_quotes.contains(key)
                    || self.signing_intents.contains_key(key)
                    || self.liability_releases.contains_key(key)
                    || item.completion.is_some()
                    || item.submission.is_some()
                    || self.holds(key.sponsor, *nonce)
                {
                    return Err(JournalError::Conflict);
                }
                self.signing_intents.insert(*key, *nonce);
            }
            Entry::ReplacementSigningIntent {
                key,
                nonce,
                fees,
                digest,
            } => {
                let item = self.items.get(key).ok_or(JournalError::Conflict)?;
                if *digest == [0; 32]
                    || self.replacement_intents.contains_key(key)
                    || self.liability_releases.contains_key(key)
                    || item.completion.is_some()
                    || item.replacement.is_some()
                    || item
                        .submission
                        .as_ref()
                        .is_none_or(|value| value.nonce != *nonce)
                    || !fees
                        .replaces(item.quote.as_ref().ok_or(JournalError::Conflict)?.fees)
                        .map_err(|_| JournalError::Corrupt)?
                {
                    return Err(JournalError::Conflict);
                }
                self.replacement_intents.insert(*key, *fees);
            }
            Entry::QuoteAdmitted { quote, admission } => {
                if admission.identity
                    != Admission::identity(quote.account, admission.batch_nonce, admission.interval)
                    || admission.request_digest == [0; 32]
                    || self.admissions.contains_key(&admission.identity)
                {
                    return Err(JournalError::Conflict);
                }
                self.apply(&Entry::Quoted {
                    quote: quote.clone(),
                })?;
                self.admissions
                    .insert(admission.identity, (*admission, quote.key));
                self.tracked_quotes.insert(quote.key);
            }
            Entry::LiabilityReleased { key, proof } => {
                if self.liability_releases.contains_key(key) {
                    return Err(JournalError::Conflict);
                }
                let item = self.items.get(key).ok_or(JournalError::Conflict)?;
                let quote = item.quote.as_ref().ok_or(JournalError::Conflict)?;
                match proof {
                    LiabilityRelease::Settled { hash } => {
                        self.settlement_cost(*key, *hash)?;
                    }
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
                            || self.replacement_intents.contains_key(key)
                            || item.submission.is_some()
                            || item.replacement.is_some()
                            || item.completion != Some(Completion::Consumed)
                            || *chain_id != quote.chain_id
                            || *account != quote.account
                            || *paymaster != quote.paymaster
                            || *quote_nonce != key.quote_nonce
                        {
                            return Err(JournalError::Conflict);
                        }
                        validate_consumed_proof(
                            key, quote, canonical, finalized, code, params, result,
                        )?;
                    }
                    LiabilityRelease::Expired {
                        canonical,
                        finalized,
                    } => {
                        let number = |value: &serde_json::Value, field: &str| {
                            crate::rpc::quantity(
                                value[field].as_str().ok_or(JournalError::Corrupt)?,
                            )
                            .map_err(|_| JournalError::Corrupt)
                        };
                        let hash = crate::rpc::bytes(
                            finalized["hash"].as_str().ok_or(JournalError::Corrupt)?,
                        )
                        .map_err(|_| JournalError::Corrupt)?;
                        if !self.tracked_quotes.contains(key)
                            || self.signing_intents.contains_key(key)
                            || self.replacement_intents.contains_key(key)
                            || item.submission.is_some()
                            || item.replacement.is_some()
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
            }
            Entry::Quoted { quote } => {
                quote.signed_quote()?;
                if self.items.contains_key(&quote.key) {
                    return Err(JournalError::Conflict);
                }
                self.items.insert(
                    quote.key,
                    Item {
                        account: quote.account,
                        quote: Some(*quote.clone()),
                        submission: None,
                        replacement: None,
                        completion: None,
                    },
                );
            }
            Entry::Prepared { key, submission } => {
                if self.liability_releases.contains_key(key) {
                    return Err(JournalError::Conflict);
                }
                if submission.raw.first() != Some(&4) || keccak(&submission.raw) != submission.hash
                {
                    return Err(JournalError::Corrupt);
                }
                if self
                    .signing_intents
                    .get(key)
                    .is_some_and(|nonce| *nonce != submission.nonce)
                    || (self.tracked_quotes.contains(key)
                        && !self.signing_intents.contains_key(key))
                    || self.items.keys().any(|other| {
                        other != key
                            && other.sponsor == key.sponsor
                            && (self
                                .signing_intents
                                .get(other)
                                .is_some_and(|nonce| *nonce == submission.nonce)
                                || self.items[other]
                                    .submission
                                    .as_ref()
                                    .is_some_and(|saved| saved.nonce == submission.nonce)
                                || self.items[other]
                                    .replacement
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
            }
            Entry::Completed {
                key,
                account,
                completion,
            } => {
                let item = self.items.entry(*key).or_insert(Item {
                    account: *account,
                    quote: None,
                    submission: None,
                    replacement: None,
                    completion: None,
                });
                if item.account != *account || item.completion.is_some() {
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
            }
            Entry::Released { .. } => return Err(JournalError::Conflict),
            Entry::Replaced { key, replacement } => {
                if self.liability_releases.contains_key(key) {
                    return Err(JournalError::Conflict);
                }
                if replacement.raw.first() != Some(&2)
                    || keccak(&replacement.raw) != replacement.hash
                {
                    return Err(JournalError::Corrupt);
                }
                let item = self.items.get_mut(key).ok_or(JournalError::Conflict)?;
                if item.completion.is_some()
                    || item.replacement.is_some()
                    || self
                        .replacement_intents
                        .get(key)
                        .is_some_and(|fees| *fees != replacement.fees)
                    || (self.tracked_quotes.contains(key)
                        && !self.replacement_intents.contains_key(key))
                    || item
                        .submission
                        .as_ref()
                        .is_none_or(|s| s.nonce != replacement.nonce)
                {
                    return Err(JournalError::Conflict);
                }
                let original = item.quote.as_ref().ok_or(JournalError::Conflict)?.fees;
                if !replacement
                    .fees
                    .replaces(original)
                    .map_err(|_| JournalError::Corrupt)?
                {
                    return Err(JournalError::Corrupt);
                }
                item.replacement = Some(replacement.clone());
            }
            Entry::Cancelled {
                key,
                hash,
                block_number,
            } => {
                let item = self.items.get_mut(key).ok_or(JournalError::Conflict)?;
                if item.completion.is_some()
                    || item.replacement.as_ref().is_none_or(|r| r.hash != *hash)
                {
                    return Err(JournalError::Conflict);
                }
                item.completion = Some(Completion::Cancelled {
                    hash: *hash,
                    block_number: *block_number,
                });
            }
            Entry::ReceiptObserved {
                key,
                hash,
                receipt,
                canonical,
                finalized,
            } => {
                let item = self.items.get(key).ok_or(JournalError::Conflict)?;
                if !item.submission.as_ref().is_some_and(|s| s.hash == *hash)
                    && !item.replacement.as_ref().is_some_and(|r| r.hash == *hash)
                {
                    return Err(JournalError::Conflict);
                }
                crate::station::validate_finalized_receipt(hash, receipt, canonical, finalized)
                    .map_err(|_| JournalError::Corrupt)?;
                if self.receipts.contains_key(hash) {
                    return Err(JournalError::Conflict);
                }
                self.receipts.insert(*hash, receipt.clone());
            }
            Entry::RatePublished { publication } => {
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
            }
            Entry::RateSettled { hash, settlement } => {
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
            }
            Entry::RatePrepared { transaction } => {
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
            }
            Entry::RateBroadcast { hash } => {
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
            }
            Entry::RateReplaced {
                previous,
                transaction,
            } => {
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
            }
            Entry::RateFinalized {
                hash,
                settlement,
                receipt,
                canonical,
                finalized,
            } => {
                let tx = self
                    .rate_transactions
                    .get(hash)
                    .ok_or(JournalError::Conflict)?;
                crate::station::validate_finalized_receipt(hash, receipt, canonical, finalized)
                    .map_err(|_| JournalError::Corrupt)?;
                let observed = crate::rate::publication_settlement(tx, receipt)
                    .map_err(|_| JournalError::Corrupt)?;
                if *settlement != observed
                    || self.publications.values().any(|(p, s)| {
                        p.owner == tx.publication.owner
                            && p.nonce == tx.publication.nonce
                            && s.is_some()
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
            }
        }
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
            let entry = serde_json::from_slice(&line).map_err(|_| JournalError::Corrupt)?;
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
                nonce,
                hash: keccak(&raw),
                raw,
            },
        }
    }
    #[test]
    fn release_replacement_and_cancellation_replay_and_refuse_conflicts(
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
        for n in [1, 2, 3] {
            journal.append(&quoted(&signer, n)?)?;
        }
        journal.append(&prepared(key(1), 5, 1))?;
        assert_eq!(
            journal.append(&prepared(key(2), 5, 2)),
            Err(JournalError::Conflict)
        );
        for refused in [
            Entry::Released {
                key: key(1),
                nonce: 6,
            },
            Entry::Released {
                key: key(2),
                nonce: 5,
            },
        ] {
            assert_eq!(journal.append(&refused), Err(JournalError::Conflict));
        }
        let release = Entry::Released {
            key: key(1),
            nonce: 5,
        };
        let retained = journal.state().items[&key(1)].submission.clone();
        let durable = std::fs::read(&path)?;
        assert_eq!(journal.append(&release), Err(JournalError::Conflict));
        assert!(journal.state().holds(signer.address(), 5));
        assert_eq!(journal.state().items[&key(1)].submission, retained);
        assert!(journal.state().items[&key(1)].completion.is_none());
        assert_eq!(std::fs::read(&path)?, durable);
        assert_eq!(journal.append(&release), Err(JournalError::Conflict));
        assert_eq!(
            journal.append(&prepared(key(2), 5, 2)),
            Err(JournalError::Conflict)
        );
        journal.append(&prepared(key(2), 6, 2))?;
        let Entry::Quoted { quote } = quoted(&signer, 2)? else {
            return Err("expected a quote".into());
        };
        let fees = quote.fees.replacement()?;
        let cancellation = crate::tx::sign_cancellation(1325, 6, fees, &signer)?;
        let replaced = |nonce: u64, fees: Fees| Entry::Replaced {
            key: key(2),
            replacement: Replacement {
                nonce,
                fees,
                hash: cancellation.hash,
                raw: cancellation.raw.clone(),
            },
        };
        let mut underpriced = fees;
        underpriced.max_fee_per_gas -= 1;
        assert_eq!(
            journal.append(&replaced(6, underpriced)),
            Err(JournalError::Corrupt)
        );
        assert_eq!(
            journal.append(&replaced(7, fees)),
            Err(JournalError::Conflict)
        );
        journal.append(&replaced(6, fees))?;
        assert_eq!(
            journal.append(&replaced(6, fees)),
            Err(JournalError::Conflict)
        );
        assert_eq!(
            journal.append(&Entry::Released {
                key: key(2),
                nonce: 6
            }),
            Err(JournalError::Conflict)
        );
        assert_eq!(
            journal.append(&prepared(key(3), 6, 3)),
            Err(JournalError::Conflict)
        );
        let cancelled = Completion::Cancelled {
            hash: cancellation.hash,
            block_number: 17,
        };
        assert_eq!(
            journal.append(&Entry::Completed {
                key: key(2),
                account: [0x11; 20],
                completion: cancelled,
            }),
            Err(JournalError::Conflict)
        );
        assert_eq!(
            journal.append(&Entry::Cancelled {
                key: key(2),
                hash: word(9),
                block_number: 17,
            }),
            Err(JournalError::Conflict)
        );
        journal.append(&Entry::Cancelled {
            key: key(2),
            hash: cancellation.hash,
            block_number: 17,
        })?;
        let state = journal.state().clone();
        drop(journal);
        let journal = Journal::open(&path)?;
        assert_eq!(journal.state(), &state);
        assert_eq!(journal.state().items[&key(1)].submission, retained);
        assert!(journal.state().holds(signer.address(), 5));
        assert!(journal.state().items[&key(1)].completion.is_none());
        assert_eq!(journal.state().items[&key(2)].completion, Some(cancelled));
        drop(journal);
        let mut legacy = OpenOptions::new().append(true).open(&path)?;
        serde_json::to_writer(&mut legacy, &release)?;
        legacy.write_all(b"\n")?;
        legacy.sync_all()?;
        drop(legacy);
        assert!(matches!(Journal::open(&path), Err(JournalError::Conflict)));
        std::fs::remove_file(path)?;
        Ok(())
    }
}
