import gzip
import hashlib
import io
import pathlib
import shutil
import tarfile
import tempfile
import time
import uuid
from build import validate, validate_context
from policy import canonical, digest, load, require, pinned_image
from provenance import Tool, source_manifest, write_document


def read_oci(archive, output):
    output = pathlib.Path(output)
    require(not output.exists(), 'OCI extraction destination exists')
    output.mkdir(parents=True)
    with tarfile.open(archive, 'r:*') as source:
        seen = set()
        total = 0
        for member in source:
            name = pathlib.PurePosixPath(member.name)
            require(not name.is_absolute() and '..' not in name.parts and not member.issym() and not member.islnk(), 'unsafe OCI archive entry')
            if member.isdir():
                continue
            require(member.isfile() and str(name) not in seen, 'invalid OCI archive member')
            seen.add(str(name))
            total += member.size
            require(0 <= member.size <= 1024 * 1024 * 1024 and total <= 4 * 1024 * 1024 * 1024, 'OCI archive exceeds bound')
            destination = output / name
            destination.parent.mkdir(parents=True, exist_ok=True)
            with source.extractfile(member) as incoming, destination.open('xb') as target:
                shutil.copyfileobj(incoming, target)
    return validate_layout(output)


def validate_layout(output, expected_digest=None):
    output = pathlib.Path(output)
    require(output.is_dir() and not output.is_symlink(), 'missing direct OCI layout')
    def document(path):
        from provenance import read_document
        return read_document(path)[0]
    require(document(output / 'oci-layout') == {'imageLayoutVersion': '1.0.0'}, 'unsupported OCI layout version')
    index = document(output / 'index.json')
    require(type(index.get('manifests')) is list and len(index['manifests']) == 1, 'OCI archive must contain exactly one platform')
    descriptor = index['manifests'][0]
    require(type(descriptor) is dict, 'invalid OCI descriptor')
    if expected_digest is not None:
        require(descriptor.get('digest') == expected_digest, 'OCI candidate changed after approval')
    def blob(descriptor):
        from policy import sha
        identity = descriptor.get('digest')
        require(type(identity) is str and identity.startswith('sha256:') and sha(identity[7:]), 'invalid OCI digest')
        require(type(descriptor.get('size')) is int and 0 < descriptor['size'] <= 1024 * 1024 * 1024, 'invalid OCI blob size')
        path = output / 'blobs' / 'sha256' / identity[7:]
        require(path.is_file() and not path.is_symlink() and path.resolve().is_relative_to(output.resolve()), 'missing or indirect OCI blob')
        with path.open('rb') as stream:
            require(path.stat().st_size == descriptor['size'] and hashlib.file_digest(stream, 'sha256').hexdigest() == identity[7:], 'OCI blob checksum mismatch')
        return path
    manifest = document(blob(descriptor))
    require(manifest.get('schemaVersion') == 2 and type(manifest.get('layers')) is list and manifest['layers'], 'invalid OCI manifest')
    config = document(blob(manifest['config']))
    require(config.get('config', {}).get('User') == '65532:65532', 'nonroot image user changed')
    require(config.get('os') == 'linux' and config.get('architecture') in ('amd64', 'arm64'), 'unsupported OCI platform')
    binary_digest = None
    layer_digests = []
    for layer in manifest['layers']:
        path = blob(layer)
        layer_digests.append(layer['digest'])
        require(layer.get('mediaType') in ('application/vnd.oci.image.layer.v1.tar', 'application/vnd.oci.image.layer.v1.tar+gzip'), 'unsupported OCI layer compression')
        layer_seen = set()
        expanded = 0
        with tarfile.open(path, 'r:*') as content:
            for member in content:
                path_name = pathlib.PurePosixPath(member.name)
                require(not path_name.is_absolute() and '..' not in path_name.parts, 'unsafe OCI layer path')
                name = str(path_name)
                require(name not in layer_seen, 'duplicate OCI layer path')
                layer_seen.add(name)
                expanded += member.size
                require(0 <= member.size <= 1024 * 1024 * 1024 and expanded <= 4 * 1024 * 1024 * 1024, 'OCI layer exceeds bound')
                if name in ('app/.wh.app', '.wh.app', '.wh..wh..opq', 'app/.wh..wh..opq', '.wh.app'):
                    binary_digest = None
                if name == 'app':
                    require(member.isdir(), 'application directory replaced by indirect entry')
                if name == 'app/app':
                    require(member.isfile() and 0 < member.size <= 256 * 1024 * 1024, 'invalid application binary')
                    with content.extractfile(member) as executable:
                        binary_digest = hashlib.file_digest(executable, 'sha256').hexdigest()
    require(binary_digest is not None, 'OCI image contains no application binary')
    return {'manifest_digest': descriptor['digest'], 'binary_sha256': binary_digest, 'architecture': config['architecture'], 'os': config['os'], 'config_digest': manifest['config']['digest'], 'layer_digests': layer_digests}


def reproduce(config, source_root, output_root):
    root = pathlib.Path(source_root).resolve()
    service, binary = config['service'], config['binary']
    validate(service, binary, config['builder'], config['runtime'])
    pinned_image(config['buildkit_image'])
    validate_context((root / '.dockerignore').read_text())
    require(config['platform'] in ('linux/amd64', 'linux/arm64'), 'unsupported build platform')
    require(config['target'] in ('release', 'confinement'), 'unreviewed Docker target')
    inputs = source_manifest(root, config['source_paths'])
    require(inputs['sha256'] == config['source_sha256'], 'build source changed before execution')
    buildx = Tool.parse(config['buildx'])
    require(type(config['worker_timeout_seconds']) is int and 60 <= config['worker_timeout_seconds'] <= 600, 'invalid clean worker timeout')
    output = pathlib.Path(output_root).resolve()
    require(not output.exists(), 'reproducibility output already exists')
    output.mkdir(parents=True)
    context = output / 'source'
    context.mkdir()
    require({'.dockerignore', 'deploy/Dockerfile.svc', 'security/supplychain/confinement.py'} <= set(inputs['files']), 'incomplete reproducible source context')
    for relative in inputs['files']:
        destination = context / relative
        destination.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(root / relative, destination)
    require(source_manifest(context, config['source_paths']) == inputs, 'clean context differs from approved source')
    results = []
    deadline = time.monotonic() + 1260
    for number in (1, 2):
        worker = 'kindlelaunch-repro-' + uuid.uuid4().hex
        directory = output / ('worker-' + str(number))
        directory.mkdir()
        archive = directory / 'image.tar'
        try:
            require(time.monotonic() < deadline, 'clean build pair time budget exhausted')
            buildx.run(['create', '--name', worker, '--driver', 'docker-container', '--driver-opt', 'image=' + config['buildkit_image']], timeout=60)
            buildx.run(['build', '--builder', worker, '--no-cache', '--pull', '--provenance=false', '--sbom=false', '--platform', config['platform'], '--target', config['target'], '--build-arg', 'SERVICE=' + service, '--build-arg', 'BIN=' + binary, '--build-arg', 'BUILD_IMAGE=' + config['builder'], '--build-arg', 'RUNTIME_IMAGE=' + config['runtime'], '--build-arg', 'SOURCE_DATE_EPOCH=0', '--output', 'type=oci,compression=gzip,force-compression=true,dest=' + str(archive), '--metadata-file', str(directory / 'build-metadata.json'), '-f', str(context / 'deploy/Dockerfile.svc'), str(context)], timeout=min(config['worker_timeout_seconds'], max(1, int(deadline - time.monotonic()))))
            require(source_manifest(root, config['source_paths']) == inputs, 'build inputs changed during execution')
            result = read_oci(archive, directory / 'layout')
            require(result['os'] + '/' + result['architecture'] == config['platform'], 'build platform mismatch')
            result.update(source=config['source_sha256'], service=service, binary=binary, worker=worker, inputs=inputs['files'], builder=config['builder'], runtime=config['runtime'], buildkit_image=config['buildkit_image'], buildx_sha256=buildx.sha256, layout=str(directory / 'layout'))
            write_document(directory / 'receipt.json', result)
            results.append(result)
        finally:
            buildx.run(['rm', '--force', worker], timeout=60)
    require(results[0]['binary_sha256'] == results[1]['binary_sha256'], 'reproducible binary digest mismatch')
    require(results[0]['inputs'] == results[1]['inputs'], 'reproducible provenance input mismatch')
    result = {'reproducible': True, 'binary_sha256': results[0]['binary_sha256'], 'source': config['source_sha256'], 'builds': results}
    write_document(output / 'reproducibility.json', result)
    return result
