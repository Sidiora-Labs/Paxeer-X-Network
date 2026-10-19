from __future__ import annotations

from typing import Literal, NotRequired, TypedDict

from .generated.client import (
    NativeActivityV1, NativeLocalGrantV1, NativePurposeV1, SignedNativePurposeV1,
    _native_activity, _native_decimal, _native_hex, _native_record, _native_text,
    _native_version,
)


class NativeEffectPrepareRequestV1(TypedDict):
    variant: Literal["native_effect_v1"]
    activity: NativeActivityV1
    actor: str
    authority: str
    account_sequence: str
    not_before: str
    not_after: str
    idempotency_key: str
    fee_limit: str
    payload: str
    payload_hash: str
    capability_id: str
    purpose: SignedNativePurposeV1
    local_grant: NotRequired[NativeLocalGrantV1 | None]


def encode_native_effect_prepare_request(
    value: NativeEffectPrepareRequestV1,
) -> NativeEffectPrepareRequestV1:
    item = _native_record(value, (
        "variant", "activity", "actor", "authority", "account_sequence",
        "not_before", "not_after", "idempotency_key", "fee_limit", "payload",
        "payload_hash", "capability_id", "purpose",
    ), ("local_grant",))
    if item["variant"] != "native_effect_v1":
        raise ValueError("native_effect_v1.variant")
    activity = _native_activity(item["activity"])
    if activity["module"] == "9":
        raise ValueError("native_effect_v1.activity")
    signed = _native_record(item["purpose"], ("purpose", "owner_public_key", "signature"))
    raw = _native_record(signed["purpose"], (
        "version", "tenant", "agent_did", "session_id", "generation",
        "expires_at_ms", "capability_id", "preparation_id", "canonical_digest",
        "commitment",
    ))
    purpose: NativePurposeV1 = {
        "version": _native_version(raw["version"]),
        "tenant": _native_text(raw["tenant"], 255),
        "agent_did": _native_text(raw["agent_did"], 255),
        "session_id": _native_hex(raw["session_id"], 32),
        "generation": _native_decimal(raw["generation"]),
        "expires_at_ms": _native_decimal(raw["expires_at_ms"]),
        "capability_id": _native_hex(raw["capability_id"], 32),
        "preparation_id": _native_hex(raw["preparation_id"], 32),
        "canonical_digest": _native_hex(raw["canonical_digest"], 32),
        "commitment": _native_hex(raw["commitment"], 32),
    }
    actor = _native_text(item["actor"], 255)
    capability_id = _native_hex(item["capability_id"], 32)
    not_before = _native_decimal(item["not_before"])
    not_after = _native_decimal(item["not_after"])
    if (actor != purpose["agent_did"] or capability_id != purpose["capability_id"]
        or purpose["generation"] == "0" or purpose["expires_at_ms"] == "0"
        or int(not_before) > int(not_after)):
        raise ValueError("native_effect_v1.binding")
    local_grant: NativeLocalGrantV1 | None = None
    if item.get("local_grant") is not None:
        grant = _native_record(item["local_grant"], (
            "version", "capability", "session_scope", "expires_at_ms",
            "owner_public_key", "signature",
        ))
        local_grant = {
            "version": _native_version(grant["version"]),
            "capability": _native_hex(grant["capability"], 1048576, False),
            "session_scope": _native_hex(grant["session_scope"], 1048576, False),
            "expires_at_ms": _native_decimal(grant["expires_at_ms"]),
            "owner_public_key": _native_hex(grant["owner_public_key"], 32),
            "signature": _native_hex(grant["signature"], 64),
        }
        if local_grant["expires_at_ms"] == "0":
            raise ValueError("native_effect_v1.expiry")
    return {
        "variant": "native_effect_v1", "activity": activity, "actor": actor,
        "authority": _native_text(item["authority"], 524288),
        "account_sequence": _native_decimal(item["account_sequence"]),
        "not_before": not_before, "not_after": not_after,
        "idempotency_key": _native_hex(item["idempotency_key"], 32),
        "fee_limit": _native_decimal(item["fee_limit"], (1 << 128) - 1),
        "payload": _native_hex(item["payload"], 524288, False),
        "payload_hash": _native_hex(item["payload_hash"], 32),
        "capability_id": capability_id,
        "purpose": {
            "purpose": purpose,
            "owner_public_key": _native_hex(signed["owner_public_key"], 32),
            "signature": _native_hex(signed["signature"], 64),
        },
        "local_grant": local_grant,
    }
