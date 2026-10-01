//! Authenticated LNI acquisition of signed batch headers and availability data.

use std::path::PathBuf;

use layerx_agent_api::read::{
    BatchRef, CheckpointRef, CheckpointValue, Freshness, RelativeTo, VerifiedRead,
};
use layerx_agent_api::verify::Level;
use layerx_agent_api::{prepare::CanonicalBytes, Sequence};
use layerx_client::availability::{
    fetch, AvailabilityResult, AvailabilitySelector, FetchContext, FetchError, FetchOutcome,
    Provider, ProviderSet, RetrievalLimits,
};
use layerx_client::evidence::{checkpoint, CheckpointSelector, EvidenceContext, EvidenceError};
use layerx_client::lni::handshake::{perform, Handshake, HandshakeConfig, HandshakeError};
use layerx_client::lni::schema::{
    decode_envelope, encode_envelope, Capability, Envelope, SchemaError,
};
use layerx_client::lni::transport::{ConnectionGate, FrameTransport, Limits, TransportError, Uds};
use layerx_crypto::ed25519;
use layerx_proof::availability::RootCommitments;
use layerx_proof::checkpoint::{CheckpointError, ThresholdReport};
use layerx_wire::hash::batch_header_digest;
use layerx_wire::receipt::{decode_batch_header, encode_batch_header};

use crate::{BatchAuthorization, CheckpointCoordinate, NodeBatch, NodeCheckpoint, NodeHead};

const BATCH_HEADER_REQUEST_TAG: u16 = 12;
const BATCH_HEADER_RESPONSE_TAG: u16 = 13;
const BATCH_SELECTOR_VERSION: u16 = 1;
const BATCH_PROOF_VERSION: u16 = 1;
const BATCH_PROOF_BYTES: usize = 2 + 32 + 32 + 8 + 8 + 64;

/// Immutable startup policy for the sole core evidence boundary.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct NodeSourceConfig {
    pub socket: PathBuf,
    pub handshake: HandshakeConfig,
    pub transport_limits: Limits,
    pub retrieval_limits: RetrievalLimits,
}

/// A batch archive input established from one authenticated LNI connection.
pub struct AcquiredBatch {
    pub batch: NodeBatch,
    pub availability: AvailabilityResult,
    /// Head coordinates. The finalised checkpoint coordinate is present only
    /// when its certificate verified through the same authenticated session.
    pub head: NodeHead,
    pub checkpoint: CheckpointAcquisition,
}

/// Certificate-verified checkpoint material for `Archive::from_node`.
pub struct AcquiredCheckpoint {
    read: VerifiedRead<CheckpointValue>,
    report: ThresholdReport,
    coordinate: CheckpointCoordinate,
}

impl AcquiredCheckpoint {
    #[must_use]
    pub const fn material(&self) -> NodeCheckpoint<'_> {
        NodeCheckpoint::verified(&self.read, &self.report)
    }

    #[must_use]
    pub const fn coordinate(&self) -> CheckpointCoordinate {
        self.coordinate
    }
}

/// Outcome of the checkpoint read that accompanies every batch acquisition.
/// A bare handshake identifier is never promoted to a finalised coordinate.
pub enum CheckpointAcquisition {
    /// The node reports no finalised checkpoint.
    NodeHasNoCheckpoint,
    Verified(Box<AcquiredCheckpoint>),
    Refused {
        checkpoint_id: [u8; 32],
        refusal: CheckpointRefusal,
    },
}

impl CheckpointAcquisition {
    /// The finalised coordinate, present only for verified evidence.
    #[must_use]
    pub fn coordinate(&self) -> Option<CheckpointCoordinate> {
        match self {
            Self::Verified(verified) => Some(verified.coordinate),
            Self::NodeHasNoCheckpoint | Self::Refused { .. } => None,
        }
    }
}

/// Exact reason an advertised checkpoint did not count as finalised.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum CheckpointRefusal {
    CapabilityMissing,
    Unavailable,
    SelectorMismatch,
    DomainMismatch,
    SignerMembership,
    Threshold,
    Signature,
    CertificateMismatch,
    Settlement,
    HeadBehindCheckpoint,
    Transport,
    CoreRefusal,
    Malformed,
}

impl CheckpointRefusal {
    #[must_use]
    pub const fn code(self) -> &'static str {
        match self {
            Self::CapabilityMissing => "checkpoint_capability_missing",
            Self::Unavailable => "checkpoint_unavailable",
            Self::SelectorMismatch => "checkpoint_selector_mismatch",
            Self::DomainMismatch => "checkpoint_domain_mismatch",
            Self::SignerMembership => "checkpoint_signer_membership",
            Self::Threshold => "checkpoint_threshold",
            Self::Signature => "checkpoint_signature",
            Self::CertificateMismatch => "checkpoint_certificate_mismatch",
            Self::Settlement => "checkpoint_settlement",
            Self::HeadBehindCheckpoint => "checkpoint_head_behind",
            Self::Transport => "checkpoint_transport",
            Self::CoreRefusal => "checkpoint_core_refusal",
            Self::Malformed => "checkpoint_malformed",
        }
    }

    /// Classifies a checkpoint read failure without discarding its kind.
    #[must_use]
    pub const fn from_evidence(error: &EvidenceError) -> Self {
        match error {
            EvidenceError::Unavailable => Self::Unavailable,
            EvidenceError::SelectorMismatch => Self::SelectorMismatch,
            EvidenceError::NetworkMismatch | EvidenceError::SequencerMismatch => {
                Self::DomainMismatch
            }
            EvidenceError::BondedSet
            | EvidenceError::Checkpoint(
                CheckpointError::SignerMembership(_) | CheckpointError::DuplicateSigner(_),
            ) => Self::SignerMembership,
            EvidenceError::Requirements
            | EvidenceError::Checkpoint(CheckpointError::Threshold { .. }) => Self::Threshold,
            EvidenceError::Checkpoint(CheckpointError::Signature(_)) => Self::Signature,
            EvidenceError::Registration
            | EvidenceError::Settlement
            | EvidenceError::Checkpoint(
                CheckpointError::Settlement | CheckpointError::Configuration(_),
            ) => Self::Settlement,
            EvidenceError::Checkpoint(_) => Self::CertificateMismatch,
            EvidenceError::Transport(_) | EvidenceError::Envelope(_) => Self::Transport,
            EvidenceError::CoreRefusal { .. } => Self::CoreRefusal,
            EvidenceError::UnexpectedResponse
            | EvidenceError::Malformed
            | EvidenceError::Activity
            | EvidenceError::Receipt
            | EvidenceError::Merkle(_)
            | EvidenceError::Inclusion(_)
            | EvidenceError::Account(_) => Self::Malformed,
        }
    }
}

/// Exact failure while acquiring core evidence. Partial bytes are never
/// returned as an archive input.
#[derive(Clone, Debug, Eq, PartialEq)]
pub enum NodeSourceError {
    Transport(TransportError),
    Handshake(HandshakeError),
    Schema(SchemaError),
    Capability(Capability),
    UnexpectedResponse,
    MalformedBatchProof,
    BatchHeader,
    BatchSelector,
    SequencerIdentity,
    HeaderSignature,
    Availability(FetchError),
    AvailabilityPartial,
}

impl From<TransportError> for NodeSourceError {
    fn from(value: TransportError) -> Self {
        Self::Transport(value)
    }
}

impl From<SchemaError> for NodeSourceError {
    fn from(value: SchemaError) -> Self {
        Self::Schema(value)
    }
}

/// Reconnect-per-acquisition LNI client. It never accepts an unsigned batch
/// value and never synthesises a proof level from node metadata.
pub struct LniArchiveSource {
    config: NodeSourceConfig,
    gate: ConnectionGate,
    correlation_id: u64,
}

impl LniArchiveSource {
    #[must_use]
    pub fn new(config: NodeSourceConfig) -> Self {
        Self {
            gate: ConnectionGate::new(config.transport_limits.maximum_connections),
            config,
            correlation_id: 1,
        }
    }

    /// Fetches one exact signed header and its complete verified availability
    /// records through the same authenticated node session.
    ///
    /// # Errors
    ///
    /// Refuses missing capabilities, selector substitution, key/range drift,
    /// invalid signatures, partial availability and all bounded transport
    /// failures.
    pub fn acquire(&mut self, batch_number: u64) -> Result<AcquiredBatch, NodeSourceError> {
        if batch_number == 0 {
            return Err(NodeSourceError::BatchSelector);
        }
        let mut transport = Uds::connect(
            &self.config.socket,
            &self.gate,
            self.config.transport_limits,
        )?;
        let handshake = perform(&mut transport, &self.config.handshake, None)
            .map_err(NodeSourceError::Handshake)?;
        for capability in [Capability::BatchHeader, Capability::AvailabilityFetch] {
            if !handshake.capabilities().contains(capability) {
                return Err(NodeSourceError::Capability(capability));
            }
        }
        let correlation_id = self.next_correlation()?;
        let mut selector = Vec::with_capacity(10);
        selector.extend_from_slice(&BATCH_SELECTOR_VERSION.to_be_bytes());
        selector.extend_from_slice(&batch_number.to_be_bytes());
        let request = encode_envelope(Envelope {
            version: handshake.node().interface_version,
            message_tag: BATCH_HEADER_REQUEST_TAG,
            correlation_id,
            canonical_payload: &selector,
            proof_material: &[],
        })?;
        transport.send(&request)?;
        let response_bytes = transport.receive()?;
        let response = decode_envelope(&response_bytes)?;
        if response.version.major != handshake.node().interface_version.major
            || response.message_tag != BATCH_HEADER_RESPONSE_TAG
            || response.correlation_id != correlation_id
        {
            return Err(NodeSourceError::UnexpectedResponse);
        }
        let authorization = decode_batch_proof(response.proof_material)?;
        let header = decode_batch_header(response.canonical_payload)
            .map_err(|_| NodeSourceError::BatchHeader)?;
        let reproduced = encode_batch_header(&header).map_err(|_| NodeSourceError::BatchHeader)?;
        if reproduced != response.canonical_payload
            || header.protocol_version() != handshake.node().protocol_version
            || header.network_id() != handshake.node().network_id
            || header.batch_number() != batch_number
        {
            return Err(NodeSourceError::BatchSelector);
        }
        if authorization.sequencer_public_key != handshake.node().authorised_sequencer_key
            || authorization.sequencer_id != header.sequencer_id()
            || batch_number < authorization.first_batch_number
            || batch_number > authorization.last_batch_number
        {
            return Err(NodeSourceError::SequencerIdentity);
        }
        let digest = batch_header_digest(&reproduced).map_err(|_| NodeSourceError::BatchHeader)?;
        ed25519::verify_digest(
            &authorization.sequencer_public_key,
            &authorization.header_signature,
            &digest,
        )
        .map_err(|_| NodeSourceError::HeaderSignature)?;

        let roots = RootCommitments {
            activity: header.activity_merkle_root(),
            receipt: header.receipt_merkle_root(),
            event: header.event_merkle_root(),
            oracle: header.oracle_root(),
        };
        let availability_correlation = self.next_correlation()?;
        let mut providers = ProviderSet::new(vec![Provider {
            name: "authenticated-node".to_owned(),
            transport: &mut transport,
        }]);
        let outcome = fetch(
            &mut providers,
            AvailabilitySelector::Batch(batch_number),
            FetchContext {
                interface_version: handshake.node().interface_version,
                correlation_id: availability_correlation,
                expected_batch_number: batch_number,
                data_availability_root: header.data_availability_root(),
                record_roots: roots,
                limits: self.config.retrieval_limits,
            },
            |_| {},
        )
        .map_err(NodeSourceError::Availability)?;
        let FetchOutcome::Complete(availability) = outcome else {
            return Err(NodeSourceError::AvailabilityPartial);
        };
        let checkpoint = acquire_checkpoint(&mut transport, &handshake, self.next_correlation()?);
        Ok(AcquiredBatch {
            batch: NodeBatch::authenticated(reproduced, authorization),
            availability: *availability,
            head: NodeHead {
                latest_sealed_batch: handshake.node().latest_sealed_batch,
                latest_finalised_checkpoint: checkpoint.coordinate(),
            },
            checkpoint,
        })
    }

    fn next_correlation(&mut self) -> Result<u64, NodeSourceError> {
        let current = self.correlation_id;
        self.correlation_id = self
            .correlation_id
            .checked_add(1)
            .ok_or(NodeSourceError::BatchSelector)?;
        Ok(current)
    }
}

/// Resolves the handshake-advertised checkpoint on the same session.
fn acquire_checkpoint(
    transport: &mut Uds,
    handshake: &Handshake,
    correlation_id: u64,
) -> CheckpointAcquisition {
    let node = handshake.node();
    let advertised = node.latest_finalised_checkpoint;
    if advertised == [0; 32] {
        return CheckpointAcquisition::NodeHasNoCheckpoint;
    }
    if !handshake.capabilities().contains(Capability::Checkpoint) {
        return CheckpointAcquisition::Refused {
            checkpoint_id: advertised,
            refusal: CheckpointRefusal::CapabilityMissing,
        };
    }
    let context = EvidenceContext {
        interface_version: node.interface_version,
        correlation_id,
        expected_protocol_version: node.protocol_version,
        expected_network_id: node.network_id,
        handshake_sequencer_key: node.authorised_sequencer_key,
    };
    verified_checkpoint(transport, advertised, context, node.latest_sealed_batch)
}

/// Reads the advertised checkpoint by identifier and keeps it only when the
/// certificate, bonded-set context, threshold, domain and head coordinate all
/// verify.
fn verified_checkpoint(
    transport: &mut Uds,
    advertised: [u8; 32],
    context: EvidenceContext,
    latest_sealed_batch: u64,
) -> CheckpointAcquisition {
    let refused = |refusal| CheckpointAcquisition::Refused {
        checkpoint_id: advertised,
        refusal,
    };
    let verified = match checkpoint(
        transport,
        CheckpointSelector::Identifier(advertised),
        context,
    ) {
        Ok(verified) => verified,
        Err(error) => return refused(CheckpointRefusal::from_evidence(&error)),
    };
    let report = verified.report().clone();
    if report.evidence().checkpoint_id() != Some(advertised)
        || report.network_id() != context.expected_network_id
        || report.protocol_version() != context.expected_protocol_version
    {
        return refused(CheckpointRefusal::SelectorMismatch);
    }
    if report.batch_number() > latest_sealed_batch {
        return refused(CheckpointRefusal::HeadBehindCheckpoint);
    }
    let Ok(bytes) = CanonicalBytes::new(verified.checkpoint_bytes().to_vec()) else {
        return refused(CheckpointRefusal::Malformed);
    };
    let (Ok(batch_reference), Ok(checkpoint_reference)) = (
        BatchRef::new(latest_sealed_batch.to_string()),
        CheckpointRef::new(hex(&advertised)),
    ) else {
        return refused(CheckpointRefusal::Malformed);
    };
    let freshness = Freshness {
        chain_head: Sequence(report.last_sequence()),
        latest_sealed_batch: batch_reference,
        latest_finalised_checkpoint: checkpoint_reference.clone(),
        value_sequence: Sequence(report.last_sequence()),
        relative_to: RelativeTo::Checkpoint(checkpoint_reference),
    };
    let coordinate = CheckpointCoordinate {
        batch_number: report.batch_number(),
        checkpoint_id: advertised,
    };
    CheckpointAcquisition::Verified(Box::new(AcquiredCheckpoint {
        read: VerifiedRead::new(
            CheckpointValue(bytes),
            Level::CheckpointFinalised,
            freshness,
        ),
        report,
        coordinate,
    }))
}

fn hex(bytes: &[u8]) -> String {
    use std::fmt::Write as _;
    bytes.iter().fold(
        String::with_capacity(bytes.len() * 2),
        |mut output, byte| {
            let _ = write!(output, "{byte:02x}");
            output
        },
    )
}

fn decode_batch_proof(bytes: &[u8]) -> Result<BatchAuthorization, NodeSourceError> {
    if bytes.len() != BATCH_PROOF_BYTES {
        return Err(NodeSourceError::MalformedBatchProof);
    }
    let version = u16::from_be_bytes(
        bytes[0..2]
            .try_into()
            .map_err(|_| NodeSourceError::MalformedBatchProof)?,
    );
    if version != BATCH_PROOF_VERSION {
        return Err(NodeSourceError::MalformedBatchProof);
    }
    let sequencer_id = bytes[2..34]
        .try_into()
        .map_err(|_| NodeSourceError::MalformedBatchProof)?;
    let sequencer_public_key = bytes[34..66]
        .try_into()
        .map_err(|_| NodeSourceError::MalformedBatchProof)?;
    let first_batch_number = u64::from_be_bytes(
        bytes[66..74]
            .try_into()
            .map_err(|_| NodeSourceError::MalformedBatchProof)?,
    );
    let last_batch_number = u64::from_be_bytes(
        bytes[74..82]
            .try_into()
            .map_err(|_| NodeSourceError::MalformedBatchProof)?,
    );
    let header_signature = bytes[82..146]
        .try_into()
        .map_err(|_| NodeSourceError::MalformedBatchProof)?;
    if first_batch_number == 0 || first_batch_number > last_batch_number {
        return Err(NodeSourceError::MalformedBatchProof);
    }
    Ok(BatchAuthorization {
        sequencer_id,
        sequencer_public_key,
        first_batch_number,
        last_batch_number,
        header_signature,
    })
}
