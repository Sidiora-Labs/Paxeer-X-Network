#nullable enable

using System.Net;
using System.Net.Http.Headers;
using System.Net.Security;
using System.Numerics;
using System.Security.Cryptography;
using System.Security.Cryptography.X509Certificates;
using System.Text;
using System.Text.Json;
using System.Text.Json.Serialization;

namespace LayerX.Sdk;

public sealed class AccessToken : IDisposable
{
    private static readonly UTF8Encoding StrictUtf8 = new(false, true);
    private readonly SecretBytes _secret;

    public AccessToken(ReadOnlySpan<byte> bytes)
    {
        try { _ = StrictUtf8.GetCharCount(bytes); }
        catch (DecoderFallbackException) { throw new PlatformSdkException(SdkErrorCode.InvalidArgument, RetryClass.Never); }
        _secret = new SecretBytes(bytes);
    }

    internal void Authorize(HttpRequestMessage request) => _secret.Use(bytes =>
    {
        var value = StrictUtf8.GetString(bytes.Span);
        if (value.Contains('\r') || value.Contains('\n'))
            throw new PlatformSdkException(SdkErrorCode.InvalidArgument, RetryClass.Never);
        request.Headers.Authorization = new AuthenticationHeaderValue("Bearer", value);
        return true;
    });

    public void Dispose() => _secret.Dispose();
    public override string ToString() => "[REDACTED]";
}

public sealed class LayerXKeyCredential : IDisposable
{
    private static readonly UTF8Encoding StrictUtf8 = new(false, true);
    private readonly string _keyId;
    private readonly SecretBytes _secret;

    public LayerXKeyCredential(string keyId, ReadOnlySpan<byte> secret)
    {
        if (string.IsNullOrEmpty(keyId) || keyId.Length > 64 || keyId.Any(character =>
            !(character is >= 'a' and <= 'z' or >= 'A' and <= 'Z' or >= '0' and <= '9' or '-' or '_')))
            throw new PlatformSdkException(SdkErrorCode.InvalidArgument, RetryClass.Never);
        _keyId = keyId;
        _secret = new SecretBytes(secret);
    }

    internal void Authorize(HttpRequestMessage request) => _secret.Use(bytes =>
    {
        string value;
        try { value = StrictUtf8.GetString(bytes.Span); }
        catch (DecoderFallbackException) { throw new PlatformSdkException(SdkErrorCode.InvalidArgument, RetryClass.Never); }
        if (!value.StartsWith("lxp_live_", StringComparison.Ordinal) || value.Length != 73 ||
            value.Skip(9).Any(character => !(character is >= '0' and <= '9' or >= 'a' and <= 'f')))
            throw new PlatformSdkException(SdkErrorCode.InvalidArgument, RetryClass.Never);
        request.Headers.TryAddWithoutValidation("Authorization", $"LayerX-Key {_keyId}:{value}");
        return true;
    });

    public void Dispose() => _secret.Dispose();
    public override string ToString() => "[REDACTED]";
}

public sealed class AgentHttpTransport : IPlatformTransport
{
    private const int MaximumResponseBytes = 8 * 1024 * 1024;
    private const int MaximumProgramsRequestBytes = 8 * 1024 * 1024;
    private const int MaximumProgramBytes = 1_048_576;
    private static readonly JsonSerializerOptions JsonOptions = new(JsonSerializerDefaults.Web);
    private static readonly HashSet<string> Operations = new(StringComparer.Ordinal)
    {
        "program.discover", "program.interface", "program.simulate",
        "program.call", "program.receipt", "program.activity",
        "program.deploy", "program.upgrade", "program.wind-down",
    };
    private readonly Uri _baseUri;
    private readonly HttpClient _httpClient;
    private readonly LayerXKeyCredential? _credential;
    private readonly AccessToken? _accessToken;
    private sealed record ProgramRoute(HttpMethod Method, string Path,
        IReadOnlySet<string> PathParameters, bool Idempotent);
    private static readonly IReadOnlyDictionary<string, ProgramRoute> ProgramRoutes =
        new Dictionary<string, ProgramRoute>(StringComparer.Ordinal)
        {
            ["program.deploy"] = new(HttpMethod.Post, ProgramLifecycleRoutes.Paths["program.deploy"], new HashSet<string>(StringComparer.Ordinal), true),
            ["program.upgrade"] = new(HttpMethod.Post, ProgramLifecycleRoutes.Paths["program.upgrade"], new HashSet<string>(StringComparer.Ordinal), true),
            ["program.wind-down"] = new(HttpMethod.Post, ProgramLifecycleRoutes.Paths["program.wind-down"], new HashSet<string>(StringComparer.Ordinal), true),
            ["program.discover"] = new(HttpMethod.Get, "/v1/programs/registry/{program_id}",
                new HashSet<string>(["program_id"], StringComparer.Ordinal), false),
            ["program.interface"] = new(HttpMethod.Get, "/v1/programs/registry/{program_id}/interface",
                new HashSet<string>(["program_id"], StringComparer.Ordinal), false),
            ["program.simulate"] = new(HttpMethod.Post, "/v1/programs/simulate",
                new HashSet<string>(StringComparer.Ordinal), false),
            ["program.call"] = new(HttpMethod.Post, "/v1/programs/call",
                new HashSet<string>(StringComparer.Ordinal), true),
            ["program.receipt"] = new(HttpMethod.Get, "/v1/programs/receipts/by-idempotency/{idempotency_key}",
                new HashSet<string>(["idempotency_key"], StringComparer.Ordinal), false),
            ["program.activity"] = new(HttpMethod.Get, "/v1/programs/activities/{activity_id}",
                new HashSet<string>(["activity_id"], StringComparer.Ordinal), false),
        };

    public AgentHttpTransport(Uri baseUri, HttpClient? httpClient = null, LayerXKeyCredential? credential = null, AccessToken? accessToken = null)
    {
        if (credential is not null && accessToken is not null) throw new PlatformSdkException(SdkErrorCode.InvalidArgument, RetryClass.Never);
        _accessToken = accessToken;
        if (!baseUri.IsAbsoluteUri || !string.IsNullOrEmpty(baseUri.UserInfo) || string.IsNullOrEmpty(baseUri.Host) ||
            !string.IsNullOrEmpty(baseUri.Query) || !string.IsNullOrEmpty(baseUri.Fragment) ||
            (baseUri.Scheme != Uri.UriSchemeHttps && (baseUri.Scheme != Uri.UriSchemeHttp || !IsLoopback(baseUri.Host))))
            throw new PlatformSdkException(SdkErrorCode.InvalidArgument, RetryClass.Never);
        if (httpClient is not null)
            throw new PlatformSdkException(SdkErrorCode.InvalidArgument, RetryClass.Never);
        _baseUri = baseUri;
        _httpClient = new HttpClient(new HttpClientHandler { AllowAutoRedirect = false });
        _credential = credential;
    }

    public async Task<JsonValue> SendAsync(TransportCall call, CancellationToken cancellationToken = default)
    {
        var descriptor = call.Operation.Descriptor();
        if (descriptor.Plane != PlatformPlane.Agent || !Operations.Contains(descriptor.Name))
            throw new PlatformSdkException(SdkErrorCode.UnavailableCapability, RetryClass.Never);
        return await SendProgramAsync(new ProgramTransportCall(descriptor.Name, call.Request,
            call.PathParameters, call.IdempotencyKey), cancellationToken).ConfigureAwait(false);
    }

    internal HttpRequestMessage ProgramRequest(ProgramTransportCall call)
    {
        if (!ProgramRoutes.TryGetValue(call.Operation, out var route) ||
            !route.PathParameters.SetEquals(call.PathParameters.Keys)) throw Invalid();
        if (route.Idempotent)
        {
            if (call.IdempotencyKey is not { } key || !Hex32(key.Value))
                throw new PlatformSdkException(SdkErrorCode.IdempotencyRequired, RetryClass.Never);
        }
        else if (call.IdempotencyKey is not null)
            throw Invalid();
        ValidateProgramRequest(call);
        var encodedRequest = route.Method == HttpMethod.Post
            ? Convert.FromHexString(Text(ProgramMap(call.Request), "signed_activity"))
            : JsonSerializer.SerializeToUtf8Bytes(call.Request, JsonOptions);
        if (encodedRequest.Length == 0 || encodedRequest.Length > MaximumProgramsRequestBytes) throw Invalid();
        var path = route.Path;
        foreach (var name in route.PathParameters)
        {
            if (!call.PathParameters.TryGetValue(name, out var value) || !Hex32(value) ||
                !ProgramMap(call.Request).TryGetValue(name, out var rawBodyValue) ||
                rawBodyValue is not JsonValue.StringValue bodyValue || bodyValue.Value != value)
                throw Invalid();
            path = path.Replace("{" + name + "}", Uri.EscapeDataString(value), StringComparison.Ordinal);
        }
        var target = RootEndpoint(_baseUri, path);
        var request = new HttpRequestMessage(route.Method, target);
        request.Headers.Accept.Add(new MediaTypeWithQualityHeaderValue("application/json"));
        request.Headers.UserAgent.ParseAdd("layerx-dotnet/0.1.0");
        request.Content = new ByteArrayContent(encodedRequest);
        request.Content.Headers.ContentType = new MediaTypeHeaderValue(route.Method == HttpMethod.Post ? "application/octet-stream" : "application/json");
        if (call.IdempotencyKey is { } idempotency)
            request.Headers.TryAddWithoutValidation("Idempotency-Key", idempotency.Value);
        if (_accessToken is not null) _accessToken.Authorize(request);
        else if (_credential is not null) _credential.Authorize(request);
        else throw new PlatformSdkException(SdkErrorCode.CapabilityRefusal, RetryClass.Never);
        return request;
    }

    public async Task<JsonValue> SendProgramAsync(ProgramTransportCall call, CancellationToken cancellationToken = default)
    {
        using var request = ProgramRequest(call);
        HttpResponseMessage response;
        try
        {
            response = await _httpClient.SendAsync(request, HttpCompletionOption.ResponseHeadersRead,
                cancellationToken).ConfigureAwait(false);
        }
        catch when (call.Operation == "program.call" || ProgramLifecycleRoutes.Ordinals.ContainsKey(call.Operation)) { throw UnknownOutcome(); }
        catch (OperationCanceledException) { throw new PlatformSdkException(SdkErrorCode.Deadline, RetryClass.Safe); }
        catch { throw new PlatformSdkException(SdkErrorCode.TransportFailure, RetryClass.Safe); }
        using (response)
        {
            try
            {
                if (response.Content.Headers.ContentType?.MediaType is not string mediaType ||
                    !string.Equals(mediaType, "application/json", StringComparison.OrdinalIgnoreCase)) throw Decode();
                var encoded = await ReadBoundedAsync(response.Content, cancellationToken).ConfigureAwait(false);
                return DecodeProgramEnvelope(call.Operation, (int)response.StatusCode, encoded);
            }
            catch (PlatformSdkException error) when ((call.Operation == "program.call" || ProgramLifecycleRoutes.Ordinals.ContainsKey(call.Operation)) &&
                error.Code is SdkErrorCode.DecodeFailure or SdkErrorCode.VerificationFailure)
            {
                throw UnknownOutcome();
            }
        }
    }

    private static JsonValue DecodeProgramEnvelope(string operation, int status, byte[] encoded)
    {
        JsonValue? document;
        try { document = JsonSerializer.Deserialize<JsonValue>(encoded, JsonOptions); }
        catch (JsonException) { throw Decode(); }
        var envelope = ResponseMap(document ?? throw Decode());
        if ((ProgramLifecycleRoutes.Ordinals.ContainsKey(operation) || operation == "program.receipt") && status is >= 200 and < 300 && Exact(envelope, "result"))
            return envelope["result"];
        if (ProgramLifecycleRoutes.Ordinals.ContainsKey(operation) && status is >= 400 and < 500 && Exact(envelope, "error"))
        {
            var refusal = ResponseMap(envelope["error"]); var code = Text(refusal, "code"); var retry = Text(refusal, "retry");
            ulong? retryAfter = null;
            if (code.Length is 0 or > 256 || !(Exact(refusal, "code", "retry") || Exact(refusal, "code", "retry", "retry_after_seconds"))) throw Decode();
            if (retry == "after")
            {
                if (refusal.GetValueOrDefault("retry_after_seconds") is not JsonValue.IntegerValue seconds || seconds.Value <= 0) throw Decode();
                if ((ulong)seconds.Value > ulong.MaxValue / 1000) throw Decode();
                retryAfter = (ulong)seconds.Value * 1000;
            }
            else if (retry != "never" || !Exact(refusal, "code", "retry")) throw Decode();
            throw new PlatformSdkException(status switch { 401 or 403 => SdkErrorCode.CapabilityRefusal, 409 => SdkErrorCode.IdempotencyConflict, 429 => SdkErrorCode.RateLimit, _ => SdkErrorCode.CoreRejection }, retry == "after" ? RetryClass.After : RetryClass.Never, retryAfterMilliseconds: retryAfter);
        }
        if (envelope.ContainsKey("class"))
        {
            if (status is >= 200 and < 300 || !Exact(envelope,
                "class", "protocol_result_code", "retriability", "request_id", "reason")) throw Decode();
            throw ProgramServiceError(envelope);
        }
        var requestId = TryText(envelope, "request_id");
        if (status is < 200 or >= 300 || !Exact(envelope, "request_id", "value", "verification_status") ||
            requestId is null || !ValidRequestId(requestId) || !envelope.TryGetValue("value", out var value) || value is JsonValue.NullValue ||
            !envelope.TryGetValue("verification_status", out var verification) ||
            !ValidProgramVerification(operation, value, verification)) throw Decode(requestId);
        return value;
    }

    private static bool ValidProgramVerification(string operation, JsonValue value, JsonValue verification)
    {
        var status = verification is JsonValue.ObjectValue objectValue ? objectValue.Value : null;
        if (status is null) return false;
        if (operation is "program.discover" or "program.interface")
            return Exact(status, "state", "requested", "achieved", "reason") && Text(status, "state") == "Unverified" &&
                Text(status, "requested") == "SequencerSigned" && Text(status, "achieved") == "Unverified" &&
                Text(status, "reason") == "server_side_receipt_verification_only";
        var pending = (operation is "program.call" or "program.receipt" or "program.activity") &&
            value is JsonValue.ObjectValue pendingValue && TryText(pendingValue.Value, "state") is "unknown" or "pending";
        if (pending)
            return Exact(status, "state", "requested", "achieved", "reason") && Text(status, "state") == "Unverified" &&
                Text(status, "requested") == "SequencerSigned" && Text(status, "achieved") == "Unverified" &&
                Text(status, "reason") == "receipt_pending";
        return (operation is "program.simulate" or "program.call" or "program.receipt" or "program.activity") &&
            Exact(status, "state", "level") && Text(status, "state") == "Achieved" &&
            Text(status, "level") == "SequencerSigned";
    }

    private static PlatformSdkException ProgramServiceError(IReadOnlyDictionary<string, JsonValue> envelope)
    {
        var requestId = Text(envelope, "request_id"); var reason = Text(envelope, "reason");
        if (!ValidRequestId(requestId) || string.IsNullOrEmpty(reason) || reason.Length > 128 ||
            reason.Any(character => !(character is >= 'a' and <= 'z' or >= '0' and <= '9' or '_' or '.'))) throw Decode();
        int? resultCode = envelope["protocol_result_code"] switch
        {
            JsonValue.NullValue => null,
            JsonValue.IntegerValue integer when integer.Value is >= int.MinValue and <= int.MaxValue => (int)integer.Value,
            _ => throw Decode(requestId),
        };
        var code = Text(envelope, "class") switch
        {
            "TransportFailure" => SdkErrorCode.TransportFailure,
            "Deadline" => SdkErrorCode.Deadline,
            "ProtocolIncompatibility" => SdkErrorCode.ProtocolIncompatibility,
            "UnavailableCapability" => SdkErrorCode.UnavailableCapability,
            "CoreRejection" => SdkErrorCode.CoreRejection,
            "VerificationFailure" => SdkErrorCode.VerificationFailure,
            "PolicyRefusal" => SdkErrorCode.PolicyRefusal,
            "CapabilityRefusal" => SdkErrorCode.CapabilityRefusal,
            "BudgetRefusal" => SdkErrorCode.BudgetRefusal,
            "RateLimit" => SdkErrorCode.RateLimit,
            "IdempotencyConflict" => SdkErrorCode.IdempotencyConflict,
            "InternalFault" => SdkErrorCode.InternalFault,
            _ => throw Decode(requestId),
        };
        var retry = Text(envelope, "retriability") switch
        {
            "Terminal" => RetryClass.Never,
            "Retriable" => RetryClass.Safe,
            _ => throw Decode(requestId),
        };
        return new PlatformSdkException(code, retry, requestId, resultCode);
    }

    private static void ValidateProgramRequest(ProgramTransportCall call)
    {
        var value = ProgramMap(call.Request);
        if (ProgramLifecycleRoutes.Ordinals.TryGetValue(call.Operation, out var ordinal))
        {
            if (!Exact(value, "payload", "signed_activity") || !BoundedHex(value.GetValueOrDefault("payload"), MaximumProgramBytes, false) ||
                !BoundedHex(value.GetValueOrDefault("signed_activity"), MaximumProgramBytes, false)) throw Invalid();
            var bound = new NativeProgramLifecycleRequest(LifecycleWire.Decode(ordinal, Convert.FromHexString(Text(value, "payload"))), Convert.FromHexString(Text(value, "signed_activity")));
            if (call.IdempotencyKey?.Value != bound.IdempotencyKey) throw Invalid();
            return;
        }
        switch (call.Operation)
        {
            case "program.discover":
            case "program.interface":
                if (!Exact(value, "program_id", "requested_verification_level") ||
                    !CanonicalProgram(value.GetValueOrDefault("program_id")) ||
                    TryText(value, "requested_verification_level") != "sequencer-signed") throw Invalid();
                break;
            case "program.receipt":
                if (!Exact(value, "idempotency_key", "expected_activity_id", "requested_verification_level") ||
                    !CanonicalHex(value.GetValueOrDefault("idempotency_key"), 32, false) ||
                    !CanonicalHex(value.GetValueOrDefault("expected_activity_id"), 32, false) ||
                    TryText(value, "requested_verification_level") != "sequencer-signed") throw Invalid();
                break;
            case "program.activity":
                if (!Exact(value, "activity_id", "requested_verification_level") ||
                    !CanonicalHex(value.GetValueOrDefault("activity_id"), 32, false) ||
                    TryText(value, "requested_verification_level") != "sequencer-signed") throw Invalid();
                break;
            case "program.simulate":
            case "program.call":
                if (Exact(value, "payload", "signed_activity"))
                {
                    if (!BoundedHex(value.GetValueOrDefault("payload"), MaximumProgramBytes, false) || !BoundedHex(value.GetValueOrDefault("signed_activity"), MaximumProgramBytes, false)) throw Invalid();
                    var payload = NativeProgramCall.Decode(Convert.FromHexString(Text(value, "payload"))).Encode();
                    var key = LifecycleWire.Bind(3, payload, Convert.FromHexString(Text(value, "signed_activity")));
                    if (call.Operation == "program.call" && call.IdempotencyKey?.Value != Convert.ToHexString(key).ToLowerInvariant()) throw Invalid();
                }
                else ValidateProgramCall(value);
                break;
            default: throw Invalid();
        }
    }

    private static void ValidateProgramCall(IReadOnlyDictionary<string, JsonValue> value)
    {
        if (!Exact(value, "program_id", "calldata", "budget", "capabilities", "signed_activity") ||
            !CanonicalProgram(value.GetValueOrDefault("program_id")) ||
            !BoundedHex(value.GetValueOrDefault("calldata"), MaximumProgramBytes, true) ||
            !BoundedHex(value.GetValueOrDefault("signed_activity"), MaximumProgramBytes, false) ||
            value.GetValueOrDefault("budget") is not JsonValue.ObjectValue budget ||
            !Exact(budget.Value, "fuel", "fee_limit") || !CanonicalUInt64(budget.Value.GetValueOrDefault("fuel"), true) ||
            !CanonicalUInt128(budget.Value.GetValueOrDefault("fee_limit")) ||
            value.GetValueOrDefault("capabilities") is not JsonValue.ArrayValue capabilities || capabilities.Value.Count > 5)
            throw Invalid();
        string[] order = ["storage_read", "storage_write", "transfer", "emit_event", "compose"];
        var previous = -1;
        foreach (var capability in capabilities.Value)
        {
            var current = capability is JsonValue.StringValue name ? Array.IndexOf(order, name.Value) : -1;
            if (current <= previous) throw Invalid();
            previous = current;
        }
    }

    private static IReadOnlyDictionary<string, JsonValue> ProgramMap(JsonValue value) =>
        value is JsonValue.ObjectValue map ? map.Value : throw Invalid();
    private static IReadOnlyDictionary<string, JsonValue> ResponseMap(JsonValue value) =>
        value is JsonValue.ObjectValue map ? map.Value : throw Decode();
    private static bool Exact(IReadOnlyDictionary<string, JsonValue> value, params string[] fields) =>
        value.Count == fields.Length && fields.All(value.ContainsKey);
    private static string Text(IReadOnlyDictionary<string, JsonValue> value, string field) =>
        TryText(value, field) ?? throw Decode();
    private static string? TryText(IReadOnlyDictionary<string, JsonValue> value, string field) =>
        value.TryGetValue(field, out var raw) && raw is JsonValue.StringValue text ? text.Value : null;
    private static bool CanonicalProgram(JsonValue? value) => CanonicalHex(value, 32, false) &&
        value is JsonValue.StringValue text && text.Value != new string('0', 64);
    private static bool CanonicalHex(JsonValue? value, int bytes, bool empty) =>
        value is JsonValue.StringValue text && (empty && text.Value.Length == 0 ||
            text.Value.Length == bytes * 2 && text.Value.All(character => character is >= '0' and <= '9' or >= 'a' and <= 'f'));
    private static bool BoundedHex(JsonValue? value, int maximum, bool empty) =>
        value is JsonValue.StringValue text && text.Value.Length % 2 == 0 && text.Value.Length <= maximum * 2 &&
        (empty || text.Value.Length != 0) && text.Value.All(character => character is >= '0' and <= '9' or >= 'a' and <= 'f');
    private static bool CanonicalUInt64(JsonValue? value, bool positive) => value is JsonValue.StringValue text &&
        CanonicalDecimal(text.Value) && ulong.TryParse(text.Value, out var parsed) && (!positive || parsed > 0);
    private static bool CanonicalUInt128(JsonValue? value) => value is JsonValue.StringValue text &&
        CanonicalDecimal(text.Value) && BigInteger.TryParse(text.Value, out var parsed) &&
        parsed >= BigInteger.Zero && parsed < (BigInteger.One << 128);
    private static bool CanonicalDecimal(string value) => !string.IsNullOrEmpty(value) &&
        (value == "0" || value[0] != '0') && value.All(character => character is >= '0' and <= '9');
    private static bool ValidRequestId(string value) => !string.IsNullOrEmpty(value) && value.Length <= 128 &&
        value.All(character => character is >= (char)0x21 and <= (char)0x7e);

    private static PlatformSdkException ServiceError(AgentEnvelope envelope)
    {
        if (string.IsNullOrEmpty(envelope.RequestId) || string.IsNullOrEmpty(envelope.Reason) ||
            envelope.Reason.Any(character => !(character is >= 'a' and <= 'z' or >= '0' and <= '9' or '_' or '.')))
            throw Decode(envelope.RequestId);
        var code = envelope.ErrorClass switch
        {
            "TransportFailure" => SdkErrorCode.TransportFailure,
            "Deadline" => SdkErrorCode.Deadline,
            "ProtocolIncompatibility" => SdkErrorCode.ProtocolIncompatibility,
            "UnavailableCapability" => SdkErrorCode.UnavailableCapability,
            "CoreRejection" => SdkErrorCode.CoreRejection,
            "VerificationFailure" => SdkErrorCode.VerificationFailure,
            "PolicyRefusal" => SdkErrorCode.PolicyRefusal,
            "CapabilityRefusal" => SdkErrorCode.CapabilityRefusal,
            "BudgetRefusal" => SdkErrorCode.BudgetRefusal,
            "RateLimit" => SdkErrorCode.RateLimit,
            "IdempotencyConflict" => SdkErrorCode.IdempotencyConflict,
            "InternalFault" => SdkErrorCode.InternalFault,
            _ => throw Decode(envelope.RequestId),
        };
        var retry = envelope.Retriability switch
        {
            "Terminal" => RetryClass.Never,
            "Retriable" => RetryClass.Safe,
            _ => throw Decode(envelope.RequestId),
        };
        return new PlatformSdkException(code, retry, envelope.RequestId, envelope.ProtocolResultCode);
    }

    private static async Task<byte[]> ReadBoundedAsync(HttpContent content, CancellationToken cancellationToken)
    {
        await using var stream = await content.ReadAsStreamAsync(cancellationToken).ConfigureAwait(false);
        using var output = new MemoryStream();
        var buffer = new byte[16 * 1024];
        while (true)
        {
            var count = await stream.ReadAsync(buffer.AsMemory(), cancellationToken).ConfigureAwait(false);
            if (count == 0) return output.ToArray();
            if (output.Length + count > MaximumResponseBytes) throw Decode();
            output.Write(buffer, 0, count);
        }
    }

    private static string ResolvePath(string template, IReadOnlyDictionary<string, string> parameters)
    {
        var path = template;
        foreach (var (name, value) in parameters)
        {
            var token = "{" + name + "}";
            if (string.IsNullOrEmpty(name) || string.IsNullOrEmpty(value) || !path.Contains(token, StringComparison.Ordinal))
                throw new PlatformSdkException(SdkErrorCode.InvalidArgument, RetryClass.Never);
            path = path.Replace(token, Uri.EscapeDataString(value), StringComparison.Ordinal);
        }
        if (!path.StartsWith("/", StringComparison.Ordinal) || path.Contains('{') || path.Contains('}'))
            throw new PlatformSdkException(SdkErrorCode.InvalidArgument, RetryClass.Never);
        return path;
    }

    private static Uri Endpoint(Uri baseUri, string path)
    {
        var builder = new UriBuilder(baseUri);
        builder.Path = baseUri.AbsolutePath.TrimEnd('/') + path;
        builder.Query = ""; builder.Fragment = "";
        return builder.Uri;
    }

    private static Uri RootEndpoint(Uri baseUri, string path)
    {
        var builder = new UriBuilder(baseUri) { Path = path, Query = "", Fragment = "" };
        return builder.Uri;
    }

    private static HttpMethod ToHttpMethod(SdkHttpMethod method) => method switch
    {
        SdkHttpMethod.Get => HttpMethod.Get,
        SdkHttpMethod.Post => HttpMethod.Post,
        SdkHttpMethod.Put => HttpMethod.Put,
        SdkHttpMethod.Patch => HttpMethod.Patch,
        SdkHttpMethod.Delete => HttpMethod.Delete,
        _ => throw new PlatformSdkException(SdkErrorCode.InvalidArgument, RetryClass.Never),
    };

    private static bool IsLoopback(string host) => string.Equals(host, "localhost", StringComparison.OrdinalIgnoreCase) ||
        IPAddress.TryParse(host, out var address) && IPAddress.IsLoopback(address);
    private static bool Hex32(string value) => value.Length == 64 && value.All(character => character is >= '0' and <= '9' or >= 'a' and <= 'f');
    private static PlatformSdkException Invalid() => new(SdkErrorCode.InvalidArgument, RetryClass.Never);
    private static PlatformSdkException Decode(string? requestId = null) => new(SdkErrorCode.DecodeFailure, RetryClass.Never, requestId);
    private static PlatformSdkException UnknownOutcome() => new(SdkErrorCode.UnknownOutcome, RetryClass.UnknownOutcome);

    private sealed record AgentEnvelope(
        [property: JsonPropertyName("request_id")] string RequestId,
        [property: JsonPropertyName("value")] JsonValue? Value,
        [property: JsonPropertyName("verification_status")] JsonValue? VerificationStatus,
        [property: JsonPropertyName("class")] string? ErrorClass,
        [property: JsonPropertyName("protocol_result_code")] int? ProtocolResultCode,
        [property: JsonPropertyName("retriability")] string? Retriability,
        [property: JsonPropertyName("reason")] string? Reason);
}

public sealed class HumanHttpTransport : IPlatformTransport
{
    private const int MaximumResponseBytes = 8 * 1024 * 1024;
    private static readonly JsonSerializerOptions JsonOptions = new(JsonSerializerDefaults.Web);
    private readonly Uri _baseUri;
    private readonly HttpClient _httpClient;
    private readonly AccessToken? _accessToken;

    public HumanHttpTransport(Uri baseUri, HttpClient? httpClient = null, AccessToken? accessToken = null)
    {
        if (!baseUri.IsAbsoluteUri || !string.IsNullOrEmpty(baseUri.UserInfo) || string.IsNullOrEmpty(baseUri.Host) ||
            (baseUri.Scheme != Uri.UriSchemeHttps && (baseUri.Scheme != Uri.UriSchemeHttp || !IsLoopback(baseUri.Host))))
            throw new PlatformSdkException(SdkErrorCode.InvalidArgument, RetryClass.Never);
        _baseUri = baseUri;
        _httpClient = httpClient ?? new HttpClient();
        _accessToken = accessToken;
    }

    public async Task<JsonValue> SendAsync(TransportCall call, CancellationToken cancellationToken = default)
    {
        var descriptor = call.Operation.Descriptor();
        if (descriptor.Plane != PlatformPlane.Human)
            throw new PlatformSdkException(SdkErrorCode.UnavailableCapability, RetryClass.Never);
        var path = ResolvePath(descriptor.Path, call.PathParameters);
        var target = new Uri(_baseUri, path);
        if (target.Scheme != _baseUri.Scheme || target.Host != _baseUri.Host || target.Port != _baseUri.Port)
            throw new PlatformSdkException(SdkErrorCode.InvalidArgument, RetryClass.Never);

        using var request = new HttpRequestMessage(ToHttpMethod(descriptor.Method), target);
        request.Headers.Accept.Add(new MediaTypeWithQualityHeaderValue("application/json"));
        request.Headers.UserAgent.ParseAdd("layerx-dotnet/0.1.0");
        if (!descriptor.Bodyless)
        {
            var encoded = JsonSerializer.SerializeToUtf8Bytes(call.Request, JsonOptions);
            request.Content = new ByteArrayContent(encoded);
            request.Content.Headers.ContentType = new MediaTypeHeaderValue("application/json");
        }
        if (call.IdempotencyKey is { } key)
            request.Headers.TryAddWithoutValidation("Idempotency-Key", key.Value);
        _accessToken?.Authorize(request);

        using var response = await _httpClient.SendAsync(request, HttpCompletionOption.ResponseHeadersRead, cancellationToken).ConfigureAwait(false);
        var encodedResponse = await ReadBoundedAsync(response.Content, cancellationToken).ConfigureAwait(false);
        HumanEnvelope? envelope;
        try { envelope = JsonSerializer.Deserialize<HumanEnvelope>(encodedResponse, JsonOptions); }
        catch (JsonException) { throw new PlatformSdkException(SdkErrorCode.DecodeFailure, RetryClass.Never); }
        if (envelope is null || string.IsNullOrEmpty(envelope.Trace) || Encoding.UTF8.GetByteCount(envelope.Trace) > 512 ||
            envelope.Trace.Contains('\0') || envelope.Trace.Contains('\r') || envelope.Trace.Contains('\n'))
            throw new PlatformSdkException(SdkErrorCode.DecodeFailure, RetryClass.Never);
        if (envelope.Ok)
        {
            if (!response.IsSuccessStatusCode || envelope.Error is not null || envelope.Result is null)
                throw new PlatformSdkException(SdkErrorCode.DecodeFailure, RetryClass.Never);
            return envelope.Result;
        }
        if (response.IsSuccessStatusCode || envelope.Error is null || envelope.Result is not null)
            throw new PlatformSdkException(SdkErrorCode.DecodeFailure, RetryClass.Never);
        throw envelope.Error.ToSdkException(envelope.Trace);
    }

    private static async Task<byte[]> ReadBoundedAsync(HttpContent content, CancellationToken cancellationToken)
    {
        await using var stream = await content.ReadAsStreamAsync(cancellationToken).ConfigureAwait(false);
        using var output = new MemoryStream();
        var buffer = new byte[16 * 1024];
        while (true)
        {
            var count = await stream.ReadAsync(buffer.AsMemory(), cancellationToken).ConfigureAwait(false);
            if (count == 0) return output.ToArray();
            if (output.Length + count > MaximumResponseBytes)
                throw new PlatformSdkException(SdkErrorCode.DecodeFailure, RetryClass.Never);
            output.Write(buffer, 0, count);
        }
    }

    private static string ResolvePath(string template, IReadOnlyDictionary<string, string> parameters)
    {
        var path = template;
        foreach (var (name, value) in parameters)
        {
            var token = "{" + name + "}";
            if (string.IsNullOrEmpty(name) || string.IsNullOrEmpty(value) || !path.Contains(token, StringComparison.Ordinal))
                throw new PlatformSdkException(SdkErrorCode.InvalidArgument, RetryClass.Never);
            path = path.Replace(token, Uri.EscapeDataString(value), StringComparison.Ordinal);
        }
        if (!path.StartsWith("/", StringComparison.Ordinal) || path.Contains('{') || path.Contains('}'))
            throw new PlatformSdkException(SdkErrorCode.InvalidArgument, RetryClass.Never);
        return path;
    }

    private static HttpMethod ToHttpMethod(SdkHttpMethod method) => method switch
    {
        SdkHttpMethod.Get => HttpMethod.Get,
        SdkHttpMethod.Post => HttpMethod.Post,
        SdkHttpMethod.Put => HttpMethod.Put,
        SdkHttpMethod.Patch => HttpMethod.Patch,
        SdkHttpMethod.Delete => HttpMethod.Delete,
        _ => throw new PlatformSdkException(SdkErrorCode.InvalidArgument, RetryClass.Never),
    };

    private static bool IsLoopback(string host)
    {
        if (string.Equals(host, "localhost", StringComparison.OrdinalIgnoreCase)) return true;
        return IPAddress.TryParse(host, out var address) && IPAddress.IsLoopback(address);
    }

    private sealed record HumanEnvelope(
        [property: JsonPropertyName("ok")] bool Ok,
        [property: JsonPropertyName("result")] JsonValue? Result,
        [property: JsonPropertyName("error")] HumanApiError? Error,
        [property: JsonPropertyName("trace")] string Trace);

    private sealed record HumanApiError(
        [property: JsonPropertyName("code")] string Code,
        [property: JsonPropertyName("retry")] string Retry,
        [property: JsonPropertyName("retry_after_ms")] ulong? RetryAfterMilliseconds)
    {
        public PlatformSdkException ToSdkException(string trace)
        {
            var code = Code switch
            {
                "rate-limited" => SdkErrorCode.RateLimit,
                "unavailable" or "upstream-degraded" => SdkErrorCode.TransportFailure,
                "refused-by-policy" => SdkErrorCode.PolicyRefusal,
                "refused-by-budget" or "refused-by-limit" => SdkErrorCode.BudgetRefusal,
                "refused-by-capability" or "forbidden" or "unauthenticated" or "session-expired" or "step-up-required" => SdkErrorCode.CapabilityRefusal,
                "conflict" => SdkErrorCode.IdempotencyConflict,
                _ => SdkErrorCode.CoreRejection,
            };
            var retry = Retry switch
            {
                "retriable" => RetryClass.Safe,
                "retriable-after" when RetryAfterMilliseconds is not null => RetryClass.After,
                "structural" or "final" => RetryClass.Never,
                _ => throw new PlatformSdkException(SdkErrorCode.DecodeFailure, RetryClass.Never),
            };
            return new PlatformSdkException(code, retry, trace, retryAfterMilliseconds: RetryAfterMilliseconds);
        }
    }
}

public sealed class AgentSessionCredential : IDisposable
{
    private static readonly UTF8Encoding StrictUtf8 = new(false, true);
    private readonly SecretBytes _tokenId;

    public string Tenant { get; }
    public string SessionId { get; }
    public ulong Generation { get; }

    public AgentSessionCredential(string tenant, ReadOnlySpan<byte> sessionId, ReadOnlySpan<byte> tokenId, ulong generation)
    {
        int tenantBytes;
        try { tenantBytes = tenant is null ? 0 : StrictUtf8.GetByteCount(tenant); }
        catch (EncoderFallbackException) { throw new PlatformSdkException(SdkErrorCode.InvalidArgument, RetryClass.Never); }
        if (tenantBytes is 0 or > 255 || tenant!.Contains('\0') || sessionId.Length != 32 || tokenId.Length != 32)
            throw new PlatformSdkException(SdkErrorCode.InvalidArgument, RetryClass.Never);
        Tenant = tenant;
        SessionId = Convert.ToHexString(sessionId).ToLowerInvariant();
        Generation = generation;
        _tokenId = new SecretBytes(tokenId);
    }

    internal void Write(Utf8JsonWriter writer) => _tokenId.Use(token =>
    {
        writer.WriteStartObject();
        writer.WriteString("tenant", Tenant);
        writer.WriteString("session_id", SessionId);
        writer.WriteString("token_id", Convert.ToHexString(token.Span).ToLowerInvariant());
        writer.WriteString("generation", Generation.ToString(System.Globalization.CultureInfo.InvariantCulture));
        writer.WriteEndObject();
        return true;
    });

    public void Dispose() => _tokenId.Dispose();
    public override string ToString() => "[REDACTED]";
}

public sealed record AgentEnvelopeResult(string RequestId, JsonValue Value, JsonValue VerificationStatus);

public sealed class AgentEnvelopeTransport : IPlatformTransport, IDisposable
{
    public const string RoutePath = "/v1/agent/rpc";
    public const string DaemonRoutePath = "/rpc";
    public const int MaximumRequestBytes = 1_048_576;
    private const int MaximumResponseBytes = 8 * 1024 * 1024;
    private static readonly JsonSerializerOptions JsonOptions = new(JsonSerializerDefaults.Web);
    private static readonly HashSet<string> BootstrapOperations = new(StringComparer.Ordinal) { "agent.register", "session.open" };
    private static readonly string[] Levels =
    [
        "Unverified", "SequencerSigned", "BatchIncluded", "StateProven", "CheckpointFinalised", "SettlementAnchored",
    ];
    private readonly Uri _endpoint;
    private readonly HttpClient _httpClient;
    private readonly AgentSessionCredential? _credential;
    private readonly LayerXKeyCredential? _gatewayKey;

    public AgentEnvelopeTransport(Uri baseUri, AgentSessionCredential? credential, LayerXKeyCredential? gatewayKey = null,
        X509Certificate2? trustedRoot = null)
    {
        if (!baseUri.IsAbsoluteUri || !string.IsNullOrEmpty(baseUri.UserInfo) || string.IsNullOrEmpty(baseUri.Host) ||
            !string.IsNullOrEmpty(baseUri.Query) || !string.IsNullOrEmpty(baseUri.Fragment) ||
            baseUri.AbsolutePath != "/" ||
            (baseUri.Scheme != Uri.UriSchemeHttps && (baseUri.Scheme != Uri.UriSchemeHttp || !IsLoopback(baseUri.Host))) ||
            (trustedRoot is not null && baseUri.Scheme != Uri.UriSchemeHttps))
            throw new PlatformSdkException(SdkErrorCode.InvalidArgument, RetryClass.Never);
        _endpoint = new UriBuilder(baseUri) { Path = RoutePath, Query = "", Fragment = "" }.Uri;
        _credential = credential;
        _gatewayKey = gatewayKey;
        var handler = new SocketsHttpHandler { AllowAutoRedirect = false, UseCookies = false, UseProxy = false };
        if (trustedRoot is not null)
        {
            var root = new X509Certificate2(trustedRoot);
            handler.SslOptions = new SslClientAuthenticationOptions
            {
                RemoteCertificateValidationCallback = (_, certificate, presented, errors) =>
                {
                    if (certificate is null || (errors & ~SslPolicyErrors.RemoteCertificateChainErrors) != SslPolicyErrors.None)
                        return false;
                    using var chain = new X509Chain();
                    chain.ChainPolicy.TrustMode = X509ChainTrustMode.CustomRootTrust;
                    chain.ChainPolicy.RevocationMode = X509RevocationMode.NoCheck;
                    chain.ChainPolicy.CustomTrustStore.Add(root);
                    if (presented is not null)
                        foreach (var element in presented.ChainElements) chain.ChainPolicy.ExtraStore.Add(element.Certificate);
                    using var leaf = new X509Certificate2(certificate);
                    return chain.Build(leaf);
                },
            };
        }
        _httpClient = new HttpClient(handler, true);
    }

    private AgentEnvelopeTransport(Uri endpoint, AgentSessionCredential? credential, HttpClientHandler handler)
    {
        _endpoint = endpoint;
        _credential = credential;
        _gatewayKey = null;
        _httpClient = new HttpClient(handler, false);
    }

    public static AgentEnvelopeTransport ForDaemon(Uri baseUri, AgentSessionCredential? credential, HttpClientHandler handler)
    {
        ArgumentNullException.ThrowIfNull(baseUri);
        ArgumentNullException.ThrowIfNull(handler);
        if (!baseUri.IsAbsoluteUri || baseUri.Scheme != Uri.UriSchemeHttps || !string.IsNullOrEmpty(baseUri.UserInfo) ||
            string.IsNullOrEmpty(baseUri.Host) || !string.IsNullOrEmpty(baseUri.Query) ||
            !string.IsNullOrEmpty(baseUri.Fragment) || baseUri.AbsolutePath != "/" ||
            handler.AllowAutoRedirect || handler.UseCookies || handler.UseProxy ||
            (handler.ClientCertificateOptions == ClientCertificateOption.Manual && handler.ClientCertificates.Count == 0))
            throw Invalid();
        return new AgentEnvelopeTransport(new UriBuilder(baseUri) { Path = DaemonRoutePath, Query = "", Fragment = "" }.Uri,
            credential, handler);
    }

    public async Task<JsonValue> SendAsync(TransportCall call, CancellationToken cancellationToken = default) =>
        (await SendEnvelopeAsync(call, cancellationToken).ConfigureAwait(false)).Value;

    public async Task<AgentEnvelopeResult> SendEnvelopeAsync(TransportCall call, CancellationToken cancellationToken = default,
        Action<int, byte[]>? observeResponse = null)
    {
        var descriptor = call.Operation.Descriptor();
        var mutating = descriptor.RequiresIdempotency;
        var requestId = NewRequestId();
        using var request = EnvelopeRequest(descriptor, call, requestId);
        HttpResponseMessage response;
        try
        {
            response = await _httpClient.SendAsync(request, HttpCompletionOption.ResponseHeadersRead, cancellationToken).ConfigureAwait(false);
        }
        catch when (mutating) { throw UnknownOutcome(requestId); }
        catch (OperationCanceledException) { throw new PlatformSdkException(SdkErrorCode.Deadline, RetryClass.Safe, requestId); }
        catch { throw new PlatformSdkException(SdkErrorCode.TransportFailure, RetryClass.Safe, requestId); }
        using (response)
        {
            byte[] encoded;
            try
            {
                if (response.Content.Headers.ContentType?.MediaType is not string mediaType ||
                    !string.Equals(mediaType, "application/json", StringComparison.OrdinalIgnoreCase))
                    throw mutating ? UnknownOutcome(requestId) : new PlatformSdkException(SdkErrorCode.TransportFailure, RetryClass.Safe, requestId);
                encoded = await ReadBoundedAsync(response.Content, requestId, cancellationToken).ConfigureAwait(false);
            }
            catch (PlatformSdkException) when (mutating) { throw UnknownOutcome(requestId); }
            catch (PlatformSdkException) { throw; }
            catch when (mutating) { throw UnknownOutcome(requestId); }
            catch (OperationCanceledException) { throw new PlatformSdkException(SdkErrorCode.Deadline, RetryClass.Safe, requestId); }
            catch { throw new PlatformSdkException(SdkErrorCode.TransportFailure, RetryClass.Safe, requestId); }
            observeResponse?.Invoke((int)response.StatusCode, encoded);
            if ((int)response.StatusCode is 502 or 503 && IsEdgeReply(encoded))
                throw mutating ? UnknownOutcome(requestId) : new PlatformSdkException(SdkErrorCode.TransportFailure, RetryClass.Safe, requestId);
            try { return DecodeResponse((int)response.StatusCode, encoded, requestId); }
            catch (PlatformSdkException error) when (mutating && error.Code is SdkErrorCode.DecodeFailure or SdkErrorCode.VerificationFailure)
            {
                throw UnknownOutcome(requestId);
            }
        }
    }

    internal HttpRequestMessage EnvelopeRequest(OperationDescriptor descriptor, TransportCall call, string requestId)
    {
        if (descriptor.Plane != PlatformPlane.Agent || call.PathParameters.Count != 0 ||
            call.Request is not JsonValue.ObjectValue) throw Invalid();
        var bootstrap = BootstrapOperations.Contains(descriptor.Name);
        if (!bootstrap && _credential is null) throw new PlatformSdkException(SdkErrorCode.CapabilityRefusal, RetryClass.Never);
        if (descriptor.RequiresIdempotency)
        {
            if (call.IdempotencyKey is not { } key || !Hex32(key.Value))
                throw new PlatformSdkException(SdkErrorCode.IdempotencyRequired, RetryClass.Never);
        }
        else if (call.IdempotencyKey is not null) throw Invalid();
        using var buffer = new MemoryStream();
        using (var writer = new Utf8JsonWriter(buffer))
        {
            writer.WriteStartObject();
            writer.WriteNumber("version", 1);
            writer.WriteString("request_id", requestId);
            writer.WriteString("operation", descriptor.Name);
            writer.WritePropertyName("request");
            JsonSerializer.Serialize(writer, call.Request, JsonOptions);
            writer.WritePropertyName("credential");
            if (bootstrap) writer.WriteNullValue();
            else _credential!.Write(writer);
            if (call.IdempotencyKey is { } idempotency) writer.WriteString("idempotency_key", idempotency.Value);
            else writer.WriteNull("idempotency_key");
            writer.WriteEndObject();
        }
        var encoded = buffer.ToArray();
        if (encoded.Length > MaximumRequestBytes) throw Invalid();
        var request = new HttpRequestMessage(HttpMethod.Post, _endpoint);
        request.Headers.Accept.Add(new MediaTypeWithQualityHeaderValue("application/json"));
        request.Headers.UserAgent.ParseAdd("layerx-dotnet/0.1.0");
        request.Headers.TryAddWithoutValidation("LayerX-Request-Id", requestId);
        request.Content = new ByteArrayContent(encoded);
        request.Content.Headers.ContentType = new MediaTypeHeaderValue("application/json");
        request.Content.Headers.ContentLength = encoded.Length;
        _gatewayKey?.Authorize(request);
        return request;
    }

    internal static AgentEnvelopeResult DecodeResponse(int status, byte[] encoded, string requestId)
    {
        JsonValue? document;
        try
        {
            using (var parsed = JsonDocument.Parse(encoded, new JsonDocumentOptions { MaxDepth = 64 }))
                if (HasDuplicateKey(parsed.RootElement)) throw Decode(requestId);
            document = JsonSerializer.Deserialize<JsonValue>(encoded, JsonOptions);
        }
        catch (JsonException) { throw Decode(requestId); }
        catch (ArgumentException) { throw Decode(requestId); }
        catch (PlatformSdkException) { throw Decode(requestId); }
        if (document is not JsonValue.ObjectValue map) throw Decode(requestId);
        var envelope = map.Value;
        if (envelope.ContainsKey("class"))
        {
            if (status is >= 200 and < 300 ||
                !Exact(envelope, "class", "protocol_result_code", "retriability", "request_id", "reason")) throw Decode(requestId);
            var echoed = TryText(envelope, "request_id");
            if (echoed != requestId && echoed != "0") throw Decode(requestId);
            throw ServiceError(envelope, requestId);
        }
        if (status != 200 || !Exact(envelope, "request_id", "value", "verification_status") ||
            TryText(envelope, "request_id") != requestId || envelope["value"] is JsonValue.NullValue ||
            !ValidVerification(envelope["verification_status"])) throw Decode(requestId);
        return new AgentEnvelopeResult(requestId, envelope["value"], envelope["verification_status"]);
    }

    private static bool ValidVerification(JsonValue value)
    {
        if (value is not JsonValue.ObjectValue map) return false;
        var status = map.Value;
        return TryText(status, "state") switch
        {
            "achieved" => Exact(status, "state", "level") && Level(TryText(status, "level")) >= 0,
            "unverified" => Exact(status, "state", "requested", "achieved", "reason") &&
                Level(TryText(status, "achieved")) is var achieved && achieved >= 0 &&
                achieved < Level(TryText(status, "requested")) && ValidReason(TryText(status, "reason")),
            _ => false,
        };
    }

    private static int Level(string? level) => level is null ? -1 : Array.IndexOf(Levels, level);

    private static PlatformSdkException ServiceError(IReadOnlyDictionary<string, JsonValue> envelope, string requestId)
    {
        if (!ValidReason(TryText(envelope, "reason"))) throw Decode(requestId);
        int? resultCode = envelope["protocol_result_code"] switch
        {
            JsonValue.NullValue => null,
            JsonValue.IntegerValue integer when integer.Value is >= int.MinValue and <= int.MaxValue => (int)integer.Value,
            _ => throw Decode(requestId),
        };
        var code = TryText(envelope, "class") switch
        {
            "TransportFailure" => SdkErrorCode.TransportFailure,
            "Deadline" => SdkErrorCode.Deadline,
            "ProtocolIncompatibility" => SdkErrorCode.ProtocolIncompatibility,
            "UnavailableCapability" => SdkErrorCode.UnavailableCapability,
            "CoreRejection" => SdkErrorCode.CoreRejection,
            "VerificationFailure" => SdkErrorCode.VerificationFailure,
            "PolicyRefusal" => SdkErrorCode.PolicyRefusal,
            "CapabilityRefusal" => SdkErrorCode.CapabilityRefusal,
            "BudgetRefusal" => SdkErrorCode.BudgetRefusal,
            "RateLimit" => SdkErrorCode.RateLimit,
            "IdempotencyConflict" => SdkErrorCode.IdempotencyConflict,
            "InternalFault" => SdkErrorCode.InternalFault,
            _ => throw Decode(requestId),
        };
        var retry = TryText(envelope, "retriability") switch
        {
            "Terminal" => RetryClass.Never,
            "Retriable" => RetryClass.Safe,
            _ => throw Decode(requestId),
        };
        return new PlatformSdkException(code, retry, requestId, resultCode);
    }

    private static bool IsEdgeReply(byte[] encoded)
    {
        try
        {
            using var parsed = JsonDocument.Parse(encoded);
            return parsed.RootElement.ValueKind == JsonValueKind.Object &&
                parsed.RootElement.TryGetProperty("ok", out var ok) && ok.ValueKind == JsonValueKind.False &&
                !parsed.RootElement.TryGetProperty("class", out _);
        }
        catch (JsonException) { return false; }
    }

    private static bool HasDuplicateKey(JsonElement element)
    {
        switch (element.ValueKind)
        {
            case JsonValueKind.Object:
                var names = new HashSet<string>(StringComparer.Ordinal);
                foreach (var property in element.EnumerateObject())
                    if (!names.Add(property.Name) || HasDuplicateKey(property.Value)) return true;
                return false;
            case JsonValueKind.Array:
                foreach (var item in element.EnumerateArray())
                    if (HasDuplicateKey(item)) return true;
                return false;
            default:
                return false;
        }
    }

    private static async Task<byte[]> ReadBoundedAsync(HttpContent content, string requestId, CancellationToken cancellationToken)
    {
        await using var stream = await content.ReadAsStreamAsync(cancellationToken).ConfigureAwait(false);
        using var output = new MemoryStream();
        var buffer = new byte[16 * 1024];
        while (true)
        {
            var count = await stream.ReadAsync(buffer.AsMemory(), cancellationToken).ConfigureAwait(false);
            if (count == 0) return output.ToArray();
            if (output.Length + count > MaximumResponseBytes) throw Decode(requestId);
            output.Write(buffer, 0, count);
        }
    }

    private static string NewRequestId()
    {
        Span<byte> bytes = stackalloc byte[8];
        ulong value;
        do
        {
            RandomNumberGenerator.Fill(bytes);
            value = System.Buffers.Binary.BinaryPrimitives.ReadUInt64LittleEndian(bytes);
        } while (value == 0);
        return value.ToString(System.Globalization.CultureInfo.InvariantCulture);
    }

    private static bool Exact(IReadOnlyDictionary<string, JsonValue> value, params string[] fields) =>
        value.Count == fields.Length && fields.All(value.ContainsKey);
    private static string? TryText(IReadOnlyDictionary<string, JsonValue> value, string field) =>
        value.TryGetValue(field, out var raw) && raw is JsonValue.StringValue text ? text.Value : null;
    private static bool ValidReason(string? reason) => !string.IsNullOrEmpty(reason) && reason.Length <= 128 &&
        reason.All(character => character is >= 'a' and <= 'z' or >= '0' and <= '9' or '_' or '.');
    private static bool IsLoopback(string host) => string.Equals(host, "localhost", StringComparison.OrdinalIgnoreCase) ||
        IPAddress.TryParse(host, out var address) && IPAddress.IsLoopback(address);
    private static bool Hex32(string value) => value.Length == 64 && value.All(character => character is >= '0' and <= '9' or >= 'a' and <= 'f');
    private static PlatformSdkException Invalid() => new(SdkErrorCode.InvalidArgument, RetryClass.Never);
    private static PlatformSdkException Decode(string requestId) => new(SdkErrorCode.DecodeFailure, RetryClass.Never, requestId);
    private static PlatformSdkException UnknownOutcome(string requestId) => new(SdkErrorCode.UnknownOutcome, RetryClass.UnknownOutcome, requestId);

    public void Dispose() => _httpClient.Dispose();
}
