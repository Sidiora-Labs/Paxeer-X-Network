//! Typed conversion from the version 1 Agent operation envelope to the shared daemon owner.
//!
//! Every arm decodes the operation `request` strictly (unknown fields refused, canonical
//! decimal integers, 64-digit lowercase hex identifiers) into the exact arguments of the
//! existing `HumanOperations` method and calls it on the one `SharedAgentOwner`, which takes
//! its own lock. Operations without an existing owner method are deliberately left out of the
//! match: they are not answered with any substitute refusal.

use layerx_agent_api::error::{ErrorClass, Level, RequestId, Retriability, VerificationStatus};
use serde::{Deserialize, Serialize};
use serde_json::{Map, Value};

use crate::agent_rpc::Rejection;
use crate::agent_rpc_adapters as adapters;
use crate::agent_rpc_peer::RpcOwnerContext;
use crate::agent_rpc_wire::*;
use crate::human::{
    HumanOperationError, HumanOperations, HumanPeer, HumanPrepare, HumanResponse, HumanSubmit,
};
use crate::human_runtime::{prepare_digest, submit_digest, HumanAuthorityBoundary, SharedAgentOwner};
use crate::session_control::OperationPermit;
use crate::tenant::Operation;

pub struct DispatchContext {
    pub request_id: RequestId,
    pub idempotency_key: Option<[u8; 32]>,
    pub peer: HumanPeer,
}

/// `verification` is `Some` only when the decoded owner payload carries a verification level;
/// payloads without one (head, approval records) report `None` and no level is invented.
pub(crate) struct Dispatched {
    pub value: Value,
    pub verification: Option<VerificationStatus>,
}

const fn rejection(class: ErrorClass, request_id: RequestId, reason: &'static str) -> Rejection {
    Rejection {
        class,
        retriability: Retriability::Terminal,
        request_id,
        reason,
    }
}

pub(crate) fn malformed(request_id: RequestId) -> Rejection {
    rejection(ErrorClass::ProtocolIncompatibility, request_id, "envelope.malformed")
}

pub(crate) fn noncanonical(request_id: RequestId) -> Rejection {
    rejection(
        ErrorClass::ProtocolIncompatibility,
        request_id,
        "envelope.noncanonical_integer",
    )
}

pub(crate) fn decimal_u64(text: &str, request_id: RequestId) -> Result<u64, Rejection> {
    decimal_u128(text, request_id)?
        .try_into()
        .map_err(|_| noncanonical(request_id))
}

pub(crate) fn decimal_u128(text: &str, request_id: RequestId) -> Result<u128, Rejection> {
    let canonical = !text.is_empty()
        && text.len() <= 39
        && text.bytes().all(|byte| byte.is_ascii_digit())
        && (text == "0" || !text.starts_with('0'));
    if !canonical {
        return Err(noncanonical(request_id));
    }
    text.parse::<u128>().map_err(|_| noncanonical(request_id))
}

pub(crate) fn hex32(text: &str, request_id: RequestId) -> Result<[u8; 32], Rejection> {
    let bytes = text.as_bytes();
    if bytes.len() != 64 || !bytes.iter().all(|byte| matches!(byte, b'0'..=b'9' | b'a'..=b'f')) {
        return Err(malformed(request_id));
    }
    let mut out = [0_u8; 32];
    for (index, pair) in bytes.chunks_exact(2).enumerate() {
        let text = std::str::from_utf8(pair).map_err(|_| malformed(request_id))?;
        out[index] = u8::from_str_radix(text, 16).map_err(|_| malformed(request_id))?;
    }
    Ok(out)
}

pub(crate) fn hex_bytes(text: &str, request_id: RequestId) -> Result<Vec<u8>, Rejection> {
    let bytes = text.as_bytes();
    if bytes.len() % 2 != 0 || !bytes.iter().all(|byte| matches!(byte, b'0'..=b'9' | b'a'..=b'f')) {
        return Err(malformed(request_id));
    }
    bytes
        .chunks_exact(2)
        .map(|pair| {
            std::str::from_utf8(pair)
                .ok()
                .and_then(|text| u8::from_str_radix(text, 16).ok())
                .ok_or_else(|| malformed(request_id))
        })
        .collect()
}

pub(crate) fn lower_hex(bytes: &[u8]) -> String {
    const DIGITS: &[u8; 16] = b"0123456789abcdef";
    let mut out = String::with_capacity(bytes.len() * 2);
    for byte in bytes {
        out.push(char::from(DIGITS[usize::from(byte >> 4)]));
        out.push(char::from(DIGITS[usize::from(byte & 0x0f)]));
    }
    out
}

pub(crate) fn decode<T: for<'de> Deserialize<'de>>(
    request: &Map<String, Value>,
    request_id: RequestId,
) -> Result<T, Rejection> {
    serde_json::from_value(Value::Object(request.clone())).map_err(|error| {
        if error.to_string().starts_with("unknown field") {
            rejection(
                ErrorClass::ProtocolIncompatibility,
                request_id,
                "envelope.unknown_field",
            )
        } else {
            malformed(request_id)
        }
    })
}

pub(crate) fn native_variant(
    request: &Map<String, Value>, id: RequestId,
) -> Result<bool, Rejection> {
    match request.get("variant") {
        None => Ok(false),
        Some(Value::String(variant)) if variant == "native_v1" => Ok(true),
        Some(_) => Err(malformed(id)),
    }
}

pub(crate) fn native_prepare_digest(
    request: &layerx_agent_api::identity::NativePrepareRequestV1,
) -> Result<[u8; 32], serde_json::Error> {
    use sha2::{Digest, Sha256};
    Ok(Sha256::new()
        .chain_update(b"LXP/agent/native-prepare/v1\0")
        .chain_update(serde_json::to_vec(&request.canonical())?)
        .finalize()
        .into())
}

pub(crate) fn native_approval_digest(
    request: &layerx_agent_api::identity::NativeApprovalDecisionV1,
    grant: bool,
) -> Result<[u8; 32], serde_json::Error> {
    use sha2::{Digest, Sha256};
    let operation = if grant { Operation::ApprovalApprove } else { Operation::ApprovalReject };
    Ok(Sha256::new()
        .chain_update(b"LXP/agent/request/v1\0")
        .chain_update(operation.name().as_bytes())
        .chain_update([0_u8])
        .chain_update(serde_json::to_vec(&request.canonical())?)
        .finalize()
        .into())
}

pub(crate) fn mutation_key(ctx: &DispatchContext) -> Result<[u8; 32], Rejection> {
    ctx.idempotency_key.ok_or_else(|| {
        rejection(
            ErrorClass::IdempotencyConflict,
            ctx.request_id,
            "envelope.idempotency_key",
        )
    })
}

fn owner_error(request_id: RequestId, error: HumanOperationError) -> Rejection {
    match error {
        HumanOperationError::Refused => {
            rejection(ErrorClass::PolicyRefusal, request_id, "owner.refused")
        }
        HumanOperationError::CapabilityRefused(dimension) => rejection(
            ErrorClass::CapabilityRefusal,
            request_id,
            match dimension {
                crate::capability::Dimension::Expiry => "capability.expiry",
                crate::capability::Dimension::ActivityType => "capability.activity_type",
                crate::capability::Dimension::Counterparty => "capability.counterparty",
                crate::capability::Dimension::Asset => "capability.asset",
                crate::capability::Dimension::Amount => "capability.amount",
                crate::capability::Dimension::Rate => "capability.rate",
                crate::capability::Dimension::Purpose => "capability.purpose",
            },
        ),

        HumanOperationError::Typed(refusal) => {
            rejection(refusal.class(), request_id, refusal.reason())
        }
        HumanOperationError::Unavailable => Rejection {
            class: ErrorClass::UnavailableCapability,
            retriability: Retriability::Retriable,
            request_id,
            reason: "owner.unavailable",
        },
    }
}

const MAX_TEXT: usize = 255;
const MAX_BYTES: usize = 1_048_576;
const MAX_EVIDENCE: usize = 64;
const MAX_TRANSITIONS: usize = 64;
const MAX_APPROVALS: usize = 100;
const MAX_DISCLOSED: usize = 64;

/// Bounded reader over the owner payload (the bytes after the Human frame magic and status),
/// mirroring the production Human client reader: big-endian integers, u32-length-prefixed
/// non-empty byte strings, UTF-8 text of at most 255 bytes, and an exact end.
pub(crate) struct Reader<'a> {
    pub(crate) bytes: &'a [u8],
    pub(crate) offset: usize,
}

impl<'a> Reader<'a> {
    fn take(&mut self, length: usize) -> Option<&'a [u8]> {
        let end = self.offset.checked_add(length)?;
        let value = self.bytes.get(self.offset..end)?;
        self.offset = end;
        Some(value)
    }
    fn fixed<const N: usize>(&mut self) -> Option<[u8; N]> {
        self.take(N)?.try_into().ok()
    }
    fn u8(&mut self) -> Option<u8> {
        Some(self.take(1)?[0])
    }
    fn u16(&mut self) -> Option<u16> {
        Some(u16::from_be_bytes(self.fixed()?))
    }
    fn u32(&mut self) -> Option<u32> {
        Some(u32::from_be_bytes(self.fixed()?))
    }
    fn i32(&mut self) -> Option<i32> {
        Some(i32::from_be_bytes(self.fixed()?))
    }
    fn u64(&mut self) -> Option<u64> {
        Some(u64::from_be_bytes(self.fixed()?))
    }
    fn u128(&mut self) -> Option<u128> {
        Some(u128::from_be_bytes(self.fixed()?))
    }
    fn bytes(&mut self) -> Option<&'a [u8]> {
        let length = usize::try_from(self.u32()?).ok()?;
        if length == 0 || length > MAX_BYTES {
            return None;
        }
        self.take(length)
    }
    fn text(&mut self) -> Option<String> {
        let bytes = self.bytes()?;
        if bytes.len() > MAX_TEXT {
            return None;
        }
        String::from_utf8(bytes.to_vec()).ok()
    }
    pub(crate) fn finish(&self) -> Option<()> {
        (self.offset == self.bytes.len()).then_some(())
    }
}

fn dec(value: impl ToString) -> Value {
    Value::String(value.to_string())
}

fn hexv(bytes: &[u8]) -> Value {
    Value::String(lower_hex(bytes))
}

fn level(value: u8) -> Option<Level> {
    Some(match value {
        0 => Level::Unverified,
        1 => Level::SequencerSigned,
        2 => Level::BatchIncluded,
        3 => Level::StateProven,
        4 => Level::CheckpointFinalised,
        5 => Level::SettlementAnchored,
        _ => return None,
    })
}

pub(crate) fn level_name(value: Level) -> &'static str {
    match value {
        Level::Unverified => "Unverified",
        Level::SequencerSigned => "SequencerSigned",
        Level::BatchIncluded => "BatchIncluded",
        Level::StateProven => "StateProven",
        Level::CheckpointFinalised => "CheckpointFinalised",
        Level::SettlementAnchored => "SettlementAnchored",
    }
}

fn state_name(value: u8) -> Option<&'static str> {
    Some(match value {
        0 => "Prepared",
        1 => "Signed",
        2 => "Queued",
        3 => "Submitted",
        4 => "Acknowledged",
        5 => "Unknown",
        6 => "Executed",
        7 => "Failed",
        8 => "Expired",
        _ => return None,
    })
}

fn decode_head(reader: &mut Reader<'_>) -> Option<(Value, Option<Level>)> {
    let mut out = Map::new();
    out.insert("chain_sequence".into(), dec(reader.u64()?));
    out.insert("sealed_batch".into(), dec(reader.u64()?));
    out.insert("finalised_checkpoint".into(), hexv(&reader.fixed::<32>()?));
    Some((Value::Object(out), None))
}

fn decode_account_state(
    reader: &mut Reader<'_>,
    account_id: [u8; 32],
) -> Option<(Value, Option<Level>)> {
    let observed = reader.fixed::<32>()?;
    let achieved = level(reader.u8()?)?;
    let value = reader.bytes()?;
    let proof = reader.bytes()?;
    let sequence = reader.u64()?;
    if observed != account_id || achieved < Level::StateProven || sequence == 0 {
        return None;
    }
    let mut out = Map::new();
    out.insert("account_id".into(), hexv(&observed));
    out.insert("canonical_value".into(), hexv(value));
    out.insert("proof".into(), hexv(proof));
    out.insert("sequence".into(), dec(sequence));
    Some((Value::Object(out), Some(achieved)))
}

fn decode_balance(reader: &mut Reader<'_>) -> Option<(Value, Option<Level>)> {
    let mut out = Map::new();
    let account = reader.fixed::<32>()?;
    let asset = reader.fixed::<32>()?;
    let currency = reader.text()?;
    let observed_at = reader.text()?;
    out.insert("age_seconds".into(), dec(reader.u64()?));
    out.insert("amount".into(), dec(reader.u128()?));
    let achieved = level(reader.u8()?)?;
    out.insert("global_sequence".into(), dec(reader.u64()?));
    out.insert("batch_number".into(), dec(reader.u64()?));
    out.insert("observed_head_sequence".into(), dec(reader.u64()?));
    out.insert("observed_checkpoint".into(), hexv(&reader.fixed::<32>()?));
    out.insert("canonical_bytes".into(), hexv(reader.bytes()?));
    out.insert("proof_material".into(), hexv(reader.bytes()?));
    if achieved < Level::CheckpointFinalised
        || account == [0; 32]
        || asset == [0; 32]
        || currency.is_empty()
        || observed_at.is_empty()
    {
        return None;
    }
    out.insert("account".into(), hexv(&account));
    out.insert("asset".into(), hexv(&asset));
    out.insert("currency".into(), Value::String(currency));
    out.insert("observed_at".into(), Value::String(observed_at));
    Some((Value::Object(out), Some(achieved)))
}

fn decode_tracked(reader: &mut Reader<'_>) -> Option<(Value, Option<Level>)> {
    let mut out = Map::new();
    out.insert("submission_ref".into(), Value::String(reader.text()?));
    let state = reader.u8()?;
    out.insert("state".into(), Value::String(state_name(state)?.into()));
    match state {
        6 => {
            out.insert("receipt_ref".into(), Value::String(reader.text()?));
        }
        7 => {
            out.insert("result_code".into(), dec(reader.i32()?));
        }
        _ => {}
    }
    let achieved = level(reader.u8()?)?;
    out.insert("verification_level".into(), Value::String(level_name(achieved).into()));
    let evidence_count = usize::from(reader.u8()?);
    if evidence_count > MAX_EVIDENCE {
        return None;
    }
    let mut evidence = Vec::with_capacity(evidence_count);
    for _ in 0..evidence_count {
        let mut item = Map::new();
        item.insert("kind".into(), Value::String(reader.text()?));
        item.insert("digest".into(), hexv(&reader.fixed::<32>()?));
        evidence.push(Value::Object(item));
    }
    out.insert("evidence".into(), Value::Array(evidence));
    let transition_count = usize::from(reader.u8()?);
    if transition_count > MAX_TRANSITIONS {
        return None;
    }
    let mut transitions = Vec::with_capacity(transition_count);
    for _ in 0..transition_count {
        let from = reader.u8()?;
        let to = reader.u8()?;
        if matches!(from, 6 | 7) || matches!(to, 6 | 7) {
            return None;
        }
        let mut item = Map::new();
        item.insert("from".into(), Value::String(state_name(from)?.into()));
        item.insert("to".into(), Value::String(state_name(to)?.into()));
        item.insert("cause".into(), Value::String(reader.text()?));
        item.insert("at".into(), dec(reader.u64()?));
        transitions.push(Value::Object(item));
    }
    out.insert("transitions".into(), Value::Array(transitions));
    Some((Value::Object(out), Some(achieved)))
}

pub(crate) fn decode_observation(reader: &mut Reader<'_>) -> Option<(Value, Option<Level>)> {
    let activity_id = reader.fixed::<32>()?;
    if activity_id == [0; 32] {
        return None;
    }
    let (submission, achieved) = decode_tracked(reader)?;
    let executed = submission.get("state") == Some(&Value::String("Executed".into()));
    let receipt = match reader.u8()? {
        0 => None,
        1 => Some(decode_receipt(reader)?.0),
        _ => return None,
    };
    if executed != receipt.is_some() {
        return None;
    }
    let mut out = Map::new();
    out.insert("activity_id".into(), hexv(&activity_id));
    out.insert("submission".into(), submission);
    out.insert("receipt".into(), receipt.unwrap_or(Value::Null));
    Some((Value::Object(out), achieved))
}

fn decode_receipt(reader: &mut Reader<'_>) -> Option<(Value, Option<Level>)> {
    let mut out = Map::new();
    out.insert("canonical_bytes".into(), hexv(reader.bytes()?));
    let mut batch = Map::new();
    for name in [
        "batch_id",
        "asset",
        "previous_state_root",
        "resulting_state_root",
        "sequencer_public_key",
    ] {
        batch.insert(name.into(), hexv(&reader.fixed::<32>()?));
    }
    out.insert("authorised_batch".into(), Value::Object(batch));
    let achieved = match reader.u8()? {
        0 => return None,
        value => level(value)?,
    };
    out.insert("verification_level".into(), Value::String(level_name(achieved).into()));
    Some((Value::Object(out), Some(achieved)))
}

fn decode_receipt_lookup(reader: &mut Reader<'_>) -> Option<(Value, Option<Level>)> {
    match reader.u8()? {
        0 => {
            let mut out = Map::new();
            out.insert("found".into(), Value::Bool(false));
            Some((Value::Object(out), None))
        }
        1 => {
            let (receipt, achieved) = decode_receipt(reader)?;
            let mut out = Map::new();
            out.insert("found".into(), Value::Bool(true));
            out.insert("receipt".into(), receipt);
            Some((Value::Object(out), achieved))
        }
        _ => None,
    }
}

/// Mirrors the client prepare reader. The client additionally re-binds the disclosure against
/// its activity registry; that registry check is not repeated here.
pub(crate) fn decode_preparation(reader: &mut Reader<'_>) -> Option<(Value, Option<Level>)> {
    let mut out = Map::new();
    out.insert("preparation_ref".into(), Value::String(reader.text()?));
    out.insert("unsigned_canonical_bytes".into(), hexv(reader.bytes()?));
    out.insert("signing_preimage".into(), hexv(reader.bytes()?));
    out.insert("activity_type".into(), dec(reader.u32()?));
    out.insert("actor".into(), Value::String(reader.text()?));
    out.insert("authority".into(), Value::String(reader.text()?));
    out.insert("account_sequence".into(), dec(reader.u64()?));
    out.insert("not_before".into(), dec(reader.u64()?));
    out.insert("not_after".into(), dec(reader.u64()?));
    out.insert("fee_limit".into(), dec(reader.u128()?));
    out.insert("payload".into(), hexv(reader.bytes()?));
    out.insert("payload_hash".into(), hexv(&reader.fixed::<32>()?));
    out.insert("idempotency_key".into(), hexv(&reader.fixed::<32>()?));
    Some((Value::Object(out), None))
}

pub(crate) fn decode_decision(reader: &mut Reader<'_>) -> Option<(Value, Option<Level>)> {
    let outcome = reader.u8()?;
    let submission_ref = match reader.u8()? {
        0 => None,
        1 => Some(reader.fixed::<32>()?),
        _ => return None,
    };
    let winning = match reader.u8()? {
        0 => None,
        1 => Some(reader.u8()?),
        _ => return None,
    };
    let already = matches!(outcome, 4 | 5);
    let effective = if already { winning? } else { outcome };
    let status = match effective {
        0 => "Approved",
        1 => "Rejected",
        2 => "Expired",
        3 => "Defective",
        _ => return None,
    };
    let mut out = Map::new();
    out.insert("status".into(), Value::String(status.into()));
    out.insert(
        "submission_ref".into(),
        if effective == 0 {
            submission_ref.map_or(Value::Null, |reference| hexv(&reference))
        } else {
            Value::Null
        },
    );
    out.insert(
        "resolution".into(),
        Value::String(if already { "AlreadyDecided" } else { "Applied" }.into()),
    );
    Some((Value::Object(out), None))
}

fn decode_approval(reader: &mut Reader<'_>) -> Option<Value> {
    let mut out = Map::new();
    out.insert("approval_id".into(), hexv(&reader.fixed::<32>()?));
    out.insert("canonical_digest".into(), hexv(&reader.fixed::<32>()?));
    out.insert("activity_type".into(), dec(reader.u16()?));
    out.insert("actor".into(), Value::String(reader.text()?));
    out.insert("authority".into(), Value::String(reader.text()?));
    let counterpart_count = usize::from(reader.u16()?);
    if counterpart_count > MAX_DISCLOSED {
        return None;
    }
    let mut counterparties = Vec::with_capacity(counterpart_count);
    for _ in 0..counterpart_count {
        counterparties.push(Value::String(reader.text()?));
    }
    out.insert("counterparties".into(), Value::Array(counterparties));
    let amount_count = usize::from(reader.u16()?);
    if amount_count > MAX_DISCLOSED {
        return None;
    }
    let mut amounts = Vec::with_capacity(amount_count);
    for _ in 0..amount_count {
        let mut item = Map::new();
        item.insert("counterparty".into(), Value::String(reader.text()?));
        item.insert("amount".into(), dec(reader.u128()?));
        amounts.push(Value::Object(item));
    }
    out.insert("amounts".into(), Value::Array(amounts));
    out.insert("asset".into(), Value::String(reader.text()?));
    out.insert("fee_limit".into(), dec(reader.u128()?));
    out.insert("expiry".into(), dec(reader.u64()?));
    out.insert("idempotency_key".into(), Value::String(reader.text()?));
    out.insert("canonical_bytes_digest".into(), hexv(&reader.fixed::<32>()?));
    out.insert("hold_reason_code".into(), Value::String(reader.text()?));
    out.insert("hold_reason".into(), Value::String(reader.text()?));
    out.insert("created_at_sequence".into(), dec(reader.u64()?));
    out.insert("expires_at_sequence".into(), dec(reader.u64()?));
    let state = match reader.u8()? {
        0 => "AwaitingApproval",
        1 => {
            out.insert("submission_ref".into(), hexv(&reader.fixed::<32>()?));
            "Approved"
        }
        2 => "Rejected",
        3 => "Expired",
        4 => "Defective",
        _ => return None,
    };
    out.insert("state".into(), Value::String(state.into()));
    Some(Value::Object(out))
}

fn decode_approval_list(reader: &mut Reader<'_>) -> Option<(Value, Option<Level>)> {
    let count = usize::from(reader.u8()?);
    if count > MAX_APPROVALS {
        return None;
    }
    let mut approvals = Vec::with_capacity(count);
    for _ in 0..count {
        approvals.push(decode_approval(reader)?);
    }
    let next_cursor = match reader.u8()? {
        0 => Value::Null,
        1 => hexv(&reader.fixed::<32>()?),
        _ => return None,
    };
    let mut out = Map::new();
    out.insert("approvals".into(), Value::Array(approvals));
    out.insert("next_cursor".into(), next_cursor);
    Some((Value::Object(out), None))
}

fn decode_approval_get(reader: &mut Reader<'_>) -> Option<(Value, Option<Level>)> {
    Some((decode_approval(reader)?, None))
}

/// Decodes the owner payload with the operation's bounded decoder. A payload that does not
/// decode completely is an owner fault, never a partial value.
pub(crate) fn dispatched(
    request_id: RequestId,
    response: Result<HumanResponse, HumanOperationError>,
    decoder: impl FnOnce(&mut Reader<'_>) -> Option<(Value, Option<Level>)>,
) -> Result<Dispatched, Rejection> {
    let response = response.map_err(|error| owner_error(request_id, error))?;
    let mut reader = Reader {
        bytes: response.bytes(),
        offset: 0,
    };
    let decoded = decoder(&mut reader).filter(|_| reader.finish().is_some());
    let (value, achieved) = decoded.ok_or_else(|| {
        rejection(ErrorClass::InternalFault, request_id, "owner.response_malformed")
    })?;
    Ok(Dispatched {
        value,
        verification: achieved.map(VerificationStatus::Achieved),
    })
}

pub(crate) fn dispatched_native<W, T>(
    request_id: RequestId,
    response: Result<HumanResponse, HumanOperationError>,
    convert: impl FnOnce(W, RequestId) -> Result<T, Rejection>,
) -> Result<Dispatched, Rejection>
where
    W: serde::de::DeserializeOwned,
    T: Canonical,
{
    let response = response.map_err(|error| owner_error(request_id, error))?;
    let refused = || rejection(ErrorClass::InternalFault, request_id, "owner.response_malformed");
    if response.bytes().len() > MAX_BYTES {
        return Err(refused());
    }
    let wire = serde_json::from_slice::<W>(response.bytes()).map_err(|_| refused())?;
    let value = convert(wire, request_id).map_err(|_| refused())?.canonical();
    Ok(Dispatched { value, verification: None })
}

#[derive(Deserialize, Serialize)]
#[serde(deny_unknown_fields)]
struct ReadAccountRequest {
    #[serde(default, skip_serializing)]
    tenant: Option<String>,
    #[serde(default, skip_serializing)]
    agent: Option<String>,
    account_id: String,
}

#[derive(Deserialize, Serialize)]
#[serde(deny_unknown_fields)]
struct CoordinatesOnly {
    #[serde(default, skip_serializing)]
    tenant: Option<String>,
    #[serde(default, skip_serializing)]
    agent: Option<String>,
}

#[derive(Deserialize, Serialize)]
#[serde(deny_unknown_fields)]
struct TrackRequest {
    #[serde(default, skip_serializing)]
    tenant: Option<String>,
    #[serde(default, skip_serializing)]
    agent: Option<String>,
    submission_ref: String,
}

#[derive(Deserialize, Serialize)]
#[serde(deny_unknown_fields)]
struct ApprovalListRequest {
    #[serde(default, skip_serializing)]
    tenant: Option<String>,
    #[serde(default, skip_serializing)]
    agent: Option<String>,
    current_sequence: String,
    cursor: Option<String>,
    limit: String,
}

#[derive(Deserialize, Serialize)]
#[serde(deny_unknown_fields)]
struct ApprovalGetRequest {
    #[serde(default, skip_serializing)]
    tenant: Option<String>,
    #[serde(default, skip_serializing)]
    agent: Option<String>,
    approval_id: String,
    current_sequence: String,
}

#[derive(Deserialize, Serialize)]
#[serde(deny_unknown_fields)]
struct ActivityLookupRequest {
    #[serde(default, skip_serializing)]
    tenant: Option<String>,
    #[serde(default, skip_serializing)]
    agent: Option<String>,
    idempotency_key: String,
    expected_activity_id: String,
}

#[derive(Deserialize, Serialize)]
#[serde(deny_unknown_fields)]
pub(crate) struct ApprovalDecisionRequest {
    #[serde(default, skip_serializing)]
    pub(crate) tenant: Option<String>,
    #[serde(default, skip_serializing)]
    pub(crate) agent: Option<String>,
    pub(crate) approval_id: String,
    pub(crate) held_digest: String,
    pub(crate) current_sequence: String,
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
pub(crate) struct PrepareRequest {
    #[serde(default)]
    tenant: Option<String>,
    #[serde(default)]
    agent: Option<String>,
    activity_type: String,
    actor: String,
    authority: String,
    account_sequence: String,
    not_before: String,
    not_after: String,
    idempotency_key: String,
    fee_limit: String,
    payload: String,
    payload_hash: String,

    #[serde(default)]
    capability_id: Option<String>,
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
pub(crate) struct SubmitRequest {
    #[serde(default)]
    tenant: Option<String>,
    #[serde(default)]
    agent: Option<String>,
    preparation_ref: String,
    signature: String,
    signer_public_key: String,
    approval_release_ref: Option<String>,
}

pub(crate) fn human_prepare(
    request: PrepareRequest,
    request_id: RequestId,
) -> Result<HumanPrepare, Rejection> {
    let _ = (request.tenant, request.agent);
    Ok(HumanPrepare {
        activity_type: u32::try_from(decimal_u64(&request.activity_type, request_id)?)
            .map_err(|_| noncanonical(request_id))?,
        actor: request.actor,
        authority: request.authority,
        account_sequence: decimal_u64(&request.account_sequence, request_id)?,
        not_before: decimal_u64(&request.not_before, request_id)?,
        not_after: decimal_u64(&request.not_after, request_id)?,
        idempotency_key: request.idempotency_key,
        fee_limit: decimal_u128(&request.fee_limit, request_id)?,
        payload: hex_bytes(&request.payload, request_id)?,
        payload_hash: hex32(&request.payload_hash, request_id)?,

        capability_id: request
            .capability_id
            .map(|text| hex32(&text, request_id))
            .transpose()?,
    })
}

pub(crate) fn human_submit(request: SubmitRequest, request_id: RequestId) -> Result<HumanSubmit, Rejection> {
    let _ = (request.tenant, request.agent);
    Ok(HumanSubmit {
        preparation_ref: request.preparation_ref,
        signature: hex_bytes(&request.signature, request_id)?,
        signer_public_key: hex32(&request.signer_public_key, request_id)?,
        approval_release_ref: request
            .approval_release_ref
            .map(|text| hex32(&text, request_id))
            .transpose()?,
    })
}

fn named<T: Serialize>(
    operation: Operation,
    typed: &T,
    request_id: RequestId,
) -> Result<Vec<u8>, Rejection> {
    let mut bytes = operation.name().as_bytes().to_vec();
    bytes.push(0);
    bytes.extend(serde_json::to_vec(typed).map_err(|_| malformed(request_id))?);
    Ok(bytes)
}

/// Digest bytes of a mutating operation decoded through its `agent_rpc_wire` struct: the same
/// `decode_wire` + `into_request` the adapters use, then `named` over the canonical JSON of the
/// converted agent-api (or program) request.
fn wire_bytes<W: serde::de::DeserializeOwned, T: Canonical>(
    operation: Operation,
    request: &Map<String, Value>,
    id: RequestId,
    into_request: fn(W, RequestId) -> Result<T, Rejection>,
) -> Result<Vec<u8>, Rejection> {
    named(operation, &into_request(decode_wire::<W>(request, id)?, id)?.canonical(), id)
}

/// Canonical bytes of the strictly decoded owner typed request, used by `agent_rpc` as the
/// `idempotency::Store::execute` request bytes (the store applies the `LXP/agent/request/v1`
/// domain). Prepare and submit use the owner's own journey digest, so the existing Human
/// prepare and submit digests are unchanged. Every other matched operation uses the operation name,
/// one NUL byte and the fixed-order JSON of the typed request after canonical validation
/// (coordinate fields excluded). Decoding rejects non-canonical decimals and non-lowercase hex,
/// so two requests with the same meaning always produce the same bytes. `None` for an
/// operation without a dispatch arm.
///
/// # Errors
/// Returns a [`Rejection`] when the request does not decode into the owner typed request.
pub(crate) fn canonical_request_bytes(
    operation: Operation,
    request: &Map<String, Value>,
    request_id: RequestId,
) -> Result<Option<Vec<u8>>, Rejection> {
    let id = request_id;
    Ok(Some(match operation {
        Operation::ReadAccount => {
            named(operation, &decode::<ReadAccountRequest>(request, id)?, id)?
        }
        Operation::ReadBalance | Operation::ReadCheckpoint => {
            named(operation, &decode::<CoordinatesOnly>(request, id)?, id)?
        }
        Operation::Track => named(operation, &decode::<TrackRequest>(request, id)?, id)?,
        Operation::ApprovalList => {
            if native_variant(request, id)? {
                wire_bytes::<NativeApprovalListV1Wire, _>(operation, request, id, NativeApprovalListV1Wire::into_request)?
            } else {
                named(operation, &decode::<ApprovalListRequest>(request, id)?, id)?
            }
        }
        Operation::ApprovalGet => {
            if native_variant(request, id)? {
                wire_bytes::<NativeApprovalGetV1Wire, _>(operation, request, id, NativeApprovalGetV1Wire::into_request)?
            } else {
                named(operation, &decode::<ApprovalGetRequest>(request, id)?, id)?
            }
        }
        Operation::ProgramReceipt => {
            named(operation, &decode::<ActivityLookupRequest>(request, id)?, id)?
        }
        Operation::ApprovalApprove | Operation::ApprovalReject => {
            if native_variant(request, id)? {
                wire_bytes::<NativeApprovalDecisionV1Wire, _>(operation, request, id, NativeApprovalDecisionV1Wire::into_request)?
            } else {
                named(operation, &decode::<ApprovalDecisionRequest>(request, id)?, id)?
            }
        }
        Operation::Prepare => {
            if native_variant(request, id)? {
                let typed = decode_wire::<NativePrepareV1Wire>(request, id)?.into_request(id)?;
                return native_prepare_digest(&typed).map(|digest| Some(digest.to_vec()))
                    .map_err(|_| malformed(id));
            }
            let typed = human_prepare(decode(request, id)?, id)?;
            crate::capability::binding::prepare_body_digest(
                prepare_digest(&typed),
                typed.capability_id.as_ref(),
            )
            .to_vec()
        }
        Operation::Submit => submit_digest(&human_submit(decode(request, id)?, id)?).to_vec(),
        Operation::BudgetCreate => {
            let wire = decode_wire::<BudgetCreateWire>(request, id)?;
            let suffix = budget_create_purpose_suffix(wire.purpose());
            let mut bytes = named(operation, &wire.into_request(id)?.canonical(), id)?;
            bytes.extend(suffix);
            bytes
        }
        Operation::BudgetFund => wire_bytes::<BudgetFundWire, _>(operation, request, id, BudgetFundWire::into_request)?,
        Operation::BudgetRevoke => wire_bytes::<BudgetTargetWire, _>(operation, request, id, BudgetTargetWire::into_request)?,
        Operation::CapabilityCreate => wire_bytes::<CapabilityCreateWire, _>(operation, request, id, CapabilityCreateWire::into_request)?,
        Operation::CapabilityAttenuate => wire_bytes::<CapabilityAttenuateWire, _>(operation, request, id, CapabilityAttenuateWire::into_request)?,
        Operation::CapabilityRevoke => wire_bytes::<CapabilityRevokeWire, _>(operation, request, id, CapabilityRevokeWire::into_request)?,
        Operation::SessionRefresh => wire_bytes::<SessionRefreshWire, _>(operation, request, id, SessionRefreshWire::into_request)?,
        Operation::SessionClose => wire_bytes::<SessionCloseWire, _>(operation, request, id, SessionCloseWire::into_request)?,
        Operation::SubscriptionCreate => wire_bytes::<SubscriptionCreateWire, _>(operation, request, id, SubscriptionCreateWire::into_request)?,
        Operation::SubscriptionPause
        | Operation::SubscriptionResume
        | Operation::SubscriptionDelete => wire_bytes::<SubscriptionTargetWire, _>(operation, request, id, SubscriptionTargetWire::into_request)?,
        Operation::SubscriptionAcknowledge => wire_bytes::<CursorAcknowledgementWire, _>(operation, request, id, CursorAcknowledgementWire::into_request)?,
        Operation::Sign => wire_bytes::<SignRequestWire, _>(operation, request, id, SignRequestWire::into_request)?,
        Operation::ProgramCall => wire_bytes::<ProgramCallWire, _>(operation, request, id, ProgramCallWire::into_request)?,
        Operation::ProgramDeploy => wire_bytes::<ProgramDeployWire, _>(operation, request, id, ProgramDeployWire::into_request)?,
        Operation::ProgramUpgrade => wire_bytes::<ProgramUpgradeWire, _>(operation, request, id, ProgramUpgradeWire::into_request)?,
        Operation::ProgramWindDown => wire_bytes::<ProgramWindDownWire, _>(operation, request, id, ProgramWindDownWire::into_request)?,
        Operation::AgentRegister
        | Operation::SessionOpen
        | Operation::AvailabilityFetch
        | Operation::BudgetList
        | Operation::BudgetReconciliation
        | Operation::CapabilityList
        | Operation::ExportOffline
        | Operation::FaucetClaim
        | Operation::ProgramActivity
        | Operation::ProgramDiscover
        | Operation::ProgramInterface
        | Operation::ProgramSimulate
        | Operation::Project

        | Operation::PolicyDryRun

        | Operation::BudgetState
        | Operation::ReadBatch
        | Operation::ReadHistory
        | Operation::ReadModuleState
        | Operation::ReadProofBundle
        | Operation::SessionList
        | Operation::SubscriptionHealth
        | Operation::SubscriptionList
        | Operation::Wait => return Ok(None),
    }))
}

/// Dispatches one authorized envelope to the shared owner. `permit` is the exact-generation
/// stop permit from `SessionControl::authorize`; holding the borrow keeps it alive for the
/// whole owner effect.
///
/// # Errors
/// Returns a [`Rejection`] for a request that does not decode into the owner typed request,
/// or an owner refusal or unavailability.
pub(crate) fn dispatch_operation<A: HumanAuthorityBoundary>(
    owner: &SharedAgentOwner<A>,
    permit: &OperationPermit,
    peer: &RpcOwnerContext<'_>,
    operation: Operation,
    request: &Map<String, Value>,
    ctx: &DispatchContext,
) -> Result<Dispatched, Rejection> {
    let _ = permit;
    let id = ctx.request_id;
    let context = peer;
    let peer = &ctx.peer;
    let shared = owner;
    let mut owner = owner.clone();
    match operation {
        Operation::ReadAccount => {
            let request: ReadAccountRequest = decode(request, id)?;
            let _ = (request.tenant, request.agent);
            let account_id = hex32(&request.account_id, id)?;
            dispatched(id, owner.account_state(peer, account_id), |reader| {
                decode_account_state(reader, account_id)
            })
        }
        Operation::ReadBalance => {
            let request: CoordinatesOnly = decode(request, id)?;
            let _ = (request.tenant, request.agent);
            dispatched(id, owner.balance(peer), decode_balance)
        }
        Operation::ReadCheckpoint => {
            let request: CoordinatesOnly = decode(request, id)?;
            let _ = (request.tenant, request.agent);
            dispatched(id, owner.head(peer), decode_head)
        }
        Operation::Track => {
            let request: TrackRequest = decode(request, id)?;
            let _ = (request.tenant, request.agent);
            dispatched(id, owner.track(peer, &request.submission_ref), decode_observation)
        }
        Operation::ApprovalList => {
            if native_variant(request, id)? {
                return adapters::approval_list_native(shared, context, request, ctx);
            }
            let request: ApprovalListRequest = decode(request, id)?;
            let _ = (request.tenant, request.agent);
            let current_sequence = decimal_u64(&request.current_sequence, id)?;
            let cursor = request.cursor.map(|text| hex32(&text, id)).transpose()?;
            let limit = u8::try_from(decimal_u64(&request.limit, id)?)
                .map_err(|_| noncanonical(id))?;
            dispatched(
                id,
                owner.approval_list(peer, current_sequence, cursor, limit),
                decode_approval_list,
            )
        }
        Operation::ApprovalGet => {
            if native_variant(request, id)? {
                return adapters::approval_get_native(shared, context, request, ctx);
            }
            let request: ApprovalGetRequest = decode(request, id)?;
            let _ = (request.tenant, request.agent);
            let approval_id = hex32(&request.approval_id, id)?;
            let current_sequence = decimal_u64(&request.current_sequence, id)?;
            dispatched(
                id,
                owner.approval_get(peer, approval_id, current_sequence),
                decode_approval_get,
            )
        }
        Operation::ProgramReceipt => {
            let request: ActivityLookupRequest = decode(request, id)?;
            let _ = (request.tenant, request.agent);
            let key = hex32(&request.idempotency_key, id)?;
            let activity = hex32(&request.expected_activity_id, id)?;
            dispatched(
                id,
                owner.receipt_by_idempotency_key(peer, key, activity),
                decode_receipt_lookup,
            )
        }
        Operation::ApprovalApprove => adapters::approval_approve(shared, context, request, ctx),
        Operation::ApprovalReject => adapters::approval_reject(shared, context, request, ctx),
        Operation::Prepare => adapters::prepare(shared, context, request, ctx),
        Operation::Submit => adapters::submit(shared, context, request, ctx),
        Operation::FaucetClaim => Err(rejection(
            ErrorClass::UnavailableCapability,
            id,
            "unavailable_capability.faucet.claim",
        )),
        Operation::BudgetCreate => adapters::budget_create(shared, context, request, ctx),
        Operation::BudgetFund => adapters::budget_fund(shared, context, request, ctx),
        Operation::BudgetList => adapters::budget_list(shared, context, request, ctx),
        Operation::BudgetRevoke => adapters::budget_revoke(shared, context, request, ctx),
        Operation::BudgetReconciliation => adapters::budget_reconciliation(shared, context, request, ctx),
        Operation::CapabilityCreate => adapters::capability_create(shared, context, request, ctx),
        Operation::CapabilityAttenuate => adapters::capability_attenuate(shared, context, request, ctx),
        Operation::CapabilityList => adapters::capability_list(shared, context, request, ctx),
        Operation::CapabilityRevoke => adapters::capability_revoke(shared, context, request, ctx),
        Operation::SessionRefresh => adapters::session_refresh(shared, context, request, ctx),
        Operation::SessionClose => adapters::session_close(shared, context, request, ctx),
        Operation::SessionList => adapters::session_list(shared, context, request, ctx),
        Operation::SubscriptionCreate => adapters::subscription_create(shared, context, request, ctx),
        Operation::SubscriptionList => adapters::subscription_list(shared, context, request, ctx),
        Operation::SubscriptionPause => adapters::subscription_pause(shared, context, request, ctx),
        Operation::SubscriptionResume => adapters::subscription_resume(shared, context, request, ctx),
        Operation::SubscriptionDelete => adapters::subscription_delete(shared, context, request, ctx),
        Operation::SubscriptionHealth => adapters::subscription_health(shared, context, request, ctx),
        Operation::SubscriptionAcknowledge => adapters::subscription_acknowledge(shared, context, request, ctx),
        Operation::AvailabilityFetch => adapters::availability_fetch(shared, context, request, ctx),
        Operation::ReadModuleState => adapters::read_module_state(shared, context, request, ctx),
        Operation::ReadHistory => adapters::read_history(shared, context, request, ctx),
        Operation::ReadBatch => adapters::read_batch(shared, context, request, ctx),
        Operation::ExportOffline => adapters::export_offline(shared, context, request, ctx),
        Operation::Wait => adapters::wait(shared, context, request, ctx),
        Operation::Sign => adapters::sign(shared, context, request, ctx),
        Operation::Project => adapters::project(shared, context, request, ctx),

        Operation::PolicyDryRun => adapters::policy_dry_run(shared, context, request, ctx),

        Operation::BudgetState => adapters::budget_state(shared, context, request, ctx),
        Operation::ProgramActivity => adapters::program_activity(shared, context, request, ctx),
        Operation::ProgramCall => adapters::program_call(shared, context, request, ctx),
        Operation::ProgramDeploy => adapters::program_deploy(shared, context, request, ctx),
        Operation::ProgramDiscover => adapters::program_discover(shared, context, request, ctx),
        Operation::ProgramSimulate => adapters::program_simulate(shared, context, request, ctx),
        Operation::ProgramUpgrade => adapters::program_upgrade(shared, context, request, ctx),
        Operation::ProgramWindDown => adapters::program_wind_down(shared, context, request, ctx),
        Operation::AgentRegister | Operation::SessionOpen => Err(rejection(
            ErrorClass::PolicyRefusal,
            id,
            "refused_pending_bootstrap_artifact",
        )),
        Operation::ProgramInterface | Operation::ReadProofBundle => Err(rejection(
            ErrorClass::UnavailableCapability,
            id,
            "unmatched_by_ruling",
        )),
    }
}

#[test]
fn capability_refusal_carries_its_dimension_to_the_typed_wire_refusal() {
    let id = RequestId(9);
    for (dimension, reason) in [
        (crate::capability::Dimension::Expiry, "capability.expiry"),
        (crate::capability::Dimension::ActivityType, "capability.activity_type"),
        (crate::capability::Dimension::Counterparty, "capability.counterparty"),
        (crate::capability::Dimension::Asset, "capability.asset"),
        (crate::capability::Dimension::Amount, "capability.amount"),
        (crate::capability::Dimension::Rate, "capability.rate"),
        (crate::capability::Dimension::Purpose, "capability.purpose"),
    ] {
        let refusal = owner_error(id, HumanOperationError::CapabilityRefused(dimension));
        assert_eq!(refusal.class, ErrorClass::CapabilityRefusal);
        assert_eq!(refusal.retriability, Retriability::Terminal);
        assert_eq!(refusal.request_id, id);
        assert_eq!(refusal.reason, reason);
    }
    let refused = owner_error(id, HumanOperationError::Refused);
    assert_eq!(refused.class, ErrorClass::PolicyRefusal);
    assert_eq!(refused.reason, "owner.refused");
    let unavailable = owner_error(id, HumanOperationError::Unavailable);
    assert_eq!(unavailable.class, ErrorClass::UnavailableCapability);
    assert_eq!(unavailable.retriability, Retriability::Retriable);
    assert_eq!(unavailable.reason, "owner.unavailable");
}

#[test]
fn typed_owner_refusals_keep_their_class_and_reason() {
    let id = RequestId(11);
    for refusal in [
        crate::human::HumanRefusal::StalePinnedHead,
        crate::human::HumanRefusal::ExportSettlementAnchoringUnavailable,
        crate::human::HumanRefusal::BudgetAuthorizationRequired,
        crate::human::HumanRefusal::Policy(
            crate::policy::PolicyDryRunRefusal::LegacyProjectPayload,
        ),
    ] {
        let rejection = owner_error(id, HumanOperationError::Typed(refusal));
        assert_eq!(rejection.class, refusal.class());
        assert_eq!(rejection.retriability, Retriability::Terminal);
        assert_eq!(rejection.request_id, id);
        assert_eq!(rejection.reason, refusal.reason());
    }
    let legacy = owner_error(
        id,
        HumanOperationError::Typed(crate::human::HumanRefusal::Policy(
            crate::policy::PolicyDryRunRefusal::LegacyProjectPayload,
        )),
    );
    assert_eq!(legacy.reason, "policy.legacy_project_payload");
}

#[test]

fn canonical_decimals_and_hex_are_exact() {
    let id = RequestId(7);
    assert_eq!(decimal_u64("0", id).ok(), Some(0));
    assert_eq!(decimal_u64("18446744073709551615", id).ok(), Some(u64::MAX));
    for bad in ["", "00", "01", "+1", "-1", "18446744073709551616", "1 "] {
        assert!(decimal_u64(bad, id).is_err(), "{bad}");
    }
    assert!(hex32(&"ab".repeat(32), id).is_ok());
    assert!(hex32(&"AB".repeat(32), id).is_err());
    assert!(hex32(&"ab".repeat(31), id).is_err());
    assert_eq!(lower_hex(&[0x0f, 0xa0]), "0fa0");
}
