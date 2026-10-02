//! Construction of proof-only offline verification exports.

use std::collections::BTreeMap;
use std::sync::Mutex;
use std::time::Duration;

use layerx_agent_api::export::{FactRef, OfflineExport};
use layerx_agent_api::prepare::CanonicalBytes;
use layerx_agent_api::read::{BatchRef, CheckpointRef, Freshness, RelativeTo, VerifiedRead};
use layerx_agent_api::verify::Level;
use layerx_agent_api::Sequence;
use layerx_client::availability::{
    AvailabilitySelector, FetchContext, FetchError, FetchOutcome, RetrievalLimits,
};
use layerx_client::evidence::{
    AccountProofVariant, CheckpointSelector, EvidenceError, ProofBundleSelector, SignedHeader,
    VerifiedProofBundle,
};
use layerx_client::Client;
use layerx_programs::hex;
use layerx_proof::availability::RootCommitments;
use layerx_proof::checkpoint::SettlementDomain;
use layerx_proof::export::{
    verify, verify_complete, CompleteExportError, ExportVerificationError,
    IndependentOfflineTrust, OfflineExport as LegacyOfflineExport, OfflineTrustError,
    TrustedCheckpointMembership, VerificationReport,
};
use layerx_proof::settlement::DeclaredDomain;
use layerx_proof::signed_authority::SignedAuthorityHistory;
use layerx_proof::export_codec::{
    parse_fact_set, AccountStateRecord, AccountStateVariant, CheckpointRecord,
    ArtifactDecodeError, CompleteOfflineArtifact, ExportCodecError, FactRefError, FactSelector, HeaderRecord,
    InclusionRecord, InclusionRecordKind, ProofRecord, ReceiptRecord, MAX_AVAILABILITY_CHUNKS,
};
use layerx_types::payload::ModuleRegistry;
use layerx_types::verify::VerificationLevel;
use layerx_wire::batch_maintenance::decode_maintenance;
use layerx_wire::receipt::{decode_batch_header, BatchHeader};

use crate::protocol_evidence::{
    EvidenceAuthority, ExportReceiptEvidence, RawReceiptEvidence, ReceiptEvidenceError,
};
use crate::receipt::{serve, ReceiptLookupKey, ReceiptStoreError};
use crate::store::{Store, TenantId};

/// Binary and complete JSON response bound; the existing agent RPC body limit.
pub const MAX_EXPORT_BYTES: usize = crate::agent_rpc::MAX_BODY_BYTES;

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct BuiltExport {
    pub artifact: LegacyOfflineExport,
    pub local_verification: VerificationReport,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub enum ExportBuildError {
    Empty,
    Verification(ExportVerificationError),
}

/// Builds an export only from proof-verifiable core evidence and labelled local views.
///
/// # Errors
///
/// Refuses an artifact holding no receipts, inclusions or checkpoints, and returns the first
/// verification failure raised by `layerx-proof` over the artifact.
pub fn build(
    artifact: LegacyOfflineExport,
    expected_settlement_domain: SettlementDomain,
) -> Result<BuiltExport, ExportBuildError> {
    if artifact.receipts.is_empty()
        && artifact.inclusions.is_empty()
        && artifact.checkpoints.is_empty()
    {
        return Err(ExportBuildError::Empty);
    }
    let local_verification =
        verify(&artifact, expected_settlement_domain).map_err(ExportBuildError::Verification)?;
    Ok(BuiltExport {
        artifact,
        local_verification,
    })
}

/// Independently configured deployment trust injected into the owner at boot:
/// genesis-anchored signed sequencer authority, the explicitly selected
/// declared settlement domain and the checkpoint membership version it pins.
#[derive(Debug)]
pub struct ExportTrustSource {
    authority: SignedAuthorityHistory,
    domain: DeclaredDomain,
    set_version: u64,
}

impl ExportTrustSource {
    /// # Errors
    /// Refuses a domain whose network differs from the authority's or whose
    /// membership at `set_version` is not a valid trusted membership.
    pub fn new(
        authority: SignedAuthorityHistory,
        domain: DeclaredDomain,
        set_version: u64,
    ) -> Result<Self, OfflineTrustError> {
        if authority.network_id() != domain.network_id() {
            return Err(OfflineTrustError::NetworkId);
        }
        TrustedCheckpointMembership::from_declared(&domain, set_version)?;
        Ok(Self {
            authority,
            domain,
            set_version,
        })
    }

    /// Binds this trust to the authenticated module registry of one request.
    ///
    /// # Errors
    /// Returns the trust-context construction refusal.
    pub fn trust(&self, registry: ModuleRegistry) -> Result<IndependentOfflineTrust, OfflineTrustError> {
        IndependentOfflineTrust::from_declared_domain(
            registry,
            self.authority.clone(),
            &self.domain,
            self.set_version,
        )
    }
}

/// Owner-held inputs of one complete export. Trust is the owner's independently
/// configured deployment trust; no request field selects it.
pub struct ExportOwnerContext<'a> {
    pub store: &'a Mutex<Store>,
    pub tenant: &'a TenantId,
    /// Account bound to the authenticated subject of the request, if any.
    pub bound_account: Option<[u8; 32]>,
    pub registry: &'a ModuleRegistry,
    pub authority: &'a EvidenceAuthority,
    pub trust: &'a IndependentOfflineTrust,
    /// First nonzero correlation identifier; each node read advances it by two.
    pub first_correlation: u64,
    pub availability_deadline: Duration,
}

/// A complete export verified offline under independent trust before release.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ProducedExport {
    pub response: VerifiedRead<OfflineExport>,
    /// Exact canonical record and fact bytes.
    pub binary_bytes: usize,
    /// Exact serialized length of the export value object.
    pub json_value_bytes: usize,
}

#[derive(Debug)]
pub enum ExportProduceError {
    Facts(FactRefError),
    SettlementAnchoringUnavailable,
    NotOwned { fact: usize },
    AccountNotBound { fact: usize },
    Store,
    Correlation,
    Evidence { fact: usize, error: EvidenceError },
    ReceiptEvidence { fact: usize, error: ReceiptEvidenceError },
    Binding { fact: usize },
    MaintenanceLink { fact: usize },
    Availability { fact: usize, error: FetchError },
    AvailabilityIncomplete { fact: usize },
    Codec(ExportCodecError),
    Artifact(ArtifactDecodeError),
    ConflictingRecord,
    MixedSnapshot,
    StaleHead,
    /// Explicit bounded-response refusal: the complete export exceeds the frame bound.
    Oversize,
    Verification(CompleteExportError),
    Contract,
}

/// Refuses a complete serialized response frame above the existing bound. The
/// transport calls this on the exact bytes it would transmit.
///
/// # Errors
/// Returns `Oversize` when the frame exceeds [`MAX_EXPORT_BYTES`].
pub const fn require_frame_within_bound(
    serialized_frame_bytes: usize,
) -> Result<(), ExportProduceError> {
    if serialized_frame_bytes > MAX_EXPORT_BYTES {
        Err(ExportProduceError::Oversize)
    } else {
        Ok(())
    }
}

/// Produces every requested fact from tenant-owned anchors and verified node
/// evidence, verifies the complete artifact offline under the owner's
/// independent trust and returns it only when every fact reaches `requested`.
///
/// # Errors
/// Refuses on the first ownership, evidence, binding, availability, snapshot,
/// size or verification failure; no partial export is ever returned.
pub fn produce(
    node: &mut Client,
    context: &ExportOwnerContext<'_>,
    facts: &[FactRef],
    requested: VerificationLevel,
) -> Result<ProducedExport, ExportProduceError> {
    if VerificationLevel::SETTLEMENT_ANCHORED <= requested {
        return Err(ExportProduceError::SettlementAnchoringUnavailable);
    }
    let texts: Vec<&str> = facts.iter().map(FactRef::as_str).collect();
    let selectors = parse_fact_set(&texts).map_err(ExportProduceError::Facts)?;
    let served = owned_anchors(context, &selectors)?;
    let head = node.head();
    let mut producer = Producer {
        node,
        context,
        served,
        correlation: context.first_correlation,
        collected: Collected::default(),
        dependencies: BTreeMap::new(),
    };
    producer.collected.binary = selectors.iter().try_fold(0_usize, |total, selector| {
        checked_total(total, selector.canonical_text().len())
    })?;
    let mut primary_headers = Vec::with_capacity(selectors.len());
    for (fact, selector) in selectors.iter().enumerate() {
        primary_headers.push(producer.fact(fact, *selector)?);
    }
    let Producer { node, collected, .. } = producer;
    if node.head() != head {
        return Err(ExportProduceError::StaleHead);
    }
    let common = primary_headers
        .first()
        .copied()
        .ok_or(ExportProduceError::Contract)?;
    if primary_headers.iter().any(|digest| *digest != common) {
        return Err(ExportProduceError::MixedSnapshot);
    }
    let common_header = collected
        .headers
        .get(&common)
        .map(|(record, _)| decode_batch_header(&record.canonical_header))
        .ok_or(ExportProduceError::Contract)?
        .map_err(|_| ExportProduceError::Contract)?;
    if common_header.batch_number() > head.sealed_batch
        || common_header.last_sequence() > head.chain_sequence
    {
        return Err(ExportProduceError::MixedSnapshot);
    }
    let certified_checkpoint = collected.certified.get(&common).copied();
    let artifact = CompleteOfflineArtifact {
        facts: selectors,
        receipts: collected.receipts.into_values().map(|(record, _)| record).collect(),
        proofs: collected.proofs.into_values().map(|(record, _)| record).collect(),
        certificates: collected
            .certificates
            .into_values()
            .map(|(record, _)| record)
            .collect(),
        headers: collected.headers.into_values().map(|(record, _)| record).collect(),
    };
    let report = verify_complete(&artifact, context.trust, requested)
        .map_err(ExportProduceError::Verification)?;
    let achieved = report.achieved();
    let encoded = artifact.encode().map_err(ExportProduceError::Artifact)?;
    let buckets = [
        &encoded.receipts,
        &encoded.proofs,
        &encoded.certificates,
        &encoded.headers,
    ];
    let mut binary_bytes = encoded
        .facts
        .iter()
        .try_fold(0_usize, |total, text| checked_total(total, text.len()))?;
    for bucket in buckets {
        for record in bucket {
            binary_bytes = checked_total(binary_bytes, record.len())?;
        }
    }
    let json_value_bytes = json_value_bytes(&encoded.facts, buckets)?;
    let relative_to = match certified_checkpoint {
        Some(checkpoint_id) if VerificationLevel::CHECKPOINT_FINALISED <= achieved => {
            RelativeTo::Checkpoint(
                CheckpointRef::new(hex::encode(&checkpoint_id))
                    .map_err(|_| ExportProduceError::Contract)?,
            )
        }
        _ => RelativeTo::Batch(
            BatchRef::new(common_header.batch_number().to_string())
                .map_err(|_| ExportProduceError::Contract)?,
        ),
    };
    let freshness = Freshness {
        chain_head: Sequence(head.chain_sequence),
        latest_sealed_batch: BatchRef::new(head.sealed_batch.to_string())
            .map_err(|_| ExportProduceError::Contract)?,
        latest_finalised_checkpoint: CheckpointRef::new(hex::encode(&head.finalised_checkpoint))
            .map_err(|_| ExportProduceError::Contract)?,
        value_sequence: Sequence(common_header.last_sequence()),
        relative_to,
    };
    let value = OfflineExport {
        facts: encoded
            .facts
            .into_iter()
            .map(FactRef::new)
            .collect::<Result<_, _>>()
            .map_err(|_| ExportProduceError::Contract)?,
        receipts: canonical(encoded.receipts)?,
        proofs: canonical(encoded.proofs)?,
        certificates: canonical(encoded.certificates)?,
        headers: canonical(encoded.headers)?,
    }
    .validate()
    .map_err(|_| ExportProduceError::Contract)?;
    Ok(ProducedExport {
        response: VerifiedRead::new(value, Level::from(achieved), freshness),
        binary_bytes,
        json_value_bytes,
    })
}

/// D8: every fact's anchor must be a tenant-served activity and every state
/// fact's account the subject-bound account, before any evidence lookup. The
/// store lock is released before node I/O.
fn owned_anchors(
    context: &ExportOwnerContext<'_>,
    selectors: &[FactSelector],
) -> Result<BTreeMap<[u8; 32], Vec<u8>>, ExportProduceError> {
    let store = context
        .store
        .lock()
        .map_err(|_| ExportProduceError::Store)?;
    let mut served = BTreeMap::new();
    for (fact, selector) in selectors.iter().enumerate() {
        if let FactSelector::State { account_id, .. } = selector {
            if context.bound_account != Some(*account_id) {
                return Err(ExportProduceError::AccountNotBound { fact });
            }
        }
        let anchor = selector.activity_id();
        if served.contains_key(&anchor) {
            continue;
        }
        let receipt = serve(
            &store,
            context.tenant.clone(),
            ReceiptLookupKey::Activity(anchor),
        )
        .map_err(|error| match error {
            ReceiptStoreError::Missing => ExportProduceError::NotOwned { fact },
            _ => ExportProduceError::Store,
        })?;
        if receipt.metadata.activity_id != anchor {
            return Err(ExportProduceError::NotOwned { fact });
        }
        served.insert(anchor, receipt.canonical_bytes);
    }
    Ok(served)
}

#[derive(Default)]
struct Collected {
    receipts: BTreeMap<FactSelector, (ReceiptRecord, Vec<u8>)>,
    proofs: BTreeMap<(u8, FactSelector), (ProofRecord, Vec<u8>)>,
    certificates: BTreeMap<FactSelector, (CheckpointRecord, Vec<u8>)>,
    headers: BTreeMap<[u8; 32], (HeaderRecord, Vec<u8>)>,
    /// Signed-header digest to the identifier of the checkpoint certifying exactly that header.
    certified: BTreeMap<[u8; 32], [u8; 32]>,
    binary: usize,
}

/// Inserts one record keyed by its bound identity; an identical record is
/// shared, a differing record under the same identity refuses the export.
fn admit<K: Ord, R>(
    map: &mut BTreeMap<K, (R, Vec<u8>)>,
    binary: &mut usize,
    key: K,
    record: R,
    bytes: Vec<u8>,
) -> Result<(), ExportProduceError> {
    if let Some((_, existing)) = map.get(&key) {
        return if *existing == bytes {
            Ok(())
        } else {
            Err(ExportProduceError::ConflictingRecord)
        };
    }
    *binary = checked_total(*binary, bytes.len())?;
    map.insert(key, (record, bytes));
    Ok(())
}

fn checked_total(total: usize, length: usize) -> Result<usize, ExportProduceError> {
    total
        .checked_add(length)
        .filter(|sum| *sum <= MAX_EXPORT_BYTES)
        .ok_or(ExportProduceError::Oversize)
}

/// Exact length of `{"facts":[..],"receipts":[..],"proofs":[..],"certificates":[..],"headers":[..]}`
/// with facts as their grammar text (no escapes exist in the grammar) and
/// records as lowercase hex strings.
fn json_value_bytes(
    facts: &[String],
    buckets: [&Vec<Vec<u8>>; 4],
) -> Result<usize, ExportProduceError> {
    fn array(
        total: usize,
        key: &str,
        lengths: impl Iterator<Item = Option<usize>>,
    ) -> Result<usize, ExportProduceError> {
        // "key":[ ... ]
        let mut total = checked_total(total, key.len() + 5)?;
        let mut first = true;
        for length in lengths {
            let element = length.ok_or(ExportProduceError::Oversize)?;
            total = checked_total(total, element)?;
            total = checked_total(total, 2 + usize::from(!first))?;
            first = false;
        }
        Ok(total)
    }
    if facts.iter().any(|text| {
        !text
            .bytes()
            .all(|byte| byte.is_ascii_graphic() && byte != b'"' && byte != b'\\')
    }) {
        return Err(ExportProduceError::Contract);
    }
    // braces plus four separating commas
    let mut total = 2 + 4;
    total = array(total, "facts", facts.iter().map(|text| Some(text.len())))?;
    for (key, bucket) in ["receipts", "proofs", "certificates", "headers"]
        .into_iter()
        .zip(buckets)
    {
        total = array(total, key, bucket.iter().map(|record| record.len().checked_mul(2)))?;
    }
    Ok(total)
}

fn canonical(records: Vec<Vec<u8>>) -> Result<Vec<CanonicalBytes>, ExportProduceError> {
    records
        .into_iter()
        .map(CanonicalBytes::new)
        .collect::<Result<_, _>>()
        .map_err(|_| ExportProduceError::Contract)
}

fn header_record(signed: &SignedHeader) -> HeaderRecord {
    HeaderRecord {
        canonical_header: signed.canonical_bytes.clone(),
        signature: signed.signature,
        sequencer_id: signed.sequencer_id,
        public_key: signed.public_key,
        first_batch: signed.first_batch_number,
        last_batch: signed.last_batch_number,
    }
}

struct ReceiptDependency {
    evidence: ExportReceiptEvidence,
    signed_header: SignedHeader,
    header_digest: [u8; 32],
}

struct Producer<'n, 'c, 'a> {
    node: &'n mut Client,
    context: &'c ExportOwnerContext<'a>,
    served: BTreeMap<[u8; 32], Vec<u8>>,
    correlation: u64,
    collected: Collected,
    dependencies: BTreeMap<[u8; 32], ReceiptDependency>,
}

impl Producer<'_, '_, '_> {
    fn next_correlation(&mut self) -> Result<u64, ExportProduceError> {
        let current = self.correlation;
        if current == 0 {
            return Err(ExportProduceError::Correlation);
        }
        self.correlation = current
            .checked_add(2)
            .ok_or(ExportProduceError::Correlation)?;
        Ok(current)
    }

    fn header(&mut self, signed: &SignedHeader) -> Result<[u8; 32], ExportProduceError> {
        let record = header_record(signed);
        let digest = record.digest().map_err(ExportProduceError::Codec)?;
        let bytes = record.encode().map_err(ExportProduceError::Codec)?;
        admit(
            &mut self.collected.headers,
            &mut self.collected.binary,
            digest,
            record,
            bytes,
        )?;
        Ok(digest)
    }

    fn proof(&mut self, kind: u8, reference: FactSelector, record: ProofRecord) -> Result<(), ExportProduceError> {
        let bytes = record.encode().map_err(ExportProduceError::Codec)?;
        admit(
            &mut self.collected.proofs,
            &mut self.collected.binary,
            (kind, reference),
            record,
            bytes,
        )
    }

    /// Produces one fact and returns the digest of its primary signed header.
    fn fact(&mut self, fact: usize, selector: FactSelector) -> Result<[u8; 32], ExportProduceError> {
        match selector {
            FactSelector::Receipt { activity_id } => {
                Ok(self.receipt_dependency(fact, activity_id)?.header_digest)
            }
            FactSelector::Activity { activity_id } => self.activity(fact, activity_id),
            FactSelector::State {
                activity_id,
                account_id,
            } => self.state(fact, activity_id, account_id),
            FactSelector::Checkpoint {
                activity_id,
                batch_number,
            } => self.checkpoint(fact, activity_id, batch_number),
        }
    }

    /// Exact tenant-served receipt bytes re-read as a receipt proof bundle and
    /// verified under daemon authority, which establishes the authorised batch.
    fn receipt_dependency(
        &mut self,
        fact: usize,
        activity_id: [u8; 32],
    ) -> Result<&ReceiptDependency, ExportProduceError> {
        if !self.dependencies.contains_key(&activity_id) {
            let correlation = self.next_correlation()?;
            let bundle = self
                .node
                .proof_bundle(
                    ProofBundleSelector::Receipt(activity_id),
                    correlation,
                    self.context.registry,
                )
                .map_err(|error| ExportProduceError::Evidence { fact, error })?;
            let VerifiedProofBundle::Receipt {
                canonical_bytes,
                activity_id: bundle_activity,
                proof,
                signed_header,
            } = bundle
            else {
                return Err(ExportProduceError::Binding { fact });
            };
            if bundle_activity != activity_id
                || self.served.get(&activity_id).map(Vec::as_slice) != Some(canonical_bytes.as_slice())
            {
                return Err(ExportProduceError::Binding { fact });
            }
            let raw = RawReceiptEvidence::new(
                canonical_bytes,
                proof,
                signed_header.canonical_bytes.clone(),
                signed_header.signature,
            );
            let evidence = self
                .context
                .authority
                .verify_receipt_for_export(&raw)
                .map_err(|error| ExportProduceError::ReceiptEvidence { fact, error })?;
            if evidence.verified().activity_id() != activity_id {
                return Err(ExportProduceError::Binding { fact });
            }
            let header_digest = self.header(&signed_header)?;
            let reference = FactSelector::Receipt { activity_id };
            let receipt = ReceiptRecord {
                reference,
                canonical_receipt: raw.canonical_receipt().to_vec(),
                authorised_batch: *evidence.authorized_batch(),
                expected_receipt_digest: evidence
                    .receipt_digest()
                    .ok_or(ExportProduceError::Binding { fact })?,
            };
            let bytes = receipt.encode().map_err(ExportProduceError::Codec)?;
            admit(
                &mut self.collected.receipts,
                &mut self.collected.binary,
                reference,
                receipt,
                bytes,
            )?;
            self.proof(
                InclusionRecordKind::Receipt as u8,
                reference,
                ProofRecord::Inclusion(InclusionRecord {
                    kind: InclusionRecordKind::Receipt,
                    reference,
                    canonical_leaf: raw.canonical_receipt().to_vec(),
                    proof: raw.proof().clone(),
                    header_digest,
                }),
            )?;
            self.dependencies.insert(
                activity_id,
                ReceiptDependency {
                    evidence,
                    signed_header,
                    header_digest,
                },
            );
        }
        self.dependencies
            .get(&activity_id)
            .ok_or(ExportProduceError::Contract)
    }

    fn activity(&mut self, fact: usize, activity_id: [u8; 32]) -> Result<[u8; 32], ExportProduceError> {
        let correlation = self.next_correlation()?;
        let bundle = self
            .node
            .proof_bundle(
                ProofBundleSelector::Activity(activity_id),
                correlation,
                self.context.registry,
            )
            .map_err(|error| ExportProduceError::Evidence { fact, error })?;
        let VerifiedProofBundle::Activity {
            canonical_bytes,
            activity_id: bundle_activity,
            proof,
            signed_header,
        } = bundle
        else {
            return Err(ExportProduceError::Binding { fact });
        };
        if bundle_activity != activity_id {
            return Err(ExportProduceError::Binding { fact });
        }
        let header_digest = self.header(&signed_header)?;
        let reference = FactSelector::Activity { activity_id };
        self.proof(
            InclusionRecordKind::Activity as u8,
            reference,
            ProofRecord::Inclusion(InclusionRecord {
                kind: InclusionRecordKind::Activity,
                reference,
                canonical_leaf: canonical_bytes,
                proof,
                header_digest,
            }),
        )?;
        Ok(header_digest)
    }

    fn state(
        &mut self,
        fact: usize,
        activity_id: [u8; 32],
        account_id: [u8; 32],
    ) -> Result<[u8; 32], ExportProduceError> {
        let correlation = self.next_correlation()?;
        let node_info = self.node.handshake().node();
        let (protocol_version, network_id) = (node_info.protocol_version, node_info.network_id);
        let bundle = self
            .node
            .proof_bundle(
                ProofBundleSelector::AccountState {
                    activity_id,
                    account_id,
                },
                correlation,
                self.context.registry,
            )
            .map_err(|error| ExportProduceError::Evidence { fact, error })?;
        let material = bundle
            .account_proof(account_id, protocol_version, network_id)
            .map_err(|error| ExportProduceError::Evidence { fact, error })?;
        if material.activity_id != activity_id {
            return Err(ExportProduceError::Binding { fact });
        }
        let signed_header = bundle.signed_header().clone();
        let header_digest = self.header(&signed_header)?;
        let variant = match material.variant {
            AccountProofVariant::Activity => AccountStateVariant::Activity,
            AccountProofVariant::Maintenance {
                activity_count,
                parameter_version,
                activity_receipt,
                activity_receipt_proof,
            } => {
                let maintenance = decode_maintenance(&material.proof.receipt_bytes)
                    .map_err(|_| ExportProduceError::MaintenanceLink { fact })?;
                let occupancy = maintenance.occupancy();
                let (global_sequence, previous_state_root) =
                    (occupancy.global_sequence, occupancy.previous_state_root);
                let dependency = self.receipt_dependency(fact, activity_id)?;
                if dependency.signed_header != signed_header
                    || dependency.header_digest != header_digest
                    || dependency.evidence.raw().canonical_receipt() != activity_receipt.as_slice()
                    || dependency.evidence.raw().proof() != &activity_receipt_proof
                    || dependency.evidence.verified().activity_id() != activity_id
                    || dependency.evidence.verified().global_sequence().checked_add(1)
                        != Some(global_sequence)
                    || dependency.evidence.resulting_state_root() != Some(previous_state_root)
                {
                    return Err(ExportProduceError::MaintenanceLink { fact });
                }
                AccountStateVariant::Maintenance {
                    activity_count,
                    parameter_version,
                    dependency_receipt_activity_id: activity_id,
                }
            }
        };
        let proof = material.proof;
        let reference = FactSelector::State {
            activity_id,
            account_id,
        };
        self.proof(
            3,
            reference,
            ProofRecord::AccountState(AccountStateRecord {
                reference,
                variant,
                account_id,
                account_value: bundle.canonical_bytes().to_vec(),
                account_root: proof.account_root,
                universal_root: proof.universal_root,
                resulting_state_root: proof.resulting_state_root,
                account_proof: proof.account_proof,
                account_tree_proof: proof.account_tree_proof,
                universal_root_proof: proof.universal_root_proof,
                receipt_bytes: proof.receipt_bytes,
                receipt_proof: proof.receipt_proof,
                header_digest,
            }),
        )?;
        Ok(header_digest)
    }

    fn checkpoint(
        &mut self,
        fact: usize,
        activity_id: [u8; 32],
        batch_number: u64,
    ) -> Result<[u8; 32], ExportProduceError> {
        let dependency = self.receipt_dependency(fact, activity_id)?;
        let header_bytes = dependency.signed_header.canonical_bytes.clone();
        let header_digest = dependency.header_digest;
        let header: BatchHeader =
            decode_batch_header(&header_bytes).map_err(|_| ExportProduceError::Binding { fact })?;
        if header.batch_number() != batch_number {
            return Err(ExportProduceError::Binding { fact });
        }
        let correlation = self.next_correlation()?;
        let verified = self
            .node
            .checkpoint_evidence(CheckpointSelector::Batch(batch_number), correlation)
            .map_err(|error| ExportProduceError::Evidence { fact, error })?;
        if verified.canonical_header() != header_bytes.as_slice()
            || verified.report().batch_number() != batch_number
        {
            return Err(ExportProduceError::Binding { fact });
        }
        let certificate = verified
            .certificate()
            .map_err(|error| ExportProduceError::Evidence { fact, error })?;
        let (bonded_set, _) = verified
            .bonded_keys()
            .map_err(|error| ExportProduceError::Evidence { fact, error })?;
        let checkpoint_id = verified
            .report()
            .evidence()
            .checkpoint_id()
            .ok_or(ExportProduceError::Binding { fact })?;
        let registered_settlement_reference = verified
            .report()
            .evidence()
            .settlement_reference()
            .map(<[u8]>::to_vec);
        let remaining = MAX_EXPORT_BYTES
            .checked_sub(self.collected.binary)
            .filter(|remaining| *remaining > 0)
            .ok_or(ExportProduceError::Oversize)?;
        let correlation = self.next_correlation()?;
        let fetch = FetchContext {
            interface_version: self.node.handshake().node().interface_version,
            correlation_id: correlation,
            expected_batch_number: batch_number,
            data_availability_root: header.data_availability_root(),
            record_roots: RootCommitments {
                activity: header.activity_merkle_root(),
                receipt: header.receipt_merkle_root(),
                event: header.event_merkle_root(),
                oracle: header.oracle_root(),
            },
            limits: RetrievalLimits {
                maximum_bytes: remaining,
                maximum_chunks: MAX_AVAILABILITY_CHUNKS,
                deadline: self.context.availability_deadline,
            },
        };
        let outcome = self
            .node
            .fetch_availability(AvailabilitySelector::Checkpoint(checkpoint_id), fetch, |_| {})
            .map_err(|error| ExportProduceError::Availability { fact, error })?;
        let FetchOutcome::Complete(result) = outcome else {
            return Err(ExportProduceError::AvailabilityIncomplete { fact });
        };
        if result.batch_number() != batch_number
            || result.data_availability_root() != header.data_availability_root()
            || result.record_roots() != fetch.record_roots
        {
            return Err(ExportProduceError::AvailabilityIncomplete { fact });
        }
        let availability = result
            .chunks
            .iter()
            .map(|chunk| (chunk.chunk().clone(), chunk.proof().clone()))
            .collect();
        let reference = FactSelector::Checkpoint {
            activity_id,
            batch_number,
        };
        let record = CheckpointRecord {
            reference,
            certificate,
            set_version: verified.set_version(),
            bonded_set,
            checkpoint_id,
            registered_settlement_reference,
            availability,
        };
        let bytes = record.encode().map_err(ExportProduceError::Codec)?;
        admit(
            &mut self.collected.certificates,
            &mut self.collected.binary,
            reference,
            record,
            bytes,
        )?;
        if let Some(existing) = self.collected.certified.insert(header_digest, checkpoint_id) {
            if existing != checkpoint_id {
                return Err(ExportProduceError::ConflictingRecord);
            }
        }
        Ok(header_digest)
    }
}
