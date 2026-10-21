import json
from dataclasses import asdict
from fractions import Fraction
from pathlib import Path

from oracle import Block, Event, IntegrityError, Ledger, candles, checksum, inventory


def evaluate_fixture(document):
    if document.get("evidence_level") != "synthetic_mathematical":
        raise IntegrityError("fixture cannot establish runtime evidence")
    blocks = []
    for raw in document["blocks"]:
        header = {key: value for key, value in raw.items() if key != "events"}
        blocks.append(Block(**header, events=tuple(Event(**e) for e in raw["events"])))
    ordered = inventory(blocks, document["start_height"], document["parent_hash"])
    ledger = Ledger()
    for event in ordered:
        ledger.apply(event)
    positions = [
        {"wallet": wallet, "pool": pool, **asdict(position)}
        for (wallet, pool), position in sorted(ledger.positions.items())
    ]
    bars = []
    for (pool, interval, opened), bar in sorted(candles(ordered).items()):
        prices = {
            key: {"numerator": bar[key].numerator, "denominator": bar[key].denominator}
            for key in ("open", "high", "low", "close")
        }
        bars.append({"pool": pool, "interval": interval, "opened": opened, **prices,
                     **{key: value for key, value in bar.items()
                        if key not in prices and key not in ("first", "last")},
                     "first": list(bar["first"]), "last": list(bar["last"])})
    return {"order": [list(e.position) for e in ordered],
            "positions": positions, "candles": bars}


def verify_inventory(path):
    raw = Path(path).read_bytes()
    if len(raw) > 1048576:
        raise IntegrityError("fixture inventory byte bound")
    document = json.loads(raw)
    if document.get("version") != 1 or document.get("production_qualified") is not False:
        raise IntegrityError("invalid primitive fixture inventory")
    fixtures = document["fixtures"]
    if len(fixtures) != 3 or {f["id"] for f in fixtures} != {
        "remaining-basis", "partial-fees-recipient", "same-second-order"
    }:
        raise IntegrityError("missing or duplicate canonical fixture")
    results = []
    for fixture in fixtures:
        actual = evaluate_fixture(fixture)
        if actual != fixture["expected"]:
            raise IntegrityError("independent expected-state mismatch: " + fixture["id"])
        results.append({"id": fixture["id"], "expected_checksum": checksum(actual)})
    return {"production_qualified": False, "evidence_level": "synthetic_mathematical",
            "fixtures": results}


if __name__ == "__main__":
    print(json.dumps(verify_inventory(Path(__file__).with_name("canonical-fixtures.json")),
                     sort_keys=True, separators=(",", ":")))
