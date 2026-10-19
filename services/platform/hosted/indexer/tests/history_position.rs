use std::collections::{BTreeMap, HashSet};

use layerx_indexer::abi::{keccak, AbiRegistry};
use layerx_indexer::api;
use layerx_indexer::codec::hex;
use layerx_indexer::paxeer::{self, AttributeEncoding, BlockInputs};
use layerx_indexer::store::{Position, Store};
use serde_json::{json, Value};

const A: &str = "0x00000000000000000000000000000000000000a1";
const B: &str = "0x00000000000000000000000000000000000000b2";
const C: &str = "0x00000000000000000000000000000000000000c3";
const TOKEN: &str = "0x00000000000000000000000000000000000000d4";
const LEDGER: &str = "0x000000000000000000000000000000000000101a";
const PAY: &str = "0x000000000000000000000000000000000000101b";
const AUTH: &str = "0x000000000000000000000000000000000000101c";

fn word(value: u64) -> String {
    format!("{value:064x}")
}

fn hash(tag: u64) -> String {
    format!("0x{tag:064x}")
}

fn padded(address: &str) -> String {
    format!("0x{:0>64}", &address[2..])
}

fn topic(signature: &str) -> String {
    format!("0x{}", hex(&keccak(signature.as_bytes())))
}

fn id(tag: u64) -> String {
    format!("0x{:064x}", 0xb0d6_0000_u64 + tag)
}

/// One log of transaction `tx_index` at `log_index` in its block.
fn log(address: &str, log_index: u64, topics: Vec<String>, data: &str) -> Value {
    json!({
        "address": address,
        "logIndex": format!("0x{log_index:x}"),
        "topics": topics,
        "data": format!("0x{data}"),
        "removed": false,
    })
}

fn transfer_log(log_index: u64, from: &str, to: &str, amount: u64) -> Value {
    log(
        TOKEN,
        log_index,
        vec![
            topic("Transfer(address,address,uint256)"),
            padded(from),
            padded(to),
        ],
        &word(amount),
    )
}

/// A transaction from `from` to `to` carrying `value` wei and `logs`.
struct Tx {
    from: &'static str,
    to: &'static str,
    value: u64,
    logs: Vec<Value>,
}

/// Decodes and commits one block of `transactions` through the Paxeer
/// block decoder.
fn commit_block(store: &Store, height: u64, transactions: Vec<Tx>) {
    let block_hash = hash(0xb10c_0000_0000 + height);
    let mut objects = Vec::new();
    let mut receipts = BTreeMap::new();
    for (index, tx) in transactions.into_iter().enumerate() {
        let index = index as u64;
        let tx_hash = hash((height << 16) + index);
        objects.push(json!({
            "hash": tx_hash,
            "from": tx.from,
            "to": tx.to,
            "value": format!("0x{:x}", tx.value),
            "transactionIndex": format!("0x{index:x}"),
        }));
        let logs: Vec<Value> = tx
            .logs
            .into_iter()
            .map(|mut entry| {
                entry["transactionIndex"] = json!(format!("0x{index:x}"));
                entry
            })
            .collect();
        receipts.insert(
            tx_hash,
            json!({ "blockHash": block_hash, "status": "0x1", "logs": logs }),
        );
    }
    let block = json!({
        "number": format!("0x{height:x}"),
        "hash": block_hash,
        "parentHash": hash(0xb10c_0000_0000 + height - 1),
        "transactions": objects,
    });
    let registry = AbiRegistry::default();
    let is_pointer = |_: &str| Ok(false);
    let inputs = BlockInputs {
        registry: &registry,
        receipts: &receipts,
        cosmos_txs: &[],
        encoding: AttributeEncoding::Base64,
        is_pointer: &is_pointer,
    };
    let unit = paxeer::decode_block(&block, &inputs).unwrap_or_else(|error| panic!("{error}"));
    store
        .commit(&unit, 2)
        .unwrap_or_else(|error| panic!("{error}"));
}

fn get(store: &Store, target: &str) -> (u16, Value) {
    let response = api::route(store, "GET", target);
    let body = serde_json::from_str(&response.body).unwrap_or_else(|error| panic!("{error}"));
    (response.status, body)
}

fn items(page: &Value) -> Vec<Value> {
    page["items"]
        .as_array()
        .unwrap_or_else(|| panic!("no items in {page}"))
        .clone()
}

fn triple(item: &Value) -> (u64, u32, u32) {
    let field = |name: &str| {
        item[name]
            .as_str()
            .and_then(|text| text.parse::<u64>().ok())
            .unwrap_or_else(|| panic!("{name} missing in {item}"))
    };
    (
        field("height_or_seq"),
        u32::try_from(field("tx_index")).unwrap_or(u32::MAX),
        u32::try_from(field("log_index")).unwrap_or(u32::MAX),
    )
}

fn store() -> Store {
    Store::open_in_memory().unwrap_or_else(|error| panic!("{error}"))
}

#[test]
fn history_orders_by_block_position_and_pages_strictly_after_the_cursor() {
    let store = store();
    commit_block(
        &store,
        10,
        vec![Tx {
            from: A,
            to: B,
            value: 7,
            logs: Vec::new(),
        }],
    );
    commit_block(
        &store,
        12,
        vec![Tx {
            from: C,
            to: TOKEN,
            value: 0,
            logs: vec![transfer_log(3, C, A, 6), transfer_log(0, C, A, 5)],
        }],
    );

    let (status, first) = get(&store, &format!("/v1/history/{A}?limit=2"));
    assert_eq!(status, 200);
    let page = items(&first);
    assert_eq!(page.len(), 2);
    assert_eq!(triple(&page[0]), (10, 0, 0));
    assert_eq!(page[0]["kind"], "native_transfer");
    assert_eq!(page[0]["direction"], "out");
    assert_eq!(page[0]["amount"], "7");
    assert_eq!(triple(&page[1]), (12, 0, 0));
    assert_eq!(page[1]["kind"], "erc20_transfer");
    assert_eq!(page[1]["amount"], "5");
    let cursor = first["next_cursor"]
        .as_str()
        .unwrap_or_else(|| panic!("no cursor"))
        .to_owned();
    assert_eq!(cursor, "000000000000000c0000000000000000");
    assert_eq!(
        Position::parse(&cursor),
        Some(Position {
            height: 12,
            tx_index: 0,
            log_index: 0
        })
    );

    let (status, second) = get(&store, &format!("/v1/history/{A}?limit=2&cursor={cursor}"));
    assert_eq!(status, 200);
    let page = items(&second);
    assert_eq!(page.len(), 1);
    assert_eq!(triple(&page[0]), (12, 0, 3));
    assert_eq!(page[0]["amount"], "6");
    assert_eq!(second["next_cursor"], Value::Null);
}

#[test]
fn rows_in_one_block_page_in_triple_order_without_repeat_or_gap() {
    let store = store();
    commit_block(
        &store,
        40,
        vec![
            Tx {
                from: A,
                to: TOKEN,
                value: 3,
                logs: vec![transfer_log(0, A, B, 11), transfer_log(1, C, A, 12)],
            },
            Tx {
                from: A,
                to: A,
                value: 9,
                logs: Vec::new(),
            },
            Tx {
                from: B,
                to: TOKEN,
                value: 0,
                logs: vec![
                    transfer_log(2, B, A, 13),
                    transfer_log(3, A, A, 14),
                    transfer_log(4, B, C, 15),
                ],
            },
            Tx {
                from: C,
                to: A,
                value: 4,
                logs: vec![transfer_log(5, C, A, 16)],
            },
        ],
    );

    let all = store
        .history(A, None, 500, None)
        .unwrap_or_else(|error| panic!("{error}"));
    let expected: Vec<((u64, u32, u32), String)> = all
        .items
        .iter()
        .map(|item| {
            (
                triple(item),
                item["direction"].as_str().unwrap_or_default().to_owned(),
            )
        })
        .collect();
    assert_eq!(
        expected,
        vec![
            ((40, 0, 0), "out".to_owned()),
            ((40, 0, 0), "out".to_owned()),
            ((40, 0, 1), "in".to_owned()),
            ((40, 1, 0), "out".to_owned()),
            ((40, 1, 0), "in".to_owned()),
            ((40, 2, 2), "in".to_owned()),
            ((40, 2, 3), "out".to_owned()),
            ((40, 2, 3), "in".to_owned()),
            ((40, 3, 0), "in".to_owned()),
            ((40, 3, 5), "in".to_owned()),
        ]
    );
    assert_eq!(all.items[0]["kind"], "native_transfer");
    assert_eq!(all.items[1]["kind"], "erc20_transfer");
    assert_eq!(all.next_cursor, None);

    for limit in 1..=4 {
        let mut seen = Vec::new();
        let mut cursor: Option<String> = None;
        let mut previous_last: Option<(u64, u32, u32)> = None;
        loop {
            let target = cursor.as_ref().map_or_else(
                || format!("/v1/history/{A}?limit={limit}"),
                |cursor| format!("/v1/history/{A}?limit={limit}&cursor={cursor}"),
            );
            let (status, page) = get(&store, &target);
            assert_eq!(status, 200);
            let rows = items(&page);
            assert!(!rows.is_empty(), "limit {limit} served an empty page");
            if let Some(last) = previous_last {
                assert!(triple(&rows[0]) > last, "limit {limit} repeated a position");
            }
            previous_last = rows.last().map(triple);
            seen.extend(rows.iter().cloned());
            match page["next_cursor"].as_str() {
                Some(next) => {
                    let position =
                        Position::parse(next).unwrap_or_else(|| panic!("bad cursor {next}"));
                    let last = previous_last.unwrap_or_default();
                    assert_eq!(
                        (position.height, position.tx_index, position.log_index),
                        last
                    );
                    cursor = Some(next.to_owned());
                }
                None => break,
            }
        }
        assert_eq!(seen, all.items, "limit {limit} paged differently");
    }
}

#[test]
fn a_page_of_fifty_reads_one_hundred_twenty_transactions_in_three_pages() {
    let store = store();
    commit_block(
        &store,
        50,
        (0..120)
            .map(|index| Tx {
                from: A,
                to: B,
                value: index + 1,
                logs: Vec::new(),
            })
            .collect(),
    );
    let mut cursor: Option<String> = None;
    let mut sizes = Vec::new();
    let mut transactions = HashSet::new();
    loop {
        let target = cursor.as_ref().map_or_else(
            || format!("/v1/history/{A}?limit=50"),
            |cursor| format!("/v1/history/{A}?limit=50&cursor={cursor}"),
        );
        let (status, page) = get(&store, &target);
        assert_eq!(status, 200);
        let rows = items(&page);
        sizes.push(rows.len());
        for row in &rows {
            assert!(transactions.insert(row["tx_id"].as_str().unwrap_or_default().to_owned()));
        }
        match page["next_cursor"].as_str() {
            Some(next) => cursor = Some(next.to_owned()),
            None => break,
        }
    }
    assert_eq!(sizes, vec![50, 50, 20]);
    assert_eq!(transactions.len(), 120);
}

#[test]
fn any_other_cursor_form_is_refused_with_invalid_cursor() {
    let store = store();
    commit_block(
        &store,
        10,
        vec![Tx {
            from: A,
            to: B,
            value: 7,
            logs: Vec::new(),
        }],
    );
    let major_one = "ab".repeat(56);
    assert_eq!(major_one.len(), 112);
    for cursor in [
        major_one.as_str(),
        "000000000000000C0000000000000000",
        "000000000000000c000000000000000",
        "000000000000000c00000000000000000",
        "0x0000000000000c0000000000000000",
        "8000000000000000000000000000000000",
        "80000000000000000000000000000000",
        "5",
        "",
    ] {
        for route in ["history", "holders"] {
            let (status, body) = get(&store, &format!("/v1/{route}/{A}?cursor={cursor}"));
            assert_eq!(status, 400, "{route} accepted cursor {cursor:?}");
            assert_eq!(
                body["error"]["code"], "invalid_cursor",
                "{route} {cursor:?}"
            );
        }
    }
    let (status, body) = get(
        &store,
        &format!("/v1/history/{A}?cursor=00000000000000090000000000000000"),
    );
    assert_eq!(status, 200);
    assert_eq!(items(&body).len(), 1);
    let (status, body) = get(
        &store,
        &format!("/v1/history/{A}?cursor=000000000000000a0000000000000000"),
    );
    assert_eq!(status, 200);
    assert!(items(&body).is_empty());
}

fn budget_created(log_index: u64, budget: u64) -> Value {
    log(
        PAY,
        log_index,
        vec![topic("BudgetCreated(bytes32,bytes32,uint128)"), id(budget)],
        &format!("{}{}", word(0xa55e7), word(500)),
    )
}

fn budget_ended(event: &str, log_index: u64, budget: u64) -> Value {
    log(
        PAY,
        log_index,
        vec![
            topic(&format!("{event}(bytes32,uint128,uint64)")),
            id(budget),
        ],
        &format!("{}{}", word(120), word(9)),
    )
}

fn grant_body(log_index: u64, grant: u64, chunk: u64) -> Value {
    log(
        AUTH,
        log_index,
        vec![topic("GrantBody(bytes32,uint8,uint8,bytes)"), id(grant)],
        &format!(
            "{}{}{}{}{}",
            word(1),
            word(chunk),
            word(0x60),
            word(2),
            format!("{:0<64}", "beef")
        ),
    )
}

#[test]
fn holder_records_carry_their_latest_state_by_holder() {
    let store = store();
    commit_block(
        &store,
        20,
        vec![
            Tx {
                from: A,
                to: PAY,
                value: 0,
                logs: vec![budget_created(0, 1), budget_created(1, 2)],
            },
            Tx {
                from: A,
                to: LEDGER,
                value: 0,
                logs: vec![log(
                    LEDGER,
                    2,
                    vec![topic("PayerGrantIssued(bytes32)"), id(3)],
                    "",
                )],
            },
            Tx {
                from: A,
                to: AUTH,
                value: 0,
                logs: vec![grant_body(3, 4, 0), grant_body(4, 4, 1)],
            },
            Tx {
                from: B,
                to: PAY,
                value: 0,
                logs: vec![budget_created(5, 5)],
            },
        ],
    );
    commit_block(
        &store,
        21,
        vec![Tx {
            from: A,
            to: PAY,
            value: 0,
            logs: vec![budget_ended("BudgetRevoked", 0, 1)],
        }],
    );
    commit_block(
        &store,
        22,
        vec![Tx {
            from: A,
            to: AUTH,
            value: 0,
            logs: vec![log(
                AUTH,
                0,
                vec![topic("GrantRevoked(bytes32,bytes)"), id(4)],
                &format!("{}{}{}", word(0x20), word(1), format!("{:0<64}", "01")),
            )],
        }],
    );
    store
        .record_source_head("paxeer", Some(30))
        .unwrap_or_else(|error| panic!("{error}"));

    let (status, budgets) = get(&store, &format!("/v1/holders/{A}?kind=budget"));
    assert_eq!(status, 200);
    let records = items(&budgets);
    assert_eq!(records.len(), 2);
    let bare = |tag: u64| id(tag)[2..].to_owned();
    assert_eq!(records[0]["record_id"], bare(1));
    assert_eq!(records[0]["kind"], "budget");
    assert_eq!(records[0]["contract"], "layerxpay");
    assert_eq!(records[0]["holder"], A);
    assert_eq!(records[0]["state"], "revoked");
    assert_eq!(records[0]["opened"]["event"], "BudgetCreated");
    assert_eq!(records[0]["opened"]["height"], "20");
    assert_eq!(records[0]["opened"]["decoded"]["args"]["amount"], "500");
    assert_eq!(records[0]["latest"]["event"], "BudgetRevoked");
    assert_eq!(records[0]["latest"]["height"], "21");
    assert_eq!(records[0]["latest"]["decoded"]["args"]["drained"], "120");
    assert_eq!(records[1]["record_id"], bare(2));
    assert_eq!(records[1]["state"], "active");
    assert_eq!(budgets["next_cursor"], Value::Null);
    assert_eq!(budgets["indexedHeight"], 22);
    assert_eq!(budgets["chainHeight"], 30);

    let (status, first) = get(&store, &format!("/v1/holders/{A}?kind=budget&limit=1"));
    assert_eq!(status, 200);
    assert_eq!(items(&first).len(), 1);
    assert_eq!(first["items"][0]["record_id"], bare(1));
    let cursor = first["next_cursor"]
        .as_str()
        .unwrap_or_else(|| panic!("no cursor"))
        .to_owned();
    assert_eq!(cursor, "00000000000000140000000000000000");
    let (status, second) = get(
        &store,
        &format!("/v1/holders/{A}?kind=budget&limit=1&cursor={cursor}"),
    );
    assert_eq!(status, 200);
    assert_eq!(items(&second).len(), 1);
    assert_eq!(second["items"][0]["record_id"], bare(2));
    assert_eq!(second["next_cursor"], Value::Null);

    let (status, grants) = get(&store, &format!("/v1/holders/{A}?kind=grant"));
    assert_eq!(status, 200);
    let records = items(&grants);
    let summary: Vec<(String, String, String)> = records
        .iter()
        .map(|record| {
            (
                record["contract"].as_str().unwrap_or_default().to_owned(),
                record["record_id"].as_str().unwrap_or_default().to_owned(),
                record["state"].as_str().unwrap_or_default().to_owned(),
            )
        })
        .collect();
    assert_eq!(
        summary,
        vec![
            ("layerxledger".to_owned(), bare(3), "active".to_owned()),
            ("layerxauth".to_owned(), bare(4), "revoked".to_owned()),
        ]
    );
    assert_eq!(records[1]["opened"]["event"], "GrantBody");
    assert_eq!(records[1]["latest"]["event"], "GrantRevoked");

    let (status, all) = get(
        &store,
        &format!("/v1/holders/{}", A.to_uppercase().replacen("0X", "0x", 1)),
    );
    assert_eq!(status, 200);
    assert_eq!(items(&all).len(), 4);
    let (status, other) = get(&store, &format!("/v1/holders/{B}"));
    assert_eq!(status, 200);
    assert_eq!(items(&other).len(), 1);
    assert_eq!(other["items"][0]["record_id"], bare(5));

    let (status, body) = get(&store, &format!("/v1/holders/{A}?kind=native_transfer"));
    assert_eq!(status, 400);
    assert_eq!(body["error"]["code"], "invalid_kind");
}

#[test]
fn freshness_carries_the_indexed_and_chain_heights() {
    let store = store();
    commit_block(
        &store,
        990,
        vec![Tx {
            from: A,
            to: B,
            value: 1,
            logs: Vec::new(),
        }],
    );
    store
        .record_source_head("paxeer", Some(1000))
        .unwrap_or_else(|error| panic!("{error}"));
    for target in [format!("/v1/history/{A}"), format!("/v1/holders/{A}")] {
        let (status, page) = get(&store, &target);
        assert_eq!(status, 200, "{target}");
        assert_eq!(page["indexedHeight"], 990, "{target}");
        assert_eq!(page["chainHeight"], 1000, "{target}");
        assert_eq!(page["version"], 1, "{target}");
        assert!(page["freshness"].is_object(), "{target}");
    }
}
