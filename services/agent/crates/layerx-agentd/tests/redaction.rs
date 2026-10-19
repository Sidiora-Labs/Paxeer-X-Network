use std::collections::BTreeSet;

use layerx_agentd::audit::{
    protect_payload, redact, DataClass, OutputSurface, PayloadEvidence, RedactionError,
    RedactionRegistry,
};
use layerx_agentd::obs;
use layerx_agentd::store::TenantId;
use layerx_agentd::tenant::{Config, RedactionPolicy, Retention};
use layerx_crypto::redact::Secret;
use layerx_types::verify::VerificationLevel;

const PRIVATE_MARKER: &[u8] = b"private-key-material-marker";
const TOKEN_MARKER: &[u8] = b"session-token-value-marker";
const CONFIG_MARKER: &[u8] = b"secret-configuration-marker";
const PAYLOAD_MARKER: &[u8] = b"signed-payload-marker";

fn tenant(value: &str) -> TenantId {
    TenantId::new(value).unwrap_or_else(|error| panic!("tenant: {error}"))
}

fn config(value: &str, policy: RedactionPolicy, audit: u64) -> Config {
    Config {
        tenant: tenant(value),
        policy_version: format!("{value}-policy"),
        redaction: policy,
        retention: Retention {
            events: 100,
            audit,
            receipts: 100,
        },
        verification_default: VerificationLevel::STATE_PROVEN,
        approval_required_for: BTreeSet::from([7]),
    }
}

fn contains_marker(output: &str) -> bool {
    [PRIVATE_MARKER, TOKEN_MARKER, CONFIG_MARKER, PAYLOAD_MARKER]
        .into_iter()
        .any(|marker| {
            output
                .as_bytes()
                .windows(marker.len())
                .any(|part| part == marker)
        })
}

#[test]
fn every_output_surface_uses_identical_secret_and_retention_redaction() {
    let config = config("alpha", RedactionPolicy::Standard, 10);
    let surfaces = [
        OutputSurface::Audit,
        OutputSurface::Log,
        OutputSurface::Metric,
        OutputSurface::Trace,
        OutputSurface::Error,
    ];
    let sensitive = [
        (DataClass::PrivateKey, PRIVATE_MARKER),
        (DataClass::SessionToken, TOKEN_MARKER),
        (DataClass::SecretConfiguration, CONFIG_MARKER),
        (
            DataClass::SignedPayload {
                written_sequence: 1,
            },
            PAYLOAD_MARKER,
        ),
    ];
    for surface in surfaces {
        for (class, marker) in sensitive {
            let rendered = if surface == OutputSurface::Audit {
                redact(&config, &config.tenant, surface, class, marker, 20)
            } else {
                obs::redact(&config, &config.tenant, surface, class, marker, 20)
            }
            .unwrap_or_else(|error| panic!("redact {surface:?}: {error}"));
            let output = format!("{rendered:?} {}", rendered.value);
            assert!(!contains_marker(&output), "{surface:?} leaked: {output}");
            assert_eq!(rendered.value.as_str(), "[REDACTED]");
        }
    }
}

#[test]
fn tenant_configuration_never_falls_back_or_cross_applies() {
    let alpha = config("alpha", RedactionPolicy::Standard, 10);
    let beta = config("beta", RedactionPolicy::ReceiptOnly, 1);
    let mut registry = RedactionRegistry::default();
    registry.configure(&alpha);
    registry.configure(&beta);

    let alpha_output = registry
        .render(
            &alpha.tenant,
            OutputSurface::Audit,
            DataClass::SignedPayload {
                written_sequence: 10,
            },
            PAYLOAD_MARKER,
            11,
        )
        .unwrap_or_else(|error| panic!("alpha render: {error}"));
    let beta_output = registry
        .render(
            &beta.tenant,
            OutputSurface::Audit,
            DataClass::SignedPayload {
                written_sequence: 10,
            },
            PAYLOAD_MARKER,
            11,
        )
        .unwrap_or_else(|error| panic!("beta render: {error}"));
    assert!(alpha_output.value.as_str().starts_with("sha256:"));
    assert_eq!(beta_output.value.as_str(), "[REDACTED]");
    assert!(matches!(
        registry.render(
            &tenant("missing"),
            OutputSurface::Log,
            DataClass::PublicText,
            b"safe",
            11,
        ),
        Err(RedactionError::MissingTenantConfiguration)
    ));
    assert!(matches!(
        redact(
            &alpha,
            &beta.tenant,
            OutputSurface::Error,
            DataClass::PublicText,
            b"safe",
            11,
        ),
        Err(RedactionError::WrongTenant)
    ));
}

#[test]
fn audit_payload_evidence_never_contains_full_bytes_and_expires_to_no_digest() {
    let config = config("alpha", RedactionPolicy::Standard, 5);
    let retained = protect_payload(&config, 10, 15, PAYLOAD_MARKER);
    let expired = protect_payload(&config, 10, 16, PAYLOAD_MARKER);
    assert!(matches!(retained, PayloadEvidence::Digest(_)));
    assert_eq!(expired, PayloadEvidence::Redacted);
    let output = format!("{retained:?} {expired:?}");
    assert!(!contains_marker(&output));
}

#[test]
fn loaded_keys_and_redaction_errors_never_render_input_values() {
    let mut key = [0_u8; 32];
    key[..PRIVATE_MARKER.len()].copy_from_slice(PRIVATE_MARKER);
    let loaded = Secret::new(key);
    assert_eq!(format!("{loaded:?}"), "[REDACTED]");

    let config = config("alpha", RedactionPolicy::Strict, 5);
    let Err(error) = redact(
        &config,
        &config.tenant,
        OutputSurface::Error,
        DataClass::PublicText,
        b"invalid\0secret-configuration-marker",
        1,
    ) else {
        panic!("invalid configuration input must be refused")
    };
    let output = format!("{error:?} {error}");
    assert!(!contains_marker(&output));
}
