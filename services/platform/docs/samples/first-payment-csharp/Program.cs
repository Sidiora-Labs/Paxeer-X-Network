using System.Text;
using System.Text.Json;
using LayerX.Sdk;

static string Required(string name)
{
    var value = Environment.GetEnvironmentVariable(name);
    if (string.IsNullOrEmpty(value))
    {
        Console.Error.WriteLine($"first-payment-csharp: missing {name}");
        Environment.Exit(1);
    }
    return value!;
}

static IReadOnlyDictionary<string, JsonValue> Fields(JsonValue value) =>
    value is JsonValue.ObjectValue record ? record.Value : new Dictionary<string, JsonValue>();

static string Text(JsonValue value, string field) =>
    Fields(value).TryGetValue(field, out var found) && found is JsonValue.StringValue text ? text.Value : string.Empty;

static IReadOnlyList<JsonValue> Items(JsonValue value, string field) =>
    Fields(value).TryGetValue(field, out var found) && found is JsonValue.ArrayValue items ? items.Value : Array.Empty<JsonValue>();

var settled = new HashSet<string> { "done", "done-finalised", "refused" };
var completed = new HashSet<string> { "done", "done-finalised" };

var apiUrl = Required("LAYERX_API_URL");
var apiToken = Required("LAYERX_API_TOKEN");
var source = Required("LAYERX_SOURCE");
var destination = Required("LAYERX_DESTINATION");
var paymentKey = Required("LAYERX_PAYMENT_KEY");
var money = JsonValue.Object(new Dictionary<string, JsonValue>
{
    ["amount"] = JsonValue.String(Required("LAYERX_AMOUNT")),
    ["currency"] = JsonValue.String(Required("LAYERX_CURRENCY")),
});

// layerx:begin integration
using var token = new AccessToken(Encoding.UTF8.GetBytes(apiToken));
var layerx = new PlatformClient(new HumanHttpTransport(new Uri(apiUrl), accessToken: token));
var quote = await layerx.HumanMoveQuoteAsync(JsonValue.Object(new Dictionary<string, JsonValue> { ["source"] = JsonValue.String(source), ["destination"] = JsonValue.String(destination), ["money"] = money }));
var journey = await layerx.HumanMoveCommitAsync(JsonValue.Object(new Dictionary<string, JsonValue> { ["quote_id"] = JsonValue.String(Text(quote, "quote_id")) }), new IdempotencyKey(paymentKey));
// layerx:end integration

var journeyId = Text(journey, "journey_id");
var parameters = new Dictionary<string, string> { ["journey_id"] = journeyId };
for (var attempt = 0; attempt < 40 && !settled.Contains(Text(journey, "state")); attempt += 1)
{
    await Task.Delay(TimeSpan.FromMilliseconds(250));
    journey = await layerx.HumanJourneyGetAsync(pathParameters: parameters);
}

var receipts = Items(journey, "evidence")
    .Where(entry => Text(entry, "class") == "layerx-receipt")
    .Select(entry => Text(entry, "evidence_id"))
    .ToArray();
var report = new Dictionary<string, object?>
{
    ["journey_id"] = journeyId,
    ["state"] = Text(journey, "state"),
    ["receipts"] = receipts,
};
if (Fields(journey).TryGetValue("refusal", out var refusal))
{
    report["refused_by"] = Text(refusal, "refused_by");
    report["money_left"] = Fields(refusal).TryGetValue("money_left", out var left)
        && left is JsonValue.BooleanValue flag && flag.Value;
}
Console.WriteLine(JsonSerializer.Serialize(report));
if (!completed.Contains(Text(journey, "state")))
{
    Environment.Exit(2);
}
