import json
from pathlib import Path
import sys

LEGACY_PARAMS = (
    "virtual_quote_default",
    "virtual_token_default",
    "min_fee_bps",
    "max_fee_bps",
    "base_fee_bps",
    "protocol_fee_bps",
    "fee_decay_rate",
    "volatility_weight",
    "concentration_weight",
    "creation_fee",
)


def fail(message):
    sys.exit(f"seed_halt_export: {message}")


def legacy_launchpad(launchpad):
    for field in ("markets", "airdrop_epochs", "airdrop_claims"):
        if launchpad[field] != []:
            fail(f"export launchpad.{field} is not empty")
    if launchpad["fee_held"] != "0":
        fail(f"export launchpad.fee_held is {launchpad['fee_held']!r}, not '0'")
    accounting = launchpad["fee_accounting"]
    if accounting != {
        "accounting_initialized": False,
        "accounting_migration_cursor": 0,
        "pending_protocol_fees": "0",
        "total_liabilities": "0",
    }:
        fail(f"export launchpad.fee_accounting is not zero: {accounting!r}")
    if launchpad["fee_rights"]["next_token_id"] != 0:
        fail(f"export launchpad.fee_rights.next_token_id is {launchpad['fee_rights']['next_token_id']!r}, not 0")
    params = launchpad["params"]
    pending = sum(int(entry["amount"]) for entry in launchpad["protocol_fees"])
    return {
        "params": {"quote_denom": "uhpx", **{name: params[name] for name in LEGACY_PARAMS}},
        "markets": [],
        "airdrop_epochs": [],
        "airdrop_claims": [],
        "protocol_fees_pending": str(pending),
    }


def merge_evm(evm, exported):
    replaced = {entry["address"].lower() for entry in exported["codes"]}
    evm["codes"] = [entry for entry in evm["codes"] if entry["address"].lower() not in replaced]
    evm["states"] = [entry for entry in evm["states"] if entry["address"].lower() not in replaced]
    codes = {entry["address"].lower() for entry in evm["codes"]}
    states = {(entry["address"].lower(), entry["key"].lower()) for entry in evm["states"]}
    for entry in exported["codes"]:
        address = entry["address"].lower()
        if address in codes:
            fail(f"evm code collides at {entry['address']}")
        codes.add(address)
    for entry in exported["states"]:
        slot = (entry["address"].lower(), entry["key"].lower())
        if slot in states:
            fail(f"evm state collides at {entry['address']} {entry['key']}")
        states.add(slot)
    evm["codes"].extend(exported["codes"])
    evm["states"].extend(exported["states"])
    evm["nonces"] = [entry for entry in evm.get("nonces", []) if entry["address"].lower() not in replaced]
    nonces = {entry["address"].lower() for entry in evm["nonces"]}
    for entry in exported["nonces"]:
        address = entry["address"].lower()
        if address in nonces:
            fail(f"evm nonce collides at {entry['address']}")
        nonces.add(address)
    evm["nonces"].extend(exported["nonces"])


def main():
    if len(sys.argv) != 3:
        fail("usage: seed_halt_export.py EXPORT GENESIS")
    export = json.loads(Path(sys.argv[1]).read_text())["app_state"]
    genesis_path = Path(sys.argv[2])
    genesis = json.loads(genesis_path.read_text())
    app_state = genesis["app_state"]
    merge_evm(app_state["evm"], export["evm"])
    app_state["launchpad"] = legacy_launchpad(export["launchpad"])
    genesis_path.write_text(json.dumps(genesis))


if __name__ == "__main__":
    main()
