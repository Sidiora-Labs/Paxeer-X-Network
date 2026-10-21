//! Authentication-free queries over protocol-public explorer state.

use crate::unified::{UnifiedAccountJoin, UnifiedAccountView};
use crate::verify::{PastedInclusion, VerificationReport, Verifier, VerifyError};
use crate::{
    AccountActivityRecord, BatchRecord, CheckpointRecord, Freshness, Indexed, Indexer,
    PublicRecord, RecordId,
};

const MAXIMUM_PAGE_SIZE: usize = 100;

/// One bounded newest-first public page and its exclusive continuation point.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Page<T> {
    pub items: Vec<T>,
    pub next_before: Option<u64>,
}

/// Query readiness against the authoritative source head: the contiguous
/// receipt-verified coverage from batch one and every uncovered range.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Readiness {
    pub source_available: bool,
    pub source_chain_sequence: u64,
    pub source_sealed_batch: u64,
    /// Highest batch `b` such that every batch in `1..=b` completed independent
    /// receipt authority; zero when none has.
    pub indexed_through: u64,
    /// Ascending, merged, inclusive ranges in `1..=source_sealed_batch` that
    /// have not completed independent receipt authority.
    pub incomplete_ranges: Vec<(u64, u64)>,
    /// True only when the source head has sealed at least one batch and every
    /// batch through it is receipt-verified. An empty projection, a projection
    /// behind the head and a head with no sealed batch are never complete.
    pub complete: bool,
}

/// Typed refusal for invalid or silently incomplete public queries.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum QueryError {
    InvalidPageSize,
    InvalidCursor,
    AccountIndexIncomplete {
        batch: u64,
    },
    IncompleteFromHead {
        source_sealed_batch: u64,
        indexed_through: u64,
    },
}

/// Every public refusal carries the same live freshness disclosure as success.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct QueryFailure {
    pub error: QueryError,
    pub freshness: Freshness,
}

/// Proof refusal paired with live public-index freshness. The error is boxed
/// so the response result remains inexpensive to move at the HTTP boundary.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct VerificationFailure {
    pub error: Box<VerifyError>,
    pub freshness: Freshness,
}

/// Public explorer surface. Construction and every method deliberately omit
/// principal, session, profile and notification inputs.
#[derive(Clone, Copy)]
pub struct PublicExplorer<'a> {
    index: &'a Indexer,
    verifier: &'a Verifier,
}

impl Indexer {
    /// Opens the authentication-free protocol-public query surface.
    #[must_use]
    pub const fn public<'a>(&'a self, verifier: &'a Verifier) -> PublicExplorer<'a> {
        PublicExplorer {
            index: self,
            verifier,
        }
    }

    /// Reports the authoritative source head, the contiguous receipt-verified
    /// coverage and every incomplete range through the head. Only batches whose
    /// every receipt passed independent receipt authority count as covered.
    #[must_use]
    pub fn readiness(&self) -> Readiness {
        let source_sealed_batch = self.observed_head.sealed_batch;
        let incomplete_ranges = incomplete_ranges(
            self.receipt_authority_batches.iter().copied(),
            source_sealed_batch,
        );
        let indexed_through = match incomplete_ranges.first() {
            None => source_sealed_batch,
            Some((first, _)) => first.saturating_sub(1),
        };
        Readiness {
            source_available: self.source_available,
            source_chain_sequence: self.observed_head.chain_sequence,
            source_sealed_batch,
            indexed_through,
            complete: self.source_available
                && source_sealed_batch > 0
                && incomplete_ranges.is_empty(),
            incomplete_ranges,
        }
    }

    /// Returns the one unified account view: the gateway-reported join across
    /// both domains with this index's own receipt-verified LayerX activity
    /// attached. An account the network reports no LayerX half for carries an
    /// empty LayerX page rather than an invented one.
    ///
    /// # Errors
    ///
    /// Refuses invalid bounds, any view for which an indexed batch has not
    /// completed independent receipt-authority verification, and any view
    /// while the projection is empty or behind the authoritative source head.
    pub fn unified_account(
        &self,
        join: UnifiedAccountJoin,
        before_sequence: Option<u64>,
        limit: usize,
    ) -> Result<Indexed<UnifiedAccountView>, QueryFailure> {
        validate_limit(limit).map_err(|error| self.failure(error))?;
        if before_sequence == Some(0) {
            return Err(self.failure(QueryError::InvalidCursor));
        }
        let layerx_activity = match join.identities.layerx_account {
            Some(account) => self
                .account_activity_page(account, before_sequence, limit)
                .map_err(|error| self.failure(error))?,
            None => {
                validate_limit(limit).map_err(|error| self.failure(error))?;
                Page {
                    items: Vec::new(),
                    next_before: None,
                }
            }
        };
        Ok(Indexed {
            value: UnifiedAccountView {
                join,
                layerx_activity,
            },
            freshness: self.freshness(),
        })
    }

    fn account_activity_page(
        &self,
        account: [u8; 32],
        before_sequence: Option<u64>,
        limit: usize,
    ) -> Result<Page<AccountActivityRecord>, QueryError> {
        validate_limit(limit)?;
        if let Some(batch) = self
            .batches
            .keys()
            .find(|batch| !self.receipt_authority_batches.contains(batch))
        {
            return Err(QueryError::AccountIndexIncomplete { batch: *batch });
        }
        let readiness = self.readiness();
        if !readiness.complete {
            return Err(QueryError::IncompleteFromHead {
                source_sealed_batch: readiness.source_sealed_batch,
                indexed_through: readiness.indexed_through,
            });
        }
        let mut records = self
            .account_activities
            .values()
            .filter(|record| record.from == account || record.to == account)
            .filter(|record| before_sequence.is_none_or(|before| record.global_sequence < before))
            .cloned()
            .collect::<Vec<_>>();
        records.sort_by(|left, right| {
            right
                .global_sequence
                .cmp(&left.global_sequence)
                .then_with(|| right.receipt_id.cmp(&left.receipt_id))
        });
        records.truncate(limit.saturating_add(1));
        Ok(page(&mut records, limit, |record| record.global_sequence))
    }

    fn failure(&self, error: QueryError) -> QueryFailure {
        QueryFailure {
            error,
            freshness: self.freshness(),
        }
    }
}

impl PublicExplorer<'_> {
    /// Browses finalised checkpoints newest-first.
    ///
    /// # Errors
    ///
    /// Refuses a zero or over-limit page size.
    pub fn checkpoints(
        &self,
        before_batch: Option<u64>,
        limit: usize,
    ) -> Result<Indexed<Page<CheckpointRecord>>, QueryFailure> {
        validate_limit(limit).map_err(|error| self.failure(error))?;
        let mut records = self
            .index
            .checkpoints_by_batch
            .iter()
            .rev()
            .filter(|(batch, _)| before_batch.is_none_or(|before| **batch < before))
            .filter_map(|(_, identifier)| self.index.checkpoints.get(identifier).cloned())
            .take(limit.saturating_add(1))
            .collect::<Vec<_>>();
        Ok(Indexed {
            value: page(&mut records, limit, |record| record.batch_number),
            freshness: self.index.freshness(),
        })
    }

    /// Browses complete availability batches newest-first.
    ///
    /// # Errors
    ///
    /// Refuses a zero or over-limit page size.
    pub fn batches(
        &self,
        before_batch: Option<u64>,
        limit: usize,
    ) -> Result<Indexed<Page<BatchRecord>>, QueryFailure> {
        validate_limit(limit).map_err(|error| self.failure(error))?;
        let mut records = self
            .index
            .batches
            .iter()
            .rev()
            .filter(|(batch, _)| before_batch.is_none_or(|before| **batch < before))
            .map(|(_, record)| record.clone())
            .take(limit.saturating_add(1))
            .collect::<Vec<_>>();
        Ok(Indexed {
            value: page(&mut records, limit, |record| record.batch_number),
            freshness: self.index.freshness(),
        })
    }

    /// Looks up one protocol-public receipt by its content identifier.
    #[must_use]
    pub fn receipt(&self, identifier: RecordId) -> Indexed<Option<PublicRecord>> {
        self.index.receipt(identifier)
    }

    /// Looks up a receipt using either its protocol receipt digest or activity
    /// identifier, not the explorer's content-addressed row key.
    #[must_use]
    pub fn receipt_by_id(&self, identifier: [u8; 32]) -> Indexed<Option<PublicRecord>> {
        Indexed {
            value: self
                .index
                .receipts_by_protocol_id
                .get(&identifier)
                .and_then(|identifier| self.index.receipts.get(identifier))
                .cloned(),
            freshness: self.index.freshness(),
        }
    }

    /// Lists receipt-verified activity for one public protocol account hash.
    ///
    /// # Errors
    ///
    /// Refuses invalid bounds, any view for which an indexed batch has not
    /// completed independent receipt-authority verification, and any view
    /// while the projection is empty or behind the authoritative source head.
    pub fn account_activity(
        &self,
        account: [u8; 32],
        before_sequence: Option<u64>,
        limit: usize,
    ) -> Result<Indexed<Page<AccountActivityRecord>>, QueryFailure> {
        Ok(Indexed {
            value: self
                .index
                .account_activity_page(account, before_sequence, limit)
                .map_err(|error| self.failure(error))?,
            freshness: self.index.freshness(),
        })
    }

    /// Verifies pasted receipt bytes independently, pairing the proof result
    /// with current explorer freshness without consulting indexed receipt state.
    ///
    /// # Errors
    ///
    /// Returns the proof failure and current freshness together.
    pub fn verify_receipt(
        &self,
        pasted_receipt: &[u8],
    ) -> Result<Indexed<VerificationReport>, VerificationFailure> {
        Ok(Indexed {
            value: self
                .verifier
                .receipt(pasted_receipt)
                .map_err(|error| VerificationFailure {
                    error: Box::new(error),
                    freshness: self.index.freshness(),
                })?,
            freshness: self.index.freshness(),
        })
    }

    /// Verifies a pasted inclusion proof independently and pairs the result
    /// with current explorer freshness.
    ///
    /// # Errors
    ///
    /// Returns the proof failure and current freshness together.
    pub fn verify_inclusion(
        &self,
        pasted: &PastedInclusion<'_>,
    ) -> Result<Indexed<VerificationReport>, VerificationFailure> {
        Ok(Indexed {
            value: self
                .verifier
                .inclusion(pasted)
                .map_err(|error| VerificationFailure {
                    error: Box::new(error),
                    freshness: self.index.freshness(),
                })?,
            freshness: self.index.freshness(),
        })
    }

    fn failure(&self, error: QueryError) -> QueryFailure {
        QueryFailure {
            error,
            freshness: self.index.freshness(),
        }
    }
}

/// Uncovered inclusive ranges in `1..=head`, given covered batches in strictly
/// ascending order. Batch zero and covered batches above the head are ignored.
fn incomplete_ranges(covered: impl Iterator<Item = u64>, head: u64) -> Vec<(u64, u64)> {
    let mut ranges = Vec::new();
    let mut next = 1_u64;
    for batch in covered
        .skip_while(|batch| *batch == 0)
        .take_while(|batch| *batch <= head)
    {
        if batch > next {
            ranges.push((next, batch - 1));
        }
        match batch.checked_add(1) {
            Some(following) => next = following,
            None => return ranges,
        }
    }
    if next <= head {
        ranges.push((next, head));
    }
    ranges
}

fn validate_limit(limit: usize) -> Result<(), QueryError> {
    if limit == 0 || limit > MAXIMUM_PAGE_SIZE {
        Err(QueryError::InvalidPageSize)
    } else {
        Ok(())
    }
}

fn page<T>(records: &mut Vec<T>, limit: usize, coordinate: impl Fn(&T) -> u64) -> Page<T> {
    let has_more = records.len() > limit;
    records.truncate(limit);
    let next_before = has_more.then(|| records.last().map(&coordinate)).flatten();
    Page {
        items: std::mem::take(records),
        next_before,
    }
}

#[cfg(test)]
mod readiness_tests {
    use layerx_client::head::Head;

    use super::{incomplete_ranges, QueryError, Readiness};
    use crate::Indexer;

    fn head(sealed_batch: u64) -> Head {
        Head {
            chain_sequence: sealed_batch.saturating_mul(10),
            sealed_batch,
            finalised_checkpoint: [0x5a; 32],
        }
    }

    #[test]
    fn empty_projection_reports_incomplete_from_head() {
        let index = Indexer::new(head(7));
        assert_eq!(
            index.readiness(),
            Readiness {
                source_available: true,
                source_chain_sequence: 70,
                source_sealed_batch: 7,
                indexed_through: 0,
                incomplete_ranges: vec![(1, 7)],
                complete: false,
            }
        );
    }

    #[test]
    fn head_without_sealed_batch_is_never_complete_empty_history() {
        let index = Indexer::new(head(0));
        let readiness = index.readiness();
        assert!(readiness.incomplete_ranges.is_empty());
        assert_eq!(readiness.indexed_through, 0);
        assert!(!readiness.complete);
        assert_eq!(
            index.account_activity_page([0x01; 32], None, 10),
            Err(QueryError::IncompleteFromHead {
                source_sealed_batch: 0,
                indexed_through: 0,
            })
        );
    }

    #[test]
    fn empty_account_history_refuses_incomplete_from_head() {
        let index = Indexer::new(head(3));
        assert_eq!(
            index.account_activity_page([0x01; 32], None, 10),
            Err(QueryError::IncompleteFromHead {
                source_sealed_batch: 3,
                indexed_through: 0,
            })
        );
    }

    #[test]
    fn advancing_source_head_extends_the_incomplete_range() {
        let mut index = Indexer::new(head(2));
        assert_eq!(index.readiness().incomplete_ranges, vec![(1, 2)]);
        assert!(index.refresh_head(head(9)).is_ok());
        let readiness = index.readiness();
        assert_eq!(readiness.source_sealed_batch, 9);
        assert_eq!(readiness.source_chain_sequence, 90);
        assert_eq!(readiness.incomplete_ranges, vec![(1, 9)]);
        assert!(!readiness.complete);
    }

    #[test]
    fn invalid_page_size_is_refused_before_readiness() {
        let index = Indexer::new(head(1));
        assert_eq!(
            index.account_activity_page([0x02; 32], None, 0),
            Err(QueryError::InvalidPageSize)
        );
        assert_eq!(
            index.account_activity_page([0x02; 32], None, 101),
            Err(QueryError::InvalidPageSize)
        );
    }

    #[test]
    fn coverage_ranges_report_gaps_behind_and_ahead() {
        let ranges = incomplete_ranges([1_u64, 2, 5, 6, 9].into_iter(), 12);
        assert_eq!(ranges, vec![(3, 4), (7, 8), (10, 12)]);
    }

    #[test]
    fn coverage_ranges_full_prefix_and_missing_origin() {
        assert!(incomplete_ranges([1_u64, 2, 3].into_iter(), 3).is_empty());
        assert_eq!(incomplete_ranges([2_u64, 3].into_iter(), 3), vec![(1, 1)]);
        assert_eq!(incomplete_ranges([0_u64, 2].into_iter(), 2), vec![(1, 1)]);
    }

    #[test]
    fn coverage_ranges_ignore_batches_above_head_and_terminate_at_maximum() {
        assert_eq!(
            incomplete_ranges([1_u64, 4, 8].into_iter(), 3),
            vec![(2, 3)]
        );
        assert_eq!(
            incomplete_ranges([u64::MAX - 1, u64::MAX].into_iter(), u64::MAX),
            vec![(1, u64::MAX - 2)]
        );
    }
}
