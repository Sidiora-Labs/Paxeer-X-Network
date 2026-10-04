//! Authenticated approval operations over the policy hold registry.

use std::collections::BTreeMap;
use std::sync::Mutex;

use layerx_agent_api::prepare::{Disclosure, Prepared};
use sha2::{Digest as _, Sha256};

use crate::budget::{self, BudgetLimiter, ReleaseKind};
use crate::policy::approval::{
    ApprovalError as RegistryError, ApprovalRegistry, ApprovalSnapshot, ApprovalState, ApproverId,
    ReleasedApproval,
};
use crate::store::{Store, TenantId};

mod events;
mod expiry;
pub(crate) mod program_requirement;
pub(crate) mod native_program;
pub mod native_program_presentation;
pub(crate) mod native_effect;
pub(crate) use expiry::PreparedDecision;

#[cfg(test)]
#[path = "../../tests/approval_ordering/mod.rs"]
mod ordering_tests;

#[cfg(test)]
mod receipt_settlement_tests {
    use std::path::PathBuf;
    use std::sync::{Arc, Mutex};

    use layerx_agent_api::identity::{ActivityType, AgentDid, Asset, AuthorityRef, ExplicitSet};
    use layerx_agent_api::prepare::{
        CanonicalBytes, Disclosure, IdempotencyRef, PreparationRef, Prepared, SigningPreimage,
    };
    use layerx_agent_api::{Amount, TimestampSeconds};
    use layerx_types::ids::Did;
    use sha2::{Digest as _, Sha256};

    use super::{
        ApprovalExpiry, ApprovalOperationError, ApprovalOutcome, ApprovalService,
        ApprovalSubmissionQueue, DecisionKey, DecisionRequest,
    };
    use crate::budget::{
        create_daemon_limit, daemon_limit_id, daemon_limits, reserve, BudgetLimiter,
        CoreTimestampMs, DaemonLimitRecord, LimitId, LimitRefusal, ReservationRequest,
    };
    use crate::capability::CapabilityId;
    use crate::policy::approval::{
        hold_reserved, released_storage_key, ApprovalContext, ApprovalError as RegistryError,
        ApprovalRegistry, ApprovalState, ApproverId,
    };
    use crate::session::SessionId;
    use crate::store::{ObjectKind, Store, TenantId, TenantKey};

    struct Fixture {
        store: Arc<Mutex<Store>>,
        limiter: BudgetLimiter,
        queue: ApprovalSubmissionQueue,
        limits: Vec<LimitId>,
        root: PathBuf,
    }

    impl Drop for Fixture {
        fn drop(&mut self) {
            let _ = std::fs::remove_dir_all(&self.root);
        }
    }

    fn tenant() -> TenantId {
        TenantId::new("tenant-receipt-settlement").unwrap_or_else(|error| panic!("{error}"))
    }

    fn hex(bytes: [u8; 32]) -> String {
        bytes.iter().map(|byte| format!("{byte:02x}")).collect()
    }

    fn prepared(id: u8) -> Prepared {
        let bytes = format!("receipt-settlement-preparation-{id}").into_bytes();
        let digest: [u8; 32] = Sha256::digest(&bytes).into();
        Prepared {
            preparation_ref: PreparationRef::new(format!("prepared-{id}"))
                .unwrap_or_else(|error| panic!("preparation: {error:?}")),
            unsigned_canonical_bytes: CanonicalBytes::new(bytes)
                .unwrap_or_else(|error| panic!("bytes: {error:?}")),
            signing_preimage: SigningPreimage::new(vec![id; 32])
                .unwrap_or_else(|error| panic!("preimage: {error:?}")),
            disclosure: Disclosure {
                canonical_digest: digest,
                activity_type: ActivityType(7),
                actor: AgentDid::new("did:layerx:receipt-settlement")
                    .unwrap_or_else(|error| panic!("actor: {error:?}")),
                authority: AuthorityRef::new("session-key")
                    .unwrap_or_else(|error| panic!("authority: {error:?}")),
                counterparties: ExplicitSet::deny_all(),
                amounts: ExplicitSet::deny_all(),
                asset: Asset::new("LXP").unwrap_or_else(|error| panic!("asset: {error:?}")),
                fee_limit: Amount(2),
                expiry: TimestampSeconds(40),
                idempotency_key: IdempotencyRef::new(hex([id; 32]))
                    .unwrap_or_else(|error| panic!("activity key: {error:?}")),
            },
            expiry: TimestampSeconds(40),
        }
    }

    fn daemon_limit(store: &mut Store, limiter: &BudgetLimiter, id: u8) -> LimitId {
        create_daemon_limit(
            store,
            limiter,
            DaemonLimitRecord {
                tenant: tenant(),
                budget_id: [id; 32],
                limit_id: daemon_limit_id([id; 32]),
                agent_digest: [7; 32],
                asset: [8; 32],
                ceiling: 1_000,
                consumed: 0,
                expiry_ms: 1_000_000,
                revoked: false,
                mutation_key: [id; 32],
                body_digest: [id; 32],
                revoke_key: [0; 32],
            },
            CoreTimestampMs(1),
        )
        .unwrap_or_else(|error| panic!("daemon limit: {error:?}"))
        .limit_id
    }

    fn held(label: &str, id: u8) -> (Fixture, ApprovalRegistry, ApprovalExpiry) {
        let root = std::env::temp_dir().join(format!(
            "layerx-receipt-settlement-{label}-{}",
            std::process::id()
        ));
        let _ = std::fs::remove_dir_all(&root);
        let store = Arc::new(Mutex::new(
            Store::open(&root).unwrap_or_else(|error| panic!("store: {error}")),
        ));
        let limiter = BudgetLimiter::new(Vec::new()).unwrap_or_else(|error| panic!("{error:?}"));
        let limits = {
            let mut guard = store.lock().unwrap_or_else(|_| panic!("store lock"));
            vec![
                daemon_limit(&mut guard, &limiter, 21),
                daemon_limit(&mut guard, &limiter, 22),
            ]
        };
        let reservation = reserve(
            &limiter,
            &ReservationRequest {
                id: [id; 32],
                amount: 10,
                expiry_sequence: 40,
                current_sequence: 10,
                applicable_limits: limits.clone(),
            },
        )
        .unwrap_or_else(|error| panic!("reserve: {error:?}"));
        let registry = ApprovalRegistry::with_store(Arc::clone(&store));
        hold_reserved(
            &registry,
            ApprovalContext {
                tenant: tenant(),
                agent: Did::new(b"did:layerx:receipt-settlement")
                    .unwrap_or_else(|error| panic!("agent: {error:?}")),
                session: SessionId([2; 32]),
                capability: CapabilityId([3; 32]),
                policy_version: "policy-v3".to_owned(),
                request_id: [id; 32],
            },
            prepared(id),
            10,
            40,
            &reservation,
        )
        .unwrap_or_else(|error| panic!("hold: {error:?}"));
        let expiry = ApprovalExpiry::from_shared_store(Arc::clone(&store));
        (
            Fixture {
                store,
                limiter,
                queue: ApprovalSubmissionQueue::default(),
                limits,
                root,
            },
            registry,
            expiry,
        )
    }

    fn released(label: &str, id: u8) -> Fixture {
        let (fixture, registry, expiry) = held(label, id);
        let decision = ApprovalService::new(&registry, &fixture.limiter, &expiry)
            .approve(
                DecisionRequest {
                    tenant: &tenant(),
                    approval_id: [id; 32],
                    idempotency_key: &DecisionKey::new(format!("grant-{id}"))
                        .unwrap_or_else(|error| panic!("decision key: {error:?}")),
                    approver: ApproverId::new("human:grant")
                        .unwrap_or_else(|error| panic!("approver: {error:?}")),
                    current_sequence: 11,
                },
                &prepared(id),
                &fixture.queue,
            )
            .unwrap_or_else(|error| panic!("approve: {error:?}"));
        assert_eq!(decision.outcome, ApprovalOutcome::Granted);
        fixture
    }

    fn durable_consumed(root: &std::path::Path) -> Vec<u128> {
        let store = Store::open(root).unwrap_or_else(|error| panic!("reopen: {error}"));
        daemon_limits(&store, &tenant())
            .unwrap_or_else(|error| panic!("daemon limits: {error:?}"))
            .iter()
            .map(|record| record.consumed)
            .collect()
    }

    #[test]
    fn executed_receipt_persists_consumption_per_reserved_limit() {
        let fixture = released("executed", 31);
        let released_key = released_storage_key(&tenant(), [31; 32])
            .unwrap_or_else(|error| panic!("key: {error:?}"));
        {
            let mut store = fixture
                .store
                .lock()
                .unwrap_or_else(|_| panic!("store lock"));
            assert_eq!(
                fixture.queue.settle_verified(
                    &tenant(),
                    [31; 32],
                    0,
                    12,
                    &mut store,
                    &fixture.limiter
                ),
                Ok(true)
            );
            assert!(store.get(&released_key).is_none());
        }
        assert_eq!(durable_consumed(&fixture.root), vec![10, 10]);
        for limit in &fixture.limits {
            assert_eq!(fixture.limiter.consumed(*limit), Ok(10));
        }
        assert_eq!(fixture.limiter.held_reservations(), Ok(0));
        assert!(fixture.queue.is_empty());
    }

    #[test]
    fn failed_receipt_releases_holds_without_consumption() {
        let fixture = released("failed", 32);
        let released_key = released_storage_key(&tenant(), [32; 32])
            .unwrap_or_else(|error| panic!("key: {error:?}"));
        assert_eq!(fixture.limiter.held_reservations(), Ok(2));
        {
            let mut store = fixture
                .store
                .lock()
                .unwrap_or_else(|_| panic!("store lock"));
            assert_eq!(
                fixture.queue.settle_verified(
                    &tenant(),
                    [32; 32],
                    7,
                    12,
                    &mut store,
                    &fixture.limiter
                ),
                Ok(true)
            );
            assert!(store.get(&released_key).is_none());
        }
        assert_eq!(durable_consumed(&fixture.root), vec![0, 0]);
        for limit in &fixture.limits {
            assert_eq!(fixture.limiter.consumed(*limit), Ok(0));
        }
        assert_eq!(fixture.limiter.held_reservations(), Ok(0));
        assert!(fixture.queue.is_empty());
    }

    #[test]
    fn unknown_limit_reservation_refused_before_write() {
        let fixture = released("unknown", 33);
        let released_key = released_storage_key(&tenant(), [33; 32])
            .unwrap_or_else(|error| panic!("key: {error:?}"));
        let restarted = BudgetLimiter::new(Vec::new()).unwrap_or_else(|error| panic!("{error:?}"));
        {
            let mut store = fixture
                .store
                .lock()
                .unwrap_or_else(|_| panic!("store lock"));
            let before = store.get(&released_key).map(|value| value.bytes().to_vec());
            assert!(before.is_some());
            let refused =
                fixture
                    .queue
                    .settle_verified(&tenant(), [33; 32], 0, 12, &mut store, &restarted);
            assert!(matches!(
                refused,
                Err(ApprovalOperationError::Reservation(LimitRefusal::UnknownLimit(id)))
                    if fixture.limits.contains(&id)
            ));
            assert_eq!(
                store.get(&released_key).map(|value| value.bytes().to_vec()),
                before
            );
        }
        assert_eq!(durable_consumed(&fixture.root), vec![0, 0]);
        assert_eq!(restarted.held_reservations(), Ok(0));
        assert_eq!(fixture.limiter.held_reservations(), Ok(2));
        for limit in &fixture.limits {
            assert_eq!(fixture.limiter.consumed(*limit), Ok(0));
        }
        assert_eq!(fixture.queue.len(), 1);
    }

    fn hold_key(store: &Store) -> TenantKey {
        let ids: Vec<Vec<u8>> = store
            .list_object_ids(&tenant(), ObjectKind::PreparedActivity)
            .into_iter()
            .filter(|id| id.starts_with(b"approval-hold-v1:"))
            .collect();
        assert_eq!(ids.len(), 1);
        TenantKey::new(tenant(), ObjectKind::PreparedActivity, ids[0].clone())
            .unwrap_or_else(|error| panic!("hold key: {error:?}"))
    }

    fn approval_entries(store: &Store) -> Vec<(TenantKey, Option<Vec<u8>>)> {
        [
            ObjectKind::PreparedActivity,
            ObjectKind::Idempotency,
            ObjectKind::Outbox,
        ]
        .into_iter()
        .flat_map(|kind| {
            store
                .list_object_ids(&tenant(), kind)
                .into_iter()
                .map(move |id| {
                    TenantKey::new(tenant(), kind, id)
                        .unwrap_or_else(|error| panic!("key: {error:?}"))
                })
        })
        .map(|key| {
            let bytes = store.get(&key).map(|value| value.bytes().to_vec());
            (key, bytes)
        })
        .collect()
    }

    fn reject(
        registry: &ApprovalRegistry,
        limiter: &BudgetLimiter,
        expiry: &ApprovalExpiry,
        id: u8,
    ) -> Result<super::ApprovalDecision, ApprovalOperationError> {
        let key = DecisionKey::new(format!("reject-{id}"))
            .unwrap_or_else(|error| panic!("decision key: {error:?}"));
        ApprovalService::new(registry, limiter, expiry).reject(DecisionRequest {
            tenant: &tenant(),
            approval_id: [id; 32],
            idempotency_key: &key,
            approver: ApproverId::new("human:reject")
                .unwrap_or_else(|error| panic!("approver: {error:?}")),
            current_sequence: 11,
        })
    }

    #[test]
    fn reject_publishes_the_staged_release_only_after_the_terminal_record() {
        let (fixture, registry, expiry) = held("reject", 34);
        {
            let mut store = fixture
                .store
                .lock()
                .unwrap_or_else(|_| panic!("store lock"));
            let hold = hold_key(&store);
            assert!(matches!(store.remove_local(&hold), Ok(true)));
        }
        assert!(matches!(
            reject(&registry, &fixture.limiter, &expiry, 34),
            Err(ApprovalOperationError::Registry(
                RegistryError::CorruptRecord
            ))
        ));
        assert_eq!(fixture.limiter.held_reservations(), Ok(2));
        for limit in &fixture.limits {
            assert_eq!(
                fixture
                    .limiter
                    .held_limits([34; 32])
                    .map(|held| held.contains(limit)),
                Ok(true)
            );
        }

        let (fixture, registry, expiry) = held("reject-complete", 35);
        let decision = reject(&registry, &fixture.limiter, &expiry, 35)
            .unwrap_or_else(|error| panic!("reject: {error:?}"));
        assert_eq!(decision.outcome, ApprovalOutcome::Rejected);
        assert_eq!(fixture.limiter.held_reservations(), Ok(0));
        for limit in &fixture.limits {
            assert_eq!(fixture.limiter.consumed(*limit), Ok(0));
        }
        assert_eq!(durable_consumed(&fixture.root), vec![0, 0]);
        let snapshot = registry
            .get_scoped(&tenant(), [35; 32], 12)
            .unwrap_or_else(|error| panic!("snapshot: {error:?}"));
        assert_eq!(snapshot.state, ApprovalState::Rejected);
        let store = fixture
            .store
            .lock()
            .unwrap_or_else(|_| panic!("store lock"));
        assert!(store
            .list_object_ids(&tenant(), ObjectKind::PreparedActivity)
            .iter()
            .all(|id| !id.starts_with(b"approval-hold-v1:")));
    }

    #[test]
    fn reject_under_an_unknown_limit_is_refused_before_any_write() {
        let (fixture, registry, expiry) = held("reject-unknown", 36);
        let restarted = BudgetLimiter::new(Vec::new()).unwrap_or_else(|error| panic!("{error:?}"));
        let before = approval_entries(
            &fixture
                .store
                .lock()
                .unwrap_or_else(|_| panic!("store lock")),
        );
        assert!(matches!(
            reject(&registry, &restarted, &expiry, 36),
            Err(ApprovalOperationError::Reservation(LimitRefusal::UnknownLimit(id)))
                if fixture.limits.contains(&id)
        ));
        assert_eq!(
            approval_entries(
                &fixture
                    .store
                    .lock()
                    .unwrap_or_else(|_| panic!("store lock"))
            ),
            before
        );
        assert_eq!(restarted.held_reservations(), Ok(0));
        assert_eq!(fixture.limiter.held_reservations(), Ok(2));
        let snapshot = registry
            .get_scoped(&tenant(), [36; 32], 12)
            .unwrap_or_else(|error| panic!("snapshot: {error:?}"));
        assert_eq!(snapshot.state, ApprovalState::AwaitingApproval);
    }
}

#[cfg(test)]
mod lock_order_tests {
    use std::path::PathBuf;
    use std::sync::{mpsc, Arc, Barrier, Mutex};
    use std::time::Duration;

    use layerx_agent_api::identity::{ActivityType, AgentDid, Asset, AuthorityRef, ExplicitSet};
    use layerx_agent_api::prepare::{
        CanonicalBytes, Disclosure, IdempotencyRef, PreparationRef, Prepared, SigningPreimage,
    };
    use layerx_agent_api::{Amount, TimestampSeconds};
    use layerx_types::ids::Did;
    use sha2::{Digest as _, Sha256};

    use super::{
        ApprovalExpiry, ApprovalOperationError, ApprovalOutcome, ApprovalService,
        ApprovalSubmissionQueue, DecisionKey, DecisionRequest,
    };
    use crate::budget::{
        create_daemon_limit, daemon_limit_id, reserve, BudgetLimiter, CoreTimestampMs,
        DaemonLimitRecord, LimitId, LimitRefusal, ReservationRequest,
    };
    use crate::capability::CapabilityId;
    use crate::policy::approval::{hold_reserved, ApprovalContext, ApprovalRegistry, ApproverId};
    use crate::session::SessionId;
    use crate::store::{ObjectKind, Store, TenantId, TenantKey};

    struct Shared {
        root: PathBuf,
        store: Arc<Mutex<Store>>,
        registry: ApprovalRegistry,
        limiter: BudgetLimiter,
        expiry: ApprovalExpiry,
        queue: ApprovalSubmissionQueue,
        limits: Vec<LimitId>,
    }

    impl Drop for Shared {
        fn drop(&mut self) {
            let _ = std::fs::remove_dir_all(&self.root);
        }
    }

    fn tenant() -> TenantId {
        TenantId::new("tenant-lock-order").unwrap_or_else(|error| panic!("{error}"))
    }

    fn prepared(id: u8) -> Prepared {
        let bytes = format!("lock-order-preparation-{id}").into_bytes();
        let digest: [u8; 32] = Sha256::digest(&bytes).into();
        Prepared {
            preparation_ref: PreparationRef::new(format!("prepared-{id}"))
                .unwrap_or_else(|error| panic!("preparation: {error:?}")),
            unsigned_canonical_bytes: CanonicalBytes::new(bytes)
                .unwrap_or_else(|error| panic!("bytes: {error:?}")),
            signing_preimage: SigningPreimage::new(vec![id; 32])
                .unwrap_or_else(|error| panic!("preimage: {error:?}")),
            disclosure: Disclosure {
                canonical_digest: digest,
                activity_type: ActivityType(7),
                actor: AgentDid::new("did:layerx:lock-order")
                    .unwrap_or_else(|error| panic!("actor: {error:?}")),
                authority: AuthorityRef::new("session-key")
                    .unwrap_or_else(|error| panic!("authority: {error:?}")),
                counterparties: ExplicitSet::deny_all(),
                amounts: ExplicitSet::deny_all(),
                asset: Asset::new("LXP").unwrap_or_else(|error| panic!("asset: {error:?}")),
                fee_limit: Amount(2),
                expiry: TimestampSeconds(40),
                idempotency_key: IdempotencyRef::new(
                    [id; 32]
                        .iter()
                        .map(|byte| format!("{byte:02x}"))
                        .collect::<String>(),
                )
                .unwrap_or_else(|error| panic!("activity key: {error:?}")),
            },
            expiry: TimestampSeconds(40),
        }
    }

    fn decision_key(label: &str) -> DecisionKey {
        DecisionKey::new(label).unwrap_or_else(|error| panic!("decision key: {error:?}"))
    }

    fn request<'a>(
        tenant: &'a TenantId,
        id: u8,
        idempotency_key: &'a DecisionKey,
        current_sequence: u64,
    ) -> DecisionRequest<'a> {
        DecisionRequest {
            tenant,
            approval_id: [id; 32],
            idempotency_key,
            approver: ApproverId::new("human:lock-order")
                .unwrap_or_else(|error| panic!("approver: {error:?}")),
            current_sequence,
        }
    }

    fn shared(label: &str) -> Shared {
        let root = std::env::temp_dir().join(format!(
            "layerx-approval-lock-order-{label}-{}",
            std::process::id()
        ));
        let _ = std::fs::remove_dir_all(&root);
        let store = Arc::new(Mutex::new(
            Store::open(&root).unwrap_or_else(|error| panic!("store: {error}")),
        ));
        let limiter = BudgetLimiter::new(Vec::new()).unwrap_or_else(|error| panic!("{error:?}"));
        let limits = {
            let mut guard = store.lock().unwrap_or_else(|_| panic!("store lock"));
            [41_u8, 42]
                .iter()
                .map(|id| {
                    create_daemon_limit(
                        &mut guard,
                        &limiter,
                        DaemonLimitRecord {
                            tenant: tenant(),
                            budget_id: [*id; 32],
                            limit_id: daemon_limit_id([*id; 32]),
                            agent_digest: [7; 32],
                            asset: [8; 32],
                            ceiling: 1_000,
                            consumed: 0,
                            expiry_ms: 1_000_000,
                            revoked: false,
                            mutation_key: [*id; 32],
                            body_digest: [*id; 32],
                            revoke_key: [0; 32],
                        },
                        CoreTimestampMs(1),
                    )
                    .unwrap_or_else(|error| panic!("daemon limit: {error:?}"))
                    .limit_id
                })
                .collect::<Vec<_>>()
        };
        Shared {
            root,
            registry: ApprovalRegistry::with_store(Arc::clone(&store)),
            expiry: ApprovalExpiry::from_shared_store(Arc::clone(&store)),
            store,
            limiter,
            queue: ApprovalSubmissionQueue::default(),
            limits,
        }
    }

    fn hold(shared: &Shared, id: u8) {
        let reservation = reserve(
            &shared.limiter,
            &ReservationRequest {
                id: [id; 32],
                amount: 10,
                expiry_sequence: 40,
                current_sequence: 10,
                applicable_limits: shared.limits.clone(),
            },
        )
        .unwrap_or_else(|error| panic!("reserve: {error:?}"));
        hold_reserved(
            &shared.registry,
            ApprovalContext {
                tenant: tenant(),
                agent: Did::new(b"did:layerx:lock-order")
                    .unwrap_or_else(|error| panic!("agent: {error:?}")),
                session: SessionId([2; 32]),
                capability: CapabilityId([3; 32]),
                policy_version: "policy-v3".to_owned(),
                request_id: [id; 32],
            },
            prepared(id),
            10,
            40,
            &reservation,
        )
        .unwrap_or_else(|error| panic!("hold: {error:?}"));
    }

    fn hold_records(store: &Store, tenant: &TenantId) -> Vec<(Vec<u8>, Option<Vec<u8>>)> {
        store
            .list_object_ids(tenant, ObjectKind::PreparedActivity)
            .into_iter()
            .map(|id| {
                let key = TenantKey::new(tenant.clone(), ObjectKind::PreparedActivity, id.clone())
                    .unwrap_or_else(|error| panic!("key: {error:?}"));
                (id, store.get(&key).map(|value| value.bytes().to_vec()))
            })
            .collect()
    }

    #[test]
    fn reject_and_receipt_settlement_under_one_limit_complete_concurrently() {
        for round in 0..16_u8 {
            let shared = Arc::new(shared(&format!("concurrent-{round}")));
            hold(&shared, 51);
            hold(&shared, 52);
            let owner = tenant();
            let grant = decision_key("grant-52");
            let granted = ApprovalService::new(&shared.registry, &shared.limiter, &shared.expiry)
                .approve(
                    request(&owner, 52, &grant, 11),
                    &prepared(52),
                    &shared.queue,
                )
                .unwrap_or_else(|error| panic!("approve: {error:?}"));
            assert_eq!(granted.outcome, ApprovalOutcome::Granted);
            let barrier = Arc::new(Barrier::new(2));
            let (sender, receiver) = mpsc::channel();
            let rejecting = {
                let (shared, barrier, sender) =
                    (Arc::clone(&shared), Arc::clone(&barrier), sender.clone());
                std::thread::spawn(move || {
                    let tenant = tenant();
                    let reject = decision_key("reject-51");
                    barrier.wait();
                    let rejected =
                        ApprovalService::new(&shared.registry, &shared.limiter, &shared.expiry)
                            .reject(request(&tenant, 51, &reject, 12))
                            .map(|decision| decision.outcome == ApprovalOutcome::Rejected);
                    let _ = sender.send(("reject", rejected));
                })
            };
            let settling = {
                let (shared, barrier) = (Arc::clone(&shared), Arc::clone(&barrier));
                std::thread::spawn(move || {
                    barrier.wait();
                    let mut store = shared.store.lock().unwrap_or_else(|_| panic!("store lock"));
                    let settled = shared.queue.settle_verified(
                        &tenant(),
                        [52; 32],
                        0,
                        12,
                        &mut store,
                        &shared.limiter,
                    );
                    drop(store);
                    let _ = sender.send(("settle", settled));
                })
            };
            let mut results = (0..2)
                .map(|_| {
                    receiver
                        .recv_timeout(Duration::from_secs(10))
                        .unwrap_or_else(|error| panic!("round {round} stalled: {error}"))
                })
                .collect::<Vec<_>>();
            results.sort_by_key(|(name, _)| *name);
            assert_eq!(results, vec![("reject", Ok(true)), ("settle", Ok(true))]);
            rejecting.join().unwrap_or_else(|_| panic!("reject thread"));
            settling.join().unwrap_or_else(|_| panic!("settle thread"));
            assert_eq!(shared.limiter.held_reservations(), Ok(0));
            for limit in &shared.limits {
                assert_eq!(shared.limiter.consumed(*limit), Ok(10));
            }
            assert!(shared.queue.is_empty());
            assert!(hold_records(
                &shared.store.lock().unwrap_or_else(|_| panic!("store lock")),
                &owner
            )
            .is_empty());
        }
    }

    #[test]
    fn reject_refuses_unknown_limit_reservation_before_any_write() {
        let shared = shared("unknown");
        hold(&shared, 61);
        let tenant = tenant();
        let restarted = BudgetLimiter::new(Vec::new()).unwrap_or_else(|error| panic!("{error:?}"));
        let before = hold_records(
            &shared.store.lock().unwrap_or_else(|_| panic!("store lock")),
            &tenant,
        );
        assert_eq!(before.len(), 1);
        let reject = decision_key("reject-61");
        let refused = ApprovalService::new(&shared.registry, &restarted, &shared.expiry)
            .reject(request(&tenant, 61, &reject, 12));
        assert!(matches!(
            refused,
            Err(ApprovalOperationError::Reservation(LimitRefusal::UnknownLimit(id)))
                if shared.limits.contains(&id)
        ));
        assert_eq!(
            hold_records(
                &shared.store.lock().unwrap_or_else(|_| panic!("store lock")),
                &tenant
            ),
            before
        );
        assert_eq!(shared.registry.audit_entry([61; 32]), Ok(None));
        assert!(shared.registry.claim_scoped(&tenant, [61; 32], 12).is_ok());
        assert_eq!(shared.limiter.held_reservations(), Ok(2));
        assert_eq!(restarted.held_reservations(), Ok(0));
    }
}

#[cfg(test)]
mod reject_persist_tests {
    use std::path::PathBuf;
    use std::sync::{Arc, Mutex};

    use layerx_agent_api::identity::{ActivityType, AgentDid, Asset, AuthorityRef, ExplicitSet};
    use layerx_agent_api::prepare::{
        CanonicalBytes, Disclosure, IdempotencyRef, PreparationRef, Prepared, SigningPreimage,
    };
    use layerx_agent_api::{Amount, TimestampSeconds};
    use layerx_types::ids::Did;
    use sha2::{Digest as _, Sha256};

    use super::{
        ApprovalExpiry, ApprovalOperationError, ApprovalOutcome, ApprovalService, DecisionKey,
        DecisionRequest,
    };
    use crate::budget::{
        create_daemon_limit, daemon_limit_id, reserve, BudgetLimiter, CoreTimestampMs,
        DaemonLimitRecord, LimitId, LimitRefusal, ReservationRequest,
    };
    use crate::capability::CapabilityId;
    use crate::policy::approval::{hold_reserved, ApprovalContext, ApprovalRegistry, ApproverId};
    use crate::session::SessionId;
    use crate::store::{ObjectKind, Store, TenantId, TenantKey};

    struct Shared {
        root: PathBuf,
        store: Arc<Mutex<Store>>,
        registry: ApprovalRegistry,
        limiter: BudgetLimiter,
        expiry: ApprovalExpiry,
        limits: Vec<LimitId>,
    }

    impl Drop for Shared {
        fn drop(&mut self) {
            let _ = std::fs::remove_dir_all(&self.root);
        }
    }

    fn tenant() -> TenantId {
        TenantId::new("tenant-lock-order").unwrap_or_else(|error| panic!("{error}"))
    }

    fn prepared(id: u8) -> Prepared {
        let bytes = format!("lock-order-preparation-{id}").into_bytes();
        let digest: [u8; 32] = Sha256::digest(&bytes).into();
        Prepared {
            preparation_ref: PreparationRef::new(format!("prepared-{id}"))
                .unwrap_or_else(|error| panic!("preparation: {error:?}")),
            unsigned_canonical_bytes: CanonicalBytes::new(bytes)
                .unwrap_or_else(|error| panic!("bytes: {error:?}")),
            signing_preimage: SigningPreimage::new(vec![id; 32])
                .unwrap_or_else(|error| panic!("preimage: {error:?}")),
            disclosure: Disclosure {
                canonical_digest: digest,
                activity_type: ActivityType(7),
                actor: AgentDid::new("did:layerx:lock-order")
                    .unwrap_or_else(|error| panic!("actor: {error:?}")),
                authority: AuthorityRef::new("session-key")
                    .unwrap_or_else(|error| panic!("authority: {error:?}")),
                counterparties: ExplicitSet::deny_all(),
                amounts: ExplicitSet::deny_all(),
                asset: Asset::new("LXP").unwrap_or_else(|error| panic!("asset: {error:?}")),
                fee_limit: Amount(2),
                expiry: TimestampSeconds(40),
                idempotency_key: IdempotencyRef::new(
                    [id; 32]
                        .iter()
                        .map(|byte| format!("{byte:02x}"))
                        .collect::<String>(),
                )
                .unwrap_or_else(|error| panic!("activity key: {error:?}")),
            },
            expiry: TimestampSeconds(40),
        }
    }

    fn decision_key(label: &str) -> DecisionKey {
        DecisionKey::new(label).unwrap_or_else(|error| panic!("decision key: {error:?}"))
    }

    fn request<'a>(
        tenant: &'a TenantId,
        id: u8,
        idempotency_key: &'a DecisionKey,
        current_sequence: u64,
    ) -> DecisionRequest<'a> {
        DecisionRequest {
            tenant,
            approval_id: [id; 32],
            idempotency_key,
            approver: ApproverId::new("human:lock-order")
                .unwrap_or_else(|error| panic!("approver: {error:?}")),
            current_sequence,
        }
    }

    fn shared(label: &str) -> Shared {
        let root = std::env::temp_dir().join(format!(
            "layerx-approval-reject-persist-{label}-{}",
            std::process::id()
        ));
        let _ = std::fs::remove_dir_all(&root);
        let store = Arc::new(Mutex::new(
            Store::open(&root).unwrap_or_else(|error| panic!("store: {error}")),
        ));
        let limiter = BudgetLimiter::new(Vec::new()).unwrap_or_else(|error| panic!("{error:?}"));
        let limits = {
            let mut guard = store.lock().unwrap_or_else(|_| panic!("store lock"));
            [41_u8, 42]
                .iter()
                .map(|id| {
                    create_daemon_limit(
                        &mut guard,
                        &limiter,
                        DaemonLimitRecord {
                            tenant: tenant(),
                            budget_id: [*id; 32],
                            limit_id: daemon_limit_id([*id; 32]),
                            agent_digest: [7; 32],
                            asset: [8; 32],
                            ceiling: 1_000,
                            consumed: 0,
                            expiry_ms: 1_000_000,
                            revoked: false,
                            mutation_key: [*id; 32],
                            body_digest: [*id; 32],
                            revoke_key: [0; 32],
                        },
                        CoreTimestampMs(1),
                    )
                    .unwrap_or_else(|error| panic!("daemon limit: {error:?}"))
                    .limit_id
                })
                .collect::<Vec<_>>()
        };
        Shared {
            root,
            registry: ApprovalRegistry::with_store(Arc::clone(&store)),
            expiry: ApprovalExpiry::from_shared_store(Arc::clone(&store)),
            store,
            limiter,
            limits,
        }
    }

    fn hold(shared: &Shared, id: u8) {
        let reservation = reserve(
            &shared.limiter,
            &ReservationRequest {
                id: [id; 32],
                amount: 10,
                expiry_sequence: 40,
                current_sequence: 10,
                applicable_limits: shared.limits.clone(),
            },
        )
        .unwrap_or_else(|error| panic!("reserve: {error:?}"));
        hold_reserved(
            &shared.registry,
            ApprovalContext {
                tenant: tenant(),
                agent: Did::new(b"did:layerx:lock-order")
                    .unwrap_or_else(|error| panic!("agent: {error:?}")),
                session: SessionId([2; 32]),
                capability: CapabilityId([3; 32]),
                policy_version: "policy-v3".to_owned(),
                request_id: [id; 32],
            },
            prepared(id),
            10,
            40,
            &reservation,
        )
        .unwrap_or_else(|error| panic!("hold: {error:?}"));
    }

    fn hold_records(store: &Store, tenant: &TenantId) -> Vec<(Vec<u8>, Option<Vec<u8>>)> {
        store
            .list_object_ids(tenant, ObjectKind::PreparedActivity)
            .into_iter()
            .map(|id| {
                let key = TenantKey::new(tenant.clone(), ObjectKind::PreparedActivity, id.clone())
                    .unwrap_or_else(|error| panic!("key: {error:?}"));
                (id, store.get(&key).map(|value| value.bytes().to_vec()))
            })
            .collect()
    }

    fn approval_entries(store: &Store, tenant: &TenantId) -> Vec<(TenantKey, Option<Vec<u8>>)> {
        [
            ObjectKind::PreparedActivity,
            ObjectKind::Idempotency,
            ObjectKind::Outbox,
        ]
        .into_iter()
        .flat_map(|kind| {
            store
                .list_object_ids(tenant, kind)
                .into_iter()
                .map(move |id| {
                    TenantKey::new(tenant.clone(), kind, id)
                        .unwrap_or_else(|error| panic!("key: {error:?}"))
                })
        })
        .map(|key| {
            let bytes = store.get(&key).map(|value| value.bytes().to_vec());
            (key, bytes)
        })
        .collect()
    }

    #[test]
    fn reject_with_a_failed_persist_leaves_the_hold_claimable_without_a_rejected_record() {
        let shared = shared("failed-persist");
        hold(&shared, 71);
        let tenant = tenant();
        let service = ApprovalService::new(&shared.registry, &shared.limiter, &shared.expiry);
        service
            .get(&tenant, [71; 32], 11)
            .unwrap_or_else(|error| panic!("get: {error:?}"));
        let reject = decision_key("reject-71");
        std::fs::remove_dir_all(&shared.root).unwrap_or_else(|error| panic!("remove: {error}"));
        assert!(matches!(
            service.reject(request(&tenant, 71, &reject, 12)),
            Err(ApprovalOperationError::Registry(
                crate::policy::approval::ApprovalError::Unavailable
            ))
        ));
        std::fs::create_dir_all(&shared.root).unwrap_or_else(|error| panic!("create: {error}"));
        assert_eq!(shared.expiry.repeated(&tenant, [71; 32], &reject), Ok(None));
        assert_eq!(shared.registry.audit_entry([71; 32]), Ok(None));
        assert_eq!(shared.limiter.held_reservations(), Ok(2));
        assert_eq!(
            hold_records(
                &shared.store.lock().unwrap_or_else(|_| panic!("store lock")),
                &tenant
            )
            .len(),
            1
        );
        assert!(shared.registry.claim_scoped(&tenant, [71; 32], 12).is_ok());
        shared
            .registry
            .abort_claim([71; 32])
            .unwrap_or_else(|error| panic!("abort: {error:?}"));
        let rejected = service
            .reject(request(&tenant, 71, &reject, 12))
            .unwrap_or_else(|error| panic!("reject: {error:?}"));
        assert_eq!(rejected.outcome, ApprovalOutcome::Rejected);
        assert_eq!(shared.limiter.held_reservations(), Ok(0));
        assert!(matches!(
            shared.expiry.repeated(&tenant, [71; 32], &reject),
            Ok(Some(decision)) if decision.outcome == ApprovalOutcome::Rejected
        ));
    }

    #[test]
    fn reject_under_an_unknown_limit_leaves_every_record_identical_and_retry_is_fresh() {
        let shared = shared("unknown");
        hold(&shared, 61);
        let tenant = tenant();
        let restarted = BudgetLimiter::new(Vec::new()).unwrap_or_else(|error| panic!("{error:?}"));
        let entries = || {
            approval_entries(
                &shared.store.lock().unwrap_or_else(|_| panic!("store lock")),
                &tenant,
            )
        };
        let before = entries();
        assert_eq!(before.len(), 1);
        let reject = decision_key("reject-61");
        for _ in 0..2 {
            let refused = ApprovalService::new(&shared.registry, &restarted, &shared.expiry)
                .reject(request(&tenant, 61, &reject, 12));
            assert!(matches!(
                refused,
                Err(ApprovalOperationError::Reservation(LimitRefusal::UnknownLimit(id)))
                    if shared.limits.contains(&id)
            ));
            assert_eq!(entries(), before);
            assert_eq!(shared.expiry.repeated(&tenant, [61; 32], &reject), Ok(None));
            assert_eq!(shared.registry.audit_entry([61; 32]), Ok(None));
        }
        assert_eq!(shared.limiter.held_reservations(), Ok(2));
        assert_eq!(restarted.held_reservations(), Ok(0));
        let rejected = ApprovalService::new(&shared.registry, &shared.limiter, &shared.expiry)
            .reject(request(&tenant, 61, &reject, 12))
            .unwrap_or_else(|error| panic!("reject: {error:?}"));
        assert_eq!(rejected.outcome, ApprovalOutcome::Rejected);
        assert_eq!(shared.limiter.held_reservations(), Ok(0));
        assert!(hold_records(
            &shared.store.lock().unwrap_or_else(|_| panic!("store lock")),
            &tenant
        )
        .is_empty());
    }
}

#[cfg(test)]
mod defective_persist_tests {
    use std::path::PathBuf;
    use std::sync::{Arc, Mutex};

    use layerx_agent_api::identity::{ActivityType, AgentDid, Asset, AuthorityRef, ExplicitSet};
    use layerx_agent_api::prepare::{
        CanonicalBytes, Disclosure, IdempotencyRef, PreparationRef, Prepared, SigningPreimage,
    };
    use layerx_agent_api::{Amount, TimestampSeconds};
    use layerx_types::ids::Did;
    use sha2::{Digest as _, Sha256};

    use super::{
        ApprovalExpiry, ApprovalOperationError, ApprovalOutcome, ApprovalService,
        ApprovalSubmissionQueue, DecisionKey, DecisionRequest,
    };
    use crate::budget::{
        create_daemon_limit, daemon_limit_id, reserve, BudgetLimiter, CoreTimestampMs,
        DaemonLimitRecord, LimitId, LimitRefusal, ReservationRequest,
    };
    use crate::capability::CapabilityId;
    use crate::policy::approval::{
        hold_reserved, ApprovalContext, ApprovalRegistry, ApprovalState, ApproverId,
    };
    use crate::session::SessionId;
    use crate::store::{ObjectKind, Store, TenantId, TenantKey};

    struct Shared {
        root: PathBuf,
        store: Arc<Mutex<Store>>,
        registry: ApprovalRegistry,
        limiter: BudgetLimiter,
        expiry: ApprovalExpiry,
        limits: Vec<LimitId>,
    }

    impl Drop for Shared {
        fn drop(&mut self) {
            let _ = std::fs::remove_dir_all(&self.root);
        }
    }

    fn tenant() -> TenantId {
        TenantId::new("tenant-lock-order").unwrap_or_else(|error| panic!("{error}"))
    }

    fn prepared(id: u8) -> Prepared {
        let bytes = format!("lock-order-preparation-{id}").into_bytes();
        let digest: [u8; 32] = Sha256::digest(&bytes).into();
        Prepared {
            preparation_ref: PreparationRef::new(format!("prepared-{id}"))
                .unwrap_or_else(|error| panic!("preparation: {error:?}")),
            unsigned_canonical_bytes: CanonicalBytes::new(bytes)
                .unwrap_or_else(|error| panic!("bytes: {error:?}")),
            signing_preimage: SigningPreimage::new(vec![id; 32])
                .unwrap_or_else(|error| panic!("preimage: {error:?}")),
            disclosure: Disclosure {
                canonical_digest: digest,
                activity_type: ActivityType(7),
                actor: AgentDid::new("did:layerx:lock-order")
                    .unwrap_or_else(|error| panic!("actor: {error:?}")),
                authority: AuthorityRef::new("session-key")
                    .unwrap_or_else(|error| panic!("authority: {error:?}")),
                counterparties: ExplicitSet::deny_all(),
                amounts: ExplicitSet::deny_all(),
                asset: Asset::new("LXP").unwrap_or_else(|error| panic!("asset: {error:?}")),
                fee_limit: Amount(2),
                expiry: TimestampSeconds(40),
                idempotency_key: IdempotencyRef::new(
                    [id; 32]
                        .iter()
                        .map(|byte| format!("{byte:02x}"))
                        .collect::<String>(),
                )
                .unwrap_or_else(|error| panic!("activity key: {error:?}")),
            },
            expiry: TimestampSeconds(40),
        }
    }

    fn decision_key(label: &str) -> DecisionKey {
        DecisionKey::new(label).unwrap_or_else(|error| panic!("decision key: {error:?}"))
    }

    fn request<'a>(
        tenant: &'a TenantId,
        id: u8,
        idempotency_key: &'a DecisionKey,
        current_sequence: u64,
    ) -> DecisionRequest<'a> {
        DecisionRequest {
            tenant,
            approval_id: [id; 32],
            idempotency_key,
            approver: ApproverId::new("human:lock-order")
                .unwrap_or_else(|error| panic!("approver: {error:?}")),
            current_sequence,
        }
    }

    fn shared(label: &str) -> Shared {
        let root = std::env::temp_dir().join(format!(
            "layerx-approval-defective-persist-{label}-{}",
            std::process::id()
        ));
        let _ = std::fs::remove_dir_all(&root);
        let store = Arc::new(Mutex::new(
            Store::open(&root).unwrap_or_else(|error| panic!("store: {error}")),
        ));
        let limiter = BudgetLimiter::new(Vec::new()).unwrap_or_else(|error| panic!("{error:?}"));
        let limits = {
            let mut guard = store.lock().unwrap_or_else(|_| panic!("store lock"));
            [41_u8, 42]
                .iter()
                .map(|id| {
                    create_daemon_limit(
                        &mut guard,
                        &limiter,
                        DaemonLimitRecord {
                            tenant: tenant(),
                            budget_id: [*id; 32],
                            limit_id: daemon_limit_id([*id; 32]),
                            agent_digest: [7; 32],
                            asset: [8; 32],
                            ceiling: 1_000,
                            consumed: 0,
                            expiry_ms: 1_000_000,
                            revoked: false,
                            mutation_key: [*id; 32],
                            body_digest: [*id; 32],
                            revoke_key: [0; 32],
                        },
                        CoreTimestampMs(1),
                    )
                    .unwrap_or_else(|error| panic!("daemon limit: {error:?}"))
                    .limit_id
                })
                .collect::<Vec<_>>()
        };
        Shared {
            root,
            registry: ApprovalRegistry::with_store(Arc::clone(&store)),
            expiry: ApprovalExpiry::from_shared_store(Arc::clone(&store)),
            store,
            limiter,
            limits,
        }
    }

    fn hold(shared: &Shared, id: u8) {
        let reservation = reserve(
            &shared.limiter,
            &ReservationRequest {
                id: [id; 32],
                amount: 10,
                expiry_sequence: 40,
                current_sequence: 10,
                applicable_limits: shared.limits.clone(),
            },
        )
        .unwrap_or_else(|error| panic!("reserve: {error:?}"));
        hold_reserved(
            &shared.registry,
            ApprovalContext {
                tenant: tenant(),
                agent: Did::new(b"did:layerx:lock-order")
                    .unwrap_or_else(|error| panic!("agent: {error:?}")),
                session: SessionId([2; 32]),
                capability: CapabilityId([3; 32]),
                policy_version: "policy-v3".to_owned(),
                request_id: [id; 32],
            },
            prepared(id),
            10,
            40,
            &reservation,
        )
        .unwrap_or_else(|error| panic!("hold: {error:?}"));
    }

    fn hold_records(store: &Store, tenant: &TenantId) -> Vec<(Vec<u8>, Option<Vec<u8>>)> {
        store
            .list_object_ids(tenant, ObjectKind::PreparedActivity)
            .into_iter()
            .map(|id| {
                let key = TenantKey::new(tenant.clone(), ObjectKind::PreparedActivity, id.clone())
                    .unwrap_or_else(|error| panic!("key: {error:?}"));
                (id, store.get(&key).map(|value| value.bytes().to_vec()))
            })
            .collect()
    }

    fn approval_entries(store: &Store, tenant: &TenantId) -> Vec<(TenantKey, Option<Vec<u8>>)> {
        [
            ObjectKind::PreparedActivity,
            ObjectKind::Idempotency,
            ObjectKind::Outbox,
        ]
        .into_iter()
        .flat_map(|kind| {
            store
                .list_object_ids(tenant, kind)
                .into_iter()
                .map(move |id| {
                    TenantKey::new(tenant.clone(), kind, id)
                        .unwrap_or_else(|error| panic!("key: {error:?}"))
                })
        })
        .map(|key| {
            let bytes = store.get(&key).map(|value| value.bytes().to_vec());
            (key, bytes)
        })
        .collect()
    }

    #[test]
    fn defective_approval_releases_every_held_limit_once_and_leaves_only_the_decision() {
        let shared = shared("released");
        hold(&shared, 81);
        let tenant = tenant();
        let service = ApprovalService::new(&shared.registry, &shared.limiter, &shared.expiry);
        let submissions = ApprovalSubmissionQueue::default();
        let approve = decision_key("approve-81");
        assert_eq!(shared.limiter.held_reservations(), Ok(2));
        for _ in 0..2 {
            let defective = service
                .approve(
                    request(&tenant, 81, &approve, 12),
                    &prepared(82),
                    &submissions,
                )
                .unwrap_or_else(|error| panic!("approve: {error:?}"));
            assert_eq!(defective.outcome, ApprovalOutcome::Defective);
            assert_eq!(defective.submission_ref, None);
            assert_eq!(shared.limiter.held_reservations(), Ok(0));
            for limit in &shared.limits {
                assert_eq!(shared.limiter.consumed(*limit), Ok(0));
            }
        }
        assert!(submissions.is_empty());
        assert!(hold_records(
            &shared.store.lock().unwrap_or_else(|_| panic!("store lock")),
            &tenant
        )
        .is_empty());
        assert!(matches!(
            shared.expiry.repeated(&tenant, [81; 32], &approve),
            Ok(Some(decision)) if decision.outcome == ApprovalOutcome::Defective
        ));
        assert_eq!(
            service
                .get(&tenant, [81; 32], 13)
                .map(|record| record.state),
            Ok(ApprovalState::Defective)
        );
    }

    #[test]
    fn defective_approval_with_a_failed_persist_leaves_the_hold_claimable_and_counted() {
        let shared = shared("failed-persist");
        hold(&shared, 91);
        let tenant = tenant();
        let service = ApprovalService::new(&shared.registry, &shared.limiter, &shared.expiry);
        let submissions = ApprovalSubmissionQueue::default();
        service
            .get(&tenant, [91; 32], 11)
            .unwrap_or_else(|error| panic!("get: {error:?}"));
        let approve = decision_key("approve-91");
        std::fs::remove_dir_all(&shared.root).unwrap_or_else(|error| panic!("remove: {error}"));
        assert!(matches!(
            service.approve(
                request(&tenant, 91, &approve, 12),
                &prepared(92),
                &submissions
            ),
            Err(ApprovalOperationError::Registry(
                crate::policy::approval::ApprovalError::Unavailable
            ))
        ));
        std::fs::create_dir_all(&shared.root).unwrap_or_else(|error| panic!("create: {error}"));
        assert_eq!(
            shared.expiry.repeated(&tenant, [91; 32], &approve),
            Ok(None)
        );
        assert_eq!(shared.registry.audit_entry([91; 32]), Ok(None));
        assert_eq!(shared.limiter.held_reservations(), Ok(2));
        assert_eq!(
            hold_records(
                &shared.store.lock().unwrap_or_else(|_| panic!("store lock")),
                &tenant
            )
            .len(),
            1
        );
        assert!(shared.registry.claim_scoped(&tenant, [91; 32], 12).is_ok());
        shared
            .registry
            .abort_claim([91; 32])
            .unwrap_or_else(|error| panic!("abort: {error:?}"));
        let defective = service
            .approve(
                request(&tenant, 91, &approve, 12),
                &prepared(92),
                &submissions,
            )
            .unwrap_or_else(|error| panic!("approve: {error:?}"));
        assert_eq!(defective.outcome, ApprovalOutcome::Defective);
        assert_eq!(shared.limiter.held_reservations(), Ok(0));
        assert!(submissions.is_empty());
        assert!(matches!(
            shared.expiry.repeated(&tenant, [91; 32], &approve),
            Ok(Some(decision)) if decision.outcome == ApprovalOutcome::Defective
        ));
    }

    #[test]
    fn defective_approval_under_an_unknown_limit_leaves_every_record_identical_and_retry_is_fresh()
    {
        let shared = shared("unknown");
        hold(&shared, 101);
        let tenant = tenant();
        let restarted = BudgetLimiter::new(Vec::new()).unwrap_or_else(|error| panic!("{error:?}"));
        let submissions = ApprovalSubmissionQueue::default();
        let entries = || {
            approval_entries(
                &shared.store.lock().unwrap_or_else(|_| panic!("store lock")),
                &tenant,
            )
        };
        let before = entries();
        assert_eq!(before.len(), 1);
        let approve = decision_key("approve-101");
        for _ in 0..2 {
            let refused = ApprovalService::new(&shared.registry, &restarted, &shared.expiry)
                .approve(
                    request(&tenant, 101, &approve, 12),
                    &prepared(102),
                    &submissions,
                );
            assert!(matches!(
                refused,
                Err(ApprovalOperationError::Reservation(LimitRefusal::UnknownLimit(id)))
                    if shared.limits.contains(&id)
            ));
            assert_eq!(entries(), before);
            assert_eq!(
                shared.expiry.repeated(&tenant, [101; 32], &approve),
                Ok(None)
            );
            assert_eq!(shared.registry.audit_entry([101; 32]), Ok(None));
        }
        assert_eq!(shared.limiter.held_reservations(), Ok(2));
        assert_eq!(restarted.held_reservations(), Ok(0));
        let defective = ApprovalService::new(&shared.registry, &shared.limiter, &shared.expiry)
            .approve(
                request(&tenant, 101, &approve, 12),
                &prepared(102),
                &submissions,
            )
            .unwrap_or_else(|error| panic!("approve: {error:?}"));
        assert_eq!(defective.outcome, ApprovalOutcome::Defective);
        assert_eq!(shared.limiter.held_reservations(), Ok(0));
        assert!(submissions.is_empty());
        assert!(hold_records(
            &shared.store.lock().unwrap_or_else(|_| panic!("store lock")),
            &tenant
        )
        .is_empty());
    }
}

#[cfg(test)]
mod reject_no_store_tests {
    use std::path::PathBuf;
    use std::sync::{Arc, Mutex};

    use layerx_agent_api::identity::{ActivityType, AgentDid, Asset, AuthorityRef, ExplicitSet};
    use layerx_agent_api::prepare::{
        CanonicalBytes, Disclosure, IdempotencyRef, PreparationRef, Prepared, SigningPreimage,
    };
    use layerx_agent_api::{Amount, TimestampSeconds};
    use layerx_types::ids::Did;
    use sha2::{Digest as _, Sha256};

    use super::{
        ApprovalExpiry, ApprovalOperationError, ApprovalOutcome, ApprovalService, DecisionKey,
        DecisionRequest,
    };
    use crate::budget::{
        create_daemon_limit, daemon_limit_id, reserve, BudgetLimiter, CoreTimestampMs,
        DaemonLimitRecord, LimitId, LimitRefusal, ReservationRequest,
    };
    use crate::capability::CapabilityId;
    use crate::policy::approval::{hold_reserved, ApprovalContext, ApprovalRegistry, ApproverId};
    use crate::session::SessionId;
    use crate::store::{Store, TenantId};

    struct Shared {
        root: PathBuf,
        store: Arc<Mutex<Store>>,
        registry: ApprovalRegistry,
        limiter: BudgetLimiter,
        expiry: ApprovalExpiry,
        limits: Vec<LimitId>,
    }

    impl Drop for Shared {
        fn drop(&mut self) {
            let _ = std::fs::remove_dir_all(&self.root);
        }
    }

    fn tenant() -> TenantId {
        TenantId::new("tenant-lock-order").unwrap_or_else(|error| panic!("{error}"))
    }

    fn prepared(id: u8) -> Prepared {
        let bytes = format!("lock-order-preparation-{id}").into_bytes();
        let digest: [u8; 32] = Sha256::digest(&bytes).into();
        Prepared {
            preparation_ref: PreparationRef::new(format!("prepared-{id}"))
                .unwrap_or_else(|error| panic!("preparation: {error:?}")),
            unsigned_canonical_bytes: CanonicalBytes::new(bytes)
                .unwrap_or_else(|error| panic!("bytes: {error:?}")),
            signing_preimage: SigningPreimage::new(vec![id; 32])
                .unwrap_or_else(|error| panic!("preimage: {error:?}")),
            disclosure: Disclosure {
                canonical_digest: digest,
                activity_type: ActivityType(7),
                actor: AgentDid::new("did:layerx:lock-order")
                    .unwrap_or_else(|error| panic!("actor: {error:?}")),
                authority: AuthorityRef::new("session-key")
                    .unwrap_or_else(|error| panic!("authority: {error:?}")),
                counterparties: ExplicitSet::deny_all(),
                amounts: ExplicitSet::deny_all(),
                asset: Asset::new("LXP").unwrap_or_else(|error| panic!("asset: {error:?}")),
                fee_limit: Amount(2),
                expiry: TimestampSeconds(40),
                idempotency_key: IdempotencyRef::new(
                    [id; 32]
                        .iter()
                        .map(|byte| format!("{byte:02x}"))
                        .collect::<String>(),
                )
                .unwrap_or_else(|error| panic!("activity key: {error:?}")),
            },
            expiry: TimestampSeconds(40),
        }
    }

    fn decision_key(label: &str) -> DecisionKey {
        DecisionKey::new(label).unwrap_or_else(|error| panic!("decision key: {error:?}"))
    }

    fn request<'a>(
        tenant: &'a TenantId,
        id: u8,
        idempotency_key: &'a DecisionKey,
        current_sequence: u64,
    ) -> DecisionRequest<'a> {
        DecisionRequest {
            tenant,
            approval_id: [id; 32],
            idempotency_key,
            approver: ApproverId::new("human:lock-order")
                .unwrap_or_else(|error| panic!("approver: {error:?}")),
            current_sequence,
        }
    }

    fn shared(label: &str) -> Shared {
        let root = std::env::temp_dir().join(format!(
            "layerx-approval-reject-no-store-{label}-{}",
            std::process::id()
        ));
        let _ = std::fs::remove_dir_all(&root);
        let store = Arc::new(Mutex::new(
            Store::open(&root).unwrap_or_else(|error| panic!("store: {error}")),
        ));
        let limiter = BudgetLimiter::new(Vec::new()).unwrap_or_else(|error| panic!("{error:?}"));
        let limits = {
            let mut guard = store.lock().unwrap_or_else(|_| panic!("store lock"));
            [41_u8, 42]
                .iter()
                .map(|id| {
                    create_daemon_limit(
                        &mut guard,
                        &limiter,
                        DaemonLimitRecord {
                            tenant: tenant(),
                            budget_id: [*id; 32],
                            limit_id: daemon_limit_id([*id; 32]),
                            agent_digest: [7; 32],
                            asset: [8; 32],
                            ceiling: 1_000,
                            consumed: 0,
                            expiry_ms: 1_000_000,
                            revoked: false,
                            mutation_key: [*id; 32],
                            body_digest: [*id; 32],
                            revoke_key: [0; 32],
                        },
                        CoreTimestampMs(1),
                    )
                    .unwrap_or_else(|error| panic!("daemon limit: {error:?}"))
                    .limit_id
                })
                .collect::<Vec<_>>()
        };
        Shared {
            root,
            registry: ApprovalRegistry::default(),
            expiry: ApprovalExpiry::from_shared_store(Arc::clone(&store)),
            store,
            limiter,
            limits,
        }
    }

    fn hold(shared: &Shared, id: u8) {
        let reservation = reserve(
            &shared.limiter,
            &ReservationRequest {
                id: [id; 32],
                amount: 10,
                expiry_sequence: 40,
                current_sequence: 10,
                applicable_limits: shared.limits.clone(),
            },
        )
        .unwrap_or_else(|error| panic!("reserve: {error:?}"));
        hold_reserved(
            &shared.registry,
            ApprovalContext {
                tenant: tenant(),
                agent: Did::new(b"did:layerx:lock-order")
                    .unwrap_or_else(|error| panic!("agent: {error:?}")),
                session: SessionId([2; 32]),
                capability: CapabilityId([3; 32]),
                policy_version: "policy-v3".to_owned(),
                request_id: [id; 32],
            },
            prepared(id),
            10,
            40,
            &reservation,
        )
        .unwrap_or_else(|error| panic!("hold: {error:?}"));
    }

    #[test]
    fn reject_without_a_registry_store_and_a_failed_decision_persist_publishes_nothing() {
        let shared = shared("failed-decision");
        hold(&shared, 111);
        let tenant = tenant();
        let service = ApprovalService::new(&shared.registry, &shared.limiter, &shared.expiry);
        service
            .get(&tenant, [111; 32], 11)
            .unwrap_or_else(|error| panic!("get: {error:?}"));
        let reject = decision_key("reject-111");
        std::fs::remove_dir_all(&shared.root).unwrap_or_else(|error| panic!("remove: {error}"));
        assert!(matches!(
            service.reject(request(&tenant, 111, &reject, 12)),
            Err(ApprovalOperationError::Registry(
                crate::policy::approval::ApprovalError::Unavailable
            ))
        ));
        std::fs::create_dir_all(&shared.root).unwrap_or_else(|error| panic!("create: {error}"));
        assert_eq!(shared.limiter.held_reservations(), Ok(2));
        assert_eq!(
            shared.expiry.repeated(&tenant, [111; 32], &reject),
            Ok(None)
        );
        assert_eq!(shared.registry.audit_entry([111; 32]), Ok(None));
        assert!(shared.registry.claim_scoped(&tenant, [111; 32], 12).is_ok());
        shared
            .registry
            .abort_claim([111; 32])
            .unwrap_or_else(|error| panic!("abort: {error:?}"));
        let rejected = service
            .reject(request(&tenant, 111, &reject, 12))
            .unwrap_or_else(|error| panic!("reject: {error:?}"));
        assert_eq!(rejected.outcome, ApprovalOutcome::Rejected);
        assert_eq!(shared.limiter.held_reservations(), Ok(0));
        assert!(matches!(
            shared.expiry.repeated(&tenant, [111; 32], &reject),
            Ok(Some(decision)) if decision.outcome == ApprovalOutcome::Rejected
        ));
    }
}

pub use events::{
    ApprovalEmission, ApprovalEventError, ApprovalEventKind, ApprovalEvents, ApprovalLifecycle,
};
pub use expiry::{ApprovalExpiry, ApprovalExpiryError, DecisionKey};
pub use crate::policy::approval::ApprovalPresentation;

pub const APPROVAL_ENFORCEMENT_NOTICE: &str =
    "daemon-enforced restriction; confers no protocol authority; bypassing layerx-agentd bypasses this restriction";

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum ApprovalEnforcement {
    DaemonOnly,
}

/// Human-readable reason attached to the policy hold.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct HoldReason {
    pub code: &'static str,
    pub message: &'static str,
}

/// Tenant-scoped record returned by list and get.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ApprovalRecord {
    pub approval_id: [u8; 32],
    pub tenant: TenantId,
    pub held_activity: Disclosure,
    pub canonical_bytes_digest: [u8; 32],
    pub hold_reason: HoldReason,
    pub created_at_sequence: u64,
    pub expires_at_sequence: u64,
    pub presentation: Option<ApprovalPresentation>,
    pub state: ApprovalState,
    pub submission_ref: Option<[u8; 32]>,
    pub enforcement: ApprovalEnforcement,
    pub authority_notice: &'static str,
}

/// Bounded deterministic approval page.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ApprovalPage {
    pub approvals: Vec<ApprovalRecord>,
    pub next_cursor: Option<[u8; 32]>,
}

/// Total result vocabulary for approval decisions.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum ApprovalOutcome {
    Granted,
    Rejected,
    Expired,
    Defective,
    AlreadyDecided,
    Conflict,
}

/// Result returned to the requesting agent.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ApprovalDecision {
    pub outcome: ApprovalOutcome,
    pub submission_ref: Option<[u8; 32]>,
    pub winning_outcome: Option<ApprovalOutcome>,
    pub enforcement: ApprovalEnforcement,
    pub authority_notice: &'static str,
}

/// One preparation held under its owning tenant and approval identity.
#[derive(Clone)]
struct QueuedSubmission {
    tenant: TenantId,
    approval_id: [u8; 32],
    prepared: Prepared,
}

/// A real pre-signing submission queue containing the exact approved preparation.
#[derive(Default)]
pub struct ApprovalSubmissionQueue {
    queued: Mutex<BTreeMap<[u8; 32], QueuedSubmission>>,
}

impl ApprovalSubmissionQueue {
    pub(crate) fn matches_released_decision(
        &self,
        tenant: &TenantId,
        approval_id: [u8; 32],
        submission_ref: [u8; 32],
        disclosure_digest: [u8; 32],
    ) -> Result<bool, ApprovalOutcome> {
        let queued = self.queued.lock().map_err(|_| ApprovalOutcome::Conflict)?;
        Ok(queued.get(&submission_ref).is_some_and(|record| {
            &record.tenant == tenant
                && record.approval_id == approval_id
                && record.prepared.disclosure.canonical_digest == disclosure_digest
        }))
    }

    pub(crate) fn settle_verified(
        &self,
        tenant: &TenantId,
        idempotency_key: [u8; 32],
        result_code: i32,
        current_sequence: u64,
        store: &mut Store,
        limiter: &BudgetLimiter,
    ) -> Result<bool, ApprovalOperationError> {
        let identity = idempotency_key
            .iter()
            .flat_map(|byte| {
                let digits = b"0123456789abcdef";
                [
                    char::from(digits[usize::from(byte >> 4)]),
                    char::from(digits[usize::from(byte & 15)]),
                ]
            })
            .collect::<String>();
        let mut queued = self
            .queued
            .lock()
            .map_err(|_| ApprovalOperationError::Registry(RegistryError::Unavailable))?;
        let selected = queued.iter().find_map(|(reference, record)| {
            (&record.tenant == tenant
                && record.prepared.disclosure.idempotency_key.as_str() == identity)
                .then_some((*reference, record.approval_id))
        });
        let Some((reference, approval_id)) = selected else {
            return Ok(false);
        };
        let key = crate::policy::approval::released_storage_key(tenant, approval_id)
            .map_err(ApprovalOperationError::Registry)?;
        let reservations = crate::policy::approval::released_reservations(
            tenant,
            store
                .get(&key)
                .ok_or(ApprovalOperationError::Registry(
                    RegistryError::CorruptRecord,
                ))?
                .bytes(),
        )
        .map_err(ApprovalOperationError::Registry)?;
        for reservation in &reservations {
            if reservation.reservation_id != approval_id {
                return Err(ApprovalOperationError::Registry(
                    RegistryError::CorruptRecord,
                ));
            }
            limiter
                .consumed(reservation.limit_id)
                .map_err(ApprovalOperationError::Reservation)?;
        }
        let executed = result_code == 0;
        let updates = if executed {
            budget::consumption_updates(store, tenant, &reservations)
                .map_err(|_| ApprovalOperationError::Registry(RegistryError::CorruptRecord))?
        } else {
            Vec::new()
        };
        let staged = budget::stage_release(
            limiter,
            approval_id,
            if executed {
                ReleaseKind::Executed
            } else {
                ReleaseKind::Failed
            },
            current_sequence,
        )
        .map_err(ApprovalOperationError::Reservation)?;
        store
            .update_local_batch_removing(updates, vec![key])
            .map_err(|_| ApprovalOperationError::Registry(RegistryError::Unavailable))?;
        let _ = staged.publish();
        queued.remove(&reference);
        Ok(true)
    }

    pub(crate) fn authorize_submit(
        &self,
        tenant: &TenantId,
        preparation_ref: &str,
        canonical_bytes: &[u8],
        release_ref: Option<[u8; 32]>,
    ) -> Result<(), ApprovalOutcome> {
        let queued = self.queued.lock().map_err(|_| ApprovalOutcome::Conflict)?;
        match release_ref {
            Some(reference) => match queued.get(&reference) {
                Some(record)
                    if &record.tenant == tenant
                        && record.prepared.preparation_ref.as_str() == preparation_ref
                        && record.prepared.unsigned_canonical_bytes.as_bytes()
                            == canonical_bytes =>
                {
                    Ok(())
                }
                _ => Err(ApprovalOutcome::Conflict),
            },
            None if queued.values().any(|record| {
                &record.tenant == tenant
                    && record.prepared.preparation_ref.as_str() == preparation_ref
                    && record.prepared.unsigned_canonical_bytes.as_bytes() == canonical_bytes
            }) =>
            {
                Err(ApprovalOutcome::Conflict)
            }
            None => Ok(()),
        }
    }

    pub(crate) fn restore(&self, records: Vec<ReleasedApproval>) -> Result<(), ApprovalOutcome> {
        let mut queued = self.queued.lock().map_err(|_| ApprovalOutcome::Conflict)?;
        let mut restored = queued.clone();
        for record in records {
            if Self::reference(&record.tenant, record.approval_id, &record.prepared)
                != record.submission_ref
                || restored
                    .insert(
                        record.submission_ref,
                        QueuedSubmission {
                            tenant: record.tenant,
                            approval_id: record.approval_id,
                            prepared: record.prepared,
                        },
                    )
                    .is_some()
            {
                return Err(ApprovalOutcome::Conflict);
            }
        }
        *queued = restored;
        Ok(())
    }

    fn reference(tenant: &TenantId, approval_id: [u8; 32], prepared: &Prepared) -> [u8; 32] {
        let mut hasher = Sha256::new();
        hasher.update(b"layerx-approved-preparation-v1");
        hasher.update(tenant.as_str().as_bytes());
        hasher.update(approval_id);
        hasher.update(prepared.unsigned_canonical_bytes.as_bytes());
        hasher.finalize().into()
    }

    fn release(
        &self,
        tenant: TenantId,
        approval_id: [u8; 32],
        prepared: Prepared,
    ) -> Result<[u8; 32], ApprovalOutcome> {
        let submission_ref = Self::reference(&tenant, approval_id, &prepared);
        let mut queued = self.queued.lock().map_err(|_| ApprovalOutcome::Conflict)?;
        match queued.get(&submission_ref) {
            Some(stored)
                if stored.tenant == tenant
                    && stored.approval_id == approval_id
                    && stored.prepared == prepared =>
            {
                Ok(submission_ref)
            }
            Some(_) => Err(ApprovalOutcome::Conflict),
            None => {
                queued.insert(
                    submission_ref,
                    QueuedSubmission {
                        tenant,
                        approval_id,
                        prepared,
                    },
                );
                Ok(submission_ref)
            }
        }
    }

    /// Returns the exact preparation released by approval.
    #[must_use]
    pub fn prepared(&self, submission_ref: [u8; 32]) -> Option<Prepared> {
        self.queued.lock().ok().and_then(|queued| {
            queued
                .get(&submission_ref)
                .map(|stored| stored.prepared.clone())
        })
    }

    /// Number of preparations waiting for the ordinary sign-and-submit path.
    #[must_use]
    pub fn len(&self) -> usize {
        self.queued.lock().map_or(0, |queued| queued.len())
    }

    /// Whether no approved preparation is queued.
    #[must_use]
    pub fn is_empty(&self) -> bool {
        self.queued.lock().map_or(true, |queued| queued.is_empty())
    }
}

/// Authenticated identity of exactly one approve or reject decision.
pub struct DecisionRequest<'a> {
    pub tenant: &'a TenantId,
    pub approval_id: [u8; 32],
    pub idempotency_key: &'a DecisionKey,
    pub approver: ApproverId,
    pub current_sequence: u64,
}

/// Tenant-authenticated list/get/approve/reject service.
pub struct ApprovalService<'a> {
    registry: &'a ApprovalRegistry,
    limiter: &'a BudgetLimiter,
    expiry: &'a ApprovalExpiry,
    #[cfg(test)]
    after_prepare: Option<&'a (dyn Fn() + Sync)>,
    #[cfg(test)]
    before_decision: Option<&'a (dyn Fn() + Sync)>,
}

impl<'a> ApprovalService<'a> {
    #[must_use]
    pub const fn new(
        registry: &'a ApprovalRegistry,
        limiter: &'a BudgetLimiter,
        expiry: &'a ApprovalExpiry,
    ) -> Self {
        Self {
            registry,
            limiter,
            expiry,
            #[cfg(test)]
            after_prepare: None,
            #[cfg(test)]
            before_decision: None,
        }
    }

    /// Lists only holds belonging to the authenticated tenant.
    ///
    /// # Errors
    ///
    /// Refuses an invalid page bound or an unavailable approval registry.
    pub fn list(
        &self,
        tenant: &TenantId,
        cursor: Option<[u8; 32]>,
        page_limit: usize,
        current_sequence: u64,
    ) -> Result<ApprovalPage, ApprovalOperationError> {
        let _decision = self
            .expiry
            .lock_decisions()
            .map_err(ApprovalOperationError::Durability)?;
        if page_limit == 0 || page_limit > 100 {
            return Err(ApprovalOperationError::InvalidPageLimit);
        }
        let mut snapshots = self
            .registry
            .list_scoped(tenant, current_sequence)
            .map_err(ApprovalOperationError::Registry)?;
        snapshots.sort_by_key(|snapshot| snapshot.context.request_id);
        let mut records = Vec::new();
        for mut snapshot in snapshots {
            if cursor.is_some_and(|value| snapshot.context.request_id <= value) {
                continue;
            }
            snapshot.state = self
                .expiry
                .observe(&snapshot, current_sequence, self.limiter)
                .map_err(ApprovalOperationError::Durability)?;
            records.push(record(snapshot));
        }
        let next_cursor = (records.len() > page_limit).then(|| records[page_limit - 1].approval_id);
        records.truncate(page_limit);
        Ok(ApprovalPage {
            approvals: records,
            next_cursor,
        })
    }

    /// Gets one hold without revealing whether another tenant owns its identifier.
    ///
    /// # Errors
    ///
    /// Returns `NotFound` for absent and cross-tenant identifiers, or the registry failure.
    pub fn get(
        &self,
        tenant: &TenantId,
        approval_id: [u8; 32],
        current_sequence: u64,
    ) -> Result<ApprovalRecord, ApprovalOperationError> {
        let _decision = self
            .expiry
            .lock_decisions()
            .map_err(ApprovalOperationError::Durability)?;
        let mut snapshot = self
            .registry
            .get_scoped(tenant, approval_id, current_sequence)
            .map_err(ApprovalOperationError::Registry)?;
        snapshot.state = self
            .expiry
            .observe(&snapshot, current_sequence, self.limiter)
            .map_err(ApprovalOperationError::Durability)?;
        Ok(record(snapshot))
    }

    /// Releases exactly the held prepared activity into the ordinary submission queue.
    ///
    /// # Errors
    ///
    /// Returns registry failures that cannot be represented by a typed decision outcome.
    pub fn approve(
        &self,
        request: DecisionRequest<'_>,
        current_prepared: &Prepared,
        submissions: &ApprovalSubmissionQueue,
    ) -> Result<ApprovalDecision, ApprovalOperationError> {
        let _decision = self
            .expiry
            .lock_decisions()
            .map_err(ApprovalOperationError::Durability)?;
        let DecisionRequest {
            tenant,
            approval_id,
            idempotency_key,
            approver,
            current_sequence,
        } = request;
        if let Some(decision) = self
            .expiry
            .repeated(tenant, approval_id, idempotency_key)
            .map_err(ApprovalOperationError::Durability)?
        {
            return Ok(decision);
        }
        let snapshot = self
            .registry
            .get_scoped(tenant, approval_id, current_sequence)
            .map_err(ApprovalOperationError::Registry)?;
        let intended = approval_intent(current_prepared, &snapshot.prepared);
        if intended == ApprovalOutcome::Defective {
            self.registry
                .check_reserved_limits(approval_id, self.limiter)?;
        }
        let submission_ref = (intended == ApprovalOutcome::Granted)
            .then(|| ApprovalSubmissionQueue::reference(tenant, approval_id, &snapshot.prepared));
        let decision_record = match self
            .expiry
            .decide(
                &snapshot,
                current_sequence,
                idempotency_key,
                intended,
                submission_ref,
                self.limiter,
            )
            .map_err(ApprovalOperationError::Durability)?
        {
            expiry::DecisionResolution::Winner => None,
            expiry::DecisionResolution::WinnerPrepared(prepared) => Some(prepared),
            expiry::DecisionResolution::Repeat(decision) => return Ok(decision),
            expiry::DecisionResolution::Conflict(winner) => {
                return Ok(conflict(winner));
            }
            expiry::DecisionResolution::Expired => {
                return Ok(decision(ApprovalOutcome::Expired, None));
            }
        };
        #[cfg(test)]
        if let Some(after_prepare) = self.after_prepare {
            after_prepare();
        }
        let claimed = match self
            .registry
            .claim_scoped(tenant, approval_id, current_sequence)
        {
            Ok(claimed) => claimed,
            Err(error) => return outcome_or_error(error),
        };
        if intended == ApprovalOutcome::Defective {
            let Some(decision_record) = decision_record else {
                self.registry
                    .abort_claim(approval_id)
                    .map_err(ApprovalOperationError::Registry)?;
                return Err(ApprovalOperationError::Durability(
                    ApprovalExpiryError::Corrupt,
                ));
            };
            self.registry.finish_claim_releasing(
                approval_id,
                ApprovalState::Defective,
                approver,
                "held_preparation_changed_after_approval_hold",
                self.limiter,
                current_sequence,
                decision_record,
                self.expiry.decision_store(),
            )?;
            return Ok(decision(ApprovalOutcome::Defective, None));
        }
        let queued_submission_ref =
            match submissions.release(tenant.clone(), approval_id, claimed.prepared.clone()) {
                Ok(reference) => reference,
                Err(outcome) => {
                    self.registry
                        .abort_claim(approval_id)
                        .map_err(ApprovalOperationError::Registry)?;
                    return Ok(decision(outcome, None));
                }
            };
        self.persist_grant(
            approval_id,
            approver,
            queued_submission_ref,
            decision_record,
        )?;
        debug_assert_eq!(submission_ref, Some(queued_submission_ref));
        Ok(decision(
            ApprovalOutcome::Granted,
            Some(queued_submission_ref),
        ))
    }

    fn persist_grant(
        &self,
        approval_id: [u8; 32],
        approver: ApproverId,
        queued_submission_ref: [u8; 32],
        decision_record: Option<PreparedDecision>,
    ) -> Result<(), ApprovalOperationError> {
        let fallback_decision_record = (!self.registry.has_durable_store())
            .then(|| decision_record.clone())
            .flatten();
        self.registry
            .complete_claim(
                approval_id,
                ApprovalState::Approved,
                approver,
                "approver_released_exact_preparation",
                Some(queued_submission_ref),
                decision_record,
            )
            .map_err(ApprovalOperationError::Registry)?;
        if let Some(prepared) = fallback_decision_record {
            self.expiry
                .persist_prepared_decision(prepared)
                .map_err(ApprovalOperationError::Durability)?;
        }
        Ok(())
    }

    /// Finalizes rejection and releases the hold's reservation deterministically.
    ///
    /// # Errors
    ///
    /// Returns registry or reservation failures that prevent a final rejection.
    pub fn reject(
        &self,
        request: DecisionRequest<'_>,
    ) -> Result<ApprovalDecision, ApprovalOperationError> {
        #[cfg(test)]
        if let Some(before_decision) = self.before_decision {
            before_decision();
        }
        let _decision = self
            .expiry
            .lock_decisions()
            .map_err(ApprovalOperationError::Durability)?;
        let DecisionRequest {
            tenant,
            approval_id,
            idempotency_key,
            approver,
            current_sequence,
        } = request;
        let snapshot = self
            .registry
            .get_scoped(tenant, approval_id, current_sequence)
            .map_err(ApprovalOperationError::Registry)?;
        self.registry
            .check_reserved_limits(approval_id, self.limiter)?;
        let decision_record = match self
            .expiry
            .decide(
                &snapshot,
                current_sequence,
                idempotency_key,
                ApprovalOutcome::Rejected,
                None,
                self.limiter,
            )
            .map_err(ApprovalOperationError::Durability)?
        {
            expiry::DecisionResolution::WinnerPrepared(prepared) => prepared,
            expiry::DecisionResolution::Winner => {
                return Err(ApprovalOperationError::Durability(
                    ApprovalExpiryError::Corrupt,
                ))
            }
            expiry::DecisionResolution::Repeat(decision) => return Ok(decision),
            expiry::DecisionResolution::Conflict(winner) => {
                return Ok(conflict(winner));
            }
            expiry::DecisionResolution::Expired => {
                return Ok(decision(ApprovalOutcome::Expired, None));
            }
        };
        match self
            .registry
            .claim_scoped(tenant, approval_id, current_sequence)
        {
            Ok(_) => {}
            Err(error) => return outcome_or_error(error),
        }
        self.registry.reject_claim_releasing(
            approval_id,
            approver,
            self.limiter,
            current_sequence,
            decision_record,
            self.expiry.decision_store(),
        )?;
        Ok(decision(ApprovalOutcome::Rejected, None))
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub enum ApprovalOperationError {
    InvalidPageLimit,
    Registry(RegistryError),
    Reservation(crate::budget::LimitRefusal),
    Durability(ApprovalExpiryError),
}

fn record(snapshot: ApprovalSnapshot) -> ApprovalRecord {
    ApprovalRecord {
        approval_id: snapshot.context.request_id,
        tenant: snapshot.context.tenant,
        held_activity: snapshot.prepared.disclosure.clone(),
        canonical_bytes_digest: canonical_digest(
            snapshot.prepared.unsigned_canonical_bytes.as_bytes(),
        ),
        hold_reason: HoldReason {
            code: "policy_approval_required",
            message: "Policy requires human approval before submission",
        },
        created_at_sequence: snapshot.created_at_sequence,
        expires_at_sequence: snapshot.expires_at_sequence,
        presentation: snapshot.presentation,
        state: snapshot.state,
        submission_ref: snapshot.submission_ref,
        enforcement: ApprovalEnforcement::DaemonOnly,
        authority_notice: APPROVAL_ENFORCEMENT_NOTICE,
    }
}

fn outcome_or_error(error: RegistryError) -> Result<ApprovalDecision, ApprovalOperationError> {
    match error {
        RegistryError::AlreadyDecided(ApprovalState::Expired) => {
            Ok(decision(ApprovalOutcome::Expired, None))
        }
        RegistryError::AlreadyDecided(_) => Ok(decision(ApprovalOutcome::AlreadyDecided, None)),
        RegistryError::Defective => Ok(decision(ApprovalOutcome::Defective, None)),
        RegistryError::DecisionConflict | RegistryError::DisclosureChanged => {
            Ok(decision(ApprovalOutcome::Conflict, None))
        }
        other => Err(ApprovalOperationError::Registry(other)),
    }
}

const fn decision(outcome: ApprovalOutcome, submission_ref: Option<[u8; 32]>) -> ApprovalDecision {
    ApprovalDecision {
        outcome,
        submission_ref,
        winning_outcome: None,
        enforcement: ApprovalEnforcement::DaemonOnly,
        authority_notice: APPROVAL_ENFORCEMENT_NOTICE,
    }
}

const fn conflict(winner: ApprovalOutcome) -> ApprovalDecision {
    ApprovalDecision {
        outcome: ApprovalOutcome::Conflict,
        submission_ref: None,
        winning_outcome: Some(winner),
        enforcement: ApprovalEnforcement::DaemonOnly,
        authority_notice: APPROVAL_ENFORCEMENT_NOTICE,
    }
}

fn canonical_digest(bytes: &[u8]) -> [u8; 32] {
    let mut hasher = Sha256::new();
    hasher.update(bytes);
    hasher.finalize().into()
}

fn approval_intent(current_prepared: &Prepared, held: &Prepared) -> ApprovalOutcome {
    let digest = canonical_digest(held.unsigned_canonical_bytes.as_bytes());
    if current_prepared != held || digest != held.disclosure.canonical_digest {
        ApprovalOutcome::Defective
    } else {
        ApprovalOutcome::Granted
    }
}

impl ApprovalSubmissionQueue {
    pub(crate) fn publish_program_release<T>(
        &self,
        tenant: TenantId,
        approval_id: [u8; 32],
        prepared: Prepared,
        persist: impl FnOnce([u8; 32]) -> Result<T, ApprovalOperationError>,
    ) -> Result<T, ApprovalOperationError> {
        let reference = Self::reference(&tenant, approval_id, &prepared);
        let mut queued = self.queued.lock().map_err(|_| ApprovalOperationError::Registry(RegistryError::Unavailable))?;
        if queued.contains_key(&reference) {
            return Err(ApprovalOperationError::Registry(RegistryError::DecisionConflict));
        }
        let result = persist(reference)?;
        queued.insert(reference, QueuedSubmission { tenant, approval_id, prepared });
        Ok(result)
    }
}

impl ApprovalService<'_> {
    pub(crate) fn decide_program(
        &self,
        request: DecisionRequest<'_>,
        context: &crate::agent_rpc_peer::RpcOwnerContext<'_>,
        current_prepared: &Prepared,
        approve: bool,
        submissions: &ApprovalSubmissionQueue,
    ) -> Result<ApprovalDecision, ApprovalOperationError> {
        self.registry.complete_program_decision(
            request, context, current_prepared, approve, self.expiry, self.limiter, submissions,
        )
    }
}
