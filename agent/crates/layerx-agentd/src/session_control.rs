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
        record.state = state;
        record.drop_signed_bytes();
        if let Some(extension) = extension {
            record.extensions.insert(extension.tag, extension.bytes);
        }
        let mut updates = vec![(key, record.encode().map_err(lifecycle)?)];
        if outcome == ReleaseKind::Executed {
            let holds: Vec<DurableBudgetReservation> =
                record.holds.iter().map(|(hold, _)| hold.clone()).collect();
            updates.extend(
                budget::consumption_updates(&store, tenant, &holds)
                    .map_err(|_| SessionControlError::Unavailable)?,
            );
        }
        store
            .update_local_batch(updates)
            .map_err(|_| SessionControlError::Unavailable)?;
        budget::release(&self.budgets, preparation_id, outcome, current_sequence)
            .map_err(|refusal| lifecycle(LifecycleError::Reservation(refusal)))
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
        Ok(holds.len())
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
            tenant::resolve(&token, &registry, &request, &mut observability)
                .map_err(SessionControlError::Authorization)?
        };
        let stop = registry
            .revocation_stop(&token)
            .map_err(SessionControlError::Session)?;
        Ok(OperationPermit {
            token,
            request,
            principal,
            stop,
        })
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
}

/// Exact-generation authorization retained across a bounded daemon operation.
pub struct OperationPermit {
    token: Token,
    request: RequestContext,
    principal: ResolvedPrincipal,
    stop: StopSignal,
}

impl OperationPermit {
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
        let lifecycle = SessionControlError::Lifecycle;
        let operation = match admission.stage {
            AdmissionStage::Prepare { .. } => Operation::Prepare,
            AdmissionStage::Sign => Operation::Sign,
            AdmissionStage::Submit => Operation::Submit,
        };
        self.require_operation(operation)?;
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
        // ponytail: a failed rewrite keeps the added in-memory hold (over-holds, never
        // under-holds) until restart restores the durable set; per-limit release if it matters.
        let added = reserve_charge(
            &control.budgets,
            preparation_id,
            charge,
            missing,
            admission.current_sequence,
            admission.core_time_ms,
        )?;
        record.holds.extend(added);
        let bytes = record.encode().map_err(lifecycle)?;
        store
            .update_local_batch(vec![(key, bytes)])
            .map_err(|_| SessionControlError::Unavailable)?;
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
