#!/bin/sh
set -eu

scanner=scripts/workspace/check-paxeer-manifests.sh
roots=$(sed -n 's/^chain_roots="\(.*\)"$/\1/p' "$scanner")
test -n "$roots"
for root in $roots; do
    if test -z "$(git ls-files -- "$root" | head -n 1)"; then
        echo "chain root $root has no tracked files in a clean checkout" >&2
        exit 1
    fi
done
sh "$scanner"
