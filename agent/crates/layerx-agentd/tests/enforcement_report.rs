use std::collections::BTreeSet;

use layerx_agentd::capability::{
    assert_narrowing, check_guarantee_wording, enforcement_report, Capability,
    CapabilityDimensions, CapabilityId, DecisionEvidence, Dimension, Enforcement, ProtocolScope,
    RateCeiling, ReportError,
};
use layerx_agentd::identity::ProtocolAuthority;
use layerx_agentd::store::TenantId;

fn binding() -> layerx_agentd::capability::Binding {
    let capability = Capability::new(
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
    .unwrap_or_else(|error| panic!("capability: {error:?}"));
    let scope = ProtocolScope {
        activity_types: BTreeSet::from([7]),
        counterparties: BTreeSet::from([[2; 32]]),
        assets: BTreeSet::from([[3; 32]]),
        amount_ceiling: 500,
        expires_at_sequence: 100,
        not_before_ms: 0,
        not_after_ms: 100_000,
        enforceable_dimensions: BTreeSet::from([
            Dimension::ActivityType,
            Dimension::Counterparty,
            Dimension::Asset,
            Dimension::Amount,
            Dimension::Expiry,
        ]),
    };
    assert_narrowing(
        &capability,
        ProtocolAuthority::CapabilityGrant([9; 32]),
        &scope,
    )
    .unwrap_or_else(|error| panic!("binding: {error:?}"))
}

#[test]
fn report_covers_every_dimension_and_is_identical_on_all_surfaces() {
    let surfaces = enforcement_report(
        &binding(),
        vec![CapabilityId([1; 32])],
        DecisionEvidence {
            allowed: true,
            deciding_dimension: None,
            resulting_activity_id: Some([8; 32]),
        },
    )
    .unwrap_or_else(|error| panic!("report: {error:?}"));
    assert_eq!(surfaces.report.restrictions.len(), 7);
    assert_eq!(surfaces.contract, surfaces.command_line);
    assert_eq!(surfaces.command_line, surfaces.audit);
    let daemon_only: Vec<_> = surfaces
        .report
        .restrictions
        .iter()
        .filter(|item| item.enforcement == Enforcement::DaemonOnly)
        .collect();
    assert_eq!(daemon_only.len(), 2);
    assert!(daemon_only
        .iter()
        .all(|item| item.statement.contains("bypassing layerx-agentd bypasses")));
    assert!(surfaces
        .report
        .restrictions
        .iter()
        .filter(|item| item.enforcement == Enforcement::Protocol)
        .all(|item| item.protocol_object_id == Some([9; 32])));
}

#[test]
fn misleading_daemon_only_guarantee_wording_is_build_check_failure() {
    assert_eq!(
        check_guarantee_wording("daemon-enforced protocol guarantee"),
        Err(ReportError::MisleadingWording)
    );
    assert!(check_guarantee_wording(
        "daemon-enforced only; bypassing layerx-agentd bypasses this restriction"
    )
    .is_ok());
}
