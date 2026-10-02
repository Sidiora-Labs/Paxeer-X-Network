//! Capability narrowing against core-resolved protocol authority.

use std::collections::BTreeSet;

use crate::identity::ProtocolAuthority;

use super::{Capability, Dimension};

/// Protocol authority bounds resolved from core state.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ProtocolScope {
    pub activity_types: BTreeSet<u16>,
    pub counterparties: BTreeSet<[u8; 32]>,
    pub assets: BTreeSet<[u8; 32]>,
    pub amount_ceiling: u128,
    pub expires_at_sequence: u64,
    pub not_before_ms: u64,
    pub not_after_ms: u64,
    pub enforceable_dimensions: BTreeSet<Dimension>,
}

/// Location at which a restriction is actually enforced.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum Enforcement {
    Protocol,
    DaemonOnly,
}

/// Complete enforcement classification for every capability dimension.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct NarrowingReport {
    pub dimensions: Vec<(Dimension, Enforcement)>,
}

/// Capability bound to its real protocol authority, never substituted for it.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Binding {
    pub capability: Capability,
    pub protocol_authority: ProtocolAuthority,
    pub report: NarrowingReport,
    pub enabled: bool,
}

impl Binding {
    /// Rechecks the current core scope and disables a now-wider capability.
    ///
    /// # Errors
    ///
    /// Names the first dimension wider than the current protocol scope, after disabling the
    /// binding.
    pub fn recheck(&mut self, scope: &ProtocolScope) -> Result<(), NarrowingError> {
        match validate(&self.capability, scope) {
            Ok(()) => {
                self.report = report(scope);
                Ok(())
            }
            Err(error) => {
                self.enabled = false;
                Err(error)
            }
        }
    }

    /// Returns only the protocol authority accepted by core submission.
    #[must_use]
    pub fn submission_authority(&self) -> &ProtocolAuthority {
        &self.protocol_authority
    }
}

/// Names the first dimension that would widen protocol authority.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum NarrowingError {
    Wider(Dimension),
}

pub(crate) fn check_narrowing(
    capability: &Capability,
    authority: ProtocolAuthority,
    scope: &ProtocolScope,
) -> Result<Binding, NarrowingError> {
    validate(capability, scope)?;
    Ok(Binding {
        capability: capability.clone(),
        protocol_authority: authority,
        report: report(scope),
        enabled: true,
    })
}

fn validate(capability: &Capability, scope: &ProtocolScope) -> Result<(), NarrowingError> {
    let dimensions = &capability.dimensions;
    if !dimensions.activity_types.is_subset(&scope.activity_types) {
        return Err(NarrowingError::Wider(Dimension::ActivityType));
    }
    if !dimensions.counterparties.is_subset(&scope.counterparties) {
        return Err(NarrowingError::Wider(Dimension::Counterparty));
    }
    if !dimensions.assets.is_subset(&scope.assets) {
        return Err(NarrowingError::Wider(Dimension::Asset));
    }
    if dimensions.amount_ceiling > scope.amount_ceiling {
        return Err(NarrowingError::Wider(Dimension::Amount));
    }
    if dimensions.expiry_sequence > scope.expires_at_sequence {
        return Err(NarrowingError::Wider(Dimension::Expiry));
    }
    Ok(())
}

fn report(scope: &ProtocolScope) -> NarrowingReport {
    let order = [
        Dimension::ActivityType,
        Dimension::Counterparty,
        Dimension::Asset,
        Dimension::Amount,
        Dimension::Rate,
        Dimension::Purpose,
        Dimension::Expiry,
    ];
    NarrowingReport {
        dimensions: order
            .into_iter()
            .map(|dimension| {
                let enforcement = if scope.enforceable_dimensions.contains(&dimension) {
                    Enforcement::Protocol
                } else {
                    Enforcement::DaemonOnly
                };
                (dimension, enforcement)
            })
            .collect(),
    }
}
