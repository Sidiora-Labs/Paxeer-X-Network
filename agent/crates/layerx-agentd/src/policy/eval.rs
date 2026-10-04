//! Pure, bounded policy evaluation.

use std::collections::{BTreeMap, BTreeSet};
use std::panic::{catch_unwind, AssertUnwindSafe};
use std::sync::Arc;

use layerx_types::ids::Did;

use crate::budget::ReconciliationState;
use crate::capability::timed::TimedCapability;
use crate::capability::{Capability, CapabilityId, Dimension, RateCeiling};
use crate::protocol_evidence::{
    AuthenticatedCoreTime, AuthenticatedCumulativeUse, AuthenticatedTimeWindowUse,
};
use crate::session::{SessionId, SessionRecord};
use crate::store::TenantId;

use super::{Decision, DecisionReason, Outcome, VerifiedPolicyContext};

/// Current activity intent fields consumed by policy evaluation.
///
/// Historical spend, usage counts, and approval status are deliberately absent:
/// those facts must come from daemon-owned verified context.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct PolicyRequest {
    pub activity_type: u16,
    pub counterparty: [u8; 32],
    pub asset: [u8; 32],
    pub amount: u128,
    pub purpose: String,
    pub core_sequence: u64,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct Effect {
    pub activity_type: u16,
    pub counterparty: [u8; 32],
    pub asset: [u8; 32],
    pub amount: u128,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct PurposeText(String);

impl PurposeText {
    #[must_use]
    pub fn as_str(&self) -> &str {
        &self.0
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct EmptyPurpose;

impl TryFrom<String> for PurposeText {
    type Error = EmptyPurpose;

    fn try_from(value: String) -> Result<Self, Self::Error> {
        if value.is_empty() {
            Err(EmptyPurpose)
        } else {
            Ok(Self(value))
        }
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub enum Purpose {
    None,
    Text(PurposeText),
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct PolicyIntentRequest {
    pub effects: Vec<Effect>,
    pub purpose: Purpose,
    pub core_sequence: u64,
}

impl From<&PolicyRequest> for PolicyIntentRequest {
    fn from(request: &PolicyRequest) -> Self {
        Self {
            effects: vec![Effect {
                activity_type: request.activity_type,
                counterparty: request.counterparty,
                asset: request.asset,
                amount: request.amount,
            }],
            purpose: PurposeText::try_from(request.purpose.clone())
                .map_or(Purpose::None, Purpose::Text),
            core_sequence: request.core_sequence,
        }
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub enum AmountBound {
    Uniform(u128),
    PerAsset(BTreeMap<[u8; 32], u128>),
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub enum RateBound {
    Sequences(RateCeiling),
    Seconds(BTreeMap<u64, u64>),
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum ExpiryBound {
    Sequence(u64),
    CoreTimeMs(u128),
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum CapabilityViewRefusal {
    Revoked,
    Malformed(Dimension),
    Unhonoured(Dimension),
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct CapabilityView {
    id: CapabilityId,
    tenant: TenantId,
    activity_types: BTreeSet<u16>,
    counterparties: BTreeSet<[u8; 32]>,
    assets: BTreeSet<[u8; 32]>,
    amount: AmountBound,
    rate: RateBound,
    purposes: BTreeSet<String>,
    expiry: ExpiryBound,
}

impl CapabilityView {
    #[must_use]
    pub const fn id(&self) -> CapabilityId {
        self.id
    }

    #[must_use]
    pub const fn tenant(&self) -> &TenantId {
        &self.tenant
    }

    #[must_use]
    pub const fn amount(&self) -> &AmountBound {
        &self.amount
    }

    #[must_use]
    pub const fn rate(&self) -> &RateBound {
        &self.rate
    }

    #[must_use]
    pub const fn expiry(&self) -> ExpiryBound {
        self.expiry
    }

    #[must_use]
    pub const fn purposes(&self) -> &BTreeSet<String> {
        &self.purposes
    }

    /// # Errors
    ///
    /// Returns `Unhonoured` naming the first dimension the sequence-coordinate evaluator cannot apply.
    pub fn evaluable(&self) -> Result<(), CapabilityViewRefusal> {
        self.sequence_bounds()
            .map(|_| ())
            .map_err(CapabilityViewRefusal::Unhonoured)
    }

    fn sequence_bounds(&self) -> Result<(u64, RateCeiling), Dimension> {
        let ExpiryBound::Sequence(expiry) = self.expiry else {
            return Err(Dimension::Expiry);
        };
        let RateBound::Sequences(rate) = self.rate else {
            return Err(Dimension::Rate);
        };
        Ok((expiry, rate))
    }
}

impl From<&Capability> for CapabilityView {
    fn from(capability: &Capability) -> Self {
        let dimensions = &capability.dimensions;
        Self {
            id: capability.id,
            tenant: capability.tenant.clone(),
            activity_types: dimensions.activity_types.clone(),
            counterparties: dimensions.counterparties.clone(),
            assets: dimensions.assets.clone(),
            amount: AmountBound::Uniform(dimensions.amount_ceiling),
            rate: RateBound::Sequences(dimensions.rate_ceiling),
            purposes: dimensions.purposes.clone(),
            expiry: ExpiryBound::Sequence(dimensions.expiry_sequence),
        }
    }
}

impl TryFrom<&TimedCapability> for CapabilityView {
    type Error = CapabilityViewRefusal;

    fn try_from(record: &TimedCapability) -> Result<Self, Self::Error> {
        if record.revoked.is_some() {
            return Err(CapabilityViewRefusal::Revoked);
        }
        if record.purposes.iter().any(String::is_empty) {
            return Err(CapabilityViewRefusal::Malformed(Dimension::Purpose));
        }
        if record.rate_ceilings.contains_key(&0) {
            return Err(CapabilityViewRefusal::Malformed(Dimension::Rate));
        }
        if record
            .amount_ceilings
            .keys()
            .any(|asset| !record.assets.contains(asset))
        {
            return Err(CapabilityViewRefusal::Malformed(Dimension::Amount));
        }
        Ok(Self {
            id: CapabilityId(record.id),
            tenant: record.tenant.clone(),
            activity_types: record.activity_types.clone(),
            counterparties: record.counterparties.clone(),
            assets: record.assets.clone(),
            amount: AmountBound::PerAsset(record.amount_ceilings.clone()),
            rate: RateBound::Seconds(record.rate_ceilings.clone()),
            purposes: record.purposes.clone(),
            expiry: ExpiryBound::CoreTimeMs(record.not_after_ms()),
        })
    }
}

/// Inclusive deterministic protocol-sequence window.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct SequenceWindow {
    pub first: u64,
    pub last: u64,
}

/// Every constraint supported by the policy vocabulary.
#[derive(Clone, Debug, Default, Eq, PartialEq)]
pub struct RuleConstraints {
    pub activity_types: BTreeSet<u16>,
    pub counterparties: BTreeSet<[u8; 32]>,
    pub assets: BTreeSet<[u8; 32]>,
    pub maximum_amount: Option<u128>,
    pub maximum_cumulative_amount: Option<u128>,
    pub maximum_cumulative_count: Option<u64>,
    pub purposes: BTreeSet<String>,
    pub capability_ids: BTreeSet<CapabilityId>,
    pub session_ids: BTreeSet<SessionId>,
    pub agents: BTreeSet<Did>,
    pub tenants: BTreeSet<TenantId>,
    pub sequence_window: Option<SequenceWindow>,
    pub required_approval: bool,
}

/// A matching rule either permits locally or refuses.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum RuleEffect {
    Permit,
    Deny,
}

/// One named deterministic policy rule.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Rule {
    pub id: String,
    pub effect: RuleEffect,
    pub constraints: RuleConstraints,
}

/// Loaded immutable policy version.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct PolicySet {
    pub version: String,
    pub rules: Vec<Rule>,
    pub evaluation_step_limit: u64,
}

/// Inputs admitted to deterministic local evaluation.
///
/// Cumulative contexts are private. Callers may supply only opaque facts issued
/// by protocol reconciliation or complete authenticated activity/receipt
/// windows; arbitrary caller-provided totals are not accepted.
pub struct EvaluationInput<'a> {
    intent: Arc<PolicyIntentRequest>,
    session: &'a SessionRecord,
    capability: Arc<CapabilityView>,
    context: VerifiedPolicyContext<'a>,
    aggregate: Option<u128>,
    focus: Option<usize>,
}

impl<'a> EvaluationInput<'a> {
    #[must_use]
    pub fn intent(&self) -> &PolicyIntentRequest {
        &self.intent
    }

    #[must_use]
    pub const fn session(&self) -> &SessionRecord {
        self.session
    }

    #[must_use]
    pub fn capability(&self) -> &CapabilityView {
        &self.capability
    }

    /// Creates an input with no canonical protocol-budget authority.
    ///
    /// Evaluation of this input always denies with `InvalidContext`.
    #[must_use]
    pub fn without_protocol_budget(
        request: &'a PolicyRequest,
        session: &'a SessionRecord,
        capability: &'a Capability,
    ) -> Self {
        Self::for_intent(
            request.into(),
            session,
            capability.into(),
            VerifiedPolicyContext::Unavailable,
        )
    }

    /// Binds an opaque result issued only by protocol-budget reconciliation.
    /// Cumulative count and approval evidence remain unavailable, so this input
    /// cannot currently yield an allow decision.
    #[must_use]
    pub fn with_verified_protocol_budget(
        request: &'a PolicyRequest,
        session: &'a SessionRecord,
        capability: &'a Capability,
        budget: &'a ReconciliationState,
    ) -> Self {
        Self::for_intent(
            request.into(),
            session,
            capability.into(),
            VerifiedPolicyContext::ProtocolBudget(budget),
        )
    }

    /// Binds cumulative amount and count issued from a complete authenticated
    /// protocol-sequence window.
    #[must_use]
    pub fn with_authenticated_cumulative_use(
        request: &'a PolicyRequest,
        session: &'a SessionRecord,
        capability: &'a Capability,
        cumulative: &'a AuthenticatedCumulativeUse,
    ) -> Self {
        Self::for_intent(
            request.into(),
            session,
            capability.into(),
            VerifiedPolicyContext::Authenticated(cumulative),
        )
    }

    #[must_use]
    pub fn for_intent(
        intent: PolicyIntentRequest,
        session: &'a SessionRecord,
        capability: CapabilityView,
        context: VerifiedPolicyContext<'a>,
    ) -> Self {
        let aggregate = intent
            .effects
            .iter()
            .try_fold(0_u128, |total, effect| total.checked_add(effect.amount));
        Self {
            intent: Arc::new(intent),
            session,
            capability: Arc::new(capability),
            context,
            aggregate,
            focus: None,
        }
    }

    fn focused(&self, index: usize) -> Self {
        Self {
            intent: Arc::clone(&self.intent),
            session: self.session,
            capability: Arc::clone(&self.capability),
            context: self.context,
            aggregate: self.aggregate,
            focus: Some(index),
        }
    }

    fn effects(&self) -> &[Effect] {
        match self.focus {
            Some(index) => self
                .intent
                .effects
                .get(index)
                .map_or(&[][..], std::slice::from_ref),
            None => &self.intent.effects,
        }
    }

    const fn authenticated_cumulative_amount(&self) -> Option<u128> {
        match self.context {
            VerifiedPolicyContext::Authenticated(cumulative) => Some(cumulative.amount()),
            VerifiedPolicyContext::ProtocolBudget(budget) => Some(budget.protocol_consumed()),
            VerifiedPolicyContext::Unavailable => None,
        }
    }

    const fn authenticated_cumulative_count(&self) -> Option<u64> {
        match self.context {
            VerifiedPolicyContext::Authenticated(cumulative) => Some(cumulative.count()),
            VerifiedPolicyContext::ProtocolBudget(_) | VerifiedPolicyContext::Unavailable => None,
        }
    }

    const fn authenticated_window(&self) -> Option<&AuthenticatedCumulativeUse> {
        match self.context {
            VerifiedPolicyContext::Authenticated(cumulative) => Some(cumulative),
            VerifiedPolicyContext::ProtocolBudget(_) | VerifiedPolicyContext::Unavailable => None,
        }
    }
}

/// Typed internal failure. Every variant maps to a denial.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum EvaluationFailure {
    InvalidRule,
    ProtocolBudgetUnavailable,
    CumulativeCountUnavailable,
    StepLimitExceeded,
    Internal,
    UnhonouredDimension(Dimension),
}

/// Rule-matching boundary used by the fail-closed evaluator.
pub trait RuleMatcher {
    /// Reports whether one rule's constraints all admit the evaluation input.
    ///
    /// # Errors
    ///
    /// Returns typed missing-authority failures for cumulative facts which have
    /// no daemon-owned producer, or `InvalidRule` for malformed rules. Every
    /// variant is caught by the evaluator and turned into a fail-closed deny.
    fn matches(&self, rule: &Rule, input: &EvaluationInput<'_>) -> Result<bool, EvaluationFailure>;
}

struct DeterministicMatcher;

impl RuleMatcher for DeterministicMatcher {
    fn matches(&self, rule: &Rule, input: &EvaluationInput<'_>) -> Result<bool, EvaluationFailure> {
        let constraints = &rule.constraints;
        let intent = &input.intent;
        let session = &input.session.request;
        if rule.id.is_empty() {
            return Err(EvaluationFailure::InvalidRule);
        }
        let cumulative_amount = input
            .authenticated_cumulative_amount()
            .ok_or(EvaluationFailure::ProtocolBudgetUnavailable)?;
        if constraints.maximum_cumulative_count.is_some()
            && input.authenticated_cumulative_count().is_none()
        {
            return Err(EvaluationFailure::CumulativeCountUnavailable);
        }
        let effects = input.effects();
        Ok(!effects.is_empty()
            && effects
                .iter()
                .all(|effect| effect_admitted(constraints, effect))
            && constraints.maximum_cumulative_amount.is_none_or(|maximum| {
                input
                    .aggregate
                    .and_then(|total| cumulative_amount.checked_add(total))
                    .is_some_and(|projected| projected <= maximum)
            })
            && constraints.maximum_cumulative_count.is_none_or(|maximum| {
                input
                    .authenticated_cumulative_count()
                    .and_then(|count| count.checked_add(1))
                    .is_some_and(|projected| projected <= maximum)
            })
            && (constraints.purposes.is_empty()
                || purpose_listed(&constraints.purposes, &intent.purpose))
            && (constraints.capability_ids.is_empty()
                || constraints.capability_ids.contains(&input.capability.id))
            && (constraints.session_ids.is_empty()
                || constraints.session_ids.contains(&session.session_id))
            && (constraints.agents.is_empty() || constraints.agents.contains(&session.agent))
            && (constraints.tenants.is_empty() || constraints.tenants.contains(&session.tenant))
            && constraints.sequence_window.is_none_or(|window| {
                intent.core_sequence >= window.first && intent.core_sequence <= window.last
            }))
    }
}

fn effect_admitted(constraints: &RuleConstraints, effect: &Effect) -> bool {
    (constraints.activity_types.is_empty()
        || constraints.activity_types.contains(&effect.activity_type))
        && (constraints.counterparties.is_empty()
            || constraints.counterparties.contains(&effect.counterparty))
        && (constraints.assets.is_empty() || constraints.assets.contains(&effect.asset))
        && constraints
            .maximum_amount
            .is_none_or(|maximum| effect.amount <= maximum)
}

fn purpose_listed(purposes: &BTreeSet<String>, purpose: &Purpose) -> bool {
    match purpose {
        Purpose::Text(text) => purposes.contains(text.as_str()),
        Purpose::None => false,
    }
}

pub(crate) fn evaluate_policy(policy: &PolicySet, input: &EvaluationInput<'_>) -> Decision {
    evaluate_with(policy, input, &DeterministicMatcher)
}

pub(crate) fn evaluate_admission(policy: &PolicySet, input: &EvaluationInput<'_>) -> Decision {
    if input.intent.core_sequence >= input.session.request.expiry_sequence
        || input.intent.effects.iter().any(|effect| {
            !input
                .session
                .request
                .permitted_activity_types
                .contains(&effect.activity_type)
        })
    {
        return Decision::deny(&policy.version, DecisionReason::InvalidContext);
    }
    let evaluated = catch_unwind(AssertUnwindSafe(
        || -> Result<Decision, EvaluationFailure> {
            if !valid_context(policy, input)? {
                return Ok(Decision::deny(
                    &policy.version,
                    DecisionReason::InvalidContext,
                ));
            }
            let mut covered = vec![false; input.intent.effects.len()];
            let decision = decide(policy, input.intent.effects.len(), &mut |rule, index| {
                let hit = DeterministicMatcher.matches(rule, &input.focused(index))?;
                if hit && rule.effect == RuleEffect::Permit {
                    covered[index] = true;
                }
                Ok(hit)
            })?;
            Ok(admission_coverage(policy, decision, &covered))
        },
    ));
    match evaluated {
        Ok(Ok(decision)) => decision,
        Ok(Err(_)) | Err(_) => Decision::deny(&policy.version, DecisionReason::EvaluationFailure),
    }
}

fn admission_coverage(policy: &PolicySet, decision: Decision, covered: &[bool]) -> Decision {
    if decision.reason == DecisionReason::ApprovalRequired && covered.iter().any(|covered| !covered)
    {
        Decision::deny(&policy.version, DecisionReason::NoPermittingRule)
    } else {
        decision
    }
}

#[allow(clippy::too_many_arguments)]
pub(crate) fn evaluate_timed_admission(
    policy: &PolicySet,
    intent: &PolicyIntentRequest,
    session: &SessionRecord,
    capability: &TimedCapability,
    observed: &AuthenticatedCoreTime,
    usage: &[AuthenticatedTimeWindowUse],
    budget: Option<&ReconciliationState>,
) -> Decision {
    let evaluated = catch_unwind(AssertUnwindSafe(
        || -> Result<Decision, EvaluationFailure> {
            let view =
                CapabilityView::try_from(capability).map_err(|_| EvaluationFailure::InvalidRule)?;
            let request = &session.request;
            let now = observed.observed_core_ms();
            if !session.open
                || policy.version.is_empty()
                || request.policy_version != policy.version
                || request.tenant != capability.tenant
                || request.agent.as_bytes() != capability.agent.as_bytes()
                || request.authority != capability.authority
                || intent.effects.is_empty()
                || intent.core_sequence != observed.through_sequence()
                || intent.core_sequence >= request.expiry_sequence
                || !session
                    .public_expiry_within(now)
                    .map_err(|_| EvaluationFailure::Internal)?
                || now < capability.created_at_ms
                || observed.through_sequence() < capability.created_at_sequence
                || capability.is_expired(now)
                || usage.len() > 4096
                || usage.len() != capability.rate_ceilings.len()
                || budget.is_some_and(|value| {
                    value.observed_head_sequence() != observed.through_sequence()
                })
            {
                return Ok(Decision::deny(
                    &policy.version,
                    DecisionReason::InvalidContext,
                ));
            }
            let mut windows = BTreeSet::new();
            for evidence in usage {
                let Some(maximum) = capability.rate_ceilings.get(&evidence.window_seconds()) else {
                    return Ok(Decision::deny(
                        &policy.version,
                        DecisionReason::InvalidContext,
                    ));
                };
                if !windows.insert(evidence.window_seconds())
                    || evidence.window_seconds() == 0
                    || evidence.actor() != &request.agent
                    || evidence.observed_batch_id() != observed.observed_batch_id()
                    || evidence.observed_core_ms() != now
                    || evidence.through_sequence() != observed.through_sequence()
                    || evidence.count() >= *maximum
                {
                    return Ok(Decision::deny(
                        &policy.version,
                        DecisionReason::InvalidContext,
                    ));
                }
            }
            let mut per_asset = BTreeMap::<[u8; 32], u128>::new();
            let mut aggregate = 0_u128;
            for effect in &intent.effects {
                if !request
                    .permitted_activity_types
                    .contains(&effect.activity_type)
                    || !view.activity_types.contains(&effect.activity_type)
                    || !view.counterparties.contains(&effect.counterparty)
                    || !view.assets.contains(&effect.asset)
                {
                    return Ok(Decision::deny(
                        &policy.version,
                        DecisionReason::InvalidContext,
                    ));
                }
                let total = per_asset.entry(effect.asset).or_default();
                *total = total
                    .checked_add(effect.amount)
                    .ok_or(EvaluationFailure::Internal)?;
                aggregate = aggregate
                    .checked_add(effect.amount)
                    .ok_or(EvaluationFailure::Internal)?;
            }
            if !purpose_listed(&view.purposes, &intent.purpose)
                || per_asset.iter().any(|(asset, total)| {
                    capability
                        .amount_ceilings
                        .get(asset)
                        .is_none_or(|ceiling| total > ceiling)
                })
            {
                return Ok(Decision::deny(
                    &policy.version,
                    DecisionReason::InvalidContext,
                ));
            }
            let mut covered = vec![false; intent.effects.len()];
            let decision = decide(policy, intent.effects.len(), &mut |rule, index| {
                if rule.id.is_empty() {
                    return Err(EvaluationFailure::InvalidRule);
                }
                let constraints = &rule.constraints;
                if constraints.maximum_cumulative_count.is_some() {
                    return Err(EvaluationFailure::CumulativeCountUnavailable);
                }
                let cumulative_admitted = match constraints.maximum_cumulative_amount {
                    Some(maximum) => budget
                        .ok_or(EvaluationFailure::ProtocolBudgetUnavailable)?
                        .protocol_consumed()
                        .checked_add(aggregate)
                        .is_some_and(|value| value <= maximum),
                    None => true,
                };
                let effect = intent
                    .effects
                    .get(index)
                    .ok_or(EvaluationFailure::Internal)?;
                let hit = effect_admitted(constraints, effect)
                    && cumulative_admitted
                    && (constraints.purposes.is_empty()
                        || purpose_listed(&constraints.purposes, &intent.purpose))
                    && (constraints.capability_ids.is_empty()
                        || constraints.capability_ids.contains(&view.id))
                    && (constraints.session_ids.is_empty()
                        || constraints.session_ids.contains(&request.session_id))
                    && (constraints.agents.is_empty()
                        || constraints.agents.contains(&request.agent))
                    && (constraints.tenants.is_empty()
                        || constraints.tenants.contains(&request.tenant))
                    && constraints.sequence_window.is_none_or(|window| {
                        intent.core_sequence >= window.first && intent.core_sequence <= window.last
                    });
                if hit && rule.effect == RuleEffect::Permit {
                    covered[index] = true;
                }
                Ok(hit)
            })?;
            Ok(admission_coverage(policy, decision, &covered))
        },
    ));
    match evaluated {
        Ok(Ok(decision)) => decision,
        Ok(Err(_)) | Err(_) => Decision::deny(&policy.version, DecisionReason::EvaluationFailure),
    }
}

pub(crate) fn evaluate_with(
    policy: &PolicySet,
    input: &EvaluationInput<'_>,
    matcher: &dyn RuleMatcher,
) -> Decision {
    match catch_unwind(AssertUnwindSafe(|| evaluate_inner(policy, input, matcher))) {
        Ok(Ok(decision)) => decision,
        Ok(Err(_)) | Err(_) => Decision::deny(&policy.version, DecisionReason::EvaluationFailure),
    }
}

fn evaluate_inner(
    policy: &PolicySet,
    input: &EvaluationInput<'_>,
    matcher: &dyn RuleMatcher,
) -> Result<Decision, EvaluationFailure> {
    if !valid_context(policy, input)? {
        return Ok(Decision::deny(
            &policy.version,
            DecisionReason::InvalidContext,
        ));
    }
    decide(policy, input.intent.effects.len(), &mut |rule, index| {
        matcher.matches(rule, &input.focused(index))
    })
}

fn decide(
    policy: &PolicySet,
    effect_count: usize,
    matches: &mut dyn FnMut(&Rule, usize) -> Result<bool, EvaluationFailure>,
) -> Result<Decision, EvaluationFailure> {
    if effect_count == 0 {
        return Ok(Decision::deny(
            &policy.version,
            DecisionReason::InvalidContext,
        ));
    }
    let width = u64::try_from(effect_count).map_err(|_| EvaluationFailure::StepLimitExceeded)?;
    let mut ordered: Vec<&Rule> = policy.rules.iter().collect();
    ordered.sort_by(|left, right| left.id.cmp(&right.id));
    let mut matched_rules = Vec::new();
    let mut permitted = Vec::new();
    let mut denied = Vec::new();
    let mut approval_missing = Vec::new();
    let mut covered = vec![false; effect_count];
    for (index, rule) in ordered.into_iter().enumerate() {
        let steps = u64::try_from(index + 1)
            .ok()
            .and_then(|rules| rules.checked_mul(width))
            .ok_or(EvaluationFailure::StepLimitExceeded)?;
        if steps > policy.evaluation_step_limit {
            return Err(EvaluationFailure::StepLimitExceeded);
        }
        let mut hit = false;
        for (effect, slot) in covered.iter_mut().enumerate() {
            if matches(rule, effect)? {
                hit = true;
                if rule.effect == RuleEffect::Permit && !rule.constraints.required_approval {
                    *slot = true;
                }
            }
        }
        if !hit {
            continue;
        }
        matched_rules.push(rule.id.clone());
        match rule.effect {
            RuleEffect::Deny => denied.push(rule.id.clone()),
            RuleEffect::Permit if rule.constraints.required_approval => {
                approval_missing.push(rule.id.clone());
            }
            RuleEffect::Permit => permitted.push(rule.id.clone()),
        }
    }

    let (outcome, deciding_rule, reason) = if let Some(rule) = denied.first() {
        (
            Outcome::Deny,
            Some(rule.clone()),
            DecisionReason::ExplicitDeny,
        )
    } else if let Some(rule) = approval_missing.first() {
        (
            Outcome::Deny,
            Some(rule.clone()),
            DecisionReason::ApprovalRequired,
        )
    } else if let Some(rule) = permitted
        .first()
        .filter(|_| covered.iter().all(|slot| *slot))
    {
        (
            Outcome::Allow,
            Some(rule.clone()),
            DecisionReason::PermittedByRule,
        )
    } else {
        (Outcome::Deny, None, DecisionReason::NoPermittingRule)
    };
    Ok(Decision {
        outcome,
        policy_version: policy.version.clone(),
        matched_rules,
        deciding_rule,
        reason,
    })
}

fn valid_context(
    policy: &PolicySet,
    input: &EvaluationInput<'_>,
) -> Result<bool, EvaluationFailure> {
    let Some(cumulative) = input.authenticated_window() else {
        return Ok(false);
    };
    let Some(cumulative_count) = input.authenticated_cumulative_count() else {
        return Ok(false);
    };
    let session = &input.session.request;
    if input.intent.effects.is_empty()
        || policy.version.is_empty()
        || policy.version != session.policy_version
        || !input.session.open
        || session.tenant != input.capability.tenant
        || cumulative.actor() != &session.agent
    {
        return Ok(false);
    }
    let (expiry, rate) = input
        .capability
        .sequence_bounds()
        .map_err(EvaluationFailure::UnhonouredDimension)?;
    let Some(expected_last) = input.intent.core_sequence.checked_sub(1) else {
        return Ok(false);
    };
    let Some(expected_first) = input
        .intent
        .core_sequence
        .checked_sub(rate.window_sequences)
    else {
        return Ok(false);
    };
    if cumulative.window()
        != (crate::protocol_evidence::CumulativeUseWindow {
            first: expected_first,
            last: expected_last,
        })
    {
        return Ok(false);
    }
    Ok(capability_admits(
        &input.capability,
        &input.intent,
        expiry,
        rate,
        cumulative_count,
    ))
}

fn capability_admits(
    capability: &CapabilityView,
    intent: &PolicyIntentRequest,
    expiry: u64,
    rate: RateCeiling,
    uses_in_window: u64,
) -> bool {
    if intent.core_sequence >= expiry {
        return false;
    }
    let mut per_asset: BTreeMap<[u8; 32], u128> = BTreeMap::new();
    for effect in &intent.effects {
        if !capability.activity_types.contains(&effect.activity_type)
            || !capability.counterparties.contains(&effect.counterparty)
            || !capability.assets.contains(&effect.asset)
        {
            return false;
        }
        let Some(total) = per_asset
            .get(&effect.asset)
            .copied()
            .unwrap_or(0)
            .checked_add(effect.amount)
        else {
            return false;
        };
        per_asset.insert(effect.asset, total);
    }
    per_asset
        .iter()
        .all(|(asset, total)| match &capability.amount {
            AmountBound::Uniform(ceiling) => total <= ceiling,
            AmountBound::PerAsset(ceilings) => {
                ceilings.get(asset).is_some_and(|ceiling| total <= ceiling)
            }
        })
        && uses_in_window < rate.maximum_uses
        && purpose_listed(&capability.purposes, &intent.purpose)
}

#[cfg(test)]
mod tests {
    use std::collections::{BTreeMap, BTreeSet};
    use std::fmt::Debug;

    use crate::capability::timed::TimedCapability;
    use crate::capability::{
        Capability, CapabilityDimensions, CapabilityId, Dimension, RateCeiling,
    };
    use crate::identity::ProtocolAuthority;
    use crate::store::TenantId;

    use super::{
        capability_admits, decide, effect_admitted, purpose_listed, AmountBound, CapabilityView,
        CapabilityViewRefusal, DecisionReason, Effect, EmptyPurpose, ExpiryBound, Outcome,
        PolicyIntentRequest, PolicyRequest, PolicySet, Purpose, PurposeText, RateBound, Rule,
        RuleConstraints, RuleEffect,
    };

    fn must<T, E: Debug>(value: Result<T, E>) -> T {
        match value {
            Ok(value) => value,
            Err(error) => panic!("{error:?}"),
        }
    }

    fn tenant() -> TenantId {
        must(TenantId::new("tenant-a"))
    }

    fn effect(asset: u8, amount: u128) -> Effect {
        Effect {
            activity_type: 7,
            counterparty: [8; 32],
            asset: [asset; 32],
            amount,
        }
    }

    fn rule(id: &str, effect: RuleEffect, constraints: RuleConstraints) -> Rule {
        Rule {
            id: id.to_owned(),
            effect,
            constraints,
        }
    }

    fn policy(rules: Vec<Rule>) -> PolicySet {
        PolicySet {
            version: "policy-v1".to_owned(),
            rules,
            evaluation_step_limit: 100,
        }
    }

    fn decide_over(policy: &PolicySet, effects: &[Effect]) -> super::Decision {
        must(decide(policy, effects.len(), &mut |rule, index| {
            Ok(effects
                .get(index)
                .is_some_and(|effect| effect_admitted(&rule.constraints, effect)))
        }))
    }

    fn legacy_capability(activity_types: BTreeSet<u16>) -> Capability {
        Capability {
            id: CapabilityId([4; 32]),
            tenant: tenant(),
            dimensions: CapabilityDimensions {
                activity_types,
                counterparties: BTreeSet::from([[8; 32]]),
                assets: BTreeSet::from([[9; 32], [10; 32]]),
                amount_ceiling: 500,
                rate_ceiling: RateCeiling {
                    maximum_uses: 10,
                    window_sequences: 100,
                },
                purposes: BTreeSet::from(["research".to_owned()]),
                expiry_sequence: 200,
            },
        }
    }

    fn intent(effects: Vec<Effect>, purpose: Purpose) -> PolicyIntentRequest {
        PolicyIntentRequest {
            effects,
            purpose,
            core_sequence: 120,
        }
    }

    fn research() -> Purpose {
        Purpose::Text(must(PurposeText::try_from("research".to_owned())))
    }

    #[test]
    fn deny_on_any_effect_takes_precedence_over_permits() {
        let effects = [effect(9, 10), effect(10, 20)];
        let permit_all = rule("a-permit", RuleEffect::Permit, RuleConstraints::default());
        let deny_second = rule(
            "b-deny",
            RuleEffect::Deny,
            RuleConstraints {
                assets: BTreeSet::from([[10; 32]]),
                ..RuleConstraints::default()
            },
        );
        let decision = decide_over(&policy(vec![deny_second, permit_all.clone()]), &effects);
        assert_eq!(decision.outcome, Outcome::Deny);
        assert_eq!(decision.reason, DecisionReason::ExplicitDeny);
        assert_eq!(decision.deciding_rule.as_deref(), Some("b-deny"));
        assert_eq!(decision.matched_rules, vec!["a-permit", "b-deny"]);

        let first_only = rule(
            "a-permit",
            RuleEffect::Permit,
            RuleConstraints {
                assets: BTreeSet::from([[9; 32]]),
                ..RuleConstraints::default()
            },
        );
        let partial = decide_over(&policy(vec![first_only]), &effects);
        assert_eq!(partial.outcome, Outcome::Deny);
        assert_eq!(partial.reason, DecisionReason::NoPermittingRule);

        let allowed = decide_over(&policy(vec![permit_all]), &effects);
        assert_eq!(allowed.outcome, Outcome::Allow);
        assert_eq!(allowed.deciding_rule.as_deref(), Some("a-permit"));
    }

    #[test]
    fn empty_effect_set_and_empty_permitted_set_deny() {
        let permit_all = rule("a-permit", RuleEffect::Permit, RuleConstraints::default());
        let empty = decide_over(&policy(vec![permit_all]), &[]);
        assert_eq!(empty.outcome, Outcome::Deny);
        assert_eq!(empty.reason, DecisionReason::InvalidContext);

        let no_rules = decide_over(&policy(Vec::new()), &[effect(9, 10)]);
        assert_eq!(no_rules.outcome, Outcome::Deny);
        assert_eq!(no_rules.reason, DecisionReason::NoPermittingRule);

        let rate = RateCeiling {
            maximum_uses: 10,
            window_sequences: 100,
        };
        let open = CapabilityView::from(&legacy_capability(BTreeSet::from([7])));
        let closed = CapabilityView::from(&legacy_capability(BTreeSet::new()));
        let request = intent(vec![effect(9, 300), effect(10, 300)], research());
        assert!(capability_admits(&open, &request, 200, rate, 0));
        assert!(!capability_admits(&closed, &request, 200, rate, 0));
        let over = intent(vec![effect(9, 300), effect(9, 300)], research());
        assert!(!capability_admits(&open, &over, 200, rate, 0));
    }

    #[test]
    fn timed_capability_converts_losslessly() {
        let mut record = TimedCapability {
            id: [4; 32],
            parent: None,
            tenant: tenant(),
            agent: "did:layerx:policy-agent".to_owned(),
            authority: ProtocolAuthority::SessionKey([1; 32]),
            activity_types: BTreeSet::from([7]),
            counterparties: BTreeSet::from([[8; 32]]),
            assets: BTreeSet::from([[9; 32], [10; 32]]),
            amount_ceilings: BTreeMap::from([([9; 32], 500)]),
            rate_ceilings: BTreeMap::from([(60, 3), (3_600, 20)]),
            purposes: BTreeSet::from(["research".to_owned()]),
            expiry_seconds: 2_000,
            grant_not_after_ms: 1_500_000,
            created_at_ms: 1_000,
            created_at_sequence: 5,
            revoked: None,
        };
        let view = must(CapabilityView::try_from(&record));
        assert_eq!(view.id(), CapabilityId([4; 32]));
        assert_eq!(view.tenant(), &tenant());
        assert_eq!(
            view.amount(),
            &AmountBound::PerAsset(BTreeMap::from([([9; 32], 500)]))
        );
        assert_eq!(
            view.rate(),
            &RateBound::Seconds(BTreeMap::from([(60, 3), (3_600, 20)]))
        );
        assert_eq!(view.expiry(), ExpiryBound::CoreTimeMs(1_500_000));
        assert_eq!(view.purposes(), &record.purposes);
        assert_eq!(
            view.evaluable(),
            Err(CapabilityViewRefusal::Unhonoured(Dimension::Expiry))
        );

        record.purposes.insert(String::new());
        assert_eq!(
            CapabilityView::try_from(&record),
            Err(CapabilityViewRefusal::Malformed(Dimension::Purpose))
        );
        record.purposes.remove("");
        record.revoked = Some((7, 9));
        assert_eq!(
            CapabilityView::try_from(&record),
            Err(CapabilityViewRefusal::Revoked)
        );
    }

    #[test]
    fn no_purpose_never_matches_purpose_allow_list() {
        assert_eq!(PurposeText::try_from(String::new()), Err(EmptyPurpose));
        let allow_list = BTreeSet::from(["research".to_owned()]);
        assert!(!purpose_listed(&allow_list, &Purpose::None));
        assert!(purpose_listed(&allow_list, &research()));

        let legacy = PolicyIntentRequest::from(&PolicyRequest {
            activity_type: 7,
            counterparty: [8; 32],
            asset: [9; 32],
            amount: 100,
            purpose: String::new(),
            core_sequence: 120,
        });
        assert_eq!(legacy.purpose, Purpose::None);
        assert_eq!(legacy.effects, vec![effect(9, 100)]);

        let rate = RateCeiling {
            maximum_uses: 10,
            window_sequences: 100,
        };
        let capability = CapabilityView::from(&legacy_capability(BTreeSet::from([7])));
        assert!(!capability_admits(&capability, &legacy, 200, rate, 0));
    }
}

impl EvaluationInput<'_> {
    pub(crate) fn program_requirement_context(
        &self,
        tenant: &str,
        actor: &[u8],
        session_id: [u8; 32],
        generation: u64,
        policy: &PolicySet,
        capability: Option<CapabilityId>,
        prepared_at: u64,
    ) -> Result<Vec<u8>, ()> {
        use serde_json::json;
        if self.focus.is_some()
            || self.aggregate.is_none()
            || !self.session.open
            || self.session.request.tenant.as_str() != tenant
            || self.session.request.agent.as_bytes() != actor
            || self.session.request.session_id.0 != session_id
            || self.session.generation != generation
            || self.capability.tenant.as_str() != tenant
            || self.intent.core_sequence != prepared_at
            || self.intent.core_sequence >= self.session.request.expiry_sequence
            || self.session.request.policy_version != policy.version
            || capability.is_some_and(|id| id != self.capability.id)
            || self.intent.effects.iter().any(|effect| {
                !self
                    .session
                    .request
                    .permitted_activity_types
                    .contains(&effect.activity_type)
            })
            || !valid_context(policy, self).map_err(|_| ())?
        {
            return Err(());
        }
        let width = u64::try_from(self.intent.effects.len()).map_err(|_| ())?;
        let rules = u64::try_from(policy.rules.len()).map_err(|_| ())?;
        if rules
            .checked_mul(width)
            .is_none_or(|steps| steps > policy.evaluation_step_limit)
        {
            return Err(());
        }
        for index in 0..self.intent.effects.len() {
            let focused = self.focused(index);
            let mut covered = false;
            for rule in &policy.rules {
                if rule.effect == RuleEffect::Permit
                    && DeterministicMatcher
                        .matches(rule, &focused)
                        .map_err(|_| ())?
                {
                    covered = true;
                }
            }
            if !covered {
                return Err(());
            }
        }
        let authority = match &self.session.request.authority {
            crate::identity::ProtocolAuthority::PrimaryKey(id) => json!(["primary_key", id]),
            crate::identity::ProtocolAuthority::SessionKey(id) => json!(["session_key", id]),
            crate::identity::ProtocolAuthority::CapabilityGrant(id) => {
                json!(["capability_grant", id])
            }
        };
        let amount = match &self.capability.amount {
            AmountBound::Uniform(n) => json!(["uniform", n.to_string()]),
            AmountBound::PerAsset(values) => json!([
                "per_asset",
                values
                    .iter()
                    .map(|(asset, n)| json!([asset, n.to_string()]))
                    .collect::<Vec<_>>()
            ]),
        };
        let rate = match &self.capability.rate {
            RateBound::Sequences(v) => json!(["sequences", v.maximum_uses, v.window_sequences]),
            RateBound::Seconds(values) => json!([
                "seconds",
                values
                    .iter()
                    .map(|(window, n)| json!([window, n]))
                    .collect::<Vec<_>>()
            ]),
        };
        let expiry = match self.capability.expiry {
            ExpiryBound::Sequence(n) => json!(["sequence", n]),
            ExpiryBound::CoreTimeMs(n) => json!(["core_ms", n.to_string()]),
        };
        let cumulative = match self.context {
            VerifiedPolicyContext::Unavailable => json!(["unavailable"]),
            VerifiedPolicyContext::ProtocolBudget(v) => json!([
                "protocol_budget",
                v.protocol_consumed().to_string(),
                v.observed_head_sequence(),
                v.window_start_sequence(),
                v.window_end_sequence()
            ]),
            VerifiedPolicyContext::Authenticated(v) => {
                if v.actor().as_bytes() != actor {
                    return Err(());
                }
                json!([
                    "authenticated",
                    v.actor().as_bytes(),
                    v.window().first,
                    v.window().last,
                    v.amount().to_string(),
                    v.count()
                ])
            }
        };
        let purpose = match &self.intent.purpose {
            Purpose::None => json!(["none"]),
            Purpose::Text(v) => json!(["text", v.as_str()]),
        };
        serde_json::to_vec(&json!({"version":1,"tenant":tenant,"actor":actor,
            "session":session_id,"generation":generation,"authority":authority,
            "session_scopes":self.session.request.scopes,
            "session_activity_types":self.session.request.permitted_activity_types,
            "session_expiry_sequence":self.session.request.expiry_sequence,
            "session_expiry_seconds":self.session.request.expiry_seconds,
            "session_policy_version":self.session.request.policy_version,
            "capability":self.capability.id.0,"activity_types":self.capability.activity_types,
            "counterparties":self.capability.counterparties,"assets":self.capability.assets,
            "amount":amount,"rate":rate,"expiry":expiry,"purposes":self.capability.purposes,
            "core_sequence":self.intent.core_sequence,"purpose":purpose,
            "effects":self.intent.effects.iter().map(|v|json!([v.activity_type,v.counterparty,
                v.asset,v.amount.to_string()])).collect::<Vec<_>>(),
            "cumulative":cumulative}))
        .map_err(|_| ())
    }
}
