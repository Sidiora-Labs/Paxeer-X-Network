use std::convert::Infallible;
use std::io::{ErrorKind, Read as _, Write};
use std::net::{Shutdown, TcpListener, TcpStream};
use std::time::{Duration, Instant};

use serde_json::{json, Map, Value};

use crate::config::ServiceConfig;
use crate::journal::{Admission, Completion, JournalError, Key};
use crate::policy::PolicyRefusal;
use crate::price::{PriceError, PriceSource};
use crate::quote::{keccak, word, Address, Word};
use crate::rpc::{bytes, hex, JsonRpc, RpcFault};
use crate::signer::QuoteSigner;
use crate::station::{GasStation, Progress, QuoteOutcome, Recovery, StationError, SubmitRequest};
use crate::tx::{self, Authorization, Call, Fees, TxError};
use crate::{QuoteError, QuoteRequest};

struct StrictValue(Value);
impl<'de> serde::Deserialize<'de> for StrictValue {
    fn deserialize<D: serde::Deserializer<'de>>(deserializer: D) -> Result<Self, D::Error> {
        struct Visitor;
        impl<'de> serde::de::Visitor<'de> for Visitor {
            type Value = StrictValue;
            fn expecting(&self, formatter: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
                formatter.write_str("JSON without duplicate object keys")
            }
            fn visit_map<M: serde::de::MapAccess<'de>>(
                self,
                mut source: M,
            ) -> Result<Self::Value, M::Error> {
                let mut fields = Map::new();
                while let Some((key, value)) = source.next_entry::<String, StrictValue>()? {
                    if fields.insert(key, value.0).is_some() {
                        return Err(serde::de::Error::custom("duplicate object key"));
                    }
                }
                Ok(StrictValue(Value::Object(fields)))
            }
            fn visit_seq<M: serde::de::SeqAccess<'de>>(
                self,
                mut source: M,
            ) -> Result<Self::Value, M::Error> {
                let mut values = Vec::new();
                while let Some(value) = source.next_element::<StrictValue>()? {
                    values.push(value.0);
                }
                Ok(StrictValue(Value::Array(values)))
            }
            fn visit_bool<E: serde::de::Error>(self, value: bool) -> Result<Self::Value, E> {
                Ok(StrictValue(json!(value)))
            }
            fn visit_i64<E: serde::de::Error>(self, value: i64) -> Result<Self::Value, E> {
                Ok(StrictValue(json!(value)))
            }
            fn visit_u64<E: serde::de::Error>(self, value: u64) -> Result<Self::Value, E> {
                Ok(StrictValue(json!(value)))
            }
            fn visit_f64<E: serde::de::Error>(self, value: f64) -> Result<Self::Value, E> {
                Ok(StrictValue(json!(value)))
            }
            fn visit_str<E: serde::de::Error>(self, value: &str) -> Result<Self::Value, E> {
                Ok(StrictValue(json!(value)))
            }
            fn visit_string<E: serde::de::Error>(self, value: String) -> Result<Self::Value, E> {
                Ok(StrictValue(json!(value)))
            }
            fn visit_unit<E: serde::de::Error>(self) -> Result<Self::Value, E> {
                Ok(StrictValue(Value::Null))
            }
        }
        deserializer.deserialize_any(Visitor)
    }
}

const HEAD_LIMIT: usize = 8_192;
const QUOTE_NONCE_ATTEMPTS: usize = 64;

/// A request the service refused, or a failure that kept it from answering.
/// Each maps to one HTTP status: the station's refusals are 4xx and an
/// unavailable price source, node, clock or journal is 5xx.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum ServiceError {
    Accept,
    Malformed,
    NotFound,
    MethodNotAllowed,
    Timeout,
    Conflict,
    LengthRequired,
    TooLarge,
    Refused,
    OriginRefused,
    ExpiredQuote,
    IncompatibleDelegation,
    ReplayStateUnavailable,
    BalanceFloor,
    SigningUnknown,
    Internal,
    Unavailable,
}
impl ServiceError {
    #[must_use]
    pub const fn status(self) -> u16 {
        match self {
            Self::Malformed => 400,
            Self::OriginRefused => 403,
            Self::NotFound => 404,
            Self::MethodNotAllowed => 405,
            Self::Timeout => 408,
            Self::Conflict => 409,
            Self::LengthRequired => 411,
            Self::TooLarge => 413,
            Self::Refused
            | Self::ExpiredQuote
            | Self::IncompatibleDelegation
            | Self::ReplayStateUnavailable => 422,
            Self::Accept | Self::Internal => 500,
            Self::Unavailable | Self::BalanceFloor | Self::SigningUnknown => 503,
        }
    }

    const fn code(self) -> &'static str {
        match self {
            Self::Malformed => "malformed",
            Self::NotFound => "not_found",
            Self::MethodNotAllowed => "method_not_allowed",
            Self::Timeout => "timeout",
            Self::Conflict => "conflict",
            Self::LengthRequired => "length_required",
            Self::TooLarge => "too_large",
            Self::Refused => "refused",
            Self::OriginRefused => "origin_refused",
            Self::ExpiredQuote => "expired_quote",
            Self::IncompatibleDelegation => "incompatible_delegation",
            Self::ReplayStateUnavailable => "replay_state_unavailable",
            Self::Accept | Self::Internal => "internal",
            Self::Unavailable => "unavailable",
            Self::BalanceFloor => "balance_floor",
            Self::SigningUnknown => "signing_outcome_unknown",
        }
    }
}
impl std::fmt::Display for ServiceError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "service refused: {}", self.code())
    }
}
impl std::error::Error for ServiceError {}

impl From<&StationError> for ServiceError {
    fn from(error: &StationError) -> Self {
        match error {
            StationError::SigningUnknown => Self::SigningUnknown,
            StationError::Quote(QuoteError::Policy(PolicyRefusal::BalanceFloor)) => {
                Self::BalanceFloor
            }
            StationError::IncompatibleDelegation => Self::IncompatibleDelegation,
            StationError::ReplayStateUnavailable => Self::ReplayStateUnavailable,
            StationError::Rpc(RpcFault::Rejected { .. })
            | StationError::Quote(
                QuoteError::InvalidRequest
                | QuoteError::AboveMaximum
                | QuoteError::Price(PriceError::Overflow | PriceError::ZeroGas)
                | QuoteError::Policy(
                    PolicyRefusal::PerQuote
                    | PolicyRefusal::PerAccount
                    | PolicyRefusal::PerInterval
                    | PolicyRefusal::InvalidAmount,
                ),
            )
            | StationError::Transaction(TxError::Invalid | TxError::Signature)
            | StationError::Invalid
            | StationError::Missing => Self::Refused,
            StationError::Conflict | StationError::Journal(JournalError::Conflict) => {
                Self::Conflict
            }
            StationError::Rpc(
                RpcFault::Unavailable
                | RpcFault::RateLimited
                | RpcFault::Divergence
                | RpcFault::Malformed,
            )
            | StationError::Price(_)
            | StationError::Quote(
                QuoteError::Price(_) | QuoteError::Policy(PolicyRefusal::ClockRegression),
            ) => Self::Unavailable,
            StationError::Rpc(RpcFault::Configuration)
            | StationError::Journal(_)
            | StationError::Quote(QuoteError::Signer(_))
            | StationError::Transaction(TxError::Signer(_)) => Self::Internal,
        }
    }
}

/// Bounds on one request: the largest body read and the total time allowed
/// to read the request head and body.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct Limits {
    pub max_body: usize,
    pub read_time: Duration,
}
impl Default for Limits {
    fn default() -> Self {
        Self {
            max_body: 65_536,
            read_time: Duration::from_secs(10),
        }
    }
}

/// The quote and sponsored submission endpoints over one gas station.
pub struct Service<S, R, P, C> {
    station: GasStation<S, R, P>,
    chain_id: u64,
    token: Address,
    decimals: u8,
    interval: u64,
    gas_limit: u64,
    max_priority_fee_per_gas: u128,
    clock: C,
    limits: Limits,
    browser_origins: Option<Vec<String>>,
}

impl<S, R, P, C> Service<S, R, P, C>
where
    S: QuoteSigner,
    R: JsonRpc,
    P: PriceSource,
    C: FnMut() -> Option<u64>,
{
    /// Serves `station` under `config`, reading the current unix time from
    /// `clock`; a clock that cannot answer makes the request unavailable.
    #[must_use]
    pub fn new(
        config: &ServiceConfig,
        station: GasStation<S, R, P>,
        clock: C,
        limits: Limits,
    ) -> Self {
        Self {
            station,
            chain_id: config.station.chain_id,
            token: config.station.token,
            decimals: config.station.decimals,
            interval: config.station.interval_seconds,
            gas_limit: config.gas_limit,
            max_priority_fee_per_gas: config.max_priority_fee_per_gas,
            clock,
            limits,
            browser_origins: std::env::var("GAS_STATION_BROWSER_ORIGINS")
                .ok()
                .and_then(|value| browser_origins(&value)),
        }
    }

    #[must_use]
    pub const fn station(&self) -> &GasStation<S, R, P> {
        &self.station
    }

    /// Answers one parsed request: a route, a method and a JSON body.
    /// # Errors
    /// Returns the refusal whose status the response carries.
    pub fn respond(&mut self, route: &str, body: &[u8]) -> Result<Value, ServiceError> {
        let body = serde_json::from_slice::<StrictValue>(body)
            .map_err(|_| ServiceError::Malformed)?
            .0;
        match route {
            "/quote" => self.quote(&body),
            "/submit" => self.submit(&body),
            "/status" => self.status(&body),
            "/retry" => self.retry(&body),
            _ => Err(ServiceError::NotFound),
        }
    }

    fn now(&mut self) -> Result<u64, ServiceError> {
        (self.clock)().ok_or(ServiceError::Unavailable)
    }

    fn fees(&self, gas_cost: u128) -> Result<Fees, ServiceError> {
        let gas_limit = u128::from(self.gas_limit);
        if gas_cost == 0 || !gas_cost.is_multiple_of(gas_limit) {
            return Err(ServiceError::Refused);
        }
        let fees = Fees {
            gas_limit: self.gas_limit,
            max_fee_per_gas: gas_cost / gas_limit,
            max_priority_fee_per_gas: self.max_priority_fee_per_gas,
        };
        match fees.gas_cost() {
            Ok(cost) if cost == gas_cost => Ok(fees),
            _ => Err(ServiceError::Refused),
        }
    }

    fn next_quote_nonce(&self) -> Result<Word, ServiceError> {
        let last = self
            .station
            .journal()
            .state()
            .items
            .keys()
            .map(|key| key.quote_nonce)
            .max()
            .unwrap_or_default();
        increment(last).ok_or(ServiceError::Unavailable)
    }

    fn quote(&mut self, body: &Value) -> Result<Value, ServiceError> {
        let request = fields(
            body,
            &[
                "account",
                "nonce",
                "calls",
                "maxTokenAmount",
                "gasCost",
                "chainId",
                "token",
                "decimals",
            ],
        )?;
        let account = address(&request["account"])?;
        let nonce = decimal_word(&request["nonce"])?;
        if self
            .station
            .batch_nonce(account)
            .map_err(|error| ServiceError::from(&error))?
            != nonce
        {
            return Err(ServiceError::Conflict);
        }
        calls(&request["calls"])?;
        let max_token_amount = amount(&request["maxTokenAmount"])?;
        let gas_cost = amount(&request["gasCost"])?;
        if decimal_u64(&request["chainId"])? != self.chain_id
            || address(&request["token"])? != self.token
            || decimals(&request["decimals"])? != self.decimals
            || max_token_amount == 0
        {
            return Err(ServiceError::Refused);
        }
        let fees = self.fees(gas_cost)?;
        self.now()?;
        let now = self
            .station
            .chain_time()
            .map_err(|error| ServiceError::from(&error))?;
        let deadline = (now - now % self.interval)
            .checked_add(self.interval - 1)
            .ok_or(ServiceError::Unavailable)?;
        let interval = now / self.interval;
        let identity = Admission::identity(account, nonce, interval);
        let request_digest =
            keccak(&serde_json::to_vec(body).map_err(|_| ServiceError::Malformed)?);
        let existing = self
            .station
            .admission(identity, request_digest, account, interval)
            .map_err(|error| ServiceError::from(&error))?;
        let mut quote_nonce = match existing {
            Some(key) => key.quote_nonce,
            None => self.next_quote_nonce()?,
        };
        let admission = Admission {
            identity,
            request_digest,
            batch_nonce: nonce,
            interval,
        };
        for _ in 0..QUOTE_NONCE_ATTEMPTS {
            let outcome = self
                .station
                .quote_admitted(
                    &QuoteRequest {
                        account,
                        max_token_amount,
                        gas_cost,
                        deadline,
                        quote_nonce,
                    },
                    fees,
                    now,
                    Some(admission),
                )
                .map_err(|error| ServiceError::from(&error))?;
            match outcome {
                QuoteOutcome::Signed(signed) => {
                    let quote = &signed.quote;
                    return Ok(json!({
                        "quote": {
                            "sponsor": hex(&quote.sponsor),
                            "token": hex(&quote.token),
                            "maxTokenAmount": decimal(&quote.max_token_amount),
                            "tokenAmount": decimal(&quote.token_amount),
                            "deadline": decimal(&quote.deadline),
                            "quoteNonce": decimal(&quote.nonce),
                            "gasCost": decimal(&quote.gas_cost),
                            "decimals": self.decimals,
                        },
                        "relayerSignature": hex(&signed.signature),
                    }));
                }
                QuoteOutcome::Completed(_) => {
                    quote_nonce = increment(quote_nonce).ok_or(ServiceError::Unavailable)?;
                }
            }
        }
        Err(ServiceError::Unavailable)
    }

    fn submit(&mut self, body: &Value) -> Result<Value, ServiceError> {
        let Submitted {
            chain_id,
            decimals,
            key,
            account,
            calls,
            token,
            maximum,
            token_amount,
            deadline,
            gas_cost,
            account_signature,
            relayer_signature,
            authorization,
            call,
        } = submitted(body)?;
        if chain_id != self.chain_id
            || decimals != self.decimals
            || authorization.chain_id != self.chain_id
        {
            return Err(ServiceError::Refused);
        }
        let saved = self
            .station
            .journal()
            .state()
            .items
            .get(&key)
            .and_then(|item| item.quote.as_ref())
            .ok_or(ServiceError::Refused)?;
        if saved.account != account
            || saved.token != token
            || saved.maximum != maximum
            || saved.amount != token_amount
            || saved.deadline != deadline
            || saved.gas_cost != gas_cost
            || saved.signature != relayer_signature
        {
            return Err(ServiceError::Refused);
        }
        let signed = saved.signed_quote().map_err(|_| ServiceError::Internal)?;
        let expected = tx::encode_sponsored(&calls, &signed, &account_signature)
            .map_err(|_| ServiceError::Refused)?;
        if call.to != account || call.value != word(0) || call.data != expected {
            return Err(ServiceError::Refused);
        }
        let local_now = self.now()?;
        if deadline < local_now
            && self
                .station
                .journal()
                .state()
                .items
                .get(&key)
                .is_some_and(|item| item.submission.is_none() && item.completion.is_none())
        {
            return Err(ServiceError::ExpiredQuote);
        }
        let now = self
            .station
            .chain_time()
            .map_err(|error| ServiceError::from(&error))?;
        if deadline < now
            && self
                .station
                .journal()
                .state()
                .items
                .get(&key)
                .is_some_and(|item| item.submission.is_none() && item.completion.is_none())
        {
            return Err(ServiceError::ExpiredQuote);
        }
        let progress = self
            .station
            .submit(
                &SubmitRequest {
                    key,
                    account,
                    calls,
                    authorizations: vec![authorization],
                    account_signature,
                },
                now,
            )
            .map_err(|error| ServiceError::from(&error))?;
        let submitted = self
            .station
            .journal()
            .state()
            .items
            .get(&key)
            .and_then(|item| item.submission.as_ref())
            .map(|submission| submission.hash);
        let hash = match progress {
            Progress::Completed(Completion::Included { hash, .. }) => hash,
            Progress::Pending => submitted.ok_or(ServiceError::Internal)?,
            Progress::Completed(Completion::Consumed) => submitted.ok_or(ServiceError::Conflict)?,
            Progress::Completed(Completion::Reverted { .. } | Completion::Cancelled { .. }) => {
                return Err(ServiceError::Refused)
            }
        };
        Ok(json!({ "transactionHash": hex(&hash) }))
    }

    /// Runs one bounded recovery pass of the station at the service clock.
    /// # Errors
    /// Returns the station's refusal; an unusable clock is unavailable.
    pub fn recover(&mut self, budget: Duration) -> Result<Recovery, StationError> {
        let now = (self.clock)().ok_or(StationError::Rpc(RpcFault::Unavailable))?;
        self.station.recover(now, budget)
    }

    fn identity(body: &Value) -> Result<(Key, Address, [u8; 65]), ServiceError> {
        let request = fields(
            body,
            &["sponsor", "quoteNonce", "account", "relayerSignature"],
        )?;
        Ok((
            Key {
                sponsor: address(&request["sponsor"])?,
                quote_nonce: decimal_word(&request["quoteNonce"])?,
            },
            address(&request["account"])?,
            signature(&request["relayerSignature"])?,
        ))
    }

    fn report(
        &self,
        key: Key,
        account: Address,
        relayer: &[u8; 65],
    ) -> Result<Value, ServiceError> {
        let status = self
            .station
            .status(key, account, relayer)
            .map_err(|error| ServiceError::from(&error))?;
        let transaction = |pair: Option<(u64, Word)>| {
            pair.map_or(Value::Null, |(nonce, hash)| {
                json!({"sponsorNonce": nonce.to_string(), "transactionHash": hex(&hash)})
            })
        };
        let (state, outcome) = match status.completion {
            None if status.replacement.is_some() => ("replacing", Value::Null),
            None if status.submission.is_some() => ("pending", Value::Null),
            None => ("quoted", Value::Null),
            Some(Completion::Consumed) => ("completed", json!({"outcome": "consumed"})),
            Some(Completion::Included {
                hash,
                block_number,
                sid_collected,
                pax_spent,
            }) => (
                "completed",
                json!({"outcome": "included", "transactionHash": hex(&hash),
                    "blockNumber": block_number.to_string(),
                    "sidCollected": decimal(&sid_collected), "paxSpent": decimal(&pax_spent)}),
            ),
            Some(Completion::Reverted { hash }) => (
                "completed",
                json!({"outcome": "reverted", "transactionHash": hex(&hash)}),
            ),
            Some(Completion::Cancelled { hash, block_number }) => (
                "completed",
                json!({"outcome": "cancelled", "transactionHash": hex(&hash),
                    "blockNumber": block_number.to_string()}),
            ),
        };
        Ok(json!({
            "state": state,
            "deadline": status.deadline.to_string(),
            "submission": transaction(status.submission),
            "replacement": transaction(status.replacement),
            "completion": outcome,
        }))
    }

    fn status(&self, body: &Value) -> Result<Value, ServiceError> {
        let (key, account, relayer) = Self::identity(body)?;
        self.report(key, account, &relayer)
    }

    fn retry(&mut self, body: &Value) -> Result<Value, ServiceError> {
        let (key, account, relayer) = Self::identity(body)?;
        let now = self.now()?;
        self.station
            .retry(key, account, &relayer, now)
            .map_err(|error| ServiceError::from(&error))?;
        self.report(key, account, &relayer)
    }

    fn answer(
        &mut self,
        stream: &mut TcpStream,
    ) -> (
        &'static str,
        Result<Value, ServiceError>,
        Option<String>,
        bool,
    ) {
        let deadline = Instant::now() + self.limits.read_time;
        let head = match read_head(stream, deadline) {
            Ok(head) => head,
            Err(error) => return ("-", Err(error), None, false),
        };
        if head.path == "/healthz" {
            return if head.method == "GET" {
                ("/healthz", Ok(json!({"status": "ok"})), None, false)
            } else {
                ("/healthz", Err(ServiceError::MethodNotAllowed), None, false)
            };
        }
        let route = match head.path.as_str() {
            "/quote" => "/quote",
            "/submit" => "/submit",
            "/status" => "/status",
            "/retry" => "/retry",
            _ => return ("-", Err(ServiceError::NotFound), None, false),
        };
        let origin = match head.origin.as_ref() {
            Some(origin)
                if self
                    .browser_origins
                    .as_ref()
                    .is_some_and(|allowed| allowed.contains(origin)) =>
            {
                Some(origin.clone())
            }
            Some(_) => return (route, Err(ServiceError::OriginRefused), None, false),
            None => None,
        };
        if head.method == "OPTIONS" {
            if origin.is_none()
                || head.request_method.as_deref() != Some("POST")
                || head.request_headers.as_deref().is_none_or(|headers| {
                    let names: Vec<_> = headers.split(',').map(str::trim).collect();
                    names.len() != 1 || !names[0].eq_ignore_ascii_case("content-type")
                })
                || head.length.is_some_and(|length| length != 0)
                || !head.rest.is_empty()
            {
                return (route, Err(ServiceError::OriginRefused), None, false);
            }
            return (route, Ok(Value::Null), origin, true);
        }
        if head.method != "POST" {
            return (route, Err(ServiceError::MethodNotAllowed), origin, false);
        }
        if head.request_method.is_some()
            || head.request_headers.is_some()
            || (origin.is_some()
                && head
                    .content_type
                    .as_deref()
                    .is_none_or(|value| !value.eq_ignore_ascii_case("application/json")))
        {
            return (route, Err(ServiceError::Malformed), origin, false);
        }
        let Some(length) = head.length else {
            return (route, Err(ServiceError::LengthRequired), origin, false);
        };
        if length > self.limits.max_body {
            return (route, Err(ServiceError::TooLarge), origin, false);
        }
        let body = match read_body(stream, head.rest, length, deadline) {
            Ok(body) => body,
            Err(error) => return (route, Err(error), origin, false),
        };
        (route, self.respond(route, &body), origin, false)
    }
}

/// Accepts connections on `listener` one at a time and answers each with the
/// service, writing one line per request to `log`: the route and the status,
/// never a body, a signature, signed bytes or key material.
/// # Errors
/// Returns only when the listener stops accepting connections.
pub fn serve<S, R, P, C>(
    listener: &TcpListener,
    service: &mut Service<S, R, P, C>,
    log: &mut impl Write,
) -> Result<Infallible, ServiceError>
where
    S: QuoteSigner,
    R: JsonRpc,
    P: PriceSource,
    C: FnMut() -> Option<u64>,
{
    loop {
        let (mut stream, _) = listener.accept().map_err(|_| ServiceError::Accept)?;
        let (route, result, origin, preflight) = service.answer(&mut stream);
        let (status, body) = match result {
            Ok(value) => (if preflight { 204 } else { 200 }, value),
            Err(error) => (error.status(), json!({ "error": error.code() })),
        };
        let delivered =
            if write_response(&mut stream, status, &body, origin.as_deref(), preflight).is_ok() {
                ""
            } else {
                " undelivered"
            };
        let _ = writeln!(log, "{route} {status}{delivered}").and_then(|()| log.flush());
    }
}

/// How often the recovery driver runs and how long one pass may take: the
/// startup pass precedes serving, a pass that leaves work pending or meets an
/// unreachable node backs off exponentially up to `max_interval`, and a pass
/// that completes work returns to `interval`.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct Schedule {
    pub interval: Duration,
    pub max_interval: Duration,
    pub budget: Duration,
}
impl Default for Schedule {
    fn default() -> Self {
        Self {
            interval: Duration::from_secs(2),
            max_interval: Duration::from_secs(60),
            budget: Duration::from_secs(20),
        }
    }
}

/// Serves requests like `serve` and, from the same thread and therefore as
/// the journal's only writer, runs a recovery pass whenever `schedule` is due
/// so every unresolved submission progresses without a client request. Each
/// pass writes one line to `log` with its counts or its refusal.
/// # Errors
/// Returns when the listener fails or a pass meets a failing journal.
pub fn drive<S, R, P, C>(
    listener: &TcpListener,
    service: &mut Service<S, R, P, C>,
    schedule: Schedule,
    log: &mut impl Write,
) -> Result<Infallible, ServiceError>
where
    S: QuoteSigner,
    R: JsonRpc,
    P: PriceSource,
    C: FnMut() -> Option<u64>,
{
    listener
        .set_nonblocking(true)
        .map_err(|_| ServiceError::Accept)?;
    let mut wait = schedule.interval;
    let mut due = Instant::now() + schedule.interval;
    loop {
        if Instant::now() >= due {
            match service.recover(schedule.budget) {
                Ok(recovery) => {
                    let _ = writeln!(
                        log,
                        "recovery completed={} pending={} unreachable={} deferred={} liability_unreachable={}",
                        recovery.completed,
                        recovery.pending,
                        recovery.unreachable,
                        recovery.deferred,
                        recovery.liability_unreachable
                    )
                    .and_then(|()| log.flush());
                    wait = if recovery.completed > 0
                        || (!recovery.liability_unreachable
                            && recovery.pending + recovery.unreachable + recovery.deferred == 0)
                    {
                        schedule.interval
                    } else {
                        wait.saturating_mul(2).min(schedule.max_interval)
                    };
                }
                Err(StationError::Journal(_)) => {
                    let _ = writeln!(log, "recovery refused: journal").and_then(|()| log.flush());
                    return Err(ServiceError::Internal);
                }
                Err(error) => {
                    let _ = writeln!(
                        log,
                        "recovery refused: {}",
                        ServiceError::from(&error).code()
                    )
                    .and_then(|()| log.flush());
                    wait = wait.saturating_mul(2).min(schedule.max_interval);
                }
            }
            due = Instant::now() + wait;
        }
        let mut stream = match listener.accept() {
            Ok((stream, _)) => stream,
            Err(error) if error.kind() == ErrorKind::WouldBlock => {
                std::thread::sleep(
                    Duration::from_millis(20).min(due.saturating_duration_since(Instant::now())),
                );
                continue;
            }
            Err(error) if error.kind() == ErrorKind::Interrupted => continue,
            Err(_) => return Err(ServiceError::Accept),
        };
        stream
            .set_nonblocking(false)
            .map_err(|_| ServiceError::Accept)?;
        let (route, result, origin, preflight) = service.answer(&mut stream);
        let (status, body) = match result {
            Ok(value) => (if preflight { 204 } else { 200 }, value),
            Err(error) => (error.status(), json!({ "error": error.code() })),
        };
        let delivered =
            if write_response(&mut stream, status, &body, origin.as_deref(), preflight).is_ok() {
                ""
            } else {
                " undelivered"
            };
        let _ = writeln!(log, "{route} {status}{delivered}").and_then(|()| log.flush());
    }
}

struct Head {
    method: String,
    path: String,
    length: Option<usize>,
    rest: Vec<u8>,
    origin: Option<String>,
    request_method: Option<String>,
    request_headers: Option<String>,
    content_type: Option<String>,
}

fn read_some(
    stream: &mut TcpStream,
    buffer: &mut [u8],
    deadline: Instant,
) -> Result<usize, ServiceError> {
    loop {
        let remaining = deadline.saturating_duration_since(Instant::now());
        if remaining.is_zero() {
            return Err(ServiceError::Timeout);
        }
        stream
            .set_read_timeout(Some(remaining))
            .map_err(|_| ServiceError::Internal)?;
        match stream.read(buffer) {
            Ok(count) => return Ok(count),
            Err(error) if error.kind() == ErrorKind::Interrupted => (),
            Err(error) if matches!(error.kind(), ErrorKind::WouldBlock | ErrorKind::TimedOut) => {
                return Err(ServiceError::Timeout)
            }
            Err(_) => return Err(ServiceError::Malformed),
        }
    }
}

fn read_head(stream: &mut TcpStream, deadline: Instant) -> Result<Head, ServiceError> {
    let mut received = Vec::new();
    let mut chunk = [0_u8; 4_096];
    let split = loop {
        if let Some(split) = received.windows(4).position(|w| w == b"\r\n\r\n") {
            break split;
        }
        if received.len() > HEAD_LIMIT {
            return Err(ServiceError::TooLarge);
        }
        let count = read_some(stream, &mut chunk, deadline)?;
        if count == 0 {
            return Err(ServiceError::Malformed);
        }
        received.extend_from_slice(&chunk[..count]);
    };
    if split > HEAD_LIMIT {
        return Err(ServiceError::TooLarge);
    }
    let head = std::str::from_utf8(&received[..split]).map_err(|_| ServiceError::Malformed)?;
    let mut lines = head.split("\r\n");
    let mut request = lines.next().unwrap_or_default().split(' ');
    let (Some(method), Some(path), Some(version), None) = (
        request.next(),
        request.next(),
        request.next(),
        request.next(),
    ) else {
        return Err(ServiceError::Malformed);
    };
    if !matches!(version, "HTTP/1.1" | "HTTP/1.0") {
        return Err(ServiceError::Malformed);
    }
    let mut length = None;
    let mut origin = None;
    let mut request_method = None;
    let mut request_headers = None;
    let mut content_type = None;
    for line in lines {
        let (name, value) = line.split_once(':').ok_or(ServiceError::Malformed)?;
        if name.is_empty()
            || !name.bytes().all(|b| b.is_ascii_alphanumeric() || b == b'-')
            || value.bytes().any(|b| b < 32 && b != b'\t' || b == 127)
        {
            return Err(ServiceError::Malformed);
        }
        let slot = if name.eq_ignore_ascii_case("origin") {
            Some(&mut origin)
        } else if name.eq_ignore_ascii_case("access-control-request-method") {
            Some(&mut request_method)
        } else if name.eq_ignore_ascii_case("access-control-request-headers") {
            Some(&mut request_headers)
        } else if name.eq_ignore_ascii_case("content-type") {
            Some(&mut content_type)
        } else {
            None
        };
        if let Some(slot) = slot {
            if slot.replace(value.trim().to_owned()).is_some() {
                return Err(ServiceError::Malformed);
            }
        }
        if name.eq_ignore_ascii_case("transfer-encoding") {
            return Err(ServiceError::LengthRequired);
        }
        if name.eq_ignore_ascii_case("content-length") {
            let value = value.trim();
            if length.is_some() || value.is_empty() || !value.bytes().all(|b| b.is_ascii_digit()) {
                return Err(ServiceError::Malformed);
            }
            length = Some(value.parse::<usize>().map_err(|_| ServiceError::TooLarge)?);
        }
    }
    Ok(Head {
        method: method.to_owned(),
        path: path.to_owned(),
        length,
        rest: received[split + 4..].to_vec(),
        origin,
        request_method,
        request_headers,
        content_type,
    })
}

fn read_body(
    stream: &mut TcpStream,
    mut body: Vec<u8>,
    length: usize,
    deadline: Instant,
) -> Result<Vec<u8>, ServiceError> {
    if body.len() > length {
        return Err(ServiceError::Malformed);
    }
    let mut chunk = [0_u8; 4_096];
    while body.len() < length {
        let wanted = chunk.len().min(length - body.len());
        let count = read_some(stream, &mut chunk[..wanted], deadline)?;
        if count == 0 {
            return Err(ServiceError::Malformed);
        }
        body.extend_from_slice(&chunk[..count]);
    }
    Ok(body)
}

struct Submitted {
    chain_id: u64,
    decimals: u8,
    key: Key,
    account: Address,
    calls: Vec<Call>,
    token: Address,
    maximum: Word,
    token_amount: Word,
    deadline: u64,
    gas_cost: Word,
    account_signature: [u8; 65],
    relayer_signature: [u8; 65],
    authorization: Authorization,
    call: Call,
}

/// Reads the body `sendSponsoredBatch` in the web application sends.
fn submitted(body: &Value) -> Result<Submitted, ServiceError> {
    let request = fields(
        body,
        &[
            "call",
            "authorization",
            "batch",
            "accountSignature",
            "relayerSignature",
        ],
    )?;
    let batch = fields(
        &request["batch"],
        &["chainId", "account", "nonce", "calls", "quote"],
    )?;
    let quote = fields(
        &batch["quote"],
        &[
            "sponsor",
            "token",
            "maxTokenAmount",
            "tokenAmount",
            "deadline",
            "quoteNonce",
            "gasCost",
            "decimals",
        ],
    )?;
    let call = fields(&request["call"], &["to", "value", "data"])?;
    decimal_word(&batch["nonce"])?;
    Ok(Submitted {
        chain_id: decimal_u64(&batch["chainId"])?,
        decimals: decimals(&quote["decimals"])?,
        key: Key {
            sponsor: address(&quote["sponsor"])?,
            quote_nonce: decimal_word(&quote["quoteNonce"])?,
        },
        account: address(&batch["account"])?,
        calls: calls(&batch["calls"])?,
        token: address(&quote["token"])?,
        maximum: decimal_word(&quote["maxTokenAmount"])?,
        token_amount: decimal_word(&quote["tokenAmount"])?,
        deadline: decimal_u64(&quote["deadline"])?,
        gas_cost: decimal_word(&quote["gasCost"])?,
        account_signature: signature(&request["accountSignature"])?,
        relayer_signature: signature(&request["relayerSignature"])?,
        authorization: authorization(&request["authorization"])?,
        call: Call {
            to: address(&call["to"])?,
            value: decimal_word(&call["value"])?,
            data: hex_bytes(&call["data"])?,
        },
    })
}

fn reason(status: u16) -> &'static str {
    match status {
        200 => "OK",
        204 => "No Content",
        403 => "Forbidden",
        400 => "Bad Request",
        404 => "Not Found",
        405 => "Method Not Allowed",
        408 => "Request Timeout",
        409 => "Conflict",
        411 => "Length Required",
        413 => "Content Too Large",
        422 => "Unprocessable Content",
        503 => "Service Unavailable",
        _ => "Internal Server Error",
    }
}

fn browser_origins(value: &str) -> Option<Vec<String>> {
    let origins: Vec<String> = value.split(',').map(str::trim).map(str::to_owned).collect();
    if origins.is_empty()
        || origins.len() > 16
        || origins.iter().any(|origin| {
            let authority = origin
                .strip_prefix("https://")
                .or_else(|| origin.strip_prefix("http://"));
            let Some(authority) = authority else {
                return true;
            };
            if authority.is_empty()
                || authority.len() > 253
                || authority.contains(['/', '?', '#', '@'])
                || !authority.bytes().all(|b| {
                    b.is_ascii_alphanumeric() || matches!(b, b'.' | b'-' | b':' | b'[' | b']')
                })
            {
                return true;
            }
            let host = if authority.starts_with('[') {
                let Some(end) = authority.find(']') else {
                    return true;
                };
                if authority[1..end].parse::<std::net::Ipv6Addr>().is_err() {
                    return true;
                }
                let tail = &authority[end + 1..];
                if !tail.is_empty()
                    && (!tail.starts_with(':')
                        || tail[1..].parse::<u16>().ok().is_none_or(|p| p == 0))
                {
                    return true;
                }
                &authority[..=end]
            } else {
                let (host, port) = authority
                    .split_once(':')
                    .map_or((authority, None), |(host, port)| (host, Some(port)));
                if port.is_some_and(|port| port.parse::<u16>().ok().is_none_or(|p| p == 0)) {
                    return true;
                }
                if host.split('.').any(|label| {
                    label.is_empty()
                        || label.len() > 63
                        || label.starts_with('-')
                        || label.ends_with('-')
                        || !label
                            .bytes()
                            .all(|b| b.is_ascii_lowercase() || b.is_ascii_digit() || b == b'-')
                }) {
                    return true;
                }
                host
            };
            host.is_empty()
                || origin.starts_with("http://")
                    && !matches!(host, "localhost" | "127.0.0.1" | "[::1]")
        })
    {
        return None;
    }
    let mut unique = origins.clone();
    unique.sort();
    unique.dedup();
    if unique.len() != origins.len() {
        return None;
    }
    Some(origins)
}

fn write_response(
    stream: &mut TcpStream,
    status: u16,
    body: &Value,
    origin: Option<&str>,
    preflight: bool,
) -> std::io::Result<()> {
    let body = if preflight {
        String::new()
    } else {
        body.to_string()
    };
    stream.set_write_timeout(Some(Duration::from_secs(10)))?;
    write!(stream, "HTTP/1.1 {status} {}\r\nContent-Type: application/json\r\nContent-Length: {}\r\nConnection: close\r\nCache-Control: no-store\r\nVary: Origin, Access-Control-Request-Method, Access-Control-Request-Headers\r\n", reason(status), body.len())?;
    if let Some(origin) = origin {
        write!(stream, "Access-Control-Allow-Origin: {origin}\r\n")?;
        if preflight {
            write!(stream, "Access-Control-Allow-Methods: POST\r\nAccess-Control-Allow-Headers: content-type\r\n")?;
        }
    }
    write!(stream, "\r\n{body}")?;
    stream.flush()?;
    stream.shutdown(Shutdown::Write)
}

fn fields<'a>(value: &'a Value, names: &[&str]) -> Result<&'a Map<String, Value>, ServiceError> {
    let map = value.as_object().ok_or(ServiceError::Malformed)?;
    if map.len() != names.len() || !names.iter().all(|name| map.contains_key(*name)) {
        return Err(ServiceError::Malformed);
    }
    Ok(map)
}

fn text(value: &Value) -> Result<&str, ServiceError> {
    value.as_str().ok_or(ServiceError::Malformed)
}

fn address(value: &Value) -> Result<Address, ServiceError> {
    let raw = text(value)?;
    if raw.len() != 42 {
        return Err(ServiceError::Malformed);
    }
    bytes(raw)
        .map_err(|_| ServiceError::Malformed)?
        .try_into()
        .map_err(|_| ServiceError::Malformed)
}

fn hex_bytes(value: &Value) -> Result<Vec<u8>, ServiceError> {
    bytes(text(value)?).map_err(|_| ServiceError::Malformed)
}

fn signature(value: &Value) -> Result<[u8; 65], ServiceError> {
    hex_bytes(value)?
        .try_into()
        .map_err(|_| ServiceError::Malformed)
}

/// Parses a canonical unsigned decimal string of at most 256 bits.
fn decimal_word(value: &Value) -> Result<Word, ServiceError> {
    let raw = text(value)?;
    if raw.is_empty()
        || raw.len() > 78
        || (raw.len() > 1 && raw.starts_with('0'))
        || !raw.bytes().all(|b| b.is_ascii_digit())
    {
        return Err(ServiceError::Malformed);
    }
    let mut result: Word = [0; 32];
    for digit in raw.bytes() {
        let mut carry = u16::from(digit - b'0');
        for byte in result.iter_mut().rev() {
            let next = u16::from(*byte) * 10 + carry;
            *byte = next.to_be_bytes()[1];
            carry = next >> 8;
        }
        if carry != 0 {
            return Err(ServiceError::Malformed);
        }
    }
    Ok(result)
}

fn amount(value: &Value) -> Result<u128, ServiceError> {
    let word = decimal_word(value)?;
    if word[..16] != [0; 16] {
        return Err(ServiceError::Refused);
    }
    let mut low = [0; 16];
    low.copy_from_slice(&word[16..]);
    Ok(u128::from_be_bytes(low))
}

fn decimal_u64(value: &Value) -> Result<u64, ServiceError> {
    u64::try_from(amount(value)?).map_err(|_| ServiceError::Refused)
}

fn decimals(value: &Value) -> Result<u8, ServiceError> {
    let number = value.as_u64().ok_or(ServiceError::Malformed)?;
    u8::try_from(number).map_err(|_| ServiceError::Refused)
}

/// Renders a 256-bit big-endian word as a canonical decimal string.
fn decimal(value: &Word) -> String {
    let mut remaining = *value;
    let mut digits = Vec::new();
    loop {
        let mut rest = 0_u16;
        for byte in &mut remaining {
            let current = (rest << 8) | u16::from(*byte);
            *byte = (current / 10).to_be_bytes()[1];
            rest = current % 10;
        }
        digits.push(char::from(b'0' + rest.to_be_bytes()[1]));
        if remaining == [0; 32] {
            break;
        }
    }
    digits.iter().rev().collect()
}

fn increment(value: Word) -> Option<Word> {
    let mut result = value;
    for byte in result.iter_mut().rev() {
        let (next, overflow) = byte.overflowing_add(1);
        *byte = next;
        if !overflow {
            return Some(result);
        }
    }
    None
}

fn calls(value: &Value) -> Result<Vec<Call>, ServiceError> {
    let list = value.as_array().ok_or(ServiceError::Malformed)?;
    if list.len() > 256 {
        return Err(ServiceError::Refused);
    }
    list.iter()
        .map(|call| {
            let call = fields(call, &["to", "value", "data"])?;
            let data = hex_bytes(&call["data"])?;
            if data.len() > 65_536 {
                return Err(ServiceError::Refused);
            }
            Ok(Call {
                to: address(&call["to"])?,
                value: decimal_word(&call["value"])?,
                data,
            })
        })
        .collect()
}

fn authorization(value: &Value) -> Result<Authorization, ServiceError> {
    let parts = fields(value, &["chainId", "address", "nonce", "yParity", "r", "s"])?;
    let parity = match parts["yParity"].as_u64() {
        Some(0) => 27,
        Some(1) => 28,
        _ => return Err(ServiceError::Malformed),
    };
    let mut signature = [0_u8; 65];
    for (index, name) in ["r", "s"].into_iter().enumerate() {
        let raw = text(&parts[name])?;
        if raw.len() != 66 {
            return Err(ServiceError::Malformed);
        }
        let part = bytes(raw).map_err(|_| ServiceError::Malformed)?;
        signature[index * 32..index * 32 + 32].copy_from_slice(&part);
    }
    signature[64] = parity;
    Ok(Authorization {
        chain_id: decimal_u64(&parts["chainId"])?,
        delegate: address(&parts["address"])?,
        nonce: decimal_u64(&parts["nonce"])?,
        signature,
    })
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn browser_policy_is_explicit_exact_and_has_no_wildcards() {
        assert_eq!(
            browser_origins("https://wallet.paxeer.network,http://127.0.0.1:3000"),
            Some(vec![
                "https://wallet.paxeer.network".to_owned(),
                "http://127.0.0.1:3000".to_owned()
            ])
        );
        for value in [
            "",
            "*",
            "null",
            "http://wallet.paxeer.network",
            "https://wallet.paxeer.network/",
            "https://wallet.paxeer.network?x",
            "https://user@wallet.paxeer.network",
            "https://wallet.paxeer.network:0",
            "https://wallet.paxeer.network,https://wallet.paxeer.network",
            "https://wallet.paxeer.network\r\nX-Allow: *",
        ] {
            assert!(browser_origins(value).is_none());
        }
        assert_eq!(ServiceError::OriginRefused.status(), 403);
    }

    #[test]
    fn decimal_words_round_trip_and_refuse_noncanonical_text() -> Result<(), ServiceError> {
        for value in [0, 1, 7, 3_145_140, u128::from(u64::MAX), u128::MAX] {
            let rendered = decimal(&word(value));
            assert_eq!(rendered, value.to_string());
            assert_eq!(decimal_word(&json!(rendered))?, word(value));
        }
        let max = "115792089237316195423570985008687907853269984665640564039457584007913129639935";
        assert_eq!(decimal_word(&json!(max))?, [255; 32]);
        assert_eq!(decimal(&[255; 32]), max);
        for invalid in [
            json!(""),
            json!("01"),
            json!("-1"),
            json!("1.0"),
            json!("0x1"),
            json!(1),
            json!("115792089237316195423570985008687907853269984665640564039457584007913129639936"),
        ] {
            assert_eq!(decimal_word(&invalid), Err(ServiceError::Malformed));
        }
        assert_eq!(amount(&json!(max)), Err(ServiceError::Refused));
        assert_eq!(increment([255; 32]), None);
        assert_eq!(increment(word(255)), Some(word(256)));
        Ok(())
    }

    #[test]
    fn station_refusals_are_4xx_and_unavailability_is_5xx() {
        for (error, status) in [
            (StationError::Invalid, 422),
            (StationError::Missing, 422),
            (StationError::Conflict, 409),
            (StationError::Rpc(RpcFault::Rejected { code: -32_000 }), 422),
            (StationError::Rpc(RpcFault::Unavailable), 503),
            (StationError::Rpc(RpcFault::Divergence), 503),
            (StationError::Price(PriceError::StaleRate), 503),
            (
                StationError::Quote(QuoteError::Price(PriceError::MissingRate)),
                503,
            ),
            (
                StationError::Quote(QuoteError::Price(PriceError::ZeroGas)),
                422,
            ),
            (StationError::Quote(QuoteError::AboveMaximum), 422),
            (
                StationError::Quote(QuoteError::Policy(PolicyRefusal::PerAccount)),
                422,
            ),
            (
                StationError::Quote(QuoteError::Policy(PolicyRefusal::BalanceFloor)),
                503,
            ),
            (StationError::Journal(JournalError::Io), 500),
            (StationError::Journal(JournalError::Conflict), 409),
            (StationError::Transaction(TxError::Signature), 422),
        ] {
            assert_eq!(ServiceError::from(&error).status(), status);
        }
        assert_eq!(
            ServiceError::Unavailable.to_string(),
            "service refused: unavailable"
        );
    }

    #[test]
    fn request_fields_are_exact_and_authorizations_canonical() -> Result<(), ServiceError> {
        assert!(fields(&json!({"a":1}), &["a"]).is_ok());
        for value in [json!({"a":1,"b":2}), json!({}), json!([1])] {
            assert_eq!(fields(&value, &["a"]).err(), Some(ServiceError::Malformed));
        }
        let r = format!("0x{}", "11".repeat(32));
        let s = format!("0x{}", "22".repeat(32));
        let parsed = authorization(&json!({"chainId":"1325","address":hex(&[0x44; 20]),
            "nonce":"0","yParity":1,"r":r,"s":s}))?;
        assert_eq!(parsed.chain_id, 1325);
        assert_eq!(parsed.delegate, [0x44; 20]);
        assert_eq!(parsed.signature[..32], [0x11; 32]);
        assert_eq!(parsed.signature[32..64], [0x22; 32]);
        assert_eq!(parsed.signature[64], 28);
        assert!(
            authorization(&json!({"chainId":"1325","address":hex(&[0x44; 20]),
            "nonce":"0","yParity":2,"r":r,"s":s}))
            .is_err()
        );
        assert_eq!(address(&json!("0x44")), Err(ServiceError::Malformed));
        assert_eq!(
            calls(&json!(vec![
                json!({"to":hex(&[0x55; 20]),"value":"0","data":"0x"});
                257
            ]))
            .err(),
            Some(ServiceError::Refused)
        );
        Ok(())
    }
    #[test]
    fn issuance_json_rejects_duplicate_keys_at_every_depth(
    ) -> Result<(), Box<dyn std::error::Error>> {
        for input in [
            br#"{"account":"one","account":"two"}"#.as_slice(),
            br#"{"calls":[{"to":"one","to":"two"}]}"#.as_slice(),
            br#"{"quote":{"deadline":"1","deadline":"2"}}"#.as_slice(),
        ] {
            assert!(serde_json::from_slice::<StrictValue>(input).is_err());
        }
        let input = br#"{"calls":[{"to":"one","value":"0","data":"0x"}],"decimals":6}"#;
        let strict = serde_json::from_slice::<StrictValue>(input)?;
        assert_eq!(strict.0, serde_json::from_slice::<Value>(input)?);
        Ok(())
    }
}
