use layerx_sdk::rpc::LIST_ASSETS_MAX_PAGE;
use serde_json::{json, Value};

use crate::http::Client;

const PROGRAM_EVENTS_MAX_PAGE: u64 = 256;
const PROGRAM_EVENT_MAX_TOPIC_BYTES: usize = 64;
const PROGRAM_EVENT_MAX_DATA_BYTES: usize = 65_536;

pub struct RpcClient {
    client: Client,
    url: String,
    credential: Option<zeroize::Zeroizing<String>>,
    identity_session: bool,
}

impl RpcClient {
    /// # Errors
    /// Requires the gateway's explicit /rpc endpoint and the HTTP client's TLS rules.
    pub fn new(url: &str, credential: Option<zeroize::Zeroizing<String>>) -> Result<Self, String> {
        let base = url.strip_suffix("/rpc").ok_or("RPC URL must end in /rpc")?;
        let client = match credential.clone() {
            Some(value) => Client::new_gateway(base, value)?,
            None => Client::new(base, None)?,
        };
        Ok(Self {
            client,
            url: url.to_owned(),
            credential,
            identity_session: false,
        })
    }

    /// # Errors
    /// Requires an explicit identity session, an /rpc endpoint, and verified HTTPS off loopback.
    pub fn new_session(url: &str, session: zeroize::Zeroizing<String>) -> Result<Self, String> {
        let base = url.strip_suffix("/rpc").ok_or("RPC URL must end in /rpc")?;
        if session.is_empty() {
            return Err("identity_session_required: an empty identity session is invalid".into());
        }
        Ok(Self {
            client: Client::new(base, Some(session))?,
            url: url.to_owned(),
            credential: None,
            identity_session: true,
        })
    }

    /// # Errors
    /// Refuses unsupported methods, invalid parameters, transport failures and RPC errors.
    pub fn call(&self, method: &str, params: &Value) -> Result<Value, String> {
        if matches!(method, "lx_subscribe" | "lx_unsubscribe") {
            return Err(format!(
                "rpc_transport_required: {method} requires authenticated WebSocket transport"
            ));
        }
        let request = request(method, params)?;
        if method == "lx_requestFunds" && !self.identity_session {
            return Err(
                "identity_session_required: lx_requestFunds requires a bearer identity session"
                    .into(),
            );
        }
        let result = decode_response(method, &self.client.post("/rpc", &request, None)?)?;
        if method == "lx_getProgramEvents" {
            program_events_page(params, &result)?;
        }
        Ok(result)
    }

    /// # Errors
    /// Requires a gateway credential and a valid live subscription; notifications are unverified.
    pub fn subscribe(&self, params: &Value, timeout: std::time::Duration) -> Result<Value, String> {
        let credential = self
            .credential
            .as_ref()
            .ok_or("gateway_credential_required: subscriptions require --gateway-credential")?;
        crate::rpc_subscription::next(&self.url, credential, params, timeout)
    }
}

/// # Errors
/// Validates positional parameters against the published `OpenRPC` contract.
pub fn request(method: &str, params: &Value) -> Result<Value, String> {
    let args = params
        .as_array()
        .ok_or("RPC parameters must be positional")?;
    match method {
        "lx_register" => registration_params(args)?,
        "lx_requestFunds" => faucet_params(args)?,
        "lx_getNodeInfo" if args.is_empty() => {}
        "lx_listAssets" => asset_page_params(args)?,
        "lx_getAsset"
        | "lx_getAccount"
        | "lx_getBalance"
        | "lx_getReceipt"
        | "lx_getActivityStatus"
        | "lx_getCheckpoint" => {
            let [Value::String(id)] = args.as_slice() else {
                return Err(format!("{method} requires one hexadecimal identifier"));
            };
            id32(id)?;
        }
        "lx_getSequence" => sequence_params(args)?,
        "lx_getProgramEvents" => {
            program_events_query(args)?;
        }
        "lx_getBatchHeader" => {
            let [Value::String(number)] = args.as_slice() else {
                return Err("lx_getBatchHeader requires one decimal string".into());
            };
            if number
                .parse::<u64>()
                .ok()
                .filter(|n| *n > 0 && n.to_string() == *number)
                .is_none()
            {
                return Err("batch number must be a positive canonical u64 string".into());
            }
        }
        "lx_getBalances" => {
            let [Value::String(did)] = args.as_slice() else {
                return Err("lx_getBalances requires one DID".into());
            };
            layerx_types::ids::Did::new(did.as_bytes())
                .map_err(|e| format!("invalid DID: {e:?}"))?;
            crate::http::validate_resource_id(did, "DID")?;
        }
        "lx_getProof" => match args.as_slice() {
            [Value::String(kind), Value::String(activity)]
                if matches!(kind.as_str(), "activity" | "receipt") =>
            {
                id32(activity)?;
            }
            [Value::String(kind), Value::String(activity), Value::String(account)]
                if kind == "account" =>
            {
                id32(activity)?;
                id32(account)?;
            }
            _ => return Err(
                "lx_getProof requires kind, activity_id, and account_id only for account proofs"
                    .into(),
            ),
        },
        "lx_estimateFee" => {
            let [Value::String(canonical)] = args.as_slice() else {
                return Err("lx_estimateFee requires canonical_hex".into());
            };
            canonical_hex(canonical)?;
        }
        "lx_subscribe" => subscription_params(args)?,
        "lx_unsubscribe" => {
            let [Value::String(subscription)] = args.as_slice() else {
                return Err("lx_unsubscribe requires one subscription identifier".into());
            };
            canonical_decimal(subscription, "subscription identifier")?;
        }
        "lx_sendActivity" => {
            let [Value::String(canonical), Value::String(commitment)] = args.as_slice() else {
                return Err("lx_sendActivity requires canonical_hex and commitment".into());
            };
            if canonical.is_empty()
                || canonical.len() > 1_048_576
                || canonical.len() % 2 != 0
                || !canonical.bytes().all(|b| b.is_ascii_hexdigit())
                || !matches!(commitment.as_str(), "executed" | "batched" | "finalised")
            {
                return Err("invalid canonical activity or commitment".into());
            }
        }
        "lx_getNodeInfo" => return Err("lx_getNodeInfo takes no parameters".into()),
        _ => {
            return Err(format!(
                "rpc_method_unavailable: {method} is absent from the published OpenRPC contract"
            ))
        }
    }
    Ok(json!({"jsonrpc":"2.0","id":1,"method":method,"params":params}))
}

fn program_events_query(args: &[Value]) -> Result<(&str, u64, u64), String> {
    let [Value::Object(query)] = args else {
        return Err("lx_getProgramEvents requires one query object".into());
    };
    let topic = query
        .get("topic")
        .and_then(Value::as_str)
        .filter(|topic| lowercase_hex_bytes(topic, 1, PROGRAM_EVENT_MAX_TOPIC_BYTES));
    let from_sequence = query.get("from_sequence").and_then(Value::as_u64);
    let limit = query
        .get("limit")
        .and_then(Value::as_u64)
        .filter(|limit| (1..=PROGRAM_EVENTS_MAX_PAGE).contains(limit));
    match (query.len(), topic, from_sequence, limit) {
        (3, Some(topic), Some(from_sequence), Some(limit)) => Ok((topic, from_sequence, limit)),
        _ => Err(format!(
            "lx_getProgramEvents requires exactly topic (1 to {PROGRAM_EVENT_MAX_TOPIC_BYTES} \
             lowercase hex bytes), from_sequence (u64) and limit (1 to {PROGRAM_EVENTS_MAX_PAGE})"
        )),
    }
}

/// # Errors
/// Refuses a program event page that does not match the query it answers.
pub fn program_events_page(params: &Value, result: &Value) -> Result<(), String> {
    let args = params
        .as_array()
        .ok_or("RPC parameters must be positional")?;
    let (topic, from_sequence, limit) = program_events_query(args)?;
    let invalid = || "lx_getProgramEvents returned a page that does not match the query".to_owned();
    let page = result
        .as_object()
        .filter(|page| page.len() == 2)
        .ok_or_else(invalid)?;
    let next = page
        .get("next_sequence")
        .and_then(Value::as_u64)
        .filter(|next| *next >= from_sequence)
        .ok_or_else(invalid)?;
    let events = page
        .get("events")
        .and_then(Value::as_array)
        .filter(|events| u64::try_from(events.len()).is_ok_and(|count| count <= limit))
        .ok_or_else(invalid)?;
    let mut previous = None;
    for event in events {
        let event = event
            .as_object()
            .filter(|event| event.len() == 4)
            .ok_or_else(invalid)?;
        let sequence = event
            .get("sequence")
            .and_then(Value::as_u64)
            .filter(|sequence| (from_sequence..next).contains(sequence))
            .filter(|sequence| previous.is_none_or(|previous| previous < *sequence))
            .ok_or_else(invalid)?;
        let program_id = event.get("program_id").and_then(Value::as_str);
        let data = event.get("data").and_then(Value::as_str);
        if !program_id.is_some_and(|id| lowercase_hex(id, 32) && id.bytes().any(|b| b != b'0'))
            || event.get("topic").and_then(Value::as_str) != Some(topic)
            || !data.is_some_and(|data| lowercase_hex_bytes(data, 0, PROGRAM_EVENT_MAX_DATA_BYTES))
        {
            return Err(invalid());
        }
        previous = Some(sequence);
    }
    Ok(())
}

fn lowercase_hex_bytes(value: &str, min: usize, max: usize) -> bool {
    value.len().is_multiple_of(2)
        && (min * 2..=max * 2).contains(&value.len())
        && lowercase_hex(value, value.len() / 2)
}

fn asset_page_params(args: &[Value]) -> Result<(), String> {
    let (cursor, limit) = match args {
        [] => (None, None),
        [cursor] => (Some(cursor), None),
        [cursor, limit] => (Some(cursor), Some(limit)),
        _ => {
            return Err(
                "lx_listAssets accepts an optional cursor and an optional limit only".into(),
            )
        }
    };
    match cursor {
        None | Some(Value::Null) => {}
        Some(Value::String(cursor)) => id32(cursor)?,
        Some(_) => {
            return Err("lx_listAssets cursor must be a nonzero asset identifier or null".into())
        }
    }
    let bounded = match limit {
        None => true,
        Some(Value::Number(limit)) => limit
            .as_u64()
            .is_some_and(|count| (1..=u64::from(LIST_ASSETS_MAX_PAGE)).contains(&count)),
        Some(_) => false,
    };
    if !bounded {
        return Err(format!(
            "lx_listAssets limit must be an integer between 1 and {LIST_ASSETS_MAX_PAGE}"
        ));
    }
    Ok(())
}

fn registration_params(args: &[Value]) -> Result<(), String> {
    let [Value::String(signer), Value::String(signature)] = args else {
        return Err("lx_register requires signer_public_key and registration_signature".into());
    };
    if !lowercase_hex(signer, 32) || !lowercase_hex(signature, 64) {
        return Err("lx_register requires a lowercase 32-byte key and 64-byte signature".into());
    }
    Ok(())
}

fn faucet_params(args: &[Value]) -> Result<(), String> {
    let [Value::String(did), Value::String(signer)] = args else {
        return Err("lx_requestFunds requires DID and signer_public_key".into());
    };
    layerx_types::ids::Did::new(did.as_bytes()).map_err(|e| format!("invalid DID: {e:?}"))?;
    let valid_did = did
        .strip_prefix("did:")
        .and_then(|value| value.split_once(':'))
        .is_some_and(|(method, identifier)| !method.is_empty() && !identifier.is_empty())
        && did.len() <= 512
        && did
            .bytes()
            .all(|byte| byte.is_ascii_alphanumeric() || b"-._:".contains(&byte));
    if !valid_did || !lowercase_hex(signer, 32) || signer.bytes().all(|byte| byte == b'0') {
        return Err(
            "lx_requestFunds requires a canonical DID and nonzero lowercase signer key".into(),
        );
    }
    Ok(())
}

fn lowercase_hex(value: &str, bytes: usize) -> bool {
    value.len() == bytes * 2
        && value
            .bytes()
            .all(|byte| byte.is_ascii_digit() || (b'a'..=b'f').contains(&byte))
}

/// # Errors
/// Rejects mismatched IDs, malformed envelopes, RPC errors, and non-object results.
pub fn decode_response(method: &str, response: &Value) -> Result<Value, String> {
    if response.get("jsonrpc") != Some(&json!("2.0"))
        || response.get("id") != Some(&json!(1))
        || response.get("result").is_some() == response.get("error").is_some()
    {
        return Err(format!("invalid JSON-RPC response for {method}"));
    }
    if let Some(error) = response.get("error") {
        let code = error
            .get("code")
            .and_then(Value::as_i64)
            .ok_or("malformed RPC error code")?;
        let message = error
            .get("message")
            .and_then(Value::as_str)
            .ok_or("malformed RPC error message")?;
        return Err(json!({"code":code,"message":message,"data":error.get("data")}).to_string());
    }
    if method == "lx_subscribe" {
        return response
            .get("result")
            .filter(|value| {
                value
                    .as_str()
                    .is_some_and(|id| !id.is_empty() && id.len() <= 128)
            })
            .cloned()
            .ok_or_else(|| "invalid subscription identifier".to_owned());
    }
    if method == "lx_unsubscribe" {
        return response
            .get("result")
            .filter(|value| value.as_bool() == Some(true))
            .cloned()
            .ok_or_else(|| "invalid unsubscribe acknowledgement".to_owned());
    }
    response
        .get("result")
        .filter(|value| value.is_object())
        .cloned()
        .ok_or_else(|| format!("{method} returned a non-object result"))
}

fn subscription_params(args: &[Value]) -> Result<(), String> {
    match args {
        [Value::String(topic)] if matches!(topic.as_str(), "receipts" | "checkpoints") => {}
        [Value::String(topic), Value::String(cursor)]
            if matches!(topic.as_str(), "receipts" | "checkpoints") =>
        {
            canonical_decimal(cursor, "subscription cursor")?;
        }
        [Value::String(topic), Value::String(account)] if topic == "account" => id32(account)?,
        [Value::String(topic), Value::String(account), Value::String(cursor)]
            if topic == "account" =>
        {
            id32(account)?;
            canonical_decimal(cursor, "subscription cursor")?;
        }
        _ => {
            return Err(
                "lx_subscribe requires receipts, checkpoints, or account with account_id, \
                 each optionally followed by a cursor"
                    .into(),
            )
        }
    }
    Ok(())
}

fn sequence_params(args: &[Value]) -> Result<(), String> {
    match args {
        [Value::String(account)] => id32(account)?,
        [Value::String(did), Value::String(selector)] if selector == "identity" => {
            layerx_types::ids::Did::new(did.as_bytes())
                .map_err(|e| format!("invalid DID: {e:?}"))?;
            crate::http::validate_resource_id(did, "DID")?;
        }
        _ => return Err("lx_getSequence requires account_id or DID and identity selector".into()),
    }
    Ok(())
}

fn canonical_hex(value: &str) -> Result<(), String> {
    if value.is_empty()
        || value.len() > 1_048_576
        || !value.len().is_multiple_of(2)
        || !value.bytes().all(|byte| byte.is_ascii_hexdigit())
    {
        return Err("invalid canonical activity hex".into());
    }
    Ok(())
}

fn canonical_decimal(value: &str, what: &str) -> Result<(), String> {
    let canonical = value == "0"
        || (!value.is_empty()
            && !value.starts_with('0')
            && value.bytes().all(|byte| byte.is_ascii_digit()));
    if !canonical {
        return Err(format!("{what} must be a canonical decimal string"));
    }
    Ok(())
}

fn id32(value: &str) -> Result<(), String> {
    if value.len() != 64
        || !value.bytes().all(|b| b.is_ascii_hexdigit())
        || value.bytes().all(|b| b == b'0')
    {
        return Err("identifier must be 64 hexadecimal characters and nonzero".into());
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use layerx_sdk::rpc::{list_assets_params, AssetListSnapshot, LIST_ASSETS_DEFAULT_PAGE};

    use super::*;

    fn asset_value(asset_id: &str) -> Value {
        json!({
            "asset_id": asset_id,
            "symbol": "LXT",
            "name": "LayerX Test",
            "decimals": 6,
            "custody_kind": 0,
            "custody_reference": "",
            "paused": false,
            "supply_cap": "1000000",
            "issuer_did": "22".repeat(32),
            "issuer_kind": 1,
            "total_units": "100",
            "salt": "33".repeat(32)
        })
    }

    fn asset_page(assets: Vec<Value>, next_cursor: Value) -> Value {
        json!({
            "assets": assets,
            "next_cursor": next_cursor,
            "observed_head_sequence": "9",
            "state_root": "44".repeat(32),
            "verification": "authenticated_committed_snapshot"
        })
    }

    fn assert_published_methods(methods: &[Value]) -> Result<(), String> {
        assert_eq!(
            methods
                .iter()
                .filter_map(|entry| entry["name"].as_str())
                .collect::<Vec<_>>(),
            [
                "lx_register",
                "lx_requestFunds",
                "lx_getAccount",
                "lx_getBalance",
                "lx_getBalances",
                "lx_getReceipt",
                "lx_getActivityStatus",
                "lx_getBatchHeader",
                "lx_getCheckpoint",
                "lx_getNodeInfo",
                "lx_getSequence",
                "lx_getProof",
                "lx_sendActivity",
                "lx_subscribe",
                "lx_unsubscribe",
                "lx_listAssets",
                "lx_getAsset",
                "lx_estimateFee",
                "lx_getProgramEvents",
                "px_resolveAccount",
                "px_getAccount",
                "px_getBalances",
                "px_listAssets",
                "px_getNetwork",
                "lx_getHistory",
                "px_getHistory",
                "px_getUnifiedHistory",
                "px_getCapabilities",
                "px_getRouteCatalogue",
            ]
        );
        let description = |method: &str| -> Result<&str, String> {
            methods
                .iter()
                .find(|entry| entry["name"] == method)
                .and_then(|entry| entry["description"].as_str())
                .ok_or_else(|| format!("{method} description missing"))
        };
        let entry = |method: &str| -> Result<&Value, String> {
            methods
                .iter()
                .find(|entry| entry["name"] == method)
                .ok_or_else(|| format!("{method} contract missing"))
        };
        assert!(description("lx_getBalances")?.contains("LNI minor 5"));
        let listing = entry("lx_listAssets")?;
        assert_eq!(listing["paramStructure"], "by-position");
        assert_eq!(listing["params"][0]["name"], "cursor");
        assert_eq!(listing["params"][0]["required"], json!(false));
        assert_eq!(
            listing["params"][0]["schema"]["type"],
            json!(["string", "null"])
        );
        assert_eq!(
            listing["params"][0]["schema"]["pattern"],
            "^[0-9a-fA-F]{64}$"
        );
        assert_eq!(listing["params"][1]["name"], "limit");
        assert_eq!(listing["params"][1]["required"], json!(false));
        assert_eq!(
            listing["params"][1]["schema"],
            json!({"type":"integer","minimum":1,"maximum":LIST_ASSETS_MAX_PAGE})
        );
        assert_eq!(listing["params"][2], Value::Null);
        let listing_description = description("lx_listAssets")?;
        for phrase in [
            "ordered by ascending asset_id".to_owned(),
            "next_cursor".to_owned(),
            "The native registry holds up to 1024 records".to_owned(),
            "cursor is the exclusive asset_id to resume after".to_owned(),
            format!("limit defaults to {LIST_ASSETS_DEFAULT_PAGE} and may not exceed {LIST_ASSETS_MAX_PAGE}"),
            "next_cursor is null once the page is the last one".to_owned(),
            "A malformed cursor or limit returns -32602".to_owned(),
        ] {
            assert!(
                listing_description.contains(&phrase),
                "lx_listAssets description lost: {phrase}"
            );
        }
        let asset = entry("lx_getAsset")?;
        assert_eq!(asset["params"][0]["name"], "asset_id");
        assert_eq!(asset["params"][0]["required"], json!(true));
        assert_eq!(asset["params"][0]["schema"]["pattern"], "^[0-9a-fA-F]{64}$");
        assert_eq!(asset["params"][1], Value::Null);
        let asset_description = description("lx_getAsset")?;
        assert!(asset_description.contains("authenticated_committed_snapshot"));
        assert!(asset_description.contains("Unknown assets or unavailable evidence return -32001"));
        let fee = entry("lx_estimateFee")?;
        assert_eq!(fee["params"][0]["name"], "canonical_hex");
        assert_eq!(fee["params"][0]["schema"]["pattern"], "^([0-9a-fA-F]{2})+$");
        assert_eq!(fee["params"][0]["schema"]["maxLength"], json!(1_048_576));
        assert_eq!(fee["params"][1], Value::Null);
        let fee_description = description("lx_estimateFee")?;
        assert!(fee_description.contains("Asset 1/2/3/4/5/6/7/8/10/11"));
        assert!(fee_description.contains("Programs 1/2/3/5/6/7"));
        let events = entry("lx_getProgramEvents")?;
        assert_eq!(events["paramStructure"], "by-position");
        assert_eq!(events["params"][0]["name"], "query");
        assert_eq!(events["params"][0]["required"], json!(true));
        assert_eq!(events["params"][1], Value::Null);
        let query = &events["params"][0]["schema"];
        assert_eq!(query["additionalProperties"], json!(false));
        assert_eq!(
            query["required"],
            json!(["topic", "from_sequence", "limit"])
        );
        assert_eq!(
            query["properties"]["topic"]["pattern"],
            format!("^([0-9a-f]{{2}}){{1,{PROGRAM_EVENT_MAX_TOPIC_BYTES}}}$")
        );
        assert_eq!(
            query["properties"]["from_sequence"],
            json!({"type":"integer","minimum":0,"maximum":u64::MAX})
        );
        assert_eq!(
            query["properties"]["limit"],
            json!({"type":"integer","minimum":1,"maximum":PROGRAM_EVENTS_MAX_PAGE})
        );
        let page = &events["result"]["schema"];
        assert_eq!(page["required"], json!(["events", "next_sequence"]));
        assert_eq!(
            page["properties"]["events"]["maxItems"],
            json!(PROGRAM_EVENTS_MAX_PAGE)
        );
        assert_eq!(
            page["properties"]["events"]["items"]["properties"]["data"]["maxLength"],
            json!(PROGRAM_EVENT_MAX_DATA_BYTES * 2)
        );
        let events_description = description("lx_getProgramEvents")?;
        assert!(events_description.contains("returning at most limit events (1 to 256)"));
        assert!(events_description.contains("Malformed or extra params return -32602"));
        let submission_description = description("lx_sendActivity")?;
        assert!(submission_description.contains("Asset ordinal 9 is reserved and refused"));
        assert!(submission_description
            .contains("An admission acknowledgement never establishes execution"));
        Ok(())
    }

    #[test]
    fn positional_requests_match_published_contract() -> Result<(), String> {
        let fixture = include_str!("../tests/fixtures/openrpc.json");
        let published_source = include_str!("../../hosted/gateway/openrpc.json");
        assert_eq!(fixture, published_source);
        let published: Value = serde_json::from_str(fixture).map_err(|error| error.to_string())?;
        let methods = published["methods"]
            .as_array()
            .ok_or("missing contract methods")?;
        assert_published_methods(methods)?;
        let id = "ab".repeat(32);
        for method in [
            "lx_getAsset",
            "lx_getAccount",
            "lx_getBalance",
            "lx_getSequence",
            "lx_getReceipt",
            "lx_getActivityStatus",
            "lx_getCheckpoint",
        ] {
            assert!(methods.iter().any(|entry| entry["name"] == method));
            assert_eq!(request(method, &json!([id]))?["params"], json!([id]));
            assert!(request(method, &json!({"account_id":id})).is_err());
            assert!(request(method, &json!([id, id])).is_err());
        }
        let sequence = methods
            .iter()
            .find(|entry| entry["name"] == "lx_getSequence")
            .ok_or("sequence contract missing")?;
        assert_eq!(sequence["params"][1]["name"], "selector");
        assert_eq!(sequence["params"][1]["schema"]["enum"], json!(["identity"]));
        assert!(sequence["description"]
            .as_str()
            .ok_or("description missing")?
            .contains("[did, \"identity\"]"));
        let subscribe = methods
            .iter()
            .find(|entry| entry["name"] == "lx_subscribe")
            .ok_or("subscribe contract missing")?;
        assert_eq!(subscribe["params"][2]["name"], "cursor");
        assert_eq!(
            subscribe["params"][2]["schema"]["pattern"],
            "^(0|[1-9][0-9]*)$"
        );
        let unsubscribe = methods
            .iter()
            .find(|entry| entry["name"] == "lx_unsubscribe")
            .ok_or("unsubscribe contract missing")?;
        assert_eq!(unsubscribe["params"][0]["name"], "subscription");
        assert_eq!(
            unsubscribe["params"][0]["schema"]["pattern"],
            "^(0|[1-9][0-9]*)$"
        );
        assert_eq!(unsubscribe["result"]["schema"]["enum"], json!([true]));
        assert!(unsubscribe["description"]
            .as_str()
            .ok_or("description missing")?
            .contains("WebSocket only"));
        request("lx_getSequence", &json!(["did:layerx:alice", "identity"]))?;
        assert!(request("lx_getSequence", &json!(["did:layerx:alice", "account"])).is_err());
        assert!(request("lx_getSequence", &json!(["../alice", "identity"])).is_err());
        request("lx_getBalances", &json!(["did:layerx:alice"]))?;
        request("lx_getNodeInfo", &json!([]))?;
        request("lx_listAssets", &json!([]))?;
        request("lx_listAssets", &json!([Value::Null]))?;
        request("lx_listAssets", &json!([id]))?;
        request("lx_listAssets", &json!([id.to_uppercase()]))?;
        request("lx_listAssets", &json!([id, 1]))?;
        request("lx_listAssets", &json!([id, LIST_ASSETS_MAX_PAGE]))?;
        request(
            "lx_listAssets",
            &json!([Value::Null, LIST_ASSETS_DEFAULT_PAGE]),
        )?;
        request("lx_estimateFee", &json!(["abcd"]))?;
        request("lx_subscribe", &json!(["receipts"]))?;
        request("lx_subscribe", &json!(["checkpoints"]))?;
        request("lx_subscribe", &json!(["account", id]))?;
        request("lx_subscribe", &json!(["receipts", "0"]))?;
        request("lx_subscribe", &json!(["checkpoints", "17"]))?;
        request("lx_subscribe", &json!(["account", id, "3"]))?;
        request("lx_unsubscribe", &json!(["1"]))?;
        request("lx_getBatchHeader", &json!(["12"]))?;
        request("lx_getProof", &json!(["account", id, id]))?;
        for commitment in ["executed", "batched", "finalised"] {
            request("lx_sendActivity", &json!(["abcd", commitment]))?;
        }
        for (method, args) in [
            ("lx_getSequence", json!(["did:layerx:alice"])),
            ("lx_getBatchHeader", json!(["01"])),
            ("lx_getBalances", json!(["../alice"])),
            ("lx_getProof", json!(["account", id])),
            ("lx_sendActivity", json!(["abc", "executed"])),
            ("lx_sendActivity", json!(["abcd", "ack"])),
            ("lx_estimateFee", json!([])),
            ("lx_estimateFee", json!(["abc"])),
            ("lx_subscribe", json!(["account"])),
            ("lx_subscribe", json!(["receipts", id])),
            ("lx_subscribe", json!(["unknown"])),
            ("lx_subscribe", json!(["receipts", "01"])),
            ("lx_subscribe", json!(["account", id, "-1"])),
            ("lx_subscribe", json!(["account", id, id])),
            ("lx_unsubscribe", json!([])),
            ("lx_unsubscribe", json!(["01"])),
            ("lx_unsubscribe", json!([1])),
            ("lx_unsubscribe", json!(["1", "2"])),
            ("lx_getAsset", json!([])),
            ("lx_listAssets", json!([id, 0])),
            (
                "lx_listAssets",
                json!([id, u64::from(LIST_ASSETS_MAX_PAGE) + 1]),
            ),
            ("lx_listAssets", json!([id, "64"])),
            ("lx_listAssets", json!([id, -1])),
            ("lx_listAssets", json!([id, LIST_ASSETS_DEFAULT_PAGE, id])),
            ("lx_listAssets", json!(["00".repeat(32)])),
            ("lx_listAssets", json!(["ab"])),
            ("lx_listAssets", json!([1])),
            ("lx_listAssets", json!([{"cursor":id}])),
        ] {
            assert!(request(method, &args).is_err());
        }
        Ok(())
    }

    #[test]
    fn program_event_queries_are_bounded_before_transport() -> Result<(), String> {
        let topic = "504158454552585f5745425f524551554553545f5631";
        for params in [
            json!([{"topic": topic, "from_sequence": 0, "limit": 1}]),
            json!([{"limit": PROGRAM_EVENTS_MAX_PAGE, "from_sequence": u64::MAX, "topic": "ab".repeat(64)}]),
            json!([{"topic": "00", "from_sequence": 40, "limit": 2}]),
        ] {
            assert_eq!(
                request("lx_getProgramEvents", &params)?,
                json!({"jsonrpc":"2.0","id":1,"method":"lx_getProgramEvents","params":params})
            );
        }
        let client = RpcClient::new("http://127.0.0.1:1/rpc", None)?;
        for params in [
            json!([]),
            json!({}),
            json!({"topic": topic, "from_sequence": 0, "limit": 1}),
            json!([{"topic": topic, "from_sequence": 0}]),
            json!([{"topic": topic, "limit": 1}]),
            json!([{"from_sequence": 0, "limit": 1}]),
            json!([{"topic": topic, "from_sequence": 0, "limit": 0}]),
            json!([{"topic": topic, "from_sequence": 0, "limit": PROGRAM_EVENTS_MAX_PAGE + 1}]),
            json!([{"topic": topic, "from_sequence": -1, "limit": 1}]),
            json!([{"topic": topic, "from_sequence": 1.5, "limit": 1}]),
            json!([{"topic": topic, "from_sequence": "1", "limit": 1}]),
            json!([{"topic": topic, "from_sequence": 0, "limit": "1"}]),
            json!([{"topic": "", "from_sequence": 0, "limit": 1}]),
            json!([{"topic": "ABCD", "from_sequence": 0, "limit": 1}]),
            json!([{"topic": "abc", "from_sequence": 0, "limit": 1}]),
            json!([{"topic": "+a", "from_sequence": 0, "limit": 1}]),
            json!([{"topic": "../", "from_sequence": 0, "limit": 1}]),
            json!([{"topic": 171, "from_sequence": 0, "limit": 1}]),
            json!([{"topic": "ab".repeat(65), "from_sequence": 0, "limit": 1}]),
            json!([{"topic": topic, "from_sequence": 0, "limit": 1, "extra": 1}]),
            json!([{"topic": topic, "from_sequence": 0, "limit": 1}, 1]),
            json!([topic, 0, 1]),
        ] {
            assert!(request("lx_getProgramEvents", &params).is_err(), "{params}");
            let refused = client
                .call("lx_getProgramEvents", &params)
                .err()
                .ok_or("malformed query reached transport")?;
            assert!(
                refused.starts_with("lx_getProgramEvents") || refused.starts_with("RPC parameters"),
                "{refused}"
            );
        }
        Ok(())
    }

    #[test]
    fn program_event_pages_decode_only_when_they_match_the_query() -> Result<(), String> {
        let topic = "504158454552585f5745425f524551554553545f5631";
        let query = json!([{"topic": topic, "from_sequence": 40, "limit": 2}]);
        let event = |sequence: u64, topic: &str| json!({"sequence": sequence, "program_id": "ab".repeat(32), "topic": topic, "data": "01"});
        let accepted = [
            json!({"events": [event(40, topic), event(41, topic)], "next_sequence": 42}),
            json!({"events": [event(41, topic)], "next_sequence": 44}),
            json!({"events": [], "next_sequence": 40}),
            json!({"events": [{"sequence": 43, "program_id": "ab".repeat(32), "topic": topic,
                "data": "ab".repeat(PROGRAM_EVENT_MAX_DATA_BYTES)}], "next_sequence": 44}),
        ];
        for page in &accepted {
            let response = json!({"jsonrpc":"2.0","id":1,"result":page});
            let decoded = decode_response("lx_getProgramEvents", &response)?;
            program_events_page(&query, &decoded)?;
            assert_eq!(&decoded, page);
        }
        for refused in [
            json!({"events": [event(40, topic), event(41, topic), event(42, topic)],
                "next_sequence": 43}),
            json!({"events": [], "next_sequence": 39}),
            json!({"events": [event(44, topic)], "next_sequence": 44}),
            json!({"events": [event(39, topic)], "next_sequence": 44}),
            json!({"events": [event(42, topic), event(41, topic)], "next_sequence": 44}),
            json!({"events": [event(41, topic), event(41, topic)], "next_sequence": 44}),
            json!({"events": [event(41, "00")], "next_sequence": 44}),
            json!({"events": [{"sequence": 41, "program_id": "00".repeat(32), "topic": topic,
                "data": "01"}], "next_sequence": 44}),
            json!({"events": [{"sequence": 41, "program_id": "AB".repeat(32), "topic": topic,
                "data": "01"}], "next_sequence": 44}),
            json!({"events": [{"sequence": 41, "program_id": "ab".repeat(32), "topic": topic,
                "data": "0"}], "next_sequence": 44}),
            json!({"events": [{"sequence": 41, "program_id": "ab".repeat(32), "topic": topic,
                "data": "ab".repeat(PROGRAM_EVENT_MAX_DATA_BYTES + 1)}], "next_sequence": 44}),
            json!({"events": [{"sequence": 41, "program_id": "ab".repeat(32), "topic": topic}],
                "next_sequence": 44}),
            json!({"events": [{"sequence": 41, "program_id": "ab".repeat(32), "topic": topic,
                "data": "01", "extra": true}], "next_sequence": 44}),
            json!({"events": [], "next_sequence": 44, "extra": true}),
            json!({"events": {}, "next_sequence": 44}),
            json!({"events": []}),
            json!({"events": [], "next_sequence": "44"}),
            json!([]),
        ] {
            assert!(program_events_page(&query, &refused).is_err(), "{refused}");
        }
        assert!(program_events_page(&json!([{"topic": topic}]), &accepted[0]).is_err());
        assert!(decode_response(
            "lx_getProgramEvents",
            &json!({"jsonrpc":"2.0","id":1,"result":[]})
        )
        .is_err());
        let unavailable = json!({"code":-32001,"message":"Read unavailable"});
        assert_eq!(
            serde_json::from_str::<Value>(
                &decode_response(
                    "lx_getProgramEvents",
                    &json!({"jsonrpc":"2.0","id":1,"error":unavailable})
                )
                .err()
                .ok_or("error lost")?
            )
            .map_err(|e| e.to_string())?,
            json!({"code":-32001,"message":"Read unavailable","data":null})
        );
        Ok(())
    }

    #[test]
    fn asset_pages_send_and_parse_the_published_cursor_contract() -> Result<(), String> {
        let cursor = [0xab; 32];
        let encoded = "ab".repeat(32);
        assert_eq!(list_assets_params(None, None), json!([]));
        assert_eq!(list_assets_params(Some(cursor), None), json!([encoded]));
        assert_eq!(
            list_assets_params(Some(cursor), Some(LIST_ASSETS_MAX_PAGE)),
            json!([encoded, LIST_ASSETS_MAX_PAGE])
        );
        assert_eq!(
            list_assets_params(None, Some(LIST_ASSETS_DEFAULT_PAGE)),
            json!([Value::Null, LIST_ASSETS_DEFAULT_PAGE])
        );
        for params in [
            list_assets_params(None, None),
            list_assets_params(Some(cursor), None),
            list_assets_params(Some(cursor), Some(1)),
            list_assets_params(Some(cursor), Some(LIST_ASSETS_MAX_PAGE)),
            list_assets_params(None, Some(LIST_ASSETS_DEFAULT_PAGE)),
        ] {
            assert_eq!(request("lx_listAssets", &params)?["params"], params);
        }
        let first = "11".repeat(32);
        let second = "22".repeat(32);
        let truncated = asset_page(
            vec![asset_value(&first), asset_value(&second)],
            json!(second),
        );
        let page =
            AssetListSnapshot::try_from(truncated.clone()).map_err(|error| format!("{error:?}"))?;
        assert_eq!(
            page.assets
                .iter()
                .map(|asset| asset.asset_id)
                .collect::<Vec<_>>(),
            [[0x11; 32], [0x22; 32]]
        );
        assert_eq!(page.next_cursor, Some([0x22; 32]));
        assert_eq!(page.into_value(), truncated);
        let last = AssetListSnapshot::try_from(asset_page(vec![asset_value(&first)], Value::Null))
            .map_err(|error| format!("{error:?}"))?;
        assert_eq!(last.next_cursor, None);
        assert_eq!(last.assets.len(), 1);
        for refused in [
            asset_page(
                vec![asset_value(&first), asset_value(&second)],
                json!(first),
            ),
            asset_page(vec![asset_value(&second), asset_value(&first)], Value::Null),
            asset_page(Vec::new(), json!(first)),
            asset_page(vec![asset_value(&first)], json!("2".repeat(63))),
            json!({
                "assets": [asset_value(&first)],
                "observed_head_sequence": "9",
                "state_root": "44".repeat(32),
                "verification": "authenticated_committed_snapshot"
            }),
        ] {
            assert!(AssetListSnapshot::try_from(refused).is_err());
        }
        Ok(())
    }

    #[test]
    fn subscriptions_require_authenticated_websocket_and_string_ack() -> Result<(), String> {
        let client = RpcClient::new("http://127.0.0.1:1/rpc", None)?;
        assert!(client.call("lx_subscribe", &json!(["receipts"])).is_err());
        assert!(client.call("lx_unsubscribe", &json!(["1"])).is_err());
        assert!(client
            .subscribe(&json!(["receipts"]), std::time::Duration::from_secs(1))
            .is_err());
        assert!(RpcClient::new("http://example.com/rpc", None).is_err());
        let response = json!({"jsonrpc":"2.0","id":1,"result":"1"});
        assert_eq!(decode_response("lx_subscribe", &response)?, "1");
        assert!(decode_response("lx_getReceipt", &response).is_err());
        for result in [json!(null), json!({}), json!(""), json!(1)] {
            assert!(decode_response(
                "lx_subscribe",
                &json!({"jsonrpc":"2.0","id":1,"result":result})
            )
            .is_err());
        }
        let cancelled = json!({"jsonrpc":"2.0","id":1,"result":true});
        assert_eq!(decode_response("lx_unsubscribe", &cancelled)?, true);
        for result in [json!(false), json!("true"), json!({}), json!(null)] {
            assert!(decode_response(
                "lx_unsubscribe",
                &json!({"jsonrpc":"2.0","id":1,"result":result})
            )
            .is_err());
        }
        let error = json!({"code":-32005,"message":"feed unavailable","data":{"reason":"native_feed_unavailable"}});
        assert_eq!(
            serde_json::from_str::<Value>(
                &decode_response(
                    "lx_subscribe",
                    &json!({"jsonrpc":"2.0","id":1,"error":error})
                )
                .err()
                .ok_or("error lost")?
            )
            .map_err(|e| e.to_string())?,
            error
        );
        Ok(())
    }

    #[test]
    fn remote_enumeration_error_is_preserved() -> Result<(), String> {
        let fields = json!({"code":-32005,"message":"DID enumeration unavailable","data":{"reason":"native_index_unavailable"}});
        let response = json!({"jsonrpc":"2.0","id":1,"error":fields});
        let error = decode_response("lx_getBalances", &response)
            .err()
            .ok_or("RPC error was hidden")?;
        assert_eq!(
            serde_json::from_str::<Value>(&error).map_err(|e| e.to_string())?,
            fields
        );
        Ok(())
    }

    #[test]
    fn response_validation_refuses_errors_and_unbound_results() -> Result<(), String> {
        let good = json!({"jsonrpc":"2.0","id":1,"result":{"state":"pending"}});
        assert_eq!(
            decode_response("lx_sendActivity", &good)?["state"],
            "pending"
        );
        for bad in [
            json!({"jsonrpc":"2.0","id":2,"result":{}}),
            json!({"jsonrpc":"2.0","id":1,"result":null}),
            json!({"jsonrpc":"2.0","id":1,"result":{},"error":{}}),
            json!({"jsonrpc":"2.0","id":1,"error":{"code":-32601,"message":"Method not found"}}),
            json!({"id":1,"result":{}}),
        ] {
            assert!(decode_response("lx_getBalances", &bad).is_err());
        }
        Ok(())
    }
}
