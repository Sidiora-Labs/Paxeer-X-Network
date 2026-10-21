use std::fmt::Debug;
use std::io::{Read as _, Write as _};
use std::net::TcpStream;
use std::os::unix::net::UnixStream;
use std::path::Path;
use std::sync::Arc;
use std::thread;
use std::time::Duration;

use layerx_client::runtime_clock::RuntimeClock;
use layerx_human_service::server::{
    default_component_limits, HttpConfig, Listener, PlainConfig, PlainServer, PrincipalLimits,
    Router, UnixComponents,
};
use serde_json::Value;

const APP_ORIGIN: &str = "https://app.wallet.example";
const SITE_ORIGIN: &str = "https://wallet.example";
const ORIGINS: &str = "https://app.wallet.example,https://wallet.example";
const UNLISTED_ORIGIN: &str = "https://other.example";
const PLAN_GOLDEN: &str = include_str!("../../../schema/human-api/golden/intent.plan.request.json");

fn required<T, E: Debug>(result: Result<T, E>, label: &str) -> T {
    result.unwrap_or_else(|error| panic!("{label}: {error:?}"))
}

fn config() -> HttpConfig {
    HttpConfig {
        maximum_header_bytes: 32_768,
        maximum_body_bytes: 1_048_576,
        allowed_origin: ORIGINS.to_owned(),
        service_version: "test".to_owned(),
    }
}

fn router() -> Arc<Router<UnixComponents>> {
    let backend = Arc::new(required(
        UnixComponents::new(
            Path::new("/run/layerx/human/absent-components.sock"),
            default_component_limits(),
        ),
        "backend",
    ));
    Arc::new(required(
        Router::new(
            backend,
            required(PrincipalLimits::new(100, 60, 100), "limits"),
            config(),
            required::<Arc<RuntimeClock>, _>(RuntimeClock::from_environment(), "clock authority"),
        ),
        "router",
    ))
}

struct Response {
    status: u16,
    headers: Vec<(String, String)>,
    body: Value,
}

impl Response {
    fn header(&self, name: &str) -> Option<&str> {
        self.headers
            .iter()
            .find(|(candidate, _)| candidate == name)
            .map(|(_, value)| value.as_str())
    }

    fn parse(raw: &str) -> Self {
        let (head, body) = raw
            .split_once("\r\n\r\n")
            .unwrap_or_else(|| panic!("HTTP body in {raw}"));
        let mut lines = head.split("\r\n");
        let status_line = lines.next().unwrap_or_default();
        let status = status_line
            .split(' ')
            .nth(1)
            .and_then(|code| code.parse().ok())
            .unwrap_or_else(|| panic!("status in {status_line}"));
        let headers = lines
            .filter_map(|line| line.split_once(':'))
            .map(|(name, value)| (name.trim().to_ascii_lowercase(), value.trim().to_owned()))
            .collect();
        let body = required(serde_json::from_str(body), "JSON body");
        Self {
            status,
            headers,
            body,
        }
    }
}

fn exchange(router: &Arc<Router<UnixComponents>>, request: &str) -> Response {
    let (mut client, mut server) = required(UnixStream::pair(), "HTTP pair");
    let shared = Arc::clone(router);
    let worker = thread::spawn(move || {
        required(shared.serve_one(&mut server, "browser-test"), "serve");
    });
    required(client.write_all(request.as_bytes()), "request");
    let mut raw = String::new();
    required(client.read_to_string(&mut raw), "response");
    required(worker.join().map_err(|_| "panicked"), "worker");
    Response::parse(&raw)
}

fn plan_request(origin: Option<&str>) -> String {
    let golden: Value = required(serde_json::from_str(PLAN_GOLDEN), "golden plan");
    let body = required(serde_json::to_string(&golden["body"]), "golden body");
    let origin = origin.map_or_else(String::new, |origin| format!("Origin: {origin}\r\n"));
    format!(
        "POST /v1/intents/plan HTTP/1.1\r\nHost: human.wallet.example\r\n{origin}Content-Type: application/json\r\nContent-Length: {}\r\n\r\n{body}",
        body.len()
    )
}

#[test]
fn preflight_from_a_listed_origin_is_answered_with_the_browser_headers() {
    let response = exchange(
        &router(),
        &format!("OPTIONS /v1/intents/plan HTTP/1.1\r\nHost: human.wallet.example\r\nOrigin: {SITE_ORIGIN}\r\nAccess-Control-Request-Method: POST\r\nAccess-Control-Request-Headers: authorization,content-type,idempotency-key\r\n\r\n"),
    );
    assert_eq!(response.status, 200, "{:?}", response.body);
    assert_eq!(
        response.header("access-control-allow-origin"),
        Some(SITE_ORIGIN)
    );
    let methods = response
        .header("access-control-allow-methods")
        .unwrap_or_default();
    for method in ["DELETE", "GET", "PATCH", "POST", "PUT"] {
        assert!(methods.contains(method), "{methods}");
    }
    let headers = response
        .header("access-control-allow-headers")
        .unwrap_or_default();
    for name in [
        "authorization",
        "content-type",
        "idempotency-key",
        "x-layerx-trace",
        "x-layerx-csrf",
    ] {
        assert!(headers.contains(name), "{headers}");
    }
    assert_eq!(response.header("access-control-max-age"), Some("600"));
    assert_eq!(
        response.header("access-control-expose-headers"),
        Some("X-LayerX-Trace")
    );
    assert_eq!(response.header("vary"), Some("Origin"));
    assert_eq!(response.header("access-control-allow-credentials"), None);
    assert_eq!(response.body["ok"], Value::Bool(true));
}

#[test]
fn preflight_from_an_unlisted_origin_is_forbidden() {
    let response = exchange(
        &router(),
        &format!("OPTIONS /v1/intents/plan HTTP/1.1\r\nHost: human.wallet.example\r\nOrigin: {UNLISTED_ORIGIN}\r\nAccess-Control-Request-Method: POST\r\n\r\n"),
    );
    assert_eq!(response.status, 403, "{:?}", response.body);
    assert_eq!(response.header("access-control-allow-origin"), None);
    assert_eq!(response.body["ok"], Value::Bool(false));
    assert_eq!(response.body["error"]["code"], "forbidden");
}

#[test]
fn intent_plan_admits_every_listed_origin_and_refuses_the_rest() {
    let router = router();
    for origin in [APP_ORIGIN, SITE_ORIGIN] {
        let response = exchange(&router, &plan_request(Some(origin)));
        assert_eq!(response.status, 401, "{origin}: {:?}", response.body);
        assert_eq!(response.body["error"]["code"], "unauthenticated");
        assert_eq!(response.header("access-control-allow-origin"), Some(origin));
        assert!(response.body["trace"].is_string());
    }
    for origin in [Some(UNLISTED_ORIGIN), None] {
        let response = exchange(&router, &plan_request(origin));
        assert_eq!(response.status, 403, "{origin:?}: {:?}", response.body);
        assert_eq!(response.body["error"]["code"], "forbidden");
        assert_eq!(response.header("access-control-allow-origin"), None);
    }
}

#[test]
fn live_check_over_plain_tcp_carries_the_allow_origin_for_a_listed_origin() {
    let bound = required(
        PlainServer::new(
            router(),
            PlainConfig {
                bind: required("127.0.0.1:0".parse(), "loopback"),
                maximum_connections: 8,
                io_deadline: Duration::from_secs(5),
            },
        )
        .bind(),
        "bind",
    );
    let address = required(bound.local_addr(), "local address");
    thread::spawn(move || {
        let _ = bound.serve();
    });
    for (origin, expected) in [(Some(APP_ORIGIN), Some(APP_ORIGIN)), (None, None)] {
        let mut client = required(TcpStream::connect(address), "connect");
        let origin = origin.map_or_else(String::new, |origin| format!("Origin: {origin}\r\n"));
        required(
            write!(
                client,
                "GET /livez HTTP/1.1\r\nHost: human.wallet.example\r\n{origin}\r\n"
            ),
            "live request",
        );
        let mut raw = String::new();
        required(client.read_to_string(&mut raw), "live response");
        let response = Response::parse(&raw);
        assert_eq!(response.status, 200, "{raw}");
        assert_eq!(response.body["result"]["live"], Value::Bool(true));
        assert_eq!(response.body["result"]["service"], "layerx-human-service");
        assert_eq!(response.header("access-control-allow-origin"), expected);
        assert_eq!(response.header("connection"), Some("close"));
    }
}

#[test]
fn listener_selection_refuses_tls_material_in_plain_mode_and_unknown_modes_by_name() {
    assert_eq!(
        Listener::parse(None, Some("/run/cert.der"), Some("/run/key.der")),
        Ok(Listener::Tls)
    );
    assert_eq!(
        Listener::parse(Some("tls"), Some("/run/cert.der"), Some("/run/key.der")),
        Ok(Listener::Tls)
    );
    assert_eq!(
        Listener::parse(Some("plain"), None, None),
        Ok(Listener::Plain)
    );
    let refused = Listener::parse(Some("plain"), Some("/run/cert.der"), None);
    assert!(
        refused
            .as_ref()
            .is_err_and(|message| message.contains("LAYERX_HUMAN_TLS_CERT_DER")),
        "{refused:?}"
    );
    let refused = Listener::parse(Some("plain"), None, Some("/run/key.der"));
    assert!(
        refused
            .as_ref()
            .is_err_and(|message| message.contains("LAYERX_HUMAN_TLS_KEY_DER")),
        "{refused:?}"
    );
    let refused = Listener::parse(Some("mutual"), None, None);
    assert!(
        refused
            .as_ref()
            .is_err_and(|message| message.contains("LAYERX_HUMAN_LISTENER")),
        "{refused:?}"
    );
}

#[test]
fn web_origin_list_accepts_only_bare_https_origins() {
    assert!(config().validate().is_ok());
    for origins in [
        "",
        "https://app.wallet.example,",
        "https://app.wallet.example,http://wallet.example",
        "https://app.wallet.example/,https://wallet.example",
        "https://app.wallet.example, https://wallet.example",
    ] {
        let config = HttpConfig {
            allowed_origin: origins.to_owned(),
            ..config()
        };
        assert!(config.validate().is_err(), "{origins}");
    }
}
