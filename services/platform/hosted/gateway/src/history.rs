//! Account history on the single network endpoint: `px_getHistory` and
//! `px_getUnifiedHistory`, answered from the `layerx-indexer` read API.
//!
//! Every answer is the indexer's own rows, oldest first in
//! `(blockHeight, txIndex, logIndex)` position order, each joined with its
//! asset's indexer record plus the symbol and decimals the `LayerX` asset
//! registry declares for it. Cursors are the indexer's 32-lowercase-hex
//! position cursors, passed through unchanged; because every side pages
//! strictly after the same position, a unified page is the exact merge of
//! every side's page at the same cursor.

use super::{public_reads, Config, Endpoint};
use layerx_indexer::store::Position;
use layerx_platform_gateway::evm;
use layerx_platform_gateway::http::{self, Client, UpstreamResponse};
use serde_json::{json, Value};
use std::collections::BTreeMap;
use std::fmt::Write as _;
use std::net::SocketAddr;

/// Rows per page when the caller names no limit.
pub(super) const DEFAULT_LIMIT: u64 = 50;
/// The largest page the gateway asks the indexer for.
pub(super) const MAXIMUM_LIMIT: u64 = 100;
const MAXIMUM_KIND: usize = 64;

/// Where the indexer read API lives.
pub(super) enum Indexer {
    /// A TLS endpoint verified with the gateway's component trust roots.
    Tls(Endpoint),
    /// A co-located indexer on a loopback address, spoken to in plain HTTP.
    Loopback {
        address: SocketAddr,
        authority: String,
        base_path: String,
    },
}

impl Indexer {
    pub(super) fn parse(value: &str) -> Result<Self, String> {
        let Some(rest) = value.strip_prefix("http://") else {
            return Endpoint::parse(value).map(Self::Tls);
        };
        let (authority, path) = rest.split_once('/').unwrap_or((rest, ""));
        if path.contains(['?', '#', '\\']) {
            return Err("indexer endpoint is not canonical".to_owned());
        }
        let address = authority
            .parse::<SocketAddr>()
            .map_err(|_| "plain HTTP indexer endpoints must name an IP address and port")?;
        if !address.ip().is_loopback() {
            return Err("plain HTTP indexer endpoints must be loopback".to_owned());
        }
        Ok(Self::Loopback {
            address,
            authority: authority.to_owned(),
            base_path: if path.is_empty() {
                String::new()
            } else {
                format!("/{}", path.trim_end_matches('/'))
            },
        })
    }
}

pub(super) fn configured_endpoint() -> Result<Option<Indexer>, String> {
    std::env::var("LAYERX_GATEWAY_INDEXER_URL")
        .ok()
        .map(|url| Indexer::parse(&url))
        .transpose()
}

/// One way of reaching the indexer: the configured endpoint and, for a TLS
/// endpoint, the gateway's component client.
pub(super) struct Source<'a> {
    pub(super) indexer: &'a Indexer,
    pub(super) client: Option<&'a Client>,
}

impl Source<'_> {
    fn get(&self, path: &str, query: &str) -> Result<(u16, Value), &'static str> {
        let upstream: Result<UpstreamResponse, String> = match self.indexer {
            Indexer::Tls(endpoint) => match self.client {
                Some(client) => client.get_with_query(endpoint, path, query),
                None => Err("no component client".to_owned()),
            },
            Indexer::Loopback {
                address,
                authority,
                base_path,
            } => http::loopback_get(*address, authority, &format!("{base_path}{path}"), query),
        };
        let upstream = upstream.map_err(|_| "indexer_unreachable")?;
        if upstream.content_type != "application/json" {
            return Err("invalid_indexer_response");
        }
        let body =
            serde_json::from_slice(&upstream.body).map_err(|_| "invalid_indexer_response")?;
        Ok((upstream.status, body))
    }
}

/// The paging selector of one history read.
#[derive(Debug, PartialEq, Eq)]
pub(super) struct Query {
    pub(super) cursor: Option<String>,
    pub(super) limit: u64,
    pub(super) kind: Option<String>,
}

impl Query {
    fn encoded(&self) -> String {
        let mut pairs = vec![format!("limit={}", self.limit)];
        if let Some(cursor) = &self.cursor {
            pairs.push(format!("cursor={}", encode(cursor)));
        }
        if let Some(kind) = &self.kind {
            pairs.push(format!("kind={}", encode(kind)));
        }
        pairs.join("&")
    }
}

/// Why a history read has no page.
#[derive(Debug, PartialEq)]
pub(super) enum Failure {
    /// The indexer refused the selector; its refusal document is kept.
    Refused(Value),
    /// The indexer could not answer.
    Unavailable(&'static str),
}

fn encode(text: &str) -> String {
    let mut encoded = String::with_capacity(text.len());
    for byte in text.bytes() {
        if byte.is_ascii_alphanumeric() || b"-._~".contains(&byte) {
            encoded.push(char::from(byte));
        } else {
            let _ = write!(encoded, "%{byte:02X}");
        }
    }
    encoded
}

/// Splits `[account, cursor?, limit?, kind?]`.
pub(super) fn params(params: Option<&Value>) -> Result<(String, Query), i32> {
    let Some(Value::Array(args)) = params else {
        return Err(-32602);
    };
    if args.is_empty() || args.len() > 4 {
        return Err(-32602);
    }
    let Value::String(account) = &args[0] else {
        return Err(-32602);
    };
    let cursor = match args.get(1) {
        None | Some(Value::Null) => None,
        Some(Value::String(cursor)) if Position::parse(cursor).is_some() => Some(cursor.clone()),
        Some(_) => return Err(-32602),
    };
    let limit = match args.get(2) {
        None | Some(Value::Null) => DEFAULT_LIMIT,
        Some(Value::Number(limit)) => limit
            .as_u64()
            .filter(|limit| (1..=MAXIMUM_LIMIT).contains(limit))
            .ok_or(-32602)?,
        Some(_) => return Err(-32602),
    };
    let kind = match args.get(3) {
        None | Some(Value::Null) => None,
        Some(Value::String(kind))
            if (1..=MAXIMUM_KIND).contains(&kind.len())
                && kind.bytes().all(|byte| {
                    byte.is_ascii_lowercase() || byte.is_ascii_digit() || byte == b'_'
                }) =>
        {
            Some(kind.clone())
        }
        Some(_) => return Err(-32602),
    };
    Ok((
        account.clone(),
        Query {
            cursor,
            limit,
            kind,
        },
    ))
}

/// A Paxeer account as the indexer keys it: a lowercase `0x` address.
pub(super) fn paxeer_key(account: &str) -> Option<String> {
    account
        .starts_with("0x")
        .then(|| evm::parse_address(account))
        .flatten()
        .map(|address| evm::address_hex(&address))
}

/// The indexer's order of one history row: its position, its ordinal,
/// indexed rows before explorer-imported rows, then its numeric row id.
type RowKey = (Position, u64, bool, u64);

struct Page {
    items: Vec<(RowKey, Value)>,
    next_cursor: Option<String>,
    indexed_height: Option<u64>,
}

/// The indexer's `indexedHeight` of a page: its indexed Paxeer height, or
/// `null` before the first indexed block.
fn indexed_height(body: &Value) -> Result<Option<u64>, Failure> {
    match body.get("indexedHeight") {
        Some(Value::Null) => Ok(None),
        Some(height) if height.is_u64() => Ok(height.as_u64()),
        _ => Err(Failure::Unavailable("invalid_indexer_response")),
    }
}

fn page(source: &Source<'_>, account: &str, query: &Query) -> Result<Page, Failure> {
    let (status, body) = source
        .get(
            &format!("/v1/history/{}", encode(account)),
            &query.encoded(),
        )
        .map_err(Failure::Unavailable)?;
    match status {
        200 => {}
        400 => return Err(Failure::Refused(body)),
        _ => return Err(Failure::Unavailable("indexer_unavailable")),
    }
    let invalid = || Failure::Unavailable("invalid_indexer_response");
    if body.get("version") != Some(&json!(1)) {
        return Err(invalid());
    }
    let next_cursor = match body.get("next_cursor") {
        Some(Value::Null) => None,
        Some(Value::String(cursor)) if Position::parse(cursor).is_some() => Some(cursor.clone()),
        _ => return Err(invalid()),
    };
    let rows = body
        .get("items")
        .and_then(Value::as_array)
        .ok_or_else(invalid)?;
    let limit = usize::try_from(query.limit).map_err(|_| invalid())?;
    let mut items: Vec<(RowKey, Value)> = Vec::with_capacity(rows.len());
    for row in rows {
        let (explorer, id) = text(row, "id")
            .filter(|_| row.get("asset").is_some_and(Value::is_string))
            .and_then(row_id)
            .ok_or_else(invalid)?;
        let position = row_position(row).ok_or_else(invalid)?;
        let ordinal = text(row, "ordinal")
            .and_then(decimal)
            .filter(|ordinal| (*ordinal).max(1) - 1 == u64::from(position.log_index))
            .ok_or_else(invalid)?;
        if items.last().is_some_and(|((last, ..), _)| *last > position) {
            return Err(invalid());
        }
        let mut row = row.clone();
        if let Some(object) = row.as_object_mut() {
            object.insert("blockHeight".to_owned(), json!(position.height));
            object.insert("txIndex".to_owned(), json!(position.tx_index));
            object.insert("logIndex".to_owned(), json!(position.log_index));
        }
        items.push(((position, ordinal, explorer, id), row));
    }
    // The indexer reads a limit of 0 as 1 and keeps the rows of the page's
    // last position together, so only rows sharing the position of row
    // `limit` may follow it.
    let limit = limit.max(1);
    if let (Some(((boundary, ..), _)), Some(overflow)) = (items.get(limit - 1), items.get(limit..))
    {
        if overflow
            .iter()
            .any(|((position, ..), _)| position != boundary)
        {
            return Err(invalid());
        }
    }
    body.get("freshness")
        .filter(|value| readiness_status(value).is_some())
        .ok_or_else(invalid)?;
    Ok(Page {
        items,
        next_cursor,
        indexed_height: indexed_height(&body)?,
    })
}

/// The `(blockHeight, txIndex, logIndex)` position an indexer row carries
/// as the decimal strings `height_or_seq`, `tx_index` and `log_index`.
fn row_position(row: &Value) -> Option<Position> {
    let field = |name: &str| text(row, name).and_then(|value| value.parse::<u64>().ok());
    let height = field("height_or_seq")?;
    i64::try_from(height).ok()?;
    Some(Position {
        height,
        tx_index: u32::try_from(field("tx_index")?).ok()?,
        log_index: u32::try_from(field("log_index")?).ok()?,
    })
}

/// A row id: the indexed row `<n>` or the explorer-imported row
/// `explorer:<n>`, as whether it is imported and its number.
fn row_id(id: &str) -> Option<(bool, u64)> {
    match id.strip_prefix("explorer:") {
        Some(number) => decimal(number).map(|number| (true, number)),
        None => decimal(id).map(|number| (false, number)),
    }
}

/// A non-empty string of ASCII digits as a `u64`.
fn decimal(text: &str) -> Option<u64> {
    (!text.is_empty() && text.bytes().all(|byte| byte.is_ascii_digit()))
        .then(|| text.parse().ok())
        .flatten()
}

fn text<'a>(document: &'a Value, field: &str) -> Option<&'a str> {
    document.get(field).and_then(Value::as_str)
}

/// The joined metadata of one asset: the indexer's record, the registry
/// symbol and decimals, the `LayerX` asset it is (or points at) and, for a
/// pointer contract, its EVM address.
fn asset_metadata(asset: &str, record: &Value, registry: &[Value]) -> Value {
    if !record.is_object() {
        return Value::Null;
    }
    let kind = text(record, "kind").unwrap_or_default();
    let denom = text(record, "denom");
    let registered = registry.iter().find(|entry| {
        if kind == "layerx" {
            text(entry, "asset_id") == Some(asset)
        } else {
            denom.is_some() && text(entry, "denom") == denom
        }
    });
    let metadata = record.get("metadata").cloned().unwrap_or(Value::Null);
    let symbol = text(&metadata, "symbol")
        .or_else(|| registered.and_then(|entry| text(entry, "symbol")))
        .map(str::to_owned);
    let decimals = metadata
        .get("decimals")
        .and_then(Value::as_u64)
        .or_else(|| registered.and_then(|entry| entry.get("decimals").and_then(Value::as_u64)))
        .or_else(|| (kind == "native").then_some(18));
    let native_id = if kind == "layerx" {
        Some(asset.to_owned())
    } else {
        registered
            .and_then(|entry| text(entry, "asset_id"))
            .map(str::to_owned)
    };
    json!({
        "asset": asset,
        "chain": record.get("chain").cloned().unwrap_or(Value::Null),
        "kind": kind,
        "address": record.get("address").cloned().unwrap_or(Value::Null),
        "denom": denom,
        "symbol": symbol,
        "decimals": decimals,
        "native_id": native_id,
        "pointer": if kind == "pointer" { record.get("address").cloned().unwrap_or(Value::Null) } else { Value::Null },
        "metadata": metadata,
    })
}

fn join_assets(
    source: &Source<'_>,
    registry: &dyn Fn() -> Vec<Value>,
    items: &mut [Value],
) -> Result<(), Failure> {
    if items.is_empty() {
        return Ok(());
    }
    let mut records: BTreeMap<String, Value> = BTreeMap::new();
    for item in items.iter() {
        let Some(asset) = text(item, "asset") else {
            continue;
        };
        if records.contains_key(asset) {
            continue;
        }
        let (status, body) = source
            .get(&format!("/v1/assets/{}", encode(asset)), "")
            .map_err(Failure::Unavailable)?;
        let record = match status {
            200 => body
                .get("asset")
                .filter(|record| text(record, "asset") == Some(asset))
                .cloned()
                .ok_or(Failure::Unavailable("invalid_indexer_response"))?,
            404 => Value::Null,
            _ => return Err(Failure::Unavailable("indexer_unavailable")),
        };
        records.insert(asset.to_owned(), record);
    }
    let registry = registry();
    for item in items.iter_mut() {
        let asset = text(item, "asset").unwrap_or_default().to_owned();
        let joined = records.get(&asset).map_or(Value::Null, |record| {
            asset_metadata(&asset, record, &registry)
        });
        if let Some(row) = item.as_object_mut() {
            row.insert("asset_metadata".to_owned(), joined);
        }
    }
    Ok(())
}

/// One page of the merged history of `accounts`, each tagged with the side
/// it answers for. A single account's page and cursor are the indexer's own;
/// several accounts are merged in the indexer's own row order (position,
/// ordinal, indexed before imported, row id) with each row tagged by
/// `side`. Rows sharing the last position of a merged page stay together,
/// as on the indexer, so resuming strictly after that position neither
/// repeats nor skips a row.
pub(super) fn history(
    source: &Source<'_>,
    registry: &dyn Fn() -> Vec<Value>,
    accounts: &[(&'static str, String)],
    query: &Query,
) -> Result<Value, Failure> {
    let limit = usize::try_from(query.limit).unwrap_or(usize::MAX);
    let mut indexed: Option<Option<u64>> = None;
    let mut lowest = |height: Option<u64>| {
        indexed = Some(match indexed {
            None => height,
            Some(seen) => seen.zip(height).map(|(seen, height)| seen.min(height)),
        });
    };
    let (mut items, next_cursor): (Vec<Value>, Option<String>) = if let [(_, account)] = accounts {
        let page = page(source, account, query)?;
        lowest(page.indexed_height);
        (
            page.items.into_iter().map(|(_, item)| item).collect(),
            page.next_cursor,
        )
    } else {
        let mut merged: BTreeMap<RowKey, Value> = BTreeMap::new();
        let mut more = false;
        for (side, account) in accounts {
            let page = page(source, account, query)?;
            lowest(page.indexed_height);
            more |= page.next_cursor.is_some();
            for (key, mut item) in page.items {
                if let Some(row) = item.as_object_mut() {
                    row.insert("side".to_owned(), json!(side));
                }
                merged.entry(key).or_insert(item);
            }
        }
        let boundary = merged
            .keys()
            .nth(limit.saturating_sub(1))
            .map(|(position, ..)| *position);
        let mut rows: Vec<(Position, Value)> = Vec::with_capacity(merged.len());
        for ((position, ..), item) in merged {
            if boundary.is_some_and(|boundary| position > boundary) {
                more = true;
                break;
            }
            rows.push((position, item));
        }
        let next_cursor = if more {
            rows.last().map(|(position, _)| position.encode())
        } else {
            None
        };
        (
            rows.into_iter().map(|(_, item)| item).collect(),
            next_cursor,
        )
    };
    join_assets(source, registry, &mut items)?;
    Ok(json!({
        "items": items,
        "next_cursor": next_cursor,
        "indexedHeight": indexed.flatten(),
    }))
}

fn refusal(id: &Value, code: i32, message: &str, data: Value) -> Value {
    let mut refusal = super::rpc::error(id, code, message);
    refusal["error"]["data"] = data;
    refusal
}

fn registry(config: &Config) -> Vec<Value> {
    super::rpc::read_result(config, "/v1/assets")
        .and_then(|result| result.get("assets").and_then(Value::as_array).cloned())
        .unwrap_or_default()
}

fn answer(config: &Config, method: &str, id: &Value, params: Option<&Value>) -> Value {
    let (account, query) = match self::params(params) {
        Ok(selected) => selected,
        Err(code) => return super::rpc::error(id, code, "Invalid params"),
    };
    let (document, accounts) = match method {
        "px_getHistory" => {
            let key = match paxeer_key(&account) {
                Some(key) => key,
                None => match super::paxeer::history_account(config, id, &account) {
                    Ok((_, key)) => key,
                    Err(refused) => return refused,
                },
            };
            (json!(key.clone()), vec![("paxeer", key)])
        }
        _ => match super::paxeer::history_account(config, id, &account) {
            Ok((document, key)) => (document, vec![("paxeer", key)]),
            Err(refused) => return refused,
        },
    };
    let Some(indexer) = &config.indexer else {
        return refusal(
            id,
            -32001,
            "History unavailable",
            json!({"code": "indexer_not_configured"}),
        );
    };
    if !public_reads::consume_read() {
        return refusal(
            id,
            -32005,
            "Read unavailable",
            json!({"code": "public_read_rate_limit"}),
        );
    }
    let source = Source {
        indexer,
        client: Some(&config.client),
    };
    let mut result = match history(&source, &|| registry(config), &accounts, &query) {
        Ok(result) => result,
        Err(Failure::Refused(body)) => return refusal(id, -32602, "Invalid params", body),
        Err(Failure::Unavailable(code)) => {
            return refusal(id, -32001, "History unavailable", json!({"code": code}))
        }
    };
    let freshness = match source.get("/readyz", "") {
        Ok((200 | 503, body)) if readiness_status(&body).is_some() => body,
        _ => {
            return refusal(
                id,
                -32001,
                "History unavailable",
                json!({"code": "invalid_indexer_readiness"}),
            )
        }
    };
    let chain_height = match super::paxeer::block_number(config) {
        Ok(height) => height,
        Err(code) => return refusal(id, -32001, "History unavailable", json!({"code": code})),
    };
    if let Some(object) = result.as_object_mut() {
        object.insert("chainHeight".to_owned(), json!(chain_height));
        object.insert("freshness".to_owned(), freshness);
        object.insert("account".to_owned(), document);
        if method == "px_getUnifiedHistory" {
            let sides: Vec<Value> = accounts
                .iter()
                .map(|(side, account)| json!({"side": side, "account": account}))
                .collect();
            object.insert("accounts".to_owned(), Value::Array(sides));
        }
    }
    json!({"jsonrpc": "2.0", "id": id, "result": result})
}

/// The record kinds `px_listHolderRecords` lists, as the indexer names them.
const HOLDER_KINDS: [&str; 2] = ["budget", "grant"];

/// Splits `[address, kind, cursor?, limit?]` of `px_listHolderRecords`.
pub(super) fn holder_params(params: Option<&Value>) -> Result<(String, Query), i32> {
    let Some(Value::Array(args)) = params else {
        return Err(-32602);
    };
    if !(2..=4).contains(&args.len()) {
        return Err(-32602);
    }
    let account = args[0].as_str().and_then(paxeer_key).ok_or(-32602)?;
    let kind = args[1]
        .as_str()
        .filter(|kind| HOLDER_KINDS.contains(kind))
        .ok_or(-32602)?;
    let cursor = match args.get(2) {
        None | Some(Value::Null) => None,
        Some(Value::String(cursor)) if Position::parse(cursor).is_some() => Some(cursor.clone()),
        Some(_) => return Err(-32602),
    };
    let limit = match args.get(3) {
        None | Some(Value::Null) => DEFAULT_LIMIT,
        Some(Value::Number(limit)) => limit
            .as_u64()
            .filter(|limit| (1..=MAXIMUM_LIMIT).contains(limit))
            .ok_or(-32602)?,
        Some(_) => return Err(-32602),
    };
    Ok((
        account,
        Query {
            cursor,
            limit,
            kind: Some(kind.to_owned()),
        },
    ))
}

/// One page of `account`'s holder records of `query.kind` from the indexer
/// `/v1/holders/{address}` route: its rows, its position cursor and the
/// indexed Paxeer height.
pub(super) fn holder_records(
    source: &Source<'_>,
    account: &str,
    query: &Query,
) -> Result<Value, Failure> {
    let (status, body) = source
        .get(
            &format!("/v1/holders/{}", encode(account)),
            &query.encoded(),
        )
        .map_err(Failure::Unavailable)?;
    match status {
        200 => {}
        400 => return Err(Failure::Refused(body)),
        _ => return Err(Failure::Unavailable("indexer_unavailable")),
    }
    let invalid = || Failure::Unavailable("invalid_indexer_response");
    if body.get("version") != Some(&json!(1)) {
        return Err(invalid());
    }
    let next_cursor = match body.get("next_cursor") {
        Some(Value::Null) => Value::Null,
        Some(Value::String(cursor)) if Position::parse(cursor).is_some() => json!(cursor),
        _ => return Err(invalid()),
    };
    let indexed_height = match body.get("indexedHeight") {
        Some(Value::Null) => Value::Null,
        Some(height) if height.is_u64() => height.clone(),
        _ => return Err(invalid()),
    };
    let items = body
        .get("items")
        .and_then(Value::as_array)
        .filter(|items| {
            u64::try_from(items.len()).is_ok_and(|count| count <= query.limit)
                && items.iter().all(|item| {
                    text(item, "holder") == Some(account)
                        && text(item, "kind") == query.kind.as_deref()
                })
        })
        .ok_or_else(invalid)?;
    Ok(json!({"items": items, "next_cursor": next_cursor, "indexedHeight": indexed_height}))
}

fn answer_holders(config: &Config, id: &Value, params: Option<&Value>) -> Value {
    let (account, query) = match holder_params(params) {
        Ok(selected) => selected,
        Err(code) => return super::rpc::error(id, code, "Invalid params"),
    };
    let Some(indexer) = &config.indexer else {
        return refusal(
            id,
            -32001,
            "History unavailable",
            json!({"code": "indexer_not_configured"}),
        );
    };
    if !public_reads::consume_read() {
        return refusal(
            id,
            -32005,
            "Read unavailable",
            json!({"code": "public_read_rate_limit"}),
        );
    }
    let source = Source {
        indexer,
        client: Some(&config.client),
    };
    let mut result = match holder_records(&source, &account, &query) {
        Ok(result) => result,
        Err(Failure::Refused(body)) => return refusal(id, -32602, "Invalid params", body),
        Err(Failure::Unavailable(code)) => {
            return refusal(id, -32001, "History unavailable", json!({"code": code}))
        }
    };
    match super::paxeer::block_number(config) {
        Ok(height) => result["chainHeight"] = json!(height),
        Err(code) => return refusal(id, -32001, "History unavailable", json!({"code": code})),
    }
    json!({"jsonrpc": "2.0", "id": id, "result": result})
}

/// Answers the two history methods and `px_listHolderRecords`, or `None`
/// for any other method.
pub(super) fn dispatch(
    config: &Config,
    method: &str,
    id: &Value,
    params: Option<&Value>,
) -> Option<Value> {
    match method {
        "px_getHistory" | "px_getUnifiedHistory" => Some(answer(config, method, id, params)),
        "px_listHolderRecords" => Some(answer_holders(config, id, params)),
        _ => None,
    }
}

fn readiness_status(body: &Value) -> Option<&'static str> {
    if body.get("version")?.as_u64()? != 1 || body.get("database")?.as_str()? != "ok" {
        return None;
    }
    let sources = body.get("sources")?.as_array()?;
    let mut rank = if sources.is_empty() { 2 } else { 0 };
    let mut names = Vec::new();
    for source in sources {
        let name = source.get("source")?.as_str()?;
        if !matches!(name, "layerx" | "paxeer" | "comet") || names.contains(&name) {
            return None;
        }
        names.push(name);
        for field in [
            "source_head",
            "indexed_cursor",
            "finalized_cursor",
            "last_success_at",
            "freshness_secs",
            "last_error_at",
        ] {
            let value = source.get(field)?;
            if !value.is_null() && value.as_u64().is_none() {
                return None;
            }
        }
        source.get("reconciled")?.as_bool()?;
        source.get("consecutive_failures")?.as_u64()?;
        let state = source.get("state")?.as_str()?;
        let source_rank = match state {
            "ready" => {
                if !source.get("reconciled")?.as_bool()?
                    || source.get("last_success_at")?.as_u64().is_none()
                    || source.get("freshness_secs")?.as_u64().is_none()
                    || source.get("consecutive_failures")?.as_u64()? != 0
                    || source.get("source_head")? != source.get("indexed_cursor")?
                {
                    return None;
                }
                0
            }
            "catching_up" | "retrying" | "failing" => 1,
            "starting" | "stalled" => 2,
            _ => return None,
        };
        rank = rank.max(source_rank);
    }
    match (body.get("status")?.as_str()?, rank) {
        ("ready", 0) => Some("available"),
        ("degraded", 1) => Some("degraded"),
        ("unavailable", 2) => Some("unavailable"),
        _ => None,
    }
}

/// The indexer's state for the gateway's own status document.
pub(super) fn status(config: &Config) -> &'static str {
    let Some(indexer) = &config.indexer else {
        return "not_configured";
    };
    let source = Source {
        indexer,
        client: Some(&config.client),
    };
    match source.get("/readyz", "") {
        Ok((200, body)) => readiness_status(&body).unwrap_or("unavailable"),
        Ok((503, body)) => match readiness_status(&body) {
            Some("degraded") => "degraded",
            _ => "unavailable",
        },
        _ => "unavailable",
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use layerx_indexer::store::{AssetRow, HolderRow, Position, Store, TransferRow, Unit};
    use std::net::TcpListener;
    use std::sync::Arc;

    const LXP: &str = "1111111111111111111111111111111111111111111111111111111111111111";
    const ALICE: &str = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa";
    const BOB: &str = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb";
    const EVE: &str = "0xeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee";
    const FRANK: &str = "0xffffffffffffffffffffffffffffffffffffffff";
    const POINTER: &str = "0xcccccccccccccccccccccccccccccccccccccccc";
    const TICKET: &str = "denom:factory/pax1issuer/ticket";
    const BUDGET: &str = "0101010101010101010101010101010101010101010101010101010101010101";

    /// One transfer leg of transaction `tx_index` at `position`: a native
    /// value row when `ordinal` is 0, else the log at index `ordinal - 1`.
    fn leg(
        (position, tx_index, ordinal): (u64, u32, u64),
        kind: &str,
        direction: &'static str,
        account: &str,
        counterparty: &str,
        asset: &str,
        amount: &str,
    ) -> TransferRow {
        TransferRow {
            height_or_seq: position,
            tx_index,
            kind: kind.to_owned(),
            direction,
            account: account.to_owned(),
            counterparty: Some(counterparty.to_owned()),
            asset: asset.to_owned(),
            amount: amount.to_owned(),
            tx_id: format!("{kind}-{position}-{tx_index}"),
            ordinal,
            decoded: json!({"position": position.to_string()}),
        }
    }

    fn asset(
        asset: &str,
        chain: &str,
        kind: &str,
        address: Option<&str>,
        denom: Option<&str>,
    ) -> AssetRow {
        AssetRow {
            asset: asset.to_owned(),
            chain: chain.to_owned(),
            kind: kind.to_owned(),
            address: address.map(str::to_owned),
            denom: denom.map(str::to_owned),
            metadata: json!({}),
        }
    }

    fn unit(
        chain: &str,
        position: u64,
        transfers: Vec<TransferRow>,
        assets: Vec<AssetRow>,
    ) -> Unit {
        Unit {
            chain: chain.to_owned(),
            position,
            hash: format!("{chain}-{position}"),
            parent: format!("{chain}-{}", position - 1),
            link: format!("{chain}-{position}"),
            boundary: position,
            transfers,
            assets,
            ..Unit::default()
        }
    }

    fn at(height: u64, tx_index: u32, log_index: u32) -> Position {
        Position {
            height,
            tx_index,
            log_index,
        }
    }

    /// A real indexer store and read API on a loopback port, filled with both
    /// chains interleaved in commit order. Row ids: 1-2 `LayerX` transfer
    /// alice->bob at (1,0,0), 3-4 Paxeer native eve->frank at (10,0,0), 5
    /// `LayerX` credit to alice at (2,0,0), 6-7 Paxeer pointer frank->eve at
    /// (11,0,0), 8 Paxeer custody deposit into alice at (11,1,0), 9-10 Paxeer
    /// bank ticket eve->frank at (12,0,0), 11 an unregistered asset to eve at
    /// (12,1,0). Eve holds one budget opened at (12,2,1).
    fn indexer() -> Indexer {
        let store = Store::open_in_memory().unwrap_or_else(|error| panic!("{error}"));
        store
            .register_assets(&[AssetRow {
                asset: format!("evm:{POINTER}"),
                ..asset("", "paxeer", "pointer", Some(POINTER), Some("ulxp"))
            }])
            .unwrap_or_else(|error| panic!("{error}"));
        let units = [
            unit(
                "layerx",
                1,
                vec![
                    leg((1, 0, 0), "lxp_transfer", "out", ALICE, BOB, LXP, "5"),
                    leg((1, 0, 0), "lxp_transfer", "in", BOB, ALICE, LXP, "5"),
                ],
                vec![AssetRow {
                    metadata: json!({"supply": "100"}),
                    ..asset(LXP, "layerx", "layerx", None, None)
                }],
            ),
            unit(
                "paxeer",
                10,
                vec![
                    leg(
                        (10, 0, 0),
                        "native_transfer",
                        "out",
                        EVE,
                        FRANK,
                        "evm:native",
                        "1000000000000000000",
                    ),
                    leg(
                        (10, 0, 0),
                        "native_transfer",
                        "in",
                        FRANK,
                        EVE,
                        "evm:native",
                        "1000000000000000000",
                    ),
                ],
                vec![asset("evm:native", "paxeer", "native", None, None)],
            ),
            unit(
                "layerx",
                2,
                vec![TransferRow {
                    counterparty: None,
                    ..leg((2, 0, 0), "lxp_credit", "in", ALICE, "", LXP, "7")
                }],
                Vec::new(),
            ),
            unit(
                "paxeer",
                11,
                vec![
                    leg(
                        (11, 0, 1),
                        "pointer_transfer",
                        "in",
                        EVE,
                        FRANK,
                        &format!("evm:{POINTER}"),
                        "3",
                    ),
                    leg(
                        (11, 0, 1),
                        "pointer_transfer",
                        "out",
                        FRANK,
                        EVE,
                        &format!("evm:{POINTER}"),
                        "3",
                    ),
                    leg((11, 1, 0), "custody_deposit", "in", ALICE, EVE, LXP, "9"),
                ],
                Vec::new(),
            ),
            Unit {
                holders: vec![HolderRow {
                    position: at(12, 2, 1),
                    holder: EVE.to_owned(),
                    kind: "budget",
                    contract: "layerxpay",
                    record_id: BUDGET.to_owned(),
                    event: "BudgetCreated",
                    state: "active",
                    tx_id: "budget-12-2".to_owned(),
                    decoded: json!({"args": {"amount": "500"}}),
                }],
                ..unit(
                    "paxeer",
                    12,
                    vec![
                        leg((12, 0, 0), "bank_transfer", "out", EVE, FRANK, TICKET, "2"),
                        leg((12, 0, 0), "bank_transfer", "in", FRANK, EVE, TICKET, "2"),
                        leg(
                            (12, 1, 1),
                            "erc20_transfer",
                            "in",
                            EVE,
                            FRANK,
                            "evm:0x0000000000000000000000000000000000000abc",
                            "4",
                        ),
                    ],
                    vec![asset(
                        TICKET,
                        "paxeer",
                        "tokenfactory",
                        None,
                        Some("factory/pax1issuer/ticket"),
                    )],
                )
            },
        ];
        for unit in &units {
            store
                .commit(unit, 64)
                .unwrap_or_else(|error| panic!("{error}"));
        }
        serve(store)
    }

    /// The indexer read API over `store` on a loopback port.
    fn serve(store: Store) -> Indexer {
        let listener = TcpListener::bind("127.0.0.1:0").unwrap_or_else(|error| panic!("{error}"));
        let address = listener
            .local_addr()
            .unwrap_or_else(|error| panic!("{error}"));
        let store = Arc::new(store);
        std::thread::spawn(move || layerx_indexer::api::serve(&listener, &store, None));
        Indexer::parse(&format!("http://{address}")).unwrap_or_else(|error| panic!("{error}"))
    }

    fn registry() -> Vec<Value> {
        vec![
            json!({"asset_id": LXP, "symbol": "LXP", "name": "LayerX Points", "decimals": 6, "denom": "ulxp"}),
        ]
    }

    fn query(cursor: Option<&str>, limit: u64, kind: Option<&str>) -> Query {
        Query {
            cursor: cursor.map(str::to_owned),
            limit,
            kind: kind.map(str::to_owned),
        }
    }

    fn read(indexer: &Indexer, accounts: &[(&'static str, String)], query: &Query) -> Value {
        history(
            &Source {
                indexer,
                client: None,
            },
            &registry,
            accounts,
            query,
        )
        .unwrap_or_else(|failure| panic!("{failure:?}"))
    }

    fn items(page: &Value) -> &[Value] {
        page["items"]
            .as_array()
            .map(Vec::as_slice)
            .unwrap_or_else(|| panic!("no items in {page}"))
    }

    fn ids(page: &Value) -> Vec<u64> {
        items(page)
            .iter()
            .map(|item| {
                item["id"]
                    .as_str()
                    .and_then(|id| id.parse().ok())
                    .unwrap_or_else(|| panic!("row id in {item}"))
            })
            .collect()
    }

    fn position(item: &Value) -> Position {
        let field = |name: &str| {
            item[name]
                .as_str()
                .and_then(|text| text.parse::<u64>().ok())
                .unwrap_or_else(|| panic!("{name} in {item}"))
        };
        let index = |name: &str| {
            u32::try_from(field(name)).unwrap_or_else(|error| panic!("{name}: {error}"))
        };
        at(
            field("height_or_seq"),
            index("tx_index"),
            index("log_index"),
        )
    }

    fn positions(page: &Value) -> Vec<Position> {
        items(page).iter().map(position).collect()
    }

    fn raw(indexer: &Indexer, path: &str, query: &str) -> Value {
        Source {
            indexer,
            client: None,
        }
        .get(path, query)
        .unwrap_or_else(|code| panic!("{code}"))
        .1
    }

    fn refused_code(indexer: &Indexer, cursor: &str) -> Value {
        match history(
            &Source {
                indexer,
                client: None,
            },
            &registry,
            &[("layerx", ALICE.to_owned())],
            &query(Some(cursor), 10, None),
        ) {
            Err(Failure::Refused(body)) => body["error"]["code"].clone(),
            other => panic!("cursor {cursor} answered {other:?}"),
        }
    }

    #[test]
    fn layerx_history_joins_registry_metadata_and_passes_the_indexer_cursor_through() {
        let indexer = indexer();
        let alice = [("layerx", ALICE.to_owned())];
        let first = read(&indexer, &alice, &query(None, 2, None));
        assert_eq!(ids(&first), vec![1, 5]);
        assert_eq!(positions(&first), vec![at(1, 0, 0), at(2, 0, 0)]);
        let upstream = raw(&indexer, &format!("/v1/history/{ALICE}"), "limit=2");
        assert_eq!(first["next_cursor"], upstream["next_cursor"]);
        assert_eq!(
            first["next_cursor"],
            json!("00000000000000020000000000000000")
        );
        assert_eq!(upstream["indexedHeight"], json!(12));
        assert_eq!(upstream["chainHeight"], Value::Null);
        assert_eq!(upstream["freshness"]["version"], json!(1));
        assert_eq!(first["indexedHeight"], json!(12));
        for (item, row) in items(&first).iter().zip(items(&upstream)) {
            let at = position(item);
            assert_eq!(item["blockHeight"], json!(at.height));
            assert_eq!(item["txIndex"], json!(at.tx_index));
            assert_eq!(item["logIndex"], json!(at.log_index));
            let mut stripped = item.clone();
            if let Some(object) = stripped.as_object_mut() {
                for field in ["asset_metadata", "blockHeight", "txIndex", "logIndex"] {
                    object.remove(field);
                }
            }
            assert_eq!(&stripped, row);
        }
        let joined = &first["items"][0]["asset_metadata"];
        assert_eq!(joined["asset"], json!(LXP));
        assert_eq!(joined["chain"], json!("layerx"));
        assert_eq!(joined["kind"], json!("layerx"));
        assert_eq!(joined["symbol"], json!("LXP"));
        assert_eq!(joined["decimals"], json!(6));
        assert_eq!(joined["native_id"], json!(LXP));
        assert_eq!(joined["pointer"], Value::Null);
        assert_eq!(joined["metadata"], json!({"supply": "100"}));
        assert_eq!(first["items"][0]["direction"], json!("out"));
        assert_eq!(first["items"][0]["counterparty"], json!(BOB));
        let cursor = first["next_cursor"].as_str().unwrap_or_default();
        let second = read(&indexer, &alice, &query(Some(cursor), 2, None));
        assert_eq!(ids(&second), vec![8]);
        assert_eq!(positions(&second), vec![at(11, 1, 0)]);
        assert_eq!(second["next_cursor"], Value::Null);
        assert_eq!(second["items"][0]["chain"], json!("paxeer"));
        assert_eq!(second["items"][0]["kind"], json!("custody_deposit"));
        let credits = read(&indexer, &alice, &query(None, 10, Some("lxp_credit")));
        assert_eq!(ids(&credits), vec![5]);
        assert_eq!(credits["items"][0]["amount"], json!("7"));
    }

    #[test]
    fn paxeer_history_joins_native_pointer_denom_and_unknown_assets() {
        let indexer = indexer();
        let eve = [("paxeer", EVE.to_owned())];
        let page = read(&indexer, &eve, &query(None, 50, None));
        assert_eq!(ids(&page), vec![3, 6, 9, 11]);
        assert_eq!(
            positions(&page),
            vec![at(10, 0, 0), at(11, 0, 0), at(12, 0, 0), at(12, 1, 0)]
        );
        assert_eq!(page["next_cursor"], Value::Null);
        let metadata = |index: usize| page["items"][index]["asset_metadata"].clone();
        let native = metadata(0);
        assert_eq!(native["asset"], json!("evm:native"));
        assert_eq!(native["kind"], json!("native"));
        assert_eq!(native["decimals"], json!(18));
        assert_eq!(native["symbol"], Value::Null);
        assert_eq!(native["pointer"], Value::Null);
        assert_eq!(page["items"][0]["amount"], json!("1000000000000000000"));
        assert_eq!(page["items"][0]["direction"], json!("out"));
        let pointer = metadata(1);
        assert_eq!(pointer["asset"], json!(format!("evm:{POINTER}")));
        assert_eq!(pointer["chain"], json!("paxeer"));
        assert_eq!(pointer["kind"], json!("pointer"));
        assert_eq!(pointer["pointer"], json!(POINTER));
        assert_eq!(pointer["denom"], json!("ulxp"));
        assert_eq!(pointer["symbol"], json!("LXP"));
        assert_eq!(pointer["decimals"], json!(6));
        assert_eq!(pointer["native_id"], json!(LXP));
        let ticket = metadata(2);
        assert_eq!(ticket["asset"], json!(TICKET));
        assert_eq!(ticket["kind"], json!("tokenfactory"));
        assert_eq!(ticket["denom"], json!("factory/pax1issuer/ticket"));
        assert_eq!(ticket["symbol"], Value::Null);
        assert_eq!(ticket["decimals"], Value::Null);
        assert_eq!(ticket["native_id"], Value::Null);
        assert_eq!(metadata(3), Value::Null);
        let paged = read(&indexer, &eve, &query(None, 3, None));
        assert_eq!(ids(&paged), vec![3, 6, 9]);
        assert_eq!(
            paged["next_cursor"],
            json!("000000000000000c0000000000000000")
        );
        let rest = read(
            &indexer,
            &eve,
            &query(Some("000000000000000c0000000000000000"), 3, None),
        );
        assert_eq!(ids(&rest), vec![11]);
        assert_eq!(rest["next_cursor"], Value::Null);
    }

    #[test]
    fn paxeer_holder_records_page_by_position_with_indexed_height() {
        let indexer = indexer();
        let budgets = raw(&indexer, &format!("/v1/holders/{EVE}"), "kind=budget");
        let records = items(&budgets);
        assert_eq!(records.len(), 1);
        assert_eq!(records[0]["kind"], json!("budget"));
        assert_eq!(records[0]["contract"], json!("layerxpay"));
        assert_eq!(records[0]["record_id"], json!(BUDGET));
        assert_eq!(records[0]["holder"], json!(EVE));
        assert_eq!(records[0]["state"], json!("active"));
        assert_eq!(
            records[0]["opened"]["cursor"],
            json!("000000000000000c0000000200000001")
        );
        assert_eq!(budgets["next_cursor"], Value::Null);
        assert_eq!(budgets["indexedHeight"], json!(12));
        let grants = raw(&indexer, &format!("/v1/holders/{EVE}"), "kind=grant");
        assert_eq!(items(&grants).len(), 0);
    }

    #[test]
    fn unified_history_merges_both_sides_in_position_order_across_pages() {
        let indexer = indexer();
        let sides = [("paxeer", EVE.to_owned()), ("layerx", ALICE.to_owned())];
        let whole = read(&indexer, &sides, &query(None, 100, None));
        assert_eq!(ids(&whole), vec![1, 5, 3, 6, 8, 9, 11]);
        let order = positions(&whole);
        assert!(order.windows(2).all(|pair| pair[0] < pair[1]), "{order:?}");
        assert_eq!(whole["next_cursor"], Value::Null);
        let tags: Vec<&str> = items(&whole)
            .iter()
            .map(|item| item["side"].as_str().unwrap_or_default())
            .collect();
        assert_eq!(
            tags,
            vec!["layerx", "layerx", "paxeer", "paxeer", "layerx", "paxeer", "paxeer"]
        );
        let chains: Vec<&str> = items(&whole)
            .iter()
            .map(|item| item["chain"].as_str().unwrap_or_default())
            .collect();
        assert_eq!(
            chains,
            vec!["layerx", "layerx", "paxeer", "paxeer", "paxeer", "paxeer", "paxeer"]
        );
        assert!(items(&whole)
            .iter()
            .all(|item| item.get("asset_metadata").is_some()));
        let mut walked = Vec::new();
        let mut cursor: Option<String> = None;
        loop {
            let page = read(&indexer, &sides, &query(cursor.as_deref(), 2, None));
            assert!(ids(&page).len() <= 2);
            walked.extend(ids(&page));
            match page["next_cursor"].as_str() {
                Some(next) => {
                    assert_eq!(
                        Some(next.to_owned()),
                        positions(&page).last().map(Position::encode)
                    );
                    cursor = Some(next.to_owned());
                }
                None => break,
            }
        }
        assert_eq!(walked, ids(&whole));
        let deposits = read(&indexer, &sides, &query(None, 10, Some("custody_deposit")));
        assert_eq!(ids(&deposits), vec![8]);
        assert_eq!(deposits["items"][0]["side"], json!("layerx"));
    }

    /// Every page of `accounts` history at `limit`, walked from the start.
    fn walk(indexer: &Indexer, accounts: &[(&'static str, String)], limit: u64) -> Vec<Vec<u64>> {
        let mut pages = Vec::new();
        let mut cursor: Option<String> = None;
        loop {
            let page = read(indexer, accounts, &query(cursor.as_deref(), limit, None));
            pages.push(ids(&page));
            match page["next_cursor"].as_str() {
                Some(next) => cursor = Some(next.to_owned()),
                None => return pages,
            }
        }
    }

    #[test]
    fn unified_history_keeps_native_before_first_log_on_a_shared_position() {
        let store = Store::open_in_memory().unwrap_or_else(|error| panic!("{error}"));
        let token = "evm:0x0000000000000000000000000000000000000abc";
        let swap = |row: TransferRow| TransferRow {
            tx_id: "swap-20-0".to_owned(),
            ..row
        };
        let units = [
            unit(
                "layerx",
                1,
                vec![
                    leg((1, 0, 0), "lxp_transfer", "out", ALICE, BOB, LXP, "5"),
                    leg((1, 0, 0), "lxp_transfer", "in", BOB, ALICE, LXP, "5"),
                ],
                vec![asset(LXP, "layerx", "layerx", None, None)],
            ),
            unit(
                "layerx",
                2,
                vec![TransferRow {
                    counterparty: None,
                    ..leg((2, 0, 0), "lxp_credit", "in", ALICE, "", LXP, "7")
                }],
                Vec::new(),
            ),
            unit(
                "paxeer",
                19,
                vec![
                    leg(
                        (19, 0, 0),
                        "native_transfer",
                        "out",
                        EVE,
                        FRANK,
                        "evm:native",
                        "1",
                    ),
                    leg(
                        (19, 0, 0),
                        "native_transfer",
                        "in",
                        FRANK,
                        EVE,
                        "evm:native",
                        "1",
                    ),
                    leg((19, 1, 1), "erc20_transfer", "out", FRANK, EVE, token, "4"),
                    leg((19, 1, 1), "erc20_transfer", "in", EVE, FRANK, token, "4"),
                    leg((19, 2, 0), "custody_deposit", "in", ALICE, EVE, LXP, "9"),
                ],
                vec![asset("evm:native", "paxeer", "native", None, None)],
            ),
            unit(
                "paxeer",
                20,
                vec![
                    swap(leg(
                        (20, 0, 0),
                        "native_transfer",
                        "out",
                        EVE,
                        FRANK,
                        "evm:native",
                        "2",
                    )),
                    swap(leg(
                        (20, 0, 0),
                        "native_transfer",
                        "in",
                        FRANK,
                        EVE,
                        "evm:native",
                        "2",
                    )),
                    swap(leg(
                        (20, 0, 1),
                        "erc20_transfer",
                        "out",
                        EVE,
                        FRANK,
                        token,
                        "6",
                    )),
                    swap(leg(
                        (20, 0, 1),
                        "erc20_transfer",
                        "in",
                        FRANK,
                        EVE,
                        token,
                        "6",
                    )),
                ],
                Vec::new(),
            ),
        ];
        for unit in &units {
            store
                .commit(unit, 64)
                .unwrap_or_else(|error| panic!("{error}"));
        }
        let indexer = serve(store);
        let eve = [("paxeer", EVE.to_owned())];
        let sides = [("paxeer", EVE.to_owned()), ("layerx", ALICE.to_owned())];
        let shared = at(20, 0, 0);

        let history = read(&indexer, &eve, &query(None, 100, None));
        assert_eq!(ids(&history), vec![4, 7, 9, 11]);
        assert_eq!(
            positions(&history),
            vec![at(19, 0, 0), at(19, 1, 0), shared, shared]
        );
        let legs: Vec<(&str, &str, &str)> = items(&history)[2..]
            .iter()
            .map(|item| {
                (
                    item["tx_id"].as_str().unwrap_or_default(),
                    item["kind"].as_str().unwrap_or_default(),
                    item["ordinal"].as_str().unwrap_or_default(),
                )
            })
            .collect();
        assert_eq!(
            legs,
            vec![
                ("swap-20-0", "native_transfer", "0"),
                ("swap-20-0", "erc20_transfer", "1")
            ]
        );
        assert_eq!(history["next_cursor"], Value::Null);

        let unified = read(&indexer, &sides, &query(None, 100, None));
        assert_eq!(ids(&unified), vec![1, 3, 4, 7, 8, 9, 11]);
        assert_eq!(
            positions(&unified),
            vec![
                at(1, 0, 0),
                at(2, 0, 0),
                at(19, 0, 0),
                at(19, 1, 0),
                at(19, 2, 0),
                shared,
                shared
            ]
        );
        let tags: Vec<&str> = items(&unified)
            .iter()
            .map(|item| item["side"].as_str().unwrap_or_default())
            .collect();
        assert_eq!(
            tags,
            vec!["layerx", "layerx", "paxeer", "paxeer", "layerx", "paxeer", "paxeer"]
        );
        assert_eq!(unified["next_cursor"], Value::Null);

        let before = at(19, 2, 0).encode();
        for (accounts, cursor) in [(&eve[..], at(19, 1, 0).encode()), (&sides[..], before)] {
            for limit in [1, 2] {
                let page = read(&indexer, accounts, &query(Some(&cursor), limit, None));
                assert_eq!(ids(&page), vec![9, 11], "{accounts:?} {limit}");
                assert_eq!(page["next_cursor"], Value::Null, "{accounts:?} {limit}");
            }
        }

        assert_eq!(walk(&indexer, &eve, 2), vec![vec![4, 7], vec![9, 11]]);
        assert_eq!(
            walk(&indexer, &sides, 2),
            vec![vec![1, 3], vec![4, 7], vec![8, 9, 11]]
        );
        assert_eq!(
            walk(&indexer, &sides, 1),
            vec![vec![1], vec![3], vec![4], vec![7], vec![8], vec![9, 11]]
        );
    }

    #[test]
    fn unknown_account_history_is_an_empty_page() {
        let indexer = indexer();
        for accounts in [
            vec![("layerx", "cd".repeat(32))],
            vec![("paxeer", format!("0x{}", "12".repeat(20)))],
            vec![
                ("paxeer", format!("0x{}", "12".repeat(20))),
                ("layerx", "cd".repeat(32)),
            ],
        ] {
            let page = read(&indexer, &accounts, &query(None, 10, None));
            assert_eq!(
                page,
                json!({"items": [], "next_cursor": null, "indexedHeight": 12})
            );
        }
    }

    #[test]
    fn history_refusals_keep_the_indexer_answer() {
        let indexer = indexer();
        for cursor in [
            "abc",
            "5",
            "000000000000000C0000000000000000",
            "ab".repeat(56).as_str(),
        ] {
            assert_eq!(
                refused_code(&indexer, cursor),
                json!("invalid_cursor"),
                "{cursor}"
            );
        }
        let closed = TcpListener::bind("127.0.0.1:0").unwrap_or_else(|error| panic!("{error}"));
        let address = closed
            .local_addr()
            .unwrap_or_else(|error| panic!("{error}"));
        drop(closed);
        let gone =
            Indexer::parse(&format!("http://{address}")).unwrap_or_else(|error| panic!("{error}"));
        assert_eq!(
            history(
                &Source {
                    indexer: &gone,
                    client: None,
                },
                &registry,
                &[("layerx", ALICE.to_owned())],
                &query(None, 10, None),
            ),
            Err(Failure::Unavailable("indexer_unreachable"))
        );
    }

    #[test]
    fn history_params_accounts_and_endpoints_are_exact() {
        let cursor = at(66, 3, 7).encode();
        assert_eq!(cursor, "00000000000000420000000300000007");
        assert_eq!(
            params(Some(&json!([ALICE]))),
            Ok((ALICE.to_owned(), query(None, DEFAULT_LIMIT, None)))
        );
        assert_eq!(
            params(Some(&json!([EVE, cursor, 7, "erc20_transfer"]))),
            Ok((
                EVE.to_owned(),
                query(Some(&cursor), 7, Some("erc20_transfer"))
            ))
        );
        assert_eq!(
            params(Some(&json!([EVE, null, null, null]))),
            Ok((EVE.to_owned(), query(None, DEFAULT_LIMIT, None)))
        );
        for invalid in [
            json!([]),
            json!({}),
            json!([1]),
            json!([EVE, 5]),
            json!([EVE, ""]),
            json!([EVE, "1&limit=500"]),
            json!([EVE, "x".repeat(33)]),
            json!([EVE, "abc"]),
            json!([EVE, "000000000000000C0000000000000000"]),
            json!([EVE, "0000000000000042000000030000000"]),
            json!([EVE, "ab".repeat(56)]),
            json!([EVE, null, 0]),
            json!([EVE, null, 101]),
            json!([EVE, null, "5"]),
            json!([EVE, null, 5.5]),
            json!([EVE, null, null, "Transfer"]),
            json!([EVE, null, null, ""]),
            json!([EVE, null, null, "a".repeat(65)]),
            json!([EVE, null, null, null, null]),
        ] {
            assert_eq!(params(Some(&invalid)), Err(-32602), "{invalid}");
        }
        assert_eq!(params(None), Err(-32602));
        assert_eq!(
            query(Some(&cursor), 7, Some("erc20_transfer")).encoded(),
            format!("limit=7&cursor={cursor}&kind=erc20_transfer")
        );
        assert_eq!(encode(TICKET), "denom%3Afactory%2Fpax1issuer%2Fticket");
        assert_eq!(
            paxeer_key(&EVE.to_ascii_uppercase().replacen("0X", "0x", 1)),
            Some(EVE.to_owned())
        );
        assert_eq!(paxeer_key(&EVE[2..]), None);
        assert_eq!(paxeer_key(ALICE), None);
        assert!(matches!(
            Indexer::parse("http://127.0.0.1:8480/indexer/"),
            Ok(Indexer::Loopback { ref base_path, .. }) if base_path == "/indexer"
        ));
        assert!(matches!(
            Indexer::parse("http://[::1]:8480"),
            Ok(Indexer::Loopback { .. })
        ));
        assert!(matches!(
            Indexer::parse("https://indexer.internal:8480"),
            Ok(Indexer::Tls(_))
        ));
        for refused in [
            "http://10.0.0.8:8480",
            "http://indexer.internal:8480",
            "http://127.0.0.1",
            "http://127.0.0.1:8480/?x",
            "ftp://indexer.internal",
            "https://10.0.0.8:8480",
        ] {
            assert!(Indexer::parse(refused).is_err(), "{refused}");
        }
    }

    #[test]
    fn history_position_cursor_passes_through_and_the_112_hex_form_is_refused() {
        let indexer = indexer();
        let eve = [("paxeer", EVE.to_owned())];
        let first = read(&indexer, &eve, &query(None, 2, None));
        let cursor = first["next_cursor"]
            .as_str()
            .unwrap_or_else(|| panic!("next cursor in {first}"))
            .to_owned();
        assert_eq!(cursor, at(11, 0, 0).encode());
        let (account, selected) = params(Some(&json!([EVE, cursor, 2])))
            .unwrap_or_else(|code| panic!("position cursor refused: {code}"));
        assert_eq!(selected.cursor.as_deref(), Some(cursor.as_str()));
        assert!(selected.encoded().contains(&format!("cursor={cursor}")));
        let next = read(&indexer, &[("paxeer", account)], &selected);
        let upstream = raw(
            &indexer,
            &format!("/v1/history/{EVE}"),
            &format!("limit=2&cursor={cursor}"),
        );
        assert_eq!(ids(&next), vec![9, 11]);
        assert_eq!(next["next_cursor"], upstream["next_cursor"]);
        assert_eq!(next["indexedHeight"], upstream["indexedHeight"]);
        let order: Vec<(u64, u32, u32)> = items(&next)
            .iter()
            .map(|item| {
                (
                    item["blockHeight"].as_u64().unwrap_or_default(),
                    u32::try_from(item["txIndex"].as_u64().unwrap_or_default()).unwrap_or_default(),
                    u32::try_from(item["logIndex"].as_u64().unwrap_or_default())
                        .unwrap_or_default(),
                )
            })
            .collect();
        assert_eq!(order, vec![(12, 0, 0), (12, 1, 0)]);
        let long = "ab".repeat(56);
        assert_eq!(params(Some(&json!([EVE, long]))), Err(-32602));
        assert_eq!(refused_code(&indexer, &long), json!("invalid_cursor"));
    }

    #[test]
    fn history_dids_resolve_through_addr_before_the_indexer_read() {
        let did = format!("did:layerx:{ALICE}");
        assert_eq!(
            super::super::paxeer::history_selector(&did),
            Ok(super::super::paxeer::Selector::Did([0xaa; 32]))
        );
        assert_eq!(
            super::super::paxeer::history_selector(EVE),
            Ok(super::super::paxeer::Selector::Evm([0xee; 20]))
        );
        for refused in [ALICE, "did:layerx:00", "nope", "did:layerx:", ""] {
            assert_eq!(
                super::super::paxeer::history_selector(refused),
                Err(-32602),
                "{refused}"
            );
        }
        let mut bound = [0_u8; 32];
        bound[12..].copy_from_slice(&[0xee; 20]);
        let address = super::super::paxeer::bound_address(&bound)
            .unwrap_or_else(|code| panic!("bound DID refused: {code}"));
        let key = evm::address_hex(&address);
        assert_eq!(key, EVE);
        let indexer = indexer();
        let resolved = read(&indexer, &[("paxeer", key)], &query(None, 50, None));
        let direct = read(
            &indexer,
            &[("paxeer", EVE.to_owned())],
            &query(None, 50, None),
        );
        assert_eq!(ids(&resolved), vec![3, 6, 9, 11]);
        assert_eq!(resolved, direct);
    }

    #[test]
    fn history_unbound_did_is_invalid_params_and_lx_get_history_is_not_found() {
        assert_eq!(
            super::super::paxeer::bound_address(&[0_u8; 32]),
            Err(super::super::paxeer::UNBOUND_DID)
        );
        let id = json!(4);
        let unbound = super::super::paxeer::history_refusal(&id, super::super::paxeer::UNBOUND_DID);
        assert_eq!(unbound["error"]["code"], json!(-32602));
        assert_eq!(
            unbound["error"]["data"]["code"],
            json!("identity_binding_unavailable")
        );
        assert_eq!(
            super::super::paxeer::history_refusal(&id, "paxeer_unreachable")["error"]["code"],
            json!(-32001)
        );
        assert_eq!(
            super::super::rpc::selector("lx_getHistory", Some(&json!([ALICE]))),
            Err(-32601)
        );
    }

    fn holder(height: u64, holder: &str, kind: &'static str, record: &str) -> HolderRow {
        HolderRow {
            position: at(height, 0, 0),
            holder: holder.to_owned(),
            kind,
            contract: if kind == "budget" {
                "layerxpay"
            } else {
                "ledger"
            },
            record_id: record.to_owned(),
            event: if kind == "budget" {
                "BudgetCreated"
            } else {
                "PayerGrantIssued"
            },
            state: "active",
            tx_id: format!("{kind}-{height}"),
            decoded: json!({}),
        }
    }

    #[test]
    fn px_list_holder_records_pages_the_indexer_holder_route_to_the_end() {
        let store = Store::open_in_memory().unwrap_or_else(|error| panic!("{error}"));
        let grant = |n: u8| format!("{n:02x}").repeat(32);
        let holders = [
            vec![holder(5, EVE, "grant", &grant(0xa1))],
            vec![
                holder(6, EVE, "budget", BUDGET),
                HolderRow {
                    position: at(6, 1, 0),
                    ..holder(6, FRANK, "grant", &grant(0xa9))
                },
            ],
            vec![holder(7, EVE, "grant", &grant(0xa2))],
            vec![holder(8, EVE, "grant", &grant(0xa3))],
        ];
        for (offset, rows) in holders.into_iter().enumerate() {
            let height = 5 + u64::try_from(offset).unwrap_or_else(|error| panic!("{error}"));
            let unit = Unit {
                holders: rows,
                ..unit("paxeer", height, Vec::new(), Vec::new())
            };
            store
                .commit(&unit, 64)
                .unwrap_or_else(|error| panic!("{error}"));
        }
        let indexer = serve(store);
        let source = Source {
            indexer: &indexer,
            client: None,
        };
        let records = |cursor: Option<&str>, limit: u64, kind: &str| {
            holder_records(&source, EVE, &query(cursor, limit, Some(kind)))
                .unwrap_or_else(|failure| panic!("{failure:?}"))
        };

        let first = records(None, 2, "grant");
        let upstream = raw(
            &indexer,
            &format!("/v1/holders/{EVE}"),
            "limit=2&kind=grant",
        );
        assert_eq!(first["items"], upstream["items"]);
        assert_eq!(first["next_cursor"], json!(at(7, 0, 0).encode()));
        assert_eq!(first["indexedHeight"], json!(8));
        assert!(first.get("freshness").is_none());

        let mut walked = Vec::new();
        let mut cursor: Option<String> = None;
        loop {
            let page = records(cursor.as_deref(), 1, "grant");
            assert!(items(&page).len() <= 1);
            for item in items(&page) {
                assert_eq!(item["holder"], json!(EVE));
                assert_eq!(item["kind"], json!("grant"));
                walked.push(item["record_id"].as_str().unwrap_or_default().to_owned());
            }
            match page["next_cursor"].as_str() {
                Some(next) => cursor = Some(next.to_owned()),
                None => break,
            }
        }
        assert_eq!(walked, vec![grant(0xa1), grant(0xa2), grant(0xa3)]);

        let budgets = records(None, 50, "budget");
        assert_eq!(items(&budgets).len(), 1);
        assert_eq!(budgets["items"][0]["record_id"], json!(BUDGET));
        assert_eq!(budgets["next_cursor"], Value::Null);

        let stranger = format!("0x{}", "12".repeat(20));
        assert_eq!(
            holder_records(&source, &stranger, &query(None, 10, Some("grant"))),
            Ok(json!({"items": [], "next_cursor": null, "indexedHeight": 8}))
        );
        assert_eq!(
            holder_records(&source, FRANK, &query(None, 10, Some("grant")))
                .map(|page| items(&page).len()),
            Ok(1)
        );
    }

    #[test]
    fn px_list_holder_records_params_are_exact() {
        let cursor = at(66, 3, 7).encode();
        assert_eq!(
            holder_params(Some(&json!([EVE, "grant"]))),
            Ok((EVE.to_owned(), query(None, DEFAULT_LIMIT, Some("grant"))))
        );
        assert_eq!(
            holder_params(Some(&json!([
                EVE.to_ascii_uppercase().replacen("0X", "0x", 1),
                "budget",
                cursor,
                7
            ]))),
            Ok((EVE.to_owned(), query(Some(&cursor), 7, Some("budget"))))
        );
        assert_eq!(
            holder_params(Some(&json!([EVE, "grant", null, null]))),
            Ok((EVE.to_owned(), query(None, DEFAULT_LIMIT, Some("grant"))))
        );
        for invalid in [
            json!([EVE]),
            json!([EVE, null]),
            json!([EVE, "transfer"]),
            json!([EVE, "Grant"]),
            json!([ALICE, "grant"]),
            json!([&EVE[2..], "grant"]),
            json!([EVE, "grant", "abc"]),
            json!([EVE, "grant", "000000000000000C0000000000000000"]),
            json!([EVE, "grant", "ab".repeat(56)]),
            json!([EVE, "grant", 5]),
            json!([EVE, "grant", null, 0]),
            json!([EVE, "grant", null, 101]),
            json!([EVE, "grant", null, "5"]),
            json!([EVE, "grant", null, null, null]),
            json!({}),
        ] {
            assert_eq!(holder_params(Some(&invalid)), Err(-32602), "{invalid}");
        }
        assert_eq!(holder_params(None), Err(-32602));
    }
}
