use super::{http, rpc, ws_wire, Config, IncomingRequest};
use layerx_platform_gateway::evm;
use serde_json::{json, Value};
use std::collections::{BTreeMap, BTreeSet};
use std::io::{Read, Write};
use std::net::TcpStream;
use std::sync::Arc;
use std::time::{Duration, Instant};

const MAX_SUBSCRIBERS: usize = 32;
const MAX_SUBSCRIPTIONS: usize = 8;

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
    serve_evm(config, request, stream)
}

#[derive(Default)]
struct RelaySubscriptions {
    outstanding: BTreeMap<String, Option<String>>,
    active: BTreeSet<String>,
}

impl RelaySubscriptions {
    fn refusal(&self, value: &Value) -> Option<Value> {
        if let Some(error) = rpc::invalid_request(value) {
            return Some(error);
        }
        let id = value.get("id").unwrap_or(&Value::Null);
        if !evm::SUBSCRIPTION_METHODS.contains(&value["method"].as_str().unwrap_or_default()) {
            return Some(rpc::error(id, -32601, "Method not found"));
        }
        if value.get("id").is_none() {
            return Some(rpc::error(&Value::Null, -32600, "Subscription id required"));
        }
        let valid = match value["params"].as_array().map(Vec::as_slice) {
            Some([Value::String(kind)]) if value["method"] == "eth_subscribe" => {
                matches!(
                    kind.as_str(),
                    "newHeads" | "logs" | "newPendingTransactions"
                )
            }
            Some([Value::String(kind), Value::Object(_)]) if value["method"] == "eth_subscribe" => {
                kind == "logs"
            }
            Some([Value::String(subscription)]) if value["method"] == "eth_unsubscribe" => {
                self.active.contains(subscription)
            }
            _ => false,
        };
        if !valid {
            return Some(rpc::error(id, -32602, "Invalid params"));
        }
        if self.outstanding.contains_key(&id.to_string()) {
            return Some(rpc::error(id, -32600, "Request id already pending"));
        }
        if self.outstanding.len() >= MAX_SUBSCRIPTIONS
            || (value["method"] == "eth_subscribe"
                && self.active.len()
                    + self
                        .outstanding
                        .values()
                        .filter(|entry| entry.is_none())
                        .count()
                    >= MAX_SUBSCRIPTIONS)
        {
            return Some(rpc::error(id, -32005, "Subscription limit"));
        }
        None
    }

    fn response(&mut self, value: &Value) -> Result<(), String> {
        if value["jsonrpc"] != "2.0" || !value.is_object() {
            return Err("invalid subscription response".into());
        }
        let Some(id) = value.get("id") else {
            if value["method"] != "eth_subscription"
                || !value["params"]["subscription"]
                    .as_str()
                    .is_some_and(|subscription| self.active.contains(subscription))
                || value["params"].get("result").is_none()
                || value.get("result").is_some()
                || value.get("error").is_some()
            {
                return Err("unmatched subscription event".into());
            }
            return Ok(());
        };
        let key = id.to_string();
        let cancellation = self
            .outstanding
            .get(&key)
            .ok_or("unmatched subscription response")?;
        if value.get("result").is_some() == value.get("error").is_some()
            || value.get("method").is_some()
        {
            return Err("invalid subscription response".into());
        }
        if let Some(error) = value.get("error") {
            if error["code"].as_i64().is_none() || error["message"].as_str().is_none() {
                return Err("invalid subscription error".into());
            }
        } else if let Some(subscription) = cancellation {
            let cancelled = value["result"]
                .as_bool()
                .ok_or("invalid unsubscribe response")?;
            if cancelled {
                self.active.remove(subscription);
            }
        } else {
            let subscription = value["result"]
                .as_str()
                .filter(|id| !id.is_empty())
                .ok_or("invalid subscription identifier")?;
            if self.active.len() >= MAX_SUBSCRIPTIONS || self.active.contains(subscription) {
                return Err("upstream subscription limit or duplicate".into());
            }
            self.active.insert(subscription.to_owned());
        }
        self.outstanding.remove(&key);
        Ok(())
    }
}

fn relay_subscription(
    upstream: &mut impl Write,
    body: &[u8],
    subscriptions: &mut RelaySubscriptions,
) -> Result<Option<Value>, String> {
    let value = match serde_json::from_slice::<Value>(body) {
        Ok(value) => value,
        Err(_) => return Ok(Some(rpc::error(&Value::Null, -32700, "Parse error"))),
    };
    if let Some(refusal) = subscriptions.refusal(&value) {
        return Ok(Some(refusal));
    }
    masked_frame(upstream, 1, body)?;
    subscriptions.outstanding.insert(
        value["id"].to_string(),
        if value["method"] == "eth_unsubscribe" {
            value["params"][0].as_str().map(str::to_owned)
        } else {
            None
        },
    );
    Ok(None)
}

fn poll_subscription(
    upstream: &mut (impl Read + Write),
    stream: &mut impl Write,
    pending: &mut Vec<u8>,
    fragments: &mut Option<Vec<u8>>,
    subscriptions: &mut RelaySubscriptions,
) -> Result<bool, String> {
    let mut buffer = [0_u8; 4096];
    match upstream.read(&mut buffer) {
        Ok(0) => {
            ws_wire::write(stream, 8, &1013_u16.to_be_bytes())?;
            return Ok(false);
        }
        Ok(count) => pending.extend_from_slice(&buffer[..count]),
        Err(error)
            if matches!(
                error.kind(),
                std::io::ErrorKind::WouldBlock
                    | std::io::ErrorKind::TimedOut
                    | std::io::ErrorKind::Interrupted
            ) => {}
        Err(_) => {
            ws_wire::write(stream, 8, &1013_u16.to_be_bytes())?;
            return Ok(false);
        }
    }
    if pending.len() > 69_642 {
        ws_wire::write(stream, 8, &1011_u16.to_be_bytes())?;
        return Ok(false);
    }
    loop {
        let frame = match upstream_frame(pending, fragments) {
            Ok(frame) => frame,
            Err(_) => {
                ws_wire::write(stream, 8, &1011_u16.to_be_bytes())?;
                return Ok(false);
            }
        };
        let Some((opcode, body)) = frame else {
            return Ok(true);
        };
        if opcode == 9 {
            if masked_frame(upstream, 10, &body).is_err() {
                ws_wire::write(stream, 8, &1013_u16.to_be_bytes())?;
                return Ok(false);
            }
            continue;
        }
        if opcode == 10 {
            continue;
        }
        if opcode == 1 {
            let valid = serde_json::from_slice::<Value>(&body)
                .ok()
                .is_some_and(|value| subscriptions.response(&value).is_ok());
            if !valid {
                ws_wire::write(stream, 8, &1011_u16.to_be_bytes())?;
                return Ok(false);
            }
        }
        ws_wire::write(stream, opcode, &body)?;
        if opcode == 8 {
            return Ok(false);
        }
    }
}

fn probe_subscription(
    upstream: &mut (impl Read + Write),
    id: &str,
    method: &str,
    params: &Value,
) -> Result<Value, String> {
    let request = json!({"jsonrpc":"2.0", "id":id, "method":method, "params":params});
    masked_frame(
        upstream,
        1,
        &serde_json::to_vec(&request).map_err(|e| e.to_string())?,
    )?;
    let mut pending = Vec::new();
    let mut fragments = None;
    let started = Instant::now();
    loop {
        if started.elapsed() >= Duration::from_secs(8) {
            return Err("sequencer websocket probe unavailable".into());
        }
        if let Some((opcode, body)) = upstream_frame(&mut pending, &mut fragments)? {
            if opcode == 9 {
                masked_frame(upstream, 10, &body)?;
                continue;
            }
            if opcode == 10 {
                continue;
            }
            if opcode != 1 {
                return Err("sequencer websocket probe closed".into());
            }
            let answer: Value =
                serde_json::from_slice(&body).map_err(|_| "sequencer websocket probe malformed")?;
            if answer["jsonrpc"] != "2.0"
                || answer["id"] != id
                || answer.get("result").is_none()
                || answer.get("error").is_some()
                || !pending.is_empty()
            {
                return Err("sequencer websocket probe refused".into());
            }
            return Ok(answer["result"].clone());
        }
        let mut buffer = [0_u8; 4096];
        match upstream.read(&mut buffer) {
            Ok(0) => return Err("sequencer websocket probe disconnected".into()),
            Ok(count) => pending.extend_from_slice(&buffer[..count]),
            Err(error)
                if matches!(
                    error.kind(),
                    std::io::ErrorKind::WouldBlock
                        | std::io::ErrorKind::TimedOut
                        | std::io::ErrorKind::Interrupted
                ) => {}
            Err(_) => return Err("sequencer websocket probe unavailable".into()),
        }
        if pending.len() > 69_642 {
            return Err("sequencer websocket probe bound".into());
        }
    }
}

fn connect_subscription(
    config: &Config,
    request: &IncomingRequest,
) -> Result<native_tls::TlsStream<TcpStream>, String> {
    let endpoint = std::env::var("LAYERX_GATEWAY_PAXEER_WS_URL")
        .ok()
        .and_then(|value| http::Endpoint::parse(&value).ok())
        .ok_or("sequencer websocket not configured")?;
    if !config
        .paxeer
        .as_ref()
        .is_some_and(|nodes| nodes.iter().any(|node| node.host == endpoint.host))
    {
        return Err("sequencer websocket network unavailable".into());
    }
    let mut upstream = config.client.connect_tls(&endpoint)?;
    let key = request
        .headers
        .get("sec-websocket-key")
        .ok_or("websocket key absent")?;
    upgrade_subscription(&mut upstream, &endpoint, key)?;
    upstream
        .get_ref()
        .set_read_timeout(Some(Duration::from_millis(50)))
        .map_err(|e| e.to_string())?;
    let chain = probe_subscription(
        &mut upstream,
        "subscription-network",
        "eth_chainId",
        &json!([]),
    )?;
    if chain
        .as_str()
        .and_then(|value| value.strip_prefix("0x"))
        .and_then(|value| u64::from_str_radix(value, 16).ok())
        != Some(125)
    {
        return Err("sequencer websocket wrong network".into());
    }
    let pending = probe_subscription(
        &mut upstream,
        "subscription-sequencer",
        "eth_getBlockByNumber",
        &json!(["pending", false]),
    )?;
    if !pending.is_object() {
        return Err("sequencer pending state unavailable".into());
    }
    upstream
        .get_ref()
        .set_write_timeout(Some(Duration::from_secs(2)))
        .map_err(|e| e.to_string())?;
    Ok(upstream)
}

fn upgrade_subscription(
    upstream: &mut (impl Read + Write),
    endpoint: &http::Endpoint,
    key: &str,
) -> Result<(), String> {
    let accept = ws_wire::accept(key).ok_or("invalid websocket key")?;
    let path = if endpoint.base_path.is_empty() {
        "/"
    } else {
        &endpoint.base_path
    };
    write!(upstream, "GET {path} HTTP/1.1\r\nHost: {}:{}\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Key: {key}\r\n\r\n", endpoint.host, endpoint.port)
        .and_then(|()| upstream.flush()).map_err(|e| e.to_string())?;
    let mut headers = Vec::new();
    let started = Instant::now();
    while !headers.ends_with(b"\r\n\r\n") {
        if headers.len() >= 8192 || started.elapsed() >= Duration::from_secs(8) {
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
        || fields.contains_key("sec-websocket-protocol")
        || fields.contains_key("content-length")
        || fields.contains_key("transfer-encoding")
    {
        return Err("upstream upgrade identity mismatch".into());
    }
    Ok(())
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
    stream
        .socket()
        .set_write_timeout(Some(Duration::from_secs(2)))
        .map_err(|e| e.to_string())?;
    write!(stream, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: {accept}\r\n\r\n")
        .and_then(|()| stream.flush()).map_err(|e| e.to_string())?;
    stream
        .socket()
        .set_read_timeout(Some(Duration::from_millis(50)))
        .map_err(|e| e.to_string())?;
    subscription_session(config, request, stream)
}

fn subscription_session<S: Connection>(
    config: &Config,
    request: &IncomingRequest,
    stream: &mut S,
) -> Result<(), String> {
    let mut upstream = None;
    let mut client = ws_wire::Reader::default();
    let mut pending = Vec::new();
    let mut fragments = None;
    let mut subscriptions = RelaySubscriptions::default();
    let started = Instant::now();
    let mut last_input = Instant::now();
    let mut last_ping = Instant::now();
    loop {
        if started.elapsed() >= Duration::from_secs(3600)
            || last_input.elapsed() >= Duration::from_secs(60)
        {
            return ws_wire::write(stream, 8, &1000_u16.to_be_bytes());
        }
        let frame = match client.read(stream) {
            Ok(frame) => frame,
            Err(_) => return ws_wire::write(stream, 8, &1002_u16.to_be_bytes()),
        };
        if let Some((opcode, body)) = frame {
            last_input = Instant::now();
            if super::authenticate_key(config, request).is_err() {
                return ws_wire::write(stream, 8, &1008_u16.to_be_bytes());
            }
            match opcode {
                8 => {
                    if let Some(upstream) = upstream.as_mut() {
                        let _ = masked_frame(upstream, 8, &body);
                    }
                    return ws_wire::write(stream, 8, &body);
                }
                9 => ws_wire::write(stream, 10, &body)?,
                10 => {}
                1 => {
                    if !subscription_text(
                        config,
                        request,
                        stream,
                        &body,
                        &mut upstream,
                        &mut subscriptions,
                    )? {
                        return Ok(());
                    }
                }
                _ => return ws_wire::write(stream, 8, &1002_u16.to_be_bytes()),
            }
        }
        if let Some(connection) = upstream.as_mut() {
            if super::authenticate_key(config, request).is_err() {
                return ws_wire::write(stream, 8, &1008_u16.to_be_bytes());
            }
            if !poll_subscription(
                connection,
                stream,
                &mut pending,
                &mut fragments,
                &mut subscriptions,
            )? {
                return Ok(());
            }
        }
        if last_ping.elapsed() >= Duration::from_secs(5) {
            if super::authenticate_key(config, request).is_err() {
                return ws_wire::write(stream, 8, &1008_u16.to_be_bytes());
            }
            ws_wire::write(stream, 9, b"px")?;
            last_ping = Instant::now();
        }
    }
}

fn subscription_answer(stream: &mut impl Write, answer: &Value) -> Result<(), String> {
    ws_wire::write(
        stream,
        1,
        &serde_json::to_vec(answer).map_err(|e| e.to_string())?,
    )
}

fn subscription_text(
    config: &Config,
    request: &IncomingRequest,
    stream: &mut impl Write,
    body: &[u8],
    upstream: &mut Option<native_tls::TlsStream<TcpStream>>,
    subscriptions: &mut RelaySubscriptions,
) -> Result<bool, String> {
    if !super::public_reads::consume_read() {
        ws_wire::write(stream, 8, &1013_u16.to_be_bytes())?;
        return Ok(false);
    }
    let value = match serde_json::from_slice::<Value>(body) {
        Ok(value) => value,
        Err(_) => {
            subscription_answer(stream, &rpc::error(&Value::Null, -32700, "Parse error"))?;
            return Ok(true);
        }
    };
    let method = value["method"].as_str().unwrap_or_default();
    if request.path == "/rpc/ws"
        && rpc::invalid_request(&value).is_none()
        && !evm::SUBSCRIPTION_METHODS.contains(&method)
        && !matches!(
            method,
            "lx_subscribe" | "lx_unsubscribe" | "lx_subscription"
        )
    {
        if let Some(answer) = rpc::dispatch(config, request, &value) {
            subscription_answer(stream, &answer)?;
        }
        return Ok(true);
    }
    if let Some(refusal) = subscriptions.refusal(&value) {
        if value.get("id").is_some() || rpc::invalid_request(&value).is_some() {
            subscription_answer(stream, &refusal)?;
        }
        return Ok(true);
    }
    if upstream.is_none() {
        match connect_subscription(config, request) {
            Ok(connection) => *upstream = Some(connection),
            Err(_) => {
                subscription_answer(
                    stream,
                    &rpc::error(&value["id"], -32003, "Sequencer unavailable"),
                )?;
                ws_wire::write(stream, 8, &1013_u16.to_be_bytes())?;
                return Ok(false);
            }
        }
    }
    let Some(connection) = upstream.as_mut() else {
        return Err("sequencer websocket unavailable".into());
    };
    match relay_subscription(connection, body, subscriptions) {
        Ok(None) => {}
        Ok(Some(refusal)) => subscription_answer(stream, &refusal)?,
        Err(_) => {
            ws_wire::write(stream, 8, &1013_u16.to_be_bytes())?;
            return Ok(false);
        }
    }
    Ok(true)
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

fn upstream_frame(
    bytes: &mut Vec<u8>,
    fragments: &mut Option<Vec<u8>>,
) -> Result<Option<(u8, Vec<u8>)>, String> {
    loop {
        if bytes.len() < 2 {
            return Ok(None);
        }
        let opcode = bytes[0] & 15;
        let fin = bytes[0] & 0x80 != 0;
        if bytes[0] & 0x70 != 0
            || bytes[1] & 0x80 != 0
            || !matches!(opcode, 0 | 1 | 8 | 9 | 10)
            || (opcode >= 8 && !fin)
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
        if (short == 126 && length < 126) || (short == 127 && length <= 65_535) {
            return Err("noncanonical upstream frame length".into());
        }
        if bytes.len() < header + length {
            return Ok(None);
        }
        let body = bytes[header..header + length].to_vec();
        if opcode == 8
            && (body.len() == 1
                || (body.len() >= 2
                    && (!matches!(u16::from_be_bytes([body[0], body[1]]), 1000..=1003 | 1007..=1014 | 3000..=4999)
                        || std::str::from_utf8(&body[2..]).is_err())))
        {
            return Err("invalid upstream websocket close".into());
        }
        bytes.drain(..header + length);
        if opcode >= 8 {
            return Ok(Some((opcode, body)));
        }
        let mut message = match (opcode, fragments.take()) {
            (1, None) => Vec::new(),
            (0, Some(message)) => message,
            _ => return Err("invalid upstream websocket fragmentation".into()),
        };
        if message.len().saturating_add(body.len()) > 65_536 {
            return Err("upstream message bound".into());
        }
        message.extend_from_slice(&body);
        if !fin {
            *fragments = Some(message);
            continue;
        }
        if std::str::from_utf8(&message).is_err() {
            return Err("invalid upstream websocket text".into());
        }
        return Ok(Some((1, message)));
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::io::Cursor;
    use std::net::{Ipv4Addr, Shutdown, TcpListener};

    fn send(value: &Value, subscriptions: &mut RelaySubscriptions) -> Vec<u8> {
        let body = serde_json::to_vec(value).unwrap_or_else(|error| panic!("{error}"));
        let mut bytes = Vec::new();
        assert_eq!(
            relay_subscription(&mut bytes, &body, subscriptions),
            Ok(None)
        );
        let mut reader = ws_wire::Reader::default();
        assert_eq!(
            reader.read(&mut Cursor::new(bytes.clone())),
            Ok(Some((1, body)))
        );
        bytes
    }

    fn upstream(value: &Value, subscriptions: &mut RelaySubscriptions) -> Vec<u8> {
        let body = serde_json::to_vec(value).unwrap_or_else(|error| panic!("{error}"));
        let mut bytes = Vec::new();
        ws_wire::write(&mut bytes, 1, &body).unwrap_or_else(|error| panic!("{error}"));
        let mut outgoing = Vec::new();
        assert_eq!(
            poll_subscription(
                &mut Cursor::new(bytes.clone()),
                &mut outgoing,
                &mut Vec::new(),
                &mut None,
                subscriptions
            ),
            Ok(true)
        );
        assert_eq!(outgoing, bytes);
        outgoing
    }

    fn subscribe(subscriptions: &mut RelaySubscriptions, id: u64, kind: &str, subscription: &str) {
        send(
            &json!({"jsonrpc":"2.0", "id":id, "method":"eth_subscribe", "params":[kind]}),
            subscriptions,
        );
        upstream(
            &json!({"jsonrpc":"2.0", "id":id, "result":subscription}),
            subscriptions,
        );
    }

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
    fn eth_subscribe_relayed() {
        let mut subscriptions = RelaySubscriptions::default();
        let body = br#"{ "jsonrpc": "2.0", "id": "logs", "method": "eth_subscribe", "params": ["logs", {"address":"0x000000000000000000000000000000000000101A"}] }"#;
        let mut bytes = Vec::new();
        assert_eq!(
            relay_subscription(&mut bytes, body, &mut subscriptions),
            Ok(None)
        );
        assert_eq!(
            ws_wire::Reader::default().read(&mut Cursor::new(bytes)),
            Ok(Some((1, body.to_vec())))
        );
        assert_eq!(
            subscriptions.outstanding,
            BTreeMap::from([(json!("logs").to_string(), None)])
        );
        upstream(
            &json!({"jsonrpc":"2.0", "id":"logs", "result":"0x1"}),
            &mut subscriptions,
        );
        let log = json!({"address":"0x000000000000000000000000000000000000101A",
            "topics":[format!("0x{}", "ab".repeat(32))], "data":"0x0102", "logIndex":"0x0", "seq":"0x2a"});
        let notification = json!({"jsonrpc":"2.0", "method":"eth_subscription",
            "params":{"subscription":"0x1", "result":log}});
        let mut outgoing = upstream(&notification, &mut subscriptions);
        let (_, body) = upstream_frame(&mut outgoing, &mut None)
            .unwrap_or_else(|error| panic!("{error}"))
            .unwrap_or_else(|| panic!("notification"));
        assert_eq!(
            serde_json::from_slice::<Value>(&body).unwrap_or_else(|error| panic!("{error}")),
            notification
        );
        assert_eq!(subscriptions.active, BTreeSet::from(["0x1".to_owned()]));
        assert!(subscriptions.outstanding.is_empty());
    }

    #[test]
    fn eth_subscribe_is_relayed() {
        let mut subscriptions = RelaySubscriptions::default();
        subscribe(&mut subscriptions, 1, "newHeads", "0x1");
        let header = json!({"number":"0x2a", "hash":format!("0x{}", "cd".repeat(32))});
        upstream(
            &json!({"jsonrpc":"2.0", "method":"eth_subscription",
            "params":{"subscription":"0x1", "result":header}}),
            &mut subscriptions,
        );
        assert_eq!(subscriptions.active, BTreeSet::from(["0x1".to_owned()]));
    }

    #[test]
    fn pending_receipts_and_lag_are_relayed_in_receipt_order() {
        let mut subscriptions = RelaySubscriptions::default();
        subscribe(&mut subscriptions, 1, "newPendingTransactions", "0x1");
        let results = [
            json!({"lagged":"0x3"}),
            json!(format!("0x{}", "ab".repeat(32))),
            json!(format!("0x{}", "cd".repeat(32))),
        ];
        let mut bytes = Vec::new();
        for result in &results {
            ws_wire::write(
                &mut bytes,
                1,
                &serde_json::to_vec(&json!({"jsonrpc":"2.0", "method":"eth_subscription",
                "params":{"subscription":"0x1", "result":result}}))
                .unwrap_or_else(|error| panic!("{error}")),
            )
            .unwrap_or_else(|error| panic!("{error}"));
        }
        let mut outgoing = Vec::new();
        assert_eq!(
            poll_subscription(
                &mut Cursor::new(bytes.clone()),
                &mut outgoing,
                &mut Vec::new(),
                &mut None,
                &mut subscriptions
            ),
            Ok(true)
        );
        assert_eq!(outgoing, bytes);
        for result in &results {
            let (_, body) = upstream_frame(&mut outgoing, &mut None)
                .unwrap_or_else(|error| panic!("{error}"))
                .unwrap_or_else(|| panic!("notification"));
            let notification: Value =
                serde_json::from_slice(&body).unwrap_or_else(|error| panic!("{error}"));
            assert_eq!(notification["method"], "eth_subscription");
            assert_eq!(notification["params"]["subscription"], "0x1");
            assert_eq!(&notification["params"]["result"], result);
        }
        assert!(outgoing.is_empty());
        assert_eq!(subscriptions.active, BTreeSet::from(["0x1".to_owned()]));
    }

    #[test]
    fn lx_subscribe_answers_method_not_found() {
        for method in ["lx_subscribe", "lx_unsubscribe", "lx_subscription"] {
            let mut subscriptions = RelaySubscriptions::default();
            let mut bytes = Vec::new();
            let request = json!({"jsonrpc":"2.0", "id":1, "method":method, "params":["receipts"]});
            assert_eq!(
                relay_subscription(
                    &mut bytes,
                    &serde_json::to_vec(&request).unwrap_or_else(|error| panic!("{error}")),
                    &mut subscriptions
                ),
                Ok(Some(
                    json!({"jsonrpc":"2.0", "id":1, "error":{"code":-32601,"message":"Method not found"}})
                ))
            );
            assert!(bytes.is_empty());
            assert!(subscriptions.active.is_empty());
            assert!(subscriptions.outstanding.is_empty());
        }
    }

    #[test]
    fn topics_and_parameters_are_exact() {
        let subscriptions = RelaySubscriptions::default();
        for params in [
            json!(["logs"]),
            json!(["logs", {}]),
            json!(["newHeads"]),
            json!(["newPendingTransactions"]),
        ] {
            assert_eq!(
                subscriptions.refusal(
                    &json!({"jsonrpc":"2.0", "id":1, "method":"eth_subscribe", "params":params})
                ),
                None
            );
        }
        for params in [
            json!([]),
            json!(["receipts"]),
            json!(["checkpoints"]),
            json!(["account", "ab".repeat(32)]),
            json!(["unknown"]),
            json!(["newHeads", {}]),
            json!(["logs", "41"]),
            json!(["logs", {}, "1"]),
            json!({}),
            Value::Null,
        ] {
            assert_eq!(
                subscriptions.refusal(
                    &json!({"jsonrpc":"2.0", "id":1, "method":"eth_subscribe", "params":params})
                ),
                Some(rpc::error(&json!(1), -32602, "Invalid params"))
            );
        }
    }

    #[test]
    fn subscription_limits_include_requests_waiting_for_acknowledgement() {
        let mut subscriptions = RelaySubscriptions::default();
        for id in 0..MAX_SUBSCRIPTIONS {
            send(
                &json!({"jsonrpc":"2.0", "id":id, "method":"eth_subscribe", "params":["logs"]}),
                &mut subscriptions,
            );
        }
        let next = json!({"jsonrpc":"2.0", "id":MAX_SUBSCRIPTIONS, "method":"eth_subscribe", "params":["logs"]});
        assert_eq!(
            subscriptions.refusal(&next),
            Some(rpc::error(
                &json!(MAX_SUBSCRIPTIONS),
                -32005,
                "Subscription limit"
            ))
        );
        for id in 0..MAX_SUBSCRIPTIONS {
            upstream(
                &json!({"jsonrpc":"2.0", "id":id, "result":format!("0x{id:x}")}),
                &mut subscriptions,
            );
        }
        assert_eq!(subscriptions.active.len(), MAX_SUBSCRIPTIONS);
        assert!(subscriptions.outstanding.is_empty());
        assert_eq!(
            subscriptions.refusal(&next),
            Some(rpc::error(
                &json!(MAX_SUBSCRIPTIONS),
                -32005,
                "Subscription limit"
            ))
        );
        send(
            &json!({"jsonrpc":"2.0", "id":"cancel", "method":"eth_unsubscribe", "params":["0x0"]}),
            &mut subscriptions,
        );
        upstream(
            &json!({"jsonrpc":"2.0", "id":"cancel", "result":true}),
            &mut subscriptions,
        );
        assert_eq!(subscriptions.active.len(), MAX_SUBSCRIPTIONS - 1);
        send(&next, &mut subscriptions);
        assert_eq!(subscriptions.outstanding.len(), 1);
    }

    #[test]
    fn unsubscribe_releases_one_identifier_without_renumbering_the_others() {
        let mut subscriptions = RelaySubscriptions::default();
        subscribe(&mut subscriptions, 1, "logs", "0x1");
        subscribe(&mut subscriptions, 2, "newHeads", "0x2");
        let request =
            json!({"jsonrpc":"2.0", "id":3, "method":"eth_unsubscribe", "params":["0x1"]});
        send(&request, &mut subscriptions);
        upstream(
            &json!({"jsonrpc":"2.0", "id":3, "result":false}),
            &mut subscriptions,
        );
        assert_eq!(
            subscriptions.active,
            BTreeSet::from(["0x1".to_owned(), "0x2".to_owned()])
        );
        send(&request, &mut subscriptions);
        upstream(
            &json!({"jsonrpc":"2.0", "id":3, "result":true}),
            &mut subscriptions,
        );
        assert_eq!(subscriptions.active, BTreeSet::from(["0x2".to_owned()]));
        assert!(subscriptions.outstanding.is_empty());
        assert_eq!(
            subscriptions.refusal(&request),
            Some(rpc::error(&json!(3), -32602, "Invalid params"))
        );
    }

    #[test]
    fn upstream_errors_are_unchanged_and_release_the_request_id() {
        let mut subscriptions = RelaySubscriptions::default();
        let request = json!({"jsonrpc":"2.0", "id":1, "method":"eth_subscribe", "params":["logs"]});
        send(&request, &mut subscriptions);
        assert_eq!(
            subscriptions.refusal(&request),
            Some(rpc::error(&json!(1), -32600, "Request id already pending"))
        );
        upstream(
            &json!({"jsonrpc":"2.0", "id":1, "error":{"code":-32000,
            "message":"pending served by sequencer", "data":{"reason":"unavailable"}}}),
            &mut subscriptions,
        );
        assert!(subscriptions.active.is_empty());
        assert!(subscriptions.outstanding.is_empty());
        assert_eq!(subscriptions.refusal(&request), None);
    }

    #[test]
    fn malformed_requests_are_refused_without_writing_upstream() {
        for (body, code) in [
            (b"{".as_slice(), -32700),
            (b"[]".as_slice(), -32600),
            (
                br#"{"jsonrpc":"2.0","id":false,"method":"eth_subscribe"}"#.as_slice(),
                -32600,
            ),
            (
                br#"{"jsonrpc":"2.0","method":"eth_subscribe","params":["logs"]}"#.as_slice(),
                -32600,
            ),
        ] {
            let mut bytes = Vec::new();
            let mut subscriptions = RelaySubscriptions::default();
            let answer = relay_subscription(&mut bytes, body, &mut subscriptions)
                .unwrap_or_else(|error| panic!("{error}"))
                .unwrap_or_else(|| panic!("refusal"));
            assert_eq!(answer["error"]["code"], code);
            assert!(bytes.is_empty());
            assert!(subscriptions.outstanding.is_empty());
        }
    }

    #[test]
    fn upstream_loss_closes_subscriptions_without_replay() {
        let listener =
            TcpListener::bind((Ipv4Addr::LOCALHOST, 0)).unwrap_or_else(|error| panic!("{error}"));
        let mut upstream = TcpStream::connect(
            listener
                .local_addr()
                .unwrap_or_else(|error| panic!("{error}")),
        )
        .unwrap_or_else(|error| panic!("{error}"));
        let (peer, _) = listener.accept().unwrap_or_else(|error| panic!("{error}"));
        upstream
            .set_read_timeout(Some(Duration::from_secs(2)))
            .unwrap_or_else(|error| panic!("{error}"));
        let mut subscriptions = RelaySubscriptions::default();
        subscribe(&mut subscriptions, 1, "logs", "0x1");
        peer.shutdown(Shutdown::Write)
            .unwrap_or_else(|error| panic!("{error}"));
        let mut outgoing = Vec::new();
        assert_eq!(
            poll_subscription(
                &mut upstream,
                &mut outgoing,
                &mut Vec::new(),
                &mut None,
                &mut subscriptions
            ),
            Ok(false)
        );
        assert_eq!(
            upstream_frame(&mut outgoing, &mut None),
            Ok(Some((8, 1013_u16.to_be_bytes().to_vec())))
        );
        assert!(outgoing.is_empty());
        assert!(subscriptions.outstanding.is_empty());
    }

    #[test]
    fn upstream_close_is_forwarded_with_its_exact_code_and_reason() {
        let mut bytes = Vec::new();
        let mut body = 1001_u16.to_be_bytes().to_vec();
        body.extend_from_slice(b"going away");
        ws_wire::write(&mut bytes, 8, &body).unwrap_or_else(|error| panic!("{error}"));
        let mut outgoing = Vec::new();
        assert_eq!(
            poll_subscription(
                &mut Cursor::new(bytes.clone()),
                &mut outgoing,
                &mut Vec::new(),
                &mut None,
                &mut RelaySubscriptions::default()
            ),
            Ok(false)
        );
        assert_eq!(outgoing, bytes);
        assert_eq!(
            upstream_frame(&mut outgoing, &mut None),
            Ok(Some((8, body)))
        );
        assert!(outgoing.is_empty());
    }

    #[test]
    fn unmatched_or_malformed_upstream_frames_close_the_client() {
        let mut values = vec![
            json!({"jsonrpc":"2.0", "id":2, "result":"0x2"}),
            json!({"jsonrpc":"2.0", "id":1, "result":"0x1", "error":{"code":-32000,"message":"refused"}}),
            json!({"jsonrpc":"2.0", "id":1, "error":{"code":"-32000", "message":"refused"}}),
            json!({"jsonrpc":"2.0", "id":1, "result":false}),
            json!({"jsonrpc":"2.0", "method":"lx_subscription", "params":{"subscription":"0x1","result":{}}}),
            json!({"jsonrpc":"2.0", "method":"eth_subscription", "params":{"subscription":"0x2","result":{}}}),
            json!({"jsonrpc":"2.0", "method":"eth_subscription", "params":{"subscription":"0x1"}}),
        ];
        values.push(json!({"jsonrpc":"1.0", "id":1, "result":"0x1"}));
        for value in values {
            let mut subscriptions = RelaySubscriptions::default();
            subscribe(&mut subscriptions, 0, "logs", "0x1");
            send(
                &json!({"jsonrpc":"2.0", "id":1, "method":"eth_subscribe", "params":["logs"]}),
                &mut subscriptions,
            );
            let mut bytes = Vec::new();
            ws_wire::write(
                &mut bytes,
                1,
                &serde_json::to_vec(&value).unwrap_or_else(|error| panic!("{error}")),
            )
            .unwrap_or_else(|error| panic!("{error}"));
            let mut outgoing = Vec::new();
            assert_eq!(
                poll_subscription(
                    &mut Cursor::new(bytes),
                    &mut outgoing,
                    &mut Vec::new(),
                    &mut None,
                    &mut subscriptions
                ),
                Ok(false)
            );
            assert_eq!(
                upstream_frame(&mut outgoing, &mut None),
                Ok(Some((8, 1011_u16.to_be_bytes().to_vec())))
            );
            assert!(outgoing.is_empty());
        }
    }

    #[test]
    fn upstream_frames_reject_invalid_masks_lengths_text_and_close() {
        for mut bytes in [
            vec![0x81, 0x80, 0, 0, 0, 0],
            vec![0x80, 0],
            vec![0x09, 0],
            vec![0x01, 0, 0x81, 0],
            vec![0x81, 126, 0, 1, 0],
            vec![0x81, 127, 0, 0, 0, 0, 0, 0, 0, 1],
            vec![0x81, 1, 255],
            vec![0x88, 1, 0],
            vec![0x88, 2, 3, 237],
            vec![0x88, 3, 3, 232, 255],
            vec![0x89, 126, 0, 126],
            vec![0x81, 127, 0, 0, 0, 0, 0, 1, 0, 1],
        ] {
            assert!(upstream_frame(&mut bytes, &mut None).is_err());
        }
        let mut bytes = Vec::new();
        ws_wire::write(&mut bytes, 1, &[b'x'; 65_536]).unwrap_or_else(|error| panic!("{error}"));
        assert_eq!(
            upstream_frame(&mut bytes, &mut None),
            Ok(Some((1, vec![b'x'; 65_536])))
        );
        assert!(bytes.is_empty());
    }

    fn server_frame(first: u8, body: &[u8]) -> Vec<u8> {
        let mut bytes = Vec::new();
        ws_wire::write(&mut bytes, 0, body).unwrap_or_else(|error| panic!("{error}"));
        bytes[0] = first;
        bytes
    }

    fn pending_block() -> Value {
        let hashes: Vec<_> = (0..40_u8).map(|index| format!("0x{index:064x}")).collect();
        json!({"number":"0x2a", "hash":Value::Null, "transactions":hashes})
    }

    #[test]
    fn fragmented_upstream_messages_are_reassembled_within_the_message_bound() {
        let text = "é".repeat(700).into_bytes();
        let mut bytes = server_frame(0x01, &text[..701]);
        bytes.extend(server_frame(0x89, b"px"));
        bytes.extend(server_frame(0x00, &text[701..1001]));
        bytes.extend(server_frame(0x80, &text[1001..]));
        let mut fragments = None;
        assert_eq!(
            upstream_frame(&mut bytes, &mut fragments),
            Ok(Some((9, b"px".to_vec())))
        );
        assert_eq!(fragments.as_deref(), Some(&text[..701]));
        assert_eq!(
            upstream_frame(&mut bytes, &mut fragments),
            Ok(Some((1, text.clone())))
        );
        assert!(bytes.is_empty());
        assert!(fragments.is_none());
        let mut bytes = server_frame(0x01, &[b'x'; 65_535]);
        bytes.extend(server_frame(0x80, b"x"));
        assert_eq!(
            upstream_frame(&mut bytes, &mut None),
            Ok(Some((1, vec![b'x'; 65_536])))
        );
        let mut bytes = server_frame(0x01, &[b'x'; 65_536]);
        bytes.extend(server_frame(0x80, b"x"));
        assert_eq!(
            upstream_frame(&mut bytes, &mut None),
            Err("upstream message bound".into())
        );
        let mut bytes = server_frame(0x01, &text[..1]);
        bytes.extend(server_frame(0x80, &text[1..2]));
        bytes.extend(server_frame(0x81, b"{}"));
        let mut fragments = None;
        assert_eq!(
            upstream_frame(&mut bytes, &mut fragments),
            Ok(Some((1, text[..2].to_vec())))
        );
        assert_eq!(
            upstream_frame(&mut bytes, &mut fragments),
            Ok(Some((1, b"{}".to_vec())))
        );
        let mut bytes = server_frame(0x01, &text[..1]);
        bytes.extend(server_frame(0x81, b"{}"));
        assert_eq!(
            upstream_frame(&mut bytes, &mut None),
            Err("invalid upstream websocket fragmentation".into())
        );
    }

    #[test]
    fn pending_block_probe_accepts_fragmented_replies() {
        let block = pending_block();
        let mut reply = serde_json::to_vec(
            &json!({"jsonrpc":"2.0", "id":"subscription-sequencer", "result":block}),
        )
        .unwrap_or_else(|error| panic!("{error}"));
        reply.push(b'\n');
        assert!(reply.len() > 2 * 1024);
        let mut buffered = Vec::new();
        for (index, chunk) in reply.chunks(1024).enumerate() {
            let opcode = u8::from(index == 0);
            let fin = if (index + 1) * 1024 >= reply.len() {
                0x80
            } else {
                0
            };
            buffered.extend(server_frame(fin | opcode, chunk));
            if index == 0 {
                buffered.extend(server_frame(0x89, b"px"));
            }
        }
        let mut unbuffered = server_frame(0x01, &reply);
        unbuffered.extend(server_frame(0x80, &[]));
        for frames in [buffered, unbuffered] {
            let listener = TcpListener::bind((Ipv4Addr::LOCALHOST, 0))
                .unwrap_or_else(|error| panic!("{error}"));
            let mut upstream = TcpStream::connect(
                listener
                    .local_addr()
                    .unwrap_or_else(|error| panic!("{error}")),
            )
            .unwrap_or_else(|error| panic!("{error}"));
            let (mut peer, _) = listener.accept().unwrap_or_else(|error| panic!("{error}"));
            upstream
                .set_read_timeout(Some(Duration::from_millis(50)))
                .unwrap_or_else(|error| panic!("{error}"));
            peer.set_read_timeout(Some(Duration::from_secs(2)))
                .unwrap_or_else(|error| panic!("{error}"));
            peer.write_all(&frames)
                .unwrap_or_else(|error| panic!("{error}"));
            assert_eq!(
                probe_subscription(
                    &mut upstream,
                    "subscription-sequencer",
                    "eth_getBlockByNumber",
                    &json!(["pending", false]),
                ),
                Ok(block.clone())
            );
            let mut reader = ws_wire::Reader::default();
            let (opcode, request) = reader
                .read(&mut peer)
                .unwrap_or_else(|error| panic!("{error}"))
                .unwrap_or_else(|| panic!("probe request"));
            assert_eq!(opcode, 1);
            assert_eq!(
                serde_json::from_slice::<Value>(&request).unwrap_or_else(|error| panic!("{error}")),
                json!({"jsonrpc":"2.0", "id":"subscription-sequencer",
                    "method":"eth_getBlockByNumber", "params":["pending", false]})
            );
        }
    }

    #[test]
    fn fragmented_notifications_are_relayed_as_one_message() {
        let mut subscriptions = RelaySubscriptions::default();
        subscribe(&mut subscriptions, 1, "newHeads", "0x1");
        let notification =
            serde_json::to_vec(&json!({"jsonrpc":"2.0", "method":"eth_subscription",
            "params":{"subscription":"0x1", "result":pending_block()}}))
            .unwrap_or_else(|error| panic!("{error}"));
        let mut bytes = server_frame(0x01, &notification[..1024]);
        bytes.extend(server_frame(0x80, &notification[1024..]));
        let mut outgoing = Vec::new();
        let mut fragments = None;
        assert_eq!(
            poll_subscription(
                &mut Cursor::new(bytes),
                &mut outgoing,
                &mut Vec::new(),
                &mut fragments,
                &mut subscriptions
            ),
            Ok(true)
        );
        assert!(fragments.is_none());
        let mut expected = Vec::new();
        ws_wire::write(&mut expected, 1, &notification).unwrap_or_else(|error| panic!("{error}"));
        assert_eq!(outgoing, expected);
    }
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
