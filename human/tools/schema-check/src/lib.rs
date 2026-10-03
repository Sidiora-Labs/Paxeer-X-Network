use std::collections::BTreeMap;
use std::fmt::Write as _;
use std::fs;
use std::path::{Path, PathBuf};

pub const BASELINE_FILE: &str = "baseline.kvx";

pub const REFRESH_COMMAND: &str = "cargo run --manifest-path human/tools/schema-check/Cargo.toml --locked -- --write-baseline human/schema/human-api";

const PRIMITIVES: &[&str] = &["string", "boolean", "integer", "object"];

const METHODS: &[&str] = &["GET", "POST", "PUT", "PATCH", "DELETE"];

const SCALAR_FORMATS: &[&str] = &["decimal", "currency", "rfc3339-utc", "copy-key"];

#[derive(Debug, Eq, PartialEq)]
pub struct Violation {
    pub path: PathBuf,
    pub rule: &'static str,
    pub detail: String,
}

#[derive(Debug, Eq, PartialEq)]
pub struct SchemaReport {
    pub declarations: usize,
    pub operations: usize,
    pub golden_vectors: usize,
    pub additions_beyond_baseline: usize,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub enum Json {
    Null,
    Bool(bool),
    Number(String),
    String(String),
    Array(Vec<Json>),
    Object(Vec<(String, Json)>),
}

impl Json {
    #[must_use]
    pub fn field<'value>(&'value self, name: &str) -> Option<&'value Json> {
        match self {
            Self::Object(pairs) => pairs
                .iter()
                .find(|(key, _)| key == name)
                .map(|(_, value)| value),
            _ => None,
        }
    }
}

struct JsonParser {
    chars: Vec<char>,
    pos: usize,
}

impl JsonParser {
    fn peek(&self) -> Option<char> {
        self.chars.get(self.pos).copied()
    }

    fn bump(&mut self) -> Option<char> {
        let value = self.peek();
        if value.is_some() {
            self.pos += 1;
        }
        value
    }

    fn skip_whitespace(&mut self) {
        while matches!(self.peek(), Some(' ' | '\t' | '\n' | '\r')) {
            self.pos += 1;
        }
    }

    fn consume(&mut self, wanted: char) -> Result<(), String> {
        match self.bump() {
            Some(found) if found == wanted => Ok(()),
            Some(found) => Err(format!("expected '{wanted}', found '{found}'")),
            None => Err(format!("expected '{wanted}', found end of input")),
        }
    }

    fn value(&mut self) -> Result<Json, String> {
        self.skip_whitespace();
        match self.peek() {
            Some('{') => self.object(),
            Some('[') => self.array(),
            Some('"') => self.string().map(Json::String),
            Some('t') => self.keyword("true", Json::Bool(true)),
            Some('f') => self.keyword("false", Json::Bool(false)),
            Some('n') => self.keyword("null", Json::Null),
            Some(other) if other == '-' || other.is_ascii_digit() => self.number(),
            Some(other) => Err(format!("unexpected character '{other}'")),
            None => Err("unexpected end of input".to_owned()),
        }
    }

    fn keyword(&mut self, word: &str, value: Json) -> Result<Json, String> {
        for wanted in word.chars() {
            self.consume(wanted)?;
        }
        Ok(value)
    }

    fn object(&mut self) -> Result<Json, String> {
        self.consume('{')?;
        self.skip_whitespace();
        let mut pairs: Vec<(String, Json)> = Vec::new();
        if self.peek() == Some('}') {
            self.pos += 1;
            return Ok(Json::Object(pairs));
        }
        loop {
            self.skip_whitespace();
            let key = self.string()?;
            if pairs.iter().any(|(existing, _)| existing == &key) {
                return Err(format!("duplicate object key {key}"));
            }
            self.skip_whitespace();
            self.consume(':')?;
            let value = self.value()?;
            pairs.push((key, value));
            self.skip_whitespace();
            match self.bump() {
                Some(',') => {}
                Some('}') => return Ok(Json::Object(pairs)),
                _ => return Err("expected ',' or '}' in object".to_owned()),
            }
        }
    }

    fn array(&mut self) -> Result<Json, String> {
        self.consume('[')?;
        self.skip_whitespace();
        let mut items = Vec::new();
        if self.peek() == Some(']') {
            self.pos += 1;
            return Ok(Json::Array(items));
        }
        loop {
            items.push(self.value()?);
            self.skip_whitespace();
            match self.bump() {
                Some(',') => {}
                Some(']') => return Ok(Json::Array(items)),
                _ => return Err("expected ',' or ']' in array".to_owned()),
            }
        }
    }

    fn string(&mut self) -> Result<String, String> {
        self.consume('"')?;
        let mut output = String::new();
        loop {
            match self.bump() {
                None => return Err("unterminated string".to_owned()),
                Some('"') => return Ok(output),
                Some('\\') => output.push(self.escape()?),
                Some(control) if (control as u32) < 0x20 => {
                    return Err("unescaped control character in string".to_owned());
                }
                Some(other) => output.push(other),
            }
        }
    }

    fn escape(&mut self) -> Result<char, String> {
        match self.bump() {
            Some('"') => Ok('"'),
            Some('\\') => Ok('\\'),
            Some('/') => Ok('/'),
            Some('b') => Ok('\u{0008}'),
            Some('f') => Ok('\u{000C}'),
            Some('n') => Ok('\n'),
            Some('r') => Ok('\r'),
            Some('t') => Ok('\t'),
            Some('u') => self.unicode_escape(),
            _ => Err("invalid escape sequence".to_owned()),
        }
    }

    fn unicode_escape(&mut self) -> Result<char, String> {
        let first = self.hex_unit()?;
        if (0xD800..=0xDBFF).contains(&first) {
            self.consume('\\')?;
            self.consume('u')?;
            let second = self.hex_unit()?;
            if !(0xDC00..=0xDFFF).contains(&second) {
                return Err("invalid low surrogate".to_owned());
            }
            let combined = 0x10000 + ((first - 0xD800) << 10) + (second - 0xDC00);
            return char::from_u32(combined).ok_or_else(|| "invalid surrogate pair".to_owned());
        }
        char::from_u32(first).ok_or_else(|| "invalid unicode escape".to_owned())
    }

    fn hex_unit(&mut self) -> Result<u32, String> {
        let mut value = 0u32;
        for _ in 0..4 {
            let digit = self
                .bump()
                .and_then(|found| found.to_digit(16))
                .ok_or_else(|| "invalid hex digit in unicode escape".to_owned())?;
            value = value * 16 + digit;
        }
        Ok(value)
    }

    fn number(&mut self) -> Result<Json, String> {
        let start = self.pos;
        if self.peek() == Some('-') {
            self.pos += 1;
        }
        if !self.digits() {
            return Err("invalid number".to_owned());
        }
        if self.peek() == Some('.') {
            self.pos += 1;
            if !self.digits() {
                return Err("invalid number fraction".to_owned());
            }
        }
        if matches!(self.peek(), Some('e' | 'E')) {
            self.pos += 1;
            if matches!(self.peek(), Some('+' | '-')) {
                self.pos += 1;
            }
            if !self.digits() {
                return Err("invalid number exponent".to_owned());
            }
        }
        Ok(Json::Number(self.chars[start..self.pos].iter().collect()))
    }

    fn digits(&mut self) -> bool {
        let start = self.pos;
        while self.peek().is_some_and(|found| found.is_ascii_digit()) {
            self.pos += 1;
        }
        self.pos > start
    }
}

/// # Errors
///
/// Returns the first syntax defect in the JSON source.
pub fn parse_json(source: &str) -> Result<Json, String> {
    let mut parser = JsonParser {
        chars: source.chars().collect(),
        pos: 0,
    };
    let value = parser.value()?;
    parser.skip_whitespace();
    if parser.pos == parser.chars.len() {
        Ok(value)
    } else {
        Err("trailing content after JSON value".to_owned())
    }
}

type Sections = BTreeMap<String, BTreeMap<String, String>>;

fn parse_kvx(source: &str) -> Result<Sections, String> {
    let mut sections: Sections = BTreeMap::new();
    let mut current = String::new();
    for (index, raw) in source.lines().enumerate() {
        let line = raw.trim();
        if line.is_empty() || line.starts_with('#') {
            continue;
        }
        if let Some(name) = line
            .strip_prefix('[')
            .and_then(|value| value.strip_suffix(']'))
        {
            name.clone_into(&mut current);
            sections.entry(current.clone()).or_default();
            continue;
        }
        let Some((key, value)) = line.split_once('=') else {
            return Err(format!("line {}: not a key/value declaration", index + 1));
        };
        if current.is_empty() {
            return Err(format!("line {}: declaration outside a section", index + 1));
        }
        let entry = sections.entry(current.clone()).or_default();
        if entry
            .insert(key.trim().to_owned(), value.trim().to_owned())
            .is_some()
        {
            return Err(format!(
                "line {}: duplicate declaration {current}.{}",
                index + 1,
                key.trim()
            ));
        }
    }
    Ok(sections)
}

fn unquote(value: &str) -> Option<&str> {
    value
        .strip_prefix('"')
        .and_then(|inner| inner.strip_suffix('"'))
}

fn parse_list(value: &str) -> Option<Vec<String>> {
    let inner = value.strip_prefix('[')?.strip_suffix(']')?.trim();
    if inner.is_empty() {
        return Some(Vec::new());
    }
    let mut items = Vec::new();
    let mut rest = inner;
    loop {
        let after_open = rest.trim_start().strip_prefix('"')?;
        let end = after_open.find('"')?;
        items.push(after_open.get(..end)?.to_owned());
        let tail = after_open.get(end + 1..)?.trim_start();
        if tail.is_empty() {
            return Some(items);
        }
        rest = tail.strip_prefix(',')?;
    }
}

fn additive_list_extension(previous: &str, current: &str) -> bool {
    match (parse_list(previous), parse_list(current)) {
        (Some(old), Some(new)) => {
            new.len() >= old.len()
                && old
                    .iter()
                    .zip(new.iter())
                    .all(|(before, after)| before == after)
        }
        _ => false,
    }
}

fn violation(path: &Path, rule: &'static str, detail: impl Into<String>) -> Violation {
    Violation {
        path: path.to_path_buf(),
        rule,
        detail: detail.into(),
    }
}

struct SchemaFile {
    name: String,
    path: PathBuf,
    sections: Sections,
}

struct Header {
    major: u32,
    minor: u32,
    includes: Vec<String>,
}

struct Encoding {
    base_path: String,
    idempotency_header: String,
}

struct Scalar {
    format: Option<String>,
    prefix: Option<String>,
}

struct EnumDef {
    variants: Vec<String>,
}

struct Field {
    name: String,
    type_name: String,
    array: bool,
}

struct Record {
    required: Vec<Field>,
    optional: Vec<Field>,
    origin: PathBuf,
}

struct Operation {
    name: String,
    method: String,
    path: String,
    request: String,
    response: String,
    idempotency: bool,
    origin: PathBuf,
}

#[derive(Default)]
struct Model {
    scalars: BTreeMap<String, Scalar>,
    enums: BTreeMap<String, EnumDef>,
    records: BTreeMap<String, Record>,
    operations: Vec<Operation>,
}

fn load_kvx(root: &Path, name: &str, violations: &mut Vec<Violation>) -> Option<SchemaFile> {
    let path = root.join(name);
    let Ok(body) = fs::read_to_string(&path) else {
        violations.push(violation(
            &path,
            "missing-schema-file",
            format!("cannot read {name}"),
        ));
        return None;
    };
    match parse_kvx(&body) {
        Ok(sections) => Some(SchemaFile {
            name: name.to_owned(),
            path,
            sections,
        }),
        Err(detail) => {
            violations.push(violation(&path, "invalid-kvx", detail));
            None
        }
    }
}

fn quoted<'entries>(
    entries: &'entries BTreeMap<String, String>,
    key: &str,
) -> Option<&'entries str> {
    entries.get(key).map(String::as_str).and_then(unquote)
}

fn check_header(file: &SchemaFile, violations: &mut Vec<Violation>) -> Header {
    let mut header = Header {
        major: 0,
        minor: 0,
        includes: Vec::new(),
    };
    let Some(schema) = file.sections.get("schema") else {
        violations.push(violation(
            &file.path,
            "missing-schema-section",
            "v1.kvx declares no [schema] section",
        ));
        return header;
    };
    match quoted(schema, "name") {
        Some(name) if !name.is_empty() => {}
        _ => violations.push(violation(
            &file.path,
            "missing-schema-name",
            "schema.name must be a quoted non-empty string",
        )),
    }
    match schema
        .get("major")
        .and_then(|value| value.parse::<u32>().ok())
    {
        Some(major) if major >= 1 => header.major = major,
        _ => violations.push(violation(
            &file.path,
            "missing-schema-version",
            "schema.major must be an explicit integer of at least 1",
        )),
    }
    match schema
        .get("minor")
        .and_then(|value| value.parse::<u32>().ok())
    {
        Some(minor) => header.minor = minor,
        None => violations.push(violation(
            &file.path,
            "missing-schema-version",
            "schema.minor must be an explicit integer",
        )),
    }
    match quoted(schema, "compatibility") {
        Some(rule) if rule.to_ascii_lowercase().contains("additive") => {}
        _ => violations.push(violation(
            &file.path,
            "missing-compatibility-rule",
            "schema.compatibility must state the additive-only rule",
        )),
    }
    match schema
        .get("includes")
        .map(String::as_str)
        .and_then(parse_list)
    {
        Some(includes) => header.includes = includes,
        None => violations.push(violation(
            &file.path,
            "missing-includes",
            "schema.includes must be a list of module files",
        )),
    }
    header
}

fn check_encoding(file: &SchemaFile, violations: &mut Vec<Violation>) -> Encoding {
    let mut encoding = Encoding {
        base_path: "/v1".to_owned(),
        idempotency_header: "Idempotency-Key".to_owned(),
    };
    let Some(section) = file.sections.get("encoding") else {
        violations.push(violation(
            &file.path,
            "missing-encoding-section",
            "v1.kvx declares no [encoding] section",
        ));
        return encoding;
    };
    for key in ["request", "response", "error", "idempotency", "versioning"] {
        match quoted(section, key) {
            Some(value) if !value.is_empty() => {}
            _ => violations.push(violation(
                &file.path,
                "missing-encoding-convention",
                format!("encoding.{key} must be a quoted non-empty declaration"),
            )),
        }
    }
    match quoted(section, "content_type") {
        Some(value) if value.contains("application/json") => {}
        _ => violations.push(violation(
            &file.path,
            "missing-encoding-convention",
            "encoding.content_type must declare application/json",
        )),
    }
    match quoted(section, "amounts") {
        Some(value) if value.contains("decimal string") => {}
        _ => violations.push(violation(
            &file.path,
            "missing-encoding-convention",
            "encoding.amounts must pin amounts to decimal strings",
        )),
    }
    match quoted(section, "base_path") {
        Some(value) if value.starts_with('/') && !value.ends_with('/') => {
            value.clone_into(&mut encoding.base_path);
        }
        _ => violations.push(violation(
            &file.path,
            "missing-encoding-convention",
            "encoding.base_path must be a versioned path such as /v1",
        )),
    }
    match quoted(section, "idempotency_header") {
        Some(value) if !value.is_empty() => value.clone_into(&mut encoding.idempotency_header),
        _ => violations.push(violation(
            &file.path,
            "missing-encoding-convention",
            "encoding.idempotency_header must name the idempotency-key header",
        )),
    }
    if let Some(golden) = file.sections.get("golden") {
        for key in ["layout", "rule", "failure_layout", "failure_rule"] {
            if quoted(golden, key).is_none_or(str::is_empty) {
                violations.push(violation(
                    &file.path,
                    "missing-golden-convention",
                    format!("golden.{key} must document the golden vector convention"),
                ));
            }
        }
    } else {
        violations.push(violation(
            &file.path,
            "missing-golden-convention",
            "v1.kvx declares no [golden] section",
        ));
    }
    encoding
}

fn check_module_header(file: &SchemaFile, major: u32, violations: &mut Vec<Violation>) {
    let Some(module) = file.sections.get("module") else {
        violations.push(violation(
            &file.path,
            "missing-module-section",
            format!("{} declares no [module] section", file.name),
        ));
        return;
    };
    let stem = file.name.strip_suffix(".kvx").unwrap_or(&file.name);
    match quoted(module, "name") {
        Some(name) if name == stem => {}
        _ => violations.push(violation(
            &file.path,
            "module-name-mismatch",
            format!("module.name must equal the file stem {stem}"),
        )),
    }
    match module
        .get("contract_major")
        .and_then(|value| value.parse::<u32>().ok())
    {
        Some(value) if value == major => {}
        _ => violations.push(violation(
            &file.path,
            "module-major-mismatch",
            format!("module.contract_major must equal the schema major {major}"),
        )),
    }
    match quoted(module, "compatibility") {
        Some("additive_only") => {}
        _ => violations.push(violation(
            &file.path,
            "module-compatibility-mismatch",
            "module.compatibility must be additive_only",
        )),
    }
}

fn parse_field(spec: &str) -> Option<Field> {
    let (name, type_spec) = spec.split_once(':')?;
    let name = name.trim();
    let type_spec = type_spec.trim();
    if name.is_empty() || type_spec.is_empty() {
        return None;
    }
    let (type_name, array) = type_spec
        .strip_suffix("[]")
        .map_or((type_spec, false), |inner| (inner, true));
    if type_name.is_empty() {
        return None;
    }
    Some(Field {
        name: name.to_owned(),
        type_name: type_name.to_owned(),
        array,
    })
}

fn name_is_free(model: &Model, name: &str) -> bool {
    !model.scalars.contains_key(name)
        && !model.enums.contains_key(name)
        && !model.records.contains_key(name)
}

fn parse_fields(
    file: &SchemaFile,
    section: &str,
    value: &str,
    violations: &mut Vec<Violation>,
) -> Vec<Field> {
    let Some(specs) = parse_list(value) else {
        violations.push(violation(
            &file.path,
            "invalid-field-declaration",
            format!("{section} must declare its fields as a list"),
        ));
        return Vec::new();
    };
    let mut fields = Vec::new();
    for spec in specs {
        match parse_field(&spec) {
            Some(field) => fields.push(field),
            None => violations.push(violation(
                &file.path,
                "invalid-field-declaration",
                format!("{section} field {spec} must be name:Type"),
            )),
        }
    }
    fields
}

fn collect_record(
    file: &SchemaFile,
    section: &str,
    name: &str,
    entries: &BTreeMap<String, String>,
    fields_key: &str,
    model: &mut Model,
    violations: &mut Vec<Violation>,
) {
    let Some(fields_value) = entries.get(fields_key) else {
        violations.push(violation(
            &file.path,
            "invalid-record-declaration",
            format!("{section} must declare {fields_key}"),
        ));
        return;
    };
    let required = parse_fields(file, section, fields_value, violations);
    let optional = entries
        .get("optional")
        .map(|value| parse_fields(file, section, value, violations))
        .unwrap_or_default();
    if name_is_free(model, name) {
        model.records.insert(
            name.to_owned(),
            Record {
                required,
                optional,
                origin: file.path.clone(),
            },
        );
    } else {
        violations.push(violation(
            &file.path,
            "duplicate-declaration",
            format!("{name} is declared more than once"),
        ));
    }
}

fn collect_scalar(
    file: &SchemaFile,
    name: &str,
    entries: &BTreeMap<String, String>,
    model: &mut Model,
    violations: &mut Vec<Violation>,
) {
    let section = format!("scalar.{name}");
    if quoted(entries, "json") != Some("string") {
        violations.push(violation(
            &file.path,
            "invalid-scalar-declaration",
            format!("{section} must declare json = \"string\""),
        ));
    }
    let typescript = quoted(entries, "typescript");
    if !matches!(typescript, Some("string" | "bigint")) {
        violations.push(violation(
            &file.path,
            "invalid-scalar-declaration",
            format!("{section} must map to a TypeScript string or bigint"),
        ));
    }
    if quoted(entries, "rust").is_none_or(str::is_empty) {
        violations.push(violation(
            &file.path,
            "invalid-scalar-declaration",
            format!("{section} must declare its Rust type"),
        ));
    }
    if let Some(marker) = entries.get("consensus_integer") {
        if marker != "true" || typescript != Some("bigint") {
            violations.push(violation(
                &file.path,
                "invalid-scalar-declaration",
                format!("{section} consensus integers must be true and map to bigint"),
            ));
        }
    }
    let format = quoted(entries, "format").map(str::to_owned);
    if let Some(value) = &format {
        if !SCALAR_FORMATS.contains(&value.as_str()) {
            violations.push(violation(
                &file.path,
                "invalid-scalar-declaration",
                format!("{section} format {value} is not a known scalar format"),
            ));
        }
    }
    let prefix = quoted(entries, "prefix").map(str::to_owned);
    if name_is_free(model, name) {
        model
            .scalars
            .insert(name.to_owned(), Scalar { format, prefix });
    } else {
        violations.push(violation(
            &file.path,
            "duplicate-declaration",
            format!("{name} is declared more than once"),
        ));
    }
}

fn collect_enum(
    file: &SchemaFile,
    name: &str,
    variants_value: &str,
    model: &mut Model,
    violations: &mut Vec<Violation>,
) {
    let variants = parse_list(variants_value).unwrap_or_default();
    if variants.is_empty() || variants.iter().any(String::is_empty) {
        violations.push(violation(
            &file.path,
            "invalid-enum-declaration",
            format!("type.{name} variants must be a non-empty list of non-empty strings"),
        ));
        return;
    }
    if name_is_free(model, name) {
        model.enums.insert(name.to_owned(), EnumDef { variants });
    } else {
        violations.push(violation(
            &file.path,
            "duplicate-declaration",
            format!("{name} is declared more than once"),
        ));
    }
}

fn collect_operation(
    file: &SchemaFile,
    name: &str,
    entries: &BTreeMap<String, String>,
    model: &mut Model,
    violations: &mut Vec<Violation>,
) {
    let section = format!("operation.{name}");
    let method = quoted(entries, "method").unwrap_or_default().to_owned();
    let path = quoted(entries, "path").unwrap_or_default().to_owned();
    let request = quoted(entries, "request").unwrap_or_default().to_owned();
    let response = quoted(entries, "response").unwrap_or_default().to_owned();
    if !METHODS.contains(&method.as_str()) {
        violations.push(violation(
            &file.path,
            "invalid-operation-declaration",
            format!("{section} must declare a method from {METHODS:?}"),
        ));
    }
    if path.is_empty() {
        violations.push(violation(
            &file.path,
            "invalid-operation-declaration",
            format!("{section} must declare its path"),
        ));
    }
    if request.is_empty() {
        violations.push(violation(
            &file.path,
            "invalid-operation-declaration",
            format!("{section} must declare its request shape"),
        ));
    }
    if response.is_empty() {
        violations.push(violation(
            &file.path,
            "invalid-operation-declaration",
            format!("{section} must declare its response shape"),
        ));
    }
    let idempotency = match entries.get("idempotency").map(String::as_str) {
        None | Some("false") => false,
        Some("true") => true,
        Some(other) => {
            violations.push(violation(
                &file.path,
                "invalid-operation-declaration",
                format!("{section} idempotency must be true or false, not {other}"),
            ));
            false
        }
    };
    if model
        .operations
        .iter()
        .any(|existing| existing.name == name)
    {
        violations.push(violation(
            &file.path,
            "duplicate-declaration",
            format!("{section} is declared more than once"),
        ));
        return;
    }
    model.operations.push(Operation {
        name: name.to_owned(),
        method,
        path,
        request,
        response,
        idempotency,
        origin: file.path.clone(),
    });
}

fn collect_declarations(files: &[SchemaFile], violations: &mut Vec<Violation>) -> Model {
    let mut model = Model::default();
    for file in files {
        for (section, entries) in &file.sections {
            if let Some(name) = section.strip_prefix("scalar.") {
                collect_scalar(file, name, entries, &mut model, violations);
            } else if let Some(name) = section.strip_prefix("type.") {
                if let Some(variants) = entries.get("variants") {
                    if entries.contains_key("required") {
                        violations.push(violation(
                            &file.path,
                            "invalid-type-declaration",
                            format!("{section} declares both variants and required"),
                        ));
                    } else {
                        collect_enum(file, name, variants, &mut model, violations);
                    }
                } else if entries.contains_key("required") {
                    collect_record(
                        file, section, name, entries, "required", &mut model, violations,
                    );
                } else {
                    violations.push(violation(
                        &file.path,
                        "invalid-type-declaration",
                        format!("{section} must declare variants or required"),
                    ));
                }
            } else if let Some(name) = section.strip_prefix("record.") {
                collect_record(
                    file, section, name, entries, "fields", &mut model, violations,
                );
            } else if let Some(name) = section.strip_prefix("operation.") {
                collect_operation(file, name, entries, &mut model, violations);
            }
        }
    }
    model
}

fn type_exists(model: &Model, name: &str) -> bool {
    PRIMITIVES.contains(&name)
        || model.scalars.contains_key(name)
        || model.enums.contains_key(name)
        || model.records.contains_key(name)
}

fn segment_well_formed(segment: &str) -> bool {
    if segment.is_empty() {
        return false;
    }
    match segment.strip_prefix('{') {
        Some(inner) => inner
            .strip_suffix('}')
            .is_some_and(|name| !name.is_empty() && !name.contains(['{', '}'])),
        None => !segment.contains(['{', '}']),
    }
}

fn check_type_references(model: &Model, violations: &mut Vec<Violation>) {
    for (name, record) in &model.records {
        for field in record.required.iter().chain(&record.optional) {
            if !type_exists(model, &field.type_name) {
                violations.push(violation(
                    &record.origin,
                    "unresolved-type",
                    format!(
                        "{name}.{} references undeclared type {}",
                        field.name, field.type_name
                    ),
                ));
            }
        }
    }
}

fn check_operation_declarations(
    model: &Model,
    encoding: &Encoding,
    violations: &mut Vec<Violation>,
) {
    for operation in &model.operations {
        let under_base = operation
            .path
            .strip_prefix(&encoding.base_path)
            .is_some_and(|rest| rest.starts_with('/'));
        if !under_base {
            violations.push(violation(
                &operation.origin,
                "invalid-operation-path",
                format!(
                    "operation.{} path {} must live under {}",
                    operation.name, operation.path, encoding.base_path
                ),
            ));
        } else if !operation.path.split('/').skip(1).all(segment_well_formed) {
            violations.push(violation(
                &operation.origin,
                "invalid-operation-path",
                format!(
                    "operation.{} path {} has a malformed segment",
                    operation.name, operation.path
                ),
            ));
        }
        for type_name in [&operation.request, &operation.response] {
            if !type_name.is_empty() && !type_exists(model, type_name) {
                violations.push(violation(
                    &operation.origin,
                    "unresolved-type",
                    format!(
                        "operation.{} references undeclared type {type_name}",
                        operation.name
                    ),
                ));
            }
        }
    }
}

fn format_matches(format: &str, text: &str) -> bool {
    match format {
        "decimal" => !text.is_empty() && text.bytes().all(|byte| byte.is_ascii_digit()),
        "currency" => {
            text.bytes()
                .next()
                .is_some_and(|first| first.is_ascii_uppercase())
                && text
                    .bytes()
                    .all(|byte| byte.is_ascii_uppercase() || byte.is_ascii_digit())
        }
        "rfc3339-utc" => {
            let bytes = text.as_bytes();
            bytes.len() >= 20
                && bytes.get(4) == Some(&b'-')
                && bytes.get(7) == Some(&b'-')
                && bytes.get(10) == Some(&b'T')
                && text.ends_with('Z')
        }
        "copy-key" => {
            !text.is_empty()
                && text.bytes().all(|byte| {
                    byte.is_ascii_lowercase()
                        || byte.is_ascii_digit()
                        || matches!(byte, b'.' | b'_' | b'-')
                })
        }
        _ => false,
    }
}

fn is_integer_text(text: &str) -> bool {
    !text.contains(['.', 'e', 'E'])
}

fn check_scalar_value(
    scalar: &Scalar,
    type_name: &str,
    value: &Json,
    path: &Path,
    location: &str,
    violations: &mut Vec<Violation>,
) {
    let Json::String(text) = value else {
        violations.push(violation(
            path,
            "invalid-scalar-value",
            format!("{location} must be a JSON string for scalar {type_name}"),
        ));
        return;
    };
    if let Some(prefix) = &scalar.prefix {
        if !(text.starts_with(prefix.as_str()) && text.len() > prefix.len()) {
            violations.push(violation(
                path,
                "invalid-scalar-value",
                format!("{location} must carry the {prefix} prefix for scalar {type_name}"),
            ));
        }
    }
    if let Some(format) = &scalar.format {
        if !format_matches(format, text) {
            violations.push(violation(
                path,
                "invalid-scalar-value",
                format!("{location} does not match the {format} format of scalar {type_name}"),
            ));
        }
    }
}

fn check_value(
    model: &Model,
    type_name: &str,
    value: &Json,
    path: &Path,
    location: &str,
    violations: &mut Vec<Violation>,
) {
    match type_name {
        "string" => {
            if !matches!(value, Json::String(_)) {
                violations.push(violation(
                    path,
                    "shape-mismatch",
                    format!("{location} must be a JSON string"),
                ));
            }
        }
        "boolean" => {
            if !matches!(value, Json::Bool(_)) {
                violations.push(violation(
                    path,
                    "shape-mismatch",
                    format!("{location} must be a JSON boolean"),
                ));
            }
        }
        "integer" => {
            let integral = matches!(value, Json::Number(text) if is_integer_text(text));
            if !integral {
                violations.push(violation(
                    path,
                    "shape-mismatch",
                    format!("{location} must be a JSON integer"),
                ));
            }
        }
        "object" => {
            if !matches!(value, Json::Object(_)) {
                violations.push(violation(
                    path,
                    "shape-mismatch",
                    format!("{location} must be a JSON object"),
                ));
            }
        }
        _ => {
            if let Some(scalar) = model.scalars.get(type_name) {
                check_scalar_value(scalar, type_name, value, path, location, violations);
            } else if let Some(definition) = model.enums.get(type_name) {
                let declared =
                    matches!(value, Json::String(text) if definition.variants.contains(text));
                if !declared {
                    violations.push(violation(
                        path,
                        "undeclared-enum-value",
                        format!("{location} must be a declared {type_name} variant"),
                    ));
                }
            } else if let Some(record) = model.records.get(type_name) {
                check_record_value(model, record, value, path, location, violations);
            } else {
                violations.push(violation(
                    path,
                    "unresolved-type",
                    format!("{location} references undeclared type {type_name}"),
                ));
            }
        }
    }
}

fn check_field_value(
    model: &Model,
    field: &Field,
    value: &Json,
    path: &Path,
    location: &str,
    violations: &mut Vec<Violation>,
) {
    if field.array {
        let Json::Array(items) = value else {
            violations.push(violation(
                path,
                "shape-mismatch",
                format!("{location} must be a JSON array"),
            ));
            return;
        };
        for (index, item) in items.iter().enumerate() {
            check_value(
                model,
                &field.type_name,
                item,
                path,
                &format!("{location}[{index}]"),
                violations,
            );
        }
    } else {
        check_value(model, &field.type_name, value, path, location, violations);
    }
}

fn check_record_value(
    model: &Model,
    record: &Record,
    value: &Json,
    path: &Path,
    location: &str,
    violations: &mut Vec<Violation>,
) {
    let Json::Object(pairs) = value else {
        violations.push(violation(
            path,
            "shape-mismatch",
            format!("{location} must be a JSON object"),
        ));
        return;
    };
    for field in &record.required {
        match value.field(&field.name) {
            Some(inner) => check_field_value(
                model,
                field,
                inner,
                path,
                &format!("{location}.{}", field.name),
                violations,
            ),
            None => violations.push(violation(
                path,
                "missing-required-field",
                format!("{location} lacks required field {}", field.name),
            )),
        }
    }
    for field in &record.optional {
        if let Some(inner) = value.field(&field.name) {
            check_field_value(
                model,
                field,
                inner,
                path,
                &format!("{location}.{}", field.name),
                violations,
            );
        }
    }
    for (key, _) in pairs {
        let declared = record
            .required
            .iter()
            .chain(&record.optional)
            .any(|field| field.name == *key);
        if !declared {
            violations.push(violation(
                path,
                "undeclared-field",
                format!("{location}.{key} is not a declared field"),
            ));
        }
    }
}

fn path_matches_template(template: &str, actual: &str) -> bool {
    let pattern: Vec<&str> = template.split('/').collect();
    let segments: Vec<&str> = actual.split('/').collect();
    pattern.len() == segments.len()
        && pattern
            .iter()
            .zip(segments.iter())
            .all(|(expected, segment)| {
                expected
                    .strip_prefix('{')
                    .and_then(|inner| inner.strip_suffix('}'))
                    .map_or_else(|| expected == segment, |_| !segment.is_empty())
            })
}

fn request_is_bodyless(model: &Model, type_name: &str) -> bool {
    model
        .records
        .get(type_name)
        .is_some_and(|record| record.required.is_empty() && record.optional.is_empty())
}

fn check_golden_request(
    model: &Model,
    encoding: &Encoding,
    operation: &Operation,
    path: &Path,
    vector: &Json,
    violations: &mut Vec<Violation>,
) {
    let Json::Object(pairs) = vector else {
        violations.push(violation(
            path,
            "invalid-golden-vector",
            "request vector must be a JSON object",
        ));
        return;
    };
    for (key, _) in pairs {
        if !matches!(key.as_str(), "method" | "path" | "headers" | "body") {
            violations.push(violation(
                path,
                "unexpected-vector-key",
                format!("request vector key {key} is not part of the convention"),
            ));
        }
    }
    let method_matches =
        matches!(vector.field("method"), Some(Json::String(method)) if *method == operation.method);
    if !method_matches {
        violations.push(violation(
            path,
            "golden-method-mismatch",
            format!("request vector method must be {}", operation.method),
        ));
    }
    let path_matches = matches!(
        vector.field("path"),
        Some(Json::String(actual)) if path_matches_template(&operation.path, actual)
    );
    if !path_matches {
        violations.push(violation(
            path,
            "golden-path-mismatch",
            format!(
                "request vector path must match the template {}",
                operation.path
            ),
        ));
    }
    let headers = vector.field("headers");
    if let Some(value) = headers {
        let Json::Object(entries) = value else {
            violations.push(violation(
                path,
                "invalid-golden-vector",
                "headers must be a JSON object",
            ));
            return;
        };
        for (name, header) in entries {
            if !matches!(header, Json::String(_)) {
                violations.push(violation(
                    path,
                    "invalid-golden-vector",
                    format!("header {name} must be a JSON string"),
                ));
            }
        }
    }
    if operation.idempotency {
        let carried = matches!(headers, Some(Json::Object(entries)) if entries
            .iter()
            .any(|(name, _)| name.eq_ignore_ascii_case(&encoding.idempotency_header)));
        if !carried {
            violations.push(violation(
                path,
                "missing-idempotency-header",
                format!(
                    "operation.{} requires the {} header in its request vector",
                    operation.name, encoding.idempotency_header
                ),
            ));
        }
    }
    match vector.field("body") {
        Some(body) => check_value(model, &operation.request, body, path, "body", violations),
        None if request_is_bodyless(model, &operation.request) => {}
        None => violations.push(violation(
            path,
            "missing-request-body",
            format!(
                "operation.{} declares request {}",
                operation.name, operation.request
            ),
        )),
    }
}

fn check_golden_response(
    model: &Model,
    operation: &Operation,
    path: &Path,
    vector: &Json,
    violations: &mut Vec<Violation>,
) {
    let Json::Object(pairs) = vector else {
        violations.push(violation(
            path,
            "invalid-golden-vector",
            "response vector must be a JSON object",
        ));
        return;
    };
    for (key, _) in pairs {
        if !matches!(key.as_str(), "status" | "body") {
            violations.push(violation(
                path,
                "unexpected-vector-key",
                format!("response vector key {key} is not part of the convention"),
            ));
        }
    }
    let status_valid = matches!(
        vector.field("status"),
        Some(Json::Number(text))
            if is_integer_text(text) && text.parse::<i64>().is_ok_and(|status| (200..300).contains(&status))
    );
    if !status_valid {
        violations.push(violation(
            path,
            "invalid-response-status",
            "response vector status must be a 2xx integer",
        ));
    }
    let Some(body @ Json::Object(body_pairs)) = vector.field("body") else {
        violations.push(violation(
            path,
            "invalid-response-envelope",
            "response vector body must be a JSON object",
        ));
        return;
    };
    for (key, _) in body_pairs {
        if !matches!(key.as_str(), "ok" | "result" | "trace") {
            violations.push(violation(
                path,
                "invalid-response-envelope",
                format!("envelope key {key} is not part of the convention"),
            ));
        }
    }
    if body.field("ok") != Some(&Json::Bool(true)) {
        violations.push(violation(
            path,
            "invalid-response-envelope",
            "golden response envelopes carry ok true",
        ));
    }
    match body.field("trace") {
        Some(trace @ Json::String(text)) if !text.is_empty() => {
            if let Some(scalar) = model.scalars.get("TraceId") {
                check_scalar_value(scalar, "TraceId", trace, path, "body.trace", violations);
            }
        }
        _ => violations.push(violation(
            path,
            "invalid-response-envelope",
            "envelope trace must be a non-empty string",
        )),
    }
    match body.field("result") {
        Some(result) => check_value(
            model,
            &operation.response,
            result,
            path,
            "body.result",
            violations,
        ),
        None => violations.push(violation(
            path,
            "invalid-response-envelope",
            format!("envelope result must match {}", operation.response),
        )),
    }
}

/// Enforces the typed error model: the ApiError record every failure envelope
/// carries, its stable machine-code enum and its retriability classification.
fn human_api_error_model(model: &Model, root: &Path, violations: &mut Vec<Violation>) {
    let origin = root.join("errors.kvx");
    let Some(record) = model.records.get("ApiError") else {
        violations.push(violation(
            &origin,
            "missing-error-model",
            "the contract must declare the ApiError record its failure envelopes carry",
        ));
        return;
    };
    for (field, wanted) in [
        ("code", "ErrorCode"),
        ("copy_key", "CopyKey"),
        ("retry", "Retriability"),
    ] {
        let carried = record
            .required
            .iter()
            .any(|declared| declared.name == field && declared.type_name == wanted);
        if !carried {
            violations.push(violation(
                &origin,
                "missing-error-model",
                format!("ApiError must require {field}:{wanted}"),
            ));
        }
    }
    for enum_name in ["ErrorCode", "Retriability"] {
        if !model.enums.contains_key(enum_name) {
            violations.push(violation(
                &origin,
                "missing-error-model",
                format!("the error model must declare the {enum_name} enum"),
            ));
        }
    }
}

/// Enforces the resumable streaming spine: the stream operations and the
/// cursor-ordered event shapes both shells rely on to render live state.
fn human_api_stream_module(model: &Model, root: &Path, violations: &mut Vec<Violation>) {
    let origin = root.join("stream.kvx");
    for operation in ["stream.open", "stream.next"] {
        if !model
            .operations
            .iter()
            .any(|declared| declared.name == operation)
        {
            violations.push(violation(
                &origin,
                "missing-stream-module",
                format!("the contract must declare operation.{operation}"),
            ));
        }
    }
    let Some(event) = model.records.get("StreamEvent") else {
        violations.push(violation(
            &origin,
            "missing-stream-module",
            "the contract must declare the StreamEvent record",
        ));
        return;
    };
    for (field, wanted) in [
        ("cursor", "Cursor"),
        ("kind", "StreamEventKind"),
        ("observed_at", "Timestamp"),
    ] {
        let carried = event
            .required
            .iter()
            .any(|declared| declared.name == field && declared.type_name == wanted);
        if !carried {
            violations.push(violation(
                &origin,
                "missing-stream-module",
                format!("StreamEvent must require {field}:{wanted}"),
            ));
        }
    }
    let paged = model.records.get("StreamPage").is_some_and(|page| {
        page.required
            .iter()
            .any(|declared| declared.name == "events" && declared.array)
            && page
                .required
                .iter()
                .any(|declared| declared.name == "next_cursor")
    });
    if !paged {
        violations.push(violation(
            &origin,
            "missing-stream-module",
            "StreamPage must carry events:StreamEvent[] and next_cursor:Cursor",
        ));
    }
}

fn human_api_stream_push(
    files: &[SchemaFile],
    model: &Model,
    root: &Path,
    violations: &mut Vec<Violation>,
) -> bool {
    let Some(file) = files
        .iter()
        .find(|file| file.sections.contains_key("transport.stream_push_v2"))
    else {
        return false;
    };
    let entries = &file.sections["transport.stream_push_v2"];
    if files
        .iter()
        .find_map(|file| file.sections.get("operation.stream.next"))
        .and_then(|entries| quoted(entries, "authorization_class"))
        != Some("read")
    {
        violations.push(violation(
            &file.path,
            "invalid-stream-push-operation",
            "push must retain the existing stream.next read entitlement",
        ));
    }
    for (key, expected) in [
        ("operation", "\"stream.next\""),
        ("profile", "2"),
        ("accept", "\"text/event-stream\""),
        ("profile_header", "\"X-LayerX-Stream-Profile\""),
        ("profile_value", "\"2\""),
        ("content_type", "\"text/event-stream; charset=utf-8\""),
        ("success_event", "\"stream.page\""),
        ("error_event", "\"stream.error\""),
        ("maximum_events", "100"),
        ("maximum_payload_bytes", "1048576"),
        ("maximum_framing_bytes", "6400"),
        ("maximum_cursor_bytes", "512"),
        ("maximum_authorization_seconds", "60"),
        ("maximum_json_depth", "64"),
    ] {
        if entries.get(key).map(String::as_str) != Some(expected) {
            violations.push(violation(
                &file.path,
                "invalid-stream-push-profile",
                format!("stream_push_v2.{key} must equal {expected}"),
            ));
        }
    }
    let Some(operation) = model
        .operations
        .iter()
        .find(|operation| operation.name == "stream.next")
    else {
        return true;
    };
    if operation.method != "GET"
        || operation.path != "/v1/stream/{cursor}"
        || operation.request != "Empty"
        || operation.response != "StreamPage"
    {
        violations.push(violation(
            &file.path,
            "invalid-stream-push-operation",
            "push must retain the existing stream.next operation shape",
        ));
    }
    let directory = root.join("golden/stream-push-v2");
    let names = ["request.json", "response.json", "failure.json"];
    match fs::read_dir(&directory) {
        Ok(paths) => {
            for entry in paths.flatten() {
                if !names.iter().any(|name| entry.file_name() == *name) || !entry.path().is_file() {
                    violations.push(violation(
                        &entry.path(),
                        "orphan-stream-push-vector",
                        "stream_push_v2 owns exactly request.json, response.json and failure.json",
                    ));
                }
            }
        }
        Err(_) => violations.push(violation(
            &directory,
            "missing-stream-push-vector",
            "stream_push_v2 requires its versioned golden vector directory",
        )),
    }
    for name in names {
        let path = directory.join(name);
        let vector = match fs::read_to_string(&path)
            .ok()
            .and_then(|body| parse_json(&body).ok())
        {
            Some(value) => value,
            None => {
                violations.push(violation(
                    &path,
                    "invalid-stream-push-vector",
                    "missing or invalid JSON vector",
                ));
                continue;
            }
        };
        if name == "request.json" {
            let Some(headers) = vector.field("headers") else {
                violations.push(violation(
                    &path,
                    "invalid-stream-push-vector",
                    "request must carry the explicit push headers",
                ));
                continue;
            };
            for (key, expected) in [
                ("Accept", "text/event-stream"),
                ("X-LayerX-Stream-Profile", "2"),
            ] {
                if headers.field(key) != Some(&Json::String(expected.to_owned())) {
                    violations.push(violation(
                        &path,
                        "invalid-stream-push-vector",
                        "request push headers differ",
                    ));
                }
            }
            if vector.field("method") != Some(&Json::String("GET".to_owned()))
                || !matches!(vector.field("path"), Some(Json::String(path)) if path.starts_with("/v1/stream/"))
                || vector.field("body").is_some()
            {
                violations.push(violation(
                    &path,
                    "invalid-stream-push-vector",
                    "request must retain bodyless stream.next GET",
                ));
            }
            continue;
        }
        if vector.field("status") != Some(&Json::Number("200".to_owned())) {
            violations.push(violation(
                &path,
                "invalid-stream-push-vector",
                "push response status must be 200",
            ));
        }
        let headers = vector.field("headers");
        for (key, expected) in [
            ("Content-Type", "text/event-stream; charset=utf-8"),
            ("X-LayerX-Stream-Profile", "2"),
        ] {
            if headers.and_then(|headers| headers.field(key))
                != Some(&Json::String(expected.to_owned()))
            {
                violations.push(violation(
                    &path,
                    "invalid-stream-push-vector",
                    "response push headers differ",
                ));
            }
        }
        let Some(Json::Array(frames)) = vector.field("frames") else {
            violations.push(violation(
                &path,
                "invalid-stream-push-vector",
                "response must contain frames",
            ));
            continue;
        };
        if frames.is_empty() || frames.len() > 100 || (name == "failure.json" && frames.len() != 1)
        {
            violations.push(violation(
                &path,
                "invalid-stream-push-vector",
                "invalid frame count",
            ));
        }
        let mut payload_bytes = 0_usize;
        let mut wire_bytes = 0_usize;
        let mut previous_cursor: Option<String> = None;
        for frame in frames {
            let Json::String(frame) = frame else {
                violations.push(violation(
                    &path,
                    "invalid-stream-push-vector",
                    "frame must be a UTF-8 string",
                ));
                continue;
            };
            wire_bytes = wire_bytes.saturating_add(frame.len());
            let event = if name == "failure.json" {
                "stream.error"
            } else {
                "stream.page"
            };
            let prefix = format!("event: {event}\ndata: ");
            let Some(data) = frame
                .strip_prefix(&prefix)
                .and_then(|frame| frame.strip_suffix("\n\n"))
                .filter(|data| !data.contains(['\n', '\r']))
            else {
                violations.push(violation(
                    &path,
                    "invalid-stream-push-vector",
                    "frame must contain exactly its event and data lines",
                ));
                continue;
            };
            payload_bytes = payload_bytes.saturating_add(data.len());
            let Ok(body) = parse_json(data) else {
                violations.push(violation(
                    &path,
                    "invalid-stream-push-vector",
                    "frame envelope is invalid JSON",
                ));
                continue;
            };
            let wrapped = Json::Object(vec![
                (
                    "status".to_owned(),
                    Json::Number(if name == "failure.json" { "410" } else { "200" }.to_owned()),
                ),
                ("body".to_owned(), body.clone()),
            ]);
            if name == "failure.json" {
                check_golden_failure(model, operation, &path, &wrapped, violations);
            } else {
                check_golden_response(model, operation, &path, &wrapped, violations);
                let result = body.field("result");
                let events = result.and_then(|result| result.field("events"));
                let cursor = match events {
                    Some(Json::Array(events)) if events.len() == 1 => events[0].field("cursor"),
                    _ => None,
                };
                if cursor.is_none()
                    || cursor != result.and_then(|result| result.field("next_cursor"))
                {
                    violations.push(violation(
                        &path,
                        "invalid-stream-push-vector",
                        "next_cursor must equal the single delivered event cursor",
                    ));
                }
                if let Some(Json::String(cursor)) = cursor {
                    if cursor.is_empty()
                        || cursor.len() > 512
                        || previous_cursor.as_ref() == Some(cursor)
                    {
                        violations.push(violation(
                            &path,
                            "invalid-stream-push-vector",
                            "event cursor is empty, repeated or exceeds its bound",
                        ));
                    }
                    previous_cursor = Some(cursor.clone());
                }
            }
        }
        if payload_bytes > 1_048_576 || wire_bytes > 1_048_576 + 6_400 {
            violations.push(violation(
                &path,
                "invalid-stream-push-vector",
                "subscription exceeds original aggregate payload bound",
            ));
        }
    }
    true
}

/// Enforces the identity module spine: account creation, session revocation,
/// the operation-bound step-up challenge shape and the wallet-binding
/// submission the authenticated plane stands on.
fn human_api_identity_module(model: &Model, root: &Path, violations: &mut Vec<Violation>) {
    let origin = root.join("identity.kvx");
    for operation in ["account.create", "session.revoke", "binding.submit"] {
        if !model
            .operations
            .iter()
            .any(|declared| declared.name == operation)
        {
            violations.push(violation(
                &origin,
                "missing-identity-module",
                format!("the contract must declare operation.{operation}"),
            ));
        }
    }
    let Some(challenge) = model.records.get("StepUpChallenge") else {
        violations.push(violation(
            &origin,
            "missing-identity-module",
            "the contract must declare the StepUpChallenge record",
        ));
        return;
    };
    for (field, wanted) in [
        ("challenge_id", "StepUpChallengeId"),
        ("confirms", "OperationDigest"),
        ("expires_at", "Timestamp"),
    ] {
        let carried = challenge
            .required
            .iter()
            .any(|declared| declared.name == field && declared.type_name == wanted);
        if !carried {
            violations.push(violation(
                &origin,
                "missing-identity-module",
                format!("StepUpChallenge must require {field}:{wanted}"),
            ));
        }
    }
    let inventoried = model.records.get("Session").is_some_and(|session| {
        session
            .required
            .iter()
            .any(|declared| declared.name == "device" && declared.type_name == "Device")
            && session.required.iter().any(|declared| {
                declared.name == "last_active_at" && declared.type_name == "Timestamp"
            })
    });
    if !inventoried {
        violations.push(violation(
            &origin,
            "missing-identity-module",
            "Session must carry device:Device and last_active_at:Timestamp",
        ));
    }
}

/// Enforces the movement module: the quote-then-commit Move money path, the
/// custody-boundary journey starts, the typed Refusal shape journeys carry and
/// the estimate/ceiling split the quote presents before commitment.
fn human_api_movement_module(model: &Model, root: &Path, violations: &mut Vec<Violation>) {
    let origin = root.join("movement.kvx");
    for operation in [
        "move.quote",
        "move.commit",
        "deposit.start",
        "withdraw.start",
    ] {
        if !model
            .operations
            .iter()
            .any(|declared| declared.name == operation)
        {
            violations.push(violation(
                &origin,
                "missing-movement-module",
                format!("the contract must declare operation.{operation}"),
            ));
        }
    }
    match model.records.get("Refusal") {
        Some(refusal) => {
            for (field, wanted) in [("refused_by", "RefusedBy"), ("money_left", "boolean")] {
                let carried = refusal
                    .required
                    .iter()
                    .any(|declared| declared.name == field && declared.type_name == wanted);
                if !carried {
                    violations.push(violation(
                        &origin,
                        "missing-movement-module",
                        format!("Refusal must require {field}:{wanted}"),
                    ));
                }
            }
        }
        None => violations.push(violation(
            &origin,
            "missing-movement-module",
            "the contract must declare the Refusal record journeys carry",
        )),
    }
    let separated = model.records.get("MoveQuote").is_some_and(|quote| {
        quote
            .required
            .iter()
            .any(|declared| declared.name == "fee_estimate")
            && quote
                .required
                .iter()
                .any(|declared| declared.name == "fee_ceiling")
    });
    if !separated {
        violations.push(violation(
            &origin,
            "missing-movement-module",
            "MoveQuote must separate fee_estimate from fee_ceiling",
        ));
    }
}

const SETTLEMENT_DOMAIN_ENUM: &str = "SettlementDomain";

const SOLE_SETTLEMENT_DOMAIN: &str = "paxeer";

const CUSTODY_CLAIM_SHAPES: &[(&str, &str)] = &[
    ("movement.kvx", "WalletSignRequest"),
    ("movement.kvx", "DepositStartRequest"),
    ("movement.kvx", "DepositConfirmRequest"),
    ("movement.kvx", "WithdrawStartRequest"),
    ("movement.kvx", "WithdrawClaimRequest"),
    ("movement.kvx", "ExitStartRequest"),
    ("movement.kvx", "ExitEligibility"),
    ("journeys.kvx", "EvidenceRef"),
    ("journeys.kvx", "EvidenceMaterial"),
];

const INTERNAL_MOVEMENT_SHAPES: &[&str] = &["MoveQuoteRequest", "MoveQuote", "MoveCommitRequest"];

const CUSTODY_CLAIM_OPERATIONS: &[&str] = &[
    "deposit.start",
    "deposit.confirm",
    "withdraw.start",
    "withdraw.claim",
    "exit.start",
];

fn record_names_settlement_domain(record: &Record) -> bool {
    record
        .required
        .iter()
        .chain(&record.optional)
        .any(|field| field.name == "settlement_domain" && field.type_name == SETTLEMENT_DOMAIN_ENUM)
}

/// Enforces the reserved settlement-domain claim vocabulary: every
/// custody-boundary claim and receipt shape names its settlement domain, with
/// paxeer the sole valid domain in this contract version, an internal movement
/// never names one, and an untagged custody claim is rejected.
fn human_api_settlement_domains(
    files: &[SchemaFile],
    model: &Model,
    root: &Path,
    violations: &mut Vec<Violation>,
) {
    let origin = root.join("movement.kvx");
    match model.enums.get(SETTLEMENT_DOMAIN_ENUM) {
        Some(definition) => {
            if definition.variants.first().map(String::as_str) != Some(SOLE_SETTLEMENT_DOMAIN) {
                violations.push(violation(
                    &origin,
                    "invalid-settlement-domain",
                    format!("{SETTLEMENT_DOMAIN_ENUM} must declare {SOLE_SETTLEMENT_DOMAIN} as its first variant"),
                ));
            }
        }
        None => {
            violations.push(violation(
                &origin,
                "missing-settlement-domain",
                format!("the contract must declare the {SETTLEMENT_DOMAIN_ENUM} enum reserving the claim vocabulary"),
            ));
            return;
        }
    }
    for (file_name, shape) in CUSTODY_CLAIM_SHAPES {
        let origin = root.join(file_name);
        let section = format!("type.{shape}");
        let declared = files
            .iter()
            .find(|file| file.name == *file_name)
            .and_then(|file| file.sections.get(&section));
        match declared.and_then(|entries| quoted(entries, "settlement_domain")) {
            Some(domain) if domain == SOLE_SETTLEMENT_DOMAIN => {}
            Some(domain) => violations.push(violation(
                &origin,
                "invalid-settlement-domain",
                format!("{section} names settlement domain {domain}; {SOLE_SETTLEMENT_DOMAIN} is the sole valid domain in this version"),
            )),
            None => violations.push(violation(
                &origin,
                "untagged-custody-claim",
                format!("{section} is a custody-boundary shape and must declare settlement_domain = \"{SOLE_SETTLEMENT_DOMAIN}\""),
            )),
        }
        match model.records.get(*shape) {
            Some(record) if record_names_settlement_domain(record) => {}
            Some(_) => violations.push(violation(
                &origin,
                "untagged-custody-claim",
                format!("{shape} must carry a settlement_domain:{SETTLEMENT_DOMAIN_ENUM} field"),
            )),
            None => {}
        }
    }
    for shape in INTERNAL_MOVEMENT_SHAPES {
        let tagged_declaration = files.iter().any(|file| {
            file.sections
                .get(&format!("type.{shape}"))
                .is_some_and(|entries| entries.contains_key("settlement_domain"))
        });
        let tagged_field = model
            .records
            .get(*shape)
            .is_some_and(record_names_settlement_domain);
        if tagged_declaration || tagged_field {
            violations.push(violation(
                &origin,
                "invalid-settlement-domain",
                format!("{shape} is an internal movement shape and never names a settlement domain under the one-ledger rule"),
            ));
        }
    }
    for operation in CUSTODY_CLAIM_OPERATIONS {
        let path = root
            .join("golden")
            .join(format!("{operation}.request.json"));
        let Ok(source) = fs::read_to_string(&path) else {
            continue;
        };
        let Ok(vector) = parse_json(&source) else {
            continue;
        };
        let Some(body) = vector.field("body") else {
            continue;
        };
        match body.field("settlement_domain") {
            Some(Json::String(domain)) if domain == SOLE_SETTLEMENT_DOMAIN => {}
            Some(_) => violations.push(violation(
                &path,
                "invalid-settlement-domain",
                format!("operation.{operation} claim names a settlement domain other than {SOLE_SETTLEMENT_DOMAIN}"),
            )),
            None => violations.push(violation(
                &path,
                "untagged-custody-claim",
                format!("operation.{operation} is a custody-boundary claim and its vector must name settlement_domain \"{SOLE_SETTLEMENT_DOMAIN}\""),
            )),
        }
    }
}

/// Enforces the managed-agent module: the lifecycle and approval-inbox
/// operations and the approval facts spine every rendered decision relies on.
fn human_api_agent_module(model: &Model, root: &Path, violations: &mut Vec<Violation>) {
    let origin = root.join("agents.kvx");
    for operation in ["agent.create", "agent.archive", "approval.approve"] {
        if !model
            .operations
            .iter()
            .any(|declared| declared.name == operation)
        {
            violations.push(violation(
                &origin,
                "missing-agent-module",
                format!("the contract must declare operation.{operation}"),
            ));
        }
    }
    let Some(facts) = model.records.get("ApprovalFacts") else {
        violations.push(violation(
            &origin,
            "missing-agent-module",
            "the contract must declare the ApprovalFacts record",
        ));
        return;
    };
    for (field, wanted) in [
        ("amount", "Money"),
        ("counterparty", "string"),
        ("asset", "CurrencyCode"),
        ("fees", "Money"),
        ("expires_at", "Timestamp"),
    ] {
        let carried = facts
            .required
            .iter()
            .any(|declared| declared.name == field && declared.type_name == wanted);
        if !carried {
            violations.push(violation(
                &origin,
                "missing-agent-module",
                format!("ApprovalFacts must require {field}:{wanted}"),
            ));
        }
    }
}

/// Enforces the activity module: the unified feed, entry detail and export
/// operations and the self-describing feed page spine.
fn human_api_activity_module(model: &Model, root: &Path, violations: &mut Vec<Violation>) {
    let origin = root.join("activity.kvx");
    for operation in [
        "activity.query",
        "activity.entry",
        "activity.export.statement",
        "activity.export.evidence",
    ] {
        if !model
            .operations
            .iter()
            .any(|declared| declared.name == operation)
        {
            violations.push(violation(
                &origin,
                "missing-activity-module",
                format!("the contract must declare operation.{operation}"),
            ));
        }
    }
    let paged = model.records.get("ActivityPage").is_some_and(|page| {
        page.required
            .iter()
            .any(|declared| declared.name == "groups" && declared.array)
            && page
                .required
                .iter()
                .any(|declared| declared.name == "next_cursor")
            && page
                .required
                .iter()
                .any(|declared| declared.name == "filter")
    });
    if !paged {
        violations.push(violation(
            &origin,
            "missing-activity-module",
            "ActivityPage must carry groups:ActivityGroup[], next_cursor:Cursor and the echoed filter",
        ));
    }
}

fn check_golden_failure(
    model: &Model,
    operation: &Operation,
    path: &Path,
    vector: &Json,
    violations: &mut Vec<Violation>,
) {
    let Json::Object(pairs) = vector else {
        violations.push(violation(
            path,
            "invalid-golden-vector",
            "failure vector must be a JSON object",
        ));
        return;
    };
    for (key, _) in pairs {
        if !matches!(key.as_str(), "status" | "body") {
            violations.push(violation(
                path,
                "unexpected-vector-key",
                format!("failure vector key {key} is not part of the convention"),
            ));
        }
    }
    let status_valid = matches!(
        vector.field("status"),
        Some(Json::Number(text))
            if is_integer_text(text) && text.parse::<i64>().is_ok_and(|status| (400..600).contains(&status))
    );
    if !status_valid {
        violations.push(violation(
            path,
            "invalid-failure-status",
            "failure vector status must be a 4xx or 5xx integer",
        ));
    }
    let Some(body @ Json::Object(body_pairs)) = vector.field("body") else {
        violations.push(violation(
            path,
            "invalid-failure-envelope",
            "failure vector body must be a JSON object",
        ));
        return;
    };
    for (key, _) in body_pairs {
        if !matches!(key.as_str(), "ok" | "error" | "trace") {
            violations.push(violation(
                path,
                "invalid-failure-envelope",
                format!("envelope key {key} is not part of the convention"),
            ));
        }
    }
    if body.field("ok") != Some(&Json::Bool(false)) {
        violations.push(violation(
            path,
            "invalid-failure-envelope",
            "golden failure envelopes carry ok false",
        ));
    }
    match body.field("trace") {
        Some(trace @ Json::String(text)) if !text.is_empty() => {
            if let Some(scalar) = model.scalars.get("TraceId") {
                check_scalar_value(scalar, "TraceId", trace, path, "body.trace", violations);
            }
        }
        _ => violations.push(violation(
            path,
            "invalid-failure-envelope",
            "envelope trace must be a non-empty string",
        )),
    }
    match body.field("error") {
        Some(error) if model.records.contains_key("ApiError") => {
            check_value(model, "ApiError", error, path, "body.error", violations);
        }
        Some(_) => {}
        None => violations.push(violation(
            path,
            "invalid-failure-envelope",
            format!(
                "operation.{} failure envelope must carry the structured error",
                operation.name
            ),
        )),
    }
}

fn check_golden(
    root: &Path,
    model: &Model,
    encoding: &Encoding,
    operation: &Operation,
    violations: &mut Vec<Violation>,
) -> usize {
    let mut vectors = 0;
    let request_path = root
        .join("golden")
        .join(format!("{}.request.json", operation.name));
    match fs::read_to_string(&request_path) {
        Ok(body) => match parse_json(&body) {
            Ok(vector) => {
                vectors += 1;
                check_golden_request(
                    model,
                    encoding,
                    operation,
                    &request_path,
                    &vector,
                    violations,
                );
            }
            Err(detail) => violations.push(violation(&request_path, "invalid-golden-json", detail)),
        },
        Err(_) => violations.push(violation(
            &request_path,
            "missing-golden-vector",
            format!("operation.{} has no golden request vector", operation.name),
        )),
    }
    let response_path = root
        .join("golden")
        .join(format!("{}.response.json", operation.name));
    match fs::read_to_string(&response_path) {
        Ok(body) => match parse_json(&body) {
            Ok(vector) => {
                vectors += 1;
                check_golden_response(model, operation, &response_path, &vector, violations);
            }
            Err(detail) => {
                violations.push(violation(&response_path, "invalid-golden-json", detail))
            }
        },
        Err(_) => violations.push(violation(
            &response_path,
            "missing-golden-vector",
            format!("operation.{} has no golden response vector", operation.name),
        )),
    }
    let failure_path = root
        .join("golden")
        .join(format!("{}.failure.json", operation.name));
    match fs::read_to_string(&failure_path) {
        Ok(body) => match parse_json(&body) {
            Ok(vector) => {
                vectors += 1;
                check_golden_failure(model, operation, &failure_path, &vector, violations);
            }
            Err(detail) => violations.push(violation(&failure_path, "invalid-golden-json", detail)),
        },
        Err(_) => violations.push(violation(
            &failure_path,
            "missing-failure-vector",
            format!("operation.{} has no golden failure vector", operation.name),
        )),
    }
    vectors
}

fn check_orphan_goldens(root: &Path, model: &Model, push: bool, violations: &mut Vec<Violation>) {
    let golden = root.join("golden");
    let Ok(entries) = fs::read_dir(&golden) else {
        violations.push(violation(
            &golden,
            "missing-golden-directory",
            "the golden vector directory is required",
        ));
        return;
    };
    for entry in entries.flatten() {
        let path = entry.path();
        let name = entry.file_name().to_string_lossy().into_owned();
        if push && name == "stream-push-v2" && path.is_dir() {
            continue;
        }
        let stem = name
            .strip_suffix(".request.json")
            .or_else(|| name.strip_suffix(".response.json"))
            .or_else(|| name.strip_suffix(".failure.json"));
        let owned = stem.is_some_and(|stem| {
            model
                .operations
                .iter()
                .any(|operation| operation.name == stem)
        });
        if !owned {
            violations.push(violation(
                &path,
                "orphan-golden-vector",
                format!("{name} does not belong to a declared operation"),
            ));
        }
    }
}

fn check_compatibility(
    root: &Path,
    major: u32,
    minor: u32,
    violations: &mut Vec<Violation>,
) -> Option<SchemaFile> {
    let file = load_kvx(root, "compatibility.kvx", violations)?;
    match file
        .sections
        .get("compatibility")
        .and_then(|top| quoted(top, "rule"))
    {
        Some(rule) if !rule.is_empty() => {}
        _ => violations.push(violation(
            &file.path,
            "invalid-compatibility-matrix",
            "compatibility.rule must document the matrix",
        )),
    }
    let rows: Vec<(&String, &BTreeMap<String, String>)> = file
        .sections
        .iter()
        .filter(|(name, _)| name.starts_with("compatibility.row."))
        .collect();
    if rows.is_empty() {
        violations.push(violation(
            &file.path,
            "invalid-compatibility-matrix",
            "the matrix must record at least one row",
        ));
    }
    for (name, entries) in &rows {
        for key in ["schema", "service", "web_app"] {
            if quoted(entries, key).is_none_or(str::is_empty) {
                violations.push(violation(
                    &file.path,
                    "invalid-compatibility-row",
                    format!("{name} must declare a quoted {key} version"),
                ));
            }
        }
    }
    let wanted = format!("{major}.{minor}");
    let recorded = rows
        .iter()
        .any(|(_, entries)| quoted(entries, "schema") == Some(wanted.as_str()));
    if !rows.is_empty() && !recorded {
        violations.push(violation(
            &file.path,
            "unrecorded-schema-version",
            format!("no matrix row records schema version {wanted}"),
        ));
    }
    Some(file)
}

fn flatten(files: &[SchemaFile]) -> BTreeMap<String, String> {
    let mut entries = BTreeMap::new();
    for file in files {
        for (section, keys) in &file.sections {
            for (key, value) in keys {
                if file.name == "v1.kvx" && section == "schema" && key == "minor" {
                    continue;
                }
                entries.insert(format!("{}:{section}.{key}", file.name), value.clone());
            }
        }
    }
    entries
}

fn check_baseline(
    root: &Path,
    major: u32,
    current: &BTreeMap<String, String>,
    violations: &mut Vec<Violation>,
) -> usize {
    let path = root.join(BASELINE_FILE);
    let Ok(body) = fs::read_to_string(&path) else {
        violations.push(violation(
            &path,
            "missing-baseline",
            format!(
                "no frozen baseline manifest; generate it deliberately with: {REFRESH_COMMAND}"
            ),
        ));
        return 0;
    };
    let sections = match parse_kvx(&body) {
        Ok(sections) => sections,
        Err(detail) => {
            violations.push(violation(&path, "invalid-kvx", detail));
            return 0;
        }
    };
    let baseline_major = sections
        .get("manifest")
        .and_then(|manifest| manifest.get("schema_major"))
        .and_then(|value| value.parse::<u32>().ok());
    if baseline_major != Some(major) {
        violations.push(violation(
            &path,
            "stale-baseline-major",
            format!("the baseline records another major version; refresh it deliberately with: {REFRESH_COMMAND}"),
        ));
        return 0;
    }
    let Some(baseline) = sections.get("entries") else {
        violations.push(violation(
            &path,
            "invalid-baseline",
            "the baseline manifest declares no [entries] section",
        ));
        return 0;
    };
    for (key, old_value) in baseline {
        match current.get(key) {
            Some(new_value) if new_value == old_value => {}
            Some(new_value) if additive_list_extension(old_value, new_value) => {}
            Some(new_value) => violations.push(violation(
                &path,
                "breaking-declaration-change",
                format!("{key}: {old_value} -> {new_value}"),
            )),
            None => violations.push(violation(
                &path,
                "breaking-declaration-removal",
                format!("{key} was removed within major {major}"),
            )),
        }
    }
    current
        .keys()
        .filter(|key| !baseline.contains_key(*key))
        .count()
}

fn load_all(root: &Path, violations: &mut Vec<Violation>) -> Option<(Header, Vec<SchemaFile>)> {
    let v1 = load_kvx(root, "v1.kvx", violations)?;
    let header = check_header(&v1, violations);
    let mut files = vec![v1];
    for include in &header.includes {
        let named_kvx = Path::new(include)
            .extension()
            .is_some_and(|extension| extension == "kvx");
        if named_kvx && root.join(include).is_file() {
            if let Some(file) = load_kvx(root, include, violations) {
                files.push(file);
            }
        } else {
            violations.push(violation(
                &root.join(include),
                "missing-include",
                format!("include {include} does not exist beside v1.kvx"),
            ));
        }
    }
    Some((header, files))
}

/// Validates the human-api contract schema: versions, includes, declarations,
/// operations, golden vectors, the compatibility matrix and the additive-only
/// baseline gate.
///
/// # Errors
///
/// Returns every conformance violation found under `root`.
pub fn human_api_schema(root: &Path) -> Result<SchemaReport, Vec<Violation>> {
    let mut violations = Vec::new();
    let Some((header, files)) = load_all(root, &mut violations) else {
        return Err(violations);
    };
    let encoding = files.first().map_or_else(
        || Encoding {
            base_path: "/v1".to_owned(),
            idempotency_header: "Idempotency-Key".to_owned(),
        },
        |v1| check_encoding(v1, &mut violations),
    );
    for file in files.iter().skip(1) {
        check_module_header(file, header.major, &mut violations);
    }
    let model = collect_declarations(&files, &mut violations);
    check_type_references(&model, &mut violations);
    check_operation_declarations(&model, &encoding, &mut violations);
    human_api_error_model(&model, root, &mut violations);
    human_api_stream_module(&model, root, &mut violations);
    let push = human_api_stream_push(&files, &model, root, &mut violations);
    human_api_identity_module(&model, root, &mut violations);
    human_api_movement_module(&model, root, &mut violations);
    human_api_settlement_domains(&files, &model, root, &mut violations);
    human_api_agent_module(&model, root, &mut violations);
    human_api_activity_module(&model, root, &mut violations);
    let mut golden_vectors = 0;
    for operation in &model.operations {
        golden_vectors += check_golden(root, &model, &encoding, operation, &mut violations);
    }
    check_orphan_goldens(root, &model, push, &mut violations);
    let mut all_files = files;
    if let Some(compatibility) =
        check_compatibility(root, header.major, header.minor, &mut violations)
    {
        all_files.push(compatibility);
    }
    let current = flatten(&all_files);
    let additions = check_baseline(root, header.major, &current, &mut violations);
    if violations.is_empty() {
        Ok(SchemaReport {
            declarations: current.len(),
            operations: model.operations.len(),
            golden_vectors,
            additions_beyond_baseline: additions,
        })
    } else {
        Err(violations)
    }
}

/// Renders the frozen baseline manifest of every current schema declaration.
///
/// # Errors
///
/// Returns every violation preventing the schema files from being loaded.
pub fn baseline_manifest(root: &Path) -> Result<String, Vec<Violation>> {
    let mut violations = Vec::new();
    let Some((header, files)) = load_all(root, &mut violations) else {
        return Err(violations);
    };
    let mut all_files = files;
    if let Some(compatibility) = load_kvx(root, "compatibility.kvx", &mut violations) {
        all_files.push(compatibility);
    }
    if !violations.is_empty() {
        return Err(violations);
    }
    let mut manifest = String::new();
    manifest.push_str("[manifest]\n");
    manifest.push_str("tool = \"layerx-human-schema-check\"\n");
    let _ = writeln!(manifest, "schema_major = {}", header.major);
    let _ = writeln!(manifest, "refresh = \"{REFRESH_COMMAND}\"");
    manifest.push_str("\n[entries]\n");
    for (key, value) in &flatten(&all_files) {
        let _ = writeln!(manifest, "{key} = {value}");
    }
    Ok(manifest)
}
