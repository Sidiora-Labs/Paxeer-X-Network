//! Persistent capability derivation graph and subtree revocation.

use std::collections::{BTreeMap, BTreeSet};

use crate::store::{ObjectKind, Store, StoreError, TenantId, TenantKey};

use super::{decode, encode, Capability, CapabilityError, CapabilityId, Dimension};

#[derive(Clone, Debug, Eq, PartialEq)]
struct Node {
    capability: Capability,
    parent: Option<CapabilityId>,
    revoked: bool,
}

/// Tenant-local capability derivation graph.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct CapabilityGraph {
    tenant: TenantId,
    nodes: BTreeMap<CapabilityId, Node>,
}

impl CapabilityGraph {
    #[must_use]
    pub fn new(tenant: TenantId) -> Self {
        Self {
            tenant,
            nodes: BTreeMap::new(),
        }
    }

    /// Installs a capability as an unparented root of this tenant's graph.
    ///
    /// # Errors
    ///
    /// Refuses a capability owned by another tenant, or an identifier the graph already holds.
    pub fn add_root(&mut self, capability: Capability) -> Result<(), AttenuationError> {
        if capability.tenant != self.tenant {
            return Err(AttenuationError::Tenant);
        }
        if self.nodes.contains_key(&capability.id) {
            return Err(AttenuationError::Duplicate);
        }
        self.nodes.insert(
            capability.id,
            Node {
                capability,
                parent: None,
                revoked: false,
            },
        );
        Ok(())
    }

    #[must_use]
    pub fn chain(&self, id: CapabilityId) -> Option<Vec<CapabilityId>> {
        let mut current = Some(id);
        let mut chain = Vec::new();
        while let Some(value) = current {
            let node = self.nodes.get(&value)?;
            chain.push(value);
            current = node.parent;
        }
        chain.reverse();
        Some(chain)
    }

    #[must_use]
    pub fn is_enabled(&self, id: CapabilityId) -> bool {
        let mut current = Some(id);
        while let Some(value) = current {
            let Some(node) = self.nodes.get(&value) else {
                return false;
            };
            if node.revoked {
                return false;
            }
            current = node.parent;
        }
        true
    }

    /// Persists the derivation graph as one daemon-local object.
    ///
    /// # Errors
    ///
    /// Returns a size overflow when the node count or an encoded capability exceeds its length
    /// bound, a capability encoding failure, or the store failure that prevented the write.
    pub fn persist(&self, store: &mut Store) -> Result<(), AttenuationError> {
        let key = TenantKey::new(
            self.tenant.clone(),
            ObjectKind::Configuration,
            b"capability-derivation-graph".to_vec(),
        )?;
        store.put_local(key, encode_graph(self)?)?;
        Ok(())
    }

    /// Restores a tenant's derivation graph, or `None` when none was ever persisted.
    ///
    /// # Errors
    ///
    /// Returns a corruption failure when the persisted graph is truncated, carries an invalid
    /// parent or revocation tag, or has trailing bytes, and the store failure otherwise.
    pub fn restore(store: &Store, tenant: TenantId) -> Result<Option<Self>, AttenuationError> {
        let key = TenantKey::new(
            tenant.clone(),
            ObjectKind::Configuration,
            b"capability-derivation-graph".to_vec(),
        )?;
        store
            .get(&key)
            .map(|value| decode_graph(tenant, value.bytes()))
            .transpose()
    }
}

/// Prepared work bound to a capability in the graph.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct RevocableActivity {
    pub capability_id: CapabilityId,
    pub submitted: bool,
    pub cancelled: bool,
}

/// Exact effect of one subtree revocation.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct RevocationResult {
    pub revoked: Vec<CapabilityId>,
    pub cancelled_unsubmitted: usize,
}

#[derive(Debug)]
pub enum AttenuationError {
    MissingParent,
    Duplicate,
    Tenant,
    Wider(Dimension),
    Store(StoreError),
    Capability(CapabilityError),
    Corrupt,
    SizeOverflow,
}

impl PartialEq for AttenuationError {
    fn eq(&self, other: &Self) -> bool {
        match (self, other) {
            (Self::Wider(left), Self::Wider(right)) => left == right,
            (Self::Store(_), Self::Store(_)) | (Self::Capability(_), Self::Capability(_)) => true,
            _ => std::mem::discriminant(self) == std::mem::discriminant(other),
        }
    }
}

impl Eq for AttenuationError {}

impl From<StoreError> for AttenuationError {
    fn from(value: StoreError) -> Self {
        Self::Store(value)
    }
}

impl From<CapabilityError> for AttenuationError {
    fn from(value: CapabilityError) -> Self {
        Self::Capability(value)
    }
}

pub(crate) fn derive(
    graph: &mut CapabilityGraph,
    parent: CapabilityId,
    child: Capability,
) -> Result<(), AttenuationError> {
    let parent_node = graph
        .nodes
        .get(&parent)
        .ok_or(AttenuationError::MissingParent)?;
    if child.tenant != graph.tenant {
        return Err(AttenuationError::Tenant);
    }
    if graph.nodes.contains_key(&child.id) {
        return Err(AttenuationError::Duplicate);
    }
    require_subset(&child, &parent_node.capability)?;
    graph.nodes.insert(
        child.id,
        Node {
            capability: child,
            parent: Some(parent),
            revoked: false,
        },
    );
    Ok(())
}

pub(crate) fn revoke(
    graph: &mut CapabilityGraph,
    root: CapabilityId,
    activities: &mut [RevocableActivity],
) -> Result<RevocationResult, AttenuationError> {
    if !graph.nodes.contains_key(&root) {
        return Err(AttenuationError::MissingParent);
    }
    let revoked: Vec<_> = graph
        .nodes
        .keys()
        .copied()
        .filter(|id| graph.chain(*id).is_some_and(|chain| chain.contains(&root)))
        .collect();
    for id in &revoked {
        if let Some(node) = graph.nodes.get_mut(id) {
            node.revoked = true;
        }
    }
    let revoked_set: BTreeSet<_> = revoked.iter().copied().collect();
    let mut cancelled_unsubmitted = 0_usize;
    for activity in activities {
        if revoked_set.contains(&activity.capability_id) && !activity.submitted {
            activity.cancelled = true;
            cancelled_unsubmitted += 1;
        }
    }
    Ok(RevocationResult {
        revoked,
        cancelled_unsubmitted,
    })
}

fn require_subset(child: &Capability, parent: &Capability) -> Result<(), AttenuationError> {
    let child = &child.dimensions;
    let parent = &parent.dimensions;
    if !child.activity_types.is_subset(&parent.activity_types) {
        return Err(AttenuationError::Wider(Dimension::ActivityType));
    }
    if !child.counterparties.is_subset(&parent.counterparties) {
        return Err(AttenuationError::Wider(Dimension::Counterparty));
    }
    if !child.assets.is_subset(&parent.assets) {
        return Err(AttenuationError::Wider(Dimension::Asset));
    }
    if child.amount_ceiling > parent.amount_ceiling {
        return Err(AttenuationError::Wider(Dimension::Amount));
    }
    if child.rate_ceiling.maximum_uses > parent.rate_ceiling.maximum_uses
        || child.rate_ceiling.window_sequences < parent.rate_ceiling.window_sequences
    {
        return Err(AttenuationError::Wider(Dimension::Rate));
    }
    if !child.purposes.is_subset(&parent.purposes) {
        return Err(AttenuationError::Wider(Dimension::Purpose));
    }
    if child.expiry_sequence > parent.expiry_sequence {
        return Err(AttenuationError::Wider(Dimension::Expiry));
    }
    Ok(())
}

fn encode_graph(graph: &CapabilityGraph) -> Result<Vec<u8>, AttenuationError> {
    let count = u16::try_from(graph.nodes.len()).map_err(|_| AttenuationError::SizeOverflow)?;
    let mut bytes = Vec::new();
    bytes.extend_from_slice(&count.to_be_bytes());
    for node in graph.nodes.values() {
        let capability = encode(&node.capability)?;
        let length = u32::try_from(capability.len()).map_err(|_| AttenuationError::SizeOverflow)?;
        bytes.extend_from_slice(&length.to_be_bytes());
        bytes.extend_from_slice(&capability);
        match node.parent {
            Some(parent) => {
                bytes.push(1);
                bytes.extend_from_slice(&parent.0);
            }
            None => bytes.push(0),
        }
        bytes.push(u8::from(node.revoked));
    }
    Ok(bytes)
}

fn decode_graph(tenant: TenantId, bytes: &[u8]) -> Result<CapabilityGraph, AttenuationError> {
    let mut offset = 0_usize;
    let count = usize::from(read_u16(bytes, &mut offset)?);
    let mut graph = CapabilityGraph::new(tenant);
    for _ in 0..count {
        let length = usize::try_from(read_u32(bytes, &mut offset)?)
            .map_err(|_| AttenuationError::SizeOverflow)?;
        let end = offset
            .checked_add(length)
            .ok_or(AttenuationError::Corrupt)?;
        let capability = decode(bytes.get(offset..end).ok_or(AttenuationError::Corrupt)?)?;
        offset = end;
        let has_parent = *bytes.get(offset).ok_or(AttenuationError::Corrupt)?;
        offset += 1;
        let parent = if has_parent == 1 {
            let mut id = [0_u8; 32];
            id.copy_from_slice(
                bytes
                    .get(offset..offset + 32)
                    .ok_or(AttenuationError::Corrupt)?,
            );
            offset += 32;
            Some(CapabilityId(id))
        } else if has_parent == 0 {
            None
        } else {
            return Err(AttenuationError::Corrupt);
        };
        let revoked = match bytes.get(offset) {
            Some(0) => false,
            Some(1) => true,
            _ => return Err(AttenuationError::Corrupt),
        };
        offset += 1;
        graph.nodes.insert(
            capability.id,
            Node {
                capability,
                parent,
                revoked,
            },
        );
    }
    if offset != bytes.len() {
        return Err(AttenuationError::Corrupt);
    }
    Ok(graph)
}

fn read_u16(bytes: &[u8], offset: &mut usize) -> Result<u16, AttenuationError> {
    let mut value = [0_u8; 2];
    value.copy_from_slice(
        bytes
            .get(*offset..*offset + 2)
            .ok_or(AttenuationError::Corrupt)?,
    );
    *offset += 2;
    Ok(u16::from_be_bytes(value))
}

fn read_u32(bytes: &[u8], offset: &mut usize) -> Result<u32, AttenuationError> {
    let mut value = [0_u8; 4];
    value.copy_from_slice(
        bytes
            .get(*offset..*offset + 4)
            .ok_or(AttenuationError::Corrupt)?,
    );
    *offset += 4;
    Ok(u32::from_be_bytes(value))
}

pub fn require_native_subset(
    child: &super::timed::NativeTimedCapabilityV1,
    parent: &super::timed::NativeTimedCapabilityV1,
) -> Result<(), super::timed::TimedError> {
    use super::timed::{require_subset, TimedError};
    child.validate()?;
    parent.validate()?;
    if child.record.tenant != parent.record.tenant
        || child.record.agent != parent.record.agent
        || child.record.authority != parent.record.authority
        || child.record.parent != Some(parent.record.id)
        || child.record.id == parent.record.id
        || child.record.created_at_ms < parent.record.created_at_ms
        || child.record.created_at_sequence < parent.record.created_at_sequence
    {
        return Err(TimedError::Malformed);
    }
    require_subset(&child.record, &parent.record)?;
    if !child.activities.is_subset(&parent.activities) {
        return Err(TimedError::Wider(Dimension::ActivityType));
    }
    if !child.purpose_commitments.is_subset(&parent.purpose_commitments) {
        return Err(TimedError::Wider(Dimension::Purpose));
    }
    if child.spend_ceilings.iter().any(|(key, amount)| {
        parent.spend_ceilings.get(key).is_none_or(|limit| amount > limit)
    }) {
        return Err(TimedError::Wider(Dimension::Amount));
    }
    Ok(())
}
