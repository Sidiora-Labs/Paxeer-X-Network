package com.sidiora.layerx.spring;

import com.fasterxml.jackson.databind.JsonNode;
import java.io.IOException;

@FunctionalInterface
public interface LayerXWebhookEventHandler {
    void handle(JsonNode event, String deliveryId) throws IOException;
}
