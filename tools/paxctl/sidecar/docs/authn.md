# Sidecar HTTP API authentication

The sidecar exposes its task API over plain HTTP. Authentication is
controlled by a single environment variable; the runtime trust
boundary is the pod, not the loopback interface.

## Environment contract

| Env | Values | Default | Behavior |
|---|---|---|---|
| `SEI_SIDECAR_AUTHN_MODE` | unset \| `unauthenticated` \| `trusted-header` | unset | unset/`unauthenticated`: bind all interfaces, no auth. `trusted-header`: bind loopback, require `X-Remote-User` on non-probe paths. |

Parsing is strict: any non-empty value other than `unauthenticated`
or `trusted-header` is a startup error so a typo cannot silently
degrade a hardened deployment.

## `trusted-header` mode

The sidecar is paired with an in-pod `kube-rbac-proxy` container on
TLS `:8443`. The proxy performs TokenReview + a single coarse
`create seinodetasks.sei.io` SubjectAccessReview against the K8s API,
then forwards passed requests to `127.0.0.1:7777` with `X-Remote-User`
naming the authenticated identity.

The sidecar requires exactly one non-empty `X-Remote-User` value per
request. Two values fail closed — that would mean the proxy is
appending rather than overwriting, which would let an attacker-
supplied header arrive first.

### Bypass paths

Four paths skip the `X-Remote-User` check because their callers do
not carry auth headers:

| Path | Caller |
|---|---|
| `/v0/healthz` | kubelet readiness probe |
| `/v0/startupz` | kubelet startup probe |
| `/v0/livez` | kubelet liveness probe |
| `/v0/metrics` | Prometheus scrape |

The `kube-rbac-proxy` `--allow-paths` flag must include all four so
probes and scrapes traverse the proxy without TokenReview. The
authoritative list is logged at sidecar startup in trusted-header
mode and is exposed programmatically via `server.BypassPaths()`.

Rejected requests are counted in the
`seictl_sidecar_authn_rejections_total{reason}` metric — labels
`missing_header`, `duplicate_header`, `empty_header` distinguish the
common misconfigurations.

### Pod-level isolation requirements

The trust boundary is the pod. Pods running in `trusted-header` mode
MUST NOT enable any of:

- `hostNetwork: true` — collapses loopback into the host's network
  namespace, exposing `127.0.0.1:7777` to every other `hostNetwork`
  pod on the node.
- `hostPID: true` — lets off-pod processes attach.
- `hostIPC: true` — exposes SysV IPC across the pod boundary.

A colocated container in the pod can still reach `127.0.0.1:7777`
directly and forge `X-Remote-User`. With `shareProcessNamespace:
true` (the current `SeiNode` default) it can also read
`/proc/<sidecar>/mem` and exfiltrate the unlocked keyring — memory
read is the load-bearing threat, not header forgery. The middleware
does not defend against either; the controller-side configuration of
the pod is what establishes the boundary.

## Controller-side contract

The contract this sidecar exposes to `sei-k8s-controller`:

- Env var: `SEI_SIDECAR_AUTHN_MODE=trusted-header`
- Internal port: `7777`, bound to `127.0.0.1`
- Probe paths: `/v0/healthz` (readiness), `/v0/startupz` (startup), `/v0/livez` (liveness)
- Prometheus path: `/v0/metrics`
- `kube-rbac-proxy --allow-paths`: `/v0/healthz,/v0/startupz,/v0/livez,/v0/metrics`
- Forwarded header: `X-Remote-User` — proxy MUST overwrite, not
  append
- Pod isolation: `hostNetwork`/`hostPID`/`hostIPC` all false

Kubelet probes hit the pod IP, not loopback. With the sidecar bound
loopback-only, probes must be routed through the proxy port.
