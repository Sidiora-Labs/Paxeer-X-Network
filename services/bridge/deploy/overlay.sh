# Sourced by the bridge deploy scripts. When BRIDGE_DEPLOY_OVERLAY names the
# private deploy overlay, apply_overlay writes the overlaid configuration of
# $chain into a private directory and points $config at it, so every check the
# script runs afterwards reads the owner, deployer, attestors, environment
# names, caps and acknowledgements the overlay fills in. Without the variable
# the committed configuration is read as it is, placeholders and all.
OVERLAY_ROOT=""

apply_overlay() {
    [ -n "${BRIDGE_DEPLOY_OVERLAY:-}" ] || return 0
    [ -r "$BRIDGE_DEPLOY_OVERLAY" ] \
        || fail "BRIDGE_DEPLOY_OVERLAY names $BRIDGE_DEPLOY_OVERLAY, which is not readable"
    command -v jq > /dev/null 2>&1 || fail "jq is required and is not on the PATH"
    OVERLAY_ROOT=$(mktemp -d)
    chmod 0700 "$OVERLAY_ROOT"
    mkdir "$OVERLAY_ROOT/$chain"
    jq --slurpfile overlay "$BRIDGE_DEPLOY_OVERLAY" -f "$(dirname "${BASH_SOURCE[0]}")/overlay.jq" "$config" \
        > "$OVERLAY_ROOT/$chain/config.json" 2> "$OVERLAY_ROOT/error" \
        || fail "$BRIDGE_DEPLOY_OVERLAY: $(cat "$OVERLAY_ROOT/error")"
    config="$OVERLAY_ROOT/$chain/config.json"
}

# The eight EVM chains in deployment order; solana is the ninth chain.
EVM_CHAINS=(ethereum base arbitrum optimism bnb polygon avalanche hyperevm)

# run_all <script> <chain>... [-- <option>...] runs a deploy script once per
# chain, in order, stopping at the first chain that fails. Each chain reads and
# writes its own record, $PAXEER_BRIDGE_DEPLOYMENT_RECORD_DIR/<chain>.json.
run_all() {
    local script=$1 chain
    shift
    local chains=() options=()
    while [ $# -gt 0 ] && [ "$1" != -- ]; do
        chains+=("$1")
        shift
    done
    [ $# -eq 0 ] || {
        shift
        options=("$@")
    }
    [ -n "${PAXEER_BRIDGE_DEPLOYMENT_RECORD_DIR:-}" ] \
        || fail "PAXEER_BRIDGE_DEPLOYMENT_RECORD_DIR is required with --all and is not set"
    [ -d "$PAXEER_BRIDGE_DEPLOYMENT_RECORD_DIR" ] \
        || fail "$PAXEER_BRIDGE_DEPLOYMENT_RECORD_DIR does not exist"
    for chain in "${chains[@]}"; do
        printf '%s: %s\n' "$(basename "$script")" "$chain" >&2
        PAXEER_BRIDGE_DEPLOYMENT_RECORD="$PAXEER_BRIDGE_DEPLOYMENT_RECORD_DIR/$chain.json" \
            bash "$script" "${options[@]}" "$chain" || fail "$chain failed; the chains after it were not run"
    done
}
