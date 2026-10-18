#!/usr/bin/env bash
set -euo pipefail
fixture_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
: "${CONTRACT_SOURCE_ROOT:?Set read-only current contract root}"
: "${FIXTURE_OUTPUT:?Set isolated fixture output path}"
export HARDHAT_EXPERIMENTAL_ALLOW_NON_LOCAL_INSTALLATION=true
exec node "$CONTRACT_SOURCE_ROOT/node_modules/hardhat/internal/cli/cli.js" --config "$fixture_dir/hardhat.config.cjs" run --no-compile "$fixture_dir/fixture.cjs" --network hardhat
