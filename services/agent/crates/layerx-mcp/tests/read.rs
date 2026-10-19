use layerx_agent_api::availability::{
    AvailabilityClass, AvailabilityCompletion, AvailabilityReport, ClassReport, ProviderRef,
};
use layerx_agent_api::identity::Asset;
use layerx_agent_api::prepare::CanonicalBytes;
use layerx_agent_api::proof::ProofBundle;
use layerx_agent_api::read::{
    AccountRef, BalanceValue, BatchRef, CheckpointRef, CheckpointValue, Freshness, HistoryCursor,
    HistoryValue, RelativeTo,
};
use layerx_agent_api::verify::Level;
use layerx_agent_api::{Amount, Sequence};
use layerx_mcp::tools::read::{
    availability, balance, checkpoint, history, proof, receipt, Continuation, Pagination,
    ReadToolError, ReceiptValue, StableCursor,
};

fn bytes(value: &[u8]) -> CanonicalBytes {
    CanonicalBytes::new(value.to_vec()).unwrap_or_else(|error| panic!("bytes: {error:?}"))
}

fn freshness() -> Freshness {
    Freshness {
        chain_head: Sequence(120),
        latest_sealed_batch: BatchRef::new("batch-11")
            .unwrap_or_else(|error| panic!("batch: {error:?}")),
        latest_finalised_checkpoint: CheckpointRef::new("checkpoint-9")
            .unwrap_or_else(|error| panic!("checkpoint: {error:?}")),
        value_sequence: Sequence(117),
        relative_to: RelativeTo::Checkpoint(
            CheckpointRef::new("checkpoint-9")
                .unwrap_or_else(|error| panic!("checkpoint: {error:?}")),
        ),
    }
}

#[test]
fn every_read_shape_requires_a_level_and_carries_the_exact_freshness() {
    let expected_freshness = freshness();
    let balance_result = balance(
        BalanceValue {
            account: AccountRef::new("did:layerx:reader")
                .unwrap_or_else(|error| panic!("account: {error:?}")),
            asset: Asset::new("LXP").unwrap_or_else(|error| panic!("asset: {error:?}")),
            amount: Amount(42),
            canonical_state: bytes(b"core-balance"),
        },
        Level::StateProven,
        expected_freshness.clone(),
        1024,
    )
    .unwrap_or_else(|error| panic!("balance: {error:?}"));
    assert_eq!(balance_result.verification_level, Level::StateProven);
    assert_eq!(balance_result.freshness, expected_freshness);
    assert!(balance_result.page.complete);

    let receipt_result = receipt(
        ReceiptValue {
            canonical_receipt: bytes(b"core-receipt"),
            evidence_ids: vec![[1; 32]],
        },
        Level::BatchIncluded,
        freshness(),
        1024,
    )
    .unwrap_or_else(|error| panic!("receipt: {error:?}"));
    assert_eq!(receipt_result.verification_level, Level::BatchIncluded);

    let checkpoint_result = checkpoint(
        CheckpointValue(bytes(b"core-certificate")),
        Level::CheckpointFinalised,
        freshness(),
        1024,
    )
    .unwrap_or_else(|error| panic!("checkpoint: {error:?}"));
    assert_eq!(
        checkpoint_result.verification_level,
        Level::CheckpointFinalised
    );

    let proof_result = proof(
        ProofBundle {
            target: bytes(b"core-root"),
            proofs: vec![bytes(b"proof-a"), bytes(b"proof-b")],
        },
        Level::StateProven,
        freshness(),
        1024,
    )
    .unwrap_or_else(|error| panic!("proof: {error:?}"));
    assert_eq!(proof_result.verification_level, Level::StateProven);

    let provider =
        ProviderRef::new("provider-a").unwrap_or_else(|error| panic!("provider: {error:?}"));
    let availability_result = availability(
        AvailabilityReport {
            completion: AvailabilityCompletion::Complete { provider },
            classes: vec![ClassReport {
                class: AvailabilityClass::Receipts,
                complete: true,
                verified_chunks: 1,
                verified_bytes: 12,
                failure: None,
            }],
            providers: Vec::new(),
        },
        Level::BatchIncluded,
        freshness(),
        1024,
    )
    .unwrap_or_else(|error| panic!("availability: {error:?}"));
    assert_eq!(availability_result.page.returned_bytes, 12);
}

#[test]
fn unverified_or_evidence_free_values_never_enter_a_fact_result() {
    assert!(matches!(
        checkpoint(
            CheckpointValue(bytes(b"unverified")),
            Level::Unverified,
            freshness(),
            1024,
        ),
        Err(ReadToolError::Unverified)
    ));
    assert!(matches!(
        receipt(
            ReceiptValue {
                canonical_receipt: bytes(b"receipt"),
                evidence_ids: Vec::new(),
            },
            Level::SequencerSigned,
            freshness(),
            1024,
        ),
        Err(ReadToolError::MissingReceiptEvidence)
    ));
}

#[test]
fn history_truncation_is_explicit_and_uses_snapshot_bound_stable_cursors() {
    let query = [0x31; 32];
    let snapshot = [0x41; 32];
    let all = HistoryValue {
        records: vec![bytes(b"one"), bytes(b"two"), bytes(b"three")],
        next_cursor: None,
    };
    let first = history(
        &all.clone(),
        Level::BatchIncluded,
        freshness(),
        query,
        snapshot,
        Pagination::new(2, 6, None).unwrap_or_else(|error| panic!("pagination: {error:?}")),
    )
    .unwrap_or_else(|error| panic!("first page: {error:?}"));
    assert!(!first.page.complete);
    assert!(first.page.explicitly_truncated);
    let Continuation::Local(cursor) = first
        .page
        .next
        .unwrap_or_else(|| panic!("continuation absent"))
    else {
        panic!("wrong continuation kind");
    };
    assert_eq!(cursor.offset, 2);
    let second = history(
        &all,
        Level::BatchIncluded,
        freshness(),
        query,
        snapshot,
        Pagination::new(2, 6, Some(cursor)).unwrap_or_else(|error| panic!("pagination: {error:?}")),
    )
    .unwrap_or_else(|error| panic!("second page: {error:?}"));
    assert!(second.page.complete);
    assert_eq!(second.value.records, vec![bytes(b"three")]);

    let wrong = StableCursor {
        query_digest: [9; 32],
        snapshot,
        offset: 0,
    };
    assert!(matches!(
        history(
            &HistoryValue {
                records: vec![bytes(b"one")],
                next_cursor: None,
            },
            Level::BatchIncluded,
            freshness(),
            query,
            snapshot,
            Pagination::new(1, 10, Some(wrong))
                .unwrap_or_else(|error| panic!("pagination: {error:?}")),
        ),
        Err(ReadToolError::CursorMismatch)
    ));
}

#[test]
fn upstream_core_pagination_is_never_misreported_as_complete() {
    let result = history(
        &HistoryValue {
            records: vec![bytes(b"one")],
            next_cursor: Some(
                HistoryCursor::new("core-cursor-2")
                    .unwrap_or_else(|error| panic!("cursor: {error:?}")),
            ),
        },
        Level::BatchIncluded,
        freshness(),
        [1; 32],
        [2; 32],
        Pagination::new(10, 1024, None).unwrap_or_else(|error| panic!("pagination: {error:?}")),
    )
    .unwrap_or_else(|error| panic!("history: {error:?}"));
    assert!(!result.page.complete);
    assert!(result.page.explicitly_truncated);
    assert!(matches!(result.page.next, Some(Continuation::Core(_))));
}
