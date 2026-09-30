#!/bin/bash
# Runs as root inside the kernel app's machine (human/wallet/deploy/human.toml)
# through flyctl ssh console, after docker/kernel/init.sh has made the volume
# layout and the treasury key. It reads the custody precompile's asset map on
# chain 125 through the pod's paxeer relay, writes the LXGB v2 genesis metadata
# with one canonical Asset record per mapped asset, generates the sequencer key,
# the checkpoint authority key and the publication recipient, and writes the
# kernel asset id (the PAX record) and the replica id the init reads. The init
# then runs bootstrap.sh, whose layerx-genesis-build signs the genesis manifest
# under /data/layerx/node; this script waits for that manifest and prints only
# public values: the network id, the digests, the public keys and the ids.
#
# Usage: kernel-genesis.sh [rotate] SYMBOL=POINTER...
#   SYMBOL=POINTER  an ERC-20 pointer of the custody asset map (SID, USDC and
#                   USDL at least); PAX is read from nativeAssetId().
#   rotate          discards the existing kernel keys, metadata and node data.
set -euo pipefail
umask 077

layerx=/data/layerx
node_data=$layerx/node
keys=$layerx/keys
genesis=$layerx/genesis
network_id=${LAYERX_NODE_NETWORK_ID:?the kernel network id comes from the app env}
rpc=http://127.0.0.1:${LAYERX_NODE_PAXEER_RELAY_PORT:-18545}
custody=0x0000000000000000000000000000000000001013
required="SID USDC USDL"

fail() {
	printf 'kernel-genesis: %s\n' "$*" >&2
	exit 1
}

rotate=0
[ "${1:-}" != rotate ] || {
	rotate=1
	shift
}
declare -A pointers=()
for argument in "$@"; do
	[[ $argument =~ ^([A-Z0-9]{1,16})=(0x[0-9a-fA-F]{40})$ ]] || fail "not SYMBOL=POINTER: $argument"
	pointers[${BASH_REMATCH[1]}]=${BASH_REMATCH[2]}
done
for symbol in $required; do
	[ -n "${pointers[$symbol]:-}" ] || fail "the pointer of $symbol is required"
done
[ "$(id -u)" = 0 ] || fail "run as root inside the kernel machine"
[ -s "$keys/treasury.key" ] || fail "the init has not made $keys/treasury.key yet"

outputs=("$keys/sequencer.key" "$keys/checkpoint-authority/key.pem" "$keys/publication/recipient.key"
	"$keys/publication/binding-policy.json" "$genesis/metadata.lxgb" "$genesis/asset-id" "$genesis/replica-id")
if [ "$rotate" = 1 ]; then
	rm -f "${outputs[@]}" "$keys/checkpoint-authority/key.pem.lock" "$layerx/guarantor-submitter/checkpoint-authority.pem"
	find "$node_data" -mindepth 1 -delete 2>/dev/null || true
else
	for file in "${outputs[@]}"; do
		[ ! -e "$file" ] || fail "$file exists; pass rotate to replace the kernel genesis"
	done
fi

# call <data>: the eth_call answer of the custody precompile, or of $to.
call() {
	curl -fsS -m 15 "$rpc" -H 'content-type: application/json' \
		-d "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"eth_call\",\"params\":[{\"to\":\"${to:-$custody}\",\"data\":\"$1\"},\"latest\"]}" |
		jq -er '.result'
}
[ "$(curl -fsS -m 15 "$rpc" -H 'content-type: application/json' \
	-d '{"jsonrpc":"2.0","id":1,"method":"eth_chainId","params":[]}' | jq -r .result)" = 0x7d ] ||
	fail "the paxeer relay at $rpc does not answer chain 125"

word() { printf '%064s' "${1#0x}" | tr ' ' 0; }
# erc20 <pointer> <selector>: the decoded symbol (95d89b41) or decimals (313ce567).
erc20() {
	to=$1 call "0x$2" | python3 -c '
import sys
data = bytes.fromhex(sys.stdin.read().strip()[2:])
if sys.argv[1] == "313ce567":
    print(int.from_bytes(data[:32], "big"))
else:
    size = int.from_bytes(data[64:96], "big") if len(data) >= 96 else 0
    print(data[96:96 + size].decode("ascii") if size else data[:32].rstrip(b"\0").decode("ascii"))
' "$2"
}

records=()
pax=$(call 0xaafcde84) || fail "eth_call nativeAssetId() failed"
pax=${pax#0x}
[[ $pax =~ ^[0-9a-f]{64}$ ]] && [ "$pax" != "$(word 0)" ] ||
	fail "custody asset map has no PAX record: eth_call $custody nativeAssetId() answered 0x$pax"
records+=("$pax:PAX:18")
for symbol in "${!pointers[@]}"; do
	pointer=${pointers[$symbol]}
	id=$(call "0xca65021c$(word "$pointer")") || fail "eth_call assetByPointer($pointer) failed"
	id=${id#0x}
	[ "$id" != "$(word 0)" ] ||
		fail "custody asset map has no $symbol record: eth_call $custody assetByPointer($pointer) answered 0x$id"
	onchain=$(erc20 "$pointer" 95d89b41) || fail "symbol() of $pointer failed"
	[ "$onchain" = "$symbol" ] || fail "$pointer is $onchain, not $symbol"
	decimals=$(erc20 "$pointer" 313ce567) || fail "decimals() of $pointer failed"
	records+=("$id:$symbol:$decimals")
done

hex_public() {
	python3 -c 'import sys; sys.stdout.buffer.write(bytes.fromhex("302e020100300506032b657004220420" + sys.argv[1]))' "$1" |
		openssl pkey -inform DER -pubout -outform DER | tail -c 32 | od -An -tx1 | tr -d ' \n'
}
treasury_public=$(hex_public "$(tr -d ' \r\n' <"$keys/treasury.key")")

# The sequencer seed, 64 hex characters as bootstrap.sh reads it, owned by the node user.
install -o 4020 -g 4020 -m 0600 /dev/null "$keys/sequencer.key"
openssl rand -hex 32 | tr -d '\n' >"$keys/sequencer.key"
sequencer_public=$(hex_public "$(cat "$keys/sequencer.key")")
replica_id=$(printf 'layerx-authority-replica:%s' "$sequencer_public" | sha256sum | cut -d' ' -f1)

authority_public=$(python3 /opt/layerx/checkpoint-authority.py "$keys/checkpoint-authority/key.pem") ||
	fail "checkpoint authority provisioning refused"

# The publication recipient: an EVM key of the pod's evm.py, and the treasury
# signer's recipient-binding policy for the kernel asset.
while :; do
	key="0x$(openssl rand -hex 32)"
	recipient=$(python3 /opt/layerx/paxeer/evm.py address /dev/stdin <<<"$key" 2>/dev/null) && break
done
printf '%s' "$key" >"$keys/publication/recipient.key"
unset key
recipient=$(printf '%s' "${recipient#0x}" | tr 'A-F' 'a-f')
printf '{"version":1,"network_id":%s,"asset_id":"%s","recipient":"%s"}' "$network_id" "$pax" "$recipient" \
	>"$keys/publication/binding-policy.json"

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
echo "sequencer_public_key=$sequencer_public"
echo "sequencer_id=$(printf 'layerx-sequencer:%s' "$sequencer_public" | sha256sum | cut -d' ' -f1)"
echo "replica_id=$replica_id"
echo "checkpoint_authority_public_key=${authority_public#0x}"
echo "publication_recipient=0x$recipient"
for record in "${records[@]}"; do
	IFS=: read -r id symbol decimals <<<"$record"
	echo "asset $symbol id=$id decimals=$decimals"
done
echo "custody node=$node_data keys=$keys genesis=$genesis/metadata.lxgb digests=genesis_sha256,metadata_sha256"
