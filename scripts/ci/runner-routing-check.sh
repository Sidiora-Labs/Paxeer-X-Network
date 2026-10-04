#!/bin/sh
set -eu

root=$(CDPATH='' cd -- "$(dirname -- "$0")/../.." && pwd)
cd "$root"

exec python3 - "$@" <<'PY'
import glob
import os
import re
import sys

import yaml

# Jobs that keep their runs-on line byte for byte: workflow file, job, reason class, original runs-on.
EXCLUDED = """
codeql.yml                      analyze                          codeql            ubuntu-24.04
docs-site.yml                   deploy                           publishing-credential ubuntu-24.04
scorecard.yml                   analysis                         scorecard         ubuntu-24.04
explorer-build.yml              explorer-backend                 container         ubuntu-24.04
explorer-images.yml             publish                          docker-action     ubuntu-24.04
human.yml                       browser-performance              docker-command    ubuntu-24.04
mirror-live.yml                 publish-retrieve                 layerx-testnet    [self-hosted, layerx-testnet]
mirror-verification-live.yml    mirror-only                      layerx-testnet    [self-hosted, layerx-testnet]
paxeer-cross-arch-build.yml     macos-arm64                      macos             macos-latest
paxeer-db-tests.yml             test                             docker-action     ubuntu-latest
paxeer-db-tests.yml             coverage                         docker-action     ubuntu-latest
paxeer-docker-build.yml         pax-images                       docker-action     ubuntu-latest
paxeer-docker-publish.yml       pax-images                       docker-action     ubuntu-latest
paxeer-ecr.yml                  publisher                        ecr               ubuntu-latest
paxeer-go-test.yml              test                             docker-action     ubuntu-latest
paxeer-go-test.yml              coverage                         docker-action     ubuntu-latest
paxeer-hpx-registry.yml         release                          macos             macos-latest
paxeer-integration-test.yml     prepare-cluster                  docker-action     ubuntu-latest
paxeer-integration-test.yml     integration-tests                docker-action     ubuntu-latest
paxeer-libwasmvm.yml            build                            docker-command    ubuntu-latest
paxeer-nightly-ecr.yml          publish                          ecr               ubuntu-latest
paxeer-ghcr-integration-test-cleanup.yml cleanup                   publishing-credential ubuntu-latest
paxeer-proto-registry.yml       push                             publishing-credential ubuntu-latest
paxeer-release-publish.yml      releaser                         publishing-credential ubuntu-latest
platform.yml                    ios-application-artifact         macos             macos-15
platform.yml                    real-ios-journey                 macos             macos-15
platform.yml                    replay-matrix                    matrix-runner     ${{ matrix.runner }}
platform.yml                    release-pipeline                 release-pipeline  ubuntu-24.04
platform.yml                    publish-crates-io                publish           ubuntu-24.04
platform.yml                    publish-npm                      publish           ubuntu-24.04
platform.yml                    publish-pypi                     publish           ubuntu-24.04
platform.yml                    publish-go-modules               publish           ubuntu-24.04
platform.yml                    publish-maven-central            publish           ubuntu-24.04
platform.yml                    publish-swiftpm                  publish           ubuntu-24.04
platform.yml                    publish-nuget                    publish           ubuntu-24.04
programs-conformance.yml        deterministic-execution          matrix-runner     ${{ matrix.os }}
publish-images.yml              publish                          docker-command    ubuntu-24.04
publish-images.yml              attest                           docker-command    ubuntu-24.04
publish-images.yml              promote                          docker-command    ubuntu-24.04
"""

# Jobs that must be routed whatever else changes.
REQUIRED_ROUTED = """
runner-canary.yml               canary
"""

REASONS = {
    "codeql", "scorecard", "container", "services", "docker-action", "docker-command",
    "ecr", "ko", "cosign", "provenance", "macos", "windows", "matrix-runner",
    "layerx-testnet", "publish", "release-pipeline", "publishing-credential",
}

ROUTED = re.compile(
    r"^\$\{\{ \(github\.event_name == 'pull_request' && "
    r"github\.event\.pull_request\.head\.repo\.full_name != github\.repository\) && "
    r"'(ubuntu-24\.04|ubuntu-latest)' \|\| vars\.CI_LINUX_RUNNER \|\| "
    r"'(ubuntu-24\.04|ubuntu-latest)' \}\}$"
)

failures = []


def fail(path, job, message):
    failures.append(f"{path}: job {job}: {message}")


excluded = {}
for line in EXCLUDED.strip().splitlines():
    name, job, reason, original = line.split(None, 3)
    if reason not in REASONS:
        fail(".github/workflows/" + name, job, f"unknown exclusion reason class {reason}")
    excluded[(name, job)] = (reason, original.strip())

required_routed = set()
for line in REQUIRED_ROUTED.strip().splitlines():
    name, job = line.split()
    required_routed.add((name, job))


def raw_runs_on(path):
    jobs = {}
    in_jobs = False
    job = None
    with open(path, encoding="utf-8") as handle:
        for text in handle:
            text = text.rstrip("\n")
            if re.match(r"^\S", text):
                in_jobs = text.rstrip() == "jobs:"
                job = None
                continue
            if not in_jobs:
                continue
            match = re.match(r"^  ([A-Za-z0-9_-]+):\s*$", text)
            if match:
                job = match.group(1)
                continue
            match = re.match(r"^    runs-on:\s*(.*?)\s*$", text)
            if match and job is not None:
                jobs[job] = match.group(1)
    return jobs


seen = set()
for path in sorted(glob.glob(".github/workflows/*.yml") + glob.glob(".github/workflows/*.yaml")):
    name = os.path.basename(path)
    with open(path, encoding="utf-8") as handle:
        document = yaml.safe_load(handle)
    parsed_jobs = (document or {}).get("jobs") or {}
    raw = raw_runs_on(path)
    for job, body in parsed_jobs.items():
        seen.add((name, job))
        body = body or {}
        if "runs-on" not in body:
            if (name, job) in excluded or (name, job) in required_routed:
                fail(path, job, "listed job has no runs-on")
            continue
        value = raw.get(job)
        if value is None:
            fail(path, job, "runs-on is not written on the job's own line")
            continue
        if (name, job) in excluded:
            reason, original = excluded[(name, job)]
            if value != original:
                fail(path, job, f"excluded ({reason}) runs-on changed from {original!r} to {value!r}")
            continue
        match = ROUTED.match(value)
        if match:
            if match.group(1) != match.group(2):
                fail(path, job, "routing expression fallbacks differ")
            continue
        if "CI_LINUX_RUNNER" in value:
            fail(path, job, f"routing expression differs from the required form: {value}")
        else:
            fail(path, job, f"routed job has a fixed runner: {value}")

for name, job in sorted(excluded):
    if (name, job) not in seen:
        fail(".github/workflows/" + name, job, "excluded job does not exist")
for name, job in sorted(required_routed):
    if (name, job) not in seen:
        fail(".github/workflows/" + name, job, "required routed job does not exist")

if failures:
    for failure in failures:
        print(failure, file=sys.stderr)
    sys.exit(1)
PY
