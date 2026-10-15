#!/usr/bin/env bash
# Compare the GitHub release of a tag with the build host's release directory: the asset names equal the
# directory's file names, every SHA256SUMS line of the publish runner equals the build host's line, and
# every downloaded asset and every local file matches its own SHA256SUMS.
# Usage: verify-release.sh --tag paxeer-network/vX.Y.Z --dist dist/paxeer-network/vX.Y.Z
set -euo pipefail
export LC_ALL=C

usage() {
  echo "verify-release: usage: verify-release.sh --tag <tag> --dist <release directory>" >&2
  exit 2
}

# Sorted names of the regular files directly under $1.
dir_files() {
  find "$1" -mindepth 1 -maxdepth 1 -type f -printf '%f\n' | sort
}

# Sorted names listed by the SHA256SUMS file $1; fails on a malformed or duplicate line.
sums_names() {
  awk '
    !(substr($0, 1, 64) ~ /^[0-9a-f]+$/ && length($0) > 66 && substr($0, 65, 2) == "  " && substr($0, 67) !~ /\//) { printf "verify-release: malformed line %d of %s\n", NR, FILENAME > "/dev/stderr"; bad = 1; next }
    { name = substr($0, 67); if (seen[name]++) { printf "verify-release: %s listed twice in %s\n", name, FILENAME > "/dev/stderr"; bad = 1 } print name }
    END { exit bad }
  ' "$1" | sort
}

# assets_match_build <release assets dir> <build release dir>: 0 and the match line when the published
# assets equal the build host's release directory, 1 naming every differing asset otherwise.
assets_match_build() {
  local release=$1 dist=$2 d n bad=0
  for d in "$release" "$dist"; do
    if [ ! -f "$d/SHA256SUMS" ]; then
      echo "verify-release: $d has no SHA256SUMS" >&2
      return 1
    fi
  done

  local missing extra
  missing=$(comm -13 <(dir_files "$release") <(dir_files "$dist"))
  extra=$(comm -23 <(dir_files "$release") <(dir_files "$dist"))
  while IFS= read -r n; do
    [ -n "$n" ] || continue
    echo "verify-release: $n is not a release asset" >&2
    bad=1
  done <<<"$missing"
  while IFS= read -r n; do
    [ -n "$n" ] || continue
    echo "verify-release: release asset $n is not in $dist" >&2
    bad=1
  done <<<"$extra"

  local build_names publish_names
  build_names=$(sums_names "$dist/SHA256SUMS") || bad=1
  publish_names=$(sums_names "$release/SHA256SUMS") || bad=1
  if [ "$build_names" != "$(dir_files "$dist" | grep -vx SHA256SUMS)" ]; then
    echo "verify-release: SHA256SUMS of $dist does not list exactly the other files of the directory" >&2
    bad=1
  fi
  if [ "$publish_names" != "$(dir_files "$release" | grep -vx SHA256SUMS)" ]; then
    echo "verify-release: SHA256SUMS of the release does not list exactly the other release assets" >&2
    bad=1
  fi

  awk '
    FILENAME == ARGV[1] { build[substr($0, 67)] = $1; next }
    { name = substr($0, 67); publish[name] = $1 }
    END {
      for (name in build) {
        if (!(name in publish)) { printf "verify-release: %s has no SHA256SUMS line on the publish runner\n", name > "/dev/stderr"; bad = 1 }
        else if (publish[name] != build[name]) { printf "verify-release: %s sum differs: build %s, publish %s\n", name, build[name], publish[name] > "/dev/stderr"; bad = 1 }
      }
      for (name in publish) if (!(name in build)) { printf "verify-release: %s has no SHA256SUMS line on the build host\n", name > "/dev/stderr"; bad = 1 }
      exit bad
    }
  ' "$dist/SHA256SUMS" "$release/SHA256SUMS" || bad=1

  for d in "$release" "$dist"; do
    (cd "$d" && sha256sum --check --strict --quiet SHA256SUMS) >&2 || {
      echo "verify-release: files of $d do not match its SHA256SUMS" >&2
      bad=1
    }
  done

  [ "$bad" = 0 ] || return 1
  echo "verify-release: $(dir_files "$release" | wc -l) assets match"
}

main() {
  local tag="" dist=""
  while [ "$#" -gt 0 ]; do
    case "$1" in
      --tag) [ "$#" -ge 2 ] || usage; tag=$2; shift 2 ;;
      --dist) [ "$#" -ge 2 ] || usage; dist=$2; shift 2 ;;
      *) usage ;;
    esac
  done
  [ -n "$tag" ] && [ -n "$dist" ] || usage
  if [ ! -d "$dist" ]; then
    echo "verify-release: no release directory $dist" >&2
    return 1
  fi

  local work
  work=$(mktemp -d)
  # shellcheck disable=SC2064
  trap "rm -rf -- '$work'" EXIT
  gh release view "$tag" --json assets --jq '.assets[].name' > "$work/assets" || {
    echo "verify-release: gh release view $tag failed" >&2
    return 1
  }
  gh release download "$tag" --dir "$work/release" || {
    echo "verify-release: gh release download $tag failed" >&2
    return 1
  }
  if [ "$(sort "$work/assets")" != "$(dir_files "$work/release")" ]; then
    echo "verify-release: downloaded assets differ from the asset list of $tag" >&2
    return 1
  fi
  assets_match_build "$work/release" "$dist"
}

if [ "${BASH_SOURCE[0]}" = "$0" ]; then
  main "$@"
fi
