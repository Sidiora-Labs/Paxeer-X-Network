use std::io::{self, ErrorKind, Read as _, Write as _};
use std::net::{SocketAddr, ToSocketAddrs as _};
use std::path::{Path, PathBuf};
use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::Arc;
use std::time::{Duration, Instant};

use crate::canonical;
use crate::fetch::{self, Fetcher, HttpClient, Url};
use crate::payment::PaymentGate;
use crate::server::{QueryError, Request, Response, Route, RouteError, RouteTable};

/// The largest canonical encoding the store keeps or accepts from a peer.
pub const MAX_CONTENT_BYTES: usize = 8_388_608;

/// Marks a request from a peer sidecar: it is answered from the local store
/// only and never forwarded to further peers.
pub const PEER_HEADER: &str = "X-Websearch-Peer";

const PEER_CONNECT_TIMEOUT: Duration = Duration::from_secs(3);
const PEER_TOTAL_TIMEOUT: Duration = Duration::from_secs(10);

/// Canonical bytes stored under their digest in the data directory.
pub struct ContentStore {
    directory: PathBuf,
    peers: Vec<Url>,
    client: HttpClient,
    sequence: AtomicU64,
}

fn invalid(message: &'static str) -> io::Error {
    io::Error::new(ErrorKind::InvalidData, message)
}

impl ContentStore {
    /// Opens the store under `data_dir/content`, creating it when absent.
    ///
    /// # Errors
    /// Refuses a peer that is not an http or https URL with no query, and
    /// returns the error creating the directory.
    pub fn open(data_dir: &Path, peers: &[String]) -> io::Result<Self> {
        let peers = peers
            .iter()
            .map(|peer| {
                Url::parse(peer)
                    .ok()
                    .filter(|url| !url.target.contains('?'))
                    .ok_or_else(|| io::Error::new(ErrorKind::InvalidInput, "invalid peer url"))
            })
            .collect::<io::Result<Vec<_>>>()?;
        let directory = data_dir.join("content");
        std::fs::create_dir_all(&directory)?;
        std::fs::File::open(data_dir)?.sync_all()?;
        let client = HttpClient::new(PEER_CONNECT_TIMEOUT)
            .map_err(|error| io::Error::other(error.code()))?;
        Ok(Self {
            directory,
            peers,
            client,
            sequence: AtomicU64::new(0),
        })
    }

    #[must_use]
    pub fn directory(&self) -> &Path {
        &self.directory
    }

    fn path_of(&self, digest: &[u8; 32]) -> PathBuf {
        self.directory.join(canonical::digest_hex(digest))
    }

    /// Stores canonical bytes of any kind - a fetched page, a search or an
    /// api answer - under their digest and returns the digest.
    ///
    /// # Errors
    /// Refuses bytes that are not one canonical encoding or are larger than
    /// [`MAX_CONTENT_BYTES`], and returns the error writing the file.
    pub fn put(&self, bytes: &[u8]) -> io::Result<[u8; 32]> {
        if bytes.len() > MAX_CONTENT_BYTES {
            return Err(invalid("content too large"));
        }
        canonical::check(bytes).map_err(|_| invalid("not canonical content"))?;
        let digest = canonical::content_digest(bytes);
        let path = self.path_of(&digest);
        match std::fs::File::open(&path) {
            Ok(file) => {
                let mut existing = Vec::new();
                (&file)
                    .take(MAX_CONTENT_BYTES as u64 + 1)
                    .read_to_end(&mut existing)?;
                if existing.len() > MAX_CONTENT_BYTES
                    || canonical::content_digest(&existing) != digest
                {
                    return Err(invalid("stored content digest mismatch"));
                }
                file.sync_all()?;
                std::fs::File::open(&self.directory)?.sync_all()?;
                return Ok(digest);
            }
            Err(error) if error.kind() == ErrorKind::NotFound => {}
            Err(error) => return Err(error),
        }
        let temporary = self.directory.join(format!(
            ".{}.{}.{}.tmp",
            canonical::digest_hex(&digest),
            std::process::id(),
            self.sequence.fetch_add(1, Ordering::Relaxed)
        ));
        let mut file = std::fs::OpenOptions::new()
            .write(true)
            .create_new(true)
            .open(&temporary)?;
        let written = (|| -> io::Result<()> {
            file.write_all(bytes)?;
            file.sync_all()?;
            std::fs::rename(&temporary, &path)?;
            std::fs::File::open(&self.directory)?.sync_all()
        })();
        if written.is_err() {
            let _ = std::fs::remove_file(&temporary);
        }
        written.map(|()| digest)
    }

    /// The bytes stored locally under a digest. A file whose bytes no longer
    /// hash to its name is removed and reads as absent.
    ///
    /// # Errors
    /// Returns the error reading the file.
    pub fn get(&self, digest: &[u8; 32]) -> io::Result<Option<Vec<u8>>> {
        let path = self.path_of(digest);
        let bytes = match std::fs::read(&path) {
            Ok(bytes) => bytes,
            Err(error) if error.kind() == ErrorKind::NotFound => return Ok(None),
            Err(error) => return Err(error),
        };
        if canonical::content_digest(&bytes) == *digest {
            Ok(Some(bytes))
        } else {
            std::fs::remove_file(&path)?;
            Ok(None)
        }
    }

    /// The bytes for a digest from the local store or, failing that, from
    /// the first peer whose answer hashes to the digest; that answer is
    /// stored before it is returned.
    ///
    /// # Errors
    /// Returns the error reading or writing the local store.
    pub fn retrieve(&self, digest: &[u8; 32]) -> io::Result<Option<Vec<u8>>> {
        if let Some(bytes) = self.get(digest)? {
            return Ok(Some(bytes));
        }
        for peer in &self.peers {
            let Some(bytes) = self.ask_peer(peer, digest) else {
                continue;
            };
            if canonical::content_digest(&bytes) != *digest || canonical::check(&bytes).is_err() {
                continue;
            }
            self.put(&bytes)?;
            return Ok(Some(bytes));
        }
        Ok(None)
    }

    fn ask_peer(&self, peer: &Url, digest: &[u8; 32]) -> Option<Vec<u8>> {
        let url = Url {
            target: format!(
                "{}/content/{}",
                peer.target.trim_end_matches('/'),
                canonical::digest_hex(digest)
            ),
            ..peer.clone()
        };
        let address: SocketAddr = (url.bare_host(), url.port).to_socket_addrs().ok()?.next()?;
        let response = self
            .client
            .get(
                &url,
                address,
                Instant::now() + PEER_TOTAL_TIMEOUT,
                MAX_CONTENT_BYTES,
                &[("Accept", "application/octet-stream"), (PEER_HEADER, "1")],
            )
            .ok()?;
        (response.status == 200).then_some(response.body)
    }

    /// The `GET /content/<digest>` resource: exactly the stored bytes, a
    /// peer's bytes that hash to the digest, or 404. A request from a peer
    /// is answered from the local store only.
    #[must_use]
    pub fn handle(&self, request: &Request) -> Response {
        let Some(digest) = request.digest else {
            return Response::error(400, "malformed_digest");
        };
        let found = if request.header(PEER_HEADER).is_some() {
            self.get(&digest)
        } else {
            self.retrieve(&digest)
        };
        match found {
            Ok(Some(bytes)) => Response::new(200, "application/octet-stream", bytes),
            Ok(None) => Response::error(404, "content_not_found"),
            Err(_) => Response::error(500, "content_store_error"),
        }
    }
}

/// Registers `GET /fetch` behind the payment gate and `GET /content/<digest>`
/// unpaid.
///
/// # Errors
/// Refuses a route that already has a handler.
pub fn register(
    routes: &mut RouteTable,
    gate: &Arc<PaymentGate>,
    fetcher: &Arc<Fetcher>,
    store: &Arc<ContentStore>,
) -> Result<(), RouteError> {
    let (fetch_fetcher, fetch_store) = (Arc::clone(fetcher), Arc::clone(store));
    PaymentGate::install_validated(
        gate,
        routes,
        Route::Fetch,
        validate_fetch_request,
        move |request: &Request| fetch::fetch_route(&fetch_fetcher, &fetch_store, request),
    )?;
    let content_store = Arc::clone(store);
    routes.set(Route::Content, move |request: &Request| {
        content_store.handle(request)
    })
}

fn validate_fetch_request(request: &Request) -> Result<(), Response> {
    let url = match request.query_param("url") {
        Ok(Some(url)) if !url.is_empty() => url,
        Ok(_) => return Err(Response::error(400, "missing_url")),
        Err(QueryError::Duplicate) => return Err(Response::error(400, "duplicate_url")),
        Err(QueryError::Malformed) => return Err(Response::error(400, "malformed_query")),
    };
    for pair in request.query.as_deref().unwrap_or_default().split('&') {
        if pair.is_empty() || !pair.contains('=') {
            return Err(Response::error(400, "malformed_query"));
        }
        let mut parameter = request.clone();
        parameter.query = Some(pair.to_owned());
        match parameter.query_param("url") {
            Ok(Some(_)) => {}
            Ok(None) => return Err(Response::error(400, "unknown_query_parameter")),
            Err(_) => return Err(Response::error(400, "malformed_query")),
        }
    }
    let mut bytes = url.bytes();
    while let Some(byte) = bytes.next() {
        if byte == b'%' {
            if !bytes.next().is_some_and(|byte| byte.is_ascii_hexdigit())
                || !bytes.next().is_some_and(|byte| byte.is_ascii_hexdigit())
            {
                return Err(Response::error(400, "invalid_url"));
            }
        } else if !byte.is_ascii_alphanumeric() && !b"-._~:/?#[]@!$&'()*+,;=".contains(&byte) {
            return Err(Response::error(400, "invalid_url"));
        }
    }
    Url::parse(&url)
        .map(|_| ())
        .map_err(|error| Response::error(error.status(), error.code()))
}
