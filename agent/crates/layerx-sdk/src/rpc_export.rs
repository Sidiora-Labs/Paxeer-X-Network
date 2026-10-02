//! `export.offline` over the version 1 Agent operation envelope, and the offline decode and
//! complete verification of its answer.

use layerx_agent_api::error::RequestId;
use layerx_agent_api::export::{
    check_export_response, validate_export_request, FactRef, OfflineExport,
};
use layerx_agent_api::prepare::CanonicalBytes;
use layerx_agent_api::read::{ReadRequest, VerifiedRead};
use layerx_agent_api::verify::{Level, VerificationStatus};
use layerx_proof::export::{
    verify_complete, CompleteExportError, CompleteVerificationReport, IndependentOfflineTrust,
};
use layerx_proof::export_codec::{CompleteOfflineArtifact, ExportCodecError};
use layerx_types::verify::VerificationLevel;
use serde_json::{json, Value};

use crate::agent_envelope::{AgentEnvelopeTransport, EnvelopeCredential, EnvelopeError};
use crate::rpc_history::lower_hex;
use crate::rpc_projection::decode_freshness;
use crate::rpc_subscription::{object, text, violation};
use crate::Operation;

/// Schema wire spelling of one verification level.
pub(crate) const fn level_wire(level: Level) -> &'static str {
    match level {
        Level::Unverified => "Unverified",
        Level::SequencerSigned => "SequencerSigned",
        Level::BatchIncluded => "BatchIncluded",
        Level::StateProven => "StateProven",
        Level::CheckpointFinalised => "CheckpointFinalised",
        Level::SettlementAnchored => "SettlementAnchored",
    }
}

/// Parses the schema wire spelling of one verification level.
pub(crate) fn level_from_wire(value: &Value, operation: Operation) -> Result<Level, EnvelopeError> {
    match value.as_str() {
        Some("Unverified") => Ok(Level::Unverified),
        Some("SequencerSigned") => Ok(Level::SequencerSigned),
        Some("BatchIncluded") => Ok(Level::BatchIncluded),
        Some("StateProven") => Ok(Level::StateProven),
        Some("CheckpointFinalised") => Ok(Level::CheckpointFinalised),
        Some("SettlementAnchored") => Ok(Level::SettlementAnchored),
        _ => Err(violation(operation)),
    }
}

const fn verification_level(level: Level) -> VerificationLevel {
    match level {
        Level::Unverified => VerificationLevel::UNVERIFIED,
        Level::SequencerSigned => VerificationLevel::SEQUENCER_SIGNED,
        Level::BatchIncluded => VerificationLevel::BATCH_INCLUDED,
        Level::StateProven => VerificationLevel::STATE_PROVEN,
        Level::CheckpointFinalised => VerificationLevel::CHECKPOINT_FINALISED,
        Level::SettlementAnchored => VerificationLevel::SETTLEMENT_ANCHORED,
    }
}

fn records(value: &Value, operation: Operation) -> Result<Vec<CanonicalBytes>, EnvelopeError> {
    value
        .as_array()
        .ok_or_else(|| violation(operation))?
        .iter()
        .map(|record| {
            record
                .as_str()
                .and_then(lower_hex)
                .and_then(|bytes| CanonicalBytes::new(bytes).ok())
                .ok_or_else(|| violation(operation))
        })
        .collect()
}

fn decode_export(value: &Value, operation: Operation) -> Result<OfflineExport, EnvelopeError> {
    let export = object(
        value,
        &["facts", "receipts", "proofs", "certificates", "headers"],
        operation,
    )?;
    Ok(OfflineExport {
        facts: export["facts"]
            .as_array()
            .ok_or_else(|| violation(operation))?
            .iter()
            .map(|fact| FactRef::new(text(fact, operation)?).map_err(|_| violation(operation)))
            .collect::<Result<Vec<_>, _>>()?,
        receipts: records(&export["receipts"], operation)?,
        proofs: records(&export["proofs"], operation)?,
        certificates: records(&export["certificates"], operation)?,
        headers: records(&export["headers"], operation)?,
    })
}

/// Decodes every bucket of a received export into the complete offline artifact. The trust
/// context is not consulted by decoding; it is taken so the artifact is only ever produced on
/// the path that then verifies it with [`verify_complete_offline_export`].
///
/// # Errors
/// Returns the strict codec refusal of the first record that is not canonical.
pub fn decode_offline_export(
    export: &OfflineExport,
    _trust: &IndependentOfflineTrust,
) -> Result<CompleteOfflineArtifact, ExportCodecError> {
    let facts: Vec<&str> = export.facts.iter().map(FactRef::as_str).collect();
    let bytes = |bucket: &[CanonicalBytes]| -> Vec<Vec<u8>> {
        bucket
            .iter()
            .map(|record| record.as_bytes().to_vec())
            .collect()
    };
    CompleteOfflineArtifact::decode(
        &facts,
        &bytes(&export.receipts),
        &bytes(&export.proofs),
        &bytes(&export.certificates),
        &bytes(&export.headers),
    )
}

/// Refusal of an offline verification of a received export.
#[derive(Clone, Debug, Eq, PartialEq)]
pub enum OfflineVerificationError {
    Codec(ExportCodecError),
    Verify(CompleteExportError),
    /// The independently verified level is below the level the response claimed.
    ClaimExceedsVerified {
        claimed: Level,
        verified: Level,
    },
}

/// Decodes and verifies a received export without the daemon or the network, at the level
/// originally requested. The response's claimed level is not trusted: a verified level below
/// it is refused.
///
/// # Errors
/// Returns the codec refusal, the complete verifier refusal, or
/// [`OfflineVerificationError::ClaimExceedsVerified`].
pub fn verify_complete_offline_export(
    read: &VerifiedRead<OfflineExport>,
    requested: Level,
    trust: &IndependentOfflineTrust,
) -> Result<CompleteVerificationReport, OfflineVerificationError> {
    let artifact =
        decode_offline_export(&read.value, trust).map_err(OfflineVerificationError::Codec)?;
    let report = verify_complete(&artifact, trust, verification_level(requested))
        .map_err(OfflineVerificationError::Verify)?;
    let verified = Level::from(report.achieved());
    if verified < read.achieved_verification_level {
        return Err(OfflineVerificationError::ClaimExceedsVerified {
            claimed: read.achieved_verification_level,
            verified,
        });
    }
    Ok(report)
}

impl AgentEnvelopeTransport {
    /// Retrieves the complete offline evidence for 1 through 16 facts at the requested level.
    ///
    /// # Errors
    ///
    /// Returns `InvalidRequest` for a fact set outside the shared grammar or a SettlementAnchored
    /// request before sending, the established error envelope, `Transport`, or `Decode` when the
    /// answer is not exactly the requested facts at or above the requested level.
    pub fn export_offline(
        &self,
        request_id: RequestId,
        credential: &EnvelopeCredential,
        request: &ReadRequest<Vec<FactRef>>,
    ) -> Result<VerifiedRead<OfflineExport>, EnvelopeError> {
        let operation = Operation::ExportOffline;
        let request =
            validate_export_request(request.clone()).map_err(|_| EnvelopeError::InvalidRequest)?;
        if request.requested_verification_level == Level::SettlementAnchored {
            return Err(EnvelopeError::InvalidRequest);
        }
        let success = self.send_operation(
            operation,
            request_id,
            &json!({
                "fact_set": request.selector.iter().map(FactRef::as_str).collect::<Vec<_>>(),
                "requested_verification_level": level_wire(request.requested_verification_level),
            }),
            Some(credential),
            None,
        )?;
        let read = object(
            &success.value,
            &["value", "achieved_verification_level", "freshness"],
            operation,
        )?;
        let read = VerifiedRead::new(
            decode_export(&read["value"], operation)?,
            level_from_wire(&read["achieved_verification_level"], operation)?,
            decode_freshness(&read["freshness"], operation)?,
        );
        if success.verification_status
            != VerificationStatus::Achieved(read.achieved_verification_level)
        {
            return Err(violation(operation));
        }
        check_export_response(&request, &read).map_err(|_| violation(operation))?;
        Ok(read)
    }
}
