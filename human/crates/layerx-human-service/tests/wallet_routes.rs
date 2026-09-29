use std::collections::BTreeSet;
use std::fmt::Debug;
use std::fs;
use std::io::{Read as _, Write as _};
use std::os::unix::net::UnixStream;
use std::path::{Path, PathBuf};
use std::sync::Arc;
use std::thread;

use layerx_client::runtime_clock::RuntimeClock;
use layerx_human_service::server::schema::{ApiSchema, Operation};
use layerx_human_service::server::{
    default_component_limits, HttpConfig, PrincipalLimits, Router, UnixComponents,
};
use serde_json::Value;

const APP_ORIGIN: &str = "https://app.wallet.example";
const ORIGINS: &str = "https://app.wallet.example,https://wallet.example";
const BEARER: &str = "eyJhbGciOiJFUzI1NiJ9.eyJzdWIiOiJ3YWxsZXQtdXNlci0wMDAxIn0.c2lnbmF0dXJl";
const SEND_CALL: &str = "this.send(";

fn required<T, E: Debug>(result: Result<T, E>, label: &str) -> T {
    result.unwrap_or_else(|error| panic!("{label}: {error:?}"))
}

fn sdk_source_root() -> PathBuf {
    Path::new(env!("CARGO_MANIFEST_DIR")).join("../../wallet/sdk/src")
}

fn golden_root() -> PathBuf {
    Path::new(env!("CARGO_MANIFEST_DIR")).join("../../schema/human-api/golden")
}

#[derive(Debug, Clone, PartialEq, Eq, PartialOrd, Ord)]
struct SdkRoute {
    method: String,
    path: String,
    file: String,
}

fn quoted(text: &str) -> Option<(&str, &str)> {
    let quote = text
        .chars()
        .next()
        .filter(|character| matches!(character, '\'' | '"' | '`'))?;
    let body = &text[quote.len_utf8()..];
    let end = body.find(quote)?;
    Some((&body[..end], &body[end + quote.len_utf8()..]))
}

fn send_calls(source: &str) -> Result<Vec<(String, String)>, String> {
    let mut calls = Vec::new();
    let mut rest = source;
    while let Some(at) = rest.find(SEND_CALL) {
        rest = &rest[at + SEND_CALL.len()..];
        let call = rest.lines().next().unwrap_or_default().trim().to_owned();
        let (method, after) = quoted(rest.trim_start())
            .ok_or_else(|| format!("send call without a literal method: {call}"))?;
        let after = after
            .trim_start()
            .strip_prefix(',')
            .ok_or_else(|| format!("send call without a path argument: {call}"))?;
        let (path, _) = quoted(after.trim_start())
            .ok_or_else(|| format!("send call without a literal path: {call}"))?;
        calls.push((method.to_owned(), path.to_owned()));
    }
    Ok(calls)
}

fn collect_sdk_routes(directory: &Path, routes: &mut BTreeSet<SdkRoute>) {
    let mut entries = required(fs::read_dir(directory), "SDK source directory")
        .map(|entry| required(entry, "SDK source entry").path())
        .collect::<Vec<_>>();
    entries.sort();
    for path in entries {
        if path.is_dir() {
            collect_sdk_routes(&path, routes);
            continue;
        }
        if path.extension().is_none_or(|extension| extension != "ts") {
            continue;
        }
        let source = required(fs::read_to_string(&path), "SDK source file");
        let file = path.display().to_string();
        for (method, route) in required(send_calls(&source), &file) {
            routes.insert(SdkRoute {
                method,
                path: route,
                file: file.clone(),
            });
        }
    }
}

fn sdk_routes() -> BTreeSet<SdkRoute> {
    let mut routes = BTreeSet::new();
    collect_sdk_routes(&sdk_source_root(), &mut routes);
    assert!(
        !routes.is_empty(),
        "no human API calls parsed under {}",
        sdk_source_root().display()
    );
    routes
}

fn is_parameter(segment: &str) -> bool {
    segment.contains("${")
}

fn probe_path(template: &str) -> String {
    template
        .split('/')
        .map(|segment| if is_parameter(segment) { "x" } else { segment })
        .collect::<Vec<_>>()
        .join("/")
}

fn live_path(template: &str, golden_path: &str) -> String {
    let template_segments = template.split('/').collect::<Vec<_>>();
    let golden_segments = golden_path.split('/').collect::<Vec<_>>();
    assert_eq!(
        template_segments.len(),
        golden_segments.len(),
        "{template} and {golden_path} differ in shape"
    );
    template_segments
        .into_iter()
        .zip(golden_segments)
        .map(|(declared, supplied)| {
            if is_parameter(declared) {
                supplied
            } else {
                declared
            }
        })
        .collect::<Vec<_>>()
        .join("/")
}

fn golden_request(operation: &Operation) -> Value {
    let path = golden_root().join(format!("{}.request.json", operation.name));
    let text = required(fs::read_to_string(&path), "golden request");
    required(serde_json::from_str(&text), "golden request JSON")
}

fn http_request(method: &str, path: &str, golden: &Value, bearer: Option<&str>) -> String {
    let mut head = format!(
        "{method} {path} HTTP/1.1\r\nHost: human.wallet.example\r\nOrigin: {APP_ORIGIN}\r\n"
    );
    if let Some(bearer) = bearer {
        head.push_str(&format!("Authorization: Bearer {bearer}\r\n"));
    }
    if let Some(headers) = golden.get("headers").and_then(Value::as_object) {
        for (name, value) in headers {
            let value = value
                .as_str()
                .unwrap_or_else(|| panic!("golden header {name} is not a string"));
            head.push_str(&format!("{name}: {value}\r\n"));
        }
    }
    match golden.get("body") {
        Some(body) => {
            let body = body.to_string();
            format!(
                "{head}Content-Type: application/json\r\nContent-Length: {}\r\n\r\n{body}",
                body.len()
            )
        }
        None => format!("{head}\r\n"),
    }
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
    body: Value,
}

impl Response {
    fn parse(raw: &str) -> Self {
        let (head, body) = raw
            .split_once("\r\n\r\n")
            .unwrap_or_else(|| panic!("HTTP body in {raw}"));
        let status_line = head.split("\r\n").next().unwrap_or_default();
        let status = status_line
            .split(' ')
            .nth(1)
            .and_then(|code| code.parse().ok())
            .unwrap_or_else(|| panic!("status in {status_line}"));
        Self {
            status,
            body: required(serde_json::from_str(body), "JSON body"),
        }
    }

    fn code(&self) -> &str {
        self.body["error"]["code"].as_str().unwrap_or_default()
    }
}

fn exchange(router: &Arc<Router<UnixComponents>>, request: &str) -> Response {
    let (mut client, mut server) = required(UnixStream::pair(), "HTTP pair");
    let shared = Arc::clone(router);
    let worker = thread::spawn(move || {
        required(shared.serve_one(&mut server, "wallet-routes-test"), "serve");
    });
    required(client.write_all(request.as_bytes()), "request");
    let mut raw = String::new();
    required(client.read_to_string(&mut raw), "response");
    required(worker.join().map_err(|_| "panicked"), "worker");
    Response::parse(&raw)
}

#[test]
fn wallet_routes_every_sdk_route_is_served_with_its_schema_and_admits_the_bearer() {
    let schema = required(ApiSchema::v1(), "schema");
    let router = router();
    for route in sdk_routes() {
        let label = format!("{} {} ({})", route.method, route.path, route.file);
        let matched = required(
            schema.route(&route.method, &probe_path(&route.path)),
            &label,
        )
        .unwrap_or_else(|| panic!("{label}: no human-api operation serves this route"));
        let operation = matched.operation;
        assert_eq!(operation.method, route.method, "{label}");
        let golden = golden_request(operation);
        assert_eq!(golden["method"], route.method, "{label}: golden method");
        let golden_path = golden["path"]
            .as_str()
            .unwrap_or_else(|| panic!("{label}: golden path"));
        let path = live_path(&route.path, golden_path);
        let routed = required(schema.route(&route.method, &path), &label)
            .unwrap_or_else(|| panic!("{label}: {path} does not route"));
        assert_eq!(routed.operation.name, operation.name, "{label}");

        let anonymous = exchange(&router, &http_request(&route.method, &path, &golden, None));
        assert_ne!(anonymous.status, 404, "{label}: {:?}", anonymous.body);
        assert_eq!(
            anonymous.code(),
            "unauthenticated",
            "{label}: {operation:?} refused the golden {} request before the credential check: {:?}",
            operation.request,
            anonymous.body
        );

        let bearer = exchange(
            &router,
            &http_request(&route.method, &path, &golden, Some(BEARER)),
        );
        assert_ne!(bearer.status, 404, "{label}: {:?}", bearer.body);
        assert_ne!(
            bearer.code(),
            "unauthenticated",
            "{label}: the wallet bearer is not admitted on {}: {:?}",
            operation.name,
            bearer.body
        );
    }
}

#[test]
fn wallet_routes_an_unknown_path_is_the_one_that_answers_not_found() {
    let response = exchange(
        &router(),
        &format!(
            "GET /v1/no-such-route HTTP/1.1\r\nHost: human.wallet.example\r\nOrigin: {APP_ORIGIN}\r\n\r\n"
        ),
    );
    assert_eq!(response.status, 404, "{:?}", response.body);
    assert_eq!(response.code(), "not-found");
}

#[test]
fn wallet_routes_parser_reads_every_quote_form_and_refuses_a_dynamic_call() {
    let source = "a(this.send('POST', '/v1/intents/plan', body));\n\
                  b(this.send(\"GET\", \"/v1/journeys\"));\n\
                  c(this.send('GET', `/v1/journeys/${journeyId}`, undefined));\n\
                  private async send(method: 'GET' | 'POST', path: string) {}\n";
    assert_eq!(
        required(send_calls(source), "literal calls"),
        vec![
            ("POST".to_owned(), "/v1/intents/plan".to_owned()),
            ("GET".to_owned(), "/v1/journeys".to_owned()),
            ("GET".to_owned(), "/v1/journeys/${journeyId}".to_owned()),
        ]
    );
    assert!(send_calls("this.send(method, path)").is_err());
    assert!(send_calls("this.send('GET', path)").is_err());
    assert_eq!(probe_path("/v1/journeys/${journeyId}"), "/v1/journeys/x");
    assert_eq!(
        live_path("/v1/journeys/${journeyId}", "/v1/journeys/jrn_1"),
        "/v1/journeys/jrn_1"
    );
}
