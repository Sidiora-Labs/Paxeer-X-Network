#!/usr/bin/env python3
import hashlib
import json
import os
from pathlib import Path
import stat
import sys
import xml.etree.ElementTree as ET

ROOT = Path(__file__).resolve().parents[3]
JVM = ROOT / 'platform/sdk/jvm'


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def snapshot():
    inputs = {}
    for directory in ('platform/sdk/jvm/src', 'agent/schema/agent-api', 'human/schema/human-api',
                      'platform/sdk/conformance/fixtures', 'tests/vectors/codec'):
        for path in sorted((ROOT / directory).rglob('*')):
            if path.name == '.env' or path.name.startswith('.env.'):
                continue
            if path.is_symlink():
                raise ValueError('symbolic input refused')
            if path.is_file():
                inputs[str(path.relative_to(ROOT))] = digest(path)
    for relative in ('platform/sdk/jvm/pom.xml', 'platform/sdk/jvm/qualification.py',
                     'platform/sdk/generators/generate_jvm.py', 'platform/sdk/conformance/run-jvm.sh'):
        inputs[relative] = digest(ROOT / relative)
    classes = {}
    for directory in ('classes', 'test-classes'):
        base = JVM / 'target' / directory
        files = sorted(base.rglob('*.class'))
        if not files:
            raise ValueError('prebuilt JVM classes unavailable')
        for path in files:
            if path.is_symlink():
                raise ValueError('symbolic artifact refused')
            classes[str(path.relative_to(ROOT))] = digest(path)
    return {'schema': 'paxeer-x.jvm-build-inputs.v1', 'inputs': inputs, 'classes': classes}


def private_path(raw, exists):
    path = Path(raw)
    if not path.is_absolute() or path.is_symlink():
        raise ValueError('absolute private record required')
    resolved = path.resolve()
    if ROOT == resolved or ROOT in resolved.parents:
        raise ValueError('build record must be outside repository')
    if exists:
        info = path.stat()
        if not stat.S_ISREG(info.st_mode) or info.st_uid != os.geteuid() or info.st_mode & 0o077 or info.st_nlink != 1:
            raise ValueError('build record is not private')
    else:
        info = path.parent.stat()
        if info.st_uid != os.geteuid() or info.st_mode & 0o077:
            raise ValueError('build record directory is not private')
    return path


def main():
    if len(sys.argv) != 3:
        raise ValueError('usage: qualification.py seal|check|reports PATH')
    operation, raw = sys.argv[1:]
    if operation == 'seal':
        path = private_path(raw, False)
        value = snapshot()
        fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
        with os.fdopen(fd, 'w') as output:
            json.dump(value, output, sort_keys=True, separators=(',', ':'))
            output.write('\n')
    elif operation == 'check':
        path = private_path(raw, True)
        if json.loads(path.read_text()) != snapshot():
            raise ValueError('JVM build inputs or compiled artifacts changed')
    elif operation == 'reports':
        files = sorted(Path(raw).glob('TEST-*.xml'))
        if not files:
            raise ValueError('JVM test corpus unavailable')
        tests = skipped = failed = 0
        for path in files:
            suite = ET.parse(path).getroot()
            if suite.tag != 'testsuite':
                raise ValueError('unexpected JVM report')
            cases = suite.findall('testcase')
            count = int(suite.attrib['tests'])
            if count != len(cases):
                raise ValueError('JVM case count mismatch')
            tests += count
            skipped += sum(case.find('skipped') is not None for case in cases)
            failed += sum(case.find('failure') is not None or case.find('error') is not None for case in cases)
            if int(suite.attrib.get('failures', '0')) or int(suite.attrib.get('errors', '0')):
                raise ValueError('JVM test failures')
        if tests <= 0 or skipped or failed:
            raise ValueError('empty, skipped or failed JVM acceptance corpus')
        print(f'PAXEER_X_GATE tests={tests + 1} skipped=0')
    else:
        raise ValueError('unknown qualification operation')


if __name__ == '__main__':
    try:
        main()
    except (OSError, ValueError, KeyError, TypeError, ET.ParseError):
        print('JVM qualification refused unavailable, stale or invalid evidence', file=sys.stderr)
        sys.exit(1)
