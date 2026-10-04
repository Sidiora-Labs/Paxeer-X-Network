use std::io::Cursor;

use layerx_platform_gateway::http::read_request;
use layerx_platform_gateway::{unified_service_catalogue, unified_service_route};

const WALLET: &str = include_str!("../../../../human/wallet/gateway/src/routes/wallet.ts");
const SIGN: &str = include_str!("../../../../human/wallet/gateway/src/routes/sign.ts");
const OWNER: &str = include_str!("../../../../human/wallet/gateway/src/index.ts");
const SDK_PROVIDER: &str = include_str!("../../../../human/wallet/sdk/src/provider.ts");
const SDK_LX: &str = include_str!("../../../../human/wallet/sdk/src/index.ts");
const SDK_GAS: &str = include_str!("../../../../human/wallet/sdk/src/modules/gas-station.ts");
const ARCHIVE: &str = include_str!("../../../../platform/relay_archive/runtime.py");

fn operation(method: &str, target: &str, source: &str) {
    let entry = unified_service_route(method, target).expect("actual unified service operation");
    assert_eq!(entry["service"], "wallet-gateway");
    assert_eq!(entry["upstream"], "wallet-gateway");
    assert_eq!(entry["authentication"], "requireAuth");
    assert_eq!(
        entry["health_predicate"],
        "service-ready-and-network-version-bound"
    );
    assert_eq!(entry["retries"], 0);
    assert_eq!(entry["proxy"], true);
    assert_eq!(entry["source"], source);
    let routes = unified_service_catalogue()["routes"]
        .as_array()
        .expect("production routes");
    assert_eq!(
        routes
            .iter()
            .filter(|route| route["method"] == method && route["path"] == entry["path"])
            .count(),
        1
    );
}

#[test]
fn real_registered_wallet_handlers_have_the_same_unified_owner() {
    assert!(OWNER.contains("app.register(walletSponsorRoutes)"));
    assert!(OWNER.contains("app.register(signRoutes)"));
    assert!(SIGN.contains("await lxApprovalRoutes(app, pool, attestors, limiter)"));
    for name in ["submit", "status"] {
        let path = format!("/v1/wallet/sponsored/{name}");
        assert!(WALLET.contains(&format!("app.post('{path}',{{preHandler:requireAuth}}")));
        operation("POST", &path, "human/wallet/gateway/src/routes/wallet.ts");
    }
    for path in [
        "/v1/wallet/sign-digest",
        "/v1/wallet/sign-custody",
        "/v1/wallet/lx/review",
        "/v1/wallet/lx/approve",
        "/v1/wallet/lx/sign",
    ] {
        assert!(SIGN.contains(&format!("app.post('{path}',{{preHandler:requireAuth")));
        operation("POST", path, "human/wallet/gateway/src/routes/sign.ts");
    }
    operation(
        "GET",
        &format!("/v1/wallet/custody/0x{}", "ab".repeat(32)),
        "human/wallet/gateway/src/routes/sign.ts",
    );
    operation(
        "GET",
        "/v1/wallet/lx/approvals/12345678-1234-4234-8234-123456789abc",
        "human/wallet/gateway/src/routes/sign.ts",
    );
    assert!(SIGN.contains("app.get('/v1/wallet/custody/:id',{preHandler:requireAuth"));
    assert!(SIGN.contains("app.get('/v1/wallet/lx/approvals/:id',{preHandler:requireAuth"));
}

#[test]
fn actual_sdk_callers_and_existing_ownership_checks_stay_connected() {
    for path in [
        "/v1/wallet/sign-digest",
        "/v1/wallet/sign-custody",
        "/v1/wallet/custody/",
    ] {
        assert!(SDK_PROVIDER.contains(path), "actual SDK caller {path}");
    }
    for path in [
        "/v1/wallet/lx/review",
        "/v1/wallet/lx/approve",
        "/v1/wallet/lx/sign",
        "/v1/wallet/lx/approvals/",
    ] {
        assert!(SDK_LX.contains(path), "actual retained LX caller {path}");
    }
    assert!(SDK_GAS.contains("/v1/wallet/sponsored/status"));
    assert!(SDK_GAS.contains("/v1/wallet/sponsored/submit"));
    assert!(WALLET.contains("wallet.address.toLowerCase()!==account.toLowerCase()"));
    assert!(SIGN.contains("where id=$1 and user_id=$2"));
    assert!(SIGN.contains("where id=$1 and principal=$2 for update"));
    assert!(SIGN.contains("explicit_lx_approval_required"));
    assert!(SIGN.contains("attestor_custody_required"));
}

#[test]
fn newly_connected_financial_routes_are_closed_to_methods_queries_and_path_aliases() {
    let custody = format!("/v1/wallet/custody/0x{}", "ab".repeat(32));
    for (method, path) in [
        ("GET", "/v1/wallet/sign-digest"),
        ("PUT", "/v1/wallet/lx/approve"),
        ("DELETE", "/v1/wallet/sponsored/submit"),
        ("POST", custody.as_str()),
        ("GET", "/v1/wallet/lx/approvals/not-a-uuid"),
        ("GET", "/v1/wallet/custody/0x11"),
        ("POST", "/v1/wallet/lx/sign?tenant=other"),
        ("POST", "/v1/wallet/lx/sign/"),
        ("POST", "/v1/wallet/lx/%73ign"),
        ("POST", "/v1/wallet/lx/../sign"),
        ("POST", "/v1/wallet/sponsored/unknown"),
        (
            "GET",
            "/v1/wallet/lx/approvals/12345678-1234-4234-8234-123456789abc?principal=other",
        ),
        ("GET", "/v1/sync/readiness?ready=true"),
        ("POST", "/v1/sync/readiness"),
        ("POST", "/internal/v1/wallet/sign-digest"),
        ("POST", "/v1/wallet/sign-digest?x=%0d%0aHost:other"),
    ] {
        assert!(
            unified_service_route(method, path).is_none(),
            "closed {method} {path}"
        );
    }
}

#[test]
fn real_ingress_retains_the_principal_credential_and_exact_financial_body() {
    let body = br#"{"approval_id":"12345678-1234-4234-8234-123456789abc"}"#;
    let head = format!("POST /v1/wallet/lx/sign HTTP/1.1\r\nHost: api-mainnet-beta.paxeer.network\r\nAuthorization: Bearer principal-session\r\nContent-Type: application/json\r\nContent-Length: {}\r\n\r\n", body.len());
    let wire = [head.as_bytes(), body.as_slice()].concat();
    let request = read_request(&mut Cursor::new(wire), 4096).expect("real production ingress");
    assert_eq!(
        request.headers.get("authorization").map(String::as_str),
        Some("Bearer principal-session")
    );
    assert_eq!(request.body, body);
    operation(
        &request.method,
        &request.path,
        "human/wallet/gateway/src/routes/sign.ts",
    );
}

#[test]
fn archive_sync_readiness_reaches_the_actual_sync_predicate() {
    let entry =
        unified_service_route("GET", "/v1/sync/readiness").expect("actual archive sync readiness");
    assert_eq!(entry["service"], "archive");
    assert_eq!(entry["upstream"], "archive");
    assert_eq!(entry["authentication"], "public");
    assert_eq!(entry["source"], "platform/relay_archive/runtime.py");
    assert!(ARCHIVE.contains("if path == \"/v1/sync/readiness\":"));
    assert!(ARCHIVE.contains("value = self.readiness()"));
    assert!(unified_service_route("GET", "/v1/sync/network").is_some());
    assert!(unified_service_route("GET", "/v1/history/activities?limit=10&cursor=0").is_some());
}
