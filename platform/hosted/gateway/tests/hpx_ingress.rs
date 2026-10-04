use std::fs;
use std::io::Write;
use std::net::{TcpListener, TcpStream};
use std::os::unix::fs::PermissionsExt;
use std::path::{Path, PathBuf};
use std::sync::atomic::{AtomicU64, Ordering};

use layerx_platform_gateway::http::{
    read_request, request_header_is_forwardable, Client, Endpoint, OutboundRequest,
};
use layerx_platform_gateway::{unified_service_route, HpxIngressPolicy};
use native_tls::Certificate;
use serde::Deserialize;

fn policy_file(value: serde_json::Value) -> PathBuf {
    static NEXT: AtomicU64 = AtomicU64::new(0);
    let directory = PathBuf::from(
        std::env::var_os("PAXEER_X_HPX_GATE_DIRECTORY").expect("real protected HPX gate directory"),
    );
    let path = directory.join(format!(
        "policy-{}-{}.json",
        std::process::id(),
        NEXT.fetch_add(1, Ordering::Relaxed)
    ));
    fs::write(&path, serde_json::to_vec(&value).expect("typed policy")).expect("policy write");
    fs::set_permissions(&path, fs::Permissions::from_mode(0o600)).expect("protected policy");
    path
}

fn socket_peer() -> std::net::SocketAddr {
    let listener = TcpListener::bind("127.0.0.1:0").expect("actual socket");
    let connection =
        TcpStream::connect(listener.local_addr().expect("actual listener")).expect("actual client");
    let (accepted, peer) = listener.accept().expect("actual accepted client");
    assert_eq!(accepted.peer_addr().expect("actual peer"), peer);
    drop(connection);
    peer
}

#[test]
fn real_socket_discards_untrusted_address_claims() {
    let peer = socket_peer();
    for header in [
        None,
        Some("8.8.8.8"),
        Some("unknown, 8.8.8.8"),
        Some("127.0.0.1:9000"),
        Some("8.8.8.8\r\nX-Real-IP: 1.1.1.1"),
    ] {
        assert_eq!(
            HpxIngressPolicy::default()
                .client_address(peer, header)
                .expect("actual direct peer"),
            peer.ip()
        );
    }
    for header in ["x-forwarded-for", "x-real-ip", "forwarded", "x-hpx-token"] {
        assert!(
            !request_header_is_forwardable(header),
            "global forwarding stays closed"
        );
    }
}

#[test]
fn explicitly_owned_proxy_policy_refuses_ambiguous_chains() {
    let peer = socket_peer();
    let path = policy_file(
        serde_json::json!({"schema":"layerx.hpx-ingress.v1","trusted_proxies":[peer.ip().to_string()]}),
    );
    let policy =
        HpxIngressPolicy::from_protected_file(&path).expect("actual operator-owned typed policy");
    for chain in [
        None,
        Some(""),
        Some("127.0.0.1"),
        Some("127.0.0.1,127.0.0.1"),
        Some("127.0.0.1:9000"),
        Some("[::1]"),
        Some("::ffff:127.0.0.1"),
        Some("127.0.0.01"),
        Some("unknown"),
        Some("127.0.0.1\n"),
    ] {
        assert!(
            policy.client_address(peer, chain).is_err(),
            "refused unsafe proxy claim"
        );
    }
    let overflow = vec!["127.0.0.1"; 17].join(",");
    assert!(policy.client_address(peer, Some(&overflow)).is_err());
}

#[test]
fn ingress_configuration_is_protected_bounded_and_explicit() {
    for value in [
        serde_json::json!({"schema":"layerx.hpx-ingress.v1","trusted_proxies":["*"]}),
        serde_json::json!({"schema":"layerx.hpx-ingress.v1","trusted_proxies":["127.0.0.0/8"]}),
        serde_json::json!({"schema":"layerx.hpx-ingress.v1","trusted_proxies":["127.0.0.1","127.0.0.1"]}),
        serde_json::json!({"schema":"layerx.hpx-ingress.v1","trusted_proxies":[]}),
        serde_json::json!({"schema":"layerx.hpx-ingress.v1","trusted_proxies":["0.0.0.0"]}),
        serde_json::json!({"schema":"layerx.hpx-ingress.v1","trusted_proxies":["127.0.0.1"],"trust_all":true}),
    ] {
        assert!(HpxIngressPolicy::from_protected_file(&policy_file(value)).is_err());
    }
    let path = policy_file(
        serde_json::json!({"schema":"layerx.hpx-ingress.v1","trusted_proxies":["127.0.0.1"]}),
    );
    fs::set_permissions(&path, fs::Permissions::from_mode(0o644)).expect("actual unsafe file");
    assert!(HpxIngressPolicy::from_protected_file(&path).is_err());
    let link = path.with_extension("link");
    std::os::unix::fs::symlink(&path, &link).expect("actual symlink");
    assert!(HpxIngressPolicy::from_protected_file(&link).is_err());
}

#[test]
fn exact_hpx_routes_preserve_the_existing_binding_and_authentication() {
    for (method, operation) in [("POST", "register"), ("GET", "myip")] {
        let path = format!("/hpx/api/{operation}");
        let route = unified_service_route(method, &path).expect("real HPX operation");
        assert_eq!(route["service"], "hpx");
        assert_eq!(route["upstream"], "hpx");
        assert_eq!(route["upstream_path"], format!("/api/{operation}"));
        assert_eq!(route["authentication"], "gateway-api-key");
        assert_eq!(
            route["health_predicate"],
            "service-ready-and-network-version-bound"
        );
        assert_eq!(route["retries"], 0);
        assert!(unified_service_route(method, &(path.clone() + "?ip=8.8.8.8")).is_none());
        assert!(unified_service_route(method, &(path + "/extra")).is_none());
    }
    assert!(unified_service_route("GET", "/hpx/api/register").is_none());
    assert!(unified_service_route("POST", "/hpx/api/myip").is_none());
    let caller = include_str!("../../../../hpx/hpx");
    assert!(caller.contains("hpx_api_request GET /api/myip 6"));
    assert!(caller.contains("hpx_api_request POST /api/register 12"));
    assert!(caller.contains("base=\"${base}/hpx\""));
    assert!(caller.contains("--header @-"));
    assert!(caller.contains("Authorization: Bearer %s"));
    assert!(caller.contains("X-HPX-Token: %s"));
    assert!(caller.contains("Environment=\"HPX_UNIFIED_GATEWAY_URL=%s\""));
    let serving = include_str!("../src/main.rs");
    assert!(serving.contains("let peer = tcp.peer_addr()"));
    assert!(serving.contains("route_with_peer(config, &request, Some(peer))"));
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct RealHpx {
    url: String,
    ca: String,
    token: String,
    node_id: String,
}

#[test]
fn actual_hpx_tls_handler_observes_sanitized_socket_identity_and_original_token_checks() {
    let path = PathBuf::from(
        std::env::var_os("PAXEER_X_HPX_GATE_DIRECTORY").expect("real HPX gate directory"),
    )
    .join("ready.json");
    let config: RealHpx =
        serde_json::from_slice(&fs::read(path).expect("real HPX process readiness"))
            .expect("typed actual process");
    let client = Client::without_identity(
        Certificate::from_der(
            &fs::read(Path::new(&config.ca)).expect("actual HPX TLS certificate"),
        )
        .expect("actual certificate"),
    );
    let endpoint = Endpoint::parse(&config.url).expect("actual configured HPX endpoint");
    let listener = TcpListener::bind("127.0.0.1:0").expect("actual ingress socket");
    let mut attacker = TcpStream::connect(listener.local_addr().expect("actual socket"))
        .expect("actual inbound connection");
    attacker.write_all(b"GET /hpx/api/myip HTTP/1.1\r\nHost: localhost\r\nX-Forwarded-For: 8.8.8.8\r\nX-Real-IP: 1.1.1.1\r\nContent-Length: 0\r\n\r\n").expect("actual forged request");
    let (mut accepted, peer) = listener.accept().expect("actual accepted stream");
    let request = read_request(&mut accepted, 4096).expect("actual production ingress parser");
    let address = HpxIngressPolicy::default()
        .client_address(
            peer,
            request.headers.get("x-forwarded-for").map(String::as_str),
        )
        .expect("actual sanitized peer");
    let response = client
        .request_hpx_forwarded(
            &endpoint,
            &OutboundRequest {
                method: "GET",
                path: "/api/myip",
                idempotency: None,
                content_type: "application/json",
                body: &[],
            },
            address,
            None,
        )
        .expect("actual HPX TLS discovery");
    assert_eq!(response.status, 200);
    assert_eq!(
        String::from_utf8(response.body).expect("IP text"),
        peer.ip().to_string()
    );
    let body = serde_json::to_vec(&serde_json::json!({"node_id":config.node_id,"p2p_port":26656,"moniker":"ingress-gate","type":"fullnode"})).expect("actual generated node identity");
    let request = OutboundRequest {
        method: "POST",
        path: "/api/register",
        idempotency: None,
        content_type: "application/json",
        body: &body,
    };
    assert_eq!(
        client
            .request_hpx_forwarded(&endpoint, &request, address, None)
            .expect("actual token refusal")
            .status,
        401
    );
    let refused = client
        .request_hpx_forwarded(&endpoint, &request, address, Some(&config.token))
        .expect("actual source-address refusal");
    assert_eq!(refused.status, 400);
    assert!(String::from_utf8(refused.body)
        .expect("actual refusal")
        .contains("public source address"));
    assert!(client
        .request_hpx_forwarded(
            &endpoint,
            &request,
            address,
            Some("invalid\r\nX-Forwarded-For: 8.8.8.8")
        )
        .is_err());
    let drift = OutboundRequest {
        path: "/api/register?ip=8.8.8.8",
        ..request
    };
    assert!(client
        .request_hpx_forwarded(&endpoint, &drift, address, Some(&config.token))
        .is_err());
    let peers = client
        .request_authorized(
            &endpoint,
            "",
            &OutboundRequest {
                method: "GET",
                path: "/api/nodes",
                idempotency: None,
                content_type: "application/json",
                body: &[],
            },
        )
        .expect("actual node registry");
    let nodes: serde_json::Value =
        serde_json::from_slice(&peers.body).expect("actual registry result");
    assert_eq!(nodes["count"], 0);
}
