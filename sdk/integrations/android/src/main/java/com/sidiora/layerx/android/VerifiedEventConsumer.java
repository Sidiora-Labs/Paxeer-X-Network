package com.sidiora.layerx.android;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.util.Base64;
import java.util.LinkedHashMap;
import java.util.Map;
import java.util.Objects;
import java.util.function.LongSupplier;
import org.bouncycastle.crypto.params.Ed25519PublicKeyParameters;
import org.bouncycastle.crypto.signers.Ed25519Signer;

/** The default event path: verify the signature and the delivery lease before any effect is applied. */
public final class VerifiedEventConsumer {
    public enum Outcome { PROCESSED, DUPLICATE, PROCESSING }

    @FunctionalInterface public interface Handler {
        void handle(JsonNode event, String deliveryId);
    }

    private static final int MAXIMUM_BODY_BYTES = 1_048_576;
    private static final long MAXIMUM_FUTURE_SKEW_MS = 30_000L;
    private static final long MAXIMUM_TIMESTAMP_SECONDS = 253_402_300_799L;

    private final Map<String, byte[]> publicKeys;
    private final EventDeliveryStore deliveries;
    private final ObjectMapper mapper;
    private final long maximumAgeMs;
    private final long leaseMs;
    private final LongSupplier clock;

    public VerifiedEventConsumer(Map<String, byte[]> publicKeys, EventDeliveryStore deliveries, ObjectMapper mapper,
                                 long maximumAgeMs, long leaseMs, LongSupplier clock) {
        Objects.requireNonNull(publicKeys, "publicKeys");
        if (publicKeys.isEmpty() || maximumAgeMs <= 0L || leaseMs <= 0L) {
            throw MobileIntegrationException.of(MobileIntegrationException.Code.INVALID_CONFIGURATION);
        }
        Map<String, byte[]> copy = new LinkedHashMap<>();
        for (Map.Entry<String, byte[]> entry : publicKeys.entrySet()) {
            if (entry.getValue() == null || entry.getValue().length != 32) {
                throw MobileIntegrationException.of(MobileIntegrationException.Code.INVALID_CONFIGURATION);
            }
            copy.put(entry.getKey(), entry.getValue().clone());
        }
        this.publicKeys = Map.copyOf(copy);
        this.deliveries = Objects.requireNonNull(deliveries, "deliveries");
        this.mapper = Objects.requireNonNull(mapper, "mapper");
        this.maximumAgeMs = maximumAgeMs;
        this.leaseMs = leaseMs;
        this.clock = clock == null ? System::currentTimeMillis : clock;
    }

    public static VerifiedEventConsumer create(PublishableConfiguration configuration, EventDeliveryStore deliveries) {
        return new VerifiedEventConsumer(configuration.eventPublicKeys(), deliveries, new ObjectMapper(),
            configuration.eventMaximumAgeMs(), 60_000L, null);
    }

    public Outcome consume(byte[] rawBody, EventEnvelopeHeaders headers, Handler handler) {
        Objects.requireNonNull(rawBody, "rawBody");
        Objects.requireNonNull(headers, "headers");
        Objects.requireNonNull(handler, "handler");
        long now = clock.getAsLong();
        long issuedAtMs = canonicalSeconds(headers.timestamp()) * 1_000L;
        byte[] publicKey = publicKeys.get(headers.keyId());
        if (!bounded(headers.id(), 255) || !identifier(headers.keyId(), 64)
                || rawBody.length == 0 || rawBody.length > MAXIMUM_BODY_BYTES
                || issuedAtMs > now + MAXIMUM_FUTURE_SKEW_MS || now - issuedAtMs > maximumAgeMs
                || publicKey == null) {
            throw MobileIntegrationException.of(MobileIntegrationException.Code.INVALID_EVENT);
        }
        byte[] signature = parseSignature(headers.signature());
        byte[] prefix = (headers.id() + "." + headers.timestamp() + ".").getBytes(StandardCharsets.UTF_8);
        if (!verifyEd25519(publicKey, signature, prefix, rawBody)) {
            throw MobileIntegrationException.of(MobileIntegrationException.Code.INVALID_EVENT);
        }

        String payloadDigest = PublishableConfiguration.hexadecimal(sha256(rawBody));
        EventDeliveryStore.Claim claim = deliveries.claim(headers.id(), payloadDigest, now + leaseMs);
        switch (claim) {
            case CONFLICT -> throw MobileIntegrationException.of(MobileIntegrationException.Code.EVENT_REPLAY);
            case COMPLETED -> { return Outcome.DUPLICATE; }
            case PROCESSING -> { return Outcome.PROCESSING; }
            case CLAIMED -> { }
        }
        try {
            handler.handle(decode(rawBody), headers.id());
            deliveries.complete(headers.id(), payloadDigest);
        } catch (RuntimeException error) {
            try {
                deliveries.release(headers.id(), payloadDigest);
            } catch (RuntimeException ignored) {
                throw error;
            }
            throw error;
        }
        return Outcome.PROCESSED;
    }

    private JsonNode decode(byte[] rawBody) {
        try {
            JsonNode event = mapper.readTree(rawBody);
            if (event == null || !event.isObject()) {
                throw MobileIntegrationException.of(MobileIntegrationException.Code.DECODE_FAILURE);
            }
            return event;
        } catch (IOException error) {
            throw MobileIntegrationException.of(MobileIntegrationException.Code.DECODE_FAILURE);
        }
    }

    private static long canonicalSeconds(String value) {
        if (value.isEmpty() || value.length() > 19 || (value.length() > 1 && value.charAt(0) == '0')) {
            throw MobileIntegrationException.of(MobileIntegrationException.Code.INVALID_EVENT);
        }
        long seconds = 0L;
        for (int index = 0; index < value.length(); index++) {
            char character = value.charAt(index);
            if (character < '0' || character > '9') {
                throw MobileIntegrationException.of(MobileIntegrationException.Code.INVALID_EVENT);
            }
            seconds = seconds * 10L + (character - '0');
            if (seconds > MAXIMUM_TIMESTAMP_SECONDS) {
                throw MobileIntegrationException.of(MobileIntegrationException.Code.INVALID_EVENT);
            }
        }
        return seconds;
    }

    private static byte[] parseSignature(String value) {
        if (!value.startsWith("v1=")) {
            throw MobileIntegrationException.of(MobileIntegrationException.Code.INVALID_EVENT);
        }
        try {
            byte[] decoded = Base64.getDecoder().decode(value.substring(3));
            if (decoded.length != 64) {
                throw MobileIntegrationException.of(MobileIntegrationException.Code.INVALID_EVENT);
            }
            return decoded;
        } catch (IllegalArgumentException error) {
            throw MobileIntegrationException.of(MobileIntegrationException.Code.INVALID_EVENT);
        }
    }

    private static boolean verifyEd25519(byte[] publicKey, byte[] signature, byte[] prefix, byte[] body) {
        try {
            Ed25519Signer signer = new Ed25519Signer();
            signer.init(false, new Ed25519PublicKeyParameters(publicKey, 0));
            signer.update(prefix, 0, prefix.length);
            signer.update(body, 0, body.length);
            return signer.verifySignature(signature);
        } catch (RuntimeException error) {
            return false;
        }
    }

    private static byte[] sha256(byte[] value) {
        try {
            return MessageDigest.getInstance("SHA-256").digest(value);
        } catch (java.security.NoSuchAlgorithmException impossible) {
            throw new AssertionError(impossible);
        }
    }

    private static boolean bounded(String value, int limit) {
        return !value.isEmpty() && value.getBytes(StandardCharsets.UTF_8).length <= limit && value.indexOf('\0') < 0;
    }

    private static boolean identifier(String value, int limit) {
        if (!bounded(value, limit)) return false;
        for (int index = 0; index < value.length(); index++) {
            char character = value.charAt(index);
            boolean allowed = (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z')
                || (character >= '0' && character <= '9')
                || character == '.' || character == '_' || character == '-';
            if (!allowed) return false;
        }
        return true;
    }
}
