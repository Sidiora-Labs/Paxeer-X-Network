use layerx_client::{caps::{CapsDiscovery, CapsProgress}, evidence::{RootSelector, verification_label}, head::HeadTracker, read::{ReadContext, Requested}};
use sha2::{Digest, Sha256};
use super::{Config, Request, Response, SequencerAuthorization, VerificationLevel, refusal, success, hex_encode, fixed_hex};

#[derive(serde::Deserialize)]
#[serde(deny_unknown_fields)]
struct Selection { did: String, account_id: String, network_id: u32 }

pub(super) fn read(config: &Config, request: &Request) -> Response {
    use subtle::ConstantTimeEq;
    let Some(token) = &config.wallet_caps_token else { return refusal(503, "caps_not_configured", Some(5)); };
    let supplied = request.headers.get("authorization").and_then(|v| v.strip_prefix("Bearer ")).unwrap_or("");
    if !bool::from(supplied.as_bytes().ct_eq(token.as_bytes())) { return refusal(401, "unauthorized", None); }
    if request.method != "POST" { return refusal(405, "method_not_allowed", None); }
    if request.query.is_some() || request.body.len() > 1024 { return refusal(400, "invalid_caps_selection", None); }
    let Ok(selection) = serde_json::from_slice::<Selection>(&request.body) else { return refusal(400, "invalid_caps_selection", None); };
    let Ok(did) = layerx_types::ids::Did::new(selection.did.as_bytes()) else { return refusal(400, "invalid_did", None); };
    let Ok(account_id) = fixed_hex::<32>("account_id", &selection.account_id) else { return refusal(400, "invalid_account_id", None); };
    if selection.network_id != config.network_id || super::main_account(&selection.did).ok() != Some(account_id) {
        return refusal(403, "caps_account_refused", None);
    }
    let Ok((mut transport, handshake)) = connect(config) else { return refusal(503, "caps_unavailable", Some(5)); };
    let node = handshake.node();
    let context = ReadContext {
        interface_version: node.interface_version, correlation_id: 1,
        expected_protocol_version: node.protocol_version, expected_network_id: config.network_id,
        requested: Requested::new(VerificationLevel::STATE_PROVEN), head: HeadTracker::new(node).current(),
        sequencer_authorization: SequencerAuthorization::new(config.sequencer_id, node.authorised_sequencer_key, 1, u64::MAX),
        handshake_sequencer_key: node.authorised_sequencer_key, root_selector: RootSelector::Latest,
    };
    let mut digest = Sha256::new();
    digest.update(b"LXP/v1/did-id\0");
    digest.update((selection.did.len() as u16).to_be_bytes());
    digest.update(selection.did.as_bytes());
    let did_hash = digest.finalize().into();
    let Ok(mut discovery) = CapsDiscovery::begin(&mut transport, handshake.capabilities(), context, did_hash, 1_048_576, super::LNI_DEADLINE, None) else {
        return refusal(503, "caps_unavailable", Some(5));
    };
    let verified = loop {
        match discovery.advance() {
            CapsProgress::Incomplete { .. } => continue,
            CapsProgress::Complete(value) | CapsProgress::Empty(value) => break value,
            CapsProgress::Unavailable => return refusal(503, "caps_unavailable", Some(5)),
            CapsProgress::Refused(_) => return refusal(502, "caps_evidence_refused", None),
        }
    };
    let Some(account) = verified.all_accounts().get(&account_id) else { return refusal(403, "caps_account_refused", None); };
    if account.kind != 1 || account.name.as_slice() != format!("agent:{}:main", selection.did).as_bytes() || did.as_bytes() != selection.did.as_bytes() {
        return refusal(403, "caps_account_refused", None);
    }
    let Some(verification) = verification_label(verified.level()) else { return refusal(502, "caps_evidence_refused", None); };
    let budgets: Vec<_> = verified.budgets().iter().filter(|b| b.owner == account_id).map(|b| serde_json::json!({
        "id": hex_encode(&b.budget_id), "owner": hex_encode(&b.owner), "asset": hex_encode(&b.asset_id),
        "account": hex_encode(&b.budget_account), "source_account": b.source_account.map(|v| hex_encode(&v)),
        "limit": b.per_period_limit.to_string(), "configured_limit": b.configured_period_limit.to_string(),
        "spent": b.spent_this_period.to_string(), "remaining": b.remaining().to_string(),
        "carry_cap": b.carry_cap.to_string(), "carried": b.carried.to_string(),
        "period_start": b.period_start.to_string(), "period_length": b.period_length.to_string(),
        "expiry": b.expiry.to_string(), "revocation_sequence": b.revocation_sequence.to_string(),
        "closed": b.closed, "revoked": b.revoked
    })).collect();
    let grants: Vec<_> = verified.grants().iter().filter(|g| g.grant.from == account_id).map(|g| {
        let v = &g.grant;
        serde_json::json!({"id": hex_encode(&v.id), "owner": hex_encode(&v.from), "recipient": hex_encode(&v.recipient),
            "asset": hex_encode(&v.asset), "per_draw_maximum": v.per_draw_maximum.to_string(), "allowance": v.allowance.to_string(),
            "drawn_total": g.drawn_total.to_string(), "drawn_this_period": g.drawn_this_period.to_string(),
            "recurring": v.recurring, "window_length": v.window_length.to_string(), "window_start": g.window_start.to_string(),
            "expiration": v.expiration.to_string(), "revocation_sequence": v.revocation_sequence.to_string(),
            "revoked_at_sequence": g.revoked_at_sequence.to_string(), "revoked": g.revoked, "invoice_settled": g.invoice_settled})
    }).collect();
    let empty = budgets.is_empty() && grants.is_empty();
    let value = serde_json::json!({"state": if empty { "empty" } else { "ready" }, "did": selection.did,
        "account_id": selection.account_id, "network_id": config.network_id, "budgets": budgets, "grants": grants,
        "observation": {"verification": verification, "state_root": hex_encode(&verified.state_root()),
            "sequence": verified.freshness().global_sequence.to_string(), "observed_head": verified.freshness().observed_head_sequence.to_string(), "batch": verified.freshness().batch_number.to_string()}});
    if value.to_string().len() > 4 * 1024 * 1024 { return refusal(503, "caps_response_bound", None); }
    success(&value)
}

fn connect(config: &Config) -> Result<(super::Uds, super::Handshake), String> {
    static GATE: std::sync::OnceLock<super::ConnectionGate> = std::sync::OnceLock::new();
    let gate = GATE.get_or_init(|| super::ConnectionGate::new(4));
    let mut transport = super::Uds::connect(&config.lni_socket, gate, super::lni_limits()).map_err(|_| "caps transport unavailable")?;
    let mut context = super::handshake_config(config);
    context.built_interface_version = super::Version::V1_8;
    let handshake = super::perform(&mut transport, &context, None).map_err(|_| "caps handshake unavailable")?;
    Ok((transport, handshake))
}
