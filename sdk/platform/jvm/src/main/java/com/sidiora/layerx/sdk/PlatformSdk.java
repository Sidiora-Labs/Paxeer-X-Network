package com.sidiora.layerx.sdk;

import java.util.Map;

public final class PlatformSdk {
    private PlatformSdk() {}
    private static final Map<String, Object> METADATA = Map.of(
        "name", "com.sidiora.layerx:layerx-sdk",
        "version", "0.1.0",
        "contractMajor", 1,
        "agentOperations", OperationCatalog.AGENT_OPERATIONS.size(),
        "humanOperations", OperationCatalog.HUMAN_ROUTES.size());

    /** Stable Codify and runtime package identity. */
    public static Map<String, Object> platform_sdk_jvm() {
        return METADATA;
    }
}
