package com.sidiora.layerx.android.sample;

import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.databind.node.ObjectNode;
import com.sidiora.layerx.android.EventEnvelopeHeaders;
import com.sidiora.layerx.android.LayerXAndroid;
import com.sidiora.layerx.android.MobileIntegrationException;
import com.sidiora.layerx.android.PublishableConfiguration;
import com.sidiora.layerx.android.VerifiedEventConsumer;
import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.Base64;
import java.util.LinkedHashMap;
import java.util.Locale;
import java.util.Map;
import java.util.concurrent.atomic.AtomicInteger;

public final class WebhookConformanceMain {
    private record Capture(String body, Map<String, String> headers) {}

    private WebhookConformanceMain() {}

    public static void main(String[] arguments) {
        try {
            Map<String, String> options = options(arguments);
            Path configurationPath = absolute(options.get("--configuration"));
            Path capturePath = absolute(options.get("--capture"));
            Path ledgerPath = absolute(options.get("--ledger"));
            String expected = options.get("--expect");
            if (!"processed".equals(expected) && !"duplicate".equals(expected)) throw invalid();
            PublishableConfiguration configuration = PublishableConfiguration.ofJsonFile(configurationPath);
            if (!Files.isRegularFile(capturePath) || Files.size(capturePath) > 2_097_152L) throw invalid();
            ObjectMapper mapper = new ObjectMapper();
            Capture capture = mapper.readValue(Files.readAllBytes(capturePath), Capture.class);
            if (capture == null || capture.body() == null || capture.headers() == null) throw invalid();
            byte[] body = Base64.getDecoder().decode(capture.body());
            if (body.length == 0 || body.length > 1_048_576) throw invalid();
            EventEnvelopeHeaders headers = EventEnvelopeHeaders.of(capture.headers());
            try (LayerXAndroid mobile = LayerXAndroid.create(configuration, ledgerPath)) {
            VerifiedEventConsumer consumer = mobile.events();
            AtomicInteger effects = new AtomicInteger();
            VerifiedEventConsumer.Handler handler = (event, id) -> effects.incrementAndGet();

            byte[] tampered = body.clone();
            tampered[tampered.length - 1] ^= 1;
            refused(() -> consumer.consume(tampered, headers, handler));
            refused(() -> consumer.consume(body, new EventEnvelopeHeaders(headers.id(),
                "0" + headers.timestamp(), headers.keyId(), headers.signature()), handler));
            refused(() -> consumer.consume(body, new EventEnvelopeHeaders(headers.id(),
                "0", headers.keyId(), headers.signature()), handler));
            refused(() -> consumer.consume(body, new EventEnvelopeHeaders(headers.id(),
                headers.timestamp(), "", headers.signature()), handler));
            refused(() -> consumer.consume(body, new EventEnvelopeHeaders(headers.id(),
                headers.timestamp(), headers.keyId(), "v2=" + headers.signature().substring(3)), handler));

            Map<String, String> missing = normalized(capture.headers());
            missing.remove(EventEnvelopeHeaders.SIGNATURE_HEADER.toLowerCase(Locale.ROOT));
            refused(() -> EventEnvelopeHeaders.of(missing));
            Map<String, String> ambiguous = normalized(capture.headers());
            ambiguous.put(EventEnvelopeHeaders.ID_HEADER.toUpperCase(Locale.ROOT), headers.id());
            refused(() -> EventEnvelopeHeaders.of(ambiguous));
            if (effects.get() != 0) throw verification();

            VerifiedEventConsumer.Outcome outcome = mobile.consume(body, capture.headers(), handler);
            String actual = outcome.name().toLowerCase(Locale.ROOT);
            int expectedEffects = "processed".equals(expected) ? 1 : 0;
            if (!expected.equals(actual) || effects.get() != expectedEffects) throw verification();
            if (mobile.consume(body, capture.headers(), handler) != VerifiedEventConsumer.Outcome.DUPLICATE
                    || effects.get() != expectedEffects) throw verification();
            ObjectNode report = mapper.createObjectNode();
            report.put("event", actual);
            report.put("event_replay", "duplicate");
            report.put("event_tamper", "rejected");
            report.put("event_timestamp", "rejected");
            report.put("event_headers", "rejected");
            report.put("effects", effects.get());
            System.out.println(mapper.writeValueAsString(report));
            }
        } catch (MobileIntegrationException error) {
            System.err.println("layerx-android-webhook: " + error.code().wire());
            System.exit(2);
        } catch (IOException | IllegalArgumentException error) {
            System.err.println("layerx-android-webhook: invalid-configuration");
            System.exit(2);
        }
    }

    private static Map<String, String> options(String[] arguments) {
        if (arguments.length != 8) throw invalid();
        Map<String, String> options = new LinkedHashMap<>();
        for (int index = 0; index < arguments.length; index += 2) {
            String name = arguments[index];
            if (!name.equals("--configuration") && !name.equals("--capture")
                    && !name.equals("--ledger") && !name.equals("--expect")) throw invalid();
            if (options.putIfAbsent(name, arguments[index + 1]) != null) throw invalid();
        }
        if (options.size() != 4) throw invalid();
        return options;
    }

    private static Path absolute(String value) {
        if (value == null || value.isEmpty()) throw invalid();
        Path path = Path.of(value);
        if (!path.isAbsolute()) throw invalid();
        return path.normalize();
    }

    private static Map<String, String> normalized(Map<String, String> headers) {
        Map<String, String> result = new LinkedHashMap<>();
        headers.forEach((name, value) -> result.put(name.toLowerCase(Locale.ROOT), value));
        return result;
    }

    private static void refused(Runnable invocation) {
        try {
            invocation.run();
        } catch (MobileIntegrationException error) {
            if (error.code() == MobileIntegrationException.Code.INVALID_EVENT) return;
            throw error;
        }
        throw verification();
    }

    private static MobileIntegrationException invalid() {
        return MobileIntegrationException.of(MobileIntegrationException.Code.INVALID_CONFIGURATION);
    }

    private static MobileIntegrationException verification() {
        return MobileIntegrationException.of(MobileIntegrationException.Code.VERIFICATION_FAILURE);
    }
}
