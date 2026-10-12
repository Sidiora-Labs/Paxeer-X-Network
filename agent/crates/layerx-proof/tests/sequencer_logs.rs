use std::fmt::Debug;

use ed25519_dalek::{Signer as _, SigningKey};
use layerx_proof::inclusion::SequencerAuthorization;
use layerx_proof::receipt::{verify_sequencer_signature, ReceiptCheck};
use layerx_proof::sequencer_logs::{
    decode_summary, verify_logs, LogField, SequencerLog, SequencerLogRefusal,
};
use layerx_wire::encode::Encoder;
use layerx_wire::hash::receipt_digest;
use layerx_wire::limits::{MAX_MESSAGE_BYTES, MAX_SEQUENCER_EFFECTS, PROTOCOL_VERSION};
use layerx_wire::receipt::Receipt;
use layerx_wire::WireError;
use sha2::{Digest as _, Sha256};

type TestResult = Result<(), String>;

const MODULE_ID: u16 = 12;
const KEY_ID: [u8; 32] = [0x33; 32];
const HEIGHT: u64 = 26_400_000;
const SIGNING_SEED: [u8; 32] = [7; 32];
const ACTIVITY_ID: [u8; 32] = [0xac; 32];
const PREVIOUS_SEQ_HASH: [u8; 32] = [0x04; 32];
const SEQUENCE: u64 = 7;
const FEE_DENOM: &str = "upax";
const SPONSOR: [u8; 20] = [0x5a; 20];
const KIND_STATE: u8 = 1;
const KIND_EVENT: u8 = 3;

fn fail(error: impl Debug) -> String {
    format!("{error:?}")
}

fn sha256(bytes: &[u8]) -> [u8; 32] {
    Sha256::digest(bytes).into()
}

fn return_data() -> [u8; 32] {
    let mut data = [0; 32];
    data[31] = 1;
    data
}

fn served_logs() -> Vec<SequencerLog> {
    vec![
        SequencerLog::new(
            [0x11; 20],
            vec![[0xa1; 32], [0xa2; 32]],
            b"transfer amount".to_vec(),
            0,
        ),
        SequencerLog::new([0x22; 20], Vec::new(), Vec::new(), 1),
        SequencerLog::new(
            [0x11; 20],
            vec![[0xb1; 32]],
            vec![0xde, 0xad, 0xbe, 0xef],
            2,
        ),
    ]
}

fn summary_body(log_count: u32) -> Vec<u8> {
    let mut body = Vec::new();
    body.extend_from_slice(&52_000_u64.to_be_bytes());
    body.extend_from_slice(&104_000_u64.to_be_bytes());
    body.extend_from_slice(&1_000_000_000_u64.to_be_bytes());
    body.extend_from_slice(&3_u32.to_be_bytes());
    body.extend_from_slice(&[0; 20]);
    body.extend_from_slice(&SPONSOR);
    body.extend_from_slice(&sha256(b""));
    body.extend_from_slice(&32_u32.to_be_bytes());
    body.extend_from_slice(&log_count.to_be_bytes());
    body.push(u8::try_from(FEE_DENOM.len()).unwrap_or(u8::MAX));
    body.extend_from_slice(FEE_DENOM.as_bytes());
    body
}

fn log_body(log: &SequencerLog) -> Vec<u8> {
    let mut body = log.address().to_vec();
    body.push(u8::try_from(log.topics().len()).unwrap_or(u8::MAX));
    for topic in log.topics() {
        body.extend_from_slice(topic);
    }
    body.extend_from_slice(&sha256(log.data()));
    body
}

struct EffectSpec {
    ordinal: u16,
    kind: u8,
    body: Vec<u8>,
}

struct ReceiptSpec {
    module_version: u32,
    effects: Vec<EffectSpec>,
}

impl ReceiptSpec {
    fn with_summary_count(logs: &[SequencerLog], summary_count: u32) -> Self {
        let mut effects = vec![EffectSpec {
            ordinal: 0,
            kind: KIND_STATE,
            body: summary_body(summary_count),
        }];
        for (ordinal, log) in (1_u16..).zip(logs) {
            effects.push(EffectSpec {
                ordinal,
                kind: KIND_EVENT,
                body: log_body(log),
            });
        }
        Self {
            module_version: 1,
            effects,
        }
    }

    fn for_logs(logs: &[SequencerLog]) -> Result<Self, String> {
        let count = u32::try_from(logs.len()).map_err(fail)?;
        Ok(Self::with_summary_count(logs, count))
    }

    fn encode(&self, signature: Option<[u8; 64]>) -> Result<Vec<u8>, WireError> {
        let mut seq_hash_preimage = b"PAXEER/v1/sequence\0".to_vec();
        seq_hash_preimage.extend_from_slice(&PREVIOUS_SEQ_HASH);
        seq_hash_preimage.extend_from_slice(&SEQUENCE.to_be_bytes());
        seq_hash_preimage.extend_from_slice(&ACTIVITY_ID);
        let mut batch_id = [0; 32];
        batch_id[24..].copy_from_slice(&HEIGHT.to_be_bytes());
        let mut from = [0; 32];
        from[12..].copy_from_slice(&[0xaa; 20]);
        let mut to = [0; 32];
        to[12..].copy_from_slice(&[0xbb; 20]);

        let mut encoder = Encoder::new(MAX_MESSAGE_BYTES);
        encoder.structure_header_version(0x5201, PROTOCOL_VERSION)?;
        encoder.u16(PROTOCOL_VERSION)?;
        encoder.bytes(&ACTIVITY_ID, 32)?;
        encoder.u64(SEQUENCE)?;
        encoder.bytes(&PREVIOUS_SEQ_HASH, 32)?;
        encoder.bytes(&sha256(&seq_hash_preimage), 32)?;
        encoder.bytes(&sha256(&return_data()), 32)?;
        encoder.i32(0)?;
        encoder.sequence_length(self.effects.len(), MAX_SEQUENCER_EFFECTS)?;
        for effect in &self.effects {
            encoder.u16(MODULE_ID)?;
            encoder.u16(effect.ordinal)?;
            encoder.u16(0)?;
            encoder.tag(effect.kind, 3)?;
            encoder.u8(0)?;
            encoder.bytes(&[0; 32], 32)?;
            encoder.bytes(&effect.body, 256)?;
        }
        encoder.u128(52_000_000_000_000)?;
        encoder.bytes(&batch_id, 32)?;
        encoder.u16(MODULE_ID)?;
        encoder.u32(self.module_version)?;
        encoder.u32(0)?;
        encoder.u8(2)?;
        encoder.bytes(&sha256(FEE_DENOM.as_bytes()), 32)?;
        encoder.u128(1_000_000_000_000_000_000)?;
        encoder.bytes(&from, 32)?;
        encoder.u128(5_000_000_000_000_000_000)?;
        encoder.u128(3_999_948_000_000_000_000)?;
        encoder.u64(41)?;
        encoder.bytes(&to, 32)?;
        encoder.u128(2_000_000_000_000_000_000)?;
        encoder.u128(3_000_000_000_000_000_000)?;
        encoder.bytes(&[0; 32], 32)?;
        encoder.bytes(&[0; 32], 32)?;
        encoder.bytes(&KEY_ID, 32)?;
        encoder.u64(1_750_000_000_250)?;
        encoder.u8(u8::from(signature.is_some()))?;
        if let Some(signature) = signature {
            encoder.bytes(&signature, 64)?;
        }
        Ok(encoder.finish())
    }

    fn sign(&self, key: &SigningKey) -> Result<Vec<u8>, String> {
        let unsigned = self.encode(None).map_err(fail)?;
        let digest = receipt_digest(&unsigned).map_err(fail)?;
        self.encode(Some(key.sign(&digest).to_bytes()))
            .map_err(fail)
    }

    fn verified(&self) -> Result<Receipt, String> {
        let key = SigningKey::from_bytes(&SIGNING_SEED);
        let bytes = self.sign(&key)?;
        let pinned = [SequencerAuthorization::new(
            KEY_ID,
            key.verifying_key().to_bytes(),
            HEIGHT - 100,
            HEIGHT + 100,
        )];
        verify_sequencer_signature(&bytes, &pinned).map_err(fail)
    }
}

fn log_refusal(index: usize, field: LogField) -> SequencerLogRefusal {
    SequencerLogRefusal::Log { index, field }
}

#[test]
fn matching_log_list_verifies_against_the_signed_effects() -> TestResult {
    let logs = served_logs();
    let spec = ReceiptSpec::for_logs(&logs)?;
    let receipt = spec.verified()?;

    let summary = decode_summary(&receipt).map_err(fail)?;
    assert_eq!(summary.gas_used(), 52_000);
    assert_eq!(summary.cumulative_gas_used(), 104_000);
    assert_eq!(summary.effective_gas_price(), 1_000_000_000);
    assert_eq!(summary.tx_index(), 3);
    assert_eq!(summary.contract_address(), [0; 20]);
    assert_eq!(summary.sponsor(), SPONSOR);
    assert_eq!(summary.vm_error_hash(), sha256(b""));
    assert_eq!(summary.return_data_len(), 32);
    assert_eq!(summary.log_count(), 3);
    assert_eq!(summary.fee_denom(), FEE_DENOM);

    let verified = verify_logs(&receipt, logs.clone()).map_err(fail)?;
    assert_eq!(verified.summary(), &summary);
    assert_eq!(verified.logs(), logs.as_slice());
    assert_eq!(verified.into_logs(), logs);

    let other_key = SigningKey::from_bytes(&[8; 32]);
    let pinned = [SequencerAuthorization::new(
        KEY_ID,
        other_key.verifying_key().to_bytes(),
        HEIGHT - 100,
        HEIGHT + 100,
    )];
    let bytes = spec.sign(&SigningKey::from_bytes(&SIGNING_SEED))?;
    assert_eq!(
        verify_sequencer_signature(&bytes, &pinned).map(|_| ()),
        Err(layerx_proof::receipt::VerificationFailure {
            check: ReceiptCheck::SequencerSignature
        })
    );
    Ok(())
}

#[test]
fn empty_log_list_verifies_against_a_receipt_without_logs() -> TestResult {
    let receipt = ReceiptSpec::for_logs(&[])?.verified()?;
    let verified = verify_logs(&receipt, Vec::new()).map_err(fail)?;
    assert_eq!(verified.summary().log_count(), 0);
    assert!(verified.logs().is_empty());
    Ok(())
}

#[test]
fn altered_log_fields_are_refused_at_the_first_mismatch() -> TestResult {
    let logs = served_logs();
    let receipt = ReceiptSpec::for_logs(&logs)?.verified()?;

    let mut altered_data = logs.clone();
    altered_data[1] = SequencerLog::new([0x22; 20], Vec::new(), vec![0x00], 1);
    assert_eq!(
        verify_logs(&receipt, altered_data),
        Err(log_refusal(1, LogField::Data))
    );

    let mut altered_byte = logs.clone();
    altered_byte[0] = SequencerLog::new(
        [0x11; 20],
        vec![[0xa1; 32], [0xa2; 32]],
        b"transfer amounT".to_vec(),
        0,
    );
    altered_byte[2] = SequencerLog::new([0x11; 20], vec![[0xb1; 32]], vec![0xde], 2);
    assert_eq!(
        verify_logs(&receipt, altered_byte),
        Err(log_refusal(0, LogField::Data))
    );

    let mut altered_topic = logs.clone();
    altered_topic[2] = SequencerLog::new(
        [0x11; 20],
        vec![[0xb2; 32]],
        vec![0xde, 0xad, 0xbe, 0xef],
        2,
    );
    assert_eq!(
        verify_logs(&receipt, altered_topic),
        Err(log_refusal(2, LogField::Topics))
    );

    let mut extra_topic = logs.clone();
    extra_topic[1] = SequencerLog::new([0x22; 20], vec![[0; 32]], Vec::new(), 1);
    assert_eq!(
        verify_logs(&receipt, extra_topic),
        Err(log_refusal(1, LogField::Topics))
    );

    let mut altered_address = logs;
    altered_address[1] = SequencerLog::new([0x23; 20], Vec::new(), Vec::new(), 1);
    assert_eq!(
        verify_logs(&receipt, altered_address),
        Err(log_refusal(1, LogField::Address))
    );
    Ok(())
}

#[test]
fn dropped_logs_are_refused() -> TestResult {
    let logs = served_logs();
    let receipt = ReceiptSpec::for_logs(&logs)?.verified()?;

    let mut dropped_middle = logs.clone();
    dropped_middle.remove(1);
    assert_eq!(
        verify_logs(&receipt, dropped_middle),
        Err(log_refusal(1, LogField::LogIndex))
    );

    let renumbered_after_drop = vec![
        logs[0].clone(),
        SequencerLog::new(
            [0x11; 20],
            vec![[0xb1; 32]],
            vec![0xde, 0xad, 0xbe, 0xef],
            1,
        ),
    ];
    assert_eq!(
        verify_logs(&receipt, renumbered_after_drop),
        Err(log_refusal(1, LogField::Address))
    );

    let mut dropped_last = logs;
    dropped_last.pop();
    assert_eq!(
        verify_logs(&receipt, dropped_last),
        Err(SequencerLogRefusal::LogCount {
            summary: 3,
            listed: 2,
            effects: 3,
        })
    );

    assert_eq!(
        verify_logs(&receipt, Vec::new()),
        Err(SequencerLogRefusal::LogCount {
            summary: 3,
            listed: 0,
            effects: 3,
        })
    );
    Ok(())
}

#[test]
fn reordered_logs_are_refused() -> TestResult {
    let logs = served_logs();
    let receipt = ReceiptSpec::for_logs(&logs)?.verified()?;

    let mut swapped = logs.clone();
    swapped.swap(0, 1);
    assert_eq!(
        verify_logs(&receipt, swapped),
        Err(log_refusal(0, LogField::LogIndex))
    );

    let renumbered_swap = vec![
        SequencerLog::new([0x22; 20], Vec::new(), Vec::new(), 0),
        SequencerLog::new(
            [0x11; 20],
            vec![[0xa1; 32], [0xa2; 32]],
            b"transfer amount".to_vec(),
            1,
        ),
        logs[2].clone(),
    ];
    assert_eq!(
        verify_logs(&receipt, renumbered_swap),
        Err(log_refusal(0, LogField::Address))
    );

    let same_address_swap = vec![
        SequencerLog::new(
            [0x11; 20],
            vec![[0xb1; 32]],
            vec![0xde, 0xad, 0xbe, 0xef],
            0,
        ),
        logs[1].clone(),
        SequencerLog::new(
            [0x11; 20],
            vec![[0xa1; 32], [0xa2; 32]],
            b"transfer amount".to_vec(),
            2,
        ),
    ];
    assert_eq!(
        verify_logs(&receipt, same_address_swap),
        Err(log_refusal(0, LogField::Topics))
    );

    let mut misordered_effects = ReceiptSpec::for_logs(&logs)?;
    misordered_effects.effects.swap(1, 2);
    let receipt = misordered_effects.verified()?;
    assert_eq!(
        verify_logs(&receipt, logs),
        Err(log_refusal(0, LogField::Ordinal))
    );
    Ok(())
}

#[test]
fn summary_log_count_must_equal_the_list_and_the_log_effects() -> TestResult {
    let logs = served_logs();

    let overcounted = ReceiptSpec::with_summary_count(&logs, 4).verified()?;
    assert_eq!(decode_summary(&overcounted).map_err(fail)?.log_count(), 4);
    assert_eq!(
        verify_logs(&overcounted, logs.clone()),
        Err(SequencerLogRefusal::LogCount {
            summary: 4,
            listed: 3,
            effects: 3,
        })
    );

    let undercounted = ReceiptSpec::with_summary_count(&logs, 2).verified()?;
    assert_eq!(
        verify_logs(&undercounted, logs.clone()),
        Err(SequencerLogRefusal::LogCount {
            summary: 2,
            listed: 3,
            effects: 3,
        })
    );
    assert_eq!(
        verify_logs(&undercounted, logs[..2].to_vec()),
        Err(SequencerLogRefusal::LogCount {
            summary: 2,
            listed: 2,
            effects: 3,
        })
    );

    let receipt = ReceiptSpec::for_logs(&logs)?.verified()?;
    let mut extra = logs;
    extra.push(SequencerLog::new([0x44; 20], Vec::new(), Vec::new(), 3));
    assert_eq!(
        verify_logs(&receipt, extra),
        Err(log_refusal(3, LogField::MissingEffect))
    );
    Ok(())
}

#[test]
fn malformed_summary_and_layout_are_refused() -> TestResult {
    let logs = served_logs();

    let mut empty_denom = ReceiptSpec::for_logs(&logs)?;
    let fixed = empty_denom.effects[0].body.len() - FEE_DENOM.len() - 1;
    empty_denom.effects[0].body.truncate(fixed);
    empty_denom.effects[0].body.push(0);
    let receipt = empty_denom.verified()?;
    assert_eq!(
        decode_summary(&receipt),
        Err(SequencerLogRefusal::SummaryBody)
    );
    assert_eq!(
        verify_logs(&receipt, logs.clone()),
        Err(SequencerLogRefusal::SummaryBody)
    );

    let mut short_denom = ReceiptSpec::for_logs(&logs)?;
    short_denom.effects[0].body.pop();
    assert_eq!(
        decode_summary(&short_denom.verified()?),
        Err(SequencerLogRefusal::SummaryBody)
    );

    let mut summary_as_event = ReceiptSpec::for_logs(&logs)?;
    summary_as_event.effects[0].kind = KIND_EVENT;
    assert_eq!(
        verify_logs(&summary_as_event.verified()?, logs.clone()),
        Err(SequencerLogRefusal::SummaryEffect)
    );

    let mut log_as_state = ReceiptSpec::for_logs(&logs)?;
    log_as_state.effects[2].kind = KIND_STATE;
    assert_eq!(
        verify_logs(&log_as_state.verified()?, logs.clone()),
        Err(log_refusal(1, LogField::Effect))
    );

    let mut truncated_log_body = ReceiptSpec::for_logs(&logs)?;
    truncated_log_body.effects[3].body.pop();
    assert_eq!(
        verify_logs(&truncated_log_body.verified()?, logs.clone()),
        Err(log_refusal(2, LogField::Body))
    );

    let mut other_layout = ReceiptSpec::for_logs(&logs)?;
    other_layout.module_version = 2;
    assert_eq!(
        verify_logs(&other_layout.verified()?, logs),
        Err(SequencerLogRefusal::ReceiptShape)
    );
    Ok(())
}
