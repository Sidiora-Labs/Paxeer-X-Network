use serde::{Deserialize, Serialize};
use sha2::{Digest as _, Sha256};

use crate::agent_rpc_peer::RpcOwnerContext;
use crate::budget::ProgramBudgetReservation;
use crate::policy::native_program::{NativeLocalOutcome, NativeProgramIntent, NativeProgramPolicy};
use crate::prepare::Prepared;
use crate::store::{ObjectKind, StorageClass, Store, TenantId, TenantKey};

const PREFIX: &[u8] = b"native-program-approval-carrier-v1:";
const PRESENTATION_PREFIX: &[u8] = b"native-program-approval-presentation-v2:";
const MAX_CARRIER: usize = 4 * 1024 * 1024;

#[derive(Clone, Debug, Eq, PartialEq)]
pub(crate) enum CarrierError {
    Missing,
    Binding,
    Policy,
    Budget,
    Corrupt,
}

#[derive(Clone, Serialize, Deserialize, Eq, PartialEq)]
#[serde(deny_unknown_fields)]
pub(crate) struct NativeProgramApprovalCarrier {
    version: u8,
    tenant: String,
    principal: String,
    actor: Vec<u8>,
    session: [u8; 32],
    generation: u64,
    preparation: [u8; 32],
    activity_module: u16,
    activity_ordinal: u16,
    canonical_bytes: Vec<u8>,
    authority: RetainedAuthority,
    created_at_sequence: u64,
    budget_expiry_sequence: u64,
    envelope_not_after: u64,
    program_budget: Vec<u8>,
    native_policy_source: Vec<u8>,
    local_policy_outcome: NativeLocalOutcome,
    matched_rules: Vec<String>,
    state: NativeApprovalState,
    terminal: Option<NativeTerminal>,
}

#[derive(Clone, Debug, Serialize, Deserialize, Eq, PartialEq)]
#[serde(deny_unknown_fields)]
pub(crate) struct NativeApprovalPresentation {
    version: u8,
    tenant: String,
    principal: String,
    actor: Vec<u8>,
    session: [u8; 32],
    generation: u64,
    preparation: [u8; 32],
    held_digest: [u8; 32],
    pub(crate) created_at_sequence: u64,
    pub(crate) budget_expiry_sequence: u64,
    pub(crate) created_at_unix_seconds: u64,
    pub(crate) activity_expires_at_unix_milliseconds: u64,
}

pub(crate) struct VerifiedUnsignedProgramCancellation {
    reservation_id: [u8; 32],
    reservation_digest: [u8; 32],
    core_now: crate::budget::CoreTimestampMs,
}

impl VerifiedUnsignedProgramCancellation {
    pub(crate) const fn reservation_id(&self) -> [u8; 32] { self.reservation_id }
    pub(crate) const fn reservation_digest(&self) -> [u8; 32] { self.reservation_digest }
    pub(crate) const fn core_now(&self) -> crate::budget::CoreTimestampMs { self.core_now }
}

pub(crate) struct VerifiedUnsignedProgramRejection {
    reservation_id: [u8; 32],
    reservation_digest: [u8; 32],
}

impl VerifiedUnsignedProgramRejection {
    pub(crate) const fn reservation_id(&self) -> [u8; 32] { self.reservation_id }
    pub(crate) const fn reservation_digest(&self) -> [u8; 32] { self.reservation_digest }
}

impl NativeProgramApprovalCarrier {
    pub(crate) fn bind(
        context: &RpcOwnerContext<'_>,
        prepared: &Prepared,
        policy: &NativeProgramPolicy,
        reservation: &ProgramBudgetReservation,
    ) -> Result<Self, CarrierError> {
        let origin = context.permit().preparation_authorization();
        let preparation: [u8; 32] = Sha256::digest(&prepared.canonical_bytes).into();
        if context.peer().subject.is_none() || context.peer().uid == 0
            || context.peer().principal.is_empty()
            || context.peer().tenant != context.principal().tenant.as_str()
            || origin.session.tenant != context.principal().tenant
            || origin.session.session_id != context.principal().session_id
            || origin.generation == 0
            || prepared.envelope.actor_did() != &context.principal().agent
            || reservation.id != preparation
            || reservation.expiry_sequence <= prepared.observed_head_sequence
        {
            return Err(CarrierError::Binding);
        }
        let intent = NativeProgramIntent::from_prepared(prepared).map_err(|_| CarrierError::Policy)?;
        let decision = policy.evaluate_local(&intent).map_err(|_| CarrierError::Policy)?;
        let carrier = Self {
            version: 1,
            tenant: context.peer().tenant.clone(),
            principal: context.peer().principal.clone(),
            actor: context.principal().agent.as_bytes().to_vec(),
            session: origin.session.session_id.0,
            generation: origin.generation,
            preparation,
            activity_module: decision.activity.module() as u16,
            activity_ordinal: decision.activity.ordinal(),
            canonical_bytes: prepared.canonical_bytes.clone(),
            authority: RetainedAuthority::from_actual(prepared.envelope.authority()),
            created_at_sequence: prepared.observed_head_sequence,
            budget_expiry_sequence: reservation.expiry_sequence,
            envelope_not_after: prepared.envelope.timestamp_bound().not_after(),
            program_budget: reservation.encode().map_err(|_| CarrierError::Budget)?,
            native_policy_source: policy.source().to_vec(),
            local_policy_outcome: decision.outcome,
            matched_rules: decision.matched_rules,
            state: if decision.outcome == NativeLocalOutcome::ApprovalRequired { NativeApprovalState::Awaiting } else { NativeApprovalState::NotRequired },
            terminal: None,
        };
        carrier.encoded()?;
        Ok(carrier)
    }

    pub(crate) fn encoded(&self) -> Result<Vec<u8>, CarrierError> {
        self.validate()?;
        let encoded = serde_json::to_vec(self).map_err(|_| CarrierError::Corrupt)?;
        if encoded.len() > MAX_CARRIER { return Err(CarrierError::Corrupt); }
        Ok(encoded)
    }

    fn validate(&self) -> Result<(), CarrierError> {
        use layerx_types::payload::{ActivityType, ModuleId};
        if self.version != 1 || self.generation == 0 || self.principal.is_empty()
            || self.canonical_bytes.is_empty() || self.canonical_bytes.len() > MAX_CARRIER
            || self.created_at_sequence >= self.budget_expiry_sequence
        {
            return Err(CarrierError::Corrupt);
        }
        TenantId::new(self.tenant.clone()).map_err(|_| CarrierError::Corrupt)?;
        layerx_types::ids::Did::new(&self.actor).map_err(|_| CarrierError::Corrupt)?;
        let module = ModuleId::from_u16(self.activity_module).map_err(|_| CarrierError::Corrupt)?;
        if module != ModuleId::Programs { return Err(CarrierError::Binding); }
        ActivityType::new(module, self.activity_ordinal).map_err(|_| CarrierError::Corrupt)?;
        let digest: [u8; 32] = Sha256::digest(&self.canonical_bytes).into();
        let budget = ProgramBudgetReservation::decode(&self.program_budget).map_err(|_| CarrierError::Budget)?;
        if digest != self.preparation || budget.id != self.preparation
            || budget.expiry_sequence != self.budget_expiry_sequence
            || budget.encode().map_err(|_| CarrierError::Budget)? != self.program_budget
        {
            return Err(CarrierError::Binding);
        }
        match (self.local_policy_outcome, self.state, &self.terminal) {
            (NativeLocalOutcome::Permitted, NativeApprovalState::NotRequired, None)
            | (NativeLocalOutcome::ApprovalRequired, NativeApprovalState::Awaiting, None) => {},
            (NativeLocalOutcome::Permitted, NativeApprovalState::Expired, Some(terminal))
                if terminal.submission_ref.is_none() && !terminal.grant && terminal.generation > 0 => {},
            (NativeLocalOutcome::ApprovalRequired, state, Some(terminal))
                if matches!(state, NativeApprovalState::Granted | NativeApprovalState::Rejected | NativeApprovalState::Expired | NativeApprovalState::Defective)
                && terminal.generation > 0 && !terminal.principal.is_empty()
                && terminal.sequence >= self.created_at_sequence
                && (terminal.submission_ref.is_some() == (state == NativeApprovalState::Granted)) => {},
            _ => return Err(CarrierError::Corrupt),
        }
        NativeProgramPolicy::load(&self.native_policy_source).map_err(|_| CarrierError::Policy)?;
        Ok(())
    }

    pub(crate) fn companion(&self) -> Result<(TenantKey, Vec<u8>), CarrierError> {
        let tenant = TenantId::new(self.tenant.clone()).map_err(|_| CarrierError::Corrupt)?;
        let key = TenantKey::new(tenant, ObjectKind::PreparedActivity,
            [PREFIX, self.preparation.as_slice()].concat()).map_err(|_| CarrierError::Corrupt)?;
        Ok((key, self.encoded()?))
    }

    pub(crate) fn presentation_companion(
        &self,
        clock: &dyn layerx_types::clock::Clock,
    ) -> Result<(TenantKey, Vec<u8>), CarrierError> {
        self.validate()?;
        if self.terminal.is_some()
            || !matches!(self.state, NativeApprovalState::Awaiting | NativeApprovalState::NotRequired)
        {
            return Err(CarrierError::Binding);
        }
        let reading = clock.sample(std::time::Duration::from_secs(1))
            .map_err(|_| CarrierError::Binding)?;
        if reading.generation == [0; 16] || reading.unix_milliseconds >= self.envelope_not_after {
            return Err(CarrierError::Binding);
        }
        let presentation = NativeApprovalPresentation {
            version: 2,
            tenant: self.tenant.clone(),
            principal: self.principal.clone(),
            actor: self.actor.clone(),
            session: self.session,
            generation: self.generation,
            preparation: self.preparation,
            held_digest: self.held_digest()?,
            created_at_sequence: self.created_at_sequence,
            budget_expiry_sequence: self.budget_expiry_sequence,
            created_at_unix_seconds: reading.unix_seconds(),
            activity_expires_at_unix_milliseconds: self.envelope_not_after,
        };
        self.validate_presentation(&presentation)?;
        let encoded = serde_json::to_vec(&presentation).map_err(|_| CarrierError::Corrupt)?;
        if encoded.len() > MAX_CARRIER { return Err(CarrierError::Corrupt); }
        Ok((self.presentation_key()?, encoded))
    }

    pub(crate) fn presentation(&self, store: &Store) -> Result<NativeApprovalPresentation, CarrierError> {
        self.validate()?;
        let stored = store.get(&self.presentation_key()?).ok_or(CarrierError::Missing)?;
        if stored.class() != StorageClass::LocalOnly || stored.bytes().len() > MAX_CARRIER {
            return Err(CarrierError::Corrupt);
        }
        let presentation: NativeApprovalPresentation = serde_json::from_slice(stored.bytes())
            .map_err(|_| CarrierError::Corrupt)?;
        self.validate_presentation(&presentation)?;
        if serde_json::to_vec(&presentation).map_err(|_| CarrierError::Corrupt)?.as_slice() != stored.bytes() {
            return Err(CarrierError::Corrupt);
        }
        Ok(presentation)
    }

    fn presentation_key(&self) -> Result<TenantKey, CarrierError> {
        let tenant = TenantId::new(self.tenant.clone()).map_err(|_| CarrierError::Corrupt)?;
        TenantKey::new(tenant, ObjectKind::PreparedActivity,
            [PRESENTATION_PREFIX, self.preparation.as_slice()].concat())
            .map_err(|_| CarrierError::Corrupt)
    }

    fn validate_presentation(&self, presentation: &NativeApprovalPresentation) -> Result<(), CarrierError> {
        if presentation.version != 2 || presentation.tenant != self.tenant
            || presentation.principal != self.principal || presentation.actor != self.actor
            || presentation.session != self.session || presentation.generation != self.generation
            || presentation.preparation != self.preparation || presentation.held_digest != self.held_digest()?
            || presentation.created_at_sequence != self.created_at_sequence
            || presentation.budget_expiry_sequence != self.budget_expiry_sequence
            || presentation.activity_expires_at_unix_milliseconds != self.envelope_not_after
            || presentation.created_at_unix_seconds.checked_mul(1000)
                .is_none_or(|created| created >= presentation.activity_expires_at_unix_milliseconds)
        {
            return Err(CarrierError::Binding);
        }
        Ok(())
    }

    pub(crate) fn read(
        store: &Store,
        context: &RpcOwnerContext<'_>,
        prepared: &Prepared,
        reservation: &ProgramBudgetReservation,
    ) -> Result<Self, CarrierError> {
        let preparation: [u8; 32] = Sha256::digest(&prepared.canonical_bytes).into();
        let key = TenantKey::new(context.principal().tenant.clone(), ObjectKind::PreparedActivity,
            [PREFIX, preparation.as_slice()].concat()).map_err(|_| CarrierError::Corrupt)?;
        let stored = store.get(&key).ok_or(CarrierError::Missing)?;
        if stored.class() != StorageClass::LocalOnly || stored.bytes().len() > MAX_CARRIER {
            return Err(CarrierError::Corrupt);
        }
        let record: Self = serde_json::from_slice(stored.bytes()).map_err(|_| CarrierError::Corrupt)?;
        if record.encoded()?.as_slice() != stored.bytes() { return Err(CarrierError::Corrupt); }
        let policy = NativeProgramPolicy::load(&record.native_policy_source).map_err(|_| CarrierError::Policy)?;
        let expected = Self::bind(context, prepared, &policy, reservation)?;
        if record.held_digest()? != expected.held_digest()? { return Err(CarrierError::Binding); }
        Ok(record)
    }
}

#[derive(Clone, Serialize, Deserialize, Eq, PartialEq)]
#[serde(tag = "kind", content = "bytes")]
enum RetainedAuthority { Owner(Vec<u8>), SessionKey(Vec<u8>), DelegatedCapability(Vec<u8>), BudgetAllowance(Vec<u8>), Escrow(Vec<u8>), ProtocolModule(Vec<u8>) }

impl RetainedAuthority {
    fn from_actual(authority: &layerx_types::activity::Authority) -> Self {
        use layerx_types::activity::Authority;
        let bytes = authority.as_bytes().to_vec();
        match authority {
            Authority::Owner(_) => Self::Owner(bytes), Authority::SessionKey(_) => Self::SessionKey(bytes),
            Authority::DelegatedCapability(_) => Self::DelegatedCapability(bytes), Authority::BudgetAllowance(_) => Self::BudgetAllowance(bytes),
            Authority::Escrow(_) => Self::Escrow(bytes), Authority::ProtocolModule(_) => Self::ProtocolModule(bytes),
        }
    }
    fn actual(&self) -> Result<layerx_types::activity::Authority, CarrierError> {
        use layerx_types::activity::Authority;
        match self {
            Self::Owner(bytes) => Authority::owner(bytes), Self::SessionKey(bytes) => Authority::session_key(bytes),
            Self::DelegatedCapability(bytes) => Authority::delegated_capability(bytes), Self::BudgetAllowance(bytes) => Authority::budget_allowance(bytes),
            Self::Escrow(bytes) => Authority::escrow(bytes), Self::ProtocolModule(bytes) => Authority::protocol_module(bytes),
        }.map_err(|_| CarrierError::Corrupt)
    }
}

#[derive(Clone, Copy, Debug, Serialize, Deserialize, Eq, PartialEq)]
pub(crate) enum NativeApprovalState { Awaiting, Granted, Rejected, Expired, Defective, NotRequired }

#[derive(Clone, Serialize, Deserialize, Eq, PartialEq)]
#[serde(deny_unknown_fields)]
struct NativeTerminal {
    key: [u8; 32],
    principal: String,
    session: [u8; 32],
    generation: u64,
    sequence: u64,
    core_ms: u64,
    grant: bool,
    submission_ref: Option<[u8; 32]>,
}

impl NativeProgramApprovalCarrier {
    pub(crate) fn held_digest(&self) -> Result<[u8; 32], CarrierError> {
        Ok(Sha256::digest(self.immutable_hold_bytes()?).into())
    }

    pub(crate) fn immutable_hold_bytes(&self) -> Result<Vec<u8>, CarrierError> {
        let mut held = self.clone();
        held.state = if held.local_policy_outcome == NativeLocalOutcome::ApprovalRequired {
            NativeApprovalState::Awaiting
        } else { NativeApprovalState::NotRequired };
        held.terminal = None;
        held.encoded()
    }

    pub(crate) fn budget(&self) -> Result<ProgramBudgetReservation, CarrierError> {
        ProgramBudgetReservation::decode(&self.program_budget).map_err(|_| CarrierError::Budget)
    }

    pub(crate) fn restore_prepared(&self, registry: &layerx_types::payload::ModuleRegistry) -> Result<Prepared, CarrierError> {
        let authority = self.authority.actual()?;
        crate::prepare::restore_canonical(&self.canonical_bytes, self.created_at_sequence, registry, Some(&authority))
            .map_err(|_| CarrierError::Corrupt)
    }

    pub(crate) fn preparation_id(&self) -> [u8; 32] { self.preparation }
    pub(crate) fn state(&self) -> NativeApprovalState { self.state }
    pub(crate) fn requires_approval(&self) -> bool { self.local_policy_outcome == NativeLocalOutcome::ApprovalRequired }
    pub(crate) fn expired_at(&self, sequence: u64, core_ms: u64) -> bool {
        sequence >= self.budget_expiry_sequence || core_ms >= self.envelope_not_after
    }
    pub(crate) fn response(&self) -> Result<layerx_agent_api::identity::NativeApprovalResultV1, CarrierError> {
        let activity = layerx_agent_api::identity::NativeActivity { module: self.activity_module, ordinal: self.activity_ordinal };
        Ok(layerx_agent_api::identity::NativeApprovalResultV1 {
            approval_id: self.preparation, held_digest: self.held_digest()?, activity,
            state: format!("{:?}", self.state), submission_ref: self.terminal.as_ref().and_then(|value| value.submission_ref),
        })
    }

    pub(crate) fn read_id(store: &Store, context: &RpcOwnerContext<'_>, id: [u8; 32]) -> Result<Self, CarrierError> {
        let record = Self::read_retained(store, &context.principal().tenant, id)?;
        if record.actor != context.principal().agent.as_bytes() { return Err(CarrierError::Binding); }
        Ok(record)
    }

    fn read_retained(store: &Store, tenant: &TenantId, id: [u8; 32]) -> Result<Self, CarrierError> {
        let key = TenantKey::new(tenant.clone(), ObjectKind::PreparedActivity,
            [PREFIX, id.as_slice()].concat()).map_err(|_| CarrierError::Corrupt)?;
        let stored = store.get(&key).ok_or(CarrierError::Missing)?;
        if stored.class() != StorageClass::LocalOnly || stored.bytes().len() > MAX_CARRIER { return Err(CarrierError::Corrupt); }
        let record: Self = serde_json::from_slice(stored.bytes()).map_err(|_| CarrierError::Corrupt)?;
        if record.encoded()?.as_slice() != stored.bytes() || record.preparation != id
            || record.tenant != tenant.as_str() {
            return Err(CarrierError::Binding);
        }
        let durable_key = crate::prepare::DurablePreparation::store_key(tenant, id).map_err(|_| CarrierError::Corrupt)?;
        let durable_raw = store.get(&durable_key).ok_or(CarrierError::Missing)?;
        if durable_raw.class() != StorageClass::LocalOnly { return Err(CarrierError::Corrupt); }
        let durable = crate::prepare::DurablePreparation::decode(tenant.clone(), durable_raw.bytes()).map_err(|_| CarrierError::Corrupt)?;
        if durable.preparation_id != id || durable.tenant != *tenant
            || durable.extensions.get(&7).map(Vec::as_slice) != Some(record.held_digest()?.as_slice())
            || durable.extensions.get(&6) != Some(&record.program_budget)
            || durable.session_id != record.session || durable.generation != record.generation {
            return Err(CarrierError::Binding);
        }
        Ok(record)
    }

    pub(crate) fn retained_for_tenant(store: &Store, tenant: &TenantId) -> Result<Vec<Self>, CarrierError> {
        let ids = store.list_object_ids(tenant, ObjectKind::PreparedActivity).into_iter()
            .filter(|id| id.starts_with(PREFIX)).collect::<Vec<_>>();
        if ids.len() > MAX_PENDING_RATE { return Err(CarrierError::Corrupt); }
        ids.into_iter().map(|id| {
            let preparation = id[PREFIX.len()..].try_into().map_err(|_| CarrierError::Corrupt)?;
            Self::read_retained(store, tenant, preparation)
        }).collect()
    }

    fn human_tenant(peer: &crate::human::HumanPeer) -> Result<TenantId, CarrierError> {
        let subject = peer.subject.as_ref().ok_or(CarrierError::Binding)?;
        let namespace = layerx_identity_binding::subject_namespace(&subject.transport_tenant, &peer.principal)
            .map_err(|_| CarrierError::Binding)?;
        let owner = layerx_types::ids::Did::new(subject.owner.as_bytes()).map_err(|_| CarrierError::Binding)?;
        let account = layerx_types::account::AccountId::parse(&subject.account).map_err(|_| CarrierError::Binding)?;
        if peer.uid == 0 || subject.transport_principal.is_empty() || namespace != peer.tenant
            || account.canonical() != subject.account
            || !subject.account.starts_with(&format!("agent:{}:", std::str::from_utf8(owner.as_bytes()).map_err(|_| CarrierError::Binding)?)) {
            return Err(CarrierError::Binding);
        }
        TenantId::new(peer.tenant.clone()).map_err(|_| CarrierError::Binding)
    }

    pub(crate) fn read_for_human(store: &Store, peer: &crate::human::HumanPeer, id: [u8; 32]) -> Result<Self, CarrierError> {
        let tenant = Self::human_tenant(peer)?;
        let record = Self::read_retained(store, &tenant, id)?;
        let subject = peer.subject.as_ref().ok_or(CarrierError::Binding)?;
        if record.principal != peer.principal || !record.human_owner_matches(store, &tenant, subject)? {
            return Err(CarrierError::Binding);
        }
        record.presentation(store)?;
        Ok(record)
    }

    pub(crate) fn list_for_human(store: &Store, peer: &crate::human::HumanPeer) -> Result<Vec<Self>, CarrierError> {
        let tenant = Self::human_tenant(peer)?;
        let subject = peer.subject.as_ref().ok_or(CarrierError::Binding)?;
        let mut result = Vec::new();
        for record in Self::retained_for_tenant(store, &tenant)? {
            if record.principal == peer.principal && record.human_owner_matches(store, &tenant, subject)? {
                record.presentation(store)?;
                result.push(record);
            }
        }
        result.sort_by_key(Self::preparation_id);
        Ok(result)
    }

    fn human_owner_matches(&self, store: &Store, tenant: &TenantId, subject: &crate::human::HumanSubject) -> Result<bool, CarrierError> {
        if self.actor == subject.owner.as_bytes() { return Ok(true); }
        crate::managed_agent::authenticates_native_owner(store, tenant, &subject.owner, &subject.account, &self.actor)
            .map_err(|_| CarrierError::Binding)
    }

    pub(crate) fn retained_principal(&self) -> &str { &self.principal }
    pub(crate) fn retained_actor(&self) -> &[u8] { &self.actor }
    pub(crate) fn retained_session(&self) -> [u8; 32] {
        self.session
    }

    pub(crate) fn unsigned_admission(&self, store: &Store, tenant: &TenantId) -> Result<bool, CarrierError> {
        if !matches!(self.state, NativeApprovalState::Awaiting | NativeApprovalState::NotRequired | NativeApprovalState::Granted) {
            return Ok(false);
        }
        let key = crate::prepare::DurablePreparation::store_key(tenant, self.preparation).map_err(|_| CarrierError::Corrupt)?;
        let stored = store.get(&key).ok_or(CarrierError::Missing)?;
        let durable = crate::prepare::DurablePreparation::decode(tenant.clone(), stored.bytes()).map_err(|_| CarrierError::Corrupt)?;
        let signed = durable.signed_bytes().map_err(|_| CarrierError::Corrupt)?;
        if durable.state != crate::prepare::LifecycleState::Prepared || durable.activity_id.is_some() || signed.is_some() {
            return Ok(false);
        }
        let pending = pending_native_uses(store, tenant)?.into_iter()
            .find(|record| record.preparation == self.preparation).ok_or(CarrierError::Missing)?;
        if pending.canonical_bytes != self.canonical_bytes || pending.actor != self.actor
            || pending.observed_head_sequence != self.created_at_sequence { return Err(CarrierError::Binding); }
        Ok(pending.activity_id.is_none() && pending.committed.is_none())
    }

    fn verified_unsigned_material(
        &self,
        store: &Store,
        lifecycle: &crate::prepare::PreparationLifecycle,
        tenant: &TenantId,
        registry: &layerx_types::payload::ModuleRegistry,
        network_id: u32,
    ) -> Result<Option<(TenantKey, crate::prepare::DurablePreparation, ProgramBudgetReservation)>, CarrierError> {
        if !self.unsigned_admission(store, tenant)? { return Ok(None); }
        let held = self;
        let id = self.preparation;
        let durable_key = crate::prepare::DurablePreparation::store_key(tenant, id).map_err(|_| CarrierError::Corrupt)?;
        let raw = store.get(&durable_key).ok_or(CarrierError::Missing)?;
        let durable = crate::prepare::DurablePreparation::decode(tenant.clone(), raw.bytes())
            .map_err(|_| CarrierError::Corrupt)?;
        let signed = durable.signed_bytes().map_err(|_| CarrierError::Corrupt)?;
        if durable.state != crate::prepare::LifecycleState::Prepared || durable.activity_id.is_some() || signed.is_some() {
            return Ok(None);
        }
        let pending = pending_native_uses(store, tenant)?.into_iter()
            .find(|record| record.preparation == id).ok_or(CarrierError::Missing)?;
        if pending.canonical_bytes != held.canonical_bytes || pending.actor != held.actor
            || pending.observed_head_sequence != held.created_at_sequence {
            return Err(CarrierError::Binding);
        }
        if pending.activity_id.is_some() || pending.committed.is_some() { return Ok(None); }
        let prepared = held.restore_prepared(registry)?;
        if prepared.envelope.network_id() != network_id
            || prepared.envelope.actor_did().as_bytes() != held.actor
            || prepared.envelope.activity_type().module() as u16 != held.activity_module
            || prepared.envelope.activity_type().ordinal() != held.activity_ordinal
            || prepared.envelope.timestamp_bound().not_after() != held.envelope_not_after
            || durable.not_after != held.envelope_not_after
            || durable.payload_hash != prepared.envelope.payload_hash()
            || !durable.holds.is_empty()
            || lifecycle.state(id).map_err(|_| CarrierError::Corrupt)? != crate::prepare::LifecycleState::Prepared
        { return Err(CarrierError::Binding); }
        if durable.extensions.get(&crate::prepare::EXTENSION_IDEMPOTENCY).map(Vec::as_slice)
            != Some(prepared.audit.idempotency_key.as_slice())
            || durable.extensions.contains_key(&crate::prepare::EXTENSION_OUTCOME)
        { return Err(CarrierError::Binding); }
        let outbox_key = TenantKey::new(tenant.clone(), ObjectKind::Outbox,
            prepared.audit.idempotency_key.to_vec()).map_err(|_| CarrierError::Corrupt)?;
        let signed_key = TenantKey::new(tenant.clone(), ObjectKind::PreparedActivity,
            prepared.audit.idempotency_key.to_vec()).map_err(|_| CarrierError::Corrupt)?;
        if store.get(&outbox_key).is_some() || store.get(&signed_key).is_some()
            || crate::budget::program_settlement_recorded(store, tenant, id).map_err(|_| CarrierError::Budget)?
        { return Err(CarrierError::Binding); }
        let reservation = held.budget()?;
        Ok(Some((durable_key, durable, reservation)))
    }

    pub(crate) fn expire_unsigned(
        store: &mut Store,
        budgets: &crate::budget::BudgetLimiter,
        lifecycle: &crate::prepare::PreparationLifecycle,
        tenant: &TenantId,
        id: [u8; 32],
        observation: &crate::protocol_evidence::AuthenticatedCoreTime,
        registry: &layerx_types::payload::ModuleRegistry,
        network_id: u32,
    ) -> Result<Option<Self>, CarrierError> {
        let mut held = Self::read_retained(store, tenant, id)?;
        let sequence = observation.through_sequence();
        let core_ms = observation.observed_core_ms();
        if sequence < held.created_at_sequence { return Err(CarrierError::Binding); }
        if !held.expired_at(sequence, core_ms) { return Ok(None); }
        let Some((durable_key, mut durable, reservation)) =
            held.verified_unsigned_material(store, lifecycle, tenant, registry, network_id)?
        else { return Ok(None); };
        let mut digest = Sha256::new();
        digest.update(b"layerx/native-program-expiry/v1\0");
        digest.update(id);
        let key = digest.finalize().into();
        held.state = NativeApprovalState::Expired;
        held.terminal = Some(NativeTerminal { key, principal: held.principal.clone(),
            session: held.session, generation: held.generation, sequence, core_ms, grant: false, submission_ref: None });
        durable.state = crate::prepare::LifecycleState::Expired;
        let kind = if sequence >= held.budget_expiry_sequence {
            crate::budget::ReleaseKind::Expired
        } else {
            crate::budget::ReleaseKind::Failed
        };
        let staged = if kind == crate::budget::ReleaseKind::Failed && reservation.allocations().is_some() {
            let proof = VerifiedUnsignedProgramCancellation {
                reservation_id: id,
                reservation_digest: reservation.settlement_binding().map_err(|_| CarrierError::Budget)?,
                core_now: crate::budget::CoreTimestampMs(core_ms),
            };
            budgets.stage_program_unsigned_cancellation(&reservation, &proof)
        } else {
            crate::budget::stage_release(budgets, id, kind, sequence)
        }.map_err(|_| CarrierError::Budget)?;
        let (carrier_key, carrier_bytes) = held.companion()?;
        store.apply_program_approval_batch(
            vec![(carrier_key, carrier_bytes), (durable_key, durable.encode().map_err(|_| CarrierError::Corrupt)?)],
            Vec::new(), vec![native_rate_key(tenant, id)?]).map_err(|_| CarrierError::Corrupt)?;
        let _ = staged.publish();
        lifecycle.invalidate_preparations(&std::collections::BTreeSet::from([id]), sequence, budgets)
            .map_err(|_| CarrierError::Corrupt)?;
        Ok(Some(held))
    }

    pub(crate) fn list(store: &Store, context: &RpcOwnerContext<'_>) -> Result<Vec<Self>, CarrierError> {
        let mut result = Vec::new();
        for id in store.list_object_ids(&context.principal().tenant, ObjectKind::PreparedActivity) {
            if !id.starts_with(PREFIX) { continue; }
            let preparation = id[PREFIX.len()..].try_into().map_err(|_| CarrierError::Corrupt)?;
            match Self::read_id(store, context, preparation) {
                Ok(record) => result.push(record),
                Err(CarrierError::Binding) => {},
                Err(error) => return Err(error),
            }
        }
        Ok(result)
    }

    pub(crate) fn decide(
        store: &mut Store, budgets: &crate::budget::BudgetLimiter,
        context: &RpcOwnerContext<'_>, id: [u8; 32], held_digest: [u8; 32], key: [u8; 32],
        grant: bool, observation: &crate::protocol_evidence::AuthenticatedCoreTime,
        registry: &layerx_types::payload::ModuleRegistry, network_id: u32, lifecycle: &crate::prepare::PreparationLifecycle,
    ) -> Result<Self, CarrierError> {
        let sequence = observation.through_sequence();
        let core_ms = observation.observed_core_ms();
        let mut held = Self::read_id(store, context, id)?;
        if held.held_digest()? != held_digest || sequence < held.created_at_sequence { return Err(CarrierError::Binding); }
        let origin = context.permit().preparation_authorization();
        if let Some(terminal) = &held.terminal {
            if terminal.key == key && terminal.principal == context.peer().principal
                && terminal.session == origin.session.session_id.0 && terminal.generation == origin.generation
                && terminal.grant == grant { return Ok(held); }
            return Err(CarrierError::Binding);
        }
        if held.expired_at(sequence, core_ms) {
            if let Some(expired) = Self::expire_unsigned(store, budgets, lifecycle, &context.principal().tenant,
                id, observation, registry, network_id)? { return Ok(expired); }
            return Err(CarrierError::Binding);
        }
        if held.state != NativeApprovalState::Awaiting { return Err(CarrierError::Binding); }
        let rejection = if grant {
            None
        } else {
            if context.peer().uid == 0 || context.peer().subject.is_none()
                || context.peer().tenant != context.principal().tenant.as_str()
                || context.peer().principal.is_empty() || origin.generation == 0
                || origin.session.tenant != context.principal().tenant
                || origin.session.session_id != context.principal().session_id
            { return Err(CarrierError::Binding); }
            Some(held.verified_unsigned_material(store, lifecycle, &context.principal().tenant, registry, network_id)?
                .ok_or(CarrierError::Binding)?)
        };
        held.state = if grant { NativeApprovalState::Granted } else { NativeApprovalState::Rejected };
        let submission_ref = if held.state == NativeApprovalState::Granted {
            let mut digest = Sha256::new(); digest.update(b"layerx/native-program-release/v1\0");
            digest.update(held_digest); digest.update(id); Some(digest.finalize().into())
        } else { None };
        held.terminal = Some(NativeTerminal { key, principal: context.peer().principal.clone(),
            session: origin.session.session_id.0, generation: origin.generation, sequence, core_ms, grant, submission_ref });
        let (carrier_key, carrier_bytes) = held.companion()?;
        if held.state == NativeApprovalState::Granted {
            store.apply_program_approval_batch(vec![(carrier_key, carrier_bytes)], Vec::new(), Vec::new())
                .map_err(|_| CarrierError::Corrupt)?;
        } else {
            let tenant = &context.principal().tenant;
            let (durable_key, mut durable, reservation) = rejection.ok_or(CarrierError::Binding)?;
            durable.state = crate::prepare::LifecycleState::Failed;
            let staged = if reservation.allocations().is_some() {
                let proof = VerifiedUnsignedProgramRejection {
                    reservation_id: id,
                    reservation_digest: reservation.settlement_binding().map_err(|_| CarrierError::Budget)?,
                };
                budgets.stage_program_unsigned_rejection(&reservation, &proof)
            } else {
                crate::budget::stage_release(budgets, id, crate::budget::ReleaseKind::Failed, sequence)
            }.map_err(|_| CarrierError::Budget)?;
            store.apply_program_approval_batch(vec![(carrier_key, carrier_bytes), (durable_key, durable.encode().map_err(|_| CarrierError::Corrupt)?)],
                Vec::new(), vec![native_rate_key(tenant, id)?]).map_err(|_| CarrierError::Corrupt)?;
            let _ = staged.publish();
            lifecycle.invalidate_preparations(&std::collections::BTreeSet::from([id]), sequence, budgets)
                .map_err(|_| CarrierError::Corrupt)?;
        }
        Ok(held)
    }

    pub(crate) fn decide_for_human(
        store: &mut Store,
        budgets: &crate::budget::BudgetLimiter,
        peer: &crate::human::HumanPeer,
        session: &crate::session::SessionRecord,
        id: [u8; 32],
        held_digest: [u8; 32],
        key: [u8; 32],
        grant: bool,
        observation: &crate::protocol_evidence::AuthenticatedCoreTime,
        registry: &layerx_types::payload::ModuleRegistry,
        network_id: u32,
        lifecycle: &crate::prepare::PreparationLifecycle,
    ) -> Result<Self, CarrierError> {
        let sequence = observation.through_sequence();
        let core_ms = observation.observed_core_ms();
        let tenant = Self::human_tenant(peer)?;
        let mut held = Self::read_for_human(store, peer, id)?;
        if key == [0; 32]
            || !session.open
            || session.request.tenant != tenant
            || session.request.session_id.0 != held.session
            || session.request.agent.as_bytes() != held.actor
            || session.generation != held.generation
            || sequence >= session.request.expiry_sequence
            || session
                .request
                .expiry_seconds
                .is_some_and(|expiry| core_ms / 1000 >= expiry)
        {
            return Err(CarrierError::Binding);
        }
        if held.held_digest()? != held_digest || sequence < held.created_at_sequence {
            return Err(CarrierError::Binding);
        }
        if let Some(terminal) = &held.terminal {
            if terminal.key == key
                && terminal.principal == peer.principal
                && terminal.session == held.session
                && terminal.generation == held.generation
                && terminal.grant == grant
            {
                return Ok(held);
            }
            return Err(CarrierError::Binding);
        }
        if held.expired_at(sequence, core_ms) {
            if let Some(expired) = Self::expire_unsigned(
                store,
                budgets,
                lifecycle,
                &tenant,
                id,
                observation,
                registry,
                network_id,
            )? {
                return Ok(expired);
            }
            return Err(CarrierError::Binding);
        }
        if held.state != NativeApprovalState::Awaiting {
            return Err(CarrierError::Binding);
        }
        let rejection = if grant {
            if held
                .verified_unsigned_material(store, lifecycle, &tenant, registry, network_id)?
                .is_none()
            {
                return Err(CarrierError::Binding);
            }
            None
        } else {
            Some(
                held.verified_unsigned_material(store, lifecycle, &tenant, registry, network_id)?
                    .ok_or(CarrierError::Binding)?,
            )
        };
        held.state = if grant {
            NativeApprovalState::Granted
        } else {
            NativeApprovalState::Rejected
        };
        let submission_ref = if held.state == NativeApprovalState::Granted {
            let mut digest = Sha256::new();
            digest.update(b"layerx/native-program-release/v1\0");
            digest.update(held_digest);
            digest.update(id);
            Some(digest.finalize().into())
        } else {
            None
        };
        let decision_session = held.session;
        let decision_generation = held.generation;
        held.terminal = Some(NativeTerminal {
            key,
            principal: peer.principal.clone(),
            session: decision_session,
            generation: decision_generation,
            sequence,
            core_ms,
            grant,
            submission_ref,
        });
        let (carrier_key, carrier_bytes) = held.companion()?;
        if held.state == NativeApprovalState::Granted {
            store
                .apply_program_approval_batch(
                    vec![(carrier_key, carrier_bytes)],
                    Vec::new(),
                    Vec::new(),
                )
                .map_err(|_| CarrierError::Corrupt)?;
        } else {
            let tenant = &tenant;
            let (durable_key, mut durable, reservation) = rejection.ok_or(CarrierError::Binding)?;
            durable.state = crate::prepare::LifecycleState::Failed;
            let staged = if reservation.allocations().is_some() {
                let proof = VerifiedUnsignedProgramRejection {
                    reservation_id: id,
                    reservation_digest: reservation
                        .settlement_binding()
                        .map_err(|_| CarrierError::Budget)?,
                };
                budgets.stage_program_unsigned_rejection(&reservation, &proof)
            } else {
                crate::budget::stage_release(
                    budgets,
                    id,
                    crate::budget::ReleaseKind::Failed,
                    sequence,
                )
            }
            .map_err(|_| CarrierError::Budget)?;
            store
                .apply_program_approval_batch(
                    vec![
                        (carrier_key, carrier_bytes),
                        (
                            durable_key,
                            durable.encode().map_err(|_| CarrierError::Corrupt)?,
                        ),
                    ],
                    Vec::new(),
                    vec![native_rate_key(tenant, id)?],
                )
                .map_err(|_| CarrierError::Corrupt)?;
            let _ = staged.publish();
            lifecycle
                .invalidate_preparations(&std::collections::BTreeSet::from([id]), sequence, budgets)
                .map_err(|_| CarrierError::Corrupt)?;
        }
        Ok(held)
    }

    pub(crate) fn authorize_submit(&self, context: &RpcOwnerContext<'_>, prepared: &Prepared,
        submission_ref: Option<[u8; 32]>, sequence: u64, core_ms: u64) -> Result<(), CarrierError> {
        let origin = context.permit().preparation_authorization();
        if self.session != origin.session.session_id.0 || self.generation != origin.generation
            || self.principal != context.peer().principal || self.canonical_bytes != prepared.canonical_bytes
            || sequence < self.created_at_sequence || sequence >= self.budget_expiry_sequence || core_ms >= self.envelope_not_after {
            return Err(CarrierError::Binding);
        }
        match self.state {
            NativeApprovalState::NotRequired if submission_ref.is_none() => Ok(()),
            NativeApprovalState::Granted if self.terminal.as_ref().is_some_and(|terminal| terminal.submission_ref == submission_ref && submission_ref.is_some()) => Ok(()),
            _ => Err(CarrierError::Binding),
        }
    }
}

const RATE_PREFIX: &[u8] = b"native-program-rate-v1:";
const MAX_PENDING_RATE: usize = 4096;

#[derive(Clone, Serialize, Deserialize, Eq, PartialEq)]
#[serde(deny_unknown_fields)]
struct PendingNativeUse {
    version: u8,
    tenant: String,
    actor: Vec<u8>,
    preparation: [u8; 32],
    canonical_bytes: Vec<u8>,
    chain: Vec<[u8; 32]>,
    observed_head_sequence: u64,
    admitted_at_ms: u64,
    activity_id: Option<[u8; 32]>,
    committed: Option<(u64, u64)>,
}

impl PendingNativeUse {
    fn encode(&self) -> Result<Vec<u8>, CarrierError> {
        if self.version != 1 || self.chain.is_empty() || self.chain.len() > 64
            || self.chain.iter().collect::<std::collections::BTreeSet<_>>().len() != self.chain.len()
            || self.admitted_at_ms == 0 || self.canonical_bytes.is_empty()
            || self.canonical_bytes.len() > MAX_CARRIER
            || <[u8; 32]>::from(Sha256::digest(&self.canonical_bytes)) != self.preparation {
            return Err(CarrierError::Corrupt);
        }
        TenantId::new(self.tenant.clone()).map_err(|_| CarrierError::Corrupt)?;
        layerx_types::ids::Did::new(&self.actor).map_err(|_| CarrierError::Corrupt)?;
        let bytes = serde_json::to_vec(self).map_err(|_| CarrierError::Corrupt)?;
        if bytes.len() > MAX_CARRIER { return Err(CarrierError::Corrupt); }
        Ok(bytes)
    }

    fn key(&self) -> Result<TenantKey, CarrierError> {
        native_rate_key(&TenantId::new(self.tenant.clone()).map_err(|_| CarrierError::Corrupt)?, self.preparation)
    }
}

fn native_rate_key(tenant: &TenantId, preparation: [u8; 32]) -> Result<TenantKey, CarrierError> {
    TenantKey::new(tenant.clone(), ObjectKind::PreparedActivity,
        [RATE_PREFIX, preparation.as_slice()].concat()).map_err(|_| CarrierError::Corrupt)
}

fn pending_native_uses(store: &Store, tenant: &TenantId) -> Result<Vec<PendingNativeUse>, CarrierError> {
    let ids = store.list_object_ids(tenant, ObjectKind::PreparedActivity).into_iter()
        .filter(|id| id.starts_with(RATE_PREFIX)).collect::<Vec<_>>();
    if ids.len() > MAX_PENDING_RATE { return Err(CarrierError::Corrupt); }
    ids.into_iter().map(|id| {
        let key = TenantKey::new(tenant.clone(), ObjectKind::PreparedActivity, id).map_err(|_| CarrierError::Corrupt)?;
        let stored = store.get(&key).ok_or(CarrierError::Missing)?;
        if stored.class() != StorageClass::LocalOnly || stored.bytes().len() > MAX_CARRIER {
            return Err(CarrierError::Corrupt);
        }
        let record: PendingNativeUse = serde_json::from_slice(stored.bytes()).map_err(|_| CarrierError::Corrupt)?;
        if record.tenant != tenant.as_str() || record.key()? != key || record.encode()?.as_slice() != stored.bytes() {
            return Err(CarrierError::Corrupt);
        }
        Ok(record)
    }).collect()
}

pub(crate) struct StagedNativeRateUse {
    record: PendingNativeUse,
    previous: Vec<PendingNativeUse>,
    counts: std::collections::BTreeMap<[u8; 32], std::collections::BTreeMap<u64, u64>>,
    observed_batch: [u8; 32],
}

impl StagedNativeRateUse {
    pub(crate) fn stage(
        store: &Store,
        constraints: &crate::capability::binding::NativeCapabilityConstraintsV1,
        prepared: &Prepared,
        evidence: &[crate::protocol_evidence::AuthenticatedTimeWindowUse],
    ) -> Result<Self, CarrierError> {
        let binding = constraints.binding();
        crate::prepare::verify_disclosure_binding(prepared).map_err(|_| CarrierError::Binding)?;
        let digest: [u8; 32] = Sha256::digest(&prepared.canonical_bytes).into();
        let windows = constraints.rate_obligations().values().flat_map(|rates| rates.keys().copied())
            .collect::<std::collections::BTreeSet<_>>();
        if digest != binding.canonical_digest() || digest != binding.preparation_id()
            || prepared.envelope.actor_did() != binding.agent()
            || constraints.chain().is_empty() || windows.is_empty() || evidence.len() != windows.len()
            || prepared.observed_head_sequence.checked_add(1).is_none() {
            return Err(CarrierError::Binding);
        }
        let mut proofs = std::collections::BTreeMap::new();
        let mut observed_batch = None;
        for proof in evidence {
            if proof.actor() != binding.agent() || proof.through_sequence() != prepared.observed_head_sequence
                || proof.observed_core_ms() != constraints.observed_at_ms()
                || !windows.contains(&proof.window_seconds())
                || observed_batch.is_some_and(|batch| batch != proof.observed_batch_id())
                || proofs.insert(proof.window_seconds(), proof).is_some() {
                return Err(CarrierError::Binding);
            }
            observed_batch = Some(proof.observed_batch_id());
        }
        let previous = pending_native_uses(store, binding.tenant())?;
        if previous.len() >= MAX_PENDING_RATE || previous.iter().any(|record| record.preparation == digest) {
            return Err(CarrierError::Binding);
        }
        let mut counts = std::collections::BTreeMap::new();
        for (capability, rates) in constraints.rate_obligations() {
            let mut actual = std::collections::BTreeMap::new();
            for (window, maximum) in rates {
                let proof = proofs.get(window).ok_or(CarrierError::Binding)?;
                let mut count = proof.count();
                for pending in previous.iter().filter(|record| record.actor == binding.agent().as_bytes()) {
                    if pending.observed_head_sequence > proof.through_sequence() || pending.admitted_at_ms > proof.observed_core_ms() {
                        return Err(CarrierError::Binding);
                    }
                    if pending.activity_id.is_some_and(|id| proof.activity_ids().contains(&id)) { continue; }
                    if let Some((sequence, timestamp)) = pending.committed {
                        if sequence > proof.through_sequence() || timestamp > proof.observed_core_ms() { return Err(CarrierError::Binding); }
                        let lower = window.checked_mul(1000).and_then(|span| proof.observed_core_ms().checked_sub(span));
                        if lower.is_some_and(|bound| timestamp <= bound) { continue; }
                        return Err(CarrierError::Binding);
                    }
                    count = count.checked_add(1).ok_or(CarrierError::Binding)?;
                }
                if count >= *maximum { return Err(CarrierError::Binding); }
                actual.insert(*window, count);
            }
            counts.insert(*capability, actual);
        }
        Ok(Self {
            record: PendingNativeUse {
                version: 1, tenant: binding.tenant().as_str().to_owned(), actor: binding.agent().as_bytes().to_vec(),
                preparation: digest, canonical_bytes: prepared.canonical_bytes.clone(), chain: constraints.chain().to_vec(),
                observed_head_sequence: prepared.observed_head_sequence, admitted_at_ms: constraints.observed_at_ms(), activity_id: None, committed: None,
            }, previous, counts, observed_batch: observed_batch.ok_or(CarrierError::Binding)?,
        })
    }

    pub(crate) fn counts(&self) -> &std::collections::BTreeMap<[u8; 32], std::collections::BTreeMap<u64, u64>> { &self.counts }
    pub(crate) fn observed_batch(&self) -> [u8; 32] { self.observed_batch }
    pub(crate) fn observed_head_sequence(&self) -> u64 { self.record.observed_head_sequence }
    pub(crate) fn next_execution_sequence(&self) -> Result<u64, CarrierError> {
        self.record.observed_head_sequence.checked_add(1).ok_or(CarrierError::Binding)
    }

    pub(crate) fn commit_joined(
        self,
        store: &mut Store,
        updates: Vec<(TenantKey, Vec<u8>)>,
        mut inserts: Vec<(TenantKey, Vec<u8>)>,
    ) -> Result<(), CarrierError> {
        let tenant = TenantId::new(self.record.tenant.clone()).map_err(|_| CarrierError::Corrupt)?;
        if pending_native_uses(store, &tenant)? != self.previous { return Err(CarrierError::Binding); }
        inserts.push((self.record.key()?, self.record.encode()?));
        store.apply_program_approval_batch(updates, inserts, Vec::new()).map_err(|_| CarrierError::Corrupt)
    }
}

pub(crate) fn native_rate_submission_update(
    store: &Store,
    tenant: &TenantId,
    prepared: &Prepared,
    submission: &crate::sign::VerifiedSubmission,
    registry: &layerx_types::payload::ModuleRegistry,
) -> Result<(TenantKey, Vec<u8>), CarrierError> {
    let preparation: [u8; 32] = Sha256::digest(&prepared.canonical_bytes).into();
    let mut record = pending_native_uses(store, tenant)?.into_iter()
        .find(|record| record.preparation == preparation).ok_or(CarrierError::Missing)?;
    let signed = layerx_wire::activity::decode_signed(submission.exact_bytes(), registry).map_err(|_| CarrierError::Binding)?;
    if layerx_wire::activity::encode_unsigned(&signed).map_err(|_| CarrierError::Binding)? != record.canonical_bytes
        || record.canonical_bytes != prepared.canonical_bytes || record.actor != prepared.envelope.actor_did().as_bytes()
        || layerx_wire::hash::activity_id(&signed).map_err(|_| CarrierError::Binding)? != submission.activity_id()
        || record.activity_id.is_some_and(|id| id != submission.activity_id()) {
        return Err(CarrierError::Binding);
    }
    record.activity_id = Some(submission.activity_id());
    Ok((record.key()?, record.encode()?))
}


pub(crate) fn native_rate_receipt_update(
    store: &Store, tenant: &TenantId, receipt: &crate::protocol_evidence::VerifiedReceiptEvidence,
) -> Result<Option<(TenantKey, Vec<u8>)>, CarrierError> {
    let mut matches = pending_native_uses(store, tenant)?.into_iter()
        .filter(|record| record.activity_id == Some(receipt.activity_id()));
    let Some(mut record) = matches.next() else { return Ok(None); };
    if matches.next().is_some() { return Err(CarrierError::Corrupt); }
    let decoded = layerx_wire::receipt::decode(receipt.canonical_receipt()).map_err(|_| CarrierError::Corrupt)?;
    let protocol = decoded.protocol().ok_or(CarrierError::Binding)?;
    let committed = (receipt.global_sequence(), protocol.timestamp());
    if protocol.activity_id() != receipt.activity_id() || committed.0 <= record.observed_head_sequence
        || committed.1 < record.admitted_at_ms || record.committed.is_some_and(|previous| previous != committed) {
        return Err(CarrierError::Binding);
    }
    record.committed = Some(committed);
    Ok(Some((record.key()?, record.encode()?)))
}
