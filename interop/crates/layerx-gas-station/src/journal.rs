use std::collections::{BTreeMap, BTreeSet};
use std::fs::{File, OpenOptions};
use std::io::{BufRead as _, BufReader, Write as _};
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
#[serde(tag = "kind", rename_all = "snake_case", deny_unknown_fields)]
pub enum Entry {
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
    RatePublished {
        publication: Publication,
    },
    RateSettled {
        hash: Word,
        settlement: Settlement,
    },
    RatePrepared {
        prepared: PreparedPublication,
    },
    RateBroadcast {
        hash: Word,
    },
    RateReplaced {
        previous: Word,
        prepared: PreparedPublication,
    },
    RateCancelled {
        previous: Word,
        cancellation: Replacement,
    },
}
/// The record version of a prepared publication that retains its signed bytes.
pub const PUBLICATION_VERSION: u32 = 2;
/// One setRate transaction journalled before it is broadcast, with the exact
/// canonical signed bytes, so that a restarted publisher rebroadcasts the same
/// transaction instead of signing another. It never holds key material.
#[derive(Clone, Debug, Deserialize, Serialize, Eq, PartialEq)]
#[serde(deny_unknown_fields)]
pub struct PreparedPublication {
    pub version: u32,
    pub chain_id: u64,
    pub paymaster: Address,
    pub publication: Publication,
    #[serde(with = "crate::tx::fee_amount")]
    pub max_priority_fee_per_gas: u128,
    pub raw: Vec<u8>,
}
impl PreparedPublication {
    #[must_use]
    pub const fn fees(&self) -> Fees {
        Fees {
            gas_limit: self.publication.gas_limit,
            max_fee_per_gas: self.publication.max_fee_per_gas,
            max_priority_fee_per_gas: self.max_priority_fee_per_gas,
        }
    }
    fn check(&self) -> Result<(), JournalError> {
        let p = &self.publication;
        if self.version != PUBLICATION_VERSION
            || self.chain_id == 0
            || self.paymaster == [0; 20]
            || p.owner == [0; 20]
            || p.hash == [0; 32]
            || p.rate == [0; 32]
            || p.gas_limit == 0
            || p.max_fee_per_gas == 0
            || self.max_priority_fee_per_gas > p.max_fee_per_gas
            || self.raw.first() != Some(&2)
            || keccak(&self.raw) != p.hash
        {
            return Err(JournalError::Corrupt);
        }
        self.fees().gas_cost().map_err(|_| JournalError::Corrupt)?;
        Ok(())
    }
}
/// Every transaction signed at one owner nonce: the prepared publication and
/// its fee replacements in order, at most one cancellation, the hashes known
/// to have been broadcast and the one canonical finalized outcome. The nonce
/// reserves only the most any one of them can spend.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Family {
    pub members: Vec<PreparedPublication>,
    pub cancellation: Option<Replacement>,
    pub broadcast: BTreeSet<Word>,
    pub finalized: Option<(Word, Settlement)>,
}
impl Family {
    /// The newest signed publication of this nonce.
    /// # Panics
    /// Never: a family is created with its first member.
    #[must_use]
    pub fn latest(&self) -> &PreparedPublication {
        &self.members[self.members.len() - 1]
    }
    /// Every hash signed at this nonce, the cancellation last.
    #[must_use]
    pub fn hashes(&self) -> Vec<Word> {
        self.members
            .iter()
            .map(|m| m.publication.hash)
            .chain(self.cancellation.as_ref().map(|c| c.hash))
            .collect()
    }
    fn gas_limit_of(&self, hash: &Word) -> Option<(u64, u128)> {
        self.members
            .iter()
            .find(|m| m.publication.hash == *hash)
            .map(|m| (m.publication.gas_limit, m.publication.max_fee_per_gas))
            .or_else(|| {
                self.cancellation
                    .as_ref()
                    .filter(|c| c.hash == *hash)
                    .map(|c| (c.fees.gas_limit, c.fees.max_fee_per_gas))
            })
    }
    fn reserved_wei(&self) -> u128 {
        self.members
            .iter()
            .map(|m| {
                m.publication
                    .max_fee_per_gas
                    .saturating_mul(u128::from(m.publication.gas_limit))
            })
            .chain(self.cancellation.as_ref().map(|c| {
                c.fees
                    .max_fee_per_gas
                    .saturating_mul(u128::from(c.fees.gas_limit))
            }))
            .max()
            .unwrap_or(0)
    }
    fn signed_at(&self) -> u64 {
        self.members[0].publication.signed_at
    }
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
    pub publications: BTreeMap<Word, (Publication, Option<Settlement>)>,
    pub families: BTreeMap<(Address, u64), Family>,
}
impl State {
    /// The oldest prepared publication nonce without a finalized outcome; the
    /// publisher recovers it before it constructs another publication.
    #[must_use]
    pub fn unresolved(&self) -> Option<&Family> {
        self.families.values().find(|f| f.finalized.is_none())
    }
    /// Unsettled publications of the first record version, which retained no
    /// signed bytes: they can only be resolved by their receipt and block any
    /// new publication until then.
    #[must_use]
    pub fn legacy_unresolved(&self) -> Vec<Publication> {
        self.publications
            .values()
            .filter(|(p, s)| {
                s.is_none()
                    && !self
                        .publications
                        .values()
                        .any(|(q, t)| t.is_some() && q.owner == p.owner && q.nonce >= p.nonce)
            })
            .map(|(p, _)| *p)
            .collect()
    }
    fn family_of(&self, hash: &Word) -> Option<(Address, u64)> {
        self.families
            .iter()
            .find(|(_, f)| f.hashes().contains(hash))
            .map(|(k, _)| *k)
    }
    /// The gas the rate publisher spent or reserved on the chain day `day`
    /// (chain time divided by 86400): the gas used of each settled
    /// publication and the gas limit of each unsettled one.
    #[must_use]
    pub fn publication_gas(&self, day: u64) -> u128 {
        self.publications
            .values()
            .filter(|(p, _)| p.signed_at / 86_400 == day)
            .map(|(p, s)| u128::from(s.map_or(p.gas_limit, |s| s.gas_used)))
            .sum::<u128>()
            + self
                .families
                .values()
                .filter(|f| f.signed_at() / 86_400 == day)
                .map(|f| {
                    u128::from(f.finalized.map_or_else(
                        || {
                            f.members
                                .iter()
                                .map(|m| m.publication.gas_limit)
                                .max()
                                .unwrap_or(0)
                        },
                        |(_, s)| s.gas_used,
                    ))
                })
                .sum::<u128>()
    }
    /// The wei the rate publisher spent on the chain day `day`: the cost of
    /// each settled publication signed that day.
    #[must_use]
    pub fn publication_wei(&self, day: u64) -> u128 {
        self.publications
            .values()
            .filter(|(p, _)| p.signed_at / 86_400 == day)
            .filter_map(|(_, s)| s.map(|s| s.cost_wei))
            .chain(
                self.families
                    .values()
                    .filter(|f| f.signed_at() / 86_400 == day)
                    .filter_map(|f| f.finalized.map(|(_, s)| s.cost_wei)),
            )
            .fold(0, u128::saturating_add)
    }
    /// The publications still able to spend: unsettled first-version
    /// publications and the newest publication of every unfinalized nonce.
    #[must_use]
    pub fn unsettled(&self) -> Vec<Publication> {
        let mut unsettled = self.legacy_unresolved();
        unsettled.extend(
            self.families
                .values()
                .filter(|f| f.finalized.is_none())
                .map(|f| f.latest().publication),
        );
        unsettled
    }
    /// The wei reserved by unsettled publications: each first-version one's
    /// fee per gas ceiling times its gas limit, and once per unfinalized nonce
    /// the most any transaction signed at it can spend.
    #[must_use]
    pub fn reserved_wei(&self) -> u128 {
        self.legacy_unresolved()
            .iter()
            .map(|p| p.max_fee_per_gas.saturating_mul(u128::from(p.gas_limit)))
            .chain(
                self.families
                    .values()
                    .filter(|f| f.finalized.is_none())
                    .map(Family::reserved_wei),
            )
            .fold(0, u128::saturating_add)
    }
    /// Whether a live submission or replacement of `sponsor` holds `nonce`.
    #[must_use]
    pub fn holds(&self, sponsor: Address, nonce: u64) -> bool {
        self.items.iter().any(|(key, item)| {
            key.sponsor == sponsor
                && (item.submission.as_ref().is_some_and(|s| s.nonce == nonce)
                    || item.replacement.as_ref().is_some_and(|r| r.nonce == nonce))
        })
    }
    fn apply(&mut self, entry: &Entry) -> Result<(), JournalError> {
        match entry {
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
                if submission.raw.first() != Some(&4) || keccak(&submission.raw) != submission.hash
                {
                    return Err(JournalError::Corrupt);
                }
                if self.holds(key.sponsor, submission.nonce) {
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
            Entry::Released { key, nonce } => {
                let item = self.items.get_mut(key).ok_or(JournalError::Conflict)?;
                if item.completion.is_some()
                    || item.replacement.is_some()
                    || item.submission.as_ref().is_none_or(|s| s.nonce != *nonce)
                {
                    return Err(JournalError::Conflict);
                }
                item.submission = None;
            }
            Entry::Replaced { key, replacement } => {
                if replacement.raw.first() != Some(&2)
                    || keccak(&replacement.raw) != replacement.hash
                {
                    return Err(JournalError::Corrupt);
                }
                let item = self.items.get_mut(key).ok_or(JournalError::Conflict)?;
                if item.completion.is_some()
                    || item.replacement.is_some()
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
            Entry::RatePublished { publication } => {
                if publication.hash == [0; 32]
                    || publication.rate == [0; 32]
                    || publication.gas_limit == 0
                    || publication.max_fee_per_gas == 0
                {
                    return Err(JournalError::Corrupt);
                }
                if self.publications.contains_key(&publication.hash)
                    || self.family_of(&publication.hash).is_some()
                {
                    return Err(JournalError::Conflict);
                }
                self.publications
                    .insert(publication.hash, (*publication, None));
            }
            Entry::RateSettled { hash, settlement } if !self.publications.contains_key(hash) => {
                let key = self.family_of(hash).ok_or(JournalError::Conflict)?;
                let family = self.families.get_mut(&key).ok_or(JournalError::Conflict)?;
                let (gas_limit, max_fee) =
                    family.gas_limit_of(hash).ok_or(JournalError::Conflict)?;
                if family.finalized.is_some()
                    || settlement.gas_used > gas_limit
                    || max_fee
                        .checked_mul(u128::from(settlement.gas_used))
                        .is_none_or(|most| settlement.cost_wei > most)
                {
                    return Err(JournalError::Conflict);
                }
                family.finalized = Some((*hash, *settlement));
            }
            Entry::RatePrepared { prepared } => {
                prepared.check()?;
                let p = prepared.publication;
                if let Some(family) = self.families.get(&(p.owner, p.nonce)) {
                    return if family.members[0] == *prepared {
                        Ok(())
                    } else {
                        Err(JournalError::Conflict)
                    };
                }
                if self.publications.contains_key(&p.hash) || self.family_of(&p.hash).is_some() {
                    return Err(JournalError::Conflict);
                }
                self.families.insert(
                    (p.owner, p.nonce),
                    Family {
                        members: vec![prepared.clone()],
                        cancellation: None,
                        broadcast: BTreeSet::new(),
                        finalized: None,
                    },
                );
            }
            Entry::RateBroadcast { hash } => {
                let key = self.family_of(hash).ok_or(JournalError::Conflict)?;
                let family = self.families.get_mut(&key).ok_or(JournalError::Conflict)?;
                if family.finalized.is_some() && !family.broadcast.contains(hash) {
                    return Err(JournalError::Conflict);
                }
                family.broadcast.insert(*hash);
            }
            Entry::RateReplaced { previous, prepared } => {
                prepared.check()?;
                let key = self.family_of(previous).ok_or(JournalError::Conflict)?;
                if self.publications.contains_key(&prepared.publication.hash)
                    || self.family_of(&prepared.publication.hash).is_some()
                {
                    return Err(JournalError::Conflict);
                }
                let family = self.families.get_mut(&key).ok_or(JournalError::Conflict)?;
                let latest = family.latest();
                let (old, new) = (&latest.publication, &prepared.publication);
                if family.finalized.is_some()
                    || family.cancellation.is_some()
                    || old.hash != *previous
                    || latest.chain_id != prepared.chain_id
                    || latest.paymaster != prepared.paymaster
                    || old.owner != new.owner
                    || old.nonce != new.nonce
                    || old.rate != new.rate
                    || new.signed_at < old.signed_at
                {
                    return Err(JournalError::Conflict);
                }
                let floor = latest.fees().replacement().map_err(|_| JournalError::Corrupt)?;
                if new.gas_limit != old.gas_limit
                    || new.max_fee_per_gas < floor.max_fee_per_gas
                    || prepared.max_priority_fee_per_gas < floor.max_priority_fee_per_gas
                {
                    return Err(JournalError::Corrupt);
                }
                family.members.push(prepared.clone());
            }
            Entry::RateCancelled {
                previous,
                cancellation,
            } => {
                if cancellation.raw.first() != Some(&2)
                    || keccak(&cancellation.raw) != cancellation.hash
                {
                    return Err(JournalError::Corrupt);
                }
                let key = self.family_of(previous).ok_or(JournalError::Conflict)?;
                if self.publications.contains_key(&cancellation.hash)
                    || self.family_of(&cancellation.hash).is_some()
                {
                    return Err(JournalError::Conflict);
                }
                let family = self.families.get_mut(&key).ok_or(JournalError::Conflict)?;
                let latest = family.latest();
                if family.finalized.is_some()
                    || family.cancellation.is_some()
                    || latest.publication.hash != *previous
                    || cancellation.nonce != latest.publication.nonce
                {
                    return Err(JournalError::Conflict);
                }
                if !cancellation
                    .fees
                    .replaces(latest.fees())
                    .map_err(|_| JournalError::Corrupt)?
                {
                    return Err(JournalError::Corrupt);
                }
                family.cancellation = Some(cancellation.clone());
            }
            Entry::RateSettled { hash, settlement } => {
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
        }
        Ok(())
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
        let file = OpenOptions::new()
            .read(true)
            .append(true)
            .create(true)
            .open(path)
            .map_err(|_| JournalError::Io)?;
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
        let mut next = self.state.clone();
        next.apply(entry)?;
        if next == self.state {
            return Ok(());
        }
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
        journal.append(&release)?;
        assert!(!journal.state().holds(signer.address(), 5));
        assert_eq!(journal.append(&release), Err(JournalError::Conflict));
        journal.append(&prepared(key(2), 5, 2))?;
        let Entry::Quoted { quote } = quoted(&signer, 2)? else {
            return Err("expected a quote".into());
        };
        let fees = quote.fees.replacement()?;
        let cancellation = crate::tx::sign_cancellation(1325, 5, fees, &signer)?;
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
            journal.append(&replaced(5, underpriced)),
            Err(JournalError::Corrupt)
        );
        assert_eq!(
            journal.append(&replaced(6, fees)),
            Err(JournalError::Conflict)
        );
        journal.append(&replaced(5, fees))?;
        assert_eq!(
            journal.append(&replaced(5, fees)),
            Err(JournalError::Conflict)
        );
        assert_eq!(
            journal.append(&Entry::Released {
                key: key(2),
                nonce: 5
            }),
            Err(JournalError::Conflict)
        );
        assert_eq!(
            journal.append(&prepared(key(3), 5, 3)),
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
        assert_eq!(journal.state().items[&key(1)].submission, None);
        assert_eq!(journal.state().items[&key(2)].completion, Some(cancelled));
        drop(journal);
        std::fs::remove_file(path)?;
        Ok(())
    }

    fn prepared_publication(
        signer: &impl QuoteSigner,
        nonce: u64,
        fees: Fees,
        signed_at: u64,
    ) -> Result<PreparedPublication, Box<dyn std::error::Error>> {
        let data = [&keccak(b"setRate(uint256)")[..4], word(3_114_000).as_slice()].concat();
        let signed = crate::tx::sign_call(1325, nonce, fees, [0x44; 20], &data, signer)?;
        Ok(PreparedPublication {
            version: PUBLICATION_VERSION,
            chain_id: 1325,
            paymaster: [0x44; 20],
            publication: Publication {
                owner: signer.address(),
                nonce,
                hash: signed.hash,
                rate: word(3_114_000),
                gas_limit: fees.gas_limit,
                max_fee_per_gas: fees.max_fee_per_gas,
                signed_at,
            },
            max_priority_fee_per_gas: fees.max_priority_fee_per_gas,
            raw: signed.raw,
        })
    }
    #[test]
    fn prepared_publications_recover_replace_cancel_and_finalize_once(
    ) -> Result<(), Box<dyn std::error::Error>> {
        let signer = crate::signer::tests::signer()?;
        let path = std::env::temp_dir().join(format!(
            "paxeer-journal-publication-{}.jsonl",
            std::process::id()
        ));
        if path.exists() {
            std::fs::remove_file(&path)?;
        }
        let fees = Fees {
            gas_limit: 60_000,
            max_fee_per_gas: 2_000_000_000,
            max_priority_fee_per_gas: 1_000_000_000,
        };
        let first = prepared_publication(&signer, 7, fees, 86_400)?;
        let mut journal = Journal::open(&path)?;
        journal.append(&Entry::RatePrepared {
            prepared: first.clone(),
        })?;
        let length = std::fs::metadata(&path)?.len();
        journal.append(&Entry::RatePrepared {
            prepared: first.clone(),
        })?;
        assert_eq!(std::fs::metadata(&path)?.len(), length);
        let mut other = first.clone();
        other.publication.signed_at += 1;
        assert_eq!(
            journal.append(&Entry::RatePrepared { prepared: other }),
            Err(JournalError::Conflict)
        );
        let second = prepared_publication(&signer, 7, Fees { max_fee_per_gas: 2_100_000_000, ..fees }, 86_401)?;
        assert_eq!(
            journal.append(&Entry::RatePrepared {
                prepared: second.clone()
            }),
            Err(JournalError::Conflict)
        );
        let mut malformed = first.clone();
        malformed.raw.push(0);
        assert_eq!(
            journal.append(&Entry::RatePrepared { prepared: malformed }),
            Err(JournalError::Corrupt)
        );
        let mut moved = first.clone();
        moved.version = 1;
        assert_eq!(
            journal.append(&Entry::RatePrepared { prepared: moved }),
            Err(JournalError::Corrupt)
        );
        assert_eq!(
            journal.state().unresolved().map(Family::latest),
            Some(&first)
        );
        assert_eq!(journal.state().reserved_wei(), 2_000_000_000 * 60_000);
        let hash = first.publication.hash;
        journal.append(&Entry::RateBroadcast { hash })?;
        journal.append(&Entry::RateBroadcast { hash })?;
        assert_eq!(
            journal.append(&Entry::RateReplaced {
                previous: hash,
                prepared: second.clone()
            }),
            Err(JournalError::Corrupt)
        );
        let bumped = fees.replacement()?;
        let third = prepared_publication(
            &signer,
            7,
            Fees {
                gas_limit: 60_000,
                ..bumped
            },
            86_402,
        )?;
        journal.append(&Entry::RateReplaced {
            previous: hash,
            prepared: third.clone(),
        })?;
        assert_eq!(
            journal.state().reserved_wei(),
            third.publication.max_fee_per_gas * 60_000
        );
        let cancellation = crate::tx::sign_cancellation(1325, 7, third.fees().replacement()?, &signer)?;
        let cancel = |previous| Entry::RateCancelled {
            previous,
            cancellation: Replacement {
                nonce: 7,
                fees: third.fees().replacement().unwrap_or(fees),
                hash: cancellation.hash,
                raw: cancellation.raw.clone(),
            },
        };
        assert_eq!(journal.append(&cancel(hash)), Err(JournalError::Conflict));
        journal.append(&cancel(third.publication.hash))?;
        let settlement = Settlement {
            block_number: 9,
            gas_used: 50_000,
            cost_wei: 50_000 * 1_000_000_000,
            succeeded: true,
        };
        assert_eq!(
            journal.append(&Entry::RateSettled {
                hash: cancellation.hash,
                settlement
            }),
            Err(JournalError::Conflict)
        );
        journal.append(&Entry::RateSettled { hash, settlement })?;
        assert_eq!(
            journal.append(&Entry::RateSettled {
                hash: third.publication.hash,
                settlement
            }),
            Err(JournalError::Conflict)
        );
        assert!(journal.state().unresolved().is_none());
        assert_eq!(journal.state().reserved_wei(), 0);
        assert_eq!(journal.state().publication_wei(1), settlement.cost_wei);
        let legacy = Publication {
            owner: signer.address(),
            nonce: 6,
            hash: word(66),
            rate: word(3_114_000),
            gas_limit: 60_000,
            max_fee_per_gas: 2_000_000_000,
            signed_at: 86_300,
        };
        journal.append(&Entry::RatePublished {
            publication: legacy,
        })?;
        assert_eq!(journal.state().legacy_unresolved(), vec![legacy]);
        assert_eq!(journal.state().reserved_wei(), 2_000_000_000 * 60_000);
        assert_eq!(
            journal.append(&Entry::RatePublished {
                publication: Publication { hash, ..legacy }
            }),
            Err(JournalError::Conflict)
        );
        let state = journal.state().clone();
        drop(journal);
        let journal = Journal::open(&path)?;
        assert_eq!(journal.state(), &state);
        drop(journal);
        std::fs::remove_file(path)?;
        Ok(())
    }
}
