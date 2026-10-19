package com.sidiora.layerx.docs;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.node.ArrayNode;
import com.fasterxml.jackson.databind.node.ObjectNode;
import com.sidiora.layerx.android.LayerXAndroid;
import com.sidiora.layerx.android.PublishableConfiguration;
import com.sidiora.layerx.sdk.IdempotencyKey;
import java.util.List;

public final class MobilePayment {
    private static final List<String> SETTLED = List.of("done", "done-finalised", "refused");
    private static final List<String> COMPLETED = List.of("done", "done-finalised");

    private MobilePayment() {}

    // layerx:begin integration
    static LayerXAndroid openLayerX() {
        return LayerXAndroid.create(PublishableConfiguration.ofEnvironment(System.getenv()));
    }

    static JsonNode pay(LayerXAndroid layerx, ObjectNode move, String paymentKey) {
        var quote = layerx.client().quote(move).toCompletableFuture().join();
        var commit = layerx.mapper().createObjectNode().put("quote_id", quote.path("quote_id").asText());
        return layerx.client().commit(commit, new IdempotencyKey(paymentKey)).toCompletableFuture().join();
    }
    // layerx:end integration

    public static void main(String[] arguments) throws Exception {
        try (LayerXAndroid layerx = openLayerX()) {
            JsonNode journey = settle(layerx, pay(layerx, move(layerx), required("LAYERX_PAYMENT_KEY")));
            System.out.println(report(layerx, journey));
            if (!COMPLETED.contains(state(journey))) {
                System.exit(2);
            }
        }
    }

    private static ObjectNode move(LayerXAndroid layerx) {
        ObjectNode money = layerx.mapper().createObjectNode()
            .put("amount", required("LAYERX_AMOUNT"))
            .put("currency", required("LAYERX_CURRENCY"));
        ObjectNode request = layerx.mapper().createObjectNode()
            .put("source", required("LAYERX_SOURCE"))
            .put("destination", required("LAYERX_DESTINATION"));
        request.set("money", money);
        return request;
    }

    private static JsonNode settle(LayerXAndroid layerx, JsonNode committed) throws InterruptedException {
        JsonNode latest = committed;
        String identifier = latest.path("journey_id").asText();
        for (int attempt = 0; attempt < 40 && !SETTLED.contains(state(latest)); attempt += 1) {
            Thread.sleep(250L);
            latest = layerx.client().journey(identifier).toCompletableFuture().join();
        }
        return latest;
    }

    private static ObjectNode report(LayerXAndroid layerx, JsonNode journey) {
        ObjectNode body = layerx.mapper().createObjectNode();
        body.put("journey_id", journey.path("journey_id").asText());
        body.put("state", state(journey));
        ArrayNode receipts = body.putArray("receipts");
        for (JsonNode reference : journey.path("evidence")) {
            if ("layerx-receipt".equals(reference.path("class").asText())) {
                receipts.add(reference.path("evidence_id").asText());
            }
        }
        if (journey.hasNonNull("refusal")) {
            body.set("refused_by", journey.path("refusal").path("refused_by"));
            body.set("money_left", journey.path("refusal").path("money_left"));
        }
        return body;
    }

    private static String state(JsonNode journey) {
        return journey.path("state").asText();
    }

    private static String required(String name) {
        String value = System.getenv(name);
        if (value == null || value.isEmpty()) {
            throw new IllegalStateException("missing " + name);
        }
        return value;
    }
}
