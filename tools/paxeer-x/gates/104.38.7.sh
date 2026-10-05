#!/usr/bin/env bash
set -euo pipefail
if (($#)); then
    echo "usage: $0 (no arguments)" >&2
    exit 2
fi
cd "$(dirname "${BASH_SOURCE[0]}")/../../.."
export PATH="$HOME/.cargo/bin:/usr/local/go/bin:$PATH"
exec python3 - <<'PY'
import os
import re
import shutil
import subprocess
import sys
import tempfile

TESTS = 0
SDKGEN = ['cargo', 'run', '--offline', '--locked', '--quiet', '--manifest-path', 'platform/Cargo.toml',
          '-p', 'layerx-platform-sdkgen', '--']
JVM = 'platform/sdk/jvm/src/main/java/com/sidiora/layerx/sdk/'
LOCKED_JVM = [JVM + 'verify/LocalVerifier.java',
              'platform/sdk/jvm/src/conformance/java/com/sidiora/layerx/sdk/ConformanceMain.java',
              'platform/sdk/conformance/run-jvm.sh']


def require(value, reason):
    if not value:
        raise RuntimeError(reason)


def execute(label, argv, cwd='.'):
    require(shutil.which(argv[0]), label + ' toolchain ' + argv[0] + ' is not installed')
    print('== ' + label + ': ' + ' '.join(argv), flush=True)
    result = subprocess.run(argv, cwd=cwd, stdin=subprocess.DEVNULL, capture_output=True,
                            text=True, timeout=1500)
    sys.stdout.write(result.stdout)
    sys.stderr.write(result.stderr)
    return result


def run(label, argv, cwd='.'):
    result = execute(label, argv, cwd)
    require(result.returncode == 0, label + ' failed with exit ' + str(result.returncode))
    return result.stdout + result.stderr


def refuse(label, argv, needle):
    result = execute(label, argv)
    require(result.returncode != 0, label + ' was accepted')
    require(needle in result.stderr, label + ' refused without naming ' + needle)
    credit(label, 1)


def credit(label, count):
    global TESTS
    require(count > 0, label + ' ran no tests')
    TESTS += count


def archived_tree(destination):
    lock = open('platform/sdk/pipeline.kvx', encoding='utf-8').read()
    roots = sorted(set(re.findall(r'^root = "([^"]+)"$', lock, re.M)))
    require(len(roots) >= 12, 'pipeline lock declares too few roots')
    paths = roots + ['platform/sdk/pipeline.kvx', 'programs/sdk/rust/src/abi_policy.rs',
                     'programs/crates/layerx-programs-runtime/src/terminal.rs']
    archive = subprocess.run(['git', 'archive', '--format=tar', 'HEAD', '--', *paths],
                             stdin=subprocess.DEVNULL, capture_output=True, timeout=300, check=True)
    subprocess.run(['tar', '-x', '-C', destination], input=archive.stdout, check=True, timeout=300)
    subprocess.run(['git', '-C', destination, 'init', '-q'], stdin=subprocess.DEVNULL, check=True,
                   timeout=60)


try:
    run('generation lock, classification and receipt contracts on the candidate tree',
        SDKGEN + ['--check-program-contracts', os.getcwd()])
    credit('candidate lock', 1)

    out = run('generation lock drift model',
              ['cargo', 'test', '--offline', '--locked', '--manifest-path', 'platform/Cargo.toml',
               '-p', 'layerx-platform-sdkgen', '--test', 'drift'])
    summary = re.findall(r'test result: ok\. (\d+) passed; 0 failed; 0 ignored', out)
    require(len(summary) == 1, 'generator drift summary missing')
    credit('generator drift', int(summary[0]))

    with tempfile.TemporaryDirectory(prefix='paxeer-x-104.38.7-') as copy:
        archived_tree(copy)
        check = SDKGEN + ['--check-program-contracts', copy]
        run('archived candidate passes the generation lock', check)
        credit('archived candidate', 1)

        unlisted = JVM + 'Unlisted.java'
        with open(os.path.join(copy, unlisted), 'w', encoding='utf-8') as stream:
            stream.write('final class Unlisted {}\n')
        refuse('undeclared file in an explicit output root', check,
               'untracked file in generated jvm root: ' + unlisted)

        lock = os.path.join(copy, 'platform/sdk/pipeline.kvx')
        with open(lock, encoding='utf-8') as stream:
            text = stream.read()
        require('\n[handwritten.platform-jvm]\n' in text, 'JVM handwritten declarations missing')
        text = text.replace('\n[handwritten.platform-jvm]\n',
                            '\n[handwritten.platform-jvm]\n"src/main/java/com/sidiora/layerx/sdk/Unlisted.java" = "handwritten"\n')
        with open(lock, 'w', encoding='utf-8') as stream:
            stream.write(text)
        run('declared handwritten file is accepted', check)
        credit('declared handwritten', 1)

        for relative in LOCKED_JVM:
            path = os.path.join(copy, relative)
            with open(path, 'rb') as stream:
                original = stream.read()
            with open(path, 'ab') as stream:
                stream.write(b'\n')
            refuse('hand-edited ' + relative, check, relative + ' is stale or hand-edited')
            with open(path, 'wb') as stream:
                stream.write(original)
        run('restored lock-covered JVM files agree byte for byte', check)
        credit('restored JVM files', 1)

        with open(os.path.join(copy, JVM + 'Unclassified.java'), 'w', encoding='utf-8') as stream:
            stream.write('final class Unclassified {}\n')
        refuse('regeneration with an unclassified file', SDKGEN + ['--write', copy],
               'unclassified file in generated jvm root: ' + JVM + 'Unclassified.java')

    run('retained platform SDK drift contract', ['make', '--no-print-directory', 'platform-sdk-check'])
    credit('make platform-sdk-check', 1)

    print('PAXEER_X_GATE tests=' + str(TESTS) + ' skipped=0')
except (OSError, RuntimeError, subprocess.SubprocessError) as error:
    print('SDK generation-lock qualification refused: ' + str(error), file=sys.stderr)
    sys.exit(78)
PY
