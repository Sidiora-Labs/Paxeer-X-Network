#!/usr/bin/env bash
# One runner for every release test: each suite once, then the operator-input and receipt checks.
set -uo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)
work=$(mktemp -d)
trap 'rm -rf -- "${work:?}"' EXIT
command -v go >/dev/null || PATH=$PATH:/usr/local/go/bin

runbook_path=spec/release-v6-12-0/docs/rollout-runbook.md
rollout_template=tools/release/env/rollout.env.example
receipt_dir=tools/release/rollout/v6.12.0
upgradeinfo_suite="go test ./tools/release/upgradeinfo/"
gov_suite=tools/paxeer-x/gov/test.sh
receipt_keys=(version plan nodes_checked nodes_passed precompiles_served pools markets oracle_fresh feed_keys
	homes_archived hosts_staged units_active services_ready readiness rollback_executed rollback_rehearsed
	new_bin_sha256 inputs_sha256 inputs_approved tag release_sha256 result)
window_receipts=(halt-height preflight stage automation frozen snapshot swap-full validators oracle services full
	rollback post-rollout)
# The suites the spec names; a named suite absent from the tree is a failure, not a skip.
expected_suites=(build.test.sh env_templates.test.sh identity.test.sh rehearse.test.sh reproducible.test.sh
	rollback.test.sh snapshot.test.sh stage.test.sh swap.test.sh verify-release.test.sh workflows.test.sh
	test_feed_key.py test_fleet_check.py test_programme.py rehearsal/test_rewrite_genesis.py
	"$gov_suite" "$upgradeinfo_suite")

# discover <base>: every suite present under <base>, one name per line.
discover() {
	local base=$1 f
	for f in "$base"/tools/release/tests/*.test.sh "$base"/tools/release/tests/test_*.py; do
		[ -f "$f" ] && echo "${f##*/}"
	done
	for f in "$base"/tools/release/rehearsal/test_*.py; do
		[ -f "$f" ] && echo "rehearsal/${f##*/}"
	done
	[ -f "$base/$gov_suite" ] && echo "$gov_suite"
	compgen -G "$base/tools/release/upgradeinfo/*_test.go" >/dev/null && echo "$upgradeinfo_suite"
	return 0
}

# run_suite <base> <name>: runs one suite from <base>.
run_suite() {
	local base=$1 name=$2
	case $name in
	"$upgradeinfo_suite") (cd "$base" && go test ./tools/release/upgradeinfo/) ;;
	"$gov_suite") (cd "$base" && bash "$gov_suite") ;;
	rehearsal/*.py) (cd "$base" && python3 "tools/release/$name") ;;
	*.py) (cd "$base" && python3 "tools/release/tests/$name") ;;
	*) (cd "$base" && bash "tools/release/tests/$name") ;;
	esac
}

# all_suites_once <base>: runs every discovered suite once; prints FAIL <name> per failing suite.
all_suites_once() {
	local base=$1 name rc=0
	while IFS= read -r name; do
		echo "== $name"
		if ! run_suite "$base" "$name" </dev/null; then
			echo "FAIL $name"
			rc=1
		fi
	done < <(discover "$base")
	return "$rc"
}

# expected_suites_present <base>: prints FAIL <name> (missing) per named suite absent from <base>.
expected_suites_present() {
	local base=$1 found name rc=0
	found=$(discover "$base")
	for name in "${expected_suites[@]}"; do
		grep -qxF "$name" <<<"$found" && continue
		echo "FAIL $name (missing)"
		rc=1
	done
	return "$rc"
}

# receipts <base>: every receipt file of the window directory.
receipts() {
	compgen -G "$1/$receipt_dir/*.json" || true
}

# runbook_inputs_are_blank <base>: the operator input table keeps every value cell empty, the Halt height H
# row is present, every rollout template key is empty, and no receipt carries halt_height.
runbook_inputs_are_blank() {
	local base=$1 rows line value rc=0 file
	[ -r "$base/$runbook_path" ] || { echo "runbook_inputs_are_blank: $runbook_path is missing" >&2; return 1; }
	rows=$(awk '/^## /{on = ($0 == "## Operator inputs"); next} on && /^\|/' "$base/$runbook_path" | tail -n +3)
	grep -q '^| Halt height H |' <<<"$rows" || { echo "runbook_inputs_are_blank: no Halt height H row" >&2; rc=1; }
	while IFS= read -r line; do
		[ -n "$line" ] || continue
		value=$(awk -F'|' '{print $3}' <<<"$line" | tr -d '[:space:]')
		[ -z "$value" ] || { echo "runbook_inputs_are_blank: filled row: $line" >&2; rc=1; }
	done <<<"$rows"
	[ -r "$base/$rollout_template" ] || { echo "runbook_inputs_are_blank: $rollout_template is missing" >&2; return 1; }
	while IFS= read -r line; do
		case $line in '' | '#'*) continue ;; esac
		[[ $line =~ ^[A-Z_][A-Z0-9_]*=$ ]] || { echo "runbook_inputs_are_blank: $rollout_template: $line" >&2; rc=1; }
	done <"$base/$rollout_template"
	for file in $(receipts "$base"); do
		if jq -e 'type == "object" and has("halt_height")' "$file" >/dev/null 2>&1; then
			echo "runbook_inputs_are_blank: ${file#"$base"/} carries halt_height" >&2
			rc=1
		fi
	done
	return "$rc"
}

# receipts_are_public_safe <base>: every receipt is a JSON object of scalars with result pass and keys of
# the receipt key set only.
receipts_are_public_safe() {
	local base=$1 file allowed bad rc=0
	allowed=$(printf '%s\n' "${receipt_keys[@]}" | jq -R . | jq -sc .)
	for file in $(receipts "$base"); do
		if ! jq -e 'type == "object"' "$file" >/dev/null 2>&1; then
			echo "receipts_are_public_safe: ${file#"$base"/} is not a JSON object" >&2
			rc=1
			continue
		fi
		bad=$(jq -r --argjson k "$allowed" 'keys - $k | join(",")' "$file")
		[ -z "$bad" ] || { echo "receipts_are_public_safe: ${file#"$base"/} carries $bad" >&2; rc=1; }
		jq -e 'all(.[]; type != "object" and type != "array")' "$file" >/dev/null ||
			{ echo "receipts_are_public_safe: ${file#"$base"/} carries a nested value" >&2; rc=1; }
		jq -e '.result == "pass"' "$file" >/dev/null ||
			{ echo "receipts_are_public_safe: ${file#"$base"/} result is not pass" >&2; rc=1; }
	done
	return "$rc"
}

# receipts_pass <base>: the thirteen window receipts exist and report pass; skips only when the window
# directory holds no receipt.
receipts_pass() {
	local base=$1 name file rc=0
	if [ -z "$(receipts "$base")" ]; then
		echo "receipts_pass: skipped, no receipt under $receipt_dir"
		return 0
	fi
	for name in "${window_receipts[@]}"; do
		file=$base/$receipt_dir/$name.json
		if [ ! -f "$file" ]; then
			echo "receipts_pass: $name.json is missing" >&2
			rc=1
		elif ! jq -e '.result == "pass"' "$file" >/dev/null 2>&1; then
			echo "receipts_pass: $name.json does not report pass" >&2
			rc=1
		fi
	done
	return "$rc"
}

# The checks themselves, on a scratch tree.
self_check() {
	local t=$work/tree out=$work/out name
	mkdir -p "$t/tools/release/tests"
	printf 'exit 0\n' >"$t/tools/release/tests/ok.test.sh"
	printf 'exit 3\n' >"$t/tools/release/tests/stage.test.sh"
	all_suites_once "$t" >"$out" 2>&1 && { echo "self_check: a failing stage.test.sh passed" >&2; return 1; }
	grep -qxF 'FAIL stage.test.sh' "$out" || { echo "self_check: no FAIL stage.test.sh line" >&2; return 1; }
	grep -q '^FAIL ok.test.sh' "$out" && { echo "self_check: a passing suite was reported" >&2; return 1; }
	printf 'exit 0\n' >"$t/tools/release/tests/stage.test.sh"
	all_suites_once "$t" >"$out" 2>&1 || { echo "self_check: passing suites failed" >&2; return 1; }
	expected_suites_present "$t" >"$out" && { echo "self_check: missing suites were accepted" >&2; return 1; }
	grep -qxF 'FAIL swap.test.sh (missing)' "$out" || { echo "self_check: missing swap.test.sh not reported" >&2; return 1; }

	mkdir -p "$t/$(dirname "$runbook_path")" "$t/tools/release/env" "$t/$receipt_dir"
	cp "$root/$runbook_path" "$t/$runbook_path"
	cp "$root/$rollout_template" "$t/$rollout_template"
	runbook_inputs_are_blank "$t" 2>"$out" || { echo "self_check: the blank runbook was refused: $(cat "$out")" >&2; return 1; }
	receipts_pass "$t" >"$out" || { echo "self_check: an empty receipt directory was refused" >&2; return 1; }
	grep -q skipped "$out" || { echo "self_check: an empty receipt directory was not skipped" >&2; return 1; }
	sed -i 's/^| Halt height H | |$/| Halt height H | 1 |/' "$t/$runbook_path"
	runbook_inputs_are_blank "$t" 2>/dev/null && { echo "self_check: a filled halt height row was accepted" >&2; return 1; }
	cp "$root/$runbook_path" "$t/$runbook_path"
	sed -i 's/^HALT_HEIGHT=$/HALT_HEIGHT=1/' "$t/$rollout_template"
	runbook_inputs_are_blank "$t" 2>/dev/null && { echo "self_check: a filled template key was accepted" >&2; return 1; }
	cp "$root/$rollout_template" "$t/$rollout_template"

	for name in "${window_receipts[@]}"; do
		printf '{"nodes_checked":4,"nodes_passed":4,"result":"pass"}\n' >"$t/$receipt_dir/$name.json"
	done
	receipts_are_public_safe "$t" 2>"$out" || { echo "self_check: clean receipts were refused: $(cat "$out")" >&2; return 1; }
	receipts_pass "$t" 2>"$out" || { echo "self_check: thirteen passing receipts were refused" >&2; return 1; }
	printf '{"halt_height":1,"result":"pass"}\n' >"$t/$receipt_dir/halt-height.json"
	receipts_are_public_safe "$t" 2>"$out" && { echo "self_check: a halt_height key was accepted" >&2; return 1; }
	grep -q halt_height "$out" || { echo "self_check: the refusal does not name halt_height" >&2; return 1; }
	runbook_inputs_are_blank "$t" 2>/dev/null && { echo "self_check: a receipt halt_height was accepted" >&2; return 1; }
	printf '{"result":"pass","nodes_checked":{"host":1}}\n' >"$t/$receipt_dir/halt-height.json"
	receipts_are_public_safe "$t" 2>/dev/null && { echo "self_check: a nested value was accepted" >&2; return 1; }
	printf '{"result":"fail"}\n' >"$t/$receipt_dir/oracle.json"
	receipts_pass "$t" 2>"$out" && { echo "self_check: a failing receipt was accepted" >&2; return 1; }
	grep -q 'oracle.json does not report pass' "$out" || { echo "self_check: the failing receipt is not named" >&2; return 1; }
	mv "$t/$receipt_dir/full.json" "$work/full.json"
	receipts_pass "$t" 2>"$out" && { echo "self_check: a missing receipt was accepted" >&2; return 1; }
	grep -q 'full.json is missing' "$out" || { echo "self_check: the missing receipt is not named" >&2; return 1; }
	return 0
}

failed=0
check() {
	local name=$1
	shift
	echo "== $name"
	"$@" || {
		echo "FAIL $name"
		failed=1
	}
}

check self_check self_check
all_suites_once "$root" || failed=1
expected_suites_present "$root" || failed=1
check runbook_inputs_are_blank runbook_inputs_are_blank "$root"
check receipts_are_public_safe receipts_are_public_safe "$root"
check receipts_pass receipts_pass "$root"

if [ "$failed" -ne 0 ]; then
	echo "release tests: failed" >&2
	exit 1
fi
echo "release tests: all passed"
