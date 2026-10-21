use std::fs;
use std::path::{Path, PathBuf};
use std::sync::atomic::{AtomicUsize, Ordering};

use layerx_human_schema_check::{baseline_manifest, human_api_schema, parse_json, SchemaReport};

static NEXT: AtomicUsize = AtomicUsize::new(0);

fn real_schema_root() -> PathBuf {
    Path::new(env!("CARGO_MANIFEST_DIR")).join("../../schema/human-api")
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
            std::env::temp_dir().join(format!("layerx-human-schema-{}-{id}", std::process::id()));
        let _ = fs::remove_dir_all(&path);
        copy_tree(&real_schema_root(), &path);
        Self(path)
    }

    fn path(&self) -> &Path {
        &self.0
    }

    fn rewrite(&self, relative: &str, from: &str, to: &str) {
        let path = self.0.join(relative);
        let body =
            fs::read_to_string(&path).unwrap_or_else(|error| panic!("read {relative}: {error}"));
        assert!(
            body.contains(from),
            "rewrite target missing in {relative}: {from}"
        );
        fs::write(&path, body.replace(from, to))
            .unwrap_or_else(|error| panic!("write {relative}: {error}"));
    }

    fn write(&self, relative: &str, body: &str) {
        fs::write(self.0.join(relative), body)
            .unwrap_or_else(|error| panic!("write {relative}: {error}"));
    }

    fn append(&self, relative: &str, extra: &str) {
        let path = self.0.join(relative);
        let mut body =
            fs::read_to_string(&path).unwrap_or_else(|error| panic!("read {relative}: {error}"));
        body.push_str(extra);
        fs::write(&path, body).unwrap_or_else(|error| panic!("write {relative}: {error}"));
    }

    fn remove(&self, relative: &str) {
        fs::remove_file(self.0.join(relative))
            .unwrap_or_else(|error| panic!("remove {relative}: {error}"));
    }

    fn read(&self, relative: &str) -> String {
        fs::read_to_string(self.0.join(relative))
            .unwrap_or_else(|error| panic!("read {relative}: {error}"))
    }
}

impl Drop for Fixture {
    fn drop(&mut self) {
        let _ = fs::remove_dir_all(&self.0);
    }
}

fn passing_report(root: &Path) -> SchemaReport {
    human_api_schema(root).unwrap_or_else(|violations| panic!("expected pass: {violations:#?}"))
}

fn violation_rules(root: &Path) -> Vec<&'static str> {
    match human_api_schema(root) {
        Ok(outcome) => panic!("expected violations, got {outcome:?}"),
        Err(violations) => violations.into_iter().map(|entry| entry.rule).collect(),
    }
}

fn add_move_recommit(fixture: &Fixture, request_vector: &str) {
    fixture.append(
        "journeys.kvx",
        "\n[type.MoveRecommitRequest]\nrequired = [\"to:string\",\"money:Money\"]\n\n[operation.move.recommit]\nmethod = \"POST\"\npath = \"/v1/moves/recommit\"\nrequest = \"MoveRecommitRequest\"\nresponse = \"Journey\"\nidempotency = true\n",
    );
    fixture.write("golden/move.recommit.request.json", request_vector);
    let journey = fixture.read("golden/journey.get.response.json");
    fixture.write("golden/move.recommit.response.json", &journey);
    let refusal = fixture.read("golden/journey.get.failure.json");
    fixture.write("golden/move.recommit.failure.json", &refusal);
}

const MOVE_RECOMMIT_REQUEST: &str = r#"{
  "method": "POST",
  "path": "/v1/moves/recommit",
  "headers": {
    "Idempotency-Key": "b1946ac92492d2347c6235b4d2611184"
  },
  "body": {
    "to": "acct-demo",
    "money": {
      "amount": "250000",
      "currency": "LXP"
    }
  }
}
"#;

#[test]
fn real_schema_passes_with_fresh_baseline() {
    let outcome = passing_report(&real_schema_root());
    assert!(outcome.operations >= 3);
    assert!(outcome.golden_vectors >= 6);
    assert_eq!(outcome.additions_beyond_baseline, 0);
}

#[test]
fn committed_baseline_matches_regeneration() {
    let manifest = baseline_manifest(&real_schema_root())
        .unwrap_or_else(|violations| panic!("baseline manifest: {violations:#?}"));
    let committed = fs::read_to_string(real_schema_root().join("baseline.kvx"))
        .unwrap_or_else(|error| panic!("read committed baseline: {error}"));
    assert_eq!(committed, manifest);
}

#[test]
fn missing_schema_version_is_rejected() {
    let fixture = Fixture::from_real_schema();
    fixture.rewrite("v1.kvx", "major = 1\n", "");
    assert!(violation_rules(fixture.path()).contains(&"missing-schema-version"));
}

#[test]
fn missing_include_is_rejected() {
    let fixture = Fixture::from_real_schema();
    fixture.rewrite("v1.kvx", "includes = [", "includes = [\"ghost.kvx\",");
    assert!(violation_rules(fixture.path()).contains(&"missing-include"));
}

#[test]
fn operation_without_response_shape_is_rejected() {
    let fixture = Fixture::from_real_schema();
    fixture.rewrite("journeys.kvx", "response = \"JourneyPage\"\n", "");
    assert!(violation_rules(fixture.path()).contains(&"invalid-operation-declaration"));
}

#[test]
fn missing_golden_vector_is_rejected() {
    let fixture = Fixture::from_real_schema();
    fixture.remove("golden/journey.list.request.json");
    assert!(violation_rules(fixture.path()).contains(&"missing-golden-vector"));
}

#[test]
fn invalid_golden_json_is_rejected() {
    let fixture = Fixture::from_real_schema();
    fixture.write("golden/version.response.json", "{");
    assert!(violation_rules(fixture.path()).contains(&"invalid-golden-json"));
}

#[test]
fn undeclared_enum_value_in_golden_vector_is_rejected() {
    let fixture = Fixture::from_real_schema();
    fixture.rewrite(
        "golden/journey.get.response.json",
        "\"state\": \"processing\"",
        "\"state\": \"totally-fine\"",
    );
    assert!(violation_rules(fixture.path()).contains(&"undeclared-enum-value"));
}

#[test]
fn scalar_encoded_as_json_number_is_rejected() {
    let fixture = Fixture::from_real_schema();
    fixture.rewrite(
        "golden/journey.get.response.json",
        "\"started_at\": \"2026-08-18T09:30:00Z\"",
        "\"started_at\": 1755509400",
    );
    assert!(violation_rules(fixture.path()).contains(&"invalid-scalar-value"));
}

#[test]
fn missing_required_field_in_golden_vector_is_rejected() {
    let fixture = Fixture::from_real_schema();
    fixture.rewrite(
        "golden/journey.get.response.json",
        "      \"kind\": \"deposit\",\n",
        "",
    );
    assert!(violation_rules(fixture.path()).contains(&"missing-required-field"));
}

#[test]
fn orphan_golden_vector_is_rejected() {
    let fixture = Fixture::from_real_schema();
    fixture.write("golden/ghost.request.json", "{\"method\": \"GET\"}");
    assert!(violation_rules(fixture.path()).contains(&"orphan-golden-vector"));
}

#[test]
fn missing_failure_vector_is_rejected() {
    let fixture = Fixture::from_real_schema();
    fixture.remove("golden/journey.get.failure.json");
    assert!(violation_rules(fixture.path()).contains(&"missing-failure-vector"));
}

#[test]
fn failure_envelope_claiming_success_is_rejected() {
    let fixture = Fixture::from_real_schema();
    fixture.rewrite(
        "golden/journey.get.failure.json",
        "\"ok\": false",
        "\"ok\": true",
    );
    assert!(violation_rules(fixture.path()).contains(&"invalid-failure-envelope"));
}

#[test]
fn failure_vector_with_success_status_is_rejected() {
    let fixture = Fixture::from_real_schema();
    fixture.rewrite(
        "golden/journey.get.failure.json",
        "\"status\": 404",
        "\"status\": 200",
    );
    assert!(violation_rules(fixture.path()).contains(&"invalid-failure-status"));
}

#[test]
fn undeclared_machine_code_is_rejected() {
    let fixture = Fixture::from_real_schema();
    fixture.rewrite(
        "golden/journey.get.failure.json",
        "\"code\": \"not-found\"",
        "\"code\": \"gone-wrong\"",
    );
    assert!(violation_rules(fixture.path()).contains(&"undeclared-enum-value"));
}

#[test]
fn rate_limit_failure_without_timing_shape_is_rejected() {
    let fixture = Fixture::from_real_schema();
    fixture.rewrite(
        "golden/version.failure.json",
        "\"retry_after_ms\": 5000",
        "\"retry_after_ms\": \"soon\"",
    );
    assert!(violation_rules(fixture.path()).contains(&"shape-mismatch"));
}

#[test]
fn contract_without_the_error_model_is_rejected() {
    let fixture = Fixture::from_real_schema();
    fixture.rewrite("errors.kvx", "[type.ApiError]", "[type.ApiFailure]");
    assert!(violation_rules(fixture.path()).contains(&"missing-error-model"));
}

#[test]
fn stream_module_without_its_spine_is_rejected() {
    let fixture = Fixture::from_real_schema();
    fixture.rewrite(
        "stream.kvx",
        "[operation.stream.next]",
        "[operation.stream.later]",
    );
    let rules = violation_rules(fixture.path());
    assert!(rules.contains(&"missing-stream-module"));
}

#[test]
fn identity_module_without_its_spine_is_rejected() {
    let fixture = Fixture::from_real_schema();
    fixture.rewrite(
        "identity.kvx",
        "[operation.account.create]",
        "[operation.account.begin]",
    );
    let rules = violation_rules(fixture.path());
    assert!(rules.contains(&"missing-identity-module"));
}

#[test]
fn step_up_challenge_without_operation_binding_is_rejected() {
    let fixture = Fixture::from_real_schema();
    fixture.rewrite(
        "identity.kvx",
        "confirms:OperationDigest",
        "signals:OperationDigest",
    );
    let rules = violation_rules(fixture.path());
    assert!(rules.contains(&"missing-identity-module"));
}

#[test]
fn agent_module_without_its_operations_is_rejected() {
    let fixture = Fixture::from_real_schema();
    fixture.rewrite(
        "agents.kvx",
        "[operation.approval.approve]",
        "[operation.approval.decide]",
    );
    assert!(violation_rules(fixture.path()).contains(&"missing-agent-module"));
}

#[test]
fn agent_module_without_the_facts_spine_is_rejected() {
    let fixture = Fixture::from_real_schema();
    fixture.rewrite("agents.kvx", "[type.ApprovalFacts]", "[type.ApprovalStory]");
    assert!(violation_rules(fixture.path()).contains(&"missing-agent-module"));
}

#[test]
fn activity_module_without_its_spine_is_rejected() {
    let fixture = Fixture::from_real_schema();
    fixture.rewrite(
        "activity.kvx",
        "[operation.activity.query]",
        "[operation.activity.scan]",
    );
    assert!(violation_rules(fixture.path()).contains(&"missing-activity-module"));
}

#[test]
fn removed_declaration_breaks_the_baseline() {
    let fixture = Fixture::from_real_schema();
    fixture.rewrite(
        "journeys.kvx",
        "export = \"Every evidence reference is sufficient to fetch the receipt or proof material for export and independent verification.\"\n",
        "",
    );
    assert!(violation_rules(fixture.path()).contains(&"breaking-declaration-removal"));
}

#[test]
fn changed_declaration_breaks_the_baseline() {
    let fixture = Fixture::from_real_schema();
    fixture.rewrite(
        "compatibility.kvx",
        "service = \"0.1.x\"",
        "service = \"0.2.x\"",
    );
    assert!(violation_rules(fixture.path()).contains(&"breaking-declaration-change"));
}

#[test]
fn added_declaration_passes_beyond_the_baseline() {
    let fixture = Fixture::from_real_schema();
    fixture.append("journeys.kvx", "\n[type.JourneyNote]\nrequired = []\n");
    let outcome = passing_report(fixture.path());
    assert_eq!(outcome.additions_beyond_baseline, 1);
}

#[test]
fn appended_enum_variant_is_additive() {
    let fixture = Fixture::from_real_schema();
    fixture.rewrite(
        "journeys.kvx",
        "\"agent-retire\"]",
        "\"agent-retire\",\"agent-rename\"]",
    );
    let outcome = passing_report(fixture.path());
    assert_eq!(outcome.additions_beyond_baseline, 0);
}

#[test]
fn removed_enum_variant_breaks_the_baseline() {
    let fixture = Fixture::from_real_schema();
    fixture.rewrite("journeys.kvx", "\"exit\",", "");
    assert!(violation_rules(fixture.path()).contains(&"breaking-declaration-change"));
}

#[test]
fn malformed_compatibility_row_is_rejected() {
    let fixture = Fixture::from_real_schema();
    fixture.rewrite("compatibility.kvx", "web_app = \"0.1.x\"\n", "");
    assert!(violation_rules(fixture.path()).contains(&"invalid-compatibility-row"));
}

#[test]
fn missing_baseline_instructs_a_deliberate_refresh() {
    let fixture = Fixture::from_real_schema();
    fixture.remove("baseline.kvx");
    assert!(violation_rules(fixture.path()).contains(&"missing-baseline"));
}

#[test]
fn idempotent_mutation_with_header_and_decimal_amount_passes() {
    let fixture = Fixture::from_real_schema();
    let base = passing_report(fixture.path());
    add_move_recommit(&fixture, MOVE_RECOMMIT_REQUEST);
    let outcome = passing_report(fixture.path());
    assert!(outcome.additions_beyond_baseline >= 1);
    assert_eq!(outcome.operations, base.operations + 1);
}

#[test]
fn idempotent_mutation_without_the_header_is_rejected() {
    let fixture = Fixture::from_real_schema();
    let vector = MOVE_RECOMMIT_REQUEST.replace(
        "  \"headers\": {\n    \"Idempotency-Key\": \"b1946ac92492d2347c6235b4d2611184\"\n  },\n",
        "",
    );
    add_move_recommit(&fixture, &vector);
    assert!(violation_rules(fixture.path()).contains(&"missing-idempotency-header"));
}

#[test]
fn amount_encoded_as_json_number_is_rejected() {
    let fixture = Fixture::from_real_schema();
    let vector = MOVE_RECOMMIT_REQUEST.replace("\"amount\": \"250000\"", "\"amount\": 250000");
    add_move_recommit(&fixture, &vector);
    assert!(violation_rules(fixture.path()).contains(&"invalid-scalar-value"));
}

#[test]
fn movement_module_without_its_operations_is_rejected() {
    let fixture = Fixture::from_real_schema();
    fixture.rewrite(
        "movement.kvx",
        "[operation.move.quote]",
        "[operation.move.preview]",
    );
    assert!(violation_rules(fixture.path()).contains(&"missing-movement-module"));
}

#[test]
fn refusal_without_the_money_left_statement_is_rejected() {
    let fixture = Fixture::from_real_schema();
    fixture.rewrite(
        "movement.kvx",
        "\"money_left:boolean\"",
        "\"money_gone:boolean\"",
    );
    assert!(violation_rules(fixture.path()).contains(&"missing-movement-module"));
}

#[test]
fn quote_without_a_separate_fee_ceiling_is_rejected() {
    let fixture = Fixture::from_real_schema();
    fixture.rewrite("movement.kvx", "\"fee_ceiling:Money\"", "\"fee:Money\"");
    assert!(violation_rules(fixture.path()).contains(&"missing-movement-module"));
}

#[test]
fn duplicate_json_object_keys_are_rejected() {
    let parsed = parse_json("{\"a\": 1, \"a\": 2}");
    assert!(parsed.is_err());
}

#[test]
fn nested_json_round_trips_through_the_parser() {
    let parsed = parse_json("{\"list\": [true, null, \"x\\u00e9\", {\"n\": -12}]}")
        .unwrap_or_else(|error| panic!("parse: {error}"));
    assert!(parsed.field("list").is_some());
}
