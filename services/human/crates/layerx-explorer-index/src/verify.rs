//! Index-independent verification of user-pasted protocol evidence.

use layerx_proof::inclusion::{
    verify_activity, verify_state, InclusionError, SequencerAuthorization,
};
use layerx_proof::merkle::{decode_proof, encode_proof as encode_merkle_proof, MerkleError, Proof};
use layerx_proof::receipt::{verify_outcome, AuthorizedBatch, ReceiptCheck};
use layerx_types::verify::VerificationLevel;

/// Which exact verifier established the reported level.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum EvidenceKind {
    Receipt,
    ActivityInclusion,
    StateInclusion,
}

/// Evidence identifiers returned only by the corresponding `layerx-proof`
/// success value.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct VerificationReport {
    pub kind: EvidenceKind,
    pub achieved_level: VerificationLevel,
    pub receipt_digest: Option<[u8; 32]>,
    pub header_digest: Option<[u8; 32]>,
    pub proof_root: Option<[u8; 32]>,
}

/// Untrusted inclusion bytes supplied by the public verifier caller. Authority
/// is deliberately absent: callers cannot select the keys trusted by a node.
pub struct PastedInclusion<'a> {
    pub kind: EvidenceKind,
    pub proof_bytes: &'a [u8],
    pub canonical_leaf_bytes: &'a [u8],
    pub named_root: [u8; 32],
    pub canonical_header_bytes: &'a [u8],
    pub header_signature: [u8; 64],
}

/// Typed verification failure. No failure carries a lower claimed level.
#[derive(Clone, Debug, Eq, PartialEq)]
pub enum VerifyError {
    MissingTrust(EvidenceKind),
    UnsupportedKind,
    Proof(MerkleError),
    Receipt(ReceiptCheck),
    Inclusion(InclusionError),
    NamedRoot,
}

/// Node-configured proof verifier. It has no index, database, network, profile
/// or clock access, and public callers cannot add trust anchors per request.
#[derive(Clone, Debug)]
pub struct Verifier {
    receipt_authorities: Vec<AuthorizedBatch>,
    sequencer_authorities: Vec<SequencerAuthorization>,
}

impl Verifier {
    /// Installs trust material accepted from the same boundary configuration as
    /// the node. Empty sets are allowed so deployments can expose one verifier
    /// kind without silently trusting pasted keys for the other kind.
    #[must_use]
    pub fn new(
        receipt_authorities: Vec<AuthorizedBatch>,
        sequencer_authorities: Vec<SequencerAuthorization>,
    ) -> Self {
        Self {
            receipt_authorities,
            sequencer_authorities,
        }
    }

    /// Verifies pasted canonical receipt bytes through `layerx-proof` against
    /// only the configured batch authorities.
    ///
    /// # Errors
    ///
    /// Returns missing trust or the exact receipt check that failed without
    /// consulting the explorer index.
    pub fn receipt(&self, pasted_receipt: &[u8]) -> Result<VerificationReport, VerifyError> {
        let mut first_failure = None;
        for authorised in &self.receipt_authorities {
            match verify_outcome(pasted_receipt, authorised) {
                Ok(verified) => {
                    return Ok(VerificationReport {
                        kind: EvidenceKind::Receipt,
                        achieved_level: verified.level(),
                        receipt_digest: verified.evidence().receipt_digest(),
                        header_digest: None,
                        proof_root: None,
                    });
                }
                Err(failure) => {
                    first_failure.get_or_insert(failure.check);
                }
            }
        }
        Err(first_failure.map_or(
            VerifyError::MissingTrust(EvidenceKind::Receipt),
            VerifyError::Receipt,
        ))
    }

    /// Reconstructs a pasted canonical Merkle proof and verifies its signed
    /// batch inclusion through the plane proof machinery and configured keys.
    ///
    /// # Errors
    ///
    /// Returns structural proof, missing trust, authority, signature, root or
    /// inclusion errors without consulting the explorer index.
    pub fn inclusion(
        &self,
        pasted: &PastedInclusion<'_>,
    ) -> Result<VerificationReport, VerifyError> {
        let proof = decode_proof(pasted.proof_bytes).map_err(VerifyError::Proof)?;
        let mut first_failure = None;
        for authorised in &self.sequencer_authorities {
            let result = match pasted.kind {
                EvidenceKind::ActivityInclusion => verify_activity(
                    pasted.canonical_leaf_bytes,
                    &proof,
                    pasted.canonical_header_bytes,
                    &pasted.header_signature,
                    authorised,
                ),
                EvidenceKind::StateInclusion => verify_state(
                    pasted.canonical_leaf_bytes,
                    &proof,
                    &pasted.named_root,
                    pasted.canonical_header_bytes,
                    &pasted.header_signature,
                    authorised,
                ),
                EvidenceKind::Receipt => return Err(VerifyError::UnsupportedKind),
            };
            match result {
                Ok(evidence) => {
                    if evidence.evidence().proof_root() != Some(pasted.named_root) {
                        return Err(VerifyError::NamedRoot);
                    }
                    return Ok(VerificationReport {
                        kind: pasted.kind,
                        achieved_level: evidence.level(),
                        receipt_digest: None,
                        header_digest: evidence.evidence().header_digest(),
                        proof_root: evidence.evidence().proof_root(),
                    });
                }
                Err(failure) => {
                    first_failure.get_or_insert(failure);
                }
            }
        }
        Err(first_failure.map_or(
            VerifyError::MissingTrust(pasted.kind),
            VerifyError::Inclusion,
        ))
    }
}

/// Produces the bounded canonical representation accepted by the public proof
/// verifier. Encoding lives in `layerx-proof`, not in the Human projection.
#[must_use]
pub fn encode_proof(proof: &Proof) -> Vec<u8> {
    encode_merkle_proof(proof)
}
