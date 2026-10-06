//! Paxeer account history below the node-indexed range, read from the
//! block explorer's REST API (`/api/v2/addresses/{address}/token-transfers`
//! and `/api/v2/addresses/{address}/transactions`).
//!
//! The node walk only reaches back to its start block, so an account's
//! older ERC-20, pointer and native transfers are imported from the
//! explorer the first time the account's history is read. Rows at or above
//! the start block are left to the node walk, so nothing is counted twice.
//! The import is persisted in the store and served after the indexed rows.

use serde_json::Value;

use crate::paxeer::NATIVE_ASSET;
use crate::store::{Store, TransferRow, EXPLORER_SOURCE};
use crate::transport::Endpoint;
use crate::IndexError;

/// The most explorer pages one feed walk reads before giving up.
pub const MAXIMUM_PAGES: usize = 2_000;
const BLOCK_LIMIT: u64 = (1 << 26) - 1;
const INDEX_LIMIT: u64 = 1 << 34;
const ZERO_ADDRESS: &str = "0x0000000000000000000000000000000000000000";

/// The explorer API base plus the first block the node walk covers.
#[derive(Clone, Debug)]
pub struct ExplorerApi {
    pub endpoint: Endpoint,
    pub floor: u64,
}

fn address(value: Option<&Value>) -> Option<String> {
    let text = match value? {
        Value::String(text) => text.as_str(),
        Value::Object(object) => object.get("hash")?.as_str()?,
        _ => return None,
    };
    let lower = text.to_ascii_lowercase();
    (lower.len() == 42
        && lower.starts_with("0x")
        && lower[2..].bytes().all(|byte| byte.is_ascii_hexdigit()))
    .then_some(lower)
}

fn number(value: Option<&Value>) -> Option<u64> {
    match value? {
        Value::Number(number) => number.as_u64(),
        Value::String(text) => text.parse().ok(),
        _ => None,
    }
}

fn decimal(value: Option<&Value>) -> Option<String> {
    let text = value?.as_str()?;
    (!text.is_empty() && text.len() <= 78 && text.bytes().all(|byte| byte.is_ascii_digit())).then(
        || {
            let trimmed = text.trim_start_matches('0');
            if trimmed.is_empty() { "0" } else { trimmed }.to_owned()
        },
    )
}

fn decode_error(what: &str) -> IndexError {
    IndexError::Decode(format!("explorer {what} is malformed"))
}

/// The legs of one transfer `from` -> `to` that belong to `account`.
fn legs(account: &str, from: &str, to: &str) -> Vec<(&'static str, u64, Option<String>)> {
    let mut legs = Vec::new();
    if from == account {
        legs.push(("out", 0, (to != ZERO_ADDRESS).then(|| to.to_owned())));
    }
    if to == account {
        legs.push(("in", 1, (from != ZERO_ADDRESS).then(|| from.to_owned())));
    }
    legs
}

/// Converts one `token-transfers` item into `account`'s keyed rows. Only
/// ERC-20 transfers below `floor` produce rows.
///
/// # Errors
/// Returns [`IndexError::Decode`] when the item is malformed.
pub fn token_transfer_rows(
    store: &Store,
    account: &str,
    floor: u64,
    item: &Value,
) -> Result<Vec<(u64, TransferRow)>, IndexError> {
    let token = item
        .get("token")
        .ok_or_else(|| decode_error("token transfer"))?;
    if token.get("type").and_then(Value::as_str) != Some("ERC-20") {
        return Ok(Vec::new());
    }
    let height = number(item.get("block_number")).ok_or_else(|| decode_error("block number"))?;
    if height >= floor {
        return Ok(Vec::new());
    }
    let log_index = number(item.get("log_index")).ok_or_else(|| decode_error("log index"))?;
    let tx_hash = item
        .get("transaction_hash")
        .or_else(|| item.get("tx_hash"))
        .and_then(Value::as_str)
        .ok_or_else(|| decode_error("transaction hash"))?
        .to_ascii_lowercase();
    let contract = address(token.get("address_hash").or_else(|| token.get("address")))
        .ok_or_else(|| decode_error("token address"))?;
    let from = address(item.get("from")).ok_or_else(|| decode_error("sender"))?;
    let to = address(item.get("to")).ok_or_else(|| decode_error("recipient"))?;
    let amount = decimal(item.get("total").and_then(|total| total.get("value")))
        .ok_or_else(|| decode_error("amount"))?;
    if height >= BLOCK_LIMIT || log_index + 1 >= INDEX_LIMIT {
        return Err(decode_error("position"));
    }
    let kind = if store.is_pointer(&contract)? {
        "pointer_transfer"
    } else {
        "erc20_transfer"
    };
    let ordinal = log_index + 1;
    let decoded = serde_json::json!({
        "block_number": height.to_string(),
        "log_index": log_index.to_string(),
        "contract": contract,
        "from": from,
        "to": to,
        "value": amount,
        "source": EXPLORER_SOURCE,
    });
    Ok(legs(account, &from, &to)
        .into_iter()
        .map(|(direction, bit, counterparty)| {
            (
                (height << 36) | (ordinal << 1) | bit,
                TransferRow {
                    height_or_seq: height,
                    kind: kind.to_owned(),
                    direction,
                    account: account.to_owned(),
                    counterparty,
                    asset: format!("evm:{contract}"),
                    amount: amount.clone(),
                    tx_id: tx_hash.clone(),
                    ordinal,
                    decoded: decoded.clone(),
                },
            )
        })
        .collect())
}

/// Converts one `transactions` item into `account`'s native value rows.
/// Only successful value transfers below `floor` produce rows.
///
/// # Errors
/// Returns [`IndexError::Decode`] when the item is malformed.
pub fn transaction_rows(
    account: &str,
    floor: u64,
    item: &Value,
) -> Result<Vec<(u64, TransferRow)>, IndexError> {
    if item.get("status").and_then(Value::as_str) != Some("ok") {
        return Ok(Vec::new());
    }
    let height = number(item.get("block_number").or_else(|| item.get("block")))
        .ok_or_else(|| decode_error("block number"))?;
    let amount = decimal(item.get("value")).ok_or_else(|| decode_error("value"))?;
    let Some(to) = address(item.get("to")) else {
        return Ok(Vec::new());
    };
    if height >= floor || amount == "0" {
        return Ok(Vec::new());
    }
    let position = number(item.get("position")).ok_or_else(|| decode_error("position"))?;
    let from = address(item.get("from")).ok_or_else(|| decode_error("sender"))?;
    let tx_hash = item
        .get("hash")
        .and_then(Value::as_str)
        .ok_or_else(|| decode_error("transaction hash"))?
        .to_ascii_lowercase();
    if height >= BLOCK_LIMIT || position >= INDEX_LIMIT {
        return Err(decode_error("position"));
    }
    let decoded = serde_json::json!({
        "block_number": height.to_string(),
        "value_wei": amount,
        "source": EXPLORER_SOURCE,
    });
    Ok(legs(account, &from, &to)
        .into_iter()
        .map(|(direction, bit, counterparty)| {
            (
                (height << 36) | (1 << 35) | (position << 1) | bit,
                TransferRow {
                    height_or_seq: height,
                    kind: "native_transfer".to_owned(),
                    direction,
                    account: account.to_owned(),
                    counterparty,
                    asset: NATIVE_ASSET.to_owned(),
                    amount: amount.clone(),
                    tx_id: tx_hash.clone(),
                    ordinal: 0,
                    decoded: decoded.clone(),
                },
            )
        })
        .collect())
}

fn encode(text: &str) -> String {
    use std::fmt::Write as _;
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

fn next_query(document: &Value) -> Result<Option<String>, IndexError> {
    match document.get("next_page_params") {
        None | Some(Value::Null) => Ok(None),
        Some(Value::Object(params)) => {
            let mut pairs = Vec::with_capacity(params.len());
            for (name, value) in params {
                let text = match value {
                    Value::String(text) => text.clone(),
                    Value::Number(number) => number.to_string(),
                    Value::Bool(flag) => flag.to_string(),
                    Value::Null => continue,
                    _ => return Err(decode_error("next page parameters")),
                };
                pairs.push(format!("{}={}", encode(name), encode(&text)));
            }
            Ok(Some(pairs.join("&")))
        }
        Some(_) => Err(decode_error("next page parameters")),
    }
}

impl ExplorerApi {
    fn walk<F>(&self, path: &str, mut each: F) -> Result<(), IndexError>
    where
        F: FnMut(&Value) -> Result<(), IndexError>,
    {
        let mut query: Option<String> = None;
        for _ in 0..MAXIMUM_PAGES {
            let target = query
                .as_ref()
                .map_or_else(|| path.to_owned(), |query| format!("{path}?{query}"));
            let document = self.endpoint.get_json(&target)?;
            for item in document
                .get("items")
                .and_then(Value::as_array)
                .ok_or_else(|| decode_error("page"))?
            {
                each(item)?;
            }
            match next_query(&document)? {
                Some(next) => query = Some(next),
                None => return Ok(()),
            }
        }
        Err(IndexError::Source(format!(
            "explorer feed {path} exceeds {MAXIMUM_PAGES} pages"
        )))
    }

    /// Imports `account`'s explorer history below the floor once; later
    /// calls return immediately. `account` is a lowercase `0x` address.
    ///
    /// # Errors
    /// Returns the explorer or store failure; nothing is marked imported
    /// unless both feeds were read to their end.
    pub fn import(&self, store: &Store, account: &str) -> Result<(), IndexError> {
        if address(Some(&Value::String(account.to_owned()))).as_deref() != Some(account)
            || store.explorer_imported(account)?
        {
            return Ok(());
        }
        let mut rows = Vec::new();
        self.walk(
            &format!("/api/v2/addresses/{account}/token-transfers"),
            |item| {
                rows.extend(token_transfer_rows(store, account, self.floor, item)?);
                Ok(())
            },
        )?;
        self.walk(
            &format!("/api/v2/addresses/{account}/transactions"),
            |item| {
                rows.extend(transaction_rows(account, self.floor, item)?);
                Ok(())
            },
        )?;
        store.import_explorer(account, &rows, true)
    }
}

#[cfg(test)]
mod tests {
    use std::io::{Read as _, Write as _};
    use std::net::TcpListener;
    use std::time::Duration;

    use serde_json::Value;

    use super::ExplorerApi;
    use crate::api::{route_with, Readiness};
    use crate::store::{Store, Unit, EXPLORER_CURSOR};
    use crate::transport::{Endpoint, Security};

    const ACCOUNT: &str = "0x9f2a6c0e3b1d4e5f60718293a4b5c6d7e8f90a1b";
    const TOKEN_PAGE_ONE: &str = include_str!("explorer_fixtures/token_transfers_page1.json");
    const TOKEN_PAGE_TWO: &str = include_str!("explorer_fixtures/token_transfers_page2.json");
    const TRANSACTIONS: &str = include_str!("explorer_fixtures/transactions.json");
    const RELAY: &str = include_str!("../fixtures/relay_archive_batches.json");

    fn explorer_server() -> (String, std::sync::Arc<std::sync::atomic::AtomicUsize>) {
        let listener = TcpListener::bind("127.0.0.1:0").unwrap_or_else(|error| panic!("{error}"));
        let address = listener
            .local_addr()
            .unwrap_or_else(|error| panic!("{error}"));
        let served = std::sync::Arc::new(std::sync::atomic::AtomicUsize::new(0));
        let counter = std::sync::Arc::clone(&served);
        std::thread::spawn(move || {
            for stream in listener.incoming() {
                let Ok(mut stream) = stream else { continue };
                let mut request = Vec::new();
                let mut buffer = [0_u8; 4096];
                while !request.windows(4).any(|window| window == b"\r\n\r\n") {
                    match stream.read(&mut buffer) {
                        Ok(0) | Err(_) => break,
                        Ok(read) => request.extend_from_slice(&buffer[..read]),
                    }
                }
                let text = String::from_utf8_lossy(&request);
                let target = text
                    .split_whitespace()
                    .nth(1)
                    .unwrap_or_default()
                    .to_owned();
                counter.fetch_add(1, std::sync::atomic::Ordering::SeqCst);
                let token = format!("/api/v2/addresses/{ACCOUNT}/token-transfers");
                let body = if target == token {
                    TOKEN_PAGE_ONE
                } else if target == format!("{token}?block_number=1200&index=4") {
                    TOKEN_PAGE_TWO
                } else if target == format!("/api/v2/addresses/{ACCOUNT}/transactions") {
                    TRANSACTIONS
                } else {
                    "{\"message\":\"Not found\"}"
                };
                let status = if body.starts_with("{\"message\"") {
                    "404 Not Found"
                } else {
                    "200 OK"
                };
                let _ = write!(
                    stream,
                    "HTTP/1.1 {status}\r\nContent-Type: application/json\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{body}",
                    body.len()
                );
            }
        });
        (format!("http://{address}"), served)
    }

    fn node_unit() -> Unit {
        let decoded = serde_json::json!({ "block_number": "5000", "value_wei": "7" });
        Unit {
            chain: "paxeer".to_owned(),
            position: 5000,
            hash: "h5000".to_owned(),
            parent: "h4999".to_owned(),
            link: "h5000".to_owned(),
            boundary: 5000,
            transfers: vec![crate::store::TransferRow {
                height_or_seq: 5000,
                kind: "native_transfer".to_owned(),
                direction: "in",
                account: ACCOUNT.to_owned(),
                counterparty: Some("0x1111111111111111111111111111111111111111".to_owned()),
                asset: crate::paxeer::NATIVE_ASSET.to_owned(),
                amount: "7".to_owned(),
                tx_id: "0xnode".to_owned(),
                ordinal: 0,
                decoded,
            }],
            events: Vec::new(),
            assets: Vec::new(),
            accounts: vec![ACCOUNT.to_owned()],
        }
    }

    fn body(response: &layerx_platform_internal::http::Response) -> Value {
        serde_json::from_str(&response.body).unwrap_or_else(|error| panic!("{error}"))
    }

    #[test]
    fn evm_history_merges_node_rows_with_explorer_rows_below_the_floor() {
        let (base, served) = explorer_server();
        let endpoint = Endpoint::parse(
            &base,
            Security::Plaintext {
                allow_remote: false,
            },
            Duration::from_secs(5),
        )
        .unwrap_or_else(|error| panic!("{error}"));
        let store = Store::open_in_memory().unwrap_or_else(|error| panic!("{error}"));
        store
            .commit(&node_unit(), 10)
            .unwrap_or_else(|error| panic!("{error}"));
        let readiness = Readiness {
            explorer: Some(ExplorerApi {
                endpoint,
                floor: 5000,
            }),
            ..Readiness::default()
        };

        let first = route_with(
            &store,
            &readiness,
            "GET",
            &format!("/v1/history/{ACCOUNT}?limit=2"),
        );
        assert_eq!(first.status, 200);
        let first = body(&first);
        let items = first["items"]
            .as_array()
            .unwrap_or_else(|| panic!("no items"));
        assert_eq!(items.len(), 2);
        assert_eq!(items[0]["tx_id"], "0xnode");
        assert_eq!(items[0]["final_basis"], "local_finality_depth");
        assert_eq!(items[1]["height_or_seq"], "1300");
        assert_eq!(items[1]["kind"], "erc20_transfer");
        assert_eq!(items[1]["direction"], "out");
        assert_eq!(items[1]["amount"], "250000000000000000");
        assert_eq!(items[1]["final_basis"], "explorer_api");
        let cursor = first["next_cursor"]
            .as_str()
            .unwrap_or_else(|| panic!("no cursor"))
            .to_owned();
        assert!(cursor.parse::<u64>().unwrap_or_default() > EXPLORER_CURSOR);
        let requests = served.load(std::sync::atomic::Ordering::SeqCst);
        assert_eq!(requests, 3);

        let second = body(&route_with(
            &store,
            &readiness,
            "GET",
            &format!("/v1/history/{ACCOUNT}?limit=2&cursor={cursor}"),
        ));
        let items = second["items"]
            .as_array()
            .unwrap_or_else(|| panic!("no items"));
        let heights: Vec<&str> = items
            .iter()
            .filter_map(|item| item["height_or_seq"].as_str())
            .collect();
        assert_eq!(heights, vec!["1250", "1200"]);
        assert_eq!(items[0]["kind"], "native_transfer");
        assert_eq!(items[0]["direction"], "in");
        assert_eq!(items[1]["direction"], "in");
        assert_eq!(items[1]["counterparty"], Value::Null);
        let cursor = second["next_cursor"]
            .as_str()
            .unwrap_or_else(|| panic!("no cursor"))
            .to_owned();

        let third = body(&route_with(
            &store,
            &readiness,
            "GET",
            &format!("/v1/history/{ACCOUNT}?limit=2&cursor={cursor}"),
        ));
        let items = third["items"]
            .as_array()
            .unwrap_or_else(|| panic!("no items"));
        assert_eq!(items.len(), 1);
        assert_eq!(items[0]["height_or_seq"], "1100");
        assert_eq!(
            items[0]["asset"],
            "evm:0x21f7cf8e59a3f3c3b2c3f0c5e8b5e43e8b1d6a0b"
        );
        assert_eq!(third["next_cursor"], Value::Null);
        assert_eq!(served.load(std::sync::atomic::Ordering::SeqCst), requests);

        let native = body(&route_with(
            &store,
            &readiness,
            "GET",
            &format!("/v1/history/{ACCOUNT}?kind=native_transfer"),
        ));
        let heights: Vec<&str> = native["items"]
            .as_array()
            .unwrap_or_else(|| panic!("no items"))
            .iter()
            .filter_map(|item| item["height_or_seq"].as_str())
            .collect();
        assert_eq!(heights, vec!["5000", "1250"]);
    }

    #[test]
    fn an_unreachable_explorer_refuses_rather_than_serving_partial_history() {
        let listener = TcpListener::bind("127.0.0.1:0").unwrap_or_else(|error| panic!("{error}"));
        let base = format!(
            "http://{}",
            listener
                .local_addr()
                .unwrap_or_else(|error| panic!("{error}"))
        );
        drop(listener);
        let endpoint = Endpoint::parse(
            &base,
            Security::Plaintext {
                allow_remote: false,
            },
            Duration::from_secs(2),
        )
        .unwrap_or_else(|error| panic!("{error}"));
        let store = Store::open_in_memory().unwrap_or_else(|error| panic!("{error}"));
        let readiness = Readiness {
            explorer: Some(ExplorerApi {
                endpoint,
                floor: 5000,
            }),
            ..Readiness::default()
        };
        let response = route_with(&store, &readiness, "GET", &format!("/v1/history/{ACCOUNT}"));
        assert_eq!(response.status, 503);
        assert_eq!(body(&response)["error"]["code"], "explorer_unavailable");
        assert!(!store.explorer_imported(ACCOUNT).unwrap_or(true));
    }

    #[test]
    fn kernel_receipts_from_the_history_route_page_per_account() {
        let relay: Value = serde_json::from_str(RELAY).unwrap_or_else(|error| panic!("{error}"));
        let store = Store::open_in_memory().unwrap_or_else(|error| panic!("{error}"));
        let batches = relay["canonical"]["batches"]
            .as_object()
            .unwrap_or_else(|| panic!("no batches"));
        let mut numbers: Vec<u64> = batches.keys().filter_map(|key| key.parse().ok()).collect();
        numbers.sort_unstable();
        let mut account = None;
        for number in numbers {
            let unit = crate::layerx::decode_batch(&batches[&number.to_string()])
                .unwrap_or_else(|error| panic!("{error}"));
            if account.is_none() {
                account = unit.transfers.first().map(|row| row.account.clone());
            }
            store
                .commit(&unit, 1)
                .unwrap_or_else(|error| panic!("{error}"));
        }
        let account = account.unwrap_or_else(|| panic!("the kernel fixture carries no transfer"));
        let all = store
            .merged_history(&account, None, 500, None)
            .unwrap_or_else(|error| panic!("{error}"));
        assert!(!all.items.is_empty());
        assert!(all
            .items
            .iter()
            .all(|item| item["chain"] == "layerx" && item["account"] == account.as_str()));
        let response = route_with(
            &store,
            &Readiness::default(),
            "GET",
            &format!("/v1/history/{account}?limit=1"),
        );
        assert_eq!(response.status, 200);
        let page = body(&response);
        assert_eq!(page["items"][0], all.items[0]);
        assert_eq!(page["next_cursor"].is_string(), all.items.len() > 1);
    }
}
