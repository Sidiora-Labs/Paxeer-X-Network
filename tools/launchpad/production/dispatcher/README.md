# Fenced production worker dispatcher

This is a coordinator-operated local workflow tool, not an OS sandbox against malicious code running as the same user. Only the coordinator owns the private state directory, signing key, reviewed check registry and canonical task statuses. Workers receive an isolated workspace and the applicable task/lease identity. No command here edits canonical KVX or marks a canonical task done.

The repository has 50 logical lanes. The current AI tool advertises 31 total slots, including the coordinator: at most 30 AI workers, reduced by other active agents. Fifty simultaneous OS fixture processes are a separate qualification target. They do not establish 50 AI agents or product throughput.

## Coordinator lifecycle

Use the current specification and matching execution-plan projection. Graph layout identity excludes status; the execution-plan projection must still match the full current spec SHA and task states. After `cg spec start/done`, the canonical coordinator refreshes that projection before subsequent dispatcher commands. A worker must never do so.

Prepare coordinator-reviewed JSON capacity and per-worker resource files. Units are CPU cores, bytes for memory/disk, and integral PIDs/executor/build/container counts. All seven classes are required for capacity; a lease must reserve positive CPU/memory/PIDs/disk and exactly one executor. The coordinator supplies a fresh available-AI-slot observation from the actual agent tool, never the configured logical lane count. Admission also measures affinity/cgroup CPU, current memory/PID headroom and filesystem free space. These are resource reservations and point-in-time observations, not OS-enforced workload limits. The actual process harness records worker high-water RSS against its reserved envelope.

```bash
python3 tools/production/dispatcher/cli.py --state-dir /tmp/kindle-wave1-state init \
  --spec spec/production-system/spec.kvx \
  --plan spec/production-system/execution-plan.json \
  --source . --interface-digest "$FROZEN_INTERFACE_SHA256" \
  --capacity /tmp/kindle-wave1-capacity.json --available-ai-slots 20

python3 tools/production/dispatcher/cli.py --state-dir /tmp/kindle-wave1-state status
python3 tools/production/dispatcher/cli.py --state-dir /tmp/kindle-wave1-state observe-ai --available-ai-slots 20
python3 tools/production/dispatcher/cli.py --state-dir /tmp/kindle-wave1-state claim \
  --task 1.01 --owner worker-L01 --resources /tmp/kindle-worker-resources.json
```

`claim` returns the unique workspace and token file. Each attempt has four probed ports in its lane's reserved range, migration range, and distinct service/schema/subject/bucket namespace. A port probe is not an OS socket reservation: the service must bind it and reject failure. TTL defaults to 1,800 seconds; the coordinator heartbeats every 60 seconds, retaining the refreshed token. Expiry, coordinator restart, OS reboot or candidate rebinding invalidates older tokens. `restart` fences all unfinished leases; it preserves already accepted artifacts. Coordinate cleanup of the old owned processes before starting replacements.

```bash
python3 tools/production/dispatcher/cli.py --state-dir /tmp/kindle-wave1-state heartbeat --token "$TOKEN_FILE"
```

After reviewing the worker's owned changes, the coordinator binds the candidate, builds the patch and registers actual tests. The registry is coordinator-owned outside worker workspaces. Registration does not qualify anything. Unsupported future task checks remain absent and fail closed.

```bash
python3 tools/production/dispatcher/cli.py --state-dir /tmp/kindle-wave1-state candidate --token "$TOKEN_FILE"
python3 tools/production/dispatcher/cli.py --state-dir /tmp/kindle-wave1-state patch --token "$TOKEN_FILE"
python3 tools/production/dispatcher/cli.py --state-dir /tmp/kindle-wave1-state register-checks \
  --task 1.01 --checks /tmp/reviewed-L01-checks.json
python3 tools/production/dispatcher/cli.py --state-dir /tmp/kindle-wave1-state qualify --token "$TOKEN_FILE"
python3 tools/production/dispatcher/cli.py --state-dir /tmp/kindle-wave1-state accept --token "$TOKEN_FILE"
```

A reviewed check file is a nonempty JSON array. Every check specifies a unique `id`, command argv, positive `timeout` and a proof policy: `go-test-json`, `unittest` or `node-tap`. Use meaningful native tests for the task's actual acceptance criteria. The parser requires positive named passes and rejects skipped/empty/failed output. Input paths are included source files; optional output files must be absolute paths outside source. Frozen locks/toolchain and actual interpreter/checker bytes are separately bound. Example shape:

```json
[{"id":"L01-read-policy","command":["go","test","-json","-count=1","./shared/chain/..."],"timeout":120,"proof":"go-test-json","inputs":["shared/chain/client.go"]}]
```

Use the correct module working directory or a reviewed coordinator wrapper for Go workspaces; the example is a shape, not a registered production check. Bytecode caches, dependency directories, build outputs and secrets follow the shared source policy and never enter signed source snapshots. Actual included source edits and file permission changes invalidate evidence.

`accept` validates the live fence, actual test receipts, current candidate, exact trusted ownership and fresh patch. Files are flushed before an atomic SQLite acceptance index and lease retirement. Historical acceptance remains usable after the worker exits and its workspace disappears. Signed filesystem orphans without a committed index are rejected. An interrupted acceptance may leave an unindexed artifact requiring coordinator inspection; it cannot qualify. Accepted artifacts remain under the private state directory; do not move them or edit the index.

Acceptance frees resources, allowing the remaining lanes to run in another batch. The latest accepted fence for each task is authoritative; superseded artifacts cannot substitute for the selected attempt. Acceptance alone does not qualify the canonical task or integrate code. The root qualifier needs its separate explicit reviewed per-task policy and accepted-candidate adapter. Only the coordinator runs `cg spec done` after those checks pass.

## Serial integration

Collect all 50 accepted directories from the same baseline/interface/layout and selected wave. Combined checks also require explicit positive native proof policies. Full-wave staging rejects missing/duplicate/wrong-wave artifacts before publication.

```bash
python3 tools/production/dispatcher/cli.py --state-dir /tmp/kindle-wave1-state stage \
  --wave 1 --accepted /tmp/kindle-wave1-state/accepted/ATTEMPT_01 \
  /tmp/kindle-wave1-state/accepted/ATTEMPT_02 \
  --output /tmp/kindle-wave1-staged --checks /tmp/reviewed-wave1-combined-checks.json
```

Supply every accepted directory; the abbreviated example intentionally does not pass a real 50-lane gate. Staging reconstructs each authenticated candidate, checks the committed index, exact trusted registry/ownership and copied outputs, then applies sorted disjoint patches to a fresh tree. Combined commands run on that tree. Failure removes the stage and leaves the baseline/canonical repository unchanged. A successful complete-wave stage still reports `canonical_integrated:false`: canonical publication and serial-gate `cg spec done` belong to the root coordinator. Partial staging is available only for explicitly initialized `--fixture` ledgers using `--fixture-partial`; it reports `qualified:false` and `complete_wave:false`.

## Actual local qualification

```bash
PYTHONDONTWRITEBYTECODE=1 python3 tools/production/dispatcher/qualification.py \
  --spec spec/production-system/spec.kvx \
  --plan spec/production-system/execution-plan.json \
  --state-dir /tmp/kindle-dispatch-proof-unique/state \
  --evidence-dir /tmp/kindle-dispatch-proof-unique/dispatcher --case all
```

Use a new state/evidence directory for each run. Eleven mandatory cases include real 50-child overlap and socket binds, actual heartbeats and control-operation timings, live-expired-child takeover, process races, OS/coordinator restart, timeout/SIGTERM descendants, path attacks, resource denial, authenticated proof rejection, all native module tests, and a real coordinator CLI acceptance/staging lifecycle. Isolated fixture statuses make wave 1 eligible solely for this test. No canonical task state or production transaction is changed.

`result.json` distinguishes standalone dispatcher-source binding from the root runner's full candidate binding. Root qualification also verifies source before/after, registry and evidence files. This safety proof is not validation of product features, real deployment, 500,000 DAU, or isolation against hostile same-UID code. Intentionally daemonized descendants escaping their process group need stronger OS containment; ordinary inherited descendants are tested and cleaned.
