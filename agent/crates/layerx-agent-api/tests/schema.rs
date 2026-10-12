use std::collections::BTreeMap;

use layerx_agent_api::{
    agent_api_compat_gate, agent_api_schema_v1, Amount, BudgetLimit, ContractVersion, Sequence,
    TimestampSeconds, VersionRequest, VersionResponse, AGENT_API_V1_SOURCE,
};

const BASELINE: &str = include_str!("../../../schema/agent-api/golden/v1.kvx");
const REQUEST_VECTOR: &str = include_str!("../../../schema/agent-api/golden/version-request.hex");
const RESPONSE_VECTOR: &str = include_str!("../../../schema/agent-api/golden/version-response.hex");
const WRITE_SOURCE: &str = include_str!("../../../schema/agent-api/write.kvx");
const READ_SOURCE: &str = include_str!("../../../schema/agent-api/read.kvx");
const STREAM_SOURCE: &str = include_str!("../../../schema/agent-api/stream.kvx");
const PROGRAMS_SOURCE: &str = include_str!("../../../schema/agent-api/programs.kvx");
const PROGRAMS_BASELINE: &str = include_str!("../../../schema/agent-api/golden/programs.kvx");
const WRITE_BASELINE: &str = include_str!("../../../schema/agent-api/golden/write.kvx");
const ERRORS_SOURCE: &str = include_str!("../../../schema/agent-api/errors.kvx");
const ERRORS_BASELINE: &str = include_str!("../../../schema/agent-api/golden/errors.kvx");

const CUSTODY_CLAIM_TAGS: &[(&str, &str)] = &[
    (
        "write.kvx",
        "type.SubmissionState.Executed.settlement_domain",
    ),
    (
        "stream.kvx",
        "type.ReceiptReference.Verified.settlement_domain",
    ),
    ("read.kvx", "operation.export.offline.settlement_domain"),
];

fn settlement_domain_gate(
    root: &BTreeMap<String, String>,
    modules: &[(&str, &BTreeMap<String, String>)],
) -> Result<(), String> {
    let variants = root
        .get("type.SettlementDomain.variants")
        .ok_or_else(|| "missing the type.SettlementDomain claim vocabulary".to_owned())?;
    if !variants.starts_with("[\"Paxeer\"") {
        return Err(format!(
            "Paxeer must be the first settlement domain variant, got {variants}"
        ));
    }
    for (module, entries) in modules {
        for (key, value) in *entries {
            if key.ends_with(".settlement_domain") && value != "\"Paxeer\"" {
                return Err(format!(
                    "{module} names a foreign settlement domain at {key}: {value}"
                ));
            }
        }
    }
    for (module, key) in CUSTODY_CLAIM_TAGS {
        let tagged = modules.iter().any(|(name, entries)| {
            name == module && entries.get(*key).map(String::as_str) == Some("\"Paxeer\"")
        });
        if !tagged {
            return Err(format!(
                "untagged custody claim: {module} must declare {key} = \"Paxeer\""
            ));
        }
    }
    Ok(())
}

fn declarations(source: &str) -> BTreeMap<String, String> {
    let mut section = String::new();
    let mut values = BTreeMap::new();
    for raw in source.lines() {
        let line = raw.trim();
        if line.is_empty() || line.starts_with('#') || line.starts_with("//") {
            continue;
        }
        if let Some(name) = line
            .strip_prefix('[')
            .and_then(|value| value.strip_suffix(']'))
        {
            name.clone_into(&mut section);
        } else if let Some((key, value)) = line.split_once('=') {
            values.insert(
                format!("{}.{}", section, key.trim()),
                value.trim().to_owned(),
            );
        }
    }
    values
}

fn hex(value: &str) -> Vec<u8> {
    let value = value.trim();
    assert!(value.as_bytes().chunks_exact(2).remainder().is_empty());
    assert!(value
        .bytes()
        .all(|byte| matches!(byte, b'0'..=b'9' | b'a'..=b'f')));
    value
        .as_bytes()
        .chunks_exact(2)
        .map(|digits| {
            let text = std::str::from_utf8(digits)
                .unwrap_or_else(|error| panic!("invalid vector UTF-8: {error}"));
            u8::from_str_radix(text, 16)
                .unwrap_or_else(|error| panic!("invalid vector hex: {error}"))
        })
        .collect()
}

#[test]
fn generated_contract_is_pinned_to_the_schema() {
    let contract = agent_api_schema_v1();
    assert_eq!(contract.name, "LayerX Agent API");
    assert_eq!(contract.version, ContractVersion { major: 2, minor: 0 });
    assert_eq!(contract.node_interface_major, 1);
    let baseline = declarations(BASELINE);
    let current = declarations(AGENT_API_V1_SOURCE);
    assert_eq!(baseline, current);
    for (key, value) in baseline {
        if key == "schema.includes" {
            assert!(
                current[&key].starts_with(value.trim_end_matches(']')),
                "contract module includes were not extended additively"
            );
        } else if key != "schema.minor" {
            assert_eq!(
                current.get(&key),
                Some(&value),
                "contract 2.0 declaration changed at {key}"
            );
        }
    }
    assert_eq!(current.get("schema.minor").map(String::as_str), Some("0"));
    assert!(current["schema.includes"].contains("approval.kvx"));
    assert!(current["schema.includes"].contains("programs.kvx"));
    assert_eq!(
        current["compatibility.history.1_1.classification"],
        "\"additive_only\""
    );
    assert_eq!(
        current["compatibility.history.1_2.classification"],
        "\"additive_only\""
    );

    let request = VersionRequest {
        request_id: Sequence(7),
        supported: contract.version,
    };
    let response = VersionResponse {
        request_id: request.request_id,
        contract: contract.version,
        node_interface_major: contract.node_interface_major,
    };
    assert_eq!(request.request_id.get(), 7);
    assert_eq!(response.contract, request.supported);
}

#[test]
fn major_two_refuses_major_one() {
    let contract = agent_api_schema_v1();
    assert_eq!(contract.version, ContractVersion { major: 2, minor: 0 });
    assert_eq!(
        declarations(AGENT_API_V1_SOURCE)["record.VersionRequest.unsupported_major"],
        "\"unsupported_major\""
    );
    for major in [0, 1, 3, u16::MAX] {
        let request = VersionRequest {
            request_id: Sequence(7),
            supported: ContractVersion { major, minor: 0 },
        };
        assert_eq!(VersionResponse::try_from(request), Err("unsupported_major"));
    }
    for minor in [0, 1, u16::MAX] {
        let request = VersionRequest {
            request_id: Sequence(7),
            supported: ContractVersion { major: 2, minor },
        };
        assert_eq!(
            VersionResponse::try_from(request),
            Ok(VersionResponse {
                request_id: Sequence(7),
                contract: contract.version,
                node_interface_major: 1,
            })
        );
    }
}

#[test]
fn write_and_error_goldens_match_major_two_schema() {
    assert_eq!(WRITE_SOURCE, WRITE_BASELINE);
    assert_eq!(ERRORS_SOURCE, ERRORS_BASELINE);
    let errors = declarations(ERRORS_SOURCE);
    assert_eq!(errors["module.contract_major"], "2");
    assert!(!errors.contains_key("type.Level.variants"));
    assert_eq!(
        errors["type.Rung.variants"],
        "[\"Submitted\",\"Receipted\",\"Committed\"]"
    );
    assert_eq!(
        errors["type.Rung.wire_names"],
        "[\"submitted\",\"receipted\",\"committed\"]"
    );
    assert_eq!(
        errors["type.VerificationStatus.Achieved.required"],
        "[\"rung\"]"
    );
    assert_eq!(errors["type.VerificationStatus.Achieved.rung"], "\"Rung\"");
    assert_eq!(
        errors["type.VerificationStatus.Unverified.requested"],
        "\"Rung\""
    );
    assert_eq!(
        errors["type.VerificationStatus.Unverified.achieved"],
        "\"Rung\""
    );
    assert_eq!(
        errors["error.LXP_IDEMPOTENT_REPLAY.class"],
        "\"CoreRejection\""
    );
    assert_eq!(
        errors["error.LXP_IDEMPOTENT_REPLAY.protocol_result_code"],
        "-302"
    );
    assert_eq!(
        errors["error.LXP_IDEMPOTENT_REPLAY.retriability"],
        "\"Terminal\""
    );
    assert_eq!(
        errors["error.LXP_IDEMPOTENT_REPLAY.reason"],
        "\"idempotent_replay\""
    );
    assert_eq!(
        errors["error.receipt_key_unknown.class"],
        "\"VerificationFailure\""
    );
    assert_eq!(
        errors["error.receipt_key_unknown.protocol_result_code"],
        "null"
    );
    assert_eq!(
        errors["error.receipt_key_unknown.retriability"],
        "\"Terminal\""
    );
    assert_eq!(
        errors["error.receipt_key_unknown.reason"],
        "\"receipt_key_unknown\""
    );
    let root = declarations(AGENT_API_V1_SOURCE);
    assert_eq!(
        root["record.AgentHttpSuccess.verification_status"],
        "\"Either {state:\\\"achieved\\\",rung:R} or {state:\\\"unverified\\\",requested:R,achieved:R,reason:ReasonCode} with achieved strictly below requested; R is one of submitted, receipted, committed. An unverified status that violates the ordering is refused as VerificationFailure verification.inconsistent.\""
    );
}

#[test]
fn programs_contract_is_included_with_receipt_resolution_and_call_idempotency() {
    let programs = declarations(PROGRAMS_SOURCE);
    assert_eq!(programs, declarations(PROGRAMS_BASELINE));
    for operation in [
        "program.discover",
        "program.interface",
        "program.simulate",
        "program.call",
        "program.receipt",
        "program.activity",
    ] {
        assert!(
            programs.contains_key(&format!("operation.{operation}.request")),
            "Programs contract operation missing {operation}"
        );
    }
    assert_eq!(
        programs["operation.program.call.required"],
        "[\"idempotency_key\"]"
    );
    assert_eq!(
        programs["operation.program.call.unknown_outcome"],
        "\"retain_signed_activity_and_resolve_by_receipt\""
    );
}

#[test]
fn golden_request_and_response_vectors_are_reviewable_exact_bytes(
) -> Result<(), Box<dyn std::error::Error>> {
    let contract = agent_api_schema_v1();
    let version_request = VersionRequest {
        request_id: Sequence(7),
        supported: contract.version,
    };
    let version_response = VersionResponse::try_from(version_request)?;
    let encoded_request = version_request.encode();
    let encoded_response = version_response.encode();
    let request = hex(REQUEST_VECTOR);
    let response = hex(RESPONSE_VECTOR);
    assert_eq!(request, encoded_request);
    assert_eq!(response, encoded_response);
    assert_eq!(&request[..4], &[0, 2, 0, 0]);
    assert_eq!(&request[4..12], &7_u64.to_be_bytes());
    assert_eq!(&response[..4], &[0, 2, 0, 0]);
    assert_eq!(&response[4..12], &7_u64.to_be_bytes());
    assert!(AGENT_API_V1_SOURCE.contains(REQUEST_VECTOR.trim()));
    assert!(AGENT_API_V1_SOURCE.contains(RESPONSE_VECTOR.trim()));
    assert!(AGENT_API_V1_SOURCE.contains("00010001000000000000000700000000010001"));
    assert!(AGENT_API_V1_SOURCE.contains("0001000100000000000000070000000000000100010001"));
    if std::env::var_os("LAYERX_AGENT_API_RECORD_VERSION_VECTORS").is_some() {
        let directory =
            std::path::Path::new(env!("CARGO_MANIFEST_DIR")).join("../../schema/agent-api/golden");
        for (name, bytes) in [
            ("version-request.hex", encoded_request.as_slice()),
            ("version-response.hex", encoded_response.as_slice()),
        ] {
            let mut encoded = String::with_capacity(bytes.len() * 2 + 1);
            for byte in bytes {
                use std::fmt::Write;
                write!(encoded, "{byte:02x}")?;
            }
            encoded.push('\n');
            std::fs::write(directory.join(name), encoded)?;
        }
    }
    Ok(())
}

#[test]
fn version_encoding_preserves_full_width_values_and_framing() {
    let request = VersionRequest {
        request_id: Sequence(0x0102_0304_0506_0708),
        supported: ContractVersion {
            major: 0xabcd,
            minor: 0x1234,
        },
    };
    assert_eq!(
        request.encode(),
        [0xab, 0xcd, 0x12, 0x34, 1, 2, 3, 4, 5, 6, 7, 8, 0, 0, 0, 0xab, 0xcd, 0x12, 0x34,]
    );
    let response = VersionResponse {
        request_id: Sequence(u64::MAX),
        contract: ContractVersion {
            major: u16::MAX,
            minor: u16::MAX,
        },
        node_interface_major: u16::MAX,
    };
    assert_eq!(
        response.encode(),
        [
            0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0, 0, 0, 0, 0,
            0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
        ]
    );
}

#[test]
fn compatibility_gate_accepts_additions_and_rejects_mutation_or_removal() {
    let previous = [("record.Request.fields", "id:u64")];
    let additive = [
        ("record.Request.fields", "id:u64"),
        ("record.Response.fields", "id:u64"),
    ];
    assert_eq!(agent_api_compat_gate(1, 1, &previous, &additive), Ok(()));
    assert!(agent_api_compat_gate(1, 1, &previous, &[]).is_err());
    assert!(
        agent_api_compat_gate(1, 1, &previous, &[("record.Request.fields", "id:u128")]).is_err()
    );
    assert_eq!(agent_api_compat_gate(1, 2, &previous, &[]), Ok(()));
}

#[test]
fn custody_receipt_shapes_name_the_paxeer_settlement_domain() {
    let root = declarations(AGENT_API_V1_SOURCE);
    assert_eq!(
        root["type.SettlementDomain.variants"], "[\"Paxeer\"]",
        "Paxeer is the sole valid settlement domain in this contract version"
    );
    let write = declarations(WRITE_SOURCE);
    let read = declarations(READ_SOURCE);
    let stream = declarations(STREAM_SOURCE);
    let modules = [
        ("v1.kvx", &root),
        ("write.kvx", &write),
        ("read.kvx", &read),
        ("stream.kvx", &stream),
    ];
    assert_eq!(settlement_domain_gate(&root, &modules), Ok(()));
}

#[test]
fn untagged_or_foreign_custody_claims_are_rejected() {
    let root = declarations(AGENT_API_V1_SOURCE);
    let read = declarations(READ_SOURCE);
    let stream = declarations(STREAM_SOURCE);

    let stripped_source = WRITE_SOURCE.replace("Executed.settlement_domain = \"Paxeer\"\n", "");
    assert!(stripped_source.len() < WRITE_SOURCE.len());
    let stripped = declarations(&stripped_source);
    let untagged = settlement_domain_gate(
        &root,
        &[
            ("v1.kvx", &root),
            ("write.kvx", &stripped),
            ("read.kvx", &read),
            ("stream.kvx", &stream),
        ],
    );
    assert!(untagged.is_err_and(|error| error.contains("untagged custody claim")));

    let foreign_source = WRITE_SOURCE.replace(
        "Executed.settlement_domain = \"Paxeer\"",
        "Executed.settlement_domain = \"Ethereum\"",
    );
    let foreign = declarations(&foreign_source);
    let mistagged = settlement_domain_gate(
        &root,
        &[
            ("v1.kvx", &root),
            ("write.kvx", &foreign),
            ("read.kvx", &read),
            ("stream.kvx", &stream),
        ],
    );
    assert!(mistagged.is_err_and(|error| error.contains("foreign settlement domain")));

    let no_vocabulary = declarations(
        &AGENT_API_V1_SOURCE.replace("variants = [\"Paxeer\"]", "variants = [\"paxeer\"]"),
    );
    assert!(settlement_domain_gate(&no_vocabulary, &[]).is_err());
}

#[test]
fn adding_a_settlement_domain_is_additive_by_construction() {
    let previous = [(
        "type.SubmissionState.Executed.settlement_domain",
        "\"Paxeer\"",
    )];
    let with_second_domain = [
        (
            "type.SubmissionState.Executed.settlement_domain",
            "\"Paxeer\"",
        ),
        ("type.SettlementDomain.variants", "[\"Paxeer\",\"Solana\"]"),
    ];
    assert_eq!(
        agent_api_compat_gate(1, 1, &previous, &with_second_domain),
        Ok(())
    );
    assert!(agent_api_compat_gate(
        1,
        1,
        &previous,
        &[(
            "type.SubmissionState.Executed.settlement_domain",
            "\"Solana\""
        )]
    )
    .is_err());
    let baseline = declarations(BASELINE);
    assert_eq!(baseline["type.SettlementDomain.variants"], "[\"Paxeer\"]");
}

#[test]
fn consensus_integers_are_exact_and_dynamic_languages_are_not_numeric() {
    assert_eq!(
        Amount::parse_decimal("340282366920938463463374607431768211455"),
        Ok(Amount(u128::MAX))
    );
    assert!(Amount::parse_decimal("340282366920938463463374607431768211456").is_err());
    assert_eq!(
        BudgetLimit::parse_decimal("9007199254740993"),
        Ok(BudgetLimit(9_007_199_254_740_993))
    );
    assert_eq!(
        Sequence::parse_decimal("18446744073709551615"),
        Ok(Sequence(u64::MAX))
    );
    assert_eq!(
        TimestampSeconds::parse_decimal("42"),
        Ok(TimestampSeconds(42))
    );

    for (key, value) in declarations(AGENT_API_V1_SOURCE) {
        if key.ends_with(".consensus_integer") {
            assert_eq!(value, "true");
        }
        if key.ends_with(".typescript") {
            assert_eq!(value, "\"bigint\"");
        }
        if key.ends_with(".python") {
            assert_eq!(value, "\"int\"");
        }
    }
}
