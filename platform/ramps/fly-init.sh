#!/bin/sh
# Init of the reference ramp app on Fly (platform/ramps/fly.toml). Runs as
# root on the machine, as the materialize-private-files init container of
# platform/ramps/deployment.yaml does: copies the [[files]] secrets to
# /run/secrets as uid 4020 with mode 0600, renders the ramp config from the
# app's env and secrets to /run/layerx-ramp/config/config.json, hands the
# journal directory on the volume to uid 4020 and starts the ramp under that
# uid on the plain listener. No secret value is printed.
set -eu
umask 077
source_dir=/run/ramp-source
secrets=/run/secrets
config_dir=/run/layerx-ramp/config
journal_dir=/data/ramp

need() {
	value=""
	eval "value=\${$1:-}"
	if ! printf '%s' "$value" | grep -Eqx "$2"; then
		echo "ramp-fly-init: $1 is unset or malformed" >&2
		exit 2
	fi
}
uint='[0-9]+'
hex32='[0-9a-fA-F]{64}'
url='https://[^ "\\]+'
text='[^"\\]+'
need RAMP_LISTEN '\[::\]:[0-9]+'
need RAMP_WORKER_ID "$text"
need RAMP_LEASE_SECONDS "$uint"
need RAMP_RECONCILE_SECONDS "$uint"
need RAMP_CLIENT_TIMEOUT_SECONDS "$uint"
need RAMP_IDENTITY_ENDPOINT "$url"
need RAMP_IDENTITY_AUDIENCE "$text"
need RAMP_RECEIPT_AUTHORITY_ENDPOINT "$url"
need RAMP_PROTOCOL_VERSION "$uint"
need RAMP_FEE_LIMIT "$uint"
need RAMP_RPC_CHAIN_ID "$uint"
need RAMP_RPC_MINIMUM_AGREEMENT "$uint"
need RAMP_REQUIRED_CONFIRMATIONS "$uint"
need RAMP_POLL_CADENCE_SECONDS "$uint"
need RAMP_DELAYED_AFTER_POLLS "$uint"
need RAMP_OPERATOR_PRINCIPAL_ID "$text"
need RAMP_ACTOR_DID 'did:layerx:[^ "\\:]+'
need RAMP_LAYERX_SIGNER_KEY_HANDLE "$text"
need RAMP_LAYERX_SIGNER_PUBLIC_KEY "$hex32"
need RAMP_PAXEER_SIGNER_KEY_HANDLE "$text"
need RAMP_COMPLIANCE_ENDPOINT "$url"
need RAMP_COMPLIANCE_PUBLIC_KEY "$hex32"
need RAMP_PROVIDER_ENDPOINT "$url"
need RAMP_PROVIDER_CALLBACK_PUBLIC_KEY "$hex32"
need RAMP_CUSTODY_ENDPOINT "$url"
need RAMP_WALLET_ADDRESS "$text"
need RAMP_VAULT_ID "$text"
need RAMP_SIGNER_ENDPOINT "$url"
need RAMP_GATEWAY_ENDPOINT "$url"
need RAMP_NETWORK_ID "$uint"
need RAMP_SEQUENCER_ID "$text"
need RAMP_SEQUENCER_PUBLIC_KEY "$hex32"
need RAMP_SEQUENCER_FIRST_BATCH "$text"
need RAMP_SEQUENCER_LAST_BATCH "$text"
need RAMP_RPC_NAMES '[a-z0-9.-]+ [a-z0-9.-]+'
# shellcheck disable=SC2086 # two validated names, split on purpose
set -- $RAMP_RPC_NAMES
if [ "$1" = "$2" ]; then
	echo "ramp-fly-init: RAMP_RPC_NAMES names one RPC name twice" >&2
	exit 2
fi

for f in quotes.json outbound-ca.pem outbound-identity.p12 outbound-identity-password paxeer-rpc-ca.der \
	identity-token compliance-token provider-token gateway-key receipt-authority-token kms-token \
	paxeer-custody-token operator-control-token; do
	if [ ! -s "$source_dir/$f" ]; then
		echo "ramp-fly-init: $source_dir/$f is missing or empty" >&2
		exit 2
	fi
done
mkdir -p "$secrets" "$config_dir" "$journal_dir"
for f in outbound-ca.pem outbound-identity.p12 outbound-identity-password paxeer-rpc-ca.der identity-token \
	compliance-token provider-token gateway-key receipt-authority-token kms-token paxeer-custody-token \
	operator-control-token; do
	cp "$source_dir/$f" "$secrets/$f"
done

jq -n --slurpfile quotes "$source_dir/quotes.json" --arg rpc1 "https://$1" --arg rpc2 "https://$2" --arg s "$secrets" \
	--arg journal "$journal_dir/journal.jsonl" '{
	listen: env.RAMP_LISTEN,
	listener: "plain",
	journal_path: $journal,
	worker_id: env.RAMP_WORKER_ID,
	lease_seconds: (env.RAMP_LEASE_SECONDS | tonumber),
	reconcile_seconds: (env.RAMP_RECONCILE_SECONDS | tonumber),
	operator: {
		principal_id: env.RAMP_OPERATOR_PRINCIPAL_ID,
		account: ("agent:" + env.RAMP_ACTOR_DID + ":main"),
		signer_key_handle: env.RAMP_LAYERX_SIGNER_KEY_HANDLE
	},
	quotes: $quotes[0],
	client_tls: {
		ca_pem: ($s + "/outbound-ca.pem"),
		identity_pkcs12: ($s + "/outbound-identity.p12"),
		identity_password_file: ($s + "/outbound-identity-password"),
		timeout_seconds: (env.RAMP_CLIENT_TIMEOUT_SECONDS | tonumber)
	},
	identity: {endpoint: env.RAMP_IDENTITY_ENDPOINT, service_token_file: ($s + "/identity-token"), audience: env.RAMP_IDENTITY_AUDIENCE},
	compliance: {endpoint: env.RAMP_COMPLIANCE_ENDPOINT, service_token_file: ($s + "/compliance-token"), public_key: env.RAMP_COMPLIANCE_PUBLIC_KEY},
	provider: {
		endpoint: env.RAMP_PROVIDER_ENDPOINT,
		credential_file: ($s + "/provider-token"),
		settlement_path: "/layerx-ramp-v1/settlements",
		status_path: "/layerx-ramp-v1/settlements"
	},
	layerx: {
		gateway_endpoint: env.RAMP_GATEWAY_ENDPOINT,
		receipt_authority_endpoint: env.RAMP_RECEIPT_AUTHORITY_ENDPOINT,
		signer_endpoint: env.RAMP_SIGNER_ENDPOINT,
		gateway_key_file: ($s + "/gateway-key"),
		authority_token_file: ($s + "/receipt-authority-token"),
		signer_token_file: ($s + "/kms-token"),
		actor_did: env.RAMP_ACTOR_DID,
		protocol_version: (env.RAMP_PROTOCOL_VERSION | tonumber),
		network_id: (env.RAMP_NETWORK_ID | tonumber),
		fee_limit: (env.RAMP_FEE_LIMIT | tonumber),
		signer_public_key: env.RAMP_LAYERX_SIGNER_PUBLIC_KEY,
		sequencer_id: env.RAMP_SEQUENCER_ID,
		sequencer_public_key: env.RAMP_SEQUENCER_PUBLIC_KEY,
		sequencer_first_batch: env.RAMP_SEQUENCER_FIRST_BATCH,
		sequencer_last_batch: env.RAMP_SEQUENCER_LAST_BATCH
	},
	paxeer: {
		custody_endpoint: env.RAMP_CUSTODY_ENDPOINT,
		custody_credential_file: ($s + "/paxeer-custody-token"),
		broadcast_path: "/layerx-paxeer-v1/rebalances",
		status_path: "/layerx-paxeer-v1/rebalances",
		operator_account: ("agent:" + env.RAMP_ACTOR_DID + ":main"),
		wallet_address: env.RAMP_WALLET_ADDRESS,
		vault_id: env.RAMP_VAULT_ID,
		signer_key_handle: env.RAMP_PAXEER_SIGNER_KEY_HANDLE,
		rpc_endpoints: [$rpc1, $rpc2],
		rpc_trust_anchor_der: ($s + "/paxeer-rpc-ca.der"),
		rpc_chain_id: (env.RAMP_RPC_CHAIN_ID | tonumber),
		rpc_minimum_agreement: (env.RAMP_RPC_MINIMUM_AGREEMENT | tonumber),
		required_confirmations: (env.RAMP_REQUIRED_CONFIRMATIONS | tonumber),
		poll_cadence_seconds: (env.RAMP_POLL_CADENCE_SECONDS | tonumber),
		delayed_after_polls: (env.RAMP_DELAYED_AFTER_POLLS | tonumber)
	},
	provider_callback_public_key: env.RAMP_PROVIDER_CALLBACK_PUBLIC_KEY,
	operator_control_token_file: ($s + "/operator-control-token")
}' >"$config_dir/config.json.new"
mv "$config_dir/config.json.new" "$config_dir/config.json"
chown -R 4020:4020 "$secrets" "$config_dir" "$journal_dir"
chmod 0600 "$secrets"/* "$config_dir/config.json"
chmod 0700 "$secrets" "$config_dir" "$journal_dir"
chmod 0711 /run/layerx-ramp
exec setpriv --reuid=4020 --regid=4020 --clear-groups --no-new-privs \
	/usr/local/bin/layerx-reference-ramp "$config_dir/config.json"
