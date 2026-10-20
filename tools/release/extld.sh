#!/usr/bin/env bash
# External linker for the paxd release build: replaces the absolute -rpath entries cgo expands from ${SRCDIR} with $ORIGIN, where the release ships libwasmvm.
set -euo pipefail

args=()
rpath=0
for arg in "$@"; do
  case $arg in
    -Wl,-rpath,/*) rpath=1 ;;
    @*)
      grep -q '^"-Wl,-rpath,/' "${arg#@}" && rpath=1
      sed -i '/^"-Wl,-rpath,\//d' "${arg#@}"
      args+=("$arg")
      ;;
    *) args+=("$arg") ;;
  esac
done
if [ "$rpath" = 1 ]; then args+=('-Wl,-rpath,$ORIGIN'); fi
exec "${CC:-gcc}" "${args[@]}"
