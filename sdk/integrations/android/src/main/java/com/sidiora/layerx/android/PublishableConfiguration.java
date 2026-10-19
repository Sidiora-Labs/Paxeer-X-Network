package com.sidiora.layerx.android;

import com.fasterxml.jackson.core.type.TypeReference;
import com.fasterxml.jackson.databind.ObjectMapper;
import java.io.IOException;
import java.net.URI;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.HashMap;
import java.util.HashSet;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Locale;
import java.util.Map;
import java.util.Set;

/** Declared-key configuration whose shape cannot carry an API secret or private key. */
public final class PublishableConfiguration {
    public static final String SERVICE_URL_KEY = "layerx.service_url";
    public static final String SESSION_BROKER_URL_KEY = "layerx.session_broker_url";
    public static final String EVENT_PUBLIC_KEY_PREFIX = "layerx.event_public_key.";
    public static final String EVENT_MAX_AGE_SECONDS_KEY = "layerx.event_max_age_seconds";
    public static final String REQUEST_TIMEOUT_SECONDS_KEY = "layerx.request_timeout_seconds";

    private static final long MAXIMUM_CONFIGURATION_BYTES = 262_144L;

    private final URI serviceUri;
    private final URI sessionBrokerUri;
    private final Map<String, byte[]> eventPublicKeys;
    private final long eventMaximumAgeMs;
    private final long requestTimeoutMs;

    private PublishableConfiguration(URI serviceUri, URI sessionBrokerUri, Map<String, byte[]> eventPublicKeys,
                                     long eventMaximumAgeMs, long requestTimeoutMs) {
        this.serviceUri = serviceUri;
        this.sessionBrokerUri = sessionBrokerUri;
        this.eventPublicKeys = Map.copyOf(eventPublicKeys);
        this.eventMaximumAgeMs = eventMaximumAgeMs;
        this.requestTimeoutMs = requestTimeoutMs;
    }

    public static PublishableConfiguration of(Map<String, String> declaredKeys) {
        URI service = null;
        URI broker = null;
        Map<String, byte[]> publicKeys = new LinkedHashMap<>();
        long maximumAgeSeconds = 300L;
        long timeoutSeconds = 30L;
        for (Map.Entry<String, String> declared : declaredKeys.entrySet()) {
            String name = declared.getKey();
            String value = declared.getValue();
            if (name == null || value == null || EmbeddedSecretDetector.isSecretShapedName(name)) {
                throw MobileIntegrationException.of(MobileIntegrationException.Code.EMBEDDED_SECRET);
            }
            switch (name) {
                case SERVICE_URL_KEY -> service = endpoint(value);
                case SESSION_BROKER_URL_KEY -> broker = endpoint(value);
                case EVENT_MAX_AGE_SECONDS_KEY -> maximumAgeSeconds = bounded(value, 1L, 3_600L);
                case REQUEST_TIMEOUT_SECONDS_KEY -> timeoutSeconds = bounded(value, 1L, 300L);
                default -> {
                    if (!name.startsWith(EVENT_PUBLIC_KEY_PREFIX)) throw invalid();
                    String identifier = name.substring(EVENT_PUBLIC_KEY_PREFIX.length());
                    if (!isKeyIdentifier(identifier) || publicKeys.containsKey(identifier)) throw invalid();
                    publicKeys.put(identifier, publicKey(value));
                }
            }
        }
        if (service == null || broker == null || publicKeys.isEmpty()) throw invalid();
        return new PublishableConfiguration(service, broker, publicKeys,
            maximumAgeSeconds * 1_000L, timeoutSeconds * 1_000L);
    }

    public static PublishableConfiguration ofJsonFile(Path path) {
        try {
            if (Files.size(path) > MAXIMUM_CONFIGURATION_BYTES) throw invalid();
            Map<String, String> declared = new ObjectMapper()
                .readValue(Files.readAllBytes(path), new TypeReference<LinkedHashMap<String, String>>() {});
            return of(declared);
        } catch (IOException | IllegalArgumentException error) {
            throw invalid();
        }
    }

    public static PublishableConfiguration ofEnvironment(Map<String, String> environment) {
        Map<String, String> declared = new HashMap<>();
        for (Map.Entry<String, String> entry : environment.entrySet()) {
            String key = declaredKeyForEnvironmentVariable(entry.getKey());
            if (key != null) declared.put(key, entry.getValue());
        }
        String eventKeyId = environment.get("LAYERX_EVENT_KEY_ID");
        String eventPublicKey = environment.get("LAYERX_EVENT_PUBLIC_KEY");
        if (eventKeyId != null || eventPublicKey != null) {
            if (eventKeyId == null || eventPublicKey == null || !isKeyIdentifier(eventKeyId)
                    || declared.putIfAbsent(EVENT_PUBLIC_KEY_PREFIX + eventKeyId, eventPublicKey) != null) {
                throw invalid();
            }
        }
        return of(declared);
    }

    public static String declaredKeyForEnvironmentVariable(String name) {
        if (!name.startsWith("LAYERX_")) return null;
        String remainder = name.substring("LAYERX_".length()).toLowerCase(Locale.ROOT);
        return switch (remainder) {
            case "service_url" -> SERVICE_URL_KEY;
            case "session_broker_url" -> SESSION_BROKER_URL_KEY;
            case "event_max_age_seconds" -> EVENT_MAX_AGE_SECONDS_KEY;
            case "request_timeout_seconds" -> REQUEST_TIMEOUT_SECONDS_KEY;
            default -> {
                String prefix = "event_public_key_";
                if (!remainder.startsWith(prefix)) yield null;
                String identifier = remainder.substring(prefix.length()).replace('_', '-');
                yield isKeyIdentifier(identifier) ? EVENT_PUBLIC_KEY_PREFIX + identifier : null;
            }
        };
    }

    public static List<String> declaredKeyNames() {
        return List.of(SERVICE_URL_KEY, SESSION_BROKER_URL_KEY, EVENT_PUBLIC_KEY_PREFIX + "<key-id>",
            EVENT_MAX_AGE_SECONDS_KEY, REQUEST_TIMEOUT_SECONDS_KEY);
    }

    public URI serviceUri() { return serviceUri; }
    public URI sessionBrokerUri() { return sessionBrokerUri; }
    public long eventMaximumAgeMs() { return eventMaximumAgeMs; }
    public long requestTimeoutMs() { return requestTimeoutMs; }

    public Map<String, byte[]> eventPublicKeys() {
        Map<String, byte[]> copy = new LinkedHashMap<>();
        for (Map.Entry<String, byte[]> entry : eventPublicKeys.entrySet()) {
            copy.put(entry.getKey(), entry.getValue().clone());
        }
        return Map.copyOf(copy);
    }

    public Set<String> exemptScannerValues() {
        Set<String> values = new HashSet<>();
        values.add(serviceUri.toString());
        values.add(sessionBrokerUri.toString());
        for (byte[] key : eventPublicKeys.values()) values.add(hexadecimal(key));
        return Set.copyOf(values);
    }

    static String hexadecimal(byte[] bytes) {
        StringBuilder builder = new StringBuilder(bytes.length * 2);
        for (byte value : bytes) {
            builder.append(Character.forDigit((value >> 4) & 0xf, 16));
            builder.append(Character.forDigit(value & 0xf, 16));
        }
        return builder.toString();
    }

    static URI endpoint(String value) {
        if (EmbeddedSecretDetector.classify(value) != null || value.length() > 2_048) throw invalid();
        URI uri;
        try {
            uri = new URI(value);
        } catch (java.net.URISyntaxException error) {
            throw invalid();
        }
        if (uri.getHost() == null || uri.getHost().isEmpty() || uri.getUserInfo() != null
                || uri.getQuery() != null || uri.getFragment() != null) {
            throw invalid();
        }
        String scheme = uri.getScheme() == null ? "" : uri.getScheme().toLowerCase(Locale.ROOT);
        if (scheme.equals("https")) return uri;
        if (scheme.equals("http") && isLoopback(uri.getHost())) return uri;
        throw invalid();
    }

    static boolean isLoopback(String host) {
        String normalized = host.toLowerCase(Locale.ROOT);
        return normalized.equals("localhost") || normalized.equals("::1") || normalized.equals("[::1]")
            || normalized.startsWith("127.");
    }

    private static byte[] publicKey(String value) {
        if (value.length() != 64) throw invalid();
        byte[] bytes = new byte[32];
        int aggregate = 0;
        for (int index = 0; index < 32; index++) {
            int high = Character.digit(value.charAt(index * 2), 16);
            int low = Character.digit(value.charAt(index * 2 + 1), 16);
            if (high < 0 || low < 0
                    || Character.isUpperCase(value.charAt(index * 2))
                    || Character.isUpperCase(value.charAt(index * 2 + 1))) {
                throw invalid();
            }
            bytes[index] = (byte) ((high << 4) | low);
            aggregate |= bytes[index];
        }
        if (aggregate == 0) throw invalid();
        return bytes;
    }

    private static long bounded(String value, long minimum, long maximum) {
        if (EmbeddedSecretDetector.classify(value) != null || value.isEmpty() || value.length() > 10) throw invalid();
        for (int index = 0; index < value.length(); index++) {
            if (value.charAt(index) < '0' || value.charAt(index) > '9') throw invalid();
        }
        long parsed = Long.parseLong(value);
        if (parsed < minimum || parsed > maximum) throw invalid();
        return parsed;
    }

    private static boolean isKeyIdentifier(String value) {
        if (value.isEmpty() || value.length() > 64) return false;
        for (int index = 0; index < value.length(); index++) {
            char character = value.charAt(index);
            boolean allowed = (character >= 'a' && character <= 'z')
                || (character >= 'A' && character <= 'Z')
                || (character >= '0' && character <= '9')
                || character == '.' || character == '_' || character == '-';
            if (!allowed) return false;
        }
        return true;
    }

    private static MobileIntegrationException invalid() {
        return MobileIntegrationException.of(MobileIntegrationException.Code.INVALID_CONFIGURATION);
    }
}
