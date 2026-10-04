#!/usr/bin/env bash
set -euo pipefail
repo_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)
cd "$repo_root"
for test_file in program-lifecycle program-response-code; do
  artifact="agent/sdk/typescript/dist/test/${test_file}.test.js"
  if [[ ! -f "$artifact" ]]; then
    echo "TypeScript Programs prebuilt test artifact missing: $artifact" >&2
    exit 78
  fi
  node "$artifact"
done

node tests/sdk/typescript/program-terminal-v5.test.mjs
