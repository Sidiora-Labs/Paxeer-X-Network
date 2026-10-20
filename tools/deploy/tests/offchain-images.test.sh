#!/usr/bin/env bash
set -euo pipefail

repo=$(cd "$(dirname "$0")/../../.." && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

fail() { echo "FAIL: $*" >&2; exit 1; }

# a copy of the repository pieces the script reads, with a value for every NEXT_PUBLIC_* arg of Dockerfile.web
mkdir -p "$work/tools/deploy" "$work/launchpad/deploy/railway" "$work/services/platform/perps-data/deploy/nats"
cp "$repo/tools/deploy/offchain-images.sh" "$work/tools/deploy/"
cp "$repo"/launchpad/deploy/Dockerfile.{svc,native,web} "$work/launchpad/deploy/"
cp "$repo/services/platform/perps-data/deploy/Dockerfile" "$work/services/platform/perps-data/deploy/"
cp "$repo/services/platform/perps-data/deploy/nats/Dockerfile" "$work/services/platform/perps-data/deploy/nats/"
names=$(sed -n 's/^ARG \(NEXT_PUBLIC_[A-Z0-9_]*\).*/\1/p' "$repo/launchpad/deploy/Dockerfile.web")
[ -n "$names" ] || fail "Dockerfile.web declares no NEXT_PUBLIC_* args"
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

run() { env -u REVIEWED_BUILD_IMAGE -u REVIEWED_RUNTIME_IMAGE -u PERPS_BUILD_IMAGE -u PERPS_RUNTIME_IMAGE "$@"; }
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

# the web row carries every NEXT_PUBLIC_* arg with its manifest value
web=$(args_of "$m" kindlelaunch-web)
for n in $names; do
	grep -qx "$n=https://${n,,}.example" <<<"$web" || fail "web row lacks $n"
done
[ "$(wc -l <<<"$web")" -eq "$(wc -w <<<"$names")" ] || fail "web row has extra args"

# a value with a space is refused with exit 2
first=$(head -n1 <<<"$names")
jq --arg n "$first" '.web.buildArgs[$n] = "https://a b"' <<<"$pristine" > "$manifest"
set +e
run bash "$script" --matrix >/dev/null 2>&1; rc=$?
set -e
[ "$rc" -eq 2 ] || fail "a value with a space must exit 2, got $rc"

# a NEXT_PUBLIC_* arg without a manifest value fails --check and is named
last=$(tail -n1 <<<"$names")
jq --arg a "$first" --arg b "$last" 'del(.web.buildArgs[$a], .web.buildArgs[$b])' <<<"$pristine" > "$manifest"
if out=$(run bash "$script" --check 2>&1); then fail "--check must fail on a missing value"; fi
grep -q "$first" <<<"$out" && grep -q "$last" <<<"$out" || fail "--check must name the missing args: $out"
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
