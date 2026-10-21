from __future__ import annotations

import asyncio
from dataclasses import dataclass, field
import json
import os
from pathlib import Path
import re
import subprocess
import uuid

from phase2_types import canonical, digest, validate_v2

SIGNER = r'''
const readline=require('node:readline');
const ethers=require(process.argv[1]);
const wallets=new Map();
const input=readline.createInterface({input:process.stdin,crlfDelay:Infinity});
let chain=Promise.resolve();
input.on('line',line=>{chain=chain.then(async()=>{
 try {
  const request=JSON.parse(line);let result;
  if(request.op==='create'){const wallet=ethers.Wallet.createRandom();wallets.set(wallet.address.toLowerCase(),wallet);result={wallet:wallet.address.toLowerCase()};}
  else if(request.op==='sign'){const wallet=wallets.get(request.wallet);if(!wallet)throw Error('unknown owned wallet');const signature=await wallet.signMessage(request.message);if(ethers.verifyMessage(request.message,signature).toLowerCase()!==request.wallet)throw Error('signature recovery mismatch');result={signature};}
  else throw Error('unsupported signing operation');
  process.stdout.write(JSON.stringify({ok:true,result})+'\n');
 } catch { process.stdout.write(JSON.stringify({ok:false})+'\n'); }
});});
'''


class WalletSigner:
    def __init__(self, node, node_sha256, ethers_module):
        executable = Path(node)
        module = Path(ethers_module)
        if not executable.is_absolute() or executable.is_symlink() or not executable.is_file() or digest(executable.read_bytes()) != node_sha256 or not module.is_absolute() or not (module / "package.json").is_file():
            raise ValueError("bound Node executable and actual ethers runtime required")
        self.child = subprocess.Popen([str(executable), "-e", SIGNER, str(module)], stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, text=True, start_new_session=True)
        self.lock = asyncio.Lock()

    def _exchange(self, request):
        if self.child.poll() is not None:
            raise RuntimeError("owned signer exited")
        self.child.stdin.write(canonical(request).decode() + "\n")
        self.child.stdin.flush()
        raw = self.child.stdout.readline(8193)
        if not raw.endswith("\n") or len(raw) > 8192:
            raise RuntimeError("invalid bounded signer response")
        reply = json.loads(raw)
        if reply.get("ok") is not True:
            raise RuntimeError("real wallet signing failed")
        return reply["result"]

    async def call(self, op, **fields):
        async with self.lock:
            try:
                return await asyncio.wait_for(asyncio.to_thread(self._exchange, {"op": op, **fields}), 10)
            except BaseException:
                self.close()
                raise

    def close(self):
        if self.child.poll() is None:
            self.child.terminate()
            try:
                self.child.wait(timeout=2)
            except subprocess.TimeoutExpired:
                self.child.kill()
                self.child.wait(timeout=2)
        self.child.stdin.close()
        self.child.stdout.close()


@dataclass
class Account:
    wallet: str
    session_id: str
    access_token: str = field(repr=False)
    expires_at: str = ""

    def public(self):
        return {"wallet": self.wallet, "session_id": self.session_id, "expires_at": self.expires_at}


async def login(http, signer):
    wallet = (await signer.call("create"))["wallet"]
    if not re.fullmatch(r"0x[0-9a-f]{40}", wallet):
        raise ValueError("real generated wallet required")
    challenge = (await http.call("POST", "/v2/auth/challenges", {"wallet": wallet, "origin": http.application_origin}, headers={"Idempotency-Key": str(uuid.uuid4())}, expected=(201,), schema="ChallengeResponse"))["data"]["challenge"]
    if challenge["wallet"].lower() != wallet or challenge["chainId"] != "125" or challenge["purpose"] != "login":
        raise ValueError("challenge wallet/chain/purpose mismatch")
    message = challenge["message"]
    if "\nChain ID: 125\n" not in message or not message.endswith("\nRequest ID: " + challenge["challengeId"]):
        raise ValueError("server SIWE message binding differs")
    signature = (await signer.call("sign", wallet=wallet, message=message))["signature"]
    value = await http.call("POST", "/v2/auth/sessions", {"challengeId": challenge["challengeId"], "proof": {"kind": "eoa_eip191", "wallet": wallet, "signature": signature}, "deviceLabel": "Mixed traffic generator"}, schema="LoginResponse")
    data = value["data"]
    if data["session"]["wallet"].lower() != wallet or not data["accessToken"]:
        raise ValueError("real authenticated session required")
    return Account(wallet, data["session"]["sessionId"], data["accessToken"], data["session"]["accessExpiresAt"])


async def metadata_headers(signer, account, method, path, body, audience):
    import secrets
    import time
    operation, nonce = str(uuid.uuid4()), secrets.token_hex(32)
    expiry = str(int(time.time()) + 120)
    message = "\n".join(("KindleLaunch metadata action v2", audience, "125", account.wallet, operation, method, path, digest(canonical(body)), nonce, expiry))
    signature = (await signer.call("sign", wallet=account.wallet, message=message))["signature"]
    return {"X-Action-Wallet": account.wallet, "Idempotency-Key": operation, "X-Action-Signature": signature, "X-Action-Nonce": nonce, "X-Action-Expires": expiry, "X-Action-Audience": audience}
