#!/usr/bin/env bash
# Builds the private charting library image from a local bundle; the bundle never enters this repository.
set -euo pipefail

image=ghcr.io/sidiora-labs/hyperpax-charting-library
source_dir=/root/lx-lanes/hyperpax
push=0

while [ $# -gt 0 ]; do
  case "$1" in
    --source) source_dir="$2"; shift 2 ;;
    --image) image="$2"; shift 2 ;;
    --push) push=1; shift ;;
    *) echo "usage: $0 [--source DIR] [--image NAME] [--push]" >&2; exit 2 ;;
  esac
done

static="$source_dir"
[ -d "$source_dir/public/static" ] && static="$source_dir/public/static"

if [ ! -f "$static/charting_library/charting_library.js" ] || [ ! -d "$static/datafeeds" ]; then
  echo "error: $static lacks charting_library/charting_library.js or datafeeds" >&2
  exit 2
fi

if [ "$push" = 1 ]; then
  config="${DOCKER_CONFIG:-$HOME/.docker}/config.json"
  if [ ! -f "$config" ] || ! grep -q '"ghcr.io"' "$config"; then
    echo "error: --push needs a docker login ghcr.io session" >&2
    exit 2
  fi
fi

tag=$(cd "$static" && find charting_library datafeeds -type f -print0 | LC_ALL=C sort -z | xargs -0 sha256sum | sha256sum | cut -c1-64)

docker build -q -f "$(dirname "$0")/charting-library.Dockerfile" -t "$image:$tag" -t "$image:latest" "$static" >&2

if [ "$push" = 1 ]; then
  docker push "$image:$tag" >&2
  docker push "$image:latest" >&2
fi

echo "$tag"
