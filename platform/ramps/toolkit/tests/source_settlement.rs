use std::fs::{self, OpenOptions};
use std::os::unix::fs::OpenOptionsExt as _;
use std::path::PathBuf;

use layerx_interop_gateway::trace::TraceId;
use layerx_ramp_toolkit::clients::SecretFile;
use layerx_ramp_toolkit::journal::{Journal, TransitionEvidence, WorkflowStage};
use layerx_ramp_toolkit::migration::{
    SOURCE_SETTLEMENT_VERSION, SourceChainKind, SourceSettlementConfig, SourceSettlementRequest,
    SourceSettlementService,
};
use layerx_ramp_toolkit::{AggregateStatus, RampDirection, RampError, RampOrder};
use serde::Deserialize;

#[test]
fn source_intake_is_closed_and_requires_canonical_bounded_evidence() {
    let valid = serde_json::json!({
        "version": SOURCE_SETTLEMENT_VERSION,
        "order_digest": ([1_u8; 32]),
        "chain": "ethereum",
        "source_evidence": "AQ=="
    });
    let request: SourceSettlementRequest = serde_json::from_value(valid.clone())
        .unwrap_or_else(|error| panic!("request decoding: {error}"));
    assert!(SourceSettlementService::evidence(&request).is_ok());
    for field in [
        "receipt_digest",
        "source_claim_id",
        "principal",
        "quote",
        "signature",
    ] {
        let mut changed = valid.clone();
        changed[field] = serde_json::json!("caller-controlled");
        assert!(serde_json::from_value::<SourceSettlementRequest>(changed).is_err());
    }
    for encoded in ["", "AQ", "AQ=", "AR==", "AQ==\n", "AQ==AAAA", "!!!!"] {
        let mut changed = request.clone();
        changed.source_evidence = encoded.to_owned();
        assert!(
            SourceSettlementService::evidence(&changed).is_err(),
            "{encoded:?}"
        );
    }
    for version in ["", "layerx-ramp-provider-v1", "layerx-migration-source-v1"] {
        let mut changed = request.clone();
        changed.version = version.to_owned();
        assert!(SourceSettlementService::evidence(&changed).is_err());
    }
    let mut changed = request.clone();
    changed.order_digest = [0; 32];
    assert!(SourceSettlementService::evidence(&changed).is_err());
    changed = request.clone();
    changed.source_evidence = "A".repeat(1_398_105);
    assert!(SourceSettlementService::evidence(&changed).is_err());
    assert!(serde_json::from_str::<SourceSettlementRequest>(
        r#"{"version":"layerx-migration-source-v2","order_digest":[],"chain":"ethereum","chain":"solana","source_evidence":"AQ=="}"#
    ).is_err());
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct SourceContract {
    version: String,
    cases: Vec<SourceContractCase>,
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct SourceContractCase {
    config: SourceSettlementConfig,
    order: RampOrder,
    request: SourceSettlementRequest,
}

fn contract() -> SourceContract {
    let path = std::env::var_os("LAYERX_RAMP_SOURCE_CONTRACT_FILE").unwrap_or_else(|| {
        panic!("genuine finalized Ethereum and Solana source contract required")
    });
    let bytes = SecretFile::new(PathBuf::from(path))
        .and_then(|file| file.read())
        .unwrap_or_else(|_| panic!("protected genuine source contract unavailable"));
    let contract: SourceContract = serde_json::from_slice(&bytes)
        .unwrap_or_else(|_| panic!("protected source contract schema refused"));
    assert_eq!(contract.version, "layerx-ramp-source-contract-v2");
    assert_eq!(contract.cases.len(), 2);
    assert!(
        contract
            .cases
            .iter()
            .any(|case| case.request.chain == SourceChainKind::Ethereum)
    );
    assert!(
        contract
            .cases
            .iter()
            .any(|case| case.request.chain == SourceChainKind::Solana)
    );
    contract
}

#[test]
fn genuine_source_finality_binds_orders_and_claims_before_any_funded_transition() {
    let trace = TraceId::mint([7; 16]);
    for (index, case) in contract().cases.into_iter().enumerate() {
        case.order
            .validate_bound()
            .unwrap_or_else(|error| panic!("real order refused: {error:?}"));
        assert_eq!(case.order.direction(), RampDirection::OnRamp);
        let service = SourceSettlementService::new(case.config)
            .unwrap_or_else(|error| panic!("actual custody configuration refused: {error:?}"));
        let record = service
            .verify(&case.order, &case.request, &trace)
            .unwrap_or_else(|error| {
                panic!("genuine configured source finality refused: {error:?}")
            });
        assert_ne!(record.source_claim_id(), [0; 32]);
        assert_eq!(
            record.evidence_digest(),
            SourceSettlementService::evidence(&case.request)
                .unwrap_or_else(|error| panic!("source evidence refused: {error:?}"))
                .digest()
        );
        assert_eq!(record.record().validate_order(&case.order), Ok(()));
        let mut wrong_request = case.request.clone();
        wrong_request.order_digest[0] ^= 1;
        assert!(service.verify(&case.order, &wrong_request, &trace).is_err());
        wrong_request = case.request.clone();
        wrong_request.chain = match wrong_request.chain {
            SourceChainKind::Ethereum => SourceChainKind::Solana,
            SourceChainKind::Solana => SourceChainKind::Ethereum,
        };
        assert!(service.verify(&case.order, &wrong_request, &trace).is_err());
        for mutation in 0..5 {
            let mut wrong_order = case.order.clone();
            match mutation {
                0 => wrong_order.quote.layerx_asset[0] ^= 1,
                1 => wrong_order.quote.layerx_amount ^= 1,
                2 => wrong_order.quote.external_amount_minor ^= 1,
                3 => {
                    wrong_order.customer.account = "agent:did:layerx:wrong-customer:main".to_owned()
                }
                _ => wrong_order.quote.direction = RampDirection::OffRamp,
            }
            wrong_order.order_digest = wrong_order.digest();
            let mut wrong_request = case.request.clone();
            wrong_request.order_digest = wrong_order.order_digest;
            assert!(
                record.record().validate_order(&wrong_order).is_err(),
                "binding mutation {mutation}"
            );
            assert!(
                service
                    .verify(&wrong_order, &wrong_request, &trace)
                    .is_err(),
                "finality mutation {mutation}"
            );
        }
        let path = std::env::temp_dir().join(format!(
            "layerx-source-contract-{}-{index}.jsonl",
            std::process::id()
        ));
        OpenOptions::new()
            .create_new(true)
            .write(true)
            .mode(0o600)
            .open(&path)
            .unwrap_or_else(|error| panic!("create actual journal: {error}"));
        let mut journal =
            Journal::open(&path).unwrap_or_else(|error| panic!("open actual journal: {error:?}"));
        let digest = case.order.order_digest;
        journal
            .create_order(case.order, 1)
            .unwrap_or_else(|error| panic!("create genuine bound order: {error:?}"));
        assert_eq!(
            journal.apply_source_settlement(record.clone(), 2),
            Err(RampError::IllegalTransition)
        );
        journal
            .acquire_lease(digest, "source-contract", 2, 30)
            .unwrap_or_else(|error| panic!("actual journal lease: {error:?}"));
        journal
            .transition(
                digest,
                WorkflowStage::CompliancePending,
                WorkflowStage::AwaitingExternalCredit,
                TransitionEvidence::empty(),
                "source-contract",
                3,
            )
            .unwrap_or_else(|error| panic!("journal eligibility: {error:?}"));
        assert_eq!(journal.apply_source_settlement(record.clone(), 4), Ok(true));
        assert_eq!(journal.source_settlement(&digest), Some(record.record()));
        let snapshot = journal
            .order(&digest)
            .unwrap_or_else(|| panic!("source order missing"));
        assert_eq!(snapshot.stage, WorkflowStage::SourceSettledV2);
        assert_eq!(snapshot.presentation().status, AggregateStatus::Pending);
        assert!(snapshot.evidence.provider_operation_id.is_none());
        assert!(snapshot.evidence.provider_evidence_digest.is_none());
        assert!(snapshot.evidence.activity_id.is_none());
        let head = journal.head();
        let count = journal.record_count();
        let bytes = fs::read(&path).unwrap_or_else(|error| panic!("read durable journal: {error}"));
        assert_eq!(
            journal.apply_source_settlement(record.clone(), 5),
            Ok(false)
        );
        assert_eq!(journal.head(), head);
        assert_eq!(journal.record_count(), count);
        assert_eq!(
            fs::read(&path).unwrap_or_else(|error| panic!("read replay journal: {error}")),
            bytes
        );
        assert_eq!(
            journal.transition(
                digest,
                WorkflowStage::SourceSettledV2,
                WorkflowStage::Done,
                TransitionEvidence::empty(),
                "source-contract",
                5
            ),
            Err(RampError::IllegalTransition)
        );
        drop(journal);
        let reopened =
            Journal::open(&path).unwrap_or_else(|error| panic!("replay V2 journal: {error:?}"));
        assert_eq!(reopened.source_settlement(&digest), Some(record.record()));
        assert_eq!(reopened.record_count(), count);
        assert_eq!(reopened.head(), head);
        assert!(!reopened.health().ready);
        drop(reopened);
        fs::remove_file(&path).unwrap_or_else(|error| panic!("remove contract journal: {error}"));
        let mut lock = path.as_os_str().to_owned();
        lock.push(".writer-lock");
        fs::remove_file(PathBuf::from(lock))
            .unwrap_or_else(|error| panic!("remove writer lock: {error}"));
    }
}
