package com.sidiora.layerx.android;

import com.fasterxml.jackson.annotation.JsonIgnoreType;
import com.sidiora.layerx.sdk.SecretBytes;
import java.net.HttpURLConnection;
import java.nio.charset.CharacterCodingException;
import java.nio.charset.CodingErrorAction;
import java.nio.charset.StandardCharsets;
import java.nio.ByteBuffer;

/** A short-lived brokered session credential that no application code can mint or persist. */
@JsonIgnoreType
public final class EphemeralSessionToken implements AutoCloseable {
    static final long MAXIMUM_LIFETIME_MS = 24L * 60L * 60L * 1_000L;
    static final long REFRESH_MARGIN_MS = 30_000L;
    private static final int MAXIMUM_TOKEN_BYTES = 4_096;

    private final SecretBytes secret;
    private final long issuedAtMs;
    private final long expiresAtMs;

    EphemeralSessionToken(byte[] value, long issuedAtMs, long expiresAtMs) {
        if (value == null || value.length == 0 || value.length > MAXIMUM_TOKEN_BYTES
                || issuedAtMs <= 0L || expiresAtMs <= issuedAtMs
                || expiresAtMs - issuedAtMs > MAXIMUM_LIFETIME_MS) {
            throw MobileIntegrationException.of(MobileIntegrationException.Code.INVALID_SESSION);
        }
        String decoded = decodeStrictUtf8(value);
        if (decoded.indexOf('\0') >= 0 || decoded.indexOf('\r') >= 0 || decoded.indexOf('\n') >= 0
                || EmbeddedSecretDetector.providerCredentialRule(decoded) != null) {
            throw MobileIntegrationException.of(MobileIntegrationException.Code.INVALID_SESSION);
        }
        this.secret = new SecretBytes(value);
        this.issuedAtMs = issuedAtMs;
        this.expiresAtMs = expiresAtMs;
    }

    public long issuedAtMs() { return issuedAtMs; }
    public long expiresAtMs() { return expiresAtMs; }

    public boolean usableAt(long nowMs) {
        return !secret.isDestroyed() && nowMs + REFRESH_MARGIN_MS < expiresAtMs;
    }

    void authorize(HttpURLConnection connection) {
        if (secret.isDestroyed()) {
            throw MobileIntegrationException.of(MobileIntegrationException.Code.SESSION_EXPIRED);
        }
        secret.use(bytes -> {
            connection.setRequestProperty("Authorization", "Bearer " + new String(bytes, StandardCharsets.UTF_8));
            return null;
        });
    }

    @Override public void close() { secret.close(); }
    @Override public String toString() { return "[REDACTED]"; }

    private static String decodeStrictUtf8(byte[] value) {
        try {
            return StandardCharsets.UTF_8.newDecoder()
                .onMalformedInput(CodingErrorAction.REPORT)
                .onUnmappableCharacter(CodingErrorAction.REPORT)
                .decode(ByteBuffer.wrap(value))
                .toString();
        } catch (CharacterCodingException error) {
            throw MobileIntegrationException.of(MobileIntegrationException.Code.INVALID_SESSION);
        }
    }
}
