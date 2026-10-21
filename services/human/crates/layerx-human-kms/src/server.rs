use crate::config::Config;
use crate::store::Store;
use crate::wire::{self, Error, Operation, Request, Result};
use rustls::{ServerConnection, StreamOwned};
use sha2::{Digest, Sha256};
use std::io::{Read, Write};
use std::net::{TcpListener, TcpStream};
use std::sync::atomic::{AtomicUsize, Ordering};
use std::sync::{Arc, Mutex};
use std::thread;
use zeroize::Zeroizing;

struct Permit(Arc<AtomicUsize>);
impl Drop for Permit {
    fn drop(&mut self) {
        self.0.fetch_sub(1, Ordering::AcqRel);
    }
}
pub(crate) fn run(
    config: Config,
    clock: &Arc<dyn layerx_types::clock::Clock>,
) -> std::result::Result<(), String> {
    let store = Arc::new(Mutex::new(Store::open(&config)?));
    let listener = TcpListener::bind(config.listen).map_err(|_| "KMS listener unavailable")?;
    let config = Arc::new(config);
    let active = Arc::new(AtomicUsize::new(0));
    for stream in listener.incoming() {
        let Ok(stream) = stream else {
            return Err("KMS accept failed".into());
        };
        if active
            .fetch_update(Ordering::AcqRel, Ordering::Acquire, |count| {
                (count < 16).then_some(count + 1)
            })
            .is_err()
        {
            continue;
        }
        let permit = Permit(Arc::clone(&active));
        let config = Arc::clone(&config);
        let store = Arc::clone(&store);
        let clock = Arc::clone(clock);
        thread::Builder::new()
            .name("human-kms".into())
            .spawn(move || {
                let _permit = permit;
                let _ = connection(stream, &config, &store, clock.as_ref());
            })
            .map_err(|_| "KMS worker unavailable")?;
    }
    Err("KMS listener stopped".into())
}
fn connection(
    mut tcp: TcpStream,
    config: &Config,
    store: &Mutex<Store>,
    clock: &dyn layerx_types::clock::Clock,
) -> Result<()> {
    tcp.set_read_timeout(Some(config.deadline))
        .map_err(|_| Error::Unavailable)?;
    tcp.set_write_timeout(Some(config.deadline))
        .map_err(|_| Error::Unavailable)?;
    let mut connection =
        ServerConnection::new(Arc::clone(&config.tls)).map_err(|_| Error::Unavailable)?;
    while connection.is_handshaking() {
        connection
            .complete_io(&mut tcp)
            .map_err(|_| Error::Refused)?;
    }
    let leaf = connection
        .peer_certificates()
        .and_then(|chain| chain.first())
        .ok_or(Error::Refused)?;
    let observed: [u8; 32] = Sha256::digest(leaf.as_ref()).into();
    let service = layerx_crypto::ct::eq_fixed(&observed, &config.client_pin);
    let executor = config
        .evm_client_pin
        .is_some_and(|pin| layerx_crypto::ct::eq_fixed(&observed, &pin));
    if !service && !executor {
        return Err(Error::Refused);
    }
    let mut tls = StreamOwned::new(connection, tcp);
    let frame = read_request(&mut tls)?;
    let request = Request::decode(&frame)?;
    let now = clock
        .sample(std::time::Duration::from_secs(1))
        .map_err(|_| Error::Unavailable)?
        .unix_seconds();
    let admission = if request.kind == Operation::ExecutorProbe {
        if executor {
            Ok(None)
        } else {
            Err(Error::Refused)
        }
    } else if !service && !matches!(request.operation, 6 | 8..=10 | 12) {
        Err(Error::Refused)
    } else {
        validate_sign(&request, config, now)
    };
    let answer = admission.and_then(|digest| {
        store
            .lock()
            .map_err(|_| Error::Unavailable)?
            .dispatch(&request, digest, now)
    });
    write_response(
        &mut tls,
        &wire::response(request.version, request.operation, answer),
    )
}
fn read_request(reader: &mut impl Read) -> Result<Zeroizing<Vec<u8>>> {
    let mut prefix = [0_u8; 4];
    reader.read_exact(&mut prefix).map_err(|_| Error::Refused)?;
    let length = usize::try_from(u32::from_be_bytes(prefix)).map_err(|_| Error::Refused)?;
    if length == 0 || length > wire::MAX_FRAME {
        return Err(Error::Refused);
    }
    let mut payload = Zeroizing::new(vec![0_u8; length]);
    reader
        .read_exact(&mut payload)
        .map_err(|_| Error::Refused)?;
    Ok(payload)
}
fn write_response(writer: &mut impl Write, payload: &[u8]) -> Result<()> {
    if payload.len() > wire::MAX_FRAME {
        return Err(Error::Unavailable);
    }
    let length = u32::try_from(payload.len()).map_err(|_| Error::Unavailable)?;
    writer
        .write_all(&length.to_be_bytes())
        .and_then(|()| writer.write_all(payload))
        .and_then(|()| writer.flush())
        .map_err(|_| Error::Unavailable)
}
fn validate_sign(request: &Request<'_>, config: &Config, now: u64) -> Result<Option<[u8; 32]>> {
    if request.operation == 13 {
        return if config.protocol == 3 && request.network == config.network && request.class == 1 {
            Ok(None)
        } else {
            Err(Error::Refused)
        };
    }
    if request.operation == 11 {
        let value = serde_json::from_slice(request.evm).map_err(|_| Error::Refused)?;
        return crate::send::digest(
            &value,
            request.binding,
            config.network,
            config.protocol,
            now,
        )
        .map(Some);
    }
    if request.operation != 5 {
        return Ok(None);
    }
    let activity =
        layerx_intents::canonical::decode_unsigned_activity(request.canonical, &config.registry)
            .map_err(|_| Error::Refused)?;
    if activity.network_id() != config.network
        || activity.protocol_version() != config.protocol
        || request.network != config.network
    {
        return Err(Error::Refused);
    }
    let disclosure = layerx_crypto::disclosure::bind(request.canonical, &config.registry)
        .map_err(|_| Error::Refused)?;
    if disclosure.reencode().map_err(|_| Error::Refused)? != request.canonical
        || wire::disclosure(&disclosure)? != request.disclosure
    {
        return Err(Error::Refused);
    }
    let mut hash = Sha256::new();
    hash.update(b"LXP/v1/signature-preimage\0");
    hash.update(request.canonical);
    let digest: [u8; 32] = hash.finalize().into();
    if !layerx_crypto::ct::eq_fixed(&digest, &request.digest) {
        return Err(Error::Integrity);
    }
    Ok(Some(digest))
}
