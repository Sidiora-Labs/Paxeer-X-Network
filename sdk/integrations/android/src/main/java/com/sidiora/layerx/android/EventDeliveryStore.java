package com.sidiora.layerx.android;

/** At-least-once delivery bookkeeping that turns a repeated relay into a single applied effect. */
public interface EventDeliveryStore {
    enum Claim { CLAIMED, PROCESSING, COMPLETED, CONFLICT }

    Claim claim(String deliveryId, String payloadDigest, long leaseUntilMs);
    void complete(String deliveryId, String payloadDigest);
    void release(String deliveryId, String payloadDigest);
}
