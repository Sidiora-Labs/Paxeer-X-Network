#!/usr/bin/env python3
"""Linux structural/source compatibility check. Never establishes native compilation."""
import argparse
import hashlib
import json
from pathlib import Path
import plistlib
import re
import xml.etree.ElementTree as ET


def parse_openstep(text):
    pattern = re.compile(r'\s+|/\*.*?\*/|//[^\n]*|"(?:\\.|[^"\\])*"|[{}()=;,]|[^\s{}()=;,]+', re.S)
    tokens = []
    end = 0
    for match in pattern.finditer(text):
        if text[end:match.start()].strip():
            raise ValueError('Unrecognized OpenStep input')
        end = match.end()
        token = match.group()
        if token.isspace() or token.startswith(('/*', '//')):
            continue
        tokens.append(token)
    if text[end:].strip():
        raise ValueError('Trailing invalid input')
    index = 0

    def take(expected=None):
        nonlocal index
        if index >= len(tokens):
            raise ValueError('Unexpected EOF')
        token = tokens[index]
        index += 1
        if expected is not None and token != expected:
            raise ValueError(f'Expected {expected}, got {token}')
        return token

    def value():
        token = take()
        if token == '{':
            result = {}
            while tokens[index] != '}':
                key = atom(take())
                if key in result:
                    raise ValueError('Duplicate dictionary key')
                take('=')
                result[key] = value()
                take(';')
            take('}')
            return result
        if token == '(':
            result = []
            while tokens[index] != ')':
                result.append(value())
                if tokens[index] != ')':
                    take(',')
            take(')')
            return result
        return atom(token)

    def atom(token):
        if token in '{}()=;,':
            raise ValueError('Invalid atom')
        return json.loads(token) if token.startswith('"') else token

    result = value()
    if index != len(tokens):
        raise ValueError('Trailing tokens')
    return result


def check_project(mobile, sdk, plugin):
    ios = mobile / 'ios'
    project = parse_openstep((ios / 'Sidiora.xcodeproj/project.pbxproj').read_text())
    objects = project['objects']
    assert objects[project['rootObject']]['isa'] == 'PBXProject'
    for key in objects:
        assert re.fullmatch('[A-F0-9]{24}', key), key
    # Every graph edge must resolve, including all target and build configuration references.
    def refs(value):
        if isinstance(value, dict):
            for item in value.values():
                refs(item)
        elif isinstance(value, list):
            for item in value:
                refs(item)
        elif re.fullmatch('[A-F0-9]{24}', value):
            assert value in objects, f'Dangling project reference: {value}'
    refs(project)
    targets = [o for o in objects.values() if o.get('isa') == 'PBXNativeTarget']
    assert len(targets) == 1 and targets[0]['name'] == 'Sidiora'
    phases = [objects[key] for key in targets[0]['buildPhases']]
    assert {'PBXSourcesBuildPhase', 'PBXResourcesBuildPhase', 'PBXFrameworksBuildPhase'}.issubset({p['isa'] for p in phases})
    files = [objects[o['fileRef']]['path'] for phase in phases for key in phase.get('files', []) for o in [objects[key]]]
    assert 'Sidiora/AppDelegate.swift' in files
    assert 'Sidiora/PrivacyInfo.xcprivacy' in files
    assert 'Sidiora/LaunchScreen.storyboard' in files
    for obj in objects.values():
        if obj.get('isa') == 'PBXFileReference' and obj.get('path', '').startswith('Sidiora/'):
            assert (ios / obj['path']).exists(), obj['path']
    for obj in objects.values():
        if obj.get('isa') == 'XCBuildConfiguration':
            settings = obj['buildSettings']
            assert settings['IPHONEOS_DEPLOYMENT_TARGET'] == '17.0'
            if 'PRODUCT_BUNDLE_IDENTIFIER' in settings:
                assert settings['PRODUCT_BUNDLE_IDENTIFIER'] == 'ag.sidiora.mobile'
                assert settings['MARKETING_VERSION'] == '0.1.0'
    scheme = ET.parse(ios / 'Sidiora.xcodeproj/xcshareddata/xcschemes/Sidiora.xcscheme')
    for ref in scheme.iter('BuildableReference'):
        assert ref.attrib['BlueprintIdentifier'] in objects
        assert ref.attrib['ReferencedContainer'] == 'container:Sidiora.xcodeproj'
    info = plistlib.loads((ios / 'Sidiora/Info.plist').read_bytes())
    assert info['NSAppTransportSecurity']['NSAllowsArbitraryLoads'] is False
    plistlib.loads((ios / 'Sidiora/PrivacyInfo.xcprivacy').read_bytes())
    ET.parse(ios / 'Sidiora/LaunchScreen.storyboard')
    manifest = ET.parse(mobile / 'android/app/src/main/AndroidManifest.xml')
    android = '{http://schemas.android.com/apk/res/android}'
    app = manifest.getroot().find('application')
    assert app.attrib[android + 'allowBackup'] == 'false'
    assert app.attrib[android + 'usesCleartextTraffic'] == 'false'
    app_gradle = (mobile / 'android/app/build.gradle').read_text()
    assert "minSdk 30" in app_gradle and "applicationId 'ag.sidiora.mobile'" in app_gradle
    assert "../../node_modules/react-native" in app_gradle
    assert (mobile / 'android/gradlew').stat().st_mode & 0o111
    properties = dict(line.split('=', 1) for line in (mobile / 'android/gradle/wrapper/gradle-wrapper.properties').read_text().splitlines() if '=' in line)
    provenance = json.loads((mobile.parents[2] / 'launchpad/evidence/production-system/L32/phase1/native-source-provenance.json').read_text())
    assert properties['distributionSha256Sum'] == provenance['gradle']['distributionSHA256']
    assert hashlib.sha256((mobile / 'android/gradle/wrapper/gradle-wrapper.jar').read_bytes()).hexdigest() == provenance['gradle']['wrapperSHA256']
    package = json.loads((sdk / 'package.json').read_text())
    ours = json.loads((mobile / 'package.json').read_text())
    assert package['version'] == ours['dependencies']['react-native'] == '0.86.0'
    helpers = (sdk / 'scripts/cocoapods/helpers.rb').read_text()
    min_ios = re.search(r'def self.min_ios_version_supported\s+return \'([^\']+)', helpers)[1]
    min_xcode = re.search(r'def self.min_xcode_version_supported\s+return \'([^\']+)', helpers)[1]
    assert tuple(map(int, min_ios.split('.'))) <= (17, 0)
    sdk_versions = (sdk / 'gradle/libs.versions.toml').read_text()
    plugin_versions = (plugin / 'gradle/libs.versions.toml').read_text()
    versions = {key: re.search(r'^' + key + r' = "([^"]+)"', sdk_versions, re.M)[1] for key in ['minSdk','compileSdk','buildTools','ndkVersion','agp','kotlin']}
    assert int(versions['minSdk']) <= 30
    root_gradle = (mobile / 'android/build.gradle').read_text()
    for key in ['compileSdk','buildTools','ndkVersion','agp','kotlin']:
        assert versions[key] in root_gradle, key
    for key in ['agp','kotlin']:
        assert f'{key} = "{versions[key]}"' in plugin_versions
    assert 'gradle-9.3.1-bin.zip' in properties['distributionUrl']
    sources = ['package.json','scripts/cocoapods/helpers.rb','gradle/libs.versions.toml']
    return {'kind':'native-project-structure-and-source-compatibility', 'nativeCompile':False, 'projectObjects':len(objects), 'sdkVersion':package['version'], 'frameworkMinimumIOS':min_ios, 'appMinimumIOS':'17.0', 'minimumXcode':min_xcode, 'androidVersions':versions, 'appMinimumAndroidSDK':30, 'sourceHashes':{p:hashlib.sha256((sdk/p).read_bytes()).hexdigest() for p in sources}, 'gradlePluginSourceHash':hashlib.sha256((plugin/'gradle/libs.versions.toml').read_bytes()).hexdigest()}

if __name__ == '__main__':
    parser = argparse.ArgumentParser()
    parser.add_argument('--sdk-root', type=Path, required=True)
    parser.add_argument('--plugin-root', type=Path, required=True)
    args = parser.parse_args()
    mobile = Path(__file__).resolve().parents[2] / 'mobile'
    print(json.dumps(check_project(mobile, args.sdk_root, args.plugin_root), indent=2))
