package com.sidiora.layerx.sdk;

import java.util.Map;
import org.junit.jupiter.api.Test;
import static org.junit.jupiter.api.Assertions.*;

public final class GeneratedDescriptorCatalogTest {
    private static void assertMatchesCatalog(OperationCatalog.Plane plane,
                                             Map<String, SchemaTypes.TypedOperation<?, ?>> descriptors) {
        descriptors.forEach((wireName, descriptor) -> {
            assertEquals(plane, descriptor.plane(), wireName);
            assertEquals(wireName, descriptor.wireName());
            assertEquals(OperationCatalog.requiresIdempotency(plane, wireName), descriptor.idempotencyRequired(), wireName);
        });
    }

    @Test
    void agentDescriptorsMatchTheOperationCatalog() {
        assertEquals(OperationCatalog.AGENT_OPERATIONS, GeneratedSchema.AGENT.keySet());
        assertMatchesCatalog(OperationCatalog.Plane.AGENT, GeneratedSchema.AGENT);
        assertTrue(OperationCatalog.AGENT_IDEMPOTENT.contains("mcp.invoke"));
        assertTrue(GeneratedSchema.AgentOperations.MCP_INVOKE.idempotencyRequired());
    }

    @Test
    void humanDescriptorsMatchTheOperationCatalog() {
        assertEquals(OperationCatalog.HUMAN_ROUTES.keySet(), GeneratedSchema.HUMAN.keySet());
        assertMatchesCatalog(OperationCatalog.Plane.HUMAN, GeneratedSchema.HUMAN);
    }
}
