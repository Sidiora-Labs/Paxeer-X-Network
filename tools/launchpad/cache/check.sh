#!/usr/bin/env bash
set -eu
: "${L09_REDIS_URL:?set the private fixture Redis URL}"
: "${L09_NAMESPACE:?set the lane allocated namespace}"
case "$L09_NAMESPACE" in kd-421d7541932d2fd2136b3973*) ;; *) exit 2;; esac
export GOWORK=off GOMAXPROCS=2 GOFLAGS=-mod=readonly
root_dir="$(cd "$(dirname "$0")/../../.." && pwd)"
go -C "$root_dir/services/launchpad/api" test -p 1 -count=1 -race -json ./internal/cache
go -C "$root_dir/sdk/launchpad/shared" test -p 1 -count=1 -race -json ./redis
