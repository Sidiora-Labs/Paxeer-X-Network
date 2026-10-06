#!/usr/bin/env bash
# usage: tools/bringup/validator-firewall.sh
#        tools/bringup/validator-firewall.sh render xweb|guard [<peer address>...]
#        tools/bringup/validator-firewall.sh state xweb|guard [<peer address>...]
#
# Adopts two nft tables on every VALIDATOR_HOSTS destination:
#   xweb   table inet xweb from /etc/xweb-firewall.nft and xweb-firewall.service:
#          tcp 8480 and 8481 (the x-websearch attestor ports) are accepted
#          from loopback and from the other validator hosts and rejected with
#          tcp reset from every other source.
#   guard  table inet paxeer_validator_guard from
#          /etc/paxeer-validator-guard.nft and paxeer-validator-guard.service:
#          the API ports 1317, 8545, 8546, 9090 and 26657 and the Docker
#          postgres ports 5433 and 5455 are dropped from every source but
#          loopback and the Docker bridges, in a prerouting chain at priority
#          -150 so a Docker-published port is closed before its DNAT.
# No other port is touched: 22 and the p2p port 26656 stay open to the
# internet. When the loaded rules of a table already behave so the host is
# left as it is; otherwise its file is rewritten to exactly those rules,
# checked with nft -c, and its unit alone is (installed and) restarted.
# Prints one line per host and table, never an address:
#   "VALIDATOR_HOSTS[k] table=<t> rules=match action=none"
#   "VALIDATOR_HOSTS[k] table=<t> rules=match action=rewritten was=<state>"
#   "VALIDATOR_HOSTS[k] table=<t> rules=<state> action=<none|rewrite=<exit>>"
#   "VALIDATOR_HOSTS[k] ssh=<exit>"
# render prints the file of one table for a host whose peers are the given
# addresses; state reads nft -j list table output on stdin and prints match
# or the first failing observation. Neither reads the host map.
# Exits 0 only when every host ends with matching rules, 2 on a usage error.
set -euo pipefail

# shellcheck source=tools/bringup/check-live.sh
. "$(dirname "${BASH_SOURCE[0]}")/check-live.sh"

usage() {
	sed -n '2,/^set -euo/p' "${BASH_SOURCE[0]}" | sed '$d' | sed 's/^# \{0,1\}//' >&2
	exit 2
}
mode=adopt
case "${1:-}:${2:-}" in
:) [ "$#" -eq 0 ] || usage ;;
render:xweb | render:guard | state:xweb | state:guard) mode="$1" ;;
*) usage ;;
esac

attestor_ports="8480 8481"
guard_ports="1317 8545 8546 9090 26657 5433 5455"

# host_addr <destination>: the address ssh connects to for the destination.
host_addr() {
	ssh -G -- "$1" 2>/dev/null | awk '$1 == "hostname" { print $2; exit }'
}

# rule_state xweb|guard <peer addresses>: reads nft -j list table on stdin
# and prints "match" when, for an IPv4 tcp packet from another source to each
# port of the table, xweb rejects with tcp reset and guard drops or rejects,
# loopback (and for xweb every peer address) is accepted, and ports 22 and
# 26656 from any other source are not refused; otherwise the first failing
# observation, such as absent, priority=0, unknown-expression, lo-8480=reject
# or other-5433=accept.
rule_state() {
	local ports="$attestor_ports"
	[ "$1" = xweb ] || ports="$guard_ports"
	python3 -c '
import ipaddress
import json
import sys

kind = sys.argv[1]
peers = sys.argv[2].split()
ports = [int(p) for p in sys.argv[3].split()]
hook, below = ("input", 0) if kind == "xweb" else ("prerouting", -100)
text = sys.stdin.read()
try:
    doc = json.loads(text)
except ValueError:
    print("absent")
    sys.exit(0)

chains, rules = [], {}
for item in doc.get("nftables", []):
    if "chain" in item and item["chain"].get("hook") == hook:
        chains.append(item["chain"])
    elif "rule" in item:
        rules.setdefault(item["rule"]["chain"], []).append(item["rule"]["expr"])
if not chains:
    print("absent")
    sys.exit(0)
chains.sort(key=lambda c: c.get("prio", 0))
if chains[0].get("prio", 0) >= below:
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
if kind == "guard":
    checks[0] = ("lo", checks[0][1], ports + [22, 26656], "accept")
checks.append(("other", {"iif": "eth0", "saddr": other}, ports, "reset" if kind == "xweb" else "closed"))
checks.append(("other", {"iif": "eth0", "saddr": other}, [22, 26656], "accept"))
try:
    for name, packet, check_ports, want in checks:
        for port in check_ports:
            got = verdict(dict(packet, port=port))
            if got != want and not (want == "closed" and got != "accept"):
                print(name + "-" + str(port) + "=" + got)
                sys.exit(0)
except (Unknown, KeyError, TypeError, ValueError):
    print("unknown-expression")
    sys.exit(0)
print("match")
' "$1" "$2" "$ports"
}

# rules_file xweb|guard <peer addresses>: the exact content of the table's
# file; the leading table and delete lines make nft -f replace the table in
# one transaction however often it is loaded.
rules_file() {
	if [ "$1" = xweb ]; then
		local ports="${attestor_ports// /, }" peers="${2// /, }"
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
		return
	fi
	cat <<EOF
table inet paxeer_validator_guard
delete table inet paxeer_validator_guard
table inet paxeer_validator_guard {
	chain prerouting {
		type filter hook prerouting priority -150; policy accept;
		iifname "lo" accept
		iifname "docker0" accept
		iifname "br-*" accept
		tcp dport { ${guard_ports// /, } } drop
	}
}
EOF
}

table_name() { if [ "$1" = xweb ]; then echo xweb; else echo paxeer_validator_guard; fi; }

if [ "$mode" != adopt ]; then
	kind="$2"
	shift 2
	if [ "$mode" = render ]; then
		[ "$kind" = guard ] || [ "$#" -gt 0 ] || usage
		rules_file "$kind" "$*"
	else
		rule_state "$kind" "$*"
	fi
	exit 0
fi

for tool in ssh timeout python3; do
	if ! command -v "$tool" >/dev/null 2>&1; then
		echo "validator-firewall: $tool is required" >&2
		exit 2
	fi
done
load_hosts

# Each file is staged beside the live one, checked by nft -c and moved into
# place before its unit restarts, so a bad file never reaches the loaded
# table. The guard unit is (re)written on every install.
# shellcheck disable=SC2016
stage='t=$(mktemp "$f.XXXXXX") && cat >"$t" && nft -c -f "$t" && chmod 0644 "$t" && mv "$t" "$f"'
# shellcheck disable=SC2016
install_xweb="f=/etc/xweb-firewall.nft && { $stage"' && systemctl restart xweb-firewall.service || { rm -f "$t"; exit 1; }; }'
# shellcheck disable=SC2016
install_guard="f=/etc/paxeer-validator-guard.nft && { $stage"' && printf "[Unit]\nDescription=Paxeer validator port guard\nBefore=network-pre.target docker.service\nWants=network-pre.target\n\n[Service]\nType=oneshot\nRemainAfterExit=yes\nExecStart=/usr/sbin/nft -f /etc/paxeer-validator-guard.nft\n\n[Install]\nWantedBy=multi-user.target\n" >/etc/systemd/system/paxeer-validator-guard.service && systemctl daemon-reload && systemctl enable --quiet paxeer-validator-guard.service && systemctl restart paxeer-validator-guard.service || { rm -f "$t"; exit 1; }; }'

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
	for kind in xweb guard; do
		read_cmd="nft -j list table inet $(table_name "$kind") 2>/dev/null || true"
		install_cmd="install_$kind"
		status=0
		loaded="$(ssh_read "${dests[$k]}" "$read_cmd")" || status=$?
		if [ "$status" -ne 0 ]; then
			echo "VALIDATOR_HOSTS[$k] ssh=$status"
			failures=$((failures + 1))
			break
		fi
		state="$(rule_state "$kind" "$peers" <<<"$loaded")"
		if [ "$state" = match ]; then
			echo "VALIDATOR_HOSTS[$k] table=$kind rules=match action=none"
			continue
		fi
		status=0
		rules_file "$kind" "$peers" | timeout "$timeout" ssh -o BatchMode=yes -- "${dests[$k]}" "${!install_cmd}" >/dev/null 2>&1 || status=$?
		if [ "$status" -ne 0 ]; then
			echo "VALIDATOR_HOSTS[$k] table=$kind rules=$state action=rewrite=$status"
			failures=$((failures + 1))
			continue
		fi
		loaded="$(ssh_read "${dests[$k]}" "$read_cmd")" || true
		after="$(rule_state "$kind" "$peers" <<<"$loaded")"
		if [ "$after" = match ]; then
			echo "VALIDATOR_HOSTS[$k] table=$kind rules=match action=rewritten was=$state"
		else
			echo "VALIDATOR_HOSTS[$k] table=$kind rules=$after action=rewritten was=$state"
			failures=$((failures + 1))
		fi
	done
done
[ "$failures" -eq 0 ]
