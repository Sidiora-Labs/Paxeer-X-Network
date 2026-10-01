//! The LayerX half: follows a relay/archive node's synchronization head and
//! reads each committed batch from `/v1/history/batches/N`, decoding every
//! receipt with the frozen `layerx-wire` decoder into the 15-field activity
//! receipt and the 21-field 402LXP receipt. Each transfer-bearing receipt
//! is classified for settlement through [`verify_settlement`] when trust is
//! configured; depth and ordering never enter that classification.

use layerx_types::receipt::{ACTIVITY_RECEIPT_FIELDS, LXP_RECEIPT_FIELDS};
use layerx_wire::receipt::{self as wire_receipt, ProtocolReceipt, Receipt};
use serde_json::{json, Map, Value};

use crate::codec::{hex, is_zero, unhex};
use crate::config::SettlementTrust;
use crate::follow::{walk_back, FollowPolicy, StepOutcome};
use crate::settlement::{
    verify_settlement, SettlementInput, SettlementRefusal, VerifiedSettlement,
};
use crate::store::{
    AssetRow, EventRow, SettlementRecord, Store, TransferRow, Unit, SETTLEMENT_EVIDENCE_ABSENT,
    SETTLEMENT_EVIDENCE_MALFORMED, SETTLEMENT_OUTSIDE_AUTHORIZATION, SETTLEMENT_TRUST_UNCONFIGURED,
};
use crate::transport::Endpoint;
use crate::IndexError;

/// The chain label of every LayerX row.
pub const CHAIN: &str = "layerx";

/// The effect kind tag of an ordered event emission.
const EFFECT_EVENT: u8 = 3;

fn text<'a>(value: &'a Value, field: &str) -> Result<&'a str, IndexError> {
    value
        .get(field)
        .and_then(Value::as_str)
        .ok_or_else(|| IndexError::Decode(format!("batch field {field} is missing")))
}

fn decimal(value: &Value, field: &str) -> Result<u64, IndexError> {
    let raw = text(value, field)?;
    if raw.is_empty() || (raw.len() > 1 && raw.starts_with('0')) {
        return Err(IndexError::Decode(format!(
            "{field} is not canonical decimal"
        )));
    }
    raw.parse()
        .map_err(|_| IndexError::Decode(format!("{field} is not canonical decimal")))
}

fn hex32(value: &Value, field: &str) -> Result<String, IndexError> {
    let raw = text(value, field)?;
    if raw.len() != 64 || !raw.bytes().all(|byte| byte.is_ascii_hexdigit()) {
        return Err(IndexError::Decode(format!("{field} is not 32-byte hex")));
    }
    Ok(raw.to_ascii_lowercase())
}

/// The base activity receipt as exactly the 15 frozen fields, in order.
///
/// # Errors
/// Refuses a field name the decoder does not expose.
pub fn activity_receipt_json(receipt: &ProtocolReceipt) -> Result<Value, IndexError> {
    let mut object = Map::new();
    for field in ACTIVITY_RECEIPT_FIELDS {
        let value = match field {
            "protocol_version" => json!(receipt.protocol_version()),
            "activity_id" => json!(hex(&receipt.activity_id())),
            "global_sequence" => json!(receipt.global_sequence().to_string()),
            "previous_state_root" => json!(hex(&receipt.previous_state_root())),
            "resulting_state_root" => json!(hex(&receipt.resulting_state_root())),
            "activity_root" => json!(hex(&receipt.activity_root())),
            "result_code" => json!(receipt.result_code()),
            "effects" => Value::Array(
                receipt
                    .effects()
                    .iter()
                    .map(|effect| {
                        json!({
                            "module_id": effect.module_id(),
                            "ordinal": effect.ordinal(),
                            "event_type": effect.event_type(),
                            "kind": effect.kind(),
                            "monetary": effect.monetary(),
                            "transfer_set_root": hex(&effect.transfer_set_root()),
                            "body": hex(effect.body()),
                        })
                    })
                    .collect(),
            ),
            "fee_charged" => json!(receipt.fee_charged().to_string()),
            "batch_id" => json!(hex(&receipt.batch_id())),
            "module_id" => json!(receipt.module_id()),
            "module_version" => json!(receipt.module_version()),
            "parameter_version" => json!(receipt.parameter_version()),
            "timestamp" => json!(receipt.timestamp().to_string()),
            "sequencer_signature" => {
                json!(receipt
                    .sequencer_signature()
                    .map(|signature| hex(&signature)))
            }
            other => {
                return Err(IndexError::Decode(format!(
                    "activity receipt field {other} has no decoder accessor"
                )))
            }
        };
        object.insert(field.to_owned(), value);
    }
    Ok(Value::Object(object))
}

/// The 402LXP financial receipt as exactly the 21 frozen fields, in order.
///
/// # Errors
/// Refuses a field name the decoder does not expose.
pub fn lxp_receipt_json(receipt: &ProtocolReceipt) -> Result<Value, IndexError> {
    let mut object = Map::new();
    for field in LXP_RECEIPT_FIELDS {
        let value = match field {
            "protocol_version" => json!(receipt.protocol_version()),
            "transaction_id" => json!(hex(&receipt.activity_id())),
            "operation" => json!(receipt.operation()),
            "global_sequence" => json!(receipt.global_sequence().to_string()),
            "asset" => json!(hex(&receipt.asset())),
            "amount" => json!(receipt.amount().to_string()),
            "from" => json!(hex(&receipt.from())),
            "from_balance_before" => json!(receipt.debit_balance_before().to_string()),
            "from_balance_after" => json!(receipt.debit_balance_after().to_string()),
            "from_sequence" => json!(receipt.debit_sequence().to_string()),
            "to" => json!(hex(&receipt.to())),
            "to_balance_before" => json!(receipt.credit_balance_before().to_string()),
            "to_balance_after" => json!(receipt.credit_balance_after().to_string()),
            "transfer_set_root" => json!(hex(&receipt.transfer_set_root())),
            "authorization_hash" => json!(hex(&receipt.authorization_hash())),
            "context_hash" => json!(hex(&receipt.context_hash())),
            "previous_state_root" => json!(hex(&receipt.previous_state_root())),
            "resulting_state_root" => json!(hex(&receipt.resulting_state_root())),
            "batch_id" => json!(hex(&receipt.batch_id())),
            "timestamp" => json!(receipt.timestamp().to_string()),
            "sequencer_signature" => {
                json!(receipt
                    .sequencer_signature()
                    .map(|signature| hex(&signature)))
            }
            other => {
                return Err(IndexError::Decode(format!(
                    "402LXP receipt field {other} has no decoder accessor"
                )))
            }
        };
        object.insert(field.to_owned(), value);
    }
    Ok(Value::Object(object))
}

fn program_outcome_json(receipt: &ProtocolReceipt) -> Value {
    receipt.program_outcome().map_or(Value::Null, |outcome| {
        json!({
            "encoding_version": outcome.encoding_version(),
            "terminal_kind": outcome.terminal_kind(),
            "result_code": outcome.result_code(),
            "runtime_version": outcome.runtime_version(),
            "abi_version": outcome.abi_version(),
            "cpu_fuel": outcome.cpu_fuel().to_string(),
            "memory_bytes": outcome.memory_bytes().to_string(),
            "fee_units": outcome.fee_units().to_string(),
        })
    })
}

/// Decodes one `/v1/history/batches/N` document into a store unit.
///
/// # Errors
/// Refuses a malformed document, an undecodable receipt, and a receipt whose
/// activity, sequence or batch disagrees with the batch that carries it.
pub fn decode_batch(document: &Value) -> Result<Unit, IndexError> {
    let batch_number = decimal(document, "batch_number")?;
    let batch_id = hex32(document, "batch_id")?;
    let previous_state_root = hex32(document, "previous_state_root")?;
    let resulting_state_root = hex32(document, "resulting_state_root")?;
    let first_sequence = decimal(document, "first_sequence")?;
    let last_sequence = decimal(document, "last_sequence")?;
    if first_sequence > last_sequence {
        return Err(IndexError::Decode(
            "batch sequence range is empty".to_owned(),
        ));
    }
    let activities = document
        .get("activities")
        .and_then(Value::as_array)
        .ok_or_else(|| IndexError::Decode("batch has no activities array".to_owned()))?;
    let maintenance = document
        .get("maintenance")
        .and_then(Value::as_array)
        .ok_or_else(|| IndexError::Decode("batch has no maintenance array".to_owned()))?;
    let mut unit = Unit {
        chain: CHAIN.to_owned(),
        position: batch_number,
        hash: batch_id.clone(),
        parent: previous_state_root,
        link: resulting_state_root,
        boundary: last_sequence,
        ..Unit::default()
    };
    for activity in activities {
        decode_activity(
            &mut unit,
            activity,
            batch_number,
            &batch_id,
            first_sequence,
            last_sequence,
        )?;
    }
    for record in maintenance {
        let sequence = decimal(record, "sequence")?;
        let kind = text(record, "kind")?;
        let receipt_hex = text(record, "receipt_hex")?.to_ascii_lowercase();
        unhex(&receipt_hex)?;
        unit.events.push(EventRow {
            height_or_seq: sequence,
            source: "layerx-maintenance".to_owned(),
            name: kind.to_owned(),
            contract: None,
            account: None,
            tx_id: format!("batch-{batch_number}"),
            ordinal: sequence,
            decoded: json!({
                "batch_number": batch_number.to_string(),
                "sequence": sequence.to_string(),
                "kind": kind,
                "receipt_hex": receipt_hex,
            }),
        });
    }
    Ok(unit)
}

fn decode_activity(
    unit: &mut Unit,
    activity: &Value,
    batch_number: u64,
    batch_id: &str,
    first_sequence: u64,
    last_sequence: u64,
) -> Result<(), IndexError> {
    let activity_id = hex32(activity, "activity_id")?;
    let sequence = decimal(activity, "sequence")?;
    if !(first_sequence..=last_sequence).contains(&sequence) {
        return Err(IndexError::Integrity(format!(
            "activity {activity_id} sequence {sequence} is outside its batch"
        )));
    }
    let actor = text(activity, "actor")?.to_owned();
    let module = activity
        .get("module")
        .and_then(Value::as_u64)
        .unwrap_or_default();
    let accounts: Vec<String> = activity
        .get("accounts")
        .and_then(Value::as_array)
        .map(|accounts| {
            accounts
                .iter()
                .filter_map(Value::as_str)
                .map(str::to_ascii_lowercase)
                .collect()
        })
        .unwrap_or_default();
    unit.accounts.extend(accounts.iter().cloned());
    let receipt_bytes = unhex(text(activity, "receipt_hex")?)?;
    let decoded = wire_receipt::decode(&receipt_bytes)
        .map_err(|error| IndexError::Decode(format!("receipt of {activity_id}: {error:?}")))?;
    let receipt = match &decoded {
        Receipt::Protocol(receipt) => receipt.as_ref(),
        Receipt::Replay(_) => {
            unit.events.push(EventRow {
                height_or_seq: sequence,
                source: "layerx-receipt".to_owned(),
                name: "replay_receipt".to_owned(),
                contract: None,
                account: accounts.first().cloned(),
                tx_id: activity_id.clone(),
                ordinal: 0,
                decoded: json!({
                    "batch_number": batch_number.to_string(),
                    "receipt_hex": hex(&receipt_bytes),
                }),
            });
            return Ok(());
        }
    };
    if hex(&receipt.activity_id()) != activity_id
        || receipt.global_sequence() != sequence
        || hex(&receipt.batch_id()) != batch_id
    {
        return Err(IndexError::Integrity(format!(
            "receipt of {activity_id} names another activity, sequence or batch"
        )));
    }
    let activity_receipt = activity_receipt_json(receipt)?;
    let lxp_receipt = lxp_receipt_json(receipt)?;
    let context = json!({
        "batch_number": batch_number.to_string(),
        "actor": actor,
        "module": module,
        "accounts": accounts,
        "operation": receipt.operation(),
        "activity_receipt": activity_receipt,
        "lxp_receipt": lxp_receipt,
        "program_outcome": program_outcome_json(receipt),
        "supply": receipt.total_units().map(|(before, after)| json!({
            "before": before.to_string(),
            "after": after.to_string(),
        })),
    });
    let asset = receipt.asset();
    if !is_zero(&asset) {
        unit.assets.push(AssetRow {
            asset: hex(&asset),
            chain: CHAIN.to_owned(),
            kind: "layerx".to_owned(),
            address: None,
            denom: None,
            metadata: receipt.total_units().map_or_else(
                || json!({}),
                |(_, after)| json!({ "supply": after.to_string(), "supply_sequence": sequence.to_string() }),
            ),
        });
    }
    if receipt.result_code() == 0 && receipt.amount() > 0 && !is_zero(&asset) {
        let from = receipt.from();
        let to = receipt.to();
        let (kind, legs): (&str, Vec<(&'static str, [u8; 32], [u8; 32])>) =
            match (is_zero(&from), is_zero(&to)) {
                (false, false) => ("lxp_transfer", vec![("out", from, to), ("in", to, from)]),
                (true, false) => ("lxp_credit", vec![("in", to, from)]),
                (false, true) => ("lxp_debit", vec![("out", from, to)]),
                (true, true) => ("lxp_transfer", Vec::new()),
            };
        for (direction, account, counterparty) in legs {
            unit.transfers.push(TransferRow {
                height_or_seq: sequence,
                kind: kind.to_owned(),
                direction,
                account: hex(&account),
                counterparty: (!is_zero(&counterparty)).then(|| hex(&counterparty)),
                asset: hex(&asset),
                amount: receipt.amount().to_string(),
                tx_id: activity_id.clone(),
                ordinal: 0,
                decoded: context.clone(),
            });
        }
    }
    for effect in receipt.effects() {
        if effect.kind() != EFFECT_EVENT {
            continue;
        }
        unit.events.push(EventRow {
            height_or_seq: sequence,
            source: "layerx-effect".to_owned(),
            name: format!("module{}/event{}", effect.module_id(), effect.event_type()),
            contract: Some(format!("module{}", effect.module_id())),
            account: accounts.first().cloned(),
            tx_id: activity_id.clone(),
            ordinal: u64::from(effect.ordinal()),
            decoded: json!({
                "module_id": effect.module_id(),
                "event_type": effect.event_type(),
                "ordinal": effect.ordinal(),
                "monetary": effect.monetary(),
                "body": hex(effect.body()),
            }),
        });
    }
    unit.events.push(EventRow {
        height_or_seq: sequence,
        source: "layerx-receipt".to_owned(),
        name: "activity".to_owned(),
        contract: Some(format!("module{}", receipt.module_id())),
        account: accounts.first().cloned(),
        tx_id: activity_id,
        ordinal: 0,
        decoded: context,
    });
    Ok(())
}

/// Why a receipt was not verified.
enum Unsettled {
    Unavailable(&'static str),
    Refused(SettlementRefusal),
}

fn evidence(value: &Value, field: &str) -> Result<Vec<u8>, Unsettled> {
    match value.get(field) {
        None | Some(Value::Null) => Err(Unsettled::Unavailable(SETTLEMENT_EVIDENCE_ABSENT)),
        Some(Value::String(text)) => {
            unhex(text).map_err(|_| Unsettled::Unavailable(SETTLEMENT_EVIDENCE_MALFORMED))
        }
        Some(_) => Err(Unsettled::Unavailable(SETTLEMENT_EVIDENCE_MALFORMED)),
    }
}

/// Verifies one receipt of a relay batch document: the batch carries the
/// canonical signed header (`signed_header_hex`; `header_hex` is only its
/// 32-byte identity), its signature (`signature_hex`) and the CP1/CX1 checkpoint
/// evidence (`checkpoint_hex`, `context_hex`); the activity carries the
/// receipt (`receipt_hex`) and its encoded inclusion proof
/// (`receipt_proof_hex`).
fn settle_receipt(
    document: &Value,
    activity: &Value,
    batch_number: u64,
    trust: &SettlementTrust,
) -> Result<VerifiedSettlement, Unsettled> {
    let authorization = &trust.authorization;
    if !(authorization.first_batch_number()..=authorization.last_batch_number())
        .contains(&batch_number)
    {
        return Err(Unsettled::Unavailable(SETTLEMENT_OUTSIDE_AUTHORIZATION));
    }
    let header = evidence(document, "signed_header_hex")?;
    let signature: [u8; 64] = evidence(document, "signature_hex")?
        .try_into()
        .map_err(|_| Unsettled::Unavailable(SETTLEMENT_EVIDENCE_MALFORMED))?;
    let checkpoint = evidence(document, "checkpoint_hex")?;
    let context = evidence(document, "context_hex")?;
    let receipt = evidence(activity, "receipt_hex")?;
    let receipt_proof = evidence(activity, "receipt_proof_hex")?;
    let policy = trust.verifier.policy();
    verify_settlement(&SettlementInput {
        receipt: &receipt,
        receipt_proof: &receipt_proof,
        header: &header,
        header_signature: &signature,
        authorization,
        batch_number,
        checkpoint: &checkpoint,
        context: &context,
        protocol_version: policy.protocol_version,
        network_id: policy.network_id,
        verifier: Some(&trust.verifier),
    })
    .map_err(Unsettled::Refused)
}

/// Classifies the settlement of every transfer-bearing receipt of `unit`,
/// decoded from the relay batch `document`. Without `trust`, or without the
/// evidence fields, a receipt is unverified; a canonical refusal publishes
/// its own level; only a [`VerifiedSettlement`] is verified.
#[must_use]
pub fn settle_batch(
    document: &Value,
    unit: &Unit,
    trust: Option<&SettlementTrust>,
) -> Vec<SettlementRecord> {
    let activities = document.get("activities").and_then(Value::as_array);
    let mut records: Vec<SettlementRecord> = Vec::new();
    for transfer in &unit.transfers {
        if records
            .iter()
            .any(|record| record.tx_id() == transfer.tx_id)
        {
            continue;
        }
        let activity = activities.and_then(|activities| {
            activities.iter().find(|activity| {
                activity
                    .get("activity_id")
                    .and_then(Value::as_str)
                    .is_some_and(|id| id.eq_ignore_ascii_case(&transfer.tx_id))
            })
        });
        // ponytail: one Paxeer publication lookup per receipt; share one per
        // batch checkpoint if relay batches grow large.
        let outcome = match (trust, activity) {
            (None, _) => Err(Unsettled::Unavailable(SETTLEMENT_TRUST_UNCONFIGURED)),
            (Some(_), None) => Err(Unsettled::Unavailable(SETTLEMENT_EVIDENCE_ABSENT)),
            (Some(trust), Some(activity)) => {
                settle_receipt(document, activity, unit.position, trust)
            }
        };
        let tx_id = transfer.tx_id.clone();
        records.push(match outcome {
            Ok(settlement) => SettlementRecord::verified(tx_id, &settlement),
            Err(Unsettled::Refused(refusal)) => {
                SettlementRecord::refused(tx_id, transfer.height_or_seq, &refusal)
            }
            Err(Unsettled::Unavailable(reason)) => {
                SettlementRecord::unavailable(tx_id, transfer.height_or_seq, reason)
            }
        });
    }
    records
}

/// Follows one relay/archive node.
pub struct LayerXIngester {
    relay: Endpoint,
    policy: FollowPolicy,
    start_batch: Option<u64>,
    settlement: Option<SettlementTrust>,
}

impl LayerXIngester {
    #[must_use]
    pub const fn new(relay: Endpoint, policy: FollowPolicy, start_batch: Option<u64>) -> Self {
        Self {
            relay,
            policy,
            start_batch,
            settlement: None,
        }
    }

    /// Verifies receipt settlement against `trust` while ingesting.
    #[must_use]
    pub fn with_settlement(mut self, trust: Option<SettlementTrust>) -> Self {
        self.settlement = trust;
        self
    }

    fn batch(&self, number: u64) -> Result<Option<Value>, IndexError> {
        let path = format!("/v1/history/batches/{number}");
        let (status, body) = self.relay.get(&path)?;
        match status {
            200 => serde_json::from_slice(&body)
                .map(Some)
                .map_err(|error| IndexError::Decode(format!("{path}: {error}"))),
            404 => Ok(None),
            other => Err(IndexError::Source(format!("GET {path} answered {other}"))),
        }
    }

    fn start(&self) -> Result<u64, IndexError> {
        if let Some(start) = self.start_batch {
            return Ok(start);
        }
        let network = self.relay.get_json("/v1/sync/network")?;
        decimal(&network, "first_batch")
    }

    /// Runs one bounded step: confirms the durable head is still canonical,
    /// then commits up to `max_units_per_step` new batches.
    ///
    /// # Errors
    /// Returns source, decode, integrity and store failures, and
    /// [`IndexError::ReorgBeyondFinality`].
    pub fn step(&self, store: &Store) -> Result<StepOutcome, IndexError> {
        let head = self.relay.get_json("/v1/sync/head")?;
        let Some(head_batch) = head
            .get("head_batch")
            .filter(|value| !value.is_null())
            .map(|_| decimal(&head, "head_batch"))
            .transpose()?
        else {
            return Ok(StepOutcome::Idle);
        };
        let start = self.start()?;
        let cursor = store.cursor(CHAIN)?;
        if let Some(cursor) = &cursor {
            let canonical = self.batch(cursor.position)?;
            let canonical_id = canonical
                .as_ref()
                .map(|doc| hex32(doc, "batch_id"))
                .transpose()?;
            if canonical_id.as_deref() != Some(cursor.hash.as_str()) {
                return self.reorg(store, cursor.position, start);
            }
        }
        let next = cursor.map_or(start, |cursor| cursor.position + 1);
        if next > head_batch {
            return Ok(StepOutcome::Idle);
        }
        let last = head_batch.min(next.saturating_add(self.policy.max_units_per_step.max(1) - 1));
        let mut units = 0;
        for number in next..=last {
            let document = self
                .batch(number)?
                .ok_or_else(|| IndexError::Source(format!("relay lost batch {number}")))?;
            let unit = decode_batch(&document)?;
            if unit.position != number {
                return Err(IndexError::Integrity(format!(
                    "relay answered batch {} for {number}",
                    unit.position
                )));
            }
            if let Some(previous) = number
                .checked_sub(1)
                .map(|p| store.link(CHAIN, p))
                .transpose()?
                .flatten()
            {
                if previous.link != unit.parent {
                    return self.reorg(store, previous.position, start);
                }
            }
            let settlements = settle_batch(&document, &unit, self.settlement.as_ref());
            store.commit_settled(&unit, self.policy.finality_depth, &settlements)?;
            units += 1;
        }
        Ok(StepOutcome::Advanced {
            units,
            position: last,
        })
    }

    fn reorg(&self, store: &Store, from: u64, start: u64) -> Result<StepOutcome, IndexError> {
        walk_back(store, CHAIN, from, start, |position| {
            self.batch(position)?
                .map(|document| hex32(&document, "batch_id"))
                .transpose()
        })
    }
}
