package com.sidiora.layerx.android;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import java.io.IOException;
import java.io.InputStream;
import java.net.HttpURLConnection;
import java.net.URI;
import java.net.URL;
import java.nio.charset.StandardCharsets;
import java.util.Locale;
import java.util.Objects;
import java.util.concurrent.locks.ReentrantLock;
import java.util.function.LongSupplier;

/** Fetches short-lived sessions from the application's own backend broker, never from a bundled key. */
public final class BrokeredSessionTokenProvider implements SessionTokenProvider, AutoCloseable {
    private static final int MAXIMUM_RESPONSE_BYTES = 64 * 1024;

    private final URI brokerUri;
    private final String audience;
    private final int timeoutMs;
    private final ObjectMapper mapper;
    private final LongSupplier clock;
    private final ReentrantLock lock = new ReentrantLock();
    private EphemeralSessionToken cached;

    public BrokeredSessionTokenProvider(URI brokerUri, String audience, int timeoutMs,
                                        ObjectMapper mapper, LongSupplier clock) {
        this.brokerUri = PublishableConfiguration.endpoint(Objects.requireNonNull(brokerUri, "brokerUri").toString());
        this.audience = requireAudience(audience);
        if (timeoutMs < 1_000 || timeoutMs > 300_000) {
            throw MobileIntegrationException.of(MobileIntegrationException.Code.INVALID_CONFIGURATION);
        }
        this.timeoutMs = timeoutMs;
        this.mapper = Objects.requireNonNull(mapper, "mapper");
        this.clock = clock == null ? System::currentTimeMillis : clock;
    }

    public static BrokeredSessionTokenProvider create(PublishableConfiguration configuration) {
        return new BrokeredSessionTokenProvider(configuration.sessionBrokerUri(), "layerx-human-api",
            (int) configuration.requestTimeoutMs(), new ObjectMapper(), null);
    }

    @Override
    public EphemeralSessionToken token() {
        long now = clock.getAsLong();
        lock.lock();
        try {
            if (cached != null && cached.usableAt(now)) return cached;
        } finally {
            lock.unlock();
        }
        EphemeralSessionToken issued = request(now);
        lock.lock();
        try {
            cached = issued;
            return cached;
        } finally {
            lock.unlock();
        }
    }

    @Override
    public void invalidate() {
        lock.lock();
        try {
            if (cached != null) cached.close();
            cached = null;
        } finally {
            lock.unlock();
        }
    }

    @Override public void close() { invalidate(); }

    private EphemeralSessionToken request(long now) {
        HttpURLConnection connection = null;
        try {
            connection = (HttpURLConnection) new URL(brokerUri.toString()).openConnection();
            connection.setRequestMethod("POST");
            connection.setConnectTimeout(timeoutMs);
            connection.setReadTimeout(timeoutMs);
            connection.setInstanceFollowRedirects(false);
            connection.setUseCaches(false);
            connection.setDoOutput(true);
            connection.setRequestProperty("Accept", "application/json");
            connection.setRequestProperty("Content-Type", "application/json");
            connection.setRequestProperty("User-Agent", "layerx-android/0.1.0");
            byte[] body = mapper.createObjectNode().put("audience", audience).toString()
                .getBytes(StandardCharsets.UTF_8);
            connection.setFixedLengthStreamingMode(body.length);
            connection.getOutputStream().write(body);
            connection.getOutputStream().flush();
            int status = connection.getResponseCode();
            if (status < 200 || status >= 300) {
                throw MobileIntegrationException.of(MobileIntegrationException.Code.INVALID_SESSION);
            }
            byte[] encoded;
            try (InputStream stream = connection.getInputStream()) {
                encoded = stream.readNBytes(MAXIMUM_RESPONSE_BYTES + 1);
            }
            if (encoded.length > MAXIMUM_RESPONSE_BYTES) {
                throw MobileIntegrationException.of(MobileIntegrationException.Code.INVALID_SESSION);
            }
            JsonNode envelope = mapper.readTree(encoded);
            if (envelope == null || !envelope.isObject() || !envelope.path("session_token").isTextual()
                    || !envelope.path("expires_at_ms").canConvertToLong()) {
                throw MobileIntegrationException.of(MobileIntegrationException.Code.INVALID_SESSION);
            }
            long issuedAt = envelope.path("issued_at_ms").canConvertToLong()
                ? envelope.path("issued_at_ms").longValue() : now;
            return new EphemeralSessionToken(
                envelope.path("session_token").textValue().getBytes(StandardCharsets.UTF_8),
                issuedAt,
                envelope.path("expires_at_ms").longValue());
        } catch (IOException error) {
            throw MobileIntegrationException.of(MobileIntegrationException.Code.TRANSPORT_FAILURE);
        } finally {
            if (connection != null) connection.disconnect();
        }
    }

    private static String requireAudience(String audience) {
        if (audience == null || audience.isEmpty() || audience.length() > 128) {
            throw MobileIntegrationException.of(MobileIntegrationException.Code.INVALID_CONFIGURATION);
        }
        String normalized = audience.toLowerCase(Locale.ROOT);
        for (int index = 0; index < normalized.length(); index++) {
            char character = normalized.charAt(index);
            boolean allowed = (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9')
                || character == '-' || character == '.' || character == '_';
            if (!allowed) throw MobileIntegrationException.of(MobileIntegrationException.Code.INVALID_CONFIGURATION);
        }
        return audience;
    }
}
