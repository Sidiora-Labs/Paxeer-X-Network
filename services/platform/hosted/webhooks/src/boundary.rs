use native_tls::{Certificate, Identity, TlsConnector};
use std::collections::BTreeMap;
use std::io::{Read, Write};
use std::net::{IpAddr, SocketAddr, TcpStream, ToSocketAddrs};
use std::sync::atomic::{AtomicUsize, Ordering};
use std::sync::mpsc;
use std::time::{Duration, Instant, SystemTime, UNIX_EPOCH};

const CONNECT_TIMEOUT: Duration = Duration::from_secs(3);
const IO_TIMEOUT: Duration = Duration::from_secs(10);
const MAX_HEADERS: usize = 32 * 1024;
const MAX_BODY: usize = 512 * 1024;
static ADMISSION_RESOLVERS: AtomicUsize = AtomicUsize::new(0);

struct ResolverPermit;

impl Drop for ResolverPermit {
    fn drop(&mut self) {
        ADMISSION_RESOLVERS.fetch_sub(1, Ordering::Release);
    }
}

pub(crate) fn resolve_until(
    host: String,
    port: u16,
    deadline: Instant,
) -> Result<Vec<SocketAddr>, String> {
    remaining(deadline)?;
    ADMISSION_RESOLVERS
        .fetch_update(Ordering::AcqRel, Ordering::Acquire, |active| {
            (active < 16).then_some(active + 1)
        })
        .map_err(|_| "admission DNS capacity unavailable")?;
    let permit = ResolverPermit;
    let (sender, receiver) = mpsc::sync_channel(1);
    std::thread::Builder::new()
        .name("webhook-admission-dns".to_owned())
        .spawn(move || {
            let _permit = permit;
            let addresses = (host.as_str(), port)
                .to_socket_addrs()
                .map(|addresses| addresses.take(16).collect::<Vec<_>>())
                .map_err(|error| error.to_string());
            let _ = sender.send(addresses);
        })
        .map_err(|error| error.to_string())?;
    receiver
        .recv_timeout(remaining(deadline)?)
        .map_err(|_| "admission DNS deadline exceeded".to_owned())?
}

#[derive(Debug)]
pub(crate) struct DeadlineTcp {
    pub(crate) socket: TcpStream,
    pub(crate) deadline: Option<Instant>,
    pub(crate) io_timeout: Duration,
}

impl DeadlineTcp {
    fn budget(&self) -> std::io::Result<Duration> {
        self.deadline.map_or(Ok(self.io_timeout), |deadline| {
            deadline
                .checked_duration_since(Instant::now())
                .filter(|value| !value.is_zero())
                .map(|value| value.min(self.io_timeout))
                .ok_or_else(|| {
                    std::io::Error::new(std::io::ErrorKind::TimedOut, "admission deadline exceeded")
                })
        })
    }
}

impl Read for DeadlineTcp {
    fn read(&mut self, bytes: &mut [u8]) -> std::io::Result<usize> {
        self.socket.set_read_timeout(Some(self.budget()?))?;
        self.socket.read(bytes)
    }
}

impl Write for DeadlineTcp {
    fn write(&mut self, bytes: &[u8]) -> std::io::Result<usize> {
        self.socket.set_write_timeout(Some(self.budget()?))?;
        self.socket.write(bytes)
    }
    fn flush(&mut self) -> std::io::Result<()> {
        self.budget()?;
        self.socket.flush()
    }
}

pub(crate) fn remaining(deadline: Instant) -> Result<Duration, String> {
    deadline
        .checked_duration_since(Instant::now())
        .filter(|value| !value.is_zero())
        .ok_or_else(|| "admission deadline exceeded".to_owned())
}

#[derive(Clone, Debug)]
pub(crate) struct Endpoint {
    host: String,
    port: u16,
    base_path: String,
}

impl Endpoint {
    pub(crate) fn parse(value: &str) -> Result<Self, String> {
        if value.is_empty()
            || value.len() > 2_048
            || value.bytes().any(|byte| byte <= b' ' || byte == b'\x7f')
        {
            return Err("endpoint is empty, oversized or contains control bytes".to_owned());
        }
        let rest = value
            .strip_prefix("https://")
            .ok_or_else(|| "endpoint must use HTTPS".to_owned())?;
        let (authority, path) = rest
            .split_once('/')
            .map_or((rest, ""), |(authority, path)| (authority, path));
        if authority.is_empty()
            || authority.contains(['@', '?', '#', '\\'])
            || path.contains(['#', '\\'])
        {
            return Err("endpoint is not canonical".to_owned());
        }
        let (host, port) = authority.rsplit_once(':').map_or_else(
            || Ok::<_, String>((authority.to_owned(), 443)),
            |(host, port)| {
                Ok((
                    host.to_owned(),
                    port.parse::<u16>()
                        .map_err(|_| "endpoint port is invalid".to_owned())?,
                ))
            },
        )?;
        if host.is_empty() || host.parse::<IpAddr>().is_ok() || !valid_dns_name(&host) {
            return Err("TLS endpoint must use a canonical DNS name".to_owned());
        }
        Ok(Self {
            host,
            port,
            base_path: if path.is_empty() {
                String::new()
            } else {
                format!("/{}", path.trim_end_matches('/'))
            },
        })
    }

    fn authority(&self) -> String {
        if self.port == 443 {
            self.host.clone()
        } else {
            format!("{}:{}", self.host, self.port)
        }
    }

    fn path(&self, suffix: &str) -> Result<String, String> {
        if suffix.is_empty() {
            return Ok(if self.base_path.is_empty() {
                "/".to_owned()
            } else {
                self.base_path.clone()
            });
        }
        if !suffix.starts_with('/') || suffix.contains(['#', '\\']) {
            return Err("request path is invalid".to_owned());
        }
        Ok(format!("{}{}", self.base_path, suffix))
    }
}

fn valid_dns_name(value: &str) -> bool {
    value.len() <= 253
        && value.split('.').all(|label| {
            !label.is_empty()
                && label.len() <= 63
                && !label.starts_with('-')
                && !label.ends_with('-')
                && label
                    .bytes()
                    .all(|byte| byte.is_ascii_alphanumeric() || byte == b'-')
        })
}

fn public_ip(address: IpAddr) -> bool {
    match address {
        IpAddr::V4(value) => {
            !(value.is_private()
                || value.is_loopback()
                || value.is_link_local()
                || value.is_multicast()
                || value.is_broadcast()
                || value.is_documentation()
                || value.is_unspecified()
                || value.octets()[0] == 0
                || value.octets()[0] >= 224
                || matches!(value.octets(), [100, 64..=127, _, _])
                || matches!(value.octets(), [192, 0, 0, _])
                || matches!(value.octets(), [198, 18 | 19, _, _]))
        }
        IpAddr::V6(value) => {
            if let Some(mapped) = value.to_ipv4_mapped() {
                return public_ip(IpAddr::V4(mapped));
            }
            let segments = value.segments();
            let ipv4_compatible = segments[..6].iter().all(|segment| *segment == 0);
            let translation = segments[0] == 0x0064 && segments[1] == 0xff9b;
            let discard_only =
                segments[0] == 0x0100 && segments[1..4].iter().all(|segment| *segment == 0);
            let teredo = segments[0] == 0x2001 && segments[1] == 0;
            let benchmarking = segments[0] == 0x2001 && segments[1] == 2;
            let orchid = segments[0] == 0x2001 && matches!(segments[1] & 0xfff0, 0x0010 | 0x0020);
            let six_to_four = segments[0] == 0x2002;
            let documentation = (segments[0] == 0x2001 && segments[1] == 0x0db8)
                || (segments[0] == 0x3fff && segments[1] & 0xf000 == 0);
            let deprecated_site_local = segments[0] & 0xffc0 == 0xfec0;
            !(value.is_loopback()
                || value.is_unspecified()
                || value.is_multicast()
                || (value.segments()[0] & 0xfe00) == 0xfc00
                || (value.segments()[0] & 0xffc0) == 0xfe80
                || ipv4_compatible
                || translation
                || discard_only
                || teredo
                || benchmarking
                || orchid
                || six_to_four
                || documentation
                || deprecated_site_local)
        }
    }
}

fn resolve(endpoint: &Endpoint, require_public: bool) -> Result<Vec<SocketAddr>, String> {
    let addresses: Vec<SocketAddr> = (endpoint.host.as_str(), endpoint.port)
        .to_socket_addrs()
        .map_err(|error| error.to_string())?
        .take(16)
        .collect();
    if addresses.is_empty()
        || (require_public && addresses.iter().any(|value| !public_ip(value.ip())))
    {
        return Err("endpoint DNS resolved outside the permitted public range".to_owned());
    }
    Ok(addresses)
}

#[derive(Clone)]
pub(crate) struct ClientIdentity {
    ca: Certificate,
    identity: Option<Identity>,
}

impl ClientIdentity {
    pub(crate) fn new(ca: Certificate, identity: Option<Identity>) -> Self {
        Self { ca, identity }
    }
}

#[derive(Clone, Debug)]
pub(crate) struct Response {
    pub(crate) status: u16,
    pub(crate) content_type: String,
    pub(crate) body: Vec<u8>,
}

#[derive(Clone, Copy, Debug)]
pub(crate) struct OutboundRequest<'a> {
    pub(crate) endpoint: &'a Endpoint,
    pub(crate) method: &'a str,
    pub(crate) path: &'a str,
    pub(crate) bearer: Option<&'a str>,
    pub(crate) idempotency: Option<&'a str>,
    pub(crate) headers: &'a [(String, String)],
    pub(crate) body: &'a [u8],
}

pub(crate) struct Client {
    identity: ClientIdentity,
    public_only: bool,
}

impl Client {
    pub(crate) fn trusted(identity: ClientIdentity) -> Self {
        Self {
            identity,
            public_only: false,
        }
    }

    pub(crate) fn public(identity: ClientIdentity) -> Self {
        Self {
            identity,
            public_only: true,
        }
    }

    pub(crate) fn validate_destination(&self, endpoint: &Endpoint) -> Result<(), String> {
        resolve(endpoint, self.public_only).map(|_| ())
    }

    pub(crate) fn request(&self, request: &OutboundRequest<'_>) -> Result<Response, String> {
        self.request_deadline(request, None)
    }

    pub(crate) fn request_until(
        &self,
        request: &OutboundRequest<'_>,
        deadline: Instant,
    ) -> Result<Response, String> {
        self.request_deadline(request, Some(deadline))
    }

    fn request_deadline(
        &self,
        request: &OutboundRequest<'_>,
        deadline: Option<Instant>,
    ) -> Result<Response, String> {
        let OutboundRequest {
            endpoint,
            method,
            path,
            bearer,
            idempotency,
            headers,
            body,
        } = *request;
        if body.len() > MAX_BODY
            || !matches!(method, "GET" | "POST" | "DELETE")
            || bearer.is_some_and(|value| value.is_empty() || value.len() > 4096)
            || idempotency.is_some_and(|value| value.is_empty() || value.len() > 128)
        {
            return Err("outbound request exceeds its boundary".to_owned());
        }
        let path = endpoint.path(path)?;
        let addresses = match deadline {
            None => resolve(endpoint, self.public_only)?,
            Some(deadline) => {
                let addresses = resolve_until(endpoint.host.clone(), endpoint.port, deadline)?;
                if addresses.is_empty()
                    || (self.public_only && addresses.iter().any(|value| !public_ip(value.ip())))
                {
                    return Err(
                        "endpoint DNS resolved outside the permitted public range".to_owned()
                    );
                }
                addresses
            }
        };
        let mut builder = TlsConnector::builder();
        builder
            .add_root_certificate(self.identity.ca.clone())
            .min_protocol_version(Some(native_tls::Protocol::Tlsv12));
        if let Some(identity) = &self.identity.identity {
            builder.identity(identity.clone());
        }
        let connector = builder.build().map_err(|error| error.to_string())?;
        let mut last = None;
        for address in addresses {
            let budget = deadline.map_or(Ok(CONNECT_TIMEOUT), |deadline| {
                remaining(deadline).map(|value| value.min(CONNECT_TIMEOUT))
            })?;
            match TcpStream::connect_timeout(&address, budget) {
                Ok(tcp) => {
                    tcp.set_read_timeout(Some(IO_TIMEOUT))
                        .map_err(|error| error.to_string())?;
                    tcp.set_write_timeout(Some(IO_TIMEOUT))
                        .map_err(|error| error.to_string())?;
                    let mut stream = connector
                        .connect(
                            &endpoint.host,
                            DeadlineTcp {
                                socket: tcp,
                                deadline,
                                io_timeout: IO_TIMEOUT,
                            },
                        )
                        .map_err(|error| error.to_string())?;
                    let authorization = bearer.map_or_else(String::new, |value| {
                        format!("Authorization: Bearer {value}\r\n")
                    });
                    let idempotency = idempotency
                        .map_or_else(String::new, |value| format!("Idempotency-Key: {value}\r\n"));
                    let mut extra = String::new();
                    for (name, value) in headers {
                        if name.is_empty()
                            || name.contains(['\r', '\n', ':'])
                            || value.contains(['\r', '\n'])
                            || name.len() > 64
                            || value.len() > 4096
                        {
                            return Err("outbound header is invalid".to_owned());
                        }
                        extra.push_str(name);
                        extra.push_str(": ");
                        extra.push_str(value);
                        extra.push_str("\r\n");
                    }
                    if let Some(deadline) = deadline {
                        let absolute = SystemTime::now()
                            .checked_add(remaining(deadline)?)
                            .and_then(|value| value.duration_since(UNIX_EPOCH).ok())
                            .ok_or("invalid admission deadline")?;
                        extra.push_str(&format!(
                            "x-layerx-admission-deadline-ms: {}\r\n",
                            absolute.as_millis()
                        ));
                    }
                    write!(
                        stream,
                        "{method} {path} HTTP/1.1\r\nHost: {}\r\nAccept: application/json\r\nContent-Type: application/json\r\nUser-Agent: LayerX-Hosted/1\r\n{authorization}{idempotency}{extra}Content-Length: {}\r\nConnection: close\r\n\r\n",
                        endpoint.authority(),
                        body.len()
                    )
                    .map_err(|error| error.to_string())?;
                    stream.write_all(body).map_err(|error| error.to_string())?;
                    stream.flush().map_err(|error| error.to_string())?;
                    let response = read_response(&mut stream)?;
                    if let Some(deadline) = deadline {
                        remaining(deadline)?;
                    }
                    return Ok(response);
                }
                Err(error) => last = Some(error),
            }
        }
        Err(last.map_or_else(
            || "endpoint did not resolve".to_owned(),
            |error| error.to_string(),
        ))
    }
}

fn read_response(stream: &mut impl Read) -> Result<Response, String> {
    let mut bytes = Vec::with_capacity(2048);
    let mut chunk = [0_u8; 2048];
    let header_end = loop {
        let count = stream.read(&mut chunk).map_err(|error| error.to_string())?;
        if count == 0 || bytes.len().saturating_add(count) > MAX_BODY {
            return Err("HTTP response is empty or exceeds its bound".to_owned());
        }
        bytes.extend_from_slice(&chunk[..count]);
        if let Some(position) = bytes.windows(4).position(|window| window == b"\r\n\r\n") {
            if position + 4 > MAX_HEADERS {
                return Err("HTTP response headers exceed their bound".to_owned());
            }
            break position + 4;
        }
    };
    let source = std::str::from_utf8(&bytes[..header_end])
        .map_err(|_| "HTTP response headers are not UTF-8".to_owned())?;
    let mut lines = source.split("\r\n");
    let start = lines
        .next()
        .ok_or_else(|| "HTTP status is missing".to_owned())?;
    let mut parts = start.split_whitespace();
    if parts.next() != Some("HTTP/1.1") {
        return Err("HTTP response must use HTTP/1.1".to_owned());
    }
    let status = parts
        .next()
        .ok_or_else(|| "HTTP status is missing".to_owned())?
        .parse::<u16>()
        .map_err(|_| "HTTP status is invalid".to_owned())?;
    let mut headers = BTreeMap::new();
    let mut content_length = 0_usize;
    for line in lines.filter(|line| !line.is_empty()) {
        let (name, value) = line
            .split_once(':')
            .ok_or_else(|| "HTTP response header is malformed".to_owned())?;
        let name = name.trim().to_ascii_lowercase();
        if name.is_empty() || headers.contains_key(&name) {
            return Err("HTTP response has duplicate headers".to_owned());
        }
        let value = value.trim().to_owned();
        if name == "transfer-encoding" {
            return Err("transfer-encoded responses are refused".to_owned());
        }
        if name == "content-length" {
            content_length = value
                .parse::<usize>()
                .map_err(|_| "HTTP response length is invalid".to_owned())?;
        }
        headers.insert(name, value);
    }
    if header_end.saturating_add(content_length) > MAX_BODY {
        return Err("HTTP response body exceeds its bound".to_owned());
    }
    while bytes.len() < header_end + content_length {
        let count = stream.read(&mut chunk).map_err(|error| error.to_string())?;
        if count == 0 || bytes.len().saturating_add(count) > MAX_BODY {
            return Err("HTTP response body is truncated".to_owned());
        }
        bytes.extend_from_slice(&chunk[..count]);
    }
    Ok(Response {
        status,
        content_type: headers.get("content-type").cloned().unwrap_or_default(),
        body: bytes[header_end..header_end + content_length].to_vec(),
    })
}

#[cfg(test)]
mod tests {
    use std::net::{IpAddr, Ipv4Addr, Ipv6Addr};

    use super::public_ip;

    #[test]
    fn public_destination_guard_rejects_translation_and_tunnel_ranges() {
        assert!(!public_ip(IpAddr::V6(Ipv6Addr::new(
            0x0064, 0xff9b, 0, 0, 0, 0, 0xc0a8, 1,
        ))));
        assert!(!public_ip(IpAddr::V6(Ipv6Addr::new(
            0x2002, 0xc0a8, 1, 0, 0, 0, 0, 1,
        ))));
        assert!(!public_ip(IpAddr::V6(Ipv6Addr::new(
            0x2001, 0, 0, 0, 0, 0, 0, 1,
        ))));
        assert!(!public_ip(IpAddr::V6(Ipv6Addr::new(
            0x0100, 0, 0, 0, 0, 0, 0, 1,
        ))));
    }

    #[test]
    fn public_destination_guard_preserves_global_addresses() {
        assert!(public_ip(IpAddr::V4(Ipv4Addr::new(1, 1, 1, 1))));
        assert!(public_ip(IpAddr::V6(Ipv6Addr::new(
            0x2606, 0x4700, 0x4700, 0, 0, 0, 0, 0x1111,
        ))));
    }
}
