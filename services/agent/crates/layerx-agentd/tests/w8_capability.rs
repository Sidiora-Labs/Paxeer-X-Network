use std::collections::BTreeSet;
use std::fs;
use std::path::PathBuf;
use std::sync::atomic::{AtomicU64, Ordering};

use layerx_agent_api::capability::{AmountCeiling, CapabilityDimensions, ExplicitSet, RateCeiling};
use layerx_agent_api::error::RequestId;
use layerx_agent_api::generated::{Amount, TimestampSeconds};
use layerx_agent_api::identity::{ActivityType, Asset, Counterparty, Purpose};
use layerx_agentd::capability::timed::{self, Insert, TimedCapability, TimedError, TimedState};
use layerx_agentd::capability::{Dimension, ProtocolScope};
use layerx_agentd::human::HumanOperationError;
use layerx_agentd::identity::ProtocolAuthority;
use layerx_agentd::store::{Store, TenantId};

const EXPIRY_SECONDS: u64 = 1_700_000_000;
const GRANT_NOT_AFTER_MS: u64 = 1_700_000_000_500;
const CREATED_AT_MS: u64 = 1_699_999_000_000;
const CREATED_AT_SEQUENCE: u64 = 88;
const ASSET_A: &str = "3333333333333333333333333333333333333333333333333333333333333333";
const ASSET_B: &str = "4444444444444444444444444444444444444444444444444444444444444444";
const ASSET_UPPER: &str = "ABABABABABABABABABABABABABABABABABABABABABABABABABABABABABABABAB";
const COUNTERPARTY: &str = "2222222222222222222222222222222222222222222222222222222222222222";

static NEXT_DIRECTORY: AtomicU64 = AtomicU64::new(1);

fn text<T, E: std::fmt::Debug>(result: Result<T, E>, label: &str) -> T {
    match result {
        Ok(value) => value,
        Err(error) => panic!("{label} must be valid: {error:?}"),
    }
}

fn test_directory(name: &str) -> PathBuf {
    let sequence = NEXT_DIRECTORY.fetch_add(1, Ordering::Relaxed);
    std::env::temp_dir().join(format!(
        "layerx-agentd-w8-capability-{name}-{}-{sequence}",
        std::process::id()
    ))
}

fn tenant() -> TenantId {
    text(TenantId::new("tenant-a"), "tenant")
}

fn request_id() -> RequestId {
    RequestId(41)
}

fn assets(values: &[&str]) -> ExplicitSet<Asset> {
    ExplicitSet::allow(values.iter().map(|v| text(Asset::new(*v), "asset")).collect())
}

fn ceiling(asset: &str, amount: u128) -> AmountCeiling {
    AmountCeiling {
        asset: text(Asset::new(asset), "ceiling asset"),
        amount: Amount(amount),
    }
}

fn window(window_seconds: u64, maximum_actions: u64) -> RateCeiling {
    RateCeiling {
        window_seconds: TimestampSeconds(window_seconds),
        maximum_actions,
    }
}

fn dimensions() -> CapabilityDimensions {
    CapabilityDimensions {
        activity_types: ExplicitSet::allow(vec![ActivityType(5), ActivityType(6)]),
        counterparties: ExplicitSet::allow(vec![text(Counterparty::new(COUNTERPARTY), "counterparty")]),
        assets: assets(&[ASSET_A, ASSET_B]),
        amount_ceilings: ExplicitSet::allow(vec![ceiling(ASSET_A, 500), ceiling(ASSET_B, 900)]),
        rate_ceilings: ExplicitSet::allow(vec![window(60, 3), window(3_600, 10)]),
        purpose_constraints: ExplicitSet::allow(vec![text(Purpose::new("service"), "purpose")]),
        expiry: TimestampSeconds(EXPIRY_SECONDS),
    }
}

fn build(
    id: [u8; 32],
    parent: Option<[u8; 32]>,
    value: &CapabilityDimensions,
    grant_not_after_ms: u64,
) -> Result<TimedCapability, TimedError> {
    TimedCapability::from_public(
        id,
        parent,
        tenant(),
        "agent-a",
        ProtocolAuthority::SessionKey([4; 32]),
        value,
        grant_not_after_ms,
        CREATED_AT_MS,
        CREATED_AT_SEQUENCE,
        request_id(),
    )
}

fn record(id: [u8; 32], parent: Option<[u8; 32]>) -> TimedCapability {
    text(build(id, parent, &dimensions(), GRANT_NOT_AFTER_MS), "timed capability")
}

fn inserted(store: &mut Store, value: TimedCapability) -> TimedCapability {
    match text(timed::insert(store, value), "insert") {
        Insert::Created(value) => value,
        Insert::Replayed(_) => panic!("a first insert must create, not replay"),
    }
}

struct Root(PathBuf);

impl Drop for Root {
    fn drop(&mut self) {
        let _ = fs::remove_dir_all(&self.0);
    }
}

#[test]
fn grant_not_after_is_carried_and_expiry_includes_equality() {
    let capability = record([1; 32], None);
    assert_eq!(capability.grant_not_after_ms, GRANT_NOT_AFTER_MS);
    assert_eq!(capability.expiry_seconds, EXPIRY_SECONDS);
    assert_eq!(capability.created_at_ms, CREATED_AT_MS);
    assert_eq!(capability.created_at_sequence, CREATED_AT_SEQUENCE);
    let bound = u64::try_from(capability.not_after_ms()).unwrap_or_else(|_| panic!("bound fits u64"));
    assert_eq!(bound, EXPIRY_SECONDS * 1_000);
    assert!(!capability.is_expired(bound - 1));
    assert!(capability.is_expired(bound));
    assert!(capability.is_expired(bound + 1));
    assert!(matches!(capability.state(bound - 1), TimedState::Active));
    assert!(matches!(capability.state(bound), TimedState::Expired));

    let earlier_grant = EXPIRY_SECONDS * 1_000 - 250;
    let clamped = text(build([2; 32], None, &dimensions(), earlier_grant), "clamped");
    assert_eq!(clamped.not_after_ms(), u128::from(earlier_grant));
    assert!(!clamped.is_expired(earlier_grant - 1));
    assert!(clamped.is_expired(earlier_grant));
}

fn grant_scope(not_before_ms: u64, not_after_ms: u64) -> ProtocolScope {
    ProtocolScope {
        activity_types: BTreeSet::from([5, 6, 7]),
        counterparties: BTreeSet::from([[0x22; 32]]),
        assets: BTreeSet::from([[0x33; 32], [0x44; 32]]),
        amount_ceiling: 1_000,
        expires_at_sequence: 200,
        not_before_ms,
        not_after_ms,
        enforceable_dimensions: BTreeSet::from([
            Dimension::ActivityType,
            Dimension::Counterparty,
            Dimension::Asset,
            Dimension::Amount,
            Dimension::Expiry,
        ]),
    }
}

#[test]
fn creation_carries_the_verified_grant_not_after_and_never_exceeds_it() {
    let scope = grant_scope(CREATED_AT_MS - 1_000, GRANT_NOT_AFTER_MS);
    let capability = text(build([11; 32], None, &dimensions(), scope.not_after_ms), "carried");
    assert_eq!(capability.grant_not_after_ms, scope.not_after_ms);
    assert!(timed::check_scope(&capability, &scope, CREATED_AT_MS).is_ok());
    assert!(matches!(
        timed::check_scope(&capability, &grant_scope(CREATED_AT_MS + 1, GRANT_NOT_AFTER_MS), CREATED_AT_MS),
        Err(TimedError::NotYetValid)
    ));
    let short = EXPIRY_SECONDS * 1_000 - 1;
    assert!(matches!(
        timed::check_scope(
            &text(build([11; 32], None, &dimensions(), short), "short grant"),
            &grant_scope(CREATED_AT_MS - 1_000, short),
            CREATED_AT_MS
        ),
        Err(TimedError::Wider(Dimension::Expiry))
    ));
    assert!(matches!(
        timed::check_scope(&capability, &grant_scope(CREATED_AT_MS - 1_000, GRANT_NOT_AFTER_MS + 1), CREATED_AT_MS),
        Err(TimedError::Wider(Dimension::Expiry))
    ));
    let mut over = dimensions();
    over.amount_ceilings = ExplicitSet::allow(vec![ceiling(ASSET_A, 1_001), ceiling(ASSET_B, 900)]);
    assert!(matches!(
        timed::check_scope(&text(build([11; 32], None, &over, GRANT_NOT_AFTER_MS), "over"), &scope, CREATED_AT_MS),
        Err(TimedError::Wider(Dimension::Amount))
    ));
}

#[test]
fn duplicate_set_entries_are_refused_per_dimension() {
    let mut value = dimensions();
    value.assets = assets(&[ASSET_A, ASSET_A, ASSET_B]);
    assert!(matches!(
        build([3; 32], None, &value, GRANT_NOT_AFTER_MS),
        Err(TimedError::Duplicate(Dimension::Asset))
    ));

    let mut value = dimensions();
    value.activity_types = ExplicitSet::allow(vec![ActivityType(5), ActivityType(5)]);
    assert!(matches!(
        build([3; 32], None, &value, GRANT_NOT_AFTER_MS),
        Err(TimedError::Duplicate(Dimension::ActivityType))
    ));

    let mut value = dimensions();
    value.amount_ceilings = ExplicitSet::allow(vec![ceiling(ASSET_A, 500), ceiling(ASSET_A, 100)]);
    assert!(matches!(
        build([3; 32], None, &value, GRANT_NOT_AFTER_MS),
        Err(TimedError::Duplicate(Dimension::Amount))
    ));

    let mut value = dimensions();
    value.rate_ceilings = ExplicitSet::allow(vec![window(60, 3), window(60, 1)]);
    assert!(matches!(
        build([3; 32], None, &value, GRANT_NOT_AFTER_MS),
        Err(TimedError::Duplicate(Dimension::Rate))
    ));
}

#[test]
fn zero_window_and_ceiling_outside_assets_are_refused() {
    let mut value = dimensions();
    value.rate_ceilings = ExplicitSet::allow(vec![window(0, 3)]);
    assert!(matches!(
        build([4; 32], None, &value, GRANT_NOT_AFTER_MS),
        Err(TimedError::ZeroWindow)
    ));

    let mut value = dimensions();
    value.assets = assets(&[ASSET_A]);
    assert!(matches!(
        build([4; 32], None, &value, GRANT_NOT_AFTER_MS),
        Err(TimedError::CeilingOutsideAssets)
    ));

    let mut value = dimensions();
    value.assets = ExplicitSet::deny_all();
    value.amount_ceilings = ExplicitSet::deny_all();
    value.rate_ceilings = ExplicitSet::deny_all();
    let denied = text(build([4; 32], None, &value, GRANT_NOT_AFTER_MS), "explicit deny-all");
    assert!(denied.assets.is_empty());
    assert!(denied.amount_ceilings.is_empty());
    assert!(denied.rate_ceilings.is_empty());
}

#[test]
fn references_are_strict_lowercase_hex() {
    let lower = "ab".repeat(32);
    assert_eq!(text(timed::parse_id(&lower, request_id()), "lowercase id"), [0xab; 32]);
    for refused in [
        "AB".repeat(32),
        format!("{}A", "a".repeat(63)),
        "a".repeat(63),
        "a".repeat(65),
        format!("+{}", "a".repeat(63)),
    ] {
        assert!(timed::parse_id(&refused, request_id()).is_err(), "{refused} must be refused");
    }

    let mut value = dimensions();
    value.assets = assets(&[ASSET_UPPER, ASSET_B]);
    value.amount_ceilings = ExplicitSet::allow(vec![ceiling(ASSET_B, 900)]);
    assert!(matches!(
        build([5; 32], None, &value, GRANT_NOT_AFTER_MS),
        Err(TimedError::Malformed)
    ));

    let mut value = dimensions();
    value.counterparties =
        ExplicitSet::allow(vec![text(Counterparty::new(ASSET_UPPER), "counterparty")]);
    assert!(matches!(
        build([5; 32], None, &value, GRANT_NOT_AFTER_MS),
        Err(TimedError::Malformed)
    ));

    let decoded = record([5; 32], None);
    assert_eq!(decoded.assets, BTreeSet::from([[0x33; 32], [0x44; 32]]));
    assert_eq!(decoded.counterparties, BTreeSet::from([[0x22; 32]]));
}

#[test]
fn attenuation_never_widens_any_dimension() {
    let parent = record([6; 32], None);

    let mut narrow = dimensions();
    narrow.activity_types = ExplicitSet::allow(vec![ActivityType(5)]);
    narrow.assets = assets(&[ASSET_A]);
    narrow.amount_ceilings = ExplicitSet::allow(vec![ceiling(ASSET_A, 400)]);
    narrow.rate_ceilings = ExplicitSet::allow(vec![window(60, 2), window(7_200, 10)]);
    narrow.expiry = TimestampSeconds(EXPIRY_SECONDS - 1);
    let child = text(build([7; 32], Some(parent.id), &narrow, GRANT_NOT_AFTER_MS), "narrow child");
    assert!(timed::require_subset(&child, &parent).is_ok());

    let wider = |change: &dyn Fn(&mut CapabilityDimensions), dimension: Dimension| {
        let mut value = dimensions();
        change(&mut value);
        let child = text(build([8; 32], Some(parent.id), &value, GRANT_NOT_AFTER_MS), "wider child");
        let refusal = timed::require_subset(&child, &parent);
        assert!(
            matches!(refusal, Err(TimedError::Wider(found)) if found == dimension),
            "{dimension:?} must refuse widening"
        );
        if let Err(error) = refusal {
            assert_eq!(error.owner_error(), HumanOperationError::CapabilityRefused(dimension));
        }
    };
    wider(&|v| v.expiry = TimestampSeconds(EXPIRY_SECONDS + 1), Dimension::Expiry);
    wider(
        &|v| v.activity_types = ExplicitSet::allow(vec![ActivityType(5), ActivityType(6), ActivityType(7)]),
        Dimension::ActivityType,
    );
    wider(
        &|v| {
            v.counterparties = ExplicitSet::allow(vec![
                text(Counterparty::new(COUNTERPARTY), "counterparty"),
                text(Counterparty::new("55".repeat(32)), "counterparty"),
            ]);
        },
        Dimension::Counterparty,
    );
    wider(
        &|v| {
            v.assets = assets(&[ASSET_A, ASSET_B, &"66".repeat(32)]);
        },
        Dimension::Asset,
    );
    wider(
        &|v| v.amount_ceilings = ExplicitSet::allow(vec![ceiling(ASSET_A, 501), ceiling(ASSET_B, 900)]),
        Dimension::Amount,
    );
    wider(
        &|v| v.rate_ceilings = ExplicitSet::allow(vec![window(30, 3), window(3_600, 10)]),
        Dimension::Rate,
    );
    wider(
        &|v| {
            v.purpose_constraints = ExplicitSet::allow(vec![
                text(Purpose::new("service"), "purpose"),
                text(Purpose::new("other"), "purpose"),
            ]);
        },
        Dimension::Purpose,
    );

    let mut empty_parent_dimensions = dimensions();
    empty_parent_dimensions.purpose_constraints = ExplicitSet::deny_all();
    let empty_parent = text(
        build([9; 32], None, &empty_parent_dimensions, GRANT_NOT_AFTER_MS),
        "deny-all parent",
    );
    assert!(matches!(
        timed::require_subset(&record([10; 32], Some(empty_parent.id)), &empty_parent),
        Err(TimedError::Wider(Dimension::Purpose))
    ));
}

#[test]
fn list_is_strictly_ascending_by_id_and_scoped_to_the_agent() {
    let root = Root(test_directory("list"));
    let mut store = text(Store::open(&root.0), "store");
    for id in [[0x30; 32], [0x10; 32], [0x20; 32]] {
        inserted(&mut store, record(id, None));
    }
    let foreign = text(
        TimedCapability::from_public(
            [0x15; 32],
            None,
            tenant(),
            "agent-b",
            ProtocolAuthority::SessionKey([4; 32]),
            &dimensions(),
            GRANT_NOT_AFTER_MS,
            CREATED_AT_MS,
            CREATED_AT_SEQUENCE,
            request_id(),
        ),
        "foreign agent",
    );
    inserted(&mut store, foreign);

    let listed = text(timed::list(&store, &tenant(), "agent-a"), "list");
    let ids: Vec<[u8; 32]> = listed.iter().map(|value| value.id).collect();
    assert_eq!(ids, vec![[0x10; 32], [0x20; 32], [0x30; 32]]);
    assert!(ids.windows(2).all(|pair| pair[0] < pair[1]));

    assert!(matches!(
        text(timed::insert(&mut store, record([0x10; 32], None)), "replay"),
        Insert::Replayed(_)
    ));
    let mut changed = dimensions();
    changed.purpose_constraints = ExplicitSet::allow(vec![text(Purpose::new("changed"), "purpose")]);
    let conflicting = text(build([0x10; 32], None, &changed, GRANT_NOT_AFTER_MS), "changed body");
    assert!(matches!(timed::insert(&mut store, conflicting), Err(TimedError::Conflict)));
    assert_eq!(text(timed::list(&store, &tenant(), "agent-a"), "relist").len(), 3);
}

#[test]
fn subtree_revocation_is_durable_across_a_store_reopen() {
    let root = Root(test_directory("revoke"));
    let parent = [0x41; 32];
    let child = [0x42; 32];
    let grandchild = [0x43; 32];
    let sibling = [0x44; 32];
    {
        let mut store = text(Store::open(&root.0), "store");
        inserted(&mut store, record(parent, None));
        inserted(&mut store, record(child, Some(parent)));
        inserted(&mut store, record(grandchild, Some(child)));
        inserted(&mut store, record(sibling, None));
        let (target, revoked) = text(
            timed::revoke_subtree(&mut store, &tenant(), "agent-a", &parent, CREATED_AT_MS + 7, CREATED_AT_SEQUENCE + 3),
            "revoke subtree",
        );
        assert_eq!(target.id, parent);
        assert_eq!(target.revoked, Some((CREATED_AT_MS + 7, CREATED_AT_SEQUENCE + 3)));
        assert_eq!(
            revoked.into_iter().collect::<BTreeSet<_>>(),
            BTreeSet::from([parent, child, grandchild])
        );
        assert!(matches!(
            timed::revoke_subtree(&mut store, &tenant(), "agent-a", &[0x7f; 32], CREATED_AT_MS + 8, CREATED_AT_SEQUENCE + 4),
            Err(TimedError::NotFound)
        ));
    }

    let store = text(Store::open(&root.0), "reopened store");
    for id in [parent, child, grandchild] {
        let restored = text(timed::restore(&store, &tenant(), &id), "restore")
            .unwrap_or_else(|| panic!("revoked record must survive reopen"));
        assert_eq!(restored.revoked, Some((CREATED_AT_MS + 7, CREATED_AT_SEQUENCE + 3)));
        assert!(matches!(restored.state(CREATED_AT_MS + 8), TimedState::Revoked));
    }
    let untouched = text(timed::restore(&store, &tenant(), &sibling), "restore sibling")
        .unwrap_or_else(|| panic!("sibling must survive reopen"));
    assert_eq!(untouched.revoked, None);
    assert!(matches!(untouched.state(CREATED_AT_MS + 8), TimedState::Active));
    let restored_grandchild = text(timed::restore(&store, &tenant(), &grandchild), "restore grandchild")
        .unwrap_or_else(|| panic!("grandchild must survive reopen"));
    assert!(timed::require_active_chain(&store, &restored_grandchild, CREATED_AT_MS + 8).is_err());
    assert!(timed::require_active_chain(&store, &untouched, CREATED_AT_MS + 8).is_ok());
}

#[test]
fn dimension_refusals_carry_their_dimension_to_the_owner_error() {
    for dimension in [
        Dimension::Expiry,
        Dimension::ActivityType,
        Dimension::Counterparty,
        Dimension::Asset,
        Dimension::Amount,
        Dimension::Rate,
        Dimension::Purpose,
    ] {
        assert_eq!(
            TimedError::Wider(dimension).owner_error(),
            HumanOperationError::CapabilityRefused(dimension)
        );
    }
    assert_eq!(TimedError::Corrupt.owner_error(), HumanOperationError::Unavailable);
    assert_eq!(TimedError::ZeroWindow.owner_error(), HumanOperationError::Refused);
    assert_eq!(TimedError::Duplicate(Dimension::Asset).owner_error(), HumanOperationError::Refused);
    assert_eq!(TimedError::CeilingOutsideAssets.owner_error(), HumanOperationError::Refused);
    assert_eq!(TimedError::Malformed.owner_error(), HumanOperationError::Refused);
}
