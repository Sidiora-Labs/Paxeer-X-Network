//! Bounded background resolution of submissions left `Unknown`.

use std::collections::BTreeMap;
use std::sync::mpsc::{self, Receiver, RecvTimeoutError, Sender};
use std::thread::{self, JoinHandle};
use std::time::{Duration, Instant};

use layerx_proof::receipt::{AuthorizedBatch, VerifiedReceipt};
use layerx_types::result::ResultCode;

use crate::client::{Client, ReconnectPolicy};
use crate::receipt::{ReceiptError, Resolution};
use crate::submit::Unknown;

/// Bounds for the unknown-outcome resolver.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct ResolverPolicy {
    /// Maximum number of unknown submissions tracked at once.
    pub capacity: usize,
    /// How long one submission may stay unknown before it is given up.
    pub expiry: Duration,
    /// Backoff between resolution attempts for one submission.
    pub backoff: ReconnectPolicy,
}

/// The converged state of one tracked submission.
#[derive(Clone, Debug, Eq, PartialEq)]
pub enum Outcome {
    Settled(Box<VerifiedReceipt>),
    Refused {
        result: ResultCode,
    },
    Expired(Unknown),
    Failed {
        unknown: Unknown,
        error: ReceiptError,
    },
    Untracked(Unknown),
}

/// One converged submission, keyed by its idempotency key.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Resolved {
    pub idempotency_key: [u8; 32],
    pub outcome: Outcome,
}

/// Why a submission was not admitted to the resolver.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum TrackError {
    Full,
    Stopped,
}

#[derive(Debug)]
struct Pending {
    unknown: Unknown,
    authorised_batch: AuthorizedBatch,
    deadline: Instant,
    due: Instant,
    tries: u8,
}

/// Bounded set of unknown submissions with per-entry backoff and expiry.
#[derive(Debug)]
pub struct UnknownResolver {
    policy: ResolverPolicy,
    pending: BTreeMap<[u8; 32], Pending>,
}

impl UnknownResolver {
    #[must_use]
    pub fn new(policy: ResolverPolicy) -> Self {
        Self {
            policy,
            pending: BTreeMap::new(),
        }
    }

    #[must_use]
    pub fn len(&self) -> usize {
        self.pending.len()
    }

    #[must_use]
    pub fn is_empty(&self) -> bool {
        self.pending.is_empty()
    }

    /// Starts tracking one unknown submission; tracking the same idempotency
    /// key again keeps the original entry.
    ///
    /// # Errors
    ///
    /// Returns [`TrackError::Full`] when the bounded set is at capacity.
    pub fn track(
        &mut self,
        unknown: Unknown,
        authorised_batch: AuthorizedBatch,
        now: Instant,
    ) -> Result<(), TrackError> {
        let key = unknown.idempotency_key();
        if self.pending.contains_key(&key) {
            return Ok(());
        }
        if self.pending.len() >= self.policy.capacity {
            return Err(TrackError::Full);
        }
        let deadline = now.checked_add(self.policy.expiry).unwrap_or(now);
        self.pending.insert(
            key,
            Pending {
                unknown,
                authorised_batch,
                deadline,
                due: now,
                tries: 0,
            },
        );
        Ok(())
    }

    /// The earliest instant at which [`Self::poll`] has work to do.
    #[must_use]
    pub fn next_due(&self) -> Option<Instant> {
        self.pending
            .values()
            .map(|entry| entry.due.min(entry.deadline))
            .min()
    }

    /// Expires overdue entries and runs one resolution attempt for every due
    /// entry, returning the submissions that converged.
    pub fn poll<F>(&mut self, now: Instant, mut resolve: F) -> Vec<Resolved>
    where
        F: FnMut(&Unknown, AuthorizedBatch) -> Result<Resolution, ReceiptError>,
    {
        let backoff = self.policy.backoff;
        let mut converged = Vec::new();
        self.pending.retain(|key, entry| {
            if now >= entry.deadline {
                converged.push(Resolved {
                    idempotency_key: *key,
                    outcome: Outcome::Expired(entry.unknown.clone()),
                });
                return false;
            }
            if now < entry.due {
                return true;
            }
            let outcome = match resolve(&entry.unknown, entry.authorised_batch) {
                Ok(Resolution::Resolved(receipt)) => Outcome::Settled(receipt),
                Ok(Resolution::Refused { result }) => Outcome::Refused { result },
                Ok(Resolution::Unknown(unknown)) => {
                    entry.unknown = unknown;
                    entry.tries = entry.tries.saturating_add(1);
                    entry.due = next_attempt(now, backoff, entry.tries, entry.deadline);
                    return true;
                }
                Err(ReceiptError::Disconnected | ReceiptError::Transport(_)) => {
                    entry.tries = entry.tries.saturating_add(1);
                    entry.due = next_attempt(now, backoff, entry.tries, entry.deadline);
                    return true;
                }
                Err(error) => Outcome::Failed {
                    unknown: entry.unknown.clone(),
                    error,
                },
            };
            converged.push(Resolved {
                idempotency_key: *key,
                outcome,
            });
            false
        });
        converged
    }
}

fn next_attempt(now: Instant, backoff: ReconnectPolicy, tries: u8, deadline: Instant) -> Instant {
    now.checked_add(backoff.delay(tries, "unknown-resolver"))
        .map_or(deadline, |due| due.min(deadline))
}

/// Handle to a background resolver thread.
#[derive(Debug)]
pub struct ResolverHandle {
    intake: Sender<(Unknown, AuthorizedBatch)>,
    outcomes: Receiver<Resolved>,
    worker: JoinHandle<()>,
}

impl ResolverHandle {
    /// Hands one unknown submission to the background loop. A submission the
    /// bounded set cannot admit comes back as [`Outcome::Untracked`].
    ///
    /// # Errors
    ///
    /// Returns [`TrackError::Stopped`] when the loop is no longer running.
    pub fn track(
        &self,
        unknown: Unknown,
        authorised_batch: AuthorizedBatch,
    ) -> Result<(), TrackError> {
        self.intake
            .send((unknown, authorised_batch))
            .map_err(|_| TrackError::Stopped)
    }

    /// Converged submissions, in the order they converged.
    #[must_use]
    pub fn outcomes(&self) -> &Receiver<Resolved> {
        &self.outcomes
    }

    /// Stops the loop; entries still unknown are abandoned.
    pub fn shutdown(self) {
        drop(self.intake);
        let _ = self.worker.join();
    }
}

/// Spawns the background loop that retries `resolve` for every tracked
/// unknown submission until it settles, is refused, fails, or expires.
#[must_use]
pub fn spawn<F>(policy: ResolverPolicy, mut resolve: F) -> ResolverHandle
where
    F: FnMut(&Unknown, AuthorizedBatch) -> Result<Resolution, ReceiptError> + Send + 'static,
{
    let (intake, requests) = mpsc::channel::<(Unknown, AuthorizedBatch)>();
    let (report, outcomes) = mpsc::channel();
    let worker = thread::spawn(move || {
        let mut resolver = UnknownResolver::new(policy);
        loop {
            let request = match resolver.next_due() {
                None => requests.recv().map_err(|_| RecvTimeoutError::Disconnected),
                Some(due) => requests.recv_timeout(due.saturating_duration_since(Instant::now())),
            };
            match request {
                Ok((unknown, authorised_batch)) => {
                    if resolver
                        .track(unknown.clone(), authorised_batch, Instant::now())
                        .is_err()
                    {
                        let rejected = Resolved {
                            idempotency_key: unknown.idempotency_key(),
                            outcome: Outcome::Untracked(unknown),
                        };
                        if report.send(rejected).is_err() {
                            return;
                        }
                    }
                }
                Err(RecvTimeoutError::Timeout) => {}
                Err(RecvTimeoutError::Disconnected) => return,
            }
            for resolved in resolver.poll(Instant::now(), &mut resolve) {
                if report.send(resolved).is_err() {
                    return;
                }
            }
        }
    });
    ResolverHandle {
        intake,
        outcomes,
        worker,
    }
}

/// Runs the background loop over a connected [`Client`], one receipt lookup
/// per attempt, with correlation ids counting up from `first_correlation_id`.
#[must_use]
pub fn spawn_for_client(
    mut client: Client,
    policy: ResolverPolicy,
    first_correlation_id: u64,
) -> ResolverHandle {
    let mut correlation_id = first_correlation_id;
    let single_lookup = ReconnectPolicy {
        maximum_attempts: 1,
        ..policy.backoff
    };
    spawn(policy, move |unknown, authorised_batch| {
        let current = correlation_id;
        correlation_id = correlation_id.wrapping_add(1);
        client.resolve_unknown(unknown, current, authorised_batch, single_lookup)
    })
}

#[cfg(test)]
mod tests {
    use std::fs;
    use std::os::unix::net::{UnixListener, UnixStream};
    use std::path::PathBuf;
    use std::sync::atomic::{AtomicU64, Ordering};
    use std::thread::{self, JoinHandle};
    use std::time::{Duration, Instant};

    use ed25519_dalek::{Signer as _, SigningKey};
    use layerx_crypto::SignatureMessage;
    use layerx_proof::receipt::AuthorizedBatch;
    use layerx_types::payload::{ActivityType, ModuleId, ModuleRegistration, ModuleRegistry};
    use layerx_types::result::ResultCode;
    use layerx_wire::activity::decode_signed;
    use layerx_wire::encode::Encoder;
    use layerx_wire::hash::{activity_id, receipt_digest, Domain};
    use layerx_wire::limits::PROTOCOL_VERSION;

    use super::{spawn, Outcome, ResolverPolicy, TrackError, UnknownResolver};
    use crate::client::ReconnectPolicy;
    use crate::lni::framing::{read_frame, write_frame};
    use crate::lni::schema::{decode_envelope, encode_envelope, Envelope, Version};
    use crate::lni::transport::{ConnectionGate, Limits, Uds};
    use crate::receipt::{resolve_unknown, LookupContext};
    use crate::submit::{submit_signed, Submission, SubmissionContext, Unknown};

    static NEXT_SOCKET: AtomicU64 = AtomicU64::new(1);
    const IDEMPOTENCY_KEY: [u8; 32] = [0x81; 32];

    struct SocketPath(PathBuf);

    impl SocketPath {
        fn new(label: &str) -> Self {
            let sequence = NEXT_SOCKET.fetch_add(1, Ordering::Relaxed);
            Self(std::env::temp_dir().join(format!(
                "layerx-resolver-{label}-{}-{sequence}.sock",
                std::process::id()
            )))
        }
    }

    impl Drop for SocketPath {
        fn drop(&mut self) {
            let _ = fs::remove_file(&self.0);
        }
    }

    fn limits() -> Limits {
        Limits {
            maximum_frame_bytes: 1024 * 1024,
            maximum_connections: 1,
            maximum_streams: 4,
            maximum_queued_bytes: 2 * 1024 * 1024,
            deadline: Duration::from_secs(2),
        }
    }

    fn registry() -> ModuleRegistry {
        let activity = match ActivityType::new(ModuleId::Asset, 1) {
            Ok(activity) => activity,
            Err(error) => panic!("activity type rejected: {error:?}"),
        };
        let registration = match ModuleRegistration::new(ModuleId::Asset, &[activity]) {
            Ok(registration) => registration,
            Err(error) => panic!("registration rejected: {error:?}"),
        };
        match ModuleRegistry::new(&[registration]) {
            Ok(registry) => registry,
            Err(error) => panic!("registry rejected: {error:?}"),
        }
    }

    fn activity_fields(encoder: &mut Encoder, public_key: &[u8; 32]) {
        assert!(encoder.tag(1, 12).is_ok());
        assert!(encoder.u16(PROTOCOL_VERSION).is_ok());
        assert!(encoder.tag(2, 12).is_ok());
        assert!(encoder.u32(77).is_ok());
        assert!(encoder.tag(3, 12).is_ok());
        assert!(encoder.u32(0x0001_0001).is_ok());
        assert!(encoder.tag(4, 12).is_ok());
        assert!(encoder.bytes(b"did:layerx:resolver", 255).is_ok());
        assert!(encoder.tag(5, 12).is_ok());
        assert!(encoder.bytes(public_key, 524_288).is_ok());
        assert!(encoder.tag(6, 12).is_ok());
        assert!(encoder.u64(9).is_ok());
        assert!(encoder.tag(7, 12).is_ok());
        assert!(encoder.u64(10).is_ok());
        assert!(encoder.u64(100).is_ok());
        assert!(encoder.tag(8, 12).is_ok());
        assert!(encoder.bytes(&IDEMPOTENCY_KEY, 32).is_ok());
        assert!(encoder.tag(9, 12).is_ok());
        assert!(encoder.u128(1000).is_ok());
        assert!(encoder.tag(10, 12).is_ok());
        assert!(encoder.bytes(&[0x91; 32], 32).is_ok());
        assert!(encoder.tag(11, 12).is_ok());
        assert!(encoder.bytes(&[0x42, 0x43], 524_288).is_ok());
    }

    fn signed_activity() -> (Vec<u8>, [u8; 32], [u8; 32]) {
        let key = SigningKey::from_bytes(&[0x31; 32]);
        let public_key = key.verifying_key().to_bytes();
        let mut unsigned = Encoder::new(4096);
        assert!(unsigned
            .structure_header_version(0x1001, PROTOCOL_VERSION)
            .is_ok());
        assert!(unsigned.u8(11).is_ok());
        activity_fields(&mut unsigned, &public_key);
        let unsigned = unsigned.finish();
        let message =
            match SignatureMessage::new(Domain::SignaturePreimage, PROTOCOL_VERSION, 77, &unsigned)
            {
                Ok(message) => message,
                Err(error) => panic!("signature scope rejected: {error:?}"),
            };
        let signature = key.sign(&message.digest()).to_bytes();
        let mut signed = Encoder::new(4096);
        assert!(signed
            .structure_header_version(0x1001, PROTOCOL_VERSION)
            .is_ok());
        assert!(signed.u8(12).is_ok());
        activity_fields(&mut signed, &public_key);
        assert!(signed.tag(12, 12).is_ok());
        assert!(signed.bytes(&signature, 128).is_ok());
        let bytes = signed.finish();
        let decoded = match decode_signed(&bytes, &registry()) {
            Ok(decoded) => decoded,
            Err(error) => panic!("signed activity rejected: {error:?}"),
        };
        let identifier = match activity_id(&decoded) {
            Ok(identifier) => identifier,
            Err(error) => panic!("activity identifier failed: {error:?}"),
        };
        (bytes, public_key, identifier)
    }

    fn receipt(activity_identifier: [u8; 32]) -> (Vec<u8>, AuthorizedBatch) {
        let key = SigningKey::from_bytes(&[0x63; 32]);
        let encode = |signature: Option<[u8; 64]>| {
            let mut encoder = Encoder::new(4096);
            assert!(encoder
                .structure_header_version(0x5201, PROTOCOL_VERSION)
                .is_ok());
            assert!(encoder.u16(PROTOCOL_VERSION).is_ok());
            assert!(encoder.bytes(&activity_identifier, 32).is_ok());
            assert!(encoder.u64(9).is_ok());
            assert!(encoder.bytes(&[2; 32], 32).is_ok());
            assert!(encoder.bytes(&[3; 32], 32).is_ok());
            assert!(encoder.bytes(&[8; 32], 32).is_ok());
            assert!(encoder.i32(0).is_ok());
            assert!(encoder.sequence_length(0, 512).is_ok());
            assert!(encoder.u128(1).is_ok());
            assert!(encoder.bytes(&[4; 32], 32).is_ok());
            assert!(encoder.u16(1).is_ok());
            assert!(encoder.u32(1).is_ok());
            assert!(encoder.u32(1).is_ok());
            assert!(encoder.u8(1).is_ok());
            assert!(encoder.bytes(&[5; 32], 32).is_ok());
            assert!(encoder.u128(25).is_ok());
            assert!(encoder.bytes(&[6; 32], 32).is_ok());
            assert!(encoder.u128(100).is_ok());
            assert!(encoder.u128(75).is_ok());
            assert!(encoder.u64(1).is_ok());
            assert!(encoder.bytes(&[7; 32], 32).is_ok());
            assert!(encoder.u128(10).is_ok());
            assert!(encoder.u128(35).is_ok());
            assert!(encoder.bytes(&[9; 32], 32).is_ok());
            assert!(encoder.bytes(&[10; 32], 32).is_ok());
            assert!(encoder.bytes(&[11; 32], 32).is_ok());
            assert!(encoder.u64(1_000).is_ok());
            assert!(encoder.u8(u8::from(signature.is_some())).is_ok());
            if let Some(value) = signature {
                assert!(encoder.bytes(&value, 64).is_ok());
            }
            encoder.finish()
        };
        let unsigned = encode(None);
        let digest = match receipt_digest(&unsigned) {
            Ok(digest) => digest,
            Err(error) => panic!("receipt digest failed: {error:?}"),
        };
        let signed = encode(Some(key.sign(&digest).to_bytes()));
        let authorised = AuthorizedBatch::new(
            [4; 32],
            [5; 32],
            [2; 32],
            [3; 32],
            key.verifying_key().to_bytes(),
        );
        (signed, authorised)
    }

    struct ReceivedEnvelope {
        correlation_id: u64,
        canonical_payload: Vec<u8>,
    }

    fn receive_envelope(stream: &mut UnixStream, expected_tag: u16) -> ReceivedEnvelope {
        let frame = match read_frame(stream, 1024 * 1024) {
            Ok(frame) => frame,
            Err(error) => panic!("request frame failed: {error:?}"),
        };
        let decoded = match decode_envelope(&frame) {
            Ok(decoded) => decoded,
            Err(error) => panic!("request envelope failed: {error:?}"),
        };
        assert_eq!(decoded.message_tag, expected_tag);
        ReceivedEnvelope {
            correlation_id: decoded.correlation_id,
            canonical_payload: decoded.canonical_payload.to_vec(),
        }
    }

    fn send_receipt(stream: &mut UnixStream, correlation_id: u64, receipt: &[u8]) {
        send_receipt_version(stream, Version::V1_0, correlation_id, receipt);
    }

    fn send_receipt_version(
        stream: &mut UnixStream,
        version: Version,
        correlation_id: u64,
        receipt: &[u8],
    ) {
        let response = match encode_envelope(Envelope {
            version,
            message_tag: 6,
            correlation_id,
            canonical_payload: receipt,
            proof_material: &[],
        }) {
            Ok(response) => response,
            Err(error) => panic!("receipt response encoding failed: {error:?}"),
        };
        if let Err(error) = write_frame(stream, &response, 1024 * 1024) {
            panic!("receipt response failed: {error:?}");
        }
    }

    fn send_refusal(stream: &mut UnixStream, correlation_id: u64, raw: i32) {
        let mut payload = vec![4_u8];
        payload.extend_from_slice(&raw.to_be_bytes());
        let response = match encode_envelope(Envelope {
            version: Version::V1_0,
            message_tag: 25,
            correlation_id,
            canonical_payload: &payload,
            proof_material: &[],
        }) {
            Ok(response) => response,
            Err(error) => panic!("refusal encoding failed: {error:?}"),
        };
        if let Err(error) = write_frame(stream, &response, 1024 * 1024) {
            panic!("refusal response failed: {error:?}");
        }
    }

    enum Reply {
        Absent,
        Receipt(Vec<u8>),
        Refusal(i32),
    }

    fn spawn_node(socket: &SocketPath, replies: Vec<Reply>) -> JoinHandle<usize> {
        let listener = match UnixListener::bind(&socket.0) {
            Ok(listener) => listener,
            Err(error) => panic!("listener failed: {error}"),
        };
        thread::spawn(move || {
            let (mut submission, _) = match listener.accept() {
                Ok(connection) => connection,
                Err(error) => panic!("submission accept failed: {error}"),
            };
            receive_envelope(&mut submission, 3);
            drop(submission);
            let (mut resolution, _) = match listener.accept() {
                Ok(connection) => connection,
                Err(error) => panic!("resolution accept failed: {error}"),
            };
            let mut served = 0;
            for reply in replies {
                let request = receive_envelope(&mut resolution, 5);
                match reply {
                    Reply::Absent => send_receipt(&mut resolution, request.correlation_id, &[]),
                    Reply::Receipt(bytes) => {
                        send_receipt(&mut resolution, request.correlation_id, &bytes);
                    }
                    Reply::Refusal(raw) => {
                        send_refusal(&mut resolution, request.correlation_id, raw)
                    }
                }
                served += 1;
            }
            served
        })
    }

    fn connect(socket: &SocketPath) -> Uds {
        let gate = ConnectionGate::new(1);
        match Uds::connect(&socket.0, &gate, limits()) {
            Ok(transport) => transport,
            Err(error) => panic!("connection failed: {error:?}"),
        }
    }

    fn unknown(socket: &SocketPath, signed: &[u8], public_key: [u8; 32]) -> Unknown {
        let mut transport = connect(socket);
        let context = SubmissionContext {
            interface_version: Version::V1_0,
            protocol_version: PROTOCOL_VERSION,
            network_id: 77,
            correlation_id: 1,
            signer_public_key: public_key,
            attempt: 1,
        };
        let outcome = match submit_signed(&mut transport, &registry(), context, signed) {
            Ok(outcome) => outcome,
            Err(error) => panic!("submission failed before transmission: {error:?}"),
        };
        let Submission::Unknown(unknown) = outcome else {
            panic!("lost submission response was not unknown");
        };
        unknown
    }

    fn policy(capacity: usize, expiry: Duration) -> ResolverPolicy {
        ResolverPolicy {
            capacity,
            expiry,
            backoff: ReconnectPolicy {
                maximum_attempts: 1,
                base_delay: Duration::from_millis(1),
                maximum_delay: Duration::from_millis(4),
                jitter_percent: 10,
            },
        }
    }

    fn lookup_over(
        mut transport: Uds,
    ) -> impl FnMut(
        &Unknown,
        AuthorizedBatch,
    ) -> Result<crate::receipt::Resolution, crate::receipt::ReceiptError>
           + Send
           + 'static {
        let mut correlation_id = 10_u64;
        move |unknown, authorised_batch| {
            correlation_id += 1;
            resolve_unknown(
                &mut transport,
                unknown,
                LookupContext {
                    interface_version: Version::V1_0,
                    correlation_id,
                    authorised_batch,
                },
                ReconnectPolicy {
                    maximum_attempts: 1,
                    base_delay: Duration::from_millis(1),
                    maximum_delay: Duration::from_millis(1),
                    jitter_percent: 0,
                },
            )
        }
    }

    #[test]
    fn background_loop_resolves_unknown_to_settled_after_absence() {
        let socket = SocketPath::new("settled");
        let (signed, public_key, identifier) = signed_activity();
        let (receipt_bytes, authorised) = receipt(identifier);
        let node = spawn_node(
            &socket,
            vec![
                Reply::Absent,
                Reply::Absent,
                Reply::Receipt(receipt_bytes.clone()),
            ],
        );
        let unknown = unknown(&socket, &signed, public_key);
        let handle = spawn(
            policy(4, Duration::from_secs(10)),
            lookup_over(connect(&socket)),
        );
        assert_eq!(handle.track(unknown.clone(), authorised), Ok(()));
        let resolved = match handle.outcomes().recv_timeout(Duration::from_secs(5)) {
            Ok(resolved) => resolved,
            Err(error) => panic!("unknown never converged: {error:?}"),
        };
        assert_eq!(resolved.idempotency_key, unknown.idempotency_key());
        let Outcome::Settled(verified) = resolved.outcome else {
            panic!("unknown converged to {:?}, not settled", resolved.outcome);
        };
        assert_eq!(verified.canonical_bytes(), receipt_bytes);
        handle.shutdown();
        assert!(matches!(node.join(), Ok(3)));
    }

    #[test]
    fn background_loop_resolves_unknown_to_refused() {
        let socket = SocketPath::new("refused");
        let (signed, public_key, identifier) = signed_activity();
        let (_, authorised) = receipt(identifier);
        let node = spawn_node(&socket, vec![Reply::Absent, Reply::Refusal(7)]);
        let unknown = unknown(&socket, &signed, public_key);
        let handle = spawn(
            policy(4, Duration::from_secs(10)),
            lookup_over(connect(&socket)),
        );
        assert_eq!(handle.track(unknown.clone(), authorised), Ok(()));
        let resolved = match handle.outcomes().recv_timeout(Duration::from_secs(5)) {
            Ok(resolved) => resolved,
            Err(error) => panic!("unknown never converged: {error:?}"),
        };
        assert_eq!(resolved.idempotency_key, unknown.idempotency_key());
        assert_eq!(
            resolved.outcome,
            Outcome::Refused {
                result: ResultCode::from_raw(7)
            }
        );
        handle.shutdown();
        assert!(matches!(node.join(), Ok(2)));
    }

    #[test]
    fn resolver_expires_unknown_that_never_converges_and_stays_bounded() {
        let socket = SocketPath::new("expired");
        let (signed, public_key, identifier) = signed_activity();
        let (_, authorised) = receipt(identifier);
        let node = spawn_node(&socket, vec![Reply::Absent]);
        let unknown = unknown(&socket, &signed, public_key);
        let expiry = Duration::from_secs(30);
        let mut resolver = UnknownResolver::new(policy(1, expiry));
        let start = Instant::now();
        assert_eq!(resolver.track(unknown.clone(), authorised, start), Ok(()));
        assert_eq!(resolver.track(unknown.clone(), authorised, start), Ok(()));
        assert_eq!(resolver.len(), 1);
        let mut lookup = lookup_over(connect(&socket));
        assert!(resolver.poll(start, &mut lookup).is_empty());
        assert_eq!(resolver.len(), 1);
        let Some(due) = resolver.next_due() else {
            panic!("pending unknown has no next attempt");
        };
        assert!(due > start && due <= start + expiry);
        let resolved = resolver.poll(start + expiry, &mut lookup);
        assert!(resolver.is_empty());
        assert_eq!(resolved.len(), 1);
        assert_eq!(resolved[0].idempotency_key, unknown.idempotency_key());
        let Outcome::Expired(expired) = &resolved[0].outcome else {
            panic!(
                "unknown converged to {:?}, not expired",
                resolved[0].outcome
            );
        };
        assert_eq!(expired.resolution_attempts(), 1);
        assert_eq!(expired.retry_bytes(), signed);
        assert!(matches!(node.join(), Ok(1)));
        let mut full = UnknownResolver::new(policy(0, expiry));
        assert_eq!(
            full.track(unknown, authorised, start),
            Err(TrackError::Full)
        );
        assert!(full.is_empty());
    }
}
