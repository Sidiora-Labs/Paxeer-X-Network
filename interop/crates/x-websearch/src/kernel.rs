//! Program web requests: a kernel program emits a request record under the
//! `PAXEERX_WEB_REQUEST_V1` topic in the same call that pays the web fee
//! account. `KernelWatcher` follows those records through the gateway,
//! `KernelAttestor` fetches or searches each payload independently and signs
//! the origin-2 digest, `ProgramExchange` trades those signatures with the
//! peer attestors under the program id and request id together, and
//! `KernelRelay` posts the observation activity with `lx_sendActivity` once
//! the registered threshold agrees.

use std::collections::BTreeMap;
use std::io::{self, Write as _};
use std::net::{SocketAddr, ToSocketAddrs as _};
use std::path::{Path, PathBuf};
use std::sync::{Arc, Mutex, PoisonError};
use std::time::{Duration, Instant};

use ed25519_dalek::{Signer as _, SigningKey as SubmitterKey};
use k256::ecdsa::SigningKey;
use layerx_wire::encode::Encoder;
use layerx_wire::hash::Domain;
use layerx_wire::limits::{MAX_MESSAGE_BYTES, PROTOCOL_VERSION};
use layerx_wire::WireError;
use serde_json::{json, Value};
use sha2::{Digest as _, Sha256};

use crate::attest::{
    network_word, recover_signer, sign_digest, signer_address, stored_response, Answer,
    AttestError, Attestation, AttestorSet, Discard, Level, Ready, MAX_RECORD_BYTES,
    MAX_RESPONSE_BYTES, ORIGIN_PROGRAM, SIGNATURE_LENGTH,
};
use crate::content::{ContentStore, PEER_HEADER};
use crate::fetch::{Fetcher, HttpClient, Url};
use crate::index::WebIndex;
use crate::payment::{hex, unhex, GatewayRpc, RpcAnswer, COMMITMENT};
use crate::fetch::FetchError;
use crate::search;
use crate::server::{Response, RouteError, RouteTable};
use crate::watch::{hex0x, keccak, unhex0x};

pub use crate::server::PROGRAM_ATTESTATION_PATH;

/// The topic every program web request record is emitted under.
pub const REQUEST_TOPIC: &[u8] = b"PAXEERX_WEB_REQUEST_V1";

/// The topics whose records the watcher decodes as program web requests.
pub const REQUEST_TOPICS: [&[u8]; 1] = [REQUEST_TOPIC];

/// Whether `topic` is one whose records the watcher decodes.
#[must_use]
pub fn request_topic(topic: &[u8]) -> bool {
    REQUEST_TOPICS.contains(&topic)
}

/// The gateway method that lists committed program events by topic.
pub const EVENTS_METHOD: &str = "lx_getProgramEvents";

/// The most events one poll asks the gateway for.
pub const EVENTS_PER_POLL: u64 = 256;

/// The activity type of a web observation: module 11, ordinal 1.
pub const OBSERVATION_ACTIVITY: u32 = 0x000B_0001;

/// Request id, kind and payload length ahead of the payload.
pub const RECORD_HEADER_BYTES: usize = 13;

/// The observation header ahead of the stored response.
pub const OBSERVATION_HEADER_BYTES: usize = 146;

/// The most attestor signatures one observation carries.
pub const MAX_OBSERVATION_SIGNATURES: usize = 32;

/// How long a posted observation activity stays valid.
pub const ACTIVITY_VALIDITY_MS: u64 = 60_000;

const ACTIVITY_STRUCTURE: u16 = 0x1001;
const ACTIVITY_FIELDS: u8 = 12;
const UNSIGNED_ACTIVITY_FIELDS: u8 = 11;
const MAX_DID_BYTES: usize = 255;
const MAX_ACTIVITY_PAYLOAD_BYTES: usize = 524_288;
const MAX_ACTIVITY_SIGNATURE_BYTES: usize = 128;
const CURSOR_FILE: &str = "kernel-cursor";
const PEER_CONNECT_TIMEOUT: Duration = Duration::from_secs(3);
const PEER_TOTAL_TIMEOUT: Duration = Duration::from_secs(10);
const DISCARD_LOG: &str = "program-discarded.jsonl";

/// The relay journal under the watcher's state directory.
pub const RELAY_JOURNAL_FILE: &str = "kernel-relay.json";

/// The version tag every relay journal carries.
pub const RELAY_JOURNAL_VERSION: &str = "PAXEERX_KERNEL_RELAY_V1";

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum KernelError {
    Authorization,
    /// The gateway endpoint is not an http or https URL with a host.
    Endpoint,
    /// No well-formed answer arrived.
    Unavailable,
    /// The gateway answered with a JSON-RPC error.
    Rejected { code: i64 },
    /// The answer does not have the shape the method defines.
    Malformed,
    /// A request record is not a canonical program web request.
    Record,
    /// The watcher's cursor could not be read or written.
    Cursor,
    /// The observation or its activity could not be encoded.
    Encode,
    /// The relay journal could not be read, is not this version or could
    /// not be written.
    Journal,
}

impl std::fmt::Display for KernelError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::Authorization => f.write_str("kernel gateway authorization refused"),
            Self::Endpoint => f.write_str("gateway endpoint refused"),
            Self::Unavailable => f.write_str("gateway unavailable"),
            Self::Rejected { code } => write!(f, "gateway rejected the call with {code}"),
            Self::Malformed => f.write_str("gateway answer malformed"),
            Self::Record => f.write_str("program web request record malformed"),
            Self::Cursor => f.write_str("kernel watch cursor unreadable or unwritable"),
            Self::Encode => f.write_str("observation activity could not be encoded"),
            Self::Journal => f.write_str("kernel relay journal unreadable or unwritable"),
        }
    }
}

impl std::error::Error for KernelError {}

impl From<WireError> for KernelError {
    fn from(_: WireError) -> Self {
        Self::Encode
    }
}

/// One committed program web request.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ProgramRequest {
    pub program_id: [u8; 32],
    pub request_id: u64,
    pub kind: u8,
    pub payload: Vec<u8>,
    pub sequence: u64,
}

/// Decodes the record a program emits: the request id big-endian, the kind,
/// the payload length big-endian and the payload.
///
/// # Errors
/// Refuses an unknown kind, an empty payload and a length that does not
/// match the record.
pub fn decode_request_record(
    program_id: [u8; 32],
    sequence: u64,
    data: &[u8],
) -> Result<ProgramRequest, KernelError> {
    if data.len() < RECORD_HEADER_BYTES {
        return Err(KernelError::Record);
    }
    let (header, payload) = data.split_at(RECORD_HEADER_BYTES);
    let mut id = [0; 8];
    id.copy_from_slice(&header[..8]);
    let mut length = [0; 4];
    length.copy_from_slice(&header[9..13]);
    let length = usize::try_from(u32::from_be_bytes(length)).map_err(|_| KernelError::Record)?;
    let kind = header[8];
    if !matches!(kind, 1 | 2) || length == 0 || payload.len() != length {
        return Err(KernelError::Record);
    }
    Ok(ProgramRequest {
        program_id,
        request_id: u64::from_be_bytes(id),
        kind,
        payload: payload.to_vec(),
        sequence,
    })
}

impl ProgramRequest {
    /// The origin-2 attestation of this request's answer.
    #[must_use]
    pub fn attestation(
        &self,
        network_id: u32,
        content_digest: [u8; 32],
        response: &[u8],
        full_length: u32,
    ) -> Attestation {
        Attestation {
            origin: ORIGIN_PROGRAM,
            network_id: network_word(u64::from(network_id)),
            requester: self.program_id,
            request_id: self.request_id,
            kind: self.kind,
            payload_hash: keccak(&self.payload),
            content_digest,
            response_hash: keccak(response),
            full_length,
        }
    }
}

fn call(rpc: &GatewayRpc, method: &str, params: &Value) -> Result<Value, KernelError> {
    match rpc.call(method, params) {
        Some(RpcAnswer::Result(value)) => Ok(value),
        Some(RpcAnswer::Error { code, .. }) => Err(KernelError::Rejected { code }),
        None => Err(KernelError::Unavailable),
    }
}

fn fixed<const N: usize>(value: Option<&Value>) -> Option<[u8; N]> {
    unhex(value?.as_str()?)?.try_into().ok()
}

fn decode_event(
    event: &Value,
    topic: &[u8],
    from: u64,
    next: u64,
) -> Result<ProgramRequest, KernelError> {
    let object = event.as_object().ok_or(KernelError::Malformed)?;
    let sequence = object
        .get("sequence")
        .and_then(Value::as_u64)
        .filter(|sequence| (from..next).contains(sequence))
        .ok_or(KernelError::Malformed)?;
    let program_id = fixed::<32>(object.get("program_id")).ok_or(KernelError::Malformed)?;
    let emitted = object
        .get("topic")
        .and_then(Value::as_str)
        .and_then(unhex)
        .ok_or(KernelError::Malformed)?;
    if emitted != topic {
        return Err(KernelError::Malformed);
    }
    let data = object
        .get("data")
        .and_then(Value::as_str)
        .and_then(unhex)
        .ok_or(KernelError::Malformed)?;
    decode_request_record(program_id, sequence, &data)
}

/// Follows committed program web request records through the gateway. The
/// next global sequence to read is kept in a cursor file under the state
/// directory, so a restart resumes where the last poll stopped.
pub struct KernelWatcher {
    rpc: GatewayRpc,
    cursor_path: PathBuf,
    next_sequence: u64,
    topics: Vec<Vec<u8>>,
}

impl KernelWatcher {
    /// Opens the watcher over [`REQUEST_TOPIC`]. A cursor file already under
    /// `state_dir` wins over `start`.
    ///
    /// # Errors
    /// Refuses an endpoint that is not an http or https URL, and returns the
    /// error creating the directory and a cursor file that does not hold one
    /// sequence number.
    pub fn open(endpoint: &str, state_dir: &Path, start: u64) -> io::Result<Self> {
        let rpc = GatewayRpc::new(endpoint)
            .map_err(|_| io::Error::new(io::ErrorKind::InvalidInput, "invalid gateway endpoint"))?;
        std::fs::create_dir_all(state_dir)?;
        let cursor_path = state_dir.join(CURSOR_FILE);
        let stored = match std::fs::read_to_string(&cursor_path) {
            Ok(text) => Some(text.trim().parse::<u64>().map_err(|_| {
                io::Error::new(io::ErrorKind::InvalidData, "kernel watch cursor malformed")
            })?),
            Err(error) if error.kind() == io::ErrorKind::NotFound => None,
            Err(error) => return Err(error),
        };
        Ok(Self {
            rpc,
            cursor_path,
            next_sequence: stored.unwrap_or(start),
            topics: vec![REQUEST_TOPIC.to_vec()],
        })
    }

    pub fn with_authorization_file(mut self, path: &Path) -> io::Result<Self> {
        self.rpc = self.rpc.with_authorization_file(path)
            .map_err(|_| io::Error::new(io::ErrorKind::PermissionDenied, "kernel gateway authorization refused"))?;
        Ok(self)
    }

    /// Watches `topics` instead of [`REQUEST_TOPIC`] alone.
    ///
    /// # Errors
    /// Refuses an empty list, a repeated topic and a topic whose records are
    /// not program web requests.
    pub fn with_topics(mut self, topics: &[&[u8]]) -> io::Result<Self> {
        let refused = topics.is_empty()
            || topics.iter().any(|topic| !request_topic(topic))
            || topics
                .iter()
                .enumerate()
                .any(|(index, topic)| topics[..index].contains(topic));
        if refused {
            return Err(io::Error::new(
                io::ErrorKind::InvalidInput,
                "kernel watch topics refused",
            ));
        }
        self.topics = topics.iter().map(|topic| topic.to_vec()).collect();
        Ok(self)
    }

    /// The topics the watcher reads, in the order it asks for them.
    #[must_use]
    pub fn topics(&self) -> &[Vec<u8>] {
        &self.topics
    }

    /// The next global sequence the watcher reads.
    #[must_use]
    pub const fn next_sequence(&self) -> u64 {
        self.next_sequence
    }

    fn store_cursor(&self, next: u64) -> Result<(), KernelError> {
        let temporary = self.cursor_path.with_extension("tmp");
        std::fs::write(&temporary, next.to_string())
            .and_then(|()| std::fs::File::open(&temporary)?.sync_all())
            .and_then(|()| std::fs::rename(&temporary, &self.cursor_path))
            .map_err(|_| KernelError::Cursor)
    }

    fn poll_topic(&self, topic: &[u8], from: u64) -> Result<(u64, Vec<Value>), KernelError> {
        let answer = call(
            &self.rpc,
            EVENTS_METHOD,
            &json!([{
                "topic": hex(topic),
                "from_sequence": from,
                "limit": EVENTS_PER_POLL,
            }]),
        )?;
        let next = answer
            .get("next_sequence")
            .and_then(Value::as_u64)
            .filter(|next| *next >= from)
            .ok_or(KernelError::Malformed)?;
        let events = answer
            .get("events")
            .and_then(Value::as_array)
            .filter(|events| u64::try_from(events.len()).is_ok_and(|n| n <= EVENTS_PER_POLL))
            .ok_or(KernelError::Malformed)?;
        Ok((next, events.clone()))
    }

    /// Reads the request records committed at or after the cursor under
    /// every watched topic, at most [`EVENTS_PER_POLL`] per topic, in
    /// sequence order. The cursor moves to the lowest next sequence the
    /// topics report, so a record past it is read again by the next poll and
    /// never skipped, and only after every record decoded.
    ///
    /// # Errors
    /// Returns the gateway's error, a malformed answer or record and a
    /// cursor that could not be written.
    pub fn poll(&mut self) -> Result<Vec<ProgramRequest>, KernelError> {
        let (next, found) = self.fetch()?;
        if next != self.next_sequence {
            self.resume_at(next)?;
        }
        Ok(found.into_iter().map(|(_, request)| request).collect())
    }

    /// Moves the cursor to `next` and writes it under the state directory.
    ///
    /// # Errors
    /// Returns a cursor that could not be written.
    pub fn resume_at(&mut self, next: u64) -> Result<(), KernelError> {
        self.store_cursor(next)?;
        self.next_sequence = next;
        Ok(())
    }

    /// What [`KernelWatcher::poll`] reads, with the topic of each record and
    /// the sequence the cursor would move to, without moving it.
    ///
    /// # Errors
    /// Returns the gateway's error and a malformed answer or record.
    pub fn fetch(&self) -> Result<(u64, Vec<(Vec<u8>, ProgramRequest)>), KernelError> {
        let from = self.next_sequence;
        let mut answers = Vec::with_capacity(self.topics.len());
        for topic in &self.topics {
            let (next, events) = self.poll_topic(topic, from)?;
            answers.push((topic, next, events));
        }
        let next = answers
            .iter()
            .map(|(_, next, _)| *next)
            .min()
            .ok_or(KernelError::Malformed)?;
        let mut requests = Vec::new();
        for (topic, reported, events) in &answers {
            let mut decoded = events
                .iter()
                .map(|event| decode_event(event, topic, from, *reported))
                .collect::<Result<Vec<_>, _>>()?;
            if decoded
                .windows(2)
                .any(|pair| pair[0].sequence >= pair[1].sequence)
            {
                return Err(KernelError::Malformed);
            }
            decoded.retain(|request| request.sequence < next);
            requests.extend(decoded.into_iter().map(|request| (topic.to_vec(), request)));
        }
        requests.sort_by_key(|(_, request)| request.sequence);
        if requests
            .windows(2)
            .any(|pair| pair[0].1.sequence == pair[1].1.sequence)
        {
            return Err(KernelError::Malformed);
        }
        Ok((next, requests))
    }
}

/// Fetches or searches a program request's payload independently and signs
/// the origin-2 digest with the web-attestor key.
pub struct KernelAttestor {
    key: SigningKey,
    signer: [u8; 20],
    network_id: u32,
    fetcher: Arc<Fetcher>,
    index: Arc<WebIndex>,
    store: Arc<ContentStore>,
}

impl KernelAttestor {
    #[must_use]
    pub fn new(
        key: SigningKey,
        network_id: u32,
        fetcher: Arc<Fetcher>,
        index: Arc<WebIndex>,
        store: Arc<ContentStore>,
    ) -> Self {
        Self {
            signer: signer_address(&key),
            key,
            network_id,
            fetcher,
            index,
            store,
        }
    }

    /// The address this attestor signs as.
    #[must_use]
    pub const fn signer(&self) -> [u8; 20] {
        self.signer
    }

    fn content(&self, request: &ProgramRequest) -> Result<([u8; 32], String), AttestError> {
        let payload = std::str::from_utf8(&request.payload).map_err(|_| AttestError::Payload)?;
        let (canonical, text) = match request.kind {
            1 => {
                let page = self.fetcher.fetch(payload).map_err(AttestError::Fetch)?;
                (page.canonical, page.text)
            }
            2 => {
                let results: Vec<search::SearchResult> = search::search(&self.index, payload)
                    .map_err(|_| AttestError::Search)?
                    .into_iter()
                    .map(|scored| scored.result)
                    .collect();
                let canonical = search::search_canonical_bytes(payload, &results)
                    .map_err(|_| AttestError::Search)?;
                let text = search::search_text(&results).map_err(|_| AttestError::Search)?;
                (canonical, text)
            }
            kind => return Err(AttestError::UnknownKind(kind)),
        };
        let digest = self.store.put(&canonical).map_err(|_| AttestError::Store)?;
        Ok((digest, text))
    }

    /// Answers one program request: the content, the stored response and the
    /// signature over the origin-2 digest. A program request carries no
    /// callback gas and no timeout height.
    ///
    /// # Errors
    /// Returns why the request could not be answered.
    pub fn attest(&self, request: &ProgramRequest) -> Result<Answer, AttestError> {
        let (content_digest, text) = self.content(request)?;
        let (response, full_length) = stored_response(&text)?;
        let attestation =
            request.attestation(self.network_id, content_digest, &response, full_length);
        let digest = attestation.digest();
        let signature = sign_digest(&self.key, &digest).map_err(AttestError::Sign)?;
        Ok(Answer {
            attestation,
            level: Level::Majority,
            response,
            callback_gas: 0,
            timeout_height: u64::MAX,
            digest,
            signer: self.signer,
            signature,
        })
    }
}

/// The observation payload the kernel's web intake admits: the 146-byte
/// header, the stored response, the signature count and the signatures in
/// ascending signer order.
///
/// # Errors
/// Refuses a response longer than a fulfilment stores, a full length shorter
/// than the response and a signature count outside one to
/// [`MAX_OBSERVATION_SIGNATURES`].
pub fn observation_bytes(
    network_id: u32,
    request: &ProgramRequest,
    ready: &Ready,
) -> Result<Vec<u8>, KernelError> {
    let response_length = u32::try_from(ready.response.len()).map_err(|_| KernelError::Encode)?;
    let count = ready.signatures.len();
    if network_id == 0
        || ready.request_id != request.request_id
        || ready.response.len() > MAX_RESPONSE_BYTES
        || ready.full_length < response_length
        || count == 0
        || count > MAX_OBSERVATION_SIGNATURES
    {
        return Err(KernelError::Encode);
    }
    let digest = request.attestation(network_id, ready.content_digest, &ready.response, ready.full_length).digest();
    if request.program_id == [0; 32] || !matches!(request.kind, 1 | 2)
        || ready.digest != digest || ready.signers.len() != count
        || ready.signers.windows(2).any(|pair| pair[0] >= pair[1])
        || ready.signers.iter().zip(&ready.signatures)
            .any(|(signer, signature)| recover_signer(&digest, signature).ok() != Some(*signer)) {
        return Err(KernelError::Encode);
    }
    let mut out = Vec::with_capacity(
        OBSERVATION_HEADER_BYTES + ready.response.len() + 1 + count * SIGNATURE_LENGTH,
    );
    out.push(ORIGIN_PROGRAM);
    out.extend_from_slice(&[0; 28]);
    out.extend_from_slice(&network_id.to_be_bytes());
    out.extend_from_slice(&request.program_id);
    out.extend_from_slice(&request.request_id.to_be_bytes());
    out.push(request.kind);
    out.extend_from_slice(&keccak(&request.payload));
    out.extend_from_slice(&ready.content_digest);
    out.extend_from_slice(&ready.full_length.to_be_bytes());
    out.extend_from_slice(&response_length.to_be_bytes());
    out.extend_from_slice(&ready.response);
    out.push(u8::try_from(count).map_err(|_| KernelError::Encode)?);
    for signature in &ready.signatures {
        out.extend_from_slice(signature);
    }
    Ok(out)
}

fn observation_signers(bytes: &[u8], digest: &[u8; 32])
    -> Result<(Vec<[u8; 20]>, Vec<[u8; SIGNATURE_LENGTH]>), KernelError> {
    if bytes.len() <= OBSERVATION_HEADER_BYTES { return Err(KernelError::Journal); }
    let length = u32::from_be_bytes(bytes[142..146].try_into().map_err(|_| KernelError::Journal)?);
    let offset = OBSERVATION_HEADER_BYTES.checked_add(length as usize).ok_or(KernelError::Journal)?;
    let count = usize::from(*bytes.get(offset).ok_or(KernelError::Journal)?);
    if count == 0 || count > MAX_OBSERVATION_SIGNATURES || bytes.len() != offset + 1 + count * SIGNATURE_LENGTH {
        return Err(KernelError::Journal);
    }
    let mut signers = Vec::new();
    let mut signatures = Vec::new();
    for bytes in bytes[offset + 1..].chunks_exact(SIGNATURE_LENGTH) {
        let signature = bytes.try_into().map_err(|_| KernelError::Journal)?;
        signers.push(recover_signer(digest, &signature).map_err(|_| KernelError::Journal)?);
        signatures.push(signature);
    }
    Ok((signers, signatures))
}

fn domain_hash(domain: Domain, bytes: &[u8]) -> [u8; 32] {
    let mut hash = Sha256::new();
    hash.update(domain.tag());
    hash.update(bytes);
    hash.finalize().into()
}

/// The envelope fields of one observation activity.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct ActivityOptions<'a> {
    pub network_id: u32,
    pub actor_did: &'a str,
    pub account_sequence: u64,
    pub fee_limit: u128,
    pub not_before: u64,
    pub not_after: u64,
}

fn encode_envelope(
    observation: &[u8],
    authority: &[u8; 32],
    options: &ActivityOptions<'_>,
    signature: Option<&[u8; 64]>,
    protocol_version: u16,
) -> Result<Vec<u8>, KernelError> {
    let mut encoder = Encoder::new(MAX_MESSAGE_BYTES);
    encoder.structure_header_version(ACTIVITY_STRUCTURE, protocol_version)?;
    encoder.u8(if signature.is_some() {
        ACTIVITY_FIELDS
    } else {
        UNSIGNED_ACTIVITY_FIELDS
    })?;
    encoder.tag(1, ACTIVITY_FIELDS)?;
    encoder.u16(protocol_version)?;
    encoder.tag(2, ACTIVITY_FIELDS)?;
    encoder.u32(options.network_id)?;
    encoder.tag(3, ACTIVITY_FIELDS)?;
    encoder.u32(OBSERVATION_ACTIVITY)?;
    encoder.tag(4, ACTIVITY_FIELDS)?;
    encoder.bytes(options.actor_did.as_bytes(), MAX_DID_BYTES)?;
    encoder.tag(5, ACTIVITY_FIELDS)?;
    encoder.bytes(authority, MAX_ACTIVITY_PAYLOAD_BYTES)?;
    encoder.tag(6, ACTIVITY_FIELDS)?;
    encoder.u64(options.account_sequence)?;
    encoder.tag(7, ACTIVITY_FIELDS)?;
    encoder.u64(options.not_before)?;
    encoder.u64(options.not_after)?;
    encoder.tag(8, ACTIVITY_FIELDS)?;
    encoder.bytes(&domain_hash(Domain::ContextHash, observation), 32)?;
    encoder.tag(9, ACTIVITY_FIELDS)?;
    encoder.u128(options.fee_limit)?;
    encoder.tag(10, ACTIVITY_FIELDS)?;
    encoder.bytes(&domain_hash(Domain::PayloadHash, observation), 32)?;
    encoder.tag(11, ACTIVITY_FIELDS)?;
    encoder.bytes(observation, MAX_ACTIVITY_PAYLOAD_BYTES)?;
    if let Some(signature) = signature {
        encoder.tag(12, ACTIVITY_FIELDS)?;
        encoder.bytes(signature, MAX_ACTIVITY_SIGNATURE_BYTES)?;
    }
    Ok(encoder.finish())
}

/// The signed observation activity: the twelve-field envelope of type
/// [`OBSERVATION_ACTIVITY`], its idempotency key the context hash of the
/// observation and its signature the submitter's Ed25519 signature over the
/// signing preimage of the eleven-field unsigned form.
///
/// # Errors
/// Refuses an empty or oversized DID, a zero network or fee limit and a
/// timestamp bound that ends before it starts.
pub fn encode_activity(
    observation: &[u8],
    submitter: &SubmitterKey,
    options: &ActivityOptions<'_>,
) -> Result<Vec<u8>, KernelError> {
    encode_activity_version(observation, submitter, options, PROTOCOL_VERSION)
}

fn encode_activity_version(
    observation: &[u8], submitter: &SubmitterKey, options: &ActivityOptions<'_>, protocol_version: u16,
) -> Result<Vec<u8>, KernelError> {
    if options.actor_did.is_empty()
        || options.actor_did.len() > MAX_DID_BYTES
        || options.network_id == 0
        || options.fee_limit == 0
        || options.not_before > options.not_after
    {
        return Err(KernelError::Encode);
    }
    let authority = submitter.verifying_key().to_bytes();
    let unsigned = encode_envelope(observation, &authority, options, None, protocol_version)?;
    let preimage = domain_hash(Domain::SignaturePreimage, &unsigned);
    let signature = submitter.sign(&preimage).to_bytes();
    encode_envelope(observation, &authority, options, Some(&signature), protocol_version)
}

/// Posts observation activities through the gateway as the submitter DID.
pub struct ObservationSubmitter {
    rpc: GatewayRpc,
    key: SubmitterKey,
    did: String,
    network_id: u32,
    fee_limit: u128,
    receipt_key: Option<[u8; 32]>,
}

impl ObservationSubmitter {
    /// # Errors
    /// Refuses an endpoint that is not an http or https URL with a host.
    pub fn new(
        endpoint: &str,
        key: SubmitterKey,
        did: String,
        network_id: u32,
        fee_limit: u128,
    ) -> Result<Self, KernelError> {
        Ok(Self {
            rpc: GatewayRpc::new(endpoint).map_err(|_| KernelError::Endpoint)?,
            key,
            did,
            network_id,
            fee_limit,
            receipt_key: None,
        })
    }

    pub fn with_authorization_file(mut self, path: &Path) -> Result<Self, KernelError> {
        self.rpc = self.rpc.with_authorization_file(path).map_err(|_| KernelError::Authorization)?;
        Ok(self)
    }

    #[must_use]
    pub fn with_receipt_key(mut self, key: [u8; 32]) -> Self {
        self.receipt_key = Some(key);
        self
    }

    fn outcome(&self, answer: Option<RpcAnswer>, activity_id: &[u8; 32]) -> Outcome {
        let Some(RpcAnswer::Result(value)) = answer else {
            return Outcome::Open;
        };
        let Some(key) = self.receipt_key else { return Outcome::Open; };
        if fixed::<32>(value.get("activity_id")) != Some(*activity_id) {
            return Outcome::Open;
        }
        let Some(bytes) = value.get("receipt").and_then(Value::as_str).and_then(unhex) else {
            return Outcome::Open;
        };
        let Ok(receipt) = layerx_proof::receipt::verify_sequencer_signature(&bytes, key) else {
            return Outcome::Open;
        };
        let Some(facts) = receipt.protocol() else { return Outcome::Open; };
        if facts.activity_id() != *activity_id || facts.module_id() != 11
            || facts.operation() != 1 || facts.fee_charged() > self.fee_limit {
            return Outcome::Open;
        }
        if facts.result_code() != 0 {
            return Outcome::Rejected(i64::from(facts.result_code()), value);
        }
        Outcome::Completed(value)
    }

    fn next_sequence(&self) -> Result<u64, KernelError> {
        let answer = call(&self.rpc, "lx_getSequence", &json!([self.did, "identity"]))?;
        let text = answer
            .get("next_sequence")
            .and_then(Value::as_str)
            .ok_or(KernelError::Malformed)?;
        if text.is_empty()
            || (text.len() > 1 && text.starts_with('0'))
            || !text.bytes().all(|byte| byte.is_ascii_digit())
        {
            return Err(KernelError::Malformed);
        }
        text.parse().map_err(|_| KernelError::Malformed)
    }

    /// Signs the observation at the submitter's next identity sequence,
    /// valid from one second before `now_ms` for [`ACTIVITY_VALIDITY_MS`],
    /// and posts it with `lx_sendActivity`. Returns the gateway's result.
    ///
    /// # Errors
    /// Returns the gateway's error, a malformed sequence answer and an
    /// activity that could not be encoded.
    pub fn submit(&self, observation: &[u8], now_ms: u64) -> Result<Value, KernelError> {
        let signed = self.sign(observation, now_ms)?;
        match self.send(&signed.activity) {
            Some(RpcAnswer::Result(value)) => Ok(value),
            Some(RpcAnswer::Error { code, .. }) => Err(KernelError::Rejected { code }),
            None => Err(KernelError::Unavailable),
        }
    }

    /// The submitter DID the activities are posted as.
    #[must_use]
    pub fn did(&self) -> &str {
        &self.did
    }

    /// Signs the observation at the submitter's next identity sequence,
    /// valid from one second before `now_ms` for [`ACTIVITY_VALIDITY_MS`],
    /// without posting it.
    ///
    /// # Errors
    /// Returns the gateway's error, a malformed sequence answer and an
    /// activity that could not be encoded.
    pub fn sign(&self, observation: &[u8], now_ms: u64) -> Result<Signed, KernelError> {
        let account_sequence = self.next_sequence()?;
        let not_after = now_ms.saturating_add(ACTIVITY_VALIDITY_MS);
        let activity = encode_activity_version(
            observation,
            &self.key,
            &ActivityOptions {
                network_id: self.network_id,
                actor_did: &self.did,
                account_sequence,
                fee_limit: self.fee_limit,
                not_before: now_ms.saturating_sub(1_000),
                not_after,
            },
            layerx_wire::limits::STATE_COMMITMENT_PROTOCOL_VERSION,
        )?;
        Ok(Signed {
            activity_id: domain_hash(Domain::ActivityId, &activity),
            activity,
            account_sequence,
            not_after,
        })
    }

    /// Posts exact signed activity bytes with `lx_sendActivity`. `None` means
    /// the outcome is unknown.
    #[must_use]
    pub fn send(&self, activity: &[u8]) -> Option<RpcAnswer> {
        self.rpc
            .call("lx_sendActivity", &json!([hex(activity), COMMITMENT]))
    }

    /// Reads the committed outcome of an activity: `lx_getActivityStatus`,
    /// then `lx_getReceipt` when the status carries no receipt and is not
    /// pending. `None` means the outcome is unknown.
    #[must_use]
    pub fn lookup(&self, activity_id: &[u8; 32]) -> Option<RpcAnswer> {
        let id = hex(activity_id);
        let status = self.rpc.call("lx_getActivityStatus", &json!([id]));
        let needs_receipt = matches!(
            &status,
            Some(RpcAnswer::Result(value))
                if value.get("receipt").is_none()
                    && value.get("state").and_then(Value::as_str) != Some("pending")
        );
        if needs_receipt {
            self.rpc.call("lx_getReceipt", &json!([id]))
        } else {
            status
        }
    }

    /// Whether the submitter's identity sequence has moved past
    /// `account_sequence`, so an activity signed at it can no longer be
    /// admitted.
    ///
    /// # Errors
    /// Returns the gateway's error and a malformed sequence answer.
    pub fn consumed(&self, account_sequence: u64) -> Result<bool, KernelError> {
        Ok(self.next_sequence()? > account_sequence)
    }
}

/// One signed observation activity, kept exactly as it was signed.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Signed {
    pub activity: Vec<u8>,
    pub activity_id: [u8; 32],
    pub account_sequence: u64,
    pub not_after: u64,
}

/// The key every program answer is held under: the program id and the
/// request id together, so two programs' request ids never share a slot.
pub type ProgramKey = ([u8; 32], u64);

/// One peer signature for a program request that was discarded, as recorded.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ProgramDiscarded {
    pub peer: String,
    pub program_id: [u8; 32],
    pub request_id: u64,
    pub reason: Discard,
    pub claimed_digest: Option<[u8; 32]>,
    pub claimed_signer: Option<[u8; 20]>,
}

impl ProgramDiscarded {
    fn line(&self) -> String {
        json!({
            "peer": self.peer,
            "program_id": hex0x(&self.program_id),
            "request_id": self.request_id,
            "reason": self.reason.code(),
            "claimed_digest": self.claimed_digest.map(|digest| hex0x(&digest)),
            "claimed_signer": self.claimed_signer.map(|signer| hex0x(&signer)),
        })
        .to_string()
    }
}

/// The record the program signature-exchange route serves for an answer:
/// the fields of [`Answer::record`] with the program id beside the request
/// id.
#[must_use]
pub fn program_record(answer: &Answer) -> Value {
    let mut record = answer.record();
    if let Some(object) = record.as_object_mut() {
        object.insert(
            "program_id".to_owned(),
            Value::String(hex0x(&answer.attestation.requester)),
        );
    }
    record
}

struct ProgramRecord {
    program_id: [u8; 32],
    request_id: u64,
    digest: [u8; 32],
    signer: [u8; 20],
    signature: [u8; SIGNATURE_LENGTH],
}

fn fixed0x<const N: usize>(value: Option<&Value>) -> Option<[u8; N]> {
    unhex0x(value?.as_str()?)?.try_into().ok()
}

fn parse_program_record(record: &Value) -> Option<ProgramRecord> {
    let object = record.as_object()?;
    let known = [
        "program_id",
        "request_id",
        "digest",
        "content_digest",
        "response_hash",
        "full_length",
        "signer",
        "signature",
    ];
    if object.keys().any(|key| !known.contains(&key.as_str())) {
        return None;
    }
    fixed0x::<32>(object.get("content_digest"))?;
    fixed0x::<32>(object.get("response_hash"))?;
    u32::try_from(object.get("full_length")?.as_u64()?).ok()?;
    Some(ProgramRecord {
        program_id: fixed0x(object.get("program_id"))?,
        request_id: object.get("request_id")?.as_u64()?,
        digest: fixed0x(object.get("digest"))?,
        signer: fixed0x(object.get("signer"))?,
        signature: fixed0x(object.get("signature"))?,
    })
}

struct Held {
    answer: Answer,
    signatures: BTreeMap<[u8; 20], [u8; SIGNATURE_LENGTH]>,
}

fn held_value(held: &Held) -> Value {
    json!({
        "response": hex(&held.answer.response),
        "content_digest": hex(&held.answer.attestation.content_digest),
        "full_length": held.answer.attestation.full_length,
        "signer": hex(&held.answer.signer),
        "signature": hex(&held.answer.signature),
        "signatures": held.signatures.iter().map(|(signer, signature)|
            json!({"signer": hex(signer), "signature": hex(signature)})).collect::<Vec<_>>(),
    })
}

/// Exchanges attestor signatures for program requests with the configured
/// peer sidecars, holding every answer under its [`ProgramKey`].
///
/// Each sidecar serves its own signed record for a program request at
/// `GET /program-attestations/<program id>/<request id>`. A record is taken
/// only when it names the same program and request, its signature recovers
/// over this sidecar's own digest to the signer it claims, and that signer is
/// a registered attestor. Every refusal is recorded, never accepted.
pub struct ProgramExchange {
    peers: Vec<(String, Url)>,
    client: HttpClient,
    answers: Mutex<BTreeMap<ProgramKey, Held>>,
    discarded: Mutex<Vec<ProgramDiscarded>>,
    log_path: PathBuf,
}

impl ProgramExchange {
    /// Opens the exchange with its discard log under `state_dir`.
    ///
    /// # Errors
    /// Refuses a peer that is not an http or https URL with no query and
    /// returns the error creating the directory.
    pub fn open(state_dir: &Path, peers: &[String]) -> io::Result<Self> {
        let peers = peers
            .iter()
            .map(|peer| {
                Url::parse(peer)
                    .ok()
                    .filter(|url| !url.target.contains('?'))
                    .map(|url| (peer.clone(), url))
                    .ok_or_else(|| io::Error::new(io::ErrorKind::InvalidInput, "invalid peer url"))
            })
            .collect::<io::Result<Vec<_>>>()?;
        std::fs::create_dir_all(state_dir)?;
        let client = HttpClient::new(PEER_CONNECT_TIMEOUT)
            .map_err(|error| io::Error::other(error.code()))?;
        Ok(Self {
            peers,
            client,
            answers: Mutex::new(BTreeMap::new()),
            discarded: Mutex::new(Vec::new()),
            log_path: state_dir.join(DISCARD_LOG),
        })
    }

    fn snapshot(&self, key: ProgramKey) -> Option<Value> {
        self.answers().get(&key).map(held_value)
    }

    fn restore(&self, request: &ProgramRequest, network_id: u32, value: &Value) -> Result<(), KernelError> {
        let response = value.get("response").and_then(Value::as_str).and_then(unhex).ok_or(KernelError::Journal)?;
        let full_length = value.get("full_length").and_then(Value::as_u64)
            .and_then(|n| u32::try_from(n).ok()).ok_or(KernelError::Journal)?;
        if response.len() > MAX_RESPONSE_BYTES || u64::from(full_length) < response.len() as u64 {
            return Err(KernelError::Journal);
        }
        let attestation = request.attestation(network_id,
            fixed(value.get("content_digest")).ok_or(KernelError::Journal)?, &response, full_length);
        let digest = attestation.digest();
        let signer = fixed(value.get("signer")).ok_or(KernelError::Journal)?;
        let signature = fixed(value.get("signature")).ok_or(KernelError::Journal)?;
        if recover_signer(&digest, &signature).ok() != Some(signer) { return Err(KernelError::Journal); }
        let mut signatures = BTreeMap::new();
        for row in value.get("signatures").and_then(Value::as_array).ok_or(KernelError::Journal)? {
            let signer = fixed(row.get("signer")).ok_or(KernelError::Journal)?;
            let signature = fixed(row.get("signature")).ok_or(KernelError::Journal)?;
            if recover_signer(&digest, &signature).ok() != Some(signer)
                || signatures.insert(signer, signature).is_some() { return Err(KernelError::Journal); }
        }
        if signatures.len() > MAX_OBSERVATION_SIGNATURES || signatures.get(&signer) != Some(&signature) {
            return Err(KernelError::Journal);
        }
        self.answers().insert((request.program_id, request.request_id), Held {
            answer: Answer { attestation, level: Level::Majority, response, callback_gas: 0,
                timeout_height: u64::MAX, digest, signer, signature }, signatures,
        });
        Ok(())
    }

    /// The file every discarded signature is appended to.
    #[must_use]
    pub fn log_path(&self) -> &Path {
        &self.log_path
    }

    fn answers(&self) -> std::sync::MutexGuard<'_, BTreeMap<ProgramKey, Held>> {
        self.answers.lock().unwrap_or_else(PoisonError::into_inner)
    }

    /// Keeps this sidecar's own answer and its own signature under the
    /// answer's program and request. An answer already held there is kept
    /// as it is.
    pub fn record(&self, answer: Answer) {
        self.answers()
            .entry((answer.attestation.requester, answer.request_id()))
            .or_insert_with(|| Held {
                signatures: BTreeMap::from([(answer.signer, answer.signature)]),
                answer,
            });
    }

    /// This sidecar's answer to a program's request.
    #[must_use]
    pub fn answer(&self, key: ProgramKey) -> Option<Answer> {
        self.answers().get(&key).map(|held| held.answer.clone())
    }

    /// The program requests this sidecar holds an answer for, ascending.
    #[must_use]
    pub fn pending(&self) -> Vec<ProgramKey> {
        self.answers().keys().copied().collect()
    }

    /// Drops the answer and signatures for a program's request.
    pub fn forget(&self, key: ProgramKey) {
        self.answers().remove(&key);
    }

    /// Every signature discarded so far, in the order it was discarded.
    #[must_use]
    pub fn discarded(&self) -> Vec<ProgramDiscarded> {
        self.discarded
            .lock()
            .unwrap_or_else(PoisonError::into_inner)
            .clone()
    }

    fn discard(&self, entry: ProgramDiscarded) {
        let line = entry.line();
        eprintln!("x-websearch discarded a peer program signature: {line}");
        let appended = std::fs::OpenOptions::new()
            .create(true)
            .append(true)
            .open(&self.log_path)
            .and_then(|mut file| {
                file.write_all(line.as_bytes())?;
                file.write_all(b"\n")?;
                file.sync_all()
            });
        if let Err(error) = appended {
            eprintln!("x-websearch could not append to the discard log: {error}");
        }
        self.discarded
            .lock()
            .unwrap_or_else(PoisonError::into_inner)
            .push(entry);
    }

    /// The `GET /program-attestations/<program id>/<request id>` resource:
    /// this sidecar's own signed record, or 404.
    #[must_use]
    pub fn handle(&self, key: ProgramKey) -> Response {
        match self.answer(key) {
            Some(answer) => Response::json(200, program_record(&answer).to_string().into_bytes()),
            None => Response::error(404, "attestation_not_found"),
        }
    }

    /// Checks one peer record for a program request against this sidecar's
    /// own answer and the registered set, and keeps its signature. Every
    /// refusal is recorded.
    ///
    /// # Errors
    /// Returns why the signature was discarded. A request this sidecar holds
    /// no answer for is not an error and takes nothing.
    pub fn accept(
        &self,
        peer: &str,
        key: ProgramKey,
        record: &Value,
        set: &AttestorSet,
    ) -> Result<Option<[u8; 20]>, Discard> {
        let Some(local) = self.answer(key) else {
            return Ok(None);
        };
        let parsed = parse_program_record(record);
        let verdict = match &parsed {
            None => Err(Discard::Malformed),
            Some(record) if (record.program_id, record.request_id) != key => {
                Err(Discard::WrongRequest)
            }
            Some(record) if record.digest != local.digest => Err(Discard::DifferentDigest),
            Some(record) => match recover_signer(&local.digest, &record.signature) {
                Ok(signer) if signer != record.signer => Err(Discard::BadSignature),
                Err(_) => Err(Discard::BadSignature),
                Ok(signer) if !set.contains(&signer) => Err(Discard::UnknownSigner),
                Ok(signer) => Ok(signer),
            },
        };
        match verdict {
            Ok(signer) => {
                if let (Some(held), Some(record)) = (self.answers().get_mut(&key), parsed) {
                    held.signatures.insert(signer, record.signature);
                }
                Ok(Some(signer))
            }
            Err(reason) => {
                self.discard(ProgramDiscarded {
                    peer: peer.to_owned(),
                    program_id: key.0,
                    request_id: key.1,
                    reason,
                    claimed_digest: parsed.as_ref().map(|record| record.digest),
                    claimed_signer: parsed.as_ref().map(|record| record.signer),
                });
                Err(reason)
            }
        }
    }

    fn ask(&self, peer: &Url, key: ProgramKey) -> Option<Value> {
        let url = Url {
            target: format!(
                "{}{PROGRAM_ATTESTATION_PATH}{}/{}",
                peer.target.trim_end_matches('/'),
                hex(&key.0),
                key.1
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
                MAX_RECORD_BYTES,
                &[("Accept", "application/json"), (PEER_HEADER, "1")],
            )
            .ok()?;
        (response.status == 200)
            .then(|| serde_json::from_slice(&response.body).ok())
            .flatten()
    }

    /// Asks every peer for its record of a program request and keeps each
    /// signature that checks out. An unreachable peer or one with no record
    /// is skipped. Returns the number of signatures held for the request.
    #[must_use]
    pub fn collect(&self, key: ProgramKey, set: &AttestorSet) -> usize {
        if self.answer(key).is_none() {
            return 0;
        }
        for (name, peer) in &self.peers {
            let Some(record) = self.ask(peer, key) else {
                continue;
            };
            let _ = self.accept(name, key, &record, set);
        }
        self.answers()
            .get(&key)
            .map_or(0, |held| held.signatures.len())
    }

    /// The signatures for the observation once at least the threshold of
    /// registered signers agree with this sidecar's answer, ascending by
    /// signer.
    #[must_use]
    pub fn ready(&self, key: ProgramKey, set: &AttestorSet) -> Option<Ready> {
        let answers = self.answers();
        let held = answers.get(&key)?;
        let answer = &held.answer;
        let registered: Vec<([u8; 20], [u8; SIGNATURE_LENGTH])> = held
            .signatures
            .iter()
            .filter(|(signer, _)| set.contains(signer))
            .map(|(signer, signature)| (*signer, *signature))
            .collect();
        let enough = set.threshold != 0
            && usize::try_from(set.threshold).is_ok_and(|n| n > set.signers.len() / 2 && n <= set.signers.len())
            && set.signers.iter().enumerate().all(|(i, signer)| !set.signers[..i].contains(signer))
            && u32::try_from(registered.len()).is_ok_and(|count| count >= set.threshold);
        if !enough {
            return None;
        }
        Some(Ready {
            request_id: key.1,
            response: answer.response.clone(),
            content_digest: answer.attestation.content_digest,
            full_length: answer.attestation.full_length,
            callback_gas: answer.callback_gas,
            digest: answer.digest,
            signers: registered.iter().map(|(signer, _)| *signer).collect(),
            signatures: registered.iter().map(|(_, signature)| *signature).collect(),
        })
    }
}

/// Registers the program signature-exchange route
/// `GET /program-attestations/<program id>/<request id>`.
///
/// # Errors
/// Refuses a table that already has a program exchange handler.
pub fn register(
    routes: &mut RouteTable,
    exchange: &Arc<ProgramExchange>,
) -> Result<(), RouteError> {
    let exchange = Arc::clone(exchange);
    routes.set_program_attestations(move |program_id, request_id| {
        exchange.handle((program_id, request_id))
    })
}

/// What one relay step did for one request.
#[derive(Clone, Debug, PartialEq)]
pub enum Step {
    /// The observation was posted; the gateway's result.
    Posted {
        program_id: [u8; 32],
        request_id: u64,
        result: Value,
    },
    /// The request could not be answered and is recorded as refused.
    Refused {
        program_id: [u8; 32],
        request_id: u64,
        reason: AttestError,
    },
    /// A journalled observation was found committed by its receipt.
    Committed {
        program_id: [u8; 32],
        request_id: u64,
        activity_id: [u8; 32],
        result: Value,
    },
    /// A journalled observation's outcome is not known yet; it stays queued.
    Unknown {
        program_id: [u8; 32],
        request_id: u64,
        activity_id: [u8; 32],
    },
    /// The gateway refused the observation; it is recorded as rejected.
    Rejected {
        program_id: [u8; 32],
        request_id: u64,
        code: i64,
    },
}

/// Where one discovered request stands.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum Stage {
    /// Answered or not yet, waiting for the registered threshold.
    AwaitingQuorum,
    /// Signed and journalled; sent or about to be sent.
    Submitting,
    /// Sent with no committed outcome read back yet.
    Unknown,
    /// Committed; the receipt was read.
    Completed,
    /// The request cannot be answered.
    Refused,
    /// The gateway refused the observation.
    Rejected,
}

impl Stage {
    /// The stage as the journal names it.
    #[must_use]
    pub const fn code(self) -> &'static str {
        match self {
            Self::AwaitingQuorum => "awaiting_quorum",
            Self::Submitting => "submitting",
            Self::Unknown => "unknown",
            Self::Completed => "completed",
            Self::Refused => "refused",
            Self::Rejected => "rejected",
        }
    }

    fn parse(code: &str) -> Option<Self> {
        [
            Self::AwaitingQuorum,
            Self::Submitting,
            Self::Unknown,
            Self::Completed,
            Self::Refused,
            Self::Rejected,
        ]
        .into_iter()
        .find(|stage| stage.code() == code)
    }

    /// Whether the request is finished: completed, refused or rejected.
    #[must_use]
    pub const fn terminal(self) -> bool {
        matches!(self, Self::Completed | Self::Refused | Self::Rejected)
    }
}

/// One discovered request with its program, request and topic identity and
/// its exact attestation and submission state.
#[derive(Clone, Debug, PartialEq)]
pub struct RelayEntry {
    pub request: ProgramRequest,
    pub topic: Vec<u8>,
    pub stage: Stage,
    /// The observation payload once the threshold agreed.
    pub observation: Option<Vec<u8>>,
    pub attestation: Option<Value>,
    /// The signed activity, journalled before it is sent.
    pub signed: Option<Signed>,
    /// Why the request was refused or rejected.
    pub reason: Option<String>,
    pub last_error: Option<String>,
    /// The committed result.
    pub result: Option<Value>,
}

const ENTRY_KEYS: [&str; 17] = [
    "program_id",
    "request_id",
    "sequence",
    "topic",
    "kind",
    "payload",
    "payload_hash",
    "stage",
    "observation",
    "attestation",
    "activity",
    "activity_id",
    "account_sequence",
    "not_after",
    "reason",
    "last_error",
    "result",
];

impl RelayEntry {
    fn key(&self) -> ProgramKey {
        (self.request.program_id, self.request.request_id)
    }

    fn value(&self) -> Value {
        let signed = self.signed.as_ref();
        json!({
            "program_id": hex0x(&self.request.program_id),
            "request_id": self.request.request_id,
            "sequence": self.request.sequence,
            "topic": hex(&self.topic),
            "kind": self.request.kind,
            "payload": hex(&self.request.payload),
            "payload_hash": hex0x(&keccak(&self.request.payload)),
            "stage": self.stage.code(),
            "observation": self.observation.as_deref().map(hex),
            "attestation": self.attestation,
            "activity": signed.map(|signed| hex(&signed.activity)),
            "activity_id": signed.map(|signed| hex0x(&signed.activity_id)),
            "account_sequence": signed.map(|signed| signed.account_sequence),
            "not_after": signed.map(|signed| signed.not_after),
            "reason": self.reason,
            "last_error": self.last_error,
            "result": self.result,
        })
    }

    fn parse(value: &Value) -> Option<Self> {
        let object = value.as_object()?;
        if object.len() != ENTRY_KEYS.len()
            || object.keys().any(|key| !ENTRY_KEYS.contains(&key.as_str()))
        {
            return None;
        }
        let text = |key: &str| object.get(key).and_then(Value::as_str);
        let optional = |key: &str| object.get(key).filter(|value| !value.is_null());
        let request = ProgramRequest {
            program_id: fixed0x(object.get("program_id"))?,
            request_id: object.get("request_id")?.as_u64()?,
            kind: u8::try_from(object.get("kind")?.as_u64()?).ok()?,
            payload: unhex(text("payload")?)?,
            sequence: object.get("sequence")?.as_u64()?,
        };
        if fixed0x::<32>(object.get("payload_hash"))? != keccak(&request.payload)
            || !matches!(request.kind, 1 | 2)
            || request.payload.is_empty()
        {
            return None;
        }
        let topic = unhex(text("topic")?)?;
        if !request_topic(&topic) {
            return None;
        }
        let stage = Stage::parse(text("stage")?)?;
        let observation = match optional("observation") {
            Some(value) => Some(unhex(value.as_str()?)?),
            None => None,
        };
        let signed = match optional("activity") {
            Some(activity) => {
                let activity = unhex(activity.as_str()?)?;
                let activity_id = fixed0x::<32>(optional("activity_id"))?;
                if activity_id != domain_hash(Domain::ActivityId, &activity) {
                    return None;
                }
                Some(Signed {
                    activity,
                    activity_id,
                    account_sequence: optional("account_sequence")?.as_u64()?,
                    not_after: optional("not_after")?.as_u64()?,
                })
            }
            None if ["activity_id", "account_sequence", "not_after"]
                .iter()
                .any(|key| optional(key).is_some()) =>
            {
                return None;
            }
            None => None,
        };
        let sending = matches!(stage, Stage::Submitting | Stage::Unknown);
        if (sending && (signed.is_none() || observation.is_none()))
            || (signed.is_some() && (observation.is_none() || optional("attestation").is_none()))
            || (matches!(stage, Stage::Completed | Stage::Rejected) && (signed.is_none() || optional("result").is_none()))
            || (stage == Stage::AwaitingQuorum && signed.is_some())
        {
            return None;
        }
        let reason = match optional("reason") {
            Some(reason) => Some(reason.as_str()?.to_owned()),
            None => None,
        };
        if matches!(stage, Stage::Refused | Stage::Rejected) != reason.is_some() {
            return None;
        }
        Some(Self {
            request,
            topic,
            stage,
            observation,
            attestation: optional("attestation").cloned(),
            signed,
            reason,
            last_error: match optional("last_error") {
                Some(value) => Some(value.as_str()?.to_owned()),
                None => None,
            },
            result: optional("result").cloned(),
        })
    }
}

/// Whether an attestation refusal may clear on a later try: an unreachable
/// or failing origin, search, store or signer. Every other refusal is fixed
/// by the request itself.
fn retryable(reason: &AttestError) -> bool {
    match reason {
        AttestError::Fetch(error) => matches!(
            error,
            FetchError::Resolve
                | FetchError::RobotsUnavailable
                | FetchError::Connect
                | FetchError::ConnectTimeout
                | FetchError::Timeout
                | FetchError::Tls
                | FetchError::Transport
        ) || matches!(error, FetchError::Status(status) if *status == 429 || *status >= 500),
        AttestError::Search | AttestError::Store | AttestError::Sign(_) => true,
        AttestError::UnknownKind(_)
        | AttestError::Payload
        | AttestError::TooLong
        | AttestError::Api(_)
        | AttestError::NotNamed(_) => false,
    }
}

/// What a gateway answer about one activity settles.
enum Outcome {
    Completed(Value),
    Open,
    Rejected(i64, Value),
}

/// Ties the watcher, the attestor, the program signature exchange and the
/// submitter together. Every discovered request is journalled with its
/// stage before the watcher's cursor moves past it, and every signed
/// activity before it is sent, so a restart resumes each request where it
/// stood and recovers a sent activity by its receipt before sending again.
pub struct KernelRelay {
    pub watcher: KernelWatcher,
    pub attestor: KernelAttestor,
    pub exchange: Arc<ProgramExchange>,
    pub set: AttestorSet,
    pub submitter: ObservationSubmitter,
    network_id: u32,
    queue: Vec<ProgramRequest>,
    entries: Vec<RelayEntry>,
    journal: Option<PathBuf>,
    _journal_lock: Option<std::fs::File>,
}

impl KernelRelay {
    /// A relay that keeps its queue in memory only.
    #[must_use]
    pub fn new(
        watcher: KernelWatcher,
        attestor: KernelAttestor,
        exchange: Arc<ProgramExchange>,
        set: AttestorSet,
        submitter: ObservationSubmitter,
    ) -> Self {
        Self {
            network_id: attestor.network_id,
            watcher,
            attestor,
            exchange,
            set,
            submitter,
            queue: Vec::new(),
            entries: Vec::new(),
            journal: None,
            _journal_lock: None,
        }
    }

    /// A relay journalled in [`RELAY_JOURNAL_FILE`] under `state_dir`. A
    /// journal already there wins over the watcher's cursor.
    ///
    /// # Errors
    /// Refuses a journal that cannot be read, is not
    /// [`RELAY_JOURNAL_VERSION`] or holds a malformed or repeated entry, and
    /// returns a cursor that could not be written.
    pub fn open(
        watcher: KernelWatcher,
        attestor: KernelAttestor,
        exchange: Arc<ProgramExchange>,
        set: AttestorSet,
        submitter: ObservationSubmitter,
        state_dir: &Path,
    ) -> Result<Self, KernelError> {
        std::fs::create_dir_all(state_dir).map_err(|_| KernelError::Journal)?;
        let path = state_dir.join(RELAY_JOURNAL_FILE);
        let lock = std::fs::OpenOptions::new().create(true).truncate(false).read(true).write(true)
            .open(state_dir.join("kernel-relay.lock")).map_err(|_| KernelError::Journal)?;
        lock.try_lock().map_err(|_| KernelError::Journal)?;
        let mut relay = Self::new(watcher, attestor, exchange, set, submitter);
        relay._journal_lock = Some(lock);
        match std::fs::read(&path) {
            Ok(bytes) => {
                let (next, entries) = parse_journal(&bytes).ok_or(KernelError::Journal)?;
                relay.entries = entries;
                if next != relay.watcher.next_sequence() {
                    relay.watcher.resume_at(next)?;
                }
            }
            Err(error) if error.kind() == io::ErrorKind::NotFound => {
                relay.watcher.resume_at(0)?;
            }
            Err(_) => return Err(KernelError::Journal),
        }
        for entry in &relay.entries {
            if let Some(value) = &entry.attestation {
                relay.exchange.restore(&entry.request, relay.network_id, value)?;
            }
            if let Some(signed) = &entry.signed {
                let observation = entry.observation.as_deref().ok_or(KernelError::Journal)?;
                if signed.activity_id != domain_hash(Domain::ActivityId, &signed.activity) {
                    return Err(KernelError::Journal);
                }
                let not_before = signed.not_after.checked_sub(ACTIVITY_VALIDITY_MS)
                    .ok_or(KernelError::Journal)?.saturating_sub(1_000);
                let expected = encode_activity_version(observation, &relay.submitter.key, &ActivityOptions {
                    network_id: relay.network_id, actor_did: relay.submitter.did(),
                    account_sequence: signed.account_sequence, fee_limit: relay.submitter.fee_limit,
                    not_before, not_after: signed.not_after,
                }, layerx_wire::limits::STATE_COMMITMENT_PROTOCOL_VERSION)?;
                if signed.activity != expected || observation.len() < OBSERVATION_HEADER_BYTES {
                    return Err(KernelError::Journal);
                }
                let held = relay.exchange.answers();
                let held = held.get(&entry.key()).ok_or(KernelError::Journal)?;
                let ready = Ready {
                    request_id: entry.request.request_id, response: held.answer.response.clone(),
                    content_digest: held.answer.attestation.content_digest,
                    full_length: held.answer.attestation.full_length, callback_gas: 0,
                    digest: held.answer.digest,
                    signers: observation_signers(observation, &held.answer.digest)?.0,
                    signatures: observation_signers(observation, &held.answer.digest)?.1,
                };
                if ready.signers.iter().zip(&ready.signatures)
                    .any(|(signer, signature)| held.signatures.get(signer) != Some(signature))
                    || observation_bytes(relay.network_id, &entry.request, &ready)? != observation {
                    return Err(KernelError::Journal);
                }
                let terminal = relay.submitter.outcome(
                    entry.result.clone().map(RpcAnswer::Result), &signed.activity_id);
                if (entry.stage == Stage::Completed && !matches!(terminal, Outcome::Completed(_)))
                    || (entry.stage == Stage::Rejected && !matches!(terminal, Outcome::Rejected(_, _))) {
                    return Err(KernelError::Journal);
                }
            }
        }
        relay.journal = Some(path);
        relay.requeue();
        Ok(relay)
    }

    /// The requests waiting for their observation to commit, in sequence
    /// order.
    #[must_use]
    pub fn queued(&self) -> &[ProgramRequest] {
        &self.queue
    }

    /// Every journalled request with its stage, in sequence order.
    #[must_use]
    pub fn entries(&self) -> &[RelayEntry] {
        &self.entries
    }

    /// The journal file, or `None` for a relay kept in memory.
    #[must_use]
    pub fn journal_path(&self) -> Option<&Path> {
        self.journal.as_deref()
    }

    fn requeue(&mut self) {
        self.queue = self
            .entries
            .iter()
            .filter(|entry| !entry.stage.terminal())
            .map(|entry| entry.request.clone())
            .collect();
    }

    fn store(&self, next: u64) -> Result<(), KernelError> {
        let Some(path) = &self.journal else {
            return Ok(());
        };
        let body = json!({
            "version": RELAY_JOURNAL_VERSION,
            "next_sequence": next,
            "entries": self.entries.iter().map(RelayEntry::value).collect::<Vec<_>>(),
        })
        .to_string();
        let temporary = path.with_extension("tmp");
        std::fs::File::create(&temporary)
            .and_then(|mut file| {
                file.write_all(body.as_bytes())?;
                file.sync_all()
            })
            .and_then(|()| std::fs::rename(&temporary, path))
            .and_then(|()| match path.parent() {
                Some(parent) => std::fs::File::open(parent)?.sync_all(),
                None => Ok(()),
            })
            .map_err(|_| KernelError::Journal)
    }

    /// Polls the watcher once and journals every new request with the
    /// cursor before the cursor moves, then advances each queued request:
    /// answers it, collects the peers' signatures, signs and journals the
    /// observation activity once the threshold agrees and sends it, and
    /// reads a sent activity's receipt before sending it again.
    ///
    /// # Errors
    /// Returns the watcher's error and a journal that could not be written;
    /// nothing is sent past an unwritten journal.
    pub fn step(&mut self, now_ms: u64) -> Result<Vec<Step>, KernelError> {
        let (next, found) = self.watcher.fetch()?;
        for (topic, request) in found {
            let key = (request.program_id, request.request_id);
            if let Some(entry) = self.entries.iter().find(|entry| entry.key() == key) {
                if entry.request != request || entry.topic != topic {
                    return Err(KernelError::Journal);
                }
                continue;
            }
            self.entries.push(RelayEntry {
                request,
                topic,
                stage: Stage::AwaitingQuorum,
                observation: None,
                attestation: None,
                signed: None,
                reason: None,
                last_error: None,
                result: None,
            });
        }
        self.entries.sort_by_key(|entry| entry.request.sequence);
        self.store(next)?;
        if next != self.watcher.next_sequence() {
            self.watcher.resume_at(next)?;
        }
        let mut steps = Vec::new();
        for index in 0..self.entries.len() {
            if !self.entries[index].stage.terminal() {
                if let Some(step) = self.advance(index, next, now_ms)? {
                    steps.push(step);
                }
            }
        }
        self.store(next)?;
        self.requeue();
        Ok(steps)
    }

    fn advance(
        &mut self,
        index: usize,
        next: u64,
        now_ms: u64,
    ) -> Result<Option<Step>, KernelError> {
        let request = self.entries[index].request.clone();
        let key = self.entries[index].key();
        if self.entries[index].stage == Stage::AwaitingQuorum {
            if self.exchange.answer(key).is_none() {
                match self.attestor.attest(&request) {
                    Ok(answer) => {
                        let snapshot = held_value(&Held {
                            signatures: BTreeMap::from([(answer.signer, answer.signature)]),
                            answer: answer.clone(),
                        });
                        self.entries[index].attestation = Some(snapshot);
                        self.entries[index].last_error = None;
                        self.store(next)?;
                        self.exchange.record(answer);
                    },
                    Err(reason) if retryable(&reason) => {
                        self.entries[index].last_error = Some(reason.to_string());
                        self.store(next)?;
                        return Ok(None);
                    },
                    Err(reason) => {
                        let entry = &mut self.entries[index];
                        entry.stage = Stage::Refused;
                        entry.reason = Some(reason.to_string());
                        return Ok(Some(Step::Refused {
                            program_id: key.0,
                            request_id: key.1,
                            reason,
                        }));
                    }
                }
            }
            let _ = self.exchange.collect(key, &self.set);
            self.entries[index].attestation = self.exchange.snapshot(key);
            self.store(next)?;
            let Some(ready) = self.exchange.ready(key, &self.set) else {
                return Ok(None);
            };
            let Ok(observation) = observation_bytes(self.network_id, &request, &ready) else {
                let entry = &mut self.entries[index];
                entry.stage = Stage::Refused;
                entry.reason = Some("observation_encode".to_owned());
                return Ok(None);
            };
            let Ok(signed) = self.submitter.sign(&observation, now_ms) else {
                return Ok(None);
            };
            let entry = &mut self.entries[index];
            entry.observation = Some(observation);
            entry.signed = Some(signed.clone());
            entry.stage = Stage::Submitting;
            self.store(next)?;
            let answer = self.submitter.send(&signed.activity);
            let fresh = matches!(&answer, Some(RpcAnswer::Result(_)));
            return Ok(Some(self.settle(index, self.submitter.outcome(answer, &signed.activity_id), fresh)));
        }
        let Some(signed) = self.entries[index].signed.clone() else {
            return Err(KernelError::Journal);
        };
        let looked = self.submitter.outcome(self.submitter.lookup(&signed.activity_id), &signed.activity_id);
        if !matches!(looked, Outcome::Open) {
            return Ok(Some(self.settle(index, looked, false)));
        }
        if now_ms <= signed.not_after {
            let answer = self.submitter.send(&signed.activity);
            return Ok(Some(self.settle(index, self.submitter.outcome(answer, &signed.activity_id), false)));
        }
        Ok(Some(self.settle(index, Outcome::Open, false)))
    }

    fn settle(&mut self, index: usize, outcome: Outcome, fresh: bool) -> Step {
        let entry = &mut self.entries[index];
        let (program_id, request_id) = entry.key();
        let activity_id = entry
            .signed
            .as_ref()
            .map_or([0; 32], |signed| signed.activity_id);
        match outcome {
            Outcome::Completed(result) => {
                entry.stage = Stage::Completed;
                entry.result = Some(result.clone());
                self.exchange.forget((program_id, request_id));
                if fresh {
                    Step::Posted {
                        program_id,
                        request_id,
                        result,
                    }
                } else {
                    Step::Committed {
                        program_id,
                        request_id,
                        activity_id,
                        result,
                    }
                }
            }
            Outcome::Open => {
                entry.stage = Stage::Unknown;
                Step::Unknown {
                    program_id,
                    request_id,
                    activity_id,
                }
            }
            Outcome::Rejected(code, result) => {
                entry.stage = Stage::Rejected;
                entry.reason = Some(format!("committed_refusal {code}"));
                entry.result = Some(result);
                self.exchange.forget((program_id, request_id));
                Step::Rejected {
                    program_id,
                    request_id,
                    code,
                }
            }
        }
    }
}

fn parse_journal(bytes: &[u8]) -> Option<(u64, Vec<RelayEntry>)> {
    let value: Value = serde_json::from_slice(bytes).ok()?;
    let object = value.as_object()?;
    if object.len() != 3
        || object.get("version")?.as_str()? != RELAY_JOURNAL_VERSION
    {
        return None;
    }
    let next = object.get("next_sequence")?.as_u64()?;
    let entries = object
        .get("entries")?
        .as_array()?
        .iter()
        .map(RelayEntry::parse)
        .collect::<Option<Vec<_>>>()?;
    let ordered = entries.windows(2).all(|pair| {
        pair[0].request.sequence < pair[1].request.sequence
    });
    let distinct = entries
        .iter()
        .enumerate()
        .all(|(index, entry)| entries[..index].iter().all(|other| other.key() != entry.key()));
    let behind = entries.iter().all(|entry| entry.request.sequence < next);
    (ordered && distinct && behind).then_some((next, entries))
}
