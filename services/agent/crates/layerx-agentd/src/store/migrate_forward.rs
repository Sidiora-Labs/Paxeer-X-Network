//! Public forward-only migration entry point.

use std::path::Path;

use super::migration::{migrate_store, MigrationError};
pub use super::migration::{MigrationReport, MigrationStep};

/// Migrates every supported prior schema through each explicit step.
///
/// # Errors
///
/// Refuses corrupt, unsupported-old, and newer stores. Migration transforms only daemon
/// metadata and preserves core-produced values byte-for-byte without a derivation callback.
pub fn migrate_forward(root: impl AsRef<Path>) -> Result<MigrationReport, MigrationError> {
    migrate_store(root.as_ref())
}
