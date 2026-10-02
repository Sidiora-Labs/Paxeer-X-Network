#!/usr/bin/env bash
# Focused behavior gate for task 108.0.7: tracked docs name only served hosts, carry no FAUCET_URL,
# fund accounts through custody credit at the router URL and mark the private-network pages.
set -uo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/../../.." || exit 1

tests=0
failed=0
check() {
	local name=$1
	shift
	tests=$((tests + 1))
	if "$@"; then
		echo "PASS $name"
	else
		echo "FAIL $name"
		failed=1
	fi
}

router=https://api-mainnet-beta.paxeer.network

docs_names_clean() {
	timeout 5m tools/bringup/docs-names.sh
}

funded_by_custody_credit() {
	grep -qi 'custody credit' "$1" && grep -qF "$router" "$1" && ! grep -qw FAUCET_URL "$1"
}

private_network_only() {
	grep -qiE 'private-network only|private network only' "$1"
}

no_docs_host() {
	! grep -qF docs.paxeer.network "$1" && grep -qF 'https://github.com/Sidiora-Labs/Paxeer-X-Network/blob/main/' "$1"
}

check_refuses() {
	local tmp out code
	tmp=$(mktemp -d) || return 1
	git clone -q --shared --no-checkout . "$tmp/repo" &&
		git -C "$tmp/repo" checkout -q HEAD -- docs platform/docs README.md platform/sdk spec tools/bringup/docs-names.sh || {
		rm -rf "$tmp"
		return 1
	}
	printf 'Claim at https://faucet.paxeer.network with FAUCET_URL set.\nRouter https://api-mainnet-beta.paxeer.network.\n' >"$tmp/repo/docs/offending.md"
	git -C "$tmp/repo" add docs/offending.md
	out=$(timeout 5m "$tmp/repo/tools/bringup/docs-names.sh")
	code=$?
	rm -rf "$tmp"
	printf '%s\n' "$out"
	[[ $code -eq 1 ]] &&
		grep -qxF 'docs-names: docs/offending.md: faucet.paxeer.network is not a public name' <<<"$out" &&
		grep -qxF 'docs-names: docs/offending.md: FAUCET_URL' <<<"$out" &&
		! grep -q 'api-mainnet-beta' <<<"$out" &&
		[[ $(wc -l <<<"$out") -eq 2 ]]
}

check docs-names-clean docs_names_clean
check docs-names-refuses-unserved-host-and-faucet-url check_refuses

for page in \
	docs/site/docs/overview/getting-started.md \
	docs/site/docs/overview/quickstart.md \
	docs/site/docs/overview/payments.md \
	docs/site/docs/platform/index.md \
	docs/wiki/Getting-Started-Beta.md \
	docs/wiki/Quickstart.md \
	docs/wiki/PaymentsQuickstart.md; do
	check "custody-credit:$page" funded_by_custody_credit "$page"
done

for page in \
	docs/site/docs/platform/faucet.md \
	docs/site/docs/operators/beta-control.md \
	docs/wiki/HostedFaucet.md \
	docs/wiki/HostedBetaControl.md \
	platform/docs/beta-environment.md \
	platform/docs/content/beta.md \
	platform/docs/beta-resets.ics; do
	check "private-network-only:$page" private_network_only "$page"
done

check package-info-docs-location no_docs_host platform/sdk/jvm/src/main/java/com/sidiora/layerx/sdk/package-info.java

echo "PAXEER_X_GATE tests=${tests} skipped=0"
exit "$failed"
