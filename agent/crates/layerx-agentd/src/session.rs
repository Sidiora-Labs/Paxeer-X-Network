//! Tenant-bound session lifecycle and daemon-only authentication tokens.

use std::collections::{BTreeMap, BTreeSet};
use std::fmt;

use layerx_types::ids::Did;
use sha2::{Digest as _, Sha256};
use zeroize::Zeroize;

use crate::events::outbound::{StopSignal, StopWatcher};
use crate::events::subscription::Termination;
use crate::identity::{IdentityRecord, ProtocolAuthority};
use crate::store::{ObjectKind, Store, StoreError, TenantId, TenantKey};

#[path = "session_revocation.rs"]
mod revocation;

pub use revocation::{
    InvalidationReason, InvalidationReport, PendingActivity, PreparationState, RevocationEvent,
};

const RECORD_VERSION: &[u8; 6] = b"LXSR05";
const PREVIOUS_RECORD_VERSION: &[u8; 6] = b"LXSR04";
const LEGACY_RECORD_VERSION: &[u8; 6] = b"LXSR02";
const FIRST_GENERATION: u64 = 1;
const TOKEN_CORRELATION_DOMAIN: &[u8] = b"layerx-agentd/tenant-audit-token-correlation/v1\0";
const REDACTED_BEARER: &str = "[REDACTED]";

/// Stable session identifier supplied by the daemon's secure identifier source.
#[derive(Clone, Copy, Debug, Eq, Ord, PartialEq, PartialOrd)]
pub struct SessionId(pub [u8; 32]);

/// Tenant-qualified session identity used by every in-memory lookup and invalidation signal.
#[derive(Clone, Debug, Eq, Ord, PartialEq, PartialOrd)]
pub struct SessionRef {
    pub tenant: TenantId,
    pub session_id: SessionId,
}

impl SessionRef {
    #[must_use]
    pub fn new(tenant: TenantId, session_id: SessionId) -> Self {
        Self { tenant, session_id }
    }
}

/// Opaque bearer material carried across daemon boundaries.
#[derive(Clone, Eq, PartialEq)]
pub struct SessionCredential {
    tenant: TenantId,
    session_id: SessionId,
    token_id: [u8; 32],
    generation: u64,
}

impl fmt::Debug for SessionCredential {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter
            .debug_struct("SessionCredential")
            .field("tenant", &self.tenant)
            .field("session_id", &self.session_id)
            .field("token_id", &REDACTED_BEARER)
            .field("generation", &self.generation)
            .finish()
    }
}

impl Drop for SessionCredential {
    fn drop(&mut self) {
        self.token_id.zeroize();
    }
}

impl SessionCredential {
    #[must_use]
    pub fn new(
        tenant: TenantId,
        session_id: SessionId,
        token_id: [u8; 32],
        generation: u64,
    ) -> Self {
        Self {
            tenant,
            session_id,
            token_id,
            generation,
        }
    }

    #[must_use]
    pub const fn tenant(&self) -> &TenantId {
        &self.tenant
    }

    #[must_use]
    pub const fn session_id(&self) -> SessionId {
        self.session_id
    }

    #[must_use]
    pub const fn token_id(&self) -> [u8; 32] {
        self.token_id
    }

    #[must_use]
    pub const fn generation(&self) -> u64 {
        self.generation
    }
}

/// Complete request required to open a session.
#[derive(Clone, Eq, PartialEq)]
pub struct OpenRequest {
    pub session_id: SessionId,
    pub token_id: [u8; 32],
    pub tenant: TenantId,
    pub agent: Did,
    pub authority: ProtocolAuthority,
    pub permitted_activity_types: BTreeSet<u16>,
    pub scopes: BTreeSet<String>,
    pub expiry_sequence: u64,
    pub expiry_seconds: Option<u64>,
    pub opening_client: String,
    pub policy_version: String,
}

impl fmt::Debug for OpenRequest {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter
            .debug_struct("OpenRequest")
            .field("session_id", &self.session_id)
            .field("token_id", &REDACTED_BEARER)
            .field("tenant", &self.tenant)
            .field("agent", &self.agent)
            .field("authority", &self.authority)
            .field("permitted_activity_types", &self.permitted_activity_types)
            .field("scopes", &self.scopes)
            .field("expiry_sequence", &self.expiry_sequence)
            .field("expiry_seconds", &self.expiry_seconds)
            .field("opening_client", &self.opening_client)
            .field("policy_version", &self.policy_version)
            .finish()
    }
}

/// A daemon authenticator. It is never accepted as protocol authority.
#[derive(Clone, Eq, PartialEq)]
pub struct Token {
    id: [u8; 32],
    session_id: SessionId,
    tenant: TenantId,
    agent: Did,
    scopes: BTreeSet<String>,
    expiry_sequence: u64,
    generation: u64,
}

impl fmt::Debug for Token {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter
            .debug_struct("Token")
            .field("id", &REDACTED_BEARER)
            .field("session_id", &self.session_id)
            .field("tenant", &self.tenant)
            .field("agent", &self.agent)
            .field("scopes", &self.scopes)
            .field("expiry_sequence", &self.expiry_sequence)
            .field("generation", &self.generation)
            .finish()
    }
}

impl Drop for Token {
    fn drop(&mut self) {
        self.id.zeroize();
    }
}

impl Token {
    /// Authorizes one operation against a server-owned set of acceptable scope spellings.
    ///
    /// # Errors
    ///
    /// Returns an error if the session, token, or scope is invalid, or durable state cannot be read or updated.
    pub fn authorize_any_scope(
        &self,
        sessions: &SessionRegistry,
        scopes: &[&str],
        core_sequence: u64,
    ) -> Result<SessionId, SessionError> {
        self.boundary(sessions).map_err(|_| SessionError::Revoked)?;
        if core_sequence >= self.expiry_sequence {
            return Err(SessionError::Expired);
        }
        if !scopes.iter().any(|scope| self.scopes.contains(*scope)) {
            return Err(SessionError::ScopeDenied);
        }
        Ok(self.session_id)
    }

    /// Checks tenant, agent, session liveness, revocation generation, core-relative expiry and
    /// scope against the current registry view.
    ///
    /// # Errors
    ///
    /// Returns `WrongPrincipal` for a mismatched tenant or agent, `Revoked` when the registry does
    /// not hold the session open at the generation the token was minted under, `Expired` once the
    /// core sequence reaches the token's expiry, and `ScopeDenied` for a scope the token does not
    /// carry.
    pub fn authorize(
        &self,
        sessions: &SessionRegistry,
        tenant: &TenantId,
        agent: &Did,
        scope: &str,
        core_sequence: u64,
    ) -> Result<SessionId, SessionError> {
        if &self.tenant != tenant || &self.agent != agent {
            return Err(SessionError::WrongPrincipal);
        }
        self.boundary(sessions).map_err(|_| SessionError::Revoked)?;
        if core_sequence >= self.expiry_sequence {
            return Err(SessionError::Expired);
        }
        if !self.scopes.contains(scope) {
            return Err(SessionError::ScopeDenied);
        }
        Ok(self.session_id)
    }

    /// Checks at one delivery or long-running-operation boundary that the token's session is
    /// still open at the token's generation.
    ///
    /// # Errors
    ///
    /// Returns the typed `RevokedEvent` when the session is absent from the registry, belongs to
    /// another principal, is closed, or has advanced to another generation.
    pub fn boundary(&self, sessions: &SessionRegistry) -> Result<(), RevokedEvent> {
        let record = sessions
            .get(&self.tenant, self.session_id)
            .filter(|record| {
                record.request.agent == self.agent && record.request.token_id == self.id
            });
        let current_generation = record.map(|record| record.generation);
        let open = record.is_some_and(|record| record.open);
        if open && current_generation == Some(self.generation) {
            return Ok(());
        }
        Err(RevokedEvent {
            tenant: self.tenant.clone(),
            session_id: self.session_id,
            token_generation: self.generation,
            current_generation,
            open,
        })
    }

    /// Returns the daemon token identifier for audit correlation.
    #[must_use]
    pub const fn token_id(&self) -> [u8; 32] {
        self.id
    }

    /// Returns an irreversible, domain-separated identifier suitable only for audit correlation.
    #[must_use]
    pub(crate) fn audit_correlation(&self) -> [u8; 32] {
        let mut digest = Sha256::new();
        digest.update(TOKEN_CORRELATION_DOMAIN);
        digest.update(self.id);
        digest.finalize().into()
    }

    /// Returns the revocation generation the token was minted under.
    #[must_use]
    pub const fn generation(&self) -> u64 {
        self.generation
    }

    /// Returns the exact opaque credential represented by this token.
    #[must_use]
    pub fn credential(&self) -> SessionCredential {
        SessionCredential::new(
            self.tenant.clone(),
            self.session_id,
            self.id,
            self.generation,
        )
    }

    #[must_use]
    pub const fn session_id(&self) -> SessionId {
        self.session_id
    }

    #[must_use]
    pub const fn tenant(&self) -> &TenantId {
        &self.tenant
    }

    #[must_use]
    pub const fn agent(&self) -> &Did {
        &self.agent
    }
}

/// Typed event that terminates in-flight work for a closed or revoked session.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct RevokedEvent {
    pub tenant: TenantId,
    pub session_id: SessionId,
    pub token_generation: u64,
    pub current_generation: Option<u64>,
    pub open: bool,
}

/// Durable session record, including the protocol authority actually used.
#[derive(Clone, Eq, PartialEq)]
pub struct SessionRecord {
    pub request: OpenRequest,
    pub open: bool,
    pub sequence: u64,
    pub budget_reserved: u128,
    pub subscription_cursor: u64,
    pub generation: u64,
    pub retired_token_ids: BTreeSet<[u8; 32]>,
}

impl fmt::Debug for SessionRecord {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter
            .debug_struct("SessionRecord")
            .field("request", &self.request)
            .field("open", &self.open)
            .field("sequence", &self.sequence)
            .field("budget_reserved", &self.budget_reserved)
            .field("subscription_cursor", &self.subscription_cursor)
            .field("generation", &self.generation)
            .field("retired_token_ids", &REDACTED_BEARER)
            .finish()
    }
}

/// In-memory index backed by the tenant-scoped durable store.
#[derive(Default)]
pub struct SessionRegistry {
    records: BTreeMap<SessionRef, SessionRecord>,
    revocation_stops: BTreeMap<SessionRef, Vec<(u64, StopWatcher)>>,
}

impl SessionRegistry {
    #[must_use]
    pub fn get(&self, tenant: &TenantId, id: SessionId) -> Option<&SessionRecord> {
        self.records.get(&SessionRef::new(tenant.clone(), id))
    }

    /// Returns the current revocation generation of one session the registry holds.
    #[must_use]
    pub fn generation(&self, tenant: &TenantId, session_id: SessionId) -> Option<u64> {
        self.get(tenant, session_id).map(|record| record.generation)
    }

    /// Authenticates exact externally carried bearer material without reconstructing a newer
    /// credential generation from a token identifier.
    ///
    /// # Errors
    ///
    /// Returns `NotFound` for a tenant-qualified session the registry never held and `Revoked`
    /// when it is closed or any token identifier or generation field was superseded.
    pub fn authenticate(&self, credential: &SessionCredential) -> Result<Token, SessionError> {
        let record = self
            .get(credential.tenant(), credential.session_id())
            .ok_or(SessionError::NotFound)?;
        if !record.open
            || record.request.token_id != credential.token_id()
            || record.generation != credential.generation()
        {
            return Err(SessionError::Revoked);
        }
        Ok(mint(record))
    }

    /// Authenticates a generation-unique opaque bearer identifier at a Human boundary. A bearer
    /// can name only the generation under which it was issued because scope changes permanently
    /// retire the previous identifier.
    ///
    /// # Errors
    ///
    /// Returns an error if the session, token, or scope is invalid, or durable state cannot be read or updated.
    pub fn authenticate_bearer(
        &self,
        tenant: &TenantId,
        session_id: SessionId,
        token_id: [u8; 32],
    ) -> Result<Token, SessionError> {
        let record = self.get(tenant, session_id).ok_or(SessionError::NotFound)?;
        if !record.open || record.request.token_id != token_id {
            return Err(SessionError::Revoked);
        }
        Ok(mint(record))
    }

    /// Records of one tenant only, open and closed; never crosses tenants.
    pub fn tenant_sessions<'a>(
        &'a self,
        tenant: &'a TenantId,
    ) -> impl Iterator<Item = &'a SessionRecord> + 'a {
        self.records
            .iter()
            .filter(move |(session, _)| &session.tenant == tenant)
            .map(|(_, record)| record)
    }

    #[must_use]
    pub fn open_count(&self) -> usize {
        self.records.values().filter(|record| record.open).count()
    }

    pub(crate) const fn records(&self) -> &BTreeMap<SessionRef, SessionRecord> {
        &self.records
    }

    pub(crate) fn replace(&mut self, key: &SessionRef, record: SessionRecord) {
        let revoked_generation = self.records.get(key).and_then(|previous| {
            (previous.open
                && (!record.open
                    || previous.generation != record.generation
                    || previous.request.token_id != record.request.token_id))
                .then_some(previous.generation)
        });
        self.records.insert(key.clone(), record);
        if let Some(generation) = revoked_generation {
            if let Some(stops) = self.revocation_stops.remove(key) {
                for (watched_generation, stop) in stops {
                    if watched_generation == generation {
                        stop.stop(Termination::SessionRevoked);
                    }
                }
            }
        }
    }

    /// Registers an exact-generation stop signal for an authorized long-running operation.
    /// The signal is armed only after a replacement record was durably persisted.
    ///
    /// # Errors
    ///
    /// Returns an error if the session, token, or scope is invalid, or durable state cannot be read or updated.
    pub fn revocation_stop(&mut self, token: &Token) -> Result<StopSignal, SessionError> {
        token.boundary(self).map_err(|_| SessionError::Revoked)?;
        let stop = StopSignal::active();
        let watchers = self
            .revocation_stops
            .entry(SessionRef::new(token.tenant.clone(), token.session_id))
            .or_default();
        watchers.retain(|(_, watcher)| watcher.live());
        watchers.push((token.generation, stop.watcher()));
        Ok(stop)
    }

    ///
    /// # Errors
    ///
    /// Returns an error if the session, token, or scope is invalid, or durable state cannot be read or updated.
    pub fn restore_tenant(&mut self, store: &Store, tenant: &TenantId) -> Result<(), SessionError> {
        let mut restored = BTreeMap::new();
        for object_id in store.list_object_ids(tenant, ObjectKind::Session) {
            let key = TenantKey::new(tenant.clone(), ObjectKind::Session, object_id.clone())?;
            let value = store.get(&key).ok_or(SessionError::NotFound)?;
            let record = decode(value.bytes(), tenant.clone())?;
            if object_id.as_slice() != record.request.session_id.0.as_slice()
                || &record.request.tenant != tenant
            {
                return Err(SessionError::IdentityMismatch);
            }
            let session = SessionRef::new(tenant.clone(), record.request.session_id);
            if self.records.contains_key(&session) || restored.insert(session, record).is_some() {
                return Err(SessionError::IdentityMismatch);
            }
        }
        self.records.extend(restored);
        Ok(())
    }
}

/// Session refusal taxonomy suitable for audit recording.
#[derive(Debug)]
pub enum SessionError {
    MissingField(&'static str),
    IdentityMismatch,
    AuthorityMissing,
    Expired,
    Revoked,
    WrongPrincipal,
    ScopeDenied,
    NotFound,
    AlreadyClosed,
    GenerationExhausted,
    TokenReuse,
    TokenHistoryExhausted,
    Store(StoreError),
}

impl PartialEq for SessionError {
    fn eq(&self, other: &Self) -> bool {
        match (self, other) {
            (Self::MissingField(left), Self::MissingField(right)) => left == right,
            (Self::Store(_), Self::Store(_)) => true,
            _ => std::mem::discriminant(self) == std::mem::discriminant(other),
        }
    }
}

impl Eq for SessionError {}

impl From<StoreError> for SessionError {
    fn from(value: StoreError) -> Self {
        Self::Store(value)
    }
}

/// Opens and durably records one independently scoped session.
///
/// # Errors
///
/// Returns `IdentityMismatch`, `MissingField`, `Expired` or `AuthorityMissing` from request
/// validation, `IdentityMismatch` for a session identifier the registry or store already records,
/// or `Store` when the session record cannot be encoded or durably written; the registry is left
/// untouched unless the record persisted.
pub fn open(
    store: &mut Store,
    registry: &mut SessionRegistry,
    identity: &IdentityRecord,
    request: OpenRequest,
    core_sequence: u64,
) -> Result<Token, SessionError> {
    validate_request(identity, &request, core_sequence)?;
    let session_ref = SessionRef::new(request.tenant.clone(), request.session_id);
    if registry.records.contains_key(&session_ref) || store.get(&session_key(&request)?).is_some() {
        return Err(SessionError::IdentityMismatch);
    }
    let record = SessionRecord {
        request,
        open: true,
        sequence: 0,
        budget_reserved: 0,
        subscription_cursor: 0,
        generation: FIRST_GENERATION,
        retired_token_ids: BTreeSet::new(),
    };
    persist_record(store, &record)?;
    let token = mint(&record);
    registry.records.insert(session_ref, record);
    Ok(token)
}

/// Advertises a verified public expiry on one open session that was enrolled without one.
///
/// # Errors
///
/// Returns `MissingField` for a zero expiry, `NotFound` for a session the registry never held,
/// `AlreadyClosed` for a closed session, `IdentityMismatch` when the session already advertises a
/// public expiry, or `Store` when the amended record cannot be persisted; the registry is left
/// untouched unless the record persisted.
pub fn amend_public_expiry(
    store: &mut Store,
    registry: &mut SessionRegistry,
    tenant: &TenantId,
    session_id: SessionId,
    expiry_seconds: u64,
) -> Result<(), SessionError> {
    if expiry_seconds == 0 {
        return Err(SessionError::MissingField("expiry_seconds"));
    }
    let session_ref = SessionRef::new(tenant.clone(), session_id);
    let existing = registry
        .records
        .get(&session_ref)
        .cloned()
        .ok_or(SessionError::NotFound)?;
    if !existing.open {
        return Err(SessionError::AlreadyClosed);
    }
    if existing.request.expiry_seconds.is_some() {
        return Err(SessionError::IdentityMismatch);
    }
    let mut amended = existing;
    amended.request.expiry_seconds = Some(expiry_seconds);
    persist_record(store, &amended)?;
    registry.replace(&session_ref, amended);
    Ok(())
}

/// Closes exactly one session without disturbing any sibling state.
///
/// # Errors
///
/// Returns `NotFound` for a session the registry never held, `AlreadyClosed` for one already
/// closed, `GenerationExhausted` when its revocation generation cannot advance, or `Store` when
/// the closed record cannot be persisted.
pub fn close(
    store: &mut Store,
    registry: &mut SessionRegistry,
    tenant: &TenantId,
    session_id: SessionId,
) -> Result<(), SessionError> {
    let session_ref = SessionRef::new(tenant.clone(), session_id);
    let existing = registry
        .records
        .get(&session_ref)
        .cloned()
        .ok_or(SessionError::NotFound)?;
    if !existing.open {
        return Err(SessionError::AlreadyClosed);
    }
    let mut closed = existing;
    closed.open = false;
    closed.generation = next_generation(&closed)?;
    persist_record(store, &closed)?;
    registry.replace(&session_ref, closed);
    Ok(())
}

///
/// # Errors
///
/// Returns an error if the session, token, or scope is invalid, or durable state cannot be read or updated.
pub fn close_with_companion(
    store: &mut Store,
    registry: &mut SessionRegistry,
    tenant: &TenantId,
    session_id: SessionId,
    companion_key: TenantKey,
    companion_bytes: Vec<u8>,
) -> Result<(), SessionError> {
    let session_ref = SessionRef::new(tenant.clone(), session_id);
    let existing = registry
        .records
        .get(&session_ref)
        .cloned()
        .ok_or(SessionError::NotFound)?;
    if !existing.open {
        return Err(SessionError::AlreadyClosed);
    }
    let mut closed = existing;
    closed.open = false;
    closed.generation = next_generation(&closed)?;
    let session_key = session_key(&closed.request)?;
    store.update_local_with_companion(
        session_key,
        encode(&closed)?,
        companion_key,
        companion_bytes,
    )?;
    registry.replace(&session_ref, closed);
    Ok(())
}

/// Narrows one open session's permitted scope, advancing its revocation generation and reissuing
/// its token under the new generation so every token minted before the change is refused.
///
/// # Errors
///
/// Returns `NotFound` for a session the registry never held, `AlreadyClosed` for a closed one,
/// `TokenReuse` when the proposed opaque bearer was ever issued for this session, `ScopeDenied`
/// when the requested scopes or activity types are empty or not a subset of the current ones,
/// `GenerationExhausted` when the generation cannot advance, or `Store` when the
/// narrowed record cannot be persisted; the session is unchanged unless the record persisted.
pub fn restrict_scope(
    store: &mut Store,
    registry: &mut SessionRegistry,
    tenant: &TenantId,
    session_id: SessionId,
    token_id: [u8; 32],
    scopes: BTreeSet<String>,
    permitted_activity_types: BTreeSet<u16>,
) -> Result<Token, SessionError> {
    let (session_ref, narrowed) = narrowed_record(
        registry,
        tenant,
        session_id,
        token_id,
        scopes,
        permitted_activity_types,
    )?;
    persist_record(store, &narrowed)?;
    let token = mint(&narrowed);
    registry.replace(&session_ref, narrowed);
    Ok(token)
}

/// Narrows a session while atomically updating its durable external coordinate and recording one
/// idempotent administrative observation.
///
/// # Errors
///
/// Returns an error if the session, token, or scope is invalid, or durable state cannot be read or updated.
pub fn restrict_scope_with_companion(
    store: &mut Store,
    registry: &mut SessionRegistry,
    tenant: &TenantId,
    restriction: ScopeRestriction,
    coordinate: (TenantKey, Vec<u8>),
    companion: (TenantKey, Vec<u8>),
) -> Result<Token, SessionError> {
    restrict_scope_with_companions(
        store,
        registry,
        tenant,
        restriction,
        coordinate,
        vec![companion],
    )
}

/// Narrows a session while atomically updating its durable coordinate and creating all of the
/// administrative records needed to replay the restriction without another generation advance.
///
/// # Errors
///
/// Returns an error if the session, token, or scope is invalid, or durable state cannot be read or updated.
pub fn restrict_scope_with_companions(
    store: &mut Store,
    registry: &mut SessionRegistry,
    tenant: &TenantId,
    restriction: ScopeRestriction,
    coordinate: (TenantKey, Vec<u8>),
    companions: Vec<(TenantKey, Vec<u8>)>,
) -> Result<Token, SessionError> {
    let ScopeRestriction {
        session_id,
        token_id,
        scopes,
        permitted_activity_types,
    } = restriction;
    let (coordinate_key, coordinate_bytes) = coordinate;
    let (session_ref, narrowed) = narrowed_record(
        registry,
        tenant,
        session_id,
        token_id,
        scopes,
        permitted_activity_types,
    )?;
    store.update_local_batch_with_companions(
        vec![
            (session_key(&narrowed.request)?, encode(&narrowed)?),
            (coordinate_key, coordinate_bytes),
        ],
        companions,
    )?;
    let token = mint(&narrowed);
    registry.replace(&session_ref, narrowed);
    Ok(token)
}

fn narrowed_record(
    registry: &SessionRegistry,
    tenant: &TenantId,
    session_id: SessionId,
    token_id: [u8; 32],
    scopes: BTreeSet<String>,
    permitted_activity_types: BTreeSet<u16>,
) -> Result<(SessionRef, SessionRecord), SessionError> {
    let session_ref = SessionRef::new(tenant.clone(), session_id);
    let existing = registry
        .records
        .get(&session_ref)
        .cloned()
        .ok_or(SessionError::NotFound)?;
    if !existing.open {
        return Err(SessionError::AlreadyClosed);
    }
    if token_id == [0; 32]
        || token_id == existing.request.token_id
        || existing.retired_token_ids.contains(&token_id)
    {
        return Err(SessionError::TokenReuse);
    }
    if scopes.is_empty()
        || !scopes.is_subset(&existing.request.scopes)
        || permitted_activity_types.is_empty()
        || !permitted_activity_types.is_subset(&existing.request.permitted_activity_types)
    {
        return Err(SessionError::ScopeDenied);
    }
    if existing.retired_token_ids.len() >= usize::from(u16::MAX) {
        return Err(SessionError::TokenHistoryExhausted);
    }
    let mut narrowed = existing;
    narrowed.retired_token_ids.insert(narrowed.request.token_id);
    narrowed.request.token_id = token_id;
    narrowed.request.scopes = scopes;
    narrowed.request.permitted_activity_types = permitted_activity_types;
    narrowed.generation = next_generation(&narrowed)?;
    Ok((session_ref, narrowed))
}

/// Applies a core revocation event to sessions and unsubmitted preparations.
///
/// # Errors
///
/// Returns `Store` when a revoked session's closed record cannot be persisted, `MissingField`
/// when its opening client or policy version exceeds the `u16` length prefix, or
/// `GenerationExhausted` when its revocation generation cannot advance.
pub fn invalidate_on_revocation(
    store: &mut Store,
    registry: &mut SessionRegistry,
    activities: &mut [PendingActivity],
    event: &RevocationEvent,
) -> Result<InvalidationReport, SessionError> {
    revocation::apply_revocation(store, registry, activities, event)
}

fn validate_request(
    identity: &IdentityRecord,
    request: &OpenRequest,
    core_sequence: u64,
) -> Result<(), SessionError> {
    if request.session_id.0 == [0; 32] {
        return Err(SessionError::MissingField("session_id"));
    }
    if request.token_id == [0; 32] {
        return Err(SessionError::MissingField("token_id"));
    }
    if identity.tenant() != &request.tenant || identity.did() != &request.agent {
        return Err(SessionError::IdentityMismatch);
    }
    if request.permitted_activity_types.is_empty() {
        return Err(SessionError::MissingField("permitted_activity_types"));
    }
    if request.scopes.is_empty() {
        return Err(SessionError::MissingField("scopes"));
    }
    if request.opening_client.is_empty() {
        return Err(SessionError::MissingField("opening_client"));
    }
    if request.policy_version.is_empty() {
        return Err(SessionError::MissingField("policy_version"));
    }
    if request.expiry_seconds == Some(0) {
        return Err(SessionError::MissingField("expiry_seconds"));
    }
    if request.expiry_sequence <= core_sequence {
        return Err(SessionError::Expired);
    }
    if !identity.authorities().contains(&request.authority) {
        return Err(SessionError::AuthorityMissing);
    }
    Ok(())
}

fn mint(record: &SessionRecord) -> Token {
    Token {
        id: record.request.token_id,
        session_id: record.request.session_id,
        tenant: record.request.tenant.clone(),
        agent: record.request.agent.clone(),
        scopes: record.request.scopes.clone(),
        expiry_sequence: record.request.expiry_sequence,
        generation: record.generation,
    }
}

fn next_generation(record: &SessionRecord) -> Result<u64, SessionError> {
    record
        .generation
        .checked_add(1)
        .ok_or(SessionError::GenerationExhausted)
}

fn session_key(request: &OpenRequest) -> Result<TenantKey, SessionError> {
    Ok(TenantKey::new(
        request.tenant.clone(),
        ObjectKind::Session,
        request.session_id.0.to_vec(),
    )?)
}

pub(crate) fn persist_record(
    store: &mut Store,
    record: &SessionRecord,
) -> Result<(), SessionError> {
    let key = session_key(&record.request)?;
    let encoded = encode(record)?;
    store.put_local(key, encoded)?;
    Ok(())
}

fn encode(record: &SessionRecord) -> Result<Vec<u8>, SessionError> {
    let client_len = u16::try_from(record.request.opening_client.len())
        .map_err(|_| SessionError::MissingField("opening_client"))?;
    let policy_len = u16::try_from(record.request.policy_version.len())
        .map_err(|_| SessionError::MissingField("policy_version"))?;
    let did = record.request.agent.as_bytes();
    let did_len = u16::try_from(did.len()).map_err(|_| SessionError::MissingField("agent"))?;
    let activity_len = u16::try_from(record.request.permitted_activity_types.len())
        .map_err(|_| SessionError::MissingField("permitted_activity_types"))?;
    let scope_len = u16::try_from(record.request.scopes.len())
        .map_err(|_| SessionError::MissingField("scopes"))?;
    let retired_len = u16::try_from(record.retired_token_ids.len())
        .map_err(|_| SessionError::TokenHistoryExhausted)?;
    if record.generation < FIRST_GENERATION {
        return Err(SessionError::MissingField("generation"));
    }
    let mut bytes = Vec::new();
    bytes.extend_from_slice(RECORD_VERSION);
    bytes.extend_from_slice(&record.request.session_id.0);
    bytes.extend_from_slice(&record.request.token_id);
    bytes.extend_from_slice(&record.request.expiry_sequence.to_be_bytes());
    match record.request.expiry_seconds {
        None => bytes.push(0),
        Some(seconds) => {
            bytes.push(1);
            bytes.extend_from_slice(&seconds.to_be_bytes());
        }
    }
    bytes.push(u8::from(record.open));
    bytes.extend_from_slice(&record.sequence.to_be_bytes());
    bytes.extend_from_slice(&record.budget_reserved.to_be_bytes());
    bytes.extend_from_slice(&record.subscription_cursor.to_be_bytes());
    bytes.extend_from_slice(&record.generation.to_be_bytes());
    bytes.extend_from_slice(&client_len.to_be_bytes());
    bytes.extend_from_slice(record.request.opening_client.as_bytes());
    bytes.extend_from_slice(&policy_len.to_be_bytes());
    bytes.extend_from_slice(record.request.policy_version.as_bytes());
    bytes.extend_from_slice(&did_len.to_be_bytes());
    bytes.extend_from_slice(did);
    let (authority_kind, authority_id) = match &record.request.authority {
        ProtocolAuthority::PrimaryKey(value) => (1, value),
        ProtocolAuthority::SessionKey(value) => (2, value),
        ProtocolAuthority::CapabilityGrant(value) => (3, value),
    };
    bytes.push(authority_kind);
    bytes.extend_from_slice(authority_id);
    bytes.extend_from_slice(&activity_len.to_be_bytes());
    for value in &record.request.permitted_activity_types {
        bytes.extend_from_slice(&value.to_be_bytes());
    }
    bytes.extend_from_slice(&scope_len.to_be_bytes());
    for value in &record.request.scopes {
        let length =
            u16::try_from(value.len()).map_err(|_| SessionError::MissingField("scopes"))?;
        bytes.extend_from_slice(&length.to_be_bytes());
        bytes.extend_from_slice(value.as_bytes());
    }
    bytes.extend_from_slice(&retired_len.to_be_bytes());
    for token_id in &record.retired_token_ids {
        bytes.extend_from_slice(token_id);
    }
    Ok(bytes)
}

struct RecordReader<'a> {
    bytes: &'a [u8],
    at: usize,
}

impl<'a> RecordReader<'a> {
    fn take(&mut self, length: usize) -> Result<&'a [u8], SessionError> {
        let end = self
            .at
            .checked_add(length)
            .ok_or(SessionError::MissingField("record"))?;
        let value = self
            .bytes
            .get(self.at..end)
            .ok_or(SessionError::MissingField("record"))?;
        self.at = end;
        Ok(value)
    }

    fn fixed<const N: usize>(&mut self, field: &'static str) -> Result<[u8; N], SessionError> {
        self.take(N)?
            .try_into()
            .map_err(|_| SessionError::MissingField(field))
    }

    fn text(&mut self) -> Result<String, SessionError> {
        let length = usize::from(u16::from_be_bytes(self.fixed::<2>("text")?));
        String::from_utf8(self.take(length)?.to_vec())
            .map_err(|_| SessionError::MissingField("text"))
    }

    fn authority(&mut self) -> Result<ProtocolAuthority, SessionError> {
        let kind = self.take(1)?[0];
        let id = self.fixed::<32>("authority")?;
        match kind {
            1 => Ok(ProtocolAuthority::PrimaryKey(id)),
            2 => Ok(ProtocolAuthority::SessionKey(id)),
            3 => Ok(ProtocolAuthority::CapabilityGrant(id)),
            _ => Err(SessionError::MissingField("authority")),
        }
    }
}

fn decode(bytes: &[u8], tenant: TenantId) -> Result<SessionRecord, SessionError> {
    let mut reader = RecordReader { bytes, at: 0 };
    let (carries_generation, carries_retired_tokens, carries_public_expiry) =
        match reader.take(6)? {
            version if version == RECORD_VERSION => (true, true, true),
            version if version == PREVIOUS_RECORD_VERSION => (true, true, false),
            version if version == LEGACY_RECORD_VERSION => (false, false, false),
            _ => return Err(SessionError::MissingField("record_version")),
        };
    let session_id = SessionId(reader.fixed::<32>("session_id")?);
    let token_id = reader.fixed::<32>("token_id")?;
    let expiry_sequence = u64::from_be_bytes(reader.fixed::<8>("expiry")?);
    let expiry_seconds = if carries_public_expiry {
        match reader.take(1)?[0] {
            0 => None,
            1 => match u64::from_be_bytes(reader.fixed::<8>("expiry_seconds")?) {
                0 => return Err(SessionError::MissingField("expiry_seconds")),
                seconds => Some(seconds),
            },
            _ => return Err(SessionError::MissingField("expiry_seconds")),
        }
    } else {
        None
    };
    let open = match reader.take(1)?[0] {
        0 => false,
        1 => true,
        _ => return Err(SessionError::MissingField("open")),
    };
    let sequence = u64::from_be_bytes(reader.fixed::<8>("sequence")?);
    let budget_reserved = u128::from_be_bytes(reader.fixed::<16>("budget")?);
    let subscription_cursor = u64::from_be_bytes(reader.fixed::<8>("cursor")?);
    let generation = if carries_generation {
        u64::from_be_bytes(reader.fixed::<8>("generation")?)
    } else {
        FIRST_GENERATION
    };
    if generation < FIRST_GENERATION {
        return Err(SessionError::MissingField("generation"));
    }
    let opening_client = reader.text()?;
    let policy_version = reader.text()?;
    let did_len = usize::from(u16::from_be_bytes(reader.fixed::<2>("agent")?));
    let agent = Did::new(reader.take(did_len)?).map_err(|_| SessionError::MissingField("agent"))?;
    let authority_id = reader.authority()?;
    let activity_count = usize::from(u16::from_be_bytes(reader.fixed::<2>("activities")?));
    let mut permitted_activity_types = BTreeSet::new();
    for _ in 0..activity_count {
        permitted_activity_types.insert(u16::from_be_bytes(reader.fixed::<2>("activities")?));
    }
    let scope_count = usize::from(u16::from_be_bytes(reader.fixed::<2>("scopes")?));
    let mut scopes = BTreeSet::new();
    for _ in 0..scope_count {
        scopes.insert(reader.text()?);
    }
    let mut retired_token_ids = BTreeSet::new();
    if carries_retired_tokens {
        let retired_count =
            usize::from(u16::from_be_bytes(reader.fixed::<2>("retired_token_ids")?));
        for _ in 0..retired_count {
            let token_id = reader.fixed::<32>("retired_token_ids")?;
            if token_id == [0; 32] || !retired_token_ids.insert(token_id) {
                return Err(SessionError::MissingField("retired_token_ids"));
            }
        }
    }
    if reader.at != bytes.len()
        || permitted_activity_types.is_empty()
        || scopes.is_empty()
        || token_id == [0; 32]
        || retired_token_ids.contains(&token_id)
    {
        return Err(SessionError::MissingField("record"));
    }
    Ok(SessionRecord {
        request: OpenRequest {
            session_id,
            token_id,
            tenant,
            agent,
            authority: authority_id,
            permitted_activity_types,
            scopes,
            expiry_sequence,
            expiry_seconds,
            opening_client,
            policy_version,
        },
        open,
        sequence,
        budget_reserved,
        subscription_cursor,
        generation,
        retired_token_ids,
    })
}

pub struct ScopeRestriction {
    pub session_id: SessionId,
    pub token_id: [u8; 32],
    pub scopes: BTreeSet<String>,
    pub permitted_activity_types: BTreeSet<u16>,
}

pub(crate) fn invalidate_with_projection(
    store: &mut Store,
    registry: &mut SessionRegistry,
    event: &RevocationEvent,
    updates: Vec<(crate::store::TenantKey, Vec<u8>)>,
) -> Result<InvalidationReport, SessionError> {
    revocation::apply_revocation_with_updates(store, registry, &mut [], event, updates)
}

impl SessionRecord {
    /// # Errors
    ///
    /// Returns `MissingField("expiry_seconds")` when no public time expiry was admitted or it overflows milliseconds.
    pub fn public_expiry_within(&self, core_time_ms: u64) -> Result<bool, SessionError> {
        let expiry_ms = self
            .request
            .expiry_seconds
            .and_then(|seconds| seconds.checked_mul(1_000))
            .ok_or(SessionError::MissingField("expiry_seconds"))?;
        Ok(core_time_ms < expiry_ms)
    }
}


const NATIVE_SCOPE_VERSION: &[u8; 6] = b"LXNS01";
const NATIVE_SCOPE_KEY_PREFIX: &[u8] = b"native-session-scope/v1/";

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct NativeSessionScopeV1 {
    pub tenant: TenantId,
    pub agent: Did,
    pub session_id: SessionId,
    pub generation: u64,
    pub permitted_activities: BTreeSet<layerx_agent_api::identity::NativeActivity>,
}

impl NativeSessionScopeV1 {
    pub fn encode(&self) -> Result<Vec<u8>, SessionError> {
        if self.session_id.0 == [0; 32] || self.generation < FIRST_GENERATION {
            return Err(SessionError::MissingField("native_session_binding"));
        }
        let tenant = self.tenant.as_str().as_bytes();
        let agent = self.agent.as_bytes();
        let tenant_len = u16::try_from(tenant.len())
            .map_err(|_| SessionError::MissingField("tenant"))?;
        let agent_len = u16::try_from(agent.len())
            .map_err(|_| SessionError::MissingField("agent"))?;
        let count = u16::try_from(self.permitted_activities.len())
            .map_err(|_| SessionError::MissingField("native_activities"))?;
        let mut bytes = Vec::new();
        bytes.extend_from_slice(NATIVE_SCOPE_VERSION);
        bytes.extend_from_slice(&tenant_len.to_be_bytes());
        bytes.extend_from_slice(tenant);
        bytes.extend_from_slice(&agent_len.to_be_bytes());
        bytes.extend_from_slice(agent);
        bytes.extend_from_slice(&self.session_id.0);
        bytes.extend_from_slice(&self.generation.to_be_bytes());
        bytes.extend_from_slice(&count.to_be_bytes());
        for activity in &self.permitted_activities {
            bytes.extend_from_slice(
                &activity.encode().map_err(|_| SessionError::MissingField("native_activity"))?,
            );
        }
        Ok(bytes)
    }

    pub fn decode(bytes: &[u8]) -> Result<Self, SessionError> {
        let mut reader = RecordReader { bytes, at: 0 };
        if reader.take(NATIVE_SCOPE_VERSION.len())? != NATIVE_SCOPE_VERSION {
            return Err(SessionError::MissingField("native_scope_version"));
        }
        let tenant = TenantId::new(reader.text()?)?;
        let agent_len = usize::from(u16::from_be_bytes(reader.fixed::<2>("agent")?));
        let agent = Did::new(reader.take(agent_len)?)
            .map_err(|_| SessionError::MissingField("agent"))?;
        let session_id = SessionId(reader.fixed::<32>("session_id")?);
        let generation = u64::from_be_bytes(reader.fixed::<8>("generation")?);
        if session_id.0 == [0; 32] || generation < FIRST_GENERATION {
            return Err(SessionError::MissingField("native_session_binding"));
        }
        let count = usize::from(u16::from_be_bytes(reader.fixed::<2>("native_activities")?));
        let mut permitted_activities = BTreeSet::new();
        let mut previous = None;
        for _ in 0..count {
            let activity = layerx_agent_api::identity::NativeActivity::decode(reader.take(5)?)
                .map_err(|_| SessionError::MissingField("native_activity"))?;
            if previous.is_some_and(|value| value >= activity) {
                return Err(SessionError::MissingField("native_activity_order"));
            }
            previous = Some(activity);
            permitted_activities.insert(activity);
        }
        if reader.at != bytes.len() {
            return Err(SessionError::MissingField("native_scope_trailing_bytes"));
        }
        Ok(Self { tenant, agent, session_id, generation, permitted_activities })
    }
}

#[derive(Debug, Eq, PartialEq)]
pub struct NativeSessionAuthorizationV1 {
    tenant: TenantId,
    agent: Did,
    session_id: SessionId,
    generation: u64,
    activity: layerx_agent_api::identity::NativeActivity,
}

impl NativeSessionAuthorizationV1 {
    #[must_use]
    pub const fn tenant(&self) -> &TenantId {
        &self.tenant
    }

    #[must_use]
    pub const fn agent(&self) -> &Did {
        &self.agent
    }

    #[must_use]
    pub const fn session_id(&self) -> SessionId {
        self.session_id
    }

    #[must_use]
    pub const fn generation(&self) -> u64 {
        self.generation
    }

    #[must_use]
    pub const fn activity(&self) -> layerx_agent_api::identity::NativeActivity {
        self.activity
    }

    pub fn revalidate(
        &self,
        store: &Store,
        registry: &SessionRegistry,
        token: &Token,
        core_sequence: u64,
    ) -> Result<(), SessionError> {
        if token.tenant() != &self.tenant || token.agent() != &self.agent {
            return Err(SessionError::WrongPrincipal);
        }
        if token.session_id() != self.session_id || token.generation() != self.generation {
            return Err(SessionError::Revoked);
        }
        admit_native(
            store, registry, token, &self.tenant, &self.agent, self.activity, core_sequence,
        )?;
        Ok(())
    }
}

fn native_scope_key(tenant: &TenantId, session_id: SessionId) -> Result<TenantKey, SessionError> {
    let mut object_id = NATIVE_SCOPE_KEY_PREFIX.to_vec();
    object_id.extend_from_slice(&session_id.0);
    Ok(TenantKey::new(tenant.clone(), ObjectKind::Configuration, object_id)?)
}

pub(crate) fn stage_native_scope_install(
    store: &Store,
    registry: &SessionRegistry,
    owner: &crate::human_runtime::VerifiedNativeOwnerV1,
    scope: &NativeSessionScopeV1,
    core_sequence: u64,
    core_time_ms: u64,
) -> Result<(TenantKey, Vec<u8>), SessionError> {
    if owner.tenant() != &scope.tenant
        || owner.agent() != &scope.agent
        || owner.session_id() != scope.session_id
    {
        return Err(SessionError::WrongPrincipal);
    }
    if owner.generation() != scope.generation || owner.head_sequence() != core_sequence {
        return Err(SessionError::Revoked);
    }
    let record = registry.get(&scope.tenant, scope.session_id).ok_or(SessionError::NotFound)?;
    if record.request.tenant != scope.tenant
        || record.request.agent != scope.agent
        || record.request.session_id != scope.session_id
    {
        return Err(SessionError::WrongPrincipal);
    }
    if !record.open || record.generation != scope.generation {
        return Err(SessionError::Revoked);
    }
    if core_sequence >= record.request.expiry_sequence || !record.public_expiry_within(core_time_ms)? {
        return Err(SessionError::Expired);
    }
    let key = native_scope_key(&scope.tenant, scope.session_id)?;
    let encoded = scope.encode()?;
    if let Some(existing) = store.get(&key) {
        if existing.class() != crate::store::StorageClass::LocalOnly {
            return Err(SessionError::ScopeDenied);
        }
        if existing.bytes() != encoded.as_slice() {
            return Err(SessionError::IdentityMismatch);
        }
    }
    Ok((key, encoded))
}

fn authenticated_native_scope(
    store: &Store,
    registry: &SessionRegistry,
    token: &Token,
    tenant: &TenantId,
    agent: &Did,
    core_sequence: u64,
) -> Result<NativeSessionScopeV1, SessionError> {
    if token.tenant() != tenant || token.agent() != agent {
        return Err(SessionError::WrongPrincipal);
    }
    token.authorize(registry, tenant, agent, "prepare", core_sequence)?;
    let record = registry.get(tenant, token.session_id()).ok_or(SessionError::Revoked)?;
    if &record.request.tenant != tenant
        || &record.request.agent != agent
        || record.request.session_id != token.session_id()
    {
        return Err(SessionError::WrongPrincipal);
    }
    if core_sequence >= token.expiry_sequence || core_sequence >= record.request.expiry_sequence {
        return Err(SessionError::Expired);
    }
    if !record.request.scopes.contains("prepare") {
        return Err(SessionError::ScopeDenied);
    }
    let value = store.get(&native_scope_key(tenant, token.session_id())?)
        .ok_or(SessionError::ScopeDenied)?;
    if value.class() != crate::store::StorageClass::LocalOnly {
        return Err(SessionError::ScopeDenied);
    }
    let scope = NativeSessionScopeV1::decode(value.bytes())?;
    if &scope.tenant != tenant || &scope.agent != agent || scope.session_id != token.session_id() {
        return Err(SessionError::WrongPrincipal);
    }
    if scope.generation != token.generation() || scope.generation != record.generation {
        return Err(SessionError::Revoked);
    }
    Ok(scope)
}

pub fn admit_native(
    store: &Store,
    registry: &SessionRegistry,
    token: &Token,
    tenant: &TenantId,
    agent: &Did,
    activity: layerx_agent_api::identity::NativeActivity,
    core_sequence: u64,
) -> Result<NativeSessionAuthorizationV1, SessionError> {
    let scope = authenticated_native_scope(store, registry, token, tenant, agent, core_sequence)?;
    let activity = activity.validate().map_err(|_| SessionError::ScopeDenied)?;
    if !scope.permitted_activities.contains(&activity) {
        return Err(SessionError::ScopeDenied);
    }
    Ok(NativeSessionAuthorizationV1 {
        tenant: scope.tenant,
        agent: scope.agent,
        session_id: scope.session_id,
        generation: scope.generation,
        activity,
    })
}

pub fn restrict_native_scope(
    store: &mut Store,
    registry: &mut SessionRegistry,
    token: &Token,
    token_id: [u8; 32],
    permitted_activities: BTreeSet<layerx_agent_api::identity::NativeActivity>,
    core_sequence: u64,
) -> Result<Token, SessionError> {
    let existing = authenticated_native_scope(
        store, registry, token, token.tenant(), token.agent(), core_sequence,
    )?;
    if !permitted_activities.is_subset(&existing.permitted_activities) {
        return Err(SessionError::ScopeDenied);
    }
    let current = registry.get(token.tenant(), token.session_id()).ok_or(SessionError::Revoked)?;
    let (session_ref, narrowed) = narrowed_record(
        registry,
        token.tenant(),
        token.session_id(),
        token_id,
        current.request.scopes.clone(),
        current.request.permitted_activity_types.clone(),
    )?;
    let scope = NativeSessionScopeV1 {
        generation: narrowed.generation,
        permitted_activities,
        ..existing
    };
    store.update_local_batch(vec![
        (session_key(&narrowed.request)?, encode(&narrowed)?),
        (native_scope_key(&scope.tenant, scope.session_id)?, scope.encode()?),
    ])?;
    let replacement = mint(&narrowed);
    registry.replace(&session_ref, narrowed);
    Ok(replacement)
}

#[cfg(test)]
mod tests {
    use super::*;

    const PRESENCE_OFFSET: usize = 6 + 32 + 32 + 8;

    fn must<T, E: fmt::Debug>(value: Result<T, E>) -> T {
        value.unwrap_or_else(|error| panic!("session record: {error:?}"))
    }

    fn tenant() -> TenantId {
        must(TenantId::new("tenant-a"))
    }

    fn record(expiry_seconds: Option<u64>) -> SessionRecord {
        SessionRecord {
            request: OpenRequest {
                session_id: SessionId([1; 32]),
                token_id: [2; 32],
                tenant: tenant(),
                agent: must(Did::new(b"agent-a")),
                authority: ProtocolAuthority::SessionKey([3; 32]),
                permitted_activity_types: BTreeSet::from([5]),
                scopes: BTreeSet::from(["prepare".to_owned()]),
                expiry_sequence: 100,
                expiry_seconds,
                opening_client: "session-suite".to_owned(),
                policy_version: "policy-v1".to_owned(),
            },
            open: true,
            sequence: 0,
            budget_reserved: 0,
            subscription_cursor: 0,
            generation: FIRST_GENERATION,
            retired_token_ids: BTreeSet::from([[4; 32]]),
        }
    }

    #[test]
    fn public_expiry_round_trips_in_the_current_version() {
        for expiry_seconds in [Some(1_900_000_000), None] {
            let original = record(expiry_seconds);
            let bytes = must(encode(&original));
            assert_eq!(&bytes[..6], RECORD_VERSION);
            assert_eq!(must(decode(&bytes, tenant())), original);
        }
    }

    #[test]
    fn previous_version_decodes_without_public_expiry() {
        let original = record(None);
        let mut bytes = must(encode(&original));
        bytes[..6].copy_from_slice(PREVIOUS_RECORD_VERSION);
        assert_eq!(bytes.remove(PRESENCE_OFFSET), 0);
        let restored = must(decode(&bytes, tenant()));
        assert_eq!(restored.request.expiry_seconds, None);
        assert_eq!(restored, original);
    }

    #[test]
    fn malformed_public_expiry_is_refused() {
        let expected = Err(SessionError::MissingField("expiry_seconds"));
        let mut unknown_presence = must(encode(&record(None)));
        unknown_presence[PRESENCE_OFFSET] = 2;
        assert_eq!(decode(&unknown_presence, tenant()), expected);
        let zero = must(encode(&record(Some(0))));
        assert_eq!(decode(&zero, tenant()), expected);
        let mut previous_with_expiry = must(encode(&record(Some(7))));
        previous_with_expiry[..6].copy_from_slice(PREVIOUS_RECORD_VERSION);
        assert!(decode(&previous_with_expiry, tenant()).is_err());
        let mut truncated = must(encode(&record(Some(7))));
        truncated.truncate(PRESENCE_OFFSET + 4);
        assert!(decode(&truncated, tenant()).is_err());
    }

    #[test]
    fn public_expiry_compares_seconds_to_core_time() {
        let bounded = record(Some(10));
        assert_eq!(bounded.public_expiry_within(9_999), Ok(true));
        assert_eq!(bounded.public_expiry_within(10_000), Ok(false));
        assert_eq!(bounded.public_expiry_within(u64::MAX), Ok(false));
        let missing = Err(SessionError::MissingField("expiry_seconds"));
        assert_eq!(record(None).public_expiry_within(0), missing);
        assert_eq!(record(Some(u64::MAX)).public_expiry_within(0), missing);
    }

    fn amend_fixture(name: &str) -> (std::path::PathBuf, Store, SessionRegistry) {
        let root =
            std::env::temp_dir().join(format!("lxp-amend-expiry-{name}-{}", std::process::id()));
        let _ = std::fs::remove_dir_all(&root);
        let mut store = must(Store::open(root.join("store")));
        let original = record(None);
        must(persist_record(&mut store, &original));
        let mut registry = SessionRegistry::default();
        registry.records.insert(
            SessionRef::new(tenant(), original.request.session_id),
            original,
        );
        (root, store, registry)
    }

    #[test]
    fn amend_moves_absent_public_expiry_to_present_once() {
        let (root, mut store, mut registry) = amend_fixture("once");
        must(amend_public_expiry(
            &mut store,
            &mut registry,
            &tenant(),
            SessionId([1; 32]),
            1_900_000_000,
        ));
        let amended = registry
            .get(&tenant(), SessionId([1; 32]))
            .unwrap_or_else(|| panic!("amended session"));
        assert_eq!(amended.request.expiry_seconds, Some(1_900_000_000));
        assert_eq!(amended.request.expiry_sequence, 100);
        let mut restored = SessionRegistry::default();
        must(restored.restore_tenant(&store, &tenant()));
        assert_eq!(
            restored
                .get(&tenant(), SessionId([1; 32]))
                .map(|record| record.request.expiry_seconds),
            Some(Some(1_900_000_000))
        );
        assert_eq!(
            amend_public_expiry(
                &mut store,
                &mut registry,
                &tenant(),
                SessionId([1; 32]),
                1_900_000_001,
            ),
            Err(SessionError::IdentityMismatch)
        );
        drop(store);
        let _ = std::fs::remove_dir_all(root);
    }

    #[test]
    fn amend_refuses_zero_and_unknown_sessions() {
        let (root, mut store, mut registry) = amend_fixture("refused");
        assert_eq!(
            amend_public_expiry(&mut store, &mut registry, &tenant(), SessionId([1; 32]), 0),
            Err(SessionError::MissingField("expiry_seconds"))
        );
        assert_eq!(
            amend_public_expiry(&mut store, &mut registry, &tenant(), SessionId([9; 32]), 7),
            Err(SessionError::NotFound)
        );
        assert_eq!(
            registry
                .get(&tenant(), SessionId([1; 32]))
                .map(|record| record.request.expiry_seconds),
            Some(None)
        );
        drop(store);
        let _ = std::fs::remove_dir_all(root);
    }
}

#[cfg(test)]
mod native_scope_tests {
    use super::*;
    use layerx_agent_api::identity::NativeActivity;
    use layerx_types::payload::{ActivityType, ModuleId};

    fn must<T, E: fmt::Debug>(value: Result<T, E>) -> T {
        value.unwrap_or_else(|error| panic!("native session: {error:?}"))
    }

    fn activity(module: ModuleId, ordinal: u16) -> NativeActivity {
        NativeActivity::from(must(ActivityType::new(module, ordinal)))
    }

    fn scope() -> NativeSessionScopeV1 {
        NativeSessionScopeV1 {
            tenant: must(TenantId::new("tenant-native")),
            agent: must(Did::new(b"agent-native")),
            session_id: SessionId([11; 32]),
            generation: FIRST_GENERATION,
            permitted_activities: BTreeSet::from([
                activity(ModuleId::Asset, 1),
                activity(ModuleId::Budget, 1),
            ]),
        }
    }

    fn fixture(name: &str) -> (std::path::PathBuf, Store, SessionRegistry, Token) {
        let root = std::env::temp_dir()
            .join(format!("lxp-native-scope-{name}-{}", std::process::id()));
        let _ = std::fs::remove_dir_all(&root);
        let mut store = must(Store::open(root.join("store")));
        let scope = scope();
        let record = SessionRecord {
            request: OpenRequest {
                session_id: scope.session_id,
                token_id: [12; 32],
                tenant: scope.tenant.clone(),
                agent: scope.agent.clone(),
                authority: ProtocolAuthority::SessionKey([13; 32]),
                permitted_activity_types: BTreeSet::from([1]),
                scopes: BTreeSet::from(["prepare".to_owned()]),
                expiry_sequence: 100,
                expiry_seconds: Some(200),
                opening_client: "native-scope-suite".to_owned(),
                policy_version: "v1".to_owned(),
            },
            open: true,
            sequence: 0,
            budget_reserved: 0,
            subscription_cursor: 0,
            generation: FIRST_GENERATION,
            retired_token_ids: BTreeSet::new(),
        };
        must(persist_record(&mut store, &record));
        let mut registry = SessionRegistry::default();
        must(registry.restore_tenant(&store, &scope.tenant));
        let token = must(registry.authenticate_bearer(&scope.tenant, scope.session_id, [12; 32]));
        (root, store, registry, token)
    }

    fn store_scope(store: &mut Store, value: &NativeSessionScopeV1) {
        must(store.put_local(
            must(native_scope_key(&value.tenant, value.session_id)),
            must(value.encode()),
        ));
    }

    fn admitted(
        store: &Store,
        registry: &SessionRegistry,
        token: &Token,
        activity: NativeActivity,
    ) -> Result<NativeSessionAuthorizationV1, SessionError> {
        admit_native(store, registry, token, token.tenant(), token.agent(), activity, 10)
    }

    #[test]
    fn native_codec_refuses_version_trailing_truncation_and_noncanonical_sets() {
        let value = scope();
        let encoded = must(value.encode());
        assert_eq!(must(NativeSessionScopeV1::decode(&encoded)), value);
        let mut version = encoded.clone();
        version[5] = b'2';
        assert_eq!(NativeSessionScopeV1::decode(&version),
            Err(SessionError::MissingField("native_scope_version")));
        let mut trailing = encoded.clone();
        trailing.push(0);
        assert_eq!(NativeSessionScopeV1::decode(&trailing),
            Err(SessionError::MissingField("native_scope_trailing_bytes")));
        for end in 0..encoded.len() {
            assert!(NativeSessionScopeV1::decode(&encoded[..end]).is_err());
        }
        let mut duplicate = encoded.clone();
        let end = duplicate.len();
        duplicate.copy_within(end - 10..end - 5, end - 5);
        assert_eq!(NativeSessionScopeV1::decode(&duplicate),
            Err(SessionError::MissingField("native_activity_order")));
        let mut reversed = encoded;
        let end = reversed.len();
        reversed[end - 10..].rotate_left(5);
        assert!(NativeSessionScopeV1::decode(&reversed).is_err());
        let mut zero_generation = must(value.encode());
        let generation_at = zero_generation.len() - 10 - 2 - 8;
        zero_generation[generation_at..generation_at + 8].fill(0);
        assert!(NativeSessionScopeV1::decode(&zero_generation).is_err());
        let mut invalid_module = must(value.encode());
        let end = invalid_module.len();
        invalid_module[end - 9..end - 7].fill(0);
        assert!(NativeSessionScopeV1::decode(&invalid_module).is_err());
        let mut invalid = value.clone();
        invalid.generation = 0;
        assert!(invalid.encode().is_err());
        invalid = value;
        invalid.permitted_activities.insert(NativeActivity { module: 0, ordinal: 1 });
        assert!(invalid.encode().is_err());
    }

    #[test]
    fn legacy_scope_confers_no_native_authority_and_modules_do_not_collide() {
        let (root, mut store, registry, token) = fixture("collision");
        let asset = activity(ModuleId::Asset, 1);
        let budget = activity(ModuleId::Budget, 1);
        assert_eq!(admitted(&store, &registry, &token, asset), Err(SessionError::ScopeDenied));
        let mut value = scope();
        value.permitted_activities = BTreeSet::from([asset]);
        store_scope(&mut store, &value);
        assert!(decode(&must(value.encode()), value.tenant.clone()).is_err());
        let legacy = registry.get(token.tenant(), token.session_id())
            .unwrap_or_else(|| panic!("current legacy record"));
        assert!(NativeSessionScopeV1::decode(&must(encode(legacy))).is_err());
        let authorization = must(admitted(&store, &registry, &token, asset));
        assert_eq!(authorization.tenant(), token.tenant());
        assert_eq!(authorization.agent(), token.agent());
        assert_eq!(authorization.session_id(), token.session_id());
        assert_eq!(authorization.generation(), token.generation());
        assert_eq!(authorization.activity(), asset);
        assert_eq!(admitted(&store, &registry, &token, budget), Err(SessionError::ScopeDenied));
        let mut restored = SessionRegistry::default();
        must(restored.restore_tenant(&store, token.tenant()));
        assert!(admitted(&store, &restored, &token, asset).is_ok());
        drop(store);
        let _ = std::fs::remove_dir_all(root);
    }

    #[test]
    fn native_narrowing_is_durable_rotates_generation_and_refuses_widening() {
        let (root, mut store, mut registry, token) = fixture("narrow");
        store_scope(&mut store, &scope());
        let asset = activity(ModuleId::Asset, 1);
        let budget = activity(ModuleId::Budget, 1);
        let authorization = must(admitted(&store, &registry, &token, asset));
        assert_eq!(authorization.revalidate(&store, &registry, &token, 10), Ok(()));
        assert_eq!(authorization.revalidate(&store, &registry, &token, 100),
            Err(SessionError::Expired));
        let replacement = must(restrict_native_scope(
            &mut store, &mut registry, &token, [14; 32], BTreeSet::from([asset]), 10,
        ));
        assert_eq!(authorization.revalidate(&store, &registry, &token, 10),
            Err(SessionError::Revoked));
        assert_eq!(authorization.revalidate(&store, &registry, &replacement, 10),
            Err(SessionError::Revoked));
        assert_eq!(replacement.generation(), token.generation() + 1);
        assert_eq!(admitted(&store, &registry, &token, asset), Err(SessionError::Revoked));
        assert!(admitted(&store, &registry, &replacement, asset).is_ok());
        assert_eq!(admitted(&store, &registry, &replacement, budget), Err(SessionError::ScopeDenied));
        assert_eq!(restrict_native_scope(
            &mut store, &mut registry, &replacement, [15; 32], BTreeSet::from([asset, budget]), 10,
        ), Err(SessionError::ScopeDenied));
        assert_eq!(restrict_native_scope(
            &mut store, &mut registry, &replacement, [12; 32], BTreeSet::from([asset]), 10,
        ), Err(SessionError::TokenReuse));
        drop(store);
        let mut store = must(Store::open(root.join("store")));
        let mut registry = SessionRegistry::default();
        must(registry.restore_tenant(&store, replacement.tenant()));
        assert!(admitted(&store, &registry, &replacement, asset).is_ok());
        assert_eq!(admitted(&store, &registry, &replacement, budget), Err(SessionError::ScopeDenied));
        let denied = must(restrict_native_scope(
            &mut store, &mut registry, &replacement, [15; 32], BTreeSet::new(), 10,
        ));
        assert_eq!(admitted(&store, &registry, &denied, asset), Err(SessionError::ScopeDenied));
        drop(store);
        let _ = std::fs::remove_dir_all(root);
    }

    #[test]
    fn native_admission_requires_server_owned_prepare_scope() {
        let (root, mut store, mut registry, token) = fixture("operation-scope");
        store_scope(&mut store, &scope());
        let asset = activity(ModuleId::Asset, 1);
        let authorization = must(admitted(&store, &registry, &token, asset));
        let replacement = must(restrict_scope(
            &mut store,
            &mut registry,
            token.tenant(),
            token.session_id(),
            [14; 32],
            BTreeSet::from(["prepare".to_owned()]),
            BTreeSet::from([1]),
        ));
        let mut current = registry.get(replacement.tenant(), replacement.session_id())
            .unwrap_or_else(|| panic!("current session"))
            .clone();
        current.request.scopes = BTreeSet::from(["read".to_owned()]);
        must(persist_record(&mut store, &current));
        registry.replace(&SessionRef::new(current.request.tenant.clone(), current.request.session_id), current);
        let read_token = must(registry.authenticate(&replacement.credential()));
        let mut grant = scope();
        grant.generation = read_token.generation();
        store_scope(&mut store, &grant);
        assert_eq!(admitted(&store, &registry, &read_token, asset), Err(SessionError::ScopeDenied));
        assert_eq!(admitted(&store, &registry, &replacement, asset), Err(SessionError::ScopeDenied));
        assert_eq!(authorization.revalidate(&store, &registry, &read_token, 10),
            Err(SessionError::Revoked));
        assert_eq!(restrict_native_scope(
            &mut store, &mut registry, &read_token, [15; 32], BTreeSet::new(), 10,
        ), Err(SessionError::ScopeDenied));
        drop(store);
        let _ = std::fs::remove_dir_all(root);
    }

    #[test]
    fn native_admission_refuses_wrong_principal_expiry_and_generation() {
        let (root, mut store, mut registry, token) = fixture("binding");
        let asset = activity(ModuleId::Asset, 1);
        let value = scope();
        store_scope(&mut store, &value);
        assert_eq!(admit_native(&store, &registry, &token,
            &must(TenantId::new("other-tenant")), token.agent(), asset, 10),
            Err(SessionError::WrongPrincipal));
        assert_eq!(admit_native(&store, &registry, &token,
            token.tenant(), &must(Did::new(b"other-agent")), asset, 10),
            Err(SessionError::WrongPrincipal));
        assert_eq!(admit_native(&store, &registry, &token,
            token.tenant(), token.agent(), asset, 100), Err(SessionError::Expired));
        for field in 0..4 {
            let mut mismatched = value.clone();
            match field {
                0 => mismatched.generation += 1,
                1 => mismatched.agent = must(Did::new(b"other-agent")),
                2 => mismatched.session_id = SessionId([19; 32]),
                _ => mismatched.tenant = must(TenantId::new("other-tenant")),
            }
            must(store.put_local(must(native_scope_key(token.tenant(), token.session_id())),
                must(mismatched.encode())));
            assert_eq!(admitted(&store, &registry, &token, asset),
                Err(if field == 0 { SessionError::Revoked } else { SessionError::WrongPrincipal }));
        }
        store_scope(&mut store, &value);
        must(close(&mut store, &mut registry, token.tenant(), token.session_id()));
        assert_eq!(admitted(&store, &registry, &token, asset), Err(SessionError::Revoked));
        drop(store);
        let _ = std::fs::remove_dir_all(root);
    }
}
