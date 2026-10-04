#!/bin/sh
set -eu
repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
exec mvn -o -q -f "$repo_root/platform/sdk/jvm/pom.xml" -Pconformance surefire:test \
    -Dtest=ProgramsContractTest -DfailIfNoTests=true -Dlayerx.repo.root="$repo_root"
