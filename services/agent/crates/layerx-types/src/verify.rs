//! Verification levels, evidence provenance, and non-authoritative projections.

use std::cmp::Ordering;

/// Internal rank for the ordered verification lattice.
#[derive(Clone, Copy, Debug, Eq, Ord, PartialEq, PartialOrd)]
enum VerificationRank {
    Unverified,
    SequencerSigned,
    BatchIncluded,
    StateProven,
    CheckpointFinalised,
    SettlementAnchored,
}

/// An ordered verification level. The internal rank is private so callers can
/// request and compare levels but cannot invent a new level representation.
#[derive(Clone, Copy, Debug, Eq, Ord, PartialEq, PartialOrd)]
pub struct VerificationLevel(VerificationRank);

impl VerificationLevel {
    /// No local evidence has passed.
    pub const UNVERIFIED: Self = Self(VerificationRank::Unverified);
    /// The authorised sequencer signature passed.
    pub const SEQUENCER_SIGNED: Self = Self(VerificationRank::SequencerSigned);
    /// Activity inclusion in the signed batch passed.
    pub const BATCH_INCLUDED: Self = Self(VerificationRank::BatchIncluded);
    /// State inclusion against the resulting state root passed.
    pub const STATE_PROVEN: Self = Self(VerificationRank::StateProven);
    /// A threshold checkpoint certificate passed.
    pub const CHECKPOINT_FINALISED: Self = Self(VerificationRank::CheckpointFinalised);
    /// The settlement reference matched the registered checkpoint anchor.
    pub const SETTLEMENT_ANCHORED: Self = Self(VerificationRank::SettlementAnchored);

    /// Compares achieved evidence strength with a requested level.
    #[must_use]
    pub const fn compare(self, requested: Self) -> Ordering {
        let achieved = self.0 as u8;
        let requested = requested.0 as u8;
        if achieved < requested {
            Ordering::Less
        } else if achieved > requested {
            Ordering::Greater
        } else {
            Ordering::Equal
        }
    }

    /// Returns the stable wire rank from zero through five.
    #[must_use]
    pub const fn wire_rank(self) -> u8 {
        self.0 as u8
    }
}

/// Identifiers for the exact evidence supporting a verified value.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct EvidenceIds {
    receipt_digest: [u8; 32],
    batch_id: Option<[u8; 32]>,
    state_root: Option<[u8; 32]>,
    checkpoint_id: Option<[u8; 32]>,
    settlement_reference_hash: Option<[u8; 32]>,
}

impl EvidenceIds {
    /// Returns the core receipt digest.
    #[must_use]
    pub const fn receipt_digest(&self) -> [u8; 32] {
        self.receipt_digest
    }

    /// Returns the batch evidence identifier when inclusion passed.
    #[must_use]
    pub const fn batch_id(&self) -> Option<[u8; 32]> {
        self.batch_id
    }

    /// Returns the proven state root when state inclusion passed.
    #[must_use]
    pub const fn state_root(&self) -> Option<[u8; 32]> {
        self.state_root
    }

    /// Returns the certificate identifier when finality passed.
    #[must_use]
    pub const fn checkpoint_id(&self) -> Option<[u8; 32]> {
        self.checkpoint_id
    }

    /// Returns the settlement reference digest when anchoring passed.
    #[must_use]
    pub const fn settlement_reference_hash(&self) -> Option<[u8; 32]> {
        self.settlement_reference_hash
    }
}

/// A decoded core value bound to proof-achieved evidence and freshness.
///
/// This type intentionally has no public constructor or level mutator.
/// `layerx-proof` gains the evidence-gated construction path with its verifiers.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Verified<T> {
    value: T,
    level: VerificationLevel,
    evidence: EvidenceIds,
    head_sequence: u64,
    relative_checkpoint: Option<[u8; 32]>,
}

impl<T> Verified<T> {
    /// Borrows the core-produced value.
    #[must_use]
    pub const fn value(&self) -> &T {
        &self.value
    }

    /// Returns the level actually achieved by proof verification.
    #[must_use]
    pub const fn level(&self) -> VerificationLevel {
        self.level
    }

    /// Returns the exact evidence identifiers.
    #[must_use]
    pub const fn evidence(&self) -> &EvidenceIds {
        &self.evidence
    }

    /// Returns the core head sequence this value is relative to.
    #[must_use]
    pub const fn head_sequence(&self) -> u64 {
        self.head_sequence
    }

    /// Returns the relative checkpoint when checkpoint evidence exists.
    #[must_use]
    pub const fn relative_checkpoint(&self) -> Option<[u8; 32]> {
        self.relative_checkpoint
    }
}

/// A locally estimated value that can never inhabit a verified-value field.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Projection<T> {
    value: T,
    rationale: String,
}

impl<T> Projection<T> {
    /// Labels a locally estimated value with its non-authoritative rationale.
    pub fn new(value: T, rationale: impl Into<String>) -> Self {
        Self {
            value,
            rationale: rationale.into(),
        }
    }

    /// Borrows the estimated value.
    #[must_use]
    pub const fn value(&self) -> &T {
        &self.value
    }

    /// Returns the explanation for the estimate.
    #[must_use]
    pub fn rationale(&self) -> &str {
        &self.rationale
    }

    /// Consumes the projection without changing its non-authoritative meaning.
    #[must_use]
    pub fn into_value(self) -> T {
        self.value
    }
}
