use layerx_agent_api::identity::NativeEffectPrepareRequestV1;
use serde_json::{json, Value};
use sha2::{Digest, Sha256};

use crate::agent_envelope::EnvelopeError;

pub const PREPARE_DOMAIN: &[u8] = b"LXP/agent/native-effect-prepare/v1\0";

pub fn encode_native_effect_prepare(
    request: &NativeEffectPrepareRequestV1,
) -> Result<Value, EnvelopeError> {
    request
        .clone()
        .validate()
        .map_err(|_| EnvelopeError::InvalidRequest)?;
    let signed = &request.purpose;
    let purpose = &signed.purpose;
    Ok(json!({
        "variant": "native_effect_v1",
        "activity": json!({"version":"1", "module":request.activity.module.to_string(), "ordinal":request.activity.ordinal.to_string()}),
        "actor": request.actor.as_str(), "authority": request.authority,
        "account_sequence": request.account_sequence.to_string(),
        "not_before": request.not_before.to_string(), "not_after": request.not_after.to_string(),
        "idempotency_key": hex(&request.idempotency_key), "fee_limit": request.fee_limit.to_string(),
        "payload": hex(&request.payload), "payload_hash": hex(&request.payload_hash),
        "capability_id": request.capability_id.as_str(),
        "purpose": {
            "purpose": {"version":"1", "tenant":purpose.tenant.as_str(),
                "agent_did":purpose.agent_did.as_str(), "session_id":purpose.session_id.as_str(),
                "generation":purpose.generation.to_string(), "expires_at_ms":purpose.expires_at_ms.to_string(),
                "capability_id":purpose.capability_id.as_str(), "preparation_id":hex(&purpose.preparation_id),
                "canonical_digest":hex(&purpose.canonical_digest), "commitment":hex(&purpose.commitment)},
            "owner_public_key":hex(&signed.owner_public_key), "signature":hex(&signed.signature)
        },
        "local_grant": request.local_grant.as_ref().map(|grant| json!({
            "version":"1", "capability":hex(&grant.capability), "session_scope":hex(&grant.session_scope),
            "expires_at_ms":grant.expires_at_ms.to_string(), "owner_public_key":hex(&grant.owner_public_key),
            "signature":hex(&grant.signature)
        }))
    }))
}

pub fn native_effect_prepare_digest(
    request: &NativeEffectPrepareRequestV1,
) -> Result<[u8; 32], EnvelopeError> {
    let canonical = encode_native_effect_prepare(request)?;
    let bytes = serde_json::to_vec(&canonical).map_err(|_| EnvelopeError::InvalidRequest)?;
    let mut digest = Sha256::new();
    digest.update(PREPARE_DOMAIN);
    digest.update(bytes);
    Ok(digest.finalize().into())
}

pub const SEND_PREPARE_DOMAIN: &[u8] = b"LXP/agent/native-send-prepare/v1\0";

pub fn encode_native_send_prepare(
    request: &layerx_agent_api::identity::NativeSendPrepareRequestV1,
) -> Result<Value, EnvelopeError> {
    request
        .clone()
        .validate()
        .map_err(|_| EnvelopeError::InvalidRequest)?;
    let signed = &request.purpose;
    let purpose = &signed.purpose;
    Ok(json!({
        "variant": "native_send_v1",
        "activity": json!({"version":"1", "module":request.activity.module.to_string(), "ordinal":request.activity.ordinal.to_string()}),
        "actor": request.actor.as_str(), "authority": request.authority,
        "account_sequence": request.account_sequence.to_string(),
        "not_before": request.not_before.to_string(), "not_after": request.not_after.to_string(),
        "idempotency_key": hex(&request.idempotency_key), "fee_limit": request.fee_limit.to_string(),
        "payload": hex(&request.payload), "payload_hash": hex(&request.payload_hash),
        "capability_id": request.capability_id.as_str(),
        "purpose": {
            "purpose": {"version":"1", "tenant":purpose.tenant.as_str(),
                "agent_did":purpose.agent_did.as_str(), "owner_did":purpose.owner_did.as_str(),
                "owner_public_key":hex(&purpose.owner_public_key), "session_id":purpose.session_id.as_str(),
                "generation":purpose.generation.to_string(), "expires_at_ms":purpose.expires_at_ms.to_string(),
                "capability_id":purpose.capability_id.as_str(), "preparation_id":hex(&purpose.preparation_id),
                "canonical_digest":hex(&purpose.canonical_digest),
                "protocol_version":purpose.protocol_version.to_string(), "network_id":purpose.network_id.to_string(),
                "activity":{"version":"1", "module":purpose.activity.module.to_string(), "ordinal":purpose.activity.ordinal.to_string()},
                "economic_action":hex(&purpose.economic_action), "idempotency_key":hex(&purpose.idempotency_key),
                "commitment":hex(&purpose.commitment)},
            "owner_public_key":hex(&signed.owner_public_key), "signature":hex(&signed.signature)
        },
        "local_grant": request.local_grant.as_ref().map(|grant| json!({
            "version":"1", "capability":hex(&grant.capability), "session_scope":hex(&grant.session_scope),
            "expires_at_ms":grant.expires_at_ms.to_string(), "owner_public_key":hex(&grant.owner_public_key),
            "signature":hex(&grant.signature)
        }))
    }))
}

pub fn native_send_prepare_digest(
    request: &layerx_agent_api::identity::NativeSendPrepareRequestV1,
) -> Result<[u8; 32], EnvelopeError> {
    let canonical = encode_native_send_prepare(request)?;
    let bytes = serde_json::to_vec(&canonical).map_err(|_| EnvelopeError::InvalidRequest)?;
    let mut digest = Sha256::new();
    digest.update(SEND_PREPARE_DOMAIN);
    digest.update(bytes);
    Ok(digest.finalize().into())
}

pub fn encode_native_send_submit(
    request: &layerx_agent_api::submit::SubmitRequest,
    signer_public_key: &[u8; 32],
) -> Result<Value, EnvelopeError> {
    let reference = request.preparation_ref.as_str();
    if reference.len() != 64
        || !reference
            .bytes()
            .all(|byte| matches!(byte, b'0'..=b'9' | b'a'..=b'f'))
        || reference.bytes().all(|byte| byte == b'0')
        || request.signature.as_bytes().len() != 64
    {
        return Err(EnvelopeError::InvalidRequest);
    }
    Ok(json!({
        "variant":"native_send_submit_v1",
        "preparation_ref":reference,
        "signature":hex(request.signature.as_bytes()),
        "signer_public_key":hex(signer_public_key),
        "approval_release_ref":request.approval_release_ref.map(|reference| hex(&reference)),
    }))
}

fn hex(bytes: &[u8]) -> String {
    use std::fmt::Write as _;
    bytes
        .iter()
        .fold(String::with_capacity(bytes.len() * 2), |mut text, byte| {
            let _ = write!(text, "{byte:02x}");
            text
        })
}

pub(crate) fn validate_request_binding(
    request: &NativeEffectPrepareRequestV1,
    credential: &crate::agent_envelope::EnvelopeCredential,
    key: layerx_agent_api::idempotency::Key,
) -> Result<(), EnvelopeError> {
    if key.bytes() != request.idempotency_key {
        return Err(EnvelopeError::InvalidRequest);
    }
    let purpose = &request.purpose.purpose;
    if purpose.tenant.as_str() != credential.tenant()
        || purpose.session_id.to_bytes().ok() != Some(credential.session_id())
        || purpose.generation != credential.generation()
    {
        return Err(EnvelopeError::InvalidCredential);
    }
    Ok(())
}

pub(crate) fn validate_send_request_binding(
    request: &layerx_agent_api::identity::NativeSendPrepareRequestV1,
    credential: &crate::agent_envelope::EnvelopeCredential,
    key: layerx_agent_api::idempotency::Key,
) -> Result<(), EnvelopeError> {
    if key.bytes() != request.idempotency_key {
        return Err(EnvelopeError::InvalidRequest);
    }
    let purpose = &request.purpose.purpose;
    if purpose.tenant.as_str() != credential.tenant()
        || purpose.session_id.to_bytes().ok() != Some(credential.session_id())
        || purpose.generation != credential.generation()
    {
        return Err(EnvelopeError::InvalidCredential);
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::agent_envelope::{encode_envelope, EnvelopeCredential};
    use ed25519_dalek::{Signer, SigningKey};
    use layerx_agent_api::error::RequestId;
    use layerx_agent_api::idempotency::Key;
    use layerx_agent_api::identity::{
        AgentDid, CapabilityId, NativeActivity, NativeLocalGrantConsentV1,
        NativePreparationPurposeV1, SessionId, SignedNativePreparationPurposeV1, TenantId,
    };

    fn request() -> NativeEffectPrepareRequestV1 {
        let payload_hex =
            include_str!("../../layerx-crypto/tests/fixtures/payments/native-1-5.hex").trim();
        let payload = payload_hex
            .as_bytes()
            .chunks_exact(2)
            .map(|p| {
                u8::from_str_radix(core::str::from_utf8(p).expect("fixture hex"), 16)
                    .expect("fixture byte")
            })
            .collect::<Vec<_>>();
        let purpose = NativePreparationPurposeV1 {
            tenant: TenantId::new("native-effect-tenant").expect("tenant"),
            agent_did: AgentDid::new("native-effect-agent").expect("agent"),
            session_id: SessionId::new("11".repeat(32)).expect("session"),
            generation: 1,
            expires_at_ms: 100_000,
            capability_id: CapabilityId::new("22".repeat(32)).expect("capability"),
            preparation_id: [3; 32],
            canonical_digest: [4; 32],
            commitment: [5; 32],
        };
        let key = SigningKey::from_bytes(&[7; 32]);
        let digest: [u8; 32] =
            Sha256::digest(purpose.canonical_bytes().expect("real purpose bytes")).into();
        let signed = SignedNativePreparationPurposeV1 {
            purpose,
            owner_public_key: key.verifying_key().to_bytes(),
            signature: key.sign(&digest).to_bytes(),
        };
        NativeEffectPrepareRequestV1 {
            activity: NativeActivity::new(1, 5).expect("native Asset Send"),
            actor: signed.purpose.agent_did.clone(),
            authority: "owner".to_owned(),
            account_sequence: 7,
            not_before: 1,
            not_after: 100,
            idempotency_key: [4; 32],
            fee_limit: 123,
            payload_hash: Sha256::digest(&payload).into(),
            payload,
            capability_id: signed.purpose.capability_id.clone(),
            purpose: signed,
            local_grant: None,
        }
    }

    #[test]
    fn native_asset_codec_retains_real_payload_and_domain() {
        let request = request();
        assert_eq!(
            request
                .activity
                .activity_type()
                .expect("packed native activity")
                .value(),
            0x0001_0005
        );
        let value = encode_native_effect_prepare(&request).expect("native effect codec");
        assert_eq!(value["variant"], "native_effect_v1");
        assert_eq!(
            value["activity"],
            json!({"version":"1","module":"1","ordinal":"5"})
        );
        assert_eq!(
            value["payload"],
            include_str!("../../layerx-crypto/tests/fixtures/payments/native-1-5.hex").trim()
        );
        assert_eq!(value["local_grant"], Value::Null);
        assert_eq!(value.as_object().expect("object").len(), 14);
        let canonical = serde_json::to_vec(&value).expect("canonical json");
        let mut actual = Sha256::new();
        actual.update(PREPARE_DOMAIN);
        actual.update(&canonical);
        assert_eq!(
            native_effect_prepare_digest(&request).expect("domain digest"),
            <[u8; 32]>::from(actual.finalize())
        );
        let mut legacy = Sha256::new();
        legacy.update(b"LXP/agent/native-prepare/v1\0");
        legacy.update(&canonical);
        assert_ne!(
            native_effect_prepare_digest(&request).expect("domain digest"),
            <[u8; 32]>::from(legacy.finalize())
        );
        if let Ok(path) = std::env::var("NATIVE_EFFECT_RUST_CANONICAL_OUTPUT") {
            std::fs::write(path, canonical).expect("private canonical codec artifact");
        }
    }

    #[test]
    fn native_effect_codec_preserves_field_and_legacy_refusals() {
        let base = request();
        let mut invalid = Vec::new();
        let mut r = base.clone();
        r.activity = NativeActivity::new(9, 3).expect("Programs");
        invalid.push(r);
        let mut r = base.clone();
        r.activity.ordinal = 0;
        invalid.push(r);
        let mut r = base.clone();
        r.activity.module = 12;
        invalid.push(r);
        let mut r = base.clone();
        r.actor = AgentDid::new("other-agent").expect("actor");
        invalid.push(r);
        let mut r = base.clone();
        r.capability_id = CapabilityId::new("33".repeat(32)).expect("cap");
        invalid.push(r);
        let mut r = base.clone();
        r.authority.clear();
        invalid.push(r);
        let mut r = base.clone();
        r.authority = "a".repeat(layerx_types::limits::MAX_AUTHORITY_BYTES + 1);
        invalid.push(r);
        let mut r = base.clone();
        r.not_after = 0;
        invalid.push(r);
        let mut r = base.clone();
        r.payload.clear();
        invalid.push(r);
        let mut r = base.clone();
        r.payload = vec![1; layerx_types::limits::MAX_PAYLOAD_BYTES + 1];
        invalid.push(r);
        let mut r = base.clone();
        r.purpose.purpose.generation = 0;
        invalid.push(r);
        let mut r = base.clone();
        r.purpose.purpose.expires_at_ms = 0;
        invalid.push(r);
        let mut r = base.clone();
        r.purpose.purpose.session_id = SessionId::new("AA".repeat(32)).expect("sessiontext");
        invalid.push(r);
        let mut r = base.clone();
        r.local_grant = Some(NativeLocalGrantConsentV1 {
            capability: Vec::new(),
            session_scope: vec![1],
            expires_at_ms: 100,
            owner_public_key: [1; 32],
            signature: [2; 64],
        });
        invalid.push(r);
        for r in invalid {
            assert_eq!(
                encode_native_effect_prepare(&r),
                Err(EnvelopeError::InvalidRequest)
            );
        }
        let legacy = layerx_agent_api::identity::NativePrepareRequestV1 {
            activity: base.activity,
            actor: base.actor,
            authority: base.authority,
            account_sequence: base.account_sequence,
            not_before: base.not_before,
            not_after: base.not_after,
            idempotency_key: base.idempotency_key,
            fee_limit: base.fee_limit,
            payload: base.payload,
            payload_hash: base.payload_hash,
            capability_id: base.capability_id,
            purpose: base.purpose,
            local_grant: None,
        };
        assert_eq!(
            crate::agent_envelope::encode_native_prepare(&legacy),
            Err(EnvelopeError::InvalidRequest)
        );
    }

    #[test]
    fn native_effect_authenticated_envelope_binds_full_session() {
        let request = request();
        let body = encode_native_effect_prepare(&request).expect("effect body");
        let credential = EnvelopeCredential::new("native-effect-tenant", [0x11; 32], [0x55; 32], 1)
            .expect("credential");
        assert_eq!(
            validate_request_binding(&request, &credential, Key::new([4; 32]).expect("key")),
            Ok(())
        );
        for c in [
            EnvelopeCredential::new("other", [0x11; 32], [0x55; 32], 1).expect("tenant"),
            EnvelopeCredential::new("native-effect-tenant", [0x12; 32], [0x55; 32], 1)
                .expect("session"),
            EnvelopeCredential::new("native-effect-tenant", [0x11; 32], [0x55; 32], 2)
                .expect("generation"),
        ] {
            assert_eq!(
                validate_request_binding(&request, &c, Key::new([4; 32]).expect("key")),
                Err(EnvelopeError::InvalidCredential)
            );
        }
        assert_eq!(
            validate_request_binding(&request, &credential, Key::new([8; 32]).expect("wrong key")),
            Err(EnvelopeError::InvalidRequest)
        );
        let envelope = encode_envelope(
            crate::Operation::Prepare,
            RequestId(7),
            &body,
            Some(&credential),
            Some(Key::new([4; 32]).expect("idempotency key")),
        )
        .expect("actual authenticated envelope");
        assert_eq!(envelope["request"], body);
        assert!(encode_envelope(
            crate::Operation::Prepare,
            RequestId(7),
            &body,
            None,
            Some(Key::new([4; 32]).expect("idempotency key"))
        )
        .is_err());
        assert!(encode_envelope(
            crate::Operation::Prepare,
            RequestId(7),
            &body,
            Some(&credential),
            None
        )
        .is_err());
    }
    fn send_request() -> layerx_agent_api::identity::NativeSendPrepareRequestV1 {
        use layerx_agent_api::identity::{NativeSendPurposeV1, SignedNativeSendPurposeV1};
        let base = request();
        let key = SigningKey::from_bytes(&[7; 32]);
        let owner_public_key = key.verifying_key().to_bytes();
        let purpose = NativeSendPurposeV1 {
            tenant: base.purpose.purpose.tenant,
            agent_did: base.actor.clone(),
            owner_did: AgentDid::new(format!("did:layerx:{}", hex(&owner_public_key)))
                .expect("owner DID"),
            owner_public_key,
            session_id: base.purpose.purpose.session_id,
            generation: 1,
            expires_at_ms: 100_000,
            capability_id: base.capability_id.clone(),
            protocol_version: 3,
            network_id: 1,
            activity: base.activity,
            preparation_id: [4; 32],
            canonical_digest: [4; 32],
            economic_action: [6; 32],
            idempotency_key: base.idempotency_key,
            commitment: [5; 32],
        };
        let digest: [u8; 32] =
            Sha256::digest(purpose.canonical_bytes().expect("Send purpose bytes")).into();
        let purpose = SignedNativeSendPurposeV1 {
            purpose,
            owner_public_key,
            signature: key.sign(&digest).to_bytes(),
        };
        layerx_agent_api::identity::NativeSendPrepareRequestV1 {
            activity: base.activity,
            actor: base.actor,
            authority: base.authority,
            account_sequence: base.account_sequence,
            not_before: base.not_before,
            not_after: base.not_after,
            idempotency_key: base.idempotency_key,
            fee_limit: base.fee_limit,
            payload: base.payload,
            payload_hash: base.payload_hash,
            capability_id: base.capability_id,
            purpose,
            local_grant: None,
        }
    }

    #[test]
    fn native_send_codec_preserves_payload_and_distinct_domain() {
        let request = send_request();
        let value = encode_native_send_prepare(&request).expect("Send codec");
        assert_eq!(value["variant"], "native_send_v1");
        assert_eq!(
            value["payload"],
            include_str!("../../layerx-crypto/tests/fixtures/payments/native-1-5.hex").trim()
        );
        assert_eq!(
            value["purpose"]["purpose"]["owner_did"],
            request.purpose.purpose.owner_did.as_str()
        );
        assert_eq!(
            value["purpose"]["purpose"]["economic_action"],
            hex(&request.purpose.purpose.economic_action)
        );
        assert_eq!(
            value["purpose"]["purpose"]["idempotency_key"],
            hex(&request.idempotency_key)
        );
        let canonical = serde_json::to_vec(&value).expect("canonical Send JSON");
        let digest: [u8; 32] = Sha256::new()
            .chain_update(SEND_PREPARE_DOMAIN)
            .chain_update(&canonical)
            .finalize()
            .into();
        assert_eq!(
            native_send_prepare_digest(&request).expect("Send digest"),
            digest
        );
        let old: [u8; 32] = Sha256::new()
            .chain_update(PREPARE_DOMAIN)
            .chain_update(&canonical)
            .finalize()
            .into();
        assert_ne!(digest, old);
        if let Ok(path) = std::env::var("NATIVE_SEND_RUST_CANONICAL_OUTPUT") {
            std::fs::write(path, canonical).expect("private Send canonical artifact");
        }
    }

    #[test]
    fn native_send_codec_refuses_changed_authority_and_request_coordinates() {
        let base = send_request();
        let mut invalid = Vec::new();
        let mut changed = base.clone();
        changed.activity = NativeActivity::new(1, 6).expect("native ordinal");
        invalid.push(changed);
        let mut changed = base.clone();
        changed.actor = AgentDid::new("other-agent").expect("actor");
        invalid.push(changed);
        let mut changed = base.clone();
        changed.idempotency_key[0] ^= 1;
        invalid.push(changed);
        let mut changed = base.clone();
        changed.purpose.owner_public_key[0] ^= 1;
        invalid.push(changed);
        let mut changed = base.clone();
        changed.purpose.purpose.owner_did = AgentDid::new("other-owner").expect("owner");
        invalid.push(changed);
        let mut changed = base.clone();
        changed.purpose.purpose.activity = NativeActivity::new(1, 6).expect("native ordinal");
        invalid.push(changed);
        let mut changed = base.clone();
        changed.purpose.purpose.generation = 0;
        invalid.push(changed);
        let mut changed = base.clone();
        changed.purpose.purpose.expires_at_ms = 0;
        invalid.push(changed);
        let mut changed = base.clone();
        changed.payload.clear();
        invalid.push(changed);
        let mut changed = base.clone();
        changed.not_after = 0;
        invalid.push(changed);
        for request in invalid {
            assert_eq!(
                encode_native_send_prepare(&request),
                Err(EnvelopeError::InvalidRequest)
            );
        }
    }

    #[test]
    fn native_send_authenticated_envelope_binds_full_session() {
        let request = send_request();
        let credential = EnvelopeCredential::new("native-effect-tenant", [0x11; 32], [0x55; 32], 1)
            .expect("credential");
        let key = Key::new(request.idempotency_key).expect("key");
        assert_eq!(
            validate_send_request_binding(&request, &credential, key),
            Ok(())
        );
        for credential in [
            EnvelopeCredential::new("other", [0x11; 32], [0x55; 32], 1).expect("tenant"),
            EnvelopeCredential::new("native-effect-tenant", [0x12; 32], [0x55; 32], 1)
                .expect("session"),
            EnvelopeCredential::new("native-effect-tenant", [0x11; 32], [0x55; 32], 2)
                .expect("generation"),
        ] {
            assert_eq!(
                validate_send_request_binding(&request, &credential, key),
                Err(EnvelopeError::InvalidCredential)
            );
        }
        assert_eq!(
            validate_send_request_binding(
                &request,
                &credential,
                Key::new([8; 32]).expect("changed key")
            ),
            Err(EnvelopeError::InvalidRequest)
        );
        let body = encode_native_send_prepare(&request).expect("Send body");
        let envelope = encode_envelope(
            crate::Operation::Prepare,
            RequestId(7),
            &body,
            Some(&credential),
            Some(key),
        )
        .expect("authenticated Send envelope");
        assert_eq!(envelope["request"], body);
        assert!(encode_envelope(
            crate::Operation::Prepare,
            RequestId(7),
            &body,
            None,
            Some(key)
        )
        .is_err());
        assert!(encode_envelope(
            crate::Operation::Prepare,
            RequestId(7),
            &body,
            Some(&credential),
            None
        )
        .is_err());
    }
}
