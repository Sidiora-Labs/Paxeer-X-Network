use base64::engine::general_purpose::STANDARD;
use base64::Engine as _;
use serde::{Deserialize, Serialize};

use crate::{MigrationError, SourceEvidence};

const VERSION: &str = "layerx-migration-source-v2";

#[derive(Serialize)]
pub struct SourceSettlementRequestV2 {
    version: &'static str,
    order_digest: [u8; 32],
    chain: String,
    source_evidence: String,
}

impl SourceSettlementRequestV2 {
    pub fn new(
        order_digest: [u8; 32],
        chain: &str,
        evidence: &SourceEvidence,
    ) -> Result<Self, MigrationError> {
        if order_digest == [0; 32] || !matches!(chain, "ethereum" | "solana") {
            return Err(MigrationError::InvalidEvidence);
        }
        Ok(Self {
            version: VERSION,
            order_digest,
            chain: chain.to_owned(),
            source_evidence: STANDARD.encode(evidence.canonical()),
        })
    }
}

#[derive(Deserialize, Serialize)]
#[serde(deny_unknown_fields)]
pub struct SourceSettlementResponseV2 {
    pub version: String,
    pub order_digest: [u8; 32],
    pub state: String,
    pub source_evidence_digest: [u8; 32],
    pub source_claim_id: Option<[u8; 32]>,
}

impl SourceSettlementResponseV2 {
    pub fn validate(
        &self,
        request: &SourceSettlementRequestV2,
        evidence: &SourceEvidence,
    ) -> Result<(), MigrationError> {
        if self.version != VERSION
            || self.order_digest != request.order_digest
            || self.source_evidence_digest != evidence.digest()
            || request.source_evidence != STANDARD.encode(evidence.canonical())
            || !matches!(self.state.as_str(), "source_pending" | "source_settled" | "layerx_pending" | "layerx_refused" | "done")
            || self.source_claim_id == Some([0; 32])
            || (self.state != "source_pending" && self.source_claim_id.is_none())
        {
            return Err(MigrationError::EvidenceMismatch);
        }
        Ok(())
    }
}
