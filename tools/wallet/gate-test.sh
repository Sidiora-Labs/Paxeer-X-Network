#!/usr/bin/env bash
set -euo pipefail

GATE_NAME="gate-test"
SCRIPT_PATH="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/$(basename "${BASH_SOURCE[0]}")"
ROOT="$(cd "$(dirname "$SCRIPT_PATH")/../.." && pwd)"
BUDGET="${WALLET_GATE_BUDGET_SECONDS:-1200}"
LOG_DIR="${WALLET_GATE_LOG_DIR:-$ROOT/.logs/wallet-gate}"
REQUIRED_TOOLS=(go pnpm cargo timeout)

usage() {
  cat <<'EOF'
Usage: tools/wallet/gate-test.sh [--check | --help]

Runs the test suites of the wallet tree in order: the Go modules
human/wallet/attestor and human/wallet/ceremony, the pnpm workspace
human/wallet, the app human/apps/wallet, the Rust crates
layerx-human-kms, layerx-human-identity-provider and layerx-human-service
in the human workspace, and layerx-platform-gateway in the platform
workspace. A target whose directory or manifest does not exist is reported
as absent and is never counted as passed.

  --check   validate this script's syntax, the required tooling
            (go, pnpm, cargo, timeout) and print the target list with
            exists or absent per target, without running any suite
  --help    print this text

Environment:
  WALLET_GATE_BUDGET_SECONDS  budget for the whole run in seconds (default 1200);
                              each target runs under timeout with the remaining
                              budget; when it is exhausted the gate stops and
                              reports the command, its exit code (124) and log
  WALLET_GATE_LOG_DIR         directory for per-target logs
                              (default .logs/wallet-gate under the repository root)
  WALLET_GATE_TARGETS_FILE    read targets from this file instead of the built-in
                              list, one per line: label<TAB>directory<TAB>command,
                              with an optional fourth field naming a file inside
                              the directory that must exist

Exit status is non-zero when any target that ran failed or the budget ran out.
EOF
}

builtin_targets() {
  printf '%s\t%s\t%s\t%s\n' \
    go-attestor human/wallet/attestor 'go test ./...' go.mod \
    go-ceremony human/wallet/ceremony 'go test ./...' go.mod \
    pnpm-wallet human/wallet 'pnpm -r test' pnpm-workspace.yaml \
    pnpm-app human/apps/wallet 'pnpm exec vitest run' package.json \
    rust-human human 'cargo test -p layerx-human-kms -p layerx-human-identity-provider -p layerx-human-service' Cargo.toml \
    rust-platform platform 'cargo test -p layerx-platform-gateway' hosted/gateway/Cargo.toml
}

LABELS=()
DIRS=()
CMDS=()
MARKERS=()

load_targets() {
  local source_text label dir cmd marker
  if [[ -n "${WALLET_GATE_TARGETS_FILE:-}" ]]; then
    if [[ ! -r "$WALLET_GATE_TARGETS_FILE" ]]; then
      echo "$GATE_NAME: targets file not readable: $WALLET_GATE_TARGETS_FILE" >&2
      exit 2
    fi
    source_text="$(cat "$WALLET_GATE_TARGETS_FILE")"
  else
    source_text="$(builtin_targets)"
  fi
  while IFS=$'\t' read -r label dir cmd marker; do
    [[ -z "$label" || "$label" == \#* ]] && continue
    if [[ -z "$dir" || -z "$cmd" ]]; then
      echo "$GATE_NAME: malformed target line for label: $label" >&2
      exit 2
    fi
    LABELS+=("$label")
    DIRS+=("$dir")
    CMDS+=("$cmd")
    MARKERS+=("${marker:-}")
  done <<<"$source_text"
  if [[ ${#LABELS[@]} -eq 0 ]]; then
    echo "$GATE_NAME: no targets" >&2
    exit 2
  fi
}

abs_dir() {
  if [[ "$1" == /* ]]; then
    printf '%s' "$1"
  else
    printf '%s' "$ROOT/$1"
  fi
}

missing_path() {
  local dir="$1" marker="$2"
  if [[ ! -d "$dir" ]]; then
    printf '%s' "$dir"
  elif [[ -n "$marker" && ! -e "$dir/$marker" ]]; then
    printf '%s' "$dir/$marker"
  fi
}

log_path_for() {
  printf '%s/%s.log' "$LOG_DIR" "$(printf '%s' "$1" | tr -c 'A-Za-z0-9._-' '_')"
}

run_check() {
  local status=0 tool i dir missing
  if bash -n "$SCRIPT_PATH"; then
    echo "syntax: ok"
  else
    echo "syntax: failed"
    status=1
  fi
  for tool in "${REQUIRED_TOOLS[@]}"; do
    if command -v "$tool" >/dev/null 2>&1; then
      echo "tool $tool: found"
    else
      echo "tool $tool: missing"
      status=1
    fi
  done
  if ! [[ "$BUDGET" =~ ^[0-9]+$ ]] || [[ "$BUDGET" -le 0 ]]; then
    echo "budget: invalid WALLET_GATE_BUDGET_SECONDS=$BUDGET"
    status=1
  else
    echo "budget: ${BUDGET}s"
  fi
  echo "targets:"
  for i in "${!LABELS[@]}"; do
    dir="$(abs_dir "${DIRS[$i]}")"
    missing="$(missing_path "$dir" "${MARKERS[$i]}")"
    if [[ -z "$missing" ]]; then
      printf '  %s\texists\t%s\t%s\n' "${LABELS[$i]}" "${DIRS[$i]}" "${CMDS[$i]}"
    else
      printf '  %s\tabsent\t%s\t%s\n' "${LABELS[$i]}" "$missing" "${CMDS[$i]}"
    fi
  done
  exit "$status"
}

RESULT_LABELS=()
RESULT_CODES=()
RESULT_LOGS=()

print_table() {
  local i
  echo
  printf '%-24s %-10s %s\n' target exit log
  for i in "${!RESULT_LABELS[@]}"; do
    printf '%-24s %-10s %s\n' "${RESULT_LABELS[$i]}" "${RESULT_CODES[$i]}" "${RESULT_LOGS[$i]}"
  done
}

print_summary() {
  local ran="$1" passed="$2" failed="$3" absent="$4" result
  if [[ "$failed" -gt 0 ]]; then
    result=FAIL
  elif [[ "$absent" -gt 0 ]]; then
    result=INCOMPLETE
  else
    result=PASS
  fi
  echo
  echo "summary: ran=$ran passed=$passed failed=$failed absent=$absent result=$result"
}

run_gate() {
  local ran=0 passed=0 failed=0 absent=0 i dir missing log start elapsed remaining rc j
  if ! [[ "$BUDGET" =~ ^[0-9]+$ ]] || [[ "$BUDGET" -le 0 ]]; then
    echo "$GATE_NAME: invalid WALLET_GATE_BUDGET_SECONDS=$BUDGET" >&2
    exit 2
  fi
  mkdir -p "$LOG_DIR"
  start=$SECONDS
  echo "$GATE_NAME: budget ${BUDGET}s, logs in $LOG_DIR"
  for i in "${!LABELS[@]}"; do
    dir="$(abs_dir "${DIRS[$i]}")"
    missing="$(missing_path "$dir" "${MARKERS[$i]}")"
    if [[ -n "$missing" ]]; then
      echo "absent: ${LABELS[$i]} ($missing)"
      absent=$((absent + 1))
      RESULT_LABELS+=("${LABELS[$i]}")
      RESULT_CODES+=(absent)
      RESULT_LOGS+=("$missing")
      continue
    fi
    log="$(log_path_for "${LABELS[$i]}")"
    elapsed=$((SECONDS - start))
    remaining=$((BUDGET - elapsed))
    if [[ "$remaining" -le 0 ]]; then
      printf 'budget exhausted before start after %ss\n' "$elapsed" >"$log"
      rc=124
    else
      echo "run: ${LABELS[$i]} in ${DIRS[$i]}: ${CMDS[$i]} (remaining ${remaining}s)"
      set +e
      (cd "$dir" && timeout --kill-after=10 "$remaining" bash -c "${CMDS[$i]}") 2>&1 | tee "$log"
      rc=${PIPESTATUS[0]}
      set -e
    fi
    ran=$((ran + 1))
    RESULT_LABELS+=("${LABELS[$i]}")
    RESULT_CODES+=("$rc")
    RESULT_LOGS+=("$log")
    if [[ "$rc" -eq 124 ]] || [[ "$rc" -eq 137 && $((SECONDS - start)) -ge "$BUDGET" ]]; then
      failed=$((failed + 1))
      for ((j = i + 1; j < ${#LABELS[@]}; j++)); do
        RESULT_LABELS+=("${LABELS[$j]}")
        RESULT_CODES+=(not-run)
        RESULT_LOGS+=("-")
      done
      echo
      echo "$GATE_NAME: budget of ${BUDGET}s exhausted"
      echo "stopped on: ${LABELS[$i]}: ${CMDS[$i]}"
      echo "exit code: $rc"
      echo "log: $log"
      print_table
      print_summary "$ran" "$passed" "$failed" "$absent"
      exit "$rc"
    fi
    if [[ "$rc" -eq 0 ]]; then
      passed=$((passed + 1))
    else
      failed=$((failed + 1))
      echo "failed: ${LABELS[$i]}: ${CMDS[$i]}: exit code $rc, log $log"
    fi
  done
  print_table
  print_summary "$ran" "$passed" "$failed" "$absent"
  if [[ "$failed" -gt 0 ]]; then
    exit 1
  fi
  exit 0
}

case "${1:-}" in
  --help | -h)
    usage
    exit 0
    ;;
  --check)
    load_targets
    run_check
    ;;
  "")
    load_targets
    run_gate
    ;;
  *)
    usage >&2
    exit 2
    ;;
esac
