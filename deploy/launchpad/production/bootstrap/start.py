#!/usr/bin/env python3
"""Start only the owned local fixture; distinguish healthy daemons from init jobs."""
from pathlib import Path
import subprocess
import sys

HERE = Path(__file__).resolve().parent
PERSISTENT = ["postgres", "redis", "nats", "minio", "chain"]


def main():
    subprocess.run([sys.executable, str(HERE / "validate.py")], check=True)
    compose = ["docker", "compose", "-f", str(HERE / "compose.yml")]
    subprocess.run(compose + ["up", "-d", "--wait", "--wait-timeout", "120", *PERSISTENT], check=True)
    subprocess.run(compose + ["--profile", "bootstrap-init", "run", "--rm", "--no-deps", "minio-init"], check=True)
    print("Private fixture daemons healthy; object bucket initialization succeeded.", flush=True)


if __name__ == "__main__":
    try:
        main()
    except subprocess.CalledProcessError as error:
        # Propagate the actual failed phase; never turn a failed init into success.
        raise SystemExit(error.returncode if error.returncode > 0 else 1)
