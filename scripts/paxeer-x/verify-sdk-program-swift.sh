#!/usr/bin/env bash
set -euo pipefail
root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)
if ! command -v swift >/dev/null 2>&1; then
  printf '%s\n' 'Swift Programs qualification requires the actual Swift toolchain and prebuilt test bundle.' >&2
  exit 78
fi
if [[ -z "${PAXEER_X_PROGRAM_TERMINAL_V5_CORPUS:-}" || ! -r "$PAXEER_X_PROGRAM_TERMINAL_V5_CORPUS" ]]; then
  printf '%s\n' 'Swift terminal-v5 qualification requires the genuine native signed ABI3/4 execution corpus.' >&2
  exit 78
fi
exec swift test --skip-build --disable-automatic-resolution \
  --package-path "$root/platform/sdk/swift" \
  --filter 'ProgramsContractTests|ReceiptFixtureTests.testNativeSignedBinding|ReceiptFixtureTests.testNativeLifecycleCFixtures|ReceiptFixtureTests.testSignedTerminalV4Vectors'
