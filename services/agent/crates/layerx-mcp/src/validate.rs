//! Contract-schema validation for every untrusted MCP argument.

use std::collections::{BTreeMap, BTreeSet};

use crate::untrusted::{
    BoundAuthority, ToolArguments, ValidatedArguments as AuthorityArguments, ValidationError,
};

const MAX_TOTAL_FIELD_BYTES: usize = 65_536;

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum ArgumentKind {
    Text,
    Bytes,
    ExactU128,
    Identifier,
    Boolean,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct FieldSchema {
    pub name: &'static str,
    pub kind: ArgumentKind,
    pub required: bool,
    pub maximum_bytes: usize,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct ToolSchema {
    pub operation: &'static str,
    pub fields: &'static [FieldSchema],
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub enum ArgumentValue {
    Text(String),
    Bytes(Vec<u8>),
    ExactUnsigned(String),
    Identifier([u8; 32]),
    Boolean(bool),
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub enum ValidatedValue {
    Text(String),
    Bytes(Vec<u8>),
    ExactU128(u128),
    Identifier([u8; 32]),
    Boolean(bool),
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct UntrustedInput {
    pub operation: String,
    pub counterparty: [u8; 32],
    pub tenant_override: Option<String>,
    pub scope_override: Option<String>,
    pub approval_override: Option<bool>,
    pub model_text: String,
    pub resource_text: String,
    pub tool_result_text: String,
    pub fields: BTreeMap<String, ArgumentValue>,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ValidatedToolArguments {
    pub authority: AuthorityArguments,
    pub fields: BTreeMap<String, ValidatedValue>,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum ArgumentError {
    InvalidSchema,
    Authority(ValidationError),
    MissingField,
    UnexpectedField,
    WrongType,
    InvalidValue,
    Oversized,
    Arithmetic,
}

/// Validates authority against daemon-held state and every field against the tool schema.
///
/// # Errors
///
/// Rejects authority overrides, unexpected or missing fields, wrong types, non-canonical exact
/// integers, NUL-bearing text, and every per-field or aggregate bound violation.
pub fn arguments(
    authority: &BoundAuthority,
    schema: ToolSchema,
    input: UntrustedInput,
) -> Result<ValidatedToolArguments, ArgumentError> {
    validate_schema(schema)?;
    if input.operation != schema.operation {
        return Err(ArgumentError::InvalidSchema);
    }
    let authority = crate::untrusted::validate(
        authority,
        ToolArguments {
            operation: input.operation,
            counterparty: input.counterparty,
            tenant_override: input.tenant_override,
            scope_override: input.scope_override,
            approval_override: input.approval_override,
            model_text: input.model_text,
            resource_text: input.resource_text,
            tool_result_text: input.tool_result_text,
        },
    )
    .map_err(ArgumentError::Authority)?;

    for field in input.fields.keys() {
        if !schema.fields.iter().any(|declared| declared.name == field) {
            return Err(ArgumentError::UnexpectedField);
        }
    }
    let mut total_bytes = 0_usize;
    let mut fields = BTreeMap::new();
    for declared in schema.fields {
        let value = input.fields.get(declared.name);
        let Some(value) = value else {
            if declared.required {
                return Err(ArgumentError::MissingField);
            }
            continue;
        };
        let (validated, encoded_bytes) = validate_value(*declared, value)?;
        total_bytes = total_bytes
            .checked_add(encoded_bytes)
            .ok_or(ArgumentError::Arithmetic)?;
        if total_bytes > MAX_TOTAL_FIELD_BYTES {
            return Err(ArgumentError::Oversized);
        }
        fields.insert(declared.name.to_owned(), validated);
    }
    Ok(ValidatedToolArguments { authority, fields })
}

fn validate_schema(schema: ToolSchema) -> Result<(), ArgumentError> {
    if !canonical_name(schema.operation) || schema.fields.is_empty() {
        return Err(ArgumentError::InvalidSchema);
    }
    let mut names = BTreeSet::new();
    for field in schema.fields {
        if !canonical_name(field.name)
            || field.maximum_bytes == 0
            || field.maximum_bytes > MAX_TOTAL_FIELD_BYTES
            || !names.insert(field.name)
        {
            return Err(ArgumentError::InvalidSchema);
        }
    }
    Ok(())
}

fn validate_value(
    schema: FieldSchema,
    value: &ArgumentValue,
) -> Result<(ValidatedValue, usize), ArgumentError> {
    let (value, length) = match (schema.kind, value) {
        (ArgumentKind::Text, ArgumentValue::Text(value)) => {
            if value.as_bytes().contains(&0) {
                return Err(ArgumentError::InvalidValue);
            }
            (ValidatedValue::Text(value.clone()), value.len())
        }
        (ArgumentKind::Bytes, ArgumentValue::Bytes(value)) => {
            (ValidatedValue::Bytes(value.clone()), value.len())
        }
        (ArgumentKind::ExactU128, ArgumentValue::ExactUnsigned(value)) => {
            if !canonical_unsigned(value) {
                return Err(ArgumentError::InvalidValue);
            }
            let parsed = value
                .parse::<u128>()
                .map_err(|_| ArgumentError::InvalidValue)?;
            (ValidatedValue::ExactU128(parsed), value.len())
        }
        (ArgumentKind::Identifier, ArgumentValue::Identifier(value)) => {
            if *value == [0; 32] {
                return Err(ArgumentError::InvalidValue);
            }
            (ValidatedValue::Identifier(*value), 32)
        }
        (ArgumentKind::Boolean, ArgumentValue::Boolean(value)) => {
            (ValidatedValue::Boolean(*value), 1)
        }
        _ => return Err(ArgumentError::WrongType),
    };
    if length > schema.maximum_bytes {
        return Err(ArgumentError::Oversized);
    }
    Ok((value, length))
}

fn canonical_name(value: &str) -> bool {
    !value.is_empty()
        && value.len() <= 128
        && value.bytes().all(|byte| {
            byte.is_ascii_lowercase()
                || byte.is_ascii_digit()
                || matches!(byte, b'.' | b'_' | b'-' | b':')
        })
}

fn canonical_unsigned(value: &str) -> bool {
    !value.is_empty()
        && value.bytes().all(|byte| byte.is_ascii_digit())
        && (value == "0" || !value.starts_with('0'))
}
