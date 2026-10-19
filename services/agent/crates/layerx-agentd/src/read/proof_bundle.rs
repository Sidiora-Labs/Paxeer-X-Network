use layerx_agent_api::prepare::CanonicalBytes;
use layerx_agent_api::read::{BatchRef, CheckpointRef, Freshness, RelativeTo, VerifiedRead};
use layerx_agent_api::proof::{ProofBundle, ProofBundleRecord, ProofBundleTarget, ProofBundleVariant};
use layerx_agent_api::verify::Level;
use layerx_agent_api::Sequence;
use layerx_client::evidence::{ProofBundleSelector, SignedHeader, VerifiedProofBundle};
use layerx_client::Client;
use layerx_proof::merkle::Proof;
use layerx_types::payload::ModuleRegistry;
use layerx_wire::receipt::decode_batch_header;

use crate::human::HumanOperationError;

pub(crate) struct ProofBundleScope<'a> {
    pub account: [u8; 32],
    pub asset: [u8; 32],
    pub retained_activity: &'a [u8],
    pub served_receipt: &'a [u8],
    pub correlation: u64,
}

fn canonical(bytes: Vec<u8>) -> Result<CanonicalBytes, HumanOperationError> {
    CanonicalBytes::new(bytes).map_err(|_| HumanOperationError::Refused)
}

fn sp1(proof: &Proof) -> Result<Vec<u8>, HumanOperationError> {
    let depth = u8::try_from(proof.siblings().len()).map_err(|_| HumanOperationError::Refused)?;
    if usize::from(depth) > layerx_proof::merkle::MAX_DEPTH {
        return Err(HumanOperationError::Refused);
    }
    let mut bytes = Vec::with_capacity(9 + usize::from(depth) * 32);
    bytes.extend_from_slice(&proof.leaf_index().to_be_bytes());
    bytes.extend_from_slice(&proof.leaf_count().to_be_bytes());
    bytes.push(depth);
    for sibling in proof.siblings() { bytes.extend_from_slice(sibling); }
    Ok(bytes)
}

fn inclusion(kind: u8, activity: [u8; 32], proof: &Proof, header: &SignedHeader)
    -> Result<Vec<u8>, HumanOperationError>
{
    let mut bytes = vec![0, 1, kind];
    bytes.extend_from_slice(&activity);
    bytes.extend_from_slice(&sp1(proof)?);
    bytes.extend_from_slice(&[0, 1]);
    bytes.extend_from_slice(&header.sequencer_id);
    bytes.extend_from_slice(&header.public_key);
    bytes.extend_from_slice(&header.first_batch_number.to_be_bytes());
    bytes.extend_from_slice(&header.last_batch_number.to_be_bytes());
    let length = u32::try_from(header.canonical_bytes.len()).map_err(|_| HumanOperationError::Refused)?;
    bytes.extend_from_slice(&length.to_be_bytes());
    bytes.extend_from_slice(&header.canonical_bytes);
    bytes.extend_from_slice(&header.signature);
    Ok(bytes)
}

fn evidence_error(error: layerx_client::evidence::EvidenceError) -> HumanOperationError {
    match error {
        layerx_client::evidence::EvidenceError::Unavailable => HumanOperationError::Unavailable,
        _ => HumanOperationError::Refused,
    }
}

pub(crate) fn acquire(
    node: &mut Client, registry: &ModuleRegistry, target: ProofBundleTarget,
    requested: Level, scope: ProofBundleScope<'_>,
) -> Result<VerifiedRead<ProofBundle>, HumanOperationError> {
    let target_bytes = target.encode().map_err(|_| HumanOperationError::Refused)?;
    let achieved = match target {
        ProofBundleTarget::AccountState { account_id, .. } => {
            if account_id != scope.account { return Err(HumanOperationError::Refused); }
            Level::StateProven
        }
        ProofBundleTarget::Activity(_) | ProofBundleTarget::Receipt(_) => Level::BatchIncluded,
    };
    if requested > achieved || scope.account == [0; 32] || scope.asset == [0; 32]
        || scope.correlation == 0 || scope.retained_activity.is_empty() || scope.served_receipt.is_empty() {
        return Err(HumanOperationError::Refused);
    }
    node.reconnect().map_err(|_| HumanOperationError::Unavailable)?;
    let head = node.head();
    let network = node.handshake().node().network_id;
    let protocol = node.handshake().node().protocol_version;
    let sequencer_key = node.handshake().node().authorised_sequencer_key;
    if head.chain_sequence == 0 || head.sealed_batch == 0 { return Err(HumanOperationError::Unavailable); }
    let activity_id = target.activity_id();
    let activity = node.proof_bundle(ProofBundleSelector::Activity(activity_id), scope.correlation, registry)
        .map_err(evidence_error)?;
    let receipt = node.proof_bundle(ProofBundleSelector::Receipt(activity_id),
        scope.correlation.checked_add(2).ok_or(HumanOperationError::Refused)?, registry)
        .map_err(evidence_error)?;
    if activity.canonical_bytes() != scope.retained_activity || receipt.canonical_bytes() != scope.served_receipt
        || activity.signed_header() != receipt.signed_header() {
        return Err(HumanOperationError::Refused);
    }
    let decoded_activity = layerx_wire::activity::decode_signed(activity.canonical_bytes(), registry)
        .map_err(|_| HumanOperationError::Refused)?;
    let decoded_receipt = layerx_wire::receipt::decode(receipt.canonical_bytes())
        .map_err(|_| HumanOperationError::Refused)?;
    let anchor = decoded_receipt.protocol().ok_or(HumanOperationError::Refused)?;
    if decoded_activity.network_id() != network || decoded_activity.protocol_version() != protocol
        || anchor.activity_id() != activity_id || anchor.asset() != scope.asset {
        return Err(HumanOperationError::Refused);
    }
    let anchor_sequence = anchor.global_sequence();
    let anchor_header = activity.signed_header().clone();
    let bundle = match target {
        ProofBundleTarget::Activity(_) => activity,
        ProofBundleTarget::Receipt(_) => receipt,
        ProofBundleTarget::AccountState { account_id, .. } => node.proof_bundle(
            ProofBundleSelector::AccountState { activity_id, account_id },
            scope.correlation.checked_add(4).ok_or(HumanOperationError::Refused)?, registry)
            .map_err(evidence_error)?,
    };
    let header = decode_batch_header(&bundle.signed_header().canonical_bytes)
        .map_err(|_| HumanOperationError::Refused)?;
    if bundle.signed_header() != &anchor_header
        || header.network_id() != network || header.protocol_version() != protocol
        || header.batch_number() == 0 || header.batch_number() > head.sealed_batch
        || header.last_sequence() > head.chain_sequence || anchor_sequence > header.last_sequence()
        || anchor_sequence < header.first_sequence() {
        return Err(HumanOperationError::Refused);
    }
    let value_sequence = if matches!(target, ProofBundleTarget::AccountState { .. }) {
        if header.batch_number() != head.sealed_batch || header.last_sequence() != head.chain_sequence {
            return Err(HumanOperationError::Unavailable);
        }
        header.last_sequence()
    } else { anchor_sequence };
    let record = match &bundle {
        VerifiedProofBundle::Activity { canonical_bytes, activity_id: id, proof, signed_header }
            if target == ProofBundleTarget::Activity(*id) => ProofBundleRecord {
                variant: ProofBundleVariant::Activity, canonical_value: canonical(canonical_bytes.clone())?,
                native_proof: canonical(inclusion(1, *id, proof, signed_header)?)?, activity_receipt: None,
            },
        VerifiedProofBundle::Receipt { canonical_bytes, activity_id: id, proof, signed_header }
            if target == ProofBundleTarget::Receipt(*id) => ProofBundleRecord {
                variant: ProofBundleVariant::Receipt, canonical_value: canonical(canonical_bytes.clone())?,
                native_proof: canonical(inclusion(3, *id, proof, signed_header)?)?, activity_receipt: None,
            },
        VerifiedProofBundle::Account { canonical_bytes, proof_material, activity_id: id, verified, .. }
            if matches!(target, ProofBundleTarget::AccountState { .. }) && *id == activity_id => {
                if verified.account().account_id != scope.account || verified.account().asset_id() != scope.asset {
                    return Err(HumanOperationError::Refused);
                }
                bundle.account_proof(scope.account, protocol, network).map_err(evidence_error)?;
                ProofBundleRecord { variant: ProofBundleVariant::Account,
                    canonical_value: canonical(canonical_bytes.clone())?, native_proof: canonical(proof_material.clone())?,
                    activity_receipt: None }
            }
        VerifiedProofBundle::MaintainedAccount { canonical_bytes, proof_material, activity_id: id,
            activity_receipt, activity_receipt_proof, verified, .. }
            if matches!(target, ProofBundleTarget::AccountState { .. }) && *id == activity_id => {
                if verified.account().account_id != scope.account || verified.account().asset_id() != scope.asset
                    || activity_receipt.as_slice() != scope.served_receipt {
                    return Err(HumanOperationError::Refused);
                }
                bundle.account_proof(scope.account, protocol, network).map_err(evidence_error)?;
                ProofBundleRecord { variant: ProofBundleVariant::MaintainedAccount,
                    canonical_value: canonical(canonical_bytes.clone())?, native_proof: canonical(proof_material.clone())?,
                    activity_receipt: Some((canonical(activity_receipt.clone())?, canonical(sp1(activity_receipt_proof)?)?)) }
            }
        _ => return Err(HumanOperationError::Refused),
    };
    let value = ProofBundle { target: target_bytes,
        proofs: vec![record.encode().map_err(|_| HumanOperationError::Refused)?] };
    value.record().map_err(|_| HumanOperationError::Refused)?;
    node.reconnect().map_err(|_| HumanOperationError::Unavailable)?;
    let current = node.head();
    if current.chain_sequence != head.chain_sequence || current.sealed_batch != head.sealed_batch
        || current.finalised_checkpoint != head.finalised_checkpoint
        || node.handshake().node().network_id != network || node.handshake().node().protocol_version != protocol
        || node.handshake().node().authorised_sequencer_key != sequencer_key {
        return Err(HumanOperationError::Unavailable);
    }
    let batch = BatchRef::new(header.batch_number().to_string()).map_err(|_| HumanOperationError::Refused)?;
    Ok(VerifiedRead::new(value, achieved, Freshness {
        chain_head: Sequence(head.chain_sequence),
        latest_sealed_batch: BatchRef::new(head.sealed_batch.to_string()).map_err(|_| HumanOperationError::Refused)?,
        latest_finalised_checkpoint: CheckpointRef::new(layerx_programs::hex::encode(&head.finalised_checkpoint))
            .map_err(|_| HumanOperationError::Refused)?,
        value_sequence: Sequence(value_sequence), relative_to: RelativeTo::Batch(batch),
    }))
}
