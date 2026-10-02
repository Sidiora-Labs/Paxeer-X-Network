//! Disclosure derivation exclusively from canonical prepared bytes.

use layerx_crypto::disclosure::{
    bind, bind_budget_mutation, BudgetStateContext, Disclosure, DisclosureError,
};
use layerx_types::payload::ModuleRegistry;

use super::{DisclosureDigest, Prepared};

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct DisclosedPreparation {
    pub disclosure: Disclosure,
    pub digest: DisclosureDigest,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum DisclosureBindingError {
    Decode(DisclosureError),
    CanonicalMismatch,
    DigestMismatch,
}

pub(crate) fn decode_and_bind(
    canonical_bytes: &[u8],
    registry: &ModuleRegistry,
    budget_context: Option<&BudgetStateContext>,
) -> Result<DisclosedPreparation, DisclosureBindingError> {
    let disclosure = match budget_context {
        Some(context) => bind_budget_mutation(canonical_bytes, registry, context),
        None => bind(canonical_bytes, registry),
    }
    .map_err(DisclosureBindingError::Decode)?;
    if disclosure
        .reencode()
        .map_err(DisclosureBindingError::Decode)?
        != canonical_bytes
    {
        return Err(DisclosureBindingError::CanonicalMismatch);
    }
    let digest = DisclosureDigest(
        disclosure
            .audit_digest()
            .map_err(DisclosureBindingError::Decode)?,
    );
    Ok(DisclosedPreparation { disclosure, digest })
}

pub(crate) fn verify_binding(prepared: &Prepared) -> Result<(), DisclosureBindingError> {
    if prepared
        .disclosure
        .reencode()
        .map_err(DisclosureBindingError::Decode)?
        != prepared.canonical_bytes
    {
        return Err(DisclosureBindingError::CanonicalMismatch);
    }
    let digest = DisclosureDigest(
        prepared
            .disclosure
            .audit_digest()
            .map_err(DisclosureBindingError::Decode)?,
    );
    if digest != prepared.disclosure_digest || digest != prepared.audit.disclosure_digest {
        return Err(DisclosureBindingError::DigestMismatch);
    }
    Ok(())
}
