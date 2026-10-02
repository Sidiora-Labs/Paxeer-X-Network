#!/bin/sh
set -eu
exec python3 -I - "$@" <<'PY'
import json
import os
import stat
import sys
import tempfile

UID = 4020
MAX_FILE_BYTES = 1024 * 1024
FILE_VARIABLES = (
    "LAYERX_GATEWAY_COMPONENT_TOKEN_FILE",
    "LAYERX_GATEWAY_AUTHORITY_TOKEN_FILE",
    "LAYERX_GATEWAY_IDENTITY_TOKEN_FILE",
    "LAYERX_GATEWAY_PROGRAM_REGISTRY_TOKEN_FILE",
    "LAYERX_GATEWAY_CLIENT_IDENTITY_PKCS12",
    "LAYERX_GATEWAY_CLIENT_IDENTITY_PASSWORD_FILE",
    "LAYERX_GATEWAY_SEQUENCER_PUBLIC_KEY_FILE",
    "LAYERX_GATEWAY_SEQUENCER_ID_FILE",
    "LAYERX_GATEWAY_SEQUENCER_FIRST_BATCH_FILE",
    "LAYERX_GATEWAY_SEQUENCER_LAST_BATCH_FILE",
    "LAYERX_GATEWAY_KEY_PROVISIONING_KEY_FILE",
    "LAYERX_GATEWAY_MODULE_REGISTRY_FILE",
    "LAYERX_GATEWAY_OUTBOUND_CA_DER",
    "LAYERX_GATEWAY_TLS_CERT_DER",
    "LAYERX_GATEWAY_TLS_KEY_DER",
    "LAYERX_GATEWAY_REDIS_USERNAME_FILE",
    "LAYERX_GATEWAY_REDIS_PASSWORD_FILE",
    "LAYERX_GATEWAY_IDENTITY_PROVISIONING_TOKEN_FILE",
    "LAYERX_GATEWAY_FAUCET_SERVICE_TOKEN_FILE",
)
BINDINGS_VARIABLE = "LAYERX_GATEWAY_ROUTE_BINDINGS_FILE"
created = []
runtime_directory = None
copied = {}


def require(condition):
    if not condition:
        raise ValueError("invalid protected startup input")


def protected_bytes(path):
    require(isinstance(path, str) and path.startswith("/"))
    parts = path.split("/")[1:]
    require(parts and all(part not in ("", ".", "..") for part in parts))
    directory = os.open("/", os.O_RDONLY | os.O_DIRECTORY | os.O_CLOEXEC)
    try:
        for part in parts[:-1]:
            child = os.open(part, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW
                            | os.O_CLOEXEC, dir_fd=directory)
            os.close(directory)
            directory = child
        descriptor = os.open(parts[-1], os.O_RDONLY | os.O_NOFOLLOW
                             | os.O_NONBLOCK | os.O_CLOEXEC, dir_fd=directory)
    finally:
        os.close(directory)
    with os.fdopen(descriptor, "rb") as source:
        before = os.fstat(source.fileno())
        require(stat.S_ISREG(before.st_mode) and stat.S_IMODE(before.st_mode) == 0o600
                and before.st_nlink == 1 and before.st_uid in (0, UID)
                and 0 < before.st_size <= MAX_FILE_BYTES)
        data = source.read(MAX_FILE_BYTES + 1)
        after = os.fstat(source.fileno())
        identity = lambda value: (value.st_dev, value.st_ino, value.st_mode,
                                  value.st_nlink, value.st_uid, value.st_gid,
                                  value.st_size, value.st_mtime_ns, value.st_ctime_ns)
        require(identity(before) == identity(after) and len(data) == before.st_size)
        return data


def private_copy(data):
    require(0 < len(data) <= MAX_FILE_BYTES)
    destination = os.path.join(runtime_directory, "input-" + str(len(created)))
    descriptor = os.open(destination, os.O_WRONLY | os.O_CREAT | os.O_EXCL
                         | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600)
    created.append(destination)
    with os.fdopen(descriptor, "wb") as output:
        output.write(data)
        output.flush()
        os.fchmod(output.fileno(), 0o600)
        os.fchown(output.fileno(), UID, UID)
        info = os.fstat(output.fileno())
        require(stat.S_ISREG(info.st_mode) and stat.S_IMODE(info.st_mode) == 0o600
                and info.st_nlink == 1 and info.st_uid == UID and info.st_gid == UID)
    return destination


def copy_file(path):
    if path not in copied:
        copied[path] = private_copy(protected_bytes(path))
    return copied[path]


def unique_object(pairs):
    result = {}
    for key, value in pairs:
        require(key not in result)
        result[key] = value
    return result


def invalid_constant(value):
    raise ValueError("invalid JSON constant")


try:
    require(os.geteuid() == 0)
    os.umask(0o077)
    runtime_directory = tempfile.mkdtemp(prefix="layerx-gateway-", dir="/run")
    environment = dict(os.environ)
    for variable in FILE_VARIABLES:
        if variable in environment:
            environment[variable] = copy_file(environment[variable])
    if BINDINGS_VARIABLE in environment:
        bindings = json.loads(protected_bytes(environment[BINDINGS_VARIABLE]),
                              object_pairs_hook=unique_object,
                              parse_constant=invalid_constant)
        require(isinstance(bindings, dict) and isinstance(bindings.get("services"), dict))
        for binding in bindings["services"].values():
            require(isinstance(binding, dict))
            authorization = binding.get("health_authorization_file")
            if authorization is not None:
                require(isinstance(authorization, str))
                binding["health_authorization_file"] = copy_file(authorization)
        environment[BINDINGS_VARIABLE] = private_copy(
            json.dumps(bindings, separators=(",", ":"), allow_nan=False).encode())
    os.chown(runtime_directory, UID, UID)
    os.chmod(runtime_directory, 0o700)
    os.execve("/sbin/su-exec", ["su-exec", "4020:4020",
                              "/usr/local/bin/layerx-gateway", *sys.argv[1:]], environment)
except Exception:
    for destination in created:
        try:
            os.unlink(destination)
        except OSError:
            pass
    if runtime_directory is not None:
        try:
            os.rmdir(runtime_directory)
        except OSError:
            pass
    print("gateway startup refused: protected file staging or privilege drop failed", file=sys.stderr)
    sys.exit(1)
PY
