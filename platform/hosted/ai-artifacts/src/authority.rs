//! Tenant authentication, admitted publisher generations, task purpose grants
//! and declaration purpose limits. A valid signature never adds authority.
use crate::store::{Grant, ObjectRecord};
use layerx_programs_ai_market::evidence::ArtifactError;
use serde::Deserialize;
use sha2::{Digest, Sha256};
use std::collections::BTreeMap;

pub const PURPOSE_INFERENCE: u16 = 1;
pub const PURPOSE_EVALUATION: u16 = 2;
pub const PURPOSE_REDISTRIBUTION: u16 = 4;
pub const PURPOSE_TRAINING: u16 = 8;

#[derive(Clone, Debug, Deserialize)]
pub struct Config {
    pub capacity_bytes: u64,
    pub tenants: Vec<Tenant>,
    pub publishers: Vec<Publisher>,
}

#[derive(Clone, Debug, Deserialize)]
pub struct Tenant {
    pub id: String,
    pub token_sha256: String,
    pub quota_bytes: u64,
    #[serde(default)]
    pub locator_hosts: Vec<String>,
}

#[derive(Clone, Debug, Deserialize)]
pub struct Publisher {
    pub tenant: String,
    pub principal: String,
    pub generation: u64,
    pub key: String,
}

pub fn publisher_key(principal: &str, generation: u64) -> String {
    format!("{principal}:{generation}")
}

fn ct_eq(a: &[u8], b: &[u8]) -> bool {
    a.len() == b.len() && a.iter().zip(b).fold(0u8, |acc, (x, y)| acc | (x ^ y)) == 0
}

impl Config {
    pub fn authenticate(&self, token: Option<&str>) -> Result<&Tenant, ArtifactError> {
        let token = token.ok_or(ArtifactError::Unauthorized)?;
        let digest = hex::encode(Sha256::digest(token.as_bytes()));
        self.tenants
            .iter()
            .find(|t| ct_eq(t.token_sha256.as_bytes(), digest.as_bytes()))
            .ok_or(ArtifactError::Unauthorized)
    }

    pub fn tenant(&self, id: &str) -> Option<&Tenant> {
        self.tenants.iter().find(|t| t.id == id)
    }

    /// The envelope's generation and key must be the admitted, unrevoked
    /// registration owned by the calling tenant.
    pub fn admit_publisher(
        &self,
        tenant: &str,
        principal: &str,
        generation: u64,
        key: &str,
        revoked: &BTreeMap<String, u64>,
    ) -> Result<(), ArtifactError> {
        let entry = self
            .publishers
            .iter()
            .find(|p| p.principal == principal && p.generation == generation)
            .ok_or(ArtifactError::Unauthorized)?;
        if entry.tenant != tenant || entry.key != key {
            return Err(ArtifactError::Unauthorized);
        }
        if revoked.contains_key(&publisher_key(principal, generation)) {
            return Err(ArtifactError::AuthorityRevoked);
        }
        Ok(())
    }
}

pub fn purpose_bit(name: &str) -> Result<u16, ArtifactError> {
    match name {
        "inference" => Ok(PURPOSE_INFERENCE),
        "evaluation" => Ok(PURPOSE_EVALUATION),
        "redistribution" => Ok(PURPOSE_REDISTRIBUTION),
        "training" => Ok(PURPOSE_TRAINING),
        _ => Err(ArtifactError::Malformed),
    }
}

/// A declared mask bounds every use; a missing declaration never grants training.
pub fn check_purpose(bit: u16, object: &ObjectRecord) -> Result<(), ArtifactError> {
    match object.declared_purposes {
        Some(mask) if mask & bit == 0 => Err(ArtifactError::PurposeDenied),
        None if bit == PURPOSE_TRAINING => Err(ArtifactError::PurposeDenied),
        _ => Ok(()),
    }
}

pub fn check_grant(
    grant: &Grant,
    caller: &str,
    root: &str,
    task: &str,
    bit: u16,
    access_generation: u64,
    now: u64,
) -> Result<(), ArtifactError> {
    if grant.grantee != caller || grant.root != root || grant.task != task {
        return Err(ArtifactError::Unauthorized);
    }
    if grant.revoked || grant.generation != access_generation {
        return Err(ArtifactError::AuthorityRevoked);
    }
    if now >= grant.expires_at {
        return Err(ArtifactError::Expired);
    }
    if grant.purpose_mask & bit == 0 {
        return Err(ArtifactError::PurposeDenied);
    }
    Ok(())
}

/// Rights wording keeps claims distinct from legal clearance.
pub fn rights_label(status: Option<u8>) -> &'static str {
    match status {
        Some(2) => "DOCUMENTED: documents supplied, not legally adjudicated",
        Some(1) => "DECLARED: publisher claim only",
        _ => "UNDECLARED",
    }
}
