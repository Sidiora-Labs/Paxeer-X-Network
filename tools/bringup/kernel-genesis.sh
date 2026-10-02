#!/bin/bash
# Runs as root inside the kernel app's machine (human/wallet/deploy/human.toml)
# through flyctl ssh console, after docker/kernel/init.sh has made the volume
# layout and the treasury key. The kernel genesis is made in two steps around
# the governance step on chain 125:
#
#   kernel-genesis.sh keys [rotate]
#       step A: generates the sequencer key, the checkpoint authority key, the
#       deposit authority key and the publication recipient key under
#       /data/layerx/keys and prints their public values, which the custody
#       governance proposals of tools/bringup/custody-governance.sh carry.
#   kernel-genesis.sh genesis COMET=HOST SID=POINTER USDC=POINTER USDL=POINTER [SYMBOL=POINTER...]
#       step C, once governance has set the custody asset map and the deposit
#       root authority: reads PAX from nativeAssetId() and every pointer from
#       assetByPointer() through the pod's paxeer relay, writes the LXGB v2
#       genesis metadata with one canonical Asset record per mapped asset, the
#       treasury recipient-binding policy and the guarantor's publication
#       authorization, the PAX custody profile that layerx-custody-proof
#       light-profile builds from the chain 125 Comet RPC at
#       https://HOST/comet (the opening credit of the first-credit path verifies
#       against it, bootstrap.sh --custody-profile) with that RPC URL beside
#       it, and the kernel asset id (the PAX record) and replica id the init
#       reads. The init then runs bootstrap.sh, whose
#       layerx-genesis-build signs the manifest under /data/layerx/node; this
#       step waits for it and prints the digests and the asset ids.
#       Precondition: every registered asset id is the derivation
#       sha256("layerx-asset:125:<SYMBOL>"); the step B proposer chooses the id
#       and this step refuses a registered id that differs from it.
#
# Nothing secret is printed. rotate discards the kernel keys, the genesis
# outputs and the node data.
set -euo pipefail
umask 077

layerx=/data/layerx
node_data=$layerx/node
keys=$layerx/keys
genesis=$layerx/genesis
network_id=${LAYERX_NODE_NETWORK_ID:?the kernel network id comes from the app env}
rpc=http://127.0.0.1:${LAYERX_NODE_PAXEER_RELAY_PORT:-18545}
custody=0x0000000000000000000000000000000000001013
deposit_key=$keys/checkpoint-submitter/deposit-authority.pem
key_files=("$keys/sequencer.key" "$keys/checkpoint-authority/key.pem" "$deposit_key" "$keys/publication/recipient.key")
genesis_files=("$keys/publication/binding-policy.json" "$keys/publication/authorization.json"
	"$genesis/metadata.lxgb" "$genesis/custody.profile" "$genesis/comet-url" "$genesis/asset-id" "$genesis/replica-id")

fail() {
	printf 'kernel-genesis: %s\n' "$*" >&2
	exit 1
}

[ "$(id -u)" = 0 ] || fail "run as root inside the kernel machine"
[ -s "$keys/treasury.key" ] || fail "the init has not made $keys/treasury.key yet"

hex_public() {
	python3 -c 'import sys; sys.stdout.buffer.write(bytes.fromhex("302e020100300506032b657004220420" + sys.argv[1]))' "$1" |
		openssl pkey -inform DER -pubout -outform DER | tail -c 32 | od -An -tx1 | tr -d ' \n'
}
pem_public() { openssl pkey -in "$1" -pubout -outform DER | tail -c 32 | od -An -tx1 | tr -d ' \n'; }
recipient_address() { python3 /opt/layerx/paxeer/evm.py address "$keys/publication/recipient.key" | tr 'A-F' 'a-f'; }

treasury_public=$(hex_public "$(tr -d ' \r\n' <"$keys/treasury.key")")

# public_values: the step A outputs, read back from the key files.
public_values() {
	sequencer_public=$(hex_public "$(cat "$keys/sequencer.key")")
	replica_id=$(printf 'layerx-authority-replica:%s' "$sequencer_public" | sha256sum | cut -d' ' -f1)
	recipient=$(recipient_address)
	recipient=${recipient#0x}
}

keys_step() {
	local file key
	if [ "${1:-}" = rotate ]; then
		rm -f "${key_files[@]}" "${genesis_files[@]}" "$keys/checkpoint-authority/key.pem.lock" \
			"$layerx/guarantor-submitter/checkpoint-authority.pem"
		find "$node_data" -mindepth 1 -delete 2>/dev/null || true
	elif [ -n "${1:-}" ]; then
		fail "usage: kernel-genesis.sh keys [rotate]"
	else
		for file in "${key_files[@]}"; do
			[ ! -e "$file" ] || fail "$file exists; pass rotate to replace the kernel keys"
		done
	fi
	# The sequencer seed, 64 hex characters as bootstrap.sh reads it, owned by the node user.
	install -o 4020 -g 4020 -m 0600 /dev/null "$keys/sequencer.key"
	openssl rand -hex 32 | tr -d '\n' >"$keys/sequencer.key"
	python3 /opt/layerx/checkpoint-authority.py "$keys/checkpoint-authority/key.pem" >/dev/null ||
		fail "checkpoint authority provisioning refused"
	# The deposit authority signs deposit roots for the guarantor, which opens
	# it as its own 0600 file (cmd/layerx-guarantor/authorization.py).
	install -o 4021 -g 4020 -m 0600 /dev/null "$deposit_key"
	openssl genpkey -algorithm ED25519 >"$deposit_key"
	# The publication recipient: an EVM key of the pod's evm.py.
	while :; do
		key="0x$(openssl rand -hex 32)"
		python3 /opt/layerx/paxeer/evm.py address /dev/stdin <<<"$key" >/dev/null 2>&1 && break
	done
	printf '%s' "$key" >"$keys/publication/recipient.key"
	unset key
	public_values
	echo "network_id=$network_id"
	echo "sequencer_public_key=$sequencer_public"
	echo "sequencer_id=$(printf 'layerx-sequencer:%s' "$sequencer_public" | sha256sum | cut -d' ' -f1)"
	echo "replica_id=$replica_id"
	echo "treasury_public_key=$treasury_public"
	echo "checkpoint_authority_public_key=$(pem_public "$keys/checkpoint-authority/key.pem")"
	echo "deposit_authority_public_key=$(pem_public "$deposit_key")"
	echo "publication_recipient=0x$recipient"
	echo "custody keys=$keys next=custody-governance.sh then kernel-genesis.sh genesis"
}

# call <data>: the eth_call answer of the custody precompile, or of $to.
call() {
	curl -fsS -m 15 "$rpc" -H 'content-type: application/json' \
		-d "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"eth_call\",\"params\":[{\"to\":\"${to:-$custody}\",\"data\":\"$1\"},\"latest\"]}" |
		jq -er '.result'
}

word() { printf '%064s' "${1#0x}" | tr ' ' 0; }
# erc20 <pointer> <selector>: the decoded symbol (95d89b41) or decimals (313ce567).
erc20() {
	to=$1 call "0x$2" | python3 -c '
import re
import sys
encoded = sys.stdin.read().strip()
data = bytes.fromhex(encoded[2:])
if sys.argv[1] == "313ce567":
    print(int.from_bytes(data[:32], "big"))
else:
    if re.fullmatch(r"0x(?:[0-9a-fA-F]{2})+", encoded) is None:
        raise SystemExit("symbol() returned invalid hex data")
    if len(data) == 32:
        symbol = data.rstrip(b"\0")
    else:
        if len(data) < 96 or int.from_bytes(data[:32], "big") != 32:
            raise SystemExit("symbol() returned an invalid ABI offset")
        size = int.from_bytes(data[32:64], "big")
        padded_size = ((size + 31) // 32) * 32
        if len(data) != 64 + padded_size or any(data[64 + size:]):
            raise SystemExit("symbol() returned invalid ABI length or padding")
        symbol = data[64:64 + size]
    if not 0 < len(symbol) <= 16 or any(byte < 33 or byte > 126 for byte in symbol):
        raise SystemExit("symbol() must contain 1 to 16 printable ASCII bytes")
    print(symbol.decode("ascii"))
' "$2"
}

# asset_id <SYMBOL>: the asset id step B registers for the symbol,
# sha256("layerx-asset:125:<SYMBOL>").
asset_id() {
	printf 'layerx-asset:125:%s' "$1" | sha256sum | cut -d' ' -f1
}

genesis_step() {
	local argument file symbol pointer id onchain decimals pax manifest record comet="" height
	local -A pointers=()
	local -a records=()
	for argument in "$@"; do
		if [[ $argument =~ ^COMET=([a-z0-9.-]+)$ ]]; then
			comet=https://${BASH_REMATCH[1]}/comet
			continue
		fi
		[[ $argument =~ ^([A-Z0-9]{1,16})=(0x[0-9a-fA-F]{40})$ ]] || fail "not SYMBOL=POINTER: $argument"
		pointers[${BASH_REMATCH[1]}]=${BASH_REMATCH[2]}
	done
	for symbol in SID USDC USDL; do
		[ -n "${pointers[$symbol]:-}" ] || fail "the pointer of $symbol is required"
	done
	[ -n "$comet" ] || fail "COMET=HOST, the chain 125 archive name serving /comet, is required"
	command -v layerx-custody-proof >/dev/null || fail "layerx-custody-proof is not in the kernel image"
	for file in "${key_files[@]}"; do
		[ -s "$file" ] || fail "$file is absent; run kernel-genesis.sh keys first"
	done
	for file in "${genesis_files[@]}"; do
		[ ! -e "$file" ] || fail "$file exists; run kernel-genesis.sh keys rotate to replace the kernel genesis"
	done
	[ "$(curl -fsS -m 15 "$rpc" -H 'content-type: application/json' \
		-d '{"jsonrpc":"2.0","id":1,"method":"eth_chainId","params":[]}' | jq -r .result)" = 0x7d ] ||
		fail "the paxeer relay at $rpc does not answer chain 125"
	id=$(call 0xb4768600) || fail "eth_call depositRootAuthority() failed"
	[ "${id#0x}" = "$(pem_public "$deposit_key")" ] ||
		fail "the deposit root authority on chain 125 is not this deposit authority: eth_call $custody depositRootAuthority() answered $id"

	pax=$(call 0xaafcde84) || fail "eth_call nativeAssetId() failed"
	pax=${pax#0x}
	if ! [[ $pax =~ ^[0-9a-f]{64}$ ]] || [ "$pax" = "$(word 0)" ]; then
		fail "custody asset map has no PAX record: eth_call $custody nativeAssetId() answered 0x$pax"
	fi
	[ "$pax" = "$(asset_id PAX)" ] ||
		fail "the PAX asset id 0x$pax is not sha256(\"layerx-asset:125:PAX\") 0x$(asset_id PAX)"
	records+=("$pax:PAX:6")

	# The PAX custody profile of the opening credit: a light-client profile
	# trusting the Comet header one below the latest, built from the chain.
	height=$(curl -fsS -m 15 "$comet" -H 'content-type: application/json' \
		-d '{"jsonrpc":"2.0","id":1,"method":"status","params":{}}' |
		jq -er '(.result // .).sync_info.latest_block_height | tonumber') || fail "the Comet RPC at $comet did not answer status"
	layerx-custody-proof light-profile --rpc "$comet" --asset "0x$pax" --network-id "$network_id" \
		--trusted-height "$((height - 1))" --trusting-period-seconds 1209600 --output "$genesis/custody.profile" >/dev/null ||
		fail "layerx-custody-proof light-profile refused the chain 125 custody state at height $((height - 1))"
	[ "$(stat -c %s "$genesis/custody.profile")" = 223 ] || fail "the custody profile is not 223 bytes"
	printf '%s\n' "$comet" >"$genesis/comet-url"
	chown 4020:4020 "$genesis/custody.profile" "$genesis/comet-url"
	chmod 0444 "$genesis/custody.profile" "$genesis/comet-url"
	for symbol in "${!pointers[@]}"; do
		pointer=${pointers[$symbol]}
		id=$(call "0xca65021c$(word "$pointer")") || fail "eth_call assetByPointer($pointer) failed"
		id=${id#0x}
		[ "$id" != "$(word 0)" ] ||
			fail "custody asset map has no $symbol record: eth_call $custody assetByPointer($pointer) answered 0x$id"
		[ "$id" = "$(asset_id "$symbol")" ] ||
			fail "the $symbol asset id 0x$id is not sha256(\"layerx-asset:125:$symbol\") 0x$(asset_id "$symbol")"
		onchain=$(erc20 "$pointer" 95d89b41) || fail "symbol() of $pointer failed"
		[ "$onchain" = "$symbol" ] || fail "$pointer is $onchain, not $symbol"
		decimals=$(erc20 "$pointer" 313ce567) || fail "decimals() of $pointer failed"
		records+=("$id:$symbol:$decimals")
	done
	public_values

	# The treasury signer's recipient-binding policy and the guarantor's
	# publication authorization, in the shapes platform/hosted/tests/publication-policy.py
	# writes for the beta cluster, with the kernel's socket paths and uids.
	printf '{"asset_id": "%s", "network_id": %s, "recipient": "%s", "version": 1}\n' "$pax" "$network_id" "$recipient" \
		>"$keys/publication/binding-policy.json"
	python3 - "$keys/publication/authorization.json" "$network_id" "$pax" "$recipient" "$treasury_public" "$deposit_key" <<'PY'
import json
import os
import sys

output, network, asset, recipient, public, deposit = sys.argv[1:]
anchor, vault = '0' * 36 + '1014', '0' * 36 + '1013'
peers = dict(peer_uid=4020, peer_gid=4020)
value = dict(version=1, network_id=int(network), chain_id=125, settlement_contract=anchor,
             checkpoint_registry=anchor, vault=vault, custody_reference='00' * 12 + vault,
             treasury=dict(socket='/run/layerx/node/treasury-signer.sock', **peers, public_key=public,
                           asset_id=asset, recipient=recipient),
             human=dict(socket='/run/layerx/human/recipient.sock', **peers),
             deposit_authority_key_file=deposit)
with os.fdopen(os.open(output, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600), 'w') as handle:
    json.dump(value, handle, sort_keys=True)
    handle.write('\n')
PY

	# The LXGB v2 metadata: the record layout bootstrap.sh writes for one asset,
	# one record per mapped asset, issued by the treasury identity, then the zero
	# fee schedule that bootstrap's --withdrawal-fee and --module-fees complete.
	python3 - "$genesis/metadata.lxgb" "$treasury_public" "${records[@]}" <<'PY'
import hashlib
import os
import sys

output, issuer_public = sys.argv[1], bytes.fromhex(sys.argv[2])
did = ('did:layerx:' + issuer_public.hex()).encode()
issuer = hashlib.sha256(b'LXP/v1/did-id\0' + len(did).to_bytes(2, 'big') + did).digest()
body = b''
for entry in sys.argv[3:]:
    asset, symbol, decimals = entry.split(':')
    asset, symbol, decimals = bytes.fromhex(asset), symbol.encode('ascii'), int(decimals)
    if not 0 < len(symbol) <= 16 or not 0 <= decimals <= 38:
        raise SystemExit('asset symbol must be 1 to 16 ASCII bytes and decimals 0..38')
    reference = bytes(12) + asset[12:]
    record = (b'\0\x03' + asset + len(symbol).to_bytes(1, 'big') + symbol + decimals.to_bytes(1, 'big')
              + b'\x02' + len(reference).to_bytes(2, 'big') + reference
              + b'\0\x0dCustody token' + bytes(16) + issuer + b'\x02' + bytes(16) + os.urandom(32))
    body += len(record).to_bytes(2, 'big') + record
schedule = b'\0\x02' + bytes(80) + (10000).to_bytes(4, 'big') + b'\x0a' + bytes(160)
metadata = (len(sys.argv) - 3).to_bytes(2, 'big') + body + len(schedule).to_bytes(2, 'big') + schedule
with os.fdopen(os.open(output, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600), 'wb') as handle:
    handle.write(metadata)
    handle.flush()
    os.fsync(handle.fileno())
PY
	chown 4020:4020 "$genesis/metadata.lxgb"

	# The ids the init hands layerxd; written last, so the init starts on a complete set.
	printf '%s\n' "$pax" >"$genesis/asset-id"
	printf '%s\n' "$replica_id" >"$genesis/replica-id"
	chmod 0444 "$genesis/asset-id" "$genesis/replica-id"

	manifest=$node_data/genesis/genesis.manifest
	for _ in $(seq 120); do
		[ -s "$manifest" ] && break
		sleep 5
	done
	[ -s "$manifest" ] || fail "layerxd bootstrap wrote no $manifest within ten minutes; see /run/layerx/init/layerxd"

	echo "network_id=$network_id"
	echo "genesis_sha256=$(sha256sum "$manifest" | cut -d' ' -f1)"
	echo "metadata_sha256=$(sha256sum "$genesis/metadata.lxgb" | cut -d' ' -f1)"
	echo "custody_profile_sha256=$(sha256sum "$genesis/custody.profile" | cut -d' ' -f1)"
	echo "sequencer_public_key=$sequencer_public"
	echo "replica_id=$replica_id"
	echo "publication_recipient=0x$recipient"
	for record in "${records[@]}"; do
		IFS=: read -r id symbol decimals <<<"$record"
		echo "asset $symbol id=$id decimals=$decimals"
	done
	echo "custody node=$node_data keys=$keys genesis=$genesis/metadata.lxgb digests=genesis_sha256,metadata_sha256"
}

case "${1:-}" in
keys) keys_step "${@:2}" ;;
genesis) genesis_step "${@:2}" ;;
*) fail "usage: kernel-genesis.sh keys [rotate] | genesis COMET=HOST SID=POINTER USDC=POINTER USDL=POINTER [SYMBOL=POINTER...]" ;;
esac
