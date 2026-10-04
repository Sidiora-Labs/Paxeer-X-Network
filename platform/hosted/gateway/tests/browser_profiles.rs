use std::collections::BTreeMap;
use std::io::Cursor;

use layerx_platform_gateway::http::{
    browser_profile_request_header, browser_profile_response_header, browser_route_profile,
    read_request, read_response_with_browser_profile, request_header_is_forwardable,
    unified_account_preflight, write_response_connection_with_browser_profile,
    write_response_connection_with_origin, BrowserRouteProfile, IncomingRequest, OutgoingResponse,
};
use layerx_platform_gateway::{
    native_explorer_public_route, production_route, NativeExplorerPublicRoute, ProductionRoute,
};
use layerx_programs_runtime::ProgramId;
use serde_json::Value;

const ORIGIN: &str = "https://api-mainnet-beta.paxeer.network";
const CATALOGUE: &str = include_str!("../../../../tools/paxeer-x/route-catalogue.json");

fn request(method: &str, path: &str, headers: &[(&str, &str)]) -> IncomingRequest {
    let mut fields = BTreeMap::from([(
        "host".to_owned(),
        "api-mainnet-beta.paxeer.network".to_owned(),
    )]);
    fields.extend(
        headers
            .iter()
            .map(|(name, value)| ((*name).to_owned(), (*value).to_owned())),
    );
    IncomingRequest {
        method: method.to_owned(),
        path: path.to_owned(),
        headers: fields,
        body: Vec::new(),
    }
}

fn account_path() -> String {
    format!("/v1/accounts/{}/unified", "11".repeat(32))
}

#[test]
fn gas_preflight_uses_each_existing_station_binding_and_health_contract() {
    let catalogue: Value = serde_json::from_str(CATALOGUE).expect("actual production catalogue");
    let routes = catalogue["routes"].as_array().expect("routes");
    for operation in ["quote", "submit", "status", "retry"] {
        let path = format!("/gas-station/{operation}");
        let actual = |method: &str| {
            let found = routes
                .iter()
                .filter(|entry| entry["method"] == method && entry["path"] == path)
                .collect::<Vec<_>>();
            assert_eq!(found.len(), 1, "one actual {method} route for {path}");
            found[0]
        };
        let post = actual("POST");
        let preflight = actual("OPTIONS");
        for field in [
            "service",
            "upstream",
            "upstream_path",
            "transport",
            "timeout_seconds",
            "retries",
            "health_predicate",
            "timeout_scope",
            "connect_timeout_seconds",
            "source",
            "proxy",
        ] {
            assert_eq!(post[field], preflight[field], "preserved {field}");
        }
        assert_eq!(post["authentication"], "signed-sponsorship-request");
        assert_eq!(preflight["authentication"], "origin-checked-preflight");
        assert_eq!(preflight["upstream_path"], format!("/{operation}"));
        assert_eq!(
            browser_route_profile(&path),
            BrowserRouteProfile::GasStation
        );
        assert!(browser_profile_request_header(
            "OPTIONS",
            &path,
            "access-control-request-method",
            "POST"
        ));
        assert!(browser_profile_request_header(
            "OPTIONS",
            &path,
            "access-control-request-headers",
            "content-type"
        ));
        assert!(!browser_profile_request_header(
            "POST",
            &path,
            "access-control-request-method",
            "POST"
        ));
        assert!(!browser_profile_request_header(
            "OPTIONS",
            &path,
            "layerx-unified-profile",
            "2"
        ));
    }
    for path in [
        "/quote",
        "/gas-station/quote/",
        "/gas-station/unknown",
        "/v1/programs/call",
    ] {
        assert_eq!(browser_route_profile(path), BrowserRouteProfile::Legacy);
        assert!(!browser_profile_request_header(
            "OPTIONS",
            path,
            "access-control-request-method",
            "POST"
        ));
    }
}

#[test]
fn gas_response_parser_and_writer_preserve_exact_station_cors_without_credentials() {
    let wire = format!("HTTP/1.1 204 No Content\r\nContent-Length: 0\r\nAccess-Control-Allow-Origin: {ORIGIN}\r\nAccess-Control-Allow-Methods: POST\r\nAccess-Control-Allow-Headers: content-type\r\nVary: Origin, Access-Control-Request-Method, Access-Control-Request-Headers\r\nAccess-Control-Allow-Credentials: true\r\nX-Untrusted: discarded\r\nConnection: close\r\n\r\n");
    let upstream = read_response_with_browser_profile(
        &mut Cursor::new(wire.as_bytes()),
        BrowserRouteProfile::GasStation,
    )
    .expect("actual HTTP response parser");
    assert_eq!(upstream.headers.len(), 4);
    assert_eq!(
        upstream
            .headers
            .iter()
            .find(|(name, _)| name == "access-control-allow-origin")
            .map(|(_, value)| value.as_str()),
        Some(ORIGIN)
    );
    assert!(!upstream
        .headers
        .iter()
        .any(|(name, _)| name == "access-control-allow-credentials"));
    let response = OutgoingResponse {
        status: upstream.status,
        content_type: upstream.content_type,
        headers: upstream.headers,
        body: upstream.body,
        retry_after: None,
    };
    let mut output = Vec::new();
    write_response_connection_with_browser_profile(
        &mut output,
        &response,
        false,
        Some(ORIGIN),
        "/gas-station/quote",
    )
    .expect("actual downstream response writer");
    let output = String::from_utf8(output)
        .expect("HTTP ASCII")
        .to_ascii_lowercase();
    assert_eq!(output.matches("access-control-allow-origin:").count(), 1);
    assert!(output.contains("access-control-allow-methods: post\r\n"));
    assert!(output.contains("access-control-allow-headers: content-type\r\n"));
    assert!(output.contains(
        "vary: origin, access-control-request-method, access-control-request-headers\r\n"
    ));
    assert!(!output.contains("access-control-allow-credentials"));
    assert!(!output.contains("layerx-unified-profile"));
    let legacy = read_response_with_browser_profile(
        &mut Cursor::new(wire.as_bytes()),
        BrowserRouteProfile::Legacy,
    )
    .expect("legacy response parser");
    assert!(legacy.headers.is_empty());
    assert!(!browser_profile_response_header(
        BrowserRouteProfile::Legacy,
        "access-control-allow-origin"
    ));
    assert!(!browser_profile_response_header(
        BrowserRouteProfile::UnifiedAccount,
        "access-control-allow-origin"
    ));
}

#[test]
fn explorer_account_profile_is_exact_scoped_and_keeps_public_data_admission() {
    let catalogue: Value = serde_json::from_str(CATALOGUE).expect("actual production catalogue");
    let routes = catalogue["routes"].as_array().expect("routes");
    let entries = routes
        .iter()
        .filter(|route| {
            route["upstream"] == "layerx-explorer"
                && route["path"] == "/v1/accounts/{accountId}/unified"
        })
        .collect::<Vec<_>>();
    assert_eq!(entries.len(), 1);
    let entry = entries[0];
    assert_eq!(entry["authentication"], "public-read");
    assert_eq!(entry["service"], "explorer");
    assert_eq!(entry["method"], "GET");
    assert_eq!(entry["path"], "/v1/accounts/{accountId}/unified");
    assert_eq!(
        entry["source"],
        "human/crates/layerx-explorer-index/src/main.rs"
    );
    for account in [
        "11".repeat(32),
        format!("0x{}", "22".repeat(20)),
        format!("did:layerx:{}", "33".repeat(32)),
        format!("did%3Alayerx%3A{}", "44".repeat(32)),
    ] {
        let path = format!("/v1/accounts/{account}/unified");
        assert_eq!(
            browser_route_profile(&path),
            BrowserRouteProfile::UnifiedAccount
        );
        for profile in ["1", "2"] {
            assert!(browser_profile_request_header(
                "GET",
                &path,
                "layerx-unified-profile",
                profile
            ));
        }
        assert!(!browser_profile_request_header(
            "POST",
            &path,
            "layerx-unified-profile",
            "2"
        ));
        for profile in [
            "",
            "0",
            "3",
            "02",
            "2,1",
            " 2",
            "2\r\nAuthorization: injected",
        ] {
            assert!(!browser_profile_request_header(
                "GET",
                &path,
                "layerx-unified-profile",
                profile
            ));
        }
    }
    for path in [
        "/v1/accounts/11/unified",
        "/v1/accounts/../unified",
        "/v1/accounts/11/activity",
        "/v1/programs/read",
        "/explorer/backend/api/v2/addresses/11/unified",
    ] {
        assert_eq!(browser_route_profile(path), BrowserRouteProfile::Legacy);
        assert!(!browser_profile_request_header(
            "GET",
            path,
            "layerx-unified-profile",
            "2"
        ));
    }
    assert!(!request_header_is_forwardable("layerx-unified-profile"));
    assert!(!request_header_is_forwardable(
        "access-control-request-method"
    ));
    for header in [
        "x-layerx-principal",
        "x-layerx-api-key",
        "x-layerx-customer-authorization",
        "authorization",
        "connection",
        "upgrade",
    ] {
        assert!(!browser_profile_request_header(
            "GET",
            &account_path(),
            header,
            "2"
        ));
    }
}

#[test]
fn native_program_and_name_reads_use_exact_public_owner_routes() {
    let id = "11".repeat(32);
    let program = ProgramId::new([0x11; 32]).expect("actual nonzero program identifier");
    let record = format!("/v1/programs/{id}");
    let resolve = format!("{record}/reads/resolve?name=alice.layerx");
    assert_eq!(
        native_explorer_public_route("GET", &record),
        Some(NativeExplorerPublicRoute::Program(program))
    );
    assert_eq!(
        native_explorer_public_route("GET", &resolve),
        Some(NativeExplorerPublicRoute::Resolve(program))
    );
    let catalogue: Value = serde_json::from_str(CATALOGUE).expect("actual production catalogue");
    let entries = catalogue["routes"]
        .as_array()
        .expect("routes")
        .iter()
        .filter(|route| route["upstream"] == "layerx-explorer")
        .collect::<Vec<_>>();
    assert_eq!(entries.len(), 3);
    let account = entries
        .iter()
        .find(|entry| entry["path"] == "/v1/accounts/{accountId}/unified")
        .expect("existing native account owner");
    for path in [
        "/v1/programs/{program_id}",
        "/v1/programs/{program_id}/reads/resolve",
    ] {
        let routes = entries
            .iter()
            .filter(|entry| entry["path"] == path)
            .collect::<Vec<_>>();
        assert_eq!(routes.len(), 1, "one exact native public route");
        let route = routes[0];
        assert_eq!(route["method"], "GET");
        assert_eq!(route["authentication"], "public-read");
        assert_eq!(route["upstream_path"], path);
        for field in [
            "service",
            "upstream",
            "transport",
            "timeout_seconds",
            "retries",
            "health_predicate",
            "timeout_scope",
            "connect_timeout_seconds",
            "source",
            "proxy",
        ] {
            assert_eq!(route[field], account[field], "same genuine owner {field}");
        }
    }
    for path in [&record, &resolve] {
        assert_eq!(browser_route_profile(path), BrowserRouteProfile::Legacy);
        assert!(!browser_profile_request_header(
            "GET",
            path,
            "layerx-unified-profile",
            "2"
        ));
        for method in ["POST", "PUT", "DELETE", "OPTIONS"] {
            assert_eq!(native_explorer_public_route(method, path), None);
        }
    }
    for path in [
        format!("/v1/programs/{}", "00".repeat(32)),
        format!("/v1/programs/{}", "AA".repeat(32)),
        "/v1/programs/11".to_owned(),
        format!("{record}?name=alice.layerx"),
        format!("{record}/"),
        format!("{record}/reads"),
        format!("{record}/reads/resolve/extra"),
        format!("{record}/reads/../resolve"),
        format!("/v1/programs/%31{}/reads/resolve", "1".repeat(63)),
        "/v1/programs/registry".to_owned(),
        format!("/v1/programs/registry/{id}"),
        format!("/v1/programs/registry/{id}/interface"),
        format!("/v1/programs/activities/{id}"),
        format!("/v1/programs/receipts/by-idempotency/{id}"),
    ] {
        assert_eq!(
            native_explorer_public_route("GET", &path),
            None,
            "closed native selector {path}"
        );
    }
    assert_eq!(
        production_route("GET", "/v1/programs/registry"),
        Ok(ProductionRoute::ProgramCatalog)
    );
    assert_eq!(
        production_route("GET", &format!("/v1/programs/registry/{id}")),
        Ok(ProductionRoute::ProgramRegistry(id.as_str()))
    );
    assert_eq!(
        production_route("GET", &format!("/v1/programs/registry/{id}/interface")),
        Ok(ProductionRoute::ProgramInterface(id.as_str()))
    );
    for (path, owner) in [
        ("/v1/programs/call", ProductionRoute::ProgramCall),
        ("/v1/programs/simulate", ProductionRoute::ProgramSimulation),
        ("/v1/programs/read", ProductionRoute::ProgramRead),
        ("/v1/programs/deploy", ProductionRoute::ProgramDeploy),
        ("/v1/programs/upgrade", ProductionRoute::ProgramUpgrade),
        ("/v1/programs/wind-down", ProductionRoute::ProgramWindDown),
    ] {
        assert_eq!(production_route("POST", path), Ok(owner));
        assert_eq!(native_explorer_public_route("GET", path), None);
        assert_eq!(native_explorer_public_route("POST", path), None);
    }
}

#[test]
fn explorer_preflight_profile_uses_real_ingress_and_preserves_legacy_cors() {
    let path = account_path();
    let wire = format!("OPTIONS {path} HTTP/1.1\r\nHost: api-mainnet-beta.paxeer.network\r\nOrigin: {ORIGIN}\r\nAccess-Control-Request-Method: GET\r\nAccess-Control-Request-Headers: authorization, layerx-unified-profile\r\nContent-Length: 0\r\n\r\n");
    let valid = read_request(&mut Cursor::new(wire.as_bytes()), 4096)
        .expect("actual production HTTP ingress");
    assert!(unified_account_preflight(&valid));
    for method in ["POST", "get", "GET,POST", ""] {
        assert!(!unified_account_preflight(&request(
            "OPTIONS",
            &path,
            &[("access-control-request-method", method)]
        )));
    }
    for headers in [
        "x-layerx-principal",
        "authorization, upgrade",
        "layerx-unified-profile,",
        "",
        "x-layerx-api-key",
    ] {
        assert!(!unified_account_preflight(&request(
            "OPTIONS",
            &path,
            &[
                ("access-control-request-method", "GET"),
                ("access-control-request-headers", headers)
            ]
        )));
    }
    let response = OutgoingResponse {
        status: 204,
        content_type: "application/json".to_owned(),
        headers: Vec::new(),
        body: Vec::new(),
        retry_after: None,
    };
    let mut scoped = Vec::new();
    write_response_connection_with_browser_profile(
        &mut scoped,
        &response,
        false,
        Some(ORIGIN),
        &path,
    )
    .expect("profile response");
    let scoped = String::from_utf8(scoped).expect("HTTP ASCII");
    assert!(scoped.contains("Last-Event-ID, LayerX-Unified-Profile\r\n"));
    assert!(scoped.contains("Access-Control-Allow-Credentials: true\r\n"));
    let mut legacy = Vec::new();
    write_response_connection_with_origin(&mut legacy, &response, false, Some(ORIGIN))
        .expect("legacy response");
    assert!(!String::from_utf8(legacy)
        .expect("HTTP ASCII")
        .contains("LayerX-Unified-Profile"));
    let mut unrelated = Vec::new();
    write_response_connection_with_browser_profile(
        &mut unrelated,
        &response,
        false,
        Some(ORIGIN),
        "/v1/programs/read",
    )
    .expect("unrelated legacy response");
    assert!(!String::from_utf8(unrelated)
        .expect("HTTP ASCII")
        .contains("LayerX-Unified-Profile"));
}
