#!/bin/sh
set -eu
umask 077

: "${LAYERX_GATEWAY_URL:?real hosted gateway URL is required}"
: "${LAYERX_GATEWAY_CA_FILE:?gateway CA file is required}"
: "${LAYERX_GATEWAY_KEY_ID:?issued gateway key ID is required}"
: "${LAYERX_GATEWAY_KEY_SECRET:?issued gateway key secret is required}"
: "${LAYERX_GATEWAY_ACTIVITY_FILE:?real signed activity file is required}"
: "${LAYERX_GATEWAY_ACTIVITY_IDEMPOTENCY_KEY:?activity protocol idempotency key is required}"
: "${LAYERX_GATEWAY_CONFLICT_ACTIVITY_FILE:?second signed activity with the same protocol idempotency key is required}"
: "${LAYERX_RECEIPT_VERIFY_BIN:?independent receipt verifier executable is required}"

python3 - <<'PYCREDENTIAL'
import os
import re
assert re.fullmatch(r"[A-Za-z0-9_-]{1,64}", os.environ["LAYERX_GATEWAY_KEY_ID"]), "invalid gateway key identifier"
assert re.fullmatch(r"lxp_live_[0-9a-f]{64}", os.environ["LAYERX_GATEWAY_KEY_SECRET"]), "invalid gateway credential"
PYCREDENTIAL

request_dir=$(mktemp -d)
trap 'rm -rf "$request_dir"' EXIT HUP INT TERM

authorization="LayerX-Key ${LAYERX_GATEWAY_KEY_ID}:${LAYERX_GATEWAY_KEY_SECRET}"
idempotency="$LAYERX_GATEWAY_ACTIVITY_IDEMPOTENCY_KEY"
printf 'header = "Authorization: %s"\n' "$authorization" > "$request_dir/authorization.curl"
printf 'header = "Authorization: Bearer %s"\n' "$LAYERX_GATEWAY_KEY_SECRET" > "$request_dir/bearer.curl"
curl --fail-with-body --silent --show-error --max-time 60 --cacert "$LAYERX_GATEWAY_CA_FILE" \
  --config "$request_dir/authorization.curl" \
  -H "Idempotency-Key: $idempotency" \
  -H 'Content-Type: application/octet-stream' \
  --data-binary "@$LAYERX_GATEWAY_ACTIVITY_FILE" \
  "$LAYERX_GATEWAY_URL/v1/activities" > "$request_dir/activity.json"

jq -er '.ok == true and (.result.activity_id | length == 64) and (.result.receipt | length > 0)' "$request_dir/activity.json" >/dev/null
activity_id=$(jq -er '.result.activity_id' "$request_dir/activity.json")
jq -er '.result.receipt' "$request_dir/activity.json" | xxd -r -p > "$request_dir/receipt.bin"

curl --fail-with-body --silent --show-error --max-time 60 --cacert "$LAYERX_GATEWAY_CA_FILE" \
  --config "$request_dir/authorization.curl" \
  "$LAYERX_GATEWAY_URL/v1/receipts/$activity_id" > "$request_dir/lookup.json"
jq -er --arg id "$activity_id" '.ok == true and .result.activity_id == $id and (.result.receipt | length > 0)' "$request_dir/lookup.json" >/dev/null

"$LAYERX_RECEIPT_VERIFY_BIN" "$request_dir/receipt.bin"

test "$(curl --silent --max-time 60 --output /dev/null --write-out '%{http_code}' --cacert "$LAYERX_GATEWAY_CA_FILE" "$LAYERX_GATEWAY_URL/__emulator/reset")" = 404
test "$(curl --silent --max-time 60 --output /dev/null --write-out '%{http_code}' --cacert "$LAYERX_GATEWAY_CA_FILE" --config "$request_dir/bearer.curl" "$LAYERX_GATEWAY_URL/v1/state")" = 401
test "$(curl --silent --max-time 60 --output /dev/null --write-out '%{http_code}' --cacert "$LAYERX_GATEWAY_CA_FILE" --config "$request_dir/authorization.curl" -H "Idempotency-Key: $idempotency" -H 'Content-Type: application/octet-stream' --data-binary "@$LAYERX_GATEWAY_CONFLICT_ACTIVITY_FILE" "$LAYERX_GATEWAY_URL/v1/activities")" = 409

principal_status=$(curl --silent --max-time 60 --output "$request_dir/principal.json" --write-out '%{http_code}' --cacert "$LAYERX_GATEWAY_CA_FILE" --config "$request_dir/authorization.curl" "$LAYERX_GATEWAY_URL/internal/v1/principal")
test "$principal_status" = 200
jq -er '.ok == true and (.result.principal_digest | test("^[0-9a-f]{64}$"))' "$request_dir/principal.json" >/dev/null
test "$(curl --silent --max-time 60 --output /dev/null --write-out '%{http_code}' --cacert "$LAYERX_GATEWAY_CA_FILE" "$LAYERX_GATEWAY_URL/internal/v1/principal")" = 401
test "$(curl --silent --max-time 60 --output /dev/null --write-out '%{http_code}' --cacert "$LAYERX_GATEWAY_CA_FILE" --config "$request_dir/authorization.curl" -H 'X-LayerX-Principal: foreign-principal' "$LAYERX_GATEWAY_URL/internal/v1/principal")" = 400
