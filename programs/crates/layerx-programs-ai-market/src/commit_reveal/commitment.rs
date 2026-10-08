#[cfg(target_arch = "wasm32")]
use crate::types::AttestationDigest;
use crate::{
    codec,
    errors::{
        ApplicationError, CodecResult, ARITHMETIC, CAPACITY, F03_NO_SCORES, F03_UNKNOWN_WORKER,
        F04_COMMIT_MISMATCH, F04_NO_SCORES, KEY_MISMATCH, NON_CANONICAL, UNAUTHORIZED,
        WRONG_CONFIG, WRONG_DOMAIN, WRONG_EPOCH, WRONG_MARKET, WRONG_PROGRAM, WRONG_ROSTER,
    },
    types::{
        ChainDomain, CommitmentDigest, EvaluatorBinding, EvaluatorId, FrozenBinding, MarketId,
        ProgramId, ReportDigest, RosterDigest, Salt32, Signature64, Version, WorkerId,
    },
    MAX_WORKERS,
};

pub const BINDING_BYTES: usize = 192;
pub const COMMIT_SCORE_BYTES: usize = 224;
pub const COMMITMENT_PREIMAGE_BYTES: usize = 278;
pub const ATTESTATION_PREIMAGE_BYTES: usize = 59;
pub const REPORT_PREIMAGE_MAX_BYTES: usize = 1402;
pub const REVEAL_MAX_BYTES: usize = 1480;
pub const REPORT_DOMAIN: &[u8; 22] = b"PAXAI/score-report/v1\0";
pub const ATTESTATION_DOMAIN: &[u8; 27] = b"PAXAI/score-attestation/v1\0";
pub const COMMITMENT_DOMAIN: &[u8; 22] = b"PAXAI/score-commit/v1\0";

fn report_error(error: ApplicationError) -> ApplicationError {
    if error == F03_NO_SCORES {
        F04_NO_SCORES
    } else {
        error
    }
}

/// Encodes the 192-byte evaluator binding.
///
/// # Errors
/// Propagates `Writer` `ARITHMETIC`/`CAPACITY` refusals (unreachable for the fixed buffer).
pub fn encode_binding(binding: &EvaluatorBinding) -> CodecResult<[u8; BINDING_BYTES]> {
    let mut bytes = [0; BINDING_BYTES];
    let mut w = codec::Writer::new(&mut bytes);
    w.put(binding.frozen.chain.as_bytes())?;
    w.put(binding.frozen.program.as_bytes())?;
    w.put(binding.frozen.market.as_bytes())?;
    w.u64(binding.frozen.epoch)?;
    w.u64(binding.frozen.config.get())?;
    w.put(binding.frozen.roster.as_bytes())?;
    w.put(binding.evaluator.as_bytes())?;
    w.u64(binding.grant.get())?;
    w.u64(binding.key_version.get())?;
    Ok(bytes)
}

/// Decodes the 192-byte evaluator binding.
///
/// # Errors
/// Returns `NON_CANONICAL` when the input is not exactly `BINDING_BYTES` or for a zero identity, digest or version.
pub fn decode_binding(bytes: &[u8]) -> CodecResult<EvaluatorBinding> {
    if bytes.len() != BINDING_BYTES {
        return Err(NON_CANONICAL);
    }
    let mut r = codec::Reader::new(bytes);
    let binding = EvaluatorBinding {
        frozen: FrozenBinding {
            chain: ChainDomain::new(r.fixed()?)?,
            program: ProgramId::new(r.fixed()?)?,
            market: MarketId::new(r.fixed()?)?,
            epoch: r.u64()?,
            config: Version::new(r.u64()?)?,
            roster: RosterDigest::new(r.fixed()?)?,
        },
        evaluator: EvaluatorId::new(r.fixed()?)?,
        grant: Version::new(r.u64()?)?,
        key_version: Version::new(r.u64()?)?,
    };
    r.finish()?;
    Ok(binding)
}

/// Compares a binding with the expected one, field by field.
///
/// # Errors
/// Returns `WRONG_DOMAIN`, `WRONG_PROGRAM`, `WRONG_MARKET`, `WRONG_EPOCH`, `WRONG_CONFIG`, `WRONG_ROSTER`, `UNAUTHORIZED` (evaluator or grant) or `KEY_MISMATCH` for the first mismatched field, in that order.
pub fn check_binding(actual: &EvaluatorBinding, expected: &EvaluatorBinding) -> CodecResult<()> {
    let a = &actual.frozen;
    let e = &expected.frozen;
    if a.chain != e.chain {
        Err(WRONG_DOMAIN)
    } else if a.program != e.program {
        Err(WRONG_PROGRAM)
    } else if a.market != e.market {
        Err(WRONG_MARKET)
    } else if a.epoch != e.epoch {
        Err(WRONG_EPOCH)
    } else if a.config != e.config {
        Err(WRONG_CONFIG)
    } else if a.roster != e.roster {
        Err(WRONG_ROSTER)
    } else if actual.evaluator != expected.evaluator || actual.grant != expected.grant {
        Err(UNAUTHORIZED)
    } else if actual.key_version != expected.key_version {
        Err(KEY_MISMATCH)
    } else {
        Ok(())
    }
}

/// Decodes a 32-byte salt.
///
/// # Errors
/// Returns `NON_CANONICAL` when the input is not 32 bytes; `F04_SALT_INVALID` for a zero salt.
pub fn decode_salt(bytes: &[u8]) -> CodecResult<Salt32> {
    Salt32::new(bytes.try_into().map_err(|_| NON_CANONICAL)?)
}

/// Decodes a 64-byte signature.
///
/// # Errors
/// Returns `NON_CANONICAL` when the input is not 64 bytes.
pub fn decode_signature(bytes: &[u8]) -> CodecResult<Signature64> {
    Ok(Signature64(bytes.try_into().map_err(|_| NON_CANONICAL)?))
}

/// Decodes a report and checks its binding and worker set.
///
/// # Errors
/// Propagates `decode_report` refusals (`F03_NO_SCORES` mapped to `F04_NO_SCORES`) and `check_binding` refusals; returns `CAPACITY` for more than `MAX_WORKERS` workers; `NON_CANONICAL` for an unsorted worker list; `F03_UNKNOWN_WORKER` for a score outside it.
pub fn validate_report<'a>(
    body: &'a [u8],
    expected: &EvaluatorBinding,
    workers: &[WorkerId],
) -> CodecResult<codec::ReportBody<'a>> {
    let report = codec::decode_report(body).map_err(report_error)?;
    check_binding(&report.binding, expected)?;
    if workers.len() > MAX_WORKERS {
        return Err(CAPACITY);
    }
    if workers.windows(2).any(|pair| pair[0] >= pair[1]) {
        return Err(NON_CANONICAL);
    }
    for entry in report.scores.entries() {
        if !workers.contains(&entry?.worker) {
            return Err(F03_UNKNOWN_WORKER);
        }
    }
    Ok(report)
}

/// Writes the report-digest preimage (domain, then body) into `output`.
///
/// # Errors
/// Propagates `decode_report` refusals (`F03_NO_SCORES` mapped to `F04_NO_SCORES`); returns `ARITHMETIC` on length overflow; `CAPACITY` when the preimage exceeds `REPORT_PREIMAGE_MAX_BYTES` or `output`.
pub fn report_preimage(body: &[u8], output: &mut [u8]) -> CodecResult<usize> {
    codec::decode_report(body).map_err(report_error)?;
    let length = REPORT_DOMAIN
        .len()
        .checked_add(body.len())
        .ok_or(ARITHMETIC)?;
    if length > REPORT_PREIMAGE_MAX_BYTES || output.len() < length {
        return Err(CAPACITY);
    }
    output[..22].copy_from_slice(REPORT_DOMAIN);
    output[22..length].copy_from_slice(body);
    Ok(length)
}

#[must_use]
pub fn attestation_preimage(report: ReportDigest) -> [u8; ATTESTATION_PREIMAGE_BYTES] {
    let mut bytes = [0; ATTESTATION_PREIMAGE_BYTES];
    bytes[..27].copy_from_slice(ATTESTATION_DOMAIN);
    bytes[27..].copy_from_slice(report.as_bytes());
    bytes
}

/// Builds the commitment preimage: domain, then binding, report digest and salt.
///
/// # Errors
/// Propagates `codec::encode_commitment_preimage`'s `CAPACITY` refusal (unreachable for the fixed buffer).
pub fn commitment_preimage(
    binding: &EvaluatorBinding,
    report: ReportDigest,
    salt: Salt32,
) -> CodecResult<[u8; COMMITMENT_PREIMAGE_BYTES]> {
    let mut bytes = [0; COMMITMENT_PREIMAGE_BYTES];
    bytes[..22].copy_from_slice(COMMITMENT_DOMAIN);
    codec::encode_commitment_preimage(binding, report, salt, &mut bytes[22..])?;
    Ok(bytes)
}

/// Compares a revealed commitment with the stored one.
///
/// # Errors
/// Returns `F04_COMMIT_MISMATCH` when they differ.
pub fn check_commitment(actual: CommitmentDigest, expected: CommitmentDigest) -> CodecResult<()> {
    if actual == expected {
        Ok(())
    } else {
        Err(F04_COMMIT_MISMATCH)
    }
}

/// Encodes the 224-byte commit-score payload.
///
/// # Errors
/// Propagates `codec::encode_commit_score`'s `CAPACITY` refusal (unreachable for the fixed buffer).
pub fn encode_commit_score(
    binding: EvaluatorBinding,
    commitment: CommitmentDigest,
) -> CodecResult<[u8; COMMIT_SCORE_BYTES]> {
    let mut bytes = [0; COMMIT_SCORE_BYTES];
    codec::encode_commit_score(
        &codec::CommitScorePayload {
            binding,
            commitment,
        },
        &mut bytes,
    )?;
    Ok(bytes)
}

/// Decodes the 224-byte commit-score payload.
///
/// # Errors
/// Returns `NON_CANONICAL` when the input is not exactly `COMMIT_SCORE_BYTES`; propagates `codec::decode_commit_score` refusals.
pub fn decode_commit_score(bytes: &[u8]) -> CodecResult<codec::CommitScorePayload> {
    if bytes.len() != COMMIT_SCORE_BYTES {
        return Err(NON_CANONICAL);
    }
    codec::decode_commit_score(bytes)
}

/// Encodes a reveal-score payload from raw body, signature and salt.
///
/// # Errors
/// Propagates `decode_report`, `decode_signature`, `decode_salt` and `codec::encode_reveal_score` refusals, with `F03_NO_SCORES` mapped to `F04_NO_SCORES`.
pub fn encode_reveal_score(
    body: &[u8],
    signature: &[u8],
    salt: &[u8],
    output: &mut [u8],
) -> CodecResult<usize> {
    let report = codec::decode_report(body).map_err(report_error)?;
    let signature = decode_signature(signature)?;
    let salt = decode_salt(salt)?;
    codec::encode_reveal_score(
        &codec::RevealScorePayload {
            report,
            signature,
            salt,
        },
        output,
    )
    .map_err(report_error)
}

/// Decodes a reveal-score payload.
///
/// # Errors
/// Propagates `codec::decode_reveal_score` refusals, with `F03_NO_SCORES` mapped to `F04_NO_SCORES`.
pub fn decode_reveal_score(bytes: &[u8]) -> CodecResult<codec::RevealScorePayload<'_>> {
    codec::decode_reveal_score(bytes).map_err(report_error)
}

#[cfg(target_arch = "wasm32")]
#[derive(Debug)]
pub enum CryptoError {
    Codec(ApplicationError),
    Sdk(layerx_program_sdk::ProgramError),
}

#[cfg(target_arch = "wasm32")]
fn sdk_sha256(bytes: &[u8]) -> Result<[u8; 32], CryptoError> {
    use layerx_program_sdk::crypto::{hash, HashAlgorithm, HashInput};
    let input = HashInput::new(bytes).map_err(CryptoError::Sdk)?;
    hash(HashAlgorithm::Sha256, input).map_err(CryptoError::Sdk)
}

#[cfg(target_arch = "wasm32")]
pub fn report_digest(body: &[u8]) -> Result<ReportDigest, CryptoError> {
    let mut bytes = [0; REPORT_PREIMAGE_MAX_BYTES];
    let length = report_preimage(body, &mut bytes).map_err(CryptoError::Codec)?;
    ReportDigest::new(sdk_sha256(&bytes[..length])?).map_err(CryptoError::Codec)
}

#[cfg(target_arch = "wasm32")]
pub fn attestation_message(report: ReportDigest) -> Result<AttestationDigest, CryptoError> {
    AttestationDigest::new(sdk_sha256(&attestation_preimage(report))?).map_err(CryptoError::Codec)
}

#[cfg(target_arch = "wasm32")]
pub fn commitment_digest(
    binding: &EvaluatorBinding,
    report: ReportDigest,
    salt: Salt32,
) -> Result<CommitmentDigest, CryptoError> {
    let bytes = commitment_preimage(binding, report, salt).map_err(CryptoError::Codec)?;
    CommitmentDigest::new(sdk_sha256(&bytes)?).map_err(CryptoError::Codec)
}
