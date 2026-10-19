#![no_main]

use std::collections::{BTreeMap, BTreeSet};

use layerx_mcp::untrusted::BoundAuthority;
use layerx_mcp::validate::{
    arguments, ArgumentKind, ArgumentValue, FieldSchema, ToolSchema, UntrustedInput,
};
use libfuzzer_sys::fuzz_target;

const FIELDS: &[FieldSchema] = &[
    FieldSchema {
        name: "payload",
        kind: ArgumentKind::Bytes,
        required: true,
        maximum_bytes: 32_768,
    },
    FieldSchema {
        name: "amount",
        kind: ArgumentKind::ExactU128,
        required: true,
        maximum_bytes: 39,
    },
    FieldSchema {
        name: "memo",
        kind: ArgumentKind::Text,
        required: false,
        maximum_bytes: 4_096,
    },
    FieldSchema {
        name: "counterparty",
        kind: ArgumentKind::Identifier,
        required: true,
        maximum_bytes: 32,
    },
    FieldSchema {
        name: "approved",
        kind: ArgumentKind::Boolean,
        required: false,
        maximum_bytes: 1,
    },
];

fn bounded_text(data: &[u8]) -> String {
    String::from_utf8_lossy(&data[..data.len().min(4_096)]).into_owned()
}

fuzz_target!(|data: &[u8]| {
    let selector = data.first().copied().unwrap_or(0);
    let body = data.get(1..).unwrap_or_default();
    let operation = if selector & 1 == 0 {
        "prepare".to_owned()
    } else {
        bounded_text(body)
    };
    let counterparty = if selector & 2 == 0 {
        [1_u8; 32]
    } else {
        let mut identifier = [0_u8; 32];
        let copied = body.len().min(identifier.len());
        identifier[..copied].copy_from_slice(&body[..copied]);
        identifier
    };
    let mut operations = BTreeSet::new();
    operations.insert("prepare".to_owned());
    let Ok(authority) = BoundAuthority::new("tenant-a", operations, [1_u8; 32], true) else {
        return;
    };
    let mut fields = BTreeMap::new();
    fields.insert(
        "payload".to_owned(),
        ArgumentValue::Bytes(body[..body.len().min(32_768)].to_vec()),
    );
    fields.insert(
        "amount".to_owned(),
        ArgumentValue::ExactUnsigned(if selector & 4 == 0 {
            "1".to_owned()
        } else {
            bounded_text(body)
        }),
    );
    fields.insert(
        "counterparty".to_owned(),
        ArgumentValue::Identifier(counterparty),
    );
    if selector & 8 != 0 {
        fields.insert("memo".to_owned(), ArgumentValue::Text(bounded_text(body)));
    }
    if selector & 16 != 0 {
        fields.insert(
            "unexpected".to_owned(),
            ArgumentValue::Boolean(selector & 32 != 0),
        );
    } else {
        fields.insert(
            "approved".to_owned(),
            ArgumentValue::Boolean(selector & 32 != 0),
        );
    }
    let _ = arguments(
        &authority,
        ToolSchema {
            operation: "prepare",
            fields: FIELDS,
        },
        UntrustedInput {
            operation,
            counterparty,
            tenant_override: (selector & 64 != 0).then(|| bounded_text(body)),
            scope_override: (selector & 128 != 0).then(|| bounded_text(body)),
            approval_override: Some(selector & 32 != 0),
            model_text: bounded_text(body),
            resource_text: bounded_text(body),
            tool_result_text: bounded_text(body),
            fields,
        },
    );
});
