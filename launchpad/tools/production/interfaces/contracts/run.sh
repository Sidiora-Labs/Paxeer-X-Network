#!/usr/bin/env bash
set -euo pipefail
interface_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
: "${CONTRACT_SOURCE_ROOT:?Set current read-only contract root}"
: "${CONTRACT_FREEZE_OUTPUT:?Set output directory}"
mkdir -p "$CONTRACT_FREEZE_OUTPUT"
export FIXTURE_OUTPUT="$CONTRACT_FREEZE_OUTPUT/golden-fixtures.json"
export HARDHAT_EXPERIMENTAL_ALLOW_NON_LOCAL_INSTALLATION=true
node "$interface_dir/export.cjs"
node "$interface_dir/client-deployment.cjs" generate
node "$CONTRACT_SOURCE_ROOT/node_modules/hardhat/internal/cli/cli.js" --config "$interface_dir/hardhat.config.cjs" run --no-compile "$interface_dir/fixture.cjs" --network hardhat
node "$interface_dir/validate.cjs"
