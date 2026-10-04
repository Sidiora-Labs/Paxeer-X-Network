//! Capability intersection and explicit gap reporting.

use std::collections::BTreeSet;

use layerx_types::error::LayerError;

use super::schema::{lni_schema_v1, Capability, Schema};

/// Negotiated capability intersection with every known and unknown gap kept.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Capabilities {
    available: BTreeSet<Capability>,
    unavailable: BTreeSet<Capability>,
    unknown_advertised: Vec<String>,
}

impl Capabilities {
    /// Computes the intersection of the built-in schema and a node's names.
    #[must_use]
    pub fn negotiate(advertised: &[String]) -> Self {
        Self::negotiate_with_schema(advertised, lni_schema_v1())
    }

    pub fn negotiate_with_schema(advertised: &[String], schema: &Schema) -> Self {
        let mut available = BTreeSet::new();
        let mut unknown_advertised = BTreeSet::new();
        for name in advertised {
            if let Some(capability) = capability_by_name(name, schema) {
                available.insert(capability);
            } else {
                unknown_advertised.insert(name.clone());
            }
        }
        let unavailable = schema
            .capabilities
            .iter()
            .copied()
            .filter(|capability| !available.contains(capability))
            .collect();
        Self {
            available,
            unavailable,
            unknown_advertised: unknown_advertised.into_iter().collect(),
        }
    }

    /// Returns whether the node and client both know the capability.
    #[must_use]
    pub fn contains(&self, capability: Capability) -> bool {
        self.available.contains(&capability)
    }

    /// Requires a negotiated capability without reconstructing a substitute.
    ///
    /// # Errors
    ///
    /// Returns an unavailable-capability layer error naming the exact gap.
    pub fn require(&self, capability: Capability) -> Result<(), LayerError> {
        if self.contains(capability) {
            Ok(())
        } else {
            Err(LayerError::UnavailableCapability {
                capability: capability.name().to_owned(),
            })
        }
    }

    /// All schema capabilities exposed by the node.
    #[must_use]
    pub const fn available(&self) -> &BTreeSet<Capability> {
        &self.available
    }

    /// All schema capabilities omitted by the node.
    #[must_use]
    pub const fn unavailable(&self) -> &BTreeSet<Capability> {
        &self.unavailable
    }

    /// Advertised names this client version does not interpret.
    #[must_use]
    pub fn unknown_advertised(&self) -> &[String] {
        &self.unknown_advertised
    }
}

fn capability_by_name(name: &str, schema: &Schema) -> Option<Capability> {
    schema
        .capabilities
        .iter()
        .copied()
        .find(|capability| capability.name() == name)
}
