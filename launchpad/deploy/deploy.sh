#!/usr/bin/env bash
# deploy.sh — registry-agnostic build+push for one KindleLaunch service.
# D6 (registry/host) is deferred: the registry is supplied via $REGISTRY, never
# hard-coded. With no $REGISTRY set, the image is built locally only.
#
# Usage:
#   deploy/deploy.sh <group>/<svc> <bin>
#   REGISTRY=ghcr.io/Sidiora-Technologies TAG=v0.1.0 deploy/deploy.sh core/indexer indexerd
set -euo pipefail

SERVICE="${1:?usage: deploy.sh <group>/<svc> <bin>}"
BIN="${2:?usage: deploy.sh <group>/<svc> <bin>}"
TAG="${TAG:-dev}"
REGISTRY="${REGISTRY:-}"

slug="kindlelaunch-$(echo "$SERVICE" | tr '/' '-')"
if [ -n "$REGISTRY" ]; then
	image="${REGISTRY}/${slug}:${TAG}"
else
	image="${slug}:${TAG}"
fi

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
echo ">> building $image (SERVICE=$SERVICE BIN=$BIN)"
docker build \
	-f "$repo_root/deploy/Dockerfile.svc" \
	--build-arg "SERVICE=$SERVICE" \
	--build-arg "BIN=$BIN" \
	-t "$image" \
	"$repo_root"

if [ -n "$REGISTRY" ]; then
	echo ">> pushing $image"
	docker push "$image"
else
	echo ">> REGISTRY unset (D6 deferred): built local image only: $image"
fi
