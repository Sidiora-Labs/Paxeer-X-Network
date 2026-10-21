import argparse
import base64
import hashlib
import json
import math
import os
import pathlib
import re
import subprocess
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
import xml.etree.ElementTree as ET


def require(condition, message):
    if not condition:
        raise ValueError(message)


def sha(path):
    return hashlib.sha256(pathlib.Path(path).read_bytes()).hexdigest()


def save(path, value):
    pathlib.Path(path).write_text(json.dumps(value, indent=2, sort_keys=True) + '\n')
    pathlib.Path(path).chmod(0o600)


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, request, file, code, message, headers, target):
        return None


class Driver:
    def __init__(self, origin, session, output):
        self.origin = origin.rstrip('/')
        self.session = session
        self.output = output
        self.samples = []
        self.opener = urllib.request.build_opener(NoRedirect())

    def command(self, path, method='GET', body=None):
        url = self.origin + '/session/' + urllib.parse.quote(self.session, safe='') + path
        request = urllib.request.Request(url, data=None if body is None else json.dumps(body).encode(), method=method, headers={'Content-Type': 'application/json'})
        started, monotonic = time.time_ns(), time.monotonic_ns()
        with self.opener.open(request, timeout=30) as response:
            raw = response.read(4 * 1024 * 1024 + 1)
            require(len(raw) <= 4 * 1024 * 1024, 'Appium response exceeded its capture bound')
            value = json.loads(raw)
            self.samples.append({'path': path, 'method': method, 'status': response.status, 'startedUnixNs': started, 'durationMs': (time.monotonic_ns() - monotonic) / 1e6, 'response': value})
            require(not isinstance(value.get('value'), dict) or not value['value'].get('error'), 'Actual Appium driver failed: ' + str(value.get('value', {}).get('error')))
            return value['value']

    def element(self, action):
        require(action['using'] in ['accessibility id', 'id', 'xpath'], 'Actual native accessibility locator required')
        value = self.command('/element', 'POST', {'using': action['using'], 'value': action['value']})
        return value.get('element-6066-11e4-a52e-4f735466cecf') or value['ELEMENT']

    def actions(self, actions):
        require(isinstance(actions, list) and len(actions) > 0, 'Real installed-app journey actions required')
        for action in actions:
            require(action['type'] in ['click', 'fill', 'text', 'visible', 'back', 'background'], 'Unsupported native action; no runtime or provider injection permitted')
            if action['type'] == 'back':
                self.command('/back', 'POST', {})
            elif action['type'] == 'background':
                self.command('/appium/app/background', 'POST', {'seconds': action['seconds']})
            else:
                identifier = self.element(action)
                if action['type'] == 'click':
                    self.command('/element/' + identifier + '/click', 'POST', {})
                elif action['type'] == 'fill':
                    self.command('/element/' + identifier + '/clear', 'POST', {})
                    self.command('/element/' + identifier + '/value', 'POST', {'text': action['text']})
                elif action['type'] == 'visible':
                    require(self.command('/element/' + identifier + '/displayed') is True, 'Actual native control is not displayed')
                else:
                    require(action['expected'] in self.command('/element/' + identifier + '/text'), 'Native interface does not display expected authoritative text')

    def capture(self, name):
        xml = self.command('/source')
        (self.output / (name + '.xml')).write_text(xml)
        (self.output / (name + '.xml')).chmod(0o600)
        screenshot = self.command('/screenshot')
        image = base64.b64decode(screenshot, validate=True)
        require(image.startswith(b'\x89PNG\r\n\x1a\n'), 'Actual Appium PNG screenshot required')
        (self.output / (name + '.png')).write_bytes(image)
        return xml


def wait_file(output, name):
    deadline = time.monotonic() + 90
    while time.monotonic() < deadline:
        path = output / name
        if path.is_file():
            return json.loads(path.read_text())
        time.sleep(.1)
    raise ValueError('Real browser/device coordination timeout: ' + name)


def actual_command(argv, output, name):
    require(isinstance(argv, list) and all(isinstance(value, str) for value in argv), 'Actual executable argv required')
    result = subprocess.run(argv, capture_output=True, timeout=30, check=False)
    save(output / (name + '.json'), {'argv': argv, 'exitCode': result.returncode, 'stdout': result.stdout.decode(errors='strict'), 'stderr': result.stderr.decode(errors='strict'), 'observedUnixNs': time.time_ns()})
    require(result.returncode == 0, 'Actual device provenance command failed: ' + name)
    return result.stdout.decode().strip()


def authorized_watchlist(config, output):
    account = next(value for value in config['accounts'] if value['wallet'].lower() == config['crossDevice']['wallet'].lower())
    bearer = os.environ.get(account['tokenEnv'])
    require(bearer, 'Genuine same-wallet account authorization required')
    url = config['origins']['gateway'].rstrip('/') + '/v1/me/watchlist?limit=50'
    request = urllib.request.Request(url, headers={'Authorization': 'Bearer ' + bearer, 'Accept': 'application/json'})
    with urllib.request.build_opener(NoRedirect()).open(request, timeout=15) as response:
        raw = response.read(4 * 1024 * 1024 + 1)
        require(response.status == 200 and len(raw) <= 4 * 1024 * 1024, 'Current authoritative watchlist response unavailable')
        value = json.loads(raw)
    save(output / 'native-authoritative-watchlist.json', value)
    require(len(value['data']['items']) <= 50, 'Native page exceeds50')
    require(re.fullmatch(r'[0-9]+', value['revision'] or ''), 'Actual watchlist revision missing')
    pools = [row['poolAddress'].lower() for row in value['data']['items']]
    require(len(set(pools)) == len(pools), 'Duplicate native/server pool identity')
    return value, pools


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--config', required=True)
    args = parser.parse_args()
    config = json.loads(pathlib.Path(args.config).read_text())
    output = pathlib.Path(config['outputRoot'])
    native = config['nativeDevice']
    binding = json.loads(pathlib.Path(config['releaseBindingPath']).read_text())
    result = {'schemaVersion': 1, 'sourceManifestSha256': config['sourceManifestSha256'], 'startedUnixNs': time.time_ns(), 'passed': False, 'physicalDevice': False, 'networkSamples': [], 'watchlistReconciliation': {'passed': False}}
    driver, collector = None, None
    try:
        require(native['platform'] == 'android', 'Actual iOS artifact/device inspection adapter required; Android evidence cannot qualify iOS')
        origin = urllib.parse.urlsplit(native['appiumOrigin'])
        require(origin.scheme == 'https' or origin.scheme == 'http' and origin.hostname in ['127.0.0.1', 'localhost', '::1'], 'Trusted actual Appium origin required')
        require(not origin.username and not origin.password, 'No Appium credentials in evidence URLs')
        require(native['sessionId'] and native['accountSessionId'] and native['udid'] and native['packageId'], 'Actual installed app/session/device identity required')
        adb = pathlib.Path(native['adbPath']).resolve()
        require(adb.is_file() and sha(adb) == native['adbSha256'] and any(tool['sha256'] == native['adbSha256'] for tool in binding['toolArtifacts']), 'Approved actual Android platform-tools binary required')
        prefix = [str(adb), '-s', native['udid']]
        hardware = actual_command(prefix + ['shell', 'getprop', 'ro.hardware'], output, 'actual-hardware')
        qemu = actual_command(prefix + ['shell', 'getprop', 'ro.kernel.qemu'], output, 'actual-emulation-status')
        model = actual_command(prefix + ['shell', 'getprop', 'ro.product.model'], output, 'actual-device-model')
        require(qemu != '1' and hardware not in ['ranchu', 'goldfish'] and model == native['physicalModel'] and bool(hardware), 'Approved physical device provenance mismatch')
        installed = actual_command(prefix + ['shell', 'pm', 'path', native['packageId']], output, 'actual-installed-apk-path')
        apk_paths = [line.removeprefix('package:') for line in installed.splitlines() if line.startswith('package:')]
        require(len(apk_paths) == 1, 'Split install requires complete artifact-set binding; single signed APK expected')
        installed_path = output / 'actual-installed.apk'
        actual_command(prefix + ['pull', apk_paths[0], str(installed_path)], output, 'actual-installed-apk-pull')
        installed_digest = sha(installed_path)
        require(installed_digest == native['artifactSha256'] and any(artifact['kind'] == 'native' and artifact['sha256'] == installed_digest for artifact in binding['artifacts']), 'Installed APK differs from current admitted candidate')
        apksigner = pathlib.Path(native['apksignerPath']).resolve()
        require(apksigner.is_file() and sha(apksigner) == native['apksignerSha256'] and any(tool['sha256'] == native['apksignerSha256'] for tool in binding['toolArtifacts']), 'Approved actual Android signature inspector required')
        signature = actual_command([str(apksigner), 'verify', '--verbose', '--print-certs', str(installed_path)], output, 'actual-installed-apk-signature')
        certificate = re.search(r'Signer #1 certificate SHA-256 digest: ([a-fA-F0-9]{64})', signature)
        require(certificate and certificate.group(1).lower() == native['signerCertificateSha256'] and any(artifact['kind'] == 'native' and artifact['sha256'] == installed_digest and artifact['signerCertificateSha256'] == native['signerCertificateSha256'] and artifact['buildVariant'] == 'release' for artifact in binding['artifacts']), 'Installed signed release certificate differs from admitted authority')
        result.update({'physicalDevice': True, 'hardware': hardware, 'model': model, 'installedArtifactSha256': installed_digest, 'udid': native['udid']})
        capture = native['networkCapture']
        require(pathlib.Path(capture['argv'][0]).is_file() and sha(capture['argv'][0]) == capture['sha256'] and any(tool['sha256'] == capture['sha256'] for tool in binding['toolArtifacts']), 'Actual device/gateway request capture adapter required')
        stream = (output / 'native-capture.log').open('wb')
        environment = {**os.environ, 'L31_NATIVE_CAPTURE_OUTPUT': str(output / 'native-network.ndjson'), 'L31_NATIVE_ACCOUNT_SESSION_ID': native['accountSessionId']}
        collector = subprocess.Popen(capture['argv'], stdout=stream, stderr=subprocess.STDOUT, env=environment)
        driver = Driver(native['appiumOrigin'], native['sessionId'], output)
        capabilities = driver.command('')
        require(capabilities.get('platformName', '').lower() == 'android' and (capabilities.get('udid') or capabilities.get('appium:udid')) == native['udid'], 'Appium session does not match inspected device')
        require(driver.command('/appium/device/current_package') == native['packageId'], 'Actual foreground app differs from candidate')
        driver.actions(native['authenticationActions'])
        driver.actions(native['profileActions'])
        driver.capture('native-profile')
        driver.actions(native['portfolioActions'])
        portfolio_xml = driver.capture('native-portfolio')
        account = next(item for item in config['accounts'] if item['wallet'].lower() == config['crossDevice']['wallet'].lower())
        request = urllib.request.Request(config['origins']['gateway'].rstrip('/') + '/v1/me/portfolio?limit=50', headers={'Authorization': 'Bearer ' + os.environ[account['tokenEnv']], 'Accept': 'application/json'})
        with urllib.request.build_opener(NoRedirect()).open(request, timeout=15) as response:
            body = response.read(4 * 1024 * 1024 + 1)
            require(response.status == 200 and len(body) <= 4 * 1024 * 1024, 'Actual native portfolio snapshot unavailable')
            portfolio = json.loads(body)
        save(output / 'native-authoritative-portfolio.json', portfolio)
        require(len(portfolio['data']['items']) <= 50 and portfolio['revision'], 'Native initial portfolio bound or revision missing')
        summary = portfolio['data']['summary']
        require(summary['wallet'].lower() == account['wallet'].lower(), 'Native portfolio account ownership mismatch')
        tree = ET.fromstring(portfolio_xml)
        portfolio_names = [node.attrib.get('content-desc') or node.attrib.get('label') or node.attrib.get('name') or '' for node in tree.iter()]
        require('Known priced subtotal (USDL raw, 6 decimals): ' + summary['pricedSubtotalUsdlRaw'] in portfolio_names, 'Native subtotal differs from authoritative snapshot')
        realized = summary['realizedPnlRaw'] if summary['realizedPnlRaw'] is not None else 'Unavailable: incomplete basis'
        require('Realized profit/loss (USDL raw, signed): ' + realized in portfolio_names, 'Native realized total differs from authoritative snapshot')
        result['financialReconciliation'] = {'passed': True, 'revision': portfolio['revision'], 'watermark': portfolio['watermark'], 'responseSha256': hashlib.sha256(body).hexdigest(), 'rows': len(portfolio['data']['items'])}

        ready = wait_file(output, 'web-offline-ready.json')
        require(ready['wallet'].lower() == config['crossDevice']['wallet'].lower() and ready['pool'] == config['crossDevice']['pool'], 'Concurrent edit identities differ')
        before, before_pools = authorized_watchlist(config, output)
        require(config['crossDevice']['pool'] in before_pools, 'Real native concurrent remove requires an existing first-page pool')
        driver.actions(native['watchlistEditActions'])
        xml = driver.capture('native-watchlist-edited')
        value, pools = authorized_watchlist(config, output)
        pool = config['crossDevice']['pool']
        require(pool not in pools, 'Actual native remove did not durably remove the concurrent pool')
        save(output / 'native-edit.json', {'wallet': config['crossDevice']['wallet'], 'pool': pool, 'revision': value['revision'], 'passed': True, 'observedUnixNs': time.time_ns()})
        web = wait_file(output, 'web-reconciled.json')
        require(web['passed'] and web['pool'] == pool, 'Browser did not acknowledge reconciled same-wallet choice')
        driver.actions(native['reconnectActions'])
        xml = driver.capture('native-reconnected')
        tree = ET.fromstring(xml)
        names = [node.attrib.get('content-desc') or node.attrib.get('label') or node.attrib.get('name') or '' for node in tree.iter()]
        require(any(name == 'Watchlist' for name in names), 'Actual native watchlist accessibility landmark missing')
        actual_pools = [name.removeprefix('Pool: ').lower() for name in names if name.startswith('Pool: ')]
        value, pools = authorized_watchlist(config, output)
        require(actual_pools and len(actual_pools) == len(set(actual_pools)) and set(actual_pools) <= set(pools), 'Native visible watchlist does not reconcile to authoritative ordered page')
        indices = [pools.index(value) for value in actual_pools]
        require(indices == sorted(indices), 'Native watchlist reordered acknowledged server entries')
        require(pool in pools and pool in actual_pools, 'Browser reconciled edit missing on native reconnect')
        result['watchlistReconciliation'] = {'passed': True, 'revision': value['revision'], 'visiblePools': actual_pools, 'serverPools': pools}
        collector.terminate()
        require(collector.wait(timeout=10) in [0, -15], 'Actual network collector failed')
        stream.close()
        trace_path = output / 'native-network.ndjson'
        require(trace_path.is_file(), 'Actual native request trace adapter did not produce observations')
        require(trace_path.stat().st_size <= 16 * 1024 * 1024, 'Actual scoped native request capture exceeded its16MiB bound')
        traces = [json.loads(line) for line in trace_path.read_text().splitlines() if line.strip()]
        account = next(item for item in config['accounts'] if item['wallet'].lower() == config['crossDevice']['wallet'].lower())
        traces = [row for row in traces if row['sessionId'] == native['accountSessionId']]
        require(any(row['method'] == 'PATCH' and row['path'].endswith('/profiles/' + account['wallet'].lower()) and row['status'] == 200 for row in traces), 'Native profile save lacks an actual durable authorized HTTP mutation')
        require(traces and any(row['method'] == 'DELETE' and row['path'].endswith('/watchlist/' + pool) and row['status'] in [200, 204] for row in traces), 'Native remove lacks matching real authorized HTTP observation')
        for row in traces:
            require(row['requestId'] and isinstance(row['startedUnixNs'], int) and isinstance(row['durationMs'], (int, float)) and math.isfinite(row['durationMs']) and row['durationMs'] >= 0, 'Native trace lacks actual request identity/time')
            require(not any(key in row for key in ['accessToken', 'authorization', 'cookie', 'signature']), 'Raw credential appeared in native trace')
        result['networkSamples'] = traces
        result['passed'] = True
        return 0
    except Exception as failure:
        result['failure'] = {'name': type(failure).__name__, 'message': str(failure)}
        save(output / 'native-edit.json', {'passed': False, 'failure': result['failure'], 'pool': config.get('crossDevice', {}).get('pool'), 'wallet': config.get('crossDevice', {}).get('wallet')})
        return 1
    finally:
        if collector and collector.poll() is None:
            collector.terminate()
            try:
                collector.wait(timeout=5)
            except subprocess.TimeoutExpired:
                collector.kill()
                collector.wait()
        if driver:
            save(output / 'native-driver-raw.json', driver.samples)
        result['finishedUnixNs'] = time.time_ns()
        save(output / 'native-result.json', result)


if __name__ == '__main__':
    raise SystemExit(main())
