//! Execution summary and log verification for sequencer-signed EVM receipts.
//!
//! A sequencer receipt is a `ProtocolReceipt` v2 whose effect 0 carries the
//! execution summary and whose effects 1..=n commit to the transaction's logs
//! in order. The functions here read a receipt that already passed
//! [`crate::receipt::verify_sequencer_signature`] and bind the log list served
//! beside it to those signed effects.

use layerx_wire::limits::PROTOCOL_VERSION;
use layerx_wire::receipt::{Effect, ProtocolReceipt, Receipt};
use sha2::{Digest as _, Sha256};

/// Module ids at or below this value belong to the kernel modules.
const KERNEL_MODULE_COUNT: u16 = 11;
/// Module version of the sequencer receipt layout.
const SEQUENCER_LAYOUT_VERSION: u32 = 1;
/// Effect kind of the execution summary.
const KIND_STATE: u8 = 1;
/// Effect kind of one log.
const KIND_EVENT: u8 = 3;
/// Largest number of topics one EVM log carries.
const MAX_LOG_TOPICS: usize = 4;
/// Largest fee denom carried by the summary, in bytes.
const MAX_FEE_DENOM_BYTES: usize = 64;
const ADDRESS_BYTES: usize = 20;
const WORD_BYTES: usize = 32;
/// Summary bytes before the fee denom: three u64 gas fields, the u32 tx index,
/// two addresses, the vm error digest, the u32 return data length, the u32 log
/// count and the u8 denom length.
const SUMMARY_FIXED_BYTES: usize = 8 + 8 + 8 + 4 + ADDRESS_BYTES * 2 + WORD_BYTES + 4 + 4 + 1;

/// The execution summary carried by effect 0 of a sequencer receipt.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ReceiptSummary {
    gas_used: u64,
    cumulative_gas_used: u64,
    effective_gas_price: u64,
    tx_index: u32,
    contract_address: [u8; 20],
    sponsor: [u8; 20],
    vm_error_hash: [u8; 32],
    return_data_len: u32,
    log_count: u32,
    fee_denom: String,
}

impl ReceiptSummary {
    /// Returns the gas used by the transaction.
    #[must_use]
    pub const fn gas_used(&self) -> u64 {
        self.gas_used
    }

    /// Returns the gas used in the pending block up to and including this
    /// transaction.
    #[must_use]
    pub const fn cumulative_gas_used(&self) -> u64 {
        self.cumulative_gas_used
    }

    /// Returns the effective gas price paid.
    #[must_use]
    pub const fn effective_gas_price(&self) -> u64 {
        self.effective_gas_price
    }

    /// Returns the position of the transaction in its block.
    #[must_use]
    pub const fn tx_index(&self) -> u32 {
        self.tx_index
    }

    /// Returns the created contract address, zero when none was created.
    #[must_use]
    pub const fn contract_address(&self) -> [u8; 20] {
        self.contract_address
    }

    /// Returns the sponsor address, zero when the transaction was not
    /// sponsored.
    #[must_use]
    pub const fn sponsor(&self) -> [u8; 20] {
        self.sponsor
    }

    /// Returns sha256 of the VM error string.
    #[must_use]
    pub const fn vm_error_hash(&self) -> [u8; 32] {
        self.vm_error_hash
    }

    /// Returns the length of the EVM return data.
    #[must_use]
    pub const fn return_data_len(&self) -> u32 {
        self.return_data_len
    }

    /// Returns the number of logs the transaction emitted.
    #[must_use]
    pub const fn log_count(&self) -> u32 {
        self.log_count
    }

    /// Returns the fee denom the fee was charged in.
    #[must_use]
    pub fn fee_denom(&self) -> &str {
        &self.fee_denom
    }
}

/// One log as served by `pax_sequencerReceipt`.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct SequencerLog {
    address: [u8; 20],
    topics: Vec<[u8; 32]>,
    data: Vec<u8>,
    log_index: u64,
}

impl SequencerLog {
    /// Creates one served log from its decoded JSON fields.
    #[must_use]
    pub const fn new(
        address: [u8; 20],
        topics: Vec<[u8; 32]>,
        data: Vec<u8>,
        log_index: u64,
    ) -> Self {
        Self {
            address,
            topics,
            data,
            log_index,
        }
    }

    /// Returns the emitting contract address.
    #[must_use]
    pub const fn address(&self) -> [u8; 20] {
        self.address
    }

    /// Returns the log topics in order.
    #[must_use]
    pub fn topics(&self) -> &[[u8; 32]] {
        &self.topics
    }

    /// Returns the log data.
    #[must_use]
    pub fn data(&self) -> &[u8] {
        &self.data
    }

    /// Returns the index of the log within its transaction.
    #[must_use]
    pub const fn log_index(&self) -> u64 {
        self.log_index
    }
}

/// A served log list bound to the signed effects of its receipt.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct VerifiedLogs {
    summary: ReceiptSummary,
    logs: Vec<SequencerLog>,
}

impl VerifiedLogs {
    /// Returns the execution summary of the receipt.
    #[must_use]
    pub const fn summary(&self) -> &ReceiptSummary {
        &self.summary
    }

    /// Returns the verified logs in transaction order.
    #[must_use]
    pub fn logs(&self) -> &[SequencerLog] {
        &self.logs
    }

    /// Returns the verified logs, consuming the result.
    #[must_use]
    pub fn into_logs(self) -> Vec<SequencerLog> {
        self.logs
    }
}

/// The part of a served log that disagreed with its signed effect.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum LogField {
    /// The receipt holds no effect for this log.
    MissingEffect,
    /// The effect is not a log effect of this receipt's module.
    Effect,
    /// The effect body is not a well-formed log commitment.
    Body,
    /// The effect ordinal is not the log's position plus one.
    Ordinal,
    /// The served log index is not the log's position.
    LogIndex,
    /// The emitting address differs.
    Address,
    /// The topic count or a topic differs.
    Topics,
    /// sha256 of the served data differs from the committed digest.
    Data,
}

/// A typed refusal naming the first disagreement between a receipt and its
/// served logs.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum SequencerLogRefusal {
    /// The receipt is not a sequencer-layout `ProtocolReceipt` v2.
    ReceiptShape,
    /// Effect 0 is absent or is not an execution summary of this receipt.
    SummaryEffect,
    /// The summary body is not the canonical summary encoding.
    SummaryBody,
    /// The served log at `index` disagrees with effect `index + 1`.
    Log {
        /// Position of the log in the served list.
        index: usize,
        /// The first field that disagreed.
        field: LogField,
    },
    /// The summary log count, the served list length and the number of log
    /// effects are not all equal.
    LogCount {
        /// Log count carried by the summary.
        summary: u32,
        /// Length of the served list.
        listed: usize,
        /// Number of log effects in the receipt.
        effects: usize,
    },
}

fn sequencer_layout(receipt: &Receipt) -> Result<&ProtocolReceipt, SequencerLogRefusal> {
    let protocol = receipt
        .protocol()
        .ok_or(SequencerLogRefusal::ReceiptShape)?;
    if protocol.protocol_version() != PROTOCOL_VERSION
        || protocol.module_id() <= KERNEL_MODULE_COUNT
        || protocol.module_version() != SEQUENCER_LAYOUT_VERSION
        || protocol.total_units().is_some()
        || protocol.program_outcome().is_some()
    {
        return Err(SequencerLogRefusal::ReceiptShape);
    }
    Ok(protocol)
}

struct Reader<'a> {
    bytes: &'a [u8],
}

impl<'a> Reader<'a> {
    fn take<const N: usize>(&mut self) -> Result<[u8; N], SequencerLogRefusal> {
        let (head, rest) = self
            .bytes
            .split_first_chunk::<N>()
            .ok_or(SequencerLogRefusal::SummaryBody)?;
        self.bytes = rest;
        Ok(*head)
    }

    fn rest(self) -> &'a [u8] {
        self.bytes
    }
}

fn decode_summary_body(body: &[u8]) -> Result<ReceiptSummary, SequencerLogRefusal> {
    if body.len() < SUMMARY_FIXED_BYTES {
        return Err(SequencerLogRefusal::SummaryBody);
    }
    let mut reader = Reader { bytes: body };
    let gas_used = u64::from_be_bytes(reader.take()?);
    let cumulative_gas_used = u64::from_be_bytes(reader.take()?);
    let effective_gas_price = u64::from_be_bytes(reader.take()?);
    let tx_index = u32::from_be_bytes(reader.take()?);
    let contract_address = reader.take()?;
    let sponsor = reader.take()?;
    let vm_error_hash = reader.take()?;
    let return_data_len = u32::from_be_bytes(reader.take()?);
    let log_count = u32::from_be_bytes(reader.take()?);
    let [denom_len] = reader.take::<1>()?;
    let denom = reader.rest();
    if denom_len == 0 || usize::from(denom_len) > MAX_FEE_DENOM_BYTES {
        return Err(SequencerLogRefusal::SummaryBody);
    }
    if denom.len() != usize::from(denom_len) {
        return Err(SequencerLogRefusal::SummaryBody);
    }
    let fee_denom = std::str::from_utf8(denom)
        .map_err(|_| SequencerLogRefusal::SummaryBody)?
        .to_owned();
    Ok(ReceiptSummary {
        gas_used,
        cumulative_gas_used,
        effective_gas_price,
        tx_index,
        contract_address,
        sponsor,
        vm_error_hash,
        return_data_len,
        log_count,
        fee_denom,
    })
}

fn summary_of(protocol: &ProtocolReceipt) -> Result<ReceiptSummary, SequencerLogRefusal> {
    let effect = protocol
        .effects()
        .first()
        .ok_or(SequencerLogRefusal::SummaryEffect)?;
    if effect.module_id() != protocol.module_id()
        || effect.ordinal() != 0
        || effect.event_type() != 0
        || effect.kind() != KIND_STATE
        || effect.monetary()
        || effect.transfer_set_root() != [0; 32]
    {
        return Err(SequencerLogRefusal::SummaryEffect);
    }
    decode_summary_body(effect.body())
}

/// Decodes the execution summary from effect 0 of a sequencer receipt.
///
/// The receipt must already have passed
/// [`crate::receipt::verify_sequencer_signature`]; this reads its signed
/// effects and does not authenticate them.
///
/// # Errors
///
/// Refuses a receipt that is not the sequencer layout (`ProtocolReceipt` v2,
/// module after the kernel modules, module version 1, no supply and no program
/// outcome), an effect 0 that is not this module's execution summary, and a
/// summary body that is not its exact canonical encoding with a 1 to 64 byte
/// UTF-8 fee denom.
pub fn decode_summary(receipt: &Receipt) -> Result<ReceiptSummary, SequencerLogRefusal> {
    summary_of(sequencer_layout(receipt)?)
}

fn check_log(
    module_id: u16,
    index: usize,
    effect: Option<&Effect>,
    log: &SequencerLog,
) -> Result<(), LogField> {
    let effect = effect.ok_or(LogField::MissingEffect)?;
    if effect.module_id() != module_id
        || effect.event_type() != 0
        || effect.kind() != KIND_EVENT
        || effect.monetary()
        || effect.transfer_set_root() != [0; 32]
    {
        return Err(LogField::Effect);
    }
    if index.checked_add(1).and_then(|n| u16::try_from(n).ok()) != Some(effect.ordinal()) {
        return Err(LogField::Ordinal);
    }
    if u64::try_from(index).ok() != Some(log.log_index) {
        return Err(LogField::LogIndex);
    }
    let body = effect.body();
    let (address, rest) = body
        .split_first_chunk::<ADDRESS_BYTES>()
        .ok_or(LogField::Body)?;
    let (&topic_count, rest) = rest.split_first().ok_or(LogField::Body)?;
    let topic_count = usize::from(topic_count);
    if topic_count > MAX_LOG_TOPICS || rest.len() != (topic_count + 1) * WORD_BYTES {
        return Err(LogField::Body);
    }
    let (topics, data_digest) = rest.split_at(topic_count * WORD_BYTES);
    if *address != log.address {
        return Err(LogField::Address);
    }
    if log.topics.len() != topic_count
        || topics
            .chunks_exact(WORD_BYTES)
            .zip(&log.topics)
            .any(|(committed, served)| committed != served.as_slice())
    {
        return Err(LogField::Topics);
    }
    if Sha256::digest(&log.data).as_slice() != data_digest {
        return Err(LogField::Data);
    }
    Ok(())
}

/// Binds the `pax_sequencerReceipt` log list to the signed effects of its
/// receipt.
///
/// Log `n` of the list must match effect `n + 1`: a log effect of the
/// receipt's module with ordinal `n + 1`, served log index `n`, and a body
/// equal to address, topic count, topics and sha256 of the served data. The
/// list length must then equal the summary log count, which must equal the
/// number of log effects. The receipt must already have passed
/// [`crate::receipt::verify_sequencer_signature`].
///
/// # Errors
///
/// Returns the summary refusals of [`decode_summary`], then
/// [`SequencerLogRefusal::Log`] naming the first log that disagrees with its
/// effect and the field that disagreed, then
/// [`SequencerLogRefusal::LogCount`] when the counts are not all equal.
pub fn verify_logs(
    receipt: &Receipt,
    logs: Vec<SequencerLog>,
) -> Result<VerifiedLogs, SequencerLogRefusal> {
    let protocol = sequencer_layout(receipt)?;
    let summary = summary_of(protocol)?;
    let log_effects = protocol.effects().get(1..).unwrap_or_default();
    for (index, log) in logs.iter().enumerate() {
        check_log(protocol.module_id(), index, log_effects.get(index), log)
            .map_err(|field| SequencerLogRefusal::Log { index, field })?;
    }
    let summary_count = usize::try_from(summary.log_count).ok();
    if summary_count != Some(logs.len()) || summary_count != Some(log_effects.len()) {
        return Err(SequencerLogRefusal::LogCount {
            summary: summary.log_count,
            listed: logs.len(),
            effects: log_effects.len(),
        });
    }
    Ok(VerifiedLogs { summary, logs })
}
