//! Real native Ed25519 and production codec tests. These test A01-A05's
//! statement obligations, not host reveal, rollback, F08 acceptance or finality.
use ed25519_dalek::{Signer, SigningKey};
use layerx_programs_ai_market::evaluators::{codec as feature, model::*};
use layerx_programs_ai_market::{codec as common, errors::*, types::*};
use sha2::{Digest, Sha256};
use std::fmt::Debug;

fn ok<T, E: Debug>(result: Result<T, E>) -> T {
    result.unwrap_or_else(|error| panic!("unexpected refusal: {error:?}"))
}
fn v(value: u64) -> Version {
    ok(Version::new(value))
}
fn signing_key() -> SigningKey {
    SigningKey::from_bytes(&[0x73; 32])
}
fn key() -> PublicKey32 {
    PublicKey32(signing_key().verifying_key().to_bytes())
}
fn score(worker: u8, value: u32) -> ScoreEntry {
    ScoreEntry {
        worker: ok(WorkerId::new([worker; 32])),
        score: ok(Score::new(value)),
    }
}
fn binding() -> EvaluatorBinding {
    EvaluatorBinding {
        frozen: FrozenBinding {
            chain: ok(ChainDomain::new([55; 32])),
            program: ok(ProgramId::new([2; 32])),
            market: ok(MarketId::new([3; 32])),
            epoch: 5,
            config: v(7),
            roster: ok(RosterDigest::new([4; 32])),
        },
        evaluator: ok(EvaluatorId::new([33; 32])),
        grant: v(9),
        key_version: v(10),
    }
}
fn grant() -> EvaluatorGrant {
    EvaluatorGrant {
        evaluator: binding().evaluator,
        principal: ok(PrincipalId::new([33; 32])),
        rubric: ok(RubricDigest::new([8; 32])),
        grant_version: v(9),
        key_version: v(10),
        signing_key: key(),
        effective_epoch: 5,
        expiry_epoch_exclusive: 7,
        status: GrantStatus::Active,
    }
}
fn worker(worker: u8) -> WorkerRosterEntry {
    WorkerRosterEntry {
        worker: ok(WorkerId::new([worker; 32])),
        owner: ok(PrincipalId::new([worker; 32])),
        recipient: ok(AccountId::new([worker; 32])),
        generation: v(1),
        key_version: v(2),
        public_key: key(),
        metadata: ok(MetadataDigest::new([6; 32])),
    }
}
fn context(workers: &[WorkerRosterEntry]) -> ReportContext<'_> {
    ReportContext {
        binding: binding(),
        frozen_grant: grant(),
        live_grant: grant(),
        approved_rubric: grant().rubric,
        market_owner: ok(PrincipalId::new([99; 32])),
        workers,
        evidence: Presence::Present(RegisteredEvidence {
            binding: binding(),
            root: ok(EvidenceRoot::new([44; 32])),
            rubric: grant().rubric,
        }),
    }
}
fn report(scores: &[ScoreEntry]) -> common::ReportBody<'_> {
    common::ReportBody {
        binding: binding(),
        evidence: ok(EvidenceRoot::new([44; 32])),
        scores: common::ScoreVector::Typed(scores),
    }
}
fn signed<'a>(body: common::ReportBody<'a>) -> SignedReport<'a> {
    let digest = ok(common::attestation_digest(ok(common::report_digest(&body))));
    SignedReport {
        body,
        signature: Signature64(signing_key().sign(&digest.bytes()).to_bytes()),
    }
}
fn encoded_body(body: &common::ReportBody<'_>) -> Vec<u8> {
    let mut bytes = [0; 1380];
    let n = ok(feature::encode_report_body(body, &mut bytes));
    bytes[..n].to_vec()
}
fn encoded_signed(report: &SignedReport<'_>) -> Vec<u8> {
    let mut bytes = [0; 1444];
    let n = ok(feature::encode_signed_report(report, &mut bytes));
    bytes[..n].to_vec()
}
/// Independent expected framing: no product encoder or digest builds expectations.
fn hash(domain: &[u8], body: &[u8]) -> [u8; 32] {
    let mut h = Sha256::new();
    h.update(domain);
    h.update([0]);
    h.update(body);
    h.finalize().into()
}
fn expected_report(entries: &[(u8, u32)]) -> Vec<u8> {
    let mut out = vec![0, 1];
    out.extend_from_slice(&[55; 32]);
    out.extend_from_slice(&[2; 32]);
    out.extend_from_slice(&[3; 32]);
    out.extend_from_slice(&5_u64.to_be_bytes());
    out.extend_from_slice(&7_u64.to_be_bytes());
    out.extend_from_slice(&[4; 32]);
    out.extend_from_slice(&[33; 32]);
    out.extend_from_slice(&9_u64.to_be_bytes());
    out.extend_from_slice(&10_u64.to_be_bytes());
    out.extend_from_slice(&[44; 32]);
    out.extend_from_slice(
        &u16::try_from(entries.len())
            .unwrap_or_else(|e| panic!("{e}"))
            .to_be_bytes(),
    );
    for (id, value) in entries {
        out.extend_from_slice(&[*id; 32]);
        out.extend_from_slice(&value.to_be_bytes());
    }
    out
}
fn expected_grant(status: u8) -> Vec<u8> {
    let mut out = vec![33; 32];
    out.extend_from_slice(&[33; 32]);
    out.extend_from_slice(&[8; 32]);
    out.extend_from_slice(&9_u64.to_be_bytes());
    out.extend_from_slice(&10_u64.to_be_bytes());
    out.extend_from_slice(&signing_key().verifying_key().to_bytes());
    out.extend_from_slice(&5_u64.to_be_bytes());
    out.extend_from_slice(&7_u64.to_be_bytes());
    out.push(status);
    out
}
fn application(error: ApplicationError) -> VerificationError {
    VerificationError::Application(error)
}

#[test]
fn a01_a02_explicit_zero_is_a_vote_and_omission_is_absent() {
    let workers = [worker(11), worker(22)];
    let ctx = context(&workers);
    let entries = [score(11, 700_000), score(22, 0)];
    let statement = signed(report(&entries));
    let bytes = encoded_signed(&statement);
    assert_eq!(bytes.len(), 364);
    let decoded = ok(feature::decode_signed_report(&bytes));
    assert_eq!(
        ok(feature::verify_signed_report(&decoded, &ctx)),
        ok(common::report_digest(&statement.body))
    );
    assert_eq!(
        decoded.body.scores.entries().map(ok).collect::<Vec<_>>(),
        entries
    );
    assert_eq!(bytes[..300], expected_report(&[(11, 700_000), (22, 0)]));
    let omitted = signed(report(&entries[..1]));
    let omitted_bytes = encoded_signed(&omitted);
    let decoded_omitted = ok(feature::decode_signed_report(&omitted_bytes));
    ok(feature::verify_signed_report(&decoded_omitted, &ctx));
    assert!(!decoded_omitted
        .body
        .scores
        .entries()
        .map(ok)
        .any(|entry| entry.worker == workers[1].worker));
    assert!(decoded
        .body
        .scores
        .entries()
        .map(ok)
        .any(|entry| entry.worker == workers[1].worker && entry.score.get() == 0));
    assert_ne!(
        ok(common::report_digest(&omitted.body)),
        ok(common::report_digest(&statement.body))
    );
    // Pure validation has no mutable state arguments; input facts stay intact.
    assert_eq!(ctx.frozen_grant, grant());
    assert_eq!(ctx.live_grant, grant());
    assert_eq!(workers, [worker(11), worker(22)]);
}

#[test]
fn independent_report_attestation_and_real_signature_framing() {
    let entries = [score(11, 700_000)];
    let body = report(&entries);
    let expected = expected_report(&[(11, 700_000)]);
    assert_eq!(expected.len(), 264);
    assert_eq!(encoded_body(&body), expected);
    let report_hash = hash(b"PAXAI/score-report/v1", &expected);
    let attestation_hash = hash(b"PAXAI/score-attestation/v1", &report_hash);
    assert_eq!(ok(common::report_digest(&body)).bytes(), report_hash);
    assert_eq!(
        ok(common::attestation_digest(ok(common::report_digest(&body)))).bytes(),
        attestation_hash
    );
    let statement = signed(body);
    let mut expected_signed = expected;
    expected_signed.extend_from_slice(&signing_key().sign(&attestation_hash).to_bytes());
    assert_eq!(encoded_signed(&statement), expected_signed);
    assert_eq!(expected_signed.len(), 328);
    let workers = [worker(11)];
    let ctx = context(&workers);
    ok(feature::verify_signed_report(&statement, &ctx));
    let wrong_message = SignedReport {
        body,
        signature: Signature64(signing_key().sign(&report_hash).to_bytes()),
    };
    assert_eq!(
        feature::verify_signed_report(&wrong_message, &ctx),
        Err(application(BAD_SIGNATURE))
    );
    let wrong_purpose = SignedReport {
        body,
        signature: Signature64(
            signing_key()
                .sign(&hash(b"PAXAI/request/v1", &report_hash))
                .to_bytes(),
        ),
    };
    assert_eq!(
        feature::verify_signed_report(&wrong_purpose, &ctx),
        Err(application(BAD_SIGNATURE))
    );
    for offset in 0..64 {
        let mut changed = statement;
        changed.signature.0[offset] ^= 1;
        assert_eq!(
            feature::verify_signed_report(&changed, &ctx),
            Err(application(BAD_SIGNATURE))
        );
    }
    let mut changed_bytes = expected_signed.clone();
    changed_bytes[263] ^= 1;
    let changed = ok(feature::decode_signed_report(&changed_bytes));
    assert_eq!(
        feature::verify_signed_report(&changed, &ctx),
        Err(application(BAD_SIGNATURE))
    );
    let mut another_signature = statement;
    another_signature.signature = Signature64(signing_key().sign(&[9; 32]).to_bytes());
    assert_eq!(
        ok(common::report_digest(&another_signature.body)),
        ok(common::report_digest(&statement.body))
    );
}

#[test]
fn a03_unsorted_duplicate_input_refuses_without_sorting_or_output_writes() {
    for entries in [[score(22, 5), score(11, 6)], [score(11, 5), score(11, 6)]] {
        let original = entries;
        let body = report(&entries);
        let mut out = [0xab; 1444];
        let before = out;
        assert_eq!(
            feature::encode_report_body(&body, &mut out),
            Err(F03_NONCANONICAL_VECTOR)
        );
        assert_eq!(out, before);
        assert_eq!(entries, original);
        let mut malformed = expected_report(&[(22, 5), (11, 6)]);
        if entries[0].worker == entries[1].worker {
            malformed = expected_report(&[(11, 5), (11, 6)]);
        }
        assert_eq!(
            feature::decode_report_body(&malformed),
            Err(F03_NONCANONICAL_VECTOR)
        );
        malformed.extend_from_slice(&[0; 64]);
        assert_eq!(
            feature::decode_signed_report(&malformed),
            Err(F03_NONCANONICAL_VECTOR)
        );
    }
}

#[test]
fn a04_scores_counts_bounds_and_exact_maximum() {
    for value in [0, 1_000_000] {
        let entries = [score(11, value)];
        let body = report(&entries);
        assert_eq!(
            ok(feature::decode_report_body(&encoded_body(&body)))
                .scores
                .entries()
                .map(ok)
                .collect::<Vec<_>>(),
            entries
        );
    }
    assert_eq!(Score::new(1_000_001), Err(F03_SCORE_RANGE));
    let out_of_range = expected_report(&[(11, 1_000_001)]);
    assert_eq!(
        feature::decode_report_body(&out_of_range),
        Err(F03_SCORE_RANGE)
    );
    let no_entries = report(&[]);
    let mut out = [0xcd; 1444];
    let before = out;
    assert_eq!(
        feature::encode_report_body(&no_entries, &mut out),
        Err(F03_NO_SCORES)
    );
    assert_eq!(out, before);
    let mut empty = expected_report(&[]);
    assert_eq!(feature::decode_report_body(&empty), Err(F03_NO_SCORES));
    empty.extend_from_slice(&[0; 64]);
    assert_eq!(feature::decode_signed_report(&empty), Err(F03_NO_SCORES));
    let entries: Vec<_> = (1..=32).map(|id| score(id, 0)).collect();
    let expected: Vec<_> = (1..=32).map(|id| (id, 0)).collect();
    let body = report(&entries);
    let statement = signed(body);
    let bytes = encoded_signed(&statement);
    assert_eq!(encoded_body(&body).len(), 1380);
    assert_eq!(bytes.len(), 1444);
    assert_eq!(&bytes[..1380], expected_report(&expected));
    assert_eq!(
        ok(feature::decode_signed_report(&bytes)).body.scores.len(),
        32
    );
    let workers: Vec<_> = (1..=32).map(worker).collect();
    ok(feature::verify_signed_report(
        &statement,
        &context(&workers),
    ));
    let too_many: Vec<_> = (1..=33).map(|id| score(id, 0)).collect();
    let mut out = [0xee; 1500];
    let before = out;
    assert_eq!(
        feature::encode_report_body(&report(&too_many), &mut out),
        Err(NON_CANONICAL)
    );
    assert_eq!(out, before);
    let encoded_33 = expected_report(&(1..=33).map(|id| (id, 0)).collect::<Vec<_>>());
    assert_eq!(feature::decode_report_body(&encoded_33), Err(NON_CANONICAL));
    let mut count_33 = expected_report(&[(11, 0)]);
    count_33[226..228].copy_from_slice(&33_u16.to_be_bytes());
    assert_eq!(feature::decode_report_body(&count_33), Err(NON_CANONICAL));
}

#[test]
fn report_every_truncation_trailing_length_schema_zero_and_short_output() {
    for count in [1, 32] {
        let entries: Vec<_> = (1..=count).map(|id| score(id, 0)).collect();
        let body = report(&entries);
        let bytes = encoded_body(&body);
        let statement = signed(body);
        let signed_bytes = encoded_signed(&statement);
        for end in 0..bytes.len() {
            assert!(
                feature::decode_report_body(&bytes[..end]).is_err(),
                "body prefix {end}"
            );
        }
        for end in 0..signed_bytes.len() {
            assert!(
                feature::decode_signed_report(&signed_bytes[..end]).is_err(),
                "signed prefix {end}"
            );
        }
        for suffix in [vec![0], vec![0; 32], vec![0; 64]] {
            let mut changed = bytes.clone();
            changed.extend_from_slice(&suffix);
            assert!(feature::decode_report_body(&changed).is_err());
            let mut changed = signed_bytes.clone();
            changed.extend_from_slice(&suffix);
            assert!(feature::decode_signed_report(&changed).is_err());
        }
        let mut short = vec![0xbc; signed_bytes.len() - 1];
        let before = short.clone();
        assert_eq!(
            feature::encode_signed_report(&statement, &mut short),
            Err(CAPACITY)
        );
        assert_eq!(short, before);
        let mut short = vec![0xbc; bytes.len() - 1];
        let before = short.clone();
        assert_eq!(
            feature::encode_report_body(&body, &mut short),
            Err(CAPACITY)
        );
        assert_eq!(short, before);
    }
    let bytes = expected_report(&[(11, 0)]);
    for version in [0_u16, 2, u16::MAX] {
        let mut changed = bytes.clone();
        changed[..2].copy_from_slice(&version.to_be_bytes());
        assert_eq!(feature::decode_report_body(&changed), Err(BAD_VERSION));
    }
    for (start, length) in [
        (2, 32),
        (34, 32),
        (66, 32),
        (106, 8),
        (114, 32),
        (146, 32),
        (178, 8),
        (186, 8),
        (194, 32),
        (228, 32),
    ] {
        let mut changed = bytes.clone();
        changed[start..start + length].fill(0);
        assert_eq!(
            feature::decode_report_body(&changed),
            Err(NON_CANONICAL),
            "zero at {start}"
        );
    }
    let mut mismatch = bytes.clone();
    mismatch[226..228].copy_from_slice(&2_u16.to_be_bytes());
    assert_eq!(feature::decode_report_body(&mismatch), Err(NON_CANONICAL));
    let malformed_vector = common::ReportBody {
        scores: common::ScoreVector::Encoded(&[1; 35]),
        ..report(&[])
    };
    assert_eq!(
        feature::encode_report_body(&malformed_vector, &mut [0; 1380]),
        Err(NON_CANONICAL)
    );
}

#[test]
fn a05_all_frozen_domain_fields_and_precedence_before_bad_signature() {
    let entries = [score(11, 700_000)];
    let workers = [worker(11), worker(22)];
    let ctx = context(&workers);
    let statement = signed(report(&entries));
    let mut changes = Vec::new();
    let mut b = binding();
    b.frozen.chain = ok(ChainDomain::new([56; 32]));
    changes.push((b, WRONG_DOMAIN));
    let mut b = binding();
    b.frozen.program = ok(ProgramId::new([6; 32]));
    changes.push((b, WRONG_DOMAIN));
    let mut b = binding();
    b.frozen.market = ok(MarketId::new([6; 32]));
    changes.push((b, WRONG_DOMAIN));
    let mut b = binding();
    b.frozen.epoch = 6;
    changes.push((b, WRONG_EPOCH));
    let mut b = binding();
    b.frozen.config = v(8);
    changes.push((b, WRONG_CONFIG));
    let mut b = binding();
    b.frozen.roster = ok(RosterDigest::new([6; 32]));
    changes.push((b, WRONG_ROSTER));
    let mut b = binding();
    b.evaluator = ok(EvaluatorId::new([6; 32]));
    changes.push((b, F03_NO_GRANT));
    let mut b = binding();
    b.grant = v(11);
    changes.push((b, F03_GRANT_VERSION_CONFLICT));
    let mut b = binding();
    b.key_version = v(11);
    changes.push((b, F03_KEY_VERSION_CONFLICT));
    for (binding, expected) in changes {
        let mut changed = statement;
        changed.body.binding = binding;
        assert_eq!(
            feature::verify_signed_report(&changed, &ctx),
            Err(application(expected))
        );
        assert_ne!(
            ok(common::report_digest(&changed.body)),
            ok(common::report_digest(&statement.body))
        );
    }
    let mut changed = statement;
    changed.body.evidence = ok(EvidenceRoot::new([45; 32]));
    assert_eq!(
        feature::verify_signed_report(&changed, &ctx),
        Err(application(F03_EVIDENCE_ROOT_MISMATCH))
    );
    let mut ctx_bad = ctx;
    ctx_bad.live_grant.status = GrantStatus::Revoked;
    changed.body.binding.frozen.chain = ok(ChainDomain::new([56; 32]));
    assert_eq!(
        feature::verify_signed_report(&changed, &ctx_bad),
        Err(application(WRONG_DOMAIN))
    );
    changed.body.binding = binding();
    assert_eq!(
        feature::verify_signed_report(&changed, &ctx_bad),
        Err(application(REVOKED))
    );
}

#[test]
fn live_frozen_key_evidence_roster_and_lifecycle_refusals() {
    let entries = [score(11, 0)];
    let workers = [worker(11), worker(22)];
    let ctx = context(&workers);
    let statement = signed(report(&entries));
    for status in [
        GrantStatus::Pending,
        GrantStatus::Revoked,
        GrantStatus::Expired,
    ] {
        let mut changed = ctx;
        changed.live_grant.status = status;
        let error = match status {
            GrantStatus::Revoked => REVOKED,
            GrantStatus::Expired => EXPIRED,
            _ => F03_NO_GRANT,
        };
        assert_eq!(
            feature::verify_signed_report(&statement, &changed),
            Err(application(error))
        );
    }
    let mut changed = ctx;
    changed.live_grant.expiry_epoch_exclusive = 5;
    changed.live_grant.effective_epoch = 4;
    assert_eq!(
        feature::verify_signed_report(&statement, &changed),
        Err(application(EXPIRED))
    );
    let mut changed = ctx;
    changed.live_grant.effective_epoch = 6;
    assert_eq!(
        feature::verify_signed_report(&statement, &changed),
        Err(application(F03_NO_GRANT))
    );
    let mut changed = ctx;
    changed.live_grant.key_version = v(11);
    assert_eq!(
        feature::verify_signed_report(&statement, &changed),
        Err(application(F03_KEY_VERSION_CONFLICT))
    );
    let mut changed = ctx;
    changed.live_grant.grant_version = v(11);
    assert_eq!(
        feature::verify_signed_report(&statement, &changed),
        Err(application(F03_GRANT_VERSION_CONFLICT))
    );
    let other_key = PublicKey32(
        SigningKey::from_bytes(&[0x42; 32])
            .verifying_key()
            .to_bytes(),
    );
    let mut changed = ctx;
    changed.live_grant.signing_key = other_key;
    assert_eq!(
        feature::verify_signed_report(&statement, &changed),
        Err(application(KEY_MISMATCH))
    );
    changed.frozen_grant.signing_key = other_key;
    assert_eq!(
        feature::verify_signed_report(&statement, &changed),
        Err(application(BAD_SIGNATURE))
    );
    let mut changed = ctx;
    changed.frozen_grant.signing_key = PublicKey32([0; 32]);
    assert_eq!(
        feature::verify_signed_report(&statement, &changed),
        Err(application(NON_CANONICAL))
    );
    let mut changed = ctx;
    changed.approved_rubric = ok(RubricDigest::new([7; 32]));
    assert_eq!(
        feature::verify_signed_report(&statement, &changed),
        Err(application(WRONG_CONFIG))
    );
    let mut changed = ctx;
    changed.market_owner = grant().principal;
    assert_eq!(
        feature::verify_signed_report(&statement, &changed),
        Err(application(ROLE_CONFLICT))
    );
    let mut conflicting_workers = workers;
    conflicting_workers[1].owner = grant().principal;
    let changed = context(&conflicting_workers);
    assert_eq!(
        feature::verify_signed_report(&statement, &changed),
        Err(application(ROLE_CONFLICT))
    );
    let reversed = [worker(22), worker(11)];
    let changed = context(&reversed);
    assert_eq!(
        feature::verify_signed_report(&statement, &changed),
        Err(application(WRONG_ROSTER))
    );
    let mut changed = ctx;
    changed.evidence = Presence::Absent;
    assert_eq!(
        feature::verify_signed_report(&statement, &changed),
        Err(application(F03_EVIDENCE_NOT_SEALED))
    );
    let unknown = [score(12, 0)];
    let unknown = signed(report(&unknown));
    assert_eq!(
        feature::verify_signed_report(&unknown, &ctx),
        Err(application(F03_UNKNOWN_WORKER))
    );
    // F09 registration must match every frozen binding, not merely its root.
    for field in 0..10 {
        let mut evidence = RegisteredEvidence {
            binding: binding(),
            root: statement.body.evidence,
            rubric: grant().rubric,
        };
        match field {
            0 => evidence.binding.frozen.chain = ok(ChainDomain::new([6; 32])),
            1 => evidence.binding.frozen.program = ok(ProgramId::new([6; 32])),
            2 => evidence.binding.frozen.market = ok(MarketId::new([6; 32])),
            3 => evidence.binding.frozen.epoch = 6,
            4 => evidence.binding.frozen.config = v(8),
            5 => evidence.binding.frozen.roster = ok(RosterDigest::new([6; 32])),
            6 => evidence.binding.evaluator = ok(EvaluatorId::new([6; 32])),
            7 => evidence.binding.grant = v(11),
            8 => evidence.binding.key_version = v(11),
            _ => evidence.rubric = ok(RubricDigest::new([6; 32])),
        }
        let mut changed = ctx;
        changed.evidence = Presence::Present(evidence);
        assert_eq!(
            feature::verify_signed_report(&statement, &changed),
            Err(application(EVIDENCE_BINDING))
        );
    }
}

#[test]
fn grant_exact_bytes_statuses_every_truncation_and_finite_checked_expiry() {
    for status in [
        GrantStatus::Pending,
        GrantStatus::Active,
        GrantStatus::Revoked,
        GrantStatus::Expired,
    ] {
        let value = EvaluatorGrant { status, ..grant() };
        let mut out = [0; 161];
        assert_eq!(ok(feature::encode_grant(&value, &mut out)), 161);
        assert_eq!(out.as_slice(), expected_grant(status as u8));
        assert_eq!(ok(feature::decode_grant(&out)), value);
        for end in 0..161 {
            assert!(feature::decode_grant(&out[..end]).is_err());
        }
        let mut trailing = out.to_vec();
        trailing.push(0);
        assert_eq!(feature::decode_grant(&trailing), Err(NON_CANONICAL));
    }
    for status in [0, 5, 255] {
        let mut bytes = expected_grant(2);
        bytes[160] = status;
        assert_eq!(feature::decode_grant(&bytes), Err(NON_CANONICAL));
    }
    for (start, length) in [(0, 32), (32, 32), (64, 32), (96, 8), (104, 8), (112, 32)] {
        let mut bytes = expected_grant(2);
        bytes[start..start + length].fill(0);
        assert_eq!(feature::decode_grant(&bytes), Err(NON_CANONICAL));
    }
    for expiry in [0_u64, 4, 5, 38, u64::MAX] {
        let value = EvaluatorGrant {
            expiry_epoch_exclusive: expiry,
            ..grant()
        };
        let mut out = [0xa5; 161];
        let before = out;
        assert_eq!(
            feature::encode_grant(&value, &mut out),
            Err(F03_BAD_ACTIVATION)
        );
        assert_eq!(out, before);
        let mut bytes = expected_grant(2);
        bytes[152..160].copy_from_slice(&expiry.to_be_bytes());
        assert_eq!(feature::decode_grant(&bytes), Err(F03_BAD_ACTIVATION));
    }
    assert_eq!(ok(default_expiry(5)), 37);
    assert_eq!(default_expiry(u64::MAX), Err(ARITHMETIC));
    assert_eq!(default_expiry(u64::MAX - 31), Err(ARITHMETIC));
    assert_eq!(ok(default_expiry(u64::MAX - 32)), u64::MAX);
    assert_eq!(v(u64::MAX).next(), Err(ARITHMETIC));
    assert_eq!(Version::new(0), Err(NON_CANONICAL));
    let near_end = EvaluatorGrant {
        effective_epoch: u64::MAX - 1,
        expiry_epoch_exclusive: u64::MAX,
        ..grant()
    };
    ok(near_end.validate());
    let mut out = [0xbe; 160];
    let before = out;
    assert_eq!(feature::encode_grant(&grant(), &mut out), Err(CAPACITY));
    assert_eq!(out, before);
}

fn consent() -> EvaluatorAdmissionConsentV1 {
    let owner = ok(PrincipalId::new([33; 32]));
    let market = binding().frozen.market;
    let nonce = [12; 32];
    EvaluatorAdmissionConsentV1 {
        chain: binding().frozen.chain,
        program: binding().frozen.program,
        market,
        evaluator: ok(common::derive_evaluator(market, owner, nonce)),
        owner,
        delegate_key: key(),
        enrollment_nonce: nonce,
        rubric: grant().rubric,
        approval: ok(Digest32::new([13; 32])),
        request: ok(RequestId::new([14; 32])),
        grant_version: v(9),
        key_version: v(10),
        effective_epoch: 5,
        config_version: v(7),
        expiry_height: 100,
    }
}
fn consent_context() -> ConsentContext {
    let value = consent();
    ConsentContext {
        expected: value,
        nomination: EvaluatorGrant {
            evaluator: value.evaluator,
            status: GrantStatus::Pending,
            ..grant()
        },
        executing_height: 99,
    }
}
fn expected_consent() -> Vec<u8> {
    let mut enrollment = vec![3; 32];
    enrollment.extend_from_slice(&[33; 32]);
    enrollment.extend_from_slice(&[12; 32]);
    let identity = hash(b"PAXAI/evaluator/v1", &enrollment);
    let mut out = vec![0, 1];
    out.extend_from_slice(&[55; 32]);
    out.extend_from_slice(&[2; 32]);
    out.extend_from_slice(&[3; 32]);
    out.extend_from_slice(&identity);
    out.extend_from_slice(&[33; 32]);
    out.extend_from_slice(&signing_key().verifying_key().to_bytes());
    out.extend_from_slice(&[12; 32]);
    out.extend_from_slice(&[8; 32]);
    out.extend_from_slice(&[13; 32]);
    out.extend_from_slice(&[14; 32]);
    for value in [9_u64, 10, 5, 7, 100] {
        out.extend_from_slice(&value.to_be_bytes());
    }
    out
}
#[test]
fn nomination_derivation_remains_pending_and_purpose_framed() {
    let value = consent();
    let nominated = ok(EvaluatorGrant::nominate(
        value.market,
        value.owner,
        value.enrollment_nonce,
        value.rubric,
        v(9),
        v(10),
        key(),
        5,
        7,
    ));
    assert_eq!(nominated.status, GrantStatus::Pending);
    assert_eq!(nominated.evaluator, value.evaluator);
    let expected = expected_consent();
    assert_eq!(value.evaluator.as_bytes(), &expected[98..130]);
    assert_ne!(
        value.evaluator.bytes(),
        ok(common::derive_worker(
            value.market,
            value.owner,
            value.enrollment_nonce
        ))
        .bytes()
    );
    assert_ne!(
        value.evaluator,
        ok(common::derive_evaluator(
            value.market,
            value.owner,
            [15; 32]
        ))
    );
    // Freshness/collisions are authoritative registry duties; derivation invents no retry nonce.
    assert_eq!(
        common::refuse_identity_collision(value.evaluator.as_bytes(), &[value.evaluator.bytes()]),
        Err(CONFLICT)
    );
}
#[test]
fn consent_exact_bytes_real_pop_and_every_truncation_or_trailing() {
    let value = consent();
    let ctx = consent_context();
    let expected = expected_consent();
    assert_eq!(expected.len(), 362);
    let digest = hash(b"PAXAI/evaluator-admission-consent/v1", &expected);
    assert_eq!(ok(feature::consent_digest(&value)).bytes(), digest);
    let statement = SignedEvaluatorConsent {
        consent: value,
        signature: Signature64(signing_key().sign(&digest).to_bytes()),
    };
    let mut out = [0; 426];
    assert_eq!(
        ok(feature::encode_signed_consent(&statement, &mut out)),
        426
    );
    let mut expected_signed = expected.clone();
    expected_signed.extend_from_slice(&signing_key().sign(&digest).to_bytes());
    assert_eq!(out.as_slice(), expected_signed);
    assert_eq!(ok(feature::decode_consent(&expected)), value);
    assert_eq!(ok(feature::decode_signed_consent(&out)), statement);
    assert_eq!(
        ok(feature::verify_signed_consent(&statement, &ctx)).bytes(),
        digest
    );
    assert_eq!(ctx.nomination.status, GrantStatus::Pending);
    for end in 0..362 {
        assert!(feature::decode_consent(&expected[..end]).is_err());
    }
    for end in 0..426 {
        assert!(feature::decode_signed_consent(&out[..end]).is_err());
    }
    let mut trailing = expected;
    trailing.push(0);
    assert_eq!(feature::decode_consent(&trailing), Err(NON_CANONICAL));
    let mut trailing = out.to_vec();
    trailing.push(0);
    assert_eq!(
        feature::decode_signed_consent(&trailing),
        Err(NON_CANONICAL)
    );
    for offset in 0..64 {
        let mut changed = statement;
        changed.signature.0[offset] ^= 1;
        assert_eq!(
            feature::verify_signed_consent(&changed, &ctx),
            Err(application(BAD_SIGNATURE))
        );
    }
    let mut wrong_frame = statement;
    wrong_frame.signature = Signature64(
        signing_key()
            .sign(&hash(b"PAXAI/score-attestation/v1", &digest))
            .to_bytes(),
    );
    assert_eq!(
        feature::verify_signed_consent(&wrong_frame, &ctx),
        Err(application(BAD_SIGNATURE))
    );
    let mut out = [0xa5; 425];
    let before = out;
    assert_eq!(
        feature::encode_signed_consent(&statement, &mut out),
        Err(CAPACITY)
    );
    assert_eq!(out, before);
    let mut out = [0xa5; 361];
    let before = out;
    assert_eq!(feature::encode_consent(&value, &mut out), Err(CAPACITY));
    assert_eq!(out, before);
}
#[test]
fn consent_mandatory_values_schema_expiry_and_all_permit_fields_bind() {
    let value = consent();
    let ctx = consent_context();
    let bytes = expected_consent();
    for version in [0_u16, 2, 65535] {
        let mut changed = bytes.clone();
        changed[..2].copy_from_slice(&version.to_be_bytes());
        assert_eq!(feature::decode_consent(&changed), Err(BAD_VERSION));
    }
    for (start, length) in [
        (2, 32),
        (34, 32),
        (66, 32),
        (98, 32),
        (130, 32),
        (162, 32),
        (226, 32),
        (258, 32),
        (290, 32),
        (322, 8),
        (330, 8),
        (346, 8),
        (354, 8),
    ] {
        let mut changed = bytes.clone();
        changed[start..start + length].fill(0);
        assert_eq!(
            feature::decode_consent(&changed),
            Err(NON_CANONICAL),
            "zero at {start}"
        );
    }
    // Nonce is a public fixed byte string; zero is not expressly reserved.
    let mut zero_nonce = value;
    zero_nonce.enrollment_nonce = [0; 32];
    zero_nonce.evaluator = ok(common::derive_evaluator(value.market, value.owner, [0; 32]));
    ok(zero_nonce.validate());
    for height in [100_u64, 101, u64::MAX] {
        let changed = ConsentContext {
            executing_height: height,
            ..ctx
        };
        assert_eq!(
            feature::check_consent_context(&value, &changed),
            Err(EXPIRED)
        );
    }
    let mut ctx_changed = ctx;
    ctx_changed.nomination.status = GrantStatus::Active;
    assert_eq!(
        feature::check_consent_context(&value, &ctx_changed),
        Err(F08_BAD_CONSENT)
    );
    ctx_changed.nomination.status = GrantStatus::Revoked;
    assert_eq!(
        feature::check_consent_context(&value, &ctx_changed),
        Err(REVOKED)
    );
    ctx_changed.nomination.status = GrantStatus::Expired;
    assert_eq!(
        feature::check_consent_context(&value, &ctx_changed),
        Err(EXPIRED)
    );
    for field in 0..15 {
        let mut changed = value;
        match field {
            0 => changed.chain = ok(ChainDomain::new([56; 32])),
            1 => changed.program = ok(ProgramId::new([6; 32])),
            2 => {
                changed.market = ok(MarketId::new([6; 32]));
                changed.evaluator = ok(common::derive_evaluator(
                    changed.market,
                    changed.owner,
                    changed.enrollment_nonce,
                ));
            }
            3 => changed.owner = ok(PrincipalId::new([6; 32])),
            4 => {
                changed.delegate_key = PublicKey32(
                    SigningKey::from_bytes(&[0x42; 32])
                        .verifying_key()
                        .to_bytes(),
                )
            }
            5 => changed.enrollment_nonce = [15; 32],
            6 => changed.rubric = ok(RubricDigest::new([6; 32])),
            7 => changed.approval = ok(Digest32::new([6; 32])),
            8 => changed.request = ok(RequestId::new([6; 32])),
            9 => changed.grant_version = v(11),
            10 => changed.key_version = v(11),
            11 => changed.effective_epoch = 6,
            12 => changed.config_version = v(8),
            13 => changed.expiry_height = 101,
            _ => changed.evaluator = ok(EvaluatorId::new([6; 32])),
        }
        let expected = if field <= 2 {
            WRONG_DOMAIN
        } else if field == 12 {
            WRONG_CONFIG
        } else {
            F08_BAD_CONSENT
        };
        assert_eq!(
            feature::check_consent_context(&changed, &ctx),
            Err(expected),
            "field {field}"
        );
        // Changing expected permit too never hides a stale nomination.
        if matches!(field, 4 | 6 | 9 | 10 | 11) {
            let ctx_changed = ConsentContext {
                expected: changed,
                ..ctx
            };
            assert_eq!(
                feature::check_consent_context(&changed, &ctx_changed),
                Err(F08_BAD_CONSENT)
            );
        }
    }
    // Validly encoded alteration still invalidates the original real signature.
    let digest = ok(feature::consent_digest(&value));
    let mut changed = value;
    changed.request = ok(RequestId::new([6; 32]));
    let ctx_changed = ConsentContext {
        expected: changed,
        ..ctx
    };
    let statement = SignedEvaluatorConsent {
        consent: changed,
        signature: Signature64(signing_key().sign(&digest.bytes()).to_bytes()),
    };
    assert_eq!(
        feature::verify_signed_consent(&statement, &ctx_changed),
        Err(application(BAD_SIGNATURE))
    );
}

#[test]
fn common_reveal_event_exact_228_bytes_and_structural_refusals() {
    let event = common::RevealScoreEvent {
        common: common::EventCommon {
            market: binding().frozen.market,
            epoch: 5,
            config: v(7),
            revision: 21,
            request: ok(RequestDigest::new([14; 32])),
            result: ok(ResultDigest::new([15; 32])),
        },
        evaluator: binding().evaluator,
        report: ok(ReportDigest::new([16; 32])),
        evidence: ok(EvidenceRoot::new([44; 32])),
        vector_count: 2,
        admitted_height: 80,
    };
    let mut expected = vec![0, 1];
    expected.extend_from_slice(&[3; 32]);
    for value in [5_u64, 7, 21] {
        expected.extend_from_slice(&value.to_be_bytes());
    }
    for value in [14, 15, 33, 16, 44] {
        expected.extend_from_slice(&[value; 32]);
    }
    expected.extend_from_slice(&2_u16.to_be_bytes());
    expected.extend_from_slice(&80_u64.to_be_bytes());
    let mut bytes = [0; 228];
    assert_eq!(ok(feature::encode_reveal_event(&event, &mut bytes)), 228);
    assert_eq!(bytes.as_slice(), expected);
    assert_eq!(ok(feature::decode_reveal_event(&bytes)), event);
    for end in 0..228 {
        assert!(feature::decode_reveal_event(&bytes[..end]).is_err());
    }
    let mut trailing = bytes.to_vec();
    trailing.push(0);
    assert!(feature::decode_reveal_event(&trailing).is_err());
    for count in [0_u16, 33, 65535] {
        let mut changed = bytes;
        changed[218..220].copy_from_slice(&count.to_be_bytes());
        assert_eq!(feature::decode_reveal_event(&changed), Err(NON_CANONICAL));
    }
    for (start, length) in [
        (2, 32),
        (42, 8),
        (58, 32),
        (90, 32),
        (122, 32),
        (154, 32),
        (186, 32),
    ] {
        let mut changed = bytes;
        changed[start..start + length].fill(0);
        assert!(feature::decode_reveal_event(&changed).is_err());
    }
    let mut changed = bytes;
    changed[..2].copy_from_slice(&2_u16.to_be_bytes());
    assert_eq!(feature::decode_reveal_event(&changed), Err(BAD_VERSION));
}

#[test]
fn core_response_status_and_numeric_application_error_contract() {
    let request = ok(RequestDigest::new([14; 32]));
    for error in [
        F03_NO_GRANT,
        F03_NO_SCORES,
        F03_SCORE_RANGE,
        F03_NONCANONICAL_VECTOR,
        WRONG_DOMAIN,
        BAD_SIGNATURE,
        F03_EVIDENCE_NOT_SEALED,
        F03_EVIDENCE_ROOT_MISMATCH,
    ] {
        let value = ok(common::ApplicationResult::failure(
            error,
            Presence::Present(request),
            21,
        ));
        let mut bytes = [0; 82];
        assert_eq!(ok(common::encode_result(&value, &mut bytes)), 82);
        assert_eq!(
            &bytes[..6],
            &[
                0,
                1,
                0,
                2,
                error.code().to_be_bytes()[0],
                error.code().to_be_bytes()[1]
            ]
        );
        assert_eq!(ok(common::decode_result(&bytes)).error, Some(error));
        assert_eq!(&bytes[78..82], &[0; 4]);
        let mut preimage = error.code().to_be_bytes().to_vec();
        preimage.extend_from_slice(&[14; 32]);
        assert_eq!(&bytes[46..78], &hash(b"PAXAI/error/v1", &preimage));
        for end in 0..82 {
            assert!(common::decode_result(&bytes[..end]).is_err());
        }
        let mut zero_error = bytes;
        zero_error[4..6].fill(0);
        assert!(common::decode_result(&zero_error).is_err());
        let mut unknown_error = bytes;
        unknown_error[4..6].copy_from_slice(&0xffff_u16.to_be_bytes());
        assert!(common::decode_result(&unknown_error).is_err());
    }
    assert_eq!(F03_NONCANONICAL_VECTOR.code(), 0x030c);
    assert_eq!(F03_SCORE_RANGE.code(), 0x030b);
    for status in [
        common::ResultStatus::Ok,
        common::ResultStatus::AlreadyApplied,
    ] {
        let value = ok(common::ApplicationResult::success(status, request, 21, &[]));
        let mut bytes = [0; 82];
        ok(common::encode_result(&value, &mut bytes));
        assert_eq!(&bytes[4..6], &[0, 0]);
        assert_eq!(ok(common::decode_result(&bytes)).status, status);
        let mut changed = bytes;
        changed[4..6].copy_from_slice(&F03_NO_GRANT.code().to_be_bytes());
        assert!(common::decode_result(&changed).is_err());
        for unknown in [3_u16, 65535] {
            let mut changed = bytes;
            changed[2..4].copy_from_slice(&unknown.to_be_bytes());
            assert!(common::decode_result(&changed).is_err());
        }
    }
    for request in [Presence::Absent, Presence::Present(request)] {
        let value = ok(common::ApplicationResult::failure(
            UNAUTHORIZED,
            request,
            21,
        ));
        assert_eq!(value.revision, 0);
    }
}
