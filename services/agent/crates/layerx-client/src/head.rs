//! Monotonic node-head and authorised sequencer-key tracking.

use crate::lni::handshake::NodeInfo;

/// Freshness coordinates attached to every head-sensitive result.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct Head {
    pub chain_sequence: u64,
    pub sealed_batch: u64,
    pub finalised_checkpoint: [u8; 32],
}

/// One advertised key's accepted batch interval.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct SequencerKeyRange {
    pub public_key: [u8; 32],
    pub first_batch: u64,
    pub last_batch: Option<u64>,
}

/// A peer attempted to move a monotonic freshness coordinate backwards.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum HeadError {
    SequenceRegression { current: u64, peer: u64 },
    BatchRegression { current: u64, peer: u64 },
    UnadvertisedSequencerKey { batch: u64, key: [u8; 32] },
}

/// Monotonic freshness and key history derived only from accepted handshakes.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct HeadTracker {
    head: Head,
    keys: Vec<SequencerKeyRange>,
}

impl HeadTracker {
    /// Creates a tracker from the first accepted startup handshake.
    #[must_use]
    pub fn new(node: &NodeInfo) -> Self {
        Self {
            head: head(node),
            keys: vec![SequencerKeyRange {
                public_key: node.authorised_sequencer_key,
                first_batch: 0,
                last_batch: None,
            }],
        }
    }

    /// Advances freshness and records a key transition without permitting a
    /// sequence or batch regression.
    ///
    /// # Errors
    ///
    /// Refuses either monotonic coordinate moving backwards.
    pub fn update(&mut self, node: &NodeInfo) -> Result<Head, HeadError> {
        if node.chain_head_sequence < self.head.chain_sequence {
            return Err(HeadError::SequenceRegression {
                current: self.head.chain_sequence,
                peer: node.chain_head_sequence,
            });
        }
        if node.latest_sealed_batch < self.head.sealed_batch {
            return Err(HeadError::BatchRegression {
                current: self.head.sealed_batch,
                peer: node.latest_sealed_batch,
            });
        }
        let current_key = self.keys.last().map(|range| range.public_key);
        if current_key != Some(node.authorised_sequencer_key) {
            if let Some(range) = self.keys.last_mut() {
                range.last_batch = node.latest_sealed_batch.checked_sub(1);
            }
            self.keys.push(SequencerKeyRange {
                public_key: node.authorised_sequencer_key,
                first_batch: node.latest_sealed_batch,
                last_batch: None,
            });
        }
        self.head = head(node);
        Ok(self.head)
    }

    /// Current freshness coordinates.
    #[must_use]
    pub const fn current(&self) -> Head {
        self.head
    }

    /// Complete non-overlapping key history observed at the boundary.
    #[must_use]
    pub fn sequencer_keys(&self) -> &[SequencerKeyRange] {
        &self.keys
    }

    /// Requires that a key was advertised for the receipt's batch.
    ///
    /// # Errors
    ///
    /// Refuses a key absent from the tracked interval for that batch.
    pub fn require_sequencer_key(&self, batch: u64, key: [u8; 32]) -> Result<(), HeadError> {
        let advertised = self.keys.iter().any(|range| {
            range.public_key == key
                && batch >= range.first_batch
                && range.last_batch.is_none_or(|last| batch <= last)
        });
        if advertised {
            Ok(())
        } else {
            Err(HeadError::UnadvertisedSequencerKey { batch, key })
        }
    }
}

const fn head(node: &NodeInfo) -> Head {
    Head {
        chain_sequence: node.chain_head_sequence,
        sealed_batch: node.latest_sealed_batch,
        finalised_checkpoint: node.latest_finalised_checkpoint,
    }
}
