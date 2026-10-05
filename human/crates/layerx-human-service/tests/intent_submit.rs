use layerx_human_test_support as support;

use std::collections::BTreeMap;
use std::fs;
use std::future::Future;
use std::pin::pin;
use std::sync::Arc;
use std::task::{Context, Poll, Wake, Waker};

use ed25519_dalek::{Signer as _, SigningKey};
use k256::ecdsa::SigningKey as EvmSigningKey;
use layerx_agent_api::idempotency::IdempotentMutation;
use layerx_agent_api::identity::{AgentDid, AuthorityRef};
use layerx_agent_api::prepare::{PreparationRef, PrepareRequest as ApiPrepareRequest};
use layerx_agent_api::submit::SubmitRequest;
use layerx_agent_api::track::{
    EvidenceRef as AgentEvidenceRef, ReceiptRef, SubmissionRef, SubmissionState, TrackRequest,
    TrackedSubmission,
};
use layerx_agent_api::verify::Level;
use layerx_agentd::outbox::{Outbox, SubmissionState as OutboxState};
use layerx_agentd::prepare::{
    prepare_activity, CorePreparationBoundary, CorePreparationState, CoreStateError,
    PreparationDefaults, PrepareRequest, Prepared,
};
use layerx_agentd::receipt::{self as daemon_receipt, ReceiptLookupKey as DaemonReceiptKey};
use layerx_agentd::sign::{attach_external_signature, verify_before_submit};
use layerx_agentd::store::{Store as AgentStore, TenantId};
use layerx_human_service::binding::{
    AgentBindingContract, AgentBindingError, AgentBindingReceipt, AgentSubmission,
    BindingAgentRequest, BindingJourney,
};
use layerx_human_service::custody::{
    CustodySigner, EnvelopeKms, KeyClass, KeyEntropy, KeyId, Keystore, SigningLimits,
};
use layerx_human_service::journeys::{
    authority_label, claim_receipt, drive_intent_journey, intent_journey_id, plan,
    start_deposit_forward_journey, start_deposit_journey, start_kernel_journey,
    start_withdrawal_intent, verify_bindings, AgentBoundary, AgentBoundaryError, AgentObservation,
    AgentPreparation, AllowanceId, AllowanceKind, AllowanceScope, BalanceEntry, BindingExpectation,
    Constraints, CustodyContext, DepositAgentPlan, DepositForwardJourney, DepositForwardStage,
    DepositPlan, Domain, Endpoint, ExecutableShape, FeeSchedule, IntentDriver, IntentLegBinding,
    IntentSubmission, IntentWithdrawalJourney, IntentWithdrawalStage, JourneyEngine, JourneyState,
    KernelStart, LegMechanism, Mechanism, ObservedState, PlannedLeg, ReceiptLookup,
    ReceiptMaterial, Refusal, Relationship, RouteRequest, SendRoute, SettlementConfig,
    SignedAllowance, SubmitPlanRequest, SubmitRefusal, UnifiedIntent, UnifiedPlan,
    WithdrawalAgentPlan, WithdrawalJourney, WithdrawalPlan,
};
use layerx_human_service::notify::JourneyId;
use layerx_human_service::store::{PrincipalId, PrincipalStore, TenancyDigest};
use layerx_human_service::trace::TraceId;
use layerx_proof::receipt::AuthorizedBatch;
use layerx_sdk::{Call, Client as AgentClient};
use layerx_types::account::AccountId;
use layerx_types::activity::{Authority, TimestampBound};
use layerx_types::amount::Amount;
use layerx_types::ids::{AssetId, Did, IdempotencyKey};
use layerx_types::intent::{
    AuthorizationSignature, ContextHash, EvmAddress, NetworkId, ProtocolVersion, PublicKey,
    SendAuthorization, SendAuthorizationKind, Sequence, TimestampSeconds,
};
use layerx_types::payload::{ActivityType, ModuleId, ModuleRegistration, ModuleRegistry};
use serde_json::{json, Value};
use sha2::{Digest as _, Sha256};
use sha3::Keccak256;

use support::{directory, principal, retention_uniform, tenancy};

const NETWORK_ID: u32 = 77;
const ASSET: [u8; 32] = [0x33; 32];
const HOME: &str = "agent:did:layerx:human:main";
const WORKER: &str = "agent:did:layerx:worker:main";
const SEQUENCE: u64 = 7;
const SEND_FEE: u128 = 2;
const CREDIT_FEE: u128 = 3;
const DEPOSIT_GAS: u128 = 4;
const MAX_FEE: u128 = 20;
const WITHDRAW_FEE: u128 = 3;
const FINALISE_GAS: u128 = 4;

struct NoopWake;

impl Wake for NoopWake {
    fn wake(self: Arc<Self>) {}
}

fn ready<F: Future>(future: F) -> F::Output {
    let mut future = pin!(future);
    let waker = Waker::from(Arc::new(NoopWake));
    let mut context = Context::from_waker(&waker);
    match future.as_mut().poll(&mut context) {
        Poll::Ready(value) => value,
        Poll::Pending => panic!("intent journey future unexpectedly blocked"),
    }
}

fn asset_send() -> ActivityType {
    ActivityType::new(ModuleId::Asset, 5)
        .unwrap_or_else(|error| panic!("asset activity: {error:?}"))
}

fn registry() -> ModuleRegistry {
    let asset = ModuleRegistration::new(ModuleId::Asset, &[asset_send()])
        .unwrap_or_else(|error| panic!("asset registration: {error:?}"));
    let governance = ActivityType::new(ModuleId::Governance, 4)
        .unwrap_or_else(|error| panic!("governance activity: {error:?}"));
    let bridge = ActivityType::new(ModuleId::Bridge, 1)
        .unwrap_or_else(|error| panic!("bridge activity: {error:?}"));
    ModuleRegistry::new(&[
        asset,
        ModuleRegistration::new(ModuleId::Governance, &[governance])
            .unwrap_or_else(|error| panic!("governance registration: {error:?}")),
        ModuleRegistration::new(ModuleId::Bridge, &[bridge])
            .unwrap_or_else(|error| panic!("bridge registration: {error:?}")),
    ])
    .unwrap_or_else(|error| panic!("module registry: {error:?}"))
}

fn account(value: &str) -> AccountId {
    AccountId::parse(value).unwrap_or_else(|error| panic!("account: {error:?}"))
}

fn actor() -> AgentDid {
    AgentDid::new("did:layerx:owner").unwrap_or_else(|error| panic!("actor: {error:?}"))
}

fn authority() -> AuthorityRef {
    AuthorityRef::new("custody-human-primary")
        .unwrap_or_else(|error| panic!("authority: {error:?}"))
}

fn hex(bytes: &[u8]) -> String {
    use std::fmt::Write as _;
    bytes.iter().fold(String::new(), |mut output, byte| {
        let _ = write!(output, "{byte:02x}");
        output
    })
}

fn evm_key() -> EvmSigningKey {
    EvmSigningKey::from_slice(&[0x5a; 32]).unwrap_or_else(|error| panic!("EVM key: {error}"))
}

fn evm_address(key: &EvmSigningKey) -> EvmAddress {
    let point = key.verifying_key().to_encoded_point(false);
    let hash = Keccak256::digest(&point.as_bytes()[1..]);
    let mut address = [0_u8; 20];
    address.copy_from_slice(&hash[12..]);
    EvmAddress::new(address)
}

fn ownership_signature(key: &EvmSigningKey, digest: [u8; 32]) -> [u8; 65] {
    let (signature, recovery) = key
        .sign_prehash_recoverable(&digest)
        .unwrap_or_else(|error| panic!("ownership signature: {error}"));
    let mut bytes = [0_u8; 65];
    bytes[..64].copy_from_slice(&signature.to_bytes());
    bytes[64] = recovery.to_byte().saturating_add(27);
    bytes
}

fn fees() -> FeeSchedule {
    FeeSchedule::new(vec![
        (LegMechanism::Protocol(Mechanism::Send), SEND_FEE),
        (
            LegMechanism::Protocol(Mechanism::BridgeDepositCredit),
            CREDIT_FEE,
        ),
        (LegMechanism::PaxeerCustodyDeposit, DEPOSIT_GAS),
        (
            LegMechanism::Protocol(Mechanism::BridgeWithdrawRequest),
            WITHDRAW_FEE,
        ),
        (LegMechanism::PaxeerWithdrawFinalise, FINALISE_GAS),
    ])
    .unwrap_or_else(|error| panic!("fees: {error:?}"))
}

fn observed(now: u64, custody: Option<EvmAddress>) -> ObservedState {
    observed_with(now, custody, Vec::new(), Vec::new())
}

fn observed_with(
    now: u64,
    custody: Option<EvmAddress>,
    extra: Vec<BalanceEntry>,
    allowances: Vec<SignedAllowance>,
) -> ObservedState {
    let home = Endpoint::human(account(HOME)).unwrap_or_else(|error| panic!("home: {error:?}"));
    let mut balances = vec![BalanceEntry::new(
        home,
        AssetId::new(ASSET),
        Amount::from_u128(500),
    )];
    balances.extend(extra);
    let context = custody.map(|wallet| {
        balances.push(BalanceEntry::new(
            Endpoint::PaxeerWallet,
            AssetId::new(ASSET),
            Amount::from_u128(500),
        ));
        CustodyContext::new(
            wallet,
            account("system:paxeer-reserve"),
            account("system:paxeer-withdrawals"),
        )
        .unwrap_or_else(|error| panic!("custody context: {error:?}"))
    });
    ObservedState::new(
        TimestampSeconds::from_u64(now),
        account(HOME),
        context,
        balances,
        allowances,
        Vec::new(),
        fees(),
    )
    .unwrap_or_else(|error| panic!("observed state: {error:?}"))
}

fn planned(
    source: Endpoint,
    destination: Endpoint,
    amount: u128,
    observed: &ObservedState,
) -> UnifiedPlan {
    let intent = UnifiedIntent::new(
        source,
        destination,
        AssetId::new(ASSET),
        Amount::from_u128(amount),
        Constraints::new(TimestampSeconds::from_u64(1_200), MAX_FEE, false),
    )
    .unwrap_or_else(|error| panic!("intent: {error:?}"));
    plan(&intent, observed).unwrap_or_else(|error| panic!("plan: {error:?}"))
}

fn kernel_plan() -> UnifiedPlan {
    planned(
        Endpoint::human(account(HOME)).unwrap_or_else(|error| panic!("home: {error:?}")),
        Endpoint::agent(account(WORKER)).unwrap_or_else(|error| panic!("worker: {error:?}")),
        90,
        &observed(1_000, None),
    )
}

fn deposit_plan_of(wallet: EvmAddress) -> UnifiedPlan {
    planned(
        Endpoint::PaxeerWallet,
        Endpoint::human(account(HOME)).unwrap_or_else(|error| panic!("home: {error:?}")),
        25,
        &observed(200, Some(wallet)),
    )
}

fn binding_document(plan: &UnifiedPlan, index: usize, sequence: u64, window: (u64, u64)) -> Value {
    let leg = &plan.legs()[index];
    json!({
        "leg_index": index,
        "action_key": hex(&plan.action_key(index).unwrap_or_else(|error| panic!("action key: {error:?}"))),
        "actor": actor().as_str(),
        "authority": authority_label(leg.authority()),
        "relationship": "self",
        "account_sequence": sequence,
        "not_before": window.0,
        "not_after": window.1,
        "fee_limit": {"amount": leg.fee().saturating_add(1).to_string(), "currency": "LXP"},
    })
}

fn submission_body(plan: &UnifiedPlan, digest: [u8; 32], bindings: Vec<Value>) -> Value {
    json!({
        "plan_digest": hex(&plan.digest()),
        "signed_digest": hex(&digest),
        "bindings": bindings,
    })
}

fn valid_request(plan: &UnifiedPlan, window: (u64, u64)) -> SubmitPlanRequest {
    let bindings = (0..plan.legs().len())
        .map(|index| binding_document(plan, index, SEQUENCE, window))
        .collect();
    SubmitPlanRequest::from_json(&submission_body(plan, plan.digest(), bindings))
        .unwrap_or_else(|error| panic!("submission: {error}"))
}

fn expectation(now: u64) -> BindingExpectation {
    BindingExpectation {
        actor: actor(),
        authority: authority(),
        account_sequence: SEQUENCE,
        currency: "LXP".to_owned(),
        now,
    }
}

#[derive(Clone, Copy)]
struct ReceiptSpec {
    activity: ActivityType,
    amount: u128,
    fee: u128,
}

struct RecordedCore(CorePreparationState);

impl CorePreparationBoundary for RecordedCore {
    fn preparation_state(&mut self, _actor: &Did) -> Result<CorePreparationState, CoreStateError> {
        Ok(self.0.clone())
    }
}

struct RealAgentLayer {
    store: AgentStore,
    outbox: Outbox,
    tenant: TenantId,
    registry: ModuleRegistry,
    specifications: BTreeMap<[u8; 32], ReceiptSpec>,
    preparations: BTreeMap<[u8; 32], Prepared>,
    receipts: BTreeMap<[u8; 32], ReceiptMaterial>,
    submissions: BTreeMap<String, [u8; 32]>,
    owner_public_key: [u8; 32],
}

impl RealAgentLayer {
    fn new(
        root: &std::path::Path,
        owner_public_key: [u8; 32],
        specifications: BTreeMap<[u8; 32], ReceiptSpec>,
    ) -> Self {
        Self {
            owner_public_key,
            store: AgentStore::open(root).unwrap_or_else(|error| panic!("agent store: {error}")),
            outbox: Outbox::default(),
            tenant: TenantId::new("tenant-a").unwrap_or_else(|error| panic!("tenant: {error}")),
            registry: registry(),
            specifications,
            preparations: BTreeMap::new(),
            receipts: BTreeMap::new(),
            submissions: BTreeMap::new(),
        }
    }

    fn tracked(
        key: [u8; 32],
        state: SubmissionState,
        digest: Option<[u8; 32]>,
    ) -> TrackedSubmission {
        let evidence = digest.map_or_else(Vec::new, |value| {
            vec![AgentEvidenceRef {
                kind: "sequencer-receipt".to_owned(),
                digest: value,
            }]
        });
        TrackedSubmission {
            submission_ref: SubmissionRef::new(format!("sub-{}", hex(&key)))
                .unwrap_or_else(|error| panic!("submission ref: {error:?}")),
            state,
            verification_level: if evidence.is_empty() {
                Level::Unverified
            } else {
                Level::SequencerSigned
            },
            evidence,
            transitions: Vec::new(),
        }
    }
}

impl AgentBoundary for RealAgentLayer {
    fn prepare(
        &mut self,
        call: &Call<IdempotentMutation<ApiPrepareRequest>>,
    ) -> Result<AgentPreparation, AgentBoundaryError> {
        let mutation = call.request();
        let key = mutation.key.bytes();
        let request = &mutation.operation;
        if !self.preparations.contains_key(&key) {
            let specification = self
                .specifications
                .get(&key)
                .ok_or(AgentBoundaryError::CorruptResponse)?;
            let mut core = RecordedCore(CorePreparationState {
                network_id: NETWORK_ID,
                account_sequence: request.account_sequence.get(),
                protocol_timestamp: request.timestamp_bound.not_before.get().saturating_add(1),
                observed_head_sequence: 88,
                module_registry: self.registry.clone(),
            });
            let prepared = prepare_activity(
                &mut core,
                PreparationDefaults {
                    timestamp_span: request
                        .timestamp_bound
                        .not_after
                        .get()
                        .saturating_sub(request.timestamp_bound.not_before.get()),
                    fee_limit: Amount::from_u128(request.fee_limit.get()),
                    maximum_payload_bytes: 1_024,
                },
                PrepareRequest {
                    actor: Did::new(request.actor.as_str().as_bytes())
                        .map_err(|_| AgentBoundaryError::CorruptResponse)?,
                    authority: Authority::owner(&self.owner_public_key)
                        .map_err(|_| AgentBoundaryError::CorruptResponse)?,
                    activity_type: specification.activity,
                    expected_account_sequence: Some(request.account_sequence.get()),
                    timestamp_bound: Some(
                        TimestampBound::new(
                            request.timestamp_bound.not_before.get(),
                            request.timestamp_bound.not_after.get(),
                        )
                        .map_err(|_| AgentBoundaryError::CorruptResponse)?,
                    ),
                    fee_limit: Some(Amount::from_u128(request.fee_limit.get())),
                    idempotency_key: IdempotencyKey::new(key),
                    payload: request.payload.as_bytes().to_vec(),
                    declared_payload_limit: 1_024,
                },
            )
            .map_err(|_| AgentBoundaryError::Refused)?;
            self.preparations.insert(key, prepared);
        }
        let prepared = self
            .preparations
            .get(&key)
            .cloned()
            .ok_or(AgentBoundaryError::CorruptResponse)?;
        Ok(AgentPreparation {
            preparation_ref: PreparationRef::new(format!("prep-{}", hex(&key)))
                .map_err(|_| AgentBoundaryError::CorruptResponse)?,
            unsigned_canonical_bytes: prepared.canonical_bytes.clone(),
            signing_preimage: prepared.signing_preimage.to_vec(),
            disclosure: prepared.disclosure.clone(),
            actor: request.actor.clone(),
            authority: request.authority.clone(),
            account_sequence: request.account_sequence.get(),
            not_before: request.timestamp_bound.not_before.get(),
            not_after: request.timestamp_bound.not_after.get(),
            fee_limit: request.fee_limit.get(),
            activity_type: prepared.envelope.activity_type(),
            payload: prepared.envelope.payload().as_bytes().to_vec(),
            payload_hash: prepared.envelope.payload_hash(),
            idempotency_key: prepared.envelope.idempotency_key().bytes(),
        })
    }

    fn submit(
        &mut self,
        call: &Call<IdempotentMutation<SubmitRequest>>,
        signer_public_key: [u8; 32],
    ) -> Result<AgentObservation, AgentBoundaryError> {
        let mutation = call.request();
        let key = mutation.key.bytes();
        let prepared = self
            .preparations
            .get(&key)
            .cloned()
            .ok_or(AgentBoundaryError::CorruptResponse)?;
        let signature: [u8; 64] = mutation
            .operation
            .signature
            .as_bytes()
            .try_into()
            .map_err(|_| AgentBoundaryError::Refused)?;
        let signed = attach_external_signature(&prepared, signature)
            .map_err(|_| AgentBoundaryError::Refused)?;
        let verified = verify_before_submit(&signed, &prepared, &signer_public_key, &self.registry)
            .map_err(|_| AgentBoundaryError::Refused)?;
        let activity_id = verified.activity_id();
        self.outbox
            .enqueue(&mut self.store, self.tenant.clone(), key, verified)
            .map_err(|_| AgentBoundaryError::Refused)?;
        self.outbox
            .transition(
                &mut self.store,
                key,
                OutboxState::Submitted,
                "real transport accepted exact bytes",
                None,
            )
            .map_err(|_| AgentBoundaryError::Refused)?;
        let specification = self
            .specifications
            .get(&key)
            .copied()
            .ok_or(AgentBoundaryError::CorruptResponse)?;
        let material = receipt(activity_id, key[0], specification);
        daemon_receipt::store(
            &mut self.store,
            self.tenant.clone(),
            key,
            &material.canonical_bytes,
            &material.authorised_batch,
        )
        .map_err(|_| AgentBoundaryError::CorruptResponse)?;
        self.receipts.insert(key, material);
        self.outbox
            .transition(
                &mut self.store,
                key,
                OutboxState::Acknowledged,
                "core acknowledged",
                None,
            )
            .map_err(|_| AgentBoundaryError::Refused)?;
        let observation = AgentObservation {
            submission: Self::tracked(key, SubmissionState::Acknowledged, None),
            activity_id,
            receipt: None,
        };
        self.submissions.insert(
            observation.submission.submission_ref.as_str().to_owned(),
            key,
        );
        Ok(observation)
    }

    fn track(&mut self, call: &Call<TrackRequest>) -> Result<AgentObservation, AgentBoundaryError> {
        let key = *self
            .submissions
            .get(call.request().submission_ref.as_str())
            .ok_or(AgentBoundaryError::CorruptResponse)?;
        let activity_id = self
            .outbox
            .status(key)
            .map(|status| status.activity_id)
            .ok_or(AgentBoundaryError::CorruptResponse)?;
        let material = self
            .receipts
            .get(&key)
            .cloned()
            .ok_or(AgentBoundaryError::CorruptResponse)?;
        let digest: [u8; 32] = Sha256::digest(&material.canonical_bytes).into();
        Ok(AgentObservation {
            submission: Self::tracked(
                key,
                SubmissionState::Executed {
                    receipt_ref: ReceiptRef::new(format!("rcp-{}", hex(&key)))
                        .map_err(|_| AgentBoundaryError::CorruptResponse)?,
                },
                Some(digest),
            ),
            activity_id,
            receipt: Some(material),
        })
    }

    fn receipt_by_idempotency_key(
        &mut self,
        idempotency_key: [u8; 32],
        expected_activity_id: [u8; 32],
    ) -> Result<ReceiptLookup, AgentBoundaryError> {
        let served = daemon_receipt::serve(
            &self.store,
            self.tenant.clone(),
            DaemonReceiptKey::Idempotency(idempotency_key),
        )
        .map_err(|_| AgentBoundaryError::Unavailable)?;
        if served.metadata.activity_id != expected_activity_id {
            return Err(AgentBoundaryError::CorruptResponse);
        }
        self.receipts
            .get(&idempotency_key)
            .cloned()
            .map_or(Ok(ReceiptLookup::Absent), |material| {
                Ok(ReceiptLookup::Found(material))
            })
    }
}

fn receipt(activity_id: [u8; 32], marker: u8, specification: ReceiptSpec) -> ReceiptMaterial {
    let previous = [marker.saturating_add(1); 32];
    let resulting = [marker.saturating_add(2); 32];
    let sequence = u64::from(marker);
    let batch = support::committed_execution_batch_id(previous, [0x81; 32], sequence);
    let signer = SigningKey::from_bytes(&[marker.saturating_add(4); 32]);
    let encode = |signature: Option<[u8; 64]>| {
        let mut bytes = Vec::new();
        push_u16(&mut bytes, layerx_intents::canonical::PROTOCOL_VERSION);
        push_u16(&mut bytes, 0x5201);
        push_u16(&mut bytes, layerx_intents::canonical::PROTOCOL_VERSION);
        push_bytes(&mut bytes, &activity_id);
        push_u64(&mut bytes, sequence);
        push_bytes(&mut bytes, &previous);
        push_bytes(&mut bytes, &resulting);
        push_bytes(&mut bytes, &[0x81; 32]);
        bytes.extend_from_slice(&0_i32.to_be_bytes());
        bytes.extend_from_slice(&0_u32.to_be_bytes());
        bytes.extend_from_slice(&specification.fee.to_be_bytes());
        push_bytes(&mut bytes, &batch);
        push_u16(&mut bytes, u16::from(specification.activity.module() as u8));
        bytes.extend_from_slice(&1_u32.to_be_bytes());
        bytes.extend_from_slice(&1_u32.to_be_bytes());
        bytes.push(
            u8::try_from(specification.activity.ordinal())
                .unwrap_or_else(|error| panic!("operation: {error}")),
        );
        push_bytes(&mut bytes, &ASSET);
        bytes.extend_from_slice(&specification.amount.to_be_bytes());
        push_bytes(&mut bytes, &[0x91; 32]);
        bytes.extend_from_slice(&specification.amount.saturating_add(10).to_be_bytes());
        bytes.extend_from_slice(&10_u128.to_be_bytes());
        push_u64(&mut bytes, 1);
        push_bytes(&mut bytes, &[0x92; 32]);
        bytes.extend_from_slice(&20_u128.to_be_bytes());
        bytes.extend_from_slice(&20_u128.saturating_add(specification.amount).to_be_bytes());
        push_bytes(&mut bytes, &[0x93; 32]);
        push_bytes(&mut bytes, &[0x94; 32]);
        push_bytes(&mut bytes, &[0x95; 32]);
        push_u64(&mut bytes, 1_000);
        bytes.push(u8::from(signature.is_some()));
        if let Some(value) = signature {
            push_bytes(&mut bytes, &value);
        }
        bytes
    };
    let mut digest = Sha256::new();
    digest.update(b"LXP/v1/receipt\0");
    digest.update(encode(None));
    let signature = signer.sign(&<[u8; 32]>::from(digest.finalize()));
    ReceiptMaterial {
        canonical_bytes: encode(Some(signature.to_bytes())),
        authorised_batch: AuthorizedBatch::new(
            batch,
            ASSET,
            previous,
            resulting,
            signer.verifying_key().to_bytes(),
        ),
        verification_level: layerx_types::verify::VerificationLevel::SEQUENCER_SIGNED,
    }
}

fn push_u16(output: &mut Vec<u8>, value: u16) {
    output.extend_from_slice(&value.to_be_bytes());
}

fn push_u64(output: &mut Vec<u8>, value: u64) {
    output.extend_from_slice(&value.to_be_bytes());
}

fn push_bytes(output: &mut Vec<u8>, value: &[u8]) {
    let length =
        u32::try_from(value.len()).unwrap_or_else(|error| panic!("receipt field length: {error}"));
    output.extend_from_slice(&length.to_be_bytes());
    output.extend_from_slice(value);
}

struct BindingCore {
    registry: ModuleRegistry,
}

impl CorePreparationBoundary for BindingCore {
    fn preparation_state(&mut self, _actor: &Did) -> Result<CorePreparationState, CoreStateError> {
        Ok(CorePreparationState {
            network_id: NETWORK_ID,
            account_sequence: 5,
            protocol_timestamp: 100,
            observed_head_sequence: 91,
            module_registry: self.registry.clone(),
        })
    }
}

struct BindingAgent {
    signer: layerx_crypto::local::LocalSigner,
    store: AgentStore,
    outbox: Outbox,
    tenant: TenantId,
}

impl AgentBindingContract for BindingAgent {
    fn submit_binding(
        &mut self,
        request: BindingAgentRequest<'_>,
    ) -> Result<AgentSubmission, AgentBindingError> {
        use layerx_crypto::signer::{sign_disclosed, Signer as _};
        let registry = registry();
        let mut core = BindingCore {
            registry: registry.clone(),
        };
        let prepared = prepare_activity(
            &mut core,
            PreparationDefaults {
                timestamp_span: 30,
                fee_limit: Amount::from_u128(1),
                maximum_payload_bytes: 512,
            },
            PrepareRequest {
                actor: request.actor.clone(),
                authority: Authority::owner(&self.signer.public_key())
                    .map_err(|_| AgentBindingError::ContractViolation)?,
                activity_type: request.compiled.activity_type(),
                expected_account_sequence: Some(5),
                timestamp_bound: None,
                fee_limit: Some(Amount::from_u128(1)),
                idempotency_key: request.idempotency_key,
                payload: request.compiled.payload().as_bytes().to_vec(),
                declared_payload_limit: 512,
            },
        )
        .map_err(|_| AgentBindingError::ContractViolation)?;
        let signature = ready(sign_disclosed(
            &self.signer,
            &prepared.canonical_bytes,
            &prepared.disclosure,
            &registry,
        ))
        .map_err(|_| AgentBindingError::Refused)?;
        let signed = attach_external_signature(&prepared, *signature.as_bytes())
            .map_err(|_| AgentBindingError::ContractViolation)?;
        let verified =
            verify_before_submit(&signed, &prepared, &self.signer.public_key(), &registry)
                .map_err(|_| AgentBindingError::ContractViolation)?;
        let submission_id = request.idempotency_key.bytes();
        let activity_id = verified.activity_id();
        self.outbox
            .enqueue(
                &mut self.store,
                self.tenant.clone(),
                submission_id,
                verified,
            )
            .map_err(|_| AgentBindingError::Unavailable)?;
        Ok(AgentSubmission {
            submission_id,
            activity_id,
        })
    }
}

fn binding_receipt(submission: AgentSubmission, address: EvmAddress) -> AgentBindingReceipt {
    let signer = SigningKey::from_bytes(&[0x35; 32]);
    let encode = |signature: Option<[u8; 64]>| {
        let mut output = Vec::new();
        push_u16(&mut output, layerx_intents::canonical::PROTOCOL_VERSION);
        push_u16(&mut output, 0x5201);
        push_u16(&mut output, layerx_intents::canonical::PROTOCOL_VERSION);
        push_bytes(&mut output, &submission.activity_id);
        push_u64(&mut output, 9);
        push_bytes(&mut output, &[2; 32]);
        push_bytes(&mut output, &[3; 32]);
        push_bytes(&mut output, &[8; 32]);
        output.extend_from_slice(&0_i32.to_be_bytes());
        output.extend_from_slice(&0_u32.to_be_bytes());
        output.extend_from_slice(&0_u128.to_be_bytes());
        push_bytes(&mut output, &[4; 32]);
        push_u16(&mut output, ModuleId::Governance as u16);
        output.extend_from_slice(&1_u32.to_be_bytes());
        output.extend_from_slice(&1_u32.to_be_bytes());
        output.push(4);
        push_bytes(&mut output, &[5; 32]);
        output.extend_from_slice(&0_u128.to_be_bytes());
        push_bytes(&mut output, &[6; 32]);
        output.extend_from_slice(&100_u128.to_be_bytes());
        output.extend_from_slice(&100_u128.to_be_bytes());
        push_u64(&mut output, 1);
        let mut recorded = [0_u8; 32];
        recorded[12..].copy_from_slice(&address.bytes());
        push_bytes(&mut output, &recorded);
        output.extend_from_slice(&10_u128.to_be_bytes());
        output.extend_from_slice(&10_u128.to_be_bytes());
        push_bytes(&mut output, &[9; 32]);
        push_bytes(&mut output, &[10; 32]);
        push_bytes(&mut output, &[11; 32]);
        push_u64(&mut output, 1_000);
        output.push(u8::from(signature.is_some()));
        if let Some(signature) = signature {
            push_bytes(&mut output, &signature);
        }
        output
    };
    let mut digest = Sha256::new();
    digest.update(b"LXP/v1/receipt\0");
    digest.update(encode(None));
    let signature = signer.sign(&<[u8; 32]>::from(digest.finalize()));
    AgentBindingReceipt {
        submission_id: submission.submission_id,
        canonical_receipt: encode(Some(signature.to_bytes())),
        authorized_batch: AuthorizedBatch::new(
            [4; 32],
            [5; 32],
            [2; 32],
            [3; 32],
            signer.verifying_key().to_bytes(),
        ),
    }
}

struct Fixture {
    root: std::path::PathBuf,
    store_root: std::path::PathBuf,
    tenancy_digest: TenancyDigest,
    principal: PrincipalId,
    public_key: [u8; 32],
    signer: CustodySigner,
    contract: AgentClient,
    trace: TraceId,
}

impl Fixture {
    fn new(label: &str) -> Self {
        let root = directory(label);
        fs::create_dir_all(&root).unwrap_or_else(|error| panic!("fixture root: {error}"));
        let store_root = root.join("human-store");
        let secret = root.join("kms-root");
        fs::write(&secret, [0x42; 64]).unwrap_or_else(|error| panic!("KMS root: {error}"));
        let tenancy_digest = tenancy(&[("owner", "tenant-a")])
            .install(&store_root)
            .unwrap_or_else(|error| panic!("tenancy: {error}"));
        let principal = principal("owner");
        let provider = EnvelopeKms::new("file-kms://human-primary", &secret)
            .unwrap_or_else(|error| panic!("provider: {error}"));
        let keystore = Keystore::open_development(root.join("custody"), NETWORK_ID, provider)
            .unwrap_or_else(|error| panic!("keystore: {error}"));
        let public_key = keystore
            .generate(
                &principal,
                &custody_key(),
                KeyClass::HumanPrimary,
                KeyEntropy::new([0x51; 32], [0x52; 16], [0x53; 24])
                    .unwrap_or_else(|error| panic!("entropy: {error}")),
            )
            .unwrap_or_else(|error| panic!("generate key: {error}"));
        let signer_store =
            PrincipalStore::open(&store_root, retention_uniform(10_000), tenancy_digest)
                .unwrap_or_else(|error| panic!("signer store: {error}"));
        let signer = CustodySigner::new(
            keystore,
            signer_store,
            registry(),
            SigningLimits::new(1_000, 10_000).unwrap_or_else(|error| panic!("limits: {error}")),
        );
        let contract = AgentClient::daemon(
            "/run/layerx-agentd.sock",
            layerx_agent_api::agent_api_schema_v1().version,
        )
        .unwrap_or_else(|error| panic!("agent SDK: {error:?}"));
        Self {
            root,
            store_root,
            tenancy_digest,
            principal,
            public_key,
            signer,
            contract,
            trace: TraceId::mint([0x44; 16]),
        }
    }

    fn store(&self) -> PrincipalStore {
        PrincipalStore::open(
            &self.store_root,
            retention_uniform(10_000),
            self.tenancy_digest,
        )
        .unwrap_or_else(|error| panic!("principal store: {error}"))
    }

    fn send_route(&self, plan: &UnifiedPlan, request: &SubmitPlanRequest) -> RouteRequest {
        self.send_route_at(plan, request, 0)
    }

    fn send_route_at(
        &self,
        plan: &UnifiedPlan,
        request: &SubmitPlanRequest,
        index: usize,
    ) -> RouteRequest {
        let binding = &request.bindings[index];
        let leg = &plan.legs()[index];
        let protocol_account = |endpoint: &Endpoint| {
            let named = match endpoint {
                Endpoint::Human(named) | Endpoint::Agent(named) | Endpoint::AgentBudget(named) => {
                    named
                }
                Endpoint::PaxeerWallet => panic!("a send leg never touches the Paxeer wallet"),
            };
            layerx_intents::canonical::account_id_for_protocol(
                named,
                layerx_intents::canonical::PROTOCOL_VERSION,
            )
            .unwrap_or_else(|error| panic!("protocol account: {error:?}"))
        };
        let signer = layerx_crypto::local::LocalSigner::new([0x51; 32]);
        let debit = layerx_crypto::send::SendDebit {
            from: protocol_account(leg.source()),
            to: protocol_account(leg.destination()),
            asset: ASSET,
            amount: leg.amount().value(),
            source_sequence: binding.account_sequence,
            idempotency_key: binding.action_key,
            expires_at: binding.not_after,
            context_hash: [0x55; 32],
            conditions: Vec::new(),
            authorization_kind: SendAuthorizationKind::Owner as u8,
            network_id: NETWORK_ID,
            protocol_version: layerx_intents::canonical::PROTOCOL_VERSION,
        };
        let route = SendRoute {
            account_sequence: Sequence::from_u64(binding.account_sequence),
            idempotency_key: IdempotencyKey::new(binding.action_key),
            expires_at: TimestampSeconds::from_u64(binding.not_after),
            context_hash: ContextHash::new([0x55; 32]),
            authorization: support::sign_send(
                &signer,
                &debit,
                SendAuthorization::new(
                    SendAuthorizationKind::Owner,
                    PublicKey::new(self.public_key),
                    AuthorizationSignature::new([0x77; 64]),
                ),
            ),
            network_id: NetworkId::new(NETWORK_ID)
                .unwrap_or_else(|error| panic!("network: {error:?}")),
            protocol_version: ProtocolVersion::new(layerx_intents::canonical::PROTOCOL_VERSION)
                .unwrap_or_else(|error| panic!("protocol: {error:?}")),
        };
        RouteRequest {
            source: leg.source().clone(),
            destination: leg.destination().clone(),
            relationship: if matches!(leg.source(), Endpoint::Agent(_)) {
                Relationship::AgentAuthorized(route)
            } else {
                Relationship::Direct(route)
            },
            asset: leg.asset(),
            amount: leg.amount(),
        }
    }

    fn bind_wallet(&self, key: &EvmSigningKey) -> BindingJourney {
        let binding = BindingJourney::new(registry());
        let wallet = evm_address(key);
        let mut store = self.store();
        let mut scope = store
            .principal(&self.principal)
            .unwrap_or_else(|error| panic!("binding scope: {error}"));
        let did = Did::new(b"did:layerx:human").unwrap_or_else(|error| panic!("DID: {error:?}"));
        let network =
            NetworkId::new(NETWORK_ID).unwrap_or_else(|error| panic!("network: {error:?}"));
        let statement = BindingJourney::issue_statement(&did, network, wallet, 100, 60)
            .unwrap_or_else(|error| panic!("binding statement: {error}"));
        let mut agent = BindingAgent {
            signer: layerx_crypto::local::LocalSigner::new([0xa5; 32]),
            store: AgentStore::open(&self.root.join("binding-agent"))
                .unwrap_or_else(|error| panic!("binding agent store: {error}")),
            outbox: Outbox::default(),
            tenant: TenantId::new("tenant-a").unwrap_or_else(|error| panic!("tenant: {error}")),
        };
        let submission = binding
            .submit_initial(
                &mut scope,
                &statement,
                &ownership_signature(key, statement.signing_digest()),
                IdempotencyKey::new([0x41; 32]),
                &mut agent,
                101,
            )
            .unwrap_or_else(|error| panic!("binding submit: {error}"));
        let _ = binding
            .finalize(
                &mut scope,
                &binding_receipt(submission, wallet),
                102,
                &TraceId::mint([0x19; 16]),
            )
            .unwrap_or_else(|error| panic!("binding finalize: {error}"));
        binding
    }
}

impl Drop for Fixture {
    fn drop(&mut self) {
        let _ = fs::remove_dir_all(&self.root);
    }
}

fn custody_key() -> KeyId {
    KeyId::new("human-primary").unwrap_or_else(|error| panic!("key: {error}"))
}

fn deposit_for(plan: &UnifiedPlan, request: &SubmitPlanRequest, wallet: EvmAddress) -> DepositPlan {
    let credit = &request.bindings[1];
    let network = NetworkId::new(NETWORK_ID).unwrap_or_else(|error| panic!("network: {error:?}"));
    DepositPlan {
        journey_id: intent_journey_id("deposit-submit", plan.digest())
            .unwrap_or_else(|error| panic!("journey id: {error}")),
        idempotency_key: plan.digest(),
        wallet,
        network,
        paxeer_chain_id: 4_294_967_312,
        layerx_network: network,
        layerx_protocol_version: layerx_intents::canonical::PROTOCOL_VERSION,
        vault: EvmAddress::new([0x66; 20]),
        asset: plan.legs()[1].asset(),
        amount: plan.legs()[1].amount(),
        recipient: account(HOME),
        reserve: account("system:paxeer-reserve"),
        currency: "LXP".to_owned(),
        agent: DepositAgentPlan {
            actor: actor(),
            authority: authority(),
            account_sequence: credit.account_sequence,
            not_before: credit.not_before,
            not_after: credit.not_after,
            fee_limit: plan.legs()[1].fee(),
            custody_key: custody_key(),
        },
    }
}

#[test]
fn intent_submit_kernel_send_creates_a_journey_that_progresses_past_getting_ready() {
    let fixture = Fixture::new("intent-submit-kernel");
    let plan = kernel_plan();
    assert_eq!(
        plan.legs()[0].mechanism(),
        LegMechanism::Protocol(Mechanism::Send)
    );
    let request = valid_request(&plan, (995, 1_100));
    let expectation = expectation(1_000);
    verify_bindings(&plan, &request, &expectation)
        .unwrap_or_else(|error| panic!("bindings: {error}"));
    let routes = vec![fixture.send_route(&plan, &request)];
    let journey_id = intent_journey_id("kernel-submit", plan.digest())
        .unwrap_or_else(|error| panic!("journey id: {error}"));
    let mut store = fixture.store();
    let mut scope = store
        .principal(&fixture.principal)
        .unwrap_or_else(|error| panic!("scope: {error}"));
    let mut journey = start_kernel_journey(
        &mut scope,
        &plan,
        &request,
        &expectation,
        KernelStart {
            routes: &routes,
            journey_id: journey_id.clone(),
            custody_key: custody_key(),
            registry: &registry(),
        },
    )
    .unwrap_or_else(|error| panic!("start: {error}"));
    let created = journey
        .status()
        .unwrap_or_else(|error| panic!("status: {error}"));
    assert_eq!(created.journey_id(), &journey_id);
    assert_eq!(created.state(), JourneyState::GettingReady);

    let key = plan
        .action_key(0)
        .unwrap_or_else(|error| panic!("key: {error:?}"));
    let mut agent = RealAgentLayer::new(
        &fixture.root.join("agent-store"),
        fixture.public_key,
        BTreeMap::from([(
            key,
            ReceiptSpec {
                activity: asset_send(),
                amount: 90,
                fee: SEND_FEE,
            },
        )]),
    );
    let status = ready(drive_intent_journey(
        &mut journey,
        &mut scope,
        &mut agent,
        &IntentDriver {
            agent_contract: &fixture.contract,
            custody: &fixture.signer,
            registry: &registry(),
            trace: &fixture.trace,
            now: 1_000,
        },
    ))
    .unwrap_or_else(|error| panic!("drive: {error}"));
    assert!(
        !matches!(
            status.state(),
            JourneyState::GettingReady | JourneyState::Refused
        ),
        "journey did not progress: {:?}",
        status.state()
    );
    let submission = IntentSubmission::from_kernel(&status, plan.digest());
    assert_eq!(submission.journey_id(), &journey_id);
    assert_ne!(submission.state(), "getting-ready");

    let repeated = start_kernel_journey(
        &mut scope,
        &plan,
        &request,
        &expectation,
        KernelStart {
            routes: &routes,
            journey_id: journey_id.clone(),
            custody_key: custody_key(),
            registry: &registry(),
        },
    )
    .unwrap_or_else(|error| panic!("repeat: {error}"));
    assert_eq!(
        repeated
            .status()
            .unwrap_or_else(|error| panic!("repeat status: {error}"))
            .journey_id(),
        &journey_id
    );
}

#[test]
fn intent_submit_custody_deposit_creates_a_deposit_journey_past_getting_ready() {
    let fixture = Fixture::new("intent-submit-deposit");
    let key = evm_key();
    let wallet = evm_address(&key);
    let binding = fixture.bind_wallet(&key);
    let plan = deposit_plan_of(wallet);
    assert_eq!(
        plan.legs()
            .iter()
            .map(layerx_human_service::journeys::PlannedLeg::mechanism)
            .collect::<Vec<_>>(),
        vec![
            LegMechanism::PaxeerCustodyDeposit,
            LegMechanism::Protocol(Mechanism::BridgeDepositCredit)
        ]
    );
    let request = valid_request(&plan, (195, 1_010));
    let expectation = expectation(200);
    let deposit = deposit_for(&plan, &request, wallet);
    let mut store = fixture.store();
    let mut scope = store
        .principal(&fixture.principal)
        .unwrap_or_else(|error| panic!("scope: {error}"));
    let journey = start_deposit_journey(
        &mut scope,
        &plan,
        &request,
        &expectation,
        &deposit,
        &binding,
    )
    .unwrap_or_else(|error| panic!("start deposit: {error}"));
    let status = journey
        .status()
        .unwrap_or_else(|error| panic!("status: {error}"));
    let submission = IntentSubmission::from_deposit(&status, plan.digest());
    assert_eq!(submission.journey_id(), &deposit.journey_id);
    assert_eq!(submission.state(), "waiting-for-you");
    assert_eq!(submission.state_copy_key(), "status.waiting-for-you");

    let mut altered = deposit.clone();
    altered.amount = Amount::from_u128(26);
    altered.journey_id =
        JourneyId::new("jrn_altereddeposit").unwrap_or_else(|error| panic!("id: {error}"));
    assert_eq!(
        start_deposit_journey(
            &mut scope,
            &plan,
            &request,
            &expectation,
            &altered,
            &binding
        )
        .err(),
        Some(SubmitRefusal::DepositMismatch)
    );
}

#[test]
fn intent_submit_refuses_a_stale_digest_with_a_typed_forbidden_code() {
    let plan = kernel_plan();
    let mut stale = plan.digest();
    stale[0] ^= 0x01;
    let bindings = vec![binding_document(&plan, 0, SEQUENCE, (995, 1_100))];
    let request = SubmitPlanRequest::from_json(&submission_body(&plan, stale, bindings))
        .unwrap_or_else(|error| panic!("submission: {error}"));
    let refusal = verify_bindings(&plan, &request, &expectation(1_000))
        .err()
        .unwrap_or_else(|| panic!("stale digest accepted"));
    assert_eq!(refusal, SubmitRefusal::SignedDigestMismatch);
    assert_eq!(refusal.status(), 403);
    assert_eq!(refusal.code(), "forbidden");
    assert_eq!(refusal.copy_key(), "error.request.forbidden");
    assert_eq!(refusal.retry(), "final");

    let mut body = submission_body(
        &plan,
        plan.digest(),
        vec![binding_document(&plan, 0, SEQUENCE, (995, 1_100))],
    );
    body["plan_digest"] = json!(hex(&stale));
    let request =
        SubmitPlanRequest::from_json(&body).unwrap_or_else(|error| panic!("submission: {error}"));
    assert_eq!(
        verify_bindings(&plan, &request, &expectation(1_000)).err(),
        Some(SubmitRefusal::PlanDigestMismatch)
    );
}

#[test]
fn intent_submit_refuses_a_wrong_account_sequence_with_a_typed_conflict() {
    let fixture = Fixture::new("intent-submit-sequence");
    let plan = kernel_plan();
    let bindings = vec![binding_document(&plan, 0, SEQUENCE + 1, (995, 1_100))];
    let request = SubmitPlanRequest::from_json(&submission_body(&plan, plan.digest(), bindings))
        .unwrap_or_else(|error| panic!("submission: {error}"));
    let refusal = verify_bindings(&plan, &request, &expectation(1_000))
        .err()
        .unwrap_or_else(|| panic!("wrong sequence accepted"));
    assert_eq!(refusal, SubmitRefusal::StaleSequence { index: 0 });
    assert_eq!(refusal.status(), 409);
    assert_eq!(refusal.code(), "conflict");
    assert_eq!(refusal.copy_key(), "error.move.quote-expired");

    let routes = vec![fixture.send_route(&plan, &request)];
    let mut store = fixture.store();
    let mut scope = store
        .principal(&fixture.principal)
        .unwrap_or_else(|error| panic!("scope: {error}"));
    let journey_id = intent_journey_id("sequence-submit", plan.digest())
        .unwrap_or_else(|error| panic!("journey id: {error}"));
    assert_eq!(
        start_kernel_journey(
            &mut scope,
            &plan,
            &request,
            &expectation(1_000),
            KernelStart {
                routes: &routes,
                journey_id: journey_id.clone(),
                custody_key: custody_key(),
                registry: &registry(),
            },
        )
        .err(),
        Some(SubmitRefusal::StaleSequence { index: 0 })
    );
    assert!(
        layerx_human_service::journeys::JourneyEngine::load(&scope, &journey_id)
            .unwrap_or_else(|error| panic!("load: {error}"))
            .is_none()
    );
}

#[test]
fn intent_submit_refuses_an_expired_validity_window_with_a_typed_quote_expiry() {
    let plan = kernel_plan();
    let request = valid_request(&plan, (995, 1_100));
    let refusal = verify_bindings(&plan, &request, &expectation(1_101))
        .err()
        .unwrap_or_else(|| panic!("expired window accepted"));
    assert_eq!(refusal, SubmitRefusal::WindowExpired { index: 0 });
    assert_eq!(refusal.status(), 409);
    assert_eq!(refusal.code(), "quote-expired");
    assert_eq!(refusal.copy_key(), "error.move.quote-expired");
    assert_eq!(refusal.retry(), "structural");

    let mut forged = request.clone();
    forged.bindings[0].actor = "did:layerx:intruder".to_owned();
    assert_eq!(
        verify_bindings(&plan, &forged, &expectation(1_000)).err(),
        Some(SubmitRefusal::LegMismatch { index: 0 })
    );
    let mut greedy = request;
    greedy.bindings[0].fee_limit = MAX_FEE + 1;
    assert_eq!(
        verify_bindings(&plan, &greedy, &expectation(1_000)).err(),
        Some(SubmitRefusal::FeeLimitMismatch { index: 0 })
    );
}

#[test]
fn intent_submit_response_matches_the_golden_field_set() {
    let path = std::path::Path::new(env!("CARGO_MANIFEST_DIR"))
        .join("../../schema/human-api/golden/intent.submit.response.json");
    let golden: Value = serde_json::from_str(
        &fs::read_to_string(&path).unwrap_or_else(|error| panic!("golden: {error}")),
    )
    .unwrap_or_else(|error| panic!("golden json: {error}"));
    let expected = golden["body"]["result"]
        .as_object()
        .unwrap_or_else(|| panic!("golden result"))
        .clone();

    let fixture = Fixture::new("intent-submit-golden");
    let plan = kernel_plan();
    let request = valid_request(&plan, (995, 1_100));
    let routes = vec![fixture.send_route(&plan, &request)];
    let mut store = fixture.store();
    let mut scope = store
        .principal(&fixture.principal)
        .unwrap_or_else(|error| panic!("scope: {error}"));
    let journey = start_kernel_journey(
        &mut scope,
        &plan,
        &request,
        &expectation(1_000),
        KernelStart {
            routes: &routes,
            journey_id: intent_journey_id("golden-submit", plan.digest())
                .unwrap_or_else(|error| panic!("journey id: {error}")),
            custody_key: custody_key(),
            registry: &registry(),
        },
    )
    .unwrap_or_else(|error| panic!("start: {error}"));
    let status = journey
        .status()
        .unwrap_or_else(|error| panic!("status: {error}"));
    let response = IntentSubmission::from_kernel(&status, plan.digest()).to_json();
    let actual = response
        .as_object()
        .unwrap_or_else(|| panic!("response object"));
    assert_eq!(
        actual.keys().collect::<Vec<_>>(),
        expected.keys().collect::<Vec<_>>()
    );
    for (name, value) in &expected {
        let decoded = &actual[name];
        assert_eq!(
            std::mem::discriminant(decoded),
            std::mem::discriminant(value),
            "field {name} has a different JSON type"
        );
    }
    let journey_id = actual["journey_id"].as_str().unwrap_or_default();
    assert!(journey_id.starts_with("jrn_") && journey_id.len() > 4);
    assert!(journey_id[4..]
        .bytes()
        .all(|byte| byte.is_ascii_lowercase() || byte.is_ascii_digit()));
    assert_eq!(actual["plan_digest"], json!(hex(&plan.digest())));
    assert_eq!(
        actual["state_copy_key"],
        json!(format!(
            "status.{}",
            actual["state"].as_str().unwrap_or_default()
        ))
    );
    let binding = IntentLegBinding::from_json(&binding_document(&plan, 0, SEQUENCE, (995, 1_100)))
        .unwrap_or_else(|error| panic!("binding: {error}"));
    assert_eq!(binding.fee_currency, "LXP");
}

fn home() -> Endpoint {
    Endpoint::human(account(HOME)).unwrap_or_else(|error| panic!("home: {error:?}"))
}

fn worker() -> Endpoint {
    Endpoint::agent(account(WORKER)).unwrap_or_else(|error| panic!("worker: {error:?}"))
}

fn worker_funded(now: u64, custody: Option<EvmAddress>) -> ObservedState {
    observed_with(
        now,
        custody,
        vec![BalanceEntry::new(
            worker(),
            AssetId::new(ASSET),
            Amount::from_u128(500),
        )],
        Vec::new(),
    )
}

fn mechanisms(plan: &UnifiedPlan) -> Vec<LegMechanism> {
    plan.legs().iter().map(PlannedLeg::mechanism).collect()
}

fn sequenced_request(plan: &UnifiedPlan, window: (u64, u64)) -> SubmitPlanRequest {
    let mut sequence = SEQUENCE;
    let mut bindings = Vec::with_capacity(plan.legs().len());
    for (index, leg) in plan.legs().iter().enumerate() {
        bindings.push(binding_document(plan, index, sequence, window));
        if leg.mechanism().domain() == Domain::LayerX {
            sequence += 1;
        }
    }
    SubmitPlanRequest::from_json(&submission_body(plan, plan.digest(), bindings))
        .unwrap_or_else(|error| panic!("submission: {error}"))
}

fn kernel_start<'a>(
    routes: &'a [RouteRequest],
    journey_id: JourneyId,
    registry: &'a ModuleRegistry,
) -> KernelStart<'a> {
    KernelStart {
        routes,
        journey_id,
        custody_key: custody_key(),
        registry,
    }
}

fn withdrawal_for(
    plan: &UnifiedPlan,
    request: &SubmitPlanRequest,
    wallet: EvmAddress,
) -> WithdrawalPlan {
    let index = plan.legs().len() - 2;
    let debit = &plan.legs()[index];
    let binding = &request.bindings[index];
    WithdrawalPlan {
        journey_id: intent_journey_id("withdraw-child", plan.digest())
            .unwrap_or_else(|error| panic!("withdrawal id: {error}")),
        idempotency_key: plan.digest(),
        network: NetworkId::new(NETWORK_ID).unwrap_or_else(|error| panic!("network: {error:?}")),
        layerx_protocol_version: layerx_intents::canonical::STATE_COMMITMENT_PROTOCOL_VERSION,
        request_anchor: layerx_types::ids::CheckpointId::new([18; 32]),
        owner: account(HOME),
        withdrawals_account: account("system:paxeer-withdrawals"),
        payout_address: wallet,
        asset: debit.asset(),
        amount: debit.amount(),
        currency: "LXP".to_owned(),
        settlement: SettlementConfig {
            checkpoint_interval_seconds: 600,
            paxeer_block_seconds: 12,
            required_confirmations: 2,
        },
        reminder_interval_seconds: 30,
        agent: WithdrawalAgentPlan {
            actor: actor(),
            authority: authority(),
            account_sequence: binding.account_sequence,
            not_before: binding.not_before,
            not_after: binding.not_after,
            fee_limit: debit.fee(),
            custody_key: custody_key(),
        },
    }
}

#[test]
fn unified_shape_planner_and_submit_classifier_share_one_contract() {
    let wallet = evm_address(&evm_key());
    let send = LegMechanism::Protocol(Mechanism::Send);
    let deposit = LegMechanism::PaxeerCustodyDeposit;
    let credit = LegMechanism::Protocol(Mechanism::BridgeDepositCredit);
    let request = LegMechanism::Protocol(Mechanism::BridgeWithdrawRequest);
    let finalise = LegMechanism::PaxeerWithdrawFinalise;
    let cases = [
        (kernel_plan(), ExecutableShape::Kernel, vec![send], (0, 1)),
        (
            deposit_plan_of(wallet),
            ExecutableShape::CustodyDeposit,
            vec![deposit, credit],
            (0, 0),
        ),
        (
            planned(
                Endpoint::PaxeerWallet,
                worker(),
                25,
                &observed(200, Some(wallet)),
            ),
            ExecutableShape::DepositForward,
            vec![deposit, credit, send],
            (2, 3),
        ),
        (
            planned(
                home(),
                Endpoint::PaxeerWallet,
                40,
                &observed(1_000, Some(wallet)),
            ),
            ExecutableShape::WithdrawToWallet,
            vec![request, finalise],
            (0, 0),
        ),
        (
            planned(
                worker(),
                Endpoint::PaxeerWallet,
                40,
                &worker_funded(1_000, Some(wallet)),
            ),
            ExecutableShape::TransferThenWithdraw,
            vec![send, request, finalise],
            (0, 1),
        ),
    ];
    let mut labels = std::collections::BTreeSet::new();
    for (plan, shape, expected, kernel) in &cases {
        assert_eq!(&mechanisms(plan), expected, "{}", shape.label());
        assert_eq!(plan.executable_shape(), *shape);
        assert_eq!(ExecutableShape::classify(plan.legs()), Ok(*shape));
        assert_eq!(ExecutableShape::of_mechanisms(expected), Some(*shape));
        assert_eq!(shape.kernel_legs(plan.legs().len()), *kernel);
        for (index, leg) in plan.legs().iter().enumerate() {
            assert_eq!(leg.index(), index);
            assert_eq!(
                ExecutableShape::is_kernel(leg.mechanism()),
                (kernel.0..kernel.1).contains(&index),
                "{} leg {index}",
                shape.label()
            );
        }
        labels.insert(shape.label());
    }
    assert_eq!(labels.len(), cases.len());
}

#[test]
fn unified_shape_refuses_unsupported_reordered_and_discontinuous_legs() {
    let wallet = evm_address(&evm_key());
    let send = LegMechanism::Protocol(Mechanism::Send);
    let deposit = LegMechanism::PaxeerCustodyDeposit;
    let credit = LegMechanism::Protocol(Mechanism::BridgeDepositCredit);
    let request = LegMechanism::Protocol(Mechanism::BridgeWithdrawRequest);
    let finalise = LegMechanism::PaxeerWithdrawFinalise;
    for combination in [
        vec![],
        vec![deposit],
        vec![request],
        vec![credit, deposit],
        vec![finalise, request],
        vec![deposit, send, credit],
        vec![request, finalise, send],
        vec![send, deposit, credit],
        vec![deposit, credit, request, finalise],
    ] {
        assert_eq!(
            ExecutableShape::of_mechanisms(&combination),
            None,
            "{combination:?}"
        );
    }

    let forward = planned(
        Endpoint::PaxeerWallet,
        worker(),
        25,
        &observed(200, Some(wallet)),
    );
    let mut reordered = forward.legs().to_vec();
    reordered.swap(1, 2);
    assert_eq!(
        ExecutableShape::classify(&reordered),
        Err(Refusal::UnsupportedShape)
    );
    let mut skipped = forward.legs().to_vec();
    skipped.remove(1);
    assert_eq!(
        ExecutableShape::classify(&skipped),
        Err(Refusal::UnsupportedShape)
    );
    assert_eq!(
        ExecutableShape::classify(&forward.legs()[1..]),
        Err(Refusal::UnsupportedShape)
    );

    let transfer = planned(
        worker(),
        Endpoint::PaxeerWallet,
        40,
        &worker_funded(1_000, Some(wallet)),
    );
    let mut broken = kernel_plan().legs().to_vec();
    broken.extend(transfer.legs()[1..].iter().cloned());
    assert_eq!(
        ExecutableShape::of_mechanisms(
            &broken.iter().map(PlannedLeg::mechanism).collect::<Vec<_>>()
        ),
        Some(ExecutableShape::TransferThenWithdraw)
    );
    assert_eq!(
        ExecutableShape::classify(&broken),
        Err(Refusal::UnsupportedShape)
    );

    let topped_up = UnifiedIntent::new(
        home(),
        Endpoint::PaxeerWallet,
        AssetId::new(ASSET),
        Amount::from_u128(600),
        Constraints::new(TimestampSeconds::from_u64(1_200), MAX_FEE, true),
    )
    .unwrap_or_else(|error| panic!("intent: {error:?}"));
    let allowance = |seed: u8, mechanism: LegMechanism| {
        SignedAllowance::new(
            AllowanceId::new([seed; 32]).unwrap_or_else(|error| panic!("allowance id: {error}")),
            AllowanceKind::BudgetAllowance,
            AllowanceScope::new(
                Endpoint::PaxeerWallet,
                home(),
                AssetId::new(ASSET),
                mechanism,
            ),
            Amount::from_u128(1_000),
            Amount::from_u128(1_000),
            TimestampSeconds::from_u64(5_000),
        )
        .unwrap_or_else(|error| panic!("allowance: {error}"))
    };
    assert_eq!(
        plan(&topped_up, &observed(1_000, Some(wallet))).err(),
        Some(Refusal::TopUpNotAuthorized { shortfall: 107 })
    );
    let authorised = observed_with(
        1_000,
        Some(wallet),
        Vec::new(),
        vec![allowance(0x61, deposit), allowance(0x62, credit)],
    );
    assert_eq!(
        plan(&topped_up, &authorised).err(),
        Some(Refusal::UnsupportedShape)
    );
}

#[test]
fn unified_shape_deposit_forward_keeps_one_parent_and_gates_forwarding_on_the_credit() {
    let fixture = Fixture::new("unified-shape-forward");
    let key = evm_key();
    let wallet = evm_address(&key);
    let binding = fixture.bind_wallet(&key);
    let plan = planned(
        Endpoint::PaxeerWallet,
        worker(),
        25,
        &observed(200, Some(wallet)),
    );
    assert_eq!(plan.executable_shape(), ExecutableShape::DepositForward);
    let request = sequenced_request(&plan, (195, 1_010));
    assert_eq!(request.bindings[2].account_sequence, SEQUENCE + 1);
    let expectation = expectation(200);
    let deposit = deposit_for(&plan, &request, wallet);
    let routes = vec![fixture.send_route_at(&plan, &request, 2)];
    let registry = registry();
    let journey_id = intent_journey_id("forward-submit", plan.digest())
        .unwrap_or_else(|error| panic!("journey id: {error}"));

    let (deposit_id, forward_id) = {
        let mut store = fixture.store();
        let mut scope = store
            .principal(&fixture.principal)
            .unwrap_or_else(|error| panic!("scope: {error}"));
        assert_eq!(
            start_deposit_journey(
                &mut scope,
                &plan,
                &request,
                &expectation,
                &deposit,
                &binding
            )
            .err(),
            Some(SubmitRefusal::UnsupportedPlan)
        );
        assert_eq!(
            start_kernel_journey(
                &mut scope,
                &plan,
                &request,
                &expectation,
                kernel_start(&routes, journey_id.clone(), &registry),
            )
            .err(),
            Some(SubmitRefusal::UnsupportedPlan)
        );
        let forward_with = |scope: &mut layerx_human_service::store::PrincipalScope<'_>,
                            request: &SubmitPlanRequest,
                            deposit: &DepositPlan,
                            routes: &[RouteRequest]| {
            start_deposit_forward_journey(
                scope,
                &plan,
                request,
                &expectation,
                deposit,
                &binding,
                kernel_start(routes, journey_id.clone(), &registry),
            )
            .err()
        };
        let mut altered = deposit.clone();
        altered.amount = Amount::from_u128(deposit.amount.value() + 1);
        assert_eq!(
            forward_with(&mut scope, &request, &altered, &routes),
            Some(SubmitRefusal::DepositMismatch)
        );
        let mut forged = request.clone();
        forged.bindings[2].action_key[0] ^= 0x01;
        assert_eq!(
            forward_with(&mut scope, &forged, &deposit, &routes),
            Some(SubmitRefusal::LegMismatch { index: 2 })
        );
        let mut reordered = request.clone();
        reordered.bindings.swap(1, 2);
        assert_eq!(
            forward_with(&mut scope, &reordered, &deposit, &routes),
            Some(SubmitRefusal::LegMismatch { index: 1 })
        );
        let mut changed = routes.clone();
        changed[0].amount = Amount::from_u128(changed[0].amount.value() + 1);
        assert_eq!(
            forward_with(&mut scope, &request, &deposit, &changed),
            Some(SubmitRefusal::RouteMismatch { index: 2 })
        );
        assert_eq!(
            forward_with(&mut scope, &request, &deposit, &[]),
            Some(SubmitRefusal::UnboundLegs {
                expected: 1,
                bound: 0
            })
        );
        assert!(DepositForwardJourney::load(&scope, &journey_id)
            .unwrap_or_else(|error| panic!("load: {error:?}"))
            .is_none());

        let parent = start_deposit_forward_journey(
            &mut scope,
            &plan,
            &request,
            &expectation,
            &deposit,
            &binding,
            kernel_start(&routes, journey_id.clone(), &registry),
        )
        .unwrap_or_else(|error| panic!("start forward: {error}"));
        let status = parent
            .status(&scope)
            .unwrap_or_else(|error| panic!("status: {error:?}"));
        assert_eq!(status.journey_id(), &journey_id);
        assert_eq!(status.deposit_journey_id(), &deposit.journey_id);
        assert!(matches!(status.stage(), DepositForwardStage::Depositing(_)));
        assert_eq!(status.credit(), None);
        assert!(status.forward_receipts().is_empty());
        let submission = IntentSubmission::from_deposit_forward(&status, plan.digest());
        assert_eq!(submission.journey_id(), &journey_id);
        assert_eq!(submission.state(), "waiting-for-you");

        let repeated = start_deposit_forward_journey(
            &mut scope,
            &plan,
            &request,
            &expectation,
            &deposit,
            &binding,
            kernel_start(&routes, journey_id.clone(), &registry),
        )
        .unwrap_or_else(|error| panic!("repeat forward: {error}"));
        assert_eq!(
            repeated
                .status(&scope)
                .unwrap_or_else(|error| panic!("repeat status: {error:?}")),
            status
        );

        let other = intent_journey_id("forward-other", plan.digest())
            .unwrap_or_else(|error| panic!("journey id: {error}"));
        assert_eq!(
            start_deposit_forward_journey(
                &mut scope,
                &plan,
                &request,
                &expectation,
                &deposit,
                &binding,
                kernel_start(&routes, other.clone(), &registry),
            )
            .err(),
            Some(SubmitRefusal::AlreadySubmitted)
        );
        assert!(DepositForwardJourney::load(&scope, &other)
            .unwrap_or_else(|error| panic!("load: {error:?}"))
            .is_none());

        let resized = planned(
            Endpoint::PaxeerWallet,
            worker(),
            26,
            &observed(200, Some(wallet)),
        );
        let resized_request = sequenced_request(&resized, (195, 1_010));
        let resized_routes = vec![fixture.send_route_at(&resized, &resized_request, 2)];
        assert_eq!(
            start_deposit_forward_journey(
                &mut scope,
                &resized,
                &resized_request,
                &expectation,
                &deposit_for(&resized, &resized_request, wallet),
                &binding,
                kernel_start(&resized_routes, journey_id.clone(), &registry),
            )
            .err(),
            Some(SubmitRefusal::AlreadySubmitted)
        );
        (
            status.deposit_journey_id().clone(),
            status.forward_journey_id().clone(),
        )
    };

    let mut store = fixture.store();
    let mut scope = store
        .principal(&fixture.principal)
        .unwrap_or_else(|error| panic!("restarted scope: {error}"));
    let mut parent = DepositForwardJourney::load(&scope, &journey_id)
        .unwrap_or_else(|error| panic!("load: {error:?}"))
        .unwrap_or_else(|| panic!("the returned parent is missing after a restart"));
    let status = parent
        .status(&scope)
        .unwrap_or_else(|error| panic!("restarted status: {error:?}"));
    assert_eq!(status.deposit_journey_id(), &deposit_id);
    assert_eq!(status.forward_journey_id(), &forward_id);
    assert!(matches!(status.stage(), DepositForwardStage::Depositing(_)));

    let forward_key = plan
        .action_key(2)
        .unwrap_or_else(|error| panic!("key: {error:?}"));
    let mut agent = RealAgentLayer::new(
        &fixture.root.join("agent-store"),
        fixture.public_key,
        BTreeMap::from([(
            forward_key,
            ReceiptSpec {
                activity: asset_send(),
                amount: 25,
                fee: SEND_FEE,
            },
        )]),
    );
    ready(parent.advance_forward(
        &mut scope,
        &fixture.contract,
        &mut agent,
        &fixture.signer,
        &registry,
        &fixture.trace,
        300,
    ))
    .unwrap_or_else(|error| panic!("advance forward: {error:?}"));
    assert!(agent.preparations.is_empty());
    assert!(agent.submissions.is_empty());
    let engine = JourneyEngine::load(&scope, &forward_id)
        .unwrap_or_else(|error| panic!("forward engine: {error}"))
        .unwrap_or_else(|| panic!("forward engine missing"));
    assert_eq!(
        engine
            .status()
            .unwrap_or_else(|error| panic!("forward status: {error}"))
            .state(),
        JourneyState::GettingReady
    );
    assert!(engine
        .verified_leg_evidence(0)
        .unwrap_or_else(|error| panic!("evidence: {error}"))
        .is_none());
    assert_eq!(
        parent
            .status(&scope)
            .unwrap_or_else(|error| panic!("gated status: {error:?}")),
        status
    );
    let child = parent
        .deposit(&scope)
        .unwrap_or_else(|error| panic!("deposit child: {error:?}"));
    assert_eq!(
        child
            .status()
            .unwrap_or_else(|error| panic!("deposit status: {error:?}"))
            .journey_id(),
        &deposit_id
    );
}

#[test]
fn unified_shape_withdraw_to_wallet_starts_one_parent_bound_to_the_signed_debit() {
    let fixture = Fixture::new("unified-shape-withdraw");
    let wallet = evm_address(&evm_key());
    let plan = planned(
        home(),
        Endpoint::PaxeerWallet,
        40,
        &observed(1_000, Some(wallet)),
    );
    assert_eq!(plan.executable_shape(), ExecutableShape::WithdrawToWallet);
    let request = sequenced_request(&plan, (995, 1_100));
    assert_eq!(request.bindings[1].account_sequence, SEQUENCE + 1);
    let expectation = expectation(1_000);
    let withdrawal = withdrawal_for(&plan, &request, wallet);
    let registry = registry();
    let journey_id = intent_journey_id("withdraw-submit", plan.digest())
        .unwrap_or_else(|error| panic!("journey id: {error}"));

    let status = {
        let mut store = fixture.store();
        let mut scope = store
            .principal(&fixture.principal)
            .unwrap_or_else(|error| panic!("scope: {error}"));
        assert_eq!(
            start_kernel_journey(
                &mut scope,
                &plan,
                &request,
                &expectation,
                kernel_start(&[], journey_id.clone(), &registry),
            )
            .err(),
            Some(SubmitRefusal::UnsupportedPlan)
        );
        let binding = &request.bindings[0];
        let mut mismatches = Vec::new();
        let mut amount = withdrawal.clone();
        amount.amount = Amount::from_u128(withdrawal.amount.value() + 1);
        mismatches.push(amount);
        let mut sequence = withdrawal.clone();
        sequence.agent.account_sequence += 1;
        mismatches.push(sequence);
        let mut window = withdrawal.clone();
        window.agent.not_after = binding.not_after + 1;
        mismatches.push(window);
        let mut fee = withdrawal.clone();
        fee.agent.fee_limit = binding.fee_limit + 1;
        mismatches.push(fee);
        let mut owner = withdrawal.clone();
        owner.owner = account(WORKER);
        mismatches.push(owner);
        for mismatch in &mismatches {
            assert_eq!(
                start_withdrawal_intent(
                    &mut scope,
                    &plan,
                    &request,
                    &expectation,
                    mismatch,
                    kernel_start(&[], journey_id.clone(), &registry),
                )
                .err(),
                Some(SubmitRefusal::WithdrawalMismatch)
            );
        }
        let mut forged = request.clone();
        forged.bindings[1].action_key[0] ^= 0x01;
        assert_eq!(
            start_withdrawal_intent(
                &mut scope,
                &plan,
                &forged,
                &expectation,
                &withdrawal,
                kernel_start(&[], journey_id.clone(), &registry),
            )
            .err(),
            Some(SubmitRefusal::LegMismatch { index: 1 })
        );
        assert!(IntentWithdrawalJourney::load(&scope, &journey_id)
            .unwrap_or_else(|error| panic!("load: {error:?}"))
            .is_none());
        assert!(WithdrawalJourney::list_readonly(&scope)
            .unwrap_or_else(|error| panic!("list: {error:?}"))
            .is_empty());

        let parent = start_withdrawal_intent(
            &mut scope,
            &plan,
            &request,
            &expectation,
            &withdrawal,
            kernel_start(&[], journey_id.clone(), &registry),
        )
        .unwrap_or_else(|error| panic!("start withdrawal: {error}"));
        let status = parent
            .status(&scope)
            .unwrap_or_else(|error| panic!("status: {error:?}"));
        assert_eq!(status.journey_id(), &journey_id);
        assert_eq!(status.prelude_journey_id(), None);
        assert_eq!(status.withdrawal_journey_id(), &withdrawal.journey_id);
        assert!(matches!(
            status.stage(),
            IntentWithdrawalStage::Withdrawing(_)
        ));
        assert!(!status.debit_claimed());
        let submission = IntentSubmission::from_withdrawal(&status, plan.digest());
        assert_eq!(submission.journey_id(), &journey_id);
        assert_ne!(submission.state(), "done");

        let repeated = start_withdrawal_intent(
            &mut scope,
            &plan,
            &request,
            &expectation,
            &withdrawal,
            kernel_start(&[], journey_id.clone(), &registry),
        )
        .unwrap_or_else(|error| panic!("repeat withdrawal: {error}"));
        assert_eq!(
            repeated
                .status(&scope)
                .unwrap_or_else(|error| panic!("repeat status: {error:?}")),
            status
        );
        let other = intent_journey_id("withdraw-other", plan.digest())
            .unwrap_or_else(|error| panic!("journey id: {error}"));
        assert_eq!(
            start_withdrawal_intent(
                &mut scope,
                &plan,
                &request,
                &expectation,
                &withdrawal,
                kernel_start(&[], other.clone(), &registry),
            )
            .err(),
            Some(SubmitRefusal::AlreadySubmitted)
        );
        let resized = planned(
            home(),
            Endpoint::PaxeerWallet,
            41,
            &observed(1_000, Some(wallet)),
        );
        let resized_request = sequenced_request(&resized, (995, 1_100));
        assert_eq!(
            start_withdrawal_intent(
                &mut scope,
                &resized,
                &resized_request,
                &expectation,
                &withdrawal_for(&resized, &resized_request, wallet),
                kernel_start(&[], journey_id.clone(), &registry),
            )
            .err(),
            Some(SubmitRefusal::AlreadySubmitted)
        );
        status
    };

    let mut store = fixture.store();
    let scope = store
        .principal(&fixture.principal)
        .unwrap_or_else(|error| panic!("restarted scope: {error}"));
    let parent = IntentWithdrawalJourney::load(&scope, &journey_id)
        .unwrap_or_else(|error| panic!("load: {error:?}"))
        .unwrap_or_else(|| panic!("the returned parent is missing after a restart"));
    assert_eq!(
        parent
            .status(&scope)
            .unwrap_or_else(|error| panic!("restarted status: {error:?}")),
        status
    );
    let children =
        WithdrawalJourney::list_readonly(&scope).unwrap_or_else(|error| panic!("list: {error:?}"));
    assert_eq!(children.len(), 1);
    assert!(
        WithdrawalJourney::load_readonly(&scope, &withdrawal.journey_id)
            .unwrap_or_else(|error| panic!("child: {error:?}"))
            .is_some()
    );
}

#[test]
fn unified_shape_transfer_then_withdraw_resumes_one_parent_and_claims_each_receipt_once() {
    let fixture = Fixture::new("unified-shape-transfer-withdraw");
    let wallet = evm_address(&evm_key());
    let plan = planned(
        worker(),
        Endpoint::PaxeerWallet,
        40,
        &worker_funded(1_000, Some(wallet)),
    );
    assert_eq!(
        plan.executable_shape(),
        ExecutableShape::TransferThenWithdraw
    );
    let request = sequenced_request(&plan, (995, 1_100));
    assert_eq!(request.bindings[1].account_sequence, SEQUENCE + 1);
    let expectation = expectation(1_000);
    let withdrawal = withdrawal_for(&plan, &request, wallet);
    let routes = vec![fixture.send_route_at(&plan, &request, 0)];
    let registry = registry();
    let journey_id = intent_journey_id("transfer-withdraw-submit", plan.digest())
        .unwrap_or_else(|error| panic!("journey id: {error}"));
    let prelude_key = plan
        .action_key(0)
        .unwrap_or_else(|error| panic!("key: {error:?}"));
    let mut agent = RealAgentLayer::new(
        &fixture.root.join("agent-store"),
        fixture.public_key,
        BTreeMap::from([(
            prelude_key,
            ReceiptSpec {
                activity: asset_send(),
                amount: plan.legs()[0].amount().value(),
                fee: SEND_FEE,
            },
        )]),
    );

    let prelude_id = {
        let mut store = fixture.store();
        let mut scope = store
            .principal(&fixture.principal)
            .unwrap_or_else(|error| panic!("scope: {error}"));
        let mut changed = routes.clone();
        changed[0].amount = Amount::from_u128(changed[0].amount.value() + 1);
        assert_eq!(
            start_withdrawal_intent(
                &mut scope,
                &plan,
                &request,
                &expectation,
                &withdrawal,
                kernel_start(&changed, journey_id.clone(), &registry),
            )
            .err(),
            Some(SubmitRefusal::RouteMismatch { index: 0 })
        );
        let mut prelude_sequence = withdrawal.clone();
        prelude_sequence.agent.account_sequence = request.bindings[0].account_sequence;
        assert_eq!(
            start_withdrawal_intent(
                &mut scope,
                &plan,
                &request,
                &expectation,
                &prelude_sequence,
                kernel_start(&routes, journey_id.clone(), &registry),
            )
            .err(),
            Some(SubmitRefusal::WithdrawalMismatch)
        );

        let mut parent = start_withdrawal_intent(
            &mut scope,
            &plan,
            &request,
            &expectation,
            &withdrawal,
            kernel_start(&routes, journey_id.clone(), &registry),
        )
        .unwrap_or_else(|error| panic!("start transfer-then-withdraw: {error}"));
        let status = parent
            .status(&scope)
            .unwrap_or_else(|error| panic!("status: {error:?}"));
        assert_eq!(status.journey_id(), &journey_id);
        assert_eq!(status.stage(), &IntentWithdrawalStage::Transferring);
        assert_eq!(
            IntentSubmission::from_withdrawal(&status, plan.digest()).state(),
            "processing"
        );
        assert!(
            WithdrawalJourney::load_readonly(&scope, &withdrawal.journey_id)
                .unwrap_or_else(|error| panic!("child: {error:?}"))
                .is_none()
        );
        let first = ready(parent.advance_prelude(
            &mut scope,
            &fixture.contract,
            &mut agent,
            &fixture.signer,
            &registry,
            &fixture.trace,
            1_000,
        ))
        .unwrap_or_else(|error| panic!("first prelude phase: {error:?}"));
        assert_eq!(first.stage(), &IntentWithdrawalStage::Transferring);
        assert!(
            WithdrawalJourney::load_readonly(&scope, &withdrawal.journey_id)
                .unwrap_or_else(|error| panic!("child: {error:?}"))
                .is_none()
        );
        status
            .prelude_journey_id()
            .cloned()
            .unwrap_or_else(|| panic!("prelude journey id"))
    };

    let mut store = fixture.store();
    let mut scope = store
        .principal(&fixture.principal)
        .unwrap_or_else(|error| panic!("restarted scope: {error}"));
    let repeated = start_withdrawal_intent(
        &mut scope,
        &plan,
        &request,
        &expectation,
        &withdrawal,
        kernel_start(&routes, journey_id.clone(), &registry),
    )
    .unwrap_or_else(|error| panic!("repeat after restart: {error}"));
    assert_eq!(
        repeated
            .status(&scope)
            .unwrap_or_else(|error| panic!("repeat status: {error:?}"))
            .journey_id(),
        &journey_id
    );
    let mut parent = IntentWithdrawalJourney::load(&scope, &journey_id)
        .unwrap_or_else(|error| panic!("load: {error:?}"))
        .unwrap_or_else(|| panic!("the returned parent is missing after a restart"));
    let mut status = parent
        .status(&scope)
        .unwrap_or_else(|error| panic!("restarted status: {error:?}"));
    for now in 1_001..1_011 {
        if status.stage() != &IntentWithdrawalStage::Transferring {
            break;
        }
        status = ready(parent.advance_prelude(
            &mut scope,
            &fixture.contract,
            &mut agent,
            &fixture.signer,
            &registry,
            &fixture.trace,
            now,
        ))
        .unwrap_or_else(|error| panic!("prelude phase at {now}: {error:?}"));
    }
    assert!(
        matches!(status.stage(), IntentWithdrawalStage::Withdrawing(_)),
        "prelude did not settle: {:?}",
        status.stage()
    );
    assert_eq!(agent.preparations.len(), 1);
    assert_eq!(agent.submissions.len(), 1);
    let engine = JourneyEngine::load(&scope, &prelude_id)
        .unwrap_or_else(|error| panic!("prelude engine: {error}"))
        .unwrap_or_else(|| panic!("prelude engine missing"));
    let evidence = engine
        .verified_leg_evidence(0)
        .unwrap_or_else(|error| panic!("evidence: {error}"))
        .unwrap_or_else(|| panic!("the prelude leg has no verified receipt"));
    assert_eq!(
        status.prelude_receipts(),
        &[(evidence.activity_id, evidence.receipt_digest)]
    );
    assert!(!status.debit_claimed());
    assert!(
        WithdrawalJourney::load_readonly(&scope, &withdrawal.journey_id)
            .unwrap_or_else(|error| panic!("child: {error:?}"))
            .is_some()
    );
    assert_eq!(
        WithdrawalJourney::list_readonly(&scope)
            .unwrap_or_else(|error| panic!("list: {error:?}"))
            .len(),
        1
    );

    let claim = |scope: &mut layerx_human_service::store::PrincipalScope<'_>,
                 digest: [u8; 32],
                 owner: &str| {
        claim_receipt(scope, evidence.activity_id, digest, owner, 1_020)
            .unwrap_or_else(|error| panic!("claim: {error:?}"))
    };
    assert!(claim(
        &mut scope,
        evidence.receipt_digest,
        journey_id.as_str()
    ));
    assert!(!claim(
        &mut scope,
        evidence.receipt_digest,
        "jrn_otherparent"
    ));
    let mut forged = evidence.receipt_digest;
    forged[0] ^= 0x01;
    assert!(!claim(&mut scope, forged, journey_id.as_str()));

    let again = ready(parent.advance_prelude(
        &mut scope,
        &fixture.contract,
        &mut agent,
        &fixture.signer,
        &registry,
        &fixture.trace,
        1_030,
    ))
    .unwrap_or_else(|error| panic!("settled prelude: {error:?}"));
    assert_eq!(again, status);
    assert_eq!(agent.preparations.len(), 1);
    assert_eq!(agent.submissions.len(), 1);
}
