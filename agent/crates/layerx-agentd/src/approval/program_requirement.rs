use serde::{Deserialize, Serialize};
use sha2::{Digest as _, Sha256};

use crate::agent_rpc_peer::RpcOwnerContext;
use crate::approval::ApprovalSubmissionQueue;
use crate::policy::approval::{ApprovalSnapshot, ApprovalState};
use crate::policy::{DecisionReason, EvaluationInput, Outcome, PolicySnapshot};
use crate::prepare::{DurablePreparation, Prepared};
use crate::store::{ObjectKind, StorageClass, Store, TenantId, TenantKey};

const PREFIX: &[u8] = b"program-required-approval-v1:";
pub(crate) const PROGRAM_REQUIREMENT_EXTENSION: u16 = 5;
const MAX_RECORD: usize = 4 * 1024 * 1024;

#[derive(Clone, Debug, Eq, PartialEq)]
pub(crate) enum Refusal {
    Missing,
    Corrupt,
    Binding,
    Policy,
    Required,
    Terminal,
    Conflict,
}

#[derive(Clone, Serialize, Deserialize, Eq, PartialEq)]
#[serde(deny_unknown_fields)]
struct Binding {
    tenant: String,
    principal: String,
    actor: Vec<u8>,
    session: [u8; 32],
    generation: u64,
    preparation: [u8; 32],
    idempotency: [u8; 32],
    canonical_len: u64,
    canonical_digest: [u8; 32],
    payload_hash: [u8; 32],
    prepared_at: u64,
}

#[derive(Clone, Copy, Serialize, Deserialize, Eq, PartialEq)]
#[serde(tag = "kind", deny_unknown_fields)]
enum Requirement {
    NotRequired,
    Required { approval_id: [u8; 32], expires_at: u64 },
    Denied,
}

#[derive(Clone, Serialize, Deserialize, Eq, PartialEq)]
#[serde(deny_unknown_fields)]
struct Immutable {
    version: u8,
    binding: Binding,
    policy_source: String,
    policy_generation: u64,
    policy_version: String,
    context: Vec<u8>,
    matched_rules: Vec<String>,
    deciding_rule: Option<String>,
    requirement: Requirement,
}

#[derive(Clone, Serialize, Deserialize, Eq, PartialEq)]
#[serde(deny_unknown_fields)]
struct DecisionStamp {
    requirement_digest: [u8; 32],
    approval_id: [u8; 32],
    decision_key: String,
    approver: Option<String>,
    approver_actor: Option<Vec<u8>>,
    approver_session: Option<[u8; 32]>,
    approver_generation: Option<u64>,
    sequence: u64,
    outcome: Terminal,
    submission_ref: Option<[u8; 32]>,
}

#[derive(Clone, Copy, Debug, Serialize, Deserialize, Eq, PartialEq)]
pub(crate) enum Terminal {
    Granted,
    Rejected,
    Expired,
    Defective,
}

#[derive(Clone, Serialize, Deserialize, Eq, PartialEq)]
#[serde(deny_unknown_fields)]
pub(crate) struct Record {
    immutable: Immutable,
    decision: Option<DecisionStamp>,
}

fn digest(bytes: &[u8]) -> [u8; 32] {
    Sha256::digest(bytes).into()
}

fn key(tenant: &TenantId, preparation: [u8; 32]) -> Result<TenantKey, Refusal> {
    TenantKey::new(
        tenant.clone(),
        ObjectKind::Configuration,
        [PREFIX, preparation.as_slice()].concat(),
    )
    .map_err(|_| Refusal::Corrupt)
}

fn binding(context: &RpcOwnerContext<'_>, prepared: &Prepared) -> Result<Binding, Refusal> {
    let principal = context.principal();
    let peer = context.peer();
    let origin = context.permit().preparation_authorization();
    if peer.subject.is_none()
        || peer.uid == 0
        || peer.principal.is_empty()
        || peer.tenant != principal.tenant.as_str()
        || origin.session.tenant != principal.tenant
        || origin.session.session_id != principal.session_id
        || origin.generation == 0
        || prepared.envelope.actor_did() != &principal.agent
        || prepared.envelope.idempotency_key().bytes() != prepared.audit.idempotency_key
        || layerx_wire::activity::encode_unsigned_envelope(&prepared.envelope)
            .map_err(|_| Refusal::Binding)? != prepared.canonical_bytes
    {
        return Err(Refusal::Binding);
    }
    let hash = digest(&prepared.canonical_bytes);
    Ok(Binding {
        tenant: peer.tenant.clone(),
        principal: peer.principal.clone(),
        actor: principal.agent.as_bytes().to_vec(),
        session: origin.session.session_id.0,
        generation: origin.generation,
        preparation: hash,
        idempotency: prepared.audit.idempotency_key,
        canonical_len: u64::try_from(prepared.canonical_bytes.len()).map_err(|_| Refusal::Corrupt)?,
        canonical_digest: hash,
        payload_hash: prepared.envelope.payload_hash(),
        prepared_at: prepared.observed_head_sequence,
    })
}

impl Record {
    pub(crate) fn evaluate(
        context: &RpcOwnerContext<'_>,
        prepared: &Prepared,
        snapshot: &PolicySnapshot,
        input: &EvaluationInput<'_>,
        approval: Option<&ApprovalSnapshot>,
    ) -> Result<Self, Refusal> {
        let source = snapshot.source().ok_or(Refusal::Policy)?;
        let selected = crate::policy::load_policy_source(source).map_err(|_| Refusal::Policy)?;
        if &selected != snapshot.policy() || snapshot.generation() == 0 {
            return Err(Refusal::Policy);
        }
        let binding = binding(context, prepared)?;
        let evidence = input.program_requirement_context(
            &binding.tenant,
            &binding.actor,
            binding.session,
            binding.generation,
            snapshot.policy(),
            approval.map(|hold| hold.context.capability),
            prepared.observed_head_sequence,
        ).map_err(|_| Refusal::Binding)?;
        let decision = crate::policy::evaluate(snapshot.policy(), input);
        if decision.policy_version != snapshot.version() {
            return Err(Refusal::Policy);
        }
        let requirement = match (decision.outcome, decision.reason) {
            (Outcome::Allow, DecisionReason::PermittedByRule) if approval.is_none() => {
                Requirement::NotRequired
            }
            (Outcome::Deny, DecisionReason::ApprovalRequired) => {
                let hold = approval.ok_or(Refusal::Required)?;
                if hold.context.tenant != context.principal().tenant
                    || hold.context.agent != context.principal().agent
                    || hold.context.session.0 != binding.session
                    || hold.context.policy_version != snapshot.version()
                    || hold.context.request_id == [0; 32]
                    || hold.state != ApprovalState::AwaitingApproval
                    || hold.submission_ref.is_some()
                    || hold.created_at_sequence != binding.prepared_at
                    || hold.expires_at_sequence <= hold.created_at_sequence
                    || hold.prepared.unsigned_canonical_bytes.as_bytes() != prepared.canonical_bytes
                    || hold.prepared.disclosure.canonical_digest != binding.canonical_digest
                    || hold.prepared.preparation_ref.as_str() != preparation_reference(binding.preparation)
                {
                    return Err(Refusal::Binding);
                }
                Requirement::Required {
                    approval_id: hold.context.request_id,
                    expires_at: hold.expires_at_sequence,
                }
            }
            _ => Requirement::Denied,
        };
        let record = Self {
            immutable: Immutable {
                version: 1,
                binding,
                policy_source: std::str::from_utf8(source).map_err(|_| Refusal::Policy)?.to_owned(),
                policy_generation: snapshot.generation(),
                policy_version: decision.policy_version,
                context: evidence,
                matched_rules: decision.matched_rules,
                deciding_rule: decision.deciding_rule,
                requirement,
            },
            decision: None,
        };
        record.encoded()?;
        Ok(record)
    }

    pub(crate) fn commitment(&self) -> Result<[u8; 32], Refusal> {
        let bytes = serde_json::to_vec(&self.immutable).map_err(|_| Refusal::Corrupt)?;
        let mut hash = Sha256::new();
        hash.update(b"layerx/program-approval-requirement/v1\0");
        hash.update(bytes);
        Ok(hash.finalize().into())
    }

    pub(crate) fn encoded(&self) -> Result<Vec<u8>, Refusal> {
        self.validate()?;
        let bytes = serde_json::to_vec(self).map_err(|_| Refusal::Corrupt)?;
        if bytes.len() > MAX_RECORD { return Err(Refusal::Corrupt); }
        Ok(bytes)
    }

    pub(crate) fn companion(&self) -> Result<(TenantKey, Vec<u8>), Refusal> {
        let tenant = TenantId::new(self.immutable.binding.tenant.clone()).map_err(|_| Refusal::Corrupt)?;
        Ok((key(&tenant, self.immutable.binding.preparation)?, self.encoded()?))
    }

    pub(crate) fn required_approval(&self) -> Result<Option<([u8; 32], u64)>, Refusal> {
        match self.immutable.requirement {
            Requirement::NotRequired => Ok(None),
            Requirement::Required { approval_id, expires_at } => Ok(Some((approval_id, expires_at))),
            Requirement::Denied => Err(Refusal::Policy),
        }
    }

    pub(crate) fn stage_terminal(
        &self,
        approval_id: [u8; 32],
        decision_key: &str,
        context: Option<&RpcOwnerContext<'_>>,
        sequence: u64,
        outcome: Terminal,
        submission_ref: Option<[u8; 32]>,
    ) -> Result<Self, Refusal> {
        self.validate()?;
        let Requirement::Required { approval_id: expected, .. } = self.immutable.requirement else {
            return Err(Refusal::Conflict);
        };
        if approval_id != expected { return Err(Refusal::Binding); }
        let (approver, approver_actor, approver_session, approver_generation) = match context {
            Some(context) => {
                let operation = match outcome {
                    Terminal::Granted | Terminal::Defective => crate::tenant::Operation::ApprovalApprove,
                    Terminal::Rejected => crate::tenant::Operation::ApprovalReject,
                    Terminal::Expired => return Err(Refusal::Binding),
                };
                let origin = context.permit().preparation_authorization();
                if context.permit().operation() != operation
                    || context.peer().subject.is_none()
                    || context.peer().uid == 0
                    || context.peer().principal.is_empty()
                    || context.principal().tenant.as_str() != self.immutable.binding.tenant
                    || context.peer().tenant != self.immutable.binding.tenant
                    || origin.session.tenant != context.principal().tenant
                    || origin.session.session_id != context.principal().session_id
                    || origin.generation == 0
                {
                    return Err(Refusal::Binding);
                }
                (
                    Some(context.peer().principal.clone()),
                    Some(context.principal().agent.as_bytes().to_vec()),
                    Some(origin.session.session_id.0),
                    Some(origin.generation),
                )
            }
            None if outcome == Terminal::Expired => (None, None, None, None),
            None => return Err(Refusal::Binding),
        };
        let stamp = DecisionStamp {
            requirement_digest: self.commitment()?,
            approval_id,
            decision_key: decision_key.to_owned(),
            approver,
            approver_actor,
            approver_session,
            approver_generation,
            sequence,
            outcome,
            submission_ref,
        };
        if let Some(existing) = &self.decision {
            if existing != &stamp { return Err(Refusal::Conflict); }
            return Ok(self.clone());
        }
        let mut updated = self.clone();
        updated.decision = Some(stamp);
        updated.encoded()?;
        Ok(updated)
    }

    fn validate(&self) -> Result<(), Refusal> {
        let immutable = &self.immutable;
        let binding = &immutable.binding;
        if immutable.version != 1 || immutable.policy_generation == 0
            || immutable.policy_version.is_empty() || immutable.context.is_empty()
            || binding.principal.is_empty() || binding.generation == 0
            || binding.actor.is_empty() || binding.canonical_len == 0
            || binding.preparation != binding.canonical_digest
        {
            return Err(Refusal::Corrupt);
        }
        TenantId::new(binding.tenant.clone()).map_err(|_| Refusal::Corrupt)?;
        let policy = crate::policy::load_policy_source(immutable.policy_source.as_bytes())
            .map_err(|_| Refusal::Policy)?;
        if policy.version != immutable.policy_version { return Err(Refusal::Policy); }
        let Requirement::Required { approval_id, expires_at } = immutable.requirement else {
            return if self.decision.is_none() { Ok(()) } else { Err(Refusal::Terminal) };
        };
        if approval_id == [0; 32] || expires_at <= binding.prepared_at {
            return Err(Refusal::Corrupt);
        }
        let Some(decision) = &self.decision else { return Ok(()); };
        if decision.requirement_digest != self.commitment()?
            || decision.approval_id != approval_id
            || decision.decision_key.is_empty() || decision.decision_key.len() > 255
            || decision.decision_key.as_bytes().contains(&0)
            || decision.sequence < binding.prepared_at
        {
            return Err(Refusal::Terminal);
        }
        let authenticated = decision.approver.as_deref().is_some_and(|value| !value.is_empty())
            && decision.approver_actor.as_ref().is_some_and(|value| !value.is_empty())
            && decision.approver_session.is_some()
            && decision.approver_generation.is_some_and(|value| value != 0);
        match decision.outcome {
            Terminal::Granted if authenticated && decision.sequence < expires_at
                && decision.submission_ref.is_some_and(|value| value != [0; 32]) => Ok(()),
            Terminal::Rejected | Terminal::Defective if authenticated
                && decision.sequence < expires_at && decision.submission_ref.is_none() => Ok(()),
            Terminal::Expired if decision.sequence >= expires_at
                && decision.approver.is_none() && decision.approver_actor.is_none()
                && decision.approver_session.is_none() && decision.approver_generation.is_none()
                && decision.submission_ref.is_none() => Ok(()),
            _ => Err(Refusal::Terminal),
        }
    }
}

pub(crate) fn read_bound(
    store: &Store,
    prepared: &Prepared,
    durable: &DurablePreparation,
) -> Result<Record, Refusal> {
    let durable_key = DurablePreparation::store_key(&durable.tenant, durable.preparation_id)
        .map_err(|_| Refusal::Binding)?;
    let retained = store.get(&durable_key).ok_or(Refusal::Missing)?;
    if retained.class() != StorageClass::LocalOnly
        || DurablePreparation::decode(durable.tenant.clone(), retained.bytes())
            .map_err(|_| Refusal::Corrupt)? != *durable
        || layerx_wire::activity::encode_unsigned_envelope(&prepared.envelope)
            .map_err(|_| Refusal::Binding)? != prepared.canonical_bytes
    {
        return Err(Refusal::Binding);
    }
    let stored = store.get(&key(&durable.tenant, durable.preparation_id)?).ok_or(Refusal::Missing)?;
    if stored.class() != StorageClass::LocalOnly || stored.bytes().len() > MAX_RECORD {
        return Err(Refusal::Corrupt);
    }
    let record: Record = serde_json::from_slice(stored.bytes()).map_err(|_| Refusal::Corrupt)?;
    if record.encoded()?.as_slice() != stored.bytes() { return Err(Refusal::Corrupt); }
    let binding = &record.immutable.binding;
    if binding.tenant != durable.tenant.as_str()
        || binding.preparation != durable.preparation_id
        || binding.session != durable.session_id || binding.generation != durable.generation
        || binding.payload_hash != durable.payload_hash
        || binding.payload_hash != prepared.envelope.payload_hash()
        || binding.actor != prepared.envelope.actor_did().as_bytes()
        || binding.idempotency != prepared.audit.idempotency_key
        || binding.idempotency != prepared.envelope.idempotency_key().bytes()
        || binding.prepared_at != prepared.observed_head_sequence
        || binding.canonical_digest != digest(&prepared.canonical_bytes)
        || binding.canonical_len != u64::try_from(prepared.canonical_bytes.len()).map_err(|_| Refusal::Binding)?
    {
        return Err(Refusal::Binding);
    }
    let commitment = record.commitment()?;
    if durable.extensions.get(&PROGRAM_REQUIREMENT_EXTENSION).map(Vec::as_slice)
        != Some(commitment.as_slice())
    {
        return Err(Refusal::Missing);
    }
    Ok(record)
}

pub(crate) fn read(
    store: &Store,
    context: &RpcOwnerContext<'_>,
    prepared: &Prepared,
    durable: &DurablePreparation,
) -> Result<Record, Refusal> {
    let record = read_bound(store, prepared, durable)?;
    if durable.terminal() || record.immutable.binding != binding(context, prepared)? {
        return Err(Refusal::Binding);
    }
    Ok(record)
}

fn preparation_reference(preparation: [u8; 32]) -> String {
    preparation.iter().map(|byte| format!("{byte:02x}")).collect()
}

pub(crate) fn authorize_program(
    store: &Store,
    context: &RpcOwnerContext<'_>,
    prepared: &Prepared,
    durable: &DurablePreparation,
    queue: &ApprovalSubmissionQueue,
    current_sequence: u64,
) -> Result<Option<[u8; 32]>, Refusal> {
    let record = read(store, context, prepared, durable)?;
    if current_sequence < record.immutable.binding.prepared_at || current_sequence >= durable.not_after {
        return Err(Refusal::Terminal);
    }
    let reference = match record.immutable.requirement {
        Requirement::NotRequired if record.decision.is_none() => None,
        Requirement::Required { approval_id, expires_at } => {
            if current_sequence >= expires_at { return Err(Refusal::Terminal); }
            let decision = record.decision.as_ref().ok_or(Refusal::Required)?;
            if decision.outcome != Terminal::Granted || decision.sequence > current_sequence {
                return Err(Refusal::Terminal);
            }
            let mut hash = Sha256::new();
            hash.update(b"layerx-approved-preparation-v1");
            hash.update(durable.tenant.as_str().as_bytes());
            hash.update(approval_id);
            hash.update(&prepared.canonical_bytes);
            let expected: [u8; 32] = hash.finalize().into();
            if decision.submission_ref != Some(expected) { return Err(Refusal::Binding); }
            Some(expected)
        }
        _ => return Err(Refusal::Terminal),
    };
    queue.authorize_submit(
        &durable.tenant,
        &preparation_reference(durable.preparation_id),
        &prepared.canonical_bytes,
        reference,
    ).map_err(|_| Refusal::Conflict)?;
    Ok(reference)
}
