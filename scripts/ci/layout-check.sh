#!/usr/bin/env bash
#
# layout-check.sh
#
# Proves that a moved tree left nothing behind at its old path. For every old
# root-relative path given, it fails when the index still lists a file under
# that path, or when a file whose paths resolve from the repository root names
# the path as a root-anchored directory: the path followed by a slash and not
# preceded by a letter, digit, dot, dash, underscore or slash.
#
# The files searched are the root build files (Makefile, chain.mk, the .mk
# files under tools, go.mod, buf.yaml, buf.gen.yaml, foundry.toml,
# foundry.paxeer.toml, the root package.json), the CI, container and deploy
# trees (.github, docker, deploy), the shell and Python scripts under scripts,
# and the root tool configuration (codify.kvx, .gitignore, .cgignore,
# .gitattributes, .pre-commit-config.yaml). Manifests that resolve paths from
# their own directory are proven by the build gates instead.
#
# Run from anywhere:
#
#   scripts/ci/layout-check.sh <old-path> [<old-path> ...]
#
# Every file left under an old path is printed as its path and every reference
# as path:line.
#
# Exit codes: 0 when no old path is left or referenced, 1 when one is, 2 when
# the check cannot run.

set -euo pipefail

usage() {
    echo "usage: scripts/ci/layout-check.sh <old-path> [<old-path> ...]" >&2
    exit 2
}

[ "$#" -ge 1 ] || usage

command -v git >/dev/null 2>&1 || {
    echo "layout-check: git is required" >&2
    exit 2
}

root=$(CDPATH='' cd -- "$(dirname -- "$0")/../.." && pwd -P)
cd "$root"

git rev-parse --git-dir >/dev/null 2>&1 || {
    echo "layout-check: $root is not a git repository" >&2
    exit 2
}

searched=(
    Makefile
    chain.mk
    ':(glob)tools/**/*.mk'
    .github
    docker
    deploy
    ':(glob)scripts/**/*.sh'
    ':(glob)scripts/**/*.py'
    buf.yaml
    buf.gen.yaml
    codify.kvx
    package.json
    .gitignore
    .cgignore
    .gitattributes
    .pre-commit-config.yaml
    foundry.toml
    foundry.paxeer.toml
    go.mod
)

failed=0

for arg in "$@"; do
    old=${arg#./}
    while [ "${old%/}" != "$old" ]; do
        old=${old%/}
    done
    case "$old" in
        '' | /* | .. | ../* | */.. | */../*)
            echo "layout-check: $arg is not a root-relative path" >&2
            exit 2
            ;;
    esac

    left=$(git -c core.quotePath=false ls-files --cached -- ":(literal)$old")
    if [ -n "$left" ]; then
        printf '%s\n' "$left"
        failed=1
    fi

    escaped=$(printf '%s' "$old" | sed 's/[][\.*^$+?(){}|]/\\&/g')
    pattern="(^|[^A-Za-z0-9._/-])$escaped/"

    status=0
    hits=$(git -c core.quotePath=false grep -n -I -E -e "$pattern" -- "${searched[@]}") || status=$?
    case "$status" in
        0)
            printf '%s\n' "$hits" | cut -d: -f1,2
            failed=1
            ;;
        1) ;;
        *)
            echo "layout-check: git grep failed with exit $status for $old" >&2
            exit 2
            ;;
    esac
done

if [ "$failed" -ne 0 ]; then
    echo "layout-check: old paths are still present or referenced" >&2
fi
exit "$failed"
