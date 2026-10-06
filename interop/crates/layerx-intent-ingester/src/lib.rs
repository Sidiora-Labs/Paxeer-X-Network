//! Exchange intent ingester: tails `eth_getLogs` on the exchange, bridge and
//! launchpad precompiles from a persisted cursor, decodes every log with
//! `layerx-intents`, and appends each decoded intent once to a journal keyed
//! by its intent id.

#![forbid(unsafe_code)]

use std::collections::HashSet;
use std::fmt;
use std::fs::{self, File, OpenOptions};
use std::io::{BufRead as _, BufReader, Read as _, Write as _};
use std::net::{TcpStream, ToSocketAddrs as _};
use std::path::{Path, PathBuf};
use std::time::Duration;

use layerx_intents::precompile::{
    keccak256, EventDecodeError, EvmLog, PrecompileEvent, PrecompileEventKind, BRIDGE_PRECOMPILE,
    EXCHANGE_PRECOMPILE, LAUNCHPAD_PRECOMPILE,
};
use serde_json::{json, Value};

/// The precompiles the ingester tails, in `eth_getLogs` filter order.
pub const TAILED_PRECOMPILES: [[u8; 20]; 3] =
    [EXCHANGE_PRECOMPILE, BRIDGE_PRECOMPILE, LAUNCHPAD_PRECOMPILE];

/// The node's block retention window (`min-retain-blocks`).
pub const DEFAULT_RETENTION_WINDOW: u64 = 100_000;

/// Widest block range requested in one `eth_getLogs` call.
pub const DEFAULT_MAX_RANGE: u64 = 1_000;

const MAX_RESPONSE_BYTES: u64 = 64 * 1024 * 1024;

#[derive(Debug)]
pub enum IngestError {
    Configuration(String),
    Transport(String),
    Rpc {
        code: i64,
        message: String,
    },
    Malformed(String),
    Decode {
        block_number: u64,
        log_index: u64,
        error: EventDecodeError,
    },
    Io(std::io::Error),
}

impl fmt::Display for IngestError {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::Configuration(detail) => write!(formatter, "configuration refused: {detail}"),
            Self::Transport(detail) => write!(formatter, "rpc transport failed: {detail}"),
            Self::Rpc { code, message } => write!(formatter, "rpc rejected {code}: {message}"),
            Self::Malformed(detail) => write!(formatter, "rpc response malformed: {detail}"),
            Self::Decode {
                block_number,
                log_index,
                error,
            } => write!(
                formatter,
                "precompile log at block {block_number} index {log_index} refused: {error:?}"
            ),
            Self::Io(error) => write!(formatter, "state io failed: {error}"),
        }
    }
}

impl std::error::Error for IngestError {}

impl From<std::io::Error> for IngestError {
    fn from(value: std::io::Error) -> Self {
        Self::Io(value)
    }
}

/// One node JSON-RPC call.
pub trait JsonRpc {
    /// # Errors
    ///
    /// Returns the transport or node refusal.
    fn call(&self, method: &str, params: Value) -> Result<Value, IngestError>;
}

/// Plain HTTP JSON-RPC to the co-located node (`http://host:port/path`).
pub struct HttpRpc {
    host: String,
    port: u16,
    path: String,
    timeout: Duration,
}

impl HttpRpc {
    /// # Errors
    ///
    /// Refuses a URL that is not `http://host[:port][/path]`.
    pub fn new(url: &str, timeout: Duration) -> Result<Self, IngestError> {
        let rest = url
            .strip_prefix("http://")
            .ok_or_else(|| IngestError::Configuration(format!("rpc url must be http://: {url}")))?;
        let (authority, path) = rest
            .find('/')
            .map_or((rest, "/"), |index| (&rest[..index], &rest[index..]));
        let (host, port) = match authority.rsplit_once(':') {
            Some((host, port)) => (
                host,
                port.parse::<u16>()
                    .map_err(|_| IngestError::Configuration(format!("rpc port: {url}")))?,
            ),
            None => (authority, 80),
        };
        if host.is_empty() {
            return Err(IngestError::Configuration(format!("rpc host: {url}")));
        }
        Ok(Self {
            host: host.to_owned(),
            port,
            path: path.to_owned(),
            timeout,
        })
    }
}

impl JsonRpc for HttpRpc {
    fn call(&self, method: &str, params: Value) -> Result<Value, IngestError> {
        let transport = |error: std::io::Error| IngestError::Transport(error.to_string());
        let address = (self.host.as_str(), self.port)
            .to_socket_addrs()
            .map_err(transport)?
            .next()
            .ok_or_else(|| IngestError::Transport(format!("no address for {}", self.host)))?;
        let mut stream = TcpStream::connect_timeout(&address, self.timeout).map_err(transport)?;
        stream
            .set_read_timeout(Some(self.timeout))
            .map_err(transport)?;
        stream
            .set_write_timeout(Some(self.timeout))
            .map_err(transport)?;
        let body =
            json!({"jsonrpc": "2.0", "id": 1, "method": method, "params": params}).to_string();
        // HTTP/1.0 keeps the reply unchunked and closes the connection after it.
        let request = format!(
            "POST {} HTTP/1.0\r\nHost: {}:{}\r\nContent-Type: application/json\r\nContent-Length: {}\r\n\r\n{body}",
            self.path,
            self.host,
            self.port,
            body.len()
        );
        stream.write_all(request.as_bytes()).map_err(transport)?;
        let mut reply = Vec::new();
        stream
            .take(MAX_RESPONSE_BYTES)
            .read_to_end(&mut reply)
            .map_err(transport)?;
        let split = reply
            .windows(4)
            .position(|window| window == b"\r\n\r\n")
            .ok_or_else(|| IngestError::Malformed("no http header terminator".to_owned()))?;
        let head = String::from_utf8_lossy(&reply[..split]);
        let status = head.split_whitespace().nth(1).unwrap_or_default();
        if status != "200" {
            return Err(IngestError::Transport(format!("http status {status}")));
        }
        let value: Value = serde_json::from_slice(&reply[split + 4..])
            .map_err(|error| IngestError::Malformed(error.to_string()))?;
        if let Some(error) = value.get("error") {
            return Err(IngestError::Rpc {
                code: error
                    .get("code")
                    .and_then(Value::as_i64)
                    .unwrap_or_default(),
                message: error
                    .get("message")
                    .and_then(Value::as_str)
                    .unwrap_or_default()
                    .to_owned(),
            });
        }
        value
            .get("result")
            .cloned()
            .ok_or_else(|| IngestError::Malformed("reply without result".to_owned()))
    }
}

/// One decoded precompile intent, as journaled.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct IngestedIntent {
    /// Idempotency key: the event's intent id, or for events without one the
    /// Keccak-256 of the transaction hash and big-endian log index.
    pub id: [u8; 32],
    pub block_number: u64,
    pub tx_hash: [u8; 32],
    pub log_index: u64,
    pub address: [u8; 20],
    pub topics: Vec<[u8; 32]>,
    pub data: Vec<u8>,
    pub event: PrecompileEvent,
}

impl IngestedIntent {
    fn decode(
        block_number: u64,
        tx_hash: [u8; 32],
        log_index: u64,
        address: [u8; 20],
        topics: Vec<[u8; 32]>,
        data: Vec<u8>,
    ) -> Result<Self, IngestError> {
        let event = PrecompileEvent::decode(&EvmLog {
            address,
            topics: &topics,
            data: &data,
        })
        .map_err(|error| IngestError::Decode {
            block_number,
            log_index,
            error,
        })?;
        let id = intent_id(&event).unwrap_or_else(|| {
            let mut preimage = tx_hash.to_vec();
            preimage.extend_from_slice(&log_index.to_be_bytes());
            keccak256(&preimage)
        });
        Ok(Self {
            id,
            block_number,
            tx_hash,
            log_index,
            address,
            topics,
            data,
            event,
        })
    }

    fn to_line(&self) -> String {
        json!({
            "id": hex(&self.id),
            "block": self.block_number,
            "tx": hex(&self.tx_hash),
            "log_index": self.log_index,
            "event": event_name(self.event.kind()),
            "address": hex(&self.address),
            "topics": self.topics.iter().map(|topic| hex(topic)).collect::<Vec<_>>(),
            "data": hex(&self.data),
        })
        .to_string()
    }

    fn from_line(line: &str) -> Result<Self, IngestError> {
        let value: Value = serde_json::from_str(line)
            .map_err(|error| IngestError::Malformed(format!("journal line: {error}")))?;
        let block = value
            .get("block")
            .and_then(Value::as_u64)
            .ok_or_else(|| IngestError::Malformed("journal block".to_owned()))?;
        let log_index = value
            .get("log_index")
            .and_then(Value::as_u64)
            .ok_or_else(|| IngestError::Malformed("journal log_index".to_owned()))?;
        let topics = value
            .get("topics")
            .and_then(Value::as_array)
            .ok_or_else(|| IngestError::Malformed("journal topics".to_owned()))?
            .iter()
            .map(fixed_field::<32>)
            .collect::<Result<Vec<_>, _>>()?;
        let entry = Self::decode(
            block,
            fixed_field(value.get("tx").unwrap_or(&Value::Null))?,
            log_index,
            fixed_field(value.get("address").unwrap_or(&Value::Null))?,
            topics,
            unhex(
                value
                    .get("data")
                    .and_then(Value::as_str)
                    .unwrap_or_default(),
            )?,
        )?;
        let recorded: [u8; 32] = fixed_field(value.get("id").unwrap_or(&Value::Null))?;
        if recorded != entry.id {
            return Err(IngestError::Malformed(format!(
                "journal id {} does not match its log",
                hex(&recorded)
            )));
        }
        Ok(entry)
    }
}

/// Returns the intent id an exchange event carries.
#[must_use]
pub const fn intent_id(event: &PrecompileEvent) -> Option<[u8; 32]> {
    match event {
        PrecompileEvent::MarginDeposited(inner) => Some(inner.intent_id),
        PrecompileEvent::MarginWithdrawalRequested(inner) => Some(inner.intent_id),
        PrecompileEvent::OrderCancelRequested(inner) => Some(inner.intent_id),
        PrecompileEvent::OrderPlaced(inner) => Some(inner.intent_id),
        PrecompileEvent::SettlementRequested(inner) => Some(inner.intent_id),
        _ => None,
    }
}

fn event_name(kind: PrecompileEventKind) -> &'static str {
    let signature = kind.signature();
    signature
        .find('(')
        .map_or(signature, |index| &signature[..index])
}

/// Append-only JSON-lines journal of decoded intents.
pub struct Journal {
    path: PathBuf,
    ids: HashSet<[u8; 32]>,
    entries: usize,
}

impl Journal {
    /// Opens the journal, re-decoding every recorded line.
    ///
    /// # Errors
    ///
    /// Refuses an unreadable journal or a line that no longer decodes.
    pub fn open(path: &Path) -> Result<Self, IngestError> {
        let mut ids = HashSet::new();
        let mut entries = 0;
        if path.exists() {
            for line in BufReader::new(File::open(path)?).lines() {
                let line = line?;
                if line.trim().is_empty() {
                    continue;
                }
                ids.insert(IngestedIntent::from_line(&line)?.id);
                entries += 1;
            }
        }
        Ok(Self {
            path: path.to_owned(),
            ids,
            entries,
        })
    }

    /// Reads every journaled intent in append order.
    ///
    /// # Errors
    ///
    /// Refuses an unreadable journal or a line that no longer decodes.
    pub fn read_all(path: &Path) -> Result<Vec<IngestedIntent>, IngestError> {
        if !path.exists() {
            return Ok(Vec::new());
        }
        BufReader::new(File::open(path)?)
            .lines()
            .filter(|line| line.as_ref().map_or(true, |line| !line.trim().is_empty()))
            .map(|line| IngestedIntent::from_line(&line?))
            .collect()
    }

    #[must_use]
    pub fn contains(&self, id: &[u8; 32]) -> bool {
        self.ids.contains(id)
    }

    #[must_use]
    pub const fn len(&self) -> usize {
        self.entries
    }

    #[must_use]
    pub const fn is_empty(&self) -> bool {
        self.entries == 0
    }

    /// Appends the intents not yet journaled and syncs them to disk; returns
    /// the appended ones.
    ///
    /// # Errors
    ///
    /// Returns the write or sync failure; nothing is marked as seen then.
    pub fn append(
        &mut self,
        intents: Vec<IngestedIntent>,
    ) -> Result<Vec<IngestedIntent>, IngestError> {
        let mut fresh = Vec::new();
        let mut batch = HashSet::new();
        for intent in intents {
            if !self.ids.contains(&intent.id) && batch.insert(intent.id) {
                fresh.push(intent);
            }
        }
        if fresh.is_empty() {
            return Ok(fresh);
        }
        let mut text = String::new();
        for intent in &fresh {
            text.push_str(&intent.to_line());
            text.push('\n');
        }
        let mut file = OpenOptions::new()
            .create(true)
            .append(true)
            .open(&self.path)?;
        file.write_all(text.as_bytes())?;
        file.sync_data()?;
        self.ids.extend(batch);
        self.entries += fresh.len();
        Ok(fresh)
    }
}

/// The cursor fell behind the node's earliest retained block; the blocks in
/// `[cursor, earliest)` are no longer served and were skipped.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct WindowAlert {
    pub cursor: u64,
    pub earliest: u64,
    pub head: u64,
}

#[derive(Clone, Debug)]
pub struct Config {
    pub state_dir: PathBuf,
    /// First block to scan when no cursor is persisted; `None` starts at the
    /// node's earliest retained block.
    pub start_block: Option<u64>,
    pub retention_window: u64,
    pub max_range: u64,
}

impl Config {
    #[must_use]
    pub fn new(state_dir: PathBuf) -> Self {
        Self {
            state_dir,
            start_block: None,
            retention_window: DEFAULT_RETENTION_WINDOW,
            max_range: DEFAULT_MAX_RANGE,
        }
    }
}

/// Progress snapshot for `/readyz`.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct Status {
    /// Next block to scan.
    pub cursor: u64,
    pub head: u64,
    pub lag: u64,
    pub alert: Option<WindowAlert>,
    pub journaled: usize,
}

pub struct Ingester<R> {
    rpc: R,
    journal: Journal,
    cursor_path: PathBuf,
    cursor: Option<u64>,
    start_block: Option<u64>,
    retention_window: u64,
    max_range: u64,
    head: u64,
    alert: Option<WindowAlert>,
}

impl<R: JsonRpc> Ingester<R> {
    /// Opens the persisted cursor and journal under `config.state_dir`.
    ///
    /// # Errors
    ///
    /// Refuses a zero window or range, or unreadable state.
    pub fn open(rpc: R, config: &Config) -> Result<Self, IngestError> {
        if config.retention_window == 0 || config.max_range == 0 {
            return Err(IngestError::Configuration(
                "retention window and max range must be positive".to_owned(),
            ));
        }
        fs::create_dir_all(&config.state_dir)?;
        let cursor_path = config.state_dir.join("cursor");
        let cursor = if cursor_path.exists() {
            let text = fs::read_to_string(&cursor_path)?;
            Some(text.trim().parse::<u64>().map_err(|_| {
                IngestError::Malformed(format!("cursor file holds {:?}", text.trim()))
            })?)
        } else {
            None
        };
        Ok(Self {
            rpc,
            journal: Journal::open(&config.state_dir.join("journal.jsonl"))?,
            cursor_path,
            cursor,
            start_block: config.start_block,
            retention_window: config.retention_window,
            max_range: config.max_range,
            head: 0,
            alert: None,
        })
    }

    #[must_use]
    pub const fn journal(&self) -> &Journal {
        &self.journal
    }

    #[must_use]
    pub fn status(&self) -> Status {
        let cursor = self.cursor.unwrap_or_default();
        Status {
            cursor,
            head: self.head,
            lag: (self.head + 1).saturating_sub(cursor),
            alert: self.alert,
            journaled: self.journal.len(),
        }
    }

    /// Scans the next block range and returns the intents journaled by it.
    ///
    /// # Errors
    ///
    /// Returns the node, decode or state failure; the cursor does not move.
    pub fn poll(&mut self) -> Result<Vec<IngestedIntent>, IngestError> {
        let head = parse_quantity(&self.rpc.call("eth_blockNumber", json!([]))?)?;
        self.head = head;
        let earliest = head.saturating_sub(self.retention_window - 1);
        let mut cursor = match self.cursor {
            Some(cursor) => cursor,
            None => self.start_block.unwrap_or(earliest),
        };
        if cursor < earliest {
            self.alert = Some(WindowAlert {
                cursor,
                earliest,
                head,
            });
            cursor = earliest;
        }
        if cursor > head {
            self.persist_cursor(cursor)?;
            return Ok(Vec::new());
        }
        let to = head.min(cursor.saturating_add(self.max_range - 1));
        let filter = json!([{
            "fromBlock": quantity(cursor),
            "toBlock": quantity(to),
            "address": TAILED_PRECOMPILES.iter().map(|address| hex(address)).collect::<Vec<_>>(),
        }]);
        let logs = self.rpc.call("eth_getLogs", filter)?;
        let logs = logs
            .as_array()
            .ok_or_else(|| IngestError::Malformed("eth_getLogs result is not a list".to_owned()))?;
        let mut decoded = Vec::with_capacity(logs.len());
        for log in logs {
            if log.get("removed").and_then(Value::as_bool) == Some(true) {
                continue;
            }
            let intent = parse_log(log)?;
            if intent.block_number < cursor || intent.block_number > to {
                return Err(IngestError::Malformed(format!(
                    "log at block {} outside requested {cursor}..={to}",
                    intent.block_number
                )));
            }
            decoded.push(intent);
        }
        decoded.sort_by_key(|intent| (intent.block_number, intent.log_index));
        let fresh = self.journal.append(decoded)?;
        self.persist_cursor(to + 1)?;
        Ok(fresh)
    }

    fn persist_cursor(&mut self, next: u64) -> Result<(), IngestError> {
        let staged = self.cursor_path.with_extension("tmp");
        let mut file = File::create(&staged)?;
        file.write_all(next.to_string().as_bytes())?;
        file.sync_data()?;
        fs::rename(&staged, &self.cursor_path)?;
        self.cursor = Some(next);
        Ok(())
    }
}

fn parse_log(log: &Value) -> Result<IngestedIntent, IngestError> {
    let field = |name: &str| {
        log.get(name)
            .ok_or_else(|| IngestError::Malformed(format!("log without {name}")))
    };
    let topics = field("topics")?
        .as_array()
        .ok_or_else(|| IngestError::Malformed("log topics".to_owned()))?
        .iter()
        .map(fixed_field::<32>)
        .collect::<Result<Vec<_>, _>>()?;
    IngestedIntent::decode(
        parse_quantity(field("blockNumber")?)?,
        fixed_field(field("transactionHash")?)?,
        parse_quantity(field("logIndex")?)?,
        fixed_field(field("address")?)?,
        topics,
        unhex(
            field("data")?
                .as_str()
                .ok_or_else(|| IngestError::Malformed("log data".to_owned()))?,
        )?,
    )
}

/// Encodes a block number as a JSON-RPC quantity.
#[must_use]
pub fn quantity(value: u64) -> String {
    format!("0x{value:x}")
}

/// Parses a JSON-RPC quantity string.
///
/// # Errors
///
/// Refuses anything but a `0x`-prefixed hex string fitting `u64`.
pub fn parse_quantity(value: &Value) -> Result<u64, IngestError> {
    value
        .as_str()
        .and_then(|text| text.strip_prefix("0x"))
        .and_then(|digits| u64::from_str_radix(digits, 16).ok())
        .ok_or_else(|| IngestError::Malformed(format!("quantity {value}")))
}

/// Lowercase `0x`-prefixed hex.
#[must_use]
pub fn hex(bytes: &[u8]) -> String {
    let mut out = String::with_capacity(2 + bytes.len() * 2);
    out.push_str("0x");
    for byte in bytes {
        out.push_str(&format!("{byte:02x}"));
    }
    out
}

/// Decodes `0x`-prefixed hex.
///
/// # Errors
///
/// Refuses a missing prefix, odd length or non-hex digit.
pub fn unhex(text: &str) -> Result<Vec<u8>, IngestError> {
    let digits = text
        .strip_prefix("0x")
        .ok_or_else(|| IngestError::Malformed(format!("hex without 0x: {text}")))?;
    if digits.len() % 2 != 0 {
        return Err(IngestError::Malformed(format!("odd hex: {text}")));
    }
    (0..digits.len())
        .step_by(2)
        .map(|index| {
            u8::from_str_radix(&digits[index..index + 2], 16)
                .map_err(|_| IngestError::Malformed(format!("hex digit: {text}")))
        })
        .collect()
}

fn fixed_field<const N: usize>(value: &Value) -> Result<[u8; N], IngestError> {
    let text = value
        .as_str()
        .ok_or_else(|| IngestError::Malformed(format!("expected hex string, got {value}")))?;
    unhex(text)?
        .try_into()
        .map_err(|_| IngestError::Malformed(format!("expected {N} bytes: {text}")))
}

/// Renders the `/readyz` body.
#[must_use]
pub fn readyz_body(status: &Status, last_error: Option<&str>) -> String {
    json!({
        "ready": last_error.is_none(),
        "cursor": status.cursor,
        "head": status.head,
        "lag": status.lag,
        "journaled": status.journaled,
        "window_alert": status.alert.map(|alert| json!({
            "cursor": alert.cursor,
            "earliest": alert.earliest,
            "head": alert.head,
        })),
        "last_error": last_error,
    })
    .to_string()
}
