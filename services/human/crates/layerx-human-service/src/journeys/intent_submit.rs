//! Execution of a signed unified plan through the existing movement journeys.

use std::fmt::{Display, Formatter};

use layerx_agent_api::identity::{AgentDid, AuthorityRef};
use layerx_sdk::Client as AgentClient;
use layerx_types::payload::ModuleRegistry;
use serde_json::{json, Value};
use sha2::{Digest as _, Sha256};

use crate::binding::BindingJourney;
use crate::custody::{CustodySigner, KeyId, Operation};
use crate::notify::JourneyId;
use crate::store::PrincipalScope;
use crate::trace::TraceId;

use super::{
    AgentBoundary, DepositForwardFailure, DepositForwardJourney, DepositForwardStage,
    DepositForwardStatus, DepositJourney, DepositJourneyError, DepositPlan, DepositStage,
    DepositStatus, Domain, Endpoint, ExecutableShape, IntentWithdrawalJourney,
    IntentWithdrawalStage, IntentWithdrawalStatus, JourneyEngine, JourneyError, JourneyLeg,
    JourneyPlan, JourneyState, JourneyStatus, LegMechanism, Mechanism, RequiredAuthority,
    RouteRequest, RouteResolver, UnifiedPlan, WithdrawalJourneyError, WithdrawalPlan,
    WithdrawalStage,
};

/// The submit classifier is the planner's executable shape contract.
pub use super::ExecutableShape as IntentShape;

const JOURNEY_DOMAIN: &[u8] = b"layerx-human-intent-journey/v1";
const SELF_RELATIONSHIP: &str = "self";
const JOURNEY_ID_BYTES: usize = 13;
const DRIVE_STEPS: usize = 6;
const TEXT_LIMIT: usize = 256;

/// Returns the signing authority label the plan operation publishes for a leg.
#[must_use]
pub const fn authority_label(authority: RequiredAuthority) -> &'static str {
    match authority {
        RequiredAuthority::PaxeerWalletKey => "paxeer-wallet-key",
        RequiredAuthority::AccountOwner => "account-owner",
        RequiredAuthority::Allowance(_) => "allowance",
    }
}

/// One submitted leg binding exactly as the contract carries it.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct IntentLegBinding {
    pub leg_index: usize,
    pub action_key: [u8; 32],
    pub actor: String,
    pub authority: String,
    pub relationship: String,
    pub account_sequence: u64,
    pub not_before: u64,
    pub not_after: u64,
    pub fee_limit: u128,
    pub fee_currency: String,
}

impl IntentLegBinding {
    /// Decodes one `IntentLegBinding` document.
    ///
    /// # Errors
    ///
    /// Refuses a missing, mistyped or out-of-range field.
    pub fn from_json(value: &Value) -> Result<Self, SubmitRefusal> {
        let leg_index = usize::try_from(unsigned(value, "leg_index")?)
            .map_err(|_| SubmitRefusal::Malformed { field: "leg_index" })?;
        let fee = value
            .get("fee_limit")
            .ok_or(SubmitRefusal::Malformed { field: "fee_limit" })?;
        let fee_limit = text(fee, "amount", "fee_limit")?
            .parse::<u128>()
            .map_err(|_| SubmitRefusal::Malformed { field: "fee_limit" })?;
        Ok(Self {
            leg_index,
            action_key: hex32(text(value, "action_key", "action_key")?).ok_or(
                SubmitRefusal::Malformed {
                    field: "action_key",
                },
            )?,
            actor: text(value, "actor", "actor")?.to_owned(),
            authority: text(value, "authority", "authority")?.to_owned(),
            relationship: text(value, "relationship", "relationship")?.to_owned(),
            account_sequence: unsigned(value, "account_sequence")?,
            not_before: unsigned(value, "not_before")?,
            not_after: unsigned(value, "not_after")?,
            fee_limit,
            fee_currency: text(fee, "currency", "fee_limit")?.to_owned(),
        })
    }
}

/// The signed submission of one plan: its digest, the signed digest and one
/// binding per leg.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct SubmitPlanRequest {
    pub plan_digest: [u8; 32],
    pub signed_digest: [u8; 32],
    pub bindings: Vec<IntentLegBinding>,
}

impl SubmitPlanRequest {
    /// Decodes the `SubmitPlanRequest` fields of a submission body.
    ///
    /// # Errors
    ///
    /// Refuses a missing or malformed digest and any malformed binding.
    pub fn from_json(body: &Value) -> Result<Self, SubmitRefusal> {
        let bindings = body
            .get("bindings")
            .and_then(Value::as_array)
            .ok_or(SubmitRefusal::Malformed { field: "bindings" })?
            .iter()
            .map(IntentLegBinding::from_json)
            .collect::<Result<Vec<_>, _>>()?;
        Ok(Self {
            plan_digest: hex32(text(body, "plan_digest", "plan_digest")?).ok_or(
                SubmitRefusal::Malformed {
                    field: "plan_digest",
                },
            )?,
            signed_digest: hex32(text(body, "signed_digest", "signed_digest")?).ok_or(
                SubmitRefusal::Malformed {
                    field: "signed_digest",
                },
            )?,
            bindings,
        })
    }
}

/// What the service itself observed for the signed-in owner at submission
/// time. The caller's bindings are checked against this, never trusted.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct BindingExpectation {
    pub actor: AgentDid,
    pub authority: AuthorityRef,
    pub account_sequence: u64,
    pub currency: String,
    pub now: u64,
}

/// Verifies the signed digest and every leg binding against the plan the
/// service re-planned at submission time.
///
/// # Errors
///
/// Refuses a digest that no longer matches, a binding count that differs
/// from the leg count, a binding naming another leg, action key, actor,
/// authority, relationship or currency, a fee limit outside the leg fee and
/// the intent ceiling, a stale account sequence and a validity window that
/// does not contain the submission time.
pub fn verify_bindings(
    plan: &UnifiedPlan,
    request: &SubmitPlanRequest,
    expectation: &BindingExpectation,
) -> Result<(), SubmitRefusal> {
    if request.plan_digest != plan.digest() {
        return Err(SubmitRefusal::PlanDigestMismatch);
    }
    if request.signed_digest != plan.digest() {
        return Err(SubmitRefusal::SignedDigestMismatch);
    }
    if request.bindings.len() != plan.legs().len() {
        return Err(SubmitRefusal::UnboundLegs {
            expected: plan.legs().len(),
            bound: request.bindings.len(),
        });
    }
    let constraints = plan.intent().constraints();
    let mut sequence = expectation.account_sequence;
    for (leg, binding) in plan.legs().iter().zip(&request.bindings) {
        let index = leg.index();
        let action_key = plan
            .action_key(index)
            .map_err(|_| SubmitRefusal::LegMismatch { index })?;
        if binding.leg_index != index
            || binding.action_key != action_key
            || binding.authority != authority_label(leg.authority())
            || binding.actor != expectation.actor.as_str()
            || binding.relationship != SELF_RELATIONSHIP
            || binding.fee_currency != expectation.currency
            || binding.not_before > binding.not_after
            || binding.not_after > constraints.deadline().value()
        {
            return Err(SubmitRefusal::LegMismatch { index });
        }
        if binding.fee_limit < leg.fee() || binding.fee_limit > constraints.max_fee() {
            return Err(SubmitRefusal::FeeLimitMismatch { index });
        }
        if binding.account_sequence != sequence {
            return Err(SubmitRefusal::StaleSequence { index });
        }
        if expectation.now < binding.not_before || expectation.now > binding.not_after {
            return Err(SubmitRefusal::WindowExpired { index });
        }
        if leg.domain() == Domain::LayerX {
            sequence = sequence
                .checked_add(1)
                .ok_or(SubmitRefusal::LegMismatch { index })?;
        }
    }
    Ok(())
}

/// Derives the stable journey identifier of one submission, so a repeated
/// submission under the same idempotency key names the original journey.
///
/// # Errors
///
/// Refuses an empty or oversized idempotency key.
pub fn intent_journey_id(
    idempotency_key: &str,
    plan_digest: [u8; 32],
) -> Result<JourneyId, SubmitRefusal> {
    if idempotency_key.is_empty() || idempotency_key.len() > TEXT_LIMIT {
        return Err(SubmitRefusal::Malformed {
            field: "Idempotency-Key",
        });
    }
    let length = u16::try_from(idempotency_key.len()).map_err(|_| SubmitRefusal::Malformed {
        field: "Idempotency-Key",
    })?;
    let mut digest = Sha256::new();
    digest.update(JOURNEY_DOMAIN);
    digest.update(length.to_be_bytes());
    digest.update(idempotency_key.as_bytes());
    digest.update(plan_digest);
    let derived: [u8; 32] = digest.finalize().into();
    JourneyId::new(format!("jrn_{}", hex(&derived[..JOURNEY_ID_BYTES])))
        .map_err(|_| SubmitRefusal::JourneyUnavailable)
}

/// Everything a kernel intent journey needs besides the plan and its bindings.
pub struct KernelStart<'a> {
    pub routes: &'a [RouteRequest],
    pub journey_id: JourneyId,
    pub custody_key: KeyId,
    pub registry: &'a ModuleRegistry,
}

/// Creates the durable engine journey for a verified plan whose legs all run
/// inside `LayerX`. Each leg's relationship material must resolve to exactly
/// the planned mechanism, term, endpoints, asset and amount.
///
/// # Errors
///
/// Refuses any binding defect, a plan of another shape, a route that does not
/// resolve to its planned leg, a submission already used under another
/// idempotency key, and a journey the engine refuses to persist.
pub fn start_kernel_journey(
    scope: &mut PrincipalScope<'_>,
    plan: &UnifiedPlan,
    request: &SubmitPlanRequest,
    expectation: &BindingExpectation,
    start: KernelStart<'_>,
) -> Result<JourneyEngine, SubmitRefusal> {
    start_kernel_profile(scope, plan, request, expectation, start, false)
}

pub fn start_native_kernel_journey(
    scope: &mut PrincipalScope<'_>,
    plan: &UnifiedPlan,
    request: &SubmitPlanRequest,
    expectation: &BindingExpectation,
    start: KernelStart<'_>,
) -> Result<JourneyEngine, SubmitRefusal> {
    start_kernel_profile(scope, plan, request, expectation, start, true)
}

fn start_kernel_profile(
    scope: &mut PrincipalScope<'_>,
    plan: &UnifiedPlan,
    request: &SubmitPlanRequest,
    expectation: &BindingExpectation,
    start: KernelStart<'_>,
    native: bool,
) -> Result<JourneyEngine, SubmitRefusal> {
    verify_bindings(plan, request, expectation)?;
    if plan.executable_shape() != ExecutableShape::Kernel {
        return Err(SubmitRefusal::UnsupportedPlan);
    }
    let legs = kernel_journey_legs(plan, request, expectation, start.routes)?;
    let journey_plan = JourneyPlan::new(
        start.journey_id,
        plan.journey_kind(),
        plan.digest(),
        start.custody_key,
        Operation::ProtocolMutation,
        legs,
    )
    .map_err(|_| SubmitRefusal::JourneyUnavailable)?;
    let engine = if native {
        if plan
            .legs()
            .iter()
            .any(|leg| leg.mechanism() != LegMechanism::Protocol(Mechanism::Send))
        {
            return Err(SubmitRefusal::UnsupportedPlan);
        }
        JourneyEngine::start_native(scope, &journey_plan, start.registry, expectation.now)
    } else {
        JourneyEngine::start(scope, &journey_plan, start.registry, expectation.now)
    };
    engine.map_err(|error| match error {
        JourneyError::IdempotencyConflict => SubmitRefusal::AlreadySubmitted,
        _ => SubmitRefusal::JourneyUnavailable,
    })
}

/// Resolves the kernel legs the plan's shape runs through the native journey
/// engine. Each route must resolve to exactly the planned mechanism, term,
/// endpoints, asset and amount of the leg at the same position.
fn kernel_journey_legs(
    plan: &UnifiedPlan,
    request: &SubmitPlanRequest,
    expectation: &BindingExpectation,
    routes: &[RouteRequest],
) -> Result<Vec<JourneyLeg>, SubmitRefusal> {
    let (first, end) = plan.executable_shape().kernel_legs(plan.legs().len());
    let planned = plan.legs().get(first..end).unwrap_or_default();
    let bound = request.bindings.get(first..end).unwrap_or_default();
    if planned.is_empty() || routes.len() != planned.len() || bound.len() != planned.len() {
        return Err(SubmitRefusal::UnboundLegs {
            expected: planned.len(),
            bound: routes.len(),
        });
    }
    let mut legs = Vec::with_capacity(planned.len());
    for ((leg, binding), route) in planned.iter().zip(bound).zip(routes) {
        let index = leg.index();
        if !ExecutableShape::is_kernel(leg.mechanism()) || binding.leg_index != index {
            return Err(SubmitRefusal::LegMismatch { index });
        }
        if &route.source != leg.source()
            || &route.destination != leg.destination()
            || route.asset != leg.asset()
            || route.amount != leg.amount()
        {
            return Err(SubmitRefusal::RouteMismatch { index });
        }
        let resolved =
            RouteResolver::resolve(route).map_err(|_| SubmitRefusal::RouteMismatch { index })?;
        let [single] = resolved.legs() else {
            return Err(SubmitRefusal::RouteMismatch { index });
        };
        if Some(single.mechanism()) != leg.mechanism().protocol() || single.term() != leg.term() {
            return Err(SubmitRefusal::RouteMismatch { index });
        }
        legs.push(
            JourneyLeg::new(
                single.intent().clone(),
                binding.action_key,
                expectation.actor.clone(),
                expectation.authority.clone(),
                binding.account_sequence,
                binding.not_before,
                binding.not_after,
                binding.fee_limit,
            )
            .map_err(|_| SubmitRefusal::LegMismatch { index })?,
        );
    }
    Ok(legs)
}

/// The service boundaries one kernel journey advances through.
pub struct IntentDriver<'a> {
    pub agent_contract: &'a AgentClient,
    pub custody: &'a CustodySigner,
    pub registry: &'a ModuleRegistry,
    pub trace: &'a TraceId,
    pub now: u64,
}

/// Advances a kernel intent journey through getting ready, signing and
/// sending, stopping once it is processing or terminal. Later progress is
/// read and continued like every other engine journey.
///
/// # Errors
///
/// Returns the engine's typed agent, custody, receipt and storage failures.
pub async fn drive_intent_journey(
    engine: &mut JourneyEngine,
    scope: &mut PrincipalScope<'_>,
    agent: &mut dyn AgentBoundary,
    driver: &IntentDriver<'_>,
) -> Result<JourneyStatus, JourneyError> {
    let mut status = engine.status()?;
    for _ in 0..DRIVE_STEPS {
        if !matches!(
            status.state(),
            JourneyState::GettingReady | JourneyState::Sending
        ) {
            break;
        }
        status = engine
            .advance(
                scope,
                driver.agent_contract,
                agent,
                driver.custody,
                driver.registry,
                driver.trace,
                driver.now,
            )
            .await?;
    }
    Ok(status)
}

/// Starts the existing deposit journey for a verified custody-deposit plan.
/// The deposit plan must credit exactly the planned amount and asset to the
/// planned home account under the observed owner and the bound sequence.
///
/// # Errors
///
/// Refuses any binding defect, a plan of another shape, a deposit plan that
/// differs from the signed plan, an unbound wallet, a submission already used
/// under another idempotency key, and a journey that cannot be persisted.
pub fn start_deposit_journey(
    scope: &mut PrincipalScope<'_>,
    plan: &UnifiedPlan,
    request: &SubmitPlanRequest,
    expectation: &BindingExpectation,
    deposit: &DepositPlan,
    binding: &BindingJourney,
) -> Result<DepositJourney, SubmitRefusal> {
    verify_bindings(plan, request, expectation)?;
    if plan.executable_shape() != ExecutableShape::CustodyDeposit {
        return Err(SubmitRefusal::UnsupportedPlan);
    }
    check_deposit(plan, request, expectation, deposit)?;
    DepositJourney::start(scope, binding, deposit, expectation.now).map_err(|error| match error {
        DepositJourneyError::IdempotencyConflict => SubmitRefusal::AlreadySubmitted,
        DepositJourneyError::BindingUnavailable => SubmitRefusal::WalletNotBound,
        _ => SubmitRefusal::JourneyUnavailable,
    })
}

/// Starts the durable deposit-forwarding parent for a verified plan that
/// deposits into the home account and then forwards through `LayerX` legs.
/// The deposit child, the forward engine and the parent row are persisted
/// together; the forward legs cannot prepare until the parent has verified
/// the credit receipt.
///
/// # Errors
///
/// Refuses any binding defect, a plan of another shape, a deposit plan that
/// differs from the signed credit leg, a forward route that does not resolve to
/// its planned leg, a submission already used under another key, and a journey
/// that cannot be persisted.
pub fn start_deposit_forward_journey(
    scope: &mut PrincipalScope<'_>,
    plan: &UnifiedPlan,
    request: &SubmitPlanRequest,
    expectation: &BindingExpectation,
    deposit: &DepositPlan,
    binding: &BindingJourney,
    forward: KernelStart<'_>,
) -> Result<DepositForwardJourney, SubmitRefusal> {
    verify_bindings(plan, request, expectation)?;
    if plan.executable_shape() != ExecutableShape::DepositForward {
        return Err(SubmitRefusal::UnsupportedPlan);
    }
    check_deposit(plan, request, expectation, deposit)?;
    let legs = kernel_journey_legs(plan, request, expectation, forward.routes)?;
    DepositForwardJourney::start(
        scope,
        binding,
        deposit,
        legs,
        forward.custody_key,
        forward.registry,
        forward.journey_id,
        plan.digest(),
        expectation.now,
    )
    .map_err(|error| match error {
        DepositJourneyError::IdempotencyConflict
        | DepositJourneyError::Journey(JourneyError::IdempotencyConflict) => {
            SubmitRefusal::AlreadySubmitted
        }
        DepositJourneyError::BindingUnavailable => SubmitRefusal::WalletNotBound,
        _ => SubmitRefusal::JourneyUnavailable,
    })
}

/// Starts the durable withdrawal parent for a verified plan that ends in a
/// withdrawal request and its Paxeer finalisation. A transfer-then-withdraw
/// plan persists its `LayerX` prelude first; the withdrawal itself starts only
/// after every prelude leg carries verified receipt evidence.
///
/// # Errors
///
/// Refuses any binding defect, a plan of another shape, a withdrawal plan that
/// differs from the signed request leg, a prelude route that does not resolve
/// to its planned leg, a submission already used under another key, and a
/// journey that cannot be persisted.
pub fn start_withdrawal_intent(
    scope: &mut PrincipalScope<'_>,
    plan: &UnifiedPlan,
    request: &SubmitPlanRequest,
    expectation: &BindingExpectation,
    withdrawal: &WithdrawalPlan,
    prelude: KernelStart<'_>,
) -> Result<IntentWithdrawalJourney, SubmitRefusal> {
    verify_bindings(plan, request, expectation)?;
    let shape = plan.executable_shape();
    if !matches!(
        shape,
        ExecutableShape::WithdrawToWallet | ExecutableShape::TransferThenWithdraw
    ) {
        return Err(SubmitRefusal::UnsupportedPlan);
    }
    let count = plan.legs().len();
    let (Some(debit), Some(debit_binding), Some(finalise)) = (
        plan.legs().get(count.saturating_sub(2)),
        request.bindings.get(count.saturating_sub(2)),
        plan.legs().last(),
    ) else {
        return Err(SubmitRefusal::UnsupportedPlan);
    };
    let Endpoint::Human(owner) = debit.source() else {
        return Err(SubmitRefusal::WithdrawalMismatch);
    };
    if debit.mechanism() != LegMechanism::Protocol(Mechanism::BridgeWithdrawRequest)
        || finalise.mechanism() != LegMechanism::PaxeerWithdrawFinalise
        || withdrawal.amount != debit.amount()
        || withdrawal.asset != debit.asset()
        || &withdrawal.owner != owner
        || withdrawal.currency != expectation.currency
        || withdrawal.agent.actor != expectation.actor
        || withdrawal.agent.authority != expectation.authority
        || withdrawal.agent.account_sequence != debit_binding.account_sequence
        || withdrawal.agent.fee_limit > debit_binding.fee_limit
        || withdrawal.agent.not_before < debit_binding.not_before
        || withdrawal.agent.not_after > debit_binding.not_after
    {
        return Err(SubmitRefusal::WithdrawalMismatch);
    }
    let prelude_legs = if shape == ExecutableShape::TransferThenWithdraw {
        kernel_journey_legs(plan, request, expectation, prelude.routes)?
    } else if prelude.routes.is_empty() {
        Vec::new()
    } else {
        return Err(SubmitRefusal::UnboundLegs {
            expected: 0,
            bound: prelude.routes.len(),
        });
    };
    IntentWithdrawalJourney::start(
        scope,
        prelude.journey_id,
        plan.digest(),
        prelude_legs,
        prelude.custody_key,
        prelude.registry,
        withdrawal,
        expectation.now,
    )
    .map_err(|error| match error {
        WithdrawalJourneyError::IdempotencyConflict
        | WithdrawalJourneyError::Journey(JourneyError::IdempotencyConflict) => {
            SubmitRefusal::AlreadySubmitted
        }
        _ => SubmitRefusal::JourneyUnavailable,
    })
}

fn check_deposit(
    plan: &UnifiedPlan,
    request: &SubmitPlanRequest,
    expectation: &BindingExpectation,
    deposit: &DepositPlan,
) -> Result<(), SubmitRefusal> {
    let (Some(custody), Some(credit), Some(credit_binding)) = (
        plan.legs().first(),
        plan.legs().get(1),
        request.bindings.get(1),
    ) else {
        return Err(SubmitRefusal::UnsupportedPlan);
    };
    if custody.mechanism() != LegMechanism::PaxeerCustodyDeposit
        || credit.mechanism() != LegMechanism::Protocol(Mechanism::BridgeDepositCredit)
    {
        return Err(SubmitRefusal::UnsupportedPlan);
    }
    let Endpoint::Human(recipient) = credit.destination() else {
        return Err(SubmitRefusal::DepositMismatch);
    };
    if deposit.amount != credit.amount()
        || deposit.asset != credit.asset()
        || &deposit.recipient != recipient
        || deposit.currency != expectation.currency
        || deposit.agent.actor != expectation.actor
        || deposit.agent.authority != expectation.authority
        || deposit.agent.account_sequence != credit_binding.account_sequence
        || deposit.agent.fee_limit > credit_binding.fee_limit
    {
        return Err(SubmitRefusal::DepositMismatch);
    }
    Ok(())
}

/// The `IntentSubmission` the submit operation returns.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct IntentSubmission {
    journey_id: JourneyId,
    plan_digest: [u8; 32],
    state: &'static str,
}

impl IntentSubmission {
    /// Describes a kernel intent journey at its current state.
    #[must_use]
    pub fn from_kernel(status: &JourneyStatus, plan_digest: [u8; 32]) -> Self {
        let state = match status.state() {
            JourneyState::GettingReady => "getting-ready",
            JourneyState::Sending => "sending",
            JourneyState::Processing => "processing",
            JourneyState::StillChecking => "still-checking",
            JourneyState::Done => "done",
            JourneyState::Refused => "refused",
        };
        Self {
            journey_id: status.journey_id().clone(),
            plan_digest,
            state,
        }
    }

    /// Describes a custody-deposit intent journey at its current stage.
    #[must_use]
    pub fn from_deposit(status: &DepositStatus, plan_digest: [u8; 32]) -> Self {
        let state = match status.stage() {
            DepositStage::WaitingForWallet => "waiting-for-you",
            DepositStage::ConfirmingPaxeer { .. } | DepositStage::CreditingLayerX => "processing",
            DepositStage::Done => "done",
            DepositStage::Failed(_) => "refused",
        };
        Self {
            journey_id: status.journey_id().clone(),
            plan_digest,
            state,
        }
    }

    /// Describes a deposit-forwarding parent journey at its current stage.
    #[must_use]
    pub fn from_deposit_forward(status: &DepositForwardStatus, plan_digest: [u8; 32]) -> Self {
        let state = match status.stage() {
            DepositForwardStage::Depositing(DepositStage::WaitingForWallet) => "waiting-for-you",
            DepositForwardStage::Depositing(DepositStage::Failed(_))
            | DepositForwardStage::Failed(
                DepositForwardFailure::DepositFailed(_) | DepositForwardFailure::ForwardRefused,
            ) => "refused",
            DepositForwardStage::Depositing(
                DepositStage::ConfirmingPaxeer { .. }
                | DepositStage::CreditingLayerX
                | DepositStage::Done,
            )
            | DepositForwardStage::Forwarding => "processing",
            DepositForwardStage::Done => "done",
        };
        Self {
            journey_id: status.journey_id().clone(),
            plan_digest,
            state,
        }
    }

    /// Describes a withdrawal parent journey at its current stage.
    #[must_use]
    pub fn from_withdrawal(status: &IntentWithdrawalStatus, plan_digest: [u8; 32]) -> Self {
        let state = match status.stage() {
            IntentWithdrawalStage::Transferring => "processing",
            IntentWithdrawalStage::Withdrawing(
                WithdrawalStage::ReadyToClaim | WithdrawalStage::ReadyToFinalise,
            )
            | IntentWithdrawalStage::AwaitingAuthorization => "waiting-for-you",
            IntentWithdrawalStage::Withdrawing(
                WithdrawalStage::Processing
                | WithdrawalStage::WaitingForSettlement { .. }
                | WithdrawalStage::ClaimSubmitting
                | WithdrawalStage::WaitingForChallengeWindow { .. }
                | WithdrawalStage::VerifyingPayout,
            ) => "processing",
            IntentWithdrawalStage::Withdrawing(WithdrawalStage::PaidOut(_)) => "done",
            IntentWithdrawalStage::Withdrawing(WithdrawalStage::Cancelled(_))
            | IntentWithdrawalStage::TransferRefused => "refused",
        };
        Self {
            journey_id: status.journey_id().clone(),
            plan_digest,
            state,
        }
    }

    #[must_use]
    pub const fn journey_id(&self) -> &JourneyId {
        &self.journey_id
    }

    #[must_use]
    pub const fn plan_digest(&self) -> [u8; 32] {
        self.plan_digest
    }

    #[must_use]
    pub const fn state(&self) -> &'static str {
        self.state
    }

    #[must_use]
    pub fn state_copy_key(&self) -> String {
        format!("status.{}", self.state)
    }

    #[must_use]
    pub fn to_json(&self) -> Value {
        json!({
            "journey_id": self.journey_id.as_str(),
            "plan_digest": hex(&self.plan_digest),
            "state": self.state,
            "state_copy_key": self.state_copy_key(),
        })
    }
}

/// Typed refusal of an intent submission. No variant creates a journey.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum SubmitRefusal {
    Malformed { field: &'static str },
    PlanDigestMismatch,
    SignedDigestMismatch,
    UnboundLegs { expected: usize, bound: usize },
    LegMismatch { index: usize },
    FeeLimitMismatch { index: usize },
    StaleSequence { index: usize },
    WindowExpired { index: usize },
    RouteMismatch { index: usize },
    DepositMismatch,
    WithdrawalMismatch,
    UnsupportedPlan,
    WalletNotBound,
    AlreadySubmitted,
    JourneyUnavailable,
}

impl SubmitRefusal {
    /// Returns the HTTP status the contract pairs with the refusal code.
    #[must_use]
    pub const fn status(self) -> u16 {
        match self {
            Self::Malformed { .. } => 400,
            Self::StaleSequence { .. }
            | Self::WindowExpired { .. }
            | Self::AlreadySubmitted
            | Self::WalletNotBound => 409,
            Self::JourneyUnavailable => 503,
            Self::PlanDigestMismatch
            | Self::SignedDigestMismatch
            | Self::UnboundLegs { .. }
            | Self::LegMismatch { .. }
            | Self::FeeLimitMismatch { .. }
            | Self::RouteMismatch { .. }
            | Self::DepositMismatch
            | Self::WithdrawalMismatch
            | Self::UnsupportedPlan => 403,
        }
    }

    /// Returns the stable `ErrorCode` from the errors contract.
    #[must_use]
    pub const fn code(self) -> &'static str {
        match self {
            Self::Malformed { .. } => "invalid-request",
            Self::StaleSequence { .. } | Self::AlreadySubmitted => "conflict",
            Self::WindowExpired { .. } => "quote-expired",
            Self::WalletNotBound => "wallet-not-bound",
            Self::JourneyUnavailable => "unavailable",
            Self::PlanDigestMismatch
            | Self::SignedDigestMismatch
            | Self::UnboundLegs { .. }
            | Self::LegMismatch { .. }
            | Self::FeeLimitMismatch { .. }
            | Self::RouteMismatch { .. }
            | Self::DepositMismatch
            | Self::WithdrawalMismatch
            | Self::UnsupportedPlan => "forbidden",
        }
    }

    /// Returns the copy-catalog key naming the human message.
    #[must_use]
    pub const fn copy_key(self) -> &'static str {
        match self {
            Self::Malformed { .. } => "error.request.invalid",
            Self::StaleSequence { .. } | Self::WindowExpired { .. } | Self::AlreadySubmitted => {
                "error.move.quote-expired"
            }
            Self::WalletNotBound => "error.wallet.not-bound",
            Self::JourneyUnavailable => "error.service.unavailable",
            Self::PlanDigestMismatch
            | Self::SignedDigestMismatch
            | Self::UnboundLegs { .. }
            | Self::LegMismatch { .. }
            | Self::FeeLimitMismatch { .. }
            | Self::RouteMismatch { .. }
            | Self::DepositMismatch
            | Self::WithdrawalMismatch
            | Self::UnsupportedPlan => "error.request.forbidden",
        }
    }

    /// Returns the retriability classification.
    #[must_use]
    pub const fn retry(self) -> &'static str {
        match self {
            Self::StaleSequence { .. }
            | Self::WindowExpired { .. }
            | Self::AlreadySubmitted
            | Self::WalletNotBound => "structural",
            Self::JourneyUnavailable => "retriable",
            Self::Malformed { .. }
            | Self::PlanDigestMismatch
            | Self::SignedDigestMismatch
            | Self::UnboundLegs { .. }
            | Self::LegMismatch { .. }
            | Self::FeeLimitMismatch { .. }
            | Self::RouteMismatch { .. }
            | Self::DepositMismatch
            | Self::WithdrawalMismatch
            | Self::UnsupportedPlan => "final",
        }
    }

    /// Returns the request field a malformed submission named.
    #[must_use]
    pub const fn field(self) -> Option<&'static str> {
        match self {
            Self::Malformed { field } => Some(field),
            _ => None,
        }
    }
}

impl Display for SubmitRefusal {
    fn fmt(&self, formatter: &mut Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::Malformed { field } => {
                write!(formatter, "the submission field {field} is malformed")
            }
            Self::PlanDigestMismatch => {
                formatter.write_str("the re-planned digest differs from the submitted plan digest")
            }
            Self::SignedDigestMismatch => {
                formatter.write_str("the signed digest differs from the re-planned digest")
            }
            Self::UnboundLegs { expected, bound } => write!(
                formatter,
                "the plan has {expected} legs and {bound} were bound"
            ),
            Self::LegMismatch { index } => {
                write!(
                    formatter,
                    "the binding for leg {index} does not match the plan"
                )
            }
            Self::FeeLimitMismatch { index } => write!(
                formatter,
                "the fee limit for leg {index} is outside the leg fee and the intent ceiling"
            ),
            Self::StaleSequence { index } => {
                write!(
                    formatter,
                    "the account sequence bound to leg {index} is stale"
                )
            }
            Self::WindowExpired { index } => write!(
                formatter,
                "the validity window bound to leg {index} does not contain the submission time"
            ),
            Self::RouteMismatch { index } => {
                write!(
                    formatter,
                    "leg {index} does not resolve to its planned route"
                )
            }
            Self::DepositMismatch => {
                formatter.write_str("the deposit plan differs from the signed plan")
            }
            Self::WithdrawalMismatch => {
                formatter.write_str("the withdrawal plan differs from the signed plan")
            }
            Self::UnsupportedPlan => {
                formatter.write_str("no movement journey executes this plan shape")
            }
            Self::WalletNotBound => formatter.write_str("no Paxeer wallet is bound"),
            Self::AlreadySubmitted => {
                formatter.write_str("this plan was already submitted under another key")
            }
            Self::JourneyUnavailable => formatter.write_str("the journey could not be created"),
        }
    }
}

impl std::error::Error for SubmitRefusal {}

fn text<'a>(value: &'a Value, name: &str, field: &'static str) -> Result<&'a str, SubmitRefusal> {
    value
        .get(name)
        .and_then(Value::as_str)
        .filter(|text| !text.is_empty() && text.len() <= TEXT_LIMIT)
        .ok_or(SubmitRefusal::Malformed { field })
}

fn unsigned(value: &Value, field: &'static str) -> Result<u64, SubmitRefusal> {
    value
        .get(field)
        .and_then(Value::as_u64)
        .ok_or(SubmitRefusal::Malformed { field })
}

fn hex32(text: &str) -> Option<[u8; 32]> {
    if text.len() != 64 {
        return None;
    }
    let nibble = |byte: u8| match byte {
        b'0'..=b'9' => Some(byte - b'0'),
        b'a'..=b'f' => Some(byte - b'a' + 10),
        _ => None,
    };
    let mut out = [0_u8; 32];
    for (slot, pair) in out.iter_mut().zip(text.as_bytes().chunks_exact(2)) {
        *slot = (nibble(pair[0])? << 4) | nibble(pair[1])?;
    }
    Some(out)
}

fn hex(bytes: &[u8]) -> String {
    const DIGITS: &[u8; 16] = b"0123456789abcdef";
    let mut out = String::with_capacity(bytes.len().saturating_mul(2));
    for byte in bytes {
        out.push(char::from(DIGITS[usize::from(byte >> 4)]));
        out.push(char::from(DIGITS[usize::from(byte & 0x0f)]));
    }
    out
}
