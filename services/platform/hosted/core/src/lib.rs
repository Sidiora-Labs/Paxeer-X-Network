//! Canonical treasury SEND construction shared by the core boundary binary and
//! its real-node tests, together with the treasury signer boundary the binary
//! signs through.

mod custody;

pub use custody::{did_for_public_key, SeedSigner, SendError, SocketSigner, TreasurySigner};

use ed25519_dalek::SigningKey;
use layerx_crypto::signer::SigningRequest;
use layerx_crypto::SignatureMessage;
use layerx_intents::{compile, Intent, IntentKind, LxpSend};
use layerx_types::account::AccountId;
use layerx_types::activity::{Authority, EnvelopeBuilder, Signature, TimestampBound};
use layerx_types::amount::Amount;
use layerx_types::ids::{AssetId, Did, IdempotencyKey};
use layerx_types::intent::{
    AuthorizationSignature, ContextHash, NetworkId, ProtocolVersion, PublicKey, SendAuthorization,
    SendAuthorizationKind, Sequence, TimestampSeconds,
};
use layerx_types::payload::{ActivityType, ModuleId, ModuleRegistration, ModuleRegistry};
use layerx_wire::encode::Encoder;
use layerx_wire::hash::Domain;
use sha2::{Digest as _, Sha256};

/// Asset module ordinal of the canonical SEND activity.
pub const SEND_ACTIVITY: u16 = 5;

/// Every input needed to build one owner-authorised asset SEND.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct SendRequest {
    pub network_id: u32,
    pub source_did: String,
    pub destination_did: String,
    pub asset: [u8; 32],
    pub amount: u128,
    pub account_sequence: u64,
    pub idempotency_key: [u8; 32],
    pub not_before_ms: u64,
    pub expires_at_ms: u64,
    pub fee_limit: u128,
}

/// A fully signed SEND ready for the LNI together with its identifying facts.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct SignedSend {
    pub canonical: Vec<u8>,
    pub activity_id: [u8; 32],
    pub source_account: [u8; 32],
    pub destination_account: [u8; 32],
    pub signer_public_key: [u8; 32],
    pub idempotency_key: [u8; 32],
}

/// Returns the canonical account identifier of `agent:<did>:main`.
///
/// # Errors
///
/// Returns the account-name validation failure as text.
pub fn main_account(did: &str) -> Result<[u8; 32], String> {
    let account = AccountId::parse(&format!("agent:{did}:main"))
        .map_err(|error| format!("account name for {did} is invalid: {error:?}"))?;
    layerx_wire::hash::account_id_for_protocol(
        &account,
        layerx_wire::limits::STATE_COMMITMENT_PROTOCOL_VERSION,
    )
    .map_err(|error| format!("account id for {did} cannot be derived: {error:?}"))
}

/// Builds the asset module registry that declares SEND.
///
/// # Errors
///
/// Returns the registry construction failure as text.
pub fn asset_registry() -> Result<(ModuleRegistry, ActivityType), String> {
    let activity_type = ActivityType::new(ModuleId::Asset, SEND_ACTIVITY)
        .map_err(|error| format!("asset send activity is unavailable: {error:?}"))?;
    let registration = ModuleRegistration::new(ModuleId::Asset, &[activity_type])
        .map_err(|error| format!("asset module registration is invalid: {error:?}"))?;
    let registry = ModuleRegistry::new(&[registration])
        .map_err(|error| format!("asset module registry is invalid: {error:?}"))?;
    Ok((registry, activity_type))
}

/// Computes the send context commitment the core recomputes during execution.
#[must_use]
pub fn send_context_hash(
    source: &[u8; 32],
    destination: &[u8; 32],
    asset: &[u8; 32],
    amount: u128,
    idempotency: &[u8; 32],
) -> [u8; 32] {
    layerx_crypto::send::send_context_hash(source, destination, asset, amount, idempotency)
}

/// Hashes bytes under one canonical wire domain tag.
#[must_use]
pub fn domain_hash(domain: Domain, bytes: &[u8]) -> [u8; 32] {
    let mut digest = Sha256::new();
    digest.update(domain.tag());
    digest.update(bytes);
    digest.finalize().into()
}

/// Builds, compiles with layerx-intents and signs one SEND from a seed held
/// in process.
///
/// # Errors
///
/// Returns every construction, compilation or encoding failure as text.
pub fn build_send(seed: &[u8; 32], request: &SendRequest) -> Result<SignedSend, String> {
    build_send_with_identity_sequence(seed, request.account_sequence, request)
}

/// Builds one SEND from a seed held in process whose actor sequence is
/// independent of its source-account sequence.
///
/// # Errors
///
/// Returns every construction, compilation or encoding failure as text.
pub fn build_send_with_identity_sequence(
    seed: &[u8; 32],
    identity_sequence: u64,
    request: &SendRequest,
) -> Result<SignedSend, String> {
    build_send_with_signer(&SeedSigner::new(seed), identity_sequence, request)
        .map_err(|error| error.to_string())
}

/// Builds one SEND whose two signatures come from `signer`, which may hold the
/// treasury identity in process or reach it over the treasury signer socket.
///
/// # Errors
///
/// Returns [`SendError::Signer`] when the signer refuses or cannot be reached
/// and [`SendError::Invalid`] for every construction, compilation or encoding
/// failure.
pub fn build_send_with_signer(
    signer: &dyn TreasurySigner,
    identity_sequence: u64,
    request: &SendRequest,
) -> Result<SignedSend, SendError> {
    validate_send_request(request)?;
    let from = AccountId::parse(&format!("agent:{}:main", request.source_did))
        .map_err(|error| SendError::Invalid(format!("source account is invalid: {error:?}")))?;
    let to = AccountId::parse(&format!("agent:{}:main", request.destination_did))
        .map_err(|error| SendError::Invalid(format!("destination account is invalid: {error:?}")))?;
    build_send_for_accounts(signer, identity_sequence, request, from, to)
}

fn build_send_for_accounts(
    signer: &dyn TreasurySigner,
    identity_sequence: u64,
    request: &SendRequest,
    from: AccountId,
    to: AccountId,
) -> Result<SignedSend, SendError> {
    validate_send_request(request)?;
    let public_key = signer.public_key();
    let source = layerx_wire::hash::account_id_for_protocol(&from, layerx_wire::limits::STATE_COMMITMENT_PROTOCOL_VERSION)
        .map_err(|error| SendError::Invalid(format!("source account is invalid: {error:?}")))?;
    let destination = layerx_wire::hash::account_id_for_protocol(&to, layerx_wire::limits::STATE_COMMITMENT_PROTOCOL_VERSION)
        .map_err(|error| SendError::Invalid(format!("destination account is invalid: {error:?}")))?;
    let context = send_context_hash(
        &source,
        &destination,
        &request.asset,
        request.amount,
        &request.idempotency_key,
    );
    let authorization = send_authorization(signer, &source, &destination, request, &context)?;
    let intent = LxpSend::new(
        from,
        to,
        AssetId::new(request.asset),
        Amount::from_u128(request.amount),
        Sequence::from_u64(request.account_sequence),
        IdempotencyKey::new(request.idempotency_key),
        TimestampSeconds::from_u64(request.expires_at_ms),
        ContextHash::new(context),
        SendAuthorization::new(
            SendAuthorizationKind::Owner,
            PublicKey::new(public_key),
            AuthorizationSignature::new(authorization),
        ),
        NetworkId::new(request.network_id)
            .map_err(|error| SendError::Invalid(format!("network id is invalid: {error:?}")))?,
        ProtocolVersion::new(layerx_wire::limits::STATE_COMMITMENT_PROTOCOL_VERSION).map_err(
            |error| SendError::Invalid(format!("protocol version is invalid: {error:?}")),
        )?,
    )
    .map_err(|error| SendError::Invalid(format!("send intent is invalid: {error:?}")))?;
    let (registry, activity_type) = asset_registry().map_err(SendError::Invalid)?;
    let compiled = compile(&Intent::v1(IntentKind::LxpSend(intent)), &registry)
        .map_err(|error| SendError::Invalid(format!("send intent does not compile: {error:?}")))?;
    if compiled.activity_type() != activity_type {
        return Err(SendError::Invalid(
            "compiled intent is not an asset send".into(),
        ));
    }
    let actor = Did::new(request.source_did.as_bytes())
        .map_err(|error| SendError::Invalid(format!("source DID is invalid: {error:?}")))?;
    let authority = Authority::owner(&public_key)
        .map_err(|error| SendError::Invalid(format!("owner authority is invalid: {error:?}")))?;
    let timestamp = TimestampBound::new(request.not_before_ms, request.expires_at_ms)
        .map_err(|error| SendError::Invalid(format!("timestamp bound is invalid: {error:?}")))?;
    let mut builder = EnvelopeBuilder::new();
    builder
        .protocol_version(layerx_wire::limits::STATE_COMMITMENT_PROTOCOL_VERSION)
        .and_then(|value| value.network_id(request.network_id))
        .and_then(|value| value.activity_type(activity_type))
        .and_then(|value| value.actor_did(actor))
        .and_then(|value| value.authority(authority))
        .and_then(|value| value.account_sequence(identity_sequence))
        .and_then(|value| value.timestamp_bound(timestamp))
        .and_then(|value| value.idempotency_key(IdempotencyKey::new(request.idempotency_key)))
        .and_then(|value| value.fee_limit(Amount::from_u128(request.fee_limit)))
        .and_then(|value| value.payload_hash(compiled.payload_hash()))
        .and_then(|value| value.payload(compiled.payload().clone()))
        .map_err(|error| SendError::Invalid(format!("send envelope is invalid: {error:?}")))?;
    let unsigned = builder
        .build()
        .map_err(|error| SendError::Invalid(format!("send envelope is incomplete: {error:?}")))?;
    let unsigned_bytes =
        layerx_wire::activity::encode_unsigned_envelope(&unsigned).map_err(|error| {
            SendError::Invalid(format!("send signing bytes are invalid: {error:?}"))
        })?;
    let digest = domain_hash(Domain::SignaturePreimage, &unsigned_bytes);
    let signature = disclosed_signature(signer, &unsigned_bytes, &registry, request.network_id)?;
    layerx_crypto::ed25519::verify_digest(&public_key, &signature, &digest).map_err(|error| {
        SendError::Invalid(format!("send signature does not verify: {error:?}"))
    })?;
    let envelope = unsigned
        .attach_signature(Signature::new(&signature).map_err(|error| {
            SendError::Invalid(format!("send signature is invalid: {error:?}"))
        })?);
    let canonical = layerx_wire::activity::encode_signed_envelope(&envelope)
        .map_err(|error| SendError::Invalid(format!("signed send is invalid: {error:?}")))?;
    let decoded = layerx_wire::activity::decode_signed(&canonical, &registry)
        .map_err(|error| SendError::Invalid(format!("signed send does not decode: {error:?}")))?;
    let activity_id = layerx_wire::hash::activity_id(&decoded)
        .map_err(|error| SendError::Invalid(format!("send activity id is invalid: {error:?}")))?;
    Ok(SignedSend {
        canonical,
        activity_id,
        source_account: source,
        destination_account: destination,
        signer_public_key: public_key,
        idempotency_key: request.idempotency_key,
    })
}

fn validate_send_request(request: &SendRequest) -> Result<(), SendError> {
    if request.amount == 0 {
        return Err(SendError::Invalid(
            "amount must be greater than zero".into(),
        ));
    }
    if request.expires_at_ms <= request.not_before_ms {
        return Err(SendError::Invalid(
            "expiry must follow the validity start".into(),
        ));
    }
    Ok(())
}

fn disclosed_signature(
    signer: &dyn TreasurySigner,
    canonical: &[u8],
    registry: &ModuleRegistry,
    network_id: u32,
) -> Result<[u8; 64], SendError> {
    let disclosure = layerx_crypto::disclosure::bind(canonical, registry)
        .map_err(|error| SendError::Invalid(format!("send disclosure is invalid: {error:?}")))?;
    let message = SignatureMessage::new(
        Domain::SignaturePreimage,
        layerx_wire::limits::STATE_COMMITMENT_PROTOCOL_VERSION,
        network_id,
        canonical,
    )
    .map_err(|error| SendError::Invalid(format!("send signing scope is invalid: {error:?}")))?;
    let request = SigningRequest::new(message, &disclosure)
        .map_err(|error| SendError::Invalid(format!("send disclosure does not bind: {error:?}")))?;
    let signature = signer
        .sign_digest(&request.message().digest())
        .map_err(SendError::Signer)?;
    layerx_crypto::ed25519::verify(&signer.public_key(), &signature, request.message()).map_err(
        |error| SendError::Signer(format!("treasury signature does not verify: {error:?}")),
    )?;
    Ok(signature)
}

fn send_authorization(
    signer: &dyn TreasurySigner,
    source: &[u8; 32],
    destination: &[u8; 32],
    request: &SendRequest,
    context: &[u8; 32],
) -> Result<[u8; 64], SendError> {
    let mut authorization = Encoder::new(512);
    authorization
        .u16(0x5301)
        .and_then(|()| authorization.fixed(source))
        .and_then(|()| authorization.fixed(destination))
        .and_then(|()| authorization.fixed(&request.asset))
        .and_then(|()| authorization.u128(request.amount))
        .and_then(|()| authorization.u64(request.account_sequence))
        .and_then(|()| authorization.fixed(&request.idempotency_key))
        .and_then(|()| authorization.u64(request.expires_at_ms))
        .and_then(|()| authorization.fixed(context))
        .and_then(|()| authorization.u8(0))
        .and_then(|()| authorization.u8(SendAuthorizationKind::Owner as u8))
        .and_then(|()| authorization.fixed(source))
        .and_then(|()| authorization.fixed(context))
        .and_then(|()| authorization.u32(request.network_id))
        .and_then(|()| authorization.u16(layerx_wire::limits::STATE_COMMITMENT_PROTOCOL_VERSION))
        .map_err(|error| {
            SendError::Invalid(format!("send authorization is too large: {error:?}"))
        })?;
    let digest = domain_hash(Domain::SignaturePreimage, &authorization.finish());
    signer.sign_digest(&digest).map_err(SendError::Signer)
}

/// Encodes bytes as lowercase hexadecimal.
#[must_use]
pub fn hex_encode(bytes: &[u8]) -> String {
    const DIGITS: &[u8; 16] = b"0123456789abcdef";
    let mut text = String::with_capacity(bytes.len() * 2);
    for byte in bytes {
        text.push(char::from(DIGITS[usize::from(byte >> 4)]));
        text.push(char::from(DIGITS[usize::from(byte & 0x0f)]));
    }
    text
}

/// Decodes hexadecimal text of any case into bytes.
///
/// # Errors
///
/// Returns a description when the text is odd-length or not hexadecimal.
pub fn hex_decode(text: &str) -> Result<Vec<u8>, String> {
    if !text.len().is_multiple_of(2) {
        return Err("hex text has odd length".into());
    }
    let mut bytes = Vec::with_capacity(text.len() / 2);
    let raw = text.as_bytes();
    for pair in raw.chunks(2) {
        let high = hex_nibble(pair[0])?;
        let low = hex_nibble(pair[1])?;
        bytes.push((high << 4) | low);
    }
    Ok(bytes)
}

/// Decodes exactly `N` bytes of hexadecimal text.
///
/// # Errors
///
/// Returns a description naming the expected width on mismatch.
pub fn fixed_hex<const N: usize>(name: &str, text: &str) -> Result<[u8; N], String> {
    let bytes = hex_decode(text).map_err(|error| format!("{name}: {error}"))?;
    <[u8; N]>::try_from(bytes).map_err(|_| format!("{name} must be {N} bytes of hex"))
}

fn hex_nibble(byte: u8) -> Result<u8, String> {
    match byte {
        b'0'..=b'9' => Ok(byte - b'0'),
        b'a'..=b'f' => Ok(byte - b'a' + 10),
        b'A'..=b'F' => Ok(byte - b'A' + 10),
        _ => Err("hex text contains a non-hexadecimal character".into()),
    }
}

/// Derives the beta treasury DID `did:layerx:<public key hex>` from a seed.
#[must_use]
pub fn treasury_did(seed: &[u8; 32]) -> String {
    did_for_public_key(&SigningKey::from_bytes(seed).verifying_key().to_bytes())
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct RegisteredCustodyAsset {
    asset: [u8; 32],
    profile: [u8; 223],
    network_id: u32,
    state_root: [u8; 32],
}

impl RegisteredCustodyAsset {
    #[must_use]
    pub const fn asset(&self) -> [u8; 32] { self.asset }
    #[must_use]
    pub const fn profile(&self) -> &[u8; 223] { &self.profile }
    #[must_use]
    pub fn profile_hash(&self) -> [u8; 32] { Sha256::digest(self.profile).into() }
    #[must_use]
    pub fn trusted_height(&self) -> u64 { custody_u64(&self.profile[161..169]) }
    pub fn account_name(&self, did: &str) -> Result<String, String> {
        let name = format!("agent:{did}:asset:{}", hex_encode(&self.asset));
        AccountId::parse(&name).map_err(|error| format!("asset account is invalid: {error:?}"))?;
        Ok(name)
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct AuthenticatedCustodyAsset {
    registered: RegisteredCustodyAsset,
    source_did: String,
    source_account: [u8; 32],
    account_sequence: u64,
    trusted_height: u64,
}

impl AuthenticatedCustodyAsset {
    #[must_use]
    pub const fn asset(&self) -> [u8; 32] { self.registered.asset() }
    #[must_use]
    pub const fn profile(&self) -> &[u8; 223] { self.registered.profile() }
    #[must_use]
    pub const fn trusted_height(&self) -> u64 { self.trusted_height }
    #[must_use]
    pub const fn account_sequence(&self) -> u64 { self.account_sequence }
    #[must_use]
    pub const fn state_root(&self) -> [u8; 32] { self.registered.state_root }
    pub fn account_name(&self, did: &str) -> Result<String, String> { self.registered.account_name(did) }
}

pub fn read_custody_profile(
    transport: &mut dyn layerx_client::lni::transport::FrameTransport,
    context: layerx_client::read::ReadContext,
    asset: [u8; 32],
    effective: &layerx_client::evidence::VerifiedEffectiveAsset,
) -> Result<RegisteredCustodyAsset, String> {
    use layerx_client::evidence::RootSelector;
    use layerx_types::verify::VerificationLevel;
    if context.expected_protocol_version != 3 || context.root_selector != RootSelector::Latest
        || context.requested.level() < VerificationLevel::STATE_PROVEN {
        return Err("custody registry requires current state-proven protocol3 reads".into());
    }
    let mut state_root = None;
    let marker = custody_read(transport, context, &mut state_root, 8, &custody_key(b"LX:CUSTODY:REGISTRY:v1", None))?;
    if marker != b"LXBR1" { return Err("custody registry marker is invalid".into()); }
    let mut selected = None;
    let mut first: Option<[u8; 223]> = None;
    for symbol in ["PAX", "SID", "USDC", "USDL"] {
        let id: [u8; 32] = Sha256::digest(format!("layerx-asset:125:{symbol}").as_bytes()).into();
        let bytes = custody_read(transport, context, &mut state_root, 8, &custody_key(b"LX:CUSTODY:PROFILE:v2", Some(&id)))?;
        let profile: [u8; 223] = bytes.try_into().map_err(|_| "custody profile length is invalid")?;
        validate_custody_profile(&profile, &id, symbol, context.expected_network_id)?;
        if first.as_ref().is_some_and(|base| base[5..97] != profile[5..97] || base[161..223] != profile[161..223]) {
            return Err("custody registry contains incompatible domain or trust".into());
        }
        if first.is_none() { first = Some(profile); }
        if id == asset { selected = Some((profile, symbol)); }
    }
    let (profile, symbol) = selected.ok_or("asset is not in the custody registry")?;
    if effective.asset_id() != asset || effective.level() < VerificationLevel::STATE_PROVEN
        || !effective.registered() || effective.paused()
        || Some(effective.state_root()) != state_root
        || effective.freshness().global_sequence != context.head.chain_sequence
        || effective.freshness().batch_number != context.head.sealed_batch {
        return Err("effective custody metadata differs from the authenticated current registry".into());
    }
    validate_custody_metadata(effective.canonical_bytes(), &asset, symbol)?;
    Ok(RegisteredCustodyAsset { asset, profile, network_id: context.expected_network_id, state_root: state_root.ok_or("custody state root missing")? })
}

pub fn read_custody_asset(
    transport: &mut dyn layerx_client::lni::transport::FrameTransport,
    context: layerx_client::read::ReadContext,
    asset: [u8; 32],
    source_did: &str,
    effective: &layerx_client::evidence::VerifiedEffectiveAsset,
) -> Result<AuthenticatedCustodyAsset, String> {
    let registered = read_custody_profile(transport, context, asset, effective)?;
    let mut state_root = Some(registered.state_root);
    let trust = custody_read(transport, context, &mut state_root, 8, &custody_key(b"LX:CUSTODY:TRUST:v2", Some(&asset)))?;
    if trust.len() != 89 || &trust[..5] != b"LXLT1" {
        return Err("custody progression trust is unavailable or malformed".into());
    }
    let height = custody_u64(&trust[5..13]);
    let seconds = custody_u64(&trust[77..85]);
    let nanos = u32::from_be_bytes(trust[85..89].try_into().map_err(|_| "trust nanos")?);
    if height <= registered.trusted_height() || height >= i64::MAX as u64
        || seconds == 0 || seconds > i64::MAX as u64 || nanos >= 1_000_000_000
        || trust[13..45].iter().all(|byte| *byte == 0) || trust[45..77].iter().all(|byte| *byte == 0) {
        return Err("custody progression trust is invalid".into());
    }
    let name = registered.account_name(source_did)?;
    let account = AccountId::parse(&name).map_err(|error| format!("source asset account: {error:?}"))?;
    let source_account = layerx_wire::hash::account_id_for_protocol(&account, 3)
        .map_err(|error| format!("source asset account id: {error:?}"))?;
    let value = layerx_client::read::account(transport, source_account, context)
        .map_err(|error| format!("source asset account evidence: {error:?}"))?;
    let evidence = layerx_client::evidence::verify_account_evidence(value.canonical_bytes(), value.proof_material(), source_account, None, custody_policy(context))
        .map_err(|error| format!("source account proof binding: {error:?}"))?;
    if evidence.state_root() != registered.state_root { return Err("source account and custody registry roots differ".into()); }
    let committed = layerx_proof::state::decode_account_value(source_account, value.canonical_bytes())
        .map_err(|error| format!("source asset account state: {error:?}"))?;
    if committed.name != name.as_bytes() || committed.asset_id() != asset || committed.frozen {
        return Err("source asset account is unavailable for Send".into());
    }
    Ok(AuthenticatedCustodyAsset { registered, source_did: source_did.into(), source_account,
        account_sequence: committed.next_sequence, trusted_height: height })
}

fn custody_read(
    transport: &mut dyn layerx_client::lni::transport::FrameTransport,
    context: layerx_client::read::ReadContext,
    state_root: &mut Option<[u8; 32]>,
    module: u16,
    key: &[u8],
) -> Result<Vec<u8>, String> {
    let value = layerx_client::read::module_state(transport, module, key, context)
        .map_err(|error| format!("custody state evidence is unavailable: {error:?}"))?;
    let evidence = layerx_client::evidence::verify_module_evidence(value.canonical_bytes(), value.proof_material(), module, key, custody_policy(context))
        .map_err(|error| format!("custody root binding: {error:?}"))?;
    if state_root.is_some_and(|root| root != evidence.state_root()) { return Err("custody registry roots differ".into()); }
    *state_root = Some(evidence.state_root());
    Ok(value.canonical_bytes().to_vec())
}

fn custody_key(domain: &[u8], asset: Option<&[u8; 32]>) -> [u8; 32] {
    let mut digest = Sha256::new(); digest.update(domain);
    if let Some(asset) = asset { digest.update(asset); }
    digest.finalize().into()
}

fn custody_u64(bytes: &[u8]) -> u64 {
    bytes.iter().fold(0, |value, byte| (value << 8) | u64::from(*byte))
}

fn validate_custody_profile(profile: &[u8; 223], asset: &[u8; 32], symbol: &str, network: u32) -> Result<(), String> {
    let mut module = Sha256::new(); module.update(b"LX:CUSTODY:MODULE:v1layerxcustody"); module.update(&profile[13..33]);
    let module_id: [u8; 32] = module.finalize().into();
    let reserve = AccountId::parse(&format!("system:paxeer-reserve:{}", symbol.to_lowercase()))
        .map_err(|error| format!("custody reserve account: {error:?}"))?;
    let reserve_id = layerx_wire::hash::account_id_for_protocol(&reserve, 3)
        .map_err(|error| format!("custody reserve id: {error:?}"))?;
    let chain = &profile[169..201];
    let chain_length = chain.iter().position(|byte| *byte == 0).unwrap_or(chain.len());
    if &profile[..5] != b"LXBC4" || custody_u64(&profile[5..13]) != 125
        || profile[13..33].iter().all(|byte| *byte == 0) || profile[33..65] != module_id
        || profile[65..97].iter().all(|byte| *byte == 0) || profile[97..129] != *asset
        || profile[129..161] != reserve_id || custody_u64(&profile[161..169]) == 0
        || custody_u64(&profile[161..169]) >= i64::MAX as u64 || chain_length == 0
        || !chain[..chain_length].iter().all(|byte| (0x21..=0x7e).contains(byte))
        || chain[chain_length..].iter().any(|byte| *byte != 0)
        || profile[201..205] != network.to_be_bytes() || profile[205..207] != [0,3]
        || custody_u64(&profile[207..215]) == 0 || custody_u64(&profile[207..215]) > u64::from(u32::MAX)
        || custody_u64(&profile[215..223]) == 0 || custody_u64(&profile[215..223]) > 253_402_300_799 {
        return Err("custody profile asset, domain, reserve or trust metadata is invalid".into());
    }
    Ok(())
}

fn validate_custody_metadata(bytes: &[u8], asset: &[u8; 32], symbol: &str) -> Result<(), String> {
    if bytes.len() < 140 || bytes[..2] != [0,3] || bytes[2..34] != *asset { return Err("registered asset metadata is malformed".into()); }
    let size = usize::from(bytes[34]);
    if size != symbol.len() || bytes.get(35..35+size) != Some(symbol.as_bytes()) { return Err("registered asset symbol mismatch".into()); }
    let at = 35 + size;
    let prefix = bytes.get(at..at+4).ok_or("registered asset metadata is truncated")?;
    let reference_length = usize::from(u16::from_be_bytes([prefix[2],prefix[3]]));
    let reference = bytes.get(at+4..at+4+reference_length).ok_or("custody metadata is truncated")?;
    let at = at+4+reference_length;
    let flags = bytes.get(at..at+2).ok_or("registered asset flags are missing")?;
    let name_length = usize::from(flags[1]);
    let tail = bytes.get(at+2+name_length..).ok_or("registered asset name is truncated")?;
    if prefix[0] > 38 || prefix[1] != 2 || reference_length == 0 || reference_length > 128
        || reference.iter().all(|byte| *byte == 0) || flags[0] != 0 || name_length == 0 || name_length > 32
        || tail.len() != 97 || tail[48] != 2 || tail[16..48].iter().all(|byte| *byte == 0)
        || tail[65..97].iter().all(|byte| *byte == 0) {
        return Err("registered asset is paused or approved custody metadata is unavailable".into());
    }
    Ok(())
}

pub fn build_asset_send_with_identity_sequence(
    seed: &[u8; 32], identity_sequence: u64, request: &SendRequest, asset: &AuthenticatedCustodyAsset,
) -> Result<SignedSend, String> {
    build_asset_send_with_signer(&SeedSigner::new(seed), identity_sequence, request, asset).map_err(|error| error.to_string())
}

pub fn build_asset_send_with_signer(
    signer: &dyn TreasurySigner, identity_sequence: u64, request: &SendRequest, asset: &AuthenticatedCustodyAsset,
) -> Result<SignedSend, SendError> {
    if request.asset != asset.asset() || request.network_id != asset.registered.network_id
        || request.source_did != asset.source_did || request.account_sequence != asset.account_sequence
        || did_for_public_key(&signer.public_key()) != request.source_did {
        return Err(SendError::Invalid("asset Send differs from authenticated registry, source or owner".into()));
    }
    let from = AccountId::parse(&asset.account_name(&request.source_did).map_err(SendError::Invalid)?)
        .map_err(|error| SendError::Invalid(format!("source asset account: {error:?}")))?;
    let to = AccountId::parse(&asset.account_name(&request.destination_did).map_err(SendError::Invalid)?)
        .map_err(|error| SendError::Invalid(format!("destination asset account: {error:?}")))?;
    let signed = build_send_for_accounts(signer, identity_sequence, request, from, to)?;
    if signed.source_account != asset.source_account { return Err(SendError::Invalid("source asset account substitution".into())); }
    Ok(signed)
}

fn custody_policy(context: layerx_client::read::ReadContext) -> layerx_client::evidence::AccountEvidencePolicy {
    layerx_client::evidence::AccountEvidencePolicy { expected_protocol_version: context.expected_protocol_version, expected_network_id: context.expected_network_id, handshake_sequencer_key: context.handshake_sequencer_key, root_selector: context.root_selector }
}
