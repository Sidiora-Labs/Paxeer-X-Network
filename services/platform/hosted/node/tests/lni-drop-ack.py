#!/usr/bin/env python3
import json
import os
from pathlib import Path
import socket
import sys

MAX_FRAME = 1_212_416


def exact(stream, length):
    value = bytearray()
    while len(value) < length:
        part = stream.recv(length - len(value))
        if not part:
            raise ValueError('peer closed before its canonical frame completed')
        value.extend(part)
    return bytes(value)


def frame(stream):
    prefix = exact(stream, 4)
    size = int.from_bytes(prefix, 'big')
    if not 18 <= size <= MAX_FRAME:
        raise ValueError('native LNI frame outside canonical bounds')
    body = exact(stream, size)
    return prefix + body, int.from_bytes(body[4:6], 'big')


def main():
    endpoint, upstream, evidence = map(Path, sys.argv[1:])
    if endpoint.exists() or evidence.exists():
        raise ValueError('fresh relay paths required')
    with socket.socket(socket.AF_UNIX) as listener:
        listener.settimeout(10)
        listener.bind(str(endpoint))
        os.chmod(endpoint, 0o600)
        listener.listen(1)
        try:
            connection, _ = listener.accept()
            with connection, socket.socket(socket.AF_UNIX) as daemon:
                connection.settimeout(15)
                daemon.settimeout(15)
                daemon.connect(str(upstream))
                for _ in range(32):
                    request, tag = frame(connection)
                    daemon.sendall(request)
                    response, response_tag = frame(daemon)
                    if tag == 3:
                        if response_tag != 4:
                            raise ValueError('the real daemon did not acknowledge the submitted activity')
                        break
                    connection.sendall(response)
                else:
                    raise ValueError('no bounded admission attempt reached the native daemon')
            listener.settimeout(1)
            try:
                retried, _ = listener.accept()
            except socket.timeout:
                pass
            else:
                retried.close()
                raise ValueError('operator reconnected after an indeterminate admission')
            with evidence.open('x') as stream:
                json.dump({'submits': 1, 'response_tag': response_tag, 'reconnections': 0}, stream)
                stream.write('\n')
        finally:
            endpoint.unlink()


if __name__ == '__main__':
    main()
