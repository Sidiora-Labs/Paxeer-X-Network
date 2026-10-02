//! `budget.create`, `budget.fund`, `budget.revoke`, `budget.list` and `budget.state` over the
//! version 1 Agent operation envelope.

use layerx_agent_api::budget::{
    AuthorityDescription, AuthorityResponse, BudgetAuthorization, BudgetCreate, BudgetEnforcement,
    BudgetFund, BudgetList, BudgetRecord, BudgetTarget, DaemonLimitView, ProtocolBudgetView,
    SignedBudgetMutation,
};
use layerx_agent_api::error::RequestId;
use layerx_agent_api::idempotency::Key;
use layerx_agent_api::identity::{AgentDid, AuthorityRef, TenantId};
use layerx_agent_api::verify::{Level, VerificationStatus};
use layerx_agent_api::{Amount, BudgetLimit};
use serde_json::{json, Map, Value};

use crate::agent_envelope::{AgentEnvelopeTransport, EnvelopeCredential, EnvelopeError};
use crate::rpc::encode_hex;
use crate::rpc_export::level_from_wire;
use crate::rpc_history::lower_hex;
use crate::rpc_projection::{bytes32, decimal_u128};
use crate::rpc_subscription::{decimal, object, text, violation};
use crate::Operation;

/// One budget record with the snapshot head it was read from and, for a protocol budget
/// mutation, the core activity whose verified receipt produced it.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct BudgetRecordEntry {
    pub head: u64,
    pub activity_id: Option<[u8; 32]>,
    pub record: BudgetRecord,
}

/// The state-proven state of one budget: its record entry, the budget account balance of the
/// budget asset and the head the state was proven at, equal to the record head. `activity_id`
/// equals the record activity_id; a state read proves current state rather than one producing
/// activity, so it is `None`.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct BudgetState {
    pub record: BudgetRecordEntry,
    pub balance: Amount,
    pub proven_head: u64,
    pub activity_id: Option<[u8; 32]>,
}

const PROTOCOL_FIELDS: [&str; 23] = [
    "enforcement",
    "budget_id",
    "head",
    "activity_id",
    "owner",
    "budget_account",
    "asset_id",
    "purpose_hash",
    "per_period_limit",
    "configured_period_limit",
    "carry_cap",
    "spent_this_period",
    "carried",
    "period_length_ms",
    "period_start_ms",
    "expiry_ms",
    "revocation_counter",
    "rollover_policy",
    "closed",
    "revoked",
    "delegates",
    "source_account",
    "achieved_verification_level",
];

const DAEMON_FIELDS: [&str; 10] = [
    "enforcement",
    "budget_id",
    "head",
    "activity_id",
    "asset",
    "ceiling",
    "consumed",
    "expiry_ms",
    "revoked",
    "notice",
];

const fn enforcement_wire(enforcement: BudgetEnforcement) -> &'static str {
    match enforcement {
        BudgetEnforcement::ProtocolBudget => "ProtocolBudget",
        BudgetEnforcement::DaemonLimit => "DaemonLimit",
    }
}

fn authorization_value(authorization: Option<&BudgetAuthorization>) -> Value {
    authorization.map_or(Value::Null, |authorization| {
        json!({
            "preparation_ref": authorization.preparation_ref.as_str(),
            "signature": encode_hex(authorization.signature.as_bytes()),
            "signer_public_key": authorization
                .signer_public_key
                .map_or(Value::Null, |key| Value::String(encode_hex(&key))),
        })
    })
}

fn flag(value: &Value, operation: Operation) -> Result<bool, EnvelopeError> {
    value.as_bool().ok_or_else(|| violation(operation))
}

fn optional_bytes32(
    value: &Value,
    operation: Operation,
) -> Result<Option<[u8; 32]>, EnvelopeError> {
    match value {
        Value::Null => Ok(None),
        value => bytes32(value, operation).map(Some),
    }
}

fn decode_authority(
    value: &Value,
    operation: Operation,
    tenant: &TenantId,
    agent_did: &AgentDid,
) -> Result<AuthorityDescription, EnvelopeError> {
    let authority = object(
        value,
        &["tenant", "agent_did", "authority_ref", "protocol_authority"],
        operation,
    )?;
    let decoded = AuthorityDescription::new(
        TenantId::new(text(&authority["tenant"], operation)?).map_err(|_| violation(operation))?,
        AgentDid::new(text(&authority["agent_did"], operation)?)
            .map_err(|_| violation(operation))?,
        AuthorityRef::new(text(&authority["authority_ref"], operation)?)
            .map_err(|_| violation(operation))?,
        authority["protocol_authority"]
            .as_str()
            .and_then(lower_hex)
            .ok_or_else(|| violation(operation))?,
    )
    .map_err(|_| violation(operation))?;
    if decoded.tenant != *tenant || decoded.agent_did != *agent_did {
        return Err(violation(operation));
    }
    Ok(decoded)
}

fn decode_protocol(
    record: &Map<String, Value>,
    budget_id: [u8; 32],
    achieved_verification_level: Level,
    operation: Operation,
) -> Result<ProtocolBudgetView, EnvelopeError> {
    if achieved_verification_level < Level::StateProven {
        return Err(violation(operation));
    }
    Ok(ProtocolBudgetView {
        budget_id,
        owner: bytes32(&record["owner"], operation)?,
        budget_account: bytes32(&record["budget_account"], operation)?,
        asset_id: bytes32(&record["asset_id"], operation)?,
        purpose_hash: bytes32(&record["purpose_hash"], operation)?,
        per_period_limit: Amount(decimal_u128(&record["per_period_limit"], operation)?),
        configured_period_limit: Amount(decimal_u128(
            &record["configured_period_limit"],
            operation,
        )?),
        carry_cap: Amount(decimal_u128(&record["carry_cap"], operation)?),
        spent_this_period: Amount(decimal_u128(&record["spent_this_period"], operation)?),
        carried: Amount(decimal_u128(&record["carried"], operation)?),
        period_length_ms: decimal(&record["period_length_ms"], operation)?,
        period_start_ms: decimal(&record["period_start_ms"], operation)?,
        expiry_ms: decimal(&record["expiry_ms"], operation)?,
        revocation_counter: decimal(&record["revocation_counter"], operation)?,
        rollover_policy: u8::try_from(decimal(&record["rollover_policy"], operation)?)
            .map_err(|_| violation(operation))?,
        closed: flag(&record["closed"], operation)?,
        revoked: flag(&record["revoked"], operation)?,
        delegates: record["delegates"]
            .as_array()
            .ok_or_else(|| violation(operation))?
            .iter()
            .map(|delegate| bytes32(delegate, operation))
            .collect::<Result<Vec<_>, _>>()?,
        source_account: optional_bytes32(&record["source_account"], operation)?,
        achieved_verification_level,
    })
}

fn decode_daemon(
    record: &Map<String, Value>,
    budget_id: [u8; 32],
    operation: Operation,
) -> Result<DaemonLimitView, EnvelopeError> {
    let view = DaemonLimitView {
        budget_id,
        asset: bytes32(&record["asset"], operation)?,
        ceiling: BudgetLimit(decimal_u128(&record["ceiling"], operation)?),
        consumed: Amount(decimal_u128(&record["consumed"], operation)?),
        expiry_ms: decimal(&record["expiry_ms"], operation)?,
        revoked: flag(&record["revoked"], operation)?,
    };
    if record["notice"].as_str() != Some(view.notice()) {
        return Err(violation(operation));
    }
    Ok(view)
}

/// Decodes one record. `mutation` is true for create, fund and revoke responses, where a
/// protocol budget record must name its producing activity; list and daemon records never do.
fn decode_entry(
    value: &Value,
    operation: Operation,
    mutation: bool,
) -> Result<BudgetRecordEntry, EnvelopeError> {
    let enforcement = match value.get("enforcement").and_then(Value::as_str) {
        Some("ProtocolBudget") => BudgetEnforcement::ProtocolBudget,
        Some("DaemonLimit") => BudgetEnforcement::DaemonLimit,
        _ => return Err(violation(operation)),
    };
    let fields: &[&str] = match enforcement {
        BudgetEnforcement::ProtocolBudget => &PROTOCOL_FIELDS,
        BudgetEnforcement::DaemonLimit => &DAEMON_FIELDS,
    };
    let record = object(value, fields, operation)?;
    let budget_id = bytes32(&record["budget_id"], operation)?;
    let head = decimal(&record["head"], operation)?;
    let activity_id = optional_bytes32(&record["activity_id"], operation)?;
    let expects_activity = mutation && enforcement == BudgetEnforcement::ProtocolBudget;
    if activity_id.is_some() != expects_activity {
        return Err(violation(operation));
    }
    let record = match enforcement {
        BudgetEnforcement::ProtocolBudget => {
            let level = level_from_wire(&record["achieved_verification_level"], operation)?;
            BudgetRecord::Protocol(decode_protocol(record, budget_id, level, operation)?)
        }
        BudgetEnforcement::DaemonLimit => {
            BudgetRecord::Daemon(decode_daemon(record, budget_id, operation)?)
        }
    };
    Ok(BudgetRecordEntry {
        head,
        activity_id,
        record,
    })
}

const fn record_budget_id(record: &BudgetRecord) -> &[u8; 32] {
    match record {
        BudgetRecord::Protocol(view) => &view.budget_id,
        BudgetRecord::Daemon(view) => &view.budget_id,
    }
}

const fn record_revoked(record: &BudgetRecord) -> bool {
    match record {
        BudgetRecord::Protocol(view) => view.revoked,
        BudgetRecord::Daemon(view) => view.revoked,
    }
}

impl AgentEnvelopeTransport {
    #[allow(clippy::too_many_arguments)]
    fn budget_mutation(
        &self,
        operation: Operation,
        request_id: RequestId,
        key: Key,
        credential: &EnvelopeCredential,
        request: &Value,
        coordinates: (&TenantId, &AgentDid),
    ) -> Result<AuthorityResponse<BudgetRecordEntry>, EnvelopeError> {
        let success =
            self.send_operation(operation, request_id, request, Some(credential), Some(key))?;
        let response = object(&success.value, &["authority", "value"], operation)?;
        let (tenant, agent_did) = coordinates;
        Ok(AuthorityResponse {
            authority: decode_authority(&response["authority"], operation, tenant, agent_did)?,
            value: decode_entry(&response["value"], operation, true)?,
        })
    }

    /// Creates one budget. A protocol budget carries the owner's signed authorisation of the
    /// prepared canonical activity; a daemon limit carries none.
    ///
    /// # Errors
    ///
    /// Returns `InvalidRequest` for a zero limit or expiry or a carrier that does not match the
    /// enforcement before sending, the established error envelope, or `Unknown` when the outcome
    /// cannot be established; reconcile with `budget.list`, never resend automatically.
    pub fn budget_create(
        &self,
        request_id: RequestId,
        key: Key,
        credential: &EnvelopeCredential,
        mutation: &SignedBudgetMutation<BudgetCreate>,
    ) -> Result<AuthorityResponse<BudgetRecordEntry>, EnvelopeError> {
        let operation = Operation::BudgetCreate;
        let mutation = mutation
            .clone()
            .validate()
            .map_err(|_| EnvelopeError::InvalidRequest)?;
        let request = &mutation.request;
        let response = self.budget_mutation(
            operation,
            request_id,
            key,
            credential,
            &json!({
                "tenant": request.tenant.as_str(),
                "agent_did": request.agent_did.as_str(),
                "asset": request.asset.as_str(),
                "limit": request.limit.0.to_string(),
                "enforcement": enforcement_wire(request.enforcement),
                "expiry": request.expiry.0.to_string(),
                "authorization": authorization_value(mutation.authorization.as_ref()),
            }),
            (&request.tenant, &request.agent_did),
        )?;
        let record = &response.value.record;
        let consistent = record.enforcement() == request.enforcement
            && match record {
                BudgetRecord::Daemon(view) => view.ceiling == request.limit,
                BudgetRecord::Protocol(_) => true,
            };
        if !consistent {
            return Err(violation(operation));
        }
        Ok(response)
    }

    /// Funds one protocol budget with the owner's signed authorisation. Daemon limits cannot be
    /// funded.
    ///
    /// # Errors
    ///
    /// Returns `InvalidRequest` for a daemon-limit or unsigned fund before sending, the
    /// established error envelope, or `Unknown` when the outcome cannot be established.
    pub fn budget_fund(
        &self,
        request_id: RequestId,
        key: Key,
        credential: &EnvelopeCredential,
        mutation: &SignedBudgetMutation<BudgetFund>,
    ) -> Result<AuthorityResponse<BudgetRecordEntry>, EnvelopeError> {
        let operation = Operation::BudgetFund;
        let mutation = mutation
            .clone()
            .validate()
            .map_err(|_| EnvelopeError::InvalidRequest)?;
        let request = &mutation.request;
        let response = self.budget_mutation(
            operation,
            request_id,
            key,
            credential,
            &json!({
                "tenant": request.tenant.as_str(),
                "agent_did": request.agent_did.as_str(),
                "budget_id": request.budget_id.as_str(),
                "amount": request.amount.0.to_string(),
                "enforcement": enforcement_wire(request.enforcement),
                "authorization": authorization_value(mutation.authorization.as_ref()),
            }),
            (&request.tenant, &request.agent_did),
        )?;
        let record = &response.value.record;
        if record.enforcement() != request.enforcement
            || encode_hex(record_budget_id(record)) != request.budget_id.as_str()
        {
            return Err(violation(operation));
        }
        Ok(response)
    }

    /// Revokes one budget. The enforcement in force is the daemon's authenticated lookup; the
    /// answer is refused unless the carrier sent matches it and the record is revoked.
    ///
    /// # Errors
    ///
    /// Returns the established error envelope, or `Unknown` when the outcome cannot be
    /// established.
    pub fn budget_revoke(
        &self,
        request_id: RequestId,
        key: Key,
        credential: &EnvelopeCredential,
        mutation: &SignedBudgetMutation<BudgetTarget>,
    ) -> Result<AuthorityResponse<BudgetRecordEntry>, EnvelopeError> {
        let operation = Operation::BudgetRevoke;
        let request = &mutation.request;
        let response = self.budget_mutation(
            operation,
            request_id,
            key,
            credential,
            &json!({
                "tenant": request.tenant.as_str(),
                "agent_did": request.agent_did.as_str(),
                "budget_id": request.budget_id.as_str(),
                "authorization": authorization_value(mutation.authorization.as_ref()),
            }),
            (&request.tenant, &request.agent_did),
        )?;
        let record = &response.value.record;
        if mutation.require_for(record.enforcement()).is_err()
            || encode_hex(record_budget_id(record)) != request.budget_id.as_str()
            || !record_revoked(record)
        {
            return Err(violation(operation));
        }
        Ok(response)
    }

    /// Lists every budget-like record of one agent, each tagged by who enforces it.
    ///
    /// # Errors
    ///
    /// Returns the established error envelope, `Transport`, or `Decode` for any answer that is
    /// not exactly an authority-wrapped record list in ascending budget order.
    pub fn budget_list(
        &self,
        request_id: RequestId,
        credential: &EnvelopeCredential,
        request: &BudgetList,
    ) -> Result<AuthorityResponse<Vec<BudgetRecordEntry>>, EnvelopeError> {
        let operation = Operation::BudgetList;
        let success = self.send_operation(
            operation,
            request_id,
            &json!({
                "tenant": request.tenant.as_str(),
                "agent_did": request.agent_did.as_str(),
            }),
            Some(credential),
            None,
        )?;
        let response = object(&success.value, &["authority", "value"], operation)?;
        let authority = decode_authority(
            &response["authority"],
            operation,
            &request.tenant,
            &request.agent_did,
        )?;
        let budgets = object(&response["value"], &["budgets"], operation)?["budgets"]
            .as_array()
            .ok_or_else(|| violation(operation))?
            .iter()
            .map(|entry| decode_entry(entry, operation, false))
            .collect::<Result<Vec<_>, _>>()?;
        if budgets
            .windows(2)
            .any(|pair| record_budget_id(&pair[0].record) >= record_budget_id(&pair[1].record))
        {
            return Err(violation(operation));
        }
        Ok(AuthorityResponse {
            authority,
            value: budgets,
        })
    }

    /// Reads the state-proven state of one budget of one agent.
    ///
    /// # Errors
    ///
    /// Returns the established error envelope (`budget.not_found` for an unknown budget),
    /// `Transport`, or `Decode` for any answer that is not exactly the authority-wrapped state of
    /// the requested budget.
    pub fn budget_state(
        &self,
        request_id: RequestId,
        credential: &EnvelopeCredential,
        request: &BudgetTarget,
    ) -> Result<AuthorityResponse<BudgetState>, EnvelopeError> {
        let operation = Operation::BudgetState;
        let success = self.send_operation(
            operation,
            request_id,
            &json!({
                "tenant": request.tenant.as_str(),
                "agent_did": request.agent_did.as_str(),
                "budget_id": request.budget_id.as_str(),
            }),
            Some(credential),
            None,
        )?;
        let response = object(&success.value, &["authority", "value"], operation)?;
        let authority = decode_authority(
            &response["authority"],
            operation,
            &request.tenant,
            &request.agent_did,
        )?;
        let state = object(
            &response["value"],
            &["record", "balance", "proven_head", "activity_id"],
            operation,
        )?;
        let record = decode_entry(&state["record"], operation, false)?;
        let proven_head = decimal(&state["proven_head"], operation)?;
        let activity_id = optional_bytes32(&state["activity_id"], operation)?;
        let status_level = match &record.record {
            BudgetRecord::Protocol(view) => view.achieved_verification_level,
            BudgetRecord::Daemon(_) => Level::Unverified,
        };
        if success.verification_status != VerificationStatus::Achieved(status_level)
            || encode_hex(record_budget_id(&record.record)) != request.budget_id.as_str()
            || proven_head != record.head
            || activity_id != record.activity_id
        {
            return Err(violation(operation));
        }
        Ok(AuthorityResponse {
            authority,
            value: BudgetState {
                record,
                balance: Amount(decimal_u128(&state["balance"], operation)?),
                proven_head,
                activity_id,
            },
        })
    }
}
