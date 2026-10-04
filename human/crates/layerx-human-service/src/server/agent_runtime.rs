//! Bounded, typed production transport for the Human journey engine.

use std::path::{Path, PathBuf};

use layerx_agent_api::idempotency::IdempotentMutation;
use layerx_agent_api::prepare::{PreparationRef, PrepareRequest};
use layerx_agent_api::submit::SubmitRequest;
use layerx_agent_api::track::{
    EvidenceRef, ReceiptRef, SubmissionRef, SubmissionState, TrackRequest, TrackedSubmission,
    Transition,
};
use layerx_agent_api::verify::Level;
use layerx_agent_api::TimestampSeconds;
use layerx_client::lni::transport::{ConnectionGate, FrameTransport, Limits, Uds};
use layerx_crypto::disclosure;
use layerx_proof::receipt::AuthorizedBatch;
use layerx_sdk::Call;
use layerx_types::payload::{ActivityType, ModuleId, ModuleRegistration, ModuleRegistry};
use layerx_types::result::ResultCode;
use layerx_types::verify::VerificationLevel;
use sha2::Digest as _;
use zeroize::Zeroizing;

use crate::journeys::DepositAgentBoundary;
use crate::journeys::{
    AgentBoundary, AgentBoundaryError, AgentObservation, AgentPreparation, ReceiptLookup,
    ReceiptMaterial,
};

const MAGIC: &[u8; 8] = b"LXHAGT01";
const TRACED_MAGIC: &[u8; 8] = b"LXHAGT02";

thread_local! {
    static REQUEST_TRACE: std::cell::RefCell<Option<crate::trace::TraceId>> = const { std::cell::RefCell::new(None) };
}

pub(crate) struct TraceContext {
    previous: Option<crate::trace::TraceId>,
    local: std::marker::PhantomData<std::rc::Rc<()>>,
}

impl TraceContext {
    pub(crate) fn enter(trace: &str) -> Result<Self, AgentBoundaryError> {
        let trace = crate::trace::TraceId::parse(trace).map_err(|_| AgentBoundaryError::Refused)?;
        let previous = REQUEST_TRACE.with(|current| current.replace(Some(trace)));
        Ok(Self {
            previous,
            local: std::marker::PhantomData,
        })
    }
}

impl Drop for TraceContext {
    fn drop(&mut self) {
        REQUEST_TRACE.with(|current| current.replace(self.previous.take()));
    }
}

const PREPARE: u8 = 1;
const SUBMIT: u8 = 2;
const TRACK: u8 = 3;
const RECEIPT_LOOKUP: u8 = 4;
const REGISTRY: u8 = 5;
const APPROVAL_LIST_FACTS: u8 = 46;
const APPROVAL_GET_FACTS: u8 = 47;
const APPROVAL_BUDGET_AFTER: u8 = 48;
const MANAGED_EVIDENCE: u8 = 49;
const NATIVE_APPROVAL_LIST_FACTS: u8 = 50;
const NATIVE_APPROVAL_GET_FACTS: u8 = 51;
const NATIVE_EFFECT_APPROVAL_LIST: u8 = 52;
const NATIVE_EFFECT_APPROVAL_GET: u8 = 53;
const NATIVE_EFFECT_APPROVAL_DECIDE: u8 = 54;
const NATIVE_EFFECT_APPROVAL_MATERIAL: u8 = 55;
const NATIVE_EFFECT_APPROVAL_BUDGET: u8 = 56;
const AGENT_BUDGET_PROOF: u8 = 57;
const NATIVE_PROGRAM_APPROVAL_LIST: u8 = 58;
const NATIVE_PROGRAM_APPROVAL_GET: u8 = 59;
const NATIVE_PROGRAM_APPROVAL_MATERIAL: u8 = 60;
const NATIVE_PROGRAM_APPROVAL_BUDGET: u8 = 61;
const NATIVE_PROGRAM_APPROVAL_DECIDE: u8 = 62;
const APPROVAL_LIST: u8 = 9;
const APPROVAL_GET: u8 = 10;
const APPROVAL_APPROVE: u8 = 11;
const APPROVAL_REJECT: u8 = 12;
const BALANCE: u8 = 6;
const NATIVE_FEE_POLICY: u8 = 39;
const SESSION_FEE_STATE: u8 = 40;
const SESSION_SEED_PREPARE: u8 = 41;
const ACCOUNT_STATE: u8 = 42;
const HEAD: u8 = 7;
const EVIDENCE: u8 = 8;
const ACCOUNT_SEQUENCE: u8 = 13;
const IDENTITY_RESOLVE: u8 = 20;
const LEASE_MAP: u8 = 21;
const OWNER_VALIDATE: u8 = 22;
const OWNER_INSTALL: u8 = 23;
const AGENT_LIST: u8 = 24;
const AGENT_GET: u8 = 25;
const AGENT_CONTROL: u8 = 26;
const AGENT_LIMIT: u8 = 27;
const AGENT_JOURNEY: u8 = 28;
const AGENT_ARCHIVE: u8 = 29;
const CAPABILITY_INSTALL: u8 = 30;
const AGENT_CONTEXT: u8 = 31;
const AGENT_BUDGET_STATE: u8 = 32;
const AGENT_KEY_POLICY: u8 = 33;
const AGENT_SESSION_SNAPSHOT: u8 = 34;
const AGENT_SESSION_SUSPEND: u8 = 35;
const AGENT_SESSION_BIND: u8 = 36;
const AGENT_LIFECYCLE_PUBLISH: u8 = 37;
const AGENT_SESSION_RESTRICT: u8 = 38;
const MAX_TEXT: usize = 255;
const MAX_BYTES: usize = 1_048_576;
const MAX_EVIDENCE: usize = 64;
const MAX_TRANSITIONS: usize = 64;

/// Production journey adapter. Every call opens one deadline-bounded framed
/// UDS exchange; no socket, request buffer, or untrusted response is retained.
pub struct AgentRuntime {
    endpoint: PathBuf,
    gate: ConnectionGate,
    limits: Limits,
    registry: ModuleRegistry,
    subject: Option<Subject>,
    native_journey: Option<(super::native_send::HumanOwnerNativeContextV1, u64)>,
}

#[derive(Clone)]
struct Subject {
    principal: String,
    owner: layerx_types::ids::Did,
    account: layerx_types::account::AccountId,
    account_id: [u8; 32],
    asset_id: [u8; 32],
}

impl AgentRuntime {
    /// Creates an isolated handle carrying the principal's provider-bound owner.
    ///
    /// # Errors
    /// Refuses nested scopes and noncanonical or foreign owner account names.
    pub fn for_subject(
        &self,
        principal: &crate::store::PrincipalId,
        owner: &layerx_types::ids::Did,
        account: &layerx_types::account::AccountId,
        asset_id: [u8; 32],
    ) -> Result<Self, AgentBoundaryError> {
        if self.subject.is_some() || asset_id == [0; 32] {
            return Err(AgentBoundaryError::Refused);
        }
        let owner_text =
            std::str::from_utf8(owner.as_bytes()).map_err(|_| AgentBoundaryError::Refused)?;
        let account_name = account.canonical();
        if !account_name.starts_with(&format!("agent:{owner_text}:")) {
            return Err(AgentBoundaryError::Refused);
        }
        let account_id = layerx_intents::canonical::account_id_for_protocol(account, 3)
            .map_err(|_| AgentBoundaryError::Refused)?;
        Ok(Self {
            endpoint: self.endpoint.clone(),
            gate: self.gate.clone(),
            limits: self.limits,
            registry: self.registry.clone(),
            native_journey: None,
            subject: Some(Subject {
                principal: principal.as_str().to_owned(),
                owner: owner.clone(),
                account: account.clone(),
                account_id,
                asset_id,
            }),
        })
    }

    fn scoped_request(&self, request: &[u8]) -> Result<Zeroizing<Vec<u8>>, AgentBoundaryError> {
        let Some(subject) = &self.subject else {
            return Ok(Zeroizing::new(request.to_vec()));
        };
        if request.len() < 9 || &request[..8] != MAGIC || request[8] == 44 {
            return Err(AgentBoundaryError::Refused);
        }
        let mut writer = Writer::new(44);
        writer.text(&subject.principal)?;
        writer.text(
            std::str::from_utf8(subject.owner.as_bytes())
                .map_err(|_| AgentBoundaryError::Refused)?,
        )?;
        writer.bytes(subject.account.canonical().as_bytes())?;
        writer.fixed(&subject.asset_id);
        writer.bytes(request)?;
        let bytes = writer.finish_secret();
        if bytes.len() > MAX_BYTES {
            return Err(AgentBoundaryError::Refused);
        }
        Ok(bytes)
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum NativeApprovalFactState {
    Awaiting,
    Granted,
    Rejected,
    Expired,
    Defective,
    NotRequired,
}

pub struct NativeApprovalFacts {
    pub approval_id: [u8; 32],
    pub held_digest: [u8; 32],
    pub owner: String,
    pub actor: String,
    pub activity_module: u16,
    pub activity_ordinal: u16,
    pub state: NativeApprovalFactState,
    pub created_at_sequence: u64,
    pub budget_expiry_sequence: u64,
    pub created_at_unix_seconds: u64,
    pub activity_expires_at_unix_milliseconds: u64,
    pub submission_ref: Option<[u8; 32]>,
}

pub struct NativeApprovalFactsPage {
    pub approvals: Vec<NativeApprovalFacts>,
    pub next_cursor: Option<[u8; 32]>,
}

#[derive(Clone, Copy, Debug, Eq, Ord, PartialEq, PartialOrd)]
pub enum NativeProgramChargeKind {
    Principal = 1,
    ProgramSpend = 2,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct NativeProgramLimit {
    pub kind: NativeProgramChargeKind,
    pub source: [u8; 32],
    pub asset: [u8; 32],
    pub destination: Option<[u8; 32]>,
    pub maximum_amount: u128,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub enum NativeProgramApprovalSemantics {
    OperationOnly,
    AuthorizedLimits(Vec<NativeProgramLimit>),
}

pub struct NativeProgramApprovalFacts {
    pub approval_id: [u8; 32],
    pub held_digest: [u8; 32],
    pub owner: String,
    pub actor: String,
    pub activity_module: u16,
    pub activity_ordinal: u16,
    pub state: NativeApprovalFactState,
    pub created_at_sequence: u64,
    pub budget_expiry_sequence: u64,
    pub created_at_unix_seconds: u64,
    pub activity_expires_at_unix_milliseconds: u64,
    pub release_ref: Option<[u8; 32]>,
    pub fee_asset: Option<[u8; 32]>,
    pub canonical_payload_bytes: Vec<u8>,
    pub semantics: NativeProgramApprovalSemantics,
}

pub struct NativeProgramApprovalFactsPage {
    pub approvals: Vec<NativeProgramApprovalFacts>,
    pub next_cursor: Option<[u8; 32]>,
}

pub struct NativeProgramApprovalMaterial {
    pub approval_id: [u8; 32],
    pub held_digest: [u8; 32],
    pub owner: String,
    pub provenance: NativeEffectMaterialProvenance,
    pub canonical_unsigned_bytes: Vec<u8>,
    pub immutable_carrier_bytes: Vec<u8>,
    pub canonical_budget_bytes: Vec<u8>,
    pub actor: String,
    pub activity_ordinal: u16,
    pub canonical_payload_bytes: Vec<u8>,
    pub fee_limit: u128,
    pub activity_expires_at_unix_milliseconds: u64,
}

impl NativeProgramApprovalMaterial {
    pub fn validate_facts(
        &self,
        facts: &NativeProgramApprovalFacts,
        registry: &ModuleRegistry,
    ) -> Result<(), AgentBoundaryError> {
        let activity =
            layerx_wire::activity::decode_unsigned(&self.canonical_unsigned_bytes, registry)
                .map_err(|_| AgentBoundaryError::CorruptResponse)?;
        if self.approval_id != facts.approval_id
            || self.held_digest != facts.held_digest
            || self.owner != facts.owner
            || <[u8; 32]>::from(sha2::Sha256::digest(&self.canonical_unsigned_bytes))
                != self.approval_id
            || <[u8; 32]>::from(sha2::Sha256::digest(&self.immutable_carrier_bytes))
                != self.held_digest
            || activity.protocol_version() != 3
            || activity.activity_type().module() != ModuleId::Programs
            || layerx_wire::activity::encode_unsigned(&activity)
                .map_err(|_| AgentBoundaryError::CorruptResponse)?
                != self.canonical_unsigned_bytes
            || activity.activity_type().module() as u16 != facts.activity_module
            || activity.activity_type().ordinal() != facts.activity_ordinal
            || activity.actor_did() != facts.actor.as_bytes()
            || activity.payload() != facts.canonical_payload_bytes
            || activity.timestamp_bound().not_after != facts.activity_expires_at_unix_milliseconds
        {
            return Err(AgentBoundaryError::CorruptResponse);
        }
        let carrier: serde_json::Value = serde_json::from_slice(&self.immutable_carrier_bytes)
            .map_err(|_| AgentBoundaryError::CorruptResponse)?;
        if carrier
            .get("created_at_sequence")
            .and_then(serde_json::Value::as_u64)
            != Some(facts.created_at_sequence)
            || carrier
                .get("budget_expiry_sequence")
                .and_then(serde_json::Value::as_u64)
                != Some(facts.budget_expiry_sequence)
            || carrier
                .get("envelope_not_after")
                .and_then(serde_json::Value::as_u64)
                != Some(facts.activity_expires_at_unix_milliseconds)
        {
            return Err(AgentBoundaryError::CorruptResponse);
        }
        let body_len = self
            .canonical_budget_bytes
            .len()
            .checked_sub(32)
            .ok_or(AgentBoundaryError::CorruptResponse)?;
        let (body, digest) = self.canonical_budget_bytes.split_at(body_len);
        let expected: [u8; 32] = sha2::Sha256::new()
            .chain_update(b"layerx:program-budget:v2\0")
            .chain_update(body)
            .finalize()
            .into();
        if digest != expected {
            return Err(AgentBoundaryError::CorruptResponse);
        }
        let mut reader = Reader::new(body.to_vec());
        if reader.fixed::<5>()? != *b"LXPB\x02" {
            return Err(AgentBoundaryError::CorruptResponse);
        }
        let legacy_len =
            usize::try_from(reader.u32()?).map_err(|_| AgentBoundaryError::CorruptResponse)?;
        let legacy = reader.take(legacy_len)?;
        let legacy_body_len = legacy
            .len()
            .checked_sub(32)
            .ok_or(AgentBoundaryError::CorruptResponse)?;
        let (legacy_body, legacy_digest) = legacy.split_at(legacy_body_len);
        let expected: [u8; 32] = sha2::Sha256::new()
            .chain_update(b"layerx:program-budget:v1\0")
            .chain_update(legacy_body)
            .finalize()
            .into();
        if !legacy_body.starts_with(b"LXPB\x01") || legacy_digest != expected {
            return Err(AgentBoundaryError::CorruptResponse);
        }
        let mut legacy = Reader::new(legacy_body.to_vec());
        if legacy.fixed::<5>()? != *b"LXPB\x01"
            || legacy.fixed::<32>()? != facts.approval_id
            || legacy.u64()? != facts.budget_expiry_sequence
        {
            return Err(AgentBoundaryError::CorruptResponse);
        }
        let actor = layerx_types::ids::Did::new(facts.actor.as_bytes())
            .map_err(|_| AgentBoundaryError::CorruptResponse)?;
        let actor_id = layerx_wire::hash::did_id_for_protocol(&actor, 3)
            .map_err(|_| AgentBoundaryError::CorruptResponse)?;
        if reader.fixed::<32>()? != facts.approval_id
            || reader.fixed::<32>()? != actor_id
            || reader.fixed::<32>()? == [0; 32]
        {
            return Err(AgentBoundaryError::CorruptResponse);
        }
        reader.u64()?;
        if reader.u64()? != facts.created_at_sequence {
            return Err(AgentBoundaryError::CorruptResponse);
        }
        let count = usize::from(reader.u16()?);
        if count == 0 || count > 256 {
            return Err(AgentBoundaryError::CorruptResponse);
        }
        let mut limits = Vec::new();
        let mut previous = None;
        let mut fee = None;
        for _ in 0..count {
            let kind = reader.u8()?;
            let source = reader.fixed::<32>()?;
            let asset = reader.fixed::<32>()?;
            let destination = match (reader.u8()?, reader.fixed::<32>()?) {
                (0, value) if value == [0; 32] => None,
                (1, value) if value != [0; 32] => Some(value),
                _ => return Err(AgentBoundaryError::CorruptResponse),
            };
            let maximum_amount = reader.u128()?;
            let key = (asset, source, kind, destination);
            if source == [0; 32]
                || asset == [0; 32]
                || maximum_amount == 0
                || previous.is_some_and(|old| old >= key)
            {
                return Err(AgentBoundaryError::CorruptResponse);
            }
            previous = Some(key);
            let limit_count = usize::from(reader.u16()?);
            if limit_count == 0 || limit_count > 1024 {
                return Err(AgentBoundaryError::CorruptResponse);
            }
            let mut previous_limit = None;
            for _ in 0..limit_count {
                let limit = reader.fixed::<16>()?;
                if previous_limit.is_some_and(|old| old >= limit) {
                    return Err(AgentBoundaryError::CorruptResponse);
                }
                previous_limit = Some(limit);
            }
            if kind == 1 || kind == 3 {
                let main =
                    layerx_types::account::AccountId::parse(&format!("agent:{}:main", facts.actor))
                        .map_err(|_| AgentBoundaryError::CorruptResponse)?;
                let asset_hex: String = asset.iter().map(|byte| format!("{byte:02x}")).collect();
                let asset_account = layerx_types::account::AccountId::parse(&format!(
                    "agent:{}:asset:{asset_hex}",
                    facts.actor
                ))
                .map_err(|_| AgentBoundaryError::CorruptResponse)?;
                let main_id = layerx_wire::hash::account_id_for_protocol(&main, 3)
                    .map_err(|_| AgentBoundaryError::CorruptResponse)?;
                let asset_id = layerx_wire::hash::account_id_for_protocol(&asset_account, 3)
                    .map_err(|_| AgentBoundaryError::CorruptResponse)?;
                if source != main_id && source != asset_id {
                    return Err(AgentBoundaryError::CorruptResponse);
                }
            }
            match kind {
                3 if destination.is_none()
                    && maximum_amount == activity.fee_limit()
                    && fee.is_none() =>
                {
                    fee = Some(asset)
                }
                1 | 2 if destination.is_some() => limits.push(NativeProgramLimit {
                    kind: if kind == 1 {
                        NativeProgramChargeKind::Principal
                    } else {
                        NativeProgramChargeKind::ProgramSpend
                    },
                    source,
                    asset,
                    destination,
                    maximum_amount,
                }),
                _ => return Err(AgentBoundaryError::CorruptResponse),
            }
        }
        reader.finish()?;
        let semantics = if limits.is_empty() {
            NativeProgramApprovalSemantics::OperationOnly
        } else {
            NativeProgramApprovalSemantics::AuthorizedLimits(limits)
        };
        if fee != facts.fee_asset
            || fee.is_some() != (activity.fee_limit() != 0)
            || semantics != facts.semantics
        {
            return Err(AgentBoundaryError::CorruptResponse);
        }
        Ok(())
    }
}

pub struct NativeProgramApprovalBudget {
    pub approval_id: [u8; 32],
    pub held_digest: [u8; 32],
    pub owner: String,
    pub budget_id: [u8; 32],
    pub asset: [u8; 32],
    pub source_account: [u8; 32],
    pub observed_at_sequence: u64,
    pub remaining: u128,
    pub terminal: bool,
    pub verification: Level,
    pub evidence_digest: [u8; 32],
    pub receipt_digest: [u8; 32],
    pub checkpoint_digest: [u8; 32],
    pub age_sequences: u64,
    pub maximum_age_sequences: u64,
    pub proof_digest: [u8; 32],
    pub verified_proof_bytes: Vec<u8>,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum NativeEffectApprovalFactState {
    Awaiting,
    Granted,
    Rejected,
    Expired,
    NotRequired,
}
pub type NativeEffectCounterparty = disclosure::Counterparty;
pub type NativeEffectAmount = disclosure::DisclosedAmount;
pub struct NativeEffectApprovalFacts {
    pub approval_id: [u8; 32],
    pub held_digest: [u8; 32],
    pub owner: String,
    pub actor: String,
    pub activity_module: u16,
    pub activity_ordinal: u16,
    pub state: NativeEffectApprovalFactState,
    pub requires_approval: bool,
    pub created_at_sequence: u64,
    pub budget_expiry_sequence: u64,
    pub created_at_unix_seconds: u64,
    pub activity_expires_at_unix_milliseconds: u64,
    pub asset: [u8; 32],
    pub fee_asset: [u8; 32],
    pub fee_limit: u128,
    pub counterparties: Vec<NativeEffectCounterparty>,
    pub amounts: Vec<NativeEffectAmount>,
    pub release_ref: Option<[u8; 32]>,
    pub submission_ref: Option<[u8; 32]>,
}
pub struct NativeEffectApprovalFactsPage {
    pub approvals: Vec<NativeEffectApprovalFacts>,
    pub next_cursor: Option<[u8; 32]>,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum NativeEffectMaterialProvenance {
    LocalOwned,
}
pub struct NativeEffectApprovalMaterial {
    pub approval_id: [u8; 32],
    pub held_digest: [u8; 32],
    pub owner: String,
    pub provenance: NativeEffectMaterialProvenance,
    pub canonical_unsigned_bytes: Vec<u8>,
    pub immutable_carrier_bytes: Vec<u8>,
    pub canonical_budget_bytes: Vec<u8>,
}
pub struct NativeEffectBudgetRow {
    pub asset: [u8; 32],
    pub budget_id: [u8; 32],
    pub source_account: [u8; 32],
    pub remaining: u128,
    pub verification: Level,
    pub evidence_digest: [u8; 32],
    pub receipt_digest: [u8; 32],
    pub checkpoint_digest: [u8; 32],
    pub age_sequences: u64,
    pub maximum_age_sequences: u64,
}
pub struct NativeEffectApprovalBudget {
    pub approval_id: [u8; 32],
    pub held_digest: [u8; 32],
    pub owner: String,
    pub observed_at_sequence: u64,
    pub fee_asset: [u8; 32],
    pub rows: Vec<NativeEffectBudgetRow>,
}

pub struct AgentApprovalFacts {
    pub approval: crate::approvals::AgentApprovalRecord,
    pub created_at_unix_seconds: u64,
    pub activity_expires_at_unix_seconds: u64,
}
pub struct AgentApprovalFactsPage {
    pub approvals: Vec<AgentApprovalFacts>,
    pub next_cursor: Option<[u8; 32]>,
}
pub struct ManagedReceiptExport {
    pub canonical_bytes: Vec<u8>,
    pub digest: [u8; 32],
    pub activity_id: [u8; 32],
    pub global_sequence: u64,
    pub verification: u8,
}
pub struct AgentApprovalPage {
    pub approvals: Vec<crate::approvals::AgentApprovalRecord>,
    pub next_cursor: Option<[u8; 32]>,
}

pub struct NativeFeePolicy {
    pub version: u8,
    pub asset_id: [u8; 32],
    pub currency: String,
    pub decimals: u8,
}

pub struct VerifiedBalance {
    pub account: [u8; 32],
    pub asset: [u8; 32],
    pub currency: String,
    pub observed_at: String,
    pub age_seconds: u64,
    pub amount: u128,
    pub verification: u8,
    pub global_sequence: u64,
    pub batch_number: u64,
    pub observed_head_sequence: u64,
    pub observed_checkpoint: [u8; 32],
    pub canonical_bytes: Vec<u8>,
    pub proof_material: Vec<u8>,
}

pub struct AgentHead {
    pub chain_sequence: u64,
    pub sealed_batch: u64,
    pub finalised_checkpoint: [u8; 32],
}

pub struct AgentCoreIdentity {
    pub head_sequence: u64,
    pub revocation_sequence: u64,
    pub verification: u8,
    pub frozen: bool,
    pub authorities: Vec<(u8, [u8; 32])>,
    pub canonical_bytes: Vec<u8>,
}
pub struct AgentLease {
    pub not_before_sequence: u64,
    pub expiry_sequence: u64,
    pub observed_head_sequence: u64,
    pub canonical_attestation: Vec<u8>,
}
pub struct AgentOwnerValidation {
    pub identity_head_sequence: u64,
    pub expiry_sequence: u64,
    pub observed_head_sequence: u64,
    pub canonical_identity: Vec<u8>,
}
pub struct AgentOwnerInstalled {
    pub token_id: AgentSessionToken,
    pub session_id: [u8; 32],
    pub generation: u64,
    pub expiry_sequence: u64,
    pub observed_head_sequence: u64,
}
pub struct AgentSessionToken(Zeroizing<[u8; 32]>);
impl AgentSessionToken {
    fn new(value: [u8; 32]) -> Result<Self, AgentBoundaryError> {
        if value == [0; 32] {
            Err(AgentBoundaryError::CorruptResponse)
        } else {
            Ok(Self(Zeroizing::new(value)))
        }
    }

    #[must_use]
    pub fn expose(&self) -> [u8; 32] {
        *self.0
    }
}
pub struct AgentSessionSeed(Zeroizing<[u8; 32]>);
impl AgentSessionSeed {
    /// # Errors
    /// Refuses a zero session seed.
    pub fn new(seed: [u8; 32]) -> Result<Self, AgentBoundaryError> {
        if seed == [0; 32] {
            Err(AgentBoundaryError::Refused)
        } else {
            Ok(Self(Zeroizing::new(seed)))
        }
    }
    pub(crate) fn expose(&self) -> &[u8; 32] {
        &self.0
    }
}
impl std::fmt::Debug for AgentSessionSeed {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str("AgentSessionSeed([redacted])")
    }
}
pub struct AgentOwnerInstall {
    pub agent: String,
    pub authority_kind: u8,
    pub authority_id: [u8; 32],
    pub session_id: [u8; 32],
    pub token_id: [u8; 32],
    pub session_public_key: [u8; 32],
    pub registration_payload: Vec<u8>,
    pub grantor: [u8; 32],
    pub grant_not_before: u64,
    pub grant_expires_at: u64,
    pub grant_revocation_sequence: u64,
    pub session_seed: Option<AgentSessionSeed>,
    pub permitted_activity_types: Vec<u16>,
    pub scopes: Vec<String>,
    pub lease_not_before_unix_ms: u64,
    pub lease_not_after_unix_ms: u64,
    pub opening_client: String,
    pub policy_version: String,
    pub lifecycle: Option<AgentLifecycleSeed>,
}
pub struct AgentLifecycleSeed {
    pub agent_id: String,
    pub name: String,
    pub purpose: String,
    pub currency: String,
    pub monthly_limit: u128,
    pub period_start: u64,
    pub period_end: u64,
    pub created_at: u64,
    pub updated_at: u64,
    pub verified_evidence: Vec<[u8; 32]>,
    pub actor: String,
    pub primary_authority: String,
    pub custody_key: String,
    pub custody_public_key: [u8; 32],
    pub owner_account: String,
    pub budget_account: String,
    pub budget_asset: [u8; 32],
    pub purpose_hash: [u8; 32],
    pub recovery_root: [u8; 32],
    pub recovery_threshold: u16,
    pub capability_id: [u8; 32],
    pub activity_types: Vec<u32>,
    pub counterparties: Vec<[u8; 32]>,
    pub assets: Vec<[u8; 32]>,
    pub amount_ceiling: u128,
    pub rate_maximum_uses: u64,
    pub rate_window_sequences: u64,
    pub purposes: Vec<String>,
    pub capability_expiry_sequence: u64,
    pub session_scopes: Vec<String>,
    pub session_expiry_unix_seconds: u64,
    pub protocol_grant_id: [u8; 32],
    pub budget_period_seconds: u64,
    pub budget_expiry_seconds: u64,
    pub initial_funding: u128,
    pub network_id: u32,
    pub creation_receipt_roots: Vec<[u8; 32]>,
}
pub struct AgentCapabilityInstall {
    pub action_key: [u8; 32],
    pub agent: String,
    pub authority_id: [u8; 32],
    pub capability_id: [u8; 32],
    pub activity_types: Vec<u16>,
    pub counterparties: Vec<[u8; 32]>,
    pub assets: Vec<[u8; 32]>,
    pub amount_ceiling: u128,
    pub rate_maximum_uses: u64,
    pub rate_window_sequences: u64,
    pub purposes: Vec<String>,
    pub expiry_sequence: u64,
}

pub struct ManagedAgentEvidence {
    pub evidence_id: String,
    pub class: String,
    pub verification: u8,
}
pub struct ManagedAgentView {
    pub agent_id: String,
    pub name: String,
    pub purpose: String,
    pub state: u8,
    pub monthly_limit: u128,
    pub currency: String,
    pub limit_enforcement: u8,
    pub period_start: String,
    pub period_end: String,
    pub spent: u128,
    pub remaining: u128,
    pub spend_verification: u8,
    pub created_at: String,
    pub updated_at: String,
    pub evidence: Vec<ManagedAgentEvidence>,
}
pub struct ManagedAgentPage {
    pub agents: Vec<ManagedAgentView>,
    pub next_cursor: Option<[u8; 32]>,
}
pub struct ManagedAgentJourneyStage {
    pub stage_id: String,
    pub copy_key: String,
    pub state: u8,
    pub evidence: Vec<ManagedAgentEvidence>,
}
pub struct ManagedAgentJourney {
    pub journey_id: String,
    pub kind: String,
    pub state: u8,
    pub stages: Vec<ManagedAgentJourneyStage>,
    pub started_at: String,
    pub updated_at: String,
    pub evidence: Vec<ManagedAgentEvidence>,
}
pub struct ManagedAgentChallenge {
    pub agent_id: String,
    pub kind: u8,
    pub delay_seconds: u64,
    pub ready_at: String,
    pub evidence: Vec<ManagedAgentEvidence>,
}
#[derive(Clone, Copy)]
pub struct AgentFinalizationEvidence {
    pub action_key: [u8; 32],
    pub activity_id: [u8; 32],
    pub receipt_digest: [u8; 32],
    pub observed_sequence: u64,
    pub verification: u8,
    pub finalized_at: u64,
}
pub struct AgentLifecycleContext {
    pub seed: AgentLifecycleSeed,
    pub agent_did: String,
    pub session_id: [u8; 32],
    pub session_token_id: AgentSessionToken,
    pub session_generation: u64,
    pub protocol_grant_id: [u8; 32],
    pub active_budget_id: [u8; 32],
    pub state: u8,
    pub current_monthly_limit: u128,
    pub spent: u128,
    pub updated_at: u64,
}
pub struct AgentBudgetState {
    pub active_budget_id: [u8; 32],
    pub revocation_sequence: u64,
    pub observed_head_sequence: u64,
    pub verification: u8,
    pub evidence_digest: [u8; 32],
    pub receipt_digest: [u8; 32],
    pub checkpoint_digest: [u8; 32],
    pub age_sequences: u64,
    pub maximum_age_sequences: u64,
    pub remaining: u128,
    pub asset: [u8; 32],
}

pub struct AgentBudgetProof {
    pub owner: String,
    pub budget_id: [u8; 32],
    pub asset: [u8; 32],
    pub source_account: [u8; 32],
    pub observed_head_sequence: u64,
    pub remaining: u128,
    pub verification: Level,
    pub evidence_digest: [u8; 32],
    pub receipt_digest: [u8; 32],
    pub checkpoint_digest: [u8; 32],
    pub age_sequences: u64,
    pub maximum_age_sequences: u64,
    pub digest: [u8; 32],
    pub canonical_export_bytes: Vec<u8>,
}
pub struct AgentKeyPolicy {
    pub agent_did: String,
    pub recovery: bool,
    pub policy_revision: u64,
    pub required_delay_seconds: u64,
    pub maximum_delay_seconds: u64,
    pub effective_sequence: u64,
    pub observed_head_sequence: u64,
    pub verification: u8,
    pub evidence_digest: [u8; 32],
    pub checkpoint_digest: [u8; 32],
    pub age_sequences: u64,
    pub maximum_age_sequences: u64,
}
pub struct AgentSessionSnapshot {
    pub agent_id: String,
    pub agent_did: String,
    pub session_id: [u8; 32],
    pub token_id: AgentSessionToken,
    pub generation: u64,
    pub open: bool,
    pub expiry_sequence: u64,
    pub sequence: u64,
}
pub struct AgentSessionObservation {
    pub agent_id: String,
    pub agent_did: String,
    pub session_id: [u8; 32],
    pub token_id: AgentSessionToken,
    pub generation: u64,
    pub open: bool,
    pub action_key: [u8; 32],
    pub evidence_digest: [u8; 32],
}

impl AgentOwnerInstall {
    /// # Errors
    /// Refuses fields or collections exceeding the canonical encoding bounds.
    pub fn body_digest(&self) -> Result<[u8; 32], AgentBoundaryError> {
        let mut digest = sha2::Sha256::new();
        digest.update(b"layerx-human-owner-install/v2");
        digest_text(&mut digest, self.agent.as_bytes())?;
        digest.update([self.authority_kind]);
        digest.update(self.authority_id);
        digest.update(self.session_id);
        digest.update(self.token_id);
        digest.update(self.session_public_key);
        digest_text(&mut digest, &self.registration_payload)?;
        digest.update(self.grantor);
        digest.update(self.grant_not_before.to_be_bytes());
        digest.update(self.grant_expires_at.to_be_bytes());
        digest.update(self.grant_revocation_sequence.to_be_bytes());
        digest.update(
            u16::try_from(self.permitted_activity_types.len())
                .map_err(|_| AgentBoundaryError::Refused)?
                .to_be_bytes(),
        );
        for value in &self.permitted_activity_types {
            digest.update(value.to_be_bytes());
        }
        digest.update(
            u16::try_from(self.scopes.len())
                .map_err(|_| AgentBoundaryError::Refused)?
                .to_be_bytes(),
        );
        for value in &self.scopes {
            digest_text(&mut digest, value.as_bytes())?;
        }
        digest.update(self.lease_not_before_unix_ms.to_be_bytes());
        digest.update(self.lease_not_after_unix_ms.to_be_bytes());
        digest_text(&mut digest, self.opening_client.as_bytes())?;
        digest_text(&mut digest, self.policy_version.as_bytes())?;
        match &self.lifecycle {
            None => digest.update([0]),
            Some(value) => {
                digest.update([1]);
                digest_lifecycle(&mut digest, value)?;
            }
        }
        Ok(digest.finalize().into())
    }
}
impl AgentLifecycleSeed {
    /// # Errors
    /// Refuses fields or collections exceeding the canonical encoding bounds.
    pub fn body_digest(&self) -> Result<[u8; 32], AgentBoundaryError> {
        let mut wire = Writer::new(0);
        encode_lifecycle(&mut wire, self)?;
        let mut digest = sha2::Sha256::new();
        digest.update(b"layerx-human-agent-lifecycle-publish/v1");
        digest.update(&wire.0[10..]);
        Ok(digest.finalize().into())
    }
}

fn digest_text(digest: &mut sha2::Sha256, value: &[u8]) -> Result<(), AgentBoundaryError> {
    digest.update(
        u32::try_from(value.len())
            .map_err(|_| AgentBoundaryError::Refused)?
            .to_be_bytes(),
    );
    digest.update(value);
    Ok(())
}

fn digest_lifecycle(
    digest: &mut sha2::Sha256,
    value: &AgentLifecycleSeed,
) -> Result<(), AgentBoundaryError> {
    for text in [
        &value.agent_id,
        &value.name,
        &value.purpose,
        &value.currency,
    ] {
        digest_text(digest, text.as_bytes())?;
    }
    digest.update(value.monthly_limit.to_be_bytes());
    digest.update(value.period_start.to_be_bytes());
    digest.update(value.period_end.to_be_bytes());
    digest.update(value.created_at.to_be_bytes());
    digest.update(value.updated_at.to_be_bytes());
    digest.update(
        u16::try_from(value.verified_evidence.len())
            .map_err(|_| AgentBoundaryError::Refused)?
            .to_be_bytes(),
    );
    for evidence in &value.verified_evidence {
        digest.update(evidence);
    }
    for text in [&value.actor, &value.primary_authority, &value.custody_key] {
        digest_text(digest, text.as_bytes())?;
    }
    digest.update(value.custody_public_key);
    digest_text(digest, value.owner_account.as_bytes())?;
    digest_text(digest, value.budget_account.as_bytes())?;
    digest.update(value.budget_asset);
    digest.update(value.purpose_hash);
    digest.update(value.recovery_root);
    digest.update(value.recovery_threshold.to_be_bytes());
    digest.update(value.capability_id);
    digest.update(
        u16::try_from(value.activity_types.len())
            .map_err(|_| AgentBoundaryError::Refused)?
            .to_be_bytes(),
    );
    for item in &value.activity_types {
        digest.update(item.to_be_bytes());
    }
    for values in [&value.counterparties, &value.assets] {
        digest.update(
            u16::try_from(values.len())
                .map_err(|_| AgentBoundaryError::Refused)?
                .to_be_bytes(),
        );
        for item in values {
            digest.update(item);
        }
    }
    digest.update(value.amount_ceiling.to_be_bytes());
    digest.update(value.rate_maximum_uses.to_be_bytes());
    digest.update(value.rate_window_sequences.to_be_bytes());
    digest.update(
        u16::try_from(value.purposes.len())
            .map_err(|_| AgentBoundaryError::Refused)?
            .to_be_bytes(),
    );
    for item in &value.purposes {
        digest_text(digest, item.as_bytes())?;
    }
    digest.update(value.capability_expiry_sequence.to_be_bytes());
    digest.update(
        u16::try_from(value.session_scopes.len())
            .map_err(|_| AgentBoundaryError::Refused)?
            .to_be_bytes(),
    );
    for item in &value.session_scopes {
        digest_text(digest, item.as_bytes())?;
    }
    digest.update(value.session_expiry_unix_seconds.to_be_bytes());
    digest.update(value.protocol_grant_id);
    digest.update(value.budget_period_seconds.to_be_bytes());
    digest.update(value.budget_expiry_seconds.to_be_bytes());
    digest.update(value.initial_funding.to_be_bytes());
    digest.update(value.network_id.to_be_bytes());
    digest.update(
        u16::try_from(value.creation_receipt_roots.len())
            .map_err(|_| AgentBoundaryError::Refused)?
            .to_be_bytes(),
    );
    for item in &value.creation_receipt_roots {
        digest.update(item);
    }
    Ok(())
}

impl AgentRuntime {
    /// # Errors
    /// Returns a boundary refusal for invalid request fields, an unavailable transport, or a malformed response.
    pub fn publish_lifecycle(
        &mut self,
        request_id: u64,
        key: [u8; 32],
        seed: &AgentLifecycleSeed,
    ) -> Result<(), AgentBoundaryError> {
        let mut writer = Writer::new(AGENT_LIFECYCLE_PUBLISH);
        writer.u64(request_id);
        writer.fixed(&key);
        writer.fixed(&seed.body_digest()?);
        let tag = writer.0.len();
        encode_lifecycle(&mut writer, seed)?;
        writer.0.remove(tag);
        let mut reader = self.exchange(&writer.finish())?;
        if reader.u8()? != 1 {
            return Err(AgentBoundaryError::CorruptResponse);
        }
        reader.finish()
    }
    /// # Errors
    /// Returns a boundary refusal for invalid request fields, an unavailable transport, or a malformed response.
    pub fn capability_install(
        &mut self,
        request: &AgentCapabilityInstall,
    ) -> Result<([u8; 32], u64, u8, [u8; 32]), AgentBoundaryError> {
        if request.action_key == [0; 32]
            || request.activity_types.is_empty()
            || request.counterparties.is_empty()
            || request.assets.is_empty()
            || request.purposes.is_empty()
            || request.amount_ceiling == 0
            || request.rate_maximum_uses == 0
            || request.rate_window_sequences == 0
            || request.expiry_sequence == 0
        {
            return Err(AgentBoundaryError::Refused);
        }
        let mut writer = Writer::new(CAPABILITY_INSTALL);
        writer.fixed(&request.action_key);
        writer.text(&request.agent)?;
        writer.fixed(&request.authority_id);
        writer.fixed(&request.capability_id);
        writer.u16(
            u16::try_from(request.activity_types.len()).map_err(|_| AgentBoundaryError::Refused)?,
        );
        for value in &request.activity_types {
            writer.u16(*value);
        }
        writer.u16(
            u16::try_from(request.counterparties.len()).map_err(|_| AgentBoundaryError::Refused)?,
        );
        for value in &request.counterparties {
            writer.fixed(value);
        }
        writer.u16(u16::try_from(request.assets.len()).map_err(|_| AgentBoundaryError::Refused)?);
        for value in &request.assets {
            writer.fixed(value);
        }
        writer.u128(request.amount_ceiling);
        writer.u64(request.rate_maximum_uses);
        writer.u64(request.rate_window_sequences);
        writer.u16(u16::try_from(request.purposes.len()).map_err(|_| AgentBoundaryError::Refused)?);
        for value in &request.purposes {
            writer.text(value)?;
        }
        writer.u64(request.expiry_sequence);
        let mut reader = self.exchange(&writer.finish())?;
        let object_id = reader.fixed()?;
        let observed_sequence = reader.u64()?;
        let verification = reader.u8()?;
        let receipt_digest = reader.fixed()?;
        if object_id != request.capability_id
            || observed_sequence == 0
            || !(2..=5).contains(&verification)
            || receipt_digest == [0; 32]
        {
            return Err(AgentBoundaryError::CorruptResponse);
        }
        reader.finish()?;
        Ok((object_id, observed_sequence, verification, receipt_digest))
    }
    /// # Errors
    /// Returns a boundary refusal for invalid request fields, an unavailable transport, or a malformed response.
    pub fn agent_list(
        &mut self,
        cursor: Option<[u8; 32]>,
        limit: u8,
    ) -> Result<ManagedAgentPage, AgentBoundaryError> {
        if limit == 0 || limit > 100 {
            return Err(AgentBoundaryError::Refused);
        }
        let mut writer = Writer::new(AGENT_LIST);
        match cursor {
            Some(value) => {
                writer.u8(1);
                writer.fixed(&value);
            }
            None => writer.u8(0),
        }
        writer.u8(limit);
        let mut reader = self.exchange(&writer.finish())?;
        let count = usize::from(reader.u8()?);
        if count > usize::from(limit) {
            return Err(AgentBoundaryError::CorruptResponse);
        }
        let mut agents = Vec::with_capacity(count);
        for _ in 0..count {
            agents.push(decode_managed_agent(&mut reader)?);
        }
        let next_cursor = match reader.u8()? {
            0 => None,
            1 => Some(reader.fixed()?),
            _ => return Err(AgentBoundaryError::CorruptResponse),
        };
        reader.finish()?;
        Ok(ManagedAgentPage {
            agents,
            next_cursor,
        })
    }
    /// # Errors
    /// Returns a boundary refusal for invalid request fields, an unavailable transport, or a malformed response.
    pub fn agent_get(&mut self, agent_id: &str) -> Result<ManagedAgentView, AgentBoundaryError> {
        let mut writer = Writer::new(AGENT_GET);
        writer.text(agent_id)?;
        let mut reader = self.exchange(&writer.finish())?;
        let value = decode_managed_agent(&mut reader)?;
        reader.finish()?;
        Ok(value)
    }
    /// # Errors
    /// Returns a boundary refusal for invalid request fields, an unavailable transport, or a malformed response.
    pub fn agent_context(
        &mut self,
        agent_id: &str,
    ) -> Result<AgentLifecycleContext, AgentBoundaryError> {
        let mut writer = Writer::new(AGENT_CONTEXT);
        writer.text(agent_id)?;
        let mut reader = self.exchange(&writer.finish())?;
        let seed = decode_lifecycle(&mut reader)?;
        let value = AgentLifecycleContext {
            seed,
            agent_did: reader.text()?,
            session_id: reader.fixed()?,
            session_token_id: AgentSessionToken::new(reader.fixed()?)?,
            session_generation: reader.u64()?,
            protocol_grant_id: reader.fixed()?,
            active_budget_id: reader.fixed()?,
            state: reader.u8()?,
            current_monthly_limit: reader.u128()?,
            spent: reader.u128()?,
            updated_at: reader.u64()?,
        };
        if value.agent_did.is_empty()
            || value.session_id == [0; 32]
            || value.session_generation == 0
            || value.protocol_grant_id == [0; 32]
            || value.active_budget_id == [0; 32]
            || value.state > 4
            || value.current_monthly_limit == 0
            || value.spent > value.current_monthly_limit
        {
            return Err(AgentBoundaryError::CorruptResponse);
        }
        reader.finish()?;
        Ok(value)
    }
    /// # Errors
    /// Returns a boundary refusal for invalid request fields, an unavailable transport, or a malformed response.
    pub fn agent_budget_state(
        &mut self,
        active_budget_id: [u8; 32],
    ) -> Result<AgentBudgetState, AgentBoundaryError> {
        if active_budget_id == [0; 32] {
            return Err(AgentBoundaryError::Refused);
        }
        let mut writer = Writer::new(AGENT_BUDGET_STATE);
        writer.fixed(&active_budget_id);
        let mut reader = self.exchange(&writer.finish())?;
        let value = AgentBudgetState {
            active_budget_id: reader.fixed()?,
            revocation_sequence: reader.u64()?,
            observed_head_sequence: reader.u64()?,
            verification: reader.u8()?,
            evidence_digest: reader.fixed()?,
            receipt_digest: reader.fixed()?,
            checkpoint_digest: reader.fixed()?,
            age_sequences: reader.u64()?,
            maximum_age_sequences: reader.u64()?,
            remaining: reader.u128()?,
            asset: reader.fixed()?,
        };
        if value.active_budget_id != active_budget_id
            || value.revocation_sequence == 0
            || value.observed_head_sequence < value.revocation_sequence
            || !(4..=5).contains(&value.verification)
            || value.evidence_digest == [0; 32]
            || value.receipt_digest == [0; 32]
            || value.checkpoint_digest == [0; 32]
            || value.maximum_age_sequences == 0
            || value.age_sequences > value.maximum_age_sequences
            || value.asset == [0; 32]
        {
            return Err(AgentBoundaryError::CorruptResponse);
        }
        reader.finish()?;
        Ok(value)
    }
    pub fn agent_budget_proof(
        &mut self,
        budget_id: [u8; 32],
    ) -> Result<AgentBudgetProof, AgentBoundaryError> {
        if self.subject.is_none() || budget_id == [0; 32] {
            return Err(AgentBoundaryError::Refused);
        }
        let mut writer = Writer::new(AGENT_BUDGET_PROOF);
        writer.fixed(&budget_id);
        let mut reader = self.exchange(&writer.finish())?;
        if reader.u16()? != 4 {
            return Err(AgentBoundaryError::CorruptResponse);
        }
        let value = AgentBudgetProof {
            owner: reader.text()?,
            budget_id: reader.fixed()?,
            asset: reader.fixed()?,
            source_account: reader.fixed()?,
            observed_head_sequence: reader.u64()?,
            remaining: reader.u128()?,
            verification: decode_level(reader.u8()?)?,
            evidence_digest: reader.fixed()?,
            receipt_digest: reader.fixed()?,
            checkpoint_digest: reader.fixed()?,
            age_sequences: reader.u64()?,
            maximum_age_sequences: reader.u64()?,
            digest: reader.fixed()?,
            canonical_export_bytes: reader.bytes()?,
        };
        reader.finish()?;
        let actual_digest: [u8; 32] = sha2::Sha256::digest(&value.canonical_export_bytes).into();
        if self
            .subject
            .as_ref()
            .is_none_or(|subject| subject.owner.as_bytes() != value.owner.as_bytes())
            || value.budget_id != budget_id
            || value.asset == [0; 32]
            || value.source_account == [0; 32]
            || value.observed_head_sequence == 0
            || !matches!(
                value.verification,
                Level::CheckpointFinalised | Level::SettlementAnchored
            )
            || value.evidence_digest == [0; 32]
            || value.receipt_digest == [0; 32]
            || value.checkpoint_digest == [0; 32]
            || value.maximum_age_sequences == 0
            || value.age_sequences > value.maximum_age_sequences
            || actual_digest != value.digest
        {
            return Err(AgentBoundaryError::CorruptResponse);
        }
        Ok(value)
    }
    /// # Errors
    /// Returns a boundary refusal for invalid request fields, an unavailable transport, or a malformed response.
    pub fn agent_key_policy(
        &mut self,
        agent_did: &str,
        recovery: bool,
    ) -> Result<AgentKeyPolicy, AgentBoundaryError> {
        let mut writer = Writer::new(AGENT_KEY_POLICY);
        writer.text(agent_did)?;
        writer.u8(u8::from(recovery));
        let mut reader = self.exchange(&writer.finish())?;
        let value = AgentKeyPolicy {
            agent_did: reader.text()?,
            recovery: match reader.u8()? {
                0 => false,
                1 => true,
                _ => return Err(AgentBoundaryError::CorruptResponse),
            },
            policy_revision: reader.u64()?,
            required_delay_seconds: reader.u64()?,
            maximum_delay_seconds: reader.u64()?,
            effective_sequence: reader.u64()?,
            observed_head_sequence: reader.u64()?,
            verification: reader.u8()?,
            evidence_digest: reader.fixed()?,
            checkpoint_digest: reader.fixed()?,
            age_sequences: reader.u64()?,
            maximum_age_sequences: reader.u64()?,
        };
        if value.agent_did != agent_did
            || value.recovery != recovery
            || value.policy_revision == 0
            || value.required_delay_seconds == 0
            || value.required_delay_seconds > value.maximum_delay_seconds
            || value.effective_sequence == 0
            || value.observed_head_sequence < value.effective_sequence
            || !(4..=5).contains(&value.verification)
            || value.evidence_digest == [0; 32]
            || value.checkpoint_digest == [0; 32]
            || value.maximum_age_sequences == 0
            || value.age_sequences > value.maximum_age_sequences
        {
            return Err(AgentBoundaryError::CorruptResponse);
        }
        reader.finish()?;
        Ok(value)
    }
    /// # Errors
    /// Returns a boundary refusal for invalid request fields, an unavailable transport, or a malformed response.
    pub fn agent_session_snapshot(
        &mut self,
        agent_id: &str,
    ) -> Result<AgentSessionSnapshot, AgentBoundaryError> {
        let mut writer = Writer::new(AGENT_SESSION_SNAPSHOT);
        writer.text(agent_id)?;
        let mut reader = self.exchange(&writer.finish())?;
        let value = AgentSessionSnapshot {
            agent_id: reader.text()?,
            agent_did: reader.text()?,
            session_id: reader.fixed()?,
            token_id: AgentSessionToken::new(reader.fixed()?)?,
            generation: reader.u64()?,
            open: match reader.u8()? {
                0 => false,
                1 => true,
                _ => return Err(AgentBoundaryError::CorruptResponse),
            },
            expiry_sequence: reader.u64()?,
            sequence: reader.u64()?,
        };
        if value.agent_id != agent_id
            || value.agent_did.is_empty()
            || value.session_id == [0; 32]
            || value.generation == 0
            || value.expiry_sequence == 0
        {
            return Err(AgentBoundaryError::CorruptResponse);
        }
        reader.finish()?;
        Ok(value)
    }
    /// # Errors
    /// Returns a boundary refusal for invalid request fields, an unavailable transport, or a malformed response.
    pub fn agent_session_suspend(
        &mut self,
        agent_id: &str,
        action_key: [u8; 32],
    ) -> Result<AgentSessionObservation, AgentBoundaryError> {
        let mut writer = Writer::new(AGENT_SESSION_SUSPEND);
        writer.text(agent_id)?;
        writer.fixed(&action_key);
        self.session_observation(writer, agent_id, action_key, false)
    }
    /// # Errors
    /// Returns a boundary refusal for invalid request fields, an unavailable transport, or a malformed response.
    pub fn agent_session_bind(
        &mut self,
        agent_id: &str,
        session_id: [u8; 32],
        token_id: [u8; 32],
        action_key: [u8; 32],
    ) -> Result<AgentSessionObservation, AgentBoundaryError> {
        let mut writer = Writer::new(AGENT_SESSION_BIND);
        writer.text(agent_id)?;
        writer.fixed(&session_id);
        writer.fixed(&token_id);
        writer.fixed(&action_key);
        let value = self.session_observation(writer, agent_id, action_key, true)?;
        if value.session_id != session_id || value.token_id.expose() != token_id {
            return Err(AgentBoundaryError::CorruptResponse);
        }
        Ok(value)
    }
    /// # Errors
    /// Returns a boundary refusal for invalid request fields, an unavailable transport, or a malformed response.
    pub fn agent_session_restrict(
        &mut self,
        agent_id: &str,
        current_sequence: u64,
        action_key: [u8; 32],
        permitted_activity_types: &[u16],
        scopes: &[String],
    ) -> Result<AgentSessionObservation, AgentBoundaryError> {
        if permitted_activity_types.is_empty()
            || permitted_activity_types.len() > 256
            || scopes.is_empty()
            || scopes.len() > 64
        {
            return Err(AgentBoundaryError::Refused);
        }
        let mut writer = Writer::new(AGENT_SESSION_RESTRICT);
        writer.text(agent_id)?;
        writer.u64(current_sequence);
        writer.fixed(&action_key);
        writer.u16(
            u16::try_from(permitted_activity_types.len())
                .map_err(|_| AgentBoundaryError::Refused)?,
        );
        for activity_type in permitted_activity_types {
            writer.u16(*activity_type);
        }
        writer.u16(u16::try_from(scopes.len()).map_err(|_| AgentBoundaryError::Refused)?);
        for scope in scopes {
            writer.text(scope)?;
        }
        self.session_observation(writer, agent_id, action_key, true)
    }
    fn session_observation(
        &mut self,
        writer: Writer,
        agent_id: &str,
        action_key: [u8; 32],
        open: bool,
    ) -> Result<AgentSessionObservation, AgentBoundaryError> {
        if action_key == [0; 32] {
            return Err(AgentBoundaryError::Refused);
        }
        let mut reader = self.exchange(&writer.finish())?;
        let value = AgentSessionObservation {
            agent_id: reader.text()?,
            agent_did: reader.text()?,
            session_id: reader.fixed()?,
            token_id: AgentSessionToken::new(reader.fixed()?)?,
            generation: reader.u64()?,
            open: match reader.u8()? {
                0 => false,
                1 => true,
                _ => return Err(AgentBoundaryError::CorruptResponse),
            },
            action_key: reader.fixed()?,
            evidence_digest: reader.fixed()?,
        };
        if value.agent_id != agent_id
            || value.agent_did.is_empty()
            || value.open != open
            || value.action_key != action_key
            || value.session_id == [0; 32]
            || value.generation == 0
            || value.evidence_digest == [0; 32]
        {
            return Err(AgentBoundaryError::CorruptResponse);
        }
        reader.finish()?;
        Ok(value)
    }
    /// # Errors
    /// Returns a boundary refusal for invalid request fields, an unavailable transport, or a malformed response.
    pub fn agent_control(
        &mut self,
        agent_id: &str,
        resume: bool,
        session_observation: [u8; 32],
        evidence: AgentFinalizationEvidence,
    ) -> Result<ManagedAgentView, AgentBoundaryError> {
        if session_observation == [0; 32] {
            return Err(AgentBoundaryError::Refused);
        }
        let mut writer = Writer::new(AGENT_CONTROL);
        writer.text(agent_id)?;
        writer.u8(u8::from(resume));
        writer.fixed(&session_observation);
        encode_finalization(&mut writer, evidence)?;
        let mut reader = self.exchange(&writer.finish())?;
        let value = decode_managed_agent(&mut reader)?;
        reader.finish()?;
        Ok(value)
    }
    /// # Errors
    /// Returns a boundary refusal for invalid request fields, an unavailable transport, or a malformed response.
    pub fn agent_limit(
        &mut self,
        agent_id: &str,
        monthly_limit: u128,
        currency: &str,
        replacement_budget_id: [u8; 32],
        evidence: AgentFinalizationEvidence,
    ) -> Result<ManagedAgentView, AgentBoundaryError> {
        if monthly_limit == 0 || replacement_budget_id == [0; 32] {
            return Err(AgentBoundaryError::Refused);
        }
        let mut writer = Writer::new(AGENT_LIMIT);
        writer.text(agent_id)?;
        writer.u128(monthly_limit);
        writer.text(currency)?;
        writer.fixed(&replacement_budget_id);
        encode_finalization(&mut writer, evidence)?;
        let mut reader = self.exchange(&writer.finish())?;
        let value = decode_managed_agent(&mut reader)?;
        reader.finish()?;
        Ok(value)
    }
    /// # Errors
    /// Returns a boundary refusal for invalid request fields, an unavailable transport, or a malformed response.
    pub fn agent_reclaim(
        &mut self,
        agent_id: &str,
        amount: u128,
        currency: &str,
        pre_observation: [u8; 32],
        post_observation: [u8; 32],
        evidence: AgentFinalizationEvidence,
    ) -> Result<ManagedAgentJourney, AgentBoundaryError> {
        let mut writer = Writer::new(AGENT_JOURNEY);
        writer.u8(0);
        writer.text(agent_id)?;
        writer.u128(amount);
        writer.text(currency)?;
        writer.u64(0);
        writer.u64(0);
        writer.fixed(&pre_observation);
        writer.fixed(&post_observation);
        encode_finalization(&mut writer, evidence)?;
        let mut reader = self.exchange(&writer.finish())?;
        let value = decode_managed_journey(&mut reader)?;
        reader.finish()?;
        Ok(value)
    }
    /// # Errors
    /// Refuses unbound original rotation bytes or unavailable checkpoint evidence.
    pub fn agent_owner_rotated(
        &mut self,
        agent_id: &str,
        custody_key: &str,
        signed_activity: &[u8],
        evidence: AgentFinalizationEvidence,
    ) -> Result<ManagedAgentJourney, AgentBoundaryError> {
        let mut writer = Writer::new(43);
        writer.text(agent_id)?;
        writer.text(custody_key)?;
        writer.bytes(signed_activity)?;
        encode_finalization(&mut writer, evidence)?;
        let mut reader = self.exchange(&writer.finish())?;
        let value = decode_managed_journey(&mut reader)?;
        reader.finish()?;
        Ok(value)
    }
    /// # Errors
    /// Returns a boundary refusal for invalid request fields, an unavailable transport, or a malformed response.
    pub fn agent_key_change(
        &mut self,
        agent_id: &str,
        recover: bool,
        challenge_delay_seconds: u64,
        ready_at: u64,
        evidence: AgentFinalizationEvidence,
    ) -> Result<ManagedAgentChallenge, AgentBoundaryError> {
        let mut writer = Writer::new(AGENT_JOURNEY);
        writer.u8(if recover { 2 } else { 1 });
        writer.text(agent_id)?;
        writer.u128(0);
        writer.text("")?;
        writer.u64(challenge_delay_seconds);
        writer.u64(ready_at);
        writer.fixed(&[0; 32]);
        writer.fixed(&[0; 32]);
        encode_finalization(&mut writer, evidence)?;
        let mut reader = self.exchange(&writer.finish())?;
        let value = decode_managed_challenge(&mut reader)?;
        reader.finish()?;
        Ok(value)
    }
    /// # Errors
    /// Returns a boundary refusal for invalid request fields, an unavailable transport, or a malformed response.
    pub fn agent_archive(
        &mut self,
        agent_id: &str,
        confirm_name: &str,
        pre_observation: [u8; 32],
        post_observation: [u8; 32],
        session_observation: [u8; 32],
        evidence: AgentFinalizationEvidence,
    ) -> Result<ManagedAgentJourney, AgentBoundaryError> {
        if session_observation == [0; 32] {
            return Err(AgentBoundaryError::Refused);
        }
        let mut writer = Writer::new(AGENT_ARCHIVE);
        writer.text(agent_id)?;
        writer.text(confirm_name)?;
        writer.fixed(&pre_observation);
        writer.fixed(&post_observation);
        writer.fixed(&session_observation);
        encode_finalization(&mut writer, evidence)?;
        let mut reader = self.exchange(&writer.finish())?;
        let value = decode_managed_journey(&mut reader)?;
        reader.finish()?;
        Ok(value)
    }

    /// # Errors
    /// Returns a boundary refusal for invalid request fields, an unavailable transport, or a malformed response.
    pub fn identity_resolve(
        &mut self,
        agent: &str,
    ) -> Result<AgentCoreIdentity, AgentBoundaryError> {
        let mut writer = Writer::new(IDENTITY_RESOLVE);
        writer.text(agent)?;
        let mut reader = self.exchange(&writer.finish())?;
        let head_sequence = reader.u64()?;
        let revocation_sequence = reader.u64()?;
        let verification = reader.u8()?;
        let frozen = match reader.u8()? {
            0 => false,
            1 => true,
            _ => return Err(AgentBoundaryError::CorruptResponse),
        };
        let count = usize::from(reader.u16()?);
        if count == 0 || count > 256 {
            return Err(AgentBoundaryError::CorruptResponse);
        }
        let mut authorities = Vec::with_capacity(count);
        for _ in 0..count {
            let kind = reader.u8()?;
            if !(1..=3).contains(&kind) {
                return Err(AgentBoundaryError::CorruptResponse);
            }
            authorities.push((kind, reader.fixed()?));
        }
        let canonical_bytes = reader.bytes()?;
        if revocation_sequence == 0 {
            return Err(AgentBoundaryError::CorruptResponse);
        }
        reader.finish()?;
        Ok(AgentCoreIdentity {
            head_sequence,
            revocation_sequence,
            verification,
            frozen,
            authorities,
            canonical_bytes,
        })
    }
    /// # Errors
    /// Returns a boundary refusal for invalid request fields, an unavailable transport, or a malformed response.
    pub fn lease_map(
        &mut self,
        not_before_unix_ms: u64,
        not_after_unix_ms: u64,
    ) -> Result<AgentLease, AgentBoundaryError> {
        let mut writer = Writer::new(LEASE_MAP);
        writer.u64(not_before_unix_ms);
        writer.u64(not_after_unix_ms);
        let mut reader = self.exchange(&writer.finish())?;
        let value = AgentLease {
            not_before_sequence: reader.u64()?,
            expiry_sequence: reader.u64()?,
            observed_head_sequence: reader.u64()?,
            canonical_attestation: reader.bytes()?,
        };
        if value.expiry_sequence <= value.not_before_sequence
            || value.expiry_sequence <= value.observed_head_sequence
        {
            return Err(AgentBoundaryError::CorruptResponse);
        }
        reader.finish()?;
        Ok(value)
    }
    /// # Errors
    /// Returns a boundary refusal for invalid request fields, an unavailable transport, or a malformed response.
    pub fn owner_validate(
        &mut self,
        request: &AgentOwnerInstall,
    ) -> Result<AgentOwnerValidation, AgentBoundaryError> {
        self.owner_exchange(OWNER_VALIDATE, None, request)
    }
    /// # Errors
    /// Returns a boundary refusal for invalid request fields, an unavailable transport, or a malformed response.
    pub fn owner_install(
        &mut self,
        request_id: u64,
        key: [u8; 32],
        body_digest: [u8; 32],
        request: &AgentOwnerInstall,
    ) -> Result<AgentOwnerInstalled, AgentBoundaryError> {
        let mut writer = Writer::new(OWNER_INSTALL);
        writer.u64(request_id);
        writer.fixed(&key);
        writer.fixed(&body_digest);
        encode_owner(&mut writer, request)?;
        let mut reader = self.exchange_secret(&writer.finish_secret())?;
        let value = AgentOwnerInstalled {
            token_id: AgentSessionToken::new(reader.fixed()?)?,
            session_id: reader.fixed()?,
            generation: reader.u64()?,
            expiry_sequence: reader.u64()?,
            observed_head_sequence: reader.u64()?,
        };
        if value.session_id == [0; 32]
            || value.generation == 0
            || value.expiry_sequence == 0
            || value.observed_head_sequence == 0
        {
            return Err(AgentBoundaryError::CorruptResponse);
        }
        reader.finish()?;
        Ok(value)
    }
    fn owner_exchange(
        &mut self,
        operation: u8,
        mutation: Option<(u64, [u8; 32], [u8; 32])>,
        request: &AgentOwnerInstall,
    ) -> Result<AgentOwnerValidation, AgentBoundaryError> {
        let mut writer = Writer::new(operation);
        if let Some((id, key, digest)) = mutation {
            writer.u64(id);
            writer.fixed(&key);
            writer.fixed(&digest);
        }
        encode_owner(&mut writer, request)?;
        let mut reader = self.exchange(&writer.finish())?;
        let value = AgentOwnerValidation {
            identity_head_sequence: reader.u64()?,
            expiry_sequence: reader.u64()?,
            observed_head_sequence: reader.u64()?,
            canonical_identity: reader.bytes()?,
        };
        reader.finish()?;
        Ok(value)
    }
    /// # Errors
    /// Returns a boundary refusal for invalid request fields, an unavailable transport, or a malformed response.
    pub fn account_sequence(
        &mut self,
        actor: &layerx_agent_api::identity::AgentDid,
        authority: &layerx_agent_api::identity::AuthorityRef,
    ) -> Result<u64, AgentBoundaryError> {
        let mut writer = Writer::new(ACCOUNT_SEQUENCE);
        writer.text(actor.as_str())?;
        writer.text(authority.as_str())?;
        let mut reader = self.exchange(&writer.finish())?;
        let sequence = reader.u64()?;
        reader.finish()?;
        Ok(sequence)
    }
    /// # Errors
    /// Refuses unauthenticated state or a canonical account not bound to the requested ID.
    pub fn account_state(
        &mut self,
        account_id: [u8; 32],
    ) -> Result<layerx_proof::state::CanonicalAccount, AgentBoundaryError> {
        let mut writer = Writer::new(ACCOUNT_STATE);
        writer.fixed(&account_id);
        let mut reader = self.exchange(&writer.finish())?;
        let observed: [u8; 32] = reader.fixed()?;
        let level = reader.u8()?;
        let bytes = reader.bytes()?;
        let proof = reader.bytes()?;
        let sequence = reader.u64()?;
        reader.finish()?;
        if observed != account_id
            || level < layerx_types::verify::VerificationLevel::STATE_PROVEN.wire_rank()
            || proof.is_empty()
            || sequence == 0
        {
            return Err(AgentBoundaryError::CorruptResponse);
        }
        layerx_proof::state::decode_account_value(account_id, &bytes)
            .map_err(|_| AgentBoundaryError::CorruptResponse)
    }
    /// # Errors
    /// Returns a boundary refusal for invalid request fields, an unavailable transport, or a malformed response.
    pub fn prepare_session_seed(
        &mut self,
        agent: &str,
        action_key: [u8; 32],
        request_digest: [u8; 32],
    ) -> Result<AgentSessionSeed, AgentBoundaryError> {
        let mut writer = Writer::new(SESSION_SEED_PREPARE);
        writer.text(agent)?;
        writer.fixed(&action_key);
        writer.fixed(&request_digest);
        let mut reader = self.exchange(&writer.finish())?;
        let seed = AgentSessionSeed::new(reader.fixed()?)?;
        reader.finish()?;
        Ok(seed)
    }

    /// # Errors
    /// Refuses unbound or unavailable committed session state.
    pub fn session_fee_state(
        &mut self,
        grant_id: [u8; 32],
    ) -> Result<layerx_crypto::session::SessionFeeState, AgentBoundaryError> {
        let mut writer = Writer::new(SESSION_FEE_STATE);
        writer.fixed(&grant_id);
        let mut reader = self.exchange(&writer.finish())?;
        let bytes = reader.bytes()?;
        let sequence = reader.u64()?;
        let root: [u8; 32] = reader.fixed()?;
        reader.finish()?;
        let state = layerx_crypto::session::SessionFeeState::decode(grant_id, &bytes)
            .map_err(|_| AgentBoundaryError::CorruptResponse)?;
        if root == [0; 32] || state.revoked_at_sequence > sequence {
            return Err(AgentBoundaryError::CorruptResponse);
        }
        Ok(state)
    }

    /// # Errors
    /// Refuses unavailable or malformed native fee metadata.
    pub fn native_fee_policy(&mut self) -> Result<NativeFeePolicy, AgentBoundaryError> {
        let mut reader = self.exchange(&Writer::new(NATIVE_FEE_POLICY).finish())?;
        let value = NativeFeePolicy {
            version: reader.u8()?,
            asset_id: reader.fixed()?,
            currency: reader.text()?,
            decimals: reader.u8()?,
        };
        let _sequence = reader.u64()?;
        let root: [u8; 32] = reader.fixed()?;
        reader.finish()?;
        if !matches!(value.version, 1 | 2)
            || value.asset_id == [0; 32]
            || root == [0; 32]
            || value.currency.is_empty()
            || !value
                .currency
                .bytes()
                .all(|byte| byte.is_ascii_alphanumeric())
            || value.decimals > 38
        {
            return Err(AgentBoundaryError::CorruptResponse);
        }
        Ok(value)
    }

    /// # Errors
    /// Refuses unavailable or unauthenticated balance evidence.
    pub fn balance(&mut self) -> Result<VerifiedBalance, AgentBoundaryError> {
        let mut reader = self.exchange(&Writer::new(BALANCE).finish())?;
        let value = VerifiedBalance {
            account: reader.fixed()?,
            asset: reader.fixed()?,
            currency: reader.text()?,
            observed_at: reader.text()?,
            age_seconds: reader.u64()?,
            amount: reader.u128()?,
            verification: reader.u8()?,
            global_sequence: reader.u64()?,
            batch_number: reader.u64()?,
            observed_head_sequence: reader.u64()?,
            observed_checkpoint: reader.fixed()?,
            canonical_bytes: reader.bytes()?,
            proof_material: reader.bytes()?,
        };
        if !matches!(value.verification, 4 | 5)
            || value.account == [0; 32]
            || value.asset == [0; 32]
            || value.currency.is_empty()
            || value.observed_at.is_empty()
            || value.canonical_bytes.is_empty()
            || value.proof_material.is_empty()
            || self.subject.as_ref().is_some_and(|subject| {
                value.account != subject.account_id || value.asset != subject.asset_id
            })
        {
            return Err(AgentBoundaryError::CorruptResponse);
        }
        reader.finish()?;
        Ok(value)
    }

    /// # Errors
    /// Returns a boundary refusal for invalid request fields, an unavailable transport, or a malformed response.
    pub fn head(&mut self) -> Result<AgentHead, AgentBoundaryError> {
        let mut reader = self.exchange(&Writer::new(HEAD).finish())?;
        let value = AgentHead {
            chain_sequence: reader.u64()?,
            sealed_batch: reader.u64()?,
            finalised_checkpoint: reader.fixed()?,
        };
        reader.finish()?;
        Ok(value)
    }

    /// # Errors
    /// Returns a boundary refusal for invalid request fields, an unavailable transport, or a malformed response.
    pub fn evidence(
        &mut self,
        idempotency_key: [u8; 32],
        expected_activity_id: [u8; 32],
    ) -> Result<ReceiptLookup, AgentBoundaryError> {
        let mut writer = Writer::new(EVIDENCE);
        writer.fixed(&idempotency_key);
        writer.fixed(&expected_activity_id);
        let mut reader = self.exchange(&writer.finish())?;
        let found = reader.u8()?;
        let value = match found {
            0 => ReceiptLookup::Absent,
            1 => ReceiptLookup::Found(decode_receipt(&mut reader)?),
            _ => return Err(AgentBoundaryError::CorruptResponse),
        };
        reader.finish()?;
        Ok(value)
    }
    /// Repeats the authenticated registry negotiation as a live readiness
    /// probe; a lockable adapter is not itself evidence that agentd is alive.
    /// # Errors
    /// Returns a boundary refusal for invalid request fields, an unavailable transport, or a malformed response.
    pub fn probe(&self) -> Result<(), AgentBoundaryError> {
        let mut runtime = Self::connect(&self.endpoint, self.limits)?;
        let head = runtime.head()?;
        if head.finalised_checkpoint == [0; 32] {
            return Err(AgentBoundaryError::CorruptResponse);
        }
        Ok(())
    }

    /// Connects to the authenticated agent peer and adopts only its
    /// core-negotiated module registry.
    /// # Errors
    /// Returns a boundary refusal for invalid request fields, an unavailable transport, or a malformed response.
    pub fn connect(endpoint: impl AsRef<Path>, limits: Limits) -> Result<Self, AgentBoundaryError> {
        let empty = ModuleRegistry::new(&[]).map_err(|_| AgentBoundaryError::Refused)?;
        let mut runtime = Self::new(endpoint, limits, empty)?;
        let mut reader = runtime.exchange(&Writer::new(REGISTRY).finish())?;
        let module_count = usize::from(reader.u16()?);
        if module_count == 0 || module_count > 32 {
            return Err(AgentBoundaryError::CorruptResponse);
        }
        let mut registrations = Vec::with_capacity(module_count);
        for _ in 0..module_count {
            let module = ModuleId::from_u16(reader.u16()?)
                .map_err(|_| AgentBoundaryError::CorruptResponse)?;
            let activity_count = usize::from(reader.u16()?);
            if activity_count == 0 || activity_count > 256 {
                return Err(AgentBoundaryError::CorruptResponse);
            }
            let mut activities = Vec::with_capacity(activity_count);
            for _ in 0..activity_count {
                let activity = ActivityType::from_u32(reader.u32()?)
                    .map_err(|_| AgentBoundaryError::CorruptResponse)?;
                activities.push(activity);
            }
            registrations.push(
                ModuleRegistration::new(module, &activities)
                    .map_err(|_| AgentBoundaryError::CorruptResponse)?,
            );
        }
        reader.finish()?;
        runtime.registry =
            ModuleRegistry::new(&registrations).map_err(|_| AgentBoundaryError::CorruptResponse)?;
        Ok(runtime)
    }

    #[must_use]
    pub fn registry(&self) -> &ModuleRegistry {
        &self.registry
    }
    /// Creates a runtime for an absolute agentd endpoint and a core-negotiated
    /// registry. The registry is used to derive disclosure from returned bytes.
    /// # Errors
    /// Refuses invalid connection limits or an empty endpoint.
    pub fn new(
        endpoint: impl AsRef<Path>,
        limits: Limits,
        registry: ModuleRegistry,
    ) -> Result<Self, AgentBoundaryError> {
        let endpoint = endpoint.as_ref();
        if !endpoint.is_absolute() || endpoint.as_os_str().is_empty() {
            return Err(AgentBoundaryError::Refused);
        }
        let limits = limits.validate().map_err(|_| AgentBoundaryError::Refused)?;
        Ok(Self {
            endpoint: endpoint.to_path_buf(),
            gate: ConnectionGate::new(limits.maximum_connections),
            limits,
            registry,
            subject: None,
            native_journey: None,
        })
    }

    fn exchange(&self, request: &[u8]) -> Result<Reader, AgentBoundaryError> {
        let request = self.scoped_request(request)?;
        let trace = REQUEST_TRACE.with(|current| current.borrow().clone());
        let request = if let Some(trace) = &trace {
            let mut envelope = Zeroizing::new(Vec::with_capacity(48 + request.len()));
            envelope.extend_from_slice(TRACED_MAGIC);
            envelope.extend_from_slice(trace.as_str().as_bytes());
            envelope.extend_from_slice(
                &u32::try_from(request.len())
                    .map_err(|_| AgentBoundaryError::Refused)?
                    .to_be_bytes(),
            );
            envelope.extend_from_slice(&request);
            if envelope.len() > MAX_BYTES {
                return Err(AgentBoundaryError::Refused);
            }
            envelope
        } else {
            request
        };
        let mut transport = Uds::connect(&self.endpoint, &self.gate, self.limits)
            .map_err(|_| AgentBoundaryError::Unavailable)?;
        transport
            .send(&request)
            .map_err(|_| AgentBoundaryError::Unavailable)?;
        let response = transport
            .receive()
            .map_err(|_| AgentBoundaryError::Unavailable)?;
        let mut reader = Reader::new(response);
        if let Some(trace) = trace {
            if reader.fixed::<8>()? != *TRACED_MAGIC
                || reader.fixed::<36>()?.as_slice() != trace.as_str().as_bytes()
            {
                return Err(AgentBoundaryError::CorruptResponse);
            }
            let response = reader.bytes()?;
            reader.finish()?;
            reader = Reader::new(response);
        }
        if reader.fixed::<8>()? != *MAGIC {
            return Err(AgentBoundaryError::CorruptResponse);
        }
        match reader.u8()? {
            0 => Ok(reader),
            1 => Err(AgentBoundaryError::Refused),
            2 => Err(AgentBoundaryError::Unavailable),
            _ => Err(AgentBoundaryError::CorruptResponse),
        }
    }

    fn exchange_secret(&self, request: &Zeroizing<Vec<u8>>) -> Result<Reader, AgentBoundaryError> {
        self.exchange(request)
    }

    fn encode_mutation_header<T>(operation: u8, mutation: &IdempotentMutation<T>) -> Writer {
        let mut writer = Writer::new(operation);
        writer.u64(mutation.request_id.0);
        writer.fixed(&mutation.key.bytes());
        writer.fixed(&mutation.body_digest.0);
        writer
    }

    fn decode_observation(reader: &mut Reader) -> Result<AgentObservation, AgentBoundaryError> {
        let activity_id = reader.fixed::<32>()?;
        if activity_id == [0; 32] {
            return Err(AgentBoundaryError::CorruptResponse);
        }
        let submission = decode_tracked(reader)?;
        let receipt = decode_optional_receipt(reader)?;
        reader.finish()?;
        let executed = matches!(submission.state, SubmissionState::Executed { .. });
        if executed != receipt.is_some() {
            return Err(AgentBoundaryError::CorruptResponse);
        }
        Ok(AgentObservation {
            submission,
            activity_id,
            receipt,
        })
    }

    /// # Errors
    /// Returns a boundary refusal for invalid request fields, an unavailable transport, or a malformed response.
    pub fn approval_list(
        &mut self,
        current_sequence: u64,
        cursor: Option<[u8; 32]>,
        limit: u8,
    ) -> Result<AgentApprovalPage, AgentBoundaryError> {
        let mut writer = Writer::new(APPROVAL_LIST);
        writer.u64(current_sequence);
        match cursor {
            Some(value) => {
                writer.u8(1);
                writer.fixed(&value);
            }
            None => writer.u8(0),
        }
        writer.u8(limit);
        let mut reader = self.exchange(&writer.finish())?;
        let count = usize::from(reader.u8()?);
        if count > 100 {
            return Err(AgentBoundaryError::CorruptResponse);
        }
        let mut approvals = Vec::with_capacity(count);
        for _ in 0..count {
            approvals.push(decode_approval(&mut reader)?);
        }
        let next_cursor = match reader.u8()? {
            0 => None,
            1 => Some(reader.fixed()?),
            _ => return Err(AgentBoundaryError::CorruptResponse),
        };
        reader.finish()?;
        Ok(AgentApprovalPage {
            approvals,
            next_cursor,
        })
    }
    /// # Errors
    /// Returns a boundary refusal for invalid request fields, an unavailable transport, or a malformed response.
    pub fn approval_get_facts(
        &mut self,
        approval_id: [u8; 32],
        current_sequence: u64,
    ) -> Result<AgentApprovalFacts, AgentBoundaryError> {
        let mut writer = Writer::new(APPROVAL_GET_FACTS);
        writer.fixed(&approval_id);
        writer.u64(current_sequence);
        let mut reader = self.exchange(&writer.finish())?;
        let facts = decode_approval_facts(&mut reader)?;
        reader.finish()?;
        if facts.approval.approval_id != approval_id {
            return Err(AgentBoundaryError::CorruptResponse);
        };
        Ok(facts)
    }
    pub fn approval_list_facts(
        &mut self,
        current_sequence: u64,
        cursor: Option<[u8; 32]>,
        limit: u8,
    ) -> Result<AgentApprovalFactsPage, AgentBoundaryError> {
        let mut writer = Writer::new(APPROVAL_LIST_FACTS);
        writer.u64(current_sequence);
        match cursor {
            Some(value) => {
                writer.u8(1);
                writer.fixed(&value)
            }
            None => writer.u8(0),
        };
        writer.u8(limit);
        let mut reader = self.exchange(&writer.finish())?;
        let count = usize::from(reader.u8()?);
        if count > 100 || count > usize::from(limit) {
            return Err(AgentBoundaryError::CorruptResponse);
        };
        let mut approvals = Vec::with_capacity(count);
        for _ in 0..count {
            approvals.push(decode_approval_facts(&mut reader)?)
        }
        let next_cursor = match reader.u8()? {
            0 => None,
            1 => Some(reader.fixed()?),
            _ => return Err(AgentBoundaryError::CorruptResponse),
        };
        reader.finish()?;
        Ok(AgentApprovalFactsPage {
            approvals,
            next_cursor,
        })
    }
    pub fn native_approval_get_facts(
        &mut self,
        approval_id: [u8; 32],
    ) -> Result<NativeApprovalFacts, AgentBoundaryError> {
        if self.subject.is_none() || approval_id == [0; 32] {
            return Err(AgentBoundaryError::Refused);
        }
        let mut writer = Writer::new(NATIVE_APPROVAL_GET_FACTS);
        writer.fixed(&approval_id);
        let mut reader = self.exchange(&writer.finish())?;
        let value = decode_native_approval_facts(&mut reader)?;
        reader.finish()?;
        if value.approval_id != approval_id {
            return Err(AgentBoundaryError::CorruptResponse);
        }
        let subject = self.subject.as_ref().ok_or(AgentBoundaryError::Refused)?;
        if value.owner.as_bytes() != subject.owner.as_bytes() {
            return Err(AgentBoundaryError::CorruptResponse);
        }
        Ok(value)
    }

    pub fn native_approval_list_facts(
        &mut self,
        cursor: Option<[u8; 32]>,
        limit: u8,
    ) -> Result<NativeApprovalFactsPage, AgentBoundaryError> {
        if self.subject.is_none() || !(1..=100).contains(&limit) || cursor == Some([0; 32]) {
            return Err(AgentBoundaryError::Refused);
        }
        let mut writer = Writer::new(NATIVE_APPROVAL_LIST_FACTS);
        match cursor {
            Some(value) => {
                writer.u8(1);
                writer.fixed(&value);
            }
            None => writer.u8(0),
        }
        writer.u8(limit);
        let mut reader = self.exchange(&writer.finish())?;
        let count = usize::from(reader.u8()?);
        if count > usize::from(limit) {
            return Err(AgentBoundaryError::CorruptResponse);
        }
        let subject = self.subject.as_ref().ok_or(AgentBoundaryError::Refused)?;
        let mut previous = cursor;
        let mut approvals = Vec::with_capacity(count);
        for _ in 0..count {
            let value = decode_native_approval_facts(&mut reader)?;
            if previous.is_some_and(|id| value.approval_id <= id)
                || value.owner.as_bytes() != subject.owner.as_bytes()
            {
                return Err(AgentBoundaryError::CorruptResponse);
            }
            previous = Some(value.approval_id);
            approvals.push(value);
        }
        let next_cursor = match reader.u8()? {
            0 => None,
            1 => Some(reader.fixed()?),
            _ => return Err(AgentBoundaryError::CorruptResponse),
        };
        reader.finish()?;
        if next_cursor.is_some()
            && (approvals.len() != usize::from(limit) || next_cursor != previous)
        {
            return Err(AgentBoundaryError::CorruptResponse);
        }
        Ok(NativeApprovalFactsPage {
            approvals,
            next_cursor,
        })
    }

    pub fn native_program_approval_list(
        &mut self,
        cursor: Option<[u8; 32]>,
        limit: u8,
    ) -> Result<NativeProgramApprovalFactsPage, AgentBoundaryError> {
        if self.subject.is_none() || !(1..=100).contains(&limit) || cursor == Some([0; 32]) {
            return Err(AgentBoundaryError::Refused);
        }
        let mut writer = Writer::new(NATIVE_PROGRAM_APPROVAL_LIST);
        writer.u16(5);
        match cursor {
            Some(id) => {
                writer.u8(1);
                writer.fixed(&id);
            }
            None => writer.u8(0),
        }
        writer.u8(limit);
        let mut reader = self.exchange(&writer.finish())?;
        if reader.u16()? != 5 {
            return Err(AgentBoundaryError::CorruptResponse);
        }
        let count = usize::from(reader.u8()?);
        if count > usize::from(limit) {
            return Err(AgentBoundaryError::CorruptResponse);
        }
        let mut approvals = Vec::with_capacity(count);
        let mut previous = cursor;
        for _ in 0..count {
            let facts = decode_native_program_approval_facts(&mut reader)?;
            self.require_program_owner(&facts.owner)?;
            if previous.is_some_and(|id| facts.approval_id <= id) {
                return Err(AgentBoundaryError::CorruptResponse);
            }
            previous = Some(facts.approval_id);
            approvals.push(facts);
        }
        let next_cursor = decode_native_ref(&mut reader)?;
        reader.finish()?;
        if next_cursor.is_some() && (count != usize::from(limit) || next_cursor != previous) {
            return Err(AgentBoundaryError::CorruptResponse);
        }
        Ok(NativeProgramApprovalFactsPage {
            approvals,
            next_cursor,
        })
    }

    pub fn native_program_approval_get(
        &mut self,
        approval_id: [u8; 32],
    ) -> Result<NativeProgramApprovalFacts, AgentBoundaryError> {
        let writer = self.program_request(NATIVE_PROGRAM_APPROVAL_GET, approval_id)?;
        let mut reader = self.exchange(&writer.finish())?;
        let facts = decode_native_program_approval_facts(&mut reader)?;
        reader.finish()?;
        self.require_program_owner(&facts.owner)?;
        if facts.approval_id != approval_id {
            return Err(AgentBoundaryError::CorruptResponse);
        }
        Ok(facts)
    }

    pub fn native_program_approval_material(
        &mut self,
        approval_id: [u8; 32],
        held_digest: [u8; 32],
    ) -> Result<NativeProgramApprovalMaterial, AgentBoundaryError> {
        if held_digest == [0; 32] {
            return Err(AgentBoundaryError::Refused);
        }
        let mut writer = self.program_request(NATIVE_PROGRAM_APPROVAL_MATERIAL, approval_id)?;
        writer.fixed(&held_digest);
        let mut reader = self.exchange(&writer.finish())?;
        if reader.u16()? != 5
            || reader.fixed::<32>()? != approval_id
            || reader.fixed::<32>()? != held_digest
        {
            return Err(AgentBoundaryError::CorruptResponse);
        }
        let owner = reader.text()?;
        self.require_program_owner(&owner)?;
        let provenance = match reader.u8()? {
            0 => NativeEffectMaterialProvenance::LocalOwned,
            _ => return Err(AgentBoundaryError::CorruptResponse),
        };
        let canonical_unsigned_bytes = reader.bytes()?;
        let immutable_carrier_bytes = reader.bytes()?;
        let canonical_budget_bytes = reader.bytes()?;
        reader.finish()?;
        if <[u8; 32]>::from(sha2::Sha256::digest(&canonical_unsigned_bytes)) != approval_id
            || <[u8; 32]>::from(sha2::Sha256::digest(&immutable_carrier_bytes)) != held_digest
        {
            return Err(AgentBoundaryError::CorruptResponse);
        }
        let activity =
            layerx_wire::activity::decode_unsigned(&canonical_unsigned_bytes, &self.registry)
                .map_err(|_| AgentBoundaryError::CorruptResponse)?;
        if activity.protocol_version() != 3
            || activity.activity_type().module() != ModuleId::Programs
            || layerx_wire::activity::encode_unsigned(&activity)
                .map_err(|_| AgentBoundaryError::CorruptResponse)?
                != canonical_unsigned_bytes
        {
            return Err(AgentBoundaryError::CorruptResponse);
        }
        validate_native_program_payload(activity.activity_type().ordinal(), activity.payload())?;
        let carrier: serde_json::Value = serde_json::from_slice(&immutable_carrier_bytes)
            .map_err(|_| AgentBoundaryError::CorruptResponse)?;
        let subject = self.subject.as_ref().ok_or(AgentBoundaryError::Refused)?;
        if carrier.get("principal").and_then(serde_json::Value::as_str)
            != Some(subject.principal.as_str())
            || carrier
                .get("activity_module")
                .and_then(serde_json::Value::as_u64)
                != Some(9)
            || carrier
                .get("activity_ordinal")
                .and_then(serde_json::Value::as_u64)
                != Some(u64::from(activity.activity_type().ordinal()))
            || !program_carrier_bytes(&carrier, "preparation", &approval_id)
            || !program_carrier_bytes(&carrier, "canonical_bytes", &canonical_unsigned_bytes)
            || !program_carrier_bytes(&carrier, "program_budget", &canonical_budget_bytes)
            || !program_carrier_bytes(&carrier, "actor", activity.actor_did())
        {
            return Err(AgentBoundaryError::CorruptResponse);
        }
        let (allocation_body, allocation_digest) = canonical_budget_bytes
            .split_at_checked(
                canonical_budget_bytes
                    .len()
                    .checked_sub(32)
                    .ok_or(AgentBoundaryError::CorruptResponse)?,
            )
            .ok_or(AgentBoundaryError::CorruptResponse)?;
        let expected: [u8; 32] = sha2::Sha256::new()
            .chain_update(b"layerx:program-budget:v2\0")
            .chain_update(allocation_body)
            .finalize()
            .into();
        if !allocation_body.starts_with(b"LXPB\x02") || allocation_digest != expected {
            return Err(AgentBoundaryError::CorruptResponse);
        }
        let actor = String::from_utf8(activity.actor_did().to_vec())
            .map_err(|_| AgentBoundaryError::CorruptResponse)?;
        let activity_ordinal = activity.activity_type().ordinal();
        let canonical_payload_bytes = activity.payload().to_vec();
        let fee_limit = activity.fee_limit();
        let activity_expires_at_unix_milliseconds = activity.timestamp_bound().not_after;
        Ok(NativeProgramApprovalMaterial {
            approval_id,
            held_digest,
            owner,
            provenance,
            canonical_unsigned_bytes,
            immutable_carrier_bytes,
            canonical_budget_bytes,
            actor,
            activity_ordinal,
            canonical_payload_bytes,
            fee_limit,
            activity_expires_at_unix_milliseconds,
        })
    }

    pub fn native_program_approval_budget(
        &mut self,
        approval_id: [u8; 32],
        held_digest: [u8; 32],
        current_sequence: u64,
    ) -> Result<NativeProgramApprovalBudget, AgentBoundaryError> {
        if held_digest == [0; 32] || current_sequence == 0 {
            return Err(AgentBoundaryError::Refused);
        }
        let mut writer = self.program_request(NATIVE_PROGRAM_APPROVAL_BUDGET, approval_id)?;
        writer.fixed(&held_digest);
        writer.u64(current_sequence);
        let mut reader = self.exchange(&writer.finish())?;
        if reader.u16()? != 5
            || reader.fixed::<32>()? != approval_id
            || reader.fixed::<32>()? != held_digest
        {
            return Err(AgentBoundaryError::CorruptResponse);
        }
        let owner = reader.text()?;
        self.require_program_owner(&owner)?;
        let budget_id = reader.fixed()?;
        let asset = reader.fixed()?;
        let source_account = reader.fixed()?;
        let observed_at_sequence = reader.u64()?;
        let remaining = reader.u128()?;
        let terminal = match reader.u8()? {
            0 => false,
            1 => true,
            _ => return Err(AgentBoundaryError::CorruptResponse),
        };
        let verification = match reader.u8()? {
            4 => Level::CheckpointFinalised,
            5 => Level::SettlementAnchored,
            _ => return Err(AgentBoundaryError::CorruptResponse),
        };
        let evidence_digest = reader.fixed()?;
        let receipt_digest = reader.fixed()?;
        let checkpoint_digest = reader.fixed()?;
        let age_sequences = reader.u64()?;
        let maximum_age_sequences = reader.u64()?;
        let proof_digest = reader.fixed()?;
        let verified_proof_bytes = reader.bytes()?;
        reader.finish()?;
        if budget_id == [0; 32]
            || asset == [0; 32]
            || source_account == [0; 32]
            || observed_at_sequence != current_sequence
            || evidence_digest == [0; 32]
            || receipt_digest == [0; 32]
            || checkpoint_digest == [0; 32]
            || maximum_age_sequences == 0
            || age_sequences > maximum_age_sequences
            || proof_digest == [0; 32]
            || <[u8; 32]>::from(sha2::Sha256::digest(&verified_proof_bytes)) != proof_digest
        {
            return Err(AgentBoundaryError::CorruptResponse);
        }
        Ok(NativeProgramApprovalBudget {
            approval_id,
            held_digest,
            owner,
            budget_id,
            asset,
            source_account,
            observed_at_sequence,
            remaining,
            terminal,
            verification,
            evidence_digest,
            receipt_digest,
            checkpoint_digest,
            age_sequences,
            maximum_age_sequences,
            proof_digest,
            verified_proof_bytes,
        })
    }

    pub fn native_program_approval_decide(
        &mut self,
        approval_id: [u8; 32],
        held_digest: [u8; 32],
        idempotency_key: &str,
        grant: bool,
        current_sequence: u64,
    ) -> Result<NativeProgramApprovalFacts, AgentBoundaryError> {
        if held_digest == [0; 32]
            || current_sequence == 0
            || idempotency_key.len() != 64
            || !idempotency_key
                .bytes()
                .all(|byte| byte.is_ascii_digit() || (b'a'..=b'f').contains(&byte))
            || idempotency_key.bytes().all(|byte| byte == b'0')
        {
            return Err(AgentBoundaryError::Refused);
        }
        let mut writer = self.program_request(NATIVE_PROGRAM_APPROVAL_DECIDE, approval_id)?;
        writer.fixed(&held_digest);
        writer.text(idempotency_key)?;
        writer.u8(u8::from(grant));
        writer.u64(current_sequence);
        let mut reader = self.exchange(&writer.finish())?;
        let facts = decode_native_program_approval_facts(&mut reader)?;
        reader.finish()?;
        self.require_program_owner(&facts.owner)?;
        if facts.approval_id != approval_id
            || facts.held_digest != held_digest
            || !matches!(
                facts.state,
                NativeApprovalFactState::Granted
                    | NativeApprovalFactState::Rejected
                    | NativeApprovalFactState::Expired
            )
            || (facts.state != NativeApprovalFactState::Expired
                && (facts.state == NativeApprovalFactState::Granted) != grant)
        {
            return Err(AgentBoundaryError::CorruptResponse);
        }
        Ok(facts)
    }

    fn program_request(
        &self,
        operation: u8,
        approval_id: [u8; 32],
    ) -> Result<Writer, AgentBoundaryError> {
        if self.subject.is_none() || approval_id == [0; 32] {
            return Err(AgentBoundaryError::Refused);
        }
        let mut writer = Writer::new(operation);
        writer.u16(5);
        writer.fixed(&approval_id);
        Ok(writer)
    }

    fn require_program_owner(&self, owner: &str) -> Result<(), AgentBoundaryError> {
        let subject = self.subject.as_ref().ok_or(AgentBoundaryError::Refused)?;
        if owner.as_bytes() != subject.owner.as_bytes() {
            return Err(AgentBoundaryError::CorruptResponse);
        }
        Ok(())
    }

    pub fn native_effect_approval_get_facts(
        &mut self,
        approval_id: [u8; 32],
    ) -> Result<NativeEffectApprovalFacts, AgentBoundaryError> {
        if self.subject.is_none() || approval_id == [0; 32] {
            return Err(AgentBoundaryError::Refused);
        }
        let mut writer = Writer::new(NATIVE_EFFECT_APPROVAL_GET);
        writer.fixed(&approval_id);
        let mut reader = self.exchange(&writer.finish())?;
        let value = decode_native_effect_approval_facts(&mut reader)?;
        reader.finish()?;
        if value.approval_id != approval_id
            || self
                .subject
                .as_ref()
                .is_none_or(|s| s.owner.as_bytes() != value.owner.as_bytes())
        {
            return Err(AgentBoundaryError::CorruptResponse);
        };
        Ok(value)
    }
    pub fn native_effect_approval_list_facts(
        &mut self,
        cursor: Option<[u8; 32]>,
        limit: u8,
    ) -> Result<NativeEffectApprovalFactsPage, AgentBoundaryError> {
        if self.subject.is_none() || !(1..=100).contains(&limit) || cursor == Some([0; 32]) {
            return Err(AgentBoundaryError::Refused);
        }
        let mut writer = Writer::new(NATIVE_EFFECT_APPROVAL_LIST);
        match cursor {
            Some(id) => {
                writer.u8(1);
                writer.fixed(&id)
            }
            None => writer.u8(0),
        };
        writer.u8(limit);
        let mut reader = self.exchange(&writer.finish())?;
        let count = usize::from(reader.u8()?);
        if count > usize::from(limit) {
            return Err(AgentBoundaryError::CorruptResponse);
        }
        let mut approvals = Vec::with_capacity(count);
        let mut previous = cursor;
        for _ in 0..count {
            let value = decode_native_effect_approval_facts(&mut reader)?;
            if previous.is_some_and(|id| value.approval_id <= id)
                || self
                    .subject
                    .as_ref()
                    .is_none_or(|s| s.owner.as_bytes() != value.owner.as_bytes())
            {
                return Err(AgentBoundaryError::CorruptResponse);
            }
            previous = Some(value.approval_id);
            approvals.push(value);
        }
        let next_cursor = decode_native_ref(&mut reader)?;
        reader.finish()?;
        if next_cursor.is_some()
            && (approvals.len() != usize::from(limit) || next_cursor != previous)
        {
            return Err(AgentBoundaryError::CorruptResponse);
        }
        Ok(NativeEffectApprovalFactsPage {
            approvals,
            next_cursor,
        })
    }
    pub fn native_effect_approval_decide(
        &mut self,
        approval_id: [u8; 32],
        held_digest: [u8; 32],
        idempotency_key: &str,
        grant: bool,
        current_sequence: u64,
    ) -> Result<NativeEffectApprovalFacts, AgentBoundaryError> {
        if self.subject.is_none()
            || approval_id == [0; 32]
            || held_digest == [0; 32]
            || current_sequence == 0
        {
            return Err(AgentBoundaryError::Refused);
        }
        let mut writer = Writer::new(NATIVE_EFFECT_APPROVAL_DECIDE);
        writer.fixed(&approval_id);
        writer.fixed(&held_digest);
        writer.text(idempotency_key)?;
        writer.u8(u8::from(grant));
        writer.u64(current_sequence);
        let mut reader = self.exchange(&writer.finish())?;
        let value = decode_native_effect_approval_facts(&mut reader)?;
        reader.finish()?;
        if value.approval_id != approval_id
            || value.held_digest != held_digest
            || self
                .subject
                .as_ref()
                .is_none_or(|s| s.owner.as_bytes() != value.owner.as_bytes())
            || !value.requires_approval
            || !matches!(
                value.state,
                NativeEffectApprovalFactState::Granted
                    | NativeEffectApprovalFactState::Rejected
                    | NativeEffectApprovalFactState::Expired
            )
            || (value.state != NativeEffectApprovalFactState::Expired
                && (value.state == NativeEffectApprovalFactState::Granted) != grant)
        {
            return Err(AgentBoundaryError::CorruptResponse);
        };
        Ok(value)
    }

    pub fn native_effect_approval_material(
        &mut self,
        approval_id: [u8; 32],
        held_digest: [u8; 32],
    ) -> Result<NativeEffectApprovalMaterial, AgentBoundaryError> {
        if self.subject.is_none() || approval_id == [0; 32] || held_digest == [0; 32] {
            return Err(AgentBoundaryError::Refused);
        }
        let mut writer = Writer::new(NATIVE_EFFECT_APPROVAL_MATERIAL);
        writer.fixed(&approval_id);
        writer.fixed(&held_digest);
        let mut reader = self.exchange(&writer.finish())?;
        if reader.u16()? != 4
            || reader.fixed::<32>()? != approval_id
            || reader.fixed::<32>()? != held_digest
        {
            return Err(AgentBoundaryError::CorruptResponse);
        }
        let owner = reader.text()?;
        if self
            .subject
            .as_ref()
            .is_none_or(|s| s.owner.as_bytes() != owner.as_bytes())
        {
            return Err(AgentBoundaryError::CorruptResponse);
        }
        let provenance = match reader.u8()? {
            0 => NativeEffectMaterialProvenance::LocalOwned,
            _ => return Err(AgentBoundaryError::CorruptResponse),
        };
        let canonical_unsigned_bytes = reader.bytes()?;
        let immutable_carrier_bytes = reader.bytes()?;
        let canonical_budget_bytes = reader.bytes()?;
        reader.finish()?;
        let actual_id: [u8; 32] = sha2::Sha256::digest(&canonical_unsigned_bytes).into();
        let actual_digest: [u8; 32] = sha2::Sha256::digest(&immutable_carrier_bytes).into();
        if actual_id != approval_id || actual_digest != held_digest {
            return Err(AgentBoundaryError::CorruptResponse);
        }
        let carrier: serde_json::Value = serde_json::from_slice(&immutable_carrier_bytes)
            .map_err(|_| AgentBoundaryError::CorruptResponse)?;
        let bound_budget = carrier
            .get("budget")
            .and_then(serde_json::Value::as_array)
            .ok_or(AgentBoundaryError::CorruptResponse)?;
        if bound_budget.len() != canonical_budget_bytes.len()
            || bound_budget
                .iter()
                .zip(&canonical_budget_bytes)
                .any(|(value, byte)| value.as_u64() != Some(u64::from(*byte)))
        {
            return Err(AgentBoundaryError::CorruptResponse);
        }
        let bound_canonical = carrier
            .get("canonical_bytes")
            .and_then(serde_json::Value::as_array)
            .ok_or(AgentBoundaryError::CorruptResponse)?;
        if bound_canonical.len() != canonical_unsigned_bytes.len()
            || bound_canonical
                .iter()
                .zip(&canonical_unsigned_bytes)
                .any(|(value, byte)| value.as_u64() != Some(u64::from(*byte)))
        {
            return Err(AgentBoundaryError::CorruptResponse);
        }
        Ok(NativeEffectApprovalMaterial {
            approval_id,
            held_digest,
            owner,
            provenance,
            canonical_unsigned_bytes,
            immutable_carrier_bytes,
            canonical_budget_bytes,
        })
    }
    pub fn native_effect_approval_budget(
        &mut self,
        approval_id: [u8; 32],
        held_digest: [u8; 32],
        current_sequence: u64,
    ) -> Result<NativeEffectApprovalBudget, AgentBoundaryError> {
        if self.subject.is_none()
            || approval_id == [0; 32]
            || held_digest == [0; 32]
            || current_sequence == 0
        {
            return Err(AgentBoundaryError::Refused);
        }
        let mut writer = Writer::new(NATIVE_EFFECT_APPROVAL_BUDGET);
        writer.fixed(&approval_id);
        writer.fixed(&held_digest);
        writer.u64(current_sequence);
        let mut reader = self.exchange(&writer.finish())?;
        if reader.u16()? != 4
            || reader.fixed::<32>()? != approval_id
            || reader.fixed::<32>()? != held_digest
        {
            return Err(AgentBoundaryError::CorruptResponse);
        }
        let owner = reader.text()?;
        let observed_at_sequence = reader.u64()?;
        let fee_asset = reader.fixed()?;
        if self
            .subject
            .as_ref()
            .is_none_or(|s| s.owner.as_bytes() != owner.as_bytes())
            || observed_at_sequence != current_sequence
            || fee_asset == [0; 32]
        {
            return Err(AgentBoundaryError::CorruptResponse);
        }
        let count = usize::from(reader.u16()?);
        if count == 0 || count > 256 {
            return Err(AgentBoundaryError::CorruptResponse);
        }
        let mut rows = Vec::with_capacity(count);
        let mut previous = None;
        for _ in 0..count {
            let asset = reader.fixed()?;
            let budget_id = reader.fixed()?;
            let source_account = reader.fixed()?;
            let remaining = reader.u128()?;
            let verification = match reader.u8()? {
                4 => Level::CheckpointFinalised,
                5 => Level::SettlementAnchored,
                _ => return Err(AgentBoundaryError::CorruptResponse),
            };
            let evidence_digest = reader.fixed()?;
            let receipt_digest = reader.fixed()?;
            let checkpoint_digest = reader.fixed()?;
            let age_sequences = reader.u64()?;
            let maximum_age_sequences = reader.u64()?;
            let key = (asset, budget_id, source_account);
            if asset == [0; 32]
                || budget_id == [0; 32]
                || source_account == [0; 32]
                || evidence_digest == [0; 32]
                || receipt_digest == [0; 32]
                || checkpoint_digest == [0; 32]
                || maximum_age_sequences == 0
                || age_sequences > maximum_age_sequences
                || previous.is_some_and(|old| old >= key)
            {
                return Err(AgentBoundaryError::CorruptResponse);
            }
            previous = Some(key);
            rows.push(NativeEffectBudgetRow {
                asset,
                budget_id,
                source_account,
                remaining,
                verification,
                evidence_digest,
                receipt_digest,
                checkpoint_digest,
                age_sequences,
                maximum_age_sequences,
            });
        }
        reader.finish()?;
        Ok(NativeEffectApprovalBudget {
            approval_id,
            held_digest,
            owner,
            observed_at_sequence,
            fee_asset,
            rows,
        })
    }

    pub fn managed_evidence(
        &mut self,
        agent_id: &str,
        digest: [u8; 32],
    ) -> Result<ManagedReceiptExport, AgentBoundaryError> {
        if digest == [0; 32] {
            return Err(AgentBoundaryError::Refused);
        };
        let mut writer = Writer::new(MANAGED_EVIDENCE);
        writer.text(agent_id)?;
        writer.fixed(&digest);
        let mut reader = self.exchange(&writer.finish())?;
        if reader.u16()? != 2 {
            return Err(AgentBoundaryError::CorruptResponse);
        };
        let value = ManagedReceiptExport {
            digest: reader.fixed()?,
            activity_id: reader.fixed()?,
            global_sequence: reader.u64()?,
            verification: reader.u8()?,
            canonical_bytes: reader.bytes()?,
        };
        reader.finish()?;
        let actual_digest: [u8; 32] = sha2::Sha256::digest(&value.canonical_bytes).into();
        let receipt = layerx_wire::receipt::decode(&value.canonical_bytes)
            .map_err(|_| AgentBoundaryError::CorruptResponse)?;
        let protocol = receipt
            .protocol()
            .ok_or(AgentBoundaryError::CorruptResponse)?;
        if value.digest != digest
            || actual_digest != digest
            || value.activity_id != protocol.activity_id()
            || value.global_sequence != protocol.global_sequence()
            || !(1..=5).contains(&value.verification)
            || layerx_wire::receipt::encode(&receipt)
                .map_err(|_| AgentBoundaryError::CorruptResponse)?
                != value.canonical_bytes
        {
            return Err(AgentBoundaryError::CorruptResponse);
        };
        Ok(value)
    }
    pub fn approval_get(
        &mut self,
        approval_id: [u8; 32],
        current_sequence: u64,
    ) -> Result<crate::approvals::AgentApprovalRecord, AgentBoundaryError> {
        let mut writer = Writer::new(APPROVAL_GET);
        writer.fixed(&approval_id);
        writer.u64(current_sequence);
        let mut reader = self.exchange(&writer.finish())?;
        let value = decode_approval(&mut reader)?;
        reader.finish()?;
        Ok(value)
    }
    /// # Errors
    /// Returns a boundary refusal for invalid request fields, an unavailable transport, or a malformed response.
    pub fn approval_decide(
        &mut self,
        approve: bool,
        approval_id: [u8; 32],
        held_digest: [u8; 32],
        idempotency_key: &str,
        current_sequence: u64,
    ) -> Result<crate::approvals::AgentDecision, AgentBoundaryError> {
        let mut writer = Writer::new(if approve {
            APPROVAL_APPROVE
        } else {
            APPROVAL_REJECT
        });
        writer.fixed(&approval_id);
        writer.fixed(&held_digest);
        writer.text(idempotency_key)?;
        writer.u64(current_sequence);
        let mut reader = self.exchange(&writer.finish())?;
        let outcome = reader.u8()?;
        let submission_ref = match reader.u8()? {
            0 => None,
            1 => Some(reader.fixed()?),
            _ => return Err(AgentBoundaryError::CorruptResponse),
        };
        let winning = match reader.u8()? {
            0 => None,
            1 => Some(reader.u8()?),
            _ => return Err(AgentBoundaryError::CorruptResponse),
        };
        reader.finish()?;
        decision(outcome, submission_ref, winning)
    }
}

fn encode_owner(
    writer: &mut Writer,
    request: &AgentOwnerInstall,
) -> Result<(), AgentBoundaryError> {
    if request.permitted_activity_types.is_empty()
        || request.permitted_activity_types.len() > 256
        || request.scopes.is_empty()
        || request.scopes.len() > 32
    {
        return Err(AgentBoundaryError::Refused);
    }
    writer.text(&request.agent)?;
    writer.u8(request.authority_kind);
    writer.fixed(&request.authority_id);
    writer.fixed(&request.session_id);
    writer.fixed(&request.token_id);
    writer.fixed(&request.session_public_key);
    writer.bytes(&request.registration_payload)?;
    writer.fixed(&request.grantor);
    writer.u64(request.grant_not_before);
    writer.u64(request.grant_expires_at);
    writer.u64(request.grant_revocation_sequence);
    match &request.session_seed {
        Some(seed) => writer.fixed(seed.expose()),
        None => writer.fixed(&[0; 32]),
    }
    writer.u16(
        u16::try_from(request.permitted_activity_types.len())
            .map_err(|_| AgentBoundaryError::Refused)?,
    );
    for value in &request.permitted_activity_types {
        writer.u16(*value);
    }
    writer.u8(u8::try_from(request.scopes.len()).map_err(|_| AgentBoundaryError::Refused)?);
    for scope in &request.scopes {
        writer.text(scope)?;
    }
    writer.u64(request.lease_not_before_unix_ms);
    writer.u64(request.lease_not_after_unix_ms);
    writer.text(&request.opening_client)?;
    writer.text(&request.policy_version)?;
    match &request.lifecycle {
        None => writer.u8(0),
        Some(value) => {
            encode_lifecycle(writer, value)?;
        }
    }
    Ok(())
}

fn encode_finalization(
    writer: &mut Writer,
    value: AgentFinalizationEvidence,
) -> Result<(), AgentBoundaryError> {
    if value.action_key == [0; 32]
        || value.activity_id == [0; 32]
        || value.receipt_digest == [0; 32]
        || value.observed_sequence == 0
        || !(4..=5).contains(&value.verification)
        || value.finalized_at == 0
    {
        return Err(AgentBoundaryError::Refused);
    }
    writer.fixed(&value.action_key);
    writer.fixed(&value.activity_id);
    writer.fixed(&value.receipt_digest);
    writer.u64(value.observed_sequence);
    writer.u8(value.verification);
    writer.u64(value.finalized_at);
    Ok(())
}

fn encode_lifecycle(
    writer: &mut Writer,
    value: &AgentLifecycleSeed,
) -> Result<(), AgentBoundaryError> {
    if value.monthly_limit == 0
        || value.period_end <= value.period_start
        || value.verified_evidence.is_empty()
        || value.verified_evidence.len() > MAX_EVIDENCE
        || value.activity_types.is_empty()
        || value.counterparties.is_empty()
        || value.assets.is_empty()
        || value.purposes.is_empty()
        || value.session_scopes.is_empty()
        || value.creation_receipt_roots.is_empty()
        || value.custody_public_key == [0; 32]
        || value.budget_asset == [0; 32]
        || value.purpose_hash == [0; 32]
        || value.recovery_root == [0; 32]
        || value.recovery_threshold == 0
        || value.capability_id == [0; 32]
        || value.protocol_grant_id == [0; 32]
        || value.network_id == 0
    {
        return Err(AgentBoundaryError::Refused);
    }
    writer.u8(1);
    for text in [
        &value.agent_id,
        &value.name,
        &value.purpose,
        &value.currency,
    ] {
        writer.text(text)?;
    }
    writer.u128(value.monthly_limit);
    writer.u64(value.period_start);
    writer.u64(value.period_end);
    writer.u64(value.created_at);
    writer.u64(value.updated_at);
    writer.u16(
        u16::try_from(value.verified_evidence.len()).map_err(|_| AgentBoundaryError::Refused)?,
    );
    for item in &value.verified_evidence {
        writer.fixed(item);
    }
    for text in [&value.actor, &value.primary_authority, &value.custody_key] {
        writer.text(text)?;
    }
    writer.fixed(&value.custody_public_key);
    writer.text(&value.owner_account)?;
    writer.text(&value.budget_account)?;
    writer.fixed(&value.budget_asset);
    writer.fixed(&value.purpose_hash);
    writer.fixed(&value.recovery_root);
    writer.u16(value.recovery_threshold);
    writer.fixed(&value.capability_id);
    writer.u16(u16::try_from(value.activity_types.len()).map_err(|_| AgentBoundaryError::Refused)?);
    for item in &value.activity_types {
        writer.u32(*item);
    }
    for values in [&value.counterparties, &value.assets] {
        writer.u16(u16::try_from(values.len()).map_err(|_| AgentBoundaryError::Refused)?);
        for item in values {
            writer.fixed(item);
        }
    }
    writer.u128(value.amount_ceiling);
    writer.u64(value.rate_maximum_uses);
    writer.u64(value.rate_window_sequences);
    writer.u16(u16::try_from(value.purposes.len()).map_err(|_| AgentBoundaryError::Refused)?);
    for item in &value.purposes {
        writer.text(item)?;
    }
    writer.u64(value.capability_expiry_sequence);
    writer.u16(u16::try_from(value.session_scopes.len()).map_err(|_| AgentBoundaryError::Refused)?);
    for item in &value.session_scopes {
        writer.text(item)?;
    }
    writer.u64(value.session_expiry_unix_seconds);
    writer.fixed(&value.protocol_grant_id);
    writer.u64(value.budget_period_seconds);
    writer.u64(value.budget_expiry_seconds);
    writer.u128(value.initial_funding);
    writer.u32(value.network_id);
    writer.u16(
        u16::try_from(value.creation_receipt_roots.len())
            .map_err(|_| AgentBoundaryError::Refused)?,
    );
    for item in &value.creation_receipt_roots {
        writer.fixed(item);
    }
    Ok(())
}

fn decode_lifecycle(reader: &mut Reader) -> Result<AgentLifecycleSeed, AgentBoundaryError> {
    let agent_id = reader.text()?;
    let name = reader.text()?;
    let purpose = reader.text()?;
    let currency = reader.text()?;
    let monthly_limit = reader.u128()?;
    let period_start = reader.u64()?;
    let period_end = reader.u64()?;
    let created_at = reader.u64()?;
    let updated_at = reader.u64()?;
    let verified_evidence = read_fixed_list(reader, 64)?;
    let actor = reader.text()?;
    let primary_authority = reader.text()?;
    let custody_key = reader.text()?;
    let custody_public_key = reader.fixed()?;
    let owner_account = reader.text()?;
    let budget_account = reader.text()?;
    let budget_asset = reader.fixed()?;
    let purpose_hash = reader.fixed()?;
    let recovery_root = reader.fixed()?;
    let recovery_threshold = reader.u16()?;
    let capability_id = reader.fixed()?;
    let activity_types = read_u32_list(reader, 256)?;
    let counterparties = read_fixed_list(reader, 256)?;
    let assets = read_fixed_list(reader, 256)?;
    let amount_ceiling = reader.u128()?;
    let rate_maximum_uses = reader.u64()?;
    let rate_window_sequences = reader.u64()?;
    let purposes = read_text_list(reader, 64)?;
    let capability_expiry_sequence = reader.u64()?;
    let session_scopes = read_text_list(reader, 64)?;
    let session_expiry_unix_seconds = reader.u64()?;
    let protocol_grant_id = reader.fixed()?;
    let budget_period_seconds = reader.u64()?;
    let budget_expiry_seconds = reader.u64()?;
    let initial_funding = reader.u128()?;
    let network_id = reader.u32()?;
    let creation_receipt_roots = read_fixed_list(reader, 64)?;
    let value = AgentLifecycleSeed {
        agent_id,
        name,
        purpose,
        currency,
        monthly_limit,
        period_start,
        period_end,
        created_at,
        updated_at,
        verified_evidence,
        actor,
        primary_authority,
        custody_key,
        custody_public_key,
        owner_account,
        budget_account,
        budget_asset,
        purpose_hash,
        recovery_root,
        recovery_threshold,
        capability_id,
        activity_types,
        counterparties,
        assets,
        amount_ceiling,
        rate_maximum_uses,
        rate_window_sequences,
        purposes,
        capability_expiry_sequence,
        session_scopes,
        session_expiry_unix_seconds,
        protocol_grant_id,
        budget_period_seconds,
        budget_expiry_seconds,
        initial_funding,
        network_id,
        creation_receipt_roots,
    };
    if value.monthly_limit == 0
        || value.period_end <= value.period_start
        || value.custody_public_key == [0; 32]
        || value.budget_asset == [0; 32]
        || value.purpose_hash == [0; 32]
        || value.recovery_root == [0; 32]
        || value.recovery_threshold == 0
        || value.capability_id == [0; 32]
        || value.protocol_grant_id == [0; 32]
        || value.network_id == 0
    {
        return Err(AgentBoundaryError::CorruptResponse);
    }
    Ok(value)
}
fn read_fixed_list(reader: &mut Reader, max: usize) -> Result<Vec<[u8; 32]>, AgentBoundaryError> {
    let n = usize::from(reader.u16()?);
    if n == 0 || n > max {
        return Err(AgentBoundaryError::CorruptResponse);
    }
    let mut v = Vec::with_capacity(n);
    for _ in 0..n {
        v.push(reader.fixed()?);
    }
    Ok(v)
}
fn read_u32_list(reader: &mut Reader, max: usize) -> Result<Vec<u32>, AgentBoundaryError> {
    let n = usize::from(reader.u16()?);
    if n == 0 || n > max {
        return Err(AgentBoundaryError::CorruptResponse);
    }
    let mut v = Vec::with_capacity(n);
    for _ in 0..n {
        v.push(reader.u32()?);
    }
    Ok(v)
}
fn read_text_list(reader: &mut Reader, max: usize) -> Result<Vec<String>, AgentBoundaryError> {
    let n = usize::from(reader.u16()?);
    if n == 0 || n > max {
        return Err(AgentBoundaryError::CorruptResponse);
    }
    let mut v = Vec::with_capacity(n);
    for _ in 0..n {
        v.push(reader.text()?);
    }
    Ok(v)
}

fn decode_managed_evidence(
    reader: &mut Reader,
) -> Result<Vec<ManagedAgentEvidence>, AgentBoundaryError> {
    let count = usize::from(reader.u8()?);
    if count > MAX_EVIDENCE {
        return Err(AgentBoundaryError::CorruptResponse);
    }
    let mut evidence = Vec::with_capacity(count);
    for _ in 0..count {
        let value = ManagedAgentEvidence {
            evidence_id: reader.text()?,
            class: reader.text()?,
            verification: reader.u8()?,
        };
        if value.evidence_id.is_empty() || value.class.is_empty() || value.verification > 5 {
            return Err(AgentBoundaryError::CorruptResponse);
        }
        evidence.push(value);
    }
    Ok(evidence)
}
fn decode_managed_agent(reader: &mut Reader) -> Result<ManagedAgentView, AgentBoundaryError> {
    let value = ManagedAgentView {
        agent_id: reader.text()?,
        name: reader.text()?,
        purpose: reader.text()?,
        state: reader.u8()?,
        monthly_limit: reader.u128()?,
        currency: reader.text()?,
        limit_enforcement: reader.u8()?,
        period_start: reader.text()?,
        period_end: reader.text()?,
        spent: reader.u128()?,
        remaining: reader.u128()?,
        spend_verification: reader.u8()?,
        created_at: reader.text()?,
        updated_at: reader.text()?,
        evidence: decode_managed_evidence(reader)?,
    };
    if value.agent_id.is_empty()
        || value.name.is_empty()
        || value.purpose.is_empty()
        || value.state > 4
        || value.monthly_limit == 0
        || value.currency.is_empty()
        || value.limit_enforcement > 1
        || value.period_start.is_empty()
        || value.period_end.is_empty()
        || value.spend_verification > 5
        || value.created_at.is_empty()
        || value.updated_at.is_empty()
        || value.spent.checked_add(value.remaining).is_none()
    {
        return Err(AgentBoundaryError::CorruptResponse);
    }
    Ok(value)
}
fn decode_managed_journey(reader: &mut Reader) -> Result<ManagedAgentJourney, AgentBoundaryError> {
    let journey_id = reader.text()?;
    let kind = reader.text()?;
    let journey_state = reader.u8()?;
    let count = usize::from(reader.u8()?);
    if count == 0 || count > 16 {
        return Err(AgentBoundaryError::CorruptResponse);
    }
    let mut stages = Vec::with_capacity(count);
    for _ in 0..count {
        let stage = ManagedAgentJourneyStage {
            stage_id: reader.text()?,
            copy_key: reader.text()?,
            state: reader.u8()?,
            evidence: decode_managed_evidence(reader)?,
        };
        if stage.stage_id.is_empty() || stage.copy_key.is_empty() || stage.state > 3 {
            return Err(AgentBoundaryError::CorruptResponse);
        }
        stages.push(stage);
    }
    let value = ManagedAgentJourney {
        journey_id,
        kind,
        state: journey_state,
        stages,
        started_at: reader.text()?,
        updated_at: reader.text()?,
        evidence: decode_managed_evidence(reader)?,
    };
    if value.journey_id.is_empty()
        || value.kind.is_empty()
        || value.state > 3
        || value.started_at.is_empty()
        || value.updated_at.is_empty()
    {
        return Err(AgentBoundaryError::CorruptResponse);
    }
    Ok(value)
}
fn decode_managed_challenge(
    reader: &mut Reader,
) -> Result<ManagedAgentChallenge, AgentBoundaryError> {
    let value = ManagedAgentChallenge {
        agent_id: reader.text()?,
        kind: reader.u8()?,
        delay_seconds: reader.u64()?,
        ready_at: reader.text()?,
        evidence: decode_managed_evidence(reader)?,
    };
    if value.agent_id.is_empty()
        || value.kind > 1
        || value.delay_seconds == 0
        || value.ready_at.is_empty()
    {
        return Err(AgentBoundaryError::CorruptResponse);
    }
    Ok(value)
}

fn decode_native_ref(reader: &mut Reader) -> Result<Option<[u8; 32]>, AgentBoundaryError> {
    match reader.u8()? {
        0 => Ok(None),
        1 => {
            let id = reader.fixed()?;
            if id == [0; 32] {
                Err(AgentBoundaryError::CorruptResponse)
            } else {
                Ok(Some(id))
            }
        }
        _ => Err(AgentBoundaryError::CorruptResponse),
    }
}

fn program_carrier_bytes(carrier: &serde_json::Value, field: &str, bytes: &[u8]) -> bool {
    carrier
        .get(field)
        .and_then(serde_json::Value::as_array)
        .is_some_and(|values| {
            values.len() == bytes.len()
                && values
                    .iter()
                    .zip(bytes)
                    .all(|(value, byte)| value.as_u64() == Some(u64::from(*byte)))
        })
}

fn validate_native_program_payload(ordinal: u16, bytes: &[u8]) -> Result<(), AgentBoundaryError> {
    use layerx_types::program_call::NativeProgramCall;
    use layerx_types::program_lifecycle::{
        NativeProgramDeploy, NativeProgramUpgrade, NativeProgramWindDown,
    };
    let encoded = match ordinal {
        1 => NativeProgramDeploy::decode(bytes)
            .and_then(|value| value.encode())
            .map_err(|_| AgentBoundaryError::CorruptResponse)?,
        2 => NativeProgramUpgrade::decode(bytes)
            .and_then(|value| value.encode())
            .map_err(|_| AgentBoundaryError::CorruptResponse)?,
        3 => NativeProgramCall::decode(bytes)
            .and_then(|value| value.encode())
            .map_err(|_| AgentBoundaryError::CorruptResponse)?,
        7 => NativeProgramWindDown::decode(bytes)
            .and_then(|value| value.encode())
            .map_err(|_| AgentBoundaryError::CorruptResponse)?,
        _ => return Err(AgentBoundaryError::CorruptResponse),
    };
    if encoded != bytes {
        return Err(AgentBoundaryError::CorruptResponse);
    }
    Ok(())
}

fn decode_native_program_approval_facts(
    reader: &mut Reader,
) -> Result<NativeProgramApprovalFacts, AgentBoundaryError> {
    if reader.u16()? != 5 {
        return Err(AgentBoundaryError::CorruptResponse);
    }
    let approval_id = reader.fixed()?;
    let held_digest = reader.fixed()?;
    let owner = reader.text()?;
    let actor_bytes = reader.bytes()?;
    if actor_bytes.len() > MAX_TEXT {
        return Err(AgentBoundaryError::CorruptResponse);
    }
    let actor = String::from_utf8(actor_bytes).map_err(|_| AgentBoundaryError::CorruptResponse)?;
    let activity_module = reader.u16()?;
    let activity_ordinal = reader.u16()?;
    let state = match reader.u8()? {
        0 => NativeApprovalFactState::Awaiting,
        1 => NativeApprovalFactState::Granted,
        2 => NativeApprovalFactState::Rejected,
        3 => NativeApprovalFactState::Expired,
        4 => NativeApprovalFactState::Defective,
        5 => NativeApprovalFactState::NotRequired,
        _ => return Err(AgentBoundaryError::CorruptResponse),
    };
    let created_at_sequence = reader.u64()?;
    let budget_expiry_sequence = reader.u64()?;
    let created_at_unix_seconds = reader.u64()?;
    let activity_expires_at_unix_milliseconds = reader.u64()?;
    let release_ref = decode_native_ref(reader)?;
    let fee_asset = decode_native_ref(reader)?;
    let canonical_payload_bytes = reader.bytes()?;
    validate_native_program_payload(activity_ordinal, &canonical_payload_bytes)?;
    let semantics = match reader.u8()? {
        0 => NativeProgramApprovalSemantics::OperationOnly,
        1 => {
            let count = usize::from(reader.u16()?);
            if count == 0 || count > 256 {
                return Err(AgentBoundaryError::CorruptResponse);
            }
            let mut limits = Vec::with_capacity(count);
            let mut previous = None;
            for _ in 0..count {
                let kind = match reader.u8()? {
                    1 => NativeProgramChargeKind::Principal,
                    2 => NativeProgramChargeKind::ProgramSpend,
                    _ => return Err(AgentBoundaryError::CorruptResponse),
                };
                let source = reader.fixed()?;
                let asset = reader.fixed()?;
                let destination = decode_native_ref(reader)?;
                let maximum_amount = reader.u128()?;
                let key = (asset, source, kind, destination);
                if source == [0; 32]
                    || asset == [0; 32]
                    || destination.is_none()
                    || maximum_amount == 0
                    || previous.is_some_and(|old| old >= key)
                {
                    return Err(AgentBoundaryError::CorruptResponse);
                }
                previous = Some(key);
                limits.push(NativeProgramLimit {
                    kind,
                    source,
                    asset,
                    destination,
                    maximum_amount,
                });
            }
            NativeProgramApprovalSemantics::AuthorizedLimits(limits)
        }
        _ => return Err(AgentBoundaryError::CorruptResponse),
    };
    layerx_types::ids::Did::new(owner.as_bytes())
        .map_err(|_| AgentBoundaryError::CorruptResponse)?;
    layerx_types::ids::Did::new(actor.as_bytes())
        .map_err(|_| AgentBoundaryError::CorruptResponse)?;
    let module =
        ModuleId::from_u16(activity_module).map_err(|_| AgentBoundaryError::CorruptResponse)?;
    ActivityType::new(module, activity_ordinal).map_err(|_| AgentBoundaryError::CorruptResponse)?;
    if module != ModuleId::Programs
        || approval_id == [0; 32]
        || held_digest == [0; 32]
        || created_at_sequence >= budget_expiry_sequence
        || created_at_unix_seconds == 0
        || created_at_unix_seconds
            .checked_mul(1000)
            .is_none_or(|created| created >= activity_expires_at_unix_milliseconds)
        || release_ref.is_some() != (state == NativeApprovalFactState::Granted)
    {
        return Err(AgentBoundaryError::CorruptResponse);
    }
    Ok(NativeProgramApprovalFacts {
        approval_id,
        held_digest,
        owner,
        actor,
        activity_module,
        activity_ordinal,
        state,
        created_at_sequence,
        budget_expiry_sequence,
        created_at_unix_seconds,
        activity_expires_at_unix_milliseconds,
        release_ref,
        fee_asset,
        canonical_payload_bytes,
        semantics,
    })
}

fn decode_native_effect_approval_facts(
    reader: &mut Reader,
) -> Result<NativeEffectApprovalFacts, AgentBoundaryError> {
    if reader.u16()? != 3 {
        return Err(AgentBoundaryError::CorruptResponse);
    }
    let approval_id = reader.fixed()?;
    let held_digest = reader.fixed()?;
    let owner = reader.text()?;
    let actor = reader.text()?;
    let activity_module = reader.u16()?;
    let activity_ordinal = reader.u16()?;
    let state = match reader.u8()? {
        0 => NativeEffectApprovalFactState::Awaiting,
        1 => NativeEffectApprovalFactState::Granted,
        2 => NativeEffectApprovalFactState::Rejected,
        3 => NativeEffectApprovalFactState::Expired,
        5 => NativeEffectApprovalFactState::NotRequired,
        _ => return Err(AgentBoundaryError::CorruptResponse),
    };
    let requires_approval = match reader.u8()? {
        0 => false,
        1 => true,
        _ => return Err(AgentBoundaryError::CorruptResponse),
    };
    let created_at_sequence = reader.u64()?;
    let budget_expiry_sequence = reader.u64()?;
    let created_at_unix_seconds = reader.u64()?;
    let activity_expires_at_unix_milliseconds = reader.u64()?;
    let asset = reader.fixed()?;
    let fee_asset = reader.fixed()?;
    let fee_limit = reader.u128()?;
    let count = usize::from(reader.u16()?);
    if count > 256 {
        return Err(AgentBoundaryError::CorruptResponse);
    }
    let mut counterparties = Vec::with_capacity(count);
    for _ in 0..count {
        let role = match reader.u8()? {
            0 => disclosure::CounterpartyRole::Payer,
            1 => disclosure::CounterpartyRole::Recipient,
            _ => return Err(AgentBoundaryError::CorruptResponse),
        };
        let account = reader.fixed()?;
        if account == [0; 32] {
            return Err(AgentBoundaryError::CorruptResponse);
        };
        counterparties.push(disclosure::Counterparty { role, account });
    }
    let count = usize::from(reader.u16()?);
    if count > 256 {
        return Err(AgentBoundaryError::CorruptResponse);
    }
    let mut amounts = Vec::with_capacity(count);
    for _ in 0..count {
        let role = match reader.u8()? {
            0 => disclosure::AmountRole::Transfer,
            1 => disclosure::AmountRole::SpendingLimit,
            2 => disclosure::AmountRole::SupplyCap,
            3 => disclosure::AmountRole::PerDrawMaximum,
            4 => disclosure::AmountRole::GrantAllowance,
            _ => return Err(AgentBoundaryError::CorruptResponse),
        };
        amounts.push(disclosure::DisclosedAmount {
            role,
            value: reader.u128()?,
        });
    }
    let release_ref = decode_native_ref(reader)?;
    let submission_ref = decode_native_ref(reader)?;
    layerx_types::ids::Did::new(owner.as_bytes())
        .map_err(|_| AgentBoundaryError::CorruptResponse)?;
    layerx_types::ids::Did::new(actor.as_bytes())
        .map_err(|_| AgentBoundaryError::CorruptResponse)?;
    let module =
        ModuleId::from_u16(activity_module).map_err(|_| AgentBoundaryError::CorruptResponse)?;
    ActivityType::new(module, activity_ordinal).map_err(|_| AgentBoundaryError::CorruptResponse)?;
    if module == ModuleId::Programs
        || approval_id == [0; 32]
        || held_digest == [0; 32]
        || fee_asset == [0; 32]
        || created_at_unix_seconds == 0
        || created_at_sequence >= budget_expiry_sequence
        || created_at_unix_seconds
            .checked_mul(1000)
            .is_none_or(|created| created >= activity_expires_at_unix_milliseconds)
        || (requires_approval && state == NativeEffectApprovalFactState::NotRequired)
        || (!requires_approval
            && !matches!(
                state,
                NativeEffectApprovalFactState::NotRequired | NativeEffectApprovalFactState::Expired
            ))
        || (release_ref.is_some() != (state == NativeEffectApprovalFactState::Granted))
        || (submission_ref.is_some()
            && !matches!(
                state,
                NativeEffectApprovalFactState::Granted | NativeEffectApprovalFactState::NotRequired
            ))
    {
        return Err(AgentBoundaryError::CorruptResponse);
    }
    Ok(NativeEffectApprovalFacts {
        approval_id,
        held_digest,
        owner,
        actor,
        activity_module,
        activity_ordinal,
        state,
        requires_approval,
        created_at_sequence,
        budget_expiry_sequence,
        created_at_unix_seconds,
        activity_expires_at_unix_milliseconds,
        asset,
        fee_asset,
        fee_limit,
        counterparties,
        amounts,
        release_ref,
        submission_ref,
    })
}

fn decode_native_approval_facts(
    reader: &mut Reader,
) -> Result<NativeApprovalFacts, AgentBoundaryError> {
    if reader.u16()? != 2 {
        return Err(AgentBoundaryError::CorruptResponse);
    }
    let approval_id = reader.fixed()?;
    let held_digest = reader.fixed()?;
    let owner = reader.text()?;
    let actor = reader.text()?;
    let activity_module = reader.u16()?;
    let activity_ordinal = reader.u16()?;
    let state = match reader.u8()? {
        0 => NativeApprovalFactState::Awaiting,
        1 => NativeApprovalFactState::Granted,
        2 => NativeApprovalFactState::Rejected,
        3 => NativeApprovalFactState::Expired,
        4 => NativeApprovalFactState::Defective,
        5 => NativeApprovalFactState::NotRequired,
        _ => return Err(AgentBoundaryError::CorruptResponse),
    };
    let created_at_sequence = reader.u64()?;
    let budget_expiry_sequence = reader.u64()?;
    let created_at_unix_seconds = reader.u64()?;
    let activity_expires_at_unix_milliseconds = reader.u64()?;
    let submission_ref = match reader.u8()? {
        0 => None,
        1 => Some(reader.fixed()?),
        _ => return Err(AgentBoundaryError::CorruptResponse),
    };
    layerx_types::ids::Did::new(owner.as_bytes())
        .map_err(|_| AgentBoundaryError::CorruptResponse)?;
    layerx_types::ids::Did::new(actor.as_bytes())
        .map_err(|_| AgentBoundaryError::CorruptResponse)?;
    let module =
        ModuleId::from_u16(activity_module).map_err(|_| AgentBoundaryError::CorruptResponse)?;
    ActivityType::new(module, activity_ordinal).map_err(|_| AgentBoundaryError::CorruptResponse)?;
    if module != ModuleId::Programs
        || approval_id == [0; 32]
        || held_digest == [0; 32]
        || created_at_sequence >= budget_expiry_sequence
        || created_at_unix_seconds
            .checked_mul(1000)
            .is_none_or(|created| created >= activity_expires_at_unix_milliseconds)
        || submission_ref == Some([0; 32])
        || submission_ref.is_some() != (state == NativeApprovalFactState::Granted)
    {
        return Err(AgentBoundaryError::CorruptResponse);
    }
    Ok(NativeApprovalFacts {
        approval_id,
        held_digest,
        owner,
        actor,
        activity_module,
        activity_ordinal,
        state,
        created_at_sequence,
        budget_expiry_sequence,
        created_at_unix_seconds,
        activity_expires_at_unix_milliseconds,
        submission_ref,
    })
}

fn decode_approval_facts(reader: &mut Reader) -> Result<AgentApprovalFacts, AgentBoundaryError> {
    if reader.u16()? != 2 {
        return Err(AgentBoundaryError::CorruptResponse);
    };
    let approval = decode_approval(reader)?;
    let created_at_unix_seconds = reader.u64()?;
    let activity_expires_at_unix_seconds = reader.u64()?;
    if created_at_unix_seconds == 0
        || created_at_unix_seconds >= activity_expires_at_unix_seconds
        || activity_expires_at_unix_seconds != approval.held_activity.expiry.0
    {
        return Err(AgentBoundaryError::CorruptResponse);
    };
    Ok(AgentApprovalFacts {
        approval,
        created_at_unix_seconds,
        activity_expires_at_unix_seconds,
    })
}
fn decode_approval(
    reader: &mut Reader,
) -> Result<crate::approvals::AgentApprovalRecord, AgentBoundaryError> {
    use layerx_agent_api::identity::{
        ActivityType as ApiActivityType, AgentDid, Asset, AuthorityRef, ExplicitSet,
    };
    use layerx_agent_api::prepare::{DisclosedAmount, Disclosure, IdempotencyRef};
    let approval_id = reader.fixed()?;
    let canonical_digest = reader.fixed()?;
    let activity_type = ApiActivityType(reader.u16()?);
    let actor = AgentDid::new(reader.text()?).map_err(|_| AgentBoundaryError::CorruptResponse)?;
    let authority =
        AuthorityRef::new(reader.text()?).map_err(|_| AgentBoundaryError::CorruptResponse)?;
    let counterpart_count = usize::from(reader.u16()?);
    if counterpart_count > 64 {
        return Err(AgentBoundaryError::CorruptResponse);
    }
    let mut counterparties = Vec::with_capacity(counterpart_count);
    for _ in 0..counterpart_count {
        counterparties
            .push(AgentDid::new(reader.text()?).map_err(|_| AgentBoundaryError::CorruptResponse)?);
    }
    let amount_count = usize::from(reader.u16()?);
    if amount_count > 64 {
        return Err(AgentBoundaryError::CorruptResponse);
    }
    let mut amounts = Vec::with_capacity(amount_count);
    for _ in 0..amount_count {
        amounts.push(DisclosedAmount {
            counterparty: AgentDid::new(reader.text()?)
                .map_err(|_| AgentBoundaryError::CorruptResponse)?,
            amount: layerx_agent_api::Amount(reader.u128()?),
        });
    }
    let held_activity = Disclosure {
        canonical_digest,
        activity_type,
        actor,
        authority,
        counterparties: ExplicitSet::allow(counterparties),
        amounts: ExplicitSet::allow(amounts),
        asset: Asset::new(reader.text()?).map_err(|_| AgentBoundaryError::CorruptResponse)?,
        fee_limit: layerx_agent_api::Amount(reader.u128()?),
        expiry: TimestampSeconds(reader.u64()?),
        idempotency_key: IdempotencyRef::new(reader.text()?)
            .map_err(|_| AgentBoundaryError::CorruptResponse)?,
    };
    let canonical_bytes_digest = reader.fixed()?;
    let hold_reason_code = reader.text()?;
    let hold_reason = reader.text()?;
    let created_at_sequence = reader.u64()?;
    let expires_at_sequence = reader.u64()?;
    let state = match reader.u8()? {
        0 => crate::approvals::AgentApprovalState::AwaitingApproval,
        1 => crate::approvals::AgentApprovalState::Approved {
            submission_ref: reader.fixed()?,
        },
        2 => crate::approvals::AgentApprovalState::Rejected,
        3 => crate::approvals::AgentApprovalState::Expired,
        4 => crate::approvals::AgentApprovalState::Defective,
        _ => return Err(AgentBoundaryError::CorruptResponse),
    };
    Ok(crate::approvals::AgentApprovalRecord {
        approval_id,
        held_activity,
        canonical_bytes_digest,
        hold_reason_code,
        hold_reason,
        created_at_sequence,
        expires_at_sequence,
        state,
    })
}
fn decision(
    outcome: u8,
    submission_ref: Option<[u8; 32]>,
    winning: Option<u8>,
) -> Result<crate::approvals::AgentDecision, AgentBoundaryError> {
    use crate::approvals::{AgentDecision, AgentDecisionResolution, AgentDecisionStatus};
    let effective = if matches!(outcome, 4 | 5) {
        winning.ok_or(AgentBoundaryError::CorruptResponse)?
    } else {
        outcome
    };
    let status = match effective {
        0 => AgentDecisionStatus::Approved { submission_ref },
        1 => AgentDecisionStatus::Rejected,
        2 => AgentDecisionStatus::Expired,
        3 => AgentDecisionStatus::Defective,
        _ => return Err(AgentBoundaryError::CorruptResponse),
    };
    Ok(AgentDecision {
        status,
        resolution: if matches!(outcome, 4 | 5) {
            AgentDecisionResolution::AlreadyDecided
        } else {
            AgentDecisionResolution::Applied
        },
    })
}

impl AgentBoundary for AgentRuntime {
    fn native_journey_preview(
        &mut self,
        call: &Call<IdempotentMutation<PrepareRequest>>,
    ) -> Result<crate::journeys::NativeJourneyPreview, AgentBoundaryError> {
        let (context, expiry) = self
            .native_journey
            .take()
            .ok_or(AgentBoundaryError::Refused)?;
        let result = self.native_send_preview_v2(
            &context,
            &call.request().operation,
            expiry,
            context.commitment,
        );
        self.native_journey = Some((context, expiry));
        let preview = result?;
        let preparation = self.native_journey_preparation(
            &call.request().operation,
            preview.canonical_bytes,
            preview.signing_preimage,
        )?;
        Ok(crate::journeys::NativeJourneyPreview {
            preparation,
            purpose: preview.purpose,
            owner_public_key: preview.owner_public_key,
            head_sequence: preview.head_sequence,
            protocol_time_ms: preview.protocol_time_ms,
            revocation_sequence: preview.revocation_sequence,
        })
    }

    fn native_journey_prepare(
        &mut self,
        call: &Call<IdempotentMutation<PrepareRequest>>,
        purpose: &layerx_agent_api::identity::SignedNativeSendPurposeV1,
    ) -> Result<crate::journeys::NativeJourneyAdmission, AgentBoundaryError> {
        self.native_journey_admit(call, purpose, None)
    }

    fn native_journey_resume(
        &mut self,
        call: &Call<IdempotentMutation<PrepareRequest>>,
        purpose: &layerx_agent_api::identity::SignedNativeSendPurposeV1,
        approval_id: [u8; 32],
        held_digest: [u8; 32],
    ) -> Result<crate::journeys::NativeJourneyAdmission, AgentBoundaryError> {
        self.native_journey_admit(call, purpose, Some((approval_id, held_digest)))
    }

    fn prepare(
        &mut self,
        call: &Call<IdempotentMutation<PrepareRequest>>,
    ) -> Result<AgentPreparation, AgentBoundaryError> {
        let mutation = call.request();
        let request = &mutation.operation;
        let mut writer = Self::encode_mutation_header(PREPARE, mutation);
        writer.u32(request.protocol_activity_type);
        writer.text(request.actor.as_str())?;
        writer.text(request.authority.as_str())?;
        writer.u64(request.account_sequence.0);
        writer.u64(request.timestamp_bound.not_before.0);
        writer.u64(request.timestamp_bound.not_after.0);
        writer.text(request.idempotency_key.as_str())?;
        writer.u128(request.fee_limit.0);
        writer.bytes(request.payload.as_bytes())?;
        writer.fixed(&request.payload_hash);

        let mut reader = self.exchange(&writer.finish())?;
        let preparation_ref =
            PreparationRef::new(reader.text()?).map_err(|_| AgentBoundaryError::CorruptResponse)?;
        let unsigned_canonical_bytes = reader.bytes()?;
        let signing_preimage = reader.bytes()?;
        let activity_type = ActivityType::from_u32(reader.u32()?)
            .map_err(|_| AgentBoundaryError::CorruptResponse)?;
        let actor = layerx_agent_api::identity::AgentDid::new(reader.text()?)
            .map_err(|_| AgentBoundaryError::CorruptResponse)?;
        let authority = layerx_agent_api::identity::AuthorityRef::new(reader.text()?)
            .map_err(|_| AgentBoundaryError::CorruptResponse)?;
        let account_sequence = reader.u64()?;
        let not_before = reader.u64()?;
        let not_after = reader.u64()?;
        let fee_limit = reader.u128()?;
        let payload = reader.bytes()?;
        let payload_hash = reader.fixed::<32>()?;
        let idempotency_key = reader.fixed::<32>()?;
        reader.finish()?;

        let disclosure = disclosure::bind(&unsigned_canonical_bytes, &self.registry)
            .map_err(|_| AgentBoundaryError::CorruptResponse)?;
        if disclosure
            .reencode()
            .map_err(|_| AgentBoundaryError::CorruptResponse)?
            != unsigned_canonical_bytes
        {
            return Err(AgentBoundaryError::CorruptResponse);
        }
        Ok(AgentPreparation {
            preparation_ref,
            unsigned_canonical_bytes,
            signing_preimage,
            disclosure,
            actor,
            authority,
            account_sequence,
            not_before,
            not_after,
            fee_limit,
            activity_type,
            payload,
            payload_hash,
            idempotency_key,
        })
    }

    fn submit(
        &mut self,
        call: &Call<IdempotentMutation<SubmitRequest>>,
        signer_public_key: [u8; 32],
    ) -> Result<AgentObservation, AgentBoundaryError> {
        if self.native_journey.is_some() {
            return self.native_journey_submit(call, signer_public_key);
        }
        let mutation = call.request();
        let mut writer = Self::encode_mutation_header(SUBMIT, mutation);
        writer.text(mutation.operation.preparation_ref.as_str())?;
        writer.bytes(mutation.operation.signature.as_bytes())?;
        writer.fixed(&signer_public_key);
        match mutation.operation.approval_release_ref {
            Some(reference) => {
                writer.u8(1);
                writer.fixed(&reference);
            }
            None => writer.u8(0),
        }
        let mut reader = self.exchange(&writer.finish())?;
        Self::decode_observation(&mut reader)
    }

    fn track(&mut self, call: &Call<TrackRequest>) -> Result<AgentObservation, AgentBoundaryError> {
        let mut writer = Writer::new(TRACK);
        writer.text(call.request().submission_ref.as_str())?;
        let mut reader = self.exchange(&writer.finish())?;
        Self::decode_observation(&mut reader)
    }

    fn receipt_by_idempotency_key(
        &mut self,
        idempotency_key: [u8; 32],
        expected_activity_id: [u8; 32],
    ) -> Result<ReceiptLookup, AgentBoundaryError> {
        let mut writer = Writer::new(RECEIPT_LOOKUP);
        writer.fixed(&idempotency_key);
        writer.fixed(&expected_activity_id);
        let mut reader = self.exchange(&writer.finish())?;
        let found = reader.u8()?;
        let result = match found {
            0 => ReceiptLookup::Absent,
            1 => ReceiptLookup::Found(decode_receipt(&mut reader)?),
            _ => return Err(AgentBoundaryError::CorruptResponse),
        };
        reader.finish()?;
        Ok(result)
    }
}

impl DepositAgentBoundary for AgentRuntime {
    fn credit_receipt(
        &mut self,
        action_key: [u8; 32],
        activity_id: [u8; 32],
    ) -> Result<ReceiptMaterial, AgentBoundaryError> {
        match self.receipt_by_idempotency_key(action_key, activity_id)? {
            ReceiptLookup::Found(material) => Ok(material),
            ReceiptLookup::Absent => Err(AgentBoundaryError::Unavailable),
        }
    }
}

impl crate::approvals::ApprovalBoundary for AgentRuntime {
    fn approval(
        &mut self,
        approval_id: [u8; 32],
        at_sequence: u64,
    ) -> Result<crate::approvals::AgentApprovalRecord, crate::approvals::ApprovalBoundaryError>
    {
        self.approval_get(approval_id, at_sequence)
            .map_err(map_approval_error)
    }
    fn verified_budget_after(
        &mut self,
        hold: &crate::approvals::AgentApprovalRecord,
        at_sequence: u64,
    ) -> Result<crate::approvals::VerifiedBudgetAfter, crate::approvals::ApprovalBoundaryError>
    {
        let mut writer = Writer::new(APPROVAL_BUDGET_AFTER);
        writer.fixed(&hold.approval_id);
        writer.fixed(&hold.canonical_bytes_digest);
        writer.u64(at_sequence);
        let mut reader = self
            .exchange(&writer.finish())
            .map_err(map_approval_error)?;
        let decode=|reader:&mut Reader| -> Result<crate::approvals::VerifiedBudgetAfter,AgentBoundaryError> {
            if reader.u16()?!=2 || reader.fixed::<32>()?!=hold.approval_id
                || reader.fixed::<32>()?!=hold.canonical_bytes_digest {return Err(AgentBoundaryError::CorruptResponse)};
            let remaining=reader.u128()?;let level=match reader.u8()? {
                4=>Level::CheckpointFinalised,5=>Level::SettlementAnchored,
                _=>return Err(AgentBoundaryError::CorruptResponse)};
            let evidence_digest=reader.fixed()?;let observed_at_sequence=reader.u64()?;let asset=reader.fixed::<32>()?;
            if evidence_digest==[0;32] || asset==[0;32] || observed_at_sequence!=at_sequence {
                return Err(AgentBoundaryError::CorruptResponse)};
            reader.finish()?;Ok(crate::approvals::VerifiedBudgetAfter{remaining,level,evidence_digest,observed_at_sequence})
        };
        decode(&mut reader).map_err(map_approval_error)
    }

    fn track_released(
        &mut self,
        submission_ref: [u8; 32],
    ) -> Result<TrackedSubmission, crate::approvals::ApprovalBoundaryError> {
        let mut writer = Writer::new(TRACK);
        writer
            .text(&hex(&submission_ref))
            .map_err(map_approval_error)?;
        let mut reader = self
            .exchange(&writer.finish())
            .map_err(map_approval_error)?;
        Self::decode_observation(&mut reader)
            .map(|value| value.submission)
            .map_err(map_approval_error)
    }
}
impl crate::approvals::AgentDecisionBoundary for AgentRuntime {
    fn approve(
        &mut self,
        approval_id: [u8; 32],
        held_digest: [u8; 32],
        idempotency_key: &str,
        current_sequence: u64,
    ) -> Result<crate::approvals::AgentDecision, crate::approvals::ApprovalBoundaryError> {
        self.approval_decide(
            true,
            approval_id,
            held_digest,
            idempotency_key,
            current_sequence,
        )
        .map_err(map_approval_error)
    }
    fn reject(
        &mut self,
        approval_id: [u8; 32],
        held_digest: [u8; 32],
        idempotency_key: &str,
        current_sequence: u64,
    ) -> Result<crate::approvals::AgentDecision, crate::approvals::ApprovalBoundaryError> {
        self.approval_decide(
            false,
            approval_id,
            held_digest,
            idempotency_key,
            current_sequence,
        )
        .map_err(map_approval_error)
    }
}
fn map_approval_error(error: AgentBoundaryError) -> crate::approvals::ApprovalBoundaryError {
    match error {
        AgentBoundaryError::Unavailable => crate::approvals::ApprovalBoundaryError::Unavailable,
        AgentBoundaryError::Refused => crate::approvals::ApprovalBoundaryError::NotFound,
        AgentBoundaryError::CorruptResponse => crate::approvals::ApprovalBoundaryError::Corrupt,
    }
}
fn hex(bytes: &[u8]) -> String {
    const H: &[u8; 16] = b"0123456789abcdef";
    bytes
        .iter()
        .flat_map(|byte| {
            [
                H[(byte >> 4) as usize] as char,
                H[(byte & 15) as usize] as char,
            ]
        })
        .collect()
}

fn decode_tracked(reader: &mut Reader) -> Result<TrackedSubmission, AgentBoundaryError> {
    let submission_ref =
        SubmissionRef::new(reader.text()?).map_err(|_| AgentBoundaryError::CorruptResponse)?;
    let state = match reader.u8()? {
        0 => SubmissionState::Prepared,
        1 => SubmissionState::Signed,
        2 => SubmissionState::Queued,
        3 => SubmissionState::Submitted,
        4 => SubmissionState::Acknowledged,
        5 => SubmissionState::Unknown,
        6 => SubmissionState::Executed {
            receipt_ref: ReceiptRef::new(reader.text()?)
                .map_err(|_| AgentBoundaryError::CorruptResponse)?,
        },
        7 => SubmissionState::Failed {
            result: ResultCode::from_raw(reader.i32()?),
        },
        8 => SubmissionState::Expired,
        _ => return Err(AgentBoundaryError::CorruptResponse),
    };
    let verification_level = decode_level(reader.u8()?)?;
    let evidence_count = usize::from(reader.u8()?);
    if evidence_count > MAX_EVIDENCE {
        return Err(AgentBoundaryError::CorruptResponse);
    }
    let mut evidence = Vec::with_capacity(evidence_count);
    for _ in 0..evidence_count {
        evidence.push(EvidenceRef {
            kind: reader.text()?,
            digest: reader.fixed::<32>()?,
        });
    }
    let transition_count = usize::from(reader.u8()?);
    if transition_count > MAX_TRANSITIONS {
        return Err(AgentBoundaryError::CorruptResponse);
    }
    let mut transitions = Vec::with_capacity(transition_count);
    for _ in 0..transition_count {
        let from = decode_state_without_data(reader.u8()?)?;
        let to = decode_state_without_data(reader.u8()?)?;
        transitions.push(
            Transition {
                from,
                to,
                cause: reader.text()?,
                at: TimestampSeconds(reader.u64()?),
            }
            .validate()
            .map_err(|_| AgentBoundaryError::CorruptResponse)?,
        );
    }
    Ok(TrackedSubmission {
        submission_ref,
        state,
        evidence,
        verification_level,
        transitions,
    })
}

fn decode_state_without_data(value: u8) -> Result<SubmissionState, AgentBoundaryError> {
    match value {
        0 => Ok(SubmissionState::Prepared),
        1 => Ok(SubmissionState::Signed),
        2 => Ok(SubmissionState::Queued),
        3 => Ok(SubmissionState::Submitted),
        4 => Ok(SubmissionState::Acknowledged),
        5 => Ok(SubmissionState::Unknown),
        8 => Ok(SubmissionState::Expired),
        _ => Err(AgentBoundaryError::CorruptResponse),
    }
}

fn decode_level(value: u8) -> Result<Level, AgentBoundaryError> {
    match value {
        0 => Ok(Level::Unverified),
        1 => Ok(Level::SequencerSigned),
        2 => Ok(Level::BatchIncluded),
        3 => Ok(Level::StateProven),
        4 => Ok(Level::CheckpointFinalised),
        5 => Ok(Level::SettlementAnchored),
        _ => Err(AgentBoundaryError::CorruptResponse),
    }
}

fn decode_optional_receipt(
    reader: &mut Reader,
) -> Result<Option<ReceiptMaterial>, AgentBoundaryError> {
    match reader.u8()? {
        0 => Ok(None),
        1 => decode_receipt(reader).map(Some),
        _ => Err(AgentBoundaryError::CorruptResponse),
    }
}

fn decode_receipt(reader: &mut Reader) -> Result<ReceiptMaterial, AgentBoundaryError> {
    let canonical_bytes = reader.bytes()?;
    let authorised_batch = AuthorizedBatch::new(
        reader.fixed::<32>()?,
        reader.fixed::<32>()?,
        reader.fixed::<32>()?,
        reader.fixed::<32>()?,
        reader.fixed::<32>()?,
    );
    let verification_level = match reader.u8()? {
        1 => VerificationLevel::SEQUENCER_SIGNED,
        2 => VerificationLevel::BATCH_INCLUDED,
        3 => VerificationLevel::STATE_PROVEN,
        4 => VerificationLevel::CHECKPOINT_FINALISED,
        5 => VerificationLevel::SETTLEMENT_ANCHORED,
        _ => return Err(AgentBoundaryError::CorruptResponse),
    };
    Ok(ReceiptMaterial {
        canonical_bytes,
        authorised_batch,
        verification_level,
    })
}

struct Writer(Zeroizing<Vec<u8>>);

impl Writer {
    fn new(operation: u8) -> Self {
        let mut bytes = Vec::with_capacity(256);
        bytes.extend_from_slice(MAGIC);
        bytes.push(operation);
        Self(Zeroizing::new(bytes))
    }
    fn u8(&mut self, value: u8) {
        self.0.push(value);
    }
    fn u16(&mut self, value: u16) {
        self.0.extend_from_slice(&value.to_be_bytes());
    }
    fn u64(&mut self, value: u64) {
        self.0.extend_from_slice(&value.to_be_bytes());
    }
    fn u32(&mut self, value: u32) {
        self.0.extend_from_slice(&value.to_be_bytes());
    }
    fn u128(&mut self, value: u128) {
        self.0.extend_from_slice(&value.to_be_bytes());
    }
    fn fixed(&mut self, value: &[u8]) {
        self.0.extend_from_slice(value);
    }
    fn text(&mut self, value: &str) -> Result<(), AgentBoundaryError> {
        if value.is_empty() || value.len() > MAX_TEXT {
            return Err(AgentBoundaryError::Refused);
        }
        self.bytes(value.as_bytes())
    }
    fn bytes(&mut self, value: &[u8]) -> Result<(), AgentBoundaryError> {
        let length = u32::try_from(value.len()).map_err(|_| AgentBoundaryError::Refused)?;
        if value.is_empty() || value.len() > MAX_BYTES {
            return Err(AgentBoundaryError::Refused);
        }
        self.u32(length);
        self.fixed(value);
        Ok(())
    }
    fn finish(mut self) -> Vec<u8> {
        std::mem::take(&mut *self.0)
    }
    fn finish_secret(mut self) -> Zeroizing<Vec<u8>> {
        Zeroizing::new(std::mem::take(&mut *self.0))
    }
}

struct Reader {
    bytes: Zeroizing<Vec<u8>>,
    offset: usize,
}

impl Reader {
    fn new(bytes: Vec<u8>) -> Self {
        Self {
            bytes: Zeroizing::new(bytes),
            offset: 0,
        }
    }
    fn take(&mut self, length: usize) -> Result<&[u8], AgentBoundaryError> {
        let end = self
            .offset
            .checked_add(length)
            .ok_or(AgentBoundaryError::CorruptResponse)?;
        let value = self
            .bytes
            .get(self.offset..end)
            .ok_or(AgentBoundaryError::CorruptResponse)?;
        self.offset = end;
        Ok(value)
    }
    fn u8(&mut self) -> Result<u8, AgentBoundaryError> {
        Ok(self.take(1)?[0])
    }
    fn u16(&mut self) -> Result<u16, AgentBoundaryError> {
        Ok(u16::from_be_bytes(self.fixed()?))
    }
    fn u32(&mut self) -> Result<u32, AgentBoundaryError> {
        Ok(u32::from_be_bytes(self.fixed()?))
    }
    fn i32(&mut self) -> Result<i32, AgentBoundaryError> {
        Ok(i32::from_be_bytes(self.fixed()?))
    }
    fn u64(&mut self) -> Result<u64, AgentBoundaryError> {
        Ok(u64::from_be_bytes(self.fixed()?))
    }
    fn u128(&mut self) -> Result<u128, AgentBoundaryError> {
        Ok(u128::from_be_bytes(self.fixed()?))
    }
    fn fixed<const N: usize>(&mut self) -> Result<[u8; N], AgentBoundaryError> {
        self.take(N)?
            .try_into()
            .map_err(|_| AgentBoundaryError::CorruptResponse)
    }
    fn bytes(&mut self) -> Result<Vec<u8>, AgentBoundaryError> {
        let length =
            usize::try_from(self.u32()?).map_err(|_| AgentBoundaryError::CorruptResponse)?;
        if length == 0 || length > MAX_BYTES {
            return Err(AgentBoundaryError::CorruptResponse);
        }
        Ok(self.take(length)?.to_vec())
    }
    fn text(&mut self) -> Result<String, AgentBoundaryError> {
        let bytes = self.bytes()?;
        if bytes.len() > MAX_TEXT {
            return Err(AgentBoundaryError::CorruptResponse);
        }
        String::from_utf8(bytes).map_err(|_| AgentBoundaryError::CorruptResponse)
    }
    fn finish(&self) -> Result<(), AgentBoundaryError> {
        if self.offset == self.bytes.len() {
            Ok(())
        } else {
            Err(AgentBoundaryError::CorruptResponse)
        }
    }
}

#[cfg(test)]
mod subject_tests {
    use super::*;

    fn runtime() -> AgentRuntime {
        let kind =
            ActivityType::new(ModuleId::Governance, 1).unwrap_or_else(|_| panic!("Governance1"));
        let registry =
            ModuleRegistry::new(&[ModuleRegistration::new(ModuleId::Governance, &[kind])
                .unwrap_or_else(|_| panic!("module"))])
            .unwrap_or_else(|_| panic!("registry"));
        AgentRuntime::new(
            "/tmp/lxp-subject-wire.sock",
            Limits {
                maximum_frame_bytes: MAX_BYTES,
                maximum_connections: 2,
                maximum_streams: 1,
                maximum_queued_bytes: MAX_BYTES,
                deadline: std::time::Duration::from_secs(1),
            },
            registry,
        )
        .unwrap_or_else(|_| panic!("typed runtime"))
    }

    #[test]
    fn cloned_subject_handle_binds_every_coordinate_without_changing_original_head_frame() {
        let prototype = runtime();
        let principal =
            crate::store::PrincipalId::new("act_person").unwrap_or_else(|_| panic!("principal"));
        let owner =
            layerx_types::ids::Did::new(b"did:layerx:person").unwrap_or_else(|_| panic!("owner"));
        let account = layerx_types::account::AccountId::parse("agent:did:layerx:person:main")
            .unwrap_or_else(|_| panic!("account"));
        let bound = prototype
            .for_subject(&principal, &owner, &account, [1; 32])
            .unwrap_or_else(|_| panic!("subject"));
        let original = Writer::new(HEAD).finish();
        assert_eq!(
            prototype
                .scoped_request(&original)
                .unwrap_or_else(|_| panic!("legacy"))
                .as_slice(),
            original
        );
        let wrapped = bound
            .scoped_request(&original)
            .unwrap_or_else(|_| panic!("scoped head"));
        let mut decoded = Reader::new(wrapped.to_vec());
        assert_eq!(
            decoded.fixed::<8>().unwrap_or_else(|_| panic!("magic")),
            *MAGIC
        );
        assert_eq!(decoded.u8().unwrap_or_else(|_| panic!("opcode")), 44);
        assert_eq!(
            decoded.text().unwrap_or_else(|_| panic!("principal")),
            "act_person"
        );
        assert_eq!(
            decoded.text().unwrap_or_else(|_| panic!("owner")),
            "did:layerx:person"
        );
        assert_eq!(
            decoded.bytes().unwrap_or_else(|_| panic!("account")),
            account.canonical().as_bytes()
        );
        assert_eq!(
            decoded.fixed::<32>().unwrap_or_else(|_| panic!("asset")),
            [1; 32]
        );
        assert_eq!(
            decoded.bytes().unwrap_or_else(|_| panic!("original")),
            original
        );
        decoded.finish().unwrap_or_else(|_| panic!("exact frame"));
        assert!(bound
            .for_subject(&principal, &owner, &account, [1; 32])
            .is_err());
        assert!(bound.scoped_request(&wrapped).is_err());
        assert!(prototype
            .for_subject(&principal, &owner, &account, [0; 32])
            .is_err());
        let other = layerx_types::account::AccountId::parse("agent:did:layerx:other:main")
            .unwrap_or_else(|_| panic!("other account"));
        assert!(prototype
            .for_subject(&principal, &owner, &other, [1; 32])
            .is_err());
    }
}

impl AgentRuntime {
    pub fn native_send_owner_context(
        &mut self,
        session: &super::native_send::HumanOwnerNativeSessionV1,
        request_id: u64,
    ) -> Result<super::native_send::NativeSendOwnerCoordinatesV1, AgentBoundaryError> {
        let mut writer = Writer::new(64);
        writer.text(&session.tenant)?;
        writer.fixed(&session.session_id);
        writer.fixed(session.credential());
        writer.u64(session.generation);
        writer.fixed(&session.owner_public_key);
        writer.u64(request_id);
        let mut reader = self.exchange_secret(&writer.finish_secret())?;
        let actor = reader.text()?;
        let kind = reader.u8()?;
        let authority: [u8; 32] = reader.fixed()?;
        if kind != 2 || authority != session.grant_id {
            return Err(AgentBoundaryError::CorruptResponse);
        }
        let value = super::native_send::NativeSendOwnerCoordinatesV1 {
            actor,
            authority: format!(
                "session:{}",
                authority
                    .iter()
                    .map(|byte| format!("{byte:02x}"))
                    .collect::<String>()
            ),
            generation: reader.u64()?,
            head_sequence: reader.u64()?,
            protocol_time_ms: reader.u64()?,
            account_sequence: reader.u64()?,
            expiry_sequence: reader.u64()?,
            expiry_ms: reader.u64()?,
            owner_public_key: reader.fixed()?,
            revocation_sequence: reader.u64()?,
            native_fee_asset: reader.fixed()?,
        };
        reader.finish()?;
        if value.actor != session.owner
            || value.generation != session.generation
            || value.owner_public_key != session.owner_public_key
            || value.head_sequence == 0
            || value.revocation_sequence == 0
            || value.native_fee_asset == [0; 32]
        {
            return Err(AgentBoundaryError::CorruptResponse);
        }
        Ok(value)
    }

    pub fn native_send_preview(
        &mut self,
        context: &super::native_send::HumanOwnerNativeContextV1,
        request: &PrepareRequest,
        purpose_expires_ms: u64,
        commitment: [u8; 32],
    ) -> Result<super::native_send::NativeSendPreviewV1, AgentBoundaryError> {
        let grant = context
            .signed_grant()
            .map_err(|_| AgentBoundaryError::Refused)?;
        let session = &context.session;
        let mut writer = Writer::new(63);
        writer.text(&session.tenant)?;
        writer.fixed(&session.session_id);
        writer.fixed(session.credential());
        writer.u64(session.generation);
        writer.u64(u64::from_be_bytes(
            context.capability_id[..8]
                .try_into()
                .map_err(|_| AgentBoundaryError::Refused)?,
        ));
        writer.u32(request.protocol_activity_type);
        writer.text(request.actor.as_str())?;
        writer.text(request.authority.as_str())?;
        writer.u64(request.account_sequence.0);
        writer.u64(request.timestamp_bound.not_before.0);
        writer.u64(request.timestamp_bound.not_after.0);
        writer.text(request.idempotency_key.as_str())?;
        writer.u128(request.fee_limit.0);
        writer.bytes(request.payload.as_bytes())?;
        writer.fixed(&request.payload_hash);
        writer.fixed(&context.capability_id);
        writer.fixed(&session.owner_public_key);
        writer.u64(purpose_expires_ms);
        writer.fixed(&commitment);
        writer.bytes(&grant.capability)?;
        writer.bytes(&grant.session_scope)?;
        writer.u64(grant.expires_at_ms);
        writer.fixed(&grant.owner_public_key);
        writer.fixed(&grant.signature);
        let mut reader = self.exchange_secret(&writer.finish_secret())?;
        let canonical_bytes = reader.bytes()?;
        let signing_preimage = reader.bytes()?;
        let purpose = layerx_agent_api::identity::NativePreparationPurposeV1::from_canonical_bytes(
            &reader.bytes()?,
        )
        .map_err(|_| AgentBoundaryError::CorruptResponse)?;
        let value = super::native_send::NativeSendPreviewV1 {
            canonical_bytes,
            signing_preimage,
            purpose,
            head_sequence: reader.u64()?,
            protocol_time_ms: reader.u64()?,
            owner_public_key: reader.fixed()?,
            revocation_sequence: reader.u64()?,
        };
        reader.finish()?;
        let canonical_digest: [u8; 32] = sha2::Sha256::digest(&value.canonical_bytes).into();
        if value.purpose.tenant.as_str() != session.tenant
            || value.purpose.agent_did.as_str() != session.owner
            || value
                .purpose
                .session_id
                .to_bytes()
                .map_err(|_| AgentBoundaryError::CorruptResponse)?
                != session.session_id
            || value
                .purpose
                .capability_id
                .to_bytes()
                .map_err(|_| AgentBoundaryError::CorruptResponse)?
                != context.capability_id
            || value.purpose.generation != session.generation
            || value.purpose.expires_at_ms != purpose_expires_ms
            || value.purpose.commitment != commitment
            || value.purpose.canonical_digest != canonical_digest
            || value.owner_public_key != session.owner_public_key
            || value.head_sequence < session.grant_sequence
            || value.revocation_sequence == 0
            || value.revocation_sequence > value.head_sequence
            || value.protocol_time_ms >= purpose_expires_ms
            || value.signing_preimage.is_empty()
        {
            return Err(AgentBoundaryError::CorruptResponse);
        }
        Ok(value)
    }
}

impl AgentRuntime {
    fn native_journey_submit(
        &mut self,
        call: &Call<IdempotentMutation<SubmitRequest>>,
        signer_public_key: [u8; 32],
    ) -> Result<AgentObservation, AgentBoundaryError> {
        let (context, _) = self
            .native_journey
            .as_ref()
            .ok_or(AgentBoundaryError::Refused)?;
        if signer_public_key != context.session.owner_public_key {
            return Err(AgentBoundaryError::Refused);
        }
        let mutation = call.request();
        let body = layerx_sdk::native_effect::encode_native_send_submit(
            &mutation.operation,
            &signer_public_key,
        )
        .map_err(|_| AgentBoundaryError::Refused)?;
        let credential = layerx_sdk::agent_envelope::EnvelopeCredential::new(
            &context.session.tenant,
            context.session.session_id,
            *context.session.credential(),
            context.session.generation,
        )
        .map_err(|_| AgentBoundaryError::Refused)?;
        let envelope = layerx_sdk::agent_envelope::encode_envelope(
            layerx_sdk::Operation::Submit,
            mutation.request_id,
            &body,
            Some(&credential),
            Some(mutation.key),
        )
        .map_err(|_| AgentBoundaryError::Refused)?;
        let encoded =
            Zeroizing::new(serde_json::to_vec(&envelope).map_err(|_| AgentBoundaryError::Refused)?);
        let mut writer = Writer::new(67);
        writer.u8(1);
        writer.bytes(&encoded)?;
        let mut reader = self.exchange_secret(&writer.finish_secret())?;
        if reader.u8()? != 1 {
            return Err(AgentBoundaryError::CorruptResponse);
        }
        let status = reader.u16()?;
        let result = reader.bytes()?;
        reader.finish()?;
        let result: serde_json::Value =
            serde_json::from_slice(&result).map_err(|_| AgentBoundaryError::CorruptResponse)?;
        let result = layerx_sdk::agent_envelope::decode_response(status, &result)
            .ok_or(AgentBoundaryError::CorruptResponse)?
            .map_err(|error| {
                if error.retriability == layerx_agent_api::error::Retriability::Retriable {
                    AgentBoundaryError::Unavailable
                } else {
                    AgentBoundaryError::Refused
                }
            })?;
        if result.request_id != mutation.request_id {
            return Err(AgentBoundaryError::CorruptResponse);
        }
        let value = result
            .value
            .as_object()
            .ok_or(AgentBoundaryError::CorruptResponse)?;
        if value.len() != 3 || !value.contains_key("receipt") {
            return Err(AgentBoundaryError::CorruptResponse);
        }
        let activity_id = decode_hex32(
            value
                .get("activity_id")
                .and_then(serde_json::Value::as_str)
                .ok_or(AgentBoundaryError::CorruptResponse)?,
        )?;
        if activity_id == [0; 32] {
            return Err(AgentBoundaryError::CorruptResponse);
        }
        let submission = value
            .get("submission")
            .and_then(serde_json::Value::as_object)
            .ok_or(AgentBoundaryError::CorruptResponse)?;
        let reference = SubmissionRef::new(
            submission
                .get("submission_ref")
                .and_then(serde_json::Value::as_str)
                .ok_or(AgentBoundaryError::CorruptResponse)?
                .to_owned(),
        )
        .map_err(|_| AgentBoundaryError::CorruptResponse)?;
        let mut writer = Writer::new(TRACK);
        writer.text(reference.as_str())?;
        let mut reader = self.exchange(&writer.finish())?;
        let observation = Self::decode_observation(&mut reader)?;
        if observation.activity_id != activity_id
            || observation.submission.submission_ref != reference
        {
            return Err(AgentBoundaryError::CorruptResponse);
        }
        Ok(observation)
    }

    pub fn bind_native_journey_context(
        &mut self,
        context: super::native_send::HumanOwnerNativeContextV1,
        expires_at_ms: u64,
    ) -> Result<(), AgentBoundaryError> {
        let subject = self.subject.as_ref().ok_or(AgentBoundaryError::Refused)?;
        if subject.owner.as_bytes() != context.session.owner.as_bytes()
            || subject.principal != context.session.principal
            || expires_at_ms == 0
            || expires_at_ms > context.expires_at_ms
            || context.commitment == [0; 32]
        {
            return Err(AgentBoundaryError::Refused);
        }
        context
            .signed_grant()
            .map_err(|_| AgentBoundaryError::Refused)?;
        self.native_journey = Some((context, expires_at_ms));
        Ok(())
    }

    fn native_journey_preparation(
        &self,
        request: &PrepareRequest,
        canonical: Vec<u8>,
        preimage: Vec<u8>,
    ) -> Result<AgentPreparation, AgentBoundaryError> {
        let activity = layerx_wire::activity::decode_unsigned(&canonical, &self.registry)
            .map_err(|_| AgentBoundaryError::CorruptResponse)?;
        let actual_preimage = layerx_wire::sign::preimage(&activity)
            .map_err(|_| AgentBoundaryError::CorruptResponse)?;
        if activity.activity_type().value() != request.protocol_activity_type
            || activity.actor_did() != request.actor.as_str().as_bytes()
            || activity.account_sequence() != request.account_sequence.0
            || activity.timestamp_bound().not_before != request.timestamp_bound.not_before.0
            || activity.timestamp_bound().not_after != request.timestamp_bound.not_after.0
            || activity.fee_limit().value() != request.fee_limit.0
            || activity.payload().as_bytes() != request.payload.as_bytes()
            || activity.payload_hash() != request.payload_hash
            || activity.idempotency_key() != decode_hex32(request.idempotency_key.as_str())?
            || actual_preimage != preimage
        {
            return Err(AgentBoundaryError::CorruptResponse);
        }
        let id: [u8; 32] = sha2::Sha256::digest(&canonical).into();
        let disclosure = disclosure::bind(&canonical, &self.registry)
            .map_err(|_| AgentBoundaryError::CorruptResponse)?;
        if disclosure
            .reencode()
            .map_err(|_| AgentBoundaryError::CorruptResponse)?
            != canonical
        {
            return Err(AgentBoundaryError::CorruptResponse);
        }
        let hex: String = id.iter().map(|byte| format!("{byte:02x}")).collect();
        let idempotency_key = decode_hex32(request.idempotency_key.as_str())?;
        Ok(AgentPreparation {
            preparation_ref: PreparationRef::new(hex)
                .map_err(|_| AgentBoundaryError::CorruptResponse)?,
            unsigned_canonical_bytes: canonical,
            signing_preimage: preimage,
            disclosure,
            actor: request.actor.clone(),
            authority: request.authority.clone(),
            account_sequence: request.account_sequence.0,
            not_before: request.timestamp_bound.not_before.0,
            not_after: request.timestamp_bound.not_after.0,
            fee_limit: request.fee_limit.0,
            activity_type: ActivityType::from_u32(request.protocol_activity_type)
                .map_err(|_| AgentBoundaryError::Refused)?,
            payload: request.payload.as_bytes().to_vec(),
            payload_hash: request.payload_hash,
            idempotency_key,
        })
    }

    fn native_journey_admit(
        &mut self,
        call: &Call<IdempotentMutation<PrepareRequest>>,
        purpose: &layerx_agent_api::identity::SignedNativeSendPurposeV1,
        expected: Option<([u8; 32], [u8; 32])>,
    ) -> Result<crate::journeys::NativeJourneyAdmission, AgentBoundaryError> {
        use crate::journeys::{AgentBoundary as _, NativeJourneyApprovalState as State};
        if let Some((approval_id, held_digest)) = expected {
            let (context, expiry) = self
                .native_journey
                .take()
                .ok_or(AgentBoundaryError::Refused)?;
            let coordinates =
                self.native_send_owner_context(&context.session, call.request().request_id.0);
            self.native_journey = Some((context, expiry));
            let coordinates = coordinates?;
            let (context, expiry) = self
                .native_journey
                .as_ref()
                .ok_or(AgentBoundaryError::Refused)?;
            let statement = &purpose.purpose;
            let request = &call.request().operation;
            if statement.preparation_id != approval_id
                || held_digest == [0; 32]
                || statement.tenant.as_str() != context.session.tenant
                || statement.agent_did.as_str() != context.session.owner
                || statement.owner_did.as_str() != context.session.owner
                || statement.agent_did != request.actor
                || statement.owner_public_key != context.session.owner_public_key
                || purpose.owner_public_key != context.session.owner_public_key
                || statement
                    .session_id
                    .to_bytes()
                    .map_err(|_| AgentBoundaryError::Refused)?
                    != context.session.session_id
                || statement
                    .capability_id
                    .to_bytes()
                    .map_err(|_| AgentBoundaryError::Refused)?
                    != context.capability_id
                || statement.generation != context.session.generation
                || statement.expires_at_ms != *expiry
                || statement.expires_at_ms > context.expires_at_ms
                || statement.commitment != context.commitment
                || statement.economic_action != context.session.session_id
                || statement.idempotency_key != decode_hex32(request.idempotency_key.as_str())?
                || coordinates.protocol_time_ms >= coordinates.expiry_ms
                || coordinates.head_sequence < context.session.grant_sequence
                || coordinates.revocation_sequence == 0
                || coordinates.revocation_sequence > coordinates.head_sequence
            {
                return Err(AgentBoundaryError::Refused);
            }
            let purpose_digest: [u8; 32] = sha2::Sha256::digest(
                statement
                    .canonical_bytes()
                    .map_err(|_| AgentBoundaryError::Refused)?,
            )
            .into();
            layerx_crypto::ed25519::verify_digest(
                &purpose.owner_public_key,
                &purpose.signature,
                &purpose_digest,
            )
            .map_err(|_| AgentBoundaryError::Refused)?;
            let facts = self.native_effect_approval_get_facts(approval_id)?;
            if matches!(
                facts.state,
                NativeEffectApprovalFactState::Rejected | NativeEffectApprovalFactState::Expired
            ) {
                let material = self.native_effect_approval_material(approval_id, held_digest)?;
                if facts.approval_id != approval_id
                    || facts.held_digest != held_digest
                    || facts.actor != request.actor.as_str()
                    || facts.owner != statement.owner_did.as_str()
                    || facts.activity_module != 1
                    || facts.activity_ordinal != 5
                    || facts.fee_limit != request.fee_limit.0
                    || facts.release_ref.is_some()
                    || facts.submission_ref.is_some()
                    || material.owner != statement.owner_did.as_str()
                    || sha2::Sha256::digest(&material.canonical_unsigned_bytes)[..]
                        != statement.canonical_digest
                {
                    return Err(AgentBoundaryError::CorruptResponse);
                }
                let activity = layerx_wire::activity::decode_unsigned(
                    &material.canonical_unsigned_bytes,
                    &self.registry,
                )
                .map_err(|_| AgentBoundaryError::CorruptResponse)?;
                if activity.protocol_version() != statement.protocol_version
                    || activity.network_id() != statement.network_id
                    || layerx_agent_api::identity::NativeActivity::from(activity.activity_type())
                        != statement.activity
                    || activity.idempotency_key() != statement.idempotency_key
                {
                    return Err(AgentBoundaryError::CorruptResponse);
                }
                let preimage = layerx_wire::sign::preimage(&activity)
                    .map_err(|_| AgentBoundaryError::CorruptResponse)?;
                let preparation = self.native_journey_preparation(
                    request,
                    material.canonical_unsigned_bytes,
                    preimage,
                )?;
                return Ok(crate::journeys::NativeJourneyAdmission {
                    preparation,
                    approval_id,
                    held_digest,
                    state: if facts.state == NativeEffectApprovalFactState::Rejected {
                        State::Rejected
                    } else {
                        State::Expired
                    },
                    release_ref: None,
                    head_sequence: coordinates.head_sequence,
                    protocol_time_ms: coordinates.protocol_time_ms,
                    revocation_sequence: coordinates.revocation_sequence,
                });
            }
        }
        let preview = self.native_journey_preview(call)?;
        let (context, _) = self
            .native_journey
            .as_ref()
            .ok_or(AgentBoundaryError::Refused)?;
        if preview.purpose != purpose.purpose
            || purpose.owner_public_key != preview.owner_public_key
            || purpose.signature == [0; 64]
        {
            return Err(AgentBoundaryError::Refused);
        }
        let request = &call.request().operation;
        let id = purpose.purpose.preparation_id;
        if expected.is_none() {
            let typed = layerx_agent_api::identity::NativeSendPrepareRequestV1 {
                activity: layerx_agent_api::identity::NativeActivity::new(1, 5)
                    .map_err(|_| AgentBoundaryError::Refused)?,
                actor: request.actor.clone(),
                authority: request.authority.as_str().to_owned(),
                account_sequence: request.account_sequence.0,
                not_before: request.timestamp_bound.not_before.0,
                not_after: request.timestamp_bound.not_after.0,
                idempotency_key: preview.preparation.idempotency_key,
                fee_limit: request.fee_limit.0,
                payload: request.payload.as_bytes().to_vec(),
                payload_hash: request.payload_hash,
                capability_id: purpose.purpose.capability_id.clone(),
                purpose: purpose.clone(),
                local_grant: Some(
                    context
                        .signed_grant()
                        .map_err(|_| AgentBoundaryError::Refused)?,
                ),
            };
            let body = layerx_sdk::native_effect::encode_native_send_prepare(&typed)
                .map_err(|_| AgentBoundaryError::Refused)?;
            let credential = layerx_sdk::agent_envelope::EnvelopeCredential::new(
                &context.session.tenant,
                context.session.session_id,
                *context.session.credential(),
                context.session.generation,
            )
            .map_err(|_| AgentBoundaryError::Refused)?;
            let envelope = layerx_sdk::agent_envelope::encode_envelope(
                layerx_sdk::Operation::Prepare,
                call.request().request_id,
                &body,
                Some(&credential),
                Some(call.request().key),
            )
            .map_err(|_| AgentBoundaryError::Refused)?;
            let bytes = Zeroizing::new(
                serde_json::to_vec(&envelope).map_err(|_| AgentBoundaryError::Refused)?,
            );
            let mut writer = Writer::new(67);
            writer.u8(1);
            writer.bytes(&bytes)?;
            let mut reader = self.exchange_secret(&writer.finish_secret())?;
            if reader.u8()? != 1 {
                return Err(AgentBoundaryError::CorruptResponse);
            }
            let status = reader.u16()?;
            let result = reader.bytes()?;
            reader.finish()?;
            if status != 200 {
                return Err(AgentBoundaryError::Refused);
            }
            let result: serde_json::Value =
                serde_json::from_slice(&result).map_err(|_| AgentBoundaryError::CorruptResponse)?;
            let value = layerx_sdk::agent_envelope::decode_native_preparation(&result["value"])
                .ok_or(AgentBoundaryError::CorruptResponse)?;
            if value.preparation_id != id
                || value.canonical_bytes != preview.preparation.unsigned_canonical_bytes
                || value.signing_preimage != preview.preparation.signing_preimage
            {
                return Err(AgentBoundaryError::CorruptResponse);
            }
        }
        let facts = self.native_effect_approval_get_facts(id)?;
        if expected.is_some_and(|(approval, digest)| approval != id || digest != facts.held_digest)
            || facts.actor != request.actor.as_str()
            || facts.activity_module != 1
            || facts.activity_ordinal != 5
            || facts.held_digest == [0; 32]
            || facts.fee_limit != request.fee_limit.0
        {
            return Err(AgentBoundaryError::CorruptResponse);
        }
        let material = self.native_effect_approval_material(id, facts.held_digest)?;
        if material.canonical_unsigned_bytes != preview.preparation.unsigned_canonical_bytes {
            return Err(AgentBoundaryError::CorruptResponse);
        }
        let state = match facts.state {
            NativeEffectApprovalFactState::Awaiting => State::Awaiting,
            NativeEffectApprovalFactState::Granted => State::Granted,
            NativeEffectApprovalFactState::Rejected => State::Rejected,
            NativeEffectApprovalFactState::Expired => State::Expired,
            NativeEffectApprovalFactState::NotRequired => State::NotRequired,
        };
        if (state == State::Granted) != facts.release_ref.is_some()
            || facts.submission_ref.is_some()
        {
            return Err(AgentBoundaryError::CorruptResponse);
        }
        Ok(crate::journeys::NativeJourneyAdmission {
            preparation: preview.preparation,
            approval_id: id,
            held_digest: facts.held_digest,
            state,
            release_ref: facts.release_ref,
            head_sequence: preview.head_sequence,
            protocol_time_ms: preview.protocol_time_ms,
            revocation_sequence: preview.revocation_sequence,
        })
    }
}

fn decode_hex32(text: &str) -> Result<[u8; 32], AgentBoundaryError> {
    if text.len() != 64
        || !text
            .bytes()
            .all(|byte| byte.is_ascii_digit() || (b'a'..=b'f').contains(&byte))
    {
        return Err(AgentBoundaryError::Refused);
    }
    let mut bytes = [0; 32];
    for (i, pair) in text.as_bytes().chunks_exact(2).enumerate() {
        let part = std::str::from_utf8(pair).map_err(|_| AgentBoundaryError::Refused)?;
        bytes[i] = u8::from_str_radix(part, 16).map_err(|_| AgentBoundaryError::Refused)?;
    }
    Ok(bytes)
}

impl AgentRuntime {
    pub fn native_send_preview_v2(
        &mut self,
        context: &super::native_send::HumanOwnerNativeContextV1,
        request: &PrepareRequest,
        purpose_expires_ms: u64,
        commitment: [u8; 32],
    ) -> Result<super::native_send::NativeSendPreviewV2, AgentBoundaryError> {
        let grant = context
            .signed_grant()
            .map_err(|_| AgentBoundaryError::Refused)?;
        let session = &context.session;
        let mut writer = Writer::new(66);
        writer.u16(1);
        writer.text(&session.tenant)?;
        writer.fixed(&session.session_id);
        writer.fixed(session.credential());
        writer.u64(session.generation);
        writer.u64(u64::from_be_bytes(
            context.capability_id[..8]
                .try_into()
                .map_err(|_| AgentBoundaryError::Refused)?,
        ));
        writer.u32(request.protocol_activity_type);
        writer.text(request.actor.as_str())?;
        writer.text(request.authority.as_str())?;
        writer.u64(request.account_sequence.0);
        writer.u64(request.timestamp_bound.not_before.0);
        writer.u64(request.timestamp_bound.not_after.0);
        writer.text(request.idempotency_key.as_str())?;
        writer.u128(request.fee_limit.0);
        writer.bytes(request.payload.as_bytes())?;
        writer.fixed(&request.payload_hash);
        writer.fixed(&context.capability_id);
        writer.fixed(&session.owner_public_key);
        writer.u64(purpose_expires_ms);
        writer.fixed(&commitment);
        writer.fixed(&session.session_id);
        writer.bytes(&grant.capability)?;
        writer.bytes(&grant.session_scope)?;
        writer.u64(grant.expires_at_ms);
        writer.fixed(&grant.owner_public_key);
        writer.fixed(&grant.signature);
        let mut reader = self.exchange_secret(&writer.finish_secret())?;
        if reader.u16()? != 1 {
            return Err(AgentBoundaryError::CorruptResponse);
        }
        let canonical_bytes = reader.bytes()?;
        let signing_preimage = reader.bytes()?;
        let purpose =
            layerx_agent_api::identity::NativeSendPurposeV1::from_canonical_bytes(&reader.bytes()?)
                .map_err(|_| AgentBoundaryError::CorruptResponse)?;
        let value = super::native_send::NativeSendPreviewV2 {
            canonical_bytes,
            signing_preimage,
            purpose,
            head_sequence: reader.u64()?,
            protocol_time_ms: reader.u64()?,
            owner_public_key: reader.fixed()?,
            revocation_sequence: reader.u64()?,
        };
        reader.finish()?;
        let canonical_digest: [u8; 32] = sha2::Sha256::digest(&value.canonical_bytes).into();
        let activity =
            layerx_wire::activity::decode_unsigned(&value.canonical_bytes, &self.registry)
                .map_err(|_| AgentBoundaryError::CorruptResponse)?;
        if value.purpose.owner_did.as_str() != session.owner
            || value.purpose.owner_public_key != session.owner_public_key
            || value.purpose.economic_action != session.session_id
            || value.purpose.idempotency_key != decode_hex32(request.idempotency_key.as_str())?
            || value.purpose.activity.module != 1
            || value.purpose.activity.ordinal != 5
            || value.purpose.protocol_version != activity.protocol_version()
            || value.purpose.network_id != activity.network_id()
            || value.purpose.tenant.as_str() != session.tenant
            || value.purpose.agent_did.as_str() != session.owner
            || value
                .purpose
                .session_id
                .to_bytes()
                .map_err(|_| AgentBoundaryError::CorruptResponse)?
                != session.session_id
            || value
                .purpose
                .capability_id
                .to_bytes()
                .map_err(|_| AgentBoundaryError::CorruptResponse)?
                != context.capability_id
            || value.purpose.generation != session.generation
            || value.purpose.expires_at_ms != purpose_expires_ms
            || value.purpose.commitment != commitment
            || value.purpose.canonical_digest != canonical_digest
            || value.owner_public_key != session.owner_public_key
            || value.head_sequence < session.grant_sequence
            || value.revocation_sequence == 0
            || value.revocation_sequence > value.head_sequence
            || value.protocol_time_ms >= purpose_expires_ms
            || value.signing_preimage.is_empty()
        {
            return Err(AgentBoundaryError::CorruptResponse);
        }
        Ok(value)
    }
}
