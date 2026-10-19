//! Offchain locator admission: HTTPS on 443 only, tenant host allowlist, every
//! DNS answer public, and the connected peer re-checked before any byte is sent.
use layerx_programs_ai_market::evidence::ArtifactError;
use std::net::{IpAddr, Ipv4Addr, Ipv6Addr, SocketAddr, TcpStream, ToSocketAddrs};

pub const MAX_LOCATOR_BYTES: usize = 2048;

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Locator {
    pub host: String,
    pub path: String,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct AdmittedLocator {
    pub locator: Locator,
    pub addrs: Vec<SocketAddr>,
}

const UNSAFE: ArtifactError = ArtifactError::UnsafeLocator;

pub fn parse_locator(uri: &str) -> Result<Locator, ArtifactError> {
    if uri.len() > MAX_LOCATOR_BYTES || uri.chars().any(|c| c.is_control() || c.is_whitespace()) {
        return Err(UNSAFE);
    }
    let rest = uri.strip_prefix("https://").ok_or(UNSAFE)?;
    if rest.contains('#') || rest.contains('\\') {
        return Err(UNSAFE);
    }
    let split = rest.find(['/', '?']).unwrap_or(rest.len());
    let (authority, path) = rest.split_at(split);
    if authority.is_empty() || authority.contains('@') || authority.contains('%') {
        return Err(UNSAFE);
    }
    let host = if let Some(v6) = authority.strip_prefix('[') {
        let (literal, tail) = v6.split_once(']').ok_or(UNSAFE)?;
        if !(tail.is_empty() || tail == ":443") {
            return Err(UNSAFE);
        }
        literal.parse::<Ipv6Addr>().map_err(|_| UNSAFE)?;
        literal.to_ascii_lowercase()
    } else {
        let host = match authority.rsplit_once(':') {
            Some((host, "443")) => host,
            Some(_) => return Err(UNSAFE),
            None => authority,
        };
        if host.is_empty()
            || !host
                .bytes()
                .all(|b| b.is_ascii_alphanumeric() || b == b'-' || b == b'.')
        {
            return Err(UNSAFE);
        }
        host.to_ascii_lowercase()
    };
    let path = if path.is_empty() { "/" } else { path };
    Ok(Locator {
        host,
        path: path.to_string(),
    })
}

fn public_v4(ip: Ipv4Addr) -> bool {
    let [a, b, c, _] = ip.octets();
    !(ip.is_unspecified()
        || ip.is_loopback()
        || ip.is_private()
        || ip.is_link_local()
        || ip.is_multicast()
        || ip.is_broadcast()
        || ip.is_documentation()
        || a == 0
        || a >= 240
        || (a == 100 && (64..128).contains(&b))
        || (a == 192 && b == 0 && c == 0)
        || (a == 198 && (b == 18 || b == 19)))
}

pub fn is_public(ip: IpAddr) -> bool {
    match ip {
        IpAddr::V4(v4) => public_v4(v4),
        IpAddr::V6(v6) => {
            if let Some(mapped) = v6.to_ipv4_mapped() {
                return public_v4(mapped);
            }
            let s = v6.segments();
            !(v6.is_unspecified()
                || v6.is_loopback()
                || v6.is_multicast()
                || (s[0] & 0xfe00) == 0xfc00
                || (s[0] & 0xffc0) == 0xfe80
                || (s[0] & 0xffc0) == 0xfec0
                || (s[0] == 0x2001 && s[1] == 0x0db8)
                || (s[0] == 0x0064 && s[1] == 0xff9b)
                || (s[0] == 0x2002)
                || (s[0] == 0x2001 && s[1] == 0))
        }
    }
}

/// Resolves exactly once; every answer must be public. The returned addresses are
/// the only connection targets, so a later rebinding answer is never consulted.
pub fn admit(
    uri: &str,
    allowlist: &[String],
    resolve: impl FnOnce(&str) -> std::io::Result<Vec<IpAddr>>,
) -> Result<AdmittedLocator, ArtifactError> {
    let locator = parse_locator(uri)?;
    if !allowlist
        .iter()
        .any(|h| h.eq_ignore_ascii_case(&locator.host))
    {
        return Err(UNSAFE);
    }
    let answers = match locator.host.parse::<IpAddr>() {
        Ok(ip) => vec![ip],
        Err(_) => resolve(&locator.host).map_err(|_| ArtifactError::ContentUnavailable)?,
    };
    if answers.is_empty() || !answers.iter().all(|ip| is_public(*ip)) {
        return Err(UNSAFE);
    }
    let addrs = answers
        .into_iter()
        .map(|ip| SocketAddr::new(ip, 443))
        .collect();
    Ok(AdmittedLocator { locator, addrs })
}

pub fn system_dns(host: &str) -> std::io::Result<Vec<IpAddr>> {
    Ok((host, 443).to_socket_addrs()?.map(|a| a.ip()).collect())
}

impl AdmittedLocator {
    /// Connects to the first admitted address and refuses, before writing any
    /// request byte or credential, when the actual peer is not that public address.
    pub fn connect(
        &self,
        connect: impl FnOnce(SocketAddr) -> std::io::Result<TcpStream>,
    ) -> Result<TcpStream, ArtifactError> {
        let target = *self.addrs.first().ok_or(UNSAFE)?;
        let stream = connect(target).map_err(|_| ArtifactError::ContentUnavailable)?;
        let peer = stream.peer_addr().map_err(|_| UNSAFE)?;
        if peer != target || !is_public(peer.ip()) {
            let _ = stream.shutdown(std::net::Shutdown::Both);
            return Err(UNSAFE);
        }
        Ok(stream)
    }
}

/// Redirects are bounded at zero: any 3xx refuses; only 200 admits a body.
pub fn check_response_head(head: &[u8]) -> Result<(), ArtifactError> {
    let line = head.split(|b| *b == b'\n').next().unwrap_or_default();
    let status = std::str::from_utf8(line)
        .ok()
        .and_then(|l| l.split_whitespace().nth(1))
        .and_then(|s| s.parse::<u16>().ok())
        .ok_or(ArtifactError::Malformed)?;
    match status {
        200 => Ok(()),
        300..=399 => Err(UNSAFE),
        _ => Err(ArtifactError::ContentUnavailable),
    }
}
