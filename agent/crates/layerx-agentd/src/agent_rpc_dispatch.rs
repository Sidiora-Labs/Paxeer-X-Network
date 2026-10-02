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
use crate::human::{HumanOperationError, HumanOperations, HumanPeer, HumanResponse};
use crate::human_runtime::{HumanAuthorityBoundary, SharedAgentOwner};
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

fn malformed(request_id: RequestId) -> Rejection {
    rejection(ErrorClass::ProtocolIncompatibility, request_id, "envelope.malformed")
}

fn noncanonical(request_id: RequestId) -> Rejection {
    rejection(
        ErrorClass::ProtocolIncompatibility,
        request_id,
        "envelope.noncanonical_integer",
    )
}

fn decimal_u64(text: &str, request_id: RequestId) -> Result<u64, Rejection> {
    decimal_u128(text, request_id)?
        .try_into()
        .map_err(|_| noncanonical(request_id))
}

fn decimal_u128(text: &str, request_id: RequestId) -> Result<u128, Rejection> {
    let canonical = !text.is_empty()
        && text.len() <= 39
        && text.bytes().all(|byte| byte.is_ascii_digit())
        && (text == "0" || !text.starts_with('0'));
    if !canonical {
        return Err(noncanonical(request_id));
    }
    text.parse::<u128>().map_err(|_| noncanonical(request_id))
}

fn hex32(text: &str, request_id: RequestId) -> Result<[u8; 32], Rejection> {
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

fn lower_hex(bytes: &[u8]) -> String {
    const DIGITS: &[u8; 16] = b"0123456789abcdef";
    let mut out = String::with_capacity(bytes.len() * 2);
    for byte in bytes {
        out.push(char::from(DIGITS[usize::from(byte >> 4)]));
        out.push(char::from(DIGITS[usize::from(byte & 0x0f)]));
    }
    out
}

fn decode<T: for<'de> Deserialize<'de>>(
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

fn owner_error(request_id: RequestId, error: HumanOperationError) -> Rejection {
    match error {
        HumanOperationError::Refused => {
            rejection(ErrorClass::PolicyRefusal, request_id, "owner.refused")
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
struct Reader<'a> {
    bytes: &'a [u8],
    offset: usize,
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
    fn finish(&self) -> Option<()> {
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

fn level_name(value: Level) -> &'static str {
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
fn dispatched(
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

/// Canonical bytes of the strictly decoded owner typed request, used by `agent_rpc` as the
/// `idempotency::Store::execute` request bytes (the store applies the `LXP/agent/request/v1`
/// domain). Every matched operation uses the operation name,
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
            named(operation, &decode::<ApprovalListRequest>(request, id)?, id)?
        }
        Operation::ApprovalGet => {
            named(operation, &decode::<ApprovalGetRequest>(request, id)?, id)?
        }
        _ => return Ok(None),
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
    operation: Operation,
    request: &Map<String, Value>,
    ctx: &DispatchContext,
) -> Result<Dispatched, Rejection> {
    let _permit = permit;
    let id = ctx.request_id;
    let peer = &ctx.peer;
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
            dispatched(id, owner.track(peer, &request.submission_ref), decode_tracked)
        }
        Operation::ApprovalList => {
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
    }
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
