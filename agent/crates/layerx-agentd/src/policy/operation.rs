//! The `policy.dry_run` producer over daemon-owned registries and verified context.

use std::collections::BTreeMap;
use std::fs::File;
use std::io::{self, Read as _};
use std::path::PathBuf;

use sha2::{Digest as _, Sha256};

use crate::budget::ReconciliationState;
use crate::capability::CapabilityId;
use crate::protocol_evidence::AuthenticatedCumulativeUse;
use crate::session::{SessionId, SessionRecord};
use crate::store::TenantId;

use super::eval::{CapabilityView, PolicyIntentRequest, Purpose};
use super::{
    dry_run, DryRunResult, EvaluationInput, PolicyRegistry, PolicyRequest,
    PolicySourceError, PolicyValidationError, MAX_POLICY_SOURCE_BYTES,
};

const REQUEST_ID_DOMAIN: &[u8] = b"layerx-agentd/policy-dry-run/v1\0";

const INTENT_REQUEST_ID_DOMAIN: &[u8] = b"layerx-agentd/policy-dry-run/v2\0";

/// One active policy registry per configured tenant.
pub type TenantPolicyRegistries = BTreeMap<TenantId, PolicyRegistry>;

/// Startup failure while loading a configured tenant policy source.
#[derive(Debug)]
pub enum PolicyLoadError {
    Read {
        tenant: TenantId,
        kind: io::ErrorKind,
    },
    Source {
        tenant: TenantId,
        error: PolicySourceError,
    },
    Registry {
        tenant: TenantId,
        error: PolicyValidationError,
    },
}

/// Loads every configured tenant policy source into its own registry.
///
/// # Errors
///
/// Returns the first tenant whose source cannot be read, is refused by the bounded source loader,
/// or is refused as the initial registry policy. No tenant is skipped.
pub fn load_tenant_registries(
    sources: &BTreeMap<TenantId, PathBuf>,
) -> Result<TenantPolicyRegistries, PolicyLoadError> {
    let limit =
        u64::try_from(MAX_POLICY_SOURCE_BYTES).map_or(u64::MAX, |bytes| bytes.saturating_add(1));
    let mut registries = BTreeMap::new();
    for (tenant, path) in sources {
        let read = |error: io::Error| PolicyLoadError::Read {
            tenant: tenant.clone(),
            kind: error.kind(),
        };
        let mut source = Vec::new();
        File::open(path)
            .map_err(read)?
            .take(limit)
            .read_to_end(&mut source)
            .map_err(read)?;
        let registry = PolicyRegistry::from_source(&source).map_err(|error| PolicyLoadError::Source {
            tenant: tenant.clone(),
            error,
        })?;
        registries.insert(tenant.clone(), registry);
    }
    Ok(registries)
}

/// Verified cumulative context available to the owner; each variant selects exactly one existing
/// `EvaluationInput` constructor and nothing is synthesized.
#[derive(Clone, Copy)]
pub enum VerifiedPolicyContext<'a> {
    /// No daemon-produced cumulative evidence: evaluation denies with `InvalidContext`.
    Unavailable,
    /// Opaque protocol-budget reconciliation result.
    ProtocolBudget(&'a ReconciliationState),
    /// Cumulative use derived from a complete authenticated sequence window.
    Authenticated(&'a AuthenticatedCumulativeUse),
}

/// Typed refusal raised before any policy evaluation.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum PolicyDryRunRefusal {
    NoPolicyForTenant,
    UnknownSession,
    UnknownCapability,
    CapabilityStore,
    /// The former `project` policy payload, which names no session or capability and carries an
    /// intent encoding with no defined grammar; it is refused, never reinterpreted.
    LegacyProjectPayload,
}

impl PolicyDryRunRefusal {
    #[must_use]
    pub const fn code(self) -> &'static str {
        match self {
            Self::NoPolicyForTenant => "policy.no_policy",
            Self::UnknownSession => "policy.unknown_session",
            Self::UnknownCapability => "policy.unknown_capability",
            Self::CapabilityStore => "policy.capability_store",
            Self::LegacyProjectPayload => "policy.legacy_project_payload",
        }
    }
}

/// Deterministic audit key for one dry-run evaluation against one policy generation.
#[must_use]
pub fn dry_run_request_id(
    tenant: &TenantId,
    session_id: SessionId,
    capability_id: CapabilityId,
    generation: u64,
    request: &PolicyRequest,
) -> [u8; 32] {
    let mut hasher = Sha256::new();
    hasher.update(REQUEST_ID_DOMAIN);
    update_bytes(&mut hasher, tenant.as_str().as_bytes());
    hasher.update(session_id.0);
    hasher.update(capability_id.0);
    hasher.update(generation.to_be_bytes());
    hasher.update(request.activity_type.to_be_bytes());
    hasher.update(request.counterparty);
    hasher.update(request.asset);
    hasher.update(request.amount.to_be_bytes());
    update_bytes(&mut hasher, request.purpose.as_bytes());
    hasher.update(request.core_sequence.to_be_bytes());
    hasher.finalize().into()
}

/// Deterministic audit key for one multi-effect dry-run evaluation against one policy generation.
#[must_use]
pub fn dry_run_intent_request_id(
    tenant: &TenantId,
    session_id: SessionId,
    capability_id: CapabilityId,
    generation: u64,
    intent: &PolicyIntentRequest,
) -> [u8; 32] {
    let mut hasher = Sha256::new();
    hasher.update(INTENT_REQUEST_ID_DOMAIN);
    update_bytes(&mut hasher, tenant.as_str().as_bytes());
    hasher.update(session_id.0);
    hasher.update(capability_id.0);
    hasher.update(generation.to_be_bytes());
    hasher.update(
        u64::try_from(intent.effects.len())
            .unwrap_or(u64::MAX)
            .to_be_bytes(),
    );
    for effect in &intent.effects {
        hasher.update(effect.activity_type.to_be_bytes());
        hasher.update(effect.counterparty);
        hasher.update(effect.asset);
        hasher.update(effect.amount.to_be_bytes());
    }
    match &intent.purpose {
        Purpose::None => hasher.update([0]),
        Purpose::Text(text) => {
            hasher.update([1]);
            update_bytes(&mut hasher, text.as_str().as_bytes());
        }
    }
    hasher.update(intent.core_sequence.to_be_bytes());
    hasher.finalize().into()
}

fn update_bytes(hasher: &mut Sha256, bytes: &[u8]) {
    hasher.update(u64::try_from(bytes.len()).unwrap_or(u64::MAX).to_be_bytes());
    hasher.update(bytes);
}

/// Evaluates the registry's active policy, captured once before evaluation, against the real
/// request, session and capability and records the decision in the registry audit.
///
/// The result is a local restriction: it carries no freshness and is never core-produced.
pub fn dry_run_with_context(
    registry: &mut PolicyRegistry,
    request_id: [u8; 32],
    request: &PolicyRequest,
    session: &SessionRecord,
    capability: impl Into<CapabilityView>,
    context: VerifiedPolicyContext<'_>,
) -> DryRunResult {
    dry_run_intent_with_context(
        registry,
        request_id,
        &PolicyIntentRequest::from(request),
        session,
        capability.into(),
        context,
    )
}

/// Evaluates every effect of one intent against the registry's active policy, captured once
/// before evaluation, and records the decision in the registry audit; nothing is prepared,
/// reserved, signed or submitted.
pub fn dry_run_intent_with_context(
    registry: &mut PolicyRegistry,
    request_id: [u8; 32],
    intent: &PolicyIntentRequest,
    session: &SessionRecord,
    capability: CapabilityView,
    context: VerifiedPolicyContext<'_>,
) -> DryRunResult {
    let snapshot = registry.begin_request();
    let input = EvaluationInput::for_intent(intent.clone(), session, capability, context);
    dry_run(registry, request_id, snapshot.policy(), &input)
}
