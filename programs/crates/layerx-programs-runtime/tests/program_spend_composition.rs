use layerx_programs_runtime::{
    derive_program_account, AbiError, Capability, CapabilitySet, ProgramId,
};

fn spend(owner: ProgramId, asset: [u8; 32], to: [u8; 32], maximum: u128) -> Capability {
    let seed = b"composition/retained";
    Capability::ProgramSpend {
        owner_program: owner,
        seed: seed.to_vec(),
        source_account: derive_program_account(owner, seed).expect("derived account"),
        asset,
        to,
        maximum_amount: maximum,
    }
}

#[test]
fn canonical_program_grants_narrow_at_every_depth_and_repeated_visit() {
    let owner = ProgramId::new([1; 32]).expect("owner");
    let mut inherited = CapabilitySet::new([spend(owner, [2; 32], [3; 32], 100)])
        .expect("root grant");
    for maximum in (1..100).rev() {
        let bytes = inherited.canonical_encoding();
        let decoded = CapabilitySet::new(CapabilitySet::decode_v2_canonical(&bytes)
            .expect("canonical grant")).expect("decoded grant");
        assert_eq!(decoded, inherited);
        assert_eq!(decoded.narrow([spend(owner, [2; 32], [3; 32], maximum + 2)]),
            Err(AbiError::CapabilityEscalation));
        assert_eq!(decoded.narrow([spend(owner, [4; 32], [3; 32], maximum)]),
            Err(AbiError::CapabilityEscalation));
        assert_eq!(decoded.narrow([spend(owner, [2; 32], [4; 32], maximum)]),
            Err(AbiError::CapabilityEscalation));
        inherited = decoded.narrow([spend(owner, [2; 32], [3; 32], maximum)])
            .expect("strictly downward grant");
    }
}

#[test]
fn fanout_does_not_merge_distinct_principal_or_program_authority() {
    let owner = ProgramId::new([5; 32]).expect("owner");
    let child = ProgramId::new([6; 32]).expect("child");
    let root = CapabilitySet::new([
        Capability::Transfer402 { asset: [7; 32], to: [8; 32], maximum_amount: 200 },
        spend(owner, [7; 32], [8; 32], 100),
    ]).expect("distinct grants");
    for maximum in [20, 30, 40] {
        let branch = root.narrow([spend(owner, [7; 32], [8; 32], maximum)])
            .expect("branch");
        assert_eq!(branch.narrow([spend(owner, [7; 32], [8; 32], maximum + 1)]),
            Err(AbiError::CapabilityEscalation));
        assert_eq!(branch.narrow([spend(child, [7; 32], [8; 32], maximum)]),
            Err(AbiError::CapabilityEscalation));
        assert_eq!(branch.narrow([Capability::Transfer402 {
            asset: [7; 32], to: [8; 32], maximum_amount: maximum,
        }]), Err(AbiError::CapabilityDenied));
    }
    assert!(root.narrow([spend(owner, [7; 32], [8; 32], 100)]).is_ok());
}
