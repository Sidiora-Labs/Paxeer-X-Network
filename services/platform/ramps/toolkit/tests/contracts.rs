use std::fs::{self, OpenOptions};
use std::io::Write as _;
use std::path::{Path, PathBuf};

use layerx_ramp_toolkit::journal::{
    CallbackIdentity, Journal, OrderSnapshot, PaxeerObservation, ProviderCallbackWrite, TornTail,
    TransitionEvidence, WorkflowStage, WriteFault, WriteStep,
};
use layerx_ramp_toolkit::{
    operator_send_authorization_message, AggregateStatus, AuthenticatedPrincipal, CreateOrder,
    OperatorIdentity, QuoteTerms, RampDirection, RampError, RampOrder, EXTERNAL_CUSTODY_LABEL,
};
use layerx_wire::limits::{LEGACY_PROTOCOL_VERSION, PROTOCOL_VERSION};

const WORKER: &str = "test-worker";
const CALLBACK_ID: &str = "provider-callback-1";
const CALLBACK_EVIDENCE_DIGEST: [u8; 32] = [9; 32];
const CALLBACK_AT: u64 = 1_003;

fn order(direction: RampDirection, customer: &str) -> RampOrder {
    let payer_grant = match direction {
        RampDirection::OnRamp => None,
        RampDirection::OffRamp => Some([7; 32]),
    };
    RampOrder::bind(
        CreateOrder {
            order_id: "order-1".to_owned(),
            quote_id: "quote-1".to_owned(),
            payer_grant,
        },
        QuoteTerms {
            quote_id: "quote-1".to_owned(),
            direction,
            layerx_asset: [1; 32],
            layerx_amount: 1_000,
            external_currency: "EUR".to_owned(),
            external_amount_minor: 100,
            rate_numerator: 10,
            rate_denominator: 1,
            fee_minor: 2,
            maximum_slippage_bps: 25,
            context: [8; 32],
            provider_token: "product-eur".to_owned(),
            payout_token: "beneficiary-123".to_owned(),
            expires_at: 2_000,
        },
        AuthenticatedPrincipal {
            principal_id: customer.to_owned(),
            account: format!("agent:did:layerx:{customer}:main"),
        },
        OperatorIdentity {
            principal_id: "operator-1".to_owned(),
            account: "agent:did:layerx:operator-1:main".to_owned(),
            signer_key_handle: "kms.operator-1.primary".to_owned(),
        },
        1_000,
    )
    .unwrap_or_else(|error| panic!("bind order: {error:?}"))
}

fn journal_path(name: &str) -> PathBuf {
    let mut path = std::env::temp_dir();
    path.push(format!("layerx-ramp-{name}-{}.jsonl", std::process::id()));
    let file = OpenOptions::new()
        .create(true)
        .write(true)
        .truncate(true)
        .open(&path)
        .unwrap_or_else(|error| panic!("create journal: {error}"));
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt as _;
        fs::set_permissions(&path, fs::Permissions::from_mode(0o600))
            .unwrap_or_else(|error| panic!("secure journal: {error}"));
    }
    drop(file);
    path
}

fn remove_journal(path: &Path) {
    fs::remove_file(path).unwrap_or_else(|error| panic!("remove journal: {error}"));
    let mut intent = path.as_os_str().to_owned();
    intent.push(".append-intent");
    match fs::remove_file(PathBuf::from(intent)) {
        Ok(()) => {}
        Err(error) if error.kind() == std::io::ErrorKind::NotFound => {}
        Err(error) => panic!("remove journal intent: {error}"),
    }
    #[cfg(unix)]
    {
        let mut lock = path.as_os_str().to_owned();
        lock.push(".writer-lock");
        fs::remove_file(PathBuf::from(lock))
            .unwrap_or_else(|error| panic!("remove journal lock: {error}"));
    }
}

fn open_journal(path: &Path) -> Journal {
    Journal::open(path).unwrap_or_else(|error| panic!("open journal: {error:?}"))
}

fn journal_bytes(path: &Path) -> Vec<u8> {
    fs::read(path).unwrap_or_else(|error| panic!("read journal: {error}"))
}

fn append_raw(path: &Path, bytes: &[u8]) {
    let mut file = OpenOptions::new()
        .append(true)
        .open(path)
        .unwrap_or_else(|error| panic!("open journal for raw append: {error}"));
    file.write_all(bytes)
        .unwrap_or_else(|error| panic!("raw append: {error}"));
    file.sync_all()
        .unwrap_or_else(|error| panic!("sync raw append: {error}"));
}

fn seeded_journal(path: &Path) -> (Journal, [u8; 32]) {
    let bound = order(RampDirection::OnRamp, "customer-a");
    let digest = bound.order_digest;
    let mut journal = open_journal(path);
    journal
        .create_order(bound, 1_001)
        .unwrap_or_else(|error| panic!("create order: {error:?}"));
    journal
        .acquire_lease(digest, WORKER, 1_001, 30)
        .unwrap_or_else(|error| panic!("lease: {error:?}"));
    journal
        .transition(
            digest,
            WorkflowStage::CompliancePending,
            WorkflowStage::AwaitingExternalCredit,
            TransitionEvidence::empty(),
            WORKER,
            1_002,
        )
        .unwrap_or_else(|error| panic!("approve: {error:?}"));
    (journal, digest)
}

fn settled_evidence() -> TransitionEvidence {
    let mut evidence = TransitionEvidence::empty();
    evidence.provider_operation_id = Some("provider-op-1".to_owned());
    evidence.provider_evidence_digest = Some([3; 32]);
    evidence
}

fn callback_write<'a>(
    digest: [u8; 32],
    callback_id: &'a str,
    provider_sequence: u64,
    evidence_digest: [u8; 32],
    expected: WorkflowStage,
    next: WorkflowStage,
    evidence: &'a TransitionEvidence,
) -> ProviderCallbackWrite<'a> {
    ProviderCallbackWrite {
        order_digest: digest,
        callback_id,
        provider_sequence,
        evidence_digest,
        expected,
        next,
        evidence,
    }
}

fn settle_callback(journal: &mut Journal, digest: [u8; 32]) -> Result<bool, RampError> {
    let evidence = settled_evidence();
    journal.apply_provider_callback(
        callback_write(
            digest,
            CALLBACK_ID,
            1,
            CALLBACK_EVIDENCE_DIGEST,
            WorkflowStage::AwaitingExternalCredit,
            WorkflowStage::ProviderSettled,
            &evidence,
        ),
        CALLBACK_AT,
    )
}

fn settled_identity(digest: [u8; 32]) -> CallbackIdentity {
    CallbackIdentity {
        order_digest: digest,
        provider_sequence: 1,
        evidence_digest: CALLBACK_EVIDENCE_DIGEST,
    }
}

fn reference_journal(name: &str) -> (PathBuf, Journal) {
    let path = journal_path(name);
    let (mut journal, digest) = seeded_journal(&path);
    assert_eq!(settle_callback(&mut journal, digest), Ok(true));
    (path, journal)
}

fn assert_same_state(journal: &Journal, reference: &Journal, step: WriteStep) {
    assert_eq!(journal.projection(), reference.projection(), "{step:?}");
    assert_eq!(journal.head(), reference.head(), "{step:?}");
    assert_eq!(journal.record_count(), reference.record_count(), "{step:?}");
}

#[test]
fn digest_binds_authenticated_customer_and_direction() {
    let first = order(RampDirection::OnRamp, "customer-a");
    let another_customer = order(RampDirection::OnRamp, "customer-b");
    let opposite_direction = order(RampDirection::OffRamp, "customer-a");
    assert_ne!(first.order_digest, another_customer.order_digest);
    assert_ne!(first.order_digest, opposite_direction.order_digest);
    assert_eq!(first.context, [8; 32]);
}

#[test]
fn payment_direction_selects_direct_send_or_customer_grant() {
    let on_ramp = order(RampDirection::OnRamp, "customer-a");
    let off_ramp = order(RampDirection::OffRamp, "customer-a");
    assert_eq!(on_ramp.payer_grant, None);
    assert_eq!(off_ramp.payer_grant, Some([7; 32]));
    let authorization = operator_send_authorization_message(&on_ramp, 9, 1, PROTOCOL_VERSION)
        .unwrap_or_else(|error| panic!("authorization message: {error:?}"));
    assert_eq!(authorization.len(), 266);
    assert_eq!(&authorization[..2], &0x5301_u16.to_be_bytes());
    assert_eq!(&authorization[264..], &PROTOCOL_VERSION.to_be_bytes());
    assert_eq!(
        operator_send_authorization_message(&on_ramp, 9, 1, LEGACY_PROTOCOL_VERSION),
        Err(RampError::InvalidOrder)
    );
    assert_eq!(
        operator_send_authorization_message(&off_ramp, 9, 1, PROTOCOL_VERSION),
        Err(RampError::InvalidOrder)
    );
}

#[test]
fn done_requires_both_verified_legs_and_external_label() {
    let path = journal_path("done-gate");
    let bound = order(RampDirection::OnRamp, "customer-a");
    let digest = bound.order_digest;
    let mut journal = open_journal(&path);
    journal
        .create_order(bound, 1_001)
        .unwrap_or_else(|error| panic!("create order: {error:?}"));
    journal
        .acquire_lease(digest, WORKER, 1_001, 30)
        .unwrap_or_else(|error| panic!("lease: {error:?}"));
    journal
        .transition(
            digest,
            WorkflowStage::CompliancePending,
            WorkflowStage::AwaitingExternalCredit,
            TransitionEvidence::empty(),
            WORKER,
            1_002,
        )
        .unwrap_or_else(|error| panic!("approve: {error:?}"));
    let direct_done = journal.transition(
        digest,
        WorkflowStage::AwaitingExternalCredit,
        WorkflowStage::Done,
        TransitionEvidence::empty(),
        WORKER,
        1_003,
    );
    assert_eq!(direct_done, Err(RampError::IllegalTransition));
    let presentation = journal
        .order(&digest)
        .unwrap_or_else(|| panic!("order missing"))
        .presentation();
    assert_eq!(presentation.status, AggregateStatus::Pending);
    assert_eq!(presentation.external_custody_label, EXTERNAL_CUSTODY_LABEL);
    drop(journal);
    remove_journal(&path);
}

#[test]
fn callback_validation_is_staged_before_any_durable_append() {
    let path = journal_path("staged-callback");
    let (mut journal, digest) = seeded_journal(&path);
    let before = journal.projection().clone();
    let bytes_before = journal_bytes(&path);
    let head_before = journal.head();
    let count_before = journal.record_count();

    let settled = settled_evidence();
    let empty = TransitionEvidence::empty();
    let illegal_transition = journal.apply_provider_callback(
        callback_write(
            digest,
            CALLBACK_ID,
            1,
            CALLBACK_EVIDENCE_DIGEST,
            WorkflowStage::AwaitingExternalCredit,
            WorkflowStage::LayerxVerified,
            &settled,
        ),
        CALLBACK_AT,
    );
    assert_eq!(illegal_transition, Err(RampError::IllegalTransition));
    let missing_evidence = journal.apply_provider_callback(
        callback_write(
            digest,
            CALLBACK_ID,
            1,
            CALLBACK_EVIDENCE_DIGEST,
            WorkflowStage::AwaitingExternalCredit,
            WorkflowStage::ProviderSettled,
            &empty,
        ),
        CALLBACK_AT,
    );
    assert_eq!(missing_evidence, Err(RampError::IllegalTransition));
    let stale_stage = journal.apply_provider_callback(
        callback_write(
            digest,
            CALLBACK_ID,
            1,
            CALLBACK_EVIDENCE_DIGEST,
            WorkflowStage::CompliancePending,
            WorkflowStage::AwaitingExternalCredit,
            &empty,
        ),
        CALLBACK_AT,
    );
    assert_eq!(stale_stage, Err(RampError::Conflict));
    let unknown_order = journal.apply_provider_callback(
        callback_write(
            [2; 32],
            CALLBACK_ID,
            1,
            CALLBACK_EVIDENCE_DIGEST,
            WorkflowStage::AwaitingExternalCredit,
            WorkflowStage::ProviderSettled,
            &settled,
        ),
        CALLBACK_AT,
    );
    assert_eq!(unknown_order, Err(RampError::InvalidOrder));
    let zero_sequence = journal.apply_provider_callback(
        callback_write(
            digest,
            CALLBACK_ID,
            0,
            CALLBACK_EVIDENCE_DIGEST,
            WorkflowStage::AwaitingExternalCredit,
            WorkflowStage::ProviderSettled,
            &settled,
        ),
        CALLBACK_AT,
    );
    assert_eq!(zero_sequence, Err(RampError::Provider));
    assert_eq!(journal.projection(), &before);
    assert_eq!(journal.callback(CALLBACK_ID), None);
    assert_eq!(journal.provider_sequence(&digest), None);
    assert_eq!(journal.head(), head_before);
    assert_eq!(journal.record_count(), count_before);
    assert_eq!(journal_bytes(&path), bytes_before);

    assert_eq!(settle_callback(&mut journal, digest), Ok(true));
    assert_eq!(
        journal.callback(CALLBACK_ID),
        Some(settled_identity(digest))
    );
    assert_eq!(journal.provider_sequence(&digest), Some(1));
    assert_eq!(
        journal.order(&digest).map(|snapshot| snapshot.stage),
        Some(WorkflowStage::ProviderSettled)
    );
    assert_eq!(journal.record_count(), count_before + 1);
    assert_eq!(journal_bytes(&path).len() as u64, journal.durable_len());
    drop(journal);
    remove_journal(&path);
}

#[test]
fn applied_callback_identity_is_idempotent_and_forged_retries_conflict() {
    let path = journal_path("callback-identity");
    let (mut journal, digest) = seeded_journal(&path);
    assert_eq!(settle_callback(&mut journal, digest), Ok(true));
    let applied = journal.projection().clone();
    let bytes_applied = journal_bytes(&path);
    let count_applied = journal.record_count();

    assert_eq!(settle_callback(&mut journal, digest), Ok(false));
    let settled = settled_evidence();
    let forged_identity = journal.apply_provider_callback(
        callback_write(
            digest,
            CALLBACK_ID,
            2,
            [10; 32],
            WorkflowStage::ProviderSettled,
            WorkflowStage::ProviderReversed,
            &settled,
        ),
        CALLBACK_AT,
    );
    assert_eq!(forged_identity, Err(RampError::Conflict));
    let stale_sequence = journal.apply_provider_callback(
        callback_write(
            digest,
            "provider-callback-0",
            1,
            [11; 32],
            WorkflowStage::ProviderSettled,
            WorkflowStage::ProviderReversed,
            &settled,
        ),
        CALLBACK_AT,
    );
    assert_eq!(stale_sequence, Err(RampError::Conflict));
    assert_eq!(journal.projection(), &applied);
    assert_eq!(journal.projection().callbacks().len(), 1);
    assert_eq!(journal.projection().provider_sequences().len(), 1);
    assert_eq!(journal.record_count(), count_applied);
    assert_eq!(journal_bytes(&path), bytes_applied);
    drop(journal);
    remove_journal(&path);
}

struct RecoveryAuthority {
    bound_order: RampOrder,
    callback: layerx_ramp_toolkit::clients::ProviderCallback,
    callback_digest: [u8; 32],
    callback_key: [u8; 32],
    operator: OperatorIdentity,
    provider: layerx_ramp_toolkit::clients::ProviderClient,
    layerx: layerx_ramp_toolkit::clients::LayerxClient,
    custody: layerx_ramp_toolkit::clients::PaxeerCustodyClient,
    tracker: layerx_paxeer_client::TrackerConfig,
}

impl RecoveryAuthority {
    fn protected() -> Self {
        use layerx_ramp_toolkit::clients::{
            configured_sequencer, parse_hex32, ActivityConfig, Endpoint, LayerxClient,
            MutualTlsClient, MutualTlsFiles, PaxeerCustodyClient, ProviderClient, SecretFile,
        };
        use std::time::Duration;
        fn field<T: serde::de::DeserializeOwned>(value: &serde_json::Value, key: &str) -> T {
            serde_json::from_value(value.get(key).cloned().unwrap_or(serde_json::Value::Null))
                .unwrap_or_else(|_| panic!("required protected recovery field: {key}"))
        }
        fn secret(value: &serde_json::Value, key: &str) -> String {
            let path: String = field(value, key);
            let bytes = SecretFile::new(path).and_then(|file| file.read())
                .unwrap_or_else(|_| panic!("protected recovery credential unavailable: {key}"));
            String::from_utf8(bytes).unwrap_or_else(|_| panic!("credential is not UTF-8"))
                .trim().to_owned()
        }
        fn endpoint(value: &serde_json::Value, key: &str) -> Endpoint {
            Endpoint::parse(&field::<String>(value, key))
                .unwrap_or_else(|_| panic!("protected recovery endpoint refused: {key}"))
        }
        let path = std::env::var_os("LAYERX_RAMP_RECOVERY_CONTRACT_CONFIG")
            .unwrap_or_else(|| panic!("protected real recovery contract configuration required"));
        let bytes = SecretFile::new(PathBuf::from(path)).and_then(|file| file.read())
            .unwrap_or_else(|_| panic!("protected real recovery configuration unavailable"));
        let config: serde_json::Value = serde_json::from_slice(&bytes)
            .unwrap_or_else(|_| panic!("protected real recovery configuration invalid"));
        let tls_config = &config["client_tls"];
        let timeout = Duration::from_secs(field(tls_config, "timeout_seconds"));
        assert!(!timeout.is_zero());
        let tls = MutualTlsFiles {
            ca_pem: field(tls_config, "ca_pem"),
            identity_pkcs12: SecretFile::new(field::<PathBuf>(tls_config, "identity_pkcs12"))
                .unwrap_or_else(|_| panic!("recovery TLS identity unavailable")),
            identity_password: SecretFile::new(field::<PathBuf>(tls_config, "identity_password_file"))
                .unwrap_or_else(|_| panic!("recovery TLS password unavailable")),
        };
        let http = || MutualTlsClient::new(&tls, timeout)
            .unwrap_or_else(|_| panic!("recovery mutual TLS client refused"));
        let provider_config = &config["provider"];
        let provider = ProviderClient {
            http: http(), endpoint: endpoint(provider_config, "endpoint"),
            credential: secret(provider_config, "credential_file"),
            settlement_path: field(provider_config, "settlement_path"),
            status_path: field(provider_config, "status_path"),
        };
        let layerx_config = &config["layerx"];
        let layerx = LayerxClient {
            http: http(), gateway: endpoint(layerx_config, "gateway_endpoint"),
            receipt_authority: endpoint(layerx_config, "receipt_authority_endpoint"),
            signer: endpoint(layerx_config, "signer_endpoint"),
            gateway_key: secret(layerx_config, "gateway_key_file"),
            authority_token: secret(layerx_config, "authority_token_file"),
            signer_token: secret(layerx_config, "signer_token_file"),
            sequencer_authorization: configured_sequencer(
                &field::<String>(layerx_config, "sequencer_id"),
                &field::<String>(layerx_config, "sequencer_public_key"),
                &field::<String>(layerx_config, "sequencer_first_batch"),
                &field::<String>(layerx_config, "sequencer_last_batch"),
            ).unwrap_or_else(|_| panic!("recovery sequencer authorization refused")),
            activity: ActivityConfig {
                actor_did: field::<String>(layerx_config, "actor_did").into_bytes(),
                protocol_version: field(layerx_config, "protocol_version"),
                network_id: field(layerx_config, "network_id"),
                fee_limit: field(layerx_config, "fee_limit"),
                signer_public_key: parse_hex32(&field::<String>(layerx_config, "signer_public_key"))
                    .unwrap_or_else(|_| panic!("recovery signer identity refused")),
            },
        };
        let custody_config = &config["paxeer"];
        let custody = PaxeerCustodyClient {
            http: http(), endpoint: endpoint(custody_config, "custody_endpoint"),
            credential: secret(custody_config, "custody_credential_file"),
            broadcast_path: field(custody_config, "broadcast_path"),
            status_path: field(custody_config, "status_path"),
            operator_account: field(custody_config, "operator_account"),
            wallet_address: field(custody_config, "wallet_address"),
            vault_id: field(custody_config, "vault_id"),
            signer_key_handle: field(custody_config, "signer_key_handle"),
        };
        let trust: PathBuf = field(custody_config, "rpc_trust_anchor_der");
        let trust = fs::read(trust).unwrap_or_else(|_| panic!("recovery RPC trust anchor unavailable"));
        assert!(!trust.is_empty());
        let tracker = layerx_paxeer_client::TrackerConfig {
            endpoints: field::<Vec<String>>(custody_config, "rpc_endpoints").into_iter()
                .map(|url| layerx_paxeer_client::EndpointConfig {
                    url, request_timeout: timeout,
                    transport: layerx_paxeer_client::EndpointTransport::PinnedTls {
                        trust_anchor_der: trust.clone(),
                    },
                    expected_chain_id: field(custody_config, "rpc_chain_id"),
                }).collect(),
            minimum_endpoint_agreement: field(custody_config, "rpc_minimum_agreement"),
            required_confirmations: field(custody_config, "required_confirmations"),
            poll_cadence: Duration::from_secs(field(custody_config, "poll_cadence_seconds")),
            delayed_after_polls: field(custody_config, "delayed_after_polls"),
        };
        fn protected_input<T: serde::de::DeserializeOwned>(name: &str) -> T {
            let path = std::env::var_os(name)
                .unwrap_or_else(|| panic!("required protected real recovery input: {name}"));
            let bytes = SecretFile::new(PathBuf::from(path)).and_then(|file| file.read())
                .unwrap_or_else(|_| panic!("protected recovery input unavailable: {name}"));
            serde_json::from_slice(&bytes)
                .unwrap_or_else(|_| panic!("protected recovery input invalid: {name}"))
        }
        let bound_order: RampOrder = protected_input("LAYERX_RAMP_RECOVERY_CONTRACT_ORDER");
        bound_order.validate_bound().unwrap_or_else(|_| panic!("recovery order identity refused"));
        assert_eq!(bound_order.direction(), RampDirection::OnRamp);
        let callback: layerx_ramp_toolkit::clients::ProviderCallback =
            protected_input("LAYERX_RAMP_RECOVERY_CONTRACT_CALLBACK");
        let callback_key = parse_hex32(&field::<String>(&config, "provider_callback_public_key"))
            .unwrap_or_else(|_| panic!("recovery callback key refused"));
        callback.verify(&bound_order, &callback_key)
            .unwrap_or_else(|_| panic!("real signed provider callback refused"));
        assert_eq!(callback.result.state, layerx_ramp_toolkit::clients::ProviderState::Settled);
        let callback_digest = layerx_ramp_toolkit::clients::callback_evidence_digest(&callback)
            .unwrap_or_else(|_| panic!("recovery callback identity unavailable"));
        Self { bound_order, callback, callback_digest, callback_key,
            operator: field(&config, "operator"), provider, layerx, custody, tracker }
    }

    fn seed(&self, path: &Path) -> (Journal, [u8; 32]) {
        let digest = self.bound_order.order_digest;
        let mut journal = open_journal(path);
        journal.create_order(self.bound_order.clone(), 1_001)
            .unwrap_or_else(|error| panic!("create real bound order: {error:?}"));
        journal.acquire_lease(digest, WORKER, 1_001, 30)
            .unwrap_or_else(|error| panic!("lease real bound order: {error:?}"));
        journal.transition(digest, WorkflowStage::CompliancePending,
            WorkflowStage::AwaitingExternalCredit, TransitionEvidence::empty(), WORKER, 1_002)
            .unwrap_or_else(|error| panic!("seed real bound order stage: {error:?}"));
        (journal, digest)
    }

    fn settle(&self, journal: &mut Journal, digest: [u8; 32]) -> Result<bool, RampError> {
        assert_eq!(digest, self.bound_order.order_digest);
        self.callback.verify(&self.bound_order, &self.callback_key)?;
        let mut evidence = TransitionEvidence::empty();
        evidence.provider_operation_id = Some(self.callback.result.operation_id.clone());
        evidence.provider_evidence_digest = self.callback.result.evidence_digest;
        evidence.refusal_code = self.callback.result.refusal_code.clone();
        evidence.retry_at = self.callback.result.retry_at;
        journal.apply_provider_callback(callback_write(
            digest, &self.callback.callback_id, self.callback.provider_sequence, self.callback_digest,
            WorkflowStage::AwaitingExternalCredit, WorkflowStage::ProviderSettled, &evidence,
        ), CALLBACK_AT)
    }

    fn identity(&self) -> CallbackIdentity {
        CallbackIdentity { order_digest: self.bound_order.order_digest,
            provider_sequence: self.callback.provider_sequence, evidence_digest: self.callback_digest }
    }

    fn provider_state(&self) -> layerx_ramp_toolkit::clients::ProviderResult {
        assert_eq!(self.operator, self.bound_order.operator);
        let key = format!("idempotency:{}", layerx_ramp_toolkit::clients::hex(&self.bound_order.order_digest));
        let current = self.provider.reconcile(&self.bound_order, &key)
            .unwrap_or_else(|_| panic!("real matching settled provider fixture required"));
        assert_eq!(current, self.callback.result);
        current
    }

    fn recover(&self, journal: &mut Journal, invoked: &std::cell::Cell<bool>) -> Result<(), RampError> {
        journal.recover_verified(|projection| {
            invoked.set(true);
            layerx_ramp_toolkit::clients::verify_recovery_settlement(
                projection, &self.operator, &self.provider, &self.layerx, &self.custody, &self.tracker,
            )
        })
    }

}

fn assert_unready(journal: &Journal) {
    for _ in 0..3 {
        let health = journal.health();
        assert!(health.writer_held);
        assert!(health.halted);
        assert!(health.recovery_required);
        assert!(!health.ready);
        assert!(journal.halted());
    }
}

fn assert_callback_recovery(fault: WriteFault, label: &str) {
    let authority = RecoveryAuthority::protected();
    let provider_before = authority.provider_state();
    let reference_path = journal_path(&format!("{label}-reference"));
    let (mut reference, reference_digest) = authority.seed(&reference_path);
    assert_eq!(authority.settle(&mut reference, reference_digest), Ok(true));
    let reference_bytes = journal_bytes(&reference_path);
    for step in WriteStep::ALL {
        let path = journal_path(&format!("{label}-{step:?}"));
        let (mut journal, digest) = authority.seed(&path);
        let before = journal.projection().clone();
        let bytes_before = journal_bytes(&path);
        let head_before = journal.head();
        let count_before = journal.record_count();
        journal.arm_write_fault(step, fault);
        assert_eq!(authority.settle(&mut journal, digest), Err(RampError::Journal), "{step:?}");
        assert_eq!(journal.armed_write_fault(), None, "{step:?}");
        assert_unready(&journal);
        assert!(journal.health().uncertain_write);
        assert_eq!(journal.projection(), &before, "{step:?}");
        assert_eq!(journal.callback(&authority.callback.callback_id), None, "{step:?}");
        assert_eq!(journal.provider_sequence(&digest), None, "{step:?}");
        assert_eq!(journal.head(), head_before, "{step:?}");
        assert_eq!(journal.record_count(), count_before, "{step:?}");
        assert_eq!(journal.acquire_lease(digest, WORKER, CALLBACK_AT, 30), Err(RampError::Journal));
        assert_eq!(authority.settle(&mut journal, digest), Err(RampError::Journal));
        let retained = journal_bytes(&path);
        match step {
            WriteStep::BeforeRecord => assert_eq!(retained, bytes_before, "{step:?}"),
            WriteStep::DuringRecord | WriteStep::BeforeTerminator => {
                assert!(retained.len() > bytes_before.len(), "{step:?}");
                assert!(retained.starts_with(&bytes_before), "{step:?}");
                assert!(reference_bytes.starts_with(&retained), "{step:?}");
                assert_ne!(retained.last(), Some(&b'\n'), "{step:?}");
            }
            WriteStep::AfterRecord | WriteStep::AfterSync => assert_eq!(retained, reference_bytes, "{step:?}"),
        }
        let mut intent_path = path.as_os_str().to_owned();
        intent_path.push(".append-intent");
        let intent_path = PathBuf::from(intent_path);
        let pending_intent = journal_bytes(&intent_path);
        assert!(!pending_intent.is_empty());
        drop(journal);
        let mut recovered = open_journal(&path);
        assert_unready(&recovered);
        assert_eq!(journal_bytes(&path), retained, "{step:?}");
        assert_eq!(journal_bytes(&intent_path), pending_intent, "{step:?}");
        assert_eq!(authority.settle(&mut recovered, digest), Err(RampError::Journal));
        assert_eq!(recovered.create_order(before.order(&digest)
            .unwrap_or_else(|| panic!("bound order absent")).order.clone(), CALLBACK_AT),
            Err(RampError::Journal));
        match step {
            WriteStep::BeforeRecord => {
                assert_eq!(recovered.recovery(), None, "{step:?}");
                assert_eq!(recovered.projection(), &before, "{step:?}");
                assert_eq!(recovered.head(), head_before, "{step:?}");
                assert_eq!(recovered.record_count(), count_before, "{step:?}");
            }
            WriteStep::DuringRecord | WriteStep::BeforeTerminator => {
                assert_eq!(recovered.recovery(), Some(TornTail {
                    offset: bytes_before.len() as u64,
                    bytes: (retained.len() - bytes_before.len()) as u64,
                }), "{step:?}");
                assert_eq!(recovered.projection(), &before, "{step:?}");
                assert_eq!(recovered.head(), head_before, "{step:?}");
                assert_eq!(recovered.record_count(), count_before, "{step:?}");
                assert!(recovered.health().torn_tail);
            }
            WriteStep::AfterRecord | WriteStep::AfterSync => {
                assert_eq!(recovered.recovery(), None, "{step:?}");
                assert_same_state(&recovered, &reference, step);
            }
        }
        let mut wrong_operator = authority.operator.clone();
        wrong_operator.principal_id.push_str("-different-owner");
        let unchanged = recovered.projection().clone();
        let denied = recovered.recover_verified(|projection| {
            layerx_ramp_toolkit::clients::verify_recovery_settlement(
                projection, &wrong_operator, &authority.provider, &authority.layerx,
                &authority.custody, &authority.tracker,
            )
        });
        assert_eq!(denied, Err(RampError::OrderBinding));
        assert_unready(&recovered);
        assert_eq!(recovered.projection(), &unchanged);
        assert_eq!(journal_bytes(&path), retained);
        assert_eq!(journal_bytes(&intent_path), pending_intent);
        assert_eq!(authority.settle(&mut recovered, digest), Err(RampError::Journal));
        let invoked = std::cell::Cell::new(false);
        assert_eq!(authority.recover(&mut recovered, &invoked), Ok(()));
        assert!(invoked.get());
        assert!(recovered.health().ready);
        assert!(!recovered.health().halted);
        assert!(!recovered.health().uncertain_write);
        assert!(!recovered.health().recovery_required);
        assert!(!recovered.health().torn_tail);
        assert_eq!(recovered.recovery(), None);
        assert!(!intent_path.exists());
        assert!(journal_bytes(&path).starts_with(&retained));
        assert_eq!(recovered.callback(&authority.callback.callback_id), Some(authority.identity()));
        assert_eq!(recovered.provider_sequence(&digest), Some(authority.callback.provider_sequence));
        assert_eq!(recovered.projection().callbacks().len(), 1);
        assert_eq!(recovered.projection().provider_sequences().len(), 1);
        assert_eq!(recovered.record_count(), count_before + 1);
        assert_same_state(&recovered, &reference, step);
        assert_eq!(journal_bytes(&path), reference_bytes, "{step:?}");
        assert_eq!(authority.settle(&mut recovered, digest), Ok(false));
        assert_same_state(&recovered, &reference, step);
        assert_eq!(journal_bytes(&path), reference_bytes, "{step:?}");
        assert_eq!(authority.provider_state(), provider_before);
        drop(recovered);
        let mut replayed = open_journal(&path);
        assert_unready(&replayed);
        assert_eq!(replayed.recovery(), None, "{step:?}");
        assert_same_state(&replayed, &reference, step);
        assert_eq!(journal_bytes(&path), reference_bytes, "{step:?}");
        assert_eq!(authority.settle(&mut replayed, digest), Err(RampError::Journal));
        let invoked = std::cell::Cell::new(false);
        assert_eq!(authority.recover(&mut replayed, &invoked), Ok(()));
        assert!(invoked.get());
        assert_eq!(authority.settle(&mut replayed, digest), Ok(false));
        assert_same_state(&replayed, &reference, step);
        assert_eq!(journal_bytes(&path), reference_bytes, "{step:?}");
        assert_eq!(authority.provider_state(), provider_before);
        drop(replayed);
        remove_journal(&path);
    }
    drop(reference);
    remove_journal(&reference_path);
}

#[test]
fn mismatched_pending_append_prefix_preserves_history_and_refuses_recovery() {
    let authority = RecoveryAuthority::protected();
    let provider_before = authority.provider_state();
    let path = journal_path("pending-prefix-mismatch");
    let (mut journal, digest) = authority.seed(&path);
    let before = journal.projection().clone();
    let original = journal_bytes(&path);
    journal.arm_write_fault(WriteStep::DuringRecord, WriteFault::Interrupt);
    assert_eq!(authority.settle(&mut journal, digest), Err(RampError::Journal));
    assert_unready(&journal);
    drop(journal);
    let mut retained = journal_bytes(&path);
    assert!(retained.len() > original.len());
    retained[original.len()] ^= 1;
    fs::write(&path, &retained).unwrap_or_else(|error| panic!("write mismatched prefix: {error}"));
    let mut intent_path = path.as_os_str().to_owned();
    intent_path.push(".append-intent");
    let intent_path = PathBuf::from(intent_path);
    let intent = journal_bytes(&intent_path);
    for _ in 0..2 {
        let mut journal = open_journal(&path);
        assert_unready(&journal);
        let invoked = std::cell::Cell::new(false);
        assert_eq!(authority.recover(&mut journal, &invoked), Err(RampError::Journal));
        assert!(!invoked.get());
        assert_unready(&journal);
        assert_eq!(authority.settle(&mut journal, digest), Err(RampError::Journal));
        assert_eq!(journal.projection(), &before);
        assert_eq!(journal_bytes(&path), retained);
        assert_eq!(journal_bytes(&intent_path), intent);
        drop(journal);
    }
    assert_eq!(authority.provider_state(), provider_before);
    remove_journal(&path);
}

#[test]
fn failed_callback_apply_retains_no_event_and_retry_is_idempotent() {
    assert_callback_recovery(WriteFault::Fail, "callback-fail");
}

#[test]
fn interrupted_callback_write_recovers_on_restart_without_repair() {
    assert_callback_recovery(WriteFault::Interrupt, "callback-interrupt");
}

#[test]
fn replaying_the_journal_reproduces_identical_projection() {
    let path = journal_path("replay-equivalence");
    let (mut journal, digest) = seeded_journal(&path);
    assert_eq!(settle_callback(&mut journal, digest), Ok(true));
    let mut layerx = TransitionEvidence::empty();
    layerx.activity_id = Some([4; 32]);
    layerx.canonical_activity = Some(vec![1, 2, 3]);
    layerx.retry_at = Some(1_100);
    journal
        .transition(
            digest,
            WorkflowStage::ProviderSettled,
            WorkflowStage::LayerxPending,
            layerx,
            WORKER,
            1_004,
        )
        .unwrap_or_else(|error| panic!("layerx pending: {error:?}"));
    journal
        .plan_paxeer([5; 32], [1; 32], 10, 1_005)
        .unwrap_or_else(|error| panic!("plan paxeer: {error:?}"));
    journal
        .observe_paxeer(
            [5; 32],
            PaxeerObservation {
                operation_id: "paxeer-op-1",
                transaction_hash: [6; 32],
                stage: "broadcast_unknown",
                block_hash: None,
                confirmations: 0,
            },
            1_006,
        )
        .unwrap_or_else(|error| panic!("observe paxeer: {error:?}"));
    let live = journal.projection().clone();
    let head = journal.head();
    let count = journal.record_count();
    let durable_len = journal.durable_len();
    assert_eq!(count, 7);
    assert_eq!(journal_bytes(&path).len() as u64, durable_len);
    drop(journal);

    let replayed = open_journal(&path);
    assert_eq!(replayed.recovery(), None);
    assert_unready(&replayed);
    assert_eq!(replayed.projection(), &live);
    assert_eq!(replayed.head(), head);
    assert_eq!(replayed.record_count(), count);
    assert_eq!(replayed.durable_len(), durable_len);
    assert_eq!(
        replayed.order(&digest).map(OrderSnapshot::presentation),
        live.order(&digest).map(OrderSnapshot::presentation)
    );
    assert_eq!(replayed.paxeer(&[5; 32]), live.paxeer(&[5; 32]));
    assert_eq!(
        replayed.callback(CALLBACK_ID),
        Some(settled_identity(digest))
    );
    drop(replayed);
    remove_journal(&path);
}

#[test]
fn torn_tail_is_recovered_but_terminated_corruption_is_rejected() {
    let path = journal_path("torn-tail");
    let (journal, digest) = seeded_journal(&path);
    let clean = journal.projection().clone();
    let head = journal.head();
    let count = journal.record_count();
    let bytes = journal_bytes(&path);
    drop(journal);

    let first_record_end = bytes
        .iter()
        .position(|byte| *byte == b'\n')
        .unwrap_or_else(|| panic!("journal has no terminated record"));
    let torn = &bytes[..first_record_end / 2];
    append_raw(&path, torn);
    let retained = journal_bytes(&path);
    assert_eq!(retained, [bytes.as_slice(), torn].concat());
    let mut recovered = open_journal(&path);
    assert_eq!(
        recovered.recovery(),
        Some(TornTail {
            offset: bytes.len() as u64,
            bytes: torn.len() as u64,
        })
    );
    assert_eq!(recovered.projection(), &clean);
    assert_eq!(recovered.head(), head);
    assert_eq!(recovered.record_count(), count);
    assert_eq!(journal_bytes(&path), retained);
    assert_unready(&recovered);
    assert!(recovered.health().torn_tail);
    assert_eq!(settle_callback(&mut recovered, digest), Err(RampError::Journal));
    let authority = RecoveryAuthority::protected();
    let invoked = std::cell::Cell::new(false);
    assert_eq!(authority.recover(&mut recovered, &invoked), Err(RampError::Journal));
    assert!(!invoked.get());
    assert_unready(&recovered);
    assert_eq!(journal_bytes(&path), retained);
    assert_eq!(recovered.projection(), &clean);
    assert_eq!(recovered.head(), head);
    assert_eq!(recovered.record_count(), count);
    assert_eq!(
        recovered.order(&digest).map(|snapshot| snapshot.stage),
        Some(WorkflowStage::AwaitingExternalCredit)
    );
    drop(recovered);
    let restarted = open_journal(&path);
    assert_unready(&restarted);
    assert_eq!(journal_bytes(&path), retained);
    assert_eq!(restarted.projection(), &clean);
    assert_eq!(restarted.head(), head);
    assert_eq!(restarted.record_count(), count);
    assert_eq!(restarted.recovery(), Some(TornTail {
        offset: bytes.len() as u64, bytes: torn.len() as u64,
    }));
    drop(restarted);
    remove_journal(&path);

    let path = journal_path("terminated-corruption");
    fs::write(&path, &bytes).unwrap_or_else(|error| panic!("seed corruption journal: {error}"));
    append_raw(&path, b"{}\n");
    let rejected = journal_bytes(&path);
    assert!(matches!(Journal::open(&path), Err(RampError::Journal)));
    assert_eq!(journal_bytes(&path), rejected);
    fs::write(&path, &bytes).unwrap_or_else(|error| panic!("restore journal: {error}"));

    append_raw(&path, b"\n");
    let rejected = journal_bytes(&path);
    assert!(matches!(Journal::open(&path), Err(RampError::Journal)));
    assert_eq!(journal_bytes(&path), rejected);
    fs::write(&path, &bytes).unwrap_or_else(|error| panic!("restore journal: {error}"));

    let last_record_start = bytes[..bytes.len() - 1]
        .iter()
        .rposition(|byte| *byte == b'\n')
        .map_or(0, |position| position + 1);
    append_raw(&path, &bytes[last_record_start..]);
    let rejected = journal_bytes(&path);
    assert!(matches!(Journal::open(&path), Err(RampError::Journal)));
    assert_eq!(journal_bytes(&path), rejected);
    fs::write(&path, &bytes).unwrap_or_else(|error| panic!("restore journal: {error}"));

    let reopened = open_journal(&path);
    assert_eq!(reopened.recovery(), None);
    assert_unready(&reopened);
    assert_eq!(reopened.projection(), &clean);
    assert_eq!(reopened.head(), head);
    drop(reopened);
    remove_journal(&path);
}

#[test]
fn paxeer_observations_preserve_inclusion_and_confirmation_history() {
    let path = journal_path("paxeer-monotonic");
    let mut journal = open_journal(&path);
    journal
        .plan_paxeer([5; 32], [1; 32], 10, 1)
        .unwrap_or_else(|error| panic!("plan: {error:?}"));
    let observe = |stage, block_hash, confirmations| PaxeerObservation {
        operation_id: "operation-1",
        transaction_hash: [6; 32],
        stage,
        block_hash,
        confirmations,
    };
    journal
        .observe_paxeer([5; 32], observe("confirming", Some([7; 32]), 2), 2)
        .unwrap_or_else(|error| panic!("included: {error:?}"));
    let before = journal_bytes(&path);
    for observation in [
        observe("confirming", Some([7; 32]), 1),
        observe("final", Some([8; 32]), 3),
        observe("unknown_stage", Some([7; 32]), 2),
        observe("missing", None, 0),
        observe("displaced", Some([8; 32]), 0),
        observe("displaced", Some([7; 32]), 1),
        observe("displaced", None, 0),
    ] {
        assert!(journal.observe_paxeer([5; 32], observation, 3).is_err());
        assert_eq!(journal_bytes(&path), before);
    }
    journal
        .observe_paxeer([5; 32], observe("final", Some([7; 32]), 3), 3)
        .unwrap_or_else(|error| panic!("final: {error:?}"));
    assert!(journal
        .observe_paxeer([5; 32], observe("confirming", Some([7; 32]), 4), 4)
        .is_err());
    journal
        .observe_paxeer([5; 32], observe("displaced", Some([7; 32]), 0), 4)
        .unwrap_or_else(|error| panic!("reorg: {error:?}"));
    journal
        .observe_paxeer([5; 32], observe("confirming", Some([8; 32]), 1), 5)
        .unwrap_or_else(|error| panic!("new inclusion: {error:?}"));
    drop(journal);
    let reopened = open_journal(&path);
    let recovered = reopened
        .paxeer(&[5; 32])
        .unwrap_or_else(|| panic!("recovered operation"));
    assert_eq!(recovered.block_hash, Some([8; 32]));
    assert_eq!(recovered.confirmations, 1);
    drop(reopened);
    remove_journal(&path);
}

#[test]
fn native_receive_binds_genuine_signed_grant_to_order_and_operator() {
    use layerx_ramp_toolkit::clients::{parse_hex32, ActivityConfig, LayerxConfig, SecretFile};

    #[derive(serde::Deserialize)]
    #[serde(deny_unknown_fields)]
    struct NativeReceiveInput {
        order: RampOrder,
        layerx: LayerxConfig,
        canonical_receive_payload: Vec<u8>,
        account_sequence: u64,
        now: u64,
    }

    let path = std::env::var_os("LAYERX_RAMP_NATIVE_RECEIVE_CONTRACT_FILE")
        .unwrap_or_else(|| panic!("genuine signed native receive contract input required"));
    let bytes = SecretFile::new(PathBuf::from(path)).and_then(|file| file.read())
        .unwrap_or_else(|_| panic!("protected native receive contract input unavailable"));
    let input: NativeReceiveInput = serde_json::from_slice(&bytes)
        .unwrap_or_else(|_| panic!("protected native receive contract input invalid"));
    let activity = ActivityConfig {
        actor_did: input.layerx.actor_did.into_bytes(),
        protocol_version: input.layerx.protocol_version,
        network_id: input.layerx.network_id,
        fee_limit: input.layerx.fee_limit,
        signer_public_key: parse_hex32(&input.layerx.signer_public_key)
            .unwrap_or_else(|_| panic!("native receive signer public identity invalid")),
    };
    let payload = &input.canonical_receive_payload;
    let order = &input.order;
    let sequence = input.account_sequence;
    let now = input.now;
    assert_eq!(activity.protocol_version, 3);
    assert_eq!(payload.len(), 733);
    assert_eq!(order.direction(), RampDirection::OffRamp);
    assert_eq!(activity.validate_native_receive(order, payload, sequence, now), Ok(()));
    let layerx_crypto::payments::Payment::Receive { payer_grant, .. } =
        layerx_crypto::payments::Payment::decode(
            layerx_types::payload::ModuleId::Asset, 6, payload, &activity.actor_did,
        ).unwrap_or_else(|_| panic!("genuine native receive decoding failed")) else {
            panic!("native receive input carries another activity")
        };
    assert!(now < order.quote.expires_at);
    assert!(now <= payer_grant.expiration);
    for index in 0..payload.len() {
        let mut changed = payload.clone();
        changed[index] ^= 1;
        assert!(activity.validate_native_receive(order, &changed, sequence, now).is_err(),
            "changed canonical native receive byte admitted: {index}");
    }
    for length in [0, 1, 732] {
        assert!(activity.validate_native_receive(order, &payload[..length], sequence, now).is_err());
    }
    let mut trailing = payload.clone();
    trailing.push(0);
    assert!(activity.validate_native_receive(order, &trailing, sequence, now).is_err());
    assert!(activity.validate_native_receive(order, payload, sequence ^ 1, now).is_err());
    assert!(activity.validate_native_receive(order, payload, sequence, order.quote.expires_at).is_err());
    if let Some(expired) = payer_grant.expiration.checked_add(1) {
        assert!(activity.validate_native_receive(order, payload, sequence, expired).is_err());
    }
    for protocol in [0, 1, 2, 4] {
        let mut changed = activity.clone();
        changed.protocol_version = protocol;
        assert!(changed.validate_native_receive(order, payload, sequence, now).is_err());
    }
    let mut changed = activity.clone();
    changed.network_id ^= 1;
    assert!(changed.validate_native_receive(order, payload, sequence, now).is_err());
    changed = activity.clone();
    changed.signer_public_key[0] ^= 1;
    assert!(changed.validate_native_receive(order, payload, sequence, now).is_err());
    changed = activity.clone();
    changed.actor_did.push(b'x');
    assert!(changed.validate_native_receive(order, payload, sequence, now).is_err());
    let mut changed = order.clone();
    changed.quote.layerx_amount ^= 1;
    changed.order_digest = changed.digest();
    assert!(activity.validate_native_receive(&changed, payload, sequence, now).is_err());
    changed = order.clone();
    changed.quote.layerx_asset[0] ^= 1;
    changed.order_digest = changed.digest();
    assert!(activity.validate_native_receive(&changed, payload, sequence, now).is_err());
    changed = order.clone();
    changed.context[0] ^= 1;
    changed.quote.context = changed.context;
    changed.order_digest = changed.digest();
    assert!(activity.validate_native_receive(&changed, payload, sequence, now).is_err());
    changed = order.clone();
    changed.payer_grant = None;
    changed.order_digest = changed.digest();
    assert!(activity.validate_native_receive(&changed, payload, sequence, now).is_err());
    changed = order.clone();
    std::mem::swap(&mut changed.customer.account, &mut changed.operator.account);
    changed.order_digest = changed.digest();
    assert!(activity.validate_native_receive(&changed, payload, sequence, now).is_err());
    changed = order.clone();
    changed.quote.direction = RampDirection::OnRamp;
    changed.order_digest = changed.digest();
    assert!(activity.validate_native_receive(&changed, payload, sequence, now).is_err());
    assert_eq!(activity.validate_native_receive(order, payload, sequence, now), Ok(()));
}
