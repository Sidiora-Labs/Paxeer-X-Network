#!/usr/bin/env bash
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
fail() { echo "FAIL: $*" >&2; exit 1; }

mkdir "$work/certs"
for i in 1 2 3 4 5; do
	openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:prime256v1 -nodes -days 1 \
		-subj "/CN=paxeer-attestor-$i" -keyout "$work/certs/attestor-$i.key" -out "$work/certs/attestor-$i.crt" 2>/dev/null
done

out=$work/out
ATTESTOR_CERT_DIR=$work/certs MODULE_REGISTRY_FILE=$root/interop/deploy/gateway/module-registry.example.json \
	"$root/tools/bringup/mint-secrets.sh" "$out" >"$work/stdout"

[ "$(stat -c %a "$out")" = 700 ] || fail "out-dir mode is not 700"
apps="paxeer-wallet-gateway paxeer-attestor-1 paxeer-attestor-2 paxeer-attestor-3 paxeer-attestor-4 paxeer-attestor-5 paxeer-human-service paxeer-shared-endpoint paxeer-webhooks paxeer-internal"
for app in $apps; do
	f=$out/$app.fly-secrets
	[ -f "$f" ] || fail "no block for $app"
	[ "$(stat -c %a "$f")" = 600 ] || fail "$f is not 0600"
	head -1 "$f" | grep -qx "fly secrets set --app $app \\\\" || fail "$f does not start a fly secrets set block"
	grep -qxF "$f" "$work/stdout" || fail "$f not reported"
done
[ "$(wc -l <"$work/stdout")" -eq 10 ] || fail "tool printed more than the block paths"

# value <app> <NAME>
value() { sed -n "s/^  $2=\(.*\)$/\1/p" "$out/$1.fly-secrets" | sed 's/ \\$//'; }

# Every [[files]] secret an app's toml names for the minted set is present.
expect() {
	local app=$1
	shift
	for name in "$@"; do [ -n "$(value "$app" "$name")" ] || fail "$app lacks $name"; done
}
for i in 1 2 3 4 5; do
	expect "paxeer-attestor-$i" WALLET_IDENTITY_BINDING_PUBLIC_KEY WALLET_CUSTODY_INVENTORY_PUBLIC_KEY WALLET_CUSTODY_INVENTORY
done
expect paxeer-human-service HUMAN_EVENTS_JOURNEY_TOKEN HUMAN_EVENTS_APPROVAL_TOKEN HUMAN_EVENTS_WEBHOOKS_TOKEN
expect paxeer-shared-endpoint ENDPOINT_EVENTS_PAYMENT_TOKEN ENDPOINT_EVENTS_WEBHOOKS_TOKEN ENDPOINT_COMPONENT_TOKEN \
	ENDPOINT_AUTHORITY_TOKEN ENDPOINT_IDENTITY_TOKEN ENDPOINT_IDENTITY_PROVISIONING_TOKEN ENDPOINT_PROGRAM_REGISTRY_TOKEN \
	ENDPOINT_KEY_PROVISIONING_KEY ENDPOINT_MODULE_REGISTRY
expect paxeer-webhooks WEBHOOKS_KMS_TOKEN WEBHOOKS_IDENTITY_TOKEN WEBHOOKS_COMPONENT_TOKEN WEBHOOKS_AUTHORITY_TOKEN \
	WEBHOOKS_JOURNEY_SOURCE_TOKEN WEBHOOKS_PAYMENT_SOURCE_TOKEN WEBHOOKS_APPROVAL_SOURCE_TOKEN WEBHOOKS_PROGRAM_SOURCE_TOKEN \
	WEBHOOKS_SOURCE_TRIGGER_TOKEN WEBHOOKS_OPERATOR_TOKEN WEBHOOKS_CURSOR_KEY
expect paxeer-internal INTERNAL_KMS_TOKEN INTERNAL_KMS_SEAL_SECRET INTERNAL_JOURNEYS_TOKEN INTERNAL_JOURNEYS_PRODUCER_TOKEN \
	INTERNAL_PAYMENTS_TOKEN INTERNAL_PAYMENTS_PRODUCER_TOKEN INTERNAL_APPROVALS_TOKEN INTERNAL_APPROVALS_PRODUCER_TOKEN \
	INTERNAL_PROGRAMS_TOKEN INTERNAL_PROGRAMS_PRODUCER_TOKEN

# Paired ends carry the same value.
same() { [ "$(value "$1" "$2")" = "$(value "$3" "$4")" ] || fail "$1 $2 differs from $3 $4"; }
same paxeer-human-service HUMAN_EVENTS_JOURNEY_TOKEN paxeer-internal INTERNAL_JOURNEYS_PRODUCER_TOKEN
same paxeer-human-service HUMAN_EVENTS_APPROVAL_TOKEN paxeer-internal INTERNAL_APPROVALS_PRODUCER_TOKEN
same paxeer-shared-endpoint ENDPOINT_EVENTS_PAYMENT_TOKEN paxeer-internal INTERNAL_PAYMENTS_PRODUCER_TOKEN
same paxeer-human-service HUMAN_EVENTS_WEBHOOKS_TOKEN paxeer-webhooks WEBHOOKS_SOURCE_TRIGGER_TOKEN
same paxeer-shared-endpoint ENDPOINT_EVENTS_WEBHOOKS_TOKEN paxeer-webhooks WEBHOOKS_SOURCE_TRIGGER_TOKEN
same paxeer-webhooks WEBHOOKS_KMS_TOKEN paxeer-internal INTERNAL_KMS_TOKEN
same paxeer-webhooks WEBHOOKS_JOURNEY_SOURCE_TOKEN paxeer-internal INTERNAL_JOURNEYS_TOKEN
for i in 2 3 4 5; do
	for name in WALLET_IDENTITY_BINDING_PUBLIC_KEY WALLET_CUSTODY_INVENTORY_PUBLIC_KEY WALLET_CUSTODY_INVENTORY; do
		same paxeer-attestor-1 "$name" "paxeer-attestor-$i" "$name"
	done
done
same paxeer-wallet-gateway WALLET_CUSTODY_INVENTORY paxeer-attestor-1 WALLET_CUSTODY_INVENTORY
[ "$(value paxeer-shared-endpoint ENDPOINT_KEY_PROVISIONING_KEY | base64 -d)" != "$(value paxeer-shared-endpoint ENDPOINT_COMPONENT_TOKEN | base64 -d)" ] ||
	fail "independent tokens collide"
value paxeer-shared-endpoint ENDPOINT_KEY_PROVISIONING_KEY | base64 -d | grep -qxE '[0-9a-f]{64}' || fail "key provisioning key is not 32-byte hex"

# No minted value reached stdout.
for app in $apps; do
	sed -n 's/^  [A-Z_]*=\(.*\)$/\1/p' "$out/$app.fly-secrets" | sed 's/ \\$//' | while read -r v; do
		if grep -qF -- "$v" "$work/stdout"; then fail "$app value printed"; fi
	done
done

# The gateway's own env schema and inventory verifier accept the minted
# files, decoded as Fly writes [[files]] secrets.
gw=$work/gateway
mkdir -m 700 "$gw"
for pair in WALLET_IDENTITY_BINDING_KEY:binding.key WALLET_CUSTODY_INVENTORY:inventory.jwt WALLET_CUSTODY_INVENTORY_PUBLIC_KEY:inventory.pem; do
	value paxeer-wallet-gateway "${pair%%:*}" | base64 -d >"$gw/${pair#*:}"
	chmod 600 "$gw/${pair#*:}"
done
touch "$gw/client.crt" "$gw/client.key" "$gw/ca.crt"
issuer=$(sed -n 's/^  ATTESTOR_AUTHORITY_ISSUER = "\(.*\)"$/\1/p' "$root/human/wallet/deploy/attestor-1.toml")
pins=$(for i in 1 2 3 4 5; do openssl x509 -in "$work/certs/attestor-$i.crt" -pubkey -noout | openssl pkey -pubin -outform DER | sha256sum | cut -d' ' -f1; done | paste -sd,)
cat >"$work/check.mts" <<'EOF'
import { createPrivateKey } from 'node:crypto';
import { readFileSync } from 'node:fs';
const { env } = await import(`${process.env.GATEWAY_SRC}/env.js`);
const { loadWalletInventory } = await import(`${process.env.GATEWAY_SRC}/agent/authority.js`);
const inventory = loadWalletInventory();
const key = createPrivateKey(readFileSync(env.WALLET_IDENTITY_BINDING_PRIVATE_KEY_FILE!));
if (key.asymmetricKeyDetails?.namedCurve !== 'prime256v1') throw new Error('binding key is not P-256');
const pins = process.env.EXPECTED_PINS!.split(',');
if (inventory.members.map((m) => m.spki_sha256).join(',') !== pins.join(',')) throw new Error('inventory members do not pin the attestor certificates');
if (inventory.members.map((m) => m.id).join(',') !== '1,2,3,4,5' || inventory.keys.length !== 0) throw new Error('inventory shape');
console.log('gateway env and inventory accepted');
EOF
(
	cd "$root/human/wallet/gateway"
	env -i PATH="$PATH" HOME="$HOME" NODE_ENV=production CORS_ORIGINS=https://paxportwallet.com \
		SUPABASE_URL="${issuer%/auth/v1}" DATABASE_URL=postgres://wallet@localhost:5432/wallet \
		WALLET_MASTER_KEY="$(openssl rand -base64 32)" \
		WALLET_IDENTITY_BINDING_TENANT="$(value paxeer-wallet-gateway WALLET_IDENTITY_BINDING_TENANT)" \
		WALLET_IDENTITY_BINDING_PRIVATE_KEY_FILE="$gw/binding.key" \
		WALLET_CUSTODY_INVENTORY_FILE="$gw/inventory.jwt" WALLET_CUSTODY_INVENTORY_PUBLIC_KEY_FILE="$gw/inventory.pem" \
		ATTESTOR_ENDPOINTS="$(sed -n 's/^  ATTESTOR_ENDPOINTS = "\(.*\)"$/\1/p' "$root/human/wallet/deploy/gateway.toml")" \
		ATTESTOR_CLIENT_CERT_FILE="$gw/client.crt" ATTESTOR_CLIENT_KEY_FILE="$gw/client.key" ATTESTOR_CA_FILE="$gw/ca.crt" \
		ATTESTOR_QUORUM=3 EXPECTED_PINS="$pins" GATEWAY_SRC="$root/human/wallet/gateway/src" \
		node_modules/.bin/tsx "$work/check.mts"
) || fail "gateway rejected the minted files"

# A second run into the filled directory is refused.
if ATTESTOR_CERT_DIR=$work/certs MODULE_REGISTRY_FILE=$root/interop/deploy/gateway/module-registry.example.json \
	"$root/tools/bringup/mint-secrets.sh" "$out" >/dev/null 2>&1; then
	fail "tool overwrote an existing mint"
fi
echo "PASS mint-secrets"
