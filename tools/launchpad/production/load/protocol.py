"""Frozen schema checks for load adapters; no invented production stream format."""
import json,hashlib
from pathlib import Path
from jsonschema import Draft202012Validator,FormatChecker
ROOT=Path(__file__).resolve().parents[4]
CONTRACTS=ROOT/"launchpad/spec/production-system/integration/contracts/v1"
def validate(name,value):
    if name not in ("IF_SUBSCRIBE","IF_STREAM_EVENT","IF_STREAM_CONTROL"): raise ValueError("unsupported frozen message")
    schema=json.loads((CONTRACTS/"schemas"/(name+".json")).read_text())
    Draft202012Validator(schema,format_checker=FormatChecker()).validate(value)
    return value
def subscribe(topics,subscription_id="load-1",after_cursor=None):
    return validate("IF_SUBSCRIBE",dict(op="subscribe",subscriptionId=subscription_id,topics=topics,afterCursor=after_cursor))
