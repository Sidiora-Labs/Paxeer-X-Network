use layerx_platform_gateway::explorer_target::{owns_target, split_target, MAX_TARGET, PREFIX};
use super::{http, response, ws, IncomingRequest, OutgoingResponse};
use std::collections::BTreeSet;
use zeroize::Zeroizing;

const ORIGIN: &str = "https://api-mainnet-beta.paxeer.network";
const HOST: &str = "api-mainnet-beta.paxeer.network";
const UPSTREAM: &str = "https://explorer-backend-production-6fc2.up.railway.app";
const MAX_BODY: usize = 8 * 1024 * 1024;
const SOCKET_PATH: &str = "/socket/v2/websocket";

struct Route {
    method: &'static str,
    path: &'static str,
    session_csrf: bool,
}

fn matches_path(template: &str, path: &str) -> bool {
    let mut expected = template.split('/');
    let mut actual = path.split('/');
    loop {
        match (expected.next(), actual.next()) {
            (None, None) => return true,
            (Some(want), Some(got)) if want.starts_with('{') && want.ends_with('}') => {
                if got.is_empty()
                    || got.len() > 256
                    || !got
                        .bytes()
                        .all(|b| b.is_ascii_alphanumeric() || b"-._~".contains(&b))
                    || matches!(got, "." | "..")
                {
                    return false;
                }
            }
            (Some(want), Some(got)) if want == got => {}
            _ => return false,
        }
    }
}

fn selected(method: &str, path: &str) -> Option<&'static Route> {
    ROUTES
        .iter()
        .find(|r| r.method == method && matches_path(r.path, path))
}

fn explorer_cookie(name: &str) -> bool {
    matches!(name, "_explorer_key" | "api_temp_token")
}

fn cookie_value(value: &str) -> bool {
    !value
        .bytes()
        .any(|b| b < 0x21 || b >= 0x7f || matches!(b, b'"' | b',' | b';' | b'\\'))
}

fn cookies(value: &str) -> Result<Zeroizing<String>, String> {
    if value.len() > 4096 {
        return Err("explorer cookies exceed bound".into());
    }
    let mut output = Zeroizing::new(String::new());
    let mut seen = BTreeSet::new();
    for part in value.split(';') {
        let (name, value) = part
            .trim()
            .split_once('=')
            .ok_or("malformed browser cookie")?;
        if !explorer_cookie(name) {
            continue;
        }
        if !seen.insert(name) || !cookie_value(value) {
            return Err("explorer cookie refused".into());
        }
        if !output.is_empty() {
            output.push_str("; ");
        }
        output.push_str(name);
        output.push('=');
        output.push_str(value);
    }
    Ok(output)
}

pub(super) fn scope_cookie(value: &str) -> Result<Option<String>, String> {
    if value.len() > 4096 || !value.bytes().all(|b| b.is_ascii_graphic() || b == b' ') {
        return Err("explorer response cookie refused".into());
    }
    let mut parts = value.split(';');
    let first = parts.next().ok_or("empty response cookie")?.trim();
    let (name, cookie) = first.split_once('=').ok_or("malformed response cookie")?;
    if !explorer_cookie(name) {
        return Ok(None);
    }
    if !cookie_value(cookie) {
        return Err("explorer response cookie value refused".into());
    }
    let mut output = first.to_owned();
    let mut secure = false;
    let mut seen = BTreeSet::new();
    for part in parts {
        let part = part.trim();
        let attribute = part
            .split('=')
            .next()
            .unwrap_or_default()
            .to_ascii_lowercase();
        if !seen.insert(attribute.clone()) || attribute.is_empty() {
            return Err("duplicate or empty cookie attribute".into());
        }
        if matches!(attribute.as_str(), "domain" | "path") {
            continue;
        }
        if matches!(attribute.as_str(), "secure" | "httponly") && part.contains('=') {
            return Err("invalid cookie flag".into());
        }
        secure |= attribute == "secure";
        output.push_str("; ");
        output.push_str(part);
    }
    output.push_str("; Path=/explorer");
    if !secure {
        output.push_str("; Secure");
    }
    Ok(Some(output))
}

fn same_origin_referer(value: &str) -> bool {
    value.strip_prefix(ORIGIN).is_some_and(|rest| {
        rest == "/explorer" || rest.starts_with("/explorer/") || rest.starts_with("/explorer?")
    })
}

fn admitted_headers(
    request: &IncomingRequest,
    route: &Route,
) -> Result<Vec<(String, Zeroizing<String>)>, u16> {
    if request.headers.get("host").is_none_or(|v| v != HOST)
        || request.headers.contains_key("x-layerx-principal")
        || request.headers.contains_key("x-layerx-api-key")
        || request.body.len() > MAX_BODY
    {
        return Err(400);
    }
    let origin = request.headers.get("origin");
    let referer = request.headers.get("referer");
    if origin.is_some_and(|v| v != ORIGIN)
        || referer.is_some_and(|v| !same_origin_referer(v))
        || request
            .headers
            .get("sec-fetch-site")
            .is_some_and(|v| !matches!(v.as_str(), "same-origin" | "none"))
    {
        return Err(403);
    }
    let unsafe_request = request.method != "GET"
        || route.path == "/api/account/auth/logout"
        || route.path == "/api/account/v2/email/resend"
        || route.path == "/api/account/v2/authenticate_via_dynamic";
    if (unsafe_request || route.path == SOCKET_PATH) && origin.is_none() && referer.is_none() {
        return Err(403);
    }
    if route.session_csrf
        && request.method != "GET"
        && request
            .headers
            .get("x-csrf-token")
            .is_none_or(|v| v.is_empty())
    {
        return Err(403);
    }
    let mut forwarded = Vec::new();
    for name in [
        "accept",
        "accept-language",
        "origin",
        "referer",
        "x-csrf-token",
        "if-none-match",
        "if-modified-since",
    ] {
        if let Some(value) = request.headers.get(name) {
            if value.len() > 4096 || !value.bytes().all(|b| b.is_ascii_graphic() || b == b' ') {
                return Err(400);
            }
            forwarded.push((name.to_owned(), Zeroizing::new(value.clone())));
        }
    }
    if route.path == "/api/account/v2/authenticate_via_dynamic" {
        if let Some(value) = request.headers.get("authorization") {
            let token = value
                .strip_prefix("Bearer ")
                .or_else(|| value.strip_prefix("bearer "));
            if value.len() > 4096
                || token.is_none_or(|v| v.is_empty() || !v.bytes().all(|b| b.is_ascii_graphic()))
            {
                return Err(400);
            }
            forwarded.push(("authorization".into(), Zeroizing::new(value.clone())));
        }
    }
    if let Some(value) = request.headers.get("cookie") {
        let value = cookies(value).map_err(|_| 400_u16)?;
        if !value.is_empty() {
            forwarded.push(("cookie".into(), value));
        }
    }
    forwarded.push(("x-forwarded-host".into(), Zeroizing::new(HOST.into())));
    forwarded.push(("x-forwarded-proto".into(), Zeroizing::new("https".into())));
    Ok(forwarded)
}

fn location(value: &str) -> Result<String, String> {
    if let Some(path) = value.strip_prefix(UPSTREAM) {
        if !path.starts_with('/') {
            return Err("explorer redirect origin refused".into());
        }
        split_target(path)?;
        return Ok(format!("{PREFIX}{path}"));
    }
    if value.starts_with('/') {
        split_target(value)?;
        return Ok(format!("{PREFIX}{value}"));
    }
    if value.starts_with("https://")
        && !value.contains(['@', '\\'])
        && value.len() <= MAX_TARGET
        && value.bytes().all(|b| b.is_ascii_graphic())
    {
        return Ok(value.to_owned());
    }
    Err("explorer redirect refused".into())
}

pub(super) fn route(request: &IncomingRequest) -> Option<OutgoingResponse> {
    if !owns_target(&request.path) {
        return None;
    }
    Some(http_route(request))
}

fn http_route(request: &IncomingRequest) -> OutgoingResponse {
    let Ok((path, _)) = split_target(&request.path) else {
        return response(400, "explorer_target_refused", None);
    };
    let Some(upstream_path) = path.strip_prefix(PREFIX) else {
        return response(404, "not_found", None);
    };
    let Some(route) = selected(&request.method, upstream_path) else {
        return response(
            if ROUTES.iter().any(|r| matches_path(r.path, upstream_path)) {
                405
            } else {
                404
            },
            "explorer_route_not_admitted",
            None,
        );
    };
    if route.path == SOCKET_PATH || request.headers.contains_key("upgrade") {
        return response(400, "invalid_websocket_upgrade", None);
    }
    let forwarded = match admitted_headers(request, route) {
        Ok(value) => value,
        Err(status) => return response(status, "explorer_request_refused", None),
    };
    let Ok(endpoint) = http::Endpoint::parse(UPSTREAM) else {
        return response(503, "explorer_not_configured", None);
    };
    let target = &request.path[PREFIX.len()..];
    let headers: Vec<(&str, &str)> = forwarded
        .iter()
        .map(|(k, v)| (k.as_str(), v.as_str()))
        .collect();
    let answer = http::explorer_request(
        &endpoint,
        &http::OutboundRequest {
            method: &request.method,
            path: target,
            idempotency: None,
            content_type: request
                .headers
                .get("content-type")
                .map_or("", String::as_str),
            body: &request.body,
        },
        &headers,
    );
    let Ok(answer) = answer else {
        return response(502, "explorer_upstream_unavailable", None);
    };
    let mut output = OutgoingResponse {
        status: answer.status,
        content_type: answer.content_type,
        headers: Vec::new(),
        body: answer.body,
        retry_after: None,
    };
    for (name, value) in answer.headers {
        if name == "set-cookie" {
            match scope_cookie(&value) {
                Ok(Some(cookie)) => {
                    output.headers.push((name, cookie));
                }
                Ok(None) => {}
                Err(_) => return response(502, "explorer_cookie_refused", None),
            }
        } else if name == "location" {
            match location(&value) {
                Ok(value) => output.headers.push((name, value)),
                Err(_) => return response(502, "explorer_redirect_refused", None),
            }
        } else if name != "cache-control" {
            output.headers.push((name, value));
        }
    }
    output
}

pub(super) fn websocket<S: ws::Connection>(
    request: &IncomingRequest,
    stream: &mut S,
) -> Option<Result<(), String>> {
    if !owns_target(&request.path) {
        return None;
    }
    let Ok((path, _)) = split_target(&request.path) else {
        return None;
    };
    if path.strip_prefix(PREFIX) != Some(SOCKET_PATH) {
        return None;
    }
    let refusal = |stream: &mut S, status, reason| {
        http::write_response_connection(stream, &response(status, reason, None), false)
    };
    let Some(route) = selected(&request.method, SOCKET_PATH) else {
        return Some(refusal(stream, 405, "method_not_allowed"));
    };
    let forwarded = match admitted_headers(request, route) {
        Ok(value) => value,
        Err(status) => return Some(refusal(stream, status, "explorer_request_refused")),
    };
    let endpoint = match http::Endpoint::parse(UPSTREAM) {
        Ok(value) => value,
        Err(_) => return Some(refusal(stream, 503, "explorer_not_configured")),
    };
    let headers: Vec<(&str, &str)> = forwarded
        .iter()
        .filter(|(k, _)| {
            matches!(
                k.as_str(),
                "cookie" | "origin" | "x-forwarded-host" | "x-forwarded-proto"
            )
        })
        .map(|(k, v)| (k.as_str(), v.as_str()))
        .collect();
    Some(ws::serve_explorer(
        request,
        &endpoint,
        &request.path[PREFIX.len()..],
        &headers,
        stream,
    ))
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn explorer_query_and_finite_routes() {
        assert_eq!(ROUTES.len(), 141);
        assert!(selected("GET", "/api/v2/addresses/0xabc/transactions").is_some());
        assert!(selected("POST", "/api/v2/watch").is_none());
        assert!(selected("POST", "/api/v2/webhooks").is_none());
        assert!(selected("GET", "/api/v2/addresses/a/b/transactions").is_none());
        assert!(selected("GET", "/api/v2/addresses/%2e%2e/transactions").is_none());
        let target =
            "/explorer/backend/api/v2/transactions?type=ERC-20,ERC-721&q=a+b&filter%5Bkind%5D=x";
        assert_eq!(
            split_target(target).unwrap().1,
            target.split_once('?').map(|(_, q)| q)
        );
        for target in ["/explorer/backendx/", "/explorer/backends/api/v2/stats"] {
            assert!(!owns_target(target));
        }
        for target in [
            "/explorer/backend/api/../v2",
            "/explorer/backend/%2fapi",
            "/explorer/backend/api?q=%",
            "/explorer/backend/api?q=x\r\nHost:evil",
        ] {
            assert!(split_target(target).is_err());
        }
    }
    #[test]
    fn explorer_cookie_isolation_and_scope() {
        assert_eq!(cookies("wallet_session=secret; _explorer_key=opaque; human_token=secret; api_temp_token=temp").unwrap().as_str(), "_explorer_key=opaque; api_temp_token=temp");
        assert!(cookies("_explorer_key=a; _explorer_key=b").is_err());
        assert_eq!(
            scope_cookie(
                "_explorer_key=opaque; Domain=example.org; Path=/; Secure; HttpOnly; SameSite=Lax"
            )
            .unwrap(),
            Some("_explorer_key=opaque; Secure; HttpOnly; SameSite=Lax; Path=/explorer".into())
        );
        assert_eq!(scope_cookie("wallet_session=secret; Path=/").unwrap(), None);
        assert!(scope_cookie("_explorer_key=a\r\nX:evil").is_err());
    }
    #[test]
    fn explorer_dynamic_proof_is_endpoint_scoped() {
        let mut request = IncomingRequest {
            method: "GET".into(),
            path: format!("{PREFIX}/api/account/v2/authenticate_via_dynamic"),
            headers: [
                ("host".into(), HOST.into()),
                ("origin".into(), ORIGIN.into()),
                ("authorization".into(), "Bearer actual-token-shape".into()),
            ]
            .into(),
            body: Vec::new(),
        };
        let dynamic = selected("GET", "/api/account/v2/authenticate_via_dynamic").unwrap();
        assert!(admitted_headers(&request, dynamic)
            .unwrap()
            .iter()
            .any(|(name, value)| name == "authorization"
                && value.as_str() == "Bearer actual-token-shape"));
        let public = selected("GET", "/api/v2/stats").unwrap();
        assert!(!admitted_headers(&request, public)
            .unwrap()
            .iter()
            .any(|(name, _)| name == "authorization"));
        request.headers.remove("origin");
        assert_eq!(admitted_headers(&request, dynamic).unwrap_err(), 403);
    }
    #[test]
    fn explorer_session_origin_and_csrf() {
        let route = selected("POST", "/api/account/v2/user/watchlist").unwrap();
        let mut request = IncomingRequest {
            method: "POST".into(),
            path: format!("{PREFIX}{}", route.path),
            headers: [
                ("host".into(), HOST.into()),
                ("origin".into(), ORIGIN.into()),
                ("x-csrf-token".into(), "token".into()),
            ]
            .into(),
            body: b"{}".to_vec(),
        };
        assert!(admitted_headers(&request, route).is_ok());
        request.headers.remove("x-csrf-token");
        assert_eq!(admitted_headers(&request, route).unwrap_err(), 403);
        request
            .headers
            .insert("x-csrf-token".into(), "token".into());
        request
            .headers
            .insert("origin".into(), "https://untrusted.invalid".into());
        assert_eq!(admitted_headers(&request, route).unwrap_err(), 403);
    }
}

static ROUTES: &[Route] = &[
    Route {
        method: "GET",
        path: "/api/v2/addresses/{address_hash_param}/nft/collections",
        session_csrf: false,
    },
    Route {
        method: "GET",
        path: "/api/v2/addresses/{address_hash_param}/transactions/csv",
        session_csrf: false,
    },
    Route {
        method: "GET",
        path: "/api/v2/transactions/{transaction_hash_param}/raw-trace",
        session_csrf: false,
    },
    Route {
        method: "GET",
        path: "/api/v2/addresses/{address_hash_param}/transactions",
        session_csrf: false,
    },
    Route {
        method: "GET",
        path: "/api/v2/search",
        session_csrf: false,
    },
    Route {
        method: "GET",
        path: "/api/v2/addresses/{address_hash_param}/unified",
        session_csrf: false,
    },
    Route {
        method: "GET",
        path: "/api/v2/withdrawals",
        session_csrf: false,
    },
    Route {
        method: "GET",
        path: "/api/v2/addresses/{address_hash_param}/logs/csv",
        session_csrf: false,
    },
    Route {
        method: "GET",
        path: "/api/v2/config/backend",
        session_csrf: false,
    },
    Route {
        method: "GET",
        path: "/api/v2/proxy/account-abstraction/bundles",
        session_csrf: false,
    },
    Route {
        method: "GET",
        path: "/api/v2/transactions/{transaction_hash_param}/internal-transactions",
        session_csrf: false,
    },
    Route {
        method: "GET",
        path: "/api/v2/transactions/watchlist",
        session_csrf: false,
    },
    Route {
        method: "GET",
        path: "/api/v2/paxeer-x/receipts",
        session_csrf: false,
    },
    Route {
        method: "GET",
        path: "/api/v2/proxy/account-abstraction/paymasters",
        session_csrf: false,
    },
    Route {
        method: "GET",
        path: "/api/v2/config/db-background-migrations",
        session_csrf: false,
    },
    Route {
        method: "GET",
        path: "/api/v2/transactions/{transaction_hash_param}/logs",
        session_csrf: false,
    },
    Route {
        method: "GET",
        path: "/api/v2/proxy/account-abstraction/factories/{address_hash_param}",
        session_csrf: false,
    },
    Route {
        method: "GET",
        path: "/api/v1/search",
        session_csrf: false,
    },
    Route {
        method: "GET",
        path: "/api/v2/config/public-metrics",
        session_csrf: false,
    },
    Route {
        method: "GET",
        path: "/api/v2/transactions/stats",
        session_csrf: false,
    },
    Route {
        method: "GET",
        path: "/api/v2/search/check-redirect",
        session_csrf: false,
    },
    Route {
        method: "GET",
        path: "/api/v2/addresses/{address_hash_param}/internal-transactions/csv",
        session_csrf: false,
    },
    Route {
        method: "GET",
        path: "/api/v2/proxy/account-abstraction/accounts",
        session_csrf: false,
    },
    Route {
        method: "GET",
        path: "/api/v2/addresses/{address_hash_param}/logs",
        session_csrf: false,
    },
    Route {
        method: "PATCH",
        path: "/api/v2/tokens/{address_hash_param}/instances/{token_id_param}/refetch-metadata",
        session_csrf: false,
    },
    Route {
        method: "GET",
        path: "/api/v2/blocks",
        session_csrf: false,
    },
    Route {
        method: "GET",
        path: "/api/v2/withdrawals/counters",
        session_csrf: false,
    },
    Route {
        method: "GET",
        path: "/api/v2/proxy/account-abstraction/factories",
        session_csrf: false,
    },
    Route {
        method: "GET",
        path: "/api/v2/transactions/{transaction_hash_param}/summary",
        session_csrf: false,
    },
    Route {
        method: "GET",
        path: "/api/v2/proxy/account-abstraction/accounts/{address_hash_param}",
        session_csrf: false,
    },
    Route {
        method: "GET",
        path: "/api/v2/blocks/{block_hash_or_number_param}/transactions",
        session_csrf: false,
    },
    Route {
        method: "GET",
        path: "/api/v2/main-page/indexing-status",
        session_csrf: false,
    },
    Route {
        method: "GET",
        path: "/api/v2/tokens/{address_hash_param}/instances/{token_id_param}/transfers-count",
        session_csrf: false,
    },
    Route {
        method: "GET",
        path: "/api/v2/smart-contracts/counters",
        session_csrf: false,
    },
    Route {
        method: "GET",
        path: "/api/v2/addresses/{address_hash_param}/coin-balance-history-by-day",
        session_csrf: false,
    },
    Route {
        method: "GET",
        path: "/api/v2/proxy/account-abstraction/operations",
        session_csrf: false,
    },
    Route {
        method: "GET",
        path: "/api/v2/internal-transactions",
        session_csrf: false,
    },
    Route {
        method: "GET",
        path: "/api/v2/proxy/account-abstraction/paymasters/{address_hash_param}",
        session_csrf: false,
    },
    Route {
        method: "GET",
        path: "/api/v2/proxy/account-abstraction/operations/{operation_hash_param}",
        session_csrf: false,
    },
    Route {
        method: "GET",
        path: "/api/v2/addresses/{address_hash_param}/withdrawals",
        session_csrf: false,
    },
    Route {
        method: "GET",
        path: "/api/v2/addresses",
        session_csrf: false,
    },
    Route {
        method: "GET",
        path: "/api/v2/blocks/{block_number_param}/countdown",
        session_csrf: false,
    },
    Route {
        method: "GET",
        path: "/api/v2/addresses/{address_hash_param}",
        session_csrf: false,
    },
    Route {
        method: "GET",
        path: "/api/v2/transactions/{transaction_hash_param}/state-changes",
        session_csrf: false,
    },
    Route {
        method: "GET",
        path: "/api/v2/proxy/account-abstraction/bundlers",
        session_csrf: false,
    },
    Route {
        method: "PATCH",
        path: "/api/v2/tokens/{address_hash_param}/instances/refetch-metadata",
        session_csrf: false,
    },
    Route {
        method: "GET",
        path: "/api/v2/addresses/{address_hash_param}/coin-balance-history",
        session_csrf: false,
    },
    Route {
        method: "GET",
        path: "/api/v2/smart-contracts/",
        session_csrf: false,
    },
    Route {
        method: "GET",
        path: "/api/v2/addresses/{address_hash_param}/tabs-counters",
        session_csrf: false,
    },
    Route {
        method: "GET",
        path: "/api/v2/stats/charts/transactions",
        session_csrf: false,
    },
    Route {
        method: "GET",
        path: "/api/v2/tokens/{address_hash_param}/instances/{token_id_param}",
        session_csrf: false,
    },
    Route {
        method: "GET",
        path: "/api/v2/config/smart-contracts/languages",
        session_csrf: false,
    },
    Route {
        method: "GET",
        path: "/api/v2/config/indexer",
        session_csrf: false,
    },
    Route {
        method: "GET",
        path: "/api/v2/tokens/{address_hash_param}/counters",
        session_csrf: false,
    },
    Route {
        method: "GET",
        path: "/api/v2/paxeer-x/capabilities",
        session_csrf: false,
    },
    Route {
        method: "GET",
        path: "/api/v2/addresses/{address_hash_param}/counters",
        session_csrf: false,
    },
    Route {
        method: "GET",
        path: "/api/v2/main-page/transactions",
        session_csrf: false,
    },
    Route {
        method: "GET",
        path: "/api/v2/proxy/account-abstraction/bundlers/{address_hash_param}",
        session_csrf: false,
    },
    Route {
        method: "GET",
        path: "/api/v2/search/quick",
        session_csrf: false,
    },
    Route {
        method: "GET",
        path: "/api/v2/stats/charts/market",
        session_csrf: false,
    },
    Route {
        method: "GET",
        path: "/api/v2/transactions/{transaction_hash_param}/token-transfers",
        session_csrf: false,
    },
    Route {
        method: "GET",
        path: "/api/v2/token-transfers",
        session_csrf: false,
    },
    Route {
        method: "GET",
        path: "/api/v2/config/backend-version",
        session_csrf: false,
    },
    Route {
        method: "GET",
        path: "/api/v2/tokens/{address_hash_param}/instances/{token_id_param}/transfers",
        session_csrf: false,
    },
    Route {
        method: "GET",
        path: "/api/v2/smart-contracts/{address_hash_param}",
        session_csrf: false,
    },
    Route {
        method: "GET",
        path: "/api/v2/transactions/{transaction_hash_param}/status",
        session_csrf: false,
    },
    Route {
        method: "GET",
        path: "/api/v2/proxy/account-abstraction/status",
        session_csrf: false,
    },
    Route {
        method: "GET",
        path: "/api/v2/stats/hot-smart-contracts",
        session_csrf: false,
    },
    Route {
        method: "GET",
        path: "/api/v2/tokens/{address_hash_param}/instances/{token_id_param}/holders",
        session_csrf: false,
    },
    Route {
        method: "GET",
        path: "/api/v2/transactions",
        session_csrf: false,
    },
    Route {
        method: "GET",
        path: "/api/v2/tokens/{address_hash_param}/transfers",
        session_csrf: false,
    },
    Route {
        method: "GET",
        path: "/api/v2/transactions/{transaction_hash_param}/external-transactions",
        session_csrf: false,
    },
    Route {
        method: "GET",
        path: "/api/v2/addresses/{address_hash_param}/internal-transactions",
        session_csrf: false,
    },
    Route {
        method: "GET",
        path: "/api/v2/addresses/{address_hash_param}/blocks-validated",
        session_csrf: false,
    },
    Route {
        method: "GET",
        path: "/api/v2/stats/charts/secondary-coin-market",
        session_csrf: false,
    },
    Route {
        method: "GET",
        path: "/api/v2/main-page/blocks",
        session_csrf: false,
    },
    Route {
        method: "GET",
        path: "/api/v2/proxy/account-abstraction/operations/{operation_hash_param}/summary",
        session_csrf: false,
    },
    Route {
        method: "GET",
        path: "/api/v2/tokens/{address_hash_param}/instances",
        session_csrf: false,
    },
    Route {
        method: "GET",
        path: "/api/v2/stats",
        session_csrf: false,
    },
    Route {
        method: "GET",
        path: "/api/v2/addresses/{address_hash_param}/nft",
        session_csrf: false,
    },
    Route {
        method: "GET",
        path: "/api/v2/addresses/{address_hash_param}/token-transfers",
        session_csrf: false,
    },
    Route {
        method: "GET",
        path: "/api/v2/smart-contracts/{address_hash_param}/audit-reports",
        session_csrf: false,
    },
    Route {
        method: "POST",
        path: "/api/v2/smart-contracts/{address_hash_param}/audit-reports",
        session_csrf: false,
    },
    Route {
        method: "GET",
        path: "/api/v2/addresses/{address_hash_param}/tokens",
        session_csrf: false,
    },
    Route {
        method: "GET",
        path: "/api/v2/blocks/{block_hash_or_number_param}",
        session_csrf: false,
    },
    Route {
        method: "GET",
        path: "/api/v2/tokens/{address_hash_param}/holders",
        session_csrf: false,
    },
    Route {
        method: "GET",
        path: "/api/v2/blocks/{block_hash_or_number_param}/withdrawals",
        session_csrf: false,
    },
    Route {
        method: "GET",
        path: "/api/v2/addresses/{address_hash_param}/token-balances",
        session_csrf: false,
    },
    Route {
        method: "GET",
        path: "/api/v2/paxeer-x/receipts/{id}",
        session_csrf: false,
    },
    Route {
        method: "GET",
        path: "/api/v2/addresses/{address_hash_param}/token-transfers/csv",
        session_csrf: false,
    },
    Route {
        method: "GET",
        path: "/api/v2/tokens/",
        session_csrf: false,
    },
    Route {
        method: "GET",
        path: "/api/v2/tokens/{address_hash_param}/holders/csv",
        session_csrf: false,
    },
    Route {
        method: "GET",
        path: "/api/v2/main-page/transactions/watchlist",
        session_csrf: false,
    },
    Route {
        method: "GET",
        path: "/api/v2/transactions/{transaction_hash_param}",
        session_csrf: false,
    },
    Route {
        method: "GET",
        path: "/api/v2/config/csv-export",
        session_csrf: false,
    },
    Route {
        method: "GET",
        path: "/api/v2/tokens/{address_hash_param}",
        session_csrf: false,
    },
    Route {
        method: "GET",
        path: "/api/v2/paxeer-x/anchors",
        session_csrf: false,
    },
    Route {
        method: "GET",
        path: "/api/v2/blocks/{block_hash_or_number_param}/internal-transactions",
        session_csrf: false,
    },
    Route {
        method: "GET",
        path: "/api/account/v2/get_csrf",
        session_csrf: true,
    },
    Route {
        method: "GET",
        path: "/api/account/v2/user/info",
        session_csrf: true,
    },
    Route {
        method: "GET",
        path: "/api/account/v2/authenticate",
        session_csrf: true,
    },
    Route {
        method: "POST",
        path: "/api/account/v2/authenticate",
        session_csrf: true,
    },
    Route {
        method: "POST",
        path: "/api/account/v2/address/link",
        session_csrf: true,
    },
    Route {
        method: "GET",
        path: "/api/account/v2/email/resend",
        session_csrf: true,
    },
    Route {
        method: "POST",
        path: "/api/account/v2/email/link",
        session_csrf: true,
    },
    Route {
        method: "GET",
        path: "/api/account/v2/user/watchlist",
        session_csrf: true,
    },
    Route {
        method: "POST",
        path: "/api/account/v2/user/watchlist",
        session_csrf: true,
    },
    Route {
        method: "PUT",
        path: "/api/account/v2/user/watchlist/{id}",
        session_csrf: true,
    },
    Route {
        method: "DELETE",
        path: "/api/account/v2/user/watchlist/{id}",
        session_csrf: true,
    },
    Route {
        method: "GET",
        path: "/api/account/v2/user/api_keys",
        session_csrf: true,
    },
    Route {
        method: "POST",
        path: "/api/account/v2/user/api_keys",
        session_csrf: true,
    },
    Route {
        method: "PUT",
        path: "/api/account/v2/user/api_keys/{api_key}",
        session_csrf: true,
    },
    Route {
        method: "DELETE",
        path: "/api/account/v2/user/api_keys/{api_key}",
        session_csrf: true,
    },
    Route {
        method: "GET",
        path: "/api/account/v2/user/custom_abis",
        session_csrf: true,
    },
    Route {
        method: "POST",
        path: "/api/account/v2/user/custom_abis",
        session_csrf: true,
    },
    Route {
        method: "PUT",
        path: "/api/account/v2/user/custom_abis/{id}",
        session_csrf: true,
    },
    Route {
        method: "DELETE",
        path: "/api/account/v2/user/custom_abis/{id}",
        session_csrf: true,
    },
    Route {
        method: "GET",
        path: "/api/account/v2/user/tags/address",
        session_csrf: true,
    },
    Route {
        method: "POST",
        path: "/api/account/v2/user/tags/address",
        session_csrf: true,
    },
    Route {
        method: "PUT",
        path: "/api/account/v2/user/tags/address/{id}",
        session_csrf: true,
    },
    Route {
        method: "DELETE",
        path: "/api/account/v2/user/tags/address/{id}",
        session_csrf: true,
    },
    Route {
        method: "GET",
        path: "/api/account/v2/user/tags/transaction",
        session_csrf: true,
    },
    Route {
        method: "POST",
        path: "/api/account/v2/user/tags/transaction",
        session_csrf: true,
    },
    Route {
        method: "PUT",
        path: "/api/account/v2/user/tags/transaction/{id}",
        session_csrf: true,
    },
    Route {
        method: "DELETE",
        path: "/api/account/v2/user/tags/transaction/{id}",
        session_csrf: true,
    },
    Route {
        method: "GET",
        path: "/api/account/v2/user/tags/address/{id}",
        session_csrf: true,
    },
    Route {
        method: "GET",
        path: "/api/account/v2/user/tags/transaction/{id}",
        session_csrf: true,
    },
    Route {
        method: "POST",
        path: "/api/account/v2/authenticate_via_wallet",
        session_csrf: false,
    },
    Route {
        method: "GET",
        path: "/api/account/v2/authenticate_via_dynamic",
        session_csrf: false,
    },
    Route {
        method: "POST",
        path: "/api/account/v2/send_otp",
        session_csrf: false,
    },
    Route {
        method: "POST",
        path: "/api/account/v2/confirm_otp",
        session_csrf: false,
    },
    Route {
        method: "GET",
        path: "/api/account/v2/siwe_message",
        session_csrf: false,
    },
    Route {
        method: "GET",
        path: "/api/account/auth/logout",
        session_csrf: true,
    },
    Route {
        method: "POST",
        path: "/api/v2/smart-contracts/{address_hash}/verification/via/standard-input",
        session_csrf: false,
    },
    Route {
        method: "POST",
        path: "/api/v2/smart-contracts/{address_hash}/verification/via/flattened-code",
        session_csrf: false,
    },
    Route {
        method: "POST",
        path: "/api/v2/smart-contracts/{address_hash}/verification/via/sourcify",
        session_csrf: false,
    },
    Route {
        method: "POST",
        path: "/api/v2/smart-contracts/{address_hash}/verification/via/multi-part",
        session_csrf: false,
    },
    Route {
        method: "POST",
        path: "/api/v2/smart-contracts/{address_hash}/verification/via/vyper-code",
        session_csrf: false,
    },
    Route {
        method: "POST",
        path: "/api/v2/smart-contracts/{address_hash}/verification/via/vyper-multi-part",
        session_csrf: false,
    },
    Route {
        method: "POST",
        path: "/api/v2/smart-contracts/{address_hash}/verification/via/vyper-standard-input",
        session_csrf: false,
    },
    Route {
        method: "GET",
        path: "/socket/v2/websocket",
        session_csrf: false,
    },
];
