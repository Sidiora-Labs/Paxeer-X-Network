#!/usr/bin/env bash
set -euo pipefail

repo=$(cd "$(dirname "$0")/../../.." && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

fail() { echo "FAIL: $*" >&2; exit 1; }

# a copy of the repository pieces the script reads, with a value for every NEXT_PUBLIC_* and WEB_* arg of Dockerfile.web
mkdir -p "$work/tools/deploy" "$work/launchpad/deploy/railway" "$work/services/platform/perps-data/deploy/nats"
cp "$repo/tools/deploy/offchain-images.sh" "$work/tools/deploy/"
cp "$repo"/launchpad/deploy/Dockerfile.{svc,native,web} "$work/launchpad/deploy/"
cp "$repo/services/platform/perps-data/deploy/Dockerfile" "$work/services/platform/perps-data/deploy/"
cp "$repo/services/platform/perps-data/deploy/nats/Dockerfile" "$work/services/platform/perps-data/deploy/nats/"
names=$(sed -n 's/^ARG \(\(NEXT_PUBLIC\|WEB\)_[A-Z0-9_]*\).*/\1/p' "$repo/launchpad/deploy/Dockerfile.web")
[ -n "$names" ] || fail "Dockerfile.web declares no NEXT_PUBLIC_* args"
grep -qx WEB_API_UPSTREAM <<<"$names" || fail "Dockerfile.web declares no WEB_API_UPSTREAM arg"
jq --arg names "$names" '.web.buildArgs = ([$names | splits("\n") | {key: ., value: "https://\(. | ascii_downcase).example"}] | from_entries)' \
	"$repo/launchpad/deploy/railway/variables.json" > "$work/launchpad/deploy/railway/variables.json"
# the perps frontend Dockerfile (until it lands, one ARG per name of the committed build args file) and the committed
# values with the gateway origin filled in
mkdir -p "$work/human/apps/perps/deploy"
perps_src="$repo/human/apps/perps/deploy/build-args.production.env"
perps_env="$work/human/apps/perps/deploy/build-args.production.env"
if [ -f "$repo/human/apps/perps/Dockerfile" ]; then
	cp "$repo/human/apps/perps/Dockerfile" "$work/human/apps/perps/"
else
	sed -n 's/^\(NEXT_PUBLIC_[A-Z0-9_]*\)=.*/ARG \1=/p' "$perps_src" > "$work/human/apps/perps/Dockerfile"
fi
sed 's|^NEXT_PUBLIC_PERPS_GATEWAY_URL=$|NEXT_PUBLIC_PERPS_GATEWAY_URL=https://gateway.example|' "$perps_src" > "$perps_env"
perps_pristine=$(cat "$perps_env")
script="$work/tools/deploy/offchain-images.sh"
manifest="$work/launchpad/deploy/railway/variables.json"
pristine=$(cat "$manifest")

sha=0123456789ab
run() { env -u REVIEWED_BUILD_IMAGE -u REVIEWED_RUNTIME_IMAGE -u PERPS_BUILD_IMAGE -u PERPS_RUNTIME_IMAGE BUILD_SHA=$sha "$@"; }
args_of() { jq -r --arg n "$2" '.[] | select(.name == $n) | .build_args' <<<"$1"; }

run bash "$script" --check || fail "--check must pass on a complete tree"

# unset base images are left out so the Dockerfile defaults hold
m=$(run bash "$script" --matrix)
jq -e 'length > 0 and all(.[]; keys == ["build_args","context","dockerfile","name","target"])' <<<"$m" >/dev/null || fail "matrix shape changed"
! jq -r '.[].build_args' <<<"$m" | grep -E '^[A-Z_]+=$' || fail "matrix carries an empty build arg"
! jq -r '.[].build_args' <<<"$m" | grep -E '^(BUILD|RUNTIME)_IMAGE=' || fail "unset base images must be omitted"
[ "$(args_of "$m" kindlelaunch-core-api)" = $'SERVICE=core/api\nBIN=apid' ] || fail "api args: $(args_of "$m" kindlelaunch-core-api)"

# the repository variable names reach the launchpad and perps rows
m=$(run env REVIEWED_BUILD_IMAGE=rb@sha256:1 REVIEWED_RUNTIME_IMAGE=rr@sha256:2 PERPS_BUILD_IMAGE=pb@sha256:3 bash "$script" --matrix)
[ "$(args_of "$m" kindlelaunch-media-metadata)" = $'SERVICE=media/metadata\nBIN=metadatad\nBUILD_IMAGE=rb@sha256:1\nRUNTIME_IMAGE=rr@sha256:2' ] || fail "metadata args: $(args_of "$m" kindlelaunch-media-metadata)"
[ "$(args_of "$m" perps-gateway)" = $'SERVICE=perps-gateway\nBUILD_IMAGE=pb@sha256:3' ] || fail "perps args: $(args_of "$m" perps-gateway)"

# the web row carries every NEXT_PUBLIC_* arg with its manifest value, and the build sha as the Sentry release
web=$(args_of "$m" kindlelaunch-web)
for n in $names; do
	[ "$n" = NEXT_PUBLIC_SENTRY_RELEASE ] && continue
	grep -qx "$n=https://${n,,}.example" <<<"$web" || fail "web row lacks $n"
done
grep -qx "NEXT_PUBLIC_SENTRY_RELEASE=$sha" <<<"$web" || fail "web row Sentry release is not the build sha"
[ "$(wc -l <<<"$web")" -eq "$(wc -w <<<"$names")" ] || fail "web row has extra args"
grep -qx "WEB_API_UPSTREAM=https://web_api_upstream.example" <<<"$web" || fail "web row lacks the WEB_API_UPSTREAM build arg"

# a WEB_* arg without a manifest value fails --check and is named, like a NEXT_PUBLIC_* one
jq 'del(.web.buildArgs.WEB_API_UPSTREAM)' <<<"$pristine" > "$manifest"
if out=$(run bash "$script" --check 2>&1); then fail "--check must fail on a missing WEB_API_UPSTREAM"; fi
grep -q 'kindlelaunch-web: .*WEB_API_UPSTREAM' <<<"$out" || fail "--check must name WEB_API_UPSTREAM: $out"
printf '%s\n' "$pristine" > "$manifest"

# a value with a space is refused with exit 2
first=$(head -n1 <<<"$names")
jq --arg n "$first" '.web.buildArgs[$n] = "https://a b"' <<<"$pristine" > "$manifest"
set +e
run bash "$script" --matrix >/dev/null 2>&1; rc=$?
set -e
[ "$rc" -eq 2 ] || fail "a value with a space must exit 2, got $rc"

# a NEXT_PUBLIC_* arg without a manifest value fails --check and is named
last=$(grep -vE "SOCIAL_API_URL|USER_API_URL|ANALYTICS_KEY|QUALIFICATION_AUTHORITY_PROFILE|SENTRY_RELEASE" <<<"$names" | tail -n1)
jq --arg a "$first" --arg b "$last" 'del(.web.buildArgs[$a], .web.buildArgs[$b])' <<<"$pristine" > "$manifest"
if out=$(run bash "$script" --check 2>&1); then fail "--check must fail on a missing value"; fi
grep -q "$first" <<<"$out" && grep -q "$last" <<<"$out" || fail "--check must name the missing args: $out"

# the committed web values: the three same-origin origins are set, the four dead or unset-is-production names may be
# empty, the Sentry release is the sha passed, and a backend host without a value still fails --check
real="$repo/launchpad/deploy/railway/variables.json"
for o in NEXT_PUBLIC_PRIVATE_API_ORIGIN=https://sidiora.fun NEXT_PUBLIC_PRIVATE_STREAM_ORIGIN=wss://sidiora.fun \
	NEXT_PUBLIC_STREAM_ORIGIN=wss://sidiora.fun; do
	[ "$(jq -r --arg n "${o%%=*}" '.web.buildArgs[$n] // empty' "$real")" = "${o#*=}" ] || fail "committed manifest lacks $o"
done
may="NEXT_PUBLIC_SOCIAL_API_URL NEXT_PUBLIC_USER_API_URL NEXT_PUBLIC_ANALYTICS_KEY NEXT_PUBLIC_PRIVATE_QUALIFICATION_AUTHORITY_PROFILE"
jq --arg may "$may" '.web.buildArgs |= with_entries(select(.key as $k | $may | split(" ") | index($k) | not)) | del(.web.buildArgs.NEXT_PUBLIC_SENTRY_RELEASE)' \
	<<<"$pristine" > "$manifest"
run bash "$script" --check || fail "--check must allow the four may-be-empty web names and take the release from the sha"
[ "$(args_of "$(run bash "$script" --matrix)" kindlelaunch-web | grep '^NEXT_PUBLIC_SENTRY_RELEASE=')" = "NEXT_PUBLIC_SENTRY_RELEASE=$sha" ] ||
	fail "the Sentry release is not the sha passed"
jq '.web.buildArgs.NEXT_PUBLIC_DATA_API_URL = ""' <<<"$(cat "$manifest")" > "$manifest.tmp" && mv "$manifest.tmp" "$manifest"
if out=$(run bash "$script" --check 2>&1); then fail "--check must fail on an empty data API URL"; fi
grep -q 'kindlelaunch-web: .*NEXT_PUBLIC_DATA_API_URL' <<<"$out" || fail "--check must name the data API URL: $out"
! grep -qE 'SOCIAL_API_URL|USER_API_URL|ANALYTICS_KEY|QUALIFICATION_AUTHORITY_PROFILE|SENTRY_RELEASE' <<<"$out" ||
	fail "--check named an allowed web name: $out"
printf '%s\n' "$pristine" > "$manifest"

# --plan on the committed web values, which leave web args without a value: --check still fails, the web row alone
# is unbuilt with an error annotation naming the missing arg, and every other row is planned exactly as --matrix has it
full=$(run bash "$script" --matrix)
jq 'del(.web.buildArgs.NEXT_PUBLIC_DATA_API_URL)' "$real" > "$manifest"
if run bash "$script" --check 2>/dev/null; then fail "--check must fail on the committed web values"; fi
run bash "$script" --plan > "$work/plan.out" 2> "$work/plan.err" || fail "--plan must pass when a row passes: $(cat "$work/plan.err")"
[ "$(grep -c . "$work/plan.out")" -eq 2 ] || fail "--plan must print exactly images= and unbuilt=: $(cat "$work/plan.out")"
[ "$(sed -n 's/^unbuilt=//p' "$work/plan.out")" = '["kindlelaunch-web"]' ] || fail "unbuilt: $(cat "$work/plan.out")"
grep -q '^::error title=kindlelaunch-web::.*NEXT_PUBLIC_DATA_API_URL' "$work/plan.err" || fail "--plan must annotate the web row: $(cat "$work/plan.err")"
[ "$(grep -c '^::error' "$work/plan.err")" -eq 1 ] || fail "--plan must annotate only the web row: $(cat "$work/plan.err")"
planned=$(sed -n 's/^images=//p' "$work/plan.out")
[ "$planned" = "$(jq -c 'map(select(.name != "kindlelaunch-web"))' <<<"$full")" ] || fail "--plan images differ from --matrix: $planned"
# a perps value with whitespace leaves only perps-web unbuilt as well, and no passing row exits 1
sed -i 's|^NEXT_PUBLIC_LOG_LEVEL=.*|NEXT_PUBLIC_LOG_LEVEL=warn x|' "$perps_env"
out=$(run bash "$script" --plan 2>/dev/null) || fail "--plan must pass with a perps value with whitespace"
[ "$(sed -n 's/^unbuilt=//p' <<<"$out")" = '["kindlelaunch-web","perps-web"]' ] || fail "unbuilt: $out"
printf '%s\n' "$perps_pristine" > "$perps_env"
mv "$work/launchpad" "$work/launchpad.away" && mv "$work/services" "$work/services.away"
sed -i 's|^NEXT_PUBLIC_PERPS_GATEWAY_URL=.*|NEXT_PUBLIC_PERPS_GATEWAY_URL=|' "$perps_env"
if out=$(run bash "$script" --plan 2>/dev/null); then fail "--plan must exit 1 when no row passes: $out"; fi
mv "$work/launchpad.away" "$work/launchpad" && mv "$work/services.away" "$work/services"
printf '%s\n' "$perps_pristine" > "$perps_env"
printf '%s\n' "$pristine" > "$manifest"

# the perps-web row carries exactly the non-empty values of the build args file
want=$(grep -E '^NEXT_PUBLIC_[A-Z0-9_]+=.' "$perps_env" | sort)
[ "$(args_of "$(run bash "$script" --matrix)" perps-web | sort)" = "$want" ] || fail "perps-web args differ from the build args file"
grep -qx 'NEXT_PUBLIC_PAXSCAN_BASE_URL=https://paxscan.io' <<<"$want" || fail "perps-web lacks the explorer URL"

# an empty value without a code fallback fails --check and is named; an empty fallback value does not
sed -i 's|^NEXT_PUBLIC_PERPS_GATEWAY_URL=.*|NEXT_PUBLIC_PERPS_GATEWAY_URL=|' "$perps_env"
if out=$(run bash "$script" --check 2>&1); then fail "--check must fail on an empty perps gateway"; fi
grep -q 'perps-web: .*NEXT_PUBLIC_PERPS_GATEWAY_URL' <<<"$out" || fail "--check must name the perps gateway: $out"
! grep -q 'NEXT_PUBLIC_PAXSCAN_API_URL' <<<"$out" || fail "--check must allow the empty paxscan API URL: $out"

# a perps value with a space is refused with exit 2
printf '%s\n' "$perps_pristine" | sed 's|^NEXT_PUBLIC_LOG_LEVEL=.*|NEXT_PUBLIC_LOG_LEVEL=warn x|' > "$perps_env"
set +e
run bash "$script" --matrix >/dev/null 2>&1; rc=$?
set -e
[ "$rc" -eq 2 ] || fail "a perps value with a space must exit 2, got $rc"
printf '%s\n' "$perps_pristine" > "$perps_env"

# a missing Dockerfile of a required row fails --check
rm "$work/launchpad/deploy/Dockerfile.native"
if out=$(run bash "$script" --check 2>&1); then fail "--check must fail on a missing dockerfile"; fi
grep -q 'kindlelaunch-media-metadata' <<<"$out" || fail "--check must name the row: $out"

echo "PASS offchain-images"
