#!/usr/bin/env python3
import argparse
import contextlib
import io
import json
import os
from pathlib import Path, PurePosixPath
import re
import subprocess
import sys
import tempfile

sys.dont_write_bytecode = True
ROOT = Path(__file__).resolve().parents[3]
CATALOGUE = ROOT / "tools/paxeer-x/retired_callers.json"
KEPT_CODES = {"LXP_MODULE_DISABLED", "LXP_ERR_MODULE_DISABLED"}
COUNT = 0


class Invalid(ValueError):
    pass


def require(value, message):
    if not value:
        raise Invalid(message)


def unique_object(pairs):
    result = {}
    for key, value in pairs:
        require(key not in result, "duplicate catalogue key: " + key)
        result[key] = value
    return result


def relative_path(value):
    require(
        isinstance(value, str) and value and "\x00" not in value,
        "path must be a nonempty string",
    )
    path = PurePosixPath(value)
    require(
        not path.is_absolute()
        and ".." not in path.parts
        and ".git" not in path.parts
        and str(path) != ".",
        "path must name a repository-relative file or tree",
    )
    return str(path)


def string_list(value, name, empty=False):
    require(isinstance(value, list) and (empty or value), name + " must be a list")
    require(
        all(
            isinstance(item, str)
            and item.strip()
            and "\n" not in item
            and "\r" not in item
            and "\x00" not in item
            for item in value
        ),
        name + " contains an invalid string",
    )
    require(len(value) == len(set(value)), name + " contains duplicates")


def validate_catalogue(document):
    require(isinstance(document, dict), "catalogue must be an object")
    require(
        set(document) <= {"components", "allowed_references", "schema"},
        "unknown catalogue field",
    )
    components = document.get("components")
    require(isinstance(components, dict) and components, "components are missing")
    for name, entry in components.items():
        require(
            isinstance(name, str) and re.fullmatch(r"[a-z][a-z0-9-]*", name),
            "invalid component ID",
        )
        require(
            isinstance(entry, dict)
            and {"symbols", "deleted_trees", "replacement"} <= set(entry),
            "incomplete component: " + name,
        )
        require(
            set(entry) <= {"symbols", "deleted_trees", "replacement", "req", "title"},
            "unknown component field: " + name,
        )
        string_list(entry["symbols"], name + " symbols")
        string_list(entry["deleted_trees"], name + " deleted trees", empty=True)
        for path in entry["deleted_trees"]:
            require(relative_path(path) == path, "noncanonical deleted tree: " + path)
        require(
            isinstance(entry["replacement"], str) and entry["replacement"].strip(),
            "replacement is missing: " + name,
        )
    allowances = document.get("allowed_references", [])
    require(isinstance(allowances, list), "allowed references must be a list")
    for entry in allowances:
        require(
            isinstance(entry, dict)
            and "path" in entry
            and set(entry) <= {"path", "symbols", "lines"}
            and ("symbols" in entry or "lines" in entry),
            "invalid allowed reference",
        )
        require(
            relative_path(entry["path"]) == entry["path"], "noncanonical allowed path"
        )
        for key in ("symbols", "lines"):
            if key in entry:
                string_list(entry[key], "allowed " + key)
    return document


def load_catalogue(path):
    return validate_catalogue(
        json.loads(path.read_text(), object_pairs_hook=unique_object)
    )


def below(path, tree):
    return path == tree or path.startswith(tree + "/")


def is_test_file(path):
    parts = PurePosixPath(path).parts
    if any(part in {"test", "tests", "testdata", "__tests__"} for part in parts[:-1]):
        return True
    name = parts[-1]
    return (
        name == "conftest.py"
        or PurePosixPath(name).stem in {"test", "tests"}
        or name.startswith("test_")
        or re.search(r"(?:_tests?|[.]test|[.]spec)[.][^.]+$", name) is not None
    )


def symbol_pattern(symbol):
    left = r"(?<![A-Za-z0-9_])" if symbol[0].isalnum() or symbol[0] == "_" else ""
    right = ""
    if symbol[-1].isalnum():
        right = r"(?![A-Za-z0-9])"
    return re.compile(left + re.escape(symbol) + right)


def allowed(path, text, symbol, allowances):
    if symbol in KEPT_CODES:
        return True
    for entry in allowances:
        if path == entry["path"] and (
            symbol in entry.get("symbols", [])
            or " ".join(text.split())
            in {" ".join(line.split()) for line in entry.get("lines", [])}
        ):
            return True
    return False


def match_line(path, number, text, patterns, allowances):
    return [
        (path, number, component, symbol)
        for component, symbol, pattern in patterns
        if pattern.search(text) and not allowed(path, text, symbol, allowances)
    ]


def git(root, arguments, data=None):
    result = subprocess.run(
        ["git", "-C", str(root), *arguments],
        input=data,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        check=False,
    )
    require(
        (
            result.returncode in {0, 1}
            if arguments[0] == "grep"
            else result.returncode == 0
        ),
        "git " + arguments[0] + " failed with exit " + str(result.returncode),
    )
    return result


def tracked_paths(root):
    data = git(root, ["ls-files", "-z", "--cached"]).stdout
    return sorted(set(os.fsdecode(path) for path in data.split(b"\x00") if path))


def path_filters(root, paths, tracked):
    result = []
    for value in paths:
        path = Path(value)
        if path.is_absolute():
            try:
                value = str(path.relative_to(root))
            except ValueError as error:
                raise Invalid("path filter is outside the repository") from error
        if value in {".", "./"}:
            result.append("")
            continue
        value = relative_path(value)
        require(
            any(below(candidate, value) for candidate in tracked),
            "path filter has no tracked files: " + value,
        )
        result.append(value)
    return result


def scan(root, catalogue, component_ids, paths, deleted=False):
    components = catalogue["components"]
    selected = sorted(set(component_ids)) if component_ids else sorted(components)
    require(all(name in components for name in selected), "unknown component")
    tracked = tracked_paths(root)
    filters = path_filters(root, paths, tracked)
    deleted_trees = {
        tree for entry in components.values() for tree in entry["deleted_trees"]
    }
    findings = []
    if deleted:
        for name in selected:
            for tree in components[name]["deleted_trees"]:
                if filters and not any(
                    not path or below(tree, path) or below(path, tree)
                    for path in filters
                ):
                    continue
                if os.path.lexists(root / tree):
                    findings.append((tree, 1, name, "deleted tree still exists"))
    files = [
        path
        for path in tracked
        if (not filters or any(not scope or below(path, scope) for scope in filters))
        and path != "tools/paxeer-x/retired_callers.json"
        and not is_test_file(path)
        and not any(below(path, tree) for tree in deleted_trees)
    ]
    patterns = [
        (name, symbol, symbol_pattern(symbol))
        for name in selected
        for symbol in components[name]["symbols"]
    ]
    if not files:
        return sorted(set(findings))
    literals = sorted({symbol for _, symbol, _ in patterns})
    pathspecs = [":(top,literal)" + path for path in files]
    pattern_bytes = ("\n".join(literals) + "\n").encode()
    for start in range(0, len(pathspecs), 128):
        result = git(
            root,
            [
                "grep",
                "--no-ext-grep",
                "--no-textconv",
                "--no-color",
                "--full-name",
                "-I",
                "-n",
                "-z",
                "-F",
                "-f",
                "-",
                "--",
                *pathspecs[start : start + 128],
            ],
            pattern_bytes,
        )
        output = result.stdout
        offset = 0
        while offset < len(output):
            path_end = output.find(b"\x00", offset)
            line_end = output.find(b"\x00", path_end + 1)
            text_end = output.find(b"\n", line_end + 1)
            require(
                path_end >= offset and line_end > path_end and text_end > line_end,
                "malformed git grep output",
            )
            path = os.fsdecode(output[offset:path_end])
            number = int(output[path_end + 1 : line_end])
            text = output[line_end + 1 : text_end].decode(
                "utf-8", errors="surrogateescape"
            )
            findings.extend(
                match_line(
                    path,
                    number,
                    text,
                    patterns,
                    catalogue.get("allowed_references", []),
                )
            )
            offset = text_end + 1
    return sorted(set(findings))


def check_equal(actual, expected, name):
    global COUNT
    require(actual == expected, "self-test failed: " + name)
    COUNT += 1
    print("ok retired-callers-case " + str(COUNT) + " " + name, flush=True)


def self_test(catalogue):
    components = catalogue["components"]
    spec = (ROOT / "spec/layerx-merge/spec.kvx").read_text()
    required = set()
    for line in spec.splitlines():
        if line.startswith("verify_cmd = ") and "retired_callers_test.py" in line:
            required.update(re.findall(r"--component ([a-z][a-z0-9-]*)", line))
    check_equal(set(components), required, "complete component catalogue")
    check_equal(
        {entry.get("req") for entry in components.values()},
        {str(11000 + number) for number in range(1, 50) if number not in {6, 7}},
        "every retirement requirement represented",
    )
    check_equal(
        "layerxverify" in components["verify"]["symbols"], True, "real verifier symbol"
    )
    check_equal(
        "layerx_paxeer_verifier" in components["paxeer-verifier"]["symbols"],
        True,
        "real checkpoint verifier symbol",
    )
    patterns = [
        ("verify", "layerxverify", symbol_pattern("layerxverify")),
        ("guarantor-runtime", "lxp_guarantor", symbol_pattern("lxp_guarantor")),
        ("guarantor-runtime", "LAYERX_GUARANTOR_", symbol_pattern("LAYERX_GUARANTOR_")),
        (
            "wire-codec",
            "layerx_wire::activity",
            symbol_pattern("layerx_wire::activity"),
        ),
    ]
    allowances = catalogue.get("allowed_references", [])
    check_equal(
        match_line("live.rs", 2, "use layerxverify;", patterns, allowances),
        [("live.rs", 2, "verify", "layerxverify")],
        "exact file and line",
    )
    check_equal(
        match_line(
            "live.rs",
            1,
            "retained_layerxverify layerxverifyExtra",
            patterns,
            allowances,
        ),
        [],
        "identifier boundaries",
    )
    check_equal(
        match_line(
            "live.c", 4, "lxp_guarantor_init LAYERX_GUARANTOR_KEY", patterns, allowances
        ),
        [
            ("live.c", 4, "guarantor-runtime", "lxp_guarantor"),
            ("live.c", 4, "guarantor-runtime", "LAYERX_GUARANTOR_"),
        ],
        "retired namespace prefixes",
    )
    check_equal(
        match_line("live.rs", 1, "layerx_wire::activity::decode", patterns, allowances),
        [("live.rs", 1, "wire-codec", "layerx_wire::activity")],
        "qualified symbol",
    )
    check_equal(
        bool(
            symbol_pattern("checkpoint-settlement.json").search(
                "checkpoint-settlementXjson"
            )
        ),
        False,
        "literal punctuation",
    )
    check_equal(
        is_test_file("agent/crates/layerx-client/examples/go_verifier_vectors.rs"),
        False,
        "examples remain live",
    )
    for path in (
        "pkg/keeper_test.go",
        "pkg/test_keeper.c",
        "src/tests.rs",
        "web/page.test.ts",
        "web/page.spec.tsx",
        "tests/live.rs",
        "pkg/testdata/live.json",
        "tools/paxeer-x/tests/routes_test.py",
    ):
        check_equal(is_test_file(path), True, "test file " + path)
    check_equal(
        below("src/retired-next/live.c", "src/retired"), False, "deleted tree boundary"
    )
    all_patterns = [
        (name, symbol, symbol_pattern(symbol))
        for name, component in components.items()
        for symbol in component["symbols"]
    ]
    for package, address in (
        ("layerxverify", "LayerXVerifyAddress"),
        ("layerxcustody", "LayerXCustodyAddress"),
        ("layerxanchor", "LayerXAnchorAddress"),
    ):
        registration = (
            f"ecommon.HexToAddress({package}.{address}): "
            f"{package}.GetVersioned(latestUpgrade, keepers),"
        )
        check_equal(
            match_line(
                "precompiles/setup.go", 81, registration, all_patterns, allowances
            ),
            [],
            "kept registration",
        )
    verifier_import = (
        "custodytypes "
        '"github.com/Sidiora-Labs/Paxeer-X-Network/modules/layerxcustody/types"'
    )
    check_equal(
        match_line(
            "custodyproof/verify.go", 260, verifier_import, all_patterns, allowances
        ),
        [],
        "kept commit and header verifier",
    )
    check_equal(
        match_line("live.go", 260, verifier_import, all_patterns, allowances) != [],
        True,
        "kept verifier allowance is path scoped",
    )
    for entry in allowances:
        for text in entry.get("lines", []):
            check_equal(
                match_line(entry["path"], 81, text, all_patterns, allowances),
                [],
                "kept line " + entry["path"],
            )
    for code in sorted(KEPT_CODES):
        code_patterns = [("verify", code, symbol_pattern(code)), patterns[0]]
        check_equal(
            match_line(
                "include/layerx/lxp_result.h", 42, code, code_patterns, allowances
            ),
            [],
            "kept disabled code",
        )
        check_equal(
            match_line("live.c", 3, code + " layerxverify", code_patterns, allowances),
            [("live.c", 3, "verify", "layerxverify")],
            "disabled code cannot hide callers",
        )
    check_equal(
        match_line(
            "precompiles/setup.go", 81, "layerxverify.execute();", patterns, allowances
        ),
        [("precompiles/setup.go", 81, "verify", "layerxverify")],
        "kept line number is not a blanket exclusion",
    )
    with tempfile.TemporaryDirectory(prefix="retired-callers-") as directory:
        root = Path(directory)
        git(root, ["init", "-q"])
        files = {
            "platform/hosted/indexer/src/abi.rs": 'retained\n("layerxverify", 0x1012),\n',
            "live.c": "lxp_guarantor_init();\n",
            "examples/vectors.rs": "layerxverify\n",
            "pkg/keeper_test.go": "layerxverify\n",
            "pkg/tests/live.rs": "layerxverify\n",
            "odd:name\nfile.rs": "layerxverify\n",
            "clean.rs": "retained_layerxverify layerxverifyExtra\n",
        }
        for entry in allowances:
            if "lines" in entry:
                files[entry["path"]] = "\n".join(entry["lines"]) + "\n"
        retired = components["paxeer-verifier"]["deleted_trees"][0]
        retired_file = (
            retired if PurePosixPath(retired).suffix else retired + "/src/lib.rs"
        )
        files[retired_file] = "layerxverify\n"
        for path, text in files.items():
            target = root / path
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_text(text)
        git(root, ["add", "--", *files])
        (root / "untracked.rs").write_text("layerxverify\n")
        expected = [
            ("examples/vectors.rs", 1, "verify", "layerxverify"),
            ("odd:name\nfile.rs", 1, "verify", "layerxverify"),
            ("platform/hosted/indexer/src/abi.rs", 2, "verify", "layerxverify"),
        ]
        check_equal(
            scan(root, catalogue, ["verify"], []),
            expected,
            "real git tracked scan and exclusions",
        )
        target = "platform/hosted/indexer/src/abi.rs"
        check_equal(
            scan(root, catalogue, ["verify"], [target]),
            [expected[-1]],
            "component and file filters",
        )
        check_equal(
            scan(root, catalogue, ["verify"], ["platform/hosted/indexer"]),
            [expected[-1]],
            "directory filter",
        )
        check_equal(
            scan(root, catalogue, ["verify"], [str(root / target)]),
            [expected[-1]],
            "absolute in-repository filter",
        )
        check_equal(
            scan(root, catalogue, ["guarantor-runtime"], [target]),
            [],
            "component filter isolates symbols",
        )
        check_equal(
            scan(root, catalogue, ["verify", "guarantor-runtime"], [target, "live.c"]),
            [("live.c", 1, "guarantor-runtime", "lxp_guarantor"), expected[-1]],
            "repeated component and path filters",
        )
        for arguments in (
            ["--component", "verify", "--path", target],
            ["--component", "verify", "--path", "./" + target],
        ):
            output = io.StringIO()
            with contextlib.redirect_stdout(output):
                exit_code = main(arguments, root)
            check_equal(exit_code, 1, "live caller exit code")
            check_equal(
                output.getvalue(),
                target + ":2: verify: layerxverify\n",
                "live caller exact diagnostic",
            )
        (root / target).write_text("retained\n")
        output = io.StringIO()
        with contextlib.redirect_stdout(output):
            exit_code = main(["--component", "verify", "--path", target], root)
        check_equal(
            (exit_code, output.getvalue()), (0, ""), "working-tree removal exits zero"
        )
        check_equal(
            scan(root, catalogue, ["paxeer-verifier"], [], deleted=True),
            [(retired, 1, "paxeer-verifier", "deleted tree still exists")],
            "deleted gate refuses remaining tree",
        )
        (root / retired_file).unlink()
        if retired_file != retired:
            parent = (root / retired_file).parent
            while parent != root / retired:
                parent.rmdir()
                parent = parent.parent
            parent.rmdir()
        check_equal(
            scan(root, catalogue, ["paxeer-verifier"], [], deleted=True),
            [],
            "unstaged tree deletion accepted",
        )
        for arguments in (
            ["--component", "unknown"],
            ["--path", "missing.rs"],
            ["--path", "../live.c"],
            ["--path", str(root.parent / "live.c")],
            ["--path", ":(glob)*"],
            ["--path", "untracked.rs"],
        ):
            output, errors = io.StringIO(), io.StringIO()
            with contextlib.redirect_stdout(output), contextlib.redirect_stderr(errors):
                exit_code = main(arguments, root)
            check_equal(exit_code, 2, "invalid filter refused")
            check_equal(output.getvalue(), "", "refusal is not a passing scan")
        (root / retired).symlink_to(root / "absent")
        check_equal(
            scan(root, catalogue, ["paxeer-verifier"], [], deleted=True),
            [(retired, 1, "paxeer-verifier", "deleted tree still exists")],
            "dangling deleted-tree symlink refused",
        )
    for document, message in (
        ({}, "components are missing"),
        ({"components": {}}, "components are missing"),
        ({"components": {"verify": {"symbols": []}}}, "incomplete component: verify"),
    ):
        try:
            validate_catalogue(document)
        except Invalid as error:
            check_equal(str(error), message, "malformed catalogue exact refusal")
        else:
            raise Invalid("self-test failed: malformed catalogue accepted")
    try:
        json.loads('{"components":{},"components":{}}', object_pairs_hook=unique_object)
    except Invalid as error:
        check_equal(
            str(error),
            "duplicate catalogue key: components",
            "duplicate catalogue key exact refusal",
        )
    else:
        raise Invalid("self-test failed: duplicate catalogue key accepted")


def main(argv=None, root=ROOT):
    parser = argparse.ArgumentParser(
        description="Check tracked files for retired callers."
    )
    parser.add_argument("--component", action="append", default=[])
    parser.add_argument("--path", action="append", default=[])
    parser.add_argument(
        "--deleted",
        action="store_true",
        help="also require the selected deleted trees to be absent",
    )
    parser.add_argument("--self-test", action="store_true")
    args = parser.parse_args(argv)
    if args.self_test and (args.component or args.path or args.deleted):
        parser.error("--self-test cannot be combined with scan filters")
    try:
        catalogue = load_catalogue(CATALOGUE)
        if args.self_test:
            self_test(catalogue)
            print(f"PAXEER_X_GATE tests={COUNT} skipped=0")
            return 0
        findings = scan(root, catalogue, args.component, args.path, args.deleted)
        for path, number, component, symbol in findings:
            display_path = path.replace("\n", "\\n").replace("\r", "\\r")
            print(f"{display_path}:{number}: {component}: {symbol}")
        return 1 if findings else 0
    except (Invalid, OSError, ValueError, TypeError, KeyError) as error:
        message = str(error) if isinstance(error, Invalid) else type(error).__name__
        print("retired-callers: refusal: " + message, file=sys.stderr)
        if args.self_test:
            print(f"PAXEER_X_GATE tests={COUNT} skipped=0")
        return 2


if __name__ == "__main__":
    sys.exit(main())
