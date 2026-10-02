//! Self-contained proof objects for offline third-party verification.

use std::collections::{BTreeMap, BTreeSet};

use layerx_types::payload::ModuleRegistry;
use layerx_types::verify::VerificationLevel;
use layerx_wire::activity::{decode_signed, encode_signed};
use layerx_wire::batch_maintenance::decode_maintenance;
use layerx_wire::handover::sequencer_id;
use layerx_wire::hash::{activity_id as hash_activity_id, receipt_execution_batch_id};
use layerx_wire::receipt::decode as decode_receipt;

use crate::availability::{reassemble, verify_chunk, AvailabilityFailure, Chunk, RootCommitments};
use crate::checkpoint::{
    verify_certificate, Certificate, CheckpointError, GuarantorKey, SettlementDomain,
};
use crate::export_codec::{
    check_fact_selectors, AccountStateVariant, CompleteOfflineArtifact, FactRefError, FactSelector,
    InclusionRecord, InclusionRecordKind, ProofRecord, MAX_CHECKPOINT_GUARANTORS,
};
use crate::inclusion::{
    verify_activity, verify_receipt, verify_state, InclusionError, SequencerAuthorization,
    VerifiedBatchHeader,
};
use crate::merkle::Proof;
use crate::receipt::{verify_outcome, AuthorizedBatch, ReceiptCheck};
use crate::settlement::DeclaredDomain;
use crate::signed_authority::SignedAuthorityHistory;
use crate::state::{
    verify_nested_account, verify_nested_account_maintenance, AccountProofError, NestedAccountProof,
};

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ReceiptFact {
    pub statement: String,
    pub canonical_receipt_bytes: Vec<u8>,
    pub authorised_batch: AuthorizedBatch,
    pub expected_receipt_digest: [u8; 32],
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum InclusionKind {
    Activity,
    State,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct InclusionFact {
    pub statement: String,
    pub kind: InclusionKind,
    pub canonical_leaf_bytes: Vec<u8>,
    pub proof: Proof,
    pub named_root: [u8; 32],
    pub canonical_header_bytes: Vec<u8>,
    pub header_signature: [u8; 64],
    pub sequencer_authorization: SequencerAuthorization,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct CheckpointFact {
    pub statement: String,
    pub certificate: Certificate,
    pub bonded_set: Vec<GuarantorKey>,
    pub registered_checkpoint_id: [u8; 32],
    pub registered_settlement_reference: Option<Vec<u8>>,
    pub availability_obtained: bool,
    pub availability: Vec<(Chunk, Proof)>,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct DerivedAggregate {
    pub label: String,
    pub rendered_value: String,
    pub contributing_receipt_digests: Vec<[u8; 32]>,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct OfflineExport {
    pub receipts: Vec<ReceiptFact>,
    pub inclusions: Vec<InclusionFact>,
    pub checkpoints: Vec<CheckpointFact>,
    pub derived_aggregates: Vec<DerivedAggregate>,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct VerificationReport {
    pub verified_receipts: usize,
    pub verified_inclusions: usize,
    pub verified_checkpoints: usize,
    pub receipt_digests: Vec<[u8; 32]>,
    pub achieved_levels: Vec<VerificationLevel>,
    pub derived_aggregates_are_protocol_facts: bool,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub enum ExportVerificationError {
    EmptyStatement,
    Receipt {
        index: usize,
        check: ReceiptCheck,
    },
    ReceiptDigest {
        index: usize,
    },
    Inclusion {
        index: usize,
        error: InclusionError,
    },
    InclusionRoot {
        index: usize,
    },
    CheckpointUnavailable {
        index: usize,
    },
    Availability {
        index: usize,
        error: Box<AvailabilityFailure>,
    },
    Checkpoint {
        index: usize,
        error: CheckpointError,
    },
    UnknownAggregateContributor {
        aggregate: usize,
        digest: [u8; 32],
    },
    AggregateWithoutContributors {
        aggregate: usize,
    },
}

/// Re-runs every proof using only the export object and `layerx-proof`.
///
/// # Errors
///
/// Returns the first malformed statement, failed receipt, inclusion,
/// checkpoint, or aggregate-consistency check.
pub fn verify(
    export: &OfflineExport,
    expected_settlement_domain: SettlementDomain,
) -> Result<VerificationReport, ExportVerificationError> {
    let mut receipt_digests = Vec::with_capacity(export.receipts.len());
    let mut achieved_levels = Vec::new();
    for (index, fact) in export.receipts.iter().enumerate() {
        require_statement(&fact.statement)?;
        let verified = verify_outcome(&fact.canonical_receipt_bytes, &fact.authorised_batch)
            .map_err(|failure| ExportVerificationError::Receipt {
                index,
                check: failure.check,
            })?;
        let digest = verified.evidence().receipt_digest().unwrap_or([0; 32]);
        if digest != fact.expected_receipt_digest {
            return Err(ExportVerificationError::ReceiptDigest { index });
        }
        receipt_digests.push(digest);
        achieved_levels.push(verified.level());
    }
    for (index, fact) in export.inclusions.iter().enumerate() {
        require_statement(&fact.statement)?;
        let evidence = match fact.kind {
            InclusionKind::Activity => verify_activity(
                &fact.canonical_leaf_bytes,
                &fact.proof,
                &fact.canonical_header_bytes,
                &fact.header_signature,
                &fact.sequencer_authorization,
            ),
            InclusionKind::State => verify_state(
                &fact.canonical_leaf_bytes,
                &fact.proof,
                &fact.named_root,
                &fact.canonical_header_bytes,
                &fact.header_signature,
                &fact.sequencer_authorization,
            ),
        }
        .map_err(|error| ExportVerificationError::Inclusion { index, error })?;
        let actual_root = match fact.kind {
            InclusionKind::Activity => evidence.header().header().activity_merkle_root(),
            InclusionKind::State => evidence.header().header().resulting_state_root(),
        };
        if actual_root != fact.named_root {
            return Err(ExportVerificationError::InclusionRoot { index });
        }
        achieved_levels.push(evidence.level());
    }
    for (index, fact) in export.checkpoints.iter().enumerate() {
        require_statement(&fact.statement)?;
        if !fact.availability_obtained {
            return Err(ExportVerificationError::CheckpointUnavailable { index });
        }
        let report = verify_certificate(
            &fact.certificate,
            &fact.bonded_set,
            &fact.registered_checkpoint_id,
            expected_settlement_domain,
            fact.registered_settlement_reference.as_deref(),
        )
        .map_err(|error| ExportVerificationError::Checkpoint { index, error })?;
        verify_availability(fact, index)?;
        achieved_levels.push(report.level());
    }
    let known: BTreeSet<_> = receipt_digests.iter().copied().collect();
    for (aggregate, view) in export.derived_aggregates.iter().enumerate() {
        if view.contributing_receipt_digests.is_empty() {
            return Err(ExportVerificationError::AggregateWithoutContributors { aggregate });
        }
        for digest in &view.contributing_receipt_digests {
            if !known.contains(digest) {
                return Err(ExportVerificationError::UnknownAggregateContributor {
                    aggregate,
                    digest: *digest,
                });
            }
        }
    }
    Ok(VerificationReport {
        verified_receipts: export.receipts.len(),
        verified_inclusions: export.inclusions.len(),
        verified_checkpoints: export.checkpoints.len(),
        receipt_digests,
        achieved_levels,
        derived_aggregates_are_protocol_facts: false,
    })
}

fn verify_availability(fact: &CheckpointFact, index: usize) -> Result<(), ExportVerificationError> {
    verify_certified_availability(&fact.certificate, &fact.availability).map_err(
        |error| match error {
            CertifiedAvailabilityError::Incomplete => {
                ExportVerificationError::CheckpointUnavailable { index }
            }
            CertifiedAvailabilityError::Failure(error) => {
                ExportVerificationError::Availability { index, error }
            }
        },
    )
}

enum CertifiedAvailabilityError {
    Incomplete,
    Failure(Box<AvailabilityFailure>),
}

fn verify_certified_availability(
    certificate: &Certificate,
    availability: &[(Chunk, Proof)],
) -> Result<(), CertifiedAvailabilityError> {
    let header = layerx_wire::receipt::decode_batch_header(certificate.checkpoint().header_bytes())
        .map_err(|_| CertifiedAvailabilityError::Incomplete)?;
    if availability.is_empty() || availability.len() > 4096 {
        return Err(CertifiedAvailabilityError::Incomplete);
    }
    let chunks = availability
        .iter()
        .map(|(chunk, proof)| {
            verify_chunk(
                chunk.clone(),
                proof,
                header.batch_number(),
                &header.data_availability_root(),
            )
            .map_err(|error| CertifiedAvailabilityError::Failure(Box::new(error)))
        })
        .collect::<Result<Vec<_>, _>>()?;
    reassemble(
        &chunks,
        RootCommitments {
            activity: header.activity_merkle_root(),
            receipt: header.receipt_merkle_root(),
            event: header.event_merkle_root(),
            oracle: header.oracle_root(),
        },
    )
    .map_err(|error| CertifiedAvailabilityError::Failure(Box::new(error)))?;
    Ok(())
}

fn require_statement(statement: &str) -> Result<(), ExportVerificationError> {
    if statement.is_empty() {
        Err(ExportVerificationError::EmptyStatement)
    } else {
        Ok(())
    }
}

/// One checkpoint-relative guarantor membership trusted independently of any export.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct TrustedCheckpointMembership {
    set_version: u64,
    keys: Vec<GuarantorKey>,
    threshold: usize,
}

impl TrustedCheckpointMembership {
    /// Pins one exact bonded set version, its ordered keys and minimum threshold.
    ///
    /// # Errors
    ///
    /// Refuses a zero version, an empty, oversized or duplicated key set, and a
    /// threshold outside `1..=keys`.
    pub fn new(
        set_version: u64,
        keys: Vec<GuarantorKey>,
        threshold: usize,
    ) -> Result<Self, OfflineTrustError> {
        let distinct: BTreeSet<_> = keys.iter().map(GuarantorKey::guarantor_id).collect();
        if set_version == 0
            || keys.is_empty()
            || keys.len() > MAX_CHECKPOINT_GUARANTORS
            || distinct.len() != keys.len()
        {
            return Err(OfflineTrustError::Membership);
        }
        if threshold == 0 || threshold > keys.len() {
            return Err(OfflineTrustError::Threshold);
        }
        Ok(Self {
            set_version,
            keys,
            threshold,
        })
    }

    /// Pins the declared guarantor set and threshold of one explicitly selected
    /// declared domain as the membership of `set_version`.
    ///
    /// # Errors
    ///
    /// Returns what [`TrustedCheckpointMembership::new`] returns.
    pub fn from_declared(
        domain: &DeclaredDomain,
        set_version: u64,
    ) -> Result<Self, OfflineTrustError> {
        Self::new(
            set_version,
            domain.guarantor_set().to_vec(),
            domain.certificate_threshold(),
        )
    }
}

/// Verifier-owned trust selected independently of the artifact under test.
#[derive(Clone, Debug)]
pub struct IndependentOfflineTrust {
    registry: ModuleRegistry,
    authority: SignedAuthorityHistory,
    settlement: SettlementDomain,
    memberships: Vec<TrustedCheckpointMembership>,
}

/// Refusal to construct an [`IndependentOfflineTrust`].
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum OfflineTrustError {
    /// The authority history has no verified signed interval.
    NoAuthority,
    /// The settlement chain or contract is zero.
    SettlementDomain,
    /// A membership is empty, oversized or has duplicate guarantors, or none was supplied.
    Membership,
    /// A membership threshold lies outside `1..=keys`.
    Threshold,
    /// Two memberships pin the same set version.
    DuplicateSetVersion,
    /// The declared domain commits to another network than the authority history.
    NetworkId,
}

impl IndependentOfflineTrust {
    /// Combines independently obtained module registry, genesis-anchored signed
    /// authority history, settlement domain and checkpoint memberships.
    ///
    /// # Errors
    ///
    /// Refuses an unverified authority history, a zero settlement domain, no
    /// membership, and duplicate membership versions.
    pub fn new(
        registry: ModuleRegistry,
        authority: SignedAuthorityHistory,
        settlement: SettlementDomain,
        memberships: Vec<TrustedCheckpointMembership>,
    ) -> Result<Self, OfflineTrustError> {
        if authority.intervals().is_empty() || authority.verified_head().is_none() {
            return Err(OfflineTrustError::NoAuthority);
        }
        if settlement.paxeer_chain_id() == 0 || settlement.settlement_contract() == [0; 20] {
            return Err(OfflineTrustError::SettlementDomain);
        }
        if memberships.is_empty() {
            return Err(OfflineTrustError::Membership);
        }
        let versions: BTreeSet<_> = memberships.iter().map(|entry| entry.set_version).collect();
        if versions.len() != memberships.len() {
            return Err(OfflineTrustError::DuplicateSetVersion);
        }
        Ok(Self {
            registry,
            authority,
            settlement,
            memberships,
        })
    }

    /// Uses one explicitly selected declared domain for settlement and membership.
    ///
    /// # Errors
    ///
    /// Refuses a domain whose network differs from the authority history and
    /// everything [`IndependentOfflineTrust::new`] refuses.
    pub fn from_declared_domain(
        registry: ModuleRegistry,
        authority: SignedAuthorityHistory,
        domain: &DeclaredDomain,
        set_version: u64,
    ) -> Result<Self, OfflineTrustError> {
        if domain.network_id() != authority.network_id() {
            return Err(OfflineTrustError::NetworkId);
        }
        let membership = TrustedCheckpointMembership::from_declared(domain, set_version)?;
        Self::new(registry, authority, domain.settlement(), vec![membership])
    }

    #[must_use]
    pub const fn settlement(&self) -> SettlementDomain {
        self.settlement
    }

    #[must_use]
    pub const fn network_id(&self) -> u32 {
        self.authority.network_id()
    }
}

/// The level one requested fact achieved.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct FactVerification {
    fact: FactSelector,
    achieved: VerificationLevel,
}

impl FactVerification {
    #[must_use]
    pub const fn fact(&self) -> FactSelector {
        self.fact
    }

    #[must_use]
    pub const fn achieved(&self) -> VerificationLevel {
        self.achieved
    }
}

/// Successful complete verification: per-fact levels and their minimum.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct CompleteVerificationReport {
    facts: Vec<FactVerification>,
    achieved: VerificationLevel,
}

impl CompleteVerificationReport {
    /// Per requested fact, in request order.
    #[must_use]
    pub fn facts(&self) -> &[FactVerification] {
        &self.facts
    }

    /// Minimum achieved level over the requested facts.
    #[must_use]
    pub const fn achieved(&self) -> VerificationLevel {
        self.achieved
    }
}

/// Exact refusal of a complete export; `fact` is the request position.
#[derive(Clone, Debug, Eq, PartialEq)]
pub enum CompleteExportError {
    /// Settlement anchoring was requested; no authenticated offline Paxeer
    /// finality proof exists, so it can never be established offline.
    SettlementAnchoringUnavailable,
    Facts(FactRefError),
    DuplicateRecord,
    OrphanRecord,
    HeaderDigest,
    MissingRecord {
        fact: usize,
    },
    Header {
        fact: usize,
    },
    Inclusion {
        fact: usize,
        error: InclusionError,
    },
    Receipt {
        fact: usize,
        check: ReceiptCheck,
    },
    ReceiptBinding {
        fact: usize,
    },
    Activity {
        fact: usize,
    },
    AccountState {
        fact: usize,
        error: AccountProofError,
    },
    MaintenanceLink {
        fact: usize,
    },
    Checkpoint {
        fact: usize,
        error: CheckpointError,
    },
    CheckpointBinding {
        fact: usize,
    },
    Membership {
        fact: usize,
    },
    AvailabilityIncomplete {
        fact: usize,
    },
    Availability {
        fact: usize,
        error: Box<AvailabilityFailure>,
    },
    LevelNotAchieved {
        fact: usize,
        achieved: VerificationLevel,
        requested: VerificationLevel,
    },
}

struct TrustedHeader {
    verified: VerifiedBatchHeader,
    signature: [u8; 64],
    authorization: SequencerAuthorization,
}

struct ReceiptLink {
    header: TrustedHeader,
    header_digest: [u8; 32],
    global_sequence: u64,
    resulting_state_root: [u8; 32],
}

struct CompleteVerifier<'a> {
    artifact: &'a CompleteOfflineArtifact,
    trust: &'a IndependentOfflineTrust,
    receipts: BTreeMap<FactSelector, usize>,
    inclusions: BTreeMap<(InclusionRecordKind, FactSelector), usize>,
    states: BTreeMap<FactSelector, usize>,
    certificates: BTreeMap<FactSelector, usize>,
    headers: BTreeMap<[u8; 32], usize>,
    used_receipts: BTreeSet<usize>,
    used_proofs: BTreeSet<usize>,
    used_certificates: BTreeSet<usize>,
    used_headers: BTreeSet<usize>,
}

fn unique<K: Ord>(
    map: &mut BTreeMap<K, usize>,
    key: K,
    index: usize,
) -> Result<(), CompleteExportError> {
    if map.insert(key, index).is_some() {
        Err(CompleteExportError::DuplicateRecord)
    } else {
        Ok(())
    }
}

impl<'a> CompleteVerifier<'a> {
    fn index(
        artifact: &'a CompleteOfflineArtifact,
        trust: &'a IndependentOfflineTrust,
    ) -> Result<Self, CompleteExportError> {
        let mut verifier = Self {
            artifact,
            trust,
            receipts: BTreeMap::new(),
            inclusions: BTreeMap::new(),
            states: BTreeMap::new(),
            certificates: BTreeMap::new(),
            headers: BTreeMap::new(),
            used_receipts: BTreeSet::new(),
            used_proofs: BTreeSet::new(),
            used_certificates: BTreeSet::new(),
            used_headers: BTreeSet::new(),
        };
        for (index, record) in artifact.receipts.iter().enumerate() {
            unique(&mut verifier.receipts, record.reference, index)?;
        }
        for (index, record) in artifact.proofs.iter().enumerate() {
            match record {
                ProofRecord::Inclusion(record) => {
                    unique(
                        &mut verifier.inclusions,
                        (record.kind, record.reference),
                        index,
                    )?;
                }
                ProofRecord::AccountState(record) => {
                    unique(&mut verifier.states, record.reference, index)?;
                }
            }
        }
        for (index, record) in artifact.certificates.iter().enumerate() {
            unique(&mut verifier.certificates, record.reference, index)?;
        }
        for (index, record) in artifact.headers.iter().enumerate() {
            let digest = record
                .digest()
                .map_err(|_| CompleteExportError::HeaderDigest)?;
            unique(&mut verifier.headers, digest, index)?;
        }
        Ok(verifier)
    }

    fn header(
        &mut self,
        digest: [u8; 32],
        fact: usize,
    ) -> Result<TrustedHeader, CompleteExportError> {
        let index = *self
            .headers
            .get(&digest)
            .ok_or(CompleteExportError::MissingRecord { fact })?;
        self.used_headers.insert(index);
        let record = &self.artifact.headers[index];
        let verified = self
            .trust
            .authority
            .verify_header(&record.canonical_header, &record.signature)
            .map_err(|_| CompleteExportError::Header { fact })?;
        let batch = verified.header().batch_number();
        let interval = self
            .trust
            .authority
            .intervals()
            .iter()
            .find(|interval| (interval.first_batch()..=interval.last_batch()).contains(&batch))
            .ok_or(CompleteExportError::Header { fact })?;
        let trusted_id = sequencer_id(&interval.public_key())
            .map_err(|_| CompleteExportError::Header { fact })?;
        if record.public_key != interval.public_key()
            || record.sequencer_id != trusted_id
            || !(record.first_batch..=record.last_batch).contains(&batch)
        {
            return Err(CompleteExportError::Header { fact });
        }
        Ok(TrustedHeader {
            verified,
            signature: record.signature,
            authorization: SequencerAuthorization::new(
                trusted_id,
                interval.public_key(),
                interval.first_batch(),
                interval.last_batch(),
            ),
        })
    }

    fn inclusion(
        &mut self,
        kind: InclusionRecordKind,
        reference: FactSelector,
        fact: usize,
    ) -> Result<&'a InclusionRecord, CompleteExportError> {
        let index = *self
            .inclusions
            .get(&(kind, reference))
            .ok_or(CompleteExportError::MissingRecord { fact })?;
        self.used_proofs.insert(index);
        match &self.artifact.proofs[index] {
            ProofRecord::Inclusion(record) => Ok(record),
            ProofRecord::AccountState(_) => Err(CompleteExportError::MissingRecord { fact }),
        }
    }

    fn receipt_link(
        &mut self,
        activity_id: [u8; 32],
        fact: usize,
    ) -> Result<ReceiptLink, CompleteExportError> {
        let reference = FactSelector::Receipt { activity_id };
        let index = *self
            .receipts
            .get(&reference)
            .ok_or(CompleteExportError::MissingRecord { fact })?;
        self.used_receipts.insert(index);
        let record = &self.artifact.receipts[index];
        let inclusion = self.inclusion(InclusionRecordKind::Receipt, reference, fact)?;
        if inclusion.canonical_leaf != record.canonical_receipt {
            return Err(CompleteExportError::ReceiptBinding { fact });
        }
        let header = self.header(inclusion.header_digest, fact)?;
        verify_receipt(
            &record.canonical_receipt,
            &inclusion.proof,
            header.verified.canonical_bytes(),
            &header.signature,
            &header.authorization,
        )
        .map_err(|error| CompleteExportError::Inclusion { fact, error })?;
        let decoded = decode_receipt(&record.canonical_receipt).map_err(|_| {
            CompleteExportError::Receipt {
                fact,
                check: ReceiptCheck::Decode,
            }
        })?;
        let protocol = decoded.protocol().ok_or(CompleteExportError::Receipt {
            fact,
            check: ReceiptCheck::ReceiptShape,
        })?;
        let committed = header.verified.header();
        if protocol.activity_id() != activity_id
            || protocol.protocol_version() != committed.protocol_version()
            || protocol.global_sequence() < committed.first_sequence()
            || protocol.global_sequence() > committed.last_sequence()
        {
            return Err(CompleteExportError::ReceiptBinding { fact });
        }
        let execution_id = receipt_execution_batch_id(protocol, committed)
            .map_err(|_| CompleteExportError::ReceiptBinding { fact })?;
        let authorised = AuthorizedBatch::new(
            execution_id,
            protocol.asset(),
            committed.previous_state_root(),
            committed.resulting_state_root(),
            header.authorization.public_key(),
        );
        if authorised != record.authorised_batch {
            return Err(CompleteExportError::ReceiptBinding { fact });
        }
        let verified =
            verify_outcome(&record.canonical_receipt, &authorised).map_err(|failure| {
                CompleteExportError::Receipt {
                    fact,
                    check: failure.check,
                }
            })?;
        if verified.evidence().receipt_digest() != Some(record.expected_receipt_digest) {
            return Err(CompleteExportError::ReceiptBinding { fact });
        }
        Ok(ReceiptLink {
            header_digest: inclusion.header_digest,
            global_sequence: protocol.global_sequence(),
            resulting_state_root: protocol.resulting_state_root(),
            header,
        })
    }

    fn activity(
        &mut self,
        activity_id: [u8; 32],
        fact: usize,
    ) -> Result<(VerificationLevel, Vec<u8>), CompleteExportError> {
        let inclusion = self.inclusion(
            InclusionRecordKind::Activity,
            FactSelector::Activity { activity_id },
            fact,
        )?;
        let header = self.header(inclusion.header_digest, fact)?;
        let activity = decode_signed(&inclusion.canonical_leaf, &self.trust.registry)
            .map_err(|_| CompleteExportError::Activity { fact })?;
        if encode_signed(&activity).map_err(|_| CompleteExportError::Activity { fact })?
            != inclusion.canonical_leaf
            || hash_activity_id(&activity).map_err(|_| CompleteExportError::Activity { fact })?
                != activity_id
        {
            return Err(CompleteExportError::Activity { fact });
        }
        let evidence = verify_activity(
            &inclusion.canonical_leaf,
            &inclusion.proof,
            header.verified.canonical_bytes(),
            &header.signature,
            &header.authorization,
        )
        .map_err(|error| CompleteExportError::Inclusion { fact, error })?;
        Ok((evidence.level(), header.verified.canonical_bytes().to_vec()))
    }

    fn state(
        &mut self,
        activity_id: [u8; 32],
        account_id: [u8; 32],
        fact: usize,
    ) -> Result<(VerificationLevel, Vec<u8>), CompleteExportError> {
        let reference = FactSelector::State {
            activity_id,
            account_id,
        };
        let index = *self
            .states
            .get(&reference)
            .ok_or(CompleteExportError::MissingRecord { fact })?;
        self.used_proofs.insert(index);
        let ProofRecord::AccountState(record) = &self.artifact.proofs[index] else {
            return Err(CompleteExportError::MissingRecord { fact });
        };
        let header = self.header(record.header_digest, fact)?;
        let proof = NestedAccountProof {
            account_id: record.account_id,
            account_root: record.account_root,
            universal_root: record.universal_root,
            resulting_state_root: record.resulting_state_root,
            account_proof: record.account_proof.clone(),
            account_tree_proof: record.account_tree_proof.clone(),
            universal_root_proof: record.universal_root_proof.clone(),
            receipt_bytes: record.receipt_bytes.clone(),
            receipt_proof: record.receipt_proof.clone(),
            header_bytes: header.verified.canonical_bytes().to_vec(),
            header_signature: header.signature,
        };
        match record.variant {
            AccountStateVariant::Activity => {
                let verified = verify_nested_account(
                    &record.account_value,
                    account_id,
                    None,
                    &proof,
                    &header.authorization,
                )
                .map_err(|error| CompleteExportError::AccountState { fact, error })?;
                if verified.receipt_activity_id() != activity_id {
                    return Err(CompleteExportError::AccountState {
                        fact,
                        error: AccountProofError::ReceiptBinding,
                    });
                }
            }
            AccountStateVariant::Maintenance {
                activity_count,
                parameter_version,
                dependency_receipt_activity_id,
            } => {
                if dependency_receipt_activity_id != activity_id {
                    return Err(CompleteExportError::MaintenanceLink { fact });
                }
                verify_nested_account_maintenance(
                    &record.account_value,
                    account_id,
                    None,
                    &proof,
                    &header.authorization,
                    activity_count,
                    parameter_version,
                )
                .map_err(|error| CompleteExportError::AccountState { fact, error })?;
                let link = self.receipt_link(activity_id, fact)?;
                let maintenance = decode_maintenance(&record.receipt_bytes)
                    .map_err(|_| CompleteExportError::MaintenanceLink { fact })?;
                let occupancy = maintenance.occupancy();
                if link.header_digest != record.header_digest
                    || link.header.verified.canonical_bytes() != header.verified.canonical_bytes()
                    || link.global_sequence.checked_add(1) != Some(occupancy.global_sequence)
                    || link.resulting_state_root != occupancy.previous_state_root
                {
                    return Err(CompleteExportError::MaintenanceLink { fact });
                }
            }
        }
        Ok((
            VerificationLevel::STATE_PROVEN,
            header.verified.canonical_bytes().to_vec(),
        ))
    }

    fn checkpoint(
        &mut self,
        activity_id: [u8; 32],
        batch_number: u64,
        fact: usize,
    ) -> Result<(VerificationLevel, Vec<u8>), CompleteExportError> {
        let reference = FactSelector::Checkpoint {
            activity_id,
            batch_number,
        };
        let index = *self
            .certificates
            .get(&reference)
            .ok_or(CompleteExportError::MissingRecord { fact })?;
        self.used_certificates.insert(index);
        let record = &self.artifact.certificates[index];
        let link = self.receipt_link(activity_id, fact)?;
        let certified = link.header.verified.canonical_bytes();
        if link.header.verified.header().batch_number() != batch_number
            || record.certificate.checkpoint().header_bytes() != certified
        {
            return Err(CompleteExportError::CheckpointBinding { fact });
        }
        let membership = self
            .trust
            .memberships
            .iter()
            .find(|membership| membership.set_version == record.set_version)
            .ok_or(CompleteExportError::Membership { fact })?;
        if membership.keys != record.bonded_set
            || record.certificate.threshold() < membership.threshold
        {
            return Err(CompleteExportError::Membership { fact });
        }
        let report = verify_certificate(
            &record.certificate,
            &membership.keys,
            &record.checkpoint_id,
            self.trust.settlement,
            record.registered_settlement_reference.as_deref(),
        )
        .map_err(|error| CompleteExportError::Checkpoint { fact, error })?;
        verify_certified_availability(&record.certificate, &record.availability).map_err(
            |error| match error {
                CertifiedAvailabilityError::Incomplete => {
                    CompleteExportError::AvailabilityIncomplete { fact }
                }
                CertifiedAvailabilityError::Failure(error) => {
                    CompleteExportError::Availability { fact, error }
                }
            },
        )?;
        Ok((report.level(), certified.to_vec()))
    }
}

/// Verifies every requested fact of a complete artifact against independent trust.
///
/// Each fact's level comes only from its own bound evidence; a receipt,
/// activity or state fact reaches checkpoint finality only when a requested
/// checkpoint fact certifies its exact signed header. The aggregate is the
/// minimum over the requested facts. Every record must be consumed.
///
/// # Errors
///
/// Refuses settlement anchoring, any missing, duplicate, orphan or failing
/// record, and any fact whose achieved level is below `requested`.
pub fn verify_complete(
    artifact: &CompleteOfflineArtifact,
    trust: &IndependentOfflineTrust,
    requested: VerificationLevel,
) -> Result<CompleteVerificationReport, CompleteExportError> {
    if requested == VerificationLevel::SETTLEMENT_ANCHORED {
        return Err(CompleteExportError::SettlementAnchoringUnavailable);
    }
    check_fact_selectors(&artifact.facts).map_err(CompleteExportError::Facts)?;
    let mut verifier = CompleteVerifier::index(artifact, trust)?;
    let mut outcomes: Vec<Option<(VerificationLevel, Vec<u8>)>> = vec![None; artifact.facts.len()];
    let mut certified = BTreeSet::new();
    for (fact, selector) in artifact.facts.iter().enumerate() {
        if let FactSelector::Checkpoint {
            activity_id,
            batch_number,
        } = *selector
        {
            let outcome = verifier.checkpoint(activity_id, batch_number, fact)?;
            certified.insert(outcome.1.clone());
            outcomes[fact] = Some(outcome);
        }
    }
    for (fact, selector) in artifact.facts.iter().enumerate() {
        let outcome = match *selector {
            FactSelector::Receipt { activity_id } => {
                let link = verifier.receipt_link(activity_id, fact)?;
                (
                    VerificationLevel::BATCH_INCLUDED,
                    link.header.verified.canonical_bytes().to_vec(),
                )
            }
            FactSelector::Activity { activity_id } => verifier.activity(activity_id, fact)?,
            FactSelector::State {
                activity_id,
                account_id,
            } => verifier.state(activity_id, account_id, fact)?,
            FactSelector::Checkpoint { .. } => continue,
        };
        outcomes[fact] = Some(outcome);
    }
    if verifier.used_receipts.len() != artifact.receipts.len()
        || verifier.used_proofs.len() != artifact.proofs.len()
        || verifier.used_certificates.len() != artifact.certificates.len()
        || verifier.used_headers.len() != artifact.headers.len()
    {
        return Err(CompleteExportError::OrphanRecord);
    }
    let mut facts = Vec::with_capacity(artifact.facts.len());
    let mut minimum = VerificationLevel::SETTLEMENT_ANCHORED;
    for (fact, (selector, outcome)) in artifact.facts.iter().zip(outcomes).enumerate() {
        let (mut achieved, header) = outcome.ok_or(CompleteExportError::MissingRecord { fact })?;
        if achieved < VerificationLevel::CHECKPOINT_FINALISED && certified.contains(&header) {
            achieved = VerificationLevel::CHECKPOINT_FINALISED;
        }
        if achieved < requested {
            return Err(CompleteExportError::LevelNotAchieved {
                fact,
                achieved,
                requested,
            });
        }
        minimum = minimum.min(achieved);
        facts.push(FactVerification {
            fact: *selector,
            achieved,
        });
    }
    Ok(CompleteVerificationReport {
        facts,
        achieved: minimum,
    })
}
