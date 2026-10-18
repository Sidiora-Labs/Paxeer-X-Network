#!/usr/bin/env python3
"""Fail closed on accidental public or unpinned bootstrap infrastructure."""
import json
from pathlib import Path
import re
import subprocess

HERE = Path(__file__).resolve().parent
EXPECTED = {"postgres", "redis", "nats", "minio", "minio-init", "chain"}


def validate(config):
    errors = []
    services = config.get("services", {})
    if set(services) != EXPECTED:
        errors.append("Fixture service set does not match the reviewed six-service inventory")
    if config.get("name") != "kindle-bootstrap-local":
        errors.append("Fixture project name changed without isolation review")
    for name, service in services.items():
        if not re.fullmatch(r"[^@]+@sha256:[0-9a-f]{64}", service.get("image", "")):
            errors.append(f"{name}: image must be pinned by sha256 digest")
        if service.get("privileged") or service.get("network_mode") == "host":
            errors.append(f"{name}: privileged or host networking is forbidden")
        if set(service.get("networks", {})) != {"fixture"}:
            errors.append(f"{name}: unexpected network membership")
        for port in service.get("ports", []):
            if port.get("host_ip") != "127.0.0.1":
                errors.append(f"{name}: published port is not IPv4 loopback-only")
        for volume in service.get("volumes", []):
            if volume.get("type") == "bind":
                if Path(volume.get("source", "")).resolve() != HERE / "nats.conf":
                    errors.append(f"{name}: unexpected host bind mount")
                if not volume.get("read_only"):
                    errors.append(f"{name}: fixture configuration mount must be read-only")
        if name != "minio-init" and (not service.get("healthcheck", {}).get("test") or service.get("healthcheck", {}).get("disable")):
            errors.append(f"{name}: application health check is required")
    network = config.get("networks", {}).get("fixture", {})
    if network.get("driver") != "bridge" or network.get("internal"):
        errors.append("Fixture requires a private bridge with working loopback publication")
    if str(network.get("driver_opts", {}).get("com.docker.network.bridge.enable_ip_masquerade")).lower() != "false":
        errors.append("Fixture bridge must disable outbound IP masquerading")
    return errors


def main():
    result = subprocess.run(["docker", "compose", "-f", str(HERE / "compose.yml"), "--profile", "bootstrap-init", "config", "--format", "json"], capture_output=True, text=True, check=True)
    config = json.loads(result.stdout)
    errors = validate(config)
    if errors:
        raise SystemExit("\n".join(errors))
    # Exercise rejection behavior without starting any containers.
    mutated = json.loads(json.dumps(config))
    mutated["services"]["postgres"]["ports"][0]["host_ip"] = "0.0.0.0"
    assert validate(mutated), "Public port mutation was accepted"
    mutated = json.loads(json.dumps(config))
    mutated["services"]["chain"]["image"] = "trufflesuite/ganache:latest"
    assert validate(mutated), "Unpinned image mutation was accepted"
    mutated = json.loads(json.dumps(config))
    mutated["networks"]["fixture"]["driver"] = "host"
    assert validate(mutated), "Host network mutation was accepted"
    print(json.dumps({"status": "pass", "services": sorted(config["services"]), "negative_scenarios": ["public_port", "mutable_image", "host_network"]}))


if __name__ == "__main__":
    main()
