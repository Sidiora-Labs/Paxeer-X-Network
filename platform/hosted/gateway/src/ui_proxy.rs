use super::{http, response, IncomingRequest};
use serde::Deserialize;
use std::collections::BTreeSet;
use std::io::{Read, Write};
use std::os::unix::fs::MetadataExt;
use std::sync::OnceLock;
use zeroize::Zeroizing;

const HOST: &str = "api-mainnet-beta.paxeer.network";
const ORIGIN: &str = "https://api-mainnet-beta.paxeer.network";
const EXPLORER: &str = "https://explorer-frontend-production-6eef.up.railway.app";
static WALLET: OnceLock<Option<http::Endpoint>> = OnceLock::new();
static HUMAN_WEB: OnceLock<Option<http::Endpoint>> = OnceLock::new();

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct Bindings {
    schema: String,
    wallet_origin: String,
    human_web_origin: Option<String>,
}

pub(super) fn configure() -> Result<(), String> {
    let (endpoint, human_web) =
        if let Some(path) = std::env::var_os("LAYERX_GATEWAY_UI_BINDINGS_FILE") {
            let before = std::fs::symlink_metadata(&path).map_err(|_| "UI bindings unavailable")?;
            if !before.is_file()
                || before.nlink() != 1
                || before.uid() != 0
                || before.mode() & 0o077 != 0
                || before.len() > 8192
            {
                return Err("UI bindings are not protected".into());
            }
            let mut file = std::fs::File::open(&path).map_err(|_| "UI bindings unavailable")?;
            let after = file.metadata().map_err(|_| "UI bindings unavailable")?;
            if before.dev() != after.dev()
                || before.ino() != after.ino()
                || before.uid() != after.uid()
                || before.mode() != after.mode()
                || before.len() != after.len()
            {
                return Err("UI binding identity changed".into());
            }
            let mut bytes = Vec::new();
            (&mut file)
                .take(8193)
                .read_to_end(&mut bytes)
                .map_err(|_| "UI bindings unreadable")?;
            if bytes.len() > 8192 {
                return Err("UI bindings exceed bound".into());
            }
            let binding: Bindings =
                serde_json::from_slice(&bytes).map_err(|_| "UI bindings malformed")?;
            let document: serde_json::Value =
                serde_json::from_slice(&bytes).map_err(|_| "UI bindings malformed")?;
            if !matches!(
                binding.schema.as_str(),
                "paxeer-x.ui-bindings.v1" | "paxeer-x.ui-bindings.v2"
            ) || (binding.schema == "paxeer-x.ui-bindings.v1"
                && document.get("human_web_origin").is_some())
            {
                return Err("UI binding schema refused".into());
            }
            let endpoint = ui_origin(&binding.wallet_origin)?;
            let human_web = binding
                .human_web_origin
                .as_deref()
                .map(ui_origin)
                .transpose()?;
            (Some(endpoint), human_web)
        } else {
            (None, None)
        };
    WALLET
        .set(endpoint)
        .map_err(|_| "UI bindings already configured")?;
    HUMAN_WEB
        .set(human_web)
        .map_err(|_| "UI bindings already configured".into())
}

fn ui_origin(origin: &str) -> Result<http::Endpoint, String> {
    let endpoint = http::Endpoint::parse(origin)?;
    if endpoint.port != 443
        || !endpoint.base_path.is_empty()
        || endpoint.host == HOST
        || endpoint.host.len() > 253
        || !endpoint.host.contains('.')
        || endpoint.host.split('.').any(|label| {
            label.is_empty()
                || label.len() > 63
                || label.starts_with('-')
                || label.ends_with('-')
                || !label
                    .bytes()
                    .all(|b| b.is_ascii_lowercase() || b.is_ascii_digit() || b == b'-')
        })
    {
        return Err("UI origin refused".into());
    }
    Ok(endpoint)
}

pub(super) fn human_web_health() -> Result<(), &'static str> {
    let endpoint = HUMAN_WEB
        .get()
        .and_then(Option::as_ref)
        .ok_or("not_configured")?;
    let request = http::OutboundRequest {
        method: "GET",
        path: "/explorer/verify",
        idempotency: None,
        content_type: "",
        body: &[],
    };
    let answer =
        http::human_web_request(endpoint, &request, &[]).map_err(|_| "upstream_unavailable")?;
    if answer.response.status != 200
        || answer.response.content_type.split(';').next() != Some("text/html")
        || answer.response.body.is_empty()
    {
        return Err("invalid_health_response");
    }
    Ok(())
}

fn matches_path(template: &str, path: &str) -> bool {
    let mut expected = template.split('/');
    let mut actual = path.split('/');
    loop {
        match (expected.next(), actual.next()) {
            (None, None) => return true,
            (Some(want), Some(got)) if want.contains('[') => {
                let Some((prefix, tail)) = want.split_once('[') else {
                    return false;
                };
                let Some((_, suffix)) = tail.split_once(']') else {
                    return false;
                };
                let Some(value) = got
                    .strip_prefix(prefix)
                    .and_then(|v| v.strip_suffix(suffix))
                else {
                    return false;
                };
                if value.is_empty()
                    || value.len() > 256
                    || matches!(value, "." | "..")
                    || !value
                        .bytes()
                        .all(|b| b.is_ascii_alphanumeric() || b"-._~".contains(&b))
                {
                    return false;
                }
            }
            (Some(want), Some(got)) if want == got => {}
            _ => return false,
        }
    }
}

fn asset(path: &str) -> bool {
    if ASSETS.contains(&path) {
        return true;
    }
    [
        "/wallet/_next/static/",
        "/explorer/_next/static/",
        "/wallet/icons/app/",
        "/explorer/assets/configs/",
        "/explorer/assets/favicon/",
        "/explorer/assets/multichain/",
        "/explorer/assets/essential-dapps/",
        "/explorer/icons/",
    ]
    .iter()
    .any(|prefix| {
        path.strip_prefix(prefix).is_some_and(|tail| {
            !tail.is_empty()
                && tail.len() < 1024
                && tail.split('/').all(|p| {
                    !p.is_empty()
                        && !p.starts_with('.')
                        && p.bytes()
                            .all(|b| b.is_ascii_alphanumeric() || b"-._[]()".contains(&b))
                })
                && [
                    ".js", ".json", ".css", ".woff", ".woff2", ".ttf", ".otf", ".png", ".jpg",
                    ".jpeg", ".webp", ".avif", ".svg", ".ico",
                ]
                .iter()
                .any(|ext| tail.ends_with(ext))
        })
    })
}

fn data_page(path: &str) -> bool {
    let Some(tail) = path.strip_prefix("/explorer/_next/data/") else {
        return false;
    };
    let Some((build, page)) = tail.split_once('/') else {
        return false;
    };
    if build.is_empty()
        || build.len() > 128
        || !build
            .bytes()
            .all(|b| b.is_ascii_alphanumeric() || b"-_".contains(&b))
    {
        return false;
    }
    let Some(page) = page.strip_suffix(".json") else {
        return false;
    };
    let page = if page == "index" {
        "/explorer".to_owned()
    } else {
        format!("/explorer/{page}")
    };
    PAGES.iter().any(|p| matches_path(p, &page))
}

fn admitted(method: &str, path: &str) -> bool {
    API.iter()
        .any(|(m, p)| *m == method && matches_path(p, path))
        || (matches!(method, "GET" | "HEAD")
            && (PAGES.iter().any(|p| matches_path(p, path)) || asset(path) || data_page(path)))
}

fn cookie_name(name: &str) -> bool {
    matches!(
        name,
        "nav_bar_collapsed"
            | "_explorer_key"
            | "api_temp_token"
            | "rewards_api_token"
            | "rewards_ref_code"
            | "txs_sort"
            | "chakra-ui-color-mode"
            | "chakra-ui-color-theme"
            | "address_identicon_type"
            | "address_format"
            | "time_format"
            | "local_time"
            | "indexing_alert"
            | "adblock_detected"
            | "_mixpanel_debug"
            | "address_nft_display_type"
            | "hide_add_to_wallet_button"
            | "uuid"
            | "show_scam_tokens"
            | "show_poor_reputation_tokens"
            | "app_profile"
            | "table_view_on_mobile"
    )
}

fn cookie_value(value: &str) -> bool {
    value
        .bytes()
        .all(|b| b > 0x20 && b < 0x7f && !b"\";,\\".contains(&b))
}

fn cookies(raw: &str) -> Result<Zeroizing<String>, String> {
    let mut output = Zeroizing::new(String::new());
    let mut seen = BTreeSet::new();
    if raw.len() > 4096 {
        return Err("UI cookies exceed bound".into());
    }
    for part in raw.split(';') {
        let (name, value) = part.trim().split_once('=').ok_or("UI cookie malformed")?;
        if !cookie_name(name) {
            continue;
        }
        if !seen.insert(name) || !cookie_value(value) {
            return Err("UI cookie refused".into());
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

fn scoped_cookie(raw: &str) -> Result<Option<String>, String> {
    if raw.len() > 4096 || !raw.bytes().all(|b| b.is_ascii_graphic() || b == b' ') {
        return Err("UI response cookie refused".into());
    }
    let mut parts = raw.split(';');
    let first = parts.next().ok_or("UI cookie empty")?.trim();
    let (name, value) = first.split_once('=').ok_or("UI cookie malformed")?;
    if !cookie_name(name) {
        return Ok(None);
    }
    if !cookie_value(value) {
        return Err("UI cookie value refused".into());
    }
    let mut output = first.to_owned();
    let mut seen = BTreeSet::new();
    for attribute in parts {
        let attribute = attribute.trim();
        let key = attribute
            .split('=')
            .next()
            .unwrap_or_default()
            .to_ascii_lowercase();
        if !seen.insert(key.clone()) {
            return Err("UI duplicate cookie attribute".into());
        }
        match key.as_str() {
            "domain" | "path" => {}
            "secure" if !attribute.contains('=') => {}
            "httponly" if !attribute.contains('=') => {
                output.push_str("; HttpOnly");
            }
            "samesite"
                if attribute.split_once('=').is_some_and(|(_, v)| {
                    matches!(v.to_ascii_lowercase().as_str(), "lax" | "strict" | "none")
                }) =>
            {
                output.push_str("; ");
                output.push_str(attribute);
            }
            "expires" | "max-age" if attribute.contains('=') => {
                output.push_str("; ");
                output.push_str(attribute);
            }
            _ => return Err("UI cookie attribute refused".into()),
        }
    }
    output.push_str("; Path=/explorer; Secure");
    Ok(Some(output))
}

fn confined(value: &str, prefix: &str) -> bool {
    http::ui_split_target(value).is_ok_and(|(path, _)| {
        path == prefix
            || path
                .strip_prefix(prefix)
                .is_some_and(|s| s.starts_with('/'))
    })
}

fn prepare(
    request: &IncomingRequest,
) -> Result<(http::Endpoint, Vec<(String, Zeroizing<String>)>), u16> {
    let (path, query) = http::ui_split_target(&request.path).map_err(|_| 400_u16)?;
    let human_web = http::human_web_owns_target(path);
    let wallet = path == "/wallet" || path.starts_with("/wallet/");
    let prefix = if wallet {
        "/wallet"
    } else if path.starts_with("/human-ui/") {
        "/human-ui"
    } else if human_web {
        "/explorer/verify"
    } else {
        "/explorer"
    };
    if path.starts_with("/wallet/api/wallet/") && !matches!(request.method.as_str(), "GET" | "HEAD")
    {
        return Err(405);
    }
    if human_web {
        if query.is_some() && path == "/api/explorer/verify" {
            return Err(400);
        }
        if !http::human_web_admitted(&request.method, path) {
            return Err(405);
        }
    } else if !admitted(&request.method, path) {
        return Err(
            if API.iter().any(|(_, p)| matches_path(p, path))
                || PAGES.iter().any(|p| matches_path(p, path))
                || asset(path)
                || data_page(path)
            {
                405
            } else {
                404
            },
        );
    }
    if request.headers.get("host").is_none_or(|v| v != HOST)
        || request.headers.contains_key("x-layerx-principal")
        || request.headers.contains_key("x-layerx-api-key")
        || request.headers.contains_key("upgrade")
        || request.body.len()
            > if request.method == "POST" && path == "/api/explorer/verify" {
                1_100_000
            } else {
                32768
            }
        || (matches!(request.method.as_str(), "GET" | "HEAD") && !request.body.is_empty())
    {
        return Err(400);
    }
    let mutating = !matches!(request.method.as_str(), "GET" | "HEAD");
    if request.headers.get("origin").is_some_and(|v| v != ORIGIN)
        || (mutating && request.headers.get("origin").is_none_or(|v| v != ORIGIN))
        || request
            .headers
            .get("sec-fetch-site")
            .is_some_and(|v| v == "cross-site" && (mutating || path.contains("/api/")))
    {
        return Err(403);
    }
    if mutating
        && request
            .headers
            .get("content-type")
            .is_none_or(|v| v.split(';').next() != Some("application/json"))
    {
        return Err(415);
    }
    let endpoint = if human_web {
        HUMAN_WEB
            .get()
            .and_then(Option::as_ref)
            .cloned()
            .ok_or(503_u16)?
    } else if wallet {
        WALLET
            .get()
            .and_then(Option::as_ref)
            .cloned()
            .ok_or(503_u16)?
    } else {
        http::Endpoint::parse(EXPLORER).map_err(|_| 503_u16)?
    };
    let mut headers = vec![
        (
            "x-forwarded-host".to_owned(),
            Zeroizing::new(HOST.to_owned()),
        ),
        (
            "x-forwarded-proto".to_owned(),
            Zeroizing::new("https".to_owned()),
        ),
    ];
    for (name, value) in &request.headers {
        if matches!(
            name.as_str(),
            "accept"
                | "accept-language"
                | "accept-encoding"
                | "rsc"
                | "next-router-state-tree"
                | "next-router-prefetch"
                | "if-none-match"
                | "if-modified-since"
                | "user-agent"
                | "origin"
                | "x-csrf-token"
        ) {
            if value.len() > 8192 {
                return Err(400);
            }
            headers.push((name.clone(), Zeroizing::new(value.clone())));
        } else if name == "next-url" {
            if !confined(value, prefix) {
                return Err(400);
            }
            headers.push((name.clone(), Zeroizing::new(value.clone())));
        } else if name == "referer" {
            if let Some(target) = value.strip_prefix(ORIGIN) {
                if confined(target, prefix) {
                    headers.push((name.clone(), Zeroizing::new(value.clone())));
                }
            }
        } else if name == "cookie" && !wallet && !human_web {
            let value = cookies(value).map_err(|_| 400_u16)?;
            if !value.is_empty() {
                headers.push((name.clone(), value));
            }
        }
    }
    Ok((endpoint, headers))
}

fn safe_response(
    answer: &mut http::UiResponse,
    endpoint: &http::Endpoint,
    prefix: &str,
) -> Result<(), String> {
    let upstream_origin = format!("https://{}", endpoint.host);
    let mut output = Vec::new();
    for (name, value) in std::mem::take(&mut answer.response.headers) {
        if name == "location" {
            let target = value
                .strip_prefix(&upstream_origin)
                .or_else(|| value.strip_prefix(ORIGIN))
                .unwrap_or(&value);
            if !confined(target, prefix) {
                return Err("UI redirect escapes mount".into());
            }
            output.push((name, target.to_owned()));
        } else if name == "set-cookie" {
            if prefix == "/explorer" {
                if let Some(value) = scoped_cookie(&value)? {
                    output.push((name, value));
                }
            }
        } else if name == "service-worker-allowed" {
            if !confined(&value, prefix) {
                return Err("UI worker scope escapes mount".into());
            }
            output.push((name, value));
        } else {
            output.push((name, value));
        }
    }
    answer.response.headers = output;
    Ok(())
}

pub(super) fn exchange(
    request: &IncomingRequest,
    downstream: &mut impl Write,
) -> Option<Result<(), String>> {
    if !http::ui_owns_target(&request.path)
        || request
            .path
            .split('?')
            .next()
            .is_some_and(|p| p == "/explorer/backend" || p.starts_with("/explorer/backend/"))
    {
        return None;
    }
    Some(serve(request, downstream))
}

fn refusal(
    request: &IncomingRequest,
    downstream: &mut impl Write,
    status: u16,
) -> Result<(), String> {
    http::write_ui_failure(
        downstream,
        response(status, "ui_request_refused", None),
        request.method == "HEAD",
    )
}

fn serve(request: &IncomingRequest, downstream: &mut impl Write) -> Result<(), String> {
    let (endpoint, headers) = match prepare(request) {
        Ok(value) => value,
        Err(status) => return refusal(request, downstream, status),
    };
    let headers: Vec<(&str, &str)> = headers
        .iter()
        .map(|(n, v)| (n.as_str(), v.as_str()))
        .collect();
    let human_web = http::human_web_owns_target(&request.path);
    let upstream_path = if request.path.starts_with("/human-ui/_next/static/") {
        request
            .path
            .strip_prefix("/human-ui")
            .unwrap_or(&request.path)
    } else {
        &request.path
    };
    let outbound = http::OutboundRequest {
        method: &request.method,
        path: upstream_path,
        idempotency: None,
        content_type: request
            .headers
            .get("content-type")
            .map_or("", String::as_str),
        body: &request.body,
    };
    let answer = if human_web {
        http::human_web_request(&endpoint, &outbound, &headers)
    } else {
        http::ui_request(&endpoint, &outbound, &headers)
    };
    let mut answer = match answer {
        Ok(value) => value,
        Err(_) => return refusal(request, downstream, 502),
    };
    let prefix = if request.path.starts_with("/wallet") {
        "/wallet"
    } else if request.path.starts_with("/human-ui/") {
        "/human-ui"
    } else if human_web {
        request.path.split('?').next().unwrap_or(&request.path)
    } else {
        "/explorer"
    };
    if safe_response(&mut answer, &endpoint, prefix).is_err() {
        return refusal(request, downstream, 502);
    }
    http::write_ui_response(downstream, answer, request.method == "HEAD")
}

const PAGES: &[&str] = &[
    "/explorer",
    "/explorer/",
    "/explorer/404",
    "/explorer/account/api-key",
    "/explorer/account/custom-abi",
    "/explorer/account/merits",
    "/explorer/account/tag-address",
    "/explorer/account/verified-addresses",
    "/explorer/account/watchlist",
    "/explorer/accounts",
    "/explorer/accounts/label/[slug]",
    "/explorer/address/[hash]",
    "/explorer/address/[hash]/contract-verification",
    "/explorer/advanced-filter",
    "/explorer/api-docs",
    "/explorer/apps",
    "/explorer/apps/[id]",
    "/explorer/auth/profile",
    "/explorer/batches",
    "/explorer/batches/[number]",
    "/explorer/batches/celestia/[height]/[commitment]",
    "/explorer/blobs/[hash]",
    "/explorer/block/[height_or_hash]",
    "/explorer/block/countdown",
    "/explorer/block/countdown/[height]",
    "/explorer/blocks",
    "/explorer/cc/tx/[hash]",
    "/explorer/chain/[chain_slug]/accounts/label/[slug]",
    "/explorer/chain/[chain_slug]/advanced-filter",
    "/explorer/chain/[chain_slug]/block/[height_or_hash]",
    "/explorer/chain/[chain_slug]/block/countdown",
    "/explorer/chain/[chain_slug]/block/countdown/[height]",
    "/explorer/chain/[chain_slug]/csv-export",
    "/explorer/chain/[chain_slug]/op/[hash]",
    "/explorer/chain/[chain_slug]/token/[hash]",
    "/explorer/chain/[chain_slug]/token/[hash]/instance/[id]",
    "/explorer/chain/[chain_slug]/tx/[hash]",
    "/explorer/chain/[chain_slug]/visualize/sol2uml",
    "/explorer/chakra",
    "/explorer/contract-verification",
    "/explorer/cross-chain-tx/[id]",
    "/explorer/csv-export",
    "/explorer/deposits",
    "/explorer/dispute-games",
    "/explorer/ecosystems",
    "/explorer/epochs",
    "/explorer/epochs/[number]",
    "/explorer/essential-dapps/[id]",
    "/explorer/gas-tracker",
    "/explorer/hot-contracts",
    "/explorer/internal-txs",
    "/explorer/interop-messages",
    "/explorer/login",
    "/explorer/mud-worlds",
    "/explorer/name-services",
    "/explorer/name-services/clusters/[name]",
    "/explorer/name-services/domains/[name]",
    "/explorer/op/[hash]",
    "/explorer/operation/[id]",
    "/explorer/operations",
    "/explorer/ops",
    "/explorer/output-roots",
    "/explorer/paxeer-x/account/[hash]",
    "/explorer/paxeer-x/anchors",
    "/explorer/paxeer-x/receipts",
    "/explorer/paxeer-x/receipts/[id]",
    "/explorer/pools",
    "/explorer/pools/[hash]",
    "/explorer/public-tags/submit",
    "/explorer/search-results",
    "/explorer/sprite",
    "/explorer/stats",
    "/explorer/stats/[id]",
    "/explorer/token-transfers",
    "/explorer/token/[hash]",
    "/explorer/token/[hash]/instance/[id]",
    "/explorer/tokens",
    "/explorer/tx/[hash]",
    "/explorer/txn-withdrawals",
    "/explorer/txs",
    "/explorer/txs/kettle/[hash]",
    "/explorer/uptime",
    "/explorer/validators",
    "/explorer/validators/[id]",
    "/explorer/verified-contracts",
    "/explorer/visualize/sol2uml",
    "/explorer/withdrawals",
    "/wallet",
    "/wallet/",
    "/wallet/account/",
    "/wallet/account/deposit/",
    "/wallet/account/history/",
    "/wallet/account/plan/",
    "/wallet/auth/callback/",
    "/wallet/offline/",
    "/wallet/privacy/",
    "/wallet/surfaces/bridge/",
    "/wallet/surfaces/exchange/",
    "/wallet/surfaces/fees/",
    "/wallet/surfaces/launchpad/",
    "/wallet/surfaces/web-data/",
];

const ASSETS: &[&str] = &[
    "/explorer/assets/envs.js",
    "/explorer/assets/favicon/android-chrome-192x192.png",
    "/explorer/assets/favicon/apple-touch-icon-180x180.png",
    "/explorer/assets/favicon/favicon-16x16.png",
    "/explorer/assets/favicon/favicon-32x32.png",
    "/explorer/assets/favicon/favicon-48x48.png",
    "/explorer/assets/favicon/favicon.ico",
    "/explorer/assets/favicon/logo-icon.png",
    "/explorer/nft-html-embed.html",
    "/explorer/robots.txt",
    "/explorer/sitemap.xml",
    "/explorer/static/4x4-easter-game-cut.png",
    "/explorer/static/apple_calendar.svg",
    "/explorer/static/capybara/capybaraSprite.png",
    "/explorer/static/capybara/capybaraSpriteX2.png",
    "/explorer/static/capybara/index.js",
    "/explorer/static/contract_star.png",
    "/explorer/static/ethereum-icon.svg",
    "/explorer/static/fonts/Inter-fallback.woff2",
    "/explorer/static/gecko_terminal.png",
    "/explorer/static/google_calendar.svg",
    "/explorer/static/identicon_logos/blockies.png",
    "/explorer/static/identicon_logos/github.png",
    "/explorer/static/identicon_logos/gradient_avatar.png",
    "/explorer/static/identicon_logos/jazzicon.png",
    "/explorer/static/identicon_logos/nouns.svg",
    "/explorer/static/labels/stage-1.svg",
    "/explorer/static/labels/stage-2.svg",
    "/explorer/static/labels/testnet.svg",
    "/explorer/static/marketplace/multisend-dark.png",
    "/explorer/static/marketplace/multisend.png",
    "/explorer/static/marketplace/revoke-dark.png",
    "/explorer/static/marketplace/revoke.png",
    "/explorer/static/marketplace/swap-dark.png",
    "/explorer/static/marketplace/swap.png",
    "/explorer/static/merits/activity_pass.svg",
    "/explorer/static/merits/badges.svg",
    "/explorer/static/merits/campaigns.svg",
    "/explorer/static/merits/cells.svg",
    "/explorer/static/merits/cells_dark.svg",
    "/explorer/static/merits/merits_program.png",
    "/explorer/static/merits/offers.svg",
    "/explorer/static/merits/streak_180.png",
    "/explorer/static/merits/streak_180_ghost.png",
    "/explorer/static/merits/streak_30.png",
    "/explorer/static/merits/streak_30_ghost.png",
    "/explorer/static/merits/streak_90.png",
    "/explorer/static/merits/streak_90_ghost.png",
    "/explorer/static/noves-logo-dark.svg",
    "/explorer/static/noves-logo.svg",
    "/explorer/static/og_placeholder.png",
    "/explorer/static/paxeer-x/icon-dark.svg",
    "/explorer/static/paxeer-x/icon.svg",
    "/explorer/static/paxeer-x/logo-dark.svg",
    "/explorer/static/paxeer-x/logo.svg",
    "/explorer/static/resizer.png",
    "/explorer/static/resizer_dark.png",
    "/explorer/static/resizer_light.png",
    "/wallet/1c56896f-202b-4da0-a145-5e469abf0f85.png",
    "/wallet/6751fc7c-8b71-48f5-b454-c34299955eb3.png",
    "/wallet/Kindle-Launch-logo-dark.webp",
    "/wallet/PAXPORT.png",
    "/wallet/PAXPORT.svg",
    "/wallet/Paxeer_Sans_Rounded/PPPangramSansRounded-Bold.otf",
    "/wallet/Paxeer_Sans_Rounded/PPPangramSansRounded-CompactExtralightReclined.otf",
    "/wallet/Paxeer_Sans_Rounded/PPPangramSansRounded-CompactRegular.otf",
    "/wallet/Paxeer_Sans_Rounded/PPPangramSansRounded-CompactThinItalic.otf",
    "/wallet/Paxeer_Sans_Rounded/PPPangramSansRounded-CompressedExtrabold.otf",
    "/wallet/Paxeer_Sans_Rounded/PPPangramSansRounded-CompressedMediumReclined.otf",
    "/wallet/Paxeer_Sans_Rounded/PPPangramSansRounded-CondensedThin.otf",
    "/wallet/Paxeer_Sans_Rounded/PPPangramSansRounded-ExtraboldItalic.otf",
    "/wallet/Paxeer_Sans_Rounded/PPPangramSansRounded-Light.otf",
    "/wallet/Paxeer_Sans_Rounded/PPPangramSansRounded-Medium.otf",
    "/wallet/Paxeer_Sans_Rounded/PPPangramSansRounded-NarrowExtraboldItalic.otf",
    "/wallet/Paxeer_Sans_Rounded/PPPangramSansRounded-NarrowExtralightReclined.otf",
    "/wallet/Paxeer_Sans_Rounded/PPPangramSansRounded-NarrowSemibold.otf",
    "/wallet/Paxeer_Sans_Rounded/PPPangramSansRounded-NarrowSemiboldItalic.otf",
    "/wallet/Paxeer_Sans_Rounded/PPPangramSansRounded-Semibold.otf",
    "/wallet/Paxeer_Sans_Rounded/PPPangramSansRounded-SlimExtralight.otf",
    "/wallet/Paxeer_Sans_Rounded/PPPangramSansRounded-SlimMediumReclined.otf",
    "/wallet/Paxeer_Sans_Rounded/PPPangramSansRounded-SlimSemiboldItalic.otf",
    "/wallet/Paxeer_Sans_Rounded/PPPangramSansRounded-Thin.otf",
    "/wallet/Untitled%20design.svg",
    "/wallet/a84ac5ac-5ebe-4f96-a9d1-4b3b664db1a5.png",
    "/wallet/argus.svg",
    "/wallet/art.png",
    "/wallet/art2.png",
    "/wallet/art3.png",
    "/wallet/avatar-1.svg",
    "/wallet/avatar-2.svg",
    "/wallet/avatar-3.svg",
    "/wallet/avatar-4.svg",
    "/wallet/avatar-5.svg",
    "/wallet/avatar-6.svg",
    "/wallet/avatar-7.svg",
    "/wallet/avatar-8.svg",
    "/wallet/c682bdc3-3f1e-4437-b571-862e7215868e.svg",
    "/wallet/chain-flow-full-logo-dark.svg",
    "/wallet/chain-flow-full-logo-light.svg",
    "/wallet/chain-flow-v2-logo-dark.svg",
    "/wallet/colosseum.svg",
    "/wallet/dao.svg",
    "/wallet/default_icon.webp",
    "/wallet/house-chimney-user%20(1).svg",
    "/wallet/house-chimney-user.svg",
    "/wallet/hyperpaxeer.svg",
    "/wallet/icons/android/launchericon-144x144.png",
    "/wallet/icons/android/launchericon-192x192.png",
    "/wallet/icons/android/launchericon-48x48.png",
    "/wallet/icons/android/launchericon-512x512.png",
    "/wallet/icons/android/launchericon-72x72.png",
    "/wallet/icons/android/launchericon-96x96.png",
    "/wallet/icons/app/apple-touch-icon-180.png",
    "/wallet/icons/app/favicon-32.png",
    "/wallet/icons/app/icon-192.png",
    "/wallet/icons/app/icon-512.png",
    "/wallet/icons/app/maskable-192.png",
    "/wallet/icons/app/maskable-512.png",
    "/wallet/icons/fiat-ramp/btc.svg",
    "/wallet/icons/fiat-ramp/buttons-popular-payment-systems-masetcard-visa-apple-pay-google-website-rectangular-rounded-edges-vector-220153006-removebg-preview.png",
    "/wallet/icons/fiat-ramp/cro.svg",
    "/wallet/icons/fiat-ramp/eth.svg",
    "/wallet/icons/fiat-ramp/klarna.png",
    "/wallet/icons/fiat-ramp/payfast.png",
    "/wallet/icons/fiat-ramp/paypal.png",
    "/wallet/icons/fiat-ramp/skrill.png",
    "/wallet/icons/fiat-ramp/sol.svg",
    "/wallet/icons/fiat-ramp/stripe.png",
    "/wallet/icons/fiat-ramp/tron.svg",
    "/wallet/icons/fiat-ramp/uni.svg",
    "/wallet/icons/fiat-ramp/xrp.svg",
    "/wallet/icons/ios/100.png",
    "/wallet/icons/ios/1024.png",
    "/wallet/icons/ios/114.png",
    "/wallet/icons/ios/120.png",
    "/wallet/icons/ios/128.png",
    "/wallet/icons/ios/144.png",
    "/wallet/icons/ios/152.png",
    "/wallet/icons/ios/16.png",
    "/wallet/icons/ios/167.png",
    "/wallet/icons/ios/180.png",
    "/wallet/icons/ios/192.png",
    "/wallet/icons/ios/20.png",
    "/wallet/icons/ios/256.png",
    "/wallet/icons/ios/29.png",
    "/wallet/icons/ios/32.png",
    "/wallet/icons/ios/40.png",
    "/wallet/icons/ios/50.png",
    "/wallet/icons/ios/512.png",
    "/wallet/icons/ios/57.png",
    "/wallet/icons/ios/58.png",
    "/wallet/icons/ios/60.png",
    "/wallet/icons/ios/64.png",
    "/wallet/icons/ios/72.png",
    "/wallet/icons/ios/76.png",
    "/wallet/icons/ios/80.png",
    "/wallet/icons/ios/87.png",
    "/wallet/icons/wallet/email.svg",
    "/wallet/icons/wallet/github.svg",
    "/wallet/icons/wallet/google.svg",
    "/wallet/icons/wallet/x.svg",
    "/wallet/icons/windows/LargeTile.scale-100.png",
    "/wallet/icons/windows/LargeTile.scale-125.png",
    "/wallet/icons/windows/LargeTile.scale-150.png",
    "/wallet/icons/windows/LargeTile.scale-200.png",
    "/wallet/icons/windows/LargeTile.scale-400.png",
    "/wallet/icons/windows/SmallTile.scale-100.png",
    "/wallet/icons/windows/SmallTile.scale-125.png",
    "/wallet/icons/windows/SmallTile.scale-150.png",
    "/wallet/icons/windows/SmallTile.scale-200.png",
    "/wallet/icons/windows/SmallTile.scale-400.png",
    "/wallet/icons/windows/SplashScreen.scale-100.png",
    "/wallet/icons/windows/SplashScreen.scale-125.png",
    "/wallet/icons/windows/SplashScreen.scale-150.png",
    "/wallet/icons/windows/SplashScreen.scale-200.png",
    "/wallet/icons/windows/SplashScreen.scale-400.png",
    "/wallet/icons/windows/Square150x150Logo.scale-100.png",
    "/wallet/icons/windows/Square150x150Logo.scale-125.png",
    "/wallet/icons/windows/Square150x150Logo.scale-150.png",
    "/wallet/icons/windows/Square150x150Logo.scale-200.png",
    "/wallet/icons/windows/Square150x150Logo.scale-400.png",
    "/wallet/icons/windows/Square44x44Logo.altform-lightunplated_targetsize-16.png",
    "/wallet/icons/windows/Square44x44Logo.altform-lightunplated_targetsize-20.png",
    "/wallet/icons/windows/Square44x44Logo.altform-lightunplated_targetsize-24.png",
    "/wallet/icons/windows/Square44x44Logo.altform-lightunplated_targetsize-256.png",
    "/wallet/icons/windows/Square44x44Logo.altform-lightunplated_targetsize-30.png",
    "/wallet/icons/windows/Square44x44Logo.altform-lightunplated_targetsize-32.png",
    "/wallet/icons/windows/Square44x44Logo.altform-lightunplated_targetsize-36.png",
    "/wallet/icons/windows/Square44x44Logo.altform-lightunplated_targetsize-40.png",
    "/wallet/icons/windows/Square44x44Logo.altform-lightunplated_targetsize-44.png",
    "/wallet/icons/windows/Square44x44Logo.altform-lightunplated_targetsize-48.png",
    "/wallet/icons/windows/Square44x44Logo.altform-lightunplated_targetsize-60.png",
    "/wallet/icons/windows/Square44x44Logo.altform-lightunplated_targetsize-64.png",
    "/wallet/icons/windows/Square44x44Logo.altform-lightunplated_targetsize-72.png",
    "/wallet/icons/windows/Square44x44Logo.altform-lightunplated_targetsize-80.png",
    "/wallet/icons/windows/Square44x44Logo.altform-lightunplated_targetsize-96.png",
    "/wallet/icons/windows/Square44x44Logo.altform-unplated_targetsize-16.png",
    "/wallet/icons/windows/Square44x44Logo.altform-unplated_targetsize-20.png",
    "/wallet/icons/windows/Square44x44Logo.altform-unplated_targetsize-24.png",
    "/wallet/icons/windows/Square44x44Logo.altform-unplated_targetsize-256.png",
    "/wallet/icons/windows/Square44x44Logo.altform-unplated_targetsize-30.png",
    "/wallet/icons/windows/Square44x44Logo.altform-unplated_targetsize-32.png",
    "/wallet/icons/windows/Square44x44Logo.altform-unplated_targetsize-36.png",
    "/wallet/icons/windows/Square44x44Logo.altform-unplated_targetsize-40.png",
    "/wallet/icons/windows/Square44x44Logo.altform-unplated_targetsize-44.png",
    "/wallet/icons/windows/Square44x44Logo.altform-unplated_targetsize-48.png",
    "/wallet/icons/windows/Square44x44Logo.altform-unplated_targetsize-60.png",
    "/wallet/icons/windows/Square44x44Logo.altform-unplated_targetsize-64.png",
    "/wallet/icons/windows/Square44x44Logo.altform-unplated_targetsize-72.png",
    "/wallet/icons/windows/Square44x44Logo.altform-unplated_targetsize-80.png",
    "/wallet/icons/windows/Square44x44Logo.altform-unplated_targetsize-96.png",
    "/wallet/icons/windows/Square44x44Logo.scale-100.png",
    "/wallet/icons/windows/Square44x44Logo.scale-125.png",
    "/wallet/icons/windows/Square44x44Logo.scale-150.png",
    "/wallet/icons/windows/Square44x44Logo.scale-200.png",
    "/wallet/icons/windows/Square44x44Logo.scale-400.png",
    "/wallet/icons/windows/Square44x44Logo.targetsize-16.png",
    "/wallet/icons/windows/Square44x44Logo.targetsize-20.png",
    "/wallet/icons/windows/Square44x44Logo.targetsize-24.png",
    "/wallet/icons/windows/Square44x44Logo.targetsize-256.png",
    "/wallet/icons/windows/Square44x44Logo.targetsize-30.png",
    "/wallet/icons/windows/Square44x44Logo.targetsize-32.png",
    "/wallet/icons/windows/Square44x44Logo.targetsize-36.png",
    "/wallet/icons/windows/Square44x44Logo.targetsize-40.png",
    "/wallet/icons/windows/Square44x44Logo.targetsize-44.png",
    "/wallet/icons/windows/Square44x44Logo.targetsize-48.png",
    "/wallet/icons/windows/Square44x44Logo.targetsize-60.png",
    "/wallet/icons/windows/Square44x44Logo.targetsize-64.png",
    "/wallet/icons/windows/Square44x44Logo.targetsize-72.png",
    "/wallet/icons/windows/Square44x44Logo.targetsize-80.png",
    "/wallet/icons/windows/Square44x44Logo.targetsize-96.png",
    "/wallet/icons/windows/StoreLogo.scale-100.png",
    "/wallet/icons/windows/StoreLogo.scale-125.png",
    "/wallet/icons/windows/StoreLogo.scale-150.png",
    "/wallet/icons/windows/StoreLogo.scale-200.png",
    "/wallet/icons/windows/StoreLogo.scale-400.png",
    "/wallet/icons/windows/Wide310x150Logo.scale-100.png",
    "/wallet/icons/windows/Wide310x150Logo.scale-125.png",
    "/wallet/icons/windows/Wide310x150Logo.scale-150.png",
    "/wallet/icons/windows/Wide310x150Logo.scale-200.png",
    "/wallet/icons/windows/Wide310x150Logo.scale-400.png",
    "/wallet/manifest.json",
    "/wallet/paxeerCOEX.svg",
    "/wallet/paxeerCOEX_light.svg",
    "/wallet/paxport_wallet.png",
    "/wallet/paxscan.svg",
    "/wallet/picsvg_download.svg",
    "/wallet/pns.svg",
    "/wallet/points.png",
    "/wallet/points.svg",
    "/wallet/pwa-192x192.svg",
    "/wallet/sid-fun-icon.svg",
    "/wallet/sid-fun-icon.webp",
    "/wallet/sidiora.webp",
    "/wallet/sidiora_perps_icon.png",
    "/wallet/sidiora_swap_icon.svg",
    "/wallet/splash_logo.png",
    "/wallet/splash_screens/10.2__iPad_landscape.png",
    "/wallet/splash_screens/10.2__iPad_portrait.png",
    "/wallet/splash_screens/10.5__iPad_Air_landscape.png",
    "/wallet/splash_screens/10.5__iPad_Air_portrait.png",
    "/wallet/splash_screens/10.9__iPad_Air_landscape.png",
    "/wallet/splash_screens/10.9__iPad_Air_portrait.png",
    "/wallet/splash_screens/11__iPad_Pro_M4_landscape.png",
    "/wallet/splash_screens/11__iPad_Pro_M4_portrait.png",
    "/wallet/splash_screens/11__iPad_Pro__10.5__iPad_Pro_landscape.png",
    "/wallet/splash_screens/11__iPad_Pro__10.5__iPad_Pro_portrait.png",
    "/wallet/splash_screens/12.9__iPad_Pro_landscape.png",
    "/wallet/splash_screens/12.9__iPad_Pro_portrait.png",
    "/wallet/splash_screens/13__iPad_Pro_M4_landscape.png",
    "/wallet/splash_screens/13__iPad_Pro_M4_portrait.png",
    "/wallet/splash_screens/4__iPhone_SE__iPod_touch_5th_generation_and_later_landscape.png",
    "/wallet/splash_screens/4__iPhone_SE__iPod_touch_5th_generation_and_later_portrait.png",
    "/wallet/splash_screens/8.3__iPad_Mini_landscape.png",
    "/wallet/splash_screens/8.3__iPad_Mini_portrait.png",
    "/wallet/splash_screens/9.7__iPad_Pro__7.9__iPad_mini__9.7__iPad_Air__9.7__iPad_landscape.png",
    "/wallet/splash_screens/9.7__iPad_Pro__7.9__iPad_mini__9.7__iPad_Air__9.7__iPad_portrait.png",
    "/wallet/splash_screens/iPhone_11_Pro_Max__iPhone_XS_Max_landscape.png",
    "/wallet/splash_screens/iPhone_11_Pro_Max__iPhone_XS_Max_portrait.png",
    "/wallet/splash_screens/iPhone_11__iPhone_XR_landscape.png",
    "/wallet/splash_screens/iPhone_11__iPhone_XR_portrait.png",
    "/wallet/splash_screens/iPhone_13_mini__iPhone_12_mini__iPhone_11_Pro__iPhone_XS__iPhone_X_landscape.png",
    "/wallet/splash_screens/iPhone_13_mini__iPhone_12_mini__iPhone_11_Pro__iPhone_XS__iPhone_X_portrait.png",
    "/wallet/splash_screens/iPhone_14_Plus__iPhone_13_Pro_Max__iPhone_12_Pro_Max_landscape.png",
    "/wallet/splash_screens/iPhone_14_Plus__iPhone_13_Pro_Max__iPhone_12_Pro_Max_portrait.png",
    "/wallet/splash_screens/iPhone_16_Plus__iPhone_15_Pro_Max__iPhone_15_Plus__iPhone_14_Pro_Max_landscape.png",
    "/wallet/splash_screens/iPhone_16_Plus__iPhone_15_Pro_Max__iPhone_15_Plus__iPhone_14_Pro_Max_portrait.png",
    "/wallet/splash_screens/iPhone_16__iPhone_15_Pro__iPhone_15__iPhone_14_Pro_landscape.png",
    "/wallet/splash_screens/iPhone_16__iPhone_15_Pro__iPhone_15__iPhone_14_Pro_portrait.png",
    "/wallet/splash_screens/iPhone_17_Pro_Max__iPhone_16_Pro_Max_landscape.png",
    "/wallet/splash_screens/iPhone_17_Pro_Max__iPhone_16_Pro_Max_portrait.png",
    "/wallet/splash_screens/iPhone_17_Pro__iPhone_17__iPhone_16_Pro_landscape.png",
    "/wallet/splash_screens/iPhone_17_Pro__iPhone_17__iPhone_16_Pro_portrait.png",
    "/wallet/splash_screens/iPhone_17e__iPhone_16e__iPhone_14__iPhone_13_Pro__iPhone_13__iPhone_12_Pro__iPhone_12_landscape.png",
    "/wallet/splash_screens/iPhone_17e__iPhone_16e__iPhone_14__iPhone_13_Pro__iPhone_13__iPhone_12_Pro__iPhone_12_portrait.png",
    "/wallet/splash_screens/iPhone_8_Plus__iPhone_7_Plus__iPhone_6s_Plus__iPhone_6_Plus_landscape.png",
    "/wallet/splash_screens/iPhone_8_Plus__iPhone_7_Plus__iPhone_6s_Plus__iPhone_6_Plus_portrait.png",
    "/wallet/splash_screens/iPhone_8__iPhone_7__iPhone_6s__iPhone_6__4.7__iPhone_SE_landscape.png",
    "/wallet/splash_screens/iPhone_8__iPhone_7__iPhone_6s__iPhone_6__4.7__iPhone_SE_portrait.png",
    "/wallet/splash_screens/iPhone_Air_landscape.png",
    "/wallet/splash_screens/iPhone_Air_portrait.png",
    "/wallet/splash_screens/icon.png",
    "/wallet/sw.js",
    "/wallet/ui_icons/ABI.svg",
    "/wallet/ui_icons/API.svg",
    "/wallet/ui_icons/ENS.svg",
    "/wallet/ui_icons/MUD.svg",
    "/wallet/ui_icons/RPC.svg",
    "/wallet/ui_icons/advanced-filter.svg",
    "/wallet/ui_icons/apps.svg",
    "/wallet/ui_icons/apps_list.svg",
    "/wallet/ui_icons/arrows/down-right.svg",
    "/wallet/ui_icons/arrows/east-mini.svg",
    "/wallet/ui_icons/arrows/east.svg",
    "/wallet/ui_icons/arrows/up-down.svg",
    "/wallet/ui_icons/arrows/up-head.svg",
    "/wallet/ui_icons/beta.svg",
    "/wallet/ui_icons/beta_xs.svg",
    "/wallet/ui_icons/blob.svg",
    "/wallet/ui_icons/blobs/image.svg",
    "/wallet/ui_icons/blobs/raw.svg",
    "/wallet/ui_icons/blobs/text.svg",
    "/wallet/ui_icons/block.svg",
    "/wallet/ui_icons/block_countdown.svg",
    "/wallet/ui_icons/brands/autoscout.svg",
    "/wallet/ui_icons/brands/blockscout.svg",
    "/wallet/ui_icons/brands/celenium.svg",
    "/wallet/ui_icons/brands/graph.svg",
    "/wallet/ui_icons/brands/pro_api.svg",
    "/wallet/ui_icons/brands/safe.svg",
    "/wallet/ui_icons/brands/solidity_scan.svg",
    "/wallet/ui_icons/brands/tac.svg",
    "/wallet/ui_icons/brands/ton.svg",
    "/wallet/ui_icons/bridge.svg",
    "/wallet/ui_icons/burger.svg",
    "/wallet/ui_icons/certified.svg",
    "/wallet/ui_icons/check.svg",
    "/wallet/ui_icons/checkered_flag.svg",
    "/wallet/ui_icons/clock-light.svg",
    "/wallet/ui_icons/clock.svg",
    "/wallet/ui_icons/close.svg",
    "/wallet/ui_icons/clusters.svg",
    "/wallet/ui_icons/coins/bitcoin.svg",
    "/wallet/ui_icons/collection.svg",
    "/wallet/ui_icons/columns.svg",
    "/wallet/ui_icons/contracts/proxy.svg",
    "/wallet/ui_icons/contracts/regular.svg",
    "/wallet/ui_icons/contracts/regular_many.svg",
    "/wallet/ui_icons/contracts/verified.svg",
    "/wallet/ui_icons/contracts/verified_many.svg",
    "/wallet/ui_icons/copy.svg",
    "/wallet/ui_icons/copy_check.svg",
    "/wallet/ui_icons/cross.svg",
    "/wallet/ui_icons/delete.svg",
    "/wallet/ui_icons/docs.svg",
    "/wallet/ui_icons/dots.svg",
    "/wallet/ui_icons/edit.svg",
    "/wallet/ui_icons/email.svg",
    "/wallet/ui_icons/error-pages/403.svg",
    "/wallet/ui_icons/error-pages/404.svg",
    "/wallet/ui_icons/error-pages/422.svg",
    "/wallet/ui_icons/error-pages/429.svg",
    "/wallet/ui_icons/error-pages/500.svg",
    "/wallet/ui_icons/explorer.svg",
    "/wallet/ui_icons/files/csv.svg",
    "/wallet/ui_icons/files/image.svg",
    "/wallet/ui_icons/files/json.svg",
    "/wallet/ui_icons/files/placeholder.svg",
    "/wallet/ui_icons/files/sol.svg",
    "/wallet/ui_icons/files/yul.svg",
    "/wallet/ui_icons/filter.svg",
    "/wallet/ui_icons/flame.svg",
    "/wallet/ui_icons/flashblock.svg",
    "/wallet/ui_icons/gas.svg",
    "/wallet/ui_icons/gas_xl.svg",
    "/wallet/ui_icons/gear.svg",
    "/wallet/ui_icons/globe.svg",
    "/wallet/ui_icons/heart_filled.svg",
    "/wallet/ui_icons/heart_outline.svg",
    "/wallet/ui_icons/hexagon.svg",
    "/wallet/ui_icons/hourglass.svg",
    "/wallet/ui_icons/info.svg",
    "/wallet/ui_icons/info_filled.svg",
    "/wallet/ui_icons/integration/full.svg",
    "/wallet/ui_icons/integration/partial.svg",
    "/wallet/ui_icons/interop.svg",
    "/wallet/ui_icons/key.svg",
    "/wallet/ui_icons/lightning.svg",
    "/wallet/ui_icons/lightning_navbar.svg",
    "/wallet/ui_icons/link.svg",
    "/wallet/ui_icons/link_external.svg",
    "/wallet/ui_icons/list_view.svg",
    "/wallet/ui_icons/lock.svg",
    "/wallet/ui_icons/merits.svg",
    "/wallet/ui_icons/merits_colored.svg",
    "/wallet/ui_icons/merits_with_dot.svg",
    "/wallet/ui_icons/minus.svg",
    "/wallet/ui_icons/monaco/cargo.svg",
    "/wallet/ui_icons/monaco/file.svg",
    "/wallet/ui_icons/monaco/folder-open.svg",
    "/wallet/ui_icons/monaco/folder.svg",
    "/wallet/ui_icons/monaco/rust.svg",
    "/wallet/ui_icons/monaco/solidity.svg",
    "/wallet/ui_icons/monaco/toml.svg",
    "/wallet/ui_icons/monaco/vyper.svg",
    "/wallet/ui_icons/moon-with-star.svg",
    "/wallet/ui_icons/moon.svg",
    "/wallet/ui_icons/multisend.svg",
    "/wallet/ui_icons/navigation/api_docs.svg",
    "/wallet/ui_icons/navigation/api_keys.svg",
    "/wallet/ui_icons/navigation/apps.svg",
    "/wallet/ui_icons/navigation/block.svg",
    "/wallet/ui_icons/navigation/blockchain.svg",
    "/wallet/ui_icons/navigation/chain_stats.svg",
    "/wallet/ui_icons/navigation/cross_chain_txs.svg",
    "/wallet/ui_icons/navigation/custom_abi.svg",
    "/wallet/ui_icons/navigation/deposits.svg",
    "/wallet/ui_icons/navigation/dex_tracker.svg",
    "/wallet/ui_icons/navigation/ecosystems.svg",
    "/wallet/ui_icons/navigation/games.svg",
    "/wallet/ui_icons/navigation/gas_tracker.svg",
    "/wallet/ui_icons/navigation/hot_contracts.svg",
    "/wallet/ui_icons/navigation/hourglass.svg",
    "/wallet/ui_icons/navigation/internal_txns.svg",
    "/wallet/ui_icons/navigation/merits.svg",
    "/wallet/ui_icons/navigation/merits_with_dot.svg",
    "/wallet/ui_icons/navigation/mud.svg",
    "/wallet/ui_icons/navigation/name_services.svg",
    "/wallet/ui_icons/navigation/operation.svg",
    "/wallet/ui_icons/navigation/other.svg",
    "/wallet/ui_icons/navigation/output_roots.svg",
    "/wallet/ui_icons/navigation/private_tags.svg",
    "/wallet/ui_icons/navigation/public_tags.svg",
    "/wallet/ui_icons/navigation/stats.svg",
    "/wallet/ui_icons/navigation/token_transfers.svg",
    "/wallet/ui_icons/navigation/tokens.svg",
    "/wallet/ui_icons/navigation/top_accounts.svg",
    "/wallet/ui_icons/navigation/transactions.svg",
    "/wallet/ui_icons/navigation/txn_batches.svg",
    "/wallet/ui_icons/navigation/uptime.svg",
    "/wallet/ui_icons/navigation/user_op.svg",
    "/wallet/ui_icons/navigation/validator.svg",
    "/wallet/ui_icons/navigation/verified_contracts.svg",
    "/wallet/ui_icons/navigation/watchlist.svg",
    "/wallet/ui_icons/navigation/withdrawals.svg",
    "/wallet/ui_icons/networks.svg",
    "/wallet/ui_icons/networks/icon-placeholder.svg",
    "/wallet/ui_icons/networks/logo-placeholder.svg",
    "/wallet/ui_icons/nft_shield.svg",
    "/wallet/ui_icons/open-link.svg",
    "/wallet/ui_icons/operation.svg",
    "/wallet/ui_icons/payment_link.svg",
    "/wallet/ui_icons/pie_chart.svg",
    "/wallet/ui_icons/plus.svg",
    "/wallet/ui_icons/private_tags.svg",
    "/wallet/ui_icons/profile.svg",
    "/wallet/ui_icons/publictags.svg",
    "/wallet/ui_icons/qr_code.svg",
    "/wallet/ui_icons/refresh.svg",
    "/wallet/ui_icons/repeat.svg",
    "/wallet/ui_icons/return.svg",
    "/wallet/ui_icons/revoke.svg",
    "/wallet/ui_icons/rocket.svg",
    "/wallet/ui_icons/rocket_xl.svg",
    "/wallet/ui_icons/scam.svg",
    "/wallet/ui_icons/scope.svg",
    "/wallet/ui_icons/score/score-not-ok.svg",
    "/wallet/ui_icons/score/score-ok.svg",
    "/wallet/ui_icons/search.svg",
    "/wallet/ui_icons/share.svg",
    "/wallet/ui_icons/sign_out.svg",
    "/wallet/ui_icons/social/coingecko.svg",
    "/wallet/ui_icons/social/coinmarketcap.svg",
    "/wallet/ui_icons/social/defi_llama.svg",
    "/wallet/ui_icons/social/discord.svg",
    "/wallet/ui_icons/social/discord_filled.svg",
    "/wallet/ui_icons/social/facebook_filled.svg",
    "/wallet/ui_icons/social/git.svg",
    "/wallet/ui_icons/social/github_filled.svg",
    "/wallet/ui_icons/social/linkedin_filled.svg",
    "/wallet/ui_icons/social/medium_filled.svg",
    "/wallet/ui_icons/social/opensea_filled.svg",
    "/wallet/ui_icons/social/reddit_filled.svg",
    "/wallet/ui_icons/social/slack_filled.svg",
    "/wallet/ui_icons/social/stats.svg",
    "/wallet/ui_icons/social/telega.svg",
    "/wallet/ui_icons/social/telegram_filled.svg",
    "/wallet/ui_icons/social/twitter.svg",
    "/wallet/ui_icons/social/twitter_filled.svg",
    "/wallet/ui_icons/star_filled.svg",
    "/wallet/ui_icons/star_outline.svg",
    "/wallet/ui_icons/status/error.svg",
    "/wallet/ui_icons/status/pending.svg",
    "/wallet/ui_icons/status/success.svg",
    "/wallet/ui_icons/status/warning.svg",
    "/wallet/ui_icons/sun.svg",
    "/wallet/ui_icons/swap.svg",
    "/wallet/ui_icons/token-placeholder.svg",
    "/wallet/ui_icons/tokens.svg",
    "/wallet/ui_icons/tokens/xdai.svg",
    "/wallet/ui_icons/transactions.svg",
    "/wallet/ui_icons/txn_batches.svg",
    "/wallet/ui_icons/uniswap.svg",
    "/wallet/ui_icons/user_op.svg",
    "/wallet/ui_icons/verification-steps/error.svg",
    "/wallet/ui_icons/verification-steps/finalized.svg",
    "/wallet/ui_icons/verification-steps/unfinalized.svg",
    "/wallet/ui_icons/verified.svg",
    "/wallet/ui_icons/wallet.svg",
    "/wallet/ui_icons/wallets/coinbase.svg",
    "/wallet/ui_icons/wallets/metamask.svg",
    "/wallet/ui_icons/wallets/okx.svg",
    "/wallet/ui_icons/wallets/rabby.svg",
    "/wallet/ui_icons/wallets/token-pocket.svg",
    "/wallet/ui_icons/wallets/trust.svg",
    "/wallet/verse_icon_app.svg",
    "/wallet/wallet.svg",
    "/wallet/wormhole.svg",
    "/wallet/x.svg",
];

const API: &[(&str, &str)] = &[
    ("DELETE", "/wallet/api/push/subscribe"),
    ("GET", "/explorer/api/config"),
    ("GET", "/explorer/api/csrf"),
    ("GET", "/explorer/api/healthz"),
    ("GET", "/explorer/api/log"),
    ("GET", "/explorer/api/metrics"),
    (
        "GET",
        "/explorer/api/tokens/[hash]/instances/[id]/media-type",
    ),
    ("GET", "/explorer/node-api/config"),
    ("GET", "/explorer/node-api/csrf"),
    ("GET", "/explorer/node-api/healthz"),
    ("GET", "/explorer/node-api/log"),
    ("GET", "/explorer/node-api/metrics"),
    (
        "GET",
        "/explorer/node-api/tokens/[hash]/instances/[id]/media-type",
    ),
    ("GET", "/wallet/api/candle/cv/bnb/history"),
    ("GET", "/wallet/api/candle/cv/bnb/price/"),
    ("GET", "/wallet/api/candle/cv/eth/history"),
    ("GET", "/wallet/api/candle/cv/eth/price/"),
    ("GET", "/wallet/api/candle/cv/pax/history"),
    ("GET", "/wallet/api/candle/cv/pax/price/"),
    ("GET", "/wallet/api/candle/cv/sol/history"),
    ("GET", "/wallet/api/candle/cv/sol/price/"),
    ("GET", "/wallet/api/candle/pax/history"),
    ("GET", "/wallet/api/candle/sid/history"),
    ("GET", "/wallet/api/fx/latest/USD"),
    ("GET", "/wallet/api/health"),
    ("GET", "/wallet/api/media"),
    ("GET", "/wallet/api/pns/api/v1/addresses/[address]"),
    ("GET", "/wallet/api/pns/api/v1/addresses:lookup"),
    ("GET", "/wallet/api/pns/api/v1/domains/[name]"),
    ("GET", "/wallet/api/pns/api/v1/domains/[name]/events"),
    ("GET", "/wallet/api/pns/api/v1/domains:lookup"),
    ("GET", "/wallet/api/points/balance/[address]"),
    ("GET", "/wallet/api/sdk/candles/history"),
    (
        "GET",
        "/wallet/api/sdk/metadata/metadata/[tokenAddress].json",
    ),
    ("GET", "/wallet/api/sdk/stats/stats/[poolAddress]"),
    (
        "GET",
        "/wallet/api/sdk/stats/stats/[poolAddress]/holders/distribution",
    ),
    ("GET", "/wallet/api/sdk/stats/stats/batch"),
    ("GET", "/wallet/api/sidiora/logo/[address].png"),
    ("GET", "/wallet/api/sidiora/metadata"),
    ("GET", "/wallet/api/token-icon/[address]"),
    ("GET", "/wallet/api/token-metadata"),
    ("GET", "/wallet/api/wallet/api/v1/[address]/dex-history"),
    ("GET", "/wallet/api/wallet/api/v1/[address]/performance"),
    ("GET", "/wallet/api/wallet/api/v1/[address]/profile"),
    ("GET", "/wallet/api/wallet/api/v1/[address]/rank"),
    ("GET", "/wallet/api/wallet/api/v1/charts/[symbol]"),
    (
        "GET",
        "/wallet/api/wallet/api/v1/portfolio/[address]/charts/holdings",
    ),
    (
        "GET",
        "/wallet/api/wallet/api/v1/portfolio/[address]/charts/pnl",
    ),
    (
        "GET",
        "/wallet/api/wallet/api/v1/portfolio/[address]/charts/tx-volume",
    ),
    (
        "GET",
        "/wallet/api/wallet/api/v1/portfolio/[address]/charts/value",
    ),
    ("GET", "/wallet/api/wallet/api/v1/portfolio/[address]/pnl"),
    ("GET", "/wallet/api/wallet/api/v1/trending"),
    ("GET", "/wallet/api/wallet/api/v2/addresses/[addressHash]"),
    (
        "GET",
        "/wallet/api/wallet/api/v2/addresses/[addressHash]/coin-balance-history",
    ),
    (
        "GET",
        "/wallet/api/wallet/api/v2/addresses/[addressHash]/coin-balance-history-by-day",
    ),
    (
        "GET",
        "/wallet/api/wallet/api/v2/addresses/[addressHash]/counters",
    ),
    (
        "GET",
        "/wallet/api/wallet/api/v2/addresses/[addressHash]/logs",
    ),
    (
        "GET",
        "/wallet/api/wallet/api/v2/addresses/[addressHash]/nft",
    ),
    (
        "GET",
        "/wallet/api/wallet/api/v2/addresses/[addressHash]/nft/collections",
    ),
    (
        "GET",
        "/wallet/api/wallet/api/v2/addresses/[addressHash]/tabs-counters",
    ),
    (
        "GET",
        "/wallet/api/wallet/api/v2/addresses/[addressHash]/token-balances",
    ),
    (
        "GET",
        "/wallet/api/wallet/api/v2/addresses/[addressHash]/token-transfers",
    ),
    (
        "GET",
        "/wallet/api/wallet/api/v2/addresses/[addressHash]/tokens",
    ),
    (
        "GET",
        "/wallet/api/wallet/api/v2/addresses/[addressHash]/transactions",
    ),
    ("GET", "/wallet/api/wallet/api/v2/main-page/blocks"),
    ("GET", "/wallet/api/wallet/api/v2/main-page/transactions"),
    ("GET", "/wallet/api/wallet/api/v2/search"),
    ("GET", "/wallet/api/wallet/api/v2/stats"),
    ("GET", "/wallet/api/wallet/api/v2/tokens"),
    ("GET", "/wallet/api/wallet/api/v2/tokens/[tokenHash]"),
    (
        "GET",
        "/wallet/api/wallet/api/v2/tokens/[tokenHash]/holders",
    ),
    ("GET", "/wallet/api/wallet/api/v2/transactions/[txHash]"),
    (
        "GET",
        "/wallet/api/wallet/api/v2/transactions/[txHash]/logs",
    ),
    (
        "GET",
        "/wallet/api/wallet/api/v2/transactions/[txHash]/token-transfers",
    ),
    ("GET", "/wallet/api/wallet/health"),
    ("HEAD", "/explorer/api/config"),
    ("HEAD", "/explorer/api/healthz"),
    ("HEAD", "/explorer/api/metrics"),
    ("HEAD", "/explorer/node-api/config"),
    ("HEAD", "/explorer/node-api/healthz"),
    ("HEAD", "/explorer/node-api/metrics"),
    ("HEAD", "/wallet/api/wallet/api/v1/[address]/dex-history"),
    ("HEAD", "/wallet/api/wallet/api/v1/[address]/performance"),
    ("HEAD", "/wallet/api/wallet/api/v1/[address]/profile"),
    ("HEAD", "/wallet/api/wallet/api/v1/[address]/rank"),
    ("HEAD", "/wallet/api/wallet/api/v1/charts/[symbol]"),
    (
        "HEAD",
        "/wallet/api/wallet/api/v1/portfolio/[address]/charts/holdings",
    ),
    (
        "HEAD",
        "/wallet/api/wallet/api/v1/portfolio/[address]/charts/pnl",
    ),
    (
        "HEAD",
        "/wallet/api/wallet/api/v1/portfolio/[address]/charts/tx-volume",
    ),
    (
        "HEAD",
        "/wallet/api/wallet/api/v1/portfolio/[address]/charts/value",
    ),
    ("HEAD", "/wallet/api/wallet/api/v1/portfolio/[address]/pnl"),
    ("HEAD", "/wallet/api/wallet/api/v1/trending"),
    ("HEAD", "/wallet/api/wallet/api/v2/addresses/[addressHash]"),
    (
        "HEAD",
        "/wallet/api/wallet/api/v2/addresses/[addressHash]/coin-balance-history",
    ),
    (
        "HEAD",
        "/wallet/api/wallet/api/v2/addresses/[addressHash]/coin-balance-history-by-day",
    ),
    (
        "HEAD",
        "/wallet/api/wallet/api/v2/addresses/[addressHash]/counters",
    ),
    (
        "HEAD",
        "/wallet/api/wallet/api/v2/addresses/[addressHash]/logs",
    ),
    (
        "HEAD",
        "/wallet/api/wallet/api/v2/addresses/[addressHash]/nft",
    ),
    (
        "HEAD",
        "/wallet/api/wallet/api/v2/addresses/[addressHash]/nft/collections",
    ),
    (
        "HEAD",
        "/wallet/api/wallet/api/v2/addresses/[addressHash]/tabs-counters",
    ),
    (
        "HEAD",
        "/wallet/api/wallet/api/v2/addresses/[addressHash]/token-balances",
    ),
    (
        "HEAD",
        "/wallet/api/wallet/api/v2/addresses/[addressHash]/token-transfers",
    ),
    (
        "HEAD",
        "/wallet/api/wallet/api/v2/addresses/[addressHash]/tokens",
    ),
    (
        "HEAD",
        "/wallet/api/wallet/api/v2/addresses/[addressHash]/transactions",
    ),
    ("HEAD", "/wallet/api/wallet/api/v2/main-page/blocks"),
    ("HEAD", "/wallet/api/wallet/api/v2/main-page/transactions"),
    ("HEAD", "/wallet/api/wallet/api/v2/search"),
    ("HEAD", "/wallet/api/wallet/api/v2/stats"),
    ("HEAD", "/wallet/api/wallet/api/v2/tokens"),
    ("HEAD", "/wallet/api/wallet/api/v2/tokens/[tokenHash]"),
    (
        "HEAD",
        "/wallet/api/wallet/api/v2/tokens/[tokenHash]/holders",
    ),
    ("HEAD", "/wallet/api/wallet/api/v2/transactions/[txHash]"),
    (
        "HEAD",
        "/wallet/api/wallet/api/v2/transactions/[txHash]/logs",
    ),
    (
        "HEAD",
        "/wallet/api/wallet/api/v2/transactions/[txHash]/token-transfers",
    ),
    ("HEAD", "/wallet/api/wallet/health"),
    ("POST", "/explorer/api/log"),
    ("POST", "/explorer/api/monitoring/invalid-api-schema"),
    ("POST", "/explorer/node-api/log"),
    ("POST", "/explorer/node-api/monitoring/invalid-api-schema"),
    ("POST", "/wallet/api/chat"),
    ("POST", "/wallet/api/push/subscribe"),
    ("GET", "/wallet/api/sdk/ranking/rankings/trending"),
    ("GET", "/wallet/api/sdk/ranking/rankings/breakout"),
    ("GET", "/wallet/api/sdk/ranking/rankings/new"),
    ("GET", "/wallet/api/sdk/ranking/rankings/top_volume"),
    ("GET", "/wallet/api/sdk/ranking/rankings/unusual"),
    ("GET", "/wallet/api/sdk/ranking/rankings/movers"),
];
