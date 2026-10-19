use layerx_proof::merkle::{
    build_proof, decode_proof, encode_proof, leaf_hash, node_hash, root, verify_path, MerkleError,
    Proof,
};

fn decode_hex(value: &str) -> [u8; 32] {
    assert_eq!(value.len(), 64);
    let mut output = [0_u8; 32];
    for (index, byte) in output.iter_mut().enumerate() {
        *byte = u8::from_str_radix(&value[index * 2..index * 2 + 2], 16)
            .unwrap_or_else(|error| panic!("invalid vector hex: {error}"));
    }
    output
}

#[test]
fn matches_core_three_leaf_vector_and_domain_tags() {
    let leaf_a = decode_hex("0000b48e7cdda31f4e79635f7bdd82f26eceec13884cfb3eb033c18151260e31");
    let leaf_b = decode_hex("8a11c3e244fda43acf6c0e6268c231dd60c17ff2380c09b981b9368ab31dfe98");
    let leaf_c = decode_hex("dc9c170fda471705a1f17e150aaedc18926ce478bc825f668701200af0eec0df");
    let node_ab = decode_hex("beb38bfc9e264ed8181638b4865e76e76e9cc6b4536a707a84c5b0b17e6da866");
    let node_cc = decode_hex("3f8d2dfa9b022d0a7faedb22385dbb5e25fa3a23abef487f723240249c0f2c89");
    let expected = decode_hex("e54a4f6431780baf4222b621c99f92e08b36ccf6ca93438b302ee80cc06cac97");

    assert_eq!(leaf_hash(b"a"), Ok(leaf_a));
    assert_eq!(leaf_hash(b"b"), Ok(leaf_b));
    assert_eq!(leaf_hash(b"c"), Ok(leaf_c));
    assert_eq!(node_hash(&leaf_a, &leaf_b), Ok(node_ab));
    assert_eq!(node_hash(&leaf_c, &leaf_c), Ok(node_cc));
    assert_eq!(root(&[b"a", b"b", b"c"]), Ok(expected));

    let mut concatenated = Vec::from(leaf_a);
    concatenated.extend_from_slice(&leaf_b);
    assert_ne!(leaf_hash(&concatenated), Ok(node_ab));
}

#[test]
fn rejects_invalid_geometry_and_mutated_paths() {
    let leaves: Vec<Vec<u8>> = (0_u16..64).map(u16::to_be_bytes).map(Vec::from).collect();
    for count in 1..=leaves.len() {
        let refs: Vec<&[u8]> = leaves[..count].iter().map(Vec::as_slice).collect();
        for index in 0..count {
            let Ok((proof, expected)) = build_proof(&refs, index) else {
                panic!("valid generated proof failed");
            };
            assert_eq!(verify_path(refs[index], &proof, &expected), Ok(()));

            let mut changed_leaf = refs[index].to_vec();
            changed_leaf.push(1);
            assert!(verify_path(&changed_leaf, &proof, &expected).is_err());

            let mut changed_root = expected;
            changed_root[0] ^= 1;
            assert_eq!(
                verify_path(refs[index], &proof, &changed_root),
                Err(MerkleError::RootMismatch)
            );

            if !proof.siblings().is_empty() {
                let mut siblings = proof.siblings().to_vec();
                siblings[0][0] ^= 1;
                let changed = Proof::new(proof.leaf_index(), proof.leaf_count(), siblings)
                    .unwrap_or_else(|error| panic!("valid geometry rejected: {error:?}"));
                assert!(verify_path(refs[index], &changed, &expected).is_err());

                let mut truncated = proof.siblings().to_vec();
                let _ = truncated.pop();
                assert_eq!(
                    Proof::new(proof.leaf_index(), proof.leaf_count(), truncated),
                    Err(MerkleError::PathLength {
                        expected: proof.siblings().len(),
                        actual: proof.siblings().len() - 1,
                    })
                );
            }
        }
    }

    assert_eq!(Proof::new(0, 0, Vec::new()), Err(MerkleError::EmptyTree));
    assert_eq!(
        Proof::new(3, 3, vec![[0; 32]; 2]),
        Err(MerkleError::LeafIndex { index: 3, count: 3 })
    );
}

#[test]
fn rejects_false_odd_promotion_sibling() {
    let leaves: [&[u8]; 3] = [b"a", b"b", b"c"];
    let Ok((proof, expected)) = build_proof(&leaves, 2) else {
        panic!("proof construction failed");
    };
    let mut siblings = proof.siblings().to_vec();
    siblings[0][0] ^= 1;
    let changed = Proof::new(2, 3, siblings)
        .unwrap_or_else(|error| panic!("valid geometry rejected: {error:?}"));
    assert_eq!(
        verify_path(b"c", &changed, &expected),
        Err(MerkleError::PromotionSibling { level: 0 })
    );
}

#[test]
fn public_proof_codec_is_canonical_and_bounded() {
    let leaves: [&[u8]; 3] = [b"a", b"b", b"c"];
    let (proof, _) = build_proof(&leaves, 1)
        .unwrap_or_else(|error| panic!("proof construction failed: {error:?}"));
    let encoded = encode_proof(&proof);
    assert_eq!(decode_proof(&encoded), Ok(proof));

    let mut unknown_version = encoded.clone();
    unknown_version[0] = 2;
    assert_eq!(decode_proof(&unknown_version), Err(MerkleError::Encoding));
    assert_eq!(
        decode_proof(&encoded[..encoded.len() - 1]),
        Err(MerkleError::Encoding)
    );
    let mut trailing = encoded;
    trailing.push(0);
    assert_eq!(decode_proof(&trailing), Err(MerkleError::Encoding));
}
