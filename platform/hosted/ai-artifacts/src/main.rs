//! Durable private artifact service (offchain). Bounded HTTP/1.1 routes for
//! PrepareObject, upload frames, FinalizeObject, ResolveArtifact, content access,
//! grants, RevokeAccess, RotateAccessEnvelope, TombstoneArtifact, publisher
//! generation revocation, locator admission, pinned TLS locator import and
//! delivery reconciliation, served over TLS unless the config explicitly names
//! a plaintext listener. Key releases are durable intents before any byte is
//! sent; tombstones, holds, finality-observed retention timers, bindings and
//! reinstatement go through the retention ledger, and payloads are purged only
//! at its deletion deadline; a restore below the live revocation or retention
//! high-water mark never becomes ready.
//! Nothing here is a protocol receipt.
mod authority;
mod crypto;
mod resolver;
mod retention;
mod store;

use authority::{Config, Listener, Tenant};
use crypto::{FrameContext, KeyProvider, LocalFileKeyProvider};
use layerx_programs_ai_market::evidence::{
    chunk_count, declaration_root, decode_declaration, decode_envelope, decode_manifest,
    expected_chunk_length, verify_publisher, ArtifactContext, ArtifactError, ArtifactKind, Privacy,
    VerificationFailure, CHUNK_BYTES, MAX_ENVELOPE_BYTES, MAX_MANIFEST_BYTES, MAX_OBJECT_BYTES,
    MAX_RECORD_BYTES,
};
use layerx_programs_ai_market::queries::{
    bind_snapshot, CaptureFacts, FinalityEvidence, QueryError, ReadProof, SnapshotBinding,
};
use layerx_programs_ai_market::state::decode_shared_state;
use layerx_programs_ai_market::types::{
    ChainDomain, Digest32, MarketId, PolicyDigest, Presence, ProgramId,
};
use layerx_programs_ai_market::{codec, MAX_CHUNK_BYTES, MAX_STATE_BYTES};
use retention::{Ledger, PurgeReport, Refusal};
use rustls::pki_types::pem::PemObject;
use rustls::pki_types::{CertificateDer, PrivateKeyDer};
use rustls::{ServerConfig, ServerConnection, StreamOwned};
use serde::Deserialize;
use serde_json::{json, Value};
use sha2::{Digest, Sha256};
use std::io::{BufRead, BufReader, Read, Write};
use std::net::{TcpListener, TcpStream};
use std::path::{Path, PathBuf};
use std::sync::{Arc, Mutex, MutexGuard};
use std::time::{Duration, SystemTime, UNIX_EPOCH};
use store::{
    Delivery, DeliveryState, Disposition, Grant, ObjectRecord, ObjectState, Session, SessionState,
    Store, Transition, STAGING_SECS,
};

const MAX_API_REQUEST: usize = 65_536;
/// A finality observation carries the whole hex-encoded market state.
const MAX_OBSERVATION_REQUEST: usize = 2 * MAX_STATE_BYTES + 4_096;
const MAX_HEAD: usize = 8_192;
const MAX_LEASE_SECS: u64 = 365 * 86_400;
const DELIVERY_DEADLINE: Duration = Duration::from_secs(10);
const MAINTENANCE_INTERVAL: Duration = Duration::from_secs(60);
const WRAPPED_KEY_BYTES: usize = 24 + 32 + 16;
const IO_TIMEOUT: Duration = Duration::from_secs(30);
const JSON: &str = "application/json";

type Reply = (u16, Vec<u8>, &'static str);
type Op = Result<Value, ArtifactError>;

struct Service {
    config: Config,
    kms: Box<dyn KeyProvider>,
    // ponytail: one global lock serializes every mutation (and makes publication
    // compare-and-set trivially atomic); shard per tenant/root if throughput matters.
    store: Mutex<Store>,
    /// Always locked after `store`, never alone before it.
    ledger: Mutex<Ledger>,
}

struct Request {
    method: String,
    path: String,
    token: Option<String>,
    body: Vec<u8>,
}

fn now() -> u64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map_or(0, |d| d.as_secs())
}

fn sha(parts: &[&[u8]]) -> [u8; 32] {
    let mut h = Sha256::new();
    for part in parts {
        h.update(part);
    }
    h.finalize().into()
}

fn unhex<const N: usize>(text: &str) -> Result<[u8; N], ArtifactError> {
    let bytes = unhex_vec(text, N)?;
    bytes.try_into().map_err(|_| ArtifactError::Malformed)
}

fn unhex_vec(text: &str, max: usize) -> Result<Vec<u8>, ArtifactError> {
    if text.len() > max * 2 {
        return Err(ArtifactError::Malformed);
    }
    hex::decode(text).map_err(|_| ArtifactError::Malformed)
}

fn request_id(text: &str) -> Result<String, ArtifactError> {
    let id = unhex::<32>(text)?;
    if id == [0; 32] {
        return Err(ArtifactError::Malformed);
    }
    Ok(hex::encode(id))
}

fn parse<'a, T: Deserialize<'a>>(body: &'a [u8]) -> Result<T, ArtifactError> {
    serde_json::from_slice(body).map_err(|_| ArtifactError::Malformed)
}

fn http_status(error: ArtifactError) -> u16 {
    use ArtifactError::*;
    match error {
        Unauthorized => 401,
        AuthorityRevoked | PurposeDenied | Expired => 403,
        ContentUnavailable => 404,
        IdempotencyConflict | IntegrityConflict => 409,
        Tombstoned => 410,
        UnsafeLocator => 422,
        QuotaExceeded | CapacityUnavailable => 507,
        StorageFailure => 500,
        UnknownDelivery => 504,
        _ => 400,
    }
}

fn access_status(error: ArtifactError) -> &'static str {
    use ArtifactError::*;
    match error {
        Expired => "EXPIRED",
        Tombstoned => "TOMBSTONED",
        IntegrityConflict => "INTEGRITY_FAILED",
        ContentUnavailable => "CONTENT_UNAVAILABLE",
        UnknownDelivery => "UNKNOWN_DELIVERY",
        _ => "ACCESS_DENIED",
    }
}

fn error_body(error: ArtifactError, extra: Value) -> Value {
    let mut body = json!({ "error": error.name(), "code": error.code() });
    if let (Value::Object(map), Value::Object(more)) = (&mut body, extra) {
        map.extend(more);
    }
    body
}

fn reply<E: Into<Refusal>>(result: Result<Value, E>) -> (u16, Value) {
    match result.map_err(Into::into) {
        Ok(value) => (200, value),
        Err(Refusal::Artifact(error)) => (http_status(error), error_body(error, json!({}))),
        Err(Refusal::RestoreNotReady) => (503, json!({ "error": Refusal::RestoreNotReady.name() })),
        Err(refusal @ (Refusal::RetentionActive { until } | Refusal::HoldActive { until })) => {
            (409, json!({ "error": refusal.name(), "until": until }))
        }
        Err(refusal) => (409, json!({ "error": refusal.name() })),
    }
}

fn query_refusal(error: QueryError) -> Refusal {
    match error {
        QueryError::Application(e) => Refusal::Artifact(e.into()),
        QueryError::FinalityUnavailable => Refusal::FinalityUnavailable,
        QueryError::BindingMismatch
        | QueryError::SnapshotConflict
        | QueryError::IntegrityFailure => ArtifactError::IntegrityConflict.into(),
        _ => ArtifactError::Malformed.into(),
    }
}

fn digest(text: &str) -> Result<Digest32, ArtifactError> {
    Ok(Digest32::new(unhex::<32>(text)?)?)
}

fn alert(report: &PurgeReport) {
    for root in &report.pending {
        eprintln!("purge alert: root {root} stays TOMBSTONED_PENDING_PURGE");
    }
}

fn context_of(bytes: &[u8; 128]) -> Result<ArtifactContext, ArtifactError> {
    let part = |i: usize| -> [u8; 32] {
        let mut out = [0; 32];
        out.copy_from_slice(&bytes[i * 32..i * 32 + 32]);
        out
    };
    Ok(ArtifactContext {
        chain: ChainDomain::new(part(0))?,
        program: ProgramId::new(part(1))?,
        market: MarketId::new(part(2))?,
        policy: PolicyDigest::new(part(3))?,
    })
}

fn key_aad(root: &str, generation: u64) -> Vec<u8> {
    [
        b"object\0".as_slice(),
        root.as_bytes(),
        &generation.to_be_bytes(),
    ]
    .concat()
}

fn session_key_aad(session: &str) -> Vec<u8> {
    [b"session\0".as_slice(), session.as_bytes()].concat()
}

fn staging_open(session: &Session) -> Result<(), ArtifactError> {
    match session.state {
        SessionState::Staging if now() < session.created_at + STAGING_SECS => Ok(()),
        SessionState::Quarantined => Err(ArtifactError::IntegrityConflict),
        _ => Err(ArtifactError::Expired),
    }
}

/// Reads a bounded header block (request or response) as lines.
fn read_head(reader: &mut impl BufRead) -> Result<Vec<String>, ArtifactError> {
    let mut lines = Vec::new();
    let mut consumed = 0usize;
    loop {
        let mut line = String::new();
        let n = reader
            .take((MAX_HEAD - consumed) as u64)
            .read_line(&mut line)
            .map_err(|_| ArtifactError::Malformed)?;
        consumed += n;
        if n == 0 || !line.ends_with('\n') {
            return Err(ArtifactError::Malformed);
        }
        let line = line.trim_end_matches(['\r', '\n']).to_string();
        if line.is_empty() {
            return Ok(lines);
        }
        lines.push(line);
    }
}

fn header<'a>(lines: &'a [String], name: &str) -> Option<&'a str> {
    lines.iter().skip(1).find_map(|line| {
        let (key, value) = line.split_once(':')?;
        key.trim().eq_ignore_ascii_case(name).then(|| value.trim())
    })
}

#[derive(Deserialize)]
struct PrepareReq {
    request_id: String,
    kind: u8,
    privacy: u8,
    context: String,
    byte_length: u64,
    expected_content_root: Option<String>,
}

#[derive(Deserialize)]
struct FinalizeReq {
    request_id: String,
    session: String,
    envelope: String,
    declaration: Option<String>,
}

#[derive(Deserialize)]
struct AccessReq {
    request_id: Option<String>,
    root: String,
    task: Option<String>,
    purpose: String,
    grant_id: Option<String>,
    index: Option<u32>,
}

#[derive(Deserialize)]
struct GrantReq {
    request_id: String,
    root: String,
    task: String,
    grantee: String,
    purposes: u16,
    ttl_secs: u64,
}

#[derive(Deserialize)]
struct RevokeReq {
    request_id: String,
    grant_id: String,
    generation: u64,
    sequence: u64,
}

#[derive(Deserialize)]
struct RootReq {
    request_id: String,
    root: String,
    reason: Option<u8>,
}

#[derive(Deserialize)]
struct HoldReq {
    request_id: String,
    root: String,
    hold: String,
    until: Option<u64>,
}

#[derive(Deserialize)]
struct ReinstateReq {
    request_id: String,
    root: String,
    grant_id: String,
    decision: String,
}

/// One market state read and the finality evidence for its native root; the
/// chain and program come from the artifact's own manifest context.
#[derive(Deserialize)]
struct ObserveReq {
    request_id: String,
    root: String,
    state: String,
    native_state_root: String,
    observed_sequence: u64,
    execution_height: u64,
    batch_id: String,
    checkpoint: String,
    settlement: Option<String>,
    rank: u8,
    publication_time_ms: u64,
}

#[derive(Deserialize)]
struct ProjectionReq {
    root: String,
}

#[derive(Deserialize)]
struct PublisherRevokeReq {
    request_id: String,
    principal: String,
    generation: u64,
}

#[derive(Deserialize)]
struct DeliveryReq {
    request_id: String,
}

#[derive(Deserialize)]
struct LocatorReq {
    uri: String,
}

#[derive(Deserialize)]
struct ImportReq {
    session: String,
    uri: String,
}

impl Service {
    fn lock(&self) -> MutexGuard<'_, Store> {
        self.store.lock().unwrap_or_else(|p| p.into_inner())
    }

    fn ledger(&self) -> MutexGuard<'_, Ledger> {
        self.ledger.lock().unwrap_or_else(|p| p.into_inner())
    }

    /// Records exactly one durable disposition per tenant request identity before
    /// acknowledging; a reused identity with different bytes is IdempotencyConflict.
    fn idempotent<E: Into<Refusal>>(
        &self,
        tenant: &Tenant,
        route: &str,
        body: &[u8],
        id: &str,
        op: impl FnOnce(&mut Store) -> Result<Value, E>,
    ) -> (u16, Value) {
        let mut store = self.lock();
        let key = format!("{}:{id}", tenant.id);
        let digest = hex::encode(sha(&[route.as_bytes(), &[0], body]));
        if let Some(prior) = store.state.requests.get(&key) {
            if prior.digest == digest {
                let value = serde_json::from_str(&prior.body).unwrap_or(Value::Null);
                return (prior.status, value);
            }
            return reply(Err(ArtifactError::IdempotencyConflict));
        }
        let (status, value) = reply(op(&mut store));
        store.state.requests.insert(
            key,
            Disposition {
                digest,
                status,
                body: value.to_string(),
            },
        );
        if store.persist().is_err() {
            let _ = store.reload();
            return reply(Err(ArtifactError::StorageFailure));
        }
        (status, value)
    }

    /// Every route but a key release answers without a delivery intent.
    fn route(&self, req: &Request) -> (Reply, Option<String>) {
        if req.method != "POST" || req.path != "/v1/resolve" {
            return (self.serve(req), None);
        }
        match self.config.authenticate(req.token.as_deref()) {
            Ok(tenant) => {
                let (result, delivery) = self.resolve(tenant, &req.body);
                (json_reply(result), delivery)
            }
            Err(e) => (json_reply(reply(Err(e))), None),
        }
    }

    fn serve(&self, req: &Request) -> Reply {
        let tenant = match self.config.authenticate(req.token.as_deref()) {
            Ok(t) => t,
            Err(e) => return json_reply(reply(Err(e))),
        };
        let path = req.path.as_str();
        if let ("PUT", Some(rest)) = (req.method.as_str(), path.strip_prefix("/v1/upload/")) {
            return json_reply(reply(self.upload(tenant, rest, &req.body)));
        }
        if req.method == "GET" && path == "/v1/usage" {
            return json_reply((200, self.usage(tenant)));
        }
        if req.method != "POST" {
            return json_reply(reply(Err(ArtifactError::Malformed)));
        }
        let body = &req.body;
        let result = match path {
            "/v1/prepare" => {
                let prepared = self.with_id::<PrepareReq, _>(
                    tenant,
                    path,
                    body,
                    |r| &r.request_id,
                    |s, r| self.prepare(s, tenant, r),
                );
                self.attach_object_key(prepared)
            }
            "/v1/import" => reply(self.import(tenant, body)),
            "/v1/finalize" => self.with_id::<FinalizeReq, _>(
                tenant,
                path,
                body,
                |r| &r.request_id,
                |s, r| self.finalize(s, tenant, r),
            ),
            "/v1/grants" => self.with_id::<GrantReq, _>(
                tenant,
                path,
                body,
                |r| &r.request_id,
                |s, r| self.grant(s, tenant, r),
            ),
            "/v1/revoke" => self.with_id::<RevokeReq, _>(
                tenant,
                path,
                body,
                |r| &r.request_id,
                |s, r| self.revoke(s, tenant, r),
            ),
            "/v1/rotate" => self.with_id::<RootReq, _>(
                tenant,
                path,
                body,
                |r| &r.request_id,
                |s, r| self.rotate(s, tenant, r),
            ),
            "/v1/tombstone" => self.with_id::<RootReq, _>(
                tenant,
                path,
                body,
                |r| &r.request_id,
                |s, r| self.tombstone(s, tenant, &r),
            ),
            "/v1/publishers/revoke" => self.with_id::<PublisherRevokeReq, _>(
                tenant,
                path,
                body,
                |r| &r.request_id,
                |s, r| self.revoke_publisher(s, tenant, r),
            ),
            "/v1/retention/hold" => self.with_id::<HoldReq, _>(
                tenant,
                path,
                body,
                |r| &r.request_id,
                |s, r| self.place_hold(s, tenant, &r),
            ),
            "/v1/retention/release" => self.with_id::<HoldReq, _>(
                tenant,
                path,
                body,
                |r| &r.request_id,
                |s, r| self.release_hold(s, tenant, &r),
            ),
            "/v1/retention/reinstate" => self.with_id::<ReinstateReq, _>(
                tenant,
                path,
                body,
                |r| &r.request_id,
                |s, r| self.reinstate(s, tenant, &r),
            ),
            "/v1/retention/binding-intent" => self.with_id::<RootReq, _>(
                tenant,
                path,
                body,
                |r| &r.request_id,
                |s, r| self.binding_intent(s, tenant, &r),
            ),
            "/v1/retention/orphan" => self.with_id::<RootReq, _>(
                tenant,
                path,
                body,
                |r| &r.request_id,
                |s, r| self.orphan_binding(s, tenant, &r),
            ),
            "/v1/retention/terminal" => self.with_id::<ObserveReq, _>(
                tenant,
                path,
                body,
                |r| &r.request_id,
                |s, r| self.observe_terminal(s, tenant, &r),
            ),
            "/v1/retention/binding" => self.with_id::<ObserveReq, _>(
                tenant,
                path,
                body,
                |r| &r.request_id,
                |s, r| self.observe_binding(s, tenant, &r),
            ),
            "/v1/retention/projection" => reply(self.projection(body)),
            "/v1/content" => return self.content(tenant, body),
            "/v1/deliveries" => self.delivery_status(tenant, body),
            "/v1/locators/check" => reply(parse::<LocatorReq>(body).and_then(|r| {
                let admitted =
                    resolver::admit(&r.uri, &tenant.locator_hosts, resolver::system_dns)?;
                Ok(json!({
                    "host": admitted.locator.host,
                    "addresses": admitted.addrs.iter().map(|a| a.to_string()).collect::<Vec<_>>(),
                }))
            })),
            _ => reply(Err(ArtifactError::Malformed)),
        };
        json_reply(result)
    }

    fn with_id<T: for<'a> Deserialize<'a>, E: Into<Refusal>>(
        &self,
        tenant: &Tenant,
        route: &str,
        body: &[u8],
        id: impl Fn(&T) -> &String,
        op: impl FnOnce(&mut Store, T) -> Result<Value, E>,
    ) -> (u16, Value) {
        let parsed: T = match parse(body) {
            Ok(v) => v,
            Err(e) => return reply(Err(e)),
        };
        let rid = match request_id(id(&parsed)) {
            Ok(v) => v,
            Err(e) => return reply(Err(e)),
        };
        self.idempotent(tenant, route, body, &rid, |store| op(store, parsed))
    }

    fn prepare(&self, store: &mut Store, tenant: &Tenant, r: PrepareReq) -> Op {
        let kind = ArtifactKind::decode(r.kind)?;
        let privacy = Privacy::decode(r.privacy)?;
        let context = unhex::<128>(&r.context)?;
        context_of(&context)?;
        if r.byte_length > MAX_OBJECT_BYTES || (privacy == Privacy::Encrypted && r.byte_length == 0)
        {
            return Err(ArtifactError::Malformed);
        }
        let chunk_count = chunk_count(r.byte_length)?;
        let expected_root = r
            .expected_content_root
            .as_deref()
            .map(|t| unhex::<32>(t).map(hex::encode))
            .transpose()?;
        let rid = request_id(&r.request_id)?;
        let session_id = hex::encode(sha(&[
            b"PAXAI/artifact-session/v1\0",
            tenant.id.as_bytes(),
            &[0],
            rid.as_bytes(),
        ]));
        let at = now();
        store.expire_staging(at);
        let used = store.state.quota_used.get(&tenant.id).copied().unwrap_or(0);
        if used.saturating_add(r.byte_length) > tenant.quota_bytes {
            return Err(ArtifactError::QuotaExceeded);
        }
        let total: u64 = store.state.quota_used.values().sum();
        if total.saturating_add(r.byte_length) > self.config.capacity_bytes {
            return Err(ArtifactError::CapacityUnavailable);
        }
        let wrapped_key = if privacy == Privacy::Encrypted {
            let key = crypto::new_object_key()?;
            Some(hex::encode(
                self.kms.wrap(&session_key_aad(&session_id), &key)?,
            ))
        } else {
            None
        };
        store.create_session_dir(&session_id)?;
        store.state.sessions.insert(
            session_id.clone(),
            Session {
                tenant: tenant.id.clone(),
                request_id: rid,
                kind: kind as u8,
                privacy: privacy as u8,
                context: hex::encode(context),
                byte_length: r.byte_length,
                chunk_count,
                expected_root,
                created_at: at,
                state: SessionState::Staging,
                root: None,
                wrapped_key,
            },
        );
        store
            .state
            .quota_used
            .insert(tenant.id.clone(), used + r.byte_length);
        store.event(&format!("prepare session={session_id}"))?;
        Ok(json!({
            "session": session_id,
            "state": "STAGING",
            "chunk_count": chunk_count,
            "chunk_bytes": CHUNK_BYTES,
            "expires_at": at + STAGING_SECS,
        }))
    }

    fn upload(&self, tenant: &Tenant, rest: &str, body: &[u8]) -> Op {
        let (session_id, index) = rest.split_once('/').ok_or(ArtifactError::Malformed)?;
        let session_id = hex::encode(unhex::<32>(session_id)?);
        let index: u32 = index.parse().map_err(|_| ArtifactError::Malformed)?;
        let store = self.lock();
        let session = store
            .state
            .sessions
            .get(&session_id)
            .filter(|s| s.tenant == tenant.id)
            .ok_or(ArtifactError::ContentUnavailable)?;
        staging_open(session)?;
        let expected = expected_chunk_length(session.byte_length, session.chunk_count, index)?;
        if body.len() != expected as usize {
            return Err(ArtifactError::LengthMismatch);
        }
        store.write_chunk(&session_id, index, body)?;
        Ok(json!({ "session": session_id, "index": index, "stored": true }))
    }

    fn receipt(root: &str, record: &ObjectRecord) -> Value {
        json!({
            "root": root,
            "bytes": record.byte_length,
            "manifest_bytes": record.manifest.len() / 2,
            "signature_status": "VALID",
            "publisher": record.publisher,
            "generation": record.generation,
            "observed_at": record.published_at,
            "durability": "local-fsync",
            "state": "AVAILABLE",
            "protocol_receipt": false,
        })
    }

    fn finalize(&self, store: &mut Store, tenant: &Tenant, r: FinalizeReq) -> Op {
        let session_id = hex::encode(unhex::<32>(&r.session)?);
        let session = store
            .state
            .sessions
            .get(&session_id)
            .filter(|s| s.tenant == tenant.id)
            .cloned()
            .ok_or(ArtifactError::ContentUnavailable)?;
        let envelope_bytes = unhex_vec(&r.envelope, MAX_ENVELOPE_BYTES)?;
        let envelope = decode_envelope(&envelope_bytes)?;
        let verified = verify_publisher(&envelope).map_err(|f| match f {
            VerificationFailure::Artifact(e) => e,
            VerificationFailure::Host(_) => ArtifactError::SignatureInvalid,
        })?;
        let root = hex::encode(verified.root.bytes());
        if session.state == SessionState::Published {
            return match (&session.root, store.state.objects.get(&root)) {
                (Some(bound), Some(record)) if *bound == root => Ok(Self::receipt(&root, record)),
                _ => Err(ArtifactError::IntegrityConflict),
            };
        }
        staging_open(&session)?;
        let manifest = decode_manifest(envelope.manifest)?;
        if hex::encode(manifest.context.bytes()) != session.context
            || manifest.kind as u8 != session.kind
            || manifest.privacy as u8 != session.privacy
        {
            return Err(ArtifactError::InvalidContext);
        }
        if manifest.byte_length != session.byte_length {
            return Err(ArtifactError::LengthMismatch);
        }
        if session
            .expected_root
            .as_ref()
            .is_some_and(|e| *e != hex::encode(manifest.content_root))
        {
            return Err(ArtifactError::RootMismatch);
        }
        let publisher = hex::encode(manifest.publisher.bytes());
        self.config.admit_publisher(
            &tenant.id,
            &publisher,
            verified.generation.get(),
            &hex::encode(verified.key.0),
            &store.state.publisher_revocations,
        )?;
        let declaration = match (&r.declaration, manifest.declaration_root == [0; 32]) {
            (None, true) => None,
            (Some(text), false) => {
                let bytes = unhex_vec(text, MAX_RECORD_BYTES)?;
                let parsed = decode_declaration(&bytes)?;
                if declaration_root(&manifest.context, &bytes)?.bytes() != manifest.declaration_root
                {
                    return Err(ArtifactError::RootMismatch);
                }
                if parsed.publisher != manifest.publisher {
                    return Err(ArtifactError::InvalidContext);
                }
                let (purposes, rights) = (parsed.purpose_mask, parsed.rights as u8);
                Some((bytes, purposes, rights))
            }
            _ => return Err(ArtifactError::Malformed),
        };
        store.verify_content(&session_id, &manifest)?;
        let object_key = match (&session.wrapped_key, manifest.privacy) {
            (Some(wrapped), Privacy::Encrypted) => {
                let key = self.kms.unwrap(
                    &session_key_aad(&session_id),
                    &unhex_vec(wrapped, WRAPPED_KEY_BYTES)?,
                )?;
                let frames = FrameContext {
                    context: manifest.context.bytes(),
                    subject: manifest.subject,
                    kind: manifest.kind as u8,
                };
                let reader = store.chunk_reader(&session_id, manifest.chunk_count);
                if let Err(error) = crypto::open_stream(&key, &frames, reader, |_| ()) {
                    if let Some(s) = store.state.sessions.get_mut(&session_id) {
                        s.state = SessionState::Quarantined;
                    }
                    store.release_quota(&tenant.id, session.byte_length);
                    store.event(&format!("quarantine session={session_id}"))?;
                    return Err(error);
                }
                Some(key)
            }
            (None, Privacy::Public) => None,
            _ => return Err(ArtifactError::InvalidContext),
        };
        let declaration_bytes = declaration
            .as_ref()
            .map(|d| d.0.clone())
            .unwrap_or_default();
        let record_digest = hex::encode(sha(&[envelope.manifest, &[0], &declaration_bytes]));
        if let Some(existing) = store.state.objects.get(&root).cloned() {
            let same = existing.record_digest == record_digest
                && existing.state != ObjectState::Publishing;
            if let Some(s) = store.state.sessions.get_mut(&session_id) {
                s.state = if same {
                    SessionState::Published
                } else {
                    SessionState::Quarantined
                };
                s.root = same.then(|| root.clone());
            }
            store.release_quota(&tenant.id, session.byte_length);
            if same {
                return Ok(Self::receipt(&root, &existing));
            }
            store.event(&format!("quarantine session={session_id}"))?;
            return Err(ArtifactError::IntegrityConflict);
        }
        let wrapped_key = object_key
            .map(|k| self.kms.wrap(&key_aad(&root, 1), &k).map(hex::encode))
            .transpose()?;
        let published_at = now();
        let record = ObjectRecord {
            tenant: tenant.id.clone(),
            session: session_id.clone(),
            manifest: hex::encode(envelope.manifest),
            envelope: hex::encode(&envelope_bytes),
            declaration: declaration.as_ref().map(|d| hex::encode(&d.0)),
            declared_purposes: declaration.as_ref().map(|d| d.1),
            rights: declaration.as_ref().map(|d| d.2),
            record_digest,
            state: ObjectState::Publishing,
            privacy: manifest.privacy as u8,
            byte_length: manifest.byte_length,
            chunk_count: manifest.chunk_count,
            publisher,
            generation: verified.generation.get(),
            wrapped_key,
            access_generation: 1,
            published_at,
            tombstone_reason: None,
            retention_until: None,
            history: vec![Transition {
                state: ObjectState::Publishing,
                at: published_at,
            }],
        };
        store.state.objects.insert(root.clone(), record);
        store.persist()?;
        store.event(&format!(
            "publish-intent root={root} kms={}",
            self.kms.key_id()
        ))?;
        let record = store
            .state
            .objects
            .get_mut(&root)
            .ok_or(ArtifactError::StorageFailure)?;
        record.transition(ObjectState::Available, published_at);
        let receipt = Self::receipt(&root, record);
        if let Some(s) = store.state.sessions.get_mut(&session_id) {
            s.state = SessionState::Published;
            s.root = Some(root.clone());
        }
        store.event(&format!("published root={root}"))?;
        Ok(receipt)
    }

    fn owned<'a>(
        store: &'a mut Store,
        tenant: &Tenant,
        root: &str,
    ) -> Result<&'a mut ObjectRecord, ArtifactError> {
        let root = hex::encode(unhex::<32>(root)?);
        store
            .state
            .objects
            .get_mut(&root)
            .filter(|o| o.tenant == tenant.id)
            .ok_or(ArtifactError::Unauthorized)
    }

    fn grant(&self, store: &mut Store, tenant: &Tenant, r: GrantReq) -> Op {
        let root = hex::encode(unhex::<32>(&r.root)?);
        let task = hex::encode(unhex::<32>(&r.task)?);
        if r.purposes == 0 || r.purposes & !0x000f != 0 || r.ttl_secs > MAX_LEASE_SECS {
            return Err(ArtifactError::Malformed);
        }
        if self.config.tenant(&r.grantee).is_none() {
            return Err(ArtifactError::Unauthorized);
        }
        let object = Self::owned(store, tenant, &root)?;
        if object.state != ObjectState::Available {
            return Err(ArtifactError::Tombstoned);
        }
        let generation = object.access_generation;
        let grant_id = hex::encode(sha(&[
            b"PAXAI/grant/v1\0",
            tenant.id.as_bytes(),
            &[0],
            r.request_id.as_bytes(),
        ]));
        let expires_at = now() + r.ttl_secs;
        store.state.grants.insert(
            grant_id.clone(),
            Grant {
                issuer: tenant.id.clone(),
                grantee: r.grantee,
                root,
                task,
                purpose_mask: r.purposes,
                generation,
                expires_at,
                revoked: false,
                revocation_sequence: 0,
                revocation_digest: None,
            },
        );
        store.event(&format!("grant id={grant_id}"))?;
        Ok(json!({ "grant_id": grant_id, "generation": generation, "expires_at": expires_at }))
    }

    fn revoke(&self, store: &mut Store, tenant: &Tenant, r: RevokeReq) -> Op {
        let grant_id = hex::encode(unhex::<32>(&r.grant_id)?);
        let grant = store
            .state
            .grants
            .get_mut(&grant_id)
            .filter(|g| g.issuer == tenant.id)
            .ok_or(ArtifactError::Unauthorized)?;
        let digest = hex::encode(sha(&[
            grant_id.as_bytes(),
            &r.generation.to_be_bytes(),
            &r.sequence.to_be_bytes(),
        ]));
        if r.sequence == grant.revocation_sequence
            && grant.revocation_digest.as_deref() == Some(digest.as_str())
        {
            return Ok(json!({ "grant_id": grant_id, "sequence": r.sequence, "revoked": true }));
        }
        if r.sequence <= grant.revocation_sequence || r.generation != grant.generation {
            return Err(ArtifactError::IdempotencyConflict);
        }
        grant.revoked = true;
        grant.revocation_sequence = r.sequence;
        grant.revocation_digest = Some(digest);
        store.state.revocation_mark += 1;
        store.event(&format!("revoke grant={grant_id} sequence={}", r.sequence))?;
        Ok(json!({ "grant_id": grant_id, "sequence": r.sequence, "revoked": true }))
    }

    fn rotate(&self, store: &mut Store, tenant: &Tenant, r: RootReq) -> Op {
        let root = hex::encode(unhex::<32>(&r.root)?);
        let object = Self::owned(store, tenant, &root)?;
        let wrapped = object
            .wrapped_key
            .as_deref()
            .ok_or(ArtifactError::Malformed)?;
        let key = self.kms.unwrap(
            &key_aad(&root, object.access_generation),
            &unhex_vec(wrapped, WRAPPED_KEY_BYTES)?,
        )?;
        let generation = object.access_generation + 1;
        object.wrapped_key = Some(hex::encode(
            self.kms.wrap(&key_aad(&root, generation), &key)?,
        ));
        object.access_generation = generation;
        store.state.revocation_mark += 1;
        store.event(&format!("rotate root={root} generation={generation}"))?;
        Ok(json!({ "root": root, "access_generation": generation }))
    }

    /// `TombstoneArtifact` through the retention ledger: the deletion deadline
    /// is the ledger's (terminal finality floor raised by holds), never a fixed
    /// offset from the request.
    fn tombstone(&self, store: &mut Store, tenant: &Tenant, r: &RootReq) -> Result<Value, Refusal> {
        let reason = r.reason.ok_or(ArtifactError::Malformed)?;
        let root = hex::encode(unhex::<32>(&r.root)?);
        let t = self
            .ledger()
            .tombstone(store, &tenant.id, &root, reason, now())?;
        Ok(json!({
            "root": root,
            "state": "TOMBSTONED",
            "reason": t.reason,
            "version": t.version,
            "tombstoned_at": t.at,
            "purge_after": t.purge_after,
        }))
    }

    fn place_hold(
        &self,
        store: &mut Store,
        tenant: &Tenant,
        r: &HoldReq,
    ) -> Result<Value, Refusal> {
        let root = hex::encode(unhex::<32>(&r.root)?);
        let hold = unhex::<32>(&r.hold)?;
        let until = r.until.ok_or(ArtifactError::Malformed)?;
        let until = self
            .ledger()
            .place_hold(store, &tenant.id, &root, hold, until, now())?;
        Ok(json!({ "root": root, "hold": hex::encode(hold), "until": until }))
    }

    fn release_hold(
        &self,
        store: &mut Store,
        tenant: &Tenant,
        r: &HoldReq,
    ) -> Result<Value, Refusal> {
        let root = hex::encode(unhex::<32>(&r.root)?);
        let hold = unhex::<32>(&r.hold)?;
        self.ledger().release_hold(store, &tenant.id, &root, hold)?;
        Ok(json!({ "root": root, "hold": hex::encode(hold), "released": true }))
    }

    fn reinstate(
        &self,
        store: &mut Store,
        tenant: &Tenant,
        r: &ReinstateReq,
    ) -> Result<Value, Refusal> {
        let root = hex::encode(unhex::<32>(&r.root)?);
        let grant = hex::encode(unhex::<32>(&r.grant_id)?);
        let decision = unhex::<32>(&r.decision)?;
        self.ledger()
            .reinstate(store, &tenant.id, &root, &grant, decision, now())?;
        Ok(json!({ "root": root, "state": "AVAILABLE" }))
    }

    /// The owner submitted a binding transaction; its outcome is unknown.
    fn binding_intent(
        &self,
        store: &mut Store,
        tenant: &Tenant,
        r: &RootReq,
    ) -> Result<Value, Refusal> {
        let root = hex::encode(unhex::<32>(&r.root)?);
        Self::owned(store, tenant, &root)?;
        let binding = self.ledger().binding_intent(store, &root)?;
        Ok(json!({ "root": root, "binding": binding.name() }))
    }

    fn orphan_binding(
        &self,
        store: &mut Store,
        tenant: &Tenant,
        r: &RootReq,
    ) -> Result<Value, Refusal> {
        self.config.observer(tenant)?;
        let root = hex::encode(unhex::<32>(&r.root)?);
        let binding = self.ledger().orphan_binding(store, &root)?;
        Ok(json!({ "root": root, "binding": binding.name() }))
    }

    fn observe_terminal(
        &self,
        store: &mut Store,
        tenant: &Tenant,
        r: &ObserveReq,
    ) -> Result<Value, Refusal> {
        self.config.observer(tenant)?;
        let (root, snapshot, state) = observed(store, r)?;
        let until = self
            .ledger()
            .observe_terminal(store, &root, &snapshot, &state, now())?;
        Ok(json!({ "root": root, "retain_until": until }))
    }

    fn observe_binding(
        &self,
        store: &mut Store,
        tenant: &Tenant,
        r: &ObserveReq,
    ) -> Result<Value, Refusal> {
        self.config.observer(tenant)?;
        let (root, snapshot, state) = observed(store, r)?;
        let binding = self
            .ledger()
            .observe_binding(store, &root, &snapshot, &state)?;
        Ok(json!({ "root": root, "binding": binding.name() }))
    }

    fn projection(&self, body: &[u8]) -> Result<Value, Refusal> {
        let r: ProjectionReq = parse(body)?;
        let root = hex::encode(unhex::<32>(&r.root)?);
        let store = self.lock();
        let p = self.ledger().projection(&store, &root)?;
        Ok(json!({
            "root": root,
            "state": p.state,
            "availability": p.availability,
            "binding": p.binding,
            "tombstone_reason": p.tombstone_reason,
            "tombstone_version": p.tombstone_version,
            "retain_until": p.retain_until,
        }))
    }

    fn revoke_publisher(&self, store: &mut Store, tenant: &Tenant, r: PublisherRevokeReq) -> Op {
        let principal = hex::encode(unhex::<32>(&r.principal)?);
        if !self.config.publishers.iter().any(|p| {
            p.principal == principal && p.generation == r.generation && p.tenant == tenant.id
        }) {
            return Err(ArtifactError::Unauthorized);
        }
        let key = authority::publisher_key(&principal, r.generation);
        if let std::collections::btree_map::Entry::Vacant(entry) =
            store.state.publisher_revocations.entry(key)
        {
            entry.insert(now());
            store.state.revocation_mark += 1;
        }
        store.event(&format!("publisher-revoke generation={}", r.generation))?;
        Ok(json!({ "principal": principal, "generation": r.generation, "revoked": true }))
    }

    /// Current authority, revocation, purpose and grant checks; returns the record.
    fn authorize(
        store: &Store,
        tenant: &Tenant,
        r: &AccessReq,
    ) -> Result<(String, ObjectRecord, Option<String>), (ArtifactError, Value)> {
        let bare = |e| (e, json!({}));
        let root = hex::encode(unhex::<32>(&r.root).map_err(bare)?);
        let object = store
            .state
            .objects
            .get(&root)
            .cloned()
            .ok_or(bare(ArtifactError::ContentUnavailable))?;
        let attribution = json!({
            "object_state": object.state.name(),
            "attribution": {
                "publisher": object.publisher,
                "generation": object.generation,
                "signature_status": "VALID",
            },
        });
        let fail = |e| (e, attribution.clone());
        match object.state {
            ObjectState::Available => {}
            ObjectState::Tombstoned | ObjectState::Purged => {
                return Err(fail(ArtifactError::Tombstoned))
            }
            ObjectState::Quarantined => return Err(fail(ArtifactError::IntegrityConflict)),
            ObjectState::Publishing => return Err(fail(ArtifactError::ContentUnavailable)),
        }
        if store
            .state
            .publisher_revocations
            .contains_key(&authority::publisher_key(
                &object.publisher,
                object.generation,
            ))
        {
            return Err(fail(ArtifactError::AuthorityRevoked));
        }
        let bit = authority::purpose_bit(&r.purpose).map_err(fail)?;
        authority::check_purpose(bit, &object).map_err(fail)?;
        if object.privacy == Privacy::Public as u8 {
            return Ok((root, object, None));
        }
        let grant_id = r
            .grant_id
            .as_deref()
            .ok_or(fail(ArtifactError::Unauthorized))
            .and_then(|g| unhex::<32>(g).map(hex::encode).map_err(fail))?;
        let task = r
            .task
            .as_deref()
            .ok_or(fail(ArtifactError::InvalidContext))
            .and_then(|t| unhex::<32>(t).map(hex::encode).map_err(fail))?;
        let grant = store
            .state
            .grants
            .get(&grant_id)
            .ok_or(fail(ArtifactError::Unauthorized))?;
        authority::check_grant(
            grant,
            &tenant.id,
            &root,
            &task,
            bit,
            object.access_generation,
            now(),
        )
        .map_err(fail)?;
        Ok((root, object, Some(grant_id)))
    }

    /// A key release returns its delivery identity; the caller acknowledges
    /// it only after the response is written within the delivery deadline.
    fn resolve(&self, tenant: &Tenant, body: &[u8]) -> ((u16, Value), Option<String>) {
        let r: AccessReq = match parse(body) {
            Ok(v) => v,
            Err(e) => return (reply(Err(e)), None),
        };
        let mut store = self.lock();
        let observed_at = now();
        let refused = |e, extra| {
            let mut body = error_body(e, extra);
            body["status"] = json!(access_status(e));
            body["observed_at"] = json!(observed_at);
            ((http_status(e), body), None)
        };
        let (root, object, grant) = match Self::authorize(&store, tenant, &r) {
            Ok(v) => v,
            Err((e, extra)) => return refused(e, extra),
        };
        let mut value = json!({
            "status": if grant.is_some() { "AVAILABLE_AUTHORIZED" } else { "AVAILABLE_PUBLIC" },
            "root": root,
            "envelope": object.envelope,
            "declaration": object.declaration,
            "rights": authority::rights_label(object.rights),
            "integrity": "ROOT_AND_SIGNATURE_VERIFIED",
            "attribution": {
                "publisher": object.publisher,
                "generation": object.generation,
                "signature_status": "VALID",
            },
            "quality": "UNESTABLISHED",
            "reward_admission": false,
            "chunk_count": object.chunk_count,
            "observed_at": observed_at,
        });
        let (Some(grant_id), Some(wrapped)) = (grant, object.wrapped_key.as_deref()) else {
            return ((200, value), None);
        };
        let rid = match r.request_id.as_deref().map(request_id) {
            Some(Ok(rid)) => rid,
            Some(Err(e)) => return (reply(Err(e)), None),
            None => return (reply(Err(ArtifactError::Malformed)), None),
        };
        let key = match unhex_vec(wrapped, WRAPPED_KEY_BYTES).and_then(|w| {
            self.kms
                .unwrap(&key_aad(&root, object.access_generation), &w)
        }) {
            Ok(k) => k,
            Err(e) => return (reply(Err(e)), None),
        };
        let id = format!("{}:{rid}", tenant.id);
        let intent = Delivery {
            digest: store::delivery_digest(
                &root,
                &grant_id,
                r.task.as_deref().unwrap_or_default(),
                &r.purpose,
            ),
            root,
            grant: grant_id,
            generation: object.access_generation,
            attempts: 0,
            state: DeliveryState::Pending,
            updated_at: observed_at,
        };
        let reconciles = match store.begin_delivery(&id, intent) {
            Ok(prior) => prior,
            Err(e) => {
                if e == ArtifactError::StorageFailure {
                    let _ = store.reload();
                }
                return refused(e, json!({ "request_id": rid }));
            }
        };
        let attempt = store.state.deliveries.get(&id).map_or(0, |d| d.attempts);
        value["object_key"] = json!(hex::encode(key));
        value["access_generation"] = json!(object.access_generation);
        value["delivery"] = json!({
            "request_id": rid,
            "attempt": attempt,
            "reconciles": reconciles.map(DeliveryState::name),
        });
        ((200, value), Some(id))
    }

    /// Delivery reconciliation view: an intent whose write exceeded its
    /// deadline or was interrupted reports UnknownDelivery until a retry with
    /// the same request identity is acknowledged.
    fn delivery_status(&self, tenant: &Tenant, body: &[u8]) -> (u16, Value) {
        let rid = match parse::<DeliveryReq>(body).and_then(|r| request_id(&r.request_id)) {
            Ok(rid) => rid,
            Err(e) => return reply(Err(e)),
        };
        let store = self.lock();
        let Some(delivery) = store.state.deliveries.get(&format!("{}:{rid}", tenant.id)) else {
            return reply(Err(ArtifactError::ContentUnavailable));
        };
        let value = json!({
            "request_id": rid,
            "status": delivery.state.name(),
            "root": delivery.root,
            "access_generation": delivery.generation,
            "attempts": delivery.attempts,
            "observed_at": delivery.updated_at,
        });
        if delivery.state == DeliveryState::Unknown {
            let e = ArtifactError::UnknownDelivery;
            return (http_status(e), error_body(e, value));
        }
        (200, value)
    }

    fn content(&self, tenant: &Tenant, body: &[u8]) -> Reply {
        let r: AccessReq = match parse(body) {
            Ok(v) => v,
            Err(e) => return json_reply(reply(Err(e))),
        };
        let mut store = self.lock();
        let (root, object, _) = match Self::authorize(&store, tenant, &r) {
            Ok(v) => v,
            Err((e, extra)) => return json_reply((http_status(e), error_body(e, extra))),
        };
        let Some(index) = r.index.filter(|i| *i < object.chunk_count) else {
            return json_reply(reply(Err(ArtifactError::Malformed)));
        };
        let expected = match expected_chunk_length(object.byte_length, object.chunk_count, index) {
            Ok(n) => n as usize,
            Err(e) => return json_reply(reply(Err(e))),
        };
        match store.read_chunk(&object.session, index) {
            Ok(Some(bytes)) if bytes.len() == expected => (200, bytes, "application/octet-stream"),
            Ok(_) => {
                // Durable bytes of an acknowledged root are absent or damaged.
                if let Some(o) = store.state.objects.get_mut(&root) {
                    o.transition(ObjectState::Quarantined, now());
                }
                let durable = store
                    .persist()
                    .and_then(|()| store.event(&format!("quarantine root={root}")));
                if let Err(e) = durable {
                    let _ = store.reload();
                    return json_reply(reply(Err(e)));
                }
                let mut body = error_body(ArtifactError::IntegrityConflict, json!({}));
                body["status"] = json!(access_status(ArtifactError::IntegrityConflict));
                json_reply((http_status(ArtifactError::IntegrityConflict), body))
            }
            Err(e) => json_reply(reply(Err(e))),
        }
    }

    /// The object key is returned only to the authenticated preparing tenant
    /// and never stored in the request disposition, events or logs.
    fn attach_object_key(&self, (status, mut value): (u16, Value)) -> (u16, Value) {
        let Some(session_id) = value["session"]
            .as_str()
            .filter(|_| status == 200)
            .map(str::to_string)
        else {
            return (status, value);
        };
        let store = self.lock();
        let Some(session) = store.state.sessions.get(&session_id) else {
            return reply(Err(ArtifactError::StorageFailure));
        };
        if session.state != SessionState::Staging {
            return (status, value);
        }
        if let Some(wrapped) = &session.wrapped_key {
            let key = unhex_vec(wrapped, WRAPPED_KEY_BYTES)
                .and_then(|w| self.kms.unwrap(&session_key_aad(&session_id), &w));
            match key {
                Ok(key) => value["object_key"] = json!(hex::encode(key)),
                Err(e) => return reply(Err(e)),
            }
        }
        (status, value)
    }

    /// Imports a staging object from an admitted HTTPS locator: pinned admitted
    /// address, peer re-check before any byte, verified TLS, zero redirects,
    /// exact declared length, no credentials sent. Bytes land as upload frames.
    fn import(&self, tenant: &Tenant, body: &[u8]) -> Op {
        let r: ImportReq = parse(body)?;
        let session_id = hex::encode(unhex::<32>(&r.session)?);
        let (byte_length, count) = {
            let store = self.lock();
            let session = store
                .state
                .sessions
                .get(&session_id)
                .filter(|s| s.tenant == tenant.id)
                .ok_or(ArtifactError::ContentUnavailable)?;
            staging_open(session)?;
            (session.byte_length, session.chunk_count)
        };
        let admitted = resolver::admit(&r.uri, &tenant.locator_hosts, resolver::system_dns)?;
        let tcp = admitted.connect(|addr| {
            let stream = TcpStream::connect_timeout(&addr, IO_TIMEOUT)?;
            stream.set_read_timeout(Some(IO_TIMEOUT))?;
            stream.set_write_timeout(Some(IO_TIMEOUT))?;
            Ok(stream)
        })?;
        let connector = native_tls::TlsConnector::builder()
            .min_protocol_version(Some(native_tls::Protocol::Tlsv12))
            .build()
            .map_err(|_| ArtifactError::StorageFailure)?;
        let host = &admitted.locator.host;
        let mut tls = connector
            .connect(host, tcp)
            .map_err(|_| ArtifactError::UnsafeLocator)?;
        let authority = if host.contains(':') {
            format!("[{host}]")
        } else {
            host.clone()
        };
        let request = format!(
            "GET {} HTTP/1.1\r\nHost: {authority}\r\nAccept: application/octet-stream\r\nConnection: close\r\n\r\n",
            admitted.locator.path
        );
        tls.write_all(request.as_bytes())
            .map_err(|_| ArtifactError::ContentUnavailable)?;
        let mut reader = BufReader::new(tls);
        let lines = read_head(&mut reader)?;
        resolver::check_response_head(lines.join("\r\n").as_bytes())?;
        if header(&lines, "transfer-encoding").is_some() {
            return Err(ArtifactError::Malformed);
        }
        let declared: u64 = header(&lines, "content-length")
            .ok_or(ArtifactError::Malformed)?
            .parse()
            .map_err(|_| ArtifactError::Malformed)?;
        if declared != byte_length {
            return Err(ArtifactError::LengthMismatch);
        }
        let mut payload = reader.take(byte_length);
        for index in 0..count {
            let length = expected_chunk_length(byte_length, count, index)? as usize;
            let mut chunk = vec![0u8; length];
            payload
                .read_exact(&mut chunk)
                .map_err(|_| ArtifactError::ContentUnavailable)?;
            let store = self.lock();
            let session = store
                .state
                .sessions
                .get(&session_id)
                .ok_or(ArtifactError::ContentUnavailable)?;
            staging_open(session)?;
            store.write_chunk(&session_id, index, &chunk)?;
        }
        Ok(json!({ "session": session_id, "imported_bytes": byte_length }))
    }

    fn usage(&self, tenant: &Tenant) -> Value {
        let store = self.lock();
        let objects = store
            .state
            .objects
            .values()
            .filter(|o| o.tenant == tenant.id && o.state == ObjectState::Available)
            .count();
        json!({
            "tenant": tenant.id,
            "quota_used": store.state.quota_used.get(&tenant.id).copied().unwrap_or(0),
            "quota_bytes": tenant.quota_bytes,
            "objects": objects,
        })
    }
}

/// Captures the observed state's identity and binds it to the finality
/// evidence with `queries::bind_snapshot`; the ledger then checks the subject
/// task record in that exact state.
fn observed(store: &Store, r: &ObserveReq) -> Result<(String, SnapshotBinding, Vec<u8>), Refusal> {
    let root = hex::encode(unhex::<32>(&r.root)?);
    let record = store
        .state
        .objects
        .get(&root)
        .ok_or(ArtifactError::ContentUnavailable)?;
    let manifest = unhex_vec(&record.manifest, MAX_MANIFEST_BYTES)?;
    let context = decode_manifest(&manifest)?.context;
    let state = unhex_vec(&r.state, MAX_STATE_BYTES)?;
    let proof = ReadProof {
        chain: context.chain,
        program: context.program,
        native_state_root: digest(&r.native_state_root)?,
        observed_sequence: r.observed_sequence,
        execution_height: r.execution_height,
        batch_id: digest(&r.batch_id)?,
    };
    let facts = CaptureFacts {
        proof,
        revision: decode_shared_state(&state)
            .map_err(ArtifactError::from)?
            .revision,
        digest: codec::state_digest(&state).map_err(ArtifactError::from)?,
        total_bytes: u32::try_from(state.len()).map_err(|_| ArtifactError::Malformed)?,
        chunks: state.len().div_ceil(MAX_CHUNK_BYTES),
    };
    let finality = FinalityEvidence {
        native_state_root: proof.native_state_root,
        checkpoint: digest(&r.checkpoint)?,
        settlement: match &r.settlement {
            Some(text) => Presence::Present(digest(text)?),
            None => Presence::Absent,
        },
        rank: r.rank,
    };
    let snapshot =
        bind_snapshot(&state, &facts, &finality, r.publication_time_ms).map_err(query_refusal)?;
    Ok((root, snapshot, state))
}

/// Restart recovery: a publication intent completes only when every durable
/// chunk re-verifies, otherwise the staging disposition is durably failed;
/// unacknowledged deliveries become unknown; due purges run on the ledger's
/// deadlines and a failed one stays pending with an alert.
fn reconcile(store: &mut Store, ledger: &mut Ledger, at: u64) -> Result<(), Refusal> {
    let pending: Vec<String> = store
        .state
        .objects
        .iter()
        .filter(|(_, o)| o.state == ObjectState::Publishing)
        .map(|(root, _)| root.clone())
        .collect();
    for root in pending {
        let Some(record) = store.state.objects.get(&root).cloned() else {
            continue;
        };
        let bytes = unhex_vec(&record.manifest, MAX_MANIFEST_BYTES)?;
        let verified =
            decode_manifest(&bytes).and_then(|m| store.verify_content(&record.session, &m));
        if verified.is_ok() {
            if let Some(o) = store.state.objects.get_mut(&root) {
                o.transition(ObjectState::Available, at);
            }
            if let Some(s) = store.state.sessions.get_mut(&record.session) {
                s.state = SessionState::Published;
                s.root = Some(root.clone());
            }
        } else {
            store.state.objects.remove(&root);
            if let Some(s) = store.state.sessions.get_mut(&record.session) {
                s.state = SessionState::Failed;
            }
            store.release_quota(&record.tenant, record.byte_length);
        }
        store.event(&format!("reconcile root={root} ok={}", verified.is_ok()))?;
    }
    let unknown = store.interrupted_deliveries(at);
    if unknown > 0 {
        store.event(&format!("reconcile unknown-deliveries={unknown}"))?;
    }
    store.expire_staging(at);
    store.persist()?;
    alert(&ledger.purge(store, at)?);
    Ok(())
}

/// Periodic pass on the service clock: staging expiry and retention purge.
fn maintain(service: &Service) {
    let mut store = service.lock();
    let mut ledger = service.ledger();
    let at = now();
    let durable = if store.expire_staging(at) > 0 {
        store.persist()
    } else {
        Ok(())
    };
    match durable
        .map_err(Refusal::from)
        .and_then(|()| ledger.purge(&mut store, at))
    {
        Ok(report) => alert(&report),
        Err(_) => {
            eprintln!("maintenance pass failed; retrying next interval");
            let _ = store.reload();
        }
    }
}

fn json_reply((status, value): (u16, Value)) -> Reply {
    (status, value.to_string().into_bytes(), JSON)
}

fn read_request(stream: &mut impl Read) -> Result<Request, ArtifactError> {
    let mut reader = BufReader::new(stream);
    let lines = read_head(&mut reader)?;
    let mut first = lines.first().ok_or(ArtifactError::Malformed)?.split(' ');
    let method = first.next().ok_or(ArtifactError::Malformed)?.to_string();
    let path = first.next().ok_or(ArtifactError::Malformed)?.to_string();
    if header(&lines, "transfer-encoding").is_some() {
        return Err(ArtifactError::Malformed);
    }
    let length: usize = header(&lines, "content-length")
        .map_or(Ok(0), str::parse)
        .map_err(|_| ArtifactError::Malformed)?;
    let token = header(&lines, "authorization")
        .and_then(|v| v.strip_prefix("Bearer "))
        .map(str::to_string);
    let limit = if path.starts_with("/v1/upload/") {
        CHUNK_BYTES as usize
    } else if path == "/v1/retention/terminal" || path == "/v1/retention/binding" {
        MAX_OBSERVATION_REQUEST
    } else {
        MAX_API_REQUEST
    };
    if length > limit {
        return Err(ArtifactError::Malformed);
    }
    let mut body = vec![0; length];
    reader
        .read_exact(&mut body)
        .map_err(|_| ArtifactError::Malformed)?;
    Ok(Request {
        method,
        path,
        token,
        body,
    })
}

/// Server TLS stream that ends every response with close_notify. Nothing is
/// written on a connection whose handshake never completed.
struct Tls(StreamOwned<ServerConnection, TcpStream>);

impl Read for Tls {
    fn read(&mut self, buf: &mut [u8]) -> std::io::Result<usize> {
        self.0.read(buf)
    }
}

impl Write for Tls {
    fn write(&mut self, buf: &[u8]) -> std::io::Result<usize> {
        if self.0.conn.is_handshaking() {
            return Err(std::io::Error::other("tls handshake incomplete"));
        }
        self.0.write(buf)
    }

    fn flush(&mut self) -> std::io::Result<()> {
        self.0.flush()
    }
}

impl Drop for Tls {
    fn drop(&mut self) {
        if !self.0.conn.is_handshaking() {
            self.0.conn.send_close_notify();
            let _ = self.0.flush();
        }
    }
}

fn tls_config(certificate: &Path, private_key: &Path) -> Result<Arc<ServerConfig>, ArtifactError> {
    let chain = CertificateDer::pem_file_iter(certificate)
        .and_then(|certs| certs.collect::<Result<Vec<_>, _>>())
        .map_err(|_| ArtifactError::Malformed)?;
    let key = PrivateKeyDer::from_pem_file(private_key).map_err(|_| ArtifactError::Malformed)?;
    let config =
        ServerConfig::builder_with_provider(Arc::new(rustls::crypto::ring::default_provider()))
            .with_safe_default_protocol_versions()
            .map_err(|_| ArtifactError::Malformed)?
            .with_no_client_auth()
            .with_single_cert(chain, key)
            .map_err(|_| ArtifactError::Malformed)?;
    Ok(Arc::new(config))
}

fn handle<S: Read + Write + Send + 'static>(service: &Service, mut stream: S) {
    let (label, ((status, body, ctype), delivery)) = match read_request(&mut stream) {
        Ok(req) => {
            let label = format!(
                "{} {}",
                req.method,
                req.path.split('/').take(3).collect::<Vec<_>>().join("/")
            );
            (label, service.route(&req))
        }
        Err(e) => ("-".to_string(), (json_reply(reply(Err(e))), None)),
    };
    eprintln!("{label} {status}");
    let mut response = format!(
        "HTTP/1.1 {status} {}\r\nContent-Type: {ctype}\r\nContent-Length: {}\r\nConnection: close\r\n\r\n",
        if status == 200 { "OK" } else { "Refused" },
        body.len()
    )
    .into_bytes();
    response.extend_from_slice(&body);
    let Some(id) = delivery else {
        let _ = stream.write_all(&response).and_then(|()| stream.flush());
        return;
    };
    let delivered = store::deliver(stream, response, DELIVERY_DEADLINE);
    let mut store = service.lock();
    if store
        .settle_delivery(&id, delivered.is_some(), now())
        .is_err()
    {
        eprintln!("delivery acknowledgement not durable; it reconciles as unknown");
        let _ = store.reload();
    }
    drop(store);
    drop(delivered);
}

fn die(what: &str, e: impl Into<Refusal>) -> ! {
    eprintln!("startup refused: {what}: {}", e.into().name());
    std::process::exit(1);
}

/// The mark must not live in the restorable data dir, or a restore would roll
/// it back with the ledger it guards; an unresolvable path is refused.
fn outside(dir: &Path, mark: &Path) -> bool {
    let (Ok(dir), Some(parent), Some(name)) = (dir.canonicalize(), mark.parent(), mark.file_name())
    else {
        return false;
    };
    let parent = if parent.as_os_str().is_empty() {
        Path::new(".")
    } else {
        parent
    };
    parent
        .canonicalize()
        .is_ok_and(|p| !p.join(name).starts_with(&dir))
}

fn main() {
    let args: Vec<String> = std::env::args().collect();
    let arg = |name: &str| args.windows(2).find(|w| w[0] == name).map(|w| w[1].clone());
    let (Some(dir), Some(config), Some(key_file)) =
        (arg("--data-dir"), arg("--config"), arg("--key-file"))
    else {
        eprintln!("usage: layerx-ai-artifacts --data-dir DIR --config FILE --key-file FILE [--listen ADDR]");
        std::process::exit(2);
    };
    let listen = arg("--listen").unwrap_or_else(|| "127.0.0.1:0".to_string());
    let config: Config = std::fs::read(&config)
        .map_err(|_| ArtifactError::StorageFailure)
        .and_then(|b| serde_json::from_slice(&b).map_err(|_| ArtifactError::Malformed))
        .unwrap_or_else(|e| die("config", e));
    let kms = LocalFileKeyProvider::load(&PathBuf::from(key_file))
        .unwrap_or_else(|e| die("key provider", e));
    let dir = PathBuf::from(dir);
    let mut store = Store::open(&dir, &config.revocation_mark).unwrap_or_else(|e| die("store", e));
    if !store.restore_ready() {
        eprintln!(
            "startup refused: restore not ready: revocation mark {} below live mark {}",
            store.state.revocation_mark,
            store.live_mark()
        );
        std::process::exit(3);
    }
    let retention_mark = config.retention_mark();
    if !outside(&dir, &retention_mark) {
        die(
            "retention mark inside the data dir",
            ArtifactError::Malformed,
        );
    }
    let mut ledger =
        Ledger::open(&dir, &retention_mark).unwrap_or_else(|e| die("retention ledger", e));
    match ledger.recover(&mut store) {
        Ok(_) => {}
        Err(Refusal::RestoreNotReady) => {
            eprintln!("startup refused: restore not ready: retention ledger below its live mark");
            std::process::exit(3);
        }
        Err(e) => die("retention recovery", e),
    }
    reconcile(&mut store, &mut ledger, now()).unwrap_or_else(|e| die("reconcile", e));
    let tls = match &config.listener {
        Listener::Plaintext => None,
        Listener::Tls {
            certificate,
            private_key,
        } => Some(tls_config(certificate, private_key).unwrap_or_else(|e| die("tls", e))),
    };
    let listener =
        TcpListener::bind(&listen).unwrap_or_else(|_| die("listen", ArtifactError::StorageFailure));
    let local = listener
        .local_addr()
        .unwrap_or_else(|_| die("listen", ArtifactError::StorageFailure));
    println!("listening {local}");
    let _ = std::io::stdout().flush();
    let service = Arc::new(Service {
        config,
        kms: Box::new(kms),
        store: Mutex::new(store),
        ledger: Mutex::new(ledger),
    });
    let maintenance = Arc::clone(&service);
    std::thread::spawn(move || loop {
        std::thread::sleep(MAINTENANCE_INTERVAL);
        maintain(&maintenance);
    });
    for stream in listener.incoming().flatten() {
        if stream
            .set_read_timeout(Some(IO_TIMEOUT))
            .and_then(|()| stream.set_write_timeout(Some(IO_TIMEOUT)))
            .is_err()
        {
            continue;
        }
        let service = Arc::clone(&service);
        let tls = tls.clone();
        std::thread::spawn(move || match tls {
            None => handle(&service, stream),
            Some(config) => {
                if let Ok(conn) = ServerConnection::new(config) {
                    handle(&service, Tls(StreamOwned::new(conn, stream)));
                }
            }
        });
    }
}
