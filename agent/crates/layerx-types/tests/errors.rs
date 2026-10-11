use std::collections::BTreeSet;

use layerx_types::error::{ErrorClass, LayerError};
use layerx_types::result::{KnownResult, ResultCode, ResultDomain, Retriability};

#[test]
fn every_layer_error_has_one_distinct_class() {
    let errors = [
        LayerError::TransportFailure { code: 1 },
        LayerError::Deadline,
        LayerError::ProtocolIncompatibility { local: 1, peer: 2 },
        LayerError::UnavailableCapability {
            capability: "proof".into(),
        },
        LayerError::CoreRejection {
            result: ResultCode::from_raw(-3),
        },
        LayerError::VerificationFailure {
            check: "signature".into(),
        },
        LayerError::PolicyRefusal {
            rule: "default-deny".into(),
        },
        LayerError::CapabilityRefusal {
            dimension: "asset".into(),
        },
        LayerError::BudgetRefusal {
            budget: "daily".into(),
        },
        LayerError::RateLimit {
            bucket: "submit".into(),
        },
        LayerError::InternalFault {
            invariant: "state".into(),
        },
    ];
    let classes: BTreeSet<ErrorClass> = errors.iter().map(LayerError::class).collect();
    assert_eq!(classes.len(), errors.len());
    assert!(classes.contains(&ErrorClass::TransportFailure));
    assert!(classes.contains(&ErrorClass::VerificationFailure));
}

#[test]
fn protocol_mapping_is_total_unique_and_lossless() {
    let mut raw_values = BTreeSet::new();
    for known in KnownResult::ALL {
        assert!(raw_values.insert(known.raw()), "duplicate result number");
        let code = ResultCode::from(*known);
        assert_eq!(code.known(), Some(*known));
        assert_eq!(code.raw(), known.raw());
        assert!(matches!(
            code.retriability(),
            Retriability::Terminal | Retriability::Retriable
        ));
    }
}

#[test]
fn unknown_numbers_round_trip_verbatim_and_fail_closed() {
    let unknown = ResultCode::from_raw(-7777);
    assert_eq!(unknown.raw(), -7777);
    assert_eq!(unknown.known(), None);
    assert_eq!(unknown.domain(), ResultDomain::Fatal);
    assert_eq!(unknown.retriability(), Retriability::Terminal);
}

#[test]
fn domain_partition_matches_the_core() {
    assert_eq!(ResultCode::from_raw(0).domain(), ResultDomain::Success);
    assert_eq!(ResultCode::from_raw(-1).domain(), ResultDomain::Codec);
    assert_eq!(ResultCode::from_raw(-100).domain(), ResultDomain::Envelope);
    assert_eq!(ResultCode::from_raw(-200).domain(), ResultDomain::Authority);
    assert_eq!(
        ResultCode::from_raw(-300).domain(),
        ResultDomain::Sequencing
    );
    assert_eq!(ResultCode::from_raw(-400).domain(), ResultDomain::Ledger);
    assert_eq!(
        ResultCode::from_raw(-500).domain(),
        ResultDomain::Arithmetic
    );
    assert_eq!(ResultCode::from_raw(-600).domain(), ResultDomain::Metering);
    assert_eq!(ResultCode::from_raw(-700).domain(), ResultDomain::Module);
    assert_eq!(ResultCode::from_raw(-800).domain(), ResultDomain::Batch);
    assert_eq!(ResultCode::from_raw(-900).domain(), ResultDomain::Storage);
    assert_eq!(ResultCode::from_raw(-1000).domain(), ResultDomain::Fatal);
    assert_eq!(ResultCode::from_raw(1).domain(), ResultDomain::Unknown);
}

/// Kernel rows of the error code registry: wire code, message, header value
/// and the Retryable column.
const KERNEL_REGISTRY: &[(&str, &str, i32, Retriability)] = &[
    ("LXP_TRUNCATED", "truncated", -1, Retriability::Terminal),
    (
        "LXP_TRAILING_BYTES",
        "trailing bytes",
        -2,
        Retriability::Terminal,
    ),
    (
        "LXP_NON_CANONICAL",
        "non canonical",
        -3,
        Retriability::Terminal,
    ),
    (
        "LXP_UNSORTED_SEQUENCE",
        "unsorted sequence",
        -4,
        Retriability::Terminal,
    ),
    (
        "LXP_LENGTH_LIMIT",
        "length limit",
        -5,
        Retriability::Terminal,
    ),
    ("LXP_INVALID_TAG", "invalid tag", -6, Retriability::Terminal),
    (
        "LXP_UNKNOWN_FIELD",
        "unknown field",
        -7,
        Retriability::Terminal,
    ),
    (
        "LXP_DUPLICATE_ENTRY",
        "duplicate entry",
        -8,
        Retriability::Terminal,
    ),
    (
        "LXP_WRONG_NETWORK",
        "wrong network",
        -100,
        Retriability::Terminal,
    ),
    (
        "LXP_VERSION_UNSUPPORTED",
        "version unsupported",
        -101,
        Retriability::Terminal,
    ),
    (
        "LXP_UNKNOWN_MODULE",
        "unknown module",
        -102,
        Retriability::Terminal,
    ),
    (
        "LXP_MODULE_DISABLED",
        "module disabled",
        -103,
        Retriability::Terminal,
    ),
    (
        "LXP_PAYLOAD_HASH_MISMATCH",
        "payload hash mismatch",
        -104,
        Retriability::Terminal,
    ),
    (
        "LXP_MALFORMED_ENVELOPE",
        "malformed envelope",
        -105,
        Retriability::Terminal,
    ),
    (
        "LXP_UNKNOWN_ACTIVITY",
        "unknown activity",
        -106,
        Retriability::Terminal,
    ),
    (
        "LXP_MALFORMED_SEND",
        "malformed send",
        -107,
        Retriability::Terminal,
    ),
    (
        "LXP_MALFORMED_RECEIVE",
        "malformed receive",
        -108,
        Retriability::Terminal,
    ),
    (
        "LXP_UNKNOWN_DID",
        "unknown did",
        -200,
        Retriability::Terminal,
    ),
    (
        "LXP_BAD_SIGNATURE",
        "bad signature",
        -201,
        Retriability::Terminal,
    ),
    (
        "LXP_AUTH_EXPIRED",
        "auth expired",
        -202,
        Retriability::Terminal,
    ),
    (
        "LXP_AUTH_REVOKED",
        "auth revoked",
        -203,
        Retriability::Terminal,
    ),
    ("LXP_AUTH_SCOPE", "auth scope", -204, Retriability::Terminal),
    (
        "LXP_AUTH_ALLOWANCE",
        "auth allowance",
        -205,
        Retriability::Terminal,
    ),
    (
        "LXP_IDENTITY_FROZEN",
        "identity frozen",
        -206,
        Retriability::Retriable,
    ),
    (
        "LXP_UNAUTHORIZED_DEBIT",
        "unauthorized debit",
        -207,
        Retriability::Terminal,
    ),
    (
        "LXP_UNKNOWN_ACCOUNT_NAMESPACE",
        "unknown account namespace",
        -208,
        Retriability::Terminal,
    ),
    (
        "LXP_MALFORMED_GRANT",
        "malformed grant",
        -209,
        Retriability::Terminal,
    ),
    (
        "LXP_UNKNOWN_AUTHORITY_KIND",
        "unknown authority kind",
        -210,
        Retriability::Terminal,
    ),
    (
        "LXP_GRANT_EXHAUSTED",
        "grant exhausted",
        -211,
        Retriability::Terminal,
    ),
    (
        "LXP_STALE_REVOCATION",
        "stale revocation",
        -212,
        Retriability::Terminal,
    ),
    (
        "LXP_CONTEXT_MISMATCH",
        "context mismatch",
        -213,
        Retriability::Terminal,
    ),
    (
        "LXP_NO_PAYER_GRANT",
        "no payer grant",
        -214,
        Retriability::Terminal,
    ),
    (
        "LXP_GRANT_SCOPE_VIOLATION",
        "grant scope violation",
        -215,
        Retriability::Terminal,
    ),
    (
        "LXP_PURPOSE_MISMATCH",
        "purpose mismatch",
        -216,
        Retriability::Terminal,
    ),
    (
        "LXP_INVOICE_ALREADY_SETTLED",
        "invoice already settled",
        -217,
        Retriability::Terminal,
    ),
    (
        "LXP_GRANT_EXPIRED",
        "grant expired",
        -218,
        Retriability::Terminal,
    ),
    (
        "LXP_GRANT_REVOKED",
        "grant revoked",
        -219,
        Retriability::Terminal,
    ),
    (
        "LXP_AUTH_THRESHOLD_UNMET",
        "auth threshold unmet",
        -220,
        Retriability::Terminal,
    ),
    (
        "LXP_AUTH_DUPLICATE_SIGNER",
        "auth duplicate signer",
        -221,
        Retriability::Terminal,
    ),
    (
        "LXP_AUTH_NOT_MATURE",
        "auth not mature",
        -222,
        Retriability::Retriable,
    ),
    (
        "LXP_SEQUENCE_GAP",
        "sequence gap",
        -300,
        Retriability::Terminal,
    ),
    (
        "LXP_SEQUENCE_REUSED",
        "sequence reused",
        -301,
        Retriability::Terminal,
    ),
    (
        "LXP_IDEMPOTENT_REPLAY",
        "idempotent replay",
        -302,
        Retriability::Terminal,
    ),
    ("LXP_EXPIRED", "expired", -303, Retriability::Terminal),
    (
        "LXP_NOT_YET_VALID",
        "not yet valid",
        -304,
        Retriability::Retriable,
    ),
    (
        "LXP_SEQUENCE_MISMATCH",
        "sequence mismatch",
        -305,
        Retriability::Terminal,
    ),
    (
        "LXP_CONDITION_UNMET",
        "condition unmet",
        -306,
        Retriability::Terminal,
    ),
    (
        "LXP_SEQUENCE_EXHAUSTED",
        "sequence exhausted",
        -307,
        Retriability::Terminal,
    ),
    (
        "LXP_INSUFFICIENT_BALANCE",
        "insufficient balance",
        -400,
        Retriability::Retriable,
    ),
    (
        "LXP_ZERO_AMOUNT",
        "zero amount",
        -401,
        Retriability::Terminal,
    ),
    (
        "LXP_ASSET_MISMATCH",
        "asset mismatch",
        -402,
        Retriability::Terminal,
    ),
    (
        "LXP_ASSET_PAUSED",
        "asset paused",
        -403,
        Retriability::Retriable,
    ),
    (
        "LXP_ACCOUNT_FROZEN",
        "account frozen",
        -404,
        Retriability::Retriable,
    ),
    (
        "LXP_CONSERVATION",
        "conservation",
        -405,
        Retriability::Terminal,
    ),
    (
        "LXP_TOO_MANY_LEGS",
        "too many legs",
        -406,
        Retriability::Terminal,
    ),
    (
        "LXP_ACCOUNT_NOT_EMPTY",
        "account not empty",
        -407,
        Retriability::Terminal,
    ),
    (
        "LXP_BALANCE_BYPASS",
        "balance bypass",
        -408,
        Retriability::Terminal,
    ),
    (
        "LXP_ACCOUNT_ID_MISMATCH",
        "account id mismatch",
        -409,
        Retriability::Terminal,
    ),
    (
        "LXP_CLIENT_SUPPLIED_BALANCE",
        "client supplied balance",
        -410,
        Retriability::Terminal,
    ),
    (
        "LXP_ASSET_ALREADY_REGISTERED",
        "asset already registered",
        -411,
        Retriability::Terminal,
    ),
    (
        "LXP_INVALID_AMOUNT",
        "invalid amount",
        -412,
        Retriability::Terminal,
    ),
    (
        "LXP_DEPOSIT_PROOF_NOT_FINAL",
        "deposit proof not final",
        -413,
        Retriability::Terminal,
    ),
    (
        "LXP_DEPOSIT_ALREADY_CREDITED",
        "deposit already credited",
        -414,
        Retriability::Terminal,
    ),
    (
        "LXP_WITHDRAWAL_ALREADY_SETTLED",
        "withdrawal already settled",
        -415,
        Retriability::Terminal,
    ),
    (
        "LXP_CHALLENGE_WINDOW_OPEN",
        "challenge window open",
        -416,
        Retriability::Terminal,
    ),
    (
        "LXP_WITHDRAWAL_CANCELLED",
        "withdrawal cancelled",
        -417,
        Retriability::Terminal,
    ),
    (
        "LXP_WITHDRAWAL_ASSET_MISMATCH",
        "withdrawal asset mismatch",
        -418,
        Retriability::Terminal,
    ),
    ("LXP_OVERFLOW", "overflow", -500, Retriability::Terminal),
    ("LXP_UNDERFLOW", "underflow", -501, Retriability::Terminal),
    ("LXP_DIV_ZERO", "div zero", -502, Retriability::Terminal),
    ("LXP_PRECISION", "precision", -503, Retriability::Terminal),
    ("LXP_FEE_LIMIT", "fee limit", -600, Retriability::Terminal),
    (
        "LXP_GAS_EXHAUSTED",
        "gas exhausted",
        -601,
        Retriability::Retriable,
    ),
    (
        "LXP_FEE_UNPAYABLE",
        "fee unpayable",
        -602,
        Retriability::Retriable,
    ),
    (
        "LXP_ESCROW_STATE",
        "escrow state",
        -700,
        Retriability::Terminal,
    ),
    (
        "LXP_BUDGET_PERIOD_CAP",
        "budget period cap",
        -701,
        Retriability::Retriable,
    ),
    (
        "LXP_STREAM_UNDERFUNDED",
        "stream underfunded",
        -702,
        Retriability::Terminal,
    ),
    (
        "LXP_MARKET_HALTED",
        "market halted",
        -703,
        Retriability::Retriable,
    ),
    (
        "LXP_ORACLE_STALE",
        "oracle stale",
        -704,
        Retriability::Retriable,
    ),
    (
        "LXP_MARGIN_INSUFFICIENT",
        "margin insufficient",
        -705,
        Retriability::Retriable,
    ),
    (
        "LXP_AGREEMENT_STATE",
        "agreement state",
        -706,
        Retriability::Terminal,
    ),
    (
        "LXP_CAPTURE_EXCEEDS_HOLD",
        "capture exceeds hold",
        -707,
        Retriability::Terminal,
    ),
    (
        "LXP_UNAUTHORIZED_CAPTURE",
        "unauthorized capture",
        -708,
        Retriability::Terminal,
    ),
    (
        "LXP_HOLD_EXPIRED",
        "hold expired",
        -709,
        Retriability::Terminal,
    ),
    (
        "LXP_UNAUTHORIZED_ESCROW_SPEND",
        "unauthorized escrow spend",
        -710,
        Retriability::Terminal,
    ),
    (
        "LXP_DISPUTE_WINDOW_CLOSED",
        "dispute window closed",
        -711,
        Retriability::Terminal,
    ),
    (
        "LXP_HOLD_DISPUTED",
        "hold disputed",
        -712,
        Retriability::Terminal,
    ),
    (
        "LXP_BUDGET_ALLOWANCE_EXCEEDED",
        "budget allowance exceeded",
        -713,
        Retriability::Terminal,
    ),
    (
        "LXP_INSUFFICIENT_BUDGET_FUNDS",
        "insufficient budget funds",
        -714,
        Retriability::Retriable,
    ),
    (
        "LXP_UNAUTHORIZED_DELEGATE",
        "unauthorized delegate",
        -715,
        Retriability::Terminal,
    ),
    (
        "LXP_BUDGET_REVOKED",
        "budget revoked",
        -716,
        Retriability::Terminal,
    ),
    (
        "LXP_NON_MONOTONIC_TIME",
        "non monotonic time",
        -717,
        Retriability::Terminal,
    ),
    (
        "LXP_ACCRUAL_OVERFLOW",
        "accrual overflow",
        -718,
        Retriability::Terminal,
    ),
    (
        "LXP_UNAUTHORIZED_METER",
        "unauthorized meter",
        -719,
        Retriability::Terminal,
    ),
    (
        "LXP_METER_REGRESSION",
        "meter regression",
        -720,
        Retriability::Terminal,
    ),
    (
        "LXP_STREAM_CLOSED",
        "stream closed",
        -721,
        Retriability::Terminal,
    ),
    (
        "LXP_MODULE_MAY_NOT_WRITE_BALANCE",
        "module may not write balance",
        -722,
        Retriability::Terminal,
    ),
    (
        "LXP_OFFER_UNAVAILABLE",
        "offer unavailable",
        -723,
        Retriability::Terminal,
    ),
    (
        "LXP_TERMS_MISMATCH",
        "terms mismatch",
        -724,
        Retriability::Terminal,
    ),
    (
        "LXP_INVALID_ATTESTATION",
        "invalid attestation",
        -725,
        Retriability::Terminal,
    ),
    (
        "LXP_DELIVERABLE_MISMATCH",
        "deliverable mismatch",
        -726,
        Retriability::Terminal,
    ),
    (
        "LXP_DELIVERY_DEADLINE_PASSED",
        "delivery deadline passed",
        -727,
        Retriability::Terminal,
    ),
    (
        "LXP_UNAUTHORIZED_DISPUTANT",
        "unauthorized disputant",
        -728,
        Retriability::Terminal,
    ),
    (
        "LXP_UNAUTHORIZED_ORACLE",
        "unauthorized oracle",
        -729,
        Retriability::Terminal,
    ),
    (
        "LXP_ORACLE_SEQUENCE",
        "oracle sequence",
        -730,
        Retriability::Terminal,
    ),
    (
        "LXP_ORACLE_BOUNDS",
        "oracle bounds",
        -731,
        Retriability::Terminal,
    ),
    (
        "LXP_ORACLE_DEVIATION",
        "oracle deviation",
        -732,
        Retriability::Terminal,
    ),
    (
        "LXP_MARKET_ALREADY_EXISTS",
        "market already exists",
        -733,
        Retriability::Terminal,
    ),
    (
        "LXP_PARAMETER_BOUNDS",
        "parameter bounds",
        -734,
        Retriability::Terminal,
    ),
    (
        "LXP_PAUSED_SCOPE",
        "paused scope",
        -735,
        Retriability::Retriable,
    ),
    (
        "LXP_PROGRAM_REFUSED",
        "program refused",
        -736,
        Retriability::Terminal,
    ),
    ("LXP_BATCH_GAP", "batch gap", -800, Retriability::Terminal),
    (
        "LXP_ROOT_MISMATCH",
        "root mismatch",
        -801,
        Retriability::Terminal,
    ),
    (
        "LXP_TIMESTAMP_REGRESSION",
        "timestamp regression",
        -802,
        Retriability::Terminal,
    ),
    (
        "LXP_ATTESTATION_THRESHOLD",
        "attestation threshold",
        -803,
        Retriability::Terminal,
    ),
    ("LXP_DA_MISSING", "da missing", -804, Retriability::Terminal),
    (
        "LXP_EQUIVOCATION",
        "equivocation",
        -805,
        Retriability::Terminal,
    ),
    (
        "LXP_LOG_CORRUPT",
        "log corrupt",
        -900,
        Retriability::Terminal,
    ),
    (
        "LXP_LOG_TRUNCATED",
        "log truncated",
        -901,
        Retriability::Terminal,
    ),
    (
        "LXP_SNAPSHOT_MISMATCH",
        "snapshot mismatch",
        -902,
        Retriability::Terminal,
    ),
    (
        "LXP_PROJECTION_STALE",
        "projection stale",
        -903,
        Retriability::Retriable,
    ),
    ("LXP_IO", "io", -904, Retriability::Retriable),
    (
        "LXP_ARENA_EXHAUSTED",
        "arena exhausted",
        -905,
        Retriability::Terminal,
    ),
    (
        "LXP_SNAPSHOT_BLOBS_MISSING",
        "snapshot blobs missing",
        -906,
        Retriability::Terminal,
    ),
    (
        "LXP_FATAL_INVARIANT",
        "fatal invariant",
        -1001,
        Retriability::Terminal,
    ),
    (
        "LXP_FATAL_REPLAY_DIVERGENCE",
        "fatal replay divergence",
        -1002,
        Retriability::Terminal,
    ),
    (
        "LXP_FATAL_SUPPLY_MISMATCH",
        "fatal supply mismatch",
        -1003,
        Retriability::Terminal,
    ),
];

#[test]
fn kernel_revert_reasons_round_trip_to_the_header_value() {
    assert_eq!(KERNEL_REGISTRY.len(), KnownResult::ALL.len() - 1);
    for (code, message, raw, _) in KERNEL_REGISTRY {
        let reason = format!("{code}: {message}");
        let detailed = format!("{code}: {message}; detail");
        let parsed = KnownResult::from_revert_reason(&reason);
        assert_eq!(parsed, KnownResult::from_revert_reason(&detailed), "{code}");
        let code_value = ResultCode::from_raw(*raw);
        if code_value.domain() == ResultDomain::Fatal {
            assert_eq!(parsed, None, "{code} must never parse from a revert");
            assert_eq!(
                Retriability::of_revert_reason(&reason),
                Retriability::Terminal
            );
        } else {
            assert_eq!(parsed.map(KnownResult::raw), Some(*raw), "{code}");
            assert_eq!(parsed, code_value.known(), "{code}");
        }
    }
}

#[test]
fn kernel_revert_retriability_matches_the_registry_column() {
    let disagreements: Vec<(&str, Retriability, Retriability)> = KERNEL_REGISTRY
        .iter()
        .filter_map(|(code, message, _, registry)| {
            let mirror = Retriability::of_revert_reason(&format!("{code}: {message}"));
            (mirror != *registry).then_some((*code, mirror, *registry))
        })
        .collect();
    assert_eq!(disagreements, Vec::new());
}

#[test]
fn unknown_success_and_malformed_reasons_fail_closed() {
    for reason in [
        "LXP_UNKNOWN: unknown",
        "LXP_OK: ok",
        "LXP_ERR_TRUNCATED: truncated",
        "LXP_TRUNCATED",
        "LXP_TRUNCATED:truncated",
        "LXP_truncated: truncated",
        "LXP_TRUNCATEDX: truncated",
        "LXP_TRUNCATED_: truncated",
        "LXP__TRUNCATED: truncated",
        "TRUNCATED: truncated",
        "LXP_XWEB_UNKNOWN_REQUEST: unknown request",
        "LXP_FATAL_INVARIANT: fatal invariant",
        "",
    ] {
        assert_eq!(KnownResult::from_revert_reason(reason), None, "{reason}");
        assert_eq!(
            Retriability::of_revert_reason(reason),
            Retriability::Terminal,
            "{reason}"
        );
    }
    assert_eq!(
        KnownResult::from_revert_reason("LXP_NOT_YET_VALID: not yet valid"),
        Some(KnownResult::NotYetValid)
    );
    assert_eq!(
        Retriability::of_revert_reason("LXP_NOT_YET_VALID: not yet valid"),
        Retriability::Retriable
    );
    assert_eq!(
        KnownResult::from_revert_reason("LXP_DA_MISSING: da missing"),
        Some(KnownResult::DaMissing)
    );
}
