//! Visa Trusted Agent Protocol conformance harness. The vectors are the
//! first-party suite under `interop/specs/conformance/visa-tap`, which the
//! gateway deployment pins by identifier, vector count and digest. Every record
//! carries the exact RFC 9421 signature-input and target wire strings and runs
//! through the production `TapRequest` and `TapVerifier` types; the credential
//! signature itself is produced from the record's own key seed, because a
//! signature over a request is a live signer's output and cannot be a literal
//! that stays valid for another target. The credential-binding case at the end
//! drives the binding store and a canonical receipt, so it stays in Rust and is
//! not a vector.

use base64::engine::general_purpose::STANDARD;
use base64::Engine as _;
use ed25519_dalek::{Signer as _, SigningKey};
use layerx_interop_gateway::principal::PrincipalId;
use layerx_interop_gateway::trace::TraceId;
use layerx_proof::inclusion::SequencerAuthorization;
use layerx_proof::receipt::verify_sequencer_signature;
use layerx_visa_tap::{
    bind_verified_agent, prepare_trusted_intent, AgentIntent, AgentPublicKey, CredentialBinding,
    CredentialBindingStore, KeyStatus, MerchantOperationResult, NonceWindow, RegisteredAgentKey,
    TapError, TapRequest, TapVerifier, TrustedAgentRegistry, VerifiedTrustedAgent,
};
use sha2::{Digest as _, Sha256};

const NOW: u64 = 1_735_689_700;
const KEY_ID: &str = "poqkLGiymh_W0uP6PZFw-dvez3QJT5SolqXBCW38r0U";
const AGENT: [u8; 32] = [0x44; 32];

const CREDENTIAL_VERIFICATION: &str =
    include_str!("../../../specs/conformance/visa-tap/credential-verification.json");
const TARGET_CANONICALIZATION: &str =
    include_str!("../../../specs/conformance/visa-tap/target-canonicalization.json");
const PAYMENT_REFERENCE: [u8; 32] = [0x66; 32];
const PAYMENT_FROM: [u8; 32] = [0x11; 32];
const PAYMENT_TO: [u8; 32] = [0x22; 32];
const PAYMENT_ASSET: [u8; 32] = [0x55; 32];
const PAYMENT_AMOUNT: u128 = 25;
const RECEIPT_HEIGHT: u64 = 950;
const V2_UNSIGNED_RECEIPT: &str = concat!(
    "000252010002000000200303030303030303030303030303030303030303030303030303030303030303000000000000",
    "000700000020040404040404040404040404040404040404040404040404040404040404040400000020968bd239d91b",
    "0e9801eb4126a4ab1f3774a23752128003f99b21715d5d765c0d00000020e3b0c44298fc1c149afbf4c8996fb92427ae",
    "41e4649b934ca495991b7852b8550000000000000002000c000000000100000000200000000000000000000000000000",
    "000000000000000000000000000000000000000000710000000000005208000000000000520800000000000000010000",
    "000100000000000000000000000000000000000000000000000000000000000000000000000000000000e3b0c44298fc",
    "1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b85500000000000000010475706178000c00010000030000",
    "0000200000000000000000000000000000000000000000000000000000000000000000000000b5000000000000000000",
    "000000000000000000101a04c4da5d6f757c251cd70947b752917e5671b677517e3d953f362a1df6648ef5b111111111",
    "111111111111111111111111111111111111111111111111111111112222222222222222222222222222222222222222",
    "2222222222222222222222226666666666666666666666666666666666666666666666666666666666666666b83bbb56",
    "a58493750cad329795c11372ac61a56cdfc2f82950a7e21619d733310000000000000000000000000000520800000020",
    "00000000000000000000000000000000000000000000000000000000000003b6000c0000000100000000020000002002",
    "cef2c10e756312b83aa7e7051954161146e03371cb3c4f091303c95e0ee8c90000000000000000000000000000000100",
    "000020000000000000000000000000060606060606060606060606060606060606060600000000000000000000000000",
    "00520a000000000000000000000000000000010000000000000000000000200000000000000000000000000707070707",
    "070707070707070707070707070707000000000000000000000000000000000000000000000000000000000000000100",
    "000020000000000000000000000000000000000000000000000000000000000000000000000020000000000000000000",
    "000000000000000000000000000000000000000000000000000020000000000000000000000000000000000000000000",
    "000000000000000000000200000000000003e800",
);
const V2_RECEIPT_DIGEST: [u8; 32] = [
    0x38, 0xc5, 0x18, 0x89, 0x86, 0xff, 0x90, 0x82, 0xbd, 0xe7, 0x85, 0xce, 0xe4, 0x6a, 0x15, 0x53,
    0xd4, 0x59, 0x09, 0x6d, 0x63, 0xae, 0x62, 0x31, 0x5e, 0x62, 0xa8, 0xca, 0xc2, 0x33, 0x39, 0xb7,
];

fn v2_keys(first_height: u64, last_height: u64) -> [SequencerAuthorization; 2] {
    let mut first_id = [0; 32];
    first_id[31] = 1;
    let mut second_id = [0; 32];
    second_id[31] = 2;
    [
        SequencerAuthorization::new(
            first_id,
            SigningKey::from_bytes(&[0x12; 32])
                .verifying_key()
                .to_bytes(),
            0,
            899,
        ),
        SequencerAuthorization::new(
            second_id,
            SigningKey::from_bytes(&[0x11; 32])
                .verifying_key()
                .to_bytes(),
            first_height,
            last_height,
        ),
    ]
}

fn v2_receipt(edit: impl FnOnce(&mut Vec<u8>)) -> Vec<u8> {
    let mut unsigned: Vec<u8> = V2_UNSIGNED_RECEIPT
        .as_bytes()
        .chunks_exact(2)
        .map(|pair| {
            let digits = std::str::from_utf8(pair)
                .unwrap_or_else(|error| panic!("receipt vector hex: {error}"));
            u8::from_str_radix(digits, 16)
                .unwrap_or_else(|error| panic!("receipt vector hex: {error}"))
        })
        .collect();
    edit(&mut unsigned);
    assert_eq!(unsigned.last(), Some(&0));
    let mut digest = Sha256::new();
    digest.update(b"LXP/v1/receipt\0");
    digest.update(&unsigned);
    let digest: [u8; 32] = digest.finalize().into();
    let signing = SigningKey::from_bytes(&[0x11; 32]);
    assert_eq!(unsigned.pop(), Some(0));
    unsigned.extend_from_slice(&[1, 0, 0, 0, 64]);
    unsigned.extend_from_slice(&signing.sign(&digest).to_bytes());
    unsigned
}

fn payment_result(
    receipt: &[u8],
    keys: &[SequencerAuthorization],
) -> Result<MerchantOperationResult, TapError> {
    MerchantOperationResult::from_receipt(
        receipt,
        keys,
        PAYMENT_REFERENCE,
        PAYMENT_FROM,
        PAYMENT_TO,
        PAYMENT_ASSET,
        PAYMENT_AMOUNT,
    )
}

struct Registry {
    key: RegisteredAgentKey,
}

impl TrustedAgentRegistry for Registry {
    fn resolve(&self, key_id: &str, _now: u64) -> Result<RegisteredAgentKey, TapError> {
        (key_id == self.key.key_id)
            .then(|| self.key.clone())
            .ok_or(TapError::UnknownKey)
    }
}

#[derive(Default)]
struct Bindings(Vec<CredentialBinding>);

impl CredentialBindingStore for Bindings {
    fn put(
        &mut self,
        _principal: &PrincipalId,
        binding: &CredentialBinding,
        _trace: &TraceId,
    ) -> Result<(), TapError> {
        self.0.push(binding.clone());
        Ok(())
    }
}

fn records(suite: &str, source: &str) -> Vec<serde_json::Value> {
    let records: Vec<serde_json::Value> =
        serde_json::from_str(suite).unwrap_or_else(|error| panic!("{source}: {error}"));
    assert!(
        !records.is_empty(),
        "{source}: a suite that carries no vector is not a conformance suite"
    );
    records
}

fn field<'a>(record: &'a serde_json::Value, name: &str, source: &str) -> &'a str {
    record[name]
        .as_str()
        .unwrap_or_else(|| panic!("{source}: every vector declares {name}"))
}

fn number(record: &serde_json::Value, name: &str, source: &str) -> u64 {
    record[name]
        .as_u64()
        .unwrap_or_else(|| panic!("{source}: every vector declares {name}"))
}

fn hex32(value: &str, source: &str) -> [u8; 32] {
    assert_eq!(value.len(), 64, "{source}: {value} is not 32 bytes");
    let mut bytes = [0_u8; 32];
    for (index, byte) in bytes.iter_mut().enumerate() {
        *byte = u8::from_str_radix(&value[index * 2..index * 2 + 2], 16)
            .unwrap_or_else(|error| panic!("{source}: {value}: {error}"));
    }
    bytes
}

fn tap_error(name: &str, source: &str) -> TapError {
    match name {
        "MalformedSignatureInput" => TapError::MalformedSignatureInput,
        "MissingCoveredComponent" => TapError::MissingCoveredComponent,
        "MissingSignatureParameter" => TapError::MissingSignatureParameter,
        "DuplicateSignatureParameter" => TapError::DuplicateSignatureParameter,
        "UnknownSignatureParameter" => TapError::UnknownSignatureParameter,
        "InvalidTag" => TapError::InvalidTag,
        "UnsupportedAlgorithm" => TapError::UnsupportedAlgorithm,
        "InvalidTarget" => TapError::InvalidTarget,
        "MalformedSignature" => TapError::MalformedSignature,
        "NotYetValid" => TapError::NotYetValid,
        "Expired" => TapError::Expired,
        "WindowTooLong" => TapError::WindowTooLong,
        "ClockSkewTooLarge" => TapError::ClockSkewTooLarge,
        "UnknownKey" => TapError::UnknownKey,
        "RegistryMismatch" => TapError::RegistryMismatch,
        "Revoked" => TapError::Revoked,
        "ExpiredKey" => TapError::ExpiredKey,
        "AlgorithmMismatch" => TapError::AlgorithmMismatch,
        "InvalidSignature" => TapError::InvalidSignature,
        "Replay" => TapError::Replay,
        other => panic!("{source}: {other} is not a TAP refusal"),
    }
}

fn intent(name: &str, source: &str) -> AgentIntent {
    match name {
        "pay" => AgentIntent::Pay,
        "browse" => AgentIntent::Browse,
        other => panic!("{source}: {other} is not a TAP interaction"),
    }
}

fn signature_base(authority: &str, path: &str, parameters: &str) -> String {
    format!("\"@authority\": {authority}\n\"@path\": {path}\n\"@signature-params\": {parameters}")
}

fn signed_request(signing: &SigningKey, nonce: &str, tag: &str, extension: &str) -> TapRequest {
    let parameters = format!(
        "(\"@authority\" \"@path\");created={};keyid=\"{KEY_ID}\";alg=\"Ed25519\";expires={};nonce=\"{nonce}\";tag=\"{tag}\"{extension}",
        NOW - 1,
        NOW + 479
    );
    let base = signature_base("shop.example", "/checkout", &parameters);
    let signature = STANDARD.encode(signing.sign(base.as_bytes()).to_bytes());
    TapRequest::parse(
        "shop.example",
        "/checkout",
        &format!("sig2={parameters}"),
        &format!("sig2=:{signature}:"),
    )
    .unwrap_or_else(|error| panic!("official-shape request must parse: {error}"))
}

fn registry(signing: &SigningKey, status: KeyStatus, expires_at: u64) -> Registry {
    Registry {
        key: RegisteredAgentKey {
            key_id: KEY_ID.to_owned(),
            agent_id: "visa-agent-7".to_owned(),
            agent_domain: "https://agent.example".to_owned(),
            layerx_agent: Some(AGENT),
            key: AgentPublicKey::Ed25519(signing.verifying_key().to_bytes()),
            status,
            expires_at,
        },
    }
}

fn assert_credential_matches_vector(
    verified: &VerifiedTrustedAgent,
    record: &serde_json::Value,
    layerx_agent: [u8; 32],
    name: &str,
    source: &str,
) -> AgentIntent {
    assert_eq!(
        verified.intent,
        intent(
            record["intent"]
                .as_str()
                .unwrap_or_else(|| panic!("{name}: a verified vector names its intent")),
            source
        ),
        "{name}: interaction"
    );
    assert_eq!(verified.layerx_agent, Some(layerx_agent), "{name}: binding");
    assert_eq!(
        verified.key_id,
        field(record, "registry_key_id", source),
        "{name}: registry key"
    );
    assert_eq!(
        verified.agent_id,
        field(record, "registry_agent_id", source),
        "{name}: registry agent"
    );
    verified.intent
}

#[test]
fn every_credential_vector_verifies_or_is_refused_exactly_as_declared() {
    let source = "interop/specs/conformance/visa-tap/credential-verification.json";
    let mut verified_intents = Vec::new();
    let mut refusals = Vec::new();
    for record in records(CREDENTIAL_VERIFICATION, source) {
        let name = field(&record, "name", source);
        let parameters = field(&record, "signature_params", source);
        let signing =
            SigningKey::from_bytes(&hex32(field(&record, "signing_key_seed", source), source));
        let base = signature_base(
            field(&record, "signed_authority", source),
            field(&record, "signed_path", source),
            parameters,
        );
        let signature = STANDARD.encode(signing.sign(base.as_bytes()).to_bytes());
        let declared = record["expect"]
            .as_str()
            .unwrap_or_else(|| panic!("{source}: {name} declares no outcome"));
        let expected = (declared != "verified").then(|| tap_error(declared, source));
        let request = TapRequest::parse(
            field(&record, "authority", source),
            field(&record, "path", source),
            &format!("sig2={parameters}"),
            &format!("sig2=:{signature}:"),
        );
        let request = match request {
            Ok(request) => request,
            Err(error) => {
                assert_eq!(Some(error), expected, "{name}: signature-input admission");
                refusals.push(error);
                continue;
            }
        };
        let layerx_agent = hex32(field(&record, "registry_layerx_agent", source), source);
        let registry = Registry {
            key: RegisteredAgentKey {
                key_id: field(&record, "registry_key_id", source).to_owned(),
                agent_id: field(&record, "registry_agent_id", source).to_owned(),
                agent_domain: field(&record, "registry_agent_domain", source).to_owned(),
                layerx_agent: Some(layerx_agent),
                key: AgentPublicKey::Ed25519(signing.verifying_key().to_bytes()),
                status: match field(&record, "key_status", source) {
                    "active" => KeyStatus::Active,
                    "revoked" => KeyStatus::Revoked,
                    other => panic!("{source}: {other} is not a registry key status"),
                },
                expires_at: number(&record, "key_expires_at", source),
            },
        };
        let now = number(&record, "now", source);
        let outcome: Result<VerifiedTrustedAgent, TapError> =
            if let Some(skew) = record["clock_skew_seconds"].as_u64() {
                TapVerifier::verify_credential(&request, &registry, now, skew)
            } else {
                let mut nonces = NonceWindow::new();
                for delivery in 0..number(&record, "replays", source) {
                    TapVerifier::verify(&request, &registry, &mut nonces, now).unwrap_or_else(
                        |error| panic!("{name}: delivery {delivery} must verify: {error}"),
                    );
                }
                TapVerifier::verify(&request, &registry, &mut nonces, now)
            };
        if let Some(error) = expected {
            assert_eq!(outcome, Err(error), "{name}");
            refusals.push(error);
        } else {
            let verified = outcome.unwrap_or_else(|error| panic!("{name}: {error}"));
            verified_intents.push(assert_credential_matches_vector(
                &verified,
                &record,
                layerx_agent,
                name,
                source,
            ));
        }
    }
    for interaction in [AgentIntent::Pay, AgentIntent::Browse] {
        assert!(
            verified_intents.contains(&interaction),
            "{source}: {interaction:?} is not covered by a verified vector"
        );
    }
    for error in [
        TapError::Replay,
        TapError::Expired,
        TapError::Revoked,
        TapError::InvalidSignature,
        TapError::NotYetValid,
        TapError::ClockSkewTooLarge,
    ] {
        assert!(
            refusals.contains(&error),
            "{source}: {error} is not covered by a refusal vector"
        );
    }
}

#[test]
fn target_components_require_one_canonical_authority_and_path_representation() {
    let source = "interop/specs/conformance/visa-tap/target-canonicalization.json";
    for record in records(TARGET_CANONICALIZATION, source) {
        let name = field(&record, "name", source);
        let parsed = TapRequest::parse(
            field(&record, "authority", source),
            field(&record, "path", source),
            field(&record, "signature_input", source),
            field(&record, "signature", source),
        );
        let accepted = record["accepted"]
            .as_bool()
            .unwrap_or_else(|| panic!("{source}: {name} declares whether it is admitted"));
        if accepted {
            let request = parsed.unwrap_or_else(|error| panic!("{name}: {error}"));
            assert_eq!(
                request.authority(),
                field(&record, "authority", source),
                "{name}: canonical authority"
            );
            assert_eq!(
                request.path(),
                field(&record, "path", source),
                "{name}: canonical path"
            );
        } else {
            assert_eq!(
                parsed.err(),
                Some(tap_error(field(&record, "refusal", source), source)),
                "{name}"
            );
        }
    }
}

#[test]
fn binding_is_scoped_non_authoritative_and_success_requires_a_real_receipt() {
    let signing = SigningKey::from_bytes(&[0x33; 32]);
    let verified = TapVerifier::verify(
        &signed_request(&signing, "unique-session-4", "agent-payer-auth", ""),
        &registry(&signing, KeyStatus::Active, NOW + 1_000),
        &mut NonceWindow::new(),
        NOW,
    )
    .unwrap_or_else(|error| panic!("credential must verify: {error}"));
    let principal = PrincipalId::new("merchant-1")
        .unwrap_or_else(|error| panic!("principal must parse: {error}"));
    let trace = TraceId::mint([7; 16]);
    let mut bindings = Bindings::default();
    let intent = prepare_trusted_intent(&principal, AGENT, &verified, &mut bindings, &trace)
        .unwrap_or_else(|error| panic!("typed intent must be prepared: {error}"));
    assert_eq!(intent.principal, principal);
    assert_eq!(intent.intent, AgentIntent::Pay);
    assert_eq!(bindings.0.len(), 1);
    assert_eq!(
        bind_verified_agent(&principal, [0x55; 32], &verified, &mut bindings, &trace),
        Err(TapError::LayerxAgentMismatch)
    );
    assert_eq!(
        payment_result(b"not a protocol receipt", &v2_keys(900, 1_000)),
        Err(TapError::ReceiptMismatch)
    );
}

#[test]
fn v2_exact_payment_verifies_against_the_governed_key_list() {
    let receipt = v2_receipt(|_| {});
    let keys = v2_keys(900, 1_000);
    let decoded = verify_sequencer_signature(&receipt, &keys)
        .unwrap_or_else(|error| panic!("canonical v2 vector: {error:?}"));
    let protocol = decoded
        .protocol()
        .unwrap_or_else(|| panic!("v2 vector must be a protocol receipt"));
    assert_eq!(protocol.protocol_version(), 2);
    assert_eq!(protocol.activity_id(), [3; 32]);
    assert_eq!(protocol.global_sequence(), 7);
    assert_eq!(protocol.result_code(), 0);
    assert_eq!(protocol.context_hash(), keys[1].sequencer_id());
    assert_eq!(&protocol.batch_id()[24..], RECEIPT_HEIGHT.to_be_bytes());
    assert_eq!(protocol.effects().len(), 2);
    let expected = Ok(MerchantOperationResult::ReceiptVerified {
        receipt_digest: V2_RECEIPT_DIGEST,
    });
    assert_eq!(payment_result(&receipt, &keys), expected);
    assert_eq!(
        payment_result(&receipt, &v2_keys(RECEIPT_HEIGHT, RECEIPT_HEIGHT)),
        expected,
    );
}

#[test]
fn v2_key_outside_its_inclusive_height_range_is_refused() {
    let receipt = v2_receipt(|_| {});
    for (first, last) in [(951, 1_000), (900, 949), (951, 949)] {
        assert_eq!(
            payment_result(&receipt, &v2_keys(first, last)),
            Err(TapError::ReceiptMismatch),
        );
    }
    let keys = v2_keys(900, 1_000);
    assert_eq!(
        payment_result(&receipt, &[]),
        Err(TapError::ReceiptMismatch)
    );
    assert_eq!(
        payment_result(&receipt, &keys[..1]),
        Err(TapError::ReceiptMismatch)
    );
    assert_eq!(
        payment_result(&receipt, &[keys[1], keys[1]]),
        Err(TapError::ReceiptMismatch),
    );
    let wrong_key =
        SequencerAuthorization::new(keys[1].sequencer_id(), keys[0].public_key(), 900, 1_000);
    assert_eq!(
        payment_result(&receipt, &[wrong_key]),
        Err(TapError::ReceiptMismatch)
    );
}

#[test]
fn v2_payment_facts_must_match_the_signed_ledger_log() {
    let receipt = v2_receipt(|_| {});
    let keys = v2_keys(900, 1_000);
    for (reference, from, to, asset, amount) in [
        (
            [0x67; 32],
            PAYMENT_FROM,
            PAYMENT_TO,
            PAYMENT_ASSET,
            PAYMENT_AMOUNT,
        ),
        (
            PAYMENT_REFERENCE,
            [0x12; 32],
            PAYMENT_TO,
            PAYMENT_ASSET,
            PAYMENT_AMOUNT,
        ),
        (
            PAYMENT_REFERENCE,
            PAYMENT_FROM,
            [0x23; 32],
            PAYMENT_ASSET,
            PAYMENT_AMOUNT,
        ),
        (
            PAYMENT_REFERENCE,
            PAYMENT_FROM,
            PAYMENT_TO,
            [0x56; 32],
            PAYMENT_AMOUNT,
        ),
        (
            PAYMENT_REFERENCE,
            PAYMENT_FROM,
            PAYMENT_TO,
            PAYMENT_ASSET,
            PAYMENT_AMOUNT + 1,
        ),
    ] {
        assert_eq!(
            MerchantOperationResult::from_receipt(
                &receipt, &keys, reference, from, to, asset, amount,
            ),
            Err(TapError::ReceiptMismatch),
        );
    }
}

#[test]
fn v2_invalid_payment_terms_are_refused() {
    let receipt = v2_receipt(|_| {});
    let keys = v2_keys(900, 1_000);
    for (reference, from, to, asset, amount) in [
        (
            [0; 32],
            PAYMENT_FROM,
            PAYMENT_TO,
            PAYMENT_ASSET,
            PAYMENT_AMOUNT,
        ),
        (
            PAYMENT_REFERENCE,
            [0; 32],
            PAYMENT_TO,
            PAYMENT_ASSET,
            PAYMENT_AMOUNT,
        ),
        (
            PAYMENT_REFERENCE,
            PAYMENT_FROM,
            [0; 32],
            PAYMENT_ASSET,
            PAYMENT_AMOUNT,
        ),
        (
            PAYMENT_REFERENCE,
            PAYMENT_FROM,
            PAYMENT_FROM,
            PAYMENT_ASSET,
            PAYMENT_AMOUNT,
        ),
        (
            PAYMENT_REFERENCE,
            PAYMENT_FROM,
            PAYMENT_TO,
            [0; 32],
            PAYMENT_AMOUNT,
        ),
        (
            PAYMENT_REFERENCE,
            PAYMENT_FROM,
            PAYMENT_TO,
            PAYMENT_ASSET,
            0,
        ),
    ] {
        assert_eq!(
            MerchantOperationResult::from_receipt(
                &receipt, &keys, reference, from, to, asset, amount,
            ),
            Err(TapError::ReceiptMismatch),
        );
    }
}

#[test]
fn v2_unverifiable_or_failed_receipts_never_produce_payment_success() {
    let keys = v2_keys(900, 1_000);
    let mut receipt = v2_receipt(|_| {});
    let last = receipt.len() - 1;
    receipt[last] ^= 1;
    assert_eq!(
        payment_result(&receipt, &keys),
        Err(TapError::ReceiptMismatch)
    );
    let mut unsigned = receipt[..receipt.len() - 69].to_vec();
    unsigned.push(0);
    assert_eq!(
        payment_result(&unsigned, &keys),
        Err(TapError::ReceiptMismatch)
    );
    assert_eq!(
        payment_result(&receipt[..42], &keys),
        Err(TapError::ReceiptMismatch)
    );
    assert_eq!(
        payment_result(&vec![0; 1_048_577], &keys),
        Err(TapError::ReceiptMismatch)
    );
    for offset in [
        10, 49, 90, 161, 165, 168, 170, 172, 178, 321, 327, 329, 331, 333, 339, 393, 395, 396, 428,
        460, 492, 524, 576, 608, 613, 617, 891,
    ] {
        let receipt = v2_receipt(|unsigned| unsigned[offset] ^= 1);
        assert_eq!(
            payment_result(&receipt, &keys),
            Err(TapError::ReceiptMismatch),
            "signed mutation at {offset} must be refused",
        );
    }
    for version in [1_u16, 3] {
        let receipt = v2_receipt(|unsigned| {
            unsigned[..2].copy_from_slice(&version.to_be_bytes());
            unsigned[4..6].copy_from_slice(&version.to_be_bytes());
        });
        assert_eq!(
            payment_result(&receipt, &keys),
            Err(TapError::ReceiptMismatch)
        );
    }
}

#[test]
fn v2_requires_one_matching_payment_log() {
    let keys = v2_keys(900, 1_000);
    let no_log = v2_receipt(|unsigned| {
        unsigned[162..166].copy_from_slice(&1_u32.to_be_bytes());
        unsigned[318..322].copy_from_slice(&0_u32.to_be_bytes());
        unsigned.drain(327..556);
    });
    assert_eq!(
        payment_result(&no_log, &keys),
        Err(TapError::ReceiptMismatch)
    );
    let duplicate = v2_receipt(|unsigned| {
        let mut log = unsigned[327..556].to_vec();
        log[2..4].copy_from_slice(&2_u16.to_be_bytes());
        unsigned.splice(556..556, log);
        unsigned[162..166].copy_from_slice(&3_u32.to_be_bytes());
        unsigned[318..322].copy_from_slice(&2_u32.to_be_bytes());
    });
    assert_eq!(
        payment_result(&duplicate, &keys),
        Err(TapError::ReceiptMismatch)
    );
}
