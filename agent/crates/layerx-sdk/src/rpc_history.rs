//! `read.history` over the version 1 Agent operation envelope.

use layerx_agent_api::error::RequestId;
use layerx_agent_api::verify::{Level, VerificationStatus};
use serde_json::{json, Value};

use crate::agent_envelope::{AgentEnvelopeTransport, EnvelopeCredential, EnvelopeError};
use crate::rpc_subscription::{decimal, object, violation};
use crate::Operation;

const CURSOR_TEXT_LENGTH: usize = 112;

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct HistoryCursor {
    pub next_sequence: u64,
    pub end_sequence: u64,
    pub observed_head_sequence: u64,
    pub observed_checkpoint: [u8; 32],
}

impl HistoryCursor {
    /// Renders the 112 lowercase hex cursor text: big-endian next, end and observed head
    /// sequences followed by the observed checkpoint.
    #[must_use]
    pub fn to_text(&self) -> String {
        let mut bytes = Vec::with_capacity(CURSOR_TEXT_LENGTH / 2);
        bytes.extend_from_slice(&self.next_sequence.to_be_bytes());
        bytes.extend_from_slice(&self.end_sequence.to_be_bytes());
        bytes.extend_from_slice(&self.observed_head_sequence.to_be_bytes());
        bytes.extend_from_slice(&self.observed_checkpoint);
        crate::rpc::encode_hex(&bytes)
    }

    /// Parses exactly 112 lowercase hex characters.
    #[must_use]
    pub fn from_text(text: &str) -> Option<Self> {
        if text.len() != CURSOR_TEXT_LENGTH {
            return None;
        }
        let bytes = lower_hex(text)?;
        let sequence = |offset: usize| {
            bytes
                .get(offset..offset + 8)
                .and_then(|slice| <[u8; 8]>::try_from(slice).ok())
                .map(u64::from_be_bytes)
        };
        Some(Self {
            next_sequence: sequence(0)?,
            end_sequence: sequence(8)?,
            observed_head_sequence: sequence(16)?,
            observed_checkpoint: bytes.get(24..56)?.try_into().ok()?,
        })
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct HistoryItem {
    pub global_sequence: u64,
    pub kind: u8,
    pub achieved_verification_level: Level,
    pub canonical: Vec<u8>,
    pub proof: Option<Vec<u8>>,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct HistoryPage {
    pub items: Vec<HistoryItem>,
    pub cursor: Option<HistoryCursor>,
    pub verification_status: VerificationStatus,
}

pub(crate) fn lower_hex(text: &str) -> Option<Vec<u8>> {
    if text.len() % 2 != 0 {
        return None;
    }
    text.as_bytes()
        .chunks_exact(2)
        .map(|pair| {
            let [high, low] = pair else {
                return None;
            };
            let nibble = |byte: u8| match byte {
                b'0'..=b'9' => Some(byte - b'0'),
                b'a'..=b'f' => Some(byte - b'a' + 10),
                _ => None,
            };
            Some((nibble(*high)? << 4) | nibble(*low)?)
        })
        .collect()
}

const fn level_wire(level: Level) -> &'static str {
    match level {
        Level::Unverified => "unverified",
        Level::SequencerSigned => "sequencer-signed",
        Level::BatchIncluded => "batch-included",
        Level::StateProven => "state-proven",
        Level::CheckpointFinalised => "checkpoint-finalised",
        Level::SettlementAnchored => "settlement-anchored",
    }
}

fn level(text: &str) -> Option<Level> {
    Some(match text {
        "unverified" => Level::Unverified,
        "sequencer-signed" => Level::SequencerSigned,
        "batch-included" => Level::BatchIncluded,
        "state-proven" => Level::StateProven,
        "checkpoint-finalised" => Level::CheckpointFinalised,
        "settlement-anchored" => Level::SettlementAnchored,
        _ => return None,
    })
}

fn decode_item(
    value: &Value,
    requested: Level,
    operation: Operation,
) -> Result<HistoryItem, EnvelopeError> {
    let item = object(
        value,
        &[
            "global_sequence",
            "kind",
            "achieved_verification_level",
            "canonical",
            "proof",
        ],
        operation,
    )?;
    let achieved = item["achieved_verification_level"]
        .as_str()
        .and_then(level)
        .filter(|achieved| *achieved >= requested)
        .ok_or_else(|| violation(operation))?;
    let bytes = |value: &Value| {
        value
            .as_str()
            .and_then(lower_hex)
            .ok_or_else(|| violation(operation))
    };
    Ok(HistoryItem {
        global_sequence: decimal(&item["global_sequence"], operation)?,
        kind: u8::try_from(decimal(&item["kind"], operation)?).map_err(|_| violation(operation))?,
        achieved_verification_level: achieved,
        canonical: bytes(&item["canonical"])?,
        proof: match &item["proof"] {
            Value::Null => None,
            value => Some(bytes(value).and_then(|proof| {
                if proof.is_empty() {
                    Err(violation(operation))
                } else {
                    Ok(proof)
                }
            })?),
        },
    })
}

impl AgentEnvelopeTransport {
    /// Reads one bounded history page over `first..=last`, resuming from `cursor`.
    ///
    /// # Errors
    ///
    /// Returns the established error envelope, `Transport`, or `Decode` for any item below
    /// the requested level, a malformed cursor or any other schema violation.
    #[allow(clippy::too_many_arguments)]
    pub fn read_history(
        &self,
        request_id: RequestId,
        credential: &EnvelopeCredential,
        first: u64,
        last: u64,
        cursor: Option<&HistoryCursor>,
        page_limit: u32,
        requested: Level,
    ) -> Result<HistoryPage, EnvelopeError> {
        let operation = Operation::ReadHistory;
        let success = self.send_operation(
            operation,
            request_id,
            &json!({
                "range": {"first": first.to_string(), "last": last.to_string()},
                "cursor": cursor.map(HistoryCursor::to_text),
                "page_limit": page_limit.to_string(),
                "requested_verification_level": level_wire(requested),
            }),
            Some(credential),
            None,
        )?;
        let page = object(&success.value, &["items", "cursor"], operation)?;
        let items = page["items"]
            .as_array()
            .ok_or_else(|| violation(operation))?
            .iter()
            .map(|item| decode_item(item, requested, operation))
            .collect::<Result<Vec<_>, _>>()?;
        if u32::try_from(items.len()).map_or(true, |count| count > page_limit) {
            return Err(violation(operation));
        }
        let cursor = match &page["cursor"] {
            Value::Null => None,
            value => Some(
                value
                    .as_str()
                    .and_then(HistoryCursor::from_text)
                    .ok_or_else(|| violation(operation))?,
            ),
        };
        Ok(HistoryPage {
            items,
            cursor,
            verification_status: success.verification_status,
        })
    }
}
