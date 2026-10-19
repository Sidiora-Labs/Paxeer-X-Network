package com.sidiora.layerx.spring;

import java.util.LinkedHashMap;
import java.util.LinkedHashSet;
import java.util.Locale;
import java.util.Map;
import java.util.Set;
import java.util.regex.Pattern;
import org.springframework.core.env.ConfigurableEnvironment;
import org.springframework.core.env.EnumerablePropertySource;
import org.springframework.core.env.Environment;
import org.springframework.core.env.PropertySource;

public final class PublishedSecretGuard {
    private PublishedSecretGuard() {}

    private static final String[] PUBLISHED_PREFIXES =
        {"NEXT_PUBLIC_", "PUBLIC_", "VITE_", "REACT_APP_", "EXPO_PUBLIC_"};
    private static final Pattern KEY_MATERIAL =
        Pattern.compile(".*(^|_)(TOKEN|SECRET|PRIVATE|CREDENTIAL|PASSWORD|SIGNING_KEY|API_KEY)(_|$).*");

    public static void assertNoPublishedSecrets(Environment environment) {
        if (!(environment instanceof ConfigurableEnvironment configurable)) return;
        Map<String, String> values = new LinkedHashMap<>();
        for (PropertySource<?> source : configurable.getPropertySources()) {
            if (!(source instanceof EnumerablePropertySource<?> enumerable)) continue;
            for (String name : enumerable.getPropertyNames()) {
                Object value = enumerable.getProperty(name);
                if (value instanceof String text && !text.isEmpty()) values.putIfAbsent(normalize(name), text);
            }
        }
        Set<String> secrets = new LinkedHashSet<>();
        for (Map.Entry<String, String> entry : values.entrySet()) {
            if (!isPublishedName(entry.getKey()) && looksLikeKeyMaterial(entry.getKey())) {
                secrets.add(entry.getValue());
            }
        }
        for (Map.Entry<String, String> entry : values.entrySet()) {
            if (!isPublishedName(entry.getKey())) continue;
            if (secrets.contains(entry.getValue()) || looksLikeKeyMaterial(entry.getKey())) {
                throw MiddlewareException.of(MiddlewareException.Code.PUBLISHED_SECRET);
            }
        }
    }

    public static boolean isPublishedName(String name) {
        for (String prefix : PUBLISHED_PREFIXES) {
            if (name.startsWith(prefix)) return true;
        }
        return false;
    }

    public static boolean looksLikeKeyMaterial(String name) {
        return KEY_MATERIAL.matcher(name).matches();
    }

    static String normalize(String name) {
        return name.toUpperCase(Locale.ROOT).replace('.', '_').replace('-', '_');
    }
}
