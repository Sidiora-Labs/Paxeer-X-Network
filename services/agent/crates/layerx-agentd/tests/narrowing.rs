use std::collections::BTreeSet;

use layerx_agentd::capability::{
    assert_narrowing, Capability, CapabilityDimensions, CapabilityId, Dimension, Enforcement,
    NarrowingError, ProtocolScope, RateCeiling,
};
use layerx_agentd::identity::ProtocolAuthority;
use layerx_agentd::store::TenantId;

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
                maximum_uses: 5,
                window_sequences: 10,
            },
            purposes: BTreeSet::from(["service".to_owned()]),
            expiry_sequence: 100,
        },
    )
    .unwrap_or_else(|error| panic!("capability: {error:?}"))
}

fn scope() -> ProtocolScope {
    ProtocolScope {
        activity_types: BTreeSet::from([7, 8]),
        counterparties: BTreeSet::from([[2; 32], [4; 32]]),
        assets: BTreeSet::from([[3; 32]]),
        amount_ceiling: 1_000,
        expires_at_sequence: 200,
        not_before_ms: 0,
        not_after_ms: 200_000,
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
fn wider_capability_is_rejected_at_creation_binding() {
    let mut value = capability();
    value.dimensions.activity_types.insert(99);
    assert_eq!(
        assert_narrowing(&value, ProtocolAuthority::SessionKey([5; 32]), &scope()),
        Err(NarrowingError::Wider(Dimension::ActivityType))
    );
}

#[test]
fn enforcement_is_reported_per_dimension_and_capability_never_becomes_authority() {
    let authority = ProtocolAuthority::SessionKey([5; 32]);
    let binding = assert_narrowing(&capability(), authority.clone(), &scope())
        .unwrap_or_else(|error| panic!("narrow binding: {error:?}"));
    assert_eq!(binding.submission_authority(), &authority);
    assert!(binding
        .report
        .dimensions
        .contains(&(Dimension::Amount, Enforcement::Protocol)));
    assert!(binding
        .report
        .dimensions
        .contains(&(Dimension::Purpose, Enforcement::DaemonOnly)));
}

#[test]
fn later_protocol_narrowing_disables_existing_capability() {
    let mut binding = assert_narrowing(
        &capability(),
        ProtocolAuthority::CapabilityGrant([8; 32]),
        &scope(),
    )
    .unwrap_or_else(|error| panic!("narrow binding: {error:?}"));
    let mut narrowed = scope();
    narrowed.amount_ceiling = 100;
    assert_eq!(
        binding.recheck(&narrowed),
        Err(NarrowingError::Wider(Dimension::Amount))
    );
    assert!(!binding.enabled);
}
