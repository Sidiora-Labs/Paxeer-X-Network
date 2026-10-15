#!/usr/bin/env bash
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)
version_file=$root/tools/release/version.json
work=$(mktemp -d)
trap 'rm -rf -- "$work"' EXIT
want=v6.12.0
semver_re='^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$'

# The comparison of .github/workflows/paxeer-release-check.yml:57-65 against the tags of repository $1.
newer_than_latest_tag() {
  local repo=$1 version=$2 latest latest_version greatest
  latest=$(git -C "$repo" tag --list 'paxeer-network/v*' --sort=-v:refname | head -n 1)
  if [[ -n "$latest" ]]; then
    latest_version=${latest#paxeer-network/}
    greatest=$(printf '%s\n%s\n' "$latest_version" "$version" | sort -V | tail -n 1)
    if [[ "$greatest" != "$version" || "$latest_version" == "$version" ]]; then
      return 1
    fi
  fi
}

version_and_upgrade_are_v6120() {
  local version upgrade
  version=$(jq -er '.version' "$version_file") || return 1
  upgrade=$(jq -er '.upgrade' "$version_file") || return 1
  [ "$version" = "$want" ] || { echo "version is $version, want $want" >&2; return 1; }
  [ "$upgrade" = "$want" ] || { echo "upgrade is $upgrade, want $want" >&2; return 1; }
  [ "$(jq -er 'keys | join(",")' "$version_file")" = "upgrade,version" ] || { echo "version.json carries keys beyond version and upgrade" >&2; return 1; }
}

upgrade_name_is_declared() {
  local upgrade decl
  upgrade=$(jq -er '.upgrade' "$version_file") || return 1
  decl="V6120Upgrade = \"$upgrade\""
  grep -rqF -- "$decl" "$root/node" "$root/modules" || { echo "no declaration $decl under node/ or modules/" >&2; return 1; }
  grep -qF -- "const $decl" "$root/node/upgrades_v6120.go" || { echo "node/upgrades_v6120.go lacks const $decl" >&2; return 1; }
}

version_is_newer_than_latest_tag() {
  local version repo=$work/tags
  version=$(jq -er '.version' "$version_file") || return 1
  [[ "$version" =~ $semver_re ]] || { echo "$version is not a semantic version" >&2; return 1; }
  git init -q "$repo"
  git -C "$repo" -c user.name=release-check -c user.email=release-check@invalid commit -q --allow-empty -m base
  git -C "$repo" tag paxeer-network/v6.8.0
  git -C "$repo" tag paxeer-network/v6.10.0
  [ "$(git -C "$repo" tag --list 'paxeer-network/v*' --sort=-v:refname | head -n 1)" = paxeer-network/v6.10.0 ] ||
    { echo "latest tag is not paxeer-network/v6.10.0" >&2; return 1; }
  newer_than_latest_tag "$repo" "$version" || { echo "$version is not newer than paxeer-network/v6.10.0" >&2; return 1; }
  if newer_than_latest_tag "$repo" v6.10.0; then echo "the latest version itself was accepted" >&2; return 1; fi
  if newer_than_latest_tag "$repo" v6.9.0; then echo "v6.9.0 was accepted after v6.10.0" >&2; return 1; fi
  if [[ v6.12 =~ $semver_re ]]; then echo "v6.12 matched the semantic version pattern" >&2; return 1; fi
  newer_than_latest_tag "$root" "$version" || { echo "$version is not newer than the latest tag of the repository" >&2; return 1; }
}

tag_is_free_and_derived() {
  local version tag derived
  version=$(jq -er '.version' "$version_file") || return 1
  tag="paxeer-network/$version"
  [ "$tag" = "paxeer-network/$want" ] || { echo "tag is $tag, want paxeer-network/$want" >&2; return 1; }
  if git -C "$root" rev-parse --verify --quiet "refs/tags/$tag" >/dev/null; then echo "release tag $tag already exists" >&2; return 1; fi
  grep -qF -- '--print-tag' "$root/tools/release/build-paxd.sh" ||
    { echo "build-paxd.sh has no --print-tag, and without it the script runs a full release build" >&2; return 1; }
  derived=$(bash "$root/tools/release/build-paxd.sh" --print-tag) || { echo "build-paxd.sh --print-tag failed" >&2; return 1; }
  [ "$derived" = "$tag" ] || { echo "build-paxd.sh derives $derived, want $tag" >&2; return 1; }
}

passed=0
failed=0
for t in version_and_upgrade_are_v6120 upgrade_name_is_declared version_is_newer_than_latest_tag tag_is_free_and_derived; do
  if (set -e; "$t"); then
    echo "PASS $t"
    passed=$((passed + 1))
  else
    echo "FAIL $t"
    failed=$((failed + 1))
  fi
done
echo "identity: $passed passed, $failed failed"
[ "$failed" -eq 0 ]
