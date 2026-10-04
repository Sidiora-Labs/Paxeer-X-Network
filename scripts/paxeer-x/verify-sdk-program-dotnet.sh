#!/usr/bin/env bash
set -euo pipefail
ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)
cd "$ROOT"
results=${LAYERX_DOTNET_PROGRAMS_RESULTS_DIR:-$(mktemp -d /tmp/layerx-dotnet-programs-XXXXXX)}
[[ $results = /* && ! -L $results ]] || { echo 'private absolute Dotnet results directory required' >&2; exit 1; }
mkdir -p "$results"
chmod 700 "$results"
[[ ! -e $results/programs-contract.trx ]] || { echo 'refusing to reuse Dotnet test results' >&2; exit 1; }
"${DOTNET:-dotnet}" test platform/sdk/dotnet/tests/LayerX.Sdk.Tests/LayerX.Sdk.Tests.csproj \
    --no-build --no-restore --configuration Release --nologo \
    --filter 'FullyQualifiedName~LayerX.Sdk.Tests.ProgramsContractTests' \
    --logger 'trx;LogFileName=programs-contract.trx' --results-directory "$results"
python3 - "$results/programs-contract.trx" <<'PY'
import sys
import xml.etree.ElementTree as ET
path = sys.argv[1]
root = ET.parse(path).getroot()
namespace = {'t': 'http://microsoft.com/schemas/VisualStudio/TeamTest/2010'}
summary = root.find('t:ResultSummary', namespace)
counters = root.find('t:ResultSummary/t:Counters', namespace)
results = root.findall('t:Results/t:UnitTestResult', namespace)
if summary is None or counters is None or summary.get('outcome') != 'Completed' or not results:
    raise SystemExit('Dotnet Programs test accounting missing')
passed = int(counters.get('passed', '-1'))
if passed != len(results) or int(counters.get('total', '-1')) != passed \
        or int(counters.get('executed', '-1')) != passed or int(counters.get('failed', '-1')) != 0 \
        or int(counters.get('notExecuted', '-1')) != 0 or any(result.get('outcome') != 'Passed' for result in results):
    raise SystemExit('Dotnet Programs cases failed, skipped or absent')
print('PAXEER_X_GATE tests=%d skipped=0' % passed)
print('Dotnet Programs results=' + path)
PY
