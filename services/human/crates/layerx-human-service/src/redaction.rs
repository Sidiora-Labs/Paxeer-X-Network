//! Redaction by construction for everything this service emits. Log lines,
//! metrics and trace spans are built from a closed vocabulary of
//! non-sensitive values, every emission is validated against the declared
//! schema registry, and the registry itself is gated so no schema can declare
//! a field able to carry secrets, key material, personal data or financial
//! values.

use std::collections::BTreeSet;
use std::fmt::{Display, Formatter};

use crate::store::{PrincipalScope, RowKey, StoreError, Table};
use crate::trace::TraceId;

const EMISSION_MAGIC: &[u8; 4] = b"LXTE";
const EMISSION_VERSION: u8 = 1;
const NAME_LIMIT: usize = 64;

/// The funnel-stage emission recording stage-level journey outcomes.
pub const FUNNEL_STAGE_SCHEMA: &str = "funnel-stage";
/// The log line emitted when an audit entry is appended.
pub const AUDIT_APPEND_SCHEMA: &str = "audit-append";
/// The span emitted around every call into the agent layer.
pub const AGENT_CALL_SCHEMA: &str = "agent-call";
/// The log line emitted when a typed error response is returned.
pub const ERROR_RESPONSE_SCHEMA: &str = "error-response";

/// Field names no emitted schema may declare, closing the channels through
/// which sensitive material most commonly leaks.
pub const BANNED_FIELD_NAMES: &[&str] = &[
    "account", "address", "amount", "balance", "email", "iban", "key", "memo", "name", "owner",
    "payload", "phone", "preimage", "secret", "token", "value",
];

/// Every schema this service is permitted to emit. An emission outside this
/// registry is unencodable and undecodable.
pub const EMITTED_SCHEMAS: &[EmissionSchema] = &[
    EmissionSchema {
        name: FUNNEL_STAGE_SCHEMA,
        fields: &[
            SchemaField {
                name: "funnel",
                class: SafeClass::Label,
            },
            SchemaField {
                name: "stage",
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
            SchemaField {
                name: "elapsed_ms",
                class: SafeClass::DurationMs,
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
        name: AGENT_CALL_SCHEMA,
        fields: &[
            SchemaField {
                name: "operation",
                class: SafeClass::Label,
            },
            SchemaField {
                name: "outcome",
                class: SafeClass::Label,
            },
            SchemaField {
                name: "elapsed_ms",
                class: SafeClass::DurationMs,
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

/// One schema this service is allowed to emit.
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
    push_str(&mut output, schema.name)?;
    push_length(&mut output, values.len())?;
    for value in values {
        encode_value(&mut output, value)?;
    }
    Ok(output)
}

fn encode_value(output: &mut Vec<u8>, value: &FieldValue) -> Result<(), RedactionError> {
    output.push(value.class().code());
    match value {
        FieldValue::Digest(digest) => output.extend_from_slice(digest),
        FieldValue::Label(label) => push_str(output, label.as_str())?,
        FieldValue::Count(count) => output.extend_from_slice(&count.to_be_bytes()),
        FieldValue::DurationMs(duration) => output.extend_from_slice(&duration.to_be_bytes()),
        FieldValue::Trace(trace) => push_str(output, trace.as_str())?,
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
    let mut reader = Reader::new(bytes);
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

fn decode_value(reader: &mut Reader<'_>) -> Result<FieldValue, RedactionError> {
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

/// The journey funnels instrumented with stage-level outcomes.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum Funnel {
    Onboarding,
    Deposit,
    Move,
    CreateAgent,
    Approval,
    Withdrawal,
}

/// Every instrumented funnel in declaration order.
pub const FUNNELS: [Funnel; 6] = [
    Funnel::Onboarding,
    Funnel::Deposit,
    Funnel::Move,
    Funnel::CreateAgent,
    Funnel::Approval,
    Funnel::Withdrawal,
];

impl Funnel {
    /// Returns the funnel's emission label.
    #[must_use]
    pub const fn label(self) -> &'static str {
        match self {
            Self::Onboarding => "onboarding",
            Self::Deposit => "deposit",
            Self::Move => "move",
            Self::CreateAgent => "create-agent",
            Self::Approval => "approval",
            Self::Withdrawal => "withdrawal",
        }
    }

    /// Returns the funnel's declared stages in journey order.
    #[must_use]
    pub const fn stages(self) -> &'static [&'static str] {
        match self {
            Self::Onboarding => &["credential", "registration", "recovery", "active"],
            Self::Deposit => &["wallet", "confirming", "crediting", "done"],
            Self::Move => &["route", "review", "legs", "done"],
            Self::CreateAgent => &["request", "orchestration", "active"],
            Self::Approval => &["opened", "decision", "settled"],
            Self::Withdrawal => &["processing", "settlement", "claim", "paid"],
        }
    }

    fn from_label(label: &str) -> Result<Self, RedactionError> {
        FUNNELS
            .into_iter()
            .find(|funnel| funnel.label() == label)
            .ok_or(RedactionError::Corrupt("unknown funnel"))
    }
}

/// The outcome of one funnel stage, making drop-off and failure measurable
/// per stage.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum StageOutcome {
    Completed,
    Failed,
    Abandoned,
}

impl StageOutcome {
    /// Returns the outcome's emission label.
    #[must_use]
    pub const fn label(self) -> &'static str {
        match self {
            Self::Completed => "completed",
            Self::Failed => "failed",
            Self::Abandoned => "abandoned",
        }
    }

    fn from_label(label: &str) -> Result<Self, RedactionError> {
        match label {
            "completed" => Ok(Self::Completed),
            "failed" => Ok(Self::Failed),
            "abandoned" => Ok(Self::Abandoned),
            _ => Err(RedactionError::Corrupt("unknown stage outcome")),
        }
    }
}

/// One stage-level funnel outcome, carrying nothing beyond the declared
/// funnel vocabulary, the trace and a duration.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct StageRecord {
    funnel: Funnel,
    stage: &'static str,
    outcome: StageOutcome,
    trace: TraceId,
    elapsed_ms: u64,
}

impl StageRecord {
    /// Builds a stage record, admitting only stages the funnel declares.
    ///
    /// # Errors
    ///
    /// Refuses a stage outside the funnel's declared stage list.
    pub fn new(
        funnel: Funnel,
        stage: &str,
        outcome: StageOutcome,
        trace: TraceId,
        elapsed_ms: u64,
    ) -> Result<Self, RedactionError> {
        let stage = funnel
            .stages()
            .iter()
            .copied()
            .find(|candidate| *candidate == stage)
            .ok_or(RedactionError::ForeignStage)?;
        Ok(Self {
            funnel,
            stage,
            outcome,
            trace,
            elapsed_ms,
        })
    }

    /// Returns the funnel.
    #[must_use]
    pub const fn funnel(&self) -> Funnel {
        self.funnel
    }

    /// Returns the stage name.
    #[must_use]
    pub const fn stage(&self) -> &'static str {
        self.stage
    }

    /// Returns the stage outcome.
    #[must_use]
    pub const fn outcome(&self) -> StageOutcome {
        self.outcome
    }

    /// Returns the trace the stage ran under.
    #[must_use]
    pub const fn trace(&self) -> &TraceId {
        &self.trace
    }

    /// Returns the elapsed stage duration in milliseconds.
    #[must_use]
    pub const fn elapsed_ms(&self) -> u64 {
        self.elapsed_ms
    }

    fn emission(&self) -> Result<Vec<u8>, RedactionError> {
        emit(
            FUNNEL_STAGE_SCHEMA,
            &[
                FieldValue::Label(Label::new(self.funnel.label())?),
                FieldValue::Label(Label::new(self.stage)?),
                FieldValue::Label(Label::new(self.outcome.label())?),
                FieldValue::Trace(self.trace.clone()),
                FieldValue::DurationMs(self.elapsed_ms),
            ],
        )
    }
}

/// Persists one funnel stage outcome into the principal's telemetry buffer
/// through the schema-gated emission path.
///
/// # Errors
///
/// Returns emission refusals and store persistence failures.
pub fn record_stage(
    scope: &mut PrincipalScope<'_>,
    key: RowKey,
    now: u64,
    record: &StageRecord,
) -> Result<(), RedactionError> {
    let bytes = record.emission()?;
    scope.put(Table::Telemetry, key, now, bytes)?;
    Ok(())
}

/// Decodes one recorded funnel stage outcome.
///
/// # Errors
///
/// Refuses bytes that are not a valid funnel-stage emission.
pub fn read_stage(bytes: &[u8]) -> Result<StageRecord, RedactionError> {
    let emission = decode(bytes)?;
    if emission.schema().name != FUNNEL_STAGE_SCHEMA {
        return Err(RedactionError::SchemaMismatch);
    }
    let [FieldValue::Label(funnel), FieldValue::Label(stage), FieldValue::Label(outcome), FieldValue::Trace(trace), FieldValue::DurationMs(elapsed_ms)] =
        emission.values()
    else {
        return Err(RedactionError::Corrupt("malformed funnel-stage emission"));
    };
    StageRecord::new(
        Funnel::from_label(funnel.as_str())?,
        stage.as_str(),
        StageOutcome::from_label(outcome.as_str())?,
        trace.clone(),
        *elapsed_ms,
    )
}

/// Redaction failures.
#[derive(Debug)]
pub enum RedactionError {
    Store(StoreError),
    UnknownSchema,
    SchemaMismatch,
    InvalidLabel,
    InvalidName,
    BannedField,
    DuplicateField,
    DuplicateSchema,
    EmptySchema,
    ForeignStage,
    Corrupt(&'static str),
    SizeOverflow,
}

impl Display for RedactionError {
    fn fmt(&self, formatter: &mut Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::Store(error) => write!(formatter, "telemetry store failure: {error}"),
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
            Self::ForeignStage => {
                formatter.write_str("stage is not declared by the funnel it names")
            }
            Self::Corrupt(reason) => write!(formatter, "corrupt emission: {reason}"),
            Self::SizeOverflow => formatter.write_str("emission exceeds encoding bounds"),
        }
    }
}

impl std::error::Error for RedactionError {}

impl From<StoreError> for RedactionError {
    fn from(value: StoreError) -> Self {
        Self::Store(value)
    }
}

fn push_length(output: &mut Vec<u8>, value: usize) -> Result<(), RedactionError> {
    let value = u32::try_from(value).map_err(|_| RedactionError::SizeOverflow)?;
    output.extend_from_slice(&value.to_be_bytes());
    Ok(())
}

fn push_str(output: &mut Vec<u8>, value: &str) -> Result<(), RedactionError> {
    push_length(output, value.len())?;
    output.extend_from_slice(value.as_bytes());
    Ok(())
}

struct Reader<'a> {
    bytes: &'a [u8],
    offset: usize,
}

impl<'a> Reader<'a> {
    const fn new(bytes: &'a [u8]) -> Self {
        Self { bytes, offset: 0 }
    }

    fn take(&mut self, length: usize) -> Result<&'a [u8], RedactionError> {
        let end = self
            .offset
            .checked_add(length)
            .ok_or(RedactionError::SizeOverflow)?;
        let value = self
            .bytes
            .get(self.offset..end)
            .ok_or(RedactionError::Corrupt("truncated emission"))?;
        self.offset = end;
        Ok(value)
    }

    fn byte(&mut self) -> Result<u8, RedactionError> {
        self.take(1)?
            .first()
            .copied()
            .ok_or(RedactionError::Corrupt("truncated emission"))
    }

    fn u64(&mut self) -> Result<u64, RedactionError> {
        let mut encoded = [0_u8; 8];
        encoded.copy_from_slice(self.take(8)?);
        Ok(u64::from_be_bytes(encoded))
    }

    fn length(&mut self) -> Result<usize, RedactionError> {
        let mut encoded = [0_u8; 4];
        encoded.copy_from_slice(self.take(4)?);
        usize::try_from(u32::from_be_bytes(encoded)).map_err(|_| RedactionError::SizeOverflow)
    }

    fn array(&mut self) -> Result<[u8; 32], RedactionError> {
        let mut value = [0_u8; 32];
        value.copy_from_slice(self.take(32)?);
        Ok(value)
    }

    fn text(&mut self) -> Result<&'a str, RedactionError> {
        let length = self.length()?;
        std::str::from_utf8(self.take(length)?)
            .map_err(|_| RedactionError::Corrupt("emission text is not UTF-8"))
    }

    const fn is_empty(&self) -> bool {
        self.offset == self.bytes.len()
    }
}

#[cfg(test)]
mod tests {
    use super::{
        verify_registry, verify_schema, EmissionSchema, RedactionError, SafeClass, SchemaField,
    };

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
                    name: "stage",
                    class: SafeClass::Label,
                },
                SchemaField {
                    name: "stage",
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
                name: "stage",
                class: SafeClass::Label,
            }],
        };
        assert!(matches!(
            verify_schema(&foreign_name),
            Err(RedactionError::InvalidName)
        ));
    }
}
