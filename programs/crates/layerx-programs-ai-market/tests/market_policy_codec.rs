use layerx_program_sdk::payments::PreparedProgramAccount;
use layerx_programs_ai_market::{
    codec::{derive_market, domain_hash},
    errors::*,
    policy::*,
    registry::*,
    types::*,
};
use sha2::{Digest, Sha256};

enum Failure {
    Application(ApplicationError),
    Program(layerx_program_sdk::ProgramError),
}
impl core::fmt::Debug for Failure {
    fn fmt(&self, f: &mut core::fmt::Formatter<'_>) -> core::fmt::Result {
        match self {
            Self::Application(error) => write!(f, "application refusal {error:?}"),
            Self::Program(error) => write!(f, "program refusal {error:?}"),
        }
    }
}
impl From<ApplicationError> for Failure {
    fn from(error: ApplicationError) -> Self {
        Self::Application(error)
    }
}
impl From<layerx_program_sdk::ProgramError> for Failure {
    fn from(error: layerx_program_sdk::ProgramError) -> Self {
        Self::Program(error)
    }
}
type Checked<T = ()> = Result<T, Failure>;

fn commitments() -> Checked<PolicyCommitments> {
    Ok(PolicyCommitments {
        model_artifact: Digest32::new([1; 32])?,
        dataset_artifact: [2; 32],
        benchmark_suite: Digest32::new([3; 32])?,
        rubric: RubricDigest::new([4; 32])?,
        task_schema: Digest32::new([5; 32])?,
        result_schema: Digest32::new([6; 32])?,
        service_terms: Digest32::new([7; 32])?,
    })
}
fn policy() -> Checked<TaskPolicyV1> {
    Ok(TaskPolicyV1::bounded_default(1, 1, commitments()?, 100, 1)?)
}
fn policy_bytes() -> [u8; 307] {
    let mut expected = [0; 307];
    expected[7] = 1;
    expected[8] = 1;
    expected[9] = 1;
    expected[10..42].copy_from_slice(&[1; 32]);
    expected[42..74].copy_from_slice(&[2; 32]);
    expected[74..106].copy_from_slice(&[3; 32]);
    expected[106..138].copy_from_slice(&[4; 32]);
    expected[138..170].copy_from_slice(&[5; 32]);
    expected[170..202].copy_from_slice(&[6; 32]);
    expected[202..234].copy_from_slice(&[7; 32]);
    expected[234..248].copy_from_slice(&[32, 8, 0, 64, 0, 16, 0, 0, 0, 16, 0, 0, 0, 32]);
    expected[252..256].copy_from_slice(&[0, 15, 66, 64]);
    expected[271] = 100;
    expected[287] = 1;
    expected[288..291].copy_from_slice(&[1, 3, 1]);
    expected
}
fn pending(version: u64) -> Checked<PendingPolicy> {
    let mut policy = policy()?;
    policy.config_version = version;
    Ok(PendingPolicy {
        digest: policy.digest()?,
        policy,
        effective_epoch: 2,
        proposer: PrincipalId::new([8; 32])?,
    })
}
fn history(version: u64, disposition: u8) -> Checked<PolicyHistoryHeader> {
    let p = pending(version)?;
    Ok(PolicyHistoryHeader {
        config_version: version,
        digest: p.digest,
        effective_epoch: 2,
        disposition,
    })
}
fn header(present: bool) -> Checked<MarketHeader> {
    let chain = ChainDomain::new([10; 32])?;
    let program = ProgramId::new([11; 32])?;
    let asset = AssetId::new([13; 32])?;
    Ok(MarketHeader {
        format_version: 1,
        market_id: derive_market(chain, program)?,
        deployment_chain_domain: chain,
        program_id: program,
        owner_principal: PrincipalId::new([12; 32])?,
        funding_asset: asset,
        rewards_account: derive_rewards_account(program, asset)?,
        refund_recipient_account: AccountId::new([14; 32])?,
        treasury_principal: if present {
            Presence::Present(PrincipalId::new([15; 32])?)
        } else {
            Presence::Absent
        },
        origin_height: 1000,
        lifecycle: 1,
        state_revision: 1,
        highest_config_version: 1,
        active_config_version: 1,
        activation_epoch: 0,
        activation_scheduled: false,
        closure_requested_at: 0,
        close_phase: 0,
        close_cursor: 0,
        suspension_reason_digest: [0; 32],
        metadata_digest: MetadataDigest::new([16; 32])?,
        closing_request_digest: [0; 32],
        reserved: [0; 8],
    })
}
fn grant() -> Checked<OperatorGrant> {
    Ok(OperatorGrant {
        principal: PrincipalId::new([17; 32])?,
        permissions: 3,
        sequence: 4,
        revoked: false,
        reserved: [0; 8],
    })
}
fn strict_input<T: core::fmt::Debug>(bytes: &[u8], decode: impl Fn(&[u8]) -> CodecResult<T>) {
    for end in 0..bytes.len() {
        let prefix = &bytes[..end];
        let original = prefix.to_vec();
        assert!(decode(prefix).is_err(), "accepted prefix {end}");
        assert_eq!(prefix, original);
    }
    for extra in [0, 1, 255] {
        let mut trailing = bytes.to_vec();
        trailing.push(extra);
        let original = trailing.clone();
        assert!(decode(&trailing).is_err());
        assert_eq!(trailing, original);
    }
}
fn short_outputs(size: usize, encode: impl Fn(&mut [u8]) -> CodecResult<usize>) -> Checked {
    for length in 0..size {
        let mut output = vec![0xa5; length];
        let before = output.clone();
        assert_eq!(encode(&mut output), Err(CAPACITY));
        assert_eq!(output, before);
    }
    let mut output = vec![0xa5; size + 1];
    assert_eq!(encode(&mut output)?, size);
    assert_eq!(output[size], 0xa5);
    Ok(())
}

#[test]
fn policy_exact_bytes_offsets_hash_and_modes() -> Checked {
    let p = policy()?;
    let expected = policy_bytes();
    let mut encoded = [0xa5; 307];
    assert_eq!(p.encode(&mut encoded), Ok(307));
    assert_eq!(encoded, expected);
    assert_eq!(TaskPolicyV1::decode(&expected), Ok(p));
    assert_eq!(encoded[9], OBJECTIVE);
    let mut preimage = b"PAXAI/policy/v1\0".to_vec();
    preimage.extend_from_slice(&expected);
    let reference: [u8; 32] = Sha256::digest(&preimage).into();
    assert_eq!(p.digest()?.bytes(), reference);
    assert_eq!(
        domain_hash("PAXAI/policy/v1", &expected)?.bytes(),
        reference
    );
    let mut subjective = p;
    subjective.assessment_mode = SUBJECTIVE;
    let mut bytes = [0; 307];
    subjective.encode(&mut bytes)?;
    for index in 0..307 {
        assert_eq!(bytes[index], if index == 9 { 2 } else { expected[index] });
    }
    assert_ne!(subjective.digest()?.bytes(), reference);
    strict_input(&expected, TaskPolicyV1::decode);
    short_outputs(307, |out| p.encode(out))?;
    Ok(())
}

#[test]
fn every_policy_numeric_bound_and_refusal_preserves_output() -> Checked {
    let p = policy()?;
    macro_rules! invalid {
        ($field:ident, $values:expr) => {
            for value in $values {
                let mut bad = p;
                bad.$field = value;
                let original = bad;
                let mut output = [0xa5; 307];
                assert_eq!(
                    bad.validate(),
                    Err(F01_INVALID_POLICY),
                    "{}={value}",
                    stringify!($field)
                );
                assert_eq!(bad.encode(&mut output), Err(F01_INVALID_POLICY));
                assert_eq!(bad.digest(), Err(F01_INVALID_POLICY));
                assert_eq!(bad, original);
                assert_eq!(output, [0xa5; 307]);
            }
        };
    }
    invalid!(config_version, [0]);
    invalid!(task_kind, [0, 4, u8::MAX]);
    invalid!(assessment_mode, [0, 3, u8::MAX]);
    invalid!(max_workers, [0, 33, u8::MAX]);
    invalid!(max_evaluators, [0, 1, 2, 9, u8::MAX]);
    invalid!(max_tasks_per_epoch, [0, 65, u16::MAX]);
    invalid!(max_input_bytes, [0, 16_777_217, u32::MAX]);
    invalid!(max_output_bytes, [0, 16_777_217, u32::MAX]);
    invalid!(task_timeout_heights, [0, 65, u16::MAX]);
    invalid!(score_min, [1, 1_000_000, u32::MAX]);
    invalid!(score_max, [0, 999_999, 1_000_001, u32::MAX]);
    invalid!(epoch_budget_cap, [0]);
    invalid!(minimum_epoch_funding, [0, 101, u128::MAX]);
    invalid!(minimum_worker_count, [0, 33, u8::MAX]);
    invalid!(minimum_evaluator_count, [0, 1, 2, 9, u8::MAX]);
    invalid!(owner_affiliation_policy, [0, 2, u8::MAX]);
    let mut limits = p;
    limits.config_version = u64::MAX;
    limits.max_input_bytes = 16_777_216;
    limits.max_output_bytes = 16_777_216;
    limits.task_timeout_heights = 64;
    limits.minimum_worker_count = 32;
    limits.minimum_evaluator_count = 8;
    limits.epoch_budget_cap = u128::MAX;
    limits.minimum_epoch_funding = u128::MAX;
    assert_eq!(limits.validate(), Ok(()));
    let mut bytes = [0; 307];
    limits.encode(&mut bytes)?;
    assert_eq!(TaskPolicyV1::decode(&bytes), Ok(limits));
    limits.max_workers = 1;
    limits.max_evaluators = 3;
    limits.max_tasks_per_epoch = 1;
    limits.max_input_bytes = 1;
    limits.max_output_bytes = 1;
    limits.task_timeout_heights = 1;
    limits.minimum_worker_count = 1;
    limits.minimum_evaluator_count = 3;
    limits.epoch_budget_cap = 1;
    limits.minimum_epoch_funding = 1;
    assert_eq!(limits.validate(), Ok(()));
    limits.minimum_worker_count = 2;
    assert_eq!(limits.validate(), Err(F01_INVALID_POLICY));
    limits.minimum_worker_count = 1;
    limits.minimum_evaluator_count = 4;
    assert_eq!(limits.validate(), Err(F01_INVALID_POLICY));
    assert_eq!(next_config_version(1), Ok(2));
    assert_eq!(next_config_version(u64::MAX - 1), Ok(u64::MAX));
    assert_eq!(next_config_version(u64::MAX), Err(ARITHMETIC));
    assert_eq!(next_config_version(0), Err(NON_CANONICAL));
    Ok(())
}

#[test]
fn all_policy_wire_negatives_required_commitments_and_reserved() -> Checked {
    let expected = policy_bytes();
    for offset in [10, 74, 106, 138, 170, 202] {
        let mut bytes = expected;
        bytes[offset..offset + 32].fill(0);
        assert_eq!(TaskPolicyV1::decode(&bytes), Err(F01_INVALID_POLICY));
    }
    for index in 291..307 {
        let mut bytes = expected;
        bytes[index] = 1;
        assert_eq!(TaskPolicyV1::decode(&bytes), Err(F01_INVALID_POLICY));
    }
    for (offset, replacements) in [
        (0, vec![vec![0; 8]]),
        (8, vec![vec![0], vec![4], vec![255]]),
        (9, vec![vec![0], vec![3], vec![255]]),
        (234, vec![vec![0], vec![33], vec![255]]),
        (235, vec![vec![0], vec![1], vec![2], vec![9], vec![255]]),
        (236, vec![vec![0, 0], vec![0, 65], vec![255; 2]]),
        (238, vec![vec![0; 4], vec![1, 0, 0, 1], vec![255; 4]]),
        (242, vec![vec![0; 4], vec![1, 0, 0, 1], vec![255; 4]]),
        (246, vec![vec![0, 0], vec![0, 65], vec![255; 2]]),
        (248, vec![vec![0, 0, 0, 1], vec![255; 4]]),
        (
            252,
            vec![
                vec![0; 4],
                vec![0, 15, 66, 63],
                vec![0, 15, 66, 65],
                vec![255; 4],
            ],
        ),
        (256, vec![vec![0; 16]]),
        (272, vec![vec![0; 16], vec![255; 16]]),
        (288, vec![vec![0], vec![33], vec![255]]),
        (289, vec![vec![0], vec![2], vec![9], vec![255]]),
        (290, vec![vec![0], vec![2], vec![255]]),
    ] {
        for replacement in replacements {
            let mut bytes = expected;
            bytes[offset..offset + replacement.len()].copy_from_slice(&replacement);
            assert_eq!(
                TaskPolicyV1::decode(&bytes),
                Err(F01_INVALID_POLICY),
                "offset {offset}"
            );
        }
    }
    let mut dataset_absent = expected;
    dataset_absent[42..74].fill(0);
    assert!(TaskPolicyV1::decode(&dataset_absent).is_ok());
    for kind in [2, 3] {
        dataset_absent[8] = kind;
        assert_eq!(
            TaskPolicyV1::decode(&dataset_absent),
            Err(F01_INVALID_POLICY)
        );
        let mut bytes = expected;
        bytes[8] = kind;
        assert!(TaskPolicyV1::decode(&bytes).is_ok());
    }
    let mut full_comparison = expected;
    full_comparison[137] ^= 1;
    assert_ne!(
        TaskPolicyV1::decode(&full_comparison)?.digest()?,
        policy()?.digest()?
    );
    assert_eq!(
        TaskPolicyV1::bounded_default(0, 1, commitments()?, 100, 1),
        Err(F01_INVALID_POLICY)
    );
    assert_eq!(
        TaskPolicyV1::bounded_default(1, 1, commitments()?, 0, 1),
        Err(F01_INVALID_POLICY)
    );
    assert_eq!(
        TaskPolicyV1::bounded_default(1, 1, commitments()?, 1, 2),
        Err(F01_INVALID_POLICY)
    );
    Ok(())
}

#[test]
fn pending_and_history_exact_layout_roundtrip_and_all_short_buffers() -> Checked {
    let p = pending(2)?;
    let mut expected = [0; 379];
    let mut expected_policy = policy_bytes();
    expected_policy[7] = 2;
    expected[..307].copy_from_slice(&expected_policy);
    expected[307..339].copy_from_slice(p.digest.as_bytes());
    expected[346] = 2;
    expected[347..379].copy_from_slice(&[8; 32]);
    let mut bytes = [0; 379];
    assert_eq!(p.encode(&mut bytes), Ok(379));
    assert_eq!(bytes, expected);
    assert_eq!(PendingPolicy::decode(&expected), Ok(p));
    strict_input(&bytes, PendingPolicy::decode);
    short_outputs(379, |out| p.encode(out))?;
    for index in 307..339 {
        let mut bad = bytes;
        bad[index] ^= 1;
        assert_eq!(PendingPolicy::decode(&bad), Err(F01_POLICY_MISMATCH));
    }
    let mut bad = bytes;
    bad[307..339].fill(0);
    assert_eq!(PendingPolicy::decode(&bad), Err(NON_CANONICAL));
    bad = bytes;
    bad[347..379].fill(0);
    assert_eq!(PendingPolicy::decode(&bad), Err(NON_CANONICAL));
    let mut mismatched = p;
    mismatched.digest = policy()?.digest()?;
    let mut untouched = [0xa5; 379];
    assert_eq!(mismatched.encode(&mut untouched), Err(F01_POLICY_MISMATCH));
    assert_eq!(untouched, [0xa5; 379]);
    for disposition in [1, 2] {
        let h = history(2, disposition)?;
        let mut expected = [0; 49];
        expected[7] = 2;
        expected[8..40].copy_from_slice(p.digest.as_bytes());
        expected[47] = 2;
        expected[48] = disposition;
        let mut bytes = [0; 49];
        assert_eq!(h.encode(&mut bytes), Ok(49));
        assert_eq!(bytes, expected);
        assert_eq!(PolicyHistoryHeader::decode(&bytes), Ok(h));
        strict_input(&bytes, PolicyHistoryHeader::decode);
        short_outputs(49, |out| h.encode(out))?;
        for disposition in [0, 3, 255] {
            let mut bad = bytes;
            bad[48] = disposition;
            assert_eq!(PolicyHistoryHeader::decode(&bad), Err(NON_CANONICAL));
        }
        let mut bad = bytes;
        bad[..8].fill(0);
        assert_eq!(PolicyHistoryHeader::decode(&bad), Err(NON_CANONICAL));
        bad = bytes;
        bad[8..40].fill(0);
        assert_eq!(PolicyHistoryHeader::decode(&bad), Err(NON_CANONICAL));
    }
    Ok(())
}

#[test]
fn history_hash_preimage_order_and_restored_conflicts() -> Checked {
    let initial = initial_policy_history_root()?;
    let expected: [u8; 32] = Sha256::digest(b"PAXAI/policy-history/v1\0").into();
    assert_eq!(initial.bytes(), expected);
    let h = history(2, 2)?;
    let mut preimage = b"PAXAI/policy-history/v1\0".to_vec();
    preimage.extend_from_slice(&expected);
    preimage.push(2);
    preimage.extend_from_slice(&[0, 0, 0, 0, 0, 0, 0, 2]);
    preimage.extend_from_slice(h.digest.as_bytes());
    preimage.extend_from_slice(&[0, 0, 0, 0, 0, 0, 0, 2]);
    let reference: [u8; 32] = Sha256::digest(&preimage).into();
    assert_eq!(fold_policy_history(initial, 1, &[h])?.bytes(), reference);
    let h3 = history(3, 2)?;
    assert_eq!(
        fold_policy_history(initial, 1, &[h, h3])?,
        fold_policy_history(fold_policy_history(initial, 1, &[h])?, 2, &[h3])?
    );
    for headers in [vec![h, h], vec![h3, h], vec![history(1, 1)?]] {
        assert_eq!(
            fold_policy_history(initial, 1, &headers),
            Err(F01_VERSION_MISMATCH)
        );
    }
    assert_eq!(fold_policy_history(initial, 0, &[]), Ok(initial));
    assert_eq!(
        fold_policy_history(initial, u64::MAX, &[h]),
        Err(F01_VERSION_MISMATCH)
    );
    let current = policy()?;
    assert_eq!(
        validate_policy_records(&current, &Presence::Absent, &[], 1),
        Ok(())
    );
    let p = pending(3)?;
    assert_eq!(
        validate_policy_records(&current, &Presence::Present(p), &[h], 3),
        Ok(())
    );
    assert_eq!(
        validate_policy_records(&current, &Presence::Absent, &[h], 2),
        Ok(())
    );
    for headers in [vec![h, h], vec![h3, h], vec![history(2, 1)?]] {
        assert_eq!(
            validate_policy_records(&current, &Presence::Absent, &headers, 3),
            Err(F01_VERSION_MISMATCH)
        );
    }
    assert_eq!(
        validate_policy_records(&current, &Presence::Present(pending(2)?), &[h3], 3),
        Err(F01_VERSION_MISMATCH)
    );
    assert_eq!(
        validate_policy_records(&current, &Presence::Present(pending(1)?), &[], 1),
        Err(F01_VERSION_MISMATCH)
    );
    assert_eq!(
        validate_policy_records(&current, &Presence::Present(pending(2)?), &[h], 2),
        Err(F01_VERSION_MISMATCH)
    );
    assert_eq!(
        validate_policy_records(&current, &Presence::Present(pending(3)?), &[], 4),
        Err(F01_VERSION_MISMATCH)
    );
    assert_eq!(
        validate_policy_records(&current, &Presence::Absent, &[], 2),
        Err(F01_VERSION_MISMATCH)
    );
    assert_eq!(
        validate_policy_records(&current, &Presence::Absent, &[], 0),
        Err(F01_VERSION_MISMATCH)
    );
    let five = [history(1, 1)?, h, h3, history(4, 2)?, history(5, 2)?];
    assert_eq!(
        validate_policy_records(&current, &Presence::Absent, &five, 5),
        Err(F01_CAPACITY_UNAVAILABLE)
    );
    let mut conflict = history(1, 1)?;
    let mut bytes = conflict.digest.bytes();
    bytes[31] ^= 1;
    conflict.digest = PolicyDigest::new(bytes)?;
    assert_eq!(
        validate_policy_records(&current, &Presence::Absent, &[conflict], 1),
        Err(F01_POLICY_MISMATCH)
    );
    assert_eq!(
        validate_policy_records(&current, &Presence::Absent, &[history(1, 2)?], 1),
        Err(F01_POLICY_MISMATCH)
    );
    Ok(())
}

#[test]
fn market_identity_and_real_sdk_rewards_derivation() -> Checked {
    let h = header(false)?;
    let mut preimage = b"PAXAI/market/v1\0".to_vec();
    preimage.extend_from_slice(&[10; 32]);
    preimage.extend_from_slice(&[11; 32]);
    let expected: [u8; 32] = Sha256::digest(&preimage).into();
    assert_eq!(h.market_id.bytes(), expected);
    let other_chain = ChainDomain::new([19; 32])?;
    let other_program = ProgramId::new([20; 32])?;
    assert_ne!(derive_market(other_chain, h.program_id)?, h.market_id);
    assert_ne!(
        derive_market(h.deployment_chain_domain, other_program)?,
        h.market_id
    );
    let sdk_program = layerx_program_sdk::ProgramId::new([11; 32])?;
    let sdk_asset = layerx_program_sdk::AssetId::new([13; 32])?;
    let sdk = PreparedProgramAccount::new(sdk_program, b"paxai/rewards/v1", sdk_asset)?;
    assert_eq!(sdk.program(), sdk_program);
    assert_eq!(sdk.asset(), sdk_asset);
    assert_eq!(sdk.seed().bytes(), b"paxai/rewards/v1");
    assert_eq!(
        derive_rewards_account(h.program_id, h.funding_asset)?.bytes(),
        sdk.account().bytes()
    );
    let mut altered = h;
    altered.highest_config_version = 2;
    altered.state_revision = 5;
    assert_eq!(altered.market_id, h.market_id);
    assert_eq!(altered.validate(), Ok(()));
    let mut account_bytes = h.rewards_account.bytes();
    account_bytes[31] ^= 1;
    altered.rewards_account = AccountId::new(account_bytes)?;
    assert_eq!(altered.validate(), Err(F01_ACCOUNT_BINDING_MISSING));
    let mut output = [0xa5; 416];
    assert_eq!(
        altered.encode(&mut output),
        Err(F01_ACCOUNT_BINDING_MISSING)
    );
    assert_eq!(output, [0xa5; 416]);
    altered = h;
    let mut market_bytes = h.market_id.bytes();
    market_bytes[31] ^= 1;
    altered.market_id = MarketId::new(market_bytes)?;
    assert_eq!(altered.validate(), Err(WRONG_MARKET));
    altered = h;
    altered.deployment_chain_domain = other_chain;
    assert_eq!(altered.validate(), Err(WRONG_MARKET));
    altered = h;
    altered.program_id = other_program;
    assert_eq!(altered.validate(), Err(WRONG_MARKET));
    Ok(())
}

#[test]
fn full_market_header_exact_bytes_treasury_offsets_and_roundtrip() -> Checked {
    for present in [false, true] {
        let h = header(present)?;
        let size = if present { 416 } else { 384 };
        let shift = if present { 32 } else { 0 };
        let mut expected = vec![0; size];
        expected[..2].copy_from_slice(&[0, 1]);
        expected[2..34].copy_from_slice(h.market_id.as_bytes());
        expected[34..66].copy_from_slice(&[10; 32]);
        expected[66..98].copy_from_slice(&[11; 32]);
        expected[98..130].copy_from_slice(&[12; 32]);
        expected[130..162].copy_from_slice(&[13; 32]);
        expected[162..194].copy_from_slice(h.rewards_account.as_bytes());
        expected[194..226].copy_from_slice(&[14; 32]);
        expected[226] = u8::from(present);
        if present {
            expected[227..259].copy_from_slice(&[15; 32]);
        }
        expected[227 + shift..235 + shift].copy_from_slice(&[0, 0, 0, 0, 0, 0, 3, 232]);
        expected[235 + shift] = 1;
        expected[243 + shift] = 1;
        expected[251 + shift] = 1;
        expected[259 + shift] = 1;
        expected[312 + shift..344 + shift].copy_from_slice(&[16; 32]);
        let mut bytes = vec![0xa5; size];
        assert_eq!(h.encode(&mut bytes), Ok(size));
        assert_eq!(bytes, expected);
        assert_eq!(h.encoded_len(), size);
        assert_eq!(MarketHeader::decode(&expected), Ok(h));
        strict_input(&bytes, MarketHeader::decode);
        short_outputs(size, |out| h.encode(out))?;
        for lifecycle in 1..=5 {
            let mut full = h;
            full.lifecycle = lifecycle;
            full.state_revision = u64::MAX;
            full.highest_config_version = u64::MAX;
            full.active_config_version = u64::MAX;
            full.origin_height = u64::MAX;
            full.activation_scheduled = true;
            full.activation_epoch = u64::MAX;
            full.closure_requested_at = u64::MAX;
            full.close_phase = 5;
            full.close_cursor = u16::MAX;
            full.suspension_reason_digest = [21; 32];
            full.closing_request_digest = [22; 32];
            full.encode(&mut bytes)?;
            assert_eq!(MarketHeader::decode(&bytes), Ok(full));
            assert_eq!(&bytes[260 + shift..268 + shift], &[255; 8]);
            assert_eq!(bytes[268 + shift], 1);
            assert_eq!(&bytes[269 + shift..277 + shift], &[255; 8]);
            assert_eq!(bytes[277 + shift], 5);
            assert_eq!(&bytes[278 + shift..280 + shift], &[255; 2]);
            assert_eq!(&bytes[280 + shift..312 + shift], &[21; 32]);
            assert_eq!(&bytes[344 + shift..376 + shift], &[22; 32]);
        }
    }
    Ok(())
}

#[test]
fn market_all_presence_bool_enum_reserved_zero_and_schedule_refusals() -> Checked {
    for present in [false, true] {
        let h = header(present)?;
        let shift = if present { 32 } else { 0 };
        let mut bytes = vec![0; h.encoded_len()];
        h.encode(&mut bytes)?;
        for invalid in 2..=255 {
            let mut bad = bytes.clone();
            bad[226] = invalid;
            assert_eq!(MarketHeader::decode(&bad), Err(NON_CANONICAL));
            bad = bytes.clone();
            bad[268 + shift] = invalid;
            assert_eq!(MarketHeader::decode(&bad), Err(NON_CANONICAL));
        }
        for value in 0..=255 {
            if (1..=5).contains(&value) {
                continue;
            }
            let mut bad = bytes.clone();
            bad[235 + shift] = value;
            assert_eq!(MarketHeader::decode(&bad), Err(F01_WRONG_LIFECYCLE));
        }
        for offset in [2, 34, 66, 98, 130, 162, 194, 312 + shift] {
            let mut bad = bytes.clone();
            bad[offset..offset + 32].fill(0);
            assert_eq!(MarketHeader::decode(&bad), Err(NON_CANONICAL));
        }
        for index in 376 + shift..384 + shift {
            let mut bad = bytes.clone();
            bad[index] = 1;
            assert_eq!(MarketHeader::decode(&bad), Err(NON_CANONICAL));
        }
        for offset in [236 + shift, 244 + shift, 252 + shift] {
            let mut bad = bytes.clone();
            bad[offset..offset + 8].fill(0);
            assert_eq!(MarketHeader::decode(&bad), Err(NON_CANONICAL));
        }
        for (offset, replacements) in [
            (0, vec![vec![0, 0], vec![0, 2], vec![255; 2]]),
            (260 + shift, vec![vec![0, 0, 0, 0, 0, 0, 0, 1]]),
            (269 + shift, vec![vec![0, 0, 0, 0, 0, 0, 0, 1]]),
            (277 + shift, vec![vec![6], vec![255]]),
            (278 + shift, vec![vec![0, 1]]),
        ] {
            for replacement in replacements {
                let mut bad = bytes.clone();
                bad[offset..offset + replacement.len()].copy_from_slice(&replacement);
                let expected = if offset == 0 {
                    BAD_VERSION
                } else {
                    NON_CANONICAL
                };
                assert_eq!(MarketHeader::decode(&bad), Err(expected));
            }
        }
        let mut bad = bytes.clone();
        bad[259 + shift] = 2;
        assert_eq!(MarketHeader::decode(&bad), Err(NON_CANONICAL));
    }
    let h = header(true)?;
    let mut bytes = [0; 416];
    h.encode(&mut bytes)?;
    let mut zero = bytes;
    zero[227..259].fill(0);
    assert_eq!(MarketHeader::decode(&zero), Err(NON_CANONICAL));
    let mut owner = bytes;
    owner[227..259].copy_from_slice(h.owner_principal.as_bytes());
    assert_eq!(MarketHeader::decode(&owner), Err(F01_PRINCIPAL_MISMATCH));
    let mut stray = bytes;
    stray[226] = 0;
    assert!(MarketHeader::decode(&stray).is_err());
    for end in 227..259 {
        assert!(MarketHeader::decode(&bytes[..end]).is_err());
    }
    let mut invalid = h;
    invalid.treasury_principal = Presence::Present(h.owner_principal);
    let mut untouched = [0xa5; 416];
    assert_eq!(invalid.encode(&mut untouched), Err(F01_PRINCIPAL_MISMATCH));
    assert_eq!(untouched, [0xa5; 416]);
    let mut absent = [0; 384];
    header(false)?.encode(&mut absent)?;
    let mut appended = absent.to_vec();
    appended.extend_from_slice(&[15; 32]);
    assert!(MarketHeader::decode(&appended).is_err());
    Ok(())
}

#[test]
fn operator_exact_layout_roundtrip_and_all_refusals() -> Checked {
    let g = grant()?;
    let mut expected = [0; 50];
    expected[..32].copy_from_slice(&[17; 32]);
    expected[32] = 3;
    expected[40] = 4;
    let mut bytes = [0; 50];
    assert_eq!(g.encode(&mut bytes), Ok(50));
    assert_eq!(bytes, expected);
    assert_eq!(OperatorGrant::decode(&expected), Ok(g));
    strict_input(&bytes, OperatorGrant::decode);
    short_outputs(50, |out| g.encode(out))?;
    for permissions in 1..=3 {
        for revoked in [false, true] {
            let full = OperatorGrant {
                permissions,
                revoked,
                sequence: u64::MAX,
                ..g
            };
            full.encode(&mut bytes)?;
            assert_eq!(bytes[32], permissions);
            assert_eq!(&bytes[33..41], &[255; 8]);
            assert_eq!(bytes[41], u8::from(revoked));
            assert_eq!(OperatorGrant::decode(&bytes), Ok(full));
        }
    }
    for value in 0..=255 {
        let mut bad = expected;
        if !(1..=3).contains(&value) {
            bad[32] = value;
            assert_eq!(OperatorGrant::decode(&bad), Err(NON_CANONICAL));
        }
        if value > 1 {
            bad = expected;
            bad[41] = value;
            assert_eq!(OperatorGrant::decode(&bad), Err(NON_CANONICAL));
        }
    }
    for index in 42..50 {
        let mut bad = expected;
        bad[index] = 1;
        assert_eq!(OperatorGrant::decode(&bad), Err(NON_CANONICAL));
    }
    let mut bad = expected;
    bad[..32].fill(0);
    assert_eq!(OperatorGrant::decode(&bad), Err(NON_CANONICAL));
    bad = expected;
    bad[33..41].fill(0);
    assert_eq!(OperatorGrant::decode(&bad), Err(NON_CANONICAL));
    let mut invalid = g;
    invalid.permissions = 0;
    let mut untouched = [0xa5; 50];
    assert_eq!(invalid.encode(&mut untouched), Err(NON_CANONICAL));
    assert_eq!(untouched, [0xa5; 50]);
    Ok(())
}

#[test]
fn clock_all_boundaries_below_origin_and_unrepresentable_endpoints() -> Checked {
    assert_eq!(market_clock(1000, 999), Err(ARITHMETIC));
    assert_eq!(
        market_clock(0, 0)?.windows,
        EpochWindows {
            start: 0,
            commit: 64,
            reveal: 80,
            settlement: 96,
            end: 128
        }
    );
    for (height, epoch, position, phase) in [
        (1000, 0, 0, EpochPhase::Work),
        (1063, 0, 63, EpochPhase::Work),
        (1064, 0, 64, EpochPhase::Commit),
        (1079, 0, 79, EpochPhase::Commit),
        (1080, 0, 80, EpochPhase::Reveal),
        (1095, 0, 95, EpochPhase::Reveal),
        (1096, 0, 96, EpochPhase::Settlement),
        (1127, 0, 127, EpochPhase::Settlement),
        (1128, 1, 0, EpochPhase::Work),
        (1255, 1, 127, EpochPhase::Settlement),
        (1256, 2, 0, EpochPhase::Work),
    ] {
        let clock = market_clock(1000, height)?;
        assert_eq!(
            (clock.epoch, clock.position, clock.phase),
            (epoch, position, phase)
        );
    }
    assert_eq!(
        EpochWindows::new(1000, 0)?,
        EpochWindows {
            start: 1000,
            commit: 1064,
            reveal: 1080,
            settlement: 1096,
            end: 1128
        }
    );
    assert_eq!(EpochWindows::new(1000, 1)?.start, 1128);
    assert_eq!(EpochWindows::new(1000, 2)?.start, 1256);
    let window = EpochWindows::new(1000, 0)?;
    assert_eq!(window.phase(999), EpochPhase::Before);
    assert_eq!(window.phase(1128), EpochPhase::After);
    assert_eq!(EpochWindows::new(0, u64::MAX), Err(ARITHMETIC));
    assert_eq!(
        EpochWindows::new(0, 144_115_188_075_855_872),
        Err(ARITHMETIC)
    );
    assert_eq!(EpochWindows::new(u64::MAX, 1), Err(ARITHMETIC));
    for origin in [
        u64::MAX,
        u64::MAX - 63,
        u64::MAX - 64,
        u64::MAX - 79,
        u64::MAX - 80,
        u64::MAX - 95,
        u64::MAX - 96,
        u64::MAX - 127,
    ] {
        assert_eq!(EpochWindows::new(origin, 0), Err(ARITHMETIC));
        assert_eq!(market_clock(origin, origin), Err(ARITHMETIC));
    }
    let last = market_clock(u64::MAX - 128, u64::MAX - 1)?;
    assert_eq!(last.windows.end, u64::MAX);
    assert_eq!(last.position, 127);
    assert_eq!(last.phase, EpochPhase::Settlement);
    assert_eq!(market_clock(u64::MAX - 128, u64::MAX), Err(ARITHMETIC));
    assert_eq!(market_clock(0, u64::MAX), Err(ARITHMETIC));
    assert_eq!(
        EpochWindows::new(0, 144_115_188_075_855_871),
        Err(ARITHMETIC)
    );
    Ok(())
}

#[test]
fn exact_maximum_worksheet_measured_owned_records_and_capacity_refusals() -> Checked {
    let maximum = maximum_f01_worksheet()?;
    assert_eq!(maximum.non_task_bytes, 1423);
    assert_eq!(maximum.task_bytes, 14912);
    assert_eq!(maximum.section_bytes, 16335);
    assert_eq!(maximum.section_headroom, 49);
    assert_eq!(maximum.charged_section_bytes, 16343);
    let mut h = header(true)?;
    h.active_config_version = 4;
    h.highest_config_version = 5;
    let mut current = policy()?;
    current.config_version = 4;
    let recent = [
        history(1, 1)?,
        history(2, 2)?,
        history(3, 2)?,
        history(4, 1)?,
    ];
    let layout = F01NonTaskLayout {
        header: &h,
        operator: Presence::Present(grant()?),
        current: &current,
        pending: Presence::Present(pending(5)?),
        recent: &recent,
        history_root: initial_policy_history_root()?,
        task_count: 64,
        task_set_root: Presence::Present(Digest32::new([23; 32])?),
    };
    assert_eq!(layout.measured_worksheet(), Ok(maximum));
    let mut too_many = layout;
    too_many.task_count = 65;
    assert_eq!(too_many.measured_worksheet(), Err(F01_CAPACITY_UNAVAILABLE));
    too_many.task_count = usize::MAX;
    assert_eq!(too_many.measured_worksheet(), Err(F01_CAPACITY_UNAVAILABLE));
    assert_eq!(check_f01_capacity(16335, 196_608), Ok(()));
    assert_eq!(check_f01_capacity(16376, 196_608), Ok(()));
    for size in [16377, 16384, 16385, usize::MAX] {
        assert_eq!(
            check_f01_capacity(size, 196_608),
            Err(F01_CAPACITY_UNAVAILABLE)
        );
    }
    assert_eq!(
        check_f01_capacity(16335, 196_609),
        Err(F01_CAPACITY_UNAVAILABLE)
    );
    assert_eq!(
        check_f01_capacity(16335, usize::MAX),
        Err(F01_CAPACITY_UNAVAILABLE)
    );
    assert_eq!(
        check_f01_capacity(16335, 16342),
        Err(F01_CAPACITY_UNAVAILABLE)
    );
    let h = header(false)?;
    let p = policy()?;
    let empty = F01NonTaskLayout {
        header: &h,
        operator: Presence::Absent,
        current: &p,
        pending: Presence::Absent,
        recent: &[],
        history_root: initial_policy_history_root()?,
        task_count: 0,
        task_set_root: Presence::Absent,
    };
    let measured = empty.measured_worksheet()?;
    assert_eq!(measured.non_task_bytes, 734);
    assert_eq!(measured.section_bytes, 734);
    assert_eq!(measured.task_bytes, 0);
    Ok(())
}

#[test]
fn exhaustive_policy_history_enums_and_invalid_record_output_is_unchanged() -> Checked {
    let expected = policy_bytes();
    for value in 0..=255 {
        if !(1..=3).contains(&value) {
            let mut bytes = expected;
            bytes[8] = value;
            assert_eq!(TaskPolicyV1::decode(&bytes), Err(F01_INVALID_POLICY));
        }
        if !(1..=2).contains(&value) {
            let mut bytes = expected;
            bytes[9] = value;
            assert_eq!(TaskPolicyV1::decode(&bytes), Err(F01_INVALID_POLICY));
            let mut h = history(1, 1)?;
            h.disposition = value;
            let before = h;
            let mut output = [0xa5; 49];
            assert_eq!(h.encode(&mut output), Err(NON_CANONICAL));
            assert_eq!(h, before);
            assert_eq!(output, [0xa5; 49]);
        }
    }
    for index in 0..16 {
        let mut p = policy()?;
        p.reserved[index] = 1;
        let mut output = [0xa5; 307];
        assert_eq!(p.encode(&mut output), Err(F01_INVALID_POLICY));
        assert_eq!(output, [0xa5; 307]);
    }
    for index in 0..8 {
        let mut h = header(false)?;
        h.reserved[index] = 1;
        let mut output = [0xa5; 416];
        assert_eq!(h.encode(&mut output), Err(NON_CANONICAL));
        assert_eq!(output, [0xa5; 416]);
        let mut g = grant()?;
        g.reserved[index] = 1;
        let mut output = [0xa5; 50];
        assert_eq!(g.encode(&mut output), Err(NON_CANONICAL));
        assert_eq!(output, [0xa5; 50]);
    }
    let mut p = pending(2)?;
    p.policy.assessment_mode = 0;
    let mut output = [0xa5; 379];
    assert_eq!(p.encode(&mut output), Err(F01_INVALID_POLICY));
    assert_eq!(output, [0xa5; 379]);
    let mut h = history(1, 1)?;
    h.config_version = 0;
    let mut output = [0xa5; 49];
    assert_eq!(h.encode(&mut output), Err(NON_CANONICAL));
    assert_eq!(output, [0xa5; 49]);
    Ok(())
}
