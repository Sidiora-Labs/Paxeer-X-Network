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

/// One kernel code of `include/layerx/lxp_result.h` joined with its row of the
/// error code registry: wire code, message, header value and Retryable column.
struct KernelRow {
    code: String,
    message: String,
    raw: i32,
    registry: Retriability,
}

fn repository_file(relative: &str) -> String {
    let path = std::path::Path::new(env!("CARGO_MANIFEST_DIR"))
        .join("../../../..")
        .join(relative);
    std::fs::read_to_string(&path)
        .unwrap_or_else(|error| panic!("failed to read {}: {error}", path.display()))
}

fn kernel_registry() -> Vec<KernelRow> {
    let header = repository_file("include/layerx/lxp_result.h");
    let table = repository_file("spec/layerx-merge/contracts/address-table.md");
    let section = table
        .split_once("\n## (b) ")
        .unwrap_or_else(|| panic!("section (b) of the registry is missing"))
        .1;
    let section = section
        .split_once("\n## ")
        .map_or(section, |(body, _)| body);
    let rows: Vec<Vec<&str>> = section
        .lines()
        .filter(|line| line.starts_with("| LXP_"))
        .map(|line| line.split('|').map(str::trim).collect())
        .collect();
    let mut kernel = Vec::new();
    for line in header.lines() {
        let Some(entry) = line.trim().strip_prefix("X(") else {
            continue;
        };
        let (name, value) = entry
            .split_once(')')
            .and_then(|(body, _)| body.split_once(','))
            .unwrap_or_else(|| panic!("malformed header table entry {entry}"));
        let raw: i32 = value
            .trim()
            .parse()
            .unwrap_or_else(|error| panic!("invalid header number in {entry}: {error}"));
        if name == "LXP_OK" {
            continue;
        }
        let code = name
            .strip_prefix("LXP_ERR_")
            .map_or_else(|| name.to_owned(), |rest| format!("LXP_{rest}"));
        let matching: Vec<&Vec<&str>> = rows.iter().filter(|row| row[1] == code).collect();
        assert_eq!(
            matching.len(),
            1,
            "{code} must have exactly one registry row"
        );
        let row = matching[0];
        assert_eq!(row.len(), 7, "{code} registry row has five columns");
        let registry = match row[5] {
            "yes" => Retriability::Retriable,
            "no" => Retriability::Terminal,
            other => panic!("{code} has the Retryable cell {other:?}"),
        };
        kernel.push(KernelRow {
            code,
            message: row[2].to_owned(),
            raw,
            registry,
        });
    }
    kernel
}

#[test]
fn the_registry_names_every_kernel_code_in_header_order() {
    let kernel = kernel_registry();
    assert_eq!(kernel.len(), KnownResult::ALL.len() - 1);
    let mirror: Vec<i32> = KnownResult::ALL[1..]
        .iter()
        .map(|known| known.raw())
        .collect();
    let header: Vec<i32> = kernel.iter().map(|row| row.raw).collect();
    assert_eq!(header, mirror);
    assert_eq!(KnownResult::ALL[0], KnownResult::Ok);
    assert_eq!(KnownResult::Ok.retriability(), Retriability::Terminal);
}

#[test]
fn kernel_revert_reasons_round_trip_to_the_header_value() {
    for KernelRow {
        code, message, raw, ..
    } in kernel_registry()
    {
        let reason = format!("{code}: {message}");
        let detailed = format!("{code}: {message}; detail");
        let parsed = KnownResult::from_revert_reason(&reason);
        assert_eq!(parsed, KnownResult::from_revert_reason(&detailed), "{code}");
        let code_value = ResultCode::from_raw(raw);
        if code_value.domain() == ResultDomain::Fatal {
            assert_eq!(parsed, None, "{code} must never parse from a revert");
            assert_eq!(
                Retriability::of_revert_reason(&reason),
                Retriability::Terminal
            );
        } else {
            assert_eq!(parsed.map(KnownResult::raw), Some(raw), "{code}");
            assert_eq!(parsed, code_value.known(), "{code}");
        }
    }
}

#[test]
fn kernel_revert_retriability_matches_the_registry_column() {
    let disagreements: Vec<(String, Retriability, Retriability)> = kernel_registry()
        .into_iter()
        .filter_map(|row| {
            let mirror = Retriability::of_revert_reason(&format!("{}: {}", row.code, row.message));
            let column = if ResultCode::from_raw(row.raw).domain() == ResultDomain::Fatal {
                Retriability::Terminal
            } else {
                row.registry
            };
            let typed = ResultCode::from_raw(row.raw).retriability();
            (mirror != column || typed != column).then_some((row.code, mirror, column))
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
