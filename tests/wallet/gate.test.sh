#!/usr/bin/env bash
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
GATE_TEST="$HERE/../../scripts/wallet/gate-test.sh"
GATE_LINT="$HERE/../../scripts/wallet/gate-lint.sh"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

failures=0

fail() {
  echo "FAIL: $*"
  failures=$((failures + 1))
}

pass() {
  echo "ok: $*"
}

run_capture() {
  local out="$1"
  shift
  set +e
  "$@" >"$out" 2>&1
  RC=$?
  set -e
}

assert_contains() {
  local file="$1" needle="$2" what="$3"
  if grep -qF -- "$needle" "$file"; then
    pass "$what"
  else
    fail "$what: expected to find '$needle' in output:"
    sed 's/^/    /' "$file"
  fi
}

for gate in "$GATE_TEST" "$GATE_LINT"; do
  name="$(basename "$gate")"
  run_capture "$WORK/$name.check.out" "$gate" --check
  if [[ "$RC" -eq 0 ]]; then
    pass "$name --check exits 0"
  else
    fail "$name --check exited $RC"
    sed 's/^/    /' "$WORK/$name.check.out"
  fi
  assert_contains "$WORK/$name.check.out" "syntax: ok" "$name --check validates its syntax"
  assert_contains "$WORK/$name.check.out" "targets:" "$name --check prints the target list"
done

for label in go-attestor go-ceremony pnpm-wallet pnpm-app rust-human rust-platform; do
  if grep -qE "^  $label"$'\t'"(exists|absent)"$'\t' "$WORK/gate-test.sh.check.out"; then
    pass "gate-test.sh --check lists $label with exists or absent"
  else
    fail "gate-test.sh --check does not list $label with exists or absent"
  fi
done

for label in go-attestor-gofmt go-attestor-vet go-ceremony-gofmt go-ceremony-vet pnpm-wallet-eslint pnpm-wallet-tsc pnpm-app-eslint pnpm-app-tsc rust-human-fmt rust-human-clippy rust-platform-fmt rust-platform-clippy; do
  if grep -qE "^  $label"$'\t'"(exists|absent)"$'\t' "$WORK/gate-lint.sh.check.out"; then
    pass "gate-lint.sh --check lists $label with exists or absent"
  else
    fail "gate-lint.sh --check does not list $label with exists or absent"
  fi
done

mkdir -p "$WORK/target"
printf 'slow\t%s\tsleep 5\nbroken\t%s\texit 3\n' "$WORK/target" "$WORK/target" >"$WORK/slow-and-broken.tsv"

LOGS1="$WORK/logs-budget-1"
run_capture "$WORK/budget-1.out" env WALLET_GATE_BUDGET_SECONDS=1 WALLET_GATE_LOG_DIR="$LOGS1" \
  WALLET_GATE_TARGETS_FILE="$WORK/slow-and-broken.tsv" "$GATE_TEST"
if [[ "$RC" -ne 0 ]]; then
  pass "budget of one second exits non-zero ($RC)"
else
  fail "budget of one second exited 0"
fi
assert_contains "$WORK/budget-1.out" "stopped on: slow: sleep 5" "budget stop reports the command it stopped on"
assert_contains "$WORK/budget-1.out" "exit code: 124" "budget stop reports exit code 124"
stop_log="$(sed -n 's/^log: //p' "$WORK/budget-1.out" | head -n 1)"
if [[ -n "$stop_log" && -f "$stop_log" ]]; then
  pass "budget stop names a log path that exists ($stop_log)"
else
  fail "budget stop log path missing or not a file: '$stop_log'"
fi
if [[ "$stop_log" == "$LOGS1/"* ]]; then
  pass "budget stop log lives under WALLET_GATE_LOG_DIR"
else
  fail "budget stop log '$stop_log' is not under $LOGS1"
fi

LOGS30="$WORK/logs-budget-30"
run_capture "$WORK/budget-30.out" env WALLET_GATE_BUDGET_SECONDS=30 WALLET_GATE_LOG_DIR="$LOGS30" \
  WALLET_GATE_TARGETS_FILE="$WORK/slow-and-broken.tsv" "$GATE_TEST"
if [[ "$RC" -ne 0 ]]; then
  pass "failing target makes the gate exit non-zero ($RC)"
else
  fail "failing target left the gate exiting 0"
fi
if grep -qE "^broken +3 +$LOGS30/broken\.log$" "$WORK/budget-30.out"; then
  pass "table shows the failing target with exit code 3 and its log"
else
  fail "table does not show broken with exit code 3:"
  sed 's/^/    /' "$WORK/budget-30.out"
fi
if grep -qE "^slow +0 +" "$WORK/budget-30.out"; then
  pass "table shows the slow target passing within the budget"
else
  fail "table does not show slow with exit code 0"
fi
assert_contains "$WORK/budget-30.out" "summary: ran=2 passed=1 failed=1 absent=0 result=FAIL" "summary counts the failure"

printf 'passing\t%s\ttrue\nmissing\t%s/nowhere\ttrue\n' "$WORK/target" "$WORK" >"$WORK/pass-and-absent.tsv"
run_capture "$WORK/absent.out" env WALLET_GATE_BUDGET_SECONDS=30 WALLET_GATE_LOG_DIR="$WORK/logs-absent" \
  WALLET_GATE_TARGETS_FILE="$WORK/pass-and-absent.tsv" "$GATE_TEST"
assert_contains "$WORK/absent.out" "absent: missing ($WORK/nowhere)" "absent target is reported with its path"
assert_contains "$WORK/absent.out" "summary: ran=1 passed=1 failed=0 absent=1 result=INCOMPLETE" "absent target is not counted as passed"

printf 'passing\t%s\ttrue\n' "$WORK/target" >"$WORK/pass.tsv"
run_capture "$WORK/pass.out" env WALLET_GATE_BUDGET_SECONDS=30 WALLET_GATE_LOG_DIR="$WORK/logs-pass" \
  WALLET_GATE_TARGETS_FILE="$WORK/pass.tsv" "$GATE_TEST"
if [[ "$RC" -eq 0 ]]; then
  pass "passing target exits 0"
else
  fail "passing target exited $RC"
  sed 's/^/    /' "$WORK/pass.out"
fi
assert_contains "$WORK/pass.out" "summary: ran=1 passed=1 failed=0 absent=0 result=PASS" "passing summary"

run_capture "$WORK/lint-pass.out" env WALLET_GATE_BUDGET_SECONDS=30 WALLET_GATE_LOG_DIR="$WORK/logs-lint-pass" \
  WALLET_GATE_TARGETS_FILE="$WORK/pass.tsv" "$GATE_LINT"
if [[ "$RC" -eq 0 ]]; then
  pass "gate-lint.sh passing target exits 0"
else
  fail "gate-lint.sh passing target exited $RC"
  sed 's/^/    /' "$WORK/lint-pass.out"
fi

run_capture "$WORK/lint-budget-1.out" env WALLET_GATE_BUDGET_SECONDS=1 WALLET_GATE_LOG_DIR="$WORK/logs-lint-1" \
  WALLET_GATE_TARGETS_FILE="$WORK/slow-and-broken.tsv" "$GATE_LINT"
assert_contains "$WORK/lint-budget-1.out" "stopped on: slow: sleep 5" "gate-lint.sh budget stop reports the command"
assert_contains "$WORK/lint-budget-1.out" "exit code: 124" "gate-lint.sh budget stop reports exit code 124"

if [[ "$failures" -gt 0 ]]; then
  echo "gate.test.sh: $failures failure(s)"
  exit 1
fi
echo "gate.test.sh: all assertions passed"
