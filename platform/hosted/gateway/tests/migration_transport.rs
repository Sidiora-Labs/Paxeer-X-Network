use layerx_platform_gateway::http::{
    request_header_is_forwardable, Client, Endpoint, OutboundRequest,
};
use native_tls::Certificate;
use std::process::Command;

fn client() -> Client {
    let output = Command::new("openssl")
        .args([
            "req",
            "-x509",
            "-newkey",
            "rsa:2048",
            "-nodes",
            "-keyout",
            "/dev/null",
            "-days",
            "1",
            "-subj",
            "/CN=localhost",
            "-addext",
            "subjectAltName=DNS:localhost",
        ])
        .output()
        .unwrap_or_else(|error| panic!("certificate generation: {error}"));
    assert!(output.status.success());
    let ca = Certificate::from_pem(&output.stdout)
        .unwrap_or_else(|error| panic!("certificate: {error}"));
    Client::without_identity(ca)
}

#[test]
fn migration_rejects_injected_or_missing_original_credentials() {
    let client = client();
    let endpoint = Endpoint::parse("https://localhost:9").unwrap_or_else(|error| panic!("{error}"));
    let request = OutboundRequest {
        method: "POST",
        path: "/internal/v2/source-settlements",
        idempotency: None,
        content_type: "application/json",
        body: b"{}",
    };
    for customer in [
        "",
        "Bearer original\r\nX-LayerX-Expected-Did: forged",
        "Bearer original\0",
        "Bearer original\t",
    ] {
        let error = client
            .request_migration_source_settlement(
                &endpoint,
                "Bearer service",
                &request,
                Some("trace"),
                customer,
                &[7; 32],
            )
            .err()
            .unwrap_or_else(|| panic!("credential accepted"));
        assert_eq!(error, "migration source settlement outside boundary");
    }
    for service in [
        "",
        "Bearer ",
        "Basic service",
        "Bearer service\r\nInjected: yes",
    ] {
        let error = client
            .request_migration_source_settlement(
                &endpoint,
                service,
                &request,
                Some("trace"),
                "Bearer original",
                &[7; 32],
            )
            .err()
            .unwrap_or_else(|| panic!("service accepted"));
        assert_eq!(error, "migration source settlement outside boundary");
    }
}

#[test]
fn migration_carrier_rejects_other_targets_and_invalid_trace() {
    let client = client();
    let endpoint = Endpoint::parse("https://localhost:9").unwrap_or_else(|error| panic!("{error}"));
    for (method, path, content_type) in [
        ("GET", "/internal/v2/source-settlements", "application/json"),
        ("POST", "/v2/migration/assets", "application/json"),
        (
            "POST",
            "/internal/v2/source-settlements/",
            "application/json",
        ),
        ("POST", "/internal/v2/source-settlements", "text/plain"),
    ] {
        let request = OutboundRequest {
            method,
            path,
            idempotency: None,
            content_type,
            body: b"{}",
        };
        assert_eq!(
            client
                .request_migration_source_settlement(
                    &endpoint,
                    "Bearer service",
                    &request,
                    None,
                    "Bearer original",
                    &[7; 32]
                )
                .err()
                .as_deref(),
            Some("migration source settlement outside boundary")
        );
    }
    let request = OutboundRequest {
        method: "POST",
        path: "/internal/v2/source-settlements",
        idempotency: None,
        content_type: "application/json",
        body: b"{}",
    };
    for trace in ["", "trace\r\nInjected: yes"] {
        assert_eq!(
            client
                .request_migration_source_settlement(
                    &endpoint,
                    "Bearer service",
                    &request,
                    Some(trace),
                    "Bearer original",
                    &[7; 32]
                )
                .err()
                .as_deref(),
            Some("outbound trace exceeds its boundary")
        );
    }
}

#[test]
fn public_forwarding_cannot_supply_internal_migration_authority() {
    let client = client();
    let endpoint = Endpoint::parse("https://localhost:9").unwrap_or_else(|error| panic!("{error}"));
    let request = OutboundRequest {
        method: "POST",
        path: "/internal/v2/source-settlements",
        idempotency: None,
        content_type: "application/json",
        body: b"{}",
    };
    for name in ["x-layerx-customer-authorization", "x-layerx-expected-did"] {
        assert!(!request_header_is_forwardable(name));
        assert_eq!(
            client
                .request_forwarded(&endpoint, "Bearer original", &request, &[(name, "forged")])
                .err()
                .as_deref(),
            Some("forwarded header outside boundary")
        );
    }
}
