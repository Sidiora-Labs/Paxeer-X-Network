#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "$0")/../../.." && pwd)"
dockerfile="$root/launchpad/deploy/Dockerfile.web"
script="$root/launchpad/deploy/web-build-env.sh"

fail() { echo "FAIL: $*" >&2; exit 1; }

out=$(env -i PATH="$PATH" NEXT_PUBLIC_EMPTY= WEB_EMPTY_UPSTREAM= NEXT_PUBLIC_SET=https://x.example WEB_SET_UPSTREAM=http://up:8080 OTHER_EMPTY= OTHER_SET=keep \
  sh -c '. "$1"; for v in NEXT_PUBLIC_EMPTY WEB_EMPTY_UPSTREAM NEXT_PUBLIC_SET WEB_SET_UPSTREAM OTHER_EMPTY OTHER_SET; do eval "printf \"%s=%s\\n\" $v \"\${$v-<unset>}\""; done' sh "$script")

expect() { grep -qxF "$1" <<<"$out" || fail "expected '$1', got: $out"; }
expect 'NEXT_PUBLIC_EMPTY=<unset>'
expect 'WEB_EMPTY_UPSTREAM=<unset>'
expect 'NEXT_PUBLIC_SET=https://x.example'
expect 'WEB_SET_UPSTREAM=http://up:8080'
expect 'OTHER_EMPTY='
expect 'OTHER_SET=keep'

! grep -qE '^ENV .*(NEXT_PUBLIC|WEB)_' "$dockerfile" || fail "Dockerfile.web must not ENV NEXT_PUBLIC_/WEB_ args"
! grep -qE '^[[:space:]]+(NEXT_PUBLIC|WEB)_[A-Z0-9_]*=' "$dockerfile" || fail "Dockerfile.web must not ENV NEXT_PUBLIC_/WEB_ args"
grep -qxF 'COPY deploy/web-build-env.sh /src/web-build-env.sh' "$dockerfile" || fail "web-build-env.sh not copied"
grep -qxF 'RUN . /src/web-build-env.sh && pnpm --filter @sidiora/web build' "$dockerfile" || fail "web build does not source web-build-env.sh"
[ "$(grep -cE '^ARG (NEXT_PUBLIC|WEB)_' "$dockerfile")" -eq 37 ] || fail "NEXT_PUBLIC_/WEB_ ARG count changed"

echo "ok"
