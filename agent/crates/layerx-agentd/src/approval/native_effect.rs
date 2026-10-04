use serde::{Deserialize, Serialize};
use sha2::{Digest as _, Sha256};
use layerx_crypto::disclosure::{BudgetStateContext, Disclosure, DisclosedNativeOperation};
use layerx_types::payload::{ActivityType, ModuleId, ModuleRegistration, ModuleRegistry};
use crate::budget::ProgramBudgetReservation;
use crate::human::{HumanPeer, HumanSubject};
use crate::policy::native_program::{NativeEffectIntent, NativeEffectPolicy};
use crate::policy::native_program::NativeLocalOutcome;
use crate::prepare::{Prepared, PreparationAuthorization};
use crate::store::{ObjectKind, StorageClass, Store, TenantId, TenantKey};

const PREFIX: &[u8] = b"native-effect-approval-carrier-v1:";
const MAX_CARRIER: usize = 4 * 1024 * 1024;
const MAX_RECORDS: usize = 4096;
pub(crate) const DURABLE_EXTENSION: u16 = 9;

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub(crate) enum NativeEffectError { Missing, Binding, Policy, Budget, Corrupt }
#[derive(Clone, Copy, Debug, Serialize, Deserialize, Eq, PartialEq)]
pub(crate) enum NativeEffectApprovalState { Awaiting, Granted, Rejected, Expired, NotRequired }

pub(crate) struct VerifiedUnsignedNativeEffectRelease { reservation_id:[u8;32], reservation_digest:[u8;32] }
impl VerifiedUnsignedNativeEffectRelease {
    pub(crate) const fn reservation_id(&self)->[u8;32]{self.reservation_id}
    pub(crate) const fn reservation_digest(&self)->[u8;32]{self.reservation_digest}
}

#[derive(Clone, Serialize, Deserialize, Eq, PartialEq)]
#[serde(deny_unknown_fields)]
pub(crate) struct NativeEffectApprovalCarrier {
    version: u8,
    tenant: String,
    principal: String,
    actor: Vec<u8>,
    session: [u8; 32],
    generation: u64,
    preparation: [u8; 32],
    activity_module: u16,
    activity_ordinal: u16,
    canonical_bytes: Vec<u8>,
    authority: RetainedAuthority,
    disclosure_digest: [u8; 32],
    fee_asset: [u8;32],
    fee_state_root: [u8;32],
    fee_observed_sequence: u64,
    budget_context: Option<RetainedBudgetContext>,
    capability: [u8; 32],
    purpose_hash: [u8; 32],
    created_at_sequence: u64,
    created_at_unix_seconds: u64,
    clock_generation: [u8; 16],
    budget_expiry_sequence: u64,
    envelope_not_after: u64,
    budget: Vec<u8>,
    policy_source: Vec<u8>,
    requires_approval: bool,
    state: NativeEffectApprovalState,
    terminal: Option<NativeEffectTerminal>,
}

#[derive(Clone, Serialize, Deserialize, Eq, PartialEq)]
#[serde(deny_unknown_fields)]
struct NativeEffectTerminal {
    key: [u8; 32], principal: String, session: [u8; 32], generation: u64,
    sequence: u64, core_ms: u64, grant: bool, release_ref: Option<[u8; 32]>, submission_ref: Option<[u8; 32]>,
}

#[derive(Clone, Serialize, Deserialize, Eq, PartialEq)]
#[serde(deny_unknown_fields)]
struct RetainedBudgetContext {
    budget_id: [u8;32], owner: [u8;32], budget_account: [u8;32], asset: [u8;32],
    source_account: [u8;32], native_source: bool, revocation_sequence: u64,
    balance: u128, state_digest: [u8;32], observed_head_sequence: u64, purpose_hash: [u8;32],
}
impl RetainedBudgetContext {
    fn from_actual(c: BudgetStateContext) -> Self { Self { budget_id:c.budget_id,owner:c.owner,
        budget_account:c.budget_account,asset:c.asset,source_account:c.source_account,native_source:c.native_source,
        revocation_sequence:c.revocation_sequence,balance:c.balance,state_digest:c.state_digest,
        observed_head_sequence:c.observed_head_sequence,purpose_hash:c.purpose_hash } }
    fn actual(&self) -> BudgetStateContext { BudgetStateContext {budget_id:self.budget_id,owner:self.owner,
        budget_account:self.budget_account,asset:self.asset,source_account:self.source_account,native_source:self.native_source,
        revocation_sequence:self.revocation_sequence,balance:self.balance,state_digest:self.state_digest,
        observed_head_sequence:self.observed_head_sequence,purpose_hash:self.purpose_hash} }
}

#[derive(Clone, Serialize, Deserialize, Eq, PartialEq)]
#[serde(tag="kind",content="bytes")]
enum RetainedAuthority { Owner(Vec<u8>), SessionKey(Vec<u8>), DelegatedCapability(Vec<u8>), BudgetAllowance(Vec<u8>), Escrow(Vec<u8>), ProtocolModule(Vec<u8>) }
impl RetainedAuthority {
    fn from_actual(a: &layerx_types::activity::Authority) -> Self {
        use layerx_types::activity::Authority;
        let bytes=a.as_bytes().to_vec();match a {Authority::Owner(_)=>Self::Owner(bytes),Authority::SessionKey(_)=>Self::SessionKey(bytes),
            Authority::DelegatedCapability(_)=>Self::DelegatedCapability(bytes),Authority::BudgetAllowance(_)=>Self::BudgetAllowance(bytes),
            Authority::Escrow(_)=>Self::Escrow(bytes),Authority::ProtocolModule(_)=>Self::ProtocolModule(bytes)}
    }
    fn actual(&self) -> Result<layerx_types::activity::Authority,NativeEffectError> {
        use layerx_types::activity::Authority;
        match self {Self::Owner(b)=>Authority::owner(b),Self::SessionKey(b)=>Authority::session_key(b),
            Self::DelegatedCapability(b)=>Authority::delegated_capability(b),Self::BudgetAllowance(b)=>Authority::budget_allowance(b),
            Self::Escrow(b)=>Authority::escrow(b),Self::ProtocolModule(b)=>Authority::protocol_module(b)}.map_err(|_|NativeEffectError::Corrupt)
    }
}

impl NativeEffectApprovalCarrier {
    pub(crate) fn bind(
        peer: &HumanPeer, prepared: &Prepared, origin: &PreparationAuthorization,
        capability: crate::capability::CapabilityId, purpose_hash: [u8;32],
        clock: &dyn layerx_types::clock::Clock, policy: &NativeEffectPolicy,
        reservation: &ProgramBudgetReservation,
        fee: &layerx_client::payments::CommittedSnapshot<layerx_client::payments::NativeFeePolicy>,
    ) -> Result<Self,NativeEffectError> {
        crate::prepare::verify_disclosure_binding(prepared).map_err(|_|NativeEffectError::Binding)?;
        let tenant=human_tenant(peer)?;
        let id: [u8;32]=Sha256::digest(&prepared.canonical_bytes).into();
        if origin.session.tenant!=tenant || origin.generation==0 || origin.session.session_id.0==[0;32]
            || capability.0==[0;32] || purpose_hash==[0;32] || prepared.envelope.protocol_version()!=3
            || prepared.envelope.activity_type().module()==ModuleId::Programs || reservation.id!=id
            || reservation.expiry_sequence<=prepared.observed_head_sequence
            || reservation.core_deadline.is_none_or(|deadline|deadline.0!=prepared.envelope.timestamp_bound().not_after()) {
            return Err(NativeEffectError::Binding)
        }
        let intent=NativeEffectIntent::from_prepared(prepared,fee,purpose_hash).map_err(|_|NativeEffectError::Policy)?;
        let decision=policy.evaluate_local(&intent).map_err(|_|NativeEffectError::Policy)?;
        let reading=clock.sample(std::time::Duration::from_secs(1)).map_err(|_|NativeEffectError::Binding)?;
        if reading.generation==[0;16] || reading.unix_milliseconds>=prepared.envelope.timestamp_bound().not_after() {
            return Err(NativeEffectError::Binding)
        }
        let context=match &prepared.disclosure.native_operation {
            Some(DisclosedNativeOperation::BudgetFund(v))=>Some(v.context),
            Some(DisclosedNativeOperation::BudgetDefund(v))=>Some(v.context),
            Some(DisclosedNativeOperation::BudgetRevoke(v))=>Some(v.context),_=>None,
        };
        let required=decision.outcome==NativeLocalOutcome::ApprovalRequired;
        let result=Self {version:1,tenant:peer.tenant.clone(),principal:peer.principal.clone(),
            actor:prepared.envelope.actor_did().as_bytes().to_vec(),session:origin.session.session_id.0,generation:origin.generation,
            preparation:id,activity_module:prepared.envelope.activity_type().module() as u16,
            activity_ordinal:prepared.envelope.activity_type().ordinal(),canonical_bytes:prepared.canonical_bytes.clone(),
            authority:RetainedAuthority::from_actual(prepared.envelope.authority()),disclosure_digest:prepared.disclosure_digest.0,
            fee_asset:fee.value.asset.asset_id,fee_state_root:fee.state_root,fee_observed_sequence:fee.observed_sequence,
            budget_context:context.map(RetainedBudgetContext::from_actual),capability:capability.0,purpose_hash,
            created_at_sequence:prepared.observed_head_sequence,created_at_unix_seconds:reading.unix_seconds(),clock_generation:reading.generation,
            budget_expiry_sequence:reservation.expiry_sequence,envelope_not_after:prepared.envelope.timestamp_bound().not_after(),
            budget:reservation.encode().map_err(|_|NativeEffectError::Budget)?,policy_source:policy.source().to_vec(),requires_approval:required,
            state:if required {NativeEffectApprovalState::Awaiting}else{NativeEffectApprovalState::NotRequired},terminal:None};
        result.validate()?;Ok(result)
    }

    fn registry(&self) -> Result<ModuleRegistry,NativeEffectError> {
        let module=ModuleId::from_u16(self.activity_module).map_err(|_|NativeEffectError::Corrupt)?;
        if module==ModuleId::Programs{return Err(NativeEffectError::Binding)}
        let activity=ActivityType::new(module,self.activity_ordinal).map_err(|_|NativeEffectError::Corrupt)?;
        ModuleRegistry::new(&[ModuleRegistration::new(module,&[activity]).map_err(|_|NativeEffectError::Corrupt)?])
            .map_err(|_|NativeEffectError::Corrupt)
    }
    pub(crate) fn disclosure(&self) -> Result<Disclosure,NativeEffectError> {
        let registry=self.registry()?;
        let disclosure=match &self.budget_context {
            Some(c)=>layerx_crypto::disclosure::bind_budget_mutation(&self.canonical_bytes,&registry,&c.actual()),
            None=>layerx_crypto::disclosure::bind(&self.canonical_bytes,&registry),
        }.map_err(|_|NativeEffectError::Corrupt)?;
        if disclosure.reencode().map_err(|_|NativeEffectError::Corrupt)?!=self.canonical_bytes
            || disclosure.audit_digest().map_err(|_|NativeEffectError::Corrupt)?!=self.disclosure_digest
            || disclosure.actor!=self.actor {return Err(NativeEffectError::Binding)};Ok(disclosure)
    }
    fn validate(&self)->Result<(),NativeEffectError>{
        let tenant=TenantId::new(self.tenant.clone()).map_err(|_|NativeEffectError::Corrupt)?;
        let _=tenant;
        if self.version!=1 || self.principal.is_empty() || self.principal.len()>255 || self.session==[0;32]
            || self.generation==0 || self.fee_asset==[0;32] || self.fee_state_root==[0;32] || self.fee_observed_sequence!=self.created_at_sequence || self.capability==[0;32] || self.purpose_hash==[0;32] || self.clock_generation==[0;16]
            || self.canonical_bytes.is_empty() || self.canonical_bytes.len()>MAX_CARRIER
            || self.preparation!=<[u8;32]>::from(Sha256::digest(&self.canonical_bytes))
            || self.created_at_sequence>=self.budget_expiry_sequence || self.created_at_unix_seconds==0
            || self.created_at_unix_seconds.checked_mul(1000).is_none_or(|time|time>=self.envelope_not_after) {
            return Err(NativeEffectError::Corrupt)
        }
        let disclosure=self.disclosure()?;
        let decoded=layerx_wire::activity::decode_unsigned(&self.canonical_bytes,&self.registry()?).map_err(|_|NativeEffectError::Corrupt)?;
        if decoded.protocol_version()!=3 || self.authority.actual()?.as_bytes()!=decoded.authority()
            || disclosure.expiry.not_after!=self.envelope_not_after {return Err(NativeEffectError::Binding)}
        let budget=self.budget()?;
        if budget.id!=self.preparation || budget.expiry_sequence!=self.budget_expiry_sequence
            || budget.core_deadline.is_none_or(|time|time.0!=self.envelope_not_after)
            || budget.encode().map_err(|_|NativeEffectError::Budget)?!=self.budget {return Err(NativeEffectError::Binding)}
        NativeEffectPolicy::load(&self.policy_source).map_err(|_|NativeEffectError::Policy)?;
        match (self.requires_approval,self.state,&self.terminal){
            (true,NativeEffectApprovalState::Awaiting,None)|(false,NativeEffectApprovalState::NotRequired,None)
            |(_,NativeEffectApprovalState::Expired,None)=>{},
            (_,state,Some(t)) if matches!(state,NativeEffectApprovalState::Granted|NativeEffectApprovalState::Rejected|NativeEffectApprovalState::Expired)
                && t.key!=[0;32] && !t.principal.is_empty() && t.session==self.session && t.generation==self.generation
                && t.sequence>=self.created_at_sequence
                && (t.release_ref.is_some()==(state==NativeEffectApprovalState::Granted))
                && (t.submission_ref.is_none() || state==NativeEffectApprovalState::Granted)
                && (state==NativeEffectApprovalState::Expired || t.grant==(state==NativeEffectApprovalState::Granted))=>{},
            _=>return Err(NativeEffectError::Corrupt)
        };Ok(())
    }
    pub(crate) fn encoded(&self)->Result<Vec<u8>,NativeEffectError>{self.validate()?;let bytes=serde_json::to_vec(self).map_err(|_|NativeEffectError::Corrupt)?;
        if bytes.len()>MAX_CARRIER{return Err(NativeEffectError::Corrupt)};Ok(bytes)}
    pub(crate) fn companion(&self)->Result<(TenantKey,Vec<u8>),NativeEffectError>{
        let key=TenantKey::new(TenantId::new(self.tenant.clone()).map_err(|_|NativeEffectError::Corrupt)?,ObjectKind::PreparedActivity,
            [PREFIX,self.preparation.as_slice()].concat()).map_err(|_|NativeEffectError::Corrupt)?;Ok((key,self.encoded()?))}
    pub(crate) fn held_digest(&self)->Result<[u8;32],NativeEffectError>{let mut held=self.clone();held.terminal=None;
        held.state=if held.requires_approval{NativeEffectApprovalState::Awaiting}else{NativeEffectApprovalState::NotRequired};
        Ok(Sha256::digest(held.encoded()?).into())}
    pub(crate) fn budget(&self)->Result<ProgramBudgetReservation,NativeEffectError>{ProgramBudgetReservation::decode(&self.budget).map_err(|_|NativeEffectError::Budget)}
    pub(crate) fn preparation_id(&self)->[u8;32]{self.preparation}
    pub(crate) fn actor(&self)->&[u8]{&self.actor}
    pub(crate) fn fee_asset(&self)->[u8;32]{self.fee_asset}
    pub(crate) fn principal(&self)->&str{&self.principal}
    pub(crate) fn session_id(&self)->[u8;32]{self.session}
    pub(crate) fn generation(&self)->u64{self.generation}
    pub(crate) fn capability(&self)->[u8;32]{self.capability}
    pub(crate) fn purpose_hash(&self)->[u8;32]{self.purpose_hash}
    pub(crate) fn state(&self)->NativeEffectApprovalState{self.state}
    pub(crate) fn requires_approval(&self)->bool{self.requires_approval}
    pub(crate) fn created_at_sequence(&self)->u64{self.created_at_sequence}
    pub(crate) fn budget_expiry_sequence(&self)->u64{self.budget_expiry_sequence}
    pub(crate) fn created_at_unix_seconds(&self)->u64{self.created_at_unix_seconds}
    pub(crate) fn activity_expires_at_unix_milliseconds(&self)->u64{self.envelope_not_after}
    pub(crate) fn submission_ref(&self)->Option<[u8;32]>{self.terminal.as_ref().and_then(|t|t.submission_ref)}
    pub(crate) fn release_ref(&self)->Option<[u8;32]>{self.terminal.as_ref().and_then(|t|t.release_ref)}
    pub(crate) fn canonical_bytes(&self)->&[u8]{&self.canonical_bytes}

    pub(crate) fn read(store:&Store,tenant:&TenantId,id:[u8;32])->Result<Self,NativeEffectError>{
        let key=TenantKey::new(tenant.clone(),ObjectKind::PreparedActivity,[PREFIX,id.as_slice()].concat()).map_err(|_|NativeEffectError::Corrupt)?;
        let value=store.get(&key).ok_or(NativeEffectError::Missing)?;
        if value.class()!=StorageClass::LocalOnly || value.bytes().len()>MAX_CARRIER{return Err(NativeEffectError::Corrupt)}
        let held:Self=serde_json::from_slice(value.bytes()).map_err(|_|NativeEffectError::Corrupt)?;
        if held.encoded()?.as_slice()!=value.bytes() || held.preparation!=id || held.tenant!=tenant.as_str(){return Err(NativeEffectError::Binding)}
        let key=crate::prepare::DurablePreparation::store_key(tenant,id).map_err(|_|NativeEffectError::Corrupt)?;
        let value=store.get(&key).ok_or(NativeEffectError::Missing)?;
        if value.class()!=StorageClass::LocalOnly{return Err(NativeEffectError::Corrupt)}
        let durable=crate::prepare::DurablePreparation::decode(tenant.clone(),value.bytes()).map_err(|_|NativeEffectError::Corrupt)?;
        if durable.preparation_id!=id || durable.session_id!=held.session || durable.generation!=held.generation
            || durable.not_after!=held.envelope_not_after || durable.extensions.get(&6)!=Some(&held.budget)
            || durable.extensions.get(&DURABLE_EXTENSION).map(Vec::as_slice)!=Some(held.held_digest()?.as_slice()) {
            return Err(NativeEffectError::Binding)
        }
        let activity=layerx_wire::activity::decode_unsigned(&held.canonical_bytes,&held.registry()?).map_err(|_|NativeEffectError::Corrupt)?;
        if durable.payload_hash!=activity.payload_hash() || durable.tenant!=*tenant {return Err(NativeEffectError::Binding)}
        let budget=held.budget()?;
        if budget.allocations().is_none() && (durable.holds.len()!=budget.holds.len()
            || budget.holds.iter().any(|h|!durable.holds.iter().any(|(saved,_)|saved==&h.reservation))) {
            return Err(NativeEffectError::Binding)
        };Ok(held)
    }
    pub(crate) fn read_for_human(store:&Store,peer:&HumanPeer,id:[u8;32])->Result<Self,NativeEffectError>{
        let tenant=human_tenant(peer)?;let held=Self::read(store,&tenant,id)?;
        if held.principal!=peer.principal || !held.owner_matches(store,&tenant,peer.subject.as_ref().ok_or(NativeEffectError::Binding)?)?{
            return Err(NativeEffectError::Binding)
        };Ok(held)
    }
    pub(crate) fn list_for_human(store:&Store,peer:&HumanPeer)->Result<Vec<Self>,NativeEffectError>{
        let tenant=human_tenant(peer)?;let subject=peer.subject.as_ref().ok_or(NativeEffectError::Binding)?;
        let ids=store.list_object_ids(&tenant,ObjectKind::PreparedActivity).into_iter().filter(|id|id.starts_with(PREFIX)).collect::<Vec<_>>();
        if ids.len()>MAX_RECORDS{return Err(NativeEffectError::Corrupt)}
        let mut result=Vec::new();for id in ids {let id=id[PREFIX.len()..].try_into().map_err(|_|NativeEffectError::Corrupt)?;
            let held=Self::read(store,&tenant,id)?;if held.principal==peer.principal && held.owner_matches(store,&tenant,subject)?{result.push(held)}}
        result.sort_by_key(Self::preparation_id);Ok(result)
    }
    fn owner_matches(&self,store:&Store,tenant:&TenantId,subject:&HumanSubject)->Result<bool,NativeEffectError>{
        if self.actor==subject.owner.as_bytes(){return Ok(true)}
        crate::managed_agent::authenticates_native_owner(store,tenant,&subject.owner,&subject.account,&self.actor).map_err(|_|NativeEffectError::Binding)
    }
    pub(crate) fn stage_decision(&self,peer:&HumanPeer,held_digest:[u8;32],key:[u8;32],grant:bool,
        observation:&crate::protocol_evidence::AuthenticatedCoreTime,session:&crate::session::SessionRecord)->Result<Self,NativeEffectError>{
        let tenant=human_tenant(peer)?;let sequence=observation.through_sequence();let core_ms=observation.observed_core_ms();
        if tenant.as_str()!=self.tenant || peer.principal!=self.principal || self.held_digest()?!=held_digest || key==[0;32]
            || !session.open || session.request.tenant!=tenant || session.request.session_id.0!=self.session
            || session.request.agent.as_bytes()!=self.actor || session.generation!=self.generation
            || sequence>=session.request.expiry_sequence
            || session.request.expiry_seconds.is_some_and(|expiry|core_ms/1000>=expiry)
            || sequence<self.created_at_sequence {return Err(NativeEffectError::Binding)}
        if let Some(t)=&self.terminal {if t.key==key && t.principal==peer.principal && t.grant==grant{return Ok(self.clone())}
            return Err(NativeEffectError::Binding)}
        if self.state!=NativeEffectApprovalState::Awaiting || !self.requires_approval{return Err(NativeEffectError::Binding)}
        let expired=sequence>=self.budget_expiry_sequence || core_ms>=self.envelope_not_after;
        let mut result=self.clone();result.state=if expired{NativeEffectApprovalState::Expired}else if grant{NativeEffectApprovalState::Granted}else{NativeEffectApprovalState::Rejected};
        let granted=result.state==NativeEffectApprovalState::Granted;
        let release_ref=if granted{let mut h=Sha256::new();h.update(b"layerx/native-effect-release/v1\0");h.update(held_digest);h.update(self.preparation);Some(h.finalize().into())}else{None};
        result.terminal=Some(NativeEffectTerminal{key,principal:peer.principal.clone(),session:self.session,generation:self.generation,
            sequence,core_ms,grant,release_ref,submission_ref:None});result.validate()?;Ok(result)
    }
    pub(crate) fn stage_expiry(&self,observation:&crate::protocol_evidence::AuthenticatedCoreTime)->Result<Self,NativeEffectError>{
        if self.terminal.is_some() || !matches!(self.state,NativeEffectApprovalState::Awaiting|NativeEffectApprovalState::NotRequired)
            || observation.through_sequence()<self.created_at_sequence
            || (observation.through_sequence()<self.budget_expiry_sequence && observation.observed_core_ms()<self.envelope_not_after){
            return Err(NativeEffectError::Binding)
        }
        let mut expired=self.clone();expired.state=NativeEffectApprovalState::Expired;expired.validate()?;Ok(expired)
    }
    pub(crate) fn unsigned_release_proof(&self,store:&Store,durable:&crate::prepare::DurablePreparation)->Result<VerifiedUnsignedNativeEffectRelease,NativeEffectError>{
        let tenant=TenantId::new(self.tenant.clone()).map_err(|_|NativeEffectError::Binding)?;
        let retained=Self::read(store,&tenant,self.preparation)?;
        let key=crate::prepare::DurablePreparation::store_key(&tenant,self.preparation).map_err(|_|NativeEffectError::Binding)?;
        let raw=store.get(&key).ok_or(NativeEffectError::Missing)?;
        let actual=crate::prepare::DurablePreparation::decode(tenant.clone(),raw.bytes()).map_err(|_|NativeEffectError::Corrupt)?;
        if actual!=*durable || raw.class()!=StorageClass::LocalOnly || retained.held_digest()?!=self.held_digest()?
            || !matches!(self.state,NativeEffectApprovalState::Rejected|NativeEffectApprovalState::Expired)
            || durable.state!=crate::prepare::LifecycleState::Prepared || durable.activity_id.is_some()
            || durable.signed_bytes().map_err(|_|NativeEffectError::Corrupt)?.is_some()
            || durable.session_id!=self.session || durable.generation!=self.generation
            || durable.extensions.get(&6)!=Some(&self.budget) || durable.extensions.get(&DURABLE_EXTENSION).map(Vec::as_slice)!=Some(self.held_digest()?.as_slice()) {
            return Err(NativeEffectError::Binding)
        }
        let record=self.budget()?;
        Ok(VerifiedUnsignedNativeEffectRelease{reservation_id:self.preparation,reservation_digest:record.settlement_binding().map_err(|_|NativeEffectError::Budget)?})
    }
    pub(crate) fn authorize_human_submit(&self,peer:&HumanPeer,prepared:&Prepared,origin:&PreparationAuthorization,
        submission_ref:Option<[u8;32]>,sequence:u64,core_ms:u64)->Result<(),NativeEffectError>{
        if peer.tenant!=self.tenant || peer.principal!=self.principal || origin.session.tenant.as_str()!=self.tenant
            || origin.session.session_id.0!=self.session || origin.generation!=self.generation
            || prepared.canonical_bytes!=self.canonical_bytes || prepared.disclosure_digest.0!=self.disclosure_digest
            || sequence<self.created_at_sequence || sequence>=self.budget_expiry_sequence || core_ms>=self.envelope_not_after
            || !matches!(self.state,NativeEffectApprovalState::Granted|NativeEffectApprovalState::NotRequired)
            || (self.requires_approval && (submission_ref.is_none() || submission_ref!=self.release_ref()))
            || (!self.requires_approval && submission_ref.is_some()) {return Err(NativeEffectError::Binding)};Ok(())
    }
    pub(crate) fn read_id(store:&Store,context:&crate::agent_rpc_peer::RpcOwnerContext<'_>,id:[u8;32])->Result<Self,NativeEffectError>{
        let held=Self::read(store,&context.principal().tenant,id)?;
        if held.actor!=context.principal().agent.as_bytes() || held.principal!=context.peer().principal {return Err(NativeEffectError::Binding)};Ok(held)
    }
    pub(crate) fn list(store:&Store,context:&crate::agent_rpc_peer::RpcOwnerContext<'_>)->Result<Vec<Self>,NativeEffectError>{
        let ids=store.list_object_ids(&context.principal().tenant,ObjectKind::PreparedActivity).into_iter()
            .filter(|id|id.starts_with(PREFIX)).collect::<Vec<_>>();
        if ids.len()>MAX_RECORDS{return Err(NativeEffectError::Corrupt)}
        let mut result=Vec::new();for id in ids{let id=id[PREFIX.len()..].try_into().map_err(|_|NativeEffectError::Corrupt)?;
            let held=Self::read(store,&context.principal().tenant,id)?;
            if held.actor==context.principal().agent.as_bytes() && held.principal==context.peer().principal{result.push(held)}}
        result.sort_by_key(Self::preparation_id);Ok(result)
    }
    pub(crate) fn authorize_submit(&self,context:&crate::agent_rpc_peer::RpcOwnerContext<'_>,prepared:&Prepared,
        release_ref:Option<[u8;32]>,sequence:u64,core_ms:u64)->Result<(),NativeEffectError>{
        let origin=context.permit().preparation_authorization();
        if self.actor!=context.principal().agent.as_bytes(){return Err(NativeEffectError::Binding)}
        self.authorize_human_submit(context.peer(),prepared,&origin,release_ref,sequence,core_ms)
    }
    pub(crate) fn response(&self)->Result<layerx_agent_api::identity::NativeApprovalResultV1,NativeEffectError>{
        Ok(layerx_agent_api::identity::NativeApprovalResultV1{approval_id:self.preparation,held_digest:self.held_digest()?,
            activity:layerx_agent_api::identity::NativeActivity{module:self.activity_module,ordinal:self.activity_ordinal},
            state:format!("{:?}",self.state),submission_ref:self.release_ref()})
    }
    pub(crate) fn restore_prepared(&self,registry:&ModuleRegistry)->Result<Prepared,NativeEffectError>{
        use layerx_types::activity::{EnvelopeBuilder,TimestampBound};
        use layerx_types::ids::{Did,IdempotencyKey};use layerx_types::amount::Amount;use layerx_types::payload::Payload;
        let a=layerx_wire::activity::decode_unsigned(&self.canonical_bytes,registry).map_err(|_|NativeEffectError::Corrupt)?;
        let payload=Payload::new(registry,a.activity_type(),a.payload()).map_err(|_|NativeEffectError::Corrupt)?;
        let hash=layerx_wire::hash::payload_hash_for(&payload).map_err(|_|NativeEffectError::Corrupt)?;
        if hash!=a.payload_hash(){return Err(NativeEffectError::Binding)}
        let actor=Did::new(a.actor_did()).map_err(|_|NativeEffectError::Corrupt)?;
        let authority=self.authority.actual()?;
        let bound=TimestampBound::new(a.timestamp_bound().not_before,a.timestamp_bound().not_after).map_err(|_|NativeEffectError::Corrupt)?;
        let mut b=EnvelopeBuilder::new();b.protocol_version(a.protocol_version()).and_then(|b|b.network_id(a.network_id()))
            .and_then(|b|b.activity_type(a.activity_type())).and_then(|b|b.actor_did(actor))
            .and_then(|b|b.authority(authority)).and_then(|b|b.account_sequence(a.account_sequence()))
            .and_then(|b|b.timestamp_bound(bound)).and_then(|b|b.idempotency_key(IdempotencyKey::new(a.idempotency_key())))
            .and_then(|b|b.fee_limit(Amount::from_u128(a.fee_limit()))).and_then(|b|b.payload_hash(hash))
            .and_then(|b|b.payload(payload)).map_err(|_|NativeEffectError::Corrupt)?;
        let envelope=b.build().map_err(|_|NativeEffectError::Corrupt)?;
        if layerx_wire::activity::encode_unsigned_envelope(&envelope).map_err(|_|NativeEffectError::Corrupt)?!=self.canonical_bytes{return Err(NativeEffectError::Binding)}
        let signing_preimage=*layerx_wire::sign::preimage_unsigned(&envelope).map_err(|_|NativeEffectError::Corrupt)?.as_bytes();
        let disclosure=self.disclosure()?;let disclosure_digest=crate::prepare::DisclosureDigest(self.disclosure_digest);
        Ok(Prepared{envelope,canonical_bytes:self.canonical_bytes.clone(),signing_preimage,observed_head_sequence:self.created_at_sequence,
            disclosure,disclosure_digest,audit:crate::prepare::PreparationAuditEntry{idempotency_key:a.idempotency_key(),observed_head_sequence:self.created_at_sequence,disclosure_digest}})
    }
}
fn human_tenant(peer:&HumanPeer)->Result<TenantId,NativeEffectError>{
    let s=peer.subject.as_ref().ok_or(NativeEffectError::Binding)?;
    let expected=layerx_identity_binding::subject_namespace(&s.transport_tenant,&peer.principal).map_err(|_|NativeEffectError::Binding)?;
    let account=layerx_types::account::AccountId::parse(&s.account).map_err(|_|NativeEffectError::Binding)?;
    layerx_types::ids::Did::new(s.owner.as_bytes()).map_err(|_|NativeEffectError::Binding)?;
    if peer.uid==0 || s.transport_principal.is_empty() || expected!=peer.tenant || account.canonical()!=s.account
        || !s.account.starts_with(&format!("agent:{}:",s.owner)){return Err(NativeEffectError::Binding)}
    TenantId::new(peer.tenant.clone()).map_err(|_|NativeEffectError::Binding)
}
