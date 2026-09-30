#!/bin/sh
# Init of the gas station app on Fly (interop/deploy/gas-station/fly.toml).
# Runs as root on the machine: renders the station configuration from the
# app's env and secrets onto the volume, hands the journal directory to uid
# 4020 and starts paxeer-gas-station under that uid. The relayer key stays in
# the env variable GAS_STATION_RELAYER_KEY_ENV names and is never written out.
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
chown -R 4020:4020 "$dir"
exec setpriv --reuid=4020 --regid=4020 --clear-groups --no-new-privs \
	/usr/local/bin/paxeer-gas-station --config "$config" --journal "$dir/sponsorship.jsonl"
