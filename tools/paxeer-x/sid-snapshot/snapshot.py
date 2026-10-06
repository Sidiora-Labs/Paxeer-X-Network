#!/usr/bin/env python3
"""Build the SID holder list (address, balance) from explorer token transfers."""
import argparse
import csv
import json
import sys
import urllib.request

SID = "0x21f7b20a555199fa73a238b1a91fd0f549068fee"
ZERO = "0x" + "0" * 40
QUERY = """
SELECT '0x' || encode(transaction_hash, 'hex'), log_index,
       '0x' || encode(from_address_hash, 'hex'), '0x' || encode(to_address_hash, 'hex'), amount
FROM token_transfers WHERE token_contract_address_hash = decode(%s, 'hex')
"""


def read_source(source):
    """Yields (tx_hash, log_index, from, to, amount) from a Postgres DSN or a CSV path."""
    if source.startswith(("postgres://", "postgresql://")):
        import psycopg2

        with psycopg2.connect(source) as conn, conn.cursor() as cur:
            cur.execute(QUERY, (SID[2:],))
            for tx, idx, frm, to, amount in cur:
                yield tx.lower(), int(idx), frm.lower(), to.lower(), int(amount)
        return
    with open(source, newline="") as f:
        for row in csv.DictReader(f):
            yield (row["transaction_hash"].lower(), int(row["log_index"]), row["from_address"].lower(),
                   row["to_address"].lower(), int(row["amount"]))


def balances(sources):
    seen = {}
    for source in sources:
        for tx, idx, frm, to, amount in read_source(source):
            prev = seen.setdefault((tx, idx), (frm, to, amount))
            if prev != (frm, to, amount):
                sys.exit(f"sources disagree on transfer {tx}:{idx}: {prev} vs {(frm, to, amount)}")
    held = {}
    for frm, to, amount in seen.values():
        held[frm] = held.get(frm, 0) - amount
        held[to] = held.get(to, 0) + amount
    held.pop(ZERO, None)
    negative = {a: b for a, b in held.items() if b < 0}
    if negative:
        sys.exit(f"negative balances, transfer history is incomplete: {negative}")
    return {a: b for a, b in held.items() if b > 0}


def balance_of(rpc, holder):
    data = "0x70a08231" + holder[2:].rjust(64, "0")
    body = json.dumps({"jsonrpc": "2.0", "id": 1, "method": "eth_call",
                       "params": [{"to": SID, "data": data}, "latest"]}).encode()
    req = urllib.request.Request(rpc, body, {"content-type": "application/json"})
    with urllib.request.urlopen(req, timeout=30) as resp:
        reply = json.load(resp)
    if "result" not in reply:
        sys.exit(f"balanceOf({holder}) failed: {reply.get('error')}")
    return int(reply["result"], 16)


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--source", action="append", required=True,
                    help="explorer Postgres DSN or CSV path (repeat for the old paxscan copy)")
    ap.add_argument("--rpc", help="JSON-RPC URL; on-chain balanceOf wins over transfer history")
    ap.add_argument("--out", required=True, help="holder list JSON path")
    args = ap.parse_args()

    held = balances(args.source)
    if args.rpc:
        for holder in sorted(held):
            onchain = balance_of(args.rpc, holder)
            if onchain != held[holder]:
                print(f"mismatch {holder}: transfers {held[holder]} chain {onchain}", file=sys.stderr)
                held[holder] = onchain
        held = {a: b for a, b in held.items() if b > 0}

    holders = [{"address": a, "balance": str(b)} for a, b in sorted(held.items())]
    with open(args.out, "w") as f:
        json.dump(holders, f, indent=2)
        f.write("\n")
    print(f"holders {len(holders)} sum {sum(held.values())}")


if __name__ == "__main__":
    main()
