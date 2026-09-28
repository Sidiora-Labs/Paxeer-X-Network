#!/bin/sh
set -eu

stream_width="${BROWSER_STREAM_WIDTH:-780}"
stream_height="${BROWSER_STREAM_HEIGHT:-1592}"
stream_max_width="${BROWSER_STREAM_MAX_WIDTH:-2560}"
stream_max_height="${BROWSER_STREAM_MAX_HEIGHT:-2560}"
xvfb_width=$(( (stream_max_width + 7) / 8 * 8 ))
xvfb_height=$(( (stream_max_height + 7) / 8 * 8 ))
secret_file="${BROWSER_STREAM_MASTER_KEY_FILE:-${BROWSER_PLANE_INTERNAL_KEY_FILE:-}}"

if [ -z "$secret_file" ] || [ ! -r "$secret_file" ]; then
  echo "A readable browser stream master-key file is required." >&2
  exit 1
fi

export SELKIES_MASTER_TOKEN
SELKIES_MASTER_TOKEN="$(tr -d '\r\n' < "$secret_file")"
if [ "${#SELKIES_MASTER_TOKEN}" -lt 32 ]; then
  echo "The browser stream master key must contain at least 32 characters." >&2
  exit 1
fi

xvfb_pid=""
openbox_pid=""
selkies_pid=""
node_pid=""

shutdown() {
  trap - TERM INT EXIT
  [ -z "$node_pid" ] || kill -TERM "$node_pid" 2>/dev/null || true
  [ -z "$selkies_pid" ] || kill -TERM "$selkies_pid" 2>/dev/null || true
  [ -z "$openbox_pid" ] || kill -TERM "$openbox_pid" 2>/dev/null || true
  [ -z "$xvfb_pid" ] || kill -TERM "$xvfb_pid" 2>/dev/null || true
  wait 2>/dev/null || true
}

trap shutdown TERM INT EXIT

Xvfb "$DISPLAY" -screen 0 "${xvfb_width}x${xvfb_height}x24" -nolisten tcp -noreset &
xvfb_pid="$!"

display_attempt=0
until xdpyinfo -display "$DISPLAY" >/dev/null 2>&1; do
  display_attempt=$((display_attempt + 1))
  if [ "$display_attempt" -ge 100 ]; then
    echo "The virtual display did not become ready." >&2
    exit 1
  fi
  sleep 0.1
done

openbox &
openbox_pid="$!"

selkies &
selkies_pid="$!"

stream_attempt=0
until node -e "fetch('http://127.0.0.1:8081/api/browser-stream/api/health').then(r=>{if(!r.ok)process.exit(1)}).catch(()=>process.exit(1))"; do
  stream_attempt=$((stream_attempt + 1))
  if [ "$stream_attempt" -ge 150 ]; then
    echo "The virtual-browser stream did not become ready." >&2
    exit 1
  fi
  sleep 0.2
done

node /srv/browser-plane/server.mjs &
node_pid="$!"

while kill -0 "$xvfb_pid" 2>/dev/null \
  && kill -0 "$openbox_pid" 2>/dev/null \
  && kill -0 "$selkies_pid" 2>/dev/null \
  && kill -0 "$node_pid" 2>/dev/null; do
  sleep 1
done

exit 1
