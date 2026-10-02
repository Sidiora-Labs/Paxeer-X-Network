//! Shared ordering authority for session-gated daemon effects.

use std::collections::{BTreeMap, BTreeSet};
use std::sync::{Arc, Mutex, RwLock};

use crate::budget::{
    self as budget, BudgetLimiter, CoreTimestampMs, DurableBudgetReservation, LimitId,
    LimitRefusal, ReleaseKind, ReservationRequest,
};
use crate::events::outbound::StopSignal;
use crate::events::subscription::Termination;
use crate::human::{HumanOperationError, HumanResponse};
use crate::managed_agent;
use crate::prepare::{
    DurablePreparation, LifecycleError, LifecycleState, PreparationAuthorization,
    PreparationExtension, PreparationInvalidationReport, PreparationLifecycle, Prepared,
    EXTENSION_IDEMPOTENCY, EXTENSION_OUTCOME,
};
use crate::session::{
    self, InvalidationReport, PendingActivity, SessionCredential, SessionError, SessionId,
    SessionRegistry, Token,
};
use crate::store::{ObjectKind, Store, TenantId, TenantKey};
use crate::tenant::{
    self, AuthorizationError, ObjectOwner, Operation, RequestContext, ResolvedPrincipal, Surface,
    TenantObservability,
};

/// The one shared owner of durable session state, its in-memory index, and exact-generation
/// operation tracking. Lock order is always registry, then store, then lifecycle/budget state.
#[derive(Clone)]
pub struct SessionControl {
    store: Arc<Mutex<Store>>,
    registry: Arc<RwLock<SessionRegistry>>,
    lifecycle: Arc<PreparationLifecycle>,
    budgets: Arc<BudgetLimiter>,
    observability: Arc<Mutex<TenantObservability>>,
    pending_invalidations: Arc<Mutex<BTreeMap<(session::SessionRef, u64), u64>>>,
}

pub(crate) struct AuthenticatedOwnerLookup {
    credential: SessionCredential,
    operation: Operation,
    surface: Surface,
    principal: ResolvedPrincipal,
    binding: Arc<()>,
}

impl AuthenticatedOwnerLookup {
    pub(crate) fn principal(&self) -> &ResolvedPrincipal {
        &self.principal
    }

    pub(crate) fn operation(&self) -> Operation {
        self.operation
    }

    pub(crate) fn binding(&self) -> Arc<()> {
        Arc::clone(&self.binding)
    }
}

impl SessionControl {
    #[must_use]
    pub fn new(
        store: Arc<Mutex<Store>>,
        registry: SessionRegistry,
        lifecycle: Arc<PreparationLifecycle>,
        budgets: Arc<BudgetLimiter>,
    ) -> Self {
        Self {
            store,
            registry: Arc::new(RwLock::new(registry)),
            lifecycle,
            budgets,
            observability: Arc::new(Mutex::new(TenantObservability::default())),
            pending_invalidations: Arc::new(Mutex::new(BTreeMap::new())),
        }
    }

    /// Receipt hook: settles one durable preparation exactly once. `Executed` moves the held
    /// amounts into each daemon limit's consumed total in the same store write that marks the
    /// record executed; `Unknown` never settles.
    ///
    /// # Errors
    ///
    /// Returns an error if the record is absent or corrupt, the durable write fails, or the
    /// in-memory release fails after the durable settlement.
    pub fn settle_write(
        &self,
        tenant: &TenantId,
        preparation_id: [u8; 32],
        outcome: ReleaseKind,
        current_sequence: u64,
    ) -> Result<bool, SessionControlError> {
        self.settle_inner(tenant, preparation_id, outcome, current_sequence, None)
    }

    /// Cancels one unsettled preparation: the `Failed` state and the replacement outcome
    /// extension (`EXTENSION_OUTCOME` only) are one store write, followed by the hold release.
    /// A record already terminal or already sent (`Submitted`, `Acknowledged`, `Unknown`) is left
    /// unchanged and returns `Ok(false)`.
    ///
    /// # Errors
    ///
    /// Returns `InvalidSignedBytes` for any other extension tag, plus every `settle_write` error.
    pub fn cancel_write(
        &self,
        tenant: &TenantId,
        preparation_id: [u8; 32],
        outcome: PreparationExtension,
        current_sequence: u64,
    ) -> Result<bool, SessionControlError> {
        if outcome.tag != EXTENSION_OUTCOME {
            return Err(SessionControlError::Lifecycle(
                LifecycleError::InvalidSignedBytes,
            ));
        }
        self.settle_inner(
            tenant,
            preparation_id,
            ReleaseKind::Failed,
            current_sequence,
            Some(outcome),
        )
    }

    fn settle_inner(
        &self,
        tenant: &TenantId,
        preparation_id: [u8; 32],
        outcome: ReleaseKind,
        current_sequence: u64,
        extension: Option<PreparationExtension>,
    ) -> Result<bool, SessionControlError> {
        let lifecycle = SessionControlError::Lifecycle;
        let state = match outcome {
            ReleaseKind::Executed => LifecycleState::Executed,
            ReleaseKind::Failed => LifecycleState::Failed,
            ReleaseKind::Expired => LifecycleState::Expired,
            ReleaseKind::Unknown => return Ok(false),
        };
        let mut store = self
            .store
            .lock()
            .map_err(|_| SessionControlError::Unavailable)?;
        let key = DurablePreparation::store_key(tenant, preparation_id).map_err(lifecycle)?;
        let stored = store
            .get(&key)
            .ok_or(lifecycle(LifecycleError::NotFound))?
            .bytes
            .clone();
        let mut record = DurablePreparation::decode(tenant.clone(), &stored).map_err(lifecycle)?;
        if record.terminal()
            || (extension.is_some()
                && matches!(
                    record.state,
                    LifecycleState::Submitted
                        | LifecycleState::Acknowledged
                        | LifecycleState::Unknown
                ))
        {
            return Ok(false);
        }
        if record.extensions.contains_key(&6)
            && (outcome == ReleaseKind::Executed || (outcome == ReleaseKind::Failed && record.activity_id.is_some())) {
            return Ok(false);
        }
        record.state = state;
        record.drop_signed_bytes();
        if let Some(extension) = extension {
            record.extensions.insert(extension.tag, extension.bytes);
        }
        let mut updates = vec![(key, record.encode().map_err(lifecycle)?)];
        if outcome == ReleaseKind::Executed {
            if let Some(encoded) = record.extensions.get(&6) {
                let reservation = budget::ProgramBudgetReservation::decode(encoded)
                    .map_err(|_| SessionControlError::Unavailable)?;
                if reservation.id != preparation_id || !record.holds.is_empty() {
                    return Err(SessionControlError::Unavailable);
                }
                updates.extend(budget::program_consumption_updates(&store, tenant, &reservation)
                    .map_err(|_| SessionControlError::Unavailable)?);
            } else {
                let holds: Vec<DurableBudgetReservation> = record.holds.iter().map(|(hold, _)| hold.clone()).collect();
                updates.extend(budget::consumption_updates(&store, tenant, &holds)
                    .map_err(|_| SessionControlError::Unavailable)?);
            }
        }
        let staged =
            budget::stage_release(&self.budgets, preparation_id, outcome, current_sequence)
                .map_err(|refusal| lifecycle(LifecycleError::Reservation(refusal)))?;
        store
            .update_local_batch(updates)
            .map_err(|_| SessionControlError::Unavailable)?;
        Ok(staged.publish())
    }

    /// Receipt-path lookup: the one unsettled preparation published for `idempotency_key`.
    ///
    /// # Errors
    ///
    /// Returns `Duplicate` when more than one unsettled record carries the key, or a corrupt
    /// record error.
    pub fn preparation_for_idempotency_key(
        &self,
        tenant: &TenantId,
        idempotency_key: [u8; 32],
    ) -> Result<Option<[u8; 32]>, SessionControlError> {
        let store = self
            .store
            .lock()
            .map_err(|_| SessionControlError::Unavailable)?;
        live_preparation_for_key(&store, tenant, idempotency_key)
    }

    /// Restores every non-terminal durable preparation, its in-memory lifecycle entry and its
    /// holds, before writes are admitted.
    ///
    /// # Errors
    ///
    /// Returns an error if a durable record is corrupt or a hold cannot be restored.
    pub fn restore_writes(&self) -> Result<usize, SessionControlError> {
        let lifecycle = SessionControlError::Lifecycle;
        let store = self
            .store
            .lock()
            .map_err(|_| SessionControlError::Unavailable)?;
        let mut records = Vec::new();
        for tenant in store.tenant_ids_for_kind(ObjectKind::Configuration) {
            records.extend(DurablePreparation::load_all(&store, &tenant).map_err(lifecycle)?);
        }
        let holds: Vec<_> = records
            .iter()
            .filter(|record| !record.terminal())
            .flat_map(|record| record.holds.iter().cloned())
            .collect();
        self.lifecycle
            .restore_durable(&records)
            .map_err(lifecycle)?;
        budget::restore_bounded(&self.budgets, &holds)
            .map_err(|refusal| lifecycle(LifecycleError::Reservation(refusal)))?;
        let mut count = holds.len();
        for record in records.iter().filter(|record| !record.terminal()) {
            if let Some(encoded) = record.extensions.get(&6) {
                let reservation = budget::ProgramBudgetReservation::decode(encoded)
                    .map_err(|_| SessionControlError::Unavailable)?;
                if reservation.id != record.preparation_id || !record.holds.is_empty() {
                    return Err(SessionControlError::Unavailable);
                }
                self.budgets.restore_program_reservation(&reservation)
                    .map_err(|refusal| lifecycle(LifecycleError::Reservation(refusal)))?;
                count = count.checked_add(reservation.holds.len()).ok_or(SessionControlError::Unavailable)?;
            }
        }
        Ok(count)
    }

    #[must_use]
    pub fn registry(&self) -> Arc<RwLock<SessionRegistry>> {
        Arc::clone(&self.registry)
    }

    #[must_use]
    pub fn store(&self) -> Arc<Mutex<Store>> {
        Arc::clone(&self.store)
    }

    /// Authenticates an exact external credential, resolves its generated operation, and arms an
    /// exact-generation stop before any effect is allowed to begin.
    ///
    /// # Errors
    ///
    /// Returns an error if session authorization, durable state, or preparation invalidation fails.
    pub fn authorize(
        &self,
        credential: &SessionCredential,
        operation: Operation,
        surface: Surface,
        core_sequence: u64,
        target_owner: Option<ObjectOwner>,
    ) -> Result<OperationPermit, SessionControlError> {
        self.retry_preparation_invalidations()?;
        let mut registry = self
            .registry
            .write()
            .map_err(|_| SessionControlError::Unavailable)?;
        let (token, request, principal) = self.resolve_credential(
            &registry, credential, operation, surface, core_sequence, target_owner,
        )?;
        let stop = registry
            .revocation_stop(&token)
            .map_err(SessionControlError::Session)?;
        Ok(OperationPermit {
            token,
            request,
            principal,
            stop,
            lookup_binding: None,
        })
    }

    fn resolve_credential(
        &self,
        registry: &SessionRegistry,
        credential: &SessionCredential,
        operation: Operation,
        surface: Surface,
        core_sequence: u64,
        target_owner: Option<ObjectOwner>,
    ) -> Result<(Token, RequestContext, ResolvedPrincipal), SessionControlError> {
        let token = registry
            .authenticate(credential)
            .map_err(SessionControlError::Session)?;
        let request = RequestContext {
            surface,
            operation,
            core_sequence,
            supplied_header_tenant: None,
            supplied_body_tenant: None,
            target_owner,
        };
        let principal = {
            let mut observability = self
                .observability
                .lock()
                .map_err(|_| SessionControlError::Unavailable)?;
            tenant::resolve(&token, registry, &request, &mut observability)
                .map_err(SessionControlError::Authorization)?
        };
        Ok((token, request, principal))
    }

    pub(crate) fn authenticate_lookup(
        &self,
        credential: &SessionCredential,
        operation: Operation,
        surface: Surface,
        core_sequence: u64,
    ) -> Result<AuthenticatedOwnerLookup, SessionControlError> {
        let registry = self.registry.read().map_err(|_| SessionControlError::Unavailable)?;
        let (_, _, principal) = self.resolve_credential(
            &registry, credential, operation, surface, core_sequence, None,
        )?;
        Ok(AuthenticatedOwnerLookup {
            credential: credential.clone(),
            operation,
            surface,
            principal,
            binding: Arc::new(()),
        })
    }

    pub(crate) fn authorize_resolved(
        &self,
        lookup: AuthenticatedOwnerLookup,
        target: crate::human_runtime::ResolvedTargetOwner,
        current_core_sequence: u64,
    ) -> Result<OperationPermit, SessionControlError> {
        let (binding, owner) = target.into_parts();
        if !Arc::ptr_eq(&binding, &lookup.binding) {
            return Err(SessionControlError::Authorization(AuthorizationError::NotAuthorized));
        }
        let mut permit = self.authorize(
            &lookup.credential, lookup.operation, lookup.surface, current_core_sequence, owner,
        )?;
        permit.lookup_binding = Some(lookup.binding);
        Ok(permit)
    }

    /// Closes one session with durable state committed before registry replacement and stop
    /// publication.
    ///
    /// # Errors
    ///
    /// Returns an error if session authorization, durable state, or preparation invalidation fails.
    pub fn close(
        &self,
        tenant: &TenantId,
        session_id: SessionId,
        current_sequence: u64,
    ) -> Result<PreparationInvalidationReport, SessionControlError> {
        let mut registry = self
            .registry
            .write()
            .map_err(|_| SessionControlError::Unavailable)?;
        let mut store = self
            .store
            .lock()
            .map_err(|_| SessionControlError::Unavailable)?;
        let generation = registry
            .generation(tenant, session_id)
            .ok_or(SessionControlError::Session(SessionError::NotFound))?;
        session::close(&mut store, &mut registry, tenant, session_id)
            .map_err(SessionControlError::Session)?;
        self.invalidate_preparations(
            &[(
                session::SessionRef::new(tenant.clone(), session_id),
                generation,
            )],
            current_sequence,
        )
    }

    ///
    /// # Errors
    ///
    /// Returns an error if session authorization, durable state, or preparation invalidation fails.
    pub fn close_with_companion(
        &self,
        tenant: &TenantId,
        session_id: SessionId,
        current_sequence: u64,
        companion_key: TenantKey,
        companion_bytes: Vec<u8>,
    ) -> Result<PreparationInvalidationReport, SessionControlError> {
        let mut registry = self
            .registry
            .write()
            .map_err(|_| SessionControlError::Unavailable)?;
        let mut store = self
            .store
            .lock()
            .map_err(|_| SessionControlError::Unavailable)?;
        let generation = registry
            .generation(tenant, session_id)
            .ok_or(SessionControlError::Session(SessionError::NotFound))?;
        session::close_with_companion(
            &mut store,
            &mut registry,
            tenant,
            session_id,
            companion_key,
            companion_bytes,
        )
        .map_err(SessionControlError::Session)?;
        self.invalidate_preparations(
            &[(
                session::SessionRef::new(tenant.clone(), session_id),
                generation,
            )],
            current_sequence,
        )
    }

    /// Atomically rotates an opaque bearer while narrowing scope and updating the Human-managed
    /// agent coordinate that distributes the new bearer.
    ///
    /// # Errors
    ///
    /// Returns an error if session authorization, durable state, or preparation invalidation fails.
    pub fn restrict_with_companion(
        &self,
        tenant: &TenantId,
        restriction: session::ScopeRestriction,
        current_sequence: u64,
        coordinate: (TenantKey, Vec<u8>),
        companion: (TenantKey, Vec<u8>),
    ) -> Result<(Token, PreparationInvalidationReport), SessionControlError> {
        let session_id = restriction.session_id;
        let mut registry = self
            .registry
            .write()
            .map_err(|_| SessionControlError::Unavailable)?;
        let generation = registry
            .generation(tenant, session_id)
            .ok_or(SessionControlError::Session(SessionError::NotFound))?;
        let mut store = self
            .store
            .lock()
            .map_err(|_| SessionControlError::Unavailable)?;
        let token = session::restrict_scope_with_companion(
            &mut store,
            &mut registry,
            tenant,
            restriction,
            coordinate,
            companion,
        )
        .map_err(SessionControlError::Session)?;
        let preparations = self.invalidate_preparations(
            &[(
                session::SessionRef::new(tenant.clone(), session_id),
                generation,
            )],
            current_sequence,
        )?;
        Ok((token, preparations))
    }

    /// Human-admin restriction entry point. The replacement bearer is generated inside the
    /// daemon and the session record, managed-agent coordinate, and observation commit together.
    ///
    /// # Errors
    ///
    /// Returns an error if session authorization, durable state, or preparation invalidation fails.
    pub fn restrict_managed_agent(
        &self,
        tenant: &TenantId,
        agent_id: &str,
        scopes: BTreeSet<String>,
        permitted_activity_types: BTreeSet<u16>,
        current_sequence: u64,
        action_key: [u8; 32],
    ) -> Result<(Token, PreparationInvalidationReport, HumanResponse), SessionControlError> {
        let mut registry = self
            .registry
            .write()
            .map_err(|_| SessionControlError::Unavailable)?;
        let mut store = self
            .store
            .lock()
            .map_err(|_| SessionControlError::Unavailable)?;
        let (_, session_id, current_token, coordinate_generation) =
            managed_agent::session_coordinates(&store, tenant, agent_id)
                .map_err(SessionControlError::Human)?;
        let session_id = SessionId(session_id);
        let record = registry
            .get(tenant, session_id)
            .cloned()
            .ok_or(SessionControlError::Session(SessionError::NotFound))?;
        if !record.open
            || record.request.token_id != current_token
            || record.generation != coordinate_generation
        {
            return Err(SessionControlError::Session(SessionError::Revoked));
        }
        if let Some(replay) = managed_agent::replay_session_token_restriction(
            &store,
            tenant,
            agent_id,
            action_key,
            current_sequence,
            &scopes,
            &permitted_activity_types,
        )
        .map_err(SessionControlError::Human)?
        {
            if replay.session_id != session_id.0
                || replay.replacement_token != current_token
                || replay.generation != record.generation
                || record.request.scopes != scopes
                || record.request.permitted_activity_types != permitted_activity_types
            {
                return Err(SessionControlError::Session(SessionError::Revoked));
            }
            let token = registry
                .authenticate_bearer(tenant, session_id, replay.replacement_token)
                .map_err(SessionControlError::Session)?;
            let preparations = self.retry_preparation_invalidations()?;
            return Ok((token, preparations, replay.response));
        }
        let (replacement_generation, replacement_token) =
            replacement_bearer(&record, current_token)?;
        let (
            response,
            coordinate_key,
            coordinate_bytes,
            companion_key,
            companion_bytes,
            ledger_key,
            ledger_bytes,
        ) = managed_agent::prepare_session_token_restriction(
            &store,
            tenant,
            agent_id,
            (
                session_id.0,
                current_token,
                coordinate_generation,
                replacement_token,
                action_key,
                replacement_generation,
                current_sequence,
                &scopes,
                &permitted_activity_types,
            ),
        )
        .map_err(SessionControlError::Human)?;
        let token = session::restrict_scope_with_companions(
            &mut store,
            &mut registry,
            tenant,
            session::ScopeRestriction {
                session_id,
                token_id: replacement_token,
                scopes,
                permitted_activity_types,
            },
            (coordinate_key, coordinate_bytes),
            vec![(companion_key, companion_bytes), (ledger_key, ledger_bytes)],
        )
        .map_err(SessionControlError::Session)?;
        let preparations = self.invalidate_preparations(
            &[(
                session::SessionRef::new(tenant.clone(), session_id),
                record.generation,
            )],
            current_sequence,
        )?;
        Ok((token, preparations, response))
    }

    /// Commits a separately verified finalized authority revocation as one short ordered batch.
    /// The opaque value can only be produced by managed-agent receipt/evidence validation.
    ///
    /// # Errors
    ///
    /// Returns an error if session authorization, durable state, or preparation invalidation fails.
    pub fn invalidate_finalized(
        &self,
        finalized: &managed_agent::ValidatedAuthorityRevocation,
    ) -> Result<(InvalidationReport, PreparationInvalidationReport), SessionControlError> {
        let event = finalized.event();
        let mut registry = self
            .registry
            .write()
            .map_err(|_| SessionControlError::Unavailable)?;
        let mut store = self
            .store
            .lock()
            .map_err(|_| SessionControlError::Unavailable)?;
        let mut detached: [PendingActivity; 0] = [];
        let report =
            session::invalidate_on_revocation(&mut store, &mut registry, &mut detached, event)
                .map_err(SessionControlError::Session)?;
        let preparations =
            self.invalidate_preparations(&report.invalidated_generations, event.observed_sequence)?;
        Ok((report, preparations))
    }

    pub(crate) fn commit_owner_rotation(
        &self,
        tenant: &TenantId,
        agent_id: &str,
        request: &managed_agent::rotation::Projection<'_>,
    ) -> Result<HumanResponse, HumanOperationError> {
        let mut registry = self
            .registry
            .write()
            .map_err(|_| HumanOperationError::Unavailable)?;
        let mut store = self
            .store
            .lock()
            .map_err(|_| HumanOperationError::Unavailable)?;
        match managed_agent::rotation::prepare(&store, &registry, tenant, agent_id, request)? {
            managed_agent::rotation::Prepared::Replay(response) => Ok(response),
            managed_agent::rotation::Prepared::Commit {
                event,
                updates,
                response,
            } => {
                let report =
                    session::invalidate_with_projection(&mut store, &mut registry, &event, updates)
                        .map_err(|_| HumanOperationError::Refused)?;
                drop(store);
                drop(registry);
                self.invalidate_preparations(
                    &report.invalidated_generations,
                    event.observed_sequence,
                )
                .map_err(|_| HumanOperationError::Unavailable)?;
                Ok(response)
            }
        }
    }

    /// Closes the caller's or another session of the same tenant and agent under one registry
    /// write guard. The permit is resolved and the target authorized before any durable
    /// mutation; a successful self-close returns the durable close result and fires the
    /// permit's own exact-generation stop.
    ///
    /// # Errors
    ///
    /// Returns an error if permit resolution, target authorization, durable state, or
    /// preparation invalidation fails.
    pub(crate) fn close_session_authorized(
        &self,
        permit: &OperationPermit,
        target: SessionId,
        current_sequence: u64,
    ) -> Result<PreparationInvalidationReport, SessionControlError> {
        permit.require_operation(Operation::SessionClose)?;
        let mut registry = self
            .registry
            .write()
            .map_err(|_| SessionControlError::Unavailable)?;
        let (tenant, generation) = permit.authorize_target(self, &registry, target)?;
        let mut store = self
            .store
            .lock()
            .map_err(|_| SessionControlError::Unavailable)?;
        session::close(&mut store, &mut registry, &tenant, target)
            .map_err(SessionControlError::Session)?;
        self.invalidate_preparations(
            &[(session::SessionRef::new(tenant, target), generation)],
            current_sequence,
        )
    }

    /// Rotates the bearer of a session owned by the permit's tenant and agent, keeping its
    /// scopes and activity types. The replacement bearer is generated inside the daemon.
    ///
    /// # Errors
    ///
    /// Returns an error if permit resolution, target authorization, durable state, or
    /// preparation invalidation fails.
    pub(crate) fn refresh_session_authorized(
        &self,
        permit: &OperationPermit,
        target: SessionId,
        current_sequence: u64,
    ) -> Result<(Token, PreparationInvalidationReport), SessionControlError> {
        permit.require_operation(Operation::SessionRefresh)?;
        self.rotate_authorized(permit, target, None, current_sequence)
    }

    /// Narrows and rotates a session owned by the permit's tenant and agent.
    ///
    /// # Errors
    ///
    /// Returns an error if permit resolution, target authorization, durable state, or
    /// preparation invalidation fails.
    pub(crate) fn restrict_session_authorized(
        &self,
        permit: &OperationPermit,
        target: SessionId,
        scopes: BTreeSet<String>,
        permitted_activity_types: BTreeSet<u16>,
        current_sequence: u64,
    ) -> Result<(Token, PreparationInvalidationReport), SessionControlError> {
        permit.require_operation(Operation::SessionRefresh)?;
        self.rotate_authorized(
            permit,
            target,
            Some((scopes, permitted_activity_types)),
            current_sequence,
        )
    }

    fn rotate_authorized(
        &self,
        permit: &OperationPermit,
        target: SessionId,
        narrowed: Option<(BTreeSet<String>, BTreeSet<u16>)>,
        current_sequence: u64,
    ) -> Result<(Token, PreparationInvalidationReport), SessionControlError> {
        let mut registry = self
            .registry
            .write()
            .map_err(|_| SessionControlError::Unavailable)?;
        let (tenant, generation) = permit.authorize_target(self, &registry, target)?;
        let record = registry
            .get(&tenant, target)
            .cloned()
            .ok_or(SessionControlError::Session(SessionError::NotFound))?;
        let (_, replacement_token) = replacement_bearer(&record, record.request.token_id)?;
        let (scopes, permitted_activity_types) = narrowed.unwrap_or_else(|| {
            (
                record.request.scopes.clone(),
                record.request.permitted_activity_types.clone(),
            )
        });
        let mut store = self
            .store
            .lock()
            .map_err(|_| SessionControlError::Unavailable)?;
        let token = session::restrict_scope(
            &mut store,
            &mut registry,
            &tenant,
            target,
            replacement_token,
            scopes,
            permitted_activity_types,
        )
        .map_err(SessionControlError::Session)?;
        let preparations = self.invalidate_preparations(
            &[(session::SessionRef::new(tenant, target), generation)],
            current_sequence,
        )?;
        Ok((token, preparations))
    }

    fn invalidate_preparations(
        &self,
        invalidated: &[(session::SessionRef, u64)],
        current_sequence: u64,
    ) -> Result<PreparationInvalidationReport, SessionControlError> {
        {
            let mut pending = self
                .pending_invalidations
                .lock()
                .map_err(|_| SessionControlError::Unavailable)?;
            for (session, generation) in invalidated {
                pending
                    .entry((session.clone(), *generation))
                    .and_modify(|sequence| *sequence = (*sequence).max(current_sequence))
                    .or_insert(current_sequence);
            }
        }
        self.retry_preparation_invalidations()
    }

    /// Retries exact-generation preparation cleanup. Authorization of those preparations never
    /// depends on cleanup succeeding: every transition still requires a current exact permit.
    ///
    /// # Errors
    ///
    /// Returns an error if session authorization, durable state, or preparation invalidation fails.
    pub fn retry_preparation_invalidations(
        &self,
    ) -> Result<PreparationInvalidationReport, SessionControlError> {
        let selected = {
            let pending = self
                .pending_invalidations
                .lock()
                .map_err(|_| SessionControlError::Unavailable)?;
            pending
                .iter()
                .map(|((session, generation), sequence)| (session.clone(), *generation, *sequence))
                .collect::<Vec<_>>()
        };
        if selected.is_empty() {
            return Ok(PreparationInvalidationReport::default());
        }
        let invalidated = selected
            .iter()
            .map(|(session, generation, _)| (session.clone(), *generation))
            .collect::<Vec<_>>();
        let sequence = selected
            .iter()
            .map(|(_, _, sequence)| *sequence)
            .max()
            .ok_or(SessionControlError::Unavailable)?;
        let report = self
            .lifecycle
            .invalidate_authorizations(&invalidated, sequence, &self.budgets)
            .map_err(SessionControlError::Lifecycle)?;
        let mut pending = self
            .pending_invalidations
            .lock()
            .map_err(|_| SessionControlError::Unavailable)?;
        for (session, generation, _) in selected {
            pending.remove(&(session, generation));
        }
        Ok(report)
    }

    /// Fails the not-yet-submitted preparations bound to a revoked capability; submitted and
    /// unknown work is preserved. The durable cleanup record is removed by the caller only after
    /// this succeeds, so a failure is retried from that record after restart.
    ///
    /// # Errors
    ///
    /// Returns an error if preparation invalidation or reservation release fails.
    pub fn invalidate_preparation_ids(
        &self,
        preparation_ids: &BTreeSet<[u8; 32]>,
        current_sequence: u64,
    ) -> Result<PreparationInvalidationReport, SessionControlError> {
        self.lifecycle
            .invalidate_preparations(preparation_ids, current_sequence, &self.budgets)
            .map_err(SessionControlError::Lifecycle)
    }

    /// # Errors
    ///
    /// Returns an error unless the in-memory lifecycle is `Signing` for the record's exact
    /// session generation and the durable record can move to `Signing` in one store write.
    pub fn mark_signing(
        &self,
        tenant: &TenantId,
        preparation_id: [u8; 32],
    ) -> Result<bool, SessionControlError> {
        self.mark_inner(tenant, preparation_id, LifecycleState::Signing)
    }

    /// # Errors
    ///
    /// Returns an error unless the in-memory lifecycle is `Signed` with retained bytes for the
    /// record's exact session generation and the durable record can move to `Signed` with
    /// those bytes in one store write.
    pub fn mark_signed(
        &self,
        tenant: &TenantId,
        preparation_id: [u8; 32],
    ) -> Result<bool, SessionControlError> {
        self.mark_inner(tenant, preparation_id, LifecycleState::Signed)
    }

    fn mark_inner(
        &self,
        tenant: &TenantId,
        preparation_id: [u8; 32],
        next: LifecycleState,
    ) -> Result<bool, SessionControlError> {
        let lifecycle = SessionControlError::Lifecycle;
        let mut store = self
            .store
            .lock()
            .map_err(|_| SessionControlError::Unavailable)?;
        let key = DurablePreparation::store_key(tenant, preparation_id).map_err(lifecycle)?;
        let stored = store
            .get(&key)
            .ok_or(lifecycle(LifecycleError::NotFound))?
            .bytes
            .clone();
        let mut record = DurablePreparation::decode(tenant.clone(), &stored).map_err(lifecycle)?;
        let changed = match next {
            LifecycleState::Submitted | LifecycleState::Acknowledged => {
                self.lifecycle.persist_sent(&mut record, next)
            }
            _ => self.lifecycle.persist_signature(&mut record, next),
        }
        .map_err(lifecycle)?;
        if !changed {
            return Ok(false);
        }
        let bytes = record.encode().map_err(lifecycle)?;
        store
            .update_local_batch(vec![(key, bytes)])
            .map_err(|_| SessionControlError::Unavailable)?;
        Ok(true)
    }

    /// # Errors
    ///
    /// Returns an error unless the in-memory lifecycle is `Submitted` for the record's exact
    /// session generation and the durable `Signed` record can move to `Submitted` in one store
    /// write.
    pub fn mark_submitted(
        &self,
        tenant: &TenantId,
        preparation_id: [u8; 32],
    ) -> Result<bool, SessionControlError> {
        self.mark_inner(tenant, preparation_id, LifecycleState::Submitted)
    }

    /// # Errors
    ///
    /// Returns an error unless the in-memory lifecycle is `Acknowledged` for the record's exact
    /// session generation and the durable `Submitted` record can move to `Acknowledged` in one
    /// store write.
    pub fn mark_acknowledged(
        &self,
        tenant: &TenantId,
        preparation_id: [u8; 32],
    ) -> Result<bool, SessionControlError> {
        self.mark_inner(tenant, preparation_id, LifecycleState::Acknowledged)
    }

    /// # Errors
    ///
    /// Returns an error unless the permit resolves at its exact open, unexpired generation and the capability is a live record of the same tenant and agent.
    pub fn bind_capability(
        &self,
        permit: &OperationPermit,
        capability_id: [u8; 32],
        observed_head_sequence: u64,
    ) -> Result<(), SessionControlError> {
        if capability_id == [0; 32] {
            return Err(SessionControlError::Session(SessionError::MissingField(
                "capability_id",
            )));
        }
        let registry = self
            .registry
            .read()
            .map_err(|_| SessionControlError::Unavailable)?;
        let (tenant, session_id, generation) =
            self.bindable_session(permit, &registry, observed_head_sequence)?;
        let mut store = self
            .store
            .lock()
            .map_err(|_| SessionControlError::Unavailable)?;
        live_capability(&store, &tenant, permit.token.agent(), capability_id)?;
        let key = SessionCapabilityBinding::key(&tenant, session_id)?;
        let existing = store
            .get(&key)
            .map(|stored| SessionCapabilityBinding::decode(&tenant, session_id, stored.bytes()))
            .transpose()?;
        if let Some(existing) = existing {
            if existing.generation == generation {
                return if existing.capability_id == capability_id {
                    Ok(())
                } else {
                    Err(SessionControlError::Session(SessionError::IdentityMismatch))
                };
            }
        }
        let binding = SessionCapabilityBinding {
            tenant,
            session_id,
            capability_id,
            generation,
            bound_at_sequence: observed_head_sequence,
        };
        store
            .put_local(key, binding.encode())
            .map_err(|_| SessionControlError::Unavailable)
    }

    /// # Errors
    ///
    /// Returns an error when the stored association is corrupt, its session is no longer open at the bound generation, or its capability is absent or revoked.
    pub fn capability_binding(
        &self,
        tenant: &TenantId,
        session_id: SessionId,
    ) -> Result<Option<SessionCapabilityBinding>, SessionControlError> {
        let registry = self
            .registry
            .read()
            .map_err(|_| SessionControlError::Unavailable)?;
        let store = self
            .store
            .lock()
            .map_err(|_| SessionControlError::Unavailable)?;
        let Some(stored) = store.get(&SessionCapabilityBinding::key(tenant, session_id)?) else {
            return Ok(None);
        };
        let binding = SessionCapabilityBinding::decode(tenant, session_id, stored.bytes())?;
        let record = registry
            .get(tenant, session_id)
            .ok_or(SessionControlError::Session(SessionError::Revoked))?;
        if !record.open || record.generation != binding.generation {
            return Err(SessionControlError::Session(SessionError::Revoked));
        }
        live_capability(&store, tenant, &record.request.agent, binding.capability_id)?;
        Ok(Some(binding))
    }

    /// # Errors
    ///
    /// Returns an error unless the permit resolves at its exact open, unexpired generation and an association is stored for its session.
    pub fn unbind_capability(
        &self,
        permit: &OperationPermit,
        observed_head_sequence: u64,
    ) -> Result<(), SessionControlError> {
        let registry = self
            .registry
            .read()
            .map_err(|_| SessionControlError::Unavailable)?;
        let (tenant, session_id, _) =
            self.bindable_session(permit, &registry, observed_head_sequence)?;
        let mut store = self
            .store
            .lock()
            .map_err(|_| SessionControlError::Unavailable)?;
        let key = SessionCapabilityBinding::key(&tenant, session_id)?;
        if store
            .remove_local(&key)
            .map_err(|_| SessionControlError::Unavailable)?
        {
            Ok(())
        } else {
            Err(SessionControlError::Session(SessionError::NotFound))
        }
    }

    fn bindable_session(
        &self,
        permit: &OperationPermit,
        registry: &SessionRegistry,
        observed_head_sequence: u64,
    ) -> Result<(TenantId, SessionId, u64), SessionControlError> {
        permit.resolve(self, registry)?;
        let tenant = permit.token.tenant().clone();
        let session_id = permit.token.session_id();
        let record = registry
            .get(&tenant, session_id)
            .ok_or(SessionControlError::Session(SessionError::NotFound))?;
        if !record.open || record.generation != permit.token.generation() {
            return Err(SessionControlError::Session(SessionError::Revoked));
        }
        if record.request.expiry_sequence <= observed_head_sequence {
            return Err(SessionControlError::Session(SessionError::Expired));
        }
        Ok((tenant, session_id, record.generation))
    }
}

/// Exact-generation authorization retained across a bounded daemon operation.
pub struct OperationPermit {
    token: Token,
    request: RequestContext,
    principal: ResolvedPrincipal,
    stop: StopSignal,
    lookup_binding: Option<Arc<()>>,
}

impl OperationPermit {
    pub(crate) fn with_native_preparation<T>(
        &self, control: &SessionControl, activity: layerx_agent_api::identity::NativeActivity,
        sequence: u64,
        effect: impl FnOnce(&mut Store, &SessionRegistry, &session::NativeSessionAuthorizationV1,
            &BudgetLimiter, &PreparationLifecycle, u64) -> Result<T, SessionControlError>,
    ) -> Result<T, SessionControlError> {
        self.require_operation(Operation::Prepare)?;
        let registry = control.registry.read().map_err(|_| SessionControlError::Unavailable)?;
        self.resolve(control, &registry)?;
        let mut store = control.store.lock().map_err(|_| SessionControlError::Unavailable)?;
        let authorization = session::admit_native(&store, &registry, &self.token,
            &self.principal.tenant, &self.principal.agent, activity, sequence).map_err(SessionControlError::Session)?;
        let expiry = registry.get(&self.principal.tenant, self.principal.session_id)
            .ok_or(SessionControlError::Session(SessionError::NotFound))?.request.expiry_sequence;
        effect(&mut store, &registry, &authorization, &control.budgets, &control.lifecycle, expiry)
    }

    pub(crate) fn with_native_authority<T>(
        &self, control: &SessionControl, activity: layerx_agent_api::identity::NativeActivity,
        sequence: u64,
        effect: impl FnOnce(&mut Store, &SessionRegistry, &session::NativeSessionAuthorizationV1,
            &BudgetLimiter, &PreparationLifecycle, u64) -> Result<T, SessionControlError>,
    ) -> Result<T, SessionControlError> {
        if !matches!(self.operation(), Operation::Prepare | Operation::Sign | Operation::Submit) {
            return Err(SessionControlError::Authorization(AuthorizationError::ScopeDenied));
        }
        let registry = control.registry.read().map_err(|_| SessionControlError::Unavailable)?;
        self.resolve(control, &registry)?;
        let mut store = control.store.lock().map_err(|_| SessionControlError::Unavailable)?;
        let authorization = session::admit_native(&store, &registry, &self.token,
            &self.principal.tenant, &self.principal.agent, activity, sequence).map_err(SessionControlError::Session)?;
        let expiry = registry.get(&self.principal.tenant, self.principal.session_id)
            .ok_or(SessionControlError::Session(SessionError::NotFound))?.request.expiry_sequence;
        effect(&mut store, &registry, &authorization, &control.budgets, &control.lifecycle, expiry)
    }

    pub(crate) fn with_native_owner_install<T>(
        &self, control: &SessionControl,
        effect: impl FnOnce(&mut Store, &SessionRegistry) -> Result<T, SessionControlError>,
    ) -> Result<T, SessionControlError> {
        self.require_operation(Operation::Prepare)?;
        let registry = control.registry.read().map_err(|_| SessionControlError::Unavailable)?;
        self.resolve(control, &registry)?;
        let mut store = control.store.lock().map_err(|_| SessionControlError::Unavailable)?;
        effect(&mut store, &registry)
    }

    pub(crate) fn matches_lookup(&self, binding: &Arc<()>) -> bool {
        self.lookup_binding.as_ref().is_some_and(|value| Arc::ptr_eq(value, binding))
    }

    #[must_use]
    pub const fn principal(&self) -> &ResolvedPrincipal {
        &self.principal
    }

    #[must_use]
    pub fn credential(&self) -> SessionCredential {
        self.token.credential()
    }

    #[must_use]
    pub const fn operation(&self) -> Operation {
        self.request.operation
    }

    #[must_use]
    pub const fn stop(&self) -> &StopSignal {
        &self.stop
    }

    #[must_use]
    pub(crate) fn preparation_authorization(&self) -> PreparationAuthorization {
        PreparationAuthorization {
            session: session::SessionRef::new(self.token.tenant().clone(), self.token.session_id()),
            generation: self.token.generation(),
        }
    }

    /// The single admission seam for prepare, sign and direct external submit. Budget limits
    /// and every other per-write binding travel as data on this one call and are published
    /// in one durable preparation record.
    ///
    /// # Errors
    ///
    /// Returns an error if session authorization, reservation, expiry, the durable record or
    /// a stored binding mismatch refuses the write; a failed prepare leaves no hold and no
    /// record behind.
    pub fn admit_write(
        &self,
        control: &SessionControl,
        admission: WriteAdmission<'_>,
    ) -> Result<DurablePreparation, SessionControlError> {
        let operation = match admission.stage {
            AdmissionStage::Prepare { .. } => Operation::Prepare,
            AdmissionStage::Sign => Operation::Sign,
            AdmissionStage::Submit => Operation::Submit,
        };
        self.require_operation(operation)?;
        self.admit_write_authorized(control, admission)
    }

    pub(crate) fn admit_program_write(
        &self,
        control: &SessionControl,
        admission: WriteAdmission<'_>,
    ) -> Result<DurablePreparation, SessionControlError> {
        self.require_program_operation()?;
        if !matches!(admission.stage, AdmissionStage::Submit) {
            return Err(SessionControlError::Authorization(AuthorizationError::ScopeDenied));
        }
        self.admit_write_authorized(control, admission)
    }

    fn admit_write_authorized(
        &self,
        control: &SessionControl,
        admission: WriteAdmission<'_>,
    ) -> Result<DurablePreparation, SessionControlError> {
        let lifecycle = SessionControlError::Lifecycle;
        let registry = control
            .registry
            .read()
            .map_err(|_| SessionControlError::Unavailable)?;
        self.resolve(control, &registry)?;
        let authorization = self.preparation_authorization();
        let tenant = authorization.session.tenant.clone();
        let preparation_id = admission.preparation_id;
        let key = DurablePreparation::store_key(&tenant, preparation_id).map_err(lifecycle)?;
        let mut store = control
            .store
            .lock()
            .map_err(|_| SessionControlError::Unavailable)?;
        if let AdmissionStage::Prepare { prepared } = admission.stage {
            if store.get(&key).is_some() {
                return Err(lifecycle(LifecycleError::Duplicate));
            }
            if authorization.generation == 0 {
                return Err(lifecycle(LifecycleError::InvalidAuthorization));
            }
            match control.lifecycle.state(preparation_id) {
                Err(LifecycleError::NotFound) => {}
                Ok(_) => return Err(lifecycle(LifecycleError::Duplicate)),
                Err(error) => return Err(lifecycle(error)),
            }
            let plan = match admission.planner {
                Some(planner) => planner(&*store)?,
                None => AdmissionPlan::default(),
            };
            if plan.updates.is_empty() && !plan.companions.is_empty() {
                return Err(SessionControlError::Unavailable);
            }
            let idempotency_key = prepared.audit.idempotency_key;
            if live_preparation_for_key(&store, &tenant, idempotency_key)?.is_some() {
                return Err(lifecycle(LifecycleError::Duplicate));
            }
            let mut extensions = BTreeMap::new();
            let own = PreparationExtension {
                tag: EXTENSION_IDEMPOTENCY,
                bytes: idempotency_key.to_vec(),
            };
            for extension in admission
                .extensions
                .into_iter()
                .chain(plan.extensions)
                .chain(std::iter::once(own))
            {
                if extensions.insert(extension.tag, extension.bytes).is_some() {
                    return Err(lifecycle(LifecycleError::InvalidSignedBytes));
                }
            }
            let holds = match &admission.charge {
                Some(charge) => reserve_charge(
                    &control.budgets,
                    preparation_id,
                    charge,
                    charge.applicable_limits.clone(),
                    admission.current_sequence,
                    admission.core_time_ms,
                )?,
                None => Vec::new(),
            };
            let mut record = DurablePreparation {
                tenant,
                preparation_id,
                session_id: authorization.session.session_id.0,
                generation: authorization.generation,
                not_after: prepared.envelope.timestamp_bound().not_after(),
                payload_hash: prepared.envelope.payload_hash(),
                state: LifecycleState::Prepared,
                activity_id: None,
                holds,
                extensions,
            };
            let release_holds = || {
                budget::release(
                    &control.budgets,
                    preparation_id,
                    ReleaseKind::Failed,
                    admission.current_sequence,
                )
                .map_err(|refusal| lifecycle(LifecycleError::Reservation(refusal)))
            };
            let published = record.encode().map_err(lifecycle).and_then(|bytes| {
                let result = if plan.updates.is_empty() {
                    store.put_local(key.clone(), bytes)
                } else {
                    let mut companions = plan.companions;
                    companions.push((key.clone(), bytes));
                    store.update_local_batch_with_companions(plan.updates, companions)
                };
                result.map_err(|_| SessionControlError::Unavailable)
            });
            if let Err(error) = published {
                release_holds()?;
                return Err(error);
            }
            if let Err(error) = control.lifecycle.register_authorized(
                preparation_id,
                prepared,
                vec![preparation_id],
                authorization,
            ) {
                // The plan and record are already one durable write; settle the record Failed
                // so restart restores no hold, and release the in-memory hold.
                record.state = LifecycleState::Failed;
                let bytes = record.encode().map_err(lifecycle)?;
                store
                    .update_local_batch(vec![(key, bytes)])
                    .map_err(|_| SessionControlError::Unavailable)?;
                release_holds()?;
                return Err(lifecycle(error));
            }
            return Ok(record);
        }
        if admission.planner.is_some() {
            return Err(lifecycle(LifecycleError::AuthorizationMismatch));
        }
        let stored = store
            .get(&key)
            .ok_or(lifecycle(LifecycleError::NotFound))?
            .bytes
            .clone();
        let mut record = DurablePreparation::decode(tenant, &stored).map_err(lifecycle)?;
        if record.session_id != authorization.session.session_id.0
            || record.generation != authorization.generation
        {
            return Err(lifecycle(LifecycleError::AuthorizationMismatch));
        }
        if admission
            .extensions
            .iter()
            .any(|extension| record.extensions.get(&extension.tag) != Some(&extension.bytes))
        {
            return Err(lifecycle(LifecycleError::AuthorizationMismatch));
        }
        control
            .lifecycle
            .check_unexpired_authorized(preparation_id, admission.core_time_ms, &authorization)
            .map_err(lifecycle)?;
        if matches!(admission.stage, AdmissionStage::Sign) {
            return Ok(record);
        }
        for (hold, _) in &record.holds {
            if control
                .budgets
                .is_retired(hold.limit_id)
                .map_err(|refusal| lifecycle(LifecycleError::Reservation(refusal)))?
            {
                return Err(lifecycle(LifecycleError::Reservation(LimitRefusal::Retired(
                    hold.limit_id,
                ))));
            }
        }
        let Some(charge) = &admission.charge else {
            return Ok(record);
        };
        let missing: Vec<LimitId> = charge
            .applicable_limits
            .iter()
            .filter(|limit| !record.holds.iter().any(|(hold, _)| hold.limit_id == **limit))
            .copied()
            .collect();
        if missing.is_empty() {
            return Ok(record);
        }
        let request = ReservationRequest {
            id: preparation_id,
            amount: charge.amount,
            expiry_sequence: charge.head_sequence_bound,
            current_sequence: admission.current_sequence,
            applicable_limits: missing,
        };
        control.budgets.reserve_locked(
            &request,
            charge.core_deadline_ms,
            CoreTimestampMs(admission.core_time_ms),
            |reservation| {
                record.holds.extend(
                    reservation
                        .durable
                        .iter()
                        .map(|hold| (hold.clone(), charge.core_deadline_ms)),
                );
                let bytes = record.encode().map_err(lifecycle)?;
                store
                    .update_local_batch(vec![(key, bytes)])
                    .map_err(|_| SessionControlError::Unavailable)
            },
        )?;
        Ok(record)
    }

    ///
    /// # Errors
    ///
    /// Returns an error if session authorization, durable state, or preparation invalidation fails.
    pub fn register_preparation(
        &self,
        control: &SessionControl,
        preparation_id: [u8; 32],
        prepared: &Prepared,
        reservation_ids: Vec<[u8; 32]>,
    ) -> Result<(), SessionControlError> {
        self.require_operation(Operation::Prepare)?;
        let registry = control
            .registry
            .read()
            .map_err(|_| SessionControlError::Unavailable)?;
        self.resolve(control, &registry)?;
        control
            .lifecycle
            .register_authorized(
                preparation_id,
                prepared,
                reservation_ids,
                self.preparation_authorization(),
            )
            .map_err(SessionControlError::Lifecycle)
    }

    ///
    /// # Errors
    ///
    /// Returns an error if session authorization, durable state, or preparation invalidation fails.
    pub fn transition_preparation(
        &self,
        control: &SessionControl,
        preparation_id: [u8; 32],
        next: LifecycleState,
        current_sequence: u64,
    ) -> Result<(), SessionControlError> {
        let expected = match next {
            LifecycleState::Signing | LifecycleState::Signed => Operation::Sign,
            LifecycleState::Submitted => Operation::Submit,
            LifecycleState::Acknowledged
            | LifecycleState::Unknown
            | LifecycleState::Executed
            | LifecycleState::Failed
            | LifecycleState::Expired => Operation::Track,
            LifecycleState::Prepared => {
                return Err(SessionControlError::Authorization(
                    AuthorizationError::ScopeDenied,
                ))
            }
        };
        self.require_operation(expected)?;
        self.transition_preparation_authorized(control, preparation_id, next, current_sequence)
    }

    pub(crate) fn transition_program_submitted(
        &self,
        control: &SessionControl,
        preparation_id: [u8; 32],
        current_sequence: u64,
    ) -> Result<(), SessionControlError> {
        self.require_program_operation()?;
        self.transition_preparation_authorized(
            control, preparation_id, LifecycleState::Submitted, current_sequence,
        )
    }

    fn transition_preparation_authorized(
        &self,
        control: &SessionControl,
        preparation_id: [u8; 32],
        next: LifecycleState,
        current_sequence: u64,
    ) -> Result<(), SessionControlError> {
        let registry = control
            .registry
            .read()
            .map_err(|_| SessionControlError::Unavailable)?;
        self.resolve(control, &registry)?;
        control
            .lifecycle
            .transition_authorized(
                preparation_id,
                next,
                current_sequence,
                &self.preparation_authorization(),
            )
            .map_err(SessionControlError::Lifecycle)
    }

    ///
    /// # Errors
    ///
    /// Returns an error if session authorization, durable state, or preparation invalidation fails.
    pub fn admit_submission(
        &self,
        control: &SessionControl,
        preparation_id: [u8; 32],
        core_batch_time_ms: u64,
    ) -> Result<(), SessionControlError> {
        self.require_operation(Operation::Submit)?;
        let registry = control
            .registry
            .read()
            .map_err(|_| SessionControlError::Unavailable)?;
        self.resolve(control, &registry)?;
        control
            .lifecycle
            .admit_submission_authorized(
                preparation_id,
                core_batch_time_ms,
                &self.preparation_authorization(),
            )
            .map_err(SessionControlError::Lifecycle)
    }

    ///
    /// # Errors
    ///
    /// Returns an error if session authorization, durable state, or preparation expiry fails.
    pub fn admit_signing(
        &self,
        control: &SessionControl,
        preparation_id: [u8; 32],
        core_batch_time_ms: u64,
    ) -> Result<(), SessionControlError> {
        self.require_operation(Operation::Sign)?;
        let registry = control
            .registry
            .read()
            .map_err(|_| SessionControlError::Unavailable)?;
        self.resolve(control, &registry)?;
        control
            .lifecycle
            .check_unexpired_authorized(
                preparation_id,
                core_batch_time_ms,
                &self.preparation_authorization(),
            )
            .map_err(SessionControlError::Lifecycle)
    }

    ///
    /// # Errors
    ///
    /// Returns an error if session authorization, durable state, or preparation invalidation fails.
    pub fn retain_signed_bytes(
        &self,
        control: &SessionControl,
        preparation_id: [u8; 32],
        signed_bytes: Vec<u8>,
        activity_id: [u8; 32],
    ) -> Result<(), SessionControlError> {
        self.require_operation(Operation::Sign)?;
        let registry = control
            .registry
            .read()
            .map_err(|_| SessionControlError::Unavailable)?;
        self.resolve(control, &registry)?;
        control
            .lifecycle
            .retain_signed_bytes_authorized(
                preparation_id,
                signed_bytes,
                activity_id,
                &self.preparation_authorization(),
            )
            .map_err(SessionControlError::Lifecycle)
    }

    ///
    /// # Errors
    ///
    /// Returns an error if session authorization, durable state, or preparation invalidation fails.
    pub fn signed_bytes(
        &self,
        control: &SessionControl,
        preparation_id: [u8; 32],
    ) -> Result<Vec<u8>, SessionControlError> {
        self.require_operation(Operation::Submit)?;
        let registry = control
            .registry
            .read()
            .map_err(|_| SessionControlError::Unavailable)?;
        self.resolve(control, &registry)?;
        control
            .lifecycle
            .signed_bytes_authorized(preparation_id, &self.preparation_authorization())
            .map_err(SessionControlError::Lifecycle)
    }

    ///
    /// # Errors
    ///
    /// Returns an error if session authorization, durable state, or preparation invalidation fails.
    pub fn submit_with_external_signature(
        &self,
        control: &SessionControl,
        preparation_id: [u8; 32],
        signed_bytes: Vec<u8>,
        activity_id: [u8; 32],
        current_sequence: u64,
        core_batch_time_ms: u64,
    ) -> Result<(), SessionControlError> {
        self.require_operation(Operation::Submit)?;
        self.submit_external_signature_authorized(
            control, preparation_id, signed_bytes, activity_id, current_sequence, core_batch_time_ms,
        )
    }

    pub(crate) fn submit_program_with_external_signature(
        &self,
        control: &SessionControl,
        preparation_id: [u8; 32],
        signed_bytes: Vec<u8>,
        activity_id: [u8; 32],
        current_sequence: u64,
        core_batch_time_ms: u64,
    ) -> Result<(), SessionControlError> {
        self.require_program_operation()?;
        self.submit_external_signature_authorized(
            control, preparation_id, signed_bytes, activity_id, current_sequence, core_batch_time_ms,
        )
    }

    fn submit_external_signature_authorized(
        &self,
        control: &SessionControl,
        preparation_id: [u8; 32],
        signed_bytes: Vec<u8>,
        activity_id: [u8; 32],
        current_sequence: u64,
        core_batch_time_ms: u64,
    ) -> Result<(), SessionControlError> {
        let registry = control
            .registry
            .read()
            .map_err(|_| SessionControlError::Unavailable)?;
        self.resolve(control, &registry)?;
        let authorization = self.preparation_authorization();
        control
            .lifecycle
            .check_unexpired_authorized(preparation_id, core_batch_time_ms, &authorization)
            .map_err(SessionControlError::Lifecycle)?;
        match control
            .lifecycle
            .state(preparation_id)
            .map_err(SessionControlError::Lifecycle)?
        {
            LifecycleState::Prepared => {
                control
                    .lifecycle
                    .transition_authorized(
                        preparation_id,
                        LifecycleState::Signing,
                        current_sequence,
                        &authorization,
                    )
                    .map_err(SessionControlError::Lifecycle)?;
                control
                    .lifecycle
                    .retain_signed_bytes_authorized(
                        preparation_id,
                        signed_bytes,
                        activity_id,
                        &authorization,
                    )
                    .map_err(SessionControlError::Lifecycle)?;
            }
            LifecycleState::Signing => {
                control
                    .lifecycle
                    .retain_signed_bytes_authorized(
                        preparation_id,
                        signed_bytes,
                        activity_id,
                        &authorization,
                    )
                    .map_err(SessionControlError::Lifecycle)?;
            }
            LifecycleState::Signed => {
                let retained = control
                    .lifecycle
                    .signed_bytes_authorized(preparation_id, &authorization)
                    .map_err(SessionControlError::Lifecycle)?;
                if retained != signed_bytes {
                    return Err(SessionControlError::Lifecycle(
                        LifecycleError::AuthorizationMismatch,
                    ));
                }
            }
            from => {
                return Err(SessionControlError::Lifecycle(
                    LifecycleError::InvalidTransition {
                        from,
                        to: LifecycleState::Signed,
                    },
                ))
            }
        }
        control
            .lifecycle
            .admit_submission_authorized(preparation_id, core_batch_time_ms, &authorization)
            .map_err(SessionControlError::Lifecycle)
    }

    fn require_program_operation(&self) -> Result<(), SessionControlError> {
        if matches!(
            self.request.operation,
            Operation::ProgramCall
                | Operation::ProgramDeploy
                | Operation::ProgramUpgrade
                | Operation::ProgramWindDown
        ) {
            Ok(())
        } else {
            Err(SessionControlError::Authorization(AuthorizationError::ScopeDenied))
        }
    }

    fn require_operation(&self, expected: Operation) -> Result<(), SessionControlError> {
        if self.request.operation == expected {
            Ok(())
        } else {
            Err(SessionControlError::Authorization(
                AuthorizationError::ScopeDenied,
            ))
        }
    }

    /// Re-resolves at a non-mutating boundary. A result computed outside the registry lock must
    /// not be released unless this succeeds afterwards.
    ///
    /// # Errors
    ///
    /// Returns an error if session authorization, durable state, or preparation invalidation fails.
    pub fn boundary(&self, control: &SessionControl) -> Result<(), SessionControlError> {
        let registry = control
            .registry
            .read()
            .map_err(|_| SessionControlError::Unavailable)?;
        self.resolve(control, &registry).map(|_| ())
    }

    /// Linearizes one irreversible effect with close, restriction, and revocation. Preparatory,
    /// side-effect-free I/O may occur before this method, but transmission, durable approval, or
    /// any other externally visible commit must occur inside it or behind a stop-aware two-phase
    /// boundary that calls it for the actual commit.
    ///
    /// # Errors
    ///
    /// Returns an error if session authorization, durable state, or preparation invalidation fails.
    pub fn commit<T>(
        &self,
        control: &SessionControl,
        commit: impl FnOnce() -> Result<T, SessionControlError>,
    ) -> Result<T, SessionControlError> {
        let registry = control
            .registry
            .read()
            .map_err(|_| SessionControlError::Unavailable)?;
        self.resolve(control, &registry)?;
        let value = commit()?;
        self.resolve(control, &registry)?;
        Ok(value)
    }

    /// Resolves this permit against the held registry and authorizes one target session of the
    /// same tenant owned by the same agent, returning the target's current exact generation.
    fn authorize_target(
        &self,
        control: &SessionControl,
        registry: &SessionRegistry,
        target: SessionId,
    ) -> Result<(TenantId, u64), SessionControlError> {
        let principal = self.resolve(control, registry)?;
        let tenant = self.token.tenant().clone();
        if principal.tenant != tenant || &principal.agent != self.token.agent() {
            return Err(SessionControlError::Authorization(
                AuthorizationError::CoordinateMismatch,
            ));
        }
        let record = registry
            .get(&tenant, target)
            .ok_or(SessionControlError::Session(SessionError::NotFound))?;
        if record.request.tenant != tenant {
            return Err(SessionControlError::Session(SessionError::IdentityMismatch));
        }
        if record.request.agent != principal.agent {
            return Err(SessionControlError::Authorization(
                AuthorizationError::NotAuthorized,
            ));
        }
        if !record.open {
            return Err(SessionControlError::Session(SessionError::AlreadyClosed));
        }
        Ok((tenant, record.generation))
    }

    fn resolve(
        &self,
        control: &SessionControl,
        registry: &SessionRegistry,
    ) -> Result<ResolvedPrincipal, SessionControlError> {
        if self.stop.reason() == Some(Termination::SessionRevoked) {
            return Err(SessionControlError::Authorization(
                AuthorizationError::Revoked,
            ));
        }
        let mut observability = control
            .observability
            .lock()
            .map_err(|_| SessionControlError::Unavailable)?;
        tenant::resolve(&self.token, registry, &self.request, &mut observability)
            .map_err(SessionControlError::Authorization)
    }

    /// Runs one subscription-store step against this permit's exact retained token and request
    /// core sequence, the control's held session registry, and the control's retained tenant
    /// observability, after the operation and revocation-aware resolution checks.
    ///
    /// # Errors
    ///
    /// Returns an error if the permit operation differs, the permit no longer resolves, or
    /// session state is unavailable. The effect's own error is returned inside `Ok`.
    pub(crate) fn with_subscription_authority<T, E>(
        &self,
        control: &SessionControl,
        operation: Operation,
        effect: impl FnOnce(&SessionRegistry, &Token, &mut TenantObservability, u64) -> Result<T, E>,
    ) -> Result<Result<T, E>, SessionControlError> {
        self.require_operation(operation)?;
        let registry = control
            .registry
            .read()
            .map_err(|_| SessionControlError::Unavailable)?;
        self.resolve(control, &registry)?;
        let mut observability = control
            .observability
            .lock()
            .map_err(|_| SessionControlError::Unavailable)?;
        Ok(effect(
            &registry,
            &self.token,
            &mut observability,
            self.request.core_sequence,
        ))
    }
}

/// Stage at which one write passes the single admission seam.
pub enum AdmissionStage<'a> {
    Prepare { prepared: &'a Prepared },
    Sign,
    Submit,
}

/// Spend charged against applicable limits for one admitted write.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct WriteCharge {
    pub amount: u128,
    pub applicable_limits: Vec<LimitId>,
    pub head_sequence_bound: u64,
    pub core_deadline_ms: Option<CoreTimestampMs>,
}

/// One prepare, sign or direct external submit passing the single admission seam.
pub struct WriteAdmission<'a> {
    pub stage: AdmissionStage<'a>,
    pub preparation_id: [u8; 32],
    pub charge: Option<WriteCharge>,
    pub extensions: Vec<PreparationExtension>,
    pub current_sequence: u64,
    pub core_time_ms: u64,
    /// Prepare only: runs under the admission store lock and returns the caller's durable
    /// charge, published in the same store write as the preparation record.
    pub planner: Option<AdmissionPlanner<'a>>,
}

/// Caller-computed durable writes joined to one prepare admission. `updates` must name existing
/// local records and `companions` absent ones; companions without an update cannot be expressed
/// as one store write and are refused.
#[derive(Clone, Debug, Default, Eq, PartialEq)]
pub struct AdmissionPlan {
    pub updates: Vec<(TenantKey, Vec<u8>)>,
    pub companions: Vec<(TenantKey, Vec<u8>)>,
    pub extensions: Vec<PreparationExtension>,
}

/// Computes an [`AdmissionPlan`] against the locked store.
pub type AdmissionPlanner<'a> =
    Box<dyn FnOnce(&Store) -> Result<AdmissionPlan, SessionControlError> + 'a>;

fn reserve_charge(
    budgets: &BudgetLimiter,
    preparation_id: [u8; 32],
    charge: &WriteCharge,
    applicable_limits: Vec<LimitId>,
    current_sequence: u64,
    core_time_ms: u64,
) -> Result<Vec<(DurableBudgetReservation, Option<CoreTimestampMs>)>, SessionControlError> {
    let request = ReservationRequest {
        id: preparation_id,
        amount: charge.amount,
        expiry_sequence: charge.head_sequence_bound,
        current_sequence,
        applicable_limits,
    };
    let reservation = match charge.core_deadline_ms {
        Some(deadline) => budget::reserve_until_core_time(
            budgets,
            &request,
            deadline,
            CoreTimestampMs(core_time_ms),
        ),
        None => budget::reserve(budgets, &request),
    }
    .map_err(|refusal| SessionControlError::Lifecycle(LifecycleError::Reservation(refusal)))?;
    Ok(reservation
        .durable
        .into_iter()
        .map(|hold| (hold, charge.core_deadline_ms))
        .collect())
}

// ponytail: linear scan of the tenant's preparation records; add a key index once a prepare
// without a caller plan can publish two absent keys in one store write.
fn live_preparation_for_key(
    store: &Store,
    tenant: &TenantId,
    idempotency_key: [u8; 32],
) -> Result<Option<[u8; 32]>, SessionControlError> {
    let lifecycle = SessionControlError::Lifecycle;
    let mut found = None;
    for preparation_id in DurablePreparation::recorded_ids(store, tenant).map_err(lifecycle)? {
        let key = DurablePreparation::store_key(tenant, preparation_id).map_err(lifecycle)?;
        let stored = store
            .get(&key)
            .ok_or(lifecycle(LifecycleError::NotFound))?;
        let record =
            DurablePreparation::decode(tenant.clone(), &stored.bytes).map_err(lifecycle)?;
        if record.terminal()
            || record.extensions.get(&EXTENSION_IDEMPOTENCY).map(Vec::as_slice)
                != Some(idempotency_key.as_slice())
        {
            continue;
        }
        if found.replace(preparation_id).is_some() {
            return Err(lifecycle(LifecycleError::Duplicate));
        }
    }
    Ok(found)
}

#[derive(Debug)]
pub enum SessionControlError {
    Session(SessionError),
    Authorization(AuthorizationError),
    Lifecycle(LifecycleError),
    Human(HumanOperationError),
    Unavailable,
}

impl From<LimitRefusal> for SessionControlError {
    fn from(refusal: LimitRefusal) -> Self {
        Self::Lifecycle(LifecycleError::Reservation(refusal))
    }
}

fn replacement_bearer(
    record: &session::SessionRecord,
    current_token: [u8; 32],
) -> Result<(u64, [u8; 32]), SessionControlError> {
    let replacement_generation =
        record
            .generation
            .checked_add(1)
            .ok_or(SessionControlError::Session(
                SessionError::GenerationExhausted,
            ))?;
    let mut replacement_token = [0_u8; 32];
    let mut available = false;
    for _ in 0..8 {
        getrandom::fill(&mut replacement_token).map_err(|_| SessionControlError::Unavailable)?;
        if replacement_token != [0; 32]
            && replacement_token != current_token
            && !record.retired_token_ids.contains(&replacement_token)
        {
            available = true;
            break;
        }
    }
    if !available {
        return Err(SessionControlError::Unavailable);
    }
    Ok((replacement_generation, replacement_token))
}

const CAPABILITY_BINDING_PREFIX: &[u8] = b"session/capability/";
const CAPABILITY_BINDING_VERSION: &[u8; 6] = b"LXSC01";
const CAPABILITY_BINDING_BODY: usize = 32 + 32 + 8 + 8;

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct SessionCapabilityBinding {
    pub tenant: TenantId,
    pub session_id: SessionId,
    pub capability_id: [u8; 32],
    pub generation: u64,
    pub bound_at_sequence: u64,
}

impl SessionCapabilityBinding {
    fn key(tenant: &TenantId, session_id: SessionId) -> Result<TenantKey, SessionControlError> {
        let mut object = CAPABILITY_BINDING_PREFIX.to_vec();
        object.extend_from_slice(&session_id.0);
        TenantKey::new(tenant.clone(), ObjectKind::Configuration, object)
            .map_err(|error| SessionControlError::Session(SessionError::Store(error)))
    }

    fn encode(&self) -> Vec<u8> {
        let mut bytes = CAPABILITY_BINDING_VERSION.to_vec();
        bytes.extend_from_slice(&self.session_id.0);
        bytes.extend_from_slice(&self.capability_id);
        bytes.extend_from_slice(&self.generation.to_be_bytes());
        bytes.extend_from_slice(&self.bound_at_sequence.to_be_bytes());
        bytes
    }

    fn decode(
        tenant: &TenantId,
        session_id: SessionId,
        bytes: &[u8],
    ) -> Result<Self, SessionControlError> {
        let corrupt =
            || SessionControlError::Session(SessionError::MissingField("capability_binding"));
        let body = bytes
            .strip_prefix(CAPABILITY_BINDING_VERSION.as_slice())
            .filter(|body| body.len() == CAPABILITY_BINDING_BODY)
            .ok_or_else(corrupt)?;
        let (stored_session, rest) = body.split_at(32);
        let (capability_id, rest) = rest.split_at(32);
        let (generation, bound_at_sequence) = rest.split_at(8);
        let capability_id: [u8; 32] = capability_id.try_into().map_err(|_| corrupt())?;
        let generation = u64::from_be_bytes(generation.try_into().map_err(|_| corrupt())?);
        let bound_at_sequence =
            u64::from_be_bytes(bound_at_sequence.try_into().map_err(|_| corrupt())?);
        if stored_session != session_id.0.as_slice() || capability_id == [0; 32] || generation == 0
        {
            return Err(corrupt());
        }
        Ok(Self {
            tenant: tenant.clone(),
            session_id,
            capability_id,
            generation,
            bound_at_sequence,
        })
    }
}

fn live_capability(
    store: &Store,
    tenant: &TenantId,
    agent: &layerx_types::ids::Did,
    capability_id: [u8; 32],
) -> Result<(), SessionControlError> {
    let record = crate::capability::timed::restore(store, tenant, &capability_id)
        .map_err(|_| SessionControlError::Unavailable)?
        .ok_or(SessionControlError::Authorization(
            AuthorizationError::NotAuthorized,
        ))?;
    if record.agent.as_bytes() != agent.as_bytes() {
        return Err(SessionControlError::Authorization(
            AuthorizationError::NotAuthorized,
        ));
    }
    if record.revoked.is_some() {
        return Err(SessionControlError::Authorization(
            AuthorizationError::Revoked,
        ));
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use layerx_types::ids::Did;
    use layerx_types::verify::VerificationLevel;

    use super::*;
    use crate::budget::{LimitConfig, LimitScope};
    use crate::capability::timed::{self, TimedCapability};
    use crate::identity::{self, CoreIdentity, IdentityError, IdentityResolver, ProtocolAuthority};
    use crate::session::OpenRequest;

    const OBSERVED: u64 = 10;
    const SESSION: SessionId = SessionId([1; 32]);

    struct CoreBoundary(CoreIdentity);

    impl IdentityResolver for CoreBoundary {
        fn resolve(&mut self, _did: &Did) -> Result<Option<CoreIdentity>, IdentityError> {
            Ok(Some(self.0.clone()))
        }
    }

    fn must<T, E: std::fmt::Debug>(value: Result<T, E>) -> T {
        value.unwrap_or_else(|error| panic!("session capability binding: {error:?}"))
    }

    fn tenant() -> TenantId {
        must(TenantId::new("tenant-a"))
    }

    fn control(name: &str) -> (std::path::PathBuf, SessionControl, Token) {
        let root = std::env::temp_dir().join(format!("lxp-scb-{name}-{}", std::process::id()));
        let _ = std::fs::remove_dir_all(&root);
        let mut store = must(Store::open(&root));
        let mut sessions = SessionRegistry::default();
        let agent = must(Did::new(b"agent-a"));
        let mut boundary = CoreBoundary(CoreIdentity {
            canonical_bytes: b"session-capability-identity".to_vec(),
            head_sequence: OBSERVED,
            revocation_sequence: 1,
            verification_level: VerificationLevel::STATE_PROVEN,
            frozen: false,
            authorities: vec![ProtocolAuthority::SessionKey([4; 32])],
        });
        let identity = must(identity::register(
            &mut store,
            tenant(),
            agent.clone(),
            &mut boundary,
        ));
        let token = must(session::open(
            &mut store,
            &mut sessions,
            &identity,
            OpenRequest {
                session_id: SESSION,
                token_id: [2; 32],
                tenant: tenant(),
                agent,
                authority: ProtocolAuthority::SessionKey([4; 32]),
                permitted_activity_types: BTreeSet::from([5]),
                scopes: BTreeSet::from(["prepare".to_owned(), "write".to_owned()]),
                expiry_sequence: 1_000,
                expiry_seconds: Some(1_900_000_000),
                opening_client: "session-capability-suite".to_owned(),
                policy_version: "policy-v1".to_owned(),
            },
            OBSERVED,
        ));
        let limiter = must(BudgetLimiter::new(vec![LimitConfig {
            id: LimitId([1; 16]),
            name: "tenant-limit".to_owned(),
            scope: LimitScope::Tenant([1; 32]),
            ceiling: 1_000,
            consumed: 0,
        }]));
        let control = SessionControl::new(
            Arc::new(Mutex::new(store)),
            sessions,
            Arc::new(PreparationLifecycle::default()),
            Arc::new(limiter),
        );
        (root, control, token)
    }

    fn capability(control: &SessionControl, id: u8, agent: &str, revoked: Option<(u64, u64)>) {
        let record = TimedCapability {
            id: [id; 32],
            parent: None,
            tenant: tenant(),
            agent: agent.to_owned(),
            authority: ProtocolAuthority::CapabilityGrant([9; 32]),
            activity_types: BTreeSet::from([5]),
            counterparties: BTreeSet::from([[8; 32]]),
            assets: BTreeSet::from([[7; 32]]),
            amount_ceilings: BTreeMap::from([([7; 32], 100)]),
            rate_ceilings: BTreeMap::from([(60, 5)]),
            purposes: BTreeSet::from(["pay".to_owned()]),
            expiry_seconds: 10_000,
            grant_not_after_ms: 10_000_000,
            created_at_ms: 1,
            created_at_sequence: 1,
            revoked,
        };
        let mut store = must(control.store.lock());
        must(store.put_local(
            must(timed::record_key(&tenant(), &record.id)),
            must(record.encode()),
        ));
    }

    fn permit(control: &SessionControl, token: &Token) -> OperationPermit {
        must(control.authorize(
            &token.credential(),
            Operation::Prepare,
            Surface::Contract,
            OBSERVED,
            None,
        ))
    }

    #[test]
    fn binding_round_trips_and_replays_only_the_same_capability() {
        let (root, control, token) = control("round-trip");
        capability(&control, 6, "agent-a", None);
        capability(&control, 7, "agent-a", None);
        let permit = permit(&control, &token);
        must(control.bind_capability(&permit, [6; 32], OBSERVED));
        let expected = SessionCapabilityBinding {
            tenant: tenant(),
            session_id: SESSION,
            capability_id: [6; 32],
            generation: 1,
            bound_at_sequence: OBSERVED,
        };
        assert_eq!(
            must(control.capability_binding(&tenant(), SESSION)),
            Some(expected.clone())
        );
        must(control.bind_capability(&permit, [6; 32], OBSERVED + 1));
        assert_eq!(
            must(control.capability_binding(&tenant(), SESSION)),
            Some(expected)
        );
        assert!(matches!(
            control.bind_capability(&permit, [7; 32], OBSERVED),
            Err(SessionControlError::Session(SessionError::IdentityMismatch))
        ));
        assert_eq!(
            must(control.capability_binding(&tenant(), SessionId([3; 32]))),
            None
        );
        let _ = std::fs::remove_dir_all(root);
    }

    #[test]
    fn binding_refuses_unusable_capabilities_and_expired_sessions() {
        let (root, control, token) = control("refusals");
        capability(&control, 6, "agent-b", None);
        capability(&control, 7, "agent-a", Some((5, 9)));
        capability(&control, 8, "agent-a", None);
        let permit = permit(&control, &token);
        assert!(matches!(
            control.bind_capability(&permit, [0; 32], OBSERVED),
            Err(SessionControlError::Session(SessionError::MissingField(
                "capability_id"
            )))
        ));
        assert!(matches!(
            control.bind_capability(&permit, [5; 32], OBSERVED),
            Err(SessionControlError::Authorization(
                AuthorizationError::NotAuthorized
            ))
        ));
        assert!(matches!(
            control.bind_capability(&permit, [6; 32], OBSERVED),
            Err(SessionControlError::Authorization(
                AuthorizationError::NotAuthorized
            ))
        ));
        assert!(matches!(
            control.bind_capability(&permit, [7; 32], OBSERVED),
            Err(SessionControlError::Authorization(
                AuthorizationError::Revoked
            ))
        ));
        assert!(matches!(
            control.bind_capability(&permit, [8; 32], 1_000),
            Err(SessionControlError::Session(SessionError::Expired))
        ));
        assert_eq!(must(control.capability_binding(&tenant(), SESSION)), None);
        let _ = std::fs::remove_dir_all(root);
    }

    #[test]
    fn session_close_leaves_the_binding_unusable() {
        let (root, control, token) = control("close");
        capability(&control, 6, "agent-a", None);
        let permit = permit(&control, &token);
        must(control.bind_capability(&permit, [6; 32], OBSERVED));
        must(control.close(&tenant(), SESSION, OBSERVED));
        assert!(matches!(
            control.capability_binding(&tenant(), SESSION),
            Err(SessionControlError::Session(SessionError::Revoked))
        ));
        assert!(control.bind_capability(&permit, [6; 32], OBSERVED).is_err());
        assert!(control.unbind_capability(&permit, OBSERVED).is_err());
        let _ = std::fs::remove_dir_all(root);
    }

    #[test]
    fn capability_revocation_leaves_the_binding_unusable() {
        let (root, control, token) = control("revoked");
        capability(&control, 6, "agent-a", None);
        let permit = permit(&control, &token);
        must(control.bind_capability(&permit, [6; 32], OBSERVED));
        capability(&control, 6, "agent-a", Some((5, 11)));
        assert!(matches!(
            control.capability_binding(&tenant(), SESSION),
            Err(SessionControlError::Authorization(
                AuthorizationError::Revoked
            ))
        ));
        let _ = std::fs::remove_dir_all(root);
    }

    #[test]
    fn unbind_clears_the_association_once() {
        let (root, control, token) = control("unbind");
        capability(&control, 6, "agent-a", None);
        let permit = permit(&control, &token);
        must(control.bind_capability(&permit, [6; 32], OBSERVED));
        must(control.unbind_capability(&permit, OBSERVED));
        assert_eq!(must(control.capability_binding(&tenant(), SESSION)), None);
        assert!(matches!(
            control.unbind_capability(&permit, OBSERVED),
            Err(SessionControlError::Session(SessionError::NotFound))
        ));
        let _ = std::fs::remove_dir_all(root);
    }

    #[test]
    fn binding_record_decodes_strictly() {
        let binding = SessionCapabilityBinding {
            tenant: tenant(),
            session_id: SESSION,
            capability_id: [6; 32],
            generation: 2,
            bound_at_sequence: OBSERVED,
        };
        let bytes = binding.encode();
        assert_eq!(&bytes[..6], CAPABILITY_BINDING_VERSION);
        assert_eq!(
            must(SessionCapabilityBinding::decode(&tenant(), SESSION, &bytes)),
            binding
        );
        let mut other_version = bytes.clone();
        other_version[5] = b'2';
        let mut zero_capability = bytes.clone();
        zero_capability[38..70].fill(0);
        let mut zero_generation = bytes.clone();
        zero_generation[70..78].fill(0);
        let mut trailing = bytes.clone();
        trailing.push(0);
        for malformed in [
            other_version,
            zero_capability,
            zero_generation,
            trailing,
            bytes[..85].to_vec(),
        ] {
            assert!(SessionCapabilityBinding::decode(&tenant(), SESSION, &malformed).is_err());
        }
        assert!(SessionCapabilityBinding::decode(&tenant(), SessionId([3; 32]), &bytes).is_err());
    }

    #[test]
    fn hold_under_unknown_limit_is_refused_on_restore_and_retained() {
        let (root, control, _token) = control("unknown-hold-refused");
        let config = |id: u8| LimitConfig {
            id: LimitId([id; 16]),
            name: "tenant-limit".to_owned(),
            scope: LimitScope::Tenant([1; 32]),
            ceiling: 1_000,
            consumed: 0,
        };
        let request = |limit: LimitId| budget::ReservationRequest {
            id: [7; 32],
            amount: 5,
            expiry_sequence: 1_000,
            current_sequence: 1,
            applicable_limits: vec![limit],
        };
        let known_limiter = must(BudgetLimiter::new(vec![config(1)]));
        let declared_limiter = must(BudgetLimiter::new(vec![config(9)]));
        let known = must(budget::reserve(&known_limiter, &request(LimitId([1; 16])))).durable;
        let declared = must(budget::reserve(
            &declared_limiter,
            &request(LimitId([9; 16])),
        ))
        .durable;
        let record = DurablePreparation {
            tenant: tenant(),
            preparation_id: [7; 32],
            session_id: SESSION.0,
            generation: 1,
            not_after: 1_900_000_000,
            payload_hash: [3; 32],
            state: LifecycleState::Prepared,
            activity_id: None,
            holds: known
                .into_iter()
                .chain(declared)
                .map(|hold| (hold, None))
                .collect(),
            extensions: std::collections::BTreeMap::new(),
        };
        let key = must(DurablePreparation::store_key(&tenant(), [7; 32]));
        let bytes = must(record.encode());
        {
            let store = control.store();
            let mut store = must(store.lock());
            must(store.put_local(key.clone(), bytes.clone()));
        }
        assert!(matches!(
            control.restore_writes(),
            Err(SessionControlError::Lifecycle(LifecycleError::Reservation(
                budget::LimitRefusal::UnknownLimit(limit)
            ))) if limit == LimitId([9; 16])
        ));
        assert_eq!(control.budgets.held_reservations(), Ok(0));
        assert_eq!(control.budgets.consumed(LimitId([1; 16])), Ok(0));
        {
            let store = control.store();
            let store = must(store.lock());
            assert_eq!(
                store.get(&key).map(|value| value.bytes().to_vec()),
                Some(bytes)
            );
        }
        let _ = std::fs::remove_dir_all(root);
    }

    struct RecordedCore(crate::prepare::CorePreparationState);

    impl crate::prepare::CorePreparationBoundary for RecordedCore {
        fn preparation_state(
            &mut self,
            _actor: &Did,
        ) -> Result<crate::prepare::CorePreparationState, crate::prepare::CoreStateError> {
            Ok(self.0.clone())
        }
    }

    fn activity_type() -> layerx_types::payload::ActivityType {
        must(layerx_types::payload::ActivityType::new(
            layerx_types::payload::ModuleId::Asset,
            5,
        ))
    }

    fn signed_payload() -> Vec<u8> {
        use ed25519_dalek::Signer as _;
        use sha2::Digest as _;
        let mut encoder = layerx_wire::encode::Encoder::new(512);
        encoder
            .u16(0x5301)
            .unwrap_or_else(|error| panic!("tag: {error:?}"));
        encoder
            .u16(10)
            .unwrap_or_else(|error| panic!("fields: {error:?}"));
        encoder
            .fixed(&[0x11; 32])
            .unwrap_or_else(|error| panic!("from: {error:?}"));
        encoder
            .fixed(&[0x22; 32])
            .unwrap_or_else(|error| panic!("to: {error:?}"));
        encoder
            .fixed(&[0x33; 32])
            .unwrap_or_else(|error| panic!("asset: {error:?}"));
        encoder
            .u128(25)
            .unwrap_or_else(|error| panic!("amount: {error:?}"));
        encoder
            .u64(5)
            .unwrap_or_else(|error| panic!("sequence: {error:?}"));
        encoder
            .fixed(&[4; 32])
            .unwrap_or_else(|error| panic!("idempotency: {error:?}"));
        encoder
            .u64(1_010)
            .unwrap_or_else(|error| panic!("expiry: {error:?}"));
        encoder
            .fixed(&[0x55; 32])
            .unwrap_or_else(|error| panic!("context: {error:?}"));
        encoder
            .u8(0)
            .unwrap_or_else(|error| panic!("conditions: {error:?}"));
        encoder
            .u8(1)
            .unwrap_or_else(|error| panic!("authority kind: {error:?}"));
        encoder
            .fixed(&[0x11; 32])
            .unwrap_or_else(|error| panic!("controller: {error:?}"));
        encoder
            .fixed(&[0x66; 32])
            .unwrap_or_else(|error| panic!("payload key: {error:?}"));
        encoder
            .fixed(&[0x77; 64])
            .unwrap_or_else(|error| panic!("payload signature: {error:?}"));
        encoder
            .fixed(&[0x55; 32])
            .unwrap_or_else(|error| panic!("signed context: {error:?}"));
        encoder
            .u32(17)
            .unwrap_or_else(|error| panic!("network: {error:?}"));
        encoder
            .u16(layerx_wire::limits::PROTOCOL_VERSION)
            .unwrap_or_else(|error| panic!("version: {error:?}"));
        let mut payload = encoder.finish();
        let offset = payload.len() - 167;
        let key = ed25519_dalek::SigningKey::from_bytes(&[0x66; 32]);
        payload[offset + 33..offset + 65].copy_from_slice(&key.verifying_key().to_bytes());
        let mut hasher = sha2::Sha256::new();
        hasher.update(layerx_wire::hash::Domain::SignaturePreimage.tag());
        hasher.update(&payload[..2]);
        hasher.update(&payload[4..offset + 33]);
        hasher.update(&payload[offset + 129..]);
        let digest: [u8; 32] = hasher.finalize().into();
        payload[offset + 65..offset + 129].copy_from_slice(&key.sign(&digest).to_bytes());
        payload
    }

    fn prepared_activity() -> Prepared {
        let registry = must(layerx_types::payload::ModuleRegistry::new(&[must(
            layerx_types::payload::ModuleRegistration::new(
                layerx_types::payload::ModuleId::Asset,
                &[activity_type()],
            ),
        )]));
        let mut core = RecordedCore(crate::prepare::CorePreparationState {
            network_id: 17,
            account_sequence: 5,
            protocol_timestamp: 1_000,
            observed_head_sequence: 88,
            module_registry: registry,
        });
        must(crate::prepare::prepare_activity(
            &mut core,
            crate::prepare::PreparationDefaults {
                timestamp_span: 30,
                fee_limit: layerx_types::amount::Amount::from_u128(12),
                maximum_payload_bytes: 1_024,
            },
            crate::prepare::PrepareRequest {
                actor: must(Did::new(b"did:layerx:expiry")),
                authority: must(layerx_types::activity::Authority::owner(
                    b"external-authority",
                )),
                activity_type: activity_type(),
                expected_account_sequence: Some(5),
                timestamp_bound: Some(must(layerx_types::activity::TimestampBound::new(
                    995, 1_010,
                ))),
                fee_limit: Some(layerx_types::amount::Amount::from_u128(7)),
                idempotency_key: layerx_types::ids::IdempotencyKey::new([4; 32]),
                payload: signed_payload(),
                declared_payload_limit: 1_024,
            },
        ))
    }

    fn daemon_limit(control: &SessionControl) -> LimitId {
        let store = control.store();
        let mut store = must(store.lock());
        must(budget::create_daemon_limit(
            &mut store,
            &control.budgets,
            budget::DaemonLimitRecord {
                tenant: tenant(),
                budget_id: [21; 32],
                limit_id: budget::daemon_limit_id([21; 32]),
                agent_digest: [7; 32],
                asset: [8; 32],
                ceiling: 1_000,
                consumed: 0,
                expiry_ms: 1_000_000,
                revoked: false,
                mutation_key: [21; 32],
                body_digest: [21; 32],
                revoke_key: [0; 32],
            },
            CoreTimestampMs(1),
        ))
        .limit_id
    }

    fn stored_consumed(store: &Store) -> Vec<u128> {
        must(budget::daemon_limits(store, &tenant()))
            .iter()
            .map(|record| record.consumed)
            .collect()
    }

    fn held_preparation(control: &SessionControl, limit: LimitId) -> (TenantKey, Vec<u8>) {
        let held = must(budget::reserve(
            &control.budgets,
            &ReservationRequest {
                id: [7; 32],
                amount: 5,
                expiry_sequence: 1_000,
                current_sequence: OBSERVED,
                applicable_limits: vec![limit],
            },
        ))
        .durable;
        let record = DurablePreparation {
            tenant: tenant(),
            preparation_id: [7; 32],
            session_id: SESSION.0,
            generation: 1,
            not_after: 1_900_000_000,
            payload_hash: [3; 32],
            state: LifecycleState::Prepared,
            activity_id: None,
            holds: held.into_iter().map(|hold| (hold, None)).collect(),
            extensions: std::collections::BTreeMap::new(),
        };
        let key = must(DurablePreparation::store_key(&tenant(), [7; 32]));
        let bytes = must(record.encode());
        let store = control.store();
        must(must(store.lock()).put_local(key.clone(), bytes.clone()));
        (key, bytes)
    }

    #[test]
    fn settle_with_a_failed_durable_write_leaves_the_hold_unpublished() {
        let (root, control, _token) = control("settle-io-failure");
        let limit = daemon_limit(&control);
        let (key, bytes) = held_preparation(&control, limit);
        must(std::fs::remove_dir_all(&root));
        assert!(matches!(
            control.settle_write(&tenant(), [7; 32], ReleaseKind::Executed, OBSERVED),
            Err(SessionControlError::Unavailable)
        ));
        assert_eq!(control.budgets.held_limits([7; 32]), Ok(vec![limit]));
        assert_eq!(control.budgets.held_reservations(), Ok(1));
        assert_eq!(control.budgets.consumed(limit), Ok(0));
        {
            let store = control.store();
            let store = must(store.lock());
            assert_eq!(
                store.get(&key).map(|value| value.bytes().to_vec()),
                Some(bytes)
            );
            assert_eq!(stored_consumed(&store), vec![0]);
        }
        assert!(!root.exists());
        assert!(must(control.registry().read())
            .get(&tenant(), SESSION)
            .is_some());
    }

    #[test]
    fn settle_with_a_durable_write_publishes_the_release_once() {
        let (root, control, _token) = control("settle-io-success");
        let limit = daemon_limit(&control);
        held_preparation(&control, limit);
        assert!(matches!(
            control.settle_write(&tenant(), [7; 32], ReleaseKind::Executed, OBSERVED),
            Ok(true)
        ));
        assert_eq!(control.budgets.held_reservations(), Ok(0));
        assert_eq!(control.budgets.consumed(limit), Ok(5));
        assert!(matches!(
            control.settle_write(&tenant(), [7; 32], ReleaseKind::Executed, OBSERVED),
            Ok(false)
        ));
        assert_eq!(control.budgets.consumed(limit), Ok(5));
        assert_eq!(stored_consumed(&must(Store::open(&root))), vec![5]);
        let _ = std::fs::remove_dir_all(root);
    }

    fn admission(prepared: &Prepared, limit: LimitId) -> WriteAdmission<'_> {
        WriteAdmission {
            stage: AdmissionStage::Prepare { prepared },
            preparation_id: [8; 32],
            charge: Some(WriteCharge {
                amount: 5,
                applicable_limits: vec![limit],
                head_sequence_bound: 1_000,
                core_deadline_ms: None,
            }),
            extensions: Vec::new(),
            current_sequence: OBSERVED,
            core_time_ms: 0,
            planner: None,
        }
    }

    #[test]
    fn admit_write_with_a_failed_durable_write_releases_its_hold() {
        let (root, control, token) = control("admit-io-failure");
        let prepared = prepared_activity();
        let permit = permit(&control, &token);
        must(std::fs::remove_dir_all(&root));
        assert!(matches!(
            permit.admit_write(&control, admission(&prepared, LimitId([1; 16]))),
            Err(SessionControlError::Unavailable)
        ));
        assert_eq!(control.budgets.held_reservations(), Ok(0));
        assert_eq!(control.budgets.consumed(LimitId([1; 16])), Ok(0));
        assert!(matches!(
            control.lifecycle.state([8; 32]),
            Err(LifecycleError::NotFound)
        ));
        let key = must(DurablePreparation::store_key(&tenant(), [8; 32]));
        assert!(must(control.store.lock()).get(&key).is_none());
        assert!(!root.exists());
        assert!(must(control.registry().read())
            .get(&tenant(), SESSION)
            .is_some());
    }

    #[test]
    fn admit_write_with_a_durable_write_holds_once() {
        let (root, control, token) = control("admit-io-success");
        let prepared = prepared_activity();
        let permit = permit(&control, &token);
        let record = must(permit.admit_write(&control, admission(&prepared, LimitId([1; 16]))));
        assert_eq!(record.holds.len(), 1);
        assert_eq!(
            control.budgets.held_limits([8; 32]),
            Ok(vec![LimitId([1; 16])])
        );
        assert_eq!(control.budgets.held_reservations(), Ok(1));
        assert_eq!(control.budgets.consumed(LimitId([1; 16])), Ok(0));
        assert!(matches!(
            control.lifecycle.state([8; 32]),
            Ok(LifecycleState::Prepared)
        ));
        let key = must(DurablePreparation::store_key(&tenant(), [8; 32]));
        assert!(must(Store::open(&root)).get(&key).is_some());
        assert!(matches!(
            permit.admit_write(&control, admission(&prepared, LimitId([1; 16]))),
            Err(SessionControlError::Lifecycle(LifecycleError::Duplicate))
        ));
        assert_eq!(control.budgets.held_reservations(), Ok(1));
        let _ = std::fs::remove_dir_all(root);
    }
    fn second_limit(control: &SessionControl) -> LimitId {
        must(control.budgets.install(LimitConfig {
            id: LimitId([2; 16]),
            name: "session-limit".to_owned(),
            scope: LimitScope::Session([2; 32]),
            ceiling: 1_000,
            consumed: 0,
        }));
        LimitId([2; 16])
    }

    fn submit(limits: Vec<LimitId>) -> WriteAdmission<'static> {
        WriteAdmission {
            stage: AdmissionStage::Submit,
            preparation_id: [8; 32],
            charge: Some(WriteCharge {
                amount: 5,
                applicable_limits: limits,
                head_sequence_bound: 1_000,
                core_deadline_ms: None,
            }),
            extensions: Vec::new(),
            current_sequence: OBSERVED,
            core_time_ms: 0,
            planner: None,
        }
    }

    fn submit_permit(control: &SessionControl, token: &Token) -> OperationPermit {
        must(control.authorize(
            &token.credential(),
            Operation::Submit,
            Surface::Contract,
            OBSERVED,
            None,
        ))
    }

    #[test]
    fn submit_rewrite_with_a_failed_durable_write_leaves_the_limiter_as_it_was() {
        let (root, control, token) = control("submit-io-failure");
        let first = LimitId([1; 16]);
        let second = second_limit(&control);
        let prepared = prepared_activity();
        must(permit(&control, &token).admit_write(&control, admission(&prepared, first)));
        let key = must(DurablePreparation::store_key(&tenant(), [8; 32]));
        let before = must(control.store.lock())
            .get(&key)
            .map(|value| value.bytes().to_vec());
        assert!(before.is_some());
        must(std::fs::remove_dir_all(&root));
        assert!(matches!(
            submit_permit(&control, &token).admit_write(&control, submit(vec![first, second])),
            Err(SessionControlError::Unavailable)
        ));
        assert_eq!(control.budgets.held_limits([8; 32]), Ok(vec![first]));
        assert_eq!(control.budgets.held_reservations(), Ok(1));
        assert_eq!(control.budgets.held_exposure(first), Ok(5));
        assert_eq!(control.budgets.held_exposure(second), Ok(0));
        assert_eq!(control.budgets.consumed(first), Ok(0));
        assert_eq!(control.budgets.consumed(second), Ok(0));
        assert_eq!(
            must(control.store.lock())
                .get(&key)
                .map(|value| value.bytes().to_vec()),
            before
        );
        assert!(!root.exists());
    }

    #[test]
    fn submit_rewrite_with_a_durable_write_holds_each_missing_limit_once() {
        let (root, control, token) = control("submit-io-success");
        let first = LimitId([1; 16]);
        let second = second_limit(&control);
        let prepared = prepared_activity();
        must(permit(&control, &token).admit_write(&control, admission(&prepared, first)));
        let permit = submit_permit(&control, &token);
        let record = must(permit.admit_write(&control, submit(vec![first, second])));
        assert_eq!(
            record
                .holds
                .iter()
                .map(|(hold, _)| hold.limit_id)
                .collect::<Vec<_>>(),
            vec![first, second]
        );
        assert_eq!(
            control.budgets.held_limits([8; 32]),
            Ok(vec![first, second])
        );
        assert_eq!(control.budgets.held_reservations(), Ok(2));
        assert_eq!(control.budgets.held_exposure(first), Ok(5));
        assert_eq!(control.budgets.held_exposure(second), Ok(5));
        let key = must(DurablePreparation::store_key(&tenant(), [8; 32]));
        let stored = must(
            must(Store::open(&root))
                .get(&key)
                .map(|value| value.bytes().to_vec())
                .ok_or("durable preparation"),
        );
        let durable = must(DurablePreparation::decode(tenant(), &stored));
        assert_eq!(durable.holds, record.holds);
        let again = must(permit.admit_write(&control, submit(vec![first, second])));
        assert_eq!(again.holds, record.holds);
        assert_eq!(control.budgets.held_reservations(), Ok(2));
        assert_eq!(control.budgets.held_exposure(second), Ok(5));
        let _ = std::fs::remove_dir_all(root);
    }
}
