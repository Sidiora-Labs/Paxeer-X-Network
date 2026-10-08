//! Allocation-free evaluator codecs and pure cryptographic statement checks.
//! No state, event emission, grant activation, salt or public admission bypass.
use super::model::*;
pub use crate::codec::{decode_reveal_event, encode_reveal_event, RevealScoreEvent};
use crate::{
    codec::{self as common, Reader, ReportBody, ScoreVector, Writer},
    errors::*,
    types::*,
};

fn room(out: &[u8], size: usize) -> CodecResult<()> {
    if out.len() < size {
        Err(CAPACITY)
    } else {
        Ok(())
    }
}
fn schema(r: &mut Reader<'_>) -> CodecResult<()> {
    if r.u16()? != crate::SCHEMA_VERSION {
        Err(BAD_VERSION)
    } else {
        Ok(())
    }
}
pub fn encode_grant(value: &EvaluatorGrant, out: &mut [u8]) -> CodecResult<usize> {
    value.validate()?;
    room(out, GRANT_BYTES)?;
    let mut w = Writer::new(out);
    w.put(value.evaluator.as_bytes())?;
    w.put(value.principal.as_bytes())?;
    w.put(value.rubric.as_bytes())?;
    w.u64(value.grant_version.get())?;
    w.u64(value.key_version.get())?;
    w.put(&value.signing_key.0)?;
    w.u64(value.effective_epoch)?;
    w.u64(value.expiry_epoch_exclusive)?;
    w.u8(value.status as u8)?;
    Ok(w.len())
}
pub fn decode_grant(input: &[u8]) -> CodecResult<EvaluatorGrant> {
    if input.len() != GRANT_BYTES {
        return Err(NON_CANONICAL);
    }
    let mut r = Reader::new(input);
    let value = EvaluatorGrant {
        evaluator: EvaluatorId::new(r.fixed()?)?,
        principal: PrincipalId::new(r.fixed()?)?,
        rubric: RubricDigest::new(r.fixed()?)?,
        grant_version: Version::new(r.u64()?)?,
        key_version: Version::new(r.u64()?)?,
        signing_key: PublicKey32(r.fixed()?),
        effective_epoch: r.u64()?,
        expiry_epoch_exclusive: r.u64()?,
        status: GrantStatus::decode(r.u8()?)?,
    };
    r.finish()?;
    value.validate()?;
    Ok(value)
}

/// F03 narrows common structural vector errors to its frozen feature codes.
/// Does not sort, repair or mutate caller input.
fn validate_scores(scores: &ScoreVector<'_>) -> CodecResult<()> {
    if let ScoreVector::Encoded(bytes) = scores {
        if bytes.len() % 36 != 0 {
            return Err(NON_CANONICAL);
        }
    }
    if scores.is_empty() {
        return Err(F03_NO_SCORES);
    }
    if scores.len() > crate::MAX_WORKERS {
        return Err(NON_CANONICAL);
    }
    let mut previous = None;
    for value in scores.entries() {
        let entry = value?;
        if previous.is_some_and(|worker| worker >= entry.worker) {
            return Err(F03_NONCANONICAL_VECTOR);
        }
        previous = Some(entry.worker);
    }
    Ok(())
}
pub fn encode_report_body(value: &ReportBody<'_>, out: &mut [u8]) -> CodecResult<usize> {
    validate_scores(&value.scores)?;
    common::encode_report(value, out)
}
pub fn decode_report_body(input: &[u8]) -> CodecResult<ReportBody<'_>> {
    // Validate prefix/types through the common codec, and vector through F03.
    // The preflight is bounded and uses only checked lengths before any slice.
    if input.len() < common::REPORT_FIXED_BYTES || input.len() > common::REPORT_MAX_BYTES {
        return Err(NON_CANONICAL);
    }
    let mut r = Reader::new(input);
    schema(&mut r)?;
    // Common owns all prefix models; F03 preflights the exact vector suffix.
    r.take(224)?;
    let count = usize::from(r.u16()?);
    if count == 0 {
        return Err(F03_NO_SCORES);
    }
    if count > crate::MAX_WORKERS {
        return Err(NON_CANONICAL);
    }
    let size = common::REPORT_FIXED_BYTES
        .checked_add(count.checked_mul(36).ok_or(ARITHMETIC)?)
        .ok_or(ARITHMETIC)?;
    if input.len() != size {
        return Err(NON_CANONICAL);
    }
    let scores = ScoreVector::Encoded(r.take(count.checked_mul(36).ok_or(ARITHMETIC)?)?);
    r.finish()?;
    validate_scores(&scores)?;
    common::decode_report(input)
}
pub fn encode_signed_report(value: &SignedReport<'_>, out: &mut [u8]) -> CodecResult<usize> {
    validate_scores(&value.body.scores)?;
    let n = common::REPORT_FIXED_BYTES
        .checked_add(value.body.scores.len().checked_mul(36).ok_or(ARITHMETIC)?)
        .and_then(|n| n.checked_add(64))
        .ok_or(ARITHMETIC)?;
    room(out, n)?;
    let body_len = encode_report_body(&value.body, out)?;
    out[body_len..n].copy_from_slice(&value.signature.0);
    Ok(n)
}
pub fn decode_signed_report(input: &[u8]) -> CodecResult<SignedReport<'_>> {
    if input.len() < common::REPORT_FIXED_BYTES + 64 || input.len() > SIGNED_REPORT_MAX_BYTES {
        return Err(NON_CANONICAL);
    }
    let end = input.len().checked_sub(64).ok_or(ARITHMETIC)?;
    let body = decode_report_body(&input[..end])?;
    let signature = Signature64(input[end..].try_into().map_err(|_| NON_CANONICAL)?);
    Ok(SignedReport { body, signature })
}

/// Domain comparisons precede expensive crypto. All report deployment domain
/// mismatches use WRONG_DOMAIN per F03 A05 (including program and market).
pub fn check_binding(actual: &EvaluatorBinding, expected: &EvaluatorBinding) -> CodecResult<()> {
    let a = actual.frozen;
    let e = expected.frozen;
    if a.chain != e.chain || a.program != e.program || a.market != e.market {
        return Err(WRONG_DOMAIN);
    }
    if a.epoch != e.epoch {
        return Err(WRONG_EPOCH);
    }
    if a.config != e.config {
        return Err(WRONG_CONFIG);
    }
    if a.roster != e.roster {
        return Err(WRONG_ROSTER);
    }
    if actual.evaluator != expected.evaluator {
        return Err(F03_NO_GRANT);
    }
    if actual.grant != expected.grant {
        return Err(F03_GRANT_VERSION_CONFLICT);
    }
    if actual.key_version != expected.key_version {
        return Err(F03_KEY_VERSION_CONFLICT);
    }
    Ok(())
}
pub fn check_report_context(body: &ReportBody<'_>, context: &ReportContext<'_>) -> CodecResult<()> {
    validate_scores(&body.scores)?;
    check_binding(&body.binding, &context.binding)?;
    let frozen = &context.frozen_grant;
    let live = &context.live_grant;
    frozen.validate()?;
    live.validate()?;
    for grant in [frozen, live] {
        if grant.evaluator != context.binding.evaluator {
            return Err(F03_NO_GRANT);
        }
        if grant.grant_version != context.binding.grant {
            return Err(F03_GRANT_VERSION_CONFLICT);
        }
        if grant.status == GrantStatus::Revoked {
            return Err(REVOKED);
        }
    }
    let epoch = context.binding.frozen.epoch;
    for grant in [frozen, live] {
        if grant.status == GrantStatus::Expired || epoch >= grant.expiry_epoch_exclusive {
            return Err(EXPIRED);
        }
        if grant.status != GrantStatus::Active || epoch < grant.effective_epoch {
            return Err(F03_NO_GRANT);
        }
        if grant.key_version != context.binding.key_version {
            return Err(F03_KEY_VERSION_CONFLICT);
        }
    }
    if live.signing_key != frozen.signing_key {
        return Err(KEY_MISMATCH);
    }
    if live.principal != frozen.principal {
        return Err(ROLE_CONFLICT);
    }
    if frozen.rubric != context.approved_rubric || live.rubric != context.approved_rubric {
        return Err(WRONG_CONFIG);
    }
    if context.workers.len() > crate::MAX_WORKERS {
        return Err(CAPACITY);
    }
    if frozen.principal == context.market_owner {
        return Err(ROLE_CONFLICT);
    }
    let mut previous = None;
    for worker in context.workers {
        nonzero_key(worker.public_key)?;
        if previous.is_some_and(|id| id >= worker.worker) {
            return Err(WRONG_ROSTER);
        }
        previous = Some(worker.worker);
        if worker.owner == frozen.principal {
            return Err(ROLE_CONFLICT);
        }
    }
    let evidence = match context.evidence {
        Presence::Absent => return Err(F03_EVIDENCE_NOT_SEALED),
        Presence::Present(value) => value,
    };
    if evidence.binding != context.binding || evidence.rubric != context.approved_rubric {
        return Err(EVIDENCE_BINDING);
    }
    if evidence.root != body.evidence {
        return Err(F03_EVIDENCE_ROOT_MISMATCH);
    }
    for value in body.scores.entries() {
        let entry = value?;
        if !context
            .workers
            .iter()
            .any(|worker| worker.worker == entry.worker)
        {
            return Err(F03_UNKNOWN_WORKER);
        }
    }
    Ok(())
}

/// Ordinary Ed25519 over exactly 32 bytes; native is real dalek, wasm is the
/// actual SDK import. Host metering/capability failures are never BAD_SIGNATURE.
pub fn verify_digest(
    key: PublicKey32,
    signature: Signature64,
    digest: [u8; 32],
) -> Result<(), VerificationError> {
    nonzero_key(key)?;
    #[cfg(target_arch = "wasm32")]
    {
        use layerx_program_sdk::crypto::{ed25519_verify, Ed25519Message};
        let message = Ed25519Message::new(&digest).map_err(VerificationError::Host)?;
        match ed25519_verify(message, &key.0, &signature.0) {
            Ok(()) => Ok(()),
            Err(error) if error.code() == -6 => Err(BAD_SIGNATURE.into()),
            Err(error) => Err(VerificationError::Host(error)),
        }
    }
    #[cfg(not(target_arch = "wasm32"))]
    {
        let key = ed25519_dalek::VerifyingKey::from_bytes(&key.0)
            .map_err(|_| VerificationError::Application(BAD_SIGNATURE))?;
        key.verify_strict(&digest, &ed25519_dalek::Signature::from_bytes(&signature.0))
            .map_err(|_| VerificationError::Application(BAD_SIGNATURE))
    }
}
pub fn verify_signed_report(
    value: &SignedReport<'_>,
    context: &ReportContext<'_>,
) -> Result<ReportDigest, VerificationError> {
    check_report_context(&value.body, context)?;
    let digest = common::report_digest(&value.body)?;
    let attestation = common::attestation_digest(digest)?;
    verify_digest(
        context.frozen_grant.signing_key,
        value.signature,
        attestation.bytes(),
    )?;
    Ok(digest)
}

pub fn encode_consent(value: &EvaluatorAdmissionConsentV1, out: &mut [u8]) -> CodecResult<usize> {
    value.validate()?;
    room(out, CONSENT_BYTES)?;
    let mut w = Writer::new(out);
    w.u16(crate::SCHEMA_VERSION)?;
    w.put(value.chain.as_bytes())?;
    w.put(value.program.as_bytes())?;
    w.put(value.market.as_bytes())?;
    w.put(value.evaluator.as_bytes())?;
    w.put(value.owner.as_bytes())?;
    w.put(&value.delegate_key.0)?;
    w.put(&value.enrollment_nonce)?;
    w.put(value.rubric.as_bytes())?;
    w.put(value.approval.as_bytes())?;
    w.put(value.request.as_bytes())?;
    w.u64(value.grant_version.get())?;
    w.u64(value.key_version.get())?;
    w.u64(value.effective_epoch)?;
    w.u64(value.config_version.get())?;
    w.u64(value.expiry_height)?;
    Ok(w.len())
}
pub fn decode_consent(input: &[u8]) -> CodecResult<EvaluatorAdmissionConsentV1> {
    if input.len() != CONSENT_BYTES {
        return Err(NON_CANONICAL);
    }
    let mut r = Reader::new(input);
    schema(&mut r)?;
    let value = EvaluatorAdmissionConsentV1 {
        chain: ChainDomain::new(r.fixed()?)?,
        program: ProgramId::new(r.fixed()?)?,
        market: MarketId::new(r.fixed()?)?,
        evaluator: EvaluatorId::new(r.fixed()?)?,
        owner: PrincipalId::new(r.fixed()?)?,
        delegate_key: PublicKey32(r.fixed()?),
        enrollment_nonce: r.fixed()?,
        rubric: RubricDigest::new(r.fixed()?)?,
        approval: Digest32::new(r.fixed()?)?,
        request: RequestId::new(r.fixed()?)?,
        grant_version: Version::new(r.u64()?)?,
        key_version: Version::new(r.u64()?)?,
        effective_epoch: r.u64()?,
        config_version: Version::new(r.u64()?)?,
        expiry_height: r.u64()?,
    };
    r.finish()?;
    value.validate()?;
    Ok(value)
}
pub fn consent_digest(value: &EvaluatorAdmissionConsentV1) -> CodecResult<Digest32> {
    let mut bytes = [0; CONSENT_BYTES];
    encode_consent(value, &mut bytes)?;
    common::domain_hash("PAXAI/evaluator-admission-consent/v1", &bytes)
}
pub fn encode_signed_consent(value: &SignedEvaluatorConsent, out: &mut [u8]) -> CodecResult<usize> {
    value.consent.validate()?;
    room(out, SIGNED_CONSENT_BYTES)?;
    encode_consent(&value.consent, out)?;
    out[CONSENT_BYTES..SIGNED_CONSENT_BYTES].copy_from_slice(&value.signature.0);
    Ok(SIGNED_CONSENT_BYTES)
}
pub fn decode_signed_consent(input: &[u8]) -> CodecResult<SignedEvaluatorConsent> {
    if input.len() != SIGNED_CONSENT_BYTES {
        return Err(NON_CANONICAL);
    }
    Ok(SignedEvaluatorConsent {
        consent: decode_consent(&input[..CONSENT_BYTES])?,
        signature: Signature64(
            input[CONSENT_BYTES..]
                .try_into()
                .map_err(|_| NON_CANONICAL)?,
        ),
    })
}
pub fn check_consent_context(
    value: &EvaluatorAdmissionConsentV1,
    context: &ConsentContext,
) -> CodecResult<()> {
    value.validate()?;
    context.expected.validate()?;
    context.nomination.validate()?;
    let expected = &context.expected;
    let grant = &context.nomination;
    if value.chain != expected.chain
        || value.program != expected.program
        || value.market != expected.market
    {
        return Err(WRONG_DOMAIN);
    }
    if value.config_version != expected.config_version {
        return Err(WRONG_CONFIG);
    }
    if grant.status == GrantStatus::Revoked {
        return Err(REVOKED);
    }
    if grant.status == GrantStatus::Expired {
        return Err(EXPIRED);
    }
    if grant.status != GrantStatus::Pending {
        return Err(F08_BAD_CONSENT);
    }
    if context.executing_height >= value.expiry_height {
        return Err(EXPIRED);
    }
    if value != expected
        || value.evaluator != grant.evaluator
        || value.owner != grant.principal
        || value.delegate_key != grant.signing_key
        || value.rubric != grant.rubric
        || value.grant_version != grant.grant_version
        || value.key_version != grant.key_version
        || value.effective_epoch != grant.effective_epoch
    {
        return Err(F08_BAD_CONSENT);
    }
    Ok(())
}
/// Delegate proof of possession only. F08 kind0 owner acceptance, registered
/// permit authority and atomic membership/replay updates remain F08's duties.
pub fn verify_signed_consent(
    value: &SignedEvaluatorConsent,
    context: &ConsentContext,
) -> Result<Digest32, VerificationError> {
    check_consent_context(&value.consent, context)?;
    let digest = consent_digest(&value.consent)?;
    verify_digest(value.consent.delegate_key, value.signature, digest.bytes())?;
    Ok(digest)
}
