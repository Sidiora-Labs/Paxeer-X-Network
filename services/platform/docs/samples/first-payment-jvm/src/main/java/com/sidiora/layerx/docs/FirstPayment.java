package com.sidiora.layerx.docs;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.databind.node.ObjectNode;
import com.sidiora.layerx.sdk.HttpProductionTransport;
import com.sidiora.layerx.sdk.IdempotencyKey;
import com.sidiora.layerx.sdk.PlatformSdkException;
import com.sidiora.layerx.sdk.ProductionClient;
import com.sidiora.layerx.sdk.SecretBytes;
import java.net.URI;
import java.nio.charset.StandardCharsets;
import java.util.List;
import java.util.Map;
import java.util.Set;

public final class FirstPayment {
    private static final Set<String> SETTLED = Set.of("done", "done-finalised", "refused");
    private static final Set<String> COMPLETED = Set.of("done", "done-finalised");
    private static final ObjectMapper MAPPER = new ObjectMapper();

    private FirstPayment() {}

    public static void main(String[] arguments) throws Exception {
        String apiUrl = required("LAYERX_API_URL");
        String apiToken = required("LAYERX_API_TOKEN");
        String source = required("LAYERX_SOURCE");
        String destination = required("LAYERX_DESTINATION");
        Map<String, String> money = Map.of("amount", required("LAYERX_AMOUNT"), "currency", required("LAYERX_CURRENCY"));
        String paymentKey = required("LAYERX_PAYMENT_KEY");

        // layerx:begin integration
        var credential = new HttpProductionTransport.BearerCredential(new SecretBytes(apiToken.getBytes(StandardCharsets.UTF_8)));
        var layerx = new ProductionClient(HttpProductionTransport.create(URI.create(apiUrl), URI.create(apiUrl), credential));
        var quote = layerx.human("move.quote", Map.of("source", source, "destination", destination, "money", money),
            JsonNode.class, ProductionClient.Options.none()).toCompletableFuture().join();
        var journey = layerx.human("move.commit", Map.of("quote_id", quote.path("quote_id").asText()), JsonNode.class,
            ProductionClient.Options.idempotent(new IdempotencyKey(paymentKey))).toCompletableFuture().join();
        // layerx:end integration

        JsonNode settled = awaitSettlement(layerx, journey);
        credential.close();
        System.out.println(report(settled));
        if (!COMPLETED.contains(settled.path("state").asText())) {
            System.exit(2);
        }
    }

    private static JsonNode awaitSettlement(ProductionClient layerx, JsonNode journey) throws InterruptedException {
        JsonNode current = journey;
        for (int attempt = 0; attempt < 40 && !SETTLED.contains(current.path("state").asText()); attempt += 1) {
            Thread.sleep(250L);
            current = layerx.human("journey.get", Map.of(), JsonNode.class,
                new ProductionClient.Options(null, Map.of("journey_id", current.path("journey_id").asText())))
                .toCompletableFuture().join();
        }
        return current;
    }

    private static String report(JsonNode journey) {
        ObjectNode report = MAPPER.createObjectNode();
        report.put("journey_id", journey.path("journey_id").asText());
        report.put("state", journey.path("state").asText());
        var receipts = report.putArray("receipts");
        for (JsonNode evidence : journey.path("evidence")) {
            if ("layerx-receipt".equals(evidence.path("class").asText())) {
                receipts.add(evidence.path("evidence_id").asText());
            }
        }
        if (journey.hasNonNull("refusal")) {
            report.put("refused_by", journey.path("refusal").path("refused_by").asText());
            report.put("money_left", journey.path("refusal").path("money_left").asBoolean());
        }
        return report.toString();
    }

    private static String required(String name) {
        String value = System.getenv(name);
        if (value == null || value.isEmpty()) {
            throw new IllegalStateException("first-payment-jvm: missing " + name);
        }
        return value;
    }
}
