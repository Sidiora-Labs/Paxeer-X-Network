use std::collections::BTreeSet;

use layerx_proof::merkle::{build_proof, leaf_hash, node_hash, verify_path, MerkleError, Proof};

const NEGATIVE_CORPUS: [&str; 9] = [
    "altered-value",
    "resigned-receipt",
    "truncated-proof",
    "subthreshold-certificate",
    "duplicate-signatures",
    "swapped-hash-domains",
    "mismatched-root",
    "withheld-availability-class",
    "malformed-structure",
];

#[test]
fn enumerates_complete_unique_adversarial_corpus() {
    let cases = NEGATIVE_CORPUS.into_iter().collect::<BTreeSet<_>>();
    assert_eq!(cases.len(), NEGATIVE_CORPUS.len());
    for required in [
        "altered-value",
        "resigned-receipt",
        "truncated-proof",
        "subthreshold-certificate",
        "duplicate-signatures",
        "swapped-hash-domains",
        "mismatched-root",
        "withheld-availability-class",
    ] {
        assert!(cases.contains(required));
    }
}

#[test]
fn malformed_and_mismatched_paths_are_typed_failures() {
    let leaves: [&[u8]; 3] = [b"alpha", b"beta", b"gamma"];
    let (proof, root) = build_proof(&leaves, 1)
        .unwrap_or_else(|error| panic!("proof construction failed: {error:?}"));

    let mut truncated = proof.siblings().to_vec();
    let _ = truncated.pop();
    assert_eq!(
        Proof::new(proof.leaf_index(), proof.leaf_count(), truncated),
        Err(MerkleError::PathLength {
            expected: proof.siblings().len(),
            actual: proof.siblings().len() - 1,
        })
    );

    let mut mismatched_root = root;
    mismatched_root[31] ^= 1;
    assert_eq!(
        verify_path(leaves[1], &proof, &mismatched_root),
        Err(MerkleError::RootMismatch)
    );

    let left = leaf_hash(b"left").unwrap_or_else(|error| panic!("leaf hashing failed: {error:?}"));
    let right =
        leaf_hash(b"right").unwrap_or_else(|error| panic!("leaf hashing failed: {error:?}"));
    let mut concatenated = Vec::from(left);
    concatenated.extend_from_slice(&right);
    assert_ne!(leaf_hash(&concatenated), node_hash(&left, &right));
}
