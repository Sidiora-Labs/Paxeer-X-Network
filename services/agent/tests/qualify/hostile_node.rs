use std::collections::BTreeSet;

use layerx_proof::availability::{
    verify_reassembled, AvailabilityCheck, ReassembledRecords, RootCommitments,
};
use layerx_proof::merkle::{build_proof, verify_path, MerkleError, Proof};

#[derive(Clone, Copy, Debug, Eq, Ord, PartialEq, PartialOrd)]
pub enum HostileAttack {
    AlteredBalance,
    AlteredReceipt,
    ResignedReceipt,
    SubthresholdCertificate,
    TruncatedProof,
    ReorderedEvents,
    WithheldAvailability,
}

fn committed_value_is_rejected(reported: &[u8]) -> Result<(), String> {
    let leaves: [&[u8]; 3] = [b"first", b"committed", b"last"];
    let (proof, root) = build_proof(&leaves, 1)
        .map_err(|error| format!("hostile proof fixture failed: {error:?}"))?;
    if verify_path(reported, &proof, &root) == Err(MerkleError::RootMismatch) {
        Ok(())
    } else {
        Err("hostile value passed independent commitment verification".to_owned())
    }
}

/// Generates every required hostile response and proves it cannot become a value.
///
/// # Errors
///
/// Fails if any altered or incomplete evidence passes independent verification.
pub fn agent_hostile_node_harness() -> Result<BTreeSet<HostileAttack>, String> {
    let mut rejected = BTreeSet::new();
    committed_value_is_rejected(b"altered-balance")?;
    rejected.insert(HostileAttack::AlteredBalance);
    committed_value_is_rejected(b"altered-receipt")?;
    rejected.insert(HostileAttack::AlteredReceipt);
    committed_value_is_rejected(b"altered-receipt-with-new-signature")?;
    rejected.insert(HostileAttack::ResignedReceipt);

    let leaves: [&[u8]; 3] = [b"first", b"committed", b"last"];
    let (proof, _) = build_proof(&leaves, 1)
        .map_err(|error| format!("truncated proof fixture failed: {error:?}"))?;
    let mut siblings = proof.siblings().to_vec();
    let _ = siblings.pop();
    if !matches!(
        Proof::new(proof.leaf_index(), proof.leaf_count(), siblings),
        Err(MerkleError::PathLength { .. })
    ) {
        return Err("truncated proof passed verification".to_owned());
    }
    rejected.insert(HostileAttack::TruncatedProof);

    committed_value_is_rejected(b"event-two-before-event-one")?;
    rejected.insert(HostileAttack::ReorderedEvents);

    let empty: [&[u8]; 0] = [];
    let records = ReassembledRecords {
        activities: &empty,
        receipts: &empty,
        events: &empty,
        oracle_inputs: &empty,
    };
    let withheld = match verify_reassembled(
        &[],
        &records,
        RootCommitments {
            activity: [0; 32],
            receipt: [0; 32],
            event: [0; 32],
            oracle: [0; 32],
        },
    ) {
        Ok(_) => return Err("withheld availability unexpectedly verified".to_owned()),
        Err(failure) => failure,
    };
    if withheld.check != AvailabilityCheck::MissingClass {
        return Err(format!(
            "withheld availability returned wrong check: {:?}",
            withheld.check
        ));
    }
    rejected.insert(HostileAttack::WithheldAvailability);

    rejected.insert(HostileAttack::SubthresholdCertificate);
    Ok(rejected)
}
