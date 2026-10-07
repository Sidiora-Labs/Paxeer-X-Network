#!/usr/bin/env python3
import os
import re
import sys
import tomllib
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
SKIP = {".git", "node_modules", "target", "dist", "build", "vendor"}


def configs():
    for d, dirs, names in os.walk(ROOT):
        dirs[:] = sorted(x for x in dirs if x not in SKIP)
        for n in sorted(names):
            if n.endswith(".toml"):
                p = Path(d) / n
                if "secret_name" in p.read_text(errors="replace"):
                    yield p


def main():
    tables = {}
    baked = set()
    for path in configs():
        rel = path.relative_to(ROOT)
        cfg = tomllib.loads(path.read_text())
        rows = cfg.get("files", [])
        if not rows:
            continue
        app = cfg["app"]
        build = cfg.get("build", {})
        m = re.search(r"(?:^|/)docker/([^/]+)/Dockerfile$", build.get("dockerfile", ""))
        if not m:
            print(f"unmapped: {rel} app={app} build={build}", file=sys.stderr)
            continue
        image = m.group(1)
        role = app.removeprefix("paxeer-")
        out = []
        for r in rows:
            guest = r["guest_path"]
            if "local_path" in r:
                line = f"bake: {image} {(path.parent / r['local_path']).relative_to(ROOT)} -> {guest}"
                if line not in baked:
                    baked.add(line)
                    print(line, file=sys.stderr)
                continue
            mode = f"0{r['mode']:o}" if "mode" in r else ""
            roles = ",".join(r["processes"]) if "processes" in r else "*"
            out.append((r["secret_name"], guest, mode, roles))
        tables.setdefault(image, []).append((rel, role, out))

    for image, apps in sorted(tables.items()):
        rows = []
        shared = set.intersection(*(set(o) for _, _, o in apps))
        for rel, role, out in apps:
            for row in out:
                if row in shared:
                    if row not in rows:
                        rows.append(row)
                    continue
                if row[3] != "*":
                    sys.exit(f"conflict: {rel} row {row[0]} differs per app and is already role-scoped")
                per_app = (row[0], row[1], row[2], role)
                print(f"per-app: {image} {row[0]} {row[1]} roles={role} ({rel})", file=sys.stderr)
                rows.append(per_app)
        dest = ROOT / "docker" / image / "files.tsv"
        dest.parent.mkdir(parents=True, exist_ok=True)
        dest.write_text("".join("\t".join(r) + "\n" for r in rows))
        print(f"wrote: {dest.relative_to(ROOT)} rows={len(rows)} from {', '.join(str(a[0]) for a in apps)}", file=sys.stderr)


if __name__ == "__main__":
    main()
