/// Boundary head coordinates paired with the exact indexed position.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct Freshness {
    pub observed_chain_sequence: u64,
    pub observed_sealed_batch: u64,
    pub observed_finalised_checkpoint: [u8; 32],
    pub indexed_batch: Option<u64>,
    pub indexed_checkpoint: Option<[u8; 32]>,
}

impl Freshness {
    /// Number of sealed batches not yet present in the index.
    #[must_use]
    pub const fn batches_behind(self) -> u64 {
        match self.indexed_batch {
            Some(batch) => self.observed_sealed_batch.saturating_sub(batch),
            None => self.observed_sealed_batch.saturating_add(1),
        }
    }

    /// Whether both the boundary head batch and its finalised checkpoint exist.
    #[must_use]
    pub fn is_current(self) -> bool {
        self.indexed_batch == Some(self.observed_sealed_batch)
            && self.indexed_checkpoint == Some(self.observed_finalised_checkpoint)
    }
}

/// One query answer carrying the freshness statement every page must render.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Indexed<T> {
    pub value: T,
    pub freshness: Freshness,
}
