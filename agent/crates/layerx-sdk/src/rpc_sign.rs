//! `sign` over the version 1 Agent operation envelope.

use layerx_agent_api::error::{Key, Level, RequestId};
use serde_json::{json, Value};

use crate::agent_envelope::{AgentEnvelopeTransport, EnvelopeCredential, EnvelopeError};
use crate::rpc_history::lower_hex;
use crate::rpc_subscription::{decimal, object, text, violation};
use crate::Operation;

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Evidence {
    pub kind: String,
    pub digest: [u8; 32],
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Transition {
    pub from: String,
    pub to: String,
    pub cause: String,
    pub at: u64,
}

/// Observation of a preparation retained in the `Signed` state.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct SignedObservation {
    pub activity_id: [u8; 32],
    pub submission_ref: String,
    pub verification_level: Level,
    pub evidence: Vec<Evidence>,
    pub transitions: Vec<Transition>,
}

fn level_name(text: &str) -> Option<Level> {
    Some(match text {
        "Unverified" => Level::Unverified,
        "SequencerSigned" => Level::SequencerSigned,
        "BatchIncluded" => Level::BatchIncluded,
        "StateProven" => Level::StateProven,
        "CheckpointFinalised" => Level::CheckpointFinalised,
        "SettlementAnchored" => Level::SettlementAnchored,
        _ => return None,
    })
}

fn digest(value: &Value, operation: Operation) -> Result<[u8; 32], EnvelopeError> {
    value
        .as_str()
        .and_then(lower_hex)
        .and_then(|bytes| <[u8; 32]>::try_from(bytes).ok())
        .ok_or_else(|| violation(operation))
}

fn decode_signed(value: &Value, operation: Operation) -> Result<SignedObservation, EnvelopeError> {
    let observation = object(value, &["activity_id", "submission", "receipt"], operation)?;
    let activity_id = digest(&observation["activity_id"], operation)?;
    if activity_id == [0; 32] || !observation["receipt"].is_null() {
        return Err(violation(operation));
    }
    let submission = object(
        &observation["submission"],
        &[
            "submission_ref",
            "state",
            "verification_level",
            "evidence",
            "transitions",
        ],
        operation,
    )?;
    if submission["state"] != "Signed" {
        return Err(violation(operation));
    }
    let evidence = submission["evidence"]
        .as_array()
        .ok_or_else(|| violation(operation))?
        .iter()
        .map(|item| {
            let item = object(item, &["kind", "digest"], operation)?;
            Ok(Evidence {
                kind: text(&item["kind"], operation)?,
                digest: digest(&item["digest"], operation)?,
            })
        })
        .collect::<Result<_, _>>()?;
    let transitions = submission["transitions"]
        .as_array()
        .ok_or_else(|| violation(operation))?
        .iter()
        .map(|item| {
            let item = object(item, &["from", "to", "cause", "at"], operation)?;
            Ok(Transition {
                from: text(&item["from"], operation)?,
                to: text(&item["to"], operation)?,
                cause: text(&item["cause"], operation)?,
                at: decimal(&item["at"], operation)?,
            })
        })
        .collect::<Result<_, _>>()?;
    Ok(SignedObservation {
        activity_id,
        submission_ref: text(&submission["submission_ref"], operation)?,
        verification_level: submission["verification_level"]
            .as_str()
            .and_then(level_name)
            .ok_or_else(|| violation(operation))?,
        evidence,
        transitions,
    })
}

impl AgentEnvelopeTransport {
    /// Attaches an externally produced 64-byte signature to a retained preparation.
    ///
    /// # Errors
    ///
    /// Returns the established error envelope, or `Unknown` when the outcome cannot be
    /// established or the response is not a `Signed` observation; reconcile with `track`.
    pub fn sign(
        &self,
        request_id: RequestId,
        key: Key,
        credential: &EnvelopeCredential,
        preparation_ref: &str,
        signature: &[u8; 64],
    ) -> Result<SignedObservation, EnvelopeError> {
        let operation = Operation::Sign;
        let success = self.send_operation(
            operation,
            request_id,
            &json!({
                "preparation_ref": preparation_ref,
                "signature": crate::rpc::encode_hex(signature),
            }),
            Some(credential),
            Some(key),
        )?;
        decode_signed(&success.value, operation)
    }
}
