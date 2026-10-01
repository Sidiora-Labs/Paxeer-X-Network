#!/usr/bin/env bash
set -euo pipefail
umask 077
script_dir=$(CDPATH='' cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
exec timeout --signal=TERM --kill-after=5s 120s python3 "$script_dir/candidate.py" inventory "$@"
