"""Loopback-only calibration transports. No production endpoint permission implied."""
import asyncio,base64,hashlib,json,os,struct,time
from protocol import subscribe
async def null_request(port,sample):
    reader,writer=await asyncio.open_connection("127.0.0.1",port,limit=4096)
    try:
        writer.write(json.dumps({"id":sample.id,"scheduled_ns":sample.scheduled_ns,"sender_monotonic_ns":time.monotonic_ns(),"sender_utc_ns":time.time_ns()}).encode()+b"\n")
        await writer.drain();data=await reader.readline()
        if len(data)>4096: raise ValueError("oversize response")
        return json.loads(data)
    finally: writer.close();await writer.wait_closed()
async def websocket_subscribe(port,topics):
    message=json.dumps(subscribe(topics)).encode()
    reader,writer=await asyncio.open_connection("127.0.0.1",port,limit=8192)
    try:
        key=base64.b64encode(os.urandom(16)).decode()
        writer.write((f"GET /v1/stream HTTP/1.1\r\nHost: 127.0.0.1:{port}\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: {key}\r\nSec-WebSocket-Version: 13\r\n\r\n").encode());await writer.drain()
        header=await reader.readuntil(b"\r\n\r\n")
        expected=base64.b64encode(hashlib.sha1((key+"258EAFA5-E914-47DA-95CA-C5AB0DC85B11").encode()).digest())
        headers={line.split(b":",1)[0].strip().lower():line.split(b":",1)[1].strip() for line in header.split(b"\r\n")[1:] if b":" in line}
        if not header.startswith(b"HTTP/1.1 101 ") or headers.get(b"sec-websocket-accept")!=expected or headers.get(b"upgrade",b"").lower()!=b"websocket" or b"upgrade" not in [v.strip().lower() for v in headers.get(b"connection",b"").split(b",")]: raise ValueError("invalid WebSocket handshake")
        mask=os.urandom(4);n=len(message)
        if n>8192: raise ValueError("frame bound")
        length=bytes([0x80|n]) if n<126 else b"\xfe"+struct.pack("!H",n)
        writer.write(b"\x81"+length+mask+bytes(v^mask[i%4] for i,v in enumerate(message)));await writer.drain()
        # Calibration receiver echoes original validated subscription, not a fake application event.
        h=await reader.readexactly(2)
        if h[0]!=0x81 or h[1]&0x80: raise ValueError("unsupported receiver frame")
        n=h[1]&127
        if n==127: raise ValueError("64-bit frames outside calibration bound")
        if n==126:n=struct.unpack("!H",await reader.readexactly(2))[0]
        if n>8192: raise ValueError("oversize receiver frame")
        result=json.loads(await reader.readexactly(n))
        if result!=json.loads(message): raise ValueError("subscription echo mismatch")
        return result
    finally:writer.close();await writer.wait_closed()
