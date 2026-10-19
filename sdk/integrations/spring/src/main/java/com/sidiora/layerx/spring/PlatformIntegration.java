package com.sidiora.layerx.spring;

import java.util.Map;

public final class PlatformIntegration {
    private PlatformIntegration() {}

    private static final Map<String, Object> METADATA = Map.of(
        "name", "com.sidiora.layerx:layerx-spring-boot-starter",
        "version", "0.1.0",
        "contractMajor", 1,
        "x402Version", X402.VERSION,
        "profile", "receipt-gated-x402-spring");

    /** Stable Codify and runtime package identity. */
    public static Map<String, Object> platform_int_spring() {
        return METADATA;
    }
}
