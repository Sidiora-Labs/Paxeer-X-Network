# File generated from our OpenAPI spec by Scalar. See README.md for details.

from typing import Dict

from pydantic import Field as FieldInfo

from .._models import BaseModel

__all__ = ["WsRuntimeStats", "Limits"]


class Limits(BaseModel):
    max_connections: int = FieldInfo(alias="maxConnections")

    subscribe_rate_limit: int = FieldInfo(alias="subscribeRateLimit")

    max_subs_per_socket: int = FieldInfo(alias="maxSubsPerSocket")

    backpressure_bytes: int = FieldInfo(alias="backpressureBytes")

    max_payload_bytes: int = FieldInfo(alias="maxPayloadBytes")

    message_rate_limit: int = FieldInfo(alias="messageRateLimit")

    message_burst: int = FieldInfo(alias="messageBurst")

    global_message_rate_limit: int = FieldInfo(alias="globalMessageRateLimit")

    global_message_burst: int = FieldInfo(alias="globalMessageBurst")


class WsRuntimeStats(BaseModel):
    snapshot_id: str = FieldInfo(alias="snapshotId")

    total_clients: int = FieldInfo(alias="totalClients")

    rejected_connections: int = FieldInfo(alias="rejectedConnections")

    total_broadcasts_sent: int = FieldInfo(alias="totalBroadcastsSent")

    total_broadcasts_dropped: int = FieldInfo(alias="totalBroadcastsDropped")

    incoming_messages: int = FieldInfo(alias="incomingMessages")

    message_rate_limit_rejections: int = FieldInfo(alias="messageRateLimitRejections")

    binary_message_rejections: int = FieldInfo(alias="binaryMessageRejections")

    oversized_message_rejections: int = FieldInfo(alias="oversizedMessageRejections")

    topics: Dict[str, int]

    topic_seq: Dict[str, int] = FieldInfo(alias="topicSeq")

    limits: Limits
