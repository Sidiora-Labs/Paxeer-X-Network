#!/usr/bin/env bash
# Off-chain service image table: one row per deployable image, pushed to ghcr.io/sidiora-labs/<name>.
#   --matrix  print the rows as a compact JSON array (the offchain-images workflow build matrix)
#   --check   exit non-zero when a required row's context or dockerfile is missing, or when a NEXT_PUBLIC_* or WEB_*
#             build arg of Dockerfile.web has no value in the web entry of launchpad/deploy/railway/variables.json, or
#             when a NEXT_PUBLIC_* build arg of the perps Dockerfile without a code fallback has no value in
#             human/apps/perps/deploy/build-args.production.env
#   --plan    print images=<matrix of every row that passes its --check test> and unbuilt=<JSON array of the rows
#             that fail it>, with one ::error annotation per failing row on stderr; a value with whitespace fails
#             only its row; exit 1 when no row passes
# The web image's NEXT_PUBLIC_SENTRY_RELEASE is the build commit: BUILD_SHA, else the checkout's 12-character HEAD.
# Base images come from the environment: REVIEWED_BUILD_IMAGE/REVIEWED_RUNTIME_IMAGE (go1.26.4 builder)
# and PERPS_BUILD_IMAGE/PERPS_RUNTIME_IMAGE, each pinned by digest; an unset one is left out so the
# Dockerfile's pinned default holds. Build arg values with whitespace are refused with exit 2 (except by --plan).
set -euo pipefail

mode=${1:-}

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
# next_public_args <row> <dockerfile> <value fn> [<may-be-empty fn>]: sets args to every NEXT_PUBLIC_* and WEB_* ARG
# of the dockerfile with its value, missing to the names with no value and spaced to the names whose value has whitespace
next_public_args() {
	args="" missing=() spaced=()
	for var in $(sed -n 's/^ARG \(\(NEXT_PUBLIC\|WEB\)_[A-Z0-9_]*\).*/\1/p' "$root/$2"); do
		value=$("$3" "$var")
		if [ -z "$value" ]; then
			{ [ -n "${4:-}" ] && "$4" "$var"; } || missing+=("$var")
		elif [[ "$value" =~ [[:space:]] ]]; then
			echo "$1: $var value contains whitespace" >&2
			[ "$mode" = --plan ] || exit 2
			spaced+=("$var")
		else
			args+=" $var=$value"
		fi
	done
	args=${args# }
}
# the web image inlines every NEXT_PUBLIC_* and WEB_* arg of Dockerfile.web from the web entry of the Railway variables manifest
lp_vars="$root/launchpad/deploy/railway/variables.json"
build_sha=${BUILD_SHA:-$(git -C "$root" rev-parse --short=12 HEAD 2>/dev/null || true)}
lp_value() {
	if [ "$1" = NEXT_PUBLIC_SENTRY_RELEASE ]; then
		echo "$build_sha"
	else
		jq -r --arg n "$1" '(.web.buildArgs // {}) + (.web.fixed // {}) | .[$n] // empty' "$lp_vars"
	fi
}
# names the web app treats as unset when empty, so an empty value is allowed
lp_may_be_empty() {
	case "${1#NEXT_PUBLIC_}" in
	SOCIAL_API_URL | USER_API_URL) ;;              # no app source reads them; social and user go through the gateway
	ANALYTICS_KEY) ;;                              # no analytics provider is wired (src/core/analytics.ts)
	PRIVATE_QUALIFICATION_AUTHORITY_PROFILE) ;;    # unset is production: authority from /v2/network/authority
	*) return 1 ;;
	esac
}
web_args="" web_missing=() web_spaced=()
if [ -f "$root/launchpad/deploy/Dockerfile.web" ] && [ -f "$lp_vars" ]; then
	next_public_args kindlelaunch-web launchpad/deploy/Dockerfile.web lp_value lp_may_be_empty
	web_args=$args web_missing=("${missing[@]}") web_spaced=("${spaced[@]}")
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
perps_args="" perps_missing=() perps_spaced=()
if [ -f "$root/human/apps/perps/Dockerfile" ] && [ -f "$perps_env" ]; then
	next_public_args perps-web human/apps/perps/Dockerfile perps_value perps_may_be_empty
	perps_args=$args perps_missing=("${missing[@]}") perps_spaced=("${spaced[@]}")
fi
rows+=("perps-web|human/apps/perps|human/apps/perps/Dockerfile||$perps_args|0")

present() { [ -d "$root/$2" ] && [ -f "$root/$3" ]; }
# row_json <name> <context> <dockerfile> <target> <args>: one build matrix entry, empty build args left out
row_json() {
	jq -n -c --arg name "$1" --arg context "$2" --arg dockerfile "$3" --arg target "$4" \
		--arg args "$(tr ' ' '\n' <<<"$5" | grep -v -e '=$' -e '^$' || true)" \
		'{name: $name, context: $context, dockerfile: $dockerfile, target: $target, build_args: $args}'
}
# row_problem <name> <context> <dockerfile> <required>: prints why the row fails its --check test, nothing when it
# passes; a missing optional row returns 1 so it is neither built nor reported
row_problem() {
	if ! present "$1" "$2" "$3"; then
		[ "$4" = 1 ] || return 1
		echo "missing context $2 or dockerfile $3"
	elif [ "$1" = kindlelaunch-web ]; then
		if [ ! -f "$lp_vars" ]; then
			echo "missing launchpad/deploy/railway/variables.json"
		elif [ "${#web_missing[@]}" -gt 0 ]; then
			echo "no value in launchpad/deploy/railway/variables.json web entry for ${web_missing[*]}"
		elif [ "${#web_spaced[@]}" -gt 0 ]; then
			echo "value with whitespace in launchpad/deploy/railway/variables.json web entry for ${web_spaced[*]}"
		fi
	elif [ "$1" = perps-web ]; then
		if [ ! -f "$perps_env" ]; then
			echo "missing human/apps/perps/deploy/build-args.production.env"
		elif [ "${#perps_missing[@]}" -gt 0 ]; then
			echo "no value in human/apps/perps/deploy/build-args.production.env for ${perps_missing[*]}"
		elif [ "${#perps_spaced[@]}" -gt 0 ]; then
			echo "value with whitespace in human/apps/perps/deploy/build-args.production.env for ${perps_spaced[*]}"
		fi
	fi
}

case "$mode" in
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
		row_json "$name" "$context" "$dockerfile" "$target" "$args"
	done | jq -s -c .
	;;
--plan)
	images=() unbuilt=()
	for row in "${rows[@]}"; do
		IFS='|' read -r name context dockerfile target args required <<<"$row"
		reason=$(row_problem "$name" "$context" "$dockerfile" "$required") || continue
		if [ -n "$reason" ]; then
			echo "::error title=$name::$name is not built: $reason" >&2
			unbuilt+=("$name")
		else
			images+=("$(row_json "$name" "$context" "$dockerfile" "$target" "$args")")
		fi
	done
	printf 'images=%s\n' "$(printf '%s\n' "${images[@]}" | jq -s -c 'map(select(. != null))')"
	printf 'unbuilt=%s\n' "$(printf '%s\n' "${unbuilt[@]}" | jq -R -s -c 'split("\n") | map(select(length > 0))')"
	[ "${#images[@]}" -gt 0 ] || { echo "::error title=offchain-images::no row passes its check" >&2; exit 1; }
	;;
*)
	echo "usage: $0 --matrix|--check|--plan" >&2
	exit 2
	;;
esac
