#!/usr/bin/env bash
# Runs snapshot.py over two overlapping CSV sources and a local JSON-RPC
# balanceOf responder, then checks the holder list shape and totals.
set -euo pipefail
here="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
work="$(mktemp -d)"; trap 'kill "${rpc_pid:-}" 2>/dev/null || true; rm -rf "$work"' EXIT

A=0x00000000000000000000000000000000000000aa
B=0x00000000000000000000000000000000000000bb
C=0x00000000000000000000000000000000000000cc
Z=0x0000000000000000000000000000000000000000
header=transaction_hash,log_index,from_address,to_address,amount
printf '%s\n0x01,0,%s,%s,1000\n0x02,3,%s,%s,400\n' "$header" $Z $A $A $B > "$work/old.csv"
printf '%s\n0x02,3,%s,%s,400\n0x03,1,%s,%s,100\n' "$header" $A $B $B $C > "$work/explorer.csv"

out="$(python3 "$here/snapshot.py" --source "$work/old.csv" --source "$work/explorer.csv" --out "$work/h.json")"
[ "$out" = "holders 3 sum 1000" ] || { echo "unexpected summary: $out"; exit 1; }
python3 - "$work/h.json" <<PY
import json, sys
h = json.load(open(sys.argv[1]))
assert isinstance(h, list) and all(set(e) == {"address", "balance"} for e in h), h
assert all(isinstance(e["address"], str) and len(e["address"]) == 42 and isinstance(e["balance"], str) for e in h), h
assert {e["address"]: int(e["balance"]) for e in h} == {"$A": 600, "$B": 300, "$C": 100}, h
PY

# Conflicting sources are refused.
printf '%s\n0x02,3,%s,%s,401\n' "$header" $A $B > "$work/bad.csv"
if python3 "$here/snapshot.py" --source "$work/old.csv" --source "$work/bad.csv" --out "$work/x.json" 2>/dev/null; then
    echo "conflicting sources accepted"; exit 1
fi

# The chain wins: B reads 250 on chain, C reads 0 and drops out.
port_file="$work/port"
python3 - "$port_file" "$B" <<'PY' &
import http.server, json, sys
port_file, b = sys.argv[1], sys.argv[2]
chain = {"aa": 600, b[-2:]: 250, "cc": 0}
class H(http.server.BaseHTTPRequestHandler):
    def do_POST(self):
        req = json.loads(self.rfile.read(int(self.headers["content-length"])))
        holder = req["params"][0]["data"][-2:]
        body = json.dumps({"jsonrpc": "2.0", "id": req["id"], "result": hex(chain[holder])}).encode()
        self.send_response(200); self.send_header("content-length", str(len(body))); self.end_headers()
        self.wfile.write(body)
    def log_message(self, *a): pass
srv = http.server.HTTPServer(("127.0.0.1", 0), H)
open(port_file, "w").write(str(srv.server_port))
srv.serve_forever()
PY
rpc_pid=$!
for _ in $(seq 50); do [ -s "$port_file" ] && break; sleep 0.1; done
out="$(python3 "$here/snapshot.py" --source "$work/old.csv" --source "$work/explorer.csv" \
    --rpc "http://127.0.0.1:$(cat "$port_file")" --out "$work/r.json" 2>/dev/null)"
[ "$out" = "holders 2 sum 850" ] || { echo "unexpected reconciled summary: $out"; exit 1; }
echo "sid-snapshot ok"
