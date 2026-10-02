//! Minimal offline-verifier entry point.

use layerx_proof::checkpoint::SettlementDomain;
use layerx_proof::export::{
    verify, verify_complete, CompleteExportError, CompleteVerificationReport,
    IndependentOfflineTrust, OfflineExport,
};
use layerx_proof::export_codec::CompleteOfflineArtifact;
use layerx_types::verify::VerificationLevel;

fn verify_without_network(
    artifact: &OfflineExport,
    expected_settlement_domain: SettlementDomain,
) -> Result<layerx_proof::export::VerificationReport, layerx_proof::export::ExportVerificationError>
{
    verify(artifact, expected_settlement_domain)
}

fn verify_complete_without_network(
    artifact: &CompleteOfflineArtifact,
    trust: &IndependentOfflineTrust,
    requested: VerificationLevel,
) -> Result<CompleteVerificationReport, CompleteExportError> {
    verify_complete(artifact, trust, requested)
}

fn main() {
    let verifier: fn(
        &OfflineExport,
        SettlementDomain,
    ) -> Result<
        layerx_proof::export::VerificationReport,
        layerx_proof::export::ExportVerificationError,
    > = verify_without_network;
    let complete: fn(
        &CompleteOfflineArtifact,
        &IndependentOfflineTrust,
        VerificationLevel,
    ) -> Result<CompleteVerificationReport, CompleteExportError> = verify_complete_without_network;
    let _ = (verifier, complete);
}
