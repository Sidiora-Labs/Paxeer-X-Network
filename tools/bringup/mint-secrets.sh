#!/usr/bin/env bash
set -euo pipefail

usage() {
	cat >&2 <<'EOF'
usage: ATTESTOR_CERT_DIR=<dir> MODULE_REGISTRY_FILE=<file> tools/bringup/mint-secrets.sh <out-dir>

Mints the beta secrets of the wallet, router, human service, webhooks and
internal apps on this host and writes one file per Fly app under <out-dir>
(created with mode 0700, every file 0600) holding a ready
"fly secrets set" command. Values of [[files]] secrets are base64, as Fly
reads them. Nothing secret is printed; stdout lists the written paths.

ATTESTOR_CERT_DIR     attestor-1.crt .. attestor-5.crt, the deployed attestor
                      TLS certificates (PEM); their SPKI SHA-256 pins are the
                      custody inventory members.
MODULE_REGISTRY_FILE  the kernel deployment's module-registry.json.

The issuer and tenant are read from human/wallet/deploy/attestor-*.toml and
must agree across the five attestors. <out-dir>/keys keeps the operator
custody inventory signing key, the only copy; the inventory expires 30 days
after minting and is re-signed with that key.
EOF
	exit 2
}

[ "$#" -eq 1 ] || usage
out=$1
: "${ATTESTOR_CERT_DIR:?ATTESTOR_CERT_DIR is required}"
: "${MODULE_REGISTRY_FILE:?MODULE_REGISTRY_FILE is required}"
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
deploy=$root/human/wallet/deploy

toml_env() { sed -n "s/^  $2 = \"\(.*\)\"$/\1/p" "$1"; }
issuer=$(toml_env "$deploy/attestor-1.toml" ATTESTOR_AUTHORITY_ISSUER)
tenant=$(toml_env "$deploy/attestor-1.toml" ATTESTOR_AUTHORITY_TENANT)
[ -n "$issuer" ] && [ -n "$tenant" ] || { echo "mint-secrets: attestor-1.toml lacks the authority issuer or tenant" >&2; exit 1; }
case "$issuer" in https://*/auth/v1) ;; *) echo "mint-secrets: issuer must be <supabase url>/auth/v1" >&2; exit 1 ;; esac
for i in 2 3 4 5; do
	[ "$(toml_env "$deploy/attestor-$i.toml" ATTESTOR_AUTHORITY_ISSUER)" = "$issuer" ] &&
		[ "$(toml_env "$deploy/attestor-$i.toml" ATTESTOR_AUTHORITY_TENANT)" = "$tenant" ] ||
		{ echo "mint-secrets: attestor-$i.toml disagrees with attestor-1.toml on issuer or tenant" >&2; exit 1; }
done
python3 -c 'import json,sys; json.load(open(sys.argv[1]))' "$MODULE_REGISTRY_FILE" ||
	{ echo "mint-secrets: $MODULE_REGISTRY_FILE is not JSON" >&2; exit 1; }

if [ -e "$out" ] && [ -n "$(ls -A "$out")" ]; then
	echo "mint-secrets: $out is not empty; refusing to replace minted secrets" >&2
	exit 1
fi
umask 077
mkdir -p "$out/keys"
chmod 0700 "$out" "$out/keys"
keys=$out/keys

pins=()
for i in 1 2 3 4 5; do
	pin=$(openssl x509 -in "$ATTESTOR_CERT_DIR/attestor-$i.crt" -pubkey -noout | openssl pkey -pubin -outform DER | sha256sum | cut -d' ' -f1)
	[ "${#pin}" -eq 64 ] || { echo "mint-secrets: cannot pin attestor-$i.crt" >&2; exit 1; }
	pins+=("$pin")
done

openssl ecparam -name prime256v1 -genkey -noout | openssl pkcs8 -topk8 -nocrypt -out "$keys/wallet_identity_binding_key.pem"
openssl pkey -in "$keys/wallet_identity_binding_key.pem" -pubout -out "$keys/wallet_identity_binding_public.pem"
openssl ecparam -name prime256v1 -genkey -noout | openssl pkcs8 -topk8 -nocrypt -out "$keys/wallet_custody_inventory_signer.pem"
openssl pkey -in "$keys/wallet_custody_inventory_signer.pem" -pubout -out "$keys/wallet_custody_inventory_authority.pem"

# The inventory token has the shape the gateway (src/agent/authority.ts) and
# the attestors (internal/server/authority.go) verify: ES256 over
# header.payload, P1363 signature, members pinned to the attestor SPKIs.
node - "$keys/wallet_custody_inventory_signer.pem" "$issuer" "$tenant" "${pins[@]}" >"$keys/wallet_custody_inventory.jwt" <<'EOF'
const { createPrivateKey, sign } = require('node:crypto');
const { readFileSync } = require('node:fs');
const [key, iss, tenant, ...pins] = process.argv.slice(2);
const iat = Math.floor(Date.now() / 1000) - 60;
const payload = { version: 1, iss, aud: 'wallet-custody-inventory', tenant, sequence: '1', iat, exp: iat + 2_592_000,
  protocol: 'wallet', threshold: 3, members: pins.map((spki_sha256, i) => ({ id: String(i + 1), spki_sha256 })), keys: [] };
const enc = (v) => Buffer.from(JSON.stringify(v)).toString('base64url');
const input = `${enc({ alg: 'ES256', typ: 'wallet-custody-inventory+jwt' })}.${enc(payload)}`;
const signature = sign('sha256', Buffer.from(input, 'ascii'), { key: createPrivateKey(readFileSync(key)), dsaEncoding: 'ieee-p1363' });
process.stdout.write(`${input}.${signature.toString('base64url')}\n`);
EOF

# 32-byte hex values, the format platform/hosted/internal/fly-init.sh mints
# and the router's parse_hex32 reads.
rand() { openssl rand -hex 32; }
journey=$(rand) approval=$(rand) payment=$(rand) program_producer=$(rand) trigger=$(rand)
kms=$(rand) seal=$(rand)
journey_source=$(rand) payment_source=$(rand) approval_source=$(rand) program_source=$(rand)

b64() { base64 -w0; }
b64v() { printf '%s' "$1" | b64; }
b64f() { b64 <"$1"; }

# block <app> NAME=value...: one ready command per app file, 0600.
block() {
	local app=$1 file=$out/$1.fly-secrets
	shift
	{
		printf 'fly secrets set --app %s' "$app"
		printf ' \\\n  %s' "$@"
		printf '\n'
	} >"$file"
	chmod 0600 "$file"
	echo "$file"
}

block paxeer-wallet-gateway \
	"WALLET_IDENTITY_BINDING_TENANT=$tenant" \
	"WALLET_IDENTITY_BINDING_KEY=$(b64f "$keys/wallet_identity_binding_key.pem")" \
	"WALLET_CUSTODY_INVENTORY=$(b64f "$keys/wallet_custody_inventory.jwt")" \
	"WALLET_CUSTODY_INVENTORY_PUBLIC_KEY=$(b64f "$keys/wallet_custody_inventory_authority.pem")"

for i in 1 2 3 4 5; do
	block "paxeer-attestor-$i" \
		"WALLET_IDENTITY_BINDING_PUBLIC_KEY=$(b64f "$keys/wallet_identity_binding_public.pem")" \
		"WALLET_CUSTODY_INVENTORY_PUBLIC_KEY=$(b64f "$keys/wallet_custody_inventory_authority.pem")" \
		"WALLET_CUSTODY_INVENTORY=$(b64f "$keys/wallet_custody_inventory.jwt")"
done

block paxeer-human-service \
	"HUMAN_EVENTS_JOURNEY_TOKEN=$(b64v "$journey")" \
	"HUMAN_EVENTS_APPROVAL_TOKEN=$(b64v "$approval")" \
	"HUMAN_EVENTS_WEBHOOKS_TOKEN=$(b64v "$trigger")"

block paxeer-shared-endpoint \
	"ENDPOINT_EVENTS_PAYMENT_TOKEN=$(b64v "$payment")" \
	"ENDPOINT_EVENTS_WEBHOOKS_TOKEN=$(b64v "$trigger")" \
	"ENDPOINT_COMPONENT_TOKEN=$(b64v "$(rand)")" \
	"ENDPOINT_AUTHORITY_TOKEN=$(b64v "$(rand)")" \
	"ENDPOINT_IDENTITY_TOKEN=$(b64v "$(rand)")" \
	"ENDPOINT_IDENTITY_PROVISIONING_TOKEN=$(b64v "$(rand)")" \
	"ENDPOINT_PROGRAM_REGISTRY_TOKEN=$(b64v "$(rand)")" \
	"ENDPOINT_KEY_PROVISIONING_KEY=$(b64v "$(rand)")" \
	"ENDPOINT_MODULE_REGISTRY=$(b64f "$MODULE_REGISTRY_FILE")"

block paxeer-webhooks \
	"WEBHOOKS_KMS_TOKEN=$(b64v "$kms")" \
	"WEBHOOKS_IDENTITY_TOKEN=$(b64v "$(rand)")" \
	"WEBHOOKS_COMPONENT_TOKEN=$(b64v "$(rand)")" \
	"WEBHOOKS_AUTHORITY_TOKEN=$(b64v "$(rand)")" \
	"WEBHOOKS_JOURNEY_SOURCE_TOKEN=$(b64v "$journey_source")" \
	"WEBHOOKS_PAYMENT_SOURCE_TOKEN=$(b64v "$payment_source")" \
	"WEBHOOKS_APPROVAL_SOURCE_TOKEN=$(b64v "$approval_source")" \
	"WEBHOOKS_PROGRAM_SOURCE_TOKEN=$(b64v "$program_source")" \
	"WEBHOOKS_SOURCE_TRIGGER_TOKEN=$(b64v "$trigger")" \
	"WEBHOOKS_OPERATOR_TOKEN=$(b64v "$(rand)")" \
	"WEBHOOKS_CURSOR_KEY=$(b64v "$(rand)")"

# The internal app's groups: the kms token and seal secret, each event
# source's consumer token (the webhooks *_SOURCE_TOKEN) and producer token
# (the human service and router upstream tokens).
block paxeer-internal \
	"INTERNAL_KMS_TOKEN=$(b64v "$kms")" \
	"INTERNAL_KMS_SEAL_SECRET=$(b64v "$seal")" \
	"INTERNAL_JOURNEYS_TOKEN=$(b64v "$journey_source")" \
	"INTERNAL_JOURNEYS_PRODUCER_TOKEN=$(b64v "$journey")" \
	"INTERNAL_PAYMENTS_TOKEN=$(b64v "$payment_source")" \
	"INTERNAL_PAYMENTS_PRODUCER_TOKEN=$(b64v "$payment")" \
	"INTERNAL_APPROVALS_TOKEN=$(b64v "$approval_source")" \
	"INTERNAL_APPROVALS_PRODUCER_TOKEN=$(b64v "$approval")" \
	"INTERNAL_PROGRAMS_TOKEN=$(b64v "$program_source")" \
	"INTERNAL_PROGRAMS_PRODUCER_TOKEN=$(b64v "$program_producer")"
