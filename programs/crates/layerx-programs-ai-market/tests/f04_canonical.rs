use layerx_programs_ai_market::{
    codec::{self, ReportBody, ScoreVector},
    commit_reveal::commitment as product,
    errors::*,
    types::*,
};
use sha2::{Digest, Sha256};

fn binding() -> EvaluatorBinding {
    EvaluatorBinding {
        frozen: FrozenBinding {
            chain: ChainDomain::new([0x11; 32]).unwrap(),
            program: ProgramId::new([0x22; 32]).unwrap(),
            market: MarketId::new([0x33; 32]).unwrap(),
            epoch: 7,
            config: Version::new(2).unwrap(),
            roster: RosterDigest::new([0x55; 32]).unwrap(),
        },
        evaluator: EvaluatorId::new([0x44; 32]).unwrap(),
        grant: Version::new(3).unwrap(),
        key_version: Version::new(4).unwrap(),
    }
}
fn workers() -> [WorkerId; 2] {
    [
        WorkerId::new([0x10; 32]).unwrap(),
        WorkerId::new([0x20; 32]).unwrap(),
    ]
}
fn body(entries: &[ScoreEntry]) -> Vec<u8> {
    let report = ReportBody {
        binding: binding(),
        evidence: EvidenceRoot::new([0x66; 32]).unwrap(),
        scores: ScoreVector::Typed(entries),
    };
    let mut bytes = [0; 1380];
    let n = codec::encode_report(&report, &mut bytes).unwrap();
    bytes[..n].to_vec()
}
fn vector_body() -> Vec<u8> {
    body(&[
        ScoreEntry {
            worker: workers()[0],
            score: Score::new(250_000).unwrap(),
        },
        ScoreEntry {
            worker: workers()[1],
            score: Score::new(750_000).unwrap(),
        },
    ])
}
fn mathematical_hash(bytes: &[u8]) -> [u8; 32] {
    Sha256::digest(bytes).into()
}
fn independent_binding() -> Vec<u8> {
    let mut b = Vec::new();
    for value in [0x11, 0x22, 0x33] {
        b.extend_from_slice(&[value; 32]);
    }
    b.extend_from_slice(&7_u64.to_be_bytes());
    b.extend_from_slice(&2_u64.to_be_bytes());
    b.extend_from_slice(&[0x55; 32]);
    b.extend_from_slice(&[0x44; 32]);
    b.extend_from_slice(&3_u64.to_be_bytes());
    b.extend_from_slice(&4_u64.to_be_bytes());
    b
}
#[test]
fn exact_binding_and_all_nine_mismatches() {
    let b = binding();
    let bytes = product::encode_binding(&b).unwrap();
    assert_eq!(bytes.as_slice(), independent_binding());
    assert_eq!(product::decode_binding(&bytes).unwrap(), b);
    for length in 0..192 {
        assert_eq!(
            product::decode_binding(&bytes[..length]),
            Err(NON_CANONICAL)
        );
    }
    let mut trailing = bytes.to_vec();
    trailing.push(0);
    assert_eq!(product::decode_binding(&trailing), Err(NON_CANONICAL));
    for (offset, error) in [
        (0, WRONG_DOMAIN),
        (32, WRONG_PROGRAM),
        (64, WRONG_MARKET),
        (103, WRONG_EPOCH),
        (111, WRONG_CONFIG),
        (112, WRONG_ROSTER),
        (144, UNAUTHORIZED),
        (183, UNAUTHORIZED),
        (191, KEY_MISMATCH),
    ] {
        let mut changed = bytes;
        changed[offset] ^= 1;
        let changed = product::decode_binding(&changed).unwrap();
        assert_eq!(product::check_binding(&changed, &b), Err(error));
    }
    for (offset, length) in [
        (0, 32),
        (32, 32),
        (64, 32),
        (104, 8),
        (112, 32),
        (144, 32),
        (176, 8),
        (184, 8),
    ] {
        let mut bad = bytes;
        bad[offset..offset + length].fill(0);
        assert_eq!(product::decode_binding(&bad), Err(NON_CANONICAL));
    }
    let mut epoch_zero = bytes;
    epoch_zero[96..104].fill(0);
    assert_eq!(
        product::decode_binding(&epoch_zero).unwrap().frozen.epoch,
        0
    );
    let mut maximum = bytes;
    maximum[104..112].fill(255);
    assert_eq!(
        product::decode_binding(&maximum)
            .unwrap()
            .frozen
            .config
            .get(),
        u64::MAX
    );
}
#[test]
fn section12_exact_preimages_and_independent_sha256() {
    let mut expected_body = vec![0, 1];
    expected_body.extend(independent_binding());
    expected_body.extend_from_slice(&[0x66; 32]);
    expected_body.extend_from_slice(&[0, 2]);
    expected_body.extend_from_slice(&[0x10; 32]);
    expected_body.extend_from_slice(&[0, 3, 0xd0, 0x90]);
    expected_body.extend_from_slice(&[0x20; 32]);
    expected_body.extend_from_slice(&[0, 0x0b, 0x71, 0xb0]);
    let actual = vector_body();
    assert_eq!(actual, expected_body);
    assert_eq!(actual.len(), 300);
    product::validate_report(&actual, &binding(), &workers()).unwrap();
    let mut expected = b"PAXAI/score-report/v1\0".to_vec();
    expected.extend(&expected_body);
    let mut output = [0; 1402];
    let n = product::report_preimage(&actual, &mut output).unwrap();
    assert_eq!(n, 322);
    assert_eq!(&output[..n], expected);
    let report = ReportDigest::new(mathematical_hash(&expected)).unwrap();
    assert_eq!(
        codec::report_digest(&codec::decode_report(&actual).unwrap()).unwrap(),
        report
    );
    let mut attestation = b"PAXAI/score-attestation/v1\0".to_vec();
    attestation.extend(report.bytes());
    assert_eq!(
        product::attestation_preimage(report).as_slice(),
        attestation
    );
    assert_eq!(attestation.len(), 59);
    assert_eq!(
        codec::attestation_digest(report).unwrap().bytes(),
        mathematical_hash(&attestation)
    );
    let salt_bytes: [u8; 32] = core::array::from_fn(|i| u8::try_from(i + 1).unwrap());
    let salt = product::decode_salt(&salt_bytes).unwrap();
    let mut commitment = b"PAXAI/score-commit/v1\0".to_vec();
    commitment.extend(independent_binding());
    commitment.extend(report.bytes());
    commitment.extend(salt_bytes);
    let preimage = product::commitment_preimage(&binding(), report, salt).unwrap();
    assert_eq!(preimage.as_slice(), commitment);
    assert_eq!(commitment.len(), 278);
    let digest = CommitmentDigest::new(mathematical_hash(&commitment)).unwrap();
    assert_eq!(
        codec::commitment_digest(&binding(), report, salt).unwrap(),
        digest
    );
    assert_eq!(product::check_commitment(digest, digest), Ok(()));
    for offset in [22, 54, 86, 125, 133, 134, 166, 205, 213, 214, 246] {
        let mut changed = preimage;
        changed[offset] ^= 1;
        let other = CommitmentDigest::new(mathematical_hash(&changed)).unwrap();
        assert_eq!(
            product::check_commitment(other, digest),
            Err(F04_COMMIT_MISMATCH)
        );
    }
    for offset in [194, 263] {
        let mut changed = actual.clone();
        changed[offset] ^= 1;
        let n = product::report_preimage(&changed, &mut output).unwrap();
        assert_ne!(mathematical_hash(&output[..n]), report.bytes());
    }
}
#[test]
fn strict_rows_empty_and_explicit_zero() {
    let bytes = vector_body();
    let b = binding();
    let roster = workers();
    for n in 0..bytes.len() {
        assert!(product::validate_report(&bytes[..n], &b, &roster).is_err());
    }
    let mut reversed = bytes.clone();
    reversed[228..264].copy_from_slice(&bytes[264..300]);
    reversed[264..300].copy_from_slice(&bytes[228..264]);
    assert_eq!(
        product::validate_report(&reversed, &b, &roster).unwrap_err(),
        NON_CANONICAL
    );
    let mut duplicate = bytes.clone();
    duplicate[264..296].copy_from_slice(&bytes[228..260]);
    assert_eq!(
        product::validate_report(&duplicate, &b, &roster).unwrap_err(),
        NON_CANONICAL
    );
    let mut unknown = bytes.clone();
    unknown[264..296].fill(0x30);
    assert_eq!(
        product::validate_report(&unknown, &b, &roster).unwrap_err(),
        F03_UNKNOWN_WORKER
    );
    let mut score = bytes.clone();
    score[260..264].copy_from_slice(&1_000_001_u32.to_be_bytes());
    assert_eq!(
        product::validate_report(&score, &b, &roster).unwrap_err(),
        F03_SCORE_RANGE
    );
    for count in [3_u16, 33] {
        let mut bad = bytes.clone();
        bad[226..228].copy_from_slice(&count.to_be_bytes());
        assert!(product::validate_report(&bad, &b, &roster).is_err());
    }
    let mut trailing = bytes.clone();
    trailing.push(0);
    assert_eq!(
        product::validate_report(&trailing, &b, &roster).unwrap_err(),
        NON_CANONICAL
    );
    let mut empty = bytes[..228].to_vec();
    empty[226..228].fill(0);
    assert_eq!(empty.len(), 228);
    assert_eq!(empty.len() + 22, 250);
    assert_eq!(
        product::validate_report(&empty, &b, &roster).unwrap_err(),
        F04_NO_SCORES
    );
    assert_eq!(
        product::report_preimage(&empty, &mut [0; 1402]),
        Err(F04_NO_SCORES)
    );
    let zero = body(&[ScoreEntry {
        worker: roster[0],
        score: Score::new(0).unwrap(),
    }]);
    assert_eq!(zero.len(), 264);
    assert_eq!(&zero[260..264], &[0; 4]);
    assert_eq!(
        product::validate_report(&zero, &b, &roster)
            .unwrap()
            .scores
            .len(),
        1
    );
    assert_eq!(
        product::validate_report(&bytes, &b, &[]).unwrap_err(),
        F03_UNKNOWN_WORKER
    );
    assert_eq!(
        product::validate_report(&bytes, &b, &[roster[1], roster[0]]).unwrap_err(),
        NON_CANONICAL
    );
}
#[test]
fn exact_operation_signature_salt_and_digest_framing() {
    let b = binding();
    let digest = CommitmentDigest::new([9; 32]).unwrap();
    let commit = product::encode_commit_score(b, digest).unwrap();
    assert_eq!(commit.len(), 224);
    assert_eq!(&commit[..192], independent_binding());
    assert_eq!(&commit[192..], &[9; 32]);
    assert_eq!(product::decode_commit_score(&commit).unwrap().binding, b);
    for n in 0..224 {
        assert!(product::decode_commit_score(&commit[..n]).is_err());
    }
    let mut bad = commit.to_vec();
    bad.push(0);
    assert!(product::decode_commit_score(&bad).is_err());
    let mut zero = commit;
    zero[192..].fill(0);
    assert!(product::decode_commit_score(&zero).is_err());
    assert!(ReportDigest::new([0; 32]).is_err());
    assert!(CommitmentDigest::new([0; 32]).is_err());
    for n in [0, 31, 33] {
        assert_eq!(product::decode_salt(&vec![1; n]), Err(NON_CANONICAL));
    }
    assert_eq!(product::decode_salt(&[0; 32]), Err(F04_SALT_INVALID));
    for n in [0, 63, 65] {
        assert_eq!(product::decode_signature(&vec![1; n]), Err(NON_CANONICAL));
    }
    let mut signature = [1; 64];
    assert_eq!(product::decode_signature(&signature).unwrap().0, signature);
    signature[0] ^= 1;
    assert_eq!(product::decode_signature(&signature).unwrap().0, signature);
    let body = vector_body();
    let mut out = [0; 1480];
    let n = product::encode_reveal_score(&body, &signature, &[1; 32], &mut out).unwrap();
    assert_eq!(n, 400);
    assert_eq!(&out[..4], &300_u32.to_be_bytes());
    assert_eq!(&out[4..304], body);
    assert_eq!(&out[304..368], signature);
    assert_eq!(&out[368..400], &[1; 32]);
    let valid = out[..n].to_vec();
    assert_eq!(
        product::decode_reveal_score(&valid).unwrap().signature.0,
        signature
    );
    for n in 0..valid.len() {
        assert!(product::decode_reveal_score(&valid[..n]).is_err());
    }
    for length in [0_u32, 299, 301, u32::MAX] {
        let mut bad = valid.clone();
        bad[..4].copy_from_slice(&length.to_be_bytes());
        assert!(product::decode_reveal_score(&bad).is_err());
    }
    let mut trailing = valid.clone();
    trailing.push(0);
    assert!(product::decode_reveal_score(&trailing).is_err());
    let mut zero = valid;
    zero[368..].fill(0);
    assert_eq!(
        product::decode_reveal_score(&zero).unwrap_err(),
        F04_SALT_INVALID
    );
    assert_eq!(
        product::encode_reveal_score(&body, &signature, &[1; 32], &mut [0; 399]),
        Err(CAPACITY)
    );
}
#[test]
fn maximum_byte_and_membership_work_bounds() {
    let workers: Vec<WorkerId> = (1..=32).map(|n| WorkerId::new([n; 32]).unwrap()).collect();
    let entries: Vec<ScoreEntry> = workers
        .iter()
        .map(|&worker| ScoreEntry {
            worker,
            score: Score::new(1_000_000).unwrap(),
        })
        .collect();
    let body = body(&entries);
    assert_eq!(body.len(), 1380);
    product::validate_report(&body, &binding(), &workers).unwrap();
    assert_eq!(
        product::report_preimage(&body, &mut [0; 1402]).unwrap(),
        1402
    );
    assert_eq!(
        product::report_preimage(&body, &mut [0; 1401]),
        Err(CAPACITY)
    );
    let mut reveal = [0; 1480];
    assert_eq!(
        product::encode_reveal_score(&body, &[1; 64], &[1; 32], &mut reveal).unwrap(),
        1480
    );
    assert_eq!(
        product::decode_reveal_score(&reveal)
            .unwrap()
            .report
            .scores
            .len(),
        32
    );
    assert_eq!(body.len() + 64, 1444);
    assert_eq!(8 * (body.len() + 64), 11552);
    assert_eq!(8 * 256, 2048);
    assert!(11552 + 2048 < 24576);
    assert_eq!(layerx_programs_ai_market::MAX_STATE_BYTES, 196608);
    assert_eq!(workers.len() * entries.len(), 1024);
    let mut excess = body.clone();
    excess[226..228].copy_from_slice(&33_u16.to_be_bytes());
    excess.extend_from_slice(&[1; 36]);
    assert_eq!(
        product::validate_report(&excess, &binding(), &workers).unwrap_err(),
        CAPACITY
    );
    let mut excess_roster = workers;
    excess_roster.push(WorkerId::new([33; 32]).unwrap());
    assert_eq!(
        product::validate_report(&body, &binding(), &excess_roster).unwrap_err(),
        CAPACITY
    );
    let mut oversized = reveal.to_vec();
    oversized.push(0);
    assert_eq!(
        product::decode_reveal_score(&oversized).unwrap_err(),
        CAPACITY
    );
}
#[test]
fn version_evidence_empty_payload_and_alternate_salt_refusals() {
    let original = vector_body();
    let mut version = original.clone();
    version[..2].copy_from_slice(&2_u16.to_be_bytes());
    assert_eq!(
        product::validate_report(&version, &binding(), &workers()).unwrap_err(),
        BAD_VERSION
    );
    let mut evidence = original.clone();
    evidence[194..226].fill(0);
    assert_eq!(
        product::validate_report(&evidence, &binding(), &workers()).unwrap_err(),
        NON_CANONICAL
    );
    let mut empty = original[..228].to_vec();
    empty[226..228].fill(0);
    let mut payload = 228_u32.to_be_bytes().to_vec();
    payload.extend(&empty);
    payload.extend([1; 64]);
    payload.extend([1; 32]);
    assert_eq!(
        product::decode_reveal_score(&payload).unwrap_err(),
        F04_NO_SCORES
    );
    assert_eq!(
        product::encode_reveal_score(&empty, &[1; 64], &[1; 32], &mut [0; 1480]),
        Err(F04_NO_SCORES)
    );
    let mut scratch = [0; 1402];
    let n = product::report_preimage(&original, &mut scratch).unwrap();
    let report = ReportDigest::new(mathematical_hash(&scratch[..n])).unwrap();
    let first =
        product::commitment_preimage(&binding(), report, product::decode_salt(&[1; 32]).unwrap())
            .unwrap();
    let second =
        product::commitment_preimage(&binding(), report, product::decode_salt(&[2; 32]).unwrap())
            .unwrap();
    let first = CommitmentDigest::new(mathematical_hash(&first)).unwrap();
    let second = CommitmentDigest::new(mathematical_hash(&second)).unwrap();
    assert_eq!(
        product::check_commitment(second, first),
        Err(F04_COMMIT_MISMATCH)
    );
    assert_eq!(product::check_commitment(first, first), Ok(()));
}
