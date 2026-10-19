//! Honest capability enforcement reporting shared by every public surface.

use std::fmt::Write as _;

use crate::identity::ProtocolAuthority;

use super::{Binding, CapabilityId, Dimension, Enforcement};

const DAEMON_ADVISORY: &str =
    "daemon-enforced only; bypassing layerx-agentd bypasses this restriction";

/// One fully classified restriction.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct RestrictionReport {
    pub dimension: Dimension,
    pub enforcement: Enforcement,
    pub protocol_object_id: Option<[u8; 32]>,
    pub statement: String,
}

/// Capability decision evidence recorded in audit and returned to callers.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct DecisionEvidence {
    pub allowed: bool,
    pub deciding_dimension: Option<Dimension>,
    pub resulting_activity_id: Option<[u8; 32]>,
}

/// Complete capability report.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct CapabilityReport {
    pub capability_id: CapabilityId,
    pub derivation_chain: Vec<CapabilityId>,
    pub restrictions: Vec<RestrictionReport>,
    pub decision: DecisionEvidence,
}

/// Identical canonical report bytes exposed by all three surfaces.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ReportSurfaces {
    pub report: CapabilityReport,
    pub contract: Vec<u8>,
    pub command_line: Vec<u8>,
    pub audit: Vec<u8>,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum ReportError {
    Incomplete,
    MisleadingWording,
}

pub(crate) fn build_report(
    binding: &Binding,
    derivation_chain: Vec<CapabilityId>,
    decision: DecisionEvidence,
) -> Result<ReportSurfaces, ReportError> {
    let protocol_object_id = authority_id(&binding.protocol_authority);
    let restrictions: Vec<_> = binding
        .report
        .dimensions
        .iter()
        .map(|(dimension, enforcement)| {
            let (object_id, statement) = match enforcement {
                Enforcement::Protocol => (
                    Some(protocol_object_id),
                    "protocol-enforced by the referenced authority object".to_owned(),
                ),
                Enforcement::DaemonOnly => (None, DAEMON_ADVISORY.to_owned()),
            };
            RestrictionReport {
                dimension: *dimension,
                enforcement: *enforcement,
                protocol_object_id: object_id,
                statement,
            }
        })
        .collect();
    if restrictions.len() != 7 || derivation_chain.last() != Some(&binding.capability.id) {
        return Err(ReportError::Incomplete);
    }
    for restriction in &restrictions {
        check_guarantee_wording(&restriction.statement)?;
    }
    let report = CapabilityReport {
        capability_id: binding.capability.id,
        derivation_chain,
        restrictions,
        decision,
    };
    let bytes = render(&report);
    Ok(ReportSurfaces {
        report,
        contract: bytes.clone(),
        command_line: bytes.clone(),
        audit: bytes,
    })
}

/// Rejects wording that calls a daemon-only control a protocol guarantee.
///
/// # Errors
///
/// Returns `ReportError::MisleadingWording` when the text names a control as
/// daemon-enforced and also calls it a protocol guarantee.
pub fn check_guarantee_wording(text: &str) -> Result<(), ReportError> {
    let normalized = text.to_ascii_lowercase();
    if normalized.contains("daemon-enforced") && normalized.contains("protocol guarantee") {
        return Err(ReportError::MisleadingWording);
    }
    Ok(())
}

fn authority_id(authority: &ProtocolAuthority) -> [u8; 32] {
    match authority {
        ProtocolAuthority::PrimaryKey(identifier)
        | ProtocolAuthority::SessionKey(identifier)
        | ProtocolAuthority::CapabilityGrant(identifier) => *identifier,
    }
}

fn render(report: &CapabilityReport) -> Vec<u8> {
    let mut text = format!(
        "capability={:02x?}\nchain={:02x?}\nallowed={}\n",
        report.capability_id.0,
        report
            .derivation_chain
            .iter()
            .map(|value| value.0)
            .collect::<Vec<_>>(),
        report.decision.allowed
    );
    for restriction in &report.restrictions {
        let _ = writeln!(
            text,
            "{:?}|{:?}|{}",
            restriction.dimension, restriction.enforcement, restriction.statement
        );
    }
    text.into_bytes()
}
