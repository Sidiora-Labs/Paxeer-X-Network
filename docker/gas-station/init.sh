#!/bin/bash
# Init of the gas station app on Fly (interop/deploy/gas-station/fly.toml).
# Runs as root on the machine: renders the station configuration and the rate
# publisher configuration from the app's env and secrets onto the volume, hands
# the journal directory to uid 4020 and runs both processes of the machine
# under that uid: paxeer-gas-station serving quotes and submissions, and
# paxeer-gas-station rate publishing the owner's rate file
# /data/gas-station/rate.toml to the paymaster with setRate. The relayer key
# stays in the env variable GAS_STATION_RELAYER_KEY_ENV names and reaches only
# the station; the paymaster owner's key stays in GAS_STATION_RATE_OWNER_KEY
# and reaches only the publisher; neither is written out. When either process
# exits, the other is stopped and the init exits with its status, so the
# machine restarts both.
set -eu
umask 077
dir=/data/gas-station
config="$dir/station.json"

# need <NAME> <pattern>: the variable must be set and match the grep -E
# pattern, so every rendered value is a plain JSON token.
need() {
	value=""
	eval "value=\${$1:-}"
	if ! printf '%s' "$value" | grep -Eqx "$2"; then
		echo "gas-station-init: $1 is unset or malformed" >&2
		exit 2
	fi
}
uint='[0-9]+'
addr='0x[0-9a-fA-F]{40}'
need GAS_STATION_LISTEN '\[::\]:[0-9]+'
need GAS_STATION_CHAIN_ID "$uint"
need GAS_STATION_ENDPOINTS 'https://[^ "\\]+( https://[^ "\\]+)*'
need GAS_STATION_PAYMASTER "$addr"
need GAS_STATION_TOKEN "$addr"
need GAS_STATION_DECIMALS "$uint"
need GAS_STATION_MAX_RATE_AGE "$uint"
need GAS_STATION_SPREAD_BPS "$uint"
need GAS_STATION_MARGIN_BPS "$uint"
need GAS_STATION_PER_ACCOUNT_LIMIT "$uint"
need GAS_STATION_PER_INTERVAL_LIMIT "$uint"
need GAS_STATION_PER_QUOTE_LIMIT "$uint"
need GAS_STATION_INTERVAL_SECONDS "$uint"
need GAS_STATION_BALANCE_FLOOR "$uint"
need GAS_STATION_GAS_LIMIT "$uint"
need GAS_STATION_MAX_PRIORITY_FEE_PER_GAS "$uint"
need GAS_STATION_RELAYER_KEY_ENV '[A-Z_][A-Z0-9_]*'
need "$GAS_STATION_RELAYER_KEY_ENV" '(0x)?[0-9a-fA-F]{64}'
need GAS_STATION_RATE_OWNER_KEY '(0x)?[0-9a-fA-F]{64}'
need GAS_STATION_RATE_CADENCE_SECONDS "$uint"
need GAS_STATION_RATE_GAS_BUDGET_PER_DAY "$uint"
need GAS_STATION_RATE_BALANCE_FLOOR "$uint"

endpoints=""
for url in $GAS_STATION_ENDPOINTS; do
	endpoints="$endpoints${endpoints:+,}\"$url\""
done

mkdir -p "$dir"
printf '{"listen":"%s","gas_limit":%s,"max_priority_fee_per_gas":%s,"chain_id":%s,"endpoints":[%s],"paymaster":"%s","token":"%s","decimals":%s,"max_rate_age":%s,"spread_bps":%s,"margin_bps":%s,"per_account_limit":%s,"per_interval_limit":%s,"per_quote_limit":%s,"interval_seconds":%s,"balance_floor":%s,"relayer_key_env":"%s"}\n' \
	"$GAS_STATION_LISTEN" "$GAS_STATION_GAS_LIMIT" "$GAS_STATION_MAX_PRIORITY_FEE_PER_GAS" \
	"$GAS_STATION_CHAIN_ID" "$endpoints" "$GAS_STATION_PAYMASTER" "$GAS_STATION_TOKEN" \
	"$GAS_STATION_DECIMALS" "$GAS_STATION_MAX_RATE_AGE" "$GAS_STATION_SPREAD_BPS" \
	"$GAS_STATION_MARGIN_BPS" "$GAS_STATION_PER_ACCOUNT_LIMIT" "$GAS_STATION_PER_INTERVAL_LIMIT" \
	"$GAS_STATION_PER_QUOTE_LIMIT" "$GAS_STATION_INTERVAL_SECONDS" "$GAS_STATION_BALANCE_FLOOR" \
	"$GAS_STATION_RELAYER_KEY_ENV" >"$config.new"
mv "$config.new" "$config"
chmod 0644 "$config"
printf '{"max_priority_fee_per_gas":%s,"chain_id":%s,"endpoints":[%s],"paymaster":"%s","token":"%s","decimals":%s,"max_rate_age":%s,"spread_bps":%s,"margin_bps":%s,"per_account_limit":%s,"per_interval_limit":%s,"per_quote_limit":%s,"interval_seconds":%s,"balance_floor":%s,"relayer_key_env":"%s","rate_owner_key_env":"GAS_STATION_RATE_OWNER_KEY","rate_cadence_seconds":%s,"rate_gas_budget_per_day":%s,"rate_balance_floor":%s}\n' \
	"$GAS_STATION_MAX_PRIORITY_FEE_PER_GAS" \
	"$GAS_STATION_CHAIN_ID" "$endpoints" "$GAS_STATION_PAYMASTER" "$GAS_STATION_TOKEN" \
	"$GAS_STATION_DECIMALS" "$GAS_STATION_MAX_RATE_AGE" "$GAS_STATION_SPREAD_BPS" \
	"$GAS_STATION_MARGIN_BPS" "$GAS_STATION_PER_ACCOUNT_LIMIT" "$GAS_STATION_PER_INTERVAL_LIMIT" \
	"$GAS_STATION_PER_QUOTE_LIMIT" "$GAS_STATION_INTERVAL_SECONDS" "$GAS_STATION_BALANCE_FLOOR" \
	"$GAS_STATION_RELAYER_KEY_ENV" "$GAS_STATION_RATE_CADENCE_SECONDS" \
	"$GAS_STATION_RATE_GAS_BUDGET_PER_DAY" "$GAS_STATION_RATE_BALANCE_FLOOR" >"$dir/rate.json.new"
mv "$dir/rate.json.new" "$dir/rate.json"
chmod 0644 "$dir/rate.json"
chown -R 4020:4020 "$dir"
env -u GAS_STATION_RATE_OWNER_KEY setpriv --reuid=4020 --regid=4020 --clear-groups --no-new-privs \
	/usr/local/bin/paxeer-gas-station --config "$config" --journal "$dir/sponsorship.jsonl" &
station=$!
env -u "$GAS_STATION_RELAYER_KEY_ENV" setpriv --reuid=4020 --regid=4020 --clear-groups --no-new-privs \
	/usr/local/bin/paxeer-gas-station rate --config "$dir/rate.json" --journal "$dir/rate.jsonl" \
	--rate-file "$dir/rate.toml" &
publisher=$!
trap 'kill -TERM "$station" "$publisher" 2>/dev/null' TERM INT
status=0
wait -n "$station" "$publisher" || status=$?
kill -TERM "$station" "$publisher" 2>/dev/null || true
wait || true
exit "$status"
