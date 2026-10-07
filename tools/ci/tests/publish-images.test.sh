#!/usr/bin/env bash
# Checks that the shared beta image table covers every image Dockerfile in the repository and that
# the publish-images workflow builds the table as a matrix with GHCR write permission.
set -euo pipefail

REPO_ROOT=$(cd "$(dirname -- "${BASH_SOURCE[0]}")/../../.." && pwd)
cd "$REPO_ROOT"
errors=0
fail() { printf 'publish-images.test: %s\n' "$*" >&2; errors=$((errors + 1)); }

source platform/hosted/tests/beta-images.sh

declare -A table_paths=()
for name in "${IMAGE_NAMES[@]}"; do
    read -r canonical dockerfile <<<"$(image_source "$name")"
    [ "$canonical" = "ghcr.io/sidiora-labs/$name:${canonical##*:}" ] || fail "$name: canonical image $canonical is not ghcr.io/sidiora-labs/$name"
    case "$dockerfile" in
        /*|*..*|'') fail "$name: Dockerfile path $dockerfile is not under the repository" ;;
    esac
    resolved=$(realpath -m -- "$REPO_ROOT/$dockerfile")
    [[ $resolved == "$REPO_ROOT"/* ]] || fail "$name: Dockerfile path $dockerfile escapes the repository"
    table_paths[$dockerfile]=1
done

for dockerfile in docker/*/Dockerfile platform/hosted/feeder/Dockerfile; do
    [ -f "$dockerfile" ] || continue
    [ -n "${table_paths[$dockerfile]+present}" ] || fail "no beta-images.sh row for $dockerfile"
done
while IFS= read -r -d '' dockerfile; do
    dockerfile=${dockerfile#./}
    [ -n "${table_paths[$dockerfile]+present}" ] || fail "no beta-images.sh row for $dockerfile"
done < <(find human/wallet interop -name Dockerfile -not -path '*/node_modules/*' -print0 2>/dev/null)
for dockerfile in docker/intent-ingester/Dockerfile docker/redis/Dockerfile; do
    [ -n "${table_paths[$dockerfile]+present}" ] || fail "no beta-images.sh row for $dockerfile"
done

workflow=.github/workflows/publish-images.yml
if python3 -c 'import yaml' 2>/dev/null; then
    python3 - "$workflow" <<'PY' || fail "workflow structure check failed"
import sys
import yaml

doc = yaml.safe_load(open(sys.argv[1]))
on = doc.get("on", doc.get(True))
push = on["push"]
assert "main" in push["branches"], "push to main is not a trigger"
assert push["tags"], "tag trigger dropped"
for path in ("docker/**", "platform/**", "interop/**", "human/wallet/**", "explorer/**"):
    assert path in push["paths"], f"missing push path {path}"
assert "workflow_dispatch" in on, "dispatch trigger dropped"
jobs = doc["jobs"]
for name in ("images", "images-dependent"):
    job = jobs[name]
    assert job["permissions"]["packages"] == "write", f"{name} lacks packages: write"
    assert "matrix.image" not in str(job["runs-on"])
    assert "fromJSON(needs.plan.outputs" in str(job["strategy"]["matrix"]["image"]), f"{name} matrix is not the table plan"
    steps = [s for s in job["steps"] if str(s.get("uses", "")).startswith("docker/build-push-action@")]
    assert len(steps) == 1, f"{name} must build with docker/build-push-action"
    w = steps[0]["with"]
    assert w["push"] is True and "ghcr.io/sidiora-labs/" in w["tags"]
    assert w["cache-from"].startswith("type=gha") and w["cache-to"].startswith("type=gha")
    assert w["target"] == "${{ matrix.image.target }}"
assert "beta-images.sh" in jobs["plan"]["steps"][-1]["run"]
text = open(sys.argv[1]).read()
assert "CI_LINUX_RUNNER" not in text and "fly-linux" not in text, "Fly runner clause remains"
for job in jobs.values():
    assert "ubuntu" in str(job["runs-on"]), "every job must run on a GitHub-hosted ubuntu label by default"
PY
else
    grep -q 'matrix:' "$workflow" || fail "workflow has no matrix"
    grep -q 'packages: write' "$workflow" || fail "workflow lacks packages: write"
    grep -q 'docker/build-push-action@' "$workflow" || fail "workflow does not use docker/build-push-action"
    grep -q 'beta-images.sh' "$workflow" || fail "workflow does not read the image table"
    ! grep -q 'CI_LINUX_RUNNER' "$workflow" || fail "Fly runner clause remains"
fi

bash -n platform/hosted/tests/beta-images.sh platform/hosted/tests/beta-cluster.sh || fail "shell syntax"

[ "$errors" -eq 0 ] || { printf 'publish-images.test: %d failure(s)\n' "$errors" >&2; exit 1; }
printf 'publish-images.test: ok (%d images)\n' "${#IMAGE_NAMES[@]}"
