#!/usr/bin/env python3
import argparse
import hashlib
import json
import math
import os
from pathlib import Path
import plistlib
import re
import base64
import shutil
import subprocess
import time
import urllib.parse
import urllib.request
import xml.etree.ElementTree as ET
import zipfile

PACKAGE = 'ag.sidiora.mobile'
WIDTHS = (320, 375, 390, 768, 1024, 1440)
PLATFORMS = ('ios', 'android', 'chrome', 'firefox', 'safari', 'android-chrome', 'ios-safari')
JOURNEYS = ('discovery', 'search', 'token-reading', 'wallet-owned-execution', 'portfolio-reconciliation', 'profile-edit', 'watchlist', 'chat', 'live', 'push')
SHA = re.compile(r'^[a-f0-9]{64}$')


def require(condition, message):
    if not condition:
        raise ValueError(message)


def finite(value, name, minimum=0):
    require(type(value) in (int, float) and math.isfinite(value) and value >= minimum, 'Invalid observed ' + name)
    return value


def load(path):
    def pairs(values):
        result = {}
        for key, value in values:
            require(key not in result, 'Duplicate JSON key')
            result[key] = value
        return result
    return json.loads(Path(path).read_text(), object_pairs_hook=pairs, parse_constant=lambda value: (_ for _ in ()).throw(ValueError('Nonfinite JSON')))


def digest(path):
    path = Path(path)
    require(path.is_file() and not path.is_symlink(), 'Actual regular artifact required')
    with path.open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def reference(root, item):
    require(isinstance(item, dict) and set(item) == {'path', 'sha256'} and SHA.fullmatch(item['sha256']), 'Hashed raw artifact reference required')
    root = Path(root).resolve()
    raw = Path(item['path'])
    require(not raw.is_absolute() and '..' not in raw.parts, 'Artifact path must remain inside capture bundle')
    path = root / raw
    require(path.resolve().is_relative_to(root) and not path.is_symlink(), 'Artifact escape or symlink')
    require(digest(path) == item['sha256'], 'Raw artifact digest mismatch: ' + item['path'])
    return path


def source_digest(root, files):
    require(isinstance(files, list) and files and len(files) == len(set(files)), 'Exact candidate source inventory required')
    root = Path(root).resolve()
    manifest = []
    for name in sorted(files):
        path = root / name
        require(not Path(name).is_absolute() and path.resolve().is_relative_to(root), 'Source inventory escape')
        manifest.append(name + '\0' + digest(path))
    return hashlib.sha256('\n'.join(manifest).encode()).hexdigest()


def percentile(values, quantile):
    require(values and 0 < quantile <= 1, 'Observed samples required')
    ordered = sorted(finite(value, 'sample', 0.000001) for value in values)
    return ordered[math.ceil(len(ordered) * quantile) - 1]


def android_frames(text):
    durations, starts = [], []
    for section in text.split('---PROFILEDATA---')[1::2]:
        lines = [line.strip().split(',') for line in section.strip().splitlines() if line.strip()]
        require(lines and all(name in lines[0] for name in ('Flags', 'IntendedVsync', 'FrameCompleted')), 'Actual Android frame timing columns required')
        header = lines[0]
        for fields in lines[1:]:
            if len(fields) < len(header):
                require(False, 'Truncated Android frame timing row')
            flags = int(fields[header.index('Flags')])
            if flags != 0:
                continue
            start, end = int(fields[header.index('IntendedVsync')]), int(fields[header.index('FrameCompleted')])
            require(0 < start < end, 'Invalid OS frame interval')
            starts.append(start)
            durations.append((end - start) / 1000000)
    require(durations, 'No genuine rendered Android frames')
    return starts, durations


def android_heap(text):
    require(re.search(r'Size\s+Alloc\s+Free', text) is not None, 'Actual OS heap allocation header unavailable')
    allocations = {}
    for line in text.splitlines():
        match = re.fullmatch(r'\s*(Native Heap|Dalvik Heap)\s+([0-9\s]+)', line)
        if not match:
            continue
        numbers = list(map(int, match.group(2).split()))
        require(len(numbers) >= 7 and match.group(1) not in allocations, 'Actual unique native/Dalvik heap columns required')
        allocations[match.group(1)] = numbers[-2]
    require(set(allocations) == {'Native Heap', 'Dalvik Heap'}, 'Actual native and Dalvik heap allocations required')
    return sum(allocations.values()) / 1024


def timed_sample(root, sample):
    raw = load(reference(root, sample))
    require(set(raw) >= {'startedNs', 'readyNs', 'ui', 'artifactDigest', 'runId'}, 'Usable UI capture and monotonic timing required')
    require(type(raw['startedNs']) is int and type(raw['readyNs']) is int and 0 < raw['startedNs'] < raw['readyNs'], 'Invalid actual route timing')
    ui = ET.parse(reference(root, raw['ui']))
    require(not any('offline snapshot' in ' '.join(node.attrib.values()).lower() or 'service unavailable' in ' '.join(node.attrib.values()).lower() for node in ui.iter()), 'Cached/offline UI cannot qualify usable online discovery')
    require(any(node.attrib.get('text') == raw['readyText'] or any(node.attrib.get(key) == raw['readyText'] for key in ('content-desc','label','name','value')) for node in ui.iter()), 'Observed usable content missing from native hierarchy')
    return (raw['readyNs'] - raw['startedNs']) / 1000000, raw


def stream_interval(root, item, run_id, expected_topics):
    text = reference(root, item).read_text()
    events = []
    for line in text.splitlines():
        marker = line.find('SIDIORA_MEASUREMENT ')
        if marker < 0:
            continue
        value = json.loads(line[marker + len('SIDIORA_MEASUREMENT '):])
        if value.get('runId') == run_id:
            finite(value.get('at'), 'stream timestamp', 1)
            events.append(value)
    require(events, 'Actual native stream observations unavailable')
    start = None
    previous = None
    healthy = False
    sample_count = 0
    best = 0
    for event in events:
        if event.get('kind') == 'snapshot':
            healthy = event.get('healthy') is True and set(event.get('topics', [])) == set(expected_topics)
            if not healthy:
                start, previous, sample_count = None, None, 0
        if event.get('kind') != 'stream-state':
            continue
        if event.get('state') != 'live' or not healthy:
            start, previous, sample_count = None, None, 0
            continue
        if previous is not None and not 0 <= event['at'] - previous <= 10000:
            start, previous, sample_count = None, None, 0
        if start is None:
            start = event['at']
        previous = event['at']
        sample_count += 1
        if sample_count >= 361:
            best = max(best,event['at'] - start)
    require(best >= 1800000, 'Thirty minutes of actual uninterrupted five-topic observations with bounded heartbeat gaps required')
    return best


def instrument_values(root, item, columns, device_id):
    raw = reference(root, item)
    trace = ET.parse(raw)
    require(columns['provider'] == 'xcode-instruments' and columns['deviceId'] == device_id and columns['unit'] in ('ns', 'bytes'), 'Exact actual Instruments schema/device/unit mapping required')
    reference(root, columns['instrumentTrace'])
    rows = trace.findall(columns['rowXPath'])
    require(rows, 'Actual instrument table rows unavailable')
    result = []
    identities = {}
    for node in trace.iter():
        if node.get('id') and node.text and node.text.strip():
            identities[node.get('id')] = node.text.strip()
    for row in rows:
        values = []
        for selector in columns['valueXPaths']:
            node = row.find(selector)
            require(node is not None, 'Required raw instrument column unavailable')
            raw_value = identities.get(node.get('ref')) if node.get('ref') else (node.text or '').strip()
            require(raw_value is not None and re.fullmatch(r'[0-9]+', raw_value), 'Numeric actual instrument value required')
            values.append(int(raw_value))
        result.append(values)
    return result


def qualify_measurements(root, value, artifact_digest, run_id):
    require(value['artifactDigest'] == artifact_digest and value['runId'] == run_id and value['physicalDevice'] is True, 'Device measurement binding mismatch')
    require(value['platform'] in ('ios', 'android') and value['hardware'] and value['os'] and value['network'], 'Declared physical reference hardware/OS/network required')
    finite(value['temperatureC'], 'device temperature')
    hardware_text = reference(root,value['hardwareTrace']).read_text()
    if value['platform'] == 'android':
        require(hardware_text.strip() == value['hardware'] and reference(root,value['osTrace']).read_text().strip() == value['os'], 'Actual Android hardware/OS differs from benchmark declaration')
        require(reference(root,value['physicalTrace']).read_bytes().strip() in (b'0',b''), 'Emulator cannot qualify physical benchmark')
        battery = reference(root,value['temperatureTrace']).read_text()
        temperature = re.search(r'^\s*temperature:\s*(\d+)\s*$',battery,re.M)
        require(temperature is not None and int(temperature.group(1))/10 == value['temperatureC'], 'Actual device temperature differs from declared sample')
    else:
        actual_lines = hardware_text.split('== Simulators ==')[0]
        require('== Devices ==' in actual_lines and value['deviceId'] in actual_lines and value['hardware'] in actual_lines and value['os'] in actual_lines, 'Actual physical iOS inventory differs from declaration')
        thermal = load(reference(root,value['temperatureTrace']))
        require(thermal['deviceId'] == value['deviceId'] and thermal['temperatureC'] == value['temperatureC'] and thermal['operator'] and thermal['observedAt'], 'Actual instrument/operator temperature observation unavailable')
    reference(root,value['networkTrace'])
    require(len({item['path'] for item in value['frameTraces']}) == len(value['frameTraces']) and len({item['path'] for item in value['heapTraces']}) == len(value['heapTraces']), 'Repeated OS trace artifact')
    require(type(value['routeCycles']) is int and len(value['coldStarts']) >= 20 and len(value['warmRoutes']) >= 20 and value['routeCycles'] >= 20, 'Complete native start and route-cycle workload required')
    cold, warm = [], []
    for collection, target in ((value['coldStarts'], cold), (value['warmRoutes'], warm)):
        require(len({item['path'] for item in collection}) == len(collection), 'Repeated raw timing artifact')
        for sample in collection:
            elapsed, raw = timed_sample(root, sample)
            require(raw['artifactDigest'] == artifact_digest and raw['runId'] == run_id, 'Native sample belongs to another candidate/run')
            target.append(elapsed)
    require(len(set(value['topics'])) == 5, 'Five distinct actual topics required')
    duration = stream_interval(root, value['streamTrace'], run_id, value['topics'])
    frames = []
    windows = []
    all_frames = {}
    require(len(value['frameTraces']) >= (180 if value['platform'] == 'android' else 1), 'Continuous OS frame captures required throughout thirty-minute scroll')
    for item in value['frameTraces']:
        path = reference(root, item)
        if value['platform'] == 'android':
            timestamps, durations = android_frames(path.read_text())
        else:
            require(value['iosFrameColumns']['unit'] == 'ns' and len(value['iosFrameColumns']['valueXPaths']) == 2, 'Actual frame start/duration nanosecond columns required')
            rows = instrument_values(root, item, value['iosFrameColumns'], value['deviceId'])
            timestamps = [row[0] for row in rows]
            durations = [finite(row[1], 'actual iOS frame duration', 1) / 1000000 for row in rows]
        windows.append((min(timestamps), max(timestamps)))
        for timestamp, duration_ms in zip(timestamps, durations):
            require(timestamp not in all_frames or all_frames[timestamp] == duration_ms, 'Conflicting raw frame identity')
            all_frames[timestamp] = duration_ms
    windows.sort()
    require((windows[-1][1] - windows[0][0]) / 1000000 >= 1800000, 'OS frame capture does not span steady workload')
    require(all((b[0] - a[1]) / 1000000 <= 15000 for a, b in zip(windows, windows[1:])), 'Unobserved scroll interval exceeds capture budget')
    heaps = []
    for item in value['heapTraces']:
        path = reference(root, item)
        if value['platform'] == 'android':
            heaps.append(android_heap(path.read_text()))
        else:
            require(value['iosHeapColumns']['unit'] == 'bytes' and len(value['iosHeapColumns']['valueXPaths']) == 1, 'Actual Instruments allocated heap byte column required')
            rows = instrument_values(root, item, value['iosHeapColumns'], value['deviceId'])
            heaps.extend(finite(row[0], 'actual iOS allocated heap', 1) / 1048576 for row in rows)
    require(len(heaps) >= 182 and heaps[0] > 0, 'Baseline, continuous and post-cycle actual heap samples required')
    frames = list(all_frames.values())
    ordered_starts = sorted(all_frames)
    cadence = [(b-a)/1000000 for a,b in zip(ordered_starts,ordered_starts[1:])]
    cold_p75, warm_p75 = percentile(cold, .75), percentile(warm, .75)
    frame_p99, median_fps = percentile(frames, .99), 1000 / percentile(cadence, .5)
    if value['platform'] == 'android':
        retained_before = android_heap(reference(root, value['retainedBaseline']).read_text())
        retained_after = android_heap(reference(root, value['retainedFinal']).read_text())
    else:
        retained_rows = instrument_values(root, value['retainedAllocations'], value['iosHeapColumns'], value['deviceId'])
        require(len(retained_rows) >= 2, 'Before/after twenty-cycle retained allocation rows required')
        retained_before, retained_after = retained_rows[0][0]/1048576, retained_rows[-1][0]/1048576
    require(retained_before > 0, 'Positive actual retained baseline required')
    growth = (retained_after - retained_before) / retained_before
    require(cold_p75 <= 2500 and warm_p75 <= 500, 'Usable native start/route latency exceeded')
    require(frame_p99 <= 50 and median_fps >= 55, 'Native scroll frame budget exceeded')
    require(max([*heaps,retained_before,retained_after]) <= 250 and growth <= .1, 'Foreground/retained heap budget exceeded')
    return {'coldP75Ms': cold_p75, 'warmP75Ms': warm_p75, 'frameP99Ms': frame_p99, 'medianFps': median_fps, 'heapMaxMiB': max([*heaps,retained_before,retained_after]), 'retainedGrowth': growth, 'durationMs': duration}


class Capture:
    def __init__(self, root):
        self.root = Path(root).resolve()
        self.root.mkdir(parents=True, exist_ok=False)
        os.chmod(self.root, 0o700)
        self.counter = 0
        self.last_command = None

    def write(self, name, value):
        self.counter += 1
        path = self.root / (str(self.counter).zfill(6) + '-' + name)
        data = value if isinstance(value, bytes) else value.encode() if isinstance(value, str) else json.dumps(value, allow_nan=False).encode()
        path.write_bytes(data)
        os.chmod(path, 0o600)
        return {'path': path.name, 'sha256': digest(path)}

    def command(self, command, name, timeout=60):
        started = time.monotonic_ns()
        result = subprocess.run(command, stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=timeout, check=False)
        stdout = self.write(name + '.stdout', result.stdout)
        stderr = self.write(name + '.stderr', result.stderr)
        self.last_command = self.write(name + '.command.json', {'argv': command, 'startedNs': started, 'endedNs': time.monotonic_ns(), 'exitCode': result.returncode, 'stdout': stdout, 'stderr': stderr})
        require(result.returncode == 0, 'Actual provider command failed; inspect raw capture ' + stderr['path'])
        return result.stdout, stdout


def approved_plan(path):
    plan = load(path)
    require(Path(plan['sourceRoot']).resolve() == Path(__file__).resolve().parents[4], 'Harness must bind its own actual integrated source root')
    require(plan['environment'] == 'staging' and plan['syntheticAccountsOnly'] is True and plan['signingOwner'] and plan['backendRevision'], 'Approved staging/signing/synthetic-account inputs required')
    require(SHA.fullmatch(plan['artifactDigest']) and SHA.fullmatch(plan['sourceDigest']) and plan['sourceDigest'] == source_digest(plan['sourceRoot'], plan['sourceFiles']), 'Exact current-source binding required')
    require(digest(plan['artifact']) == plan['artifactDigest'], 'Signed candidate file digest mismatch')
    require(re.fullmatch(r'measure_[A-Za-z0-9_-]{16,96}', plan['runId']), 'Bound measurement run identifier required')
    origin = urllib.parse.urlsplit(plan['origin'])
    require(origin.scheme == 'https' and origin.netloc and not origin.username and not origin.password and not origin.query and not origin.fragment and origin.path in ('', '/'), 'Approved staging HTTPS origin required')
    require(len(set(plan['topics'])) == 5 and all(topic.startswith('market.') for topic in plan['topics']), 'Explicit five-topic native configuration required')
    return plan


def capture_android(plan, output):
    require(plan['platform'] == 'android' and re.fullmatch(r'[A-Za-z0-9._:-]{1,128}', plan['deviceId']), 'Explicit actual Android device required')
    capture = Capture(output)
    artifact_ref = capture.write('signed-candidate.apk',Path(plan['artifact']).read_bytes())
    require(artifact_ref['sha256'] == plan['artifactDigest'], 'Copied native candidate changed')
    candidate_path = str(capture.root / artifact_ref['path'])
    prefix = [plan['adb'], '-s', plan['deviceId']]
    def adb(*args, name='adb', timeout=60):
        return capture.command(prefix + list(args), name, timeout)
    state, _ = adb('get-state', name='device-state')
    qemu, physical_ref = adb('shell', 'getprop', 'ro.kernel.qemu', name='physical-device')
    require(state.strip() == b'device' and qemu.strip() in (b'0', b''), 'Physical connected Android device required')
    sdk, _ = adb('shell', 'getprop', 'ro.build.version.sdk', name='android-sdk')
    require(int(sdk.strip()) >= 30, 'Android 11 minimum required')
    hardware, hardware_ref = adb('shell', 'getprop', 'ro.product.model', name='hardware')
    os_version, os_ref = adb('shell', 'getprop', 'ro.build.version.release', name='os-version')
    battery, temperature_ref = adb('shell', 'dumpsys', 'battery', name='temperature')
    _, network_ref = adb('shell','dumpsys','connectivity',name='network-state')
    temperature = re.search(rb'^\s*temperature:\s*(\d+)\s*$', battery, re.M)
    require(temperature is not None, 'Actual device battery temperature unavailable')
    signature, signature_ref = capture.command([plan['apksigner'], 'verify', '--verbose', '--print-certs', candidate_path], 'apk-signature')
    signature_verification = capture.last_command
    actual = re.findall(rb'Signer #\d+ certificate SHA-256 digest: ([a-fA-F0-9]{64})', signature)
    require(len(actual) == 1 and actual[0].decode().lower() == plan['signingCertificateSha256'], 'Candidate signing certificate differs from approved owner')
    require(Path(plan['artifact']).stat().st_size <= 100 * 1048576, 'Native download exceeds 100MiB')
    adb('install', '-r', candidate_path, name='install-signed-release', timeout=180)
    package, _ = adb('shell', 'dumpsys', 'package', PACKAGE, name='installed-package')
    require(b'DEBUGGABLE' not in package and PACKAGE.encode() in package, 'Installable nondebug release package required')
    def hierarchy(name):
        adb('shell', 'uiautomator', 'dump', '/sdcard/sidiora-qualification.xml', name=name + '-dump')
        raw, ref = adb('exec-out', 'cat', '/sdcard/sidiora-qualification.xml', name=name + '-hierarchy')
        return ET.fromstring(raw), ref
    def ready(text, deadline):
        while time.monotonic() < deadline:
            tree, ref = hierarchy('ready')
            if any(node.get('text') == text or node.get('content-desc') == text for node in tree.iter('node')):
                return time.monotonic_ns(), ref
        raise ValueError('Actual usable UI timed out')
    def tap(label):
        tree, _ = hierarchy('tap')
        nodes = [node for node in tree.iter('node') if node.get('content-desc') == label and node.get('clickable') == 'true']
        require(len(nodes) == 1, 'One actual labelled native action required: ' + label)
        match = re.fullmatch(r'\[(\d+),(\d+)\]\[(\d+),(\d+)\]', nodes[0].get('bounds', ''))
        require(match is not None, 'Actual action bounds unavailable')
        x1, y1, x2, y2 = map(int, match.groups())
        require(x2 > x1 and y2 > y1, 'Native action has no visible geometry')
        started = time.monotonic_ns()
        adb('shell', 'input', 'tap', str((x1+x2)//2), str((y1+y2)//2), name='route-tap')
        return started
    cold, warm = [], []
    for _ in range(20):
        adb('shell', 'am', 'force-stop', PACKAGE, name='cold-stop')
        start = time.monotonic_ns()
        adb('shell', 'am', 'start', '-W', '-n', PACKAGE + '/.MainActivity', name='cold-launch')
        end, ui = ready(plan['discoveryReadyText'], time.monotonic() + 30)
        cold.append(capture.write('cold.json', {'startedNs': start, 'readyNs': end, 'readyText': plan['discoveryReadyText'], 'ui': ui, 'artifactDigest': plan['artifactDigest'], 'runId': plan['runId']}))
    baseline, baseline_ref = adb('shell', 'dumpsys', 'meminfo', PACKAGE, name='baseline-heap')
    android_heap(baseline.decode())
    tap('Discover')
    ready(plan['discoveryReadyText'], time.monotonic() + 30)
    adb('logcat', '-c', name='clear-observation-log')
    frames, heaps = [], [baseline_ref]
    started = time.monotonic()
    while time.monotonic() - started < 1815:
        adb('shell', 'dumpsys', 'gfxinfo', PACKAGE, 'reset', name='frame-window-reset')
        interval = time.monotonic()
        while time.monotonic() - interval < 1:
            swipe = plan['scrollGesture']
            require(len(swipe) == 5 and all(type(n) is int and n >= 0 for n in swipe), 'Actual declared scroll coordinates/duration required')
            adb('shell', 'input', 'swipe', *map(str, swipe), name='actual-scroll')
        _, frame_ref = adb('shell', 'dumpsys', 'gfxinfo', PACKAGE, 'framestats', name='frames')
        _, heap_ref = adb('shell', 'dumpsys', 'meminfo', PACKAGE, name='heap')
        frames.append(frame_ref)
        heaps.append(heap_ref)
    _, final_heap = adb('shell', 'dumpsys', 'meminfo', PACKAGE, name='final-heap')
    heaps.append(final_heap)
    _, stream_ref = adb('logcat', '-d', '-v', 'brief', 'ReactNativeJS:I', '*:S', name='stream-observation')
    _, retained_baseline = adb('shell', 'dumpsys', 'meminfo', PACKAGE, name='retained-before-cycles')
    require(len(plan['routeCycle']) >= 2, 'Explicit functional route cycle required')
    for _ in range(20):
        for route in plan['routeCycle']:
            start = tap(route['label'])
            end, ui = ready(route['readyText'], time.monotonic() + 30)
            warm.append(capture.write('warm.json', {'startedNs': start, 'readyNs': end, 'readyText': route['readyText'], 'ui': ui, 'artifactDigest': plan['artifactDigest'], 'runId': plan['runId']}))
    tap('Discover')
    ready(plan['discoveryReadyText'], time.monotonic() + 30)
    _, retained_final = adb('shell', 'dumpsys', 'meminfo', PACKAGE, name='retained-after-cycles')
    screenshot, screenshot_ref = adb('exec-out', 'screencap', '-p', name='screen')
    require(screenshot.startswith(b'\x89PNG\r\n\x1a\n'), 'Actual native screenshot unavailable')
    result = {'artifactDigest': plan['artifactDigest'], 'sourceDigest': plan['sourceDigest'], 'runId': plan['runId'], 'platform': 'android', 'physicalDevice': True, 'deviceId': plan['deviceId'], 'hardware': hardware.decode().strip(), 'os': os_version.decode().strip(), 'network': plan['network'], 'temperatureC': int(temperature.group(1)) / 10, 'hardwareTrace': hardware_ref, 'osTrace': os_ref, 'temperatureTrace': temperature_ref,'physicalTrace':physical_ref,'networkTrace':network_ref, 'signatureTrace': signature_ref,'signatureVerification':signature_verification,'artifact':artifact_ref, 'coldStarts': cold, 'warmRoutes': warm, 'frameTraces': frames, 'heapTraces': heaps, 'routeCycles': 20, 'topics': plan['topics'], 'streamTrace': stream_ref, 'retainedBaseline': retained_baseline, 'retainedFinal': retained_final, 'screenshot': screenshot_ref}
    capture.write('measurements.json', result)
    return result


class DeviceDriver:
    def __init__(self, endpoint, session_id, capture):
        parsed = urllib.parse.urlsplit(endpoint)
        require(parsed.scheme == 'https' and not parsed.username and not parsed.password and not parsed.query and not parsed.fragment, 'Approved HTTPS WebDriver provider required')
        require(re.fullmatch(r'[A-Za-z0-9_-]{1,128}', session_id), 'Actual existing device session required')
        self.origin = endpoint.rstrip('/')
        self.session = session_id
        self.capture = capture

    def call(self, suffix, value=None, method=None, max_bytes=8388608):
        url = self.origin + '/session/' + self.session + suffix
        request = urllib.request.Request(url, data=None if value is None else json.dumps(value, allow_nan=False).encode(), headers={'Content-Type': 'application/json'}, method=method or ('POST' if value is not None else 'GET'))
        class NoRedirect(urllib.request.HTTPRedirectHandler):
            def redirect_request(self, request, fp, code, msg, headers, newurl):
                raise ValueError('Device provider redirect rejected')
        started = time.monotonic_ns()
        with urllib.request.build_opener(NoRedirect).open(request, timeout=60) as response:
            require(type(max_bytes) is int and max_bytes > 0, 'Explicit provider response budget required')
            raw = response.read(max_bytes+1)
            require(len(raw) <= max_bytes, 'Device provider response too large')
        reference_item = self.capture.write('webdriver-response.json', raw)
        self.capture.write('webdriver-command.json', {'method': request.method, 'path': suffix, 'startedNs': started, 'endedNs': time.monotonic_ns(), 'response': reference_item})
        parsed = json.loads(raw)
        require('value' in parsed and not (isinstance(parsed['value'], dict) and 'error' in parsed['value']), 'Actual device automation provider failed')
        return parsed['value']

    def mobile(self, command, arguments, max_bytes=8388608):
        return self.call('/execute/sync', {'script': 'mobile: ' + command, 'args': [arguments]},max_bytes=max_bytes)

    def ready(self, label, deadline):
        while time.monotonic() < deadline:
            text = self.call('/source')
            require(isinstance(text, str), 'Actual XCUITest hierarchy unavailable')
            tree = ET.fromstring(text)
            if any(any(node.get(key) == label for key in ('name', 'label', 'value')) for node in tree.iter()):
                return time.monotonic_ns(), self.capture.write('ios-hierarchy.xml', text)
        raise ValueError('Actual usable iOS content timed out')

    def tap(self, label):
        element = self.call('/element', {'using': 'accessibility id', 'value': label})
        key = element.get('element-6066-11e4-a52e-4f735466cecf')
        require(key and re.fullmatch(r'[A-Za-z0-9_-]+', key), 'Actual labelled XCUITest element required')
        started = time.monotonic_ns()
        self.call('/element/' + key + '/click', {}, 'POST')
        return started


def capture_ios(plan, output):
    require(plan['platform'] == 'ios' and plan['deviceId'] and plan['instrumentTemplate'], 'Explicit physical iOS device and real Instruments template required')
    capture = Capture(output)
    artifact_ref = capture.write('signed-candidate.ipa',Path(plan['artifact']).read_bytes())
    require(artifact_ref['sha256'] == plan['artifactDigest'], 'Copied native candidate changed')
    xcrun = plan['xcrun']
    inventory, inventory_ref = capture.command([xcrun, 'xctrace', 'list', 'devices'], 'ios-physical-device-inventory')
    device_lines = inventory.decode().split('== Simulators ==')[0]
    matching = [line for line in device_lines.splitlines() if plan['deviceId'] in line]
    require('== Devices ==' in device_lines and len(matching) == 1 and plan['hardware'] in matching[0] and plan['os'] in matching[0], 'Actual physical iOS identity/hardware/OS unavailable')
    require(Path(plan['artifact']).suffix == '.ipa', 'Signed IPA artifact required')
    with zipfile.ZipFile(capture.root/artifact_ref['path']) as ipa:
        files = ipa.namelist()
        plist_paths = [name for name in files if re.fullmatch(r'Payload/[^/]+\.app/Info.plist', name)]
        require(len(plist_paths) == 1 and not any(Path(name).is_absolute() or '..' in Path(name).parts for name in files) and sum(item.file_size for item in ipa.infolist()) <= 512*1048576 and all((item.external_attr >> 16) & 0o170000 != 0o120000 for item in ipa.infolist()), 'Invalid actual native IPA layout')
        ipa.extractall(capture.root / 'ipa')
    app = (capture.root / 'ipa' / plist_paths[0]).parent
    info = plistlib.loads((app / 'Info.plist').read_bytes())
    require(info['CFBundleIdentifier'] == PACKAGE and tuple(map(int, info['MinimumOSVersion'].split('.'))) >= (17,0), 'Signed iOS bundle identity/minimum version mismatch')
    capture.command([plan['codesign'], '--verify', '--deep', '--strict', str(app)], 'ios-signature-verify')
    signature_verification = capture.last_command
    certificate_prefix = str(capture.root / 'signing-certificate')
    capture.command([plan['codesign'], '-d', '--extract-certificates', certificate_prefix, str(app)], 'ios-signing-certificate')
    certificate_ref={'path':Path(certificate_prefix+'0').name,'sha256':digest(certificate_prefix+'0')}
    require(certificate_ref['sha256'] == plan['signingCertificateSha256'], 'Actual native signing certificate differs from approved owner')
    capture.command([xcrun, 'devicectl', 'device', 'install', 'app', '--device', plan['deviceId'], str(app)], 'ios-install', 180)
    driver = DeviceDriver(plan['deviceProviderURL'], plan['deviceSessionId'], capture)
    device_session = driver.call('')
    require(device_session.get('appium:udid',device_session.get('udid')) == plan['deviceId'], 'Actual XCUITest session is bound to another device')
    cold, warm = [], []
    for _ in range(20):
        driver.mobile('terminateApp', {'bundleId': PACKAGE})
        started = time.monotonic_ns()
        driver.mobile('launchApp', {'bundleId': PACKAGE})
        ended, ui = driver.ready(plan['discoveryReadyText'], time.monotonic() + 30)
        cold.append(capture.write('ios-cold.json', {'startedNs': started, 'readyNs': ended, 'readyText': plan['discoveryReadyText'], 'ui': ui, 'artifactDigest': plan['artifactDigest'], 'runId': plan['runId']}))
    driver.tap('Discover')
    driver.ready(plan['discoveryReadyText'], time.monotonic() + 30)
    driver.mobile('startPerfRecord', {'timeout': 1900000, 'profileName': plan['instrumentTemplate'], 'pid': 'current'})
    started = time.monotonic()
    try:
        while time.monotonic() - started < 1815:
            driver.mobile('swipe', {'direction': 'up'})
        cycle_started = time.monotonic_ns()
        require(len(plan['routeCycle']) >= 2, 'Actual functional iOS route cycle required')
        for _ in range(20):
            for route in plan['routeCycle']:
                started = driver.tap(route['label'])
                ended, ui = driver.ready(route['readyText'], time.monotonic() + 30)
                warm.append(capture.write('ios-warm.json', {'startedNs': started, 'readyNs': ended, 'readyText': route['readyText'], 'ui': ui, 'artifactDigest': plan['artifactDigest'], 'runId': plan['runId']}))
        driver.tap('Discover')
        driver.ready(plan['discoveryReadyText'], time.monotonic() + 30)
        cycle_ended = time.monotonic_ns()
    finally:
        require(type(plan['instrumentArchiveMaxBytes']) is int and plan['instrumentArchiveMaxBytes'] > 0, 'Approved full Instruments archive capacity required')
        encoded = driver.mobile('stopPerfRecord', {'profileName': plan['instrumentTemplate']},max_bytes=(plan['instrumentArchiveMaxBytes']*4//3)+1048576)
    require(isinstance(encoded, str) and encoded, 'Actual Instruments archive unavailable')
    archived = base64.b64decode(encoded, validate=True)
    require(len(archived) <= plan['instrumentArchiveMaxBytes'], 'Actual Instruments archive exceeds approved capacity')
    archive_ref = capture.write('instruments.zip', archived)
    driver.mobile('backgroundApp', {'seconds': 5})
    screenshot = driver.call('/screenshot')
    screenshot_ref = capture.write('ios-screen.png', base64.b64decode(screenshot, validate=True))
    result = {'artifactDigest': plan['artifactDigest'], 'sourceDigest': plan['sourceDigest'], 'runId': plan['runId'], 'platform': 'ios', 'physicalDevice': True, 'deviceId': plan['deviceId'], 'hardwareTrace': inventory_ref,'artifact':artifact_ref,'signature':certificate_ref,'signatureVerification':signature_verification, 'coldStarts': cold, 'warmRoutes': warm, 'routeCycles': 20,'cycleStartedNs':cycle_started,'cycleEndedNs':cycle_ended, 'topics': plan['topics'], 'instrumentTrace': archive_ref, 'screenshot': screenshot_ref, 'status': 'captured-unqualified', 'requiredExports': ['actual-device-frame-start-and-duration-nanoseconds', 'actual-process-allocated-heap-bytes-including-before-and-after-route-cycle', 'native-stream-log-and-temperature-network-observation']}
    capture.write('ios-measurements-input.json', result)
    return result


def export_ios(plan, output):
    require(plan['platform'] == 'ios', 'Actual physical iOS capture required')
    input_path = Path(plan['iosCaptureInput']).resolve()
    require(digest(input_path) == plan['iosCaptureInputSha256'], 'Actual physical capture input changed')
    original = load(input_path)
    require(original['artifactDigest'] == plan['artifactDigest'] and original['sourceDigest'] == plan['sourceDigest'] and original['runId'] == plan['runId'], 'Actual iOS capture belongs to another candidate')
    capture = Capture(output)
    for path in input_path.parent.iterdir():
        require(not path.is_symlink(), 'Capture symlink rejected')
        if path.is_file():
            shutil.copy2(path,capture.root/path.name)
    capture.counter = 1000000
    archived = reference(capture.root, original['instrumentTrace'])
    extracted = capture.root / 'instruments'
    with zipfile.ZipFile(archived) as zipped:
        require(all(not Path(item.filename).is_absolute() and '..' not in Path(item.filename).parts and (item.external_attr >> 16) & 0o170000 != 0o120000 for item in zipped.infolist()), 'Actual Instruments archive contains unsafe paths')
        zipped.extractall(extracted)
    candidates = [item for item in extracted.rglob('*.trace') if item.is_dir()]
    require(len(candidates) == 1, 'One actual Xcode instrument trace required')
    frame_xml, heap_xml = capture.root/'instruments-frames.xml', capture.root/'instruments-heap.xml'
    for name, xpath, destination in (('frames',plan['frameExportXPath'],frame_xml),('heap',plan['heapExportXPath'],heap_xml)):
        require(xpath, 'Exact actual Xcode trace schema export XPath required')
        capture.command([plan['xcrun'],'xctrace','export','--input',str(candidates[0]),'--xpath',xpath,'--output',str(destination)],'actual-instruments-'+name,180)
    frame_ref={'path':frame_xml.name,'sha256':digest(frame_xml)}
    heap_ref={'path':heap_xml.name,'sha256':digest(heap_xml)}
    thermal=capture.write('actual-temperature-observation.json',Path(plan['actualTemperatureObservation']).read_bytes())
    stream=capture.write('actual-native-stream-observation.log',Path(plan['nativeStreamLog']).read_bytes())
    network=capture.write('actual-network-observation',Path(plan['networkObservation']).read_bytes())
    actual_thermal=load(reference(capture.root,thermal))
    frame_columns={**plan['iosFrameColumns'],'provider':'xcode-instruments','deviceId':plan['deviceId'],'instrumentTrace':original['instrumentTrace'],'unit':'ns'}
    heap_columns={**plan['iosHeapColumns'],'provider':'xcode-instruments','deviceId':plan['deviceId'],'instrumentTrace':original['instrumentTrace'],'unit':'bytes'}
    result={**original,'hardware':plan['hardware'],'os':plan['os'],'network':plan['network'],'temperatureC':actual_thermal['temperatureC'],'temperatureTrace':thermal,'networkTrace':network,'frameTraces':[frame_ref],'heapTraces':[heap_ref],'retainedAllocations':heap_ref,'iosFrameColumns':frame_columns,'iosHeapColumns':heap_columns,'streamTrace':stream}
    capture.write('actual-ios-measurements.json',result)
    return {'status':'exported-unqualified','captureRoot':str(capture.root)}



def build_signed(plan, output):
    require(plan['environment'] == 'staging' and plan['syntheticAccountsOnly'] is True and plan['signingOwner'], 'Approved signing owner/staging input required')
    require(Path(plan['sourceRoot']).resolve() == Path(__file__).resolve().parents[4] and plan['sourceDigest'] == source_digest(plan['sourceRoot'], plan['sourceFiles']), 'Exact signed-candidate source binding required')
    capture = Capture(output)
    mobile = Path(plan['sourceRoot']) / 'apps/launchpad/mobile'
    platform = plan['platform']
    if platform == 'android':
        required = ('SIDIORA_ANDROID_KEYSTORE','SIDIORA_ANDROID_STORE_PASSWORD','SIDIORA_ANDROID_KEY_ALIAS','SIDIORA_ANDROID_KEY_PASSWORD','SIDIORA_SIGNING_OWNER','SIDIORA_PUBLIC_CONFIGURATION','SIDIORA_ASSOCIATED_HOST')
        require(all(os.environ.get(name) for name in required) and os.environ['SIDIORA_SIGNING_OWNER'] == plan['signingOwner'], 'Actual managed Android signing-provider inputs missing')
        capture.command([str(mobile / 'android/gradlew'), '-p', str(mobile / 'android'), ':app:assembleRelease', '--console=plain'], 'signed-android-release-build', 1800)
        artifact = mobile / 'android/app/build/outputs/apk/release/app-release.apk'
        raw, signature = capture.command([plan['apksigner'], 'verify', '--print-certs', str(artifact)], 'built-apk-owner')
        require(plan['signingCertificateSha256'] in raw.decode().lower(), 'Built APK signer differs from approved owner')
    elif platform == 'ios':
        required = ('SIDIORA_IOS_TEAM','SIDIORA_IOS_SIGNING_IDENTITY','SIDIORA_IOS_PROVISIONING_PROFILE','SIDIORA_SIGNING_OWNER')
        require(all(os.environ.get(name) for name in required) and os.environ['SIDIORA_SIGNING_OWNER'] == plan['signingOwner'], 'Actual managed iOS signing-provider inputs missing')
        require(digest(plan['exportOptions']) == plan['exportOptionsSha256'], 'Approved store export configuration missing or changed')
        archive, export = capture.root / 'Sidiora.xcarchive', capture.root / 'signed-export'
        capture.command([plan['xcodebuild'], '-workspace', str(mobile / 'ios/Sidiora.xcworkspace'), '-scheme', 'Sidiora', '-configuration', 'Release', '-destination', 'generic/platform=iOS', '-archivePath', str(archive), 'archive'], 'signed-ios-release-build', 1800)
        capture.command([plan['xcodebuild'], '-exportArchive', '-archivePath', str(archive), '-exportPath', str(export), '-exportOptionsPlist', plan['exportOptions']], 'approved-ios-export', 600)
        candidates = list(export.glob('*.ipa'))
        require(len(candidates) == 1, 'One actual exported signed native IPA required')
        artifact = candidates[0]
        signature = None
    else:
        raise ValueError('Actual native platform required')
    require(source_digest(plan['sourceRoot'],plan['sourceFiles']) == plan['sourceDigest'], 'Source changed during signed build')
    require(artifact.is_file() and artifact.stat().st_size <= 100*1048576, 'Actual signed candidate/download budget failed')
    artifact_ref = capture.write('signed-candidate' + artifact.suffix, artifact.read_bytes())
    result = {'sourceDigest': plan['sourceDigest'], 'artifactDigest': artifact_ref['sha256'], 'platform': platform, 'signingOwner': plan['signingOwner'], 'artifact': artifact_ref, 'signature': signature, 'status': 'built-unqualified'}
    capture.write('signed-build-input.json', result)
    return result



def authoritative_financial(root, row):
    proof = row['financial']
    def result(name):
        value = load(reference(root, proof[name]))
        require(value.get('jsonrpc') == '2.0' and 'error' not in value and isinstance(value.get('result'), dict), 'Actual authoritative RPC reply required')
        return value['result']
    transaction, receipt, block = result('transaction'), result('receipt'), result('block')
    expected_hash = proof['transactionHash'].lower()
    require(re.fullmatch(r'0x[0-9a-f]{64}', expected_hash) and proof['requestId'], 'Bound submitted operation identity required')
    require(transaction['hash'].lower() == receipt['transactionHash'].lower() == expected_hash, 'Financial transaction/receipt mismatch')
    require(transaction['from'].lower() == proof['from'].lower() and transaction['to'].lower() == proof['to'].lower(), 'Actual sender/destination mismatch')
    require(receipt['blockHash'].lower() == block['hash'].lower() and receipt['blockNumber'] == block['number'] and receipt['status'] in ('0x0','0x1'), 'Actual canonical financial receipt unavailable')
    require(proof['observedStatus'] == ('included_success' if receipt['status'] == '0x1' else 'included_reverted'), 'Displayed financial result disagrees with canonical receipt')


DOM_PROBE = r"""
const settings=arguments[0], nodes=settings.criticalSelectors.map(selector=>document.querySelector(selector));
if(nodes.some(node=>!node))throw new Error('Actual critical actions missing');
const viewport=window.visualViewport,safeProbe=document.createElement('div');safeProbe.style.cssText='position:fixed;pointer-events:none;visibility:hidden;padding:env(safe-area-inset-top) env(safe-area-inset-right) env(safe-area-inset-bottom) env(safe-area-inset-left)';document.body.appendChild(safeProbe);const safeStyle=getComputedStyle(safeProbe),safe={top:parseFloat(safeStyle.paddingTop),right:parseFloat(safeStyle.paddingRight),bottom:parseFloat(safeStyle.paddingBottom),left:parseFloat(safeStyle.paddingLeft)};safeProbe.remove();if(Object.values(safe).some(value=>!Number.isFinite(value)))throw new Error('Actual safe-area measurements unavailable');
const obscured=nodes.filter(node=>{const r=node.getBoundingClientRect();if(r.width<44||r.height<44||r.left<0||r.top<0||r.right>innerWidth||r.bottom>(viewport?.height??innerHeight))return true;return [[r.left+2,r.top+2],[r.right-2,r.bottom-2],[(r.left+r.right)/2,(r.top+r.bottom)/2]].some(([x,y])=>{const top=document.elementFromPoint(x,y);return !top||!(node===top||node.contains(top));});}).length;
const keyboardHeight=navigator.virtualKeyboard?.boundingRect?.height??null;
const active=document.activeElement, focusStyle=active?getComputedStyle(active):null;
const focusVisible=!!active&&active.matches(':focus-visible')&&!!focusStyle&&(focusStyle.outlineStyle!=='none'||focusStyle.boxShadow!=='none'||active.getAttribute('data-focus-visible')==='true');
const identifiers=settings.identifierSelectors.map(selector=>document.querySelector(selector));
const identifiersCopyable=identifiers.length>0&&identifiers.every(node=>node&&getComputedStyle(node).userSelect!=='none'&&node.textContent.trim().length>0);
const observed={pool:document.querySelector(settings.poolSelector)?.textContent??null,amount:document.querySelector(settings.amountSelector)?.value??null,scroll:scrollY};
const textZoom=parseFloat(getComputedStyle(document.documentElement).fontSize)/settings.baselineRootFontPx;
const metrics=window.__sidioraQualificationMetrics;
if(!metrics||typeof metrics.interactions!=='object'||!Object.keys(metrics.interactions).length)throw new Error('Actual PerformanceObserver interaction samples unavailable');
const sorted=Object.values(metrics.interactions).sort((a,b)=>b-a),inpMs=sorted[Math.floor(sorted.length/50)];
return {cssWidth:innerWidth,cssHeight:innerHeight,devicePixelRatio,zoom:viewport?.scale??1,textZoom,keyboardVisible:keyboardHeight>0||!!viewport&&innerHeight-viewport.height>120,focusVisible,obscuredCriticalActions:obscured,identifiersCopyable,continuityPreserved:observed.pool===settings.before.pool&&observed.amount===settings.before.amount&&Math.abs(observed.scroll-settings.before.scroll)<=1,observed,before:settings.before,cls:metrics.cls,inpMs,interactionCount:sorted.length,interactionDurationsMs:sorted,safeArea:safe,keyboardHeight,userAgent:navigator.userAgent};
"""
PERFORMANCE_OBSERVER = r"""
if(window.__sidioraQualificationMetrics)throw new Error('Do not replace existing actual measurement samples');
window.__sidioraQualificationMetrics={cls:0,interactions:{},sessionValue:0,sessionStarted:null,lastShift:null};
const cls=new PerformanceObserver(list=>{for(const entry of list.getEntries()){if(entry.hadRecentInput)continue;const m=window.__sidioraQualificationMetrics;if(m.lastShift===null||entry.startTime-m.lastShift>1000||entry.startTime-m.sessionStarted>5000){m.sessionStarted=entry.startTime;m.sessionValue=0;}m.lastShift=entry.startTime;m.sessionValue+=entry.value;m.cls=Math.max(m.cls,m.sessionValue);}});cls.observe({type:'layout-shift',buffered:true});
const event=new PerformanceObserver(list=>{for(const entry of list.getEntries())if(entry.interactionId>0)window.__sidioraQualificationMetrics.interactions[entry.interactionId]=Math.max(window.__sidioraQualificationMetrics.interactions[entry.interactionId]??0,entry.duration);});event.observe({type:'event',buffered:true,durationThreshold:0});
window.__sidioraQualificationObservers=[cls,event];return true;
"""


def capture_accessibility(plan, output):
    require(plan['browserPlatform'] in PLATFORMS[2:] and digest(plan['axeScript']) == plan['axeScriptSha256'] and plan['axeVersion'] and plan['toolOwnerApproval'], 'Actual approved pinned axe-core distribution required')
    capture = Capture(output)
    driver = DeviceDriver(plan['deviceProviderURL'],plan['deviceSessionId'],capture)
    current = driver.call('/execute/sync',{'script':'return {origin:location.origin,url:location.href};','args':[]})
    require(current['origin'] == plan['origin'].rstrip('/'), 'Actual accessibility browser is outside approved staging origin')
    script = Path(plan['axeScript']).read_text()
    capture.write('actual-axe-script.js',script)
    driver.call('/execute/sync',{'script':script+';return typeof axe!=="undefined"?axe.version:null;','args':[]})
    result = driver.call('/execute/async',{'script':'const done=arguments[arguments.length-1];if(!window.axe||axe.version!==arguments[0]){done({error:"Pinned actual axe version unavailable"});return;}axe.run(document,{runOnly:{type:"tag",values:["wcag2a","wcag2aa","wcag21aa","wcag22aa"]}}).then(done,error=>done({error:"Actual axe run failed"}));','args':[plan['axeVersion']]})
    require(isinstance(result,dict) and 'violations' in result and 'error' not in result and result['testEngine']['version'] == plan['axeVersion'], 'Actual complete automated accessibility result unavailable')
    artifact = capture.write('actual-axe-observations.json',result)
    return {'status':'captured-unqualified','platform':plan['browserPlatform'],'artifactDigest':plan['artifactDigest'],'axeReport':artifact,'violations':len(result['violations'])}



def prepare_responsive(plan, output):
    require(plan['browserPlatform'] in PLATFORMS[2:], 'Actual supported browser/device session required')
    capture = Capture(output)
    driver = DeviceDriver(plan['deviceProviderURL'], plan['deviceSessionId'], capture)
    driver.call('/execute/sync', {'script': PERFORMANCE_OBSERVER, 'args': []})
    before = driver.call('/execute/sync', {'script': 'const s=arguments[0];return {pool:document.querySelector(s.poolSelector)?.textContent??null,amount:document.querySelector(s.amountSelector)?.value??null,scroll:scrollY,baselineRootFontPx:parseFloat(getComputedStyle(document.documentElement).fontSize),userAgent:navigator.userAgent,cssWidth:innerWidth};', 'args': [plan['probe']]})
    require(before['pool'] and before['amount'] is not None and before['baselineRootFontPx'] > 0, 'Actual selected pool/input before transition required')
    actual = capture.write('before-responsive-transition.json', before)
    return {'status': 'observer-started-unqualified','beforeTrace':actual,'actualBefore':before}



def capture_responsive(plan, output):
    require(plan['browserPlatform'] in PLATFORMS[2:] and plan['deviceProviderURL'] and plan['deviceSessionId'], 'Actual declared browser/device session required')
    require(plan['viewport']['width'] in WIDTHS and plan['viewport']['zoom'] in (1,2) and plan['viewport']['orientation'] in ('portrait','landscape'), 'Declared required responsive viewport needed')
    require(plan['browserSettingsTrace'] and plan['probe']['before'] and plan['probe']['criticalSelectors'] and plan['probe']['identifierSelectors'], 'Actual text-zoom settings and before-transition state required')
    capture = Capture(output)
    driver = DeviceDriver(plan['deviceProviderURL'], plan['deviceSessionId'], capture)
    observation = driver.call('/execute/sync', {'script': DOM_PROBE, 'args': [plan['probe']]})
    require(observation['cssWidth'] == plan['viewport']['width'] and observation['textZoom'] == plan['viewport']['zoom'], 'Actual measured width/text zoom differ from planned case')
    geometry = capture.write('responsive-geometry.json', observation)
    settings = capture.write('actual-browser-settings', Path(plan['browserSettingsTrace']).read_bytes())
    screenshot = capture.write('actual-browser-screen.png', base64.b64decode(driver.call('/screenshot'), validate=True))
    result = {'platform': plan['browserPlatform'], 'width': plan['viewport']['width'], 'zoom': plan['viewport']['zoom'], 'orientation': plan['viewport']['orientation'], 'artifactDigest': plan['artifactDigest'], 'sourceDigest': plan['sourceDigest'], 'backendRevision': plan['backendRevision'], 'geometryTrace': geometry, 'settingsTrace': settings, 'screenshot': screenshot, 'status': 'captured-unqualified'}
    capture.write('responsive-input.json', result)
    return result



def validate_release(root, bundle, candidate):
    require(bundle['schemaVersion'] == 4 and bundle['environment'] == 'staging' and bundle['syntheticAccountsOnly'] is True, 'Observed release bundle v4 required')
    require(candidate['environment'] == 'staging' and candidate['syntheticAccountsOnly'] is True and candidate['signingOwner'], 'Approved staging signing owner required')
    require(Path(candidate['sourceRoot']).resolve() == Path(__file__).resolve().parents[4] and candidate['sourceDigest'] == source_digest(candidate['sourceRoot'], candidate['sourceFiles']), 'Actual current-source release inventory changed')
    artifacts = candidate['releaseArtifacts']
    require(set(artifacts) == set(PLATFORMS) and all(SHA.fullmatch(value) for value in artifacts.values()), 'Exact per-platform release artifact inventory required')
    release_set = hashlib.sha256(json.dumps(artifacts,sort_keys=True,separators=(',',':')).encode()).hexdigest()
    require(bundle['releaseSetDigest'] == release_set and bundle['sourceDigest'] == candidate['sourceDigest'] and bundle['backendRevision'] == candidate['backendRevision'] and bundle['runId'] == candidate['runId'], 'Current platform candidates/backend/run binding mismatch')
    require(bundle['walletSupport'] is True and set(bundle['artifacts']) == set(PLATFORMS), 'Actual supported wallet path and complete artifact set required')
    metrics = {}
    for platform, artifact_digest in artifacts.items():
        entry = bundle['artifacts'][platform]
        artifact = reference(root, entry['artifact'])
        require(digest(artifact) == artifact_digest, 'Platform release candidate differs from source-bound inventory')
        if platform not in ('ios','android'):
            continue
        require(artifact.stat().st_size <= 100*1048576, 'Native per-platform download budget exceeded')
        signer = reference(root, entry['signature'])
        approved = candidate['signingCertificates'][platform]
        require(SHA.fullmatch(approved), 'Approved signing-owner certificate pin required')
        if platform == 'ios':
            require(digest(signer) == approved, 'Actual approved iOS certificate missing')
        else:
            fingerprints = re.findall(r'Signer #\d+ certificate SHA-256 digest: ([a-fA-F0-9]{64})', signer.read_text())
            require(len(fingerprints) == 1 and fingerprints[0].lower() == approved, 'Actual approved Android certificate missing')
        verification = load(reference(root, entry['signatureVerification']))
        require(verification['exitCode'] == 0 and verification['argv'] and verification['stdout'] and verification['stderr'], 'Actual native signing verification command failed or missing')
        reference(root, verification['stdout']); reference(root,verification['stderr'])
        require(entry['measurements'], 'Physical native measurement matrix unavailable')
        metrics[platform] = []
        for item in entry['measurements']:
            observed = load(reference(root,item))
            require(observed['sourceDigest'] == candidate['sourceDigest'] and observed['platform'] == platform, 'Native trace belongs to another source/platform')
            metrics[platform].append(qualify_measurements(root,observed,artifact_digest,candidate['runId']))
    expected = {(platform, journey) for platform in PLATFORMS for journey in JOURNEYS}
    observed = set()
    for row in bundle['journeys']:
        key = (row['platform'], row['journey'])
        require(key in expected and key not in observed and row['passed'] is True and row['expected'] and row['observed'] and row['reviewer'], 'Complete unique actual journey result required')
        require(row['artifactDigest'] == artifacts[row['platform']] and row['backendRevision'] == candidate['backendRevision'], 'Journey from another release candidate')
        for name in ('networkTrace', 'authoritativeState', 'screenshot', 'focusTrace'):
            reference(root, row[name])
        if row['journey'] in ('wallet-owned-execution','portfolio-reconciliation'):
            authoritative_financial(root, row)
        observed.add(key)
    require(observed == expected, 'Complete native/browser journey matrix missing')
    responsive = set()
    for row in bundle['responsive']:
        key = (row['platform'], row['width'], row['zoom'], row['orientation'])
        require(key not in responsive and row['platform'] in PLATFORMS[2:] and row['width'] in WIDTHS and row['zoom'] in (1, 2) and row['orientation'] in ('portrait', 'landscape'), 'Responsive matrix row invalid/duplicated')
        raw = load(reference(root, row['geometryTrace']))
        require(raw['cssWidth'] == row['width'] and raw['textZoom'] == row['zoom'] and raw['keyboardVisible'] is True and raw['focusVisible'] is True and raw['interactionCount'] > 0 and raw['obscuredCriticalActions'] == 0 and raw['continuityPreserved'] is True and raw['identifiersCopyable'] is True, 'Actual responsive/input/focus geometry failed')
        require(finite(raw['cls'], 'CLS') <= .1 and finite(raw['inpMs'], 'INP') <= 200, 'Responsive performance budget exceeded')
        reference(root, row['screenshot'])
        reference(root, row['settingsTrace'])
        responsive.add(key)
    require(responsive == {(platform, width, zoom, direction) for platform in PLATFORMS[2:] for width in WIDTHS for zoom in (1, 2) for direction in ('portrait', 'landscape')}, 'Missing responsive widths/zoom/keyboard/orientation observation')
    for technology in ('NVDA+Chrome', 'VoiceOver+Safari'):
        actual = [row for row in bundle['manualAccessibility'] if row['technology'] == technology]
        require({row['journey'] for row in actual} == set(JOURNEYS), 'Full manual assistive-technology journey recording missing')
        for row in actual:
            require(row['passed'] is True and row['criticalDefects'] == 0 and row['reviewer'], 'Critical observed accessibility defect')
            reference(root, row['recording'])
            reference(root, row['focusTrace'])
    require(bundle['axeReports'], 'Actual automated accessibility observations required')
    for item in bundle['axeReports']:
        require(load(reference(root, item))['violations'] == [], 'Automated WCAG accessibility violation')
    return {'laneMeasurementsPassed': True, 'releaseGate': 'pending-serial-9.1-shared-load-restore-acceptance', 'metrics': metrics}


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('mode', choices=('build-signed','capture-android', 'capture-ios', 'export-ios', 'prepare-responsive', 'capture-responsive','capture-accessibility', 'analyze'))
    parser.add_argument('--plan', required=True, type=Path)
    parser.add_argument('--output', type=Path)
    parser.add_argument('--bundle', type=Path)
    args = parser.parse_args()
    plan = load(args.plan) if args.mode in ('build-signed','analyze') else approved_plan(args.plan)
    if args.mode == 'analyze':
        require(args.bundle is not None, 'Actual captured release bundle required')
        result = validate_release(args.bundle.parent, load(args.bundle), plan)
    else:
        require(args.output is not None, 'Private unique raw capture directory required')
        result = {'build-signed':build_signed,'capture-android':capture_android,'capture-ios':capture_ios,'export-ios':export_ios,'prepare-responsive':prepare_responsive,'capture-responsive':capture_responsive,'capture-accessibility':capture_accessibility}[args.mode](plan,args.output)
    print(json.dumps(result, allow_nan=False))


if __name__ == '__main__':
    main()
