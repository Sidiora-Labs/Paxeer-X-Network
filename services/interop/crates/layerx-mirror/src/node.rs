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
    pub checkpoint_policy: NativeCheckpointPolicy,
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
        let checkpoint = acquire_checkpoint(&mut transport, &handshake, self.next_correlation()?, &self.config.checkpoint_policy);
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

    /// Refreshes the independent node head and native finality evidence.
    ///
    /// # Errors
    /// Refuses transport, handshake, and correlation failures.
    pub fn observe_checkpoint(&mut self) -> Result<(NodeHead, CheckpointAcquisition), NodeSourceError> {
        let mut transport = Uds::connect(&self.config.socket, &self.gate, self.config.transport_limits)?;
        let handshake = perform(&mut transport, &self.config.handshake, None)
            .map_err(NodeSourceError::Handshake)?;
        let checkpoint = acquire_checkpoint(&mut transport, &handshake, self.next_correlation()?, &self.config.checkpoint_policy);
        let head = NodeHead { latest_sealed_batch: handshake.node().latest_sealed_batch,
                              latest_finalised_checkpoint: checkpoint.coordinate() };
        Ok((head, checkpoint))
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
    policy: &NativeCheckpointPolicy,
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
    verified_checkpoint(transport, advertised, context, node.latest_sealed_batch, node.chain_head_sequence, policy)
}

/// Reads the advertised checkpoint by identifier and keeps it only when the
/// certificate, bonded-set context, threshold, domain and head coordinate all
/// verify.
fn verified_checkpoint(
    transport: &mut Uds,
    advertised: [u8; 32],
    context: EvidenceContext,
    latest_sealed_batch: u64,
    chain_head_sequence: u64,
    policy: &NativeCheckpointPolicy,
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
    if report.batch_number() > latest_sealed_batch || report.last_sequence() > chain_head_sequence {
        return refused(CheckpointRefusal::HeadBehindCheckpoint);
    }
    if let Err(error) = native_publication(&verified, policy, context.handshake_sequencer_key) {
        return refused(error);
    }
    let Ok(canonical) = archival_certificate(&verified) else {
        return refused(CheckpointRefusal::Malformed);
    };
    let Ok(bytes) = CanonicalBytes::new(canonical) else {
        return refused(CheckpointRefusal::Malformed);
    };
    let (Ok(batch_reference), Ok(checkpoint_reference)) = (
        BatchRef::new(latest_sealed_batch.to_string()),
        CheckpointRef::new(hex(&advertised)),
    ) else {
        return refused(CheckpointRefusal::Malformed);
    };
    let freshness = Freshness {
        chain_head: Sequence(chain_head_sequence),
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

#[derive(Clone, Debug, Eq, PartialEq, serde::Deserialize)]
#[serde(deny_unknown_fields)]
pub struct NativeCheckpointPolicy {
    pub rpc: crate::rpc::RpcQuorumConfig,
    pub chain_id: u64,
    pub genesis_hash_hex: String,
    pub sequencer_public_key_hex: String,
    pub confirmations: u64,
}

fn unhex(value: &str) -> Result<Vec<u8>, CheckpointRefusal> {
    let text = value.strip_prefix("0x").unwrap_or(value);
    if text.len() % 2 != 0 || text.len() > 4_000_000 {
        return Err(CheckpointRefusal::Malformed);
    }
    text.as_bytes().chunks_exact(2).map(|pair| {
        let digit = |v: u8| match v {
            b'0'..=b'9' => Some(v - b'0'), b'a'..=b'f' => Some(v - b'a' + 10),
            b'A'..=b'F' => Some(v - b'A' + 10), _ => None,
        };
        Ok(digit(pair[0]).ok_or(CheckpointRefusal::Malformed)? * 16
            + digit(pair[1]).ok_or(CheckpointRefusal::Malformed)?)
    }).collect()
}

fn field_bytes(value: &serde_json::Value, name: &str) -> Result<Vec<u8>, CheckpointRefusal> {
    unhex(value.get(name).and_then(serde_json::Value::as_str).ok_or(CheckpointRefusal::Malformed)?)
}

fn quantity(value: &serde_json::Value) -> Result<u64, CheckpointRefusal> {
    let text = value.as_str().and_then(|v| v.strip_prefix("0x")).ok_or(CheckpointRefusal::Malformed)?;
    u64::from_str_radix(text, 16).map_err(|_| CheckpointRefusal::Malformed)
}

fn word(value: &[u8], index: usize) -> Result<u64, CheckpointRefusal> {
    let begin = index.checked_mul(32).ok_or(CheckpointRefusal::Malformed)?;
    let bytes = value.get(begin..begin + 32).ok_or(CheckpointRefusal::Malformed)?;
    if bytes[..24].iter().any(|v| *v != 0) { return Err(CheckpointRefusal::Malformed); }
    Ok(u64::from_be_bytes(bytes[24..].try_into().map_err(|_| CheckpointRefusal::Malformed)?))
}

fn native_call(rpc: &crate::rpc::RpcCluster, signature: &str, argument: Option<&[u8; 32]>, block: &str)
    -> Result<Vec<u8>, CheckpointRefusal>
{
    use sha3::{Digest, Keccak256};
    let mut data = Keccak256::digest(signature.as_bytes())[..4].to_vec();
    if let Some(argument) = argument { data.extend_from_slice(argument); }
    let result = rpc.call("eth_call", serde_json::json!([{
        "to": "0x0000000000000000000000000000000000001014", "data": format!("0x{}", hex(&data))
    }, block])).map_err(|_| CheckpointRefusal::Settlement)?;
    unhex(result.as_str().ok_or(CheckpointRefusal::Malformed)?)
}

fn dynamic_argument(input: &[u8], index: usize) -> Result<&[u8], CheckpointRefusal> {
    let offset = usize::try_from(word(input, index)?).map_err(|_| CheckpointRefusal::Malformed)?;
    if offset < 96 || offset % 32 != 0 { return Err(CheckpointRefusal::Malformed); }
    let suffix = input.get(offset..).ok_or(CheckpointRefusal::Malformed)?;
    let length = usize::try_from(word(suffix, 0)?).map_err(|_| CheckpointRefusal::Malformed)?;
    suffix.get(32..32_usize.checked_add(length).ok_or(CheckpointRefusal::Malformed)?)
        .ok_or(CheckpointRefusal::Malformed)
}

fn native_publication(
    verified: &layerx_client::evidence::VerifiedCheckpoint,
    policy: &NativeCheckpointPolicy,
    handshake_key: [u8; 32],
) -> Result<(), CheckpointRefusal> {
    let candidate = layerx_client::evidence::FinalityEvidenceCandidate::from_exact_bytes(
        verified.checkpoint_bytes().to_vec(), verified.context_bytes().to_vec(),
        verified.report().protocol_version(), verified.report().network_id(),
    ).map_err(|_| CheckpointRefusal::CertificateMismatch)?;
    native_candidate_publication(&candidate, policy, handshake_key)
}

fn archival_certificate(verified: &layerx_client::evidence::VerifiedCheckpoint) -> Result<Vec<u8>, CheckpointRefusal> {
    let certificate = verified.certificate().map_err(|_| CheckpointRefusal::CertificateMismatch)?;
    let projection = native_certificate_projection(&certificate)?;
    crate::publisher::wrap_native_certificate(&projection, verified.checkpoint_bytes(), verified.context_bytes())
        .map_err(|_| CheckpointRefusal::Malformed)
}

pub(crate) fn native_candidate_publication(
    verified: &layerx_client::evidence::FinalityEvidenceCandidate,
    policy: &NativeCheckpointPolicy,
    handshake_key: [u8; 32],
) -> Result<(), CheckpointRefusal> {
    let certificate = verified.certificate().map_err(|_| CheckpointRefusal::CertificateMismatch)?;
    let reference = certificate.settlement_reference().ok_or(CheckpointRefusal::Settlement)?;
    let anchor = layerx_client::evidence::ANCHOR_SETTLEMENT_CONTRACT;
    let expected_hash = verified.observed_block_hash().map_err(|_| CheckpointRefusal::Malformed)?.ok_or(CheckpointRefusal::Settlement)?;
    if policy.chain_id == 0 || policy.confirmations == 0 || reference.len() != 110
        || reference[..2] != [0, 1] || reference[10..30] != anchor
        || unhex(&policy.sequencer_public_key_hex)? != handshake_key {
        return Err(CheckpointRefusal::DomainMismatch);
    }
    let u64_at = |begin| -> Result<u64, CheckpointRefusal> {
        Ok(u64::from_be_bytes(reference[begin..begin + 8].try_into().map_err(|_| CheckpointRefusal::Malformed)?))
    };
    if u64_at(2)? != policy.chain_id { return Err(CheckpointRefusal::DomainMismatch); }
    let submission = u64_at(94)?;
    let rpc = crate::rpc::RpcCluster::new(&policy.rpc).map_err(|_| CheckpointRefusal::Settlement)?;
    let call = |method: &str, params| rpc.call(method, params).map_err(|_| CheckpointRefusal::Settlement);
    if quantity(&call("eth_chainId", serde_json::json!([]))?)? != policy.chain_id {
        return Err(CheckpointRefusal::DomainMismatch);
    }
    let genesis = call("eth_getBlockByNumber", serde_json::json!(["0x0", false]))?;
    let genesis_hash = unhex(&policy.genesis_hash_hex)?;
    if genesis_hash.len() != 32 || genesis_hash.iter().all(|v| *v == 0)
        || field_bytes(&genesis, "hash")? != genesis_hash { return Err(CheckpointRefusal::DomainMismatch); }
    let latest = quantity(&call("eth_blockNumber", serde_json::json!([]))?)?;
    let confirmed = latest.checked_sub(policy.confirmations - 1).ok_or(CheckpointRefusal::Settlement)?;
    if submission == 0 || confirmed < submission { return Err(CheckpointRefusal::Settlement); }
    let block = format!("0x{confirmed:x}");
    let head = call("eth_getBlockByNumber", serde_json::json!([block, false]))?;
    let head_hash = field_bytes(&head, "hash")?;
    if head_hash.len() != 32 || head_hash.iter().all(|v| *v == 0) { return Err(CheckpointRefusal::Settlement); }
    let header = decode_batch_header(verified.canonical_header()).map_err(|_| CheckpointRefusal::Malformed)?;
    let identifier = verified.checkpoint_id();
    if reference[30..62] != identifier { return Err(CheckpointRefusal::CertificateMismatch); }
    let mut batch = [0; 32]; batch[24..].copy_from_slice(&header.batch_number().to_be_bytes());
    let record = native_call(&rpc, "checkpoint(uint64)", Some(&batch), &block)?;
    let status = native_call(&rpc, "statusOf(uint64)", Some(&batch), &block)?;
    let header_hash = batch_header_digest(verified.canonical_header()).map_err(|_| CheckpointRefusal::Malformed)?;
    if record.len() != 18 * 32 || status.len() != 32 || word(&status, 0)? != 2
        || word(&record, 0)? != header.batch_number() || record[32..64] != identifier
        || record[64..96] != header_hash || word(&record, 3)? != header.epoch()
        || word(&record, 4)? != header.first_sequence() || word(&record, 5)? != header.last_sequence()
        || record[192..224] != header.previous_state_root() || record[224..256] != header.resulting_state_root()
        || record[256..288] != header.receipt_merkle_root() || record[288..320] != header.data_availability_root()
        || record[320..352] != header.sequencer_id() || word(&record, 11)? != header.timestamp_ms()
        || word(&record, 12)? != 2 || word(&record, 13)? != certificate.attestations().len() as u64
        || word(&record, 14)? != 31 || word(&record, 15)? != 0 || word(&record, 16)? != submission
        || word(&record, 17)? < submission || word(&record, 17)? > confirmed {
        return Err(CheckpointRefusal::CertificateMismatch);
    }
    let signers = native_call(&rpc, "checkpointGuarantors(uint64)", Some(&batch), &block)?;
    if signers.len() != 64 + certificate.attestations().len() * 32 || word(&signers, 0)? != 32
        || word(&signers, 1)? != certificate.attestations().len() as u64 {
        return Err(CheckpointRefusal::SignerMembership);
    }
    for (index, attestation) in certificate.attestations().iter().enumerate() {
        if signers[64 + index * 32..96 + index * 32] != attestation.guarantor_id() {
            return Err(CheckpointRefusal::SignerMembership);
        }
    }
    native_submission(&rpc, verified, handshake_key, submission, expected_hash)?;
    let submitted_block = format!("0x{submission:x}");
    if field_bytes(&call("eth_getBlockByNumber", serde_json::json!([block, false]))?, "hash")? != head_hash
        || field_bytes(&call("eth_getBlockByNumber", serde_json::json!([submitted_block, false]))?, "hash")? != expected_hash {
        return Err(CheckpointRefusal::Settlement);
    }
    Ok(())
}

fn native_submission(
    rpc: &crate::rpc::RpcCluster,
    verified: &layerx_client::evidence::FinalityEvidenceCandidate,
    handshake_key: [u8; 32], submission: u64, expected_hash: [u8; 32],
) -> Result<(), CheckpointRefusal> {
    use sha3::{Digest, Keccak256};
    let call = |method: &str, params| rpc.call(method, params).map_err(|_| CheckpointRefusal::Settlement);
    let certificate = verified.certificate().map_err(|_| CheckpointRefusal::CertificateMismatch)?;
    let reference = certificate.settlement_reference().ok_or(CheckpointRefusal::Settlement)?;
    let anchor = layerx_client::evidence::ANCHOR_SETTLEMENT_CONTRACT;
    let header = decode_batch_header(verified.canonical_header()).map_err(|_| CheckpointRefusal::Malformed)?;
    let header_hash = batch_header_digest(verified.canonical_header()).map_err(|_| CheckpointRefusal::Malformed)?;
    let identifier = verified.checkpoint_id();
    let mut batch = [0; 32]; batch[24..].copy_from_slice(&header.batch_number().to_be_bytes());
    let transaction = format!("0x{}", hex(&reference[62..94]));
    let receipt = call("eth_getTransactionReceipt", serde_json::json!([transaction]))?;
    let tx = call("eth_getTransactionByHash", serde_json::json!([transaction]))?;
    for value in [&receipt, &tx] {
        if field_bytes(value, "to")? != anchor || field_bytes(value, "blockHash")? != expected_hash
            || quantity(&value["blockNumber"])? != submission { return Err(CheckpointRefusal::Settlement); }
    }
    if quantity(&receipt["status"])? != 1 || field_bytes(&receipt, "transactionHash")? != reference[62..94]
        || field_bytes(&tx, "hash")? != reference[62..94] { return Err(CheckpointRefusal::Settlement); }
    let submitted_block = format!("0x{submission:x}");
    let canonical = call("eth_getBlockByNumber", serde_json::json!([submitted_block, false]))?;
    if field_bytes(&canonical, "hash")? != expected_hash
        || quantity(&canonical["timestamp"])?.checked_mul(1000) != Some(u64::from_be_bytes(reference[102..110].try_into().map_err(|_| CheckpointRefusal::Malformed)?)) {
        return Err(CheckpointRefusal::Settlement);
    }
    let input = field_bytes(&tx, "input")?;
    if input.len() < 100 || input[..4] != Keccak256::digest(b"submitCheckpoint(bytes,bytes,bytes)")[..4] {
        return Err(CheckpointRefusal::Settlement);
    }
    let arguments = &input[4..];
    let submitted_header = dynamic_argument(arguments, 0)?;
    let signature: [u8; 64] = dynamic_argument(arguments, 1)?.try_into().map_err(|_| CheckpointRefusal::Signature)?;
    if submitted_header != verified.canonical_header() { return Err(CheckpointRefusal::CertificateMismatch); }
    ed25519::verify_digest(&handshake_key, &signature, &header_hash).map_err(|_| CheckpointRefusal::Signature)?;
    let payload = verified.checkpoint_bytes();
    let end = payload.len().checked_sub(reference.len() + 2).ok_or(CheckpointRefusal::Malformed)?;
    let mut submitted_certificate = payload[..end].to_vec(); submitted_certificate.extend_from_slice(&[0, 0]);
    if dynamic_argument(arguments, 2)? != submitted_certificate { return Err(CheckpointRefusal::CertificateMismatch); }
    let threshold = native_call(rpc, "threshold()", None, &submitted_block)?;
    if threshold.len() != 32 || word(&threshold, 0)? != certificate.threshold() as u64 {
        return Err(CheckpointRefusal::Threshold);
    }
    for attestation in certificate.attestations() {
        let member = native_call(rpc, "guarantor(bytes32)", Some(&attestation.guarantor_id()), &submitted_block)?;
        if member.len() != 224 || member[..32] != attestation.guarantor_id()
            || member[32..44].iter().any(|v| *v != 0) || member[44..64] != attestation.signer()
            || member[96..128].iter().all(|v| *v == 0) || word(&member, 5)? != 2 || word(&member, 6)? != 1 {
            return Err(CheckpointRefusal::SignerMembership);
        }
    }
    let topic = Keccak256::digest(b"CheckpointSubmitted(uint64,bytes32,bytes32,bytes32,uint8)");
    let logs = receipt["logs"].as_array().ok_or(CheckpointRefusal::Malformed)?;
    let mut matches = 0;
    for log in logs {
        if field_bytes(log, "address")? != anchor { continue; }
        let topics = log["topics"].as_array().ok_or(CheckpointRefusal::Malformed)?;
        if topics.first().and_then(serde_json::Value::as_str).map(unhex).transpose()?.as_deref() != Some(topic.as_slice()) { continue; }
        if topics.len() != 3 || unhex(topics[1].as_str().ok_or(CheckpointRefusal::Malformed)?)? != batch
            || unhex(topics[2].as_str().ok_or(CheckpointRefusal::Malformed)?)? != identifier
            || log["removed"] != false || field_bytes(log, "transactionHash")? != reference[62..94]
            || field_bytes(log, "blockHash")? != expected_hash || quantity(&log["blockNumber"])? != submission {
            return Err(CheckpointRefusal::Settlement);
        }
        let data = field_bytes(log, "data")?;
        if data.len() != 96 || data[..32] != header.resulting_state_root() || data[32..64] != header.receipt_merkle_root()
            || word(&data, 2)? != certificate.attestations().len() as u64 { return Err(CheckpointRefusal::Settlement); }
        matches += 1;
    }
    if matches != 1 { return Err(CheckpointRefusal::Settlement); }
    Ok(())
}

pub(crate) fn native_certificate_projection(certificate: &layerx_proof::checkpoint::Certificate) -> Result<Vec<u8>, CheckpointRefusal> {
    let mut bytes = certificate.checkpoint().header_bytes().to_vec();
    let append = |output: &mut Vec<u8>, value: &[u8]| -> Result<(), CheckpointRefusal> {
        output.extend_from_slice(&u32::try_from(value.len()).map_err(|_| CheckpointRefusal::Malformed)?.to_be_bytes());
        output.extend_from_slice(value); Ok(())
    };
    append(&mut bytes, certificate.checkpoint().validity_proof())?;
    bytes.extend_from_slice(&u32::try_from(certificate.attestations().len()).map_err(|_| CheckpointRefusal::Malformed)?.to_be_bytes());
    for attestation in certificate.attestations() {
        append(&mut bytes, &attestation.guarantor_id())?;
        append(&mut bytes, &attestation.signature())?;
    }
    bytes.extend_from_slice(&u32::try_from(certificate.threshold()).map_err(|_| CheckpointRefusal::Malformed)?.to_be_bytes());
    append(&mut bytes, certificate.settlement_reference().ok_or(CheckpointRefusal::Settlement)?)?;
    let decoded = layerx_wire::receipt::decode_checkpoint(&bytes).map_err(|_| CheckpointRefusal::Malformed)?;
    if layerx_wire::receipt::encode_checkpoint(&decoded).map_err(|_| CheckpointRefusal::Malformed)? != bytes {
        return Err(CheckpointRefusal::Malformed);
    }
    Ok(bytes)
}
