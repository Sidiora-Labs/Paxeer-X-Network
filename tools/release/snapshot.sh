#!/usr/bin/env bash
set -euo pipefail

exec python3 - "$@" <<'PY'
import argparse
import json
import os
from pathlib import Path
import re
import shlex
import subprocess
import sys
import tarfile
import tempfile


class Refusal(Exception):
    pass


def require(condition, message):
    if not condition:
        raise Refusal(message)


def load_env(path):
    values = {}
    wanted = {"VALIDATOR_HOMES", "FULLNODE_HOMES", "SNAPSHOT_DIR", "HALT_HEIGHT"}
    for line in path.read_text().splitlines():
        line = line.strip()
        if not line or line.startswith("#"):
            continue
        if line.startswith("export "):
            line = line[7:].lstrip()
        key, separator, raw = line.partition("=")
        key = key.strip()
        require(
            bool(separator) and re.fullmatch(r"[A-Z_][A-Z0-9_]*", key),
            "malformed env assignment",
        )
        if key not in wanted:
            continue
        require(key not in values, f"duplicate env key {key}")
        words = shlex.split(raw, comments=True)
        require(len(words) <= 1, f"quote the value of {key}")
        values[key] = words[0] if words else ""
    require(wanted <= values.keys(), "env file must declare all snapshot keys")
    require(values["SNAPSHOT_DIR"].strip(), "empty SNAPSHOT_DIR")
    require(
        re.fullmatch(r"[1-9][0-9]*", values["HALT_HEIGHT"]),
        "HALT_HEIGHT must be a positive decimal integer",
    )
    validators = values["VALIDATOR_HOMES"].split()
    fullnodes = values["FULLNODE_HOMES"].split()
    require(validators or fullnodes, "no validator or full-node homes")
    homes = []
    names = set()
    for value in validators + fullnodes:
        home = Path(value)
        require(
            home.is_absolute() and home.name not in ("", ".", ".."),
            "homes must be absolute paths",
        )
        require(home.name not in names, "duplicate home archive name")
        require(
            not any(home.resolve() == previous.resolve() for previous in homes),
            "duplicate home",
        )
        names.add(home.name)
        homes.append(home)
    directory = Path(values["SNAPSHOT_DIR"])
    require(directory.is_absolute(), "SNAPSHOT_DIR must be absolute")
    for home in homes:
        require(
            not directory.resolve().is_relative_to((home / "data").resolve()),
            "SNAPSHOT_DIR must be outside every data directory",
        )
    return homes, len(validators), directory, values["HALT_HEIGHT"]


def stopped(homes):
    result = subprocess.run(["pgrep", "-x", "paxd"], capture_output=True, text=True)
    require(result.returncode in (0, 1), "cannot inspect paxd processes")
    targets = {home.resolve() for home in homes}
    for pid in result.stdout.split():
        proc = Path("/proc") / pid
        try:
            args = (proc / "cmdline").read_bytes().split(b"\0")
            args = [os.fsdecode(arg) for arg in args if arg]
            if not args:
                continue
            selected = None
            for i, arg in enumerate(args):
                if arg == "--home" and i + 1 < len(args):
                    selected = args[i + 1]
                elif arg.startswith("--home="):
                    selected = arg.partition("=")[2]
            if selected is None:
                environment = dict(
                    item.split(b"=", 1)
                    for item in (proc / "environ").read_bytes().split(b"\0")
                    if b"=" in item
                )
                selected = os.fsdecode(environment.get(b"PAX_HOME", b""))
            require(
                selected, "running paxd has no explicit home; stop it before snapshot"
            )
            path = Path(selected)
            if not path.is_absolute():
                path = proc / "cwd" / path
            require(
                path.resolve() not in targets, "node still running for a snapshot home"
            )
        except FileNotFoundError:
            continue


def signing_state(raw, height, validator):
    state = json.loads(raw)
    require(
        isinstance(state, dict)
        and isinstance(state.get("height"), str)
        and re.fullmatch(r"0|[1-9][0-9]*", state["height"]),
        "invalid priv_validator_state.json",
    )
    if validator:
        require(
            state["height"] == height or halt_proposal(state, height),
            "validator signing state is not at HALT_HEIGHT",
        )


def halt_proposal(state, height):
    if state["height"] != str(int(height) + 1):
        return False
    if not all(
        type(state.get(key)) is int and state[key] == value
        for key, value in (("round", 0), ("step", 1))
    ):
        return False
    signature = state.get("signature")
    signbytes = state.get("signbytes")
    if not isinstance(signature, str) or not signature:
        return False
    if not isinstance(signbytes, str) or not re.fullmatch(
        r"(?:[0-9A-Fa-f]{2})+", signbytes
    ):
        return False
    raw = bytes.fromhex(signbytes)
    length = 0
    for i, byte in enumerate(raw[:10]):
        length |= (byte & 0x7F) << (7 * i)
        if byte < 0x80:
            body = raw[i + 1 :]
            return len(body) == length and body[:2] == b"\x08\x20"
    return False


def verify(archive, height, validator):
    require(archive.is_file() and not archive.is_symlink(), "missing snapshot archive")
    print(f"snapshot: verifying {archive.name}", flush=True)
    subprocess.run(["tar", "--zstd", "-tf", str(archive)], check=True)
    state_found = False
    store_found = False
    with subprocess.Popen(
        ["zstd", "-dc", "--", str(archive)], stdout=subprocess.PIPE
    ) as decoder:
        try:
            with tarfile.open(fileobj=decoder.stdout, mode="r|") as stream:
                for member in stream:
                    path = Path(member.name)
                    require(
                        not path.is_absolute()
                        and ".." not in path.parts
                        and path.parts
                        and path.parts[0] == "data",
                        "archive member outside data",
                    )
                    require(
                        member.isdir()
                        or member.isfile()
                        or member.issym()
                        or member.islnk(),
                        "archive contains a special file",
                    )
                    if member.issym() or member.islnk():
                        link = Path(member.linkname)
                        target = path.parent / link if member.issym() else link
                        require(
                            not link.is_absolute()
                            and ".." not in link.parts
                            and target.parts
                            and target.parts[0] == "data",
                            "archive link leaves data",
                        )
                    if member.name == "data/priv_validator_state.json":
                        require(
                            member.isfile() and 0 < member.size <= 65536,
                            "invalid priv_validator_state.json",
                        )
                        with stream.extractfile(member) as source:
                            signing_state(source.read(), height, validator)
                        state_found = True
                    if (
                        member.isfile()
                        and member.size > 0
                        and (
                            member.name == "data/application.db"
                            or member.name.startswith("data/application.db/")
                        )
                    ):
                        store_found = True
            while decoder.stdout.read(1024 * 1024):
                pass
            decoder.stdout.close()
            require(decoder.wait() == 0, "cannot decompress snapshot")
        except BaseException:
            decoder.stdout.close()
            decoder.terminate()
            decoder.wait()
            raise
    require(state_found, "archive lacks data/priv_validator_state.json")
    require(store_found, "archive lacks application store")


def receipt(path, count, result):
    path.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    with tempfile.NamedTemporaryFile(mode="w", dir=path.parent, delete=False) as output:
        temporary = Path(output.name)
        try:
            json.dump({"homes_archived": count, "result": result}, output)
            output.write("\n")
            output.flush()
            os.fsync(output.fileno())
            os.replace(temporary, path)
        finally:
            temporary.unlink(missing_ok=True)


def main():
    parser = argparse.ArgumentParser(prog="snapshot.sh")
    parser.add_argument("--env-file", type=Path, required=True)
    parser.add_argument("--verify", action="store_true")
    parser.add_argument("--receipt", type=Path)
    args = parser.parse_args()
    os.umask(0o077)
    os.environ.pop("TAR_OPTIONS", None)
    count = 0
    result = "fail"
    try:
        homes, validator_count, directory, height = load_env(args.env_file)
        archives = [directory / f"{home.name}-pre-v6.12.0.tar.zst" for home in homes]
        if args.receipt is not None:
            require(
                args.receipt.resolve() != args.env_file.resolve(),
                "receipt would replace env file",
            )
            require(
                args.receipt.resolve() not in {a.resolve() for a in archives},
                "receipt would replace snapshot",
            )
            for home in homes:
                require(
                    not args.receipt.resolve().is_relative_to(home.resolve()),
                    "receipt must be outside node homes",
                )
        if not args.verify:
            stopped(homes)
            for i, home in enumerate(homes):
                data = home / "data"
                require(
                    data.is_dir() and not data.is_symlink(),
                    "missing real data directory",
                )
                for entry in data.rglob("*"):
                    require(
                        entry.is_file() or entry.is_dir(),
                        "data contains a special or dangling file",
                    )
                    if entry.is_symlink():
                        link = Path(os.readlink(entry))
                        require(
                            not link.is_absolute()
                            and ".." not in link.parts
                            and entry.resolve().is_relative_to(data.resolve()),
                            "data link leaves data",
                        )
                signing_state(
                    (data / "priv_validator_state.json").read_bytes(),
                    height,
                    i < validator_count,
                )
                require((data / "application.db").exists(), "missing application store")
            require(
                not any(os.path.lexists(a) for a in archives),
                "snapshot already exists; use --verify",
            )
            directory.mkdir(parents=True, exist_ok=True, mode=0o700)
        for i, (home, archive) in enumerate(zip(homes, archives)):
            if args.verify:
                verify(archive, height, i < validator_count)
            else:
                stopped(homes)
                with tempfile.TemporaryDirectory(
                    prefix=".snapshot-", dir=directory
                ) as work:
                    temporary = Path(work) / archive.name
                    subprocess.run(
                        [
                            "tar",
                            "--zstd",
                            "-cf",
                            str(temporary),
                            "-C",
                            str(home),
                            "data",
                        ],
                        check=True,
                    )
                    stopped(homes)
                    verify(temporary, height, i < validator_count)
                    os.link(temporary, archive)
            count += 1
        result = "pass"
    except (
        Refusal,
        OSError,
        ValueError,
        tarfile.TarError,
        subprocess.SubprocessError,
    ) as error:
        print(f"snapshot: {error}", file=sys.stderr)
    if args.receipt is not None:
        try:
            require(
                args.receipt.resolve() != args.env_file.resolve(),
                "receipt would replace env file",
            )
            if "archives" in locals():
                require(
                    args.receipt.resolve() not in {a.resolve() for a in archives},
                    "receipt would replace snapshot",
                )
                require(
                    not any(
                        args.receipt.resolve().is_relative_to(h.resolve())
                        for h in homes
                    ),
                    "receipt must be outside node homes",
                )
            receipt(args.receipt, count, result)
        except (Refusal, OSError) as error:
            print(f"snapshot: cannot write receipt: {error}", file=sys.stderr)
            return 1
    return 0 if result == "pass" else 1


sys.exit(main())
PY
