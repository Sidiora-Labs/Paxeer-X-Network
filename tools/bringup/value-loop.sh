#!/bin/bash
# Runs as root inside the kernel app's machine (human/wallet/deploy/human.toml);
# tools/bringup/check-live.sh kernel-value-loop pipes it to bash -s through
# flyctl ssh console. It proves the value loop of the kernel:
#
#   value-loop.sh [DEPOSIT_TX=0x<hash>] [CHECKPOINT_SECONDS=<n>]
#
# 1. Preconditions, each a single "precondition <name> <detail>" line and exit
#    3 when absent: the genesis ids (asset-id, replica-id) and the custody
#    profile of tools/bringup/kernel-genesis.sh genesis, the publication policy
#    on the volume, the sequencer's LNI socket, and the receipt authority
#    answering /readyz on 9445 (task 2.1).
# 2. Two accounts, sender and recipient: 32-byte Ed25519 seeds generated on
#    first run under /data/layerx/keys/value-loop; only their DIDs and main
#    account ids are printed.
# 3. The asset is the PAX record: the genesis asset id, which must equal
#    nativeAssetId() of the custody precompile read through the pod's relay.
# 4. The sender is funded by the first-credit path of the node test: while its
#    main account is unopened (-208) the owner's funded depositor sends
#    deposit(bytes32 <sender main id>) to the custody precompile on chain 125
#    and passes the transaction as DEPOSIT_TX; the script proves the deposit
#    with layerx-custody-proof light-credit against the genesis custody
#    profile, signs the credit with sign-credit and submits it with layerxctl.
# 5. The sender SENDs one unit of PAX to the recipient (layerx-node-probe
#    write-send, layerxctl submit over the LNI); the signed SEND and its
#    activity id are kept under /data/layerx/value-loop, so a rerun proves the
#    same activity instead of sending again.
# 6. Balances of both main accounts through layerx-node-probe balance, the
#    batch carrying the activity through the receipt authority's
#    /v1/authorized-batches/by-activity route, and statusOf on the anchor
#    precompile for a sealed batch at or after it, 1 submitted or 2 final.
#
# Output lines: account, asset, credit, activity, balance, batch, checkpoint;
# exit 0 when every step answered, 1 when a step failed, 3 on a precondition.
# Nothing secret is printed.
set -euo pipefail
umask 077

layerx=${LAYERX_KERNEL_DATA:-/data/layerx}
layerx=${layerx%/}
run=${LAYERX_KERNEL_RUN:-/run/layerx}
run=${run%/}
tls=${LAYERX_KERNEL_TLS:-/data/tls}
tls=${tls%/}
keys=$layerx/keys
genesis=$layerx/genesis
state=$layerx/value-loop
lni=$run/node/layerxd.lni.sock
network_id=${LAYERX_NODE_NETWORK_ID:-125}
relay=http://127.0.0.1:${LAYERX_NODE_PAXEER_RELAY_PORT:-18545}
authority=https://127.0.0.1:9445
custody=0x0000000000000000000000000000000000001013
anchor=0x0000000000000000000000000000000000001014
deposit_topic=0x7edb71c9100c656847896d0b5b194f69f7da287eb57964a81e7f807a6a944028

precondition() {
	echo "precondition $*"
	exit 3
}

failed() {
	echo "$*"
	exit 1
}

# as_lni <command...>: runs the command as the LNI client uid of the pod.
as_lni() { setpriv --reuid 4021 --regid 4020 --clear-groups -- "$@"; }

# call <to> <data>: the eth_call answer through the pod's paxeer relay.
call() {
	curl -fsS -m 15 "$relay" -H 'content-type: application/json' \
		-d "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"eth_call\",\"params\":[{\"to\":\"$1\",\"data\":\"$2\"},\"latest\"]}" |
		jq -er '.result'
}

seed_public() {
	od -An -tx1 "$1" | tr -d ' \n' | python3 -c '
import sys
from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey
from cryptography.hazmat.primitives.serialization import Encoding, PublicFormat
key = Ed25519PrivateKey.from_private_bytes(bytes.fromhex(sys.stdin.read().strip()))
print(key.public_key().public_bytes(Encoding.Raw, PublicFormat.Raw).hex())'
}

# main_id <did>: sha256("LX:ACCOUNT:v1" || u32 length || "agent:<did>:main").
main_id() {
	python3 -c '
import hashlib, sys
name = ("agent:" + sys.argv[1] + ":main").encode()
print(hashlib.sha256(b"LX:ACCOUNT:v1" + len(name).to_bytes(4, "big") + name).hexdigest())' "$1"
}

handshake_sealed() {
	as_lni layerx-node-probe handshake --socket "$lni" --network-id "$network_id" 2>/dev/null |
		jq -er '.latest_sealed_batch'
}

balance() {
	as_lni layerx-node-probe balance --socket "$lni" --network-id "$network_id" \
		--account "agent:$1:main" --asset "$asset" 2>/dev/null
}

main() {
	local argument deposit_tx="" checkpoint_seconds=600 file pid bearer="" role seed public did id
	local chain_pax answer amount activity sealed batch status b latest deadline token_files
	for argument in "$@"; do
		case "$argument" in
		DEPOSIT_TX=0x*) deposit_tx=${argument#DEPOSIT_TX=} ;;
		CHECKPOINT_SECONDS=*) checkpoint_seconds=${argument#CHECKPOINT_SECONDS=} ;;
		*) failed "usage value-loop.sh [DEPOSIT_TX=0x<hash>] [CHECKPOINT_SECONDS=<n>]" ;;
		esac
	done
	[[ -z $deposit_tx || $deposit_tx =~ ^0x[0-9a-fA-F]{64}$ ]] || failed "usage DEPOSIT_TX is not a transaction hash"
	[[ $checkpoint_seconds =~ ^[0-9]+$ ]] || failed "usage CHECKPOINT_SECONDS is not a number"
	[ "$(id -u)" = 0 ] || failed "usage run as root inside the kernel machine"

	for file in "$genesis/asset-id" "$genesis/replica-id"; do
		[ -s "$file" ] || precondition genesis-ids "$file absent; run kernel-genesis.sh genesis (step C) first"
	done
	for file in "$genesis/custody.profile" "$genesis/comet-url"; do
		[ -s "$file" ] || precondition custody-profile "$file absent; the genesis was built without the opening-credit profile, run kernel-genesis.sh keys rotate and genesis"
	done
	for file in "$keys/publication/authorization.json" "$keys/publication/binding-policy.json"; do
		[ -s "$file" ] || precondition publication-policy "$file absent; run kernel-genesis.sh genesis (step C) first"
	done
	[ -S "$lni" ] || precondition kernel-node "$lni absent; the sequencer has not bootstrapped the genesis"
	for pid in /proc/[0-9]*; do
		[ "$(basename "$(readlink "$pid/exe" 2>/dev/null)")" = layerx-receipt-authority ] || continue
		token_files=$(tr '\000' '\n' <"$pid/environ" 2>/dev/null | sed -n 's/^LAYERX_AUTHORITY_TOKEN_FILES=//p' | head -n 1) || true
		[ -n "$token_files" ] && bearer=$(tr -d '\r\n' <"${token_files%%:*}") && break
	done
	[ -n "$bearer" ] || precondition receipt-authority "no layerx-receipt-authority process with a token file; task 2.1 runs it on 9445"
	[ -s "$tls/receipt-authority/ca.pem" ] ||
		precondition receipt-authority "$tls/receipt-authority/ca.pem absent; task 2.1 issues its certificate"
	answer=$(curl -sS -m 10 --cacert "$tls/receipt-authority/ca.pem" "$authority/readyz" 2>/dev/null) || answer=""
	[ "$(jq -r '.ready' <<<"$answer" 2>/dev/null)" = true ] ||
		precondition receipt-authority "not answering ready on $authority/readyz: ${answer:-no answer}"
	for tool in layerx-node-probe layerxctl sign-credit layerx-custody-proof; do
		command -v "$tool" >/dev/null || precondition image "$tool is not in the kernel image"
	done

	asset=$(tr -d ' \r\n' <"$genesis/asset-id")
	chain_pax=$(call "$custody" 0xaafcde84) || failed "asset eth_call $custody nativeAssetId() failed through $relay"
	chain_pax=${chain_pax#0x}
	[ "$chain_pax" = "$asset" ] || failed "asset PAX genesis=$asset chain=$chain_pax"
	echo "asset PAX id=$asset"

	install -d -o 0 -g 4020 -m 0750 "$keys/value-loop"
	install -d -o 4021 -g 4020 -m 0700 "$state"
	declare -gA dids=() mains=()
	for role in sender recipient; do
		seed=$keys/value-loop/$role.key
		if [ ! -s "$seed" ]; then
			install -o 4021 -g 4020 -m 0400 /dev/null "$seed.new"
			openssl rand 32 >"$seed.new"
			mv "$seed.new" "$seed"
		fi
		public=$(seed_public "$seed")
		did=did:layerx:$public
		id=$(main_id "$did")
		dids[$role]=$did
		mains[$role]=$id
		echo "account $role did=$did main=$id"
	done

	answer=$(balance "${dids[sender]}") || failed "balance sender read failed"
	if jq -e '.refused.result == -208' <<<"$answer" >/dev/null 2>&1; then
		[ -n "$deposit_tx" ] || precondition deposit-tx "the sender main account ${mains[sender]} is unopened; send deposit(bytes32 0x${mains[sender]}) of PAX to $custody from the funded depositor on chain 125 and pass its hash as DEPOSIT_TX"
		answer=$(curl -fsS -m 15 "$relay" -H 'content-type: application/json' \
			-d "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"eth_getTransactionReceipt\",\"params\":[\"$deposit_tx\"]}") ||
			failed "credit receipt of $deposit_tx unreadable through $relay"
		answer=$(python3 -c '
import json, sys
receipt = json.loads(sys.argv[1]).get("result") or {}
custody, topic, asset, beneficiary = sys.argv[2:6]
if int(receipt.get("status", "0x0"), 16) != 1:
    sys.exit("deposit transaction not successful")
logs = [log for log in receipt.get("logs", []) if log["address"].lower() == custody
        and log["topics"] and log["topics"][0].lower() == topic]
if len(logs) != 1 or len(logs[0]["topics"]) != 4:
    sys.exit("not exactly one custody deposit")
data = bytes.fromhex(logs[0]["data"][2:])
if logs[0]["topics"][2][2:].lower() != asset or data[:32].hex() != beneficiary:
    sys.exit("deposit asset or beneficiary differs")
print(logs[0]["topics"][1][2:].lower(), int.from_bytes(data[32:64], "big"))' \
			"$answer" "$custody" "$deposit_topic" "$asset" "${mains[sender]}" 2>&1) ||
			failed "credit deposit=$deposit_tx $answer"
		read -r id amount <<<"$answer"
		public=${dids[sender]#did:layerx:}
		rm -f "$state/credit" "$state/credit.activity"
		layerx-custody-proof light-credit --rpc "$(cat "$genesis/comet-url")" --profile "$genesis/custody.profile" \
			--deposit-id "0x$id" --owner-key "0x$public" \
			--trusted-validators-height "$(tail -c +162 "$genesis/custody.profile" | head -c 8 | od -An -tu8 --endian=big | tr -d ' ')" \
			--output "$state/credit" >/dev/null 2>&1 || failed "credit deposit=$deposit_tx light-credit refused"
		sign-credit "$genesis/custody.profile" "$state/credit" "${dids[sender]}" "$keys/value-loop/sender.key" \
			0 "$(($(date +%s%N) / 1000000))" "$state/credit.activity" >/dev/null 2>&1 ||
			failed "credit deposit=$deposit_tx sign-credit refused"
		chown 4021:4020 "$state/credit.activity"
		answer=$(as_lni layerxctl submit --socket "$lni" --network-id "$network_id" --protocol-version 3 \
			--actor "${dids[sender]}" --public-key "$public" --activity "$state/credit.activity" 2>&1) || answer=""
		jq -e '.state == "acknowledged"' <<<"$answer" >/dev/null 2>&1 || failed "credit deposit=$deposit_tx submit=${answer:-none}"
		deadline=$(($(date +%s) + 120))
		until balance "${dids[sender]}" | jq -e --arg a "$amount" '.balance == $a' >/dev/null 2>&1; do
			[ "$(date +%s)" -lt "$deadline" ] || failed "credit deposit=$deposit_tx amount=$amount balance=never"
			sleep 1
		done
		echo "credit deposit=$deposit_tx amount=$amount"
	fi

	if [ ! -s "$state/send.activity-id" ]; then
		rm -f "$state/send.bin"
		activity=$(as_lni layerx-node-probe write-send --socket "$lni" --network-id "$network_id" \
			--seed-file "$keys/value-loop/sender.key" --destination-did "${dids[recipient]}" \
			--asset "$asset" --output "$state/send.bin" 2>/dev/null) || failed "activity write-send refused"
		answer=$(as_lni layerxctl submit --socket "$lni" --network-id "$network_id" --protocol-version 3 \
			--actor "${dids[sender]}" --public-key "${dids[sender]#did:layerx:}" --activity "$state/send.bin" 2>&1) || answer=""
		jq -e --arg a "$activity" '.state == "acknowledged" and .activity_id == $a' <<<"$answer" >/dev/null 2>&1 ||
			failed "activity id=$activity submit=${answer:-none}"
		printf '%s\n' "$activity" >"$state/send.activity-id"
	fi
	activity=$(cat "$state/send.activity-id")
	echo "activity id=$activity"
	deadline=$(($(date +%s) + 120))
	until as_lni layerxctl read-state --socket "$lni" --network-id "$network_id" --protocol-version 3 \
		--actor "${dids[sender]}" >/dev/null 2>&1; do
		[ "$(date +%s)" -lt "$deadline" ] || failed "activity id=$activity sealed=never"
		sleep 1
	done

	for role in sender recipient; do
		answer=$(balance "${dids[$role]}") || answer=""
		echo "balance $role $(jq -c '{balance, refused}' <<<"$answer" 2>/dev/null || echo none)"
	done

	deadline=$(($(date +%s) + 120))
	while :; do
		answer=$(curl -sS -m 15 --cacert "$tls/receipt-authority/ca.pem" -H "Authorization: Bearer $bearer" \
			"$authority/v1/authorized-batches/by-activity/$activity" 2>/dev/null) || answer=""
		batch=$(jq -er --arg a "$activity" 'select(.activity_id == $a) | .batch_id' <<<"$answer" 2>/dev/null) && break
		[ "$(date +%s)" -lt "$deadline" ] || failed "batch activity=$activity authority=${answer:-none}"
		sleep 2
	done
	sealed=$(handshake_sealed) || failed "batch id=$batch sealed=unreadable"
	echo "batch id=$batch sealed=$sealed"

	deadline=$(($(date +%s) + checkpoint_seconds))
	while :; do
		latest=$(handshake_sealed) || latest=$sealed
		for ((b = sealed; b <= latest && b < sealed + 64; b++)); do
			status=$(call "$anchor" "0x4eb47710$(printf '%064x' "$b")") || status=0x0
			case "$((16#${status#0x}))" in
			1) echo "checkpoint batch=$b status=submitted" && return 0 ;;
			2) echo "checkpoint batch=$b status=final" && return 0 ;;
			esac
		done
		[ "$(date +%s)" -lt "$deadline" ] || failed "checkpoint batch=$sealed status=none"
		sleep 5
	done
}

main "$@" </dev/null
exit
