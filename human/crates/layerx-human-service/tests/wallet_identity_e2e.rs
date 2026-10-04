use layerx_human_test_support as support;

#[allow(dead_code)]
#[path = "../../layerx-human-kms/tests/support/attestor_cluster.rs"]
mod cluster;

use std::collections::BTreeMap;
use std::env;
use std::fs;
use std::future::Future;
use std::os::unix::fs::PermissionsExt as _;
use std::path::Path;
use std::pin::pin;
use std::sync::Arc;
use std::task::{Context, Poll, Wake, Waker};
use std::time::{Duration, SystemTime, UNIX_EPOCH};

use base64::{engine::general_purpose::URL_SAFE_NO_PAD, Engine as _};
use ed25519_dalek::{Signer as _, SigningKey};
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
    prepare_activity_for_protocol, CorePreparationBoundary, CorePreparationState, CoreStateError,
    PreparationDefaults, PrepareRequest, Prepared,
};
use layerx_agentd::receipt::{self as daemon_receipt, ReceiptLookupKey as DaemonReceiptKey};
use layerx_agentd::sign::{attach_external_signature, verify_before_submit};
use layerx_agentd::store::{Store as AgentStore, TenantId};
use layerx_human_identity_provider::{AssertionConfig, AssertionVerifier, Policy, State};
use layerx_human_service::custody::{
    CustodyError, CustodySigner, KeyId, Keystore, KmsError, SendPlanAuthorization, SigningLimits,
};
use layerx_human_service::journeys::{
    authority_label, drive_intent_journey, intent_journey_id, plan, start_kernel_journey,
    verify_bindings, AgentBoundary, AgentBoundaryError, AgentObservation, AgentPreparation,
    BalanceEntry, BindingExpectation, Constraints, Endpoint, FeeSchedule, IntentDriver,
    IntentSubmission, JourneyPhase, JourneyState, KernelStart, LegMechanism, Mechanism,
    ObservedState, ReceiptLookup, ReceiptMaterial, Relationship, RouteRequest, SendRoute,
    SubmitPlanRequest, UnifiedIntent, UnifiedPlan,
};
use layerx_human_service::server::production_components::{
    AttestorCustodyConfig, AttestorKms, ProductionComponentsConfig,
};
use layerx_human_service::store::{PrincipalId, PrincipalStore};
use layerx_human_service::trace::TraceId;
use layerx_proof::receipt::AuthorizedBatch;
use layerx_sdk::{Call, Client as AgentClient};
use layerx_types::account::AccountId;
use layerx_types::activity::{Authority, TimestampBound};
use layerx_types::amount::Amount;
use layerx_types::ids::{AssetId, Did, IdempotencyKey};
use layerx_types::intent::{
    AuthorizationSignature, ContextHash, NetworkId, ProtocolVersion, PublicKey, SendAuthorization,
    SendAuthorizationKind, Sequence, TimestampSeconds,
};
use layerx_types::payload::{ActivityType, ModuleId, ModuleRegistration, ModuleRegistry};
use serde_json::{json, Value};
use sha2::{Digest as _, Sha256};

use support::{directory, retention_uniform, tenancy};

const NETWORK_ID: u32 = 125;
const ASSET: [u8; 32] = [0x33; 32];
const WORKER: &str = "agent:did:layerx:worker:main";
const SUBJECT: &str = "wallet-user-0001";
const WALLET_ACCOUNT: &str = "0x00000000000000000000000000000000000000a1";
const SEQUENCE: u64 = 7;
const SEND_FEE: u128 = 2;
const MAX_FEE: u128 = 20;
const AMOUNT: u128 = 90;
const SEND_PROTOCOL: u16 = layerx_intents::canonical::STATE_COMMITMENT_PROTOCOL_VERSION;

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
        Poll::Pending => panic!("wallet identity journey future unexpectedly blocked"),
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

fn activity_types() -> Vec<String> {
    registry()
        .registrations()
        .iter()
        .flat_map(|module| {
            module
                .activity_types()
                .iter()
                .map(|kind| kind.value().to_string())
        })
        .collect()
}

fn account(value: &str) -> AccountId {
    AccountId::parse(value).unwrap_or_else(|error| panic!("account: {error:?}"))
}

fn authority() -> AuthorityRef {
    AuthorityRef::new("custody-human-primary")
        .unwrap_or_else(|error| panic!("authority: {error:?}"))
}

fn custody_key() -> KeyId {
    KeyId::new("human-primary").unwrap_or_else(|error| panic!("key: {error}"))
}

fn hex(bytes: &[u8]) -> String {
    use std::fmt::Write as _;
    bytes.iter().fold(String::new(), |mut output, byte| {
        let _ = write!(output, "{byte:02x}");
        output
    })
}

fn wall_clock() -> u64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .unwrap_or_else(|error| panic!("clock: {error}"))
        .as_secs()
}

fn fees() -> FeeSchedule {
    FeeSchedule::new(vec![(LegMechanism::Protocol(Mechanism::Send), SEND_FEE)])
        .unwrap_or_else(|error| panic!("fees: {error:?}"))
}

fn kernel_plan(home: &str, now: u64) -> UnifiedPlan {
    let source = Endpoint::human(account(home)).unwrap_or_else(|error| panic!("home: {error:?}"));
    let observed = ObservedState::new(
        TimestampSeconds::from_u64(now),
        account(home),
        None,
        vec![BalanceEntry::new(
            source.clone(),
            AssetId::new(ASSET),
            Amount::from_u128(500),
        )],
        Vec::new(),
        Vec::new(),
        fees(),
    )
    .unwrap_or_else(|error| panic!("observed state: {error:?}"));
    let intent = UnifiedIntent::new(
        source,
        Endpoint::agent(account(WORKER)).unwrap_or_else(|error| panic!("worker: {error:?}")),
        AssetId::new(ASSET),
        Amount::from_u128(AMOUNT),
        Constraints::new(
            TimestampSeconds::from_u64(now.saturating_add(200)),
            MAX_FEE,
            false,
        ),
    )
    .unwrap_or_else(|error| panic!("intent: {error:?}"));
    plan(&intent, &observed).unwrap_or_else(|error| panic!("plan: {error:?}"))
}

fn submit_request(plan: &UnifiedPlan, actor: &AgentDid, window: (u64, u64)) -> SubmitPlanRequest {
    let bindings: Vec<Value> = (0..plan.legs().len())
        .map(|index| {
            let leg = &plan.legs()[index];
            json!({
                "leg_index": index,
                "action_key": hex(&plan.action_key(index).unwrap_or_else(|error| panic!("action key: {error:?}"))),
                "actor": actor.as_str(),
                "authority": authority_label(leg.authority()),
                "relationship": "self",
                "account_sequence": SEQUENCE,
                "not_before": window.0,
                "not_after": window.1,
                "fee_limit": {"amount": leg.fee().saturating_add(1).to_string(), "currency": "LXP"},
            })
        })
        .collect();
    SubmitPlanRequest::from_json(&json!({
        "plan_digest": hex(&plan.digest()),
        "signed_digest": hex(&plan.digest()),
        "bindings": bindings,
    }))
    .unwrap_or_else(|error| panic!("submission: {error}"))
}

struct SendAuthority<'a> {
    cluster: &'a cluster::Cluster,
    kms: &'a AttestorKms,
    signer: &'a CustodySigner,
    principal: &'a PrincipalId,
    public_key: [u8; 32],
}

fn send_route(
    plan: &UnifiedPlan,
    request: &SubmitPlanRequest,
    home: &str,
    owner: &SendAuthority<'_>,
) -> RouteRequest {
    let binding = &request.bindings[0];
    let protocol = SEND_PROTOCOL;
    let authorization = SendPlanAuthorization {
        plan_id: binding.action_key,
        action_key: binding.action_key,
        principal: owner.principal.as_str().to_owned(),
        tenant: "tenant-a".to_owned(),
        binding_digest: owner
            .signer
            .evm_binding(owner.principal, &custody_key())
            .unwrap_or_else(|error| panic!("custody binding: {error:?}"))
            .digest(),
        from: layerx_intents::canonical::account_id_for_protocol(&account(home), protocol)
            .unwrap_or_else(|error| panic!("source account: {error:?}")),
        to: layerx_intents::canonical::account_id_for_protocol(&account(WORKER), protocol)
            .unwrap_or_else(|error| panic!("destination account: {error:?}")),
        asset: ASSET,
        amount: AMOUNT,
        sequence: binding.account_sequence,
        idempotency_key: binding.action_key,
        expires_at: binding.not_after,
        context: [0x55; 32],
        network: NETWORK_ID,
        protocol,
        not_before: binding.not_before,
        not_after: binding.not_after,
    };
    let signature = owner
        .kms
        .authorize_kernel_send(
            owner.signer,
            owner.principal,
            &custody_key(),
            &authorization,
            binding.fee_limit,
        )
        .unwrap_or_else(|error| {
            panic!(
                "owner send authorization: {error}: {:?}: {}",
                owner.kms.last_refusal_code(),
                owner.cluster.logs()
            )
        });
    let leg = &plan.legs()[0];
    RouteRequest {
        source: leg.source().clone(),
        destination: leg.destination().clone(),
        relationship: Relationship::Direct(SendRoute {
            account_sequence: Sequence::from_u64(binding.account_sequence),
            idempotency_key: IdempotencyKey::new(binding.action_key),
            expires_at: TimestampSeconds::from_u64(binding.not_after),
            context_hash: ContextHash::new([0x55; 32]),
            authorization: SendAuthorization::new(
                SendAuthorizationKind::Owner,
                PublicKey::new(owner.public_key),
                AuthorizationSignature::new(signature),
            ),
            network_id: NetworkId::new(NETWORK_ID)
                .unwrap_or_else(|error| panic!("network: {error:?}")),
            protocol_version: ProtocolVersion::new(protocol)
                .unwrap_or_else(|error| panic!("protocol: {error:?}")),
        }),
        asset: leg.asset(),
        amount: leg.amount(),
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
    signers: Vec<[u8; 32]>,
    owner: [u8; 32],
}

impl RealAgentLayer {
    fn new(
        root: &std::path::Path,
        owner: [u8; 32],
        specifications: BTreeMap<[u8; 32], ReceiptSpec>,
    ) -> Self {
        Self {
            store: AgentStore::open(root).unwrap_or_else(|error| panic!("agent store: {error}")),
            outbox: Outbox::default(),
            tenant: TenantId::new("tenant-a").unwrap_or_else(|error| panic!("tenant: {error}")),
            registry: registry(),
            specifications,
            preparations: BTreeMap::new(),
            receipts: BTreeMap::new(),
            submissions: BTreeMap::new(),
            signers: Vec::new(),
            owner,
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
            let prepared = prepare_activity_for_protocol(
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
                    authority: Authority::owner(&self.owner)
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
                SEND_PROTOCOL,
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
        self.signers.push(signer_public_key);
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

fn write_private(path: &Path, bytes: &[u8]) {
    fs::write(path, bytes).unwrap_or_else(|error| panic!("write {}: {error}", path.display()));
    fs::set_permissions(path, fs::Permissions::from_mode(0o600))
        .unwrap_or_else(|error| panic!("permissions {}: {error}", path.display()));
}

/// The complete components environment of the kernel host under attestor custody: the five
/// attestors, three signers, the service's client leaf chained to the attestor CA, and no
/// `LAYERX_HUMAN_KMS_*` variable at all.
fn loader_environment(
    root: &Path,
    cluster: &cluster::Cluster,
    tenancy_digest: [u8; 32],
) -> Vec<(&'static str, String)> {
    write_private(
        &root.join("attestor-ca.der"),
        &cluster
            .pki
            .certificate_der("node-ca")
            .unwrap_or_else(|error| panic!("node CA: {error}")),
    );
    write_private(
        &root.join("attestor-client.der"),
        &cluster
            .pki
            .certificate_der("gateway")
            .unwrap_or_else(|error| panic!("gateway certificate: {error}")),
    );
    write_private(
        &root.join("attestor-client-key.der"),
        &cluster
            .pki
            .key_der("gateway")
            .unwrap_or_else(|error| panic!("gateway key: {error}")),
    );
    let path = |name: &str| root.join(name).display().to_string();
    let secret = |byte: u8| URL_SAFE_NO_PAD.encode([byte; 32]);
    let nodes = cluster
        .nodes
        .iter()
        .map(|(id, address)| format!("{id}={address}"))
        .collect::<Vec<_>>()
        .join(",");
    vec![
        ("LAYERX_HUMAN_STORE_ROOT", path("human-store")),
        ("LAYERX_HUMAN_CUSTODY_ROOT", path("custody")),
        ("LAYERX_HUMAN_AUTH_INDEX_ROOT", path("auth-index")),
        (
            "LAYERX_HUMAN_TENANCY_DIGEST",
            URL_SAFE_NO_PAD.encode(tenancy_digest),
        ),
        ("LAYERX_HUMAN_AUTH_INDEX_KEY", secret(0x61)),
        ("LAYERX_HUMAN_STREAM_CURSOR_KEY", secret(0x62)),
        ("LAYERX_HUMAN_RP_ID", "paxportwallet.com".to_owned()),
        ("LAYERX_HUMAN_RP_NAME", "Paxport".to_owned()),
        (
            "LAYERX_HUMAN_ORIGIN",
            "https://paxportwallet.com".to_owned(),
        ),
        ("LAYERX_HUMAN_CEREMONY_TTL_SECONDS", "300".to_owned()),
        ("LAYERX_HUMAN_ASSERTION_TTL_SECONDS", "300".to_owned()),
        ("LAYERX_HUMAN_SESSION_TTL_SECONDS", "900".to_owned()),
        ("LAYERX_HUMAN_REFRESH_TTL_SECONDS", "86400".to_owned()),
        ("LAYERX_HUMAN_STEP_UP_TTL_SECONDS", "300".to_owned()),
        ("LAYERX_HUMAN_AUTH_RATE_ATTEMPTS", "10".to_owned()),
        ("LAYERX_HUMAN_AUTH_RATE_WINDOW_SECONDS", "60".to_owned()),
        (
            "LAYERX_HUMAN_RETENTION_JOURNEYS_SECONDS",
            "86400".to_owned(),
        ),
        (
            "LAYERX_HUMAN_RETENTION_NOTIFICATIONS_SECONDS",
            "86400".to_owned(),
        ),
        ("LAYERX_HUMAN_RETENTION_AUDIT_SECONDS", "86400".to_owned()),
        (
            "LAYERX_HUMAN_RETENTION_TELEMETRY_SECONDS",
            "86400".to_owned(),
        ),
        ("LAYERX_HUMAN_RETENTION_CACHE_SECONDS", "86400".to_owned()),
        ("LAYERX_HUMAN_CAPABILITY_TTL_SECONDS", "30".to_owned()),
        ("LAYERX_HUMAN_AGENT_SOCKET", path("agent.sock")),
        ("LAYERX_HUMAN_AGENT_MAX_FRAME_BYTES", "1048576".to_owned()),
        ("LAYERX_HUMAN_AGENT_MAX_CONNECTIONS", "4".to_owned()),
        ("LAYERX_HUMAN_AGENT_MAX_STREAMS", "4".to_owned()),
        ("LAYERX_HUMAN_AGENT_MAX_QUEUED_BYTES", "1048576".to_owned()),
        ("LAYERX_HUMAN_AGENT_DEADLINE_SECONDS", "5".to_owned()),
        ("LAYERX_HUMAN_SECURITY_SOCKET", path("security.sock")),
        ("LAYERX_HUMAN_SECURITY_DEADLINE_SECONDS", "5".to_owned()),
        (
            "LAYERX_HUMAN_SECURITY_MAX_FRAME_BYTES",
            "1048576".to_owned(),
        ),
        (
            "LAYERX_HUMAN_IDENTITY_BINDING_SOCKET",
            path("identity-binding.sock"),
        ),
        (
            "LAYERX_HUMAN_IDENTITY_BINDING_TENANT",
            "tenant-a".to_owned(),
        ),
        ("LAYERX_HUMAN_IDENTITY_BINDING_PEER_UID", "4020".to_owned()),
        ("LAYERX_HUMAN_IDENTITY_BINDING_PEER_GID", "4020".to_owned()),
        (
            "LAYERX_HUMAN_IDENTITY_BINDING_DEADLINE_SECONDS",
            "10".to_owned(),
        ),
        ("LAYERX_HUMAN_IDENTITY_SOCKET", path("identity.sock")),
        ("LAYERX_HUMAN_IDENTITY_DEADLINE_SECONDS", "5".to_owned()),
        (
            "LAYERX_HUMAN_IDENTITY_MAX_FRAME_BYTES",
            "1048576".to_owned(),
        ),
        ("LAYERX_HUMAN_IDENTITY_PEER_UID", "4020".to_owned()),
        ("LAYERX_HUMAN_IDENTITY_PEER_GID", "4020".to_owned()),
        ("LAYERX_HUMAN_MOVEMENT_SOCKET", path("movement.sock")),
        ("LAYERX_HUMAN_MOVEMENT_PEER_UID", "4020".to_owned()),
        ("LAYERX_HUMAN_MOVEMENT_PEER_GID", "4020".to_owned()),
        (
            "LAYERX_HUMAN_MOVEMENT_MAX_FRAME_BYTES",
            "1048576".to_owned(),
        ),
        ("LAYERX_HUMAN_MOVEMENT_DEADLINE_SECONDS", "5".to_owned()),
        ("LAYERX_HUMAN_ATTESTOR_NODES", nodes),
        ("LAYERX_HUMAN_ATTESTOR_SIGNERS", cluster::SIGNERS.join(",")),
        (
            "LAYERX_HUMAN_ATTESTOR_ROOT_CERTIFICATE_DER",
            path("attestor-ca.der"),
        ),
        (
            "LAYERX_HUMAN_ATTESTOR_CLIENT_CERTIFICATE_DER",
            path("attestor-client.der"),
        ),
        (
            "LAYERX_HUMAN_ATTESTOR_CLIENT_PRIVATE_KEY_DER",
            path("attestor-client-key.der"),
        ),
        ("LAYERX_HUMAN_ATTESTOR_DEADLINE_SECONDS", "120".to_owned()),
        ("LAYERX_HUMAN_NETWORK_ID", NETWORK_ID.to_string()),
        ("LAYERX_HUMAN_SIGNING_RATE_MAXIMUM", "1000".to_owned()),
        (
            "LAYERX_HUMAN_SIGNING_RATE_WINDOW_SECONDS",
            "10000".to_owned(),
        ),
        (
            "LAYERX_HUMAN_AGENT_ACTOR",
            format!("did:layerx:{}", hex(&[0x77; 32])),
        ),
        (
            "LAYERX_HUMAN_AGENT_AUTHORITY",
            "custody-human-primary".to_owned(),
        ),
        (
            "LAYERX_HUMAN_AGENT_TIMESTAMP_SPAN_SECONDS",
            "300".to_owned(),
        ),
        ("LAYERX_HUMAN_AGENT_FEE_LIMIT", "1000000".to_owned()),
        (
            "LAYERX_HUMAN_ONBOARDING_SPONSOR_PRINCIPAL",
            "onboarding-sponsor".to_owned(),
        ),
        ("LAYERX_HUMAN_ONBOARDING_INITIAL_FUNDING", "1000".to_owned()),
        ("LAYERX_HUMAN_EVM_GAS_LIMIT", "300000".to_owned()),
        ("LAYERX_HUMAN_EVM_MAX_FEE_PER_GAS", "1000000000".to_owned()),
        (
            "LAYERX_HUMAN_EVM_MAX_PRIORITY_FEE_PER_GAS",
            "1000000000".to_owned(),
        ),
        (
            "LAYERX_HUMAN_BINDING_STATEMENT_TTL_SECONDS",
            "300".to_owned(),
        ),
        (
            "LAYERX_HUMAN_AGENT_PURPOSE_CATALOG",
            path("purpose-catalog.json"),
        ),
        (
            "LAYERX_HUMAN_AGENT_OWNER_ACCOUNT",
            WALLET_ACCOUNT.to_owned(),
        ),
        ("LAYERX_HUMAN_AGENT_RECOVERY_ROOT", secret(0x63)),
        ("LAYERX_HUMAN_AGENT_RECOVERY_THRESHOLD", "1".to_owned()),
        (
            "LAYERX_HUMAN_PAXEER_RPC_URL",
            "https://127.0.0.1:9443".to_owned(),
        ),
        ("LAYERX_HUMAN_PAXEER_RPC_TIMEOUT_SECONDS", "5".to_owned()),
        (
            "LAYERX_HUMAN_PAXEER_TRUST_ANCHOR_DER",
            path("attestor-ca.der"),
        ),
        ("LAYERX_HUMAN_PAXEER_CHAIN_ID", "125".to_owned()),
        (
            "LAYERX_HUMAN_PAXEER_RPC_URLS",
            json!(["https://127.0.0.1:9443", "https://127.0.0.2:9443"]).to_string(),
        ),
        ("LAYERX_HUMAN_PAXEER_MINIMUM_AGREEMENT", "2".to_owned()),
        ("LAYERX_HUMAN_EXIT_REQUIRED_CONFIRMATIONS", "12".to_owned()),
        ("LAYERX_HUMAN_ACTIVITY_FRESHNESS_SECONDS", "300".to_owned()),
        (
            "LAYERX_HUMAN_ACTIVITY_EXPORT_MAXIMUM_BYTES",
            "1048576".to_owned(),
        ),
        ("LAYERX_HUMAN_EXIT_POLL_CADENCE_SECONDS", "5".to_owned()),
        ("LAYERX_HUMAN_EXIT_DELAYED_AFTER_POLLS", "12".to_owned()),
        (
            "LAYERX_HUMAN_CONTINUATION_UNKNOWN_DEADLINE_SECONDS",
            "300".to_owned(),
        ),
    ]
}

/// Loads the attestor custody backend the way the components binary does on the kernel
/// host: through `ProductionComponentsConfig::from_environment` with the attestor group set
/// and the KMS group absent.
fn loader_custody(environment: &[(&'static str, String)]) -> AttestorCustodyConfig {
    for (name, value) in environment {
        env::set_var(name, value);
    }
    let config = ProductionComponentsConfig::from_environment()
        .unwrap_or_else(|error| panic!("components loader without the KMS group: {error}"));
    assert_eq!(config.protocol_version(), SEND_PROTOCOL);
    config
        .attestor_custody()
        .cloned()
        .unwrap_or_else(|| panic!("components loader did not select attestor custody"))
}

fn kernel_policy() -> Value {
    let mut caps = serde_json::Map::new();
    caps.insert("native".to_owned(), json!({ "per_operation": "1000" }));
    caps.insert(hex(&ASSET), json!({ "per_operation": "1000" }));
    json!({
        "version": 1,
        "defaults": {
            "modules": { "asset": [asset_send().ordinal()] },
            "caps": caps,
        }
    })
}

fn identity_state(root: &std::path::Path, key_set_url: String) -> State {
    let mut state = State::open(
        root,
        Policy {
            root: [0x43; 32],
            threshold: 1,
            delay_seconds: 86_400,
        },
    )
    .unwrap_or_else(|error| panic!("identity state: {error}"));
    state
        .enable_assertion(
            AssertionVerifier::new(AssertionConfig {
                jwks_url: key_set_url,
                issuer: cluster::ISSUER.to_owned(),
                audience: cluster::AUDIENCE.to_owned(),
                clock_skew_seconds: 60,
                refresh_interval_seconds: 300,
            })
            .unwrap_or_else(|error| panic!("assertion verifier: {error}")),
        )
        .unwrap_or_else(|error| panic!("enable assertion: {error}"));
    state
}

#[test]
fn wallet_identity_e2e_signs_a_kernel_send_through_the_attestors_to_a_verified_receipt() {
    let kms_variables: Vec<String> = env::vars()
        .map(|(name, _)| name)
        .filter(|name| name.starts_with("LAYERX_HUMAN_KMS_"))
        .collect();
    assert!(
        kms_variables.is_empty(),
        "the attestor-only path must run without the KMS group: {kms_variables:?}"
    );
    let cluster = cluster::Cluster::start_with_kernel_policy(&activity_types(), &kernel_policy())
        .unwrap_or_else(|error| panic!("attestor cluster: {error}"));
    let root = directory("wallet-identity-e2e");
    fs::create_dir_all(&root).unwrap_or_else(|error| panic!("fixture root: {error}"));
    fs::set_permissions(&root, fs::Permissions::from_mode(0o700))
        .unwrap_or_else(|error| panic!("fixture root permissions: {error}"));

    let mut identity = identity_state(&root.join("identity"), cluster.key_set_url());
    let assertion = cluster
        .tokens
        .mint(SUBJECT)
        .unwrap_or_else(|error| panic!("assertion: {error}"));
    let signed_in = wall_clock();
    let (principal, created) = identity
        .open_or_create_by_assertion(&assertion, None, signed_in)
        .unwrap_or_else(|error| panic!("assertion sign-in: {error}"));
    assert!(created);
    assert_eq!(principal.subject(), SUBJECT);
    assert_eq!(principal.did(), None);
    let principal_id = PrincipalId::new(principal.principal())
        .unwrap_or_else(|error| panic!("principal: {error}"));

    let store_root = root.join("human-store");
    let tenancy_digest = tenancy(&[(principal.principal(), "tenant-a")])
        .install(&store_root)
        .unwrap_or_else(|error| panic!("tenancy: {error}"));

    let environment = loader_environment(&root, &cluster, tenancy_digest.bytes());
    let kms = AttestorKms::connect(loader_custody(&environment))
        .unwrap_or_else(|error| panic!("attestor custody: {error}"));
    let keystore = Keystore::open_production(root.join("custody"), NETWORK_ID, kms.clone())
        .unwrap_or_else(|error| panic!("production keystore: {error}"));
    let public_key = kms
        .create_owned_key(
            &keystore,
            &principal_id,
            &custody_key(),
            principal.subject(),
            WALLET_ACCOUNT,
        )
        .unwrap_or_else(|error| panic!("attestor key: {error}: {}", cluster.logs()));
    let descriptor = keystore
        .describe(&principal_id, &custody_key())
        .unwrap_or_else(|error| panic!("describe key: {error:?}"));
    assert_eq!(descriptor.public_key, public_key);

    let did = format!("did:layerx:{}", hex(&public_key));
    let (reopened, created) = identity
        .open_or_create_by_assertion(&assertion, Some(&did), wall_clock())
        .unwrap_or_else(|error| panic!("wallet DID: {error}"));
    assert!(!created);
    assert_eq!(reopened.principal(), principal.principal());
    let resolved = identity
        .resolve_assertion(&assertion, wall_clock())
        .unwrap_or_else(|error| panic!("resolve assertion: {error}"));
    assert_eq!(resolved.principal(), principal.principal());
    assert_eq!(resolved.did(), Some(did.as_str()));

    let signing_assertion = cluster
        .tokens
        .mint(SUBJECT)
        .unwrap_or_else(|error| panic!("signing assertion: {error}"));
    kms.admit_assertion(resolved.subject(), &signing_assertion)
        .unwrap_or_else(|error| panic!("admit assertion: {error}"));
    let signer_store = PrincipalStore::open(&store_root, retention_uniform(10_000), tenancy_digest)
        .unwrap_or_else(|error| panic!("signer store: {error}"));
    let signer = CustodySigner::new(
        keystore,
        signer_store,
        registry(),
        SigningLimits::new(1_000, 10_000).unwrap_or_else(|error| panic!("limits: {error}")),
    );

    let primary_key =
        KeyId::new("human-primary").unwrap_or_else(|error| panic!("primary key: {error}"));
    assert!(
        signer
            .public_wallet_identity(resolved.principal(), &primary_key)
            .is_err(),
        "native-only attestor fixture has no approved secp256k1 wallet identity"
    );

    let home = format!("agent:{did}:main");
    let actor = AgentDid::new(did.clone()).unwrap_or_else(|error| panic!("actor: {error:?}"));
    let now = wall_clock();
    let plan = kernel_plan(&home, now);
    assert_eq!(
        plan.legs()[0].mechanism(),
        LegMechanism::Protocol(Mechanism::Send)
    );
    let request = submit_request(
        &plan,
        &actor,
        (now.saturating_sub(5), now.saturating_add(100)),
    );
    let expectation = BindingExpectation {
        actor: actor.clone(),
        authority: authority(),
        account_sequence: SEQUENCE,
        currency: "LXP".to_owned(),
        now,
    };
    verify_bindings(&plan, &request, &expectation)
        .unwrap_or_else(|error| panic!("bindings: {error}"));
    let client = cluster
        .client()
        .unwrap_or_else(|error| panic!("operator client: {error}"));
    let audit_before = cluster
        .audit(&client)
        .unwrap_or_else(|error| panic!("audit before: {error}"));
    let routes = vec![send_route(
        &plan,
        &request,
        &home,
        &SendAuthority {
            cluster: &cluster,
            kms: &kms,
            signer: &signer,
            principal: &principal_id,
            public_key,
        },
    )];
    let journey_id = intent_journey_id("kernel-submit", plan.digest())
        .unwrap_or_else(|error| panic!("journey id: {error}"));

    let mut store = PrincipalStore::open(&store_root, retention_uniform(10_000), tenancy_digest)
        .unwrap_or_else(|error| panic!("principal store: {error}"));
    let mut scope = store
        .principal(&principal_id)
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
    assert_eq!(
        journey
            .status()
            .unwrap_or_else(|error| panic!("status: {error}"))
            .state(),
        JourneyState::GettingReady
    );

    let key = plan
        .action_key(0)
        .unwrap_or_else(|error| panic!("key: {error:?}"));
    let mut agent = RealAgentLayer::new(
        &root.join("agent-store"),
        public_key,
        BTreeMap::from([(
            key,
            ReceiptSpec {
                activity: asset_send(),
                amount: AMOUNT,
                fee: SEND_FEE,
            },
        )]),
    );
    let contract = AgentClient::daemon(
        "/run/layerx-agentd.sock",
        layerx_agent_api::agent_api_schema_v1().version,
    )
    .unwrap_or_else(|error| panic!("agent SDK: {error:?}"));
    let trace = TraceId::mint([0x44; 16]);
    let mut status = ready(drive_intent_journey(
        &mut journey,
        &mut scope,
        &mut agent,
        &IntentDriver {
            agent_contract: &contract,
            custody: &signer,
            registry: &registry(),
            trace: &trace,
            now,
        },
    ))
    .unwrap_or_else(|error| {
        panic!(
            "drive: {error}: {:?}: {}",
            kms.last_refusal_code(),
            cluster.logs()
        )
    });
    assert!(
        !matches!(
            status.state(),
            JourneyState::GettingReady | JourneyState::Refused
        ),
        "journey did not progress: {:?}",
        status.state()
    );
    assert_eq!(agent.signers, vec![public_key]);
    let audit_after = cluster
        .audit(&client)
        .unwrap_or_else(|error| panic!("audit after: {error}"));
    assert!(
        audit_after
            .iter()
            .zip(&audit_before)
            .all(|(after, before)| after > before),
        "attestor audit did not advance: {audit_before:?} -> {audit_after:?}"
    );

    for step in 1..=8_u64 {
        if status.state() == JourneyState::Done {
            break;
        }
        status = ready(journey.advance(
            &mut scope,
            &contract,
            &mut agent,
            &signer,
            &registry(),
            &trace,
            now.saturating_add(step),
        ))
        .unwrap_or_else(|error| panic!("advance: {error}"));
    }
    assert_eq!(status.state(), JourneyState::Done);
    assert_eq!(status.phases(), [JourneyPhase::ReceiptVerified]);
    assert!(status.receipt_digests().iter().all(Option::is_some));
    assert_eq!(agent.signers, vec![public_key]);
    let submission = IntentSubmission::from_kernel(&status, plan.digest());
    assert_eq!(submission.journey_id(), &journey_id);

    env::remove_var("LAYERX_HUMAN_ATTESTOR_NODES");
    let refused = ProductionComponentsConfig::from_environment()
        .err()
        .unwrap_or_else(|| panic!("components loader started with neither custody backend"));
    assert!(
        refused.contains("LAYERX_HUMAN_KMS_PROVIDER_REFERENCE is required"),
        "refusal without any custody backend does not name the KMS group: {refused}"
    );
    for (name, _) in &environment {
        env::remove_var(name);
    }

    drop(scope);
    drop(store);
    let _ = fs::remove_dir_all(&root);
}

#[test]
fn wallet_identity_e2e_refuses_startup_when_the_attestors_are_unreachable() {
    let root = cluster::scratch("wallet-identity-unreachable")
        .unwrap_or_else(|error| panic!("scratch: {error}"));
    let mut pki = cluster::Pki::new(&root);
    pki.authority("node-ca")
        .unwrap_or_else(|error| panic!("node CA: {error}"));
    pki.authority("gateway-ca")
        .unwrap_or_else(|error| panic!("gateway CA: {error}"));
    pki.leaf("gateway-ca", "gateway", false)
        .unwrap_or_else(|error| panic!("gateway leaf: {error}"));
    let addresses = cluster::free_addresses(cluster::NODES)
        .unwrap_or_else(|error| panic!("addresses: {error}"));
    let nodes = addresses
        .iter()
        .enumerate()
        .map(|(index, address)| (format!("node-{}", index + 1), *address))
        .collect();
    let config = AttestorCustodyConfig::new(
        nodes,
        cluster::SIGNERS.iter().map(|id| (*id).to_owned()).collect(),
        pki.certificate_der("node-ca")
            .unwrap_or_else(|error| panic!("node CA DER: {error}")),
        pki.certificate_der("gateway")
            .unwrap_or_else(|error| panic!("gateway DER: {error}")),
        pki.key_der("gateway")
            .unwrap_or_else(|error| panic!("gateway key DER: {error}")),
        Duration::from_secs(5),
    )
    .unwrap_or_else(|error| panic!("attestor custody configuration: {error}"));
    let refused = AttestorKms::connect(config);
    assert!(
        matches!(refused, Err(CustodyError::Kms(KmsError::Unavailable))),
        "unreachable attestors were not refused: {refused:?}"
    );
    let _ = fs::remove_dir_all(&root);
}
