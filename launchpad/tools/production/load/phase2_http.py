from __future__ import annotations

import asyncio
import http.client
import json
import ssl
import time
from urllib.parse import urlsplit

from phase2_types import canonical, origin, unique, validate_v2


class HTTPFailure(RuntimeError):
    def __init__(self, status, code):
        self.status = status
        self.code = code
        super().__init__(f"HTTP {status}: {code}")


class HTTP:
    def __init__(self, base, application_origin, *, ca_file=None, owned_local=False, timeout=3, max_bytes=1048576):
        self.url = origin(base, ("https", "http") if owned_local else ("https",))
        if self.url.scheme == "http" and self.url.hostname not in ("127.0.0.1", "::1", "localhost"):
            raise ValueError("plain HTTP confined to owned loopback")
        origin(application_origin)
        if not 0 < timeout <= 10 or not 1 <= max_bytes <= 1048576:
            raise ValueError("bounded transport required")
        self.application_origin = application_origin
        self.context = ssl.create_default_context(cafile=ca_file)
        self.timeout, self.max_bytes = timeout, max_bytes

    def request(self, method, path, body=None, *, bearer=None, headers=None, expected=(200,), schema=None):
        parsed = urlsplit(path)
        if method not in ("GET", "POST", "PUT", "PATCH", "DELETE") or not path.startswith("/") or path.startswith("//") or parsed.netloc or parsed.fragment or any(c in path for c in "\r\n\\") or len(path) > 8192:
            raise ValueError("explicit bounded request path required")
        raw = None if body is None else canonical(body)
        if raw is not None and len(raw) > self.max_bytes:
            raise ValueError("request byte bound")
        request_headers = {"Origin": self.application_origin, "Accept": "application/json"}
        if raw is not None:
            request_headers["Content-Type"] = "application/json"
        if bearer is not None:
            if not bearer or len(bearer) > 2048 or any(c in bearer for c in "\r\n"):
                raise ValueError("invalid bearer transport")
            request_headers["Authorization"] = "Bearer " + bearer
        for key, value in (headers or {}).items():
            if key.lower() in ("authorization", "host", "origin", "cookie") or any(c in key + value for c in "\r\n"):
                raise ValueError("credential/origin header override denied")
            request_headers[key] = value
        connection = (http.client.HTTPSConnection(self.url.hostname, self.url.port, timeout=self.timeout, context=self.context) if self.url.scheme == "https" else http.client.HTTPConnection(self.url.hostname, self.url.port, timeout=self.timeout))
        started = time.monotonic()
        try:
            connection.request(method, path, body=raw, headers=request_headers)
            response = connection.getresponse()
            chunks, count = [], 0
            while True:
                remaining = self.timeout - (time.monotonic() - started)
                if remaining <= 0:
                    raise TimeoutError("HTTP total deadline")
                if connection.sock is not None:
                    connection.sock.settimeout(remaining)
                chunk = response.read1(min(65536, self.max_bytes + 1 - count))
                if not chunk:
                    break
                count += len(chunk)
                if count > self.max_bytes:
                    raise ValueError("response byte bound")
                chunks.append(chunk)
            value = json.loads(b"".join(chunks), object_pairs_hook=unique) if count else None
            if response.status not in expected:
                code = value.get("code", "UNEXPECTED_STATUS") if isinstance(value, dict) else "UNEXPECTED_STATUS"
                raise HTTPFailure(response.status, str(code)[:80])
            if schema:
                validate_v2(schema, value)
            return value
        finally:
            connection.close()

    async def call(self, *args, **kwargs):
        return await asyncio.to_thread(self.request, *args, **kwargs)
