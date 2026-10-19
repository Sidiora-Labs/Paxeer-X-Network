# seictl smoke runbook

End-to-end happy-path validation against the harbor cluster. Run this before declaring seictl MVP-shipped, and after any change that touches IAM, Pod Identity, manifest rendering, or the apply path.

Estimated time: 20 minutes (includes the 5-minute bench duration).

## Pre-flight

```sh
# 1. Build the binary (or pull a release).
make build && export PATH="$PWD/build:$PATH"
seictl --version

# 2. Confirm AWS caller is the harbor account.
aws sts get-caller-identity --query Account --output text
# expect: 189176372795
# If not, switch profiles: aws sso login --profile <harbor-profile>

# 3. Confirm kubectl context points at harbor.
kubectl config current-context
# expect: harbor (or whatever your kubeconfig calls it)

# 4. Confirm sei-k8s-controller is running and the SeiNodeDeployment CRD exists.
kubectl get crd seinodedeployments.sei.io -o name
# expect: customresourcedefinition.apiextensions.k8s.io/seinodedeployments.sei.io
```

## Step 1 — onboard

`onboard` provisions the engineer's IAM role + Pod Identity association + cell manifests. `--no-pr` skips the platform-repo PR (we don't want to clutter `sei-protocol/platform` with a smoke run); side effects on AWS + the local config file still happen with `--apply`.

```sh
seictl onboard --alias bdchatham --no-pr --apply | jq .
```

**Expected envelope:**

```jsonc
{
  "kind": "seictl.OnboardResult",
  "data": {
    "alias": "bdchatham",
    "namespace": "eng-bdchatham",
    "configPath": "/Users/brandon/.seictl/config.json",
    "generatedFiles": [
      "clusters/harbor/engineers/bdchatham/namespace.yaml",
      "clusters/harbor/engineers/bdchatham/workload-service-account.yaml",
      "clusters/harbor/engineers/bdchatham/kustomization.yaml"
    ],
    "awsResources": [
      {"kind": "Policy", "arn": "arn:aws:iam::189176372795:policy/seictl/harbor-workload-eng-bdchatham", "action": "create"},
      {"kind": "Role", "arn": "arn:aws:iam::189176372795:role/seictl/harbor-workload-eng-bdchatham", "action": "create"},
      {"kind": "Attachment", "arn": "...", "action": "create"},
      {"kind": "PodIdentityAssociation", "arn": "...", "action": "create"}
    ],
    "dryRun": false
  }
}
```

**Verify the side effects:**

```sh
# Config file written with mode 0600.
ls -la ~/.seictl/config.json
cat ~/.seictl/config.json
# expect: {"alias": "bdchatham", "namespace": "eng-bdchatham"}

# IAM role exists.
aws iam get-role --role-name harbor-workload-eng-bdchatham --query 'Role.Arn'

# Pod Identity association exists.
aws eks list-pod-identity-associations \
  --cluster-name harbor \
  --namespace eng-bdchatham \
  --query 'associations[?serviceAccount==`workload-service-account`]'
```

**Note:** the namespace + SA themselves don't exist in the cluster yet — those get created by Flux *after* the onboard PR merges into platform. For the smoke run, create them manually:

```sh
kubectl create namespace eng-bdchatham
kubectl create serviceaccount workload-service-account -n eng-bdchatham
```

(In the real engineer flow, you'd run `seictl onboard` *without* `--no-pr`, the platform PR merges, Flux applies the manifests, and these resources appear without manual intervention.)

## Step 2 — bench up

Pin to a known seid digest. `mock-` variants (built with the `mock_balances` tag) are required for any bench that uses seiload's EVMTransfer profile, since the profile sends from pre-seeded accounts that only exist in the mock build.

```sh
# Resolve the latest sei-chain main SHA → mock image.
SEI_SHA=$(gh api repos/sei-protocol/sei-chain/commits/main --jq '.sha')
SEID_IMAGE="189176372795.dkr.ecr.us-east-2.amazonaws.com/sei/sei-chain:mock-${SEI_SHA}"
echo "Pinning to: $SEID_IMAGE"

# Confirm the image actually exists in ECR (sei-chain CI may still be publishing).
aws ecr describe-images \
  --repository-name sei/sei-chain \
  --image-ids "imageTag=mock-${SEI_SHA}" \
  --query 'imageDetails[0].imageDigest'

seictl bench up \
  --image "$SEID_IMAGE" \
  --name smoke \
  --size s \
  --duration 5 \
  --apply | jq .
```

**Expected envelope** (`size s` = 4 validators + 1 RPC):

```jsonc
{
  "kind": "seictl.BenchUpResult",
  "data": {
    "chainId": "bench-bdchatham-smoke",
    "name": "smoke",
    "namespace": "eng-bdchatham",
    "imageDigest": "sha256:...",
    "validators": 4,
    "rpcNodes": 1,
    "duration": "5m",
    "endpoints": {
      "tendermintRpc": ["http://bench-bdchatham-smoke-rpc-internal.eng-bdchatham.svc.cluster.local:26657"],
      "evmJsonRpc": ["http://bench-bdchatham-smoke-rpc-internal.eng-bdchatham.svc.cluster.local:8545"]
    },
    "manifests": [
      {"kind": "ConfigMap", "name": "seiload-profile-bench-bdchatham-smoke", "action": "create"},
      {"kind": "SeiNodeDeployment", "name": "bench-bdchatham-smoke", "action": "create"},
      {"kind": "SeiNodeDeployment", "name": "bench-bdchatham-smoke-rpc", "action": "create"},
      {"kind": "Job", "name": "seiload-bench-bdchatham-smoke", "action": "create"}
    ],
    "appliedAt": "..."
  }
}
```

**Wait for validators to reach `Running`:**

```sh
kubectl get snd -n eng-bdchatham -w
# Ctrl-C once both SNDs show readyReplicas matching desired (4 validators, 1 rpc)
```

## Step 3 — verify the chain is alive

Hit the EVM JSON-RPC endpoint from inside the cluster (no port-forward; matches what seiload does):

```sh
kubectl run curl-smoke --rm -i --restart=Never \
  --image=curlimages/curl:8.10.1 \
  -n eng-bdchatham \
  -- curl -s -X POST \
    -H 'Content-Type: application/json' \
    -d '{"jsonrpc":"2.0","method":"eth_blockNumber","id":1,"params":[]}' \
    http://bench-bdchatham-smoke-rpc-internal.eng-bdchatham.svc.cluster.local:8545
```

**Expected:** a JSON response with a non-zero `result` (block number in hex).

**Tail the seiload Job:**

```sh
kubectl logs -n eng-bdchatham -f job/seiload-bench-bdchatham-smoke
# expect: load profile starts, transactions submitting, target TPS reported
```

## Step 4 — bench list

```sh
seictl bench list | jq .
```

**Expected:** one item with `chainId: bench-bdchatham-smoke`, `validatorsReady: 4`, `rpcReady: 1`, `loadJobPhase: Running`.

## Step 5 — bench down

```sh
# Dry-run first to preview what gets deleted.
seictl bench down --name smoke --dry-run | jq .

# Then actually delete.
seictl bench down --name smoke --apply | jq .
```

**Expected:** `deletedAt` populated; if any resource is `still-terminating`, the `hint` field will say so.

**Verify the namespace is clean:**

```sh
kubectl get snd,job,cm -n eng-bdchatham
# expect: no resources from the bench (only the workload-service-account + namespace remain)
```

## Step 6 — verify S3 results landed

```sh
aws s3 ls s3://harbor-validation-results/eng-bdchatham/evm-transfer/smoke/
# expect: report.log
aws s3 cp s3://harbor-validation-results/eng-bdchatham/evm-transfer/smoke/report.log -
# expect: seiload's stdout — TPS, latencies, etc.
```

## Cleanup (post-smoke)

```sh
# Remove the smoke namespace (not normally needed once the engineer-cell PR lands).
kubectl delete namespace eng-bdchatham

# Remove the local config file (optional — only if you want to re-test the missing-config error path).
rm ~/.seictl/config.json
```

## Common failures

| Symptom | Likely cause | Fix |
|---|---|---|
| `aws-create-failed` on onboard | Wrong AWS profile / SSO expired | `aws sso login`, retry |
| `image-resolution` error on bench up | `mock-<sha>` not yet published in ECR | Wait 60s for sei-chain CI to finish, retry; or pin to an older known-good `mock-<sha>` |
| Pods stuck in `ImagePullBackOff` | ECR pull from kubelet failed | `kubectl describe pod` — check for `403` (IAM) or `not found` (digest mismatch) |
| Validators never reach Ready | Genesis ceremony hung | `kubectl logs` validator-0 — look for `Stalling` or `panic`; usually a bad seid image |
| EVM RPC returns connection refused | RPC SND not yet block-syncing | Wait — RPC follows validators; can take 30-60s after validators are Ready |
| S3 results missing after teardown | seiload Job failed before completing | Check `kubectl logs -p job/seiload-bench-bdchatham-smoke` for the failed pod's stdout |

## Sign-off

When all steps pass without manual intervention, seictl is MVP-shipped end-to-end on harbor.
