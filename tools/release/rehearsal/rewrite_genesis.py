#!/usr/bin/env python3
import argparse
import base64
import binascii
import copy
import hashlib
import json
from pathlib import Path
import runpy
import sys
from datetime import datetime, timezone

_CODEC = runpy.run_path(
    str(
        Path(__file__).resolve().parents[3] / "platform/hosted/paxeer/anchor-genesis.py"
    )
)
BECH32 = _CODEC["bech32"]
PUBKEY_TYPE = "tendermint/PubKeyEd25519"
SDK_PUBKEY_TYPE = "/cosmos.crypto.ed25519.PubKey"


def require(condition, message):
    if not condition:
        raise ValueError(message)


def integer(value, name, minimum=0, maximum=(1 << 63) - 1):
    require(
        isinstance(value, str) and value.isascii() and value.isdecimal(),
        name + " must be a decimal string",
    )
    number = int(value)
    require(minimum <= number <= maximum, name + " is out of range")
    return number


def public_key(value):
    require(isinstance(value, str), "public key must be base64 text")
    try:
        raw = base64.b64decode(value, validate=True)
    except (ValueError, binascii.Error) as error:
        raise ValueError("public key must be canonical base64") from error
    require(
        len(raw) == 32 and base64.b64encode(raw).decode() == value,
        "public key must encode exactly 32 bytes",
    )
    address = hashlib.sha256(raw).digest()[:20]
    return address.hex().upper(), BECH32("paxvalcons", address)


def indexed(entries, field, name):
    require(isinstance(entries, list), name + " must be an array")
    result = {}
    for entry in entries:
        require(isinstance(entry, dict), name + " entries must be objects")
        value = entry[field]
        require(isinstance(value, str) and value, name + " address is missing")
        require(value not in result, name + " contains duplicate addresses")
        result[value] = entry
    return result


def rewrite(exported, keys):
    require(isinstance(exported, dict), "export must be an object")
    require(isinstance(keys, dict), "keys must map operator addresses to public keys")
    result = copy.deepcopy(exported)
    app = result["app_state"]
    staking = app["staking"]
    height = str(integer(result["initial_height"], "initial height", 1))
    validators = indexed(staking["validators"], "operator_address", "validators")
    bonded = {
        operator: validator
        for operator, validator in validators.items()
        if validator["status"] == "BOND_STATUS_BONDED"
    }
    require(len(bonded) == 4, "export must contain exactly four bonded validators")
    require(set(keys) == set(bonded), "keys must name exactly the bonded operators")
    powers = indexed(staking["last_validator_powers"], "address", "last powers")
    require(set(powers) == set(bonded), "last powers must name the bonded operators")
    amounts = {integer(row["power"], "voting power", 1) for row in powers.values()}
    require(len(amounts) == 1, "bonded validators must have equal voting power")
    power = amounts.pop()
    require(4 * power <= ((1 << 63) - 1) // 8, "total voting power is out of range")

    old_addresses = {}
    for operator, validator in validators.items():
        pubkey = validator["consensus_pubkey"]
        require(pubkey["@type"] == SDK_PUBKEY_TYPE, "validator key must be Ed25519")
        _, consensus = public_key(pubkey["key"])
        require(consensus not in old_addresses, "export contains duplicate public keys")
        old_addresses[consensus] = operator

    replacements = {}
    new_addresses = set()
    comet_validators = []
    for operator in sorted(bonded):
        validator = bonded[operator]
        require(validator["jailed"] is False, "bonded validator must not be jailed")
        integer(validator["tokens"], "bonded tokens", 1, (1 << 256) - 1)
        key = keys[operator]
        require(isinstance(key, dict), "replacement key must be an object")
        pubkey = key["pub_key"]
        require(pubkey["type"] == PUBKEY_TYPE, "replacement key must be Ed25519")
        address, consensus = public_key(pubkey["value"])
        require(
            key["address"] == address, "replacement address does not match public key"
        )
        require(
            consensus not in new_addresses, "replacement public keys must be distinct"
        )
        require(consensus not in old_addresses, "replacement keys must be fresh")
        new_addresses.add(consensus)
        _, old = public_key(validator["consensus_pubkey"]["key"])
        replacements[old] = consensus
        validator["consensus_pubkey"] = {
            "@type": SDK_PUBKEY_TYPE,
            "key": pubkey["value"],
        }
        comet_validators.append(
            {
                "address": address,
                "pub_key": {"type": PUBKEY_TYPE, "value": pubkey["value"]},
                "power": str(power),
                "name": validator["description"]["moniker"],
            }
        )

    slashing = app["slashing"]
    infos = indexed(slashing["signing_infos"], "address", "signing infos")
    bitmaps = indexed(slashing["missed_blocks"], "address", "missed blocks")
    require(
        not new_addresses.intersection(set(infos) | set(bitmaps)),
        "replacement address already has slashing state",
    )
    for old, new in replacements.items():
        if old in infos:
            entry = infos.pop(old)
            info = entry["validator_signing_info"]
            require(info["address"] == old, "signing info address mismatch")
            require(
                info["tombstoned"] is False, "bonded validator must not be tombstoned"
            )
            info["address"] = new
            info["start_height"] = height
            entry["address"] = new
        else:
            require(old not in bitmaps, "missed blocks lack signing info")
            entry = {
                "address": new,
                "validator_signing_info": {
                    "address": new,
                    "start_height": height,
                    "index_offset": "0",
                    "jailed_until": datetime.fromtimestamp(0, timezone.utc)
                    .isoformat()
                    .replace("+00:00", "Z"),
                    "tombstoned": False,
                    "missed_blocks_counter": "0",
                },
            }
        infos[new] = entry
        if old in bitmaps:
            entry = bitmaps.pop(old)
            entry["address"] = new
            bitmaps[new] = entry
    slashing["signing_infos"] = [infos[address] for address in sorted(infos)]
    slashing["missed_blocks"] = [bitmaps[address] for address in sorted(bitmaps)]
    result["validators"] = comet_validators
    staking["last_validator_powers"] = [
        {"address": operator, "power": str(power)} for operator in sorted(bonded)
    ]
    staking["last_total_power"] = str(4 * power)
    app["bank"]["supply"] = []
    return result


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("exported", nargs="?", default="-")
    parser.add_argument(
        "--keys",
        required=True,
        help="JSON mapping bonded operator addresses to {address, pub_key} key records",
    )
    parser.add_argument("--output", default="-")
    args = parser.parse_args()
    try:
        with open(args.keys, encoding="utf-8") as source:
            keys = json.load(source)
        if args.exported == "-":
            exported = json.load(sys.stdin)
        else:
            with open(args.exported, encoding="utf-8") as source:
                exported = json.load(source)
        output = json.dumps(rewrite(exported, keys), indent=2) + "\n"
        if args.output == "-":
            sys.stdout.write(output)
        else:
            Path(args.output).write_text(output, encoding="utf-8")
    except (OSError, ValueError, KeyError, TypeError) as error:
        print("rewrite_genesis: " + str(error), file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
