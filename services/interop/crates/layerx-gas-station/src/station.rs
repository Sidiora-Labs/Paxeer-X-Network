use std::cell::{Cell, RefCell};
use std::time::{Duration, Instant};

use serde_json::{json, Value};

use crate::config::{DidGasLimit, StationConfig};
use crate::journal::{
    Admission, Completion, Entry, Item, Journal, JournalError, Key, LiabilityRelease, QuoteRecord,
    Submission,
};
use crate::policy::{BudgetKey, DidBudget};
use crate::price::{PriceError, PriceSource};
use crate::quote::{address_word, keccak, word, Address, Word};
use crate::rpc::{bytes, hex, quantity, read, JsonRpc, RpcFault};
use crate::signer::{QuoteSigner, SignerError};
use crate::tx::{self, Authorization, Call, Fees, TransactionRequest, TxError};
use crate::{QuoteError, QuoteRequest, SignedQuote, Station};

#[derive(Debug, Eq, PartialEq)]
pub enum StationError {
    Rpc(RpcFault),
    Journal(JournalError),
    Quote(QuoteError),
    Price(PriceError),
    Transaction(TxError),
    Invalid,
    Missing,
    Conflict,
    IncompatibleDelegation,
    ReplayStateUnavailable,
    SigningUnknown,
}
impl std::fmt::Display for StationError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "station refused: {self:?}")
    }
}
impl std::error::Error for StationError {}
impl From<RpcFault> for StationError {
    fn from(e: RpcFault) -> Self {
        Self::Rpc(e)
    }
}
impl From<JournalError> for StationError {
    fn from(e: JournalError) -> Self {
        Self::Journal(e)
    }
}
impl From<TxError> for StationError {
    fn from(e: TxError) -> Self {
        Self::Transaction(e)
    }
}

pub enum QuoteOutcome {
    Signed(SignedQuote),
    Completed(Completion),
}
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum Progress {
    Pending,
    Completed(Completion),
}
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
enum AccountCode {
    Undelegated,
    Delegated,
}
enum Receipt {
    Absent,
    Unfinalized,
    Final(Value),
}
pub struct SubmitRequest {
    pub key: Key,
    pub account: Address,
    pub calls: Vec<Call>,
    pub authorizations: Vec<Authorization>,
    pub account_signature: [u8; 65],
}
struct IntentSigner<'a, S> {
    signer: &'a S,
    journal: RefCell<&'a mut Journal>,
    key: Key,
    nonce: u64,
    error: Cell<Option<JournalError>>,
}
impl<S: QuoteSigner> QuoteSigner for IntentSigner<'_, S> {
    fn address(&self) -> Address {
        self.signer.address()
    }
    fn sign_digest(&self, digest: Word) -> Result<[u8; 65], SignerError> {
        let entry = Entry::SigningIntent {
            key: self.key,
            nonce: self.nonce,
            digest,
        };
        if let Err(error) = self.journal.borrow_mut().append(&entry) {
            self.error.set(Some(error));
            return Err(SignerError::Signing);
        }
        self.signer.sign_digest(digest)
    }
}
/// # Errors
/// Refuses conflicting journal state, an unknown signing outcome or a failing signer.
pub fn prepare_submission(
    journal: &mut Journal,
    mut request: TransactionRequest<'_>,
    did: &str,
    signer: &impl QuoteSigner,
) -> Result<Submission, StationError> {
    let key = Key {
        sponsor: signer.address(),
        quote_nonce: request.quote.quote.nonce,
    };
    let state = journal.state();
    let item = state.items.get(&key).ok_or(StationError::Missing)?;
    let quote = item.quote.as_ref().ok_or(StationError::Missing)?;
    if item.submission.is_some()
        || item.completion.is_some()
        || !state.tracked_quotes.contains(&key)
        || state.liability_releases.contains_key(&key)
        || quote.did != did
        || quote.account != request.account
        || quote.chain_id != request.chain_id
        || quote.paymaster != request.paymaster
        || quote.fees != request.fees
        || quote.batch_nonce != request.batch_nonce
        || quote.signed_quote()?.digest != request.quote.digest
        || quote.signature != request.quote.signature
    {
        return Err(StationError::Conflict);
    }
    if state.signing_intents.contains_key(&key) {
        return Err(StationError::SigningUnknown);
    }
    if state.items.iter().any(|(other, value)| {
        *other != key
            && value.account == request.account
            && value.completion.is_none()
            && !state.refused(other)
            && state.signing_intents.contains_key(other)
            && value
                .quote
                .as_ref()
                .is_some_and(|saved| saved.batch_nonce == request.batch_nonce)
    }) {
        return Err(StationError::Conflict);
    }
    request.nonce = state
        .next_nonce(key.sponsor, request.nonce)
        .ok_or(StationError::SigningUnknown)?;
    let nonce = request.nonce;
    let (transaction, journal_error) = {
        let signer = IntentSigner {
            signer,
            journal: RefCell::new(&mut *journal),
            key,
            nonce,
            error: Cell::new(None),
        };
        let transaction = tx::sign(&request, &signer);
        (transaction, signer.error.get())
    };
    if let Some(error) = journal_error {
        return Err(error.into());
    }
    let transaction = match transaction {
        Ok(value) => value,
        Err(_) if journal.state().signing_intents.contains_key(&key) => {
            return Err(StationError::SigningUnknown)
        }
        Err(error) => return Err(error.into()),
    };
    let submission = Submission {
        did: did.to_owned(),
        batch_nonce: request.batch_nonce,
        nonce,
        hash: transaction.hash,
        raw: transaction.raw,
    };
    journal.append(&Entry::Prepared {
        key,
        submission: submission.clone(),
    })?;
    Ok(submission)
}

pub struct GasStation<S, R, P> {
    station: Station<S>,
    rpc: R,
    prices: P,
    journal: Journal,
    recovery_cursor: Option<(u64, Key)>,
    liability_cursor: Option<Key>,
}
impl<S: QuoteSigner, R: JsonRpc, P: PriceSource> GasStation<S, R, P> {
    /// # Errors
    /// Refuses incompatible journal identity, policy history, configuration or chain identity.
    pub fn new(
        config: StationConfig,
        signer: S,
        rpc: R,
        prices: P,
        journal: Journal,
    ) -> Result<Self, StationError> {
        let mut station = Station::new(config, signer).map_err(|_| StationError::Invalid)?;
        let limit = std::env::var("GAS_STATION_DID_GAS_LIMIT").ok();
        let limit = DidGasLimit::try_from(limit.as_deref()).map_err(|_| StationError::Invalid)?;
        station.policy = station.policy.with_did_budget(DidBudget::new(limit.0));
        let chain: String = read(&rpc, "eth_chainId", json!([]))?;
        rpc.set_deadline(None)?;
        if quantity(&chain)? != u128::from(station.config.chain_id) {
            return Err(StationError::Invalid);
        }
        if journal
            .state()
            .items
            .values()
            .any(|item| item.submission.is_some() && item.completion == Some(Completion::Consumed))
        {
            return Err(StationError::Conflict);
        }
        let mut quotes: Vec<_> = journal
            .state()
            .items
            .values()
            .filter_map(|i| i.quote.as_ref())
            .collect();
        quotes.sort_by_key(|q| q.issued_at);
        for quote in quotes {
            if quote.chain_id != station.config.chain_id
                || quote.paymaster != station.config.paymaster
                || quote.token != station.config.token
                || quote.key.sponsor != station.signer.address()
            {
                return Err(StationError::Conflict);
            }
            if journal.state().admissions.values().any(|(admission, key)| {
                *key == quote.key
                    && admission.interval != quote.issued_at / station.config.interval_seconds
            }) {
                return Err(StationError::Conflict);
            }
            let budget = quote.budget_key()?;
            station
                .policy
                .restore_did_usage(budget.as_str(), quote.fees.gas_limit, quote.issued_at)
                .map_err(|error| StationError::Quote(QuoteError::Policy(error)))?;
            station
                .policy
                .restore_usage(
                    quote.account,
                    budget.as_str(),
                    amount(quote.amount)?,
                    amount(quote.gas_cost)?,
                    quote.issued_at,
                )
                .map_err(|e| StationError::Quote(QuoteError::Policy(e)))?;
        }
        station.policy.reconcile(
            journal.state().active_pax()?,
            journal.state().finalized_spend()?,
        );
        Ok(Self {
            station,
            rpc,
            prices,
            journal,
            recovery_cursor: None,
            liability_cursor: None,
        })
    }
    #[must_use]
    pub const fn journal(&self) -> &Journal {
        &self.journal
    }
    /// # Errors
    /// Refuses an unreachable node or a malformed block.
    pub fn chain_time(&self) -> Result<u64, StationError> {
        let block = self
            .rpc
            .call("eth_getBlockByNumber", json!(["latest", false]))?;
        u64::try_from(field_quantity(&block, "timestamp")?).map_err(|_| RpcFault::Malformed.into())
    }

    fn reconcile_policy(&mut self) -> Result<(), StationError> {
        self.station.policy.reconcile(
            self.journal.state().active_pax()?,
            self.journal.state().finalized_spend()?,
        );
        Ok(())
    }

    fn sponsor_floor(&self, additional: u128) -> Result<(), StationError> {
        let balance: String = read(
            &self.rpc,
            "eth_getBalance",
            json!([hex(&self.station.signer.address()), "pending"]),
        )?;
        self.station
            .policy
            .ensure_balance(quantity(&balance)?, additional)
            .map_err(|error| StationError::Quote(QuoteError::Policy(error)))
    }

    fn release_settled(&mut self, key: Key) -> Result<(), StationError> {
        if self.journal.state().liability_releases.contains_key(&key) {
            return Ok(());
        }
        let completion = self
            .journal
            .state()
            .items
            .get(&key)
            .and_then(|item| item.completion);
        let Some(Completion::Included { hash, .. } | Completion::Reverted { hash }) = completion
        else {
            return Ok(());
        };
        if !self.journal.state().receipts.contains_key(&hash)
            && !matches!(self.receipt(key, &hash)?, Receipt::Final(_))
        {
            return Ok(());
        }
        self.journal.append(&Entry::LiabilityReleased {
            key,
            proof: LiabilityRelease::Settled { hash },
        })?;
        self.reconcile_policy()
    }

    fn release_consumed(&mut self, key: Key) -> Result<(), StationError> {
        let state = self.journal.state();
        if state.liability_releases.contains_key(&key)
            || !state.tracked_quotes.contains(&key)
            || state.signing_intents.contains_key(&key)
        {
            return Ok(());
        }
        let item = state.items.get(&key).ok_or(StationError::Missing)?;
        if item.submission.is_some() || item.completion != Some(Completion::Consumed) {
            return Ok(());
        }
        let quote = item.quote.clone().ok_or(StationError::Missing)?;
        let finalized = self
            .rpc
            .call("eth_getBlockByNumber", json!(["finalized", false]))?;
        let height = field_quantity(&finalized, "number")?;
        let canonical = self.rpc.call(
            "eth_getBlockByNumber",
            json!([format!("0x{height:x}"), false]),
        )?;
        let block = json!({"blockHash":finalized["hash"],"requireCanonical":true});
        let code: String = read(
            &self.rpc,
            "eth_getCode",
            json!([hex(&quote.account), block]),
        )?;
        let data = [
            keccak(b"usedQuoteNonces(address,uint256)")[..4].to_vec(),
            address_word(key.sponsor).to_vec(),
            key.quote_nonce.to_vec(),
        ]
        .concat();
        let (params, result) = self.replay_observation(quote.account, &data, &block)?;
        if bytes(result.as_str().ok_or(RpcFault::Malformed)?)? != word(1) {
            return Err(RpcFault::Divergence.into());
        }
        let current = self
            .rpc
            .call("eth_getBlockByNumber", json!(["finalized", false]))?;
        let checked = self.rpc.call(
            "eth_getBlockByNumber",
            json!([format!("0x{height:x}"), false]),
        )?;
        if field_quantity(&current, "number")? < height || checked != canonical {
            return Err(RpcFault::Divergence.into());
        }
        self.journal.append(&Entry::LiabilityReleased {
            key,
            proof: LiabilityRelease::Consumed {
                chain_id: quote.chain_id,
                account: quote.account,
                paymaster: quote.paymaster,
                quote_nonce: key.quote_nonce,
                canonical,
                finalized,
                code,
                params,
                result,
            },
        })?;
        self.reconcile_policy()
    }

    fn reconcile_liabilities(&mut self, deadline: Instant) -> Result<(), StationError> {
        let mut keys: Vec<_> = self
            .journal
            .state()
            .items
            .iter()
            .filter(|(key, item)| {
                item.quote.is_some()
                    && !self.journal.state().liability_releases.contains_key(key)
                    && (item.submission.is_none() || item.completion.is_some())
            })
            .map(|(key, _)| *key)
            .collect();
        if let Some(cursor) = self.liability_cursor {
            let offset = keys.partition_point(|key| *key <= cursor);
            keys.rotate_left(offset);
        }
        let mut expiry = None;
        for key in keys.into_iter().take(64) {
            if Instant::now() >= deadline {
                break;
            }
            self.rpc.set_deadline(Some(deadline))?;
            let result = (|| {
                let item = self.journal.state().items[&key].clone();
                if item.submission.is_some() {
                    return self.release_settled(key);
                }
                if self.journal.state().signing_intents.contains_key(&key)
                    || !self.journal.state().tracked_quotes.contains(&key)
                {
                    return Ok(());
                }
                if item.completion == Some(Completion::Consumed) {
                    return self.release_consumed(key);
                }
                let quote = item.quote.ok_or(StationError::Missing)?;
                if expiry.is_none() {
                    let finalized = self
                        .rpc
                        .call("eth_getBlockByNumber", json!(["finalized", false]))?;
                    let height = field_quantity(&finalized, "number")?;
                    let canonical = self.rpc.call(
                        "eth_getBlockByNumber",
                        json!([format!("0x{height:x}"), false]),
                    )?;
                    expiry = Some((canonical, finalized));
                }
                let (canonical, finalized) = expiry.as_ref().ok_or(StationError::Missing)?;
                if field_quantity(finalized, "timestamp")? > u128::from(quote.deadline) {
                    self.journal.append(&Entry::LiabilityReleased {
                        key,
                        proof: LiabilityRelease::Expired {
                            canonical: canonical.clone(),
                            finalized: finalized.clone(),
                        },
                    })?;
                    self.reconcile_policy()?;
                }
                Ok(())
            })();
            self.rpc.set_deadline(None)?;
            result?;
            self.liability_cursor = Some(key);
        }
        Ok(())
    }

    /// # Errors
    /// Refuses conflicting admissions or exhausted interval capacity.
    pub fn admission(
        &self,
        identity: Word,
        digest: Word,
        account: Address,
        interval: u64,
    ) -> Result<Option<Key>, StationError> {
        if let Some((saved, key)) = self.journal.state().admissions.get(&identity) {
            if saved.request_digest != digest {
                return Err(StationError::Conflict);
            }
            return Ok(Some(*key));
        }
        let mut total = 0usize;
        let mut account_total = 0usize;
        let mut active = 0usize;
        for (key, item) in &self.journal.state().items {
            if item.quote.is_some() && !self.journal.state().liability_releases.contains_key(key) {
                active += 1;
            }
            if item.quote.as_ref().is_some_and(|quote| {
                quote.issued_at / self.station.config.interval_seconds == interval
            }) {
                total += 1;
                if item.account == account {
                    account_total += 1;
                }
            }
        }
        if total >= 128 || account_total >= 4 || active >= 1024 {
            return Err(StationError::Conflict);
        }
        Ok(None)
    }

    fn anchored_block(&self, tag: &str) -> Result<Value, StationError> {
        let block = self.rpc.call("eth_getBlockByNumber", json!([tag, false]))?;
        let hash = block["hash"].as_str().ok_or(RpcFault::Malformed)?;
        let decoded = bytes(hash)?;
        if decoded.len() != 32 || decoded.iter().all(|value| *value == 0) {
            return Err(RpcFault::Malformed.into());
        }
        Ok(json!({"blockHash": hash, "requireCanonical": true}))
    }
    fn account_code(&self, account: Address, block: &Value) -> Result<AccountCode, StationError> {
        let encoded: String = read(&self.rpc, "eth_getCode", json!([hex(&account), block]))?;
        if encoded.len() > 49_154 {
            return Err(StationError::IncompatibleDelegation);
        }
        let code = bytes(&encoded)?;
        if code.is_empty() {
            return Ok(AccountCode::Undelegated);
        }
        if code.len() == 23
            && code[..3] == [0xef, 0x01, 0x00]
            && code[3..] == self.station.config.paymaster[..]
        {
            return Ok(AccountCode::Delegated);
        }
        Err(StationError::IncompatibleDelegation)
    }
    fn replay_word(
        &self,
        account: Address,
        data: &[u8],
        block: &Value,
    ) -> Result<Word, StationError> {
        let (_, value) = self.replay_observation(account, data, block)?;
        bytes(value.as_str().ok_or(RpcFault::Malformed)?)?
            .try_into()
            .map_err(|_| StationError::ReplayStateUnavailable)
    }
    fn replay_observation(
        &self,
        account: Address,
        data: &[u8],
        block: &Value,
    ) -> Result<(Value, Value), StationError> {
        let request = json!({"to":hex(&account), "data":hex(data)});
        let params = match self.account_code(account, block)? {
            AccountCode::Delegated => json!([request, block]),
            AccountCode::Undelegated => {
                let code: String = read(
                    &self.rpc,
                    "eth_getCode",
                    json!([hex(&self.station.config.paymaster), block]),
                )?;
                if code.len() <= 2 || code.len() > 49_154 {
                    return Err(StationError::ReplayStateUnavailable);
                }
                let raw = bytes(&code)?;
                if raw.starts_with(&[0xef, 0x01, 0x00]) {
                    return Err(StationError::ReplayStateUnavailable);
                }
                let mut overrides = serde_json::Map::new();
                overrides.insert(hex(&account), json!({"code":code}));
                json!([request, block, Value::Object(overrides)])
            }
        };
        let result = self
            .rpc
            .call("eth_call", params.clone())
            .map_err(|error| match error {
                RpcFault::Rejected { .. } => StationError::ReplayStateUnavailable,
                other => StationError::Rpc(other),
            })?;
        let encoded = result
            .as_str()
            .ok_or(StationError::ReplayStateUnavailable)?;
        if encoded.len() != 66 || bytes(encoded)?.len() != 32 {
            return Err(StationError::ReplayStateUnavailable);
        }
        Ok((params, result))
    }
    /// The delegated account's batch nonce read at the pending block tag.
    /// # Errors
    /// Returns the RPC fault of a failed pending read, or refuses unavailable
    /// replay state or an incompatible delegation.
    pub fn batch_nonce(&self, account: Address) -> Result<Word, StationError> {
        self.replay_word(account, &keccak(b"nonce()")[..4], &json!("pending"))
    }
    fn consumed(&self, key: Key, account: Address) -> Result<bool, StationError> {
        let data = [
            keccak(b"usedQuoteNonces(address,uint256)")[..4].to_vec(),
            address_word(key.sponsor).to_vec(),
            key.quote_nonce.to_vec(),
        ]
        .concat();
        let block = self.anchored_block("finalized")?;
        match self.replay_word(account, &data, &block)? {
            value if value == word(0) => Ok(false),
            value if value == word(1) => Ok(true),
            _ => Err(StationError::ReplayStateUnavailable),
        }
    }
    fn receipt(&mut self, key: Key, hash: &Word) -> Result<Receipt, StationError> {
        let receipt = self
            .rpc
            .call("eth_getTransactionReceipt", json!([hex(hash)]))?;
        if receipt.is_null() {
            return Ok(Receipt::Absent);
        }
        let block = field_quantity(&receipt, "blockNumber")?;
        let finalized = self
            .rpc
            .call("eth_getBlockByNumber", json!(["finalized", false]))?;
        if field_quantity(&finalized, "number")? < block {
            return Ok(Receipt::Unfinalized);
        }
        let canonical = self.rpc.call(
            "eth_getBlockByNumber",
            json!([format!("0x{block:x}"), false]),
        )?;
        validate_finalized_receipt(hash, &receipt, &canonical, &finalized)?;
        if let Some(saved) = self.journal.state().receipts.get(hash) {
            if saved != &receipt {
                return Err(RpcFault::Divergence.into());
            }
        } else {
            self.journal.append(&Entry::ReceiptObserved {
                key,
                hash: *hash,
                receipt: receipt.clone(),
                canonical,
                finalized,
            })?;
        }
        Ok(Receipt::Final(receipt))
    }
    fn complete(
        &mut self,
        key: Key,
        account: Address,
        completion: Completion,
    ) -> Result<Progress, StationError> {
        self.journal.append(&Entry::Completed {
            key,
            account,
            completion,
        })?;
        self.release_settled(key)?;
        if completion == Completion::Consumed {
            self.release_consumed(key)?;
        }
        Ok(Progress::Completed(completion))
    }
    /// Resolves the budget key of `account` through addr 0x1004 `getLayerXDid`
    /// at pending state; an unbound account keys by its own address.
    fn budget_key(&self, account: Address) -> Result<BudgetKey, StationError> {
        let mut precompile = [0; 20];
        precompile[18..].copy_from_slice(&[0x10, 0x04]);
        let call = |method: &[u8]| {
            let data = [keccak(method)[..4].to_vec(), address_word(account).to_vec()].concat();
            read::<String>(
                &self.rpc,
                "eth_call",
                json!([{"to":hex(&precompile),"data":hex(&data)}, "pending"]),
            )
        };
        match call(b"getLayerXDid(address)") {
            Ok(encoded) => {
                BudgetKey::parse(&decode_did(&encoded)?).map_err(|_| RpcFault::Malformed.into())
            }
            Err(RpcFault::Rejected { .. }) => {
                let encoded = call(b"getUnifiedAccount(address)")?;
                if encoded.len() > 1026 {
                    return Err(RpcFault::Malformed.into());
                }
                let raw = bytes(&encoded)?;
                if raw.get(..32) != Some(address_word(account).as_slice())
                    || raw.get(32..64) != Some(word(128).as_slice())
                    || raw.get(64..96) != Some(word(0).as_slice())
                    || raw.get(96..128) != Some(word(0).as_slice())
                {
                    return Err(StationError::ReplayStateUnavailable);
                }
                abi_string(&raw, 128)?;
                BudgetKey::account(account).map_err(|_| StationError::Invalid)
            }
            Err(error) => Err(error.into()),
        }
    }

    /// # Errors
    /// Refuses invalid prices, policy, conflicting nonce reuse or an undurable quote.
    pub fn quote(
        &mut self,
        request: &QuoteRequest,
        fees: Fees,
        now: u64,
    ) -> Result<QuoteOutcome, StationError> {
        self.quote_admitted(request, fees, now, None)
    }

    fn validate_admission(
        &self,
        key: Key,
        request: &QuoteRequest,
        now: u64,
        admission: Option<Admission>,
    ) -> Result<(), StationError> {
        if let Some(admission) = admission {
            if admission.interval != now / self.station.config.interval_seconds
                || admission.identity
                    != Admission::identity(
                        request.account,
                        admission.batch_nonce,
                        admission.interval,
                    )
            {
                return Err(StationError::Invalid);
            }
            if self
                .admission(
                    admission.identity,
                    admission.request_digest,
                    request.account,
                    admission.interval,
                )?
                .is_some_and(|saved| saved != key)
            {
                return Err(StationError::Conflict);
            }
        }
        Ok(())
    }

    /// # Errors
    /// Refuses invalid prices, policy, admissions, conflicting nonce reuse or an undurable quote.
    pub fn quote_admitted(
        &mut self,
        request: &QuoteRequest,
        fees: Fees,
        now: u64,
        admission: Option<Admission>,
    ) -> Result<QuoteOutcome, StationError> {
        self.reconcile_liabilities(Instant::now() + Duration::from_secs(2))?;
        let key = Key {
            sponsor: self.station.signer.address(),
            quote_nonce: request.quote_nonce,
        };
        self.validate_admission(key, request, now, admission)?;
        if let Some(item) = self.journal.state().items.get(&key) {
            if item.account != request.account {
                return Err(StationError::Conflict);
            }
            if let Some(completion) = item.completion {
                return Ok(QuoteOutcome::Completed(completion));
            }
            let saved = item.quote.as_ref().ok_or(StationError::Missing)?;
            if saved.maximum != word(request.max_token_amount)
                || saved.gas_cost != word(request.gas_cost)
                || saved.deadline != request.deadline
                || saved.fees != fees
            {
                return Err(StationError::Conflict);
            }
            return Ok(QuoteOutcome::Signed(saved.signed_quote()?));
        }
        if self.consumed(key, request.account)? {
            self.complete(key, request.account, Completion::Consumed)?;
            return Ok(QuoteOutcome::Completed(Completion::Consumed));
        }
        if fees.gas_cost()? != request.gas_cost {
            return Err(StationError::Invalid);
        }
        let balance: String = read(
            &self.rpc,
            "eth_getBalance",
            json!([hex(&key.sponsor), "pending"]),
        )?;
        let rate = self.prices.governed_rate().map_err(StationError::Price)?;
        let budget = self.budget_key(request.account);
        let batch_nonce = self.batch_nonce(request.account)?;
        if admission.is_some_and(|value| value.batch_nonce != batch_nonce) {
            return Err(StationError::Conflict);
        }
        if self.journal.state().items.iter().any(|(other, item)| {
            *other != key
                && item.account == request.account
                && item.completion.is_none()
                && !self.journal.state().refused(other)
                && self.journal.state().signing_intents.contains_key(other)
                && item
                    .quote
                    .as_ref()
                    .is_some_and(|saved| saved.batch_nonce == batch_nonce)
        }) {
            return Err(StationError::Conflict);
        }
        let mut policy = self.station.policy.clone();
        let signed = policy
            .quote_for_budget(
                request,
                &rate,
                fees,
                &self.station.signer,
                budget.as_ref(),
                (quantity(&balance)?, now),
            )
            .map_err(StationError::Quote)?;
        let budget = budget?;
        let record = Box::new(QuoteRecord {
            key,
            did: budget.as_str().to_owned(),
            batch_nonce,
            chain_id: self.station.config.chain_id,
            paymaster: self.station.config.paymaster,
            account: request.account,
            token: signed.quote.token,
            maximum: signed.quote.max_token_amount,
            amount: signed.quote.token_amount,
            deadline: request.deadline,
            gas_cost: signed.quote.gas_cost,
            issued_at: now,
            signature: signed.signature.to_vec(),
            fees,
        });
        let entry = match admission {
            Some(admission) => Entry::QuoteAdmitted {
                quote: record,
                admission,
            },
            None => Entry::QuoteReserved { quote: record },
        };
        self.journal.append(&entry)?;
        self.station.policy = policy;
        self.reconcile_policy()?;
        Ok(QuoteOutcome::Signed(signed))
    }
    /// # Errors
    /// Refuses account signatures, authorizations, expired quotes or conflicting journal state.
    pub fn submit(&mut self, request: &SubmitRequest, now: u64) -> Result<Progress, StationError> {
        if self
            .journal
            .state()
            .signing_intents
            .contains_key(&request.key)
            && self
                .journal
                .state()
                .items
                .get(&request.key)
                .is_some_and(|item| item.submission.is_none())
        {
            return Err(StationError::SigningUnknown);
        }
        if self
            .journal
            .state()
            .liability_releases
            .get(&request.key)
            .is_some_and(|proof| matches!(proof, LiabilityRelease::Expired { .. }))
        {
            return Err(StationError::Invalid);
        }
        if request.key.sponsor != self.station.signer.address() {
            return Err(StationError::Invalid);
        }
        if let Some(item) = self.journal.state().items.get(&request.key) {
            if item.account != request.account {
                return Err(StationError::Conflict);
            }
            if let Some(done) = item.completion {
                return Ok(Progress::Completed(done));
            }
            if item.submission.is_some() {
                return self.resume(request.key, now);
            }
        }
        if self.consumed(request.key, request.account)? {
            return self.complete(request.key, request.account, Completion::Consumed);
        }
        let quote = self
            .journal
            .state()
            .items
            .get(&request.key)
            .and_then(|i| i.quote.clone())
            .ok_or(StationError::Missing)?;
        if quote.deadline < now {
            return Err(StationError::Invalid);
        }
        let signed = quote.signed_quote()?;
        let batch_nonce = self.batch_nonce(request.account)?;
        self.account_code(request.account, &json!("pending"))?;
        let account_nonce = self.rpc.pending_nonce(&request.account)?;
        if request.authorizations.len() != 1
            || request.authorizations[0].chain_id != self.station.config.chain_id
            || request.authorizations[0].delegate != self.station.config.paymaster
            || request.authorizations[0].nonce != account_nonce
        {
            return Err(StationError::Invalid);
        }
        if batch_nonce != quote.batch_nonce
            || self.budget_key(request.account)?.as_str() != quote.did
        {
            return Err(StationError::Conflict);
        }
        let nonce = self.rpc.pending_nonce(&request.key.sponsor)?;
        self.sponsor_floor(0)?;
        prepare_submission(
            &mut self.journal,
            TransactionRequest {
                chain_id: quote.chain_id,
                account: request.account,
                paymaster: quote.paymaster,
                nonce,
                batch_nonce,
                fees: quote.fees,
                calls: &request.calls,
                authorizations: &request.authorizations,
                quote: &signed,
                account_signature: &request.account_signature,
            },
            &quote.did,
            &self.station.signer,
        )?;
        self.broadcast(request.key)
    }

    /// Sends the durable bytes of `key`. A definitive refusal the sequencer
    /// holds no record of closes the submission and frees its sponsor nonce;
    /// an ambiguous send stays pending with the nonce held.
    fn broadcast(&mut self, key: Key) -> Result<Progress, StationError> {
        self.sponsor_floor(0)?;
        let submission = self
            .journal
            .state()
            .items
            .get(&key)
            .and_then(|i| i.submission.clone())
            .ok_or(StationError::Missing)?;
        match self
            .rpc
            .send_raw_transaction(&submission.raw, &submission.hash)
        {
            Ok(_)
            | Err(
                RpcFault::Unavailable
                | RpcFault::RateLimited
                | RpcFault::Divergence
                | RpcFault::Malformed,
            ) => Ok(Progress::Pending),
            Err(RpcFault::Rejected { code }) => {
                if self.sequencer_record(&submission)? {
                    return Ok(Progress::Pending);
                }
                self.refuse(key, submission.hash)?;
                Err(RpcFault::Rejected { code }.into())
            }
            Err(error) => Err(error.into()),
        }
    }
    /// Whether the sequencer holds `submission` at its sponsor nonce.
    fn sequencer_record(&self, submission: &Submission) -> Result<bool, StationError> {
        match self.rpc.sequencer_receipt(&submission.hash)? {
            Some(receipt) if receipt.nonce == submission.nonce => Ok(true),
            Some(_) => Err(RpcFault::Divergence.into()),
            None => Ok(false),
        }
    }
    fn refuse(&mut self, key: Key, hash: Word) -> Result<(), StationError> {
        self.journal.append(&Entry::Refused { key, hash })?;
        self.reconcile_policy()
    }
    /// # Errors
    /// Refuses malformed receipts or RPC failures; every rebroadcast uses the
    /// durable bytes.
    pub fn resume(&mut self, key: Key, now: u64) -> Result<Progress, StationError> {
        self.advance(key, now)
    }
    /// Advances one submission from its finalized receipt, else from the
    /// sequencer's record of its hash: held there, it stays pending; unknown
    /// there after its quote deadline, it is closed and its nonce freed;
    /// unknown before, its durable bytes are sent again.
    fn advance(&mut self, key: Key, now: u64) -> Result<Progress, StationError> {
        let item = self
            .journal
            .state()
            .items
            .get(&key)
            .cloned()
            .ok_or(StationError::Missing)?;
        if let Some(done) = item.completion {
            return Ok(Progress::Completed(done));
        }
        if self.journal.state().refused(&key) {
            return Err(StationError::Invalid);
        }
        let Some(submission) = item.submission else {
            if self.journal.state().signing_intents.contains_key(&key) {
                return Err(StationError::SigningUnknown);
            }
            return Ok(Progress::Pending);
        };
        let quote = item.quote.as_ref().ok_or(StationError::Missing)?;
        match self.receipt(key, &submission.hash)? {
            Receipt::Final(receipt) => {
                let completion = receipt_completion(&receipt, &submission, quote)?;
                return self.complete(key, item.account, completion);
            }
            Receipt::Unfinalized => return Ok(Progress::Pending),
            Receipt::Absent => (),
        }
        if self.sequencer_record(&submission)? {
            return Ok(Progress::Pending);
        }
        if quote.deadline < now {
            self.refuse(key, submission.hash)?;
            return Err(StationError::Invalid);
        }
        self.broadcast(key)
    }
    /// The submissions the journal holds without a completion or a refusal:
    /// prepared or broadcast, oldest sponsor nonce first.
    #[must_use]
    pub fn unresolved(&self) -> Vec<Key> {
        let mut keys: Vec<_> = self
            .journal
            .state()
            .items
            .iter()
            .filter(|(key, item)| {
                item.completion.is_none()
                    && !self.journal.state().refused(key)
                    && (item.submission.is_some()
                        || self.journal.state().signing_intents.contains_key(key))
            })
            .map(|(key, item)| {
                (
                    item.submission.as_ref().map_or_else(
                        || {
                            self.journal
                                .state()
                                .signing_intents
                                .get(key)
                                .copied()
                                .unwrap_or(0)
                        },
                        |s| s.nonce,
                    ),
                    *key,
                )
            })
            .collect();
        keys.sort_unstable();
        keys.into_iter().map(|(_, key)| key).collect()
    }
    /// One recovery pass over every unresolved submission, at most until
    /// `budget` elapses: a finalized receipt completes it, a sequencer record
    /// of its hash keeps it pending, and a definitive refusal or an unknown
    /// hash past its quote deadline frees its sponsor nonce without sending
    /// it again. A key the nodes cannot answer for stays pending; a divergent
    /// or malformed observation, a conflicting record or a failing journal
    /// stops the pass without acting on it.
    /// # Errors
    /// Refuses divergent or malformed observations and journal failures.
    pub fn recover(&mut self, now: u64, budget: Duration) -> Result<Recovery, StationError> {
        let deadline = Instant::now()
            .checked_add(budget)
            .ok_or(StationError::Invalid)?;
        let mut recovery = Recovery::default();
        match self.reconcile_liabilities(deadline) {
            Ok(()) => (),
            Err(StationError::Rpc(RpcFault::Unavailable | RpcFault::RateLimited)) => {
                recovery.liability_unreachable = true;
            }
            Err(error) => return Err(error),
        }
        let mut keys: Vec<_> = self
            .unresolved()
            .into_iter()
            .map(|key| {
                (
                    self.journal.state().items[&key]
                        .submission
                        .as_ref()
                        .map_or_else(
                            || {
                                self.journal
                                    .state()
                                    .signing_intents
                                    .get(&key)
                                    .copied()
                                    .unwrap_or(0)
                            },
                            |s| s.nonce,
                        ),
                    key,
                )
            })
            .collect();
        if let Some(cursor) = self.recovery_cursor {
            let offset = keys.partition_point(|key| *key <= cursor);
            keys.rotate_left(offset);
        }
        for (nonce, key) in keys {
            if Instant::now() >= deadline {
                recovery.deferred += 1;
                continue;
            }
            self.recovery_cursor = Some((nonce, key));
            self.rpc.set_deadline(Some(deadline))?;
            let result = self.advance(key, now);
            self.rpc.set_deadline(None)?;
            match result {
                Ok(Progress::Completed(_)) => recovery.completed += 1,
                Ok(Progress::Pending) => recovery.pending += 1,
                Err(StationError::Invalid | StationError::Rpc(RpcFault::Rejected { .. }))
                    if self.journal.state().refused(&key) =>
                {
                    recovery.refused += 1;
                }
                Err(StationError::Rpc(RpcFault::Unavailable | RpcFault::RateLimited)) => {
                    recovery.unreachable += 1;
                }
                Err(
                    StationError::SigningUnknown
                    | StationError::Quote(QuoteError::Policy(
                        crate::policy::PolicyRefusal::BalanceFloor,
                    )),
                ) => {
                    recovery.deferred += 1;
                }
                Err(error) => return Err(error),
            }
        }
        Ok(recovery)
    }

    fn authenticated(
        &self,
        key: Key,
        account: Address,
        relayer_signature: &[u8; 65],
    ) -> Result<&Item, StationError> {
        let item = self
            .journal
            .state()
            .items
            .get(&key)
            .ok_or(StationError::Missing)?;
        let quote = item.quote.as_ref().ok_or(StationError::Missing)?;
        if item.account != account || quote.signature.as_slice() != relayer_signature.as_slice() {
            return Err(StationError::Missing);
        }
        Ok(item)
    }
    /// The durable state of one submission identity, readable by whoever holds
    /// the station's signature over its quote, before or after the quote's
    /// deadline.
    /// # Errors
    /// Refuses an unknown identity or a signature the station did not issue for it.
    pub fn status(
        &self,
        key: Key,
        account: Address,
        relayer_signature: &[u8; 65],
    ) -> Result<Status, StationError> {
        let item = self.authenticated(key, account, relayer_signature)?;
        if item.completion.is_none()
            && item.submission.is_none()
            && self.journal.state().signing_intents.contains_key(&key)
        {
            return Err(StationError::SigningUnknown);
        }
        let deadline = item.quote.as_ref().map_or(0, |q| q.deadline);
        Ok(Status {
            deadline,
            submission: item.submission.as_ref().map(|s| (s.nonce, s.hash)),
            refused: self.journal.state().refused(&key),
            completion: item.completion,
        })
    }
    /// Resumes one already submitted identity from its durable bytes, also
    /// after its quote expired, without signing anything new for the client.
    /// An identity that was quoted but never submitted is refused once its
    /// quote expired, as a fresh submission would be.
    /// # Errors
    /// Refuses unauthenticated, unsubmitted expired or failing identities.
    pub fn retry(
        &mut self,
        key: Key,
        account: Address,
        relayer_signature: &[u8; 65],
        now: u64,
    ) -> Result<Progress, StationError> {
        let item = self.authenticated(key, account, relayer_signature)?;
        if let Some(done) = item.completion {
            return Ok(Progress::Completed(done));
        }
        if item.submission.is_none() {
            return Err(if self.journal.state().signing_intents.contains_key(&key) {
                StationError::SigningUnknown
            } else {
                StationError::Invalid
            });
        }
        self.rpc
            .set_deadline(Some(Instant::now() + Duration::from_secs(20)))?;
        let result = self.advance(key, now);
        self.rpc.set_deadline(None)?;
        result
    }
}
/// The counts of one recovery pass: submissions completed, still pending,
/// refused with their sponsor nonce freed, unanswered by the nodes and
/// deferred to the next pass by its time budget.
#[derive(Clone, Copy, Debug, Default, Eq, PartialEq)]
pub struct Recovery {
    pub liability_unreachable: bool,
    pub completed: usize,
    pub pending: usize,
    pub refused: usize,
    pub unreachable: usize,
    pub deferred: usize,
}
/// The durable state of one submission identity: its quote deadline, the
/// sponsor nonce and hash of its prepared transaction, whether the sequencer
/// refused it and freed that nonce, and its completion.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct Status {
    pub deadline: u64,
    pub submission: Option<(u64, Word)>,
    pub refused: bool,
    pub completion: Option<Completion>,
}

fn abi_string(raw: &[u8], offset: usize) -> Result<&str, StationError> {
    let length: Word = raw
        .get(offset..offset + 32)
        .ok_or(RpcFault::Malformed)?
        .try_into()
        .map_err(|_| RpcFault::Malformed)?;
    let length = usize::try_from(amount(length)?).map_err(|_| RpcFault::Malformed)?;
    let start = offset.checked_add(32).ok_or(RpcFault::Malformed)?;
    let end = start.checked_add(length).ok_or(RpcFault::Malformed)?;
    let padded = length.checked_add(31).ok_or(RpcFault::Malformed)? / 32 * 32;
    let tail = start.checked_add(padded).ok_or(RpcFault::Malformed)?;
    if raw.len() != tail
        || raw
            .get(end..tail)
            .ok_or(RpcFault::Malformed)?
            .iter()
            .any(|byte| *byte != 0)
    {
        return Err(RpcFault::Malformed.into());
    }
    std::str::from_utf8(raw.get(start..end).ok_or(RpcFault::Malformed)?)
        .map_err(|_| RpcFault::Malformed.into())
}

fn decode_did(encoded: &str) -> Result<String, StationError> {
    if encoded.len() > 386 {
        return Err(RpcFault::Malformed.into());
    }
    let raw = bytes(encoded)?;
    let key: Word = raw
        .get(..32)
        .ok_or(RpcFault::Malformed)?
        .try_into()
        .map_err(|_| RpcFault::Malformed)?;
    if key == [0; 32] || raw.get(32..64) != Some(word(64).as_slice()) {
        return Err(RpcFault::Malformed.into());
    }
    let did = abi_string(&raw, 64)?;
    if did != format!("did:layerx:{}", &hex(&key)[2..]) {
        return Err(RpcFault::Malformed.into());
    }
    Ok(did.to_owned())
}

fn amount(value: Word) -> Result<u128, StationError> {
    if value[..16] != [0; 16] {
        return Err(StationError::Invalid);
    }
    Ok(u128::from_be_bytes(
        value[16..].try_into().map_err(|_| StationError::Invalid)?,
    ))
}
fn field_quantity(value: &Value, name: &str) -> Result<u128, RpcFault> {
    quantity(value[name].as_str().ok_or(RpcFault::Malformed)?)
}
pub(crate) fn validate_finalized_receipt(
    hash: &Word,
    receipt: &Value,
    canonical: &Value,
    finalized: &Value,
) -> Result<(), RpcFault> {
    let receipt_hash = bytes(
        receipt["transactionHash"]
            .as_str()
            .ok_or(RpcFault::Malformed)?,
    )?;
    let block_hash = bytes(receipt["blockHash"].as_str().ok_or(RpcFault::Malformed)?)?;
    let final_hash = bytes(finalized["hash"].as_str().ok_or(RpcFault::Malformed)?)?;
    let block = field_quantity(receipt, "blockNumber")?;
    if receipt_hash.as_slice() != hash || block_hash.len() != 32 || final_hash.len() != 32 {
        return Err(RpcFault::Malformed);
    }
    if canonical["hash"] != receipt["blockHash"]
        || field_quantity(canonical, "number")? != block
        || field_quantity(finalized, "number")? < block
    {
        return Err(RpcFault::Divergence);
    }
    Ok(())
}

fn receipt_completion(
    receipt: &Value,
    submission: &Submission,
    quote: &QuoteRecord,
) -> Result<Completion, StationError> {
    if receipt["transactionHash"] != hex(&submission.hash)
        || receipt["from"] != hex(&quote.key.sponsor)
        || receipt["to"] != hex(&quote.account)
    {
        return Err(RpcFault::Malformed.into());
    }
    let status = field_quantity(receipt, "status")?;
    if status == 0 {
        return Ok(Completion::Reverted {
            hash: submission.hash,
        });
    }
    if status != 1 {
        return Err(RpcFault::Malformed.into());
    }
    let gas = field_quantity(receipt, "gasUsed")?;
    let price = field_quantity(receipt, "effectiveGasPrice")?;
    if gas == 0 || gas > u128::from(quote.fees.gas_limit) || price > quote.fees.max_fee_per_gas {
        return Err(RpcFault::Malformed.into());
    }
    let spent = gas.checked_mul(price).ok_or(RpcFault::Malformed)?;
    let logs = receipt["logs"].as_array().ok_or(RpcFault::Malformed)?;
    let transfer = json!([
        hex(&keccak(b"Transfer(address,address,uint256)")),
        hex(&address_word(quote.account)),
        hex(&address_word(quote.key.sponsor))
    ]);
    let sponsored = json!([
        hex(&keccak(b"Sponsored(address,address,uint256,uint256)")),
        hex(&address_word(quote.key.sponsor)),
        hex(&address_word(quote.token))
    ]);
    let mut collected = 0_u128;
    let mut sponsored_count = 0;
    for log in logs {
        if log["removed"] == true {
            return Err(RpcFault::Malformed.into());
        }
        if log["address"] == hex(&quote.token) && log["topics"] == transfer {
            let data = bytes(log["data"].as_str().ok_or(RpcFault::Malformed)?)?;
            let value: Word = data.try_into().map_err(|_| RpcFault::Malformed)?;
            collected = collected
                .checked_add(amount(value)?)
                .ok_or(RpcFault::Malformed)?;
        }
        if log["address"] == hex(&quote.account) && log["topics"] == sponsored {
            if log["data"] != hex(&[quote.amount, quote.key.quote_nonce].concat()) {
                return Err(RpcFault::Malformed.into());
            }
            sponsored_count += 1;
        }
    }
    if word(collected) != quote.amount || sponsored_count != 1 {
        return Err(RpcFault::Malformed.into());
    }
    Ok(Completion::Included {
        hash: submission.hash,
        block_number: u64::try_from(field_quantity(receipt, "blockNumber")?)
            .map_err(|_| RpcFault::Malformed)?,
        sid_collected: word(collected),
        pax_spent: word(spent),
    })
}
#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn rejects_wide_amounts_and_noncanonical_receipt_quantities() {
        assert!(amount([255; 32]).is_err());
        assert_eq!(amount(word(u128::MAX)).ok(), Some(u128::MAX));
        assert_eq!(
            field_quantity(&json!({"gasUsed":"0x00"}), "gasUsed"),
            Err(RpcFault::Malformed)
        );
        assert_eq!(
            field_quantity(&json!({}), "gasUsed"),
            Err(RpcFault::Malformed)
        );
    }
}
