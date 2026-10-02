use std::collections::BTreeSet;
use std::fs;
use std::path::PathBuf;
use std::sync::atomic::{AtomicU64, Ordering};

use layerx_agent_api::identity::{
    ActivityType, AgentDid, Asset, CapabilityId, Counterparty, ExplicitSet, TenantId as ApiTenantId,
};
use layerx_agent_api::read::{AccountRef, ModuleRef};
use layerx_agent_api::subscription::{
    Cursor as ApiCursor, DeliveryTarget, SubscriptionCreate, SubscriptionFilter, SubscriptionId,
    SubscriptionScope, SubscriptionTarget, TenantObject,
};
use layerx_agent_api::Sequence;
use layerx_agentd::events::subscription::{Continuity, Store as SubscriptionStore, SubscriptionError};
use layerx_agentd::identity::{
    register, CoreIdentity, IdentityError, IdentityRecord, IdentityResolver, ProtocolAuthority,
};
use layerx_agentd::session::{open, OpenRequest, SessionId, SessionRegistry, Token};
use layerx_agentd::store::{Store, TenantId};
use layerx_agentd::tenant::{AuthorizationError, TenantObservability};
use layerx_types::ids::Did;
use layerx_types::result::ResultCode;
use layerx_types::verify::VerificationLevel;

static NEXT_DIRECTORY: AtomicU64 = AtomicU64::new(1);

struct BoundaryIdentity(CoreIdentity);

impl IdentityResolver for BoundaryIdentity {
    fn resolve(&mut self, _did: &Did) -> Result<Option<CoreIdentity>, IdentityError> {
        Ok(Some(self.0.clone()))
    }
}

fn text<T, E: std::fmt::Debug>(result: Result<T, E>, label: &str) -> T {
    match result {
        Ok(value) => value,
        Err(error) => panic!("{label} must be valid: {error:?}"),
    }
}

fn test_directory(name: &str) -> PathBuf {
    let sequence = NEXT_DIRECTORY.fetch_add(1, Ordering::Relaxed);
    std::env::temp_dir().join(format!(
        "layerx-agentd-w7-subscription-permit-{name}-{}-{sequence}",
        std::process::id()
    ))
}

fn durable_tenant() -> TenantId {
    text(TenantId::new("tenant-a"), "durable tenant")
}

fn api_tenant() -> ApiTenantId {
    text(ApiTenantId::new("tenant-a"), "API tenant")
}

fn tenant_object<T>(value: T) -> TenantObject<T> {
    TenantObject {
        tenant: api_tenant(),
        value,
    }
}

fn scope() -> SubscriptionScope {
    SubscriptionScope {
        tenant: api_tenant(),
        agent: text(AgentDid::new("agent-a"), "agent"),
        capability: text(CapabilityId::new("capability-a"), "capability"),
    }
}

fn filter() -> SubscriptionFilter {
    SubscriptionFilter {
        agents: ExplicitSet::allow(vec![tenant_object(text(AgentDid::new("agent-a"), "agent"))]),
        accounts: ExplicitSet::allow(vec![tenant_object(text(
            AccountRef::new("account-7"),
            "account",
        ))]),
        activity_types: ExplicitSet::allow(vec![ActivityType(9)]),
        modules: ExplicitSet::allow(vec![tenant_object(text(ModuleRef::new("asset"), "module"))]),
        assets: ExplicitSet::allow(vec![tenant_object(text(Asset::new("LXR"), "asset"))]),
        counterparties: ExplicitSet::allow(vec![tenant_object(text(
            Counterparty::new("counterparty-4"),
            "counterparty",
        ))]),
        result_classes: ExplicitSet::allow(vec![ResultCode::from_raw(0)]),
    }
}

fn request() -> SubscriptionCreate {
    SubscriptionCreate {
        scope: scope(),
        filter: filter(),
        start: ApiCursor(Sequence(0)),
        delivery_target: text(DeliveryTarget::new("uds://consumer-a"), "delivery target"),
    }
}

fn subscription_id(name: &str) -> SubscriptionId {
    text(SubscriptionId::new(name), "subscription identifier")
}

fn target(id: &SubscriptionId) -> SubscriptionTarget {
    SubscriptionTarget {
        scope: scope(),
        subscription_id: id.clone(),
    }
}

fn session_identity(store: &mut Store) -> IdentityRecord {
    let mut boundary = BoundaryIdentity(CoreIdentity {
        canonical_bytes: b"w7-subscription-identity".to_vec(),
        head_sequence: 10,
        revocation_sequence: 1,
        verification_level: VerificationLevel::STATE_PROVEN,
        frozen: false,
        authorities: vec![ProtocolAuthority::SessionKey([4; 32])],
    });
    text(
        register(
            store,
            durable_tenant(),
            text(Did::new(b"agent-a"), "agent DID"),
            &mut boundary,
        ),
        "identity",
    )
}

fn session(
    store: &mut Store,
    sessions: &mut SessionRegistry,
    identity: &IdentityRecord,
    id: u8,
) -> Token {
    text(
        open(
            store,
            sessions,
            identity,
            OpenRequest {
                session_id: SessionId([id; 32]),
                token_id: [id.wrapping_add(1); 32],
                tenant: durable_tenant(),
                agent: text(Did::new(b"agent-a"), "agent DID"),
                authority: ProtocolAuthority::SessionKey([4; 32]),
                permitted_activity_types: BTreeSet::from([9]),
                scopes: BTreeSet::from(["subscribe".to_owned()]),
                expiry_sequence: 100,
                expiry_seconds: None,
                opening_client: "w7-subscription-permit-suite".to_owned(),
                policy_version: "policy-v1".to_owned(),
            },
            10,
        ),
        "session",
    )
}

struct Setup {
    root: PathBuf,
    sessions: SessionRegistry,
    owner: Token,
    sibling: Token,
    subscriptions: SubscriptionStore,
    observability: TenantObservability,
}

impl Setup {
    fn new(name: &str) -> Self {
        let root = test_directory(name);
        let mut session_store = text(Store::open(root.join("sessions")), "session store");
        let identity = session_identity(&mut session_store);
        let mut sessions = SessionRegistry::default();
        let owner = session(&mut session_store, &mut sessions, &identity, 1);
        let sibling = session(&mut session_store, &mut sessions, &identity, 2);
        let subscriptions = text(
            SubscriptionStore::open(
                text(Store::open(root.join("events")), "event store"),
                durable_tenant(),
            ),
            "subscription store",
        );
        Self {
            root,
            sessions,
            owner,
            sibling,
            subscriptions,
            observability: TenantObservability::default(),
        }
    }
}

impl Drop for Setup {
    fn drop(&mut self) {
        let _ = fs::remove_dir_all(&self.root);
    }
}

#[test]
fn permit_bound_list_returns_only_the_owner_sessions_records() {
    let mut setup = Setup::new("list");
    let owner_id = subscription_id("subscription-owner");
    let sibling_id = subscription_id("subscription-sibling");
    let owned = text(
        setup.subscriptions.create_authorized(
            &setup.sessions,
            &setup.owner,
            &mut setup.observability,
            11,
            owner_id,
            request(),
        ),
        "owner create",
    );
    let sibling_owned = text(
        setup.subscriptions.create_authorized(
            &setup.sessions,
            &setup.sibling,
            &mut setup.observability,
            11,
            sibling_id,
            request(),
        ),
        "sibling create",
    );

    assert_eq!(
        text(
            setup.subscriptions.list_authorized(
                &setup.sessions,
                &setup.owner,
                &mut setup.observability,
                11,
                &scope(),
            ),
            "owner list",
        ),
        vec![owned]
    );
    assert_eq!(
        text(
            setup.subscriptions.list_authorized(
                &setup.sessions,
                &setup.sibling,
                &mut setup.observability,
                11,
                &scope(),
            ),
            "sibling list",
        ),
        vec![sibling_owned]
    );
    assert!(setup.subscriptions.list(&scope()).is_empty());
}

#[test]
fn health_continuity_authorized_returns_the_bound_record_and_its_continuity() {
    let mut setup = Setup::new("health");
    let id = subscription_id("subscription-health");
    let created = text(
        setup.subscriptions.create_authorized(
            &setup.sessions,
            &setup.owner,
            &mut setup.observability,
            11,
            id.clone(),
            request(),
        ),
        "create",
    );

    let (record, continuity) = text(
        setup.subscriptions.health_continuity_authorized(
            &setup.sessions,
            &setup.owner,
            &mut setup.observability,
            11,
            &target(&id),
        ),
        "health continuity",
    );
    assert_eq!(record, created);
    assert_eq!(
        record,
        text(
            setup.subscriptions.health_authorized(
                &setup.sessions,
                &setup.owner,
                &mut setup.observability,
                11,
                &target(&id),
            ),
            "health",
        )
    );
    assert_eq!(continuity, Continuity::Healthy);

    assert!(matches!(
        setup.subscriptions.health_continuity_authorized(
            &setup.sessions,
            &setup.sibling,
            &mut setup.observability,
            11,
            &target(&id),
        ),
        Err(SubscriptionError::Authorization(
            AuthorizationError::NotAuthorized
        ))
    ));
    assert!(matches!(
        setup.subscriptions.health_continuity_authorized(
            &setup.sessions,
            &setup.owner,
            &mut setup.observability,
            11,
            &target(&subscription_id("subscription-unknown")),
        ),
        Err(SubscriptionError::NotFound)
    ));
    assert!(matches!(
        setup.subscriptions.continuity(&target(&id)),
        Err(SubscriptionError::AuthorizationRequired)
    ));
}
