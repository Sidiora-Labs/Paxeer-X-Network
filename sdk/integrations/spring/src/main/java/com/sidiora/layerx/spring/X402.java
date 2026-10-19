package com.sidiora.layerx.spring;

import com.fasterxml.jackson.core.JsonProcessingException;
import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.databind.node.ArrayNode;
import com.fasterxml.jackson.databind.node.JsonNodeFactory;
import com.fasterxml.jackson.databind.node.ObjectNode;
import com.fasterxml.jackson.databind.node.TextNode;
import java.math.BigInteger;
import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.security.NoSuchAlgorithmException;
import java.util.ArrayList;
import java.util.Base64;
import java.util.Collections;
import java.util.HashSet;
import java.util.Iterator;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.Set;
import java.util.regex.Pattern;

public final class X402 {
    private X402() {}

    public static final int VERSION = 2;
    public static final String PAYMENT_REQUIRED_HEADER = "PAYMENT-REQUIRED";
    public static final String PAYMENT_SIGNATURE_HEADER = "PAYMENT-SIGNATURE";
    public static final String PAYMENT_RESPONSE_HEADER = "PAYMENT-RESPONSE";
    public static final int MAX_HEADER_BYTES = 64 * 1024;

    static final byte[] MERKLE_LEAF_DOMAIN = "LXP/v1/merkle-leaf\0".getBytes(StandardCharsets.US_ASCII);
    static final byte[] PAYMENT_KEY_DOMAIN =
        "LayerX/middleware/x402/idempotency\0".getBytes(StandardCharsets.US_ASCII);

    private static final ObjectMapper MAPPER = new ObjectMapper();
    private static final JsonNodeFactory NODES = JsonNodeFactory.instance;
    private static final BigInteger MAX_U128 = BigInteger.ONE.shiftLeft(128).subtract(BigInteger.ONE);
    private static final Pattern AMOUNT = Pattern.compile("0|[1-9][0-9]*");
    private static final Pattern IDENTIFIER = Pattern.compile("[A-Za-z0-9._-]+");
    private static final Pattern PRINTABLE = Pattern.compile("[\\x20-\\x7e]+");
    private static final Pattern URL = Pattern.compile("https?://[^\\s\\x00-\\x1f\\x7f]+");
    private static final Pattern HEX32 = Pattern.compile("[0-9a-fA-F]{64}");
    private static final Pattern LOWER_HEX32 = Pattern.compile("[0-9a-f]{64}");
    private static final Pattern BASE64 = Pattern.compile("[A-Za-z0-9+/]*={0,2}");
    private static final Pattern REFUSAL = Pattern.compile("[a-z][a-z0-9_]{0,63}");

    public record ResourceInfo(String url, String description, String mimeType, String serviceName,
                               List<String> tags, String iconUrl) {
        public ResourceInfo { tags = tags == null ? null : List.copyOf(tags); }

        public static ResourceInfo of(String url) { return new ResourceInfo(url, null, null, null, null, null); }

        public ObjectNode toNode() {
            ObjectNode node = NODES.objectNode();
            node.put("url", url);
            if (description != null) node.put("description", description);
            if (mimeType != null) node.put("mimeType", mimeType);
            if (serviceName != null) node.put("serviceName", serviceName);
            if (tags != null) {
                ArrayNode array = node.putArray("tags");
                for (String tag : tags) array.add(tag);
            }
            if (iconUrl != null) node.put("iconUrl", iconUrl);
            return node;
        }
    }

    public record Extension(JsonNode info, JsonNode schema) {
        public ObjectNode toNode() {
            ObjectNode node = NODES.objectNode();
            node.set("info", info);
            node.set("schema", schema);
            return node;
        }
    }

    public record PaymentRequirements(String scheme, String network, String amount, String asset, String payTo,
                                      long maxTimeoutSeconds, JsonNode extra) {
        public ObjectNode toNode() {
            ObjectNode node = NODES.objectNode();
            node.put("scheme", scheme);
            node.put("network", network);
            node.put("amount", amount);
            node.put("asset", asset);
            node.put("payTo", payTo);
            node.put("maxTimeoutSeconds", maxTimeoutSeconds);
            if (extra != null) node.set("extra", extra);
            return node;
        }
    }

    public record PaymentRequired(ResourceInfo resource, List<PaymentRequirements> accepts, String error,
                                  Map<String, Extension> extensions) {
        public PaymentRequired {
            accepts = List.copyOf(accepts);
            extensions = extensions == null ? null : Map.copyOf(extensions);
        }

        public ObjectNode toNode() {
            ObjectNode node = NODES.objectNode();
            node.put("x402Version", VERSION);
            node.set("resource", resource.toNode());
            ArrayNode array = node.putArray("accepts");
            for (PaymentRequirements item : accepts) array.add(item.toNode());
            if (error != null) node.put("error", error);
            if (extensions != null) {
                ObjectNode container = node.putObject("extensions");
                for (Map.Entry<String, Extension> entry : sorted(extensions).entrySet()) {
                    container.set(entry.getKey(), entry.getValue().toNode());
                }
            }
            return node;
        }
    }

    public record PaymentPayload(Map<String, JsonNode> payload, PaymentRequirements accepted, ResourceInfo resource,
                                 Map<String, Extension> extensions) {
        public PaymentPayload {
            payload = Map.copyOf(payload);
            extensions = extensions == null ? null : Map.copyOf(extensions);
        }

        public ObjectNode toNode() {
            ObjectNode node = NODES.objectNode();
            node.put("x402Version", VERSION);
            ObjectNode body = node.putObject("payload");
            for (Map.Entry<String, JsonNode> entry : sorted(payload).entrySet()) {
                body.set(entry.getKey(), entry.getValue());
            }
            node.set("accepted", accepted.toNode());
            if (resource != null) node.set("resource", resource.toNode());
            if (extensions != null) {
                ObjectNode container = node.putObject("extensions");
                for (Map.Entry<String, Extension> entry : sorted(extensions).entrySet()) {
                    container.set(entry.getKey(), entry.getValue().toNode());
                }
            }
            return node;
        }
    }

    public record ReceiptEvidence(String receipt, String receiptDigest, String verificationLevel) {
        public static ReceiptEvidence sequencerSigned(String receipt, String receiptDigest) {
            return new ReceiptEvidence(receipt, receiptDigest, "sequencer-signed");
        }

        public ObjectNode toNode() {
            ObjectNode node = NODES.objectNode();
            node.put("receipt", receipt);
            node.put("receiptDigest", receiptDigest);
            node.put("verificationLevel", verificationLevel);
            return node;
        }
    }

    public record SettlementResponse(boolean success, String errorReason, String payer, String transaction,
                                     String network, String amount, Map<String, JsonNode> extensions) {
        public SettlementResponse { extensions = extensions == null ? null : Map.copyOf(extensions); }

        public ObjectNode toNode() {
            ObjectNode node = NODES.objectNode();
            node.put("success", success);
            node.put("transaction", transaction);
            node.put("network", network);
            if (errorReason != null) node.put("errorReason", errorReason);
            if (payer != null) node.put("payer", payer);
            if (amount != null) node.put("amount", amount);
            if (extensions != null) {
                ObjectNode container = node.putObject("extensions");
                for (Map.Entry<String, JsonNode> entry : sorted(extensions).entrySet()) {
                    container.set(entry.getKey(), entry.getValue());
                }
            }
            return node;
        }
    }

    public static String encodePaymentRequiredHeader(PaymentRequired value) {
        return encodeHeader(parsePaymentRequired(value.toNode()).toNode());
    }

    public static PaymentRequired decodePaymentRequiredHeader(String value) {
        return parsePaymentRequired(decodeHeader(value, MiddlewareException.Code.INVALID_PAYMENT_REQUIRED));
    }

    public static String encodePaymentPayloadHeader(PaymentPayload value) {
        return encodeHeader(parsePaymentPayload(value.toNode()).toNode());
    }

    public static PaymentPayload decodePaymentPayloadHeader(String value) {
        return parsePaymentPayload(decodeHeader(value, MiddlewareException.Code.INVALID_PAYMENT_PAYLOAD));
    }

    public static String encodeSettlementHeader(SettlementResponse value) {
        return encodeHeader(parseSettlement(value.toNode()).toNode());
    }

    public static SettlementResponse decodeSettlementHeader(String value) {
        return parseSettlement(decodeHeader(value, MiddlewareException.Code.INVALID_PAYMENT_PAYLOAD));
    }

    public static PaymentRequired parsePaymentRequired(JsonNode value) {
        MiddlewareException.Code code = MiddlewareException.Code.INVALID_PAYMENT_REQUIRED;
        ObjectNode object = asObject(value, code);
        exactKeys(object, List.of("x402Version", "resource", "accepts"), List.of("error", "extensions"), code);
        if (!object.get("x402Version").isInt() || object.get("x402Version").intValue() != VERSION) fail(code);
        JsonNode accepts = object.get("accepts");
        if (!accepts.isArray() || accepts.isEmpty() || accepts.size() > 32) fail(code);
        List<PaymentRequirements> requirements = new ArrayList<>(accepts.size());
        for (JsonNode item : accepts) requirements.add(parseRequirements(item));
        return new PaymentRequired(
            parseResource(object.get("resource")),
            requirements,
            object.has("error") ? boundedString(object.get("error"), 512, code) : null,
            object.has("extensions") ? parseExtensions(object.get("extensions")) : null);
    }

    public static PaymentPayload parsePaymentPayload(JsonNode value) {
        MiddlewareException.Code code = MiddlewareException.Code.INVALID_PAYMENT_PAYLOAD;
        ObjectNode object = asObject(value, code);
        exactKeys(object, List.of("x402Version", "payload", "accepted"), List.of("resource", "extensions"), code);
        if (!object.get("x402Version").isInt() || object.get("x402Version").intValue() != VERSION) fail(code);
        ObjectNode payload = asObject(object.get("payload"), code);
        Map<String, JsonNode> entries = new LinkedHashMap<>();
        Iterator<String> names = payload.fieldNames();
        while (names.hasNext()) {
            String name = names.next();
            entries.put(name, payload.get(name));
        }
        return new PaymentPayload(
            entries,
            parseRequirements(object.get("accepted")),
            object.has("resource") ? parseResource(object.get("resource")) : null,
            object.has("extensions") ? parseExtensions(object.get("extensions")) : null);
    }

    public static SettlementResponse parseSettlement(JsonNode value) {
        MiddlewareException.Code code = MiddlewareException.Code.INVALID_PAYMENT_PAYLOAD;
        ObjectNode object = asObject(value, code);
        exactKeys(object, List.of("success", "transaction", "network"),
            List.of("errorReason", "payer", "amount", "extensions"), code);
        if (!object.get("success").isBoolean()) fail(code);
        boolean success = object.get("success").booleanValue();
        if (!object.get("transaction").isTextual()) fail(code);
        String transaction = object.get("transaction").textValue();
        String errorReason = object.has("errorReason") ? boundedString(object.get("errorReason"), 512, code) : null;
        if (success ? transaction.isEmpty() || errorReason != null : errorReason == null) fail(code);
        if (!success && !"settlement_pending".equals(errorReason) && !transaction.isEmpty()) fail(code);
        if (!success && "settlement_pending".equals(errorReason) && transaction.isEmpty()) fail(code);
        Map<String, JsonNode> extensions = null;
        if (object.has("extensions")) {
            ObjectNode container = asObject(object.get("extensions"), code);
            extensions = new LinkedHashMap<>();
            Iterator<String> names = container.fieldNames();
            while (names.hasNext()) {
                String name = names.next();
                extensions.put(name, container.get(name));
            }
        }
        return new SettlementResponse(
            success,
            errorReason,
            object.has("payer") ? boundedString(object.get("payer"), 256, code) : null,
            transaction,
            parseNetwork(object.get("network")),
            object.has("amount") ? parseAmount(object.get("amount")) : null,
            extensions);
    }

    public static ResourceInfo parseResource(JsonNode value) {
        MiddlewareException.Code code = MiddlewareException.Code.INVALID_PAYMENT_REQUIRED;
        ObjectNode object = asObject(value, code);
        exactKeys(object, List.of("url"),
            List.of("description", "mimeType", "serviceName", "tags", "iconUrl"), code);
        List<String> tags = null;
        if (object.has("tags")) {
            JsonNode array = object.get("tags");
            if (!array.isArray() || array.size() > 5) fail(code);
            tags = new ArrayList<>(array.size());
            for (JsonNode tag : array) tags.add(printableString(tag, 32));
        }
        return new ResourceInfo(
            parseUrl(object.get("url")),
            object.has("description") ? boundedString(object.get("description"), 512, code) : null,
            object.has("mimeType") ? boundedString(object.get("mimeType"), 32, code) : null,
            object.has("serviceName") ? printableString(object.get("serviceName"), 32) : null,
            tags,
            object.has("iconUrl") ? parseUrl(object.get("iconUrl")) : null);
    }

    public static PaymentRequirements parseRequirements(JsonNode value) {
        MiddlewareException.Code code = MiddlewareException.Code.INVALID_PAYMENT_REQUIRED;
        ObjectNode object = asObject(value, code);
        exactKeys(object, List.of("scheme", "network", "amount", "asset", "payTo", "maxTimeoutSeconds"),
            List.of("extra"), code);
        JsonNode timeout = object.get("maxTimeoutSeconds");
        if (!timeout.isIntegralNumber() || timeout.longValue() <= 0 || timeout.longValue() > 0xffff_ffffL) fail(code);
        String asset = boundedString(object.get("asset"), 256, code);
        String payTo = boundedString(object.get("payTo"), 256, code);
        parseHex32(asset, code);
        parseHex32(payTo, code);
        return new PaymentRequirements(
            identifierString(object.get("scheme"), 32, code),
            parseNetwork(object.get("network")),
            parseAmount(object.get("amount")),
            asset,
            payTo,
            timeout.longValue(),
            object.has("extra") ? object.get("extra") : null);
    }

    public static Map<String, Extension> parseExtensions(JsonNode value) {
        MiddlewareException.Code code = MiddlewareException.Code.INVALID_PAYMENT_REQUIRED;
        ObjectNode object = asObject(value, code);
        if (object.size() > 32) fail(code);
        Map<String, Extension> extensions = new LinkedHashMap<>();
        Iterator<String> names = object.fieldNames();
        while (names.hasNext()) {
            String name = names.next();
            if (!bounded(name, 32) || !IDENTIFIER.matcher(name).matches()) fail(code);
            ObjectNode entry = asObject(object.get(name), code);
            exactKeys(entry, List.of("info", "schema"), List.of(), code);
            extensions.put(name, new Extension(entry.get("info"), entry.get("schema")));
        }
        return extensions;
    }

    public static PaymentRequirements matchRequirements(PaymentRequired required, PaymentPayload payload) {
        String accepted = canonicalJson(payload.accepted().toNode());
        PaymentRequirements match = null;
        for (PaymentRequirements candidate : required.accepts()) {
            if (canonicalJson(candidate.toNode()).equals(accepted)) {
                match = candidate;
                break;
            }
        }
        if (match == null) fail(MiddlewareException.Code.REQUIREMENTS_MISMATCH);
        Map<String, Extension> declared = required.extensions();
        if (declared != null) {
            for (Map.Entry<String, Extension> entry : declared.entrySet()) {
                Map<String, Extension> actual = payload.extensions();
                Extension present = actual == null ? null : actual.get(entry.getKey());
                if (present == null
                        || !canonicalJson(present.toNode()).equals(canonicalJson(entry.getValue().toNode()))) {
                    fail(MiddlewareException.Code.REQUIREMENTS_MISMATCH);
                }
            }
        }
        return match;
    }

    public static ReceiptEvidence parseReceiptEvidence(Map<String, JsonNode> payload) {
        MiddlewareException.Code code = MiddlewareException.Code.INVALID_PAYMENT_PAYLOAD;
        ObjectNode object = NODES.objectNode();
        for (Map.Entry<String, JsonNode> entry : payload.entrySet()) object.set(entry.getKey(), entry.getValue());
        exactKeys(object, List.of("receipt", "receiptDigest", "verificationLevel"), List.of("idempotencyKey"), code);
        JsonNode level = object.get("verificationLevel");
        if (!level.isTextual() || !"sequencer-signed".equals(level.textValue())) {
            fail(MiddlewareException.Code.VERIFICATION_FAILURE);
        }
        JsonNode digest = object.get("receiptDigest");
        if (!digest.isTextual()) fail(code);
        parseHex32(digest.textValue(), code);
        return ReceiptEvidence.sequencerSigned(
            boundedString(object.get("receipt"), MAX_HEADER_BYTES, code), digest.textValue());
    }

    public static SettlementResponse refusal(PaymentRequirements requirements, String reason) {
        String safe = reason != null && REFUSAL.matcher(reason).matches() ? reason : "payment_refused";
        return new SettlementResponse(false, safe, null, "", requirements.network(), null, null);
    }

    public static String canonicalJson(JsonNode node) {
        if (node == null || node.isNull()) return "null";
        if (node.isObject()) {
            List<String> names = new ArrayList<>();
            node.fieldNames().forEachRemaining(names::add);
            Collections.sort(names);
            StringBuilder builder = new StringBuilder("{");
            for (int index = 0; index < names.size(); index += 1) {
                if (index > 0) builder.append(',');
                builder.append(TextNode.valueOf(names.get(index)))
                    .append(':')
                    .append(canonicalJson(node.get(names.get(index))));
            }
            return builder.append('}').toString();
        }
        if (node.isArray()) {
            StringBuilder builder = new StringBuilder("[");
            for (int index = 0; index < node.size(); index += 1) {
                if (index > 0) builder.append(',');
                builder.append(canonicalJson(node.get(index)));
            }
            return builder.append(']').toString();
        }
        if (node.isNumber() && !node.isIntegralNumber() && !Double.isFinite(node.doubleValue())) {
            fail(MiddlewareException.Code.INVALID_PAYMENT_PAYLOAD);
        }
        if (!node.isValueNode()) fail(MiddlewareException.Code.INVALID_PAYMENT_PAYLOAD);
        return node.toString();
    }

    public static byte[] sha256(byte[]... values) {
        try {
            MessageDigest digest = MessageDigest.getInstance("SHA-256");
            for (byte[] value : values) digest.update(value);
            return digest.digest();
        } catch (NoSuchAlgorithmException error) {
            throw new IllegalStateException(error);
        }
    }

    public static byte[] merkleLeafDigest(byte[] canonicalReceipt) {
        return sha256(MERKLE_LEAF_DOMAIN, canonicalReceipt);
    }

    public static String paymentIdempotencyKey(String principal, byte[] requestDigest) {
        return hex(sha256(PAYMENT_KEY_DOMAIN, principal.getBytes(StandardCharsets.UTF_8), requestDigest));
    }

    public static String hex(byte[] value) {
        StringBuilder builder = new StringBuilder(value.length * 2);
        for (byte item : value) builder.append(Character.forDigit((item >> 4) & 0xf, 16))
            .append(Character.forDigit(item & 0xf, 16));
        return builder.toString();
    }

    public static byte[] parseHex32(String value, MiddlewareException.Code code) {
        String digits = value.startsWith("0x") ? value.substring(2) : value;
        if (!HEX32.matcher(digits).matches()) fail(code);
        byte[] result = new byte[32];
        for (int index = 0; index < 32; index += 1) {
            result[index] = (byte) Integer.parseInt(digits.substring(index * 2, index * 2 + 2), 16);
        }
        return result;
    }

    public static boolean isLowerHex32(String value) {
        return value != null && LOWER_HEX32.matcher(value).matches();
    }

    public static String encodeBase64(byte[] value) {
        return Base64.getEncoder().encodeToString(value);
    }

    public static byte[] decodeBase64(String value, MiddlewareException.Code code) {
        if (value.isEmpty() || value.length() > MAX_HEADER_BYTES * 2 || !BASE64.matcher(value).matches()) fail(code);
        try {
            return Base64.getDecoder().decode(value);
        } catch (IllegalArgumentException error) {
            throw MiddlewareException.of(code);
        }
    }

    public static String parseAmount(JsonNode value) {
        MiddlewareException.Code code = MiddlewareException.Code.INVALID_PAYMENT_REQUIRED;
        if (!value.isTextual() || !AMOUNT.matcher(value.textValue()).matches()) fail(code);
        BigInteger amount = new BigInteger(value.textValue());
        if (amount.signum() <= 0 || amount.compareTo(MAX_U128) > 0) fail(code);
        return value.textValue();
    }

    public static String parseNetwork(JsonNode value) {
        if (!value.isTextual()) fail(MiddlewareException.Code.INVALID_PAYMENT_REQUIRED);
        String text = value.textValue();
        String[] parts = text.split(":", -1);
        if (parts.length != 2 || !"layerx".equals(parts[0]) || !bounded(parts[1], 64)
                || !IDENTIFIER.matcher(parts[1]).matches()) {
            fail(MiddlewareException.Code.UNSUPPORTED_PAYMENT);
        }
        return text;
    }

    public static String parseUrl(JsonNode value) {
        MiddlewareException.Code code = MiddlewareException.Code.INVALID_PAYMENT_REQUIRED;
        String text = boundedString(value, 2048, code);
        if (!URL.matcher(text).matches()) fail(code);
        return text;
    }

    public static boolean constantTimeEquals(byte[] left, byte[] right) {
        return MessageDigest.isEqual(left, right);
    }

    public static boolean constantTimeEquals(String left, String right) {
        return MessageDigest.isEqual(left.getBytes(StandardCharsets.UTF_8), right.getBytes(StandardCharsets.UTF_8));
    }

    static String encodeHeader(JsonNode value) {
        byte[] bytes;
        try {
            bytes = MAPPER.writeValueAsBytes(value);
        } catch (JsonProcessingException error) {
            throw MiddlewareException.of(MiddlewareException.Code.INVALID_PAYMENT_PAYLOAD);
        }
        if (bytes.length > MAX_HEADER_BYTES) fail(MiddlewareException.Code.INVALID_PAYMENT_PAYLOAD);
        return encodeBase64(bytes);
    }

    static JsonNode decodeHeader(String value, MiddlewareException.Code code) {
        byte[] bytes = decodeBase64(value, code);
        if (bytes.length > MAX_HEADER_BYTES) fail(code);
        try {
            return MAPPER.readTree(bytes);
        } catch (java.io.IOException error) {
            throw MiddlewareException.of(code);
        }
    }

    static ObjectNode asObject(JsonNode value, MiddlewareException.Code code) {
        if (value == null || !value.isObject()) fail(code);
        return (ObjectNode) value;
    }

    static void exactKeys(ObjectNode value, List<String> required, List<String> optional,
                          MiddlewareException.Code code) {
        Set<String> allowed = new HashSet<>(required);
        allowed.addAll(optional);
        for (String key : required) {
            if (!value.has(key)) fail(code);
        }
        Iterator<String> names = value.fieldNames();
        while (names.hasNext()) {
            if (!allowed.contains(names.next())) fail(code);
        }
    }

    static String boundedString(JsonNode value, int limit, MiddlewareException.Code code) {
        if (value == null || !value.isTextual() || !bounded(value.textValue(), limit)) fail(code);
        return value.textValue();
    }

    static String printableString(JsonNode value, int limit) {
        MiddlewareException.Code code = MiddlewareException.Code.INVALID_PAYMENT_REQUIRED;
        String text = boundedString(value, limit, code);
        if (!PRINTABLE.matcher(text).matches()) fail(code);
        return text;
    }

    static String identifierString(JsonNode value, int limit, MiddlewareException.Code code) {
        String text = boundedString(value, limit, code);
        if (!IDENTIFIER.matcher(text).matches()) fail(code);
        return text;
    }

    static boolean bounded(String value, int limit) {
        return !value.isEmpty() && value.length() <= limit && value.indexOf('\0') < 0;
    }

    static <T> Map<String, T> sorted(Map<String, T> value) {
        Map<String, T> ordered = new LinkedHashMap<>();
        List<String> names = new ArrayList<>(value.keySet());
        Collections.sort(names);
        for (String name : names) ordered.put(name, value.get(name));
        return ordered;
    }

    static void fail(MiddlewareException.Code code) {
        throw MiddlewareException.of(code);
    }
}
