package com.sidiora.layerx.sdk;

import com.fasterxml.jackson.annotation.JsonCreator;
import com.fasterxml.jackson.annotation.JsonValue;
import com.fasterxml.jackson.core.JsonParser;
import com.fasterxml.jackson.core.JsonToken;
import com.fasterxml.jackson.databind.DeserializationContext;
import com.fasterxml.jackson.databind.JsonDeserializer;
import com.fasterxml.jackson.databind.annotation.JsonDeserialize;
import java.io.IOException;
import java.math.BigInteger;
import java.util.Objects;
import java.util.regex.Pattern;

/** An unsigned, integer-only 128-bit amount expressed in protocol base units. */
@JsonDeserialize(using = ProtocolAmount.DecimalDeserializer.class)
public record ProtocolAmount(BigInteger value) implements Comparable<ProtocolAmount> {
    public static final BigInteger MAX_VALUE = BigInteger.ONE.shiftLeft(128).subtract(BigInteger.ONE);
    private static final Pattern CANONICAL = Pattern.compile("0|[1-9][0-9]*");

    public ProtocolAmount {
        Objects.requireNonNull(value, "value");
        if (value.signum() < 0 || value.compareTo(MAX_VALUE) > 0) {
            throw PlatformSdkException.invalidArgument();
        }
    }

    @JsonCreator
    public static ProtocolAmount parse(String value) {
        if (value == null || value.length() > 39 || !CANONICAL.matcher(value).matches()) {
            throw PlatformSdkException.invalidArgument();
        }
        return new ProtocolAmount(new BigInteger(value));
    }

    public static ProtocolAmount of(BigInteger value) {
        return new ProtocolAmount(value);
    }

    public static final class DecimalDeserializer extends JsonDeserializer<ProtocolAmount> {
        @Override
        public ProtocolAmount deserialize(JsonParser parser, DeserializationContext context) throws IOException {
            if (!parser.hasToken(JsonToken.VALUE_STRING)) throw PlatformSdkException.invalidArgument();
            return ProtocolAmount.parse(parser.getText());
        }
    }

    @Override @JsonValue
    public String toString() {
        return value.toString(10);
    }

    @Override
    public int compareTo(ProtocolAmount other) {
        return value.compareTo(other.value);
    }
}
