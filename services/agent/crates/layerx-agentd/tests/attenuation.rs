use std::collections::BTreeSet;
use std::fs;
use std::sync::atomic::{AtomicU64, Ordering};

use layerx_agentd::capability::{
    attenuate, revoke_subtree, AttenuationError, Capability, CapabilityDimensions, CapabilityGraph,
    CapabilityId, Dimension, RateCeiling, RevocableActivity,
};
use layerx_agentd::store::{Store, TenantId};

static NEXT_DIRECTORY: AtomicU64 = AtomicU64::new(1);

fn capability(id: u8, amount: u128, activities: BTreeSet<u16>) -> Capability {
    Capability::new(
        CapabilityId([id; 32]),
        TenantId::new("tenant-a").unwrap_or_else(|error| panic!("tenant: {error}")),
        CapabilityDimensions {
            activity_types: activities,
            counterparties: BTreeSet::from([[2; 32]]),
            assets: BTreeSet::from([[3; 32]]),
            amount_ceiling: amount,
            rate_ceiling: RateCeiling {
                maximum_uses: 5,
                window_sequences: 10,
            },
            purposes: BTreeSet::from(["service".to_owned()]),
            expiry_sequence: 100,
        },
    )
    .unwrap_or_else(|error| panic!("capability: {error:?}"))
}

#[test]
fn deep_chain_is_traceable_and_widening_is_refused() {
    let mut graph = CapabilityGraph::new(
        TenantId::new("tenant-a").unwrap_or_else(|error| panic!("tenant: {error}")),
    );
    graph
        .add_root(capability(1, 1_000, BTreeSet::from([7, 8])))
        .unwrap_or_else(|error| panic!("root: {error:?}"));
    attenuate(
        &mut graph,
        CapabilityId([1; 32]),
        capability(2, 500, BTreeSet::from([7])),
    )
    .unwrap_or_else(|error| panic!("child: {error:?}"));
    attenuate(
        &mut graph,
        CapabilityId([2; 32]),
        capability(3, 100, BTreeSet::from([7])),
    )
    .unwrap_or_else(|error| panic!("grandchild: {error:?}"));
    assert_eq!(
        graph.chain(CapabilityId([3; 32])),
        Some(vec![
            CapabilityId([1; 32]),
            CapabilityId([2; 32]),
            CapabilityId([3; 32])
        ])
    );
    assert_eq!(
        attenuate(
            &mut graph,
            CapabilityId([2; 32]),
            capability(4, 700, BTreeSet::from([7])),
        ),
        Err(AttenuationError::Wider(Dimension::Amount))
    );
}

#[test]
fn middle_revocation_disables_subtree_and_only_cancels_unsubmitted() {
    let mut graph = CapabilityGraph::new(
        TenantId::new("tenant-a").unwrap_or_else(|error| panic!("tenant: {error}")),
    );
    graph
        .add_root(capability(1, 1_000, BTreeSet::from([7])))
        .unwrap_or_else(|error| panic!("root: {error:?}"));
    attenuate(
        &mut graph,
        CapabilityId([1; 32]),
        capability(2, 500, BTreeSet::from([7])),
    )
    .unwrap_or_else(|error| panic!("child: {error:?}"));
    attenuate(
        &mut graph,
        CapabilityId([2; 32]),
        capability(3, 100, BTreeSet::from([7])),
    )
    .unwrap_or_else(|error| panic!("grandchild: {error:?}"));
    let mut activities = [
        RevocableActivity {
            capability_id: CapabilityId([3; 32]),
            submitted: false,
            cancelled: false,
        },
        RevocableActivity {
            capability_id: CapabilityId([3; 32]),
            submitted: true,
            cancelled: false,
        },
    ];
    let result = revoke_subtree(&mut graph, CapabilityId([2; 32]), &mut activities)
        .unwrap_or_else(|error| panic!("revoke: {error:?}"));
    assert_eq!(result.cancelled_unsubmitted, 1);
    assert!(graph.is_enabled(CapabilityId([1; 32])));
    assert!(!graph.is_enabled(CapabilityId([2; 32])));
    assert!(!graph.is_enabled(CapabilityId([3; 32])));
    assert!(activities[0].cancelled);
    assert!(!activities[1].cancelled);
}

#[test]
fn graph_and_revocations_restore_consistently() {
    let sequence = NEXT_DIRECTORY.fetch_add(1, Ordering::Relaxed);
    let root = std::env::temp_dir().join(format!(
        "layerx-agentd-attenuation-{}-{sequence}",
        std::process::id()
    ));
    let tenant = TenantId::new("tenant-a").unwrap_or_else(|error| panic!("tenant: {error}"));
    let mut graph = CapabilityGraph::new(tenant.clone());
    graph
        .add_root(capability(1, 1_000, BTreeSet::from([7])))
        .unwrap_or_else(|error| panic!("root: {error:?}"));
    attenuate(
        &mut graph,
        CapabilityId([1; 32]),
        capability(2, 500, BTreeSet::from([7])),
    )
    .unwrap_or_else(|error| panic!("child: {error:?}"));
    revoke_subtree(&mut graph, CapabilityId([2; 32]), &mut [])
        .unwrap_or_else(|error| panic!("revoke: {error:?}"));
    let mut store = Store::open(&root).unwrap_or_else(|error| panic!("store: {error}"));
    graph
        .persist(&mut store)
        .unwrap_or_else(|error| panic!("persist: {error:?}"));
    drop(store);
    let reopened = Store::open(&root).unwrap_or_else(|error| panic!("reopen: {error}"));
    let restored = CapabilityGraph::restore(&reopened, tenant)
        .unwrap_or_else(|error| panic!("restore: {error:?}"))
        .unwrap_or_else(|| panic!("graph missing"));
    assert_eq!(
        restored.chain(CapabilityId([2; 32])),
        graph.chain(CapabilityId([2; 32]))
    );
    assert!(!restored.is_enabled(CapabilityId([2; 32])));
    let _ = fs::remove_dir_all(root);
}
