//! Drives the ingester's real HTTP JSON-RPC client, decoder, journal and
//! cursor against a loopback node that replays recorded precompile logs built
//! from the `layerx-intents` precompile ABI vectors.

use std::fmt::Debug;
use std::fs;
use std::io::{BufRead as _, BufReader, Read as _, Write as _};
use std::net::TcpListener;
use std::path::PathBuf;
use std::sync::{Arc, Mutex};
use std::time::Duration;

use layerx_intent_ingester::{
    hex, readyz_body, Config, HttpRpc, IngestError, Ingester, Journal, WindowAlert,
};
use layerx_intents::precompile::{
    keccak256, EvmLog, PrecompileEvent, PrecompileEventKind, BRIDGE_PRECOMPILE,
    EXCHANGE_PRECOMPILE, LAUNCHPAD_PRECOMPILE,
};
use serde_json::{json, Value};

fn checked<T, E: Debug>(value: Result<T, E>) -> T {
    value.unwrap_or_else(|error| panic!("{error:?}"))
}

fn word_u64(value: u64) -> [u8; 32] {
    word_u128(u128::from(value))
}

fn word_u128(value: u128) -> [u8; 32] {
    let mut word = [0; 32];
    word[16..].copy_from_slice(&value.to_be_bytes());
    word
}

fn word_address(value: [u8; 20]) -> [u8; 32] {
    let mut word = [0; 32];
    word[12..].copy_from_slice(&value);
    word
}

#[derive(Clone)]
struct RecordedLog {
    block: u64,
    index: u64,
    tx: [u8; 32],
    address: [u8; 20],
    topics: Vec<[u8; 32]>,
    data: Vec<u8>,
}

impl RecordedLog {
    fn json(&self) -> Value {
        json!({
            "address": hex(&self.address),
            "topics": self.topics.iter().map(|topic| hex(topic)).collect::<Vec<_>>(),
            "data": hex(&self.data),
            "blockNumber": format!("0x{:x}", self.block),
            "transactionHash": hex(&self.tx),
            "logIndex": format!("0x{:x}", self.index),
            "removed": false,
        })
    }

    fn decoded(&self) -> PrecompileEvent {
        checked(PrecompileEvent::decode(&EvmLog {
            address: self.address,
            topics: &self.topics,
            data: &self.data,
        }))
    }

    fn tx_key(&self) -> [u8; 32] {
        let mut preimage = self.tx.to_vec();
        preimage.extend_from_slice(&self.index.to_be_bytes());
        keccak256(&preimage)
    }
}

fn log(
    block: u64,
    index: u64,
    address: [u8; 20],
    topics: Vec<[u8; 32]>,
    words: &[[u8; 32]],
) -> RecordedLog {
    let mut tx = [0; 32];
    tx[..8].copy_from_slice(&block.to_be_bytes());
    tx[8..16].copy_from_slice(&index.to_be_bytes());
    tx[31] = 0x7a;
    RecordedLog {
        block,
        index,
        tx,
        address,
        topics,
        data: words.concat(),
    }
}

/// One log for each exchange event plus a bridge and a launchpad event, in
/// the shapes the precompile ABIs declare.
fn vectors() -> Vec<RecordedLog> {
    let owner = word_address([0xab; 20]);
    vec![
        log(
            10,
            0,
            EXCHANGE_PRECOMPILE,
            vec![
                PrecompileEventKind::OrderPlaced.topic0(),
                [0x31; 32],
                [0x11; 32],
                owner,
            ],
            &[
                word_u64(2),
                word_u128((1_u128 << 64) | 2),
                word_u128(5000),
                word_u64(0),
                word_u64(9),
            ],
        ),
        log(
            10,
            1,
            EXCHANGE_PRECOMPILE,
            vec![
                PrecompileEventKind::OrderCancelRequested.topic0(),
                [0x41; 32],
                [0x31; 32],
                owner,
            ],
            &[word_u64(10)],
        ),
        log(
            11,
            0,
            EXCHANGE_PRECOMPILE,
            vec![
                PrecompileEventKind::SettlementRequested.topic0(),
                [0x51; 32],
                [0x52; 32],
                owner,
            ],
            &[word_u64(11)],
        ),
        log(
            12,
            0,
            EXCHANGE_PRECOMPILE,
            vec![
                PrecompileEventKind::MarginDeposited.topic0(),
                [0x61; 32],
                [0x62; 32],
                owner,
            ],
            &[[0x63; 32], word_u128(700), [0x64; 32], word_u64(12)],
        ),
        log(
            12,
            3,
            EXCHANGE_PRECOMPILE,
            vec![
                PrecompileEventKind::MarginWithdrawalRequested.topic0(),
                [0x71; 32],
                [0x62; 32],
                owner,
            ],
            &[[0x63; 32], word_u128(300), word_u64(13)],
        ),
        log(
            13,
            0,
            BRIDGE_PRECOMPILE,
            vec![
                PrecompileEventKind::BridgeOut.topic0(),
                word_u64(1),
                word_address([0x0e; 20]),
                word_u64(4),
            ],
            &[word_u128(900), word_address([0x0f; 20])],
        ),
        log(
            14,
            0,
            LAUNCHPAD_PRECOMPILE,
            vec![
                PrecompileEventKind::FeesBurned.topic0(),
                word_address([0x0c; 20]),
            ],
            &[word_u128(55)],
        ),
    ]
}

struct NodeState {
    head: u64,
    logs: Vec<RecordedLog>,
    /// Serve every matching log twice, as a re-orged or retried node would.
    duplicate: bool,
    ranges: Vec<(u64, u64)>,
}

/// Loopback JSON-RPC node replaying recorded logs over real HTTP.
struct Node {
    url: String,
    state: Arc<Mutex<NodeState>>,
}

fn quantity(value: &Value) -> u64 {
    let text = value.as_str().unwrap_or_else(|| panic!("quantity {value}"));
    checked(u64::from_str_radix(text.trim_start_matches("0x"), 16))
}

fn answer(state: &Mutex<NodeState>, request: &Value) -> Value {
    let mut state = checked(state.lock());
    let method = request["method"].as_str().unwrap_or_default();
    let result = match method {
        "eth_blockNumber" => json!(format!("0x{:x}", state.head)),
        "eth_getLogs" => {
            let filter = &request["params"][0];
            let from = quantity(&filter["fromBlock"]);
            let to = quantity(&filter["toBlock"]);
            let addresses: Vec<String> = filter["address"]
                .as_array()
                .unwrap_or_else(|| panic!("address filter"))
                .iter()
                .map(|value| value.as_str().unwrap_or_default().to_owned())
                .collect();
            assert_eq!(
                addresses,
                vec![
                    hex(&EXCHANGE_PRECOMPILE),
                    hex(&BRIDGE_PRECOMPILE),
                    hex(&LAUNCHPAD_PRECOMPILE)
                ]
            );
            state.ranges.push((from, to));
            let copies = if state.duplicate { 2 } else { 1 };
            let mut out = Vec::new();
            for recorded in &state.logs {
                if (from..=to).contains(&recorded.block) {
                    for _ in 0..copies {
                        out.push(recorded.json());
                    }
                }
            }
            Value::Array(out)
        }
        other => {
            return json!({"jsonrpc": "2.0", "id": request["id"], "error": {"code": -32601, "message": other}})
        }
    };
    json!({"jsonrpc": "2.0", "id": request["id"], "result": result})
}

impl Node {
    fn start(head: u64, logs: Vec<RecordedLog>) -> Self {
        let listener = checked(TcpListener::bind("127.0.0.1:0"));
        let url = format!("http://{}/rpc", checked(listener.local_addr()));
        let state = Arc::new(Mutex::new(NodeState {
            head,
            logs,
            duplicate: false,
            ranges: Vec::new(),
        }));
        let served = Arc::clone(&state);
        std::thread::spawn(move || {
            for stream in listener.incoming() {
                let Ok(stream) = stream else { continue };
                let mut reader = BufReader::new(checked(stream.try_clone()));
                let mut length = 0;
                loop {
                    let mut line = String::new();
                    checked(reader.read_line(&mut line));
                    let line = line.trim_end();
                    if line.is_empty() {
                        break;
                    }
                    if let Some(value) = line.to_ascii_lowercase().strip_prefix("content-length:") {
                        length = checked(value.trim().parse::<usize>());
                    }
                }
                let mut body = vec![0; length];
                checked(reader.read_exact(&mut body));
                let request: Value = checked(serde_json::from_slice(&body));
                let reply = answer(&served, &request).to_string();
                let mut stream = stream;
                checked(stream.write_all(
                    format!(
                        "HTTP/1.0 200 OK\r\nContent-Type: application/json\r\nContent-Length: {}\r\n\r\n{reply}",
                        reply.len()
                    )
                    .as_bytes(),
                ));
            }
        });
        Self { url, state }
    }

    fn rpc(&self) -> HttpRpc {
        checked(HttpRpc::new(&self.url, Duration::from_secs(5)))
    }

    fn set(&self, update: impl FnOnce(&mut NodeState)) {
        let mut guard = checked(self.state.lock());
        update(&mut guard);
    }

    fn ranges(&self) -> Vec<(u64, u64)> {
        checked(self.state.lock()).ranges.clone()
    }
}

fn state_dir(name: &str) -> PathBuf {
    let dir = std::env::temp_dir().join(format!(
        "layerx-intent-ingester-{name}-{}",
        std::process::id()
    ));
    let _ = fs::remove_dir_all(&dir);
    dir
}

fn config(dir: &PathBuf) -> Config {
    let mut config = Config::new(dir.clone());
    config.start_block = Some(0);
    config
}

#[test]
fn replay_decodes_every_vector_and_journals_it_by_intent_id() {
    let recorded = vectors();
    let node = Node::start(20, recorded.clone());
    let dir = state_dir("replay");
    let mut ingester = checked(Ingester::open(node.rpc(), &config(&dir)));
    let fresh = checked(ingester.poll());
    assert_eq!(fresh.len(), recorded.len());
    for (intent, source) in fresh.iter().zip(&recorded) {
        assert_eq!(intent.event, source.decoded());
        assert_eq!(intent.block_number, source.block);
        assert_eq!(intent.log_index, source.index);
        assert_eq!(intent.tx_hash, source.tx);
        let expected = if source.address == EXCHANGE_PRECOMPILE {
            source.topics[1]
        } else {
            source.tx_key()
        };
        assert_eq!(intent.id, expected);
    }
    assert!(matches!(fresh[0].event, PrecompileEvent::OrderPlaced(_)));
    assert!(matches!(fresh[5].event, PrecompileEvent::BridgeOut(_)));
    assert!(matches!(fresh[6].event, PrecompileEvent::FeesBurned(_)));
    assert_eq!(
        checked(Journal::read_all(&dir.join("journal.jsonl"))),
        fresh
    );
    let status = ingester.status();
    assert_eq!((status.cursor, status.head, status.lag), (21, 20, 0));
    assert_eq!(status.journaled, recorded.len());
    assert_eq!(checked(fs::read_to_string(dir.join("cursor"))), "21");
    assert!(checked(ingester.poll()).is_empty());
    let body: Value = checked(serde_json::from_str(&readyz_body(&status, None)));
    assert_eq!(body["cursor"], 21);
    assert_eq!(body["lag"], 0);
    assert_eq!(body["ready"], true);
}

#[test]
fn cursor_persists_across_restart() {
    let node = Node::start(20, vectors());
    let dir = state_dir("restart");
    let mut settings = config(&dir);
    settings.max_range = 12;
    {
        let mut ingester = checked(Ingester::open(node.rpc(), &settings));
        let first = checked(ingester.poll());
        assert_eq!(first.len(), 3);
        assert_eq!(ingester.status().cursor, 12);
        assert_eq!(ingester.status().lag, 9);
    }
    assert_eq!(checked(fs::read_to_string(dir.join("cursor"))), "12");
    let mut restarted = checked(Ingester::open(node.rpc(), &settings));
    assert_eq!(restarted.status().cursor, 12);
    assert_eq!(restarted.journal().len(), 3);
    let second = checked(restarted.poll());
    assert_eq!(second.len(), 4);
    assert_eq!(second[0].block_number, 12);
    assert_eq!(node.ranges(), vec![(0, 11), (12, 20)]);
    assert_eq!(
        checked(Journal::read_all(&dir.join("journal.jsonl"))).len(),
        7
    );
}

#[test]
fn cursor_outside_the_retention_window_alerts_and_never_reads_below_it() {
    let head = 200_010;
    let shifted: Vec<RecordedLog> = vectors()
        .into_iter()
        .map(|mut recorded| {
            recorded.block += 100_000;
            recorded
        })
        .collect();
    let node = Node::start(head, shifted);
    let dir = state_dir("window");
    checked(fs::create_dir_all(&dir));
    checked(fs::write(dir.join("cursor"), "5"));
    let mut settings = config(&dir);
    settings.max_range = 100_000;
    let mut ingester = checked(Ingester::open(node.rpc(), &settings));
    assert!(ingester.status().alert.is_none());
    let fresh = checked(ingester.poll());
    let earliest = head - 100_000 + 1;
    let alert = WindowAlert {
        cursor: 5,
        earliest,
        head,
    };
    assert_eq!(ingester.status().alert, Some(alert));
    assert_eq!(node.ranges(), vec![(earliest, head)]);
    // The two logs at block 100_010 lie below the retained window and are
    // never requested; the five inside it are ingested.
    assert_eq!(fresh.len(), 5);
    assert!(fresh.iter().all(|intent| intent.block_number >= earliest));
    let body: Value = checked(serde_json::from_str(&readyz_body(&ingester.status(), None)));
    assert_eq!(body["window_alert"]["cursor"], 5);
    assert_eq!(body["window_alert"]["earliest"], earliest);

    // Inside the window there is no alert.
    let dir = state_dir("window-inside");
    let mut inside = Config::new(dir);
    inside.start_block = Some(earliest);
    inside.max_range = 100_000;
    let mut ingester = checked(Ingester::open(node.rpc(), &inside));
    checked(ingester.poll());
    assert!(ingester.status().alert.is_none());
}

#[test]
fn duplicate_logs_and_rescans_are_journaled_once() {
    let mut recorded = vectors();
    // A second log carrying an already seen intent id.
    let mut replayed = recorded[0].clone();
    replayed.block = 15;
    replayed.tx[31] = 0x7b;
    recorded.push(replayed);
    let node = Node::start(20, recorded);
    node.set(|state| state.duplicate = true);
    let dir = state_dir("dupes");
    let mut ingester = checked(Ingester::open(node.rpc(), &config(&dir)));
    let fresh = checked(ingester.poll());
    assert_eq!(fresh.len(), 7);
    let mut ids: Vec<_> = fresh.iter().map(|intent| intent.id).collect();
    ids.sort_unstable();
    ids.dedup();
    assert_eq!(ids.len(), 7);

    // Losing the cursor rescans from the start without re-journaling.
    drop(ingester);
    checked(fs::remove_file(dir.join("cursor")));
    let mut rescanned = checked(Ingester::open(node.rpc(), &config(&dir)));
    assert!(checked(rescanned.poll()).is_empty());
    assert_eq!(
        checked(Journal::read_all(&dir.join("journal.jsonl"))).len(),
        7
    );
}

#[test]
fn undecodable_precompile_log_holds_the_cursor() {
    let mut recorded = vectors();
    recorded[2].data.truncate(16);
    let node = Node::start(20, recorded);
    let dir = state_dir("refuse");
    let mut ingester = checked(Ingester::open(node.rpc(), &config(&dir)));
    let Err(IngestError::Decode {
        block_number,
        log_index,
        ..
    }) = ingester.poll()
    else {
        panic!("malformed log accepted");
    };
    assert_eq!((block_number, log_index), (11, 0));
    assert!(!dir.join("cursor").exists());
    assert!(ingester.journal().is_empty());
}
