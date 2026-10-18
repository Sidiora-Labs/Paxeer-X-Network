#!/usr/bin/env bash
set -eu
: "${L09_REDIS_URL:?set allocated real Redis URL}"
: "${L09_DATABASE_URL:?set allocated disposable PostgreSQL URL}"
: "${L09_NAMESPACE:?set allocated cache namespace}"
case "$L09_NAMESPACE" in kd-421d7541932d2fd2136b3973*) ;; *) exit 2;; esac
export GOWORK=off GOMAXPROCS=2 GOFLAGS=-mod=readonly
root_dir="$(cd "$(dirname "$0")/../.." && pwd)"
if [ "${L09_PHASE:-2}" = "4" ]; then
  : "${L09_CAPACITY_CONFIG:?set the actual provider measurement configuration}"
  : "${L09_CAPACITY_CONFIG_SHA256:?set its exact SHA256}"
  : "${L09_CAPACITY_OUTPUT:?set a fresh lane-owned measurement output directory}"
  python3 "$root_dir/tools/cache/test_capacity.py"
  go -C "$root_dir/core/api" test -p 1 -count=1 -json ./internal/cache -run 'TestRealAdapterMeasurementCapture$'
  python3 "$root_dir/tools/cache/capacity.py" --config "$L09_CAPACITY_CONFIG" --config-sha256 "$L09_CAPACITY_CONFIG_SHA256" --output "$L09_CAPACITY_OUTPUT" --harness-check
  exit
fi
if [ "${L09_PHASE:-2}" = "3" ]; then
  : "${L09_IDENTITY_ENDPOINT:?set the real fixture identity endpoint}"
  : "${L09_IDENTITY_SERVICE_TOKEN:?set the allocated fixture service credential}"
  : "${L09_WALLET_A_ACCESS:?set the first admitted wallet session credential}"
  : "${L09_WALLET_B_ACCESS:?set the second admitted wallet session credential}"
  : "${L09_EXCLUSIVE_REDIS_DB:?set the exclusive nonzero Redis fixture database number}"
  go -C "$root_dir/shared" test -p 1 -count=1 -json ./redis -run 'TestRealPolicySnapshotOrdering$'
  go -C "$root_dir/core/api" test -p 1 -count=1 -json ./internal/cache -run 'Test(IndependentCancelledWaitersAndActiveCompletion|RealTwoAuthenticatedWalletCacheIsolation|RealHotExpiryAcrossTenReplicas|RealExclusiveRedisFlushPreservesDurableTruth)$'
  exit
fi
go -C "$root_dir/shared" test -p 1 -count=1 -json ./redis -run 'Test(PolicySensitiveStaleRejected|RealPolicySnapshotOrdering|ProjectionIsolation|RealRedisPrimitives)$'
go -C "$root_dir/core/api" test -p 1 -count=1 -json ./internal/cache -run 'Test(RoutePoliciesRejectSensitiveStale|EmergencyAllocationSumAndScope|LocalPolicyRevisionFloorAndBounds|GenerationCutoverAndCopies|RealPostgresOriginBudgetAndRedisLoss)$'
