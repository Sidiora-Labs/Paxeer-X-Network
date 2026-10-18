import copy
import importlib
import pathlib
from policy import require, digest, pinned_image
from provenance import Tool, read_document, write_document


def validate_spdx(document, schema):
    validator = importlib.import_module('jsonschema')
    validator.Draft7Validator(schema).validate(document)
    require(document.get('spdxVersion') == 'SPDX-2.3' and document.get('dataLicense') == 'CC0-1.0' and document.get('SPDXID') == 'SPDXRef-DOCUMENT', 'invalid SPDX identity')
    require(type(document.get('packages')) is list and document['packages'], 'empty SPDX inventory')
    ids = set()
    for package in document['packages']:
        require(type(package.get('SPDXID')) is str and package['SPDXID'] not in ids, 'duplicate SPDX package identity')
        ids.add(package['SPDXID'])
        require(all(type(package.get(key)) is str and package[key] for key in ('name', 'versionInfo', 'licenseConcluded')), 'missing SPDX package identity or license')
    return document


def package_kind(package):
    refs = package.get('externalRefs', [])
    require(type(refs) is list, 'invalid SPDX package references')
    purls = [reference.get('referenceLocator', '') for reference in refs if reference.get('referenceType') == 'purl']
    if any(value.startswith('pkg:golang/') for value in purls):
        return 'go'
    if any(value.startswith('pkg:npm/') for value in purls):
        return 'javascript'
    if any(value.startswith(('pkg:deb/', 'pkg:rpm/', 'pkg:apk/', 'pkg:alpm/')) for value in purls):
        return 'os'
    return None


def inventory_projection(documents):
    packages = []
    kinds = set()
    for document in documents:
        for package in document['packages']:
            kind = package_kind(package)
            if kind:
                kinds.add(kind)
                packages.append({'name': package['name'], 'versionInfo': package['versionInfo'], 'licenseConcluded': package['licenseConcluded'], 'kind': kind})
    require(kinds == {'go', 'javascript', 'os'}, 'SBOM does not inventory Go, JavaScript and OS dependencies')
    return {'spdxVersion': 'SPDX-2.3', 'packages': packages}


def generate(config, image, javascript_root, output_root):
    if config.get('oci_archive'):
        from provenance import artifact_snapshot
        with artifact_snapshot(config['oci_archive']) as archive:
            return _generate({**config, 'oci_archive': str(archive)}, image, javascript_root, output_root)
    return _generate(config, image, javascript_root, output_root)


def _generate(config, image, javascript_root, output_root):
    pinned_image(image)
    tool = Tool.parse(config['tool'])
    schema, _ = read_document(config['schema_path'], config['schema_sha256'])
    if config.get('oci_archive'):
        import tempfile
        from reproduce import read_oci
        with tempfile.TemporaryDirectory(prefix='sbom-approved-oci-') as temporary:
            actual = read_oci(config['oci_archive'], pathlib.Path(temporary) / 'layout')
            require(actual['manifest_digest'] == image.rsplit('@', 1)[1], 'SBOM archive differs from approved image')
    output = pathlib.Path(output_root)
    output.mkdir(parents=True, exist_ok=True)
    paths = []
    documents = []
    image_source = 'oci-archive:' + str(pathlib.Path(config['oci_archive']).resolve()) if config.get('oci_archive') else image
    for scope, target in (('image', image_source), ('javascript', 'dir:' + str(pathlib.Path(javascript_root).resolve()))):
        destination = output / ('sbom-' + scope + '.spdx.json')
        require(not destination.exists(), 'SBOM output already exists')
        tool.run(['scan', target, '--output', 'spdx-json=' + str(destination)], timeout=600)
        document, raw = read_document(destination)
        validate_spdx(document, schema)
        paths.append({'scope': scope, 'path': str(destination), 'sha256': digest(raw)})
        documents.append(document)
    projection = inventory_projection(documents)
    write_document(output / 'sbom-inventory.json', projection)
    return {'artifacts': paths, 'projection_path': str(output / 'sbom-inventory.json'), 'projection_sha256': digest((output / 'sbom-inventory.json').read_bytes()), 'tool_sha256': tool.sha256}
