use std::{fs, path::Path};

use ed25519_dalek::{Signer as _, SigningKey};
use layerx_proof::inclusion::{verify_header, SequencerAuthorization};
use layerx_proof::merkle::{decode_proof, Proof};
use layerx_proof::receipt::{
    authorized_maintained_activity_batch_chain, verify_outcome_maintained_chain,
    verify_program_outcome, verify_program_preexecution_rejection,
    verify_program_preexecution_rejection_maintained_chain, AuthorizedBatch,
    MaintainedOutcomeEvidence, ReceiptCheck,
};
use layerx_types::result::{KnownResult, ResultCode, ResultDomain};
use layerx_types::verify::VerificationLevel;
use layerx_wire::receipt::{decode, encode_unsigned};

struct Capture {
    name: String,
    receipt: Vec<u8>,
    header: Vec<u8>,
    signature: [u8; 64],
    proof: Proof,
    maintenance: Vec<u8>,
    maintenance_proof: Proof,
    receipts: Vec<Vec<u8>>,
    authorization: SequencerAuthorization,
    batch: AuthorizedBatch,
}

fn must<T, E: std::fmt::Debug>(value: Result<T, E>) -> T {
    value.unwrap_or_else(|error| panic!("genuine native terminal evidence: {error:?}"))
}

fn read(path: &str) -> Vec<u8> {
    must(fs::read(Path::new(path)))
}

fn hex32(text: &str) -> [u8; 32] {
    assert_eq!(text.len(), 64);
    let mut result = [0; 32];
    for (index, slot) in result.iter_mut().enumerate() {
        *slot = must(u8::from_str_radix(&text[index * 2..index * 2 + 2], 16));
    }
    result
}

fn captures() -> Vec<Capture> {
    let path = must(std::env::var("LAYERX_NATIVE_TERMINAL_RECORDS"));
    let inventory = must(fs::read_to_string(path));
    let mut lines = inventory.lines();
    let pins = lines
        .next()
        .expect("retained native sequencer pins")
        .split('\t')
        .collect::<Vec<_>>();
    assert_eq!(pins.len(), 4);
    let authorization = SequencerAuthorization::new(
        hex32(pins[0]),
        hex32(pins[1]),
        must(pins[2].parse()),
        must(pins[3].parse()),
    );
    let result = lines
        .map(|line| {
            let fields = line.split('\t').collect::<Vec<_>>();
            assert_eq!(fields.len(), 9);
            let receipt = read(fields[1]);
            let proof = must(decode_proof(&read(fields[2])));
            let header = read(fields[3]);
            let signature = must(read(fields[4]).try_into());
            let maintenance = read(fields[5]);
            let maintenance_proof = must(decode_proof(&read(fields[6])));
            let receipts = fields[7].split('|').map(read).collect::<Vec<_>>();
            assert_eq!(fields[8], receipts.len().to_string());
            let checked = must(verify_header(&header, &signature, &authorization));
            let signed = checked.header();
            let decoded = must(decode(&receipt));
            let protocol = decoded.protocol().expect("real native protocol receipt");
            let record = must(layerx_wire::batch_maintenance::decode_maintenance(
                &maintenance,
            ));
            must(record.verify_header(signed));
            let count = must(u32::try_from(
                signed.last_sequence() - signed.first_sequence(),
            ));
            let batch_id = must(layerx_wire::hash::receipt_execution_batch_id_maintenance(
                protocol,
                signed,
                record.occupancy(),
                count,
            ));
            assert_eq!(batch_id, protocol.batch_id());
            let batch = AuthorizedBatch::new(
                batch_id,
                protocol.asset(),
                signed.previous_state_root(),
                signed.resulting_state_root(),
                authorization.public_key(),
            );
            Capture {
                name: fields[0].to_owned(),
                receipt,
                header,
                signature,
                proof,
                maintenance,
                maintenance_proof,
                receipts,
                authorization,
                batch,
            }
        })
        .collect::<Vec<_>>();
    assert_eq!(result.len(), 4);
    result
}

impl Capture {
    fn evidence(&self) -> MaintainedOutcomeEvidence<'_> {
        MaintainedOutcomeEvidence {
            header: &self.header,
            header_signature: &self.signature,
            activity_proof: &self.proof,
            maintenance: &self.maintenance,
            maintenance_proof: &self.maintenance_proof,
            authorization: &self.authorization,
        }
    }

    fn activity_batch(&self) -> AuthorizedBatch {
        must(authorized_maintained_activity_batch_chain(
            &self.receipt,
            &self.batch,
            &self.evidence(),
            &self.receipts,
        ))
    }

    fn resign(&self, bytes: &mut [u8]) {
        let mut seed = [0; 32];
        seed[0] = 0x45;
        let signer = SigningKey::from_bytes(&seed);
        assert_eq!(
            signer.verifying_key().to_bytes(),
            self.authorization.public_key()
        );
        let decoded = must(decode(bytes));
        let digest = must(layerx_wire::hash::receipt_digest(&must(encode_unsigned(
            &decoded,
        ))));
        let offset = bytes.len() - 64;
        bytes[offset..].copy_from_slice(&signer.sign(&digest).to_bytes());
    }
}

#[test]
fn genuine_native_preexecution_rejection_preserves_existing_guest_refusal() {
    let captures = captures();
    let terminal = captures
        .iter()
        .find(|capture| capture.name == "terminal-0")
        .expect("native terminal");
    let decoded = must(decode(&terminal.receipt));
    let protocol = decoded.protocol().expect("native protocol");
    assert_eq!(protocol.result_code(), KnownResult::IdentityFrozen.raw());
    assert!(protocol.program_outcome().is_none());
    assert!(protocol.effects().is_empty());
    assert_eq!(protocol.fee_charged(), 0);
    let activity_batch = terminal.activity_batch();
    assert_eq!(
        verify_program_outcome(&terminal.receipt, &activity_batch)
            .err()
            .map(|failure| failure.check),
        Some(ReceiptCheck::ReceiptShape)
    );
    assert!(verify_outcome_maintained_chain(
        &terminal.receipt,
        &terminal.batch,
        &terminal.evidence(),
        &terminal.receipts
    )
    .is_err());
    let verified = must(verify_program_preexecution_rejection_maintained_chain(
        &terminal.receipt,
        &terminal.batch,
        &terminal.evidence(),
        &terminal.receipts,
    ));
    assert_eq!(verified.canonical_bytes(), terminal.receipt);
    assert_eq!(verified.level(), VerificationLevel::SEQUENCER_SIGNED);
    assert_eq!(
        verified
            .receipt()
            .protocol()
            .expect("verified protocol")
            .result_code(),
        -206
    );
    for capture in captures
        .iter()
        .filter(|capture| capture.name != "terminal-0")
    {
        must(verify_outcome_maintained_chain(
            &capture.receipt,
            &capture.batch,
            &capture.evidence(),
            &capture.receipts,
        ));
        assert!(verify_program_preexecution_rejection_maintained_chain(
            &capture.receipt,
            &capture.batch,
            &capture.evidence(),
            &capture.receipts,
        )
        .is_err());
    }
}

#[test]
fn native_terminal_result_domain_is_closed_and_excludes_replay_and_success() {
    let terminal = captures()
        .into_iter()
        .find(|capture| capture.name == "terminal-0")
        .expect("terminal");
    let batch = terminal.activity_batch();
    assert_eq!(terminal.receipt.len(), 610);
    for known in KnownResult::ALL {
        let mut bytes = terminal.receipt.clone();
        bytes[158..162].copy_from_slice(&known.raw().to_be_bytes());
        terminal.resign(&mut bytes);
        let allowed = *known != KnownResult::IdempotentReplay
            && matches!(
                ResultCode::from(*known).domain(),
                ResultDomain::Codec
                    | ResultDomain::Envelope
                    | ResultDomain::Authority
                    | ResultDomain::Sequencing
                    | ResultDomain::Ledger
                    | ResultDomain::Arithmetic
                    | ResultDomain::Metering
                    | ResultDomain::Module,
            );
        let result = verify_program_preexecution_rejection(&bytes, &batch);
        assert_eq!(result.is_ok(), allowed, "exact native domain for {known:?}");
        if !allowed {
            assert_eq!(
                result.err().map(|failure| failure.check),
                Some(ReceiptCheck::ResultCode)
            );
        }
    }
    for unknown in [-99_i32, -199, -299, -399, -499, -599, -699, -799, 1] {
        let mut bytes = terminal.receipt.clone();
        bytes[158..162].copy_from_slice(&unknown.to_be_bytes());
        terminal.resign(&mut bytes);
        assert_eq!(
            verify_program_preexecution_rejection(&bytes, &batch)
                .err()
                .map(|failure| failure.check),
            Some(ReceiptCheck::ResultCode)
        );
    }
}

#[test]
fn signed_terminal_mutations_cannot_invent_execution_effects_or_identity() {
    let terminal = captures()
        .into_iter()
        .find(|capture| capture.name == "terminal-0")
        .expect("terminal");
    let batch = terminal.activity_batch();
    for offset in [
        166_usize, 233, 265, 285, 317, 333, 349, 361, 393, 409, 429, 465, 501,
    ] {
        let mut bytes = terminal.receipt.clone();
        bytes[offset] = 1;
        terminal.resign(&mut bytes);
        assert_eq!(
            verify_program_preexecution_rejection(&bytes, &batch)
                .err()
                .map(|failure| failure.check),
            Some(ReceiptCheck::ReceiptShape),
            "nonzero native zero-field at {offset}"
        );
    }
    for (offset, length, expected) in [
        (10, 32, ReceiptCheck::ActivityId),
        (42, 8, ReceiptCheck::ActivityId),
        (126, 32, ReceiptCheck::ActivityId),
        (186, 32, ReceiptCheck::BatchId),
        (54, 32, ReceiptCheck::PreviousStateRoot),
        (90, 32, ReceiptCheck::ResultingStateRoot),
    ] {
        let mut bytes = terminal.receipt.clone();
        bytes[offset..offset + length].fill(0);
        terminal.resign(&mut bytes);
        assert_eq!(
            verify_program_preexecution_rejection(&bytes, &batch)
                .err()
                .map(|failure| failure.check),
            Some(expected)
        );
    }
    for offset in [219_usize, 223, 228] {
        let mut bytes = terminal.receipt.clone();
        bytes[offset] ^= 1;
        terminal.resign(&mut bytes);
        assert_eq!(
            verify_program_preexecution_rejection(&bytes, &batch)
                .err()
                .map(|failure| failure.check),
            Some(ReceiptCheck::Module)
        );
    }
    let mut effect = layerx_wire::encode::Encoder::new(64);
    must(effect.u16(9));
    must(effect.u16(0));
    must(effect.u16(1));
    must(effect.u8(1));
    must(effect.u8(0));
    must(effect.bytes(&[0; 32], 32));
    must(effect.bytes(&[], 256));
    let mut bytes = terminal.receipt.clone();
    bytes[162..166].copy_from_slice(&1_u32.to_be_bytes());
    bytes.splice(166..166, effect.finish());
    terminal.resign(&mut bytes);
    assert_eq!(
        verify_program_preexecution_rejection(&bytes, &batch)
            .err()
            .map(|failure| failure.check),
        Some(ReceiptCheck::ReceiptShape)
    );
}

#[test]
fn complete_native_maintenance_authority_and_tamper_refusals_remain_required() {
    let captures = captures();
    for capture in &captures {
        let mut omitted = capture.receipts.clone();
        omitted.pop();
        assert!(verify_program_preexecution_rejection_maintained_chain(
            &capture.receipt,
            &capture.batch,
            &capture.evidence(),
            &omitted,
        )
        .is_err());
        let mut changed = capture.receipts.clone();
        changed[0][10] ^= 1;
        assert!(authorized_maintained_activity_batch_chain(
            &capture.receipt,
            &capture.batch,
            &capture.evidence(),
            &changed,
        )
        .is_err());
        if changed.len() > 1 {
            let mut reordered = capture.receipts.clone();
            reordered.reverse();
            assert!(authorized_maintained_activity_batch_chain(
                &capture.receipt,
                &capture.batch,
                &capture.evidence(),
                &reordered,
            )
            .is_err());
        }
    }
    let terminal = captures
        .iter()
        .find(|capture| capture.name == "terminal-0")
        .expect("terminal");
    let activity = terminal.activity_batch();
    let mut forged = terminal.receipt.clone();
    let signature = forged.len() - 1;
    forged[signature] ^= 1;
    assert_eq!(
        verify_program_preexecution_rejection(&forged, &activity)
            .err()
            .map(|failure| failure.check),
        Some(ReceiptCheck::SequencerSignature)
    );
    for count in [0_usize, 4, 158, terminal.receipt.len() - 1] {
        assert!(
            verify_program_preexecution_rejection(&terminal.receipt[..count], &activity).is_err()
        );
    }
    let mut trailing = terminal.receipt.clone();
    trailing.push(0);
    assert!(verify_program_preexecution_rejection(&trailing, &activity).is_err());
    let mut unsupported = terminal.receipt.clone();
    unsupported[1] = 2;
    unsupported[5] = 2;
    terminal.resign(&mut unsupported);
    assert_eq!(
        verify_program_preexecution_rejection(&unsupported, &activity)
            .err()
            .map(|failure| failure.check),
        Some(ReceiptCheck::ProtocolVersion)
    );
    let mut unsigned = must(encode_unsigned(&must(decode(&terminal.receipt))));
    assert_eq!(unsigned.last(), Some(&0));
    assert_eq!(
        verify_program_preexecution_rejection(&unsigned, &activity)
            .err()
            .map(|failure| failure.check),
        Some(ReceiptCheck::MissingSignature)
    );
    unsigned[10] ^= 1;
    assert!(verify_program_preexecution_rejection(&unsigned, &activity).is_err());
    for replacement in [
        AuthorizedBatch::new(
            [1; 32],
            [0; 32],
            activity.previous_state_root(),
            activity.resulting_state_root(),
            activity.sequencer_public_key(),
        ),
        AuthorizedBatch::new(
            activity.batch_id(),
            [0; 32],
            [1; 32],
            activity.resulting_state_root(),
            activity.sequencer_public_key(),
        ),
        AuthorizedBatch::new(
            activity.batch_id(),
            [0; 32],
            activity.previous_state_root(),
            [1; 32],
            activity.sequencer_public_key(),
        ),
        AuthorizedBatch::new(
            activity.batch_id(),
            [1; 32],
            activity.previous_state_root(),
            activity.resulting_state_root(),
            activity.sequencer_public_key(),
        ),
        AuthorizedBatch::new(
            activity.batch_id(),
            [0; 32],
            activity.previous_state_root(),
            activity.resulting_state_root(),
            [1; 32],
        ),
    ] {
        assert!(verify_program_preexecution_rejection(&terminal.receipt, &replacement).is_err());
    }
    let mut evidence = terminal.evidence();
    let mut bad_signature = terminal.signature;
    bad_signature[0] ^= 1;
    evidence.header_signature = &bad_signature;
    assert!(verify_program_preexecution_rejection_maintained_chain(
        &terminal.receipt,
        &terminal.batch,
        &evidence,
        &terminal.receipts,
    )
    .is_err());
    let wrong_authority =
        SequencerAuthorization::new([1; 32], terminal.authorization.public_key(), 1, 100);
    evidence = terminal.evidence();
    evidence.authorization = &wrong_authority;
    assert!(verify_program_preexecution_rejection_maintained_chain(
        &terminal.receipt,
        &terminal.batch,
        &evidence,
        &terminal.receipts,
    )
    .is_err());
    let wrong_range = SequencerAuthorization::new(
        terminal.authorization.sequencer_id(),
        terminal.authorization.public_key(),
        99,
        100,
    );
    evidence = terminal.evidence();
    evidence.authorization = &wrong_range;
    assert!(verify_program_preexecution_rejection_maintained_chain(
        &terminal.receipt,
        &terminal.batch,
        &evidence,
        &terminal.receipts,
    )
    .is_err());
    let wrong_batch = AuthorizedBatch::new(
        terminal.batch.batch_id(),
        [0; 32],
        [1; 32],
        terminal.batch.resulting_state_root(),
        terminal.batch.sequencer_public_key(),
    );
    assert!(verify_program_preexecution_rejection_maintained_chain(
        &terminal.receipt,
        &wrong_batch,
        &terminal.evidence(),
        &terminal.receipts,
    )
    .is_err());
    let mut maintenance = terminal.maintenance.clone();
    maintenance[10] ^= 1;
    evidence = terminal.evidence();
    evidence.maintenance = &maintenance;
    assert!(verify_program_preexecution_rejection_maintained_chain(
        &terminal.receipt,
        &terminal.batch,
        &evidence,
        &terminal.receipts,
    )
    .is_err());
    evidence = terminal.evidence();
    evidence.activity_proof = &terminal.maintenance_proof;
    assert!(verify_program_preexecution_rejection_maintained_chain(
        &terminal.receipt,
        &terminal.batch,
        &evidence,
        &terminal.receipts,
    )
    .is_err());
}
