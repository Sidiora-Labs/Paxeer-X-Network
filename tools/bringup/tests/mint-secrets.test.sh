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
services="wallet-gateway router webhooks-public webhooks-ingress internal-kms internal-journeys internal-payments internal-approvals internal-programs identity dashboard ramp interop"
boxes="paxeer-attestor-1 paxeer-attestor-2 paxeer-attestor-3 paxeer-attestor-4 paxeer-attestor-5 paxeer-human-service paxeer-program-registry"
files=""
for s in $services; do files="$files $s.railway.env $s.railway.sh"; done
for b in $boxes; do files="$files $b.box.env"; done
for f in $files; do
	[ -f "$out/$f" ] || fail "no $f"
	[ "$(stat -c %a "$out/$f")" = 600 ] || fail "$f is not 0600"
	grep -qxF "$out/$f" "$work/stdout" || fail "$f not reported"
done
[ "$(wc -l <"$work/stdout")" -eq 33 ] || fail "tool printed more than the written paths"
[ "$(find "$out" -maxdepth 1 -type f | wc -l)" -eq 33 ] || fail "tool wrote files beyond the Railway and box forms"
for f in $files; do
	case "$f" in *.sh) continue ;; esac
	grep -vqE '^[A-Z][A-Z0-9_]*=[^[:space:]]+$' "$out/$f" && fail "$f holds a line that is not KEY=value"
done
if ls "$out" | grep -qi fly; then fail "a Fly form was written"; fi

# value <file stem> <NAME>
value() { sed -n "s/^$2=//p" "$out/$1"; }
# plain <file stem> <NAME>: the value as its consumer reads it.
plain() { value "$1" "$2" | base64 -d; }

# Every secret a consumer needs is present.
expect() {
	local stem=$1
	shift
	for name in "$@"; do [ -n "$(value "$stem" "$name")" ] || fail "$stem lacks $name"; done
	[ "$(wc -l <"$out/$stem")" -eq "$#" ] || fail "$stem holds more than its secrets"
}
expect wallet-gateway.railway.env WALLET_IDENTITY_BINDING_TENANT WALLET_IDENTITY_BINDING_KEY WALLET_CUSTODY_INVENTORY WALLET_CUSTODY_INVENTORY_PUBLIC_KEY
for i in 1 2 3 4 5; do
	expect "paxeer-attestor-$i.box.env" WALLET_IDENTITY_BINDING_PUBLIC_KEY WALLET_CUSTODY_INVENTORY_PUBLIC_KEY WALLET_CUSTODY_INVENTORY
done
expect paxeer-human-service.box.env HUMAN_EVENTS_JOURNEY_TOKEN HUMAN_EVENTS_APPROVAL_TOKEN HUMAN_EVENTS_WEBHOOKS_TOKEN \
	KERNEL_BACKEND_ADMIN_TOKEN KERNEL_GATEWAY_COMPONENT_TOKEN KERNEL_GATEWAY_AUTHORITY_TOKEN KERNEL_WEBHOOKS_COMPONENT_TOKEN \
	KERNEL_WEBHOOKS_AUTHORITY_TOKEN LAYERX_REGISTRY_NODE_AUTHORIZATION LAYERX_REGISTRY_RECEIPT_AUTHORITY_AUTHORIZATION
expect paxeer-program-registry.box.env REGISTRY_IDENTITY_TOKEN REGISTRY_PROGRAM_EVENTS_TOKEN REGISTRY_WEBHOOKS_EVENTS_TOKEN \
	REGISTRY_REQUEST_TOKEN REGISTRY_PUBLICATION_TOKEN LAYERX_REGISTRY_NODE_AUTHORIZATION LAYERX_REGISTRY_RECEIPT_AUTHORITY_AUTHORIZATION
expect router.railway.env ENDPOINT_EVENTS_PAYMENT_TOKEN ENDPOINT_EVENTS_WEBHOOKS_TOKEN ENDPOINT_COMPONENT_TOKEN \
	ENDPOINT_AUTHORITY_TOKEN ENDPOINT_IDENTITY_TOKEN ENDPOINT_IDENTITY_PROVISIONING_TOKEN ENDPOINT_PROGRAM_REGISTRY_TOKEN \
	ENDPOINT_KEY_PROVISIONING_KEY ENDPOINT_MODULE_REGISTRY
for s in webhooks-public webhooks-ingress; do
	expect "$s.railway.env" WEBHOOKS_KMS_TOKEN WEBHOOKS_IDENTITY_TOKEN WEBHOOKS_COMPONENT_TOKEN WEBHOOKS_AUTHORITY_TOKEN \
		WEBHOOKS_JOURNEY_SOURCE_TOKEN WEBHOOKS_PAYMENT_SOURCE_TOKEN WEBHOOKS_APPROVAL_SOURCE_TOKEN WEBHOOKS_PROGRAM_SOURCE_TOKEN \
		WEBHOOKS_SOURCE_TRIGGER_TOKEN WEBHOOKS_OPERATOR_TOKEN WEBHOOKS_CURSOR_KEY
done
cmp -s "$out/webhooks-public.railway.env" "$out/webhooks-ingress.railway.env" || fail "the webhooks roles hold different secrets"
expect internal-kms.railway.env INTERNAL_KMS_TOKEN INTERNAL_KMS_SEAL_SECRET
for role in JOURNEYS PAYMENTS APPROVALS PROGRAMS; do
	lower=$(printf '%s' "$role" | tr A-Z a-z)
	expect "internal-$lower.railway.env" "INTERNAL_${role}_TOKEN" "INTERNAL_${role}_PRODUCER_TOKEN"
done
expect identity.railway.env IDENTITY_GATEWAY_TOKEN IDENTITY_REGISTRY_TOKEN IDENTITY_WEBHOOKS_TOKEN IDENTITY_DASHBOARD_TOKEN \
	IDENTITY_FAUCET_TOKEN IDENTITY_TESTNET_TOKEN IDENTITY_RAMP_TOKEN IDENTITY_PROVISIONING_TOKEN IDENTITY_REGISTRAR_TOKEN
expect dashboard.railway.env DASHBOARD_IDENTITY_TOKEN
expect ramp.railway.env RAMP_IDENTITY_TOKEN
expect interop.railway.env INTEROP_AUTHORITY_TOKEN

# Each minted token, decoded as its consumer reads it, is one 32-byte hex
# value shared by every end.
hex() { grep -qxE '[0-9a-f]{64}' <<<"$1" || fail "$2 is not 32-byte hex"; }
same() {
	local first=$1 v
	shift
	hex "$first" "the shared token"
	for v in "$@"; do [ "$v" = "$first" ] || fail "paired ends differ"; done
}
same "$(plain paxeer-human-service.box.env HUMAN_EVENTS_JOURNEY_TOKEN)" "$(plain internal-journeys.railway.env INTERNAL_JOURNEYS_PRODUCER_TOKEN)"
same "$(plain paxeer-human-service.box.env HUMAN_EVENTS_APPROVAL_TOKEN)" "$(plain internal-approvals.railway.env INTERNAL_APPROVALS_PRODUCER_TOKEN)"
same "$(plain router.railway.env ENDPOINT_EVENTS_PAYMENT_TOKEN)" "$(plain internal-payments.railway.env INTERNAL_PAYMENTS_PRODUCER_TOKEN)"
same "$(value paxeer-program-registry.box.env REGISTRY_PROGRAM_EVENTS_TOKEN)" "$(plain internal-programs.railway.env INTERNAL_PROGRAMS_PRODUCER_TOKEN)"
same "$(plain webhooks-public.railway.env WEBHOOKS_SOURCE_TRIGGER_TOKEN)" "$(plain paxeer-human-service.box.env HUMAN_EVENTS_WEBHOOKS_TOKEN)" \
	"$(plain router.railway.env ENDPOINT_EVENTS_WEBHOOKS_TOKEN)" "$(value paxeer-program-registry.box.env REGISTRY_WEBHOOKS_EVENTS_TOKEN)"
same "$(plain webhooks-public.railway.env WEBHOOKS_KMS_TOKEN)" "$(plain internal-kms.railway.env INTERNAL_KMS_TOKEN)"
for role in JOURNEY:journeys PAYMENT:payments APPROVAL:approvals PROGRAM:programs; do
	same "$(plain webhooks-ingress.railway.env "WEBHOOKS_${role%%:*}_SOURCE_TOKEN")" \
		"$(plain "internal-${role#*:}.railway.env" "INTERNAL_$(printf '%s' "${role#*:}" | tr a-z A-Z)_TOKEN")"
done
hex "$(plain internal-kms.railway.env INTERNAL_KMS_SEAL_SECRET)" INTERNAL_KMS_SEAL_SECRET
same "$(plain identity.railway.env IDENTITY_GATEWAY_TOKEN)" "$(plain router.railway.env ENDPOINT_IDENTITY_TOKEN)"
same "$(plain identity.railway.env IDENTITY_PROVISIONING_TOKEN)" "$(plain router.railway.env ENDPOINT_IDENTITY_PROVISIONING_TOKEN)"
same "$(plain identity.railway.env IDENTITY_WEBHOOKS_TOKEN)" "$(plain webhooks-public.railway.env WEBHOOKS_IDENTITY_TOKEN)"
same "$(plain identity.railway.env IDENTITY_DASHBOARD_TOKEN)" "$(plain dashboard.railway.env DASHBOARD_IDENTITY_TOKEN)"
same "$(plain identity.railway.env IDENTITY_RAMP_TOKEN)" "$(plain ramp.railway.env RAMP_IDENTITY_TOKEN)"
same "$(plain identity.railway.env IDENTITY_REGISTRY_TOKEN)" "$(value paxeer-program-registry.box.env REGISTRY_IDENTITY_TOKEN)"
same "$(plain paxeer-human-service.box.env KERNEL_GATEWAY_COMPONENT_TOKEN)" "$(plain router.railway.env ENDPOINT_COMPONENT_TOKEN)"
same "$(plain paxeer-human-service.box.env KERNEL_GATEWAY_AUTHORITY_TOKEN)" "$(plain router.railway.env ENDPOINT_AUTHORITY_TOKEN)" \
	"$(plain interop.railway.env INTEROP_AUTHORITY_TOKEN)"
same "$(plain paxeer-human-service.box.env KERNEL_WEBHOOKS_COMPONENT_TOKEN)" "$(plain webhooks-ingress.railway.env WEBHOOKS_COMPONENT_TOKEN)"
same "$(plain paxeer-human-service.box.env KERNEL_WEBHOOKS_AUTHORITY_TOKEN)" "$(plain webhooks-ingress.railway.env WEBHOOKS_AUTHORITY_TOKEN)"
same "$(value paxeer-human-service.box.env LAYERX_REGISTRY_NODE_AUTHORIZATION)" "$(value paxeer-program-registry.box.env LAYERX_REGISTRY_NODE_AUTHORIZATION)"
same "$(value paxeer-human-service.box.env LAYERX_REGISTRY_RECEIPT_AUTHORITY_AUTHORIZATION)" \
	"$(value paxeer-program-registry.box.env LAYERX_REGISTRY_RECEIPT_AUTHORITY_AUTHORIZATION)"
same "$(value paxeer-program-registry.box.env REGISTRY_REQUEST_TOKEN)" "$(plain router.railway.env ENDPOINT_PROGRAM_REGISTRY_TOKEN)"
hex "$(value paxeer-program-registry.box.env REGISTRY_PUBLICATION_TOKEN)" REGISTRY_PUBLICATION_TOKEN
[ "$(value paxeer-program-registry.box.env REGISTRY_PUBLICATION_TOKEN)" != "$(value paxeer-program-registry.box.env REGISTRY_REQUEST_TOKEN)" ] ||
	fail "registry request and publication tokens collide"
for i in 2 3 4 5; do
	for name in WALLET_IDENTITY_BINDING_PUBLIC_KEY WALLET_CUSTODY_INVENTORY_PUBLIC_KEY WALLET_CUSTODY_INVENTORY; do
		[ "$(value paxeer-attestor-1.box.env "$name")" = "$(value "paxeer-attestor-$i.box.env" "$name")" ] || fail "attestor-$i $name differs"
	done
done
[ "$(value wallet-gateway.railway.env WALLET_CUSTODY_INVENTORY)" = "$(value paxeer-attestor-1.box.env WALLET_CUSTODY_INVENTORY)" ] ||
	fail "gateway and attestor inventories differ"
[ "$(plain router.railway.env ENDPOINT_KEY_PROVISIONING_KEY)" != "$(plain router.railway.env ENDPOINT_COMPONENT_TOKEN)" ] ||
	fail "independent tokens collide"
hex "$(plain router.railway.env ENDPOINT_KEY_PROVISIONING_KEY)" "key provisioning key"
cmp -s <(plain router.railway.env ENDPOINT_MODULE_REGISTRY) "$root/interop/deploy/gateway/module-registry.example.json" ||
	fail "module registry does not round-trip"

# Each Railway script reads its own dotenv and sets every variable of it on
# standard input, without a deploy.
for s in $services; do
	f=$out/$s.railway.sh
	sh -n "$f" || fail "$f is not valid sh"
	grep -qF -- "--stdin --service $s --environment" "$f" || fail "$f does not set variables of $s on stdin"
	grep -qF -- "--skip-deploys" "$f" || fail "$f deploys"
	grep -qF "/$s.railway.env\"" "$f" || fail "$f does not read its dotenv"
done

# No minted value reached stdout.
for f in $files; do
	case "$f" in *.sh) continue ;; esac
	sed 's/^[A-Z_0-9]*=//' "$out/$f" | while read -r v; do
		if grep -qF -- "$v" "$work/stdout"; then fail "$f value printed"; fi
	done
done

# The gateway's own env schema and inventory verifier accept the minted
# files, decoded as the env-files shim writes them.
gw=$work/gateway
mkdir -m 700 "$gw"
for pair in WALLET_IDENTITY_BINDING_KEY:binding.key WALLET_CUSTODY_INVENTORY:inventory.jwt WALLET_CUSTODY_INVENTORY_PUBLIC_KEY:inventory.pem; do
	value wallet-gateway.railway.env "${pair%%:*}" | base64 -d >"$gw/${pair#*:}"
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
		WALLET_IDENTITY_BINDING_TENANT="$(value wallet-gateway.railway.env WALLET_IDENTITY_BINDING_TENANT)" \
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
