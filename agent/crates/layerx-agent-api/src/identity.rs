//! Agent identity, session, capability, and budget contract types.

use crate::write_contract::{PreparationRef, SignatureBytes};
use crate::verify::Level;
use crate::{Amount, BudgetLimit, Sequence, TimestampSeconds};

/// Contract construction failure before a request can cross the daemon boundary.
#[derive(Clone, Debug, Eq, PartialEq)]
pub enum ContractError {
    Empty(&'static str),
    Zero(&'static str),
    DaemonLimitFunding,
    /// The value is present but not in its single canonical form.
    Malformed(&'static str),
    /// The value lies outside the bound the producing authority accepts.
    OutOfRange(&'static str),
    /// Two values that must describe the same observation disagree.
    Mismatch(&'static str),
}

/// Decodes exactly 64 lowercase hexadecimal characters into 32 bytes.
///
/// # Errors
/// Returns [`ContractError::Malformed`] for any other length or character.
pub fn strict_hex32(text: &str, field: &'static str) -> Result<[u8; 32], ContractError> {
    let bytes = text.as_bytes();
    if bytes.len() != 64 {
        return Err(ContractError::Malformed(field));
    }
    let nibble = |byte: u8| match byte {
        b'0'..=b'9' => Ok(byte - b'0'),
        b'a'..=b'f' => Ok(byte - b'a' + 10),
        _ => Err(ContractError::Malformed(field)),
    };
    let mut output = [0_u8; 32];
    for (slot, pair) in output.iter_mut().zip(bytes.chunks_exact(2)) {
        *slot = (nibble(pair[0])? << 4) | nibble(pair[1])?;
    }
    Ok(output)
}

macro_rules! required_text {
    ($name:ident, $field:literal) => {
        #[derive(Clone, Debug, Eq, Hash, Ord, PartialEq, PartialOrd)]
        pub struct $name(String);

        impl $name {
            /// Constructs a non-empty contract identifier.
            ///
            /// # Errors
            /// Returns [`ContractError::Empty`] when the required value is empty.
            pub fn new(value: impl Into<String>) -> Result<Self, ContractError> {
                let value = value.into();
                if value.is_empty() {
                    return Err(ContractError::Empty($field));
                }
                Ok(Self(value))
            }

            #[must_use]
            pub fn as_str(&self) -> &str {
                &self.0
            }
        }
    };
}

required_text!(TenantId, "tenant");
required_text!(AgentDid, "agent_did");
required_text!(AuthorityRef, "authority_ref");
required_text!(ClientId, "client");
required_text!(PolicyVersion, "policy_version");
required_text!(SessionId, "session_id");
required_text!(CapabilityId, "capability_id");
required_text!(BudgetId, "budget_id");
required_text!(Counterparty, "counterparty");
required_text!(Asset, "asset");
required_text!(Purpose, "purpose");

impl CapabilityId {
    /// Returns the 32-byte owner-issued identifier named by this text.
    ///
    /// # Errors
    /// Returns [`ContractError::Malformed`] unless the text is 64 lowercase hex characters.
    pub fn to_bytes(&self) -> Result<[u8; 32], ContractError> {
        strict_hex32(self.as_str(), "capability_id")
    }
}

impl SessionId {
    /// Returns the 32-byte session identifier named by this text.
    ///
    /// # Errors
    /// Returns [`ContractError::Malformed`] unless the text is 64 lowercase hex characters.
    pub fn to_bytes(&self) -> Result<[u8; 32], ContractError> {
        strict_hex32(self.as_str(), "session_id")
    }
}

#[derive(Clone, Copy, Debug, Eq, Hash, Ord, PartialEq, PartialOrd)]
pub struct ActivityType(pub u16);

/// A restriction dimension that a caller must supply explicitly.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ExplicitSet<T>(Vec<T>);

impl<T> ExplicitSet<T> {
    #[must_use]
    pub const fn deny_all() -> Self {
        Self(Vec::new())
    }

    #[must_use]
    pub const fn allow(values: Vec<T>) -> Self {
        Self(values)
    }

    #[must_use]
    pub fn values(&self) -> &[T] {
        &self.0
    }
}

/// Complete session authority context; no operation may synthesize defaults.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct SessionContext {
    pub tenant: TenantId,
    pub agent_did: AgentDid,
    pub authority_ref: AuthorityRef,
    pub permitted_activity_types: ExplicitSet<ActivityType>,
    pub expiry: TimestampSeconds,
    pub client: ClientId,
    pub policy_version: PolicyVersion,
}

impl SessionContext {
    /// Creates a context only when expiry is explicitly nonzero.
    ///
    /// # Errors
    /// Returns [`ContractError::Zero`] when expiry is zero.
    pub fn new(
        tenant: TenantId,
        agent_did: AgentDid,
        authority_ref: AuthorityRef,
        permitted_activity_types: ExplicitSet<ActivityType>,
        expiry: TimestampSeconds,
        client: ClientId,
        policy_version: PolicyVersion,
    ) -> Result<Self, ContractError> {
        if expiry.0 == 0 {
            return Err(ContractError::Zero("expiry"));
        }
        Ok(Self {
            tenant,
            agent_did,
            authority_ref,
            permitted_activity_types,
            expiry,
            client,
            policy_version,
        })
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct AgentRegistration {
    pub tenant: TenantId,
    pub agent_did: AgentDid,
    pub authority_ref: AuthorityRef,
    pub client: ClientId,
    pub policy_version: PolicyVersion,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct SessionOpen(pub SessionContext);

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct SessionRefresh {
    pub session_id: SessionId,
    pub context: SessionContext,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct SessionClose {
    pub session_id: SessionId,
    pub context: SessionContext,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct SessionList(pub SessionContext);

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct AmountCeiling {
    pub asset: Asset,
    pub amount: Amount,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct RateCeiling {
    pub window_seconds: TimestampSeconds,
    pub maximum_actions: u64,
}

/// Every capability dimension is present, including explicitly empty deny sets.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct CapabilityDimensions {
    pub activity_types: ExplicitSet<ActivityType>,
    pub counterparties: ExplicitSet<Counterparty>,
    pub assets: ExplicitSet<Asset>,
    pub amount_ceilings: ExplicitSet<AmountCeiling>,
    pub rate_ceilings: ExplicitSet<RateCeiling>,
    pub purpose_constraints: ExplicitSet<Purpose>,
    pub expiry: TimestampSeconds,
}

impl CapabilityDimensions {
    /// Validates the only scalar dimension that can be malformed.
    ///
    /// # Errors
    /// Returns [`ContractError::Zero`] when expiry is zero.
    pub fn validate(self) -> Result<Self, ContractError> {
        if self.expiry.0 == 0 {
            return Err(ContractError::Zero("expiry"));
        }
        Ok(self)
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct CapabilityCreate {
    pub tenant: TenantId,
    pub agent_did: AgentDid,
    pub dimensions: CapabilityDimensions,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct CapabilityAttenuate {
    pub tenant: TenantId,
    pub agent_did: AgentDid,
    pub parent_id: CapabilityId,
    pub dimensions: CapabilityDimensions,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct CapabilityList {
    pub tenant: TenantId,
    pub agent_did: AgentDid,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct CapabilityRevoke {
    pub tenant: TenantId,
    pub agent_did: AgentDid,
    pub capability_id: CapabilityId,
}

/// States who actually enforces a budget-like restriction.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum BudgetEnforcement {
    ProtocolBudget,
    DaemonLimit,
}

impl BudgetEnforcement {
    pub const DAEMON_LIMIT_NOTICE: &'static str =
        "Bypassing the daemon bypasses this limit. It is not equivalent to a protocol budget.";

    #[must_use]
    pub const fn guarantee(self) -> &'static str {
        match self {
            Self::ProtocolBudget => "protocol_enforced",
            Self::DaemonLimit => "daemon_enforced",
        }
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct BudgetCreate {
    pub tenant: TenantId,
    pub agent_did: AgentDid,
    pub asset: Asset,
    pub limit: BudgetLimit,
    pub enforcement: BudgetEnforcement,
    pub expiry: TimestampSeconds,
}

impl BudgetCreate {
    /// Validates finite limit and expiry.
    ///
    /// # Errors
    /// Returns [`ContractError::Zero`] for a zero limit or expiry.
    pub fn validate(self) -> Result<Self, ContractError> {
        if self.limit.0 == 0 {
            return Err(ContractError::Zero("limit"));
        }
        if self.expiry.0 == 0 {
            return Err(ContractError::Zero("expiry"));
        }
        Ok(self)
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct BudgetFund {
    pub tenant: TenantId,
    pub agent_did: AgentDid,
    pub budget_id: BudgetId,
    pub amount: Amount,
    pub enforcement: BudgetEnforcement,
}

impl BudgetFund {
    /// Protocol funding cannot be represented for a daemon-only limit.
    ///
    /// # Errors
    /// Returns [`ContractError::DaemonLimitFunding`] for daemon-only limits.
    pub fn validate(self) -> Result<Self, ContractError> {
        if matches!(self.enforcement, BudgetEnforcement::DaemonLimit) {
            return Err(ContractError::DaemonLimitFunding);
        }
        Ok(self)
    }
}

/// Owner signature over a prepared canonical budget activity.
///
/// `signer_public_key` is only an equality assertion against the cached authorised owner key;
/// it never names the verifying authority.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct BudgetAuthorization {
    pub preparation_ref: PreparationRef,
    pub signature: SignatureBytes,
    pub signer_public_key: Option<[u8; 32]>,
}

/// A budget mutation and its conditionally required owner authorisation.
///
/// Protocol budgets execute signed core activities and require the carrier; daemon limits are
/// enforced by session authority and must not carry one.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct SignedBudgetMutation<T> {
    pub request: T,
    pub authorization: Option<BudgetAuthorization>,
}

impl<T> SignedBudgetMutation<T> {
    /// Checks the carrier against the enforcement actually in force for the budget.
    ///
    /// # Errors
    /// Returns [`ContractError::Empty`] for an unsigned protocol mutation and
    /// [`ContractError::Mismatch`] for a signed daemon-limit mutation.
    pub const fn require_for(&self, enforcement: BudgetEnforcement) -> Result<(), ContractError> {
        match (enforcement, &self.authorization) {
            (BudgetEnforcement::ProtocolBudget, None) => {
                Err(ContractError::Empty("budget_authorization"))
            }
            (BudgetEnforcement::DaemonLimit, Some(_)) => {
                Err(ContractError::Mismatch("budget_authorization"))
            }
            _ => Ok(()),
        }
    }
}

impl SignedBudgetMutation<BudgetCreate> {
    /// Validates the request and its carrier for the requested enforcement.
    ///
    /// # Errors
    /// Returns the request validation failure or the carrier refusal.
    pub fn validate(self) -> Result<Self, ContractError> {
        let request = self.request.validate()?;
        let mutation = Self {
            request,
            authorization: self.authorization,
        };
        mutation.require_for(mutation.request.enforcement)?;
        Ok(mutation)
    }
}

impl SignedBudgetMutation<BudgetFund> {
    /// Validates the request and its carrier; only protocol budgets can be funded.
    ///
    /// # Errors
    /// Returns the request validation failure or the carrier refusal.
    pub fn validate(self) -> Result<Self, ContractError> {
        let request = self.request.validate()?;
        let mutation = Self {
            request,
            authorization: self.authorization,
        };
        mutation.require_for(mutation.request.enforcement)?;
        Ok(mutation)
    }
}

/// A protocol budget record projected from proven core state; units are explicit.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ProtocolBudgetView {
    pub budget_id: [u8; 32],
    pub owner: [u8; 32],
    pub budget_account: [u8; 32],
    pub asset_id: [u8; 32],
    pub purpose_hash: [u8; 32],
    pub per_period_limit: Amount,
    pub configured_period_limit: Amount,
    pub carry_cap: Amount,
    pub spent_this_period: Amount,
    pub carried: Amount,
    pub period_length_ms: u64,
    pub period_start_ms: u64,
    pub expiry_ms: u64,
    pub revocation_counter: u64,
    pub rollover_policy: u8,
    pub closed: bool,
    pub revoked: bool,
    pub delegates: Vec<[u8; 32]>,
    pub source_account: Option<[u8; 32]>,
    pub achieved_verification_level: Level,
}

impl ProtocolBudgetView {
    /// Projects one decoded core budget record whose state was proven.
    ///
    /// # Errors
    /// Returns [`ContractError::OutOfRange`] when the attached evidence is below state proof.
    pub fn from_proven(
        record: &layerx_client::budget::ProtocolBudgetRecord,
        achieved_verification_level: Level,
    ) -> Result<Self, ContractError> {
        if achieved_verification_level < Level::StateProven {
            return Err(ContractError::OutOfRange("budget_verification_level"));
        }
        Ok(Self {
            budget_id: record.budget_id,
            owner: record.owner,
            budget_account: record.budget_account,
            asset_id: record.asset_id,
            purpose_hash: record.purpose_hash,
            per_period_limit: Amount(record.per_period_limit),
            configured_period_limit: Amount(record.configured_period_limit),
            carry_cap: Amount(record.carry_cap),
            spent_this_period: Amount(record.spent_this_period),
            carried: Amount(record.carried),
            period_length_ms: record.period_length,
            period_start_ms: record.period_start,
            expiry_ms: record.expiry,
            revocation_counter: record.revocation_sequence,
            rollover_policy: record.rollover_policy,
            closed: record.closed,
            revoked: record.revoked,
            delegates: record.delegates.clone(),
            source_account: record.source_account,
            achieved_verification_level,
        })
    }
}

/// A durable daemon-enforced limit. It never carries chain verification.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct DaemonLimitView {
    pub budget_id: [u8; 32],
    pub asset: [u8; 32],
    pub ceiling: BudgetLimit,
    pub consumed: Amount,
    pub expiry_ms: u64,
    pub revoked: bool,
}

impl DaemonLimitView {
    /// The bypass notice every daemon-limit record carries.
    #[must_use]
    pub const fn notice(&self) -> &'static str {
        BudgetEnforcement::DAEMON_LIMIT_NOTICE
    }
}

/// One budget-like record tagged by who actually enforces it.
#[derive(Clone, Debug, Eq, PartialEq)]
pub enum BudgetRecord {
    Protocol(ProtocolBudgetView),
    Daemon(DaemonLimitView),
}

impl BudgetRecord {
    #[must_use]
    pub const fn enforcement(&self) -> BudgetEnforcement {
        match self {
            Self::Protocol(_) => BudgetEnforcement::ProtocolBudget,
            Self::Daemon(_) => BudgetEnforcement::DaemonLimit,
        }
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct BudgetRecords(pub Vec<BudgetRecord>);

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct BudgetList {
    pub tenant: TenantId,
    pub agent_did: AgentDid,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct BudgetTarget {
    pub tenant: TenantId,
    pub agent_did: AgentDid,
    pub budget_id: BudgetId,
}

/// Exact authority attached to every successful or refused response.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct AuthorityDescription {
    pub tenant: TenantId,
    pub agent_did: AgentDid,
    pub authority_ref: AuthorityRef,
    pub protocol_authority: Vec<u8>,
}

impl AuthorityDescription {
    /// Creates an authority description with non-empty canonical protocol bytes.
    ///
    /// # Errors
    /// Returns [`ContractError::Empty`] when protocol authority bytes are absent.
    pub fn new(
        tenant: TenantId,
        agent_did: AgentDid,
        authority_ref: AuthorityRef,
        protocol_authority: Vec<u8>,
    ) -> Result<Self, ContractError> {
        if protocol_authority.is_empty() {
            return Err(ContractError::Empty("protocol_authority"));
        }
        Ok(Self {
            tenant,
            agent_did,
            authority_ref,
            protocol_authority,
        })
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct AuthorityResponse<T> {
    pub authority: AuthorityDescription,
    pub value: T,
}

/// Hypothetical local policy evaluation; it creates no preparation, signature or submission.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct PolicyDryRunRequest {
    pub tenant: TenantId,
    pub agent_did: AgentDid,
    pub session_id: SessionId,
    pub capability_id: CapabilityId,
    pub activity_type: ActivityType,
    pub counterparty: [u8; 32],
    pub asset: [u8; 32],
    pub amount: Amount,
    pub purpose: String,
    /// Caller-stated evaluation sequence; it is not a freshness claim.
    pub core_sequence: Sequence,
}

impl PolicyDryRunRequest {
    /// Validates the session and capability identifiers.
    ///
    /// # Errors
    /// Returns [`ContractError::Malformed`] for a non-canonical identifier.
    pub fn validate(self) -> Result<Self, ContractError> {
        self.session_id.to_bytes()?;
        self.capability_id.to_bytes()?;
        Ok(self)
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum PolicyOutcome {
    Allow,
    Deny,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum PolicyDecisionReason {
    PermittedByRule,
    ExplicitDeny,
    ApprovalRequired,
    NoPermittingRule,
    InvalidContext,
    EvaluationFailure,
}

/// Local restriction result. It is not protocol authorisation and carries no freshness.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct PolicyDryRunResult {
    pub outcome: PolicyOutcome,
    pub policy_version: PolicyVersion,
    pub matched_rules: Vec<String>,
    pub deciding_rule: Option<String>,
    pub reason: PolicyDecisionReason,
    pub authority_statement: String,
}

impl PolicyDryRunResult {
    /// Refuses a result without its local-restriction statement or with empty rule names.
    ///
    /// # Errors
    /// Returns [`ContractError::Empty`] for an absent statement or an empty rule identifier.
    pub fn validate(self) -> Result<Self, ContractError> {
        if self.authority_statement.is_empty() {
            return Err(ContractError::Empty("authority_statement"));
        }
        if self.matched_rules.iter().any(String::is_empty)
            || self.deciding_rule.as_deref() == Some("")
        {
            return Err(ContractError::Empty("rule_id"));
        }
        Ok(self)
    }
}
