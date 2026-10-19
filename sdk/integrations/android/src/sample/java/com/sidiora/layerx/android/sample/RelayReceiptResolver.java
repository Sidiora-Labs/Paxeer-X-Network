package com.sidiora.layerx.android.sample;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.sidiora.layerx.android.MobileIntegrationException;
import com.sidiora.layerx.android.ReceiptGate;
import com.sidiora.layerx.sdk.verify.LocalVerifier;
import java.io.IOException;
import java.io.InputStream;
import java.net.HttpURLConnection;
import java.net.URI;
import java.net.URL;
import java.net.URLEncoder;
import java.nio.charset.StandardCharsets;
import java.util.Base64;
import java.util.Locale;
import java.util.Objects;

/** Resolves receipt evidence from the application's own relay; the device still verifies it. */
public final class RelayReceiptResolver implements ReceiptGate.ReceiptResolver {
    private static final int MAXIMUM_RESPONSE_BYTES = 4 * 1024 * 1024;

    private final URI relayUri;
    private final ObjectMapper mapper;
    private final int timeoutMs;

    public RelayReceiptResolver(URI relayUri, ObjectMapper mapper, int timeoutMs) {
        this.relayUri = requireRelay(Objects.requireNonNull(relayUri, "relayUri"));
        this.mapper = Objects.requireNonNull(mapper, "mapper");
        if (timeoutMs < 1_000 || timeoutMs > 300_000) {
            throw MobileIntegrationException.of(MobileIntegrationException.Code.INVALID_CONFIGURATION);
        }
        this.timeoutMs = timeoutMs;
    }

    @Override
    public ReceiptGate.Evidence resolve(String receiptReference) {
        String prefix = relayUri.toString();
        while (prefix.endsWith("/")) prefix = prefix.substring(0, prefix.length() - 1);
        String target = prefix + "/" + URLEncoder.encode(receiptReference, StandardCharsets.UTF_8).replace("+", "%20");
        HttpURLConnection connection = null;
        try {
            connection = (HttpURLConnection) new URL(target).openConnection();
            connection.setRequestMethod("GET");
            connection.setConnectTimeout(timeoutMs);
            connection.setReadTimeout(timeoutMs);
            connection.setInstanceFollowRedirects(false);
            connection.setUseCaches(false);
            connection.setRequestProperty("Accept", "application/json");
            connection.setRequestProperty("User-Agent", "layerx-android/0.1.0");
            int status = connection.getResponseCode();
            if (status < 200 || status >= 300) {
                throw MobileIntegrationException.of(MobileIntegrationException.Code.DECODE_FAILURE);
            }
            byte[] encoded;
            try (InputStream stream = connection.getInputStream()) {
                encoded = stream.readNBytes(MAXIMUM_RESPONSE_BYTES + 1);
            }
            if (encoded.length > MAXIMUM_RESPONSE_BYTES) {
                throw MobileIntegrationException.of(MobileIntegrationException.Code.DECODE_FAILURE);
            }
            return evidence(mapper.readTree(encoded));
        } catch (IOException error) {
            throw MobileIntegrationException.of(MobileIntegrationException.Code.TRANSPORT_FAILURE);
        } finally {
            if (connection != null) connection.disconnect();
        }
    }

    private ReceiptGate.Evidence evidence(JsonNode payload) {
        if (payload == null || !payload.isObject() || !payload.path("canonical_receipt_base64").isTextual()
                || !payload.path("authorized_batch").isObject()) {
            throw MobileIntegrationException.of(MobileIntegrationException.Code.DECODE_FAILURE);
        }
        JsonNode batch = payload.path("authorized_batch");
        byte[] canonical;
        try {
            canonical = Base64.getDecoder().decode(payload.path("canonical_receipt_base64").textValue());
        } catch (IllegalArgumentException error) {
            throw MobileIntegrationException.of(MobileIntegrationException.Code.DECODE_FAILURE);
        }
        return new ReceiptGate.Evidence(canonical, new LocalVerifier.AuthorizedReceiptBatch(
            hex32(field(batch, "batch_id")),
            hex32(field(batch, "asset")),
            hex32(field(batch, "previous_state_root")),
            hex32(field(batch, "resulting_state_root")),
            hex32(field(batch, "sequencer_public_key"))));
    }

    private static String field(JsonNode batch, String name) {
        if (!batch.path(name).isTextual()) {
            throw MobileIntegrationException.of(MobileIntegrationException.Code.DECODE_FAILURE);
        }
        return batch.path(name).textValue();
    }

    public static byte[] hex32(String value) {
        if (value == null || value.length() != 64) {
            throw MobileIntegrationException.of(MobileIntegrationException.Code.DECODE_FAILURE);
        }
        byte[] bytes = new byte[32];
        for (int index = 0; index < 32; index++) {
            int high = Character.digit(value.charAt(index * 2), 16);
            int low = Character.digit(value.charAt(index * 2 + 1), 16);
            if (high < 0 || low < 0) {
                throw MobileIntegrationException.of(MobileIntegrationException.Code.DECODE_FAILURE);
            }
            bytes[index] = (byte) ((high << 4) | low);
        }
        return bytes;
    }

    private static URI requireRelay(URI uri) {
        String scheme = uri.getScheme() == null ? "" : uri.getScheme().toLowerCase(Locale.ROOT);
        String host = uri.getHost();
        if (host == null || host.isEmpty() || uri.getUserInfo() != null
                || uri.getQuery() != null || uri.getFragment() != null) {
            throw MobileIntegrationException.of(MobileIntegrationException.Code.INVALID_CONFIGURATION);
        }
        String normalized = host.toLowerCase(Locale.ROOT);
        boolean loopback = normalized.equals("localhost") || normalized.equals("::1")
            || normalized.equals("[::1]") || normalized.startsWith("127.");
        if (scheme.equals("https") || (scheme.equals("http") && loopback)) return uri;
        throw MobileIntegrationException.of(MobileIntegrationException.Code.INVALID_CONFIGURATION);
    }
}
