use super::{http, public_reads, rpc, ws_wire, Config, IncomingRequest};
use serde_json::{json, Value};
use std::collections::{BTreeMap, BTreeSet};
use std::io::{Read, Write};
use std::net::TcpStream;
use std::sync::{mpsc, Arc, Mutex, OnceLock};
use std::time::{Duration, Instant};

const MAX_SUBSCRIBERS: usize = 32;
const QUEUE_DEPTH: usize = 16;
const MAX_SUBSCRIPTIONS: usize = 8;
const MAX_RESUME: u64 = 16;

#[derive(Default)]
struct Hub {
    next_id: u64,
    clients: BTreeMap<u64, mpsc::SyncSender<(u64, Value)>>,
}
static HUB: OnceLock<Mutex<Hub>> = OnceLock::new();
static WORKER: std::sync::Once = std::sync::Once::new();
static SOCKETS: std::sync::atomic::AtomicUsize = std::sync::atomic::AtomicUsize::new(0);

struct SocketGuard;
impl SocketGuard {
    fn acquire() -> Option<Self> {
        use std::sync::atomic::Ordering;
        SOCKETS
            .fetch_update(Ordering::AcqRel, Ordering::Acquire, |count| {
                (count < MAX_SUBSCRIBERS).then_some(count + 1)
            })
            .ok()
            .map(|_| Self)
    }
}
impl Drop for SocketGuard {
    fn drop(&mut self) {
        SOCKETS.fetch_sub(1, std::sync::atomic::Ordering::AcqRel);
    }
}

impl Hub {
    fn register(&mut self) -> Result<(u64, mpsc::Receiver<(u64, Value)>), String> {
        if self.clients.len() >= MAX_SUBSCRIBERS {
            return Err("subscription capacity exhausted".into());
        }
        self.next_id = self
            .next_id
            .checked_add(1)
            .ok_or("subscription identifier exhausted")?;
        let (sender, receiver) = mpsc::sync_channel(QUEUE_DEPTH);
        self.clients.insert(self.next_id, sender);
        Ok((self.next_id, receiver))
    }
    fn publish(&mut self, sequence: u64, value: &Value) {
        self.clients
            .retain(|_, sender| sender.try_send((sequence, value.clone())).is_ok());
    }
}

struct Registration(u64);
impl Drop for Registration {
    fn drop(&mut self) {
        if let Ok(mut hub) = HUB.get_or_init(Mutex::default).lock() {
            hub.clients.remove(&self.0);
        }
    }
}

fn receipt_event(config: &Config, sequence: u64) -> Result<Option<Value>, String> {
    let (endpoint, token) = config
        .backend(super::KernelBackend::PublicCore)
        .map_err(|_| "core unavailable")?;
    let answer = super::upstream_json(
        config,
        endpoint,
        token,
        "GET",
        &format!("/internal/v1/receipt-events/{sequence}"),
        None,
        &[],
    )
    .map_err(|_| "receipt feed unavailable")?;
    if !matches!(answer.status, 200 | 202) || answer.content_type != "application/json" {
        return Err("receipt feed refused".into());
    }
    let document: Value =
        serde_json::from_slice(&answer.body).map_err(|_| "invalid receipt feed")?;
    if answer.status == 202 {
        if document["result"]["state"] != "pending" {
            return Err("invalid receipt wait".into());
        }
        return Ok(None);
    }
    let receipt = &document["result"];
    if receipt["global_sequence"].as_u64() != Some(sequence)
        || receipt["receipt"].as_str().is_none_or(str::is_empty)
    {
        return Err("invalid receipt sequence".into());
    }
    Ok(Some(receipt.clone()))
}

fn feed(config: &Config, mut sequence: u64) -> Result<(), String> {
    loop {
        let (delivered, event) = if let Some(receipt) = receipt_event(config, sequence)? {
            let delivered = sequence;
            sequence = sequence
                .checked_add(1)
                .ok_or("receipt sequence exhausted")?;
            (delivered, receipt)
        } else {
            std::thread::sleep(Duration::from_millis(100));
            (sequence.saturating_sub(1), Value::Null)
        };
        let mut hub = HUB
            .get_or_init(Mutex::default)
            .lock()
            .map_err(|_| "subscription lock unavailable")?;
        hub.publish(delivered, &event);
    }
}

fn start_feed(config: &Arc<Config>) -> Result<(), String> {
    let sequence = head_sequence(config)
        .and_then(|head| head.checked_add(1))
        .ok_or("invalid node head")?;
    WORKER.call_once(|| {
        let config = Arc::clone(config);
        std::thread::spawn(move || {
            let mut next = sequence;
            loop {
                if feed(&config, next).is_err() {
                    if let Ok(mut hub) = HUB.get_or_init(Mutex::default).lock() {
                        hub.clients.clear();
                    }
                    std::thread::sleep(Duration::from_secs(1));
                    if let Some(sequence) =
                        head_sequence(&config).and_then(|head| head.checked_add(1))
                    {
                        next = sequence;
                    }
                }
            }
        });
    });
    Ok(())
}

fn head_sequence(config: &Config) -> Option<u64> {
    rpc::read_result(config, "/v1/node-info")?["chain_head_sequence"]
        .as_str()
        .and_then(sequence_value)
}

fn sequence_value(text: &str) -> Option<u64> {
    let value: u64 = text.parse().ok()?;
    (value.to_string() == text).then_some(value)
}

#[derive(Clone, Debug, PartialEq)]
enum Topic {
    Receipts,
    Checkpoints,
    Account(String),
}

fn account_selector(account: &str) -> bool {
    super::parse_hex32(account).is_ok() && account != "00".repeat(32)
}

fn selector(params: Option<&Value>) -> Result<(Topic, Option<u64>), i32> {
    let Some(Value::Array(args)) = params else {
        return Err(-32602);
    };
    let (topic, cursor) = match args.as_slice() {
        [Value::String(name)] if name == "receipts" => (Topic::Receipts, None),
        [Value::String(name)] if name == "checkpoints" => (Topic::Checkpoints, None),
        [Value::String(name), Value::String(cursor)] if name == "receipts" => {
            (Topic::Receipts, Some(cursor))
        }
        [Value::String(name), Value::String(cursor)] if name == "checkpoints" => {
            (Topic::Checkpoints, Some(cursor))
        }
        [Value::String(name), Value::String(account)]
            if name == "account" && account_selector(account) =>
        {
            (Topic::Account(account.to_ascii_lowercase()), None)
        }
        [Value::String(name), Value::String(account), Value::String(cursor)]
            if name == "account" && account_selector(account) =>
        {
            (Topic::Account(account.to_ascii_lowercase()), Some(cursor))
        }
        _ => return Err(-32602),
    };
    match cursor {
        None => Ok((topic, None)),
        Some(text) => Ok((topic, Some(sequence_value(text).ok_or(-32602)?))),
    }
}

fn cancellation(params: Option<&Value>) -> Result<u64, i32> {
    let Some(Value::Array(args)) = params else {
        return Err(-32602);
    };
    match args.as_slice() {
        [Value::String(subscription)] => sequence_value(subscription).ok_or(-32602),
        _ => Err(-32602),
    }
}

fn allowed(scopes: &str, topic: &Topic) -> bool {
    scopes.split(',').any(|scope| {
        scope
            == match topic {
                Topic::Receipts => "receipt:read",
                _ => "state:read",
            }
    })
}

struct Subscription {
    id: u64,
    topic: Topic,
    last: Option<Value>,
    cursor: Option<u64>,
}

#[derive(Default)]
struct Subscriptions {
    next: u64,
    active: Vec<Subscription>,
}

fn cancel(subscriptions: &mut Subscriptions, subscription: u64) -> bool {
    let before = subscriptions.active.len();
    subscriptions
        .active
        .retain(|entry| entry.id != subscription);
    subscriptions.active.len() != before
}

fn resume_window(topic: &Topic, cursor: u64, head: u64) -> Result<Vec<u64>, i32> {
    if cursor > head {
        return Err(-32602);
    }
    if head.saturating_sub(cursor) > MAX_RESUME {
        return Err(-32005);
    }
    if head == cursor {
        return Ok(Vec::new());
    }
    if matches!(topic, Topic::Receipts) {
        return Ok((cursor.saturating_add(1)..=head).collect());
    }
    Ok(vec![head])
}

fn resume(
    config: &Config,
    subscription: &mut Subscription,
    cursor: u64,
    resumed: &mut Vec<Value>,
) -> Result<(), i32> {
    let head = head_sequence(config).ok_or(-32001)?;
    for sequence in resume_window(&subscription.topic, cursor, head)? {
        let receipt = receipt_event(config, sequence)
            .map_err(|_| -32001)?
            .ok_or(-32001)?;
        if let Some(result) = notification(config, subscription, &receipt) {
            resumed.push(event(subscription.id, &result, sequence));
        }
        subscription.cursor = Some(sequence);
    }
    Ok(())
}

fn command(
    config: &Config,
    request: &IncomingRequest,
    value: &Value,
    subscriptions: &mut Subscriptions,
    resumed: &mut Vec<Value>,
) -> Option<Value> {
    let method = value.get("method").and_then(Value::as_str);
    if !matches!(method, Some("lx_subscribe" | "lx_unsubscribe")) {
        return rpc::dispatch(config, request, value);
    }
    if let Some(error) = rpc::invalid_request(value) {
        return Some(error);
    }
    let id = value.get("id")?;
    if method == Some("lx_unsubscribe") {
        return Some(match cancellation(value.get("params")) {
            Ok(subscription) => {
                if cancel(subscriptions, subscription) {
                    json!({"jsonrpc":"2.0","id":id,"result":true})
                } else {
                    rpc::error(id, -32602, "Unknown subscription")
                }
            }
            Err(code) => rpc::error(id, code, "Invalid params"),
        });
    }
    let (topic, cursor) = match selector(value.get("params")) {
        Ok(selection) => selection,
        Err(code) => return Some(rpc::error(id, code, "Invalid params")),
    };
    let Ok(record) = super::authenticate_key(config, request) else {
        return Some(rpc::error(id, -32002, "Authentication required"));
    };
    if !allowed(&record.scopes, &topic) {
        return Some(rpc::error(id, -32002, "Insufficient scope"));
    }
    if subscriptions.active.len() >= MAX_SUBSCRIPTIONS {
        return Some(rpc::error(id, -32005, "Subscription limit"));
    }
    let Some(next) = subscriptions.next.checked_add(1) else {
        return Some(rpc::error(id, -32005, "Subscription limit"));
    };
    let mut subscription = Subscription {
        id: next,
        topic,
        last: None,
        cursor,
    };
    if let Some(cursor) = cursor {
        if let Err(code) = resume(config, &mut subscription, cursor, resumed) {
            resumed.clear();
            return Some(rpc::error(id, code, resume_refusal(code)));
        }
    }
    subscriptions.next = next;
    subscriptions.active.push(subscription);
    Some(json!({"jsonrpc":"2.0","id":id,"result":next.to_string()}))
}

fn resume_refusal(code: i32) -> &'static str {
    match code {
        -32005 => "Resume window exceeded",
        -32001 => "Read unavailable",
        _ => "Invalid params",
    }
}

fn event(subscription: u64, result: &Value, cursor: u64) -> Value {
    json!({"jsonrpc":"2.0","method":"lx_subscription","params":{"subscription":subscription.to_string(),"result":result,"cursor":cursor.to_string()}})
}

fn advance(cursor: Option<u64>, sequence: u64, receipt: &Value) -> Option<u64> {
    if !receipt.is_null() && cursor.is_some_and(|delivered| sequence <= delivered) {
        return None;
    }
    Some(cursor.map_or(sequence, |delivered| delivered.max(sequence)))
}

fn notification(
    config: &Config,
    subscription: &mut Subscription,
    receipt: &Value,
) -> Option<Value> {
    let value = match &subscription.topic {
        Topic::Receipts => {
            if receipt.is_null() {
                return None;
            }
            receipt.clone()
        }
        Topic::Account(account) => {
            if receipt.is_null() {
                return None;
            }
            rpc::read_result(config, &format!("/v1/accounts/{account}/balance"))?
        }
        Topic::Checkpoints => {
            let node = rpc::read_result(config, "/v1/node-info")?;
            let checkpoint = node["latest_finalised_checkpoint"].as_str()?;
            if super::parse_hex32(checkpoint).is_err() || checkpoint == "00".repeat(32) {
                return None;
            }
            rpc::read_result(config, &format!("/v1/checkpoints/{checkpoint}"))?
        }
    };
    if subscription.last.as_ref() == Some(&value) {
        return None;
    }
    subscription.last = Some(value.clone());
    Some(value)
}

fn valid_upgrade(request: &IncomingRequest) -> Option<String> {
    valid_upgrade_origin(request, false)
}

fn valid_upgrade_origin(request: &IncomingRequest, allowed_origin: bool) -> Option<String> {
    let header = |name: &str| request.headers.get(name).map_or("", String::as_str);
    if request.method != "GET"
        || !request.body.is_empty()
        || !header("upgrade").eq_ignore_ascii_case("websocket")
        || !header("connection")
            .split(',')
            .any(|s| s.trim().eq_ignore_ascii_case("upgrade"))
        || header("sec-websocket-version") != "13"
        || (request.headers.contains_key("origin") && !allowed_origin)
    {
        return None;
    }
    ws_wire::accept(header("sec-websocket-key"))
}

pub(super) trait Connection: Read + Write {
    fn socket(&self) -> &TcpStream;
}

impl Connection for rustls::StreamOwned<rustls::ServerConnection, TcpStream> {
    fn socket(&self) -> &TcpStream {
        &self.sock
    }
}

impl Connection for TcpStream {
    fn socket(&self) -> &TcpStream {
        self
    }
}

pub(super) fn serve<S: Connection>(
    config: &Arc<Config>,
    request: &IncomingRequest,
    stream: &mut S,
) -> Result<(), String> {
    let Some(accept) = (if config.routes.origin(request).is_some() {
        valid_upgrade_origin(request, true)
    } else {
        valid_upgrade(request)
    }) else {
        return http::write_response(
            stream,
            &super::response(400, "invalid_websocket_upgrade", None),
        );
    };
    let record = match super::authenticate_key(config, request) {
        Ok(record) => record,
        Err(answer) => return http::write_response(stream, &answer),
    };
    if ![Topic::Receipts, Topic::Checkpoints]
        .iter()
        .any(|topic| allowed(&record.scopes, topic))
    {
        return http::write_response(stream, &super::response(403, "insufficient_scope", None));
    }
    let Some(_socket) = SocketGuard::acquire() else {
        return http::write_response(stream, &super::response(429, "subscription_limit", Some(1)));
    };
    let (id, receiver) = {
        let mut hub = HUB
            .get_or_init(Mutex::default)
            .lock()
            .map_err(|_| "subscription lock unavailable")?;
        match hub.register() {
            Ok(value) => value,
            Err(_) => {
                return http::write_response(
                    stream,
                    &super::response(429, "subscription_limit", Some(1)),
                )
            }
        }
    };
    let _registration = Registration(id);
    start_feed(config)?;
    write!(stream, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: {accept}\r\n\r\n").and_then(|()| stream.flush()).map_err(|e| e.to_string())?;
    stream
        .socket()
        .set_read_timeout(Some(Duration::from_millis(50)))
        .map_err(|e| e.to_string())?;
    stream
        .socket()
        .set_write_timeout(Some(Duration::from_secs(2)))
        .map_err(|e| e.to_string())?;
    session(config, request, stream, &receiver)
}

fn session<S: Connection>(
    config: &Config,
    request: &IncomingRequest,
    stream: &mut S,
    receiver: &mpsc::Receiver<(u64, Value)>,
) -> Result<(), String> {
    let mut reader = ws_wire::Reader::default();
    let mut subscriptions = Subscriptions::default();
    let started = Instant::now();
    let mut last_input = Instant::now();
    let mut last_ping = Instant::now();
    loop {
        if started.elapsed() > Duration::from_secs(3600)
            || last_input.elapsed() > Duration::from_secs(60)
        {
            return ws_wire::write(stream, 8, &1000_u16.to_be_bytes());
        }
        match reader.read(stream) {
            Ok(Some((8, body))) => return ws_wire::write(stream, 8, &body),
            Ok(Some((9, body))) => {
                last_input = Instant::now();
                ws_wire::write(stream, 10, &body)?;
            }
            Ok(Some((10, _))) => last_input = Instant::now(),
            Ok(Some((1, body))) => {
                if super::authenticate_key(config, request).is_err() {
                    return ws_wire::write(stream, 8, &1008_u16.to_be_bytes());
                }
                last_input = Instant::now();
                if !public_reads::consume_read() {
                    return ws_wire::write(stream, 8, &1013_u16.to_be_bytes());
                }
                let mut resumed = Vec::new();
                let answer = match serde_json::from_slice::<Value>(&body) {
                    Ok(value) => command(config, request, &value, &mut subscriptions, &mut resumed),
                    Err(_) => Some(rpc::error(&Value::Null, -32700, "Parse error")),
                };
                if let Some(answer) = answer {
                    ws_wire::write(
                        stream,
                        1,
                        &serde_json::to_vec(&answer).map_err(|e| e.to_string())?,
                    )?;
                }
                for replayed in resumed {
                    ws_wire::write(
                        stream,
                        1,
                        &serde_json::to_vec(&replayed).map_err(|e| e.to_string())?,
                    )?;
                }
            }
            Ok(_) => (),
            Err(_) => return ws_wire::write(stream, 8, &1002_u16.to_be_bytes()),
        }
        match receiver.try_recv() {
            Ok((sequence, receipt)) => {
                let Ok(record) = super::authenticate_key(config, request) else {
                    return ws_wire::write(stream, 8, &1008_u16.to_be_bytes());
                };
                for subscription in &mut subscriptions.active {
                    if !allowed(&record.scopes, &subscription.topic) {
                        return ws_wire::write(stream, 8, &1008_u16.to_be_bytes());
                    }
                    let Some(position) = advance(subscription.cursor, sequence, &receipt) else {
                        continue;
                    };
                    if let Some(result) = notification(config, subscription, &receipt) {
                        ws_wire::write(
                            stream,
                            1,
                            &serde_json::to_vec(&event(subscription.id, &result, position))
                                .map_err(|e| e.to_string())?,
                        )?;
                    }
                    subscription.cursor = Some(position);
                }
            }
            Err(mpsc::TryRecvError::Disconnected) => {
                return ws_wire::write(stream, 8, &1013_u16.to_be_bytes())
            }
            Err(mpsc::TryRecvError::Empty) => (),
        }
        if last_ping.elapsed() >= Duration::from_secs(5) {
            if super::authenticate_key(config, request).is_err() {
                return ws_wire::write(stream, 8, &1008_u16.to_be_bytes());
            }
            ws_wire::write(stream, 9, b"lx")?;
            last_ping = Instant::now();
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn socket_capacity_is_held_until_connection_closes() {
        let sockets: Vec<_> = (0..MAX_SUBSCRIBERS)
            .map(|_| SocketGuard::acquire().unwrap_or_else(|| panic!("slot")))
            .collect();
        assert!(SocketGuard::acquire().is_none());
        drop(sockets);
        assert!(SocketGuard::acquire().is_some());
    }

    #[test]
    fn bounded_fanout_disconnects_slow_consumers() {
        let mut hub = Hub::default();
        let (_, slow) = hub.register().unwrap_or_else(|e| panic!("{e}"));
        let (_, fast) = hub.register().unwrap_or_else(|e| panic!("{e}"));
        for n in 0..=QUEUE_DEPTH {
            let sequence = u64::try_from(n).unwrap_or_else(|e| panic!("{e}"));
            let value = json!(n);
            hub.publish(sequence, &value);
            assert_eq!(fast.try_recv(), Ok((sequence, value)));
        }
        assert_eq!(hub.clients.len(), 1);
        for _ in 0..QUEUE_DEPTH {
            assert!(slow.try_recv().is_ok());
        }
        assert_eq!(slow.try_recv(), Err(mpsc::TryRecvError::Disconnected));
        let receivers: Vec<_> = (1..MAX_SUBSCRIBERS)
            .map(|_| hub.register().unwrap_or_else(|e| panic!("{e}")).1)
            .collect();
        assert!(hub.register().is_err());
        drop(receivers);
    }
    #[test]
    fn upgrade_rejects_wrong_version_origin_body_and_connection() {
        let mut request = IncomingRequest {
            method: "GET".into(),
            path: "/rpc/ws".into(),
            body: Vec::new(),
            headers: BTreeMap::from([
                ("upgrade".into(), "websocket".into()),
                ("connection".into(), "keep-alive, Upgrade".into()),
                ("sec-websocket-version".into(), "13".into()),
                (
                    "sec-websocket-key".into(),
                    "dGhlIHNhbXBsZSBub25jZQ==".into(),
                ),
            ]),
        };
        assert!(valid_upgrade(&request).is_some());
        for (header, invalid) in [
            ("upgrade", "http"),
            ("connection", "close"),
            ("sec-websocket-version", "12"),
            ("sec-websocket-key", "bad"),
        ] {
            let original = request.headers.insert(header.into(), invalid.into());
            assert!(valid_upgrade(&request).is_none());
            request
                .headers
                .insert(header.into(), original.unwrap_or_default());
        }
        request
            .headers
            .insert("origin".into(), "https://example.com".into());
        assert!(valid_upgrade(&request).is_none());
        request.headers.remove("origin");
        request.body.push(1);
        assert!(valid_upgrade(&request).is_none());
        request.body.clear();
        request.method = "POST".into();
        assert!(valid_upgrade(&request).is_none());
    }

    #[test]
    fn topics_and_scopes_are_exact() {
        assert_eq!(
            selector(Some(&json!(["receipts"]))),
            Ok((Topic::Receipts, None))
        );
        assert_eq!(
            selector(Some(&json!(["checkpoints"]))),
            Ok((Topic::Checkpoints, None))
        );
        assert_eq!(
            selector(Some(&json!(["account", "ab".repeat(32)]))),
            Ok((Topic::Account("ab".repeat(32)), None))
        );
        for args in [
            json!([]),
            json!(["account"]),
            json!(["account", "00".repeat(32)]),
            json!(["receipts", "x"]),
            json!(["unknown"]),
        ] {
            assert_eq!(selector(Some(&args)), Err(-32602));
        }
        assert!(allowed("receipt:read", &Topic::Receipts));
        assert!(!allowed("state:read", &Topic::Receipts));
        assert!(!allowed("activity:write", &Topic::Checkpoints));
    }

    #[test]
    fn resume_cursors_are_canonical_decimal_positions() {
        assert_eq!(
            selector(Some(&json!(["receipts", "41"]))),
            Ok((Topic::Receipts, Some(41)))
        );
        assert_eq!(
            selector(Some(&json!(["checkpoints", "0"]))),
            Ok((Topic::Checkpoints, Some(0)))
        );
        assert_eq!(
            selector(Some(&json!(["account", "ab".repeat(32), "7"]))),
            Ok((Topic::Account("ab".repeat(32)), Some(7)))
        );
        for args in [
            json!(["receipts", "07"]),
            json!(["receipts", ""]),
            json!(["receipts", "-1"]),
            json!(["receipts", "+1"]),
            json!(["receipts", "18446744073709551616"]),
            json!(["receipts", 41]),
            json!(["checkpoints", "1", "2"]),
            json!(["account", "ab".repeat(32), "07"]),
            json!(["account", "ab".repeat(32), "1", "2"]),
        ] {
            assert_eq!(selector(Some(&args)), Err(-32602));
        }
    }

    #[test]
    fn resume_windows_are_bounded_and_topic_shaped() {
        assert_eq!(resume_window(&Topic::Receipts, 4, 7), Ok(vec![5, 6, 7]));
        assert_eq!(resume_window(&Topic::Receipts, 7, 7), Ok(Vec::new()));
        assert_eq!(
            resume_window(&Topic::Receipts, 0, MAX_RESUME),
            Ok((1..=MAX_RESUME).collect::<Vec<_>>())
        );
        assert_eq!(
            resume_window(&Topic::Receipts, 0, MAX_RESUME + 1),
            Err(-32005)
        );
        assert_eq!(resume_window(&Topic::Receipts, 8, 7), Err(-32602));
        assert_eq!(resume_window(&Topic::Checkpoints, 4, 7), Ok(vec![7]));
        assert_eq!(
            resume_window(&Topic::Account("ab".repeat(32)), 4, 7),
            Ok(vec![7])
        );
        assert_eq!(
            resume_window(&Topic::Account("ab".repeat(32)), 7, 7),
            Ok(Vec::new())
        );
    }

    #[test]
    fn cursors_skip_replayed_receipts_and_track_idle_wakes() {
        let receipt = json!({"global_sequence":9});
        assert_eq!(advance(None, 9, &receipt), Some(9));
        assert_eq!(advance(Some(4), 9, &receipt), Some(9));
        assert_eq!(advance(Some(9), 9, &receipt), None);
        assert_eq!(advance(Some(12), 9, &receipt), None);
        assert_eq!(advance(None, 9, &Value::Null), Some(9));
        assert_eq!(advance(Some(9), 9, &Value::Null), Some(9));
        assert_eq!(advance(Some(12), 9, &Value::Null), Some(12));
    }

    #[test]
    fn cancellations_name_one_canonical_subscription() {
        assert_eq!(cancellation(Some(&json!(["3"]))), Ok(3));
        for args in [
            json!([]),
            json!(["3", "4"]),
            json!([3]),
            json!(["03"]),
            json!([""]),
        ] {
            assert_eq!(cancellation(Some(&args)), Err(-32602));
        }
        assert_eq!(
            cancellation(Some(&json!({"subscription":"3"}))),
            Err(-32602)
        );
        assert_eq!(cancellation(None), Err(-32602));
    }

    #[test]
    fn unsubscribe_releases_one_identifier_without_renumbering_the_others() {
        let mut subscriptions = Subscriptions {
            next: 3,
            active: vec![
                Subscription {
                    id: 1,
                    topic: Topic::Receipts,
                    last: None,
                    cursor: Some(4),
                },
                Subscription {
                    id: 2,
                    topic: Topic::Checkpoints,
                    last: None,
                    cursor: None,
                },
                Subscription {
                    id: 3,
                    topic: Topic::Account("ab".repeat(32)),
                    last: None,
                    cursor: None,
                },
            ],
        };
        assert!(cancel(&mut subscriptions, 2));
        assert!(!cancel(&mut subscriptions, 2));
        assert!(!cancel(&mut subscriptions, 4));
        assert_eq!(
            subscriptions
                .active
                .iter()
                .map(|entry| entry.id)
                .collect::<Vec<_>>(),
            vec![1, 3]
        );
        assert_eq!(subscriptions.next, 3);
        assert_eq!(subscriptions.active[0].cursor, Some(4));
        assert!(cancel(&mut subscriptions, 1));
        assert!(cancel(&mut subscriptions, 3));
        assert!(subscriptions.active.is_empty());
        assert_eq!(subscriptions.next, 3);
    }

    #[test]
    fn notifications_carry_the_subscription_and_its_cursor() {
        let frame = event(3, &json!({"activity_id":"ab"}), 41);
        assert_eq!(
            frame,
            json!({"jsonrpc":"2.0","method":"lx_subscription","params":{"subscription":"3","result":{"activity_id":"ab"},"cursor":"41"}})
        );
        assert_eq!(sequence_value("41"), Some(41));
        assert_eq!(sequence_value("041"), None);
        assert_eq!(sequence_value("0"), Some(0));
        assert_eq!(resume_refusal(-32005), "Resume window exceeded");
        assert_eq!(resume_refusal(-32001), "Read unavailable");
        assert_eq!(resume_refusal(-32602), "Invalid params");
    }
}

fn masked_frame(stream: &mut impl Write, opcode: u8, body: &[u8]) -> Result<(), String> {
    if body.len() > 65_536 {
        return Err("websocket message too large".into());
    }
    let mut mask = [0_u8; 4];
    getrandom::fill(&mut mask).map_err(|_| "websocket randomness unavailable")?;
    let mut frame = vec![0x80 | opcode];
    if body.len() < 126 {
        frame.push(0x80 | u8::try_from(body.len()).map_err(|_| "frame length")?);
    } else if body.len() <= usize::from(u16::MAX) {
        frame.push(0x80 | 126);
        frame.extend_from_slice(
            &u16::try_from(body.len())
                .map_err(|_| "frame length")?
                .to_be_bytes(),
        );
    } else {
        frame.push(0x80 | 127);
        frame.extend_from_slice(
            &u64::try_from(body.len())
                .map_err(|_| "frame length")?
                .to_be_bytes(),
        );
    }
    frame.extend_from_slice(&mask);
    frame.extend(body.iter().enumerate().map(|(i, b)| b ^ mask[i % 4]));
    stream
        .write_all(&frame)
        .and_then(|()| stream.flush())
        .map_err(|e| e.to_string())
}

fn upstream_frame(bytes: &mut Vec<u8>) -> Result<Option<(u8, Vec<u8>)>, String> {
    if bytes.len() < 2 {
        return Ok(None);
    }
    let opcode = bytes[0] & 15;
    if bytes[0] & 0x70 != 0
        || bytes[0] & 0x80 == 0
        || bytes[1] & 0x80 != 0
        || !matches!(opcode, 1 | 8 | 9 | 10)
    {
        return Err("upstream websocket frame refused".into());
    }
    let short = bytes[1];
    let header = match short {
        126 => 4,
        127 => 10,
        _ => 2,
    };
    if bytes.len() < header {
        return Ok(None);
    }
    let length = match short {
        126 => usize::from(u16::from_be_bytes([bytes[2], bytes[3]])),
        127 => usize::try_from(u64::from_be_bytes(
            bytes[2..10].try_into().map_err(|_| "frame length")?,
        ))
        .map_err(|_| "frame length")?,
        _ => usize::from(short),
    };
    if length > 65_536 || (opcode >= 8 && length > 125) {
        return Err("upstream frame bound".into());
    }
    if bytes.len() < header + length {
        return Ok(None);
    }
    let body = bytes[header..header + length].to_vec();
    bytes.drain(..header + length);
    Ok(Some((opcode, body)))
}

pub(super) fn serve_evm<S: Connection>(
    config: &Arc<Config>,
    request: &IncomingRequest,
    stream: &mut S,
) -> Result<(), String> {
    let Some(accept) = (if config.routes.origin(request).is_some() {
        valid_upgrade_origin(request, true)
    } else {
        valid_upgrade(request)
    }) else {
        return http::write_response(
            stream,
            &super::response(400, "invalid_websocket_upgrade", None),
        );
    };
    if let Err(answer) = super::authenticate_key(config, request) {
        return http::write_response(stream, &answer);
    }
    let Some(_socket) = SocketGuard::acquire() else {
        return http::write_response(stream, &super::response(429, "subscription_limit", Some(1)));
    };
    let endpoint = match std::env::var("LAYERX_GATEWAY_PAXEER_WS_URL")
        .ok()
        .and_then(|value| http::Endpoint::parse(&value).ok())
    {
        Some(endpoint) => endpoint,
        None => {
            return http::write_response(
                stream,
                &super::response(503, "paxeer_websocket_not_configured", None),
            )
        }
    };
    if !config
        .paxeer
        .as_ref()
        .is_some_and(|nodes| nodes.iter().any(|node| node.host == endpoint.host))
        || super::paxeer::status(config) != "available"
    {
        return http::write_response(
            stream,
            &super::response(503, "paxeer_websocket_network_unavailable", None),
        );
    }
    let mut upstream = match config.client.connect_tls(&endpoint) {
        Ok(upstream) => upstream,
        Err(_) => {
            return http::write_response(
                stream,
                &super::response(503, "paxeer_websocket_unavailable", None),
            )
        }
    };
    let key = request
        .headers
        .get("sec-websocket-key")
        .ok_or("websocket key absent")?;
    let path = if endpoint.base_path.is_empty() {
        "/"
    } else {
        &endpoint.base_path
    };
    write!(upstream, "GET {path} HTTP/1.1\r\nHost: {}:{}\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Key: {key}\r\n\r\n", endpoint.host, endpoint.port)
        .and_then(|()| upstream.flush()).map_err(|e| e.to_string())?;
    let mut headers = Vec::new();
    while !headers.ends_with(b"\r\n\r\n") {
        if headers.len() >= 8192 {
            return Err("upstream upgrade header bound".into());
        }
        let mut byte = [0_u8; 1];
        upstream
            .read_exact(&mut byte)
            .map_err(|_| "upstream upgrade unavailable")?;
        headers.push(byte[0]);
    }
    let headers = String::from_utf8(headers).map_err(|_| "upstream upgrade malformed")?;
    let mut lines = headers.split("\r\n");
    if lines.next() != Some("HTTP/1.1 101 Switching Protocols") {
        return Err("upstream upgrade refused".into());
    }
    let mut fields = BTreeMap::new();
    for line in lines.filter(|line| !line.is_empty()) {
        let (name, value) = line.split_once(':').ok_or("upstream upgrade malformed")?;
        if fields
            .insert(name.to_ascii_lowercase(), value.trim().to_owned())
            .is_some()
        {
            return Err("duplicate upgrade header".into());
        }
    }
    if fields.get("sec-websocket-accept") != Some(&accept)
        || !fields
            .get("upgrade")
            .is_some_and(|v| v.eq_ignore_ascii_case("websocket"))
        || !fields.get("connection").is_some_and(|value| {
            value
                .split(',')
                .any(|token| token.trim().eq_ignore_ascii_case("upgrade"))
        })
        || fields.contains_key("sec-websocket-extensions")
    {
        return Err("upstream upgrade identity mismatch".into());
    }
    write!(stream, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: {accept}\r\n\r\n").and_then(|()| stream.flush()).map_err(|e| e.to_string())?;
    stream
        .socket()
        .set_read_timeout(Some(Duration::from_millis(50)))
        .map_err(|e| e.to_string())?;
    upstream
        .get_ref()
        .set_read_timeout(Some(Duration::from_millis(50)))
        .map_err(|e| e.to_string())?;
    let mut client = ws_wire::Reader::default();
    let mut pending = Vec::new();
    let started = Instant::now();
    let mut outstanding = BTreeMap::new();
    let mut subscriptions = BTreeSet::new();
    while started.elapsed() < Duration::from_secs(3600) {
        if let Some((opcode, body)) = client.read(stream)? {
            if super::authenticate_key(config, request).is_err() {
                return ws_wire::write(stream, 8, &1008_u16.to_be_bytes());
            }
            if opcode == 1 {
                let value: Value =
                    serde_json::from_slice(&body).map_err(|_| "invalid subscription request")?;
                let id = value.get("id").ok_or("subscription id required")?;
                if let Some(error) = rpc::invalid_request(&value) {
                    ws_wire::write(
                        stream,
                        1,
                        &serde_json::to_vec(&error).map_err(|e| e.to_string())?,
                    )?;
                    continue;
                }
                if !matches!(
                    value["method"].as_str(),
                    Some("eth_subscribe" | "eth_unsubscribe")
                ) {
                    ws_wire::write(
                        stream,
                        1,
                        &serde_json::to_vec(&rpc::error(
                            id,
                            -32601,
                            "Subscription method required",
                        ))
                        .map_err(|e| e.to_string())?,
                    )?;
                    continue;
                }
                if subscriptions.len() + outstanding.len() >= MAX_SUBSCRIPTIONS
                    && value["method"] == "eth_subscribe"
                {
                    return ws_wire::write(stream, 8, &1008_u16.to_be_bytes());
                }
                if outstanding.len() >= MAX_SUBSCRIPTIONS
                    || outstanding.contains_key(&id.to_string())
                {
                    return ws_wire::write(stream, 8, &1008_u16.to_be_bytes());
                }
                let cancellation = if value["method"] == "eth_unsubscribe" {
                    let args = value["params"]
                        .as_array()
                        .ok_or("unsubscribe params required")?;
                    if args.len() != 1 {
                        return Err("unsubscribe params invalid".into());
                    }
                    let subscription = args[0].as_str().ok_or("unsubscribe id invalid")?;
                    if !subscriptions.contains(subscription) {
                        return Err("unknown subscription".into());
                    }
                    Some(subscription.to_owned())
                } else {
                    None
                };
                outstanding.insert(id.to_string(), cancellation);
            }
            masked_frame(&mut upstream, opcode, &body)?;
            if opcode == 8 {
                return Ok(());
            }
        }
        let mut buffer = [0_u8; 4096];
        match upstream.read(&mut buffer) {
            Ok(0) => return Ok(()),
            Ok(count) => pending.extend_from_slice(&buffer[..count]),
            Err(error)
                if matches!(
                    error.kind(),
                    std::io::ErrorKind::WouldBlock | std::io::ErrorKind::TimedOut
                ) => {}
            Err(error) => return Err(error.to_string()),
        }
        if pending.len() > 69_642 {
            return Err("subscription buffer bound".into());
        }
        while let Some((opcode, body)) = upstream_frame(&mut pending)? {
            if opcode == 1 {
                let value: Value =
                    serde_json::from_slice(&body).map_err(|_| "invalid subscription response")?;
                if let Some(id) = value.get("id") {
                    let cancellation = outstanding
                        .remove(&id.to_string())
                        .ok_or("unmatched subscription response")?;
                    if let Some(subscription) = cancellation {
                        if value["result"] == true {
                            subscriptions.remove(&subscription);
                        }
                    } else if let Some(subscription) = value["result"].as_str() {
                        if subscriptions.len() >= MAX_SUBSCRIPTIONS {
                            return Err("upstream subscription limit".into());
                        }
                        subscriptions.insert(subscription.to_owned());
                    }
                } else if value["method"] != "eth_subscription"
                    || !value["params"]["subscription"]
                        .as_str()
                        .is_some_and(|s| subscriptions.contains(s))
                {
                    return Err("unmatched subscription event".into());
                }
            }
            ws_wire::write(stream, opcode, &body)?;
            if opcode == 8 {
                return Ok(());
            }
        }
    }
    ws_wire::write(stream, 8, &1000_u16.to_be_bytes())
}

const EXPLORER_FRAME_LIMIT: usize = 65_536;
const EXPLORER_MESSAGE_LIMIT: usize = 1_048_576;
const EXPLORER_HEADER_LIMIT: usize = 8192;
const EXPLORER_POLL: Duration = Duration::from_millis(25);
const EXPLORER_WRITE: Duration = Duration::from_secs(2);

fn explorer_token(value: &str) -> bool {
    !value.is_empty()
        && value
            .bytes()
            .all(|byte| byte.is_ascii_alphanumeric() || b"!#$%&'*+-.^_`|~".contains(&byte))
}

fn explorer_header_value(value: &str) -> bool {
    value
        .bytes()
        .all(|byte| byte == b'\t' || (32..=126).contains(&byte))
}

fn explorer_extensions(
    value: &str,
) -> Result<Vec<(String, BTreeMap<String, Option<String>>)>, String> {
    let mut extensions = Vec::new();
    for extension in value.split(',') {
        let mut parts = extension.split(';');
        let name = parts.next().unwrap_or_default().trim();
        if !explorer_token(name) {
            return Err("invalid websocket extension".into());
        }
        let mut parameters = BTreeMap::new();
        for part in parts {
            let (key, value) = part
                .trim()
                .split_once('=')
                .map_or((part.trim(), None), |(key, value)| {
                    (key.trim(), Some(value.trim()))
                });
            let value = value.map(|value| {
                value
                    .strip_prefix('"')
                    .and_then(|v| v.strip_suffix('"'))
                    .unwrap_or(value)
            });
            if !explorer_token(key)
                || value.is_some_and(|value| !explorer_token(value))
                || parameters
                    .insert(key.to_owned(), value.map(str::to_owned))
                    .is_some()
            {
                return Err("invalid websocket extension parameter".into());
            }
        }
        extensions.push((name.to_owned(), parameters));
    }
    Ok(extensions)
}

fn explorer_deflate_parameters(
    parameters: &BTreeMap<String, Option<String>>,
    response: bool,
) -> bool {
    parameters.iter().all(|(name, value)| match name.as_str() {
        "server_no_context_takeover" | "client_no_context_takeover" => value.is_none(),
        "server_max_window_bits" | "client_max_window_bits" => {
            if value.is_none() {
                return !response && name == "client_max_window_bits";
            }
            value.as_deref().is_some_and(|value| {
                matches!(value, "8" | "9" | "10" | "11" | "12" | "13" | "14" | "15")
            })
        }
        _ => false,
    })
}

fn explorer_deflate(offer: Option<&str>, selected: Option<&str>) -> Result<bool, String> {
    let Some(selected) = selected else {
        return Ok(false);
    };
    let offered = explorer_extensions(offer.ok_or("unsolicited websocket extension")?)?;
    let negotiated = explorer_extensions(selected)?;
    if negotiated.len() != 1
        || negotiated[0].0 != "permessage-deflate"
        || !explorer_deflate_parameters(&negotiated[0].1, true)
    {
        return Err("unsupported websocket extension".into());
    }
    let selected = &negotiated[0].1;
    let matches = offered.iter().any(|(name, parameters)| {
        if name != "permessage-deflate" || !explorer_deflate_parameters(parameters, false) {
            return false;
        }
        if parameters.contains_key("server_no_context_takeover")
            && !selected.contains_key("server_no_context_takeover")
        {
            return false;
        }
        if selected.contains_key("client_max_window_bits")
            && !parameters.contains_key("client_max_window_bits")
        {
            return false;
        }
        for name in ["server_max_window_bits", "client_max_window_bits"] {
            if let Some(Some(bound)) = parameters.get(name) {
                let selected_bound = selected
                    .get(name)
                    .and_then(Option::as_deref)
                    .unwrap_or("15");
                if selected_bound.parse::<u8>().ok() > bound.parse::<u8>().ok() {
                    return false;
                }
            }
        }
        true
    });
    if !matches {
        return Err("websocket extension offer mismatch".into());
    }
    Ok(true)
}

fn explorer_upgrade_response(
    raw: &[u8],
    request: &IncomingRequest,
    accept: &str,
) -> Result<(Vec<u8>, bool), String> {
    if raw.len() > EXPLORER_HEADER_LIMIT || !raw.ends_with(b"\r\n\r\n") {
        return Err("upstream websocket header bound".into());
    }
    let text = std::str::from_utf8(raw).map_err(|_| "invalid upstream websocket headers")?;
    let mut lines = text[..text.len() - 4].split("\r\n");
    let status = lines.next().ok_or("absent websocket status")?;
    let mut status_parts = status.splitn(3, ' ');
    if status_parts.next() != Some("HTTP/1.1")
        || status_parts.next() != Some("101")
        || !explorer_header_value(status)
    {
        return Err("upstream websocket upgrade refused".into());
    }
    let mut fields = BTreeMap::new();
    let mut cookies = Vec::new();
    for line in lines {
        let (name, value) = line
            .split_once(':')
            .ok_or("invalid upstream websocket header")?;
        if !explorer_token(name) || !explorer_header_value(value) {
            return Err("invalid upstream websocket header".into());
        }
        let name = name.to_ascii_lowercase();
        if name == "set-cookie" {
            if let Some(cookie) = super::explorer_proxy::scope_cookie(value.trim())? {
                cookies.push(cookie);
            }
            continue;
        }
        if matches!(name.as_str(), "content-length" | "transfer-encoding") {
            return Err("unexpected websocket response body".into());
        }
        if matches!(
            name.as_str(),
            "upgrade"
                | "connection"
                | "sec-websocket-accept"
                | "sec-websocket-protocol"
                | "sec-websocket-extensions"
        ) && fields.insert(name, value.trim().to_owned()).is_some()
        {
            return Err("duplicate upstream websocket header".into());
        }
    }
    if fields.get("sec-websocket-accept").map(String::as_str) != Some(accept)
        || !fields
            .get("upgrade")
            .is_some_and(|value| value.eq_ignore_ascii_case("websocket"))
        || !fields.get("connection").is_some_and(|value| {
            value
                .split(',')
                .any(|value| value.trim().eq_ignore_ascii_case("upgrade"))
        })
    {
        return Err("upstream websocket identity mismatch".into());
    }
    if let Some(selected) = fields.get("sec-websocket-protocol") {
        if !explorer_token(selected)
            || !request
                .headers
                .get("sec-websocket-protocol")
                .is_some_and(|offered| offered.split(',').any(|value| value.trim() == selected))
        {
            return Err("upstream websocket protocol mismatch".into());
        }
    }
    let compressed = explorer_deflate(
        request
            .headers
            .get("sec-websocket-extensions")
            .map(String::as_str),
        fields.get("sec-websocket-extensions").map(String::as_str),
    )?;
    let mut response = format!("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: {accept}\r\n");
    for name in ["sec-websocket-protocol", "sec-websocket-extensions"] {
        if let Some(value) = fields.get(name) {
            response.push_str(&format!("{name}: {value}\r\n"));
        }
    }
    for cookie in cookies {
        response.push_str(&format!("Set-Cookie: {cookie}\r\n"));
    }
    response.push_str("\r\n");
    if response.len() > EXPLORER_HEADER_LIMIT {
        return Err("explorer websocket response header bound".into());
    }
    Ok((response.into_bytes(), compressed))
}

#[derive(Default)]
struct ExplorerFrames {
    bytes: Vec<u8>,
    fragment: Option<(u8, bool, usize)>,
    text_tail: Vec<u8>,
    closed: bool,
}

impl ExplorerFrames {
    fn text(&mut self, body: &[u8], final_frame: bool) -> Result<(), String> {
        let mut text = std::mem::take(&mut self.text_tail);
        text.extend_from_slice(body);
        match std::str::from_utf8(&text) {
            Ok(_) => Ok(()),
            Err(error) if error.error_len().is_none() && !final_frame => {
                self.text_tail
                    .extend_from_slice(&text[error.valid_up_to()..]);
                Ok(())
            }
            Err(_) => Err("invalid websocket text".into()),
        }
    }

    fn frame(
        &mut self,
        masked: bool,
        compression: bool,
    ) -> Result<Option<(Vec<u8>, bool)>, String> {
        if self.bytes.len() < 2 {
            return Ok(None);
        }
        let first = self.bytes[0];
        let opcode = first & 15;
        let final_frame = first & 128 != 0;
        let compressed = first & 64 != 0;
        let short = self.bytes[1] & 127;
        if self.closed
            || first & 48 != 0
            || (self.bytes[1] & 128 != 0) != masked
            || !matches!(opcode, 0 | 1 | 2 | 8 | 9 | 10)
            || (compressed && (!compression || !matches!(opcode, 1 | 2)))
        {
            return Err("invalid explorer websocket frame".into());
        }
        let extended = match short {
            126 => 2,
            127 => 8,
            _ => 0,
        };
        let header = 2 + extended + if masked { 4 } else { 0 };
        if self.bytes.len() < header {
            return Ok(None);
        }
        let length = match short {
            126 => usize::from(u16::from_be_bytes([self.bytes[2], self.bytes[3]])),
            127 => usize::try_from(u64::from_be_bytes(
                self.bytes[2..10]
                    .try_into()
                    .map_err(|_| "websocket length")?,
            ))
            .map_err(|_| "websocket length")?,
            _ => usize::from(short),
        };
        if length > EXPLORER_FRAME_LIMIT
            || (short == 126 && length < 126)
            || (short == 127 && length <= 65_535)
            || (opcode >= 8 && (!final_frame || length > 125))
        {
            return Err("explorer websocket frame bound".into());
        }
        if self.bytes.len() < header + length {
            return Ok(None);
        }
        let body: Vec<u8> = self.bytes[header..header + length]
            .iter()
            .enumerate()
            .map(|(i, byte)| {
                if masked {
                    byte ^ self.bytes[header - 4 + i % 4]
                } else {
                    *byte
                }
            })
            .collect();
        let closed = opcode == 8;
        if opcode >= 8 {
            if closed
                && (length == 1
                    || (length >= 2
                        && (!matches!(u16::from_be_bytes([body[0], body[1]]), 1000..=1003 | 1007..=1014 | 3000..=4999)
                            || std::str::from_utf8(&body[2..]).is_err())))
            {
                return Err("invalid websocket close".into());
            }
        } else {
            let (kind, compressed, previous) = match (opcode, self.fragment) {
                (1 | 2, None) => (opcode, compressed, 0),
                (0, Some(fragment)) => fragment,
                _ => return Err("invalid websocket fragmentation".into()),
            };
            let total = previous
                .checked_add(length)
                .ok_or("websocket message bound")?;
            if total > EXPLORER_MESSAGE_LIMIT {
                return Err("websocket message bound".into());
            }
            if kind == 1 && !compressed {
                self.text(&body, final_frame)?;
            }
            self.fragment = if final_frame {
                None
            } else {
                Some((kind, compressed, total))
            };
        }
        self.closed = closed;
        Ok(Some((
            self.bytes.drain(..header + length).collect(),
            closed,
        )))
    }

    fn read(&mut self, stream: &mut impl Read) -> Result<bool, String> {
        let remaining = (EXPLORER_FRAME_LIMIT + 14)
            .checked_sub(self.bytes.len())
            .ok_or("websocket buffer bound")?;
        if remaining == 0 {
            return Err("websocket buffer bound".into());
        }
        let mut chunk = [0_u8; 4096];
        let capacity = remaining.min(chunk.len());
        match stream.read(&mut chunk[..capacity]) {
            Ok(0) => Err("explorer websocket disconnected".into()),
            Ok(count) => {
                self.bytes.extend_from_slice(&chunk[..count]);
                Ok(true)
            }
            Err(error)
                if matches!(
                    error.kind(),
                    std::io::ErrorKind::TimedOut
                        | std::io::ErrorKind::WouldBlock
                        | std::io::ErrorKind::Interrupted
                ) =>
            {
                Ok(false)
            }
            Err(_) => Err("explorer websocket read failed".into()),
        }
    }
}

fn explorer_write(stream: &mut impl Write, mut bytes: &[u8]) -> Result<(), String> {
    let started = Instant::now();
    while !bytes.is_empty() {
        if started.elapsed() >= EXPLORER_WRITE {
            return Err("explorer websocket write deadline".into());
        }
        let count = stream
            .write(bytes)
            .map_err(|_| "explorer websocket write failed")?;
        if count == 0 {
            return Err("explorer websocket write closed".into());
        }
        bytes = &bytes[count..];
    }
    if started.elapsed() >= EXPLORER_WRITE {
        return Err("explorer websocket write deadline".into());
    }
    stream
        .flush()
        .map_err(|_| "explorer websocket flush failed".into())
}

fn explorer_upgrade_request(
    request: &IncomingRequest,
    endpoint: &http::Endpoint,
    target: &str,
    forwarded: &[(&str, &str)],
) -> Result<Vec<u8>, String> {
    if target.split('?').next() != Some("/socket/v2/websocket")
        || !target.bytes().all(|byte| (33..=126).contains(&byte))
        || target.contains(['#', '\\'])
        || endpoint.host.is_empty()
        || !endpoint
            .host
            .bytes()
            .all(|byte| byte.is_ascii_alphanumeric() || matches!(byte, b'.' | b'-'))
    {
        return Err("invalid explorer websocket target".into());
    }
    let mut fields = BTreeMap::new();
    for (name, value) in &request.headers {
        if name.starts_with("sec-websocket-") {
            if !explorer_token(name) || !explorer_header_value(value) {
                return Err("invalid explorer websocket request header".into());
            }
            fields.insert(name.clone(), value.clone());
        }
    }
    for (name, value) in forwarded {
        let name = name.to_ascii_lowercase();
        if !explorer_token(&name)
            || !explorer_header_value(value)
            || !matches!(
                name.as_str(),
                "origin"
                    | "cookie"
                    | "x-forwarded-host"
                    | "x-forwarded-proto"
                    | "x-forwarded-port"
                    | "x-forwarded-prefix"
                    | "x-forwarded-for"
                    | "forwarded"
            )
        {
            return Err("invalid explorer forwarded header".into());
        }
        fields.insert(name, (*value).to_owned());
    }
    if fields
        .get("sec-websocket-protocol")
        .is_some_and(|value| value.split(',').any(|value| !explorer_token(value.trim())))
    {
        return Err("invalid websocket protocol offer".into());
    }
    if let Some(value) = fields.get("sec-websocket-extensions") {
        explorer_extensions(value)?;
    }
    let host = if endpoint.port == 443 {
        endpoint.host.clone()
    } else {
        format!("{}:{}", endpoint.host, endpoint.port)
    };
    let mut outgoing = format!(
        "GET {target} HTTP/1.1\r\nHost: {host}\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n"
    );
    for (name, value) in fields {
        outgoing.push_str(&format!("{name}: {value}\r\n"));
    }
    outgoing.push_str("\r\n");
    if outgoing.len() > EXPLORER_HEADER_LIMIT {
        return Err("explorer websocket request bound".into());
    }
    Ok(outgoing.into_bytes())
}

pub(super) fn serve_explorer<S: Connection>(
    request: &IncomingRequest,
    endpoint: &http::Endpoint,
    target: &str,
    forwarded: &[(&str, &str)],
    stream: &mut S,
) -> Result<(), String> {
    stream
        .socket()
        .set_write_timeout(Some(EXPLORER_WRITE))
        .map_err(|_| "websocket write timeout")?;
    let Some(accept) = valid_upgrade_origin(request, true) else {
        return http::write_response(
            stream,
            &super::response(400, "invalid_websocket_upgrade", None),
        );
    };
    let outgoing = match explorer_upgrade_request(request, endpoint, target, forwarded) {
        Ok(outgoing) => outgoing,
        Err(_) => {
            return http::write_response(
                stream,
                &super::response(400, "invalid_websocket_upgrade", None),
            )
        }
    };
    let Some(_socket) = SocketGuard::acquire() else {
        return http::write_response(stream, &super::response(429, "subscription_limit", Some(1)));
    };
    let handshake = (|| -> Result<_, String> {
        let mut upstream = http::connect_public_tls(endpoint)?;
        upstream
            .get_ref()
            .set_read_timeout(Some(Duration::from_millis(250)))
            .map_err(|_| "websocket read timeout")?;
        upstream
            .get_ref()
            .set_write_timeout(Some(EXPLORER_WRITE))
            .map_err(|_| "websocket write timeout")?;
        explorer_write(&mut upstream, &outgoing)?;
        let started = Instant::now();
        let mut headers = Vec::new();
        while !headers.ends_with(b"\r\n\r\n") {
            if headers.len() >= EXPLORER_HEADER_LIMIT || started.elapsed() >= Duration::from_secs(8)
            {
                return Err("explorer websocket upgrade bound".into());
            }
            let mut byte = [0_u8; 1];
            match upstream.read(&mut byte) {
                Ok(1) => headers.push(byte[0]),
                Err(error)
                    if matches!(
                        error.kind(),
                        std::io::ErrorKind::TimedOut
                            | std::io::ErrorKind::WouldBlock
                            | std::io::ErrorKind::Interrupted
                    ) =>
                {
                    ()
                }
                _ => return Err("explorer websocket upgrade unavailable".into()),
            }
        }
        let (response, compressed) = explorer_upgrade_response(&headers, request, &accept)?;
        stream
            .socket()
            .set_read_timeout(Some(EXPLORER_POLL))
            .map_err(|_| "websocket read timeout")?;
        upstream
            .get_ref()
            .set_read_timeout(Some(EXPLORER_POLL))
            .map_err(|_| "websocket read timeout")?;
        Ok((upstream, response, compressed))
    })();
    let (mut upstream, response, compressed) = match handshake {
        Ok(handshake) => handshake,
        Err(_) => {
            return http::write_response(
                stream,
                &super::response(502, "explorer_websocket_unavailable", None),
            )
        }
    };
    explorer_write(stream, &response)?;
    let mut client = ExplorerFrames::default();
    let mut server = ExplorerFrames::default();
    let started = Instant::now();
    let mut activity = Instant::now();
    let mut closing: Option<Instant> = None;
    loop {
        if started.elapsed() >= Duration::from_secs(3600)
            || activity.elapsed() >= Duration::from_secs(90)
        {
            masked_frame(&mut upstream, 8, &1000_u16.to_be_bytes())?;
            return ws_wire::write(stream, 8, &1000_u16.to_be_bytes());
        }
        if closing.is_some_and(|started| started.elapsed() >= Duration::from_secs(2)) {
            return Ok(());
        }
        if !client.closed {
            if let Some((frame, closed)) = client.frame(true, compressed)? {
                if server.closed && !closed {
                    return Err("websocket data after close".into());
                }
                explorer_write(&mut upstream, &frame)?;
                activity = Instant::now();
                if closed {
                    closing.get_or_insert_with(Instant::now);
                }
            } else {
                client.read(stream)?;
            }
        }
        if !server.closed {
            if let Some((frame, closed)) = server.frame(false, compressed)? {
                if client.closed && !closed {
                    return Err("websocket data after close".into());
                }
                explorer_write(stream, &frame)?;
                activity = Instant::now();
                if closed {
                    closing.get_or_insert_with(Instant::now);
                }
            } else {
                server.read(&mut upstream)?;
            }
        }
        if client.closed && server.closed {
            return Ok(());
        }
    }
}

#[cfg(test)]
mod explorer_tests {
    use super::*;

    fn request() -> IncomingRequest {
        IncomingRequest {
            method: "GET".into(),
            path: "/explorer/backend/socket/v2/websocket?vsn=2.0.0".into(),
            body: Vec::new(),
            headers: BTreeMap::from([
                ("upgrade".into(), "websocket".into()),
                ("connection".into(), "Upgrade".into()),
                ("sec-websocket-version".into(), "13".into()),
                (
                    "sec-websocket-key".into(),
                    "dGhlIHNhbXBsZSBub25jZQ==".into(),
                ),
            ]),
        }
    }

    fn frame(first: u8, body: &[u8], masked: bool) -> Vec<u8> {
        let mut bytes = Vec::new();
        if masked {
            masked_frame(&mut bytes, first & 15, body).unwrap_or_else(|error| panic!("{error}"));
        } else {
            ws_wire::write(&mut bytes, first & 15, body).unwrap_or_else(|error| panic!("{error}"));
        }
        bytes[0] = first;
        bytes
    }

    fn response(extra: &str) -> Vec<u8> {
        format!("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: keep-alive, Upgrade\r\nSec-WebSocket-Accept: s3pPLMBiTxaQ9kYGzzhZRbK+xOo=\r\n{extra}\r\n").into_bytes()
    }

    #[test]
    fn explorer_request_preserves_query_and_only_filtered_cookies() {
        let mut request = request();
        request.headers.insert(
            "cookie".into(),
            "wallet_session=secret; _explorer_key=allowed".into(),
        );
        request
            .headers
            .insert("origin".into(), "https://untrusted.example".into());
        request
            .headers
            .insert("authorization".into(), "Bearer secret".into());
        request
            .headers
            .insert("x-forwarded-host".into(), "untrusted.example".into());
        request
            .headers
            .insert("sec-websocket-protocol".into(), "phoenix".into());
        let endpoint = http::Endpoint::parse("https://explorer.example")
            .unwrap_or_else(|error| panic!("{error}"));
        let outgoing = explorer_upgrade_request(
            &request,
            &endpoint,
            "/socket/v2/websocket?vsn=2.0.0&token=a%2Bb%26c",
            &[
                ("Cookie", "_explorer_key=allowed"),
                ("Origin", "https://gateway.example"),
                ("X-Forwarded-Host", "gateway.example"),
                ("X-Forwarded-Proto", "https"),
            ],
        )
        .unwrap_or_else(|error| panic!("{error}"));
        let text = String::from_utf8(outgoing).unwrap_or_else(|error| panic!("{error}"));
        assert!(text.starts_with("GET /socket/v2/websocket?vsn=2.0.0&token=a%2Bb%26c HTTP/1.1\r\n"));
        assert!(text.contains("cookie: _explorer_key=allowed\r\n"));
        assert!(text.contains("origin: https://gateway.example\r\n"));
        assert!(text.contains("sec-websocket-protocol: phoenix\r\n"));
        assert!(!text.contains("secret"));
        assert!(!text.contains("untrusted"));
        let outgoing = explorer_upgrade_request(&request, &endpoint, "/socket/v2/websocket", &[])
            .unwrap_or_else(|error| panic!("{error}"));
        let text = String::from_utf8(outgoing).unwrap_or_else(|error| panic!("{error}"));
        assert!(!text.contains("cookie:"));
        assert!(!text.contains("origin:"));
        for target in [
            "/other",
            "//socket/v2/websocket",
            "/socket/v2/websocket?x=bad\r\nInjected: yes",
            "/socket/v2/websocket#fragment",
        ] {
            assert!(explorer_upgrade_request(&request, &endpoint, target, &[]).is_err());
        }
        for header in [
            ("Host", "untrusted.example"),
            ("Cookie", "value\r\nInjected: yes"),
            ("Authorization", "secret"),
        ] {
            assert!(explorer_upgrade_request(
                &request,
                &endpoint,
                "/socket/v2/websocket",
                &[header]
            )
            .is_err());
        }
    }

    #[test]
    fn explorer_upgrade_binds_accept_protocol_and_extensions() {
        let mut request = request();
        request
            .headers
            .insert("sec-websocket-protocol".into(), "phoenix, other".into());
        request.headers.insert(
            "sec-websocket-extensions".into(),
            "permessage-deflate; client_max_window_bits".into(),
        );
        let accept = valid_upgrade_origin(&request, true).unwrap_or_else(|| panic!("upgrade"));
        let (raw, compressed) = explorer_upgrade_response(&response("Sec-WebSocket-Protocol: phoenix\r\nSec-WebSocket-Extensions: permessage-deflate; client_max_window_bits=12\r\n"), &request, &accept).unwrap_or_else(|error| panic!("{error}"));
        assert!(compressed);
        let text = String::from_utf8(raw).unwrap_or_else(|error| panic!("{error}"));
        assert!(text.contains("sec-websocket-protocol: phoenix\r\n"));
        assert!(text.contains(
            "sec-websocket-extensions: permessage-deflate; client_max_window_bits=12\r\n"
        ));
        assert!(explorer_upgrade_response(&response(""), &request, "wrong").is_err());
        for extra in [
            "Sec-WebSocket-Protocol: unsolicited\r\n",
            "Sec-WebSocket-Protocol: phoenix, other\r\n",
            "Sec-WebSocket-Protocol: phoenix\r\nSec-WebSocket-Protocol: phoenix\r\n",
            "Sec-WebSocket-Extensions: unknown\r\n",
            "Sec-WebSocket-Extensions: permessage-deflate; unknown\r\n",
            "Content-Length: 0\r\n",
            "Transfer-Encoding: chunked\r\n",
            " folded: value\r\n",
        ] {
            assert!(
                explorer_upgrade_response(&response(extra), &request, &accept).is_err(),
                "{extra}"
            );
        }
        let valid = response("");
        for invalid in [
            String::from_utf8_lossy(&valid).replace("101 Switching Protocols", "200 OK"),
            String::from_utf8_lossy(&valid).replace("Upgrade: websocket", "Upgrade: h2c"),
            String::from_utf8_lossy(&valid).replace("keep-alive, Upgrade", "close"),
        ] {
            assert!(explorer_upgrade_response(invalid.as_bytes(), &request, &accept).is_err());
        }
        assert!(explorer_upgrade_response(&valid[..valid.len() - 1], &request, &accept).is_err());
    }

    #[test]
    fn explorer_upgrade_scopes_each_allowed_cookie_and_preserves_flags() {
        let request = request();
        let accept = valid_upgrade_origin(&request, true).unwrap_or_else(|| panic!("upgrade"));
        let raw = response("Set-Cookie: _explorer_key=abc; Domain=explorer.example; Path=/; HttpOnly; Secure; SameSite=Lax\r\nSet-Cookie: api_temp_token=def; Path=/api; Max-Age=30; SameSite=Strict\r\nSet-Cookie: wallet_session=discard; Secure\r\n");
        let (raw, _) = explorer_upgrade_response(&raw, &request, &accept)
            .unwrap_or_else(|error| panic!("{error}"));
        let text = String::from_utf8(raw).unwrap_or_else(|error| panic!("{error}"));
        assert_eq!(text.matches("Set-Cookie:").count(), 2);
        assert!(text.contains(
            "Set-Cookie: _explorer_key=abc; HttpOnly; Secure; SameSite=Lax; Path=/explorer\r\n"
        ));
        assert!(text.contains("Set-Cookie: api_temp_token=def; Max-Age=30; SameSite=Strict; Path=/explorer; Secure\r\n"));
        assert!(!text.contains("Domain="));
        assert!(!text.contains("wallet_session"));
        for cookie in [
            "_explorer_key=bad value",
            "_explorer_key=abc; HttpOnly=1",
            "api_temp_token=abc; Secure; Secure",
        ] {
            assert!(explorer_upgrade_response(
                &response(&format!("Set-Cookie: {cookie}\r\n")),
                &request,
                &accept
            )
            .is_err());
        }
    }

    #[test]
    fn explorer_deflate_rejects_unsolicited_or_widened_parameters() {
        assert_eq!(explorer_deflate(None, None), Ok(false));
        assert!(explorer_deflate(None, Some("permessage-deflate")).is_err());
        assert_eq!(
            explorer_deflate(
                Some("permessage-deflate"),
                Some("permessage-deflate; server_no_context_takeover")
            ),
            Ok(true)
        );
        for (offer, selected) in [
            (
                "permessage-deflate",
                "permessage-deflate; client_max_window_bits=12",
            ),
            (
                "permessage-deflate; server_max_window_bits=10",
                "permessage-deflate; server_max_window_bits=11",
            ),
            (
                "permessage-deflate; server_max_window_bits=10",
                "permessage-deflate",
            ),
            (
                "permessage-deflate; server_no_context_takeover",
                "permessage-deflate",
            ),
            (
                "permessage-deflate",
                "permessage-deflate; server_max_window_bits=16",
            ),
            (
                "permessage-deflate",
                "permessage-deflate; server_no_context_takeover=1",
            ),
            (
                "permessage-deflate",
                "permessage-deflate, permessage-deflate",
            ),
            (
                "permessage-deflate",
                "permessage-deflate; server_max_window_bits=12; server_max_window_bits=12",
            ),
        ] {
            assert!(explorer_deflate(Some(offer), Some(selected)).is_err());
        }
    }

    #[test]
    fn explorer_frames_preserve_phoenix_binary_fragmentation_and_control_bytes() {
        for masked in [false, true] {
            let mut reader = ExplorerFrames::default();
            let frames = [
                frame(0x01, b"[null,\"1\",\"phoenix\",\"heartbeat\",", masked),
                frame(0x89, b"ping", masked),
                frame(0x8a, b"pong", masked),
                frame(0x80, b"{}]", masked),
                frame(0x82, &[0, 255, 128, 1], masked),
                frame(0x88, &1000_u16.to_be_bytes(), masked),
            ];
            for (index, bytes) in frames.iter().enumerate() {
                reader.bytes.extend_from_slice(bytes);
                assert_eq!(
                    reader.frame(masked, false),
                    Ok(Some((bytes.clone(), index == frames.len() - 1)))
                );
                assert!(reader.bytes.is_empty());
            }
            reader.bytes.extend(frame(0x81, b"late", masked));
            assert!(reader.frame(masked, false).is_err());
        }
    }

    #[test]
    fn explorer_text_handles_split_utf8_and_frame_read_boundaries() {
        let mut reader = ExplorerFrames::default();
        for bytes in [frame(0x01, &[0xe2], true), frame(0x80, &[0x82, 0xac], true)] {
            for byte in &bytes[..bytes.len() - 1] {
                reader.bytes.push(*byte);
                assert_eq!(reader.frame(true, false), Ok(None));
            }
            reader.bytes.push(bytes[bytes.len() - 1]);
            assert_eq!(reader.frame(true, false), Ok(Some((bytes, false))));
        }
        assert!(reader.text_tail.is_empty());
        let compressed = frame(0xc1, &[0xff, 0xfe, 0x00], false);
        reader.bytes.extend_from_slice(&compressed);
        assert_eq!(reader.frame(false, true), Ok(Some((compressed, false))));
    }

    #[test]
    fn explorer_frames_reject_invalid_masks_lengths_rsv_utf8_and_close() {
        for bytes in [
            frame(0x81, b"unmasked", false),
            frame(0xa1, b"reserved", true),
            frame(0xc1, b"unnegotiated", true),
            frame(0x80, b"orphan", true),
            frame(0x09, b"fragmented ping", true),
            frame(0x89, &[0; 126], true),
            frame(0x81, &[0xff], true),
            frame(0x81, &[0xe2], true),
            frame(0x88, &[1], true),
            frame(0x88, &1005_u16.to_be_bytes(), true),
            frame(0x88, &[3, 232, 255], true),
            vec![0x81, 0xfe, 0, 1, 1, 2, 3, 4],
            vec![0x81, 0xff, 0, 0, 0, 0, 0, 1, 0, 1, 1, 2, 3, 4],
            vec![0x81, 0xff, 128, 0, 0, 0, 0, 0, 0, 0, 1, 2, 3, 4],
        ] {
            assert!(ExplorerFrames {
                bytes,
                ..ExplorerFrames::default()
            }
            .frame(true, false)
            .is_err());
        }
        let mut reader = ExplorerFrames {
            bytes: frame(0x01, b"start", true),
            ..ExplorerFrames::default()
        };
        assert!(reader.frame(true, false).is_ok());
        reader.bytes = frame(0x81, b"interleaved data", true);
        assert!(reader.frame(true, false).is_err());
        let mut reader = ExplorerFrames {
            bytes: frame(0x80, b"x", true),
            fragment: Some((1, false, EXPLORER_MESSAGE_LIMIT)),
            ..ExplorerFrames::default()
        };
        assert!(reader.frame(true, false).is_err());
        let bytes = frame(0x82, &[0; EXPLORER_FRAME_LIMIT], true);
        let mut reader = ExplorerFrames {
            bytes: bytes.clone(),
            ..ExplorerFrames::default()
        };
        assert_eq!(reader.frame(true, false), Ok(Some((bytes, false))));
    }
}
