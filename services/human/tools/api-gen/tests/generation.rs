use std::collections::BTreeSet;
use std::fs;
use std::path::{Path, PathBuf};
use std::sync::atomic::{AtomicUsize, Ordering};

use layerx_human_api_gen::{
    check_client, generate_client, write_client, GeneratedClient, GENERATED_FILES,
};

static NEXT: AtomicUsize = AtomicUsize::new(0);

fn real_schema_root() -> PathBuf {
    Path::new(env!("CARGO_MANIFEST_DIR")).join("../../schema/human-api")
}

fn committed_output_root() -> PathBuf {
    Path::new(env!("CARGO_MANIFEST_DIR")).join("../../../../apps/web/src/api/generated")
}

fn copy_tree(from: &Path, to: &Path) {
    fs::create_dir_all(to).unwrap_or_else(|error| panic!("create {}: {error}", to.display()));
    let entries =
        fs::read_dir(from).unwrap_or_else(|error| panic!("read {}: {error}", from.display()));
    for entry in entries {
        let entry = entry.unwrap_or_else(|error| panic!("read entry: {error}"));
        let source = entry.path();
        let target = to.join(entry.file_name());
        if source.is_dir() {
            copy_tree(&source, &target);
        } else {
            fs::copy(&source, &target)
                .unwrap_or_else(|error| panic!("copy {}: {error}", source.display()));
        }
    }
}

struct Fixture(PathBuf);

impl Fixture {
    fn from_real_schema() -> Self {
        let id = NEXT.fetch_add(1, Ordering::Relaxed);
        let path =
            std::env::temp_dir().join(format!("layerx-human-api-gen-{}-{id}", std::process::id()));
        let _ = fs::remove_dir_all(&path);
        copy_tree(&real_schema_root(), &path.join("schema"));
        Self(path)
    }

    fn schema(&self) -> PathBuf {
        self.0.join("schema")
    }

    fn out(&self) -> PathBuf {
        self.0.join("out")
    }

    fn rewrite(&self, relative: &str, from: &str, to: &str) {
        let path = self.schema().join(relative);
        let body =
            fs::read_to_string(&path).unwrap_or_else(|error| panic!("read {relative}: {error}"));
        assert!(
            body.contains(from),
            "rewrite target missing in {relative}: {from}"
        );
        fs::write(&path, body.replace(from, to))
            .unwrap_or_else(|error| panic!("write {relative}: {error}"));
    }

    fn append(&self, relative: &str, extra: &str) {
        let path = self.schema().join(relative);
        let mut body =
            fs::read_to_string(&path).unwrap_or_else(|error| panic!("read {relative}: {error}"));
        body.push_str(extra);
        fs::write(&path, body).unwrap_or_else(|error| panic!("write {relative}: {error}"));
    }

    fn append_out(&self, name: &str, extra: &str) {
        let path = self.out().join(name);
        let mut body =
            fs::read_to_string(&path).unwrap_or_else(|error| panic!("read {name}: {error}"));
        body.push_str(extra);
        fs::write(&path, body).unwrap_or_else(|error| panic!("write {name}: {error}"));
    }

    fn write_fresh_output(&self) -> GeneratedClient {
        write_client(&self.schema(), &self.out())
            .unwrap_or_else(|violations| panic!("expected generation: {violations:#?}"))
    }
}

impl Drop for Fixture {
    fn drop(&mut self) {
        let _ = fs::remove_dir_all(&self.0);
    }
}

fn generated(root: &Path) -> GeneratedClient {
    generate_client(root)
        .unwrap_or_else(|violations| panic!("expected generation: {violations:#?}"))
}

fn check_rules(root: &Path, out_dir: &Path) -> Vec<&'static str> {
    match check_client(root, out_dir) {
        Ok(generated) => panic!(
            "expected drift, got {} fresh file(s)",
            generated.files.len()
        ),
        Err(violations) => violations.into_iter().map(|entry| entry.rule).collect(),
    }
}

fn declared_sections(prefix: &str) -> Vec<String> {
    let mut names = Vec::new();
    let entries = fs::read_dir(real_schema_root())
        .unwrap_or_else(|error| panic!("read schema root: {error}"));
    for entry in entries {
        let path = entry
            .unwrap_or_else(|error| panic!("read entry: {error}"))
            .path();
        if path.extension().is_none_or(|extension| extension != "kvx") {
            continue;
        }
        let body = fs::read_to_string(&path)
            .unwrap_or_else(|error| panic!("read {}: {error}", path.display()));
        for line in body.lines() {
            let section = line
                .trim()
                .strip_prefix('[')
                .and_then(|value| value.strip_suffix(']'));
            if let Some(name) = section.and_then(|value| value.strip_prefix(prefix)) {
                names.push(name.to_owned());
            }
        }
    }
    names
}

fn camel(name: &str) -> String {
    let mut out = String::new();
    for (index, segment) in name.split(['.', '-', '_']).enumerate() {
        if index == 0 {
            out.push_str(segment);
        } else {
            let mut chars = segment.chars();
            if let Some(first) = chars.next() {
                out.extend(first.to_uppercase());
                out.push_str(chars.as_str());
            }
        }
    }
    out
}

fn index_body(client: &GeneratedClient) -> &str {
    client
        .files
        .get("index.ts")
        .unwrap_or_else(|| panic!("index.ts missing from generation"))
}

fn conformance_body(client: &GeneratedClient) -> &str {
    client
        .files
        .get("conformance.ts")
        .unwrap_or_else(|| panic!("conformance.ts missing from generation"))
}

#[test]
fn real_schema_generates_deterministically() {
    let first = generated(&real_schema_root());
    let second = generated(&real_schema_root());
    assert_eq!(first, second);
    let names: Vec<&str> = first.files.keys().map(String::as_str).collect();
    assert_eq!(names, GENERATED_FILES);
}

#[test]
fn generated_client_covers_every_schema_declaration() {
    let client = generated(&real_schema_root());
    let index = index_body(&client);
    let conformance = conformance_body(&client);
    let operations = declared_sections("operation.");
    assert_eq!(client.operations, operations.len());
    for operation in &operations {
        assert!(
            index.contains(&format!("\"{operation}\": {{ method: ")),
            "index.ts must declare the {operation} shape"
        );
        assert!(
            index.contains(&format!("  {}(", camel(operation))),
            "index.ts must declare the {operation} client method"
        );
        assert!(
            conformance.contains(&format!("\"{operation}\": async (run) =>")),
            "conformance.ts must drive the {operation} client method"
        );
    }
    let mut types = declared_sections("type.");
    types.extend(declared_sections("record."));
    let enums: BTreeSet<String> = types
        .iter()
        .filter(|name| index.contains(&format!("Variants: readonly {name}[]")))
        .cloned()
        .collect();
    for name in &types {
        let declared = index.contains(&format!("export interface {name} {{"))
            || index.contains(&format!("export type {name} = "));
        assert!(declared, "index.ts must declare the {name} type");
        assert!(
            index.contains(&format!("export function decode{name}(")),
            "index.ts must decode {name}"
        );
        if !enums.contains(name) {
            assert!(
                index.contains(&format!("export function encode{name}(")),
                "index.ts must encode {name}"
            );
        }
    }
    for name in declared_sections("scalar.") {
        assert!(
            index.contains(&format!("export type {name} = ")),
            "index.ts must alias the {name} scalar"
        );
    }
    for name in [
        "ApiError",
        "ErrorCode",
        "Retriability",
        "StreamEvent",
        "StreamEventKind",
        "Journey",
    ] {
        assert!(
            index.contains(&format!("export function decode{name}(")),
            "index.ts must decode {name}"
        );
    }
    assert!(index.contains("export class HumanApiError extends Error {"));
}

#[test]
fn committed_client_matches_regeneration() {
    let client = check_client(&real_schema_root(), &committed_output_root())
        .unwrap_or_else(|violations| panic!("committed output drifted: {violations:#?}"));
    assert!(client.operations >= 3);
}

#[test]
fn fresh_regeneration_passes_the_gate() {
    let fixture = Fixture::from_real_schema();
    let written = fixture.write_fresh_output();
    let checked = check_client(&fixture.schema(), &fixture.out())
        .unwrap_or_else(|violations| panic!("fresh output drifted: {violations:#?}"));
    assert_eq!(written, checked);
}

#[test]
fn hand_edited_output_fails_the_gate() {
    let fixture = Fixture::from_real_schema();
    fixture.write_fresh_output();
    fixture.append_out("index.ts", "\nexport const handEdited = true;\n");
    assert!(check_rules(&fixture.schema(), &fixture.out()).contains(&"stale-or-hand-edited-output"));
}

#[test]
fn stale_output_after_schema_change_fails_the_gate() {
    let fixture = Fixture::from_real_schema();
    fixture.write_fresh_output();
    fixture.append(
        "journeys.kvx",
        "\n[type.JourneyNote]\nrequired = [\"note:string\"]\n",
    );
    assert!(check_rules(&fixture.schema(), &fixture.out()).contains(&"stale-or-hand-edited-output"));
}

#[test]
fn missing_output_fails_the_gate() {
    let fixture = Fixture::from_real_schema();
    fixture.write_fresh_output();
    fs::remove_file(fixture.out().join("conformance.ts"))
        .unwrap_or_else(|error| panic!("remove conformance.ts: {error}"));
    assert!(check_rules(&fixture.schema(), &fixture.out()).contains(&"missing-generated-output"));
}

#[test]
fn unexpected_output_fails_the_gate() {
    let fixture = Fixture::from_real_schema();
    fixture.write_fresh_output();
    fs::write(
        fixture.out().join("extra.ts"),
        "export const extra = true;\n",
    )
    .unwrap_or_else(|error| panic!("write extra.ts: {error}"));
    assert!(check_rules(&fixture.schema(), &fixture.out()).contains(&"unexpected-generated-output"));
}

fn refusal_rules(root: &Path) -> Vec<&'static str> {
    match generate_client(root) {
        Ok(client) => panic!(
            "expected refusal, generated {} operations",
            client.operations
        ),
        Err(violations) => violations.into_iter().map(|entry| entry.rule).collect(),
    }
}

#[test]
fn unresolved_reference_refuses_generation() {
    let fixture = Fixture::from_real_schema();
    fixture.rewrite("journeys.kvx", "state:JourneyState", "state:GhostState");
    assert!(refusal_rules(&fixture.schema()).contains(&"unresolved-type"));
}

#[test]
fn defective_scalar_mapping_refuses_generation() {
    let fixture = Fixture::from_real_schema();
    fixture.rewrite(
        "v1.kvx",
        "typescript = \"bigint\"",
        "typescript = \"number\"",
    );
    assert!(refusal_rules(&fixture.schema()).contains(&"invalid-scalar-declaration"));
}

#[test]
fn consensus_amounts_map_to_bigint() {
    let client = generated(&real_schema_root());
    let index = index_body(&client);
    assert!(index.contains("export type Amount = bigint;"));
    assert!(index.contains("amount: decodeConsensusInteger(object[\"amount\"], at + \".amount\"),"));
    assert!(index.contains("amount: encodeConsensusInteger(value.amount),"));
}

#[test]
fn idempotent_operations_require_the_key_and_reads_carry_none() {
    let client = generated(&real_schema_root());
    let index = index_body(&client);
    assert!(index.contains(
        "accountCreate(request: AccountCreateRequest, idempotencyKey: string): Promise<AccountCreation>;"
    ));
    assert!(index.contains("journeyGet(journey_id: string): Promise<Journey>;"));
    assert!(index.contains("agentPause(agent_id: string, idempotencyKey: string): Promise<Agent>;"));
}

#[test]
fn operation_paths_encode_their_parameters() {
    let client = generated(&real_schema_root());
    let index = index_body(&client);
    assert!(index.contains("\"/v1/journeys/\" + encodeURIComponent(journey_id)"));
    assert!(index.contains("\"/v1/agents/\" + encodeURIComponent(agent_id) + \"/pause\""));
}
