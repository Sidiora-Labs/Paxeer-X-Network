#!/usr/bin/env python3
"""Generate Swift and C# operation surfaces from the LayerX schemas."""

from __future__ import annotations

import argparse
import json
import re
from dataclasses import dataclass
from pathlib import Path


@dataclass(frozen=True)
class Operation:
    plane: str
    name: str
    method: str
    path: str
    request: str
    response: str
    idempotent: bool
    bodyless: bool


def parse_document(path: Path) -> dict[str, dict[str, str]]:
    current: str | None = None
    values: dict[str, str] = {}
    sections: dict[str, dict[str, str]] = {}

    def finish() -> None:
        nonlocal current, values
        if current is None:
            return
        if current in sections:
            raise ValueError(f"duplicate section {current} in {path}")
        sections[current] = values
        current = None
        values = {}

    for raw in path.read_text(encoding="utf-8").splitlines():
        line = raw.strip()
        section = re.fullmatch(r"\[([^]]+)]", line)
        if section:
            finish()
            current = section.group(1)
            continue
        if current is not None and "=" in line and not line.startswith("#"):
            key, value = line.split("=", 1)
            values[key.strip()] = value.strip()
    finish()
    return sections


def operations(repo: Path) -> list[Operation]:
    found: dict[tuple[str, str], Operation] = {}
    for plane, relative in (
        ("agent", "services/agent/schema/agent-api"),
        ("human", "services/human/schema/human-api"),
    ):
        root = repo / relative
        sections: dict[str, dict[str, str]] = {}
        root_document = parse_document(root / "v1.kvx")
        includes = json.loads(root_document.get("schema", {}).get("includes", "[]"))
        if not isinstance(includes, list) or not all(isinstance(item, str) for item in includes):
            raise ValueError(f"invalid schema.includes in {root / 'v1.kvx'}")
        for path in [root / "v1.kvx", *(root / item for item in includes)]:
            if path.parent != root or not path.is_file():
                raise ValueError(f"invalid schema include {path}")
            for name, values in parse_document(path).items():
                if not name.startswith(("operation.", "mutation.")):
                    continue
                if name in sections and sections[name] != values:
                    raise ValueError(f"conflicting section {name}")
                sections[name] = values
        agent_mutations = {
            name.removeprefix("mutation.")
            for name in sections
            if name.startswith("mutation.")
        }
        for section, values in sections.items():
            if not section.startswith("operation."):
                continue
            name = section.removeprefix("operation.")
            method = values.get("method", '"POST"').strip('"')
            route = values.get("path", '""').strip('"')
            request = values.get("request", '"object"').strip('"')
            response = values.get("response", '"object"').strip('"')
            idempotent = values.get("idempotency") == "true" or (
                plane == "agent"
                and (name in agent_mutations or "idempotency_key" in values.get("required", ""))
            )
            operation = Operation(
                plane, name, method, route, request, response, idempotent, request == "Empty"
            )
            key = (plane, operation.name)
            previous = found.get(key)
            if previous is not None and previous != operation:
                raise ValueError(f"conflicting operation {plane}:{operation.name}")
            found[key] = operation
    return sorted(found.values(), key=lambda item: (item.plane, item.name))


def words(operation: Operation) -> list[str]:
    return [operation.plane, *re.split(r"[.\-_]", operation.name)]


def swift_name(operation: Operation) -> str:
    pieces = words(operation)
    return pieces[0] + "".join(piece[:1].upper() + piece[1:] for piece in pieces[1:])


def csharp_name(operation: Operation) -> str:
    return "".join(piece[:1].upper() + piece[1:] for piece in words(operation))


def swift_catalog(items: list[Operation]) -> str:
    cases = "\n".join(
        f'    case {swift_name(item)} = "{item.plane}:{item.name}"' for item in items
    )
    descriptors = "\n".join(
        "        case .{case_name}: return OperationDescriptor(plane: .{plane}, name: \"{name}\", "
        "method: .{method}, path: \"{path}\", requestType: \"{request}\", "
        "responseType: \"{response}\", requiresIdempotency: {idempotent}, bodyless: {bodyless})".format(
            case_name=swift_name(item),
            plane=item.plane,
            name=item.name,
            method=item.method.lower(),
            path=item.path,
            request=item.request.replace('"', '\\"'),
            response=item.response.replace('"', '\\"'),
            idempotent=str(item.idempotent).lower(),
            bodyless=str(item.bodyless).lower(),
        )
        for item in items
    )
    methods = []
    for item in items:
        name = swift_name(item)
        if item.idempotent and item.bodyless:
            signature = (
                f"    func {name}(idempotencyKey: IdempotencyKey, "
                "pathParameters: [String: String] = [:]) async throws -> JSONValue"
            )
            call = (
                f"try await mutate(.{name}, request: .emptyObject, idempotencyKey: idempotencyKey, "
                "pathParameters: pathParameters)"
            )
        elif item.idempotent:
            signature = (
                f"    func {name}(_ request: JSONValue, idempotencyKey: IdempotencyKey, "
                "pathParameters: [String: String] = [:]) async throws -> JSONValue"
            )
            call = (
                f"try await mutate(.{name}, request: request, idempotencyKey: idempotencyKey, "
                "pathParameters: pathParameters)"
            )
        elif item.bodyless:
            signature = (
                f"    func {name}(_ request: JSONValue = .object([:]), "
                "pathParameters: [String: String] = [:]) async throws -> JSONValue"
            )
            call = f"try await read(.{name}, request: request, pathParameters: pathParameters)"
        else:
            signature = (
                f"    func {name}(_ request: JSONValue, "
                "pathParameters: [String: String] = [:]) async throws -> JSONValue"
            )
            call = f"try await read(.{name}, request: request, pathParameters: pathParameters)"
        methods.append(f"{signature} {{\n        {call}\n    }}")
    return f'''// Generated from agent-api and human-api. Do not hand-edit.

public enum PlatformOperation: String, CaseIterable, Sendable {{
{cases}

    public var descriptor: OperationDescriptor {{
        switch self {{
{descriptors}
        }}
    }}
}}

public extension PlatformClient {{
{chr(10).join(methods)}
}}

private let sdkMetadata = SDKMetadata(name: "LayerXSDK", version: "0.1.0", agentOperations: {sum(item.plane == "agent" for item in items)}, humanOperations: {sum(item.plane == "human" for item in items)})

public func platform_sdk_swift() -> SDKMetadata {{ sdkMetadata }}
'''


def csharp_catalog(items: list[Operation]) -> str:
    enum_cases = ",\n".join(f"    {csharp_name(item)}" for item in items)
    descriptors = "\n".join(
        "            PlatformOperation.{case_name} => new(PlatformPlane.{plane}, \"{name}\", "
        "SdkHttpMethod.{method}, \"{path}\", \"{request}\", \"{response}\", {idempotent}, {bodyless}),".format(
            case_name=csharp_name(item),
            plane=item.plane.capitalize(),
            name=item.name,
            method=item.method.capitalize() if item.method != "DELETE" else "Delete",
            path=item.path,
            request=item.request.replace('"', '\\"'),
            response=item.response.replace('"', '\\"'),
            idempotent=str(item.idempotent).lower(),
            bodyless=str(item.bodyless).lower(),
        )
        for item in items
    )
    methods = []
    for item in items:
        name = csharp_name(item)
        if item.idempotent and item.bodyless:
            signature = (
                f"    public static Task<JsonValue> {name}Async(this PlatformClient client, "
                "IdempotencyKey idempotencyKey, IReadOnlyDictionary<string, string>? pathParameters = null, "
                "CancellationToken cancellationToken = default)"
            )
            call = (
                f"client.MutateAsync(PlatformOperation.{name}, JsonValue.EmptyObject, idempotencyKey, "
                "pathParameters, cancellationToken)"
            )
        elif item.idempotent:
            signature = (
                f"    public static Task<JsonValue> {name}Async(this PlatformClient client, JsonValue request, "
                "IdempotencyKey idempotencyKey, IReadOnlyDictionary<string, string>? pathParameters = null, "
                "CancellationToken cancellationToken = default)"
            )
            call = (
                f"client.MutateAsync(PlatformOperation.{name}, request, idempotencyKey, "
                "pathParameters, cancellationToken)"
            )
        elif item.bodyless:
            signature = (
                f"    public static Task<JsonValue> {name}Async(this PlatformClient client, JsonValue? request = null, "
                "IReadOnlyDictionary<string, string>? pathParameters = null, CancellationToken cancellationToken = default)"
            )
            call = (
                f"client.ReadAsync(PlatformOperation.{name}, request ?? JsonValue.EmptyObject, "
                "pathParameters, cancellationToken)"
            )
        else:
            signature = (
                f"    public static Task<JsonValue> {name}Async(this PlatformClient client, JsonValue request, "
                "IReadOnlyDictionary<string, string>? pathParameters = null, CancellationToken cancellationToken = default)"
            )
            call = (
                f"client.ReadAsync(PlatformOperation.{name}, request, "
                "pathParameters, cancellationToken)"
            )
        methods.append(f"{signature} =>\n        {call};")
    return f'''// Generated from agent-api and human-api. Do not hand-edit.
#nullable enable

namespace LayerX.Sdk;

public enum PlatformOperation
{{
{enum_cases}
}}

public static class GeneratedOperationCatalog
{{
    public static OperationDescriptor Descriptor(this PlatformOperation operation) => operation switch
    {{
{descriptors}
        _ => throw new ArgumentOutOfRangeException(nameof(operation)),
    }};

    public static SdkMetadata platform_sdk_dotnet()
    {{
        return new("LayerX.Sdk", "0.1.0", {sum(item.plane == "agent" for item in items)}, {sum(item.plane == "human" for item in items)});
    }}
}}

public static class GeneratedPlatformClientExtensions
{{
{chr(10).join(methods)}
}}
'''


def render(repo: Path) -> dict[Path, str]:
    items = operations(repo)
    manifest = {
        "schema": 1,
        "operations": [
            {
                "plane": item.plane,
                "name": item.name,
                "method": item.method,
                "path": item.path,
                "request": item.request,
                "response": item.response,
                "idempotency": item.idempotent,
                "bodyless": item.bodyless,
            }
            for item in items
        ],
    }
    return {
        repo / "sdk/platform/swift/Sources/LayerXSDK/Generated/OperationCatalog.swift": swift_catalog(items),
        repo / "sdk/platform/dotnet/Generated/OperationCatalog.cs": csharp_catalog(items),
        repo / "sdk/platform/conformance/operations.json": json.dumps(manifest, indent=2) + "\n",
    }


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--check", action="store_true")
    parser.add_argument("repo", nargs="?", default=".")
    args = parser.parse_args()
    repo = Path(args.repo).resolve()
    stale: list[str] = []
    for path, content in render(repo).items():
        if args.check:
            if not path.is_file() or path.read_text(encoding="utf-8") != content:
                stale.append(str(path.relative_to(repo)))
        else:
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(content, encoding="utf-8")
    if stale:
        raise SystemExit("stale generated portable SDK output: " + ", ".join(stale))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
