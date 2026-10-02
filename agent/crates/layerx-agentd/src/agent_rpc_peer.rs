use crate::human::{HumanOperationError, HumanPeer};
use crate::human_runtime::{HumanAuthorityBoundary, UnifiedAgentOwner};
use crate::session_control::{AuthenticatedOwnerLookup, OperationPermit, SessionControl, SessionControlError};
use std::sync::Arc;
use crate::tenant::ResolvedPrincipal;

/// Owner operation context for one authorized agent RPC request: the retained session permit
/// and the server-restored Human subject binding re-bound to the permit's agent.
pub struct RpcOwnerContext<'p> {
    permit: &'p OperationPermit,
    peer: HumanPeer,
}

/// Binds an authorized permit to exactly one retained, subject-bound Human peer of its tenant.
///
/// # Errors
///
/// Returns `HumanOperationError::Refused` when the permit no longer resolves, when the tenant has
/// no retained subject binding or more than one, or when the authority does not bind the agent.
pub fn bind<'p, A: HumanAuthorityBoundary>(
    owner: &mut UnifiedAgentOwner<A>,
    permit: &'p OperationPermit,
) -> Result<RpcOwnerContext<'p>, HumanOperationError> {
    permit
        .boundary(&owner.session_control)
        .map_err(|_| HumanOperationError::Refused)?;
    let principal = permit.principal();
    let tenant = principal.tenant.as_str();
    let mut candidates = owner
        .retained_peers()
        .iter()
        .filter(|peer| peer.tenant == tenant && peer.subject.is_some() && peer.uid != 0);
    let (Some(retained), None) = (candidates.next(), candidates.next()) else {
        return Err(HumanOperationError::Refused);
    };
    let retained = retained.clone();
    let peer = owner.bind_rpc_subject(&retained, &principal.agent)?;
    if peer.subject.is_none() || peer.uid == 0 || peer.tenant != tenant {
        return Err(HumanOperationError::Refused);
    }
    Ok(RpcOwnerContext { permit, peer })
}

pub(crate) struct BoundRpcPeer {
    peer: HumanPeer,
    principal: ResolvedPrincipal,
    binding: Arc<()>,
}

impl BoundRpcPeer {
    pub(crate) fn peer(&self) -> &HumanPeer {
        &self.peer
    }

    pub(crate) fn matches(&self, lookup: &AuthenticatedOwnerLookup) -> bool {
        self.principal == *lookup.principal()
            && Arc::ptr_eq(&self.binding, &lookup.binding())
    }
}

pub(crate) fn bind_lookup<A: HumanAuthorityBoundary>(
    owner: &mut UnifiedAgentOwner<A>,
    lookup: &AuthenticatedOwnerLookup,
) -> Result<BoundRpcPeer, HumanOperationError> {
    let principal = lookup.principal();
    let tenant = principal.tenant.as_str();
    let mut candidates = owner.retained_peers().iter()
        .filter(|peer| peer.tenant == tenant && peer.subject.is_some() && peer.uid != 0);
    let (Some(retained), None) = (candidates.next(), candidates.next()) else {
        return Err(HumanOperationError::Refused);
    };
    let retained = retained.clone();
    let peer = owner.bind_rpc_subject(&retained, &principal.agent)?;
    if peer.subject.is_none() || peer.uid == 0 || peer.tenant != tenant {
        return Err(HumanOperationError::Refused);
    }
    Ok(BoundRpcPeer { peer, principal: principal.clone(), binding: lookup.binding() })
}

pub(crate) fn from_resolved<'p>(
    control: &SessionControl,
    permit: &'p OperationPermit,
    bound: BoundRpcPeer,
) -> Result<RpcOwnerContext<'p>, SessionControlError> {
    permit.boundary(control)?;
    if permit.principal() != &bound.principal || !permit.matches_lookup(&bound.binding)
        || bound.peer.subject.is_none() || bound.peer.uid == 0
        || bound.peer.tenant != permit.principal().tenant.as_str()
    {
        return Err(SessionControlError::Authorization(crate::tenant::AuthorizationError::NotAuthorized));
    }
    Ok(RpcOwnerContext { permit, peer: bound.peer })
}

impl RpcOwnerContext<'_> {
    #[must_use]
    pub const fn peer(&self) -> &HumanPeer {
        &self.peer
    }

    #[must_use]
    pub const fn principal(&self) -> &ResolvedPrincipal {
        self.permit.principal()
    }

    #[must_use]
    pub const fn permit(&self) -> &OperationPermit {
        self.permit
    }

    /// Runs the request's single irreversible effect under the permit's commit linearization.
    ///
    /// # Errors
    ///
    /// Returns an error if the session no longer authorizes the operation or the effect fails.
    pub fn commit<T>(
        &self,
        control: &SessionControl,
        effect: impl FnOnce(&HumanPeer) -> Result<T, SessionControlError>,
    ) -> Result<T, SessionControlError> {
        let peer = &self.peer;
        self.permit.commit(control, || effect(peer))
    }
}
