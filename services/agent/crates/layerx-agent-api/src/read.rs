//! Verified read, proof, availability, export, and projection contract types.

use crate::identity::{Asset, ContractError};
use crate::verify::Level;
use crate::write_contract::CanonicalBytes;
use crate::{Amount, Sequence, TimestampSeconds};
use layerx_proof::export_codec::{parse_fact_set, FactRefError, FactSelector};

macro_rules! required_reference {
    ($name:ident, $field:literal) => {
        #[derive(Clone, Debug, Eq, Hash, Ord, PartialEq, PartialOrd)]
        pub struct $name(String);

        impl $name {
            /// Constructs a non-empty read reference.
            ///
            /// # Errors
            /// Returns [`ContractError::Empty`] when the reference is empty.
            pub fn new(value: impl Into<String>) -> Result<Self, ContractError> {
                let value = value.into();
                if value.is_empty() {
                    return Err(ContractError::Empty($field));
                }
                Ok(Self(value))
            }

            #[must_use]
            pub fn as_str(&self) -> &str {
                &self.0
            }
        }
    };
}

required_reference!(AccountRef, "account");
required_reference!(ModuleRef, "module");
required_reference!(BatchRef, "batch");
required_reference!(CheckpointRef, "checkpoint");
required_reference!(HistoryCursor, "cursor");
required_reference!(ProviderRef, "provider");
required_reference!(FactRef, "fact");

impl FactRef {
    /// Parses this reference with the single shared strict fact grammar.
    ///
    /// # Errors
    /// Returns the grammar refusal of [`FactSelector::parse`].
    pub fn selector(&self) -> Result<FactSelector, FactRefError> {
        FactSelector::parse(self.as_str())
    }
}

/// Validates an export request: 1..=16 unique facts, each in the strict shared grammar.
///
/// # Errors
/// Returns the first grammar, count or duplicate refusal of [`parse_fact_set`].
pub fn validate_export_request(
    request: ReadRequest<Vec<FactRef>>,
) -> Result<ReadRequest<Vec<FactRef>>, FactRefError> {
    let texts: Vec<&str> = request.selector.iter().map(FactRef::as_str).collect();
    parse_fact_set(texts.as_slice())?;
    Ok(request)
}

/// Checks that a verified export answers exactly the requested facts at the requested level.
///
/// # Errors
/// Returns [`ContractError::Mismatch`] when the stated facts differ from the request,
/// [`ContractError::OutOfRange`] when the achieved level is below the requested level, or the
/// evidence refusal of [`OfflineExport::validate`].
pub fn check_export_response(
    request: &ReadRequest<Vec<FactRef>>,
    response: &VerifiedRead<OfflineExport>,
) -> Result<(), ContractError> {
    if response.value.facts != request.selector {
        return Err(ContractError::Mismatch("export_facts"));
    }
    if response.achieved_verification_level < request.requested_verification_level {
        return Err(ContractError::OutOfRange("export_verification_level"));
    }
    response.value.clone().validate()?;
    Ok(())
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub enum RelativeTo {
    Batch(BatchRef),
    Checkpoint(CheckpointRef),
}

/// Full freshness coordinate attached to every authoritative read.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Freshness {
    pub chain_head: Sequence,
    pub latest_sealed_batch: BatchRef,
    pub latest_finalised_checkpoint: CheckpointRef,
    pub value_sequence: Sequence,
    pub relative_to: RelativeTo,
}

mod sealed {
    pub trait Sealed {}
}

/// Values whose bytes and meaning originate from the `LayerX` core or its evidence.
pub trait CoreProduced: sealed::Sealed {}

/// An authoritative value that always carries achieved proof level and freshness.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct VerifiedRead<T: CoreProduced> {
    pub value: T,
    pub achieved_verification_level: Level,
    pub freshness: Freshness,
}

impl<T: CoreProduced> VerifiedRead<T> {
    #[must_use]
    pub const fn new(value: T, achieved_verification_level: Level, freshness: Freshness) -> Self {
        Self {
            value,
            achieved_verification_level,
            freshness,
        }
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ReadRequest<S> {
    pub selector: S,
    pub requested_verification_level: Level,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct BalanceSelector {
    pub account: AccountRef,
    pub asset: Asset,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ModuleStateSelector {
    pub module: ModuleRef,
    pub key: CanonicalBytes,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct HistorySelector {
    pub first: Sequence,
    pub last: Sequence,
    pub cursor: Option<HistoryCursor>,
    pub page_limit: u32,
}

impl HistorySelector {
    /// Validates an ordered, bounded history request.
    ///
    /// # Errors
    /// Returns [`ContractError::Zero`] for an inverted range or zero page limit.
    pub fn validate(self) -> Result<Self, ContractError> {
        if self.last.0 < self.first.0 {
            return Err(ContractError::Zero("history_range"));
        }
        if self.page_limit == 0 {
            return Err(ContractError::Zero("page_limit"));
        }
        Ok(self)
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct BalanceValue {
    pub account: AccountRef,
    pub asset: Asset,
    pub amount: Amount,
    pub canonical_state: CanonicalBytes,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct AccountValue(pub CanonicalBytes);

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ModuleStateValue(pub CanonicalBytes);

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct HistoryValue {
    pub records: Vec<CanonicalBytes>,
    pub next_cursor: Option<HistoryCursor>,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct BatchValue(pub CanonicalBytes);

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct CheckpointValue(pub CanonicalBytes);

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ProofBundle {
    pub target: CanonicalBytes,
    pub proofs: Vec<CanonicalBytes>,
}


pub const MAX_PROOF_BUNDLE_BYTES: usize = 262_144;

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum ProofBundleTarget {
    Activity([u8; 32]),
    AccountState { activity_id: [u8; 32], account_id: [u8; 32] },
    Receipt([u8; 32]),
}

impl ProofBundleTarget {
    pub fn decode(bytes: &[u8]) -> Result<Self, ContractError> {
        if !matches!(bytes.len(), 35 | 67) || bytes[..2] != [0, 1] {
            return Err(ContractError::Mismatch("proof_bundle_target"));
        }
        let activity_id: [u8; 32] = bytes[3..35].try_into()
            .map_err(|_| ContractError::Mismatch("proof_bundle_target"))?;
        if activity_id == [0; 32] { return Err(ContractError::Zero("activity_id")); }
        match (bytes[2], bytes.len()) {
            (1, 35) => Ok(Self::Activity(activity_id)),
            (3, 35) => Ok(Self::Receipt(activity_id)),
            (2, 67) => {
                let account_id: [u8; 32] = bytes[35..67].try_into()
                    .map_err(|_| ContractError::Mismatch("account_id"))?;
                if account_id == [0; 32] { return Err(ContractError::Zero("account_id")); }
                Ok(Self::AccountState { activity_id, account_id })
            }
            _ => Err(ContractError::Mismatch("proof_bundle_target")),
        }
    }

    pub const fn activity_id(self) -> [u8; 32] {
        match self {
            Self::Activity(id) | Self::Receipt(id) => id,
            Self::AccountState { activity_id, .. } => activity_id,
        }
    }

    pub fn encode(self) -> Result<CanonicalBytes, ContractError> {
        let mut bytes = vec![0, 1, match self {
            Self::Activity(_) => 1, Self::AccountState { .. } => 2, Self::Receipt(_) => 3,
        }];
        bytes.extend_from_slice(&self.activity_id());
        if let Self::AccountState { account_id, .. } = self { bytes.extend_from_slice(&account_id); }
        Self::decode(&bytes)?;
        CanonicalBytes::new(bytes)
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
#[repr(u8)]
pub enum ProofBundleVariant { Activity = 1, Account = 2, Receipt = 3, MaintainedAccount = 4 }

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ProofBundleRecord {
    pub variant: ProofBundleVariant,
    pub canonical_value: CanonicalBytes,
    pub native_proof: CanonicalBytes,
    pub activity_receipt: Option<(CanonicalBytes, CanonicalBytes)>,
}

impl ProofBundleRecord {
    pub fn encode(&self) -> Result<CanonicalBytes, ContractError> {
        if (self.variant == ProofBundleVariant::MaintainedAccount) != self.activity_receipt.is_some() {
            return Err(ContractError::Mismatch("proof_bundle_variant"));
        }
        let mut bytes = b"LXPB1".to_vec();
        bytes.push(self.variant as u8);
        let mut append = |field: &CanonicalBytes| -> Result<(), ContractError> {
            let value = field.as_bytes();
            if value.is_empty() || bytes.len().checked_add(4).and_then(|n| n.checked_add(value.len()))
                .filter(|n| *n <= MAX_PROOF_BUNDLE_BYTES).is_none() {
                return Err(ContractError::OutOfRange("proof_bundle_bytes"));
            }
            let length = u32::try_from(value.len()).map_err(|_| ContractError::OutOfRange("proof_bundle_bytes"))?;
            bytes.extend_from_slice(&length.to_be_bytes());
            bytes.extend_from_slice(value);
            Ok(())
        };
        append(&self.canonical_value)?;
        append(&self.native_proof)?;
        if let Some((receipt, proof)) = &self.activity_receipt { append(receipt)?; append(proof)?; }
        CanonicalBytes::new(bytes)
    }

    pub fn decode(bytes: &[u8]) -> Result<Self, ContractError> {
        if bytes.len() < 6 || bytes.len() > MAX_PROOF_BUNDLE_BYTES || &bytes[..5] != b"LXPB1" {
            return Err(ContractError::Mismatch("proof_bundle_record"));
        }
        let variant = match bytes[5] {
            1 => ProofBundleVariant::Activity, 2 => ProofBundleVariant::Account,
            3 => ProofBundleVariant::Receipt, 4 => ProofBundleVariant::MaintainedAccount,
            _ => return Err(ContractError::Mismatch("proof_bundle_variant")),
        };
        let mut offset = 6_usize;
        let mut field = || -> Result<CanonicalBytes, ContractError> {
            let length_end = offset.checked_add(4).ok_or(ContractError::OutOfRange("proof_bundle_bytes"))?;
            let length = u32::from_be_bytes(bytes.get(offset..length_end)
                .ok_or(ContractError::Mismatch("proof_bundle_record"))?.try_into()
                .map_err(|_| ContractError::Mismatch("proof_bundle_record"))?) as usize;
            let end = length_end.checked_add(length).ok_or(ContractError::OutOfRange("proof_bundle_bytes"))?;
            let value = CanonicalBytes::new(bytes.get(length_end..end)
                .ok_or(ContractError::Mismatch("proof_bundle_record"))?.to_vec())?;
            offset = end;
            Ok(value)
        };
        let canonical_value = field()?;
        let native_proof = field()?;
        let activity_receipt = if variant == ProofBundleVariant::MaintainedAccount {
            Some((field()?, field()?))
        } else { None };
        if offset != bytes.len() { return Err(ContractError::Mismatch("proof_bundle_trailing")); }
        Ok(Self { variant, canonical_value, native_proof, activity_receipt })
    }
}

impl ProofBundle {
    pub fn check_response(
        request: &ReadRequest<CanonicalBytes>, response: &VerifiedRead<Self>,
    ) -> Result<ProofBundleRecord, ContractError> {
        ProofBundleTarget::decode(request.selector.as_bytes())?;
        if response.value.target != request.selector {
            return Err(ContractError::Mismatch("proof_bundle_target"));
        }
        let record = response.value.record()?;
        let expected = match record.variant {
            ProofBundleVariant::Activity | ProofBundleVariant::Receipt => Level::BatchIncluded,
            ProofBundleVariant::Account | ProofBundleVariant::MaintainedAccount => Level::StateProven,
        };
        if response.achieved_verification_level != expected
            || response.achieved_verification_level < request.requested_verification_level {
            return Err(ContractError::OutOfRange("proof_bundle_verification_level"));
        }
        Ok(record)
    }

    pub fn record(&self) -> Result<ProofBundleRecord, ContractError> {
        let target = ProofBundleTarget::decode(self.target.as_bytes())?;
        if self.proofs.len() != 1 { return Err(ContractError::OutOfRange("proof_bundle_count")); }
        let record = ProofBundleRecord::decode(self.proofs[0].as_bytes())?;
        if !matches!((target, record.variant),
            (ProofBundleTarget::Activity(_), ProofBundleVariant::Activity)
            | (ProofBundleTarget::Receipt(_), ProofBundleVariant::Receipt)
            | (ProofBundleTarget::AccountState { .. }, ProofBundleVariant::Account | ProofBundleVariant::MaintainedAccount)) {
            return Err(ContractError::Mismatch("proof_bundle_target_variant"));
        }
        Ok(record)
    }
}

macro_rules! core_produced {
    ($($name:ty),+ $(,)?) => {
        $(
            impl sealed::Sealed for $name {}
            impl CoreProduced for $name {}
        )+
    };
}

core_produced!(
    BalanceValue,
    AccountValue,
    ModuleStateValue,
    HistoryValue,
    BatchValue,
    CheckpointValue,
    ProofBundle,
);

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum AvailabilityClass {
    Activities,
    Receipts,
    Oracle,
    StateDiff,
    Recovery,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ClassReport {
    pub class: AvailabilityClass,
    pub complete: bool,
    pub verified_chunks: u32,
    pub verified_bytes: u64,
    pub failure: Option<String>,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ProviderReport {
    pub provider: ProviderRef,
    pub classes: Vec<ClassReport>,
    pub failure: Option<String>,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub enum AvailabilityCompletion {
    Complete { provider: ProviderRef },
    Partial,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct AvailabilityReport {
    pub completion: AvailabilityCompletion,
    pub classes: Vec<ClassReport>,
    pub providers: Vec<ProviderReport>,
}

impl sealed::Sealed for AvailabilityReport {}
impl CoreProduced for AvailabilityReport {}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct AvailabilityRequest {
    pub selector: String,
    pub requested_verification_level: Level,
    pub maximum_bytes: u64,
    pub maximum_chunks: u32,
    pub deadline: TimestampSeconds,
}

impl AvailabilityRequest {
    /// Enforces finite retrieval bounds.
    ///
    /// # Errors
    /// Returns [`ContractError::Zero`] for any zero bound or empty selector.
    pub fn validate(self) -> Result<Self, ContractError> {
        if self.selector.is_empty() {
            return Err(ContractError::Empty("availability_selector"));
        }
        if self.maximum_bytes == 0 || self.maximum_chunks == 0 || self.deadline.0 == 0 {
            return Err(ContractError::Zero("availability_bound"));
        }
        Ok(self)
    }
}

/// Self-contained artifacts needed to verify a stated fact set offline.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct OfflineExport {
    pub facts: Vec<FactRef>,
    pub receipts: Vec<CanonicalBytes>,
    pub proofs: Vec<CanonicalBytes>,
    pub certificates: Vec<CanonicalBytes>,
    pub headers: Vec<CanonicalBytes>,
}

impl sealed::Sealed for OfflineExport {}
impl CoreProduced for OfflineExport {}

impl OfflineExport {
    /// Refuses an export that states no facts or carries no verification evidence.
    ///
    /// # Errors
    /// Returns [`ContractError::Empty`] when facts or all evidence are absent.
    pub fn validate(self) -> Result<Self, ContractError> {
        if self.facts.is_empty() {
            return Err(ContractError::Empty("fact_set"));
        }
        if self.receipts.is_empty()
            && self.proofs.is_empty()
            && self.certificates.is_empty()
            && self.headers.is_empty()
        {
            return Err(ContractError::Empty("offline_evidence"));
        }
        Ok(self)
    }
}

/// Explicitly non-authoritative estimate; it cannot satisfy [`CoreProduced`].
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ProjectionResult<T> {
    pub projected: T,
    pub rationale: String,
    pub observed_freshness: Freshness,
}

impl<T> ProjectionResult<T> {
    /// Creates a projection with a mandatory rationale.
    ///
    /// # Errors
    /// Returns [`ContractError::Empty`] when the rationale is empty.
    pub fn new(
        projected: T,
        rationale: impl Into<String>,
        observed_freshness: Freshness,
    ) -> Result<Self, ContractError> {
        let rationale = rationale.into();
        if rationale.is_empty() {
            return Err(ContractError::Empty("projection_rationale"));
        }
        Ok(Self {
            projected,
            rationale,
            observed_freshness,
        })
    }
}

/// Hypothetical fee meter for one canonical activity, read against committed native state.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct FeeProjectionRequest {
    /// Full protocol module and ordinal packed into the canonical u32 form.
    pub protocol_activity_type: u32,
    /// Canonical activity size in bytes; zero is accepted by the native meter.
    pub canonical_bytes: u64,
    pub execution_units: u64,
    pub storage_units: u64,
}

impl FeeProjectionRequest {
    /// Enforces the native meter bound without clamping.
    ///
    /// # Errors
    /// Returns [`ContractError::OutOfRange`] when `canonical_bytes` exceeds the native maximum.
    pub const fn validate(self) -> Result<Self, ContractError> {
        if self.canonical_bytes > layerx_client::client::MAX_FEE_METER_CANONICAL_BYTES {
            return Err(ContractError::OutOfRange("canonical_bytes"));
        }
        Ok(self)
    }

    #[must_use]
    pub const fn meter(self) -> layerx_client::client::FeeMeter {
        layerx_client::client::FeeMeter {
            activity_type: self.protocol_activity_type,
            canonical_bytes: self.canonical_bytes,
            execution_units: self.execution_units,
            storage_units: self.storage_units,
        }
    }
}

/// Fee computed by the committed native schedule at one captured head; never an executed fee.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct FeeProjection {
    pub request: FeeProjectionRequest,
    pub parameter_version: u32,
    pub fee: Amount,
    pub canonical_schedule: CanonicalBytes,
    /// Committed snapshot the native read answered from; equal to the captured head.
    pub snapshot_sequence: Sequence,
    pub snapshot_state_root: [u8; 32],
}

impl FeeProjection {
    /// Builds the projected value from one authenticated fee observation.
    ///
    /// # Errors
    /// Returns the meter refusal, or [`ContractError::Mismatch`] when the observation's
    /// snapshot is not the captured head.
    pub fn from_observation(
        request: FeeProjectionRequest,
        observation: layerx_client::client::FeeObservation,
    ) -> Result<Self, ContractError> {
        let request = request.validate()?;
        let (head, snapshot) = observation.into_parts();
        if snapshot.observed_sequence != head.chain_sequence {
            return Err(ContractError::Mismatch("fee_snapshot_sequence"));
        }
        if snapshot.value.parameter_version == 0 {
            return Err(ContractError::Zero("parameter_version"));
        }
        if snapshot.state_root == [0; 32] {
            return Err(ContractError::Zero("snapshot_state_root"));
        }
        Ok(Self {
            request,
            parameter_version: snapshot.value.parameter_version,
            fee: Amount(snapshot.value.fee),
            canonical_schedule: CanonicalBytes::new(snapshot.value.canonical_schedule)?,
            snapshot_sequence: Sequence(snapshot.observed_sequence),
            snapshot_state_root: snapshot.state_root,
        })
    }

    /// Wraps the value as an explicitly non-authoritative projection.
    ///
    /// # Errors
    /// Returns [`ContractError::Mismatch`] when the freshness does not name this snapshot as
    /// both chain head and value sequence, or the rationale refusal of [`ProjectionResult::new`].
    pub fn into_projection(
        self,
        rationale: impl Into<String>,
        observed_freshness: Freshness,
    ) -> Result<ProjectionResult<Self>, ContractError> {
        if observed_freshness.chain_head != self.snapshot_sequence
            || observed_freshness.value_sequence != self.snapshot_sequence
        {
            return Err(ContractError::Mismatch("fee_snapshot_sequence"));
        }
        ProjectionResult::new(self, rationale, observed_freshness)
    }
}

#[cfg(test)]
mod proof_bundle_framing_tests {
    use super::*;

    #[test]
    fn target_preserves_native_pb1_and_refuses_noncanonical_selectors() -> Result<(), ContractError> {
        for target in [ProofBundleTarget::Activity([1; 32]), ProofBundleTarget::Receipt([2; 32]),
            ProofBundleTarget::AccountState { activity_id: [3; 32], account_id: [4; 32] }] {
            let encoded = target.encode()?;
            assert_eq!(ProofBundleTarget::decode(encoded.as_bytes())?, target);
            let mut trailing = encoded.as_bytes().to_vec();
            trailing.push(0);
            assert!(ProofBundleTarget::decode(&trailing).is_err());
            let mut version = encoded.as_bytes().to_vec();
            version[1] = 2;
            assert!(ProofBundleTarget::decode(&version).is_err());
            let mut unknown = encoded.as_bytes().to_vec();
            unknown[2] = 4;
            assert!(ProofBundleTarget::decode(&unknown).is_err());
        }
        assert!(ProofBundleTarget::Activity([0; 32]).encode().is_err());
        assert!(ProofBundleTarget::AccountState { activity_id: [1; 32], account_id: [0; 32] }.encode().is_err());
        for length in 0..35 { assert!(ProofBundleTarget::decode(&vec![0; length]).is_err()); }
        Ok(())
    }

    #[test]
    fn framing_keeps_four_variants_and_maintenance_link_distinct() -> Result<(), ContractError> {
        for variant in [ProofBundleVariant::Activity, ProofBundleVariant::Receipt,
            ProofBundleVariant::Account, ProofBundleVariant::MaintainedAccount] {
            let record = ProofBundleRecord {
                variant, canonical_value: CanonicalBytes::new(vec![1])?,
                native_proof: CanonicalBytes::new(vec![2])?,
                activity_receipt: if variant == ProofBundleVariant::MaintainedAccount {
                    Some((CanonicalBytes::new(vec![3])?, CanonicalBytes::new(vec![4])?))
                } else { None },
            };
            let encoded = record.encode()?;
            assert_eq!(ProofBundleRecord::decode(encoded.as_bytes())?, record);
            let mut trailing = encoded.as_bytes().to_vec();
            trailing.push(0);
            assert!(ProofBundleRecord::decode(&trailing).is_err());
            let mut unknown = encoded.as_bytes().to_vec();
            unknown[5] = 5;
            assert!(ProofBundleRecord::decode(&unknown).is_err());
            let mut empty = encoded.as_bytes().to_vec();
            empty[6..10].fill(0);
            assert!(ProofBundleRecord::decode(&empty).is_err());
            let mut wrong_link = record.clone();
            wrong_link.activity_receipt = if record.activity_receipt.is_some() { None }
                else { Some((CanonicalBytes::new(vec![3])?, CanonicalBytes::new(vec![4])?)) };
            assert!(wrong_link.encode().is_err());
        }
        Ok(())
    }

    #[test]
    fn framing_refuses_oversize_duplicate_and_wrong_target_variant() -> Result<(), ContractError> {
        let mut record = ProofBundleRecord {
            variant: ProofBundleVariant::Receipt,
            canonical_value: CanonicalBytes::new(vec![1; MAX_PROOF_BUNDLE_BYTES - 15])?,
            native_proof: CanonicalBytes::new(vec![2])?, activity_receipt: None,
        };
        assert_eq!(record.encode()?.as_bytes().len(), MAX_PROOF_BUNDLE_BYTES);
        record.canonical_value = CanonicalBytes::new(vec![1; MAX_PROOF_BUNDLE_BYTES - 14])?;
        assert!(record.encode().is_err());
        assert!(ProofBundleRecord::decode(&vec![1; MAX_PROOF_BUNDLE_BYTES + 1]).is_err());
        record.canonical_value = CanonicalBytes::new(vec![1])?;
        let encoded = record.encode()?;
        let mut value = ProofBundle { target: ProofBundleTarget::Receipt([1; 32]).encode()?,
            proofs: vec![encoded.clone()] };
        assert!(value.record().is_ok());
        value.proofs.push(encoded);
        assert!(value.record().is_err());
        value.proofs.pop();
        value.target = ProofBundleTarget::Activity([1; 32]).encode()?;
        assert!(value.record().is_err());
        Ok(())
    }
}
