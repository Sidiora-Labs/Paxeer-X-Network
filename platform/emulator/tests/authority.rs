//! Authority-resolution parity between the emulator core and an executing node.
//!
//! The emulator bridge resolves the authority of every activity through the
//! same `lxp_authority_resolve_activity` entry an executing node uses, so the
//! grant it binds, the scope it admits and the authority hash it derives are
//! the node's, not a locally synthesised stand-in. These tests drive the real
//! bridge over its C boundary, recompute the authority-hash preimage
//! independently in Rust from the published domain tag, and pin every field of
//! the owner grant the node would build for the same activity.

use std::ffi::{c_void, CStr};
use std::os::raw::{c_char, c_int, c_uchar, c_uint, c_ulonglong};

const EMULATOR_SEED: [u8; 32] = [0x42; 32];
const ACTOR_DID: &[u8] = b"did:layerx:authority";
const UNKNOWN_DID: &[u8] = b"did:layerx:unfunded";
const NETWORK_ID: u32 = 402;
const PROTOCOL_VERSION: u16 = 3;
const CLOCK_MS: u64 = 1_700_000_000_000;
const NOT_BEFORE_MS: u64 = 1_699_999_970_000;
const NOT_AFTER_MS: u64 = 1_700_000_120_000;
const TIMESTAMP_WINDOW_MS: u64 = 86_400_000;
const ASSET_MODULE: u16 = 1;
const GOVERNANCE_MODULE: u16 = 7;
const PROGRAMS_MODULE: u16 = 9;
const ORDINAL_MINIMUM: u16 = 1;
const ORDINAL_MAXIMUM: u16 = 11;
const AUTHORITY_OWNER: u32 = 1;

#[repr(C)]
#[derive(Clone, Copy)]
struct CoreAuthority {
    actor: [u8; 32],
    principal: [u8; 32],
    verified_key: [u8; 32],
    grant_id: [u8; 32],
    grantor: [u8; 32],
    grantee: [u8; 32],
    authority_hash: [u8; 32],
    kind: c_uint,
    not_before: c_ulonglong,
    not_after: c_ulonglong,
    scope_module_mask: c_ulonglong,
    scope_activity_ordinal_min: u16,
    scope_activity_ordinal_max: u16,
    scope_maximum_per_activity_hi: c_ulonglong,
    scope_maximum_per_activity_lo: c_ulonglong,
    scope_maximum_total_hi: c_ulonglong,
    scope_maximum_total_lo: c_ulonglong,
    scope_maximum_per_period_hi: c_ulonglong,
    scope_maximum_per_period_lo: c_ulonglong,
    revoked: u8,
}

impl CoreAuthority {
    fn blank() -> Self {
        Self {
            actor: [0; 32],
            principal: [0; 32],
            verified_key: [0; 32],
            grant_id: [0; 32],
            grantor: [0; 32],
            grantee: [0; 32],
            authority_hash: [0; 32],
            kind: 0,
            not_before: 0,
            not_after: 0,
            scope_module_mask: 0,
            scope_activity_ordinal_min: 0,
            scope_activity_ordinal_max: 0,
            scope_maximum_per_activity_hi: 0,
            scope_maximum_per_activity_lo: 0,
            scope_maximum_total_hi: 0,
            scope_maximum_total_lo: 0,
            scope_maximum_per_period_hi: 0,
            scope_maximum_per_period_lo: 0,
            revoked: 0,
        }
    }
}

unsafe extern "C" {
    fn platform_emulator_create_for_protocol(
        network_id: c_uint,
        timestamp_ms: c_ulonglong,
        sequencer_seed: *const c_uchar,
        protocol_version: u16,
    ) -> *mut c_void;
    fn platform_emulator_destroy(emulator: *mut c_void);
    fn platform_emulator_error_name(result: c_int) -> *const c_char;
    fn platform_emulator_prefund(
        emulator: *mut c_void,
        did: *const c_uchar,
        did_length: usize,
        public_key: *const c_uchar,
        amount_hi: c_ulonglong,
        amount_lo: c_ulonglong,
    ) -> c_int;
    fn platform_emulator_resolve_authority(
        emulator: *mut c_void,
        activity: *const c_uchar,
        length: usize,
        authority: *mut CoreAuthority,
    ) -> c_int;
}

struct Emulator {
    handle: *mut c_void,
}

impl Emulator {
    fn boot() -> Result<Self, String> {
        let handle = unsafe {
            platform_emulator_create_for_protocol(
                NETWORK_ID,
                CLOCK_MS,
                EMULATOR_SEED.as_ptr(),
                PROTOCOL_VERSION,
            )
        };
        if handle.is_null() {
            return Err("the emulator core refused to boot".to_owned());
        }
        Ok(Self { handle })
    }

    fn prefund(&self, did: &[u8], public_key: &[u8; 32], amount_lo: u64) -> Result<(), String> {
        let status = unsafe {
            platform_emulator_prefund(
                self.handle,
                did.as_ptr(),
                did.len(),
                public_key.as_ptr(),
                0,
                amount_lo,
            )
        };
        if status == 0 {
            Ok(())
        } else {
            Err(format!("prefund refused with {}", error_name(status)))
        }
    }

    fn resolve(&self, activity: &[u8]) -> Result<CoreAuthority, String> {
        let mut view = CoreAuthority::blank();
        let status = unsafe {
            platform_emulator_resolve_authority(
                self.handle,
                activity.as_ptr(),
                activity.len(),
                &raw mut view,
            )
        };
        if status == 0 {
            Ok(view)
        } else {
            Err(error_name(status))
        }
    }
}

impl Drop for Emulator {
    fn drop(&mut self) {
        unsafe { platform_emulator_destroy(self.handle) }
    }
}

fn error_name(status: c_int) -> String {
    let name = unsafe { platform_emulator_error_name(status) };
    if name.is_null() {
        return format!("status {status}");
    }
    unsafe { CStr::from_ptr(name) }
        .to_string_lossy()
        .into_owned()
}

fn checked<T, E: std::fmt::Debug>(result: Result<T, E>) -> Result<T, String> {
    result.map_err(|error| format!("{error:?}"))
}

fn sha256(parts: &[&[u8]]) -> [u8; 32] {
    use sha2::{Digest, Sha256};
    let mut hasher = Sha256::new();
    for part in parts {
        hasher.update(part);
    }
    let digest = hasher.finalize();
    let mut out = [0u8; 32];
    out.copy_from_slice(&digest);
    out
}

/// Recomputes the core authority hash from the published domain tag exactly as
/// `lxp_authority_hash` derives it: the tag, the one-byte kind, the grant
/// identifier and the verified key.
fn authority_hash(kind: u8, grant_id: &[u8; 32], verified_key: &[u8; 32]) -> [u8; 32] {
    use layerx_wire::hash::Domain;
    sha256(&[Domain::AuthorityHash.tag(), &[kind], grant_id, verified_key])
}

/// Recomputes the core DID identifier exactly as `lxp_did_id_derive` does: the
/// domain tag, the big-endian DID length and the DID bytes.
fn did_identifier(did: &[u8]) -> Result<[u8; 32], String> {
    use layerx_wire::hash::Domain;
    let length = u16::try_from(did.len()).map_err(|error| error.to_string())?;
    Ok(sha256(&[Domain::DidId.tag(), &length.to_be_bytes(), did]))
}

fn signed_activity(
    did: &[u8],
    ordinal: u16,
    not_before: u64,
    not_after: u64,
    sequence: u64,
) -> Result<Vec<u8>, String> {
    use ed25519_dalek::{Signer, SigningKey};
    use layerx_types::activity::{Authority, EnvelopeBuilder, Signature, TimestampBound};
    use layerx_types::amount::Amount;
    use layerx_types::ids::{Did, IdempotencyKey};
    use layerx_types::payload::{
        ActivityType, ModuleId, ModuleRegistration, ModuleRegistry, Payload,
    };
    let key = SigningKey::from_bytes(&EMULATOR_SEED);
    let kind = checked(ActivityType::new(ModuleId::Programs, ordinal))?;
    let registry = checked(ModuleRegistry::new(&[checked(ModuleRegistration::new(
        ModuleId::Programs,
        &[kind],
    ))?]))?;
    let payload = checked(Payload::new(&registry, kind, &[0x11, 0x22, 0x33, 0x44]))?;
    let hash = checked(layerx_wire::hash::payload_hash_for(&payload))?;
    let mut idempotency = [9; 32];
    idempotency[24..].copy_from_slice(&sequence.to_be_bytes());
    let mut builder = EnvelopeBuilder::new();
    checked(builder.protocol_version(PROTOCOL_VERSION))?;
    checked(builder.network_id(NETWORK_ID))?;
    checked(builder.activity_type(kind))?;
    checked(builder.actor_did(checked(Did::new(did))?))?;
    checked(builder.authority(checked(Authority::owner(&key.verifying_key().to_bytes()))?))?;
    checked(builder.account_sequence(sequence))?;
    checked(builder.timestamp_bound(checked(TimestampBound::new(not_before, not_after))?))?;
    checked(builder.idempotency_key(IdempotencyKey::new(idempotency)))?;
    checked(builder.fee_limit(Amount::from_u128(1_000_000)))?;
    checked(builder.payload_hash(hash))?;
    checked(builder.payload(payload))?;
    let unsigned = checked(builder.build())?;
    let preimage = checked(layerx_wire::sign::preimage_unsigned(&unsigned))?;
    let signature = key.sign(preimage.as_bytes()).to_bytes();
    checked(layerx_wire::activity::encode_signed_envelope(
        &unsigned.attach_signature(checked(Signature::new(&signature))?),
    ))
}

fn booted() -> Result<(Emulator, [u8; 32]), String> {
    use ed25519_dalek::SigningKey;
    let public = SigningKey::from_bytes(&EMULATOR_SEED)
        .verifying_key()
        .to_bytes();
    let emulator = Emulator::boot()?;
    emulator.prefund(ACTOR_DID, &public, 100_000_000)?;
    Ok((emulator, public))
}

fn check_owner_grant(view: &CoreAuthority, public: &[u8; 32]) -> Result<(), String> {
    let did = did_identifier(ACTOR_DID)?;
    assert_eq!(view.kind, AUTHORITY_OWNER);
    assert_eq!(view.revoked, 0);
    assert_eq!(view.verified_key, *public);
    assert_eq!(view.grantor, did);
    assert_eq!(view.grantee, did);
    assert_eq!(view.actor, did);
    assert_eq!(view.principal, did);
    assert_ne!(view.grant_id, [0u8; 32]);
    assert_eq!(view.not_before, NOT_BEFORE_MS);
    assert_eq!(view.not_after, NOT_AFTER_MS + 1);
    assert_ne!(view.not_after, u64::MAX);
    Ok(())
}

fn check_declared_envelope_scope(view: &CoreAuthority) {
    let declared = (1u64 << ASSET_MODULE) | (1u64 << GOVERNANCE_MODULE) | (1u64 << PROGRAMS_MODULE);
    assert_eq!(view.scope_module_mask, declared);
    assert_ne!(view.scope_module_mask, u64::MAX);
    assert_eq!(view.scope_activity_ordinal_min, ORDINAL_MINIMUM);
    assert_eq!(view.scope_activity_ordinal_max, ORDINAL_MAXIMUM);
    assert_eq!(view.scope_maximum_per_activity_hi, 0);
    assert_eq!(view.scope_maximum_per_activity_lo, 0);
    assert_eq!(view.scope_maximum_total_hi, 0);
    assert_eq!(view.scope_maximum_total_lo, 0);
    assert_eq!(view.scope_maximum_per_period_hi, 0);
    assert_eq!(view.scope_maximum_per_period_lo, 0);
}

/// The emulator library publishes the build-script link directives for the
/// `LayerX` C core, so the bridge entry points these tests call resolve only when
/// the library itself is part of this binary. Driving its public entry keeps
/// that dependency explicit and pins the refusal an empty command line earns.
#[test]
fn emulator_entry_refuses_an_empty_command_line() {
    assert!(layerx_platform_emulator::run(Vec::<String>::new()).is_err());
}

#[test]
fn emulator_binds_the_node_owner_grant_and_its_authority_hash() -> Result<(), String> {
    let (emulator, public) = booted()?;
    let activity = signed_activity(ACTOR_DID, 3, NOT_BEFORE_MS, NOT_AFTER_MS, 0)?;
    let view = emulator.resolve(&activity).map_err(|name| {
        format!("the emulator refused to resolve a signed owner activity with {name}")
    })?;
    check_owner_grant(&view, &public)?;
    check_declared_envelope_scope(&view);
    let kind = u8::try_from(view.kind).map_err(|error| error.to_string())?;
    assert_eq!(
        view.authority_hash,
        authority_hash(kind, &view.grant_id, &view.verified_key)
    );
    assert_ne!(
        view.authority_hash,
        authority_hash(kind, &[0u8; 32], &view.verified_key)
    );
    Ok(())
}

#[test]
fn emulator_authority_hash_is_stable_across_equal_activities() -> Result<(), String> {
    let (emulator, public) = booted()?;
    let first = emulator.resolve(&signed_activity(
        ACTOR_DID,
        3,
        NOT_BEFORE_MS,
        NOT_AFTER_MS,
        0,
    )?)?;
    let second = emulator.resolve(&signed_activity(
        ACTOR_DID,
        1,
        NOT_BEFORE_MS,
        NOT_AFTER_MS,
        1,
    )?)?;
    let shifted = emulator.resolve(&signed_activity(
        ACTOR_DID,
        3,
        NOT_BEFORE_MS + 1,
        NOT_AFTER_MS,
        2,
    )?)?;
    check_owner_grant(&first, &public)?;
    check_declared_envelope_scope(&first);
    assert_eq!(first.grant_id, second.grant_id);
    assert_eq!(first.authority_hash, second.authority_hash);
    assert_ne!(first.grant_id, shifted.grant_id);
    assert_ne!(first.authority_hash, shifted.authority_hash);
    Ok(())
}

#[test]
fn emulator_refuses_authority_the_node_would_refuse() -> Result<(), String> {
    let (emulator, _) = booted()?;
    let unknown = signed_activity(UNKNOWN_DID, 3, NOT_BEFORE_MS, NOT_AFTER_MS, 0)?;
    assert_eq!(
        emulator.resolve(&unknown).err(),
        Some("LXP_ERR_UNKNOWN_DID".to_owned())
    );
    let early = signed_activity(ACTOR_DID, 3, CLOCK_MS + 1_000, CLOCK_MS + 2_000, 1)?;
    assert_eq!(
        emulator.resolve(&early).err(),
        Some("LXP_ERR_NOT_YET_VALID".to_owned())
    );
    let expired = signed_activity(ACTOR_DID, 3, CLOCK_MS - 2_000, CLOCK_MS - 1_000, 2)?;
    assert_eq!(
        emulator.resolve(&expired).err(),
        Some("LXP_ERR_EXPIRED".to_owned())
    );
    let unbounded = signed_activity(
        ACTOR_DID,
        3,
        NOT_BEFORE_MS,
        NOT_BEFORE_MS + TIMESTAMP_WINDOW_MS + 1,
        3,
    )?;
    assert_eq!(
        emulator.resolve(&unbounded).err(),
        Some("LXP_ERR_MALFORMED_ENVELOPE".to_owned())
    );
    Ok(())
}

fn conformance_hex(value: &str) -> Result<Vec<u8>, String> {
    if value.is_empty() || value.len() % 2 != 0 || !value.bytes().all(|byte| byte.is_ascii_digit() || (b'a'..=b'f').contains(&byte)) {
        return Err("noncanonical conformance hexadecimal bytes".to_owned());
    }
    value.as_bytes().chunks_exact(2).map(|pair| {
        let text = std::str::from_utf8(pair).map_err(|error| error.to_string())?;
        u8::from_str_radix(text, 16).map_err(|error| error.to_string())
    }).collect()
}

fn conformance_hex32(value: &serde_json::Value) -> Result<[u8; 32], String> {
    conformance_hex(value.as_str().ok_or("missing canonical hexadecimal field")?)?
        .try_into().map_err(|_| "expected 32 bytes".to_owned())
}

fn conformance_encode(bytes: &[u8]) -> String {
    use std::fmt::Write as _;
    let mut output = String::with_capacity(bytes.len() * 2);
    for byte in bytes { let _ = write!(output, "{byte:02x}"); }
    output
}

fn conformance_verified_state(state: &serde_json::Value, key: [u8; 32]) -> Result<serde_json::Value, String> {
    let receipt = conformance_hex(state["receipt"].as_str().ok_or("head omitted receipt")?)?;
    let verified = checked(layerx_proof::receipt::verify_sequencer_signature(&receipt, key))?;
    let protocol = verified.protocol().ok_or("head is not a native receipt")?;
    if protocol.protocol_version() != PROTOCOL_VERSION
        || protocol.resulting_state_root() != conformance_hex32(&state["root"])?
        || Some(protocol.global_sequence()) != state["sequence"].as_u64() {
        return Err("state observation does not match its genuine signed head receipt".to_owned());
    }
    Ok(serde_json::json!({"root": state["root"], "sequence": state["sequence"]}))
}

fn conformance_verified_outcome(
    row: &serde_json::Value,
    observation: &serde_json::Value,
    key: [u8; 32],
    network: u32,
) -> Result<serde_json::Value, String> {
    use layerx_types::payload::{ActivityType, ModuleId, ModuleRegistration, ModuleRegistry};
    let bytes = conformance_hex(observation["receipt"].as_str().ok_or("execution omitted receipt")?)?;
    let decoded = checked(layerx_wire::receipt::decode(&bytes))?;
    let protocol = decoded.protocol().ok_or("execution is not a native receipt")?;
    let before = conformance_verified_state(&observation["receipt_before"], key)?;
    let authorised = layerx_proof::receipt::AuthorizedBatch::new(
        protocol.batch_id(), protocol.asset(), conformance_hex32(&before["root"])?,
        protocol.resulting_state_root(), key,
    );
    let verified = checked(layerx_proof::receipt::verify_outcome(&bytes, &authorised))?;
    let receipt = checked(layerx_wire::receipt::decode(verified.canonical_bytes()))?;
    let protocol = receipt.protocol().ok_or("verified protocol receipt absent")?;
    let types: Vec<ActivityType> = (1..=11).map(|ordinal| checked(ActivityType::new(ModuleId::Programs, ordinal))).collect::<Result<_, _>>()?;
    let registration = checked(ModuleRegistration::new(ModuleId::Programs, &types))?;
    let registry = checked(ModuleRegistry::new(&[registration]))?;
    let activity_bytes = conformance_hex(row["activity"].as_str().ok_or("actual request absent")?)?;
    let activity = checked(layerx_wire::activity::decode_signed(&activity_bytes, &registry))?;
    if activity.network_id() != network
        || activity.protocol_version() != PROTOCOL_VERSION
        || checked(layerx_wire::hash::activity_id(&activity))? != protocol.activity_id()
        || Some(i64::from(protocol.result_code())) != row["expected"]["result_code"].as_i64()
        || layerx_types::result::ResultCode::from_raw(protocol.result_code()).known().is_none() {
        return Err("receipt differs from the actual declared canonical operation or result".to_owned());
    }
    let abi = row["guest_abi"].as_u64().ok_or("declared guest ABI absent")?;
    if abi != 0 && protocol.program_outcome().map(|outcome| u64::from(outcome.abi_version())) != Some(abi) {
        return Err("actual receipt does not prove the declared guest ABI execution".to_owned());
    }
    let effects: Vec<serde_json::Value> = protocol.effects().iter().map(|effect| serde_json::json!({
        "ordinal":effect.ordinal(), "module":effect.module_id(), "event":effect.event_type(),
        "kind":effect.kind(), "monetary":effect.monetary(), "transfer_set_root":conformance_encode(&effect.transfer_set_root()),
        "body":conformance_encode(effect.body()),
    })).collect();
    let units = protocol.total_units().map(|(actual, charged)| [actual.to_string(), charged.to_string()]);
    Ok(serde_json::json!({
        "canonical_unsigned":conformance_encode(&checked(layerx_wire::receipt::encode_unsigned(&receipt))?),
        "result_code":protocol.result_code(), "module":protocol.module_id(), "module_version":protocol.module_version(),
        "operation":protocol.operation(), "abi":abi, "effects":effects,
        "amount":protocol.amount().to_string(), "fee_charged":protocol.fee_charged().to_string(), "total_units":units,
    }))
}

fn conformance_compare(left: &serde_json::Value, right: &serde_json::Value) -> Result<(), String> {
    if left != right { return Err("same-status canonical observation divergence".to_owned()); }
    Ok(())
}

#[test]
fn conformance_verifies_actual_signed_observations() -> Result<(), String> {
    let path = std::env::var("PAXEER_X_CONFORMANCE_OBSERVATIONS")
        .map_err(|_| "genuine served conformance observations are required".to_owned())?;
    let bytes = std::fs::read(path).map_err(|error| error.to_string())?;
    if bytes.is_empty() || bytes.len() > 16 * 1024 * 1024 { return Err("bounded nonempty observations required".to_owned()); }
    let capture: serde_json::Value = serde_json::from_slice(&bytes).map_err(|error| error.to_string())?;
    assert_eq!(capture["schema"], "layerx.emulator-conformance.observations.v1");
    assert_eq!(capture["protocol_version"], PROTOCOL_VERSION);
    let network = u32::try_from(capture["network_id"].as_u64().ok_or("network absent")?).map_err(|error| error.to_string())?;
    let cases = capture["cases"].as_array().ok_or("nonempty actual cases required")?;
    assert_eq!(cases.len(), 20);
    let mut witnessed_state_failure = false;
    for (index, row) in cases.iter().enumerate() {
        let id = row["id"].as_str().ok_or("case identifier absent")?;
        let mut compared = Vec::new();
        for environment in ["emulator", "hosted"] {
            let key = conformance_hex32(&capture["pins"][environment])?;
            assert_ne!(key, [0; 32]);
            let observed = &row[environment];
            let state = conformance_verified_state(&observed["state"], key)?;
            let before = conformance_verified_state(&observed["before"], key)?;
            if index == 0 { assert_eq!(before, capture["initial_state"], "actual initial state must match its signed known head"); }
            let outcome = if row["kind"] == "rejection" {
                assert_eq!(state, before, "refusal changed signed state");
                serde_json::json!({"status":observed["status"], "error":observed["error"], "stage":observed["stage"]})
            } else {
                let outcome = conformance_verified_outcome(row, observed, key, network)?;
                assert_eq!(outcome, row["expected"]["observation"], "{id}: undeclared effect or metered cost");

                outcome
            };
            if !witnessed_state_failure && state != before {
                let left = serde_json::json!({"status":200, "state":before});
                let right = serde_json::json!({"status":200, "state":state});
                assert!(conformance_compare(&left, &right).is_err(), "same-status different genuine signed state must be refused");
                witnessed_state_failure = true;
            }
            compared.push(serde_json::json!({"outcome":outcome, "state":state}));
        }
        conformance_compare(&compared[0], &compared[1]).map_err(|error| format!("{id}: {error}"))?;
        println!("EMULATOR_CONFORMANCE_CASE {id}");
    }
    assert!(witnessed_state_failure);
    println!("EMULATOR_CONFORMANCE_COMPARATOR different-state");
    Ok(())
}
