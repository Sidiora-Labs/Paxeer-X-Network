#!/usr/bin/env bash
# Checks the tracked explorer configuration a user sees on paxscan.io.
set -euo pipefail

root="$(cd "$(dirname "$0")/../.." && pwd)"
preset="$root/frontend/configs/envs/paxeer-x.env"
footer="$root/frontend/configs/paxeer-x/footer.json"
backend="$root/deploy/env/backend.example.env"
repo="$(cd "$root/.." && pwd)"
workflow="$repo/.github/workflows/explorer-images.yml"
fail=0

value() { grep -E "^$2=" "$1" | tail -n1 | cut -d= -f2-; }
expect() {
  local got
  got="$(value "$1" "$2")"
  if [ "$got" != "$3" ]; then
    echo "FAIL ${1#"$root"/} $2: want '$3', got '$got'"
    fail=1
  fi
}
absent() {
  if grep -nE "$2" "$1"; then
    echo "FAIL ${1#"$root"/} carries '$2'"
    fail=1
  fi
}

expect "$preset" NEXT_PUBLIC_API_PROTOCOL https
expect "$preset" NEXT_PUBLIC_API_HOST api.paxscan.io
expect "$preset" NEXT_PUBLIC_API_WEBSOCKET_PROTOCOL wss
expect "$preset" NEXT_PUBLIC_APP_PROTOCOL https
expect "$preset" NEXT_PUBLIC_APP_HOST paxscan.io
expect "$preset" NEXT_PUBLIC_NETWORK_NAME 'Paxeer X Network'
expect "$preset" NEXT_PUBLIC_FOOTER_LINKS \
  https://raw.githubusercontent.com/Sidiora-Labs/Paxeer-X-Network/main/explorer/frontend/configs/paxeer-x/footer.json
expect "$backend" PAXEER_X_CAPABILITIES_ENABLED false

case "$(value "$preset" NEXT_PUBLIC_OG_DESCRIPTION)" in
  *'Paxeer X Network'*) ;;
  *) echo "FAIL NEXT_PUBLIC_OG_DESCRIPTION does not name Paxeer X Network"; fail=1 ;;
esac
case "$(value "$preset" NEXT_PUBLIC_OTHER_LINKS)" in
  *"'https://paxeer.network'"*"'https://docs.paxeer.app'"*) ;;
  *) echo "FAIL NEXT_PUBLIC_OTHER_LINKS lacks paxeer.network and docs.paxeer.app"; fail=1 ;;
esac

for f in "$preset" "$footer"; do
  absent "$f" 'REPLACE_|chainflowtrading|dev-paxeer|hyperpaxeer|//(www\.)?paxeer\.app|Argus|DEFI_DROPDOWN|GAS_REFUEL'
done

for pair in explorer-backend:BLOCKSCOUT_VERSION explorer-frontend:GIT_COMMIT_SHA; do
  image="${pair%%:*}" arg="${pair#*:}"
  if ! grep -qE "^ARG $arg\$" "$repo/docker/$image/Dockerfile"; then
    echo "FAIL docker/$image/Dockerfile does not declare ARG $arg"
    fail=1
  fi
  if ! grep -A4 "dockerfile: docker/$image/Dockerfile" "$workflow" | grep -qE "^ +$arg=.*\{\{ github\.sha \}\}"; then
    echo "FAIL explorer-images.yml does not pass $arg with the commit sha to $image"
    fail=1
  fi
done

if ! python3 - "$footer" <<'PY'
import json, sys
groups = json.load(open(sys.argv[1]))
urls = {link["url"] for g in groups for link in g["links"]}
assert all(isinstance(g["title"], str) and g["title"] for g in groups)
for want in ("https://paxeer.network", "https://docs.paxeer.app"):
    assert want in urls, want
PY
then
  echo "FAIL footer.json must be valid and link paxeer.network and docs.paxeer.app"
  fail=1
fi

[ "$fail" -eq 0 ] && echo "explorer config check: ok"
exit "$fail"
