#!/usr/bin/env bash
# Off-chain service image table: one row per deployable image, pushed to ghcr.io/sidiora-labs/<name>.
#   --matrix  print the rows as a compact JSON array (the offchain-images workflow build matrix)
#   --check   exit non-zero when a required row's context or dockerfile is missing, or when a NEXT_PUBLIC_*
#             build arg of Dockerfile.web has no value in the web entry of launchpad/deploy/railway/variables.json, or
#             when a NEXT_PUBLIC_* build arg of the perps Dockerfile without a code fallback has no value in
#             human/apps/perps/deploy/build-args.production.env
# Base images come from the environment: REVIEWED_BUILD_IMAGE/REVIEWED_RUNTIME_IMAGE (go1.26.4 builder)
# and PERPS_BUILD_IMAGE/PERPS_RUNTIME_IMAGE, each pinned by digest; an unset one is left out so the
# Dockerfile's pinned default holds. Build arg values with whitespace are refused with exit 2.
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)

lp_args="BUILD_IMAGE=${REVIEWED_BUILD_IMAGE:-} RUNTIME_IMAGE=${REVIEWED_RUNTIME_IMAGE:-}"
px_args="BUILD_IMAGE=${PERPS_BUILD_IMAGE:-} RUNTIME_IMAGE=${PERPS_RUNTIME_IMAGE:-}"

# name|context|dockerfile|target|build args (space separated)|required
rows=()
# launchpad Go services: the TARGETS table of launchpad/security/supplychain/build.py, named as deploy.sh names them
for pair in core/api:apid core/indexer:indexerd core/pnl-tracker:pnld core/ranking-algo:rankingd \
	core/stats-workers:statsd core/trading-charts:chartsd media/gateway:mediagatewayd \
	media/livestream:livestreamd media/social:sociald media/user:userd; do
	svc=${pair%%:*} bin=${pair#*:}
	rows+=("kindlelaunch-${svc//\//-}|launchpad|launchpad/deploy/Dockerfile.svc|release|SERVICE=$svc BIN=$bin $lp_args|1")
done
rows+=("kindlelaunch-media-metadata|launchpad|launchpad/deploy/Dockerfile.native|release|SERVICE=media/metadata BIN=metadatad $lp_args|1")
# next_public_args <row> <dockerfile> <value fn> [<may-be-empty fn>]: sets args to every NEXT_PUBLIC_* ARG of the
# dockerfile with its value and missing to the names with no value
next_public_args() {
	args="" missing=()
	for var in $(sed -n 's/^ARG \(NEXT_PUBLIC_[A-Z0-9_]*\).*/\1/p' "$root/$2"); do
		value=$("$3" "$var")
		if [ -z "$value" ]; then
			{ [ -n "${4:-}" ] && "$4" "$var"; } || missing+=("$var")
		elif [[ "$value" =~ [[:space:]] ]]; then
			echo "$1: $var value contains whitespace" >&2
			exit 2
		else
			args+=" $var=$value"
		fi
	done
	args=${args# }
}
# the web image inlines every NEXT_PUBLIC_* arg of Dockerfile.web from the web entry of the Railway variables manifest
lp_vars="$root/launchpad/deploy/railway/variables.json"
lp_value() { jq -r --arg n "$1" '(.web.buildArgs // {}) + (.web.fixed // {}) | .[$n] // empty' "$lp_vars"; }
web_args="" web_missing=()
if [ -f "$root/launchpad/deploy/Dockerfile.web" ] && [ -f "$lp_vars" ]; then
	next_public_args kindlelaunch-web launchpad/deploy/Dockerfile.web lp_value
	web_args=$args web_missing=("${missing[@]}")
fi
rows+=("kindlelaunch-web|launchpad|launchpad/deploy/Dockerfile.web||${web_args# }|1")
# perps backend: one image per SERVICE, named as services/platform/perps-data/deploy/services.json names them
for pair in perps-data-indexer:perps-indexer perps-gateway:perps-gateway perps-explorer:perps-explorer perps-portal:perps-portal; do
	rows+=("${pair%%:*}|.|services/platform/perps-data/deploy/Dockerfile|runtime|SERVICE=${pair#*:} $px_args|1")
done
rows+=("perps-data-nats|.|services/platform/perps-data/deploy/nats/Dockerfile|||1")
# the Hyperpax perps frontend inlines every NEXT_PUBLIC_* arg of its Dockerfile from the committed build args file
perps_env="$root/human/apps/perps/deploy/build-args.production.env"
perps_value() { sed -n "s/^$1=//p" "$perps_env" | tail -n1; }
# names the app reads with a code fallback, so an empty value is allowed; a name absent from the file is not
perps_may_be_empty() {
	grep -q "^$1=" "$perps_env" || return 1
	case "${1#NEXT_PUBLIC_}" in
	PAXEER_RPC_URL | PAXEER_RPC_URLS) ;;           # built-in mainnet-beta node list (src/core/rpc.ts)
	PAXSCAN_API_URL | PAXSCAN_WS_URL) ;;           # empty default, the app warns (src/core/paxscan/config.ts)
	MARKET_PROXY_HOST) ;;                          # built-in feed host (src/core/feeds.ts)
	*_PRECOMPILE) ;;                               # release precompile addresses (src/core/network/precompiles.ts)
	RELEASE_SPOT_OPEN) ;;                          # spot stays closed until set (src/core/nav.ts)
	AUTH_REDIRECT_URL) ;;                          # window origin /auth/callback (src/core/wallet/embedded/client.ts)
	WALLET_IFRAME_ORIGIN) ;;                       # the iframe provider is not installed (src/core/wallet/postMessageEip1193.ts)
	SENTRY_DSN | SENTRY_ENV | SENTRY_TUNNEL) ;;    # Sentry off, NODE_ENV, no tunnel (sentry.client.config.ts, next.config.js)
	VERCEL_GIT_COMMIT_SHA) ;;                      # "development" meta tag (src/pages/_document.tsx)
	*) return 1 ;;
	esac
}
perps_args="" perps_missing=()
if [ -f "$root/human/apps/perps/Dockerfile" ] && [ -f "$perps_env" ]; then
	next_public_args perps-web human/apps/perps/Dockerfile perps_value perps_may_be_empty
	perps_args=$args perps_missing=("${missing[@]}")
fi
rows+=("perps-web|human/apps/perps|human/apps/perps/Dockerfile||$perps_args|0")

present() { [ -d "$root/$2" ] && [ -f "$root/$3" ]; }

case "${1:-}" in
--check)
	bad=0
	for row in "${rows[@]}"; do
		IFS='|' read -r name context dockerfile _ _ required <<<"$row"
		if [ "$required" = 1 ] && ! present "$name" "$context" "$dockerfile"; then
			echo "$name: missing context $context or dockerfile $dockerfile" >&2
			bad=1
		fi
	done
	if [ "${#web_missing[@]}" -gt 0 ]; then
		echo "kindlelaunch-web: no value in launchpad/deploy/railway/variables.json web entry for ${web_missing[*]}" >&2
		bad=1
	fi
	if [ "${#perps_missing[@]}" -gt 0 ]; then
		echo "perps-web: no value in human/apps/perps/deploy/build-args.production.env for ${perps_missing[*]}" >&2
		bad=1
	fi
	if [ -f "$root/human/apps/perps/Dockerfile" ] && [ ! -f "$perps_env" ]; then
		echo "perps-web: missing human/apps/perps/deploy/build-args.production.env" >&2
		bad=1
	fi
	if [ ! -f "$lp_vars" ]; then
		echo "kindlelaunch-web: missing launchpad/deploy/railway/variables.json" >&2
		bad=1
	fi
	exit "$bad"
	;;
--matrix)
	for row in "${rows[@]}"; do
		IFS='|' read -r name context dockerfile target args _ <<<"$row"
		present "$name" "$context" "$dockerfile" || continue
		jq -n -c --arg name "$name" --arg context "$context" --arg dockerfile "$dockerfile" --arg target "$target" \
			--arg args "$(tr ' ' '\n' <<<"$args" | grep -v -e '=$' -e '^$' || true)" \
			'{name: $name, context: $context, dockerfile: $dockerfile, target: $target, build_args: $args}'
	done | jq -s -c .
	;;
*)
	echo "usage: $0 --matrix|--check" >&2
	exit 2
	;;
esac
