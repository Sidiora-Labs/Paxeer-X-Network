#!/usr/bin/env bash
# Programme checks of the v6.12.0 rehearsal. Each check_* function runs one programme.py scenario
# against the four rehearsal nodes and prints PASS <name> or FAIL <name>, returning non-zero on failure.
# The driver defines release, logs, credentials, units, sidecar_pids, halt_height, rpc_url and evm_url.

programme_args() {
  local i pid
  for ((i = 0; i < 4; i++)); do
    printf '%s\n' --evm "$(evm_url "$i")" --comet "$(rpc_url "$i")"
  done
  printf '%s\n' --halt-height "$halt_height"
  printf '%s\n' --trader-key "${programme_trader_key_file:-$PROGRAMME_TRADER_KEY_FILE}"
  printf '%s\n' --maker-key "${programme_maker_key_file:-$PROGRAMME_MAKER_KEY_FILE}"
  printf '%s\n' --feed-key "$credentials/${units[0]}/feed.key"
  for pid in "${sidecar_pids[@]}"; do
    printf '%s\n' --sidecar-pid "$pid"
  done
}

run_programme_check() {
  local name=$1 args
  mapfile -t args < <(programme_args)
  if python3 "$release/rehearsal/programme.py" "$name" "${args[@]}" >"$logs/programme-$name.log" 2>&1; then
    echo "PASS $name"
  else
    echo "FAIL $name"
    return 1
  fi
}

check_launchpad_interface() { run_programme_check check_launchpad_interface; }
check_usdl_erc20_custody() { run_programme_check check_usdl_erc20_custody; }
check_pool_parity() { run_programme_check check_pool_parity; }
check_fee_right_and_pool_token() { run_programme_check check_fee_right_and_pool_token; }
check_perps_order_types() { run_programme_check check_perps_order_types; }
check_perps_margin() { run_programme_check check_perps_margin; }
check_perps_economics() { run_programme_check check_perps_economics; }
check_perps_markets() { run_programme_check check_perps_markets; }
check_perps_log_rebuild() { run_programme_check check_perps_log_rebuild; }
check_spot_interface() { run_programme_check check_spot_interface; }
check_spot_settlement() { run_programme_check check_spot_settlement; }
check_spot_logs() { run_programme_check check_spot_logs; }
check_sidecar_submissions() { run_programme_check check_sidecar_submissions; }
check_price_freshness() { run_programme_check check_price_freshness; }
check_price_submission_bounds() { run_programme_check check_price_submission_bounds; }
check_plan_and_version() { run_programme_check check_plan_and_version; }

run_programme_checks() {
  local check failed=0
  for check in check_launchpad_interface check_usdl_erc20_custody check_pool_parity check_fee_right_and_pool_token \
    check_perps_order_types check_perps_margin check_perps_economics check_perps_markets check_perps_log_rebuild \
    check_spot_interface check_spot_settlement check_spot_logs \
    check_sidecar_submissions check_price_freshness check_price_submission_bounds check_plan_and_version; do
    "$check" || failed=1
  done
  return "$failed"
}
