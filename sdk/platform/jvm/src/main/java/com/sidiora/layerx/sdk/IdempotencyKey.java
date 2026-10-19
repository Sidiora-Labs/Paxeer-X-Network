package com.sidiora.layerx.sdk;

import com.fasterxml.jackson.annotation.JsonCreator;
import com.fasterxml.jackson.annotation.JsonValue;

/** A validated key for replay-safe protocol mutations. */
public record IdempotencyKey(String value) {
    @JsonCreator
    public IdempotencyKey {
        if (value == null || value.isEmpty() || value.length() > 255 || value.indexOf('\0') >= 0) {
            throw PlatformSdkException.invalidArgument();
        }
    }

    @Override @JsonValue
    public String toString() {
        return value;
    }
}
