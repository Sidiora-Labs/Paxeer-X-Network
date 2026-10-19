use std::fs::{self, OpenOptions};
use std::io::Write as _;
use std::path::{Path, PathBuf};

use serde_json::json;

use crate::config::ensure_directory_empty;

pub fn create(name: &str, parent: &Path) -> Result<serde_json::Value, String> {
    validate_name(name)?;
    let project = parent.join(name);
    ensure_directory_empty(&project)?;
    fs::create_dir_all(project.join("src"))
        .map_err(|error| format!("could not create {}: {error}", project.display()))?;
    let crate_name = name.replace('-', "_");
    write_new(
        &project.join("Cargo.toml"),
        &format!(
            "[package]\nname = \"{name}\"\nversion = \"0.1.0\"\nedition = \"2021\"\n\n[lib]\ncrate-type = [\"cdylib\"]\n\n[dependencies]\nlayerx-program-sdk = {{ path = \"{sdk}\" }}\n\n[profile.release]\nopt-level = \"z\"\nlto = true\ncodegen-units = 1\npanic = \"abort\"\nstrip = true\n",
            sdk = sdk_path()
        ),
    )?;
    write_new(
        &project.join("src/lib.rs"),
        "#![no_std]\n\nuse layerx_program_sdk::{program, trap_on_panic, ProgramError};\n\ntrap_on_panic!();\n\nprogram!(handle);\n\nfn handle(value: i64) -> Result<i64, ProgramError> {\n    Ok(value)\n}\n",
    )?;
    write_new(
        &project.join("LayerX.toml"),
        "schema = 1\nkind = \"program\"\nabi = 1\nenvironment = \"emulator\"\n",
    )?;
    write_new(&project.join(".gitignore"), "/target\n")?;
    Ok(json!({
        "path": absolute(&project)?.display().to_string(),
        "package": name,
        "crate": crate_name,
        "kind": "rust-program",
        "files": ["Cargo.toml", "LayerX.toml", "src/lib.rs", ".gitignore"],
    }))
}

fn write_new(path: &Path, contents: &str) -> Result<(), String> {
    let mut file = OpenOptions::new()
        .write(true)
        .create_new(true)
        .open(path)
        .map_err(|error| format!("could not create {}: {error}", path.display()))?;
    file.write_all(contents.as_bytes())
        .and_then(|()| file.sync_all())
        .map_err(|error| format!("could not write {}: {error}", path.display()))
}

fn validate_name(name: &str) -> Result<(), String> {
    if name.is_empty()
        || name.len() > 64
        || name.starts_with('-')
        || !name
            .bytes()
            .all(|byte| byte.is_ascii_lowercase() || byte.is_ascii_digit() || byte == b'-')
    {
        return Err("project name must be a lowercase Cargo package name".into());
    }
    Ok(())
}

const SDK_DEFAULT_PATH: &str = concat!(env!("CARGO_MANIFEST_DIR"), "/../../../programs/sdk/rust");

fn sdk_path() -> String {
    std::env::var("LAYERX_PROGRAM_SDK").unwrap_or_else(|_| SDK_DEFAULT_PATH.to_string())
}

fn absolute(path: &Path) -> Result<PathBuf, String> {
    path.canonicalize()
        .map_err(|error| format!("could not resolve {}: {error}", path.display()))
}
