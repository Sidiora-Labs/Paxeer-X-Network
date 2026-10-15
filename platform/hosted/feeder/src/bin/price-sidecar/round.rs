//! Start checks, the feed round and its retry plan: Chainflow tickers into
//! one `MsgSubmitPrices`, the account read with `abci_query` and the
//! broadcast with `broadcast_tx_sync` on the consensus RPC.

use std::path::Path;
use std::sync::Mutex;
use std::thread;
use std::time::Duration;

use base64::engine::general_purpose::STANDARD;
use base64::Engine as _;
use layerx_oracle_feeder::chainflow::{FetchError, Ticker};
use layerx_oracle_feeder::cosmos_tx::{self, encode_tx, Observation, Submission};
use layerx_oracle_feeder::{address_word, hex_decode, hex_encode, Journal, Sequencer};
use serde_json::{json, Value};
use zeroize::Zeroizing;

use crate::health::{lock, Metrics};
use crate::{now_ms, Config, MarketConfig};

/// Price feed precompile.
pub const PRICEFEED: [u8; 20] = {
    let mut address = [0; 20];
    address[18] = 0x10;
    address[19] = 0x20;
    address
};
/// `getValidatorByFeedAccount(address)`.
pub const GET_VALIDATOR_BY_FEED_ACCOUNT_SELECTOR: [u8; 4] = [0x00, 0x7e, 0xb8, 0x55];
/// `getObservation(uint256,address)`.
pub const GET_OBSERVATION_SELECTOR: [u8; 4] = [0x48, 0xf8, 0x36, 0x66];
/// gRPC query path of the auth module's account query.
pub const ACCOUNT_QUERY_PATH: &str = "/cosmos.auth.v1beta1.Query/Account";
const BASE_ACCOUNT_TYPE_URL: &str = "/cosmos.auth.v1beta1.BaseAccount";
const PRICEFEED_CODESPACE: &str = "pricefeed";
const CODE_NOT_VALIDATOR: u64 = 5;
const CODE_UNREGISTERED_FEEDER: u64 = 10;
const CODE_RATE_LIMITED: u64 = 11;
/// Text of the auth signature check's sequence refusal.
const SEQUENCE_MISMATCH: &str = "account sequence mismatch";
/// `getObservation` answers a static five-word tuple.
const OBSERVATION_ANSWER_BYTES: usize = 5 * 32;

/// JSON-RPC clients of the consensus RPC and the EVM RPC.
pub struct Rpc {
    agent: ureq::Agent,
    consensus_url: String,
    evm: Sequencer,
}

impl Rpc {
    /// # Errors
    /// Refuses endpoints that are not http or https URLs.
    pub fn new(agent: ureq::Agent, consensus_url: String, evm_url: String) -> Result<Self, String> {
        if !consensus_url.starts_with("https://") && !consensus_url.starts_with("http://") {
            return Err("consensus RPC endpoint must be an http or https URL".to_owned());
        }
        let evm = Sequencer::new(agent.clone(), evm_url)
            .map_err(|_| "EVM RPC endpoint must be an http or https URL".to_owned())?;
        Ok(Self {
            agent,
            consensus_url,
            evm,
        })
    }

    /// Posts one consensus JSON-RPC call and returns the response body.
    ///
    /// # Errors
    /// Transport failures and HTTP statuses other than 200.
    fn consensus(&self, method: &str, params: &Value) -> Result<String, String> {
        let request = json!({"jsonrpc": "2.0", "id": 1, "method": method, "params": params});
        let mut response = self
            .agent
            .post(&self.consensus_url)
            .config()
            .http_status_as_error(false)
            .build()
            .send_json(&request)
            .map_err(|e| format!("{method}: {e}"))?;
        let status = response.status().as_u16();
        if status != 200 {
            return Err(format!("{method}: status {status}"));
        }
        response
            .body_mut()
            .read_to_string()
            .map_err(|e| format!("{method}: {e}"))
    }

    /// `abci_query` of the auth account of `feeder` (bech32) at the latest height.
    ///
    /// # Errors
    /// Transport failures.
    pub fn query_account(&self, feeder: &str) -> Result<String, String> {
        let mut data = vec![0x0a];
        put_varint(&mut data, feeder.len() as u64);
        data.extend_from_slice(feeder.as_bytes());
        self.consensus(
            "abci_query",
            &json!({
                "path": ACCOUNT_QUERY_PATH,
                "data": hex_encode(&data),
                "height": "0",
                "prove": false,
            }),
        )
    }

    /// `broadcast_tx_sync` of the `TxRaw` bytes.
    ///
    /// # Errors
    /// Transport failures.
    pub fn broadcast(&self, tx: &[u8]) -> Result<String, String> {
        self.consensus("broadcast_tx_sync", &json!({"tx": STANDARD.encode(tx)}))
    }

    /// `eth_call` of `data` on 0x1020 at the latest block; returns the hex answer.
    ///
    /// # Errors
    /// Transport failures, JSON-RPC errors and a non-string answer.
    pub fn call_pricefeed(&self, data: &[u8]) -> Result<String, String> {
        let answer = self.evm.call(
            "eth_call",
            &json!([{
                "to": format!("0x{}", hex_encode(&PRICEFEED)),
                "data": format!("0x{}", hex_encode(data)),
            }, "latest"]),
        )?;
        answer
            .as_str()
            .map(str::to_owned)
            .ok_or_else(|| "eth_call: malformed result".to_owned())
    }
}

fn put_varint(out: &mut Vec<u8>, mut value: u64) {
    while value >= 0x80 {
        out.push((value.to_le_bytes()[0] & 0x7f) | 0x80);
        value >>= 7;
    }
    out.push(value.to_le_bytes()[0]);
}

fn address_arg(address: &[u8; 20]) -> [u8; 32] {
    let mut word = [0; 32];
    word[12..].copy_from_slice(address);
    word
}

/// `getValidatorByFeedAccount(feedAccount)` calldata.
#[must_use]
pub fn validator_by_feed_account_call(feed_account: &[u8; 20]) -> Vec<u8> {
    let mut data = GET_VALIDATOR_BY_FEED_ACCOUNT_SELECTOR.to_vec();
    data.extend_from_slice(&address_arg(feed_account));
    data
}

/// `getObservation(marketId, validator)` calldata.
#[must_use]
pub fn observation_call(market_id: u64, validator: &[u8; 20]) -> Vec<u8> {
    let mut data = GET_OBSERVATION_SELECTOR.to_vec();
    let mut id = [0; 32];
    id[24..].copy_from_slice(&market_id.to_be_bytes());
    data.extend_from_slice(&id);
    data.extend_from_slice(&address_arg(validator));
    data
}

/// Refuses to start unless `getValidatorByFeedAccount` answered `validator`.
///
/// # Errors
/// Names the feed account, the configured validator and the answer.
pub fn check_registration(
    answer: &str,
    validator: &[u8; 20],
    feed_account: &[u8; 20],
) -> Result<(), String> {
    let registered = address_word(answer)?;
    if &registered == validator {
        Ok(())
    } else {
        Err(format!(
            "feed account 0x{} is not registered to validator 0x{}: getValidatorByFeedAccount answered 0x{}",
            hex_encode(feed_account),
            hex_encode(validator),
            hex_encode(&registered)
        ))
    }
}

/// The stored sequence of a `getObservation` answer.
///
/// # Errors
/// Refuses an answer other than five words with a `uint64` first word.
pub fn observation_sequence(answer: &str) -> Result<u64, String> {
    let bytes = hex_decode(answer.strip_prefix("0x").unwrap_or(answer))?;
    if bytes.len() != OBSERVATION_ANSWER_BYTES || bytes[..24].iter().any(|byte| *byte != 0) {
        return Err("malformed getObservation answer".to_owned());
    }
    let mut sequence = [0; 8];
    sequence.copy_from_slice(&bytes[24..32]);
    Ok(u64::from_be_bytes(sequence))
}

/// Raises the journal so the next sequence of `symbol` is at least
/// `getObservation sequence + 1`.
///
/// # Errors
/// Refuses a malformed `getObservation` answer.
pub fn sync_sequence(journal: &mut Journal, symbol: &str, answer: &str) -> Result<(), String> {
    let stored = observation_sequence(answer)?;
    if stored >= journal.next_observation_sequence(symbol) {
        journal
            .markets
            .entry(symbol.to_owned())
            .or_default()
            .sequence = stored;
    }
    Ok(())
}

/// Auth account of the feed key as `abci_query` answered it.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct Account {
    pub number: u64,
    pub sequence: u64,
    /// Height the query was answered at.
    pub height: u64,
}

enum Field<'a> {
    Varint(u64),
    Bytes(&'a [u8]),
}

fn read_varint(bytes: &[u8], at: &mut usize) -> Result<u64, String> {
    let mut value = 0u64;
    for shift in (0..64).step_by(7) {
        let byte = *bytes.get(*at).ok_or("truncated varint")?;
        *at += 1;
        value |= u64::from(byte & 0x7f) << shift;
        if byte & 0x80 == 0 {
            return Ok(value);
        }
    }
    Err("varint over 64 bits".to_owned())
}

fn fields(bytes: &[u8]) -> Result<Vec<(u64, Field<'_>)>, String> {
    let mut out = Vec::new();
    let mut at = 0;
    while at < bytes.len() {
        let key = read_varint(bytes, &mut at)?;
        let field = match key & 7 {
            0 => Field::Varint(read_varint(bytes, &mut at)?),
            2 => {
                let len =
                    usize::try_from(read_varint(bytes, &mut at)?).map_err(|_| "length overflow")?;
                let end = at.checked_add(len).ok_or("length overflow")?;
                let value = bytes.get(at..end).ok_or("truncated field")?;
                at = end;
                Field::Bytes(value)
            }
            1 => {
                at = at
                    .checked_add(8)
                    .filter(|end| *end <= bytes.len())
                    .ok_or("truncated field")?;
                continue;
            }
            5 => {
                at = at
                    .checked_add(4)
                    .filter(|end| *end <= bytes.len())
                    .ok_or("truncated field")?;
                continue;
            }
            wire => return Err(format!("unsupported wire type {wire}")),
        };
        out.push((key >> 3, field));
    }
    Ok(out)
}

fn bytes_field<'a>(fields: &[(u64, Field<'a>)], number: u64) -> Option<&'a [u8]> {
    fields.iter().rev().find_map(|(n, field)| match field {
        Field::Bytes(value) if *n == number => Some(*value),
        _ => None,
    })
}

fn varint_field(fields: &[(u64, Field<'_>)], number: u64) -> u64 {
    fields
        .iter()
        .rev()
        .find_map(|(n, field)| match field {
            Field::Varint(value) if *n == number => Some(*value),
            _ => None,
        })
        .unwrap_or(0)
}

fn rpc_result(body: &str) -> Result<Value, String> {
    result_of(serde_json::from_str(body).map_err(|e| format!("rpc answer: {e}"))?)
}

fn result_of(mut answer: Value) -> Result<Value, String> {
    if let Some(error) = answer.get("error") {
        return Err(format!("rpc error: {error}"));
    }
    answer
        .get_mut("result")
        .map(Value::take)
        .ok_or_else(|| "rpc answer without a result".to_owned())
}

fn json_u64(value: Option<&Value>) -> Option<u64> {
    match value? {
        Value::String(text) => text.parse().ok(),
        Value::Number(number) => number.as_u64(),
        _ => None,
    }
}

/// Reads the `abci_query` answer of the account query of `feeder`.
///
/// # Errors
/// Refuses a failed query, a malformed answer, an account other than a base
/// account and an account of another address.
pub fn parse_account(body: &str, feeder: &str) -> Result<Account, String> {
    let result = rpc_result(body)?;
    let response = result.get("response").ok_or("abci_query: no response")?;
    let code = json_u64(response.get("code")).unwrap_or(0);
    if code != 0 {
        return Err(format!(
            "abci_query: code {code} {}: {}",
            response
                .get("codespace")
                .and_then(Value::as_str)
                .unwrap_or(""),
            response.get("log").and_then(Value::as_str).unwrap_or("")
        ));
    }
    let height = json_u64(response.get("height")).ok_or("abci_query: malformed height")?;
    let value = STANDARD
        .decode(
            response
                .get("value")
                .and_then(Value::as_str)
                .ok_or("abci_query: no value")?,
        )
        .map_err(|e| format!("abci_query: value: {e}"))?;
    let response_fields = fields(&value)?;
    let any = fields(bytes_field(&response_fields, 1).ok_or("abci_query: no account")?)?;
    let type_url = bytes_field(&any, 1).unwrap_or_default();
    if type_url != BASE_ACCOUNT_TYPE_URL.as_bytes() {
        return Err(format!(
            "feed account is a {}, not a base account",
            String::from_utf8_lossy(type_url)
        ));
    }
    let account = fields(bytes_field(&any, 2).unwrap_or_default())?;
    if bytes_field(&account, 1) != Some(feeder.as_bytes()) {
        return Err(format!("abci_query answered another account than {feeder}"));
    }
    Ok(Account {
        number: varint_field(&account, 3),
        sequence: varint_field(&account, 4),
        height,
    })
}

/// What a `broadcast_tx_sync` answered, or why no answer came.
#[derive(Clone, Debug, Eq, PartialEq)]
pub enum Outcome {
    Accepted { hash: String },
    SequenceMismatch(String),
    RateLimited,
    UnregisteredFeeder,
    NotValidator,
    Refused(String),
    Transport(String),
}

impl Outcome {
    /// Metric label of the outcome.
    #[must_use]
    pub fn label(&self) -> &'static str {
        match self {
            Self::Accepted { .. } => "accepted",
            Self::SequenceMismatch(_) => "sequence_mismatch",
            Self::RateLimited => "rate_limited",
            Self::UnregisteredFeeder => "unregistered_feeder",
            Self::NotValidator => "not_validator",
            Self::Refused(_) => "refused",
            Self::Transport(_) => "transport",
        }
    }
}

/// Classifies a `broadcast_tx_sync` response body.
#[must_use]
pub fn parse_broadcast(body: &str) -> Outcome {
    let answer = match serde_json::from_str::<Value>(body) {
        Ok(answer) => answer,
        Err(error) => return Outcome::Transport(format!("broadcast_tx_sync: {error}")),
    };
    let result = match result_of(answer) {
        Ok(result) => result,
        Err(error) => return Outcome::Refused(error),
    };
    let Some(code) = json_u64(result.get("code")) else {
        return Outcome::Refused("broadcast_tx_sync: answer without a code".to_owned());
    };
    let log = result.get("log").and_then(Value::as_str).unwrap_or("");
    let codespace = result
        .get("codespace")
        .and_then(Value::as_str)
        .unwrap_or("");
    match (code, codespace) {
        (0, _) => Outcome::Accepted {
            hash: result
                .get("hash")
                .and_then(Value::as_str)
                .unwrap_or("")
                .to_ascii_lowercase(),
        },
        (CODE_NOT_VALIDATOR, PRICEFEED_CODESPACE) => Outcome::NotValidator,
        (CODE_UNREGISTERED_FEEDER, PRICEFEED_CODESPACE) => Outcome::UnregisteredFeeder,
        (CODE_RATE_LIMITED, PRICEFEED_CODESPACE) => Outcome::RateLimited,
        _ if log.contains(SEQUENCE_MISMATCH) => Outcome::SequenceMismatch(log.to_owned()),
        _ => Outcome::Refused(format!("code {code} {codespace}: {log}")),
    }
}

/// One broadcast attempt: wait `delay_ms`, re-read the account first when `reread`.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct Attempt {
    pub delay_ms: u64,
    pub reread: bool,
}

/// Retry rule of one round: a sequence mismatch re-reads the account and
/// retries once, a transport failure retries after a doubling backoff up to
/// `max_attempts` attempts, every other outcome ends the round.
#[derive(Clone, Debug)]
pub struct RetryPlan {
    backoff_ms: u64,
    max_attempts: u32,
    attempts: u32,
    transport_failures: u32,
    reread: bool,
}

impl RetryPlan {
    #[must_use]
    pub fn new(backoff_ms: u64, max_attempts: u32) -> Self {
        Self {
            backoff_ms,
            max_attempts,
            attempts: 0,
            transport_failures: 0,
            reread: false,
        }
    }

    /// The next attempt after `last` (`None` before the first), or `None`
    /// when the round is over.
    pub fn next(&mut self, last: Option<&Outcome>) -> Option<Attempt> {
        let attempt = match last {
            None if self.attempts == 0 => Attempt {
                delay_ms: 0,
                reread: false,
            },
            Some(Outcome::Transport(_)) if self.attempts < self.max_attempts => {
                let doubling = 1u64
                    .checked_shl(self.transport_failures)
                    .unwrap_or(u64::MAX);
                self.transport_failures += 1;
                Attempt {
                    delay_ms: self.backoff_ms.saturating_mul(doubling),
                    reread: false,
                }
            }
            Some(Outcome::SequenceMismatch(_)) if !self.reread => {
                self.reread = true;
                Attempt {
                    delay_ms: 0,
                    reread: true,
                }
            }
            _ => return None,
        };
        self.attempts += 1;
        Some(attempt)
    }
}

/// The running sidecar: feed key, addresses and the journal of sequences.
pub struct Sidecar {
    secret: Zeroizing<[u8; 32]>,
    feed_account: [u8; 20],
    feeder: String,
    journal: Journal,
}

/// Checks the registration on 0x1020 and syncs every market's next sequence
/// to `max(journal, getObservation sequence + 1)`.
///
/// # Errors
/// Refuses a feed account not registered to the configured validator, failed
/// calls, malformed answers and journal failures.
pub fn start(
    config: &Config,
    rpc: &Rpc,
    secret: Zeroizing<[u8; 32]>,
    state_dir: &Path,
) -> Result<Sidecar, String> {
    let validator = config.validator()?;
    let feed_account = config.feed_account()?;
    check_registration(
        &rpc.call_pricefeed(&validator_by_feed_account_call(&feed_account))?,
        &validator,
        &feed_account,
    )?;
    let mut journal = Journal::load(state_dir, 0)?;
    for market in &config.markets {
        let answer = rpc.call_pricefeed(&observation_call(market.market_id, &validator))?;
        sync_sequence(&mut journal, &market.symbol, &answer)
            .map_err(|e| format!("{}: {e}", market.symbol))?;
    }
    journal.store(state_dir)?;
    Ok(Sidecar::new(secret, feed_account, journal))
}

impl Sidecar {
    #[must_use]
    pub fn new(secret: Zeroizing<[u8; 32]>, feed_account: [u8; 20], journal: Journal) -> Self {
        Self {
            secret,
            feed_account,
            feeder: cosmos_tx::feed_account(&feed_account),
            journal,
        }
    }

    /// The observations of the markets that produced a ticker, each with its
    /// next sequence, spent in the journal; `None` when no market did.
    /// Failures and observation times are counted in `metrics`.
    pub fn collect(
        &mut self,
        results: &[(&MarketConfig, Result<Ticker, FetchError>)],
        metrics: &mut Metrics,
    ) -> Option<Vec<Observation>> {
        let mut observations = Vec::with_capacity(results.len());
        for (market, result) in results {
            match result {
                Ok(ticker) => {
                    metrics.observed(&market.symbol, ticker.observed_at_ms);
                    let sequence = self.journal.next_observation_sequence(&market.symbol);
                    let record = self
                        .journal
                        .markets
                        .entry(market.symbol.clone())
                        .or_default();
                    record.sequence = sequence;
                    record.price.clone_from(&ticker.index_price);
                    record.observed_at = ticker.observed_at_ms;
                    record.activity_id.clear();
                    observations.push(Observation {
                        market_id: market.market_id,
                        sequence,
                        index_price: ticker.index_price.clone(),
                        mark_price: ticker.mark_price.clone(),
                        source_timestamp_ms: ticker.observed_at_ms,
                    });
                }
                Err(error) => {
                    metrics.fetch_error(&market.symbol, error.kind());
                    eprintln!("price-sidecar: {} {error:?}", market.symbol);
                }
            }
        }
        (!observations.is_empty()).then_some(observations)
    }

    /// The submission of `observations` over `account`: the account sequence
    /// is the larger of the chain's and the journal's.
    #[must_use]
    pub fn next_submission(
        &self,
        config: &Config,
        account: Account,
        observations: Vec<Observation>,
    ) -> Submission {
        Submission {
            chain_id: config.chain_id.clone(),
            account_number: account.number,
            sequence: account.sequence.max(self.journal.next_account_sequence),
            gas_limit: config.gas_limit,
            feed_account: self.feed_account,
            observations,
        }
    }

    /// Advances the journal past an accepted `submission` and records its
    /// transaction hash on each market it carried.
    pub fn record_broadcast(
        &mut self,
        config: &Config,
        submission: &Submission,
        outcome: &Outcome,
    ) {
        if let Outcome::Accepted { hash } = outcome {
            self.journal.next_account_sequence = submission.sequence.saturating_add(1);
            for observation in &submission.observations {
                let record = config
                    .markets
                    .iter()
                    .find(|market| market.market_id == observation.market_id)
                    .and_then(|market| self.journal.markets.get_mut(&market.symbol));
                if let Some(record) = record {
                    record.activity_id.clone_from(hash);
                }
            }
        }
    }

    fn attempt(
        &mut self,
        config: &Config,
        rpc: &Rpc,
        observations: &[Observation],
        account: &mut Option<Account>,
        reread: bool,
    ) -> Outcome {
        let read = match *account {
            Some(read) if !reread => read,
            _ => match rpc.query_account(&self.feeder) {
                Err(error) => return Outcome::Transport(error),
                Ok(body) => match parse_account(&body, &self.feeder) {
                    Ok(read) => read,
                    Err(error) => return Outcome::Refused(error),
                },
            },
        };
        if reread {
            self.journal.next_account_sequence = read.sequence;
        }
        *account = Some(read);
        let submission = self.next_submission(config, read, observations.to_vec());
        let outcome = match encode_tx(&self.secret, &submission) {
            Ok(tx) => rpc
                .broadcast(&tx)
                .map_or_else(Outcome::Transport, |body| parse_broadcast(&body)),
            Err(error) => Outcome::Refused(error),
        };
        self.record_broadcast(config, &submission, &outcome);
        outcome
    }

    fn submit(
        &mut self,
        config: &Config,
        rpc: &Rpc,
        observations: &[Observation],
        metrics: &Mutex<Metrics>,
        state_dir: &Path,
    ) -> Result<(), String> {
        let mut plan = RetryPlan::new(config.retry_backoff_ms, config.retry_max_attempts);
        let mut account = None;
        let mut last = None;
        while let Some(attempt) = plan.next(last.as_ref()) {
            thread::sleep(Duration::from_millis(attempt.delay_ms));
            let outcome = self.attempt(config, rpc, observations, &mut account, attempt.reread);
            let target_height = account.map_or(0, |read| read.height.saturating_add(1));
            lock(metrics).submission(&outcome, target_height, now_ms());
            match &outcome {
                Outcome::Accepted { hash } => {
                    self.journal.store(state_dir)?;
                    eprintln!(
                        "price-sidecar: {hash} {} observations for height {target_height}",
                        observations.len()
                    );
                }
                other => eprintln!("price-sidecar: submission {}: {other:?}", other.label()),
            }
            last = Some(outcome);
        }
        Ok(())
    }
}

/// One round: fetch every market in parallel, submit one transaction with
/// the markets that produced a ticker, none when every market failed.
///
/// # Errors
/// Refuses only journal write failures; fetch and broadcast failures are
/// counted and logged.
pub fn run_round(
    config: &Config,
    agent: &ureq::Agent,
    api_key: &str,
    rpc: &Rpc,
    sidecar: &mut Sidecar,
    metrics: &Mutex<Metrics>,
    state_dir: &Path,
) -> Result<(), String> {
    let results: Vec<(&MarketConfig, Result<Ticker, FetchError>)> = thread::scope(|scope| {
        let handles: Vec<_> = config
            .markets
            .iter()
            .map(|market| {
                (
                    market,
                    scope.spawn(move || {
                        config
                            .chainflow
                            .fetch_ticker(agent, &market.symbol, api_key, now_ms())
                    }),
                )
            })
            .collect();
        handles
            .into_iter()
            .map(|(market, handle)| {
                let result = handle
                    .join()
                    .unwrap_or_else(|_| Err(FetchError::Transport("fetch panicked".to_owned())));
                (market, result)
            })
            .collect()
    });
    let observations = {
        let mut metrics = lock(metrics);
        metrics.round();
        sidecar.collect(&results, &mut metrics)
    };
    let Some(observations) = observations else {
        return Ok(());
    };
    sidecar.journal.store(state_dir)?;
    sidecar.submit(config, rpc, &observations, metrics, state_dir)
}

#[cfg(test)]
mod tests {
    use super::*;
    use layerx_oracle_feeder::chainflow::{status_error, ChainflowConfig};
    use layerx_oracle_feeder::MarketRecord;

    const TICKER_BTC: &str = include_str!("../../../tests/fixtures/chainflow_ticker_btc.json");
    const FEEDER: &str = "pax10xcqpzrky6eff2g52qdye53xkk9jxkvra08ynt";
    const FEED_ACCOUNT: [u8; 20] = [
        0x79, 0xb0, 0x00, 0x88, 0x76, 0x26, 0xb2, 0x94, 0xa9, 0x14, 0x50, 0x1a, 0x4c, 0xd2, 0x26,
        0xb5, 0x8b, 0x23, 0x59, 0x83,
    ];
    const VALIDATOR: [u8; 20] = [0x22; 20];
    const NOW: u64 = 1_700_000_000_500;
    const ACCOUNT_BODY: &str = r#"{"jsonrpc":"2.0","id":1,"result":{"response":{"code":0,"log":"","info":"","index":"0","key":null,"value":"ClQKIC9jb3Ntb3MuYXV0aC52MWJldGExLkJhc2VBY2NvdW50EjAKKnBheDEweGNxcHpya3k2ZWZmMmc1MnFkeWU1M3hrazlqeGt2cmEwOHludBgJIAM=","proofOps":null,"height":"811","codespace":""}}}"#;
    const ACCEPTED_BODY: &str = r#"{"jsonrpc":"2.0","id":1,"result":{"code":0,"data":"","log":"[]","codespace":"","hash":"9F86D081884C7D659A2FEAA0C55AD015A3BF4F1B2B0B822CD15D6C15B0F00A08"}}"#;
    const MISMATCH_BODY: &str = r#"{"jsonrpc":"2.0","id":1,"result":{"code":32,"data":"","log":"account sequence mismatch, expected 4, got 3: incorrect account sequence","codespace":"sdk","hash":"9F86D081884C7D659A2FEAA0C55AD015A3BF4F1B2B0B822CD15D6C15B0F00A08"}}"#;
    const NOT_VALIDATOR_BODY: &str = r#"{"jsonrpc":"2.0","id":1,"result":{"code":5,"data":"","log":"not a bonded validator","codespace":"pricefeed","hash":"9F86D081884C7D659A2FEAA0C55AD015A3BF4F1B2B0B822CD15D6C15B0F00A08"}}"#;
    const RATE_LIMITED_BODY: &str = r#"{"jsonrpc":"2.0","id":1,"result":{"code":11,"data":"","log":"the validator has already submitted prices at this height","codespace":"pricefeed","hash":"9F86D081884C7D659A2FEAA0C55AD015A3BF4F1B2B0B822CD15D6C15B0F00A08"}}"#;
    const SYMBOLS: [&str; 18] = [
        "BTC", "ETH", "SOL", "AVAX", "LINK", "TSLA", "NVDA", "NAS100", "XAU", "SPX500", "GOOGL",
        "PAX", "SID", "HYPE", "XRP", "ASTER", "TRUMP", "BNB",
    ];

    fn config() -> Config {
        Config {
            chain_id: "test-1".to_owned(),
            validator: format!("0x{}", hex_encode(&VALIDATOR)),
            feed_account: hex_encode(&FEED_ACCOUNT),
            gas_limit: 200_000,
            cadence_ms: 1000,
            retry_backoff_ms: 200,
            retry_max_attempts: 3,
            chainflow: ChainflowConfig {
                base_url: "http://127.0.0.1:1".to_owned(),
                api_key_env: "PRICE_FEED_REST_API_KEY".to_owned(),
                timeout_ms: 800,
                max_quote_age_ms: 10_000,
            },
            markets: SYMBOLS
                .iter()
                .zip(0u64..)
                .map(|(symbol, market_id)| MarketConfig {
                    market_id,
                    symbol: (*symbol).to_owned(),
                })
                .collect(),
        }
    }

    fn sidecar(journal: Journal) -> Sidecar {
        Sidecar::new(Zeroizing::new([1; 32]), FEED_ACCOUNT, journal)
    }

    fn ticker(config: &Config, symbol: &str) -> Result<Ticker, FetchError> {
        let body = TICKER_BTC.replace("PERP_BTC_USDC", &format!("PERP_{symbol}_USDC"));
        config.chainflow.parse_ticker(&body, symbol, NOW)
    }

    fn word(value: u64) -> String {
        format!("{value:064x}")
    }

    #[test]
    fn submission_advances_sequence() -> Result<(), String> {
        let config = config();
        let mut sidecar = sidecar(Journal::default());
        let account = parse_account(ACCOUNT_BODY, FEEDER)?;
        assert_eq!(
            account,
            Account {
                number: 9,
                sequence: 3,
                height: 811
            }
        );
        let observations = sidecar
            .collect(
                &[(&config.markets[0], ticker(&config, "BTC"))],
                &mut Metrics::default(),
            )
            .ok_or("no observation")?;
        let submission = sidecar.next_submission(&config, account, observations.clone());
        assert_eq!(submission.sequence, 3);
        assert_eq!(submission.account_number, 9);
        assert_eq!(submission.chain_id, "test-1");
        assert_eq!(submission.gas_limit, 200_000);
        assert!(!encode_tx(&sidecar.secret, &submission)?.is_empty());
        let outcome = parse_broadcast(ACCEPTED_BODY);
        assert_eq!(
            outcome,
            Outcome::Accepted {
                hash: "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08".to_owned()
            }
        );
        sidecar.record_broadcast(&config, &submission, &outcome);
        assert_eq!(
            sidecar
                .next_submission(&config, account, observations)
                .sequence,
            4
        );
        assert_eq!(
            sidecar.journal.markets.get("BTC"),
            Some(&MarketRecord {
                sequence: 1,
                price: "65000500000000000000000".to_owned(),
                observed_at: 1_700_000_000_000,
                activity_id: "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"
                    .to_owned(),
            })
        );
        assert!(parse_account(ACCOUNT_BODY, "pax1other").is_err());
        Ok(())
    }

    #[test]
    fn retry_policy() {
        let mut plan = RetryPlan::new(200, 3);
        let mut attempts = Vec::new();
        let mut feed = [
            Outcome::SequenceMismatch("account sequence mismatch, expected 4, got 3".to_owned()),
            Outcome::Accepted {
                hash: String::new(),
            },
        ]
        .into_iter();
        let mut last = None;
        while let Some(attempt) = plan.next(last.as_ref()) {
            attempts.push(attempt);
            last = feed.next();
        }
        assert_eq!(
            attempts,
            vec![
                Attempt {
                    delay_ms: 0,
                    reread: false
                },
                Attempt {
                    delay_ms: 0,
                    reread: true
                }
            ]
        );

        let mut plan = RetryPlan::new(200, 3);
        let mut attempts = Vec::new();
        let failure = Outcome::Transport("connection refused".to_owned());
        let mut last = None;
        while let Some(attempt) = plan.next(last.as_ref()) {
            attempts.push(attempt);
            last = Some(failure.clone());
        }
        assert_eq!(
            attempts.iter().map(|a| a.delay_ms).collect::<Vec<_>>(),
            vec![0, 200, 400]
        );
        assert!(attempts.iter().all(|a| !a.reread));

        let mut plan = RetryPlan::new(200, 3);
        assert!(plan.next(None).is_some());
        assert_eq!(
            plan.next(Some(&parse_broadcast(MISMATCH_BODY)))
                .map(|a| a.reread),
            Some(true)
        );
        assert_eq!(plan.next(Some(&parse_broadcast(MISMATCH_BODY))), None);

        for body in [RATE_LIMITED_BODY, NOT_VALIDATOR_BODY, ACCEPTED_BODY] {
            let mut plan = RetryPlan::new(200, 3);
            assert!(plan.next(None).is_some());
            assert_eq!(plan.next(Some(&parse_broadcast(body))), None);
        }
        assert_eq!(parse_broadcast(RATE_LIMITED_BODY), Outcome::RateLimited);
        assert_eq!(
            parse_broadcast(
                r#"{"jsonrpc":"2.0","id":1,"result":{"code":10,"log":"feeder is not a registered feed account","codespace":"pricefeed","hash":""}}"#
            ),
            Outcome::UnregisteredFeeder
        );
        assert_eq!(
            parse_broadcast(MISMATCH_BODY),
            Outcome::SequenceMismatch(
                "account sequence mismatch, expected 4, got 3: incorrect account sequence"
                    .to_owned()
            )
        );
        assert_eq!(
            parse_broadcast(
                r#"{"jsonrpc":"2.0","id":1,"error":{"code":-32603,"message":"Internal error","data":"tx already exists in cache"}}"#
            )
            .label(),
            "refused"
        );
        assert_eq!(
            parse_broadcast("<html>bad gateway</html>").label(),
            "transport"
        );
    }

    #[test]
    fn start_checks_registration_and_sequence() -> Result<(), String> {
        let config = config();
        assert_eq!(
            hex_encode(&validator_by_feed_account_call(&FEED_ACCOUNT)),
            format!("007eb855{}{}", "00".repeat(12), hex_encode(&FEED_ACCOUNT))
        );
        assert_eq!(
            hex_encode(&observation_call(0, &VALIDATOR)),
            format!("48f83666{}{}{}", word(0), "00".repeat(12), "22".repeat(20))
        );
        let mut journal = Journal::default();
        journal.markets.insert(
            "BTC".to_owned(),
            MarketRecord {
                sequence: 4,
                ..MarketRecord::default()
            },
        );
        assert_eq!(journal.next_observation_sequence("BTC"), 5);
        let answer = format!("0x{}{}{}{}{}", word(9), word(1), word(1), word(1), word(1));
        sync_sequence(&mut journal, "BTC", &answer)?;
        sync_sequence(&mut journal, "ETH", &format!("0x{}", "0".repeat(320)))?;
        assert!(!journal.markets.contains_key("ETH"));
        let mut sidecar = sidecar(journal);
        let observations = sidecar
            .collect(
                &[
                    (&config.markets[0], ticker(&config, "BTC")),
                    (&config.markets[1], ticker(&config, "ETH")),
                ],
                &mut Metrics::default(),
            )
            .ok_or("no observation")?;
        assert_eq!(
            observations
                .iter()
                .map(|o| (o.market_id, o.sequence))
                .collect::<Vec<_>>(),
            vec![(0, 10), (1, 1)]
        );
        assert!(sync_sequence(&mut Journal::default(), "BTC", "0x").is_err());

        let registered = format!("0x{}{}", "00".repeat(12), "22".repeat(20));
        assert_eq!(
            check_registration(&registered, &VALIDATOR, &FEED_ACCOUNT),
            Ok(())
        );
        let zero = format!("0x{}", "00".repeat(32));
        assert_eq!(
            check_registration(&zero, &VALIDATOR, &FEED_ACCOUNT),
            Err(format!(
                "feed account 0x{} is not registered to validator 0x{}: getValidatorByFeedAccount answered 0x{}",
                hex_encode(&FEED_ACCOUNT),
                "22".repeat(20),
                "00".repeat(20)
            ))
        );
        Ok(())
    }

    #[test]
    fn round_skips_failed_markets() -> Result<(), String> {
        let config = config();
        let mut sidecar = sidecar(Journal::default());
        let results: Vec<_> = config
            .markets
            .iter()
            .map(|market| {
                let status = if market.symbol == "TSLA" || market.symbol == "XAU" {
                    503
                } else {
                    200
                };
                (
                    market,
                    status_error(status).and_then(|()| ticker(&config, &market.symbol)),
                )
            })
            .collect();
        let mut metrics = Metrics::default();
        let observations = sidecar
            .collect(&results, &mut metrics)
            .ok_or("no observation")?;
        assert_eq!(observations.len(), 16);
        assert!(observations
            .iter()
            .all(|o| o.market_id != 5 && o.market_id != 8));
        let account = parse_account(ACCOUNT_BODY, FEEDER)?;
        let submission = sidecar.next_submission(&config, account, observations);
        assert_eq!(submission.observations.len(), 16);
        assert!(!encode_tx(&sidecar.secret, &submission)?.is_empty());
        let text = metrics.metrics_text(NOW);
        assert!(
            text.contains("price_sidecar_fetch_errors_total{market=\"TSLA\",kind=\"stale\"} 1\n")
        );
        assert!(
            text.contains("price_sidecar_fetch_errors_total{market=\"XAU\",kind=\"stale\"} 1\n")
        );
        assert_eq!(text.matches("price_sidecar_fetch_errors_total{").count(), 2);
        assert!(text.contains("price_sidecar_market_age_ms{market=\"BTC\"} 500\n"));
        Ok(())
    }

    #[test]
    fn round_without_tickers_sends_nothing() {
        let config = config();
        let mut sidecar = sidecar(Journal::default());
        let results: Vec<_> = config
            .markets
            .iter()
            .map(|market| {
                (
                    market,
                    status_error(429).and_then(|()| ticker(&config, &market.symbol)),
                )
            })
            .collect();
        let mut metrics = Metrics::default();
        assert_eq!(sidecar.collect(&results, &mut metrics), None);
        assert!(sidecar.journal.markets.is_empty());
        let text = metrics.metrics_text(NOW);
        let rate_limited: u64 = text
            .lines()
            .filter(|line| line.contains("kind=\"rate_limited\""))
            .filter_map(|line| line.rsplit(' ').next()?.parse::<u64>().ok())
            .sum();
        assert_eq!(rate_limited, 18);
    }

    #[test]
    fn refused_validator_turns_not_ready() -> Result<(), String> {
        let config = config();
        let mut metrics = Metrics::default();
        metrics.submission(&parse_broadcast(ACCEPTED_BODY), 812, 1_700_000_000_000);
        assert!(metrics.is_ready(1_700_000_000_100, config.cadence_ms));
        let outcome = parse_broadcast(NOT_VALIDATOR_BODY);
        assert_eq!(outcome, Outcome::NotValidator);
        let mut plan = RetryPlan::new(200, 3);
        assert!(plan.next(None).is_some());
        assert_eq!(plan.next(Some(&outcome)), None);
        metrics.submission(&outcome, 813, 1_700_000_000_200);
        assert!(!metrics.is_ready(1_700_000_000_300, config.cadence_ms));
        assert!(crate::health::respond(
            b"GET /readyz HTTP/1.1\r\n\r\n",
            &metrics,
            1_700_000_000_300,
            config.cadence_ms
        )
        .starts_with("HTTP/1.1 503 Service Unavailable\r\n"));

        let mut sidecar = sidecar(Journal::default());
        let results: Vec<_> = config
            .markets
            .iter()
            .map(|market| (market, ticker(&config, &market.symbol)))
            .collect();
        let observations = sidecar
            .collect(&results, &mut metrics)
            .ok_or("no observation")?;
        assert_eq!(observations.len(), 18);
        Ok(())
    }

    type Blank = fn(&mut Config);

    #[test]
    fn config_names_each_empty_field() -> Result<(), String> {
        let config = config();
        assert_eq!(config.validate(), Ok(()));
        let blanks: [(&str, Blank); 5] = [
            ("chain_id", |c| c.chain_id.clear()),
            ("validator", |c| c.validator.clear()),
            ("feed_account", |c| c.feed_account.clear()),
            ("chainflow.base_url", |c| c.chainflow.base_url.clear()),
            ("chainflow.api_key_env", |c| c.chainflow.api_key_env.clear()),
        ];
        for (field, blank) in blanks {
            let mut config = config.clone();
            blank(&mut config);
            assert_eq!(
                config.validate(),
                Err(format!("config field {field} is empty"))
            );
        }
        let mut zero = config.clone();
        zero.gas_limit = 0;
        assert_eq!(
            zero.validate(),
            Err("config field gas_limit is zero".to_owned())
        );
        let mut zero_address = config.clone();
        zero_address.validator = "00".repeat(20);
        assert_eq!(
            zero_address.validate(),
            Err("config field validator must be a non-zero 20-byte hex address".to_owned())
        );
        let mut repeated = config.clone();
        repeated.markets[1].market_id = 0;
        assert_eq!(
            repeated.validate(),
            Err("config field markets[1] repeats a market id or symbol".to_owned())
        );
        let parsed: Config = serde_json::from_str(
            r#"{"chain_id":"test-1","validator":"2222222222222222222222222222222222222222","feed_account":"79b000887626b294a914501a4cd226b58b235983","gas_limit":200000,"retry_backoff_ms":200,"retry_max_attempts":3,"chainflow":{"base_url":"http://127.0.0.1:1","api_key_env":"PRICE_FEED_REST_API_KEY","timeout_ms":800,"max_quote_age_ms":10000},"markets":[{"market_id":0,"symbol":"BTC"}]}"#,
        )
        .map_err(|e| e.to_string())?;
        assert_eq!(parsed.cadence_ms, 1000);
        assert_eq!(parsed.feed_account()?, FEED_ACCOUNT);
        Ok(())
    }
}
