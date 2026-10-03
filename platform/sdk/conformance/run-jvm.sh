#!/bin/sh
set -eu

repo_root=${1:-.}
repo_root=$(cd "$repo_root" && pwd)

if [ "${2:-}" = "--prebuilt" ]; then
	: "${PAXEER_X_JVM_BUILD_PROVENANCE:?set protected JVM build record}"
	: "${PAXEER_X_SDKGEN_BIN:?set genuine prebuilt platform SDK generator}"
	: "${PAXEER_X_EVIDENCE_DIR:?set private qualification evidence directory}"
	python3 "$repo_root/platform/sdk/jvm/qualification.py" check "$PAXEER_X_JVM_BUILD_PROVENANCE"
	python3 "$repo_root/platform/sdk/generators/generate_jvm.py" "$repo_root" --check
	"$PAXEER_X_SDKGEN_BIN" --check "$repo_root"
	reports=$(mktemp -d "$PAXEER_X_EVIDENCE_DIR/jvm-reports.XXXXXXXX")
	mvn -o -q -f "$repo_root/platform/sdk/jvm/pom.xml" -Pconformance surefire:test exec:java \
		-DfailIfNoTests=true -Dlayerx.repo.root="$repo_root" \
		-Dlayerx.conformance.reportDirectory="$reports" \
		-Dexec.classpathScope=test -Dexec.mainClass=com.sidiora.layerx.sdk.ConformanceMain \
		-Dexec.args="$repo_root"
	python3 "$repo_root/platform/sdk/jvm/qualification.py" check "$PAXEER_X_JVM_BUILD_PROVENANCE"
	python3 "$repo_root/platform/sdk/jvm/qualification.py" reports "$reports"
	exit 0
fi
if [ "$#" -gt 1 ]; then
	echo 'usage: run-jvm.sh [repository] [--prebuilt]' >&2
	exit 2
fi

cargo run --offline --manifest-path "$repo_root/platform/Cargo.toml" --locked \
	-p layerx-platform-sdkgen -- --check "$repo_root"
mvn -o -q -f "$repo_root/platform/sdk/jvm/pom.xml" -Pconformance test \
	exec:java -Dexec.classpathScope=test \
	-Dexec.mainClass=com.sidiora.layerx.sdk.ConformanceMain \
	-Dexec.args="$repo_root"
