#!/usr/bin/env python3
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import tempfile

root = Path(__file__).resolve().parents[3]
route_file = root / "tools/paxeer-x/route-catalogue.json"
routes = json.loads(route_file.read_text())["routes"]
for tail in ("accounts", "assets"):
    route = [r for r in routes if r["path"] == "/v2/migration/" + tail]
    assert len(route) == 1
    route = route[0]
    assert route["service"] == "interop" and route["upstream"] == "interop"
    assert route["method"] == "POST" and route["proxy"] is True
    assert route["authentication"] == "gateway-api-key" and route["retries"] == 0
    assert route["source"] == "interop/crates/layerx-interop-service/src/server.rs"
spec = importlib.util.spec_from_file_location("migration_renderer", root / "interop/deploy/gateway/render.py")
renderer = importlib.util.module_from_spec(spec)
spec.loader.exec_module(renderer)
assert renderer.migration_profile({}) is None
with tempfile.TemporaryDirectory(prefix="migration-render-boundary-") as directory:
    profile = Path(directory) / "profile.json"
    profile.write_text("{}")
    profile.chmod(0o600)
    for path in (str(profile), "", str(profile.parent)):
        try:
            renderer.migration_profile({renderer.MIGRATION_VARIABLE: path})
        except renderer.Refused:
            pass
        else:
            raise AssertionError("invalid migration profile accepted")
    profile.write_text('{"unexpected":true}')
    try:
        renderer.migration_profile({renderer.MIGRATION_VARIABLE: str(profile)})
    except renderer.Refused:
        pass
    else:
        raise AssertionError("unknown migration profile field accepted")
target = Path(os.environ.get("CARGO_TARGET_DIR", "/root/lx-target/migration-gateway"))
binaries = [p for p in (target / "debug/deps").glob("migration_transport-*") if p.is_file() and os.access(p, os.X_OK)]
assert len(binaries) == 1, "one declared migration transport build artifact required"
result = subprocess.run([str(binaries[0]), "--nocapture"], cwd=root, check=True, capture_output=True, text=True)
print(result.stdout, end="")
assert "3 passed; 0 failed; 0 ignored" in result.stdout
print("PAXEER_X_GATE tests=10 skipped=0")
