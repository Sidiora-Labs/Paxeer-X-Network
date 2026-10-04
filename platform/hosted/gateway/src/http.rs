use crate::pay_timing;
use native_tls::{Certificate, Identity, TlsConnector, TlsStream};
use std::collections::BTreeMap;
use std::fmt::Write as _;
use std::io::{Read, Write};
use std::net::{IpAddr, TcpStream, ToSocketAddrs};
use std::sync::{Mutex, OnceLock};
use std::time::{Duration, Instant};
use zeroize::Zeroize;

const CONNECT_TIMEOUT: Duration = Duration::from_secs(3);
const IO_TIMEOUT: Duration = Duration::from_secs(8);
const MAX_HEADERS: usize = 32 * 1024;
const MAX_RESPONSE: usize = 8 * 1024 * 1024;
const MAX_IDLE_CONNECTIONS_PER_ENDPOINT: usize = 8;
const MAX_IDLE_AGE: Duration =
    Duration::from_secs(layerx_platform_internal::http::IO_TIMEOUT.as_secs() / 2);

#[derive(Clone)]
pub struct Endpoint {
    pub host: String,
    pub port: u16,
    pub base_path: String,
}

impl Endpoint {
    /// # Errors
    /// Refuses noncanonical HTTPS endpoints or invalid DNS names and ports.
    pub fn parse(value: &str) -> Result<Self, String> {
        let rest = value
            .strip_prefix("https://")
            .ok_or_else(|| "component endpoint must use HTTPS".to_owned())?;
        let (authority, path) = rest
            .split_once('/')
            .map_or((rest, ""), |(authority, path)| (authority, path));
        if authority.is_empty()
            || authority.contains(['@', '?', '#', '\\'])
            || path.contains(['?', '#', '\\'])
        {
            return Err("component endpoint is not canonical".to_owned());
        }
        let (host, port) = authority.rsplit_once(':').map_or_else(
            || Ok::<_, String>((authority.to_owned(), 443)),
            |(host, port)| {
                Ok((
                    host.to_owned(),
                    port.parse::<u16>()
                        .map_err(|_| "component endpoint port is invalid".to_owned())?,
                ))
            },
        )?;
        if host.is_empty() || host.parse::<IpAddr>().is_ok() {
            return Err("component TLS endpoint must use a DNS name".to_owned());
        }
        let base_path = if path.is_empty() {
            String::new()
        } else {
            format!("/{}", path.trim_end_matches('/'))
        };
        Ok(Self {
            host,
            port,
            base_path,
        })
    }

    fn authority(&self) -> String {
        if self.port == 443 {
            self.host.clone()
        } else {
            format!("{}:{}", self.host, self.port)
        }
    }
}

pub struct Client {
    ca: Certificate,
    identity: Option<Identity>,
    connector: OnceLock<Result<TlsConnector, String>>,
    idle: Mutex<BTreeMap<String, Vec<IdleConnection>>>,
}

struct IdleConnection {
    stream: TlsStream<TcpStream>,
    retained_at: Instant,
}

impl IdleConnection {
    fn reusable(&self, now: Instant) -> bool {
        now.checked_duration_since(self.retained_at)
            .is_some_and(|age| age < MAX_IDLE_AGE)
    }
}

pub struct OutboundRequest<'a> {
    pub method: &'a str,
    pub path: &'a str,
    pub idempotency: Option<&'a str>,
    pub content_type: &'a str,
    pub body: &'a [u8],
}

#[derive(Clone, Copy, Default)]
struct OutboundHeaders<'a> {
    trace: Option<&'a str>,
    freshness: Option<(u64, Option<[u8; 32]>)>,
    publication_key: Option<&'a str>,
    query: Option<&'a str>,
    forwarded: &'a [(&'a str, &'a str)],
    migration_source: Option<(&'a str, &'a [u8; 32])>,
}

impl Client {
    pub(super) fn independent(&self) -> Self {
        Self {
            ca: self.ca.clone(),
            identity: self.identity.clone(),
            connector: OnceLock::new(),
            idle: Mutex::new(BTreeMap::new()),
        }
    }

    #[must_use]
    pub fn new(ca: Certificate, identity: Identity) -> Self {
        Self {
            ca,
            identity: Some(identity),
            connector: OnceLock::new(),
            idle: Mutex::new(BTreeMap::new()),
        }
    }

    #[must_use]
    pub fn without_identity(ca: Certificate) -> Self {
        Self {
            ca,
            identity: None,
            connector: OnceLock::new(),
            idle: Mutex::new(BTreeMap::new()),
        }
    }

    pub fn connect_tls(&self, endpoint: &Endpoint) -> Result<TlsStream<TcpStream>, String> {
        let mut failure = "upstream unavailable".to_owned();
        for address in (endpoint.host.as_str(), endpoint.port)
            .to_socket_addrs()
            .map_err(|_| "upstream resolution failed")?
            .take(8)
        {
            match TcpStream::connect_timeout(&address, CONNECT_TIMEOUT) {
                Ok(tcp) => {
                    tcp.set_read_timeout(Some(IO_TIMEOUT))
                        .map_err(|e| e.to_string())?;
                    tcp.set_write_timeout(Some(IO_TIMEOUT))
                        .map_err(|e| e.to_string())?;
                    return self
                        .connector()?
                        .connect(&endpoint.host, tcp)
                        .map_err(|_| "upstream TLS refused".to_owned());
                }
                Err(error) => failure = error.to_string(),
            }
        }
        Err(failure)
    }

    fn connector(&self) -> Result<&TlsConnector, String> {
        match self.connector.get_or_init(|| {
            let mut builder = TlsConnector::builder();
            builder
                .add_root_certificate(self.ca.clone())
                .min_protocol_version(Some(native_tls::Protocol::Tlsv12));
            if let Some(identity) = &self.identity {
                builder.identity(identity.clone());
            }
            builder.build().map_err(|error| error.to_string())
        }) {
            Ok(connector) => Ok(connector),
            Err(error) => Err(error.clone()),
        }
    }

    fn take_idle(&self, pool_key: &str) -> Result<Option<TlsStream<TcpStream>>, String> {
        let mut idle = self
            .idle
            .lock()
            .map_err(|_| "gateway HTTP connection pool is unavailable".to_owned())?;
        let now = Instant::now();
        let Some(connections) = idle.get_mut(pool_key) else {
            return Ok(None);
        };
        while let Some(connection) = connections.pop() {
            if connection.reusable(now) {
                return Ok(Some(connection.stream));
            }
        }
        Ok(None)
    }

    fn retain_idle(&self, pool_key: &str, stream: TlsStream<TcpStream>) -> Result<(), String> {
        let mut idle = self
            .idle
            .lock()
            .map_err(|_| "gateway HTTP connection pool is unavailable".to_owned())?;
        let now = Instant::now();
        let connections = idle.entry(pool_key.to_owned()).or_default();
        connections.retain(|connection| connection.reusable(now));
        if connections.len() < MAX_IDLE_CONNECTIONS_PER_ENDPOINT {
            connections.push(IdleConnection {
                stream,
                retained_at: now,
            });
        }
        Ok(())
    }

    /// # Errors
    /// Refuses requests outside the configured bounds and TLS or HTTP failures.
    pub fn request(
        &self,
        endpoint: &Endpoint,
        bearer: &str,
        request: &OutboundRequest<'_>,
    ) -> Result<UpstreamResponse, String> {
        self.request_authorized(endpoint, &format!("Bearer {bearer}"), request)
    }

    /// Sends one bounded request to a component that authenticates no caller,
    /// such as the Paxeer boundary's public EVM JSON-RPC relay. The mutually
    /// authenticated TLS identity is still presented; no bearer credential is
    /// disclosed to a component that has no use for one.
    ///
    /// # Errors
    /// Refuses requests outside the configured bounds and TLS or HTTP failures.
    pub fn request_unauthenticated(
        &self,
        endpoint: &Endpoint,
        request: &OutboundRequest<'_>,
    ) -> Result<UpstreamResponse, String> {
        self.request_authorized(endpoint, "", request)
    }

    /// Sends one bounded request with the supplied authorization value. An
    /// empty value sends no `Authorization` header at all.
    ///
    /// # Errors
    /// Refuses requests outside the configured bounds and TLS or HTTP failures.
    pub fn request_authorized(
        &self,
        endpoint: &Endpoint,
        authorization: &str,
        request: &OutboundRequest<'_>,
    ) -> Result<UpstreamResponse, String> {
        self.request_authorized_traced(endpoint, authorization, request, None)
    }

    /// Propagates the ingress trace identifier unchanged across the boundary.
    ///
    /// # Errors
    /// Refuses invalid traces, requests outside bounds and TLS or HTTP failures.
    pub fn request_authorized_traced(
        &self,
        endpoint: &Endpoint,
        authorization: &str,
        request: &OutboundRequest<'_>,
        trace: Option<&str>,
    ) -> Result<UpstreamResponse, String> {
        self.request_with_freshness(
            endpoint,
            authorization,
            request,
            OutboundHeaders {
                trace,
                ..OutboundHeaders::default()
            },
        )
    }

    pub fn request_migration_source_settlement(
        &self,
        endpoint: &Endpoint,
        service_authorization: &str,
        request: &OutboundRequest<'_>,
        trace: Option<&str>,
        customer_authorization: &str,
        expected_did: &[u8; 32],
    ) -> Result<UpstreamResponse, String> {
        self.request_with_freshness(
            endpoint,
            service_authorization,
            request,
            OutboundHeaders {
                trace,
                migration_source: Some((customer_authorization, expected_did)),
                ..OutboundHeaders::default()
            },
        )
    }

    /// Forwards one bounded request together with the caller's publication key,
    /// which the component resolves at the identity authority.
    ///
    /// # Errors
    /// Refuses invalid publication keys, requests outside the configured bounds
    /// and TLS or HTTP failures.
    pub fn request_with_publication_key(
        &self,
        endpoint: &Endpoint,
        bearer: &str,
        request: &OutboundRequest<'_>,
        trace: Option<&str>,
        publication_key: &str,
    ) -> Result<UpstreamResponse, String> {
        self.request_with_freshness(
            endpoint,
            &format!("Bearer {bearer}"),
            request,
            OutboundHeaders {
                trace,
                publication_key: Some(publication_key),
                ..OutboundHeaders::default()
            },
        )
    }

    /// Sends one bounded program read that the component answers only at or
    /// after `minimum_sequence`, and only against `expected_state_root` when
    /// one is given.
    ///
    /// # Errors
    /// Refuses requests outside the configured bounds and TLS or HTTP failures.
    pub fn request_program_read(
        &self,
        endpoint: &Endpoint,
        bearer: &str,
        request: &OutboundRequest<'_>,
        minimum_sequence: u64,
        expected_state_root: Option<[u8; 32]>,
    ) -> Result<UpstreamResponse, String> {
        self.request_with_freshness(
            endpoint,
            &format!("Bearer {bearer}"),
            request,
            OutboundHeaders {
                freshness: Some((minimum_sequence, expected_state_root)),
                ..OutboundHeaders::default()
            },
        )
    }

    /// Sends one unauthenticated `GET` whose query the caller composed from
    /// percent-encoded pairs, such as a paged read from the history indexer.
    ///
    /// # Errors
    /// Refuses a query outside the percent-encoded pair alphabet, requests
    /// outside the configured bounds and TLS or HTTP failures.
    pub fn get_with_query(
        &self,
        endpoint: &Endpoint,
        path: &str,
        query: &str,
    ) -> Result<UpstreamResponse, String> {
        if !query_is_canonical(query) {
            return Err("outbound query exceeds its boundary".to_owned());
        }
        self.request_with_freshness(
            endpoint,
            "",
            &OutboundRequest {
                method: "GET",
                path,
                idempotency: None,
                content_type: "application/json",
                body: &[],
            },
            OutboundHeaders {
                query: (!query.is_empty()).then_some(query),
                ..OutboundHeaders::default()
            },
        )
    }

    pub fn request_forwarded(
        &self,
        endpoint: &Endpoint,
        authorization: &str,
        request: &OutboundRequest<'_>,
        forwarded: &[(&str, &str)],
    ) -> Result<UpstreamResponse, String> {
        let (path, query) = split_target(request.path)?;
        let request = OutboundRequest { path, ..*request };
        self.request_with_freshness(
            endpoint,
            authorization,
            &request,
            OutboundHeaders {
                query,
                forwarded,
                ..OutboundHeaders::default()
            },
        )
    }

    pub fn stream_forwarded(
        &self,
        endpoint: &Endpoint,
        authorization: &str,
        request: &OutboundRequest<'_>,
        forwarded: &[(&str, &str)],
        downstream: &mut impl Write,
        origin: Option<&str>,
    ) -> Result<(), String> {
        let (path, query) = split_target(request.path)?;
        let request = OutboundRequest { path, ..*request };
        let outbound = OutboundHeaders {
            query,
            forwarded,
            ..OutboundHeaders::default()
        };
        check_outbound_boundary(authorization, &request, outbound)?;
        if request.method != "GET" || !request.body.is_empty() {
            return Err("stream request is invalid".to_owned());
        }
        if origin.is_some_and(|value| {
            value.is_empty() || !value.bytes().all(|byte| byte.is_ascii_graphic())
        }) {
            return Err("invalid stream origin".to_owned());
        }
        let mut upstream = self.connect_tls(endpoint)?;
        send_request(&mut upstream, endpoint, authorization, &request, outbound)?;
        let started = Instant::now();
        let head = read_head(&mut upstream, MAX_RESPONSE, true, started)?;
        let mut parts = head.0.split_whitespace();
        if parts.next() != Some("HTTP/1.1") {
            return Err("invalid stream HTTP version".to_owned());
        }
        let status = parts
            .next()
            .ok_or("missing stream status")?
            .parse::<u16>()
            .map_err(|_| "invalid stream status")?;
        let content_type = head
            .1
            .iter()
            .find(|(name, _)| name == "content-type")
            .map_or("", |(_, value)| value.as_str());
        if status != 200 || content_type.split(';').next() != Some("text/event-stream") {
            let response = response_parts(read_message_body(
                &mut upstream,
                MAX_RESPONSE,
                true,
                started,
                head,
            )?)?;
            return write_response_connection_with_origin(
                downstream,
                &OutgoingResponse {
                    status: response.status,
                    content_type: response.content_type,
                    headers: response.headers,
                    body: response.body,
                    retry_after: None,
                },
                false,
                origin,
            );
        }
        let mut headers = format!("HTTP/1.1 200 OK\r\nContent-Type: {content_type}\r\nCache-Control: no-store\r\nX-Content-Type-Options: nosniff\r\nX-Accel-Buffering: no\r\nTransfer-Encoding: chunked\r\nConnection: close\r\n");
        if let Some(origin) = origin {
            write!(headers, "Access-Control-Allow-Origin: {origin}\r\nAccess-Control-Allow-Credentials: true\r\nVary: Origin\r\n").map_err(|error| error.to_string())?;
        }
        for (name, value) in &head.1 {
            if name == "set-cookie" || name == "last-event-id" {
                write!(headers, "{name}: {value}\r\n").map_err(|error| error.to_string())?;
            }
        }
        if headers.len() > MAX_HEADERS {
            return Err("stream response headers exceed their bound".to_owned());
        }
        headers.push_str("\r\n");
        downstream
            .write_all(headers.as_bytes())
            .and_then(|()| downstream.flush())
            .map_err(|error| error.to_string())?;
        stream_body(&mut upstream, downstream, head.2, head.3, started)
    }

    fn request_with_freshness(
        &self,
        endpoint: &Endpoint,
        authorization: &str,
        request: &OutboundRequest<'_>,
        headers: OutboundHeaders<'_>,
    ) -> Result<UpstreamResponse, String> {
        let total_started = Instant::now();
        check_outbound_boundary(authorization, request, headers)?;
        let connector_started = Instant::now();
        let connector = self.connector()?;
        pay_timing("gateway.http.connector", connector_started);
        let pool_key = format!("{}:{}", endpoint.host, endpoint.port);
        let pool_started = Instant::now();
        let pooled = self.take_idle(&pool_key)?;
        pay_timing("gateway.http.pool", pool_started);
        if let Some(mut stream) = pooled {
            let exchange_started = Instant::now();
            let result = exchange(&mut stream, endpoint, authorization, request, headers);
            pay_timing("gateway.http.exchange", exchange_started);
            if result
                .as_ref()
                .is_ok_and(|response| !response.connection_close)
            {
                self.retain_idle(&pool_key, stream)?;
            }
            pay_timing("gateway.http.total", total_started);
            return result;
        }
        let resolve_started = Instant::now();
        let addresses = (endpoint.host.as_str(), endpoint.port)
            .to_socket_addrs()
            .map_err(|error| error.to_string())?;
        pay_timing("gateway.http.resolve", resolve_started);
        let mut last_error = None;
        for address in addresses.take(8) {
            let connect_started = Instant::now();
            match TcpStream::connect_timeout(&address, CONNECT_TIMEOUT) {
                Ok(tcp) => {
                    pay_timing("gateway.http.tcp_connect", connect_started);
                    tcp.set_nodelay(true).map_err(|error| error.to_string())?;
                    tcp.set_read_timeout(Some(IO_TIMEOUT))
                        .map_err(|error| error.to_string())?;
                    tcp.set_write_timeout(Some(IO_TIMEOUT))
                        .map_err(|error| error.to_string())?;
                    let tls_started = Instant::now();
                    let mut stream = connector
                        .connect(&endpoint.host, tcp)
                        .map_err(|error| error.to_string())?;
                    pay_timing("gateway.http.tls_handshake", tls_started);
                    let exchange_started = Instant::now();
                    let result = exchange(&mut stream, endpoint, authorization, request, headers);
                    pay_timing("gateway.http.exchange", exchange_started);
                    if result
                        .as_ref()
                        .is_ok_and(|response| !response.connection_close)
                    {
                        self.retain_idle(&pool_key, stream)?;
                    }
                    pay_timing("gateway.http.total", total_started);
                    return result;
                }
                Err(error) => {
                    pay_timing("gateway.http.tcp_connect", connect_started);
                    last_error = Some(error);
                }
            }
        }
        Err(last_error.map_or_else(
            || "component endpoint did not resolve".to_owned(),
            |error| error.to_string(),
        ))
    }
}

fn check_outbound_boundary(
    authorization: &str,
    request: &OutboundRequest<'_>,
    headers: OutboundHeaders<'_>,
) -> Result<(), String> {
    let OutboundHeaders {
        trace,
        publication_key,
        ..
    } = headers;
    if let Some((customer_authorization, _)) = headers.migration_source {
        let bearer = authorization.strip_prefix("Bearer ");
        if request.method != "POST"
            || request.path != "/internal/v2/source-settlements"
            || request.content_type != "application/json"
            || bearer.is_none_or(|value| {
                value.is_empty() || !value.bytes().all(|b| b.is_ascii_graphic())
            })
            || authorization.len() > 4096
            || customer_authorization.is_empty()
            || customer_authorization.len() > 4096
            || customer_authorization
                .bytes()
                .any(|b| !b.is_ascii_graphic() && b != b' ')
            || !headers.forwarded.is_empty()
            || headers.query.is_some()
            || headers.publication_key.is_some()
            || headers.freshness.is_some()
        {
            return Err("migration source settlement outside boundary".to_owned());
        }
    }
    for (name, value) in headers.forwarded {
        if !request_header_is_forwardable(name)
            || value.len() > 4096
            || value.bytes().any(|b| !b.is_ascii_graphic() && b != b' ')
        {
            return Err("forwarded header outside boundary".to_owned());
        }
    }
    if request
        .idempotency
        .is_some_and(|s| s.len() > 256 || !s.bytes().all(|b| b.is_ascii_graphic()))
        || !request
            .content_type
            .bytes()
            .all(|b| b.is_ascii_graphic() || b == b' ')
    {
        return Err("outbound metadata outside boundary".to_owned());
    }
    let path = request.path;
    let body = request.body;
    if !path.starts_with('/')
        || path.len() > MAX_QUERY
        || !path
            .bytes()
            .all(|byte| byte.is_ascii_graphic() && !b"?#\\".contains(&byte))
        || !request.method.bytes().all(|byte| byte.is_ascii_uppercase())
        || request.method.is_empty()
        || headers
            .query
            .is_some_and(|query| !query_is_canonical(query))
        || body.len() > MAX_RESPONSE
    {
        return Err("outbound request exceeds its boundary".to_owned());
    }
    if authorization.len() > 4096
        || authorization
            .bytes()
            .any(|byte| matches!(byte, b'\r' | b'\n' | 0))
    {
        return Err("outbound authorization exceeds its boundary".to_owned());
    }
    if trace.is_some_and(|value| {
        value.is_empty()
            || value.len() > 64
            || value.bytes().any(|byte| matches!(byte, b'\r' | b'\n' | 0))
    }) {
        return Err("outbound trace exceeds its boundary".to_owned());
    }
    if publication_key.is_some_and(|value| {
        value.is_empty() || value.len() > 4096 || !value.bytes().all(|byte| byte.is_ascii_graphic())
    }) {
        return Err("outbound publication key exceeds its boundary".to_owned());
    }
    Ok(())
}

const MAX_QUERY: usize = 2048;

/// True for an empty query or `name=value` pairs joined by `&` whose bytes are
/// unreserved characters or complete `%XX` escapes.
#[must_use]
pub fn query_is_canonical(query: &str) -> bool {
    let bytes = query.as_bytes();
    if bytes.len() > MAX_QUERY {
        return false;
    }
    let mut index = 0;
    while index < bytes.len() {
        match bytes[index] {
            b'%' => {
                if !bytes
                    .get(index + 1..index + 3)
                    .is_some_and(|pair| pair.iter().all(u8::is_ascii_hexdigit))
                {
                    return false;
                }
                index += 3;
            }
            byte if byte.is_ascii_alphanumeric() || b"-._~=&".contains(&byte) => index += 1,
            _ => return false,
        }
    }
    true
}

/// One plaintext `GET` to a component on a loopback address, the only place
/// the gateway speaks plain HTTP (a co-located history indexer, for example).
/// The connection is closed after the answer.
///
/// # Errors
/// Refuses non-loopback addresses, a path or query outside its boundary and
/// connection or HTTP failures.
pub fn loopback_get(
    address: std::net::SocketAddr,
    host: &str,
    path: &str,
    query: &str,
) -> Result<UpstreamResponse, String> {
    if !address.ip().is_loopback() {
        return Err("plain HTTP is only admitted to loopback components".to_owned());
    }
    if !path.starts_with('/')
        || path.len() > MAX_QUERY
        || !path
            .bytes()
            .all(|byte| byte.is_ascii_graphic() && !b"?#\\".contains(&byte))
        || !query_is_canonical(query)
        || host.is_empty()
        || !host.bytes().all(|byte| byte.is_ascii_graphic())
    {
        return Err("outbound request exceeds its boundary".to_owned());
    }
    let mut tcp =
        TcpStream::connect_timeout(&address, CONNECT_TIMEOUT).map_err(|error| error.to_string())?;
    tcp.set_read_timeout(Some(IO_TIMEOUT))
        .map_err(|error| error.to_string())?;
    tcp.set_write_timeout(Some(IO_TIMEOUT))
        .map_err(|error| error.to_string())?;
    let query = if query.is_empty() {
        String::new()
    } else {
        format!("?{query}")
    };
    write!(
        tcp,
        "GET {path}{query} HTTP/1.1\r\nHost: {host}\r\nAccept: application/json\r\nConnection: close\r\n\r\n"
    )
    .map_err(|error| error.to_string())?;
    tcp.flush().map_err(|error| error.to_string())?;
    read_response(&mut tcp)
}

pub struct IncomingRequest {
    pub method: String,
    pub path: String,
    pub headers: BTreeMap<String, String>,
    pub body: Vec<u8>,
}

impl Drop for IncomingRequest {
    fn drop(&mut self) {
        for value in self.headers.values_mut() {
            value.zeroize();
        }
        self.body.zeroize();
    }
}

pub struct OutgoingResponse {
    pub content_type: String,
    pub headers: Vec<(String, String)>,
    pub status: u16,
    pub body: Vec<u8>,
    pub retry_after: Option<u64>,
}

pub struct UpstreamResponse {
    pub headers: Vec<(String, String)>,
    pub status: u16,
    pub content_type: String,
    pub body: Vec<u8>,
    connection_close: bool,
}

fn send_request(
    stream: &mut TlsStream<TcpStream>,
    endpoint: &Endpoint,
    authorization: &str,
    request: &OutboundRequest<'_>,
    headers: OutboundHeaders<'_>,
) -> Result<(), String> {
    let OutboundHeaders {
        trace,
        freshness,
        publication_key,
        query,
        forwarded,
        migration_source,
    } = headers;
    let idempotency = request
        .idempotency
        .map_or_else(String::new, |key| format!("Idempotency-Key: {key}\r\n"));
    let trace = trace.map_or_else(String::new, |value| format!("X-Trace-Id: {value}\r\n"));
    let freshness = freshness.map_or_else(String::new, |(minimum, root)| {
        let mut headers = format!("LayerX-Minimum-Sequence: {minimum}\r\n");
        if let Some(root) = root {
            headers.push_str("LayerX-Expected-State-Root: ");
            for byte in root {
                let _ = write!(headers, "{byte:02x}");
            }
            headers.push_str("\r\n");
        }
        headers
    });
    let publication = publication_key.map_or_else(zeroize::Zeroizing::default, |key| {
        zeroize::Zeroizing::new(format!("LayerX-Key: {key}\r\n"))
    });
    let migration = migration_source.map_or_else(zeroize::Zeroizing::default, |(customer, did)| {
        let mut value = zeroize::Zeroizing::new(format!(
            "X-LayerX-Customer-Authorization: {customer}\r\nX-LayerX-Expected-Did: "
        ));
        for byte in did {
            let _ = write!(value, "{byte:02x}");
        }
        value.push_str("\r\n");
        value
    });
    let authorization = if authorization.is_empty() {
        zeroize::Zeroizing::new(String::new())
    } else {
        zeroize::Zeroizing::new(format!("Authorization: {authorization}\r\n"))
    };
    let forwarded_headers = zeroize::Zeroizing::new(
        forwarded
            .iter()
            .map(|(name, value)| format!("{name}: {value}\r\n"))
            .collect::<String>(),
    );
    let accept = if forwarded.iter().any(|(name, _)| *name == "accept") {
        ""
    } else {
        "Accept: application/json\r\n"
    };
    let forwarded = forwarded_headers.as_str();
    let migration_headers = migration.as_str();
    let mut outbound = zeroize::Zeroizing::new(Vec::new());
    write!(
        outbound,
        "{} {}{}{} HTTP/1.1\r\nHost: {}\r\n{}{accept}Content-Type: {}\r\n{idempotency}{trace}{freshness}{forwarded}{migration_headers}{}Content-Length: {}\r\nConnection: keep-alive\r\n\r\n",
        request.method,
        endpoint.base_path,
        request.path,
        query.map_or_else(String::new, |query| format!("?{query}")),
        endpoint.authority(),
        authorization.as_str(),
        request.content_type,
        publication.as_str(),
        request.body.len()
    )
    .map_err(|error| error.to_string())?;
    outbound.extend_from_slice(request.body);
    stream
        .write_all(&outbound)
        .map_err(|error| error.to_string())?;
    stream.flush().map_err(|error| error.to_string())
}

fn exchange(
    stream: &mut TlsStream<TcpStream>,
    endpoint: &Endpoint,
    authorization: &str,
    request: &OutboundRequest<'_>,
    headers: OutboundHeaders<'_>,
) -> Result<UpstreamResponse, String> {
    send_request(stream, endpoint, authorization, request, headers)?;
    read_response(stream)
}

/// # Errors
/// Refuses malformed, truncated or oversized HTTP requests and read failures.
pub fn read_request(stream: &mut impl Read, maximum: usize) -> Result<IncomingRequest, String> {
    let (start, headers, body) = read_message(stream, maximum, false)?;
    let headers: BTreeMap<_, _> = headers.into_iter().collect();
    let mut parts = start.split_whitespace();
    let method = parts
        .next()
        .ok_or_else(|| "request method is missing".to_owned())?;
    let path = parts
        .next()
        .ok_or_else(|| "request target is missing".to_owned())?;
    if parts.next() != Some("HTTP/1.1")
        || parts.next().is_some()
        || (if crate::explorer_target::owns_target(path) {
            crate::explorer_target::split_target(path).is_err()
        } else {
            if ui_owns_target(path) {
                ui_split_target(path).is_err()
            } else {
                split_target(path).is_err()
            }
        })
        || !headers.contains_key("host")
    {
        return Err("request line is invalid".to_owned());
    }
    Ok(IncomingRequest {
        method: method.to_owned(),
        path: path.to_owned(),
        headers,
        body,
    })
}

pub fn request_header_is_forwardable(name: &str) -> bool {
    matches!(
        name,
        "x-agent-key"
            | "x-agent-nonce"
            | "x-agent-expires"
            | "x-agent-signature"
            | "x-agent-attestor-authorization-id"
            | "x-agent-attestor-authorization"
            | "x-trace-id"
            | "cookie"
            | "x-csrf-token"
            | "origin"
            | "payment-signature"
            | "payment-required"
            | "payment-response"
            | "x-payment"
            | "x-payment-response"
            | "last-event-id"
            | "accept"
            | "x-layerx-csrf"
            | "x-layerx-trace"
            | "layerx-payer-did"
            | "x-layerx-wallet-binding"
    )
}

fn response_header_is_forwardable(name: &str) -> bool {
    matches!(
        name,
        "set-cookie"
            | "payment-required"
            | "payment-response"
            | "x-payment-response"
            | "retry-after"
            | "www-authenticate"
            | "content-disposition"
            | "last-event-id"
            | "etag"
            | "x-content-sha256"
            | "x-layerx-batch"
    )
}

fn browser_response_header_is_forwardable(name: &str) -> bool {
    matches!(
        name,
        "cache-control"
            | "content-encoding"
            | "vary"
            | "location"
            | "content-security-policy"
            | "last-modified"
            | "expires"
            | "x-csrf-token"
    )
}

pub fn split_target(target: &str) -> Result<(&str, Option<&str>), String> {
    let (path, query) = target
        .split_once('?')
        .map_or((target, None), |(path, query)| (path, Some(query)));
    if !path.starts_with('/')
        || path.len() > MAX_QUERY
        || !path
            .bytes()
            .all(|byte| byte.is_ascii_graphic() && !b"?#\\".contains(&byte))
        || query.is_some_and(|query| !query_is_canonical(query))
    {
        return Err("request target exceeds its boundary".to_owned());
    }
    Ok((path, query))
}

fn read_response(stream: &mut impl Read) -> Result<UpstreamResponse, String> {
    response_parts(read_message(stream, MAX_RESPONSE, true)?)
}

fn response_parts((start, fields, body): HttpMessage) -> Result<UpstreamResponse, String> {
    let mut parts = start.split_whitespace();
    if parts.next() != Some("HTTP/1.1") {
        return Err("component response must use HTTP/1.1".to_owned());
    }
    let status = parts
        .next()
        .ok_or("component response status is missing")?
        .parse::<u16>()
        .map_err(|_| "component response status is invalid")?;
    if !(100..=599).contains(&status) {
        return Err("component response status is invalid".to_owned());
    }
    let field = |name: &str| {
        fields
            .iter()
            .find(|(key, _)| key == name)
            .map(|(_, value)| value.as_str())
    };
    let content_type = field("content-type").unwrap_or_default().to_owned();
    let connection_close = field("connection").is_some_and(|value| {
        value
            .split(',')
            .any(|token| token.trim().eq_ignore_ascii_case("close"))
    }) || (field("content-length").is_none()
        && field("transfer-encoding").is_none());
    let headers = fields
        .into_iter()
        .filter(|(name, _)| response_header_is_forwardable(name))
        .collect();
    Ok(UpstreamResponse {
        status,
        content_type,
        headers,
        body,
        connection_close,
    })
}

type HttpMessage = (String, Vec<(String, String)>, Vec<u8>);

fn bounded_line(
    stream: &mut impl Read,
    maximum: usize,
    started: Instant,
) -> Result<Vec<u8>, String> {
    let mut bytes = Vec::new();
    loop {
        if started.elapsed() > IO_TIMEOUT || bytes.len() >= maximum {
            return Err("HTTP line exceeds its deadline or bound".to_owned());
        }
        let mut byte = [0_u8; 1];
        stream
            .read_exact(&mut byte)
            .map_err(|error| error.to_string())?;
        bytes.push(byte[0]);
        if bytes.ends_with(b"\r\n") {
            bytes.truncate(bytes.len() - 2);
            return Ok(bytes);
        }
    }
}

fn bounded_body(stream: &mut impl Read, body: &mut [u8], started: Instant) -> Result<(), String> {
    let mut offset = 0;
    while offset < body.len() {
        if started.elapsed() > IO_TIMEOUT {
            return Err("HTTP message deadline exceeded".to_owned());
        }
        let end = body.len().min(offset + 2048);
        let count = stream
            .read(&mut body[offset..end])
            .map_err(|error| error.to_string())?;
        if count == 0 {
            return Err("HTTP body is truncated".to_owned());
        }
        offset += count;
    }
    Ok(())
}

type MessageHead = (String, Vec<(String, String)>, Option<usize>, bool, usize);

fn read_head(
    stream: &mut impl Read,
    maximum: usize,
    response: bool,
    started: Instant,
) -> Result<MessageHead, String> {
    let start = String::from_utf8(bounded_line(stream, MAX_HEADERS.min(maximum), started)?)
        .map_err(|_| "HTTP start line is not UTF-8")?;
    let mut header_size = start.len() + 2;
    let mut headers = Vec::new();
    let mut names = std::collections::BTreeSet::new();
    let mut content_length = None;
    let mut chunked = false;
    loop {
        let line = bounded_line(
            stream,
            MAX_HEADERS.min(maximum).saturating_sub(header_size),
            started,
        )?;
        header_size += line.len() + 2;
        if line.is_empty() {
            break;
        }
        let line = std::str::from_utf8(&line).map_err(|_| "HTTP header is not UTF-8")?;
        let (name, value) = line.split_once(':').ok_or("HTTP header is malformed")?;
        if name.is_empty()
            || !name
                .bytes()
                .all(|byte| byte.is_ascii_alphanumeric() || b"!#$%&'*+-.^_`|~".contains(&byte))
        {
            return Err("HTTP header name is invalid".to_owned());
        }
        let name = name.to_ascii_lowercase();
        if !names.insert(name.clone()) && !(response && name == "set-cookie") {
            return Err("duplicate HTTP header".to_owned());
        }
        let value = value.trim().to_owned();
        if !value
            .bytes()
            .all(|byte| byte.is_ascii_graphic() || byte == b' ' || byte == b'\t')
        {
            return Err("HTTP header value is invalid".to_owned());
        }
        if name == "transfer-encoding" {
            if !response || !value.eq_ignore_ascii_case("chunked") {
                return Err("transfer-encoded message is not accepted".to_owned());
            }
            chunked = true;
        }
        if name == "content-length" {
            if value.is_empty() || !value.bytes().all(|byte| byte.is_ascii_digit()) {
                return Err("content length is invalid".to_owned());
            }
            content_length = Some(
                value
                    .parse::<usize>()
                    .map_err(|_| "content length is invalid")?,
            );
        }
        headers.push((name, value));
    }
    if chunked && content_length.is_some() {
        return Err("conflicting HTTP framing".to_owned());
    }
    Ok((start, headers, content_length, chunked, header_size))
}

fn read_message(
    stream: &mut impl Read,
    maximum: usize,
    response: bool,
) -> Result<HttpMessage, String> {
    let started = Instant::now();
    let head = read_head(stream, maximum, response, started)?;
    read_message_body(stream, maximum, response, started, head)
}

fn read_message_body(
    stream: &mut impl Read,
    maximum: usize,
    response: bool,
    started: Instant,
    head: MessageHead,
) -> Result<HttpMessage, String> {
    let (start, headers, content_length, chunked, header_size) = head;
    let limit = maximum.saturating_sub(header_size);
    let mut body = Vec::new();
    if chunked {
        let mut overhead = 0_usize;
        loop {
            let line = bounded_line(stream, 1024, started)?;
            overhead = overhead.saturating_add(line.len() + 4);
            if overhead > MAX_HEADERS {
                return Err("HTTP chunk overhead exceeds its bound".to_owned());
            }
            let size = std::str::from_utf8(&line).map_err(|_| "invalid chunk size")?;
            if size.is_empty() || !size.bytes().all(|byte| byte.is_ascii_hexdigit()) {
                return Err("invalid chunk size".to_owned());
            }
            let size = usize::from_str_radix(size, 16).map_err(|_| "invalid chunk size")?;
            if size == 0 {
                if !bounded_line(stream, MAX_HEADERS, started)?.is_empty() {
                    return Err("HTTP trailers are not accepted".to_owned());
                }
                break;
            }
            if size > limit.saturating_sub(body.len()) {
                return Err("HTTP body exceeds its bound".to_owned());
            }
            let offset = body.len();
            body.resize(offset + size, 0);
            bounded_body(stream, &mut body[offset..], started)?;
            if !bounded_line(stream, 2, started)?.is_empty() {
                return Err("invalid chunk terminator".to_owned());
            }
        }
    } else if let Some(length) = content_length {
        if length > limit {
            return Err("HTTP body exceeds its bound".to_owned());
        }
        body.resize(length, 0);
        bounded_body(stream, &mut body, started)?;
    } else if response && !matches!(start.split_whitespace().nth(1), Some("101" | "204" | "304")) {
        let mut chunk = [0_u8; 2048];
        loop {
            if started.elapsed() > IO_TIMEOUT {
                return Err("HTTP message deadline exceeded".to_owned());
            }
            let count = stream.read(&mut chunk).map_err(|error| error.to_string())?;
            if count == 0 {
                break;
            }
            if count > limit.saturating_sub(body.len()) {
                return Err("HTTP body exceeds its bound".to_owned());
            }
            body.extend_from_slice(&chunk[..count]);
        }
    }
    if started.elapsed() > IO_TIMEOUT {
        return Err("HTTP message deadline exceeded".to_owned());
    }
    Ok((start, headers, body))
}

fn stream_body(
    upstream: &mut impl Read,
    downstream: &mut impl Write,
    content_length: Option<usize>,
    chunked: bool,
    started: Instant,
) -> Result<(), String> {
    if content_length.is_some_and(|length| length > MAX_RESPONSE) {
        return Err("stream body exceeds its bound".to_owned());
    }
    let mut total = 0_usize;
    let mut remaining = content_length;
    let mut buffer = [0_u8; 4096];
    loop {
        if started.elapsed() >= Duration::from_secs(300) {
            return Err("stream lifetime exceeded".to_owned());
        }
        let mut chunk_remaining = if chunked {
            let line = bounded_line(upstream, 1024, Instant::now())?;
            let size = std::str::from_utf8(&line).map_err(|_| "invalid stream chunk size")?;
            if size.is_empty() || !size.bytes().all(|byte| byte.is_ascii_hexdigit()) {
                return Err("invalid stream chunk size".to_owned());
            }
            let size = usize::from_str_radix(size, 16).map_err(|_| "invalid stream chunk size")?;
            if size == 0 {
                if !bounded_line(upstream, MAX_HEADERS, Instant::now())?.is_empty() {
                    return Err("stream trailers are not accepted".to_owned());
                }
                break;
            }
            Some(size)
        } else {
            remaining
        };
        if chunk_remaining == Some(0) {
            break;
        }
        if chunk_remaining.is_some_and(|size| size > MAX_RESPONSE.saturating_sub(total)) {
            return Err("stream body exceeds its bound".to_owned());
        }
        loop {
            if started.elapsed() >= Duration::from_secs(300) {
                return Err("stream lifetime exceeded".to_owned());
            }
            let maximum = chunk_remaining.map_or(buffer.len(), |left| left.min(buffer.len()));
            let count = upstream
                .read(&mut buffer[..maximum])
                .map_err(|error| error.to_string())?;
            if count == 0 {
                if chunk_remaining.is_some() {
                    return Err("stream body is truncated".to_owned());
                }
                downstream
                    .write_all(b"0\r\n\r\n")
                    .and_then(|()| downstream.flush())
                    .map_err(|error| error.to_string())?;
                return Ok(());
            }
            total = total.checked_add(count).ok_or("stream body bound")?;
            if total > MAX_RESPONSE {
                return Err("stream body exceeds its bound".to_owned());
            }
            write!(downstream, "{count:x}\r\n").map_err(|error| error.to_string())?;
            downstream
                .write_all(&buffer[..count])
                .and_then(|()| downstream.write_all(b"\r\n"))
                .and_then(|()| downstream.flush())
                .map_err(|error| error.to_string())?;
            if let Some(left) = chunk_remaining.as_mut() {
                *left -= count;
                if *left == 0 {
                    break;
                }
            }
        }
        if chunked {
            if !bounded_line(upstream, 2, Instant::now())?.is_empty() {
                return Err("invalid stream chunk terminator".to_owned());
            }
        } else {
            remaining = Some(0);
        }
    }
    downstream
        .write_all(b"0\r\n\r\n")
        .and_then(|()| downstream.flush())
        .map_err(|error| error.to_string())
}

/// # Errors
/// Returns an error when writing or flushing the response fails.
pub fn write_response(stream: &mut impl Write, response: &OutgoingResponse) -> Result<(), String> {
    write_response_connection(stream, response, false)
}

/// Writes a response with the bounded connection lifecycle selected by the server.
///
/// # Errors
/// Returns an error when writing or flushing the response fails.
pub fn write_response_connection(
    stream: &mut impl Write,
    response: &OutgoingResponse,
    keep_alive: bool,
) -> Result<(), String> {
    write_response_connection_with_origin(stream, response, keep_alive, None)
}

pub fn write_response_connection_with_origin(
    stream: &mut impl Write,
    response: &OutgoingResponse,
    keep_alive: bool,
    origin: Option<&str>,
) -> Result<(), String> {
    let cors = match origin {
        Some(origin) if origin.bytes().all(|b| b.is_ascii_graphic()) => format!("Access-Control-Allow-Origin: {origin}\r\nVary: Origin\r\nAccess-Control-Allow-Credentials: true\r\nAccess-Control-Allow-Methods: GET, POST, PUT, DELETE, OPTIONS\r\nAccess-Control-Allow-Headers: Content-Type, Authorization, Idempotency-Key, X-Agent-Key, X-Agent-Nonce, X-Agent-Expires, X-Agent-Signature, X-Agent-Attestor-Authorization-Id, X-Agent-Attestor-Authorization, X-Trace-Id, X-CSRF-Token, X-LayerX-CSRF, X-LayerX-Trace, LayerX-Payer-DID, X-LayerX-Wallet-Binding, Payment-Signature, X-Payment, Last-Event-ID\r\nAccess-Control-Expose-Headers: Payment-Required, Payment-Response, X-Payment-Response, Retry-After, Content-Disposition, ETag, X-Content-SHA256, X-LayerX-Batch\r\n"),
        Some(_) => return Err("invalid CORS origin".to_owned()),
        None => String::new(),
    };
    if !response
        .content_type
        .bytes()
        .all(|byte| byte.is_ascii_graphic() || byte == b' ')
    {
        return Err("invalid response content type".to_owned());
    }
    let content_type = if response.content_type.is_empty() {
        String::new()
    } else {
        format!("Content-Type: {}\r\n", response.content_type)
    };
    let mut forwarded = String::new();
    for (name, value) in &response.headers {
        if !(response_header_is_forwardable(name) || browser_response_header_is_forwardable(name))
            || !value
                .bytes()
                .all(|byte| byte.is_ascii_graphic() || byte == b' ' || byte == b'\t')
        {
            return Err("invalid response header".to_owned());
        }
        write!(forwarded, "{name}: {value}\r\n").map_err(|error| error.to_string())?;
        if forwarded.len() > MAX_HEADERS {
            return Err("response headers exceed their bound".to_owned());
        }
    }
    let reason = match response.status {
        200 => "OK",
        201 => "Created",
        202 => "Accepted",
        204 => "No Content",
        302 => "Found",
        304 => "Not Modified",
        400 => "Bad Request",
        401 => "Unauthorized",
        402 => "Payment Required",
        403 => "Forbidden",
        404 => "Not Found",
        409 => "Conflict",
        415 => "Unsupported Media Type",
        429 => "Too Many Requests",
        502 => "Bad Gateway",
        503 => "Service Unavailable",
        _ => "Error",
    };
    let retry = response
        .retry_after
        .map_or_else(String::new, |value| format!("Retry-After: {value}\r\n"));
    let connection = if keep_alive { "keep-alive" } else { "close" };
    write!(
        stream,
        "HTTP/1.1 {} {reason}\r\n{content_type}Cache-Control: no-store\r\nX-Content-Type-Options: nosniff\r\n{cors}{retry}{forwarded}Content-Length: {}\r\nConnection: {connection}\r\n\r\n",
        response.status,
        response.body.len()
    )
    .map_err(|error| error.to_string())?;
    stream
        .write_all(&response.body)
        .map_err(|error| error.to_string())?;
    stream.flush().map_err(|error| error.to_string())
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::io::Cursor;

    #[test]
    fn ingress_preserves_query_and_unconsumed_websocket_bytes() {
        let wire = b"GET /rpc/ws?cursor=1%2F2 HTTP/1.1\r\nHost: gateway.example\r\n\r\n\x81\x00";
        let mut reader = Cursor::new(wire);
        let request = read_request(&mut reader, 4096).unwrap_or_else(|error| panic!("{error}"));
        assert_eq!(request.path, "/rpc/ws?cursor=1%2F2");
        assert_eq!(reader.position(), (wire.len() - 2) as u64);
        for target in ["/v1?x=%", "/v1?x=%GG", "/v1?x=#fragment", "/v1?x=\r\n"] {
            assert!(split_target(target).is_err());
        }
    }

    #[test]
    fn upstream_chunked_body_and_repeated_cookies_are_preserved() {
        let mut reader = Cursor::new(b"HTTP/1.1 200 OK\r\nContent-Type: text/plain\r\nSet-Cookie: a=1; Secure\r\nSet-Cookie: b=2; Secure\r\nTransfer-Encoding: chunked\r\n\r\n2\r\nok\r\n0\r\n\r\n");
        let response = read_response(&mut reader).unwrap_or_else(|error| panic!("{error}"));
        assert_eq!(response.body, b"ok");
        assert_eq!(response.content_type, "text/plain");
        assert_eq!(response.headers.len(), 2);
        assert_eq!(response.headers[0].1, "a=1; Secure");
        assert_eq!(response.headers[1].1, "b=2; Secure");
    }

    #[test]
    fn conflicting_framing_and_ingress_duplicates_remain_refused() {
        for wire in [
            &b"HTTP/1.1 200 OK\r\nContent-Length: 2\r\nTransfer-Encoding: chunked\r\n\r\n0\r\n\r\n"
                [..],
            &b"HTTP/1.1 200 OK\r\nContent-Length: 2\r\nContent-Length: 2\r\n\r\nok"[..],
            &b"HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\n\r\nFFFFFFF\r\n"[..],
        ] {
            assert!(read_response(&mut Cursor::new(wire)).is_err());
        }
        for wire in [
            &b"GET / HTTP/1.1\r\nHost: a\r\nHost: b\r\n\r\n"[..],
            &b"POST / HTTP/1.1\r\nHost: a\r\nTransfer-Encoding: chunked\r\n\r\n0\r\n\r\n"[..],
        ] {
            assert!(read_request(&mut Cursor::new(wire), 4096).is_err());
        }
    }

    #[test]
    fn response_serialization_preserves_content_type_and_cookie_headers() {
        let response = OutgoingResponse {
            status: 200,
            content_type: "text/plain".to_owned(),
            headers: vec![
                ("set-cookie".to_owned(), "a=1; Secure".to_owned()),
                ("set-cookie".to_owned(), "b=2; Secure".to_owned()),
            ],
            body: b"ok".to_vec(),
            retry_after: None,
        };
        let mut bytes = Vec::new();
        write_response(&mut bytes, &response).unwrap_or_else(|error| panic!("{error}"));
        let parsed =
            read_response(&mut Cursor::new(bytes)).unwrap_or_else(|error| panic!("{error}"));
        assert_eq!(parsed.headers, response.headers);
        assert_eq!(parsed.content_type, response.content_type);
        assert_eq!(parsed.body, response.body);
    }
}

pub fn connect_public_tls(endpoint: &Endpoint) -> Result<TlsStream<TcpStream>, String> {
    if !endpoint.base_path.is_empty()
        || endpoint.host.is_empty()
        || !endpoint
            .host
            .bytes()
            .all(|b| b.is_ascii_alphanumeric() || b == b'.' || b == b'-')
        || endpoint.port != 443
    {
        return Err("public HTTPS endpoint refused".into());
    }
    let connector = TlsConnector::builder()
        .min_protocol_version(Some(native_tls::Protocol::Tlsv12))
        .build()
        .map_err(|_| "public TLS configuration unavailable")?;
    let started = Instant::now();
    let addresses = (endpoint.host.as_str(), endpoint.port)
        .to_socket_addrs()
        .map_err(|_| "public upstream resolution failed")?;
    for address in addresses.take(8) {
        if started.elapsed() >= IO_TIMEOUT {
            break;
        }
        let disallowed = match address.ip() {
            IpAddr::V4(ip) => {
                ip.is_private()
                    || ip.is_loopback()
                    || ip.is_link_local()
                    || ip.is_unspecified()
                    || ip.is_multicast()
                    || ip.is_broadcast()
            }
            IpAddr::V6(ip) => {
                ip.is_loopback()
                    || ip.is_unspecified()
                    || ip.is_multicast()
                    || (ip.segments()[0] & 0xfe00 == 0xfc00)
                    || (ip.segments()[0] & 0xffc0 == 0xfe80)
                    || ip.to_ipv4_mapped().is_some()
            }
        };
        if disallowed {
            return Err("public upstream address refused".into());
        }
        if let Ok(tcp) = TcpStream::connect_timeout(&address, CONNECT_TIMEOUT) {
            tcp.set_nodelay(true)
                .map_err(|_| "public upstream socket unavailable")?;
            tcp.set_read_timeout(Some(IO_TIMEOUT))
                .map_err(|_| "public upstream deadline unavailable")?;
            tcp.set_write_timeout(Some(IO_TIMEOUT))
                .map_err(|_| "public upstream deadline unavailable")?;
            return connector
                .connect(&endpoint.host, tcp)
                .map_err(|_| "public upstream TLS refused".into());
        }
    }
    Err("public upstream unavailable".into())
}

pub fn explorer_request(
    endpoint: &Endpoint,
    request: &OutboundRequest<'_>,
    forwarded: &[(&str, &str)],
) -> Result<UpstreamResponse, String> {
    crate::explorer_target::split_target(request.path)?;
    if !matches!(request.method, "GET" | "POST" | "PUT" | "PATCH" | "DELETE")
        || request.body.len() > MAX_RESPONSE
        || request.content_type.len() > 256
        || !request
            .content_type
            .bytes()
            .all(|b| b.is_ascii_graphic() || b == b' ')
        || request.idempotency.is_some()
    {
        return Err("explorer request outside boundary".into());
    }
    let mut head = zeroize::Zeroizing::new(format!(
        "{} {} HTTP/1.1\r\nHost: {}\r\nContent-Length: {}\r\nConnection: close\r\n",
        request.method,
        request.path,
        endpoint.authority(),
        request.body.len()
    ));
    if !request.content_type.is_empty() {
        write!(head, "Content-Type: {}\r\n", request.content_type)
            .map_err(|_| "explorer header encoding failed")?;
    }
    let mut seen = std::collections::BTreeSet::new();
    for (name, value) in forwarded {
        if !(matches!(
            *name,
            "accept"
                | "accept-language"
                | "cookie"
                | "origin"
                | "referer"
                | "x-csrf-token"
                | "if-none-match"
                | "if-modified-since"
                | "x-forwarded-host"
                | "x-forwarded-proto"
        ) || (*name == "authorization"
            && request.method == "GET"
            && request.path.split('?').next() == Some("/api/account/v2/authenticate_via_dynamic")))
            || !seen.insert(*name)
            || value.len() > 4096
            || !value.bytes().all(|b| b.is_ascii_graphic() || b == b' ')
        {
            return Err("explorer forwarded header refused".into());
        }
        write!(head, "{name}: {value}\r\n").map_err(|_| "explorer header encoding failed")?;
    }
    if head.len() + 2 > MAX_HEADERS {
        return Err("explorer headers exceed bound".into());
    }
    head.push_str("\r\n");
    let mut upstream = connect_public_tls(endpoint)?;
    upstream
        .write_all(head.as_bytes())
        .and_then(|()| upstream.write_all(request.body))
        .and_then(|()| upstream.flush())
        .map_err(|_| "explorer upstream write failed")?;
    let started = Instant::now();
    let head = read_head(&mut upstream, MAX_RESPONSE, true, started)?;
    let status = head
        .0
        .split_whitespace()
        .nth(1)
        .and_then(|v| v.parse::<u16>().ok())
        .ok_or("explorer upstream status refused")?;
    if status < 200 {
        return Err("explorer HTTP upgrade or interim response refused".into());
    }
    let (start, fields, body) = if matches!(status, 204 | 304) {
        if head.3 || (status == 204 && head.2.is_some_and(|n| n != 0)) {
            return Err("explorer bodyless response framing refused".into());
        }
        (head.0, head.1, Vec::new())
    } else {
        read_message_body(&mut upstream, MAX_RESPONSE, true, started, head)?
    };
    let nominated: std::collections::BTreeSet<String> = fields
        .iter()
        .filter(|(name, _)| name == "connection")
        .flat_map(|(_, value)| {
            value
                .split(',')
                .map(|token| token.trim().to_ascii_lowercase())
        })
        .collect();
    let fields: Vec<(String, String)> = fields
        .into_iter()
        .filter(|(name, _)| !nominated.contains(name))
        .collect();
    let browser_headers: Vec<(String, String)> = fields
        .iter()
        .filter(|(name, _)| browser_response_header_is_forwardable(name))
        .cloned()
        .collect();
    let mut answer = response_parts((start, fields, body))?;
    answer.headers.extend(browser_headers);
    Ok(answer)
}

pub fn ui_owns_target(target: &str) -> bool {
    ["/wallet", "/explorer"].iter().any(|prefix| {
        target
            .strip_prefix(prefix)
            .is_some_and(|tail| tail.is_empty() || tail.starts_with('/') || tail.starts_with('?'))
    })
}

pub fn ui_split_target(target: &str) -> Result<(&str, Option<&str>), String> {
    if target.len() > 4096 {
        return Err("UI target exceeds bound".into());
    }
    let (path, query) = target
        .split_once('?')
        .map_or((target, None), |(p, q)| (p, Some(q)));
    if !path.starts_with('/')
        || path.len() > 2048
        || path.contains(['#', '\\'])
        || !path.bytes().all(|b| b.is_ascii_graphic())
        || path.contains("//")
        || path.split('/').any(|p| matches!(p, "." | ".."))
    {
        return Err("UI path refused".into());
    }
    let mut path_bytes = path.as_bytes().iter().copied();
    while let Some(byte) = path_bytes.next() {
        if byte == b'%' && (path_bytes.next() != Some(b'2') || path_bytes.next() != Some(b'0')) {
            return Err("UI path escape refused".into());
        }
    }
    if let Some(query) = query {
        if query.len() > 2048 {
            return Err("UI query exceeds bound".into());
        }
        let bytes = query.as_bytes();
        let mut i = 0;
        while i < bytes.len() {
            if bytes[i] == b'%' {
                let digits = bytes.get(i + 1..i + 3).ok_or("UI query escape refused")?;
                if !digits.iter().all(u8::is_ascii_hexdigit) {
                    return Err("UI query escape refused".into());
                }
                let value = u8::from_str_radix(
                    std::str::from_utf8(digits).map_err(|_| "UI query escape refused")?,
                    16,
                )
                .map_err(|_| "UI query escape refused")?;
                if value == 0 || value == b'\r' || value == b'\n' {
                    return Err("UI query control refused".into());
                }
                i += 3;
            } else if bytes[i].is_ascii_alphanumeric()
                || b"-._~!$&'()*+,;=:@/?[]".contains(&bytes[i])
            {
                i += 1;
            } else {
                return Err("UI query character refused".into());
            }
        }
    }
    Ok((path, query))
}

pub struct UiResponse {
    pub response: UpstreamResponse,
    representation_length: Option<usize>,
    stream: Option<(TlsStream<TcpStream>, Option<usize>, bool, Instant)>,
}

fn ui_response_header(name: &str) -> bool {
    matches!(
        name,
        "cache-control"
            | "vary"
            | "location"
            | "set-cookie"
            | "content-encoding"
            | "content-security-policy"
            | "content-security-policy-report-only"
            | "service-worker-allowed"
            | "etag"
            | "last-modified"
            | "expires"
            | "retry-after"
            | "content-disposition"
            | "x-accel-buffering"
            | "x-nextjs-cache"
            | "x-nextjs-prerender"
            | "x-nextjs-stale-time"
            | "cross-origin-opener-policy"
            | "cross-origin-resource-policy"
            | "permissions-policy"
            | "referrer-policy"
            | "strict-transport-security"
            | "x-frame-options"
    )
}

pub fn ui_request(
    endpoint: &Endpoint,
    request: &OutboundRequest<'_>,
    forwarded: &[(&str, &str)],
) -> Result<UiResponse, String> {
    let (path, _) = ui_split_target(request.path)?;
    if !ui_owns_target(path)
        || !matches!(request.method, "GET" | "HEAD" | "POST" | "DELETE")
        || request.body.len() > 32768
        || request.idempotency.is_some()
        || request.content_type.len() > 256
        || !request
            .content_type
            .bytes()
            .all(|b| b.is_ascii_graphic() || b == b' ')
    {
        return Err("UI outbound request refused".into());
    }
    let mut head = zeroize::Zeroizing::new(format!(
        "{} {} HTTP/1.1\r\nHost: {}\r\nContent-Length: {}\r\nConnection: close\r\n",
        request.method,
        request.path,
        endpoint.authority(),
        request.body.len()
    ));
    if !request.content_type.is_empty() {
        write!(head, "Content-Type: {}\r\n", request.content_type)
            .map_err(|_| "UI header encoding")?;
    }
    let mut seen = std::collections::BTreeSet::new();
    for (name, value) in forwarded {
        if !matches!(
            *name,
            "accept"
                | "accept-language"
                | "accept-encoding"
                | "cookie"
                | "origin"
                | "referer"
                | "rsc"
                | "next-router-state-tree"
                | "next-router-prefetch"
                | "next-url"
                | "x-csrf-token"
                | "if-none-match"
                | "if-modified-since"
                | "user-agent"
                | "x-forwarded-host"
                | "x-forwarded-proto"
        ) || !seen.insert(*name)
            || value.len() > 8192
            || !value.bytes().all(|b| b.is_ascii_graphic() || b == b' ')
        {
            return Err("UI forwarded header refused".into());
        }
        write!(head, "{name}: {value}\r\n").map_err(|_| "UI header encoding")?;
    }
    if head.len() + 2 > MAX_HEADERS {
        return Err("UI request headers exceed bound".into());
    }
    head.push_str("\r\n");
    let mut upstream = connect_public_tls(endpoint)?;
    upstream
        .write_all(head.as_bytes())
        .and_then(|()| upstream.write_all(request.body))
        .and_then(|()| upstream.flush())
        .map_err(|_| "UI upstream write failed")?;
    let started = Instant::now();
    let head = read_head(&mut upstream, MAX_RESPONSE, true, started)?;
    let mut start = head.0.split_whitespace();
    if start.next() != Some("HTTP/1.1") {
        return Err("UI upstream version refused".into());
    }
    let status = start
        .next()
        .and_then(|v| v.parse::<u16>().ok())
        .ok_or("UI upstream status refused")?;
    if !(200..=599).contains(&status) {
        return Err("UI interim or upgrade refused".into());
    }
    let content_type = head
        .1
        .iter()
        .find(|(n, _)| n == "content-type")
        .map_or("", |(_, v)| v.as_str())
        .to_owned();
    let nominated: std::collections::BTreeSet<String> = head
        .1
        .iter()
        .filter(|(n, _)| n == "connection")
        .flat_map(|(_, v)| v.split(',').map(|s| s.trim().to_ascii_lowercase()))
        .collect();
    if nominated.contains("content-type")
        || nominated.contains("content-length")
        || nominated.contains("transfer-encoding")
    {
        return Err("UI framing nominated by connection".into());
    }
    let fields = head
        .1
        .iter()
        .filter(|(n, _)| ui_response_header(n) && !nominated.contains(n))
        .cloned()
        .collect();
    let representation_length = head.2;
    let streaming = request.method == "POST"
        && path == "/wallet/api/chat"
        && status == 200
        && content_type.split(';').next() == Some("text/event-stream");
    let (body, stream) = if request.method == "HEAD" || matches!(status, 204 | 304) {
        if status == 204 && (head.3 || head.2.is_some_and(|n| n != 0)) {
            return Err("UI bodyless response framing refused".into());
        }
        (Vec::new(), None)
    } else if streaming {
        (Vec::new(), Some((upstream, head.2, head.3, started)))
    } else {
        (
            read_message_body(&mut upstream, MAX_RESPONSE, true, started, head)?.2,
            None,
        )
    };
    Ok(UiResponse {
        response: UpstreamResponse {
            status,
            content_type,
            headers: fields,
            body,
            connection_close: true,
        },
        representation_length,
        stream,
    })
}

pub fn write_ui_response(
    downstream: &mut impl Write,
    mut answer: UiResponse,
    head_only: bool,
) -> Result<(), String> {
    let response = &answer.response;
    let mut head = format!(
        "HTTP/1.1 {} UI\r\nConnection: close\r\nX-Content-Type-Options: nosniff\r\n",
        response.status
    );
    if !response.content_type.is_empty() {
        write!(head, "Content-Type: {}\r\n", response.content_type)
            .map_err(|_| "UI response encoding")?;
    }
    let mut has_cache = false;
    for (name, value) in &response.headers {
        if !ui_response_header(name)
            || !value
                .bytes()
                .all(|b| b.is_ascii_graphic() || b == b' ' || b == b'\t')
        {
            return Err("UI response header refused".into());
        }
        has_cache |= name == "cache-control";
        write!(head, "{name}: {value}\r\n").map_err(|_| "UI response encoding")?;
    }
    if !has_cache {
        head.push_str("Cache-Control: no-store\r\n");
    }
    if answer.stream.is_some() {
        head.push_str("Transfer-Encoding: chunked\r\n");
    } else if response.status != 204 {
        let length = if head_only || response.status == 304 {
            answer.representation_length
        } else {
            Some(response.body.len())
        };
        if let Some(length) = length {
            write!(head, "Content-Length: {length}\r\n").map_err(|_| "UI response encoding")?;
        }
    }
    head.push_str("\r\n");
    if head.len() > MAX_HEADERS {
        return Err("UI response headers exceed bound".into());
    }
    downstream
        .write_all(head.as_bytes())
        .and_then(|()| downstream.flush())
        .map_err(|_| "UI response write failed")?;
    if let Some((mut upstream, length, chunked, started)) = answer.stream.take() {
        return stream_body(&mut upstream, downstream, length, chunked, started);
    }
    if !head_only && !matches!(response.status, 204 | 304) {
        downstream
            .write_all(&response.body)
            .map_err(|_| "UI body write failed")?;
    }
    downstream
        .flush()
        .map_err(|_| "UI response flush failed".into())
}

pub fn write_ui_failure(
    downstream: &mut impl Write,
    response: OutgoingResponse,
    head_only: bool,
) -> Result<(), String> {
    let length = response.body.len();
    write_ui_response(
        downstream,
        UiResponse {
            response: UpstreamResponse {
                status: response.status,
                content_type: response.content_type,
                headers: response.headers,
                body: response.body,
                connection_close: true,
            },
            representation_length: Some(length),
            stream: None,
        },
        head_only,
    )
}
