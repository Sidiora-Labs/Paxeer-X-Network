import contextlib
import dataclasses
import stat
import hashlib
import os
import pathlib
import re
import subprocess
import tempfile
import time
from ci_tools import execution_snapshot
from policy import canonical, digest, load, require, sha


@dataclasses.dataclass(frozen=True)
class Tool:
    path: str
    sha256: str

    @classmethod
    def parse(cls, value):
        require(type(value) is dict and set(value) == {'path', 'sha256'}, 'invalid tool pin')
        require(type(value['path']) is str and sha(value['sha256']), 'invalid tool identity')
        return cls(value['path'], value['sha256'])

    def run(self, arguments, *, cwd=None, timeout=300, env=None, input_bytes=None):
        require(type(arguments) in (list, tuple) and all(type(x) is str and '\x00' not in x for x in arguments), 'invalid tool arguments')
        with tempfile.TemporaryFile() as stdout, tempfile.TemporaryFile() as stderr:
            require(input_bytes is None or type(input_bytes) is bytes and len(input_bytes) <= 1024 * 1024, 'invalid tool standard input')
            with execution_snapshot(self.path, self.sha256) as (executable, descriptor):
                result = subprocess.run([executable, *arguments], pass_fds=(descriptor,), cwd=cwd, env=env, input=input_bytes, stdout=stdout, stderr=stderr, timeout=timeout, check=False)
            require(stdout.tell() <= 32 * 1024 * 1024 and stderr.tell() <= 4 * 1024 * 1024, 'tool output too large')
            stdout.seek(0)
            stderr.seek(0)
            output, error = stdout.read(), stderr.read()
            require(result.returncode == 0, 'tool exited ' + str(result.returncode) + ': ' + error.decode('utf-8', 'replace')[:2048])
            return output


def read_document(path, expected_digest=None):
    path = pathlib.Path(path)
    require(path.is_file() and not path.is_symlink(), 'missing document: ' + str(path))
    with path.open('rb') as source:
        data = source.read(4 * 1024 * 1024 + 1)
    require(len(data) <= 4 * 1024 * 1024, 'document too large')
    if expected_digest is not None:
        require(sha(expected_digest) and digest(data) == expected_digest, 'document digest mismatch')
    return load(data), data


def write_document(path, value):
    path = pathlib.Path(path)
    path.parent.mkdir(parents=True, exist_ok=True)
    descriptor, temporary = tempfile.mkstemp(prefix='.' + path.name, dir=path.parent)
    try:
        with os.fdopen(descriptor, 'wb') as target:
            target.write(canonical(value) + b'\n')
            target.flush()
            os.fsync(target.fileno())
        os.replace(temporary, path)
        directory = os.open(path.parent, os.O_RDONLY | os.O_DIRECTORY)
        try:
            os.fsync(directory)
        finally:
            os.close(directory)
    finally:
        if os.path.exists(temporary):
            os.unlink(temporary)


@contextlib.contextmanager
def artifact_snapshot(path):
    path = pathlib.Path(path)
    require(path.is_file() and not path.is_symlink(), 'missing direct artifact')
    with tempfile.TemporaryDirectory(prefix='immutable-oci-artifact-') as temporary:
        target = pathlib.Path(temporary) / 'image.tar'
        with path.open('rb') as source, target.open('xb') as output:
            metadata = os.fstat(source.fileno())
            require(stat.S_ISREG(metadata.st_mode) and 0 < metadata.st_size <= 4 * 1024 * 1024 * 1024, 'invalid artifact size')
            remaining = metadata.st_size
            while remaining:
                chunk = source.read(min(remaining, 1024 * 1024))
                require(chunk, 'artifact truncated during snapshot')
                output.write(chunk)
                remaining -= len(chunk)
            require(not source.read(1), 'artifact grew during snapshot')
            output.flush()
            os.fsync(output.fileno())
        target.chmod(0o400)
        yield target


def source_manifest(root, paths):
    root = pathlib.Path(root).resolve()
    require(type(paths) is list and paths and len(paths) == len(set(paths)), 'invalid source input list')
    files = {}
    for name in sorted(paths):
        require(type(name) is str and not pathlib.PurePosixPath(name).is_absolute() and '..' not in pathlib.PurePosixPath(name).parts, 'unsafe source input')
        path = root / name
        require(path.is_file() and not path.is_symlink() and path.resolve().is_relative_to(root), 'missing or indirect source input: ' + name)
        with path.open('rb') as source:
            files[name] = hashlib.file_digest(source, 'sha256').hexdigest()
    return {'files': files, 'sha256': digest(canonical(files))}


def make_statement(expectation, build, materials, now):
    require(type(now) is int and now >= 0, 'invalid statement clock')
    require(build['source'] == expectation['source'] and build['image'] == expectation['image'], 'build subject mismatch')
    require(sha(expectation['source']) and sha(expectation['workflow']) and sha(build['binary_sha256']), 'invalid provenance digest')
    require(type(materials) is dict and materials and all(type(k) is str and sha(v) for k, v in materials.items()), 'missing provenance materials')
    image_digest = expectation['image'].rsplit('@sha256:', 1)
    require(len(image_digest) == 2 and sha(image_digest[1]), 'invalid immutable subject')
    return {
        '_type': 'https://in-toto.io/Statement/v1',
        'subject': [{'name': image_digest[0], 'digest': {'sha256': image_digest[1]}}],
        'predicateType': 'https://slsa.dev/provenance/v1',
        'predicate': {
            'buildDefinition': {
                'buildType': 'https://sidiora.dev/build/container/v2',
                'externalParameters': {'source': expectation['source'], 'workflow': expectation['workflow'], 'repository': expectation['repository'], 'ref': expectation['ref'], 'service': build['service'], 'binary': build['binary'], 'architecture': build['architecture']},
                'internalParameters': {'binary_sha256': build['binary_sha256'], 'sbom': expectation['sbom']},
                'resolvedDependencies': [{'uri': name, 'digest': {'sha256': value}} for name, value in sorted(materials.items())],
            },
            'runDetails': {'builder': {'id': expectation['builder']}, 'metadata': {'invocationId': expectation['run_id'], 'startedOn': time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime(now))}},
        },
    }


def validate_statement(statement, expected):
    require(type(statement) is dict and statement.get('_type') == 'https://in-toto.io/Statement/v1' and statement.get('predicateType') == 'https://slsa.dev/provenance/v1', 'invalid provenance statement')
    name, image_digest = expected['image'].rsplit('@sha256:', 1)
    require(statement.get('subject') == [{'name': name, 'digest': {'sha256': image_digest}}], 'provenance image mismatch')
    predicate = statement['predicate']
    build = predicate['buildDefinition']
    require(build.get('buildType') == 'https://sidiora.dev/build/container/v2', 'untrusted build type')
    external = build['externalParameters']
    for field in ('source', 'workflow', 'repository', 'ref', 'service', 'binary', 'architecture'):
        require(external.get(field) == expected[field], 'provenance input mismatch: ' + field)
    require(build['internalParameters'].get('sbom') == expected['sbom'], 'provenance SBOM mismatch')
    run = predicate['runDetails']
    require(run['builder'].get('id') == expected['builder'] and run['metadata'].get('invocationId') == expected['run_id'], 'provenance builder/run mismatch')
    dependencies = build.get('resolvedDependencies')
    require(type(dependencies) is list and dependencies, 'missing provenance dependencies')
    actual = {}
    for dependency in dependencies:
        require(type(dependency) is dict and set(dependency) == {'uri', 'digest'} and set(dependency['digest']) == {'sha256'} and sha(dependency['digest']['sha256']), 'invalid provenance dependency')
        require(dependency['uri'] not in actual, 'duplicate provenance dependency')
        actual[dependency['uri']] = dependency['digest']['sha256']
    require(actual == expected['materials'], 'provenance material mismatch')
    return statement
