#!/usr/bin/env bash
set -euo pipefail

cd "$(git -C "$(dirname "$0")" rev-parse --show-toplevel)"

spec=spec/paxeer-x-bringup/spec.kvx
host_re='([a-z0-9-]+\.)+paxeer\.network'
docs=(docs platform/docs README.md ':(glob)platform/sdk/**/package-info.java' ':(glob)platform/sdk/**/README.md')

section() {
	awk -v s="[$1]" '$0 == s { on = 1; next } /^\[/ { on = 0 } on' "$spec"
}

expand() {
	local h
	while read -r h; do
		if [[ $h =~ ^api([0-9]+)\.\.api([0-9]+)\.(.+)$ ]]; then
			local first=${BASH_REMATCH[1]} last=${BASH_REMATCH[2]} rest=${BASH_REMATCH[3]} i
			for ((i = first; i <= last; i++)); do echo "api$i.$rest"; done
		else
			echo "$h"
		fi
	done
}

allowed=$({
	section decision.public_names
	section decision | grep '^derivation_message '
} | grep -oiE "(api[0-9]+\.\.)?$host_re" | tr '[:upper:]' '[:lower:]' | expand | sort -u)

rpc_count=$(grep -cE '^api([1-9]|1[0-6])\.mainnet-beta\.paxeer\.network$' <<<"$allowed" || true)
if [[ $rpc_count -ne 16 ]] || ! grep -qx 'api\.mainnet-beta\.router\.paxeer\.network' <<<"$allowed"; then
	echo "docs-names: cannot read the router and the sixteen RPC names from [decision.public_names] of $spec" >&2
	exit 1
fi

status=0

hits=$(git grep -oiE "$host_re" -- "${docs[@]}") || [[ $? -eq 1 ]]
while IFS=: read -r file name; do
	[[ -n $file ]] || continue
	name=$(tr '[:upper:]' '[:lower:]' <<<"$name")
	if ! grep -qxF "$name" <<<"$allowed"; then
		echo "docs-names: $file: $name is not a public name"
		status=1
	fi
done < <(sort -u <<<"$hits")

faucet=$(git grep -lw FAUCET_URL -- "${docs[@]}") || [[ $? -eq 1 ]]
while read -r file; do
	[[ -n $file ]] || continue
	echo "docs-names: $file: FAUCET_URL"
	status=1
done <<<"$faucet"

exit "$status"
