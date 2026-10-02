use std::collections::BTreeSet;
use std::fs;
use std::future::Future;
use std::path::PathBuf;
use std::pin::pin;
use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::{Arc, Mutex, Wake};
use std::task::{Context, Poll, Waker};

use layerx_agent_api::budget::{
    AuthorityDescription, AuthorityResponse, BudgetAuthorization, BudgetEnforcement, BudgetId,
    BudgetRecord, BudgetTarget, DaemonLimitView, ProtocolBudgetView, SignedBudgetMutation,
};
use layerx_agent_api::generated::{Amount as ApiAmount, BudgetLimit};
use layerx_agent_api::identity::{AgentDid, AuthorityRef, ContractError, TenantId as ApiTenantId};
use layerx_agent_api::submit::{PreparationRef, SignatureBytes};
use layerx_agent_api::verify::Level;
use layerx_agentd::budget::{
    budget_create_identity, core_expiry_ms, create_daemon_limit, daemon_limit_id, daemon_limits,
    hold_unknown, load_daemon_limits, rebuild, release_expired_core_time, reserve_until_core_time,
    revoke_daemon_limit, BudgetCreationError, BudgetLimiter, CoreTimestampMs, DaemonLimitError,
    DaemonLimitRecord, LimitConfig, LimitId, LimitRefusal, LimitScope, ProtocolBudgetRecord,
    ProtocolBudgetState, ReleaseKind, ReservationRequest, UnknownReservation,
};
use layerx_agentd::identity::{
    register, CoreIdentity, IdentityError, IdentityResolver, ProtocolAuthority,
};
use layerx_agentd::prepare::{
    prepare_activity_for_protocol, CorePreparationBoundary, CorePreparationState, CoreStateError,
    PreparationDefaults, PreparationLifecycle, PrepareError, PrepareRequest, Prepared,
};
use layerx_agentd::session::{open, OpenRequest, SessionId, SessionRegistry, Token};
use layerx_agentd::session_control::{
    AdmissionStage, OperationPermit, SessionControl, WriteAdmission, WriteCharge,
};
use layerx_agentd::sign::{attach_external_signature, verify_before_submit, VerifiedSubmission};
use layerx_agentd::store::{Store, TenantId};
use layerx_agentd::tenant::{Operation, Surface};
use layerx_crypto::local::LocalSigner;
use layerx_crypto::signer::{sign_disclosed, Signer};
use layerx_types::account::AccountId;
use layerx_types::activity::{Authority, TimestampBound};
use layerx_types::amount::Amount;
use layerx_types::ids::{Did, IdempotencyKey};
use layerx_types::payload::{ActivityType, ModuleId, ModuleRegistration, ModuleRegistry};
use layerx_types::verify::VerificationLevel;
use layerx_wire::encode::Encoder;
use layerx_wire::hash;

const ACTOR: &str = "did:layerx:alice";
const ENVELOPE_SEQUENCE: u64 = 5;
const CORE_TIME_MS: u64 = 1_000;
const NOT_AFTER_MS: u64 = 1_010;
const OBSERVED_HEAD_SEQUENCE: u64 = 88;
const HEAD_SEQUENCE_BOUND: u64 = 1_000;
const BUDGET_ID: [u8; 32] = [0x0b; 32];
const ASSET: [u8; 32] = [0x44; 32];
const PURPOSE: [u8; 32] = [0x54; 32];
const AGENT_DIGEST: [u8; 32] = [0x21; 32];
const PER_PERIOD_LIMIT: u128 = 5_000;
const INITIAL_AMOUNT: u128 = 1_200;
const PERIOD_LENGTH_MS: u64 = 60_000;
const PERIOD_START_MS: u64 = 900;
const EXPIRY_MS: u64 = 1_700_000_000_000;
const DAEMON_CEILING: u128 = 1_000;

static NEXT_DIRECTORY: AtomicU64 = AtomicU64::new(1);

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
        Poll::Pending => panic!("local signing unexpectedly blocked"),
    }
}

struct RecordedCore(CorePreparationState);

impl CorePreparationBoundary for RecordedCore {
    fn preparation_state(&mut self, _actor: &Did) -> Result<CorePreparationState, CoreStateError> {
        Ok(self.0.clone())
    }
}

struct BoundaryIdentity(CoreIdentity);

impl IdentityResolver for BoundaryIdentity {
    fn resolve(&mut self, _did: &Did) -> Result<Option<CoreIdentity>, IdentityError> {
        Ok(Some(self.0.clone()))
    }
}

fn text<T, E: std::fmt::Debug>(result: Result<T, E>, label: &str) -> T {
    match result {
        Ok(value) => value,
        Err(error) => panic!("{label} must be valid: {error:?}"),
    }
}

struct Root(PathBuf);

impl Root {
    fn new(name: &str) -> Self {
        let sequence = NEXT_DIRECTORY.fetch_add(1, Ordering::Relaxed);
        Self(std::env::temp_dir().join(format!(
            "layerx-agentd-budget-{name}-{}-{sequence}",
            std::process::id()
        )))
    }
}

impl Drop for Root {
    fn drop(&mut self) {
        let _ = fs::remove_dir_all(&self.0);
    }
}

fn tenant() -> TenantId {
    text(TenantId::new("tenant-a"), "tenant")
}

fn hex(value: &[u8; 32]) -> String {
    value.iter().map(|byte| format!("{byte:02x}")).collect()
}

fn account(name: &str) -> [u8; 32] {
    text(
        hash::account_id_for_protocol(&text(AccountId::parse(name), "account name"), 3),
        "account id",
    )
}

fn main_account() -> [u8; 32] {
    account(&format!("agent:{ACTOR}:main"))
}

fn asset_account() -> [u8; 32] {
    account(&format!("agent:{ACTOR}:asset:{}", hex(&ASSET)))
}

fn budget_account() -> [u8; 32] {
    account(&format!("agent:{ACTOR}:budget:{}", hex(&BUDGET_ID)))
}

fn create_kind() -> ActivityType {
    text(ActivityType::new(ModuleId::Budget, 1), "create activity")
}

fn send_kind() -> ActivityType {
    text(ActivityType::new(ModuleId::Asset, 5), "send activity")
}

fn registry() -> ModuleRegistry {
    text(
        ModuleRegistry::new(&[
            text(
                ModuleRegistration::new(ModuleId::Asset, &[send_kind()]),
                "asset",
            ),
            text(
                ModuleRegistration::new(ModuleId::Budget, &[create_kind()]),
                "budget",
            ),
        ]),
        "registry",
    )
}

fn core_create(source: Option<([u8; 32], u64)>) -> Vec<u8> {
    let mut bytes = Vec::with_capacity(251);
    bytes.extend_from_slice(&(if source.is_some() { 2_u16 } else { 1 }).to_be_bytes());
    bytes.extend_from_slice(&BUDGET_ID);
    bytes.extend_from_slice(&budget_account());
    bytes.extend_from_slice(&ASSET);
    bytes.extend_from_slice(&PURPOSE);
    bytes.extend_from_slice(&PER_PERIOD_LIMIT.to_be_bytes());
    bytes.extend_from_slice(&0_u128.to_be_bytes());
    bytes.extend_from_slice(&INITIAL_AMOUNT.to_be_bytes());
    bytes.extend_from_slice(&PERIOD_LENGTH_MS.to_be_bytes());
    bytes.extend_from_slice(&PERIOD_START_MS.to_be_bytes());
    bytes.extend_from_slice(&EXPIRY_MS.to_be_bytes());
    bytes.extend_from_slice(&3_u64.to_be_bytes());
    bytes.push(1);
    if let Some((account, sequence)) = source {
        bytes.extend_from_slice(&account);
        bytes.extend_from_slice(&sequence.to_be_bytes());
    }
    bytes
}

fn legacy_create() -> Vec<u8> {
    let mut encoder = Encoder::new(512);
    text(encoder.u16(0x4201), "tag");
    text(encoder.u16(10), "fields");
    for fixed in [BUDGET_ID, main_account(), budget_account(), ASSET] {
        text(encoder.fixed(&fixed), "fixed");
    }
    text(encoder.u128(PER_PERIOD_LIMIT), "limit");
    text(encoder.u64(PERIOD_LENGTH_MS), "period");
    text(encoder.u8(1), "rollover");
    text(encoder.u128(0), "carry cap");
    text(encoder.fixed(&PURPOSE), "purpose");
    text(encoder.u64(EXPIRY_MS), "expiry");
    encoder.finish()
}

fn prepare(
    signer: &LocalSigner,
    activity_type: ActivityType,
    payload: Vec<u8>,
) -> Result<Prepared, PrepareError> {
    let mut core = RecordedCore(CorePreparationState {
        network_id: 17,
        account_sequence: ENVELOPE_SEQUENCE,
        protocol_timestamp: CORE_TIME_MS,
        observed_head_sequence: OBSERVED_HEAD_SEQUENCE,
        module_registry: registry(),
    });
    prepare_activity_for_protocol(
        &mut core,
        PreparationDefaults {
            timestamp_span: 30,
            fee_limit: Amount::from_u128(12),
            maximum_payload_bytes: 1_024,
        },
        PrepareRequest {
            actor: text(Did::new(ACTOR.as_bytes()), "DID"),
            authority: text(Authority::owner(&signer.public_key()), "authority"),
            activity_type,
            expected_account_sequence: Some(ENVELOPE_SEQUENCE),
            timestamp_bound: Some(text(TimestampBound::new(995, NOT_AFTER_MS), "timestamp")),
            fee_limit: Some(Amount::from_u128(7)),
            idempotency_key: IdempotencyKey::new([4; 32]),
            payload,
            declared_payload_limit: 1_024,
        },
        3,
    )
}

fn signed_create(source: Option<([u8; 32], u64)>) -> (Prepared, VerifiedSubmission) {
    let signer = LocalSigner::new([0xa5; 32]);
    let prepared = text(
        prepare(&signer, create_kind(), core_create(source)),
        "prepare",
    );
    let signature = text(
        ready(sign_disclosed(
            &signer,
            &prepared.canonical_bytes,
            &prepared.disclosure,
            &registry(),
        )),
        "sign",
    );
    let signed = text(
        attach_external_signature(&prepared, *signature.as_bytes()),
        "attach",
    );
    let verified = text(
        verify_before_submit(&signed, &prepared, &signer.public_key(), &registry()),
        "verify",
    );
    (prepared, verified)
}

fn core_record(spent_this_period: u128, revoked: bool) -> Vec<u8> {
    let mut bytes = vec![0_u8; 278];
    bytes[1] = 1;
    bytes[2..34].copy_from_slice(&BUDGET_ID);
    bytes[34..66].copy_from_slice(&main_account());
    bytes[66..98].copy_from_slice(&budget_account());
    bytes[98..130].copy_from_slice(&ASSET);
    bytes[130..162].copy_from_slice(&PURPOSE);
    bytes[162..178].copy_from_slice(&PER_PERIOD_LIMIT.to_be_bytes());
    bytes[178..194].copy_from_slice(&PER_PERIOD_LIMIT.to_be_bytes());
    bytes[210..226].copy_from_slice(&spent_this_period.to_be_bytes());
    bytes[242..250].copy_from_slice(&PERIOD_LENGTH_MS.to_be_bytes());
    bytes[250..258].copy_from_slice(&PERIOD_START_MS.to_be_bytes());
    bytes[258..266].copy_from_slice(&EXPIRY_MS.to_be_bytes());
    bytes[266..274].copy_from_slice(&3_u64.to_be_bytes());
    bytes[274] = 1;
    bytes[276] = u8::from(revoked);
    bytes
}

fn daemon_record(budget_id: [u8; 32], expiry_ms: u64, mutation_key: [u8; 32]) -> DaemonLimitRecord {
    DaemonLimitRecord {
        tenant: tenant(),
        budget_id,
        limit_id: daemon_limit_id(budget_id),
        agent_digest: AGENT_DIGEST,
        asset: ASSET,
        ceiling: DAEMON_CEILING,
        consumed: 0,
        expiry_ms,
        revoked: false,
        mutation_key,
        body_digest: [0xbd; 32],
        revoke_key: [0; 32],
    }
}

fn hold_request(id: u8, amount: u128, limit: LimitId) -> ReservationRequest {
    ReservationRequest {
        id: [id; 32],
        amount,
        expiry_sequence: HEAD_SEQUENCE_BOUND,
        current_sequence: OBSERVED_HEAD_SEQUENCE,
        applicable_limits: vec![limit],
    }
}

fn send_prepared() -> Prepared {
    let signer = LocalSigner::new([0xa5; 32]);
    let mut encoder = Encoder::new(512);
    text(encoder.u16(0x5301), "tag");
    text(encoder.u16(10), "fields");
    text(encoder.fixed(&[0x11; 32]), "from");
    text(encoder.fixed(&[0x22; 32]), "to");
    text(encoder.fixed(&[0x33; 32]), "asset");
    text(encoder.u128(25), "amount");
    text(encoder.u64(ENVELOPE_SEQUENCE), "sequence");
    text(encoder.fixed(&[4; 32]), "idempotency");
    text(encoder.u64(NOT_AFTER_MS), "expiry");
    text(encoder.fixed(&[0x55; 32]), "context");
    text(encoder.u8(0), "conditions");
    text(encoder.u8(1), "authority kind");
    text(encoder.fixed(&[0x11; 32]), "controller");
    text(encoder.fixed(&[0x66; 32]), "payload key");
    text(encoder.fixed(&[0x77; 64]), "payload signature");
    text(encoder.fixed(&[0x55; 32]), "signed context");
    text(encoder.u32(17), "network");
    text(
        encoder.u16(layerx_wire::limits::PROTOCOL_VERSION),
        "version",
    );
    let payload = support::send_authorization::sign(encoder.finish());
    let mut core = RecordedCore(CorePreparationState {
        network_id: 17,
        account_sequence: ENVELOPE_SEQUENCE,
        protocol_timestamp: CORE_TIME_MS,
        observed_head_sequence: OBSERVED_HEAD_SEQUENCE,
        module_registry: registry(),
    });
    text(
        layerx_agentd::prepare::prepare_activity(
            &mut core,
            PreparationDefaults {
                timestamp_span: 30,
                fee_limit: Amount::from_u128(12),
                maximum_payload_bytes: 1_024,
            },
            PrepareRequest {
                actor: text(Did::new(b"did:layerx:budget-suite"), "DID"),
                authority: text(Authority::owner(&signer.public_key()), "authority"),
                activity_type: send_kind(),
                expected_account_sequence: Some(ENVELOPE_SEQUENCE),
                timestamp_bound: Some(text(TimestampBound::new(995, NOT_AFTER_MS), "timestamp")),
                fee_limit: Some(Amount::from_u128(7)),
                idempotency_key: IdempotencyKey::new([4; 32]),
                payload,
                declared_payload_limit: 1_024,
            },
        ),
        "prepare send",
    )
}

fn opened_session(store: &mut Store, sessions: &mut SessionRegistry) -> Token {
    let mut boundary = BoundaryIdentity(CoreIdentity {
        canonical_bytes: b"budget-suite-identity".to_vec(),
        head_sequence: 10,
        revocation_sequence: 1,
        verification_level: VerificationLevel::STATE_PROVEN,
        frozen: false,
        authorities: vec![ProtocolAuthority::SessionKey([4; 32])],
    });
    let identity = text(
        register(
            store,
            tenant(),
            text(Did::new(b"agent-a"), "agent DID"),
            &mut boundary,
        ),
        "identity",
    );
    text(
        open(
            store,
            sessions,
            &identity,
            OpenRequest {
                session_id: SessionId([1; 32]),
                token_id: [2; 32],
                tenant: tenant(),
                agent: text(Did::new(b"agent-a"), "agent DID"),
                authority: ProtocolAuthority::SessionKey([4; 32]),
                permitted_activity_types: BTreeSet::from([5]),
                scopes: BTreeSet::from(["prepare".to_owned(), "write".to_owned()]),
                expiry_sequence: HEAD_SEQUENCE_BOUND,
                expiry_seconds: None,
                opening_client: "budget-suite".to_owned(),
                policy_version: "policy-v1".to_owned(),
            },
            10,
        ),
        "session",
    )
}

/// A real session-controlled daemon whose single daemon limit was installed through the
/// durable registry before the controller was built.
struct Harness {
    control: SessionControl,
    limiter: Arc<BudgetLimiter>,
    token: Token,
    limit: LimitId,
    prepared: Prepared,
}

impl Harness {
    fn new(root: &Root) -> Self {
        let mut store = text(Store::open(&root.0), "store");
        let limiter = Arc::new(text(BudgetLimiter::new(Vec::new()), "limiter"));
        let created = text(
            create_daemon_limit(
                &mut store,
                &limiter,
                daemon_record(BUDGET_ID, EXPIRY_MS, [0x31; 32]),
                CoreTimestampMs(CORE_TIME_MS),
            ),
            "daemon limit",
        );
        let mut sessions = SessionRegistry::default();
        let token = opened_session(&mut store, &mut sessions);
        let control = SessionControl::new(
            Arc::new(Mutex::new(store)),
            sessions,
            Arc::new(PreparationLifecycle::default()),
            Arc::clone(&limiter),
        );
        Self {
            control,
            limiter,
            token,
            limit: created.limit_id,
            prepared: send_prepared(),
        }
    }

    fn permit(&self, operation: Operation) -> OperationPermit {
        text(
            self.control.authorize(
                &self.token.credential(),
                operation,
                Surface::Contract,
                OBSERVED_HEAD_SEQUENCE,
                None,
            ),
            "permit",
        )
    }

    fn admit(&self, preparation_id: u8, amount: u128) -> bool {
        self.permit(Operation::Prepare)
            .admit_write(
                &self.control,
                WriteAdmission {
                    stage: AdmissionStage::Prepare {
                        prepared: &self.prepared,
                    },
                    preparation_id: [preparation_id; 32],
                    charge: Some(WriteCharge {
                        amount,
                        applicable_limits: vec![self.limit],
                        head_sequence_bound: HEAD_SEQUENCE_BOUND,
                        core_deadline_ms: Some(CoreTimestampMs(NOT_AFTER_MS)),
                    }),
                    extensions: Vec::new(),
                    current_sequence: OBSERVED_HEAD_SEQUENCE,
                    core_time_ms: CORE_TIME_MS,
                },
            )
            .is_ok()
    }

    fn held(&self, preparation_id: u8) -> bool {
        text(self.limiter.has_reservation([preparation_id; 32]), "held")
    }

    fn consumed(&self) -> u128 {
        text(self.limiter.consumed(self.limit), "consumed")
    }
}

#[test]
fn core_create_identity_is_read_from_the_signed_v1_and_v2_bytes() {
    let (prepared, v1) = signed_create(None);
    let identity = text(
        budget_create_identity(v1.exact_bytes(), &registry()),
        "v1 identity",
    );
    assert_eq!(identity.encoding_version, 1);
    assert_eq!(identity.owner, main_account());
    assert_eq!(identity.budget_id, BUDGET_ID);
    assert_eq!(identity.budget_account, budget_account());
    assert_eq!(identity.asset, ASSET);
    assert_eq!(identity.purpose, PURPOSE);
    assert_eq!(identity.per_period_limit, PER_PERIOD_LIMIT);
    assert_eq!(identity.carry_cap, 0);
    assert_eq!(identity.initial_amount, INITIAL_AMOUNT);
    assert_eq!(identity.period_length_ms, PERIOD_LENGTH_MS);
    assert_eq!(identity.period_start_ms, PERIOD_START_MS);
    assert_eq!(identity.expiry_ms, EXPIRY_MS);
    assert_eq!(identity.revocation_sequence, 3);
    assert_eq!(identity.rollover, 1);
    assert_eq!(identity.source_account, main_account());
    assert_eq!(identity.source_sequence, ENVELOPE_SEQUENCE);
    assert_eq!(
        budget_create_identity(&prepared.canonical_bytes, &registry()),
        Err(BudgetCreationError::NotBudgetCreation)
    );

    let (_, v2) = signed_create(Some((asset_account(), ENVELOPE_SEQUENCE)));
    let identity = text(
        budget_create_identity(v2.exact_bytes(), &registry()),
        "v2 identity",
    );
    assert_eq!(identity.encoding_version, 2);
    assert_eq!(identity.source_account, asset_account());
    assert_eq!(identity.source_sequence, ENVELOPE_SEQUENCE);
    assert_eq!(identity.expiry_ms, EXPIRY_MS);
}

#[test]
fn legacy_0x4201_create_cannot_be_prepared_or_identified() {
    let signer = LocalSigner::new([0xa5; 32]);
    assert!(matches!(
        prepare(&signer, create_kind(), legacy_create()),
        Err(PrepareError::Disclosure(_))
    ));
    let mut truncated = core_create(None);
    truncated.pop();
    assert!(matches!(
        prepare(&signer, create_kind(), truncated),
        Err(PrepareError::Disclosure(_))
    ));
    let mut trailing = core_create(Some((main_account(), ENVELOPE_SEQUENCE)));
    trailing.push(0);
    assert!(matches!(
        prepare(&signer, create_kind(), trailing),
        Err(PrepareError::Disclosure(_))
    ));
}

#[test]
fn public_seconds_reach_core_milliseconds_only_through_checked_multiplication() {
    assert_eq!(core_expiry_ms(1_700_000_000), Ok(EXPIRY_MS));
    assert_eq!(core_expiry_ms(1), Ok(1_000));
    let largest = u64::MAX / 1_000;
    assert_eq!(core_expiry_ms(largest), Ok(largest * 1_000));
    assert_eq!(
        core_expiry_ms(largest + 1),
        Err(BudgetCreationError::InvalidLimit)
    );
    assert_eq!(
        core_expiry_ms(u64::MAX),
        Err(BudgetCreationError::InvalidLimit)
    );
    assert_eq!(core_expiry_ms(0), Err(BudgetCreationError::InvalidLimit));
}

#[test]
fn core_time_reservation_treats_deadline_equality_as_expired() {
    let limit = LimitId([7; 16]);
    let limiter = text(
        BudgetLimiter::new(vec![LimitConfig {
            id: limit,
            name: "agent-limit".to_owned(),
            scope: LimitScope::Agent(AGENT_DIGEST),
            ceiling: DAEMON_CEILING,
            consumed: 0,
        }]),
        "limiter",
    );
    assert_eq!(
        reserve_until_core_time(
            &limiter,
            &hold_request(1, 100, limit),
            CoreTimestampMs(CORE_TIME_MS),
            CoreTimestampMs(CORE_TIME_MS),
        )
        .err(),
        Some(LimitRefusal::InvalidRequest)
    );
    assert_eq!(limiter.held_reservations(), Ok(0));
    text(
        reserve_until_core_time(
            &limiter,
            &hold_request(1, 100, limit),
            CoreTimestampMs(CORE_TIME_MS + 1),
            CoreTimestampMs(CORE_TIME_MS),
        ),
        "live reservation",
    );
    assert_eq!(
        release_expired_core_time(&limiter, [1; 32], CoreTimestampMs(CORE_TIME_MS)),
        Ok(false)
    );
    assert_eq!(limiter.has_reservation([1; 32]), Ok(true));
    assert_eq!(
        release_expired_core_time(&limiter, [1; 32], CoreTimestampMs(CORE_TIME_MS + 1)),
        Ok(true)
    );
    assert_eq!(limiter.has_reservation([1; 32]), Ok(false));
    assert_eq!(limiter.consumed(limit), Ok(0));
}

#[test]
fn daemon_limit_expiry_equality_is_expired_and_a_later_expiry_is_live() {
    let root = Root::new("expiry");
    let mut store = text(Store::open(&root.0), "store");
    let limiter = text(BudgetLimiter::new(Vec::new()), "limiter");
    assert!(matches!(
        create_daemon_limit(
            &mut store,
            &limiter,
            daemon_record(BUDGET_ID, CORE_TIME_MS, [0x31; 32]),
            CoreTimestampMs(CORE_TIME_MS),
        ),
        Err(DaemonLimitError::Expired)
    ));
    assert!(matches!(
        limiter.consumed(daemon_limit_id(BUDGET_ID)),
        Err(LimitRefusal::UnknownLimit(_))
    ));
    let created = text(
        create_daemon_limit(
            &mut store,
            &limiter,
            daemon_record(BUDGET_ID, CORE_TIME_MS + 1, [0x31; 32]),
            CoreTimestampMs(CORE_TIME_MS),
        ),
        "live limit",
    );
    assert_eq!(created.expiry_ms, CORE_TIME_MS + 1);
    assert_eq!(created.limit_id, daemon_limit_id(BUDGET_ID));
    assert_eq!(limiter.consumed(created.limit_id), Ok(0));
}

#[test]
fn daemon_limit_creation_replays_exactly_and_refuses_changed_bodies_and_new_keys() {
    let root = Root::new("replay");
    let mut store = text(Store::open(&root.0), "store");
    let limiter = text(BudgetLimiter::new(Vec::new()), "limiter");
    let first = text(
        create_daemon_limit(
            &mut store,
            &limiter,
            daemon_record(BUDGET_ID, EXPIRY_MS, [0x31; 32]),
            CoreTimestampMs(CORE_TIME_MS),
        ),
        "create",
    );
    let replay = text(
        create_daemon_limit(
            &mut store,
            &limiter,
            daemon_record(BUDGET_ID, EXPIRY_MS, [0x31; 32]),
            CoreTimestampMs(CORE_TIME_MS),
        ),
        "replay",
    );
    assert_eq!(replay, first);
    let mut changed = daemon_record(BUDGET_ID, EXPIRY_MS, [0x31; 32]);
    changed.body_digest = [0xbe; 32];
    changed.ceiling = DAEMON_CEILING + 1;
    assert!(matches!(
        create_daemon_limit(&mut store, &limiter, changed, CoreTimestampMs(CORE_TIME_MS)),
        Err(DaemonLimitError::Conflict)
    ));
    assert!(matches!(
        create_daemon_limit(
            &mut store,
            &limiter,
            daemon_record(BUDGET_ID, EXPIRY_MS, [0x32; 32]),
            CoreTimestampMs(CORE_TIME_MS),
        ),
        Err(DaemonLimitError::Conflict)
    ));
    let mut zero = daemon_record([0; 32], EXPIRY_MS, [0x33; 32]);
    zero.limit_id = daemon_limit_id([0; 32]);
    assert!(matches!(
        create_daemon_limit(&mut store, &limiter, zero, CoreTimestampMs(CORE_TIME_MS)),
        Err(DaemonLimitError::Invalid)
    ));
    let mut unbounded = daemon_record([0x0c; 32], EXPIRY_MS, [0x34; 32]);
    unbounded.ceiling = 0;
    assert!(matches!(
        create_daemon_limit(
            &mut store,
            &limiter,
            unbounded,
            CoreTimestampMs(CORE_TIME_MS)
        ),
        Err(DaemonLimitError::Invalid)
    ));
    assert_eq!(limiter.consumed(first.limit_id), Ok(0));
}

#[test]
fn a_limit_id_already_bound_or_not_derived_from_its_budget_is_refused() {
    let root = Root::new("collision");
    let mut store = text(Store::open(&root.0), "store");
    let fresh = text(BudgetLimiter::new(Vec::new()), "fresh limiter");
    let mut mismatched = daemon_record([0x0c; 32], EXPIRY_MS, [0x35; 32]);
    mismatched.limit_id = daemon_limit_id(BUDGET_ID);
    assert!(matches!(
        create_daemon_limit(
            &mut store,
            &fresh,
            mismatched,
            CoreTimestampMs(CORE_TIME_MS)
        ),
        Err(DaemonLimitError::Invalid)
    ));
    assert!(matches!(
        fresh.consumed(daemon_limit_id(BUDGET_ID)),
        Err(LimitRefusal::UnknownLimit(_))
    ));
    assert_eq!(text(daemon_limits(&store, &tenant()), "limits").len(), 0);

    let colliding = daemon_limit_id(BUDGET_ID);
    let limiter = text(
        BudgetLimiter::new(vec![LimitConfig {
            id: colliding,
            name: "enrolment-limit".to_owned(),
            scope: LimitScope::Tenant([1; 32]),
            ceiling: 10,
            consumed: 0,
        }]),
        "limiter",
    );
    assert!(matches!(
        create_daemon_limit(
            &mut store,
            &limiter,
            daemon_record(BUDGET_ID, EXPIRY_MS, [0x31; 32]),
            CoreTimestampMs(CORE_TIME_MS),
        ),
        Err(DaemonLimitError::Limit(LimitRefusal::InvalidConfiguration))
    ));
    let held = text(
        reserve_until_core_time(
            &limiter,
            &hold_request(1, 10, colliding),
            CoreTimestampMs(NOT_AFTER_MS),
            CoreTimestampMs(CORE_TIME_MS),
        ),
        "enrolment limit keeps its own ceiling",
    );
    assert_eq!(held.applied_limits, vec![colliding]);
    assert!(matches!(
        reserve_until_core_time(
            &limiter,
            &hold_request(2, 1, colliding),
            CoreTimestampMs(NOT_AFTER_MS),
            CoreTimestampMs(CORE_TIME_MS),
        ),
        Err(LimitRefusal::Exceeded { ceiling: 10, .. })
    ));
}

#[test]
fn revoked_daemon_limit_blocks_new_holds_and_its_tombstone_survives_restart() {
    let root = Root::new("revoke");
    {
        let mut store = text(Store::open(&root.0), "store");
        let limiter = text(BudgetLimiter::new(Vec::new()), "limiter");
        let created = text(
            create_daemon_limit(
                &mut store,
                &limiter,
                daemon_record(BUDGET_ID, EXPIRY_MS, [0x31; 32]),
                CoreTimestampMs(CORE_TIME_MS),
            ),
            "create",
        );
        let revoked = text(
            revoke_daemon_limit(&mut store, &limiter, &tenant(), BUDGET_ID, [0x41; 32]),
            "revoke",
        );
        assert!(revoked.revoked);
        assert_eq!(revoked.budget_id, BUDGET_ID);
        assert_eq!(limiter.is_retired(created.limit_id), Ok(true));
        assert_eq!(
            reserve_until_core_time(
                &limiter,
                &hold_request(1, 10, created.limit_id),
                CoreTimestampMs(NOT_AFTER_MS),
                CoreTimestampMs(CORE_TIME_MS),
            )
            .err(),
            Some(LimitRefusal::Retired(created.limit_id))
        );
        assert_eq!(limiter.held_reservations(), Ok(0));
        let replay = text(
            revoke_daemon_limit(&mut store, &limiter, &tenant(), BUDGET_ID, [0x41; 32]),
            "revoke replay",
        );
        assert_eq!(replay, revoked);
        assert!(matches!(
            revoke_daemon_limit(&mut store, &limiter, &tenant(), BUDGET_ID, [0x42; 32]),
            Err(DaemonLimitError::Revoked)
        ));
        assert!(matches!(
            create_daemon_limit(
                &mut store,
                &limiter,
                daemon_record(BUDGET_ID, EXPIRY_MS, [0x36; 32]),
                CoreTimestampMs(CORE_TIME_MS),
            ),
            Err(DaemonLimitError::Conflict)
        ));
    }
    let store = text(Store::open(&root.0), "reopened store");
    let limiter = text(BudgetLimiter::new(Vec::new()), "restarted limiter");
    let loaded = text(load_daemon_limits(&store, &limiter), "load");
    assert_eq!(loaded.len(), 1);
    assert!(loaded[0].revoked);
    assert_eq!(loaded[0].budget_id, BUDGET_ID);
    assert_eq!(limiter.is_retired(daemon_limit_id(BUDGET_ID)), Ok(true));
    assert_eq!(
        reserve_until_core_time(
            &limiter,
            &hold_request(2, 10, daemon_limit_id(BUDGET_ID)),
            CoreTimestampMs(NOT_AFTER_MS),
            CoreTimestampMs(CORE_TIME_MS),
        )
        .err(),
        Some(LimitRefusal::Retired(daemon_limit_id(BUDGET_ID)))
    );
}

#[test]
fn write_admission_binds_holds_and_receipts_settle_each_charge_exactly_once() {
    let root = Root::new("admission");
    let harness = Harness::new(&root);

    assert!(harness.admit(1, 300));
    assert!(harness.held(1));
    assert!(
        !harness.admit(1, 300),
        "the same preparation must not be charged twice"
    );
    assert_eq!(harness.limiter.held_reservations(), Ok(1));
    assert!(
        !harness.admit(2, 701),
        "consumed plus held plus request passes the ceiling"
    );
    assert!(!harness.held(2));
    assert!(harness.admit(2, 200));

    assert_eq!(
        harness
            .control
            .settle_write(&tenant(), [1; 32], ReleaseKind::Executed, 89)
            .ok(),
        Some(true)
    );
    assert_eq!(harness.consumed(), 300);
    assert!(!harness.held(1));
    assert_eq!(
        harness
            .control
            .settle_write(&tenant(), [1; 32], ReleaseKind::Executed, 90)
            .ok(),
        Some(false)
    );
    assert_eq!(
        harness.consumed(),
        300,
        "a repeated receipt must not consume again"
    );

    assert_eq!(
        harness
            .control
            .settle_write(&tenant(), [2; 32], ReleaseKind::Unknown, 89)
            .ok(),
        Some(false)
    );
    assert!(harness.held(2), "an unknown outcome keeps its hold");
    assert_eq!(
        harness
            .control
            .settle_write(&tenant(), [2; 32], ReleaseKind::Failed, 89)
            .ok(),
        Some(true)
    );
    assert!(!harness.held(2));
    assert_eq!(
        harness.consumed(),
        300,
        "a failed outcome releases without consumption"
    );

    assert!(!harness.admit(3, 701));
    assert!(harness.admit(3, 700));
    assert_eq!(harness.limiter.held_reservations(), Ok(1));
}

#[test]
fn restart_restores_unsettled_holds_once_consumed_totals_and_unknown_accounting() {
    let root = Root::new("restart");
    {
        let harness = Harness::new(&root);
        assert!(harness.admit(1, 300));
        assert!(harness.admit(2, 250));
        assert_eq!(
            harness
                .control
                .settle_write(&tenant(), [1; 32], ReleaseKind::Executed, 89)
                .ok(),
            Some(true)
        );
        assert_eq!(
            harness
                .control
                .settle_write(&tenant(), [2; 32], ReleaseKind::Unknown, 89)
                .ok(),
            Some(false)
        );
        let store = harness.control.store();
        let mut store = store
            .lock()
            .unwrap_or_else(|_| panic!("store lock poisoned"));
        text(
            hold_unknown(
                &mut store,
                &UnknownReservation {
                    tenant: tenant(),
                    id: [2; 32],
                    amount: 250,
                    expiry_sequence: HEAD_SEQUENCE_BOUND,
                    resolved: None,
                },
            ),
            "hold unknown",
        );
    }

    let store = text(Store::open(&root.0), "reopened store");
    let limiter = Arc::new(text(BudgetLimiter::new(Vec::new()), "restarted limiter"));
    let loaded = text(load_daemon_limits(&store, &limiter), "load limits");
    assert_eq!(loaded.len(), 1);
    assert_eq!(loaded[0].consumed, 300);
    assert_eq!(limiter.consumed(daemon_limit_id(BUDGET_ID)), Ok(300));

    let accounting = text(
        rebuild(
            &store,
            &tenant(),
            &[[2; 32]],
            &[],
            &ProtocolBudgetState {
                evidence: support::raw_state_leaf(core_record(250, false), 50),
            },
            &support::evidence_verifier(),
        ),
        "restart accounting",
    );
    assert_eq!(accounting.held_unresolved, 250);
    assert_eq!(accounting.unresolved_count, 1);
    assert_eq!(accounting.protocol_consumed, Some(250));
    assert!(accounting.reconciled);
    assert!(accounting.require_write_ready().is_ok());

    let control = SessionControl::new(
        Arc::new(Mutex::new(store)),
        SessionRegistry::default(),
        Arc::new(PreparationLifecycle::default()),
        Arc::clone(&limiter),
    );
    assert_eq!(control.restore_writes().ok(), Some(1));
    assert_eq!(limiter.has_reservation([2; 32]), Ok(true));
    assert_eq!(limiter.has_reservation([1; 32]), Ok(false));
    assert_eq!(limiter.held_reservations(), Ok(1));
    assert_eq!(limiter.consumed(daemon_limit_id(BUDGET_ID)), Ok(300));
    assert_eq!(
        control
            .settle_write(&tenant(), [1; 32], ReleaseKind::Executed, 91)
            .ok(),
        Some(false)
    );
    assert_eq!(limiter.consumed(daemon_limit_id(BUDGET_ID)), Ok(300));
}

#[test]
fn authority_response_carries_exact_authority_and_truthful_enforcement() {
    let owner = LocalSigner::new([0xa5; 32]).public_key();
    let api_tenant = text(ApiTenantId::new("tenant-a"), "tenant");
    let agent = text(AgentDid::new(ACTOR), "agent");
    let reference = text(AuthorityRef::new(hex(&owner)), "authority ref");
    assert_eq!(
        AuthorityDescription::new(
            api_tenant.clone(),
            agent.clone(),
            reference.clone(),
            Vec::new()
        ),
        Err(ContractError::Empty("protocol_authority"))
    );
    let authority = text(
        AuthorityDescription::new(api_tenant.clone(), agent.clone(), reference, owner.to_vec()),
        "authority",
    );

    let record = text(
        ProtocolBudgetRecord::decode(&core_record(0, false)),
        "record",
    );
    assert_eq!(
        ProtocolBudgetView::from_proven(&record, Level::BatchIncluded),
        Err(ContractError::OutOfRange("budget_verification_level"))
    );
    let view = text(
        ProtocolBudgetView::from_proven(&record, Level::StateProven),
        "view",
    );
    assert_eq!(view.expiry_ms, EXPIRY_MS);
    assert_eq!(view.period_length_ms, PERIOD_LENGTH_MS);
    assert_eq!(view.period_start_ms, PERIOD_START_MS);
    assert_eq!(view.owner, main_account());
    assert_eq!(view.per_period_limit, ApiAmount(PER_PERIOD_LIMIT));
    let response = AuthorityResponse {
        authority: authority.clone(),
        value: BudgetRecord::Protocol(view),
    };
    assert_eq!(response.authority.tenant, api_tenant);
    assert_eq!(response.authority.agent_did, agent);
    assert_eq!(response.authority.protocol_authority, owner.to_vec());
    assert_eq!(
        response.value.enforcement(),
        BudgetEnforcement::ProtocolBudget
    );

    let daemon = DaemonLimitView {
        budget_id: BUDGET_ID,
        asset: ASSET,
        ceiling: BudgetLimit(DAEMON_CEILING),
        consumed: ApiAmount(300),
        expiry_ms: EXPIRY_MS,
        revoked: false,
    };
    assert_eq!(daemon.notice(), BudgetEnforcement::DAEMON_LIMIT_NOTICE);
    let response = AuthorityResponse {
        authority,
        value: BudgetRecord::Daemon(daemon),
    };
    assert_eq!(response.value.enforcement(), BudgetEnforcement::DaemonLimit);

    let target = BudgetTarget {
        tenant: api_tenant,
        agent_did: agent,
        budget_id: text(BudgetId::new(hex(&BUDGET_ID)), "budget id"),
    };
    let unsigned = SignedBudgetMutation {
        request: target.clone(),
        authorization: None,
    };
    assert_eq!(
        unsigned.require_for(BudgetEnforcement::ProtocolBudget),
        Err(ContractError::Empty("budget_authorization"))
    );
    assert_eq!(unsigned.require_for(BudgetEnforcement::DaemonLimit), Ok(()));
    let signed = SignedBudgetMutation {
        request: target,
        authorization: Some(BudgetAuthorization {
            preparation_ref: text(PreparationRef::new(hex(&[0x0f; 32])), "preparation ref"),
            signature: text(SignatureBytes::new(vec![0x5a; 64]), "signature"),
            signer_public_key: Some(owner),
        }),
    };
    assert_eq!(
        signed.require_for(BudgetEnforcement::ProtocolBudget),
        Ok(())
    );
    assert_eq!(
        signed.require_for(BudgetEnforcement::DaemonLimit),
        Err(ContractError::Mismatch("budget_authorization"))
    );
}

mod support;
