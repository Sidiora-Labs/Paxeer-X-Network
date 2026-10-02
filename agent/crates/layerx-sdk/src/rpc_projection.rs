//! `project` fee projection over the version 1 Agent operation envelope.

use layerx_agent_api::error::RequestId;
use layerx_agent_api::identity::strict_hex32;
use layerx_agent_api::prepare::CanonicalBytes;
use layerx_agent_api::read::{
    BatchRef, CheckpointRef, FeeProjection, FeeProjectionRequest, Freshness, ProjectionResult,
    RelativeTo,
};
use layerx_agent_api::verify::{Level, VerificationStatus};
use layerx_agent_api::{Amount, Sequence};
use serde_json::{json, Value};

use crate::agent_envelope::{AgentEnvelopeTransport, EnvelopeCredential, EnvelopeError};
use crate::rpc_history::lower_hex;
use crate::rpc_subscription::{decimal, object, text, violation};
use crate::Operation;

/// Decodes the schema wire form of [`Freshness`]: decimal sequences, non-empty references and
/// a `relative_to` object with exactly one of `batch` or `checkpoint`.
pub(crate) fn decode_freshness(
    value: &Value,
    operation: Operation,
) -> Result<Freshness, EnvelopeError> {
    let freshness = object(
        value,
        &[
            "chain_head",
            "latest_sealed_batch",
            "latest_finalised_checkpoint",
            "value_sequence",
            "relative_to",
        ],
        operation,
    )?;
    let relative = freshness["relative_to"]
        .as_object()
        .filter(|relative| relative.len() == 1)
        .ok_or_else(|| violation(operation))?;
    let relative_to = match (relative.get("batch"), relative.get("checkpoint")) {
        (Some(batch), None) => RelativeTo::Batch(
            BatchRef::new(text(batch, operation)?).map_err(|_| violation(operation))?,
        ),
        (None, Some(checkpoint)) => RelativeTo::Checkpoint(
            CheckpointRef::new(text(checkpoint, operation)?).map_err(|_| violation(operation))?,
        ),
        _ => return Err(violation(operation)),
    };
    Ok(Freshness {
        chain_head: Sequence(decimal(&freshness["chain_head"], operation)?),
        latest_sealed_batch: BatchRef::new(text(&freshness["latest_sealed_batch"], operation)?)
            .map_err(|_| violation(operation))?,
        latest_finalised_checkpoint: CheckpointRef::new(text(
            &freshness["latest_finalised_checkpoint"],
            operation,
        )?)
        .map_err(|_| violation(operation))?,
        value_sequence: Sequence(decimal(&freshness["value_sequence"], operation)?),
        relative_to,
    })
}

/// Parses one canonical decimal `u128`: no sign, no leading zero except `0`.
pub(crate) fn decimal_u128(value: &Value, operation: Operation) -> Result<u128, EnvelopeError> {
    value
        .as_str()
        .and_then(|text| {
            let parsed: u128 = text.parse().ok()?;
            (parsed.to_string() == text).then_some(parsed)
        })
        .ok_or_else(|| violation(operation))
}

/// Parses exactly 64 lowercase hex characters into 32 bytes.
pub(crate) fn bytes32(value: &Value, operation: Operation) -> Result<[u8; 32], EnvelopeError> {
    value
        .as_str()
        .and_then(|text| strict_hex32(text, "bytes32").ok())
        .ok_or_else(|| violation(operation))
}

/// Requires the success envelope to claim no verification level.
pub(crate) fn require_unverified_status(
    status: &VerificationStatus,
    operation: Operation,
) -> Result<(), EnvelopeError> {
    if *status == VerificationStatus::Achieved(Level::Unverified) {
        Ok(())
    } else {
        Err(violation(operation))
    }
}

fn request_value(request: &FeeProjectionRequest) -> Value {
    json!({
        "protocol_activity_type": request.protocol_activity_type.to_string(),
        "canonical_bytes": request.canonical_bytes.to_string(),
        "execution_units": request.execution_units.to_string(),
        "storage_units": request.storage_units.to_string(),
    })
}

fn decode_request(
    value: &Value,
    operation: Operation,
) -> Result<FeeProjectionRequest, EnvelopeError> {
    let request = object(
        value,
        &[
            "protocol_activity_type",
            "canonical_bytes",
            "execution_units",
            "storage_units",
        ],
        operation,
    )?;
    FeeProjectionRequest {
        protocol_activity_type: u32::try_from(decimal(
            &request["protocol_activity_type"],
            operation,
        )?)
        .map_err(|_| violation(operation))?,
        canonical_bytes: decimal(&request["canonical_bytes"], operation)?,
        execution_units: decimal(&request["execution_units"], operation)?,
        storage_units: decimal(&request["storage_units"], operation)?,
    }
    .validate()
    .map_err(|_| violation(operation))
}

fn decode_projection(value: &Value, operation: Operation) -> Result<FeeProjection, EnvelopeError> {
    let projection = object(
        value,
        &[
            "request",
            "parameter_version",
            "fee",
            "canonical_schedule",
            "snapshot_sequence",
            "snapshot_state_root",
        ],
        operation,
    )?;
    let parameter_version = u32::try_from(decimal(&projection["parameter_version"], operation)?)
        .ok()
        .filter(|version| *version != 0)
        .ok_or_else(|| violation(operation))?;
    let snapshot_state_root = bytes32(&projection["snapshot_state_root"], operation)?;
    if snapshot_state_root == [0; 32] {
        return Err(violation(operation));
    }
    Ok(FeeProjection {
        request: decode_request(&projection["request"], operation)?,
        parameter_version,
        fee: Amount(decimal_u128(&projection["fee"], operation)?),
        canonical_schedule: projection["canonical_schedule"]
            .as_str()
            .and_then(lower_hex)
            .and_then(|bytes| CanonicalBytes::new(bytes).ok())
            .ok_or_else(|| violation(operation))?,
        snapshot_sequence: Sequence(decimal(&projection["snapshot_sequence"], operation)?),
        snapshot_state_root,
    })
}

impl AgentEnvelopeTransport {
    /// Projects the fee of one hypothetical activity. The result is an estimate: it is never
    /// core-produced, never a [`layerx_agent_api::read::VerifiedRead`] and never an executed
    /// outcome.
    ///
    /// # Errors
    ///
    /// Returns `InvalidRequest` for a meter outside the native bound before sending, the
    /// established error envelope, `Transport`, or `Decode` when the answer is not exactly a
    /// projection of the sent request bound to its snapshot freshness.
    pub fn project_fee(
        &self,
        request_id: RequestId,
        credential: &EnvelopeCredential,
        request: &FeeProjectionRequest,
    ) -> Result<ProjectionResult<FeeProjection>, EnvelopeError> {
        let operation = Operation::Project;
        let request = request
            .validate()
            .map_err(|_| EnvelopeError::InvalidRequest)?;
        let success = self.send_operation(
            operation,
            request_id,
            &request_value(&request),
            Some(credential),
            None,
        )?;
        require_unverified_status(&success.verification_status, operation)?;
        let result = object(
            &success.value,
            &["projected", "rationale", "observed_freshness"],
            operation,
        )?;
        let projection = decode_projection(&result["projected"], operation)?;
        if projection.request != request {
            return Err(violation(operation));
        }
        projection
            .into_projection(
                text(&result["rationale"], operation)?,
                decode_freshness(&result["observed_freshness"], operation)?,
            )
            .map_err(|_| violation(operation))
    }
}
