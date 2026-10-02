use super::*;
use layerx_types::payload::{ModuleId, ModuleRegistration};

fn checked<T, E: std::fmt::Debug>(result: Result<T, E>) -> T {
    result.unwrap_or_else(|error| panic!("canonical recovery: {error:?}"))
}

fn captured() -> (Vec<u8>, Vec<u8>, ModuleRegistry, u64) {
    let path = std::path::Path::new(env!("CARGO_MANIFEST_DIR"))
        .join("../../../tests/fixtures/custody/daemon-light-credit-receipt");
    let kind = checked(ActivityType::new(ModuleId::Bridge, 1));
    let registration = checked(ModuleRegistration::new(ModuleId::Bridge, &[kind]));
    let registry = checked(ModuleRegistry::new(&[registration]));
    let signed = checked(std::fs::read(path.join("activity")));
    let activity = checked(layerx_wire::activity::decode_signed(&signed, &registry));
    let canonical = checked(layerx_wire::activity::encode_unsigned(&activity));
    let header = checked(layerx_wire::receipt::decode_batch_header(
        &checked(std::fs::read(path.join("header")))));
    (canonical, signed, registry, header.last_sequence())
}

#[test]
fn recovery_preserves_every_canonical_field_and_the_retained_authority_variant() {
    let (canonical, _, registry, observed) = captured();
    let activity = checked(layerx_wire::activity::decode_unsigned(&canonical, &registry));
    let authorities = [
        checked(Authority::owner(activity.authority())),
        checked(Authority::session_key(activity.authority())),
        checked(Authority::delegated_capability(activity.authority())),
        checked(Authority::budget_allowance(activity.authority())),
        checked(Authority::escrow(activity.authority())),
        checked(Authority::protocol_module(activity.authority())),
    ];
    for authority in &authorities {
        let restored = checked(restore_canonical(&canonical, observed, &registry, Some(authority)));
        assert_eq!(restored.envelope.authority(), authority);
        assert_eq!(restored.canonical_bytes, canonical);
        assert_eq!(restored.observed_head_sequence, observed);
        assert_eq!(restored.audit.observed_head_sequence, observed);
        assert_eq!(restored.envelope.protocol_version(), activity.protocol_version());
        assert_eq!(restored.envelope.network_id(), activity.network_id());
        assert_eq!(restored.envelope.actor_did().as_bytes(), activity.actor_did());
        assert_eq!(restored.envelope.activity_type(), activity.activity_type());
        assert_eq!(restored.envelope.account_sequence(), activity.account_sequence());
        assert_eq!(restored.envelope.fee_limit().value(), activity.fee_limit());
        assert_eq!(restored.envelope.payload_hash(), activity.payload_hash());
        assert_eq!(restored.envelope.idempotency_key().bytes(), activity.idempotency_key());
        assert_eq!(restored.audit.idempotency_key, activity.idempotency_key());
        assert_eq!(restored.envelope.timestamp_bound().not_before(), activity.timestamp_bound().not_before);
        assert_eq!(restored.envelope.timestamp_bound().not_after(), activity.timestamp_bound().not_after);
        assert_eq!(restored.signing_preimage, *checked(layerx_wire::sign::preimage(&activity)).as_bytes());
        assert_eq!(restored.disclosure_digest, checked(disclose(&canonical, &registry)).digest);
        checked(verify_disclosure_binding(&restored));
        let again = checked(restore_canonical(&restored.canonical_bytes,
            restored.observed_head_sequence, &registry, Some(restored.envelope.authority())));
        assert_eq!(again, restored);
    }
}

#[test]
fn recovery_refuses_missing_authority_signed_bytes_malformed_payload_and_wrong_registry() {
    let (canonical, signed, registry, observed) = captured();
    let activity = checked(layerx_wire::activity::decode_unsigned(&canonical, &registry));
    let authority = checked(Authority::owner(activity.authority()));
    assert_eq!(restore_canonical(&canonical, observed, &registry, None),
        Err(RestorePreparationError::MissingAuthority));
    let mut changed_authority = activity.authority().to_vec();
    changed_authority[0] ^= 1;
    let changed_authority = checked(Authority::owner(&changed_authority));
    assert_eq!(restore_canonical(&canonical, observed, &registry, Some(&changed_authority)),
        Err(RestorePreparationError::AuthorityMismatch));
    assert!(restore_canonical(&signed, observed, &registry, Some(&authority)).is_err());
    let mut trailing = canonical.clone();
    trailing.push(0);
    assert!(restore_canonical(&trailing, observed, &registry, Some(&authority)).is_err());
    assert!(restore_canonical(&canonical[..canonical.len() - 1], observed,
        &registry, Some(&authority)).is_err());
    let mut payload_changed = canonical.clone();
    let last = payload_changed.len() - 1;
    payload_changed[last] ^= 1;
    assert_eq!(restore_canonical(&payload_changed, observed, &registry, Some(&authority)),
        Err(RestorePreparationError::PayloadHashMismatch));
    let kind = checked(ActivityType::new(ModuleId::Asset, 1));
    let registration = checked(ModuleRegistration::new(ModuleId::Asset, &[kind]));
    let other_registry = checked(ModuleRegistry::new(&[registration]));
    assert!(restore_canonical(&canonical, observed, &other_registry, Some(&authority)).is_err());
}
