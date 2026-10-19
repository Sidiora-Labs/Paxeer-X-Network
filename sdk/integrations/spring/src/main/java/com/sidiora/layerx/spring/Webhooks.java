package com.sidiora.layerx.spring;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.util.LinkedHashMap;
import java.util.Map;
import java.util.Objects;
import java.util.concurrent.ConcurrentHashMap;
import java.util.function.LongSupplier;
import java.util.regex.Pattern;

public final class Webhooks {
    private Webhooks() {}

    public static final String ID_HEADER = "layerx-webhook-id";
    public static final String TIMESTAMP_HEADER = "layerx-webhook-timestamp";
    public static final String KEY_HEADER = "layerx-webhook-key-id";
    public static final String SIGNATURE_HEADER = "layerx-webhook-signature";
    public static final long DEFAULT_MAXIMUM_AGE_MS = 5L * 60L * 1000L;
    public static final long DEFAULT_LEASE_MS = 60L * 1000L;
    public static final int MAXIMUM_WEBHOOK_BYTES = 1_048_576;

    private static final Pattern CANONICAL_INTEGER = Pattern.compile("0|[1-9][0-9]*");
    private static final Pattern IDENTIFIER = Pattern.compile("[A-Za-z0-9._-]+");
    private static final ObjectMapper MAPPER = new ObjectMapper();

    public record RequestHeaders(String id, String timestamp, String keyId, String signature) {}

    public record DeliveryClaim(String deliveryId, String payloadDigest, long leaseUntilMs) {}

    public enum ClaimResult { CLAIMED, PROCESSING, COMPLETED, CONFLICT }

    public enum ConsumeResult { PROCESSED, DUPLICATE, PROCESSING }

    public interface DeliveryStore {
        ClaimResult claim(DeliveryClaim value);

        void complete(String deliveryId, String payloadDigest);

        void release(String deliveryId, String payloadDigest);
    }

    public static final class InMemoryDeliveryStore implements DeliveryStore {
        private record Entry(String payloadDigest, long leaseUntilMs, boolean completed) {}

        private final Map<String, Entry> entries = new ConcurrentHashMap<>();
        private final LongSupplier clock;

        public InMemoryDeliveryStore() { this(System::currentTimeMillis); }

        public InMemoryDeliveryStore(LongSupplier clock) {
            this.clock = Objects.requireNonNull(clock, "clock");
        }

        @Override
        public synchronized ClaimResult claim(DeliveryClaim value) {
            Entry existing = entries.get(value.deliveryId());
            if (existing == null) {
                entries.put(value.deliveryId(), new Entry(value.payloadDigest(), value.leaseUntilMs(), false));
                return ClaimResult.CLAIMED;
            }
            if (!existing.payloadDigest().equals(value.payloadDigest())) return ClaimResult.CONFLICT;
            if (existing.completed()) return ClaimResult.COMPLETED;
            if (existing.leaseUntilMs() > clock.getAsLong()) return ClaimResult.PROCESSING;
            entries.put(value.deliveryId(), new Entry(value.payloadDigest(), value.leaseUntilMs(), false));
            return ClaimResult.CLAIMED;
        }

        @Override
        public synchronized void complete(String deliveryId, String payloadDigest) {
            Entry existing = entries.get(deliveryId);
            if (existing == null || !existing.payloadDigest().equals(payloadDigest)) {
                throw MiddlewareException.of(MiddlewareException.Code.WEBHOOK_REPLAY);
            }
            entries.put(deliveryId, new Entry(payloadDigest, 0L, true));
        }

        @Override
        public synchronized void release(String deliveryId, String payloadDigest) {
            Entry existing = entries.get(deliveryId);
            if (existing != null && existing.payloadDigest().equals(payloadDigest) && !existing.completed()) {
                entries.remove(deliveryId);
            }
        }
    }

    public static final class VerifiedWebhookConsumer {
        private final Map<String, byte[]> keys;
        private final DeliveryStore deliveries;
        private final long maximumAgeMs;
        private final long leaseMs;
        private final LongSupplier clock;

        public VerifiedWebhookConsumer(Map<String, byte[]> publicKeys, DeliveryStore deliveries,
                                       long maximumAgeMs, long leaseMs, LongSupplier clock) {
            Objects.requireNonNull(publicKeys, "publicKeys");
            if (publicKeys.isEmpty() || maximumAgeMs <= 0 || leaseMs <= 0) {
                throw MiddlewareException.of(MiddlewareException.Code.INVALID_WEBHOOK);
            }
            this.keys = new LinkedHashMap<>(publicKeys);
            this.deliveries = Objects.requireNonNull(deliveries, "deliveries");
            this.maximumAgeMs = maximumAgeMs;
            this.leaseMs = leaseMs;
            this.clock = clock == null ? System::currentTimeMillis : clock;
        }

        public ConsumeResult consume(byte[] rawBody, RequestHeaders headers, LayerXWebhookEventHandler handle)
                throws IOException {
            long now = clock.getAsLong();
            long timestampSeconds = parseCanonicalInteger(headers.timestamp());
            if (timestampSeconds > 253_402_300_799L) {
                throw MiddlewareException.of(MiddlewareException.Code.INVALID_WEBHOOK);
            }
            long timestampMs = timestampSeconds * 1000L;
            if (!X402.bounded(headers.id(), 255)
                    || !X402.bounded(headers.keyId(), 64)
                    || !IDENTIFIER.matcher(headers.keyId()).matches()
                    || timestampMs > now + 30_000L
                    || now - timestampMs > maximumAgeMs) {
                throw MiddlewareException.of(MiddlewareException.Code.INVALID_WEBHOOK);
            }
            byte[] publicKey = keys.get(headers.keyId());
            if (publicKey == null || publicKey.length != 32) {
                throw MiddlewareException.of(MiddlewareException.Code.INVALID_WEBHOOK);
            }
            byte[] signature = parseSignature(headers.signature());
            byte[] prefix = (headers.id() + "." + headers.timestamp() + ".").getBytes(StandardCharsets.UTF_8);
            byte[] message = new byte[prefix.length + rawBody.length];
            System.arraycopy(prefix, 0, message, 0, prefix.length);
            System.arraycopy(rawBody, 0, message, prefix.length, rawBody.length);
            if (!Ed25519Verifier.verify(publicKey, signature, message)) {
                throw MiddlewareException.of(MiddlewareException.Code.INVALID_WEBHOOK);
            }
            String payloadDigest = X402.hex(X402.sha256(rawBody));
            ClaimResult claim = deliveries.claim(new DeliveryClaim(headers.id(), payloadDigest, now + leaseMs));
            if (claim == ClaimResult.CONFLICT) {
                throw MiddlewareException.of(MiddlewareException.Code.WEBHOOK_REPLAY);
            }
            if (claim == ClaimResult.COMPLETED) return ConsumeResult.DUPLICATE;
            if (claim == ClaimResult.PROCESSING) return ConsumeResult.PROCESSING;
            try {
                handle.handle(decodeEvent(rawBody), headers.id());
                deliveries.complete(headers.id(), payloadDigest);
            } catch (IOException error) {
                deliveries.release(headers.id(), payloadDigest);
                throw error;
            } catch (RuntimeException error) {
                deliveries.release(headers.id(), payloadDigest);
                throw error;
            }
            return ConsumeResult.PROCESSED;
        }
    }

    static JsonNode decodeEvent(byte[] rawBody) {
        JsonNode event;
        try {
            event = MAPPER.readTree(rawBody);
        } catch (IOException error) {
            throw MiddlewareException.of(MiddlewareException.Code.INVALID_WEBHOOK);
        }
        if (event == null || !event.isObject()) {
            throw MiddlewareException.of(MiddlewareException.Code.INVALID_WEBHOOK);
        }
        return event;
    }

    public static byte[] parseSignature(String value) {
        String encoded = value != null && value.startsWith("v1=") ? value.substring(3) : "";
        byte[] signature = X402.decodeBase64(encoded, MiddlewareException.Code.INVALID_WEBHOOK);
        if (signature.length != 64) throw MiddlewareException.of(MiddlewareException.Code.INVALID_WEBHOOK);
        return signature;
    }

    public static long parseCanonicalInteger(String value) {
        if (value == null || value.length() > 19 || !CANONICAL_INTEGER.matcher(value).matches()) {
            throw MiddlewareException.of(MiddlewareException.Code.INVALID_WEBHOOK);
        }
        try {
            return Long.parseLong(value);
        } catch (NumberFormatException error) {
            throw MiddlewareException.of(MiddlewareException.Code.INVALID_WEBHOOK);
        }
    }
}
