use std::collections::BTreeSet;
use std::fs;
use std::path::PathBuf;
use std::sync::atomic::{AtomicU64, Ordering};

use layerx_agentd::capability::{
    evaluate, Capability, CapabilityDimensions, CapabilityId, Decision, Dimension, PreparedIntent,
    RateCeiling,
};
use layerx_agentd::store::{Store, TenantId};

static NEXT_DIRECTORY: AtomicU64 = AtomicU64::new(1);

fn capability() -> Capability {
    Capability::new(
        CapabilityId([1; 32]),
        TenantId::new("tenant-a").unwrap_or_else(|error| panic!("tenant: {error}")),
        CapabilityDimensions {
            activity_types: BTreeSet::from([7]),
            counterparties: BTreeSet::from([[2; 32]]),
            assets: BTreeSet::from([[3; 32]]),
            amount_ceiling: 500,
            rate_ceiling: RateCeiling {
                maximum_uses: 4,
                window_sequences: 20,
            },
            purposes: BTreeSet::from(["service-payment".to_owned()]),
            expiry_sequence: 100,
        },
    )
    .unwrap_or_else(|error| panic!("capability: {error:?}"))
}

fn intent() -> PreparedIntent {
    PreparedIntent {
        activity_type: 7,
        counterparty: [2; 32],
        asset: [3; 32],
        amount: 500,
        purpose: "service-payment".to_owned(),
        core_sequence: 99,
        uses_in_window: 3,
    }
}

#[test]
fn every_dimension_is_required_and_checked_in_stable_order() {
    let capability = capability();
    assert_eq!(evaluate(&capability, &intent()), Decision::Allow);
    let mut cases = Vec::new();
    let mut value = intent();
    value.core_sequence = 100;
    cases.push((value, Dimension::Expiry));
    let mut value = intent();
    value.activity_type = 8;
    cases.push((value, Dimension::ActivityType));
    let mut value = intent();
    value.counterparty = [9; 32];
    cases.push((value, Dimension::Counterparty));
    let mut value = intent();
    value.asset = [9; 32];
    cases.push((value, Dimension::Asset));
    let mut value = intent();
    value.amount = 501;
    cases.push((value, Dimension::Amount));
    let mut value = intent();
    value.uses_in_window = 4;
    cases.push((value, Dimension::Rate));
    let mut value = intent();
    value.purpose = "other".to_owned();
    cases.push((value, Dimension::Purpose));
    for (value, dimension) in cases {
        assert_eq!(evaluate(&capability, &value), Decision::Refuse(dimension));
        assert_eq!(evaluate(&capability, &value), evaluate(&capability, &value));
    }
}

#[test]
fn bounds_accept_inside_and_at_limit_and_reject_outside() {
    let capability = capability();
    let mut below = intent();
    below.amount = 499;
    below.uses_in_window = 2;
    assert_eq!(evaluate(&capability, &below), Decision::Allow);
    assert_eq!(evaluate(&capability, &intent()), Decision::Allow);
    let mut above = intent();
    above.amount = 501;
    assert_eq!(
        evaluate(&capability, &above),
        Decision::Refuse(Dimension::Amount)
    );
}

#[test]
fn capability_restores_from_tenant_scoped_store() {
    let sequence = NEXT_DIRECTORY.fetch_add(1, Ordering::Relaxed);
    let root: PathBuf = std::env::temp_dir().join(format!(
        "layerx-agentd-capability-{}-{sequence}",
        std::process::id()
    ));
    let value = capability();
    let mut store = Store::open(&root).unwrap_or_else(|error| panic!("store: {error}"));
    value
        .persist(&mut store)
        .unwrap_or_else(|error| panic!("persist: {error:?}"));
    drop(store);
    let reopened = Store::open(&root).unwrap_or_else(|error| panic!("reopen: {error}"));
    let restored = Capability::restore(&reopened, value.tenant.clone(), value.id)
        .unwrap_or_else(|error| panic!("restore: {error:?}"));
    assert_eq!(restored, Some(value));
    let _ = fs::remove_dir_all(root);
}
