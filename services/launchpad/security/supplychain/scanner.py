import datetime
from pathlib import Path
from policy import require, sha, digest, canonical, pinned_image
from provenance import Tool, read_document


def database_state(path, expected_digest, now, max_age):
    metadata, raw = read_document(path, expected_digest)
    require(type(max_age) is int and 0 < max_age <= 86400, 'invalid scanner freshness bound')
    stamp = metadata.get('UpdatedAt')
    require(type(stamp) is str, 'missing scanner database timestamp')
    date = datetime.datetime.fromisoformat(stamp.replace('Z', '+00:00'))
    require(date.tzinfo is not None, 'scanner database timestamp has no timezone')
    updated = int(date.timestamp())
    require(0 <= now - updated < max_age, 'scanner database stale or future')
    return {'sha256': digest(raw), 'updated': updated}


def findings_from_trivy(reports, analyses):
    require(type(analyses) is dict, 'missing vulnerability exposure authority')
    found = {}
    for scope, report in reports.items():
        require(type(report) is dict and report.get('SchemaVersion') == 2 and type(report.get('Results')) is list, 'incomplete Trivy report: ' + scope)
        for result in report['Results']:
            require(type(result) is dict and type(result.get('Target')) is str, 'invalid scanner target')
            vulnerabilities = result.get('Vulnerabilities', [])
            require(type(vulnerabilities) is list, 'invalid vulnerability results')
            for vulnerability in vulnerabilities:
                require(type(vulnerability) is dict and all(type(vulnerability.get(k)) is str and vulnerability[k] for k in ('VulnerabilityID', 'PkgName', 'InstalledVersion', 'Severity')), 'incomplete scanner finding')
                identity = vulnerability['VulnerabilityID'] + ':' + vulnerability['PkgName'] + '@' + vulnerability['InstalledVersion']
                severity = vulnerability['Severity'].lower()
                require(severity in ('critical', 'high', 'medium', 'low'), 'unknown vulnerability severity: ' + identity)
                if identity in found:
                    require(found[identity]['severity'] == severity, 'conflicting vulnerability severity: ' + identity)
                    continue
                authority = analyses.get(identity)
                require(type(authority) is dict and set(authority) in ({'reachable', 'analysis'}, {'reachable', 'analysis', 'exception'}), 'missing finding exposure analysis: ' + identity)
                require(type(authority['reachable']) is bool and type(authority['analysis']) is str and authority['analysis'].strip(), 'invalid finding exposure analysis: ' + identity)
                found[identity] = {'id': identity, 'severity': severity, **authority}
    require(set(analyses) == set(found), 'unused or stale vulnerability exposure analysis')
    return [found[key] for key in sorted(found)]


def response_state(config, findings, now):
    critical = {finding['id'] for finding in findings if finding['severity'] == 'critical'}
    if not critical:
        return {}
    require(config.get('response_authority_path') and config.get('response_authority_sha256'), 'missing critical response authority')
    records, _ = read_document(config['response_authority_path'], config['response_authority_sha256'])
    require(type(records) is dict and critical <= set(records), 'missing critical response record')
    responses = {}
    for identity in sorted(critical):
        record = records[identity]
        require(type(record) is dict and set(record) == {'discovered', 'triaged', 'mitigated', 'owner', 'ticket'}, 'invalid critical response record: ' + identity)
        require(type(record['discovered']) is int and 0 <= record['discovered'] <= now and type(record['owner']) is str and record['owner'] and type(record['ticket']) is str and record['ticket'], 'invalid critical response authority: ' + identity)
        for field, limit in (('triaged', 14400), ('mitigated', 86400)):
            value = record[field]
            require(value is None or type(value) is int and record['discovered'] <= value <= now and value - record['discovered'] <= limit, 'critical response deadline exceeded: ' + identity + ':' + field)
            require(now - record['discovered'] < limit or value is not None, 'critical response overdue: ' + identity + ':' + field)
        require(record['mitigated'] is None or record['triaged'] is not None and record['triaged'] <= record['mitigated'], 'critical response chronology invalid: ' + identity)
        responses[identity] = record
    return responses


def scan(config, image, source_root, output_root, now):
    if config.get('oci_archive'):
        from provenance import artifact_snapshot
        with artifact_snapshot(config['oci_archive']) as archive:
            return _scan({**config, 'oci_archive': str(archive)}, image, source_root, output_root, now)
    return _scan(config, image, source_root, output_root, now)


def _scan(config, image, source_root, output_root, now):
    pinned_image(image)
    require(type(now) is int and now >= 0, 'invalid scan clock')
    tool = Tool.parse(config['tool'])
    state = database_state(config['database_metadata'], config['database_metadata_sha256'], now, config['max_database_age_seconds'])
    cache = Path(config['cache_directory']).resolve()
    metadata = Path(config['database_metadata']).resolve()
    require(metadata == cache / 'db' / 'metadata.json', 'scanner cache/database mismatch')
    database = cache / 'db' / 'trivy.db'
    require(database.is_file() and not database.is_symlink() and sha(config['database_sha256']), 'missing scanner database bytes')
    with database.open('rb') as data:
        import hashlib
        require(hashlib.file_digest(data, 'sha256').hexdigest() == config['database_sha256'], 'scanner database checksum mismatch')
    if config.get('oci_archive'):
        import tempfile
        from reproduce import read_oci
        with tempfile.TemporaryDirectory(prefix='scan-approved-oci-') as temporary:
            actual = read_oci(config['oci_archive'], Path(temporary) / 'layout')
            require(actual['manifest_digest'] == image.rsplit('@', 1)[1], 'scanner archive differs from approved image')
    output = Path(output_root)
    output.mkdir(parents=True, exist_ok=True)
    reports = {}
    for scope, target in (('image', image), ('fs', str(Path(source_root).resolve()))):
        destination = output / ('trivy-' + scope + '.json')
        require(not destination.exists(), 'scanner output already exists')
        subject = ['--input', str(Path(config['oci_archive']).resolve())] if scope == 'image' and config.get('oci_archive') else [target]
        tool.run([scope, '--cache-dir', str(cache), '--skip-db-update', '--skip-java-db-update', '--scanners', 'vuln', '--format', 'json', '--output', str(destination), *subject], timeout=600)
        reports[scope], _ = read_document(destination)
    require(database_state(metadata, config['database_metadata_sha256'], now, config['max_database_age_seconds']) == state, 'scanner database changed during scan')
    with database.open('rb') as data:
        import hashlib
        require(hashlib.file_digest(data, 'sha256').hexdigest() == config['database_sha256'], 'scanner database bytes changed during scan')
    analyses, _ = read_document(config['analysis_path'], config['analysis_sha256'])
    findings = findings_from_trivy(reports, analyses)
    responses = response_state(config, findings, now)
    return {'scanner_status': 'success', 'tool_sha256': tool.sha256, 'database_sha256': config['database_sha256'], 'database_metadata': state, 'reports_sha256': {k: digest(canonical(v)) for k, v in reports.items()}, 'vulnerabilities': findings, 'critical_response': responses, 'image': image, 'scanned_at': now}
