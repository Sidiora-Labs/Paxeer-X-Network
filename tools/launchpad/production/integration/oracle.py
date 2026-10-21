"""Independent assurance mathematics. Never imports a product reducer.
Inputs are normalized decoded observations, NOT proof that a transaction mined.
"""
from dataclasses import dataclass, asdict
from fractions import Fraction
import hashlib
import json
import re

UINT256 = 2**256 - 1
INTERVALS = {"1m":60,"5m":300,"15m":900,"1h":3600,"4h":14400,"1d":86400,"1w":604800}

class IntegrityError(ValueError): pass

def integer(value, maximum=UINT256):
    if type(value) is not int or not 0 <= value <= maximum:
        raise IntegrityError("bounded unsigned integer required")
    return value

def identity(value, size):
    if not isinstance(value,str) or not re.fullmatch("0x[0-9a-f]{%d}" % size,value) or int(value[2:],16)==0:
        raise IntegrityError("nonzero normalized hex identity required")
    return value

@dataclass(frozen=True)
class Event:
    height: int
    block_hash: str
    parent_hash: str
    timestamp: int
    tx_index: int
    log_index: int
    tx_hash: str
    deployment: str
    pool: str
    recipient: str
    side: str
    token_raw: int
    usdl_raw: int
    token_decimals: int
    chain_id: int = 125
    abi_known: bool = True
    inventory_owner: str | None = None

    def __post_init__(self):
        if type(self.chain_id) is not int or self.chain_id != 125 or self.abi_known is not True:
            raise IntegrityError("wrong chain or unknown ABI")
        for x in (self.height,self.timestamp,self.tx_index,self.log_index,self.token_raw,self.usdl_raw): integer(x)
        integer(self.token_decimals,255)
        if not self.token_raw or not self.usdl_raw or self.side not in ("buy","sell"):
            raise IntegrityError("invalid trade")
        for x in (self.block_hash,self.parent_hash,self.tx_hash): identity(x,64)
        for x in (self.pool,self.recipient): identity(x,40)
        if self.inventory_owner is None:
            if self.side=="sell": raise IntegrityError("sell inventory owner must be explicit")
            object.__setattr__(self,"inventory_owner",self.recipient)
        identity(self.inventory_owner,40)
        if self.side=="buy" and self.inventory_owner!=self.recipient:
            raise IntegrityError("buy token recipient must own received inventory")
        if not isinstance(self.deployment,str) or not re.fullmatch("[A-Za-z0-9_-]{1,64}",self.deployment):
            raise IntegrityError("deployment identity required")

    @property
    def position(self): return (self.height,self.tx_index,self.log_index)
    @property
    def key(self): return (self.chain_id,self.deployment,self.tx_hash,self.log_index)

@dataclass(frozen=True)
class Block:
    height: int
    block_hash: str
    parent_hash: str
    timestamp: int
    events: tuple
    complete: bool = True
    def __post_init__(self):
        integer(self.height); integer(self.timestamp)
        identity(self.block_hash,64); identity(self.parent_hash,64)
        if self.complete is not True or type(self.events) is not tuple or len(self.events)>100000:
            raise IntegrityError("incomplete or oversized committed block")
        for e in self.events:
            if not isinstance(e,Event) or (e.height,e.block_hash,e.parent_hash,e.timestamp)!=(self.height,self.block_hash,self.parent_hash,self.timestamp):
                raise IntegrityError("event/header mismatch")


def inventory(blocks, start_height, parent_hash, max_events=100000):
    """Fail closed on any gap/conflict; never roll back committed blocks.
    Explicit complete empty blocks are required. Exact transport duplicates allowed.
    """
    integer(start_height); identity(parent_hash,64); integer(max_events,100000)
    total_events=0
    headers={}; events={}; positions={}; complete_sets={}; tx_positions={}; tx_hashes={}; log_positions={}
    for i,b in enumerate(blocks):
        if i>=100000: raise IntegrityError("inventory bound")
        if not isinstance(b,Block): raise IntegrityError("block required")
        total_events+=len(b.events)
        if total_events>max_events: raise IntegrityError("total inventory event bound")
        previous=headers.get(b.height)
        header=(b.block_hash,b.parent_hash,b.timestamp)
        if previous is not None and previous != header: raise IntegrityError("committed header disagreement: quarantine")
        fact_set=frozenset(b.events)
        if b.height in complete_sets and complete_sets[b.height]!=fact_set:
            raise IntegrityError("complete block event inventory disagreement")
        complete_sets[b.height]=fact_set
        headers[b.height]=header
        for e in b.events:
            txkey=(e.height,e.tx_index)
            if txkey in tx_positions and tx_positions[txkey]!=e.tx_hash:
                raise IntegrityError("transaction index/hash disagreement")
            tx_positions[txkey]=e.tx_hash
            if e.tx_hash in tx_hashes and tx_hashes[e.tx_hash]!=(e.height,e.tx_index,e.block_hash):
                raise IntegrityError("transaction hash position disagreement")
            tx_hashes[e.tx_hash]=(e.height,e.tx_index,e.block_hash)
            logkey=(e.height,e.log_index)
            if logkey in log_positions and log_positions[logkey]!=e.key:
                raise IntegrityError("global block log index collision")
            log_positions[logkey]=e.key
            if e.key in events and events[e.key]!=e: raise IntegrityError("event equivocation")
            if e.position in positions and positions[e.position]!=e.key: raise IntegrityError("canonical position collision")
            events[e.key]=e; positions[e.position]=e.key
    if not headers: raise IntegrityError("empty inventory has no completeness proof")
    last_time=-1
    for h,header in sorted(headers.items()):
        if h != start_height or header[1]!=parent_hash or header[2]<last_time: raise IntegrityError("gap, parent or timestamp disagreement")
        parent_hash=header[0]; start_height+=1; last_time=header[2]
    ordered=tuple(sorted(events.values(),key=lambda e:e.position))
    prior={}
    for e in ordered:
        if e.height in prior and e.log_index<=prior[e.height]:
            raise IntegrityError("global log order disagrees with transaction order")
        prior[e.height]=e.log_index
    return ordered

@dataclass
class Position:
    quantity: int = 0
    cost: int = 0
    realized: int = 0
    unknown: int = 0

class Ledger:
    """Weighted remaining-cost raw integer oracle; partial disposal floors cost.
    Full disposal consumes all residual cost. Transfers introduce unknown basis;
    known/unknown attribution on later disposal requires explicit quantities.
    Fees must be normalized into actual received tokens / actual USDL debit or credit.
    """
    def __init__(self): self.positions={}; self.seen={}
    def apply(self,e):
        if e.key in self.seen:
            if self.seen[e.key]!=e: raise IntegrityError("ledger event conflict")
            return
        key=(e.inventory_owner,e.pool)
        p=self.positions.get(key,Position())
        if e.side=="sell" and (p.unknown or e.token_raw>p.quantity):
            raise IntegrityError("unknown basis or insufficient known inventory")
        if e.side=="buy":
            p=Position(p.quantity+e.token_raw,p.cost+e.usdl_raw,p.realized,p.unknown)
        else:
            allocated=p.cost if e.token_raw==p.quantity else p.cost*e.token_raw//p.quantity
            p=Position(p.quantity-e.token_raw,p.cost-allocated,p.realized+e.usdl_raw-allocated,p.unknown)
        integer(p.quantity+p.unknown)
        self.positions[key]=p; self.seen[e.key]=e
    def transfer_in(self,wallet,pool,quantity):
        identity(wallet,40); identity(pool,40); integer(quantity)
        p=self.positions.get((wallet,pool),Position())
        integer(p.quantity+p.unknown+quantity)
        self.positions[(wallet,pool)]=Position(p.quantity,p.cost,p.realized,p.unknown+quantity)
    def unrealized(self,wallet,pool,price):
        p=self.positions[(wallet,pool)]
        if not isinstance(price,Fraction) or price<0: raise IntegrityError("nonnegative exact raw-unit price required")
        if p.unknown: return None
        return (p.quantity*price.numerator)//price.denominator-p.cost


def candles(events,max_events=100000):
    """Exact human USDL/token ratios; UTC half-open, Monday weekly anchor.
    No carry bars, float math, timer revisions, or uncommitted publications.
    """
    integer(max_events,100000)
    bounded=[]
    for e in events:
        if len(bounded)>=max_events: raise IntegrityError("candle input bound")
        if not isinstance(e,Event): raise IntegrityError("normalized event required")
        bounded.append(e)
    buckets={}; seen={}; decimals={}
    for e in sorted(bounded,key=lambda e:e.position):
        if e.key in seen:
            if seen[e.key]!=e: raise IntegrityError("candle event conflict")
            continue
        seen[e.key]=e
        if e.pool in decimals and decimals[e.pool]!=e.token_decimals: raise IntegrityError("token decimal disagreement")
        decimals[e.pool]=e.token_decimals
        price=Fraction(e.usdl_raw*10**e.token_decimals,e.token_raw*10**6)
        for interval,seconds in INTERVALS.items():
            anchor=345600 if interval=="1w" else 0
            start=(e.timestamp-anchor)//seconds*seconds+anchor
            key=(e.pool,interval,start)
            if key not in buckets:
                buckets[key]={"open":price,"high":price,"low":price,"close":price,"token_raw":0,"usdl_raw":0,"buy_usdl_raw":0,"sell_usdl_raw":0,"trades":0,"first":e.position,"last":e.position}
            b=buckets[key]; b["high"]=max(b["high"],price); b["low"]=min(b["low"],price); b["close"]=price
            b["token_raw"]+=e.token_raw; b["usdl_raw"]+=e.usdl_raw; b[e.side+"_usdl_raw"]+=e.usdl_raw; b["trades"]+=1; b["last"]=e.position
    return buckets

def checksum(value):
    def encode(x):
        if isinstance(x,Fraction): return {"numerator":str(x.numerator),"denominator":str(x.denominator)}
        if isinstance(x,dict): return [[encode(k),encode(v)] for k,v in sorted(x.items(),key=lambda kv:repr(kv[0]))]
        if isinstance(x,(tuple,list)): return [encode(i) for i in x]
        if isinstance(x,Position): return encode(asdict(x))
        if type(x) is int: return str(x)
        return x
    return hashlib.sha256(json.dumps(encode(value),separators=(",",":"),sort_keys=True).encode()).hexdigest()
