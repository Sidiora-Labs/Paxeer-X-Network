#!/usr/bin/env bash
# usage: tools/bringup/validator-firewall.sh
#
# Adopts on every VALIDATOR_HOSTS destination the table inet xweb that
# /etc/xweb-firewall.nft and xweb-firewall.service install: tcp 8480 and 8481
# (the x-websearch attestor ports) are accepted from loopback and from the
# other validator hosts and rejected with tcp reset from every other source,
# and no other port is touched. When the loaded rules already behave so the
# host is left as it is; otherwise the file is rewritten to exactly those
# rules, checked with nft -c, and xweb-firewall.service alone is restarted.
# Prints one line per host, never an address:
#   "VALIDATOR_HOSTS[k] rules=match action=none"
#   "VALIDATOR_HOSTS[k] rules=match action=rewritten was=<state>"
#   "VALIDATOR_HOSTS[k] rules=<state> action=<none|rewrite=<exit>>"
#   "VALIDATOR_HOSTS[k] ssh=<exit>"
# Exits 0 only when every host ends with matching rules, 2 on a usage error.
set -euo pipefail

# shellcheck source=tools/bringup/check-live.sh
. "$(dirname "${BASH_SOURCE[0]}")/check-live.sh"

if [ "$#" -ne 0 ]; then
	sed -n '2,/^set -euo/p' "${BASH_SOURCE[0]}" | sed '$d' | sed 's/^# \{0,1\}//' >&2
	exit 2
fi
for tool in ssh timeout python3; do
	if ! command -v "$tool" >/dev/null 2>&1; then
		echo "validator-firewall: $tool is required" >&2
		exit 2
	fi
done
load_hosts

attestor_ports="8480 8481"

# host_addr <destination>: the address ssh connects to for the destination.
host_addr() {
	ssh -G -- "$1" 2>/dev/null | awk '$1 == "hostname" { print $2; exit }'
}

# rule_state <peer addresses>: reads nft -j list table inet xweb on stdin and
# prints "match" when, for an IPv4 tcp packet to each attestor port, loopback
# and every peer address are accepted and any other source is rejected with
# tcp reset, while ports 22 and 26656 from any other source are not refused;
# otherwise the first failing observation, such as absent, unknown-expression,
# lo-8480=reject or other-8481=accept.
rule_state() {
	python3 -c '
import ipaddress
import json
import sys

peers = sys.argv[1].split()
ports = [int(p) for p in sys.argv[2].split()]
text = sys.stdin.read()
try:
    doc = json.loads(text)
except ValueError:
    print("absent")
    sys.exit(0)

chains, rules = [], {}
for item in doc.get("nftables", []):
    if "chain" in item and item["chain"].get("hook") == "input":
        chains.append(item["chain"])
    elif "rule" in item:
        rules.setdefault(item["rule"]["chain"], []).append(item["rule"]["expr"])
if not chains:
    print("absent")
    sys.exit(0)
chains.sort(key=lambda c: c.get("prio", 0))
if chains[0].get("prio", 0) >= 0:
    print("priority=" + str(chains[0].get("prio")))
    sys.exit(0)

class Unknown(Exception):
    pass

def contains(right, value):
    items = right["set"] if isinstance(right, dict) and "set" in right else [right]
    for item in items:
        if isinstance(item, dict) and "prefix" in item:
            net = ipaddress.ip_network(item["prefix"]["addr"] + "/" + str(item["prefix"]["len"]), strict=False)
            if isinstance(value, ipaddress.IPv4Address) and value in net:
                return True
        elif isinstance(item, dict) and "range" in item:
            low, high = item["range"]
            if isinstance(value, int) and low <= value <= high:
                return True
            if isinstance(value, ipaddress.IPv4Address) and ipaddress.ip_address(low) <= value <= ipaddress.ip_address(high):
                return True
        elif isinstance(item, dict):
            raise Unknown()
        elif isinstance(value, ipaddress.IPv4Address):
            if ipaddress.ip_address(item) == value:
                return True
        elif item == value:
            return True
    return False

def field(left, packet):
    if "payload" in left:
        p = left["payload"]
        if p.get("protocol") in ("tcp", "th") and p.get("field") == "dport":
            return packet["port"]
        if p.get("protocol") == "ip" and p.get("field") == "saddr":
            return packet["saddr"]
        if p.get("protocol") in ("ip6", "udp", "icmp", "icmpv6"):
            return None
    if "meta" in left:
        key = left["meta"].get("key")
        if key in ("iif", "iifname"):
            return packet["iif"]
        if key == "l4proto":
            return "tcp"
        if key == "nfproto":
            return "ipv4"
    raise Unknown()

def verdict(packet):
    for chain in chains:
        for expr in rules.get(chain["name"], []):
            hit, action = True, None
            for e in expr:
                if "match" in e:
                    m = e["match"]
                    value = field(m["left"], packet)
                    if value is None:
                        hit = False
                        break
                    found = contains(m["right"], value)
                    if m["op"] in ("==", "in"):
                        hit = found
                    elif m["op"] == "!=":
                        hit = not found
                    else:
                        raise Unknown()
                    if not hit:
                        break
                elif "accept" in e:
                    action = "accept"
                elif "drop" in e:
                    action = "drop"
                elif "reject" in e:
                    kind = (e["reject"] or {}).get("type")
                    action = "reset" if kind == "tcp reset" else "reject"
                elif "counter" in e or "log" in e:
                    continue
                else:
                    raise Unknown()
            if hit and action:
                if action != "accept":
                    return action
                break
        else:
            if chain.get("policy") == "drop":
                return "drop"
    return "accept"

other = ipaddress.ip_address("192.0.2.1")
checks = [("lo", {"iif": "lo", "saddr": ipaddress.ip_address("127.0.0.1")}, ports, "accept")]
for n, peer in enumerate(peers):
    checks.append(("peer" + str(n), {"iif": "eth0", "saddr": ipaddress.ip_address(peer)}, ports, "accept"))
checks.append(("other", {"iif": "eth0", "saddr": other}, ports, "reset"))
checks.append(("other", {"iif": "eth0", "saddr": other}, [22, 26656], "accept"))
try:
    for name, packet, check_ports, want in checks:
        for port in check_ports:
            got = verdict(dict(packet, port=port))
            if got != want:
                print(name + "-" + str(port) + "=" + got)
                sys.exit(0)
except (Unknown, KeyError, TypeError, ValueError):
    print("unknown-expression")
    sys.exit(0)
print("match")
' "$1" "$attestor_ports"
}

# rules_file <peer addresses>: the exact /etc/xweb-firewall.nft content; the
# leading table and delete lines make nft -f replace the table in one
# transaction however often it is loaded.
rules_file() {
	local ports="${attestor_ports// /, }" peers="${1// /, }"
	cat <<EOF
table inet xweb
delete table inet xweb
table inet xweb {
	chain input {
		type filter hook input priority -10; policy accept;
		iif "lo" tcp dport { $ports } accept
		ip saddr { $peers } tcp dport { $ports } accept
		tcp dport { $ports } reject with tcp reset
	}
}
EOF
}

# The file is staged beside the live one, checked by nft -c and moved into
# place before the restart, so a bad file never reaches the loaded table.
# shellcheck disable=SC2016
install_cmd='t=$(mktemp /etc/xweb-firewall.nft.XXXXXX) && cat >"$t" && nft -c -f "$t" && chmod 0644 "$t" && mv "$t" /etc/xweb-firewall.nft && systemctl restart xweb-firewall.service || { rm -f "$t"; exit 1; }'
read_cmd='nft -j list table inet xweb 2>/dev/null || true'

read -r -a dests <<<"$VALIDATOR_HOSTS"
addrs=()
for k in "${!dests[@]}"; do
	addrs[k]="$(host_addr "${dests[$k]}")"
done

failures=0
for k in "${!dests[@]}"; do
	peers=""
	for j in "${!dests[@]}"; do
		[ "$j" -eq "$k" ] || peers="$peers ${addrs[$j]}"
	done
	peers="${peers# }"
	status=0
	loaded="$(ssh_read "${dests[$k]}" "$read_cmd")" || status=$?
	if [ "$status" -ne 0 ]; then
		echo "VALIDATOR_HOSTS[$k] ssh=$status"
		failures=$((failures + 1))
		continue
	fi
	state="$(rule_state "$peers" <<<"$loaded")"
	if [ "$state" = match ]; then
		echo "VALIDATOR_HOSTS[$k] rules=match action=none"
		continue
	fi
	status=0
	rules_file "$peers" | timeout "$timeout" ssh -o BatchMode=yes -- "${dests[$k]}" "$install_cmd" >/dev/null 2>&1 || status=$?
	if [ "$status" -ne 0 ]; then
		echo "VALIDATOR_HOSTS[$k] rules=$state action=rewrite=$status"
		failures=$((failures + 1))
		continue
	fi
	loaded="$(ssh_read "${dests[$k]}" "$read_cmd")" || true
	after="$(rule_state "$peers" <<<"$loaded")"
	if [ "$after" = match ]; then
		echo "VALIDATOR_HOSTS[$k] rules=match action=rewritten was=$state"
	else
		echo "VALIDATOR_HOSTS[$k] rules=$after action=rewritten was=$state"
		failures=$((failures + 1))
	fi
done
[ "$failures" -eq 0 ]
