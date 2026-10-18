from __future__ import annotations

import asyncio
import base64
import hashlib
import json
import os
import ssl
import struct
import time
import uuid
from urllib.parse import quote

from phase2_types import canonical, digest, origin, unique, validate_v2


class ReconciliationError(ValueError):
    pass


class HandshakeDenied(ReconciliationError):
    def __init__(self, status):
        self.status = status
        super().__init__("actual stream handshake denied")


class ReceiverState:
    def __init__(self, topics, snapshot, *, retain_fills=False):
        validate_v2("TopicSnapshot", snapshot)
        entries = {row["topic"]: row for row in snapshot["topics"]}
        if set(entries) != set(topics) or len(entries) != len(topics):
            raise ReconciliationError("snapshot scope differs")
        self.sequence = {topic: int(row["sequence"]) for topic, row in entries.items()}
        self.revision = {topic: int(row["revision"]) for topic, row in entries.items()}
        self.cursor = snapshot["resumeCursor"]
        self.frames = self.controls = 0
        self.fills = {} if retain_fills else None
        self.last_digest = {}
        self.checkpoint_count = 0

    def receive(self, value):
        if "op" in value:
            validate_v2("StreamControl", value)
            self.controls += 1
            if value["op"] in ("error", "resnapshot_required", "integrity_incident", "subscription_closed"):
                raise ReconciliationError("explicit server control: " + value["op"])
            if value["op"] == "checkpoint":
                if not value["cursor"]:
                    raise ReconciliationError("checkpoint lacks opaque cursor")
                self.cursor = value["cursor"]
                self.checkpoint_count += 1
            return False
        validate_v2("StreamEvent", value)
        topic = value["topic"]
        if topic not in self.sequence:
            raise ReconciliationError("event outside accepted scope")
        sequence, revision = int(value["sequence"]), int(value["revision"])
        fingerprint = digest(canonical(value))
        if sequence <= self.sequence[topic] and topic.startswith("market.trades.") and self.fills is not None and self.fills.get(value["eventId"]) == fingerprint:
            return False
        if sequence == self.sequence[topic] and fingerprint == self.last_digest.get(topic):
            return False
        if sequence <= self.sequence[topic] or revision < self.revision[topic]:
            raise ReconciliationError("sequence or revision regression")
        if value["coalesced"]:
            if not topic.startswith("market.price.") or value["sourceSequenceFirst"] is None or value["sourceSequenceLast"] is None or int(value["sourceSequenceFirst"]) != self.sequence[topic] + 1 or int(value["sourceSequenceLast"]) != sequence:
                raise ReconciliationError("invalid coalesced source coverage")
        elif sequence != self.sequence[topic] + 1 or value["sourceSequenceFirst"] is not None or value["sourceSequenceLast"] is not None:
            raise ReconciliationError("durable source gap")
        if topic.startswith("market.trades.") and self.fills is not None:
            identity = value["payload"]["eventId"]
            if identity != value["eventId"]:
                raise ReconciliationError("fill identity differs from canonical event")
            previous = self.fills.get(identity)
            if previous is not None and previous != fingerprint:
                raise ReconciliationError("immutable fill changed")
            if len(self.fills) >= 1000000 and previous is None:
                raise ReconciliationError("bounded replay inventory exhausted")
            self.fills[identity] = fingerprint
        self.sequence[topic], self.revision[topic] = sequence, revision
        self.last_digest[topic] = fingerprint
        self.frames += 1
        return True


class WebSocket:
    def __init__(self, base, application_origin, *, ca_file=None, owned_local=False):
        self.url = origin(base, ("https", "http") if owned_local else ("https",))
        if self.url.scheme == "http" and self.url.hostname not in ("127.0.0.1", "::1", "localhost"):
            raise ValueError("plain WebSocket confined to owned loopback")
        origin(application_origin)
        self.application_origin = application_origin
        self.context = ssl.create_default_context(cafile=ca_file)
        self.reader = self.writer = None

    async def connect(self, ticket=None):
        key = base64.b64encode(os.urandom(16)).decode()
        protocols = ["sidiora.v2"]
        if ticket is not None:
            import re
            if not re.fullmatch(r"[0-9a-f]{64}", ticket):
                raise ValueError("invalid single-use ticket")
            protocols.append("ticket." + ticket)
        secure = self.url.scheme == "https"
        self.reader, self.writer = await asyncio.wait_for(asyncio.open_connection(self.url.hostname, self.url.port or (443 if secure else 80), ssl=self.context if secure else None, server_hostname=self.url.hostname if secure else None, limit=32768), 5)
        request = ("GET /v2/stream HTTP/1.1\r\nHost: " + self.url.netloc + "\r\nOrigin: " + self.application_origin + "\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: " + key + "\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Protocol: " + ", ".join(protocols) + "\r\n\r\n")
        self.writer.write(request.encode())
        await self.writer.drain()
        header = await asyncio.wait_for(self.reader.readuntil(b"\r\n\r\n"), 5)
        status_line = header.split(b"\r\n", 1)[0].split(b" ")
        if len(status_line) >= 2 and status_line[1] in (b"401", b"403", b"409"):
            raise HandshakeDenied(int(status_line[1]))
        headers = {}
        for line in header.split(b"\r\n")[1:]:
            if b":" in line:
                name, value = line.split(b":", 1)
                name = name.lower().strip()
                if name in headers:
                    raise ReconciliationError("duplicate handshake header")
                headers[name] = value.strip()
        accept = base64.b64encode(hashlib.sha1((key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11").encode()).digest())
        if not header.startswith(b"HTTP/1.1 101 ") or headers.get(b"sec-websocket-accept") != accept or headers.get(b"sec-websocket-protocol") != b"sidiora.v2" or headers.get(b"upgrade", b"").lower() != b"websocket" or b"upgrade" not in [x.strip().lower() for x in headers.get(b"connection", b"").split(b",")]:
            raise ReconciliationError("actual v2 handshake denied")

    async def send_frame(self, opcode, payload):
        if len(payload) > 262144:
            raise ValueError("outgoing frame bound")
        mask = os.urandom(4)
        count = len(payload)
        length = bytes([0x80 | count]) if count < 126 else (b"\xfe" + struct.pack("!H", count) if count < 65536 else b"\xff" + struct.pack("!Q", count))
        self.writer.write(bytes([0x80 | opcode]) + length + mask + bytes(value ^ mask[i % 4] for i, value in enumerate(payload)))
        await asyncio.wait_for(self.writer.drain(), 5)

    async def subscribe(self, topics, cursor):
        body = validate_v2("Subscribe", {"op": "subscribe", "subscriptionId": str(uuid.uuid4()), "topics": topics, "afterCursor": cursor})
        await self.send_frame(1, canonical(body))

    async def receive(self):
        fragments, size, fragmented = [], 0, False
        while True:
            first, second = await asyncio.wait_for(self.reader.readexactly(2), 10)
            final, opcode = bool(first & 0x80), first & 15
            if first & 0x70 or second & 0x80:
                raise ReconciliationError("unsupported server masking/extensions")
            count = second & 127
            if count == 126:
                count = struct.unpack("!H", await self.reader.readexactly(2))[0]
            elif count == 127:
                count = struct.unpack("!Q", await self.reader.readexactly(8))[0]
            if count > 262144 or size + count > 262144:
                raise ReconciliationError("bounded server message exceeded")
            if opcode >= 8 and (not final or count > 125):
                raise ReconciliationError("invalid control frame")
            payload = await self.reader.readexactly(count)
            if opcode == 8:
                raise ConnectionError("server explicitly closed receiver")
            if opcode == 9:
                await self.send_frame(10, payload)
                continue
            if opcode == 10:
                continue
            if opcode == 1 and not fragmented:
                fragmented = not final
            elif opcode != 0 or not fragmented:
                raise ReconciliationError("invalid text continuation")
            fragments.append(payload)
            size += count
            if final:
                return json.loads(b"".join(fragments).decode("utf-8"), object_pairs_hook=unique)

    async def close(self):
        if self.writer is not None:
            self.writer.close()
            try:
                await asyncio.wait_for(self.writer.wait_closed(), 2)
            except (OSError, TimeoutError):
                pass


async def connect_receiver(http, websocket_origin, topics, account, *, ca_file=None, owned_local=False, state=None, retain_fills=False):
    topics = sorted(topics)
    validate_v2("Subscribe", {"op": "subscribe", "subscriptionId": "load-scope", "topics": topics, "afterCursor": None})
    bearer = None if account is None else account.access_token
    if state is None:
        snapshot = (await http.call("GET", "/v2/snapshots?topics=" + quote(",".join(topics), safe=""), bearer=bearer, schema="TopicSnapshotReadResponse"))["data"]["snapshot"]
        state = ReceiverState(topics, snapshot, retain_fills=retain_fills)
    ticket = None
    if account is not None:
        body = {"schemaMajor": 2, "topics": topics, "filterDigest": digest(canonical(topics)), "origin": http.application_origin, "endpoint": "/v2/stream"}
        validate_v2("StreamTicketIssueBody", body)
        ticket = (await http.call("POST", "/v2/stream/tickets", body, bearer=bearer, schema="StreamTicketIssueResponse"))["data"]["ticket"]
    socket = WebSocket(websocket_origin, http.application_origin, ca_file=ca_file, owned_local=owned_local)
    try:
        await socket.connect(ticket)
        await socket.subscribe(topics, state.cursor)
        welcome = await socket.receive()
        if welcome.get("op") != "subscribed":
            raise ReconciliationError("real accepted subscription acknowledgement required")
        state.receive(welcome)
        return socket, state
    except BaseException:
        await socket.close()
        raise
