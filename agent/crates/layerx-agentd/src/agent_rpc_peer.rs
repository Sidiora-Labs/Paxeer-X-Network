use crate::human::{HumanOperationError, HumanPeer};
use crate::human_runtime::{HumanAuthorityBoundary, UnifiedAgentOwner};
use crate::session_control::{OperationPermit, SessionControl, SessionControlError};
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
        self,
        control: &SessionControl,
        effect: impl FnOnce(&HumanPeer) -> Result<T, SessionControlError>,
    ) -> Result<T, SessionControlError> {
        let peer = &self.peer;
        self.permit.commit(control, || effect(peer))
    }
}
