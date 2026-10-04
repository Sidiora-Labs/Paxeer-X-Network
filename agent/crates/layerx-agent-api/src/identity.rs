//! Agent identity, session, capability, and budget contract types.

use crate::verify::Level;
use crate::write_contract::{PreparationRef, SignatureBytes};
use crate::{Amount, BudgetLimit, Sequence, TimestampSeconds};

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum TenantRecoveryReason {
    RecoveryPending,
    StoreUnavailable,
    StoreRefused,
    BudgetStateUnverified,
    ReceiptEvidenceMissing,
    DurableRecoveryFailed,
    SpendUnreconciled,
    TransportUnavailable,
    VerifiedReadUnavailable,
}

impl TenantRecoveryReason {
    pub const fn as_str(self) -> &'static str {
        match self {
            Self::RecoveryPending => "recovery_pending",
            Self::StoreUnavailable => "store_unavailable",
            Self::StoreRefused => "store_refused",
            Self::BudgetStateUnverified => "budget_state_unverified",
            Self::ReceiptEvidenceMissing => "receipt_evidence_missing",
            Self::DurableRecoveryFailed => "durable_recovery_failed",
            Self::SpendUnreconciled => "spend_unreconciled",
            Self::TransportUnavailable => "transport_unavailable",
            Self::VerifiedReadUnavailable => "verified_read_unavailable",
        }
    }

    pub fn parse(value: &str) -> Option<Self> {
        match value {
            "recovery_pending" => Some(Self::RecoveryPending),
            "store_unavailable" => Some(Self::StoreUnavailable),
            "store_refused" => Some(Self::StoreRefused),
            "budget_state_unverified" => Some(Self::BudgetStateUnverified),
            "receipt_evidence_missing" => Some(Self::ReceiptEvidenceMissing),
            "durable_recovery_failed" => Some(Self::DurableRecoveryFailed),
            "spend_unreconciled" => Some(Self::SpendUnreconciled),
            "transport_unavailable" => Some(Self::TransportUnavailable),
            "verified_read_unavailable" => Some(Self::VerifiedReadUnavailable),
            _ => None,
        }
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct TenantReadiness {
    pub transport_ready: bool,
    pub verified_reads_ready: bool,
    pub writes_admitted: bool,
    pub recovery_reason: Option<TenantRecoveryReason>,
}

impl TenantReadiness {
    pub fn validate(&self) -> Result<(), ContractError> {
        if self.writes_admitted != self.recovery_reason.is_none()
            || (self.writes_admitted && (!self.transport_ready || !self.verified_reads_ready))
            || (self.verified_reads_ready && !self.transport_ready)
        {
            return Err(ContractError::Mismatch("tenant_readiness"));
        }
        Ok(())
    }
}
/// Contract construction failure before a request can cross the daemon boundary.
#[derive(Clone, Debug, Eq, PartialEq)]
pub enum ContractError {
    Empty(&'static str),
    Zero(&'static str),
    DaemonLimitFunding,
    /// The value is present but not in its single canonical form.
    Malformed(&'static str),
    /// The value lies outside the bound the producing authority accepts.
    OutOfRange(&'static str),
    /// Two values that must describe the same observation disagree.
    Mismatch(&'static str),
}

/// Decodes exactly 64 lowercase hexadecimal characters into 32 bytes.
///
/// # Errors
/// Returns [`ContractError::Malformed`] for any other length or character.
pub fn strict_hex32(text: &str, field: &'static str) -> Result<[u8; 32], ContractError> {
    let bytes = text.as_bytes();
    if bytes.len() != 64 {
        return Err(ContractError::Malformed(field));
    }
    let nibble = |byte: u8| match byte {
        b'0'..=b'9' => Ok(byte - b'0'),
        b'a'..=b'f' => Ok(byte - b'a' + 10),
        _ => Err(ContractError::Malformed(field)),
    };
    let mut output = [0_u8; 32];
    for (slot, pair) in output.iter_mut().zip(bytes.chunks_exact(2)) {
        *slot = (nibble(pair[0])? << 4) | nibble(pair[1])?;
    }
    Ok(output)
}

macro_rules! required_text {
    ($name:ident, $field:literal) => {
        #[derive(Clone, Debug, Eq, Hash, Ord, PartialEq, PartialOrd)]
        pub struct $name(String);

        impl $name {
            /// Constructs a non-empty contract identifier.
            ///
            /// # Errors
            /// Returns [`ContractError::Empty`] when the required value is empty.
            pub fn new(value: impl Into<String>) -> Result<Self, ContractError> {
                let value = value.into();
                if value.is_empty() {
                    return Err(ContractError::Empty($field));
                }
                Ok(Self(value))
            }

            #[must_use]
            pub fn as_str(&self) -> &str {
                &self.0
            }
        }
    };
}

required_text!(TenantId, "tenant");
required_text!(AgentDid, "agent_did");
required_text!(AuthorityRef, "authority_ref");
required_text!(ClientId, "client");
required_text!(PolicyVersion, "policy_version");
required_text!(SessionId, "session_id");
required_text!(CapabilityId, "capability_id");
required_text!(BudgetId, "budget_id");
required_text!(Counterparty, "counterparty");
required_text!(Asset, "asset");
required_text!(Purpose, "purpose");

impl CapabilityId {
    /// Returns the 32-byte owner-issued identifier named by this text.
    ///
    /// # Errors
    /// Returns [`ContractError::Malformed`] unless the text is 64 lowercase hex characters.
    pub fn to_bytes(&self) -> Result<[u8; 32], ContractError> {
        strict_hex32(self.as_str(), "capability_id")
    }
}

impl SessionId {
    /// Returns the 32-byte session identifier named by this text.
    ///
    /// # Errors
    /// Returns [`ContractError::Malformed`] unless the text is 64 lowercase hex characters.
    pub fn to_bytes(&self) -> Result<[u8; 32], ContractError> {
        strict_hex32(self.as_str(), "session_id")
    }
}

#[derive(Clone, Copy, Debug, Eq, Hash, Ord, PartialEq, PartialOrd)]
pub struct ActivityType(pub u16);

#[derive(Clone, Copy, Debug, Eq, Hash, Ord, PartialEq, PartialOrd)]
pub struct NativeActivity {
    pub module: u16,
    pub ordinal: u16,
}

impl NativeActivity {
    pub const VERSION: u8 = 1;

    pub fn new(module: u16, ordinal: u16) -> Result<Self, ContractError> {
        Self { module, ordinal }.validate()
    }

    pub fn validate(self) -> Result<Self, ContractError> {
        self.activity_type()?;
        Ok(self)
    }

    pub fn activity_type(self) -> Result<layerx_types::payload::ActivityType, ContractError> {
        let module = layerx_types::payload::ModuleId::from_u16(self.module)
            .map_err(|_| ContractError::Malformed("native_activity.module"))?;
        layerx_types::payload::ActivityType::new(module, self.ordinal)
            .map_err(|_| ContractError::Zero("native_activity.ordinal"))
    }

    pub fn encode(self) -> Result<[u8; 5], ContractError> {
        self.validate()?;
        let module = self.module.to_be_bytes();
        let ordinal = self.ordinal.to_be_bytes();
        Ok([Self::VERSION, module[0], module[1], ordinal[0], ordinal[1]])
    }

    pub fn decode(bytes: &[u8]) -> Result<Self, ContractError> {
        let bytes: &[u8; 5] = bytes
            .try_into()
            .map_err(|_| ContractError::Malformed("native_activity.length"))?;
        if bytes[0] != Self::VERSION {
            return Err(ContractError::Malformed("native_activity.version"));
        }
        Self::new(
            u16::from_be_bytes([bytes[1], bytes[2]]),
            u16::from_be_bytes([bytes[3], bytes[4]]),
        )
    }
}

impl From<layerx_types::payload::ActivityType> for NativeActivity {
    fn from(value: layerx_types::payload::ActivityType) -> Self {
        Self {
            module: value.module() as u16,
            ordinal: value.ordinal(),
        }
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct NativePreparationPurposeV1 {
    pub tenant: TenantId,
    pub agent_did: AgentDid,
    pub session_id: SessionId,
    pub generation: u64,
    pub expires_at_ms: u64,
    pub capability_id: CapabilityId,
    pub preparation_id: [u8; 32],
    pub canonical_digest: [u8; 32],
    pub commitment: [u8; 32],
}

impl NativePreparationPurposeV1 {
    pub const VERSION: u8 = 1;
    pub const DOMAIN: &'static [u8] = b"LayerX/native/preparation-purpose/v1\0";

    pub fn validate(self) -> Result<Self, ContractError> {
        self.validate_fields()?;
        Ok(self)
    }

    fn validate_fields(&self) -> Result<(), ContractError> {
        let tenant = self.tenant.as_str().as_bytes();
        if tenant.is_empty() || tenant.len() > 255 || tenant.contains(&0) {
            return Err(ContractError::Malformed("tenant"));
        }
        layerx_types::ids::Did::new(self.agent_did.as_str().as_bytes())
            .map_err(|_| ContractError::Malformed("agent_did"))?;
        self.session_id.to_bytes()?;
        self.capability_id.to_bytes()?;
        if self.generation == 0 {
            return Err(ContractError::Zero("generation"));
        }
        if self.expires_at_ms == 0 {
            return Err(ContractError::Zero("expires_at_ms"));
        }
        Ok(())
    }

    pub fn from_canonical_bytes(bytes: &[u8]) -> Result<Self, ContractError> {
        let rest = bytes
            .strip_prefix(Self::DOMAIN)
            .ok_or(ContractError::Malformed("native_purpose.domain"))?;
        let (version, rest) = rest
            .split_first()
            .ok_or(ContractError::Malformed("native_purpose.version"))?;
        if *version != Self::VERSION {
            return Err(ContractError::Malformed("native_purpose.version"));
        }
        let (tenant, rest) = native_purpose_text(rest, 255, "tenant")?;
        let (agent, rest) =
            native_purpose_text(rest, layerx_types::limits::MAX_DID_BYTES, "agent_did")?;
        if rest.len() != 176 {
            return Err(ContractError::Malformed("native_purpose.length"));
        }
        let session: [u8; 32] = native_purpose_fixed(&rest[..32])?;
        let capability: [u8; 32] = native_purpose_fixed(&rest[48..80])?;
        let value = Self {
            tenant: TenantId::new(tenant)?,
            agent_did: AgentDid::new(agent)?,
            session_id: SessionId::new(native_purpose_hex32(&session))?,
            generation: u64::from_be_bytes(native_purpose_fixed(&rest[32..40])?),
            expires_at_ms: u64::from_be_bytes(native_purpose_fixed(&rest[40..48])?),
            capability_id: CapabilityId::new(native_purpose_hex32(&capability))?,
            preparation_id: native_purpose_fixed(&rest[80..112])?,
            canonical_digest: native_purpose_fixed(&rest[112..144])?,
            commitment: native_purpose_fixed(&rest[144..176])?,
        }
        .validate()?;
        if value.canonical_bytes()?.as_slice() != bytes {
            return Err(ContractError::Malformed("native_purpose.canonical_bytes"));
        }
        Ok(value)
    }

    pub fn canonical_bytes(&self) -> Result<Vec<u8>, ContractError> {
        self.validate_fields()?;
        let mut bytes = Self::DOMAIN.to_vec();
        bytes.push(Self::VERSION);
        for (field, value) in [
            ("tenant", self.tenant.as_str()),
            ("agent_did", self.agent_did.as_str()),
        ] {
            let length =
                u32::try_from(value.len()).map_err(|_| ContractError::OutOfRange(field))?;
            bytes.extend_from_slice(&length.to_be_bytes());
            bytes.extend_from_slice(value.as_bytes());
        }
        bytes.extend_from_slice(&self.session_id.to_bytes()?);
        bytes.extend_from_slice(&self.generation.to_be_bytes());
        bytes.extend_from_slice(&self.expires_at_ms.to_be_bytes());
        bytes.extend_from_slice(&self.capability_id.to_bytes()?);
        bytes.extend_from_slice(&self.preparation_id);
        bytes.extend_from_slice(&self.canonical_digest);
        bytes.extend_from_slice(&self.commitment);
        Ok(bytes)
    }
}

fn native_purpose_text<'a>(
    bytes: &'a [u8],
    maximum: usize,
    field: &'static str,
) -> Result<(&'a str, &'a [u8]), ContractError> {
    let (length, rest) = bytes
        .split_first_chunk::<4>()
        .ok_or(ContractError::Malformed(field))?;
    let length = usize::try_from(u32::from_be_bytes(*length))
        .map_err(|_| ContractError::OutOfRange(field))?;
    if length == 0 || length > maximum {
        return Err(ContractError::OutOfRange(field));
    }
    let (text, rest) = rest
        .split_at_checked(length)
        .ok_or(ContractError::Malformed(field))?;
    let text = core::str::from_utf8(text).map_err(|_| ContractError::Malformed(field))?;
    Ok((text, rest))
}

fn native_purpose_fixed<const N: usize>(bytes: &[u8]) -> Result<[u8; N], ContractError> {
    bytes
        .try_into()
        .map_err(|_| ContractError::Malformed("native_purpose.length"))
}

fn native_purpose_hex32(bytes: &[u8; 32]) -> String {
    const HEX: &[u8; 16] = b"0123456789abcdef";
    let mut text = String::with_capacity(64);
    for byte in bytes {
        text.push(char::from(HEX[usize::from(byte >> 4)]));
        text.push(char::from(HEX[usize::from(byte & 15)]));
    }
    text
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct SignedNativePreparationPurposeV1 {
    pub purpose: NativePreparationPurposeV1,
    pub owner_public_key: [u8; 32],
    pub signature: [u8; 64],
}

impl SignedNativePreparationPurposeV1 {
    pub fn validate(self) -> Result<Self, ContractError> {
        Ok(Self {
            purpose: self.purpose.validate()?,
            owner_public_key: self.owner_public_key,
            signature: self.signature,
        })
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct NativeSendPurposeV1 {
    pub tenant: TenantId,
    pub agent_did: AgentDid,
    pub owner_did: AgentDid,
    pub owner_public_key: [u8; 32],
    pub session_id: SessionId,
    pub generation: u64,
    pub expires_at_ms: u64,
    pub capability_id: CapabilityId,
    pub protocol_version: u16,
    pub network_id: u32,
    pub activity: NativeActivity,
    pub preparation_id: [u8; 32],
    pub canonical_digest: [u8; 32],
    pub economic_action: [u8; 32],
    pub idempotency_key: [u8; 32],
    pub commitment: [u8; 32],
}

impl NativeSendPurposeV1 {
    pub const VERSION: u8 = 1;
    pub const DOMAIN: &'static [u8] = b"LayerX/native/send-purpose/v1\0";

    pub fn validate(self) -> Result<Self, ContractError> {
        self.validate_fields()?;
        Ok(self)
    }

    fn validate_fields(&self) -> Result<(), ContractError> {
        let tenant = self.tenant.as_str().as_bytes();
        if tenant.is_empty() || tenant.len() > 255 || tenant.contains(&0) {
            return Err(ContractError::Malformed("tenant"));
        }
        for did in [&self.agent_did, &self.owner_did] {
            if did.as_str().as_bytes().contains(&0) {
                return Err(ContractError::Malformed("native_send.did"));
            }
            layerx_types::ids::Did::new(did.as_str().as_bytes())
                .map_err(|_| ContractError::Malformed("native_send.did"))?;
        }
        let expected_owner = format!(
            "did:layerx:{}",
            native_purpose_hex32(&self.owner_public_key)
        );
        if self.owner_public_key == [0; 32] || self.owner_did.as_str() != expected_owner {
            return Err(ContractError::Mismatch("native_send.owner"));
        }
        self.session_id.to_bytes()?;
        self.capability_id.to_bytes()?;
        if self.generation == 0 || self.expires_at_ms == 0 {
            return Err(ContractError::Zero("native_send.lifetime"));
        }
        if self.protocol_version != 3
            || self.network_id == 0
            || self.activity != NativeActivity::new(1, 5)?
        {
            return Err(ContractError::Mismatch("native_send.domain"));
        }
        for (name, value) in [
            ("preparation_id", self.preparation_id),
            ("canonical_digest", self.canonical_digest),
            ("economic_action", self.economic_action),
            ("idempotency_key", self.idempotency_key),
            ("commitment", self.commitment),
        ] {
            if value == [0; 32] {
                return Err(ContractError::Zero(name));
            }
        }
        Ok(())
    }

    pub fn canonical_bytes(&self) -> Result<Vec<u8>, ContractError> {
        self.validate_fields()?;
        let mut bytes = Self::DOMAIN.to_vec();
        bytes.push(Self::VERSION);
        for (field, value) in [
            ("tenant", self.tenant.as_str()),
            ("agent_did", self.agent_did.as_str()),
            ("owner_did", self.owner_did.as_str()),
        ] {
            let length =
                u32::try_from(value.len()).map_err(|_| ContractError::OutOfRange(field))?;
            bytes.extend_from_slice(&length.to_be_bytes());
            bytes.extend_from_slice(value.as_bytes());
        }
        bytes.extend_from_slice(&self.owner_public_key);
        bytes.extend_from_slice(&self.session_id.to_bytes()?);
        bytes.extend_from_slice(&self.generation.to_be_bytes());
        bytes.extend_from_slice(&self.expires_at_ms.to_be_bytes());
        bytes.extend_from_slice(&self.capability_id.to_bytes()?);
        bytes.extend_from_slice(&self.protocol_version.to_be_bytes());
        bytes.extend_from_slice(&self.network_id.to_be_bytes());
        bytes.extend_from_slice(&self.activity.encode()?);
        for value in [
            self.preparation_id,
            self.canonical_digest,
            self.economic_action,
            self.idempotency_key,
            self.commitment,
        ] {
            bytes.extend_from_slice(&value);
        }
        Ok(bytes)
    }

    pub fn from_canonical_bytes(bytes: &[u8]) -> Result<Self, ContractError> {
        let rest = bytes
            .strip_prefix(Self::DOMAIN)
            .ok_or(ContractError::Malformed("native_send.domain"))?;
        let (version, rest) = rest
            .split_first()
            .ok_or(ContractError::Malformed("native_send.version"))?;
        if *version != Self::VERSION {
            return Err(ContractError::Malformed("native_send.version"));
        }
        let (tenant, rest) = native_purpose_text(rest, 255, "tenant")?;
        let (agent, rest) =
            native_purpose_text(rest, layerx_types::limits::MAX_DID_BYTES, "agent_did")?;
        let (owner, rest) =
            native_purpose_text(rest, layerx_types::limits::MAX_DID_BYTES, "owner_did")?;
        if rest.len() != 283 {
            return Err(ContractError::Malformed("native_send.length"));
        }
        let session: [u8; 32] = native_purpose_fixed(&rest[32..64])?;
        let capability: [u8; 32] = native_purpose_fixed(&rest[80..112])?;
        let value = Self {
            tenant: TenantId::new(tenant)?,
            agent_did: AgentDid::new(agent)?,
            owner_did: AgentDid::new(owner)?,
            owner_public_key: native_purpose_fixed(&rest[..32])?,
            session_id: SessionId::new(native_purpose_hex32(&session))?,
            generation: u64::from_be_bytes(native_purpose_fixed(&rest[64..72])?),
            expires_at_ms: u64::from_be_bytes(native_purpose_fixed(&rest[72..80])?),
            capability_id: CapabilityId::new(native_purpose_hex32(&capability))?,
            protocol_version: u16::from_be_bytes(native_purpose_fixed(&rest[112..114])?),
            network_id: u32::from_be_bytes(native_purpose_fixed(&rest[114..118])?),
            activity: NativeActivity::decode(&rest[118..123])?,
            preparation_id: native_purpose_fixed(&rest[123..155])?,
            canonical_digest: native_purpose_fixed(&rest[155..187])?,
            economic_action: native_purpose_fixed(&rest[187..219])?,
            idempotency_key: native_purpose_fixed(&rest[219..251])?,
            commitment: native_purpose_fixed(&rest[251..283])?,
        }
        .validate()?;
        if value.canonical_bytes()?.as_slice() != bytes {
            return Err(ContractError::Malformed("native_send.canonical_bytes"));
        }
        Ok(value)
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct SignedNativeSendPurposeV1 {
    pub purpose: NativeSendPurposeV1,
    pub owner_public_key: [u8; 32],
    pub signature: [u8; 64],
}

impl SignedNativeSendPurposeV1 {
    pub fn validate(self) -> Result<Self, ContractError> {
        let purpose = self.purpose.validate()?;
        if self.owner_public_key != purpose.owner_public_key {
            return Err(ContractError::Mismatch("native_send.signature_owner"));
        }
        Ok(Self { purpose, ..self })
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct NativeLocalGrantConsentV1 {
    pub capability: Vec<u8>,
    pub session_scope: Vec<u8>,
    pub expires_at_ms: u64,
    pub owner_public_key: [u8; 32],
    pub signature: [u8; 64],
}

impl NativeLocalGrantConsentV1 {
    pub const MAX_RECORD_BYTES: usize = 1_048_576;

    pub fn validate(&self) -> Result<(), ContractError> {
        for (name, record) in [
            ("capability", &self.capability),
            ("session_scope", &self.session_scope),
        ] {
            if record.is_empty() {
                return Err(ContractError::Empty(name));
            }
            if record.len() > Self::MAX_RECORD_BYTES {
                return Err(ContractError::OutOfRange(name));
            }
        }
        if self.expires_at_ms == 0 {
            return Err(ContractError::Zero("expires_at_ms"));
        }
        Ok(())
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct NativePrepareRequestV1 {
    pub activity: NativeActivity,
    pub actor: AgentDid,
    pub authority: String,
    pub account_sequence: u64,
    pub not_before: u64,
    pub not_after: u64,
    pub idempotency_key: [u8; 32],
    pub fee_limit: u128,
    pub payload: Vec<u8>,
    pub payload_hash: [u8; 32],
    pub capability_id: CapabilityId,
    pub purpose: SignedNativePreparationPurposeV1,
    pub local_grant: Option<NativeLocalGrantConsentV1>,
}

impl NativePrepareRequestV1 {
    pub fn validate(self) -> Result<Self, ContractError> {
        if self.activity.activity_type()?.module() != layerx_types::payload::ModuleId::Programs {
            return Err(ContractError::Mismatch("native_prepare.module"));
        }
        layerx_types::ids::Did::new(self.actor.as_str().as_bytes())
            .map_err(|_| ContractError::Malformed("actor"))?;
        if self.authority.is_empty() {
            return Err(ContractError::Empty("authority"));
        }
        if self.authority.len() > layerx_types::limits::MAX_AUTHORITY_BYTES {
            return Err(ContractError::OutOfRange("authority"));
        }
        if self.not_after < self.not_before {
            return Err(ContractError::OutOfRange("timestamp_bound"));
        }
        if self.payload.is_empty() {
            return Err(ContractError::Empty("payload"));
        }
        if self.payload.len() > layerx_types::limits::MAX_PAYLOAD_BYTES {
            return Err(ContractError::OutOfRange("payload"));
        }
        self.capability_id.to_bytes()?;
        if self.actor != self.purpose.purpose.agent_did {
            return Err(ContractError::Mismatch("actor"));
        }
        if self.capability_id != self.purpose.purpose.capability_id {
            return Err(ContractError::Mismatch("capability_id"));
        }
        if let Some(local_grant) = &self.local_grant {
            local_grant.validate()?;
        }
        Ok(Self {
            purpose: self.purpose.validate()?,
            ..self
        })
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct NativeEffectPrepareRequestV1 {
    pub activity: NativeActivity,
    pub actor: AgentDid,
    pub authority: String,
    pub account_sequence: u64,
    pub not_before: u64,
    pub not_after: u64,
    pub idempotency_key: [u8; 32],
    pub fee_limit: u128,
    pub payload: Vec<u8>,
    pub payload_hash: [u8; 32],
    pub capability_id: CapabilityId,
    pub purpose: SignedNativePreparationPurposeV1,
    pub local_grant: Option<NativeLocalGrantConsentV1>,
}

impl NativeEffectPrepareRequestV1 {
    pub fn validate(self) -> Result<Self, ContractError> {
        if self.activity.activity_type()?.module() == layerx_types::payload::ModuleId::Programs {
            return Err(ContractError::Mismatch("native_effect_prepare.module"));
        }
        layerx_types::ids::Did::new(self.actor.as_str().as_bytes())
            .map_err(|_| ContractError::Malformed("actor"))?;
        if self.authority.is_empty() {
            return Err(ContractError::Empty("authority"));
        }
        if self.authority.len() > layerx_types::limits::MAX_AUTHORITY_BYTES {
            return Err(ContractError::OutOfRange("authority"));
        }
        if self.not_after < self.not_before {
            return Err(ContractError::OutOfRange("timestamp_bound"));
        }
        if self.payload.is_empty() {
            return Err(ContractError::Empty("payload"));
        }
        if self.payload.len() > layerx_types::limits::MAX_PAYLOAD_BYTES {
            return Err(ContractError::OutOfRange("payload"));
        }
        self.capability_id.to_bytes()?;
        if self.actor != self.purpose.purpose.agent_did {
            return Err(ContractError::Mismatch("actor"));
        }
        if self.capability_id != self.purpose.purpose.capability_id {
            return Err(ContractError::Mismatch("capability_id"));
        }
        if let Some(local_grant) = &self.local_grant {
            local_grant.validate()?;
        }
        Ok(Self {
            purpose: self.purpose.validate()?,
            ..self
        })
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct NativeSendPrepareRequestV1 {
    pub activity: NativeActivity,
    pub actor: AgentDid,
    pub authority: String,
    pub account_sequence: u64,
    pub not_before: u64,
    pub not_after: u64,
    pub idempotency_key: [u8; 32],
    pub fee_limit: u128,
    pub payload: Vec<u8>,
    pub payload_hash: [u8; 32],
    pub capability_id: CapabilityId,
    pub purpose: SignedNativeSendPurposeV1,
    pub local_grant: Option<NativeLocalGrantConsentV1>,
}

impl NativeSendPrepareRequestV1 {
    pub fn validate(self) -> Result<Self, ContractError> {
        if self.activity != NativeActivity::new(1, 5)? {
            return Err(ContractError::Mismatch("native_send_prepare.activity"));
        }
        layerx_types::ids::Did::new(self.actor.as_str().as_bytes())
            .map_err(|_| ContractError::Malformed("actor"))?;
        if self.authority.is_empty() {
            return Err(ContractError::Empty("authority"));
        }
        if self.authority.len() > layerx_types::limits::MAX_AUTHORITY_BYTES {
            return Err(ContractError::OutOfRange("authority"));
        }
        if self.not_after < self.not_before {
            return Err(ContractError::OutOfRange("timestamp_bound"));
        }
        if self.payload.is_empty() {
            return Err(ContractError::Empty("payload"));
        }
        if self.payload.len() > layerx_types::limits::MAX_PAYLOAD_BYTES {
            return Err(ContractError::OutOfRange("payload"));
        }
        self.capability_id.to_bytes()?;
        if self.actor != self.purpose.purpose.agent_did {
            return Err(ContractError::Mismatch("actor"));
        }
        if self.capability_id != self.purpose.purpose.capability_id {
            return Err(ContractError::Mismatch("capability_id"));
        }
        if self.activity != self.purpose.purpose.activity
            || self.idempotency_key != self.purpose.purpose.idempotency_key
        {
            return Err(ContractError::Mismatch("native_send_prepare.request"));
        }
        if let Some(local_grant) = &self.local_grant {
            local_grant.validate()?;
        }
        Ok(Self {
            purpose: self.purpose.validate()?,
            ..self
        })
    }
}

#[derive(Clone, Copy, Debug, Default, Eq, PartialEq)]
pub struct NativeApprovalListV1;

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct NativeApprovalGetV1 {
    pub approval_id: [u8; 32],
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct NativeApprovalDecisionV1 {
    pub approval_id: [u8; 32],
    pub held_digest: [u8; 32],
    pub current_sequence: u64,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct NativePrepareResultV1 {
    pub preparation_id: [u8; 32],
    pub canonical_bytes: Vec<u8>,
    pub signing_preimage: [u8; 32],
    pub activity: NativeActivity,
    pub approval_required: bool,
    pub approval_id: Option<[u8; 32]>,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct NativeApprovalResultV1 {
    pub approval_id: [u8; 32],
    pub held_digest: [u8; 32],
    pub activity: NativeActivity,
    pub state: String,
    pub submission_ref: Option<[u8; 32]>,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct NativeApprovalListResultV1 {
    pub approvals: Vec<NativeApprovalResultV1>,
}

/// A restriction dimension that a caller must supply explicitly.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ExplicitSet<T>(Vec<T>);

impl<T> ExplicitSet<T> {
    #[must_use]
    pub const fn deny_all() -> Self {
        Self(Vec::new())
    }

    #[must_use]
    pub const fn allow(values: Vec<T>) -> Self {
        Self(values)
    }

    #[must_use]
    pub fn values(&self) -> &[T] {
        &self.0
    }
}

/// Complete session authority context; no operation may synthesize defaults.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct SessionContext {
    pub tenant: TenantId,
    pub agent_did: AgentDid,
    pub authority_ref: AuthorityRef,
    pub permitted_activity_types: ExplicitSet<ActivityType>,
    pub expiry: TimestampSeconds,
    pub client: ClientId,
    pub policy_version: PolicyVersion,
}

impl SessionContext {
    /// Creates a context only when expiry is explicitly nonzero.
    ///
    /// # Errors
    /// Returns [`ContractError::Zero`] when expiry is zero.
    pub fn new(
        tenant: TenantId,
        agent_did: AgentDid,
        authority_ref: AuthorityRef,
        permitted_activity_types: ExplicitSet<ActivityType>,
        expiry: TimestampSeconds,
        client: ClientId,
        policy_version: PolicyVersion,
    ) -> Result<Self, ContractError> {
        if expiry.0 == 0 {
            return Err(ContractError::Zero("expiry"));
        }
        Ok(Self {
            tenant,
            agent_did,
            authority_ref,
            permitted_activity_types,
            expiry,
            client,
            policy_version,
        })
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct AgentRegistration {
    pub tenant: TenantId,
    pub agent_did: AgentDid,
    pub authority_ref: AuthorityRef,
    pub client: ClientId,
    pub policy_version: PolicyVersion,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct SessionOpen(pub SessionContext);

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct SessionRefresh {
    pub session_id: SessionId,
    pub context: SessionContext,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct SessionClose {
    pub session_id: SessionId,
    pub context: SessionContext,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct SessionList(pub SessionContext);

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct AmountCeiling {
    pub asset: Asset,
    pub amount: Amount,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct RateCeiling {
    pub window_seconds: TimestampSeconds,
    pub maximum_actions: u64,
}

/// Every capability dimension is present, including explicitly empty deny sets.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct CapabilityDimensions {
    pub activity_types: ExplicitSet<ActivityType>,
    pub counterparties: ExplicitSet<Counterparty>,
    pub assets: ExplicitSet<Asset>,
    pub amount_ceilings: ExplicitSet<AmountCeiling>,
    pub rate_ceilings: ExplicitSet<RateCeiling>,
    pub purpose_constraints: ExplicitSet<Purpose>,
    pub expiry: TimestampSeconds,
}

impl CapabilityDimensions {
    /// Validates the only scalar dimension that can be malformed.
    ///
    /// # Errors
    /// Returns [`ContractError::Zero`] when expiry is zero.
    pub fn validate(self) -> Result<Self, ContractError> {
        if self.expiry.0 == 0 {
            return Err(ContractError::Zero("expiry"));
        }
        Ok(self)
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct CapabilityCreate {
    pub tenant: TenantId,
    pub agent_did: AgentDid,
    pub dimensions: CapabilityDimensions,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct CapabilityAttenuate {
    pub tenant: TenantId,
    pub agent_did: AgentDid,
    pub parent_id: CapabilityId,
    pub dimensions: CapabilityDimensions,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct CapabilityList {
    pub tenant: TenantId,
    pub agent_did: AgentDid,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct CapabilityRevoke {
    pub tenant: TenantId,
    pub agent_did: AgentDid,
    pub capability_id: CapabilityId,
}

/// States who actually enforces a budget-like restriction.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum BudgetEnforcement {
    ProtocolBudget,
    DaemonLimit,
}

impl BudgetEnforcement {
    pub const DAEMON_LIMIT_NOTICE: &'static str =
        "Bypassing the daemon bypasses this limit. It is not equivalent to a protocol budget.";

    #[must_use]
    pub const fn guarantee(self) -> &'static str {
        match self {
            Self::ProtocolBudget => "protocol_enforced",
            Self::DaemonLimit => "daemon_enforced",
        }
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct BudgetCreate {
    pub tenant: TenantId,
    pub agent_did: AgentDid,
    pub asset: Asset,
    pub limit: BudgetLimit,
    pub enforcement: BudgetEnforcement,
    pub expiry: TimestampSeconds,
}

impl BudgetCreate {
    /// Validates finite limit and expiry.
    ///
    /// # Errors
    /// Returns [`ContractError::Zero`] for a zero limit or expiry.
    pub fn validate(self) -> Result<Self, ContractError> {
        if self.limit.0 == 0 {
            return Err(ContractError::Zero("limit"));
        }
        if self.expiry.0 == 0 {
            return Err(ContractError::Zero("expiry"));
        }
        Ok(self)
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct BudgetFund {
    pub tenant: TenantId,
    pub agent_did: AgentDid,
    pub budget_id: BudgetId,
    pub amount: Amount,
    pub enforcement: BudgetEnforcement,
}

impl BudgetFund {
    /// Protocol funding cannot be represented for a daemon-only limit.
    ///
    /// # Errors
    /// Returns [`ContractError::DaemonLimitFunding`] for daemon-only limits.
    pub fn validate(self) -> Result<Self, ContractError> {
        if matches!(self.enforcement, BudgetEnforcement::DaemonLimit) {
            return Err(ContractError::DaemonLimitFunding);
        }
        Ok(self)
    }
}

/// Owner signature over a prepared canonical budget activity.
///
/// `signer_public_key` is only an equality assertion against the cached authorised owner key;
/// it never names the verifying authority.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct BudgetAuthorization {
    pub preparation_ref: PreparationRef,
    pub signature: SignatureBytes,
    pub signer_public_key: Option<[u8; 32]>,
}

/// A budget mutation and its conditionally required owner authorisation.
///
/// Protocol budgets execute signed core activities and require the carrier; daemon limits are
/// enforced by session authority and must not carry one.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct SignedBudgetMutation<T> {
    pub request: T,
    pub authorization: Option<BudgetAuthorization>,
}

impl<T> SignedBudgetMutation<T> {
    /// Checks the carrier against the enforcement actually in force for the budget.
    ///
    /// # Errors
    /// Returns [`ContractError::Empty`] for an unsigned protocol mutation and
    /// [`ContractError::Mismatch`] for a signed daemon-limit mutation.
    pub const fn require_for(&self, enforcement: BudgetEnforcement) -> Result<(), ContractError> {
        match (enforcement, &self.authorization) {
            (BudgetEnforcement::ProtocolBudget, None) => {
                Err(ContractError::Empty("budget_authorization"))
            }
            (BudgetEnforcement::DaemonLimit, Some(_)) => {
                Err(ContractError::Mismatch("budget_authorization"))
            }
            _ => Ok(()),
        }
    }
}

impl SignedBudgetMutation<BudgetCreate> {
    /// Validates the request and its carrier for the requested enforcement.
    ///
    /// # Errors
    /// Returns the request validation failure or the carrier refusal.
    pub fn validate(self) -> Result<Self, ContractError> {
        let request = self.request.validate()?;
        let mutation = Self {
            request,
            authorization: self.authorization,
        };
        mutation.require_for(mutation.request.enforcement)?;
        Ok(mutation)
    }
}

impl SignedBudgetMutation<BudgetFund> {
    /// Validates the request and its carrier; only protocol budgets can be funded.
    ///
    /// # Errors
    /// Returns the request validation failure or the carrier refusal.
    pub fn validate(self) -> Result<Self, ContractError> {
        let request = self.request.validate()?;
        let mutation = Self {
            request,
            authorization: self.authorization,
        };
        mutation.require_for(mutation.request.enforcement)?;
        Ok(mutation)
    }
}

/// A protocol budget record projected from proven core state; units are explicit.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ProtocolBudgetView {
    pub budget_id: [u8; 32],
    pub owner: [u8; 32],
    pub budget_account: [u8; 32],
    pub asset_id: [u8; 32],
    pub purpose_hash: [u8; 32],
    pub per_period_limit: Amount,
    pub configured_period_limit: Amount,
    pub carry_cap: Amount,
    pub spent_this_period: Amount,
    pub carried: Amount,
    pub period_length_ms: u64,
    pub period_start_ms: u64,
    pub expiry_ms: u64,
    pub revocation_counter: u64,
    pub rollover_policy: u8,
    pub closed: bool,
    pub revoked: bool,
    pub delegates: Vec<[u8; 32]>,
    pub source_account: Option<[u8; 32]>,
    pub achieved_verification_level: Level,
}

impl ProtocolBudgetView {
    /// Projects one decoded core budget record whose state was proven.
    ///
    /// # Errors
    /// Returns [`ContractError::OutOfRange`] when the attached evidence is below state proof.
    pub fn from_proven(
        record: &layerx_client::budget::ProtocolBudgetRecord,
        achieved_verification_level: Level,
    ) -> Result<Self, ContractError> {
        if achieved_verification_level < Level::StateProven {
            return Err(ContractError::OutOfRange("budget_verification_level"));
        }
        Ok(Self {
            budget_id: record.budget_id,
            owner: record.owner,
            budget_account: record.budget_account,
            asset_id: record.asset_id,
            purpose_hash: record.purpose_hash,
            per_period_limit: Amount(record.per_period_limit),
            configured_period_limit: Amount(record.configured_period_limit),
            carry_cap: Amount(record.carry_cap),
            spent_this_period: Amount(record.spent_this_period),
            carried: Amount(record.carried),
            period_length_ms: record.period_length,
            period_start_ms: record.period_start,
            expiry_ms: record.expiry,
            revocation_counter: record.revocation_sequence,
            rollover_policy: record.rollover_policy,
            closed: record.closed,
            revoked: record.revoked,
            delegates: record.delegates.clone(),
            source_account: record.source_account,
            achieved_verification_level,
        })
    }
}

/// A durable daemon-enforced limit. It never carries chain verification.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct DaemonLimitView {
    pub budget_id: [u8; 32],
    pub asset: [u8; 32],
    pub ceiling: BudgetLimit,
    pub consumed: Amount,
    pub expiry_ms: u64,
    pub revoked: bool,
}

impl DaemonLimitView {
    /// The bypass notice every daemon-limit record carries.
    #[must_use]
    pub const fn notice(&self) -> &'static str {
        BudgetEnforcement::DAEMON_LIMIT_NOTICE
    }
}

/// One budget-like record tagged by who actually enforces it.
#[derive(Clone, Debug, Eq, PartialEq)]
pub enum BudgetRecord {
    Protocol(ProtocolBudgetView),
    Daemon(DaemonLimitView),
}

impl BudgetRecord {
    #[must_use]
    pub const fn enforcement(&self) -> BudgetEnforcement {
        match self {
            Self::Protocol(_) => BudgetEnforcement::ProtocolBudget,
            Self::Daemon(_) => BudgetEnforcement::DaemonLimit,
        }
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct BudgetRecords(pub Vec<BudgetRecord>);

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct BudgetList {
    pub tenant: TenantId,
    pub agent_did: AgentDid,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct BudgetTarget {
    pub tenant: TenantId,
    pub agent_did: AgentDid,
    pub budget_id: BudgetId,
}

/// Exact authority attached to every successful or refused response.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct AuthorityDescription {
    pub tenant: TenantId,
    pub agent_did: AgentDid,
    pub authority_ref: AuthorityRef,
    pub protocol_authority: Vec<u8>,
}

impl AuthorityDescription {
    /// Creates an authority description with non-empty canonical protocol bytes.
    ///
    /// # Errors
    /// Returns [`ContractError::Empty`] when protocol authority bytes are absent.
    pub fn new(
        tenant: TenantId,
        agent_did: AgentDid,
        authority_ref: AuthorityRef,
        protocol_authority: Vec<u8>,
    ) -> Result<Self, ContractError> {
        if protocol_authority.is_empty() {
            return Err(ContractError::Empty("protocol_authority"));
        }
        Ok(Self {
            tenant,
            agent_did,
            authority_ref,
            protocol_authority,
        })
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct AuthorityResponse<T> {
    pub authority: AuthorityDescription,
    pub value: T,
}

/// Magic and version that open every explicit policy intent carrier.
pub const POLICY_INTENT_MAGIC: [u8; 8] = [0x4c, 0x58, 0x50, 0x44, 0x00, 0x00, 0x00, 0x01];

/// Largest accepted canonical intent, explicit or raw.
pub const MAX_POLICY_INTENT_BYTES: usize = layerx_types::limits::MAX_PAYLOAD_BYTES;

/// Session-context policy dry run carrying explicit or raw canonical intent bytes.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct LegacyPolicyDryRun {
    pub context: SessionContext,
    pub canonical_intent: Vec<u8>,
}

/// Explicit policy intent carrier refusal.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum PolicyIntentError {
    Empty(&'static str),
    TooLarge,
    Version,
    Truncated,
    TrailingBytes,
    InvalidUtf8,
}

impl core::fmt::Display for PolicyIntentError {
    fn fmt(&self, formatter: &mut core::fmt::Formatter<'_>) -> core::fmt::Result {
        match self {
            Self::Empty(field) => write!(formatter, "{field} is empty"),
            Self::TooLarge => formatter.write_str("policy intent exceeds its size bound"),
            Self::Version => formatter.write_str("policy intent magic or version is not supported"),
            Self::Truncated => formatter.write_str("policy intent is truncated"),
            Self::TrailingBytes => formatter.write_str("policy intent has trailing bytes"),
            Self::InvalidUtf8 => formatter.write_str("policy intent purpose is not UTF-8"),
        }
    }
}

impl std::error::Error for PolicyIntentError {}

/// Versioned purpose text and canonical unsigned activity.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct PolicyIntent {
    purpose: String,
    canonical_activity: Vec<u8>,
}

impl PolicyIntent {
    /// Builds a carrier from a nonempty purpose and nonempty canonical unsigned activity.
    ///
    /// # Errors
    /// Returns [`PolicyIntentError::Empty`] for an empty part and [`PolicyIntentError::TooLarge`] above the bound.
    pub fn new(purpose: &str, canonical_activity: &[u8]) -> Result<Self, PolicyIntentError> {
        if purpose.is_empty() {
            return Err(PolicyIntentError::Empty("purpose"));
        }
        if canonical_activity.is_empty() {
            return Err(PolicyIntentError::Empty("canonical_activity"));
        }
        let total = (POLICY_INTENT_MAGIC.len() + 8)
            .checked_add(purpose.len())
            .and_then(|length| length.checked_add(canonical_activity.len()))
            .ok_or(PolicyIntentError::TooLarge)?;
        if total > MAX_POLICY_INTENT_BYTES
            || u32::try_from(purpose.len()).is_err()
            || u32::try_from(canonical_activity.len()).is_err()
        {
            return Err(PolicyIntentError::TooLarge);
        }
        Ok(Self {
            purpose: purpose.to_owned(),
            canonical_activity: canonical_activity.to_vec(),
        })
    }

    #[must_use]
    pub fn purpose(&self) -> &str {
        &self.purpose
    }

    #[must_use]
    pub fn canonical_activity(&self) -> &[u8] {
        &self.canonical_activity
    }

    #[must_use]
    pub fn encode(&self) -> Vec<u8> {
        let mut output = Vec::with_capacity(
            POLICY_INTENT_MAGIC.len() + 8 + self.purpose.len() + self.canonical_activity.len(),
        );
        output.extend_from_slice(&POLICY_INTENT_MAGIC);
        for part in [self.purpose.as_bytes(), self.canonical_activity.as_slice()] {
            let length = part.len().to_be_bytes();
            output.extend_from_slice(&length[length.len() - 4..]);
            output.extend_from_slice(part);
        }
        output
    }

    /// Decodes exactly one carrier with no trailing bytes.
    ///
    /// # Errors
    /// Returns the [`PolicyIntentError`] naming the first violated rule.
    pub fn decode(bytes: &[u8]) -> Result<Self, PolicyIntentError> {
        if bytes.len() > MAX_POLICY_INTENT_BYTES {
            return Err(PolicyIntentError::TooLarge);
        }
        let rest = bytes
            .strip_prefix(POLICY_INTENT_MAGIC.as_slice())
            .ok_or(PolicyIntentError::Version)?;
        let (purpose, rest) = split_intent_part(rest)?;
        let (canonical_activity, rest) = split_intent_part(rest)?;
        if !rest.is_empty() {
            return Err(PolicyIntentError::TrailingBytes);
        }
        let purpose = core::str::from_utf8(purpose).map_err(|_| PolicyIntentError::InvalidUtf8)?;
        Self::new(purpose, canonical_activity)
    }
}

fn split_intent_part(bytes: &[u8]) -> Result<(&[u8], &[u8]), PolicyIntentError> {
    let (length, rest) = bytes
        .split_first_chunk::<4>()
        .ok_or(PolicyIntentError::Truncated)?;
    let length =
        usize::try_from(u32::from_be_bytes(*length)).map_err(|_| PolicyIntentError::TooLarge)?;
    rest.split_at_checked(length)
        .ok_or(PolicyIntentError::Truncated)
}

/// Canonical intent bytes as either an explicit carrier or raw canonical unsigned activity.
#[derive(Clone, Debug, Eq, PartialEq)]
pub enum IntentCarrier<'a> {
    Explicit(PolicyIntent),
    Raw(&'a [u8]),
}

impl<'a> IntentCarrier<'a> {
    /// Treats bytes opening with the carrier family tag as an explicit carrier and all others as raw.
    ///
    /// # Errors
    /// Returns [`PolicyIntentError`] for empty or oversized bytes and for any malformed explicit carrier.
    pub fn classify(bytes: &'a [u8]) -> Result<Self, PolicyIntentError> {
        if bytes.is_empty() {
            return Err(PolicyIntentError::Empty("canonical_intent"));
        }
        if bytes.len() > MAX_POLICY_INTENT_BYTES {
            return Err(PolicyIntentError::TooLarge);
        }
        if bytes.starts_with(&POLICY_INTENT_MAGIC[..4]) {
            return PolicyIntent::decode(bytes).map(Self::Explicit);
        }
        Ok(Self::Raw(bytes))
    }
}

/// Hypothetical local policy evaluation; it creates no preparation, signature or submission.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct PolicyDryRunRequest {
    pub tenant: TenantId,
    pub agent_did: AgentDid,
    pub session_id: SessionId,
    pub capability_id: CapabilityId,
    pub activity_type: ActivityType,
    pub counterparty: [u8; 32],
    pub asset: [u8; 32],
    pub amount: Amount,
    pub purpose: String,
    /// Caller-stated evaluation sequence; it is not a freshness claim.
    pub core_sequence: Sequence,
}

impl PolicyDryRunRequest {
    /// Validates the session and capability identifiers.
    ///
    /// # Errors
    /// Returns [`ContractError::Malformed`] for a non-canonical identifier.
    pub fn validate(self) -> Result<Self, ContractError> {
        self.session_id.to_bytes()?;
        self.capability_id.to_bytes()?;
        Ok(self)
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum PolicyOutcome {
    Allow,
    Deny,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum PolicyDecisionReason {
    PermittedByRule,
    ExplicitDeny,
    ApprovalRequired,
    NoPermittingRule,
    InvalidContext,
    EvaluationFailure,
}

/// Local restriction result. It is not protocol authorisation and carries no freshness.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct PolicyDryRunResult {
    pub outcome: PolicyOutcome,
    pub policy_version: PolicyVersion,
    pub matched_rules: Vec<String>,
    pub deciding_rule: Option<String>,
    pub reason: PolicyDecisionReason,
    pub authority_statement: String,
}

impl PolicyDryRunResult {
    /// Refuses a result without its local-restriction statement or with empty rule names.
    ///
    /// # Errors
    /// Returns [`ContractError::Empty`] for an absent statement or an empty rule identifier.
    pub fn validate(self) -> Result<Self, ContractError> {
        if self.authority_statement.is_empty() {
            return Err(ContractError::Empty("authority_statement"));
        }
        if self.matched_rules.iter().any(String::is_empty)
            || self.deciding_rule.as_deref() == Some("")
        {
            return Err(ContractError::Empty("rule_id"));
        }
        Ok(self)
    }
}

#[cfg(test)]
mod tests {
    use super::{IntentCarrier, PolicyIntent, PolicyIntentError, POLICY_INTENT_MAGIC};

    const ACTIVITY: [u8; 3] = [0x10, 0x01, 0xaa];

    fn carrier(version: u8, purpose: &[u8], activity: &[u8]) -> Vec<u8> {
        let mut bytes = POLICY_INTENT_MAGIC.to_vec();
        bytes[7] = version;
        for part in [purpose, activity] {
            let length = part.len().to_be_bytes();
            bytes.extend_from_slice(&length[length.len() - 4..]);
            bytes.extend_from_slice(part);
        }
        bytes
    }

    #[test]
    fn explicit_intent_round_trips_exactly() -> Result<(), PolicyIntentError> {
        let intent = PolicyIntent::new("rent", &ACTIVITY)?;
        let encoded = intent.encode();
        assert_eq!(
            encoded,
            [
                0x4c, 0x58, 0x50, 0x44, 0, 0, 0, 1, 0, 0, 0, 4, b'r', b'e', b'n', b't', 0, 0, 0, 3,
                0x10, 0x01, 0xaa
            ]
        );
        assert_eq!(encoded, carrier(1, b"rent", &ACTIVITY));
        let decoded = PolicyIntent::decode(&encoded)?;
        assert_eq!(decoded, intent);
        assert_eq!(decoded.purpose(), "rent");
        assert_eq!(decoded.canonical_activity(), &ACTIVITY);
        assert_eq!(decoded.encode(), encoded);
        assert_eq!(
            IntentCarrier::classify(&encoded)?,
            IntentCarrier::Explicit(intent)
        );
        Ok(())
    }

    #[test]
    fn trailing_bytes_are_refused() {
        let mut bytes = carrier(1, b"rent", &ACTIVITY);
        bytes.push(0);
        assert_eq!(
            PolicyIntent::decode(&bytes),
            Err(PolicyIntentError::TrailingBytes)
        );
        assert_eq!(
            IntentCarrier::classify(&bytes),
            Err(PolicyIntentError::TrailingBytes)
        );
    }

    #[test]
    fn empty_purpose_is_refused() {
        assert_eq!(
            PolicyIntent::new("", &ACTIVITY),
            Err(PolicyIntentError::Empty("purpose"))
        );
        assert_eq!(
            PolicyIntent::decode(&carrier(1, b"", &ACTIVITY)),
            Err(PolicyIntentError::Empty("purpose"))
        );
        assert_eq!(
            PolicyIntent::decode(&carrier(1, &[0xff], &ACTIVITY)),
            Err(PolicyIntentError::InvalidUtf8)
        );
    }

    #[test]
    fn wrong_version_and_truncation_are_refused() {
        let other = carrier(2, b"rent", &ACTIVITY);
        assert_eq!(
            PolicyIntent::decode(&other),
            Err(PolicyIntentError::Version)
        );
        assert_eq!(
            IntentCarrier::classify(&other),
            Err(PolicyIntentError::Version)
        );
        let valid = carrier(1, b"rent", &ACTIVITY);
        assert_eq!(
            PolicyIntent::decode(&valid[..valid.len() - 1]),
            Err(PolicyIntentError::Truncated)
        );
    }

    #[test]
    fn raw_activity_is_classified_raw() -> Result<(), PolicyIntentError> {
        assert_eq!(
            IntentCarrier::classify(&ACTIVITY)?,
            IntentCarrier::Raw(&ACTIVITY)
        );
        assert_eq!(
            IntentCarrier::classify(&[]),
            Err(PolicyIntentError::Empty("canonical_intent"))
        );
        Ok(())
    }
}

#[cfg(test)]
mod native_contract_tests {
    use super::{
        AgentDid, CapabilityId, ContractError, NativeActivity, NativePreparationPurposeV1,
        SessionId, TenantId,
    };
    use layerx_types::payload::{ActivityType as CoreActivityType, ModuleId};

    fn purpose() -> Result<NativePreparationPurposeV1, ContractError> {
        NativePreparationPurposeV1 {
            tenant: TenantId::new("tenant-a")?,
            agent_did: AgentDid::new("did:layerx:alice")?,
            session_id: SessionId::new("11".repeat(32))?,
            generation: 1,
            expires_at_ms: 1000,
            capability_id: CapabilityId::new("22".repeat(32))?,
            preparation_id: [3; 32],
            canonical_digest: [4; 32],
            commitment: [5; 32],
        }
        .validate()
    }

    #[test]
    fn native_activity_round_trips_real_modules_and_full_ordinals() -> Result<(), ContractError> {
        for module in ModuleId::ALL {
            for ordinal in [1, 256, u16::MAX] {
                let activity = NativeActivity::new(module as u16, ordinal)?;
                let core = activity.activity_type()?;
                assert_eq!(core.module(), module);
                assert_eq!(core.ordinal(), ordinal);
                assert_eq!(NativeActivity::from(core), activity);
                assert_eq!(NativeActivity::decode(&activity.encode()?)?, activity);
            }
        }
        assert_eq!(NativeActivity::new(9, 258)?.encode()?, [1, 0, 9, 1, 2]);
        Ok(())
    }

    #[test]
    fn native_activity_refuses_unknown_module_zero_ordinal_version_and_length() {
        for module in [0, 12, u16::MAX] {
            assert_eq!(
                NativeActivity::new(module, 1),
                Err(ContractError::Malformed("native_activity.module"))
            );
            assert!(NativeActivity { module, ordinal: 1 }.encode().is_err());
        }
        assert_eq!(
            NativeActivity::new(1, 0),
            Err(ContractError::Zero("native_activity.ordinal"))
        );
        for bytes in [
            &[0, 0, 1, 0, 1][..],
            &[2, 0, 1, 0, 1][..],
            &[1, 0, 12, 0, 1][..],
            &[1, 0, 1, 0, 0][..],
            &[1, 0, 1, 0][..],
            &[1, 0, 1, 0, 1, 0][..],
            &[][..],
        ] {
            assert!(NativeActivity::decode(bytes).is_err());
        }
    }

    #[test]
    fn native_activity_preserves_module_identity_for_equal_ordinals() -> Result<(), ContractError> {
        let asset = NativeActivity::new(1, 1)?;
        let budget = NativeActivity::new(3, 1)?;
        assert_ne!(asset, budget);
        assert_ne!(asset.encode()?, budget.encode()?);
        assert_ne!(asset.activity_type()?, budget.activity_type()?);
        assert_eq!(
            CoreActivityType::from_u32(0x0003_0001).map(NativeActivity::from),
            Ok(budget)
        );
        Ok(())
    }

    #[test]
    fn native_purpose_canonical_bytes_bind_exact_identity_and_commitment(
    ) -> Result<(), ContractError> {
        let value = purpose()?;
        let mut expected = NativePreparationPurposeV1::DOMAIN.to_vec();
        expected.push(1);
        expected.extend_from_slice(&8_u32.to_be_bytes());
        expected.extend_from_slice(b"tenant-a");
        expected.extend_from_slice(&16_u32.to_be_bytes());
        expected.extend_from_slice(b"did:layerx:alice");
        expected.extend_from_slice(&[0x11; 32]);
        expected.extend_from_slice(&1_u64.to_be_bytes());
        expected.extend_from_slice(&1000_u64.to_be_bytes());
        expected.extend_from_slice(&[0x22; 32]);
        expected.extend_from_slice(&[3; 32]);
        expected.extend_from_slice(&[4; 32]);
        expected.extend_from_slice(&[5; 32]);
        assert_eq!(value.canonical_bytes()?, expected);
        for field in 0..9 {
            let mut changed = value.clone();
            match field {
                0 => changed.tenant = TenantId::new("tenant-b")?,
                1 => changed.agent_did = AgentDid::new("did:layerx:bob")?,
                2 => changed.session_id = SessionId::new("33".repeat(32))?,
                3 => changed.generation = 2,
                4 => changed.capability_id = CapabilityId::new("44".repeat(32))?,
                5 => changed.preparation_id[0] ^= 1,
                6 => changed.canonical_digest[0] ^= 1,
                7 => changed.commitment[0] ^= 1,
                8 => changed.expires_at_ms = 1001,
                _ => unreachable!(),
            }
            assert_ne!(changed.canonical_bytes()?, expected);
        }
        let mut first = value.clone();
        first.tenant = TenantId::new("ab")?;
        first.agent_did = AgentDid::new("c")?;
        let mut second = value;
        second.tenant = TenantId::new("a")?;
        second.agent_did = AgentDid::new("bc")?;
        assert_ne!(first.canonical_bytes()?, second.canonical_bytes()?);
        Ok(())
    }

    #[test]
    fn native_purpose_canonical_decoder_refuses_truncation_trailing_version_and_text(
    ) -> Result<(), ContractError> {
        let value = purpose()?;
        let bytes = value.canonical_bytes()?;
        assert_eq!(
            NativePreparationPurposeV1::from_canonical_bytes(&bytes)?,
            value
        );
        for length in 0..bytes.len() {
            assert!(NativePreparationPurposeV1::from_canonical_bytes(&bytes[..length]).is_err());
        }
        let mut trailing = bytes.clone();
        trailing.push(0);
        assert!(NativePreparationPurposeV1::from_canonical_bytes(&trailing).is_err());
        let mut version = bytes.clone();
        version[NativePreparationPurposeV1::DOMAIN.len()] = 2;
        assert!(NativePreparationPurposeV1::from_canonical_bytes(&version).is_err());
        let mut invalid_text = bytes.clone();
        invalid_text[NativePreparationPurposeV1::DOMAIN.len() + 5] = 0xff;
        assert!(NativePreparationPurposeV1::from_canonical_bytes(&invalid_text).is_err());
        let mut nul_tenant = bytes.clone();
        nul_tenant[NativePreparationPurposeV1::DOMAIN.len() + 5] = 0;
        assert!(NativePreparationPurposeV1::from_canonical_bytes(&nul_tenant).is_err());
        let mut length = bytes;
        let offset = NativePreparationPurposeV1::DOMAIN.len() + 1;
        length[offset..offset + 4].copy_from_slice(&256_u32.to_be_bytes());
        assert!(NativePreparationPurposeV1::from_canonical_bytes(&length).is_err());
        Ok(())
    }

    #[test]
    fn native_purpose_refuses_noncanonical_identifiers_zero_generation_and_zero_expiry(
    ) -> Result<(), ContractError> {
        let valid = purpose()?;
        for field in 0..7 {
            let mut invalid = valid.clone();
            match field {
                0 => invalid.session_id = SessionId::new("AA".repeat(32))?,
                1 => invalid.capability_id = CapabilityId::new("ab")?,
                2 => invalid.generation = 0,
                6 => invalid.expires_at_ms = 0,
                3 => invalid.tenant = TenantId::new("bad\0tenant")?,
                4 => invalid.tenant = TenantId::new("a".repeat(256))?,
                5 => {
                    invalid.agent_did =
                        AgentDid::new("a".repeat(layerx_types::limits::MAX_DID_BYTES + 1))?;
                }
                _ => unreachable!(),
            }
            assert!(invalid.canonical_bytes().is_err());
            assert!(invalid.validate().is_err());
        }
        Ok(())
    }
}
