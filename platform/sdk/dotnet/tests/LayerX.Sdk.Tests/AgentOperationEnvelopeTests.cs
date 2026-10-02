using System.Security.Cryptography.X509Certificates;
using System.Text;
using System.Text.Json;
using LayerX.Sdk;
using Xunit;
using Xunit.Abstractions;

namespace LayerX.Sdk.Tests;

[Trait("Category", "AgentOperationEnvelopeProbe")]
public sealed class AgentOperationEnvelopeTests(ITestOutputHelper output)
{
    private static readonly Dictionary<string, string> NoPathParameters = new(StringComparer.Ordinal);
    private static readonly Dictionary<string, PlatformOperation> AgentOperations =
        Enum.GetValues<PlatformOperation>().Where(value => value.Descriptor().Plane == PlatformPlane.Agent)
            .ToDictionary(value => value.Descriptor().Name, StringComparer.Ordinal);

    private sealed class CaseFile : IDisposable
    {
        public required Uri Gateway { get; init; }
        public required X509Certificate2 Root { get; init; }
        public required string KeyId { get; init; }
        public required byte[] KeySecret { get; init; }
        public required string Tenant { get; init; }
        public required byte[] SessionId { get; init; }
        public required byte[] TokenId { get; init; }
        public required ulong Generation { get; init; }
        public required JsonElement Requests { get; init; }
        public required string[] Cases { get; init; }
        public required string Phase { get; init; }
        public required string ResponseDirectory { get; init; }

        public void Dispose()
        {
            Root.Dispose();
            Array.Clear(KeySecret);
            Array.Clear(TokenId);
        }
    }

    private static InvalidOperationException Refused(string reason) => new($"agent envelope probe input refused: {reason}");

    private static string Text(JsonElement value, string field) =>
        value.TryGetProperty(field, out var raw) && raw.ValueKind == JsonValueKind.String && raw.GetString() is { Length: > 0 } text
            ? text : throw Refused(field);

    private static byte[] Hex32(JsonElement value, string field)
    {
        var text = Text(value, field);
        if (text.Length != 64 || text.Any(character => !(character is >= '0' and <= '9' or >= 'a' and <= 'f'))) throw Refused(field);
        return Convert.FromHexString(text);
    }

    private static string AbsoluteFile(JsonElement value, string field)
    {
        var path = Text(value, field);
        return Path.IsPathRooted(path) && File.Exists(path) ? path : throw Refused(field);
    }

    private static CaseFile Load()
    {
        var path = Environment.GetEnvironmentVariable("PAXEER_X_AGENT_ENVELOPE_CASE");
        if (string.IsNullOrEmpty(path) || !Path.IsPathRooted(path) || !File.Exists(path)) throw Refused("PAXEER_X_AGENT_ENVELOPE_CASE");
        using var document = JsonDocument.Parse(File.ReadAllBytes(path));
        var root = document.RootElement;
        if (root.ValueKind != JsonValueKind.Object) throw Refused("case file");

        var endpoint = new Uri(Text(root, "endpoint"), UriKind.Absolute);
        if (endpoint.Scheme != Uri.UriSchemeHttps || endpoint.AbsolutePath != AgentEnvelopeTransport.RoutePath ||
            !string.IsNullOrEmpty(endpoint.Query) || !string.IsNullOrEmpty(endpoint.Fragment) ||
            !string.Equals(endpoint.Host, Text(root, "server_name"), StringComparison.OrdinalIgnoreCase)) throw Refused("endpoint");

        X509Certificate2 ca;
        if (root.TryGetProperty("ca_pem", out _)) ca = X509Certificate2.CreateFromPemFile(AbsoluteFile(root, "ca_pem"));
        else ca = new X509Certificate2(File.ReadAllBytes(AbsoluteFile(root, "ca_der")));

        var key = File.ReadAllText(AbsoluteFile(root, "gateway_api_key_file"), new UTF8Encoding(false, true));
        if (key.EndsWith('\n')) key = key[..^1];
        var separator = key.IndexOf(':');
        if (separator <= 0 || key.Contains('\n') || key.Contains('\r')) throw Refused("gateway_api_key_file");

        using var credentialDocument = JsonDocument.Parse(File.ReadAllBytes(AbsoluteFile(root, "credential_file")));
        var credential = credentialDocument.RootElement;
        if (credential.ValueKind != JsonValueKind.Object || credential.EnumerateObject().Count() != 4) throw Refused("credential_file");
        var generation = Text(credential, "generation");
        if ((generation != "0" && generation[0] == '0') || generation.Any(character => character is < '0' or > '9') ||
            !ulong.TryParse(generation, out var parsedGeneration)) throw Refused("credential_file generation");

        if (!root.TryGetProperty("cases", out var cases) || cases.ValueKind != JsonValueKind.Array ||
            !root.TryGetProperty("requests", out var requests) || requests.ValueKind != JsonValueKind.Object) throw Refused("cases/requests");
        var directory = Text(root, "response_dir");
        if (!Path.IsPathRooted(directory) || !Directory.Exists(directory)) throw Refused("response_dir");

        return new CaseFile
        {
            Gateway = new Uri(endpoint.GetLeftPart(UriPartial.Authority) + "/"),
            Root = ca,
            KeyId = key[..separator],
            KeySecret = Encoding.UTF8.GetBytes(key[(separator + 1)..]),
            Tenant = Text(credential, "tenant"),
            SessionId = Hex32(credential, "session_id"),
            TokenId = Hex32(credential, "token_id"),
            Generation = parsedGeneration,
            Requests = requests.Clone(),
            Cases = cases.EnumerateArray().Select(item => item.ValueKind == JsonValueKind.String ? item.GetString()! : throw Refused("cases")).ToArray(),
            Phase = Text(root, "phase"),
            ResponseDirectory = directory,
        };
    }

    private static AgentEnvelopeTransport Transport(CaseFile input, AgentSessionCredential? credential, LayerXKeyCredential key) =>
        new(input.Gateway, credential, key, input.Root);

    private static AgentSessionCredential Credential(CaseFile input, ulong generation) =>
        new(input.Tenant, input.SessionId, input.TokenId, generation);

    private static TransportCall Call(CaseFile input, string caseId)
    {
        if (!input.Requests.TryGetProperty(caseId, out var entry) || entry.ValueKind != JsonValueKind.Object) throw Refused($"requests.{caseId}");
        var name = Text(entry, "operation");
        if (!AgentOperations.TryGetValue(name, out var operation)) throw Refused($"requests.{caseId}.operation");
        if (!entry.TryGetProperty("request", out var request) || request.ValueKind != JsonValueKind.Object) throw Refused($"requests.{caseId}.request");
        IdempotencyKey? idempotency = entry.TryGetProperty("idempotency_key", out _)
            ? new IdempotencyKey(Convert.ToHexString(Hex32(entry, "idempotency_key")).ToLowerInvariant()) : null;
        var value = JsonSerializer.Deserialize<JsonValue>(request.GetRawText())!;
        return new TransportCall(operation, value, NoPathParameters, idempotency);
    }

    private static void AssertSuccess(AgentEnvelopeResult result)
    {
        Assert.False(string.IsNullOrEmpty(result.RequestId));
        Assert.IsType<JsonValue.ObjectValue>(result.VerificationStatus);
        Assert.IsNotType<JsonValue.NullValue>(result.Value);
    }

    private void Emit(string line)
    {
        Console.Out.WriteLine(line);
        Console.Out.Flush();
        output.WriteLine(line);
    }

    [Fact]
    public async Task ProcessCasesReachTheDaemonThroughTheGateway()
    {
        using var input = Load();
        Assert.Equal("read", input.Phase);
        string[] expected = ["read", "program_read", "approval_list"];
        Assert.Equal(expected, input.Cases);
        string[] operations = ["read.account", "program.interface", "approval.list"];
        using var key = new LayerXKeyCredential(input.KeyId, input.KeySecret);
        using var credential = Credential(input, input.Generation);
        using var transport = Transport(input, credential, key);
        var passed = 0;
        for (var index = 0; index < input.Cases.Length; index++)
        {
            var caseId = input.Cases[index];
            var call = Call(input, caseId);
            Assert.Equal(operations[index], call.Operation.Descriptor().Name);
            int? status = null; byte[]? body = null;
            var result = await transport.SendEnvelopeAsync(call, observeResponse: (code, bytes) => { status = code; body = bytes; });
            Assert.Equal(200, status);
            using (var parsed = JsonDocument.Parse(body!))
            {
                var record = new Dictionary<string, object> { ["status"] = status!.Value, ["body"] = parsed.RootElement };
                await File.WriteAllBytesAsync(Path.Combine(input.ResponseDirectory, caseId + ".json"),
                    JsonSerializer.SerializeToUtf8Bytes(record));
            }
            AssertSuccess(result);
            passed++;
            Emit($"PAXEER_X_AGENT_ENVELOPE_CASE {caseId} passed");
        }
        Assert.Equal(expected.Length, passed);
        Emit($"PAXEER_X_AGENT_ENVELOPE_CASES={passed}");
    }

    [Fact]
    public async Task RetiredFaucetClaimReturnsTerminalUnavailableCapability()
    {
        using var input = Load();
        using var key = new LayerXKeyCredential(input.KeyId, input.KeySecret);
        using var credential = Credential(input, input.Generation);
        using var transport = Transport(input, credential, key);
        var error = await Assert.ThrowsAsync<PlatformSdkException>(() =>
            transport.SendEnvelopeAsync(new TransportCall(PlatformOperation.AgentFaucetClaim, JsonValue.EmptyObject, NoPathParameters, null)));
        Assert.Equal(SdkErrorCode.UnavailableCapability, error.Code);
        Assert.Equal(RetryClass.Never, error.Retry);
        Assert.False(string.IsNullOrEmpty(error.RequestId));
    }

    [Fact]
    public async Task WrongGenerationIsRefusedByTheDaemonSessionAuthority()
    {
        using var input = Load();
        using var key = new LayerXKeyCredential(input.KeyId, input.KeySecret);
        using var credential = Credential(input, input.Generation == ulong.MaxValue ? input.Generation - 1 : input.Generation + 1);
        using var transport = Transport(input, credential, key);
        var error = await Assert.ThrowsAsync<PlatformSdkException>(() => transport.SendEnvelopeAsync(Call(input, "read")));
        Assert.Equal(SdkErrorCode.PolicyRefusal, error.Code);
        Assert.Equal(RetryClass.Never, error.Retry);
    }

    [Fact]
    public async Task CatalogueReadWithoutSessionCredentialIsRefusedBeforeSending()
    {
        using var input = Load();
        using var key = new LayerXKeyCredential(input.KeyId, input.KeySecret);
        using var transport = Transport(input, null, key);
        var error = await Assert.ThrowsAsync<PlatformSdkException>(() => transport.SendEnvelopeAsync(Call(input, "read")));
        Assert.Equal(SdkErrorCode.CapabilityRefusal, error.Code);
    }

    [Fact]
    public async Task MutationWithoutIdempotencyKeyIsRefusedBeforeSending()
    {
        using var input = Load();
        using var key = new LayerXKeyCredential(input.KeyId, input.KeySecret);
        using var credential = Credential(input, input.Generation);
        using var transport = Transport(input, credential, key);
        var error = await Assert.ThrowsAsync<PlatformSdkException>(() =>
            transport.SendEnvelopeAsync(new TransportCall(PlatformOperation.AgentBudgetCreate, JsonValue.EmptyObject, NoPathParameters, null)));
        Assert.Equal(SdkErrorCode.IdempotencyRequired, error.Code);
    }

    [Fact]
    public void DirectDaemonSurfaceRefusesPlaintextAndMissingClientCertificateBeforeSending()
    {
        using (var handler = new HttpClientHandler { AllowAutoRedirect = false, UseCookies = false, UseProxy = false })
        {
            var plaintext = Assert.Throws<PlatformSdkException>(() =>
                AgentEnvelopeTransport.ForDaemon(new Uri("http://localhost/"), null, handler));
            Assert.Equal(SdkErrorCode.InvalidArgument, plaintext.Code);
            var noCertificate = Assert.Throws<PlatformSdkException>(() =>
                AgentEnvelopeTransport.ForDaemon(new Uri("https://localhost/"), null, handler));
            Assert.Equal(SdkErrorCode.InvalidArgument, noCertificate.Code);
        }
        using (var redirecting = new HttpClientHandler { UseCookies = false, UseProxy = false, ClientCertificateOptions = ClientCertificateOption.Automatic })
        {
            var redirect = Assert.Throws<PlatformSdkException>(() =>
                AgentEnvelopeTransport.ForDaemon(new Uri("https://localhost/"), null, redirecting));
            Assert.Equal(SdkErrorCode.InvalidArgument, redirect.Code);
        }
    }
}
