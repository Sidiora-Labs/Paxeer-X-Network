use std::env;
use std::fs;
use std::path::{Path, PathBuf};

#[derive(Debug, Eq, PartialEq)]
struct Code {
    name: String,
    value: i32,
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

fn header_codes(source: &str) -> Result<Vec<Code>, String> {
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
        codes.push(Code {
            name: rust_name(name.trim())?,
            value,
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

fn parity(header: &[Code], mirror: &[Code]) -> Result<(), String> {
    for (index, expected) in header.iter().enumerate() {
        match mirror.get(index) {
            Some(actual) if actual == expected => {}
            Some(actual) => {
                return Err(format!(
                    "result code {index} drifted: header declares {} = {}, src/result.rs declares {} = {}",
                    expected.name, expected.value, actual.name, actual.value
                ));
            }
            None => {
                return Err(format!(
                    "src/result.rs is missing {} = {}",
                    expected.name, expected.value
                ));
            }
        }
    }
    if mirror.len() > header.len() {
        let extra = &mirror[header.len()];
        return Err(format!(
            "src/result.rs declares {} = {} which the header does not",
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
    let header = crate_dir.join("../../../include/layerx/lxp_result.h");
    let mirror = crate_dir.join("src/result.rs");
    println!("cargo:rerun-if-changed={}", header.display());
    println!("cargo:rerun-if-changed={}", mirror.display());
    let declared = header_codes(&read_file(&header))
        .unwrap_or_else(|error| panic!("invalid {}: {error}", header.display()));
    let mirrored = mirror_codes(&read_file(&mirror))
        .unwrap_or_else(|error| panic!("invalid {}: {error}", mirror.display()));
    parity(&declared, &mirrored).unwrap_or_else(|error| {
        panic!("protocol result-code parity failure: {error}");
    });
    let programs = crate_dir.join("../../../include/layerx/programs.h");
    println!("cargo:rerun-if-changed={}", programs.display());
    let maximum = guest_abi_maximum(&read_file(&programs))
        .unwrap_or_else(|error| panic!("invalid {}: {error}", programs.display()));
    let generated =
        PathBuf::from(env::var_os("OUT_DIR").unwrap_or_else(|| panic!("OUT_DIR is unavailable")))
            .join("guest_abi.rs");
    let body = format!(
        "// Generated from include/layerx/programs.h by build.rs. Do not edit.\n\n\
/// The highest guest ABI version the kernel's Programs module admits.\n\
pub const MAX_VERSION: u16 = {maximum};\n"
    );
    fs::write(&generated, body)
        .unwrap_or_else(|error| panic!("failed to write {}: {error}", generated.display()));
}
