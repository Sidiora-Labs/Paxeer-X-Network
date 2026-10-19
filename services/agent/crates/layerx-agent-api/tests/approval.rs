use std::collections::BTreeMap;

const SCHEMA: &str = include_str!("../../../schema/agent-api/approval.kvx");
const GOLDEN: &str = include_str!("../../../schema/agent-api/golden/approval.kvx");

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

fn decode_hex(value: &str) -> Vec<u8> {
    let value = value.trim_matches('"');
    assert_eq!(value.len() % 2, 0, "golden hex length must be even");
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
fn agent_api_approval_module() {
    let schema = declarations(SCHEMA);
    assert_eq!(schema["module.name"], "\"approval\"");
    assert_eq!(schema["module.contract_major"], "1");
    assert_eq!(schema["module.contract_minor"], "1");
    assert_eq!(schema["module.compatibility"], "\"additive_only\"");
    for operation in ["list", "get", "approve", "reject"] {
        assert!(schema.contains_key(&format!("operation.approval.{operation}.request")));
        assert!(schema.contains_key(&format!("operation.approval.{operation}.response")));
    }
    assert_eq!(
        schema["type.ApprovalRecord.required"],
        "[\"approval_id\",\"tenant\",\"held_activity\",\"canonical_bytes_digest\",\"hold_reason\",\"created_at\",\"expires_at\",\"state\"]"
    );
    assert!(schema["type.ApprovalRecord.expiry_rule"].contains("deterministically"));
}

#[test]
fn approval_contract_is_explicitly_daemon_enforced_without_protocol_authority() {
    let schema = declarations(SCHEMA);
    assert_eq!(
        schema["guarantee.ApprovalHold.enforcement"],
        "\"daemon_enforced\""
    );
    assert_eq!(
        schema["guarantee.ApprovalHold.authority"],
        "\"restriction_only\""
    );
    let notice = &schema["guarantee.ApprovalHold.notice"];
    assert!(notice.contains("confers no protocol authority"));
    assert!(notice.contains("bypassing the daemon bypasses the restriction"));
}

#[test]
fn every_approval_operation_and_lifecycle_event_has_exact_golden_bytes() {
    assert_eq!(SCHEMA, GOLDEN);
    let schema = declarations(GOLDEN);
    let vectors = [
        "golden.request.approval.list.encoded_hex",
        "golden.response.approval.list.encoded_hex",
        "golden.request.approval.get.encoded_hex",
        "golden.response.approval.get.encoded_hex",
        "golden.request.approval.approve.encoded_hex",
        "golden.response.approval.approve.encoded_hex",
        "golden.request.approval.reject.encoded_hex",
        "golden.response.approval.reject.encoded_hex",
        "golden.event.approval.created.encoded_hex",
        "golden.event.approval.granted.encoded_hex",
        "golden.event.approval.rejected.encoded_hex",
        "golden.event.approval.expired.encoded_hex",
        "golden.event.approval.defective.encoded_hex",
    ];
    for vector in vectors {
        let bytes = decode_hex(&schema[vector]);
        assert_eq!(
            bytes.first(),
            Some(&b'{'),
            "{vector} is not a canonical map"
        );
        assert_eq!(bytes.last(), Some(&b'}'), "{vector} is not a canonical map");
        std::str::from_utf8(&bytes)
            .unwrap_or_else(|error| panic!("{vector} is not canonical UTF-8: {error}"));
    }
}
