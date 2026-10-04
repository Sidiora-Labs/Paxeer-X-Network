#!/bin/sh
set -eu
repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
exec python3 "$repo_root/scripts/programs/generate-abi-vectors.py" --check
