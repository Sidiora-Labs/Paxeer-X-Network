from __future__ import annotations

import argparse
import json
import os
from pathlib import Path
import tempfile
from urllib.parse import urlsplit

from contracts import canonical, load, safe_diagnostics, sha, validate_binding
from contracts import require


def safe_directory(path):
    path = Path(path).absolute()
    for component in (path, *path.parents):
        require(not component.is_symlink(), 'configuration directory symlink denied')
    return path


def atomic_write(path, raw):
    path = safe_directory(Path(path).parent) / Path(path).name
    require(not path.is_symlink(), 'configuration file symlink denied')
    path.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    descriptor, temporary = tempfile.mkstemp(prefix='.observability-', dir=path.parent)
    try:
        os.fchmod(descriptor, 0o600)
        with os.fdopen(descriptor, 'wb') as output:
            output.write(raw)
            output.flush()
            os.fsync(output.fileno())
        os.replace(temporary, path)
        directory = os.open(path.parent, os.O_RDONLY | os.O_DIRECTORY)
        try:
            os.fsync(directory)
        finally:
            os.close(directory)
    finally:
        if os.path.exists(temporary):
            os.unlink(temporary)


def configuration_files(binding, config):
    root = Path(__file__).resolve().parents[1]
    rules = load(root/'prometheus-rules.json')
    scrape_configs = [{'job_name':s['service'], 'metrics_path':'/metrics', 'scheme':urlsplit(s['base_url']).scheme, 'static_configs':[{'targets':[urlsplit(s['base_url']).netloc]}]} for s in binding['services']]
    prometheus = {'global': {'scrape_interval':'15s', 'evaluation_interval':'15s', 'external_labels':{'deployment_digest':binding['deployment_digest']}}, 'rule_files':['prometheus-rules.json'], 'scrape_configs':scrape_configs}
    return {'prometheus.json':canonical(prometheus), 'prometheus-rules.json':canonical(rules), 'otel-collector.yaml':(root/'otel-collector.yaml').read_bytes(), 'diagnostics.json':canonical(safe_diagnostics(binding))}


def render(binding, config, output):
    validate_binding(binding, config)
    output = safe_directory(output)
    files = configuration_files(binding, config)
    for name, raw in files.items():
        atomic_write(output/name, raw)
    manifest = {'version':'2.0', 'configuration_sha256':sha(canonical(config)), 'binding_sha256':sha(canonical(binding)), 'files':{name:sha(raw) for name,raw in files.items()}, 'secret_references':config['exporter'], 'activation_allowed':False}
    atomic_write(output/'render-manifest.json', canonical(manifest))
    return manifest


def verify_bundle(binding, config, target):
    validate_binding(binding,config)
    target = safe_directory(target)
    manifest = load(target/'render-manifest.json')
    require(manifest['binding_sha256'] == sha(canonical(binding)) and manifest['configuration_sha256'] == sha(canonical(config)), 'activation bundle identity differs')
    expected = {'prometheus.json', 'prometheus-rules.json', 'otel-collector.yaml', 'diagnostics.json'}
    require(set(manifest['files']) == expected and {p.name for p in target.iterdir()} == expected | {'render-manifest.json'}, 'activation bundle inventory differs')
    for name, expected_hash in manifest['files'].items():
        file = target/name
        require(file.is_file() and not file.is_symlink() and sha(file.read_bytes()) == expected_hash, 'activation bundle changed')
    for name,raw in configuration_files(binding,config).items():
        require(manifest['files'][name] == sha(raw), 'activation configuration differs from bound source')
    require(load(target/'diagnostics.json') == safe_diagnostics(binding), 'activation diagnostics changed')
    require(manifest['secret_references'] == config['exporter'] and manifest['activation_allowed'] is False, 'activation bundle authority differs')
    return manifest


def immutable_bundle(binding, config, output):
    output = safe_directory(output)
    output.mkdir(parents=True, exist_ok=True, mode=0o700)
    target = output/('revision-'+str(config['revision'])+'-'+sha(canonical(binding)))
    if target.exists():
        return target, verify_bundle(binding, config, target)
    temporary = Path(tempfile.mkdtemp(prefix='.activation-', dir=output))
    try:
        render(binding, config, temporary)
        manifest = verify_bundle(binding, config, temporary)
        os.rename(temporary, target)
        directory = os.open(output, os.O_RDONLY | os.O_DIRECTORY)
        try:
            os.fsync(directory)
        finally:
            os.close(directory)
        return target, manifest
    finally:
        if temporary.exists():
            for file in temporary.iterdir():
                file.unlink()
            temporary.rmdir()


def main():
    p=argparse.ArgumentParser();p.add_argument('--binding', required=True);p.add_argument('--config', required=True);p.add_argument('--output', required=True);a=p.parse_args()
    print(json.dumps(render(load(a.binding),load(a.config),a.output)))


if __name__ == '__main__':
    main()
