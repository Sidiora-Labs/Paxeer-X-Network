use std::collections::{BTreeMap, BTreeSet};

use layerx_types::verify::VerificationLevel;

use crate::store::TenantId;

#[derive(Clone, Debug, Eq, PartialEq)]
pub enum SignerMaterial {
    LocalEncryptedReference(String),
    External {
        endpoint: String,
        public_key: [u8; 32],
    },
}

/// One signer configuration inseparably bound to its owning tenant.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct SignerBinding {
    tenant: TenantId,
    signer_id: String,
    material: SignerMaterial,
}

impl SignerBinding {
    /// Binds one signer configuration to the tenant that owns it.
    ///
    /// # Errors
    ///
    /// Returns `IsolationError::InvalidConfiguration` when the signer id, local key reference
    /// or external endpoint is empty, over 255 bytes or carries a NUL, or when an external
    /// public key is all zeroes.
    pub fn new(
        tenant: TenantId,
        signer_id: impl Into<String>,
        material: SignerMaterial,
    ) -> Result<Self, IsolationError> {
        let signer_id = signer_id.into();
        if !valid_text(&signer_id)
            || match &material {
                SignerMaterial::LocalEncryptedReference(reference) => !valid_text(reference),
                SignerMaterial::External {
                    endpoint,
                    public_key,
                } => !valid_text(endpoint) || *public_key == [0; 32],
            }
        {
            return Err(IsolationError::InvalidConfiguration);
        }
        Ok(Self {
            tenant,
            signer_id,
            material,
        })
    }

    #[must_use]
    pub const fn tenant(&self) -> &TenantId {
        &self.tenant
    }

    #[must_use]
    pub fn signer_id(&self) -> &str {
        &self.signer_id
    }

    #[must_use]
    pub const fn material(&self) -> &SignerMaterial {
        &self.material
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum RedactionPolicy {
    Strict,
    Standard,
    ReceiptOnly,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct Retention {
    pub events: u64,
    pub audit: u64,
    pub receipts: u64,
}

/// Complete tenant-specific behavior; no field falls back to another tenant.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Config {
    pub tenant: TenantId,
    pub policy_version: String,
    pub redaction: RedactionPolicy,
    pub retention: Retention,
    pub verification_default: VerificationLevel,
    pub approval_required_for: BTreeSet<u16>,
}

impl Config {
    /// Returns the configuration unchanged once every tenant-specific field is populated.
    ///
    /// # Errors
    ///
    /// Returns `IsolationError::InvalidConfiguration` for an empty, oversized or NUL-bearing
    /// policy version, or a zero event, audit or receipt retention.
    pub fn validate(self) -> Result<Self, IsolationError> {
        if !valid_text(&self.policy_version)
            || self.retention.events == 0
            || self.retention.audit == 0
            || self.retention.receipts == 0
        {
            Err(IsolationError::InvalidConfiguration)
        } else {
            Ok(self)
        }
    }
}

#[derive(Clone, Copy, Debug, Eq, Ord, PartialEq, PartialOrd)]
pub enum ChannelKind {
    Subscription,
    Stream,
    McpServer,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ChannelBinding {
    tenant: TenantId,
    kind: ChannelKind,
    channel_id: String,
}

impl ChannelBinding {
    /// Binds one channel identifier to the tenant that owns it.
    ///
    /// # Errors
    ///
    /// Returns `IsolationError::InvalidConfiguration` when the channel id is empty, longer
    /// than 255 bytes or carries a NUL.
    pub fn new(
        tenant: TenantId,
        kind: ChannelKind,
        channel_id: impl Into<String>,
    ) -> Result<Self, IsolationError> {
        let channel_id = channel_id.into();
        if !valid_text(&channel_id) {
            return Err(IsolationError::InvalidConfiguration);
        }
        Ok(Self {
            tenant,
            kind,
            channel_id,
        })
    }

    #[must_use]
    pub const fn tenant(&self) -> &TenantId {
        &self.tenant
    }

    #[must_use]
    pub const fn kind(&self) -> ChannelKind {
        self.kind
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum IsolationError {
    InvalidConfiguration,
    Duplicate,
    NotAuthorized,
}

/// Tenant-indexed signer, configuration, and channel registry.
#[derive(Debug, Default)]
pub struct TenantIsolation {
    signers: BTreeMap<(TenantId, String), SignerBinding>,
    configs: BTreeMap<TenantId, Config>,
    channels: BTreeMap<(TenantId, ChannelKind, String), ChannelBinding>,
}

impl TenantIsolation {
    /// Registers a signer binding under its tenant without displacing an existing one.
    ///
    /// # Errors
    ///
    /// Returns `IsolationError::Duplicate` when that tenant already has a signer under the
    /// same identifier; the stored binding is left untouched.
    pub fn bind_signer(&mut self, binding: SignerBinding) -> Result<(), IsolationError> {
        let key = (binding.tenant.clone(), binding.signer_id.clone());
        if self.signers.contains_key(&key) {
            return Err(IsolationError::Duplicate);
        }
        self.signers.insert(key, binding);
        Ok(())
    }

    /// Returns no existence detail for missing and cross-tenant signer identifiers.
    ///
    /// # Errors
    ///
    /// Returns `IsolationError::NotAuthorized` alike for unknown signer identifiers and for
    /// ones registered by another tenant.
    pub fn signer(
        &self,
        tenant: &TenantId,
        signer_id: &str,
    ) -> Result<&SignerBinding, IsolationError> {
        self.signers
            .get(&(tenant.clone(), signer_id.to_owned()))
            .ok_or(IsolationError::NotAuthorized)
    }

    /// Installs the complete configuration for one tenant, replacing any earlier one.
    ///
    /// # Errors
    ///
    /// Propagates `Config::validate`: `IsolationError::InvalidConfiguration` for an invalid
    /// policy version or a zero retention window, in which case nothing is stored.
    pub fn set_config(&mut self, config: Config) -> Result<(), IsolationError> {
        let config = config.validate()?;
        self.configs.insert(config.tenant.clone(), config);
        Ok(())
    }

    /// Returns the configuration owned by the authenticated tenant.
    ///
    /// # Errors
    ///
    /// Returns `IsolationError::NotAuthorized` when that tenant has no configuration set,
    /// disclosing nothing about any other tenant.
    pub fn config(&self, tenant: &TenantId) -> Result<&Config, IsolationError> {
        self.configs
            .get(tenant)
            .ok_or(IsolationError::NotAuthorized)
    }

    /// Registers a channel binding under its tenant and channel kind.
    ///
    /// # Errors
    ///
    /// Returns `IsolationError::Duplicate` when that tenant already bound the same channel
    /// identifier for the same `ChannelKind`.
    pub fn bind_channel(&mut self, binding: ChannelBinding) -> Result<(), IsolationError> {
        let key = (
            binding.tenant.clone(),
            binding.kind,
            binding.channel_id.clone(),
        );
        if self.channels.contains_key(&key) {
            return Err(IsolationError::Duplicate);
        }
        self.channels.insert(key, binding);
        Ok(())
    }

    /// Authorizes subscription cursor, backfill, stream, and MCP operations
    /// without exposing whether another tenant owns the supplied identifier.
    ///
    /// # Errors
    ///
    /// Returns `IsolationError::NotAuthorized` for unknown identifiers and equally for ones
    /// bound by another tenant or under a different `ChannelKind`.
    pub fn channel(
        &self,
        tenant: &TenantId,
        kind: ChannelKind,
        channel_id: &str,
    ) -> Result<&ChannelBinding, IsolationError> {
        self.channels
            .get(&(tenant.clone(), kind, channel_id.to_owned()))
            .ok_or(IsolationError::NotAuthorized)
    }

    /// Rejects a filter as one generic authorization failure if any referenced
    /// object belongs outside the authenticated tenant.
    ///
    /// # Errors
    ///
    /// Returns `IsolationError::NotAuthorized` as soon as one referenced tenant differs from
    /// the authenticated one, without naming which reference failed.
    #[allow(clippy::unused_self)]
    pub fn validate_filter<'a>(
        &self,
        tenant: &TenantId,
        referenced_tenants: impl IntoIterator<Item = &'a TenantId>,
    ) -> Result<(), IsolationError> {
        if referenced_tenants
            .into_iter()
            .any(|referenced| referenced != tenant)
        {
            Err(IsolationError::NotAuthorized)
        } else {
            Ok(())
        }
    }
}

fn valid_text(value: &str) -> bool {
    !value.is_empty() && value.len() <= 255 && !value.as_bytes().contains(&0)
}
