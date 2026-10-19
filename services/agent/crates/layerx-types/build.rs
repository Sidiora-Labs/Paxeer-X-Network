use std::env;
use std::fmt::Write as _;
use std::fs;
use std::path::{Path, PathBuf};

#[derive(Debug, Eq, PartialEq)]
struct Code {
    name: String,
    value: i32,
}

struct HeaderCode {
    code: Code,
    c_name: String,
}

fn read_file(path: &Path) -> String {
    fs::read_to_string(path)
        .unwrap_or_else(|error| panic!("failed to read {}: {error}", path.display()))
}

fn rust_name(c_name: &str) -> Result<String, String> {
    if c_name == "LXP_OK" {
        return Ok("Ok".to_owned());
    }
    let (prefix, rest) = if let Some(rest) = c_name.strip_prefix("LXP_ERR_") {
        ("", rest)
    } else if let Some(rest) = c_name.strip_prefix("LXP_FATAL_") {
        ("Fatal", rest)
    } else {
        return Err(format!("unmapped result code prefix in {c_name}"));
    };
    let mut mapped = prefix.to_owned();
    for word in rest.split('_') {
        if word.is_empty() {
            return Err(format!("empty word in {c_name}"));
        }
        let mut characters = word.chars();
        let Some(first) = characters.next() else {
            return Err(format!("empty word in {c_name}"));
        };
        mapped.extend(first.to_uppercase());
        mapped.extend(characters.flat_map(char::to_lowercase));
    }
    Ok(mapped)
}

fn header_codes(source: &str) -> Result<Vec<HeaderCode>, String> {
    let mut codes = Vec::new();
    let mut inside = false;
    for raw in source.lines() {
        let line = raw.trim();
        if line.starts_with("#define LXP_RESULT_CODE_LIST(X)") {
            inside = true;
            continue;
        }
        if !inside {
            continue;
        }
        let Some(body) = line.strip_prefix("X(") else {
            return Err(format!("unexpected line inside the code table: {line}"));
        };
        let body = body
            .trim_end_matches('\\')
            .trim_end()
            .strip_suffix(')')
            .ok_or_else(|| format!("unterminated table entry: {line}"))?;
        let (name, value) = body
            .split_once(',')
            .ok_or_else(|| format!("table entry without a number: {line}"))?;
        let value = value
            .trim()
            .parse::<i32>()
            .map_err(|error| format!("invalid number in {line}: {error}"))?;
        let c_name = name.trim();
        codes.push(HeaderCode {
            code: Code {
                name: rust_name(c_name)?,
                value,
            },
            c_name: c_name.to_owned(),
        });
        if !raw.trim_end().ends_with('\\') {
            inside = false;
        }
    }
    if codes.is_empty() {
        return Err("LXP_RESULT_CODE_LIST is empty or unreadable".to_owned());
    }
    Ok(codes)
}

fn mirror_codes(source: &str) -> Result<Vec<Code>, String> {
    let body = source
        .split_once("protocol_result_codes! {")
        .ok_or_else(|| "protocol_result_codes! invocation is missing".to_owned())?
        .1
        .split_once("\n}")
        .ok_or_else(|| "protocol_result_codes! invocation is unterminated".to_owned())?
        .0;
    let mut codes = Vec::new();
    for raw in body.lines() {
        let line = raw.trim();
        if line.is_empty() {
            continue;
        }
        let entry = line
            .strip_suffix(';')
            .ok_or_else(|| format!("mirror entry without a terminator: {line}"))?;
        let (name, tail) = entry
            .split_once(" = ")
            .ok_or_else(|| format!("mirror entry without a number: {line}"))?;
        let (value, retriability) = tail
            .split_once(", ")
            .ok_or_else(|| format!("mirror entry without a retriability: {line}"))?;
        if retriability != "Terminal" && retriability != "Retriable" {
            return Err(format!("unknown retriability in {line}"));
        }
        let value = value
            .parse::<i32>()
            .map_err(|error| format!("invalid number in {line}: {error}"))?;
        codes.push(Code {
            name: name.to_owned(),
            value,
        });
    }
    Ok(codes)
}

fn registry_retryable(source: &str) -> Result<Vec<(String, String)>, String> {
    let section = source
        .split_once("\n## (b) ")
        .ok_or_else(|| "section (b) Error code registry is missing".to_owned())?
        .1;
    let section = section
        .split_once("\n## ")
        .map_or(section, |(body, _)| body);
    let mut rows = Vec::new();
    for line in section.lines() {
        if !line.starts_with("| LXP_") {
            continue;
        }
        let cells: Vec<&str> = line.split('|').map(str::trim).collect();
        let ["", code, _message, _construct, _origin, retryable, ""] = cells.as_slice() else {
            return Err(format!(
                "registry row does not have the five columns Code, Message, Emitting construct, origin and Retryable: {line}"
            ));
        };
        rows.push(((*code).to_owned(), (*retryable).to_owned()));
    }
    if rows.is_empty() {
        return Err("section (b) holds no LXP_ registry row".to_owned());
    }
    Ok(rows)
}

fn registry_code_name(c_name: &str) -> String {
    c_name
        .strip_prefix("LXP_ERR_")
        .map_or_else(|| c_name.to_owned(), |rest| format!("LXP_{rest}"))
}

fn retriability(
    header: &HeaderCode,
    registry: &[(String, String)],
) -> Result<&'static str, String> {
    if header.c_name == "LXP_OK" {
        return Ok("Terminal");
    }
    let code = registry_code_name(&header.c_name);
    let mut rows = registry.iter().filter(|(name, _)| *name == code);
    let Some((_, cell)) = rows.next() else {
        return Err(format!(
            "{} has no registry row {code} in section (b)",
            header.c_name
        ));
    };
    if rows.next().is_some() {
        return Err(format!(
            "{code} has more than one registry row in section (b)"
        ));
    }
    let class = match cell.as_str() {
        "yes" => "Retriable",
        "no" => "Terminal",
        other => {
            return Err(format!(
                "{code} has the Retryable cell {other:?}, which is neither yes nor no"
            ));
        }
    };
    if header.code.value <= -1000 {
        return Ok("Terminal");
    }
    Ok(class)
}

fn result_codes_body(
    header: &[HeaderCode],
    registry: &[(String, String)],
) -> Result<String, String> {
    let mut entries = String::new();
    for code in header {
        let class = retriability(code, registry)?;
        writeln!(
            entries,
            "    {} = {}, {class};",
            code.code.name, code.code.value
        )
        .map_err(|error| format!("failed to format the result-code table: {error}"))?;
    }
    Ok(format!(
        "// Generated from include/layerx/lxp_result.h and the Retryable column of\n\
// spec/layerx-merge/contracts/address-table.md section (b) by build.rs. Do not edit.\n\n\
protocol_result_codes! {{\n{entries}}}\n"
    ))
}

fn guest_abi_maximum(source: &str) -> Result<u16, String> {
    let mut maximum = 0;
    for raw in source.lines() {
        let Some(rest) = raw.trim().strip_prefix("LX_PROGRAMS_GUEST_ABI_V") else {
            continue;
        };
        let (declared, value) = rest
            .split_once("_VERSION = ")
            .ok_or_else(|| format!("unexpected guest ABI enumerator: {rest}"))?;
        let value = value
            .trim_end_matches(',')
            .trim()
            .parse::<u16>()
            .map_err(|error| format!("invalid guest ABI number in {rest}: {error}"))?;
        if declared.parse::<u16>().ok() != Some(value) {
            return Err(format!(
                "guest ABI enumerator {rest} names a version it does not equal"
            ));
        }
        maximum = maximum.max(value);
    }
    if maximum == 0 {
        return Err("no LX_PROGRAMS_GUEST_ABI_V*_VERSION enumerator is declared".to_owned());
    }
    Ok(maximum)
}

struct ProgramsModuleAbi {
    versions: Vec<(String, u32)>,
    initial: u32,
    account: u32,
    sandbox: u32,
    sandbox_destroy: u32,
}

fn programs_module_abi_version(table: &[(String, u32)], name: &str) -> Result<u32, String> {
    table
        .iter()
        .find(|(declared, _)| declared == name)
        .map(|(_, version)| *version)
        .ok_or_else(|| format!("no {name} enumerator is declared"))
}

fn programs_module_abi(source: &str) -> Result<ProgramsModuleAbi, String> {
    let mut versions: Vec<(String, u32)> = Vec::new();
    for raw in source.lines() {
        let line = raw.trim();
        let Some(rest) = line.strip_prefix("LX_PROGRAMS_") else {
            continue;
        };
        let Some((suffix, value)) = rest.split_once(" = ") else {
            continue;
        };
        if !suffix.ends_with("ABI_VERSION") {
            continue;
        }
        let value = value
            .trim_end_matches(',')
            .trim()
            .parse::<u32>()
            .map_err(|error| format!("invalid Programs module ABI number in {line}: {error}"))?;
        if value == 0 {
            return Err(format!("Programs module ABI {line} declares version zero"));
        }
        if versions.iter().any(|(_, declared)| *declared == value) {
            return Err(format!(
                "Programs module ABI {line} repeats version {value}"
            ));
        }
        let expected = u32::try_from(versions.len() + 1)
            .map_err(|error| format!("too many Programs module ABI versions: {error}"))?;
        if value != expected {
            return Err(format!(
                "Programs module ABI {line} does not ascend from one without a gap; {expected} was expected"
            ));
        }
        versions.push((format!("LX_PROGRAMS_{suffix}"), value));
    }
    if versions.is_empty() {
        return Err("no LX_PROGRAMS_*ABI_VERSION enumerator is declared".to_owned());
    }
    let initial = programs_module_abi_version(&versions, "LX_PROGRAMS_ABI_VERSION")?;
    let account = programs_module_abi_version(&versions, "LX_PROGRAMS_ACCOUNT_ABI_VERSION")?;
    let sandbox = programs_module_abi_version(&versions, "LX_PROGRAMS_SANDBOX_ABI_VERSION")?;
    let sandbox_destroy =
        programs_module_abi_version(&versions, "LX_PROGRAMS_SANDBOX_DESTROY_ABI_VERSION")?;
    let highest = versions
        .iter()
        .map(|(_, version)| *version)
        .max()
        .ok_or_else(|| "no Programs module ABI version is declared".to_owned())?;
    if sandbox_destroy != highest {
        return Err(format!(
            "the highest Programs module ABI {highest} is not LX_PROGRAMS_SANDBOX_DESTROY_ABI_VERSION = {sandbox_destroy}, the version src/modules/programs/registration.c registers the module at"
        ));
    }
    if initial >= account || account >= sandbox || sandbox >= sandbox_destroy {
        return Err(format!(
            "the named Programs module ABI versions {initial}, {account}, {sandbox} and {sandbox_destroy} do not ascend"
        ));
    }
    Ok(ProgramsModuleAbi {
        versions,
        initial,
        account,
        sandbox,
        sandbox_destroy,
    })
}

fn programs_module_abi_body(table: &ProgramsModuleAbi) -> String {
    let mut entries = String::new();
    for (name, version) in &table.versions {
        writeln!(entries, "    (\"{name}\", {version}),")
            .unwrap_or_else(|error| panic!("failed to format the module ABI table: {error}"));
    }
    let count = table.versions.len();
    let initial = table.initial;
    let account = table.account;
    let sandbox = table.sandbox;
    let sandbox_destroy = table.sandbox_destroy;
    format!(
        "// Generated from include/layerx/programs.h by build.rs. Do not edit.\n\n\
/// Every Programs module ABI version the kernel header allocates, in header order.\n\
pub const VERSIONS: [(&str, u32); {count}] = [\n{entries}];\n\n\
/// The version the kernel header allocates to `LX_PROGRAMS_ABI_VERSION`.\n\
pub const INITIAL: u32 = {initial};\n\n\
/// The version the kernel header allocates to `LX_PROGRAMS_ACCOUNT_ABI_VERSION`.\n\
pub const ACCOUNT: u32 = {account};\n\n\
/// The version the kernel header allocates to `LX_PROGRAMS_SANDBOX_ABI_VERSION`.\n\
pub const SANDBOX: u32 = {sandbox};\n\n\
/// The version the kernel header allocates to\n\
/// `LX_PROGRAMS_SANDBOX_DESTROY_ABI_VERSION`, the version the Programs module\n\
/// registers at and the only module ABI a state-commitment receipt carries.\n\
pub const SANDBOX_DESTROY: u32 = {sandbox_destroy};\n\n\
/// The Programs module ABI version the kernel publishes at now.\n\
pub const CURRENT: u32 = {sandbox_destroy};\n"
    )
}

struct ProgramsActivityTable {
    module: u16,
    types: Vec<(String, u16)>,
    deploy: u16,
    upgrade: u16,
    call: u16,
    call_operation: u8,
}

fn programs_activity_ordinal(table: &[(String, u16)], name: &str) -> Result<u16, String> {
    table
        .iter()
        .find(|(declared, _)| declared == name)
        .map(|(_, ordinal)| *ordinal)
        .ok_or_else(|| format!("no {name} enumerator is declared"))
}

fn programs_activity_table(source: &str) -> Result<ProgramsActivityTable, String> {
    let mut module = 0u16;
    let mut types: Vec<(String, u16)> = Vec::new();
    for raw in source.lines() {
        let line = raw.trim();
        let Some(rest) = line.strip_prefix("LX_PROGRAMS_") else {
            continue;
        };
        let Some((suffix, value)) = rest.split_once(" = ") else {
            continue;
        };
        let Some(digits) = value.trim_end_matches(',').trim().strip_prefix("0x") else {
            continue;
        };
        let value = u32::from_str_radix(digits, 16)
            .map_err(|error| format!("invalid activity type number in {line}: {error}"))?;
        let declared_module = u16::try_from(value >> 16)
            .map_err(|error| format!("activity type {line} names no module: {error}"))?;
        let ordinal = u16::try_from(value & 0xffff)
            .map_err(|error| format!("activity type {line} names no ordinal: {error}"))?;
        if declared_module == 0 || ordinal == 0 {
            return Err(format!(
                "activity type {line} declares a zero module or a zero ordinal"
            ));
        }
        if module == 0 {
            module = declared_module;
        } else if module != declared_module {
            return Err(format!(
                "activity type {line} declares module {declared_module} while the table declares {module}"
            ));
        }
        if types.iter().any(|(_, declared)| *declared == ordinal) {
            return Err(format!("activity type {line} repeats ordinal {ordinal}"));
        }
        types.push((format!("LX_PROGRAMS_{suffix}"), ordinal));
    }
    if types.is_empty() {
        return Err("no LX_PROGRAMS_* activity type enumerator is declared".to_owned());
    }
    let deploy = programs_activity_ordinal(&types, "LX_PROGRAMS_DEPLOY")?;
    let upgrade = programs_activity_ordinal(&types, "LX_PROGRAMS_UPGRADE")?;
    let call = programs_activity_ordinal(&types, "LX_PROGRAMS_CALL")?;
    let call_operation = u8::try_from(call).map_err(|error| {
        format!("LX_PROGRAMS_CALL ordinal {call} does not fit the receipt operation tag: {error}")
    })?;
    Ok(ProgramsActivityTable {
        module,
        types,
        deploy,
        upgrade,
        call,
        call_operation,
    })
}

fn programs_activity_body(table: &ProgramsActivityTable) -> String {
    let mut entries = String::new();
    for (name, ordinal) in &table.types {
        writeln!(entries, "    (\"{name}\", {ordinal}),")
            .unwrap_or_else(|error| panic!("failed to format the activity table: {error}"));
    }
    let count = table.types.len();
    let module = table.module;
    let deploy = table.deploy;
    let upgrade = table.upgrade;
    let call = table.call;
    let call_operation = table.call_operation;
    format!(
        "// Generated from include/layerx/programs.h by build.rs. Do not edit.\n\n\
/// The protocol module every Programs activity type of the kernel header carries.\n\
pub const MODULE_ID: u16 = {module};\n\n\
/// Every Programs activity type the kernel header allocates, in header order.\n\
pub const ORDINALS: [(&str, u16); {count}] = [\n{entries}];\n\n\
/// The ordinal the kernel header allocates to `LX_PROGRAMS_DEPLOY`.\n\
pub const DEPLOY_ORDINAL: u16 = {deploy};\n\n\
/// The ordinal the kernel header allocates to `LX_PROGRAMS_UPGRADE`.\n\
pub const UPGRADE_ORDINAL: u16 = {upgrade};\n\n\
/// The ordinal the kernel header allocates to `LX_PROGRAMS_CALL`.\n\
pub const CALL_ORDINAL: u16 = {call};\n\n\
/// The one-byte ledger operation tag the kernel binds into a call receipt.\n\
pub const CALL_OPERATION: u8 = {call_operation};\n"
    )
}

fn parity(header: &[Code], mirror: &[Code]) -> Result<(), String> {
    for (index, expected) in header.iter().enumerate() {
        match mirror.get(index) {
            Some(actual) if actual == expected => {}
            Some(actual) => {
                return Err(format!(
                    "result code {index} drifted: header declares {} = {}, the generated result_codes.rs declares {} = {}",
                    expected.name, expected.value, actual.name, actual.value
                ));
            }
            None => {
                return Err(format!(
                    "the generated result_codes.rs is missing {} = {}",
                    expected.name, expected.value
                ));
            }
        }
    }
    if mirror.len() > header.len() {
        let extra = &mirror[header.len()];
        return Err(format!(
            "the generated result_codes.rs declares {} = {} which the header does not",
            extra.name, extra.value
        ));
    }
    Ok(())
}

fn main() {
    let crate_dir = PathBuf::from(
        env::var_os("CARGO_MANIFEST_DIR")
            .unwrap_or_else(|| panic!("CARGO_MANIFEST_DIR is unavailable")),
    );
    let header = crate_dir.join("../../../../include/layerx/lxp_result.h");
    let registry = crate_dir.join("../../../../spec/layerx-merge/contracts/address-table.md");
    println!("cargo:rerun-if-changed={}", header.display());
    println!("cargo:rerun-if-changed={}", registry.display());
    let declared = header_codes(&read_file(&header))
        .unwrap_or_else(|error| panic!("invalid {}: {error}", header.display()));
    let rows = registry_retryable(&read_file(&registry))
        .unwrap_or_else(|error| panic!("invalid {}: {error}", registry.display()));
    let table = result_codes_body(&declared, &rows).unwrap_or_else(|error| {
        panic!("protocol result-code registry failure: {error}");
    });
    let generated_codes = mirror_codes(&table)
        .unwrap_or_else(|error| panic!("invalid generated result-code table: {error}"));
    let header_list: Vec<Code> = declared.into_iter().map(|code| code.code).collect();
    parity(&header_list, &generated_codes).unwrap_or_else(|error| {
        panic!("protocol result-code parity failure: {error}");
    });
    let out_dir =
        PathBuf::from(env::var_os("OUT_DIR").unwrap_or_else(|| panic!("OUT_DIR is unavailable")));
    let generated = out_dir.join("result_codes.rs");
    fs::write(&generated, table)
        .unwrap_or_else(|error| panic!("failed to write {}: {error}", generated.display()));
    let programs = crate_dir.join("../../../../include/layerx/programs.h");
    println!("cargo:rerun-if-changed={}", programs.display());
    let programs_header = read_file(&programs);
    let maximum = guest_abi_maximum(&programs_header)
        .unwrap_or_else(|error| panic!("invalid {}: {error}", programs.display()));
    let generated = out_dir.join("guest_abi.rs");
    let body = format!(
        "// Generated from include/layerx/programs.h by build.rs. Do not edit.\n\n\
/// The highest guest ABI version the kernel's Programs module admits.\n\
pub const MAX_VERSION: u16 = {maximum};\n"
    );
    fs::write(&generated, body)
        .unwrap_or_else(|error| panic!("failed to write {}: {error}", generated.display()));
    let activity = programs_activity_table(&programs_header)
        .unwrap_or_else(|error| panic!("invalid {}: {error}", programs.display()));
    let generated = out_dir.join("programs_activity.rs");
    fs::write(&generated, programs_activity_body(&activity))
        .unwrap_or_else(|error| panic!("failed to write {}: {error}", generated.display()));
    let module_abi = programs_module_abi(&programs_header)
        .unwrap_or_else(|error| panic!("invalid {}: {error}", programs.display()));
    let generated = out_dir.join("programs_module_abi.rs");
    fs::write(&generated, programs_module_abi_body(&module_abi))
        .unwrap_or_else(|error| panic!("failed to write {}: {error}", generated.display()));
}
