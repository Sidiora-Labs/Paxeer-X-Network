//! Receipt-bound LayerX settlement verification.
//!
//! An indexed receipt is settlement-verified only when all of the following
//! hold: the exact checkpoint certificate and bonded-set context pass the
//! canonical client checks (guarantor quorum, settlement domain and
//! registration binding), the signed batch header is byte-for-byte the header
//! that checkpoint certifies, the receipt is included under that header's
//! receipt root and its signature verifies under the authenticated sequencer
//! authorisation, and the same certificate is independently published on
//! Paxeer. Reorganisation depth, challenge-window defaults and receipt
//! ordering are never inputs, so none of them can produce a verified level.

use layerx_client::evidence::{EvidenceError, FinalityEvidenceCandidate, VerifiedCheckpoint};
use layerx_paxeer_verifier::{BlockAnchor, EndpointFailure, PaxeerCheckpointVerifier};
use layerx_proof::inclusion::{verify_receipt, InclusionError, SequencerAuthorization};
use layerx_proof::merkle::{decode_proof, MerkleError};
use layerx_wire::receipt::decode_batch_header;

/// The settlement level a receipt carries in the published contract.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum SettlementLevel {
    /// No settlement evidence was available or the publication could not be
    /// established; nothing is claimed beyond local index stability.
    Unverified,
    /// Supplied evidence contradicts itself or the receipt.
    Invalid,
    /// Every check in [`verify_settlement`] passed.
    Verified,
}

impl SettlementLevel {
    /// Returns the published label.
    #[must_use]
    pub const fn as_str(self) -> &'static str {
        match self {
            Self::Unverified => "unverified",
            Self::Invalid => "invalid",
            Self::Verified => "verified",
        }
    }
}

/// Exact evidence for one indexed receipt, as produced by the finality
/// producer and the replica that served the receipt.
#[derive(Clone, Copy, Debug)]
pub struct SettlementInput<'a> {
    /// Canonical receipt bytes as indexed.
    pub receipt: &'a [u8],
    /// Encoded index-aware Merkle proof of the receipt under the header's
    /// receipt root.
    pub receipt_proof: &'a [u8],
    /// Canonical signed batch header bytes.
    pub header: &'a [u8],
    /// Sequencer signature over the batch header digest.
    pub header_signature: &'a [u8; 64],
    /// Sequencer authorisation authenticated from the genesis trust and
    /// handover history for this batch, never taken from the evidence itself.
    pub authorization: &'a SequencerAuthorization,
    /// Batch number the index recorded for this receipt.
    pub batch_number: u64,
    /// Exact CP1 checkpoint certificate bytes.
    pub checkpoint: &'a [u8],
    /// Exact CX1 bonded-set and registration context bytes.
    pub context: &'a [u8],
    /// Protocol version the deployment is pinned to.
    pub protocol_version: u16,
    /// LayerX network the deployment is pinned to.
    pub network_id: u32,
    /// Independent Paxeer publication verifier; `None` when the deployment has
    /// no configured settlement trust.
    pub verifier: Option<&'a PaxeerCheckpointVerifier>,
}

/// The exact check that kept a receipt below the verified level.
#[derive(Clone, Debug, Eq, PartialEq)]
pub enum SettlementRefusal {
    /// The checkpoint certificate or its context failed canonical checks.
    Checkpoint(EvidenceError),
    /// The signed header is not the header the checkpoint certifies.
    HeaderNotCheckpointed,
    /// The checkpoint certifies a different batch than the index recorded.
    BatchMismatch { indexed: u64, checkpointed: u64 },
    /// The receipt proof bytes are not a canonical Merkle proof.
    ProofEncoding(MerkleError),
    /// Header authority, signature or receipt inclusion failed.
    Inclusion(InclusionError),
    /// The receipt bytes failed canonical decoding.
    ReceiptDecode,
    /// The receipt is not a protocol receipt.
    ReceiptShape,
    /// The receipt names a different protocol version than its header.
    ProtocolVersion,
    /// The receipt sequence lies outside the header's sequence range.
    SequenceRange,
    /// No independent publication verifier is configured.
    PublicationUnconfigured,
    /// The Paxeer publication could not be established.
    Publication(EndpointFailure),
    /// The publication names a different header, set or registration.
    PublicationBinding(EvidenceError),
}

impl SettlementRefusal {
    /// Returns the level this refusal publishes.
    #[must_use]
    pub const fn level(&self) -> SettlementLevel {
        match self {
            Self::PublicationUnconfigured | Self::Publication(_) => SettlementLevel::Unverified,
            Self::Checkpoint(_)
            | Self::HeaderNotCheckpointed
            | Self::BatchMismatch { .. }
            | Self::ProofEncoding(_)
            | Self::Inclusion(_)
            | Self::ReceiptDecode
            | Self::ReceiptShape
            | Self::ProtocolVersion
            | Self::SequenceRange
            | Self::PublicationBinding(_) => SettlementLevel::Invalid,
        }
    }
}

/// Settlement established for one receipt and the evidence source it rests on.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct VerifiedSettlement {
    checkpoint_id: [u8; 32],
    settlement_reference: Vec<u8>,
    set_version: u64,
    batch_number: u64,
    global_sequence: u64,
    header_digest: [u8; 32],
    registration: BlockAnchor,
    confirmed_head: BlockAnchor,
}

impl VerifiedSettlement {
    /// Always [`SettlementLevel::Verified`].
    #[must_use]
    pub const fn level(&self) -> SettlementLevel {
        SettlementLevel::Verified
    }

    #[must_use]
    pub const fn checkpoint_id(&self) -> [u8; 32] {
        self.checkpoint_id
    }

    #[must_use]
    pub fn settlement_reference(&self) -> &[u8] {
        &self.settlement_reference
    }

    #[must_use]
    pub const fn set_version(&self) -> u64 {
        self.set_version
    }

    #[must_use]
    pub const fn batch_number(&self) -> u64 {
        self.batch_number
    }

    #[must_use]
    pub const fn global_sequence(&self) -> u64 {
        self.global_sequence
    }

    #[must_use]
    pub const fn header_digest(&self) -> [u8; 32] {
        self.header_digest
    }

    /// Paxeer block that registered the checkpoint.
    #[must_use]
    pub const fn registration(&self) -> BlockAnchor {
        self.registration
    }

    /// Paxeer head that confirmed the registration.
    #[must_use]
    pub const fn confirmed_head(&self) -> BlockAnchor {
        self.confirmed_head
    }
}

/// Verifies one receipt's settlement through the canonical LayerX crates.
///
/// # Errors
///
/// Returns the first failed check. Local evidence failures map to
/// [`SettlementLevel::Invalid`]; a missing verifier or unestablished
/// publication maps to [`SettlementLevel::Unverified`].
pub fn verify_settlement(
    input: &SettlementInput<'_>,
) -> Result<VerifiedSettlement, SettlementRefusal> {
    let candidate = FinalityEvidenceCandidate::from_exact_bytes(
        input.checkpoint.to_vec(),
        input.context.to_vec(),
        input.protocol_version,
        input.network_id,
    )
    .map_err(SettlementRefusal::Checkpoint)?;
    if candidate.canonical_header() != input.header {
        return Err(SettlementRefusal::HeaderNotCheckpointed);
    }
    let checkpointed = decode_batch_header(candidate.canonical_header())
        .map_err(|_| SettlementRefusal::HeaderNotCheckpointed)?
        .batch_number();
    if checkpointed != input.batch_number {
        return Err(SettlementRefusal::BatchMismatch {
            indexed: input.batch_number,
            checkpointed,
        });
    }
    let proof = decode_proof(input.receipt_proof).map_err(SettlementRefusal::ProofEncoding)?;
    let inclusion = verify_receipt(
        input.receipt,
        &proof,
        input.header,
        input.header_signature,
        input.authorization,
    )
    .map_err(SettlementRefusal::Inclusion)?;
    let header = inclusion.header().header();
    let decoded = layerx_wire::receipt::decode(input.receipt)
        .map_err(|_| SettlementRefusal::ReceiptDecode)?;
    let receipt = decoded.protocol().ok_or(SettlementRefusal::ReceiptShape)?;
    if receipt.protocol_version() != header.protocol_version() {
        return Err(SettlementRefusal::ProtocolVersion);
    }
    if receipt.global_sequence() < header.first_sequence()
        || receipt.global_sequence() > header.last_sequence()
    {
        return Err(SettlementRefusal::SequenceRange);
    }
    let global_sequence = receipt.global_sequence();
    let header_digest = inclusion.header().digest();
    let verifier = input
        .verifier
        .ok_or(SettlementRefusal::PublicationUnconfigured)?;
    let certificate = candidate
        .certificate()
        .map_err(SettlementRefusal::Checkpoint)?;
    let set_version = candidate
        .set_version()
        .map_err(SettlementRefusal::Checkpoint)?;
    let publication = verifier
        .verify(&certificate, set_version)
        .map_err(SettlementRefusal::Publication)?;
    VerifiedCheckpoint::from_independent_publication(candidate, &publication)
        .map_err(SettlementRefusal::PublicationBinding)?;
    Ok(VerifiedSettlement {
        checkpoint_id: publication.checkpoint_id(),
        settlement_reference: publication.settlement_reference().to_vec(),
        set_version: publication.set_version(),
        batch_number: checkpointed,
        global_sequence,
        header_digest,
        registration: publication.registration(),
        confirmed_head: publication.confirmed_head(),
    })
}

#[cfg(test)]
mod tests {
    use super::*;
    use layerx_proof::merkle::{build_proof, encode_proof};

    const FINALITY_VECTOR: &str =
        include_str!("../../../../tests/vectors/finality_evidence_v1.vec");

    struct Vector {
        protocol_version: u16,
        network_id: u32,
        checkpoint: Vec<u8>,
        context: Vec<u8>,
        header: Vec<u8>,
        batch_number: u64,
        sequencer_id: [u8; 32],
    }

    fn field(name: &str) -> &'static str {
        FINALITY_VECTOR
            .lines()
            .filter_map(|line| line.split_once('='))
            .find(|(key, _)| *key == name)
            .map_or_else(
                || panic!("finality vector lacks {name}"),
                |(_, value)| value,
            )
    }

    fn hex(encoded: &str) -> Vec<u8> {
        assert_eq!(encoded.len() % 2, 0, "odd-length finality vector");
        (0..encoded.len())
            .step_by(2)
            .map(|index| {
                u8::from_str_radix(&encoded[index..index + 2], 16)
                    .unwrap_or_else(|error| panic!("invalid finality vector hex: {error}"))
            })
            .collect()
    }

    fn vector() -> Vector {
        assert_eq!(field("version"), "1");
        let protocol_version = field("protocol_version")
            .parse()
            .unwrap_or_else(|error| panic!("invalid protocol version: {error}"));
        let network_id = field("network_id")
            .parse()
            .unwrap_or_else(|error| panic!("invalid network id: {error}"));
        let checkpoint = hex(field("checkpoint_payload"));
        let context = hex(field("finality_proof"));
        let candidate = FinalityEvidenceCandidate::from_exact_bytes(
            checkpoint.clone(),
            context.clone(),
            protocol_version,
            network_id,
        )
        .unwrap_or_else(|error| panic!("recorded CP1/CX1 refused: {error:?}"));
        let header = candidate.canonical_header().to_vec();
        let decoded = decode_batch_header(&header)
            .unwrap_or_else(|error| panic!("checkpointed header refused: {error:?}"));
        Vector {
            protocol_version,
            network_id,
            checkpoint,
            context,
            header,
            batch_number: decoded.batch_number(),
            sequencer_id: decoded.sequencer_id(),
        }
    }

    fn proof_bytes(leaf: &[u8]) -> Vec<u8> {
        let (proof, _) =
            build_proof(&[leaf], 0).unwrap_or_else(|error| panic!("proof refused: {error:?}"));
        encode_proof(&proof)
    }

    struct Case {
        vector: Vector,
        header: Vec<u8>,
        signature: [u8; 64],
        authorization: SequencerAuthorization,
        batch_number: u64,
        checkpoint: Vec<u8>,
        context: Vec<u8>,
        network_id: u32,
        receipt_proof: Vec<u8>,
    }

    const LEAF: &[u8] = b"receipt leaf; every case below refuses before inclusion is reached";

    impl Case {
        fn new() -> Self {
            let vector = vector();
            Self {
                header: vector.header.clone(),
                signature: [0; 64],
                authorization: SequencerAuthorization::new(
                    vector.sequencer_id,
                    [0x5a; 32],
                    vector.batch_number,
                    vector.batch_number,
                ),
                batch_number: vector.batch_number,
                checkpoint: vector.checkpoint.clone(),
                context: vector.context.clone(),
                network_id: vector.network_id,
                receipt_proof: proof_bytes(LEAF),
                vector,
            }
        }

        fn run(&self) -> Result<VerifiedSettlement, SettlementRefusal> {
            verify_settlement(&SettlementInput {
                receipt: LEAF,
                receipt_proof: &self.receipt_proof,
                header: &self.header,
                header_signature: &self.signature,
                authorization: &self.authorization,
                batch_number: self.batch_number,
                checkpoint: &self.checkpoint,
                context: &self.context,
                protocol_version: self.vector.protocol_version,
                network_id: self.network_id,
                verifier: None,
            })
        }
    }

    fn refusal(case: &Case) -> SettlementRefusal {
        match case.run() {
            Ok(settlement) => panic!("unsigned evidence settled: {settlement:?}"),
            Err(refusal) => refusal,
        }
    }

    #[test]
    fn checkpointed_header_with_forged_signature_is_invalid() {
        let case = Case::new();
        let refused = refusal(&case);
        assert_eq!(
            refused,
            SettlementRefusal::Inclusion(InclusionError::HeaderSignature)
        );
        assert_eq!(refused.level(), SettlementLevel::Invalid);
    }

    #[test]
    fn header_other_than_the_checkpointed_header_is_invalid() {
        let mut case = Case::new();
        let last = case.header.len() - 1;
        case.header[last] ^= 1;
        assert_eq!(refusal(&case), SettlementRefusal::HeaderNotCheckpointed);
    }

    #[test]
    fn indexed_batch_other_than_the_checkpointed_batch_is_invalid() {
        let mut case = Case::new();
        case.batch_number = case.vector.batch_number + 1;
        assert_eq!(
            refusal(&case),
            SettlementRefusal::BatchMismatch {
                indexed: case.vector.batch_number + 1,
                checkpointed: case.vector.batch_number,
            }
        );
    }

    #[test]
    fn corrupted_checkpoint_or_context_is_invalid() {
        let mut case = Case::new();
        case.checkpoint[0] ^= 1;
        assert_eq!(
            refusal(&case),
            SettlementRefusal::Checkpoint(EvidenceError::Malformed)
        );

        let mut case = Case::new();
        case.context[0] ^= 1;
        assert_eq!(
            refusal(&case),
            SettlementRefusal::Checkpoint(EvidenceError::Malformed)
        );

        let mut case = Case::new();
        let last = case.checkpoint.len() - 1;
        case.checkpoint[last] ^= 1;
        assert_eq!(
            refusal(&case),
            SettlementRefusal::Checkpoint(EvidenceError::Registration)
        );
    }

    #[test]
    fn checkpoint_for_another_network_is_invalid() {
        let mut case = Case::new();
        case.network_id = case.vector.network_id + 1;
        let refused = refusal(&case);
        assert_eq!(
            refused,
            SettlementRefusal::Checkpoint(EvidenceError::NetworkMismatch)
        );
        assert_eq!(refused.level(), SettlementLevel::Invalid);
    }

    #[test]
    fn unauthorised_sequencer_or_batch_range_is_invalid() {
        let mut case = Case::new();
        let mut other = case.vector.sequencer_id;
        other[0] ^= 1;
        case.authorization =
            SequencerAuthorization::new(other, [0x5a; 32], case.batch_number, case.batch_number);
        assert_eq!(
            refusal(&case),
            SettlementRefusal::Inclusion(InclusionError::SequencerIdentity)
        );

        let mut case = Case::new();
        case.authorization = SequencerAuthorization::new(
            case.vector.sequencer_id,
            [0x5a; 32],
            case.batch_number + 1,
            case.batch_number + 1,
        );
        assert_eq!(
            refusal(&case),
            SettlementRefusal::Inclusion(InclusionError::BatchNumber)
        );
    }

    #[test]
    fn non_canonical_receipt_proof_is_invalid() {
        let mut case = Case::new();
        case.receipt_proof.push(0);
        assert_eq!(
            refusal(&case),
            SettlementRefusal::ProofEncoding(MerkleError::Encoding)
        );
    }

    #[test]
    fn absent_publication_never_reads_as_verified() {
        assert_eq!(
            SettlementRefusal::PublicationUnconfigured.level(),
            SettlementLevel::Unverified
        );
        assert_eq!(SettlementLevel::Unverified.as_str(), "unverified");
        assert_eq!(SettlementLevel::Invalid.as_str(), "invalid");
        assert_eq!(SettlementLevel::Verified.as_str(), "verified");
    }
}
