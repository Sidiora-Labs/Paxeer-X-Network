#!/usr/bin/env python3
import hashlib
import json
import os
import re
import copy
from pathlib import Path
import subprocess
import sys

root = Path(__file__).resolve().parents[3]

def artifact(record):
    if not isinstance(record, dict) or set(record) != {"path", "sha256"} or not isinstance(record["path"], str) or not record["path"] or not isinstance(record["sha256"], str) or not re.fullmatch(r"[0-9a-f]{64}", record["sha256"]):
        raise ValueError("invalid evidence artifact")
    path = Path(record["path"])
    if not path.is_absolute():
        path = root / path
    if not path.is_file() or not 0 < path.stat().st_size <= 64*1024*1024:
        raise ValueError("missing nonempty evidence artifact: " + str(path))
    digest = hashlib.sha256(path.read_bytes()).hexdigest()
    if digest != record["sha256"]:
        raise ValueError("evidence digest mismatch: " + str(path))
    return path

def unique_object(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise ValueError("duplicate manifest field: " + key)
        result[key] = value
    return result

def read_json(path):
    if not 0 < path.stat().st_size <= 16*1024*1024:
        raise ValueError("invalid bounded JSON evidence")
    return json.loads(path.read_text(), object_pairs_hook=unique_object)

def validate_manifest(manifest):
    expected = {"implementation", "version", "scope", "domains", "licenseEvidence", "assets", "browserEvidence"}
    if not isinstance(manifest,dict) or set(manifest) != expected or manifest["implementation"] not in ("licensed_vendor", "owned_accessible_fallback"):
        raise ValueError("invalid chart implementation manifest")
    if not isinstance(manifest["version"], str) or not 1 <= len(manifest["version"]) <= 160 or not isinstance(manifest["scope"], str) or not 1 <= len(manifest["scope"]) <= 1000:
        raise ValueError("missing version or licensed scope")
    domains = manifest["domains"]
    if not isinstance(domains,list) or not domains or any(not isinstance(v,str) or not re.fullmatch(r"[a-zA-Z0-9](?:[a-zA-Z0-9.-]{0,251}[a-zA-Z0-9])?(?::[0-9]{1,5})?",v) for v in domains) or len(set(domains)) != len(domains):
        raise ValueError("explicit deployment domains required")
    artifact(manifest["licenseEvidence"])
    if not isinstance(manifest["assets"],list) or not manifest["assets"]:
        raise ValueError("deployable chart assets required")
    seen_assets = set()
    for record in manifest["assets"]:
        asset = artifact(record).resolve()
        asset.relative_to(root.resolve())
        if asset in seen_assets:
            raise ValueError("duplicate deployable chart asset")
        seen_assets.add(asset)
    browser_path = artifact(manifest["browserEvidence"])
    browser = read_json(browser_path)
    if not isinstance(browser,dict):
        raise ValueError("invalid browser evidence")
    if browser.get("revision") != os.environ.get("L11_BROWSER_SOURCE_REVISION") or not browser.get("revision"):
        raise ValueError("browser evidence revision authority mismatch")
    required = {"missing_script_404", "csp_rejection", "accessible_retry", "usable_trading_forms", "normal_chart"}
    journeys = browser.get("journeys",{})
    if not isinstance(journeys,dict) or set(journeys) != required:
        raise ValueError("required chart browser journeys absent")
    for name, record in journeys.items():
        if not isinstance(record,dict) or type(record.get("exit_code")) is not int or record["exit_code"] != 0 or not isinstance(record.get("command"),str) or not record["command"].strip():
            raise ValueError("failed or unexecuted browser journey: " + name)
        artifact(record["artifact"])

def exercise_missing_assets(manifest):
    absent = copy.deepcopy(manifest)
    absent["assets"][0] = {"path": str(root / "tools/production/charts-history/__absent_library_asset__"), "sha256": "0"*64}
    try:
        validate_manifest(absent)
    except ValueError as error:
        if "missing nonempty evidence artifact" not in str(error):
            raise
    else:
        raise ValueError("missing chart script was admitted")
    empty = copy.deepcopy(manifest)
    empty["assets"] = []
    try:
        validate_manifest(empty)
    except ValueError as error:
        if "deployable chart assets required" not in str(error):
            raise
    else:
        raise ValueError("manifest without chart assets was admitted")

def main():
    manifest_path = os.environ.get("L11_CHART_LICENSE_MANIFEST")
    if not manifest_path:
        raise ValueError("external chart license/deployable asset/browser evidence manifest required")
    manifest = read_json(Path(manifest_path))
    validate_manifest(manifest)
    exercise_missing_assets(manifest)
    env = dict(os.environ, GOWORK="off", GOMAXPROCS="2", GOFLAGS="-mod=readonly")
    return subprocess.run(["go","test","-p","1","-count=1","-json","./internal/httpapi","-run","^TestProduction|^TestHistory(Cancelled5000|SQLTimeout|LegacyRegistration)|^FuzzHistoryValidatedInputs$"],cwd=root/"core/trading-charts",env=env,timeout=240).returncode

if __name__ == "__main__":
    try:
        sys.exit(main())
    except subprocess.TimeoutExpired:
        sys.exit(124)
    except (OSError,ValueError,KeyError,TypeError) as error:
        print(str(error),file=sys.stderr)
        sys.exit(1)
