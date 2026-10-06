#!/usr/bin/env bash
# Assemble one immutable HPX release and atomically publish it through
# /srv/hpx/artifacts/current. Generated binaries and registry data stay outside Git.
set -euo pipefail

HPX_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PAXEER_ROOT="$(cd "${HPX_DIR}/.." && pwd)"
MONOREPO_ROOT="${PAXEER_ROOT}"

SRC_BIN="${SRC_BIN:-${PAXEER_ROOT}/build/paxd}"
SRC_CFG="${SRC_CFG:-${HPX_RUNTIME_CONFIG_DIR:-/root/.paxeer/config}}"
WASM_ROOT="${WASM_ROOT:-${PAXEER_ROOT}}"
ARTIFACTS_ROOT="${HPX_ARTIFACTS_ROOT:-/srv/hpx/artifacts}"
RELEASES_DIR="${ARTIFACTS_ROOT}/releases"

CHAIN_ID="${HPX_CHAIN_ID:-hyperpax_125-1}"
EVM_CHAIN_ID="${HPX_EVM_CHAIN_ID:-125}"
P2P_PORT="${HPX_P2P_PORT:-26656}"
RPC_PORT="${HPX_RPC_PORT:-26657}"
PAXD_VERSION="${HPX_PAXD_VERSION:-v6.11.0}"
RELEASE_REPO="${HPX_RELEASE_REPO:-Sidiora-Labs/Paxeer-X-Network}"
BINARIES_URL="https://github.com/${RELEASE_REPO}/releases/download/paxeer-network/${PAXD_VERSION}"
BLOCKS_BEHIND_THRESHOLD=200
# Comma-separated node_id@host:port seeds from the private deploy env; at least three.
SEED_PEERS="${HPX_SEED_PEERS:-}"

say() { printf '\033[0;36m[publish]\033[0m %s\n' "$*"; }
die() { printf '\033[0;31m[publish] ERROR:\033[0m %s\n' "$*" >&2; exit 1; }

required=(
  "$SRC_BIN"
  "$SRC_CFG/genesis.json"
  "$SRC_CFG/config.toml"
  "$SRC_CFG/app.toml"
  "$WASM_ROOT/wasm-runtime/internal/api/libwasmvm.x86_64.so"
  "$WASM_ROOT/wasm-runtime/internal/api/libwasmvm.aarch64.so"
  "$WASM_ROOT/wasm/x/wasm/artifacts/v152/api/libwasmvm152.x86_64.so"
  "$WASM_ROOT/wasm/x/wasm/artifacts/v152/api/libwasmvm152.aarch64.so"
  "$WASM_ROOT/wasm/x/wasm/artifacts/v155/api/libwasmvm155.x86_64.so"
  "$WASM_ROOT/wasm/x/wasm/artifacts/v155/api/libwasmvm155.aarch64.so"
  "$HPX_DIR/hpx"
  "$HPX_DIR/get-hpx.sh"
  "$HPX_DIR/install.sh"
  "$HPX_DIR/uninstall.sh"
)
for file in "${required[@]}"; do
  [ -f "$file" ] || die "required input not found: $file"
done

[[ "$PAXD_VERSION" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || die "invalid paxd release tag: $PAXD_VERSION"
IFS=, read -r -a seeds <<< "$SEED_PEERS"
[ "${#seeds[@]}" -ge 3 ] || die "HPX_SEED_PEERS must list at least three seeds"
seeds_json=$(printf '%s\n' "${seeds[@]}" | jq -R . | jq -sc .)
for seed in "${seeds[@]}"; do
  [[ "$seed" =~ ^[0-9a-f]{40}@[^@:[:space:]]+:[0-9]+$ ]] || die "invalid seed: $seed"
done
[ "$(printf '%s\n' "${seeds[@]}" | sort -u | wc -l)" -eq "${#seeds[@]}" ] || die "duplicate seed in HPX_SEED_PEERS"

genesis_chain_id=$(jq -er '.chain_id' "$SRC_CFG/genesis.json") \
  || die "cannot read chain_id from $SRC_CFG/genesis.json"
[ "$genesis_chain_id" = "$CHAIN_ID" ] \
  || die "genesis chain_id $genesis_chain_id does not match $CHAIN_ID"

source_revision=$(git -C "$MONOREPO_ROOT" rev-parse HEAD 2>/dev/null || true)
[ -n "$source_revision" ] || die "cannot resolve monorepo source revision"
paxd_sha=$(sha256sum "$SRC_BIN" | awk '{print $1}')
release_id="${HPX_RELEASE_ID:-$(date -u +%Y%m%dT%H%M%SZ)-${paxd_sha:0:12}}"
[[ "$release_id" =~ ^[A-Za-z0-9._-]+$ ]] || die "invalid release id: $release_id"

mkdir -p "$RELEASES_DIR"
stage=$(mktemp -d "${RELEASES_DIR}/.stage.XXXXXXXX")
cleanup() { [ -n "${stage:-}" ] && [ -d "$stage" ] && chmod -R u+w "$stage" && rm -rf "$stage"; }
trap cleanup EXIT
mkdir -p "$stage/lib" "$stage/config/fullnode" "$stage/config/validator"

say "staging paxd from ${SRC_BIN}"
install -m 0755 "$SRC_BIN" "$stage/paxd"
printf '%s  paxd\n' "$paxd_sha" > "$stage/paxd.sha256"

runtime_path="$WASM_ROOT/wasm-runtime/internal/api:$WASM_ROOT/wasm/x/wasm/artifacts/v152/api:$WASM_ROOT/wasm/x/wasm/artifacts/v155/api"
binary_info=$(LD_LIBRARY_PATH="$runtime_path" "$SRC_BIN" version --long --output json 2>/dev/null) \
  || die "published paxd cannot report its build identity with the required native libraries"
paxd_commit=$(printf '%s' "$binary_info" | jq -er '.commit') \
  || die "published paxd has no embedded source commit"

say "staging all supported libwasmvm runtimes"
install -m 0644 "$WASM_ROOT/wasm-runtime/internal/api/libwasmvm.x86_64.so" "$stage/lib/"
install -m 0644 "$WASM_ROOT/wasm-runtime/internal/api/libwasmvm.aarch64.so" "$stage/lib/"
install -m 0644 "$WASM_ROOT/wasm/x/wasm/artifacts/v152/api/libwasmvm152.x86_64.so" "$stage/lib/"
install -m 0644 "$WASM_ROOT/wasm/x/wasm/artifacts/v152/api/libwasmvm152.aarch64.so" "$stage/lib/"
install -m 0644 "$WASM_ROOT/wasm/x/wasm/artifacts/v155/api/libwasmvm155.x86_64.so" "$stage/lib/"
install -m 0644 "$WASM_ROOT/wasm/x/wasm/artifacts/v155/api/libwasmvm155.aarch64.so" "$stage/lib/"

install -m 0644 "$SRC_CFG/genesis.json" "$stage/genesis.json"

make_config() {
  local mode="$1" dst="$2"
  sed -E \
    -e 's|^moniker = .*|moniker = "hpx-node"|' \
    -e "s|^mode = .*|mode = \"$mode\"|" \
    -e 's|^external-address = .*|external-address = ""|' \
    -e 's|^persistent-peers = .*|persistent-peers = ""|' \
    -e 's|^bootstrap-peers = .*|bootstrap-peers = ""|' \
    -e 's|^pex = .*|pex = true|' \
    -e "s|^blocks-behind-threshold = .*|blocks-behind-threshold = $BLOCKS_BEHIND_THRESHOLD|" \
    "$SRC_CFG/config.toml" > "$dst"
  grep -qx "blocks-behind-threshold = $BLOCKS_BEHIND_THRESHOLD" "$dst" \
    || die "$SRC_CFG/config.toml has no blocks-behind-threshold setting"
}

say "staging fullnode and validator configurations"
make_config full "$stage/config/fullnode/config.toml"
install -m 0644 "$SRC_CFG/app.toml" "$stage/config/fullnode/app.toml"
make_config validator "$stage/config/validator/config.toml"
install -m 0644 "$SRC_CFG/app.toml" "$stage/config/validator/app.toml"

for script in hpx get-hpx.sh install.sh uninstall.sh; do
  install -m 0755 "$HPX_DIR/$script" "$stage/$script"
done

cat > "$stage/chain-info.json" <<JSON
{
  "chain_id": "$CHAIN_ID",
  "evm_chain_id": $EVM_CHAIN_ID,
  "paxd_version": "$PAXD_VERSION",
  "paxd_commit": "$paxd_commit",
  "paxd_sha256": "$paxd_sha",
  "p2p_port": $P2P_PORT,
  "rpc_port": $RPC_PORT,
  "seeds": $seeds_json,
  "binaries_url": "$BINARIES_URL",
  "blocks_behind_threshold": $BLOCKS_BEHIND_THRESHOLD,
  "release_id": "$release_id",
  "source_revision": "$source_revision",
  "published_at": "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
}
JSON

say "writing sorted SHA-256 manifest"
(
  cd "$stage"
  find . -type f ! -name checksums.txt -print0 \
    | LC_ALL=C sort -z \
    | xargs -0 sha256sum \
    | sed 's#  \./#  #'
) > "$stage/checksums.txt"

release_dir="$RELEASES_DIR/$release_id"
[ ! -e "$release_dir" ] || die "release already exists: $release_dir"
chmod -R a-w "$stage"
chmod 0555 "$stage"
mv "$stage" "$release_dir"
stage=""

next_link="$ARTIFACTS_ROOT/.current.${release_id}"
ln -s "releases/$release_id" "$next_link"
mv -Tf "$next_link" "$ARTIFACTS_ROOT/current"
trap - EXIT

say "published ${release_id} at ${ARTIFACTS_ROOT}/current"
say "source revision ${source_revision}"
find "$release_dir" -type f -printf '%P\n' | sort | sed 's/^/    /'
