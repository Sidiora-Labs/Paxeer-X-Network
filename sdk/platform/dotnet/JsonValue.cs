#nullable enable

using System.Collections.ObjectModel;
using System.Text.Json;
using System.Text.Json.Serialization;

namespace LayerX.Sdk;

[JsonConverter(typeof(JsonValueConverter))]
public abstract record JsonValue
{
    public sealed record NullValue : JsonValue;
    public sealed record BooleanValue(bool Value) : JsonValue;
    public sealed record IntegerValue(long Value) : JsonValue;
    public sealed record StringValue(string Value) : JsonValue;
    public sealed record ArrayValue(IReadOnlyList<JsonValue> Value) : JsonValue;
    public sealed record ObjectValue(IReadOnlyDictionary<string, JsonValue> Value) : JsonValue;

    public static JsonValue Null { get; } = new NullValue();
    public static JsonValue EmptyObject { get; } = Object(new Dictionary<string, JsonValue>());
    public static JsonValue Boolean(bool value) => new BooleanValue(value);
    public static JsonValue Integer(long value) => new IntegerValue(value);
    public static JsonValue String(string value) => new StringValue(value ?? throw InvalidArgument());
    public static JsonValue Array(IEnumerable<JsonValue> values) =>
        new ArrayValue(System.Array.AsReadOnly((values ?? throw InvalidArgument()).ToArray()));
    public static JsonValue Object(IReadOnlyDictionary<string, JsonValue> values) =>
        new ObjectValue(new ReadOnlyDictionary<string, JsonValue>(
            (values ?? throw InvalidArgument()).ToDictionary(item => item.Key, item => item.Value, StringComparer.Ordinal)));

    private static PlatformSdkException InvalidArgument() =>
        new(SdkErrorCode.InvalidArgument, RetryClass.Never);
}

public sealed class JsonValueConverter : JsonConverter<JsonValue>
{
    public override JsonValue Read(ref Utf8JsonReader reader, Type typeToConvert, JsonSerializerOptions options)
    {
        using var document = JsonDocument.ParseValue(ref reader);
        return Convert(document.RootElement);
    }

    public override void Write(Utf8JsonWriter writer, JsonValue value, JsonSerializerOptions options)
    {
        switch (value)
        {
            case JsonValue.NullValue:
                writer.WriteNullValue();
                break;
            case JsonValue.BooleanValue boolean:
                writer.WriteBooleanValue(boolean.Value);
                break;
            case JsonValue.IntegerValue integer:
                writer.WriteNumberValue(integer.Value);
                break;
            case JsonValue.StringValue text:
                writer.WriteStringValue(text.Value);
                break;
            case JsonValue.ArrayValue array:
                writer.WriteStartArray();
                foreach (var item in array.Value) JsonSerializer.Serialize(writer, item, options);
                writer.WriteEndArray();
                break;
            case JsonValue.ObjectValue map:
                writer.WriteStartObject();
                foreach (var item in map.Value.OrderBy(item => item.Key, StringComparer.Ordinal))
                {
                    writer.WritePropertyName(item.Key);
                    JsonSerializer.Serialize(writer, item.Value, options);
                }
                writer.WriteEndObject();
                break;
            default:
                throw new JsonException("Unsupported LayerX JSON value.");
        }
    }

    private static JsonValue Convert(JsonElement value) => value.ValueKind switch
    {
        JsonValueKind.Null => JsonValue.Null,
        JsonValueKind.True => JsonValue.Boolean(true),
        JsonValueKind.False => JsonValue.Boolean(false),
        JsonValueKind.String => JsonValue.String(value.GetString() ?? throw DecodeFailure()),
        JsonValueKind.Number when value.TryGetInt64(out var integer) => JsonValue.Integer(integer),
        JsonValueKind.Number => throw DecodeFailure(),
        JsonValueKind.Array => JsonValue.Array(value.EnumerateArray().Select(Convert)),
        JsonValueKind.Object => JsonValue.Object(value.EnumerateObject().ToDictionary(
            property => property.Name, property => Convert(property.Value), StringComparer.Ordinal)),
        _ => throw DecodeFailure(),
    };

    private static JsonException DecodeFailure() => new("LayerX JSON must use integer-only numbers.");
}
