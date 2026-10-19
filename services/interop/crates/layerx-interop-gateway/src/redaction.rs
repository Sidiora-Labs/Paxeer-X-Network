//! Redaction by construction for everything the gateway emits, mirroring the
//! human plane's enforcement exactly. Log lines, metrics and trace spans are
//! built from a closed vocabulary of non-sensitive values, every emission is
//! validated against the declared schema registry, and the registry itself is
//! gated so no schema can declare a field able to carry secrets, key
//! material, personal data or financial values.

use std::collections::BTreeSet;
use std::fmt::{Display, Formatter};

use crate::codec::{self, CodecError, Decoder};
use crate::trace::TraceId;

const EMISSION_MAGIC: &[u8; 4] = b"LXIE";
const EMISSION_VERSION: u8 = 1;
const NAME_LIMIT: usize = 64;

/// The span emitted around every adapter translation the gateway handles.
pub const ADAPTER_CALL_SCHEMA: &str = "adapter-call";
/// The log line emitted when an audit entry is appended.
pub const AUDIT_APPEND_SCHEMA: &str = "audit-append";
/// The log line emitted when a typed error response is returned.
pub const ERROR_RESPONSE_SCHEMA: &str = "error-response";

/// Field names no emitted schema may declare, closing the channels through
/// which sensitive material most commonly leaks.
pub const BANNED_FIELD_NAMES: &[&str] = &[
    "account", "address", "amount", "balance", "email", "iban", "key", "memo", "name", "owner",
    "payload", "phone", "preimage", "secret", "token", "value",
];

/// Every schema the gateway is permitted to emit. An emission outside this
/// registry is unencodable and undecodable.
pub const EMITTED_SCHEMAS: &[EmissionSchema] = &[
    EmissionSchema {
        name: ADAPTER_CALL_SCHEMA,
        fields: &[
            SchemaField {
                name: "adapter",
                class: SafeClass::Label,
            },
            SchemaField {
                name: "operation",
                class: SafeClass::Label,
            },
            SchemaField {
                name: "outcome",
                class: SafeClass::Label,
            },
            SchemaField {
                name: "trace",
                class: SafeClass::Trace,
            },
        ],
    },
    EmissionSchema {
        name: AUDIT_APPEND_SCHEMA,
        fields: &[
            SchemaField {
                name: "sequence",
                class: SafeClass::Count,
            },
            SchemaField {
                name: "kind",
                class: SafeClass::Label,
            },
            SchemaField {
                name: "link",
                class: SafeClass::Digest,
            },
            SchemaField {
                name: "trace",
                class: SafeClass::Trace,
            },
        ],
    },
    EmissionSchema {
        name: ERROR_RESPONSE_SCHEMA,
        fields: &[
            SchemaField {
                name: "code",
                class: SafeClass::Label,
            },
            SchemaField {
                name: "retriability",
                class: SafeClass::Label,
            },
            SchemaField {
                name: "trace",
                class: SafeClass::Trace,
            },
        ],
    },
];

fn valid_name(value: &str) -> bool {
    !value.is_empty()
        && value.len() <= NAME_LIMIT
        && value.bytes().all(|byte| {
            byte.is_ascii_lowercase() || byte.is_ascii_digit() || byte == b'-' || byte == b'_'
        })
}

/// The closed set of value classes an emission field can declare. No class
/// exists for free text, amounts or any other sensitive shape.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum SafeClass {
    Digest,
    Label,
    Count,
    DurationMs,
    Trace,
}

impl SafeClass {
    const fn code(self) -> u8 {
        match self {
            Self::Digest => 1,
            Self::Label => 2,
            Self::Count => 3,
            Self::DurationMs => 4,
            Self::Trace => 5,
        }
    }

    fn from_code(value: u8) -> Result<Self, RedactionError> {
        match value {
            1 => Ok(Self::Digest),
            2 => Ok(Self::Label),
            3 => Ok(Self::Count),
            4 => Ok(Self::DurationMs),
            5 => Ok(Self::Trace),
            _ => Err(RedactionError::Corrupt("unknown value class")),
        }
    }
}

/// A bounded machine label limited to `a-z`, `0-9`, `-` and `_`, the only
/// text-shaped value an emission can carry.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Label(String);

impl Label {
    /// Creates a bounded label.
    ///
    /// # Errors
    ///
    /// Rejects empty, oversize and out-of-charset values.
    pub fn new(value: impl Into<String>) -> Result<Self, RedactionError> {
        let value = value.into();
        if valid_name(&value) {
            Ok(Self(value))
        } else {
            Err(RedactionError::InvalidLabel)
        }
    }

    /// Returns the label.
    #[must_use]
    pub fn as_str(&self) -> &str {
        &self.0
    }
}

/// A value an emission can carry. Secrets, key material, personal data and
/// financial values are unrepresentable in this vocabulary.
#[derive(Clone, Debug, Eq, PartialEq)]
pub enum FieldValue {
    Digest([u8; 32]),
    Label(Label),
    Count(u64),
    DurationMs(u64),
    Trace(TraceId),
}

impl FieldValue {
    /// Returns the value's class.
    #[must_use]
    pub const fn class(&self) -> SafeClass {
        match self {
            Self::Digest(_) => SafeClass::Digest,
            Self::Label(_) => SafeClass::Label,
            Self::Count(_) => SafeClass::Count,
            Self::DurationMs(_) => SafeClass::DurationMs,
            Self::Trace(_) => SafeClass::Trace,
        }
    }
}

/// One declared field of an emitted schema.
#[derive(Clone, Copy, Debug)]
pub struct SchemaField {
    /// The field name.
    pub name: &'static str,
    /// The class of value the field carries.
    pub class: SafeClass,
}

/// One schema the gateway is allowed to emit.
#[derive(Clone, Copy, Debug)]
pub struct EmissionSchema {
    /// The schema name.
    pub name: &'static str,
    /// The declared fields in emission order.
    pub fields: &'static [SchemaField],
}

/// Verifies one schema against the redaction rules: bounded machine names
/// only, no banned field name, no duplicate field, no empty schema.
///
/// # Errors
///
/// Returns the first violated rule.
pub fn verify_schema(schema: &EmissionSchema) -> Result<(), RedactionError> {
    if !valid_name(schema.name) {
        return Err(RedactionError::InvalidName);
    }
    if schema.fields.is_empty() {
        return Err(RedactionError::EmptySchema);
    }
    let mut seen = BTreeSet::new();
    for field in schema.fields {
        if !valid_name(field.name) {
            return Err(RedactionError::InvalidName);
        }
        if BANNED_FIELD_NAMES.contains(&field.name) {
            return Err(RedactionError::BannedField);
        }
        if !seen.insert(field.name) {
            return Err(RedactionError::DuplicateField);
        }
    }
    Ok(())
}

/// The gate over emitted schemas: verifies every registered schema and
/// refuses duplicate registrations. A failure here is a build defect.
///
/// # Errors
///
/// Returns the first violated rule.
pub fn verify_registry() -> Result<(), RedactionError> {
    let mut seen = BTreeSet::new();
    for schema in EMITTED_SCHEMAS {
        verify_schema(schema)?;
        if !seen.insert(schema.name) {
            return Err(RedactionError::DuplicateSchema);
        }
    }
    Ok(())
}

fn lookup(name: &str) -> Result<&'static EmissionSchema, RedactionError> {
    EMITTED_SCHEMAS
        .iter()
        .find(|schema| schema.name == name)
        .ok_or(RedactionError::UnknownSchema)
}

/// Encodes one emission against its declared schema, refusing anything the
/// schema does not declare.
///
/// # Errors
///
/// Refuses unknown schemas, arity mismatches, class mismatches and encoding
/// bound overflows.
pub fn emit(schema_name: &str, values: &[FieldValue]) -> Result<Vec<u8>, RedactionError> {
    let schema = lookup(schema_name)?;
    if values.len() != schema.fields.len() {
        return Err(RedactionError::SchemaMismatch);
    }
    for (field, value) in schema.fields.iter().zip(values) {
        if field.class != value.class() {
            return Err(RedactionError::SchemaMismatch);
        }
    }
    let mut output = Vec::new();
    output.extend_from_slice(EMISSION_MAGIC);
    output.push(EMISSION_VERSION);
    codec::push_bytes(&mut output, schema.name.as_bytes())?;
    codec::push_length(&mut output, values.len())?;
    for value in values {
        encode_value(&mut output, value)?;
    }
    Ok(output)
}

fn encode_value(output: &mut Vec<u8>, value: &FieldValue) -> Result<(), RedactionError> {
    output.push(value.class().code());
    match value {
        FieldValue::Digest(digest) => output.extend_from_slice(digest),
        FieldValue::Label(label) => codec::push_bytes(output, label.as_str().as_bytes())?,
        FieldValue::Count(count) => output.extend_from_slice(&count.to_be_bytes()),
        FieldValue::DurationMs(duration) => output.extend_from_slice(&duration.to_be_bytes()),
        FieldValue::Trace(trace) => codec::push_bytes(output, trace.as_str().as_bytes())?,
    }
    Ok(())
}

/// One decoded emission with its registered schema.
#[derive(Clone, Debug)]
pub struct Emission {
    schema: &'static EmissionSchema,
    values: Vec<FieldValue>,
}

impl Emission {
    /// Returns the registered schema the emission was validated against.
    #[must_use]
    pub const fn schema(&self) -> &'static EmissionSchema {
        self.schema
    }

    /// Returns the values in declared field order.
    #[must_use]
    pub fn values(&self) -> &[FieldValue] {
        &self.values
    }
}

/// Decodes one emission, re-validating it against the schema registry so a
/// foreign or drifted emission is refused rather than interpreted.
///
/// # Errors
///
/// Refuses unregistered schemas, class drift and malformed bytes.
pub fn decode(bytes: &[u8]) -> Result<Emission, RedactionError> {
    let mut reader = Decoder::new(bytes);
    if reader.take(4)? != EMISSION_MAGIC {
        return Err(RedactionError::Corrupt("invalid emission header"));
    }
    if reader.byte()? != EMISSION_VERSION {
        return Err(RedactionError::Corrupt("unknown emission version"));
    }
    let name = reader.text()?;
    let schema = lookup(name)?;
    let count = reader.length()?;
    if count != schema.fields.len() {
        return Err(RedactionError::SchemaMismatch);
    }
    let mut values = Vec::with_capacity(count);
    for field in schema.fields {
        let value = decode_value(&mut reader)?;
        if value.class() != field.class {
            return Err(RedactionError::SchemaMismatch);
        }
        values.push(value);
    }
    if !reader.is_empty() {
        return Err(RedactionError::Corrupt("trailing bytes"));
    }
    Ok(Emission { schema, values })
}

fn decode_value(reader: &mut Decoder<'_>) -> Result<FieldValue, RedactionError> {
    match SafeClass::from_code(reader.byte()?)? {
        SafeClass::Digest => Ok(FieldValue::Digest(reader.array()?)),
        SafeClass::Label => Ok(FieldValue::Label(Label::new(reader.text()?)?)),
        SafeClass::Count => Ok(FieldValue::Count(reader.u64()?)),
        SafeClass::DurationMs => Ok(FieldValue::DurationMs(reader.u64()?)),
        SafeClass::Trace => {
            let trace = TraceId::parse(reader.text()?)
                .map_err(|_| RedactionError::Corrupt("malformed trace identifier"))?;
            Ok(FieldValue::Trace(trace))
        }
    }
}

/// Redaction failures.
#[derive(Debug)]
pub enum RedactionError {
    UnknownSchema,
    SchemaMismatch,
    InvalidLabel,
    InvalidName,
    BannedField,
    DuplicateField,
    DuplicateSchema,
    EmptySchema,
    Corrupt(&'static str),
    SizeOverflow,
}

impl Display for RedactionError {
    fn fmt(&self, formatter: &mut Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::UnknownSchema => formatter.write_str("emission schema is not registered"),
            Self::SchemaMismatch => {
                formatter.write_str("emission does not match its declared schema")
            }
            Self::InvalidLabel => formatter.write_str("invalid emission label"),
            Self::InvalidName => formatter.write_str("invalid schema or field name"),
            Self::BannedField => formatter.write_str("schema declares a banned field name"),
            Self::DuplicateField => formatter.write_str("schema declares a duplicate field"),
            Self::DuplicateSchema => formatter.write_str("schema registered twice"),
            Self::EmptySchema => formatter.write_str("schema declares no fields"),
            Self::Corrupt(reason) => write!(formatter, "corrupt emission: {reason}"),
            Self::SizeOverflow => formatter.write_str("emission exceeds encoding bounds"),
        }
    }
}

impl std::error::Error for RedactionError {}

impl From<CodecError> for RedactionError {
    fn from(value: CodecError) -> Self {
        match value {
            CodecError::Truncated => Self::Corrupt("truncated emission"),
            CodecError::Overflow => Self::SizeOverflow,
        }
    }
}

#[cfg(test)]
mod tests {
    use super::{
        decode, emit, verify_registry, verify_schema, EmissionSchema, FieldValue, Label,
        RedactionError, SafeClass, SchemaField, ADAPTER_CALL_SCHEMA,
    };
    use crate::trace::TraceId;

    #[test]
    fn emitted_schemas_pass_the_redaction_gate() {
        verify_registry().unwrap_or_else(|error| panic!("registry rejected: {error}"));
    }

    #[test]
    fn schemas_carrying_sensitive_channels_are_refused() {
        let banned = EmissionSchema {
            name: "hostile",
            fields: &[SchemaField {
                name: "amount",
                class: SafeClass::Count,
            }],
        };
        assert!(matches!(
            verify_schema(&banned),
            Err(RedactionError::BannedField)
        ));
        let duplicate = EmissionSchema {
            name: "hostile",
            fields: &[
                SchemaField {
                    name: "outcome",
                    class: SafeClass::Label,
                },
                SchemaField {
                    name: "outcome",
                    class: SafeClass::Label,
                },
            ],
        };
        assert!(matches!(
            verify_schema(&duplicate),
            Err(RedactionError::DuplicateField)
        ));
        let empty = EmissionSchema {
            name: "hostile",
            fields: &[],
        };
        assert!(matches!(
            verify_schema(&empty),
            Err(RedactionError::EmptySchema)
        ));
        let foreign_name = EmissionSchema {
            name: "Hostile Schema",
            fields: &[SchemaField {
                name: "outcome",
                class: SafeClass::Label,
            }],
        };
        assert!(matches!(
            verify_schema(&foreign_name),
            Err(RedactionError::InvalidName)
        ));
    }

    #[test]
    fn emissions_outside_the_declared_schema_are_unencodable() {
        let trace = TraceId::mint([7; 16]);
        let label = |value: &str| {
            FieldValue::Label(
                Label::new(value).unwrap_or_else(|error| panic!("label {value}: {error}")),
            )
        };
        assert!(matches!(
            emit("unregistered", &[label("x")]),
            Err(RedactionError::UnknownSchema)
        ));
        assert!(matches!(
            emit(ADAPTER_CALL_SCHEMA, &[label("x402")]),
            Err(RedactionError::SchemaMismatch)
        ));
        assert!(matches!(
            emit(
                ADAPTER_CALL_SCHEMA,
                &[
                    label("x402"),
                    label("begin"),
                    FieldValue::Count(1),
                    FieldValue::Trace(trace.clone()),
                ]
            ),
            Err(RedactionError::SchemaMismatch)
        ));
        let encoded = emit(
            ADAPTER_CALL_SCHEMA,
            &[
                label("x402"),
                label("begin"),
                label("accepted"),
                FieldValue::Trace(trace.clone()),
            ],
        )
        .unwrap_or_else(|error| panic!("valid emission refused: {error}"));
        let emission =
            decode(&encoded).unwrap_or_else(|error| panic!("valid emission unreadable: {error}"));
        assert_eq!(emission.schema().name, ADAPTER_CALL_SCHEMA);
        assert_eq!(emission.values()[3], FieldValue::Trace(trace));
    }

    #[test]
    fn free_text_is_unrepresentable_in_the_emission_vocabulary() {
        assert!(matches!(
            Label::new("PAN 4111 1111 1111 1111"),
            Err(RedactionError::InvalidLabel)
        ));
        assert!(matches!(Label::new(""), Err(RedactionError::InvalidLabel)));
    }
}
